// Package handlers — handler de Expedição do Portal V2 (user-scoped, READ-ONLY).
//
// Espelha templates/portal/v2/sections/expedicao.php + a fonte de dados
// WC_MelhorEnvio\Portal\Portal_Orders::format_order (src/Portal/Portal_Orders.php).
//
// Rotas (namespace /wp-json/senderzz/v1):
//
//	GET /portal/expedicao — lista pedidos de FRETE do produtor autenticado (limit 500)
//
// ── Por que READ-ONLY (sem approve/cancel/retry em Go) ──────────────────────────
//
// As três ações da expedicao.php (Aprovar / Cancelar / Reprocessar) NÃO são flips
// puros de status: cada uma dispara efeito colateral de carteira TPC via hook WC.
//   - Aprovar  → dispara DÉBITO TPC + compra de etiqueta (hook WC, idempotente).
//   - Cancelar → dispara ESTORNO TPC + webhook + notificação.
//   - Retry    → reprocessa etiqueta; não debita se _senderzz_wallet_debited=yes.
//
// Um UPDATE direto em sz_orders.status='aprovado' a partir do Go PULARIA o hook →
// pedido "aprovado" sem débito e sem etiqueta = bug financeiro (pior que sem rota).
// Viola também "don't touch wallet" / "own only expedicao.go".
//
// Por isso, fielmente à expedicao.php (cujos botões POST em admin-ajax.php, NÃO em
// REST), este handler apenas EMITE a lista + as flags can_approve/can_cancel/can_retry.
// O front renderiza os botões; durante a migração eles continuam batendo no WP AJAX
// (action=senderzz_portal, szaction=approve|cancel|retry). O nonce 'senderzz_portal'
// é mintado pelo WP — o Go não consegue gerá-lo (follow-up de migração; ver nota de
// contrato ao front). Mesma postura do orders.go ("não inventar mutação") e paralela
// ao orders.go que abre o post.php do wp-admin para o detalhe.
//
// ── Escopo por usuário (fail-closed, equality estrita — nunca OR/IN) ─────────────
//
// Espelha o guard da V1 (expedicao.php linhas 60–65):
//   - role "produtor"            → WHERE o.produtor_id = $1  ($1 = portal id, u.ID)
//   - role "afiliado"/"affiliate"→ BLOQUEADO (afiliado nunca vê Expedição) → 403
//   - role "operator" / demais   → lista vazia (escopo por class_ids ainda não
//     migrado em sz_orders; fail-closed p/ não mis-atribuir).
//
// CANONICAL id-space (idêntico a orders.go / admin order_detail.go):
//   - sz_orders attribution produtor : o.produtor_id = u.id (portal id)
//   - sz_order_meta                  : meta.order_id = o.id (portal/sz id)
//   - wc_me_labels                   : label.wc_order_id = COALESCE(o.wp_order_id, o.id)
//   - afiliado (display)             : subquery escalar OR + prioridade wp_user_id>id
//     (determinística, single-value → não cruza atribuição) — reuso verbatim de orders.go
//
// ── Divergências de orders.go que NÃO devem ser copiadas (todas em expedicao.php) ─
//   - Pedidos MOTOBOY são FILTRADOS FORA (expedicao.php linhas 76–83):
//     delivery_mode='motoboy' OU carrier/método contém "motoboy".
//   - Coluna VALOR = shipping_total_raw (expedicao.php:294), NÃO total_no_ship.
//     Como motoboy já está excluído, sem override de taxa → VALOR = COALESCE(o.shipping,0).
//   - Coluna COMISSÃO = _sz_aff_commission por pedido (format_order), cast-safe.
//     NÃO é o agregado do ledger (esse é p/ saldo pendente/disponível, inexistente aqui).
package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/portal-service/internal/auth"
	"github.com/senderzz/portal-service/internal/httpx"
)

// ExpedicaoHandler agrupa as dependências do handler de Expedição.
// Construção idêntica a WebhookHandler/IntegrationsHandler/OrdersHandler para
// wiring uniforme pelo integrador.
type ExpedicaoHandler struct {
	Pool *pgxpool.Pool
}

// expedicaoActions — flags por pedido que decidem quais botões o front renderiza.
// Espelha Portal_Orders::format_order (can_approve/can_cancel/can_retry).
type expedicaoActions struct {
	CanApprove bool `json:"can_approve"`
	CanCancel  bool `json:"can_cancel"`
	CanRetry   bool `json:"can_retry"`
}

// expedicaoRow — uma linha da tabela de Expedição. Campos espelham as colunas de
// expedicao.php (PEDIDO/CLIENTE/PRODUTO/TRANSPORTADORA/RASTREIO/STATUS/VALOR/COMISSÃO/DATA)
// e os data-* dos <tr> (filtros client-side).
type expedicaoRow struct {
	ID                int64            `json:"id"`               // sz_orders.id
	WCOrderID         *int64           `json:"wc_order_id"`      // sz_orders.wp_order_id
	Number            string           `json:"number"`           // order_number (fallback wp_order_id)
	Status            string           `json:"status"`           // status cru (sem "wc-") — badge é client-side
	FinancialStatus   string           `json:"financial_status"` // pós-entrega: pagamento_agendado|vencido|concluido
	ScheduledPayment  string           `json:"scheduled_payment_date"`
	ClienteNome       string           `json:"cliente_nome"`         // billing.name (sz_order_addresses tipo=billing)
	ClienteTelefone   string           `json:"cliente_telefone"`     // billing.telefone (mesmo endereço)
	ClienteCPF        string           `json:"cliente_cpf"`          // _billing_cpf (sz_order_meta)
	CheckoutLinkID    *int64           `json:"checkout_link_id"`     // senderzz_checkout_links.id (via token)
	ProductName       string           `json:"product_name"`         // offer_name → 1º item
	SenderzzOfferName string           `json:"senderzz_offer_name"`  // _senderzz_offer_name (== kit p/ filtro de oferta)
	AffiliateName     string           `json:"affiliate_name"`       // resolvido de o.affiliate_id
	ShippingName      string           `json:"shipping_name"`        // transportadora (carrier)
	TrackingCodes     []string         `json:"tracking_codes"`       // wc_me_labels.tracking_code (0..n)
	TrackingURL       string           `json:"tracking_url"`         // link assinado da página pública de rastreio FALK
	ShippingTotalRaw  float64          `json:"shipping_total_raw"`   // FRETE cobrado (= o.shipping)
	ProducerNet       float64          `json:"producer_net"`         // LÍQUIDO = valor do produto, o.subtotal (pedido do dono 2026-07-23)
	AffiliateComm     float64          `json:"affiliate_commission"` // COMISSÃO (_sz_aff_commission)
	HasLabel          bool             `json:"has_label"`            // existe etiqueta em wc_me_labels
	LabelError        string           `json:"label_error"`          // motivo real da última falha de emissão (AUDIT-2026-07-28)
	DateMachine       string           `json:"date_machine"`         // created_at (Y-m-d H:i:s) — filtro de data
	DeliveryDate      string           `json:"delivery_date"`        // data de entrega (YYYY-MM-DD)
	Actions           expedicaoActions `json:"actions"`              // can_approve/can_cancel/can_retry
	// Endereço de destino (tipo='shipping', fallback billing) — pro drawer de
	// detalhe do front (parity com Cash on Delivery, que sempre mostra endereço).
	DestCEP         string `json:"dest_cep"`
	DestLogradouro  string `json:"dest_logradouro"`
	DestNumero      string `json:"dest_numero"`
	DestComplemento string `json:"dest_complemento"`
	DestBairro      string `json:"dest_bairro"`
	DestCidade      string `json:"dest_cidade"`
	DestUF          string `json:"dest_uf"`
}

// Status sets espelham Portal_Orders.php (constantes da classe):
//
//	const CANCELLABLE_STATUSES = [ 'on-hold' ];
//	const RETRYABLE_STATUSES   = [ 'saldoinsuficiente', 'erro' ];
//	can_approve = status === 'on-hold'
//
// listExpedicaoLimit é o teto da listagem de Expedição (espelha o LIMIT 500
// histórico). AUDIT PERF-list-endpoints-hard-limit: a List busca limit+1 para
// detectar truncamento (has_more) sem COUNT. ATENÇÃO: a lista filtra pedidos
// motoboy DEPOIS do fetch (continue), então o has_more compara o nº de linhas
// LIDAS do banco com o teto — não len(out), que já vem desfalcado pelo filtro.
const listExpedicaoLimit = 500

// Mantidos como sets para checagem O(1) — exact match, nunca substring.
// expedicaoCancellableStatuses — produtor cancela em Pendente (pending/
// aguardando/on-hold — ainda não emitiu etiqueta), Aprovado (processing) ou
// Separado (em_separacao/embalado). AUDIT-2026-07-28 (dono): "pedido pendente
// tb deve deixar cancelar" — amplia a regra original que só cobria
// Aprovado/Separado. Nenhum outro status permite (postado/entregue/etc).
var expedicaoCancellableStatuses = map[string]bool{
	"pending":      true, // pendente (checkout nativo Go)
	"aguardando":   true, // pendente (alias legado)
	"on-hold":      true, // pendente (alias WooCommerce)
	"em_andamento": true, // aprovando/emitindo etiqueta agora (AUDIT-2026-07-28)
	"processing":   true, // grupo "Aprovado" (Expedicao.tsx FILTER_GROUPS.aprovado)
	"em_separacao": true, // grupo "Separado"
	"embalado":     true, // grupo "Separado"
}

// expedicaoApprovableStatuses — mesmo bucket do FILTER_GROUPS.pendente do front
// (Expedicao.tsx:79). Aprovar é green-light do produtor (sem efeito de carteira —
// ver Expedicao.tsx:712-714: emissão de etiqueta/débito é ação de ops, não do
// produtor), então widen seguro.
var expedicaoApprovableStatuses = map[string]bool{
	"pending":    true,
	"aguardando": true,
	"on-hold":    true,
}

var expedicaoRetryableStatuses = map[string]bool{
	"saldoinsuficiente": true,
	"erro":              true,
}

// tableExists — guarda de migração graceful (idêntico a orders.go / handlers admin).
func (h *ExpedicaoHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

func (h *ExpedicaoHandler) columnExists(ctx context.Context, table, column string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			  FROM information_schema.columns
			 WHERE table_schema = 'public'
			   AND table_name = $1
			   AND column_name = $2
		)`, table, column).Scan(&ok)
	return ok
}

// ── GET /portal/expedicao ───────────────────────────────────────────────────────

// List retorna os pedidos de frete (não-motoboy) do produtor autenticado (limit 500),
// já recortados pelo dono. Filtros (chip de status, busca, produto, afiliado, oferta,
// data) são aplicados no client (espelha szV2ExFilter de expedicao.php).
//
// Envelope:
//
//	{ ok:true, data:[expedicaoRow...], total:N, role:"produtor" }
//
// Afiliado recebe 403 (mesmo guard da V1: Expedição não disponível no seu perfil).
func (h *ExpedicaoHandler) List(w http.ResponseWriter, r *http.Request) {
	// Sem cache — dono reportou dado "preso" mesmo após deploy; blinda contra
	// qualquer camada intermediária (CDN/proxy) guardando a resposta JSON.
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	ctx := r.Context()

	// Afiliado nunca vê Expedição — fail-closed (espelha expedicao.php:60–65).
	if u.Role == "afiliado" || u.Role == "affiliate" || u.Role == "afiliada" || u.Role == "cliente" {
		httpx.WriteErr(w, http.StatusForbidden, "Expedição não está disponível no seu perfil.")
		return
	}

	// sz_orders ainda não migrada → 503 graceful (espelha orders.go / admin order_detail).
	if !h.tableExists(ctx, "sz_orders") {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "sz_orders ainda não migrada")
		return
	}

	// Escopo por role — equality estrita, NUNCA OR/IN. Só produtor tem linhas.
	if u.Role != "produtor" {
		// Operator (OL) e demais: escopo por class_ids ainda não migrado em sz_orders.
		// Lista vazia p/ NÃO mis-atribuir pedidos de outro produtor (fail-closed).
		slog.Info("[portal_expedicao] role sem escopo em sz_orders — lista vazia",
			"user_id", u.ID, "role", u.Role)
		httpx.WriteOK(w, map[string]any{
			"data":     []expedicaoRow{},
			"total":    0,
			"has_more": false,
			"limit":    listExpedicaoLimit,
			"role":     u.Role,
		})
		return
	}

	// Tabelas auxiliares disponíveis (degradação graciosa por subquery escalar).
	hasMeta := h.tableExists(ctx, "sz_order_meta")
	hasItems := h.tableExists(ctx, "sz_order_items")
	hasAddr := h.tableExists(ctx, "sz_order_addresses")
	hasPortalUsers := h.tableExists(ctx, "senderzz_portal_users")
	hasFinancialStatus := h.columnExists(ctx, "sz_orders", "financial_status") &&
		h.columnExists(ctx, "sz_orders", "scheduled_payment_date")
	financialStatusSel := "''::text AS financial_status"
	scheduledPaymentSel := "NULL::text AS scheduled_payment_date"
	if hasFinancialStatus {
		_, _ = h.Pool.Exec(ctx, `
			UPDATE sz_orders
			   SET financial_status = 'vencido',
			       financial_status_updated_at = NOW(),
			       updated_at = NOW()
			 WHERE financial_status = 'pagamento_agendado'
			   AND scheduled_payment_date < CURRENT_DATE`)
		financialStatusSel = "COALESCE(o.financial_status,'') AS financial_status"
		scheduledPaymentSel = "COALESCE(o.scheduled_payment_date::text,'') AS scheduled_payment_date"
	}

	// status: AUDIT-2026-07-28 (dono: "cancelei e não estornou ainda, está em
	// Alerta, sempre que ocorrer isso deve ir pra Em cancelamento") — pedido
	// cancelado com estorno de etiqueta ainda PENDENTE mostra 'em_cancelamento'
	// em vez de 'cancelled' puro. Assim que o estorno confirma, volta a
	// mostrar 'cancelled' normal.
	statusSel := "COALESCE(o.status,'') AS status"
	if h.tableExists(ctx, "tpc_transacoes") {
		statusSel = `CASE
		               WHEN lower(regexp_replace(COALESCE(o.status,''), '^wc-', '')) = 'cancelled'
		                AND EXISTS (
		                    SELECT 1 FROM tpc_transacoes t
		                     WHERE t.order_id = COALESCE(o.wp_order_id, o.id)
		                       AND t.tipo = 'credito' AND t.status = 'pendente'
		                       AND t.referencia LIKE 'cancel_label_%'
		                )
		               THEN 'em_cancelamento'
		               ELSE COALESCE(o.status,'')
		             END AS status`
	}
	hasLabels := h.tableExists(ctx, "wc_me_labels")

	// _senderzz_offer_name — nome do kit (== senderzz_offer_name de format_order).
	offerNameSel := "''::text AS offer_name"
	// _senderzz_delivery_mode — usado p/ filtrar pedidos motoboy.
	deliveryModeSel := "''::text AS delivery_mode"
	// _sz_aff_commission — COMISSÃO por pedido (cast-safe contra meta não-numérica).
	commSel := "0::float AS aff_commission"
	if hasMeta {
		offerNameSel = `COALESCE((SELECT meta_value FROM sz_order_meta
		                 WHERE order_id = o.id AND meta_key='_senderzz_offer_name'
		                 LIMIT 1), '') AS offer_name`
		deliveryModeSel = `COALESCE((SELECT meta_value FROM sz_order_meta
		                   WHERE order_id = o.id AND meta_key='_senderzz_delivery_mode'
		                   LIMIT 1), '') AS delivery_mode`
		// Guard regex: meta não-numérica não derruba a listagem (cast-safe).
		commSel = `COALESCE((SELECT CASE WHEN meta_value ~ '^[0-9]+(\.[0-9]+)?$'
		                                 THEN meta_value::numeric ELSE 0 END
		            FROM sz_order_meta
		            WHERE order_id = o.id AND meta_key='_sz_aff_commission'
		            LIMIT 1), 0)::float AS aff_commission`
	}

	// Nome do produto = 1º item de sz_order_items (fallback de products_label).
	produtoSel := "''::text AS produto_nome"
	if hasItems {
		produtoSel = `COALESCE((SELECT nome FROM sz_order_items
		              WHERE order_id = o.id ORDER BY id ASC LIMIT 1), '') AS produto_nome`
	}

	// Cliente = nome do endereço de cobrança (billing) — espelha billing.name.
	clienteSel := "''::text AS cliente_nome"
	clienteTelSel := "''::text AS cliente_telefone"
	if hasAddr {
		clienteSel = `COALESCE((SELECT nome FROM sz_order_addresses
		              WHERE order_id = o.id
		              ORDER BY CASE WHEN tipo='billing' THEN 0 ELSE 1 END, id ASC
		              LIMIT 1), '') AS cliente_nome`
		clienteTelSel = `COALESCE((SELECT telefone FROM sz_order_addresses
		              WHERE order_id = o.id
		              ORDER BY CASE WHEN tipo='billing' THEN 0 ELSE 1 END, id ASC
		              LIMIT 1), '') AS cliente_telefone`
	}

	// CPF do cliente — vive em sz_order_meta (_billing_cpf), não em sz_order_addresses.
	// ID do link de checkout que gerou o pedido — AUDIT-2026-07-29 (dono: "coloca
	// pra aparecer o id do checkout"). Mesma junção fiel de orders.go (token em
	// sz_order_meta._senderzz_offer_token → senderzz_checkout_links.token).
	checkoutLinkIDSel := "NULL::bigint AS checkout_link_id"
	if hasMeta && h.tableExists(ctx, "senderzz_checkout_links") {
		checkoutLinkIDSel = `(SELECT cl.id FROM senderzz_checkout_links cl
		                      WHERE cl.token = (SELECT meta_value FROM sz_order_meta
		                                         WHERE order_id = o.id AND meta_key='_senderzz_offer_token'
		                                         LIMIT 1)
		                      LIMIT 1) AS checkout_link_id`
	}

	clienteCPFSel := "''::text AS cliente_cpf"
	if hasMeta {
		clienteCPFSel = `COALESCE((SELECT meta_value FROM sz_order_meta
		                 WHERE order_id = o.id AND meta_key='_billing_cpf' LIMIT 1), '') AS cliente_cpf`
	}

	// Afiliado (display): subquery escalar com prioridade wp_user_id > id.
	// Determinística e single-value → segura contra cross-attribution. Reuso de orders.go.
	afiliadoSel := "''::text AS afiliado_nome"
	if hasPortalUsers {
		afiliadoSel = `COALESCE((SELECT pu.nome FROM senderzz_portal_users pu
		               WHERE pu.wp_user_id = o.affiliate_id OR pu.id = o.affiliate_id
		               ORDER BY CASE WHEN pu.wp_user_id = o.affiliate_id THEN 0 ELSE 1 END
		               LIMIT 1), '') AS afiliado_nome`
	}

	// Transportadora (carrier): AUDIT-2026-07-28 (dono, print real: pedidos
	// 1657-1661 mostravam "Jadlog" mas a etiqueta REAL saiu Correios/PAC) —
	// _sz_freight_company é só a COTAÇÃO/intenção do checkout; uma vez que a
	// etiqueta existe, wc_me_labels.service_name é o que FOI DE FATO
	// cobrado/enviado — tem que vencer. Meta só serve de fallback ANTES da
	// etiqueta existir. Também corrigido: chave de junção era o.wp_order_id CRU
	// (NULL em todo pedido nativo/API), agora COALESCE(o.wp_order_id, o.id) —
	// mesma convenção usada em qualquer outro JOIN com wc_me_labels no projeto.
	// AUDIT-2026-07-28: coluna mostrava o SERVIÇO (PAC/Express/.Package) em vez
	// da EMPRESA (Correios/Jadlog) — company_name (nova coluna, migration 515) é
	// a empresa de fato; fallback pro service_name em etiquetas antigas sem ela.
	labelSub := "NULL::text"
	if hasLabels {
		labelSub = `(SELECT COALESCE(NULLIF(l.company_name, ''), l.service_name) FROM wc_me_labels l
		            WHERE l.wc_order_id = COALESCE(o.wp_order_id, o.id)
		              AND l.status <> 'canceled'
		            ORDER BY l.id DESC LIMIT 1)`
	}
	carrierSel := "''::text AS carrier"
	if hasMeta {
		carrierSel = `COALESCE(
		              NULLIF(` + labelSub + `, ''),
		              NULLIF((SELECT meta_value FROM sz_order_meta
		                      WHERE order_id = o.id AND meta_key='_sz_freight_company' LIMIT 1), ''),
		              '') AS carrier`
	} else if hasLabels {
		carrierSel = `COALESCE(` + labelSub + `, '') AS carrier`
	}
	// has_label = existe etiqueta para o pedido (chave canônica).
	hasLabelSel := "false AS has_label"
	if hasLabels {
		hasLabelSel = `EXISTS(SELECT 1 FROM wc_me_labels l2
		               WHERE l2.wc_order_id = COALESCE(o.wp_order_id, o.id)
		                 AND l2.status <> 'canceled') AS has_label`
	}
	// label_error — AUDIT-2026-07-28: motivo real da última falha de emissão
	// (sz_order_meta._sz_label_error), persistido enquanto o pedido fica em
	// 'em_andamento'. Vazio quando não há falha pendente.
	labelErrorSel := "''::text AS label_error"
	if hasMeta {
		labelErrorSel = `COALESCE((SELECT meta_value FROM sz_order_meta
		                 WHERE order_id = o.id AND meta_key = '_sz_label_error' LIMIT 1), '') AS label_error`
	}

	// Endereço de destino (tipo='shipping', fallback billing) — parity com Cash on
	// Delivery, que sempre mostra o endereço completo no drawer de detalhe.
	addrCols := []string{"cep", "logradouro", "numero", "complemento", "bairro", "cidade", "uf"}
	destSel := make([]string, len(addrCols))
	for i, c := range addrCols {
		if hasAddr {
			destSel[i] = `COALESCE((SELECT ` + c + ` FROM sz_order_addresses
			              WHERE order_id = o.id
			              ORDER BY CASE WHEN tipo='shipping' THEN 0 ELSE 1 END, id ASC
			              LIMIT 1), '') AS dest_` + c
		} else {
			destSel[i] = "''::text AS dest_" + c
		}
	}

	deliveryDateSel := "o.created_at::date::text AS delivery_date"
	if hasMeta {
		deliveryDateSel = `COALESCE(NULLIF((SELECT meta_value FROM sz_order_meta
		                 WHERE order_id = o.id AND meta_key='_sz_delivery_date' LIMIT 1), ''),
		                 o.created_at::date::text) AS delivery_date`
	}

	// number = wp_order_id se existir, senão o próprio o.id. NUNCA order_number
	// ("SZ-0001634") — mesmo padrão de orderLabel() no Cash on Delivery.
	sqlQ := `SELECT o.id, o.wp_order_id,
	                COALESCE(o.wp_order_id::text, o.id::text) AS number,
	                o.order_number,
	                ` + statusSel + `,
	                ` + financialStatusSel + `,
	                ` + scheduledPaymentSel + `,
	                ` + clienteSel + `,
	                ` + clienteTelSel + `,
	                ` + clienteCPFSel + `,
	                ` + checkoutLinkIDSel + `,
	                ` + produtoSel + `,
	                ` + offerNameSel + `,
	                ` + afiliadoSel + `,
	                ` + carrierSel + `,
	                ` + hasLabelSel + `,
	                ` + labelErrorSel + `,
	                COALESCE(o.shipping,0)::float AS shipping,
	                COALESCE(o.subtotal,0)::float AS producer_net,
	                ` + commSel + `,
	                ` + deliveryModeSel + `,
	                ` + deliveryDateSel + `,
	                o.created_at::text AS created_at,
	                ` + strings.Join(destSel, ",\n\t                ") + `
	         FROM sz_orders o
	         WHERE o.produtor_id = $1
	         ORDER BY delivery_date DESC NULLS LAST, o.created_at DESC, o.id DESC
	         LIMIT $2`

	// N+1: busca 1 a mais que o teto p/ detectar truncamento sem COUNT. // PERF-list-endpoints-hard-limit
	rows, err := h.Pool.Query(ctx, sqlQ, u.ID, listExpedicaoLimit+1)
	if err != nil {
		slog.Error("[portal_expedicao] erro ao listar pedidos", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer rows.Close()

	// fetched conta as linhas LIDAS do banco (antes do filtro motoboy) para detectar
	// truncamento corretamente — len(out) não serve pois o continue descarta linhas.
	// // PERF-list-endpoints-hard-limit
	fetched := 0
	out := []expedicaoRow{}
	for rows.Next() {
		fetched++
		// Linha-sentinela (a limit+1): só sinaliza has_more — não processa nem inclui.
		if fetched > listExpedicaoLimit {
			break
		}
		var (
			er           expedicaoRow
			wcOrderID    sql.NullInt64
			orderNumber  string
			offerName    string
			produtoNome  string
			deliveryMode string
		)
		if err := rows.Scan(
			&er.ID, &wcOrderID, &er.Number, &orderNumber, &er.Status, &er.FinancialStatus, &er.ScheduledPayment,
			&er.ClienteNome, &er.ClienteTelefone, &er.ClienteCPF, &er.CheckoutLinkID, &produtoNome, &offerName, &er.AffiliateName,
			&er.ShippingName, &er.HasLabel, &er.LabelError,
			&er.ShippingTotalRaw, &er.ProducerNet, &er.AffiliateComm, &deliveryMode,
			&er.DeliveryDate, &er.DateMachine,
			&er.DestCEP, &er.DestLogradouro, &er.DestNumero, &er.DestComplemento,
			&er.DestBairro, &er.DestCidade, &er.DestUF,
		); err != nil {
			slog.Error("[portal_expedicao] erro ao ler linha", "user_id", u.ID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao ler pedidos")
			return
		}

		// Normaliza status: remove prefixo "wc-" (sz_orders já guarda sem, mas por segurança).
		statusPlain := strings.TrimPrefix(strings.ToLower(er.Status), "wc-")
		er.Status = statusPlain

		// Número usado no fallback de rastreio público da Falk. Para Loggi e
		// Correios, depois de carregar o código da etiqueta, o link aponta para
		// o Melhor Rastreio.
		numForSig := strings.TrimSpace(orderNumber)
		if numForSig == "" {
			numForSig = strconv.FormatInt(er.ID, 10)
		}

		// Filtra pedidos MOTOBOY — fora da Expedição (espelha expedicao.php:76–83).
		// delivery_mode='motoboy' OU carrier/transportadora contém "motoboy".
		hay := strings.ToLower(er.ShippingName)
		if strings.EqualFold(deliveryMode, "motoboy") || strings.Contains(hay, "motoboy") {
			continue
		}

		if wcOrderID.Valid {
			v := wcOrderID.Int64
			er.WCOrderID = &v
		}

		// products_label: offer_name vence; senão 1º item.
		er.SenderzzOfferName = offerName
		if offerName != "" {
			er.ProductName = offerName
		} else {
			// Sem _senderzz_offer_name: cai no nome do 1º item. Em kits, cada linha de
			// sz_order_items grava "<kit> — <variante>" (ex.: "3 Egipzya Sérum + 1
			// Egipzya Espuma — Sérum") — o kit já lista os componentes, então o sufixo
			// " — <variante>" só duplica visualmente ("Sérum...Sérum"). Corta o sufixo.
			if i := strings.LastIndex(produtoNome, " — "); i > 0 {
				produtoNome = produtoNome[:i]
			}
			er.ProductName = produtoNome
		}

		// Rastreio: códigos de wc_me_labels do pedido (0..n). AUDIT-2026-07-28:
		// usava wp_order_id CRU (NULL em pedido nativo/API) — ficava sempre "—"
		// pra esses. Chave canônica: COALESCE(wp_order_id, id), igual ao resto.
		wcOrderKey := er.ID
		if wcOrderID.Valid {
			wcOrderKey = wcOrderID.Int64
		}
		if hasLabels {
			er.TrackingCodes = h.loadTrackingCodes(ctx, wcOrderKey, statusPlain)
		}
		if er.TrackingCodes == nil {
			er.TrackingCodes = []string{}
		}

		// Loggi/Correios usam a página agregadora do Melhor Rastreio. Demais
		// transportadoras preservam a página pública assinada da Falk.
		er.TrackingURL = melhorRastreioLink(er.ShippingName, er.TrackingCodes)
		if er.TrackingURL == "" {
			if code := signedTrackingCode(numForSig); code != "" {
				er.TrackingURL = checkoutPublicBase() + "/checkout/rastreio/" + url.QueryEscape(code)
			}
		}

		// Flags de ação — espelha format_order, exceto CanApprove: o WP legado só
		// via 'on-hold' porque o gateway COD do WooCommerce cria o pedido nesse
		// status; o checkout nativo Go (checkout.go) cria como 'pending'. Widened
		// pro grupo "Pendente" inteiro (mesmo bucket que FILTER_GROUPS.pendente no
		// front) — senão o botão Aprovar nunca aparece pra nenhum pedido FALK.
		er.Actions = expedicaoActions{
			CanApprove: expedicaoApprovableStatuses[statusPlain],
			CanCancel:  expedicaoCancellableStatuses[statusPlain],
			CanRetry:   expedicaoRetryableStatuses[statusPlain],
		}

		out = append(out, er)
	}
	if rows.Err() != nil {
		slog.Error("[portal_expedicao] erro após iteração", "user_id", u.ID, "err", rows.Err())
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao processar pedidos")
		return
	}

	// has_more pela contagem de linhas LIDAS (não len(out), desfalcado pelo filtro
	// motoboy). // PERF-list-endpoints-hard-limit
	hasMore := fetched > listExpedicaoLimit

	httpx.WriteOK(w, map[string]any{
		"data":     out,
		"total":    len(out),
		"has_more": hasMore,
		"limit":    listExpedicaoLimit,
		"role":     u.Role,
	})
}

// Approve — green-light nativo do produtor (POST /portal/expedicao/{id}/approve).
// Só troca sz_orders.status de um estado do bucket "pendente" pra 'processing'
// (== FILTER_GROUPS.aprovado no front). SEM efeito de carteira/etiqueta — essa
// parte é ação de ops (POST /labels/emit/{order_id}), não do produtor (ver
// Expedicao.tsx:712-714). Substitui a chamada morta a admin-ajax.php (nonce
// 'senderzz_portal' nunca mintado pelo SPA — sempre falhava).
func (h *ExpedicaoHandler) Approve(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	if u.Role != "produtor" {
		httpx.WriteErr(w, http.StatusForbidden, "ação disponível apenas para produtor")
		return
	}
	orderID := chi.URLParam(r, "id")
	if orderID == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "id do pedido ausente")
		return
	}
	ctx := r.Context()

	// Frete do pedido — custo que será debitado da carteira TPC quando ops emitir
	// a etiqueta (POST /labels/emit). Aprovar sem saldo pra cobrir deixaria o
	// pedido "processing" fadado a travar depois; barra aqui, na entrada.
	var produtorID int64
	var frete float64
	err := h.Pool.QueryRow(ctx,
		`SELECT produtor_id, COALESCE(shipping, 0)::float
		   FROM sz_orders
		  WHERE id = $1
		    AND lower(regexp_replace(status, '^wc-', '')) IN ('pending', 'aguardando', 'on-hold')`,
		orderID,
	).Scan(&produtorID, &frete)
	if err == pgx.ErrNoRows {
		httpx.WriteErr(w, http.StatusConflict, "pedido não encontrado ou não está mais pendente")
		return
	}
	if err != nil {
		slog.Error("[portal_expedicao] falha ao carregar pedido p/ aprovação", "order_id", orderID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao aprovar pedido")
		return
	}
	if produtorID != u.ID {
		httpx.WriteErr(w, http.StatusNotFound, "pedido não encontrado")
		return
	}

	var saldo, reservado float64
	saldoErr := h.Pool.QueryRow(ctx,
		`SELECT saldo::float, saldo_reservado::float FROM tpc_carteira WHERE user_id = $1`,
		u.ID,
	).Scan(&saldo, &reservado)
	if saldoErr != nil && saldoErr != pgx.ErrNoRows {
		slog.Error("[portal_expedicao] falha ao consultar carteira p/ aprovação", "user_id", u.ID, "err", saldoErr)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao verificar saldo")
		return
	}
	disponivel := saldo - reservado
	if disponivel < 0 {
		disponivel = 0
	}

	if disponivel < frete {
		httpx.WriteErr(w, http.StatusPaymentRequired,
			fmt.Sprintf("saldo insuficiente para aprovar (disponível R$ %.2f, frete R$ %.2f). Recarregue a carteira.",
				disponivel, frete))
		return
	}

	// AUDIT-2026-07-28 (dono, pedido 1660/Cleni): "precisamos evitar que pedido
	// fique como aprovado sem emissão de etiqueta" — 'em_andamento' primeiro
	// (saldo já verificado, tentando emitir agora); só vira 'processing' de
	// verdade DEPOIS que a etiqueta é criada com sucesso na ME.
	var newStatus string
	err = h.Pool.QueryRow(ctx,
		`UPDATE sz_orders SET status = 'em_andamento', updated_at = NOW()
		  WHERE id = $1 AND produtor_id = $2
		    AND lower(regexp_replace(status, '^wc-', '')) IN ('pending', 'aguardando', 'on-hold')
		 RETURNING status`,
		orderID, u.ID,
	).Scan(&newStatus)
	if err == pgx.ErrNoRows {
		httpx.WriteErr(w, http.StatusConflict, "pedido não encontrado ou não está mais pendente")
		return
	}
	if err != nil {
		slog.Error("[portal_expedicao] falha ao aprovar pedido", "order_id", orderID, "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao aprovar pedido")
		return
	}

	_, _ = h.Pool.Exec(ctx,
		`INSERT INTO sz_order_status_history
		    (order_id, status_de, status_para, motivo, actor_id, actor_tipo, created_at)
		 VALUES ($1, NULL, 'em_andamento', 'aprovado pelo produtor via portal (saldo verificado), emitindo etiqueta', $2, 'produtor', NOW())`,
		orderID, u.ID,
	)

	// AUDIT-2026-07-27 (pedido do dono): restaura o comportamento ORIGINAL da
	// expedicao.php (ver header do arquivo) — Aprovar dispara débito TPC + compra
	// de etiqueta na hora, não só o flip de status. A migração Go tinha deixado
	// isso pra ação manual de ops (POST /labels/emit), e "aprovei o 1633... não
	// descontou da carteira, não gerou etiqueta" — produtor esperava o fluxo
	// completo. Chamada HTTP interna pro labels-service (mesmo endpoint 1-click
	// já usado por ops), autenticada com um JWT recém-emitido pro PRÓPRIO
	// produtor (mesmo claim que o front dele usaria) — reusa a lógica real de
	// emissão (cálculo de frete, reserva+débito de saldo, criação no ME, etiqueta)
	// sem duplicar nada. Falha aqui NÃO desfaz a aprovação (já comprometida) —
	// fica visível pro produtor/ops via label_warning e o pedido segue "processing"
	// pronto pra reprocessar (POST /labels/emit/{id} de novo é idempotente).
	labelID, labelWarning := h.autoEmitLabel(ctx, orderID, u.ID)

	resp := map[string]any{"ok": true, "status": newStatus}
	if labelID > 0 {
		// Etiqueta criada de verdade — SÓ AGORA vira 'processing' (Aprovado).
		if _, err := h.Pool.Exec(ctx,
			`UPDATE sz_orders SET status = 'processing', updated_at = NOW() WHERE id = $1`, orderID,
		); err != nil {
			slog.Error("[portal_expedicao] Approve: falha ao promover para processing após etiqueta", "order_id", orderID, "err", err)
		} else {
			_, _ = h.Pool.Exec(ctx,
				`INSERT INTO sz_order_status_history
				    (order_id, status_de, status_para, motivo, actor_id, actor_tipo, created_at)
				 VALUES ($1, 'em_andamento', 'processing', 'etiqueta emitida com sucesso', $2, 'produtor', NOW())`,
				orderID, u.ID,
			)
			_, _ = h.Pool.Exec(ctx, `DELETE FROM sz_order_meta WHERE order_id = $1 AND meta_key = '_sz_label_error'`, orderID)
			resp["status"] = "processing"
		}
		resp["label_id"] = labelID
	} else if labelWarning != "" {
		// Falhou — fica em 'em_andamento' (NUNCA "Aprovado" fantasma). Motivo real
		// persistido pra aparecer na tela mesmo depois de recarregar.
		_, _ = h.Pool.Exec(ctx,
			`INSERT INTO sz_order_meta (order_id, meta_key, meta_value)
			 VALUES ($1, '_sz_label_error', $2)
			 ON CONFLICT (order_id, meta_key) DO UPDATE SET meta_value = EXCLUDED.meta_value`,
			orderID, labelWarning,
		)
	}
	if labelWarning != "" {
		resp["label_warning"] = labelWarning
	}
	httpx.WriteOK(w, resp)
}

// ── POST /portal/expedicao/{id}/cancel ───────────────────────────────────────

// Cancel cancela um pedido de expedição do produtor autenticado — SÓ permitido
// quando o status está em expedicaoCancellableStatuses (Aprovado/processing ou
// Separado/em_separacao|embalado; pedido do dono). Antes deste handler, o botão
// "Cancelar" do front batia em /wp-admin/admin-ajax.php (WordPress) — endpoint
// que NUNCA funciona no FALK (stack 100% Go, sem WP): o nonce 'senderzz_portal'
// nunca é mintado pelo SPA (gap documentado em runAction/Expedicao.tsx), então
// todo cancelamento falhava silenciosamente com "-1". Este é o substituto nativo.
//
// Fluxo (espelha o header do arquivo — "Cancelar dispara ESTORNO TPC"):
//  1. Valida posse + status cancelável (UPDATE atômico com guard, como Approve).
//  2. Se existir etiqueta ativa em wc_me_labels pro pedido, cancela via
//     DELETE /labels/{id} no labels-service (mesmo endpoint que já cancela na
//     ME E estorna a carteira TPC — DeleteLabel em go/labels — não duplicamos
//     essa lógica financeira aqui). Best-effort: falha aqui não desfaz o
//     cancelamento do pedido (já comprometido) — fica logado pra reconciliação.
func (h *ExpedicaoHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	if u.Role != "produtor" {
		httpx.WriteErr(w, http.StatusForbidden, "ação disponível apenas para produtor")
		return
	}
	orderID := chi.URLParam(r, "id")
	if orderID == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "id do pedido ausente")
		return
	}
	ctx := r.Context()

	// UPDATE atômico com guard de posse + status — mesmo padrão do Approve.
	// A lista de status permitidos vive em expedicaoCancellableStatuses (única
	// fonte da verdade, mesma usada pra computar can_cancel na listagem).
	var newStatus string
	var wpOrderID sql.NullInt64
	err := h.Pool.QueryRow(ctx,
		`UPDATE sz_orders SET status = 'cancelled', updated_at = NOW()
		  WHERE id = $1 AND produtor_id = $2
		    AND lower(regexp_replace(status, '^wc-', '')) IN ('pending', 'aguardando', 'on-hold', 'em_andamento', 'processing', 'em_separacao', 'embalado')
		 RETURNING status, wp_order_id`,
		orderID, u.ID,
	).Scan(&newStatus, &wpOrderID)
	if err == pgx.ErrNoRows {
		httpx.WriteErr(w, http.StatusConflict, "pedido não encontrado, não pertence a você, ou não está mais em um status cancelável")
		return
	}
	if err != nil {
		slog.Error("[portal_expedicao] falha ao cancelar pedido", "order_id", orderID, "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao cancelar pedido")
		return
	}

	_, _ = h.Pool.Exec(ctx,
		`INSERT INTO sz_order_status_history
		    (order_id, status_de, status_para, motivo, actor_id, actor_tipo, created_at)
		 VALUES ($1, NULL, 'cancelled', 'cancelado pelo produtor via portal', $2, 'produtor', NOW())`,
		orderID, u.ID,
	)

	// wc_order_id canônico (COALESCE(wp_order_id, id)) — mesmo fallback usado em
	// go/admin/internal/handlers/order_detail.go pra pedidos nativos Go (wp_order_id
	// NULL). Busca etiqueta ativa (não cancelada/entregue) pra cancelar+estornar.
	wcOrderKey := wpOrderID.Int64
	if wcOrderKey == 0 {
		id64, _ := strconv.ParseInt(orderID, 10, 64)
		wcOrderKey = id64
	}
	var labelID int64
	labelErr := h.Pool.QueryRow(ctx,
		`SELECT id FROM wc_me_labels
		  WHERE wc_order_id = $1 AND status NOT IN ('canceled', 'delivered')
		  ORDER BY id DESC LIMIT 1`,
		wcOrderKey,
	).Scan(&labelID)

	// AUDIT-2026-07-28 (dono): "tire qualquer menção ao melhor envio quando
	// cancela pedido. Se der erro deixa logado visível apenas para admin" —
	// falha ao cancelar na operadora/reconciliar NUNCA vai pro produtor, só
	// pro slog (visível pra admin nos logs do serviço, ver cancelLabel abaixo).
	// Produtor só recebe o status do pedido (sempre cancelado com sucesso
	// aqui) e a mensagem de reembolso quando dá tudo certo.
	refundMsg := ""
	if labelErr == nil && labelID > 0 {
		_, refundMsg = h.cancelLabel(ctx, labelID, u.ID)
	} else if labelErr != nil && labelErr != pgx.ErrNoRows {
		slog.Warn("[portal_expedicao] Cancel: falha ao consultar etiqueta pro estorno", "order_id", orderID, "err", labelErr)
	}

	resp := map[string]any{"ok": true, "status": newStatus}
	if refundMsg != "" {
		resp["estorno_mensagem"] = refundMsg
	}
	httpx.WriteOK(w, resp)
}

// cancelLabel chama DELETE /labels/{id} no labels-service (cancela na ME +
// registra estorno PENDENTE na carteira TPC — lógica já existe em
// go/labels/internal/handlers/labels.go::DeleteLabel, não duplicada aqui) com
// um JWT interno do produtor dono. Best-effort: retorna mensagem de aviso em
// caso de falha (pedido já foi cancelado; não bloqueia o fluxo principal).
//
// AUDIT-2026-07-28 (dono): o saldo NUNCA é creditado nesta chamada — só depois
// que a Melhor Envio confirmar o reembolso (ver ConfirmarEstorno no
// wallet-service). Por isso o segundo retorno (refundMsg) sempre comunica o
// prazo de 2-24h em vez de implicar crédito imediato.
func (h *ExpedicaoHandler) cancelLabel(ctx context.Context, labelID int64, producerID int64) (string, string) {
	base := strings.TrimRight(os.Getenv("LABELS_SERVICE_URL"), "/")
	if base == "" {
		base = "http://labels-service:8084"
	}

	tok, err := auth.EmitJWT(producerID, "", "produtor", "")
	if err != nil {
		slog.Error("[portal_expedicao] cancelLabel: falha ao emitir JWT interno", "label_id", labelID, "err", err)
		return "pedido cancelado, mas a etiqueta não pôde ser cancelada automaticamente (erro interno) — peça pra ops cancelar manualmente", ""
	}

	url := base + "/wp-json/wc-melhor-envio/v1/labels/" + strconv.FormatInt(labelID, 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		slog.Error("[portal_expedicao] cancelLabel: falha ao montar requisição", "label_id", labelID, "err", err)
		return "pedido cancelado, mas a etiqueta não pôde ser cancelada automaticamente (erro interno) — peça pra ops cancelar manualmente", ""
	}
	req.Header.Set("Authorization", "Bearer "+tok)

	client := &http.Client{Timeout: 25 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		slog.Error("[portal_expedicao] cancelLabel: falha de rede", "label_id", labelID, "err", err)
		return "pedido cancelado, mas a etiqueta não pôde ser cancelada automaticamente — peça pra ops cancelar manualmente", ""
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		slog.Error("[portal_expedicao] cancelLabel: labels-service recusou", "label_id", labelID, "status", resp.StatusCode, "body", string(body))
		return "pedido cancelado, mas houve falha ao cancelar a etiqueta/registrar o estorno automaticamente — peça pra ops reconciliar", ""
	}

	var parsed struct {
		EstornoMensagem string `json:"estorno_mensagem"`
	}
	if jsonErr := json.Unmarshal(body, &parsed); jsonErr == nil && parsed.EstornoMensagem != "" {
		return "", parsed.EstornoMensagem
	}
	return "", ""
}

// autoEmitLabel chama POST /labels/emit/{order_id} no labels-service com um JWT
// interno recém-emitido pro produtor dono do pedido. Best-effort: erro aqui vira
// aviso, não reverte a aprovação (dinheiro/status já comprometidos na aprovação
// em si — a etiqueta pode ser reprocessada depois, é idempotente por design).
func (h *ExpedicaoHandler) autoEmitLabel(ctx context.Context, orderID string, producerID int64) (int64, string) {
	base := strings.TrimRight(os.Getenv("LABELS_SERVICE_URL"), "/")
	if base == "" {
		base = "http://labels-service:8084"
	}

	tok, err := auth.EmitJWT(producerID, "", "produtor", "")
	if err != nil {
		slog.Error("[portal_expedicao] autoEmitLabel: falha ao emitir JWT interno", "order_id", orderID, "err", err)
		return 0, "etiqueta não emitida automaticamente (erro interno) — peça pra ops emitir manualmente"
	}

	url := base + "/wp-json/wc-melhor-envio/v1/labels/emit/" + orderID
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(nil))
	if err != nil {
		return 0, "etiqueta não emitida automaticamente (erro interno)"
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 25 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		slog.Error("[portal_expedicao] autoEmitLabel: falha de rede", "order_id", orderID, "err", err)
		return 0, "etiqueta não emitida automaticamente (labels-service indisponível) — peça pra ops emitir manualmente"
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))

	var out struct {
		LabelID int64  `json:"label_id"`
		Erro    string `json:"erro"`
	}
	_ = json.Unmarshal(body, &out)

	if resp.StatusCode != http.StatusOK {
		msg := out.Erro
		if msg == "" {
			msg = "falha ao emitir etiqueta (HTTP " + strconv.Itoa(resp.StatusCode) + ")"
		}
		slog.Error("[portal_expedicao] autoEmitLabel: labels-service recusou", "order_id", orderID, "status", resp.StatusCode, "erro", msg)
		return 0, "etiqueta não emitida automaticamente: " + msg
	}

	return out.LabelID, ""
}

// loadTrackingCodes — códigos de rastreio do pedido a partir de wc_me_labels.
// Join key canônica: l.wc_order_id = wpOrderID (= o.wp_order_id). Ignora vazios.
// Mantido fora da query principal para não causar fan-out de linhas (1 pedido → n etiquetas).
func (h *ExpedicaoHandler) loadTrackingCodes(ctx context.Context, wpOrderID int64, orderStatus string) []string {
	rows, err := h.Pool.Query(ctx,
		`SELECT tracking_code, status FROM wc_me_labels
		  WHERE wc_order_id = $1
		    AND tracking_code IS NOT NULL
		    AND tracking_code != ''
		  ORDER BY id DESC`,
		wpOrderID,
	)
	if err != nil {
		return []string{}
	}
	defer rows.Close()

	activeCodes := []string{}
	canceledCodes := []string{}
	for rows.Next() {
		var code, status string
		if err := rows.Scan(&code, &status); err != nil {
			continue
		}
		code = strings.TrimSpace(code)
		if code == "" {
			continue
		}
		if trackingLabelIsActive(status) {
			activeCodes = append(activeCodes, code)
		} else {
			canceledCodes = append(canceledCodes, code)
		}
	}
	return selectTrackingCodes(activeCodes, canceledCodes, orderStatus)
}

func selectTrackingCodes(activeCodes, canceledCodes []string, orderStatus string) []string {
	if len(activeCodes) > 0 {
		return activeCodes
	}
	status := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(orderStatus)), "wc-")
	if status == "entregue" || status == "completo" {
		return canceledCodes
	}
	return []string{}
}

// trackingLabelIsActive impede que códigos de etiquetas canceladas voltem a
// aparecer por dados históricos ou por uma consulta sem o predicado SQL.
func trackingLabelIsActive(status string) bool {
	return !strings.EqualFold(strings.TrimSpace(status), "canceled")
}
