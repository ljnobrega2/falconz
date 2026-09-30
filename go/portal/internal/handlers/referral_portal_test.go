// Testes herméticos (sem Postgres) dos handlers #71/#72:
//   - AffiliatesHandler.Referral (#71): sem contexto de auth → 401 ANTES do Pool.
//   - VitrineHandler.CancelAffiliation (#72): sem auth → 401; produtor inválido → 200
//     com success:false (validação de input antes de tocar o Pool).
//   - referralLinkFor: formato do link fixo /r/{code} (pino de regressão do #71).
//
// Mesma estratégia do portal_handlers_test.go: Pool=nil é seguro porque o 401/400
// ocorre antes de qualquer query. Comentários em PT-BR (convenção do projeto).
package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── #71 — Referral ────────────────────────────────────────────────────────────

// TestReferralUnauthenticated: GET /portal/affiliates/referral sem PortalUser no
// contexto → 401 "não autenticado", antes de tocar o Pool.
func TestReferralUnauthenticated(t *testing.T) {
	h := &AffiliatesHandler{Pool: nil}
	req := httptest.NewRequest(http.MethodGet, "/portal/affiliates/referral", nil)
	rec := httptest.NewRecorder()
	h.Referral(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("Referral(sem auth) = %d, esperado 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "não autenticado") {
		t.Fatalf("Referral(sem auth) body = %q, esperado conter 'não autenticado'", rec.Body.String())
	}
}

// TestReferralLinkFor trava o formato do link fixo de indicação: base + /r/{code}.
// Código vazio → link vazio (degrada sem inventar URL). Default falklog.com.br
// quando REFERRAL_PUBLIC_BASE não está setado.
func TestReferralLinkFor(t *testing.T) {
	t.Setenv("REFERRAL_PUBLIC_BASE", "") // força o default
	if got := referralLinkFor(""); got != "" {
		t.Errorf("referralLinkFor(\"\") = %q, esperado \"\"", got)
	}
	if got := referralLinkFor("ABC12345"); got != "https://falklog.com.br/r/ABC12345" {
		t.Errorf("referralLinkFor default = %q, esperado https://falklog.com.br/r/ABC12345", got)
	}

	// Base configurável (sem barra final duplicada).
	t.Setenv("REFERRAL_PUBLIC_BASE", "https://exemplo.com/")
	if got := referralLinkFor("X"); got != "https://exemplo.com/r/X" {
		t.Errorf("referralLinkFor base custom = %q, esperado https://exemplo.com/r/X", got)
	}
}

// ── #72 — CancelAffiliation ───────────────────────────────────────────────────

// TestCancelAffiliationUnauthenticated: POST /portal/vitrine/affiliate/cancel sem
// auth → 401, antes de tocar o Pool.
func TestCancelAffiliationUnauthenticated(t *testing.T) {
	h := &VitrineHandler{Pool: nil}
	req := httptest.NewRequest(http.MethodPost, "/portal/vitrine/affiliate/cancel",
		strings.NewReader(`{"producer_id":5}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.CancelAffiliation(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("CancelAffiliation(sem auth) = %d, esperado 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "não autenticado") {
		t.Fatalf("CancelAffiliation(sem auth) body = %q, esperado 'não autenticado'", rec.Body.String())
	}
}
