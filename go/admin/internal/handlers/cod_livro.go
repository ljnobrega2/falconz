// Package handlers — endpoint admin para o Livro COD (tela financeira única).
// Reescrito conforme layout oficial do dono: 13 colunas por pedido.
//
// Colunas (nesta ordem): Pedido | Data | Situação | Produtor | Afiliado | Comissão % |
//   Valor pedido | Taxas | Frustrado produtor | Frustrado afiliado | Afiliado (R$) |
//   Produtor (R$) | Repasse.
//
// Fontes de dados (graceful degradation via tableExists):
//   - sz_orders ........................ pedido base (status, total, fees, splits, ids, datas)
//   - sz_order_meta .................... comissão %, penalidades, nomes (fallback)
//   - senderzz_affiliate_transactions . repasse (commission pending/approved) e penalidade (penalty)
//   - senderzz_portal_users ........... NOME do produtor e do afiliado
//
// JOINS (id-space validado no banco — NÃO trocar):
//   - PRODUTOR: produtor_id casa com senderzz_portal_users.id (role='produtor').
//               (produtor_id=15 → portal id 15 = "Gabriel Campos"; NÃO existe wp_user_id=15.)
//   - AFILIADO: affiliate_id casa com senderzz_portal_users.wp_user_id (SÓ wp_user_id, nunca IN(id,wp_user_id)).
//               (affiliate_id=28 → wp_user_id 28 = "Gabriel Matias"; id=28 = "Keven" seria errado.)
//   Os dois ids vivem em ESPAÇOS DIFERENTES neste dataset. Cada nome usa COALESCE(join, meta).
//
// Mapeamento de Situação (CHECK do banco: pending/processing/aguardando/on-hold/em_separacao/
//   embalado/enviado/entregue/completo/cancelled/frustrado/reembolsado):
//   - "Recebido"                = completo, entregue
//   - "Estornado / não recebido"= frustrado, cancelled, reembolsado
//   - "Previsto"                = qualquer outro (aguardando, embalado, etc.)
//
// Zeragem por situação:
//   - Afiliado (R$) = 0 quando estornado.
//   - Produtor (R$) = 0 quando previsto OU estornado (só vale quando recebido).
//   - Frustrado afiliado/produtor = 0 quando NÃO estornado.
//
// Repasse: Previsto (não recebido) | Pendente/Disponível (recebido) | Não repassar (estornado).
//   Disponível = existe commission approved/paid; Pendente = commission pending.
package handlers

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

type CodLivroHandler struct{ Pool *pgxpool.Pool }

// Constantes de status agrupados. Valores canônicos do CHECK de sz_orders.
// Atenção: o status estornado é 'reembolsado' (não 'refunded') — 'refunded' nunca casaria.
const (
	codLivroReceivedStatuses   = `'completo','entregue'`
	codLivroFrustratedStatuses = `'frustrado','cancelled','reembolsado'`
)

var codLivroDateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// codLivroDateRange resolve o intervalo (default: últimos 7 dias até hoje).
// Aceita apenas YYYY-MM-DD; valores inválidos caem no default. Garante from <= to.
func codLivroDateRange(r *http.Request) (from, to string) {
	q := r.URL.Query()
	from = q.Get("from")
	to = q.Get("to")
	now := time.Now()
	if !codLivroDateRe.MatchString(to) {
		to = now.Format("2006-01-02")
	}
	if !codLivroDateRe.MatchString(from) {
		from = now.AddDate(0, 0, -7).Format("2006-01-02")
	}
	if from > to {
		from, to = to, from
	}
	return from, to
}

func (h *CodLivroHandler) tableExists(ctx context.Context, name string) bool {
	return tableExistsCached(ctx, h.Pool, name) // AUDIT-2026-06-18 Onda2 (go-infoschema-cache)
}

// CodLivroSummary — KPIs do topo da tela.
// bruto_cod         = SUM(total) WHERE status != estornado
// afiliados         = SUM(affiliate_bruta) WHERE status != estornado
// taxas_senderzz    = SUM(delivery_fee + producer_take) WHERE status != estornado
// liquido_produtor  = SUM(producer_net_live) WHERE status != estornado
// previsto_produtor = SUM(producer_net_live) WHERE status NOT IN (recebido, estornado)
type CodLivroSummary struct {
	BrutoCOD         float64 `json:"bruto_cod"`
	Afiliados        float64 `json:"afiliados"`
	TaxasSenderzz    float64 `json:"taxas_senderzz"`
	LiquidoProdutor  float64 `json:"liquido_produtor"`
	PrevistoProdutor float64 `json:"previsto_produtor"`
}

// Summary retorna os 5 KPIs principais.
// GET /cod-livro/summary?from=YYYY-MM-DD&to=YYYY-MM-DD
func (h *CodLivroHandler) Summary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	from, to := codLivroDateRange(r)
	out := CodLivroSummary{}

	if !h.tableExists(ctx, "sz_orders") {
		httpx.JSON(w, 200, out)
		return
	}

	// Único round-trip: 5 sums em uma query só.
	// Filtro de data: created_at::date BETWEEN — evita problema de timestamptz truncando o último dia.
	// AUDIT-FINANCEIRO-2026-06-25: lê a VIEW canônica sz_order_financials (fonte única),
	// NÃO mais a coluna stale producer_net. Decomposição golden #1587:
	//   bruto = total · afiliados = affiliate_bruta (não a líquida — S8) · taxas FALK =
	//   delivery_fee + producer_take (S4: take afiliado já está dentro da bruta) ·
	//   líquido = producer_net_live (= total − bruta − delivery − take produtor).
	// Reconcilia: bruto = afiliados + taxas + líquido.
	_ = h.Pool.QueryRow(ctx,
		`SELECT
		   COALESCE(SUM(CASE WHEN status NOT IN (`+codLivroFrustratedStatuses+`) THEN total                       ELSE 0 END), 0) AS bruto_cod,
		   COALESCE(SUM(CASE WHEN status NOT IN (`+codLivroFrustratedStatuses+`) THEN affiliate_bruta             ELSE 0 END), 0) AS afiliados,
		   COALESCE(SUM(CASE WHEN status NOT IN (`+codLivroFrustratedStatuses+`) THEN delivery_fee + producer_take ELSE 0 END), 0) AS taxas_senderzz,
		   COALESCE(SUM(CASE WHEN status NOT IN (`+codLivroFrustratedStatuses+`) THEN producer_net_live           ELSE 0 END), 0) AS liquido_produtor,
		   COALESCE(SUM(CASE WHEN status NOT IN (`+codLivroReceivedStatuses+`)
		                       AND status NOT IN (`+codLivroFrustratedStatuses+`)
		                  THEN producer_net_live ELSE 0 END), 0) AS previsto_produtor
		 FROM sz_order_financials
		 WHERE created_at::date BETWEEN $1::date AND $2::date`,
		from, to,
	).Scan(&out.BrutoCOD, &out.Afiliados, &out.TaxasSenderzz, &out.LiquidoProdutor, &out.PrevistoProdutor)

	httpx.JSON(w, 200, out)
}

// CodLivroOrder — linha da tabela por pedido (13 colunas do layout oficial).
//   Pedido | Data | Situação | Produtor | Afiliado | Comissão % | Valor pedido | Taxas |
//   Frustrado produtor | Frustrado afiliado | Afiliado (R$) | Produtor (R$) | Repasse.
type CodLivroOrder struct {
	OrderID           int64   `json:"order_id"`          // Pedido (#)
	DataPedido        string  `json:"data_pedido"`       // Data
	Situacao          string  `json:"situacao"`          // Recebido | Previsto | Estornado / não recebido
	WcStatus          string  `json:"wc_status"`         // status cru (sublabel da Situação)
	ProducerID        int64   `json:"producer_id"`
	ProducerName      string  `json:"producer_name"`     // Produtor (NOME)
	AffiliateID       int64   `json:"affiliate_id"`
	AffiliateName     string  `json:"affiliate_name"`    // Afiliado (NOME)
	CommissionPct     float64 `json:"commission_pct"`    // Comissão %
	ValorPedido       float64 `json:"valor_pedido"`      // Valor pedido (total)
	Taxas             float64 `json:"taxas"`             // Taxas (entrega + transação, única)
	FrustradoProdutor float64 `json:"frustrado_produtor"` // penalidade produtor (0 se não estornado)
	FrustradoAfiliado float64 `json:"frustrado_afiliado"` // penalidade afiliado (0 se não estornado)
	AfiliadoRS        float64 `json:"afiliado_rs"`       // Afiliado (R$) líquido (0 se estornado)
	ProdutorRS        float64 `json:"produtor_rs"`       // Produtor (R$) líquido (0 se previsto/estornado)
	Repasse           string  `json:"repasse"`           // Previsto | Pendente | Disponível | Não repassar
}

// Orders retorna as linhas detalhadas (até 300).
// GET /cod-livro/orders?from=YYYY-MM-DD&to=YYYY-MM-DD&limit=300
func (h *CodLivroHandler) Orders(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	from, to := codLivroDateRange(r)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 300 {
		limit = 300
	}

	out := []CodLivroOrder{}
	if !h.tableExists(ctx, "sz_orders") {
		httpx.JSON(w, 200, map[string]any{"items": out, "count": 0})
		return
	}

	hasTx := h.tableExists(ctx, "senderzz_affiliate_transactions")
	hasUsers := h.tableExists(ctx, "senderzz_portal_users")
	hasMeta := h.tableExists(ctx, "sz_order_meta")

	// Subquery de transações de afiliado por pedido (GROUP BY order_id — NÃO multiplica linhas).
	//   tx_pending  : existe commission pending  → Repasse "Pendente"
	//   tx_available: existe commission approved/paid → Repasse "Disponível"
	//   tx_penalty  : soma penalty approved → Frustrado afiliado (fonte autoritativa).
	// Quando a tabela não existe, subquery vazia que sempre falha (tudo NULL → 0/false).
	txJoin := `LEFT JOIN (SELECT NULL::bigint AS order_id, FALSE AS tx_pending, FALSE AS tx_available,
	                              0::numeric AS tx_penalty) tx ON FALSE`
	if hasTx {
		txJoin = `LEFT JOIN (
			SELECT order_id,
			       bool_or(type='commission' AND status='pending')            AS tx_pending,
			       bool_or(type='commission' AND status IN ('approved','paid')) AS tx_available,
			       COALESCE(SUM(amount) FILTER (WHERE type='penalty'
			                AND status IN ('approved','paid')), 0)             AS tx_penalty
			FROM senderzz_affiliate_transactions
			WHERE order_id IS NOT NULL AND order_id > 0
			GROUP BY order_id
		) tx ON tx.order_id = o.id`
	}

	// JOIN do PRODUTOR — produtor_id casa com senderzz_portal_users.id (role='produtor').
	prodJoin := ""
	prodNameExpr := `''::text`
	if hasUsers {
		prodJoin = `LEFT JOIN senderzz_portal_users u_prod
		              ON u_prod.id = o.produtor_id AND u_prod.role = 'produtor'`
		prodNameExpr = `COALESCE(u_prod.nome,'')`
	}

	// JOIN do AFILIADO — affiliate_id casa SÓ com senderzz_portal_users.wp_user_id.
	affUserJoin := ""
	affNameExpr := `''::text`
	if hasUsers {
		affUserJoin = `LEFT JOIN senderzz_portal_users u_aff ON u_aff.wp_user_id = o.affiliate_id`
		affNameExpr = `COALESCE(u_aff.nome,'')`
	}

	// Subquery de meta por pedido (comissão %, penalidades, nomes de fallback).
	// pivota apenas as chaves necessárias para 1 linha por order_id (NÃO multiplica).
	metaJoin := `LEFT JOIN (SELECT NULL::bigint AS order_id, NULL::numeric AS m_pct,
	                               NULL::numeric AS m_pen_aff, NULL::numeric AS m_pen_prod,
	                               NULL::text AS m_aff_name, NULL::text AS m_prod_name) m ON FALSE`
	if hasMeta {
		metaJoin = `LEFT JOIN (
			SELECT order_id,
			       MAX(CASE WHEN meta_key='_sz_aff_commission_pct'      THEN NULLIF(meta_value,'')::numeric END) AS m_pct,
			       MAX(CASE WHEN meta_key='_sz_aff_frustration_penalty' THEN NULLIF(meta_value,'')::numeric END) AS m_pen_aff,
			       MAX(CASE WHEN meta_key='_sz_prod_frustration_penalty' THEN NULLIF(meta_value,'')::numeric END) AS m_pen_prod,
			       MAX(CASE WHEN meta_key='_sz_aff_name'                THEN meta_value END) AS m_aff_name,
			       MAX(CASE WHEN meta_key='_sz_aff_producer_name'       THEN meta_value END) AS m_prod_name
			FROM sz_order_meta
			WHERE meta_key IN ('_sz_aff_commission_pct','_sz_aff_frustration_penalty',
			                   '_sz_prod_frustration_penalty','_sz_aff_name','_sz_aff_producer_name')
			GROUP BY order_id
		) m ON m.order_id = o.id`
	}

	// Comissão %: AUDIT-FINANCEIRO-2026-06-25 — da VIEW canônica, sobre a BRUTA
	// (affiliate_bruta/total). Antes usava a líquida (affiliate_amount/total) → 57% errado.
	commPctExpr := `COALESCE(ROUND(COALESCE(f.affiliate_bruta,0)/NULLIF(o.total,0)*100), 0)`

	// Frustrado afiliado: penalidade da tx (autoritativa) → fallback meta. Só quando estornado.
	// Frustrado produtor: só meta (não há tx de penalidade de produtor); 0 quando ausente.
	rows, err := h.Pool.Query(ctx,
		`SELECT o.id,
		        COALESCE(o.created_at::text,'') AS data_pedido,
		        CASE
		          WHEN o.status IN (`+codLivroReceivedStatuses+`)   THEN 'Recebido'
		          WHEN o.status IN (`+codLivroFrustratedStatuses+`) THEN 'Estornado / não recebido'
		          ELSE 'Previsto'
		        END AS situacao,
		        COALESCE(o.status,'') AS wc_status,
		        COALESCE(o.produtor_id, 0)::bigint AS producer_id,
		        COALESCE(NULLIF(`+prodNameExpr+`,''), COALESCE(m.m_prod_name,'')) AS producer_name,
		        COALESCE(o.affiliate_id, 0)::bigint AS affiliate_id,
		        COALESCE(NULLIF(`+affNameExpr+`,''), COALESCE(m.m_aff_name,'')) AS affiliate_name,
		        `+commPctExpr+` AS commission_pct,
		        COALESCE(o.total,0) AS valor_pedido,
		        COALESCE(o.delivery_fee,0) + COALESCE(o.transaction_fee,0) AS taxas,
		        CASE WHEN o.status IN (`+codLivroFrustratedStatuses+`)
		             THEN COALESCE(m.m_pen_prod,0) ELSE 0 END AS frustrado_produtor,
		        CASE WHEN o.status IN (`+codLivroFrustratedStatuses+`)
		             THEN COALESCE(NULLIF(tx.tx_penalty,0), m.m_pen_aff, 0) ELSE 0 END AS frustrado_afiliado,
		        CASE WHEN o.status IN (`+codLivroFrustratedStatuses+`)
		             THEN 0 ELSE COALESCE(o.affiliate_amount,0) END AS afiliado_rs,
		        CASE WHEN o.status IN (`+codLivroReceivedStatuses+`)
		             THEN COALESCE(f.producer_net_live,0) ELSE 0 END AS produtor_rs,
		        CASE
		          WHEN o.status IN (`+codLivroFrustratedStatuses+`) THEN 'Não repassar'
		          WHEN COALESCE(tx.tx_available,FALSE)              THEN 'Disponível'
		          WHEN COALESCE(tx.tx_pending,FALSE)                THEN 'Pendente'
		          WHEN o.status IN (`+codLivroReceivedStatuses+`)   THEN 'Pendente'
		          ELSE 'Previsto'
		        END AS repasse
		 FROM sz_orders o
		 LEFT JOIN sz_order_financials f ON f.order_id = o.id
		 `+txJoin+`
		 `+prodJoin+`
		 `+affUserJoin+`
		 `+metaJoin+`
		 WHERE o.created_at::date BETWEEN $1::date AND $2::date
		 ORDER BY o.id DESC
		 LIMIT $3`,
		from, to, limit,
	)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	for rows.Next() {
		var o CodLivroOrder
		_ = rows.Scan(
			&o.OrderID, &o.DataPedido, &o.Situacao, &o.WcStatus,
			&o.ProducerID, &o.ProducerName, &o.AffiliateID, &o.AffiliateName,
			&o.CommissionPct, &o.ValorPedido, &o.Taxas,
			&o.FrustradoProdutor, &o.FrustradoAfiliado,
			&o.AfiliadoRS, &o.ProdutorRS, &o.Repasse,
		)
		out = append(out, o)
	}

	httpx.JSON(w, 200, map[string]any{"items": out, "count": len(out)})
}

// CodLivroAffiliateRow — resumo agrupado por afiliado.
type CodLivroAffiliateRow struct {
	AffiliateID    int64   `json:"affiliate_id"`
	AffiliateName  string  `json:"affiliate_name"`
	AffiliateEmail string  `json:"affiliate_email"`
	Pedidos        int64   `json:"pedidos"`
	Recebidos      int64   `json:"recebidos"`
	Previstos      int64   `json:"previstos"`
	Frustrados     int64   `json:"frustrados"`
	Pendente       float64 `json:"pendente"`
	Disponivel     float64 `json:"disponivel"`
	PrevistoValor  float64 `json:"previsto_valor"`
}

// AffiliatesSummary — agrupa as linhas do período por affiliate_id.
// GET /cod-livro/affiliates-summary?from=&to=
func (h *CodLivroHandler) AffiliatesSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	from, to := codLivroDateRange(r)

	out := []CodLivroAffiliateRow{}
	if !h.tableExists(ctx, "sz_orders") {
		httpx.JSON(w, 200, map[string]any{"items": out, "count": 0})
		return
	}

	hasTx := h.tableExists(ctx, "senderzz_affiliate_transactions")
	hasUsers := h.tableExists(ctx, "senderzz_portal_users")

	// Subquery tx por (order_id) — usada para somar pendente/disponível por afiliado nas linhas do período.
	txJoin := `LEFT JOIN (SELECT NULL::bigint AS order_id, 0::numeric AS tx_pendente, 0::numeric AS tx_disponivel)
	             tx ON FALSE`
	if hasTx {
		txJoin = `LEFT JOIN (
			SELECT order_id,
			       SUM(CASE WHEN type='commission' AND status='pending'   THEN amount ELSE 0 END) AS tx_pendente,
			       SUM(CASE WHEN type='commission' AND status IN ('approved','paid') THEN amount ELSE 0 END) AS tx_disponivel
			FROM senderzz_affiliate_transactions
			WHERE order_id IS NOT NULL AND order_id > 0
			GROUP BY order_id
		) tx ON tx.order_id = o.id`
	}

	// Afiliado casa SÓ por wp_user_id (id-space distinto — ver header do arquivo).
	userJoin := ""
	nameExprAff := `''::text`
	emailExprAff := `''::text`
	if hasUsers {
		userJoin = `LEFT JOIN senderzz_portal_users u ON u.wp_user_id = o.affiliate_id`
		nameExprAff = `MAX(COALESCE(u.nome,''))`
		emailExprAff = `MAX(COALESCE(u.email,''))`
	}

	rows, err := h.Pool.Query(ctx,
		`SELECT o.affiliate_id::bigint AS affiliate_id,
		        `+nameExprAff+` AS affiliate_name,
		        `+emailExprAff+` AS affiliate_email,
		        COUNT(*)::bigint AS pedidos,
		        SUM(CASE WHEN o.status IN (`+codLivroReceivedStatuses+`)                  THEN 1 ELSE 0 END)::bigint AS recebidos,
		        SUM(CASE WHEN o.status IN (`+codLivroFrustratedStatuses+`)                THEN 1 ELSE 0 END)::bigint AS frustrados,
		        SUM(CASE WHEN o.status NOT IN (`+codLivroReceivedStatuses+`)
		                  AND o.status NOT IN (`+codLivroFrustratedStatuses+`)            THEN 1 ELSE 0 END)::bigint AS previstos,
		        COALESCE(SUM(tx.tx_pendente),0)   AS pendente,
		        COALESCE(SUM(tx.tx_disponivel),0) AS disponivel,
		        COALESCE(SUM(CASE WHEN o.status NOT IN (`+codLivroReceivedStatuses+`)
		                           AND o.status NOT IN (`+codLivroFrustratedStatuses+`)
		                          THEN COALESCE(o.affiliate_amount,0) ELSE 0 END), 0) AS previsto_valor
		 FROM sz_orders o
		 `+txJoin+`
		 `+userJoin+`
		 WHERE o.created_at::date BETWEEN $1::date AND $2::date
		   AND COALESCE(o.affiliate_id, 0) > 0
		 GROUP BY o.affiliate_id
		 ORDER BY pedidos DESC, o.affiliate_id ASC`,
		from, to,
	)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	for rows.Next() {
		var a CodLivroAffiliateRow
		// recebidos / frustrados / previstos vêm na ordem da query (recebidos, frustrados, previstos).
		_ = rows.Scan(
			&a.AffiliateID, &a.AffiliateName, &a.AffiliateEmail, &a.Pedidos,
			&a.Recebidos, &a.Frustrados, &a.Previstos,
			&a.Pendente, &a.Disponivel, &a.PrevistoValor,
		)
		out = append(out, a)
	}

	httpx.JSON(w, 200, map[string]any{"items": out, "count": len(out)})
}

// CodLivroProducerRow — resumo agrupado por produtor.
type CodLivroProducerRow struct {
	ProducerID         int64   `json:"producer_id"`
	ProducerName       string  `json:"producer_name"`
	ProducerEmail      string  `json:"producer_email"`
	Pedidos            int64   `json:"pedidos"`
	Recebidos          int64   `json:"recebidos"`
	Frustrados         int64   `json:"frustrados"`
	Previstos          int64   `json:"previstos"`
	Bruto              float64 `json:"bruto"`
	BrutoPrevisto      float64 `json:"bruto_previsto"`
	TaxasSenderzz      float64 `json:"taxas_senderzz"`
	Afiliado           float64 `json:"afiliado"`
	LiquidoProdutor    float64 `json:"liquido_produtor"`
	FrustradoProdutor  float64 `json:"frustrado_produtor"`
	FrustradoAfiliados float64 `json:"frustrado_afiliados"`
	FrustradoValor     float64 `json:"frustrado_valor"`
}

// ProducersSummary — agrupa por produtor (produtor_id).
// GET /cod-livro/producers-summary?from=&to=
func (h *CodLivroHandler) ProducersSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	from, to := codLivroDateRange(r)

	out := []CodLivroProducerRow{}
	if !h.tableExists(ctx, "sz_orders") {
		httpx.JSON(w, 200, map[string]any{"items": out, "count": 0})
		return
	}

	hasTx := h.tableExists(ctx, "senderzz_affiliate_transactions")
	hasUsers := h.tableExists(ctx, "senderzz_portal_users")
	hasMeta := h.tableExists(ctx, "sz_order_meta")

	txJoin := `LEFT JOIN (SELECT NULL::bigint AS order_id, 0::numeric AS tx_penalty) tx ON FALSE`
	if hasTx {
		txJoin = `LEFT JOIN (
			SELECT order_id,
			       SUM(CASE WHEN type='penalty' THEN ABS(amount) ELSE 0 END) AS tx_penalty
			FROM senderzz_affiliate_transactions
			WHERE order_id IS NOT NULL AND order_id > 0
			GROUP BY order_id
		) tx ON tx.order_id = o.id`
	}

	// MED23: penalidade do PRODUTOR vem do meta _sz_prod_frustration_penalty (não há tx
	// de penalidade de produtor). Mesma fonte/sinal usados no handler Orders (positivo, sem ABS).
	metaJoin := `LEFT JOIN (SELECT NULL::bigint AS order_id, NULL::numeric AS m_pen_prod) m ON FALSE`
	if hasMeta {
		metaJoin = `LEFT JOIN (
			SELECT order_id,
			       MAX(CASE WHEN meta_key='_sz_prod_frustration_penalty' THEN NULLIF(meta_value,'')::numeric END) AS m_pen_prod
			FROM sz_order_meta
			WHERE meta_key = '_sz_prod_frustration_penalty'
			GROUP BY order_id
		) m ON m.order_id = o.id`
	}

	// Produtor casa por portal id (role='produtor') — id-space distinto do afiliado.
	userJoin := ""
	nameExpr := `''::text`
	emailExpr := `''::text`
	if hasUsers {
		userJoin = `LEFT JOIN senderzz_portal_users up ON up.id = o.produtor_id AND up.role = 'produtor'`
		nameExpr = `MAX(COALESCE(up.nome,''))`
		emailExpr = `MAX(COALESCE(up.email,''))`
	}

	// MED23: frustrado_produtor = penalidade do produtor (meta); frustrado_afiliados = penalidade
	// do afiliado (tx); frustrado_valor = soma das duas, por pedido estornado.
	rows, err := h.Pool.Query(ctx,
		`SELECT o.produtor_id::bigint AS producer_id,
		        `+nameExpr+` AS producer_name,
		        `+emailExpr+` AS producer_email,
		        COUNT(*)::bigint AS pedidos,
		        SUM(CASE WHEN o.status IN (`+codLivroReceivedStatuses+`)   THEN 1 ELSE 0 END)::bigint AS recebidos,
		        SUM(CASE WHEN o.status IN (`+codLivroFrustratedStatuses+`) THEN 1 ELSE 0 END)::bigint AS frustrados,
		        SUM(CASE WHEN o.status NOT IN (`+codLivroReceivedStatuses+`)
		                  AND o.status NOT IN (`+codLivroFrustratedStatuses+`)
		                 THEN 1 ELSE 0 END)::bigint AS previstos,
		        COALESCE(SUM(CASE WHEN o.status NOT IN (`+codLivroFrustratedStatuses+`)
		                          THEN COALESCE(o.total,0) ELSE 0 END), 0)             AS bruto,
		        COALESCE(SUM(CASE WHEN o.status NOT IN (`+codLivroReceivedStatuses+`)
		                           AND o.status NOT IN (`+codLivroFrustratedStatuses+`)
		                          THEN COALESCE(o.total,0) ELSE 0 END), 0)             AS bruto_previsto,
		        COALESCE(SUM(CASE WHEN o.status NOT IN (`+codLivroFrustratedStatuses+`)
		                          THEN COALESCE(f.delivery_fee,0)+COALESCE(f.producer_take,0) ELSE 0 END), 0)      AS taxas_senderzz,
		        COALESCE(SUM(CASE WHEN o.status NOT IN (`+codLivroFrustratedStatuses+`)
		                          THEN COALESCE(f.affiliate_bruta,0) ELSE 0 END), 0)  AS afiliado,
		        COALESCE(SUM(CASE WHEN o.status NOT IN (`+codLivroFrustratedStatuses+`)
		                          THEN COALESCE(f.producer_net_live,0) ELSE 0 END), 0)      AS liquido_produtor,
		        COALESCE(SUM(CASE WHEN o.status IN (`+codLivroFrustratedStatuses+`)
		                          THEN COALESCE(m.m_pen_prod,0) ELSE 0 END), 0)         AS frustrado_produtor,
		        COALESCE(SUM(CASE WHEN o.status IN (`+codLivroFrustratedStatuses+`)
		                          THEN COALESCE(tx.tx_penalty,0) ELSE 0 END), 0)        AS frustrado_afiliados,
		        COALESCE(SUM(CASE WHEN o.status IN (`+codLivroFrustratedStatuses+`)
		                          THEN COALESCE(tx.tx_penalty,0)+COALESCE(m.m_pen_prod,0) ELSE 0 END), 0) AS frustrado_valor
		 FROM sz_orders o
		 LEFT JOIN sz_order_financials f ON f.order_id = o.id
		 `+txJoin+`
		 `+metaJoin+`
		 `+userJoin+`
		 WHERE o.created_at::date BETWEEN $1::date AND $2::date
		   AND COALESCE(o.produtor_id, 0) > 0
		 GROUP BY o.produtor_id
		 ORDER BY bruto DESC, o.produtor_id ASC`,
		from, to,
	)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	for rows.Next() {
		var p CodLivroProducerRow
		_ = rows.Scan(
			&p.ProducerID, &p.ProducerName, &p.ProducerEmail, &p.Pedidos,
			&p.Recebidos, &p.Frustrados, &p.Previstos,
			&p.Bruto, &p.BrutoPrevisto, &p.TaxasSenderzz,
			&p.Afiliado, &p.LiquidoProdutor, &p.FrustradoProdutor,
			&p.FrustradoAfiliados, &p.FrustradoValor,
		)
		out = append(out, p)
	}

	httpx.JSON(w, 200, map[string]any{"items": out, "count": len(out)})
}
