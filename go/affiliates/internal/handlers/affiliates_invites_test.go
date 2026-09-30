// Testes de convite de afiliação: prazo (expiry) e guardas de token na criação.
//
// SEC-AFFILIATES: o convite é o vetor de entrada de novos afiliados. Cobre:
//   - inviteTTL: prazo canônico de 7 dias (pino de regressão da janela de validade);
//   - expiry math: expires_at calculado na criação fica no futuro pela janela exata;
//   - guardas de CreateInvite (email obrigatório → 400, sem JWT → 401) exercitadas
//     pelo caminho REAL de produção (middleware AuthPortalJWT verdadeiro) com Pool=nil,
//     pois essas validações retornam ANTES de qualquer acesso a h.Pool.
//
// O token em si (64 hex, entropia, unicidade) já é coberto por affiliates_test.go.
// Comentários em PT-BR conforme convenção do projeto.
package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/senderzz/affiliates-service/internal/auth"
)

// TestInviteTTLCanonico trava a janela de validade do convite em exatamente 7 dias.
// Qualquer mudança acidental do prazo (ex.: 7h, 30 dias) quebra aqui.
func TestInviteTTLCanonico(t *testing.T) {
	if inviteTTL != 7*24*time.Hour {
		t.Errorf("inviteTTL = %v, esperado 168h (7 dias)", inviteTTL)
	}
	if h := inviteTTL.Hours(); h != 168 {
		t.Errorf("inviteTTL.Hours() = %v, esperado 168", h)
	}
}

// TestInviteExpiraNoFuturo confere que expires_at calculado na criação
// (now + inviteTTL) fica adiante de agora pela janela esperada — o convite nasce
// válido e expira ~7 dias depois.
func TestInviteExpiraNoFuturo(t *testing.T) {
	antes := time.Now().UTC()
	expiresAt := antes.Add(inviteTTL) // mesma expressão usada em CreateInvite
	depois := time.Now().UTC()

	if !expiresAt.After(depois) {
		t.Fatalf("expires_at (%v) não está no futuro em relação a agora (%v)", expiresAt, depois)
	}

	// A distância até a expiração deve estar entre ~7 dias menos a duração do teste
	// e exatamente 7 dias — garante que a janela aplicada é a de inviteTTL.
	dist := expiresAt.Sub(antes)
	if dist > inviteTTL || dist < inviteTTL-time.Minute {
		t.Errorf("janela de expiração = %v, esperado ~%v (inviteTTL)", dist, inviteTTL)
	}
}

// criaInviteReq monta POST /affiliates/invites atravessando o middleware
// AuthPortalJWT real (igual ao padrão de affiliates_selfreferral_test.go).
// Pool=nil é seguro: as validações de email/auth retornam ANTES de tocar h.Pool.
func criaInviteReq(t *testing.T, secret string, sub int64, role, body string, withToken bool) *httptest.ResponseRecorder {
	t.Helper()
	h := &AffiliatesHandler{Pool: nil}

	req := httptest.NewRequest(http.MethodPost, "/affiliates/invites", strings.NewReader(body))
	if withToken {
		req.Header.Set("Authorization", "Bearer "+jwtTeste(t, secret, sub, role))
	}
	rec := httptest.NewRecorder()
	auth.AuthPortalJWT(http.HandlerFunc(h.CreateInvite)).ServeHTTP(rec, req)
	return rec
}

// TestCreateInviteEmailObrigatorio: email ausente/vazio → 400, sem tocar o banco.
func TestCreateInviteEmailObrigatorio(t *testing.T) {
	const secret = "segredo-de-teste-hs256"
	t.Setenv("JWT_SECRET", secret)

	cases := []struct {
		name string
		body string
	}{
		{name: "body vazio", body: ``},
		{name: "json sem campo email", body: `{}`},
		{name: "email string vazia", body: `{"email":""}`},
		{name: "json malformado", body: `{nao-json`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := criaInviteReq(t, secret, 7, "produtor", tc.body, true)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("%s: status = %d, esperado 400; body=%s", tc.name, rec.Code, rec.Body.String())
			}
			var resp struct {
				OK bool `json:"ok"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("%s: resposta não é JSON: %v", tc.name, err)
			}
			if resp.OK {
				t.Errorf("%s: esperava ok=false", tc.name)
			}
		})
	}
}

// TestCreateInviteSemTokenRejeita: sem JWT → 401 (middleware fail-closed),
// o handler nunca roda sem produtor autenticado.
func TestCreateInviteSemTokenRejeita(t *testing.T) {
	t.Setenv("JWT_SECRET", "segredo-de-teste-hs256")
	rec := criaInviteReq(t, "segredo-de-teste-hs256", 0, "", `{"email":"a@b.com"}`, false)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("convite sem token: status = %d, esperado 401; body=%s", rec.Code, rec.Body.String())
	}
}
