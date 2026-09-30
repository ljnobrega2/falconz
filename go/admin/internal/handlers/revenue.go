// Handler de FATURAMENTO FALKZ — lê o livro-razão senderzz_revenue.
//
// GET /revenue/summary → { total, meta_ano, pendente, pendente_pedidos, by_component:[{component,lancamentos,receita}], by_month:[{mes,receita}] }
// GET /revenue?from=&to=&produtor= → { items:[ lançamentos ] }
//
// senderzz_revenue (ver infra/postgres/schema-revenue.sql) booka o take da
// plataforma: taxa_afiliado_4_99 + taxa_transacao_produtor (auditoria 2026-06-18).
package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

type RevenueHandler struct{ Pool *pgxpool.Pool }

// Summary — GET /revenue/summary?from=&to=&produtor=
//
// MED43 — total, by_component e by_month respeitam os mesmos filtros (período +
// produtor) da lista de lançamentos. O `pendente` (previsão de pedidos em
// andamento) permanece global — é uma projeção, não receita realizada filtrável.
func (h *RevenueHandler) Summary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	q := r.URL.Query()
	from := strings.TrimSpace(q.Get("from"))
	to := strings.TrimSpace(q.Get("to"))
	produtor := strings.TrimSpace(q.Get("produtor"))

	// PREVISÃO de receita: pedidos em andamento (não realizados). AUDIT-2026-07-11
	// — usava sz_orders.status (vocabulário WooCommerce, nunca atualizado após a
	// migração pro fluxo motoboy — órfão) em vez de sz_motoboy_pedidos.status
	// (fonte real do estado do pedido). Achado: 5 de 7 pedidos "em andamento"
	// já estavam CANCELADO no status real, inflando a previsão. Fonte correta =
	// mesmo set "reservante" usado no trigger de estoque (agendado/aprovado/
	// embalado/em_rota/a_caminho/reagendado — 493-stock-kit-multiplier.sql).
	//
	// AUDIT-2026-07-14 — previsão só somava transaction_fee (afiliado) +
	// delivery_fee (entrega), OMITINDO taxa_transacao_produtor (total × pct do
	// produtor) — componente que todo pedido completo/entregue sempre gera
	// (ver trigger sz_revenue_capture_order, infra/postgres/503). Previsão
	// subestimava. Agora soma os 3 componentes reais, mesmo pct configurável
	// usado no trigger (sz_producer_transaction_fee_pct, default 4.99).
	var pendente float64
	var pendentePedidos int64
	if tableExistsCached(ctx, h.Pool, "sz_orders") && tableExistsCached(ctx, h.Pool, "sz_motoboy_pedidos") {
		_ = h.Pool.QueryRow(ctx,
			`SELECT COALESCE(SUM(
			           COALESCE(o.transaction_fee,0)
			         + COALESCE(o.delivery_fee,0)
			         + ROUND((COALESCE(o.total,0) * COALESCE((SELECT NULLIF(value,'')::numeric FROM senderzz_options WHERE name='sz_producer_transaction_fee_pct'), 4.99) / 100)::numeric, 2)
			       ),0)::float8, COUNT(*)
			   FROM sz_orders o
			   JOIN sz_motoboy_pedidos mp ON COALESCE(o.wp_order_id, o.id) = mp.wc_order_id
			  WHERE mp.status IN ('agendado','aprovado','embalado','em_rota','a_caminho','reagendado')`).
			Scan(&pendente, &pendentePedidos)
	}

	if !tableExistsCached(ctx, h.Pool, "senderzz_revenue") {
		httpx.JSON(w, 200, map[string]any{"total": 0, "meta_ano": float64(1000000), "pendente": pendente, "pendente_pedidos": pendentePedidos, "by_component": []any{}, "by_month": []any{}})
		return
	}

	// Predicado WHERE compartilhado (período + produtor) — mesma semântica da List.
	const revFilter = `
		  WHERE ($1='' OR rv.created_at >= ($1::date)::timestamp AT TIME ZONE 'America/Sao_Paulo')
		    AND ($2='' OR rv.created_at <= (($2::date + 1)::timestamp AT TIME ZONE 'America/Sao_Paulo' - interval '1 second'))
		    AND ($3='' OR EXISTS (
		          SELECT 1 FROM senderzz_portal_users p
		           WHERE (p.id = rv.produtor_id OR p.wp_user_id = rv.affiliate_id)
		             AND p.nome ILIKE '%'||$3||'%'))`

	var total float64
	_ = h.Pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(rv.amount),0)::float8 FROM senderzz_revenue rv`+revFilter,
		from, to, produtor).Scan(&total)

	type compRow struct {
		Component   string  `json:"component"`
		Lancamentos int64   `json:"lancamentos"`
		Receita     float64 `json:"receita"`
	}
	byComp := []compRow{}
	if rows, err := h.Pool.Query(ctx,
		`SELECT rv.component, COUNT(*), COALESCE(SUM(rv.amount),0)::float8
		   FROM senderzz_revenue rv`+revFilter+`
		  GROUP BY rv.component ORDER BY 3 DESC`, from, to, produtor); err == nil {
		defer rows.Close()
		for rows.Next() {
			var c compRow
			_ = rows.Scan(&c.Component, &c.Lancamentos, &c.Receita)
			byComp = append(byComp, c)
		}
	}

	type monthRow struct {
		Mes     string  `json:"mes"`
		Receita float64 `json:"receita"`
	}
	byMonth := []monthRow{}
	if rows, err := h.Pool.Query(ctx,
		`SELECT to_char(rv.created_at,'YYYY-MM') mes, COALESCE(SUM(rv.amount),0)::float8
		   FROM senderzz_revenue rv`+revFilter+`
		  GROUP BY 1 ORDER BY 1`, from, to, produtor); err == nil {
		defer rows.Close()
		for rows.Next() {
			var m monthRow
			_ = rows.Scan(&m.Mes, &m.Receita)
			byMonth = append(byMonth, m)
		}
	}

	httpx.JSON(w, 200, map[string]any{
		"total":            total,
		"meta_ano":         float64(1000000),
		"pendente":         pendente,
		"pendente_pedidos": pendentePedidos,
		"by_component":     byComp,
		"by_month":         byMonth,
	})
}

// List — GET /revenue?from=&to=&produtor=&limit=
func (h *RevenueHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !tableExistsCached(ctx, h.Pool, "senderzz_revenue") {
		httpx.JSON(w, 200, map[string]any{"items": []any{}})
		return
	}
	q := r.URL.Query()
	from := strings.TrimSpace(q.Get("from"))
	to := strings.TrimSpace(q.Get("to"))
	produtor := strings.TrimSpace(q.Get("produtor"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 1000 {
		limit = 300
	}

	type lanc struct {
		ID         int64   `json:"id"`
		OrderID    *int64  `json:"order_id"`
		Component  string  `json:"component"`
		Produtor   string  `json:"produtor"` // nome do produtor OU afiliado (parte da receita)
		BaseAmount float64 `json:"base_amount"`
		Receita    float64 `json:"receita"`
		Ref        string  `json:"ref"`
		CreatedAt  string  `json:"created_at"`
	}
	out := []lanc{}
	// MED41 — Resolve o nome da parte com fallback tolerante a papel:
	//   1) produtor_id = portal id (qualquer role; não exige role='produtor')
	//   2) affiliate_id = wp_user_id (qualquer role; não exige role='afiliado')
	// Sem o casamento estrito por papel, a coluna Nome não fica vazia quando o
	// produtor_id existe mas o role está divergente (ex.: usuário reclassificado).
	// MED42 — O filtro de Produtor casa produtor_id E affiliate_id, para não
	// ignorar lançamentos de afiliados (taxa_afiliado_4_99, taxa_frustrado).
	rows, err := h.Pool.Query(ctx,
		`SELECT rv.id, rv.order_id, rv.component,
		        COALESCE(
		          (SELECT p.nome FROM senderzz_portal_users p WHERE p.id = rv.produtor_id AND p.nome <> '' LIMIT 1),
		          (SELECT p.nome FROM senderzz_portal_users p WHERE p.wp_user_id = rv.affiliate_id AND p.nome <> '' LIMIT 1),
		          ''
		        ) AS produtor,
		        rv.base_amount::float8, rv.amount::float8, rv.ref, rv.created_at::text
		   FROM senderzz_revenue rv
		  WHERE ($1='' OR rv.created_at >= ($1::date)::timestamp AT TIME ZONE 'America/Sao_Paulo')
		    AND ($2='' OR rv.created_at <= (($2::date + 1)::timestamp AT TIME ZONE 'America/Sao_Paulo' - interval '1 second'))
		    AND ($3='' OR EXISTS (
		          SELECT 1 FROM senderzz_portal_users p
		           WHERE (p.id = rv.produtor_id OR p.wp_user_id = rv.affiliate_id)
		             AND p.nome ILIKE '%'||$3||'%'))
		  ORDER BY rv.created_at DESC, rv.id DESC
		  LIMIT $4`, from, to, produtor, limit)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var l lanc
		_ = rows.Scan(&l.ID, &l.OrderID, &l.Component, &l.Produtor, &l.BaseAmount, &l.Receita, &l.Ref, &l.CreatedAt)
		out = append(out, l)
	}
	httpx.JSON(w, 200, map[string]any{"items": out})
}
