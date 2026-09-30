// Package handlers — handler de Relatórios do Portal V2 (user-scoped).
//
// Espelha templates/portal/v2/sections/reports.php. Relatórios do produtor
// (vendas / comissões / entregas por período), agregados sobre o MESMO conjunto
// de pedidos visíveis usado em orders.go — recortado pela sessão do portal.
//
// Rotas (namespace /wp-json/senderzz/v1):
//
//	GET /portal/reports  — métricas agregadas (COD/motoboy + Expedição) do período
//
// A tela é READ-ONLY (o CSV é gerado client-side em reports.php). Não há rota de
// mutação aqui.
//
// ── Escopo por usuário (fail-closed, equality estrita — nunca OR/IN) ───────────
//
// IDÊNTICO a orders.go (espelha senderzz_current_user_order_scope):
//   - role "produtor"            → WHERE o.produtor_id = $1   ($1 = portal id, u.ID)
//   - role "afiliado"/"affiliate"→ WHERE o.affiliate_id = $1  ($1 = wp_user_id, u.WPUserID)
//   - role "operator" (OL)/demais → escopo por class_ids não migrado em sz_orders →
//     métricas zeradas (fail-closed: NÃO mis-atribui pedidos de outro produtor).
//
// CANONICAL id-space (NÃO improvisar — idêntico a orders.go / admin affiliates.go):
//   - sz_orders attribution afiliado : o.affiliate_id = u.wp_user_id (NUNCA IN(id,wp_user_id))
//   - sz_orders attribution produtor : o.produtor_id  = u.id (portal id)
//
// SEGURANÇA — afiliado NUNCA recebe dados do produtor:
//
//	O escopo do afiliado é estritamente o.affiliate_id = u.WPUserID, então ele só
//	agrega os pedidos ATRIBUÍDOS A ELE. Além disso, o painel Expedição é OMITIDO p/
//	afiliado (has_exp=false, espelha $sz8rp_has_exp = !is_aff), exatamente como no WP.
//	Nenhuma taxa/líquido do produtor é exposta nas métricas (são counts + receita do
//	próprio escopo).
//
// ── Separação COD (motoboy) × Expedição ───────────────────────────────────────
//
// Espelha $sz8rp_is_mb de reports.php: um pedido é COD/motoboy quando
//
//	delivery_mode='motoboy'  OU  existe linha em sz_motoboy_pedidos (motoboy_status)
//	OU  o status é um dos status de fluxo motoboy.
//
// Caso contrário entra em Expedição. Afiliado só recebe o grupo COD (sem painel exp).
package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/portal-service/internal/auth"
	"github.com/senderzz/portal-service/internal/httpx"
)

// ReportsHandler agrupa as dependências dos handlers de relatórios.
// Construção idêntica a OrdersHandler/WebhookHandler (somente Pool) — wiring uniforme.
type ReportsHandler struct {
	Pool *pgxpool.Pool
}

// reportStatusRow — uma linha de "Pedidos por status". Pct é calculado server-side
// (igual à barra do WP: round(cnt / total_do_grupo * 100)).
type reportStatusRow struct {
	Status string `json:"status"` // status cru — label/cor é client-side (st_labels)
	Count  int    `json:"count"`
	Pct    int    `json:"pct"`
}

// reportProductRow — uma linha de "Produtos mais vendidos" (top 10 por faturamento).
type reportProductRow struct {
	Name    string  `json:"name"`
	Qty     int     `json:"qty"`
	Revenue float64 `json:"revenue"`
}

// reportRegionRow — uma linha de "Pedidos por região" (top 6 por contagem).
// Região = UF do endereço (billing-first), derivada de sz_order_addresses.uf.
type reportRegionRow struct {
	Region string `json:"region"`
	Count  int    `json:"count"`
	Pct    int    `json:"pct"`
}

// reportAffiliateRow — uma linha de "Afiliados ativos no período" = CAMPEÕES DE VENDA.
// SOMENTE produtor (dado do produtor; afiliado NUNCA recebe esta lista). Ordenado por
// faturamento desc; quem fez R$0 no período é excluído (HAVING faturamento > 0).
// effectiveness = entregues/(entregues+frustrados)*100 (0 se nenhum desfecho ainda).
type reportAffiliateRow struct {
	AffiliateID   int64   `json:"affiliate_id"`  // afiliado wp_user_id (= sz_orders.affiliate_id)
	Name          string  `json:"name"`          // COALESCE(nome, email, 'Afiliado #'||id)
	Revenue       float64 `json:"revenue"`       // faturamento no período (Σ total_no_ship, = KPI Faturamento)
	// AUDIT-2026-07-11: Effectiveness redefinida (pedido do dono) — agendado/embalado/
	// em_rota/entregue contam como EFETIVO; frustrado/cancelado descontam a eficiência.
	// = (total - frustrados - cancelados) / total * 100 (não mais entregues/(entregues+frustrados)).
	Effectiveness    int     `json:"effectiveness"`
	ComissaoAfiliado float64 `json:"comissao_afiliado"` // Σ sz_order_financials.affiliate_liquida no período
	ComissaoProdutor float64 `json:"comissao_produtor"` // Σ sz_order_financials.producer_net_live no período
	ComissaoFalk     float64 `json:"comissao_falk"`     // Σ (affiliate_take + producer_take) no período
}

// reportMetrics — bloco de métricas de UM grupo (COD ou Expedição). Espelha o array
// devolvido por sz9rp_metrics() em reports.php: KPIs + by_status + by_product + by_region.
type reportMetrics struct {
	Total           int                `json:"total"`            // total de pedidos do grupo
	Receita         float64            `json:"receita"`          // faturamento (Σ total_no_ship)
	ComissaoLiquida float64            `json:"comissao_liquida"` // Σ comissão LÍQUIDA de afiliados (sz_orders.affiliate_amount, já net 4,99% — #1587)
	LiquidoProdutor float64            `json:"liquido_produtor"` // Σ(total_no_ship − fee4.99% − comissão) de pedidos válidos (exceto frustrado/cancelado)
	Cancelados      int                `json:"cancelados"`       // status cancelled|cancelado
	Ticket          float64            `json:"ticket"`           // receita / total (0 se total=0)
	ByStatus        []reportStatusRow  `json:"by_status"`        // ordem de inserção (1ª ocorrência)
	ByProduct       []reportProductRow `json:"by_product"`       // top 10 por faturamento desc
	ByRegion        []reportRegionRow  `json:"by_region"`        // top 6 por contagem desc
}

// reportsResponse — payload de GET /portal/reports. has_exp espelha $sz8rp_has_exp:
// afiliado NÃO vê o painel Expedição (exp vem nulo e has_exp=false).
type reportsResponse struct {
	COD         reportMetrics        `json:"cod"`          // grupo motoboy/COD (sempre presente)
	Exp         *reportMetrics       `json:"exp"`          // grupo expedição (nil p/ afiliado)
	ByAffiliate []reportAffiliateRow `json:"by_affiliate"` // campeões de venda (produtor-only; abrange COD+exp)
	HasExp      bool                 `json:"has_exp"`      // false p/ afiliado (esconde aba Expedição)
	Total       int                  `json:"total"`        // total de pedidos AGREGADOS na janela (cap em limit)
	HasMore     bool                 `json:"has_more"`     // true se a janela foi truncada (KPIs parciais) // PERF-list-endpoints-hard-limit
	Limit       int                  `json:"limit"`        // teto de pedidos agregados // PERF-list-endpoints-hard-limit
	From        string               `json:"from"`         // janela aplicada (YYYY-MM-DD)
	To          string               `json:"to"`           // janela aplicada (YYYY-MM-DD)
	Role        string               `json:"role"`
	IsAff       bool                 `json:"is_affiliate"`
}

// listReportsLimit é o teto de pedidos AGREGADOS na janela de relatórios (espelha
// o LIMIT 500 histórico). AUDIT PERF-list-endpoints-hard-limit: a List busca
// limit+1 só para sinalizar has_more — quando truncado, os KPIs são PARCIAIS
// (calculados sobre os primeiros `limit` pedidos da janela). O agregado é feito
// sobre exatamente `limit` linhas (a sentinela limit+1 é descartada antes).
const listReportsLimit = 500

// reportCanceledStatuses — status que contam como "Cancelados" no KPI.
// Espelha o teste in_array($st, ['cancelled','cancelado']) de sz9rp_metrics.
var reportCanceledStatuses = map[string]bool{
	"cancelled": true,
	"cancelado": true,
}

// motoboyFlowStatuses — status de fluxo motoboy (espelha $mbst de $sz8rp_is_mb).
// exact match, nunca substring.
var motoboyFlowStatuses = map[string]bool{
	"aguardando": true, // pedido COD aceito/agendado (sz_orders usa 'aguardando', não 'agendado')
	"agendado":   true,
	"embalado":   true,
	"acaminho":   true,
	"emrota":     true,
	"em_rota":    true,
	"entregue":   true,
	"frustrado":  true,
	"cancelado":  true, // cancelado pelo motoboy = ainda era COD
	"devolvido":  true,
}

// tableExists — guarda de migração graceful (idêntico a orders.go / expedicao.go).
func (h *ReportsHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// reportOrderAgg — linha enxuta usada só p/ agregação (não vai pro JSON cru).
type reportOrderAgg struct {
	Status          string
	ProductName     string
	TotalNoShip     float64
	RawTotal        float64 // total bruto do pedido (antes do desconto de shipping)
	Region          string
	DeliveryMode    string
	IsMotoboyRow    bool
	AffiliateAmount float64 // sz_orders.affiliate_amount — comissão LÍQUIDA do afiliado
	TaxaFrustrado   float64 // sz_motoboy_pedidos.valor_taxa_frustrado
	TaxaEntrega     float64 // sz_motoboy_pedidos.valor_taxa
	// LiquidoFin: pré-calculado em sz_order_financials (0 = não disponível → deriva inline).
	LiquidoFin float64
}

// ── GET /portal/reports ─────────────────────────────────────────────────────────
//
// List devolve as métricas agregadas, separadas em COD (motoboy) e Expedição,
// recortadas pela sessão. Filtro de janela OPCIONAL via query string:
//
//	?from=YYYY-MM-DD&to=YYYY-MM-DD
//
// PARIDADE com reports.php: SEM from/to → set recente completo (cap 500), igual ao
// carregamento inicial da tela (as datas-default dos inputs são só client-side, no
// clique "Filtrar"). COM from/to → recorta a janela no servidor. Datas inválidas/
// incompletas são ignoradas (cai no set completo) — fail-safe, nunca 400 por filtro.
// from/to no envelope refletem a janela aplicada ("" = sem recorte).
//
// Envelope:
//
//	{ ok:true, cod:{...}, exp:{...}|null, has_exp:bool, total:N,
//	  from:"...", to:"...", role:"produtor", is_affiliate:false }
func (h *ReportsHandler) List(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	ctx := r.Context()

	// sz_orders ainda não migrada → 503 graceful (espelha orders.go / expedicao.go).
	if !h.tableExists(ctx, "sz_orders") {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "sz_orders ainda não migrada")
		return
	}

	isAffiliate := u.Role == "afiliado" || u.Role == "affiliate" || u.Role == "afiliada" || u.Role == "cliente"

	// Janela de datas — só recorta quando from E to são passados (= clique "Filtrar"
	// do WP). Sem params → from/to vazios → set recente completo (paridade c/ a tela).
	from, to := parseReportWindow(r.URL.Query().Get("from"), r.URL.Query().Get("to"))

	// has_exp = produtor com Expedição ATIVA. AUDIT-2026-07-11: opt-IN — default
	// INATIVA; só ativa quando o admin liga EXPLICITAMENTE settings.expedicao_ativa
	// em Produtores (mesma regra de links_portal.go::expedicaoAtiva). Afiliado nunca
	// tem expedição.
	hasExp := !isAffiliate
	if hasExp {
		var expFlag *string
		_ = h.Pool.QueryRow(ctx,
			`SELECT settings ->> 'expedicao_ativa' FROM senderzz_portal_users WHERE id = $1`,
			u.ID,
		).Scan(&expFlag)
		hasExp = expFlag != nil && *expFlag == "true"
	}

	// Resposta vazia (mas válida) p/ roles sem escopo migrado em sz_orders.
	emptyResp := func() {
		var exp *reportMetrics
		if hasExp {
			exp = &reportMetrics{ByStatus: []reportStatusRow{}, ByProduct: []reportProductRow{}, ByRegion: []reportRegionRow{}}
		}
		writeReports(w, reportsResponse{
			COD:         reportMetrics{ByStatus: []reportStatusRow{}, ByProduct: []reportProductRow{}, ByRegion: []reportRegionRow{}},
			Exp:         exp,
			ByAffiliate: []reportAffiliateRow{},
			HasExp:      hasExp,
			Total:       0,
			From:        from,
			To:          to,
			Role:        u.Role,
			IsAff:       isAffiliate,
		})
	}

	// Escopo por role — equality estrita, NUNCA OR/IN (evita cross-attribution).
	var whereScope string
	var scopeArg int64
	switch {
	case isAffiliate:
		// Afiliado: pedidos atribuídos ao seu wp_user_id. NUNCA vê dados do produtor.
		whereScope = "o.affiliate_id = $1"
		scopeArg = u.WPUserID
	case u.Role == "produtor":
		whereScope = "o.produtor_id = $1"
		scopeArg = u.ID
	default:
		// Operator (OL) e demais: escopo por class_ids ainda não migrado em sz_orders.
		// Métricas zeradas p/ NÃO mis-atribuir pedidos de outro produtor (fail-closed).
		slog.Info("[portal_reports] role sem escopo em sz_orders — métricas vazias",
			"user_id", u.ID, "role", u.Role)
		emptyResp()
		return
	}

	// Tabelas auxiliares (degradação graciosa via subquery escalar — igual a orders.go).
	hasMeta := h.tableExists(ctx, "sz_order_meta")
	hasItems := h.tableExists(ctx, "sz_order_items")
	hasMotoboy := h.tableExists(ctx, "sz_motoboy_pedidos")

	// offer_name (== senderzz_offer_name) e offer_value (vence total_no_ship).
	offerNameSel := "''::text AS offer_name"
	offerValueSel := "0::float AS offer_value"
	deliveryModeSel := "''::text AS delivery_mode"
	if hasMeta {
		offerNameSel = `COALESCE((SELECT meta_value FROM sz_order_meta
		                 WHERE order_id = o.id AND meta_key='_senderzz_offer_name'
		                 LIMIT 1), '') AS offer_name`
		// Guard regex: meta não-numérica não derruba a agregação (cast-safe).
		offerValueSel = `COALESCE((SELECT CASE WHEN meta_value ~ '^[0-9]+(\.[0-9]+)?$'
		                                       THEN meta_value::numeric ELSE 0 END
		                  FROM sz_order_meta
		                  WHERE order_id = o.id AND meta_key='_senderzz_offer_value'
		                  LIMIT 1), 0)::float AS offer_value`
		deliveryModeSel = `COALESCE((SELECT meta_value FROM sz_order_meta
		                   WHERE order_id = o.id AND meta_key='_senderzz_delivery_mode'
		                   LIMIT 1), '') AS delivery_mode`
	}

	// Nome do produto = 1º item de sz_order_items (fallback de products_label).
	produtoSel := "''::text AS produto_nome"
	if hasItems {
		produtoSel = `COALESCE((SELECT nome FROM sz_order_items
		              WHERE order_id = o.id ORDER BY id ASC LIMIT 1), '') AS produto_nome`
	}

	// Região = UF do endereço (billing-first), derivada de sz_order_addresses.
	// Antes era hardcoded '' (paridade com um WP que não populava $o['region']),
	// o que deixava a seção "Pedidos por região" sempre em "Sem dados no período".
	// Agora pegamos a UF do endereço billing; se ausente, cai no shipping. Sem a
	// tabela de endereços (migração graceful) → '' (by_region vazio, como antes).
	regionSel := "''::text AS region"
	if h.tableExists(ctx, "sz_order_addresses") {
		regionSel = `COALESCE(
		               (SELECT a.uf FROM sz_order_addresses a
		                WHERE a.order_id = o.id AND a.tipo='billing' AND a.uf <> '' LIMIT 1),
		               (SELECT a.uf FROM sz_order_addresses a
		                WHERE a.order_id = o.id AND a.tipo='shipping' AND a.uf <> '' LIMIT 1),
		               '') AS region`
	}

	// has_mb_row = existe linha em sz_motoboy_pedidos (keyed por wp_order_id).
	// Espelha o ramo motoboy_status de $sz8rp_is_mb.
	hasMbRowSel := "false AS has_mb_row"
	taxaFrustradoSel := "0::float AS taxa_frustrado"
	if hasMotoboy {
		hasMbRowSel = `EXISTS(SELECT 1 FROM sz_motoboy_pedidos mp
		               WHERE mp.wc_order_id = COALESCE(o.wp_order_id, o.id)) AS has_mb_row`
		taxaFrustradoSel = `COALESCE((SELECT mp.valor_taxa_frustrado FROM sz_motoboy_pedidos mp
		               WHERE mp.wc_order_id = COALESCE(o.wp_order_id, o.id)
		               ORDER BY mp.id DESC LIMIT 1), 0)::float AS taxa_frustrado,
		               COALESCE((SELECT mp.valor_taxa FROM sz_motoboy_pedidos mp
		               WHERE mp.wc_order_id = COALESCE(o.wp_order_id, o.id)
		               ORDER BY mp.id DESC LIMIT 1), 0)::float AS taxa_entrega`
	}

	// mbStatusOverride: quando o motoboy cancelou o pedido mas sz_orders ainda está
	// 'aguardando', usa mp.status para refletir o status real no dashboard.
	mbStatusOverrideSel := "COALESCE(o.status,'') AS status"
	if hasMotoboy {
		mbStatusOverrideSel = `COALESCE(
		    NULLIF(
		        (SELECT mp.status FROM sz_motoboy_pedidos mp
		          WHERE mp.wc_order_id = COALESCE(o.wp_order_id, o.id)
		          ORDER BY mp.id DESC LIMIT 1),
		        ''),
		    o.status, '') AS status`
	}

	// total_no_ship: offer_value vence; senão max(0, total - shipping). Fiel a format_order.
	//
	// PARIDADE de janela com WP: reports.php carrega o conjunto recente visível
	// (senderzz_get_visible_orders_for_user, limit 500, sem filtro de data) e calcula
	// os KPIs sobre TODO ele — as datas de/até são apenas os valores-default dos inputs,
	// aplicados client-side só no clique "Filtrar". Logo:
	//   - SEM from/to → mesmo set recente do WP, sem piso de data (cap 500, igual à tela).
	//   - COM from/to → recorta a janela no servidor (o equivalente ao clique "Filtrar").
	// (orders.go LIMIT 300 / expedicao.go LIMIT 500 também não aplicam piso de data.)
	// Restringe a pedidos com linha em sz_motoboy_pedidos — paridade com /orders/motoboy
	// (inner join wc_order_id). Sem esta restrição, o dashboard conta sz_orders que nunca
	// tiveram despacho motoboy, divergindo da lista de pedidos.
	mbExistsClause := ""
	if hasMotoboy {
		mbExistsClause = `
		           AND EXISTS(SELECT 1 FROM sz_motoboy_pedidos mp WHERE mp.wc_order_id = COALESCE(o.wp_order_id, o.id))`
	}

	dateClause := ""
	args := []any{scopeArg}
	if from != "" && to != "" {
		dateClause = `
		           AND o.created_at >= $2::date
		           AND o.created_at <  ($3::date + INTERVAL '1 day')`
		args = append(args, from, to)
	}

	// N+1: busca 1 a mais que o teto p/ sinalizar has_more (KPIs parciais quando
	// truncado). O placeholder do LIMIT é o último arg (índice varia com a janela).
	// // PERF-list-endpoints-hard-limit
	args = append(args, listReportsLimit+1)
	limitPlaceholder := "$" + strconv.Itoa(len(args))

	sqlQ := `SELECT ` + mbStatusOverrideSel + `,
	                ` + produtoSel + `,
	                ` + offerNameSel + `,
	                ` + offerValueSel + `,
	                COALESCE(o.total,0)::float            AS total,
	                COALESCE(o.shipping,0)::float         AS shipping,
	                ` + deliveryModeSel + `,
	                ` + regionSel + `,
	                ` + hasMbRowSel + `,
	                COALESCE(o.affiliate_amount,0)::float AS affiliate_amount,
	                ` + taxaFrustradoSel + `,
	                COALESCE(f.liquido_produtor,0)::float AS liquido_fin
	         FROM sz_orders o
	         LEFT JOIN sz_order_financials f ON f.order_id = o.id
	         WHERE ` + whereScope + mbExistsClause + dateClause + `
	         ORDER BY o.id DESC
	         LIMIT ` + limitPlaceholder

	rows, err := h.Pool.Query(ctx, sqlQ, args...)
	if err != nil {
		slog.Error("[portal_reports] erro ao agregar pedidos", "user_id", u.ID, "role", u.Role, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer rows.Close()

	// fetched conta as linhas LIDAS p/ detectar truncamento; a sentinela (limit+1)
	// NÃO entra no agregado (KPIs sobre exatamente `limit` linhas). // PERF-list-endpoints-hard-limit
	fetched := 0
	var cod, exp []reportOrderAgg
	for rows.Next() {
		fetched++
		if fetched > listReportsLimit {
			break // sentinela: só sinaliza has_more, não agrega.
		}
		var (
			a           reportOrderAgg
			produtoNome string
			offerName   string
			offerValue  float64
			total       float64
			shipping    float64
		)
		if err := rows.Scan(
			&a.Status, &produtoNome, &offerName, &offerValue,
			&total, &shipping, &a.DeliveryMode, &a.Region, &a.IsMotoboyRow,
			&a.AffiliateAmount, &a.TaxaFrustrado, &a.TaxaEntrega, &a.LiquidoFin,
		); err != nil {
			slog.Error("[portal_reports] erro ao ler linha", "user_id", u.ID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao ler pedidos")
			return
		}

		// Normaliza status: remove prefixo "wc-" (sz_orders já guarda sem, por segurança).
		a.Status = strings.TrimPrefix(strings.ToLower(a.Status), "wc-")

		// products_label: offer_name vence; senão 1º item (== format_order).
		if offerName != "" {
			a.ProductName = offerName
		} else {
			a.ProductName = produtoNome
		}

		// total_no_ship: offer_value vence; senão max(0, total - shipping).
		a.RawTotal = total
		if offerValue > 0 {
			a.TotalNoShip = offerValue
		} else {
			v := total - shipping
			if v < 0 {
				v = 0
			}
			a.TotalNoShip = v
		}

		// Classificação COD × Expedição (espelha $sz8rp_is_mb):
		//   delivery_mode='motoboy' OU linha em sz_motoboy_pedidos OU status de fluxo motoboy.
		isMb := strings.EqualFold(a.DeliveryMode, "motoboy") ||
			a.IsMotoboyRow ||
			motoboyFlowStatuses[a.Status]

		if isMb {
			cod = append(cod, a)
		} else {
			exp = append(exp, a)
		}
	}
	if rows.Err() != nil {
		slog.Error("[portal_reports] erro após iteração", "user_id", u.ID, "err", rows.Err())
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao processar pedidos")
		return
	}

	// Métricas por grupo (espelha sz9rp_metrics).
	resp := reportsResponse{
		COD:     computeReportMetrics(cod),
		HasExp:  hasExp,
		Total:   len(cod) + len(exp),
		HasMore: fetched > listReportsLimit, // PERF-list-endpoints-hard-limit
		Limit:   listReportsLimit,
		From:    from,
		To:      to,
		Role:    u.Role,
		IsAff:   isAffiliate,
	}
	if hasExp {
		m := computeReportMetrics(exp)
		resp.Exp = &m
	}
	// Afiliado: resp.Exp permanece nil (omitido na UX — sem aba Expedição).

	// Campeões de venda (produtor-only). O afiliado NUNCA recebe dados de outros
	// afiliados/do produtor — só o produtor enxerga seu ranking. A janela de datas
	// aplicada é a mesma do recorte acima (from/to). // SEGURANÇA: equality estrita.
	if u.Role == "produtor" {
		resp.ByAffiliate = h.affiliateChampions(ctx, u.ID, from, to)
	}

	writeReports(w, resp)
}

// affiliateChampions devolve o ranking "Afiliados ativos no período" (campeões de
// venda) do PRODUTOR (portalID = u.ID). PARIDADE com o DB confirmado:
//   - sz_orders.affiliate_id = afiliado wp_user_id (NUNCA portal id).
//   - nome vem de senderzz_portal_users via u.wp_user_id = o.affiliate_id; sem
//     portal user → nome/email NULL → fallback 'Afiliado #'||id.
//
// Regras: ordena por faturamento desc; EXCLUI quem fez R$0 (HAVING > 0).
//
// "Vendeu R$" usa total_no_ship (= offer_value se houver, senão max(0,total-shipping)),
// MESMA semântica do KPI "Faturamento" do painel COD/Expedição — assim os dois números
// reconciliam na mesma tela (a soma do ranking não estoura o KPI). (offer_value vem de
// sz_order_meta._senderzz_offer_value; sem a tabela → cai p/ max(0,total-shipping).)
//
// effectiveness = entregues/(entregues+frustrados)*100 (delivered = completo/completed/
// entregue/delivered; frustrados = frustrado/devolvido — idêntico a DONE/FRUSTR do front).
// Divisão protegida (só conta quando entregues+frustrados > 0).
// SEGURANÇA: equality estrita o.produtor_id=$1 — nunca expõe pedidos de outro produtor.
func (h *ReportsHandler) affiliateChampions(ctx context.Context, portalID int64, from, to string) []reportAffiliateRow {
	out := []reportAffiliateRow{}

	// total_no_ship por pedido — paridade com o row-loop/sz9rp_metrics. offer_value
	// vence (>0); senão max(0, total-shipping). offer_value só quando sz_order_meta existe.
	noShipExpr := "GREATEST(COALESCE(o.total,0) - COALESCE(o.shipping,0), 0)"
	if h.tableExists(ctx, "sz_order_meta") {
		noShipExpr = `CASE WHEN COALESCE((SELECT CASE WHEN meta_value ~ '^[0-9]+(\.[0-9]+)?$'
		                                              THEN meta_value::numeric ELSE 0 END
		                                   FROM sz_order_meta
		                                   WHERE order_id = o.id AND meta_key='_senderzz_offer_value'
		                                   LIMIT 1), 0) > 0
		                  THEN (SELECT meta_value::numeric FROM sz_order_meta
		                        WHERE order_id = o.id AND meta_key='_senderzz_offer_value' LIMIT 1)
		                  ELSE GREATEST(COALESCE(o.total,0) - COALESCE(o.shipping,0), 0) END`
	}

	dateClause := ""
	args := []any{portalID}
	if from != "" && to != "" {
		dateClause = `
		           AND o.created_at >= $2::date
		           AND o.created_at <  ($3::date + INTERVAL '1 day')`
		args = append(args, from, to)
	}
	// AUDIT-2026-07-11: falhas = frustrado/devolvido/cancelado (desconta eficiência);
	// tudo mais (agendado/embalado/em_rota/entregue/etc.) é EFETIVO. Comissão somada
	// via sz_order_financials (affiliate_liquida/producer_net_live/take), já usado
	// sem guard em outros pontos deste arquivo (linha ~395).
	q := `SELECT o.affiliate_id,
	             COALESCE(NULLIF(u.nome,''), NULLIF(u.email,''), 'Afiliado #'||o.affiliate_id) AS name,
	             COALESCE(SUM(` + noShipExpr + `),0)::float AS revenue,
	             COUNT(*) AS total_pedidos,
	             COALESCE(SUM(CASE WHEN lower(o.status) IN ('frustrado','devolvido','cancelled','cancelado','emcancelamento') THEN 1 ELSE 0 END),0) AS falhas,
	             COALESCE(SUM(f.affiliate_liquida),0)::float AS comissao_afiliado,
	             COALESCE(SUM(f.producer_net_live),0)::float AS comissao_produtor,
	             COALESCE(SUM(f.affiliate_take + f.producer_take),0)::float AS comissao_falk
	      FROM sz_orders o
	      LEFT JOIN senderzz_portal_users u ON u.wp_user_id = o.affiliate_id
	      LEFT JOIN sz_order_financials f ON f.order_id = o.id
	      WHERE o.produtor_id = $1 AND o.affiliate_id IS NOT NULL` + dateClause + `
	      GROUP BY o.affiliate_id, u.nome, u.email
	      HAVING COALESCE(SUM(` + noShipExpr + `),0) > 0
	      ORDER BY revenue DESC, o.affiliate_id ASC
	      LIMIT 20`
	rows, err := h.Pool.Query(ctx, q, args...)
	if err != nil {
		slog.Error("[portal_reports] erro ao agregar campeões de afiliado", "produtor_id", portalID, "err", err)
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var (
			r      reportAffiliateRow
			total  int
			falhas int
		)
		if err := rows.Scan(&r.AffiliateID, &r.Name, &r.Revenue, &total, &falhas,
			&r.ComissaoAfiliado, &r.ComissaoProdutor, &r.ComissaoFalk); err != nil {
			slog.Error("[portal_reports] erro ao ler linha de campeão", "produtor_id", portalID, "err", err)
			return out
		}
		r.Revenue = round2(r.Revenue)
		r.ComissaoAfiliado = round2(r.ComissaoAfiliado)
		r.ComissaoProdutor = round2(r.ComissaoProdutor)
		r.ComissaoFalk = round2(r.ComissaoFalk)
		if total > 0 {
			r.Effectiveness = int(roundHalf(float64(total-falhas) / float64(total) * 100))
		}
		out = append(out, r)
	}
	if rows.Err() != nil {
		slog.Error("[portal_reports] erro após iteração de campeões", "produtor_id", portalID, "err", rows.Err())
	}
	return out
}

// writeReports serializa o envelope de relatórios via httpx.WriteOK (map[string]any).
// Espalha os campos no topo p/ o contrato { ok:true, cod, exp, has_exp, total, ... }
// (mesma forma documentada p/ o integrador). exp=nil → omitido (afiliado sem painel).
func writeReports(w http.ResponseWriter, resp reportsResponse) {
	byAff := resp.ByAffiliate
	if byAff == nil {
		byAff = []reportAffiliateRow{} // afiliado: lista sempre vazia (nunca nil → JSON [])
	}
	payload := map[string]any{
		"cod":          resp.COD,
		"by_affiliate": byAff, // campeões de venda (produtor-only; [] p/ afiliado)
		"has_exp":      resp.HasExp,
		"total":        resp.Total,
		"has_more":     resp.HasMore, // PERF-list-endpoints-hard-limit
		"limit":        resp.Limit,   // PERF-list-endpoints-hard-limit
		"from":         resp.From,
		"to":           resp.To,
		"role":         resp.Role,
		"is_affiliate": resp.IsAff,
	}
	if resp.Exp != nil {
		payload["exp"] = resp.Exp
	}
	httpx.WriteOK(w, payload)
}

// computeReportMetrics agrega um grupo de pedidos em KPIs + breakdowns.
// Espelha 1:1 a função sz9rp_metrics() de reports.php:
//   - receita = Σ total_no_ship; cancelados = status cancelled|cancelado
//   - ticket  = round(receita/total, 2)
//   - by_status: contagem por status, na ordem da 1ª ocorrência (PHP preserva inserção)
//   - by_product: top 10 por faturamento desc (rev); qty = nº de pedidos do produto
//   - by_region: top 6 por contagem desc
//
// Pct (barra) é calculado server-side: round(cnt / total_do_breakdown * 100).
func computeReportMetrics(orders []reportOrderAgg) reportMetrics {
	m := reportMetrics{
		ByStatus:  []reportStatusRow{},
		ByProduct: []reportProductRow{},
		ByRegion:  []reportRegionRow{},
	}
	if len(orders) == 0 {
		return m
	}

	m.Total = len(orders)

	// by_status preserva a ordem da 1ª ocorrência (como o array associativo do PHP).
	statusOrder := []string{}
	statusCount := map[string]int{}
	// by_product
	type prodAcc struct {
		qty int
		rev float64
	}
	prodMap := map[string]*prodAcc{}
	// by_region
	regionCount := map[string]int{}
	regionOrder := []string{}

	for _, o := range orders {
		m.Receita += o.TotalNoShip
		m.ComissaoLiquida += o.AffiliateAmount // Σ comissão líquida de afiliados (já net, #1587)
		if reportCanceledStatuses[o.Status] {
			m.Cancelados++
		}
		// LiquidoProdutor: usa sz_order_financials quando disponível (fonte única de verdade).
		// Fallback: calcula inline com afil_bruta = affiliate_amount/(1−fee_afil_pct).
		// Regra do painel: soma tudo que está em andamento/concluído e exclui apenas
		// frustrado/cancelado.
		if !reportCanceledStatuses[o.Status] && o.Status != "frustrado" && o.Status != "devolvido" {
			if o.LiquidoFin > 0 {
				m.LiquidoProdutor += o.LiquidoFin
			} else {
				afilBruta := round2(o.AffiliateAmount / 0.9501)
				fee := round2(o.RawTotal * 0.0499)
				net := o.TotalNoShip - afilBruta - fee - o.TaxaEntrega
				if net < 0 {
					net = 0
				}
				m.LiquidoProdutor += net
			}
		}

		if _, seen := statusCount[o.Status]; !seen {
			statusOrder = append(statusOrder, o.Status)
		}
		statusCount[o.Status]++

		if o.ProductName != "" {
			p := prodMap[o.ProductName]
			if p == nil {
				p = &prodAcc{}
				prodMap[o.ProductName] = p
			}
			p.qty++
			p.rev += o.TotalNoShip
		}

		if o.Region != "" {
			if _, seen := regionCount[o.Region]; !seen {
				regionOrder = append(regionOrder, o.Region)
			}
			regionCount[o.Region]++
		}
	}

	m.ComissaoLiquida = round2(m.ComissaoLiquida)
	m.LiquidoProdutor = round2(m.LiquidoProdutor)
	if m.Total > 0 {
		m.Ticket = round2(m.Receita / float64(m.Total))
	}

	// ── by_status (ordem de inserção; pct sobre Σ contagens = total do grupo) ──
	stTotal := 0
	for _, c := range statusCount {
		stTotal += c
	}
	for _, st := range statusOrder {
		c := statusCount[st]
		pct := 0
		if stTotal > 0 {
			pct = int(roundHalf(float64(c) / float64(stTotal) * 100))
		}
		m.ByStatus = append(m.ByStatus, reportStatusRow{Status: st, Count: c, Pct: pct})
	}

	// ── by_product (top 10 por faturamento desc; tie-break por nome p/ determinismo) ──
	products := make([]reportProductRow, 0, len(prodMap))
	for name, p := range prodMap {
		products = append(products, reportProductRow{Name: name, Qty: p.qty, Revenue: round2(p.rev)})
	}
	sort.SliceStable(products, func(i, j int) bool {
		if products[i].Revenue != products[j].Revenue {
			return products[i].Revenue > products[j].Revenue
		}
		return products[i].Name < products[j].Name
	})
	if len(products) > 10 {
		products = products[:10]
	}
	m.ByProduct = products

	// ── by_region (top 6 por contagem desc; pct sobre Σ contagens de região) ──
	rgTotal := 0
	for _, c := range regionCount {
		rgTotal += c
	}
	regions := make([]reportRegionRow, 0, len(regionCount))
	for _, rg := range regionOrder {
		regions = append(regions, reportRegionRow{Region: rg, Count: regionCount[rg]})
	}
	sort.SliceStable(regions, func(i, j int) bool {
		if regions[i].Count != regions[j].Count {
			return regions[i].Count > regions[j].Count
		}
		return regions[i].Region < regions[j].Region
	})
	if len(regions) > 6 {
		regions = regions[:6]
	}
	for i := range regions {
		if rgTotal > 0 {
			regions[i].Pct = int(roundHalf(float64(regions[i].Count) / float64(rgTotal) * 100))
		}
	}
	m.ByRegion = regions

	m.LiquidoProdutor = round2(m.LiquidoProdutor)
	if m.LiquidoProdutor < 0 {
		m.LiquidoProdutor = 0
	}

	return m
}

// parseReportWindow valida from/to (YYYY-MM-DD) e devolve a janela a aplicar.
//
// PARIDADE com WP: o recorte por data só acontece quando AMBOS from e to são passados
// (equivale ao clique "Filtrar" da tela). Sem isso → ("","") → o handler NÃO aplica
// piso de data e usa o set recente completo (igual ao carregamento inicial do WP).
// Datas inválidas/incompletas → ("","") (fail-safe — filtro nunca derruba com 400).
// Quando ambos válidos, garante from <= to (corrige trocando).
func parseReportWindow(fromQ, toQ string) (string, string) {
	const layout = "2006-01-02"
	if fromQ == "" || toQ == "" {
		return "", ""
	}
	from, errF := time.Parse(layout, fromQ)
	to, errT := time.Parse(layout, toQ)
	if errF != nil || errT != nil {
		return "", ""
	}
	if from.After(to) {
		from, to = to, from
	}
	return from.Format(layout), to.Format(layout)
}

// round2 é reusado de wallet.go (mesmo pacote handlers) — paridade com round($v,2)
// do PHP (half-away-from-zero). Usado em ticket / receita / faturamento por produto.

// roundHalf arredonda half-up (paridade com round() do PHP, que é half-away-from-zero
// p/ os valores não-negativos usados aqui — contagens/percentuais/dinheiro ≥ 0).
func roundHalf(v float64) int64 {
	if v < 0 {
		return int64(v - 0.5)
	}
	return int64(v + 0.5)
}
