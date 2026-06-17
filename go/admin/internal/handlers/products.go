// Handler CRUD de produtos (sz_products).
//
// GET    /products?q=&produtor_id=&status=&limit=100&offset=0&auto_sync=1
// GET    /products/stats
// POST   /products
// POST   /products/sync-from-orders
// PUT    /products/{id}
// DELETE /products/{id}
package handlers

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

// ProductsHandler expõe CRUD para sz_products.
type ProductsHandler struct{ Pool *pgxpool.Pool }

// Product é o shape retornado pela API.
type Product struct {
	ID             int64   `json:"id"`
	WPPostID       *int64  `json:"wp_post_id"`
	ProdutorID     int64   `json:"produtor_id"`
	ProdutorNome   string  `json:"produtor_nome"`
	Nome           string  `json:"nome"`
	SKU            *string `json:"sku"`
	Preco          float64 `json:"preco"`
	Descricao      *string `json:"descricao"`
	Categoria      *string `json:"categoria"`
	Status         string  `json:"status"`
	CreatedAt      string  `json:"created_at"`
	AfiliadosCount int64   `json:"afiliados_count"`
}

// tableExistsProducts retorna true se sz_products já existe no banco.
func (h *ProductsHandler) tableExistsProducts(r *http.Request) bool {
	return h.tableExists(r.Context(), "sz_products")
}

// tableExists — checagem genérica para qualquer tabela public.<name>.
func (h *ProductsHandler) tableExists(ctx context.Context, name string) bool {
	var exists bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = $1
		)`, name).Scan(&exists)
	return exists
}

// List — GET /products
func (h *ProductsHandler) List(w http.ResponseWriter, r *http.Request) {
	// Degradação graciosa: tabela ainda não existe.
	if !h.tableExistsProducts(r) {
		httpx.JSON(w, 200, map[string]any{"items": []Product{}, "total": int64(0)})
		return
	}

	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	search := strings.TrimSpace(q.Get("q"))
	status := strings.TrimSpace(q.Get("status"))
	produtorID, _ := strconv.ParseInt(q.Get("produtor_id"), 10, 64)
	autoSync := q.Get("auto_sync") == "1"

	// Auto-trigger opt-in: se nenhum produto cadastrado e o cliente pediu auto_sync,
	// importa do histórico de pedidos antes de listar. Evita surpresa silenciosa.
	if autoSync && h.tableExists(r.Context(), "sz_order_items") && h.tableExists(r.Context(), "sz_orders") {
		var existing int64
		_ = h.Pool.QueryRow(r.Context(), `SELECT COUNT(*) FROM sz_products`).Scan(&existing)
		if existing == 0 {
			_, _ = h.runSyncFromOrders(r.Context())
		}
	}

	rows, err := h.Pool.Query(r.Context(),
		`SELECT
		    sp.id,
		    sp.wp_post_id,
		    sp.produtor_id,
		    COALESCE(pu.nome, pu.email, '') AS produtor_nome,
		    sp.nome,
		    sp.sku,
		    sp.preco::float8,
		    sp.descricao,
		    sp.categoria,
		    sp.status,
		    sp.created_at::text,
		    -- senderzz_affiliates.produto_id é wp_post_id; com fallback para sp.id quando wp_post_id é NULL
		    (SELECT COUNT(*) FROM senderzz_affiliates sa
		        WHERE sa.produto_id = COALESCE(sp.wp_post_id, sp.id)) AS afiliados_count
		FROM sz_products sp
		LEFT JOIN senderzz_portal_users pu ON pu.id = sp.produtor_id
		WHERE ($1 = '' OR sp.nome ILIKE '%' || $1 || '%')
		  AND ($2 = '' OR sp.status = $2)
		  AND ($3 = 0  OR sp.produtor_id = $3)
		ORDER BY sp.id DESC
		LIMIT $4 OFFSET $5`,
		search, status, produtorID, limit, offset)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	items := []Product{}
	for rows.Next() {
		var p Product
		if err := rows.Scan(
			&p.ID, &p.WPPostID, &p.ProdutorID, &p.ProdutorNome,
			&p.Nome, &p.SKU, &p.Preco, &p.Descricao, &p.Categoria,
			&p.Status, &p.CreatedAt, &p.AfiliadosCount,
		); err != nil {
			httpx.Err(w, 500, "scan_error", err.Error())
			return
		}
		items = append(items, p)
	}

	var total int64
	_ = h.Pool.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM sz_products sp
		 WHERE ($1 = '' OR sp.nome ILIKE '%' || $1 || '%')
		   AND ($2 = '' OR sp.status = $2)
		   AND ($3 = 0  OR sp.produtor_id = $3)`,
		search, status, produtorID).Scan(&total)

	httpx.JSON(w, 200, map[string]any{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// productBody é o payload de criação/edição.
type productBody struct {
	WPPostID   *int64   `json:"wp_post_id"`
	ProdutorID int64    `json:"produtor_id"`
	Nome       string   `json:"nome"`
	SKU        *string  `json:"sku"`
	Preco      float64  `json:"preco"`
	Descricao  *string  `json:"descricao"`
	Categoria  *string  `json:"categoria"`
	Status     string   `json:"status"`
}

// Create — POST /products
func (h *ProductsHandler) Create(w http.ResponseWriter, r *http.Request) {
	if !h.tableExistsProducts(r) {
		httpx.Err(w, 503, "table_not_found", "tabela sz_products não existe — execute a migration v462")
		return
	}

	var b productBody
	if err := httpx.DecodeJSON(r, &b); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	if strings.TrimSpace(b.Nome) == "" {
		httpx.Err(w, 400, "validation", "campo nome é obrigatório")
		return
	}
	if b.ProdutorID == 0 {
		httpx.Err(w, 400, "validation", "campo produtor_id é obrigatório")
		return
	}
	if b.Status == "" {
		b.Status = "active"
	}

	var id int64
	err := h.Pool.QueryRow(r.Context(),
		`INSERT INTO sz_products (wp_post_id, produtor_id, nome, sku, preco, descricao, categoria, status)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		 RETURNING id`,
		b.WPPostID, b.ProdutorID, b.Nome, b.SKU, b.Preco, b.Descricao, b.Categoria, b.Status,
	).Scan(&id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	httpx.JSON(w, 201, map[string]any{"id": id})
}

// Update — PUT /products/{id}
func (h *ProductsHandler) Update(w http.ResponseWriter, r *http.Request) {
	if !h.tableExistsProducts(r) {
		httpx.Err(w, 503, "table_not_found", "tabela sz_products não existe — execute a migration v462")
		return
	}

	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var b productBody
	if err := httpx.DecodeJSON(r, &b); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	if strings.TrimSpace(b.Nome) == "" {
		httpx.Err(w, 400, "validation", "campo nome é obrigatório")
		return
	}

	_, err := h.Pool.Exec(r.Context(),
		`UPDATE sz_products
		 SET wp_post_id = COALESCE($1, wp_post_id),
		     produtor_id = $2,
		     nome        = $3,
		     sku         = $4,
		     preco       = $5,
		     descricao   = $6,
		     categoria   = $7,
		     status      = $8,
		     updated_at  = NOW()
		 WHERE id = $9`,
		b.WPPostID, b.ProdutorID, b.Nome, b.SKU, b.Preco, b.Descricao, b.Categoria, b.Status, id,
	)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	httpx.JSON(w, 200, map[string]any{"ok": true})
}

// Delete — DELETE /products/{id}
func (h *ProductsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if !h.tableExistsProducts(r) {
		httpx.Err(w, 503, "table_not_found", "tabela sz_products não existe — execute a migration v462")
		return
	}

	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	_, err := h.Pool.Exec(r.Context(), `DELETE FROM sz_products WHERE id=$1`, id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	httpx.JSON(w, 200, map[string]any{"ok": true})
}

// runSyncFromOrders — executa o INSERT…SELECT idempotente que importa produtos
// distintos do histórico de sz_order_items. Retorna a contagem de linhas inseridas.
//
// Usado por SyncFromOrders e pelo auto-trigger do List (?auto_sync=1).
//
// NOTA sobre produtor_id: sz_orders.produtor_id armazena wp_user_id, enquanto
// sz_products.produtor_id é portal_users.id (LEFT JOIN no List usa pu.id = sp.produtor_id).
// Tentamos resolver via senderzz_portal_users.wp_user_id; se não houver match,
// caímos no fallback `1` (mantém o COALESCE pedido pelo spec).
func (h *ProductsHandler) runSyncFromOrders(ctx context.Context) (int64, error) {
	// DISTINCT ON precisa estar alinhado com o ORDER BY: pegamos a linha mais
	// recente (oi.id DESC) por produto_id e tomamos MAX(preco_unit) na janela.
	// NOT EXISTS garante idempotência — produtos já cadastrados não são duplicados.
	rows, err := h.Pool.Query(ctx,
		`INSERT INTO sz_products
		    (wp_post_id, produtor_id, nome, sku, preco, status, meta, created_at, updated_at)
		 SELECT DISTINCT ON (oi.produto_id)
		     NULLIF(oi.produto_id, 0)                            AS wp_post_id,
		     COALESCE(pu.id, o.produtor_id, 1)                   AS produtor_id,
		     COALESCE(NULLIF(oi.nome, ''), 'Produto')            AS nome,
		     NULLIF(oi.sku, '')                                  AS sku,
		     MAX(oi.preco_unit) OVER (PARTITION BY oi.produto_id) AS preco,
		     'active'                                            AS status,
		     jsonb_build_object('source','sync-from-orders')     AS meta,
		     NOW(),
		     NOW()
		 FROM sz_order_items oi
		 LEFT JOIN sz_orders o            ON o.id = oi.order_id
		 LEFT JOIN senderzz_portal_users pu ON pu.wp_user_id = o.produtor_id
		 WHERE oi.produto_id IS NOT NULL AND oi.produto_id > 0
		   AND NOT EXISTS (
		       SELECT 1 FROM sz_products sp
		       WHERE sp.wp_post_id = oi.produto_id
		   )
		 ORDER BY oi.produto_id, oi.id DESC
		 RETURNING id`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var count int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return count, err
		}
		count++
	}
	return count, rows.Err()
}

// SyncFromOrders — POST /products/sync-from-orders
//
// Importa produtos distintos do histórico de sz_order_items para sz_products.
// Idempotente: re-execução não duplica (NOT EXISTS por wp_post_id).
func (h *ProductsHandler) SyncFromOrders(w http.ResponseWriter, r *http.Request) {
	if !h.tableExistsProducts(r) {
		httpx.Err(w, 503, "table_not_found", "tabela sz_products não existe — execute a migration v462")
		return
	}
	if !h.tableExists(r.Context(), "sz_order_items") {
		httpx.Err(w, 503, "table_not_found", "tabela sz_order_items não existe — não há histórico para sincronizar")
		return
	}

	synced, err := h.runSyncFromOrders(r.Context())
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	httpx.JSON(w, 200, map[string]any{"ok": true, "synced": synced})
}

// Stats — GET /products/stats
//
// KPIs do topo da tela Produtos: contagem ativos, total de vínculos de afiliados
// e receita dos últimos 30 dias (soma de subtotais em sz_order_items).
func (h *ProductsHandler) Stats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	out := map[string]any{
		"active_count":     int64(0),
		"total_affiliates": int64(0),
		"revenue_30d":      float64(0),
	}

	if h.tableExists(ctx, "sz_products") {
		var n int64
		_ = h.Pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM sz_products WHERE status = 'active'`).Scan(&n)
		out["active_count"] = n
	}

	// Conta apenas vínculos que apontam para produtos sincronizados (join via wp_post_id).
	// Se sz_products ainda não existe, devolve 0 (degradação graciosa).
	if h.tableExists(ctx, "senderzz_affiliates") && h.tableExists(ctx, "sz_products") {
		var n int64
		_ = h.Pool.QueryRow(ctx,
			`SELECT COUNT(*)
			   FROM senderzz_affiliates sa
			   JOIN sz_products sp
			     ON sa.produto_id = COALESCE(sp.wp_post_id, sp.id)`).Scan(&n)
		out["total_affiliates"] = n
	}

	// Receita 30d: soma subtotal de itens cujo produto está em sz_products.
	if h.tableExists(ctx, "sz_order_items") && h.tableExists(ctx, "sz_orders") && h.tableExists(ctx, "sz_products") {
		var v float64
		_ = h.Pool.QueryRow(ctx,
			`SELECT COALESCE(SUM(oi.subtotal), 0)::float8
			   FROM sz_order_items oi
			   JOIN sz_orders o      ON o.id = oi.order_id
			   JOIN sz_products sp   ON sp.wp_post_id = oi.produto_id
			  WHERE o.created_at >= NOW() - INTERVAL '30 days'`).Scan(&v)
		out["revenue_30d"] = v
	}

	httpx.JSON(w, 200, out)
}
