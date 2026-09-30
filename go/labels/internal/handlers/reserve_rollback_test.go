// SEC-LABELS-RESERVE: testes da liberação resiliente de reserva no rollback.
//
// Cobre a barreira anti-vazamento (E2E-label-wallet-reserve-leak): quando a
// emissão da etiqueta falha após reservar saldo e ANTES do débito confirmado
// (inclusive quando DebitarReserva falha por timeout), a reserva DEVE ser
// liberada. Estes testes exercitam liberarReservaRollback — o corpo do defer de
// rollback do PostLabel — sem tocar banco, ME ou fila (todos nil no handler).
//
// O wallet.Client é construído via env + wallet.NewClient() (NÃO struct literal:
// o campo secret é unexported e inacessível do pacote handlers). O endpoint
// /internal/liberar-reserva é fakeado com httptest.Server.
package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/senderzz/labels-service/internal/wallet"
)

// newWalletForTest aponta o wallet.Client para um httptest.Server via env vars.
// Restaura as env vars no fim do teste. WALLET_INTERNAL_SECRET != "" é necessário
// senão o cliente fail-closed antes de qualquer chamada de rede.
func newWalletForTest(t *testing.T, baseURL string) *wallet.Client {
	t.Helper()
	t.Setenv("WALLET_SERVICE_URL", baseURL)
	t.Setenv("WALLET_INTERNAL_SECRET", "segredo-de-teste")
	return wallet.NewClient()
}

// TestLiberarReservaRollback_Sucesso: wallet responde {"ok":true} → a liberação é
// chamada exatamente uma vez para a tx reservada. Guarda o wiring do rollback:
// se o defer/método deixar de chamar LiberarReserva, hits == 0 e o teste falha.
func TestLiberarReservaRollback_Sucesso(t *testing.T) {
	var hits int32
	var gotTxID int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/liberar-reserva" {
			t.Errorf("path inesperado: %s", r.URL.Path)
		}
		atomic.AddInt32(&hits, 1)
		body, _ := io.ReadAll(r.Body)
		var req struct {
			TxID   int64  `json:"tx_id"`
			Motivo string `json:"motivo"`
		}
		_ = json.Unmarshal(body, &req)
		atomic.StoreInt64(&gotTxID, req.TxID)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv.Close()

	h := &LabelHandler{wallet: newWalletForTest(t, srv.URL)} // db/me/queue nil de propósito

	h.liberarReservaRollback(4242, 777)

	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Fatalf("LiberarReserva chamada %d vezes, esperava 1", n)
	}
	if tx := atomic.LoadInt64(&gotTxID); tx != 4242 {
		t.Fatalf("tx_id enviado = %d, esperava 4242", tx)
	}
}

// TestLiberarReservaRollback_BestEffortEmFalha: wallet recusa (500 / {"ok":false}).
// A liberação é best-effort: não deve entrar em pânico nem propagar — apenas loga
// e retorna. É o caminho "reconciliar manualmente". Cobre o cenário em que o
// DebitarReserva falhou por timeout E a liberação subsequente também falha.
func TestLiberarReservaRollback_BestEffortEmFalha(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "erro": "indisponível"})
	}))
	defer srv.Close()

	h := &LabelHandler{wallet: newWalletForTest(t, srv.URL)}

	// Não deve entrar em pânico nem bloquear; best-effort engole o erro.
	h.liberarReservaRollback(99, 1)

	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Fatalf("LiberarReserva chamada %d vezes, esperava 1 (best-effort, sem retry)", n)
	}
}

// TestLiberarReservaRollback_NoOpTxZero: txID == 0 (wallet desativado ou reserva
// nunca criada) → nenhuma chamada de rede. Guarda o early-return.
func TestLiberarReservaRollback_NoOpTxZero(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv.Close()

	h := &LabelHandler{wallet: newWalletForTest(t, srv.URL)}

	h.liberarReservaRollback(0, 1)

	if n := atomic.LoadInt32(&hits); n != 0 {
		t.Fatalf("txID==0 deveria ser no-op, mas wallet foi chamado %d vez(es)", n)
	}
}

// TestLiberarReservaRollback_NoOpWalletNil: h.wallet == nil → no-op, sem panic.
// Cobre o caso wallet desativado em que reservaTxID nunca foi setado.
func TestLiberarReservaRollback_NoOpWalletNil(t *testing.T) {
	h := &LabelHandler{wallet: nil}
	// Não deve entrar em pânico mesmo com txID != 0 e wallet nil.
	h.liberarReservaRollback(123, 1)
}
