// Package handlers — leitura de estoque REAL do produtor (sz_stock). // FEAT-PORTAL
//
// Rota (namespace /wp-json/senderzz/v1):
//
//	GET /portal/producer-stock — estoque real dos produtos do produtor logado
//
// DISTINTO de GET /portal/stock (stock_portal.go): aquele handler é o espelho
// READ-ONLY da tela "Estoque" do .php e retorna KPIs ZERADOS por design (predata
// sz_stock e avisa explicitamente contra derivar números por outro caminho). Este
// endpoint NOVO lê a tabela sz_stock real (criada em schema-fixes-v471-stock.sql,
// alimentada pelo trigger trg_stock_pedido_*). NÃO fundir os dois — seriam duas
// "verdades" de estoque divergentes (o header de stock_portal.go alerta sobre isso).
//
// ── ID-SPACE (verificado contra o migrador — NÃO improvisar) ───────────────────
// sz_stock.product_id é populado pelo trigger a partir de sz_order_items.produto_id,
// que o migrador define como itemmeta _product_id (migrate-wp-to-pg.py:376) — ou
// seja, é o WC product id (= WP post id). Os produtos do produtor casam por
// COALESCE(sp.wp_post_id, sp.id), EXATAMENTE como products.go/stock.go fazem o join
// de afiliados (a.produto_id = COALESCE(sp.wp_post_id, sp.id)). Join por sp.id
// estaria errado (id-space diferente → 0 linhas / linhas erradas).
//
// ── DERIVADOS ──────────────────────────────────────────────────────────────────
//   - qty_sellable NÃO é coluna — = GREATEST(qty_available - qty_reserved, 0).
//   - cd_nome via LEFT JOIN sz_motoboy_cds (linhas com cd_id=0 default não casam →
//     cd_nome vazio; NÃO derrubar a linha por isso).
//
// Escopo: SOMENTE produtor (sp.produtor_id = u.ID). afiliado/operator → 403
// (estoque físico é dado do produtor — não vaza para afiliado, por construção).
//
// Degradação graciosa: sz_stock / sz_products ausente (42P01) → items:[] (200),
// não 500 — mantém a tela renderizável durante a janela de migração.
package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/portal-service/internal/auth"
	"github.com/senderzz/portal-service/internal/httpx"
)

// StockProducerHandler agrupa as dependências do handler de estoque do produtor.
type StockProducerHandler struct {
	Pool *pgxpool.Pool
}

// producerStockItem — uma linha de estoque (produto × CD) do produtor.
type producerStockItem struct {
	ProductID         int64  `json:"product_id"`   // sz_products.id (id do espelho)
	ProductName       string `json:"product_name"` // sz_products.nome
	CDID              int64  `json:"cd_id"`        // sz_stock.cd_id (0 = sem CD definido)
	CDNome            string `json:"cd_nome"`      // "" quando cd_id=0 ou CD ausente
	QtyAvailable      int64  `json:"qty_available"`
	QtyReserved       int64  `json:"qty_reserved"`
	QtySellable       int64  `json:"qty_sellable"` // = max(available - reserved, 0)
	LowStockThreshold int64  `json:"low_stock_threshold"`
}

// ── GET /portal/producer-stock ────────────────────────────────────────────────── // FEAT-PORTAL

// List retorna o estoque real consolidado dos produtos do produtor logado.
// Expedição e COD compartilham o mesmo estoque físico; CDs são somados aqui.
func (h *StockProducerHandler) List(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	// Estoque físico é dado do produtor — só produtor acessa (não vaza p/ afiliado).
	if u.Role != "produtor" {
		httpx.WriteErr(w, http.StatusForbidden, "apenas o produtor pode consultar o estoque")
		return
	}

	rows, err := h.Pool.Query(r.Context(),
		`SELECT sp.id,
		        sp.nome,
		        0 AS cd_id,
		        'Estoque único' AS cd_nome,
		        SUM(st.qty_available)::bigint,
		        SUM(st.qty_reserved)::bigint,
		        GREATEST(SUM(st.qty_available) - SUM(st.qty_reserved), 0)::bigint AS qty_sellable,
		        MAX(st.low_stock_threshold)::bigint
		   FROM sz_stock st
		   JOIN sz_products sp
		     ON sp.produtor_id = $1
		    AND st.product_id = COALESCE(sp.wp_post_id, sp.id)
		    AND sp.status IS DISTINCT FROM 'deleted'
		  GROUP BY sp.id, sp.nome
		  ORDER BY sp.nome ASC`,
		u.ID,
	)
	if err != nil {
		// sz_stock / sz_products / sz_motoboy_cds ausente → lista vazia (degradação graciosa).
		if isUndefinedTable(err) {
			httpx.WriteOK(w, map[string]any{"items": []producerStockItem{}, "total": 0})
			return
		}
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer rows.Close()

	items := []producerStockItem{}
	for rows.Next() {
		var it producerStockItem
		if err := rows.Scan(
			&it.ProductID, &it.ProductName, &it.CDID, &it.CDNome,
			&it.QtyAvailable, &it.QtyReserved, &it.QtySellable, &it.LowStockThreshold,
		); err != nil {
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao ler estoque")
			return
		}
		items = append(items, it)
	}
	if rows.Err() != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao processar estoque")
		return
	}

	httpx.WriteOK(w, map[string]any{"items": items, "total": len(items)})
}

// ── POST /portal/producer-stock/shipments ──────────────────────────────────── // FEAT-STOCK-SHIPMENTS
//
// Cria uma remessa (status='pendente') do produtor logado pro CD — paridade com
// o ciclo pending do senderzz-stock-shipments.php legado. NÃO credita sz_stock
// aqui (crédito só acontece quando o admin confirma o recebimento físico, ver
// go/admin/internal/handlers/stock_shipments.go Confirm).
type shipmentItemBody struct {
	ProductID   int64 `json:"product_id"`
	VariationID int64 `json:"variation_id"`
	Quantity    int64 `json:"quantity"`
}

type createShipmentBody struct {
	CDID   int64              `json:"cd_id"`
	Motivo string             `json:"motivo"`
	Items  []shipmentItemBody `json:"items"`
}

func (h *StockProducerHandler) CreateShipment(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	if u.Role != "produtor" {
		httpx.WriteErr(w, http.StatusForbidden, "apenas o produtor pode criar remessa de estoque")
		return
	}

	var b createShipmentBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	if len(b.Items) == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "remessa precisa de ao menos 1 item")
		return
	}
	for _, it := range b.Items {
		if it.ProductID <= 0 || it.Quantity <= 0 {
			httpx.WriteErr(w, http.StatusBadRequest, "cada item precisa de product_id e quantity válidos")
			return
		}
	}

	ctx := r.Context()
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var shipmentID int64
	if err := tx.QueryRow(ctx,
		`INSERT INTO sz_stock_shipments (producer_id, cd_id, motivo, status)
		 VALUES ($1, $2, $3, 'pendente') RETURNING id`,
		u.ID, b.CDID, b.Motivo,
	).Scan(&shipmentID); err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao criar remessa")
		return
	}

	for _, it := range b.Items {
		if _, err := tx.Exec(ctx,
			`INSERT INTO sz_stock_shipment_items (shipment_id, product_id, variation_id, quantity)
			 VALUES ($1, $2, $3, $4)`,
			shipmentID, it.ProductID, it.VariationID, it.Quantity,
		); err != nil {
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao gravar item da remessa")
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	httpx.WriteOK(w, map[string]any{"shipment_id": shipmentID, "status": "pendente"})
}

// ── POST /portal/producer-stock/shipments/{id}/send ─────────────────────────── // FEAT-STOCK-SHIPMENTS
//
// pendente → enviado. Só o dono da remessa pode marcar como despachada.
func (h *StockProducerHandler) SendShipment(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	if u.Role != "produtor" {
		httpx.WriteErr(w, http.StatusForbidden, "apenas o produtor pode despachar remessa de estoque")
		return
	}

	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if id <= 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}

	ctx := r.Context()
	tag, err := h.Pool.Exec(ctx,
		`UPDATE sz_stock_shipments
		    SET status = 'enviado', sent_at = NOW(), updated_at = NOW()
		  WHERE id = $1 AND producer_id = $2 AND status = 'pendente'`,
		id, u.ID,
	)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "remessa não encontrada ou não está pendente")
		return
	}

	httpx.WriteOK(w, map[string]any{"shipment_id": id, "status": "enviado"})
}

// listShipments — GET /portal/producer-stock/shipments — remessas do produtor logado.
func (h *StockProducerHandler) ListShipments(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	if u.Role != "produtor" {
		httpx.WriteErr(w, http.StatusForbidden, "apenas o produtor pode consultar remessas de estoque")
		return
	}

	rows, err := h.Pool.Query(r.Context(),
		`SELECT id, cd_id, status, COALESCE(motivo,''), created_at::text, updated_at::text
		   FROM sz_stock_shipments WHERE producer_id = $1 ORDER BY id DESC LIMIT 200`,
		u.ID,
	)
	if err != nil {
		if isUndefinedTable(err) {
			httpx.WriteOK(w, map[string]any{"items": []any{}})
			return
		}
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer rows.Close()

	type shipmentRow struct {
		ID        int64  `json:"id"`
		CDID      int64  `json:"cd_id"`
		Status    string `json:"status"`
		Motivo    string `json:"motivo"`
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
	}
	items := []shipmentRow{}
	for rows.Next() {
		var it shipmentRow
		if err := rows.Scan(&it.ID, &it.CDID, &it.Status, &it.Motivo, &it.CreatedAt, &it.UpdatedAt); err != nil {
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao ler remessas")
			return
		}
		items = append(items, it)
	}
	httpx.WriteOK(w, map[string]any{"items": items})
}
