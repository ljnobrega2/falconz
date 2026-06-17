// Package handlers — endpoint admin para configurações operacionais do módulo Motoboy.
// Espelha sz_mb_tab_config() em includes/motoboy/admin.php:1345 (PHP legado).
// Persiste em senderzz_options (key/value). Defaults aplicados quando tabela ausente ou option inexistente.
package handlers

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

type MotoboyConfigHandler struct{ Pool *pgxpool.Pool }

// ----- defaults ---------------------------------------------------------

const (
	mbDefaultGeofence  = 500    // metros (raio padrão)
	mbDefaultInicio    = "08:00"
	mbDefaultFim       = "18:00"
	mbDefaultCcFeePct  = 0.0
	mbMinGeofence      = 50
	mbMaxGeofence      = 5000
	mbMaxCcFeePct      = 30.0
)

// Regex HH:MM 24h. Espelha validação client-side.
var mbReHora = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

// ----- helpers ----------------------------------------------------------

// tableExists verifica presença de uma tabela no schema public.
func (h *MotoboyConfigHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// getOptionString lê option string com fallback. Tabela ausente → fallback.
func (h *MotoboyConfigHandler) getOptionString(ctx context.Context, key, def string) string {
	if !h.tableExists(ctx, "senderzz_options") {
		return def
	}
	var raw string
	err := h.Pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE "key"=$1`, key).Scan(&raw)
	if err != nil {
		return def
	}
	s := strings.TrimSpace(raw)
	if s == "" {
		return def
	}
	return s
}

// getOptionInt lê option inteiro com fallback.
func (h *MotoboyConfigHandler) getOptionInt(ctx context.Context, key string, def int) int {
	raw := h.getOptionString(ctx, key, "")
	if raw == "" {
		return def
	}
	// Aceita valores em formato float (ex: "500.0000") gravados por outras handlers.
	if dot := strings.IndexByte(raw, '.'); dot > 0 {
		raw = raw[:dot]
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return n
}

// getOptionFloat lê option float com fallback. Aceita vírgula como decimal.
func (h *MotoboyConfigHandler) getOptionFloat(ctx context.Context, key string, def float64) float64 {
	raw := h.getOptionString(ctx, key, "")
	if raw == "" {
		return def
	}
	raw = strings.ReplaceAll(raw, ",", ".")
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return def
	}
	return f
}

// upsertOption grava option (UPSERT em senderzz_options).
func (h *MotoboyConfigHandler) upsertOption(ctx context.Context, key, value string) error {
	if !h.tableExists(ctx, "senderzz_options") {
		// Degradação graciosa: tabela ainda não migrada, ignora silenciosamente.
		return nil
	}
	_, err := h.Pool.Exec(ctx,
		`INSERT INTO senderzz_options ("key", value)
		 VALUES ($1, $2)
		 ON CONFLICT ("key") DO UPDATE SET value = EXCLUDED.value`, key, value)
	return err
}

// ----- payload -----------------------------------------------------------

// MotoboyConfig estrutura única lida e gravada pelo handler.
type MotoboyConfig struct {
	GeofenceMetros int     `json:"geofence_metros"`
	HorarioInicio  string  `json:"horario_inicio"`
	HorarioFim     string  `json:"horario_fim"`
	CcFeePct       float64 `json:"cc_fee_pct"`
}

// ----- GET /motoboy-config ----------------------------------------------

// Get carrega as 4 chaves do senderzz_options com defaults.
func (h *MotoboyConfigHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	out := MotoboyConfig{
		GeofenceMetros: h.getOptionInt(ctx, "sz_motoboy_geofence_metros", mbDefaultGeofence),
		HorarioInicio:  h.getOptionString(ctx, "sz_motoboy_horario_inicio", mbDefaultInicio),
		HorarioFim:     h.getOptionString(ctx, "sz_motoboy_horario_fim", mbDefaultFim),
		CcFeePct:       h.getOptionFloat(ctx, "sz_motoboy_cc_fee_pct", mbDefaultCcFeePct),
	}

	// Sanidade: se valor gravado violou range, força default (defesa em profundidade).
	if out.GeofenceMetros <= 0 {
		out.GeofenceMetros = mbDefaultGeofence
	}
	if !mbReHora.MatchString(out.HorarioInicio) {
		out.HorarioInicio = mbDefaultInicio
	}
	if !mbReHora.MatchString(out.HorarioFim) {
		out.HorarioFim = mbDefaultFim
	}
	if out.CcFeePct < 0 {
		out.CcFeePct = 0
	}
	if out.CcFeePct > mbMaxCcFeePct {
		out.CcFeePct = mbMaxCcFeePct
	}

	httpx.JSON(w, 200, out)
}

// ----- POST /motoboy-config ---------------------------------------------

// Save valida e persiste cada campo. Erros viram 400 com mensagem PT-BR.
func (h *MotoboyConfigHandler) Save(w http.ResponseWriter, r *http.Request) {
	var in MotoboyConfig
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}

	// --- validação geofence (int > 0) ---
	if in.GeofenceMetros <= 0 {
		httpx.Err(w, 400, "invalid_geofence", "geofence_metros deve ser maior que zero")
		return
	}
	// Clamp em range razoável (50..5000) para evitar lixo.
	if in.GeofenceMetros < mbMinGeofence {
		in.GeofenceMetros = mbMinGeofence
	}
	if in.GeofenceMetros > mbMaxGeofence {
		in.GeofenceMetros = mbMaxGeofence
	}

	// --- validação horários (HH:MM 24h) ---
	in.HorarioInicio = strings.TrimSpace(in.HorarioInicio)
	in.HorarioFim = strings.TrimSpace(in.HorarioFim)
	if !mbReHora.MatchString(in.HorarioInicio) {
		httpx.Err(w, 400, "invalid_horario_inicio", "horario_inicio deve estar no formato HH:MM")
		return
	}
	if !mbReHora.MatchString(in.HorarioFim) {
		httpx.Err(w, 400, "invalid_horario_fim", "horario_fim deve estar no formato HH:MM")
		return
	}

	// --- clamp taxa cartão (0..30) ---
	if in.CcFeePct < 0 {
		in.CcFeePct = 0
	}
	if in.CcFeePct > mbMaxCcFeePct {
		in.CcFeePct = mbMaxCcFeePct
	}

	ctx := r.Context()

	// UPSERT atômico por chave. Erro em qualquer um aborta com 500.
	pairs := map[string]string{
		"sz_motoboy_geofence_metros": strconv.Itoa(in.GeofenceMetros),
		"sz_motoboy_horario_inicio":  in.HorarioInicio,
		"sz_motoboy_horario_fim":     in.HorarioFim,
		"sz_motoboy_cc_fee_pct":      strconv.FormatFloat(in.CcFeePct, 'f', 4, 64),
	}
	for k, v := range pairs {
		if err := h.upsertOption(ctx, k, v); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
	}

	httpx.JSON(w, 200, map[string]any{"ok": true, "config": in})
}
