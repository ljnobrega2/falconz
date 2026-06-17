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
	UserID           int64   `json:"user_id"`
	Email            string  `json:"email"`
	Nome             string  `json:"nome"`
	Telefone         string  `json:"telefone"`           // senderzz_portal_user_meta._billing_phone (WC convention)
	CPF              string  `json:"cpf"`                // senderzz_portal_user_meta._billing_cpf
	PixKey           string  `json:"pix_key"`            // u.settings->>'pix_key' ou meta _senderzz_pix_key / _pix_key
	AffiliateCode    string  `json:"affiliate_code"`
	ComissaoPct      float64 `json:"comissao_pct"`
	Status           string  `json:"status"`
	CreatedAt        string  `json:"created_at"`
	Vinculos         int64   `json:"vinculos"`
	LinksCount       int64   `json:"links_count"`        // total de links de checkout do afiliado
	TotalClicks      int64   `json:"total_clicks"`       // soma de clicks em todos os links
	TotalVendido30d  float64 `json:"total_vendido_30d"`  // SUM(sz_orders.total) últimos 30d
	TotalComissao30d float64 `json:"total_comissao_30d"` // SUM(affiliate_amount) últimos 30d
	PedidosCount30d  int64   `json:"pedidos_count_30d"`
	LastOrderAt      *string `json:"last_order_at"`      // último pedido como afiliado (data ISO)
}

func (h *AffiliatesHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	// busca textual por email ou nome do afiliado (?q=) — passado raw; SQL monta os wildcards
	search := strings.TrimSpace(q.Get("q"))

	// Lista TODOS os usuários com role='affiliate' em senderzz_portal_users,
	// com LEFT JOIN para agregar vínculos e link_token (afiliado pode existir
	// sem vínculo ativo — nesse caso status='sem_vinculo').
	//
	// Enriquecimento (auditoria 2026-06-17):
	//   - telefone / cpf / pix_key: meta sensíveis via senderzz_portal_user_meta
	//     (WC convention _billing_phone / _billing_cpf; PIX em u.settings->>'pix_key'
	//     com fallback para _senderzz_pix_key / _pix_key).
	//   - links_count / total_clicks: senderzz_affiliate_links (via vínculo a.id).
	//   - total_vendido_30d / total_comissao_30d / pedidos_count_30d / last_order_at:
	//     agregados de sz_orders WHERE affiliate_id IN (u.id, u.wp_user_id) — o
	//     wp_user_id é o atalho para pedidos legados que ainda gravam o ID WP.
	rows, err := h.Pool.Query(r.Context(),
		`SELECT
		    u.id AS user_id,
		    u.email,
		    COALESCE(u.nome, '') AS nome,
		    COALESCE((
		        SELECT meta_value FROM senderzz_portal_user_meta
		         WHERE user_id = u.id AND meta_key = '_billing_phone' LIMIT 1
		    ), '') AS telefone,
		    COALESCE((
		        SELECT meta_value FROM senderzz_portal_user_meta
		         WHERE user_id = u.id AND meta_key = '_billing_cpf' LIMIT 1
		    ), '') AS cpf,
		    COALESCE(
		        NULLIF(u.settings->>'pix_key',''),
		        (SELECT meta_value FROM senderzz_portal_user_meta
		           WHERE user_id = u.id AND meta_key IN ('_senderzz_pix_key','_pix_key')
		           ORDER BY meta_key DESC LIMIT 1),
		        ''
		    ) AS pix_key,
		    COALESCE(MAX(al.link_token), '') AS affiliate_code,
		    COALESCE(MAX(a.comissao_pct), 0)::float8 AS comissao_pct,
		    COALESCE(MAX(a.status), 'sem_vinculo') AS status,
		    COALESCE(MIN(a.created_at), u.created_at)::text AS created_at,
		    COUNT(DISTINCT a.id) AS vinculos,
		    -- Links: dois saltos (vínculo a.id → links.affiliate_id).
		    COALESCE((
		        SELECT COUNT(DISTINCT al2.id) FROM senderzz_affiliate_links al2
		          JOIN senderzz_affiliates a2 ON a2.id = al2.affiliate_id
		         WHERE a2.afiliado_id = u.id
		    ), 0) AS links_count,
		    COALESCE((
		        SELECT SUM(al2.clicks) FROM senderzz_affiliate_links al2
		          JOIN senderzz_affiliates a2 ON a2.id = al2.affiliate_id
		         WHERE a2.afiliado_id = u.id
		    ), 0) AS total_clicks,
		    -- 30d sales: sz_orders.affiliate_id pode referenciar u.id (Portal) ou
		    -- u.wp_user_id (pedidos WC legacy). COALESCE pra evitar IN com NULL.
		    COALESCE((
		        SELECT SUM(o.total)::float8 FROM sz_orders o
		         WHERE o.affiliate_id IN (u.id, COALESCE(u.wp_user_id, 0))
		           AND o.created_at >= NOW() - INTERVAL '30 days'
		    ), 0)::float8 AS total_vendido_30d,
		    COALESCE((
		        SELECT SUM(o.affiliate_amount)::float8 FROM sz_orders o
		         WHERE o.affiliate_id IN (u.id, COALESCE(u.wp_user_id, 0))
		           AND o.created_at >= NOW() - INTERVAL '30 days'
		    ), 0)::float8 AS total_comissao_30d,
		    COALESCE((
		        SELECT COUNT(*) FROM sz_orders o
		         WHERE o.affiliate_id IN (u.id, COALESCE(u.wp_user_id, 0))
		           AND o.created_at >= NOW() - INTERVAL '30 days'
		    ), 0) AS pedidos_count_30d,
		    (
		        SELECT MAX(o.created_at)::text FROM sz_orders o
		         WHERE o.affiliate_id IN (u.id, COALESCE(u.wp_user_id, 0))
		    ) AS last_order_at
		FROM senderzz_portal_users u
		LEFT JOIN senderzz_affiliates a ON a.afiliado_id = u.id
		LEFT JOIN senderzz_affiliate_links al ON al.affiliate_id = a.id AND al.active = TRUE
		WHERE u.role = 'affiliate'
		  AND ($1 = '' OR u.email ILIKE '%' || $1 || '%' OR u.nome ILIKE '%' || $1 || '%')
		GROUP BY u.id, u.email, u.nome, u.created_at, u.wp_user_id, u.settings
		ORDER BY u.created_at DESC
		LIMIT $2 OFFSET $3`, search, limit, offset)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []affiliate{}
	for rows.Next() {
		var a affiliate
		_ = rows.Scan(&a.UserID, &a.Email, &a.Nome,
			&a.Telefone, &a.CPF, &a.PixKey,
			&a.AffiliateCode, &a.ComissaoPct, &a.Status, &a.CreatedAt, &a.Vinculos,
			&a.LinksCount, &a.TotalClicks,
			&a.TotalVendido30d, &a.TotalComissao30d, &a.PedidosCount30d,
			&a.LastOrderAt)
		out = append(out, a)
	}

	// Total = todos os usuários com role='affiliate' que casam com a busca.
	var total int64
	_ = h.Pool.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM senderzz_portal_users
		 WHERE role = 'affiliate'
		   AND ($1 = '' OR email ILIKE '%' || $1 || '%' OR nome ILIKE '%' || $1 || '%')`,
		search).Scan(&total)

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
		        ''::text AS link_url,
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
	ID            int64   `json:"id"`
	OrderID       *int64  `json:"order_id"`        // sz_orders.id (pedido que gerou a comissão)
	OrderNumber   string  `json:"order_number"`    // sz_orders.order_number (ex.: SZ-0001234)
	OrderTotal    float64 `json:"order_total"`     // sz_orders.total (valor total do pedido)
	OrderStatus   string  `json:"order_status"`    // sz_orders.status
	AfiliadoNome  string  `json:"afiliado_nome"`
	AfiliadoEmail string  `json:"afiliado_email"`
	ProdutorNome  string  `json:"produtor_nome"`
	ProdutorEmail string  `json:"produtor_email"`
	ProdutoNome   *string `json:"produto_nome"`
	ProdutoID     *int64  `json:"produto_id"`      // primeiro item do pedido (sz_order_items)
	ComissaoPct   float64 `json:"comissao_pct"`    // regra aplicada
	Valor         float64 `json:"valor"`
	Tipo          string  `json:"tipo"`
	StatusTx      string  `json:"status_tx"`       // pending / available / cancelled etc.
	LinkToken     string  `json:"link_token"`      // oferta usada
	LinkURL       string  `json:"link_url"`        // app.senderzz.com.br/checkout/<token>
	AvailableAt   *string `json:"available_at"`    // data prevista de liberação (senderzz_affiliate_transactions.available_at)
	CreatedAt     string  `json:"created_at"`
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

	// Enriquecimento (auditoria 2026-06-17):
	//   - order_number / order_total / order_status: LEFT JOIN sz_orders por t.order_id.
	//   - afiliado_email / produtor_email: das junções em senderzz_portal_users.
	//   - produto (nome + id): primeiro item via subquery escalar (evita fan-out).
	//   - link_token / link_url: link mais ativo do vínculo; URL = base do checkout.
	//   - available_at: senderzz_affiliate_transactions.available_at (adicionada na v463).
	sqlList := `SELECT t.id,
		        t.order_id,
		        COALESCE(o.order_number,'') AS order_number,
		        COALESCE(o.total, 0)::float8 AS order_total,
		        COALESCE(o.status,'') AS order_status,
		        COALESCE(af.nome,'') AS afiliado_nome,
		        COALESCE(af.email,'') AS afiliado_email,
		        COALESCE(p.nome,'')  AS produtor_nome,
		        COALESCE(p.email,'') AS produtor_email,
		        COALESCE((
		          SELECT i.nome FROM sz_order_items i
		          WHERE i.order_id = t.order_id
		          ORDER BY i.id ASC LIMIT 1
		        ), NULL) AS produto_nome,
		        (
		          SELECT i.produto_id FROM sz_order_items i
		          WHERE i.order_id = t.order_id
		          ORDER BY i.id ASC LIMIT 1
		        ) AS produto_id,
		        COALESCE(a.comissao_pct, 0)::float8 AS comissao_pct,
		        COALESCE(t.amount, 0),
		        t.type,
		        COALESCE(t.status,'') AS status_tx,
		        COALESCE((
		          SELECT al.link_token FROM senderzz_affiliate_links al
		          WHERE al.affiliate_id = a.id
		          ORDER BY al.active DESC, al.id ASC LIMIT 1
		        ), '') AS link_token,
		        COALESCE((
		          SELECT CASE WHEN al.link_token <> ''
		                      THEN 'https://app.senderzz.com.br/checkout/' || al.link_token
		                      ELSE '' END
		            FROM senderzz_affiliate_links al
		           WHERE al.affiliate_id = a.id
		           ORDER BY al.active DESC, al.id ASC LIMIT 1
		        ), '') AS link_url,
		        t.available_at::text AS available_at,
		        t.created_at::text
		 FROM senderzz_affiliate_transactions t
		 JOIN senderzz_affiliates a  ON a.afiliado_id = t.affiliate_id
		 LEFT JOIN senderzz_portal_users af ON af.id = a.afiliado_id
		 LEFT JOIN senderzz_portal_users p  ON p.id  = a.produtor_id
		 LEFT JOIN sz_orders o ON o.id = t.order_id` +
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
		_ = rows.Scan(&c.ID, &c.OrderID,
			&c.OrderNumber, &c.OrderTotal, &c.OrderStatus,
			&c.AfiliadoNome, &c.AfiliadoEmail,
			&c.ProdutorNome, &c.ProdutorEmail,
			&c.ProdutoNome, &c.ProdutoID,
			&c.ComissaoPct, &c.Valor, &c.Tipo, &c.StatusTx,
			&c.LinkToken, &c.LinkURL,
			&c.AvailableAt, &c.CreatedAt)
		out = append(out, c)
	}

	var total int64
	_ = h.Pool.QueryRow(r.Context(), sqlCount, tipo, dataIni, dataFim, search).Scan(&total)

	httpx.JSON(w, 200, map[string]any{"items": out, "total": total})
}
