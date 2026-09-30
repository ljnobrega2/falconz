package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestPixWebhookRateLimiter valida o porte do rate-limit 30/min por IP
// (webhook.php:60-65): a 31ª requisição na janela retorna 429.
// Testável sem DB porque o limiter roda ANTES da validação de assinatura e do
// acesso ao banco — exatamente a ordem do WP.
func TestPixWebhookRateLimiter(t *testing.T) {
	l := newPixWebhookRateLimiter()

	// 30 requisições do mesmo IP devem passar.
	for i := 0; i < pixWebhookMaxPorMinuto; i++ {
		if !l.allow("1.2.3.4") {
			t.Fatalf("requisição %d/%d foi bloqueada antes do limite", i+1, pixWebhookMaxPorMinuto)
		}
	}
	// A 31ª deve ser bloqueada.
	if l.allow("1.2.3.4") {
		t.Fatal("31ª requisição deveria ser bloqueada (>= 30)")
	}
	// Outro IP não é afetado (chave por IP).
	if !l.allow("5.6.7.8") {
		t.Fatal("IP distinto não deveria estar limitado")
	}
}

// TestPostPixWebhookRateLimited429 valida que o handler responde 429 quando o
// limite por IP é estourado, antes de qualquer acesso ao banco (h.db nil).
func TestPostPixWebhookRateLimited429(t *testing.T) {
	h := &PixHandler{db: nil, rl: newPixWebhookRateLimiter()}

	// Pré-satura a janela do IP.
	for i := 0; i < pixWebhookMaxPorMinuto; i++ {
		h.rl.allow("9.9.9.9")
	}

	req := httptest.NewRequest(http.MethodPost, "/wp-json/tp-carteira/v1/webhook/pix",
		strings.NewReader(`{"status":"paid"}`))
	req.RemoteAddr = "9.9.9.9:55555"
	rec := httptest.NewRecorder()

	h.PostPixWebhook(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("esperado 429, obtido %d (body: %s)", rec.Code, rec.Body.String())
	}
}

// TestClientIP valida a extração de IP (remoção da porta).
func TestClientIP(t *testing.T) {
	cases := map[string]string{
		"1.2.3.4:5678": "1.2.3.4",
		"1.2.3.4":      "1.2.3.4",
		"[::1]:8080":   "::1",
		"":             "unknown",
	}
	for remoteAddr, want := range cases {
		req := httptest.NewRequest(http.MethodPost, "/x", nil)
		req.RemoteAddr = remoteAddr
		if got := clientIP(req); got != want {
			t.Errorf("clientIP(%q) = %q, esperado %q", remoteAddr, got, want)
		}
	}
}
