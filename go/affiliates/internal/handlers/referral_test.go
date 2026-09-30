// Testes do código de indicação permanente (referral). FEAT-RBAC-2026-06-21.
//
// Cobre, sem DB (Pool=nil seguro nos caminhos exercitados):
//   - isHexCode: validação de formato do código (pino de regressão);
//   - ResolveReferral PÚBLICO: código malformado → 404 ANTES de tocar o banco;
//   - Referral AUTENTICADO: sem JWT → 401 (middleware fail-closed);
//   - roteamento chi: /affiliates/referral/{code} resolve ao handler PÚBLICO (sem
//     JWT) e /affiliates/referral fica ATRÁS do JWT — o teste discriminante pedido
//     na revisão (público vs autenticado pelo MESMO router montado como em produção).
//
// Comentários em PT-BR conforme convenção do projeto.
package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/senderzz/affiliates-service/internal/auth"
)

// TestIsHexCode trava o formato do referral_code: hex de 8 a 16 chars. Os códigos
// REAIS têm 8 (md5 upper, trigger 434); a regra anterior (exatamente 16) rejeitava
// TODO código real — este pino impede a regressão.
func TestIsHexCode(t *testing.T) {
	validos := []string{
		"ABCDEF12",          // 8 chars (formato real, md5 upper do trigger 434)
		"0123abcd",          // 8 chars minúsculo
		"0123456789abcde",   // 15 chars
		"0123456789abcdef",  // 16 chars (legado randomHex(8))
		"ABCDEF0123456789",  // 16 chars maiúsculo
		"aaaaaaaaaaaaaaaa",  // 16 chars
	}
	for _, c := range validos {
		if !isHexCode(c) {
			t.Errorf("isHexCode(%q) = false, esperado true", c)
		}
	}

	invalidos := []string{
		"",                  // vazio
		"abc",               // 3 chars — curto demais (< 8)
		"0123456",           // 7 chars — 1 abaixo do mínimo
		"0123456789abcdeff", // 17 chars — acima do máximo
		"0123456g",          // 'g' não é hex
		"zzzzzzzz",          // fora do alfabeto hex
		"0123 567",          // espaço
	}
	for _, c := range invalidos {
		if isHexCode(c) {
			t.Errorf("isHexCode(%q) = true, esperado false", c)
		}
	}
}

// TestResolveReferralCodigoMalformado: código fora do formato → 404 sem tocar o
// banco. Pool=nil prova que a validação retorna ANTES de qualquer query.
func TestResolveReferralCodigoMalformado(t *testing.T) {
	h := &AffiliatesHandler{Pool: nil}

	for _, code := range []string{"abc", "naohex0000000000", "0123456789abcdeff", ""} {
		t.Run(code, func(t *testing.T) {
			r := chi.NewRouter()
			r.Get("/affiliates/referral/{code}", h.ResolveReferral)

			req := httptest.NewRequest(http.MethodGet, "/affiliates/referral/"+code, nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			// code vazio cai no 404 do chi (rota não casa /affiliates/referral/);
			// demais caem no 404 do handler. Em ambos o resultado p/ o cliente é 404.
			if rec.Code != http.StatusNotFound {
				t.Fatalf("code=%q: status = %d, esperado 404; body=%s", code, rec.Code, rec.Body.String())
			}
		})
	}
}

// TestReferralSemTokenRejeita: GET /affiliates/referral sem JWT → 401 (middleware
// fail-closed). O handler nunca roda sem usuário autenticado.
func TestReferralSemTokenRejeita(t *testing.T) {
	t.Setenv("JWT_SECRET", "segredo-de-teste-hs256")
	h := &AffiliatesHandler{Pool: nil}

	req := httptest.NewRequest(http.MethodGet, "/affiliates/referral", nil)
	rec := httptest.NewRecorder()
	auth.AuthPortalJWT(http.HandlerFunc(h.Referral)).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("referral sem token: status = %d, esperado 401; body=%s", rec.Code, rec.Body.String())
	}
}

// TestRoteamentoPublicoVsAutenticado é o teste DISCRIMINANTE: monta um router
// idêntico em estrutura ao de produção (grupo autenticado + grupo público irmão)
// e prova que:
//   - /affiliates/referral/{code} passa SEM JWT (atinge ResolveReferral → 404 p/
//     código malformado, NÃO 401/503);
//   - /affiliates/referral        exige JWT (sem token → 401 do middleware, nunca
//     chega ao handler).
func TestRoteamentoPublicoVsAutenticado(t *testing.T) {
	t.Setenv("JWT_SECRET", "segredo-de-teste-hs256")
	h := &AffiliatesHandler{Pool: nil}

	r := chi.NewRouter()
	r.Route("/wp-json/senderzz/v1", func(r chi.Router) {
		// Grupo autenticado (espelha main.go).
		r.Group(func(r chi.Router) {
			r.Use(auth.AuthPortalJWT)
			r.Get("/affiliates/referral", h.Referral)
		})
		// Grupo público irmão (sem JWT) — sob /affiliates/ p/ casar o gateway FALK.
		r.Group(func(r chi.Router) {
			r.Get("/affiliates/referral/{code}", h.ResolveReferral)
		})
	})

	// 1) Público: sem token, código malformado → 404 (chegou ao handler público,
	//    NÃO foi barrado por auth). Um 401/503 aqui significaria que o middleware
	//    de auth vazou para a rota pública — regressão grave. O body confirma que
	//    foi o NOSSO handler que respondeu (e não o 404 "rota não casa" do chi).
	{
		req := httptest.NewRequest(http.MethodGet, "/wp-json/senderzz/v1/affiliates/referral/naohex0000000000", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("rota pública /affiliates/referral/{code}: status = %d, esperado 404 (sem auth); body=%s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "indicação não encontrada") {
			t.Fatalf("rota pública: body não veio do handler (esperava 'indicação não encontrada'); body=%s", rec.Body.String())
		}
	}

	// 2) Autenticado: sem token → 401 (middleware barra antes do handler).
	{
		req := httptest.NewRequest(http.MethodGet, "/wp-json/senderzz/v1/affiliates/referral", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("rota autenticada /affiliates/referral: status = %d, esperado 401; body=%s", rec.Code, rec.Body.String())
		}
	}
}
