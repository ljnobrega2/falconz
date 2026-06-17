// Package handlers — endpoint admin para o Dashboard operacional.
// Espelha tab_overview_operacao() em src/Admin/Unified_Menu.php.
//
// Endpoints:
//
//	GET /dashboard         → KPIs operacionais (6 cards)
//	GET /dashboard/alerts  → 6 linhas de alertas operacionais
//	GET /dashboard/stopped-orders → pedidos parados 24h+
package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

type DashboardHandler struct{ Pool *pgxpool.Pool }

// kpis — 6 contadores da visão operacional. Espelha tab_overview_operacao() do PHP.
type kpis struct {
	PedidosHoje    int64 `json:"pedidos_hoje"`    // wc-processing|wc-agendado|completed|frustrado na data de hoje
	Agendados      int64 `json:"agendados"`        // COUNT status=agendado (sem filtro de data)
	EmRota         int64 `json:"em_rota"`          // COUNT status IN (em_rota, em-rota)
	EntreguesHoje  int64 `json:"entregues_hoje"`   // COUNT status IN (completed, entregue) WHERE DATE=hoje
	FrustradosHoje int64 `json:"frustrados_hoje"`  // COUNT status=frustrado WHERE DATE=hoje
	AlertasTotal   int64 `json:"alertas_total"`    // SUM audit_counts + webhook_fails(7d) + pedidos_parados
}

// alertas — 6 linhas de alertas operacionais. Espelha alert_line() + get_audit_counts() + webhook_failure_count().
type alertas struct {
	SaldoDivergente     int64 `json:"saldo_divergente"`      // wallet
	AffSemTransacao     int64 `json:"aff_sem_transacao"`     // aff_missing
	WalletDivergente    int64 `json:"wallet_divergente"`     // aff_bad
	SplitDivergente     int64 `json:"split_divergente"`      // split
	WebhooksFalhando7d  int64 `json:"webhooks_falhando_7d"`  // webhook_failure_count (7 dias, 4 tabelas)
	PedidosParados24h   int64 `json:"pedidos_parados_24h"`   // stopped_order_rows (24h)
}

// stoppedOrder — linha de pedido parado.
type stoppedOrder struct {
	PedidoID int64  `json:"pedido_id"`
	Status   string `json:"status"`
	Email    string `json:"email"`
}

// tableExistsDash verifica existência da tabela no schema public.
func (h *DashboardHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// ordensMirror detecta qual tabela espelha as WC orders.
// Prioridade: sz_orders (sincronizado pelo plugin), depois sem espelho.
func (h *DashboardHandler) orderTable(ctx context.Context) string {
	if h.tableExists(ctx, "sz_orders") {
		return "sz_orders"
	}
	return ""
}

// countByStatus conta pedidos WC por slice de status.
// Statuses em sz_orders ficam sem o prefixo "wc-" (e.g., "agendado" não "wc-agendado").
// A função aceita ambos e normaliza.
func (h *DashboardHandler) countByStatus(ctx context.Context, tbl string, statuses []string, date string) int64 {
	if tbl == "" || len(statuses) == 0 {
		return 0
	}
	// Normaliza: remove prefixo "wc-" pois sz_orders armazena sem ele
	normalized := make([]string, 0, len(statuses)*2)
	for _, s := range statuses {
		plain := s
		if len(s) > 3 && s[:3] == "wc-" {
			plain = s[3:]
		}
		normalized = append(normalized, plain)
	}

	args := []any{}
	in := ""
	for i, s := range normalized {
		if i > 0 {
			in += ","
		}
		args = append(args, s)
		in += "$" + itoa(i+1)
	}

	dateClause := ""
	if date != "" {
		args = append(args, date)
		dateClause = " AND date_created_gmt::date = $" + itoa(len(args))
	}

	var n int64
	_ = h.Pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM "+tbl+" WHERE status IN ("+in+")"+dateClause,
		args...).Scan(&n)
	return n
}

// itoa converte int para string (pequeno helper inline).
func itoa(n int) string {
	b := []byte{}
	if n == 0 {
		return "0"
	}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// todayBRT retorna a data de hoje em America/Sao_Paulo (YYYY-MM-DD).
func todayBRT() string {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		loc = time.UTC
	}
	return time.Now().In(loc).Format("2006-01-02")
}

// webhookFailCount conta falhas de webhook nos últimos 7 dias nas 4 tabelas possíveis.
// Espelha webhook_failure_count() PHP.
func (h *DashboardHandler) webhookFailCount(ctx context.Context) int64 {
	tables := []string{
		"senderzz_producer_webhook_logs",
		"senderzz_webhook_logs",
		"sz_webhook_logs",
		"wcme_webhook_logs",
	}
	for _, tbl := range tables {
		if !h.tableExists(ctx, tbl) {
			continue
		}
		// Detecta qual coluna de código HTTP existe
		codeCol := ""
		for _, col := range []string{"response_code", "http_code"} {
			var exists bool
			_ = h.Pool.QueryRow(ctx,
				`SELECT EXISTS (
					SELECT FROM information_schema.columns
					WHERE table_schema='public' AND table_name=$1 AND column_name=$2
				)`, tbl, col).Scan(&exists)
			if exists {
				codeCol = col
				break
			}
		}
		if codeCol == "" {
			continue
		}
		// Detecta coluna de data
		dateCol := ""
		for _, col := range []string{"fired_at", "created_at"} {
			var exists bool
			_ = h.Pool.QueryRow(ctx,
				`SELECT EXISTS (
					SELECT FROM information_schema.columns
					WHERE table_schema='public' AND table_name=$1 AND column_name=$2
				)`, tbl, col).Scan(&exists)
			if exists {
				dateCol = col
				break
			}
		}
		var n int64
		where := "(" + codeCol + " IS NULL OR " + codeCol + " < 200 OR " + codeCol + " >= 300)"
		if dateCol != "" {
			where += " AND " + dateCol + " >= NOW() - INTERVAL '7 days'"
		}
		_ = h.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM "+tbl+" WHERE "+where).Scan(&n)
		return n
	}
	return 0
}

// stoppedCount conta pedidos com _senderzz_motoboy_flow_status em estado ativo mas sem movimentação por 24h+.
func (h *DashboardHandler) stoppedCount(ctx context.Context) int64 {
	rows := h.stoppedOrderRows(ctx)
	return int64(len(rows))
}

// stoppedOrderRows retorna pedidos parados 24h+ (meta _senderzz_motoboy_flow_status).
// Espelha stopped_order_rows() PHP.
func (h *DashboardHandler) stoppedOrderRows(ctx context.Context) []stoppedOrder {
	if !h.tableExists(ctx, "sz_orders_meta") && !h.tableExists(ctx, "wc_orders_meta") {
		return nil
	}
	metaTbl := "sz_orders_meta"
	if !h.tableExists(ctx, "sz_orders_meta") {
		metaTbl = "wc_orders_meta"
	}
	orderCol := "order_id"

	threshold := time.Now().Add(-24 * time.Hour).UTC().Format("2006-01-02 15:04:05")

	// Detecta se há sz_orders para join de status e email
	if !h.tableExists(ctx, "sz_orders") {
		return nil
	}

	rows, err := h.Pool.Query(ctx,
		`SELECT DISTINCT m.`+orderCol+` AS pedido_id,
		        COALESCE(o.status,'') AS status,
		        COALESCE(o.billing_email,'') AS email
		 FROM `+metaTbl+` m
		 INNER JOIN sz_orders o ON o.id = m.`+orderCol+`
		 WHERE m.meta_key = '_senderzz_motoboy_flow_status'
		   AND m.meta_value IN ('agendado','embalado','em_rota','em-rota')
		   AND COALESCE(o.status,'') NOT IN ('completed','entregue','frustrado','cancelled','refunded','failed')
		   AND COALESCE(o.date_updated_gmt, o.date_created_gmt) < $1
		 ORDER BY COALESCE(o.date_updated_gmt, o.date_created_gmt) ASC
		 LIMIT 100`, threshold)
	if err != nil {
		return nil
	}
	defer rows.Close()

	out := []stoppedOrder{}
	for rows.Next() {
		var s stoppedOrder
		_ = rows.Scan(&s.PedidoID, &s.Status, &s.Email)
		out = append(out, s)
	}
	return out
}

// auditCounts retorna os 4 contadores financeiros (espelha audit.go Counts).
func (h *DashboardHandler) auditCounts(ctx context.Context) (split, affBad, affMissing, wallet int64) {
	if h.tableExists(ctx, "sz_orders") && h.tableExists(ctx, "senderzz_affiliate_transactions") {
		_ = h.Pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM sz_orders o
			 WHERE o.status IN ('completo','entregue')
			   AND ABS(COALESCE(o.gross,0)
			           - COALESCE(o.affiliate_amount,0)
			           - COALESCE(o.senderzz_fee,0)
			           - COALESCE(o.producer_net,0)) > 0.01`).Scan(&split)

		_ = h.Pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM sz_orders o
			 LEFT JOIN senderzz_affiliate_transactions t
			   ON t.order_id = o.id AND t.type='commission' AND t.status <> 'cancelled'
			 WHERE o.status IN ('completo','entregue')
			   AND COALESCE(o.affiliate_id,0) > 0
			   AND COALESCE(o.affiliate_amount,0) > 0
			   AND t.id IS NULL`).Scan(&affMissing)

		_ = h.Pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM sz_orders o
			 JOIN senderzz_affiliate_transactions t
			   ON t.order_id = o.id AND t.type='commission' AND t.status <> 'cancelled'
			 WHERE o.status IN ('completo','entregue')
			   AND ABS(COALESCE(o.affiliate_amount,0) - COALESCE(t.amount,0)) > 0.01`).Scan(&affBad)
	}
	if h.tableExists(ctx, "sz_orders") && h.tableExists(ctx, "sz_cod_wallet_transactions") {
		_ = h.Pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM sz_orders o
			 JOIN sz_cod_wallet_transactions c
			   ON c.order_id = o.id AND c.type='credit'
			 WHERE o.status IN ('completo','entregue')
			   AND ABS(COALESCE(o.producer_net,0) - COALESCE(c.net,0)) > 0.01`).Scan(&wallet)
	}
	return
}

// Summary retorna os 6 KPIs operacionais.
// GET /dashboard
func (h *DashboardHandler) Summary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	today := todayBRT()
	tbl := h.orderTable(ctx)

	var k kpis
	k.PedidosHoje = h.countByStatus(ctx, tbl, []string{"processing", "agendado", "completed", "frustrado"}, today)
	k.Agendados = h.countByStatus(ctx, tbl, []string{"agendado"}, "")
	k.EmRota = h.countByStatus(ctx, tbl, []string{"em_rota", "em-rota"}, "")
	k.EntreguesHoje = h.countByStatus(ctx, tbl, []string{"completed", "entregue"}, today)
	k.FrustradosHoje = h.countByStatus(ctx, tbl, []string{"frustrado"}, today)

	split, affBad, affMissing, wallet := h.auditCounts(ctx)
	webhookFail := h.webhookFailCount(ctx)
	stopped := h.stoppedCount(ctx)
	k.AlertasTotal = split + affBad + affMissing + wallet + webhookFail + stopped

	httpx.JSON(w, 200, k)
}

// Alerts retorna as 6 linhas de alertas operacionais.
// GET /dashboard/alerts
func (h *DashboardHandler) Alerts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	split, affBad, affMissing, wallet := h.auditCounts(ctx)
	webhookFail := h.webhookFailCount(ctx)
	stopped := h.stoppedCount(ctx)

	out := alertas{
		SaldoDivergente:    wallet,
		AffSemTransacao:    affMissing,
		WalletDivergente:   affBad,
		SplitDivergente:    split,
		WebhooksFalhando7d: webhookFail,
		PedidosParados24h:  stopped,
	}
	httpx.JSON(w, 200, out)
}

// StoppedOrders retorna pedidos parados 24h+ (meta flow_status ativo sem movimentação).
// GET /dashboard/stopped-orders
func (h *DashboardHandler) StoppedOrders(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows := h.stoppedOrderRows(ctx)
	if rows == nil {
		rows = []stoppedOrder{}
	}
	httpx.JSON(w, 200, map[string]any{"items": rows, "count": len(rows)})
}
