// Package handlers — histórico admin dos webhooks de MUDANÇA DE STATUS DE PEDIDO
// (order_status_*, sistema DIFERENTE do "Expedição › Webhooks" por classe de envio
// em expedicao_webhooks.go). Esse aqui é o outbox transacional (410-webhook-outbox.sql,
// trigger em sz_orders) + dispatcher (go/cron/internal/dispatch), configurado pelo
// PRODUTOR (senderzz_portal_webhooks) — sem tela admin até agora, dono pediu pra não
// precisar mais perguntar pro produtor se o webhook disparou.
//
// Tabelas envolvidas:
//   - sz_webhook_outbox        (fila transacional; sent_at NULL = pendente)
//   - senderzz_portal_webhooks (cadastro por produtor: url, secret, event_types)
//   - senderzz_webhook_log     (histórico de disparo: response_code/body por tentativa)
package handlers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

type OrderWebhooksHandler struct{ Pool *pgxpool.Pool }

func (h *OrderWebhooksHandler) tableExists(ctx context.Context, name string) bool {
	return tableExistsCached(ctx, h.Pool, name)
}

// OrderWebhookLogRow — uma tentativa de entrega registrada em senderzz_webhook_log.
type OrderWebhookLogRow struct {
	ID           int64  `json:"id"`
	WebhookID    int64  `json:"webhook_id"`
	ProdutorID   int64  `json:"produtor_id"`
	ProdutorNome string `json:"produtor_nome"`
	URL          string `json:"url"`
	EventType    string `json:"event_type"`
	ResponseCode *int   `json:"response_code"`
	ResponseBody string `json:"response_body"`
	CreatedAt    string `json:"created_at"`
}

// ListLogs — GET /order-webhooks/logs?limit=100&event_type=&produtor_id=
// Histórico de disparos reais (senderzz_webhook_log), mais recentes primeiro.
func (h *OrderWebhooksHandler) ListLogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "senderzz_webhook_log") {
		httpx.JSON(w, 200, map[string]any{"items": []OrderWebhookLogRow{}})
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	eventType := r.URL.Query().Get("event_type")
	produtorID, _ := strconv.ParseInt(r.URL.Query().Get("produtor_id"), 10, 64)

	hasUsers := h.tableExists(ctx, "senderzz_portal_users")
	nomeSel := "''::text"
	nomeJoin := ""
	if hasUsers {
		nomeSel = "COALESCE(u.nome, '')"
		nomeJoin = "LEFT JOIN senderzz_portal_users u ON u.id = pw.user_id"
	}

	query := `
		SELECT l.id, l.webhook_id, pw.user_id, ` + nomeSel + `, pw.url,
		       l.event_type, l.response_code, COALESCE(l.response_body, ''),
		       l.created_at::text
		  FROM senderzz_webhook_log l
		  JOIN senderzz_portal_webhooks pw ON pw.id = l.webhook_id
		  ` + nomeJoin + `
		 WHERE ($1 = '' OR l.event_type = $1)
		   AND ($2 <= 0 OR pw.user_id = $2)
		 ORDER BY l.id DESC
		 LIMIT $3`

	rows, err := h.Pool.Query(ctx, query, eventType, produtorID, limit)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []OrderWebhookLogRow{}
	for rows.Next() {
		var row OrderWebhookLogRow
		if err := rows.Scan(&row.ID, &row.WebhookID, &row.ProdutorID, &row.ProdutorNome,
			&row.URL, &row.EventType, &row.ResponseCode, &row.ResponseBody, &row.CreatedAt); err != nil {
			continue
		}
		out = append(out, row)
	}
	httpx.JSON(w, 200, map[string]any{"items": out})
}

// OutboxSummaryRow — status agregado do outbox por event_type (pendente vs enviado).
type OutboxSummaryRow struct {
	EventType string `json:"event_type"`
	Total     int64  `json:"total"`
	Enviados  int64  `json:"enviados"`
	Pendentes int64  `json:"pendentes"`
	Esgotados int64  `json:"esgotados"`
}

// OutboxSummary — GET /order-webhooks/outbox-summary
// Visão rápida: quais status já dispararam evento e quantos ainda estão
// pendentes de envio (sz_webhook_outbox.sent_at IS NULL).
func (h *OrderWebhooksHandler) OutboxSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_webhook_outbox") {
		httpx.JSON(w, 200, map[string]any{"items": []OutboxSummaryRow{}, "dispatch_enabled": false})
		return
	}

	// AUDIT-2026-07-31: "pendentes" misturava retry-ainda-tentando com
	// esgotado-pra-sempre (attempts >= max_attempts) — sem alerta nenhum pro
	// operador quando um webhook desistia de vez. Separa "esgotados" (nunca
	// mais vai tentar sozinho — precisa reenviar manual ou o produtor nunca
	// recebe aquele evento).
	rows, err := h.Pool.Query(ctx, `
		SELECT event_type, COUNT(*) AS total, COUNT(sent_at) AS enviados,
		       COUNT(*) FILTER (WHERE sent_at IS NULL AND attempts < max_attempts) AS pendentes,
		       COUNT(*) FILTER (WHERE sent_at IS NULL AND attempts >= max_attempts) AS esgotados
		  FROM sz_webhook_outbox
		 GROUP BY event_type
		 ORDER BY total DESC`)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []OutboxSummaryRow{}
	for rows.Next() {
		var row OutboxSummaryRow
		if err := rows.Scan(&row.EventType, &row.Total, &row.Enviados, &row.Pendentes, &row.Esgotados); err != nil {
			continue
		}
		out = append(out, row)
	}

	var enabled string
	_ = h.Pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE name = 'webhook_dispatch_enabled'`,
	).Scan(&enabled)

	httpx.JSON(w, 200, map[string]any{
		"items":            out,
		"dispatch_enabled": enabled == "1",
	})
}

// ResendStale — POST /order-webhooks/resend-stale
//
// AUDIT-2026-07-30 #13 (dono: "faça conforme melhor prática"): o dispatcher
// (go/cron/internal/dispatch) só entrega eventos criados dentro da janela de
// frescor (webhook_dispatch_freshness_seconds, default 2h) — proteção
// intencional contra inundar o endpoint do produtor com backlog velho após
// uma queda. Eventos mais antigos que isso ficam "pendente" pra sempre, SEM
// alternativa manual até agora.
//
// Best practice: não alargar a janela global (isso enfraqueceria a proteção
// permanentemente pro PRÓXIMO incidente também) — em vez disso, uma ação
// ADMIN EXPLÍCITA que só "renova" created_at/next_attempt_at dos pendentes
// travados, sem duplicar a lógica de entrega/assinatura HMAC (que mora no
// cron, módulo sem HTTP, e não é importado aqui de propósito — convenção do
// projeto). O próximo tick do cron (≤60s) entrega normalmente pelo pipeline
// já testado, com todo o rate-limit/retry/idempotência de sempre.
func (h *OrderWebhooksHandler) ResendStale(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_webhook_outbox") {
		httpx.Err(w, 404, "not_found", "outbox de webhooks não existe")
		return
	}

	tag, err := h.Pool.Exec(ctx, `
		UPDATE sz_webhook_outbox
		   SET created_at = NOW(), next_attempt_at = NOW()
		 WHERE sent_at IS NULL
		   AND attempts < max_attempts`)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 200, map[string]any{"ok": true, "renovados": tag.RowsAffected()})
}

// ResendExhausted — POST /order-webhooks/resend-exhausted
//
// AUDIT-2026-07-31: ResendStale (acima) SÓ pega attempts < max_attempts — um
// webhook que esgotou as 6 tentativas fica travado pra SEMPRE, sem alerta e
// sem caminho manual (achado ao vivo revisando o dispatcher). Diferente de
// ResendStale (que só renova o relógio pra sair da janela de frescor), aqui
// PRECISA zerar attempts também — senão o SELECT do cron (attempts <
// max_attempts) continua excluindo a linha no próximo tick.
func (h *OrderWebhooksHandler) ResendExhausted(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_webhook_outbox") {
		httpx.Err(w, 404, "not_found", "outbox de webhooks não existe")
		return
	}

	tag, err := h.Pool.Exec(ctx, `
		UPDATE sz_webhook_outbox
		   SET created_at = NOW(), next_attempt_at = NOW(), attempts = 0, last_error = NULL
		 WHERE sent_at IS NULL
		   AND attempts >= max_attempts`)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 200, map[string]any{"ok": true, "renovados": tag.RowsAffected()})
}
