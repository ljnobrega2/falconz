// Package handlers — SAQUE + ANTECIPAÇÃO da comissão do AFILIADO (Portal V2).
//
// ESPELHO do fluxo COD do produtor (wallet.go Withdraw/Anticipate), portado para
// o ledger de AFILIADO. Hoje só o produtor COD saca/antecipa; aqui o afiliado
// passa a sacar e antecipar a PRÓPRIA comissão — com os mesmos invariantes
// financeiros (locks, idempotência, dual-insert atômico, taxa server-side).
//
// Rotas (namespace /wp-json/senderzz/v1):
//
//	POST /portal/affiliate-wallet/withdraw    — solicitar saque da comissão (afiliado + flag)
//	POST /portal/affiliate-wallet/anticipate  — antecipar comissão pendente   (afiliado + flag)
//
// ── id-space CANÔNICO (porte fiel — NUNCA improvisar) ─────────────────────────
//
// A comissão do afiliado vive em senderzz_affiliate_transactions, keyed por
// affiliate_id = senderzz_affiliates.id (id do VÍNCULO, NÃO do usuário). Um
// afiliado (1 wp_user_id) pode ter VÁRIOS vínculos (1 por produtor). Por isso:
//
//   - SALDO e DUP-LOCK são SEMPRE escopados pelo afiliado via JOIN
//     senderzz_affiliates a ON a.id = tx.affiliate_id WHERE a.afiliado_id = $wp.
//     (NUNCA por um único affiliate_id — senão um saque booked sob outro vínculo
//     escaparia do dup-lock; double-spend.) affiliado_id = wp_user_id (canônico).
//
//   - A LINHA de booking (senderzz_affiliate_withdrawals + a tx de débito + a tx
//     anticipation_fee) precisa de UM affiliate_id concreto (a coluna é um único
//     bigint). Usamos o "vínculo-casa" determinístico: o vínculo ativo mais antigo
//     do afiliado (homeAffiliateLink). A escolha NÃO afeta correção — o trigger de
//     receita resolve a.afiliado_id a partir de QUALQUER vínculo do afiliado.
//
// ── REGRA FINANCEIRA (de onde / QUANDO debita) ────────────────────────────────
//
// DISPONÍVEL do afiliado = a MESMA fórmula do wallet.go Summary / dashboard:
//   GREATEST(0, SUM(CASE
//       WHEN approved AND penalty THEN -ABS(amount)   -- frustração (despesa)
//       WHEN approved             THEN amount          -- comissão + débitos (saque -)
//       ELSE 0 END))
//
// PROPRIEDADE DO DÉBITO (load-bearing — descoberto na árvore): o SAQUE do afiliado
// é debitado NA APROVAÇÃO pelo ADMIN, NÃO no pedido. Os dois caminhos de aprovação
// do admin (go/admin cod_saques.go::ApproveAffiliate e bulk_queues.go::
// CodSaquesAffiliateApprove) JÁ: travam a carteira (senderzz_affiliate_wallet FOR
// UPDATE), validam saldo, debitam o cache E inserem a tx type='withdrawal',
// amount=-bruto, status='paid'. O REJECT só vira o status (não estorna nada).
//
// LOGO o pedido de saque (este handler) cria SÓ a linha 'pending' em
// senderzz_affiliate_withdrawals — NÃO insere a tx de débito. Se inserisse, o
// approve do admin debitaria DE NOVO (double-debit) e o reject não estornaria
// (afiliado perderia o bruto). A trava de overdraft é o saldo agregado validado
// AQUI no pedido (gate) + a revalidação FOR UPDATE no approve do admin (autoridade).
// A taxa (fee>0) é capturada como receita 'taxa_saque' pelo trigger
// trg_sz_revenue_capture_affsaque quando o admin vira o saque p/ 'approved'/'paid'.
//
// PENDENTE = status='pending'. A ANTECIPAÇÃO é SELF-SERVICE (sem caminho de
// settlement no admin): libera o pendente (UPDATE pending→approved) e cobra a taxa
// % via uma tx type='anticipation_fee' amount=-(bruto×%); o líquido fica disponível.
package handlers

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/portal-service/internal/auth"
	"github.com/senderzz/portal-service/internal/httpx"
)

// AffiliateWalletHandler agrupa as deps dos handlers de saque/antecipação do
// afiliado. Construção idêntica a WalletHandler/AffiliatesHandler (só Pool).
type AffiliateWalletHandler struct {
	Pool *pgxpool.Pool
}

// ── Options de governança (senderzz_options — semeadas na migração 476) ───────

// affWithdrawMin lê o mínimo de saque do afiliado (sz_aff_withdraw_min). Default
// 10.00 (paridade com o R$10 do COD). Fail-safe: option ausente/ inválida → 10.
func (h *AffiliateWalletHandler) affWithdrawMin(r *http.Request) float64 {
	return h.optFloat(r, "sz_aff_withdraw_min", 10.0)
}

// affWithdrawFee lê a taxa FIXA de saque do afiliado (sz_aff_withdraw_fee).
// Default 2.99 (espelha sz_cod_withdraw_fee). Server-side — afiliado não edita.
func (h *AffiliateWalletHandler) affWithdrawFee(r *http.Request) float64 {
	return h.optFloat(r, "sz_aff_withdraw_fee", 2.99)
}

// affAnticipationFeePct lê a taxa % de antecipação (sz_aff_anticipation_fee_pct).
// Default 4.99 (espelha sz_cod_anticipation_fee_pct). Percentual sobre o bruto.
func (h *AffiliateWalletHandler) affAnticipationFeePct(r *http.Request) float64 {
	return h.optFloat(r, "sz_aff_anticipation_fee_pct", 4.99)
}

// optFloat lê uma option numérica (aceita "2,99"/"2.99"); fallback em erro/neg.
func (h *AffiliateWalletHandler) optFloat(r *http.Request, name string, def float64) float64 {
	var v string
	if err := h.Pool.QueryRow(r.Context(),
		`SELECT value FROM senderzz_options WHERE name = $1`, name,
	).Scan(&v); err != nil {
		return def
	}
	f, err := strconv.ParseFloat(replaceCommaDot(v), 64)
	if err != nil || f < 0 {
		return def
	}
	return f
}

// affWithdrawEnabled — flag senderzz_dashboard_v2_aff_withdraw_enabled.
// DEFAULT fail-OPEN ('yes' quando ausente): o dono quer o recurso habilitado e a
// migração semeia 'yes'. Mantém-se desligável trocando o value p/ 'no'.
func (h *AffiliateWalletHandler) affWithdrawEnabled(r *http.Request) bool {
	return h.flagOn(r, "senderzz_dashboard_v2_aff_withdraw_enabled")
}

// affAnticipateEnabled — flag senderzz_dashboard_v2_aff_anticipate_enabled.
func (h *AffiliateWalletHandler) affAnticipateEnabled(r *http.Request) bool {
	return h.flagOn(r, "senderzz_dashboard_v2_aff_anticipate_enabled")
}

// flagOn lê uma flag de senderzz_options. Ausente → true (fail-OPEN: o dono quer
// habilitado; a migração 476 semeia 'yes'). 'no'/'0'/'off'/'false' → false.
func (h *AffiliateWalletHandler) flagOn(r *http.Request, name string) bool {
	var v string
	if err := h.Pool.QueryRow(r.Context(),
		`SELECT value FROM senderzz_options WHERE name = $1`, name,
	).Scan(&v); err != nil {
		return true // ausente → habilitado (default do dono)
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "no", "0", "off", "false", "":
		return false
	default:
		return true
	}
}

// ── Lock do ledger de afiliado ────────────────────────────────────────────────

// affWalletLockKey — chave int64 estável para pg_advisory_xact_lock(bigint),
// derivada do wp_user_id do AFILIADO. Namespace "sz_aff_wallet:" isola este lock
// do COD ("sz_cod_wallet:") e dos demais. Withdraw e Anticipate do afiliado USAM
// A MESMA CHAVE: ambos consomem o mesmo guard "1 saque ativo por afiliado", logo
// precisam ser mutuamente exclusivos por afiliado (anti double-spend). P0.
func affWalletLockKey(wpUserID int64) int64 {
	hsh := fnv.New64a()
	_, _ = hsh.Write([]byte("sz_aff_wallet:" + strconv.FormatInt(wpUserID, 10)))
	return int64(hsh.Sum64()) //nolint:gosec — wrap intencional p/ caber em bigint
}

// homeAffiliateLink resolve o "vínculo-casa" do afiliado: o vínculo ativo mais
// antigo (fallback ao mais antigo de qualquer status) p/ ancorar a LINHA de
// booking (withdrawals + tx de débito/fee). A escolha NÃO afeta correção: o
// trigger de receita resolve a.afiliado_id a partir de QUALQUER vínculo. Sem
// vínculo → (0,false): o afiliado não tem ledger nenhum (logo, saldo R$0).
func (h *AffiliateWalletHandler) homeAffiliateLink(tx pgx.Tx, ctx context.Context, wpUserID int64) (int64, bool) {
	var id int64
	err := tx.QueryRow(ctx,
		`SELECT id FROM senderzz_affiliates
		  WHERE afiliado_id = $1
		  ORDER BY (status = 'active') DESC, id ASC
		  LIMIT 1`,
		wpUserID,
	).Scan(&id)
	if err != nil {
		return 0, false
	}
	return id, true
}

// ── POST /portal/affiliate-wallet/withdraw ────────────────────────────────────

// withdrawAffRequest — body opcional de saque. account_id seleciona uma conta PIX
// do afiliado (sz_cod_withdraw_accounts keyed por wp_user_id); se ausente, usa a
// conta DEFAULT. pix_key permite informar uma chave avulsa (sem conta cadastrada).
type withdrawAffRequest struct {
	AccountID int64  `json:"account_id"`
	PixKey    string `json:"pix_key"`
	Amount    string `json:"amount"` // "1.234,56" ou "1234.56"
}

// WithdrawAffiliate solicita um saque da COMISSÃO do afiliado. Espelha os guards
// do COD (wallet.go Withdraw): flag, role afiliado, escopo do PRÓPRIO afiliado,
// mínimo, dup-lock, taxa fixa server-side. SÓ cria a linha 'pending' — o débito
// é do admin no approve (ver doc do pacote). Não há dual-insert aqui.
//
// Fluxo (uma transação pgx):
//  0. advisory lock por wp_user_id (affWalletLockKey) — serializa saque+antecip.
//  1. resolve o vínculo-casa (homeAffiliateLink) DENTRO do lock
//  2. dup-lock: já existe withdrawal pending/approved DO AFILIADO → 422
//  3. saldo DISPONÍVEL (fórmula do Summary) — amount > available → 422  (gate)
//  4. taxa fixa = sz_aff_withdraw_fee; net_amount = amount - fee
//  5. INSERT senderzz_affiliate_withdrawals (status='pending') — ÚNICA escrita.
//     O débito (cache + tx type='withdrawal') é do admin no approve.
func (h *AffiliateWalletHandler) WithdrawAffiliate(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	// Gate de role: SÓ afiliado (inclui 'cliente', que tem acesso a comissões).
	if !isAffiliate(u.Role) {
		httpx.WriteErr(w, http.StatusForbidden, "Saque de comissão disponível apenas para afiliados.")
		return
	}
	// Flag (default habilitado — o dono quer ON; a migração 476 semeia 'yes').
	if !h.affWithdrawEnabled(r) {
		httpx.WriteErr(w, http.StatusForbidden, "Saque de comissão não habilitado.")
		return
	}
	if u.WPUserID <= 0 {
		// Sem wp_user_id não há vínculo de afiliado (afiliado_id = wp_user_id).
		httpx.WriteErr(w, http.StatusForbidden, "Conta sem vínculo de afiliado.")
		return
	}

	var req withdrawAffRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}

	amount := round2(parseMoney(req.Amount))
	minWithdraw := round2(h.affWithdrawMin(r))
	if amount < minWithdraw {
		httpx.WriteErr(w, http.StatusUnprocessableEntity,
			"Valor mínimo para saque é R$ "+strconv.FormatFloat(minWithdraw, 'f', 2, 64)+".")
		return
	}

	// Conta PIX: default do afiliado (sz_cod_withdraw_accounts keyed por wp_user_id)
	// ou a conta informada por account_id; senão usa a pix_key avulsa do body.
	codKey := codWalletKey(u)
	pixKey, pixType, holderCPF := h.resolveAffiliatePix(r, codKey, req.AccountID, req.PixKey)
	if strings.TrimSpace(pixKey) == "" {
		httpx.WriteErr(w, http.StatusUnprocessableEntity,
			"Cadastre uma conta PIX ou informe a chave PIX antes de sacar.")
		return
	}

	fee := round2(h.affWithdrawFee(r))

	tx, err := h.Pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		slog.Error("[aff_wallet] erro ao iniciar tx de saque", "wp_user_id", u.WPUserID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	// P0 — advisory lock por wp_user_id ANTES de qualquer leitura.
	if _, err := tx.Exec(r.Context(),
		`SELECT pg_advisory_xact_lock($1)`, affWalletLockKey(u.WPUserID),
	); err != nil {
		slog.Error("[aff_wallet] erro no advisory lock de saque", "wp_user_id", u.WPUserID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Vínculo-casa p/ ancorar a linha de booking (dentro do lock).
	homeLink, ok := h.homeAffiliateLink(tx, r.Context(), u.WPUserID)
	if !ok {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "Sem vínculo de afiliado para saque.")
		return
	}

	// Dup-lock AFILIADO-scopado (via JOIN, não pelo home link) — anti double-spend.
	var pendingWd int64
	if err := tx.QueryRow(r.Context(),
		`SELECT COUNT(*)
		   FROM senderzz_affiliate_withdrawals wd
		   JOIN senderzz_affiliates a ON a.id = wd.affiliate_id
		  WHERE a.afiliado_id = $1
		    AND wd.status IN ('pending','approved')`,
		u.WPUserID,
	).Scan(&pendingWd); err != nil {
		slog.Error("[aff_wallet] erro no dup-lock de saque", "wp_user_id", u.WPUserID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	if pendingWd > 0 {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "Já existe um saque em análise. Aguarde a conclusão.")
		return
	}

	// Saldo DISPONÍVEL (fórmula idêntica ao Summary — fonte única de verdade).
	// Sem FOR UPDATE (agregado): o advisory lock acima já serializa por afiliado.
	var available float64
	if err := tx.QueryRow(r.Context(),
		`SELECT GREATEST(0, COALESCE(SUM(CASE
		            WHEN tx.status = 'approved' AND tx.type = 'penalty' THEN -ABS(tx.amount)
		            WHEN tx.status = 'approved'                          THEN tx.amount
		            ELSE 0 END), 0))
		   FROM senderzz_affiliate_transactions tx
		   JOIN senderzz_affiliates a ON a.id = tx.affiliate_id
		  WHERE a.afiliado_id = $1`,
		u.WPUserID,
	).Scan(&available); err != nil {
		slog.Error("[aff_wallet] erro ao ler saldo disponível", "wp_user_id", u.WPUserID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	if amount <= 0 || amount > round2(available) {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "Valor indisponível para saque.")
		return
	}

	if fee > amount {
		fee = amount
	}
	net := round2(amount - fee)
	if net < 0 {
		net = 0
	}

	// ÚNICA escrita: ordem de saque (status='pending'). holder_name NÃO existe
	// nesta tabela (≠ sz_cod_withdrawals) — não inserimos. created_at/requested_at
	// têm default. NÃO inserimos a tx de débito aqui: o débito (cache +
	// senderzz_affiliate_transactions type='withdrawal') é do admin no approve
	// (cod_saques.go::ApproveAffiliate / bulk_queues.go::CodSaquesAffiliateApprove),
	// que trava a carteira FOR UPDATE e revalida o saldo. Debitar aqui causaria
	// double-debit no approve e perda do bruto no reject (que não estorna). A taxa
	// (fee>0) vira receita 'taxa_saque' via trigger quando o admin aprova/paga.
	// AUDIT-2026-07-30 LOW: pix_key avulsa (sem conta cadastrada) não passa por
	// nenhuma checagem de titularidade — afiliado pode direcionar o próprio saque
	// pra uma chave PIX de terceiro. Não dá pra validar titularidade
	// automaticamente sem integração bancária; marca a linha com um admin_note
	// de alerta pra a aprovação manual (cod_saques.go::ApproveAffiliate) exigir
	// conferência antes de pagar.
	var adminNote *string
	if strings.TrimSpace(pixType) == "" {
		note := "⚠ PIX sem conta cadastrada (chave avulsa) — conferir titularidade antes de pagar."
		adminNote = &note
	}

	var wdID int64
	if err := tx.QueryRow(r.Context(),
		`INSERT INTO senderzz_affiliate_withdrawals
		    (affiliate_id, amount, fee, net_amount, status, pix_key, pix_type, holder_cpf, admin_note)
		 VALUES ($1, $2, $3, $4, 'pending', $5, $6, $7, $8)
		 RETURNING id`,
		homeLink, amount, fee, net, pixKey, pixType, holderCPF, adminNote,
	).Scan(&wdID); err != nil {
		slog.Error("[aff_wallet] erro ao inserir saque", "wp_user_id", u.WPUserID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "Não foi possível registrar o saque. Tente novamente.")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("[aff_wallet] erro ao commit do saque", "wp_user_id", u.WPUserID, "wd_id", wdID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	slog.Info("[aff_wallet] saque de comissão solicitado",
		"wp_user_id", u.WPUserID, "home_link", homeLink, "wd_id", wdID, "amount", amount, "fee", fee, "net", net)
	httpx.WriteOK(w, map[string]any{
		"ok":            true,
		"withdrawal_id": wdID,
		"amount":        amount,
		"fee":           fee,
		"net_amount":    net,
		"message":       "Saque de comissão solicitado com sucesso.",
	})
}

// resolveAffiliatePix devolve (pix_key, pix_type, holder_cpf) p/ o saque do
// afiliado. Prioridade: (1) conta por account_id; (2) conta DEFAULT do afiliado;
// (3) pix_key avulsa do body (sem conta cadastrada → pix_type/holder_cpf vazios).
// Escopo por user_id = codWalletKey(u) (= wp_user_id): só as PRÓPRIAS contas.
func (h *AffiliateWalletHandler) resolveAffiliatePix(r *http.Request, codKey, accountID int64, bodyPixKey string) (string, string, string) {
	// (1)/(2): tenta a conta cadastrada (informada ou a default).
	q := `SELECT COALESCE(pix_key,''), COALESCE(pix_type,''), COALESCE(holder_cpf,'')
	        FROM sz_cod_withdraw_accounts
	       WHERE user_id = $1 AND COALESCE(active,false) IS TRUE`
	var (
		pixKey, pixType, holderCPF string
		err                        error
	)
	if accountID > 0 {
		err = h.Pool.QueryRow(r.Context(), q+` AND id = $2`, codKey, accountID).
			Scan(&pixKey, &pixType, &holderCPF)
	} else {
		err = h.Pool.QueryRow(r.Context(), q+` ORDER BY is_default DESC, id ASC LIMIT 1`, codKey).
			Scan(&pixKey, &pixType, &holderCPF)
	}
	if err == nil && strings.TrimSpace(pixKey) != "" {
		return pixKey, pixType, holderCPF
	}
	// (3): pix_key avulsa do body (degradação — sem conta cadastrada).
	return strings.TrimSpace(bodyPixKey), "", ""
}

// ── POST /portal/affiliate-wallet/anticipate ──────────────────────────────────

// AnticipateAffiliate antecipa a comissão PENDENTE do afiliado. Espelho do COD
// Anticipate: flag, role afiliado, escopo do PRÓPRIO afiliado, taxa %, atômico.
//
// Fluxo (uma transação pgx):
//  0. advisory lock por wp_user_id (MESMA chave do saque) — serializa os dois
//  1. vínculo-casa (homeAffiliateLink) p/ ancorar a tx de fee
//  2. SELECT pending tx do afiliado (status='pending', available_at no futuro)
//     FOR UPDATE OF tx; soma o bruto e coleta os ids
//  3. total = SUM(amount); se <= 0 → 422 "Não há comissão pendente para antecipar"
//  4. fee = round(total × pct/100); net = total - fee
//  5. UPDATE essas tx pending → 'approved' (libera; saem do Future automaticamente)
//  6. INSERT tx type='anticipation_fee', amount=-(fee), status='approved'
func (h *AffiliateWalletHandler) AnticipateAffiliate(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	if !isAffiliate(u.Role) {
		httpx.WriteErr(w, http.StatusForbidden, "Antecipação de comissão disponível apenas para afiliados.")
		return
	}
	if !h.affAnticipateEnabled(r) {
		httpx.WriteErr(w, http.StatusForbidden, "Antecipação de comissão não habilitada.")
		return
	}
	if u.WPUserID <= 0 {
		httpx.WriteErr(w, http.StatusForbidden, "Conta sem vínculo de afiliado.")
		return
	}

	antPct := h.affAnticipationFeePct(r)

	tx, err := h.Pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		slog.Error("[aff_wallet] erro ao iniciar tx de antecipação", "wp_user_id", u.WPUserID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	// P0 — advisory lock (MESMA chave do saque): mutuamente exclusivos por afiliado.
	if _, err := tx.Exec(r.Context(),
		`SELECT pg_advisory_xact_lock($1)`, affWalletLockKey(u.WPUserID),
	); err != nil {
		slog.Error("[aff_wallet] erro no advisory lock de antecipação", "wp_user_id", u.WPUserID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	homeLink, ok := h.homeAffiliateLink(tx, r.Context(), u.WPUserID)
	if !ok {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "Sem vínculo de afiliado para antecipar.")
		return
	}

	// Trava a comissão PENDENTE do afiliado (available_at no futuro) FOR UPDATE OF
	// tx — impede que o cron de release as solte no meio da antecipação. Escopo
	// por afiliado via JOIN. available_at no futuro = ainda não liberada.
	rows, err := tx.Query(r.Context(),
		`SELECT tx.id, tx.amount
		   FROM senderzz_affiliate_transactions tx
		   JOIN senderzz_affiliates a ON a.id = tx.affiliate_id
		  WHERE a.afiliado_id = $1
		    AND tx.status = 'pending'
		    AND tx.type   = 'commission'
		    AND (tx.available_at IS NULL OR tx.available_at > NOW())
		  ORDER BY tx.available_at ASC NULLS LAST, tx.id ASC
		  FOR UPDATE OF tx`,
		u.WPUserID,
	)
	if err != nil {
		slog.Error("[aff_wallet] erro ao travar comissão pendente", "wp_user_id", u.WPUserID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	var pendingIDs []int64
	var total float64
	for rows.Next() {
		var id int64
		var amt float64
		if err := rows.Scan(&id, &amt); err != nil {
			rows.Close()
			slog.Error("[aff_wallet] erro ao ler comissão pendente", "wp_user_id", u.WPUserID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
			return
		}
		pendingIDs = append(pendingIDs, id)
		total += amt
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		slog.Error("[aff_wallet] erro após iterar comissão pendente", "wp_user_id", u.WPUserID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	total = round2(total)
	if total <= 0 || len(pendingIDs) == 0 {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "Não há comissão pendente para antecipar.")
		return
	}

	fee := round2(total * (antPct / 100))
	net := round2(total - fee)
	if net < 0 {
		net = 0
	}
	pctStr := strconv.FormatFloat(antPct, 'f', 2, 64)

	// Passo 5: libera as tx pendentes (pending → approved). Elas saem do Future
	// (que filtra 'pending') → re-antecipação fica naturalmente bloqueada.
	if _, err := tx.Exec(r.Context(),
		`UPDATE senderzz_affiliate_transactions
		    SET status = 'approved', available_at = NOW(), updated_at = NOW()
		  WHERE id = ANY($1)`,
		pendingIDs,
	); err != nil {
		slog.Error("[aff_wallet] erro ao liberar comissão antecipada", "wp_user_id", u.WPUserID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "Não foi possível processar a antecipação. Tente novamente.")
		return
	}

	// Passo 6: cobra a taxa de antecipação (amount NEGATIVO, status='approved').
	// O trigger sz_revenue_capture_afftx (estendido na migração 476) book essa
	// taxa como 'taxa_antecipacao' em senderzz_revenue.
	if _, err := tx.Exec(r.Context(),
		`INSERT INTO senderzz_affiliate_transactions
		    (affiliate_id, type, amount, status, description, created_at, updated_at)
		 VALUES ($1, 'anticipation_fee', $2, 'approved', $3, NOW(), NOW())`,
		homeLink, -fee,
		"Taxa de antecipação de comissão ("+pctStr+"%)",
	); err != nil {
		slog.Error("[aff_wallet] erro ao inserir taxa de antecipação", "wp_user_id", u.WPUserID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "Não foi possível registrar a taxa de antecipação. Tente novamente.")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("[aff_wallet] erro ao commit da antecipação", "wp_user_id", u.WPUserID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	slog.Info("[aff_wallet] antecipação de comissão aprovada",
		"wp_user_id", u.WPUserID, "home_link", homeLink, "count", len(pendingIDs), "total", total, "fee", fee, "net", net)
	httpx.WriteOK(w, map[string]any{
		"ok":      true,
		"total":   total,
		"fee":     fee,
		"net":     net,
		"message": "Antecipação aprovada! Valor líquido já disponível para saque.",
	})
}
