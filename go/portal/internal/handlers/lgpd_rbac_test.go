// Testes dos 3 fixes da auditoria do go/portal (sem Postgres):
//
//	P0 LGPD — canal público do titular (DataRequest): valida input ANTES do DB.
//	P1 LGPD — revogação de consentimento (AccountConsentRevoke): auth + input.
//	P1 RBAC — governança de afiliado exclusiva do produtor: afiliado E operator
//	          recebem 403 ANTES de tocar o Pool (gate por role, da sessão).
//
// Estratégia hermética (mesma de portal_handlers_test.go): construímos o handler
// com Pool=nil e exercitamos só os caminhos que respondem antes de qualquer query
// (401 sem auth, 400 input ruim, 403 role errado, 429 rate-limit). O caminho 200
// (que toca o banco) fica para a prova de integração via psql (fora do teste Go).
package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/senderzz/portal-service/internal/auth"
)

// authReq monta um POST /x com corpo JSON E um PortalUser de role dado no
// contexto (simula o pós-AuthPortalJWT). Pool=nil é seguro nos caminhos de gate.
func authReq(role, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	u := &auth.PortalUser{ID: 10, WPUserID: 20, Email: "p@x.com", Role: role}
	return req.WithContext(auth.ContextWithUser(context.Background(), u))
}

// ── P1 RBAC — governança de afiliado é EXCLUSIVA do produtor ──────────────────

// affiliateGovernanceHandlers — os handlers de GOVERNANÇA do programa de afiliados
// (escrita). Todos devem barrar afiliado E operator (OL) com 403, e deixar o
// produtor passar para a próxima etapa (que aqui falha de outra forma, NÃO 403).
func affiliateGovernanceHandlers(h *AffiliatesHandler) map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"DefaultCommission":    h.DefaultCommission,
		"AutoApprove":          h.AutoApprove,
		"PerAffiliateOverride": h.PerAffiliateOverride,
		"Approve":              h.Approve,
		"Reject":               h.Reject,
		"Commission":           h.Commission,
		"Delete":               h.Delete,
		"CreateInvite":         h.CreateInvite,
		"RevokeInvite":         h.RevokeInvite,
	}
}

// TestAffiliateGovernanceBlocksOperatorAndAffiliate: o OPERATOR (OL) e o AFILIADO
// recebem 403 em TODA escrita de governança — antes de tocar o Pool.
// SEC-RBAC-AFF-GOVERNANCE: o bug era o operator passar (gate só barrava afiliado).
func TestAffiliateGovernanceBlocksOperatorAndAffiliate(t *testing.T) {
	h := &AffiliatesHandler{Pool: nil} // nil seguro: o 403 ocorre antes de qualquer query.
	for _, role := range []string{"operator", "afiliado", "affiliate", "afiliada", "operador"} {
		for name, fn := range affiliateGovernanceHandlers(h) {
			rec := httptest.NewRecorder()
			fn(rec, authReq(role, `{"commission_pct":10}`))
			if rec.Code != http.StatusForbidden {
				t.Errorf("%s(role=%s) = %d, esperado 403 (governança exclusiva do produtor)",
					name, role, rec.Code)
			}
			if !strings.Contains(rec.Body.String(), "Sem permissão") {
				t.Errorf("%s(role=%s) body = %q, esperado 'Sem permissão.'",
					name, role, rec.Body.String())
			}
		}
	}
}

// TestAffiliateGovernanceUnauthenticated: sem contexto de auth → 401 (não 403).
func TestAffiliateGovernanceUnauthenticated(t *testing.T) {
	h := &AffiliatesHandler{Pool: nil}
	for name, fn := range affiliateGovernanceHandlers(h) {
		rec := doReq(fn, `{}`)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s(sem auth) = %d, esperado 401", name, rec.Code)
		}
	}
}

// TestAffiliateGovernanceProdutorPassesGate: o PRODUTOR PASSA do gate de role —
// o status resultante NÃO é 403/401 (cai em 400 input ruim, ou pânico de Pool nil
// recuperado, mas nunca o "Sem permissão" do gate). Garante que o fix não quebrou
// o produtor. Usamos os handlers que validam input ANTES do DB (parse do {id} ou
// do body), para checar que o gate de role já foi ultrapassado.
func TestAffiliateGovernanceProdutorPassesGate(t *testing.T) {
	h := &AffiliatesHandler{Pool: nil}
	// Approve/Reject/Commission/Delete validam o {id} da rota DEPOIS do gate de
	// role. Sem {id} na rota chi, parseAffiliateID falha → 400 "ID inválido."
	// (prova: passou do gate de role; NÃO é 403).
	idHandlers := map[string]http.HandlerFunc{
		"Approve":    h.Approve,
		"Reject":     h.Reject,
		"Commission": h.Commission,
		"Delete":     h.Delete,
	}
	// AMBAS as grafias de produtor passam o gate — isProdutorRole aceita
	// "produtor" E "producer" (motoboy_portal.go). Sem a grafia "producer" aqui,
	// uma regressão que só reconhecesse "produtor" passaria despercebida (o
	// produtor-EN levaria 403 indevido). // SEC-RBAC-AFF-GOVERNANCE
	for _, role := range []string{"produtor", "producer"} {
		for name, fn := range idHandlers {
			rec := httptest.NewRecorder()
			fn(rec, authReq(role, `{"commission_pct":10}`))
			if rec.Code == http.StatusForbidden {
				t.Errorf("%s(role=%s) = 403 — o produtor NÃO pode ser barrado pelo gate de role", name, role)
			}
			if rec.Code == http.StatusUnauthorized {
				t.Errorf("%s(role=%s) = 401 — produtor autenticado não pode ser não-autenticado", name, role)
			}
		}
	}
}

// ── P1 LGPD — revogação de consentimento ──────────────────────────────────────

// TestConsentRevokeUnauthenticated: sem auth → 401 (não revoga nada).
func TestConsentRevokeUnauthenticated(t *testing.T) {
	h := &SettingsHandler{Pool: nil}
	rec := doReq(h.AccountConsentRevoke, `{"doc_type":"privacy","doc_version":"v1"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("AccountConsentRevoke(sem auth) = %d, esperado 401", rec.Code)
	}
}

// TestConsentRevokeBadInput: corpo ruim / campos vazios → 400 (antes do DB).
func TestConsentRevokeBadInput(t *testing.T) {
	h := &SettingsHandler{Pool: nil}
	cases := []string{
		`{ not json`,
		`{}`,
		`{"doc_type":"","doc_version":""}`,
		`{"doc_type":"privacy","doc_version":""}`,
		`{"doc_type":"","doc_version":"v1"}`,
	}
	for _, body := range cases {
		rec := httptest.NewRecorder()
		// Autenticado como produtor: o 400 vem da validação de input, não do gate.
		h.AccountConsentRevoke(rec, authReq("produtor", body))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("AccountConsentRevoke(%s) = %d, esperado 400", body, rec.Code)
		}
	}
}

// ── P0 LGPD — canal público do titular ────────────────────────────────────────

// TestDataRequestBadInput: e-mail inválido OU tipo fora da whitelist → 400.
// Roda ANTES de qualquer query (Pool=nil é seguro).
func TestDataRequestBadInput(t *testing.T) {
	h := &DataRequestHandler{Pool: nil}
	cases := []string{
		`{ not json`,                                            // JSON ruim
		`{"email":"","request_type":"acesso"}`,                  // e-mail vazio
		`{"email":"semarroba","request_type":"acesso"}`,         // e-mail sem @
		`{"email":"a@b.com","request_type":""}`,                 // tipo vazio
		`{"email":"a@b.com","request_type":"hackear"}`,          // tipo fora da whitelist
		`{"email":"a@b.com","request_type":"DROP TABLE"}`,       // injeção óbvia → 400
		`{"email":"a@b.com","request_type":"acesso; truncate"}`, // não casa whitelist
	}
	for _, body := range cases {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		h.DataRequest(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("DataRequest(%s) = %d, esperado 400", body, rec.Code)
		}
	}
}

// TestDSRAllowedTypesMatchSchema: a whitelist Go casa EXATAMENTE os 7 tipos do
// CHECK chk_dsr_request_type (380-lgpd-completo.sql). Se o schema mudar e o Go
// não, este teste falha — evita whitelist e CHECK divergirem silenciosamente.
func TestDSRAllowedTypesMatchSchema(t *testing.T) {
	want := []string{
		"acesso", "correcao", "exclusao", "portabilidade",
		"revogacao_consentimento", "oposicao", "info_compartilhamento",
	}
	if len(dsrAllowedTypes) != len(want) {
		t.Fatalf("dsrAllowedTypes tem %d tipos, esperado %d", len(dsrAllowedTypes), len(want))
	}
	for _, ty := range want {
		if !dsrAllowedTypes[ty] {
			t.Errorf("dsrAllowedTypes faltando %q (deve casar o CHECK do schema)", ty)
		}
	}
}

// TestDSRRateLimiter: estoura o teto de IP e confirma o bloqueio na janela.
func TestDSRRateLimiter(t *testing.T) {
	rl := newDSRRateLimiter()
	rl.ipMax = 3 // teto pequeno p/ o teste (isola do default folgado)
	ip := "203.0.113.99"
	for i := 1; i <= 3; i++ {
		if !rl.allow(ip) {
			t.Fatalf("tentativa %d deveria ser permitida (teto=3)", i)
		}
	}
	if rl.allow(ip) {
		t.Error("4ª tentativa deveria ser bloqueada (passou do teto)")
	}
	// Outro IP não é afetado pelo bloqueio do primeiro.
	if !rl.allow("198.51.100.1") {
		t.Error("IP distinto não deveria estar bloqueado")
	}
	// IP vazio nunca bloqueia (degradação graciosa).
	if !rl.allow("") {
		t.Error("IP vazio deveria sempre permitir")
	}
}
