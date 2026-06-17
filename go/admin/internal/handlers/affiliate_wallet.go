// Package handlers — endpoint admin para carteira de afiliados.
// Espelha src/Admin/Unified_Menu.php::tab_fin_carteira_afiliados() (PHP legado)
// e seções administrativas de includes/senderzz-affiliates.php sobre Postgres.
//
// Tabelas envolvidas:
//   - senderzz_affiliates             (cadastro do afiliado)
//   - senderzz_affiliate_wallet       (saldo agregado por afiliado)
//   - senderzz_affiliate_transactions (livro razão de comissão/penalidade/saque)
//   - senderzz_affiliate_withdrawals  (solicitações de saque)
package handlers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

// AffiliateWalletHandler agrupa endpoints da tela "Carteira de Afiliados".
type AffiliateWalletHandler struct{ Pool *pgxpool.Pool }

// ---------------------------------------------------------------------------
// Tipos de resposta
// ---------------------------------------------------------------------------

// AffWalletSummary — KPIs globais agregados em todos os afiliados.
// Espelha o painel topo da aba "Carteira de afiliados" do PHP legado.
type AffWalletSummary struct {
	TotalPendente    float64 `json:"total_pendente"`
	TotalDisponivel  float64 `json:"total_disponivel"`
	TotalDebt        float64 `json:"total_debt"`
	AffiliatesCount  int64   `json:"affiliates_count"`
}

// AffWalletRow — linha da tabela principal (um afiliado por linha).
type AffWalletRow struct {
	AffiliateID       int64   `json:"affiliate_id"`
	Nome              string  `json:"nome"`
	Email             string  `json:"email"`
	PendingBalance    float64 `json:"pending_balance"`
	Balance           float64 `json:"balance"`
	DebtAmount        float64 `json:"debt_amount"`
	SaquesTotal       float64 `json:"saques_total"`
	PenalidadesTotal  float64 `json:"penalidades_total"`
	PedidosValidos    int64   `json:"pedidos_validos"`
}

// AffWalletTx — transação individual exibida no drawer de detalhes.
type AffWalletTx struct {
	ID          int64   `json:"id"`
	OrderID     *int64  `json:"order_id"`
	Type        string  `json:"type"`
	Status      string  `json:"status"`
	Amount      float64 `json:"amount"`
	AvailableAt *string `json:"available_at"`
	MetaJSON    *string `json:"meta_json"`
	CreatedAt   string  `json:"created_at"`
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// tableExists — utilitário compartilhado com audit.go; verifica se a tabela
// existe no schema public antes de rodar a query. Graceful degradation:
// se tabelas ainda não foram migradas do MySQL, devolve resposta vazia.
func (h *AffiliateWalletHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// ---------------------------------------------------------------------------
// GET /affiliates-wallet/summary
// Soma global dos saldos. Tabelas ausentes contam como 0.
// ---------------------------------------------------------------------------
func (h *AffiliateWalletHandler) Summary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := AffWalletSummary{}

	if h.tableExists(ctx, "senderzz_affiliate_wallet") {
		// SUM com COALESCE para que tabela vazia devolva 0 e não NULL.
		_ = h.Pool.QueryRow(ctx,
			`SELECT
				COALESCE(SUM(pending_balance), 0),
				COALESCE(SUM(balance), 0),
				COALESCE(SUM(debt_amount), 0),
				COUNT(*)
			 FROM senderzz_affiliate_wallet`).
			Scan(&out.TotalPendente, &out.TotalDisponivel, &out.TotalDebt, &out.AffiliatesCount)
	}

	httpx.JSON(w, 200, out)
}

// ---------------------------------------------------------------------------
// GET /affiliates-wallet?limit=300&q=
// Lista afiliados com saldos + agregados (saques, penalidades, pedidos).
// Filtro `q` casa email OU nome via ILIKE.
// ---------------------------------------------------------------------------
func (h *AffiliateWalletHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 1000 {
		limit = 300
	}
	search := q.Get("q")

	// Se as tabelas básicas não existem (migração ainda não rodou) devolve vazio.
	if !h.tableExists(ctx, "senderzz_affiliates") ||
		!h.tableExists(ctx, "senderzz_affiliate_wallet") {
		httpx.JSON(w, 200, map[string]any{"items": []AffWalletRow{}})
		return
	}

	// LEFT JOIN com subqueries agregadas em senderzz_affiliate_transactions e
	// senderzz_affiliate_withdrawals. Tabelas opcionais (caso ainda não migradas)
	// são tratadas como 0 via condicional dinâmica.
	hasTx := h.tableExists(ctx, "senderzz_affiliate_transactions")
	hasWd := h.tableExists(ctx, "senderzz_affiliate_withdrawals")

	// Monta os blocos de subquery em PT-BR. Quando a tabela faltar, injeta zero.
	saquesSQL := "0::numeric"
	if hasWd {
		saquesSQL = `COALESCE((
			SELECT SUM(amount) FROM senderzz_affiliate_withdrawals wd
			WHERE wd.affiliate_id = a.id
			  AND wd.status IN ('approved','paid')
		), 0)`
	}

	penaSQL := "0::numeric"
	pedidosSQL := "0::bigint"
	if hasTx {
		penaSQL = `COALESCE((
			SELECT SUM(amount) FROM senderzz_affiliate_transactions tx
			WHERE tx.affiliate_id = a.id
			  AND tx.type = 'penalty'
			  AND tx.status <> 'cancelled'
		), 0)`
		pedidosSQL = `COALESCE((
			SELECT COUNT(DISTINCT order_id) FROM senderzz_affiliate_transactions tx
			WHERE tx.affiliate_id = a.id
			  AND tx.type = 'commission'
			  AND tx.status <> 'cancelled'
			  AND tx.order_id IS NOT NULL
		), 0)`
	}

	sql := `
		SELECT
			a.id,
			COALESCE(u.nome, '')  AS nome,
			COALESCE(u.email, '') AS email,
			COALESCE(w.pending_balance, 0) AS pending_balance,
			COALESCE(w.balance, 0)         AS balance,
			COALESCE(w.debt_amount, 0)     AS debt_amount,
			` + saquesSQL + `  AS saques_total,
			` + penaSQL + `    AS penalidades_total,
			` + pedidosSQL + ` AS pedidos_validos
		FROM senderzz_affiliates a
		LEFT JOIN senderzz_portal_users u   ON u.id = a.afiliado_id
		LEFT JOIN senderzz_affiliate_wallet w ON w.affiliate_id = a.id
		WHERE ($1 = ''
		       OR u.email ILIKE '%' || $1 || '%'
		       OR u.nome  ILIKE '%' || $1 || '%')
		ORDER BY COALESCE(w.balance, 0) DESC, a.id DESC
		LIMIT $2`

	rows, err := h.Pool.Query(ctx, sql, search, limit)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []AffWalletRow{}
	for rows.Next() {
		var x AffWalletRow
		if err := rows.Scan(
			&x.AffiliateID, &x.Nome, &x.Email,
			&x.PendingBalance, &x.Balance, &x.DebtAmount,
			&x.SaquesTotal, &x.PenalidadesTotal, &x.PedidosValidos,
		); err != nil {
			continue
		}
		out = append(out, x)
	}

	httpx.JSON(w, 200, map[string]any{"items": out})
}

// ---------------------------------------------------------------------------
// GET /affiliates-wallet/{id}/transactions?limit=200
// Lista transações do afiliado (livro razão). Ordenadas por id DESC.
// ---------------------------------------------------------------------------
func (h *AffiliateWalletHandler) Transactions(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	affID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || affID <= 0 {
		httpx.Err(w, 400, "bad_request", "affiliate_id inválido")
		return
	}
	ctx := r.Context()

	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 1000 {
		limit = 200
	}

	if !h.tableExists(ctx, "senderzz_affiliate_transactions") {
		httpx.JSON(w, 200, map[string]any{"items": []AffWalletTx{}})
		return
	}

	rows, err := h.Pool.Query(ctx,
		`SELECT id, order_id, type, status, amount,
		        available_at::text, meta_json::text, created_at::text
		 FROM senderzz_affiliate_transactions
		 WHERE affiliate_id = $1
		 ORDER BY id DESC
		 LIMIT $2`, affID, limit)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []AffWalletTx{}
	for rows.Next() {
		var t AffWalletTx
		if err := rows.Scan(&t.ID, &t.OrderID, &t.Type, &t.Status, &t.Amount,
			&t.AvailableAt, &t.MetaJSON, &t.CreatedAt); err != nil {
			continue
		}
		out = append(out, t)
	}

	httpx.JSON(w, 200, map[string]any{"items": out})
}

// ---------------------------------------------------------------------------
// POST /affiliates-wallet/{id}/wallet-fix
// Sincroniza balance / pending_balance do afiliado a partir do somatório das
// transações. Equivalente ao AuditHandler.FixAffiliateWallet — reimplementado
// aqui para coesão (a tela inteira mora neste arquivo).
// ---------------------------------------------------------------------------
func (h *AffiliateWalletHandler) WalletFix(w http.ResponseWriter, r *http.Request) {
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
		`UPDATE senderzz_affiliate_wallet
		 SET balance = COALESCE((
		       SELECT SUM(amount) FROM senderzz_affiliate_transactions
		       WHERE affiliate_id = $1 AND status = 'available'
		     ), 0),
		     pending_balance = COALESCE((
		       SELECT SUM(amount) FROM senderzz_affiliate_transactions
		       WHERE affiliate_id = $1 AND status = 'pending'
		     ), 0),
		     updated_at = NOW()
		 WHERE affiliate_id = $1`, affID)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 200, map[string]any{
		"ok":            true,
		"affiliate_id":  affID,
		"rows_affected": tag.RowsAffected(),
	})
}

// ---------------------------------------------------------------------------
// POST /affiliates-wallet/{id}/release-pending
// Promove transações pending → available cujo available_at já venceu.
// Em seguida chama wallet-fix para reagregar balance/pending_balance.
// ---------------------------------------------------------------------------
func (h *AffiliateWalletHandler) ReleasePending(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	affID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || affID <= 0 {
		httpx.Err(w, 400, "bad_request", "affiliate_id inválido")
		return
	}
	ctx := r.Context()

	if !h.tableExists(ctx, "senderzz_affiliate_transactions") {
		httpx.Err(w, 503, "tables_missing", "tabelas de afiliado ainda não migradas")
		return
	}

	// Passo 1: libera transações vencidas.
	tag, err := h.Pool.Exec(ctx,
		`UPDATE senderzz_affiliate_transactions
		 SET status = 'available'
		 WHERE affiliate_id = $1
		   AND type = 'commission'
		   AND status = 'pending'
		   AND available_at <= NOW()`, affID)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	released := tag.RowsAffected()

	// Passo 2: ressincroniza a carteira encadeando o wallet-fix.
	// Se a tabela da carteira não existe, retorna só o resultado da liberação.
	if !h.tableExists(ctx, "senderzz_affiliate_wallet") {
		httpx.JSON(w, 200, map[string]any{
			"ok":            true,
			"affiliate_id":  affID,
			"released":      released,
			"wallet_synced": false,
		})
		return
	}

	_, err = h.Pool.Exec(ctx,
		`UPDATE senderzz_affiliate_wallet
		 SET balance = COALESCE((
		       SELECT SUM(amount) FROM senderzz_affiliate_transactions
		       WHERE affiliate_id = $1 AND status = 'available'
		     ), 0),
		     pending_balance = COALESCE((
		       SELECT SUM(amount) FROM senderzz_affiliate_transactions
		       WHERE affiliate_id = $1 AND status = 'pending'
		     ), 0),
		     updated_at = NOW()
		 WHERE affiliate_id = $1`, affID)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 200, map[string]any{
		"ok":            true,
		"affiliate_id":  affID,
		"released":      released,
		"wallet_synced": true,
	})
}

// ---------------------------------------------------------------------------
// GET /affiliates-wallet/transaction-types
// Enum de tipos suportados pelo livro razão. Usado pelo frontend para popular
// chips de filtro no drawer de transações.
// ---------------------------------------------------------------------------
func (h *AffiliateWalletHandler) TransactionTypes(w http.ResponseWriter, r *http.Request) {
	types := []string{
		"commission",
		"penalty",
		"withdrawal",
		"approval",
		"manual_credit",
		"manual_debit",
		"frustration_reversal",
	}
	httpx.JSON(w, 200, map[string]any{"items": types})
}
