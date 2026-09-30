// payments_test.go — AUDIT SEC-PAY (hardening + cobertura do caminho de pagamento).
//
// Cobre a superfície de SEGURANÇA do webhook de pagamento
// (POST /payments/webhook/{gateway}) e os helpers puros de payments.go.
//
// Estratégia (espelha tracking_sign_test.go + orders_test.go):
//   - A MAIORIA dos gates de segurança roda ANTES de qualquer acesso a banco
//     (fail-closed por secret vazio, validação HMAC, whitelist de status). Esses
//     são testes httptest puros, SEM banco — provam inclusive que a assinatura
//     CORRETA é aceita (status não-aprovado retorna 200 "ignorado" antes do 1º
//     SELECT). Ver TestWebhook_* abaixo.
//   - Apenas o teste de IDEMPOTÊNCIA da confirmação precisa de Postgres real; é
//     DB-gated (SKIP sem DATABASE_URL — go test local segue verde; no CI o
//     Postgres + 040-orders.sql estão presentes). Ver TestWebhook_IdempotentConfirm.
//   - Funções puras (HMAC, extractStr/extractInt64, nullableStrPayment) têm testes
//     unitários diretos, sem rede/DB.
//
// NOTA (correção de premissa): "secret vazio → fail-closed" é comportamento do
// HANDLER (retorna 503 antes de validar). hmacSHA256HexPayment com secret vazio
// devolve um HMAC válido de chave vazia — por isso o vetor de secret-vazio vive no
// bucket httptest (503), não num teste de função pura.
package handlers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const testWebhookSecret = "test-webhook-secret-ci"

// signPayment computa a assinatura que o handler espera no header X-Signature:
// "sha256=" + hex(HMAC-SHA256(body, secret)).
func signPayment(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// testWebhookRouter monta um router mínimo com APENAS a rota do webhook de
// pagamento — exatamente como em cmd/server/main.go (sem JWT, autenticado por HMAC).
// pool pode ser nil nos testes que retornam ANTES de qualquer acesso a banco.
func testWebhookRouter(pool *pgxpool.Pool) http.Handler {
	h := NewPaymentHandler(pool)
	r := chi.NewRouter()
	r.Post("/payments/webhook/{gateway}", h.PostPaymentWebhook)
	return r
}

// doWebhook dispara POST /payments/webhook/{gateway} com o corpo e a assinatura
// informados. sig=="" omite o header. Devolve o status HTTP e o corpo cru.
func doWebhook(t *testing.T, router http.Handler, gateway string, body []byte, sig string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/payments/webhook/"+gateway, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if sig != "" {
		req.Header.Set("X-Signature", sig)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// ── Fail-closed por secret vazio (handler, não helper) ─────────────────────────

// TestWebhook_NoSecretFailClosed: WEBHOOK_SECRET vazio → 503, sem tocar no banco
// (pool nil prova que retorna antes de qualquer SELECT).
func TestWebhook_NoSecretFailClosed(t *testing.T) {
	t.Setenv("WEBHOOK_SECRET", "")
	router := testWebhookRouter(nil)

	body := []byte(`{"status":"paid","id":"tx-1"}`)
	// Mesmo com assinatura "válida" para a chave vazia, o handler recusa por falta
	// de configuração (fail-closed). Não há como assinar sem secret de qualquer modo.
	code, _ := doWebhook(t, router, "pix", body, signPayment(body, ""))
	if code != http.StatusServiceUnavailable {
		t.Fatalf("secret vazio deveria dar 503 (fail-closed), deu %d", code)
	}
}

// ── Validação HMAC ─────────────────────────────────────────────────────────────

// TestWebhook_MissingSignature: secret configurado mas SEM header de assinatura → 401.
func TestWebhook_MissingSignature(t *testing.T) {
	t.Setenv("WEBHOOK_SECRET", testWebhookSecret)
	router := testWebhookRouter(nil)

	body := []byte(`{"status":"paid","id":"tx-1"}`)
	code, _ := doWebhook(t, router, "pix", body, "")
	if code != http.StatusUnauthorized {
		t.Fatalf("sem assinatura deveria dar 401, deu %d", code)
	}
}

// TestWebhook_WrongSignature: assinatura assinada com OUTRO secret → 401.
func TestWebhook_WrongSignature(t *testing.T) {
	t.Setenv("WEBHOOK_SECRET", testWebhookSecret)
	router := testWebhookRouter(nil)

	body := []byte(`{"status":"paid","id":"tx-1"}`)
	badSig := signPayment(body, "secret-errado")
	code, _ := doWebhook(t, router, "pix", body, badSig)
	if code != http.StatusUnauthorized {
		t.Fatalf("assinatura errada deveria dar 401, deu %d", code)
	}
}

// TestWebhook_TamperedBody: assinatura válida para o corpo ORIGINAL, mas o corpo
// foi adulterado depois de assinar → 401 (o HMAC não casa).
func TestWebhook_TamperedBody(t *testing.T) {
	t.Setenv("WEBHOOK_SECRET", testWebhookSecret)
	router := testWebhookRouter(nil)

	original := []byte(`{"status":"paid","id":"tx-1","amount":100}`)
	sig := signPayment(original, testWebhookSecret)
	tampered := []byte(`{"status":"paid","id":"tx-1","amount":999999}`)
	code, _ := doWebhook(t, router, "pix", tampered, sig)
	if code != http.StatusUnauthorized {
		t.Fatalf("corpo adulterado deveria dar 401, deu %d", code)
	}
}

// TestWebhook_ValidSigStatusNotApproved: assinatura CORRETA + status fora da
// whitelist {paid,approved} → 200 "ignorado", retornando ANTES do 1º SELECT.
// Este é o teste que prova que a validação HMAC PASSA sem precisar de banco
// (pool nil; se o handler chegasse ao SELECT, dava panic de nil pool).
func TestWebhook_ValidSigStatusNotApproved(t *testing.T) {
	t.Setenv("WEBHOOK_SECRET", testWebhookSecret)
	router := testWebhookRouter(nil)

	body := []byte(`{"status":"pending","id":"tx-1"}`)
	sig := signPayment(body, testWebhookSecret)
	code, respBody := doWebhook(t, router, "pix", body, sig)
	if code != http.StatusOK {
		t.Fatalf("status não-aprovado com sig válida deveria dar 200 (ignorado), deu %d", code)
	}
	if !strings.Contains(respBody, "ignorado") {
		t.Fatalf("resposta deveria indicar 'ignorado', veio %q", respBody)
	}
}

// TestWebhook_XHubSignatureFallback: o handler aceita X-Hub-Signature como
// fallback de X-Signature. Sig correta + status não-aprovado → 200 (sem banco).
func TestWebhook_XHubSignatureFallback(t *testing.T) {
	t.Setenv("WEBHOOK_SECRET", testWebhookSecret)
	router := testWebhookRouter(nil)

	body := []byte(`{"status":"pending","id":"tx-1"}`)
	req := httptest.NewRequest(http.MethodPost, "/payments/webhook/pix", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature", signPayment(body, testWebhookSecret))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("X-Hub-Signature válida + status não-aprovado deveria dar 200, deu %d", rec.Code)
	}
}

// ── Funções puras: HMAC ────────────────────────────────────────────────────────

// TestHmacSHA256HexPayment_Deterministic: mesmo corpo + mesmo secret → mesmo
// digest; corpo diferente OU secret diferente → digest diferente. 64 chars hex.
func TestHmacSHA256HexPayment_Deterministic(t *testing.T) {
	body := []byte(`{"status":"paid","id":"abc"}`)
	d1 := hmacSHA256HexPayment(body, "s1")
	d2 := hmacSHA256HexPayment(body, "s1")
	if d1 != d2 {
		t.Fatalf("HMAC não determinístico: %q != %q", d1, d2)
	}
	if len(d1) != 64 {
		t.Fatalf("HMAC-SHA256 hex deveria ter 64 chars, tem %d", len(d1))
	}
	if hmacSHA256HexPayment(body, "s2") == d1 {
		t.Fatal("secret diferente deveria produzir digest diferente")
	}
	if hmacSHA256HexPayment([]byte(`{"status":"paid","id":"xyz"}`), "s1") == d1 {
		t.Fatal("corpo diferente deveria produzir digest diferente")
	}
}

// TestHmacSHA256HexPayment_MatchesHmacEqual: o digest do helper, prefixado com
// "sha256=", casa via hmac.Equal com a assinatura esperada — replica exatamente a
// comparação do handler (constant-time).
func TestHmacSHA256HexPayment_MatchesHmacEqual(t *testing.T) {
	body := []byte(`{"status":"approved","id":"tx-9"}`)
	const secret = "abc123"
	expected := "sha256=" + hmacSHA256HexPayment(body, secret)
	got := signPayment(body, secret) // assinador independente do teste
	if !hmac.Equal([]byte(got), []byte(expected)) {
		t.Fatalf("hmac.Equal deveria casar: got=%q expected=%q", got, expected)
	}
	// Sig de outro secret NÃO deve casar.
	wrong := signPayment(body, "outro")
	if hmac.Equal([]byte(wrong), []byte(expected)) {
		t.Fatal("hmac.Equal NÃO deveria casar com secret errado")
	}
}

// ── Funções puras: extração de payload ─────────────────────────────────────────

// TestExtractStr: chaves diretas e caminhos com ponto; ignora vazios; respeita ordem.
func TestExtractStr(t *testing.T) {
	payload := map[string]any{
		"status": "paid",
		"id":     "",
		"payment": map[string]any{
			"id":     "tx-42",
			"status": "approved",
		},
	}
	if v := extractStr(payload, "status"); v != "paid" {
		t.Fatalf("status direto: esperado 'paid', veio %q", v)
	}
	// "id" direto é vazio → deve pular para o caminho aninhado "payment.id".
	if v := extractStr(payload, "id", "payment.id"); v != "tx-42" {
		t.Fatalf("fallback aninhado: esperado 'tx-42', veio %q", v)
	}
	if v := extractStr(payload, "payment.status"); v != "approved" {
		t.Fatalf("caminho aninhado: esperado 'approved', veio %q", v)
	}
	// Chave inexistente → "".
	if v := extractStr(payload, "naoexiste", "payment.naoexiste"); v != "" {
		t.Fatalf("chave inexistente deveria dar \"\", veio %q", v)
	}
}

// TestExtractInt64: int de float64 (JSON numbers), caminhos com ponto, e zero/ausente.
func TestExtractInt64(t *testing.T) {
	payload := map[string]any{
		"order_id": float64(123),
		"metadata": map[string]any{
			"order_id": float64(456),
		},
		"zero": float64(0),
	}
	if v := extractInt64(payload, "order_id"); v != 123 {
		t.Fatalf("order_id direto: esperado 123, veio %d", v)
	}
	if v := extractInt64(payload, "metadata.order_id"); v != 456 {
		t.Fatalf("caminho aninhado: esperado 456, veio %d", v)
	}
	// zero é tratado como ausente (v > 0 no helper) → cai pra próxima/0.
	if v := extractInt64(payload, "zero", "metadata.order_id"); v != 456 {
		t.Fatalf("zero deveria pular para fallback 456, veio %d", v)
	}
	if v := extractInt64(payload, "naoexiste"); v != 0 {
		t.Fatalf("chave inexistente deveria dar 0, veio %d", v)
	}
}

// TestNullableStrPayment: "" → nil; não-vazio → ponteiro para o valor.
func TestNullableStrPayment(t *testing.T) {
	if p := nullableStrPayment(""); p != nil {
		t.Fatalf("string vazia deveria dar nil, veio %v", *p)
	}
	if p := nullableStrPayment("tx-1"); p == nil || *p != "tx-1" {
		t.Fatalf("string não-vazia deveria dar ponteiro para 'tx-1', veio %v", p)
	}
}

// ── Idempotência da confirmação (DB-gated) ─────────────────────────────────────

// seedPaymentSeq garante order_number único e curto (≤20 chars, limite do schema).
var seedPaymentSeq atomic.Int64

// seedOrderWithPayment insere um pedido 'pending' + um pagamento 'pending' do
// gateway/ref informados, e devolve (orderID, paymentID). Limpa via t.Cleanup.
func seedOrderWithPayment(t *testing.T, pool *pgxpool.Pool, gateway, gatewayRef string) (int64, int64) {
	t.Helper()
	ctx := context.Background()
	orderNumber := "SZPAY-" +
		strconv.FormatInt(time.Now().UnixNano()%0x1000000, 36) + "-" +
		strconv.FormatInt(seedPaymentSeq.Add(1), 10)

	var orderID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO sz_orders (order_number, user_id, produtor_id, status, payment_status, total)
		 VALUES ($1, 0, 0, 'pending', 'pending', 100.00) RETURNING id`,
		orderNumber,
	).Scan(&orderID); err != nil {
		t.Fatalf("falha ao inserir pedido de teste: %v", err)
	}

	var paymentID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO sz_order_payments (order_id, gateway, gateway_ref, valor, status, created_at)
		 VALUES ($1, $2, $3, 100.00, 'pending', NOW()) RETURNING id`,
		orderID, gateway, gatewayRef,
	).Scan(&paymentID); err != nil {
		t.Fatalf("falha ao inserir pagamento de teste: %v", err)
	}

	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM sz_order_payments WHERE order_id = $1`, orderID)
		_, _ = pool.Exec(c, `DELETE FROM sz_order_status_history WHERE order_id = $1`, orderID)
		_, _ = pool.Exec(c, `DELETE FROM sz_orders WHERE id = $1`, orderID)
	})
	return orderID, paymentID
}

// TestWebhook_IdempotentConfirm: o cerne do SEC-PAY. Duas entregas IDÊNTICAS do
// mesmo gateway_ref/status='paid' confirmam o pagamento UMA vez:
//   - 1ª entrega → 200, payment vira 'paid', pedido transita pending→processing.
//   - 2ª entrega → 200 idempotente, SEM reprocessar.
//
// Asserções fortes: exatamente UMA transição para 'processing' no histórico e o
// pagamento continua 'paid' (paid_at não é re-disparado em loop). Prova que a
// guarda atômica (SELECT FOR UPDATE) impede duplo-processamento.
func TestWebhook_IdempotentConfirm(t *testing.T) {
	t.Setenv("WEBHOOK_SECRET", testWebhookSecret)
	dsn := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dsn == "" {
		t.Skip("DATABASE_URL ausente — teste de idempotência DB-gated pulado (roda no CI)")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("falha ao conectar ao Postgres de teste: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatalf("Postgres de teste inacessível: %v", err)
	}

	router := testWebhookRouter(pool)

	const gateway = "pix"
	gatewayRef := "tx-idem-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	orderID, paymentID := seedOrderWithPayment(t, pool, gateway, gatewayRef)

	body := []byte(`{"status":"paid","id":"` + gatewayRef + `"}`)
	sig := signPayment(body, testWebhookSecret)

	// 1ª entrega — confirma.
	code1, _ := doWebhook(t, router, gateway, body, sig)
	if code1 != http.StatusOK {
		t.Fatalf("1ª entrega deveria dar 200, deu %d", code1)
	}

	// 2ª entrega IDÊNTICA — idempotente.
	code2, respBody2 := doWebhook(t, router, gateway, body, sig)
	if code2 != http.StatusOK {
		t.Fatalf("2ª entrega deveria dar 200 idempotente, deu %d", code2)
	}
	if !strings.Contains(respBody2, "idempotente") {
		t.Fatalf("2ª entrega deveria indicar 'idempotente', veio %q", respBody2)
	}

	ctx := context.Background()

	// Pagamento confirmado exatamente uma vez (status 'paid').
	var payStatus string
	if err := pool.QueryRow(ctx,
		`SELECT status FROM sz_order_payments WHERE id = $1`, paymentID,
	).Scan(&payStatus); err != nil {
		t.Fatalf("falha ao reler pagamento: %v", err)
	}
	if payStatus != "paid" {
		t.Fatalf("pagamento deveria estar 'paid', está %q", payStatus)
	}

	// Exatamente UMA transição para 'processing' no histórico (não duplicada).
	var processingCount int64
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM sz_order_status_history
		  WHERE order_id = $1 AND status_para = 'processing'`, orderID,
	).Scan(&processingCount); err != nil {
		t.Fatalf("falha ao contar transições: %v", err)
	}
	if processingCount != 1 {
		t.Fatalf("deveria haver exatamente 1 transição para 'processing', há %d", processingCount)
	}

	// Pedido terminou em 'processing'.
	var orderStatus string
	if err := pool.QueryRow(ctx,
		`SELECT status FROM sz_orders WHERE id = $1`, orderID,
	).Scan(&orderStatus); err != nil {
		t.Fatalf("falha ao reler pedido: %v", err)
	}
	if orderStatus != "processing" {
		t.Fatalf("pedido deveria estar 'processing', está %q", orderStatus)
	}
}
