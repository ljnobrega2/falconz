// Package handlers — endpoint admin para LER e EDITAR a "regra de cálculo"
// (taxas) que governa o breakdown financeiro de todo o site.
//
// Pedido do dono: "regra de cálculo muda SÓ via admin". As taxas já vivem em
// senderzz_options e já são LIDAS pelos handlers de cálculo (order_detail.go,
// view sz_order_financeiro etc.). Este handler é a porta de escrita: alterar
// aqui reflete automaticamente em todo o site, sem deploy.
//
// Chaves controladas:
//   - sz_producer_transaction_fee_pct  — taxa de transação do PRODUTOR (default 4,99%)
//   - sz_affiliate_transaction_fee_pct — taxa de transação do AFILIADO (default 4,99%)
//   - motoboy_repasse_padrao           — repasse padrão do motoboy por entrega (default 18)
//
// Ambas as taxas são configuráveis via admin e gravadas em senderzz_options.
// Ao salvar, o trigger sz_options_financials_sync re-calcula sz_order_financials
// automaticamente para todos os pedidos.
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

// ConfigTaxasHandler — leitura/escrita das taxas em senderzz_options.
type ConfigTaxasHandler struct{ Pool *pgxpool.Pool }

// ----- helpers -----------------------------------------------------------

func (h *ConfigTaxasHandler) optTableExists(ctx context.Context) bool {
	return tableExistsCached(ctx, h.Pool, "senderzz_options") // mesmo cache infoschema
}

// getOptionFloat lê option float com fallback. Tabela ausente → fallback.
func (h *ConfigTaxasHandler) getOptionFloat(ctx context.Context, key string, def float64) float64 {
	if !h.optTableExists(ctx) {
		return def
	}
	var raw string
	if err := h.Pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE name=$1`, key).Scan(&raw); err != nil {
		return def
	}
	if raw == "" {
		return def
	}
	return parseRate(raw) // parseRate (cod_taxas.go) aceita "4,99"/"4.99"/number
}

// upsertOption grava option (UPSERT em senderzz_options via ON CONFLICT).
func (h *ConfigTaxasHandler) upsertOption(ctx context.Context, key, value string) error {
	if !h.optTableExists(ctx) {
		return nil
	}
	_, err := h.Pool.Exec(ctx,
		`INSERT INTO senderzz_options (name, value)
		 VALUES ($1, $2)
		 ON CONFLICT (name) DO UPDATE SET value = EXCLUDED.value`, key, value)
	return err
}

// ----- payloads ----------------------------------------------------------

// configTaxasResp — resposta JSON limpa do GET.
type configTaxasResp struct {
	ProducerTransactionFeePct  float64 `json:"producer_transaction_fee_pct"`  // editável
	AffiliateTransactionFeePct float64 `json:"affiliate_transaction_fee_pct"` // editável
	MotoboyRepassePadrao       float64 `json:"motoboy_repasse_padrao"`        // editável
}

type configTaxasSave struct {
	ProducerTransactionFeePct  *any `json:"producer_transaction_fee_pct"`
	AffiliateTransactionFeePct *any `json:"affiliate_transaction_fee_pct"`
	MotoboyRepassePadrao       *any `json:"motoboy_repasse_padrao"`
}

// ----- GET /admin/config/taxas -------------------------------------------

func (h *ConfigTaxasHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	httpx.JSON(w, 200, configTaxasResp{
		ProducerTransactionFeePct:  h.getOptionFloat(ctx, "sz_producer_transaction_fee_pct", 4.99),
		AffiliateTransactionFeePct: h.getOptionFloat(ctx, "sz_affiliate_transaction_fee_pct", 4.99),
		MotoboyRepassePadrao:       h.getOptionFloat(ctx, "motoboy_repasse_padrao", 18.0),
	})
}

// ----- POST /admin/config/taxas ------------------------------------------

func (h *ConfigTaxasHandler) Save(w http.ResponseWriter, r *http.Request) {
	var in configTaxasSave
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	ctx := r.Context()

	// Taxa de transação do produtor: percentual 0..100.
	if in.ProducerTransactionFeePct != nil {
		pct := parseRate(*in.ProducerTransactionFeePct)
		if pct < 0 || pct > 100 {
			httpx.Err(w, 400, "invalid_range", "producer_transaction_fee_pct deve estar entre 0 e 100")
			return
		}
		if err := h.upsertOption(ctx, "sz_producer_transaction_fee_pct",
			strconv.FormatFloat(pct, 'f', 4, 64)); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
	}

	// Taxa de transação do afiliado.
	if in.AffiliateTransactionFeePct != nil {
		pct := parseRate(*in.AffiliateTransactionFeePct)
		if pct < 0 || pct > 100 {
			httpx.Err(w, 400, "invalid_range", "affiliate_transaction_fee_pct deve estar entre 0 e 100")
			return
		}
		if err := h.upsertOption(ctx, "sz_affiliate_transaction_fee_pct",
			strconv.FormatFloat(pct, 'f', 4, 64)); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
	}

	// Repasse padrão do motoboy: valor em R$, >= 0 (sem teto superior).
	if in.MotoboyRepassePadrao != nil {
		v := parseRate(*in.MotoboyRepassePadrao) // parseRate já força >= 0
		if err := h.upsertOption(ctx, "motoboy_repasse_padrao",
			strconv.FormatFloat(v, 'f', 4, 64)); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
	}

	// Retorna os valores salvos (lê de novo do banco — fonte da verdade).
	h.Get(w, r)
}

// ----- Taxa de transação por PRODUTOR (override individual) -------------
//
// AUDIT-2026-07-17: sz_producer_transaction_fee_pct era única global. Regra do
// dono: "taxas e cálculos COD devem ser personalizados... por produtor". Chave
// nova sz_producer_fee_rules ({"<produtor_id>": pct}) — override individual, com
// fallback pro global (migração 508-producer-transaction-fee-per-producer.sql,
// já lida pelo trigger sz_revenue_capture_order). Espelha o padrão de
// go/admin/internal/handlers/expedicao_integracoes.go (markup por produto).

const optProducerFeeRules = "sz_producer_fee_rules"

// producerFeeRow — 1 linha da tabela (produtor + override, se houver).
type producerFeeRow struct {
	ProducerID int64    `json:"producer_id"`
	Name       string   `json:"name"`
	Pct        *float64 `json:"pct"` // nil = sem override (usa o global)
}

// readProducerFeeRules lê o mapa produtor_id → pct da option JSON.
func (h *ConfigTaxasHandler) readProducerFeeRules(ctx context.Context) map[int64]float64 {
	out := map[int64]float64{}
	var raw string
	if err := h.Pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE name=$1`, optProducerFeeRules).Scan(&raw); err != nil {
		return out
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return out
	}
	var asObj map[string]float64
	if err := json.Unmarshal([]byte(raw), &asObj); err != nil {
		return out
	}
	for k, v := range asObj {
		id, err := strconv.ParseInt(strings.TrimSpace(k), 10, 64)
		if err != nil || id <= 0 {
			continue
		}
		out[id] = v
	}
	return out
}

// ----- GET /admin/config/taxas/produtores --------------------------------
//
// Lista TODOS os produtores (role='produtor') com o override atual, se houver.
func (h *ConfigTaxasHandler) ListProducerFees(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rules := h.readProducerFeeRules(ctx)

	rows, err := h.Pool.Query(ctx,
		`SELECT id, name FROM senderzz_portal_users WHERE role = 'produtor' ORDER BY name ASC, id ASC`)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []producerFeeRow{}
	for rows.Next() {
		var row producerFeeRow
		if err := rows.Scan(&row.ProducerID, &row.Name); err != nil {
			continue
		}
		if pct, ok := rules[row.ProducerID]; ok {
			p := pct
			row.Pct = &p
		}
		out = append(out, row)
	}
	httpx.JSON(w, 200, map[string]any{"items": out})
}

// ----- POST /admin/config/taxas/produtores -------------------------------
//
// UPSERT em lote das overrides (mesmo formato do markup: envia só o que tem
// override; ausentes/omitidos voltam a herdar o global).
type saveProducerFeesReq struct {
	Rules []struct {
		ProducerID int64   `json:"producer_id"`
		Pct        float64 `json:"pct"`
	} `json:"rules"`
}

func (h *ConfigTaxasHandler) SaveProducerFees(w http.ResponseWriter, r *http.Request) {
	var in saveProducerFeesReq
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	rulesMap := map[string]float64{}
	for _, rule := range in.Rules {
		if rule.ProducerID <= 0 {
			continue
		}
		if rule.Pct < 0 || rule.Pct > 100 {
			httpx.Err(w, 400, "invalid_range",
				"pct do produtor "+strconv.FormatInt(rule.ProducerID, 10)+" deve estar entre 0 e 100")
			return
		}
		rulesMap[strconv.FormatInt(rule.ProducerID, 10)] = rule.Pct
	}
	b, err := json.Marshal(rulesMap)
	if err != nil {
		httpx.Err(w, 500, "encode_error", err.Error())
		return
	}
	ctx := r.Context()
	if err := h.upsertOption(ctx, optProducerFeeRules, string(b)); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	h.ListProducerFees(w, r)
}

// ----- DELETE /admin/config/taxas/produtores/{id} ------------------------
//
// Remove o override individual — o produtor volta a usar a taxa global.
func (h *ConfigTaxasHandler) DeleteProducerFee(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	ctx := r.Context()
	rules := h.readProducerFeeRules(ctx)
	if _, ok := rules[id]; !ok {
		httpx.JSON(w, 200, map[string]any{"ok": true})
		return
	}
	delete(rules, id)
	rulesMap := map[string]float64{}
	for cid, pct := range rules {
		rulesMap[strconv.FormatInt(cid, 10)] = pct
	}
	b, err := json.Marshal(rulesMap)
	if err != nil {
		httpx.Err(w, 500, "encode_error", err.Error())
		return
	}
	if err := h.upsertOption(ctx, optProducerFeeRules, string(b)); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	httpx.JSON(w, 200, map[string]any{"ok": true})
}
