// Package handlers — endpoints admin para saques (COD/Produtor + Afiliado).
//
// Espelha tab_fin_saques() em src/Admin/Unified_Menu.php:1529 + sz_cod_admin_page()
// em includes/senderzz-cod-wallet.php:858 (PHP legado) sobre Postgres.
//
// Surface:
//
//	GET  /cod-saques/producer                    — lista sz_cod_withdrawals
//	POST /cod-saques/producer/{id}/mark-paid     — completa saque (proof_url + admin_note)
//	POST /cod-saques/producer/{id}/reject        — recusa saque
//	POST /cod-saques/producer/{id}/upload-proof  — upload multipart do comprovante
//	GET  /cod-saques/affiliate                   — lista senderzz_affiliate_withdrawals
//	POST /cod-saques/affiliate/{id}/approve      — aprova (transação: debita wallet + insere tx)
//	POST /cod-saques/affiliate/{id}/reject       — recusa
//	GET  /cod-saques/global-rules                — lê senderzz_options (5 chaves)
//	POST /cod-saques/global-rules                — UPSERT senderzz_options
//	GET  /cod-saques/producer/overrides          — lista overrides COD por produtor
//	POST /cod-saques/producer/overrides          — salva overrides COD por produtor
package handlers

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/auth"
	"github.com/senderzz/admin-service/internal/email"
	"github.com/senderzz/admin-service/internal/httpx"
)

type CodSaquesHandler struct{ Pool *pgxpool.Pool }

// operatorAllowed mantém a fila financeira disponível ao OL sem transformar as
// rotas administrativas de saques em rotas públicas para qualquer usuário do
// portal. Admin continua autorizado para preservar o comportamento existente.
func operatorAllowed(r *http.Request) bool {
	a := auth.ActorFromCtx(r.Context())
	return a != nil && (a.Kind == auth.ActorAdmin || a.Kind == auth.ActorKind("operator") || a.Kind == auth.ActorKind("operador"))
}

func (h *CodSaquesHandler) operatorOrForbidden(w http.ResponseWriter, r *http.Request) bool {
	if operatorAllowed(r) {
		return true
	}
	httpx.Err(w, http.StatusForbidden, "forbidden", "acesso restrito ao operador logístico")
	return false
}

// Wrappers do OL: somente a fila e as decisões de saque, sem regras globais ou
// overrides de produtor.
func (h *CodSaquesHandler) ListProducerOperator(w http.ResponseWriter, r *http.Request) {
	if h.operatorOrForbidden(w, r) {
		h.ListProducer(w, r)
	}
}
func (h *CodSaquesHandler) MarkProducerPaidOperator(w http.ResponseWriter, r *http.Request) {
	if h.operatorOrForbidden(w, r) {
		h.MarkProducerPaid(w, r)
	}
}
func (h *CodSaquesHandler) RejectProducerOperator(w http.ResponseWriter, r *http.Request) {
	if h.operatorOrForbidden(w, r) {
		h.RejectProducer(w, r)
	}
}
func (h *CodSaquesHandler) UploadProducerProofOperator(w http.ResponseWriter, r *http.Request) {
	if h.operatorOrForbidden(w, r) {
		h.UploadProducerProof(w, r)
	}
}
func (h *CodSaquesHandler) ListAffiliateOperator(w http.ResponseWriter, r *http.Request) {
	if h.operatorOrForbidden(w, r) {
		h.ListAffiliate(w, r)
	}
}
func (h *CodSaquesHandler) ApproveAffiliateOperator(w http.ResponseWriter, r *http.Request) {
	if h.operatorOrForbidden(w, r) {
		h.ApproveAffiliate(w, r)
	}
}
func (h *CodSaquesHandler) RejectAffiliateOperator(w http.ResponseWriter, r *http.Request) {
	if h.operatorOrForbidden(w, r) {
		h.RejectAffiliate(w, r)
	}
}
func (h *CodSaquesHandler) UploadAffiliateProofOperator(w http.ResponseWriter, r *http.Request) {
	if h.operatorOrForbidden(w, r) {
		h.UploadAffiliateProof(w, r)
	}
}

// ─── Tipos de resposta ───────────────────────────────────────────────────

// ProducerWithdrawal — linha de sz_cod_withdrawals + email do dono (portal_users).
type ProducerWithdrawal struct {
	ID          int64   `json:"id"`
	UserID      int64   `json:"user_id"`
	UserEmail   string  `json:"user_email"`
	Amount      float64 `json:"amount"`
	Fee         float64 `json:"fee"`
	Net         float64 `json:"net"`
	PixKey      string  `json:"pix_key"`
	PixType     string  `json:"pix_type"`
	HolderName  string  `json:"holder_name"`
	HolderCPF   string  `json:"holder_cpf"`
	Status      string  `json:"status"`
	AdminNote   *string `json:"admin_note"`
	ProofURL    *string `json:"proof_url"`
	CompletedAt *string `json:"completed_at"`
	CreatedAt   string  `json:"created_at"`
}

// AffiliateWithdrawal — linha de senderzz_affiliate_withdrawals + nome do afiliado.
type AffiliateWithdrawal struct {
	ID            int64   `json:"id"`
	AffiliateID   int64   `json:"affiliate_id"`
	AffiliateName string  `json:"affiliate_name"`
	Amount        float64 `json:"amount"`
	Fee           float64 `json:"fee"`
	NetAmount     float64 `json:"net_amount"`
	PixKey        string  `json:"pix_key"`
	BankInfo      string  `json:"bank_info"`
	Status        string  `json:"status"`
	AdminNote     *string `json:"admin_note"`
	ProofURL      *string `json:"proof_url"`
	DecidedAt     *string `json:"decided_at"`
	DecidedBy     *int64  `json:"decided_by"`
	CreatedAt     string  `json:"created_at"`
}

// GlobalRules — espelha senderzz_cod_finance_settings + sz_admin_motoboy_fee + sz_admin_operational_fund_fee.
type GlobalRules struct {
	RetentionDays      int     `json:"retention_days"`
	WithdrawFee        float64 `json:"withdraw_fee"`
	AnticipationFeePct float64 `json:"anticipation_fee_pct"`
	MotoboyFee         float64 `json:"motoboy_fee"`
	OperationalFundFee float64 `json:"operational_fund_fee"`
}

// ─── Helpers ─────────────────────────────────────────────────────────────

// tableExists igual ao padrão de audit.go — checa schema "public".
func (h *CodSaquesHandler) tableExists(ctx context.Context, name string) bool {
	return tableExistsCached(ctx, h.Pool, name) // AUDIT-2026-06-18 Onda2 (go-infoschema-cache)
}

// decodeBody decodifica JSON do body em out. Retorna 400 se inválido.
func (h *CodSaquesHandler) decodeBody(w http.ResponseWriter, r *http.Request, out any) bool {
	if err := httpx.DecodeJSON(r, out); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return false
	}
	return true
}

// parseIDParam lê chi URLParam "id" como int64 positivo.
func parseIDParam(r *http.Request) (int64, bool) {
	s := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// parseListLimit lê ?limit= clampado em [1, 500], default 120 para paridade com PHP.
func parseListLimit(r *http.Request) int {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 120
	}
	if limit > 500 {
		limit = 500
	}
	return limit
}

// ─── Saques produtor ─────────────────────────────────────────────────────

// ListProducer lista sz_cod_withdrawals com JOIN em senderzz_portal_users por user_id (wp_user_id).
// GET /cod-saques/producer?status=&limit=120
func (h *CodSaquesHandler) ListProducer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_cod_withdrawals") {
		httpx.JSON(w, 200, map[string]any{"items": []ProducerWithdrawal{}, "count": 0})
		return
	}
	status := r.URL.Query().Get("status")
	limit := parseListLimit(r)

	// CASE manual replica FIELD(status, …) do MySQL: análise/pendente sai antes
	// de pago/rejeitado para o admin priorizar o que falta decidir.
	rows, err := h.Pool.Query(ctx,
		`SELECT w.id, w.user_id,
		        COALESCE(p.email,'') AS user_email,
		        COALESCE(w.amount,0), COALESCE(w.fee,0), COALESCE(w.net,0),
		        COALESCE(w.pix_key,''), COALESCE(w.pix_type,''),
		        COALESCE(w.holder_name,''), COALESCE(w.holder_cpf,''),
		        COALESCE(w.status,''),
		        w.admin_note, w.proof_url,
		        w.completed_at::text,
		        w.created_at::text
		 FROM sz_cod_withdrawals w
		 LEFT JOIN senderzz_portal_users p ON p.wp_user_id = w.user_id
		 WHERE ($1='' OR w.status=$1)
		 ORDER BY CASE w.status
		   WHEN 'analysis' THEN 1
		   WHEN 'pending'  THEN 2
		   WHEN 'paid'     THEN 3
		   WHEN 'rejected' THEN 4
		   ELSE 9 END,
		   w.id DESC
		 LIMIT $2`, status, limit)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []ProducerWithdrawal{}
	for rows.Next() {
		var pw ProducerWithdrawal
		_ = rows.Scan(&pw.ID, &pw.UserID, &pw.UserEmail,
			&pw.Amount, &pw.Fee, &pw.Net,
			&pw.PixKey, &pw.PixType, &pw.HolderName, &pw.HolderCPF,
			&pw.Status, &pw.AdminNote, &pw.ProofURL,
			&pw.CompletedAt, &pw.CreatedAt)
		out = append(out, pw)
	}

	// AUDIT-2026-06-21 #8/#18 (LGPD-PII-AUDIT): a lista de saques produtor serve chave
	// PIX, nome e CPF do titular de cada saque. Registra na trilha de accountability —
	// escopo = nº de saques retornados. Best-effort, espelha order_detail.go.
	if actor := auth.FromCtx(ctx); actor != nil {
		logPIIAccess(ctx, h.Pool, actor.ID, actor.Email, "producer", int64(len(out)),
			[]string{"pix_key", "holder_name", "holder_cpf"}, "view", r.RemoteAddr)
	} else {
		logPIIAccess(ctx, h.Pool, 0, "", "producer", int64(len(out)),
			[]string{"pix_key", "holder_name", "holder_cpf"}, "view", r.RemoteAddr)
	}

	httpx.JSON(w, 200, map[string]any{"items": out, "count": len(out)})
}

// MarkProducerPaid marca saque produtor como pago.
// POST /cod-saques/producer/{id}/mark-paid  body: {proof_url, admin_note}
func (h *CodSaquesHandler) MarkProducerPaid(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(r)
	if !ok {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_cod_withdrawals") {
		httpx.Err(w, 503, "table_missing", "tabela sz_cod_withdrawals não migrada")
		return
	}
	var body struct {
		ProofURL  string `json:"proof_url"`
		AdminNote string `json:"admin_note"`
	}
	if !h.decodeBody(w, r, &body) {
		return
	}

	// Transação com lock pessimista — espelha ApproveAffiliate (SELECT … FOR UPDATE).
	// Sem a transação o FOR UPDATE seria inócuo: o pool devolveria a conexão logo
	// após o SELECT e o lock de linha cairia antes do UPDATE rodar, abrindo janela
	// para marcar o mesmo saque como pago duas vezes em corrida.
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	// Rollback é no-op após Commit bem-sucedido.
	defer tx.Rollback(ctx)

	// 1. Carrega saque + bloqueia a linha (evita pagar duas vezes em corrida).
	var status string
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(status,'')
		 FROM sz_cod_withdrawals
		 WHERE id=$1
		 FOR UPDATE`, id).Scan(&status); err != nil {
		httpx.Err(w, 404, "not_found", "saque não encontrado")
		return
	}

	// 2. UPDATE com a guarda mantida (status <> 'paid') — idempotência fail-safe.
	// AUDIT-2026-07-30: decided_by/decided_at faltava aqui (só o path afiliado
	// gravava) — ação financeira sem rastro de qual admin pagou.
	var decidedBy any
	if actor := auth.FromCtx(ctx); actor != nil {
		decidedBy = actor.ID
	}
	tag, err := tx.Exec(ctx,
		`UPDATE sz_cod_withdrawals
		 SET status='paid',
		     proof_url=NULLIF($1::text,''),
		     admin_note=NULLIF($2::text,''),
		     decided_by=$4,
		     decided_at=NOW(),
		     completed_at=NOW(),
		     updated_at=NOW()
		 WHERE id=$3::bigint AND status <> 'paid'`,
		body.ProofURL, body.AdminNote, id, decidedBy)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.Err(w, 404, "not_found", "saque não encontrado ou já pago")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// AUDIT-2026-07-30 CRITICAL: a tx de withdrawal (débito) é inserida com
	// status='available' (wallet.go) e o Summary soma justamente status='available'
	// pra compor o saldo disponível — incluindo esse débito negativo. Flipar pra
	// 'released' aqui REMOVIA o débito da soma, e como o valor é negativo,
	// removê-lo ELEVA o disponível de volta pelo valor recém-pago (saldo
	// "reaparecia" e podia ser sacado de novo — drenagem repetível confirmada em
	// produção). 'released' não é lido em nenhum outro lugar do código pra essa
	// tabela — não fazia sentido nenhum além de causar o bug. Fix: NÃO tocar no
	// status da tx de débito ao pagar — ela permanece 'available' pra sempre,
	// debitando o disponível pra sempre (dinheiro realmente saiu). Contraste com
	// RejectProducer, que corretamente usa 'reversed' (dinheiro nunca saiu).

	// Notifica produtor por e-mail (espelha sz_cod_notify_user() do PHP). Best-effort:
	// falha de envio não desfaz o pagamento já commitado, só loga (mesmo padrão de
	// password_reset.go — e-mail nunca é o caminho crítico de uma transação financeira).
	go notifyWithdrawalPaid(h.Pool, id)

	httpx.JSON(w, 200, map[string]any{"ok": true, "id": id, "status": "paid"})
}

// RejectProducer recusa saque produtor.
// POST /cod-saques/producer/{id}/reject  body: {admin_note}
func (h *CodSaquesHandler) RejectProducer(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(r)
	if !ok {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_cod_withdrawals") {
		httpx.Err(w, 503, "table_missing", "tabela sz_cod_withdrawals não migrada")
		return
	}
	var body struct {
		AdminNote string `json:"admin_note"`
	}
	if !h.decodeBody(w, r, &body) {
		return
	}

	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer tx.Rollback(ctx)

	var decidedBy any
	if actor := auth.FromCtx(ctx); actor != nil {
		decidedBy = actor.ID
	}
	tag, err := tx.Exec(ctx,
		`UPDATE sz_cod_withdrawals
		 SET status='rejected',
		     admin_note=NULLIF($1::text,''),
		     decided_by=$3,
		     decided_at=NOW(),
		     completed_at=NOW(),
		     updated_at=NOW()
		 WHERE id=$2::bigint AND status NOT IN ('paid','rejected')`,
		body.AdminNote, id, decidedBy)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.Err(w, 404, "not_found", "saque não encontrado ou já decidido")
		return
	}

	// Estorna débito na wallet: reverte a tx de saque (status='available' → 'reversed').
	// Sem estorno o saldo fica permanentemente debitado e o produtor pode criar
	// um segundo saque gerando saldo negativo.
	// Constraint sz_cod_wallet_transactions_status_check: pending/available/released/reversed.
	if h.tableExists(ctx, "sz_cod_wallet_transactions") {
		_, _ = tx.Exec(ctx,
			`UPDATE sz_cod_wallet_transactions
			 SET status='reversed', updated_at=NOW()
			 WHERE user_id=(SELECT user_id FROM sz_cod_withdrawals WHERE id=$1)
			   AND type='withdrawal' AND status='available'
			   AND description LIKE '%#'||$1::text`,
			id)
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	httpx.JSON(w, 200, map[string]any{"ok": true, "id": id, "status": "rejected"})
}

// ─── Saques afiliado ─────────────────────────────────────────────────────

// ListAffiliate lista senderzz_affiliate_withdrawals + nome do afiliado.
// Ordem espelha PHP: FIELD(status, 'pending', 'analysis', 'em_analise', 'approved', 'rejected'), id DESC.
// GET /cod-saques/affiliate?status=&limit=120
func (h *CodSaquesHandler) ListAffiliate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "senderzz_affiliate_withdrawals") {
		httpx.JSON(w, 200, map[string]any{"items": []AffiliateWithdrawal{}, "count": 0})
		return
	}
	status := r.URL.Query().Get("status")
	limit := parseListLimit(r)

	// CASE espelha FIELD(status, ...) do MySQL — ordem fixa para "pending" sair primeiro.
	rows, err := h.Pool.Query(ctx,
		`SELECT w.id, w.affiliate_id,
		        COALESCE(NULLIF(p.nome,''), p.email, '') AS affiliate_name,
		        COALESCE(w.amount,0), COALESCE(w.fee,0), COALESCE(w.net_amount,0),
		        COALESCE(w.pix_key,''), COALESCE(w.bank_info,''),
		        COALESCE(w.status,''),
		        w.admin_note,
		        w.proof_url,
		        w.decided_at::text,
		        w.decided_by,
		        w.created_at::text
		 FROM senderzz_affiliate_withdrawals w
		 LEFT JOIN senderzz_affiliates  a ON a.id = w.affiliate_id
		 LEFT JOIN senderzz_portal_users p ON p.wp_user_id = a.afiliado_id
		 WHERE ($1='' OR w.status=$1)
		 ORDER BY CASE w.status
		   WHEN 'pending'    THEN 1
		   WHEN 'analysis'   THEN 2
		   WHEN 'em_analise' THEN 3
		   WHEN 'approved'   THEN 4
		   WHEN 'rejected'   THEN 5
		   ELSE 9 END,
		   w.id DESC
		 LIMIT $2`, status, limit)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []AffiliateWithdrawal{}
	for rows.Next() {
		var aw AffiliateWithdrawal
		_ = rows.Scan(&aw.ID, &aw.AffiliateID, &aw.AffiliateName,
			&aw.Amount, &aw.Fee, &aw.NetAmount,
			&aw.PixKey, &aw.BankInfo,
			&aw.Status, &aw.AdminNote, &aw.ProofURL,
			&aw.DecidedAt, &aw.DecidedBy, &aw.CreatedAt)
		out = append(out, aw)
	}

	// AUDIT-2026-06-21 #8/#18 (LGPD-PII-AUDIT): a lista de saques afiliado serve chave
	// PIX e dados bancários do titular de cada saque. Registra na trilha de
	// accountability — escopo = nº de saques retornados. Best-effort, espelha order_detail.go.
	if actor := auth.FromCtx(ctx); actor != nil {
		logPIIAccess(ctx, h.Pool, actor.ID, actor.Email, "affiliate", int64(len(out)),
			[]string{"pix_key", "bank_info"}, "view", r.RemoteAddr)
	} else {
		logPIIAccess(ctx, h.Pool, 0, "", "affiliate", int64(len(out)),
			[]string{"pix_key", "bank_info"}, "view", r.RemoteAddr)
	}

	httpx.JSON(w, 200, map[string]any{"items": out, "count": len(out)})
}

// ApproveAffiliate aprova saque de afiliado em transação:
//  1. SELECT wallet FOR UPDATE  (lock pessimista)
//  2. valida balance >= amount
//  3. UPDATE wallet (debita)
//  4. INSERT tx tipo=withdrawal status=available
//  5. UPDATE withdrawal status=approved
//
// POST /cod-saques/affiliate/{id}/approve  body: {admin_note}
func (h *CodSaquesHandler) ApproveAffiliate(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(r)
	if !ok {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	ctx := r.Context()
	if !h.tableExists(ctx, "senderzz_affiliate_withdrawals") ||
		!h.tableExists(ctx, "senderzz_affiliate_wallet") ||
		!h.tableExists(ctx, "senderzz_affiliate_transactions") {
		httpx.Err(w, 503, "tables_missing", "tabelas de afiliado ainda não migradas")
		return
	}
	var body struct {
		AdminNote string `json:"admin_note"`
		ProofURL  string `json:"proof_url"`
	}
	if !h.decodeBody(w, r, &body) {
		return
	}

	// decided_by sai do admin autenticado (auth.FromCtx). Sem admin = 401 (middleware barra).
	admin := auth.FromCtx(ctx)
	var adminID int64
	if admin != nil {
		adminID = admin.ID
	}

	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	// Rollback é no-op após Commit bem-sucedido.
	defer tx.Rollback(ctx)

	// 1. Carrega saque + bloqueia linha (evita aprovar duas vezes em corrida).
	var affiliateID int64
	var amount float64
	var status string
	err = tx.QueryRow(ctx,
		`SELECT affiliate_id, COALESCE(amount,0), COALESCE(status,'')
		 FROM senderzz_affiliate_withdrawals
		 WHERE id=$1
		 FOR UPDATE`, id).Scan(&affiliateID, &amount, &status)
	if err != nil {
		httpx.Err(w, 404, "not_found", "saque não encontrado")
		return
	}
	if status != "pending" && status != "analysis" && status != "em_analise" {
		httpx.Err(w, 409, "invalid_state", "saque já foi decidido")
		return
	}

	// 2. Lock pessimista na wallet + recomputa saldo vivo do livro de transações.
	// Não confia no cache (balance) — pode estar stale se a sync não rodou desde
	// o pedido de saque. Fórmula idêntica ao SyncWallet: SUM excluindo pending e
	// cancelled; penalty entra como -ABS (PHP grava positivo mas é débito).
	var balance float64
	err = tx.QueryRow(ctx,
		`SELECT GREATEST(0, COALESCE(
		    (SELECT SUM(CASE WHEN t.type='penalty' THEN -ABS(t.amount) ELSE t.amount END)
		       FROM senderzz_affiliate_transactions t
		      WHERE t.affiliate_id = $1
		        AND t.status NOT IN ('pending','cancelled')), 0))
		 FROM senderzz_affiliate_wallet
		 WHERE affiliate_id=$1 FOR UPDATE`, affiliateID).Scan(&balance)
	if err != nil {
		httpx.Err(w, 409, "wallet_missing", "carteira do afiliado não encontrada")
		return
	}
	if balance < amount {
		httpx.Err(w, 409, "insufficient_balance",
			fmt.Sprintf("saldo real insuficiente (R$ %.2f disponível, saque R$ %.2f)", balance, amount))
		return
	}

	// 3. Debita wallet — atualiza cache com saldo recém-calculado menos o saque.
	if _, err := tx.Exec(ctx,
		`UPDATE senderzz_affiliate_wallet
		 SET balance = GREATEST(0, $2::numeric - $1::numeric), updated_at=NOW()
		 WHERE affiliate_id=$3`, amount, balance, affiliateID); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// 4. Insere tx de saída (amount negativo, status=paid, source=admin_approve_saque).
	if _, err := tx.Exec(ctx,
		`INSERT INTO senderzz_affiliate_transactions
		   (affiliate_id, type, status, amount, available_at, meta_json, created_at)
		 VALUES ($1, 'withdrawal', 'paid', -$2::numeric, NOW(),
		         jsonb_build_object('source','admin_approve_saque','withdrawal_id',$3::bigint),
		         NOW())`, affiliateID, amount, id); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// 5. Atualiza saque para approved.
	if _, err := tx.Exec(ctx,
		`UPDATE senderzz_affiliate_withdrawals
		 SET status='approved',
		     decided_at=NOW(),
		     decided_by=NULLIF($1::bigint,0),
		     admin_note=NULLIF($2::text,''),
		     proof_url=NULLIF($3::text,'')
		 WHERE id=$4::bigint`, adminID, body.AdminNote, body.ProofURL, id); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	httpx.JSON(w, 200, map[string]any{"ok": true, "id": id, "status": "approved", "debited": amount})
}

// RejectAffiliate recusa saque afiliado.
// POST /cod-saques/affiliate/{id}/reject  body: {admin_note}
func (h *CodSaquesHandler) RejectAffiliate(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(r)
	if !ok {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	ctx := r.Context()
	if !h.tableExists(ctx, "senderzz_affiliate_withdrawals") {
		httpx.Err(w, 503, "table_missing", "tabela senderzz_affiliate_withdrawals não migrada")
		return
	}
	var body struct {
		AdminNote string `json:"admin_note"`
	}
	if !h.decodeBody(w, r, &body) {
		return
	}

	admin := auth.FromCtx(ctx)
	var adminID int64
	if admin != nil {
		adminID = admin.ID
	}

	tag, err := h.Pool.Exec(ctx,
		`UPDATE senderzz_affiliate_withdrawals
		 SET status='rejected',
		     decided_at=NOW(),
		     decided_by=NULLIF($1::bigint,0),
		     admin_note=NULLIF($2::text,'')
		 WHERE id=$3::bigint AND status IN ('pending','analysis','em_analise')`,
		adminID, body.AdminNote, id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.Err(w, 404, "not_found", "saque não encontrado ou já decidido")
		return
	}
	httpx.JSON(w, 200, map[string]any{"ok": true, "id": id, "status": "rejected"})
}

// ─── Regras globais ──────────────────────────────────────────────────────

// defaults retorna as regras default do PHP (`senderzz_cod_finance_settings` + admin fees).
func defaultGlobalRules() GlobalRules {
	return GlobalRules{
		RetentionDays:      7,
		WithdrawFee:        0,
		AnticipationFeePct: 0,
		MotoboyFee:         0,
		OperationalFundFee: 0,
	}
}

// readOption busca uma chave em senderzz_options. Retorna defaultVal se a chave não existe.
// "key" é quoted porque casa com o estilo defensivo adotado em cod_taxas.go.
func (h *CodSaquesHandler) readOption(ctx context.Context, key, defaultVal string) string {
	var v string
	err := h.Pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE name=$1`, key).Scan(&v)
	if err != nil {
		return defaultVal
	}
	return v
}

// optionKeys mapeia campos GlobalRules → chave em senderzz_options.
// PHP guarda 3 chaves em senderzz_cod_finance_settings (JSON) + 2 chaves planas.
// No Postgres simplificamos: 5 chaves planas com prefixo sz_cod_ + 2 já-planas.
var optionKeys = map[string]string{
	"retention_days":       "sz_cod_retention_days",
	"withdraw_fee":         "sz_cod_withdraw_fee",
	"anticipation_fee_pct": "sz_cod_anticipation_fee_pct",
	"motoboy_fee":          "sz_admin_motoboy_fee",
	"operational_fund_fee": "sz_admin_operational_fund_fee",
}

// GetGlobalRules lê as 5 chaves de senderzz_options. Tabela ausente = defaults sem erro.
// GET /cod-saques/global-rules
func (h *CodSaquesHandler) GetGlobalRules(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rules := defaultGlobalRules()
	if !h.tableExists(ctx, "senderzz_options") {
		httpx.JSON(w, 200, map[string]any{"rules": rules, "table_ready": false})
		return
	}
	// Cada chave é independente — se não existir, mantém o default já populado.
	if v := h.readOption(ctx, optionKeys["retention_days"], ""); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			rules.RetentionDays = n
		}
	}
	if v := h.readOption(ctx, optionKeys["withdraw_fee"], ""); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			rules.WithdrawFee = n
		}
	}
	if v := h.readOption(ctx, optionKeys["anticipation_fee_pct"], ""); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			rules.AnticipationFeePct = n
		}
	}
	if v := h.readOption(ctx, optionKeys["motoboy_fee"], ""); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			rules.MotoboyFee = n
		}
	}
	if v := h.readOption(ctx, optionKeys["operational_fund_fee"], ""); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			rules.OperationalFundFee = n
		}
	}
	httpx.JSON(w, 200, map[string]any{"rules": rules, "table_ready": true})
}

// SetGlobalRules UPSERT das 5 chaves. Tabela ausente = 503 com hint claro.
// POST /cod-saques/global-rules  body: GlobalRules
func (h *CodSaquesHandler) SetGlobalRules(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "senderzz_options") {
		httpx.Err(w, 503, "table_missing", "settings table not yet migrated")
		return
	}
	var body GlobalRules
	if !h.decodeBody(w, r, &body) {
		return
	}

	// Saneamento: nada negativo. Espelha max(0, ...) do PHP.
	if body.RetentionDays < 0 {
		body.RetentionDays = 0
	}
	if body.WithdrawFee < 0 {
		body.WithdrawFee = 0
	}
	if body.AnticipationFeePct < 0 {
		body.AnticipationFeePct = 0
	}
	if body.MotoboyFee < 0 {
		body.MotoboyFee = 0
	}
	if body.OperationalFundFee < 0 {
		body.OperationalFundFee = 0
	}

	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer tx.Rollback(ctx)

	upsert := func(key, value string) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO senderzz_options (name, value) VALUES ($1, $2)
			 ON CONFLICT (name) DO UPDATE SET value = EXCLUDED.value`, key, value)
		return e
	}

	pairs := []struct{ key, val string }{
		{optionKeys["retention_days"], strconv.Itoa(body.RetentionDays)},
		{optionKeys["withdraw_fee"], strconv.FormatFloat(body.WithdrawFee, 'f', 2, 64)},
		{optionKeys["anticipation_fee_pct"], strconv.FormatFloat(body.AnticipationFeePct, 'f', 2, 64)},
		{optionKeys["motoboy_fee"], strconv.FormatFloat(body.MotoboyFee, 'f', 2, 64)},
		{optionKeys["operational_fund_fee"], strconv.FormatFloat(body.OperationalFundFee, 'f', 2, 64)},
	}
	for _, p := range pairs {
		if err := upsert(p.key, p.val); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	httpx.JSON(w, 200, map[string]any{"ok": true, "rules": body})
}

// ─── Upload de comprovante ───────────────────────────────────────────────

// codProofDir retorna o diretório local onde comprovantes COD são gravados.
// Configurável via COD_PROOF_UPLOAD_PATH. Default: ./uploads/cod-proofs/
func codProofDir() string {
	if v := strings.TrimSpace(os.Getenv("COD_PROOF_UPLOAD_PATH")); v != "" {
		return v
	}
	return "./uploads/cod-proofs"
}

// codProofURL retorna o prefixo público das URLs dos comprovantes.
// Configurável via COD_PROOF_UPLOAD_URL. Default: /uploads/cod-proofs/
//
// P0-07: este prefixo é servido como estático pelo proxy/nginx (não há endpoint
// Go que serve /uploads/cod-proofs/). O proxy DEVE responder esses arquivos com
// "Content-Disposition: attachment" (nunca inline) para impedir que um comprovante
// malicioso seja renderizado no browser do admin. Espelha a convenção de custody.
func codProofURL() string {
	if v := strings.TrimSpace(os.Getenv("COD_PROOF_UPLOAD_URL")); v != "" {
		return strings.TrimRight(v, "/") + "/"
	}
	return "/uploads/cod-proofs/"
}

// UploadProducerProof recebe um comprovante (imagem ou PDF) via multipart e devolve a URL pública.
// Espelha wp_handle_upload($_FILES['proof_file']) de sz_cod_admin_complete_withdrawal() (linha 857).
//
// POST /cod-saques/producer/{id}/upload-proof  multipart: proof_file
func (h *CodSaquesHandler) UploadProducerProof(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(r)
	if !ok {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	// 32 MB de teto (imagem + campos).
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		httpx.Err(w, 400, "bad_request", "multipart inválido: "+err.Error())
		return
	}

	file, header, ferr := r.FormFile("proof_file")
	if ferr != nil {
		httpx.Err(w, 400, "file_missing", "campo proof_file obrigatório")
		return
	}
	defer file.Close()

	if header.Size > 16<<20 {
		httpx.Err(w, 413, "file_too_large", "comprovante excede 16MB")
		return
	}

	// P0-07: valida por MAGIC BYTES (não pelo Content-Type do cliente, que é
	// spoofável). http.DetectContentType lê os primeiros 512 bytes e assina o
	// tipo real. Só aceita jpeg/png/pdf — qualquer outro tipo é rejeitado (400).
	sniff := make([]byte, 512)
	n, rerr := io.ReadFull(file, sniff)
	if rerr != nil && rerr != io.ErrUnexpectedEOF && rerr != io.EOF {
		httpx.Err(w, 400, "file_invalid", "não foi possível ler o comprovante: "+rerr.Error())
		return
	}
	mime := http.DetectContentType(sniff[:n])

	// IMPORTANTE: rebobina o arquivo após o sniff, senão os 512 bytes lidos
	// somem do conteúdo gravado em disco e o comprovante fica corrompido.
	if _, serr := file.Seek(0, io.SeekStart); serr != nil {
		httpx.Err(w, 500, "upload_error", "falha ao reposicionar o arquivo: "+serr.Error())
		return
	}

	// Aceite só jpeg/png/pdf (match exato — DetectContentType devolve exatamente
	// estes para as assinaturas correspondentes). Extensão derivada do tipo real,
	// não do nome do arquivo enviado pelo cliente (também spoofável).
	var ext string
	switch mime {
	case "image/jpeg":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	case "application/pdf":
		ext = ".pdf"
	default:
		httpx.Err(w, 400, "file_invalid", "comprovante precisa ser JPEG, PNG ou PDF (detectado: "+mime+")")
		return
	}

	dir := codProofDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		httpx.Err(w, 500, "upload_error", fmt.Sprintf("mkdir uploads: %v", err))
		return
	}

	// P2-01: nome NÃO-previsível (anti-IDOR) — crypto/rand, não UnixNano.
	rb := make([]byte, 16)
	if _, err := crand.Read(rb); err != nil {
		httpx.Err(w, 500, "upload_error", "falha ao gerar nome seguro")
		return
	}
	name := fmt.Sprintf("cod-proof-%d-%s%s", id, hex.EncodeToString(rb), ext)
	full := filepath.Join(dir, name)

	dst, err := os.Create(full)
	if err != nil {
		httpx.Err(w, 500, "upload_error", fmt.Sprintf("criar arquivo: %v", err))
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		_ = os.Remove(full)
		httpx.Err(w, 500, "upload_error", fmt.Sprintf("gravar arquivo: %v", err))
		return
	}

	proofURL := codProofURL() + name
	httpx.JSON(w, 200, map[string]any{"ok": true, "proof_url": proofURL})
}

// POST /cod-saques/affiliate/{id}/upload-proof  multipart: proof_file
// Espelha UploadProducerProof mas para saques de afiliado.
func (h *CodSaquesHandler) UploadAffiliateProof(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(r)
	if !ok {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		httpx.Err(w, 400, "bad_request", "multipart inválido ou arquivo muito grande (max 16 MB)")
		return
	}
	file, header, ferr := r.FormFile("proof_file")
	if ferr != nil {
		httpx.Err(w, 400, "file_missing", "campo proof_file obrigatório")
		return
	}
	defer file.Close()
	magic := make([]byte, 512)
	n, _ := file.Read(magic)
	ct := http.DetectContentType(magic[:n])
	if _, err := file.Seek(0, 0); err != nil {
		httpx.Err(w, 500, "upload_error", "seek falhou")
		return
	}
	var ext string
	switch {
	case strings.HasPrefix(ct, "image/jpeg"):
		ext = ".jpg"
	case strings.HasPrefix(ct, "image/png"):
		ext = ".png"
	case ct == "application/pdf" || strings.HasSuffix(strings.ToLower(header.Filename), ".pdf"):
		ext = ".pdf"
	default:
		httpx.Err(w, 415, "unsupported_media", "apenas jpeg, png ou pdf são aceitos")
		return
	}
	dir := codProofDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		httpx.Err(w, 500, "upload_error", fmt.Sprintf("criar dir: %v", err))
		return
	}
	rb := make([]byte, 8)
	if _, err := crand.Read(rb); err != nil {
		httpx.Err(w, 500, "upload_error", "rand falhou")
		return
	}
	name := fmt.Sprintf("aff-proof-%d-%s%s", id, hex.EncodeToString(rb), ext)
	full := filepath.Join(dir, name)
	dst, err := os.Create(full)
	if err != nil {
		httpx.Err(w, 500, "upload_error", fmt.Sprintf("criar arquivo: %v", err))
		return
	}
	defer dst.Close()
	if _, err := io.Copy(dst, file); err != nil {
		_ = os.Remove(full)
		httpx.Err(w, 500, "upload_error", fmt.Sprintf("gravar arquivo: %v", err))
		return
	}
	proofURL := codProofURL() + name
	httpx.JSON(w, 200, map[string]any{"ok": true, "proof_url": proofURL})
}

// ─── Overrides de regras COD por produtor ───────────────────────────────

// ProducerOverrideItem — produtor com seus overrides individuais de regras COD.
// Campos nil significam "herdar global" (sem registro em senderzz_portal_user_meta).
type ProducerOverrideItem struct {
	UserID          int64    `json:"user_id"`
	Nome            string   `json:"nome"`
	Email           string   `json:"email"`
	RetentionDays   *int     `json:"retention_days"`   // _senderzz_cod_retention_days
	WithdrawFee     *float64 `json:"withdraw_fee"`     // _senderzz_cod_withdraw_fee
	AnticipationFee *float64 `json:"anticipation_fee"` // _senderzz_cod_anticipation_fee_pct
	// effective — valor em uso após aplicar fallback global
	EffRetentionDays   int     `json:"eff_retention_days"`
	EffWithdrawFee     float64 `json:"eff_withdraw_fee"`
	EffAnticipationFee float64 `json:"eff_anticipation_fee"`
}

// GetProducerOverrides lista produtores do portal com seus overrides COD individuais.
// Produtores = senderzz_portal_users WHERE role IN ('client','producer') —
// espelha sz_cod_get_product_producer_ids() / sz_cod_admin_page() linha 885 do PHP.
// Overrides lidos de senderzz_portal_user_meta com chaves _senderzz_cod_*.
//
// GET /cod-saques/producer/overrides
func (h *CodSaquesHandler) GetProducerOverrides(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if !h.tableExists(ctx, "senderzz_portal_users") {
		httpx.JSON(w, 200, map[string]any{"items": []ProducerOverrideItem{}, "table_ready": false})
		return
	}

	// Lê regras globais (fallback quando o produtor não tem override).
	globalRules := defaultGlobalRules()
	if h.tableExists(ctx, "senderzz_options") {
		if v := h.readOption(ctx, optionKeys["retention_days"], ""); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				globalRules.RetentionDays = n
			}
		}
		if v := h.readOption(ctx, optionKeys["withdraw_fee"], ""); v != "" {
			if n, err := strconv.ParseFloat(v, 64); err == nil {
				globalRules.WithdrawFee = n
			}
		}
		if v := h.readOption(ctx, optionKeys["anticipation_fee_pct"], ""); v != "" {
			if n, err := strconv.ParseFloat(v, 64); err == nil {
				globalRules.AnticipationFeePct = n
			}
		}
	}

	rows, err := h.Pool.Query(ctx,
		`SELECT id, COALESCE(nome,''), COALESCE(email,'')
		 FROM senderzz_portal_users
		 WHERE role IN ('client','producer')
		 ORDER BY id ASC`)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	type baseRow struct {
		ID    int64
		Nome  string
		Email string
	}
	users := []baseRow{}
	for rows.Next() {
		var u baseRow
		if err := rows.Scan(&u.ID, &u.Nome, &u.Email); err != nil {
			continue
		}
		users = append(users, u)
	}
	rows.Close()

	hasMeta := h.tableExists(ctx, "senderzz_portal_user_meta")

	out := []ProducerOverrideItem{}
	for _, u := range users {
		item := ProducerOverrideItem{
			UserID: u.ID,
			Nome:   u.Nome,
			Email:  u.Email,
		}

		if hasMeta {
			metaRows, err := h.Pool.Query(ctx,
				`SELECT meta_key, COALESCE(meta_value,'')
				 FROM senderzz_portal_user_meta
				 WHERE user_id=$1
				   AND meta_key IN (
				     '_senderzz_cod_retention_days',
				     '_senderzz_cod_withdraw_fee',
				     '_senderzz_cod_anticipation_fee_pct'
				   )`, u.ID)
			if err == nil {
				for metaRows.Next() {
					var k, v string
					if err := metaRows.Scan(&k, &v); err != nil {
						continue
					}
					v = strings.TrimSpace(v)
					if v == "" {
						continue
					}
					switch k {
					case "_senderzz_cod_retention_days":
						if n, err := strconv.Atoi(v); err == nil {
							item.RetentionDays = &n
						}
					case "_senderzz_cod_withdraw_fee":
						if f, err := strconv.ParseFloat(strings.ReplaceAll(v, ",", "."), 64); err == nil {
							item.WithdrawFee = &f
						}
					case "_senderzz_cod_anticipation_fee_pct":
						if f, err := strconv.ParseFloat(strings.ReplaceAll(v, ",", "."), 64); err == nil {
							item.AnticipationFee = &f
						}
					}
				}
				metaRows.Close()
			}
		}

		// Efetivo = override ou global.
		if item.RetentionDays != nil {
			item.EffRetentionDays = *item.RetentionDays
		} else {
			item.EffRetentionDays = globalRules.RetentionDays
		}
		if item.WithdrawFee != nil {
			item.EffWithdrawFee = *item.WithdrawFee
		} else {
			item.EffWithdrawFee = globalRules.WithdrawFee
		}
		if item.AnticipationFee != nil {
			item.EffAnticipationFee = *item.AnticipationFee
		} else {
			item.EffAnticipationFee = globalRules.AnticipationFeePct
		}

		out = append(out, item)
	}

	httpx.JSON(w, 200, map[string]any{"items": out, "table_ready": true})
}

// SetProducerOverrides salva overrides individuais de regras COD por produtor.
// Campo vazio / zero = deleta o override (produtor volta a herdar o global).
// Espelha o loop de update_user_meta() em senderzz-cod-wallet.php:843.
//
// POST /cod-saques/producer/overrides
// body: {items: [{user_id, retention_days, withdraw_fee, anticipation_fee_pct}]}
func (h *CodSaquesHandler) SetProducerOverrides(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if !h.tableExists(ctx, "senderzz_portal_user_meta") {
		httpx.Err(w, 503, "table_missing", "tabela senderzz_portal_user_meta não migrada")
		return
	}

	var body struct {
		Items []struct {
			UserID          int64    `json:"user_id"`
			RetentionDays   *int     `json:"retention_days"`
			WithdrawFee     *float64 `json:"withdraw_fee"`
			AnticipationFee *float64 `json:"anticipation_fee_pct"`
		} `json:"items"`
	}
	if !h.decodeBody(w, r, &body) {
		return
	}

	for _, it := range body.Items {
		if it.UserID <= 0 {
			continue
		}

		// retention_days: nil ou < 0 = deletar override.
		if it.RetentionDays != nil && *it.RetentionDays >= 0 {
			v := strconv.Itoa(*it.RetentionDays)
			if _, err := h.Pool.Exec(ctx,
				`INSERT INTO senderzz_portal_user_meta (user_id, meta_key, meta_value)
				 VALUES ($1, '_senderzz_cod_retention_days', $2)
				 ON CONFLICT (user_id, meta_key) DO UPDATE SET meta_value = EXCLUDED.meta_value`,
				it.UserID, v); err != nil {
				httpx.Err(w, 500, "db_error", err.Error())
				return
			}
		} else {
			_, _ = h.Pool.Exec(ctx,
				`DELETE FROM senderzz_portal_user_meta
				 WHERE user_id=$1 AND meta_key='_senderzz_cod_retention_days'`, it.UserID)
		}

		// withdraw_fee: nil ou <= 0 = deletar override.
		if it.WithdrawFee != nil && *it.WithdrawFee > 0 {
			v := strconv.FormatFloat(*it.WithdrawFee, 'f', 2, 64)
			if _, err := h.Pool.Exec(ctx,
				`INSERT INTO senderzz_portal_user_meta (user_id, meta_key, meta_value)
				 VALUES ($1, '_senderzz_cod_withdraw_fee', $2)
				 ON CONFLICT (user_id, meta_key) DO UPDATE SET meta_value = EXCLUDED.meta_value`,
				it.UserID, v); err != nil {
				httpx.Err(w, 500, "db_error", err.Error())
				return
			}
		} else {
			_, _ = h.Pool.Exec(ctx,
				`DELETE FROM senderzz_portal_user_meta
				 WHERE user_id=$1 AND meta_key='_senderzz_cod_withdraw_fee'`, it.UserID)
		}

		// anticipation_fee_pct: nil ou <= 0 = deletar override.
		if it.AnticipationFee != nil && *it.AnticipationFee > 0 {
			v := strconv.FormatFloat(*it.AnticipationFee, 'f', 2, 64)
			if _, err := h.Pool.Exec(ctx,
				`INSERT INTO senderzz_portal_user_meta (user_id, meta_key, meta_value)
				 VALUES ($1, '_senderzz_cod_anticipation_fee_pct', $2)
				 ON CONFLICT (user_id, meta_key) DO UPDATE SET meta_value = EXCLUDED.meta_value`,
				it.UserID, v); err != nil {
				httpx.Err(w, 500, "db_error", err.Error())
				return
			}
		} else {
			_, _ = h.Pool.Exec(ctx,
				`DELETE FROM senderzz_portal_user_meta
				 WHERE user_id=$1 AND meta_key='_senderzz_cod_anticipation_fee_pct'`, it.UserID)
		}
	}

	httpx.JSON(w, 200, map[string]any{"ok": true, "saved": len(body.Items)})
}

// notifyWithdrawalPaid envia o e-mail de "saque pago" ao produtor. Rodado em
// goroutine própria (fire-and-forget) a partir de MarkProducerPaid — usa
// context.Background() porque o request HTTP original já foi respondido antes
// do envio terminar. Sem e-mail cadastrado ou falha de SMTP: só loga (Send já
// trata isso internamente), nunca propaga erro pro chamador.
func notifyWithdrawalPaid(pool *pgxpool.Pool, withdrawalID int64) {
	ctx := context.Background()
	var userEmail string
	var net float64
	err := pool.QueryRow(ctx,
		`SELECT COALESCE(p.email,''), COALESCE(w.net,0)
		 FROM sz_cod_withdrawals w
		 LEFT JOIN senderzz_portal_users p ON p.wp_user_id = w.user_id
		 WHERE w.id=$1`, withdrawalID).Scan(&userEmail, &net)
	if err != nil || userEmail == "" {
		return
	}
	subject := "Seu saque foi pago"
	body := fmt.Sprintf(
		"<p>Seu saque de <strong>R$ %.2f</strong> foi processado e pago.</p>",
		net)
	_ = email.Send(userEmail, subject, body)
}
