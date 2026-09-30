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
//
//	GET    /checkout-links?q=&active=&produto_id=&affiliate_id=&limit=100&offset=0
//	GET    /checkout-links/offers       (ofertas migradas — somente leitura)
//	GET    /checkout-links/export.csv   (relatório p/ tirar do banco)
//	POST   /checkout-links              → 410 Gone (links de afiliado são automáticos)
//	PUT    /checkout-links/{id}         → 410 Gone
//	DELETE /checkout-links/{id}         → 410 Gone
//
// DECISÃO DO DONO: "link afiliado nao criamos e automatico nao deve ter no
// admin" + "admin nao precisa ter tela de todos os checkouts isso precisa ser
// emitido via relatorio pra tirar do banco". A escrita manual foi desativada
// (Create/Update/Delete viram 410); a leitura interna que outros handlers usam
// (List/ListOffers; Commissions) foi preservada; e export.csv substitui a tela
// de "todos os checkouts".
//
// PT-BR mantido em comentários e mensagens de erro (convenção do projeto).
package handlers

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

// CheckoutLinksHandler expõe CRUD para senderzz_affiliate_links.
type CheckoutLinksHandler struct{ Pool *pgxpool.Pool }

// checkoutLinkBaseURL — prefixo público do link de checkout. A URL completa
// fica em `link_url` no payload de resposta para evitar reconstrução no front.
const checkoutLinkBaseURL = "https://app.falklog.com.br/checkout/"

// CheckoutLink é o shape retornado pela API (já enriquecido com nomes/KPIs).
type CheckoutLink struct {
	ID            int64   `json:"id"`
	AffiliateID   int64   `json:"affiliate_id"` // senderzz_affiliates.id (vínculo)
	AfiliadoNome  string  `json:"afiliado_nome"`
	AfiliadoEmail string  `json:"afiliado_email"`
	ProdutorNome  string  `json:"produtor_nome"`
	LinkToken     string  `json:"link_token"`
	LinkURL       string  `json:"link_url"` // computed: base + token
	ProdutoID     int64   `json:"produto_id"`
	ProdutoNome   string  `json:"produto_nome"` // de sz_products.nome (match por wp_post_id)
	Active        bool    `json:"active"`
	Clicks        int64   `json:"clicks"`
	Conversoes    int64   `json:"conversoes"`     // COUNT sz_orders por afiliado wp_user_id
	ReceitaGerada float64 `json:"receita_gerada"` // SUM sz_orders.total por afiliado wp_user_id
	CreatedAt     string  `json:"created_at"`
}

// tableExists — checagem genérica para qualquer tabela public.<name>.
// Mesma assinatura usada nos demais handlers para degradação graciosa.
func (h *CheckoutLinksHandler) tableExists(ctx context.Context, name string) bool {
	return tableExistsCached(ctx, h.Pool, name) // AUDIT-2026-06-18 Onda2 (go-infoschema-cache)
}

// ---------------------------------------------------------------------------
// GET /checkout-links
// ---------------------------------------------------------------------------
//
// Filtros aceitos via querystring:
//
//	q             — busca textual (token, nome do afiliado, email, nome do produto)
//	active        — "1" ou "0"; vazio = ambos
//	produto_id    — wp_post_id do produto (filtra al.produto_id)
//	affiliate_id  — wp_user_id do afiliado (resolvido para a.afiliado_id)
//	limit/offset  — paginação (default 100, máx 200)
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
// ESCRITA MANUAL DESATIVADA — POST/PUT/DELETE → 410 Gone
// ---------------------------------------------------------------------------
//
// DECISÃO DO DONO: "link afiliado nao criamos e automatico nao deve ter no
// admin". Links de afiliado são gerados AUTOMATICAMENTE no fluxo de afiliação
// (portal) — o admin nunca os cria, edita ou exclui manualmente. As rotas
// continuam registradas (compatibilidade de roteador) mas respondem 410 Gone
// com a mensagem canônica em PT-BR. A LEITURA (List/ListOffers/ExportCSV) e o
// uso interno por outros handlers (ex.: Commissions lê senderzz_affiliate_links)
// permanecem intactos.
const checkoutLinkManualMsg = "Links de afiliado são automáticos — não são criados/editados no admin."

// Create — desativado (410). Mantido para satisfazer o wiring de rotas.
func (h *CheckoutLinksHandler) Create(w http.ResponseWriter, r *http.Request) {
	httpx.Err(w, http.StatusGone, "gone", checkoutLinkManualMsg)
}

// Update — desativado (410).
func (h *CheckoutLinksHandler) Update(w http.ResponseWriter, r *http.Request) {
	httpx.Err(w, http.StatusGone, "gone", checkoutLinkManualMsg)
}

// Delete — desativado (410).
func (h *CheckoutLinksHandler) Delete(w http.ResponseWriter, r *http.Request) {
	httpx.Err(w, http.StatusGone, "gone", checkoutLinkManualMsg)
}

// ===========================================================================
// OFERTAS DE CHECKOUT (somente leitura) — senderzz_checkout_links
// ===========================================================================
//
// IMPORTANTE — DUAS coisas distintas, NÃO confundir:
//   - senderzz_affiliate_links  → link de RASTREIO do afiliado (clicks). CRUD acima.
//                                   Hoje vazio (0 linhas migradas).
//   - senderzz_checkout_links   → link de OFERTA do PRODUTOR que define o PREÇO
//                                   de venda (display_value/price_label). 38 linhas
//                                   migradas fielmente do WP. SOMENTE LEITURA aqui.
//
// Este endpoint expõe os links de oferta migrados para a tela de admin os listar.
// Joins fiéis (verificados contra o banco migrado):
//   - producer_id → senderzz_portal_users.id          (NÃO wp_user_id) — 38/38
//   - conversões/receita → sz_orders cujo pedido tem
//       sz_order_meta._senderzz_checkout_link_id = checkout_links.id  (cl.id::text)
//   - post_id NÃO é um produto e sim o CANAL de envio (22 = correio / 1075 =
//       motoboy). Não existe FK direta da oferta para sz_products: post_id não casa
//       com sz_products.wp_post_id (esses CPTs não foram migrados como produtos).
//
// PRODUTO DA OFERTA (TAREFA N parte b — nome do produto visível no admin):
//   Como não há elo por id, resolvemos o produto por NOME (best-effort): casamos
//   cl.name (sem o sufixo " — Motoboy") contra sz_products.nome, case-insensitive,
//   via subquery ESCALAR (LIMIT 1) — NUNCA JOIN por nome, que multiplicaria linhas
//   em colisão de nome. Quando não há match (ofertas descritivas como "5 Potes
//   padrão", que descrevem variação/quantidade e não um produto cadastrado),
//   caímos no próprio cl.name como descritor. Não escopamos por produtor porque os
//   id-spaces de cl.producer_id e sp.produtor_id NÃO se alinham de forma confiável
//   no dump vivo (verificado: oferta producer_id=40 casa produto produtor_id=15),
//   então um filtro por produtor descartaria matches válidos.
//
// GET /checkout-links/offers?q=&tipo=&producer_id=&limit=100&offset=0

// CheckoutOffer — shape de um link de oferta de checkout (preço de venda).
type CheckoutOffer struct {
	ID            int64   `json:"id"`
	ProducerID    int64   `json:"producer_id"` // senderzz_portal_users.id
	ProdutorNome  string  `json:"produtor_nome"`
	PostID        int64   `json:"post_id"` // wp_post_id de origem (pode não ter produto migrado)
	Token         string  `json:"token"`
	Tipo          string  `json:"tipo"`          // correio / motoboy
	URL           string  `json:"url"`           // link público completo (migrado, fiel)
	DisplayValue  float64 `json:"display_value"` // preço de venda
	PriceLabel    string  `json:"price_label"`   // "R$ 349,00"
	Nome          string  `json:"name"`          // descritor da oferta
	ProdutoNome   string  `json:"produto_nome"`  // nome do produto resolvido (best-effort por name; ver ListOffers)
	Slug          string  `json:"slug"`
	AffiliateVis  bool    `json:"affiliate_visible"`
	Conversoes    int64   `json:"conversoes"`     // pedidos atribuídos a este link
	ReceitaGerada float64 `json:"receita_gerada"` // SUM sz_orders.total atribuída
	CreatedAt     string  `json:"created_at"`
}

// ListOffers — lista os links de oferta de checkout migrados (senderzz_checkout_links).
// Somente leitura (não há CRUD aqui — os links são gerados no fluxo de produto do portal).
func (h *CheckoutLinksHandler) ListOffers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "senderzz_checkout_links") {
		httpx.JSON(w, 200, map[string]any{"items": []CheckoutOffer{}, "total": int64(0)})
		return
	}

	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	search := strings.TrimSpace(q.Get("q"))
	tipo := strings.TrimSpace(q.Get("tipo"))
	producerID, _ := strconv.ParseInt(q.Get("producer_id"), 10, 64)

	// Conversões/receita só agregadas quando sz_orders + sz_order_meta existem.
	// cl.id::text porque _senderzz_checkout_link_id é gravado como string na meta.
	convExpr := "0::bigint"
	revExpr := "0::float8"
	if h.tableExists(ctx, "sz_orders") && h.tableExists(ctx, "sz_order_meta") {
		convExpr = `(SELECT COUNT(*) FROM sz_order_meta m
		             WHERE m.meta_key='_senderzz_checkout_link_id'
		               AND m.meta_value = cl.id::text)::bigint`
		revExpr = `(SELECT COALESCE(SUM(o.total),0) FROM sz_orders o
		            JOIN sz_order_meta m ON m.order_id = o.id
		                 AND m.meta_key='_senderzz_checkout_link_id'
		            WHERE m.meta_value = cl.id::text)::float8`
	}

	hasPortalUsers := h.tableExists(ctx, "senderzz_portal_users")
	produtorSel := "''::text AS produtor_nome"
	produtorJoin := ""
	if hasPortalUsers {
		produtorSel = "COALESCE(pu.nome,'') AS produtor_nome"
		produtorJoin = "LEFT JOIN senderzz_portal_users pu ON pu.id = cl.producer_id"
	}

	// PRODUTO (best-effort por NOME). Subquery escalar (LIMIT 1) — nunca JOIN por
	// nome (multiplicaria linhas em colisão). O regexp tira o sufixo " — Motoboy"
	// para casar o par correio/motoboy com o mesmo produto.
	//
	// IMPORTANTE: retornamos string VAZIA quando NÃO há produto casável (em vez de
	// cair no descritor da oferta). Só o backend sabe se houve match; devolver vazio
	// deixa o front distinguir "produto resolvido" de "fallback" sem reinferir por
	// comparação de string (que falha quando o nome do produto == descritor da oferta,
	// ex.: "Dorvax"). O front exibe o descritor (sem sufixo) quando vier vazio.
	produtoExpr := `''::text`
	if h.tableExists(ctx, "sz_products") {
		produtoExpr = `COALESCE(
		    (SELECT sp.nome FROM sz_products sp
		      WHERE lower(sp.nome) = lower(regexp_replace(cl.name, ' — Motoboy$', ''))
		      ORDER BY sp.id ASC LIMIT 1),
		    '')`
	}

	sqlList := `
		SELECT cl.id, cl.producer_id, ` + produtorSel + `,
		       cl.post_id, cl.token, cl.tipo, cl.url,
		       cl.display_value, cl.price_label, cl.name,
		       ` + produtoExpr + ` AS produto_nome,
		       cl.slug, cl.affiliate_visible,
		       ` + convExpr + ` AS conversoes,
		       ` + revExpr + ` AS receita_gerada,
		       cl.created_at::text
		FROM senderzz_checkout_links cl
		` + produtorJoin + `
		WHERE ($1 = '' OR cl.name ILIKE '%' || $1 || '%' OR cl.token ILIKE '%' || $1 || '%')
		  AND ($2 = '' OR cl.tipo = $2)
		  AND ($3 = 0  OR cl.producer_id = $3)
		ORDER BY cl.id DESC
		LIMIT $4 OFFSET $5`

	rows, err := h.Pool.Query(ctx, sqlList, search, tipo, producerID, limit, offset)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	items := []CheckoutOffer{}
	for rows.Next() {
		var o CheckoutOffer
		if err := rows.Scan(
			&o.ID, &o.ProducerID, &o.ProdutorNome,
			&o.PostID, &o.Token, &o.Tipo, &o.URL,
			&o.DisplayValue, &o.PriceLabel, &o.Nome, &o.ProdutoNome, &o.Slug, &o.AffiliateVis,
			&o.Conversoes, &o.ReceitaGerada, &o.CreatedAt,
		); err != nil {
			httpx.Err(w, 500, "scan_error", err.Error())
			return
		}
		items = append(items, o)
	}

	var total int64
	_ = h.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM senderzz_checkout_links cl
		 WHERE ($1 = '' OR cl.name ILIKE '%' || $1 || '%' OR cl.token ILIKE '%' || $1 || '%')
		   AND ($2 = '' OR cl.tipo = $2)
		   AND ($3 = 0  OR cl.producer_id = $3)`,
		search, tipo, producerID).Scan(&total)

	httpx.JSON(w, 200, map[string]any{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// ===========================================================================
// RELATÓRIO — GET /checkout-links/export.csv
// ===========================================================================
//
// DECISÃO DO DONO: "admin nao precisa ter tela de todos os checkouts isso
// precisa ser emitido via relatorio pra tirar do banco". Este endpoint é esse
// relatório: puxa os links de oferta direto do banco (senderzz_checkout_links —
// a tabela que carrega tipo/url/valor; senderzz_affiliate_links é de rastreio e
// está vazia) e devolve CSV. Mesmos filtros opcionais de ListOffers (q/tipo/
// producer_id) para espelhar o que a tela mostrava. Escopo admin (roda dentro
// do mesmo grupo autenticado das demais rotas). Cap de 5000 linhas.
//
// Separador ";", UTF-8 BOM (Excel reconhece acentos), header PT-BR.
func (h *CheckoutLinksHandler) ExportCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "senderzz_checkout_links") {
		httpx.Err(w, 503, "table_missing", "tabela senderzz_checkout_links não migrada")
		return
	}

	q := r.URL.Query()
	search := strings.TrimSpace(q.Get("q"))
	tipo := strings.TrimSpace(q.Get("tipo"))
	producerID, _ := strconv.ParseInt(q.Get("producer_id"), 10, 64)

	hasPortalUsers := h.tableExists(ctx, "senderzz_portal_users")
	produtorSel := "''::text AS produtor_nome"
	produtorJoin := ""
	if hasPortalUsers {
		produtorSel = "COALESCE(pu.nome,'') AS produtor_nome"
		produtorJoin = "LEFT JOIN senderzz_portal_users pu ON pu.id = cl.producer_id"
	}

	rows, err := h.Pool.Query(ctx, `
		SELECT cl.id, `+produtorSel+`, cl.token, cl.tipo, cl.url,
		       cl.display_value, cl.created_at::text
		FROM senderzz_checkout_links cl
		`+produtorJoin+`
		WHERE ($1 = '' OR cl.name ILIKE '%' || $1 || '%' OR cl.token ILIKE '%' || $1 || '%')
		  AND ($2 = '' OR cl.tipo = $2)
		  AND ($3 = 0  OR cl.producer_id = $3)
		ORDER BY cl.id DESC
		LIMIT 5000`, search, tipo, producerID)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/csv; charset=UTF-8")
	w.Header().Set("Content-Disposition", `attachment; filename="links-checkout.csv"`)

	// BOM UTF-8 para Excel reconhecer acentos.
	_, _ = w.Write([]byte("\xEF\xBB\xBF"))

	cw := csv.NewWriter(w)
	cw.Comma = ';'

	_ = cw.Write([]string{
		"ID", "Produtor", "Token", "Tipo", "URL", "Valor", "Criado em",
	})

	for rows.Next() {
		var (
			id           int64
			produtorNome string
			token        string
			tipoVal      string
			url          string
			displayValue float64
			createdAt    string
		)
		if err := rows.Scan(&id, &produtorNome, &token, &tipoVal, &url, &displayValue, &createdAt); err != nil {
			// Linha corrompida não derruba o download — segue.
			continue
		}
		_ = cw.Write([]string{
			strconv.FormatInt(id, 10),
			produtorNome,
			token,
			tipoVal,
			url,
			fmt.Sprintf("%.2f", displayValue),
			createdAt,
		})
	}
	cw.Flush()
}
