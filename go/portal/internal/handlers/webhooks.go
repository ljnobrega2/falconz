// Package handlers — handlers de webhooks do Portal V2.
//
// Rotas (namespace /wp-json/senderzz/v1):
//   GET  /portal/webhooks                — lista webhooks do usuário (url != '')
//   POST /portal/webhooks                — cria/atualiza webhook
//   DELETE /portal/webhooks/{id}         — soft-delete (url='', active=false)
//   GET  /portal/webhooks/{id}/history   — histórico de disparos
//   POST /portal/webhooks/clear-history  — limpa logs de webhook
//
// Soft-delete (espelha Portal_Page.php::ajax_webhooks_delete):
//   Hard-delete faz senderzz_pw_ensure_user_webhook_slots() recriar o slot.
//   Soft-delete: url='', active=false. Query de listagem filtra url != ''.
//
// Whitelist de event_types (DT-CODE-02):
//   order_status_enviado, order_status_entregue, order_status_cancelado,
//   order_status_frustrado, order_status_em_rota, order_status_embalado
//   Qualquer outro valor é rejeitado com 400.
package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/portal-service/internal/auth"
	"github.com/senderzz/portal-service/internal/httpx"
)

// WebhookHandler agrupa as dependências dos handlers de webhook.
type WebhookHandler struct {
	Pool *pgxpool.Pool
}

// allowedEventTypes — whitelist de tipos de evento aceitos.
// DT-CODE-02: não usar substring match — exact match apenas.
// Substring match é o bypass CRIT-04 (ver CHANGELOG-SECURITY.md).
// Todos os valores usam o prefixo completo order_status_* — espelha a spec.
var allowedEventTypes = map[string]bool{
	"order_status_enviado":   true,
	"order_status_entregue":  true,
	"order_status_cancelado": true,
	"order_status_frustrado": true,
	"order_status_em_rota":   true,
	"order_status_embalado":  true,
}

// webhookResponse representa um webhook na listagem.
type webhookResponse struct {
	ID              int64             `json:"id"`
	URL             string            `json:"url"`
	Active          bool              `json:"active"`
	EventTypes      []string          `json:"event_types"`
	ShippingClassID *int              `json:"shipping_class_id,omitempty"`
	CreatedAt       string            `json:"created_at"`
	UpdatedAt       string            `json:"updated_at"`
}

// webhookLogEntry representa uma entrada no histórico de disparos.
type webhookLogEntry struct {
	ID           int64  `json:"id"`
	EventType    string `json:"event_type"`
	ResponseCode *int   `json:"response_code"`
	ResponseBody *string `json:"response_body,omitempty"`
	CreatedAt    string `json:"created_at"`
}

// createWebhookRequest é o body de POST /portal/webhooks.
type createWebhookRequest struct {
	URL             string   `json:"url"`
	Secret          string   `json:"secret"`
	Active          *bool    `json:"active"`
	EventTypes      []string `json:"event_types"`
	ShippingClassID *int     `json:"shipping_class_id"`
}

// ── GET /portal/webhooks ──────────────────────────────────────────────────────

// List retorna os webhooks configurados pelo usuário autenticado.
// Filtra slots com url='' (soft-deleted).
func (h *WebhookHandler) List(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	rows, err := h.Pool.Query(r.Context(),
		`SELECT id, url, active, event_types,
		        shipping_class_id, created_at, updated_at
		   FROM senderzz_portal_webhooks
		  WHERE user_id = $1
		    AND url != ''
		  ORDER BY id ASC`,
		u.ID,
	)
	if err != nil {
		slog.Error("[portal_webhooks] erro ao listar webhooks", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer rows.Close()

	var result []webhookResponse
	for rows.Next() {
		var wh webhookResponse
		var etJSON []byte
		err := rows.Scan(
			&wh.ID, &wh.URL, &wh.Active, &etJSON,
			&wh.ShippingClassID, &wh.CreatedAt, &wh.UpdatedAt,
		)
		if err != nil {
			slog.Error("[portal_webhooks] erro ao ler linha", "user_id", u.ID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao ler webhooks")
			return
		}
		if err := json.Unmarshal(etJSON, &wh.EventTypes); err != nil {
			wh.EventTypes = []string{}
		}
		result = append(result, wh)
	}
	if rows.Err() != nil {
		slog.Error("[portal_webhooks] erro após iteração", "user_id", u.ID, "err", rows.Err())
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao processar webhooks")
		return
	}

	if result == nil {
		result = []webhookResponse{}
	}

	httpx.WriteOK(w, map[string]any{"data": result, "total": len(result)})
}

// ── POST /portal/webhooks ────────────────────────────────────────────────────

// Create cria ou atualiza um webhook.
// Valida a whitelist de event_types (DT-CODE-02) antes de persistir.
func (h *WebhookHandler) Create(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	var req createWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}
	if req.URL == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "url é obrigatória")
		return
	}

	// Valida event_types contra whitelist — DT-CODE-02: exact match, nunca substring.
	for _, et := range req.EventTypes {
		if !allowedEventTypes[et] {
			httpx.WriteErr(w, http.StatusBadRequest,
				"tipo de evento inválido: "+et+
					" — permitidos: order_status_enviado, order_status_entregue, order_status_cancelado, order_status_frustrado, order_status_em_rota, order_status_embalado")
			return
		}
	}

	etJSON, _ := json.Marshal(req.EventTypes)

	active := true
	if req.Active != nil {
		active = *req.Active
	}

	var id int64
	err := h.Pool.QueryRow(r.Context(),
		`INSERT INTO senderzz_portal_webhooks
		    (user_id, url, secret, active, event_types, shipping_class_id, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, NOW())
		 RETURNING id`,
		u.ID, req.URL, req.Secret, active, etJSON, req.ShippingClassID,
	).Scan(&id)
	if err != nil {
		slog.Error("[portal_webhooks] erro ao criar webhook", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	slog.Info("[portal_webhooks] webhook criado", "user_id", u.ID, "webhook_id", id, "url", req.URL)
	httpx.WriteOK(w, map[string]any{"id": id, "mensagem": "webhook criado com sucesso"})
}

// ── DELETE /portal/webhooks/{id} ─────────────────────────────────────────────

// Delete realiza soft-delete do webhook: url='', active=false.
// Não usa hard-delete pois senderzz_pw_ensure_user_webhook_slots() recriaria o slot.
func (h *WebhookHandler) Delete(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	idStr := chi.URLParam(r, "id")
	webhookID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || webhookID <= 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}

	// Soft-delete: url='', active=false. Garante que o webhook pertence ao usuário.
	result, err := h.Pool.Exec(r.Context(),
		`UPDATE senderzz_portal_webhooks
		    SET url = '', active = false, updated_at = NOW()
		  WHERE id = $1 AND user_id = $2`,
		webhookID, u.ID,
	)
	if err != nil {
		slog.Error("[portal_webhooks] erro ao deletar webhook", "user_id", u.ID, "webhook_id", webhookID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	if result.RowsAffected() == 0 {
		httpx.WriteErr(w, http.StatusNotFound, "webhook não encontrado")
		return
	}

	slog.Info("[portal_webhooks] soft-delete aplicado", "user_id", u.ID, "webhook_id", webhookID)
	httpx.WriteOK(w, map[string]any{"mensagem": "webhook removido com sucesso"})
}

// ── GET /portal/webhooks/{id}/history ────────────────────────────────────────

// History retorna o histórico de disparos do webhook.
func (h *WebhookHandler) History(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	idStr := chi.URLParam(r, "id")
	webhookID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || webhookID <= 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}

	// Garante que o webhook pertence ao usuário antes de retornar o histórico.
	var ownerID int64
	errOwner := h.Pool.QueryRow(r.Context(),
		`SELECT user_id FROM senderzz_portal_webhooks WHERE id = $1`,
		webhookID,
	).Scan(&ownerID)
	if errOwner == pgx.ErrNoRows {
		httpx.WriteErr(w, http.StatusNotFound, "webhook não encontrado")
		return
	}
	if errOwner != nil {
		slog.Error("[portal_webhooks] erro ao verificar ownership", "user_id", u.ID, "webhook_id", webhookID, "err", errOwner)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	if ownerID != u.ID {
		httpx.WriteErr(w, http.StatusNotFound, "webhook não encontrado")
		return
	}

	limit := 50
	rows, err := h.Pool.Query(r.Context(),
		`SELECT id, event_type, response_code, response_body, created_at
		   FROM senderzz_webhook_log
		  WHERE webhook_id = $1
		  ORDER BY created_at DESC
		  LIMIT $2`,
		webhookID, limit,
	)
	if err != nil {
		slog.Error("[portal_webhooks] erro ao buscar histórico", "webhook_id", webhookID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer rows.Close()

	var entries []webhookLogEntry
	for rows.Next() {
		var e webhookLogEntry
		if err := rows.Scan(&e.ID, &e.EventType, &e.ResponseCode, &e.ResponseBody, &e.CreatedAt); err != nil {
			slog.Error("[portal_webhooks] erro ao ler log", "webhook_id", webhookID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao ler histórico")
			return
		}
		entries = append(entries, e)
	}

	if entries == nil {
		entries = []webhookLogEntry{}
	}

	httpx.WriteOK(w, map[string]any{"data": entries, "total": len(entries)})
}

// ── POST /portal/webhooks/clear-history ──────────────────────────────────────

// ClearHistory deleta todos os logs dos webhooks do usuário autenticado.
func (h *WebhookHandler) ClearHistory(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	// Body pode conter webhook_id opcional para limpar só um webhook.
	var body struct {
		WebhookID *int64 `json:"webhook_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	var deleted int64

	if body.WebhookID != nil {
		// Garante que o webhook pertence ao usuário.
		var ownerID int64
		errScan := h.Pool.QueryRow(r.Context(),
			`SELECT user_id FROM senderzz_portal_webhooks WHERE id = $1`,
			*body.WebhookID,
		).Scan(&ownerID)
		if errScan == pgx.ErrNoRows || ownerID != u.ID {
			httpx.WriteErr(w, http.StatusNotFound, "webhook não encontrado")
			return
		}
		if errScan != nil {
			slog.Error("[portal_webhooks] erro ao verificar ownership do webhook", "user_id", u.ID, "err", errScan)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
			return
		}

		result, err2 := h.Pool.Exec(r.Context(),
			`DELETE FROM senderzz_webhook_log WHERE webhook_id = $1`,
			*body.WebhookID,
		)
		if err2 != nil {
			slog.Error("[portal_webhooks] erro ao limpar log por webhook_id", "user_id", u.ID, "err", err2)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
			return
		}
		deleted = result.RowsAffected()
	} else {
		// Limpa todos os logs dos webhooks do usuário.
		result, err2 := h.Pool.Exec(r.Context(),
			`DELETE FROM senderzz_webhook_log
			  WHERE webhook_id IN (
			      SELECT id FROM senderzz_portal_webhooks WHERE user_id = $1
			  )`,
			u.ID,
		)
		if err2 != nil {
			slog.Error("[portal_webhooks] erro ao limpar todos os logs", "user_id", u.ID, "err", err2)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
			return
		}
		deleted = result.RowsAffected()
	}

	slog.Info("[portal_webhooks] histórico limpo", "user_id", u.ID, "registros_deletados", deleted)
	httpx.WriteOK(w, map[string]any{"registros_deletados": deleted})
}
