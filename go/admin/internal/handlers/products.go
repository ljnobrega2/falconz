// Handler CRUD de produtos (sz_products).
//
// GET    /products?q=&produtor_id=&status=&limit=100&offset=0
// POST   /products
// PUT    /products/{id}
// DELETE /products/{id}
package handlers

import (
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
	var exists bool
	_ = h.Pool.QueryRow(r.Context(),
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = 'sz_products'
		)`).Scan(&exists)
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
		    (SELECT COUNT(*) FROM senderzz_affiliates sa WHERE sa.produto_id = sp.id) AS afiliados_count
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
