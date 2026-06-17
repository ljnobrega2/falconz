// Package handlers — endpoint admin para o Modo de Manutenção.
// Espelha includes/senderzz-maintenance.php (PHP legado).
// Persiste em senderzz_options (key=senderzz_maintenance_settings, value=JSON).
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

// MaintenanceHandler gerencia leitura, escrita e preview do modo manutenção.
type MaintenanceHandler struct{ Pool *pgxpool.Pool }

// maintenanceOptionKey nome da option em senderzz_options.
const maintenanceOptionKey = "senderzz_maintenance_settings"

// Defaults espelham PHP senderzz_maintenance_defaults().
const (
	defaultTitle   = "Estamos ajustando a operação"
	defaultMessage = "A plataforma Senderzz está temporariamente em manutenção para melhorias operacionais. Voltaremos em breve."
)

// MaintenanceSettings shape persistida/retornada.
type MaintenanceSettings struct {
	Enabled    bool   `json:"enabled"`
	ReturnDate string `json:"return_date"` // YYYY-MM-DD ou ""
	ReturnTime string `json:"return_time"` // HH:MM ou ""
	Title      string `json:"title"`
	Message    string `json:"message"`
}

// ----- helpers -----------------------------------------------------------

// tableExists verifica presença de uma tabela no schema public.
func (h *MaintenanceHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// defaults retorna settings com valores default (manutenção desativada).
func maintenanceDefaults() MaintenanceSettings {
	return MaintenanceSettings{
		Enabled:    false,
		ReturnDate: "",
		ReturnTime: "",
		Title:      defaultTitle,
		Message:    defaultMessage,
	}
}

// readSettings lê option JSON; se ausente/inválida, devolve defaults.
// Graceful degradation: tabela ausente → defaults sem erro.
func (h *MaintenanceHandler) readSettings(ctx context.Context) MaintenanceSettings {
	out := maintenanceDefaults()
	if !h.tableExists(ctx, "senderzz_options") {
		return out
	}
	var raw string
	err := h.Pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE "key"=$1`, maintenanceOptionKey).Scan(&raw)
	if err != nil || strings.TrimSpace(raw) == "" {
		return out
	}

	// O option pode estar gravado como objeto direto ou como wrapper PHP-style;
	// tentamos JSON puro primeiro. Em paridade com PHP usamos campos snake_case.
	type rawShape struct {
		Enabled    any    `json:"enabled"`
		ReturnDate string `json:"return_date"`
		ReturnTime string `json:"return_time"`
		Title      string `json:"title"`
		Message    string `json:"message"`
	}
	var rs rawShape
	if err := json.Unmarshal([]byte(raw), &rs); err != nil {
		return out
	}

	// enabled: aceita bool, "1"/"0", "true"/"false".
	switch v := rs.Enabled.(type) {
	case bool:
		out.Enabled = v
	case string:
		s := strings.ToLower(strings.TrimSpace(v))
		out.Enabled = (s == "1" || s == "true" || s == "yes" || s == "on")
	case float64:
		out.Enabled = v != 0
	}
	out.ReturnDate = strings.TrimSpace(rs.ReturnDate)
	out.ReturnTime = strings.TrimSpace(rs.ReturnTime)
	if t := strings.TrimSpace(rs.Title); t != "" {
		out.Title = t
	}
	if m := strings.TrimSpace(rs.Message); m != "" {
		out.Message = m
	}
	return out
}

// upsertSettings grava option como JSON (UPSERT).
func (h *MaintenanceHandler) upsertSettings(ctx context.Context, s MaintenanceSettings) error {
	if !h.tableExists(ctx, "senderzz_options") {
		// Graceful degradation: sem tabela, simplesmente não persiste mas não falha.
		return nil
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, err = h.Pool.Exec(ctx,
		`INSERT INTO senderzz_options ("key", value)
		 VALUES ($1, $2)
		 ON CONFLICT ("key") DO UPDATE SET value = EXCLUDED.value`,
		maintenanceOptionKey, string(b))
	return err
}

// ----- validação ---------------------------------------------------------

var (
	reDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	reTime = regexp.MustCompile(`^\d{2}:\d{2}$`)
)

// validateSettings aplica regras: title ≤90 chars, date YYYY-MM-DD, time HH:MM.
// Strings vazias são aceitas (significam "sem data/hora prevista").
func validateSettings(s *MaintenanceSettings) (string, bool) {
	s.Title = strings.TrimSpace(s.Title)
	s.Message = strings.TrimSpace(s.Message)
	s.ReturnDate = strings.TrimSpace(s.ReturnDate)
	s.ReturnTime = strings.TrimSpace(s.ReturnTime)

	if len([]rune(s.Title)) > 90 {
		return "title excede 90 caracteres", false
	}
	if s.ReturnDate != "" && !reDate.MatchString(s.ReturnDate) {
		return "return_date inválida (use YYYY-MM-DD)", false
	}
	if s.ReturnTime != "" && !reTime.MatchString(s.ReturnTime) {
		return "return_time inválida (use HH:MM)", false
	}
	// Paridade com PHP: vazio cai para defaults.
	if s.Title == "" {
		s.Title = defaultTitle
	}
	if s.Message == "" {
		s.Message = defaultMessage
	}
	return "", true
}

// ----- endpoints ---------------------------------------------------------

// GET /maintenance — settings persistidas (ou defaults se ausente).
func (h *MaintenanceHandler) Get(w http.ResponseWriter, r *http.Request) {
	out := h.readSettings(r.Context())
	httpx.JSON(w, http.StatusOK, out)
}

// POST /maintenance — grava settings após validação.
//
// Body aceita o mesmo shape de MaintenanceSettings.
// "enabled" aceita bool, "1"/"0", "true"/"false".
func (h *MaintenanceHandler) Save(w http.ResponseWriter, r *http.Request) {
	// Lemos como rawInput porque "enabled" pode chegar como bool ou string.
	type rawInput struct {
		Enabled    any    `json:"enabled"`
		ReturnDate string `json:"return_date"`
		ReturnTime string `json:"return_time"`
		Title      string `json:"title"`
		Message    string `json:"message"`
	}
	var in rawInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.Err(w, http.StatusBadRequest, "bad_request", "json inválido")
		return
	}

	s := MaintenanceSettings{
		ReturnDate: in.ReturnDate,
		ReturnTime: in.ReturnTime,
		Title:      in.Title,
		Message:    in.Message,
	}
	// Normaliza enabled vindo do JS (bool, "1", "true"...).
	switch v := in.Enabled.(type) {
	case bool:
		s.Enabled = v
	case string:
		x := strings.ToLower(strings.TrimSpace(v))
		s.Enabled = (x == "1" || x == "true" || x == "yes" || x == "on")
	case float64:
		s.Enabled = v != 0
	case nil:
		s.Enabled = false
	}

	if msg, ok := validateSettings(&s); !ok {
		httpx.Err(w, http.StatusBadRequest, "validation_error", msg)
		return
	}

	if err := h.upsertSettings(r.Context(), s); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "settings": s})
}

// GET /maintenance/preview-data — sempre devolve shape "pronto para render".
//
// Mesma semântica do GET, mas título/mensagem vazios são substituídos pelos
// defaults — facilita renderização live no front-end durante edição.
func (h *MaintenanceHandler) PreviewData(w http.ResponseWriter, r *http.Request) {
	out := h.readSettings(r.Context())
	if strings.TrimSpace(out.Title) == "" {
		out.Title = defaultTitle
	}
	if strings.TrimSpace(out.Message) == "" {
		out.Message = defaultMessage
	}
	httpx.JSON(w, http.StatusOK, out)
}
