// SEC-LABELS: testes de INTEGRAÇÃO da idempotência (mecanismo, não só o predicado).
//
// Ao contrário de labels_test.go (helpers puros), estes testes exercitam o caminho
// REAL que garante a idempotência — usando o pool Postgres de verdade — sem NUNCA
// chamar a ME externa:
//
//  1. PostLabel — short-circuit de duplicata: uma 2ª chamada para o MESMO
//     (wc_order_id, service_id) com etiqueta viva retorna `duplicate=true` ANTES
//     de Calculate/Reservar/CreateShipment. Provado injetando um MEClient cujo
//     transporte chama t.Fatal se for tocado: se a ME for chamada, o teste falha.
//
//  2. PostTrackingWebhook — dedup de replay: o MESMO evento (tracking_code, status)
//     enviado 2× colapsa via ON CONFLICT (uq_event_key) → 2ª resposta `duplicate=true`,
//     sem reexecutar o UPDATE. Garante anti-loop no replay do provedor.
//
// GATE: requer DATABASE_URL apontando para um Postgres com o schema de labels
// carregado (wc_me_labels, tpc_webhook_events). Sem isso → t.Skip, preservando o
// contrato "go test ./... verde sem infra" do projeto.
package handlers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/labels-service/internal/me"
)

// ─── infra de gate / fixtures ─────────────────────────────────────────────────

// dbPoolOrSkip conecta no DATABASE_URL ou pula o teste (gate de infra).
// Verifica também a presença das tabelas do schema — se ausentes, pula com motivo
// claro (DB existe mas schema não carregado neste ambiente).
func dbPoolOrSkip(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL não definida — pulando testes de idempotência com banco")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("DATABASE_URL inacessível (%v) — pulando testes de idempotência com banco", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("banco inacessível via Ping (%v) — pulando testes de idempotência com banco", err)
	}
	// Confere o schema mínimo necessário.
	var n int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM pg_tables
		  WHERE tablename IN ('wc_me_labels','tpc_webhook_events')`).Scan(&n)
	if err != nil || n < 2 {
		pool.Close()
		t.Skipf("schema de labels ausente (tabelas encontradas=%d, err=%v) — pulando", n, err)
	}
	// Fecha o pool via t.Cleanup (NÃO defer pool.Close() no chamador): t.Cleanup é
	// LIFO, então este close roda DEPOIS dos cleanups de limpeza de linhas que os
	// testes registram em seguida — caso contrário o pool estaria fechado e os
	// DELETE de limpeza falhariam silenciosamente (vazando linhas de teste).
	t.Cleanup(pool.Close)
	return pool
}

// failingME devolve um MEClient cujo transporte chama t.Fatal se invocado.
// Usado para PROVAR que o short-circuit de duplicata retorna antes de tocar a ME.
func failingME(t *testing.T) *me.MEClient {
	t.Helper()
	return &me.MEClient{
		Token:   "stub",
		BaseURL: "https://melhorenvio.com.br/api/v2", // passa o guard P2-02
		HTTPClient: &http.Client{
			Transport: roundTripFatal{t: t},
		},
	}
}

type roundTripFatal struct{ t *testing.T }

func (rt roundTripFatal) RoundTrip(r *http.Request) (*http.Response, error) {
	rt.t.Fatalf("ME foi chamada (%s %s) — o short-circuit de idempotência deveria ter retornado antes", r.Method, r.URL.Path)
	return nil, fmt.Errorf("inalcançável")
}

// uniqueOrderID gera um wc_order_id alto e único entre execuções E entre chamadas
// rápidas sucessivas (um contador atômico evita colisão quando dois testes pegam o
// mesmo nanossegundo). Faixa >= 900000000 isola das linhas reais.
var orderIDseq int64

func uniqueOrderID() int {
	n := atomic.AddInt64(&orderIDseq, 1)
	return 900000000 + int(time.Now().UnixNano()%70000000) + int(n*100)
}

// ─── 1) PostLabel — short-circuit de duplicata (sem tocar a ME) ───────────────

func TestPostLabel_IdempotenteNaoTocaME(t *testing.T) {
	pool := dbPoolOrSkip(t) // pool fechado via t.Cleanup dentro do helper (LIFO)
	ctx := context.Background()

	orderID := uniqueOrderID()
	const serviceID = 1
	// Pré-insere uma etiqueta VIVA (draft) para o par (order, service).
	// me_shipment_id é único por execução (deriva do orderID) — a tabela tem
	// UNIQUE(uq_me_shipment_id); um valor fixo colidiria entre reruns.
	var seededID int64
	err := pool.QueryRow(ctx,
		`INSERT INTO wc_me_labels
		     (wc_order_id, me_shipment_id, status, service_id, service_name, price, from_cep, to_cep)
		 VALUES ($1, $2, 'draft', $3, 'PAC', '23.45', '01001000', '20010000')
		 RETURNING id`,
		orderID, fmt.Sprintf("ship-seed-%d", orderID), serviceID,
	).Scan(&seededID)
	if err != nil {
		t.Fatalf("seed wc_me_labels: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM wc_me_labels WHERE wc_order_id = $1`, orderID)
	})

	// Handler com DB real + ME que falha se tocada; wallet/queue nil (o short-circuit
	// retorna antes de qualquer um deles e antes de ler o user_id do JWT).
	h := &LabelHandler{db: pool, me: failingME(t)}

	body, _ := json.Marshal(createLabelRequest{
		WCOrderID: orderID,
		ServiceID: serviceID,
		FromCEP:   "01001000",
		ToCEP:     "20010000",
		Products:  []me.CalcProduct{{Height: 11, Width: 15, Length: 20, Weight: 0.3, Quantity: 1}},
	})
	req := httptest.NewRequest(http.MethodPost, "/labels", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.PostLabel(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("PostLabel duplicado: status esperava 200, obteve %d (body=%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		OK        bool   `json:"ok"`
		Duplicate bool   `json:"duplicate"`
		LabelID   int64  `json:"label_id"`
		Status    string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("PostLabel duplicado: resposta não-JSON: %v (body=%s)", err, rec.Body.String())
	}
	if !resp.OK || !resp.Duplicate {
		t.Fatalf("PostLabel duplicado: esperava ok=true duplicate=true, obteve %+v", resp)
	}
	if resp.LabelID != seededID {
		t.Fatalf("PostLabel duplicado: esperava label_id da etiqueta semeada (%d), obteve %d", seededID, resp.LabelID)
	}
	// Não deve ter criado uma 2ª linha — continua exatamente 1 etiqueta para o par.
	var count int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM wc_me_labels WHERE wc_order_id = $1 AND service_id = $2`,
		orderID, serviceID).Scan(&count); err != nil {
		t.Fatalf("contagem pós-duplicata: %v", err)
	}
	if count != 1 {
		t.Fatalf("idempotência violada: esperava 1 etiqueta para o par, há %d", count)
	}
}

// TestPostLabel_CanceladaNaoBloqueia: a última etiqueta 'canceled' NÃO é idempotente —
// o fluxo prossegue (e aqui falha de propósito ao tocar a ME, provando que NÃO houve
// short-circuit). Documenta o complemento da regra labelBlocksReissue no nível do banco.
func TestPostLabel_CanceladaNaoBloqueia(t *testing.T) {
	pool := dbPoolOrSkip(t) // pool fechado via t.Cleanup dentro do helper (LIFO)
	ctx := context.Background()

	orderID := uniqueOrderID()
	const serviceID = 2
	if _, err := pool.Exec(ctx,
		`INSERT INTO wc_me_labels
		     (wc_order_id, me_shipment_id, status, service_id, service_name, price, from_cep, to_cep)
		 VALUES ($1, $2, 'canceled', $3, 'SEDEX', '40.00', '01001000', '20010000')`,
		orderID, fmt.Sprintf("ship-canceled-%d", orderID), serviceID,
	); err != nil {
		t.Fatalf("seed canceled: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM wc_me_labels WHERE wc_order_id = $1`, orderID)
	})

	// Aqui a ME DEVE ser tentada (sem short-circuit). Como queremos provar isso sem
	// ir à rede, injetamos um transporte que sinaliza "fui chamado" e devolve um erro
	// — o handler então retorna 502 (falha no Calculate), não 200/duplicate.
	var meHit bool
	h := &LabelHandler{db: pool, me: &me.MEClient{
		Token:   "stub",
		BaseURL: "https://melhorenvio.com.br/api/v2",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			meHit = true
			return nil, fmt.Errorf("rede desabilitada no teste")
		})},
	}}

	body, _ := json.Marshal(createLabelRequest{
		WCOrderID: orderID, ServiceID: serviceID,
		FromCEP: "01001000", ToCEP: "20010000",
		Products: []me.CalcProduct{{Height: 11, Width: 15, Length: 20, Weight: 0.3, Quantity: 1}},
	})
	req := httptest.NewRequest(http.MethodPost, "/labels", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.PostLabel(rec, req)

	if !meHit {
		t.Fatal("etiqueta 'canceled' deveria NÃO bloquear → o fluxo deveria ter chamado a ME (Calculate)")
	}
	// Calculate falhou (sem rede) → 502 Bad Gateway, nunca 200/duplicate.
	if rec.Code == http.StatusOK {
		t.Fatalf("etiqueta 'canceled' não deveria gerar resposta idempotente 200 (body=%s)", rec.Body.String())
	}
}

// roundTripFunc local (handlers) — adapta função a http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// ─── 2) PostTrackingWebhook — dedup de replay (ON CONFLICT) ───────────────────

func TestPostTrackingWebhook_ReplayDedup(t *testing.T) {
	pool := dbPoolOrSkip(t) // pool fechado via t.Cleanup dentro do helper (LIFO)
	ctx := context.Background()

	const secret = "segredo-webhook-teste"
	t.Setenv("TRACKING_WEBHOOK_SECRET", secret)

	// Tracking code único para isolar o evento e a linha de wc_me_labels.
	trackingCode := fmt.Sprintf("BRTEST%d", time.Now().UnixNano()%1000000000)
	orderID := uniqueOrderID()

	// Semeia uma etiqueta 'posted' com esse tracking_code para o UPDATE ter alvo.
	if _, err := pool.Exec(ctx,
		`INSERT INTO wc_me_labels
		     (wc_order_id, me_shipment_id, status, service_id, service_name, price, tracking_code)
		 VALUES ($1, $2, 'posted', 1, 'PAC', '23.45', $3)`,
		orderID, "ship-"+trackingCode, trackingCode,
	); err != nil {
		t.Fatalf("seed etiqueta com tracking_code: %v", err)
	}

	eventKey := fmt.Sprintf("track:%s:%s", trackingCode, "delivered")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM wc_me_labels WHERE wc_order_id = $1`, orderID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM tpc_webhook_events WHERE event_key = $1`, eventKey)
	})

	h := &LabelHandler{db: pool} // me/queue nil: o webhook não os usa neste caminho.

	payload := []byte(fmt.Sprintf(`{"tracking_code":%q,"status":"delivered"}`, trackingCode))
	sign := func(b []byte) string {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(b)
		return "sha256=" + hex.EncodeToString(mac.Sum(nil))
	}

	post := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/webhook/tracking", bytes.NewReader(payload))
		req.Header.Set("X-Webhook-Signature", sign(payload))
		rec := httptest.NewRecorder()
		h.PostTrackingWebhook(rec, req)
		return rec
	}

	// 1ª entrega: processa, atualiza 1 linha, NÃO é duplicata.
	rec1 := post()
	if rec1.Code != http.StatusOK {
		t.Fatalf("webhook 1ª entrega: status esperava 200, obteve %d (body=%s)", rec1.Code, rec1.Body.String())
	}
	var r1 struct {
		Duplicate bool  `json:"duplicate"`
		Updated   int64 `json:"updated"`
	}
	_ = json.Unmarshal(rec1.Body.Bytes(), &r1)
	if r1.Duplicate {
		t.Fatalf("webhook 1ª entrega não deveria ser duplicata: %s", rec1.Body.String())
	}
	if r1.Updated != 1 {
		t.Fatalf("webhook 1ª entrega: esperava 1 linha atualizada, obteve %d", r1.Updated)
	}

	// 2ª entrega (replay do MESMO evento): dedup via ON CONFLICT → duplicate=true,
	// SEM reexecutar o UPDATE (updated=0). É o anti-loop.
	rec2 := post()
	if rec2.Code != http.StatusOK {
		t.Fatalf("webhook replay: status esperava 200, obteve %d (body=%s)", rec2.Code, rec2.Body.String())
	}
	var r2 struct {
		Duplicate bool  `json:"duplicate"`
		Updated   int64 `json:"updated"`
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &r2); err != nil {
		t.Fatalf("webhook replay: resposta não-JSON: %v", err)
	}
	if !r2.Duplicate {
		t.Fatalf("webhook replay: esperava duplicate=true, obteve %s", rec2.Body.String())
	}
	if r2.Updated != 0 {
		t.Fatalf("webhook replay: esperava updated=0 (sem reexecução), obteve %d", r2.Updated)
	}

	// Exatamente 1 evento persistido para a chave — o replay não duplicou registro.
	var evCount int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM tpc_webhook_events WHERE event_key = $1`, eventKey).Scan(&evCount); err != nil {
		t.Fatalf("contagem de eventos: %v", err)
	}
	if evCount != 1 {
		t.Fatalf("idempotência de webhook violada: esperava 1 evento para a chave, há %d", evCount)
	}
}
