// Handler de REMESSAS DE ESTOQUE produtor→CD (sz_stock_shipments). FEAT-STOCK-SHIPMENTS
//
// Paridade com includes/senderzz-stock-shipments.php (legado): ciclo
// pendente→enviado→confirmado→concluido. Criação/despacho ficam no portal
// (go/portal stock_producer_portal.go — só o produtor dono cria/envia); aqui só
// as ações do lado CD/admin: listar, ver detalhe, confirmar recebimento (credita
// sz_stock) e concluir.
//
// GET  /stock-shipments               → lista (filtro opcional ?status=)
// GET  /stock-shipments/{id}          → detalhe + itens
// POST /stock-shipments/{id}/confirm  → enviado→confirmado; credita sz_stock (tipo='entrada')
// POST /stock-shipments/{id}/conclude → confirmado→concluido (housekeeping, sem efeito em estoque)
package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/auth"
	"github.com/senderzz/admin-service/internal/httpx"
)

// StockShipmentHandler expõe o ciclo de remessa de estoque produtor→CD.
type StockShipmentHandler struct{ Pool *pgxpool.Pool }

type shipmentListItem struct {
	ID          int64  `json:"id"`
	ProducerID  int64  `json:"producer_id"`
	CdID        int64  `json:"cd_id"`
	Status      string `json:"status"`
	Motivo      string `json:"motivo"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
	ConfirmedAt string `json:"confirmed_at,omitempty"`
}

// List — GET /stock-shipments?status=
func (h *StockShipmentHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !tableExistsCached(ctx, h.Pool, "sz_stock_shipments") {
		httpx.JSON(w, 200, map[string]any{"ok": true, "items": []shipmentListItem{}})
		return
	}

	status := strings.TrimSpace(r.URL.Query().Get("status"))

	rows, err := h.Pool.Query(ctx,
		`SELECT id, producer_id, cd_id, status, COALESCE(motivo,''),
		        created_at::text, updated_at::text, COALESCE(confirmed_at::text,'')
		   FROM sz_stock_shipments
		  WHERE ($1 = '' OR status = $1)
		  ORDER BY id DESC LIMIT 200`,
		status)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	items := []shipmentListItem{}
	for rows.Next() {
		var it shipmentListItem
		if err := rows.Scan(&it.ID, &it.ProducerID, &it.CdID, &it.Status, &it.Motivo,
			&it.CreatedAt, &it.UpdatedAt, &it.ConfirmedAt); err != nil {
			httpx.Err(w, 500, "scan_error", err.Error())
			return
		}
		items = append(items, it)
	}
	httpx.JSON(w, 200, map[string]any{"ok": true, "items": items})
}

type shipmentItemRow struct {
	ID          int64 `json:"id"`
	ProductID   int64 `json:"product_id"`
	VariationID int64 `json:"variation_id"`
	Quantity    int64 `json:"quantity"`
}

// Detail — GET /stock-shipments/{id}
func (h *StockShipmentHandler) Detail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if id <= 0 {
		httpx.Err(w, 400, "validation", "id inválido")
		return
	}

	var it shipmentListItem
	err := h.Pool.QueryRow(ctx,
		`SELECT id, producer_id, cd_id, status, COALESCE(motivo,''),
		        created_at::text, updated_at::text, COALESCE(confirmed_at::text,'')
		   FROM sz_stock_shipments WHERE id = $1`, id,
	).Scan(&it.ID, &it.ProducerID, &it.CdID, &it.Status, &it.Motivo,
		&it.CreatedAt, &it.UpdatedAt, &it.ConfirmedAt)
	if err == pgx.ErrNoRows {
		httpx.Err(w, 404, "not_found", "remessa não encontrada")
		return
	}
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	rows, err := h.Pool.Query(ctx,
		`SELECT id, product_id, variation_id, quantity
		   FROM sz_stock_shipment_items WHERE shipment_id = $1 ORDER BY id ASC`, id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	items := []shipmentItemRow{}
	for rows.Next() {
		var si shipmentItemRow
		if err := rows.Scan(&si.ID, &si.ProductID, &si.VariationID, &si.Quantity); err != nil {
			httpx.Err(w, 500, "scan_error", err.Error())
			return
		}
		items = append(items, si)
	}

	httpx.JSON(w, 200, map[string]any{"ok": true, "shipment": it, "items": items})
}

// Confirm — POST /stock-shipments/{id}/confirm
//
// enviado→confirmado. Credita sz_stock para cada item da remessa (tipo='entrada',
// mesma mecânica de Create/Bipar — UPSERT + linha em sz_stock_movements). Não
// aceita remessa que não esteja em 'enviado' (evita confirmar 2x / pular etapa).
func (h *StockShipmentHandler) Confirm(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if id <= 0 {
		httpx.Err(w, 400, "validation", "id inválido")
		return
	}

	admin := auth.FromCtx(ctx)
	var adminID *int64
	if admin != nil {
		adminID = &admin.ID
	}

	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var producerID, cdID int64
	var status string
	if err := tx.QueryRow(ctx,
		`SELECT producer_id, cd_id, status FROM sz_stock_shipments WHERE id = $1 FOR UPDATE`,
		id).Scan(&producerID, &cdID, &status); err != nil {
		if err == pgx.ErrNoRows {
			httpx.Err(w, 404, "not_found", "remessa não encontrada")
			return
		}
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if status != "enviado" {
		httpx.Err(w, 422, "invalid_status",
			"só é possível confirmar remessa no status 'enviado' (atual: "+status+")")
		return
	}

	rows, err := tx.Query(ctx,
		`SELECT product_id, variation_id, quantity FROM sz_stock_shipment_items WHERE shipment_id = $1`,
		id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	type item struct {
		productID, variationID, quantity int64
	}
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.productID, &it.variationID, &it.quantity); err != nil {
			rows.Close()
			httpx.Err(w, 500, "scan_error", err.Error())
			return
		}
		items = append(items, it)
	}
	rows.Close()

	for _, it := range items {
		if _, err := tx.Exec(ctx,
			`INSERT INTO sz_stock (product_id, variation_id, cd_id, qty_available, updated_at)
			 VALUES ($1, $2, $3, $4, NOW())
			 ON CONFLICT (product_id, variation_id, cd_id) DO UPDATE
			    SET qty_available = sz_stock.qty_available + EXCLUDED.qty_available,
			        updated_at    = NOW()`,
			it.productID, it.variationID, cdID, it.quantity,
		); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO sz_stock_movements
			    (product_id, variation_id, cd_id, delta_available, delta_reserved, tipo, motivo, created_at)
			 VALUES ($1, $2, $3, $4, 0, 'entrada', $5, NOW())`,
			it.productID, it.variationID, cdID, it.quantity,
			"remessa #"+strconv.FormatInt(id, 10)+" confirmada (produtor #"+strconv.FormatInt(producerID, 10)+")",
		); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
	}

	if _, err := tx.Exec(ctx,
		`UPDATE sz_stock_shipments
		    SET status = 'confirmado', confirmed_at = NOW(), confirmed_by = $2, updated_at = NOW()
		  WHERE id = $1`,
		id, adminID,
	); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 200, map[string]any{"ok": true, "shipment_id": id, "status": "confirmado"})
}

// Conclude — POST /stock-shipments/{id}/conclude
//
// confirmado→concluido. Estado terminal, housekeeping — não mexe em sz_stock
// (crédito já aconteceu no Confirm).
func (h *StockShipmentHandler) Conclude(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if id <= 0 {
		httpx.Err(w, 400, "validation", "id inválido")
		return
	}

	tag, err := h.Pool.Exec(ctx,
		`UPDATE sz_stock_shipments
		    SET status = 'concluido', concluded_at = NOW(), updated_at = NOW()
		  WHERE id = $1 AND status = 'confirmado'`,
		id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.Err(w, 422, "invalid_status", "só é possível concluir remessa já confirmada")
		return
	}

	httpx.JSON(w, 200, map[string]any{"ok": true, "shipment_id": id, "status": "concluido"})
}
