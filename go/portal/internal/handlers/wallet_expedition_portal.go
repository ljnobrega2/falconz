// Package handlers — handler da Carteira de Expedição (TPC) do Portal V2 (user-scoped).
//
// Espelha templates/portal/v2/sections/wallet-expedition.php sobre Postgres.
// Esta é a carteira PRÉ-PAGA de FRETE (TPC — "Te Pago o Carteira"), distinta da
// Carteira COD (sz_cod_wallet_* em wallet.go). Aqui o produtor recarrega saldo via
// PIX e consome frete (Melhor Envio). Replica os dados + REGRAS da seção:
//   - KPI: saldo disponível (= saldo - saldo_reservado), com flag "saldo baixo" (< R$10)
//   - Histórico de movimentações (recargas e consumos de frete), com filtro de período
//
// Rotas (namespace /wp-json/senderzz/v1):
//
//	GET /portal/wallet-expedition/summary  — KPI de saldo (disponível/total/reservado)
//	GET /portal/wallet-expedition/history  — extrato TPC (?period= e/ou ?from=&to=)
//
// ── Espelho do PHP / governança (porte fiel) ────────────────────────────────
//
// Fonte de dados (includes/tpc/database.php — mesmas tabelas que o Go admin já lê
// em go/admin/internal/handlers/tpc_*.go):
//
//	tpc_carteira    — saldo atual por usuário (saldo, saldo_reservado)
//	tpc_transacoes  — ledger imutável de movimentações (tipo, valor, status, …)
//
// SCOPING (segurança — afiliado NUNCA recebe dados do produtor):
//
//	tpc_carteira.user_id e tpc_transacoes.user_id guardam o ID NATIVO do portal
//	(senderzz_portal_users.id). A migração de 2026-07-28 eliminou o namespace misto
//	com wp_user_id; consultar pelo ID WordPress divide a carteira em duas identidades.
//
//	Portanto o escopo aqui é SEMPRE u.ID, recortado pela sessão do portal.
//	Cada usuário (produtor OU afiliado) vê EXCLUSIVAMENTE a sua própria carteira TPC
//	(a sua, keyed pelo seu id nativo). Não há JOIN de vínculo produtor↔afiliado nesta
//	carteira — o id-space do dono é o do portal e jamais cruzamos id-spaces, então
//	é impossível um afiliado puxar a carteira de um produtor por aqui.
//
// Saldo disponível = max(0, saldo - saldo_reservado) — espelha tpc_get_saldo_disponivel().
//
// Histórico (espelha tpc_get_historico_carteira / a tabela renderizada no .php):
//
//	Colunas exibidas: Data, Descrição, Pedido, Tipo, Valor, Taxa, Líquido.
//	tpc_transacoes não tem colunas taxa/líquido — o .php cai no fallback fee=0 e
//	net=valor. Mantemos o mesmo contrato (Fee=0, Net=Valor) para UX idêntica.
//	O badge "Tipo" mapeia tipo→rótulo igual ao match() do .php:
//	  recarga/credito/credit/pix → "Recarga" (success)
//	  frete/debito/debit/label   → "Frete"   (neutral)
//	  estorno/refund             → "Estorno" (warning)
//	  (outros)                   → ucfirst(tipo) (neutral)
//
// Filtro de período (espelha as chips Hoje/7d/30d/Tudo + os date inputs from/to do .php):
//   - ?period=hoje|7d|30d|all (default 7d, igual à chip ativa do .php que nasce em 7d)
//   - ?from=YYYY-MM-DD&to=YYYY-MM-DD — quando ambos presentes, SOBRESCREVE o period
//     (o .php desativa as chips quando o usuário mexe nos date inputs).
//
// NOTA — Recarga PIX (Gerar PIX / verificar confirmação): o .php dispara via
// admin-ajax.php (action=senderzz_portal, szaction=generate_pix / check_pix), que
// continua sendo servido pelo PHP/WP nesta fase do strangler-fig. Este handler Go
// cobre o lado READ-ONLY (saldo + extrato) da seção; a emissão/confirmação de PIX
// permanece no WP até a migração da carteira PIX (go/wallet/*). Não duplicamos o
// fluxo de issuance aqui para não criar dois donos do mesmo workflow financeiro.
//
// Degradação graciosa: se tpc_carteira / tpc_transacoes ainda não existirem no
// espelho Postgres, devolve saldo 0 / extrato vazio em vez de 500 (mirror do .php,
// que cai no empty-state "Nenhuma movimentação encontrada").
//
// NOTA — coluna meta_json: o espelho Postgres de tpc_transacoes (schema-wallet.sql)
// não tem a coluna meta_json (existe só em senderzz_affiliate_transactions). Logo o
// extrato NÃO filtra a alocação interna do admin por meta_json — mesma postura do
// go/admin/.../tpc_transacoes.go (cujo WHERE também não a referencia). Referenciar
// meta_json aqui dispararia 42703 (column does not exist) → 500.
package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/portal-service/internal/auth"
	"github.com/senderzz/portal-service/internal/httpx"
)

// WalletexpeditionHandler agrupa as dependências do handler da Carteira de Expedição.
// Construção idêntica a WebhookHandler/WalletHandler — o integrador instancia com
// &WalletexpeditionHandler{Pool: pool} e registra as rotas igualmente.
type WalletexpeditionHandler struct {
	Pool *pgxpool.Pool
}

// Tetos do extrato TPC. AUDIT PERF-list-endpoints-hard-limit: a History busca
// limit+1 p/ detectar truncamento (has_more) sem COUNT. histAllLimit p/ ?period=all
// (espelha o LIMIT 1000 histórico); histWindowLimit p/ janela com datas (LIMIT 200).
const (
	histExpAllLimit    = 1000
	histExpWindowLimit = 200
)

// ── Tipos de resposta ───────────────────────────────────────────────────────

// walletExpeditionSummary — o KPI de saldo do topo da seção (espelha o KPI card).
// LowBalance reproduz $sz9we_saldo_baixo = $sz9we_saldo < 10.0 (cor + meta no .php).
type walletExpeditionSummary struct {
	Available  float64 `json:"available"`   // saldo disponível = max(0, saldo - reservado)
	Balance    float64 `json:"balance"`     // saldo bruto (tpc_carteira.saldo)
	Reserved   float64 `json:"reserved"`    // saldo_reservado (frete em reserva)
	LowBalance bool    `json:"low_balance"` // true = saldo < R$10 → "Saldo insuficiente para envios"
}

// walletExpeditionTx — uma linha do "Histórico" de movimentações TPC.
// Campos espelham as colunas da tabela do .php (Data, Descrição, Pedido, Tipo,
// Valor, Taxa, Líquido). TipoLabel/TipoBadge são derivados do tipo cru (match do .php).
type walletExpeditionTx struct {
	Date        string  `json:"date"`        // dd/mm/aaaa hh:mm
	Description string  `json:"description"` // descrição do lançamento
	Order       string  `json:"order"`       // "#123" ou "—"
	Tipo        string  `json:"tipo"`        // tipo cru (recarga|frete|estorno|…)
	TipoLabel   string  `json:"tipo_label"`  // rótulo legível (Recarga|Frete|Estorno|…)
	TipoBadge   string  `json:"tipo_badge"`  // success|neutral|warning (classe szv2-badge-*)
	Value       float64 `json:"value"`       // valor bruto
	Fee         float64 `json:"fee"`         // taxa (0 — TPC não tem coluna; mirror do .php)
	Net         float64 `json:"net"`         // líquido (= valor; mirror do .php)
	Status      string  `json:"status"`      // status cru (confirmado|pendente|…)
}

// ── Helpers ─────────────────────────────────────────────────────────────────

// tableExists — guarda de migração graceful (idêntico aos demais handlers portal).
func (h *WalletexpeditionHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// expeditionWalletUserID é a fonte única de identidade da Carteira de Expedição.
// WPUserID permanece apenas para integrações legadas; nunca define o dono de tpc_*.
func expeditionWalletUserID(u *auth.PortalUser) int64 {
	if u == nil {
		return 0
	}
	return u.ID
}

// tipoBadgeExpedition mapeia o tipo cru da transação para (badge, rótulo) — espelha
// fielmente o match( strtolower( $sz9we_tipo ) ) do wallet-expedition.php.
func tipoBadgeExpedition(tipo string) (badge, label string) {
	switch strings.ToLower(strings.TrimSpace(tipo)) {
	case "recarga", "credit", "credito", "pix":
		return "success", "Recarga"
	case "frete", "debit", "debito", "label":
		return "neutral", "Frete"
	case "estorno", "refund":
		return "warning", "Estorno"
	default:
		if tipo == "" {
			return "neutral", "—"
		}
		// ucfirst(tipo) — mirror do default do .php.
		return "neutral", strings.ToUpper(tipo[:1]) + tipo[1:]
	}
}

// expeditionHistoryRange resolve a janela [from,to] do extrato.
// Se from/to (YYYY-MM-DD) vierem ambos preenchidos, têm prioridade sobre period
// (espelha o .php: mexer nos date inputs desativa as chips de período).
// Caso contrário, usa as chips hoje/7d/30d/all via periodHistoryRange (reuso de wallet.go).
// Retorna (from, to, all) — all=true ignora a janela.
func expeditionHistoryRange(period, fromStr, toStr string) (time.Time, time.Time, bool) {
	fromStr = strings.TrimSpace(fromStr)
	toStr = strings.TrimSpace(toStr)
	if fromStr != "" && toStr != "" {
		if f, errF := time.Parse("2006-01-02", fromStr); errF == nil {
			if t, errT := time.Parse("2006-01-02", toStr); errT == nil {
				// to inclusivo até o fim do dia (espelha created_at <= '...23:59:59').
				to := time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 0, t.Location())
				return f, to, false
			}
		}
	}
	if period == "" {
		period = "7d" // chip ativa default no .php (szv2-period-btn--active em 7d)
	}
	return periodHistoryRange(period) // reuso do helper de wallet.go (hoje/7d/30d/all)
}

// ── GET /portal/wallet-expedition/summary ───────────────────────────────────

// Summary devolve o KPI de saldo da Carteira de Expedição do usuário autenticado.
// Escopo exclusivo por u.ID (id nativo do portal), inclusive quando existe
// WPUserID legado. Ver expeditionWalletUserID.
func (h *WalletexpeditionHandler) Summary(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	walletUserID := expeditionWalletUserID(u)

	out := walletExpeditionSummary{}

	// Tabela não migrada ainda — devolve saldo 0 em vez de 500 (mirror do empty-state).
	if !h.tableExists(r.Context(), "tpc_carteira") {
		httpx.WriteOK(w, map[string]any{"data": out, "scope": "tpc"})
		return
	}

	var saldo, reservado float64
	// COALESCE + LIMIT 1: usuário sem linha em tpc_carteira → saldo 0 (não 500).
	err := h.Pool.QueryRow(r.Context(),
		`SELECT COALESCE(saldo, 0), COALESCE(saldo_reservado, 0)
		   FROM tpc_carteira
		  WHERE user_id = $1
		  LIMIT 1`,
		walletUserID,
	).Scan(&saldo, &reservado)
	if err != nil {
		if err != pgx.ErrNoRows {
			slog.Error("[portal_wallet_expedition] erro ao ler saldo TPC", "user_id", u.ID, "err", err)
		}
		// Sem carteira (ErrNoRows) ou erro brando — trata como saldo 0 (mirror do .php).
		saldo, reservado = 0, 0
	}

	available := saldo - reservado
	if available < 0 {
		available = 0 // espelha round(max(0, …)) de tpc_get_saldo_disponivel().
	}

	out = walletExpeditionSummary{
		Available:  available,
		Balance:    saldo,
		Reserved:   reservado,
		LowBalance: available < 10.0, // $sz9we_saldo_baixo = $sz9we_saldo < 10.0
	}

	httpx.WriteOK(w, map[string]any{"data": out, "scope": "tpc"})
}

// ── GET /portal/wallet-expedition/history ───────────────────────────────────

// History devolve o extrato de movimentações TPC do usuário autenticado.
// Filtro ?period=hoje|7d|30d|all (default 7d) e/ou ?from=&to= (YYYY-MM-DD,
// ambos sobrescrevem period). Escopo SEMPRE pelo id nativo u.ID.
func (h *WalletexpeditionHandler) History(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	walletUserID := expeditionWalletUserID(u)

	q := r.URL.Query()
	from, to, all := expeditionHistoryRange(q.Get("period"), q.Get("from"), q.Get("to"))

	out := []walletExpeditionTx{}

	// Tabela não migrada ainda — devolve extrato vazio em vez de 500.
	if !h.tableExists(r.Context(), "tpc_transacoes") {
		emptyLimit := histExpWindowLimit
		if all {
			emptyLimit = histExpAllLimit
		}
		httpx.WriteOK(w, map[string]any{"data": out, "total": 0, "has_more": false, "limit": emptyLimit, "scope": "tpc"})
		return
	}

	// Base: ledger TPC do dono (id nativo do portal). O espelho Postgres de tpc_transacoes
	// (infra/postgres/schema-wallet.sql) NÃO possui a coluna meta_json — ela existe
	// apenas em senderzz_affiliate_transactions. Por isso NÃO filtramos alocação
	// interna do admin por meta_json aqui, exatamente como go/admin/.../tpc_transacoes.go,
	// cujo WHERE também não referencia meta_json (referenciá-la dispara 42703 → 500).
	base := `
		SELECT created_at,
		       COALESCE(tipo, ''),
		       COALESCE(valor, 0),
		       COALESCE(descricao, ''),
		       order_id,
		       COALESCE(status, 'confirmado')
		  FROM tpc_transacoes
		 WHERE user_id = $1`

	// N+1: busca 1 a mais que o teto efetivo p/ detectar truncamento sem COUNT.
	// // PERF-list-endpoints-hard-limit
	histLimit := histExpWindowLimit
	if all {
		histLimit = histExpAllLimit
	}

	var rows pgx.Rows
	var err error
	if all {
		rows, err = h.Pool.Query(r.Context(),
			base+` ORDER BY created_at DESC, id DESC LIMIT $2`,
			walletUserID, histLimit+1)
	} else {
		rows, err = h.Pool.Query(r.Context(),
			base+` AND created_at BETWEEN $2 AND $3 ORDER BY created_at DESC, id DESC LIMIT $4`,
			walletUserID, from, to, histLimit+1)
	}
	if err != nil {
		// 42P01 (tabela ausente) já tratada acima; aqui é erro real.
		slog.Error("[portal_wallet_expedition] erro no extrato TPC", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer rows.Close()

	for rows.Next() {
		var created time.Time
		var tipo, desc, status string
		var valor float64
		var orderID *int64
		if err := rows.Scan(&created, &tipo, &valor, &desc, &orderID, &status); err != nil {
			continue
		}
		badge, label := tipoBadgeExpedition(tipo)
		if desc == "" {
			desc = "—"
		}
		out = append(out, walletExpeditionTx{
			Date:        fmtDateTimeBR(created),
			Description: desc,
			Order:       fmtOrder(orderID),
			Tipo:        tipo,
			TipoLabel:   label,
			TipoBadge:   badge,
			Value:       valor,
			Fee:         0,     // TPC não tem coluna de taxa — mirror do fallback do .php
			Net:         valor, // líquido = valor — mirror do fallback do .php
			Status:      status,
		})
	}

	// has_more=true quando veio a linha extra → o front sabe que o extrato foi
	// truncado (antes a truncagem era silenciosa). Devolve só o teto. // PERF-list-endpoints-hard-limit
	hasMore := len(out) > histLimit
	if hasMore {
		out = out[:histLimit]
	}

	httpx.WriteOK(w, map[string]any{"data": out, "total": len(out), "has_more": hasMore, "limit": histLimit, "scope": "tpc"})
}
