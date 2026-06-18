// Package handlers — handlers de configurações do Portal V2.
//
// Rotas (namespace /wp-json/senderzz/v1):
//   GET  /portal/settings       — retorna configurações do usuário
//   POST /portal/settings       — atualiza configurações
//   POST /portal/settings/2fa   — ativa/desativa 2FA por e-mail
//
// Configurações armazenadas em senderzz_portal_users.settings (JSONB):
//   {
//     "pix_key":          string,   // chave PIX (CPF, CNPJ, e-mail, telefone, aleatória)
//     "pix_key_tipo":     string,   // cpf | cnpj | email | telefone | aleatoria
//     "notify_email":     bool,     // receber notificações por e-mail
//     "notify_whatsapp":  bool      // receber notificações por WhatsApp
//   }
//
// shipping_class_id é armazenado na coluna dedicada (não no JSONB de settings)
// pois é referenciada por outros serviços (motoboy, session.go).
package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/portal-service/internal/auth"
	"github.com/senderzz/portal-service/internal/httpx"
)

// SettingsHandler agrupa dependências dos handlers de configuração.
type SettingsHandler struct {
	Pool *pgxpool.Pool
}

// updateSettingsRequest é o body de POST /portal/settings.
type updateSettingsRequest struct {
	PIXKey          *string `json:"pix_key"`
	PIXKeyTipo      *string `json:"pix_key_tipo"`
	NotifyEmail     *bool   `json:"notify_email"`
	NotifyWhatsApp  *bool   `json:"notify_whatsapp"`
	ShippingClassID *int64  `json:"shipping_class_id"`
}

// twoFAToggleRequest é o body de POST /portal/settings/2fa.
type twoFAToggleRequest struct {
	Enabled bool `json:"enabled"`
}

// ── GET /portal/settings ──────────────────────────────────────────────────────

// Get retorna as configurações do usuário autenticado.
func (h *SettingsHandler) Get(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	var settingsRaw []byte
	var shippingClassID *int64
	var twofaEnabled bool

	err := h.Pool.QueryRow(r.Context(),
		`SELECT settings, shipping_class_id, twofa_enabled
		   FROM senderzz_portal_users
		  WHERE id = $1`,
		u.ID,
	).Scan(&settingsRaw, &shippingClassID, &twofaEnabled)
	if err != nil {
		slog.Error("[portal_settings] erro ao buscar configurações", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	httpx.WriteOK(w, map[string]any{
		"settings":          json.RawMessage(settingsRaw),
		"shipping_class_id": shippingClassID,
		"twofa_enabled":     twofaEnabled,
	})
}

// ── POST /portal/settings ─────────────────────────────────────────────────────

// Update atualiza as configurações do usuário autenticado.
// Campos no body são todos opcionais — apenas os enviados são atualizados.
func (h *SettingsHandler) Update(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	var req updateSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}

	// Atualiza shipping_class_id na coluna dedicada (referenciada por outros serviços).
	if req.ShippingClassID != nil {
		_, err := h.Pool.Exec(r.Context(),
			`UPDATE senderzz_portal_users SET shipping_class_id = $1 WHERE id = $2`,
			req.ShippingClassID, u.ID,
		)
		if err != nil {
			slog.Error("[portal_settings] erro ao atualizar shipping_class_id", "user_id", u.ID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
			return
		}
	}

	// Atualiza campos do JSONB settings individualmente via jsonb_set.
	// Isso preserva campos existentes não enviados no request.
	type settingsField struct {
		key   string
		value any
	}

	fields := make([]settingsField, 0, 4)
	if req.PIXKey != nil {
		fields = append(fields, settingsField{key: "pix_key", value: *req.PIXKey})
	}
	if req.PIXKeyTipo != nil {
		fields = append(fields, settingsField{key: "pix_key_tipo", value: *req.PIXKeyTipo})
	}
	if req.NotifyEmail != nil {
		fields = append(fields, settingsField{key: "notify_email", value: *req.NotifyEmail})
	}
	if req.NotifyWhatsApp != nil {
		fields = append(fields, settingsField{key: "notify_whatsapp", value: *req.NotifyWhatsApp})
	}

	for _, f := range fields {
		valueJSON, err := json.Marshal(f.value)
		if err != nil {
			continue
		}
		_, err = h.Pool.Exec(r.Context(),
			`UPDATE senderzz_portal_users
			    SET settings = jsonb_set(
			            COALESCE(settings, '{}'),
			            $1::text[],
			            $2::jsonb,
			            true
			        )
			  WHERE id = $3`,
			[]string{f.key}, valueJSON, u.ID,
		)
		if err != nil {
			slog.Error("[portal_settings] erro ao atualizar campo de settings",
				"user_id", u.ID, "field", f.key, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
			return
		}
	}

	slog.Info("[portal_settings] configurações atualizadas", "user_id", u.ID)
	httpx.WriteOK(w, map[string]any{"mensagem": "configurações atualizadas com sucesso"})
}

// ── POST /portal/settings/2fa ─────────────────────────────────────────────────

// Toggle2FA ativa ou desativa o 2FA por e-mail para o usuário autenticado.
// Quando desativado, remove qualquer código 2FA pendente.
func (h *SettingsHandler) Toggle2FA(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	var req twoFAToggleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}

	_, err := h.Pool.Exec(r.Context(),
		`UPDATE senderzz_portal_users SET twofa_enabled = $1 WHERE id = $2`,
		req.Enabled, u.ID,
	)
	if err != nil {
		slog.Error("[portal_settings] erro ao atualizar twofa_enabled", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Se desativando, remove código 2FA pendente para não deixar lixo no banco.
	if !req.Enabled {
		_, _ = h.Pool.Exec(r.Context(),
			`DELETE FROM senderzz_portal_2fa WHERE user_id = $1`,
			u.ID,
		)
	}

	status := "desativado"
	if req.Enabled {
		status = "ativado"
	}

	slog.Info("[portal_settings] 2FA atualizado", "user_id", u.ID, "enabled", req.Enabled)
	httpx.WriteOK(w, map[string]any{
		"twofa_enabled": req.Enabled,
		"mensagem":      "2FA " + status + " com sucesso",
	})
}
