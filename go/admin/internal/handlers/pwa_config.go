// Package handlers — endpoint admin para configuração do PWA (Progressive Web App).
// Espelha includes/senderzz-app-pwa.php (manifest, service worker, ícones).
// Persiste configurações em senderzz_options com prefixo sz_pwa_*.
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

// PwaConfigHandler gerencia leitura e escrita das configurações do PWA.
type PwaConfigHandler struct{ Pool *pgxpool.Pool }

// Chaves de option em senderzz_options.
const (
	pwaOptionAppName         = "sz_pwa_app_name"
	pwaOptionShortName       = "sz_pwa_short_name"
	pwaOptionThemeColor      = "sz_pwa_theme_color"
	pwaOptionBgColor         = "sz_pwa_background_color"
	pwaOptionIcon192         = "sz_pwa_icon_192"
	pwaOptionIcon512         = "sz_pwa_icon_512"
	pwaOptionSwCacheVersion  = "sz_pwa_sw_cache_version"
	pwaOptionDisplay         = "sz_pwa_display"
	pwaOptionStartURL        = "sz_pwa_start_url"
)

// Valores padrão espelhando senderzz-app-pwa.php.
const (
	pwaDefaultAppName        = "Senderzz"
	pwaDefaultShortName      = "SZ"
	pwaDefaultThemeColor     = "#f3f8fb"
	pwaDefaultBgColor        = "#f3f8fb"
	pwaDefaultIcon192        = "/wp-content/plugins/senderzz-logistics/assets/pwa-icon-192.png"
	pwaDefaultIcon512        = "/wp-content/plugins/senderzz-logistics/assets/pwa-icon-512.png"
	pwaDefaultSwCacheVersion = "app250"
	pwaDefaultDisplay        = "standalone"
	pwaDefaultStartURL       = "/app/"
	pwaDefaultAppURL         = "/app/"
	pwaDefaultSwURL          = "/app-sw.js"
	pwaDefaultManifestURL    = "/app-manifest.json"
)

// reHexColor valida cor hexadecimal (#RGB ou #RRGGBB).
var reHexColor = regexp.MustCompile(`^#([0-9A-Fa-f]{3}|[0-9A-Fa-f]{6})$`)

// displayModes lista de valores válidos para display.
var displayModes = map[string]bool{
	"standalone": true,
	"browser":    true,
	"minimal-ui": true,
	"fullscreen": true,
}

// PwaConfig shape completo retornado/persistido.
type PwaConfig struct {
	AppName        string `json:"app_name"`
	ShortName      string `json:"short_name"`
	StartURL       string `json:"start_url"`
	Display        string `json:"display"`
	ThemeColor     string `json:"theme_color"`
	BgColor        string `json:"background_color"`
	Icon192        string `json:"icon_192"`
	Icon512        string `json:"icon_512"`
	SwCacheVersion string `json:"sw_cache_version"`
	// URLs computadas — somente leitura, não persistidas.
	AppURL      string `json:"app_url"`
	SwURL       string `json:"sw_url"`
	ManifestURL string `json:"manifest_url"`
}

// ----- helpers -----------------------------------------------------------

// tableExistsPwa verifica presença de uma tabela no schema public.
func (h *PwaConfigHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// readPwaOption lê valor único de senderzz_options; retorna fallback se ausente.
func (h *PwaConfigHandler) readPwaOption(ctx context.Context, key, fallback string) string {
	if !h.tableExists(ctx, "senderzz_options") {
		return fallback
	}
	var raw string
	err := h.Pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE "key"=$1`, key).Scan(&raw)
	raw = strings.TrimSpace(raw)
	if err != nil || raw == "" || raw == `""` {
		return fallback
	}
	// Valor pode estar gravado como JSON string com aspas.
	var s string
	if err := json.Unmarshal([]byte(raw), &s); err == nil {
		return s
	}
	return raw
}

// upsertPwaOption grava/atualiza um par key/value em senderzz_options.
func (h *PwaConfigHandler) upsertPwaOption(ctx context.Context, key, value string) error {
	if !h.tableExists(ctx, "senderzz_options") {
		return nil
	}
	b, _ := json.Marshal(value)
	_, err := h.Pool.Exec(ctx,
		`INSERT INTO senderzz_options ("key", value)
		 VALUES ($1, $2)
		 ON CONFLICT ("key") DO UPDATE SET value = EXCLUDED.value`,
		key, string(b))
	return err
}

// loadConfig lê todas as options e retorna PwaConfig com defaults.
func (h *PwaConfigHandler) loadConfig(ctx context.Context) PwaConfig {
	return PwaConfig{
		AppName:        h.readPwaOption(ctx, pwaOptionAppName, pwaDefaultAppName),
		ShortName:      h.readPwaOption(ctx, pwaOptionShortName, pwaDefaultShortName),
		StartURL:       h.readPwaOption(ctx, pwaOptionStartURL, pwaDefaultStartURL),
		Display:        h.readPwaOption(ctx, pwaOptionDisplay, pwaDefaultDisplay),
		ThemeColor:     h.readPwaOption(ctx, pwaOptionThemeColor, pwaDefaultThemeColor),
		BgColor:        h.readPwaOption(ctx, pwaOptionBgColor, pwaDefaultBgColor),
		Icon192:        h.readPwaOption(ctx, pwaOptionIcon192, pwaDefaultIcon192),
		Icon512:        h.readPwaOption(ctx, pwaOptionIcon512, pwaDefaultIcon512),
		SwCacheVersion: h.readPwaOption(ctx, pwaOptionSwCacheVersion, pwaDefaultSwCacheVersion),
		// URLs computadas: estáticas (controladas pelo WP/nginx, não pelo admin Go).
		AppURL:      pwaDefaultAppURL,
		SwURL:       pwaDefaultSwURL,
		ManifestURL: pwaDefaultManifestURL,
	}
}

// validatePwaInput valida os campos editáveis.
func validatePwaInput(c *PwaConfig) (string, bool) {
	c.AppName = strings.TrimSpace(c.AppName)
	c.ShortName = strings.TrimSpace(c.ShortName)
	c.ThemeColor = strings.TrimSpace(c.ThemeColor)
	c.BgColor = strings.TrimSpace(c.BgColor)
	c.Icon192 = strings.TrimSpace(c.Icon192)
	c.Icon512 = strings.TrimSpace(c.Icon512)
	c.SwCacheVersion = strings.TrimSpace(c.SwCacheVersion)
	c.Display = strings.TrimSpace(c.Display)

	if c.AppName == "" {
		c.AppName = pwaDefaultAppName
	}
	if c.ShortName == "" {
		c.ShortName = pwaDefaultShortName
	}
	if len([]rune(c.ShortName)) > 12 {
		return "short_name excede 12 caracteres", false
	}
	if c.ThemeColor != "" && !reHexColor.MatchString(c.ThemeColor) {
		return fmt.Sprintf("theme_color inválida (use hex #RGB ou #RRGGBB): %s", c.ThemeColor), false
	}
	if c.BgColor != "" && !reHexColor.MatchString(c.BgColor) {
		return fmt.Sprintf("background_color inválida (use hex #RGB ou #RRGGBB): %s", c.BgColor), false
	}
	if c.Display != "" && !displayModes[c.Display] {
		return fmt.Sprintf("display inválido: %s (opções: standalone, browser, minimal-ui, fullscreen)", c.Display), false
	}
	if c.ThemeColor == "" {
		c.ThemeColor = pwaDefaultThemeColor
	}
	if c.BgColor == "" {
		c.BgColor = pwaDefaultBgColor
	}
	if c.Display == "" {
		c.Display = pwaDefaultDisplay
	}
	if c.SwCacheVersion == "" {
		c.SwCacheVersion = pwaDefaultSwCacheVersion
	}
	return "", true
}

// ----- endpoints ---------------------------------------------------------

// GET /pwa-config
// Retorna configuração atual do PWA com defaults aplicados.
func (h *PwaConfigHandler) Get(w http.ResponseWriter, r *http.Request) {
	cfg := h.loadConfig(r.Context())
	httpx.JSON(w, http.StatusOK, cfg)
}

// POST /pwa-config
// Persiste campos editáveis do PWA. URLs computadas são ignoradas.
func (h *PwaConfigHandler) Save(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var in PwaConfig
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.Err(w, http.StatusBadRequest, "bad_request", "json inválido")
		return
	}

	if msg, ok := validatePwaInput(&in); !ok {
		httpx.Err(w, http.StatusBadRequest, "validation_error", msg)
		return
	}

	// Persiste cada campo individualmente.
	pairs := map[string]string{
		pwaOptionAppName:        in.AppName,
		pwaOptionShortName:      in.ShortName,
		pwaOptionThemeColor:     in.ThemeColor,
		pwaOptionBgColor:        in.BgColor,
		pwaOptionIcon192:        in.Icon192,
		pwaOptionIcon512:        in.Icon512,
		pwaOptionSwCacheVersion: in.SwCacheVersion,
		pwaOptionDisplay:        in.Display,
	}
	for key, val := range pairs {
		if err := h.upsertPwaOption(ctx, key, val); err != nil {
			httpx.Err(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
	}

	// Retorna configuração completa gravada.
	cfg := h.loadConfig(ctx)
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "config": cfg})
}

// GET /pwa-config/test-manifest
// Busca /app-manifest.json na APP_BASE_URL e retorna o JSON parsado.
// Retorna {ok, manifest, error} — nunca falha com status 5xx.
func (h *PwaConfigHandler) TestManifest(w http.ResponseWriter, r *http.Request) {
	baseURL := strings.TrimRight(os.Getenv("APP_BASE_URL"), "/")
	if baseURL == "" {
		httpx.JSON(w, http.StatusOK, map[string]any{
			"ok":       false,
			"manifest": nil,
			"error":    "APP_BASE_URL não configurada",
		})
		return
	}

	manifestURL, err := url.JoinPath(baseURL, pwaDefaultManifestURL)
	if err != nil {
		httpx.JSON(w, http.StatusOK, map[string]any{
			"ok":       false,
			"manifest": nil,
			"error":    fmt.Sprintf("URL inválida: %v", err),
		})
		return
	}

	resp, err := http.Get(manifestURL) //nolint:gosec // URL construída internamente
	if err != nil {
		httpx.JSON(w, http.StatusOK, map[string]any{
			"ok":       false,
			"manifest": nil,
			"error":    fmt.Sprintf("falha ao buscar manifest: %v", err),
		})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024)) // limite 512 KB
	if err != nil {
		httpx.JSON(w, http.StatusOK, map[string]any{
			"ok":       false,
			"manifest": nil,
			"error":    fmt.Sprintf("falha ao ler resposta: %v", err),
		})
		return
	}

	if resp.StatusCode != http.StatusOK {
		httpx.JSON(w, http.StatusOK, map[string]any{
			"ok":       false,
			"manifest": nil,
			"error":    fmt.Sprintf("manifest retornou HTTP %d", resp.StatusCode),
		})
		return
	}

	var manifest any
	if err := json.Unmarshal(body, &manifest); err != nil {
		httpx.JSON(w, http.StatusOK, map[string]any{
			"ok":       false,
			"manifest": nil,
			"error":    fmt.Sprintf("manifest não é JSON válido: %v", err),
		})
		return
	}

	httpx.JSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"manifest": manifest,
		"error":    nil,
	})
}
