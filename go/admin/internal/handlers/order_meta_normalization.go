// Package handlers — endpoint admin para Normalização de Meta de Pedidos.
// Espelha tab_meta_normalization() em src/Admin/Unified_Menu.php:1780.
// Persiste estado em senderzz_options (keys: senderzz_meta_norm_last_run, senderzz_meta_norm_log).
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

// OrderMetaNormalizationHandler gerencia leitura e execução da normalização de meta.
type OrderMetaNormalizationHandler struct{ Pool *pgxpool.Pool }

const (
	metaNormLastRunKey   = "senderzz_meta_norm_last_run"
	// PHP grava em senderzz_meta_norm_log (class-senderzz-order-meta.php:379,382,397,404).
	metaDivergenceLogKey = "senderzz_meta_norm_log"
)

// READ_FALLBACKS espelha $READ_FALLBACKS em class-senderzz-order-meta.php.
// Chave = campo canônico, valor = lista de fallbacks (índice 0 = o próprio canônico).
var metaReadFallbacks = map[string][]string{
	"_senderzz_fee_total": {
		"_senderzz_fee_total",
		"_sz_mb_taxa_total",
		"_sz_taxa_total",
		"_senderzz_shipping_charged",
		"_senderzz_shipping_real_cost",
	},
	"_senderzz_fee_delivery": {
		"_senderzz_fee_delivery",
		"_sz_mb_taxa_entrega",
		"_sz_taxa_entrega",
	},
	"_senderzz_fee_transaction": {
		"_senderzz_fee_transaction",
		"_sz_mb_taxa_adicional",
		"_sz_taxa_adicional",
	},
	"_senderzz_fee_percent": {
		"_senderzz_fee_percent",
		"_sz_mb_taxa_percentual",
	},
	"_senderzz_delivery_date": {
		"_senderzz_delivery_date",
		"_sz_delivery_date",
		"_sz_motoboy_entrega_data",
	},
	"_senderzz_motoboy_status": {
		"_senderzz_motoboy_status",
		"_senderzz_motoboy_flow_status",
	},
	"_senderzz_order_gross_total": {
		"_senderzz_order_gross_total",
		"_senderzz_offer_value",
		"_sz_aff_order_total",
	},
	"_senderzz_affiliate_commission": {
		"_senderzz_affiliate_commission",
		"_sz_aff_commission",
	},
	"_senderzz_affiliate_commission_pct": {
		"_senderzz_affiliate_commission_pct",
		"_sz_aff_commission_pct",
	},
	"_senderzz_producer_user_id": {
		"_senderzz_producer_user_id",
		"_senderzz_owner_user_id",
		"_sz_aff_producer_id",
	},
	"_senderzz_affiliate_id": {
		"_senderzz_affiliate_id",
		"_sz_affiliate_id",
		"_sz_affiliate_ref",
	},
	"_senderzz_affiliate_user_id": {
		"_senderzz_affiliate_user_id",
		"_sz_affiliate_user_id",
	},
}

// metaLastRun estrutura persistida em senderzz_options para rastrear a última execução.
type metaLastRun struct {
	At         string `json:"at"`          // ISO-8601
	Updated    int    `json:"updated"`     // quantidade de metas atualizadas
	NextOffset int    `json:"next_offset"` // próximo offset para paginação
}

// metaDivergenceEntry é a representação normalizada para a UI.
// PHP emite dois shapes; este é o shape de saída unificado para o React.
type metaDivergenceEntry struct {
	OrderID        int    `json:"order_id"`
	CanonicalKey   string `json:"canonical_key"`
	CanonicalValue string `json:"canonical_value"`
	LegacyKey      string `json:"legacy_key"`
	LegacyValue    string `json:"legacy_value"`
	Action         string `json:"action"` // "divergence" | "filled" | "would_fill"
}

// metaDivergenceRaw decodifica qualquer dos dois shapes que o PHP grava em senderzz_meta_norm_log.
// Shape 1 (divergence): canonical, canonical_value, legacy_field, legacy_value, action
// Shape 2 (filled/would_fill): canonical, from_field, value, action
type metaDivergenceRaw struct {
	OrderID        int    `json:"order_id"`
	Canonical      string `json:"canonical"`        // canônico (ambos os shapes)
	CanonicalValue string `json:"canonical_value"`  // shape 1
	LegacyField    string `json:"legacy_field"`     // shape 1
	LegacyValue    string `json:"legacy_value"`     // shape 1
	FromField      string `json:"from_field"`       // shape 2
	Value          string `json:"value"`            // shape 2
	Action         string `json:"action"`
}

// normalizeRaw converte metaDivergenceRaw (qualquer shape) para metaDivergenceEntry.
func normalizeRaw(r metaDivergenceRaw) metaDivergenceEntry {
	e := metaDivergenceEntry{
		OrderID:      r.OrderID,
		CanonicalKey: r.Canonical,
		Action:       r.Action,
	}
	if r.Action == "divergence" {
		e.CanonicalValue = r.CanonicalValue
		e.LegacyKey = r.LegacyField
		e.LegacyValue = r.LegacyValue
	} else {
		// filled / would_fill: valor veio do campo legado para preencher o canônico vazio
		e.CanonicalValue = "" // canônico estava vazio antes de preencher
		e.LegacyKey = r.FromField
		e.LegacyValue = r.Value
	}
	return e
}

// ----- helpers -----------------------------------------------------------

// tableExistsMeta verifica presença de uma tabela no schema public.
func (h *OrderMetaNormalizationHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// readOption lê um valor de senderzz_options; retorna "" se ausente ou tabela inexistente.
func (h *OrderMetaNormalizationHandler) readOption(ctx context.Context, key string) string {
	if !h.tableExists(ctx, "senderzz_options") {
		return ""
	}
	var raw string
	_ = h.Pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE "key"=$1`, key).Scan(&raw)
	return strings.TrimSpace(raw)
}

// upsertOption grava/atualiza um valor em senderzz_options como JSON.
func (h *OrderMetaNormalizationHandler) upsertOption(ctx context.Context, key string, value any) error {
	if !h.tableExists(ctx, "senderzz_options") {
		// Graceful degradation: tabela ausente, não persiste mas não falha.
		return nil
	}
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = h.Pool.Exec(ctx,
		`INSERT INTO senderzz_options ("key", value)
		 VALUES ($1, $2)
		 ON CONFLICT ("key") DO UPDATE SET value = EXCLUDED.value`,
		key, string(b))
	return err
}

// metaTable retorna a tabela de order meta disponível: sz_orders_meta, wc_orders_meta, ou "".
func (h *OrderMetaNormalizationHandler) metaTable(ctx context.Context) string {
	if h.tableExists(ctx, "sz_orders_meta") {
		return "sz_orders_meta"
	}
	if h.tableExists(ctx, "wc_orders_meta") {
		return "wc_orders_meta"
	}
	return ""
}

// orderTable retorna a tabela de pedidos WC disponível: wc_orders ou wp_posts como fallback.
// Em Postgres mirrors, wc_orders espelha a tabela HPOS do MySQL.
func (h *OrderMetaNormalizationHandler) orderTable(ctx context.Context) string {
	if h.tableExists(ctx, "wc_orders") {
		return "wc_orders"
	}
	if h.tableExists(ctx, "sz_orders") {
		return "sz_orders"
	}
	if h.tableExists(ctx, "wp_posts") {
		return "wp_posts"
	}
	return ""
}

// countOrders retorna total de pedidos WC/shop_order; 0 gracioso se tabela ausente.
func (h *OrderMetaNormalizationHandler) countOrders(ctx context.Context) int {
	tbl := h.orderTable(ctx)
	if tbl == "" {
		return 0
	}
	var n int
	// wc_orders e sz_orders filtram por type='shop_order'; wp_posts por post_type.
	if tbl == "wp_posts" {
		_ = h.Pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM wp_posts WHERE post_type='shop_order'`).Scan(&n)
	} else {
		_ = h.Pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM `+tbl+` WHERE type='shop_order'`).Scan(&n)
	}
	return n
}

// countNormalized conta pedidos que têm pelo menos um campo canônico preenchido.
// Usa a tabela de meta disponível.
func (h *OrderMetaNormalizationHandler) countNormalized(ctx context.Context) int {
	metaTbl := h.metaTable(ctx)
	orderTbl := h.orderTable(ctx)
	if metaTbl == "" || orderTbl == "" {
		return 0
	}

	canonicals := make([]string, 0, len(metaReadFallbacks))
	for k := range metaReadFallbacks {
		canonicals = append(canonicals, k)
	}

	// Conta pedidos distintos que têm TODOS os canônicos preenchidos com valor não-vazio.
	// Para simplificar, conta pedidos que têm ao menos um canônico preenchido.
	args := make([]any, len(canonicals))
	placeholders := make([]string, len(canonicals))
	for i, k := range canonicals {
		args[i] = k
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}

	var n int
	_ = h.Pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT order_id)
		FROM `+metaTbl+`
		WHERE meta_key IN (`+strings.Join(placeholders, ",")+`)
		  AND meta_value IS NOT NULL AND meta_value <> ''`,
		args...).Scan(&n)
	return n
}

// hposActive verifica se HPOS está ativo (tabela wc_orders_meta ou sz_orders existe).
func (h *OrderMetaNormalizationHandler) hposActive(ctx context.Context) bool {
	return h.tableExists(ctx, "wc_orders_meta") || h.tableExists(ctx, "sz_orders_meta") || h.tableExists(ctx, "sz_orders")
}

// fetchOrderIDs retorna IDs de pedidos shop_order LIMIT/OFFSET.
func (h *OrderMetaNormalizationHandler) fetchOrderIDs(ctx context.Context, limit, offset int) ([]int64, error) {
	tbl := h.orderTable(ctx)
	if tbl == "" {
		return nil, nil
	}
	var q string
	if tbl == "wp_posts" {
		q = fmt.Sprintf(`SELECT ID FROM wp_posts WHERE post_type='shop_order' ORDER BY ID ASC LIMIT %d OFFSET %d`, limit, offset)
	} else {
		q = fmt.Sprintf(`SELECT id FROM %s WHERE type='shop_order' ORDER BY id ASC LIMIT %d OFFSET %d`, tbl, limit, offset)
	}
	rows, err := h.Pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			continue
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// getOrderMeta retorna um mapa meta_key→meta_value para os order IDs e meta_keys fornecidos.
// Graceful: retorna mapa vazio se a tabela não existir.
func (h *OrderMetaNormalizationHandler) getOrderMeta(ctx context.Context, metaTbl string, orderIDs []int64, keys []string) map[string]map[string]string {
	result := make(map[string]map[string]string) // orderID_str → meta_key → meta_value
	if metaTbl == "" || len(orderIDs) == 0 || len(keys) == 0 {
		return result
	}

	// Monta placeholders para order_id IN ($1,...) e meta_key IN ($n,...)
	args := make([]any, 0, len(orderIDs)+len(keys))
	idPlaceholders := make([]string, len(orderIDs))
	for i, id := range orderIDs {
		args = append(args, id)
		idPlaceholders[i] = fmt.Sprintf("$%d", i+1)
	}
	keyPlaceholders := make([]string, len(keys))
	for i, k := range keys {
		args = append(args, k)
		keyPlaceholders[i] = fmt.Sprintf("$%d", len(orderIDs)+i+1)
	}

	q := `SELECT order_id::text, meta_key, COALESCE(meta_value,'') FROM ` + metaTbl +
		` WHERE order_id IN (` + strings.Join(idPlaceholders, ",") + `)` +
		` AND meta_key IN (` + strings.Join(keyPlaceholders, ",") + `)`

	rows, err := h.Pool.Query(ctx, q, args...)
	if err != nil {
		return result
	}
	defer rows.Close()
	for rows.Next() {
		var orderIDStr, mk, mv string
		if err := rows.Scan(&orderIDStr, &mk, &mv); err != nil {
			continue
		}
		if result[orderIDStr] == nil {
			result[orderIDStr] = make(map[string]string)
		}
		result[orderIDStr][mk] = mv
	}
	return result
}

// upsertOrderMeta escreve meta_key=meta_value para um pedido.
// Usa UPDATE primeiro; se nenhuma linha atualizada, faz INSERT.
// Não usa ON CONFLICT porque wc_orders_meta/sz_orders_meta pode não ter unique em (order_id, meta_key).
func (h *OrderMetaNormalizationHandler) upsertOrderMeta(ctx context.Context, metaTbl string, orderID int64, metaKey, metaValue string) error {
	if metaTbl == "" {
		return nil
	}
	tag, err := h.Pool.Exec(ctx,
		`UPDATE `+metaTbl+` SET meta_value = $3 WHERE order_id = $1 AND meta_key = $2`,
		orderID, metaKey, metaValue)
	if err != nil {
		return err
	}
	// RowsAffected: se 0, linha não existe — insere.
	if tag.RowsAffected() == 0 {
		_, err = h.Pool.Exec(ctx,
			`INSERT INTO `+metaTbl+` (order_id, meta_key, meta_value) VALUES ($1, $2, $3)`,
			orderID, metaKey, metaValue)
	}
	return err
}

// ----- endpoints ---------------------------------------------------------

// GET /order-meta/normalization-status
// Retorna KPIs: total, normalizados, pendentes, divergências, última execução, HPOS ativo.
func (h *OrderMetaNormalizationHandler) Status(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	total := h.countOrders(ctx)
	normalized := h.countNormalized(ctx)
	pending := total - normalized
	if pending < 0 {
		pending = 0
	}

	hpos := h.hposActive(ctx)

	// Lê last_run da option.
	var lastRun *metaLastRun
	var lastRunUpdated int
	if raw := h.readOption(ctx, metaNormLastRunKey); raw != "" {
		var lr metaLastRun
		if err := json.Unmarshal([]byte(raw), &lr); err == nil {
			lastRun = &lr
			lastRunUpdated = lr.Updated
		}
	}

	// Conta divergências do log persistido (key PHP real: senderzz_meta_norm_log).
	divergencesCount := 0
	if raw := h.readOption(ctx, metaDivergenceLogKey); raw != "" {
		var items []metaDivergenceRaw
		if err := json.Unmarshal([]byte(raw), &items); err == nil {
			divergencesCount = len(items)
		}
	}

	type statusResponse struct {
		OrdersTotal      int          `json:"orders_total"`
		OrdersNormalized int          `json:"orders_normalized"`
		OrdersPending    int          `json:"orders_pending"`
		DivergencesCount int          `json:"divergences_count"`
		LastRun          *metaLastRun `json:"last_run"`
		LastRunUpdated   int          `json:"last_run_updated"`
		HposActive       bool         `json:"hpos_active"`
	}

	httpx.JSON(w, http.StatusOK, statusResponse{
		OrdersTotal:      total,
		OrdersNormalized: normalized,
		OrdersPending:    pending,
		DivergencesCount: divergencesCount,
		LastRun:          lastRun,
		LastRunUpdated:   lastRunUpdated,
		HposActive:       hpos,
	})
}

// POST /order-meta/normalize
// Body: {batch_size: 50, offset: 0, dry_run: false}
// Executa normalização real: lê campos de meta, detecta divergências, preenche canônicos vazios.
func (h *OrderMetaNormalizationHandler) Normalize(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	type normalizeInput struct {
		BatchSize int  `json:"batch_size"`
		Offset    int  `json:"offset"`
		DryRun    bool `json:"dry_run"`
	}
	var in normalizeInput
	in.BatchSize = 50 // default
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.Err(w, http.StatusBadRequest, "bad_request", "json inválido")
		return
	}

	// Valida batch_size entre 10 e 500.
	if in.BatchSize < 10 || in.BatchSize > 500 {
		httpx.Err(w, http.StatusBadRequest, "validation_error",
			fmt.Sprintf("batch_size deve estar entre 10 e 500 (recebido: %d)", in.BatchSize))
		return
	}
	if in.Offset < 0 {
		in.Offset = 0
	}

	metaTbl := h.metaTable(ctx)

	// Busca IDs do batch.
	ids, err := h.fetchOrderIDs(ctx, in.BatchSize, in.Offset)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	done := len(ids)
	updated := 0
	var batchLog []metaDivergenceRaw

	if len(ids) > 0 && metaTbl != "" {
		// Coleta todos os meta_keys necessários (canônicos + fallbacks).
		allKeys := make(map[string]struct{})
		for canonical, fallbacks := range metaReadFallbacks {
			allKeys[canonical] = struct{}{}
			for _, fb := range fallbacks {
				allKeys[fb] = struct{}{}
			}
		}
		keysSlice := make([]string, 0, len(allKeys))
		for k := range allKeys {
			keysSlice = append(keysSlice, k)
		}

		// Busca todos os meta values do batch de uma vez.
		metaMap := h.getOrderMeta(ctx, metaTbl, ids, keysSlice)

		for _, orderID := range ids {
			orderIDStr := fmt.Sprintf("%d", orderID)
			orderMeta := metaMap[orderIDStr] // pode ser nil

			for canonical, fallbacks := range metaReadFallbacks {
				canonicalVal := ""
				if orderMeta != nil {
					canonicalVal = orderMeta[canonical]
				}

				if canonicalVal != "" {
					// Canônico preenchido — verificar divergências com legados.
					for _, legacy := range fallbacks[1:] {
						legacyVal := ""
						if orderMeta != nil {
							legacyVal = orderMeta[legacy]
						}
						if legacyVal != "" && legacyVal != canonicalVal {
							batchLog = append(batchLog, metaDivergenceRaw{
								OrderID:        int(orderID),
								Canonical:      canonical,
								CanonicalValue: canonicalVal,
								LegacyField:    legacy,
								LegacyValue:    legacyVal,
								Action:         "divergence",
							})
						}
					}
					continue
				}

				// Canônico vazio — tentar preencher com primeiro fallback com valor.
				for _, legacy := range fallbacks[1:] {
					legacyVal := ""
					if orderMeta != nil {
						legacyVal = orderMeta[legacy]
					}
					if legacyVal != "" {
						action := "would_fill"
						if !in.DryRun {
							action = "filled"
							if upsertErr := h.upsertOrderMeta(ctx, metaTbl, orderID, canonical, legacyVal); upsertErr == nil {
								updated++
								// Atualiza o mapa local para detectar divergências corretas.
								if metaMap[orderIDStr] == nil {
									metaMap[orderIDStr] = make(map[string]string)
								}
								metaMap[orderIDStr][canonical] = legacyVal
							}
						}
						batchLog = append(batchLog, metaDivergenceRaw{
							OrderID:   int(orderID),
							Canonical: canonical,
							FromField: legacy,
							Value:     legacyVal,
							Action:    action,
						})
						break
					}
				}
			}
		}
	}

	nextOffset := in.Offset + in.BatchSize

	// Persiste batchLog no log acumulado (apenas se não for dry_run).
	if !in.DryRun && len(batchLog) > 0 {
		var existing []metaDivergenceRaw
		if raw := h.readOption(ctx, metaDivergenceLogKey); raw != "" {
			_ = json.Unmarshal([]byte(raw), &existing)
		}
		merged := append(existing, batchLog...)
		// Mantém últimas 1000 entradas (espelha PHP: array_slice(..., -1000)).
		if len(merged) > 1000 {
			merged = merged[len(merged)-1000:]
		}
		_ = h.upsertOption(ctx, metaDivergenceLogKey, merged)
	}

	// Persiste last_run (somente se não for dry_run).
	if !in.DryRun {
		lr := metaLastRun{
			At:         time.Now().UTC().Format(time.RFC3339),
			Updated:    updated,
			NextOffset: nextOffset,
		}
		_ = h.upsertOption(ctx, metaNormLastRunKey, lr)
	}

	type normalizeResponse struct {
		Done       int  `json:"done"`
		Updated    int  `json:"updated"`
		NextOffset int  `json:"next_offset"`
		DryRun     bool `json:"dry_run"`
	}

	httpx.JSON(w, http.StatusOK, normalizeResponse{
		Done:       done,
		Updated:    updated,
		NextOffset: nextOffset,
		DryRun:     in.DryRun,
	})
}

// GET /order-meta/divergences?limit=50
// Retorna lista de divergências do log persistido em senderzz_meta_norm_log (key PHP real).
// Decodifica os dois shapes PHP e normaliza para o shape unificado do React.
// Retorna newest-first (espelha array_slice(array_reverse($divergences), 0, 50) do PHP).
func (h *OrderMetaNormalizationHandler) Divergences(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 {
			limit = v
		}
	}

	var rawItems []metaDivergenceRaw
	if raw := h.readOption(ctx, metaDivergenceLogKey); raw != "" {
		_ = json.Unmarshal([]byte(raw), &rawItems)
	}

	// Reverso (newest-first) e trunca ao limite — espelha PHP array_slice(array_reverse(...), 0, 50).
	out := make([]metaDivergenceEntry, 0, limit)
	for i := len(rawItems) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, normalizeRaw(rawItems[i]))
	}

	httpx.JSON(w, http.StatusOK, map[string]any{"items": out})
}

// DELETE /order-meta/divergence-log
// Limpa o log de divergências em senderzz_options (key PHP real: senderzz_meta_norm_log).
func (h *OrderMetaNormalizationHandler) ClearDivergenceLog(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := h.upsertOption(ctx, metaDivergenceLogKey, []metaDivergenceRaw{}); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}
