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
	"github.com/senderzz/admin-service/internal/auth"
	"github.com/senderzz/admin-service/internal/httpx"
)

// ProductsHandler expõe CRUD para sz_products.
type ProductsHandler struct{ Pool *pgxpool.Pool }

// productImageExpr — resolve a imagem do produto best-effort de sz_products.meta
// (jsonb; sem coluna de imagem dedicada). Conjunto de chaves IDÊNTICO a
// go/portal products.go::productImageExpr e go/orders checkout.go::resolveProductImage
// (fonte única de verdade — não divergir). Alias `sp`. meta NULL → COALESCE devolve ”.
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

// productListableFilter é a cláusula WHERE (sem bind params) que define o que é
// LISTÁVEL na tela Produtos: exclui soft-deletados e os eventos financeiros que
// não são produto (recarga / frete interno / carteira de frete). É a ÚNICA fonte
// dessas exclusões — o SELECT principal do List, o COUNT do `total` e o
// `active_count` do Stats compartilham este fragmento para nunca divergirem. A
// divergência (SELECT excluía, mas os dois COUNT não) era a causa do bug
// "X de Y" inconsistente / produtos-sistema inflando os KPIs. Usa o alias `sp`.
const productListableFilter = `
	  AND sp.status IS DISTINCT FROM 'deleted'
	  AND sp.nome NOT ILIKE '%recarga%'
	  AND sp.nome NOT ILIKE '%frete interno%'
	  AND sp.nome NOT ILIKE '%carteira de frete%'`

// Product é o shape retornado pela API.
//
// Produto NÃO tem preço (regra do dono v469): o preço de venda vive no link de
// oferta (senderzz_checkout_links), não no produto. O que o produto carrega são
// SOMENTE as dimensões físicas (altura/largura/comprimento em cm, peso em kg).
// As quatro dimensões são *float64 porque a origem WP pode não ter a meta — nesse
// caso a coluna fica NULL (não inventamos 0).
type Product struct {
	ID           int64   `json:"id"`
	WPPostID     *int64  `json:"wp_post_id"`
	ProdutorID   int64   `json:"produtor_id"`
	ProdutorNome string  `json:"produtor_nome"`
	Nome         string  `json:"nome"`
	SKU          *string `json:"sku"`
	Barcode      *string `json:"barcode"`
	Descricao    *string `json:"descricao"`
	Categoria    *string `json:"categoria"`
	Status       string  `json:"status"`
	// Variacao — atributo livre do produto (ex.: 'pote', 'cápsula'). Regra do dono
	// (migration 461): cadastrada no menu do produto; a lista de pedidos lê daqui.
	// Coluna NOT NULL DEFAULT '' → string (nunca *string, p/ não gravar NULL).
	Variacao       string   `json:"variacao"`
	Altura         *float64 `json:"altura"`      // cm
	Largura        *float64 `json:"largura"`     // cm
	Comprimento    *float64 `json:"comprimento"` // cm
	Peso           *float64 `json:"peso"`        // kg
	CreatedAt      string   `json:"created_at"`
	AfiliadosCount int64    `json:"afiliados_count"`
	// ImageURL — URL da imagem do produto (OPCIONAL). Best-effort de sz_products.meta
	// (productImageExpr). "" quando não há imagem cadastrada. Gravada pelo Create/Update
	// na chave meta->>'image_url' (a mesma que o checkout/portal leem). Round-trip: o
	// form de edição mostra a miniatura atual a partir deste campo.
	ImageURL string `json:"image_url"`
	// VitrineVisible — toggle "Visível na vitrine" (migração 479). true = o produto
	// aparece na Vitrine do portal (descoberta de afiliados); false = oculto. A tela
	// Produtos NÃO filtra por este campo (o admin precisa ver os ocultos p/ religá-los);
	// quem filtra é a vitrine (go/portal vitrine.go). COALESCE(...,true) no SELECT mantém
	// produtos legados visíveis. Round-trip: o form mostra o estado atual do toggle.
	VitrineVisible bool `json:"vitrine_visible"`
	// ParentProductID/ParentProductNome — FEAT-VARIACAO (migration 505): quando
	// preenchido, este produto é uma VARIAÇÃO de outro (mesmo padrão de linha
	// própria em sz_products, só com o vínculo de exibição). nil = produto solto.
	ParentProductID   *int64  `json:"parent_product_id"`
	ParentProductNome *string `json:"parent_product_nome"`
	// Custo — FEAT-CRM-FINANCEIRO (2026-07-28): COGS, privado do produtor+admin.
	// Migration 509. List zera pra afiliado (ver loop do List — ActorAfiliado).
	Custo *float64 `json:"custo,omitempty"`
}

// tableExistsProducts retorna true se sz_products já existe no banco.
func (h *ProductsHandler) tableExistsProducts(r *http.Request) bool {
	return h.tableExists(r.Context(), "sz_products")
}

// tableExists — checagem genérica para qualquer tabela public.<name>.
func (h *ProductsHandler) tableExists(ctx context.Context, name string) bool {
	return tableExistsCached(ctx, h.Pool, name) // AUDIT-2026-06-18 Onda2 (go-infoschema-cache)
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
	// MED30: o front (Products.tsx:168-169) envia data_ini/data_fim (range de
	// criação, YYYY-MM-DD inclusivo) — espelha orders.go:192-200. String vazia =
	// sem filtro (guard `$N = '' OR …`).
	dataIni := strings.TrimSpace(q.Get("data_ini"))
	dataFim := strings.TrimSpace(q.Get("data_fim"))

	// FEAT-RBAC-ORDERS-2026-06-24 — ESCOPO POR PAPEL (DualAuth).
	// O filtro /products é chamado pela tela de pedidos compartilhada. Escopo pela
	// identidade AUTENTICADA (auth.ActorFromCtx), NUNCA pelo ?produtor_id= do cliente:
	//   - admin/nil → sem escopo (vê tudo — inalterado).
	//   - produtor  → FORÇA produtor_id = <portalUserID> (ignora o ?produtor_id= do front).
	//   - afiliado  → só produtos dos PRODUTORES a que o afiliado está vinculado
	//                 (senderzz_affiliates, join canônico id/wp_user_id). Sem vínculo → vazio.
	// affScope é um fragmento WHERE sem bind-params (subquery literal por wp_user_id),
	// compartilhado entre o SELECT e o COUNT para não divergirem.
	affScope := ""
	if actor := auth.ActorFromCtx(r.Context()); actor != nil {
		switch actor.Kind {
		case auth.ActorAdmin:
			// vê tudo.
		case auth.ActorProdutor:
			produtorID = actor.PortalUserID // sobrepõe input do cliente.
		case auth.ActorAfiliado:
			affScope = `
		  AND EXISTS (
		      SELECT 1 FROM senderzz_affiliates sa
		       WHERE sa.afiliado_id = ` + strconv.FormatInt(actor.WPUserID, 10) + `
		         AND (sa.produtor_id = sp.produtor_id
		              OR sa.produtor_id = (SELECT pu2.wp_user_id FROM senderzz_portal_users pu2 WHERE pu2.id = sp.produtor_id)))`
		default:
			// role de portal sem escopo → vazio (fail-closed).
			httpx.JSON(w, 200, map[string]any{"items": []Product{}, "total": int64(0), "limit": limit, "offset": offset})
			return
		}
	}

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
		    sp.barcode,
		    sp.descricao,
		    sp.categoria,
		    sp.status,
		    -- variação: atributo do produto (migration 461; NOT NULL DEFAULT '' → string)
		    COALESCE(sp.variacao, '') AS variacao,
		    -- dimensões físicas (NULLable → *float8 no scan; não usamos COALESCE p/ não inventar 0)
		    sp.altura::float8,
		    sp.largura::float8,
		    sp.comprimento::float8,
		    sp.peso::float8,
		    sp.created_at::text,
		    -- Afiliados do produto = afiliados vinculados ao PRODUTOR do produto.
		    -- O vínculo em senderzz_affiliates é por produtor (produtor_id), NÃO por
		    -- produto: na base real produto_id=0 em todas as linhas — afiliar-se é ao
		    -- catálogo do produtor, não a um SKU. A versão antiga juntava por
		    -- produto_id = COALESCE(wp_post_id, id) e dava sempre 0 (bug: "afiliados não
		    -- aparecem ao Datalaprox"). Usa o join canônico do produtor (id OR
		    -- wp_user_id) — MESMA contagem que producers.go:110-114 (cross-screen
		    -- consistency). Produtos do mesmo produtor compartilham o número.
		    COALESCE((
		        SELECT COUNT(DISTINCT sa.afiliado_id) FROM senderzz_affiliates sa
		         WHERE sa.produtor_id = pu.id OR sa.produtor_id = pu.wp_user_id
		    ), 0) AS afiliados_count,
		    -- imagem (OPCIONAL) best-effort de sz_products.meta — round-trip do form.
		    `+productImageExpr+` AS image_url,
		    -- toggle "Visível na vitrine" (migração 479) — round-trip do form. NÃO é
		    -- filtro: o admin lista TODOS (inclusive ocultos) p/ poder religá-los; quem
		    -- filtra é a vitrine. COALESCE(...,true) tolera produtos legados.
		    COALESCE(sp.vitrine_visible, true) AS vitrine_visible,
		    sp.parent_product_id,
		    parent.nome AS parent_product_nome,
		    sp.custo::float8 AS custo
		FROM sz_products sp
		LEFT JOIN senderzz_portal_users pu ON pu.id = sp.produtor_id
		LEFT JOIN sz_products parent ON parent.id = sp.parent_product_id
		WHERE ($1 = '' OR sp.nome ILIKE '%' || $1 || '%')
		  AND ($2 = '' OR sp.status = $2)
		  AND ($3 = 0  OR sp.produtor_id = $3)
		  -- MED30: intervalo de criação inclusivo (espelha orders.go:192-200).
		  AND ($6 = '' OR sp.created_at >= ($6::date)::timestamp AT TIME ZONE 'America/Sao_Paulo')
		  AND ($7 = '' OR sp.created_at <= (($7::date + 1)::timestamp AT TIME ZONE 'America/Sao_Paulo' - interval '1 second'))`+
			// não mostra soft-deletados (ver Delete) nem eventos financeiros que não
			// são produto: recarga / frete interno / carteira de frete. Fonte única
			// compartilhada com o COUNT do `total` abaixo e com Stats.active_count.
			productListableFilter+
			// FEAT-RBAC-ORDERS-2026-06-24 — escopo afiliado (EXISTS vínculo). Vazio p/ admin/produtor.
			affScope+`
		ORDER BY sp.id DESC
		LIMIT $4 OFFSET $5`,
		search, status, produtorID, limit, offset, dataIni, dataFim)
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
			&p.Nome, &p.SKU, &p.Barcode, &p.Descricao, &p.Categoria, &p.Status,
			&p.Variacao,
			&p.Altura, &p.Largura, &p.Comprimento, &p.Peso,
			&p.CreatedAt, &p.AfiliadosCount, &p.ImageURL, &p.VitrineVisible,
			&p.ParentProductID, &p.ParentProductNome, &p.Custo,
		); err != nil {
			httpx.Err(w, 500, "scan_error", err.Error())
			return
		}
		// FEAT-CRM-FINANCEIRO: custo é privado do produtor+admin. Afiliado usa esta
		// MESMA rota (affScope acima) — nunca deve ver o COGS de terceiro.
		if actor := auth.ActorFromCtx(r.Context()); actor != nil && actor.Kind == auth.ActorAfiliado {
			p.Custo = nil
		}
		items = append(items, p)
	}

	var total int64
	_ = h.Pool.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM sz_products sp
		 WHERE ($1 = '' OR sp.nome ILIKE '%' || $1 || '%')
		   AND ($2 = '' OR sp.status = $2)
		   AND ($3 = 0  OR sp.produtor_id = $3)
		   -- MED30: mesmo filtro de período do SELECT principal, senão total != items.
		   AND ($4 = '' OR sp.created_at >= ($4::date)::timestamp AT TIME ZONE 'America/Sao_Paulo')
		   AND ($5 = '' OR sp.created_at <= (($5::date + 1)::timestamp AT TIME ZONE 'America/Sao_Paulo' - interval '1 second'))`+
			// MESMO fragmento de exclusão do SELECT principal: sem ele o `total`
			// contava soft-deletados e produtos-sistema (recarga/carteira), gerando
			// o "X de Y" inconsistente (ex.: "2 de 4" com só 2 linhas listadas).
			productListableFilter+
			// FEAT-RBAC-ORDERS-2026-06-24 — MESMO escopo afiliado do SELECT (total↔itens coerente).
			affScope,
		search, status, produtorID, dataIni, dataFim).Scan(&total)

	httpx.JSON(w, 200, map[string]any{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// productBody é o payload de criação/edição.
//
// Sem `preco`: o produto não tem preço (regra v469). Quem define o preço de venda
// é o link de oferta. Aqui o produtor informa apenas as dimensões físicas (cm/kg),
// todas opcionais (*float64 → NULL quando não informadas).
type productBody struct {
	WPPostID    *int64   `json:"wp_post_id"`
	ProdutorID  int64    `json:"produtor_id"`
	Nome        string   `json:"nome"`
	SKU         *string  `json:"sku"`
	Barcode     *string  `json:"barcode"`
	Descricao   *string  `json:"descricao"`
	Categoria   *string  `json:"categoria"`
	Status      string   `json:"status"`
	Variacao    string   `json:"variacao"`    // atributo livre (ex.: 'pote') — NOT NULL DEFAULT ''
	Altura      *float64 `json:"altura"`      // cm
	Largura     *float64 `json:"largura"`     // cm
	Comprimento *float64 `json:"comprimento"` // cm
	Peso        *float64 `json:"peso"`        // kg
	// ImageURL — URL da imagem do produto (OPCIONAL; "" = sem imagem). Persistida em
	// sz_products.meta sob a chave 'image_url' (mesma chave que go/orders checkout.go
	// ::resolveProductImage e go/portal lêem → a miniatura aparece no checkout/portal).
	ImageURL string `json:"image_url"`
	// VitrineVisible — toggle "Visível na vitrine" (migração 479). *bool (não bool) p/
	// distinguir AUSENTE (nil) de false: omitido no body → default true (o fluxo de
	// criação existente mantém o produto visível, a menos que o admin desligue). O front
	// sempre envia o campo; o pointer é só a rede de segurança p/ clientes de API antigos.
	VitrineVisible *bool `json:"vitrine_visible"`
	// ParentProductID — FEAT-VARIACAO: produto base do qual esta é variação
	// (opcional). nil/0 = produto solto (comportamento de sempre).
	ParentProductID *int64 `json:"parent_product_id"`
	// Custo — FEAT-CRM-FINANCEIRO (2026-07-28): COGS, privado do produtor+admin.
	Custo *float64 `json:"custo"`
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

	// SKU único no catálogo (case-insensitive), exceto soft-deletados (migration 506).
	// Checagem explícita ANTES do INSERT p/ mensagem amigável — o índice único no
	// banco é só a rede de segurança contra corrida/API direta.
	if b.SKU != nil && strings.TrimSpace(*b.SKU) != "" {
		var dup bool
		_ = h.Pool.QueryRow(r.Context(),
			`SELECT EXISTS(SELECT 1 FROM sz_products WHERE lower(sku) = lower($1) AND status IS DISTINCT FROM 'deleted')`,
			strings.TrimSpace(*b.SKU)).Scan(&dup)
		if dup {
			httpx.Err(w, 409, "validation", "já existe um produto com este SKU")
			return
		}
	}

	// `preco` fica de fora: a coluna é NOT NULL DEFAULT 0.00, então o banco aplica
	// 0.00 sozinho (produto não tem preço — regra v469). Gravamos só as dimensões.
	// meta: grava a imagem (OPCIONAL) na chave 'image_url' quando informada; vazio →
	// '{}' (sem imagem). É a MESMA chave que o checkout/portal lêem (productImageExpr).
	// vitrineVisible: ausente no body (nil) → true (mantém o produto visível na vitrine;
	// o fluxo de criação existente não esconde nada sem o admin pedir).
	vitrineVisible := true
	if b.VitrineVisible != nil {
		vitrineVisible = *b.VitrineVisible
	}
	var id int64
	err := h.Pool.QueryRow(r.Context(),
		`INSERT INTO sz_products
		    (wp_post_id, produtor_id, nome, sku, barcode, descricao, categoria, status, altura, largura, comprimento, peso, variacao, meta, vitrine_visible, parent_product_id, custo)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,
		         CASE WHEN $14::text <> '' THEN jsonb_build_object('image_url', $14::text) ELSE '{}'::jsonb END,
		         $15, $16, $17)
		 RETURNING id`,
		b.WPPostID, b.ProdutorID, b.Nome, b.SKU, b.Barcode, b.Descricao, b.Categoria, b.Status,
		b.Altura, b.Largura, b.Comprimento, b.Peso, b.Variacao, strings.TrimSpace(b.ImageURL), vitrineVisible,
		b.ParentProductID, b.Custo,
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

	// SKU único no catálogo, excluindo o próprio produto (migration 506).
	if b.SKU != nil && strings.TrimSpace(*b.SKU) != "" {
		var dup bool
		_ = h.Pool.QueryRow(r.Context(),
			`SELECT EXISTS(SELECT 1 FROM sz_products WHERE lower(sku) = lower($1) AND status IS DISTINCT FROM 'deleted' AND id <> $2)`,
			strings.TrimSpace(*b.SKU), id).Scan(&dup)
		if dup {
			httpx.Err(w, 409, "validation", "já existe um produto com este SKU")
			return
		}
	}

	// `preco` não é mais editável pela UI (regra v469) — deixamos a coluna intacta.
	// meta: merge da imagem na chave 'image_url' (|| preserva outras chaves, ex.:
	// 'source' do sync-from-orders). image_url='' limpa a imagem (resolveProductImage
	// usa NULLIF → trata '' como ausente). Mesma chave lida pelo checkout/portal.
	// vitrineVisible: ausente no body (nil) → true (o front sempre envia o campo; o
	// default só protege clientes de API antigos de esconderem o produto sem querer).
	vitrineVisible := true
	if b.VitrineVisible != nil {
		vitrineVisible = *b.VitrineVisible
	}
	_, err := h.Pool.Exec(r.Context(),
		`UPDATE sz_products
		 SET wp_post_id      = COALESCE($1, wp_post_id),
		     produtor_id     = $2,
		     nome            = $3,
		     sku             = $4,
		     barcode         = $5,
		     descricao       = $6,
		     categoria       = $7,
		     status          = $8,
		     altura          = $9,
		     largura         = $10,
		     comprimento     = $11,
		     peso            = $12,
		     variacao        = $13,
		     meta            = COALESCE(meta, '{}'::jsonb) || jsonb_build_object('image_url', $14::text),
		     vitrine_visible = $15,
		     custo           = $17,
		     updated_at      = NOW()
		 WHERE id = $16`,
		b.WPPostID, b.ProdutorID, b.Nome, b.SKU, b.Barcode, b.Descricao, b.Categoria, b.Status,
		b.Altura, b.Largura, b.Comprimento, b.Peso, b.Variacao, strings.TrimSpace(b.ImageURL), vitrineVisible, id,
		b.Custo,
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
	// SOFT-DELETE: hard DELETE faz o produto "sumir e reaparecer" — runSyncFromOrders
	// re-insere a partir de sz_order_items (NOT EXISTS por wp_post_id). Marcando
	// status='deleted' o tombstone permanece, o List filtra (status<>'deleted') e o
	// auto-seed não recria (a linha ainda EXISTE por wp_post_id).
	_, err := h.Pool.Exec(r.Context(),
		`UPDATE sz_products SET status='deleted', updated_at=NOW() WHERE id=$1`, id)
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
// caímos no fallback `1` (sentinela seguro).
//
// MED31: NÃO usar o.produtor_id como fallback intermediário — é um wp_user_id e a
// coluna espera portal_users.id. Gravá-lo aqui cria produto órfão (LEFT JOIN no
// List não casa) ou, pior, atribui o produto ao portal_user cujo .id colide com
// esse wp_user_id (cross-attribution — a classe de bug que product_approval.go:19-21
// alerta). Só pu.id (resolvido) ou o sentinela 1.
func (h *ProductsHandler) runSyncFromOrders(ctx context.Context) (int64, error) {
	// DISTINCT ON precisa estar alinhado com o ORDER BY: pegamos a linha mais
	// recente (oi.id DESC) por produto_id e tomamos MAX(preco_unit) na janela.
	// NOT EXISTS garante idempotência — produtos já cadastrados não são duplicados.
	rows, err := h.Pool.Query(ctx,
		`INSERT INTO sz_products
		    (wp_post_id, produtor_id, nome, sku, preco, status, meta, created_at, updated_at)
		 SELECT DISTINCT ON (oi.produto_id)
		     NULLIF(oi.produto_id, 0)                            AS wp_post_id,
		     COALESCE(pu.id, 1)                                  AS produtor_id, -- MED31: sem o.produtor_id (wp_user_id ≠ portal id)
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
		// MED32: produto aprovado pelo admin vira status='aprovado' (product_approval.go),
		// não 'active' — contar só 'active' subestima o KPI (Image#35). Conta os dois
		// status "vivos"; LOWER() tolera legado (espelha affiliate_rules.go:309).
		//
		// Alias `sp` + productListableFilter: o KPI "Produtos ativos" tem de refletir
		// EXATAMENTE o que é listável. Sem o fragmento, "Recarga de Carteira de Frete"
		// (status='active', produto-sistema) inflava o KPI para 3 enquanto a tabela
		// mostrava 2 — a divergência relatada pelo dono. Fonte única com o List.
		_ = h.Pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM sz_products sp
			  WHERE LOWER(COALESCE(sp.status,'')) IN ('active','aprovado')`+
				productListableFilter).Scan(&n)
		out["active_count"] = n
	}

	// Afiliados distintos vinculados a algum PRODUTOR que tenha produto listável.
	// O vínculo é por produtor (produtor_id), não por produto (produto_id=0 na base
	// real) — a versão antiga juntava por produto_id = COALESCE(wp_post_id, id) e dava
	// 0, espelhando o mesmo bug do afiliados_count por linha. Join canônico do produtor
	// (id OR wp_user_id) + productListableFilter (mesma exclusão do List/active_count).
	// Se sz_products ainda não existe, devolve 0 (degradação graciosa).
	if h.tableExists(ctx, "senderzz_affiliates") && h.tableExists(ctx, "sz_products") {
		var n int64
		_ = h.Pool.QueryRow(ctx,
			`SELECT COUNT(DISTINCT sa.afiliado_id)
			   FROM senderzz_affiliates sa
			  WHERE EXISTS (
			      SELECT 1 FROM sz_products sp
			      LEFT JOIN senderzz_portal_users pu ON pu.id = sp.produtor_id
			      WHERE (sa.produtor_id = pu.id OR sa.produtor_id = pu.wp_user_id)`+
				productListableFilter+`
			  )`).Scan(&n)
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
