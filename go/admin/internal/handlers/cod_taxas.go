// Package handlers — endpoint admin para configuração de taxas COD / entrega.
// Espelha tab_fin_taxas_entrega() em src/Admin/Unified_Menu.php:1333 (PHP legado).
// Persiste em senderzz_options (key/value) + senderzz_portal_user_meta.
package handlers

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

type CodTaxasHandler struct{ Pool *pgxpool.Pool }

// ----- helpers -----------------------------------------------------------

// tableExists verifica presença de uma tabela no schema public.
func (h *CodTaxasHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// parseRate aceita number ou string; troca vírgula por ponto e força >= 0.
// Espelha o parse() do PHP em tab_fin_taxas_entrega.
func parseRate(v any) float64 {
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

// parseRatePtr retorna *float64; nil quando não enviado / vazio (inerit global).
func parseRatePtr(v any) *float64 {
	if v == nil {
		return nil
	}
	switch x := v.(type) {
	case string:
		if strings.TrimSpace(x) == "" {
			return nil
		}
	}
	f := parseRate(v)
	return &f
}

// getOptionFloat lê option float com fallback. Tabela ausente → fallback.
func (h *CodTaxasHandler) getOptionFloat(ctx context.Context, key string, def float64) float64 {
	if !h.tableExists(ctx, "senderzz_options") {
		return def
	}
	var raw string
	err := h.Pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE "key"=$1`, key).Scan(&raw)
	if err != nil {
		return def
	}
	return parseRate(raw)
}

// getOptionFloatPtr lê option float ou nil se não existe (placeholder "padrão").
func (h *CodTaxasHandler) getOptionFloatPtr(ctx context.Context, key string) *float64 {
	if !h.tableExists(ctx, "senderzz_options") {
		return nil
	}
	var raw string
	err := h.Pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE "key"=$1`, key).Scan(&raw)
	if err != nil {
		return nil
	}
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil
	}
	f := parseRate(s)
	return &f
}

// getOptionJSON lê option e decodifica como JSON em out.
func (h *CodTaxasHandler) getOptionJSON(ctx context.Context, key string, out any) {
	if !h.tableExists(ctx, "senderzz_options") {
		return
	}
	var raw string
	if err := h.Pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE "key"=$1`, key).Scan(&raw); err != nil {
		return
	}
	if strings.TrimSpace(raw) == "" {
		return
	}
	_ = json.Unmarshal([]byte(raw), out)
}

// upsertOption grava option (UPSERT em senderzz_options).
func (h *CodTaxasHandler) upsertOption(ctx context.Context, key, value string) error {
	if !h.tableExists(ctx, "senderzz_options") {
		return nil
	}
	_, err := h.Pool.Exec(ctx,
		`INSERT INTO senderzz_options ("key", value)
		 VALUES ($1, $2)
		 ON CONFLICT ("key") DO UPDATE SET value = EXCLUDED.value`, key, value)
	return err
}

// deleteOption remove option (volta a herdar global).
func (h *CodTaxasHandler) deleteOption(ctx context.Context, key string) error {
	if !h.tableExists(ctx, "senderzz_options") {
		return nil
	}
	_, err := h.Pool.Exec(ctx, `DELETE FROM senderzz_options WHERE "key"=$1`, key)
	return err
}

// upsertOrDelete grava se ptr != nil e > 0; senão deleta (inerit).
func (h *CodTaxasHandler) upsertOrDelete(ctx context.Context, key string, val *float64) error {
	if val == nil || *val <= 0 {
		return h.deleteOption(ctx, key)
	}
	return h.upsertOption(ctx, key, strconv.FormatFloat(*val, 'f', 4, 64))
}

// ----- payloads ----------------------------------------------------------

// CodTaxasGlobal cobre os 4 valores principais + 4 penalidades. KPI grid.
type CodTaxasGlobal struct {
	TaxaClienteCOD          float64 `json:"taxa_cliente_cod"`           // sz_motoboy_taxa_entrega
	TaxaTransacaoPercentual float64 `json:"taxa_transacao_percentual"`  // sz_motoboy_taxa_percentual
	TaxaMotoboyEntrega      float64 `json:"taxa_motoboy_entrega"`       // sz_mbw_taxa_entrega
	TaxaMotoboyFrustrado    float64 `json:"taxa_motoboy_frustrado"`     // sz_mbw_taxa_frustrado
	AffFirstGlobal          float64 `json:"aff_first_global"`           // sz_aff_first_frustration_penalty
	AffRepeatGlobal         float64 `json:"aff_repeat_global"`          // sz_aff_default_penalty_value
	ProdFirstGlobal         float64 `json:"prod_first_global"`          // sz_prod_first_frustration_penalty
	ProdRepeatGlobal        float64 `json:"prod_repeat_global"`         // sz_aff_producer_frustration_penalty
}

// MotoboyTaxaItem taxa custom de um motoboy. nil = herdar global.
type MotoboyTaxaItem struct {
	ID            int64    `json:"id"`
	Nome          string   `json:"nome"`
	TaxaEntrega   *float64 `json:"taxa_entrega"`
	TaxaFrustrado *float64 `json:"taxa_frustrado"`
}

// ProducerTaxaItem configuração Senderzz por produtor.
type ProducerTaxaItem struct {
	UserID           int64    `json:"user_id"`
	Nome             string   `json:"nome"`
	Email            string   `json:"email"`
	MotoboyAtivo     bool     `json:"motoboy_ativo"`
	TaxaEntrega      *float64 `json:"taxa_entrega"`
	TaxaManuseio     *float64 `json:"taxa_manuseio"`
	TaxaPercentual   *float64 `json:"taxa_percentual"`
	FrustracaoFirst  *float64 `json:"frustracao_first"`
	FrustracaoRepeat *float64 `json:"frustracao_repeat"`
}

// AffiliateTaxaItem override de penalidade por afiliado.
type AffiliateTaxaItem struct {
	ID     int64    `json:"id"`
	Nome   string   `json:"nome"`
	First  *float64 `json:"first"`
	Repeat *float64 `json:"repeat"`
}

// ----- GET /cod-taxas/global ---------------------------------------------

func (h *CodTaxasHandler) GetGlobal(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := CodTaxasGlobal{
		TaxaClienteCOD:          h.getOptionFloat(ctx, "sz_motoboy_taxa_entrega", 25.0),
		TaxaTransacaoPercentual: h.getOptionFloat(ctx, "sz_motoboy_taxa_percentual", 0.0),
		TaxaMotoboyEntrega:      h.getOptionFloat(ctx, "sz_mbw_taxa_entrega", 18.0),
		TaxaMotoboyFrustrado:    h.getOptionFloat(ctx, "sz_mbw_taxa_frustrado", 5.0),
		AffFirstGlobal:          h.getOptionFloat(ctx, "sz_aff_first_frustration_penalty", 5.0),
		AffRepeatGlobal:         h.getOptionFloat(ctx, "sz_aff_default_penalty_value", 5.0),
		ProdFirstGlobal:         h.getOptionFloat(ctx, "sz_prod_first_frustration_penalty", 0.0),
		ProdRepeatGlobal:        h.getOptionFloat(ctx, "sz_aff_producer_frustration_penalty", 8.0),
	}
	httpx.JSON(w, 200, out)
}

// ----- POST /cod-taxas/global --------------------------------------------

// rawGlobal aceita string/number do JS (parseRate normaliza).
type rawGlobal struct {
	TaxaClienteCOD          any `json:"taxa_cliente_cod"`
	TaxaTransacaoPercentual any `json:"taxa_transacao_percentual"`
	TaxaMotoboyEntrega      any `json:"taxa_motoboy_entrega"`
	TaxaMotoboyFrustrado    any `json:"taxa_motoboy_frustrado"`
	AffFirstGlobal          any `json:"aff_first_global"`
	AffRepeatGlobal         any `json:"aff_repeat_global"`
	ProdFirstGlobal         any `json:"prod_first_global"`
	ProdRepeatGlobal        any `json:"prod_repeat_global"`
}

func (h *CodTaxasHandler) SaveGlobal(w http.ResponseWriter, r *http.Request) {
	var in rawGlobal
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	ctx := r.Context()

	pairs := map[string]float64{
		"sz_motoboy_taxa_entrega":              parseRate(in.TaxaClienteCOD),
		"sz_motoboy_taxa_percentual":           parseRate(in.TaxaTransacaoPercentual),
		"sz_mbw_taxa_entrega":                  parseRate(in.TaxaMotoboyEntrega),
		"sz_mbw_taxa_frustrado":                parseRate(in.TaxaMotoboyFrustrado),
		"sz_aff_first_frustration_penalty":     parseRate(in.AffFirstGlobal),
		"sz_aff_default_penalty_value":         parseRate(in.AffRepeatGlobal),
		"sz_prod_first_frustration_penalty":    parseRate(in.ProdFirstGlobal),
		"sz_aff_producer_frustration_penalty":  parseRate(in.ProdRepeatGlobal),
	}
	for k, v := range pairs {
		if err := h.upsertOption(ctx, k, strconv.FormatFloat(v, 'f', 4, 64)); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
	}
	httpx.JSON(w, 200, map[string]any{"ok": true})
}

// ----- GET /cod-taxas/motoboys -------------------------------------------

func (h *CodTaxasHandler) GetMotoboys(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := []MotoboyTaxaItem{}

	if !h.tableExists(ctx, "sz_motoboys") {
		httpx.JSON(w, 200, map[string]any{"items": out})
		return
	}

	rows, err := h.Pool.Query(ctx,
		`SELECT id, COALESCE(nome,'') FROM sz_motoboys WHERE ativo=true ORDER BY nome ASC`)
	if err != nil {
		httpx.JSON(w, 200, map[string]any{"items": out})
		return
	}
	defer rows.Close()

	for rows.Next() {
		var item MotoboyTaxaItem
		if err := rows.Scan(&item.ID, &item.Nome); err != nil {
			continue
		}
		item.TaxaEntrega = h.getOptionFloatPtr(ctx, "sz_mbw_taxa_entrega_mb_"+strconv.FormatInt(item.ID, 10))
		item.TaxaFrustrado = h.getOptionFloatPtr(ctx, "sz_mbw_taxa_frustrado_mb_"+strconv.FormatInt(item.ID, 10))
		out = append(out, item)
	}
	httpx.JSON(w, 200, map[string]any{"items": out})
}

// ----- POST /cod-taxas/motoboys ------------------------------------------

type rawMotoboyItem struct {
	ID            int64 `json:"id"`
	TaxaEntrega   any   `json:"taxa_entrega"`
	TaxaFrustrado any   `json:"taxa_frustrado"`
}

func (h *CodTaxasHandler) SaveMotoboys(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Items []rawMotoboyItem `json:"items"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	ctx := r.Context()

	for _, it := range body.Items {
		if it.ID <= 0 {
			continue
		}
		idStr := strconv.FormatInt(it.ID, 10)
		if err := h.upsertOrDelete(ctx, "sz_mbw_taxa_entrega_mb_"+idStr, parseRatePtr(it.TaxaEntrega)); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
		if err := h.upsertOrDelete(ctx, "sz_mbw_taxa_frustrado_mb_"+idStr, parseRatePtr(it.TaxaFrustrado)); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
	}
	httpx.JSON(w, 200, map[string]any{"ok": true})
}

// ----- GET /cod-taxas/producers ------------------------------------------

func (h *CodTaxasHandler) GetProducers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := []ProducerTaxaItem{}

	if !h.tableExists(ctx, "senderzz_portal_users") {
		httpx.JSON(w, 200, map[string]any{"items": out})
		return
	}

	// Lê produtores e clientes (papéis legados que vinham do WP).
	rows, err := h.Pool.Query(ctx,
		`SELECT id, COALESCE(nome,''), COALESCE(email,'')
		 FROM senderzz_portal_users
		 WHERE role IN ('client','producer')
		 ORDER BY id ASC`)
	if err != nil {
		httpx.JSON(w, 200, map[string]any{"items": out})
		return
	}

	type baseRow struct {
		ID    int64
		Nome  string
		Email string
	}
	users := []baseRow{}
	for rows.Next() {
		var u baseRow
		if err := rows.Scan(&u.ID, &u.Nome, &u.Email); err != nil {
			continue
		}
		users = append(users, u)
	}
	rows.Close()

	// Overrides por usuário (mapa user_id → valor) — armazenado como JSON.
	affOver := map[string]float64{}
	prodOver := map[string]float64{}
	h.getOptionJSON(ctx, "sz_frustration_aff_overrides", &affOver)
	h.getOptionJSON(ctx, "sz_frustration_prod_overrides", &prodOver)

	hasMeta := h.tableExists(ctx, "senderzz_portal_user_meta")

	for _, u := range users {
		item := ProducerTaxaItem{
			UserID: u.ID,
			Nome:   u.Nome,
			Email:  u.Email,
		}

		if hasMeta {
			// Lê meta keys (uma query por user_id para manter código simples).
			metaRows, err := h.Pool.Query(ctx,
				`SELECT meta_key, COALESCE(meta_value,'')
				 FROM senderzz_portal_user_meta
				 WHERE user_id=$1
				   AND meta_key IN ('_sz_motoboy_ativo','_sz_motoboy_taxa_entrega',
				                    '_sz_motoboy_taxa_manuseio','_sz_motoboy_taxa_percentual')`, u.ID)
			if err == nil {
				for metaRows.Next() {
					var k, v string
					if err := metaRows.Scan(&k, &v); err != nil {
						continue
					}
					vTrim := strings.TrimSpace(v)
					switch k {
					case "_sz_motoboy_ativo":
						item.MotoboyAtivo = (vTrim == "1" || vTrim == "true")
					case "_sz_motoboy_taxa_entrega":
						if vTrim != "" {
							f := parseRate(vTrim)
							item.TaxaEntrega = &f
						}
					case "_sz_motoboy_taxa_manuseio":
						if vTrim != "" {
							f := parseRate(vTrim)
							item.TaxaManuseio = &f
						}
					case "_sz_motoboy_taxa_percentual":
						if vTrim != "" {
							f := parseRate(vTrim)
							item.TaxaPercentual = &f
						}
					}
				}
				metaRows.Close()
			}
		}

		key := strconv.FormatInt(u.ID, 10)
		if v, ok := affOver[key]; ok {
			vv := v
			item.FrustracaoFirst = &vv
		}
		if v, ok := prodOver[key]; ok {
			vv := v
			item.FrustracaoRepeat = &vv
		}

		out = append(out, item)
	}

	httpx.JSON(w, 200, map[string]any{"items": out})
}

// ----- POST /cod-taxas/producers -----------------------------------------

type rawProducerItem struct {
	UserID           int64 `json:"user_id"`
	MotoboyAtivo     bool  `json:"motoboy_ativo"`
	TaxaEntrega      any   `json:"taxa_entrega"`
	TaxaManuseio     any   `json:"taxa_manuseio"`
	TaxaPercentual   any   `json:"taxa_percentual"`
	FrustracaoFirst  any   `json:"frustracao_first"`
	FrustracaoRepeat any   `json:"frustracao_repeat"`
}

func (h *CodTaxasHandler) SaveProducers(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Items []rawProducerItem `json:"items"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	ctx := r.Context()

	hasMeta := h.tableExists(ctx, "senderzz_portal_user_meta")

	// Mapas JSON existentes; mantém merge para não estourar outros usuários.
	affOver := map[string]float64{}
	prodOver := map[string]float64{}
	h.getOptionJSON(ctx, "sz_frustration_aff_overrides", &affOver)
	h.getOptionJSON(ctx, "sz_frustration_prod_overrides", &prodOver)

	for _, it := range body.Items {
		if it.UserID <= 0 {
			continue
		}
		key := strconv.FormatInt(it.UserID, 10)

		if hasMeta {
			// motoboy_ativo é sempre gravado (boolean ↔ '1'/'0').
			ativoVal := "0"
			if it.MotoboyAtivo {
				ativoVal = "1"
			}
			if err := h.upsertMeta(ctx, it.UserID, "_sz_motoboy_ativo", ativoVal); err != nil {
				httpx.Err(w, 500, "db_error", err.Error())
				return
			}
			// Valores numéricos: vazio → deleta meta (inerit). 0 também deleta (não persiste lixo).
			if err := h.metaUpsertOrDelete(ctx, it.UserID, "_sz_motoboy_taxa_entrega", parseRatePtr(it.TaxaEntrega)); err != nil {
				httpx.Err(w, 500, "db_error", err.Error())
				return
			}
			if err := h.metaUpsertOrDelete(ctx, it.UserID, "_sz_motoboy_taxa_manuseio", parseRatePtr(it.TaxaManuseio)); err != nil {
				httpx.Err(w, 500, "db_error", err.Error())
				return
			}
			if err := h.metaUpsertOrDelete(ctx, it.UserID, "_sz_motoboy_taxa_percentual", parseRatePtr(it.TaxaPercentual)); err != nil {
				httpx.Err(w, 500, "db_error", err.Error())
				return
			}
		}

		// Penalidades de frustração: vazio = remove do mapa (volta a herdar global).
		if first := parseRatePtr(it.FrustracaoFirst); first != nil && *first > 0 {
			affOver[key] = *first
		} else {
			delete(affOver, key)
		}
		if rep := parseRatePtr(it.FrustracaoRepeat); rep != nil && *rep > 0 {
			prodOver[key] = *rep
		} else {
			delete(prodOver, key)
		}
	}

	// Persiste arrays JSON.
	if b, err := json.Marshal(affOver); err == nil {
		_ = h.upsertOption(ctx, "sz_frustration_aff_overrides", string(b))
	}
	if b, err := json.Marshal(prodOver); err == nil {
		_ = h.upsertOption(ctx, "sz_frustration_prod_overrides", string(b))
	}

	httpx.JSON(w, 200, map[string]any{"ok": true})
}

// upsertMeta grava uma linha em senderzz_portal_user_meta.
func (h *CodTaxasHandler) upsertMeta(ctx context.Context, userID int64, key, value string) error {
	// Estrutura assumida: (user_id, meta_key, meta_value) com UNIQUE(user_id, meta_key).
	_, err := h.Pool.Exec(ctx,
		`INSERT INTO senderzz_portal_user_meta (user_id, meta_key, meta_value)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (user_id, meta_key) DO UPDATE SET meta_value = EXCLUDED.meta_value`,
		userID, key, value)
	return err
}

// deleteMeta remove um meta_key.
func (h *CodTaxasHandler) deleteMeta(ctx context.Context, userID int64, key string) error {
	_, err := h.Pool.Exec(ctx,
		`DELETE FROM senderzz_portal_user_meta WHERE user_id=$1 AND meta_key=$2`, userID, key)
	return err
}

// metaUpsertOrDelete grava se val != nil && > 0; senão deleta (inerit global).
func (h *CodTaxasHandler) metaUpsertOrDelete(ctx context.Context, userID int64, key string, val *float64) error {
	if val == nil || *val <= 0 {
		return h.deleteMeta(ctx, userID, key)
	}
	return h.upsertMeta(ctx, userID, key, strconv.FormatFloat(*val, 'f', 4, 64))
}

// ----- GET /cod-taxas/affiliates -----------------------------------------

func (h *CodTaxasHandler) GetAffiliates(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := []AffiliateTaxaItem{}

	if !h.tableExists(ctx, "senderzz_affiliates") {
		httpx.JSON(w, 200, map[string]any{"items": out})
		return
	}

	// Lista afiliados distintos com nome do usuário portal.
	rows, err := h.Pool.Query(ctx,
		`SELECT DISTINCT a.afiliado_id,
		        COALESCE(NULLIF(u.nome,''), 'Afiliado #' || a.afiliado_id::text) AS nome
		 FROM senderzz_affiliates a
		 LEFT JOIN senderzz_portal_users u ON u.id = a.afiliado_id
		 WHERE a.afiliado_id > 0
		 ORDER BY a.afiliado_id ASC`)
	if err != nil {
		httpx.JSON(w, 200, map[string]any{"items": out})
		return
	}
	defer rows.Close()

	affOver := map[string]float64{}
	prodOver := map[string]float64{}
	h.getOptionJSON(ctx, "sz_frustration_aff_overrides", &affOver)
	h.getOptionJSON(ctx, "sz_frustration_prod_overrides", &prodOver)

	for rows.Next() {
		var it AffiliateTaxaItem
		if err := rows.Scan(&it.ID, &it.Nome); err != nil {
			continue
		}
		key := strconv.FormatInt(it.ID, 10)
		if v, ok := affOver[key]; ok {
			vv := v
			it.First = &vv
		}
		if v, ok := prodOver[key]; ok {
			vv := v
			it.Repeat = &vv
		}
		out = append(out, it)
	}
	httpx.JSON(w, 200, map[string]any{"items": out})
}

// ----- POST /cod-taxas/affiliates ----------------------------------------

type rawAffiliateItem struct {
	ID     int64 `json:"id"`
	First  any   `json:"first"`
	Repeat any   `json:"repeat"`
}

func (h *CodTaxasHandler) SaveAffiliates(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Items []rawAffiliateItem `json:"items"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	ctx := r.Context()

	affOver := map[string]float64{}
	prodOver := map[string]float64{}
	h.getOptionJSON(ctx, "sz_frustration_aff_overrides", &affOver)
	h.getOptionJSON(ctx, "sz_frustration_prod_overrides", &prodOver)

	for _, it := range body.Items {
		if it.ID <= 0 {
			continue
		}
		key := strconv.FormatInt(it.ID, 10)
		if v := parseRatePtr(it.First); v != nil && *v > 0 {
			affOver[key] = *v
		} else {
			delete(affOver, key)
		}
		if v := parseRatePtr(it.Repeat); v != nil && *v > 0 {
			prodOver[key] = *v
		} else {
			delete(prodOver, key)
		}
	}

	if b, err := json.Marshal(affOver); err == nil {
		_ = h.upsertOption(ctx, "sz_frustration_aff_overrides", string(b))
	}
	if b, err := json.Marshal(prodOver); err == nil {
		_ = h.upsertOption(ctx, "sz_frustration_prod_overrides", string(b))
	}

	httpx.JSON(w, 200, map[string]any{"ok": true})
}
