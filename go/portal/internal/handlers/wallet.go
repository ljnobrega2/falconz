// Package handlers — handler da Carteira do Portal V2 (user-scoped).
//
// Espelha templates/portal/v2/sections/wallet.php sobre Postgres.
// Replica os dados + REGRAS da seção "Carteira":
//   - KPIs: saldo disponível / saldo pendente / saque em análise
//   - Histórico de movimentação (transações passadas)
//   - Lançamentos futuros (recebíveis pendentes a liberar)
//   - Ordens de saque (histórico de saques do produtor)
//   - Contas PIX para recebimento
//   - Pedir saque (com flag, mínimo R$10, lock de duplicado, taxa server-side)
//
// Rotas (namespace /wp-json/senderzz/v1):
//
//	GET  /portal/wallet/summary      — KPIs (available/pending/analysis)
//	GET  /portal/wallet/history      — extrato de movimentação (?period=)
//	GET  /portal/wallet/future       — lançamentos futuros (?period=)
//	GET  /portal/wallet/withdrawals  — ordens de saque (produtor)
//	GET  /portal/wallet/accounts     — contas PIX do usuário (produtor)
//	POST /portal/wallet/accounts     — cadastrar conta PIX (produtor; limite 3)
//	POST /portal/wallet/withdraw     — solicitar saque COD (produtor + flag)
//
// ── Espelho do PHP / governança (porte fiel) ────────────────────────────────
//
// Duas fontes de dados conforme o role do usuário autenticado (u.Role):
//
//	PRODUTOR / OPERATOR → Carteira COD (motoboy). Scope SEMPRE por wp_user_id
//	(senderzz_portal_wallet_user_id() retorna wp_user_id — o WP user_id é o
//	"dono financeiro"). Tabelas:
//	  - sz_cod_wallet_transactions  (livro razão; soma a coluna `net`)
//	  - sz_cod_withdrawals          (ordens de saque)
//	  - sz_cod_withdraw_accounts    (contas PIX)
//	Saldo disponível = SUM(net) WHERE status='available'
//	Saldo pendente   = SUM(net) WHERE status='pending'
//	Saque em análise = SUM(amount FROM withdrawals) WHERE status IN ('analysis','pending')
//
//	AFILIADO → comissão do livro razão de afiliado. Scope pelo VÍNCULO
//	(NUNCA por wp_user_id direto, NUNCA por senderzz_affiliate_wallet que é
//	keyed por id de vínculo e fica defasado):
//	  JOIN senderzz_affiliates a ON a.id = tx.affiliate_id
//	   WHERE a.afiliado_id = $wp_user_id   (id-space canônico: afiliado_id = wp_user_id)
//	pendente/disponível agregados do ledger senderzz_affiliate_transactions
//	por status ('pending'/'available'); soma a coluna `amount`.
//	Afiliado é READ-ONLY aqui: withdraw/accounts/withdrawals são produtor-COD.
//	O saque de afiliado vive na seção Afiliados (outro workflow é dono).
//	Afiliado NÃO vê taxa de entrega (não há essa coluna nesta carteira).
//
// Regras de saque (produtor) — espelha sz_portal_v2_rest_cod_withdraw()
// (includes/senderzz-rest.php:769):
//   - flag senderzz_dashboard_v2_withdraw_enabled (option) default 'no' → 403
//   - subconta (parent_user_id != 0) NÃO pode sacar (gate da wallet.php)
//   - valor mínimo R$ 10,00 (server-side)
//   - lock: já existe saque em status ('analysis','pending','approved') → 422
//   - saldo disponível com FOR UPDATE; amount > available → 422
//   - taxa = min(amount, withdraw_fee global); net = amount - fee
//   - dual insert em UMA transação pgx:
//     INSERT sz_cod_withdrawals  (status='analysis')
//     INSERT sz_cod_wallet_transactions (type='withdrawal', net=-amount)
//
// Taxa de saque = global/admin (produtor não edita) — lida de senderzz_options
// chave sz_cod_withdraw_fee (default 2.99). Dias de retenção e taxa de
// antecipação são governança do produtor/admin e não são editados aqui.
package handlers

import (
	"encoding/json"
	"hash/fnv"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/portal-service/internal/auth"
	"github.com/senderzz/portal-service/internal/httpx"
)

// WalletHandler agrupa as dependências dos handlers de carteira.
// Construção idêntica a WebhookHandler/IntegrationsHandler — o integrador
// instancia com &WalletHandler{Pool: pool} e registra as rotas igualmente.
type WalletHandler struct {
	Pool *pgxpool.Pool
}

// ── Tipos de resposta ───────────────────────────────────────────────────────

// walletSummary — os 3 KPIs do topo da seção Carteira.
type walletSummary struct {
	Available float64 `json:"available"` // saldo disponível para saque
	Pending   float64 `json:"pending"`   // saldo pendente (aguardando liberação)
	Analysis  float64 `json:"analysis"`  // saque em análise (aguardando aprovação)
}

// walletTx — uma linha do "Histórico de movimentação".
type walletTx struct {
	Date        string  `json:"date"`        // dd/mm/aaaa hh:mm
	Description string  `json:"description"` // descrição do lançamento
	Order       string  `json:"order"`       // "#123" ou "—"
	Movement    string  `json:"movement"`    // "Disponível" | "Pendente"
	Type        string  `json:"type"`        // tipo cru da tx (ex.: 'withdrawal'); "" no ledger afiliado
	Value       float64 `json:"value"`       // bruto (gross/amount)
	Fee         float64 `json:"fee"`         // taxa
	Net         float64 `json:"net"`         // líquido (recebido = gross - fee no saque)
	Status      string  `json:"status"`      // status cru
}

// walletFutureRow — uma linha de "Lançamentos futuros".
type walletFutureRow struct {
	Date        string  `json:"date"`        // data de liberação dd/mm/aaaa
	Description string  `json:"description"` // descrição
	Order       string  `json:"order"`       // "#123" ou "—"
	Commission  float64 `json:"commission"`  // valor líquido a receber
	ReleaseAt   string  `json:"release_at"`  // data de liberação dd/mm/aaaa
}

// walletWithdrawal — uma linha de "Ordens de saque" (produtor).
type walletWithdrawal struct {
	ID         int64   `json:"id"`
	CreatedAt  string  `json:"created_at"`
	Amount     float64 `json:"amount"`
	Fee        float64 `json:"fee"`
	Net        float64 `json:"net"`
	HolderName string  `json:"holder_name"`
	PixKey     string  `json:"pix_key"`
	PixType    string  `json:"pix_type"`
	Status     string  `json:"status"`
	ProofURL   *string `json:"proof_url"`
}

// walletAccount — conta PIX para recebimento.
type walletAccount struct {
	ID         int64  `json:"id"`
	HolderName string `json:"holder_name"`
	HolderCPF  string `json:"holder_cpf"`
	PixType    string `json:"pix_type"`
	PixKey     string `json:"pix_key"`
	IsDefault  bool   `json:"is_default"`
}

// withdrawRequest — body de POST /portal/wallet/withdraw.
type withdrawRequest struct {
	AccountID int64  `json:"account_id"`
	Amount    string `json:"amount"` // aceita "1.234,56" ou "1234.56"
}

// ── Helpers ─────────────────────────────────────────────────────────────────

// isAffiliate diz se o role do usuário é afiliado-LIKE (afiliado OU cliente).
// u.Role é o valor cru do banco: produtor | afiliado | operator | cliente.
// REGRA DO DONO: 'cliente' (signup sem aprovação) tem acesso COD/comissões/carteira
// igual ao afiliado (vê só os PRÓPRIOS números — escopo por wp_user_id; nada de produtor).
func isAffiliate(role string) bool {
	r := strings.ToLower(strings.TrimSpace(role))
	return r == "afiliado" || r == "cliente"
}

// #75 (REVERTIDO) — o gate isStrictAffiliate foi REMOVIDO de Accounts/AddAccount: o
// afiliado AGORA gerencia as PRÓPRIAS contas PIX de saque (escopo por codWalletKey =
// wp_user_id, sem colisão com produtor). A função foi excluída por ter ficado sem uso.
// Summary/History/Future/Withdraw seguem usando isAffiliate (cliente/afiliado leem o
// ledger de comissão e NÃO sacam pela rota COD).

// Tetos do extrato da carteira (mantém os valores históricos: 1000 sem filtro de
// período, 200 com janela). AUDIT PERF-list-endpoints-hard-limit: a History busca
// limit+1 (N+1) para devolver has_more sem COUNT.
const (
	walletHistLimitAll    = 1000 // sem filtro de período (History/Future)
	walletHistLimitWindow = 200  // com janela [from,to] (History/Future)
	listWithdrawalsLimit  = 50   // ordens de saque (Withdrawals)
)

// walletHistLimit devolve o teto efetivo do extrato conforme haja filtro de período.
func walletHistLimit(all bool) int {
	if all {
		return walletHistLimitAll
	}
	return walletHistLimitWindow
}

// periodHistoryRange resolve o filtro de período do extrato (espelha
// sz_cod_wallet_history): hoje/ontem/7d/30d/mes/all → janela [from,to].
// Retorna (from, to, all) — quando all=true ignora a janela.
func periodHistoryRange(period string) (time.Time, time.Time, bool) {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch period {
	case "all":
		return time.Time{}, time.Time{}, true
	case "hoje", "today":
		return today, now, false
	case "ontem", "yesterday":
		return today.AddDate(0, 0, -1), today.Add(-time.Second), false
	case "7d":
		return now.AddDate(0, 0, -7), now, false
	case "mes", "month":
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()), now, false
	default: // "30d" e desconhecidos
		return now.AddDate(0, 0, -30), now, false
	}
}

// periodFutureUntil resolve o limite superior de "Lançamentos futuros"
// (espelha sz_cod_wallet_future): hoje/amanha/7d/15d/30d/all → release_at <= to.
// Retorna (to, all) — quando all=true ignora o limite.
func periodFutureUntil(period string) (time.Time, bool) {
	now := time.Now()
	switch period {
	case "all":
		return time.Time{}, true
	case "hoje", "today":
		return time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, now.Location()), false
	case "amanha", "tomorrow":
		return time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, now.Location()).AddDate(0, 0, 1), false
	case "7d":
		return now.AddDate(0, 0, 7), false
	case "15d":
		return now.AddDate(0, 0, 15), false
	default: // "30d" e desconhecidos
		return now.AddDate(0, 0, 30), false
	}
}

// fmtOrder devolve "#123" para order_id presente ou "—" para nulo.
func fmtOrder(orderID *int64) string {
	if orderID == nil || *orderID <= 0 {
		return "—"
	}
	return "#" + strconv.FormatInt(*orderID, 10)
}

// fmtDateTimeBR formata para dd/mm/aaaa hh:mm (espelha date_i18n('d/m/Y H:i')).
func fmtDateTimeBR(t time.Time) string { return t.Format("02/01/2006 15:04") }

// fmtDateBR formata para dd/mm/aaaa (espelha date_i18n('d/m/Y')).
func fmtDateBR(t time.Time) string { return t.Format("02/01/2006") }

// parseMoney aceita "1.234,56" (pt-BR) ou "1234.56" (US) e devolve float.
// Espelha sz_cod_parse_money() do PHP de forma conservadora.
func parseMoney(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	// Formato pt-BR: vírgula é decimal, ponto é milhar.
	if strings.Contains(s, ",") {
		s = strings.ReplaceAll(s, ".", "")
		s = strings.ReplaceAll(s, ",", ".")
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}

// withdrawFeeGlobal lê a taxa de saque global de senderzz_options
// (chave sz_cod_withdraw_fee — espelha optionKeys em go/admin/cod_saques.go).
// Default 2.99 (sz_cod_default_rules). Produtor não edita esta taxa.
func (h *WalletHandler) withdrawFeeGlobal(r *http.Request) float64 {
	var v string
	err := h.Pool.QueryRow(r.Context(),
		`SELECT value FROM senderzz_options WHERE name = $1`,
		"sz_cod_withdraw_fee",
	).Scan(&v)
	if err != nil {
		return 2.99
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || f < 0 {
		return 2.99
	}
	return f
}

// anticipationFeePct lê a taxa de antecipação global de senderzz_options
// (chave sz_cod_anticipation_fee_pct — espelha optionKeys em go/admin/cod_saques.go).
// Default 4.99 (sz_cod_default_rules). É um percentual aplicado sobre o total
// antecipado. Governança do admin/produtor — não editado aqui.
func (h *WalletHandler) anticipationFeePct(r *http.Request) float64 {
	var v string
	err := h.Pool.QueryRow(r.Context(),
		`SELECT value FROM senderzz_options WHERE name = $1`,
		"sz_cod_anticipation_fee_pct",
	).Scan(&v)
	if err != nil {
		return 4.99
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || f < 0 {
		return 4.99
	}
	return f
}

// withdrawEnabled lê a flag senderzz_dashboard_v2_withdraw_enabled em
// senderzz_options. Default 'no' (fail-closed) — botão/saque desabilitados.
func (h *WalletHandler) withdrawEnabled(r *http.Request) bool {
	var v string
	err := h.Pool.QueryRow(r.Context(),
		`SELECT value FROM senderzz_options WHERE name = $1`,
		"senderzz_dashboard_v2_withdraw_enabled",
	).Scan(&v)
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(v), "yes")
}

// isSubAccount diz se o usuário do portal é subconta (parent_user_id != 0).
// Subconta não pode sacar (gate da wallet.php: !is_sub). parent_user_id não
// está no PortalUser do contexto — lido aqui de senderzz_portal_users.
func (h *WalletHandler) isSubAccount(r *http.Request, portalID int64) bool {
	var parent *int64
	err := h.Pool.QueryRow(r.Context(),
		`SELECT parent_user_id FROM senderzz_portal_users WHERE id = $1`,
		portalID,
	).Scan(&parent)
	if err != nil {
		return false
	}
	return parent != nil && *parent != 0
}

// ── GET /portal/wallet/summary ──────────────────────────────────────────────

// Summary devolve os 3 KPIs (available/pending/analysis).
//
//	Produtor/operator → soma sz_cod_wallet_transactions.net por status +
//	                    sz_cod_withdrawals.amount em análise.
//	Afiliado          → soma senderzz_affiliate_transactions.amount por status
//	                    (via vínculo afiliado_id = wp_user_id). Não há "análise"
//	                    de saque nesta carteira (saque de afiliado é em Afiliados).
func (h *WalletHandler) Summary(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	out := walletSummary{}

	if isAffiliate(u.Role) {
		// Afiliado: agrega o LEDGER por vínculo. NUNCA usa senderzz_affiliate_wallet
		// (keyed por id de vínculo, defasado) nem IN(u.id,u.wp_user_id).
		//
		// CRIT-A (AUDIT-CRIT-AB): penalty (frustração) é despesa do afiliado mas o PHP a
		// grava com amount POSITIVO — sem tratamento ela inflava o sacável (afiliado wp28
		// via R$51 que deveria ser R$0). Subtrai como -ABS(amount) e aplica piso GREATEST(0,...),
		// fiel ao débito do PHP (debit=min(balance,penalty)). Bate com o cache do WalletFix.
		//
		// Saques com status='paid' (aprovados e pagos pelo admin) têm amount negativo e
		// devem sair do disponível. O bloqueio de pendentes (análise) é subtraído antes do
		// GREATEST(0,...) para que o valor em análise não apareça como disponível.
		var grossAvail, pending float64
		err := h.Pool.QueryRow(r.Context(),
			`SELECT
			    COALESCE(SUM(CASE
			        WHEN tx.status = 'approved' AND tx.type = 'penalty' THEN -ABS(tx.amount)
			        WHEN tx.status IN ('approved','paid')               THEN tx.amount
			        ELSE 0 END), 0),
			    COALESCE(SUM(CASE WHEN tx.status = 'pending' THEN tx.amount ELSE 0 END), 0)
			   FROM senderzz_affiliate_transactions tx
			   JOIN senderzz_affiliates a ON a.id = tx.affiliate_id
			  WHERE a.afiliado_id = $1`,
			u.WPUserID,
		).Scan(&grossAvail, &pending)
		if err != nil {
			slog.Error("[portal_wallet] erro ao agregar ledger afiliado", "user_id", u.ID, "wp_user_id", u.WPUserID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
			return
		}

		// Saques aguardando aprovação (pending/approved) — somados para o card "Em análise"
		// e subtraídos do disponível para que o valor bloqueado não apareça como sacável.
		if err2 := h.Pool.QueryRow(r.Context(),
			`SELECT COALESCE(SUM(wd.amount), 0)
			   FROM senderzz_affiliate_withdrawals wd
			   JOIN senderzz_affiliates a ON a.id = wd.affiliate_id
			  WHERE a.afiliado_id = $1
			    AND wd.status IN ('pending','em_analise','analysis')`,
			u.WPUserID,
		).Scan(&out.Analysis); err2 != nil {
			slog.Warn("[portal_wallet] saques afiliado em análise: tabela ausente ou erro (degradado p/ 0)", "err", err2)
			out.Analysis = 0
		}

		out.Available = math.Max(0, grossAvail-out.Analysis)
		out.Pending = pending
		httpx.WriteOK(w, map[string]any{"summary": out, "scope": "affiliate"})
		return
	}

	// Produtor/operator: Carteira COD scopada por wp_user_id — ou, p/ usuário NATIVO
	// do portal (sem vínculo WP), por id do portal (#22). codWalletKey resolve isso.
	codKey := codWalletKey(u)
	// FIX (carteira zerada): os créditos COD vindos do WP gravam net=0/fee=0 no
	// Postgres (a regra canônica do PHP é net=gross, fee=0 — includes/senderzz-cod-wallet.php:243),
	// então somar `net` cru zerava Disponível e Pendente mesmo havendo recebíveis
	// (#1561 gross=63,54 etc). COALESCE(NULLIF(net,0), gross) cai no bruto só quando
	// net está zerado/NULL e mantém o net real nas tx de saque/antecipação (que
	// gravam net != 0). Nunca inventa número: usa o gross já persistido.
	// SAQUE debita o GROSS (valor total que sai da carteira), não o net (recebido).
	// Para type='withdrawal', net = recebido (gross - fee) é MENOR que o débito real;
	// usar net subdebitaria o disponível pela taxa. O débito do disponível é o gross
	// (-amount). Demais tx (cod_received, antecipação) seguem COALESCE(NULLIF(net,0),gross).
	err := h.Pool.QueryRow(r.Context(),
		`SELECT
		    COALESCE(SUM(CASE WHEN status = 'available' THEN (CASE WHEN type = 'withdrawal' THEN gross ELSE COALESCE(NULLIF(net,0), gross) END) ELSE 0 END), 0),
		    COALESCE(SUM(CASE WHEN status = 'pending'   THEN (CASE WHEN type = 'withdrawal' THEN gross ELSE COALESCE(NULLIF(net,0), gross) END) ELSE 0 END), 0)
		   FROM sz_cod_wallet_transactions
		  WHERE user_id = $1`,
		codKey,
	).Scan(&out.Available, &out.Pending)
	if err != nil {
		slog.Error("[portal_wallet] erro ao agregar COD", "user_id", u.ID, "cod_key", codKey, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Saque em análise: soma de sz_cod_withdrawals.amount em análise/pendente.
	if err := h.Pool.QueryRow(r.Context(),
		`SELECT COALESCE(SUM(amount), 0)
		   FROM sz_cod_withdrawals
		  WHERE user_id = $1
		    AND status IN ('analysis', 'pending')`,
		codKey,
	).Scan(&out.Analysis); err != nil {
		slog.Error("[portal_wallet] erro ao somar saques em análise", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	httpx.WriteOK(w, map[string]any{"summary": out, "scope": "cod"})
}

// ── GET /portal/wallet/history ──────────────────────────────────────────────

// History devolve o extrato de movimentação (transações passadas).
// Filtro ?period= (hoje|ontem|7d|30d|mes|all), default 30d.
func (h *WalletHandler) History(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	period := r.URL.Query().Get("period")
	if period == "" {
		period = "30d"
	}
	from, to, all := periodHistoryRange(period)

	out := []walletTx{}

	if isAffiliate(u.Role) {
		// Extrato de comissão do afiliado (ledger por vínculo).
		base := `
			SELECT tx.created_at, COALESCE(tx.meta_json::text, ''), tx.order_id,
			       tx.status, tx.amount, COALESCE(tx.type,'commission')
			  FROM senderzz_affiliate_transactions tx
			  JOIN senderzz_affiliates a ON a.id = tx.affiliate_id
			 WHERE a.afiliado_id = $1`
		var rows pgx.Rows
		var err error
		// N+1: busca um a mais que o teto efetivo p/ detectar truncamento. // PERF-list-endpoints-hard-limit
		histLimit := walletHistLimit(all)
		if all {
			rows, err = h.Pool.Query(r.Context(), base+` ORDER BY tx.created_at DESC LIMIT $2`, u.WPUserID, histLimit+1)
		} else {
			rows, err = h.Pool.Query(r.Context(), base+` AND tx.created_at BETWEEN $2 AND $3 ORDER BY tx.created_at DESC LIMIT $4`, u.WPUserID, from, to, histLimit+1)
		}
		if err != nil {
			slog.Error("[portal_wallet] erro no extrato afiliado", "user_id", u.ID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
			return
		}
		defer rows.Close()
		for rows.Next() {
			var created time.Time
			var meta, status, txType string
			var orderID *int64
			var amount float64
			if err := rows.Scan(&created, &meta, &orderID, &status, &amount, &txType); err != nil {
				continue
			}
			desc := "Comissão de afiliado"
			if txType == "withdrawal" {
				desc = "Saque"
			} else if txType == "penalty" {
				desc = "Penalidade"
			}
			out = append(out, walletTx{
				Date:        fmtDateTimeBR(created),
				Description: desc,
				Order:       fmtOrder(orderID),
				Movement:    movementLabel(status),
				Type:        txType,
				Value:       amount,
				Fee:         0,
				Net:         amount,
				Status:      status,
			})
		}
		hasMore := len(out) > histLimit
		if hasMore {
			out = out[:histLimit]
		}
		httpx.WriteOK(w, map[string]any{"data": out, "total": len(out), "has_more": hasMore, "limit": histLimit, "scope": "affiliate"})
		return
	}

	// Produtor/operator: extrato COD. Chave por wp_user_id ou, p/ nativo, id portal (#22).
	codKey := codWalletKey(u)
	// FIX (líquido R$0 no extrato): mesmo caso do Summary — créditos COD do WP têm
	// net=0/fee=0. Exibe o líquido efetivo com fallback ao bruto (NULLIF(net,0)→gross)
	// só nas linhas com net zerado; nas tx de saque/antecipação (net != 0) preserva o net.
	base := `
		SELECT created_at, COALESCE(description, ''), order_id, status, type,
		       gross, fee, COALESCE(NULLIF(net,0), gross)
		  FROM sz_cod_wallet_transactions
		 WHERE user_id = $1`
	var rows pgx.Rows
	var err error
	// N+1: idêntico ao ramo afiliado — detecta has_more sem COUNT. // PERF-list-endpoints-hard-limit
	histLimit := walletHistLimit(all)
	if all {
		rows, err = h.Pool.Query(r.Context(), base+` ORDER BY created_at DESC LIMIT $2`, codKey, histLimit+1)
	} else {
		rows, err = h.Pool.Query(r.Context(), base+` AND created_at BETWEEN $2 AND $3 ORDER BY created_at DESC LIMIT $4`, codKey, from, to, histLimit+1)
	}
	if err != nil {
		slog.Error("[portal_wallet] erro no extrato COD", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var created time.Time
		var desc, status, txType string
		var orderID *int64
		var gross, fee, net float64
		if err := rows.Scan(&created, &desc, &orderID, &status, &txType, &gross, &fee, &net); err != nil {
			continue
		}
		if desc == "" {
			desc = "Recebimento COD"
		}
		// Saque: Value=gross (valor total sacado), Fee=taxa, Net=recebido (gross-fee).
		// O Type cru viaja para a UI rotular a linha como "Saque" (em vez do status).
		out = append(out, walletTx{
			Date:        fmtDateTimeBR(created),
			Description: desc,
			Order:       fmtOrder(orderID),
			Movement:    movementLabel(status),
			Type:        txType,
			Value:       gross,
			Fee:         fee,
			Net:         net,
			Status:      status,
		})
	}

	hasMore := len(out) > histLimit
	if hasMore {
		out = out[:histLimit]
	}
	httpx.WriteOK(w, map[string]any{"data": out, "total": len(out), "has_more": hasMore, "limit": histLimit, "scope": "cod"})
}

// movementLabel mapeia status → rótulo de movimento (espelha PHP).
func movementLabel(status string) string {
	if status == "available" {
		return "Disponível"
	}
	return "Pendente"
}

// ── GET /portal/wallet/future ───────────────────────────────────────────────

// Future devolve os lançamentos futuros (recebíveis pendentes a liberar).
// Filtro ?period= (hoje|amanha|7d|15d|30d|all), default 30d.
func (h *WalletHandler) Future(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	period := r.URL.Query().Get("period")
	if period == "" {
		period = "30d"
	}
	until, all := periodFutureUntil(period)

	out := []walletFutureRow{}
	var total float64

	if isAffiliate(u.Role) {
		// Recebíveis pendentes do afiliado: ledger pendente ordenado por available_at.
		base := `
			SELECT tx.available_at, COALESCE(tx.meta_json::text, ''), tx.order_id, tx.amount
			  FROM senderzz_affiliate_transactions tx
			  JOIN senderzz_affiliates a ON a.id = tx.affiliate_id
			 WHERE a.afiliado_id = $1
			   AND tx.status = 'pending'`
		var rows pgx.Rows
		var err error
		// N+1: detecta truncamento sem COUNT (has_more separado do total monetário). // PERF-list-endpoints-hard-limit
		futLimit := walletHistLimit(all)
		if all {
			rows, err = h.Pool.Query(r.Context(), base+` ORDER BY tx.available_at ASC LIMIT $2`, u.WPUserID, futLimit+1)
		} else {
			rows, err = h.Pool.Query(r.Context(), base+` AND tx.available_at <= $2 ORDER BY tx.available_at ASC LIMIT $3`, u.WPUserID, until, futLimit+1)
		}
		if err != nil {
			slog.Error("[portal_wallet] erro nos futuros afiliado", "user_id", u.ID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
			return
		}
		defer rows.Close()
		for rows.Next() {
			var releaseAt *time.Time
			var meta string
			var orderID *int64
			var amount float64
			if err := rows.Scan(&releaseAt, &meta, &orderID, &amount); err != nil {
				continue
			}
			total += amount
			rel := "—"
			if releaseAt != nil {
				rel = fmtDateBR(*releaseAt)
			}
			out = append(out, walletFutureRow{
				Date:        rel,
				Description: "Comissão de afiliado",
				Order:       fmtOrder(orderID),
				Commission:  amount,
				ReleaseAt:   rel,
			})
		}
		hasMore := len(out) > futLimit
		if hasMore {
			// Desfaz a contribuição da linha extra no total monetário e a remove.
			total -= out[futLimit].Commission
			out = out[:futLimit]
		}
		httpx.WriteOK(w, map[string]any{"data": out, "total": round2(total), "has_more": hasMore, "limit": futLimit, "scope": "affiliate"})
		return
	}

	// Produtor/operator: recebíveis COD pendentes ordenados por release_at.
	// Chave por wp_user_id ou, p/ nativo, id portal (#22).
	codKey := codWalletKey(u)
	// FIX (Antecipar "não faz nada" + total zerado): o front gateia openAnticipate em
	// future.total; com net=0 nos créditos COD do WP o total vinha 0 → toast "Não há
	// valores para antecipar" e o drawer nunca abria. Fallback ao bruto (NULLIF(net,0)→gross).
	base := `
		SELECT release_at, COALESCE(description, ''), order_id, COALESCE(NULLIF(net,0), gross)
		  FROM sz_cod_wallet_transactions
		 WHERE user_id = $1
		   AND status = 'pending'`
	var rows pgx.Rows
	var err error
	// N+1: idêntico ao ramo afiliado. // PERF-list-endpoints-hard-limit
	futLimit := walletHistLimit(all)
	if all {
		rows, err = h.Pool.Query(r.Context(), base+` ORDER BY release_at ASC LIMIT $2`, codKey, futLimit+1)
	} else {
		rows, err = h.Pool.Query(r.Context(), base+` AND release_at <= $2 ORDER BY release_at ASC LIMIT $3`, codKey, until, futLimit+1)
	}
	if err != nil {
		slog.Error("[portal_wallet] erro nos futuros COD", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var releaseAt *time.Time
		var desc string
		var orderID *int64
		var net float64
		if err := rows.Scan(&releaseAt, &desc, &orderID, &net); err != nil {
			continue
		}
		total += net
		if desc == "" {
			desc = "Recebimento COD"
		}
		rel := "—"
		if releaseAt != nil {
			rel = fmtDateBR(*releaseAt)
		}
		out = append(out, walletFutureRow{
			Date:        rel,
			Description: desc,
			Order:       fmtOrder(orderID),
			Commission:  net,
			ReleaseAt:   rel,
		})
	}

	hasMore := len(out) > futLimit
	if hasMore {
		total -= out[futLimit].Commission
		out = out[:futLimit]
	}
	httpx.WriteOK(w, map[string]any{"data": out, "total": round2(total), "has_more": hasMore, "limit": futLimit, "scope": "cod"})
}

// round2 arredonda para 2 casas (paridade com round(...,2) do PHP).
func round2(v float64) float64 {
	return float64(int64(v*100+sign(v)*0.5)) / 100
}

func sign(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}

// ── GET /portal/wallet/withdrawals ──────────────────────────────────────────

// Withdrawals lista as ordens de saque do produtor (sz_cod_withdrawals).
// Afiliado/subconta: saque vive em outra seção — retorna lista vazia.
func (h *WalletHandler) Withdrawals(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	out := []walletWithdrawal{}

	// Histórico de saques é só de produtor (wallet.php: !is_aff && !is_sub).
	if isAffiliate(u.Role) {
		httpx.WriteOK(w, map[string]any{"data": out, "total": 0, "has_more": false, "limit": listWithdrawalsLimit, "scope": "affiliate"})
		return
	}

	// Chave por wp_user_id ou, p/ usuário nativo do portal, id portal (#22).
	rows, err := h.Pool.Query(r.Context(),
		`SELECT id, created_at, amount, fee, net,
		        COALESCE(holder_name, ''), COALESCE(pix_key, ''),
		        COALESCE(pix_type, ''), status, proof_url
		   FROM sz_cod_withdrawals
		  WHERE user_id = $1
		  ORDER BY id DESC
		  LIMIT $2`,
		codWalletKey(u), listWithdrawalsLimit+1, // N+1. // PERF-list-endpoints-hard-limit
	)
	if err != nil {
		slog.Error("[portal_wallet] erro ao listar saques", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer rows.Close()

	for rows.Next() {
		var wd walletWithdrawal
		var created time.Time
		if err := rows.Scan(
			&wd.ID, &created, &wd.Amount, &wd.Fee, &wd.Net,
			&wd.HolderName, &wd.PixKey, &wd.PixType, &wd.Status, &wd.ProofURL,
		); err != nil {
			continue
		}
		wd.CreatedAt = fmtDateTimeBR(created)
		out = append(out, wd)
	}

	hasMore := len(out) > listWithdrawalsLimit
	if hasMore {
		out = out[:listWithdrawalsLimit]
	}

	httpx.WriteOK(w, map[string]any{"data": out, "total": len(out), "has_more": hasMore, "limit": listWithdrawalsLimit, "scope": "cod"})
}

// ── GET /portal/wallet/accounts ─────────────────────────────────────────────

// Accounts lista as contas PIX ativas do produtor (sz_cod_withdraw_accounts).
// Conta default primeiro (is_default DESC, id ASC) — espelha sz_cod_get_accounts.
func (h *WalletHandler) Accounts(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	out := []walletAccount{}

	// #75 (REVERTIDO): o afiliado AGORA gerencia as PRÓPRIAS contas PIX de saque.
	// Antes era read-only ("saque na seção Afiliados"); o dono destravou — cada user
	// (produtor, cliente OU afiliado) tem as suas contas, escopadas por codWalletKey
	// (= wp_user_id), SEM colisão. O gate isStrictAffiliate foi removido daqui e do
	// AddAccount. Mantidos: limite de 3, validações (CPF/PIX), is_default e o escopo
	// por user_id (não mudou). Subconta continua barrada no AddAccount.
	// Chave por wp_user_id ou, p/ usuário nativo do portal, id portal (#22).
	rows, err := h.Pool.Query(r.Context(),
		`SELECT id,
		        COALESCE(holder_name, ''),
		        COALESCE(holder_cpf, ''),
		        COALESCE(pix_type, ''),
		        COALESCE(pix_key, ''),
		        COALESCE(is_default, false) IS TRUE
		   FROM sz_cod_withdraw_accounts
		  WHERE user_id = $1 AND COALESCE(active, false) IS TRUE
		  ORDER BY is_default DESC, id ASC`,
		codWalletKey(u),
	)
	if err != nil {
		slog.Error("[portal_wallet] erro ao listar contas PIX", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer rows.Close()

	for rows.Next() {
		var a walletAccount
		if err := rows.Scan(&a.ID, &a.HolderName, &a.HolderCPF, &a.PixType, &a.PixKey, &a.IsDefault); err != nil {
			continue
		}
		out = append(out, a)
	}

	httpx.WriteOK(w, map[string]any{"accounts": out, "scope": "cod"})
}

// addAccountRequest — body de POST /portal/wallet/accounts (cadastrar conta PIX).
// Espelha os campos enviados por settings.php (szV2PixAdd) / Settings.tsx.
type addAccountRequest struct {
	HolderName string `json:"holder_name"`
	HolderCPF  string `json:"holder_cpf"`
	PixType    string `json:"pix_type"`
	PixKey     string `json:"pix_key"`
	Banco      string `json:"banco"`   // recebido p/ paridade de UX; coluna não existe no espelho PG
	Agencia    string `json:"agencia"` // dado bancário do titular (migração 426); OPCIONAL
	Conta      string `json:"conta"`   // dado bancário do titular (migração 426); OPCIONAL
}

// pixTypesAllowed — tipos de chave PIX aceitos (lowercase), espelha o whitelist do
// PHP sz_cod_rest_save_pix: ['cpf','email','telefone','aleatoria'].
var pixTypesAllowed = map[string]bool{
	"cpf":       true,
	"cnpj":      true,
	"email":     true,
	"telefone":  true,
	"aleatoria": true,
}

// onlyDigits remove tudo que não for dígito (espelha sz_cod_only_digits do PHP).
func onlyDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// validCPF valida um CPF brasileiro (11 dígitos + dígitos verificadores).
// Espelha sz_cod_valid_cpf do PHP — rejeita comprimento != 11, sequências
// repetidas (000.../111...) e DV inconsistente.
func validCPF(cpf string) bool {
	cpf = onlyDigits(cpf)
	if len(cpf) != 11 {
		return false
	}
	// Rejeita os 11 dígitos iguais (00000000000, 11111111111, ...).
	allSame := true
	for i := 1; i < 11; i++ {
		if cpf[i] != cpf[0] {
			allSame = false
			break
		}
	}
	if allSame {
		return false
	}
	// Calcula os dois dígitos verificadores.
	for t := 9; t < 11; t++ {
		sum := 0
		for i := 0; i < t; i++ {
			sum += int(cpf[i]-'0') * (t + 1 - i)
		}
		d := (sum * 10) % 11
		if d == 10 {
			d = 0
		}
		if d != int(cpf[t]-'0') {
			return false
		}
	}
	return true
}

// validDoc — CPF (11) ou CNPJ (14) do titular, conforme o tamanho informado.
// validCNPJ vem de document_change.go (mesmo pacote).
func validDoc(doc string) bool {
	d := onlyDigits(doc)
	if len(d) == 14 {
		return validCNPJ(d)
	}
	return validCPF(d)
}

// ── POST /portal/wallet/accounts ────────────────────────────────────────────
//
// AddAccount cadastra uma conta PIX de recebimento do produtor. Porte fiel de
// sz_cod_rest_save_pix() (includes/senderzz-cod-wallet.php:533):
//   - afiliado NÃO gerencia contas aqui (saque é na seção Afiliados) → 403
//   - subconta NÃO cadastra/saca → 403 (mesmo gate de Withdraw)
//   - nome do titular >= 5 chars → 422
//   - CPF do titular válido (DV) → 422
//   - tipo PIX em [cpf,email,telefone,aleatoria]; default 'cpf'
//   - conteúdo da chave >= 3 chars → 422
//   - quando tipo='cpf', a chave deve ser o MESMO CPF do titular → 422
//   - limite de 3 contas ativas por usuário → 422
//   - is_default = (1ª conta ? 1 : 0)
//
// Escopo: user_id = wp_user_id (dono financeiro da carteira COD), idêntico a
// Accounts/Withdraw. O espelho PG não tem colunas bank_name/account_type
// (banco é recebido só p/ paridade de UX e ignorado na escrita). agencia/conta
// SÃO persistidos (migração 426) — opcionais, sem validação obrigatória (o WP
// exige agência/conta, mas o fluxo PIX do Portal V2 não, então não criamos 422).
//
// DIVERGÊNCIA CONSCIENTE do WP (sz_cod_rest_save_pix grava sz_cod_mask_cpf($cpf)):
// aqui gravamos os 11 dígitos CRUS em holder_cpf, NÃO o CPF mascarado. Razão: este
// é um registro de PAGAMENTO — o admin precisa do CPF completo p/ conciliar o PIX
// na hora do repasse; mascarar na escrita perderia o dado real. A máscara é
// responsabilidade da camada de exibição (a UI Settings.tsx mascara a chave PIX).
// A validação type=cpf ⇒ pix_key == holder_cpf usa os dígitos crus, então é coerente.
func (h *WalletHandler) AddAccount(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	// #75 (REVERTIDO): o afiliado AGORA cadastra a PRÓPRIA conta PIX de payout — o
	// gate isStrictAffiliate (antes 403 "...gerenciadas na carteira COD...") foi
	// removido. Escopo por codWalletKey(u) = wp_user_id (vê/usa só os PRÓPRIOS dados),
	// sem colisão com produtor. Subconta SEGUE barrada (não saca pela rota COD).
	// Subconta não cadastra conta de saque (mesmo gate de Withdraw).
	if h.isSubAccount(r, u.ID) {
		httpx.WriteErr(w, http.StatusForbidden, "Cadastro de conta indisponível para subcontas.")
		return
	}

	// #22: usuário NATIVO do portal (sem vínculo WP) PODE cadastrar conta — antes era
	// barrado aqui com 403 "Usuário sem vínculo WP". A chave da carteira COD é
	// resolvida por codWalletKey (wp_user_id se houver, senão id do portal).
	codKey := codWalletKey(u)

	var req addAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}

	holder := strings.TrimSpace(req.HolderName)
	// holder_cpf guarda o documento do titular — CPF (11) OU CNPJ (14). Nome de
	// campo/coluna ficou de quando só CPF existia; validDoc aceita os dois.
	holderCPF := onlyDigits(req.HolderCPF)
	pixType := strings.ToLower(strings.TrimSpace(req.PixType))
	pixKey := strings.TrimSpace(req.PixKey)
	// Dados bancários OPCIONAIS (migração 426): só dígitos da agência; conta crua
	// (pode ter dígito verificador '-'). Vazio → grava string vazia, sem 422.
	agencia := onlyDigits(req.Agencia)
	conta := strings.TrimSpace(req.Conta)

	// Validações (espelham 1:1 sz_cod_rest_save_pix, + CNPJ 2026-07-21).
	if len(holder) < 5 {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "Informe o nome completo do titular.")
		return
	}
	if !validDoc(holderCPF) {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "CPF/CNPJ do titular inválido.")
		return
	}
	if !pixTypesAllowed[pixType] {
		pixType = "cpf"
	}
	if len(pixKey) < 3 {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "Informe o conteúdo da chave PIX.")
		return
	}
	// Quando a chave for CPF/CNPJ, deve ser o mesmo documento do titular.
	if (pixType == "cpf" || pixType == "cnpj") && onlyDigits(pixKey) != holderCPF {
		httpx.WriteErr(w, http.StatusUnprocessableEntity,
			"Quando a chave PIX for CPF/CNPJ, ela deve ser o mesmo documento do titular.")
		return
	}

	// Limite de 3 contas ativas por usuário (espelha o COUNT>=3 do PHP).
	var count int
	if err := h.Pool.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM sz_cod_withdraw_accounts
		  WHERE user_id = $1 AND COALESCE(active, false) IS TRUE`,
		codKey,
	).Scan(&count); err != nil {
		slog.Error("[portal_wallet] erro ao contar contas PIX", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	if count >= 3 {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "Limite de 3 contas para saque atingido.")
		return
	}

	// is_default = 1ª conta. `name` (NOT NULL no espelho) = holder p/ compat.
	isDefault := count == 0
	var newID int64
	err := h.Pool.QueryRow(r.Context(),
		`INSERT INTO sz_cod_withdraw_accounts
		     (user_id, name, holder_name, holder_cpf, pix_type, pix_key,
		      agencia, conta, is_default, active, created_at)
		 VALUES ($1, $2, $2, $3, $4, $5, $6, $7, $8, true, NOW())
		 RETURNING id`,
		codKey, holder, holderCPF, pixType, pixKey, agencia, conta, isDefault,
	).Scan(&newID)
	if err != nil {
		slog.Error("[portal_wallet] erro ao cadastrar conta PIX", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao cadastrar conta PIX")
		return
	}

	slog.Info("[portal_wallet] conta PIX cadastrada", "user_id", u.ID, "account_id", newID, "is_default", isDefault)
	httpx.WriteOK(w, map[string]any{
		"id":         newID,
		"is_default": isDefault,
		"message":    "Conta PIX cadastrada.",
	})
}

// codWalletKey — chave de PROPRIEDADE da carteira COD (sz_cod_wallet_transactions
// .user_id, sz_cod_withdrawals.user_id, sz_cod_withdraw_accounts.user_id).
//
// #22 — Carteira p/ usuário NATIVO do portal (sem wp_user_id): FALK é standalone,
// então um usuário criado direto no portal (sem vínculo WP, WPUserID<=0) PRECISA
// ter carteira COD. A regra histórica (scope SEMPRE por wp_user_id, espelho de
// senderzz_portal_wallet_user_id() do PHP) só valia porque, no WP, todo usuário tem
// wp_users.ID. No portal nativo isso não existe.
//
// FALLBACK:
//
//	WPUserID>0  → usa wp_user_id (NÃO quebra usuários com vínculo WP — mesma chave
//	               de sempre; nada migra).
//	WPUserID<=0 → usa -u.ID (id do portal NEGADO) como chave da carteira COD.
//
// POR QUE NEGATIVO (e não u.ID cru) — P0 anti-colisão de FUNDOS: senderzz_portal_users
// .id (sequência IDENTITY própria, hoje 12..63) e wp_users.ID (espaço do WordPress,
// inclui 1, 2, ... 990003) SE SOBREPÕEM na faixa baixa. sz_cod_wallet_transactions
// .user_id é um bigint SEM tag de namespace. Se um nativo usasse u.ID cru, o nativo
// id=63 leria/sacaria o saldo do WP user wp_user_id=63 (roubo horizontal de fundos).
// Como wp ids são SEMPRE positivos, negar o id do portal cria um espaço disjunto que
// NUNCA colide. bigint aceita negativos. Toda leitura/escrita do portal passa por
// aqui → consistência interna preservada (saldo/saque/extrato/lock usam a MESMA chave).
//
// CONSISTÊNCIA: esta MESMA função alimenta saldo (Summary), extrato (History),
// futuros (Future), saques (Withdrawals), contas PIX (Accounts/AddAccount), saque
// (Withdraw) e antecipação (Anticipate) — e o advisory lock (codWalletLockKey).
func codWalletKey(u *auth.PortalUser) int64 {
	if u.WPUserID > 0 {
		return u.WPUserID
	}
	return -u.ID // namespace disjunto (negativo) p/ nativo — nunca colide com wp ids (positivos)
}

// codWalletLockKey — chave int64 estável para pg_advisory_xact_lock(bigint),
// derivada do wp_user_id (dono financeiro da carteira COD). Namespace
// "sz_cod_wallet:" isola este lock dos demais advisory locks do sistema
// (ex.: checkout). Withdraw e Anticipate USAM A MESMA CHAVE de propósito:
// ambos consomem o mesmo guard "1 saque ativo por user" (COUNT em
// 'analysis','pending','approved'), logo precisam ser mutuamente exclusivos
// por usuário — sem isso, um saque e uma antecipação concorrentes furariam
// o guard (double-spend / overdraft). P0 financeiro.
func codWalletLockKey(wpUserID int64) int64 {
	hsh := fnv.New64a()
	_, _ = hsh.Write([]byte("sz_cod_wallet:" + strconv.FormatInt(wpUserID, 10)))
	return int64(hsh.Sum64()) //nolint:gosec — wrap intencional para caber em bigint
}

// ── POST /portal/wallet/withdraw ────────────────────────────────────────────

// Withdraw solicita um saque COD (produtor). Porte fiel de
// sz_portal_v2_rest_cod_withdraw() (includes/senderzz-rest.php:769):
//   - flag senderzz_dashboard_v2_withdraw_enabled (option) default 'no' → 403
//   - subconta não pode sacar → 403
//   - conta PIX deve pertencer ao usuário → 422
//   - valor mínimo R$ 10,00 → 422
//   - advisory lock por wp_user_id (pg_advisory_xact_lock) serializa saque +
//     antecipação concorrentes → COUNT+INSERT viram atômicos (anti double-spend)
//   - lock de saque já em ('analysis','pending','approved') → 422
//   - saldo disponível agregado; amount > available → 422
//   - taxa = min(amount, withdraw_fee global); net = amount - fee
//   - dual insert (withdrawals 'analysis' + transactions 'withdrawal' net=-amount)
//     numa transação pgx (BeginTx + defer Rollback + Commit, igual Login2FA).
func (h *WalletHandler) Withdraw(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	// Afiliado: saque é na seção Afiliados (outro workflow). Aqui é COD-produtor.
	if isAffiliate(u.Role) {
		httpx.WriteErr(w, http.StatusForbidden, "saque indisponível para este perfil")
		return
	}

	// Guard de flag (fail-closed) — espelha o guard duplo do PHP.
	if !h.withdrawEnabled(r) {
		httpx.WriteErr(w, http.StatusForbidden, "Saque V2 não habilitado. Utilize o painel clássico.")
		return
	}

	// Subconta não pode sacar (wallet.php: !is_sub).
	if h.isSubAccount(r, u.ID) {
		httpx.WriteErr(w, http.StatusForbidden, "Saque não disponível para subcontas.")
		return
	}

	// #22: usuário NATIVO do portal (sem vínculo WP) PODE sacar — antes era barrado
	// aqui com 403. Toda a operação (conta PIX, lock, saldo, dual insert) usa codKey
	// como chave de propriedade: wp_user_id se houver, senão id do portal. Consistente
	// com Summary/History/Future/Accounts → o nativo saca do MESMO saldo que vê.
	codKey := codWalletKey(u)

	var req withdrawRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}

	amount := round2(parseMoney(req.Amount))

	// Valor mínimo R$ 10 (validação server-side).
	if amount < 10.0 {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "Valor mínimo para saque é R$ 10,00.")
		return
	}

	// Conta PIX deve pertencer ao usuário (status ativo).
	var acc walletAccount
	err := h.Pool.QueryRow(r.Context(),
		`SELECT id,
		        COALESCE(holder_name, ''), COALESCE(holder_cpf, ''),
		        COALESCE(pix_type, ''), COALESCE(pix_key, '')
		   FROM sz_cod_withdraw_accounts
		  WHERE id = $1 AND user_id = $2 AND COALESCE(active, false) IS TRUE`,
		req.AccountID, codKey,
	).Scan(&acc.ID, &acc.HolderName, &acc.HolderCPF, &acc.PixType, &acc.PixKey)
	if err == pgx.ErrNoRows {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "Cadastre uma conta PIX no Perfil antes de sacar.")
		return
	}
	if err != nil {
		slog.Error("[portal_wallet] erro ao validar conta PIX", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	fee := h.withdrawFeeGlobal(r)

	// Transação: serializa saques concorrentes (espelha START TRANSACTION + FOR UPDATE).
	tx, err := h.Pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		slog.Error("[portal_wallet] erro ao iniciar transação de saque", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	// P0 — advisory lock por wp_user_id ANTES de qualquer leitura: serializa
	// saques/antecipações concorrentes do MESMO usuário. Sem isto, o par
	// "COUNT (guard) + INSERT" não é atômico (retry/concorrência) → double-spend.
	// O lock é liberado no fim da transação (xact). Mesma chave em Anticipate.
	if _, err := tx.Exec(r.Context(),
		`SELECT pg_advisory_xact_lock($1)`, codWalletLockKey(codKey),
	); err != nil {
		slog.Error("[portal_wallet] erro no advisory lock de saque", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Lock de saque duplicado em andamento (guard "1 saque ativo por user").
	var pendingWd int64
	if err := tx.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM sz_cod_withdrawals
		  WHERE user_id = $1 AND status IN ('analysis','pending','approved')`,
		codKey,
	).Scan(&pendingWd); err != nil {
		slog.Error("[portal_wallet] erro no lock de saque", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	if pendingWd > 0 {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "Já existe um saque em análise. Aguarde a conclusão.")
		return
	}

	// Saldo disponível. SEM FOR UPDATE: (1) FOR UPDATE com agregado é inválido no
	// Postgres (erro "FOR UPDATE is not allowed with aggregate functions"); (2) o
	// advisory lock acima já garante consistência sob concorrência. O débito do
	// saque entra como status='analysis' (fora deste SUM de 'available'), portanto
	// o agregado nunca foi a trava de overdraft — o guard COUNT é. P0.
	// BUG-FIX 2026-06-24: o saldo do SAQUE tem de usar a MESMA fórmula do Summary (o
	// valor que o produtor VÊ). O antigo SUM(net) cru somava os débitos de saque
	// (net=-amount) E ignorava 25+ recebíveis COD com net=0/gross>0 → o saldo do check
	// ficava NEGATIVO e nenhum saque passava. Fórmula canônica (espelha o Summary,
	// ~linha 388): saque usa gross (débito); recebível usa net ou, se net=0, gross.
	var available float64
	if err := tx.QueryRow(r.Context(),
		`SELECT COALESCE(SUM(
		           CASE WHEN type = 'withdrawal' THEN gross
		                ELSE COALESCE(NULLIF(net, 0), gross) END
		        ), 0)
		   FROM sz_cod_wallet_transactions
		  WHERE user_id = $1 AND status = 'available'`,
		codKey,
	).Scan(&available); err != nil {
		slog.Error("[portal_wallet] erro ao ler saldo disponível", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	available = round2(available)

	if amount <= 0 || amount > available {
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

	// INSERT 1: ordem de saque. BUG-FIX 2026-06-24: a tabela sz_cod_withdrawals NÃO
	// tem coluna account_id (a conta PIX é DENORMALIZADA em pix_key/pix_type/holder_*),
	// e o CHECK de status NÃO aceita 'analysis' (só pending/approved/paid/rejected/
	// cancelled). O código antigo (escrito p/ um schema divergente) quebrava no INSERT —
	// mascarado pelo bug do saldo. Ordem nasce 'pending' (aguarda aprovação do admin).
	var wdID int64
	if err := tx.QueryRow(r.Context(),
		`INSERT INTO sz_cod_withdrawals
		    (user_id, amount, fee, net,
		     pix_key, pix_type, holder_name, holder_cpf,
		     status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'pending', NOW(), NOW())
		 RETURNING id`,
		codKey, amount, fee, net,
		acc.PixKey, acc.PixType, acc.HolderName, acc.HolderCPF,
	).Scan(&wdID); err != nil {
		slog.Error("[portal_wallet] erro ao inserir saque", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "Não foi possível registrar o saque. Tente novamente.")
		return
	}

	// INSERT 2: lançamento contábil de débito (type='withdrawal').
	// gross = -amount (valor total que SAI da carteira — é o débito do disponível);
	// fee  = taxa de saque; net = -net (recebido = amount - fee). Antes net era -amount
	// (igual ao gross), o que exibia LÍQUIDO = valor total em vez do recebido. O débito
	// do disponível usa o GROSS (Summary CASE type='withdrawal' THEN gross), então o
	// saldo é debitado pelo valor total e o LÍQUIDO mostra o que o produtor recebe.
	// BUG-FIX 2026-06-24: o débito entra como status='available' (o CHECK não aceita
	// 'analysis'), assim o Summary (que soma só status='available') DEBITA o disponível
	// na hora → saldo cai pra 0 e não dá pra sacar o mesmo dinheiro 2x. (Se o admin
	// recusar o saque, o estorno é tratado no fluxo de admin.)
	// amount é NOT NULL e segue a convenção da tabela: amount == gross (negativo no
	// débito). gross=-amount (sai da carteira), fee=taxa, net=-net (recebido líquido).
	if _, err := tx.Exec(r.Context(),
		`INSERT INTO sz_cod_wallet_transactions
		    (user_id, type, status, amount, gross, fee, net, description, created_at, updated_at)
		 VALUES ($1, 'withdrawal', 'available', $2, $3, $4, $5, $6, NOW(), NOW())`,
		codKey, -amount, -amount, fee, -net,
		"Solicitação de saque COD V2 #"+strconv.FormatInt(wdID, 10),
	); err != nil {
		slog.Error("[portal_wallet] erro ao inserir lançamento de saque", "user_id", u.ID, "wd_id", wdID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "Não foi possível registrar a movimentação do saque. Tente novamente.")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("[portal_wallet] erro ao commit do saque", "user_id", u.ID, "wd_id", wdID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	slog.Info("[portal_wallet] saque solicitado", "user_id", u.ID, "cod_key", codKey, "wd_id", wdID, "amount", amount, "fee", fee, "net", net)
	httpx.WriteOK(w, map[string]any{
		"success": true,
		"message": "Saque solicitado com sucesso.",
		"fee":     fee,
		"net":     net,
		"wd_id":   wdID,
	})
}

// anticipateRequest — body de POST /portal/wallet/anticipate.
//
// #94 — ANTECIPAÇÃO NÃO PEDE CONTA PIX: antecipar apenas move o saldo pendente →
// disponível, descontando a taxa de antecipação. O líquido fica CREDITADO no
// disponível (passo 8); o saque para PIX é um fluxo separado (Withdraw). Por isso
// o body é vazio — não há account_id. O struct é mantido (vazio) para preservar
// o contrato/decodificação e facilitar evolução futura.
type anticipateRequest struct{}

// ── POST /portal/wallet/anticipate ──────────────────────────────────────────
//
// Anticipate antecipa TODOS os recebíveis pendentes do produtor para a conta
// PIX informada. O valor antecipado é a SOMA de todas as tx pendentes — nunca
// vem do front (espelha sz_portal_v2_rest_cod_anticipate → sz_cod_rest_withdraw
// com tipo=antecipacao). Porte fiel de includes/senderzz-cod-wallet.php:570-652.
//
// Fluxo (uma transação pgx):
//  0. advisory lock por wp_user_id (pg_advisory_xact_lock) — MESMA chave do
//     Withdraw; serializa saque+antecipação concorrentes → guard atômico (P0)
//  1. lock de saque duplicado em ('analysis','pending','approved') → 422
//  2. SELECT pending tx FOR UPDATE (trava contra release concorrente); soma net
//  3. amount = SUM(net); se <= 0 → 422 "Não há saldo pendente para antecipar"
//  4. fee = round(amount * antFeePct/100); net = amount - fee
//  5. UPDATE essas tx → status='anticipation_pending' (cron não libera de novo)
//  6. INSERT sz_cod_withdrawals (status='approved')
//  7. INSERT tx type='anticipation'        (status='approved', net=-net)
//  8. INSERT tx type='anticipation_credit' (status='available', net=+net)
//
// Guards idênticos ao Withdraw: flag, afiliado→403, subconta→403, conta do user.
func (h *WalletHandler) Anticipate(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	// Antecipação é COD-produtor. Afiliado não tem recebíveis COD aqui.
	if isAffiliate(u.Role) {
		httpx.WriteErr(w, http.StatusForbidden, "antecipação indisponível para este perfil")
		return
	}

	// Guard de flag (fail-closed) — espelha o guard duplo do PHP.
	if !h.withdrawEnabled(r) {
		httpx.WriteErr(w, http.StatusForbidden, "Antecipação não habilitada.")
		return
	}

	// Subconta não pode antecipar (mesmo gate do saque).
	if h.isSubAccount(r, u.ID) {
		httpx.WriteErr(w, http.StatusForbidden, "Antecipação não disponível para subcontas.")
		return
	}

	// #22: usuário NATIVO do portal (sem vínculo WP) PODE antecipar — antes era
	// barrado aqui com 403. codKey = wp_user_id se houver, senão id do portal (mesma
	// chave de Withdraw/Summary → antecipa do MESMO saldo pendente que vê).
	codKey := codWalletKey(u)

	// #94 — antecipação NÃO valida/exige conta PIX (o body é vazio). Antecipar só
	// move o pendente → disponível menos a taxa; o crédito líquido vai para o
	// saldo disponível (passo 8). O saque para PIX é outro fluxo (Withdraw).
	antPct := h.anticipationFeePct(r)

	tx, err := h.Pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		slog.Error("[portal_wallet] erro ao iniciar transação de antecipação", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	// P0 — advisory lock por wp_user_id ANTES de qualquer leitura. MESMA chave
	// do Withdraw (codWalletLockKey): saque e antecipação compartilham o guard
	// "1 saque ativo por user", logo precisam ser mutuamente exclusivos. Sem
	// isto, "COUNT (guard) + UPDATE/INSERT" não é atômico → double-spend.
	if _, err := tx.Exec(r.Context(),
		`SELECT pg_advisory_xact_lock($1)`, codWalletLockKey(codKey),
	); err != nil {
		slog.Error("[portal_wallet] erro no advisory lock de antecipação", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Lock de saque duplicado em andamento (mesma regra do saque normal).
	var pendingWd int64
	if err := tx.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM sz_cod_withdrawals
		  WHERE user_id = $1 AND status IN ('analysis','pending','approved')`,
		codKey,
	).Scan(&pendingWd); err != nil {
		slog.Error("[portal_wallet] erro no lock de antecipação", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	if pendingWd > 0 {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "Já existe um saque em análise. Aguarde a conclusão.")
		return
	}

	// Trava as tx pendentes FOR UPDATE (impede que o cron de release as solte
	// no meio da antecipação) e coleta os ids + soma o net.
	// FIX: usa COALESCE(NULLIF(net,0), gross) — sem isso os créditos COD do WP (net=0)
	// somariam amount=0 e o handler retornaria 422 "Não há saldo pendente para antecipar",
	// fazendo "Confirmar antecipação" falhar mesmo com recebíveis. Mesma regra do Future/Summary.
	rows, err := tx.Query(r.Context(),
		`SELECT id, COALESCE(NULLIF(net,0), gross)
		   FROM sz_cod_wallet_transactions
		  WHERE user_id = $1
		    AND status = 'pending'
		    AND release_at IS NOT NULL
		  ORDER BY release_at ASC
		  FOR UPDATE`,
		codKey,
	)
	if err != nil {
		slog.Error("[portal_wallet] erro ao travar recebíveis pendentes", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	var pendingIDs []int64
	var amount float64
	for rows.Next() {
		var id int64
		var net float64
		if err := rows.Scan(&id, &net); err != nil {
			rows.Close()
			slog.Error("[portal_wallet] erro ao ler recebível pendente", "user_id", u.ID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
			return
		}
		pendingIDs = append(pendingIDs, id)
		amount += net
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		slog.Error("[portal_wallet] erro após iterar recebíveis pendentes", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	amount = round2(amount)
	if amount <= 0 || len(pendingIDs) == 0 {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "Não há saldo pendente para antecipar.")
		return
	}

	fee := round2(amount * (antPct / 100))
	net := round2(amount - fee)
	if net < 0 {
		net = 0
	}
	pctStr := strconv.FormatFloat(antPct, 'f', 2, 64)
	desc := "Antecipação de recebíveis COD (" + pctStr + "%)"

	// Passo 5: marca as tx pendentes como 'anticipation_pending'.
	if _, err := tx.Exec(r.Context(),
		`UPDATE sz_cod_wallet_transactions
		    SET status = 'anticipation_pending', updated_at = NOW()
		  WHERE id = ANY($1)`,
		pendingIDs,
	); err != nil {
		slog.Error("[portal_wallet] erro ao marcar tx antecipadas", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "Não foi possível processar a antecipação. Tente novamente.")
		return
	}

	// Passo 6: ordem de antecipação já aprovada (antecipação é automática).
	// #94 — SEM conta/PIX: a antecipação não tem destino PIX (os campos pix_key/
	// pix_type/holder_name/holder_cpf ficam NULL — todos nullable no schema). Esta
	// linha existe para o livro de saques E para disparar o trigger de receita
	// (trg_sz_revenue_capture_codsaque book a taxa de antecipação como 'taxa_saque'
	// em status 'approved'/'paid' com fee>0). NÃO há coluna account_id no schema.
	var wdID int64
	if err := tx.QueryRow(r.Context(),
		`INSERT INTO sz_cod_withdrawals
		    (user_id, amount, fee, net, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, 'approved', NOW(), NOW())
		 RETURNING id`,
		codKey, amount, fee, net,
	).Scan(&wdID); err != nil {
		slog.Error("[portal_wallet] erro ao inserir saque de antecipação", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "Não foi possível registrar a antecipação. Tente novamente.")
		return
	}

	// Passo 7: débito contábil do pendente antecipado (net = -net).
	if _, err := tx.Exec(r.Context(),
		`INSERT INTO sz_cod_wallet_transactions
		    (user_id, type, status, amount, gross, fee, net, description, created_at, updated_at)
		 VALUES ($1, 'anticipation', 'approved', $2, $2, $3, $4, $5, NOW(), NOW())`,
		codKey, -amount, fee, -net,
		desc+" #"+strconv.FormatInt(wdID, 10),
	); err != nil {
		slog.Error("[portal_wallet] erro ao inserir débito de antecipação", "user_id", u.ID, "wd_id", wdID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "Não foi possível registrar a movimentação. Tente novamente.")
		return
	}

	// Passo 8: crédito líquido no disponível (net = +net).
	if _, err := tx.Exec(r.Context(),
		`INSERT INTO sz_cod_wallet_transactions
		    (user_id, type, status, amount, gross, fee, net, description, created_at, updated_at)
		 VALUES ($1, 'anticipation_credit', 'available', $2, $2, 0, $3, $4, NOW(), NOW())`,
		codKey, net, net,
		"Crédito de antecipação #"+strconv.FormatInt(wdID, 10)+" (líquido após taxa de "+pctStr+"%)",
	); err != nil {
		slog.Error("[portal_wallet] erro ao inserir crédito de antecipação", "user_id", u.ID, "wd_id", wdID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "Não foi possível registrar o crédito. Tente novamente.")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("[portal_wallet] erro ao commit da antecipação", "user_id", u.ID, "wd_id", wdID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	slog.Info("[portal_wallet] antecipação aprovada", "user_id", u.ID, "cod_key", codKey, "wd_id", wdID, "amount", amount, "fee", fee, "net", net)
	httpx.WriteOK(w, map[string]any{
		"success": true,
		"message": "Antecipação aprovada! Valor líquido já disponível para saque.",
		"fee":     fee,
		"net":     net,
		"wd_id":   wdID,
	})
}

// ── GET /portal/earnings-credited ─────────────────────────────────────────────
//
// EarningsCredited devolve o GANHO EFETIVAMENTE CREDITADO (vitalício) do usuário —
// a métrica que alimenta a barra de gamificação "rumo a R$ 1 mi" (ProgressTier) na
// sidebar do portal. Substitui o antigo cálculo de FATURAMENTO BRUTO (Σ valor dos
// pedidos via /portal/reports), que somava receita gerada e NÃO o que caiu na conta.
//
// Regra de negócio (decidida com o dono — NÃO alterar):
//
//   - AFILIADO / CLIENTE / signup → comissão LÍQUIDA creditada (lifetime) =
//     Σ commission(approved|paid) − Σ penalty(approved|paid)
//     em senderzz_affiliate_transactions, pelo VÍNCULO afiliado_id = wp_user_id
//     (a tabela de transações guarda affiliate_id = id do VÍNCULO; o wp_user_id mora
//     em senderzz_affiliates.afiliado_id — id-space canônico idêntico a Summary()).
//     EXCLUI pending/cancelled (não creditados) e withdrawal/anticipation_fee (saque,
//     não ganho). GREATEST(0, …) p/ nunca ficar negativo (afiliado só com penalty → 0).
//
//   - PRODUTOR → COD que caiu na carteira (lifetime) =
//     Σ COALESCE(NULLIF(net,0),gross) em sz_cod_wallet_transactions onde
//     type='cod_received' AND status IN ('available','paid'). SÓ cod_received já
//     liberado; NÃO conta pending nem saques/débitos (withdrawal). Chave por
//     codWalletKey(u) (= wp_user_id, ou id do portal p/ nativo) — idêntico a Summary().
//
//   - operador / demais papéis → 0.
//
// Resiliência (é só um widget): tabela ausente OU erro de query → creditado=0, nunca
// 500. A barra degrada para zero em vez de quebrar o layout da sidebar.
func (h *WalletHandler) EarningsCredited(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	// Detecção de papel — espelha Layout.tsx (isAffiliate/isProducer). Cliente/signup
	// cai no ramo afiliado. NÃO usa o helper isAffiliate() do pacote: ele só cobre
	// 'afiliado'/'cliente' e aqui precisamos da lista completa (afiliada/affiliate/client).
	role := strings.ToLower(strings.TrimSpace(u.Role))
	isAff := role == "affiliate" || role == "afiliado" || role == "afiliada" ||
		role == "cliente" || role == "client"
	isProd := role == "produtor" || role == "producer"

	var creditado float64

	switch {
	case isAff:
		// Comissão líquida creditada (lifetime). MESMO id-space de WalletHandler.Summary:
		// JOIN senderzz_affiliates a ON a.id = tx.affiliate_id WHERE a.afiliado_id = wp_user_id.
		// status IN ('approved','paid') = creditado; penalty subtrai (-ABS). GREATEST(0,…).
		if u.WPUserID > 0 {
			err := h.Pool.QueryRow(r.Context(),
				`SELECT GREATEST(0, COALESCE(SUM(CASE
				    WHEN tx.type = 'commission' AND tx.status IN ('approved','paid') THEN tx.amount
				    WHEN tx.type = 'penalty'    AND tx.status IN ('approved','paid') THEN -ABS(tx.amount)
				    ELSE 0 END), 0))::float
				   FROM senderzz_affiliate_transactions tx
				   JOIN senderzz_affiliates a ON a.id = tx.affiliate_id
				  WHERE a.afiliado_id = $1`,
				u.WPUserID,
			).Scan(&creditado)
			if err != nil {
				// Tabela ausente / erro → degrada p/ 0 (não 500 — é só a barra de gamificação).
				slog.Warn("[portal_earnings] afiliado: erro/tabela ausente — creditado=0",
					"user_id", u.ID, "wp_user_id", u.WPUserID, "err", err)
				creditado = 0
			}
		}

	case isProd:
		// COD creditado na carteira (lifetime) — só cod_received já liberado
		// (available/paid). NÃO conta pending nem o débito do saque (withdrawal).
		// COALESCE(NULLIF(net,0),gross): cod_received grava net=0 → cai no gross.
		codKey := codWalletKey(u)
		err := h.Pool.QueryRow(r.Context(),
			`SELECT GREATEST(0, COALESCE(SUM(COALESCE(NULLIF(net,0), gross)), 0))::float
			   FROM sz_cod_wallet_transactions
			  WHERE user_id = $1
			    AND type = 'cod_received'
			    AND status IN ('available','paid')`,
			codKey,
		).Scan(&creditado)
		if err != nil {
			slog.Warn("[portal_earnings] produtor: erro/tabela ausente — creditado=0",
				"user_id", u.ID, "cod_key", codKey, "err", err)
			creditado = 0
		}

	default:
		// operador / papel desconhecido → 0 (não tem ganho creditado nesta métrica).
		creditado = 0
	}

	httpx.WriteOK(w, map[string]any{"creditado": round2(creditado)})
}

// ── Transferência Carteira COD → Carteira de Expedição (TPC) ───────────────
//
// FEAT: produtor pede transferência de saldo disponível da Carteira COD para
// a Carteira de Expedição (frete pré-pago). NÃO executa na hora — vira pedido
// 'pending' em sz_wallet_freight_transfers, aprovado/rejeitado pelo admin
// (go/admin). Só produtor (afiliado tem workflow próprio de carteira; TPC não
// existe para afiliado/cliente).
//
// Débito imediato na solicitação (mesmo padrão do saque, ver Withdraw acima):
// lançamento type='freight_transfer' status='available' já sai do disponível,
// evitando double-spend enquanto o pedido aguarda aprovação. Se rejeitado, o
// admin estorna via type='refund' (go/admin).
//
// A propriedade da carteira TPC é SEMPRE senderzz_portal_users.id. A coluna
// wp_user_id da transferência é mantida apenas como referência legada; o admin
// credita portal_user_id e nunca escolhe o dono financeiro por wp_user_id.
type freightTransferRequest struct {
	Amount string `json:"amount"`
}

// ── POST /portal/wallet/freight-transfer ────────────────────────────────────
func (h *WalletHandler) FreightTransfer(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	if isAffiliate(u.Role) || u.Role == "client" {
		httpx.WriteErr(w, http.StatusForbidden, "transferência para carteira de frete disponível apenas para produtor")
		return
	}
	if h.isSubAccount(r, u.ID) {
		httpx.WriteErr(w, http.StatusForbidden, "Transferência não disponível para subcontas.")
		return
	}

	codKey := codWalletKey(u)

	var req freightTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}
	amount := round2(parseMoney(req.Amount))
	if amount < 10.0 {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "Valor mínimo para transferência é R$ 10,00.")
		return
	}

	tx, err := h.Pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		slog.Error("[portal_wallet] erro ao iniciar transação de transferência frete", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	// Mesmo advisory lock do saque/antecipação — serializa qualquer operação
	// concorrente sobre o disponível do MESMO usuário (P0 anti double-spend).
	if _, err := tx.Exec(r.Context(),
		`SELECT pg_advisory_xact_lock($1)`, codWalletLockKey(codKey),
	); err != nil {
		slog.Error("[portal_wallet] erro no advisory lock de transferência frete", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Guard: só 1 saque/transferência pendente por vez (mesmo guard do saque,
	// agora estendido para não deixar solicitar 2 transferências concorrentes).
	var pending int64
	if err := tx.QueryRow(r.Context(),
		`SELECT
		   (SELECT COUNT(*) FROM sz_cod_withdrawals WHERE user_id=$1 AND status IN ('analysis','pending','approved'))
		 + (SELECT COUNT(*) FROM sz_wallet_freight_transfers WHERE user_id=$1 AND status='pending')`,
		codKey,
	).Scan(&pending); err != nil {
		slog.Error("[portal_wallet] erro no lock de transferência frete", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	if pending > 0 {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "Já existe um saque ou transferência em análise. Aguarde a conclusão.")
		return
	}

	// Saldo disponível — mesma fórmula canônica do Withdraw (espelha Summary).
	var available float64
	if err := tx.QueryRow(r.Context(),
		`SELECT COALESCE(SUM(
		           CASE WHEN type IN ('withdrawal','freight_transfer') THEN gross
		                ELSE COALESCE(NULLIF(net, 0), gross) END
		        ), 0)
		   FROM sz_cod_wallet_transactions
		  WHERE user_id = $1 AND status = 'available'`,
		codKey,
	).Scan(&available); err != nil {
		slog.Error("[portal_wallet] erro ao ler saldo disponível (transferência frete)", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	available = round2(available)
	if amount <= 0 || amount > available {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "Valor indisponível para transferência.")
		return
	}

	var wftID int64
	if err := tx.QueryRow(r.Context(),
		`INSERT INTO sz_wallet_freight_transfers
		    (user_id, portal_user_id, wp_user_id, amount, status, requested_at)
		 VALUES ($1, $2, $3, $4, 'pending', NOW())
		 RETURNING id`,
		codKey, u.ID, u.WPUserID, amount,
	).Scan(&wftID); err != nil {
		slog.Error("[portal_wallet] erro ao inserir transferência frete", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "Não foi possível registrar a transferência. Tente novamente.")
		return
	}

	if _, err := tx.Exec(r.Context(),
		`INSERT INTO sz_cod_wallet_transactions
		    (user_id, type, status, amount, gross, fee, net, description, created_at, updated_at)
		 VALUES ($1, 'freight_transfer', 'available', $2, $3, 0, $4, $5, NOW(), NOW())`,
		codKey, -amount, -amount, -amount,
		"Transferência p/ carteira de frete #"+strconv.FormatInt(wftID, 10)+" (aguardando aprovação)",
	); err != nil {
		slog.Error("[portal_wallet] erro ao inserir lançamento de transferência frete", "user_id", u.ID, "wft_id", wftID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "Não foi possível registrar a movimentação. Tente novamente.")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("[portal_wallet] erro ao commit de transferência frete", "user_id", u.ID, "wft_id", wftID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	slog.Info("[portal_wallet] transferência p/ frete solicitada", "user_id", u.ID, "cod_key", codKey, "wft_id", wftID, "amount", amount)
	httpx.WriteOK(w, map[string]any{
		"success": true,
		"message": "Transferência solicitada. Aguarde a aprovação do admin.",
		"id":      wftID,
	})
}

type freightTransferRow struct {
	ID          int64      `json:"id"`
	Amount      float64    `json:"amount"`
	Status      string     `json:"status"`
	AdminNote   string     `json:"admin_note"`
	RequestedAt time.Time  `json:"requested_at"`
	DecidedAt   *time.Time `json:"decided_at"`
}

// ── GET /portal/wallet/freight-transfers ────────────────────────────────────
func (h *WalletHandler) FreightTransfers(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	rows, err := h.Pool.Query(r.Context(),
		`SELECT id, amount, status, COALESCE(admin_note, ''), requested_at, decided_at
		   FROM sz_wallet_freight_transfers
		  WHERE portal_user_id = $1
		  ORDER BY requested_at DESC
		  LIMIT 100`,
		u.ID,
	)
	if err != nil {
		slog.Error("[portal_wallet] erro ao listar transferências frete", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer rows.Close()

	out := []freightTransferRow{}
	for rows.Next() {
		var it freightTransferRow
		if err := rows.Scan(&it.ID, &it.Amount, &it.Status, &it.AdminNote, &it.RequestedAt, &it.DecidedAt); err != nil {
			continue
		}
		out = append(out, it)
	}
	httpx.WriteOK(w, map[string]any{"data": out})
}
