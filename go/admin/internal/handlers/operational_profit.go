package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

// OperationalProfitHandler consolida apenas o canal COD. A expedição usa o
// relatório canônico do labels-service, que conhece o custo real do Melhor Envio.
type OperationalProfitHandler struct{ Pool *pgxpool.Pool }

type operationalProfitSummary struct {
	DateFrom     string                     `json:"date_from"`
	DateTo       string                     `json:"date_to"`
	Revenue      float64                    `json:"revenue"`
	Costs        float64                    `json:"costs"`
	Profit       float64                    `json:"profit"`
	MarginPct    float64                    `json:"margin_pct"`
	Orders       int64                      `json:"orders"`
	Transactions int64                      `json:"transactions"`
	OtherFees    float64                    `json:"other_fees"`
	Items        []codOperationalProfitItem `json:"items"`
}

type codOperationalProfitItem struct {
	OrderID       int64   `json:"order_id"`
	OrderNumber   string  `json:"order_number"`
	Date          string  `json:"date"`
	Status        string  `json:"status"`
	DeliveryFee   float64 `json:"delivery_fee"`
	AffiliateFee  float64 `json:"affiliate_fee"`
	ProducerFee   float64 `json:"producer_fee"`
	FrustratedFee float64 `json:"frustrated_fee"`
	OtherRevenue  float64 `json:"other_revenue"`
	Revenue       float64 `json:"revenue"`
	MotoboyCost   float64 `json:"motoboy_cost"`
	Profit        float64 `json:"profit"`
}

func operationalProfitDates(r *http.Request) (string, string, bool) {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	from := strings.TrimSpace(r.URL.Query().Get("date_from"))
	to := strings.TrimSpace(r.URL.Query().Get("date_to"))
	if from == "" {
		from = now.Format("2006-01") + "-01"
	}
	if to == "" {
		to = now.Format("2006-01-02")
	}
	fromDate, fromErr := time.Parse("2006-01-02", from)
	toDate, toErr := time.Parse("2006-01-02", to)
	return from, to, fromErr == nil && toErr == nil && !fromDate.After(toDate)
}

// CODSummary — GET /operational-profit/cod?date_from=&date_to=
//
// Receita COD:
//   - lançamentos do senderzz_revenue ligados a um pedido motoboy;
//   - taxas financeiras sem pedido (saque e antecipação), próprias do COD.
//
// Custo COD: repasse incorrido nos pedidos finalizados, pela mesma regra da
// carteira do motoboy. Ele é derivado dos pedidos porque sz_motoboy_ganhos é um
// espelho que depende de sync manual e pode ficar atrasado.
func (h *OperationalProfitHandler) CODSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	from, to, valid := operationalProfitDates(r)
	if !valid {
		httpx.Err(w, http.StatusBadRequest, "invalid_period", "periodo invalido")
		return
	}

	out := operationalProfitSummary{DateFrom: from, DateTo: to, Items: []codOperationalProfitItem{}}
	if !tableExistsCached(ctx, h.Pool, "senderzz_revenue") ||
		!tableExistsCached(ctx, h.Pool, "sz_orders") ||
		!tableExistsCached(ctx, h.Pool, "sz_motoboy_pedidos") {
		httpx.JSON(w, http.StatusOK, out)
		return
	}

	err := h.Pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(rv.amount), 0)::float8,
		       COUNT(*)::bigint,
		       COUNT(DISTINCT rv.order_id)::bigint
		  FROM senderzz_revenue rv
		 WHERE rv.created_at >= ($1::date)::timestamp AT TIME ZONE 'America/Sao_Paulo'
		   AND rv.created_at < (($2::date + 1)::timestamp AT TIME ZONE 'America/Sao_Paulo')
		   AND (
		        (rv.order_id IS NOT NULL AND EXISTS (
		           SELECT 1
		             FROM sz_orders o
		             JOIN sz_motoboy_pedidos mp
		               ON mp.wc_order_id = COALESCE(o.wp_order_id, o.id)
		            WHERE o.id = rv.order_id
		        ))
		        OR (rv.order_id IS NULL AND rv.component IN ('taxa_saque', 'taxa_antecipacao'))
		   )`, from, to).Scan(&out.Revenue, &out.Transactions, &out.Orders)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "db_error", "erro ao calcular receita COD")
		return
	}

	if tableExistsCached(ctx, h.Pool, "senderzz_options") {
		err = h.Pool.QueryRow(ctx, `
			SELECT COALESCE(SUM(
			         CASE mp.status
			           WHEN 'entregue' THEN COALESCE(
			             (SELECT NULLIF(o.value, '')::numeric
			                FROM senderzz_options o
			               WHERE o.name = 'sz_mbw_taxa_entrega_mb_' ||
			                 COALESCE(NULLIF(mp.baixa_motoboy_id, 0), mp.motoboy_id)::text),
			             (SELECT NULLIF(o.value, '')::numeric
			                FROM senderzz_options o
			               WHERE o.name = 'sz_mbw_taxa_entrega'),
			             18
			           )
			           WHEN 'frustrado' THEN COALESCE(
			             (SELECT NULLIF(o.value, '')::numeric
			                FROM senderzz_options o
			               WHERE o.name = 'sz_mbw_taxa_frustrado_mb_' ||
			                 COALESCE(NULLIF(mp.baixa_motoboy_id, 0), mp.motoboy_id)::text),
			             (SELECT NULLIF(o.value, '')::numeric
			                FROM senderzz_options o
			               WHERE o.name = 'sz_mbw_taxa_frustrado'),
			             5
			           )
			           ELSE 0
			         END
			       ), 0)::float8
			  FROM sz_motoboy_pedidos mp
			 WHERE mp.status IN ('entregue', 'frustrado')
			   AND COALESCE(NULLIF(mp.baixa_motoboy_id, 0), mp.motoboy_id) IS NOT NULL
			   AND COALESCE(mp.baixa_at, mp.ts_entregue, mp.ts_frustrado,
			                mp.updated_at, mp.created_at) >= $1::date
			   AND COALESCE(mp.baixa_at, mp.ts_entregue, mp.ts_frustrado,
			                mp.updated_at, mp.created_at) < $2::date + interval '1 day'`, from, to).
			Scan(&out.Costs)
		if err != nil {
			httpx.Err(w, http.StatusInternalServerError, "db_error", "erro ao calcular custos COD")
			return
		}
	}

	// Detalhamento por pedido: união dos pedidos que tiveram receita OU custo no
	// período. Assim uma entrega sem lançamento de receita aparece como prejuízo,
	// em vez de sumir do relatório.
	rows, err := h.Pool.Query(ctx, `
		WITH revenue_by_order AS (
		  SELECT rv.order_id,
		         MIN((rv.created_at AT TIME ZONE 'America/Sao_Paulo')::date) AS occurred_on,
		         COALESCE(SUM(rv.amount) FILTER (WHERE rv.component='taxa_entrega'), 0) AS delivery_fee,
		         COALESCE(SUM(rv.amount) FILTER (WHERE rv.component='taxa_afiliado_4_99'), 0) AS affiliate_fee,
		         COALESCE(SUM(rv.amount) FILTER (WHERE rv.component='taxa_transacao_produtor'), 0) AS producer_fee,
		         COALESCE(SUM(rv.amount) FILTER (WHERE rv.component IN ('taxa_frustrado','taxa_frustrado_produtor')), 0) AS frustrated_fee,
		         COALESCE(SUM(rv.amount) FILTER (WHERE rv.component NOT IN (
		           'taxa_entrega','taxa_afiliado_4_99','taxa_transacao_produtor',
		           'taxa_frustrado','taxa_frustrado_produtor'
		         )), 0) AS other_revenue,
		         COALESCE(SUM(rv.amount), 0) AS revenue
		    FROM senderzz_revenue rv
		   WHERE rv.order_id IS NOT NULL
		     AND rv.created_at >= ($1::date)::timestamp AT TIME ZONE 'America/Sao_Paulo'
		     AND rv.created_at < (($2::date + 1)::timestamp AT TIME ZONE 'America/Sao_Paulo')
		     AND EXISTS (
		       SELECT 1 FROM sz_orders so
		       JOIN sz_motoboy_pedidos smp ON smp.wc_order_id=COALESCE(so.wp_order_id,so.id)
		       WHERE so.id=rv.order_id
		     )
		   GROUP BY rv.order_id
		),
		cost_by_order AS (
		  SELECT so.id AS order_id,
		         MIN(COALESCE(mp.baixa_at, mp.ts_entregue, mp.ts_frustrado,
		                      mp.updated_at, mp.created_at)::date) AS occurred_on,
		         COALESCE(SUM(CASE mp.status
		           WHEN 'entregue' THEN COALESCE(
		             (SELECT NULLIF(opt.value,'')::numeric FROM senderzz_options opt
		               WHERE opt.name='sz_mbw_taxa_entrega_mb_' ||
		                 COALESCE(NULLIF(mp.baixa_motoboy_id,0),mp.motoboy_id)::text),
		             (SELECT NULLIF(opt.value,'')::numeric FROM senderzz_options opt
		               WHERE opt.name='sz_mbw_taxa_entrega'),
		             18
		           )
		           WHEN 'frustrado' THEN COALESCE(
		             (SELECT NULLIF(opt.value,'')::numeric FROM senderzz_options opt
		               WHERE opt.name='sz_mbw_taxa_frustrado_mb_' ||
		                 COALESCE(NULLIF(mp.baixa_motoboy_id,0),mp.motoboy_id)::text),
		             (SELECT NULLIF(opt.value,'')::numeric FROM senderzz_options opt
		               WHERE opt.name='sz_mbw_taxa_frustrado'),
		             5
		           )
		           ELSE 0
		         END),0) AS motoboy_cost
		    FROM sz_motoboy_pedidos mp
		    JOIN sz_orders so ON COALESCE(so.wp_order_id,so.id)=mp.wc_order_id
		   WHERE mp.status IN ('entregue','frustrado')
		     AND COALESCE(NULLIF(mp.baixa_motoboy_id,0),mp.motoboy_id) IS NOT NULL
		     AND COALESCE(mp.baixa_at,mp.ts_entregue,mp.ts_frustrado,
		                  mp.updated_at,mp.created_at) >= $1::date
		     AND COALESCE(mp.baixa_at,mp.ts_entregue,mp.ts_frustrado,
		                  mp.updated_at,mp.created_at) < $2::date + interval '1 day'
		   GROUP BY so.id
		),
		order_keys AS (
		  SELECT order_id FROM revenue_by_order
		  UNION
		  SELECT order_id FROM cost_by_order
		)
		SELECT k.order_id,
		       COALESCE(NULLIF(so.order_number,''),COALESCE(so.wp_order_id,so.id)::text),
		       COALESCE(r.occurred_on,c.occurred_on,(so.created_at AT TIME ZONE 'America/Sao_Paulo')::date)::text,
		       COALESCE((SELECT mp.status FROM sz_motoboy_pedidos mp
		                  WHERE mp.wc_order_id=COALESCE(so.wp_order_id,so.id)
		                  ORDER BY mp.id DESC LIMIT 1),so.status,''),
		       COALESCE(r.delivery_fee,0)::float8,
		       COALESCE(r.affiliate_fee,0)::float8,
		       COALESCE(r.producer_fee,0)::float8,
		       COALESCE(r.frustrated_fee,0)::float8,
		       COALESCE(r.other_revenue,0)::float8,
		       COALESCE(r.revenue,0)::float8,
		       COALESCE(c.motoboy_cost,0)::float8,
		       (COALESCE(r.revenue,0)-COALESCE(c.motoboy_cost,0))::float8
		  FROM order_keys k
		  JOIN sz_orders so ON so.id=k.order_id
		  LEFT JOIN revenue_by_order r ON r.order_id=k.order_id
		  LEFT JOIN cost_by_order c ON c.order_id=k.order_id
		 ORDER BY COALESCE(r.occurred_on,c.occurred_on) DESC,k.order_id DESC`, from, to)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "db_error", "erro ao detalhar lucro COD")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var item codOperationalProfitItem
		if err := rows.Scan(&item.OrderID, &item.OrderNumber, &item.Date, &item.Status,
			&item.DeliveryFee, &item.AffiliateFee, &item.ProducerFee,
			&item.FrustratedFee, &item.OtherRevenue, &item.Revenue,
			&item.MotoboyCost, &item.Profit); err != nil {
			httpx.Err(w, http.StatusInternalServerError, "db_error", "erro ao ler detalhe COD")
			return
		}
		out.Items = append(out.Items, item)
	}
	if err := rows.Err(); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "db_error", "erro ao detalhar lucro COD")
		return
	}
	out.Orders = int64(len(out.Items))
	_ = h.Pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount),0)::float8 FROM senderzz_revenue
		 WHERE order_id IS NULL
		   AND component IN ('taxa_saque','taxa_antecipacao')
		   AND created_at >= ($1::date)::timestamp AT TIME ZONE 'America/Sao_Paulo'
		   AND created_at < (($2::date + 1)::timestamp AT TIME ZONE 'America/Sao_Paulo')`, from, to).
		Scan(&out.OtherFees)

	out.Profit = out.Revenue - out.Costs
	if out.Revenue != 0 {
		out.MarginPct = out.Profit / out.Revenue * 100
	}
	httpx.JSON(w, http.StatusOK, out)
}
