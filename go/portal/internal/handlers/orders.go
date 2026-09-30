// Package handlers — handler de Pedidos do Portal V2 (user-scoped).
//
// Espelha templates/portal/v2/sections/orders.php + a fonte de dados
// WC_MelhorEnvio\Portal\Portal_Orders::format_order (src/Portal/Portal_Orders.php).
//
// Rotas (namespace /wp-json/senderzz/v1):
//
//	GET /portal/orders       — lista pedidos visíveis do usuário autenticado (limit 300)
//	GET /portal/orders/{id}  — detalhe do pedido com breakdown financeiro ROLE-SCOPED
//
// A tela é READ-ONLY: o CSV é gerado client-side e mudança de status / troca de
// motoboy são EXCLUSIVAS do painel OL (section motoboys-dia). Não há rota de mutação
// aqui — não inventar toggle/cancel/status (viola a regra OL-only do CLAUDE.md).
//
// ── Breakdown financeiro role-scoped (GET /portal/orders/{id}) ─────────────────
//
// O detalhe expõe o breakdown financeiro JÁ FILTRADO PELO PAPEL da sessão — a
// filtragem é no BACKEND (segurança: afiliado não pode ver taxa de entrega / taxa /
// líquido do produtor nem a comissão bruta, NEM via API). Espelha a fórmula oficial
// de fees.go + o admin order_detail.go::fillFinanceiroAfiliado.
//
//   - PRODUTOR : breakdown completo (valor, bruta, taxa 4,99%, líquida afiliado,
//     taxa entrega) + FINAL = líquido do produtor. A taxa de transação É a do afiliado
//     (transaction_fee) e está dentro da BRUTA — não há linha separada de produtor.
//   - AFILIADO : SÓ a taxa de transação do afiliado (4,99%) + a comissão líquida dele.
//     Campos do produtor (taxa_entrega, liquido_produtor, comissao_afiliado_bruta)
//     NÃO são incluídos no JSON (ptr omitempty = nil).
//   - OPERATOR : 404 (sem escopo em sz_orders — igual à List).
//
// Escopo de PROPRIEDADE re-aplicado no {id} (mesmo WHERE da List): id fora do escopo
// → 404 (nunca 403 — não vaza existência). A filtragem por papel acontece DEPOIS de
// confirmar a posse.
//
// ── Escopo por usuário (fail-closed, equality estrita — nunca OR/IN) ───────────
//
// Espelha senderzz_current_user_order_scope (includes/senderzz-access-scope.php):
//   - role "produtor"           → WHERE o.produtor_id = $1   ($1 = portal id, u.ID)
//   - role "afiliado"/"affiliate"→ WHERE o.affiliate_id = $1 ($1 = wp_user_id, u.WPUserID)
//   - role "operator" (OL)       → escopo por class_ids (ainda não migrado em
//     sz_orders); retorna lista vazia para não
//     mis-atribuir pedidos de outro produtor.
//
// CANONICAL id-space (idêntico ao handler admin affiliates.go / order_detail.go):
//   - sz_orders attribution afiliado : o.affiliate_id = u.wp_user_id (NUNCA IN(id,wp_user_id))
//   - sz_orders attribution produtor : o.produtor_id  = u.id (portal id)
//   - nome do afiliado (display)     : subquery escalar OR + prioridade wp_user_id>id
//     (determinística, single-value → não cruza atribuição)
//
// total_no_ship (Valor exibido na tabela) — fiel a format_order:
//
//	se _senderzz_offer_value > 0 → total_no_ship = _senderzz_offer_value
//	senão                         → total_no_ship = max(0, total - shipping)
//	(pedido motoboy: shipping já entra zerado no cálculo do WP; aqui usamos a
//	 coluna shipping de sz_orders como melhor aproximação migrada).
package handlers

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/portal-service/internal/auth"
	"github.com/senderzz/portal-service/internal/httpx"
)

// OrdersHandler agrupa as dependências dos handlers de pedidos.
// Construção idêntica a WebhookHandler/IntegrationsHandler para wiring uniforme.
type OrdersHandler struct {
	Pool *pgxpool.Pool
}

// orderRow — uma linha da tabela de pedidos. Campos espelham os usados em
// orders.php (data-* dos <tr>) e em Portal_Orders::format_order.
type orderRow struct {
	ID                int64   `json:"id"`                  // sz_orders.id
	WCOrderID         *int64  `json:"wc_order_id"`         // sz_orders.wp_order_id (link p/ post.php)
	Number            string  `json:"number"`              // order_number (fallback wp_order_id)
	Status            string  `json:"status"`              // status cru — label/badge é client-side
	ClienteNome       string  `json:"cliente_nome"`        // billing.name (sz_order_addresses tipo=billing)
	ProductName       string  `json:"product_name"`        // products_label (offer_name → 1º item)
	SenderzzOfferName string  `json:"senderzz_offer_name"` // _senderzz_offer_name (== kit p/ filtro)
	AffiliateName     string  `json:"affiliate_name"`      // resolvido de o.affiliate_id
	TotalNoShip       float64 `json:"total_no_ship"`       // Valor (offer_value | total-shipping)
	DateMachine       string  `json:"date_machine"`        // created_at (Y-m-d H:i:s) — usado p/ filtro de data
	// DateBR — created_at já formatado no fuso BR (DD/MM/AAAA HH:MM) p/ EXIBIÇÃO humana
	// (CSV). NÃO serializado no JSON da List (json:"-"): o front filtra/ordena por
	// date_machine (ISO cru) — esse contrato de máquina não pode virar BR. O CSV, porém,
	// é lido por humano e não passa por nenhum formatador JS, então usa a versão BR daqui.
	DateBR string `json:"-"`
	DeliveryDate      *string `json:"delivery_date"`       // _sz_delivery_date | reagendado_para (YYYY-MM-DD)
	// Flags de UI por STATUS (puras — sem gate de role; o front combina com is_affiliate).
	// Calculadas sobre sz_orders.status (o.status), cujo vocabulário é
	// {aguardando, completo, frustrado, cancelled} — NÃO {agendado, embalado} (esse é o
	// vocabulário de sz_motoboy_pedidos.status, outro id-space). 'aguardando' é o estado
	// agendado/ativo do pedido em sz_orders → can_reagendar/can_cancelar; 'frustrado' → can_clone.
	// Casa com o guard das mutações (orders_mutations.go), que resolve o pedido motoboy do
	// pedido e gateia em sz_motoboy_pedidos.status: 'aguardando' mapeia sempre p/ mb='agendado'
	// (∈ statusReagendaveis/statusCancelaveis) e 'frustrado' p/ mb='frustrado' → flag-true ⟹ mutação-aceita.
	CanReagendar bool `json:"can_reagendar"`
	CanCancelar  bool `json:"can_cancelar"`
	CanClone     bool `json:"can_clone"`
}

// blankBuyerPIIForAffiliate — APAGA o nome do comprador (cliente final) quando o
// usuário autenticado é AFILIADO. Espelha o padrão de
// motoboy_portal.go::blankClientPII (apagar, não mascarar parcialmente).
//
// P1 LGPD — broken access control / Art. 6º III (minimização): o afiliado só pode
// ver nº do pedido, produto, comissão e status. O nome do comprador NÃO faz parte
// desse conjunto e vazava em /portal/orders (List/Detail/CSV). Apagamos de vez
// (string vazia) em vez de mascarar parcialmente — qualquer parte ainda seria PII e
// violaria a whitelist. Mantém o TIPO string ("" em vez de nil) p/ não mudar o shape
// do JSON/CSV (a coluna continua presente, só vazia) — o front não quebra.
//
// Aplicado SÓ p/ afiliado; produtor (dono do pedido) e operator (OL) seguem vendo
// o nome — eles têm base legal (execução do pedido / governança).
func blankBuyerNameForAffiliate(name string, isAffiliate bool) string {
	if isAffiliate {
		return ""
	}
	return name
}

// tableExists — guarda de migração graceful (idêntico aos handlers admin).
func (h *OrdersHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// ── GET /portal/orders ─────────────────────────────────────────────────────────

// List retorna os pedidos visíveis do usuário autenticado (limit 300),
// escopados por role. Filtros (produto/status/afiliado/kit/data) são aplicados
// no client (espelha o JS szV2OrFilter de orders.php) — aqui devolvemos o set
// completo já recortado pelo dono.
//
// Envelope:
//
//	{ ok:true, data:[orderRow...], total:N, role:"produtor", is_affiliate:false }
//
// is_affiliate=true esconde a coluna Afiliado no front (espelha $sz9or_is_aff).
func (h *OrdersHandler) List(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	ctx := r.Context()

	// sz_orders ainda não migrada → 503 (graceful, espelha admin order_detail).
	if !h.tableExists(ctx, "sz_orders") {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "sz_orders ainda não migrada")
		return
	}

	isAffiliate := u.Role == "afiliado" || u.Role == "affiliate" || u.Role == "afiliada" || u.Role == "cliente"

	// N+1: busca um a mais que o teto para saber se há mais (sem COUNT). // PERF-list-endpoints-hard-limit
	out, scoped, err := h.fetchOrders(ctx, u, listOrdersLimit+1)
	if err != nil {
		slog.Error("[portal_orders] erro ao listar pedidos", "user_id", u.ID, "role", u.Role, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	if !scoped {
		// Operator (OL) e demais roles: escopo por class_ids ainda não migrado.
		slog.Info("[portal_orders] role sem escopo em sz_orders — lista vazia",
			"user_id", u.ID, "role", u.Role)
	}

	// has_more=true quando veio a linha extra → o front sabe que a lista foi
	// truncada (antes a truncagem em 300 era silenciosa). Devolve só o teto.
	hasMore := len(out) > listOrdersLimit
	if hasMore {
		out = out[:listOrdersLimit]
	}

	httpx.WriteOK(w, map[string]any{
		"data":         out,
		"total":        len(out),
		"has_more":     hasMore,
		"limit":        listOrdersLimit,
		"role":         u.Role,
		"is_affiliate": isAffiliate,
	})
}

// fetchOrders monta e executa a query de pedidos escopada por role e devolve as
// linhas já formatadas (mesma regra de products_label / total_no_ship de format_order).
// scoped=false → role sem escopo migrado (operator/demais): out vem vazio e o caller
// trata como "lista vazia" (fail-closed, NÃO mis-atribui pedidos). Reusado por List
// e ExportCSV para garantir o MESMO recorte de propriedade nos dois caminhos.
// listOrdersLimit é o teto da listagem (espelha o LIMIT 300 histórico).
// AUDIT PERF-list-endpoints-hard-limit: a List busca limit+1 para detectar
// truncamento (has_more) sem COUNT extra; a ExportCSV usa um teto bem maior.
const listOrdersLimit = 300

func (h *OrdersHandler) fetchOrders(ctx context.Context, u *auth.PortalUser, limit int) ([]orderRow, bool, error) {
	isAffiliate := u.Role == "afiliado" || u.Role == "affiliate" || u.Role == "afiliada" || u.Role == "cliente"

	// Escopo por role — equality estrita, NUNCA OR/IN (evita cross-attribution).
	var whereSQL string
	var scopeArg int64
	switch {
	case isAffiliate:
		whereSQL = "WHERE o.affiliate_id = $1"
		scopeArg = u.WPUserID
	case u.Role == "produtor":
		whereSQL = "WHERE o.produtor_id = $1"
		scopeArg = u.ID
	default:
		// Operator (OL)/demais: sem escopo migrado → lista vazia (fail-closed).
		return []orderRow{}, false, nil
	}

	// Selects enriquecidos via subqueries escalares (sem fan-out), montados
	// conforme tabelas disponíveis. Espelha admin orders.go (afiliadoSel/produtoSel).
	hasMeta := h.tableExists(ctx, "sz_order_meta")
	hasItems := h.tableExists(ctx, "sz_order_items")
	hasAddr := h.tableExists(ctx, "sz_order_addresses")
	hasPortalUsers := h.tableExists(ctx, "senderzz_portal_users")
	hasMotoboy := h.tableExists(ctx, "sz_motoboy_pedidos")

	// _senderzz_offer_name — nome do kit (== senderzz_offer_name de format_order).
	offerNameSel := "''::text AS offer_name"
	// _senderzz_offer_value — quando > 0, substitui total_no_ship.
	offerValueSel := "0::float AS offer_value"
	if hasMeta {
		offerNameSel = `COALESCE((SELECT meta_value FROM sz_order_meta
		                 WHERE order_id = o.id AND meta_key='_senderzz_offer_name'
		                 LIMIT 1), '') AS offer_name`
		// Guard regex: meta não-numérica não derruba a listagem (cast-safe).
		offerValueSel = `COALESCE((SELECT CASE WHEN meta_value ~ '^[0-9]+(\.[0-9]+)?$'
		                                       THEN meta_value::numeric ELSE 0 END
		                  FROM sz_order_meta
		                  WHERE order_id = o.id AND meta_key='_senderzz_offer_value'
		                  LIMIT 1), 0)::float AS offer_value`
	}

	// Nome do produto = 1º item de sz_order_items (fallback de products_label).
	produtoSel := "''::text AS produto_nome"
	if hasItems {
		produtoSel = `COALESCE((SELECT nome FROM sz_order_items
		              WHERE order_id = o.id ORDER BY id ASC LIMIT 1), '') AS produto_nome`
	}

	// Cliente = nome do endereço de cobrança (billing) — espelha billing.name.
	clienteSel := "''::text AS cliente_nome"
	if hasAddr {
		clienteSel = `COALESCE((SELECT nome FROM sz_order_addresses
		              WHERE order_id = o.id
		              ORDER BY CASE WHEN tipo='billing' THEN 0 ELSE 1 END, id ASC
		              LIMIT 1), '') AS cliente_nome`
	}

	// Afiliado (display): subquery escalar com prioridade wp_user_id > id.
	// Determinística e single-value → segura contra cross-attribution.
	afiliadoSel := "''::text AS afiliado_nome"
	if hasPortalUsers {
		afiliadoSel = `COALESCE((SELECT pu.nome FROM senderzz_portal_users pu
		               WHERE pu.wp_user_id = o.affiliate_id OR pu.id = o.affiliate_id
		               ORDER BY CASE WHEN pu.wp_user_id = o.affiliate_id THEN 0 ELSE 1 END
		               LIMIT 1), '') AS afiliado_nome`
	}

	// Data de entrega: _sz_delivery_date (meta) → fallback sz_motoboy_pedidos.reagendado_para
	// (keyed por wc_order_id = o.wp_order_id). Espelha F6/F7 de orders.php. Subquery escalar.
	deliverySel := "NULL::text AS delivery_date"
	switch {
	case hasMeta && hasMotoboy:
		deliverySel = `COALESCE(
		    NULLIF((SELECT meta_value FROM sz_order_meta
		            WHERE order_id = o.id AND meta_key='_sz_delivery_date' LIMIT 1), ''),
		    (SELECT mp.reagendado_para::text FROM sz_motoboy_pedidos mp
		      WHERE mp.wc_order_id = COALESCE(o.wp_order_id, o.id) AND mp.reagendado_para IS NOT NULL
		      ORDER BY mp.id DESC LIMIT 1)
		) AS delivery_date`
	case hasMeta:
		deliverySel = `NULLIF((SELECT meta_value FROM sz_order_meta
		               WHERE order_id = o.id AND meta_key='_sz_delivery_date' LIMIT 1), '')
		               AS delivery_date`
	case hasMotoboy:
		deliverySel = `(SELECT mp.reagendado_para::text FROM sz_motoboy_pedidos mp
		                WHERE mp.wc_order_id = COALESCE(o.wp_order_id, o.id) AND mp.reagendado_para IS NOT NULL
		                ORDER BY mp.id DESC LIMIT 1) AS delivery_date`
	}

	// total_no_ship: offer_value vence; senão max(0, total - shipping). Fiel a format_order.
	sqlQ := `SELECT o.id, o.wp_order_id,
	                COALESCE(NULLIF(o.order_number,''), o.wp_order_id::text) AS number,
	                COALESCE(o.status,'') AS status,
	                ` + clienteSel + `,
	                ` + produtoSel + `,
	                ` + offerNameSel + `,
	                ` + offerValueSel + `,
	                COALESCE(o.total,0)::float    AS total,
	                COALESCE(o.shipping,0)::float AS shipping,
	                ` + afiliadoSel + `,
	                o.created_at::text AS created_at,
	                to_char(o.created_at AT TIME ZONE 'America/Sao_Paulo', 'DD/MM/YYYY HH24:MI') AS created_at_br,
	                ` + deliverySel + `
	         FROM sz_orders o
	         ` + whereSQL + `
	         ORDER BY o.created_at DESC
	         LIMIT $2`

	rows, err := h.Pool.Query(ctx, sqlQ, scopeArg, limit)
	if err != nil {
		return nil, true, err
	}
	defer rows.Close()

	out := []orderRow{}
	for rows.Next() {
		var (
			or          orderRow
			wcOrderID   sql.NullInt64
			offerName   string
			offerValue  float64
			produtoNome string
			total       float64
			shipping    float64
			delivery    sql.NullString
		)
		if err := rows.Scan(
			&or.ID, &wcOrderID, &or.Number, &or.Status,
			&or.ClienteNome, &produtoNome, &offerName, &offerValue,
			&total, &shipping, &or.AffiliateName,
			&or.DateMachine, &or.DateBR, &delivery,
		); err != nil {
			return nil, true, err
		}

		// P1 LGPD: afiliado não vê o nome do comprador (apaga após o Scan, antes de
		// montar a linha). Cobre List E ExportCSV (ambos passam por fetchOrders).
		or.ClienteNome = blankBuyerNameForAffiliate(or.ClienteNome, isAffiliate)

		if wcOrderID.Valid {
			v := wcOrderID.Int64
			or.WCOrderID = &v
		}

		// products_label: offer_name vence; senão 1º item.
		or.SenderzzOfferName = offerName
		if offerName != "" {
			or.ProductName = offerName
		} else {
			or.ProductName = produtoNome
		}

		// total_no_ship: offer_value vence; senão total - shipping (≥ 0).
		if offerValue > 0 {
			or.TotalNoShip = offerValue
		} else {
			v := total - shipping
			if v < 0 {
				v = 0
			}
			or.TotalNoShip = v
		}

		if delivery.Valid && delivery.String != "" {
			d := delivery.String
			or.DeliveryDate = &d
		}

		// Flags de UI por STATUS (puras — sem gate de role). Populadas aqui em fetchOrders
		// → cobrem List (afiliado E produtor; ambos passam por este caminho).
		// or.Status é sz_orders.status (vocabulário {aguardando, completo, frustrado, cancelled}):
		// 'aguardando' (estado agendado/ativo do pedido) → can_reagendar/can_cancelar;
		// 'frustrado' → can_clone. (NÃO usar 'agendado'/'embalado' aqui — esse vocabulário é
		// de sz_motoboy_pedidos.status, não de sz_orders.)
		or.CanReagendar = or.Status == "aguardando"
		or.CanCancelar = or.Status == "aguardando"
		or.CanClone = or.Status == "frustrado"

		out = append(out, or)
	}
	if rows.Err() != nil {
		return nil, true, rows.Err()
	}
	return out, true, nil
}

// ── GET /portal/orders/export.csv ─────────────────────────────────────────────── // FEAT-PORTAL

// ExportCSV exporta a lista de pedidos do usuário como CSV (Content-Type text/csv).
// Reusa fetchOrders → MESMO recorte de propriedade da List (afiliado→affiliate_id,
// produtor→produtor_id; operator/demais → CSV só com cabeçalho). BYPASSA httpx.WriteOK:
// escreve text/csv via encoding/csv.
//
// SEGURANÇA (mesma de orders.Detail): o afiliado NÃO recebe a coluna Afiliado nem
// qualquer campo do produtor — o CSV expõe apenas os mesmos campos da listagem
// (número, status, cliente, produto, valor sem frete, datas). Não há taxa/líquido do
// produtor na lista, então nada do produtor vaza por aqui.
func (h *OrdersHandler) ExportCSV(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	ctx := r.Context()

	if !h.tableExists(ctx, "sz_orders") {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "sz_orders ainda não migrada")
		return
	}

	isAffiliate := u.Role == "afiliado" || u.Role == "affiliate" || u.Role == "afiliada" || u.Role == "cliente"

	// CSV não pagina no front: usa um teto bem mais alto que a listagem (a
	// exportação deve trazer o histórico, não os 300 da tela). // PERF-list-endpoints-hard-limit
	out, _, err := h.fetchOrders(ctx, u, 100000)
	if err != nil {
		slog.Error("[portal_orders] erro ao exportar CSV", "user_id", u.ID, "role", u.Role, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Cabeçalhos CSV (text/csv + download). Sem coluna Afiliado p/ afiliado (espelha is_affiliate).
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="pedidos.csv"`)
	w.WriteHeader(http.StatusOK)

	cw := csv.NewWriter(w)
	defer cw.Flush()

	header := []string{"numero", "status", "cliente", "produto", "valor_sem_frete", "data_venda", "data_entrega"}
	if !isAffiliate {
		header = append(header, "afiliado")
	}
	_ = cw.Write(header)

	for _, o := range out {
		// data_entrega no CSV (humano) também em BR. O JSON da List mantém delivery_date
		// ISO (o front faz brDate(p.delivery_date) parseando YYYY-MM-DD); aqui é só string
		// p/ o CSV, então convertemos a versão exibida — sem tocar no campo JSON.
		delivery := ""
		if o.DeliveryDate != nil {
			delivery = csvDateBR(*o.DeliveryDate)
		}
		// P1 LGPD: defesa em profundidade — fetchOrders já apaga o nome p/ afiliado,
		// reforçamos aqui antes de escrever a coluna no CSV (caminho explícito).
		rec := []string{
			o.Number,
			o.Status,
			blankBuyerNameForAffiliate(o.ClienteNome, isAffiliate),
			o.ProductName,
			fmt.Sprintf("%.2f", o.TotalNoShip),
			o.DateBR, // data_venda EXIBIDA em BR (DD/MM/AAAA HH:MM) — CSV é p/ humano, não filtro
			delivery,
		}
		if !isAffiliate {
			rec = append(rec, o.AffiliateName)
		}
		_ = cw.Write(rec)
	}
}

// csvDateBR — converte uma data date-only "YYYY-MM-DD..." p/ exibição BR "DD/MM/YYYY"
// (uso EXCLUSIVO do CSV humano — o delivery_date do JSON da List permanece ISO p/ o
// front parsear). Guarda: vazio → "", formato inesperado → devolve cru (não corrompe).
// As duas fontes do delivery_date (meta _sz_delivery_date e reagendado_para::text) são
// date-only, então basta os 10 primeiros chars. Espelha a convenção fmtDateBR (d/m/Y).
func csvDateBR(s string) string {
	if s == "" {
		return ""
	}
	if t, err := time.Parse("2006-01-02", s[:min(10, len(s))]); err == nil {
		return t.Format("02/01/2006")
	}
	return s
}

// ── GET /portal/orders/{id} ──────────────────────────────────────────────────────

// orderFinanceiro — breakdown financeiro DISCRIMINADO, já FILTRADO por papel no
// backend. Os campos do PRODUTOR são ponteiros com omitempty: para um afiliado eles
// chegam nil e o `encoding/json` os OMITE da resposta (segurança: o afiliado nunca
// recebe taxa de entrega / taxa / líquido do produtor, nem a comissão bruta — nem via
// API). Para produtor todos os campos vêm preenchidos.
//
// Campos SEMPRE presentes (afiliado E produtor veem):
//   - valor_pedido              : sz_orders.total
//   - taxa_transacao_afiliado   : sz_orders.transaction_fee (taxa 4,99% REAL; 0 quando
//     frustrado/cancelado/reembolsado)
//   - comissao_afiliado_liquida : sz_orders.affiliate_amount (JÁ líquida — NÃO recalcular)
//   - frustrado / comissao_pct  : flags de exibição
//
// Campos SÓ-PRODUTOR (ponteiros omitempty — ausentes p/ afiliado):
//   - comissao_afiliado_bruta   : affiliate_amount + transaction_fee (= total × pct)
//   - taxa_entrega              : sz_orders.delivery_fee
//   - liquido_produtor          : valor − BRUTA − taxa_entrega (FINAL). taxa_transacao_produtor
//     é OMITIDA: transaction_fee é a taxa do AFILIADO e já está dentro da BRUTA — exibi-la
//     como linha separada do produtor seria dupla contagem.
type orderFinanceiro struct {
	ValorPedido             float64 `json:"valor_pedido"`
	ComissaoPct             float64 `json:"comissao_pct"`
	TaxaTransacaoAfiliado   float64 `json:"taxa_transacao_afiliado"`
	ComissaoAfiliadoLiquida float64 `json:"comissao_afiliado_liquida"`
	Frustrado               bool    `json:"frustrado"`
	ValorFrustradoAfiliado  float64 `json:"valor_frustrado_afiliado,omitempty"`
	// ── Só produtor (omitidos p/ afiliado) ──
	ComissaoAfiliadoBruta  *float64 `json:"comissao_afiliado_bruta,omitempty"`
	TaxaEntrega            *float64 `json:"taxa_entrega,omitempty"`
	TaxaTransacaoProdutor  *float64 `json:"taxa_transacao_produtor,omitempty"`
	LiquidoProdutor        *float64 `json:"liquido_produtor,omitempty"`
	ValorFrustradoProdutor *float64 `json:"valor_frustrado_produtor,omitempty"`
}

// orderDetail — payload do detalhe do pedido (read-only) + breakdown role-scoped.
type orderDetail struct {
	ID            int64           `json:"id"`
	WCOrderID     *int64          `json:"wc_order_id"`
	Number        string          `json:"number"`
	Status        string          `json:"status"`
	ClienteNome   string          `json:"cliente_nome"`
	ProductName   string          `json:"product_name"`
	AffiliateName string          `json:"affiliate_name"` // vazio p/ afiliado (não vê coluna afiliado)
	CreatedAt     string          `json:"created_at"`
	DeliveryDate  *string         `json:"delivery_date"`
	Financeiro    orderFinanceiro `json:"financeiro"`
}

// financeiroFrustratedStatuses — status estornados (espelha admin order_detail.go).
// Frustrado/cancelado/reembolsado → comissão NÃO sofre a taxa de 4,99% (taxa=0).
var financeiroFrustratedStatuses = map[string]bool{
	"frustrado":   true,
	"cancelled":   true,
	"cancelado":   true,
	"reembolsado": true,
	"refunded":    true,
}

// Detail retorna o detalhe de UM pedido do usuário autenticado com o breakdown
// financeiro JÁ recortado pelo papel da sessão. Reaplica o escopo de propriedade da
// List (afiliado→affiliate_id=WPUserID, produtor→produtor_id=ID); id fora do escopo
// → 404 (sem vazar existência). operator/demais → 404 (sem escopo em sz_orders).
func (h *OrdersHandler) Detail(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	ctx := r.Context()

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}

	if !h.tableExists(ctx, "sz_orders") {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "sz_orders ainda não migrada")
		return
	}

	isAffiliate := u.Role == "afiliado" || u.Role == "affiliate" || u.Role == "afiliada" || u.Role == "cliente"
	isProdutor := u.Role == "produtor"

	// Escopo de PROPRIEDADE — equality estrita (mesmo critério da List).
	var whereScope string
	var scopeArg int64
	switch {
	case isAffiliate:
		whereScope = "o.affiliate_id = $2"
		scopeArg = u.WPUserID
	case isProdutor:
		whereScope = "o.produtor_id = $2"
		scopeArg = u.ID
	default:
		// operator/cliente/etc: sem escopo migrado em sz_orders → 404 (igual à List
		// que devolve lista vazia; aqui o recurso singular não é acessível).
		slog.Info("[portal_orders] detalhe negado p/ role sem escopo",
			"user_id", u.ID, "role", u.Role, "order_id", id)
		httpx.WriteErr(w, http.StatusNotFound, "pedido não encontrado")
		return
	}

	hasMeta := h.tableExists(ctx, "sz_order_meta")
	hasItems := h.tableExists(ctx, "sz_order_items")
	hasAddr := h.tableExists(ctx, "sz_order_addresses")
	hasPortalUsers := h.tableExists(ctx, "senderzz_portal_users")
	hasMotoboy := h.tableExists(ctx, "sz_motoboy_pedidos")

	offerNameSel := "''::text AS offer_name"
	offerValueSel := "0::float AS offer_value"
	if hasMeta {
		offerNameSel = `COALESCE((SELECT meta_value FROM sz_order_meta
		                 WHERE order_id = o.id AND meta_key='_senderzz_offer_name'
		                 LIMIT 1), '') AS offer_name`
		offerValueSel = `COALESCE((SELECT CASE WHEN meta_value ~ '^[0-9]+(\.[0-9]+)?$'
		                                       THEN meta_value::numeric ELSE 0 END
		                  FROM sz_order_meta
		                  WHERE order_id = o.id AND meta_key='_senderzz_offer_value'
		                  LIMIT 1), 0)::float AS offer_value`
	}
	produtoSel := "''::text AS produto_nome"
	if hasItems {
		produtoSel = `COALESCE((SELECT nome FROM sz_order_items
		              WHERE order_id = o.id ORDER BY id ASC LIMIT 1), '') AS produto_nome`
	}
	clienteSel := "''::text AS cliente_nome"
	if hasAddr {
		clienteSel = `COALESCE((SELECT nome FROM sz_order_addresses
		              WHERE order_id = o.id
		              ORDER BY CASE WHEN tipo='billing' THEN 0 ELSE 1 END, id ASC
		              LIMIT 1), '') AS cliente_nome`
	}
	afiliadoSel := "''::text AS afiliado_nome"
	if hasPortalUsers {
		afiliadoSel = `COALESCE((SELECT pu.nome FROM senderzz_portal_users pu
		               WHERE pu.wp_user_id = o.affiliate_id OR pu.id = o.affiliate_id
		               ORDER BY CASE WHEN pu.wp_user_id = o.affiliate_id THEN 0 ELSE 1 END
		               LIMIT 1), '') AS afiliado_nome`
	}
	deliverySel := "NULL::text AS delivery_date"
	switch {
	case hasMeta && hasMotoboy:
		deliverySel = `COALESCE(
		    NULLIF((SELECT meta_value FROM sz_order_meta
		            WHERE order_id = o.id AND meta_key='_sz_delivery_date' LIMIT 1), ''),
		    (SELECT mp.reagendado_para::text FROM sz_motoboy_pedidos mp
		      WHERE mp.wc_order_id = COALESCE(o.wp_order_id, o.id) AND mp.reagendado_para IS NOT NULL
		      ORDER BY mp.id DESC LIMIT 1)
		) AS delivery_date`
	case hasMeta:
		deliverySel = `NULLIF((SELECT meta_value FROM sz_order_meta
		               WHERE order_id = o.id AND meta_key='_sz_delivery_date' LIMIT 1), '')
		               AS delivery_date`
	case hasMotoboy:
		deliverySel = `(SELECT mp.reagendado_para::text FROM sz_motoboy_pedidos mp
		                WHERE mp.wc_order_id = COALESCE(o.wp_order_id, o.id) AND mp.reagendado_para IS NOT NULL
		                ORDER BY mp.id DESC LIMIT 1) AS delivery_date`
	}

	sqlQ := `SELECT o.id, o.wp_order_id,
	                COALESCE(NULLIF(o.order_number,''), o.wp_order_id::text) AS number,
	                COALESCE(o.status,'') AS status,
	                ` + clienteSel + `,
	                ` + produtoSel + `,
	                ` + offerNameSel + `,
	                ` + offerValueSel + `,
	                COALESCE(o.total,0)::float            AS total,
	                COALESCE(o.affiliate_amount,0)::float AS affiliate_amount,
	                COALESCE(o.delivery_fee,0)::float     AS delivery_fee,
	                COALESCE(o.transaction_fee,0)::float  AS transaction_fee,
	                ` + afiliadoSel + `,
	                to_char(o.created_at AT TIME ZONE 'America/Sao_Paulo', 'DD/MM/YYYY HH24:MI') AS created_at,
	                ` + deliverySel + `
	         FROM sz_orders o
	         WHERE o.id = $1 AND ` + whereScope + `
	         LIMIT 1`

	var (
		d           orderDetail
		wcOrderID   sql.NullInt64
		offerName   string
		offerValue  float64
		produtoNome string
		total       float64
		affAmount   float64
		deliveryFee float64
		txFee       float64
		delivery    sql.NullString
	)
	err = h.Pool.QueryRow(ctx, sqlQ, id, scopeArg).Scan(
		&d.ID, &wcOrderID, &d.Number, &d.Status,
		&d.ClienteNome, &produtoNome, &offerName, &offerValue,
		&total, &affAmount, &deliveryFee, &txFee,
		&d.AffiliateName, &d.CreatedAt, &delivery,
	)
	if err != nil {
		// Fora do escopo OU inexistente → 404 (não diferencia: não vaza posse).
		if errors.Is(err, pgx.ErrNoRows) {
			httpx.WriteErr(w, http.StatusNotFound, "pedido não encontrado")
			return
		}
		slog.Error("[portal_orders] erro ao carregar detalhe", "user_id", u.ID, "order_id", id, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	if wcOrderID.Valid {
		v := wcOrderID.Int64
		d.WCOrderID = &v
	}
	if offerName != "" {
		d.ProductName = offerName
	} else {
		d.ProductName = produtoNome
	}
	if delivery.Valid && delivery.String != "" {
		dd := delivery.String
		d.DeliveryDate = &dd
	}
	// Afiliado não vê a coluna afiliado (espelha is_affiliate da List/tela).
	// P1 LGPD: e também não vê o nome do comprador (cliente final) — minimização.
	if isAffiliate {
		d.AffiliateName = ""
		d.ClienteNome = blankBuyerNameForAffiliate(d.ClienteNome, true)
	}

	// ── Breakdown financeiro (MODELO FINANCEIRO REAL — verificado c/ dados) ──────
	// As colunas de sz_orders JÁ vêm com os valores finais — NÃO recalcular a taxa de
	// 4,99% (isso era DUPLA COBRANÇA: aplicava 4,99% sobre um valor que já estava líquido).
	//
	//   sz_orders.affiliate_amount = senderzz_affiliate_transactions.amount = comissão
	//                                LÍQUIDA do afiliado (JÁ net — NÃO é a bruta).
	//   sz_orders.transaction_fee  = a taxa de transação REAL do afiliado (4,99%, take da
	//                                plataforma). É o valor a EXIBIR como taxa do afiliado.
	//   BRUTA                      = affiliate_amount + transaction_fee (= total × pct).
	//                                Ex.: 157,34 + 8,26 = 165,60 = 276 × 60%.
	//
	// Enriquece com o status da transação de comissão (ledger) p/ o discriminador.
	comissaoPct, txStatus := h.loadAffiliateCommissionMeta(ctx, d.ID)

	frustrado := financeiroFrustratedStatuses[d.Status]
	// taxa 4,99% só p/ comissão NORMAL: nem frustrado (status pedido), nem comissão
	// cancelada/revertida no ledger.
	aplicaTaxa := !frustrado && txStatus != "cancelled" && txStatus != "reversed"

	// LÍQUIDA = affiliate_amount (valor armazenado, NUNCA recalculado via ×0.9501).
	liquidaAfiliado := affAmount
	var bruta, taxaAfiliado float64
	if aplicaTaxa {
		// taxa = transaction_fee REAL (não recomputar); BRUTA = líquida + taxa.
		taxaAfiliado = txFee
		bruta = affAmount + txFee
	} else {
		// Frustrado/cancelado/revertido: sem taxa; bruta == líquida (sem desconto).
		taxaAfiliado = 0
		bruta = affAmount
	}

	fin := orderFinanceiro{
		ValorPedido:             total,
		ComissaoPct:             comissaoPct,
		TaxaTransacaoAfiliado:   taxaAfiliado,
		ComissaoAfiliadoLiquida: liquidaAfiliado,
		Frustrado:               frustrado,
	}

	// Penalidade de frustrado (afiliado) — meta. Sempre visível (é a parte do afiliado).
	if frustrado && hasMeta {
		m := h.loadOrderMetaPair(ctx, d.ID,
			"_sz_aff_frustration_penalty", "_sz_prod_frustration_penalty")
		if v, ok := m["_sz_aff_frustration_penalty"]; ok {
			fin.ValorFrustradoAfiliado = v
		}
		// ValorFrustradoProdutor é SÓ-produtor → preenchido só no branch produtor abaixo.
		if isProdutor {
			if v, ok := m["_sz_prod_frustration_penalty"]; ok {
				vp := v
				fin.ValorFrustradoProdutor = &vp
			}
		}
	}

	// ── Recorte por papel (SEGURANÇA: campos do produtor só p/ produtor) ────────
	if isProdutor {
		// LÍQUIDO produtor = total − BRUTA − taxa_entrega. A BRUTA JÁ inclui a taxa de
		// transação do afiliado (transaction_fee) → NÃO subtrair txFee de novo (seria a
		// dupla cobrança). Resultado idêntico ao antigo total−affiliate_amount−txFee−delivery
		// por coincidência algébrica (BRUTA = affiliate_amount + transaction_fee).
		liquidoProdutor := total - bruta - deliveryFee
		if frustrado {
			liquidoProdutor = 0 // frustrado zera o líquido do produtor (espelha admin)
		}
		brutaCopy := bruta
		deliveryCopy := deliveryFee
		liqCopy := liquidoProdutor
		fin.ComissaoAfiliadoBruta = &brutaCopy
		fin.TaxaEntrega = &deliveryCopy
		// TaxaTransacaoProdutor permanece nil (omitido): transaction_fee É a taxa do
		// afiliado e já está dentro da BRUTA; exibi-la como linha separada do produtor
		// causaria dupla contagem (o subtotal não fecharia com o líquido).
		fin.LiquidoProdutor = &liqCopy
	}
	// AFILIADO: ponteiros do produtor permanecem nil → omitidos do JSON. O afiliado
	// recebe APENAS valor_pedido, taxa_transacao_afiliado e comissao_afiliado_liquida.

	d.Financeiro = fin
	httpx.WriteOK(w, map[string]any{
		"data":         d,
		"role":         u.Role,
		"is_affiliate": isAffiliate,
	})
}

// loadAffiliateCommissionMeta — devolve (comissao_pct exibição, status da transação
// de comissão). pct via senderzz_affiliates (display-only); status via
// senderzz_affiliate_transactions/commissions (discriminador da taxa). Tudo opcional:
// ausência → ("", 0) e o breakdown aplica a taxa normal. Espelha o id-space canônico
// (affiliate_id de sz_orders = wp_user_id do afiliado).
func (h *OrdersHandler) loadAffiliateCommissionMeta(ctx context.Context, orderID int64) (float64, string) {
	var pct float64
	var status string

	// Status da comissão no ledger (tx vence; senão commissions com normalização).
	if h.tableExists(ctx, "senderzz_affiliate_transactions") {
		var s sql.NullString
		_ = h.Pool.QueryRow(ctx,
			`SELECT COALESCE(status,'') FROM senderzz_affiliate_transactions
			  WHERE order_id = $1 AND type = 'commission'
			  ORDER BY id ASC LIMIT 1`, orderID).Scan(&s)
		status = s.String
	} else if h.tableExists(ctx, "senderzz_affiliate_commissions") {
		var s sql.NullString
		_ = h.Pool.QueryRow(ctx,
			`SELECT COALESCE(status,'') FROM senderzz_affiliate_commissions
			  WHERE order_id = $1 ORDER BY id ASC LIMIT 1`, orderID).Scan(&s)
		switch s.String { // normaliza legado p/ vocabulário do discriminador
		case "estornada":
			status = "cancelled"
		default:
			status = s.String
		}
	}

	// comissao_pct (exibição) — via vínculo. Pode vir 0 (id-space divergente); ok.
	if h.tableExists(ctx, "senderzz_affiliates") && h.tableExists(ctx, "sz_orders") {
		var p sql.NullFloat64
		_ = h.Pool.QueryRow(ctx,
			`SELECT COALESCE(a.comissao_pct,0)
			   FROM sz_orders o
			   LEFT JOIN senderzz_affiliates a
			     ON a.id = o.affiliate_id OR a.afiliado_id = o.affiliate_id
			  WHERE o.id = $1
			  ORDER BY CASE WHEN a.afiliado_id = o.affiliate_id THEN 0 ELSE 1 END
			  LIMIT 1`, orderID).Scan(&p)
		pct = p.Float64
	}
	return pct, status
}

// loadOrderMetaPair — lê DUAS chaves numéricas de sz_order_meta numa query.
// Retorna map só com as chaves presentes E numéricas (cast-safe).
func (h *OrdersHandler) loadOrderMetaPair(ctx context.Context, orderID int64, k1, k2 string) map[string]float64 {
	out := map[string]float64{}
	rows, err := h.Pool.Query(ctx,
		`SELECT meta_key, meta_value FROM sz_order_meta
		  WHERE order_id = $1 AND meta_key = ANY($2::text[])`,
		orderID, []string{k1, k2})
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err == nil {
			if f, perr := strconv.ParseFloat(v, 64); perr == nil {
				out[k] = f
			}
		}
	}
	return out
}
