// Package handlers — handler da Vitrine de produtos do Portal V2.
//
// Espelha templates/portal/v2/sections/vitrine.php — porta a "Vitrine de
// produtos" do portal para o backend Go, mantendo UX/UI e REGRAS idênticas ao WP.
//
// Rotas (namespace /wp-json/senderzz/v1):
//
//	GET  /portal/vitrine            — catálogo GLOBAL de produtos + CDs (com overlay do usuário)
//	POST /portal/vitrine/affiliate  — solicita afiliação a um produtor (request_affiliation)
//
// A edição da descrição da vitrine (modal, tela "Produto", só o produtor dono)
// reaproveita a rota já existente POST /portal/products/{id}/vitrine-description
// (ProductsHandler.SaveVitrineDescription) — NÃO duplicamos aqui.
//
// ── CATÁLOGO É GLOBAL (não user-scoped) — ponto crítico ───────────────────────
//
//	A vitrine é uma tela de DESCOBERTA: produtor E afiliado veem TODOS os produtos
//	publicados, idênticos para os dois (vitrine.php faz wc_get_products(limit=-1)
//	sem gate por role — $sz9vt_is_aff é calculado mas nunca filtra a lista). O
//	objetivo é justamente se afiliar a produtores aos quais você AINDA NÃO está
//	vinculado. Aplicar o escopo do products.go (produtor vê o seu / afiliado o seu)
//	QUEBRARIA a feature. Logo: List = TODOS os sz_products ativos (mesmos filtros
//	recarga/frete/kit), sem branch por role.
//
//	A única parte user-scoped é o OVERLAY por card:
//	  - aff_status : senderzz_affiliates a
//	        ON (pu.id = a.produtor_id OR pu.wp_user_id = a.produtor_id)
//	       AND a.afiliado_id = u.wp_user_id   → null | pending | active
//	    (CANONICAL: afiliado casa por wp_user_id; produtor por id OU wp_user_id —
//	     espelha o Commissions handler. NUNCA IN(u.id,u.wp_user_id).)
//	  - is_own     : produtor.wp_user_id == u.wp_user_id (WP compara producer_id === wp_uid)
//
// ── id-space CANÔNICO (porte fiel — nunca improvisar) ─────────────────────────
//   - sz_products.produtor_id          = senderzz_portal_users.id  (portal id)
//   - sz_orders.produtor_id            = portal id do produtor (segue task + admin
//     producers.go; o comentário do schema-orders.sql diz wp_user_id mas o id-space
//     canônico do projeto é portal id — não improvisar).
//   - sz_orders.affiliate_id           = wp_user_id do afiliado.
//   - senderzz_affiliates.afiliado_id  = wp_user_id do afiliado.
//   - senderzz_affiliates.produtor_id  = id OU wp_user_id do produtor (join canônico).
//
// ── REGRAS DE NEGÓCIO portadas fielmente do WP ────────────────────────────────
//   - Stats por card (espelha vitrine.php):
//     vendidos          = SUM(sz_order_items.quantidade) dos pedidos PAGOS do
//     produto (produto_id = COALESCE(sp.wp_post_id, sp.id)).
//     total_distribuido = SUM(sz_orders.affiliate_amount) do produtor (comissão
//     já distribuída a afiliados). Degrada a 0 se não migrado.
//     comm_pct          = comissão real DEFINIDA PELO PRODUTOR (governança:
//     comissão de afiliado é do produtor, não do admin). Lida de
//     senderzz_affiliates.comissao_pct (MAX dos vínculos ativos do
//     produtor, ex.: 60% p/ Datalaprox/Dorvax); produtor sem vínculo
//     ativo cai no global sz_aff_default_commission_pct (default 10)
//     — fiel ao WP (sz_aff_producer_default_commission_pct).
//     comm_max          = maior (oferta.display_value * comm_pct/100) entre as
//     ofertas do produtor — "Ganhe até R$ X por venda".
//   - Filtra "não-produtos": recarga / carteira de frete / frete interno + kits
//     (kit|combo|pacote|pack|bundle|conjunto) — espelha o preg_match do WP.
//   - request_affiliation: cria SEMPRE status='pending' (aprovação = PRODUTOR).
//     Guards do WP: produtor inválido; não afiliar ao próprio produtor; já existe
//     vínculo. Mensagens PT-BR idênticas ao WP.
//
// ── LIMITAÇÕES DO ESPELHO PG (degradação graciosa, como products.go) ──────────
//   - senderzz_affiliates do PG NÃO tem deleted_at nem total_sales: o "já existe
//     vínculo" usa só (produtor_id, afiliado_id); o total distribuído vem do ledger
//     de pedidos (affiliate_amount), não de total_sales.
//   - produto_id é NOT NULL + UNIQUE(produtor_id,afiliado_id,produto_id): a
//     solicitação de afiliação é por PRODUTOR (sem produto), então gravamos
//     produto_id=0 — satisfaz a constraint, dedup pela UNIQUE, e o overlay
//     (que só precisa de QUALQUER linha do par) continua detectando o vínculo.
//   - imagem do produto: não há coluna dedicada no espelho PG; tentamos sp.meta->>'image'.
//     HOJE sz_products.meta está NULL p/ TODOS os produtos (o import não trouxe a URL de
//     imagem — no WP a foto vem de wp_get_attachment_image_url(get_image_id())). Logo o
//     card cai no placeholder. NÃO inventamos URL: assim que o import popular sp.meta
//     com {"image": "..."} (ou surgir coluna dedicada), a foto aparece sem mudar o front. // VITRINE-FOTO
package handlers

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/portal-service/internal/auth"
	"github.com/senderzz/portal-service/internal/httpx"
)

// VitrineHandler agrupa as dependências dos handlers da vitrine.
// Construção idêntica a WebhookHandler/ProductsHandler — o integrador monta com
// &handlers.VitrineHandler{Pool: pool}.
type VitrineHandler struct {
	Pool *pgxpool.Pool
}

// defaultAffiliateCommissionPct espelha sz_aff_default_commission_pct (default 10
// em senderzz-affiliates.php / admin affiliate_rules.go). Usado como teto de
// comissão padrão do produtor quando não há override por-produtor no espelho PG.
const defaultAffiliateCommissionPct = 10.0

// vitrineProductsLimit é o teto do catálogo da vitrine (espelha o LIMIT 1000
// histórico). AUDIT PERF-list-endpoints-hard-limit: a List busca limit+1 para
// detectar truncamento (has_more) sem COUNT extra e devolve só o teto.
const vitrineProductsLimit = 1000

// ── Shapes de resposta ────────────────────────────────────────────────────────

// vitrineCD — uma localidade (CD) da tela 2 do modal. NÃO reaproveita o cdItem
// do products.go porque a vitrine precisa de cidade/uf p/ a legenda "Cidade – UF"
// sob o nome do CD (vitrine.php tela 2: sub = [city, uf].join(' – ')). Espelha o
// SELECT id, nome, cidade, uf FROM sz_motoboy_cds WHERE ativo do WP.
type vitrineCD struct {
	ID     int64  `json:"id"`
	Nome   string `json:"nome"`
	Cidade string `json:"cidade"`
	UF     string `json:"uf"`
}

// vitrineCard — um produto na grade da vitrine, espelhando os campos consumidos
// pela section (card + modal de 3 telas).
type vitrineCard struct {
	PID            int64             `json:"pid"`             // sz_products.id
	WPPostID       *int64            `json:"wp_post_id"`      // ID WC original (pode ser NULL)
	Name           string            `json:"name"`            // sp.nome
	Description    string            `json:"description"`     // sp.descricao (descrição da vitrine)
	DescriptionRaw string            `json:"description_raw"` // mesmo valor — edição inline no modal
	Image          string            `json:"image"`           // url (sp.meta->>'image' ou "")
	QtySold        int64             `json:"qty_sold"`        // vendidos (sz_order_items)
	Revenue        float64           `json:"revenue"`         // receita bruta dos itens vendidos
	CommPaid       float64           `json:"comm_paid"`       // total distribuído (sz_orders.affiliate_amount)
	CommPct        float64           `json:"comm_pct"`        // comissão % padrão do produtor (teto "Ganhe até" — MAX dos vínculos ativos)
	MyCommPct      float64           `json:"my_comm_pct"`     // comissão % ATIVA do PRÓPRIO viewer p/ este produtor (0 se não vinculado) — #69
	CommMax        float64           `json:"comm_max"`        // maior comissão R$/venda entre ofertas
	ProducerID     int64             `json:"producer_id"`     // wp_user_id do produtor (p/ afiliação e is_own)
	IsOwn          bool              `json:"is_own"`          // produtor == usuário logado
	AffStatus      *string           `json:"aff_status"`      // null | pending | active
	Links          []productCheckout `json:"links"`           // ofertas (reaproveita o shape de products.go)
	CDs            []vitrineCD       `json:"cds"`             // localidades (mesmas p/ todos — armazém compartilhado)

	// producerPortalID — portal id do produtor (sz_products.produtor_id). Interno:
	// chave p/ casar stats por produtor (total distribuído / comissão default).
	// Não serializa (a UI usa producer_id = wp_user_id p/ afiliação).
	producerPortalID int64
}

// ── GET /portal/vitrine ───────────────────────────────────────────────────────

// List retorna o catálogo GLOBAL de produtos da vitrine + os CDs, com o overlay
// do usuário autenticado (aff_status / is_own / comm_pct / comm_max / stats).
//
// NÃO escopa o catálogo por role (ver doc do pacote) — só o overlay é por usuário.
// Degrada a listas/zeros se uma tabela do espelho ainda não existir (como products.go).
func (h *VitrineHandler) List(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	ctx := r.Context()

	// ── 1. Produtos (catálogo global) + produtor (id/wp_user_id) + overlay aff_status.
	//
	// Overlay de afiliação via subquery escalar com o join canônico:
	//   senderzz_affiliates a ON (pu.id = a.produtor_id OR pu.wp_user_id = a.produtor_id)
	//                        AND a.afiliado_id = u.wp_user_id   (NUNCA IN(u.id,u.wp_user_id)).
	// ORDER BY prioriza 'active' > 'pending' > resto p/ o overlay refletir o melhor vínculo.
	rows, err := h.Pool.Query(ctx,
		`SELECT sp.id,
		        sp.wp_post_id,
		        sp.nome,
		        COALESCE(sp.descricao, '')      AS descricao,
		        COALESCE(sp.meta->>'image', '') AS image,
		        COALESCE(pu.id, 0)              AS producer_portal_id,
		        COALESCE(pu.wp_user_id, 0)      AS producer_wp_id,
		        (
		            SELECT a.status
		              FROM senderzz_affiliates a
		             WHERE (pu.id = a.produtor_id OR pu.wp_user_id = a.produtor_id)
		               AND a.afiliado_id = $1
		             ORDER BY CASE a.status WHEN 'active' THEN 0 WHEN 'pending' THEN 1 ELSE 2 END
		             LIMIT 1
		        )                               AS aff_status,
		        -- #69: comissão % do PRÓPRIO viewer (vínculo ATIVO) com este produtor.
		        -- É a fonte preferida de "sua comissão" na aba Ofertas (mais fiel que o
		        -- comm_pct = MAX dos vínculos, que é o teto "Ganhe até"). 0 → o front
		        -- cai no fallback (padrão do produtor → global).
		        COALESCE((
		            SELECT a.comissao_pct
		              FROM senderzz_affiliates a
		             WHERE (pu.id = a.produtor_id OR pu.wp_user_id = a.produtor_id)
		               AND a.afiliado_id = $1
		               AND a.status = 'active'
		               AND a.comissao_pct > 0
		             ORDER BY a.comissao_pct DESC
		             LIMIT 1
		        ), 0)::float8                   AS my_comm_pct
		   FROM sz_products sp
		   LEFT JOIN senderzz_portal_users pu
		          ON pu.id = sp.produtor_id
		         AND pu.role = 'produtor'
		  WHERE sp.status IS DISTINCT FROM 'deleted'
		    -- APROVAÇÃO DE PRODUTO: a vitrine (storefront público/afiliados) NÃO
		    -- mostra produto pendente nem reprovado — só entra após o admin aprovar.
		    -- Produtos legados (active/inactive/draft/archived) continuam visíveis:
		    -- excluímos só os 2 estados negativos do ciclo de aprovação, não exigimos
		    -- 'aprovado' (não esconde catálogo pré-feature). // produto-aprovacao
		    AND sp.status NOT IN ('a_aprovar', 'reprovado')
		    -- TOGGLE "Visível na vitrine" (migração 479): produto desligado pelo
		    -- admin/produtor não aparece na descoberta. COALESCE(...,true) mantém os
		    -- produtos legados (coluna ausente em valor → true) visíveis. // vitrine-visible
		    AND COALESCE(sp.vitrine_visible, true) = true
		    AND sp.nome NOT ILIKE '%recarga%'
		    AND sp.nome NOT ILIKE '%carteira de frete%'
		    AND sp.nome NOT ILIKE '%frete interno%'
		    AND sp.nome !~* '\m(kit|combo|pacote|pack|bundle|conjunto)\M'
		  ORDER BY sp.nome ASC
		  LIMIT $2`,
		// N+1: busca 1 a mais que o teto p/ detectar truncamento sem COUNT. // PERF-list-endpoints-hard-limit
		u.WPUserID, vitrineProductsLimit+1,
	)
	if err != nil {
		if isUndefinedTable(err) {
			httpx.WriteOK(w, map[string]any{"products": []vitrineCard{}, "cds": []vitrineCD{}, "total": 0, "has_more": false, "limit": vitrineProductsLimit})
			return
		}
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer rows.Close()

	cards := []vitrineCard{}
	postIDs := []int64{}
	producerSeen := map[int64]bool{}
	producerIDs := []int64{}
	for rows.Next() {
		var (
			c                vitrineCard
			producerPortalID int64
			producerWPID     int64
			affStatus        *string
			myCommPct        float64
		)
		if err := rows.Scan(
			&c.PID, &c.WPPostID, &c.Name, &c.Description, &c.Image,
			&producerPortalID, &producerWPID, &affStatus, &myCommPct,
		); err != nil {
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao ler produtos")
			return
		}
		c.DescriptionRaw = c.Description
		c.ProducerID = producerWPID // WP grava producer_id = wp_user_id no card
		c.IsOwn = producerWPID != 0 && producerWPID == u.WPUserID
		c.AffStatus = affStatus
		c.MyCommPct = myCommPct
		c.Links = []productCheckout{}
		c.CDs = []vitrineCD{}
		c.producerPortalID = producerPortalID

		cards = append(cards, c)
		if c.WPPostID != nil {
			postIDs = append(postIDs, *c.WPPostID)
		}
		// producerPortalID alimenta ofertas + comissão real (ambos chaveados por
		// produtor, não por produto — ver fetchOffers/fetchProducerCommissions).
		if producerPortalID != 0 && !producerSeen[producerPortalID] {
			producerSeen[producerPortalID] = true
			producerIDs = append(producerIDs, producerPortalID)
		}
	}
	if rows.Err() != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao processar produtos")
		return
	}

	// has_more=true quando veio a linha extra → o front sabe que o catálogo foi
	// truncado (antes a truncagem em 1000 era silenciosa). Devolve só o teto.
	// // PERF-list-endpoints-hard-limit
	hasMore := len(cards) > vitrineProductsLimit
	if hasMore {
		cards = cards[:vitrineProductsLimit]
		// postIDs pode ter um id extra do card removido — irrelevante (só alimenta
		// lookups por chave; sem card correspondente, o resultado é descartado).
	}

	// ── 2. CDs (localidades) — mesmos p/ todos (armazém compartilhado), como no WP.
	cds := h.fetchCDs(ctx)

	// ── 3. Vendidos + receita por produto (sz_order_items dos pedidos PAGOS).
	//   produto casa por COALESCE(sp.wp_post_id, sp.id) (espelha _product_id do WP).
	soldByPost, revenueByPost := h.fetchSales(ctx, postIDs)

	// ── 4. Total distribuído POR PRODUTO (SUM(sz_orders.affiliate_amount) via
	//   sz_order_items). Chave = produto_id (= postKey do card). Antes era por
	//   produtor e vazava o mesmo total p/ todos os cards do produtor.
	commByProduct := h.fetchDistributed(ctx, postIDs)

	// ── 5. Ofertas (checkouts) por PRODUTOR — espelha $sz9vt_links_by_producer do WP.
	//   Chave = producer_id (portal id), NÃO post_id (ver doc de fetchOffers).
	linksByProducer := h.fetchOffers(ctx, u, producerIDs)

	// ── 6. Comissão % real por PRODUTOR (senderzz_affiliates.comissao_pct) + global
	//   como fallback p/ produtor sem vínculo ativo (fiel ao WP).
	commPctByProducer := h.fetchProducerCommissions(ctx, producerIDs)
	defaultPct := h.defaultCommissionPct(ctx)

	// ── 7. Costura tudo nos cards + calcula comm_pct / comm_max.
	for i := range cards {
		c := &cards[i]
		c.CDs = cds

		postKey := c.PID // fallback p/ produto nativo Go (sem WC post)
		if c.WPPostID != nil && *c.WPPostID != 0 {
			postKey = *c.WPPostID
		}
		c.QtySold = soldByPost[postKey]
		c.Revenue = revenueByPost[postKey]
		c.CommPaid = commByProduct[postKey]

		// Comissão real do produtor: MAX(comissao_pct) dos vínculos ativos (ex.: 60%
		// p/ Datalaprox/Dorvax), com fallback ao global quando o produtor ainda não
		// tem afiliado ativo — fiel à governança WP (comissão é do produtor).
		c.CommPct = defaultPct
		if pct, ok := commPctByProducer[c.producerPortalID]; ok && pct > 0 {
			c.CommPct = pct
		}

		// Ofertas (por produtor) + maior comissão R$/venda (espelha $sz9vt_best_comm
		// do WP: melhor oferta por preço → comissão absoluta = preço * pct / 100).
		if lks, ok := linksByProducer[c.producerPortalID]; ok {
			c.Links = lks
			var bestPrice float64
			for _, lk := range lks {
				if lk.DisplayValue > bestPrice {
					bestPrice = lk.DisplayValue
				}
			}
			c.CommMax = bestPrice * c.CommPct / 100.0
		}
	}

	httpx.WriteOK(w, map[string]any{
		"products": cards,
		"cds":      cds,
		"total":    len(cards),
		"has_more": hasMore,              // PERF-list-endpoints-hard-limit
		"limit":    vitrineProductsLimit, // PERF-list-endpoints-hard-limit
	})
}

// ── POST /portal/vitrine/affiliate ────────────────────────────────────────────

// affiliateRequest é o body de POST /portal/vitrine/affiliate.
type affiliateRequest struct {
	ProducerID int64 `json:"producer_id"`
}

// Affiliate solicita afiliação a um produtor (espelha request_affiliation do WP).
//
// Guards (mensagens PT-BR idênticas ao WP):
//   - producer_id ausente/0           → "Produtor inválido."
//   - producer_id == próprio usuário  → "Não é possível se afiliar ao próprio produtor."
//   - já existe vínculo (produtor,afiliado) → "Já existe um vínculo de afiliação com este produtor."
//
// Insere SEMPRE status='pending' (aprovação é do PRODUTOR). produto_id=0 satisfaz
// a constraint NOT NULL + UNIQUE(produtor_id,afiliado_id,produto_id) do espelho PG.
func (h *VitrineHandler) Affiliate(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	var req affiliateRequest
	if err := decodeJSONBody(r, &req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}

	// Guard 1: produtor inválido (espelha absint == 0 do WP).
	if req.ProducerID <= 0 {
		httpx.WriteOK(w, map[string]any{"success": false, "message": "Produtor inválido."})
		return
	}

	// Guard 2: não afiliar ao próprio produtor (WP compara target === wp_uid).
	if req.ProducerID == u.WPUserID {
		httpx.WriteOK(w, map[string]any{"success": false, "message": "Não é possível se afiliar ao próprio produtor."})
		return
	}

	ctx := r.Context()

	// Guard 3: já existe vínculo (produtor, afiliado). O espelho PG não tem
	// deleted_at — basta o par (produtor_id = wp_user_id do produtor). Mantém a
	// parity WP: um vínculo 'revoked' (recusado pelo produtor) BLOQUEIA re-solicitação
	// (espelha o deleted_at do WP, que preserva a linha). O CANCELAMENTO do próprio
	// usuário (#72) é HARD-DELETE (CancelAffiliation), então não deixa linha p/ travar.
	var existingID int64
	errExist := h.Pool.QueryRow(ctx,
		`SELECT id FROM senderzz_affiliates
		  WHERE produtor_id = $1
		    AND afiliado_id = $2
		  LIMIT 1`,
		req.ProducerID, u.WPUserID,
	).Scan(&existingID)
	if errExist == nil {
		httpx.WriteOK(w, map[string]any{"success": false, "message": "Já existe um vínculo de afiliação com este produtor."})
		return
	}
	if errExist != pgx.ErrNoRows {
		if isUndefinedTable(errExist) {
			httpx.WriteErr(w, http.StatusServiceUnavailable, "afiliação indisponível no momento")
			return
		}
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Comissão padrão do produtor-alvo (governança: definida pelo produtor; sem
	// override por-produtor no espelho PG → global, default 10).
	commPct := h.defaultCommissionPct(ctx)

	// Insere o vínculo pendente. produto_id=0 (vínculo por produtor, sem produto).
	// Sem ON CONFLICT: o Guard 3 acima já garante que não há linha do par (o cancel
	// do #72 é HARD-DELETE, não deixa 'revoked' p/ colidir). Plain VALUES — parity WP.
	_, err := h.Pool.Exec(ctx,
		`INSERT INTO senderzz_affiliates
		    (produtor_id, afiliado_id, produto_id, status, comissao_pct, created_at)
		 VALUES ($1, $2, 0, 'pending', $3, NOW())`,
		req.ProducerID, u.WPUserID, commPct,
	)
	if err != nil {
		if isUndefinedTable(err) {
			httpx.WriteErr(w, http.StatusServiceUnavailable, "afiliação indisponível no momento")
			return
		}
		httpx.WriteOK(w, map[string]any{"success": false, "message": "Erro ao registrar afiliação."})
		return
	}

	httpx.WriteOK(w, map[string]any{"success": true, "message": "Solicitação enviada. Aguarde aprovação do produtor."})
}

// ── POST /portal/vitrine/affiliate/cancel ─────────────────────────────────────

// CancelAffiliation CANCELA um pedido de afiliação PENDENTE do próprio usuário (#72).
//
// Escopo ESTRITO ao solicitante: só remove um vínculo onde afiliado_id = u.WPUserID
// (atribuição estrita por wp_user_id — id-space canônico) E status = 'pending'. Um
// vínculo JÁ APROVADO ('active') NÃO é cancelável por aqui (isso é "sair do programa",
// fluxo distinto governado pelo produtor via Delete). O produtor é identificado pelo
// mesmo producer_id (= wp_user_id do produtor) usado no Affiliate.
//
// HARD-DELETE (e NÃO soft 'revoked'): o overlay aff_status da vitrine (List) NÃO
// filtra por status (ORDER BY active>pending>resto, LIMIT 1), então uma linha
// 'revoked' VAZARIA como aff_status='revoked' no próximo reload — e o front trata só
// active|pending|null (card ficaria sem botão Afiliar-me nem Cancelar). Apagando a
// linha, o overlay volta a null → o card mostra "Afiliar-me" de novo e o usuário pode
// re-solicitar (INSERT plain, sem colidir com a UNIQUE). O 'revoked' continua RESERVADO
// para a recusa do PRODUTOR (que deve bloquear re-solicitação — parity WP); só o
// cancelamento do PRÓPRIO usuário apaga de fato.
func (h *VitrineHandler) CancelAffiliation(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	var req affiliateRequest
	if err := decodeJSONBody(r, &req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}
	if req.ProducerID <= 0 {
		httpx.WriteOK(w, map[string]any{"success": false, "message": "Produtor inválido."})
		return
	}

	ctx := r.Context()

	// Hard-delete SOMENTE o vínculo PENDENTE do próprio usuário com este produtor.
	// Escopo estrito por afiliado_id (wp_user_id) + status='pending' — não toca vínculo
	// ativo nem de terceiros.
	//
	// id-space do produtor: o front envia producer_id = card.producer_id (= wp_user_id
	// do produtor). Mas o vínculo pode ter sido criado por dois caminhos com produtor_id
	// DIFERENTE: Affiliate grava wp_user_id; RedeemInvite grava o PORTAL id. O overlay
	// da List (que decide quando o botão Cancelar aparece) usa a OR-join canônica
	// (pu.id = a.produtor_id OR pu.wp_user_id = a.produtor_id) — então um redeem-pending
	// TAMBÉM aparece como 'pending' no card. Usamos a MESMA OR-form aqui p/ o Cancelar
	// casar os dois id-spaces (senão o redeem-pending mostraria o botão mas o DELETE não
	// acharia nada). O bound de segurança continua sendo afiliado_id=$1 + status='pending'.
	res, err := h.Pool.Exec(ctx,
		`DELETE FROM senderzz_affiliates a
		  USING senderzz_portal_users pu
		  WHERE a.afiliado_id = $1
		    AND a.status = 'pending'
		    AND (pu.id = $2 OR pu.wp_user_id = $2)
		    AND (a.produtor_id = pu.id OR a.produtor_id = pu.wp_user_id)`,
		u.WPUserID, req.ProducerID,
	)
	if err != nil {
		if isUndefinedTable(err) {
			httpx.WriteErr(w, http.StatusServiceUnavailable, "afiliação indisponível no momento")
			return
		}
		httpx.WriteOK(w, map[string]any{"success": false, "message": "Erro ao cancelar afiliação."})
		return
	}
	if res.RowsAffected() == 0 {
		httpx.WriteOK(w, map[string]any{"success": false, "message": "Nenhuma solicitação pendente encontrada."})
		return
	}

	httpx.WriteOK(w, map[string]any{"success": true, "message": "Solicitação cancelada."})
}

// ── Helpers internos ──────────────────────────────────────────────────────────

// fetchCDs retorna os CDs ativos (localidades) com cidade/uf p/ a legenda da
// tela 2 do modal. Espelha o SELECT id, nome, cidade, uf FROM sz_motoboy_cds
// WHERE ativo=1 ORDER BY nome do WP. Degrada a [] se a tabela faltar.
func (h *VitrineHandler) fetchCDs(ctx context.Context) []vitrineCD {
	out := []vitrineCD{}
	rows, err := h.Pool.Query(ctx,
		`SELECT id, nome, COALESCE(cidade, ''), COALESCE(uf, '')
		   FROM sz_motoboy_cds
		  WHERE ativo = TRUE
		  ORDER BY nome ASC
		  LIMIT 50`,
	)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var c vitrineCD
		if err := rows.Scan(&c.ID, &c.Nome, &c.Cidade, &c.UF); err != nil {
			return out
		}
		out = append(out, c)
	}
	return out
}

// fetchSales retorna vendidos (qty) e receita (subtotal) por produto, somando
// itens de pedidos PAGOS. Espelha o agregado de wc_order_items do WP (status
// completed/processing/enviado/entregue). Degrada a mapas vazios se faltar tabela.
func (h *VitrineHandler) fetchSales(ctx context.Context, postIDs []int64) (map[int64]int64, map[int64]float64) {
	sold := map[int64]int64{}
	revenue := map[int64]float64{}
	if len(postIDs) == 0 {
		return sold, revenue
	}
	rows, err := h.Pool.Query(ctx,
		`SELECT oi.produto_id,
		        COALESCE(SUM(oi.quantidade), 0)::bigint AS qty,
		        COALESCE(SUM(oi.subtotal), 0)::float8   AS total
		   FROM sz_order_items oi
		   JOIN sz_orders o ON o.id = oi.order_id
		  WHERE oi.produto_id = ANY($1)
		    AND o.status IN ('completo','processing','enviado','entregue')
		  GROUP BY oi.produto_id`,
		postIDs,
	)
	if err != nil {
		return sold, revenue
	}
	defer rows.Close()
	for rows.Next() {
		var pid, qty int64
		var total float64
		if err := rows.Scan(&pid, &qty, &total); err != nil {
			return sold, revenue
		}
		sold[pid] = qty
		revenue[pid] = total
	}
	return sold, revenue
}

// fetchDistributed retorna o total distribuído POR PRODUTO (chave = produto_id =
// postKey do card, espelhando _product_id do WP) — soma de sz_orders.affiliate_amount
// atribuída a cada produto via sz_order_items. Antes era agregado por PRODUTOR e
// colado em TODOS os cards do produtor (bug: Dorvax qty=0 exibia o mesmo total do
// Datalaprox). Agora cada produto recebe só a comissão dos pedidos que contêm aquele
// produto. O affiliate_amount é por PEDIDO (não por item), então o SELECT DISTINCT
// (order,produto) garante que cada par pedido↔produto conte o valor do pedido uma
// única vez — sem dupla-contagem por múltiplas linhas do mesmo produto no pedido.
// Pedido com N produtos distintos atribui o affiliate_amount a cada um (aproximação
// fiel; não há split por item no espelho PG). Degrada a {}.
func (h *VitrineHandler) fetchDistributed(ctx context.Context, postIDs []int64) map[int64]float64 {
	out := map[int64]float64{}
	if len(postIDs) == 0 {
		return out
	}
	rows, err := h.Pool.Query(ctx,
		`SELECT t.produto_id,
		        COALESCE(SUM(t.aa), 0)::float8 AS distribuido
		   FROM (
		        SELECT DISTINCT o.id, oi.produto_id, o.affiliate_amount AS aa
		          FROM sz_orders o
		          JOIN sz_order_items oi ON oi.order_id = o.id
		         WHERE oi.produto_id = ANY($1)
		   ) t
		  GROUP BY t.produto_id`,
		postIDs,
	)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var pid int64
		var dist float64
		if err := rows.Scan(&pid, &dist); err != nil {
			return out
		}
		out[pid] = dist
	}
	return out
}

// fetchOffers retorna as ofertas (checkouts) por PRODUTOR (portal id), reaproveitando
// o shape productCheckout do products.go. Degrada a {}. Limita a 8 ofertas por produtor
// (espelha $sz9vt_prod_links do WP). É ROLE-AWARE (regra do dono 2026-06-24):
//
//   - PRODUTOR (ou qualquer não-afiliado): vê as ofertas de EXPEDIÇÃO (correio) —
//     exclui tipo='motoboy', SEM ?r= (url pura). Comportamento histórico do WP.
//   - AFILIADO: o link que ele divulga é CASH ON DELIVERY (tipo='motoboy'), affiliate_visible,
//     e RASTREÁVEL (?r=<token> via appendRefToken, token = id do VÍNCULO). É a MESMA
//     fonte de affiliates_portal.listAffiliateLinks — o afiliado vende COD, não correio.
//
// CHAVE = producer_id, NÃO post_id. Espelha o WP ($sz9vt_links_by_producer): as ofertas
// da vitrine são agrupadas pelo PRODUTOR e mostradas em TODOS os cards daquele produtor.
// senderzz_checkout_links.post_id (ex.: 22, 1075) é o post WC da OFERTA/método, NÃO o
// wp_post_id do produto da vitrine (ex.: 1278 Datalaprox) — eles NUNCA casam. Casar por
// post_id (bug anterior) zerava a aba "Ofertas". O id-space do producer_id do checkout =
// sz_products.produtor_id = senderzz_affiliates.produtor_id (portal id). // VITRINE-OFERTAS
func (h *VitrineHandler) fetchOffers(ctx context.Context, u *auth.PortalUser, producerIDs []int64) map[int64][]productCheckout {
	out := map[int64][]productCheckout{}
	if len(producerIDs) == 0 {
		return out
	}
	isAff := u != nil && (u.Role == "afiliado" || u.Role == "cliente")
	if isAff {
		return h.fetchOffersAffiliate(ctx, u, producerIDs)
	}
	// FEAT-VITRINE-OFERTA-PCT (#69): lê a comissão POR-OFERTA direto da COLUNA
	// dedicada cl.affiliate_commission_pct (migração 428; links_portal.go:174 já a
	// lê em produção, então existe no espelho). 0.00 = "sem % própria" → o front cai
	// no fallback (pct do vínculo do afiliado → padrão do produtor → global). Antes
	// este SELECT trazia 0::float8 fixo e a aba Ofertas nunca via a % por-oferta.
	rows, err := h.Pool.Query(ctx,
		`SELECT cl.producer_id, cl.id, cl.name, cl.price_label, cl.display_value,
		        cl.tipo, cl.url, cl.slug, cl.affiliate_visible,
		        COALESCE(cl.affiliate_commission_pct, 0)::float8
		   FROM senderzz_checkout_links cl
		  WHERE cl.producer_id = ANY($1)
		    AND cl.tipo <> 'motoboy'
		    AND cl.display_value > 0
		  ORDER BY cl.display_value DESC, cl.id DESC
		  LIMIT 500`,
		producerIDs,
	)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var producerID int64
		var c productCheckout
		if err := rows.Scan(
			&producerID, &c.ID, &c.Name, &c.PriceLabel, &c.DisplayValue,
			&c.Tipo, &c.URL, &c.Slug, &c.AffiliateVisible, &c.CommissionPct,
		); err != nil {
			return out
		}
		if len(out[producerID]) < 8 {
			out[producerID] = append(out[producerID], c)
		}
	}
	return out
}

// fetchOffersAffiliate retorna, por PRODUTOR (portal id), as ofertas CASH ON DELIVERY
// (tipo='motoboy', affiliate_visible) que o afiliado pode divulgar — com a url RASTREÁVEL
// (?r=<token>, token = id do VÍNCULO do afiliado com aquele produtor). Espelha FIELMENTE
// affiliates_portal.listAffiliateLinks: o vínculo NÃO é por produto (produto_id costuma
// ser 0 = producer-wide) — casa por PRODUTOR (cl.producer_id ↔ vínculo ativo do afiliado).
// A Vitrine renderiza lk.url como o link da oferta, então o COD rastreável vai em URL.
func (h *VitrineHandler) fetchOffersAffiliate(ctx context.Context, u *auth.PortalUser, producerIDs []int64) map[int64][]productCheckout {
	out := map[int64][]productCheckout{}
	if u == nil || u.WPUserID == 0 || len(producerIDs) == 0 {
		return out
	}
	// Salt de referência (espelha get_option('sz_aff_ref_salt')). Sem salt → token vazio
	// (degradação graciosa; a url base ainda é copiável).
	refSalt := ""
	_ = h.Pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE name = $1`,
		"sz_aff_ref_salt",
	).Scan(&refSalt)

	rows, err := h.Pool.Query(ctx,
		`SELECT cl.producer_id, cl.id, cl.name, cl.price_label, cl.display_value,
		        cl.tipo, cl.url, cl.slug, cl.affiliate_visible,
		        COALESCE(cl.affiliate_commission_pct, 0)::float8,
		        (
		            SELECT a.id
		              FROM senderzz_affiliates a
		              JOIN senderzz_portal_users p
		                ON (p.id = a.produtor_id OR p.wp_user_id = a.produtor_id)
		               AND p.role = 'produtor'
		             WHERE p.id = cl.producer_id
		               AND a.afiliado_id = $2
		               AND a.status = 'active'
		             ORDER BY a.id ASC
		             LIMIT 1
		        ) AS vinculo_id
		   FROM senderzz_checkout_links cl
		  WHERE cl.producer_id = ANY($1)
		    AND cl.tipo = 'motoboy'
		    AND cl.affiliate_visible = TRUE
		    AND cl.display_value > 0
		  -- VITRINE = superfície de DESCOBERTA: a oferta aparece para QUALQUER
		  -- afiliado/cliente navegando (o opt-in do produtor é cl.affiliate_visible).
		  -- NÃO exigir vínculo ativo aqui — senão afiliado revogado/pendente/novo vê
		  -- "produtor sem ofertas" (bug). O LINK continua travado no front até afiliar
		  -- (vinculo_id só vem != NULL quando o vínculo está ativo → sem ?r= token).
		  ORDER BY cl.display_value DESC, cl.id DESC
		  LIMIT 500`,
		producerIDs, u.WPUserID,
	)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var producerID int64
		var c productCheckout
		var vinculoID *int64
		if err := rows.Scan(
			&producerID, &c.ID, &c.Name, &c.PriceLabel, &c.DisplayValue,
			&c.Tipo, &c.URL, &c.Slug, &c.AffiliateVisible, &c.CommissionPct,
			&vinculoID,
		); err != nil {
			return out
		}
		// URL RASTREÁVEL: COD + ?r=<token> (token = id do vínculo). A Vitrine usa lk.url
		// como o link da oferta — o afiliado abre/divulga o COD com a referência dele.
		if vinculoID != nil && *vinculoID > 0 && c.URL != "" {
			c.URL = appendRefToken(c.URL, *vinculoID, refSalt)
		}
		if len(out[producerID]) < 8 {
			out[producerID] = append(out[producerID], c)
		}
	}
	return out
}

// fetchProducerCommissions retorna a comissão % real POR PRODUTOR (portal id),
// lida do espelho senderzz_affiliates. No WP a comissão de afiliado é governada
// pelo PRODUTOR (sz_aff_producer_default_commission_pct lê o user_meta
// _sz_aff_default_commission_pct do produtor). O espelho PG não tem esse user_meta,
// mas TEM a comissão real acordada por vínculo (senderzz_affiliates.comissao_pct,
// ex.: 50–60% p/ o produtor 15) — fonte muito mais fiel que o default global 10%.
//
// Usa MAX(comissao_pct) dos vínculos ATIVOS do produtor: o card mostra "Ganhe ATÉ
// R$ X / X% por venda", então o melhor percentual ofertado é a leitura correta da
// headline. Produtores sem vínculo ativo caem no default global (fallback do caller).
// Degrada a {} se a tabela faltar. // VITRINE-COMISSAO
func (h *VitrineHandler) fetchProducerCommissions(ctx context.Context, producerIDs []int64) map[int64]float64 {
	out := map[int64]float64{}
	if len(producerIDs) == 0 {
		return out
	}
	rows, err := h.Pool.Query(ctx,
		`SELECT produtor_id, MAX(comissao_pct)::float8
		   FROM senderzz_affiliates
		  WHERE produtor_id = ANY($1)
		    AND status = 'active'
		    AND comissao_pct > 0
		  GROUP BY produtor_id`,
		producerIDs,
	)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var pid int64
		var pct float64
		if err := rows.Scan(&pid, &pct); err != nil {
			return out
		}
		if pct > 100 {
			pct = 100
		}
		out[pid] = pct
	}
	return out
}

// defaultCommissionPct lê a comissão % padrão global sz_aff_default_commission_pct
// (senderzz_options) com fallback/clamp — fiel a sz_aff_default_commission_pct do WP.
// Sem override por-produtor no espelho PG, todos os produtores caem aqui.
func (h *VitrineHandler) defaultCommissionPct(ctx context.Context) float64 {
	var raw string
	err := h.Pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE name = $1`,
		"sz_aff_default_commission_pct",
	).Scan(&raw)
	if err != nil {
		return defaultAffiliateCommissionPct
	}
	v, perr := strconv.ParseFloat(strings.TrimSpace(strings.ReplaceAll(raw, ",", ".")), 64)
	if perr != nil || v <= 0 {
		return defaultAffiliateCommissionPct
	}
	if v > 100 {
		v = 100
	}
	return v
}
