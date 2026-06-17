// Package handlers — endpoints admin para saques (COD/Produtor + Afiliado).
//
// Espelha tab_fin_saques() em src/Admin/Unified_Menu.php:1529 + sz_cod_admin_page()
// em includes/senderzz-cod-wallet.php:858 (PHP legado) sobre Postgres.
//
// Surface:
//   GET  /cod-saques/producer                    — lista sz_cod_withdrawals
//   POST /cod-saques/producer/{id}/mark-paid     — completa saque (proof_url + admin_note)
//   POST /cod-saques/producer/{id}/reject        — recusa saque
//   POST /cod-saques/producer/{id}/upload-proof  — upload multipart do comprovante
//   GET  /cod-saques/affiliate                   — lista senderzz_affiliate_withdrawals
//   POST /cod-saques/affiliate/{id}/approve      — aprova (transação: debita wallet + insere tx)
//   POST /cod-saques/affiliate/{id}/reject       — recusa
//   GET  /cod-saques/global-rules                — lê senderzz_options (5 chaves)
//   POST /cod-saques/global-rules                — UPSERT senderzz_options
//   GET  /cod-saques/producer/overrides          — lista overrides COD por produtor
//   POST /cod-saques/producer/overrides          — salva overrides COD por produtor
package handlers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/auth"
	"github.com/senderzz/admin-service/internal/httpx"
)

type CodSaquesHandler struct{ Pool *pgxpool.Pool }

// ─── Tipos de resposta ───────────────────────────────────────────────────

// ProducerWithdrawal — linha de sz_cod_withdrawals + email do dono (portal_users).
type ProducerWithdrawal struct {
	ID          int64    `json:"id"`
	UserID      int64    `json:"user_id"`
	UserEmail   string   `json:"user_email"`
	Amount      float64  `json:"amount"`
	Fee         float64  `json:"fee"`
	Net         float64  `json:"net"`
	PixKey      string   `json:"pix_key"`
	PixType     string   `json:"pix_type"`
	HolderName  string   `json:"holder_name"`
	HolderCPF   string   `json:"holder_cpf"`
	Status      string   `json:"status"`
	AdminNote   *string  `json:"admin_note"`
	ProofURL    *string  `json:"proof_url"`
	CompletedAt *string  `json:"completed_at"`
	CreatedAt   string   `json:"created_at"`
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
	DecidedAt     *string `json:"decided_at"`
	DecidedBy     *int64  `json:"decided_by"`
	CreatedAt     string  `json:"created_at"`
}

// GlobalRules — espelha senderzz_cod_finance_settings + sz_admin_motoboy_fee + sz_admin_operational_fund_fee.
type GlobalRules struct {
	RetentionDays       int     `json:"retention_days"`
	WithdrawFee         float64 `json:"withdraw_fee"`
	AnticipationFeePct  float64 `json:"anticipation_fee_pct"`
	MotoboyFee          float64 `json:"motoboy_fee"`
	OperationalFundFee  float64 `json:"operational_fund_fee"`
}

// ─── Helpers ─────────────────────────────────────────────────────────────

// tableExists igual ao padrão de audit.go — checa schema "public".
func (h *CodSaquesHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
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

	tag, err := h.Pool.Exec(ctx,
		`UPDATE sz_cod_withdrawals
		 SET status='paid',
		     proof_url=NULLIF($1,''),
		     admin_note=NULLIF($2,''),
		     completed_at=NOW(),
		     updated_at=NOW()
		 WHERE id=$3 AND status <> 'paid'`,
		body.ProofURL, body.AdminNote, id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.Err(w, 404, "not_found", "saque não encontrado ou já pago")
		return
	}

	// Replica do PHP: marca a tx de withdrawal correspondente como paid.
	// Best-effort — não bloqueia a resposta se a tabela ainda não existe.
	if h.tableExists(ctx, "sz_cod_wallet_transactions") {
		_, _ = h.Pool.Exec(ctx,
			`UPDATE sz_cod_wallet_transactions
			 SET status='paid', updated_at=NOW()
			 WHERE user_id=(SELECT user_id FROM sz_cod_withdrawals WHERE id=$1)
			   AND type='withdrawal' AND status='analysis'`, id)
	}

	// TODO: notificar produtor por e-mail (espelha sz_cod_notify_user() do PHP).
	// Infraestrutura SMTP não existe neste serviço Go — implementar quando houver
	// pacote de envio de e-mail (sz_cod_admin_complete_withdrawal, linha 857 de senderzz-cod-wallet.php).
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

	tag, err := h.Pool.Exec(ctx,
		`UPDATE sz_cod_withdrawals
		 SET status='rejected',
		     admin_note=NULLIF($1,''),
		     completed_at=NOW(),
		     updated_at=NOW()
		 WHERE id=$2 AND status NOT IN ('paid','rejected')`,
		body.AdminNote, id)
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
		        w.decided_at::text,
		        w.decided_by,
		        w.created_at::text
		 FROM senderzz_affiliate_withdrawals w
		 LEFT JOIN senderzz_affiliates  a ON a.id = w.affiliate_id
		 LEFT JOIN senderzz_portal_users p ON p.id = a.afiliado_id
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
			&aw.Status, &aw.AdminNote,
			&aw.DecidedAt, &aw.DecidedBy, &aw.CreatedAt)
		out = append(out, aw)
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

	// 2. Lock pessimista na wallet — paridade com SELECT … FOR UPDATE do PHP.
	var balance float64
	err = tx.QueryRow(ctx,
		`SELECT COALESCE(balance,0) FROM senderzz_affiliate_wallet
		 WHERE affiliate_id=$1 FOR UPDATE`, affiliateID).Scan(&balance)
	if err != nil {
		httpx.Err(w, 409, "wallet_missing", "carteira do afiliado não encontrada")
		return
	}
	if balance < amount {
		httpx.Err(w, 409, "insufficient_balance", "saldo insuficiente para aprovar o saque")
		return
	}

	// 3. Debita wallet.
	if _, err := tx.Exec(ctx,
		`UPDATE senderzz_affiliate_wallet
		 SET balance = GREATEST(0, balance - $1), updated_at=NOW()
		 WHERE affiliate_id=$2`, amount, affiliateID); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// 4. Insere tx de saída (amount negativo, status=paid, source=admin_approve_saque).
	if _, err := tx.Exec(ctx,
		`INSERT INTO senderzz_affiliate_transactions
		   (affiliate_id, type, status, amount, available_at, meta_json, created_at)
		 VALUES ($1, 'withdrawal', 'paid', -$2, NOW(),
		         jsonb_build_object('source','admin_approve_saque','withdrawal_id',$3),
		         NOW())`, affiliateID, amount, id); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// 5. Atualiza saque para approved.
	if _, err := tx.Exec(ctx,
		`UPDATE senderzz_affiliate_withdrawals
		 SET status='approved',
		     decided_at=NOW(),
		     decided_by=NULLIF($1,0),
		     admin_note=NULLIF($2,'')
		 WHERE id=$3`, adminID, body.AdminNote, id); err != nil {
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
		     decided_by=NULLIF($1,0),
		     admin_note=NULLIF($2,'')
		 WHERE id=$3 AND status IN ('pending','analysis','em_analise')`,
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
		`SELECT value FROM senderzz_options WHERE "key"=$1`, key).Scan(&v)
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
			`INSERT INTO senderzz_options ("key", value) VALUES ($1, $2)
			 ON CONFLICT ("key") DO UPDATE SET value = EXCLUDED.value`, key, value)
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

	mime := strings.ToLower(header.Header.Get("Content-Type"))
	if !strings.HasPrefix(mime, "image/") && mime != "application/pdf" {
		httpx.Err(w, 400, "file_invalid", "comprovante precisa ser image/* ou application/pdf (recebido: "+mime+")")
		return
	}

	// Extensão segura.
	ext := strings.ToLower(filepath.Ext(header.Filename))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".heic", ".pdf":
		// OK
	default:
		if mime == "application/pdf" {
			ext = ".pdf"
		} else {
			ext = ".jpg"
		}
	}

	dir := codProofDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		httpx.Err(w, 500, "upload_error", fmt.Sprintf("mkdir uploads: %v", err))
		return
	}

	name := fmt.Sprintf("cod-proof-%d-%d%s", id, time.Now().UnixNano(), ext)
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
	UserID           int64    `json:"user_id"`
	Nome             string   `json:"nome"`
	Email            string   `json:"email"`
	RetentionDays    *int     `json:"retention_days"`    // _senderzz_cod_retention_days
	WithdrawFee      *float64 `json:"withdraw_fee"`      // _senderzz_cod_withdraw_fee
	AnticipationFee  *float64 `json:"anticipation_fee"`  // _senderzz_cod_anticipation_fee_pct
	// effective — valor em uso após aplicar fallback global
	EffRetentionDays    int     `json:"eff_retention_days"`
	EffWithdrawFee      float64 `json:"eff_withdraw_fee"`
	EffAnticipationFee  float64 `json:"eff_anticipation_fee"`
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
			UserID          int64   `json:"user_id"`
			RetentionDays   *int    `json:"retention_days"`
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
