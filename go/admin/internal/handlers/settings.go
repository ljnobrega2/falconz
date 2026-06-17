// Package handlers — endpoint admin para configurações gerais do sistema.
// Espelha tab_config_geral() / opções dispersas no admin WP legado.
// Persiste em senderzz_options (key/value), com algumas chaves também
// legíveis via variáveis de ambiente (prevalecem sobre o banco).
package handlers

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

type SettingsHandler struct{ Pool *pgxpool.Pool }

// ----- helpers -----------------------------------------------------------

func (h *SettingsHandler) optTableExists(ctx context.Context) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name='senderzz_options'
		)`).Scan(&ok)
	return ok
}

func (h *SettingsHandler) getOpt(ctx context.Context, key, def string) string {
	if !h.optTableExists(ctx) {
		return def
	}
	var v string
	if err := h.Pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE "key"=$1`, key).Scan(&v); err != nil {
		return def
	}
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func (h *SettingsHandler) upsertOpt(ctx context.Context, key, value string) error {
	if !h.optTableExists(ctx) {
		return nil
	}
	_, err := h.Pool.Exec(ctx,
		`INSERT INTO senderzz_options ("key", value)
		 VALUES ($1, $2)
		 ON CONFLICT ("key") DO UPDATE SET value = EXCLUDED.value`, key, value)
	return err
}

func settingsMaskSecret(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) <= 4 {
		return "•••"
	}
	return "••• " + s[len(s)-4:]
}

// ----- payload -----------------------------------------------------------

type settingsResp struct {
	MeToken            string  `json:"me_token"`
	MeTokenFromEnv     bool    `json:"me_token_from_env"`
	PixKey             string  `json:"pix_key"`
	PixKeyType         string  `json:"pix_key_type"`
	WebhookSecretHint  string  `json:"webhook_secret_hint"`
	JwtSecretHint      string  `json:"jwt_secret_hint"`
	MotoboyccFeePct    float64 `json:"motoboy_cc_fee_pct"`
	PortalName         string  `json:"portal_name"`
}

type settingsSave struct {
	MeToken         *string  `json:"me_token"`
	PixKey          *string  `json:"pix_key"`
	PixKeyType      *string  `json:"pix_key_type"`
	MotoboyccFeePct *float64 `json:"motoboy_cc_fee_pct"`
	PortalName      *string  `json:"portal_name"`
}

// ----- GET /settings -----------------------------------------------------

func (h *SettingsHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tokenFromEnv := strings.TrimSpace(os.Getenv("SENDERZZ_ME_TOKEN")) != ""
	rawToken := h.getOpt(ctx, "tpc_me_token", "")
	if tokenFromEnv {
		rawToken = os.Getenv("SENDERZZ_ME_TOKEN")
	}

	rawWH := h.getOpt(ctx, "tpc_webhook_secret", "")
	if strings.TrimSpace(os.Getenv("SENDERZZ_WEBHOOK_SECRET")) != "" {
		rawWH = os.Getenv("SENDERZZ_WEBHOOK_SECRET")
	}
	rawJWT := h.getOpt(ctx, "tpc_jwt_secret", "")
	if strings.TrimSpace(os.Getenv("SENDERZZ_JWT_SECRET")) != "" {
		rawJWT = os.Getenv("SENDERZZ_JWT_SECRET")
	}

	ccFee := 0.0
	if raw := h.getOpt(ctx, "sz_motoboy_cc_fee_pct", ""); raw != "" {
		v, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", "."), 64)
		if err == nil {
			ccFee = v
		}
	}

	httpx.JSON(w, 200, settingsResp{
		MeToken:           settingsMaskSecret(rawToken),
		MeTokenFromEnv:    tokenFromEnv,
		PixKey:            h.getOpt(ctx, "tpc_pix_key", ""),
		PixKeyType:        h.getOpt(ctx, "tpc_pix_key_type", "cpf"),
		WebhookSecretHint: settingsMaskSecret(rawWH),
		JwtSecretHint:     settingsMaskSecret(rawJWT),
		MotoboyccFeePct:   ccFee,
		PortalName:        h.getOpt(ctx, "senderzz_portal_name", "Senderzz"),
	})
}

// ----- PUT /settings -----------------------------------------------------

func (h *SettingsHandler) Save(w http.ResponseWriter, r *http.Request) {
	var in settingsSave
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	ctx := r.Context()

	if in.MeToken != nil && strings.TrimSpace(os.Getenv("SENDERZZ_ME_TOKEN")) == "" {
		if err := h.upsertOpt(ctx, "tpc_me_token", strings.TrimSpace(*in.MeToken)); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
	}
	if in.PixKey != nil {
		if err := h.upsertOpt(ctx, "tpc_pix_key", strings.TrimSpace(*in.PixKey)); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
	}
	if in.PixKeyType != nil {
		allowed := map[string]bool{"cpf": true, "cnpj": true, "email": true, "phone": true, "random": true}
		t := strings.TrimSpace(*in.PixKeyType)
		if !allowed[t] {
			httpx.Err(w, 400, "invalid_pix_key_type", "tipo de chave inválido")
			return
		}
		if err := h.upsertOpt(ctx, "tpc_pix_key_type", t); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
	}
	if in.MotoboyccFeePct != nil {
		v := *in.MotoboyccFeePct
		if v < 0 {
			v = 0
		}
		if v > 30 {
			v = 30
		}
		if err := h.upsertOpt(ctx, "sz_motoboy_cc_fee_pct", strconv.FormatFloat(v, 'f', 4, 64)); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
	}
	if in.PortalName != nil {
		name := strings.TrimSpace(*in.PortalName)
		if name == "" {
			name = "Senderzz"
		}
		if err := h.upsertOpt(ctx, "senderzz_portal_name", name); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
	}

	h.Get(w, r)
}
