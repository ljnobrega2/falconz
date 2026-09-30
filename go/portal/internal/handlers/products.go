// Package handlers — handler de Produtos do Portal V2 (escopo do usuário logado).
//
// Espelha templates/portal/v2/sections/products.php — porta a tela "Produtos"
// do portal para o backend Go, mantendo UX/UI e REGRAS idênticas ao WP.
//
// Rotas (namespace /wp-json/senderzz/v1):
//
//	GET  /portal/products                        — lista produtos do usuário (escopo por role)
//	GET  /portal/products/cds                     — CDs reais (sz_motoboy_cds) p/ o seletor
//	GET  /portal/products/{id}                    — detalhe de 1 produto (KPIs + checkouts)
//	POST /portal/products/{id}/vitrine-description — salva descrição da vitrine (só produtor dono)
//
// ── ESCOPO POR ROLE (canonical id-space — nunca improvisar) ───────────────────
//   - produtor : vê os SEUS produtos        → sp.produtor_id = u.id  (portal id)
//     (sz_products.produtor_id é portal_users.id — ver admin/products.go)
//   - afiliado : vê os produtos a que está vinculado →
//     senderzz_affiliates a JOIN onde u.wp_user_id = a.afiliado_id
//     (NUNCA IN(u.id,u.wp_user_id)). produto_id casa com sp.wp_post_id.
//   - operator : sem produtos próprios → lista vazia (degradação graciosa).
//
// ── REGRAS DE NEGÓCIO portadas fielmente do WP ────────────────────────────────
//   - Produto NÃO tem preço (v469): o preço de venda vive no link de oferta
//     (senderzz_checkout_links). Aqui o produto carrega só altura/largura/
//     comprimento (cm) + peso (kg) e a descrição da vitrine.
//   - Descrição da vitrine: no WP é a postmeta _sz_vitrine_description; no Go-land
//     ela mora em sz_products.descricao (TEXT). Só o produtor DONO pode editar.
//   - Filtro de produtos "que não são produto" (recarga / frete interno / carteira
//     de frete) — espelha o filtro NOT ILIKE do admin/products.go e da vitrine.
//   - Checkouts por produto: senderzz_checkout_links (ofertas do produtor),
//     escopados por cl.producer_id = u.id (portal id — ver schema v467) e casados
//     ao produto por cl.post_id = sp.wp_post_id. Links tipo='motoboy' não viram
//     card principal (espelha o `continue` do WP). Comissão % vem de
//     senderzz_affiliates.comissao_pct do vínculo (afiliado vê o seu).
//
// ── LIMITAÇÕES DO ESPELHO PG (auditadas contra infra/postgres/*.sql) ──────────
//   - Os KPIs físicos (Disponíveis/Reservados/Em rota/Entregues/Frustrados) no WP
//     vêm de wp_sz_motoboy_stock_custody. Essa tabela NÃO existe no Postgres
//     migrado — os KPIs retornam 0 (degradação graciosa) até a tabela ser criada.
//     A UI deve renderizar os cards normalmente com 0.
//   - O espelho senderzz_checkout_links (schema v467) não carrega as colunas
//     components_text / link_motoboy_id / affiliate_commission_pct / payload da
//     origem WP — a associação link→produto é por post_id (não por texto).
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/portal-service/internal/auth"
	"github.com/senderzz/portal-service/internal/httpx"
)

// ProductsHandler agrupa as dependências dos handlers de produtos.
// Construção idêntica a WebhookHandler/IntegrationsHandler — o integrador
// monta com &handlers.ProductsHandler{Pool: pool}.
type ProductsHandler struct {
	Pool *pgxpool.Pool
}

func valueOrEmpty(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// listProductsCDsLimit é o teto do seletor de CDs (espelha o LIMIT 100 histórico).
// AUDIT PERF-list-endpoints-hard-limit: a CDs busca limit+1 para detectar
// truncamento (has_more) sem COUNT extra e devolve só o teto.
const listProductsCDsLimit = 100

// productImageExpr — SQL que resolve a imagem do produto (a MESMA do WP) best-effort
// de sz_products.meta (jsonb). sz_products NÃO tem coluna de imagem dedicada e o WP
// guarda a foto como attachment (wp_get_attachment_image_url(get_image_id())) — no
// espelho PG a URL, quando sincronizada, mora em meta sob uma destas chaves. meta
// pode ser NULL → o COALESCE devolve NULL e o Scan (string) recebe ”. Conjunto de
// chaves IDÊNTICO a go/admin product_approval.go::imagem_url e go/orders checkout.go
// ::resolveProductImage (fonte única de verdade — não divergir).
const productImageExpr = `COALESCE(
        NULLIF(sp.meta->>'image_url', ''),
        NULLIF(sp.meta->>'thumb_url', ''),
        NULLIF(sp.meta->>'thumbnail', ''),
        NULLIF(sp.meta->>'imagem', ''),
        NULLIF(sp.meta->>'image', ''),
        NULLIF(sp.meta->>'imagem_url', ''),
        NULLIF(sp.meta->>'label_url', ''),
        ''
    )`

// ── Shapes de resposta ────────────────────────────────────────────────────────

// productCheckout — uma oferta/checkout vinculada ao produto.
// Espelha as colunas da tabela "Checkouts deste produto" da section WP.
type productCheckout struct {
	ID           int64   `json:"id"`
	Name         string  `json:"name"`          // cl.name (descritor da oferta)
	PriceLabel   string  `json:"price_label"`   // "R$ 349,00"
	DisplayValue float64 `json:"display_value"` // preço de venda numérico
	Tipo         string  `json:"tipo"`          // correio | motoboy
	URL          string  `json:"url"`           // link público completo (Expedição)
	// CODUrl — link público do checkout Cash on Delivery (motoboy) PAREADO a esta
	// oferta de expedição. Pareamento por NOME: o link COD do produtor cujo name é
	// `cl.name || ' — Motoboy'` (em-dash U+2014). "" quando não há par COD. A UI
	// mostra os dois botões (Expedição + Cash on Delivery) quando ambos existem;
	// só Cash on Delivery quando o produtor não tem oferta de expedição (regra do dono).
	CODUrl           string  `json:"cod_url"`
	Slug             string  `json:"slug"` // slug/token
	AffiliateVisible bool    `json:"affiliate_visible"`
	CommissionPct    float64 `json:"affiliate_commission_pct"` // comissao_pct do vínculo (0 se não houver)
}

// productItem — um produto na lista, espelhando os campos consumidos pela section.
// KPIs físicos vêm zerados enquanto sz_motoboy_stock_custody não existir no PG.
type productItem struct {
	ID        int64   `json:"id"`         // sz_products.id
	WPPostID  *int64  `json:"wp_post_id"` // ID original WP (pode ser NULL)
	Name      string  `json:"name"`
	SKU       *string `json:"sku"`
	Descricao *string `json:"descricao"` // = descrição da vitrine (_sz_vitrine_description)
	Categoria *string `json:"categoria"`
	Status    string  `json:"status"`
	// Variacao — atributo livre do produto (sz_products.variacao, ex.: "pote" ou
	// "200mg, 400mg"). O modal "Gerar checkout" usa como SELECT de variação opcional.
	Variacao    *string  `json:"variacao"`
	Altura      *float64 `json:"altura"`      // cm
	Largura     *float64 `json:"largura"`     // cm
	Comprimento *float64 `json:"comprimento"` // cm
	Peso        *float64 `json:"peso"`        // kg
	// Custo — FEAT-CRM-FINANCEIRO (2026-07-28): só populado no branch PRODUTOR da
	// List (nunca no branch afiliado — dado financeiro privado do dono do produto).
	Custo *float64 `json:"custo,omitempty"`
	// Image — URL da imagem do produto (a MESMA do WP). Best-effort de sz_products.meta
	// (jsonb): tenta as chaves de imagem sincronizadas do WP. Vazia ("") quando o meta
	// não traz imagem (o front cai no placeholder SVG). Mesmo conjunto de chaves de
	// go/admin product_approval.go e go/orders checkout.go (fonte única de verdade).
	Image string `json:"image"`
	// KPIs físicos — espelham os 5 cards do WP. Zerados sem a tabela de custódia.
	Available      int64             `json:"available"`
	Reserved       int64             `json:"reserved"`
	Route          int64             `json:"route"`
	Delivered      int64             `json:"delivered"`
	Frustrated     int64             `json:"frustrated"`
	VitrineVisible bool              `json:"vitrine_visible"`
	Checkouts      []productCheckout `json:"checkouts"`
}

// cdItem — um Centro de Distribuição para o seletor de CD.
type cdItem struct {
	ID    int64  `json:"id"`
	Nome  string `json:"nome"`
	Ativo bool   `json:"ativo"`
}

// ── GET /portal/products ──────────────────────────────────────────────────────

// List retorna os produtos do usuário autenticado, escopados por role:
//   - produtor : sp.produtor_id = u.id  (portal id)
//   - afiliado : produtos vinculados via senderzz_affiliates (u.wp_user_id = a.afiliado_id)
//   - operator : lista vazia
//
// Filtra os "não-produtos" (recarga / frete interno / carteira de frete) e
// soft-deletados (status='deleted'), espelhando o admin/products.go.
func (h *ProductsHandler) List(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	isAff := u.Role == "afiliado" || u.Role == "cliente"

	// operator não tem produtos próprios — lista vazia (degradação graciosa).
	if u.Role == "operator" {
		httpx.WriteOK(w, map[string]any{"data": []productItem{}, "total": 0})
		return
	}

	// Filtros opcionais via querystring.
	q := r.URL.Query()
	search := strings.TrimSpace(q.Get("q"))

	var (
		rows pgx.Rows
		err  error
	)

	if isAff {
		// AFILIADO: vê os produtos dos PRODUTORES a que está vinculado e aprovado.
		// Canonical join: u.wp_user_id = a.afiliado_id (NUNCA IN(u.id,u.wp_user_id)).
		//
		// ESCOPO PRODUTOR-based (regra do dono 2026-06-24), espelhando listAffiliateLinks:
		// o vínculo costuma ser producer-wide (senderzz_affiliates.produto_id = 0), então
		// `a.produto_id = COALESCE(sp.wp_post_id, sp.id)` (versão antiga) zerava a lista
		// mesmo com vínculo ativo. Agora casa por PRODUTOR (sp.produtor_id ↔ a.produtor_id):
		// o afiliado vê TODOS os produtos dos produtores a que tem vínculo ativo.
		rows, err = h.Pool.Query(r.Context(),
			`SELECT DISTINCT
			        sp.id, sp.wp_post_id, sp.nome, sp.sku, sp.descricao,
			        sp.categoria, sp.status, sp.variacao,
			        sp.altura::float8, sp.largura::float8,
			        sp.comprimento::float8, sp.peso::float8,
			        `+productImageExpr+` AS image,
			        COALESCE(sp.vitrine_visible, true) AS vitrine_visible,
			        NULL::float8 AS custo
			   FROM sz_products sp
			  WHERE sp.status IS DISTINCT FROM 'deleted'
			    AND sp.nome NOT ILIKE '%recarga%'
			    AND sp.nome NOT ILIKE '%frete interno%'
			    AND sp.nome NOT ILIKE '%carteira de frete%'
			    AND ($2 = '' OR sp.nome ILIKE '%' || $2 || '%')
			    AND EXISTS (
			            SELECT 1
			              FROM senderzz_affiliates a
			              JOIN senderzz_portal_users p
			                ON (p.id = a.produtor_id OR p.wp_user_id = a.produtor_id)
			               AND p.role = 'produtor'
			             WHERE p.id = sp.produtor_id
			               AND a.afiliado_id = $1
			               AND a.status = 'active'
			        )
			  ORDER BY sp.nome ASC`,
			u.WPUserID, search,
		)
	} else {
		// PRODUTOR: vê os SEUS produtos. sz_products.produtor_id é portal_users.id.
		rows, err = h.Pool.Query(r.Context(),
			`SELECT sp.id, sp.wp_post_id, sp.nome, sp.sku, sp.descricao,
			        sp.categoria, sp.status, sp.variacao,
			        sp.altura::float8, sp.largura::float8,
			        sp.comprimento::float8, sp.peso::float8,
			        `+productImageExpr+` AS image,
			        COALESCE(sp.vitrine_visible, true) AS vitrine_visible,
			        sp.custo::float8 AS custo
			   FROM sz_products sp
			  WHERE sp.produtor_id = $1
			    AND sp.status IS DISTINCT FROM 'deleted'
			    AND sp.nome NOT ILIKE '%recarga%'
			    AND sp.nome NOT ILIKE '%frete interno%'
			    AND sp.nome NOT ILIKE '%carteira de frete%'
			    AND ($2 = '' OR sp.nome ILIKE '%' || $2 || '%')
			  ORDER BY sp.nome ASC`,
			u.ID, search,
		)
	}
	if err != nil {
		// Tabela ausente ou erro de query → degradação graciosa (lista vazia).
		// Mantém a tela renderizável durante a janela de migração.
		if isUndefinedTable(err) {
			httpx.WriteOK(w, map[string]any{"data": []productItem{}, "total": 0})
			return
		}
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer rows.Close()

	items := []productItem{}
	ids := []int64{}
	postIDs := []int64{}
	for rows.Next() {
		var p productItem
		if err := rows.Scan(
			&p.ID, &p.WPPostID, &p.Name, &p.SKU, &p.Descricao,
			&p.Categoria, &p.Status, &p.Variacao,
			&p.Altura, &p.Largura, &p.Comprimento, &p.Peso,
			&p.Image, &p.VitrineVisible, &p.Custo,
		); err != nil {
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao ler produtos")
			return
		}
		p.Checkouts = []productCheckout{}
		items = append(items, p)
		ids = append(ids, p.ID)
		if p.WPPostID != nil {
			postIDs = append(postIDs, *p.WPPostID)
		}
	}
	if rows.Err() != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao processar produtos")
		return
	}

	// Anexa os checkouts por produto. Erros aqui não derrubam a lista — os cards
	// de produto continuam visíveis sem ofertas (espelha empty state do WP).
	h.attachCheckouts(r, u, isAff, items, postIDs)
	// Anexa os KPIs físicos REAIS de estoque (Disponíveis/Reservados) de sz_stock.
	// SÓ para o produtor dono — estoque físico NÃO vaza para afiliado (espelha o gate
	// 403 de stock_producer_portal.go: "estoque é dado do produtor"). Afiliado segue
	// com KPIs em 0 (comportamento anterior preservado). Erros não derrubam a lista.
	if !isAff {
		h.attachStock(r, items, postIDs)
	}

	httpx.WriteOK(w, map[string]any{"data": items, "total": len(items)})
}

// ── POST /portal/products ─────────────────────────────────────────────────────  // FEAT-PORTAL-SALES
//
// Create implementa o "criar produto" do produtor — o primeiro elo do fluxo de
// venda que a UX-AUDIT (2.2 Produtor, ADD P0) apontava como notImplemented().
// É um INSERT REAL em sz_products (a tabela e todas as colunas existem no espelho
// PG, ver infra/postgres/110-products-v462.sql + 170-fixes-v469-produto-dimensoes.sql).
//
// GOVERNANÇA (porte fiel — espelha admin go/admin products.Create + section
// products.php "novo produto"):
//   - SÓ o produtor cria produtos (afiliado/operator → 403). O afiliado promove
//     ofertas de terceiros, não cadastra catálogo.
//   - produtor_id = u.ID (PORTAL id — id-space canônico de sz_products.produtor_id,
//     NUNCA wp_user_id; o List/Detail leem por sp.produtor_id = u.ID).
//   - Produto NÃO tem preço (regra v469): a coluna preco é NOT NULL DEFAULT 0.00 e
//     o banco a aplica sozinho. O preço de venda vive no link de oferta.
//   - status default 'active' (CHECK active|inactive|draft|archived; 'deleted' é
//     soft-delete, não selecionável na criação).
//   - Dimensões físicas (altura/largura/comprimento/cm, peso/kg) — opcionais (NULL
//     quando não informadas; não inventar 0).
//   - wp_post_id fica NULL (produto nativo Go — não existe post WP correspondente).
type createProductRequest struct {
	Nome        string   `json:"nome"`
	SKU         *string  `json:"sku"`
	Descricao   *string  `json:"descricao"`
	Categoria   *string  `json:"categoria"`
	Status      string   `json:"status"`
	Altura      *float64 `json:"altura"`      // cm
	Largura     *float64 `json:"largura"`     // cm
	Comprimento *float64 `json:"comprimento"` // cm
	Peso        *float64 `json:"peso"`        // kg
	// Custo — FEAT-CRM-FINANCEIRO (2026-07-28): COGS do produto. Privado do
	// produtor (+ admin) — NUNCA exposto em GetOffer/checkout público nem pra
	// afiliado. Migration 509.
	Custo  *float64 `json:"custo"`
	Imagem *string  `json:"imagem"`
}

// productStatusAllowed espelha o CHECK de sz_products.status (sem 'deleted', que é
// tombstone de soft-delete e não pode ser definido na criação/edição pela UI).
// Inclui os tokens do ciclo de aprovação ('a_aprovar','aprovado','reprovado'),
// mas o PRODUTOR não pode SE auto-aprovar — produtorMayEditStatus filtra isso.
var productStatusAllowed = map[string]bool{
	"active": true, "inactive": true, "draft": true, "archived": true,
	"a_aprovar": true, "aprovado": true, "reprovado": true,
}

// produtorMayEditStatus lista os status que o PRODUTOR pode definir manualmente
// na edição. O ciclo de aprovação ('a_aprovar'/'aprovado'/'reprovado') é
// EXCLUSIVO do admin (espelha: afiliado não se auto-aprova). Se o produtor
// mandar um status fora desta lista no Update, preservamos o status atual da
// linha (não deixa o produtor pular a fila de aprovação nem reprovar a si mesmo).
var produtorMayEditStatus = map[string]bool{
	"active": true, "inactive": true, "draft": true, "archived": true,
}

// produtorProductAutoApprove lê a flag de auto-aprovação de produto DO PRODUTOR
// (senderzz_portal_user_meta '_sz_prod_auto_approve' = '1', keyed por PORTAL id).
// Espelha producerAutoApprove() do programa de afiliados ('_sz_aff_auto_approve'):
// se ligada, o produto nasce 'aprovado' direto em vez de 'a_aprovar'. Default
// false (produto novo vai para a fila de aprovação do admin).
func (h *ProductsHandler) produtorProductAutoApprove(ctx context.Context, portalID int64) bool {
	var raw string
	err := h.Pool.QueryRow(ctx,
		`SELECT meta_value FROM senderzz_portal_user_meta
		  WHERE user_id = $1 AND meta_key = '_sz_prod_auto_approve'
		  LIMIT 1`,
		portalID,
	).Scan(&raw)
	if err != nil {
		return false
	}
	return raw == "1"
}

// Create cadastra um novo produto do produtor logado (INSERT real em sz_products).
func (h *ProductsHandler) Create(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	// Só o produtor cadastra catálogo (afiliado/operator não têm produtos próprios).
	if u.Role != "produtor" {
		httpx.WriteErr(w, http.StatusForbidden, "apenas o produtor pode cadastrar produtos")
		return
	}

	var req createProductRequest
	if err := decodeJSONBody(r, &req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}
	req.Nome = strings.TrimSpace(req.Nome)
	if req.Nome == "" {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "Informe o nome do produto.")
		return
	}

	// APROVAÇÃO DE PRODUTO (espelha vitrine.RequestAffiliation "always pending"):
	// o status NO CADASTRO é decidido pelo servidor, NÃO pelo cliente — o produtor
	// não escolhe nascer 'active'. Produto novo vai para a fila de aprovação do
	// admin com status 'a_aprovar'. Se o produtor tiver auto-aprovação ligada
	// ('_sz_prod_auto_approve'=1, espelho do '_sz_aff_auto_approve'), nasce
	// 'active' direto (AUDIT-2026-07-14: unificado com o status do fluxo de
	// aprovação manual — era 'aprovado', token duplicado do mesmo estado
	// "vendável" de um produto criado direto). Qualquer req.Status enviado
	// pela UI é ignorado aqui.
	newStatus := "a_aprovar"
	if h.produtorProductAutoApprove(r.Context(), u.ID) {
		newStatus = "active"
	}

	// INSERT escopado ao produtor (produtor_id = u.ID, PORTAL id). preco fica de
	// fora (DEFAULT 0.00 — produto não tem preço, regra v469). wp_post_id = NULL.
	var newID int64
	err := h.Pool.QueryRow(r.Context(),
		`INSERT INTO sz_products
		     (produtor_id, nome, sku, descricao, categoria, status,
		      altura, largura, comprimento, peso, custo, meta, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
		         CASE WHEN NULLIF($12, '') IS NULL THEN NULL ELSE jsonb_build_object('image_url', $12) END,
		         NOW(), NOW())
		 RETURNING id`,
		u.ID, req.Nome, req.SKU, req.Descricao, req.Categoria, newStatus,
		req.Altura, req.Largura, req.Comprimento, req.Peso, req.Custo, strings.TrimSpace(valueOrEmpty(req.Imagem)),
	).Scan(&newID)
	if err != nil {
		if isUndefinedTable(err) {
			httpx.WriteErr(w, http.StatusServiceUnavailable, "recurso indisponível no momento")
			return
		}
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao cadastrar produto")
		return
	}

	msg := "Produto cadastrado. Aguardando aprovação."
	if newStatus == "active" {
		msg = "Produto cadastrado com sucesso."
	}
	httpx.WriteOK(w, map[string]any{"id": newID, "status": newStatus, "mensagem": msg})
}

// ── PUT /portal/products/{id} ─────────────────────────────────────────────────  // FEAT-PORTAL-SALES
//
// Update edita um produto do produtor (nome, sku, descrição, categoria, status,
// dimensões). Escopado ao DONO (produtor_id = u.ID). Mesmo gate de Create.
// preco NÃO é editável (regra v469).
func (h *ProductsHandler) Update(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	if u.Role != "produtor" {
		httpx.WriteErr(w, http.StatusForbidden, "apenas o produtor pode editar produtos")
		return
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}

	var req createProductRequest
	if err := decodeJSONBody(r, &req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}
	req.Nome = strings.TrimSpace(req.Nome)
	if req.Nome == "" {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "Informe o nome do produto.")
		return
	}
	if req.Status == "" {
		req.Status = "active"
	}
	if !productStatusAllowed[req.Status] {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "Status inválido.")
		return
	}

	// APROVAÇÃO DE PRODUTO: o produtor NÃO pode mexer no ciclo de aprovação (não
	// se auto-aprova nem se reprova — isso é do admin, espelha afiliado). O CASE
	// abaixo só aplica o status enviado quando ele é um status EDITORIAL permitido
	// ao produtor (produtorMayEditStatus) E a linha NÃO está no ciclo de aprovação
	// ('a_aprovar'/'aprovado'/'reprovado'); caso contrário PRESERVA o status atual.
	// $5 = status pedido; $6 = é editorial? (bool calculado em Go).
	statusEditavel := produtorMayEditStatus[req.Status]

	res, err := h.Pool.Exec(r.Context(),
		`UPDATE sz_products
		    SET nome = $1, sku = $2, descricao = $3, categoria = $4,
		        status = CASE
		                   WHEN status IN ('a_aprovar','aprovado','reprovado') THEN status
		                   WHEN $6 THEN $5
		                   ELSE status
		                 END,
		        altura = $7, largura = $8, comprimento = $9, peso = $10, custo = $13,
		        meta = CASE WHEN $14 THEN jsonb_set(COALESCE(meta, '{}'::jsonb), '{image_url}', to_jsonb($15::text), true) ELSE meta END,
		        updated_at = NOW()
		  WHERE id = $11 AND produtor_id = $12
		    AND status IS DISTINCT FROM 'deleted'`,
		req.Nome, req.SKU, req.Descricao, req.Categoria, req.Status, statusEditavel,
		req.Altura, req.Largura, req.Comprimento, req.Peso,
		id, u.ID, req.Custo, req.Imagem != nil, strings.TrimSpace(valueOrEmpty(req.Imagem)),
	)
	if err != nil {
		if isUndefinedTable(err) {
			httpx.WriteErr(w, http.StatusServiceUnavailable, "recurso indisponível no momento")
			return
		}
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao salvar produto")
		return
	}
	if res.RowsAffected() == 0 {
		httpx.WriteErr(w, http.StatusNotFound, "produto não encontrado")
		return
	}
	httpx.WriteOK(w, map[string]any{"mensagem": "Produto atualizado com sucesso."})
}

// SaveImage troca apenas a foto do produto, sem exigir que o front replique os
// demais campos editoriais/logísticos do produto.
func (h *ProductsHandler) SaveImage(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	if u.Role != "produtor" {
		httpx.WriteErr(w, http.StatusForbidden, "apenas o produtor pode editar produtos")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}
	var req struct {
		Imagem string `json:"imagem"`
	}
	if err := decodeJSONBody(r, &req); err != nil {
		httpx.WriteErr(w, 400, "corpo da requisição inválido")
		return
	}
	imageURL := strings.TrimSpace(req.Imagem)
	if imageURL != "" && !strings.HasPrefix(strings.ToLower(imageURL), "https://") && !strings.HasPrefix(imageURL, "/uploads/products/") {
		httpx.WriteErr(w, 400, "imagem deve ser uma URL https://")
		return
	}
	res, err := h.Pool.Exec(r.Context(),
		`UPDATE sz_products SET meta = jsonb_set(COALESCE(meta, '{}'::jsonb), '{image_url}', to_jsonb($1::text), true), updated_at = NOW()
		  WHERE id = $2 AND produtor_id = $3 AND status IS DISTINCT FROM 'deleted'`, imageURL, id, u.ID)
	if err != nil {
		httpx.WriteErr(w, 500, "erro ao salvar imagem")
		return
	}
	if res.RowsAffected() == 0 {
		httpx.WriteErr(w, 404, "produto não encontrado")
		return
	}
	httpx.WriteOK(w, map[string]any{"imagem": imageURL, "mensagem": "imagem do produto atualizada"})
}

// ── DELETE /portal/products/{id} ──────────────────────────────────────────────  // FEAT-PORTAL-SALES
//
// Delete faz SOFT-DELETE (status='deleted'), espelhando o admin go/admin
// products.Delete. POR QUE soft: o auto-seed (runSyncFromOrders) re-insere por
// wp_post_id se o produto for hard-deletado; o tombstone status='deleted' faz o
// List filtrar e o auto-seed não recriar. Escopado ao DONO (produtor_id = u.ID).
func (h *ProductsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	if u.Role != "produtor" {
		httpx.WriteErr(w, http.StatusForbidden, "apenas o produtor pode excluir produtos")
		return
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}

	res, err := h.Pool.Exec(r.Context(),
		`UPDATE sz_products SET status = 'deleted', updated_at = NOW()
		  WHERE id = $1 AND produtor_id = $2 AND status IS DISTINCT FROM 'deleted'`,
		id, u.ID,
	)
	if err != nil {
		if isUndefinedTable(err) {
			httpx.WriteErr(w, http.StatusServiceUnavailable, "recurso indisponível no momento")
			return
		}
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao excluir produto")
		return
	}
	if res.RowsAffected() == 0 {
		httpx.WriteErr(w, http.StatusNotFound, "produto não encontrado")
		return
	}
	httpx.WriteOK(w, map[string]any{"mensagem": "Produto excluído."})
}

// ── GET /portal/products/{id} ─────────────────────────────────────────────────

// Detail retorna um único produto (com checkouts), garantindo escopo do usuário.
// Produtor → só o seu produto; afiliado → só produto a que está vinculado.
func (h *ProductsHandler) Detail(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}

	isAff := u.Role == "afiliado" || u.Role == "cliente"

	var p productItem
	var scanErr error
	if isAff {
		// AFILIADO: guard PRODUTOR-based (regra do dono 2026-06-24), espelhando
		// listAffiliateLinks. Vínculo costuma ser producer-wide (produto_id=0), então
		// casa por PRODUTOR (sp.produtor_id ↔ a.produtor_id), não por produto.
		scanErr = h.Pool.QueryRow(r.Context(),
			`SELECT sp.id, sp.wp_post_id, sp.nome, sp.sku, sp.descricao,
			        sp.categoria, sp.status, sp.variacao,
			        sp.altura::float8, sp.largura::float8,
			        sp.comprimento::float8, sp.peso::float8,
			        `+productImageExpr+` AS image,
			        COALESCE(sp.vitrine_visible, true)
			   FROM sz_products sp
			  WHERE sp.id = $1
			    AND sp.status IS DISTINCT FROM 'deleted'
			    AND EXISTS (
			            SELECT 1
			              FROM senderzz_affiliates a
			              JOIN senderzz_portal_users p
			                ON (p.id = a.produtor_id OR p.wp_user_id = a.produtor_id)
			               AND p.role = 'produtor'
			             WHERE p.id = sp.produtor_id
			               AND a.afiliado_id = $2
			               AND a.status = 'active'
			        )
			  LIMIT 1`,
			id, u.WPUserID,
		).Scan(&p.ID, &p.WPPostID, &p.Name, &p.SKU, &p.Descricao,
			&p.Categoria, &p.Status, &p.Variacao, &p.Altura, &p.Largura, &p.Comprimento, &p.Peso, &p.Image, &p.VitrineVisible)
	} else {
		scanErr = h.Pool.QueryRow(r.Context(),
			`SELECT sp.id, sp.wp_post_id, sp.nome, sp.sku, sp.descricao,
			        sp.categoria, sp.status, sp.variacao,
			        sp.altura::float8, sp.largura::float8,
			        sp.comprimento::float8, sp.peso::float8,
			        `+productImageExpr+` AS image,
			        COALESCE(sp.vitrine_visible, true)
			   FROM sz_products sp
			  WHERE sp.id = $1
			    AND sp.produtor_id = $2
			    AND sp.status IS DISTINCT FROM 'deleted'
			  LIMIT 1`,
			id, u.ID,
		).Scan(&p.ID, &p.WPPostID, &p.Name, &p.SKU, &p.Descricao,
			&p.Categoria, &p.Status, &p.Variacao, &p.Altura, &p.Largura, &p.Comprimento, &p.Peso, &p.Image, &p.VitrineVisible)
	}

	if scanErr == pgx.ErrNoRows {
		httpx.WriteErr(w, http.StatusNotFound, "produto não encontrado")
		return
	}
	if scanErr != nil {
		if isUndefinedTable(scanErr) {
			httpx.WriteErr(w, http.StatusNotFound, "produto não encontrado")
			return
		}
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	p.Checkouts = []productCheckout{}
	postIDs := []int64{}
	if p.WPPostID != nil {
		postIDs = append(postIDs, *p.WPPostID)
	}
	items := []productItem{p}
	h.attachCheckouts(r, u, isAff, items, postIDs)
	// Estoque físico só para o produtor dono (não vaza para afiliado) — ver List.
	if !isAff {
		h.attachStock(r, items, postIDs)
	}

	httpx.WriteOK(w, map[string]any{"data": items[0]})
}

// ── GET /portal/products/{id}/variations ──────────────────────────────────────── // FEAT-PORTAL

// productVariation — uma variação de um produto (atributos + SKU).
// O espelho PG não tem tabela de variações; este shape existe para o contrato JSON.
type productVariation struct {
	ID         int64             `json:"id"`
	SKU        string            `json:"sku"`
	Attributes map[string]string `json:"attributes"`
	Price      float64           `json:"price"`
}

// Variations retorna as variações de um produto do usuário (escopo por role).
//
// LIMITAÇÃO DO ESPELHO PG (auditada): NÃO existe tabela de variações no Postgres
// migrado e o migrador (migrate-wp-to-pg.py) NÃO popula nenhuma chave 'variations'
// em sz_products.meta. Portanto retornamos variations:[] (degradação graciosa —
// produto simples sem variações), e NÃO 501: a tabela base (sz_products) EXISTE e o
// produto é validável; o que falta é apenas o dado de variação, que o WP grava em
// product_variation posts (não migrados). Quando o migrador passar a popular
// sp.meta->'variations', este handler lê de lá sem mudar contrato.
//
// Confirma o escopo do produto (produtor dono | afiliado vinculado) antes de
// responder — id fora do escopo → 404 (não vaza existência).
func (h *ProductsHandler) Variations(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}

	isAff := u.Role == "afiliado" || u.Role == "cliente"

	// Confirma posse/visibilidade do produto e carrega meta (fonte futura das variações).
	var metaRaw []byte
	var scanErr error
	if isAff {
		// AFILIADO: guard PRODUTOR-based (regra do dono 2026-06-24) — vínculo costuma
		// ser producer-wide (produto_id=0); casa por produtor, não por produto.
		scanErr = h.Pool.QueryRow(r.Context(),
			`SELECT sp.meta
			   FROM sz_products sp
			  WHERE sp.id = $1
			    AND sp.status IS DISTINCT FROM 'deleted'
			    AND EXISTS (
			            SELECT 1
			              FROM senderzz_affiliates a
			              JOIN senderzz_portal_users p
			                ON (p.id = a.produtor_id OR p.wp_user_id = a.produtor_id)
			               AND p.role = 'produtor'
			             WHERE p.id = sp.produtor_id
			               AND a.afiliado_id = $2
			               AND a.status = 'active'
			        )
			  LIMIT 1`,
			id, u.WPUserID,
		).Scan(&metaRaw)
	} else {
		scanErr = h.Pool.QueryRow(r.Context(),
			`SELECT sp.meta
			   FROM sz_products sp
			  WHERE sp.id = $1
			    AND sp.produtor_id = $2
			    AND sp.status IS DISTINCT FROM 'deleted'
			  LIMIT 1`,
			id, u.ID,
		).Scan(&metaRaw)
	}

	if scanErr == pgx.ErrNoRows {
		httpx.WriteErr(w, http.StatusNotFound, "produto não encontrado")
		return
	}
	if scanErr != nil {
		if isUndefinedTable(scanErr) {
			httpx.WriteErr(w, http.StatusNotFound, "produto não encontrado")
			return
		}
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Tenta extrair sp.meta->'variations' (fonte futura). Ausente/inválido → [].
	variations := []productVariation{}
	if len(metaRaw) > 0 {
		var meta struct {
			Variations []productVariation `json:"variations"`
		}
		if err := json.Unmarshal(metaRaw, &meta); err == nil && meta.Variations != nil {
			variations = meta.Variations
		}
	}

	httpx.WriteOK(w, map[string]any{"variations": variations, "total": len(variations)})
}

// ── GET /portal/products/{id}/checkouts ───────────────────────────────────────
//
// Checkouts lista os checkouts/ofertas vinculados a UM produto, escopados ao dono.
// Endpoint dedicado para a aba "Checkouts deste produto" da tela de produto.
//
// POR QUE um endpoint separado (não só o attach do List): a associação link↔produto
// por cl.post_id = sp.wp_post_id NÃO casa nos dados migrados. Os checkouts migrados do
// WP carregam cl.post_id = ID do produto NO WooCommerce (ex.: 22 p/ correio, 1075 p/
// motoboy) — que é o CANAL, não o sz_products.wp_post_id (ex.: 1278). go/admin
// checkout_links.go documenta o mesmo: "post_id → sz_products.wp_post_id NÃO casa
// neste dump (CPTs 22/1075 não migrados como produtos)". Logo o attach por post_id
// devolve [] e a aba mostrava "Nenhum checkout vinculado" mesmo havendo ofertas.
//
// ESCOPO (não há como reconstruir o split por-produto dos dados migrados — a única
// pista é o cl.name; não é coluna confiável):
//   - produtor : TODAS as suas ofertas correio/expedição (cl.producer_id = u.ID),
//     EXCETO tipo='motoboy' (espelha o `continue` do WP — motoboy não vira card
//     principal). É o conjunto correto p/ o produtor dono ver suas ofertas.
//   - afiliado : só ofertas afiliáveis (affiliate_visible) do produto a que ele tem
//     vínculo ativo; comissão % vem do vínculo (senderzz_affiliates.comissao_pct).
//
// Future-proof: quando ofertas REAIS por-produto existirem (cl.post_id =
// sp.wp_post_id), a oferta deste produto continua aparecendo; só os links de canal
// legados (post_id não atribuível a NENHUM produto do produtor) também aparecem —
// é o comportamento honesto enquanto o split não é reconstruível.
//
// Confirma posse/visibilidade do produto antes de responder (id fora do escopo → 404).
func (h *ProductsHandler) Checkouts(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}

	isAff := u.Role == "afiliado" || u.Role == "cliente"

	// Guard de posse/visibilidade do produto (mesma lógica do Detail). Carrega
	// wp_post_id p/ casar ofertas REAIS por-produto (quando existirem) e o PRODUTOR
	// do produto (sp.produtor_id, portal id) p/ escopar os links COD ao produtor certo.
	var postID *int64
	var productProducerID int64
	var scanErr error
	if isAff {
		// AFILIADO: guard PRODUTOR-based (regra do dono 2026-06-24), espelhando
		// affiliates_portal.listAffiliateLinks. O vínculo costuma ser producer-wide
		// (senderzz_affiliates.produto_id = 0), então `a.produto_id = COALESCE(wp_post_id,id)`
		// (versão antiga) dava 404 mesmo com vínculo ativo. Aqui exige só vínculo ativo
		// entre o afiliado e o PRODUTOR do produto (sp.produtor_id ↔ a.produtor_id).
		scanErr = h.Pool.QueryRow(r.Context(),
			`SELECT sp.wp_post_id, sp.produtor_id
			   FROM sz_products sp
			  WHERE sp.id = $1
			    AND sp.status IS DISTINCT FROM 'deleted'
			    AND EXISTS (
			            SELECT 1
			              FROM senderzz_affiliates a
			              JOIN senderzz_portal_users p
			                ON (p.id = a.produtor_id OR p.wp_user_id = a.produtor_id)
			               AND p.role = 'produtor'
			             WHERE p.id = sp.produtor_id
			               AND a.afiliado_id = $2
			               AND a.status = 'active'
			        )
			  LIMIT 1`,
			id, u.WPUserID,
		).Scan(&postID, &productProducerID)
	} else {
		scanErr = h.Pool.QueryRow(r.Context(),
			`SELECT sp.wp_post_id, sp.produtor_id
			   FROM sz_products sp
			  WHERE sp.id = $1
			    AND sp.produtor_id = $2
			    AND sp.status IS DISTINCT FROM 'deleted'
			  LIMIT 1`,
			id, u.ID,
		).Scan(&postID, &productProducerID)
	}
	if scanErr == pgx.ErrNoRows {
		httpx.WriteErr(w, http.StatusNotFound, "produto não encontrado")
		return
	}
	if scanErr != nil {
		if isUndefinedTable(scanErr) {
			httpx.WriteErr(w, http.StatusNotFound, "produto não encontrado")
			return
		}
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	out := []productCheckout{}

	// expedicaoAtiva: lê settings->>'expedicao_ativa' do PRODUTOR DO PRODUTO
	// (productProducerID, já resolvido acima — cobre afiliado promovendo produto de
	// terceiro, não só produtor dono). AUDIT-2026-07-11: opt-IN — default INATIVA;
	// só ativa com flag EXPLICITAMENTE 'true'. Antes: afiliado (isAff) nunca rodava
	// essa query e ficava hardcoded true, vazando o botão Expedição mesmo com o
	// produtor desativado.
	expedicaoAtiva := false
	{
		var rawExp *string
		_ = h.Pool.QueryRow(r.Context(),
			`SELECT settings->>'expedicao_ativa' FROM senderzz_portal_users WHERE id = $1`,
			productProducerID,
		).Scan(&rawExp)
		if rawExp != nil && *rawExp == "true" {
			expedicaoAtiva = true
		}
	}

	// Salt de referência do afiliado (espelha get_option('sz_aff_ref_salt')). Lido só
	// para o ramo AFILIADO: sem ele a url de venda não carrega o ?r= e a venda não é
	// atribuída. Sem salt → token vazio (degradação graciosa, idêntica a
	// links_portal.affiliateRefSalt / affiliates_portal.listAffiliateLinks).
	refSalt := ""
	if isAff {
		_ = h.Pool.QueryRow(r.Context(),
			`SELECT value FROM senderzz_options WHERE name = $1`,
			"sz_aff_ref_salt",
		).Scan(&refSalt)
	}

	var rows pgx.Rows
	if isAff {
		// AFILIADO (regra do dono 2026-06-24): o link que o afiliado divulga é CASH ON
		// DELIVERY (tipo='motoboy', affiliate_visible) — o afiliado VENDE COD, não correio.
		//
		// JOIN POR PRODUTOR, não por produto: o vínculo do afiliado costuma ser
		// producer-wide (senderzz_affiliates.produto_id = 0), então `a.produto_id = cl.post_id`
		// (versão antiga) NUNCA casava e a aba vinha vazia. Espelhamos affiliates_portal.
		// listAffiliateLinks: EXISTS de vínculo ativo entre o afiliado e o PRODUTOR do link
		// (cl.producer_id ↔ a.produtor_id). Mostra os links COD do produtor a que ele tem
		// vínculo ativo — consistente com o card da Vitrine e com o ramo produtor (que já
		// expõe os links de canal legados via post_id não-atribuível).
		//
		// URL RASTREÁVEL: trazemos vinculo_id e, no scan, anexamos ?r=<token> via
		// appendRefToken. O COD vai em CODUrl (url="") → a UI renderiza só o botão
		// "Cash on Delivery" (mesma forma do produtor SEM expedição). NÃO mexe no ramo
		// do produtor (cuja url segue PURA, sem ?r=).
		// Escopo ao PRODUTOR deste produto ($2 = sp.produtor_id) — sem isso a aba
		// vazaria os links COD de OUTROS produtores a que o afiliado tem vínculo.
		rows, err = h.Pool.Query(r.Context(),
			`SELECT cl.id, cl.name, cl.price_label, cl.display_value,
			        cl.tipo, cl.url, cl.slug, cl.affiliate_visible,
			        COALESCE((
			            SELECT a.comissao_pct
			              FROM senderzz_affiliates a
			              JOIN senderzz_portal_users p
			                ON (p.id = a.produtor_id OR p.wp_user_id = a.produtor_id)
			               AND p.role = 'produtor'
			             WHERE p.id = cl.producer_id
			               AND a.afiliado_id = $1
			               AND a.status = 'active'
			             ORDER BY a.comissao_pct DESC
			             LIMIT 1
			        ), 0)::float8,
			        (
			            SELECT a.id
			              FROM senderzz_affiliates a
			              JOIN senderzz_portal_users p
			                ON (p.id = a.produtor_id OR p.wp_user_id = a.produtor_id)
			               AND p.role = 'produtor'
			             WHERE p.id = cl.producer_id
			               AND a.afiliado_id = $1
			               AND a.status = 'active'
			             ORDER BY a.id ASC
			             LIMIT 1
			        ) AS vinculo_id
			   FROM senderzz_checkout_links cl
			  WHERE cl.producer_id = $2
			    AND cl.affiliate_visible = TRUE
			    AND cl.tipo = 'motoboy'
			    AND EXISTS (
			            SELECT 1
			              FROM senderzz_affiliates a
			              JOIN senderzz_portal_users p
			                ON (p.id = a.produtor_id OR p.wp_user_id = a.produtor_id)
			               AND p.role = 'produtor'
			             WHERE p.id = cl.producer_id
			               AND a.afiliado_id = $1
			               AND a.status = 'active'
			        )
			  ORDER BY cl.id DESC
			  LIMIT 300`,
			u.WPUserID, productProducerID,
		)
	} else {
		// PRODUTOR: cada oferta é retornada como um link individual. Quando a
		// expedição está ativa, isso entrega as 3 opções: Expedição, COD e Link único.
		//
		// Future-proof: prioriza ofertas REAIS deste produto (cl.post_id = COALESCE(wp_post_id, id)),
		// mas inclui também os links de CANAL legados (post_id não atribuível a NENHUM
		// produto do produtor) — sem isso a aba ficaria vazia com os dados migrados.
		//
		// BUGFIX (produto NATIVO): a chave que o Create grava em cl.post_id é
		// COALESCE(sp.wp_post_id, sp.id) (links_portal.go). Para produtos nativos (Go,
		// wp_post_id NULL) isso vale sp.id. A versão antiga casava só por sp.wp_post_id
		// ($2 = postID = NULL) → a 1ª condição caía e a 2ª também excluía (cl.post_id =
		// sp.id ESTÁ no conjunto de produtos do produtor) → o link de EXPEDIÇÃO (correio)
		// sumia e a tela mostrava só Cash on Delivery, mesmo com expedição ATIVA. Casamos
		// agora pela chave EFETIVA = COALESCE(wp_post_id, id), idêntica à de Create.
		effectivePostID := id
		if postID != nil && *postID > 0 {
			effectivePostID = *postID
		}
		rows, err = h.Pool.Query(r.Context(),
			`SELECT cl.id, cl.name, cl.price_label, cl.display_value,
			        cl.tipo, cl.url, cl.slug, cl.affiliate_visible,
			        cl.affiliate_commission_pct::float8
			   FROM senderzz_checkout_links cl
			  WHERE cl.producer_id = $1
			    AND (cl.tipo = 'motoboy' OR ($3 AND cl.tipo <> 'motoboy'))
			    AND (
			          cl.post_id = $2
			       OR cl.post_id NOT IN (
			             SELECT COALESCE(wp_post_id, id)
			               FROM sz_products
			              WHERE produtor_id = $1
			                AND status IS DISTINCT FROM 'deleted'
			          )
			       -- Bundle: também mostra na página de QUALQUER produto que faça parte
			       -- da composição do checkout, não só no post_id "principal" (linha 0).
			       OR EXISTS (
			             SELECT 1
			               FROM jsonb_array_elements(COALESCE(cl.composition_items, '[]'::jsonb)) item
			              WHERE (item->>'product_id')::bigint = $2
			          )
			        )
			  ORDER BY cl.id DESC
			  LIMIT 300`,
			u.ID, effectivePostID, expedicaoAtiva,
		)
	}
	if err != nil {
		if isUndefinedTable(err) {
			httpx.WriteOK(w, map[string]any{"data": out, "total": 0})
			return
		}
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer rows.Close()

	for rows.Next() {
		var c productCheckout
		if isAff {
			// Afiliado: o link é CASH ON DELIVERY (cl.tipo='motoboy'). Lemos a url do COD
			// em rawURL e a movemos para CODUrl (url="") → a UI renderiza só o botão
			// "Cash on Delivery" (mesma forma do produtor SEM expedição).
			var vinculoID *int64
			var rawURL string
			if err := rows.Scan(
				&c.ID, &c.Name, &c.PriceLabel, &c.DisplayValue,
				&c.Tipo, &rawURL, &c.Slug, &c.AffiliateVisible, &c.CommissionPct,
				&vinculoID,
			); err != nil {
				httpx.WriteErr(w, http.StatusInternalServerError, "erro ao ler checkouts")
				return
			}
			// URL RASTREÁVEL: anexa ?r=<token> (token = id do vínculo) — o afiliado abre
			// o checkout COM a referência dele e a comissão é atribuída. Sem salt/vínculo
			// a url fica pura (degradação graciosa). appendRefToken vem de links_portal.go.
			if vinculoID != nil && *vinculoID > 0 && rawURL != "" {
				rawURL = appendRefToken(rawURL, *vinculoID, refSalt)
			}
			c.CODUrl = rawURL // afiliado divulga o COD → botão "Cash on Delivery"
			c.URL = ""        // sem botão "Expedição" para o afiliado
		} else {
			// Produtor: cada tipo é uma linha independente. O COD fica no campo
			// cod_url para a UI preservar o botão/semântica existente.
			var rawURL string
			if err := rows.Scan(
				&c.ID, &c.Name, &c.PriceLabel, &c.DisplayValue,
				&c.Tipo, &rawURL, &c.Slug, &c.AffiliateVisible, &c.CommissionPct,
			); err != nil {
				httpx.WriteErr(w, http.StatusInternalServerError, "erro ao ler checkouts")
				return
			}
			if c.Tipo == "motoboy" {
				c.CODUrl = rawURL
			} else {
				c.URL = rawURL
			}
		}
		out = append(out, c)
	}
	if rows.Err() != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao processar checkouts")
		return
	}

	// FALLBACK "só Cash on Delivery" (regra do dono): produtor SEM nenhuma oferta de
	// expedição (a query acima voltou vazia) ainda precisa ver os links COD. Lista os
	// próprios links motoboy do produtor como linhas (cod_url = link motoboy, url = "")
	// — a UI mostra só o botão "Cash on Delivery". Não roda para afiliado.
	if !isAff && len(out) == 0 {
		mbRows, mbErr := h.Pool.Query(r.Context(),
			`SELECT cl.id, cl.name, cl.price_label, cl.display_value,
			        cl.tipo, cl.url, cl.slug, cl.affiliate_visible,
			        cl.affiliate_commission_pct::float8
			   FROM senderzz_checkout_links cl
			  WHERE cl.producer_id = $1
			    AND cl.tipo = 'motoboy'
			  ORDER BY cl.id DESC
			  LIMIT 300`,
			u.ID,
		)
		if mbErr == nil {
			defer mbRows.Close()
			for mbRows.Next() {
				var c productCheckout
				var mbURL string
				if err := mbRows.Scan(
					&c.ID, &c.Name, &c.PriceLabel, &c.DisplayValue,
					&c.Tipo, &mbURL, &c.Slug, &c.AffiliateVisible, &c.CommissionPct,
				); err != nil {
					break
				}
				// Link motoboy vira o cod_url; sem expedição → url fica "".
				c.CODUrl = mbURL
				c.URL = ""
				out = append(out, c)
			}
		}
	}

	httpx.WriteOK(w, map[string]any{"data": out, "total": len(out)})
}

// ── GET /portal/products/cds ──────────────────────────────────────────────────

// CDs retorna os Centros de Distribuição reais (sz_motoboy_cds) para o seletor
// de CD que aparece antes da grade de produtos. Espelha a seção Localidades.
func (h *ProductsHandler) CDs(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	// N+1: busca 1 a mais que o teto p/ detectar truncamento sem COUNT. // PERF-list-endpoints-hard-limit
	rows, err := h.Pool.Query(r.Context(),
		`SELECT id, nome, ativo
		   FROM sz_motoboy_cds
		  ORDER BY nome ASC
		  LIMIT $1`,
		listProductsCDsLimit+1,
	)
	if err != nil {
		// Sem tabela de CDs → seletor vazio (o front cai no "Todos os CDs").
		if isUndefinedTable(err) {
			httpx.WriteOK(w, map[string]any{"data": []cdItem{}, "total": 0, "has_more": false, "limit": listProductsCDsLimit})
			return
		}
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer rows.Close()

	out := []cdItem{}
	for rows.Next() {
		var c cdItem
		if err := rows.Scan(&c.ID, &c.Nome, &c.Ativo); err != nil {
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao ler CDs")
			return
		}
		out = append(out, c)
	}
	if rows.Err() != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao processar CDs")
		return
	}

	// has_more=true quando veio a linha extra → o front sabe que a lista de CDs foi
	// truncada (antes a truncagem em 100 era silenciosa). Devolve só o teto. // PERF-list-endpoints-hard-limit
	hasMore := len(out) > listProductsCDsLimit
	if hasMore {
		out = out[:listProductsCDsLimit]
	}

	httpx.WriteOK(w, map[string]any{"data": out, "total": len(out), "has_more": hasMore, "limit": listProductsCDsLimit})
}

// ── POST /portal/products/{id}/vitrine-description ────────────────────────────

// saveVitrineDescriptionRequest é o body do salvamento da descrição da vitrine.
type saveVitrineDescriptionRequest struct {
	Description string `json:"description"`
}

// SaveVitrineDescription grava a descrição da vitrine do produto.
// Espelha save_vitrine_description (Portal_Page.php): SÓ o produtor DONO edita.
// No Go-land a descrição da vitrine vive em sz_products.descricao (no WP era a
// postmeta _sz_vitrine_description).
func (h *ProductsHandler) SaveVitrineDescription(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	// Apenas produtor edita descrição — afiliado/operator só leem.
	if u.Role != "produtor" {
		httpx.WriteErr(w, http.StatusForbidden, "apenas o produtor dono pode editar a descrição")
		return
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}

	var req saveVitrineDescriptionRequest
	if err := decodeJSONBody(r, &req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}

	// UPDATE escopado ao dono — produtor só altera o próprio produto.
	res, err := h.Pool.Exec(r.Context(),
		`UPDATE sz_products
		    SET descricao = $1, updated_at = NOW()
		  WHERE id = $2 AND produtor_id = $3
		    AND status IS DISTINCT FROM 'deleted'`,
		req.Description, id, u.ID,
	)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	if res.RowsAffected() == 0 {
		httpx.WriteErr(w, http.StatusNotFound, "produto não encontrado")
		return
	}

	httpx.WriteOK(w, map[string]any{"mensagem": "descrição salva com sucesso"})
}

// ── POST /portal/products/{id}/vitrine-toggle ─────────────────────────────────

// VitrineToggle alterna vitrine_visible do produto (true↔false). Só produtor dono.
func (h *ProductsHandler) VitrineToggle(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	if u.Role != "produtor" {
		httpx.WriteErr(w, http.StatusForbidden, "apenas o produtor dono pode alterar visibilidade")
		return
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}

	var newVisible bool
	err = h.Pool.QueryRow(r.Context(),
		`UPDATE sz_products
		    SET vitrine_visible = NOT COALESCE(vitrine_visible, true),
		        updated_at = NOW()
		  WHERE id = $1 AND produtor_id = $2
		    AND status IS DISTINCT FROM 'deleted'
		  RETURNING COALESCE(vitrine_visible, true)`,
		id, u.ID,
	).Scan(&newVisible)
	if err == pgx.ErrNoRows {
		httpx.WriteErr(w, http.StatusNotFound, "produto não encontrado")
		return
	}
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	httpx.WriteOK(w, map[string]any{"vitrine_visible": newVisible})
}

// ── Helpers internos ──────────────────────────────────────────────────────────

// attachCheckouts anexa as ofertas de checkout (senderzz_checkout_links) aos
// produtos por cl.post_id = sp.wp_post_id.
//
// Escopo:
//   - produtor : cl.producer_id = u.id (portal id — schema v467).
//   - afiliado : ofertas afiliáveis (affiliate_visible) dos produtos vinculados;
//     a comissão % vem de senderzz_affiliates.comissao_pct do vínculo do afiliado.
//
// Erros são silenciosos: sem ofertas, os cards de produto mostram empty state.
func (h *ProductsHandler) attachCheckouts(r *http.Request, u *auth.PortalUser, isAff bool, items []productItem, postIDs []int64) {
	if len(items) == 0 || len(postIDs) == 0 {
		return
	}

	// Mapa wp_post_id → índice em items (para distribuir os checkouts).
	idxByPost := map[int64]int{}
	for i := range items {
		if items[i].WPPostID != nil {
			idxByPost[*items[i].WPPostID] = i
		}
	}

	var (
		rows pgx.Rows
		err  error
	)

	if isAff {
		// AFILIADO: só ofertas visíveis a afiliados (cl.affiliate_visible) cujos
		// produtos ele tem vínculo ativo. A comissão % é a do vínculo (comissao_pct).
		// Junta cl.post_id → a.produto_id (WC product id) e o vínculo do afiliado.
		// tipo='motoboy' fica de fora (não vira card principal — espelha o WP).
		rows, err = h.Pool.Query(r.Context(),
			`SELECT m.matched_post_id, cl.id, cl.name, cl.price_label, cl.display_value,
			        cl.tipo, cl.url, cl.slug, cl.affiliate_visible,
			        COALESCE(a.comissao_pct, 0)::float8
			   FROM senderzz_checkout_links cl
			  CROSS JOIN LATERAL (
			        SELECT cl.post_id AS matched_post_id WHERE cl.post_id = ANY($2)
			        UNION
			        SELECT (item->>'product_id')::bigint
			          FROM jsonb_array_elements(COALESCE(cl.composition_items, '[]'::jsonb)) item
			         WHERE (item->>'product_id')::bigint = ANY($2)
			  ) m
			   JOIN senderzz_affiliates a
			     ON a.produto_id = m.matched_post_id
			    AND a.afiliado_id = $1
			    AND a.status = 'active'
			  WHERE cl.affiliate_visible = TRUE
			    AND cl.tipo <> 'motoboy'
			  ORDER BY cl.id DESC
			  LIMIT 300`,
			u.WPUserID, postIDs,
		)
	} else {
		// PRODUTOR: todas as suas ofertas (cl.producer_id = u.id). A comissão %
		// não é por afiliado aqui — é o teto definido pelo produtor; sem coluna no
		// espelho PG (affiliate_commission_pct não migrada), retornamos 0.
		// tipo='motoboy' fica de fora do card principal (espelha o WP).
		rows, err = h.Pool.Query(r.Context(),
			`SELECT m.matched_post_id, cl.id, cl.name, cl.price_label, cl.display_value,
			        cl.tipo, cl.url, cl.slug, cl.affiliate_visible,
			        0::float8
			   FROM senderzz_checkout_links cl
			  CROSS JOIN LATERAL (
			        SELECT cl.post_id AS matched_post_id WHERE cl.post_id = ANY($2)
			        UNION
			        SELECT (item->>'product_id')::bigint
			          FROM jsonb_array_elements(COALESCE(cl.composition_items, '[]'::jsonb)) item
			         WHERE (item->>'product_id')::bigint = ANY($2)
			  ) m
			  WHERE cl.producer_id = $1
			    AND cl.tipo <> 'motoboy'
			  ORDER BY cl.id DESC
			  LIMIT 300`,
			u.ID, postIDs,
		)
	}
	if err != nil {
		// Tabela ausente ou erro: produtos seguem sem ofertas (empty state no front).
		return
	}
	defer rows.Close()

	for rows.Next() {
		var postID int64
		var c productCheckout
		if err := rows.Scan(
			&postID, &c.ID, &c.Name, &c.PriceLabel, &c.DisplayValue,
			&c.Tipo, &c.URL, &c.Slug, &c.AffiliateVisible, &c.CommissionPct,
		); err != nil {
			return
		}
		if i, ok := idxByPost[postID]; ok {
			items[i].Checkouts = append(items[i].Checkouts, c)
		}
	}
}

// attachStock anexa os KPIs físicos REAIS de estoque (Disponíveis/Reservados) aos
// produtos, somando sz_stock por produto (todos os CDs/variações).
//
// ── ID-SPACE (verificado contra stock_producer_portal.go — NÃO improvisar) ──────
//
//	sz_stock.product_id = sz_products.wp_post_id (o WC product id = WP post id que o
//	trigger trg_stock_pedido_* grava). NUNCA sz_products.id interno. Por isso o
//	mapeamento é por wp_post_id (os mesmos postIDs já coletados no List/Detail).
//	Para Datalaprox (wp_post_id=1278) → Disponíveis = SUM(qty_available) = 300.
//
//	Disponíveis = estoque vendável = SUM(qty_available) - SUM(qty_reserved).
//	Reservados = SUM(qty_reserved). Em rota/Entregues/Frustrados NÃO vivem em
//	sz_stock (ficam 0 —
//	dependem da tabela de custódia que não existe no PG migrado).
//
// Erros são silenciosos: sem sz_stock (42P01) ou linhas, os KPIs ficam em 0
// (degradação graciosa — espelha o comportamento anterior da tela).
func (h *ProductsHandler) attachStock(r *http.Request, items []productItem, _ []int64) {
	if len(items) == 0 {
		return
	}

	// Chave de estoque = COALESCE(wp_post_id, id) — MESMA regra do admin
	// (stock.go stockProductNameExpr / admin-ui Products.tsx stockKey): produto
	// sincronizado do WP usa wp_post_id, produto nativo (sem wp_post_id, ex.:
	// variações criadas direto no admin) usa o próprio id. A versão antiga só
	// casava por wp_post_id — produtos nativos nunca recebiam KPI de estoque
	// (ficavam sempre 0 mesmo com sz_stock preenchido).
	idxByKey := map[int64]int{}
	keys := make([]int64, 0, len(items))
	for i := range items {
		key := items[i].ID
		if items[i].WPPostID != nil {
			key = *items[i].WPPostID
		}
		idxByKey[key] = i
		keys = append(keys, key)
	}

	rows, err := h.Pool.Query(r.Context(),
		`SELECT product_id,
		        COALESCE(SUM(qty_available), 0)::bigint,
		        COALESCE(SUM(qty_reserved),  0)::bigint
		   FROM sz_stock
		  WHERE product_id = ANY($1)
		  GROUP BY product_id`,
		keys,
	)
	if err != nil {
		// sz_stock ausente ou erro: produtos seguem com KPIs em 0 (degradação graciosa).
		return
	}
	defer rows.Close()

	for rows.Next() {
		var productID, onHand, reserved int64
		if err := rows.Scan(&productID, &onHand, &reserved); err != nil {
			return
		}
		if i, ok := idxByKey[productID]; ok {
			items[i].Available = stockAvailableForSale(onHand, reserved)
			items[i].Reserved = reserved
		}
	}
}

// stockAvailableForSale devolve o saldo ainda livre para novos pedidos.
// qty_available é o estoque físico em mãos e inclui unidades já reservadas;
// por isso o KPI "Disponíveis" precisa descontar qty_reserved.
func stockAvailableForSale(onHand, reserved int64) int64 {
	available := onHand - reserved
	if available < 0 {
		return 0
	}
	return available
}

// isUndefinedTable retorna true se o erro do Postgres for "undefined_table"
// (SQLSTATE 42P01). Usado para degradação graciosa quando uma tabela do espelho
// (sz_products, senderzz_checkout_links, sz_motoboy_cds) ainda não foi criada.
func isUndefinedTable(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "42P01"
	}
	return false
}

// decodeJSONBody decodifica o corpo JSON da requisição em dst.
// Wrapper fino sobre json.Decoder — mesmo padrão dos demais handlers do portal.
func decodeJSONBody(r *http.Request, dst any) error {
	return json.NewDecoder(r.Body).Decode(dst)
}
