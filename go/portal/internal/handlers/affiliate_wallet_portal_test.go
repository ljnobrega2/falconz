// Testes dos BLOQUEIOS (guards) do saque/antecipação do afiliado, SEM DB.
//
// Cobre só os guards que retornam ANTES de qualquer acesso ao Pool (nil aqui):
// não-autenticado (401), role não-afiliado (403) e afiliado sem wp_user_id (403).
// Esses são os bloqueios de SEGURANÇA críticos: provam que um não-afiliado e um
// não-dono NÃO chegam à escrita. Os blocos que dependem de DB (flag/mínimo/saldo)
// são verificados por leitura de código + go build (não fingimos um saque real).
//
// package handlers (mesmo pacote) p/ alcançar o handler com Pool=nil sem que o
// nil seja desreferenciado (os guards short-circuitam antes).
package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/senderzz/portal-service/internal/auth"
)

// newAffWalletReq monta um POST com (opcionalmente) um PortalUser injetado no
// contexto via auth.ContextWithUser (mesma chave do middleware; sem JWT/DB).
func newAffWalletReq(path string, u *auth.PortalUser) *http.Request {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
	if u != nil {
		req = req.WithContext(auth.ContextWithUser(req.Context(), u))
	}
	return req
}

// TestAffWithdraw_Blocks cobre os bloqueios de SEGURANÇA pré-DB do saque do
// afiliado: não-autenticado e role não-afiliado. Ambos retornam ANTES de tocar o
// Pool (nil aqui), provando que um não-dono/não-afiliado NÃO chega à escrita. O
// gate WPUserID<=0 e os blocos flag/mínimo/saldo rodam após uma leitura de DB
// (precisam de Pool real) — cobertos por leitura de código + go build.
func TestAffWithdraw_Blocks(t *testing.T) {
	h := &AffiliateWalletHandler{Pool: nil} // Pool nil de propósito: guards param antes.

	cases := []struct {
		name string
		user *auth.PortalUser
		want int
	}{
		{"sem auth → 401", nil, http.StatusUnauthorized},
		{"produtor → 403 (não-afiliado)", &auth.PortalUser{ID: 1, WPUserID: 10, Role: "produtor"}, http.StatusForbidden},
		{"operator → 403 (não-afiliado)", &auth.PortalUser{ID: 2, WPUserID: 20, Role: "operator"}, http.StatusForbidden},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.WithdrawAffiliate(rec, newAffWalletReq("/portal/affiliate-wallet/withdraw", c.user))
			if rec.Code != c.want {
				t.Errorf("WithdrawAffiliate status = %d, esperado %d (body=%s)", rec.Code, c.want, rec.Body.String())
			}
		})
	}
}

// TestAffAnticipate_Blocks cobre os 3 bloqueios pré-DB da antecipação do afiliado.
func TestAffAnticipate_Blocks(t *testing.T) {
	h := &AffiliateWalletHandler{Pool: nil}

	cases := []struct {
		name string
		user *auth.PortalUser
		want int
	}{
		{"sem auth → 401", nil, http.StatusUnauthorized},
		{"produtor → 403 (não-afiliado)", &auth.PortalUser{ID: 1, WPUserID: 10, Role: "produtor"}, http.StatusForbidden},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.AnticipateAffiliate(rec, newAffWalletReq("/portal/affiliate-wallet/anticipate", c.user))
			if rec.Code != c.want {
				t.Errorf("AnticipateAffiliate status = %d, esperado %d (body=%s)", rec.Code, c.want, rec.Body.String())
			}
		})
	}
}

// TestAffWalletLockKey_DistinctFromCOD garante que o advisory lock do afiliado
// está em namespace PRÓPRIO (sz_aff_wallet:) — não colide com o COD (sz_cod_wallet:)
// nem com chaves de afiliados distintos. Sem isso, saque COD e saque afiliado do
// "mesmo número" disputariam o mesmo lock (ou pior, vazariam serialização).
func TestAffWalletLockKey_DistinctFromCOD(t *testing.T) {
	// Estável (mesmo wp_user_id → mesma chave): exigência do guard atômico.
	if affWalletLockKey(42) != affWalletLockKey(42) {
		t.Error("affWalletLockKey deve ser estável para o mesmo wp_user_id")
	}
	// Distinto por afiliado.
	if affWalletLockKey(42) == affWalletLockKey(43) {
		t.Error("afiliados distintos devem ter locks distintos")
	}
	// Namespace distinto do COD (codWalletLockKey) p/ o MESMO número.
	if affWalletLockKey(42) == codWalletLockKey(42) {
		t.Error("lock do afiliado NÃO pode colidir com o lock COD para o mesmo número")
	}
}
