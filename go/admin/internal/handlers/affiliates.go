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
	ID          int64  `json:"id"`
	ProdutorID  int64  `json:"produtor_id"`
	ProdutorNome string `json:"produtor_nome"`
	AfiliadoID  int64  `json:"afiliado_id"`
	AfiliadoNome string `json:"afiliado_nome"`
	ProdutoID   int64  `json:"produto_id"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
}

func (h *AffiliatesHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	status := q.Get("status")

	rows, err := h.Pool.Query(r.Context(),
		`SELECT a.id, a.produtor_id,
		        COALESCE(p.nome,'') AS produtor_nome,
		        a.afiliado_id,
		        COALESCE(af.nome,'') AS afiliado_nome,
		        a.produto_id, a.status, a.created_at::text
		 FROM senderzz_affiliates a
		 LEFT JOIN senderzz_portal_users p  ON p.id = a.produtor_id
		 LEFT JOIN senderzz_portal_users af ON af.id = a.afiliado_id
		 WHERE ($1='' OR a.status=$1)
		 ORDER BY a.id DESC LIMIT $2 OFFSET $3`, status, limit, offset)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []affiliate{}
	for rows.Next() {
		var a affiliate
		_ = rows.Scan(&a.ID, &a.ProdutorID, &a.ProdutorNome,
			&a.AfiliadoID, &a.AfiliadoNome, &a.ProdutoID, &a.Status, &a.CreatedAt)
		out = append(out, a)
	}

	var total int64
	_ = h.Pool.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM senderzz_affiliates WHERE ($1='' OR status=$1)`, status).Scan(&total)

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

	rows, err := h.Pool.Query(r.Context(),
		`SELECT a.id,
		        COALESCE(p.nome,'')  AS produtor_nome,
		        COALESCE(af.nome,'') AS afiliado_nome,
		        NULL::text           AS produto_nome,
		        COALESCE(a.comissao_pct, 0),
		        a.status,
		        a.created_at::text
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
		_ = rows.Scan(&l.ID, &l.ProdutorNome, &l.AfiliadoNome, &l.ProdutoNome, &l.ComissaoPct, &l.Status, &l.CreatedAt)
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
	AfiliadoNome string  `json:"afiliado_nome"`
	ProdutorNome string  `json:"produtor_nome"`
	ProdutoNome  *string `json:"produto_nome"`
	Valor        float64 `json:"valor"`
	Tipo         string  `json:"tipo"`
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

	sqlList := `SELECT t.id,
		        COALESCE(af.nome,'') AS afiliado_nome,
		        COALESCE(p.nome,'')  AS produtor_nome,
		        NULL::text           AS produto_nome,
		        COALESCE(t.amount, 0),
		        t.type,
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
		_ = rows.Scan(&c.ID, &c.AfiliadoNome, &c.ProdutorNome, &c.ProdutoNome, &c.Valor, &c.Tipo, &c.CreatedAt)
		out = append(out, c)
	}

	var total int64
	_ = h.Pool.QueryRow(r.Context(), sqlCount, tipo, dataIni, dataFim, search).Scan(&total)

	httpx.JSON(w, 200, map[string]any{"items": out, "total": total})
}
