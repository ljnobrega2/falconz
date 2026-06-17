package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

type OrdersHandler struct{ Pool *pgxpool.Pool }

// pedido — linha da tabela sz_motoboy_pedidos enriquecida com dados de clientes,
// produto, afiliado e sz_order_id (necessário para /audit/fix-order/{id}).
type pedido struct {
	ID                int64   `json:"id"`
	WCOrderID         *int64  `json:"wc_order_id"`
	SzOrderID         *int64  `json:"sz_order_id"` // sz_orders.id — chave para audit-fix
	MotoboyID         *int64  `json:"motoboy_id"`
	Status            string  `json:"status"`
	Valor             float64 `json:"valor"`
	TaxaMotoboy       float64 `json:"taxa_motoboy"`        // mp.valor_taxa — repasse motoboy
	TaxaFrustrado     float64 `json:"taxa_frustrado"`      // mp.valor_taxa_frustrado
	DestNome          string  `json:"dest_nome"`
	DestCEP           string  `json:"dest_cep"`
	DestCidade        string  `json:"dest_cidade"`
	DestUF            string  `json:"dest_uf"`
	ClienteNome       string  `json:"cliente_nome"`
	Produto           string  `json:"produto"`
	AfiliadoNome      string  `json:"afiliado_nome"`
	OfertaLink        string  `json:"oferta_link"` // senderzz_affiliate_links.link_token (oferta usada)
	Comissao          float64 `json:"comissao"`
	CreatedAt         string  `json:"created_at"`
}

func (h *OrdersHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// ListMotoboy — lista pedidos sz_motoboy_pedidos com filtros:
//   - status: status do pedido
//   - date: data de criação (YYYY-MM-DD)
//   - cidade: substring case-insensitive em dest_cidade
//   - s: busca por wc_order_id numérico ou dest_nome ILIKE
//   - stopped: "1" → pedidos parados há 24h+ em status operacional
//   - limit: máx 200, default 50
//
// GET /orders/motoboy
func (h *OrdersHandler) ListMotoboy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	status := q.Get("status")
	date := q.Get("date")
	cidade := q.Get("cidade")
	search := q.Get("s")
	stopped := q.Get("stopped") == "1" || q.Get("stopped") == "true"

	// JOIN opcional com sz_orders para resolver sz_order_id (auditoria), afiliado e produto.
	hasSzOrders := h.tableExists(ctx, "sz_orders")
	hasPortalUsers := h.tableExists(ctx, "senderzz_portal_users")

	// Monta SELECT dinâmico conforme tabelas disponíveis.
	//
	// Campos base: sempre de sz_motoboy_pedidos.
	// Campos enriquecidos: via LEFT JOIN quando tabelas existem.

	szOrderSel := "NULL::bigint AS sz_order_id"
	szOrderJoin := ""
	afiliadoSel := "''::text AS afiliado_nome"
	produtoSel := "''::text AS produto"
	ofertaSel := "''::text AS oferta_link"

	if hasSzOrders {
		szOrderSel = "o.id AS sz_order_id"
		szOrderJoin = "LEFT JOIN sz_orders o ON o.wp_order_id = mp.wc_order_id"

		// Produto: primeiro item de sz_order_items via subquery escalar (sem fan-out).
		hasOrderItems := h.tableExists(ctx, "sz_order_items")
		if hasOrderItems {
			produtoSel = "COALESCE((SELECT nome FROM sz_order_items WHERE order_id = o.id ORDER BY id ASC LIMIT 1), '') AS produto"
		}

		// Afiliado: subquery escalar com prioridade wp_user_id > id para evitar
		// fan-out de um LEFT JOIN com condição OR (dois usuários podem ter ids cruzados).
		if hasPortalUsers {
			afiliadoSel = `COALESCE((SELECT pu.nome FROM senderzz_portal_users pu
			               WHERE pu.wp_user_id = o.affiliate_id OR pu.id = o.affiliate_id
			               ORDER BY CASE WHEN pu.wp_user_id = o.affiliate_id THEN 0 ELSE 1 END
			               LIMIT 1), '') AS afiliado_nome`
		}

		// Oferta (link_token): pega o link ativo associado ao vínculo do afiliado com o produtor.
		// Subquery escalar para evitar fan-out (afiliado pode ter múltiplos links).
		if h.tableExists(ctx, "senderzz_affiliate_links") && h.tableExists(ctx, "senderzz_affiliates") {
			ofertaSel = `COALESCE((
			              SELECT al.link_token
			              FROM senderzz_affiliate_links al
			              JOIN senderzz_affiliates sa ON sa.id = al.affiliate_id
			              WHERE sa.afiliado_id = o.affiliate_id
			                AND (o.produtor_id IS NULL OR sa.produtor_id = o.produtor_id)
			              ORDER BY al.active DESC, al.id ASC
			              LIMIT 1), '') AS oferta_link`
		}
	}

	// Constrói cláusulas WHERE dinâmicas.
	args := []any{}
	wheres := []string{}

	if stopped {
		// Parado: status operacional sem atualização há 24h+.
		wheres = append(wheres, "mp.status IN ('pendente','agendado','embalado','em_rota')")
		wheres = append(wheres, "mp.updated_at < NOW() - INTERVAL '24 hours'")
	} else {
		if status != "" {
			args = append(args, status)
			wheres = append(wheres, "mp.status = $"+strconv.Itoa(len(args)))
		}
		if date != "" {
			args = append(args, date)
			wheres = append(wheres, "DATE(mp.created_at) = $"+strconv.Itoa(len(args)))
		}
		if cidade != "" {
			args = append(args, "%"+strings.ToLower(cidade)+"%")
			wheres = append(wheres, "LOWER(COALESCE(mp.dest_cidade,'')) LIKE $"+strconv.Itoa(len(args)))
		}
		if search != "" {
			// Tenta match exato por wc_order_id numérico; também busca no nome do destinatário.
			wcID, errS := strconv.ParseInt(search, 10, 64)
			if errS == nil && wcID > 0 {
				args = append(args, wcID)
				args = append(args, "%"+strings.ToLower(search)+"%")
				wheres = append(wheres,
					"(mp.wc_order_id = $"+strconv.Itoa(len(args)-1)+
						" OR LOWER(COALESCE(mp.dest_nome,'')) LIKE $"+strconv.Itoa(len(args))+")")
			} else {
				args = append(args, "%"+strings.ToLower(search)+"%")
				wheres = append(wheres, "LOWER(COALESCE(mp.dest_nome,'')) LIKE $"+strconv.Itoa(len(args)))
			}
		}
	}

	whereSQL := ""
	if len(wheres) > 0 {
		whereSQL = "WHERE " + strings.Join(wheres, " AND ")
	}

	args = append(args, limit)
	limitArg := "$" + strconv.Itoa(len(args))

	// Coluna de comissão: de sz_orders se disponível, senão 0.
	comissaoSel := "0::float AS comissao"
	if hasSzOrders {
		comissaoSel = "COALESCE(o.affiliate_amount, 0) AS comissao"
	}

	sqlQ := `SELECT mp.id, mp.wc_order_id, ` + szOrderSel + `,
	                mp.motoboy_id, COALESCE(mp.status,''), COALESCE(mp.valor_pedido,0),
	                COALESCE(mp.valor_taxa,0), COALESCE(mp.valor_taxa_frustrado,0),
	                COALESCE(mp.dest_nome,''), COALESCE(mp.dest_cep,''),
	                COALESCE(mp.dest_cidade,''), COALESCE(mp.dest_uf,''),
	                COALESCE(mp.dest_nome,'') AS cliente_nome,
	                ` + produtoSel + `, ` + afiliadoSel + `, ` + ofertaSel + `, ` + comissaoSel + `,
	                mp.created_at::text
	         FROM sz_motoboy_pedidos mp
	         ` + szOrderJoin + `
	         ` + whereSQL + `
	         ORDER BY mp.id DESC LIMIT ` + limitArg

	rows, err := h.Pool.Query(ctx, sqlQ, args...)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []pedido{}
	for rows.Next() {
		var p pedido
		var szOrdID sql.NullInt64
		_ = rows.Scan(
			&p.ID, &p.WCOrderID, &szOrdID,
			&p.MotoboyID, &p.Status, &p.Valor,
			&p.TaxaMotoboy, &p.TaxaFrustrado,
			&p.DestNome, &p.DestCEP,
			&p.DestCidade, &p.DestUF, &p.ClienteNome,
			&p.Produto, &p.AfiliadoNome, &p.OfertaLink, &p.Comissao,
			&p.CreatedAt,
		)
		if szOrdID.Valid {
			p.SzOrderID = &szOrdID.Int64
		}
		out = append(out, p)
	}
	httpx.JSON(w, 200, map[string]any{"items": out})
}

// AuditFix executa correção financeira de um pedido motoboy.
// O {id} na URL é sz_motoboy_pedidos.id; resolve sz_orders.id via wc_order_id antes de corrigir.
// POST /orders/motoboy/{id}/audit-fix
func (h *OrdersHandler) AuditFix(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	mbPedidoID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || mbPedidoID <= 0 {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	ctx := r.Context()

	if !h.tableExists(ctx, "sz_motoboy_pedidos") {
		httpx.Err(w, 503, "tables_missing", "sz_motoboy_pedidos ainda não migrada")
		return
	}

	// Resolve wc_order_id → sz_orders.id
	var wcOrderID sql.NullInt64
	_ = h.Pool.QueryRow(ctx,
		`SELECT wc_order_id FROM sz_motoboy_pedidos WHERE id = $1 LIMIT 1`, mbPedidoID,
	).Scan(&wcOrderID)

	if !wcOrderID.Valid || wcOrderID.Int64 <= 0 {
		httpx.Err(w, 404, "not_found", "pedido motoboy não encontrado ou sem wc_order_id")
		return
	}

	if !h.tableExists(ctx, "sz_orders") {
		httpx.Err(w, 503, "tables_missing", "sz_orders ainda não migrada")
		return
	}

	var szOrderID sql.NullInt64
	_ = h.Pool.QueryRow(ctx,
		`SELECT id FROM sz_orders WHERE wp_order_id = $1 LIMIT 1`, wcOrderID.Int64,
	).Scan(&szOrderID)

	if !szOrderID.Valid || szOrderID.Int64 <= 0 {
		httpx.Err(w, 404, "not_found", "sz_orders sem linha para wp_order_id correspondente")
		return
	}

	orderID := szOrderID.Int64
	res := map[string]int{"missing_inserted": 0, "bad_transaction_updated": 0, "producer_wallet_updated": 0}

	if h.tableExists(ctx, "senderzz_affiliate_transactions") {
		tag, e := h.Pool.Exec(ctx,
			`INSERT INTO senderzz_affiliate_transactions
			   (order_id, affiliate_id, type, status, amount, available_at, meta_json, created_at)
			 SELECT o.id, o.affiliate_id, 'commission', 'pending',
			        COALESCE(o.affiliate_amount,0),
			        NOW() + INTERVAL '1 day' * COALESCE(o.retention_days, 7),
			        jsonb_build_object('source','admin_audit_fix_order'),
			        NOW()
			 FROM sz_orders o
			 LEFT JOIN senderzz_affiliate_transactions t
			   ON t.order_id = o.id AND t.type='commission' AND t.status <> 'cancelled'
			 WHERE o.id = $1
			   AND COALESCE(o.affiliate_id,0) > 0
			   AND COALESCE(o.affiliate_amount,0) > 0
			   AND t.id IS NULL`, orderID)
		if e == nil {
			res["missing_inserted"] = int(tag.RowsAffected())
		}

		tag, e = h.Pool.Exec(ctx,
			`UPDATE senderzz_affiliate_transactions t
			 SET amount = o.affiliate_amount,
			     meta_json = COALESCE(meta_json, '{}'::jsonb) || jsonb_build_object('source','admin_audit_fix_order','prev_amount', t.amount),
			     updated_at = NOW()
			 FROM sz_orders o
			 WHERE t.order_id = o.id
			   AND t.type='commission' AND t.status <> 'cancelled'
			   AND o.id = $1
			   AND ABS(COALESCE(o.affiliate_amount,0) - COALESCE(t.amount,0)) > 0.01`, orderID)
		if e == nil {
			res["bad_transaction_updated"] = int(tag.RowsAffected())
		}
	}

	if h.tableExists(ctx, "sz_cod_wallet_transactions") {
		tag, e := h.Pool.Exec(ctx,
			`UPDATE sz_cod_wallet_transactions c
			 SET gross = COALESCE(o.total,0),
			     fee = COALESCE(o.senderzz_fee,0) + COALESCE(o.delivery_fee,0),
			     net = COALESCE(o.producer_net,0),
			     updated_at = NOW()
			 FROM sz_orders o
			 WHERE c.order_id = o.id AND c.type='credit'
			   AND o.id = $1
			   AND ABS(COALESCE(o.producer_net,0) - COALESCE(c.net,0)) > 0.01`, orderID)
		if e == nil {
			res["producer_wallet_updated"] = int(tag.RowsAffected())
		}
	}

	httpx.JSON(w, 200, map[string]any{
		"ok":              true,
		"motoboy_pedido_id": mbPedidoID,
		"sz_order_id":    orderID,
		"result":         res,
	})
}
