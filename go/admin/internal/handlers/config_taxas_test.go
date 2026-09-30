// Testes de INTEGRAÇÃO (com DB) da "regra de cálculo" (taxas) — config_taxas.go.
//
// Regra do dono: "mudar a fórmula SÓ via admin". Estes testes travam exatamente
// isso na borda HTTP:
//   - GET/POST SEM auth admin → barrado (401) pelo auth.Middleware.
//   - GET COM auth admin → 200 + taxas atuais.
//   - POST COM auth admin → persiste em senderzz_options; valor original
//     RESTAURADO no cleanup (a taxa governa o cálculo do SITE inteiro).
//   - POST fora do range (0..100) → 400 invalid_range (guarda da fórmula).
//
// PRIMEIRO teste DB-backed do pacote: este arquivo carrega o harness compartilhado
// (testPoolOrSkip / mintAdmin) em handlers_dbtest.go. Sem DATABASE_URL → SKIP
// silencioso (não falha o `go test` em máquina sem banco).
//
// IMPORTANTE — restauração: a taxa de produção (sz_producer_transaction_fee_pct)
// é LIDA pelo order_detail.go / view sz_order_financeiro. O teste captura o valor
// ANTES, altera, e restaura o MESMO valor via t.Cleanup (roda mesmo se o assert
// falhar). Nunca deixa a taxa de produção alterada.
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/senderzz/admin-service/internal/auth"
)

// mountConfigTaxas monta um router chi mínimo espelhando main.go:
// GET/POST /config/taxas atrás do auth.Middleware (a auth vive no MIDDLEWARE,
// não no handler — chamar h.Get direto devolveria 200 sem token).
func mountConfigTaxas(t *testing.T) (*chi.Mux, *ConfigTaxasHandler) {
	t.Helper()
	pool := testPoolOrSkip(t)
	h := &ConfigTaxasHandler{Pool: pool}
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(pool))
		r.Get("/config/taxas", h.Get)
		r.Post("/config/taxas", h.Save)
	})
	return r, h
}

// readProducerFee lê o valor cru atual da option (string), para captura/restauração.
func readProducerFee(t *testing.T, h *ConfigTaxasHandler) (string, bool) {
	t.Helper()
	var raw string
	err := h.Pool.QueryRow(context.Background(),
		`SELECT value FROM senderzz_options WHERE name='sz_producer_transaction_fee_pct'`).Scan(&raw)
	if err != nil {
		return "", false
	}
	return raw, true
}

// optionFloatOrDefault lê uma option como float (via parseRate, mesma leitura do
// handler) com fallback ao default quando a linha não existe / está vazia. Espelha
// h.getOptionFloat sem depender do método para a asserção de igualdade.
func optionFloatOrDefault(t *testing.T, h *ConfigTaxasHandler, key string, def float64) float64 {
	t.Helper()
	var raw string
	err := h.Pool.QueryRow(context.Background(),
		`SELECT value FROM senderzz_options WHERE name=$1`, key).Scan(&raw)
	if err != nil || raw == "" {
		return def
	}
	return parseRate(raw)
}

// ─── GET/POST sem auth → 401 (auth.Middleware só emite Unauthorized) ──────────

func TestConfigTaxas_SemAuth_401(t *testing.T) {
	t.Setenv("ADMIN_JWT_SECRET", "test-admin-secret-config")
	t.Setenv("JWT_SECRET", "test-admin-secret-config") // gate 503 do main.go checa AMBOS
	r, _ := mountConfigTaxas(t)

	// GET sem header Authorization.
	t.Run("GET sem token → 401", func(t *testing.T) {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/config/taxas", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("GET sem auth = %d, esperado 401", rec.Code)
		}
	})

	// POST sem header Authorization — NÃO pode atravessar para o handler de escrita.
	t.Run("POST sem token → 401 (escrita barrada)", func(t *testing.T) {
		body := strings.NewReader(`{"producer_transaction_fee_pct":99}`)
		req := httptest.NewRequest(http.MethodPost, "/config/taxas", body)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("POST sem auth = %d, esperado 401", rec.Code)
		}
	})
}

// ─── GET com admin → 200 + taxas atuais ──────────────────────────────────────

func TestConfigTaxas_GetComAdmin_200(t *testing.T) {
	t.Setenv("ADMIN_JWT_SECRET", "test-admin-secret-config")
	t.Setenv("JWT_SECRET", "test-admin-secret-config")
	r, h := mountConfigTaxas(t)
	token := mintAdmin(t, h.Pool, 990000101, "cfgtaxas-get@test.local")

	req := httptest.NewRequest(http.MethodGet, "/config/taxas", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET com admin = %d (%s), esperado 200", rec.Code, rec.Body.String())
	}
	var got configTaxasResp
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("resposta GET não é configTaxasResp: %v — body=%s", err, rec.Body.String())
	}
	// Contrato (DONO): GET retorna as taxas ATUAIS — sz_producer_transaction_fee_pct
	// e motoboy_repasse_padrao. Asserta IGUALDADE contra o valor cru no banco (com
	// fallback aos defaults 4,99 / 18 quando a option não está persistida), não só
	// faixa: um valor errado-mas-não-negativo passaria num range-check.
	wantProducer := optionFloatOrDefault(t, h, "sz_producer_transaction_fee_pct", 4.99)
	wantMotoboy := optionFloatOrDefault(t, h, "motoboy_repasse_padrao", 18.0)
	if got.ProducerTransactionFeePct != wantProducer {
		t.Errorf("producer_transaction_fee_pct = %v, esperado %v (valor atual do banco)",
			got.ProducerTransactionFeePct, wantProducer)
	}
	if got.MotoboyRepassePadrao != wantMotoboy {
		t.Errorf("motoboy_repasse_padrao = %v, esperado %v (valor atual do banco)",
			got.MotoboyRepassePadrao, wantMotoboy)
	}
	// AUDIT-2026-07-30: campo affiliate_transaction_fee_read_only não existe mais
	// em configTaxasResp — refactor anterior (não commitado até esta sessão)
	// tornou affiliate_transaction_fee_pct editável via senderzz_options
	// (config_taxas.go:73/89), igual producer/motoboy. Teste asserta contra o
	// valor cru do banco, igual os outros dois campos acima — não mais contra a
	// constante hardcoded taxaTransacaoAfiliadoPct (que só é usada pelo cálculo
	// de comissão em orders-service/checkout.go, hoje DESSINCRONIZADA dessa
	// option — ver AUDIT achado novo).
	wantAffiliate := optionFloatOrDefault(t, h, "sz_affiliate_transaction_fee_pct", 4.99)
	if got.AffiliateTransactionFeePct != wantAffiliate {
		t.Errorf("affiliate_transaction_fee_pct = %v, esperado %v (valor atual do banco)",
			got.AffiliateTransactionFeePct, wantAffiliate)
	}
}

// ─── POST com admin muda producer pct → persiste; RESTAURA original ──────────

func TestConfigTaxas_PostComAdmin_PersisteERestaura(t *testing.T) {
	t.Setenv("ADMIN_JWT_SECRET", "test-admin-secret-config")
	t.Setenv("JWT_SECRET", "test-admin-secret-config")
	r, h := mountConfigTaxas(t)
	token := mintAdmin(t, h.Pool, 990000102, "cfgtaxas-post@test.local")

	// 1) Captura o valor ORIGINAL (cru) ANTES de qualquer escrita.
	original, existed := readProducerFee(t, h)

	// 2) RESTAURAÇÃO garantida — roda mesmo se um assert abaixo falhar.
	t.Cleanup(func() {
		ctx := context.Background()
		if existed {
			_, _ = h.Pool.Exec(ctx,
				`UPDATE senderzz_options SET value=$1 WHERE name='sz_producer_transaction_fee_pct'`,
				original)
		} else {
			// não existia antes do teste → remove a linha que o POST criou.
			_, _ = h.Pool.Exec(ctx,
				`DELETE FROM senderzz_options WHERE name='sz_producer_transaction_fee_pct'`)
		}
	})

	// 3) POST muda 4.99 → 7.25 (exemplo do dono).
	body := strings.NewReader(`{"producer_transaction_fee_pct":7.25}`)
	req := httptest.NewRequest(http.MethodPost, "/config/taxas", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST com admin = %d (%s), esperado 200", rec.Code, rec.Body.String())
	}
	// Save() reespelha o GET → resposta deve trazer o novo valor.
	var got configTaxasResp
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("resposta POST não é configTaxasResp: %v — body=%s", err, rec.Body.String())
	}
	if got.ProducerTransactionFeePct != 7.25 {
		t.Errorf("resposta POST producer pct = %v, esperado 7.25", got.ProducerTransactionFeePct)
	}

	// 4) Confirma PERSISTÊNCIA direta no banco (fonte da verdade do site).
	persisted, ok := readProducerFee(t, h)
	if !ok {
		t.Fatalf("option sz_producer_transaction_fee_pct não persistiu no banco")
	}
	if parseRate(persisted) != 7.25 {
		t.Errorf("valor persistido = %q (%.4f), esperado 7.25", persisted, parseRate(persisted))
	}
}

// ─── POST fora do range (0..100) → 400 invalid_range (guarda da fórmula) ──────

func TestConfigTaxas_PostForaDoRange_400(t *testing.T) {
	t.Setenv("ADMIN_JWT_SECRET", "test-admin-secret-config")
	t.Setenv("JWT_SECRET", "test-admin-secret-config")
	r, h := mountConfigTaxas(t)
	token := mintAdmin(t, h.Pool, 990000103, "cfgtaxas-range@test.local")

	// Captura + restaura: defensivo, embora 400 NÃO deva ter escrito nada.
	original, existed := readProducerFee(t, h)
	t.Cleanup(func() {
		ctx := context.Background()
		if existed {
			_, _ = h.Pool.Exec(ctx,
				`UPDATE senderzz_options SET value=$1 WHERE name='sz_producer_transaction_fee_pct'`, original)
		}
	})

	body := strings.NewReader(`{"producer_transaction_fee_pct":150}`) // > 100
	req := httptest.NewRequest(http.MethodPost, "/config/taxas", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST pct=150 = %d, esperado 400 invalid_range", rec.Code)
	}
	// E NÃO pode ter alterado a option (validação roda antes do upsert).
	after, ok := readProducerFee(t, h)
	if ok && existed && after != original {
		t.Errorf("POST inválido alterou a option: era %q, ficou %q", original, after)
	}
}
