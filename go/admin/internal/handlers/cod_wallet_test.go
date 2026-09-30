// Testes unitários dos helpers PUROS da fatia "carteira COD" (sem DB, sem rede).
//
// Cobre validação de input e formatação que sustentam saldo/saque do produtor e
// do afiliado:
//   - parseIDParam / parseListLimit  (cod_saques.go) — guarda de path/query
//   - defaultGlobalRules / optionKeys (cod_saques.go) — contrato de regras globais
//   - codProofDir / codProofURL       (cod_saques.go) — caminho/URL de comprovante
//   - strDeref                         (cod_wallet_producer.go) — *string nil-safe
//   - clampCodPage                     (cod_wallet_transactions.go) — paginação
//   - buildCodTxWhere                  (cod_wallet_transactions.go) — WHERE dinâmico
//
// Estilo seguindo affiliates_test.go / helpers_test.go: package handlers (mesmo
// pacote, alcança não-exportados), table-driven, PT-BR. Identificadores de fixture
// ficam locais às funções para não colidir no namespace do pacote de teste.
package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// ─── parseIDParam — chi URLParam "id" como int64 positivo ────────────────────

// reqWithID monta uma request com o chi RouteContext populado ({id}=val), sem
// precisar de um router real. Injeta o rctx via chi.RouteCtxKey.
func reqWithID(val string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/x", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", val)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestParseIDParam(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		wantID int64
		wantOK bool
	}{
		{name: "id positivo simples", in: "5", wantID: 5, wantOK: true},
		{name: "id grande", in: "9007199254740993", wantID: 9007199254740993, wantOK: true},
		{name: "zero rejeitado", in: "0", wantID: 0, wantOK: false},
		{name: "negativo rejeitado", in: "-1", wantID: 0, wantOK: false},
		{name: "vazio rejeitado", in: "", wantID: 0, wantOK: false},
		{name: "não numérico rejeitado", in: "abc", wantID: 0, wantOK: false},
		{name: "decimal rejeitado", in: "5.0", wantID: 0, wantOK: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id, ok := parseIDParam(reqWithID(c.in))
			if ok != c.wantOK || id != c.wantID {
				t.Errorf("parseIDParam(%q) = (%d, %v), esperado (%d, %v)", c.in, id, ok, c.wantID, c.wantOK)
			}
		})
	}
}

// ─── parseListLimit — clamp [1,500], default 120 ─────────────────────────────

func TestParseListLimit(t *testing.T) {
	cases := map[string]int{
		"":      120, // ausente → default
		"0":     120, // <=0 → default
		"-10":   120, // negativo → default
		"abc":   120, // inválido (Atoi falha → 0) → default
		"50":    50,  // dentro do range
		"500":   500, // limite superior exato
		"501":   500, // acima do teto → clamp 500
		"99999": 500, // muito acima → clamp 500
	}
	for raw, want := range cases {
		r := httptest.NewRequest(http.MethodGet, "/x?limit="+raw, nil)
		if got := parseListLimit(r); got != want {
			t.Errorf("parseListLimit(limit=%q) = %d, esperado %d", raw, got, want)
		}
	}
}

// ─── defaultGlobalRules / optionKeys — contrato das regras globais ───────────

// TestDefaultGlobalRules trava os defaults espelhados do PHP
// (senderzz_cod_finance_settings): retenção 7 dias, taxas zeradas.
func TestDefaultGlobalRules(t *testing.T) {
	d := defaultGlobalRules()
	if d.RetentionDays != 7 {
		t.Errorf("RetentionDays default = %d, esperado 7", d.RetentionDays)
	}
	if d.WithdrawFee != 0 || d.AnticipationFeePct != 0 || d.MotoboyFee != 0 || d.OperationalFundFee != 0 {
		t.Errorf("taxas default não zeradas: %+v", d)
	}
}

// TestOptionKeysMapping trava o mapeamento campo→chave em senderzz_options.
// Mudar uma chave aqui quebraria a leitura/escrita das regras já persistidas.
func TestOptionKeysMapping(t *testing.T) {
	want := map[string]string{
		"retention_days":       "sz_cod_retention_days",
		"withdraw_fee":         "sz_cod_withdraw_fee",
		"anticipation_fee_pct": "sz_cod_anticipation_fee_pct",
		"motoboy_fee":          "sz_admin_motoboy_fee",
		"operational_fund_fee": "sz_admin_operational_fund_fee",
	}
	if len(optionKeys) != len(want) {
		t.Fatalf("optionKeys tem %d chaves, esperado %d", len(optionKeys), len(want))
	}
	for k, v := range want {
		if got := optionKeys[k]; got != v {
			t.Errorf("optionKeys[%q] = %q, esperado %q", k, got, v)
		}
	}
}

// ─── codProofDir / codProofURL — caminho e URL do comprovante ────────────────

func TestCodProofDir(t *testing.T) {
	t.Run("default quando env vazio", func(t *testing.T) {
		t.Setenv("COD_PROOF_UPLOAD_PATH", "")
		if got := codProofDir(); got != "./uploads/cod-proofs" {
			t.Errorf("codProofDir() = %q, esperado ./uploads/cod-proofs", got)
		}
	})
	t.Run("override via env (com trim de espaços)", func(t *testing.T) {
		t.Setenv("COD_PROOF_UPLOAD_PATH", "  /var/data/proofs  ")
		if got := codProofDir(); got != "/var/data/proofs" {
			t.Errorf("codProofDir() = %q, esperado /var/data/proofs", got)
		}
	})
}

// TestCodProofURL cobre o default e a normalização de barra final do override.
func TestCodProofURL(t *testing.T) {
	t.Run("default termina em barra", func(t *testing.T) {
		t.Setenv("COD_PROOF_UPLOAD_URL", "")
		if got := codProofURL(); got != "/uploads/cod-proofs/" {
			t.Errorf("codProofURL() = %q, esperado /uploads/cod-proofs/", got)
		}
	})
	t.Run("override sem barra ganha barra", func(t *testing.T) {
		t.Setenv("COD_PROOF_UPLOAD_URL", "https://cdn.x/p")
		if got := codProofURL(); got != "https://cdn.x/p/" {
			t.Errorf("codProofURL() = %q, esperado https://cdn.x/p/", got)
		}
	})
	t.Run("override com barra não duplica", func(t *testing.T) {
		t.Setenv("COD_PROOF_UPLOAD_URL", "https://cdn.x/p/")
		got := codProofURL()
		if got != "https://cdn.x/p/" {
			t.Errorf("codProofURL() = %q, esperado https://cdn.x/p/", got)
		}
		if strings.HasSuffix(got, "//") {
			t.Errorf("codProofURL() = %q tem barra dupla", got)
		}
	})
}

// ─── strDeref — *string nil-safe (cod_wallet_producer.go) ────────────────────

func TestStrDeref(t *testing.T) {
	if got := strDeref(nil); got != "" {
		t.Errorf("strDeref(nil) = %q, esperado \"\"", got)
	}
	s := "pix@banco.com"
	if got := strDeref(&s); got != s {
		t.Errorf("strDeref(&%q) = %q, esperado %q", s, got, s)
	}
	empty := ""
	if got := strDeref(&empty); got != "" {
		t.Errorf("strDeref(&\"\") = %q, esperado \"\"", got)
	}
}

// ─── clampCodPage — paginação (cod_wallet_transactions.go) ───────────────────

func TestClampCodPage(t *testing.T) {
	cases := []struct {
		name           string
		page, perPage  int
		wantPg, wantPp int
	}{
		{name: "valores válidos passam direto", page: 3, perPage: 50, wantPg: 3, wantPp: 50},
		{name: "page <=0 vira 1", page: 0, perPage: 25, wantPg: 1, wantPp: 25},
		{name: "page negativa vira 1", page: -5, perPage: 25, wantPg: 1, wantPp: 25},
		{name: "perPage <=0 vira 50", page: 2, perPage: 0, wantPg: 2, wantPp: 50},
		{name: "perPage negativa vira 50", page: 2, perPage: -1, wantPg: 2, wantPp: 50},
		{name: "perPage acima de 200 clampa 200", page: 1, perPage: 999, wantPg: 1, wantPp: 200},
		{name: "perPage exato 200 mantém", page: 1, perPage: 200, wantPg: 1, wantPp: 200},
		{name: "ambos inválidos → defaults", page: -9, perPage: -9, wantPg: 1, wantPp: 50},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pg, pp := clampCodPage(c.page, c.perPage)
			if pg != c.wantPg || pp != c.wantPp {
				t.Errorf("clampCodPage(%d,%d) = (%d,%d), esperado (%d,%d)",
					c.page, c.perPage, pg, pp, c.wantPg, c.wantPp)
			}
		})
	}
}

// ─── buildCodTxWhere — WHERE dinâmico dos filtros ────────────────────────────

// buildCodTxWhere aceita qualquer coisa com Get(string)string → url.Values serve.
// Verifica: (a) só filtros presentes viram condição; (b) placeholders $N são
// sequenciais e batem 1:1 com os args; (c) filtro `q` só aparece com hasUsers.
func TestBuildCodTxWhere(t *testing.T) {
	t.Run("query vazia não gera condições", func(t *testing.T) {
		conds, args := buildCodTxWhere(url.Values{}, true, true)
		if len(conds) != 0 || len(args) != 0 {
			t.Errorf("query vazia: conds=%v args=%v, esperado vazio", conds, args)
		}
	})

	t.Run("user_id e order_id positivos viram condições; placeholders sequenciais", func(t *testing.T) {
		q := url.Values{}
		q.Set("user_id", "21")
		q.Set("order_id", "1570")
		conds, args := buildCodTxWhere(q, false, false)
		if len(conds) != 2 || len(args) != 2 {
			t.Fatalf("esperado 2 conds/2 args, veio conds=%v args=%v", conds, args)
		}
		// $1 e $2 na ordem de inserção (user_id antes de order_id).
		if !strings.Contains(conds[0], "t.user_id = $1") {
			t.Errorf("cond[0] = %q, esperado conter t.user_id = $1", conds[0])
		}
		if !strings.Contains(conds[1], "t.order_id = $2") {
			t.Errorf("cond[1] = %q, esperado conter t.order_id = $2", conds[1])
		}
		if args[0].(int64) != 21 || args[1].(int64) != 1570 {
			t.Errorf("args = %v, esperado [21 1570]", args)
		}
	})

	t.Run("user_id <=0 é ignorado (não vira condição)", func(t *testing.T) {
		q := url.Values{}
		q.Set("user_id", "0")
		q.Set("order_id", "-3")
		conds, args := buildCodTxWhere(q, false, false)
		if len(conds) != 0 || len(args) != 0 {
			t.Errorf("ids <=0 deveriam ser ignorados: conds=%v args=%v", conds, args)
		}
	})

	t.Run("type e status (strings) viram condições; vazio é trimado fora", func(t *testing.T) {
		q := url.Values{}
		q.Set("type", "commission")
		q.Set("status", "  ") // só espaços → trimado → ignorado
		conds, args := buildCodTxWhere(q, false, false)
		if len(conds) != 1 || len(args) != 1 {
			t.Fatalf("esperado só o type: conds=%v args=%v", conds, args)
		}
		if args[0].(string) != "commission" {
			t.Errorf("arg = %v, esperado commission", args[0])
		}
	})

	t.Run("filtro q exige hasUsers", func(t *testing.T) {
		q := url.Values{}
		q.Set("q", "gabriel")

		// hasUsers=false → q ignorado.
		conds, args := buildCodTxWhere(q, false, false)
		if len(conds) != 0 || len(args) != 0 {
			t.Errorf("com hasUsers=false, q deveria ser ignorado: conds=%v args=%v", conds, args)
		}

		// hasUsers=true → q gera condição ILIKE com placeholder reaproveitado.
		conds, args = buildCodTxWhere(q, true, false)
		if len(conds) != 1 || len(args) != 1 {
			t.Fatalf("com hasUsers=true, esperado 1 cond/1 arg: conds=%v args=%v", conds, args)
		}
		if !strings.Contains(conds[0], "ILIKE") {
			t.Errorf("cond do filtro q = %q, esperado conter ILIKE", conds[0])
		}
		if args[0].(string) != "gabriel" {
			t.Errorf("arg = %v, esperado gabriel", args[0])
		}
	})

	t.Run("includeRelease=false não adiciona filtros de release_at", func(t *testing.T) {
		q := url.Values{}
		q.Set("release_from", "2026-01-01")
		q.Set("release_to", "2026-12-31")
		conds, _ := buildCodTxWhere(q, false, false)
		for _, c := range conds {
			if strings.Contains(c, "release_at") {
				t.Errorf("includeRelease=false não deveria gerar filtro release_at: %q", c)
			}
		}
	})
}
