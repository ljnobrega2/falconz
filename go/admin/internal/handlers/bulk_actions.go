// Package handlers — endpoints de ações em lote para etiquetas.
// Espelha src/Admin/Bulk_Actions/Bulk_Pipeline.php (PHP legado) sobre Postgres.
// Enfileira geração de etiquetas via tabela wc_me_labels; processamento real
// é assíncrono (Asynq). Este handler apenas enfileira e consulta status.
package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

// BulkActionsHandler lida com ações em lote de etiquetas.
type BulkActionsHandler struct{ Pool *pgxpool.Pool }

// tableExistsBulk verifica se tabela existe no schema public.
func (h *BulkActionsHandler) tableExistsBulk(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// bulkOrder representa um pedido elegível para geração em lote.
type bulkOrder struct {
	OrderID        int64   `json:"order_id"`
	CustomerName   string  `json:"customer_name"`
	Status         string  `json:"status"`
	ShippingClass  string  `json:"shipping_class"`
	ShippingClassID *int64 `json:"shipping_class_id"`
	LabelStatus    string  `json:"label_status"` // none|queued|processing|done|error|cancelled
	Total          float64 `json:"total"`
	CreatedAt      string  `json:"created_at"`
}

// shippingClass representa uma classe de envio para o dropdown de filtro.
type shippingClass struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// queueItem representa o status de uma etiqueta na fila.
type queueItem struct {
	OrderID  int64   `json:"order_id"`
	Status   string  `json:"status"` // queued|processing|done|error
	PrintURL *string `json:"print_url"`
	Error    *string `json:"error"`
}

// ListOrders lista pedidos elegíveis para geração de etiqueta em lote.
// GET /bulk-actions/orders?status=&shipping_class=&date_from=&date_to=&limit=200
func (h *BulkActionsHandler) ListOrders(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	status := q.Get("status")
	shippingClass := q.Get("shipping_class")
	dateFrom := q.Get("date_from")
	dateTo := q.Get("date_to")

	// Verifica tabelas necessárias — degradação graciosa se ausentes.
	if !h.tableExistsBulk(ctx, "sz_orders") {
		httpx.JSON(w, 200, map[string]any{"items": []bulkOrder{}, "count": 0})
		return
	}

	hasLabels := h.tableExistsBulk(ctx, "wc_me_labels")

	// Monta SELECT com JOIN opcional para label_status.
	var sb strings.Builder
	sb.WriteString(`
		SELECT
			o.id AS order_id,
			COALESCE(o.customer_name, '') AS customer_name,
			COALESCE(o.status, '') AS status,
			COALESCE(o.shipping_class, '') AS shipping_class,
			o.shipping_class_id,
			COALESCE(o.gross, 0) AS total,
			o.created_at::text`)

	if hasLabels {
		sb.WriteString(`,
			COALESCE(l.status, 'none') AS label_status`)
	} else {
		sb.WriteString(`,
			'none'::text AS label_status`)
	}

	sb.WriteString(`
		FROM sz_orders o`)

	if hasLabels {
		sb.WriteString(`
		LEFT JOIN wc_me_labels l ON l.wc_order_id = o.id`)
	}

	sb.WriteString(`
		WHERE 1=1`)

	args := []any{}
	argN := 1

	if status != "" {
		sb.WriteString(fmt.Sprintf(` AND o.status = $%d`, argN))
		args = append(args, status)
		argN++
	}
	if shippingClass != "" {
		sb.WriteString(fmt.Sprintf(` AND o.shipping_class = $%d`, argN))
		args = append(args, shippingClass)
		argN++
	}
	if dateFrom != "" {
		sb.WriteString(fmt.Sprintf(` AND o.created_at >= $%d`, argN))
		args = append(args, dateFrom)
		argN++
	}
	if dateTo != "" {
		sb.WriteString(fmt.Sprintf(` AND o.created_at <= $%d`, argN))
		args = append(args, dateTo+" 23:59:59")
		argN++
	}

	sb.WriteString(fmt.Sprintf(` ORDER BY o.id DESC LIMIT $%d`, argN))
	args = append(args, limit)

	rows, err := h.Pool.Query(ctx, sb.String(), args...)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []bulkOrder{}
	for rows.Next() {
		var bo bulkOrder
		if err := rows.Scan(
			&bo.OrderID, &bo.CustomerName, &bo.Status,
			&bo.ShippingClass, &bo.ShippingClassID,
			&bo.Total, &bo.CreatedAt, &bo.LabelStatus,
		); err == nil {
			out = append(out, bo)
		}
	}

	httpx.JSON(w, 200, map[string]any{"items": out, "count": len(out)})
}

// generateLabelsRequest corpo do POST /bulk-actions/generate-labels.
type generateLabelsRequest struct {
	OrderIDs []int64 `json:"order_ids"`
	Mode     string  `json:"mode"` // with_pdf | no_pdf | print_batch
}

// GenerateLabels enfileira geração de etiquetas para os pedidos selecionados.
// Processamento real é assíncrono. Este endpoint apenas enfileira (INSERT/UPDATE).
// POST /bulk-actions/generate-labels
func (h *BulkActionsHandler) GenerateLabels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req generateLabelsRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Err(w, 400, "bad_request", "JSON inválido")
		return
	}

	// Valida order_ids.
	if len(req.OrderIDs) == 0 {
		httpx.Err(w, 400, "bad_request", "order_ids não pode ser vazio")
		return
	}
	if len(req.OrderIDs) > 100 {
		httpx.Err(w, 400, "bad_request", "máximo de 100 pedidos por requisição")
		return
	}

	// Valida modo.
	validModes := map[string]bool{"with_pdf": true, "no_pdf": true, "print_batch": true}
	if !validModes[req.Mode] {
		httpx.Err(w, 400, "bad_request", "mode inválido: use with_pdf, no_pdf ou print_batch")
		return
	}

	// Verifica tabela de etiquetas — degradação graciosa.
	if !h.tableExistsBulk(ctx, "wc_me_labels") {
		httpx.Err(w, 503, "table_missing", "tabela wc_me_labels ainda não migrada")
		return
	}

	queued := 0
	alreadyQueued := 0
	var errs []string

	now := time.Now()

	for _, orderID := range req.OrderIDs {
		// Verifica status atual para distinguir "já na fila" de "novo".
		var currentStatus string
		err := h.Pool.QueryRow(ctx,
			`SELECT status FROM wc_me_labels WHERE wc_order_id = $1`, orderID,
		).Scan(&currentStatus)

		if err == nil && (currentStatus == "queued" || currentStatus == "processing") {
			alreadyQueued++
			continue
		}

		// INSERT ... ON CONFLICT: atualiza para queued se já existir com outro status.
		_, err = h.Pool.Exec(ctx,
			`INSERT INTO wc_me_labels (wc_order_id, status, mode, created_at, updated_at)
			 VALUES ($1, 'queued', $2, $3, $3)
			 ON CONFLICT (wc_order_id)
			 DO UPDATE SET status = 'queued', mode = EXCLUDED.mode, updated_at = EXCLUDED.updated_at`,
			orderID, req.Mode, now)
		if err != nil {
			errs = append(errs, fmt.Sprintf("pedido %d: %s", orderID, err.Error()))
			continue
		}
		queued++
	}

	httpx.JSON(w, 200, map[string]any{
		"ok":            len(errs) == 0,
		"queued":        queued,
		"already_queued": alreadyQueued,
		"errors":        errs,
	})
}

// QueueStatus retorna o status de etiquetas para os pedidos informados.
// GET /bulk-actions/queue-status?order_ids=1,2,3
func (h *BulkActionsHandler) QueueStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	rawIDs := r.URL.Query().Get("order_ids")
	if rawIDs == "" {
		httpx.Err(w, 400, "bad_request", "order_ids é obrigatório")
		return
	}

	// Parse dos IDs separados por vírgula.
	parts := strings.Split(rawIDs, ",")
	ids := make([]int64, 0, len(parts))
	for _, p := range parts {
		id, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
		if err == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		httpx.Err(w, 400, "bad_request", "nenhum order_id válido")
		return
	}

	// Tabela ausente → retorna "none" para todos.
	if !h.tableExistsBulk(ctx, "wc_me_labels") {
		items := make([]queueItem, 0, len(ids))
		for _, id := range ids {
			items = append(items, queueItem{OrderID: id, Status: "none"})
		}
		httpx.JSON(w, 200, map[string]any{"items": items})
		return
	}

	// Monta cláusula IN com placeholders.
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	inClause := strings.Join(placeholders, ",")

	rows, err := h.Pool.Query(ctx,
		fmt.Sprintf(`
			SELECT wc_order_id, COALESCE(status,'none'), print_url, error_message
			FROM wc_me_labels
			WHERE wc_order_id IN (%s)`, inClause),
		args...)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	// Mapeia resultados por order_id.
	found := map[int64]queueItem{}
	for rows.Next() {
		var qi queueItem
		_ = rows.Scan(&qi.OrderID, &qi.Status, &qi.PrintURL, &qi.Error)
		found[qi.OrderID] = qi
	}

	// Retorna item para cada ID pedido — "none" para os não encontrados.
	items := make([]queueItem, 0, len(ids))
	for _, id := range ids {
		if qi, ok := found[id]; ok {
			items = append(items, qi)
		} else {
			items = append(items, queueItem{OrderID: id, Status: "none"})
		}
	}

	httpx.JSON(w, 200, map[string]any{"items": items})
}

// ShippingClasses lista classes de envio disponíveis para o filtro.
// GET /bulk-actions/shipping-classes
func (h *BulkActionsHandler) ShippingClasses(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Tabela ausente → lista vazia.
	if !h.tableExistsBulk(ctx, "sz_orders") {
		httpx.JSON(w, 200, map[string]any{"items": []shippingClass{}})
		return
	}

	// Tenta buscar de tabela dedicada; fallback para DISTINCT em sz_orders.
	hasSCTable := h.tableExistsBulk(ctx, "sz_shipping_classes")

	var items []shippingClass

	if hasSCTable {
		rows, err := h.Pool.Query(ctx,
			`SELECT id, COALESCE(name,'') FROM sz_shipping_classes ORDER BY name`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var sc shippingClass
				_ = rows.Scan(&sc.ID, &sc.Name)
				items = append(items, sc)
			}
		}
	} else {
		// Fallback: DISTINCT a partir de sz_orders.
		rows, err := h.Pool.Query(ctx,
			`SELECT DISTINCT
				COALESCE(shipping_class_id, 0) AS id,
				COALESCE(shipping_class, 'Padrão') AS name
			 FROM sz_orders
			 WHERE shipping_class IS NOT NULL AND shipping_class != ''
			 ORDER BY name`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var sc shippingClass
				_ = rows.Scan(&sc.ID, &sc.Name)
				items = append(items, sc)
			}
		}
	}

	if items == nil {
		items = []shippingClass{}
	}

	httpx.JSON(w, 200, map[string]any{"items": items})
}
