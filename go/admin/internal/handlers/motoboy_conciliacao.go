// Package handlers — endpoint admin para conciliação bancária de motoboy.
//
// Espelha sz_mb_tab_conciliacao() em includes/motoboy/admin.php:2064 (§2.8)
// sobre Postgres. Cada linha é um pedido (sz_motoboy_pedidos) em status
// entregue ou frustrado dentro do range. Confronto:
//
//   - entrega → soma pgto_dinheiro + pgto_pix + pgto_cartao
//   - frustrado → valor_taxa_frustrado (taxa de tentativa)
//
// O "ganho" (sz_motoboy_ganhos) associado começa como pendente; a conciliação
// promove para 'disponivel' (liberado para carteira).
//
// Surface:
//
//	GET  /motoboy-conciliacao?date_from=&date_to=     — KPIs + lista (limite 1000)
//	POST /motoboy-conciliacao/{pedido_id}/conciliar   — promove pendente→disponivel
//
// Notas:
//   - Filtro de data espelha o PHP: DATE(COALESCE(baixa_at, ts_entregue,
//     ts_frustrado, updated_at, created_at)) BETWEEN $from AND $to.
//     NÃO filtra por created_at sozinho (sugestão do advisor item 6).
//   - total_validado é computado em SQL via CASE (advisor item 7).
//   - conciliacao = sz_motoboy_ganhos.status (pendente|disponivel|pago).
//     Se não houver ganho registrado, vira 'pendente' por default.
//   - PATH PARAM é pedido_id (PRIMARY KEY de sz_motoboy_pedidos), NÃO
//     wc_order_id — paridade com o spec do usuário (advisor item 3).
package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

type MotoboyConciliacaoHandler struct{ Pool *pgxpool.Pool }

// ─── Tipos de resposta ───────────────────────────────────────────────────

// ConcKPIs — 8 KPIs no topo da tela (4 contagens + 3 totais + taxa frustrado).
type ConcKPIs struct {
	PedidosPeriodo        int64   `json:"pedidos_periodo"`
	Entregues             int64   `json:"entregues"`
	Frustrados            int64   `json:"frustrados"`
	AguardandoConciliacao int64   `json:"aguardando_conciliacao"`
	TotalDinheiro         float64 `json:"total_dinheiro"`
	TotalPix              float64 `json:"total_pix"`
	TotalCartao           float64 `json:"total_cartao"`
	TaxaFrustradoPct      float64 `json:"taxa_frustrado_pct"`
}

// ConcItem — linha da tabela de conciliação (1 pedido por linha).
type ConcItem struct {
	PedidoID       int64   `json:"pedido_id"`
	Data           string  `json:"data"`
	WcOrderID      int64   `json:"wc_order_id"`
	MotoboyID      *int64  `json:"motoboy_id"`
	MotoboyNome    string  `json:"motoboy_nome"`
	ZonaNome       string  `json:"zona_nome"`
	DestNome       string  `json:"dest_nome"`
	Status         string  `json:"status"`
	Forma          string  `json:"forma"`
	PgtoDinheiro   float64 `json:"pgto_dinheiro"`
	PgtoPix        float64 `json:"pgto_pix"`
	PgtoCartao     float64 `json:"pgto_cartao"`
	TaxaFrustrado  float64 `json:"taxa_frustrado"`
	TotalValidado  float64 `json:"total_validado"`
	Conciliacao    string  `json:"conciliacao"`
}

// ConcResponse — payload completo (kpis + items).
type ConcResponse struct {
	KPIs  ConcKPIs   `json:"kpis"`
	Items []ConcItem `json:"items"`
}

// ─── Helpers ─────────────────────────────────────────────────────────────

func (h *MotoboyConciliacaoHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// parseConcDateRange — aceita date_from/date_to em YYYY-MM-DD.
// Default: últimos 7 dias até hoje (paridade com $default_inicio em admin.php:2073).
// Se from > to, faz swap (paridade com o block "if strtotime(\$inicio) > ...").
func parseConcDateRange(r *http.Request) (string, string) {
	q := r.URL.Query()
	from := strings.TrimSpace(q.Get("date_from"))
	to := strings.TrimSpace(q.Get("date_to"))
	today := time.Now()
	def_to := today.Format("2006-01-02")
	def_from := today.AddDate(0, 0, -7).Format("2006-01-02")

	if !isISODate(from) {
		from = def_from
	}
	if !isISODate(to) {
		to = def_to
	}
	if from > to { // strings YYYY-MM-DD comparam lexicograficamente como datas
		from, to = to, from
	}
	return from, to
}

// formaFromAmounts replica $formas → implode( ' + ', ... ) do PHP.
// Inclui o valor em R$ ao lado para deixar explícito no front (paridade com
// spec do usuário: "Dinheiro R$ 50,00 + PIX R$ 100,00").
func formaFromAmounts(status string, dinheiro, pix, cartao, taxa float64) string {
	parts := []string{}
	if dinheiro > 0 {
		parts = append(parts, fmt.Sprintf("Dinheiro R$ %s", brMoney(dinheiro)))
	}
	if pix > 0 {
		parts = append(parts, fmt.Sprintf("PIX R$ %s", brMoney(pix)))
	}
	if cartao > 0 {
		parts = append(parts, fmt.Sprintf("Cartão R$ %s", brMoney(cartao)))
	}
	if status == "frustrado" {
		if taxa > 0 {
			parts = append(parts, fmt.Sprintf("Frustrado R$ %s", brMoney(taxa)))
		} else {
			parts = append(parts, "Frustrado")
		}
	}
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, " + ")
}

// brMoney formata float como "1.234,56" (separador BR).
func brMoney(v float64) string {
	// 2 casas com vírgula + milhares com ponto.
	s := strconv.FormatFloat(v, 'f', 2, 64)
	parts := strings.SplitN(s, ".", 2)
	intPart := parts[0]
	decPart := "00"
	if len(parts) == 2 {
		decPart = parts[1]
	}
	neg := false
	if strings.HasPrefix(intPart, "-") {
		neg = true
		intPart = intPart[1:]
	}
	// Insere ponto a cada 3 dígitos pela direita.
	var b strings.Builder
	n := len(intPart)
	for i, c := range intPart {
		if i > 0 && (n-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	out := b.String() + "," + decPart
	if neg {
		out = "-" + out
	}
	return out
}

// ─── Listagem + KPIs ─────────────────────────────────────────────────────

// List devolve KPIs + linhas de conciliação no range. Endpoint único para
// minimizar round-trips do front (mesma agregação que o PHP faz no foreach).
//
// GET /motoboy-conciliacao?date_from=YYYY-MM-DD&date_to=YYYY-MM-DD
func (h *MotoboyConciliacaoHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := ConcResponse{KPIs: ConcKPIs{}, Items: []ConcItem{}}

	if !h.tableExists(ctx, "sz_motoboy_pedidos") {
		httpx.JSON(w, 200, out)
		return
	}

	from, to := parseConcDateRange(r)

	// total_validado em SQL via CASE (advisor item 7).
	// data_baixa em SQL via COALESCE (advisor item 6).
	// JOIN com sz_motoboy_ganhos é LEFT — pedido sem ganho registrado vira 'pendente'.
	// motoboy_id efetivo é COALESCE(baixa_motoboy_id, motoboy_id) (paridade com PHP).
	rows, err := h.Pool.Query(ctx,
		`SELECT
		    mp.id                                          AS pedido_id,
		    COALESCE(
		      mp.baixa_at, mp.ts_entregue, mp.ts_frustrado,
		      mp.updated_at, mp.created_at
		    )::date::text                                  AS data_baixa,
		    COALESCE(mp.wc_order_id, 0)                    AS wc_order_id,
		    COALESCE(NULLIF(mp.baixa_motoboy_id, 0), mp.motoboy_id) AS motoboy_id,
		    COALESCE(m.nome, '')                           AS motoboy_nome,
		    COALESCE(z.nome, '')                           AS zona_nome,
		    COALESCE(mp.dest_nome, '')                     AS dest_nome,
		    COALESCE(mp.status, '')                        AS status,
		    COALESCE(mp.pgto_dinheiro, 0)::float8          AS pgto_dinheiro,
		    COALESCE(mp.pgto_pix, 0)::float8               AS pgto_pix,
		    COALESCE(mp.pgto_cartao, 0)::float8            AS pgto_cartao,
		    COALESCE(mp.valor_taxa_frustrado, 0)::float8   AS taxa_frustrado,
		    CASE
		      WHEN mp.status='frustrado'
		      THEN COALESCE(mp.valor_taxa_frustrado, 0)
		      ELSE COALESCE(mp.pgto_dinheiro, 0)
		         + COALESCE(mp.pgto_pix, 0)
		         + COALESCE(mp.pgto_cartao, 0)
		    END::float8                                    AS total_validado,
		    COALESCE(g.status, 'pendente')                 AS conciliacao
		  FROM sz_motoboy_pedidos mp
		  LEFT JOIN sz_motoboys m ON m.id = COALESCE(NULLIF(mp.baixa_motoboy_id, 0), mp.motoboy_id)
		  LEFT JOIN sz_motoboy_zonas z ON z.id = m.zona_id
		  LEFT JOIN sz_motoboy_ganhos g ON g.pedido_id = mp.id
		     AND g.tipo = CASE WHEN mp.status='frustrado' THEN 'frustrado' ELSE 'entrega' END
		  WHERE mp.status IN ('entregue','frustrado')
		    AND COALESCE(
		          mp.baixa_at, mp.ts_entregue, mp.ts_frustrado,
		          mp.updated_at, mp.created_at
		        )::date BETWEEN $1::date AND $2::date
		  ORDER BY data_baixa DESC, mp.wc_order_id DESC
		  LIMIT 1000`,
		from, to)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	// Agrega KPIs enquanto materializa items (paridade com $totais no PHP).
	for rows.Next() {
		var it ConcItem
		if err := rows.Scan(
			&it.PedidoID, &it.Data, &it.WcOrderID,
			&it.MotoboyID, &it.MotoboyNome, &it.ZonaNome, &it.DestNome,
			&it.Status,
			&it.PgtoDinheiro, &it.PgtoPix, &it.PgtoCartao,
			&it.TaxaFrustrado, &it.TotalValidado, &it.Conciliacao,
		); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
		it.Forma = formaFromAmounts(it.Status, it.PgtoDinheiro, it.PgtoPix, it.PgtoCartao, it.TaxaFrustrado)

		out.KPIs.PedidosPeriodo++
		if it.Status == "frustrado" {
			out.KPIs.Frustrados++
		} else {
			out.KPIs.Entregues++
			out.KPIs.TotalDinheiro += it.PgtoDinheiro
			out.KPIs.TotalPix += it.PgtoPix
			out.KPIs.TotalCartao += it.PgtoCartao
		}
		if it.Conciliacao == "pendente" {
			out.KPIs.AguardandoConciliacao++
		}

		out.Items = append(out.Items, it)
	}

	// Taxa frustrado% = frustrados / pedidos * 100. Zero-safe.
	if out.KPIs.PedidosPeriodo > 0 {
		out.KPIs.TaxaFrustradoPct = roundCents(
			float64(out.KPIs.Frustrados) * 100.0 / float64(out.KPIs.PedidosPeriodo))
	}
	// Arredonda totais em 2 casas para evitar ruído de float64.
	out.KPIs.TotalDinheiro = roundCents(out.KPIs.TotalDinheiro)
	out.KPIs.TotalPix = roundCents(out.KPIs.TotalPix)
	out.KPIs.TotalCartao = roundCents(out.KPIs.TotalCartao)

	httpx.JSON(w, 200, out)
}

// ─── Conciliar pedido ────────────────────────────────────────────────────

// Conciliar promove o(s) ganho(s) pendente(s) do pedido para 'disponivel'.
// Replica a action sz_mb_conciliar_pedido em admin.php:2080.
//
// Diferença vs PHP: o PHP filtra por wc_order_id; o spec do usuário pediu
// path param pedido_id (PK de sz_motoboy_pedidos). Usamos pedido_id —
// consistente com o JSON payload de /motoboy-conciliacao (advisor item 3).
//
// POST /motoboy-conciliacao/{pedido_id}/conciliar
func (h *MotoboyConciliacaoHandler) Conciliar(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_motoboy_ganhos") {
		httpx.Err(w, 503, "table_missing", "sz_motoboy_ganhos ainda não migrada")
		return
	}

	idStr := chi.URLParam(r, "pedido_id")
	pedidoID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || pedidoID <= 0 {
		httpx.Err(w, 400, "bad_request", "pedido_id inválido")
		return
	}

	// UPDATE direto — ganho pode ser único (entrega) ou ter mais de uma linha
	// se houve frustrado seguido de retentativa. O PHP só promove o primeiro;
	// aqui promovemos todos os pendentes do pedido (idempotente, mais robusto
	// pra casos de retentativa).
	tag, err := h.Pool.Exec(ctx,
		`UPDATE sz_motoboy_ganhos
		    SET status='disponivel'
		  WHERE pedido_id=$1 AND status='pendente'`,
		pedidoID)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	rowsUpdated := tag.RowsAffected()
	if rowsUpdated == 0 {
		httpx.JSON(w, 200, map[string]any{
			"ok":                  true,
			"ganhos_atualizados":  0,
			"hint":                "nenhum lançamento pendente para este pedido",
		})
		return
	}

	httpx.JSON(w, 200, map[string]any{
		"ok":                 true,
		"ganhos_atualizados": rowsUpdated,
	})
}
