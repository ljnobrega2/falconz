// Package handlers — endpoint admin para audit engine.
// Espelha includes/senderzz-audit-engine.php (PHP legado) sobre Postgres.
// Detecta 4 tipos de divergência financeira e oferece batch + per-order fixes.
package handlers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

type AuditHandler struct{ Pool *pgxpool.Pool }

// AuditCounts contadores por tipo. Espelha senderzz_audit_counts() PHP.
type AuditCounts struct {
	Split      int64 `json:"split"`       // bruto != aff+taxa+prod
	AffBad     int64 `json:"aff_bad"`     // amount em tx != esperado
	AffMissing int64 `json:"aff_missing"` // comissão sem tx
	Wallet     int64 `json:"wallet"`      // net em carteira != esperado
	Total      int64 `json:"total"`
}

type AuditProblem struct {
	OrderID    int64   `json:"order_id"`
	TypeKey    string  `json:"type_key"` // split | aff_bad | aff_missing | wallet
	TypeLabel  string  `json:"type_label"`
	Expected   float64 `json:"expected"`
	Actual     float64 `json:"actual"`
	AffiliateID *int64 `json:"affiliate_id,omitempty"`
	ProducerID *int64  `json:"producer_id,omitempty"`
}

func (h *AuditHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// Counts retorna contadores por tipo. Tabelas ausentes contam como 0.
// GET /audit/counts
func (h *AuditHandler) Counts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := AuditCounts{}

	if h.tableExists(ctx, "sz_orders") && h.tableExists(ctx, "senderzz_affiliate_transactions") {
		_ = h.Pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM sz_orders o
			 WHERE o.status IN ('completo','entregue')
			   AND ABS(COALESCE(o.gross,0)
			           - COALESCE(o.affiliate_amount,0)
			           - COALESCE(o.senderzz_fee,0)
			           - COALESCE(o.producer_net,0)) > 0.01`).Scan(&out.Split)

		_ = h.Pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM sz_orders o
			 LEFT JOIN senderzz_affiliate_transactions t
			   ON t.order_id = o.id AND t.type='commission' AND t.status <> 'cancelled'
			 WHERE o.status IN ('completo','entregue')
			   AND COALESCE(o.affiliate_id,0) > 0
			   AND COALESCE(o.affiliate_amount,0) > 0
			   AND t.id IS NULL`).Scan(&out.AffMissing)

		_ = h.Pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM sz_orders o
			 JOIN senderzz_affiliate_transactions t
			   ON t.order_id = o.id AND t.type='commission' AND t.status <> 'cancelled'
			 WHERE o.status IN ('completo','entregue')
			   AND ABS(COALESCE(o.affiliate_amount,0) - COALESCE(t.amount,0)) > 0.01`).Scan(&out.AffBad)
	}

	if h.tableExists(ctx, "sz_orders") && h.tableExists(ctx, "sz_cod_wallet_transactions") {
		_ = h.Pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM sz_orders o
			 JOIN sz_cod_wallet_transactions c
			   ON c.order_id = o.id AND c.type='credit'
			 WHERE o.status IN ('completo','entregue')
			   AND ABS(COALESCE(o.producer_net,0) - COALESCE(c.net,0)) > 0.01`).Scan(&out.Wallet)
	}

	out.Total = out.Split + out.AffBad + out.AffMissing + out.Wallet
	httpx.JSON(w, 200, out)
}

// Problems lista pedidos divergentes. Limite máx 500.
// GET /audit/problems?limit=100&type=split|aff_bad|aff_missing|wallet
func (h *AuditHandler) Problems(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	typeKey := q.Get("type")

	out := []AuditProblem{}

	if h.tableExists(ctx, "sz_orders") && h.tableExists(ctx, "senderzz_affiliate_transactions") {
		if typeKey == "" || typeKey == "split" {
			rows, err := h.Pool.Query(ctx,
				`SELECT o.id, o.affiliate_id, o.produtor_id,
				        COALESCE(o.gross,0) AS expected,
				        (COALESCE(o.affiliate_amount,0)
				         + COALESCE(o.senderzz_fee,0)
				         + COALESCE(o.producer_net,0)) AS actual
				 FROM sz_orders o
				 WHERE o.status IN ('completo','entregue')
				   AND ABS(COALESCE(o.gross,0)
				           - COALESCE(o.affiliate_amount,0)
				           - COALESCE(o.senderzz_fee,0)
				           - COALESCE(o.producer_net,0)) > 0.01
				 ORDER BY o.id DESC LIMIT $1`, limit)
			if err == nil {
				for rows.Next() {
					var p AuditProblem
					p.TypeKey = "split"
					p.TypeLabel = "Total divergente"
					_ = rows.Scan(&p.OrderID, &p.AffiliateID, &p.ProducerID, &p.Expected, &p.Actual)
					out = append(out, p)
				}
				rows.Close()
			}
		}

		if typeKey == "" || typeKey == "aff_missing" {
			rows, err := h.Pool.Query(ctx,
				`SELECT o.id, o.affiliate_id, o.produtor_id, COALESCE(o.affiliate_amount,0), 0
				 FROM sz_orders o
				 LEFT JOIN senderzz_affiliate_transactions t
				   ON t.order_id = o.id AND t.type='commission' AND t.status <> 'cancelled'
				 WHERE o.status IN ('completo','entregue')
				   AND COALESCE(o.affiliate_id,0) > 0
				   AND COALESCE(o.affiliate_amount,0) > 0
				   AND t.id IS NULL
				 ORDER BY o.id DESC LIMIT $1`, limit)
			if err == nil {
				for rows.Next() {
					var p AuditProblem
					p.TypeKey = "aff_missing"
					p.TypeLabel = "Afiliado sem transação"
					_ = rows.Scan(&p.OrderID, &p.AffiliateID, &p.ProducerID, &p.Expected, &p.Actual)
					out = append(out, p)
				}
				rows.Close()
			}
		}

		if typeKey == "" || typeKey == "aff_bad" {
			rows, err := h.Pool.Query(ctx,
				`SELECT o.id, o.affiliate_id, o.produtor_id,
				        COALESCE(o.affiliate_amount,0), COALESCE(t.amount,0)
				 FROM sz_orders o
				 JOIN senderzz_affiliate_transactions t
				   ON t.order_id = o.id AND t.type='commission' AND t.status <> 'cancelled'
				 WHERE o.status IN ('completo','entregue')
				   AND ABS(COALESCE(o.affiliate_amount,0) - COALESCE(t.amount,0)) > 0.01
				 ORDER BY o.id DESC LIMIT $1`, limit)
			if err == nil {
				for rows.Next() {
					var p AuditProblem
					p.TypeKey = "aff_bad"
					p.TypeLabel = "Comissão afiliado divergente"
					_ = rows.Scan(&p.OrderID, &p.AffiliateID, &p.ProducerID, &p.Expected, &p.Actual)
					out = append(out, p)
				}
				rows.Close()
			}
		}
	}

	if (typeKey == "" || typeKey == "wallet") &&
		h.tableExists(ctx, "sz_orders") && h.tableExists(ctx, "sz_cod_wallet_transactions") {
		rows, err := h.Pool.Query(ctx,
			`SELECT o.id, o.affiliate_id, o.produtor_id,
			        COALESCE(o.producer_net,0), COALESCE(c.net,0)
			 FROM sz_orders o
			 JOIN sz_cod_wallet_transactions c
			   ON c.order_id = o.id AND c.type='credit'
			 WHERE o.status IN ('completo','entregue')
			   AND ABS(COALESCE(o.producer_net,0) - COALESCE(c.net,0)) > 0.01
			 ORDER BY o.id DESC LIMIT $1`, limit)
		if err == nil {
			for rows.Next() {
				var p AuditProblem
				p.TypeKey = "wallet"
				p.TypeLabel = "Produtor COD divergente"
				_ = rows.Scan(&p.OrderID, &p.AffiliateID, &p.ProducerID, &p.Expected, &p.Actual)
				out = append(out, p)
			}
			rows.Close()
		}
	}

	httpx.JSON(w, 200, map[string]any{"items": out, "count": len(out)})
}

// FixAll executa as 4 funções de correção em batch.
// POST /audit/fix-all
func (h *AuditHandler) FixAll(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	res := map[string]int{
		"missing_inserted":         0,
		"bad_transactions_updated": 0,
		"producer_wallet_updated":  0,
		"wallet_summary_synced":    0,
	}

	if h.tableExists(ctx, "sz_orders") && h.tableExists(ctx, "senderzz_affiliate_transactions") {
		tag, err := h.Pool.Exec(ctx,
			`INSERT INTO senderzz_affiliate_transactions
			   (order_id, affiliate_id, type, status, amount, available_at, meta_json, created_at)
			 SELECT o.id, o.affiliate_id, 'commission', 'pending',
			        COALESCE(o.affiliate_amount,0),
			        NOW() + INTERVAL '1 day' * COALESCE(o.retention_days, 7),
			        jsonb_build_object('source','admin_audit_fix'),
			        NOW()
			 FROM sz_orders o
			 LEFT JOIN senderzz_affiliate_transactions t
			   ON t.order_id = o.id AND t.type='commission' AND t.status <> 'cancelled'
			 WHERE o.status IN ('completo','entregue')
			   AND COALESCE(o.affiliate_id,0) > 0
			   AND COALESCE(o.affiliate_amount,0) > 0
			   AND t.id IS NULL`)
		if err == nil {
			res["missing_inserted"] = int(tag.RowsAffected())
		}

		tag, err = h.Pool.Exec(ctx,
			`UPDATE senderzz_affiliate_transactions t
			 SET amount = o.affiliate_amount,
			     meta_json = COALESCE(meta_json, '{}'::jsonb) || jsonb_build_object('source','admin_audit_fix','prev_amount', t.amount),
			     updated_at = NOW()
			 FROM sz_orders o
			 WHERE t.order_id = o.id
			   AND t.type='commission' AND t.status <> 'cancelled'
			   AND o.status IN ('completo','entregue')
			   AND ABS(COALESCE(o.affiliate_amount,0) - COALESCE(t.amount,0)) > 0.01`)
		if err == nil {
			res["bad_transactions_updated"] = int(tag.RowsAffected())
		}
	}

	if h.tableExists(ctx, "sz_orders") && h.tableExists(ctx, "sz_cod_wallet_transactions") {
		tag, err := h.Pool.Exec(ctx,
			`UPDATE sz_cod_wallet_transactions c
			 SET gross = COALESCE(o.gross,0),
			     fee = COALESCE(o.senderzz_fee,0) + COALESCE(o.delivery_fee,0),
			     net = COALESCE(o.producer_net,0),
			     updated_at = NOW()
			 FROM sz_orders o
			 WHERE c.order_id = o.id AND c.type='credit'
			   AND o.status IN ('completo','entregue')
			   AND ABS(COALESCE(o.producer_net,0) - COALESCE(c.net,0)) > 0.01`)
		if err == nil {
			res["producer_wallet_updated"] = int(tag.RowsAffected())
		}
	}

	if h.tableExists(ctx, "senderzz_affiliate_wallet") && h.tableExists(ctx, "senderzz_affiliate_transactions") {
		tag, err := h.Pool.Exec(ctx,
			`UPDATE senderzz_affiliate_wallet w
			 SET balance = COALESCE((
			       SELECT SUM(amount) FROM senderzz_affiliate_transactions
			       WHERE affiliate_id = w.affiliate_id
			         AND status='available'
			     ), 0),
			     pending_balance = COALESCE((
			       SELECT SUM(amount) FROM senderzz_affiliate_transactions
			       WHERE affiliate_id = w.affiliate_id
			         AND status='pending'
			     ), 0),
			     updated_at = NOW()`)
		if err == nil {
			res["wallet_summary_synced"] = int(tag.RowsAffected())
		}
	}

	httpx.JSON(w, 200, map[string]any{"ok": true, "result": res})
}

// FixOrder corrige todos os tipos para um único pedido.
// POST /audit/fix-order/{id}
func (h *AuditHandler) FixOrder(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	orderID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || orderID <= 0 {
		httpx.Err(w, 400, "bad_request", "order_id inválido")
		return
	}
	ctx := r.Context()
	res := map[string]int{"missing_inserted": 0, "bad_transaction_updated": 0, "producer_wallet_updated": 0}

	if h.tableExists(ctx, "sz_orders") && h.tableExists(ctx, "senderzz_affiliate_transactions") {
		tag, err := h.Pool.Exec(ctx,
			`INSERT INTO senderzz_affiliate_transactions
			   (order_id, affiliate_id, type, status, amount, available_at, meta_json, created_at)
			 SELECT o.id, o.affiliate_id, 'commission', 'pending',
			        COALESCE(o.affiliate_amount,0),
			        NOW() + INTERVAL '1 day' * COALESCE(o.retention_days, 7),
			        jsonb_build_object('source','admin_audit_fix_order'),
			        NOW()
			 FROM sz_orders o
			 LEFT JOIN senderzz_affiliate_transactions t
			   ON t.order_id = o.id AND t.type='commission' AND t.status <> 'cancelled'
			 WHERE o.id = $1
			   AND COALESCE(o.affiliate_id,0) > 0
			   AND COALESCE(o.affiliate_amount,0) > 0
			   AND t.id IS NULL`, orderID)
		if err == nil {
			res["missing_inserted"] = int(tag.RowsAffected())
		}

		tag, err = h.Pool.Exec(ctx,
			`UPDATE senderzz_affiliate_transactions t
			 SET amount = o.affiliate_amount,
			     meta_json = COALESCE(meta_json, '{}'::jsonb) || jsonb_build_object('source','admin_audit_fix_order','prev_amount', t.amount),
			     updated_at = NOW()
			 FROM sz_orders o
			 WHERE t.order_id = o.id
			   AND t.type='commission' AND t.status <> 'cancelled'
			   AND o.id = $1
			   AND ABS(COALESCE(o.affiliate_amount,0) - COALESCE(t.amount,0)) > 0.01`, orderID)
		if err == nil {
			res["bad_transaction_updated"] = int(tag.RowsAffected())
		}
	}

	if h.tableExists(ctx, "sz_orders") && h.tableExists(ctx, "sz_cod_wallet_transactions") {
		tag, err := h.Pool.Exec(ctx,
			`UPDATE sz_cod_wallet_transactions c
			 SET gross = COALESCE(o.gross,0),
			     fee = COALESCE(o.senderzz_fee,0) + COALESCE(o.delivery_fee,0),
			     net = COALESCE(o.producer_net,0),
			     updated_at = NOW()
			 FROM sz_orders o
			 WHERE c.order_id = o.id AND c.type='credit'
			   AND o.id = $1
			   AND ABS(COALESCE(o.producer_net,0) - COALESCE(c.net,0)) > 0.01`, orderID)
		if err == nil {
			res["producer_wallet_updated"] = int(tag.RowsAffected())
		}
	}

	httpx.JSON(w, 200, map[string]any{"ok": true, "order_id": orderID, "result": res})
}

// FixAffiliateWallet sincroniza saldo de carteira de um afiliado específico.
// POST /affiliates/{id}/wallet-fix
func (h *AuditHandler) FixAffiliateWallet(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	affID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || affID <= 0 {
		httpx.Err(w, 400, "bad_request", "affiliate_id inválido")
		return
	}
	ctx := r.Context()

	if !h.tableExists(ctx, "senderzz_affiliate_wallet") ||
		!h.tableExists(ctx, "senderzz_affiliate_transactions") {
		httpx.Err(w, 503, "tables_missing", "tabelas de afiliado ainda não migradas")
		return
	}

	tag, err := h.Pool.Exec(ctx,
		`UPDATE senderzz_affiliate_wallet w
		 SET balance = COALESCE((
		       SELECT SUM(amount) FROM senderzz_affiliate_transactions
		       WHERE affiliate_id = w.affiliate_id AND status='available'
		     ), 0),
		     pending_balance = COALESCE((
		       SELECT SUM(amount) FROM senderzz_affiliate_transactions
		       WHERE affiliate_id = w.affiliate_id AND status='pending'
		     ), 0),
		     updated_at = NOW()
		 WHERE w.affiliate_id = $1`, affID)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	httpx.JSON(w, 200, map[string]any{"ok": true, "affiliate_id": affID, "rows_affected": tag.RowsAffected()})
}
