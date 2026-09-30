// Package handlers — endpoint admin para o Dashboard operacional.
// Espelha tab_overview_operacao() em src/Admin/Unified_Menu.php.
//
// Endpoints:
//
//	GET /dashboard         → KPIs operacionais (6 cards)
//	GET /dashboard/alerts  → 7 linhas de alertas operacionais
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
	PedidosHoje    int64 `json:"pedidos_hoje"`    // sz_orders status processing|agendado|completo|frustrado, created_at=hoje
	Agendados      int64 `json:"agendados"`        // COUNT sz_motoboy_pedidos.status=agendado (sem filtro de data)
	EmRota         int64 `json:"em_rota"`          // COUNT sz_motoboy_pedidos.status IN (em_rota, em-rota)
	EntreguesHoje  int64 `json:"entregues_hoje"`   // COUNT sz_orders status IN (completo, entregue) WHERE data_entrega=hoje
	FrustradosHoje int64 `json:"frustrados_hoje"`  // COUNT sz_orders status=frustrado WHERE created_at=hoje
	AlertasTotal   int64 `json:"alertas_total"`    // SUM audit_counts + webhook_fails(7d) + pedidos_parados
}

// alertas — 7 linhas de alertas operacionais. Espelha alert_line() + get_audit_counts() + webhook_failure_count().
type alertas struct {
	SaldoDivergente        int64 `json:"saldo_divergente"`         // wallet
	AffSemTransacao        int64 `json:"aff_sem_transacao"`        // aff_missing
	WalletDivergente       int64 `json:"wallet_divergente"`        // aff_bad
	SplitDivergente        int64 `json:"split_divergente"`         // split
	WebhooksFalhando7d     int64 `json:"webhooks_falhando_7d"`     // webhook_failure_count (7 dias, 4 tabelas)
	PedidosParados24h      int64 `json:"pedidos_parados_24h"`      // stopped_order_rows (24h)
	CronFinanceiroFalhando int64 `json:"cron_financeiro_falhando"` // QUALQUER cron last_status='error' (inclui os que liberam dinheiro: COD + comissão)
}

// stoppedOrder — linha de pedido parado.
type stoppedOrder struct {
	PedidoID int64  `json:"pedido_id"`
	Status   string `json:"status"`
	Email    string `json:"email"`
}

// tableExistsDash verifica existência da tabela no schema public.
func (h *DashboardHandler) tableExists(ctx context.Context, name string) bool {
	return tableExistsCached(ctx, h.Pool, name) // AUDIT-2026-06-18 Onda2 (go-infoschema-cache)
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
		// AUDIT-2026-06-18 Onda2 (go-date-sargable): range sargável no lugar de
		// created_at::date = $N (cast na coluna impede uso de índice). Resultado
		// idêntico: todos os timestamps do dia $N.
		p := itoa(len(args))
		dateClause = " AND created_at >= $" + p + " AND created_at < $" + p + "::date + interval '1 day'"
	}

	var n int64
	_ = h.Pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM "+tbl+" WHERE status IN ("+in+")"+dateClause,
		args...).Scan(&n)
	return n
}

// countMbByStatus conta linhas em sz_motoboy_pedidos cujo status do fluxo
// motoboy está em `statuses`. ALTO-12: os estados operacionais 'agendado' e
// 'em_rota' NÃO existem em sz_orders.status — vivem em sz_motoboy_pedidos.status
// (vide bulk_actions.go:35-40, fluxo agendado → embalado → em_rota → ...).
// Graceful: tabela ausente = 0.
func (h *DashboardHandler) countMbByStatus(ctx context.Context, statuses []string) int64 {
	if len(statuses) == 0 || !h.tableExists(ctx, "sz_motoboy_pedidos") {
		return 0
	}
	args := []any{}
	in := ""
	for i, s := range statuses {
		if i > 0 {
			in += ","
		}
		args = append(args, s)
		in += "$" + itoa(i+1)
	}
	var n int64
	_ = h.Pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM sz_motoboy_pedidos WHERE status IN ("+in+")",
		args...).Scan(&n)
	return n
}

// countDeliveredToday conta pedidos entregues HOJE pela DATA DE ENTREGA.
// ALTO-12 corrige duas coisas:
//   (a) status correto em sz_orders = 'completo'/'entregue' (não 'completed' —
//       vide auditCounts() acima);
//   (c) "entregues hoje" deve filtrar pela data de ENTREGA, não created_at.
//       Usa o COALESCE canônico de bulk_actions.go:191
//       (mp.data_entrega → mp.reagendado_para → o.created_at::date) via LEFT JOIN,
//       preservando entregas Melhor Envio (sem linha motoboy) pelo fallback.
// COUNT(DISTINCT o.id) evita duplicar pedidos com múltiplas linhas mp.
func (h *DashboardHandler) countDeliveredToday(ctx context.Context, tbl, date string) int64 {
	if tbl == "" {
		return 0
	}
	dateCol := "o.created_at::date"
	join := ""
	if h.tableExists(ctx, "sz_motoboy_pedidos") {
		join = " LEFT JOIN sz_motoboy_pedidos mp ON mp.wc_order_id = o.id"
		dateCol = "COALESCE(mp.data_entrega, mp.reagendado_para, o.created_at::date)"
	}
	var n int64
	_ = h.Pool.QueryRow(ctx,
		"SELECT COUNT(DISTINCT o.id) FROM "+tbl+" o"+join+
			" WHERE o.status IN ('completo','entregue') AND "+dateCol+" = $1::date",
		date).Scan(&n)
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

// cronFailCount conta QUALQUER cron em estado de ERRO na última execução
// (senderzz_cron_status.last_status='error') — sem filtro por nome. Inclui,
// portanto, os que LIBERAM DINHEIRO (sz_cod_release_due, sz_affiliate_release_due,
// cujo dinheiro pode ficar preso em pending na falha) E os demais (tracking,
// limpeza etc.). A contagem é deliberadamente ampla: qualquer cron falho merece
// alerta. O runner go/cron grava EXATAMENTE 'error' na falha (vide go/cron
// main.go: res.status="error"); por isso filtramos por '=error' e NÃO por '!=ok'
// — os estados 'never'/'manual_trigger'/'skipped' são normais e não devem
// disparar alerta. Graceful: tabela ausente = 0.
func (h *DashboardHandler) cronFailCount(ctx context.Context) int64 {
	if !h.tableExists(ctx, "senderzz_cron_status") {
		return 0
	}
	var n int64
	_ = h.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM senderzz_cron_status WHERE last_status = 'error'`).Scan(&n)
	return n
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
		   AND COALESCE(o.updated_at, o.created_at) < $1
		 ORDER BY COALESCE(o.updated_at, o.created_at) ASC
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
			`SELECT COUNT(*) FROM sz_order_financials f
			 WHERE f.status IN ('completo','entregue')
			   AND ABS(COALESCE(f.total,0)
			           - COALESCE(f.affiliate_liquida,0)
			           - COALESCE(f.affiliate_take,0)
			           - COALESCE(f.delivery_fee,0)
			           - COALESCE(f.producer_take,0)
			           - COALESCE(f.producer_net_live,0)) > 0.01`).Scan(&split)

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
		// BUG-FIX 2026-07-14: type='credit' nunca existiu na tabela (real='cod_received') —
		// esse contador ficava sempre 0. Ver AUDIT-2026-07-14 (audit.go).
		_ = h.Pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM sz_orders o
			 JOIN (SELECT order_id, SUM(COALESCE(NULLIF(net,0),gross)) net
			         FROM sz_cod_wallet_transactions
			        WHERE type='cod_received' AND status <> 'reversed'
			        GROUP BY order_id) c ON c.order_id = o.id
			 JOIN sz_order_financials f ON f.order_id = o.id
			 WHERE o.status IN ('completo','entregue')
			   AND COALESCE(o.produtor_id,0) > 0
			   AND ABS(COALESCE(f.producer_net_live,0) - COALESCE(c.net,0)) > 0.01`).Scan(&wallet)
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
	// ALTO-12 (a): status em sz_orders é 'completo', não 'completed' (vide auditCounts).
	k.PedidosHoje = h.countByStatus(ctx, tbl, []string{"processing", "agendado", "completo", "frustrado"}, today)
	// ALTO-12 (b): agendado/em_rota vivem em sz_motoboy_pedidos.status, não em sz_orders.status.
	k.Agendados = h.countMbByStatus(ctx, []string{"agendado"})
	k.EmRota = h.countMbByStatus(ctx, []string{"em_rota", "em-rota"})
	// ALTO-12 (a)+(c): status 'completo'/'entregue' e filtro pela DATA DE ENTREGA.
	k.EntreguesHoje = h.countDeliveredToday(ctx, tbl, today)
	k.FrustradosHoje = h.countByStatus(ctx, tbl, []string{"frustrado"}, today)

	split, affBad, affMissing, wallet := h.auditCounts(ctx)
	webhookFail := h.webhookFailCount(ctx)
	stopped := h.stoppedCount(ctx)
	cronFail := h.cronFailCount(ctx)
	k.AlertasTotal = split + affBad + affMissing + wallet + webhookFail + stopped + cronFail

	httpx.JSON(w, 200, k)
}

// Alerts retorna as 7 linhas de alertas operacionais.
// GET /dashboard/alerts
func (h *DashboardHandler) Alerts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	split, affBad, affMissing, wallet := h.auditCounts(ctx)
	webhookFail := h.webhookFailCount(ctx)
	stopped := h.stoppedCount(ctx)
	cronFail := h.cronFailCount(ctx)

	out := alertas{
		SaldoDivergente:        wallet,
		AffSemTransacao:        affMissing,
		WalletDivergente:       affBad,
		SplitDivergente:        split,
		WebhooksFalhando7d:     webhookFail,
		PedidosParados24h:      stopped,
		CronFinanceiroFalhando: cronFail,
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
