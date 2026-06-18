// Package handlers — handlers de integrações do Portal V2.
//
// Rotas (namespace /wp-json/senderzz/v1):
//   GET    /portal/integrations       — retorna flags de integração do usuário
//   POST   /portal/integrations/toggle — alterna uma flag (whitelist de chaves)
//   DELETE /portal/integrations/logs  — limpa log de integração do usuário
//
// As flags de integração são armazenadas em senderzz_portal_users.integrations (JSONB).
// Schema esperado: {active, paused, require_paid, ignore_duplicates, auto_cheapest}
//
// Whitelist de chaves permitidas (espelha Portal_Page.php integrations_toggle):
//   active, paused, require_paid, ignore_duplicates, auto_cheapest
//
// Nota "Pausar recebimento":
//   O toggle de pausa envia key=active com valor INVERTIDO no PHP:
//   checked → valor 0 (pausa), unchecked → valor 1 (ativa).
//   O handler Go aceita qualquer combinação key+value — a lógica de inversão
//   fica no frontend (espelha comportamento PHP).
package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/portal-service/internal/auth"
	"github.com/senderzz/portal-service/internal/httpx"
)

// IntegrationsHandler agrupa dependências dos handlers de integração.
type IntegrationsHandler struct {
	Pool *pgxpool.Pool
}

// allowedIntegrationKeys — whitelist de chaves de integração permitidas.
// Espelha Portal_Page.php integrations_toggle: allowed = ['active','paused',
// 'require_paid','ignore_duplicates','auto_cheapest'].
var allowedIntegrationKeys = map[string]bool{
	"active":             true,
	"paused":             true,
	"require_paid":       true,
	"ignore_duplicates":  true,
	"auto_cheapest":      true,
}

// integrationToggleRequest é o body de POST /portal/integrations/toggle.
type integrationToggleRequest struct {
	Key   string `json:"key"`
	Value any    `json:"value"` // bool ou int (0/1 do PHP)
}

// ── GET /portal/integrations ──────────────────────────────────────────────────

// List retorna as flags de integração do usuário autenticado.
func (h *IntegrationsHandler) List(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	var integrationsRaw []byte
	err := h.Pool.QueryRow(r.Context(),
		`SELECT integrations FROM senderzz_portal_users WHERE id = $1`,
		u.ID,
	).Scan(&integrationsRaw)
	if err != nil {
		slog.Error("[portal_integrations] erro ao buscar integrations", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	httpx.WriteOK(w, map[string]any{
		"integrations": json.RawMessage(integrationsRaw),
	})
}

// ── POST /portal/integrations/toggle ─────────────────────────────────────────

// Toggle alterna uma flag de integração do usuário.
// Rejeita chaves não whitelistadas para evitar escrita arbitrária no JSON.
func (h *IntegrationsHandler) Toggle(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	var req integrationToggleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}
	if req.Key == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "key é obrigatória")
		return
	}

	// Whitelist — rejeita chaves não permitidas.
	if !allowedIntegrationKeys[req.Key] {
		httpx.WriteErr(w, http.StatusBadRequest,
			"chave inválida: "+req.Key+
				" — permitidas: active, paused, require_paid, ignore_duplicates, auto_cheapest")
		return
	}

	// Atualiza apenas a chave específica no JSONB usando jsonb_set.
	// Serializa o value em JSON para injeção segura.
	valueJSON, err := json.Marshal(req.Value)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "valor inválido")
		return
	}

	_, err = h.Pool.Exec(r.Context(),
		`UPDATE senderzz_portal_users
		    SET integrations = jsonb_set(
		            COALESCE(integrations, '{}'),
		            $1::text[],
		            $2::jsonb,
		            true
		        )
		  WHERE id = $3`,
		[]string{req.Key}, // path para jsonb_set como array Postgres
		valueJSON,
		u.ID,
	)
	if err != nil {
		slog.Error("[portal_integrations] erro ao atualizar flag", "user_id", u.ID, "key", req.Key, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Registra no log de integração.
	logPayload, _ := json.Marshal(map[string]any{"key": req.Key, "value": req.Value})
	_, _ = h.Pool.Exec(r.Context(),
		`INSERT INTO senderzz_integration_log (user_id, event, payload)
		 VALUES ($1, 'integration_toggle', $2)`,
		u.ID, logPayload,
	)

	slog.Info("[portal_integrations] flag atualizada", "user_id", u.ID, "key", req.Key, "value", req.Value)
	httpx.WriteOK(w, map[string]any{"key": req.Key, "value": req.Value})
}

// ── DELETE /portal/integrations/logs ─────────────────────────────────────────

// ClearLogs deleta todos os logs de integração do usuário autenticado.
// Espelha integrations_clear_logs de Portal_Page.php.
func (h *IntegrationsHandler) ClearLogs(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	result, err := h.Pool.Exec(r.Context(),
		`DELETE FROM senderzz_integration_log WHERE user_id = $1`,
		u.ID,
	)
	if err != nil {
		slog.Error("[portal_integrations] erro ao limpar logs", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	deleted := result.RowsAffected()
	slog.Info("[portal_integrations] logs limpos", "user_id", u.ID, "registros_deletados", deleted)
	httpx.WriteOK(w, map[string]any{"registros_deletados": deleted})
}
