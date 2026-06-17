// Handler CRUD da tela "Gestão de Links de Checkout".
// Tabela: senderzz_affiliate_links.
//
// IMPORTANTE — Ambiguidade do termo "affiliate_id":
//   - senderzz_affiliate_links.affiliate_id  → FK para senderzz_affiliates.id
//     (PK do vínculo produtor↔afiliado, NÃO é wp_user_id do afiliado).
//   - sz_orders.affiliate_id                 → wp_user_id do afiliado.
//   - Endpoint /affiliates retorna user_id   → wp_user_id.
//
// Convenção desta API: o filtro de list e o body de POST usam afiliado_user_id
// (wp_user_id) — o handler resolve internamente o vínculo correspondente em
// senderzz_affiliates antes de gravar/filtrar. Para POST, exige que o vínculo
// (afiliado_id, produto_id) já exista — admin não cria vínculo aqui, só link.
//
// Endpoints:
//   GET    /checkout-links?q=&active=&produto_id=&affiliate_id=&limit=100&offset=0
//   POST   /checkout-links             body { affiliate_id, produto_id, active }
//   PUT    /checkout-links/{id}        body { active, produto_id }
//   DELETE /checkout-links/{id}
//
// PT-BR mantido em comentários e mensagens de erro (convenção do projeto).
package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

// CheckoutLinksHandler expõe CRUD para senderzz_affiliate_links.
type CheckoutLinksHandler struct{ Pool *pgxpool.Pool }

// checkoutLinkBaseURL — prefixo público do link de checkout. A URL completa
// fica em `link_url` no payload de resposta para evitar reconstrução no front.
const checkoutLinkBaseURL = "https://app.senderzz.com.br/checkout/"

// CheckoutLink é o shape retornado pela API (já enriquecido com nomes/KPIs).
type CheckoutLink struct {
	ID            int64   `json:"id"`
	AffiliateID   int64   `json:"affiliate_id"`   // senderzz_affiliates.id (vínculo)
	AfiliadoNome  string  `json:"afiliado_nome"`
	AfiliadoEmail string  `json:"afiliado_email"`
	ProdutorNome  string  `json:"produtor_nome"`
	LinkToken     string  `json:"link_token"`
	LinkURL       string  `json:"link_url"`        // computed: base + token
	ProdutoID     int64   `json:"produto_id"`
	ProdutoNome   string  `json:"produto_nome"`    // de sz_products.nome (match por wp_post_id)
	Active        bool    `json:"active"`
	Clicks        int64   `json:"clicks"`
	Conversoes    int64   `json:"conversoes"`      // COUNT sz_orders por afiliado wp_user_id
	ReceitaGerada float64 `json:"receita_gerada"`  // SUM sz_orders.total por afiliado wp_user_id
	CreatedAt     string  `json:"created_at"`
}

// tableExists — checagem genérica para qualquer tabela public.<name>.
// Mesma assinatura usada nos demais handlers para degradação graciosa.
func (h *CheckoutLinksHandler) tableExists(ctx context.Context, name string) bool {
	var exists bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = $1
		)`, name).Scan(&exists)
	return exists
}

// genLinkToken gera 32 bytes aleatórios → 64 chars hex.
// UNIQUE constraint em senderzz_affiliate_links.link_token cobre colisão
// (probabilidade nula com 256 bits de entropia — sem retry loop).
func genLinkToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ---------------------------------------------------------------------------
// GET /checkout-links
// ---------------------------------------------------------------------------
//
// Filtros aceitos via querystring:
//   q             — busca textual (token, nome do afiliado, email, nome do produto)
//   active        — "1" ou "0"; vazio = ambos
//   produto_id    — wp_post_id do produto (filtra al.produto_id)
//   affiliate_id  — wp_user_id do afiliado (resolvido para a.afiliado_id)
//   limit/offset  — paginação (default 100, máx 200)
//
// Enriquecimentos:
//   - afiliado_nome/email vêm de senderzz_portal_users (JOIN por a.afiliado_id)
//   - produtor_nome vem de senderzz_portal_users (JOIN por a.produtor_id)
//   - produto_nome vem de sz_products (LEFT JOIN por wp_post_id = al.produto_id)
//   - conversoes/receita vêm de sz_orders agregado por afiliado wp_user_id
//
// Tabela ausente → resposta vazia (degradação graciosa, como demais handlers).
func (h *CheckoutLinksHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "senderzz_affiliate_links") {
		httpx.JSON(w, 200, map[string]any{"items": []CheckoutLink{}, "total": int64(0)})
		return
	}

	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	search := strings.TrimSpace(q.Get("q"))
	activeStr := strings.TrimSpace(q.Get("active"))
	produtoID, _ := strconv.ParseInt(q.Get("produto_id"), 10, 64)
	// affiliate_id no filtro = wp_user_id (convenção desta API, ver topo do arquivo).
	afiliadoUserID, _ := strconv.ParseInt(q.Get("affiliate_id"), 10, 64)

	// Subqueries para conversões/receita: agrega sz_orders pelo wp_user_id do
	// afiliado (a.afiliado_id). COALESCE garante 0 quando sz_orders está vazio
	// ou ausente. NULLIF protege contra divisão futura se necessária.
	// hasOrders é opcional — quando ausente, retornamos 0 nos campos derivados.
	hasOrders := h.tableExists(ctx, "sz_orders")

	// Monta SELECT dinâmico: sem sz_orders, subqueries viram literais 0.
	convExpr := "0::bigint"
	revExpr := "0::float8"
	if hasOrders {
		convExpr = `(SELECT COUNT(*) FROM sz_orders o WHERE o.affiliate_id = a.afiliado_id)::bigint`
		revExpr = `(SELECT COALESCE(SUM(o.total), 0) FROM sz_orders o WHERE o.affiliate_id = a.afiliado_id)::float8`
	}

	sqlList := `
		SELECT
		    al.id,
		    al.affiliate_id,
		    COALESCE(af.nome, '')  AS afiliado_nome,
		    COALESCE(af.email, '') AS afiliado_email,
		    COALESCE(p.nome, '')   AS produtor_nome,
		    al.link_token,
		    al.produto_id,
		    COALESCE(sp.nome, '')  AS produto_nome,
		    al.active,
		    COALESCE(al.clicks, 0) AS clicks,
		    ` + convExpr + ` AS conversoes,
		    ` + revExpr + ` AS receita_gerada,
		    al.created_at::text
		FROM senderzz_affiliate_links al
		JOIN senderzz_affiliates a       ON a.id  = al.affiliate_id
		LEFT JOIN senderzz_portal_users af ON af.id = a.afiliado_id
		LEFT JOIN senderzz_portal_users p  ON p.id  = a.produtor_id
		LEFT JOIN sz_products sp          ON sp.wp_post_id = al.produto_id
		WHERE ($1 = '' OR al.link_token ILIKE '%' || $1 || '%'
		                OR COALESCE(af.nome, '')  ILIKE '%' || $1 || '%'
		                OR COALESCE(af.email, '') ILIKE '%' || $1 || '%'
		                OR COALESCE(sp.nome, '')  ILIKE '%' || $1 || '%')
		  AND ($2 = ''  OR ($2 = '1' AND al.active = TRUE) OR ($2 = '0' AND al.active = FALSE))
		  AND ($3 = 0   OR al.produto_id = $3)
		  AND ($4 = 0   OR a.afiliado_id = $4)
		ORDER BY al.id DESC
		LIMIT $5 OFFSET $6`

	rows, err := h.Pool.Query(ctx, sqlList,
		search, activeStr, produtoID, afiliadoUserID, limit, offset)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	items := []CheckoutLink{}
	for rows.Next() {
		var l CheckoutLink
		if err := rows.Scan(
			&l.ID, &l.AffiliateID, &l.AfiliadoNome, &l.AfiliadoEmail, &l.ProdutorNome,
			&l.LinkToken, &l.ProdutoID, &l.ProdutoNome, &l.Active, &l.Clicks,
			&l.Conversoes, &l.ReceitaGerada, &l.CreatedAt,
		); err != nil {
			httpx.Err(w, 500, "scan_error", err.Error())
			return
		}
		// URL completa montada no servidor — front só copia/exibe.
		l.LinkURL = checkoutLinkBaseURL + l.LinkToken
		items = append(items, l)
	}

	// Contagem total (mesmo WHERE; sem JOIN de sz_products para reduzir custo).
	// af/p JOINs mantidos pois o filtro $1 referencia af.nome/email.
	var total int64
	_ = h.Pool.QueryRow(ctx,
		`SELECT COUNT(*)
		 FROM senderzz_affiliate_links al
		 JOIN senderzz_affiliates a       ON a.id  = al.affiliate_id
		 LEFT JOIN senderzz_portal_users af ON af.id = a.afiliado_id
		 LEFT JOIN sz_products sp          ON sp.wp_post_id = al.produto_id
		 WHERE ($1 = '' OR al.link_token ILIKE '%' || $1 || '%'
		                 OR COALESCE(af.nome, '')  ILIKE '%' || $1 || '%'
		                 OR COALESCE(af.email, '') ILIKE '%' || $1 || '%'
		                 OR COALESCE(sp.nome, '')  ILIKE '%' || $1 || '%')
		   AND ($2 = ''  OR ($2 = '1' AND al.active = TRUE) OR ($2 = '0' AND al.active = FALSE))
		   AND ($3 = 0   OR al.produto_id = $3)
		   AND ($4 = 0   OR a.afiliado_id = $4)`,
		search, activeStr, produtoID, afiliadoUserID).Scan(&total)

	httpx.JSON(w, 200, map[string]any{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// ---------------------------------------------------------------------------
// POST /checkout-links
// ---------------------------------------------------------------------------
//
// Body: { affiliate_id (wp_user_id), produto_id (wp_post_id), active }
//
// Resolução do vínculo:
//   - busca senderzz_affiliates por (afiliado_id = body.affiliate_id, produto_id = body.produto_id)
//   - se não existir → 400. Admin deve criar o vínculo antes (regra do produtor).
//
// O token é gerado server-side (32 bytes hex). UNIQUE constraint cobre colisão.
type checkoutLinkCreateBody struct {
	AffiliateID int64 `json:"affiliate_id"` // wp_user_id do afiliado (NÃO o vínculo PK)
	ProdutoID   int64 `json:"produto_id"`   // wp_post_id do produto
	Active      bool  `json:"active"`
}

func (h *CheckoutLinksHandler) Create(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "senderzz_affiliate_links") || !h.tableExists(ctx, "senderzz_affiliates") {
		httpx.Err(w, 503, "table_not_found",
			"tabelas senderzz_affiliate_links/senderzz_affiliates ausentes — rode a migration de afiliados")
		return
	}

	var b checkoutLinkCreateBody
	if err := httpx.DecodeJSON(r, &b); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	if b.AffiliateID == 0 {
		httpx.Err(w, 400, "validation", "campo affiliate_id (wp_user_id) é obrigatório")
		return
	}
	if b.ProdutoID == 0 {
		httpx.Err(w, 400, "validation", "campo produto_id é obrigatório")
		return
	}

	// Resolve o vínculo: senderzz_affiliates.id correspondente.
	// Sem vínculo cadastrado, recusa o link — admin não cria vínculo aqui.
	var vinculoID int64
	err := h.Pool.QueryRow(ctx,
		`SELECT id FROM senderzz_affiliates
		 WHERE afiliado_id = $1 AND produto_id = $2
		 ORDER BY id ASC LIMIT 1`,
		b.AffiliateID, b.ProdutoID).Scan(&vinculoID)
	if err != nil {
		httpx.Err(w, 400, "vinculo_inexistente",
			"vínculo afiliado↔produto não cadastrado — cadastre o vínculo em senderzz_affiliates primeiro")
		return
	}

	token, err := genLinkToken()
	if err != nil {
		httpx.Err(w, 500, "rand_error", err.Error())
		return
	}

	var id int64
	err = h.Pool.QueryRow(ctx,
		`INSERT INTO senderzz_affiliate_links (affiliate_id, link_token, produto_id, active)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id`,
		vinculoID, token, b.ProdutoID, b.Active).Scan(&id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 201, map[string]any{
		"id":         id,
		"link_token": token,
		"link_url":   checkoutLinkBaseURL + token,
	})
}

// ---------------------------------------------------------------------------
// PUT /checkout-links/{id}
// ---------------------------------------------------------------------------
//
// Body opcional: { active, produto_id }. Campos ausentes ficam inalterados via
// COALESCE com a coluna atual.
type checkoutLinkUpdateBody struct {
	Active    *bool  `json:"active"`
	ProdutoID *int64 `json:"produto_id"`
}

func (h *CheckoutLinksHandler) Update(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "senderzz_affiliate_links") {
		httpx.Err(w, 503, "table_not_found", "tabela senderzz_affiliate_links ausente")
		return
	}
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if id == 0 {
		httpx.Err(w, 400, "validation", "id inválido")
		return
	}

	var b checkoutLinkUpdateBody
	if err := httpx.DecodeJSON(r, &b); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}

	// Update parcial: COALESCE preserva o valor existente quando o campo é NULL
	// no payload (clientes que só querem mudar active não precisam reenviar produto_id).
	_, err := h.Pool.Exec(ctx,
		`UPDATE senderzz_affiliate_links
		 SET active     = COALESCE($1, active),
		     produto_id = COALESCE($2, produto_id)
		 WHERE id = $3`,
		b.Active, b.ProdutoID, id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	httpx.JSON(w, 200, map[string]any{"ok": true, "id": id})
}

// ---------------------------------------------------------------------------
// DELETE /checkout-links/{id}
// ---------------------------------------------------------------------------
//
// Hard delete — não há referência FK saindo daqui (senderzz_affiliate_links é
// folha; sz_orders não referencia este id). Se algum dia carregar relatórios
// históricos, considerar soft-delete via active=FALSE.
func (h *CheckoutLinksHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "senderzz_affiliate_links") {
		httpx.Err(w, 503, "table_not_found", "tabela senderzz_affiliate_links ausente")
		return
	}
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if id == 0 {
		httpx.Err(w, 400, "validation", "id inválido")
		return
	}
	_, err := h.Pool.Exec(ctx,
		`DELETE FROM senderzz_affiliate_links WHERE id = $1`, id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	httpx.JSON(w, 200, map[string]any{"ok": true})
}
