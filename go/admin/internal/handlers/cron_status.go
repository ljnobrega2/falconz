// Package handlers — endpoint admin para CronStatus.
// Lista os 18 crons do plugin Senderzz (vide AUDIT-ADMIN-WP.md §19),
// merge com metadata de última execução guardada em senderzz_cron_status.
// Em produção, jobs reais rodam via Asynq (Redis); esta tela é VIEWER +
// manual trigger (apenas marca a linha; não enfileira ainda).
package handlers

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

type CronStatusHandler struct{ Pool *pgxpool.Pool }

// CronInfo é a struct hard-coded de cada cron (nome, frequência, etc.) +
// os campos de metadata (last_run, last_status…) preenchidos via merge com
// senderzz_cron_status. Os campos *time.Time são serializados como null
// quando ainda não houve execução.
type CronInfo struct {
	Name           string     `json:"name"`
	Frequency      string     `json:"frequency"`
	Description    string     `json:"description"`
	Tables         []string   `json:"tables"`
	HandlerPath    string     `json:"handler_path"`
	LastRun        *time.Time `json:"last_run"`
	LastStatus     string     `json:"last_status"`
	LastMessage    string     `json:"last_message"`
	LastDurationMs int64      `json:"last_duration_ms"`
	NextRun        *time.Time `json:"next_run"`
}

// cronCatalog — fonte da verdade dos 18 crons do plugin.
// Mantém ordem estável para a UI (não ordenar alfabético).
var cronCatalog = []CronInfo{
	{
		Name:        "tpc_cron_verificar_recargas_pix",
		Frequency:   "5min",
		Description: "Verifica recargas PIX pendentes via API ME e concilia saldo da carteira.",
		Tables:      []string{"tpc_recargas", "tpc_transacoes"},
		HandlerPath: "go/wallet/internal/jobs/pix.go",
	},
	{
		Name:        "senderzz_db_cleanup",
		Frequency:   "daily",
		Description: "Limpeza diária de logs antigos (webhooks, integrações, sessões expiradas).",
		Tables:      []string{"senderzz_webhook_log", "senderzz_integration_log"},
		HandlerPath: "includes/senderzz-cleanup.php",
	},
	{
		Name:        "senderzz_me_reconcile_shipments",
		Frequency:   "5min",
		Description: "Reconcilia envios Melhor Envio (status, postagem, eventos não recebidos).",
		Tables:      []string{"wc_me_labels"},
		HandlerPath: "includes/senderzz-me-reconcile.php",
	},
	{
		Name:        "sz_posted_polling_cron",
		Frequency:   "5min",
		Description: "Polling de etiquetas postadas para forçar atualização de tracking.",
		Tables:      []string{"wc_me_labels"},
		HandlerPath: "includes/senderzz-posted-polling.php",
	},
	{
		Name:        "senderzz_auto_generate_label",
		Frequency:   "single",
		Description: "Gera etiqueta automaticamente após status pago (job único agendado por pedido).",
		Tables:      []string{"wc_me_labels"},
		HandlerPath: "src/Pipeline/Label_Pipeline.php",
	},
	{
		Name:        "senderzz_check_low_balance",
		Frequency:   "single 30s",
		Description: "Verifica saldo baixo da carteira ME e dispara alerta ao produtor.",
		Tables:      []string{"tpc_carteira"},
		HandlerPath: "includes/senderzz-low-balance.php",
	},
	{
		Name:        "sz_cod_release_cron",
		Frequency:   "hourly",
		Description: "Libera saldos COD (cash on delivery) retidos após período de retenção.",
		Tables:      []string{"sz_cod_wallet_transactions"},
		HandlerPath: "includes/senderzz-cod-wallet.php",
	},
	{
		Name:        "sz_aff_release_commissions",
		Frequency:   "hourly",
		Description: "Libera comissões de afiliado vencidas (status pending → available).",
		Tables:      []string{"senderzz_affiliate_transactions", "senderzz_affiliate_wallet"},
		HandlerPath: "includes/senderzz-affiliates.php",
	},
	{
		Name:        "sz_motoboy_geofence_check",
		Frequency:   "1min",
		Description: "Verifica cerca geográfica dos motoboys ativos (Haversine vs CDs/zonas).",
		Tables:      []string{"sz_motoboys", "sz_motoboy_audit"},
		HandlerPath: "includes/motoboy/geofence.php",
	},
	{
		Name:        "sz_pix_auto_reconcile_cron",
		Frequency:   "5min",
		Description: "Reconciliação automática PIX contra saldo ME (fail-closed se divergir).",
		Tables:      []string{"tpc_recargas", "tpc_transacoes"},
		HandlerPath: "includes/tpc/pix-auto-reconcile.php",
	},
	{
		Name:        "sz_wallet_divergence_check",
		Frequency:   "daily",
		Description: "Audita carteira: soma de transações vs saldo persistido (alerta se divergir).",
		Tables:      []string{"tpc_carteira", "tpc_transacoes"},
		HandlerPath: "includes/tpc/wallet.php",
	},
	{
		Name:        "senderzz_generate_label_cron",
		Frequency:   "single 1s",
		Description: "Geração de etiqueta agendada (cron-once) para evitar timeout na requisição original.",
		Tables:      []string{"wc_me_labels"},
		HandlerPath: "src/Queue/Label_Job.php",
	},
	{
		Name:        "senderzz_check_me_refund_status",
		Frequency:   "single 300s",
		Description: "Confere status de estorno/cancelamento ME após 5 min e atualiza saldo.",
		Tables:      []string{"wc_me_labels", "tpc_transacoes"},
		HandlerPath: "includes/senderzz-me-refund.php",
	},
	{
		Name:        "senderzz_cancel_me_label_async",
		Frequency:   "single",
		Description: "Cancela etiqueta ME de forma assíncrona (job único agendado pós-checkout).",
		Tables:      []string{"wc_me_labels"},
		HandlerPath: "includes/senderzz-me-cancel.php",
	},
	{
		Name:        "senderzz_push_new_order",
		Frequency:   "single 3-5s",
		Description: "Envia novo pedido aos webhooks configurados (debounce 3-5s para agregar metas).",
		Tables:      []string{"senderzz_webhook_log"},
		HandlerPath: "includes/senderzz-webhooks.php",
	},
	{
		Name:        "senderzz_retry_label_pipeline",
		Frequency:   "single 60s",
		Description: "Re-executa pipeline de etiqueta em caso de falha transitória (retry com backoff).",
		Tables:      []string{"wc_me_labels"},
		HandlerPath: "src/Pipeline/Label_Pipeline.php",
	},
	{
		Name:        "senderzz_portal_cleanup_sessions",
		Frequency:   "twicedaily",
		Description: "Limpa sessões expiradas do portal de OLs (2x/dia).",
		Tables:      []string{"wp_senderzz_portal_sessions"},
		HandlerPath: "src/Portal/Portal_Auth.php",
	},
	{
		Name:        "wc_melhor_envio_check_posted",
		Frequency:   "hourly",
		Description: "Verifica etiquetas marcadas como postadas mas sem evento de tracking confirmado.",
		Tables:      []string{"wc_me_labels"},
		HandlerPath: "src/Webhook/Tracking_Webhook.php",
	},
}

// cronCatalogIndex — mapa nome → índice no slice. Usado para whitelist em
// trigger/skip-next (rejeita nomes arbitrários com 404).
var cronCatalogIndex = func() map[string]int {
	m := make(map[string]int, len(cronCatalog))
	for i, c := range cronCatalog {
		m[c.Name] = i
	}
	return m
}()

// tableExists espelha o helper do AuditHandler (graceful degradation).
func (h *CronStatusHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// frequencyToDuration converte a string de frequência em time.Duration.
// Usado para calcular next_run em /skip-next (now + freq*2 = pula próxima).
// Para entradas "single*" retorna 1h (skip-next em job one-shot é
// conceitualmente no-op; mantemos 1h para a UI exibir algo coerente).
func frequencyToDuration(freq string) time.Duration {
	switch freq {
	case "1min":
		return 1 * time.Minute
	case "5min":
		return 5 * time.Minute
	case "hourly":
		return 1 * time.Hour
	case "twicedaily":
		return 12 * time.Hour
	case "daily":
		return 24 * time.Hour
	default:
		// single, single Xs, etc. — sem cadência periódica
		return 1 * time.Hour
	}
}

// List retorna o catálogo dos 18 crons mesclado com metadata da tabela
// senderzz_cron_status (se existir). Se a tabela não existe, todos os
// crons retornam com last_status="never" (graceful).
// GET /crons
func (h *CronStatusHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Copia catálogo (cada request) e seta defaults de "never".
	out := make([]CronInfo, len(cronCatalog))
	copy(out, cronCatalog)
	for i := range out {
		out[i].LastStatus = "never"
	}

	if h.tableExists(ctx, "senderzz_cron_status") {
		rows, err := h.Pool.Query(ctx,
			`SELECT name, last_run, last_status, last_message,
			        last_duration_ms, next_run
			   FROM senderzz_cron_status`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var (
					name         string
					lastRun      *time.Time
					lastStatus   *string
					lastMessage  *string
					lastDuration *int64
					nextRun      *time.Time
				)
				if err := rows.Scan(&name, &lastRun, &lastStatus, &lastMessage,
					&lastDuration, &nextRun); err != nil {
					continue
				}
				idx, ok := cronCatalogIndex[name]
				if !ok {
					continue
				}
				out[idx].LastRun = lastRun
				if lastStatus != nil && *lastStatus != "" {
					out[idx].LastStatus = *lastStatus
				}
				if lastMessage != nil {
					out[idx].LastMessage = *lastMessage
				}
				if lastDuration != nil {
					out[idx].LastDurationMs = *lastDuration
				}
				out[idx].NextRun = nextRun
			}
		}
	}

	httpx.JSON(w, 200, map[string]any{"items": out})
}

// Trigger registra a execução manual do cron.
// Atualiza senderzz_cron_status (última execução) e insere uma linha em
// senderzz_cron_runs (histórico). Enfileiramento real no Asynq ainda pendente —
// o botão dispara o registro; o handler PHP/worker correspondente NÃO é invocado.
// POST /crons/{name}/trigger
func (h *CronStatusHandler) Trigger(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if _, ok := cronCatalogIndex[name]; !ok {
		httpx.Err(w, 404, "not_found", "cron desconhecido")
		return
	}

	ctx := r.Context()
	now := time.Now().UTC()

	// Atualiza status atual (última execução).
	if h.tableExists(ctx, "senderzz_cron_status") {
		_, _ = h.Pool.Exec(ctx,
			`INSERT INTO senderzz_cron_status
			   (name, last_run, last_status, last_message, last_duration_ms)
			 VALUES ($1, $2, 'manual_trigger', 'disparado manualmente via painel admin', 0)
			 ON CONFLICT (name) DO UPDATE SET
			   last_run         = EXCLUDED.last_run,
			   last_status      = EXCLUDED.last_status,
			   last_message     = EXCLUDED.last_message,
			   last_duration_ms = EXCLUDED.last_duration_ms`, name, now)
	}

	// Insere linha de histórico (graceful se tabela ausente).
	if h.tableExists(ctx, "senderzz_cron_runs") {
		_, _ = h.Pool.Exec(ctx,
			`INSERT INTO senderzz_cron_runs (name, started_at, duration_ms, status, message)
			 VALUES ($1, $2, 0, 'manual_trigger', 'disparado manualmente via painel admin')`,
			name, now)
	}

	// queued: false — o handler PHP/worker correspondente NÃO é invocado.
	// Apenas o registro histórico é feito. Integração real com Asynq pendente.
	httpx.JSON(w, 200, map[string]any{"ok": true, "queued": false, "name": name})
}

// SkipNext marca next_run = now + frequency*2 para pular a próxima execução.
// POST /crons/{name}/skip-next
func (h *CronStatusHandler) SkipNext(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	idx, ok := cronCatalogIndex[name]
	if !ok {
		httpx.Err(w, 404, "not_found", "cron desconhecido")
		return
	}

	ctx := r.Context()
	dur := frequencyToDuration(cronCatalog[idx].Frequency)
	nextRun := time.Now().Add(dur * 2)

	if h.tableExists(ctx, "senderzz_cron_status") {
		_, _ = h.Pool.Exec(ctx,
			`INSERT INTO senderzz_cron_status
			   (name, next_run, last_status, last_message)
			 VALUES ($1, $2, 'skipped', 'next run skipped via admin')
			 ON CONFLICT (name) DO UPDATE SET
			   next_run     = EXCLUDED.next_run,
			   last_status  = EXCLUDED.last_status,
			   last_message = EXCLUDED.last_message`, name, nextRun)
	}

	httpx.JSON(w, 200, map[string]any{
		"ok":       true,
		"name":     name,
		"next_run": nextRun.UTC().Format(time.RFC3339),
	})
}

// recentRunRow — linha retornada pelo histórico de execuções.
type recentRunRow struct {
	StartedAt  *time.Time `json:"started_at"`
	DurationMs int64      `json:"duration_ms"`
	Status     string     `json:"status"`
	Message    string     `json:"message"`
}

// RecentRuns retorna as últimas execuções de um cron consultando
// senderzz_cron_runs. Graceful: se a tabela não existir (instalação antiga),
// retorna lista vazia sem erro.
// GET /crons/{name}/recent-runs?limit=20
func (h *CronStatusHandler) RecentRuns(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if _, ok := cronCatalogIndex[name]; !ok {
		httpx.Err(w, 404, "not_found", "cron desconhecido")
		return
	}

	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	ctx := r.Context()
	runs := make([]recentRunRow, 0)

	if h.tableExists(ctx, "senderzz_cron_runs") {
		rows, err := h.Pool.Query(ctx,
			`SELECT started_at, duration_ms, status, message
			   FROM senderzz_cron_runs
			  WHERE name = $1
			  ORDER BY started_at DESC
			  LIMIT $2`, name, limit)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var row recentRunRow
				if err := rows.Scan(&row.StartedAt, &row.DurationMs, &row.Status, &row.Message); err != nil {
					continue
				}
				runs = append(runs, row)
			}
		}
	}

	httpx.JSON(w, 200, map[string]any{
		"items": runs,
		"name":  name,
		"limit": limit,
	})
}
