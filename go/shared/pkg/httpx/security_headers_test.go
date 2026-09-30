// AUDIT GO-HDR-01 — testes do middleware de cabeçalhos de segurança.
package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestSecurityHeaders_DefineTodos garante que o middleware seta o conjunto
// completo de cabeçalhos esperados em uma resposta normal.
func TestSecurityHeaders_DefineTodos(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	h := SecurityHeaders()(next)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/qualquer", nil)
	h.ServeHTTP(rec, req)

	esperado := map[string]string{
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":           "DENY",
		"Referrer-Policy":           "no-referrer",
		"Permissions-Policy":        "geolocation=(), microphone=(), camera=()",
		"Content-Security-Policy":   cspAPIDefault,
		"Strict-Transport-Security": hstsDefault,
	}
	for k, v := range esperado {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("header %q: esperado=%q obtido=%q", k, v, got)
		}
	}
}

// TestSecurityHeaders_PreflightOptions confirma que mesmo um preflight OPTIONS
// (que tipicamente curto-circuita antes do handler de negócio) recebe os headers,
// pois o middleware seta tudo ANTES de chamar o próximo na cadeia.
func TestSecurityHeaders_PreflightOptions(t *testing.T) {
	// next responde 204 direto, como faz um middleware de CORS no preflight.
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	h := SecurityHeaders()(next)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/qualquer", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status esperado=204 obtido=%d", rec.Code)
	}
	if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("preflight sem X-Frame-Options: obtido=%q", got)
	}
	if got := rec.Header().Get("Content-Security-Policy"); got != cspAPIDefault {
		t.Errorf("preflight sem CSP: obtido=%q", got)
	}
}
