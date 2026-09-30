// Package handlers — Bip/SKU na EXPEDIÇÃO (modalidade operada pelo OL).
//
// Rota (namespace /wp-json/senderzz/v1):
//
//	POST /portal/expedicao/bipar — valida o SKU bipado contra o SKU do pedido
//
// ── REGRA (verbatim do dono) ───────────────────────────────────────────────────
// "validação de sku do produto com o que ta no pedido se for diferente nao deixa
// seguir". Bipar o código de barras lê um SKU; comparamos com o(s) SKU(s)
// esperado(s) do pedido (sz_order_items.sku). Se NÃO casar com nenhum item →
// 422 (PT-BR "SKU não confere com o pedido."), NÃO avança a ação. Digitar o
// código manualmente é fallback ACEITO (manual_typed=true).
//
// SEMPRE registra a leitura em sz_pack_scans (context='expedicao'), inclusive no
// caminho de bloqueio (matched=false ANTES do 422) — a auditoria nunca é pulada.
//
// ── Por que vertical slice (não marca embalado aqui) ───────────────────────────
// expedicao.go é READ-ONLY quanto a status/carteira: marcar embalado dispara
// efeito de carteira TPC via hook WC e segue no WP-AJAX durante a migração (ver
// header de expedicao.go). Este endpoint faz SÓ a validação + registro e devolve
// ok/422 — o front bipa, recebe o veredito e só então chama o marcar-embalado do
// WP. É a fatia utilizável da regra sem invadir o caminho financeiro.
//
// ── id-space (canonical — idêntico a expedicao.go/orders.go) ───────────────────
//   - body.wc_order_id  = sz_orders.wp_order_id  (ID WooCommerce do pedido)
//   - sz_order_items.order_id = sz_orders.id     (id portal/sz — NÃO o wc id)
//   Por isso resolvemos wc_order_id → sz_orders.id ANTES de ler os itens.
//
// ── Escopo (fail-closed) ───────────────────────────────────────────────────────
// Só o PRODUTOR dono do pedido bipa (o.produtor_id = u.ID). Afiliado → 403
// (mesmo guard da Expedição). Pedido inexistente/fora de escopo → 404 (não
// vaza existência de pedido alheio).
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/senderzz/portal-service/internal/auth"
	"github.com/senderzz/portal-service/internal/httpx"
)

// biparRequest — body de POST /portal/expedicao/bipar.
type biparRequest struct {
	WCOrderID   int64  `json:"wc_order_id"`
	SKU         string `json:"sku"`          // SKU lido do código de barras (ou digitado)
	ManualTyped bool   `json:"manual_typed"` // true = digitou o código (fallback aceito)
}

// Bipar valida o SKU bipado contra os SKUs do pedido e registra a leitura.
//
// 200 { ok:true, matched:true, ... }   → SKU confere (pode seguir p/ embalar).
// 422 { ok:false, matched:false, ... } → "SKU não confere com o pedido." (bloqueia).
// 404                                  → pedido inexistente ou fora do escopo do produtor.
func (h *ExpedicaoHandler) Bipar(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	ctx := r.Context()

	// Afiliado nunca opera Expedição — fail-closed (espelha expedicao.php / List).
	if u.Role == "afiliado" || u.Role == "affiliate" || u.Role == "afiliada" || u.Role == "cliente" {
		httpx.WriteErr(w, http.StatusForbidden, "Expedição não está disponível no seu perfil.")
		return
	}

	var req biparRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}
	skuScanned := strings.TrimSpace(req.SKU)
	if req.WCOrderID <= 0 || skuScanned == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "Informe wc_order_id e sku.")
		return
	}

	if !h.tableExists(ctx, "sz_orders") || !h.tableExists(ctx, "sz_order_items") {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "expedição ainda não migrada")
		return
	}

	// Resolve wc_order_id → sz_orders.id + valida propriedade do produtor.
	// produtor_id = u.ID (id portal) — MESMO recorte de expedicao.go.
	var szOrderID int64
	err := h.Pool.QueryRow(ctx,
		`SELECT id FROM sz_orders
		  WHERE wp_order_id = $1 AND produtor_id = $2
		  LIMIT 1`,
		req.WCOrderID, u.ID,
	).Scan(&szOrderID)
	if errors.Is(err, pgx.ErrNoRows) {
		// Não vaza existência de pedido de outro produtor.
		httpx.WriteErr(w, http.StatusNotFound, "Pedido não encontrado.")
		return
	}
	if err != nil {
		slog.Error("[portal_expedicao_bipar] erro ao resolver pedido", "user_id", u.ID, "wc_order_id", req.WCOrderID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// SKUs esperados = todos os itens do pedido (não só o 1º). Casa se o bipado ∈ set.
	// product_id e sku_expected guardados p/ auditoria (1º item como referência).
	matched, expectedSample, productID := h.matchOrderSKU(ctx, szOrderID, skuScanned)

	// Registra SEMPRE (inclusive no bloqueio) — auditoria nunca é pulada.
	h.recordScan(ctx, packScan{
		Context:     "expedicao",
		WCOrderID:   &req.WCOrderID,
		ProductID:   productID,
		SKUScanned:  skuScanned,
		SKUExpected: expectedSample,
		Matched:     matched,
		Quantity:    1,
		ManualTyped: req.ManualTyped,
		Actor:       u.Email,
	})

	if !matched {
		// 422 — bloqueia o avanço. Front NÃO deve chamar o marcar-embalado.
		slog.Info("[portal_expedicao_bipar] SKU não confere — bloqueado",
			"user_id", u.ID, "sz_order_id", szOrderID, "sku_scanned", skuScanned)
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "SKU não confere com o pedido.")
		return
	}

	slog.Info("[portal_expedicao_bipar] SKU confere",
		"user_id", u.ID, "sz_order_id", szOrderID, "manual_typed", req.ManualTyped)
	httpx.WriteOK(w, map[string]any{
		"ok":           true,
		"matched":      true,
		"wc_order_id":  req.WCOrderID,
		"sku":          skuScanned,
		"manual_typed": req.ManualTyped,
		"mensagem":     "SKU confere com o pedido.",
	})
}

// matchOrderSKU verifica se o SKU bipado casa com ALGUM item do pedido.
// Retorna (matched, sampleExpectedSKU, productIDDoItemCasado|1ºItem).
// Comparação case-insensitive e trim (código de barras pode trazer espaços/caixa).
func (h *ExpedicaoHandler) matchOrderSKU(ctx context.Context, szOrderID int64, skuScanned string) (bool, *string, *int64) {
	rows, err := h.Pool.Query(ctx,
		`SELECT produto_id, COALESCE(sku,'') FROM sz_order_items
		  WHERE order_id = $1
		  ORDER BY id ASC`,
		szOrderID,
	)
	if err != nil {
		slog.Error("[portal_expedicao_bipar] erro ao ler itens do pedido", "sz_order_id", szOrderID, "err", err)
		return false, nil, nil
	}
	defer rows.Close()

	want := strings.ToLower(strings.TrimSpace(skuScanned))
	var (
		firstProductID *int64
		firstSKU       *string
	)
	for rows.Next() {
		var (
			pid *int64
			sku string
		)
		if err := rows.Scan(&pid, &sku); err != nil {
			continue
		}
		// Guarda o 1º item como referência de auditoria (sku_expected/product_id).
		if firstProductID == nil {
			firstProductID = pid
			s := sku
			firstSKU = &s
		}
		if sku != "" && strings.ToLower(strings.TrimSpace(sku)) == want {
			pidCopy := pid
			skuCopy := sku
			return true, &skuCopy, pidCopy
		}
	}
	return false, firstSKU, firstProductID
}
