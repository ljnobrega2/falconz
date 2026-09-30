// Package handlers — ações em LOTE para as filas de aprovação do admin.
//
// AUDIT UX (persona Admin, Onda 2 — P0): "Multi-seleção + barra de ações em lote
// nas filas: Saques COD (aprovar/pagar), Onboarding (aprovar/rejeitar), Pedidos
// (cancelar/reagendar). Reusar o motor de BulkActions de etiquetas."
//
// Princípios (espelham o que o motor de etiquetas em bulk_actions.go já faz, mas
// para as filas de DECISÃO em vez de geração de etiqueta):
//
//   - IDEMPOTENTE: cada operação reusa VERBATIM as cláusulas-guarda de status das
//     handlers singulares (cod_saques.go, onboarding.go, orders.go). Reexecutar um
//     lote reporta os ids já-decididos como "skipped", nunca aplica duas vezes.
//   - TRANSACIONAL POR ITEM: cada id roda na SUA própria transação. Um id que falha
//     (ex.: saldo insuficiente no saque de afiliado) NÃO desfaz os outros 99 — ele
//     entra em "errors" e o lote continua. (NÃO é uma transação única gigante.)
//   - AUTH ADMIN: as rotas ficam dentro do grupo autenticado (auth.Middleware) em
//     main.go; decided_by/actor sai de auth.FromCtx, igual às handlers singulares.
//   - CAP de 100 ids por requisição (mesmo teto de bulk_actions.go).
//
// Surface (todas POST, sob /wp-json/senderzz/v1/admin/):
//
//	/bulk-actions/cod-saques/producer/mark-paid   body {ids:[], admin_note?}
//	/bulk-actions/cod-saques/producer/reject      body {ids:[], admin_note?}
//	/bulk-actions/cod-saques/affiliate/approve    body {ids:[], admin_note?}
//	/bulk-actions/cod-saques/affiliate/reject     body {ids:[], admin_note?}
//	/bulk-actions/onboarding/approve              body {ids:[], notes?}
//	/bulk-actions/onboarding/reject               body {ids:[], notes}      (notes obrigatório)
//	/bulk-actions/orders/cancel                   body {ids:[]}             (ids = sz_motoboy_pedidos.id)
//	/bulk-actions/orders/reschedule               body {ids:[], data:"YYYY-MM-DD"}
package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/auth"
	"github.com/senderzz/admin-service/internal/httpx"
)

// BulkQueuesHandler lida com ações em lote nas filas de decisão do admin.
type BulkQueuesHandler struct{ Pool *pgxpool.Pool }

func (h *BulkQueuesHandler) tableExists(ctx context.Context, name string) bool {
	return tableExistsCached(ctx, h.Pool, name)
}

// bulkMaxIDs — teto de ids por requisição (paridade com bulk_actions.go).
const bulkMaxIDs = 100

// bulkIDsBody — payload comum das ações em lote sobre filas.
// Notes/AdminNote/Data são opcionais conforme a operação.
type bulkIDsBody struct {
	IDs       []int64 `json:"ids"`
	AdminNote string  `json:"admin_note"`
	Notes     string  `json:"notes"`
	Data      string  `json:"data"` // YYYY-MM-DD (apenas reschedule)
}

// bulkItemResult — resultado por id. Status ∈ {ok, skipped, error}.
// Skipped = já estava no estado-alvo / fora do estado elegível (idempotência).
type bulkItemResult struct {
	ID      int64  `json:"id"`
	Status  string `json:"status"` // ok | skipped | error
	Message string `json:"message,omitempty"`
}

// bulkSummary — agregado devolvido ao front (mesma forma para todas as ações).
type bulkSummary struct {
	OK      int              `json:"ok"`
	Skipped int              `json:"skipped"`
	Errors  int              `json:"errors"`
	Results []bulkItemResult `json:"results"`
}

func (s *bulkSummary) add(id int64, status, msg string) {
	s.Results = append(s.Results, bulkItemResult{ID: id, Status: status, Message: msg})
	switch status {
	case "ok":
		s.OK++
	case "skipped":
		s.Skipped++
	default:
		s.Errors++
	}
}

// decodeBulk lê o body comum e valida o cap de ids. Devolve false (e já respondeu)
// quando o corpo é inválido, sem ids, ou acima do teto.
func (h *BulkQueuesHandler) decodeBulk(w http.ResponseWriter, r *http.Request) (*bulkIDsBody, bool) {
	var b bulkIDsBody
	if err := httpx.DecodeJSON(r, &b); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return nil, false
	}
	// Dedup defensivo + descarta ids inválidos (<=0).
	seen := map[int64]bool{}
	clean := b.IDs[:0]
	for _, id := range b.IDs {
		if id > 0 && !seen[id] {
			seen[id] = true
			clean = append(clean, id)
		}
	}
	b.IDs = clean
	if len(b.IDs) == 0 {
		httpx.Err(w, 400, "bad_request", "ids não pode ser vazio")
		return nil, false
	}
	if len(b.IDs) > bulkMaxIDs {
		httpx.Err(w, 400, "bad_request", "máximo de 100 ids por requisição")
		return nil, false
	}
	return &b, true
}

// adminIDFromCtx — id do admin autenticado (0 se ausente; middleware já barra 401).
func adminIDFromCtx(ctx context.Context) int64 {
	if a := auth.FromCtx(ctx); a != nil {
		return a.ID
	}
	return 0
}

// ─── Saques produtor (sz_cod_withdrawals) ────────────────────────────────────

// CodSaquesProducerMarkPaid — marca N saques de produtor como pagos (idempotente).
// Espelha CodSaquesHandler.MarkProducerPaid: SELECT … FOR UPDATE + guard status<>'paid'.
// POST /bulk-actions/cod-saques/producer/mark-paid  body {ids:[], admin_note?}
func (h *BulkQueuesHandler) CodSaquesProducerMarkPaid(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	b, ok := h.decodeBulk(w, r)
	if !ok {
		return
	}
	if !h.tableExists(ctx, "sz_cod_withdrawals") {
		httpx.Err(w, 503, "table_missing", "tabela sz_cod_withdrawals não migrada")
		return
	}
	decidedBy := adminIDFromCtx(ctx)
	out := bulkSummary{Results: []bulkItemResult{}}
	for _, id := range b.IDs {
		var paidID int64 = id
		err := h.runTx(ctx, func(tx pgx.Tx) (string, string, error) {
			var status string
			if e := tx.QueryRow(ctx,
				`SELECT COALESCE(status,'') FROM sz_cod_withdrawals WHERE id=$1 FOR UPDATE`, id).Scan(&status); e != nil {
				if e == pgx.ErrNoRows {
					return "error", "saque não encontrado", nil
				}
				return "", "", e
			}
			tag, e := tx.Exec(ctx,
				`UPDATE sz_cod_withdrawals
				 SET status='paid', proof_url=NULLIF($1,''), admin_note=NULLIF($2,''),
				     decided_by=NULLIF($4::bigint,0), decided_at=NOW(),
				     completed_at=NOW(), updated_at=NOW()
				 WHERE id=$3 AND status <> 'paid'`,
				"", b.AdminNote, id, decidedBy)
			if e != nil {
				return "", "", e
			}
			if tag.RowsAffected() == 0 {
				return "skipped", "já pago", nil
			}
			// AUDIT-2026-07-30 CRITICAL: removido o UPDATE que flipava a tx de débito
			// pra 'analysis'/'released'. Filtro nem batia (débito é inserido com
			// status='available', nunca 'analysis' — bulk não fazia nada há tempos)
			// e, pior, o mecanismo em si é o mesmo bug do handler singular
			// (cod_saques.go MarkProducerPaid): Summary soma status='available'
			// incluindo o débito negativo; tirar o débito da soma ELEVA o disponível
			// de volta pelo valor pago. Fix real (espelha o handler singular): não
			// tocar no status da tx de débito ao pagar — ela fica 'available' pra
			// sempre, debitando o disponível pra sempre (dinheiro realmente saiu).
			return "ok", "", nil
		})
		h.recordTxResult(&out, id, err)
		if err.err == nil && err.status == "ok" {
			// Espelha o handler singular: notifica produtor (best-effort, não desfaz pagamento).
			go notifyWithdrawalPaid(h.Pool, paidID)
		}
	}
	httpx.JSON(w, 200, out)
}

// CodSaquesProducerReject — recusa N saques de produtor (idempotente).
// Espelha RejectProducer: guard status NOT IN ('paid','rejected').
// POST /bulk-actions/cod-saques/producer/reject  body {ids:[], admin_note?}
func (h *BulkQueuesHandler) CodSaquesProducerReject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	b, ok := h.decodeBulk(w, r)
	if !ok {
		return
	}
	if !h.tableExists(ctx, "sz_cod_withdrawals") {
		httpx.Err(w, 503, "table_missing", "tabela sz_cod_withdrawals não migrada")
		return
	}
	decidedBy := adminIDFromCtx(ctx)
	hasTxTable := h.tableExists(ctx, "sz_cod_wallet_transactions")
	out := bulkSummary{Results: []bulkItemResult{}}
	for _, id := range b.IDs {
		outcome := h.runTx(ctx, func(tx pgx.Tx) (string, string, error) {
			tag, e := tx.Exec(ctx,
				`UPDATE sz_cod_withdrawals
				 SET status='rejected', admin_note=NULLIF($1,''),
				     decided_by=NULLIF($3::bigint,0), decided_at=NOW(),
				     completed_at=NOW(), updated_at=NOW()
				 WHERE id=$2 AND status NOT IN ('paid','rejected')`,
				b.AdminNote, id, decidedBy)
			if e != nil {
				return "", "", e
			}
			if tag.RowsAffected() == 0 {
				return "skipped", "não encontrado ou já decidido", nil
			}
			// AUDIT-2026-07-30: faltava o estorno da tx de débito (singular
			// RejectProducer estorna via status='reversed'). Sem isso, recusar um
			// saque deixava o disponível permanentemente debitado — o produtor
			// perdia o valor mesmo sem receber (dinheiro nunca saiu de fato).
			if hasTxTable {
				_, _ = tx.Exec(ctx,
					`UPDATE sz_cod_wallet_transactions
					 SET status='reversed', updated_at=NOW()
					 WHERE user_id=(SELECT user_id FROM sz_cod_withdrawals WHERE id=$1)
					   AND type='withdrawal' AND status='available'
					   AND description LIKE '%#'||$1::text`, id)
			}
			return "ok", "", nil
		})
		h.recordTxResult(&out, id, outcome)
	}
	httpx.JSON(w, 200, out)
}

// ─── Saques afiliado (senderzz_affiliate_withdrawals) ────────────────────────

// CodSaquesAffiliateApprove — aprova N saques de afiliado.
// Replica INTEGRALMENTE ApproveAffiliate por id (operação que DEBITA a carteira):
//   1. SELECT withdrawal FOR UPDATE + checa estado elegível;
//   2. SELECT wallet FOR UPDATE; valida balance >= amount;
//   3. debita wallet; 4. insere tx de saída; 5. marca approved.
// Saldo insuficiente em um id = aquele id entra em "errors"; o lote prossegue.
// POST /bulk-actions/cod-saques/affiliate/approve  body {ids:[], admin_note?}
func (h *BulkQueuesHandler) CodSaquesAffiliateApprove(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	b, ok := h.decodeBulk(w, r)
	if !ok {
		return
	}
	if !h.tableExists(ctx, "senderzz_affiliate_withdrawals") ||
		!h.tableExists(ctx, "senderzz_affiliate_wallet") ||
		!h.tableExists(ctx, "senderzz_affiliate_transactions") {
		httpx.Err(w, 503, "tables_missing", "tabelas de afiliado ainda não migradas")
		return
	}
	adminID := adminIDFromCtx(ctx)

	out := bulkSummary{Results: []bulkItemResult{}}
	for _, id := range b.IDs {
		err := h.runTx(ctx, func(tx pgx.Tx) (string, string, error) {
			// 1. Carrega + bloqueia saque.
			var affiliateID int64
			var amount float64
			var status string
			if e := tx.QueryRow(ctx,
				`SELECT affiliate_id, COALESCE(amount,0), COALESCE(status,'')
				 FROM senderzz_affiliate_withdrawals WHERE id=$1 FOR UPDATE`, id).
				Scan(&affiliateID, &amount, &status); e != nil {
				if e == pgx.ErrNoRows {
					return "error", "saque não encontrado", nil
				}
				return "", "", e
			}
			if status != "pending" && status != "analysis" && status != "em_analise" {
				return "skipped", "já decidido", nil
			}
			// 2. Bloqueia carteira + recalcula saldo vivo (evita stale cache).
			var balance float64
			if e := tx.QueryRow(ctx,
				`SELECT GREATEST(0, COALESCE(
				    (SELECT SUM(CASE WHEN t.type='penalty' THEN -ABS(t.amount) ELSE t.amount END)
				       FROM senderzz_affiliate_transactions t
				      WHERE t.affiliate_id = $1
				        AND t.status NOT IN ('pending','cancelled')), 0))
				 FROM senderzz_affiliate_wallet
				 WHERE affiliate_id=$1 FOR UPDATE`, affiliateID).Scan(&balance); e != nil {
				if e == pgx.ErrNoRows {
					return "error", "carteira do afiliado não encontrada", nil
				}
				return "", "", e
			}
			if balance < amount {
				return "error", "saldo insuficiente", nil
			}
			// 3. Debita — usa saldo vivo calculado acima.
			if _, e := tx.Exec(ctx,
				`UPDATE senderzz_affiliate_wallet
				 SET balance = GREATEST(0, $2::numeric - $1::numeric), updated_at=NOW()
				 WHERE affiliate_id=$3`, amount, balance, affiliateID); e != nil {
				return "", "", e
			}
			// 4. Insere tx de saída.
			if _, e := tx.Exec(ctx,
				`INSERT INTO senderzz_affiliate_transactions
				   (affiliate_id, type, status, amount, available_at, meta_json, created_at)
				 VALUES ($1, 'withdrawal', 'paid', -$2::numeric, NOW(),
				         jsonb_build_object('source','admin_bulk_approve_saque','withdrawal_id',$3),
				         NOW())`, affiliateID, amount, id); e != nil {
				return "", "", e
			}
			// 5. Marca approved.
			if _, e := tx.Exec(ctx,
				`UPDATE senderzz_affiliate_withdrawals
				 SET status='approved', decided_at=NOW(), decided_by=NULLIF($1,0), admin_note=NULLIF($2,'')
				 WHERE id=$3`, adminID, b.AdminNote, id); e != nil {
				return "", "", e
			}
			return "ok", "", nil
		})
		h.recordTxResult(&out, id, err)
	}
	httpx.JSON(w, 200, out)
}

// CodSaquesAffiliateReject — recusa N saques de afiliado (idempotente).
// Espelha RejectAffiliate: guard status IN ('pending','analysis','em_analise').
// POST /bulk-actions/cod-saques/affiliate/reject  body {ids:[], admin_note?}
func (h *BulkQueuesHandler) CodSaquesAffiliateReject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	b, ok := h.decodeBulk(w, r)
	if !ok {
		return
	}
	if !h.tableExists(ctx, "senderzz_affiliate_withdrawals") {
		httpx.Err(w, 503, "table_missing", "tabela senderzz_affiliate_withdrawals não migrada")
		return
	}
	adminID := adminIDFromCtx(ctx)

	out := bulkSummary{Results: []bulkItemResult{}}
	for _, id := range b.IDs {
		tag, err := h.Pool.Exec(ctx,
			`UPDATE senderzz_affiliate_withdrawals
			 SET status='rejected', decided_at=NOW(), decided_by=NULLIF($1,0), admin_note=NULLIF($2,'')
			 WHERE id=$3 AND status IN ('pending','analysis','em_analise')`,
			adminID, b.AdminNote, id)
		if err != nil {
			out.add(id, "error", err.Error())
			continue
		}
		if tag.RowsAffected() == 0 {
			out.add(id, "skipped", "não encontrado ou já decidido")
			continue
		}
		out.add(id, "ok", "")
	}
	httpx.JSON(w, 200, out)
}

// ─── Onboarding (senderzz_onboarding_requests) ───────────────────────────────

// OnboardingApprove — aprova N solicitações de onboarding.
// Replica os efeitos de OnboardingHandler.Approve por id (cria shipping class +
// portal_user + markup), com guard de idempotência: já-aprovada → "skipped".
// POST /bulk-actions/onboarding/approve  body {ids:[], notes?}
func (h *BulkQueuesHandler) OnboardingApprove(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	b, ok := h.decodeBulk(w, r)
	if !ok {
		return
	}
	if !h.tableExists(ctx, "senderzz_onboarding_requests") {
		httpx.Err(w, 503, "table_missing", "tabela senderzz_onboarding_requests ausente")
		return
	}
	// Reusa a handler singular como motor canônico dos efeitos colaterais (shipping
	// class + portal_user + markup) — evita duplicar essa lógica e mantê-la em sincronia.
	onb := &OnboardingHandler{Pool: h.Pool}
	notes := strings.TrimSpace(b.Notes)

	out := bulkSummary{Results: []bulkItemResult{}}
	for _, id := range b.IDs {
		// Guard de idempotência ANTES de agir: já-aprovada → skipped (não reprocessa
		// efeitos colaterais). Linha ausente → error.
		var status string
		if e := h.Pool.QueryRow(ctx,
			`SELECT COALESCE(status,'') FROM senderzz_onboarding_requests WHERE id=$1`, id).Scan(&status); e != nil {
			if e == pgx.ErrNoRows {
				out.add(id, "error", "solicitação não encontrada")
			} else {
				out.add(id, "error", e.Error())
			}
			continue
		}
		if status == "approved" {
			out.add(id, "skipped", "já aprovada")
			continue
		}
		if err := onb.approveCore(ctx, id, notes); err != nil {
			out.add(id, "error", err.Error())
			continue
		}
		out.add(id, "ok", "")
	}
	httpx.JSON(w, 200, out)
}

// OnboardingReject — recusa N solicitações (idempotente).
// Espelha OnboardingHandler.Reject: notes obrigatório, guard status='pending'.
// POST /bulk-actions/onboarding/reject  body {ids:[], notes}
func (h *BulkQueuesHandler) OnboardingReject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	b, ok := h.decodeBulk(w, r)
	if !ok {
		return
	}
	notes := strings.TrimSpace(b.Notes)
	if notes == "" {
		httpx.Err(w, 400, "validation", "motivo (notes) é obrigatório para rejeição")
		return
	}
	if !h.tableExists(ctx, "senderzz_onboarding_requests") {
		httpx.Err(w, 503, "table_missing", "tabela senderzz_onboarding_requests ausente")
		return
	}
	out := bulkSummary{Results: []bulkItemResult{}}
	for _, id := range b.IDs {
		tag, err := h.Pool.Exec(ctx,
			`UPDATE senderzz_onboarding_requests
			 SET status='rejected', notes=$2
			 WHERE id=$1 AND status='pending'`, id, notes)
		if err != nil {
			out.add(id, "error", err.Error())
			continue
		}
		if tag.RowsAffected() == 0 {
			out.add(id, "skipped", "não está pendente")
			continue
		}
		out.add(id, "ok", "")
	}
	httpx.JSON(w, 200, out)
}

// ─── Pedidos motoboy (sz_motoboy_pedidos) ────────────────────────────────────

// OrdersCancel — cancela N pedidos motoboy (idempotente).
// ids = sz_motoboy_pedidos.id (consistente com OrdersHandler.Cancelar singular).
// Guard de idempotência: já 'cancelado' → "skipped". Registra audit por pedido.
// POST /bulk-actions/orders/cancel  body {ids:[]}
func (h *BulkQueuesHandler) OrdersCancel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	b, ok := h.decodeBulk(w, r)
	if !ok {
		return
	}
	if !h.tableExists(ctx, "sz_motoboy_pedidos") {
		httpx.Err(w, 503, "tables_missing", "sz_motoboy_pedidos ainda não migrada")
		return
	}
	out := bulkSummary{Results: []bulkItemResult{}}
	for _, id := range b.IDs {
		var deStatus sql.NullString
		if e := h.Pool.QueryRow(ctx,
			`SELECT status FROM sz_motoboy_pedidos WHERE id=$1`, id).Scan(&deStatus); e != nil {
			if e == pgx.ErrNoRows {
				out.add(id, "error", "pedido motoboy não encontrado")
			} else {
				out.add(id, "error", e.Error())
			}
			continue
		}
		tx, err := h.Pool.Begin(ctx)
		if err != nil {
			out.add(id, "error", err.Error())
			continue
		}
		tag, err := tx.Exec(ctx,
			`UPDATE sz_motoboy_pedidos
			 SET status='cancelado', updated_at=NOW()
			 WHERE id=$1 AND status <> 'cancelado'`, id)
		if err != nil {
			tx.Rollback(ctx)
			out.add(id, "error", err.Error())
			continue
		}
		if tag.RowsAffected() == 0 {
			tx.Rollback(ctx)
			out.add(id, "skipped", "já cancelado")
			continue
		}
		// BRIDGE motoboy → sz_orders (mesma tx). Mesmo bug de OrdersHandler.Cancelar
		// singular (pedido 1605/Isaias): sem isso o painel mostra "Cancelado" mas
		// sz_orders.status fica preso em pending.
		if szStatus, mapped := forceMotoboyStatusToSzOrder("cancelado"); mapped {
			if _, err := tx.Exec(ctx, `
				UPDATE sz_orders o
				   SET status = $2, updated_at = NOW()
				  FROM sz_motoboy_pedidos p
				 WHERE p.id = $1
				   AND COALESCE(o.wp_order_id, o.id) = p.wc_order_id
				   AND o.status <> $2`,
				id, szStatus,
			); err != nil {
				tx.Rollback(ctx)
				out.add(id, "error", err.Error())
				continue
			}
		}
		if err := tx.Commit(ctx); err != nil {
			out.add(id, "error", err.Error())
			continue
		}
		h.writeMotoboyAudit(ctx, id, "cancelar", deStatus, "cancelado", nil)
		out.add(id, "ok", "")
	}
	httpx.JSON(w, 200, out)
}

// OrdersReschedule — reagenda N pedidos motoboy para a MESMA data (idempotente
// por natureza — gravar a mesma data de novo é seguro). Espelha a regra de
// OrdersHandler.Reagendar: > 5 dias úteis → 'pre_agendado', senão 'agendado'.
// POST /bulk-actions/orders/reschedule  body {ids:[], data:"YYYY-MM-DD"}
func (h *BulkQueuesHandler) OrdersReschedule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	b, ok := h.decodeBulk(w, r)
	if !ok {
		return
	}
	chosen, perr := time.Parse("2006-01-02", strings.TrimSpace(b.Data))
	if perr != nil {
		httpx.Err(w, 400, "bad_request", "data inválida — use o formato YYYY-MM-DD")
		return
	}
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if chosen.Before(today) {
		httpx.Err(w, 400, "bad_request", "data no passado não permitida")
		return
	}
	if !h.tableExists(ctx, "sz_motoboy_pedidos") {
		httpx.Err(w, 503, "tables_missing", "sz_motoboy_pedidos ainda não migrada")
		return
	}
	// Threshold compartilhado por todo o lote (data única).
	threshold := addBusinessDays(today, 5)
	newStatus := "agendado"
	if chosen.After(threshold) {
		newStatus = "pre_agendado"
	}

	out := bulkSummary{Results: []bulkItemResult{}}
	for _, id := range b.IDs {
		var deStatus sql.NullString
		if e := h.Pool.QueryRow(ctx,
			`SELECT status FROM sz_motoboy_pedidos WHERE id=$1`, id).Scan(&deStatus); e != nil {
			if e == pgx.ErrNoRows {
				out.add(id, "error", "pedido motoboy não encontrado")
			} else {
				out.add(id, "error", e.Error())
			}
			continue
		}
		tx, err := h.Pool.Begin(ctx)
		if err != nil {
			out.add(id, "error", err.Error())
			continue
		}
		tag, err := tx.Exec(ctx,
			`UPDATE sz_motoboy_pedidos
			 SET reagendado_para=$1, status=$2, updated_at=NOW()
			 WHERE id=$3`, b.Data, newStatus, id)
		if err != nil {
			tx.Rollback(ctx)
			out.add(id, "error", err.Error())
			continue
		}
		if tag.RowsAffected() == 0 {
			tx.Rollback(ctx)
			out.add(id, "error", "pedido motoboy não encontrado")
			continue
		}
		// BRIDGE reverso: reagendar pedido frustrado/cancelado reativa o motoboy
		// mas sz_orders.status ficava preso — reset p/ 'pending'.
		if deStatus.Valid && (deStatus.String == "frustrado" || deStatus.String == "cancelado") {
			if _, err := tx.Exec(ctx, `
				UPDATE sz_orders o
				   SET status = 'pending', updated_at = NOW()
				  FROM sz_motoboy_pedidos p
				 WHERE p.id = $1
				   AND COALESCE(o.wp_order_id, o.id) = p.wc_order_id
				   AND o.status <> 'pending'`,
				id,
			); err != nil {
				tx.Rollback(ctx)
				out.add(id, "error", err.Error())
				continue
			}
		}
		if err := tx.Commit(ctx); err != nil {
			out.add(id, "error", err.Error())
			continue
		}
		h.writeMotoboyAudit(ctx, id, "reagendar", deStatus, newStatus,
			map[string]any{"reagendado_para": b.Data})
		out.add(id, "ok", "")
	}
	httpx.JSON(w, 200, out)
}

// ─── Infra compartilhada ─────────────────────────────────────────────────────

// runTx roda fn numa transação dedicada. fn devolve (status, msg, err):
//   - err != nil  → ROLLBACK, item vira "error" com o erro técnico;
//   - status="ok" → COMMIT (se o commit falhar, vira "error");
//   - demais (skipped/error de regra) → ROLLBACK limpo, status preservado.
// O sentinela txOutcome carrega (status, msg) para fora sem confundir com err real.
type txOutcome struct {
	status string
	msg    string
	err    error
}

func (h *BulkQueuesHandler) runTx(ctx context.Context, fn func(pgx.Tx) (string, string, error)) txOutcome {
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		return txOutcome{err: err}
	}
	defer tx.Rollback(ctx) //nolint:errcheck — no-op após Commit

	status, msg, ferr := fn(tx)
	if ferr != nil {
		return txOutcome{err: ferr}
	}
	if status == "ok" {
		if cerr := tx.Commit(ctx); cerr != nil {
			return txOutcome{err: cerr}
		}
	}
	// skipped/error-de-regra: rollback (defer) sem commit — nada a persistir.
	return txOutcome{status: status, msg: msg}
}

// recordTxResult traduz um txOutcome em uma linha do summary.
func (h *BulkQueuesHandler) recordTxResult(out *bulkSummary, id int64, o txOutcome) {
	if o.err != nil {
		out.add(id, "error", o.err.Error())
		return
	}
	out.add(id, o.status, o.msg)
}

// writeMotoboyAudit — registra ação em sz_motoboy_audit (best-effort), espelhando
// OrdersHandler.writeMotoboyAudit (source distinto para rastrear a origem em lote).
func (h *BulkQueuesHandler) writeMotoboyAudit(ctx context.Context, pedidoID int64, acao string, deStatus sql.NullString, paraStatus string, meta map[string]any) {
	if !h.tableExists(ctx, "sz_motoboy_audit") {
		return
	}
	if meta == nil {
		meta = map[string]any{}
	}
	var actorID *int64
	if adm := auth.FromCtx(ctx); adm != nil {
		tmp := adm.ID
		actorID = &tmp
		meta["admin_email"] = adm.Email
		meta["admin_nome"] = adm.Nome
	}
	meta["source"] = "admin_bulk_orders"
	metaJSON, _ := json.Marshal(meta)
	var de any
	if deStatus.Valid {
		de = deStatus.String
	}
	_, _ = h.Pool.Exec(ctx,
		`INSERT INTO sz_motoboy_audit
		   (pedido_id, motoboy_id, actor_tipo, actor_id,
		    acao, de_status, para_status, meta_json, created_at)
		 VALUES ($1, NULL, 'admin', $2, $3, $4, $5, $6, NOW())`,
		pedidoID, actorID, acao, de, paraStatus, string(metaJSON),
	)
}
