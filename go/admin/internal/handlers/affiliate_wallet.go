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
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/auth"
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
	TotalPendente   float64 `json:"total_pendente"`
	TotalDisponivel float64 `json:"total_disponivel"`
	TotalDebt       float64 `json:"total_debt"`
	AffiliatesCount int64   `json:"affiliates_count"`
}

// AffWalletRow — linha da tabela principal (um AFILIADO/PESSOA por linha).
//
// MED29: a chave aqui é o wp_user_id da PESSOA (a.afiliado_id), não o id do
// vínculo (senderzz_affiliates.id). Um afiliado vinculado a N produtores tinha N
// linhas no MySQL/painel — agregamos por afiliado_id para mostrar UMA linha e
// somar as N carteiras-cache do mesmo afiliado. O campo `affiliate_id` exposto ao
// front passa a ser o afiliado_id (= wp_user_id) — as ações da linha (Ver tx /
// Liberar pendentes / Sync wallet) usam esse id e escopam por a.afiliado_id.
type AffWalletRow struct {
	AffiliateID      int64   `json:"affiliate_id"`
	Nome             string  `json:"nome"`
	Email            string  `json:"email"`
	PendingBalance   float64 `json:"pending_balance"`
	Balance          float64 `json:"balance"`
	DebtAmount       float64 `json:"debt_amount"`
	SaquesTotal      float64 `json:"saques_total"`
	PenalidadesTotal float64 `json:"penalidades_total"`
	PedidosValidos   int64   `json:"pedidos_validos"`
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
	return tableExistsCached(ctx, h.Pool, name) // AUDIT-2026-06-18 Onda2 (go-infoschema-cache)
}

// ---------------------------------------------------------------------------
// GET /affiliates-wallet/summary
// Soma global dos saldos. Tabelas ausentes contam como 0.
// ---------------------------------------------------------------------------
func (h *AffiliateWalletHandler) Summary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := AffWalletSummary{}

	// MED29: o COUNT(*) cru contava VÍNCULOS (uma linha de carteira por par
	// produtor/afiliado), inflando affiliates_count para afiliados multi-produtor.
	// As SOMAS de dinheiro permanecem corretas (cada carteira-cache reflete só o
	// próprio vínculo → somar todas é aditivo, não duplicado). Só a CONTAGEM
	// precisava ser por PESSOA: COUNT(DISTINCT a.afiliado_id).
	//
	// Alinhamos a população com a List (INNER JOIN em senderzz_portal_users
	// role='afiliado', que descarta vínculos órfãos) para que o "X de Y" do front
	// concilie: total da tabela == affiliates_count do KPI.
	if h.tableExists(ctx, "senderzz_affiliates") &&
		h.tableExists(ctx, "senderzz_affiliate_transactions") {
		// AUDIT-FINANCEIRO-2026-06-25: somas LIVE do livro razão (não do cache
		// senderzz_affiliate_wallet). Os 3 estados são apurados POR PESSOA numa derived
		// (GROUP BY afiliado_id) e só depois somados — o piso GREATEST(0,...) precisa
		// ser por afiliado, senão o saldo negativo de um cancelaria o positivo de outro.
		//   pendente  = SUM(comissão pending)
		//   disponivel= GREATEST(0, SUM(comissão approved) - SUM(ABS penalty approved))
		//   sacado    = SUM(saques liquidados, amount positivo) [debt_amount json]
		_ = h.Pool.QueryRow(ctx,
			`SELECT
				COALESCE(SUM(per.pendente), 0),
				COALESCE(SUM(per.disponivel), 0),
				COALESCE(SUM(per.sacado), 0),
				COUNT(*)
			 FROM (
				SELECT a.afiliado_id AS afid,
					COALESCE(SUM(CASE WHEN t.status = 'pending' AND t.type = 'commission'
					                  THEN t.amount ELSE 0 END), 0) AS pendente,
					-- AUDIT-2026-07-11: mesmo fix do List() — saque paga status='paid',
					-- não 'approved'; sem isto o Disponível nunca descontava o já sacado.
					GREATEST(0, COALESCE(SUM(CASE
					                  WHEN t.status IN ('pending','cancelled') THEN 0
					                  WHEN t.type = 'penalty' THEN -ABS(t.amount)
					                  ELSE t.amount
					                  END), 0)) AS disponivel,
					COALESCE(SUM(CASE WHEN t.type = 'withdrawal'
					                  AND t.status IN ('approved','paid','available')
					                  THEN ABS(t.amount) ELSE 0 END), 0) AS sacado
				FROM senderzz_affiliates a
				JOIN senderzz_portal_users u
				  ON u.wp_user_id = a.afiliado_id AND u.role = 'afiliado'
				LEFT JOIN senderzz_affiliate_transactions t ON t.affiliate_id = a.id
				GROUP BY a.afiliado_id
			 ) per`).
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
	// MED27: o front (AffiliateWallet.tsx) enviava data_ini/data_fim mas a List
	// nunca os bindava. Aplicamos como recorte de PERÍODO sobre as colunas de FLUXO
	// (saques/penalidades/pedidos) — inclusivo nos dois extremos (YYYY-MM-DD).
	// Saldos (balance/pending/debt) são ESTOQUE as-of-now (cache de carteira) e
	// NÃO são recortados por data — só os números de fluxo do período mudam.
	dataIni := q.Get("data_ini")
	dataFim := q.Get("data_fim")

	// Se as tabelas básicas não existem (migração ainda não rodou) devolve vazio.
	if !h.tableExists(ctx, "senderzz_affiliates") ||
		!h.tableExists(ctx, "senderzz_affiliate_transactions") {
		httpx.JSON(w, 200, map[string]any{"items": []AffWalletRow{}})
		return
	}

	// MED29: agregamos por PESSOA (a.afiliado_id = wp_user_id), não por vínculo.
	// - balance/pending/debt: SOMA do CACHE de carteira (senderzz_affiliate_wallet,
	//   PK = affiliate_id = id do vínculo, ≤1 linha por vínculo → 1:1 antes de somar,
	//   SEM fan-out). Mantém o propósito do botão "Sync wallet" (recomputa o cache) e
	//   casa List↔Summary (ambos leem o cache). Estoque, nunca recortado por data.
	// - saques/penalidades/pedidos: subqueries escalares escopadas por afiliado_id
	//   (mesma forma de affiliates.go) → fan-out-free. RECEBEM o recorte de data (MED27).
	hasTx := h.tableExists(ctx, "senderzz_affiliate_transactions")
	hasWd := h.tableExists(ctx, "senderzz_affiliate_withdrawals")

	// Monta os blocos de subquery de FLUXO em PT-BR. Quando a tabela faltar, injeta
	// zero. As subqueries são ESCALARES, escopadas pelo afiliado_id (coluna `p.afid`
	// da agregação interna) — ficam no nível EXTERNO (não dentro de agregados),
	// evitando o footgun de "MAX(subquery correlacionada a coluna agrupada)".
	// $3 = data_ini, $4 = data_fim (inclusivo). Coluna de data por tabela:
	// withdrawals → wd.requested_at; transactions → tx.created_at.
	saquesSQL := "0::numeric"
	if hasWd {
		saquesSQL = `COALESCE((
			SELECT SUM(wd.amount) FROM senderzz_affiliate_withdrawals wd
			  JOIN senderzz_affiliates aw ON aw.id = wd.affiliate_id
			WHERE aw.afiliado_id = p.afid
			  AND wd.status IN ('approved','paid')
			  AND ($3 = '' OR wd.requested_at >= ($3::date)::timestamp AT TIME ZONE 'America/Sao_Paulo')
			  AND ($4 = '' OR wd.requested_at <= (($4::date + 1)::timestamp AT TIME ZONE 'America/Sao_Paulo' - interval '1 second'))
		), 0)`
	}

	penaSQL := "0::numeric"
	pedidosSQL := "0::bigint"
	if hasTx {
		penaSQL = `COALESCE((
			SELECT SUM(tx.amount) FROM senderzz_affiliate_transactions tx
			  JOIN senderzz_affiliates at2 ON at2.id = tx.affiliate_id
			WHERE at2.afiliado_id = p.afid
			  AND tx.type = 'penalty'
			  AND tx.status <> 'cancelled'
			  AND ($3 = '' OR tx.created_at >= ($3::date)::timestamp AT TIME ZONE 'America/Sao_Paulo')
			  AND ($4 = '' OR tx.created_at <= (($4::date + 1)::timestamp AT TIME ZONE 'America/Sao_Paulo' - interval '1 second'))
		), 0)`
		// pedidos_validos: COUNT(DISTINCT order_id) por PESSOA — não SOMAR contagens
		// por vínculo (mesmo pedido pode aparecer em vínculos distintos).
		pedidosSQL = `COALESCE((
			SELECT COUNT(DISTINCT tx.order_id) FROM senderzz_affiliate_transactions tx
			  JOIN senderzz_affiliates at2 ON at2.id = tx.affiliate_id
			WHERE at2.afiliado_id = p.afid
			  AND tx.type = 'commission'
			  AND tx.status <> 'cancelled'
			  AND tx.order_id IS NOT NULL
			  AND ($3 = '' OR tx.created_at >= ($3::date)::timestamp AT TIME ZONE 'America/Sao_Paulo')
			  AND ($4 = '' OR tx.created_at <= (($4::date + 1)::timestamp AT TIME ZONE 'America/Sao_Paulo' - interval '1 second'))
		), 0)`
	}

	// Estrutura em duas camadas:
	//   1) `p` (derived) agrega o CACHE de carteira por PESSOA (GROUP BY afiliado_id):
	//      uma linha por afiliado, com balance/pending/debt somados dos N vínculos.
	//   2) o SELECT externo anexa as colunas de FLUXO como subqueries escalares
	//      (saques/penalidades/pedidos) escopadas por p.afid — sem aninhar em agregado.
	sql := `
		SELECT
			p.afid                AS afiliado_id,
			p.nome,
			p.email,
			p.pending_balance,
			p.balance,
			p.debt_amount,
			` + saquesSQL + `  AS saques_total,
			` + penaSQL + `    AS penalidades_total,
			` + pedidosSQL + ` AS pedidos_validos
		FROM (
			SELECT
				a.afiliado_id              AS afid,
				COALESCE(MAX(u.nome), '')  AS nome,
				COALESCE(MAX(u.email), '') AS email,
				-- AUDIT-FINANCEIRO-2026-06-25: saldos LIVE do livro razão (não do cache
				-- senderzz_affiliate_wallet). pending_balance=comissão pending; balance=
				-- GREATEST(0, comissão approved - ABS penalty approved) por PESSOA;
				-- debt_amount[json]=SACADO (saques liquidados, amount positivo).
				COALESCE(SUM(CASE WHEN t.status = 'pending' AND t.type = 'commission'
				                  THEN t.amount ELSE 0 END), 0) AS pending_balance,
				-- AUDIT-2026-07-11: Disponível SÓ olhava status='approved', mas saque
				-- pago grava status='paid' (não 'approved') — o saque nunca era
				-- descontado do Disponível, deixando "Disponível == Saques" mesmo
				-- depois de sacado tudo (confusão saque x disponível). Fix: qualquer
				-- lançamento resolvido (fora pending/cancelled) entra na conta; penalty
				-- inverte sinal (grava positivo), withdrawal/anticipation_fee já
				-- gravam amount NEGATIVO (subtraem sozinhos). Mesma fórmula do cache
				-- em FixAffiliateWallet (linha ~527) — unificada.
				GREATEST(0, COALESCE(SUM(CASE
				                  WHEN t.status IN ('pending','cancelled') THEN 0
				                  WHEN t.type = 'penalty' THEN -ABS(t.amount)
				                  ELSE t.amount
				                  END), 0)) AS balance,
				COALESCE(SUM(CASE WHEN t.type = 'withdrawal'
				                  AND t.status IN ('approved','paid','available')
				                  THEN ABS(t.amount) ELSE 0 END), 0) AS debt_amount
			FROM senderzz_affiliates a
			-- afiliado_id é wp_user_id (mesmo id-space de sz_orders.affiliate_id). JOIN por
			-- wp_user_id resolve o nome certo; INNER JOIN descarta vínculos órfãos (sem
			-- usuário real → linhas com nome vazio que o dono pediu pra excluir).
			JOIN senderzz_portal_users u ON u.wp_user_id = a.afiliado_id AND u.role = 'afiliado'
			LEFT JOIN senderzz_affiliate_transactions t ON t.affiliate_id = a.id
			WHERE ($1 = ''
			       OR u.email ILIKE '%' || $1 || '%'
			       OR u.nome  ILIKE '%' || $1 || '%')
			GROUP BY a.afiliado_id
		) p
		ORDER BY pedidos_validos DESC, p.afid DESC
		LIMIT $2`

	// $3/$4 (data_ini/data_fim) só aparecem no texto quando hasWd || hasTx (os blocos
	// de fluxo). Se AMBAS as tabelas faltarem (degradação graciosa — cada tableExists
	// é independente por contrato deste arquivo), o SQL referencia só $1/$2 e bindar 4
	// params faria o Postgres rejeitar ("bind supplies 4, requires 2") → List 500.
	// Bind condicional mantém a contagem sempre casada.
	args := []any{search, limit}
	if hasWd || hasTx {
		args = append(args, dataIni, dataFim)
	}
	rows, err := h.Pool.Query(ctx, sql, args...)
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
//
// MED29: {id} é o afiliado_id (= wp_user_id) da PESSOA, não o id do vínculo. O
// drawer mostra UMA pessoa que pode ter N vínculos → unimos TODAS as transações
// de todos os vínculos dela via JOIN em senderzz_affiliates WHERE afiliado_id = $1
// (mesmo escopo de affiliate_dashboard_portal.go::aggregateLedger).
// ---------------------------------------------------------------------------
func (h *AffiliateWalletHandler) Transactions(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	affID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || affID <= 0 {
		httpx.Err(w, 400, "bad_request", "afiliado_id inválido")
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
		`SELECT tx.id, tx.order_id, tx.type, tx.status, tx.amount,
		        tx.available_at::text, tx.meta_json::text, tx.created_at::text
		 FROM senderzz_affiliate_transactions tx
		   JOIN senderzz_affiliates a ON a.id = tx.affiliate_id
		 WHERE a.afiliado_id = $1
		 ORDER BY tx.id DESC
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
		httpx.Err(w, 400, "bad_request", "afiliado_id inválido")
		return
	}
	ctx := r.Context()

	if !h.tableExists(ctx, "senderzz_affiliate_wallet") ||
		!h.tableExists(ctx, "senderzz_affiliate_transactions") {
		httpx.Err(w, 503, "tables_missing", "tabelas de afiliado ainda não migradas")
		return
	}

	// MED29: {id} é o afiliado_id (PESSOA). Recomputamos a carteira-cache de CADA
	// vínculo dela INDEPENDENTEMENTE — o JOIN-UPDATE casa w.affiliate_id = a.id e
	// escopa por a.afiliado_id = $1; a subquery de SUM continua por affiliate_id
	// (= w.affiliate_id, o vínculo), então cada carteira agrega só as próprias
	// transações. Assim a List (que SOMA os caches por pessoa) bate com o livro razão.
	//
	// AUDIT-2026-06-21 #HIGH-4: balance recomputado incluindo TODOS os lançamentos
	// liquidados, não só os 'approved'. O saque aprovado é gravado como type='withdrawal',
	// amount NEGATIVO (-$amount), status='available' (ver includes/senderzz-affiliates.php:4619-4622).
	// Filtrar só por 'approved' excluía esse débito e RESSUSCITAVA o valor já sacado
	// (double-spend financeiro a cada "Sincronizar carteira").
	// Regra correta: balance = SUM(amount) WHERE status NOT IN ('pending','cancelled')
	//   - inclui 'approved'/'available' (comissões liberadas +, saques -) — ambos liquidados;
	//   - 'cancelled' cobre estornos/reversões: sz_aff_reverse_order apenas marca a linha
	//     da comissão como 'cancelled' (NÃO insere linha compensatória negativa — ver
	//     includes/senderzz-affiliates.php:1722), então excluí-la zera o crédito estornado;
	//   - 'pending' fica fora de balance (vira pending_balance abaixo).
	// Regressão garantida: commission approved +500 + withdrawal available -500 => balance 0.
	//
	// CRIT-A (AUDIT-CRIT-AB): a penalidade de frustração (type='penalty') é uma DESPESA
	// do afiliado, mas o PHP a grava com amount POSITIVO (includes/senderzz-affiliates.php:1814)
	// — somá-la creditava indevidamente o sacável (R$68 a mais; afiliado wp28 mostrava R$51
	// que deveria ser R$0). A penalty já é receita da plataforma (senderzz_revenue.taxa_frustrado).
	// Correção fiel ao PHP (linhas 1811-1813: debit=min(balance,penalty), resto→debt):
	//   - penalty entra como -ABS(amount) (robusto se a fonte PHP passar a gravar negativo);
	//   - GREATEST(0,...) piso = não deixa o balance ficar negativo (excedente vira debt).
	tag, err := h.Pool.Exec(ctx,
		`UPDATE senderzz_affiliate_wallet w
		 SET balance = GREATEST(0, COALESCE((
		       SELECT SUM(CASE WHEN type = 'penalty' THEN -ABS(amount) ELSE amount END)
		       FROM senderzz_affiliate_transactions
		       WHERE affiliate_id = w.affiliate_id AND status NOT IN ('pending','cancelled')
		     ), 0)),
		     pending_balance = COALESCE((
		       SELECT SUM(amount) FROM senderzz_affiliate_transactions
		       WHERE affiliate_id = w.affiliate_id AND status = 'pending'
		     ), 0),
		     updated_at = NOW()
		 FROM senderzz_affiliates a
		 WHERE w.affiliate_id = a.id AND a.afiliado_id = $1`, affID)
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
// POST /affiliates-wallet/{id}/release-pending[?force=1]
// Promove transações pending → approved.
//   - SEM force: respeita a retenção (só available_at já vencida OU NULL).
//   - COM force=1: OVERRIDE do dono — libera AGORA ignorando o prazo de retenção.
//
// Em AMBOS carimba available_at = NOW() ao aprovar e devolve still_pending +
// next_release_at p/ o front exibir um toast honesto. Depois chama wallet-fix.
// ---------------------------------------------------------------------------
func (h *AffiliateWalletHandler) ReleasePending(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	affID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || affID <= 0 {
		httpx.Err(w, 400, "bad_request", "afiliado_id inválido")
		return
	}
	ctx := r.Context()

	if !h.tableExists(ctx, "senderzz_affiliate_transactions") {
		httpx.Err(w, 503, "tables_missing", "tabelas de afiliado ainda não migradas")
		return
	}

	// MED29: {id} é o afiliado_id (PESSOA). Liberamos as comissões de TODOS os
	// vínculos dela — escopo via JOIN em senderzz_affiliates por afiliado_id.
	//
	// force=1: OVERRIDE do dono — libera AGORA, ignorando a retenção (available_at no
	// futuro). Sem force: respeita o prazo, mas trata available_at NULL como vencida —
	// `NULL <= NOW()` é NULL/false e deixava pendente-sem-data PRESA pra sempre via
	// admin (a antecipação afiliado-side já trata `IS NULL OR > NOW()`, paridade aqui).
	// Em AMBOS os casos carimba available_at = NOW() ao aprovar (igual à antecipação) —
	// senão um gate de available_at downstream re-prenderia a linha e o dinheiro
	// liberado nunca ficaria sacável ("liberei e não atualizou nada").
	force := r.URL.Query().Get("force") == "1" || r.URL.Query().Get("force") == "true"
	maturityCond := "AND (tx.available_at IS NULL OR tx.available_at <= NOW())"
	if force {
		maturityCond = "" // dono libera independente do prazo de retenção
	}
	tag, err := h.Pool.Exec(ctx,
		`UPDATE senderzz_affiliate_transactions tx
		 SET status = 'approved', available_at = NOW(), updated_at = NOW()
		 FROM senderzz_affiliates a
		 WHERE tx.affiliate_id = a.id
		   AND a.afiliado_id = $1
		   AND tx.type = 'commission'
		   AND tx.status = 'pending' `+maturityCond, affID)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	released := tag.RowsAffected()

	// Toast honesto: quantas pendentes RESTARAM e quando a próxima vence. Sem isso o
	// front só vê "0 liberadas" e parece quebrado quando, na verdade, ainda há retenção.
	var stillPending int64
	var nextReleaseAt *time.Time
	_ = h.Pool.QueryRow(ctx,
		`SELECT COUNT(*), MIN(tx.available_at)
		   FROM senderzz_affiliate_transactions tx
		   JOIN senderzz_affiliates a ON a.id = tx.affiliate_id
		  WHERE a.afiliado_id = $1
		    AND tx.type = 'commission'
		    AND tx.status = 'pending'`, affID).Scan(&stillPending, &nextReleaseAt)
	var nextReleaseStr *string
	if nextReleaseAt != nil {
		s := nextReleaseAt.Format(time.RFC3339)
		nextReleaseStr = &s
	}

	// Auditoria: force-release move dinheiro real ANTES da retenção — registra quem/quanto.
	if force && released > 0 {
		var adminID int64
		if admin := auth.FromCtx(ctx); admin != nil {
			adminID = admin.ID
		}
		_, _ = h.Pool.Exec(ctx,
			`INSERT INTO senderzz_portal_audit_log (user_id, action, entity_type, entity_id, meta, created_at)
			 VALUES ($1, 'affiliate_release_pending_force', 'affiliate', $2,
			         jsonb_build_object('released', $3::bigint), NOW())`,
			adminID, affID, released)
	}

	// Passo 2: ressincroniza a carteira encadeando o wallet-fix.
	// Se a tabela da carteira não existe, retorna só o resultado da liberação.
	if !h.tableExists(ctx, "senderzz_affiliate_wallet") {
		httpx.JSON(w, 200, map[string]any{
			"ok":              true,
			"affiliate_id":    affID,
			"released":        released,
			"forced":          force,
			"still_pending":   stillPending,
			"next_release_at": nextReleaseStr,
			"wallet_synced":   false,
		})
		return
	}

	// AUDIT-2026-06-21 #HIGH-4: mesma correção do WalletFix — balance inclui débitos
	// de saque (type='withdrawal', status='available', amount<0). 'approved' sozinho
	// ressuscitava o valor já sacado (double-spend). JOIN-UPDATE recomputa cada
	// carteira-cache do afiliado independentemente (MED29).
	//
	// CRIT-A (AUDIT-CRIT-AB): penalty (despesa, gravada positiva no PHP) deve SUBTRAIR,
	// não somar — mesma expressão do WalletFix acima (-ABS + piso GREATEST(0,...)).
	_, err = h.Pool.Exec(ctx,
		`UPDATE senderzz_affiliate_wallet w
		 SET balance = GREATEST(0, COALESCE((
		       SELECT SUM(CASE WHEN type = 'penalty' THEN -ABS(amount) ELSE amount END)
		       FROM senderzz_affiliate_transactions
		       WHERE affiliate_id = w.affiliate_id AND status NOT IN ('pending','cancelled')
		     ), 0)),
		     pending_balance = COALESCE((
		       SELECT SUM(amount) FROM senderzz_affiliate_transactions
		       WHERE affiliate_id = w.affiliate_id AND status = 'pending'
		     ), 0),
		     updated_at = NOW()
		 FROM senderzz_affiliates a
		 WHERE w.affiliate_id = a.id AND a.afiliado_id = $1`, affID)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 200, map[string]any{
		"ok":              true,
		"affiliate_id":    affID,
		"released":        released,
		"forced":          force,
		"still_pending":   stillPending,
		"next_release_at": nextReleaseStr,
		"wallet_synced":   true,
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
