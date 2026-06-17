// Package handlers — endpoint admin para o Livro COD (tela financeira única).
// Espelha src/Admin/Unified_Menu.php::tab_fin_taxas() (PHP legado) sobre Postgres.
// Cobre 5 KPIs, tabela split de 22 colunas e dois resumos colapsáveis (afiliado/produtor).
//
// Fontes de dados (graceful degradation via tableExists):
//   - sz_orders ........................ pedido base (status, gross, fees, splits, ids, datas)
//   - senderzz_affiliate_transactions ... agregações de comissão (pending/available/cancelled) e penalty
//   - senderzz_portal_users ............ nome/email do afiliado e do produtor (substitui wp_users no PG)
//
// Observações relevantes para a paridade com o PHP:
//   - "Recebido"   = sz_orders.status IN ('completo','entregue')
//   - "Estornado"  = sz_orders.status IN ('frustrado','cancelled','refunded')
//   - "Previsto"   = qualquer outro status não final
//   - bruto_valido / taxas / afiliado / liquido_produtor somam APENAS quando NÃO estornado
//     (replicando a CASE WHEN ... ELSE 0 do PHP).
//   - frustrado_produtor: o PHP busca em wp_tpc_transacoes (referencia 'sz_frustrado_<id>')
//     que ainda não foi migrado para o Postgres. Mantemos em 0 e marcamos com TODO até a tabela existir.
//   - bruto_estornado = frustrado_afiliado + frustrado_produtor (semantica do PHP).
//   - valor_nao_recebido = sz_orders.gross quando estornado (distinto de bruto_estornado).
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

// Constantes de status agrupados (espelha o ladder do PHP tab_fin_taxas).
// O PHP aceita também 'wc-*' e 'failed'/'frustracao' — no Postgres usamos apenas a forma canônica.
const (
	codLivroReceivedStatuses  = `'completo','entregue'`
	codLivroFrustratedStatuses = `'frustrado','cancelled','refunded'`
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
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// CodLivroSummary — KPIs do topo da tela.
// bruto_cod         = SUM(gross) WHERE status != estornado
// afiliados         = SUM(affiliate_amount) WHERE status != estornado
// taxas_senderzz    = SUM(senderzz_fee) WHERE status != estornado
// liquido_produtor  = SUM(producer_net) WHERE status != estornado
// previsto_produtor = SUM(producer_net) WHERE status NOT IN (recebido, estornado)
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
	_ = h.Pool.QueryRow(ctx,
		`SELECT
		   COALESCE(SUM(CASE WHEN status NOT IN (`+codLivroFrustratedStatuses+`) THEN COALESCE(gross,0)            ELSE 0 END), 0) AS bruto_cod,
		   COALESCE(SUM(CASE WHEN status NOT IN (`+codLivroFrustratedStatuses+`) THEN COALESCE(affiliate_amount,0) ELSE 0 END), 0) AS afiliados,
		   COALESCE(SUM(CASE WHEN status NOT IN (`+codLivroFrustratedStatuses+`) THEN COALESCE(senderzz_fee,0)     ELSE 0 END), 0) AS taxas_senderzz,
		   COALESCE(SUM(CASE WHEN status NOT IN (`+codLivroFrustratedStatuses+`) THEN COALESCE(producer_net,0)     ELSE 0 END), 0) AS liquido_produtor,
		   COALESCE(SUM(CASE WHEN status NOT IN (`+codLivroReceivedStatuses+`)
		                       AND status NOT IN (`+codLivroFrustratedStatuses+`)
		                  THEN COALESCE(producer_net,0) ELSE 0 END), 0) AS previsto_produtor
		 FROM sz_orders
		 WHERE created_at::date BETWEEN $1::date AND $2::date`,
		from, to,
	).Scan(&out.BrutoCOD, &out.Afiliados, &out.TaxasSenderzz, &out.LiquidoProdutor, &out.PrevistoProdutor)

	httpx.JSON(w, 200, out)
}

// CodLivroOrder — linha da tabela split (22 colunas + affiliate_name).
// Campos numéricos derivam dos splits do pedido respeitando o status (estornado zera receita,
// previsto não soma como recebido, etc.).
type CodLivroOrder struct {
	OrderID            int64   `json:"order_id"`
	DataPedido         string  `json:"data_pedido"`
	Situacao           string  `json:"situacao"` // Recebido | Estornado | Previsto
	AffiliateID        int64   `json:"affiliate_id"`
	AffiliateName      string  `json:"affiliate_name"`
	AffiliateEmail     string  `json:"affiliate_email"`
	ProducerID         int64   `json:"producer_id"`
	CommissionPct      float64 `json:"commission_pct"`
	ValorPedido        float64 `json:"valor_pedido"`
	BrutoValido        float64 `json:"bruto_valido"`
	TaxasSenderzz      float64 `json:"taxas_senderzz"`
	TaxaEntrega        float64 `json:"taxa_entrega"`
	TaxaTransacao      float64 `json:"taxa_transacao"`
	ValorAfiliado      float64 `json:"valor_afiliado"`
	LiquidoProdutor    float64 `json:"liquido_produtor"`
	ValorNaoRecebido   float64 `json:"valor_nao_recebido"`
	BrutoEstornado     float64 `json:"bruto_estornado"`
	FrustradoAfiliado  float64 `json:"frustrado_afiliado"`
	FrustradoProdutor  float64 `json:"frustrado_produtor"`
	RepassePendente    float64 `json:"repasse_pendente"`
	RepasseDisponivel  float64 `json:"repasse_disponivel"`
	RepasseEstornado   float64 `json:"repasse_estornado"`
	StatusRepasse      string  `json:"status_repasse"`
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
	hasAffiliates := h.tableExists(ctx, "senderzz_affiliates")

	// JOIN de transações de afiliado (pendente/disponível/estornado e penalidade por pedido).
	// Quando a tabela não existe, faz NULL via subquery vazia que sempre falha.
	txJoin := `LEFT JOIN (SELECT NULL::bigint AS order_id, 0::numeric AS tx_pendente, 0::numeric AS tx_disponivel,
	                              0::numeric AS tx_estornado, 0::numeric AS tx_penalty) tx ON FALSE`
	if hasTx {
		txJoin = `LEFT JOIN (
			SELECT order_id,
			       SUM(CASE WHEN type='commission' AND status='pending'   THEN amount ELSE 0 END) AS tx_pendente,
			       SUM(CASE WHEN type='commission' AND status='available' THEN amount ELSE 0 END) AS tx_disponivel,
			       SUM(CASE WHEN type='commission' AND status IN ('cancelled','canceled','refunded','reversed','void')
			                THEN ABS(amount) ELSE 0 END) AS tx_estornado,
			       SUM(CASE WHEN type='penalty' THEN ABS(amount) ELSE 0 END) AS tx_penalty
			FROM senderzz_affiliate_transactions
			WHERE order_id IS NOT NULL AND order_id > 0
			GROUP BY order_id
		) tx ON tx.order_id = o.id`
	}

	// JOIN de usuários (afiliado) — nome + email.
	userJoin := ""
	nameExpr := `''::text`
	emailExpr := `''::text`
	if hasUsers {
		userJoin = `LEFT JOIN senderzz_portal_users u_aff ON u_aff.id = o.affiliate_id`
		nameExpr = `COALESCE(u_aff.nome,'')`
		emailExpr = `COALESCE(u_aff.email,'')`
	}

	// JOIN para comissao_pct canônica (senderzz_affiliates.comissao_pct).
	// Usa subquery GROUP BY para evitar linhas duplicadas quando senderzz_affiliates tem múltiplos produtos
	// por par (afiliado_id, produtor_id). COALESCE: valor armazenado → derivado por aritmética.
	// Quando a tabela não existe, mantém cálculo derivado via (affiliate_amount/gross*100).
	affJoin := ""
	commPctExpr := `CASE WHEN COALESCE(o.total,0) > 0
		             THEN ROUND( (COALESCE(o.affiliate_amount,0) / o.total * 100)::numeric, 2)
		             ELSE 0
		        END`
	if hasAffiliates {
		affJoin = `LEFT JOIN (
			SELECT afiliado_id, produtor_id, MAX(comissao_pct) AS comissao_pct
			FROM senderzz_affiliates
			WHERE comissao_pct IS NOT NULL AND comissao_pct > 0
			GROUP BY afiliado_id, produtor_id
		) saff ON saff.afiliado_id = o.affiliate_id AND saff.produtor_id = o.produtor_id`
		commPctExpr = `CASE WHEN COALESCE(saff.comissao_pct, 0) > 0
		             THEN ROUND(saff.comissao_pct::numeric, 2)
		             WHEN COALESCE(o.total,0) > 0
		             THEN ROUND( (COALESCE(o.affiliate_amount,0) / o.total * 100)::numeric, 2)
		             ELSE 0
		        END`
	}

	// TODO(senderzz): frustrado_produtor depende da migração de wp_tpc_transacoes
	//                 (referencia LIKE 'sz_frustrado_%') para Postgres. Por ora mantemos 0.
	rows, err := h.Pool.Query(ctx,
		`SELECT o.id,
		        COALESCE(o.created_at::text,'') AS data_pedido,
		        CASE
		          WHEN o.status IN (`+codLivroReceivedStatuses+`)   THEN 'Recebido'
		          WHEN o.status IN (`+codLivroFrustratedStatuses+`) THEN 'Estornado'
		          ELSE 'Previsto'
		        END AS situacao,
		        COALESCE(o.affiliate_id, 0)::bigint AS affiliate_id,
		        `+nameExpr+` AS affiliate_name,
		        `+emailExpr+` AS affiliate_email,
		        COALESCE(o.produtor_id, 0)::bigint AS producer_id,
		        `+commPctExpr+` AS commission_pct,
		        COALESCE(o.total,0) AS valor_pedido,
		        CASE WHEN o.status IN (`+codLivroFrustratedStatuses+`) THEN 0 ELSE COALESCE(o.total,0)             END AS bruto_valido,
		        CASE WHEN o.status IN (`+codLivroFrustratedStatuses+`) THEN 0 ELSE COALESCE(o.senderzz_fee,0)      END AS taxas_senderzz,
		        CASE WHEN o.status IN (`+codLivroFrustratedStatuses+`) THEN 0 ELSE COALESCE(o.delivery_fee,0)      END AS taxa_entrega,
		        CASE WHEN o.status IN (`+codLivroFrustratedStatuses+`) THEN 0 ELSE COALESCE(o.transaction_fee,0)   END AS taxa_transacao,
		        CASE WHEN o.status IN (`+codLivroFrustratedStatuses+`) THEN 0 ELSE COALESCE(o.affiliate_amount,0)  END AS valor_afiliado,
		        CASE WHEN o.status IN (`+codLivroFrustratedStatuses+`) THEN 0 ELSE COALESCE(o.producer_net,0)      END AS liquido_produtor,
		        CASE WHEN o.status IN (`+codLivroFrustratedStatuses+`) THEN COALESCE(o.total,0) ELSE 0             END AS valor_nao_recebido,
		        CASE WHEN o.status IN (`+codLivroFrustratedStatuses+`)
		             THEN COALESCE(tx.tx_penalty,0) + 0 /* frustrado_produtor sem fonte ainda */
		             ELSE 0
		        END AS bruto_estornado,
		        CASE WHEN o.status IN (`+codLivroFrustratedStatuses+`) THEN COALESCE(tx.tx_penalty,0) ELSE 0 END AS frustrado_afiliado,
		        0::numeric AS frustrado_produtor,
		        COALESCE(tx.tx_pendente,0)    AS repasse_pendente,
		        COALESCE(tx.tx_disponivel,0)  AS repasse_disponivel,
		        COALESCE(tx.tx_estornado,0)   AS repasse_estornado,
		        CASE
		          WHEN o.status IN (`+codLivroFrustratedStatuses+`)                                   THEN 'Não repassar'
		          WHEN COALESCE(tx.tx_disponivel,0) > 0                                                THEN 'Disponível'
		          WHEN COALESCE(tx.tx_pendente,0)   > 0                                                THEN 'Pendente'
		          WHEN COALESCE(o.affiliate_id,0) > 0 AND COALESCE(o.affiliate_amount,0) > 0           THEN 'Previsto'
		          ELSE 'Sem afiliado'
		        END AS status_repasse
		 FROM sz_orders o
		 `+txJoin+`
		 `+userJoin+`
		 `+affJoin+`
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
			&o.OrderID, &o.DataPedido, &o.Situacao,
			&o.AffiliateID, &o.AffiliateName, &o.AffiliateEmail, &o.ProducerID,
			&o.CommissionPct, &o.ValorPedido, &o.BrutoValido,
			&o.TaxasSenderzz, &o.TaxaEntrega, &o.TaxaTransacao,
			&o.ValorAfiliado, &o.LiquidoProdutor, &o.ValorNaoRecebido,
			&o.BrutoEstornado, &o.FrustradoAfiliado, &o.FrustradoProdutor,
			&o.RepassePendente, &o.RepasseDisponivel, &o.RepasseEstornado,
			&o.StatusRepasse,
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
			       SUM(CASE WHEN type='commission' AND status='available' THEN amount ELSE 0 END) AS tx_disponivel
			FROM senderzz_affiliate_transactions
			WHERE order_id IS NOT NULL AND order_id > 0
			GROUP BY order_id
		) tx ON tx.order_id = o.id`
	}

	userJoin := ""
	nameExprAff := `''::text`
	emailExprAff := `''::text`
	if hasUsers {
		userJoin = `LEFT JOIN senderzz_portal_users u ON u.id = o.affiliate_id`
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

	userJoin := ""
	emailExpr := `''::text`
	if hasUsers {
		userJoin = `LEFT JOIN senderzz_portal_users up ON up.id = o.produtor_id`
		emailExpr = `MAX(COALESCE(up.email,''))`
	}

	// frustrado_valor = soma das penalidades (afiliado + produtor). frustrado_produtor segue 0 (TODO).
	rows, err := h.Pool.Query(ctx,
		`SELECT o.produtor_id::bigint AS producer_id,
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
		                          THEN COALESCE(o.senderzz_fee,0) ELSE 0 END), 0)      AS taxas_senderzz,
		        COALESCE(SUM(CASE WHEN o.status NOT IN (`+codLivroFrustratedStatuses+`)
		                          THEN COALESCE(o.affiliate_amount,0) ELSE 0 END), 0)  AS afiliado,
		        COALESCE(SUM(CASE WHEN o.status NOT IN (`+codLivroFrustratedStatuses+`)
		                          THEN COALESCE(o.producer_net,0) ELSE 0 END), 0)      AS liquido_produtor,
		        0::numeric                                                              AS frustrado_produtor,
		        COALESCE(SUM(CASE WHEN o.status IN (`+codLivroFrustratedStatuses+`)
		                          THEN COALESCE(tx.tx_penalty,0) ELSE 0 END), 0)        AS frustrado_afiliados,
		        COALESCE(SUM(CASE WHEN o.status IN (`+codLivroFrustratedStatuses+`)
		                          THEN COALESCE(tx.tx_penalty,0) ELSE 0 END), 0)        AS frustrado_valor
		 FROM sz_orders o
		 `+txJoin+`
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
			&p.ProducerID, &p.ProducerEmail, &p.Pedidos,
			&p.Recebidos, &p.Frustrados, &p.Previstos,
			&p.Bruto, &p.BrutoPrevisto, &p.TaxasSenderzz,
			&p.Afiliado, &p.LiquidoProdutor, &p.FrustradoProdutor,
			&p.FrustradoAfiliados, &p.FrustradoValor,
		)
		out = append(out, p)
	}

	httpx.JSON(w, 200, map[string]any{"items": out, "count": len(out)})
}
