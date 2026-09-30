// Testes de handlers HTTP do Portal V2 — caminhos que NÃO tocam o Postgres.
//
// AUDIT TEST-PORTAL-HANDLERS-MOSTLY-UNTESTED (HIGH): os 30+ handlers do portal
// (auth, wallet, sessions, etc.) não tinham nenhum teste de handler. Uma mudança
// acidental que transformasse um 401/400 em 200/501-vazio passaria despercebida.
//
// Estratégia hermética (sem DB, sem deps novas): TODO handler protegido faz
// auth.FromContext(ctx) e responde 401 "não autenticado" ANTES de qualquer acesso
// ao Pool. Os handlers de login validam JSON/campos ANTES do QueryRow. Portanto,
// construindo o handler com Pool=nil e batendo com httptest, exercitamos:
//   - auth:     Login/Login2FA → 400 (JSON ruim / campos vazios)
//   - protegido (auth/wallet/sessions): sem contexto → 401
//   - LGPD:     DeleteAccount → 400 (JSON ruim / senha vazia)
//
// O caminho 200 e os 501 (que exigem usuário no contexto + DB) ficam para o job
// de integração com Postgres (fora deste arquivo, sem deps novas aqui).
package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// doReq monta um *http.Request POST com o corpo JSON dado e roda o handler,
// devolvendo o ResponseRecorder. Sem contexto de auth (usuário não autenticado).
func doReq(h http.HandlerFunc, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

// ── auth: Login ────────────────────────────────────────────────────────────────

// TestLoginBadJSON: corpo não-JSON → 400 (antes de qualquer acesso ao banco).
func TestLoginBadJSON(t *testing.T) {
	h := &AuthHandler{Pool: nil} // nil é seguro: o 400 ocorre antes de tocar o Pool.
	rec := doReq(h.Login, "{ not json")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Login(bad json) = %d, esperado 400", rec.Code)
	}
}

// TestLoginEmptyFields: e-mail/senha vazios → 400 (validação de input).
func TestLoginEmptyFields(t *testing.T) {
	h := &AuthHandler{Pool: nil}
	cases := []string{
		`{}`,
		`{"email":"","senha":""}`,
		`{"email":"a@b.com","senha":""}`,
		`{"email":"","senha":"x"}`,
	}
	for _, body := range cases {
		rec := doReq(h.Login, body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("Login(%s) = %d, esperado 400", body, rec.Code)
		}
	}
}

// ── auth: Login2FA ─────────────────────────────────────────────────────────────

// TestLogin2FABadJSON: corpo inválido → 400.
func TestLogin2FABadJSON(t *testing.T) {
	h := &AuthHandler{Pool: nil}
	rec := doReq(h.Login2FA, "::::")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Login2FA(bad json) = %d, esperado 400", rec.Code)
	}
}

// TestLogin2FAEmptyFields: partial_token/codigo vazios → 400.
func TestLogin2FAEmptyFields(t *testing.T) {
	h := &AuthHandler{Pool: nil}
	cases := []string{
		`{}`,
		`{"partial_token":"","codigo":""}`,
		`{"partial_token":"x","codigo":""}`,
		`{"partial_token":"","codigo":"123456"}`,
	}
	for _, body := range cases {
		rec := doReq(h.Login2FA, body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("Login2FA(%s) = %d, esperado 400", body, rec.Code)
		}
	}
}

// ── auth: rotas protegidas sem contexto → 401 ─────────────────────────────────

// TestProtectedAuthHandlersUnauthenticated cobre os handlers de auth que exigem
// PortalUser no contexto. Sem o middleware AuthPortalJWT, FromContext devolve nil
// e o handler responde 401 antes de tocar o Pool.
func TestProtectedAuthHandlersUnauthenticated(t *testing.T) {
	h := &AuthHandler{Pool: nil}
	handlers := map[string]http.HandlerFunc{
		"Logout":  h.Logout,
		"Refresh": h.Refresh,
		"Me":      h.Me,
	}
	for name, fn := range handlers {
		rec := doReq(fn, `{}`)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s(sem auth) = %d, esperado 401", name, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "não autenticado") {
			t.Errorf("%s(sem auth) body = %q, esperado conter 'não autenticado'", name, rec.Body.String())
		}
	}
}

// ── wallet: rotas protegidas sem contexto → 401 ───────────────────────────────

// TestWalletHandlersUnauthenticated: os GETs da carteira respondem 401 sem auth,
// antes de qualquer SELECT (proteção de escopo financeiro).
func TestWalletHandlersUnauthenticated(t *testing.T) {
	h := &WalletHandler{Pool: nil}
	handlers := map[string]http.HandlerFunc{
		"Summary": h.Summary,
		"History": h.History,
	}
	for name, fn := range handlers {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		rec := httptest.NewRecorder()
		fn(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Wallet.%s(sem auth) = %d, esperado 401", name, rec.Code)
		}
	}
}

// ── sessions: rotas protegidas sem contexto → 401 ─────────────────────────────

// TestSessionsHandlersUnauthenticated: List/RevokeAll exigem auth → 401 sem ela.
func TestSessionsHandlersUnauthenticated(t *testing.T) {
	h := &SessionsHandler{Pool: nil}

	recList := httptest.NewRecorder()
	h.List(recList, httptest.NewRequest(http.MethodGet, "/x", nil))
	if recList.Code != http.StatusUnauthorized {
		t.Errorf("Sessions.List(sem auth) = %d, esperado 401", recList.Code)
	}

	recRevoke := doReq(h.RevokeAll, `{}`)
	if recRevoke.Code != http.StatusUnauthorized {
		t.Errorf("Sessions.RevokeAll(sem auth) = %d, esperado 401", recRevoke.Code)
	}
}

// ── LGPD: DeleteAccount ────────────────────────────────────────────────────────

// TestDeleteAccountUnauthenticated: sem auth → 401 (não dá nem para tentar excluir).
func TestDeleteAccountUnauthenticated(t *testing.T) {
	h := &SettingsHandler{Pool: nil}
	rec := doReq(h.DeleteAccount, `{"password":"x"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("DeleteAccount(sem auth) = %d, esperado 401", rec.Code)
	}
}

// ── login rate limiter (unidade) ──────────────────────────────────────────────

// TestLoginRateLimiterDesligadoPorIP: o login não bloqueia por IP por decisão
// operacional; o rate limit específico de recuperação de senha continua ativo.
func TestLoginRateLimiterPorIP(t *testing.T) {
	rl := &loginRateLimiter{
		byIP:     map[string]*loginRLEntry{},
		byEmail:  map[string]*loginRLEntry{},
		ipMax:    3,
		emailMax: 0, // desativa o eixo e-mail para isolar o eixo IP
		window:   loginRLWindow,
	}
	ip := "203.0.113.7"
	for i := 1; i <= 20; i++ {
		if !rl.allow(ip, "qualquer@x.com") {
			t.Fatalf("tentativa %d deveria ser permitida: login rate limit desligado", i)
		}
	}
}

// TestLoginRateLimiterDesligadoPorEmail: o login não bloqueia por e-mail por
// decisão operacional, mesmo com os campos do limitador configurados.
func TestLoginRateLimiterPorEmail(t *testing.T) {
	rl := &loginRateLimiter{
		byIP:     map[string]*loginRLEntry{},
		byEmail:  map[string]*loginRLEntry{},
		ipMax:    0, // desativa o eixo IP para isolar o eixo e-mail
		emailMax: 2,
		window:   loginRLWindow,
	}
	email := "Vitima@Falkz.test" // normalização para minúsculas é testada de quebra
	for i := 0; i < 20; i++ {
		if !rl.allow("1.1.1.1", email) {
			t.Fatalf("tentativa %d deveria ser permitida: login rate limit desligado", i+1)
		}
	}
}

// TestLoginRateLimiterDisabled: teto <= 0 nunca bloqueia (modo dev/desligado).
func TestLoginRateLimiterDisabled(t *testing.T) {
	rl := &loginRateLimiter{
		byIP:     map[string]*loginRLEntry{},
		byEmail:  map[string]*loginRLEntry{},
		ipMax:    0,
		emailMax: 0,
		window:   loginRLWindow,
	}
	for i := 0; i < 100; i++ {
		if !rl.allow("9.9.9.9", "x@x.com") {
			t.Fatalf("com tetos 0 nenhuma tentativa deveria bloquear (i=%d)", i)
		}
	}
}

// ── SEC-RBAC: auditoria de níveis de acesso (RBAC) + anti-IDOR (2026-06-18) ──────

// TestIsAffiliateRole cobre o discriminador de papel afiliado (motoboy_portal.go),
// usado por TODOS os gates de papel do portal (afiliado NÃO aprova/edita/exclui
// afiliado, não cria checkout, não vê expedição etc.). É a função PURA central de
// autorização — se ela classificar errado, um gate inteiro abre ou fecha indevidamente.
// // SEC-RBAC
func TestIsAffiliateRole(t *testing.T) {
	// Aceitos como afiliado (todas as grafias usadas no banco/PHP).
	for _, role := range []string{"afiliado", "affiliate", "afiliada"} {
		if !isAffiliateRole(role) {
			t.Errorf("isAffiliateRole(%q) = false, esperado true (é afiliado)", role)
		}
	}
	// NÃO-afiliados: produtor/operator/admin e variações não devem casar
	// (senão um produtor levaria 403 ou um afiliado escaparia do gate).
	for _, role := range []string{
		"produtor", "operator", "admin", "",
		"Afiliado",   // case-sensitive: o banco grava minúsculas; maiúsculo não casa
		"afiliados",  // plural não é uma role válida
		" afiliado ", // sem trim — o contexto traz a role já normalizada
	} {
		if isAffiliateRole(role) {
			t.Errorf("isAffiliateRole(%q) = true, esperado false (não é afiliado)", role)
		}
	}
}

// ── Notif (sz-notif): IDOR fechado — exigem sessão (401 sem auth) ───────────────
//
// ANTES estas rotas eram públicas e liam user_id do body/query (IDOR: assinar/ler/
// sobrescrever push de outro usuário). Agora derivam o dono da sessão. Sem contexto
// de auth, TODAS respondem 401 ANTES de tocar o Pool (por isso Pool=nil é seguro) —
// prova que nenhum user_id de cliente consegue agir sem sessão.

// doGet monta um GET (p/ GetPrefs) sem contexto de auth.
func doGet(h http.HandlerFunc, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

// TestNotifSubscribeRequiresAuth: subscribe sem sessão → 401 (não cria inscrição
// para nenhum user_id; fecha o IDOR de "assinar pelo outro"). // SEC-RBAC
func TestNotifSubscribeRequiresAuth(t *testing.T) {
	h := &NotifHandler{Pool: nil} // nil seguro: 401 ocorre antes de tocar o Pool.
	rec := doReq(h.Subscribe, `{"endpoint":"https://x","keys":{"p256dh":"a","auth":"b"},"user_id":999}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("Subscribe(sem auth) = %d, esperado 401", rec.Code)
	}
}

// TestNotifUnsubscribeRequiresAuth: unsubscribe sem sessão → 401. // SEC-RBAC
func TestNotifUnsubscribeRequiresAuth(t *testing.T) {
	h := &NotifHandler{Pool: nil}
	rec := doReq(h.Unsubscribe, `{"endpoint":"https://x"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("Unsubscribe(sem auth) = %d, esperado 401", rec.Code)
	}
}

// TestNotifGetPrefsRequiresAuth: prefs (GET) sem sessão → 401, mesmo passando
// ?user_id= (o param é ignorado; fecha o vazamento de prefs alheias). // SEC-RBAC
func TestNotifGetPrefsRequiresAuth(t *testing.T) {
	h := &NotifHandler{Pool: nil}
	rec := doGet(h.GetPrefs, "/prefs?user_id=999")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GetPrefs(sem auth) = %d, esperado 401", rec.Code)
	}
}

// TestNotifSavePrefsRequiresAuth: prefs (POST) sem sessão → 401, mesmo com
// user_id no body (o IDOR de escrita está fechado). // SEC-RBAC
func TestNotifSavePrefsRequiresAuth(t *testing.T) {
	h := &NotifHandler{Pool: nil}
	rec := doReq(h.SavePrefs, `{"user_id":999,"prefs":{"pedido_feito":{"enabled":0}}}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("SavePrefs(sem auth) = %d, esperado 401", rec.Code)
	}
}

// ── FEAT-PORTAL-SALES: criação de produto / oferta + dashboard de comissões ─────
//
// Mesmo princípio hermético: TODO handler protegido faz auth.FromContext ANTES de
// tocar o Pool. Com Pool=nil e sem contexto de sessão, todos respondem 401. Isso
// trava a regressão "um POST de criação vira 200/501 sem auth" — o loop de venda
// só existe para usuário autenticado e com o papel certo (o gate de role roda DEPOIS
// do 401, então o caminho 403 fica para o job de integração com Postgres).

// TestProductsCreateRequiresAuth: criar produto sem sessão → 401.
func TestProductsCreateRequiresAuth(t *testing.T) {
	h := &ProductsHandler{Pool: nil}
	rec := doReq(h.Create, `{"nome":"Produto X"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("ProductsCreate(sem auth) = %d, esperado 401", rec.Code)
	}
}

// TestProductsUpdateRequiresAuth: editar produto sem sessão → 401.
func TestProductsUpdateRequiresAuth(t *testing.T) {
	h := &ProductsHandler{Pool: nil}
	rec := doReq(h.Update, `{"nome":"Produto X"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("ProductsUpdate(sem auth) = %d, esperado 401", rec.Code)
	}
}

// TestProductsDeleteRequiresAuth: excluir produto sem sessão → 401.
func TestProductsDeleteRequiresAuth(t *testing.T) {
	h := &ProductsHandler{Pool: nil}
	rec := doReq(h.Delete, `{}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("ProductsDelete(sem auth) = %d, esperado 401", rec.Code)
	}
}

// TestLinksCreateRequiresAuth: gerar oferta/checkout sem sessão → 401.
func TestLinksCreateRequiresAuth(t *testing.T) {
	h := &LinksHandler{Pool: nil}
	rec := doReq(h.Create, `{"product_id":1,"name":"3 Potes","price":123.0}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("LinksCreate(sem auth) = %d, esperado 401", rec.Code)
	}
}

// TestLinksCommissionUpdateRequiresAuth: definir comissão da oferta sem sessão → 401.
// (Antes era 501 incondicional; agora persiste de verdade — o 401 deve vir primeiro.)
func TestLinksCommissionUpdateRequiresAuth(t *testing.T) {
	h := &LinksHandler{Pool: nil}
	rec := doReq(h.CommissionUpdate, `{"commission_pct":10}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("LinksCommissionUpdate(sem auth) = %d, esperado 401", rec.Code)
	}
}

// TestAffiliateDashboardRequiresAuth: dashboard de comissões sem sessão → 401.
func TestAffiliateDashboardRequiresAuth(t *testing.T) {
	h := &AffiliateDashboardHandler{Pool: nil}
	rec := doGet(h.Dashboard, "/portal/affiliates/dashboard")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("AffiliateDashboard(sem auth) = %d, esperado 401", rec.Code)
	}
}

// TestBrlLabel: o price_label é fiel ao formato pt-BR migrado ("R$ 1.234,56").
// Pura lógica — sem auth/DB. Trava a regressão de formatação de moeda.
func TestBrlLabel(t *testing.T) {
	cases := map[float64]string{
		0:       "R$ 0,00",
		1:       "R$ 1,00",
		123:     "R$ 123,00",
		123.4:   "R$ 123,40",
		1234.56: "R$ 1.234,56",
		1000000: "R$ 1.000.000,00",
		99.999:  "R$ 100,00", // arredonda p/ cima nos centavos
	}
	for in, want := range cases {
		if got := brlLabel(in); got != want {
			t.Errorf("brlLabel(%v) = %q, esperado %q", in, got, want)
		}
	}
}
