// Testes de INTEGRAÇÃO contra Postgres real — as invariantes financeiras
// DB-bound do go/wallet que os testes puros não conseguem cobrir.
//
// Por que aqui (e não só helpers puros): "idempotência de referência" e
// "reconcile" só existem CONTRA o banco — são o constraint UNIQUE
// (user_id, referencia, tipo) de tpc_transacoes e o ON CONFLICT (event_key) de
// tpc_webhook_events em ação, mais a query de divergência ledger×carteira do
// reconcile job. Um teste puro não exercita nada disso.
//
// Gate: pulam (t.Skip) quando DATABASE_URL não está setada — `go test` continua
// verde em CI sem banco. Com DATABASE_URL exportada, RODAM de verdade. (Não é
// build-tag de propósito: a DATABASE_URL fornecida deve DIRIGIR a execução sob
// `go test`, sem flag extra.)
//
// Isolamento: cada teste usa um namespace de user_id alto e único (testUserBase +
// offset), limpa o que criou em t.Cleanup, e NÃO usa t.Parallel (operações DB com
// FOR UPDATE/Serializable não devem correr concorrentes no mesmo schema de teste).
//
// SEC-WALLET-SEP: toca SOMENTE tabelas tpc_* (expedição/frete). ZERO referência a
// sz_cod_wallet_transactions (carteira COD/motoboy — território do portal).
package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/wallet-service/internal/middleware"
	"github.com/shopspring/decimal"
)

// testUserBase é a base do namespace de user_id dos testes de integração.
// Valor alto e improvável em dados reais para não colidir com carteiras de produção/dev.
const testUserBase int64 = 990000000

// requireDB abre um pool contra DATABASE_URL ou pula o teste se ausente.
// Falha o teste (não pula) se a URL existe mas a conexão não sobe — uma URL
// configurada que não conecta é um erro de ambiente que deve ser visível, não
// um skip silencioso.
func requireDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL não definida — pulando teste de integração DB")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("falha ao criar pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("DATABASE_URL definida mas banco inacessível: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// cleanupUser remove TODAS as linhas tpc_* de um user_id de teste, em ordem segura
// (transações/recargas/webhook_events antes da carteira). Idempotente.
func cleanupUser(t *testing.T, pool *pgxpool.Pool, userID int64) {
	t.Helper()
	ctx := context.Background()
	// webhook_events referenciam recarga_id — limpa por recarga do usuário.
	_, _ = pool.Exec(ctx,
		`DELETE FROM tpc_webhook_events
		 WHERE recarga_id IN (SELECT id FROM tpc_recargas WHERE user_id = $1)`, userID)
	_, _ = pool.Exec(ctx, `DELETE FROM tpc_transacoes WHERE user_id = $1`, userID)
	_, _ = pool.Exec(ctx, `DELETE FROM tpc_recargas WHERE user_id = $1`, userID)
	_, _ = pool.Exec(ctx, `DELETE FROM tpc_carteira WHERE user_id = $1`, userID)
}

// authReq monta um *http.Request com o user_id já injetado no contexto (igual ao
// que AuthJWT grava após validar o token) — permite testar handlers protegidos
// sem forjar JWT. A validação de token tem cobertura própria em AuthJWT.
func authReq(method, target, body string, userID int64) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	return req.WithContext(middleware.WithUserID(req.Context(), userID))
}

// saldoDe lê o saldo bruto da carteira (para asserts). Zero se não houver linha.
func saldoDe(t *testing.T, pool *pgxpool.Pool, userID int64) decimal.Decimal {
	t.Helper()
	var s string
	err := pool.QueryRow(context.Background(),
		`SELECT saldo FROM tpc_carteira WHERE user_id = $1`, userID).Scan(&s)
	if err != nil {
		return decimal.Zero
	}
	return decimal.RequireFromString(s)
}

// carteiraDe lê saldo e saldo_reservado como Decimal. Comparar via Decimal (não
// string) é obrigatório: o pgx escaneia numeric(10,2) no formato canônico do
// Postgres, que pode suprimir zeros à direita ("0" em vez de "0.00") — uma
// comparação de string falharia espúria onde o VALOR é idêntico.
func carteiraDe(t *testing.T, pool *pgxpool.Pool, userID int64) (saldo, reservado decimal.Decimal) {
	t.Helper()
	var s, r string
	err := pool.QueryRow(context.Background(),
		`SELECT saldo, saldo_reservado FROM tpc_carteira WHERE user_id = $1`, userID).Scan(&s, &r)
	if err != nil {
		return decimal.Zero, decimal.Zero
	}
	return decimal.RequireFromString(s), decimal.RequireFromString(r)
}

// ─── Idempotência de referência: CRÉDITO ─────────────────────────────────────

// TestCreditarIdempotenciaDeReferencia prova a invariante central do ledger:
// dois POST /creditar com a MESMA referência creditam UMA vez só. O segundo bate
// no UNIQUE (user_id, referencia, tipo) → ON CONFLICT DO NOTHING → resposta
// idempotente, saldo inalterado. Sem isso, um retry de webhook duplicaria dinheiro.
func TestCreditarIdempotenciaDeReferencia(t *testing.T) {
	pool := requireDB(t)
	userID := testUserBase + 1
	cleanupUser(t, pool, userID)
	t.Cleanup(func() { cleanupUser(t, pool, userID) })

	h := NewWalletHandler(pool)
	ref := fmt.Sprintf("itest-credito-%d", userID)
	body := fmt.Sprintf(`{"valor":"25.00","referencia":%q,"descricao":"itest"}`, ref)

	// 1º crédito: cria a transação, saldo = 25.00.
	rec1 := httptest.NewRecorder()
	h.PostCreditar(rec1, authReq(http.MethodPost, "/creditar", body, userID))
	if rec1.Code != http.StatusOK {
		t.Fatalf("1º creditar: status %d, body %s", rec1.Code, rec1.Body.String())
	}
	if got := saldoDe(t, pool, userID); !got.Equal(decimal.RequireFromString("25.00")) {
		t.Fatalf("após 1º crédito: saldo %s, esperado 25.00", got.StringFixed(2))
	}

	// 2º crédito com a MESMA referência: idempotente, saldo NÃO muda.
	rec2 := httptest.NewRecorder()
	h.PostCreditar(rec2, authReq(http.MethodPost, "/creditar", body, userID))
	if rec2.Code != http.StatusOK {
		t.Fatalf("2º creditar: status %d, body %s", rec2.Code, rec2.Body.String())
	}
	if !strings.Contains(rec2.Body.String(), `"idempotente":true`) {
		t.Errorf("2º creditar deveria sinalizar idempotente, body: %s", rec2.Body.String())
	}
	if got := saldoDe(t, pool, userID); !got.Equal(decimal.RequireFromString("25.00")) {
		t.Errorf("após 2º crédito (duplicado): saldo %s, esperado 25.00 (sem dobrar)", got.StringFixed(2))
	}

	// Há exatamente UMA transação de crédito para essa referência.
	var n int
	_ = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM tpc_transacoes
		 WHERE user_id = $1 AND referencia = $2 AND tipo = 'credito'`,
		userID, ref).Scan(&n)
	if n != 1 {
		t.Errorf("transações de crédito para a referência = %d, esperado 1", n)
	}
}

// ─── Idempotência de referência: RESERVA + ciclo de vida ─────────────────────

// TestReservarDebitarLiberarCiclo exercita o ciclo de reserva contra o banco real:
//   - creditar 100 → reservar 30 (disponível cai p/ 70, saldo intacto)
//   - reservar de novo MESMA referência → idempotente (não reserva de novo)
//   - debitar-reserva → saldo 70, reservado 0
//
// Cobre o FOR UPDATE + a aritmética de centavos (decimal) ponta-a-ponta.
func TestReservarDebitarLiberarCiclo(t *testing.T) {
	pool := requireDB(t)
	userID := testUserBase + 2
	cleanupUser(t, pool, userID)
	t.Cleanup(func() { cleanupUser(t, pool, userID) })

	h := NewWalletHandler(pool)

	// Credita 100 para ter saldo.
	credBody := fmt.Sprintf(`{"valor":"100.00","referencia":"itest-seed-%d","descricao":"seed"}`, userID)
	recCred := httptest.NewRecorder()
	h.PostCreditar(recCred, authReq(http.MethodPost, "/creditar", credBody, userID))
	if recCred.Code != http.StatusOK {
		t.Fatalf("seed creditar: status %d, body %s", recCred.Code, recCred.Body.String())
	}

	ref := fmt.Sprintf("itest-reserva-%d", userID)
	resBody := fmt.Sprintf(`{"valor":"30.00","referencia":%q,"descricao":"reserva itest"}`, ref)

	// eq compara dois Decimals (centavos exatos), formatando o erro de forma legível.
	eq := func(label string, got decimal.Decimal, want string) {
		if !got.Equal(decimal.RequireFromString(want)) {
			t.Errorf("%s = %s, esperado %s", label, got.StringFixed(2), want)
		}
	}

	// 1ª reserva: saldo 100 intacto, reservado 30.
	rec1 := httptest.NewRecorder()
	h.PostReservar(rec1, authReq(http.MethodPost, "/reservar", resBody, userID))
	if rec1.Code != http.StatusOK {
		t.Fatalf("reservar: status %d, body %s", rec1.Code, rec1.Body.String())
	}
	saldo, reservado := carteiraDe(t, pool, userID)
	eq("após reservar saldo", saldo, "100.00")
	eq("após reservar reservado", reservado, "30.00")

	// 2ª reserva MESMA referência → idempotente, reservado NÃO sobe para 60.
	rec2 := httptest.NewRecorder()
	h.PostReservar(rec2, authReq(http.MethodPost, "/reservar", resBody, userID))
	if rec2.Code != http.StatusOK || !strings.Contains(rec2.Body.String(), `"idempotente":true`) {
		t.Fatalf("reservar duplicada deveria ser idempotente: status %d body %s", rec2.Code, rec2.Body.String())
	}
	_, reservado = carteiraDe(t, pool, userID)
	eq("após reserva duplicada reservado (sem dobrar)", reservado, "30.00")

	// Debita a reserva: saldo 70, reservado 0.
	debBody := fmt.Sprintf(`{"referencia":%q}`, ref)
	recDeb := httptest.NewRecorder()
	h.PostDebitarReserva(recDeb, authReq(http.MethodPost, "/debitar-reserva", debBody, userID))
	if recDeb.Code != http.StatusOK {
		t.Fatalf("debitar-reserva: status %d, body %s", recDeb.Code, recDeb.Body.String())
	}
	saldo, reservado = carteiraDe(t, pool, userID)
	eq("após debitar-reserva saldo", saldo, "70.00")
	eq("após debitar-reserva reservado", reservado, "0.00")

	// Debitar de novo a MESMA reserva → idempotente (já confirmada).
	recDeb2 := httptest.NewRecorder()
	h.PostDebitarReserva(recDeb2, authReq(http.MethodPost, "/debitar-reserva", debBody, userID))
	if recDeb2.Code != http.StatusOK || !strings.Contains(recDeb2.Body.String(), `"idempotente":true`) {
		t.Fatalf("debitar-reserva repetido deveria ser idempotente: status %d body %s", recDeb2.Code, recDeb2.Body.String())
	}
	if got := saldoDe(t, pool, userID); !got.Equal(decimal.RequireFromString("70.00")) {
		t.Errorf("saldo após debitar-reserva repetido = %s, esperado 70.00 (sem dobrar débito)", got.StringFixed(2))
	}
}

// TestReservarSaldoInsuficiente prova que reservar acima do disponível é recusado
// (402) e não cria reserva — o FOR UPDATE + checagem de saldoDisponivel em ação.
func TestReservarSaldoInsuficiente(t *testing.T) {
	pool := requireDB(t)
	userID := testUserBase + 3
	cleanupUser(t, pool, userID)
	t.Cleanup(func() { cleanupUser(t, pool, userID) })

	h := NewWalletHandler(pool)
	// Sem crédito: saldo 0. Reservar 10 deve falhar com 402.
	body := fmt.Sprintf(`{"valor":"10.00","referencia":"itest-insuf-%d","descricao":"x"}`, userID)
	rec := httptest.NewRecorder()
	h.PostReservar(rec, authReq(http.MethodPost, "/reservar", body, userID))
	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("reservar sem saldo: status %d (esperado 402), body %s", rec.Code, rec.Body.String())
	}
	// Nenhuma reserva pendente criada.
	var n int
	_ = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM tpc_transacoes WHERE user_id=$1 AND status='pendente'`, userID).Scan(&n)
	if n != 0 {
		t.Errorf("reservas pendentes = %d, esperado 0 (reserva recusada não persiste)", n)
	}
}

// ─── Recarga + webhook PIX: confirmação e idempotência de event_key ──────────

// TestPixWebhookConfirmaRecargaEIdempotente prova o caminho real de crédito do
// webhook contra o banco: confirmarRecarga credita a carteira e marca a recarga
// como 'confirmado'; uma segunda passagem do MESMO event_key (retry do provedor)
// é absorvida por tpc_webhook_events.uq_event_key → ZERO crédito duplicado.
//
// Chamamos confirmarRecarga diretamente (em vez do HTTP PostPixWebhook) para não
// depender de WEBHOOK_SECRET/assinatura — o objetivo aqui é a invariante de
// crédito+idempotência, não a validação de assinatura (coberta noutro nível).
func TestPixWebhookConfirmaRecargaEIdempotente(t *testing.T) {
	pool := requireDB(t)
	userID := testUserBase + 4
	cleanupUser(t, pool, userID)
	t.Cleanup(func() { cleanupUser(t, pool, userID) })

	ctx := context.Background()

	// Cria a recarga pendente diretamente (espelha criarRecargaPendente).
	var recargaID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO tpc_recargas (user_id, valor, status, me_pix_id, expires_at)
		 VALUES ($1, '40.00', 'pendente', $2, NOW() + INTERVAL '15 minutes')
		 RETURNING id`,
		userID, fmt.Sprintf("ME-ITEST-%d", userID),
	).Scan(&recargaID); err != nil {
		t.Fatalf("inserir recarga: %v", err)
	}

	h := NewPixHandler(pool)
	recarga := recargaRow{
		ID:      recargaID,
		UserID:  userID,
		Valor:   decimal.RequireFromString("40.00"),
		Status:  "pendente",
		MePixID: fmt.Sprintf("ME-ITEST-%d", userID),
	}
	referencia := fmt.Sprintf("recarga:%d", recargaID)
	evento := pixWebhookEvent{
		EventKey:    fmt.Sprintf("pix:ME-ITEST-%d", userID),
		Source:      "pix",
		EventType:   "payment",
		PayloadHash: sha256hex([]byte("itest-payload")),
		RecargaID:   recargaID,
		MeID:        recarga.MePixID,
		Status:      "paid",
	}

	// 1ª confirmação: credita 40, recarga → confirmado, evento registrado.
	already, err := h.confirmarRecarga(ctx, recarga, referencia, evento)
	if err != nil {
		t.Fatalf("1ª confirmarRecarga: %v", err)
	}
	if already {
		t.Fatal("1ª confirmação não deveria reportar alreadyProcessed")
	}
	if got := saldoDe(t, pool, userID); !got.Equal(decimal.RequireFromString("40.00")) {
		t.Fatalf("após confirmação: saldo %s, esperado 40.00", got.StringFixed(2))
	}
	var status string
	_ = pool.QueryRow(ctx, `SELECT status FROM tpc_recargas WHERE id=$1`, recargaID).Scan(&status)
	if status != "confirmado" {
		t.Errorf("status da recarga = %q, esperado 'confirmado'", status)
	}

	// 2ª confirmação MESMO event_key (retry): idempotente, saldo NÃO dobra.
	already2, err := h.confirmarRecarga(ctx, recarga, referencia, evento)
	if err != nil {
		t.Fatalf("2ª confirmarRecarga: %v", err)
	}
	if !already2 {
		t.Error("2ª confirmação (mesmo event_key) deveria reportar alreadyProcessed=true")
	}
	if got := saldoDe(t, pool, userID); !got.Equal(decimal.RequireFromString("40.00")) {
		t.Errorf("após retry: saldo %s, esperado 40.00 (sem creditar de novo)", got.StringFixed(2))
	}
	// Exatamente 1 evento e 1 transação de crédito.
	var nEv, nTx int
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM tpc_webhook_events WHERE event_key=$1`, evento.EventKey).Scan(&nEv)
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM tpc_transacoes WHERE user_id=$1 AND referencia=$2 AND tipo='credito'`, userID, referencia).Scan(&nTx)
	if nEv != 1 {
		t.Errorf("eventos de webhook = %d, esperado 1", nEv)
	}
	if nTx != 1 {
		t.Errorf("transações de crédito = %d, esperado 1 (idempotência por referência)", nTx)
	}
}

// ─── Reconcile: divergência de saldo + expiração de recargas ─────────────────

// TestReconcileExpiraRecargasPendentes prova expirarRecargasPendentes contra o
// banco: uma recarga 'pendente' com expires_at no passado vira 'expirado'; uma
// pendente ainda válida permanece 'pendente'. Espelha tpc_expirar_recargas_antigas.
func TestReconcileExpiraRecargasPendentes(t *testing.T) {
	pool := requireDB(t)
	userID := testUserBase + 5
	cleanupUser(t, pool, userID)
	t.Cleanup(func() { cleanupUser(t, pool, userID) })

	ctx := context.Background()

	// Recarga vencida (expires_at no passado).
	var vencidaID int64
	_ = pool.QueryRow(ctx,
		`INSERT INTO tpc_recargas (user_id, valor, status, expires_at)
		 VALUES ($1, '15.00', 'pendente', NOW() - INTERVAL '1 hour') RETURNING id`,
		userID).Scan(&vencidaID)
	// Recarga ainda válida (expires_at no futuro).
	var validaID int64
	_ = pool.QueryRow(ctx,
		`INSERT INTO tpc_recargas (user_id, valor, status, expires_at)
		 VALUES ($1, '15.00', 'pendente', NOW() + INTERVAL '1 hour') RETURNING id`,
		userID).Scan(&validaID)

	// Roda só a etapa de expiração via SQL idêntico ao job (sem Asynq/Redis).
	tag, err := pool.Exec(ctx,
		`UPDATE tpc_recargas SET status = 'expirado'
		 WHERE status = 'pendente' AND expires_at < NOW() AND user_id = $1`,
		userID)
	if err != nil {
		t.Fatalf("expirar recargas: %v", err)
	}
	if tag.RowsAffected() < 1 {
		t.Fatalf("nenhuma recarga expirada, esperado >= 1")
	}

	var statusVencida, statusValida string
	_ = pool.QueryRow(ctx, `SELECT status FROM tpc_recargas WHERE id=$1`, vencidaID).Scan(&statusVencida)
	_ = pool.QueryRow(ctx, `SELECT status FROM tpc_recargas WHERE id=$1`, validaID).Scan(&statusValida)
	if statusVencida != "expirado" {
		t.Errorf("recarga vencida: status %q, esperado 'expirado'", statusVencida)
	}
	if statusValida != "pendente" {
		t.Errorf("recarga válida: status %q, esperado 'pendente' (não deve expirar)", statusValida)
	}
}

// TestReconcileDetectaDivergenciaDeSaldo prova a query de reconciliação
// ledger×carteira: quando o saldo da carteira NÃO bate com a soma das transações
// confirmadas, a divergência é detectada (a query de reconciliarSaldos a retorna).
// Reproduzimos o cenário forçando um saldo inconsistente e rodando a MESMA query
// de divergência, restrita ao user_id de teste.
func TestReconcileDetectaDivergenciaDeSaldo(t *testing.T) {
	pool := requireDB(t)
	userID := testUserBase + 6
	cleanupUser(t, pool, userID)
	t.Cleanup(func() { cleanupUser(t, pool, userID) })

	ctx := context.Background()

	// Carteira com saldo 100, mas SEM nenhuma transação confirmada → ledger diz 0.
	// Divergência = |100 - 0| = 100 > 0.01.
	if _, err := pool.Exec(ctx,
		`INSERT INTO tpc_carteira (user_id, saldo, saldo_reservado) VALUES ($1, '100.00', '0.00')`,
		userID); err != nil {
		t.Fatalf("inserir carteira divergente: %v", err)
	}

	// Mesma fórmula de reconciliarSaldos, restrita ao user_id de teste.
	var diff string
	err := pool.QueryRow(ctx,
		`SELECT ABS(c.saldo - COALESCE(SUM(
		     CASE
		         WHEN tx.tipo = 'credito' AND tx.status = 'confirmado' THEN tx.valor
		         WHEN tx.tipo IN ('debito','reserva') AND tx.status = 'confirmado' THEN -tx.valor
		         ELSE 0
		     END), 0))
		 FROM tpc_carteira c
		 LEFT JOIN tpc_transacoes tx ON tx.user_id = c.user_id
		 WHERE c.user_id = $1
		 GROUP BY c.user_id, c.saldo`,
		userID).Scan(&diff)
	if err != nil {
		t.Fatalf("query de divergência: %v", err)
	}
	d := decimal.RequireFromString(diff)
	if !d.GreaterThan(decimal.RequireFromString("0.01")) {
		t.Errorf("divergência detectada = %s, esperado > 0.01 (carteira 100 vs ledger 0)", d.StringFixed(2))
	}

	// Sanidade: uma carteira CONSISTENTE (saldo = soma do ledger) NÃO diverge.
	userOK := testUserBase + 7
	cleanupUser(t, pool, userOK)
	t.Cleanup(func() { cleanupUser(t, pool, userOK) })
	hw := NewWalletHandler(pool)
	body := fmt.Sprintf(`{"valor":"100.00","referencia":"itest-consist-%d","descricao":"x"}`, userOK)
	recC := httptest.NewRecorder()
	hw.PostCreditar(recC, authReq(http.MethodPost, "/creditar", body, userOK))
	if recC.Code != http.StatusOK {
		t.Fatalf("creditar consistente: status %d body %s", recC.Code, recC.Body.String())
	}
	var diffOK string
	_ = pool.QueryRow(ctx,
		`SELECT ABS(c.saldo - COALESCE(SUM(
		     CASE
		         WHEN tx.tipo = 'credito' AND tx.status = 'confirmado' THEN tx.valor
		         WHEN tx.tipo IN ('debito','reserva') AND tx.status = 'confirmado' THEN -tx.valor
		         ELSE 0
		     END), 0))
		 FROM tpc_carteira c
		 LEFT JOIN tpc_transacoes tx ON tx.user_id = c.user_id
		 WHERE c.user_id = $1
		 GROUP BY c.user_id, c.saldo`,
		userOK).Scan(&diffOK)
	if decimal.RequireFromString(diffOK).GreaterThan(decimal.RequireFromString("0.01")) {
		t.Errorf("carteira consistente acusou divergência %s, esperado ~0", diffOK)
	}
}
