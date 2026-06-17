// Package handlers — endpoint admin para a tela "Expedição › Integrações" (Markup de frete).
// Espelha senderzz_markup_admin_page() em includes/senderzz-markup.php:149 (PHP legado).
// Persiste em senderzz_options (key/value) usando JSON para os dois pares de chaves:
//   - senderzz_markup_default → {"pct": float, "fixed": float}
//   - senderzz_markup_rules   → {"<class_id>": {"pct": float, "fixed": float}, ...}
// Fórmula de cobrança aplicada ao cliente: final = (base * (1 + pct/100)) + fixed.
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

// ExpedicaoIntegracoesHandler — handler de markup de frete por classe.
type ExpedicaoIntegracoesHandler struct{ Pool *pgxpool.Pool }

// ----- constantes / defaults --------------------------------------------

const (
	expIntOptDefault = "senderzz_markup_default"
	expIntOptRules   = "senderzz_markup_rules"

	// Limites usados na validação (espelha o PHP: pct 0-500 e fixed 0-999).
	expIntMaxPct   = 500.0
	expIntMaxFixed = 999.0
)

// ----- helpers internos -------------------------------------------------

// tableExists verifica presença de uma tabela no schema public.
func (h *ExpedicaoIntegracoesHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// getOptionRaw lê value bruto da option (string). Tabela ausente → "".
func (h *ExpedicaoIntegracoesHandler) getOptionRaw(ctx context.Context, key string) string {
	if !h.tableExists(ctx, "senderzz_options") {
		return ""
	}
	var raw string
	err := h.Pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE "key"=$1`, key).Scan(&raw)
	if err != nil {
		return ""
	}
	return raw
}

// upsertOption grava option (UPSERT em senderzz_options). Tabela ausente → no-op silencioso.
func (h *ExpedicaoIntegracoesHandler) upsertOption(ctx context.Context, key, value string) error {
	if !h.tableExists(ctx, "senderzz_options") {
		return nil
	}
	_, err := h.Pool.Exec(ctx,
		`INSERT INTO senderzz_options ("key", value)
		 VALUES ($1, $2)
		 ON CONFLICT ("key") DO UPDATE SET value = EXCLUDED.value`, key, value)
	return err
}

// parseExpIntFloat normaliza qualquer tipo JSON (number/string) em float64 >= 0.
func parseExpIntFloat(v any) float64 {
	if v == nil {
		return 0
	}
	switch x := v.(type) {
	case float64:
		return math.Max(0, x)
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return 0
		}
		return math.Max(0, f)
	case string:
		s := strings.TrimSpace(strings.ReplaceAll(x, ",", "."))
		if s == "" {
			return 0
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0
		}
		return math.Max(0, f)
	case int:
		return math.Max(0, float64(x))
	case int64:
		return math.Max(0, float64(x))
	}
	return 0
}

// ----- payloads ---------------------------------------------------------

// MarkupPair representa um par {pct, fixed} (padrão ou regra por classe).
type MarkupPair struct {
	Pct   float64 `json:"pct"`
	Fixed float64 `json:"fixed"`
}

// MarkupRule é o item de regra exposto na API (inclui nome resolvido da classe).
type MarkupRule struct {
	ClassID   int64   `json:"class_id"`
	ClassName string  `json:"class_name"`
	Pct       float64 `json:"pct"`
	Fixed     float64 `json:"fixed"`
}

// MarkupResponse shape do GET /expedicao/markup.
type MarkupResponse struct {
	Default MarkupPair   `json:"default"`
	Rules   []MarkupRule `json:"rules"`
}

// ShippingClass — item de classe de entrega (para o dropdown do modal).
type ShippingClass struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// PreviewRequest body do POST /expedicao/markup/preview.
type PreviewRequest struct {
	ClassID  int64 `json:"class_id"`
	BaseCost any   `json:"base_cost"` // aceita number ou string
}

// PreviewResponse retorno do cálculo de preview.
type PreviewResponse struct {
	BaseCost  float64 `json:"base_cost"`
	Pct       float64 `json:"pct"`
	Fixed     float64 `json:"fixed"`
	FinalCost float64 `json:"final_cost"`
}

// ----- leitura interna --------------------------------------------------

// Defaults do sistema (espelha senderzz_get_markup_default() em markup.php).
const (
	expIntDefaultPct   = 20.0
	expIntDefaultFixed = 3.99
)

// readDefault retorna o par padrão (option senderzz_markup_default).
// Aceita tanto JSON novo {pct, fixed} quanto fallback nas options legadas
// senderzz_markup_default_pct / _fixed gravadas pelo PHP via $_POST.
// Quando nenhuma option existe, retorna os defaults do sistema (20% / R$3,99).
// Quando a option existe e foi explicitamente salva (mesmo como 0/0), respeita o valor salvo.
func (h *ExpedicaoIntegracoesHandler) readDefault(ctx context.Context) MarkupPair {
	raw := strings.TrimSpace(h.getOptionRaw(ctx, expIntOptDefault))
	if raw != "" {
		// Aceita {"pct":..,"fixed":..} (Go) ou objeto-like serializado.
		var pair struct {
			Pct   any `json:"pct"`
			Fixed any `json:"fixed"`
		}
		if err := json.Unmarshal([]byte(raw), &pair); err == nil {
			return MarkupPair{
				Pct:   parseExpIntFloat(pair.Pct),
				Fixed: parseExpIntFloat(pair.Fixed),
			}
		}
	}
	// Fallback compatível com o PHP legado (duas options separadas).
	legacyPct := strings.TrimSpace(h.getOptionRaw(ctx, "senderzz_markup_default_pct"))
	legacyFixed := strings.TrimSpace(h.getOptionRaw(ctx, "senderzz_markup_default_fixed"))
	if legacyPct != "" || legacyFixed != "" {
		out := MarkupPair{Pct: expIntDefaultPct, Fixed: expIntDefaultFixed}
		if legacyPct != "" {
			out.Pct = parseExpIntFloat(legacyPct)
		}
		if legacyFixed != "" {
			out.Fixed = parseExpIntFloat(legacyFixed)
		}
		return out
	}
	// Nenhuma option gravada → defaults do sistema (instalação nova).
	return MarkupPair{Pct: expIntDefaultPct, Fixed: expIntDefaultFixed}
}

// readRules retorna o mapa class_id → {pct,fixed} da option senderzz_markup_rules.
// Tolerante a chaves numéricas tanto string ("1") quanto número (1).
func (h *ExpedicaoIntegracoesHandler) readRules(ctx context.Context) map[int64]MarkupPair {
	out := map[int64]MarkupPair{}
	raw := strings.TrimSpace(h.getOptionRaw(ctx, expIntOptRules))
	if raw == "" {
		return out
	}

	// Tentativa 1: map[string]struct{pct,fixed}.
	var asObj map[string]struct {
		Pct   any `json:"pct"`
		Fixed any `json:"fixed"`
	}
	if err := json.Unmarshal([]byte(raw), &asObj); err == nil && asObj != nil {
		for k, v := range asObj {
			id, err := strconv.ParseInt(strings.TrimSpace(k), 10, 64)
			if err != nil || id <= 0 {
				continue
			}
			pair := MarkupPair{
				Pct:   parseExpIntFloat(v.Pct),
				Fixed: parseExpIntFloat(v.Fixed),
			}
			// Só persiste a entrada se houver pelo menos um valor > 0 (matches PHP semantics
			// onde o input vazio é equivalente a "herdar global").
			if pair.Pct > 0 || pair.Fixed > 0 {
				out[id] = pair
			}
		}
		return out
	}
	// Sem fallback adicional: option corrompida → mapa vazio.
	return out
}

// resolveClassName busca o nome da classe; retorna "Classe #N" se não houver tabela ou linha.
func (h *ExpedicaoIntegracoesHandler) resolveClassName(ctx context.Context, classID int64) string {
	fallback := fmt.Sprintf("Classe #%d", classID)
	if !h.tableExists(ctx, "senderzz_shipping_classes") {
		return fallback
	}
	var name string
	err := h.Pool.QueryRow(ctx,
		`SELECT COALESCE(name,'') FROM senderzz_shipping_classes WHERE id=$1`, classID).Scan(&name)
	if err != nil || strings.TrimSpace(name) == "" {
		return fallback
	}
	return name
}

// listShippingClasses lista todas as classes disponíveis.
// Tabela ausente → slice vazio (degradação graciosa).
func (h *ExpedicaoIntegracoesHandler) listShippingClasses(ctx context.Context) []ShippingClass {
	out := []ShippingClass{}
	if !h.tableExists(ctx, "senderzz_shipping_classes") {
		return out
	}
	rows, err := h.Pool.Query(ctx,
		`SELECT id, COALESCE(name,'') FROM senderzz_shipping_classes ORDER BY name ASC, id ASC`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var c ShippingClass
		if err := rows.Scan(&c.ID, &c.Name); err != nil {
			continue
		}
		if strings.TrimSpace(c.Name) == "" {
			c.Name = fmt.Sprintf("Classe #%d", c.ID)
		}
		out = append(out, c)
	}
	return out
}

// ----- GET /expedicao/markup --------------------------------------------

// GetMarkup retorna o par default + lista de regras (já com class_name resolvido).
func (h *ExpedicaoIntegracoesHandler) GetMarkup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	resp := MarkupResponse{
		Default: h.readDefault(ctx),
		Rules:   []MarkupRule{},
	}
	for id, pair := range h.readRules(ctx) {
		resp.Rules = append(resp.Rules, MarkupRule{
			ClassID:   id,
			ClassName: h.resolveClassName(ctx, id),
			Pct:       pair.Pct,
			Fixed:     pair.Fixed,
		})
	}
	// Ordena por class_id para output determinístico (mapa é unordered).
	sortMarkupRules(resp.Rules)
	httpx.JSON(w, http.StatusOK, resp)
}

// sortMarkupRules — ordenação estável por class_id ascendente.
// Implementação inline (sem dependência de sort) para evitar import extra.
func sortMarkupRules(items []MarkupRule) {
	n := len(items)
	for i := 1; i < n; i++ {
		cur := items[i]
		j := i - 1
		for j >= 0 && items[j].ClassID > cur.ClassID {
			items[j+1] = items[j]
			j--
		}
		items[j+1] = cur
	}
}

// ----- POST /expedicao/markup -------------------------------------------

// rawMarkupSave é o shape aceito do cliente (any para tolerar string/number do JS).
type rawMarkupSave struct {
	Default struct {
		Pct   any `json:"pct"`
		Fixed any `json:"fixed"`
	} `json:"default"`
	Rules []struct {
		ClassID int64 `json:"class_id"`
		Pct     any   `json:"pct"`
		Fixed   any   `json:"fixed"`
	} `json:"rules"`
}

// validatePair valida um par contra os limites (pct 0-500, fixed 0-999).
func validatePair(p MarkupPair) error {
	if p.Pct < 0 || p.Pct > expIntMaxPct {
		return fmt.Errorf("pct fora do intervalo (0-%v)", expIntMaxPct)
	}
	if p.Fixed < 0 || p.Fixed > expIntMaxFixed {
		return fmt.Errorf("fixed fora do intervalo (0-%v)", expIntMaxFixed)
	}
	return nil
}

// SaveMarkup faz UPSERT das duas options (default + rules) num único POST.
// Regras com pct==0 && fixed==0 são removidas (equivalente a "voltar a herdar global").
func (h *ExpedicaoIntegracoesHandler) SaveMarkup(w http.ResponseWriter, r *http.Request) {
	var in rawMarkupSave
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.Err(w, http.StatusBadRequest, "bad_request", "json inválido")
		return
	}
	ctx := r.Context()

	// 1) Default.
	def := MarkupPair{
		Pct:   parseExpIntFloat(in.Default.Pct),
		Fixed: parseExpIntFloat(in.Default.Fixed),
	}
	if err := validatePair(def); err != nil {
		httpx.Err(w, http.StatusBadRequest, "validation", "default: "+err.Error())
		return
	}
	defBytes, err := json.Marshal(def)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "encode_error", err.Error())
		return
	}
	if err := h.upsertOption(ctx, expIntOptDefault, string(defBytes)); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	// 2) Regras: dedup por class_id (último vencedor). Vazias (0/0) ignoradas.
	rulesMap := map[string]MarkupPair{}
	seen := map[int64]bool{}
	for _, rule := range in.Rules {
		if rule.ClassID <= 0 {
			continue
		}
		pair := MarkupPair{
			Pct:   parseExpIntFloat(rule.Pct),
			Fixed: parseExpIntFloat(rule.Fixed),
		}
		if err := validatePair(pair); err != nil {
			httpx.Err(w, http.StatusBadRequest, "validation",
				fmt.Sprintf("classe %d: %s", rule.ClassID, err.Error()))
			return
		}
		// 0/0 = não persiste (equivalente a remoção / herdar global).
		if pair.Pct == 0 && pair.Fixed == 0 {
			continue
		}
		rulesMap[strconv.FormatInt(rule.ClassID, 10)] = pair
		seen[rule.ClassID] = true
	}

	// Persiste mapa serializado (objeto com chaves string).
	rulesBytes, err := json.Marshal(rulesMap)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "encode_error", err.Error())
		return
	}
	if err := h.upsertOption(ctx, expIntOptRules, string(rulesBytes)); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ----- GET /expedicao/shipping-classes ----------------------------------

// GetShippingClasses retorna lista para popular o dropdown do modal.
// Tabela ausente → {items: []}.
func (h *ExpedicaoIntegracoesHandler) GetShippingClasses(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	items := h.listShippingClasses(ctx)
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

// ----- POST /expedicao/markup/preview -----------------------------------

// PreviewMarkup calcula o custo final aplicando a regra da classe (ou default).
// Fórmula: final = (base * (1 + pct/100)) + fixed.
func (h *ExpedicaoIntegracoesHandler) PreviewMarkup(w http.ResponseWriter, r *http.Request) {
	var in PreviewRequest
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.Err(w, http.StatusBadRequest, "bad_request", "json inválido")
		return
	}
	base := parseExpIntFloat(in.BaseCost)
	if base < 0 {
		httpx.Err(w, http.StatusBadRequest, "validation", "base_cost deve ser >= 0")
		return
	}

	ctx := r.Context()

	// Resolve pct/fixed: regra da classe se existir, senão default.
	def := h.readDefault(ctx)
	pair := def
	if in.ClassID > 0 {
		rules := h.readRules(ctx)
		if rule, ok := rules[in.ClassID]; ok {
			pair = rule
		}
	}

	final := (base * (1 + pair.Pct/100.0)) + pair.Fixed
	// Trunca/arredonda para 2 casas (BRL).
	final = math.Round(final*100) / 100

	httpx.JSON(w, http.StatusOK, PreviewResponse{
		BaseCost:  base,
		Pct:       pair.Pct,
		Fixed:     pair.Fixed,
		FinalCost: final,
	})
}

