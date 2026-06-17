package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

type AffiliatesHandler struct{ Pool *pgxpool.Pool }

type affiliate struct {
	UserID          int64   `json:"user_id"`
	Email           string  `json:"email"`
	Nome            string  `json:"nome"`
	AffiliateCode   string  `json:"affiliate_code"`
	ComissaoPct     float64 `json:"comissao_pct"`
	Status          string  `json:"status"`
	CreatedAt       string  `json:"created_at"`
	Vinculos        int64   `json:"vinculos"`
	TotalVendido30d float64 `json:"total_vendido_30d"` // SUM(sz_orders.total) últimos 30d
	TotalComissao30d float64 `json:"total_comissao_30d"` // SUM(affiliate_amount) últimos 30d
	PedidosCount30d int64   `json:"pedidos_count_30d"`
}

func (h *AffiliatesHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	status := strings.TrimSpace(q.Get("status"))
	// busca textual por email ou nome do afiliado (?q=)
	search := strings.TrimSpace(q.Get("q"))
	if search != "" {
		search = "%" + search + "%"
	}

	// Lista AFILIADOS ÚNICOS (usuários) com agregações dos vínculos produtor↔afiliado.
	// LEFT JOIN com subquery agregada de sz_orders (últimos 30d) — total vendido + comissão.
	// Subquery escalar nas colunas (em vez de JOIN agregado) evita fan-out quando o afiliado
	// tem múltiplos vínculos e múltiplos pedidos no período.
	rows, err := h.Pool.Query(r.Context(),
		`SELECT af.id AS user_id,
		        COALESCE(af.email,'') AS email,
		        COALESCE(af.nome,'')  AS nome,
		        COALESCE(MAX(al.link_token), '')          AS affiliate_code,
		        COALESCE(MAX(a.comissao_pct), 0)::float8  AS comissao_pct,
		        COALESCE(MAX(a.status), 'pending')        AS status,
		        MIN(a.created_at)::text                   AS created_at,
		        COUNT(DISTINCT a.id)                      AS vinculos,
		        COALESCE((
		          SELECT SUM(COALESCE(o.total,0))
		          FROM sz_orders o
		          WHERE o.affiliate_id = af.id
		            AND o.created_at >= NOW() - INTERVAL '30 days'
		            AND o.status NOT IN ('frustrado','cancelled','refunded')
		        ), 0)::float8 AS total_vendido_30d,
		        COALESCE((
		          SELECT SUM(COALESCE(o.affiliate_amount,0))
		          FROM sz_orders o
		          WHERE o.affiliate_id = af.id
		            AND o.created_at >= NOW() - INTERVAL '30 days'
		            AND o.status NOT IN ('frustrado','cancelled','refunded')
		        ), 0)::float8 AS total_comissao_30d,
		        COALESCE((
		          SELECT COUNT(*)
		          FROM sz_orders o
		          WHERE o.affiliate_id = af.id
		            AND o.created_at >= NOW() - INTERVAL '30 days'
		        ), 0)::bigint AS pedidos_count_30d
		 FROM senderzz_portal_users af
		 JOIN senderzz_affiliates a ON a.afiliado_id = af.id
		 LEFT JOIN senderzz_affiliate_links al
		        ON al.affiliate_id = a.id AND al.active = TRUE
		 WHERE ($1='' OR a.status = $1)
		   AND ($2='' OR COALESCE(af.email,'') ILIKE $2
		              OR COALESCE(af.nome,'')  ILIKE $2)
		 GROUP BY af.id, af.email, af.nome
		 ORDER BY MIN(a.created_at) DESC
		 LIMIT $3 OFFSET $4`, status, search, limit, offset)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []affiliate{}
	for rows.Next() {
		var a affiliate
		_ = rows.Scan(&a.UserID, &a.Email, &a.Nome, &a.AffiliateCode,
			&a.ComissaoPct, &a.Status, &a.CreatedAt, &a.Vinculos,
			&a.TotalVendido30d, &a.TotalComissao30d, &a.PedidosCount30d)
		out = append(out, a)
	}

	// Total = contagem de afiliados únicos que casam com o filtro.
	var total int64
	_ = h.Pool.QueryRow(r.Context(),
		`SELECT COUNT(DISTINCT af.id)
		 FROM senderzz_portal_users af
		 JOIN senderzz_affiliates a ON a.afiliado_id = af.id
		 WHERE ($1='' OR a.status = $1)
		   AND ($2='' OR COALESCE(af.email,'') ILIKE $2
		              OR COALESCE(af.nome,'')  ILIKE $2)`, status, search).Scan(&total)

	httpx.JSON(w, 200, map[string]any{"items": out, "total": total})
}

type affiliateLink struct {
	ID           int64   `json:"id"`
	ProdutorNome string  `json:"produtor_nome"`
	AfiliadoNome string  `json:"afiliado_nome"`
	ProdutoNome  *string `json:"produto_nome"`
	ComissaoPct  float64 `json:"comissao_pct"`
	Status       string  `json:"status"`
	CreatedAt    string  `json:"created_at"`
	LinkToken    string  `json:"link_token"`  // oferta — token do link de checkout
	LinkURL      string  `json:"link_url"`    // url completa (quando disponível)
	LinkActive   bool    `json:"link_active"`
}

func (h *AffiliatesHandler) Links(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	status := strings.TrimSpace(q.Get("status"))
	// busca textual por nome de afiliado ou produtor (?q=)
	search := strings.TrimSpace(q.Get("q"))
	if search != "" {
		search = "%" + search + "%"
	}

	// Junta primeiro link ativo (token + url) por vínculo via subquery escalar
	// — evita fan-out quando o afiliado tem várias ofertas no mesmo vínculo.
	rows, err := h.Pool.Query(r.Context(),
		`SELECT a.id,
		        COALESCE(p.nome,'')  AS produtor_nome,
		        COALESCE(af.nome,'') AS afiliado_nome,
		        NULL::text           AS produto_nome,
		        COALESCE(a.comissao_pct, 0),
		        a.status,
		        a.created_at::text,
		        COALESCE((SELECT al.link_token FROM senderzz_affiliate_links al
		                  WHERE al.affiliate_id = a.id
		                  ORDER BY al.active DESC, al.id ASC LIMIT 1), '') AS link_token,
		        COALESCE((SELECT al.link_url FROM senderzz_affiliate_links al
		                  WHERE al.affiliate_id = a.id
		                  ORDER BY al.active DESC, al.id ASC LIMIT 1), '') AS link_url,
		        COALESCE((SELECT al.active FROM senderzz_affiliate_links al
		                  WHERE al.affiliate_id = a.id
		                  ORDER BY al.active DESC, al.id ASC LIMIT 1), FALSE) AS link_active
		 FROM senderzz_affiliates a
		 LEFT JOIN senderzz_portal_users p  ON p.id  = a.produtor_id
		 LEFT JOIN senderzz_portal_users af ON af.id = a.afiliado_id
		 WHERE ($1='' OR a.status=$1)
		   AND ($2='' OR COALESCE(p.nome,'')  ILIKE $2
		              OR COALESCE(af.nome,'') ILIKE $2)
		 ORDER BY a.id DESC LIMIT $3 OFFSET $4`, status, search, limit, offset)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []affiliateLink{}
	for rows.Next() {
		var l affiliateLink
		_ = rows.Scan(&l.ID, &l.ProdutorNome, &l.AfiliadoNome, &l.ProdutoNome,
			&l.ComissaoPct, &l.Status, &l.CreatedAt,
			&l.LinkToken, &l.LinkURL, &l.LinkActive)
		out = append(out, l)
	}

	var total int64
	_ = h.Pool.QueryRow(r.Context(),
		`SELECT COUNT(*)
		 FROM senderzz_affiliates a
		 LEFT JOIN senderzz_portal_users p  ON p.id  = a.produtor_id
		 LEFT JOIN senderzz_portal_users af ON af.id = a.afiliado_id
		 WHERE ($1='' OR a.status=$1)
		   AND ($2='' OR COALESCE(p.nome,'')  ILIKE $2
		              OR COALESCE(af.nome,'') ILIKE $2)`,
		status, search).Scan(&total)

	httpx.JSON(w, 200, map[string]any{"items": out, "total": total})
}

type affiliateCommission struct {
	ID           int64   `json:"id"`
	OrderID      *int64  `json:"order_id"`     // sz_orders.id (pedido que gerou a comissão)
	AfiliadoNome string  `json:"afiliado_nome"`
	ProdutorNome string  `json:"produtor_nome"`
	ProdutoNome  *string `json:"produto_nome"`
	ComissaoPct  float64 `json:"comissao_pct"` // regra aplicada
	Valor        float64 `json:"valor"`
	Tipo         string  `json:"tipo"`
	StatusTx     string  `json:"status_tx"`    // pending / available / cancelled etc.
	LinkToken    string  `json:"link_token"`   // oferta usada
	CreatedAt    string  `json:"created_at"`
}

func (h *AffiliatesHandler) Commissions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset, _ := strconv.Atoi(q.Get("offset"))

	// ?status= filtra t.type (commission, penalty, approval…)
	tipo := strings.TrimSpace(q.Get("status"))
	if tipo == "all" {
		tipo = ""
	}
	dataIni := strings.TrimSpace(q.Get("data_ini"))
	dataFim := strings.TrimSpace(q.Get("data_fim"))
	// busca textual por nome de afiliado ou produtor (?q=)
	search := strings.TrimSpace(q.Get("q"))
	if search != "" {
		search = "%" + search + "%"
	}

	whereClause := `
		WHERE ($1='' OR t.type=$1)
		  AND ($2='' OR t.created_at >= ($2 || ' 00:00:00')::timestamp)
		  AND ($3='' OR t.created_at <= ($3 || ' 23:59:59')::timestamp)
		  AND ($4='' OR COALESCE(af.nome,'') ILIKE $4
		             OR COALESCE(p.nome,'')  ILIKE $4)`

	// Enriquecimento: order_id, comissao_pct do vínculo, produto (primeiro item),
	// link_token (oferta usada). Subqueries escalares para evitar fan-out.
	sqlList := `SELECT t.id,
		        t.order_id,
		        COALESCE(af.nome,'') AS afiliado_nome,
		        COALESCE(p.nome,'')  AS produtor_nome,
		        COALESCE((
		          SELECT i.nome FROM sz_order_items i
		          WHERE i.order_id = t.order_id
		          ORDER BY i.id ASC LIMIT 1
		        ), NULL) AS produto_nome,
		        COALESCE(a.comissao_pct, 0)::float8 AS comissao_pct,
		        COALESCE(t.amount, 0),
		        t.type,
		        COALESCE(t.status,'') AS status_tx,
		        COALESCE((
		          SELECT al.link_token FROM senderzz_affiliate_links al
		          WHERE al.affiliate_id = a.id
		          ORDER BY al.active DESC, al.id ASC LIMIT 1
		        ), '') AS link_token,
		        t.created_at::text
		 FROM senderzz_affiliate_transactions t
		 JOIN senderzz_affiliates a  ON a.afiliado_id = t.affiliate_id
		 LEFT JOIN senderzz_portal_users af ON af.id = a.afiliado_id
		 LEFT JOIN senderzz_portal_users p  ON p.id  = a.produtor_id` +
		whereClause + `
		 ORDER BY t.id DESC LIMIT $5 OFFSET $6`

	sqlCount := `SELECT COUNT(*)
		 FROM senderzz_affiliate_transactions t
		 JOIN senderzz_affiliates a  ON a.afiliado_id = t.affiliate_id
		 LEFT JOIN senderzz_portal_users af ON af.id = a.afiliado_id
		 LEFT JOIN senderzz_portal_users p  ON p.id  = a.produtor_id` +
		whereClause

	rows, err := h.Pool.Query(r.Context(), sqlList, tipo, dataIni, dataFim, search, limit, offset)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []affiliateCommission{}
	for rows.Next() {
		var c affiliateCommission
		_ = rows.Scan(&c.ID, &c.OrderID, &c.AfiliadoNome, &c.ProdutorNome, &c.ProdutoNome,
			&c.ComissaoPct, &c.Valor, &c.Tipo, &c.StatusTx, &c.LinkToken, &c.CreatedAt)
		out = append(out, c)
	}

	var total int64
	_ = h.Pool.QueryRow(r.Context(), sqlCount, tipo, dataIni, dataFim, search).Scan(&total)

	httpx.JSON(w, 200, map[string]any{"items": out, "total": total})
}
