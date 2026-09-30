// Testes de INTEGRAÇÃO (com DB) do viewer de crons (cron_status.go) e do alerta
// de cron financeiro falhando (dashboard.go).
//
// ACHADO P1 que estes testes travam: os crons que LIBERAM DINHEIRO
// (sz_cod_release_due = COD pending→available; sz_affiliate_release_due =
// comissão pending→approved) ficavam INVISÍVEIS no viewer porque o catálogo
// usava nomes que NÃO existem no banco (sz_cod_release_cron / sz_aff_release_commissions).
// List() descartava a linha real (whitelist `if !ok { continue }`) e mostrava
// "never" enganoso, mesmo o runner go/cron tendo rodado.
//
//   - TestCronStatus_LiberaDinheiro_Visivel: prova, contra o DB real, que o
//     viewer agora devolve sz_cod_release_due e sz_affiliate_release_due com
//     last_run PREENCHIDO (não "never"). Roda o código NOVO (não a borda viva),
//     então satisfaz `go test` sem reiniciar serviço.
//   - TestDashboardAlerts_CronFinanceiroFalhando: prova que um cron em
//     last_status='error' aparece no alerta cron_financeiro_falhando. Seed +
//     restauração via t.Cleanup — nunca deixa lixo no senderzz_cron_status.
//
// SKIP silencioso sem DATABASE_URL (testPoolOrSkip). Auth no MIDDLEWARE → precisa
// de JWT secret via t.Setenv + admin sintético via mintAdmin (mesmo padrão de
// config_taxas_test.go).
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/senderzz/admin-service/internal/auth"
)

// mountCrons monta um router chi mínimo espelhando main.go: GET /crons atrás do
// auth.Middleware (a auth vive no MIDDLEWARE; chamar h.List direto devolveria 200
// sem token e não provaria a borda real).
func mountCrons(t *testing.T) (*chi.Mux, *CronStatusHandler) {
	t.Helper()
	pool := testPoolOrSkip(t)
	h := &CronStatusHandler{Pool: pool}
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(pool))
		r.Get("/crons", h.List)
	})
	return r, h
}

// cronListResp espelha o envelope de List() (apenas os campos que assertamos).
type cronListResp struct {
	Items []struct {
		Name       string     `json:"name"`
		LastRun    *time.Time `json:"last_run"`
		LastStatus string     `json:"last_status"`
	} `json:"items"`
}

// dashAlertsResp espelha o campo do alerta novo de Alerts().
type dashAlertsResp struct {
	CronFinanceiroFalhando int64 `json:"cron_financeiro_falhando"`
}

// ─── Viewer mostra os crons que liberam dinheiro com last_run real ───────────

func TestCronStatus_LiberaDinheiro_Visivel(t *testing.T) {
	t.Setenv("ADMIN_JWT_SECRET", "test-admin-secret-cron")
	t.Setenv("JWT_SECRET", "test-admin-secret-cron")
	r, h := mountCrons(t)
	token := mintAdmin(t, h.Pool, 990000301, "cron-list@test.local")

	req := httptest.NewRequest(http.MethodGet, "/crons", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /crons com admin = %d (%s), esperado 200", rec.Code, rec.Body.String())
	}
	var got cronListResp
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("resposta /crons não é cronListResp: %v — body=%s", err, rec.Body.String())
	}

	// Indexa por nome.
	byName := make(map[string]struct {
		LastRun    *time.Time
		LastStatus string
	}, len(got.Items))
	for _, it := range got.Items {
		byName[it.Name] = struct {
			LastRun    *time.Time
			LastStatus string
		}{it.LastRun, it.LastStatus}
	}

	// Os DOIS crons financeiros, com nome REAL, devem estar no catálogo e — se o
	// runner já rodou (linha em senderzz_cron_status) — com last_run preenchido e
	// status != "never". Lemos o DB diretamente para decidir a asserção forte vs.
	// fraca, sem assumir que o runner rodou nesta máquina.
	for _, name := range []string{"sz_cod_release_due", "sz_affiliate_release_due"} {
		row, ok := byName[name]
		if !ok {
			t.Fatalf("cron financeiro %q ausente do viewer — catálogo desalinhado (regressão do ACHADO P1)", name)
		}

		// O DB tem linha real para este cron?
		var dbLastRun *time.Time
		var dbLastStatus string
		err := h.Pool.QueryRow(context.Background(),
			`SELECT last_run, last_status FROM senderzz_cron_status WHERE name = $1`,
			name).Scan(&dbLastRun, &dbLastStatus)

		if err != nil {
			// Sem linha no DB (runner nunca rodou aqui): o viewer mostra "never",
			// o que é honesto. Não falha — mas registra para visibilidade.
			t.Logf("%q sem linha em senderzz_cron_status (runner não rodou nesta máquina); viewer=%q", name, row.LastStatus)
			continue
		}

		// Há linha real → o viewer NÃO pode mais descartá-la nem mostrar "never".
		if row.LastStatus == "never" {
			t.Errorf("%q tem linha real no DB (last_status=%q) mas o viewer mostra \"never\" — List() descartou a linha (ACHADO P1 não corrigido)", name, dbLastStatus)
		}
		if dbLastRun != nil && row.LastRun == nil {
			t.Errorf("%q tem last_run no DB (%v) mas o viewer devolveu null — merge não aplicou a linha real", name, dbLastRun)
		}
		if row.LastStatus != dbLastStatus {
			t.Errorf("%q last_status viewer=%q != DB=%q", name, row.LastStatus, dbLastStatus)
		}
	}
}

// ─── Alerta dispara quando um cron financeiro está em last_status='error' ────

func TestDashboardAlerts_CronFinanceiroFalhando(t *testing.T) {
	t.Setenv("ADMIN_JWT_SECRET", "test-admin-secret-cron")
	t.Setenv("JWT_SECRET", "test-admin-secret-cron")
	pool := testPoolOrSkip(t)
	h := &DashboardHandler{Pool: pool}
	r := chi.NewRouter()
	token := mintAdmin(t, pool, 990000302, "cron-alert@test.local")
	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(pool))
		r.Get("/dashboard/alerts", h.Alerts)
	})

	ctx := context.Background()

	// Baseline: alerta atual (pode haver outros crons em error legitimamente).
	base := readCronAlert(t, r, token)

	// Seed: marca um cron sintético como 'error'. UPSERT por name; t.Cleanup remove.
	const seedName = "sz_test_cron_falho_990302"
	if _, err := pool.Exec(ctx,
		`INSERT INTO senderzz_cron_status (name, last_run, last_status, last_message, last_duration_ms)
		 VALUES ($1, NOW(), 'error', 'seed de teste — cron falho', 0)
		 ON CONFLICT (name) DO UPDATE SET last_status='error', last_run=NOW()`,
		seedName); err != nil {
		t.Fatalf("seed cron error: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM senderzz_cron_status WHERE name=$1`, seedName)
	})

	after := readCronAlert(t, r, token)
	if after != base+1 {
		t.Fatalf("cron_financeiro_falhando = %d, esperado baseline+1 (%d) após seed 'error'", after, base+1)
	}

	// E o estado 'skipped'/'manual_trigger'/'never' NÃO deve contar (filtro ='error',
	// não '!=ok'). Re-seta o seed para 'skipped' e confere que o alerta volta ao baseline.
	if _, err := pool.Exec(ctx,
		`UPDATE senderzz_cron_status SET last_status='skipped' WHERE name=$1`, seedName); err != nil {
		t.Fatalf("re-seed cron skipped: %v", err)
	}
	if back := readCronAlert(t, r, token); back != base {
		t.Fatalf("cron_financeiro_falhando = %d com seed 'skipped', esperado baseline %d — filtro deve ser ='error', não '!=ok'", back, base)
	}
}

// readCronAlert faz GET /dashboard/alerts autenticado e devolve o contador do alerta de cron.
func readCronAlert(t *testing.T, r *chi.Mux, token string) int64 {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/dashboard/alerts", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /dashboard/alerts = %d (%s), esperado 200", rec.Code, rec.Body.String())
	}
	var got dashAlertsResp
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("resposta /dashboard/alerts inválida: %v — body=%s", err, rec.Body.String())
	}
	return got.CronFinanceiroFalhando
}
