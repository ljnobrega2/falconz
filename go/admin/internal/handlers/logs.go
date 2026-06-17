package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

type LogsHandler struct{ Pool *pgxpool.Pool }

// webhookLog espelha senderzz_webhook_log (schema-portal.sql).
// Colunas reais: id, webhook_id, event_type, payload, response_code, response_body, created_at.
type webhookLog struct {
	ID           int64           `json:"id"`
	WebhookID    int64           `json:"webhook_id"`
	EventType    string          `json:"event_type"`
	Payload      json.RawMessage `json:"payload"`
	ResponseCode *int            `json:"response_code"`
	ResponseBody *string         `json:"response_body"`
	Success      bool            `json:"success"`
	CreatedAt    string          `json:"created_at"`
}

// integrationLog espelha senderzz_integration_log (schema-portal.sql).
// Colunas reais: id, user_id, event, payload, created_at.
type integrationLog struct {
	ID        int64           `json:"id"`
	UserID    int64           `json:"user_id"`
	Event     string          `json:"event"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt string          `json:"created_at"`
}

func (h *LogsHandler) Webhooks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	offset, _ := strconv.Atoi(q.Get("offset"))

	rows, err := h.Pool.Query(r.Context(),
		`SELECT id, webhook_id, event_type, payload, response_code, response_body, created_at::text
		 FROM senderzz_webhook_log
		 ORDER BY id DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []webhookLog{}
	for rows.Next() {
		var l webhookLog
		var payload []byte
		_ = rows.Scan(&l.ID, &l.WebhookID, &l.EventType, &payload, &l.ResponseCode, &l.ResponseBody, &l.CreatedAt)
		if len(payload) > 0 {
			l.Payload = json.RawMessage(payload)
		} else {
			l.Payload = json.RawMessage("{}")
		}
		// success derivado de response_code < 400
		if l.ResponseCode != nil {
			l.Success = *l.ResponseCode < 400
		}
		out = append(out, l)
	}

	var total int64
	_ = h.Pool.QueryRow(r.Context(), `SELECT COUNT(*) FROM senderzz_webhook_log`).Scan(&total)
	httpx.JSON(w, 200, map[string]any{"items": out, "total": total})
}

func (h *LogsHandler) Integrations(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	offset, _ := strconv.Atoi(q.Get("offset"))

	rows, err := h.Pool.Query(r.Context(),
		`SELECT id, user_id, event, payload, created_at::text
		 FROM senderzz_integration_log
		 ORDER BY id DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []integrationLog{}
	for rows.Next() {
		var l integrationLog
		var payload []byte
		_ = rows.Scan(&l.ID, &l.UserID, &l.Event, &payload, &l.CreatedAt)
		if len(payload) > 0 {
			l.Payload = json.RawMessage(payload)
		} else {
			l.Payload = json.RawMessage("{}")
		}
		out = append(out, l)
	}

	var total int64
	_ = h.Pool.QueryRow(r.Context(), `SELECT COUNT(*) FROM senderzz_integration_log`).Scan(&total)
	httpx.JSON(w, 200, map[string]any{"items": out, "total": total})
}

func (h *LogsHandler) MoyboyAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	offset, _ := strconv.Atoi(q.Get("offset"))

	// sz_motoboy_audit colunas: id, pedido_id, motoboy_id, actor_tipo, actor_id,
	// acao, de_status, para_status, meta_json, ip_address, created_at.
	rows, err := h.Pool.Query(r.Context(),
		`SELECT id, pedido_id, motoboy_id, actor_tipo, acao, de_status, para_status, meta_json, created_at::text
		 FROM sz_motoboy_audit
		 ORDER BY id DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	type auditRow struct {
		ID         int64   `json:"id"`
		PedidoID   *int64  `json:"pedido_id"`
		MotoboyID  *int64  `json:"motoboy_id"`
		ActorTipo  string  `json:"actor_tipo"`
		Acao       string  `json:"acao"`
		DeStatus   *string `json:"de_status"`
		ParaStatus *string `json:"para_status"`
		MetaJSON   *string `json:"meta_json"`
		CreatedAt  string  `json:"created_at"`
	}
	out := []auditRow{}
	for rows.Next() {
		var a auditRow
		_ = rows.Scan(&a.ID, &a.PedidoID, &a.MotoboyID, &a.ActorTipo, &a.Acao,
			&a.DeStatus, &a.ParaStatus, &a.MetaJSON, &a.CreatedAt)
		out = append(out, a)
	}

	var total int64
	_ = h.Pool.QueryRow(r.Context(), `SELECT COUNT(*) FROM sz_motoboy_audit`).Scan(&total)
	httpx.JSON(w, 200, map[string]any{"items": out, "total": total})
}
