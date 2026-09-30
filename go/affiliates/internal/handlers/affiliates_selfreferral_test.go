// Teste do guard anti-auto-afiliação (self-referral) de Request().
//
// SEC-AFFILIATES: o vínculo de afiliação NÃO pode ter afiliado_id == produtor_id
// (linha 60 de affiliates.go: "barrar auto-afiliação (produtor_id != afiliado)").
// A guarda vive inline em Request() (affiliates.go:210-213) e retorna 400 ANTES de
// qualquer acesso a h.Pool — por isso este teste exercita o caminho REAL de produção
// com Pool=nil (sem DB), atravessando o middleware AuthPortalJWT verdadeiro com um JWT
// HS256 assinado de teste. Nenhuma linha de produção é alterada para tornar isto testável.
//
// Comentários em PT-BR conforme convenção do projeto.
package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/senderzz/affiliates-service/internal/auth"
)

// jwtTeste assina um JWT HS256 no mesmo formato emitido pelo PHP
// (tpc_jwt_encode): claim "sub"=user_id, "role", "exp" obrigatório.
func jwtTeste(t *testing.T, secret string, sub int64, role string) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":  sub,
		"role": role,
		"exp":  time.Now().Add(time.Hour).Unix(),
	})
	s, err := tok.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("falha ao assinar JWT de teste: %v", err)
	}
	return s
}

// fazRequest monta a requisição POST /affiliates/request com body JSON e
// atravessa o middleware AuthPortalJWT real (que injeta o PortalUser no contexto).
// Pool=nil é seguro: tanto o guard self-referral quanto as validações de body
// retornam ANTES de tocar h.Pool.
func fazRequest(t *testing.T, secret string, sub int64, role string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	h := &AffiliatesHandler{Pool: nil}

	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("falha ao serializar body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/affiliates/request", strings.NewReader(string(payload)))
	req.Header.Set("Authorization", "Bearer "+jwtTeste(t, secret, sub, role))

	rec := httptest.NewRecorder()
	// Encadeia o middleware real -> handler real. Exercita o caminho de produção.
	handler := auth.AuthPortalJWT(http.HandlerFunc(h.Request))
	handler.ServeHTTP(rec, req)
	return rec
}

// TestRequestBarraAutoAfiliacao confirma que afiliado_id == produtor_id é rejeitado
// com 400 — o guard SEC-AFFILIATES de auto-afiliação está coberto pelo caminho real.
func TestRequestBarraAutoAfiliacao(t *testing.T) {
	const secret = "segredo-de-teste-hs256"
	t.Setenv("JWT_SECRET", secret)

	// Usuário 7 (produtor) tenta se afiliar ao PRÓPRIO produto (produtor_id=7).
	rec := fazRequest(t, secret, 7, "produtor", map[string]any{
		"produtor_id": 7,
		"produto_id":  1,
	})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("auto-afiliação (afiliado_id==produtor_id): status = %d, esperado 400; body=%s",
			rec.Code, rec.Body.String())
	}

	var resp struct {
		OK   bool   `json:"ok"`
		Erro string `json:"erro"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resposta não é JSON válido: %v (body=%s)", err, rec.Body.String())
	}
	if resp.OK {
		t.Errorf("auto-afiliação deveria falhar (ok=false), obteve ok=true")
	}
	if !strings.Contains(resp.Erro, "próprio produto") {
		t.Errorf("mensagem de erro inesperada: %q (esperava menção a auto-afiliação)", resp.Erro)
	}
}

// TestRequestValidaCamposObrigatorios garante que produtor_id/produto_id zerados são
// barrados (400) antes de qualquer acesso a DB — borda complementar ao guard.
func TestRequestValidaCamposObrigatorios(t *testing.T) {
	const secret = "segredo-de-teste-hs256"
	t.Setenv("JWT_SECRET", secret)

	cases := []struct {
		name string
		body map[string]any
	}{
		{name: "produtor_id ausente", body: map[string]any{"produto_id": 1}},
		{name: "produto_id ausente", body: map[string]any{"produtor_id": 9}},
		{name: "ambos zerados", body: map[string]any{"produtor_id": 0, "produto_id": 0}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Afiliado 99 != produtor para isolar a validação de campos do guard.
			rec := fazRequest(t, secret, 99, "affiliate", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s: status = %d, esperado 400; body=%s", tc.name, rec.Code, rec.Body.String())
			}
		})
	}
}

// TestRequestSemTokenRejeita confirma que o middleware fail-closed barra requisições
// sem JWT (401) — o handler nunca roda sem usuário autenticado no contexto.
func TestRequestSemTokenRejeita(t *testing.T) {
	t.Setenv("JWT_SECRET", "segredo-de-teste-hs256")
	h := &AffiliatesHandler{Pool: nil}

	req := httptest.NewRequest(http.MethodPost, "/affiliates/request",
		strings.NewReader(`{"produtor_id":7,"produto_id":1}`))
	rec := httptest.NewRecorder()
	auth.AuthPortalJWT(http.HandlerFunc(h.Request)).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("requisição sem token: status = %d, esperado 401; body=%s", rec.Code, rec.Body.String())
	}
}
