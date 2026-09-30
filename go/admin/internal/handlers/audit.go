// Package handlers — endpoint admin para audit engine.
// Espelha includes/senderzz-audit-engine.php (PHP legado) sobre Postgres.
// Detecta 4 tipos de divergência financeira e oferece batch + per-order fixes.
package handlers

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

type AuditHandler struct{ Pool *pgxpool.Pool }

// AuditCounts contadores por tipo. Espelha senderzz_audit_counts() PHP.
type AuditCounts struct {
	Split         int64 `json:"split"`          // bruto != aff+taxa+prod
	AffBad        int64 `json:"aff_bad"`        // amount em tx != esperado
	AffMissing    int64 `json:"aff_missing"`    // comissão sem tx
	Wallet        int64 `json:"wallet"`         // net em carteira != esperado
	WalletMissing int64 `json:"wallet_missing"` // pedido entregue sem NENHUM lançamento cod_received
	Total         int64 `json:"total"`
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
	return tableExistsCached(ctx, h.Pool, name) // AUDIT-2026-06-18 Onda2 (go-infoschema-cache)
}

// Counts retorna contadores por tipo. Tabelas ausentes contam como 0.
// GET /audit/counts
func (h *AuditHandler) Counts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := AuditCounts{}

	if h.tableExists(ctx, "sz_orders") && h.tableExists(ctx, "senderzz_affiliate_transactions") {
		// BUG-FIX 2026-07-14: comparava contra o.senderzz_fee, coluna NUNCA
		// populada em sz_orders (sempre 0) — dava falso positivo em 100% dos
		// pedidos entregues (33/33). Fórmula certa é auto-consistência da própria
		// f (golden): total = bruta_afiliado + taxa_plataforma_produtor + taxa_entrega
		// + líquido_produtor (ver 490-fix-producer-net-entregue-gate.sql).
		_ = h.Pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM sz_orders o
			 JOIN sz_order_financials f ON f.order_id = o.id
			 WHERE o.status IN ('completo','entregue')
			   AND ABS(COALESCE(f.total_pedido,0)
			           - COALESCE(f.comissao_afiliado_bruta,0)
			           - COALESCE(f.taxa_plataforma_produtor,0)
			           - COALESCE(f.taxa_entrega,0)
			           - COALESCE(f.liquido_produtor,0)) > 0.01`).Scan(&out.Split)

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
		// BUG-FIX 2026-07-14: tipo real na tabela é 'cod_received' — 'credit' nunca
		// existiu, então esse JOIN nunca batia e Wallet ficava sempre 0 (cego).
		_ = h.Pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM sz_orders o
			 JOIN sz_order_financials f ON f.order_id = o.id
			 JOIN (SELECT order_id, SUM(COALESCE(NULLIF(net,0),gross)) net
			         FROM sz_cod_wallet_transactions
			        WHERE type='cod_received' AND status <> 'reversed'
			        GROUP BY order_id) c ON c.order_id = o.id
			 WHERE o.status IN ('completo','entregue')
			   AND COALESCE(o.produtor_id,0) > 0
			   AND ABS(COALESCE(f.producer_net_live,0) - COALESCE(c.net,0)) > 0.01`).Scan(&out.Wallet)

		// WalletMissing: pedido entregue com produtor mas SEM nenhuma linha
		// cod_received — o gap que o JOIN acima (por natureza) não enxerga.
		_ = h.Pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM sz_orders o
			 JOIN sz_order_financials f ON f.order_id = o.id
			 LEFT JOIN sz_cod_wallet_transactions c
			   ON c.order_id = o.id AND c.type='cod_received' AND c.status <> 'reversed'
			 WHERE o.status IN ('completo','entregue')
			   AND COALESCE(o.produtor_id,0) > 0
			   AND COALESCE(f.liquido_produtor,0) > 0
			   AND c.id IS NULL`).Scan(&out.WalletMissing)
	}

	out.Total = out.Split + out.AffBad + out.AffMissing + out.Wallet + out.WalletMissing
	httpx.JSON(w, 200, out)
}

// Problems lista pedidos divergentes. Limite máx 500.
// GET /audit/problems?limit=100&type=split|aff_bad|aff_missing|wallet&data_ini=&data_fim=
//
// MED39 (AUDIT-2026-06-22): data_ini/data_fim (YYYY-MM-DD, inclusivos) filtram
// por o.created_at. Antes os params eram enviados pelo painel (AuditEngine.tsx)
// e silenciosamente ignorados — chips davam feedback falso de filtro ativo.
// Predicado sargável espelhando pix.go:84-85 (NÃO castar a coluna). Aplicado às
// 4 queries (split, aff_missing, aff_bad, wallet), todas sobre sz_orders o.
// Counts() permanece global (painel chama /audit/counts sem datas) — não filtrar.
func (h *AuditHandler) Problems(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	typeKey := q.Get("type")
	dataIni := strings.TrimSpace(q.Get("data_ini"))
	dataFim := strings.TrimSpace(q.Get("data_fim"))

	// Cláusula de data compartilhada pelas 4 queries. $1=data_ini, $2=data_fim.
	// '' = sem limite naquela ponta. Ordem dos args fixa: data_ini, data_fim, limit.
	dateClause := ` AND ($1 = '' OR o.created_at >= ($1::date)::timestamp AT TIME ZONE 'America/Sao_Paulo')
	                AND ($2 = '' OR o.created_at <= (($2::date + 1)::timestamp AT TIME ZONE 'America/Sao_Paulo' - interval '1 second'))`

	out := []AuditProblem{}

	if h.tableExists(ctx, "sz_orders") && h.tableExists(ctx, "senderzz_affiliate_transactions") {
		if typeKey == "" || typeKey == "split" {
			// BUG-FIX 2026-07-14: o.senderzz_fee nunca é populado (sempre 0) —
			// ver Counts() acima. Fórmula certa = auto-consistência de f (golden).
			rows, err := h.Pool.Query(ctx,
				`SELECT o.id, o.affiliate_id, o.produtor_id,
				        COALESCE(f.total_pedido,0) AS expected,
				        (COALESCE(f.comissao_afiliado_bruta,0)
				         + COALESCE(f.taxa_plataforma_produtor,0)
				         + COALESCE(f.taxa_entrega,0)
				         + COALESCE(f.liquido_produtor,0)) AS actual
				 FROM sz_orders o
				 JOIN sz_order_financials f ON f.order_id = o.id
				 WHERE o.status IN ('completo','entregue')
				   AND ABS(COALESCE(f.total_pedido,0)
				           - COALESCE(f.comissao_afiliado_bruta,0)
				           - COALESCE(f.taxa_plataforma_produtor,0)
				           - COALESCE(f.taxa_entrega,0)
				           - COALESCE(f.liquido_produtor,0)) > 0.01`+dateClause+`
				 ORDER BY o.id DESC LIMIT $3`, dataIni, dataFim, limit)
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
				   AND t.id IS NULL`+dateClause+`
				 ORDER BY o.id DESC LIMIT $3`, dataIni, dataFim, limit)
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
				   AND ABS(COALESCE(o.affiliate_amount,0) - COALESCE(t.amount,0)) > 0.01`+dateClause+`
				 ORDER BY o.id DESC LIMIT $3`, dataIni, dataFim, limit)
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
		// BUG-FIX 2026-07-14: type='credit' nunca existiu na tabela (real='cod_received').
		rows, err := h.Pool.Query(ctx,
			`SELECT o.id, o.affiliate_id, o.produtor_id,
			        COALESCE(f.producer_net_live,0), COALESCE(c.net,0)
			 FROM sz_orders o
			 JOIN sz_order_financials f ON f.order_id = o.id
			 JOIN (SELECT order_id, SUM(COALESCE(NULLIF(net,0),gross)) net
			         FROM sz_cod_wallet_transactions
			        WHERE type='cod_received' AND status <> 'reversed'
			        GROUP BY order_id) c ON c.order_id = o.id
			 WHERE o.status IN ('completo','entregue')
			   AND COALESCE(o.produtor_id,0) > 0
			   AND ABS(COALESCE(f.producer_net_live,0) - COALESCE(c.net,0)) > 0.01`+dateClause+`
			 ORDER BY o.id DESC LIMIT $3`, dataIni, dataFim, limit)
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

	if (typeKey == "" || typeKey == "wallet_missing") &&
		h.tableExists(ctx, "sz_orders") && h.tableExists(ctx, "sz_cod_wallet_transactions") {
		rows, err := h.Pool.Query(ctx,
			`SELECT o.id, o.affiliate_id, o.produtor_id, COALESCE(f.liquido_produtor,0), 0
			 FROM sz_orders o
			 JOIN sz_order_financials f ON f.order_id = o.id
			 LEFT JOIN sz_cod_wallet_transactions c
			   ON c.order_id = o.id AND c.type='cod_received' AND c.status <> 'reversed'
			 WHERE o.status IN ('completo','entregue')
			   AND COALESCE(o.produtor_id,0) > 0
			   AND COALESCE(f.liquido_produtor,0) > 0
			   AND c.id IS NULL`+dateClause+`
			 ORDER BY o.id DESC LIMIT $3`, dataIni, dataFim, limit)
		if err == nil {
			for rows.Next() {
				var p AuditProblem
				p.TypeKey = "wallet_missing"
				p.TypeLabel = "Produtor sem lançamento COD"
				_ = rows.Scan(&p.OrderID, &p.AffiliateID, &p.ProducerID, &p.Expected, &p.Actual)
				out = append(out, p)
			}
			rows.Close()
		}
	}

	httpx.JSON(w, 200, map[string]any{"items": out, "count": len(out)})
}

// FixAll executa as 4 funções de correção em batch.
// POST /audit/fix-all?limit=500
//
// P0-08: todo o batch roda dentro de UMA transação (read-modify-write atômico).
// Cada bloco de correção é envolvido em SAVEPOINT (transação aninhada do pgx),
// preservando a tolerância a falha por bloco do código legado — uma falha isolada
// (ex.: coluna ausente em sz_orders) reverte só aquele bloco e mantém os demais,
// em vez de abortar a transação inteira.
//
// A sincronização de saldos da carteira (bloco 4) é o read-modify-write crítico:
// ela lê SUM das transações e regrava balance/pending. As linhas-alvo da carteira
// são bloqueadas com SELECT … FOR UPDATE (paginadas via ?limit) antes da
// recomputação, serializando contra ApproveAffiliate/ReleasePending concorrentes
// e evitando lost update. Idempotente.
func (h *AuditHandler) FixAll(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Paginação/limite do bloco de carteira: clamp [1, 5000], default 500.
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 500
	}
	if limit > 5000 {
		limit = 5000
	}

	res := map[string]int{
		"missing_inserted":         0,
		"bad_transactions_updated": 0,
		"producer_wallet_inserted": 0,
		"producer_wallet_updated":  0,
		"wallet_summary_synced":    0,
	}

	hasAffTx := h.tableExists(ctx, "sz_orders") && h.tableExists(ctx, "senderzz_affiliate_transactions")
	hasCod := h.tableExists(ctx, "sz_orders") && h.tableExists(ctx, "sz_cod_wallet_transactions")
	hasWallet := h.tableExists(ctx, "senderzz_affiliate_wallet") && h.tableExists(ctx, "senderzz_affiliate_transactions")

	// Sem nenhuma tabela relevante: nada a fazer, resposta vazia idempotente.
	if !hasAffTx && !hasCod && !hasWallet {
		httpx.JSON(w, 200, map[string]any{"ok": true, "result": res})
		return
	}

	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	// Rollback é no-op após Commit bem-sucedido.
	defer tx.Rollback(ctx)

	// runStep executa fn dentro de um SAVEPOINT. Se fn falhar, reverte só o
	// savepoint (preserva os outros blocos) e devolve o erro para log/decisão.
	runStep := func(fn func(sp pgx.Tx) error) error {
		sp, err := tx.Begin(ctx)
		if err != nil {
			return err
		}
		if err := fn(sp); err != nil {
			_ = sp.Rollback(ctx)
			return err
		}
		return sp.Commit(ctx)
	}

	if hasAffTx {
		// Bloco 1: insere comissões faltantes (status='pending'). Idempotente — só
		// insere onde não há transação 'commission' não-cancelada para o pedido.
		_ = runStep(func(sp pgx.Tx) error {
			tag, err := sp.Exec(ctx,
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
			if err != nil {
				return err
			}
			res["missing_inserted"] = int(tag.RowsAffected())
			return nil
		})

		// Bloco 2: corrige amount divergente das comissões já existentes. Idempotente —
		// só toca linhas cujo amount difere do esperado (> 0.01).
		_ = runStep(func(sp pgx.Tx) error {
			tag, err := sp.Exec(ctx,
				`UPDATE senderzz_affiliate_transactions t
				 SET amount = o.affiliate_amount,
				     meta_json = COALESCE(meta_json, '{}'::jsonb) || jsonb_build_object('source','admin_audit_fix','prev_amount', t.amount),
				     updated_at = NOW()
				 FROM sz_orders o
				 WHERE t.order_id = o.id
				   AND t.type='commission' AND t.status <> 'cancelled'
				   AND o.status IN ('completo','entregue')
				   AND ABS(COALESCE(o.affiliate_amount,0) - COALESCE(t.amount,0)) > 0.01`)
			if err != nil {
				return err
			}
			res["bad_transactions_updated"] = int(tag.RowsAffected())
			return nil
		})
	}

	if hasCod {
		// Bloco 3a (BUG-FIX 2026-07-14): insere cod_received AUSENTE. Antes só
		// existia o UPDATE (3b) — pedido entregue sem NENHUM lançamento nunca era
		// detectado nem corrigido (mesmo gap do aff_missing, mas sem o fix
		// correspondente). user_id da wallet é o wp_user_id (senderzz_portal_users),
		// não o produtor_id de sz_orders — ver cod_wallet_producer.go:58-60.
		_ = runStep(func(sp pgx.Tx) error {
			tag, err := sp.Exec(ctx,
				`INSERT INTO sz_cod_wallet_transactions
				   (user_id, order_id, type, status, amount, gross, net, fee, created_at, updated_at, description)
				 SELECT CASE WHEN pu.wp_user_id > 0 THEN pu.wp_user_id ELSE -pu.id END, o.id, 'cod_received', 'available',
				        f.liquido_produtor, f.liquido_produtor, 0, 0, NOW(), NOW(),
				        'Recebimento COD Motoboy do pedido #' || o.id || ' (líquido produtor) [audit_fix]'
				 FROM sz_orders o
				 JOIN sz_order_financials f ON f.order_id = o.id
				 JOIN senderzz_portal_users pu ON pu.id = o.produtor_id
				 LEFT JOIN sz_cod_wallet_transactions c
				   ON c.order_id = o.id AND c.type='cod_received' AND c.status <> 'reversed'
				 WHERE o.status IN ('completo','entregue')
				   AND COALESCE(o.produtor_id,0) > 0
				   AND COALESCE(f.liquido_produtor,0) > 0
				   AND c.id IS NULL`)
			if err != nil {
				return err
			}
			res["producer_wallet_inserted"] = int(tag.RowsAffected())
			return nil
		})

		// Bloco 3b: ressincroniza carteira do produtor (COD) já existente.
		// BUG-FIX 2026-07-14: type='credit' nunca existiu (real='cod_received') —
		// esse UPDATE nunca tocava nenhuma linha.
		// GUARDA (AUDIT-2026-07-14): só corrige PRA CIMA (f.producer_net_live >=
		// valor já creditado). Nunca reduz — pedidos antigos foram creditados com
		// fórmula anterior (drift ~R$1,02/pedido, sempre a favor do produtor); esse
		// dinheiro já está disponível/pode já ter sido sacado, então tosar
		// retroativamente seria clawback sobre saldo real. Só sobe quando a fórmula
		// atual dá valor MAIOR que o creditado (produtor tinha ficado devendo).
		_ = runStep(func(sp pgx.Tx) error {
			tag, err := sp.Exec(ctx,
				`UPDATE sz_cod_wallet_transactions c
				 SET gross = f.producer_net_live,
				     net = f.producer_net_live,
				     updated_at = NOW()
				 FROM sz_orders o
				 JOIN sz_order_financials f ON f.order_id = o.id
				 WHERE c.order_id = o.id AND c.type='cod_received' AND c.status <> 'reversed'
				   AND o.status IN ('completo','entregue')
				   AND f.producer_net_live > COALESCE(NULLIF(c.net,0), c.gross) + 0.01`)
			if err != nil {
				return err
			}
			res["producer_wallet_updated"] = int(tag.RowsAffected())
			return nil
		})
	}

	if hasWallet {
		// Bloco 4 (crítico): read-modify-write da carteira de afiliados.
		// Bloqueia uma página de linhas (ORDER BY + LIMIT determinístico) com
		// FOR UPDATE antes de recomputar, serializando com saques concorrentes.
		_ = runStep(func(sp pgx.Tx) error {
			// AUDIT-2026-06-21 #HIGH-4: reconciliador BATCH (varre todos os afiliados).
			// Mesmo bug dos demais: balance = SUM WHERE status='approved' excluía os
			// débitos de saque (type='withdrawal', status='paid', amount<0) e ressuscitava
			// o valor sacado para CADA afiliado da página. Corrigido para incluir saídas:
			// SUM WHERE status NOT IN ('pending','cancelled'). 'cancelled' cobre estornos.
			//
			// CRIT-A (AUDIT-CRIT-AB): este reconciliador escreve o MESMO cache
			// (senderzz_affiliate_wallet.balance) que o WalletFix/ReleasePending — sem a
			// correção da penalty ele recontaminaria o cache (penalty positiva somada como
			// crédito). penalty entra como -ABS(amount) + piso GREATEST(0,...), idêntico
			// aos demais sites. Mantém consistência admin↔portal.
			tag, err := sp.Exec(ctx,
				`UPDATE senderzz_affiliate_wallet w
				 SET balance = GREATEST(0, COALESCE((
				       SELECT SUM(CASE WHEN type='penalty' THEN -ABS(amount) ELSE amount END)
				       FROM senderzz_affiliate_transactions
				       WHERE affiliate_id = w.affiliate_id
				         AND status NOT IN ('pending','cancelled')
				     ), 0)),
				     pending_balance = COALESCE((
				       SELECT SUM(amount) FROM senderzz_affiliate_transactions
				       WHERE affiliate_id = w.affiliate_id
				         AND status='pending'
				     ), 0),
				     updated_at = NOW()
				 WHERE w.affiliate_id IN (
				       SELECT affiliate_id FROM senderzz_affiliate_wallet
				       ORDER BY affiliate_id ASC
				       LIMIT $1
				       FOR UPDATE
				 )`, limit)
			if err != nil {
				return err
			}
			res["wallet_summary_synced"] = int(tag.RowsAffected())
			return nil
		})
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
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
	res := map[string]int{"missing_inserted": 0, "bad_transaction_updated": 0, "producer_wallet_inserted": 0, "producer_wallet_updated": 0}

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
		// BUG-FIX 2026-07-14: insere cod_received ausente (pedido nunca creditado).
		tag, err := h.Pool.Exec(ctx,
			`INSERT INTO sz_cod_wallet_transactions
			   (user_id, order_id, type, status, amount, gross, net, fee, created_at, updated_at, description)
				 SELECT CASE WHEN pu.wp_user_id > 0 THEN pu.wp_user_id ELSE -pu.id END, o.id, 'cod_received', 'available',
			        f.liquido_produtor, f.liquido_produtor, 0, 0, NOW(), NOW(),
			        'Recebimento COD Motoboy do pedido #' || o.id || ' (líquido produtor) [audit_fix_order]'
			 FROM sz_orders o
			 JOIN sz_order_financials f ON f.order_id = o.id
			 JOIN senderzz_portal_users pu ON pu.id = o.produtor_id
			 LEFT JOIN sz_cod_wallet_transactions c
			   ON c.order_id = o.id AND c.type='cod_received' AND c.status <> 'reversed'
			 WHERE o.id = $1
			   AND COALESCE(o.produtor_id,0) > 0
			   AND COALESCE(f.liquido_produtor,0) > 0
			   AND c.id IS NULL`, orderID)
		if err == nil {
			res["producer_wallet_inserted"] = int(tag.RowsAffected())
		}

		// BUG-FIX 2026-07-14: type='credit' nunca existiu (real='cod_received').
		// GUARDA: só corrige pra cima, nunca clawback (ver bloco 3b do FixAll).
		tag, err = h.Pool.Exec(ctx,
			`UPDATE sz_cod_wallet_transactions c
			 SET gross = f.producer_net_live,
			     net = f.producer_net_live,
			     updated_at = NOW()
			 FROM sz_orders o
			 JOIN sz_order_financials f ON f.order_id = o.id
			 WHERE c.order_id = o.id AND c.type='cod_received' AND c.status <> 'reversed'
			   AND o.id = $1
			   AND f.producer_net_live > COALESCE(NULLIF(c.net,0), c.gross) + 0.01`, orderID)
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

	// P0-08: read-modify-write em transação com lock pessimista.
	// A recomputação de balance/pending lê SUM das transações e regrava a carteira;
	// sem a transação + SELECT … FOR UPDATE da linha alvo, um ApproveAffiliate
	// concorrente (que faz SELECT balance FOR UPDATE → debita) poderia interleavar
	// entre a leitura agregada e a gravação, sobrescrevendo o débito (lost update).
	// O lock na linha da carteira serializa com aquele fluxo.
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	// Rollback é no-op após Commit bem-sucedido.
	defer tx.Rollback(ctx)

	// 1. Bloqueia a linha da carteira alvo. Carteira inexistente = 0 linhas afetadas
	//    (preserva o comportamento legado: o UPDATE original também não tocaria nada).
	var lockedAff int64
	err = tx.QueryRow(ctx,
		`SELECT affiliate_id FROM senderzz_affiliate_wallet
		 WHERE affiliate_id = $1 FOR UPDATE`, affID).Scan(&lockedAff)
	if err != nil {
		// Sem carteira: nada a corrigir. Idempotente — responde 0 linhas.
		_ = tx.Rollback(ctx)
		httpx.JSON(w, 200, map[string]any{"ok": true, "affiliate_id": affID, "rows_affected": 0})
		return
	}

	// 2. Recomputa saldos a partir do livro razão (status correto — ver P0-02).
	// AUDIT-2026-06-21 #HIGH-4: balance inclui débitos de saque (type='withdrawal',
	// status='available', amount<0 — ver includes/senderzz-affiliates.php:4619-4622).
	// Filtrar só 'approved' excluía esses débitos e ressuscitava o valor já sacado
	// (double-spend). Regra: SUM WHERE status NOT IN ('pending','cancelled') —
	// 'approved'/'available' cobre comissões liberadas (+) E saques (-); 'cancelled'
	// cobre estornos (linha de comissão marcada 'cancelled', sem compensatória).
	// Regressão: +500 approved + -500 available => balance 0.
	//
	// CRIT-A (AUDIT-CRIT-AB): escreve o MESMO cache que WalletFix — penalty (despesa
	// gravada positiva no PHP) entra como -ABS(amount) + piso GREATEST(0,...), senão
	// recontamina o saldo sacável (penalty somada como crédito). Idêntico aos demais sites.
	tag, err := tx.Exec(ctx,
		`UPDATE senderzz_affiliate_wallet w
		 SET balance = GREATEST(0, COALESCE((
		       SELECT SUM(CASE WHEN type='penalty' THEN -ABS(amount) ELSE amount END)
		       FROM senderzz_affiliate_transactions
		       WHERE affiliate_id = w.affiliate_id AND status NOT IN ('pending','cancelled')
		     ), 0)),
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

	if err := tx.Commit(ctx); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	httpx.JSON(w, 200, map[string]any{"ok": true, "affiliate_id": affID, "rows_affected": tag.RowsAffected()})
}
