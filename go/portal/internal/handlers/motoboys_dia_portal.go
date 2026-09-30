// Package handlers — handler de "Motoboys — dia" do Portal V2 (user-scoped).
//
// Espelha templates/portal/v2/sections/motoboys-dia.php. A tela mostra, por
// motoboy, os pedidos do DIA atual com KPIs (entregues / frustrados / em rota /
// pendentes), taxa de entrega (barra de progresso), a lista de pedidos do dia e o
// total R$ do dia — além de um alerta de "pedidos sem motoboy atribuído hoje".
//
// Rotas (namespace /wp-json/senderzz/v1):
//
//	GET /portal/motoboys-dia — KPIs por motoboy do dia + pedidos sem motoboy (user-scoped)
//
// ── DESVIO DELIBERADO vs motoboys-dia.php ──────────────────────────────────────
//
// A section WP é OL-only (return early p/ afiliado) e agrega FROM sz_motoboys
// (TODOS os motoboys ativos, escopo GLOBAL do operador). Aqui a regra é
// USER-SCOPED: produtor vê os motoboys que carregam OS PEDIDOS DELE; afiliado vê
// os dele. Por isso a agregação parte de sz_motoboy_pedidos JOIN sz_orders (recorte
// por dono), NUNCA de sz_motoboys — bolt-on de filtro de produtor em cima da view
// global vazaria contagens cross-produtor. Consequência: motoboys sem pedidos DESTE
// usuário no dia simplesmente não aparecem (idêntico ao princípio de motoboy_portal.go,
// que sempre deriva de pedidos, nunca de uma lista global). O "sem motoboy" também
// é recortado pelo mesmo JOIN em sz_orders.
//
// A visibilidade de NAV (operator-only) é fato de front-end (dashboard-v2.php) e não
// governa o que a API computa. Aqui o escopo de dados é por dono, fail-closed.
//
// ── ESCOPO POR USUÁRIO (fail-closed, equality estrita — nunca OR/IN) ────────────
//
// CANONICAL id-space (idêntico a motoboy_portal.go / orders.go / expedicao.go):
//   - sz_orders attribution afiliado : o.affiliate_id = u.WPUserID  (NUNCA IN(id,wp_user_id))
//   - sz_orders attribution produtor : o.produtor_id  = u.ID        (portal id)
//   - operator/demais roles          : lista vazia (escopo por class_ids ainda não
//                                       migrado em sz_orders) — fail-closed.
//
// Segurança: afiliado NUNCA recebe dados do produtor. O recorte é só strict-equality;
// diferente de expedicao.go (que bloqueia afiliado com 403), aqui o afiliado VÊ o
// dele — o invariante inegociável é o escopo estrito, não o show/hide.
//
// ── PARIDADE DE KPI (CASE byte-for-byte com motoboys-dia.php) ───────────────────
//
//	entregues  = status = 'entregue'
//	frustrados = status = 'frustrado'
//	em_rota    = status IN ('em_rota','a_caminho')
//	pendentes  = status NOT IN ('entregue','frustrado','cancelado')
//	pct        = round(entregues / total * 100)   (0 se total = 0)
//
// Read-only: a section WP não tem mutações (só "Atualizar" = reload). Sem
// ccFeePct / dois-valores (cartão) — isto não é a tela de cobrança.
package handlers

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/portal-service/internal/auth"
	"github.com/senderzz/portal-service/internal/httpx"
)

// MotoboysdiaHandler agrupa as dependências do handler de "Motoboys — dia".
// Construção idêntica a MotoboyHandler/WebhookHandler (só Pool) — wiring uniforme
// no main.go. O próprio handler faz o user-scoping pela sessão.
type MotoboysdiaHandler struct {
	Pool *pgxpool.Pool
}

// mdPedidoRow — um pedido do dia na lista do card do motoboy (motoboys-dia.php:142-151).
type mdPedidoRow struct {
	WCOrderID   *int64  `json:"wc_order_id"`  // sz_motoboy_pedidos.wc_order_id (#número)
	DestNome    string  `json:"dest_nome"`    // dest_nome (cliente)
	Status      string  `json:"status"`       // status cru — badge/cor é client-side
	ValorPedido float64 `json:"valor_pedido"` // valor_pedido (R$)
}

// mdMotoboyCard — um card de motoboy do dia (motoboys-dia.php:72-165).
type mdMotoboyCard struct {
	ID         int64         `json:"id"`          // sz_motoboys.id
	Nome       string        `json:"nome"`        // sz_motoboys.nome
	Telefone   string        `json:"telefone"`    // sz_motoboys.telefone ('' → front mostra "—")
	Total      int64         `json:"total"`       // total de pedidos do dia
	Entregues  int64         `json:"entregues"`   // KPI verde
	Frustrados int64         `json:"frustrados"`  // KPI vermelho
	EmRota     int64         `json:"em_rota"`     // KPI laranja (brand)
	Pendentes  int64         `json:"pendentes"`   // KPI cinza
	Pct        int64         `json:"pct"`         // taxa de entrega (entregues/total*100)
	TotalValor float64       `json:"total_valor"` // total R$ do dia
	Pedidos    []mdPedidoRow `json:"pedidos"`     // pedidos do dia (limit listMotoboysDiaPedidosLimit)
	// PedidosHasMore: true quando o motoboy tem mais pedidos no dia que o teto exibido
	// (o KPI Total já traz a contagem real; este flag torna a truncagem explícita).
	// // PERF-list-endpoints-hard-limit
	PedidosHasMore bool `json:"pedidos_has_more"`
}

// listMotoboysDiaPedidosLimit é o teto de pedidos exibidos por motoboy no painel
// do dia (espelha o LIMIT 30 histórico de motoboys-dia.php). // PERF-list-endpoints-hard-limit
const listMotoboysDiaPedidosLimit = 30

// tableExists — guarda de migração graceful (idêntico aos demais handlers portal).
// Método próprio do struct: os de MotoboyHandler não são compartilháveis sem editar
// aquele arquivo (edit-only neste arquivo novo).
func (h *MotoboysdiaHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// columnExists — guarda de coluna (algumas colunas vieram em schema-fixes posteriores).
func (h *MotoboysdiaHandler) columnExists(ctx context.Context, table, column string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.columns
			WHERE table_schema='public' AND table_name=$1 AND column_name=$2
		)`, table, column).Scan(&ok)
	return ok
}

// blankMotoboyCardPII — APAGA a PII exposta no card do motoboy quando o usuário
// autenticado é AFILIADO: nome e telefone do MOTOBOY. Os KPIs, total e o ID (chave
// de front) permanecem. Espelha blankClientPII (motoboy_portal.go): apaga em vez
// de mascarar parcial (Art. 6º III — minimização). Produtor/operator não chamam.
func blankMotoboyCardPII(c *mdMotoboyCard) {
	c.Nome = ""
	c.Telefone = ""
}

// blankMotoboyPedidoPII — APAGA o nome do cliente final (dest_nome) na linha de
// pedido do card quando o usuário é AFILIADO. Mantém nº do pedido, status e valor.
func blankMotoboyPedidoPII(pr *mdPedidoRow) {
	pr.DestNome = ""
}

// ── GET /portal/motoboys-dia ────────────────────────────────────────────────────

// List retorna, por motoboy, os KPIs do DIA dos pedidos do usuário autenticado,
// escopados por role (strict-equality). Envelope:
//
//	{ ok:true, data:[mdMotoboyCard...], total:N, sem_motoboy:K,
//	  hoje:"2026-06-18", role:"produtor", is_affiliate:false }
//
// Roles sem escopo em sz_orders (operator/demais) → data vazia, sem_motoboy 0
// (fail-closed: jamais devolve pedidos de outro produtor).
func (h *MotoboysdiaHandler) List(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	ctx := r.Context()

	isAffiliate := isAffiliateRole(u.Role)

	// "Hoje" no fuso de São Paulo (America/Sao_Paulo, UTC−3) — paridade com
	// wp_date('Y-m-d') de motoboys-dia.php:20. NÃO usar time.Now().UTC(): às 21h
	// local viraria o dia seguinte e a tela de "motoboys do dia" mostraria amanhã
	// (vazio) enquanto o WP ainda mostra hoje. Fallback p/ UTC se a tzdata faltar.
	loc, locErr := time.LoadLocation("America/Sao_Paulo")
	if locErr != nil {
		loc = time.UTC
	}
	hoje := time.Now().In(loc).Format("2006-01-02")

	emptyOut := func() {
		httpx.WriteOK(w, map[string]any{
			"data": []mdMotoboyCard{}, "total": 0, "sem_motoboy": 0,
			"hoje": hoje, "role": u.Role, "is_affiliate": isAffiliate,
		})
	}

	// sz_orders / sz_motoboy_pedidos ainda não migradas → graceful vazio (não 503:
	// a tela tem fallback de empty-state; mantém a UX idêntica ao WP sem motoboys).
	if !h.tableExists(ctx, "sz_orders") || !h.tableExists(ctx, "sz_motoboy_pedidos") {
		emptyOut()
		return
	}

	// Escopo por role — equality estrita, NUNCA OR/IN (evita cross-attribution).
	var whereScope string
	var scopeArg int64
	switch {
	case isAffiliate:
		whereScope = "o.affiliate_id = $1"
		scopeArg = u.WPUserID
	case u.Role == "produtor":
		whereScope = "o.produtor_id = $1"
		scopeArg = u.ID
	default:
		// Operator (OL) e demais: escopo por class_ids ainda não migrado em sz_orders.
		// Vazio p/ NÃO mis-atribuir pedidos de outro produtor (fail-closed).
		slog.Info("[portal_motoboys_dia] role sem escopo em sz_orders — lista vazia",
			"user_id", u.ID, "role", u.Role)
		emptyOut()
		return
	}

	hasMotoboys := h.tableExists(ctx, "sz_motoboys")

	// created_at da linha de pedido pode não existir em schemas antigos → fallback
	// para o created_at do pedido WC (sz_orders.o.created_at).
	pedDate := "mp.created_at"
	if !h.columnExists(ctx, "sz_motoboy_pedidos", "created_at") {
		pedDate = "o.created_at"
	}

	// Nome/telefone do motoboy: só se a tabela existir (nenhum sibling consulta
	// sz_motoboys — não presumir). Sem ela, agregamos por motoboy_id com rótulo cru.
	nomeSel := "COALESCE(NULLIF(m.nome,''), 'Motoboy #' || mp.motoboy_id::text) AS nome"
	telSel := "COALESCE(m.telefone,'') AS telefone"
	motoboyJoin := "LEFT JOIN sz_motoboys m ON m.id = mp.motoboy_id"
	if !hasMotoboys {
		nomeSel = "('Motoboy #' || mp.motoboy_id::text) AS nome"
		telSel = "''::text AS telefone"
		motoboyJoin = ""
	}

	// ── Agregação POR MOTOBOY (parte de sz_motoboy_pedidos JOIN sz_orders) ───────
	// Só pedidos COM motoboy atribuído (motoboy_id > 0); os "sem motoboy" entram na
	// contagem separada abaixo. KPIs byte-for-byte com motoboys-dia.php:24-30.
	aggSQL := `
		SELECT mp.motoboy_id,
		       ` + nomeSel + `,
		       ` + telSel + `,
		       COUNT(mp.id)                                                              AS total,
		       SUM(CASE WHEN mp.status='entregue'  THEN 1 ELSE 0 END)                    AS entregues,
		       SUM(CASE WHEN mp.status='frustrado' THEN 1 ELSE 0 END)                    AS frustrados,
		       SUM(CASE WHEN mp.status IN ('em_rota','a_caminho') THEN 1 ELSE 0 END)     AS em_rota,
		       SUM(CASE WHEN mp.status NOT IN ('entregue','frustrado','cancelado') THEN 1 ELSE 0 END) AS pendentes,
		       SUM(COALESCE(mp.valor_pedido,0))::float                                   AS total_valor
		  FROM sz_motoboy_pedidos mp
		  JOIN sz_orders o ON COALESCE(o.wp_order_id, o.id) = mp.wc_order_id
		  ` + motoboyJoin + `
		 WHERE ` + whereScope + `
		   AND mp.motoboy_id IS NOT NULL AND mp.motoboy_id > 0
		   AND ` + pedDate + ` >= $2::date AND ` + pedDate + ` < $2::date + interval '1 day'
		 GROUP BY mp.motoboy_id, ` + nomeSelGroupCols(hasMotoboys) + `
		 ORDER BY nome ASC`

	rows, err := h.Pool.Query(ctx, aggSQL, scopeArg, hoje)
	if err != nil {
		slog.Error("[portal_motoboys_dia] erro ao agregar motoboys", "user_id", u.ID, "role", u.Role, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer rows.Close()

	cards := []mdMotoboyCard{}
	motoboyIDs := []int64{}
	for rows.Next() {
		var c mdMotoboyCard
		var totalValor float64
		if err := rows.Scan(
			&c.ID, &c.Nome, &c.Telefone,
			&c.Total, &c.Entregues, &c.Frustrados, &c.EmRota, &c.Pendentes,
			&totalValor,
		); err != nil {
			slog.Error("[portal_motoboys_dia] erro ao ler linha de agregação", "user_id", u.ID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao ler motoboys")
			return
		}
		c.TotalValor = totalValor
		if c.Total > 0 {
			c.Pct = int64(float64(c.Entregues)/float64(c.Total)*100 + 0.5) // round half-up (= PHP round)
		}
		// P1 LGPD: afiliado não vê nome/telefone do motoboy (só KPIs). O ORDER BY
		// nome no SQL é só ordenação server-side — não vaza nada ao cliente.
		if isAffiliate {
			blankMotoboyCardPII(&c)
		}
		c.Pedidos = []mdPedidoRow{}
		cards = append(cards, c)
		motoboyIDs = append(motoboyIDs, c.ID)
	}
	if rows.Err() != nil {
		slog.Error("[portal_motoboys_dia] erro após iteração de agregação", "user_id", u.ID, "err", rows.Err())
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao processar motoboys")
		return
	}

	// ── Pedidos do dia por motoboy (limit 30 cada) ───────────────────────────────
	// motoboys-dia.php:83-89: por motoboy, ordenado por id ASC. Recorte de dono via
	// o mesmo JOIN em sz_orders (segurança: jamais lista pedido de outro produtor).
	for i := range cards {
		pedSQL := `
			SELECT mp.wc_order_id,
			       COALESCE(mp.dest_nome,'')          AS dest_nome,
			       COALESCE(mp.status,'')             AS status,
			       COALESCE(mp.valor_pedido,0)::float AS valor_pedido
			  FROM sz_motoboy_pedidos mp
			  JOIN sz_orders o ON COALESCE(o.wp_order_id, o.id) = mp.wc_order_id
			 WHERE ` + whereScope + `
			   AND mp.motoboy_id = $2
			   AND ` + pedDate + ` >= $3::date AND ` + pedDate + ` < $3::date + interval '1 day'
			 ORDER BY mp.id ASC
			 LIMIT $4`
		// N+1: busca 1 a mais que o teto p/ sinalizar pedidos_has_more sem COUNT. // PERF-list-endpoints-hard-limit
		pRows, perr := h.Pool.Query(ctx, pedSQL, scopeArg, cards[i].ID, hoje, listMotoboysDiaPedidosLimit+1)
		if perr != nil {
			slog.Error("[portal_motoboys_dia] erro ao listar pedidos do motoboy",
				"user_id", u.ID, "motoboy_id", cards[i].ID, "err", perr)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
			return
		}
		for pRows.Next() {
			var pr mdPedidoRow
			var wcOrderID sql.NullInt64
			if err := pRows.Scan(&wcOrderID, &pr.DestNome, &pr.Status, &pr.ValorPedido); err != nil {
				pRows.Close()
				slog.Error("[portal_motoboys_dia] erro ao ler pedido do motoboy", "user_id", u.ID, "err", err)
				httpx.WriteErr(w, http.StatusInternalServerError, "erro ao ler pedidos")
				return
			}
			if wcOrderID.Valid {
				v := wcOrderID.Int64
				pr.WCOrderID = &v
			}
			// P1 LGPD: afiliado não vê o nome do cliente final (dest_nome).
			if isAffiliate {
				blankMotoboyPedidoPII(&pr)
			}
			cards[i].Pedidos = append(cards[i].Pedidos, pr)
		}
		if pRows.Err() != nil {
			pRows.Close()
			slog.Error("[portal_motoboys_dia] erro após iteração de pedidos", "user_id", u.ID, "err", pRows.Err())
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao processar pedidos")
			return
		}
		pRows.Close()

		// pedidos_has_more: veio a linha extra → corta e sinaliza. // PERF-list-endpoints-hard-limit
		if len(cards[i].Pedidos) > listMotoboysDiaPedidosLimit {
			cards[i].Pedidos = cards[i].Pedidos[:listMotoboysDiaPedidosLimit]
			cards[i].PedidosHasMore = true
		}
	}

	// ── Pedidos SEM motoboy hoje (motoboys-dia.php:41-46) ────────────────────────
	// Recortado pelo mesmo JOIN em sz_orders. Exclui cancelado/devolvido (idêntico ao WP).
	var semMotoboy int64
	semSQL := `
		SELECT COUNT(*)
		  FROM sz_motoboy_pedidos mp
		  JOIN sz_orders o ON COALESCE(o.wp_order_id, o.id) = mp.wc_order_id
		 WHERE ` + whereScope + `
		   AND (mp.motoboy_id IS NULL OR mp.motoboy_id = 0)
		   AND ` + pedDate + ` >= $2::date AND ` + pedDate + ` < $2::date + interval '1 day'
		   AND mp.status NOT IN ('cancelado','devolvido')`
	if err := h.Pool.QueryRow(ctx, semSQL, scopeArg, hoje).Scan(&semMotoboy); err != nil {
		// Best-effort: não derruba a tela por causa do alerta. Loga e segue com 0.
		slog.Warn("[portal_motoboys_dia] erro ao contar pedidos sem motoboy", "user_id", u.ID, "err", err)
		semMotoboy = 0
	}

	httpx.WriteOK(w, map[string]any{
		"data":         cards,
		"total":        len(cards),
		"sem_motoboy":  semMotoboy,
		"hoje":         hoje,
		"role":         u.Role,
		"is_affiliate": isAffiliate,
	})
}

// nomeSelGroupCols — colunas de sz_motoboys que precisam entrar no GROUP BY quando a
// tabela existe (nome/telefone vêm de m.*). Sem a tabela, agrega só por mp.motoboy_id.
func nomeSelGroupCols(hasMotoboys bool) string {
	if hasMotoboys {
		return "m.nome, m.telefone"
	}
	return "mp.motoboy_id"
}
