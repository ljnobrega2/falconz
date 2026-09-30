// Package handlers — handler de Motoboy / Cash On Delivery do Portal V2 (user-scoped).
//
// Espelha templates/portal/v2/sections/motoboy.php (v455 funcional) + a fonte de
// dados senderzz_get_visible_orders_for_user. A tela lista os pedidos motoboy/COD
// do PRODUTOR (ou do afiliado, escopado) com financeiro, complemento longo em
// destaque laranja, dois valores (dinheiro + cartão com taxa) e as ações de
// Cancelar / Reagendar com as regras de status do template.
//
// Rotas (namespace /wp-json/senderzz/v1):
//
//	GET  /portal/motoboy                  — lista pedidos motoboy/COD do usuário (limit 200)
//	POST /portal/motoboy/{id}/cancelar    — cancela pedido (produtor dono; status agendado|embalado)
//	POST /portal/motoboy/{id}/reagendar   — reagenda entrega (produtor dono; status agendado|embalado|frustrado|cancelado)
//
//	{id} nas mutações = wc_order_id (o `data-order-id` do template é o WC order id;
//	a propriedade do pedido é verificada por sz_orders.produtor_id = u.ID).
//
// ── REGRAS PORTADAS FIELMENTE DO motoboy.php ───────────────────────────────────
//
//	$sz4mb_can_act    = !is_affiliate && empty(parent_user_id)  → SÓ produtor muta.
//	$sz4mb_can_cancel = can_act && status IN (agendado, embalado)
//	$sz4mb_can_resched= can_act && status IN (agendado, embalado, frustrado, cancelado)
//	Complemento > 32 chars → flag de destaque laranja (complement_flag).
//	Dois valores: dinheiro (bruto) + cartão = bruto * (1 + cc_fee_pct/100),
//	              cc_fee_pct = senderzz_options['sz_motoboy_cc_fee_pct'] (default 0).
//
// ── ESCOPO POR USUÁRIO (fail-closed, equality estrita — nunca OR/IN) ────────────
//
// CANONICAL id-space (idêntico a orders.go / admin affiliates.go / order_detail.go):
//   - sz_orders attribution afiliado : o.affiliate_id = u.WPUserID  (NUNCA IN(id,wp_user_id))
//   - sz_orders attribution produtor : o.produtor_id  = u.ID        (portal id)
//   - nome do afiliado (display)     : subquery escalar OR + prioridade wp_user_id>id
//     (determinística, single-value → não cruza atribuição)
//
// ── VISIBILIDADE FINANCEIRA POR ROLE (GOVERNANÇA) ──────────────────────────────
//
// Afiliado NÃO vê taxa de entrega nem o líquido do produtor; vê só sua comissão.
// Produtor vê tudo (bruto, taxa, comissão afiliado, líquido). Frustrado substitui
// a comissão pela taxa de frustração. Valores vêm das COLUNAS armazenadas em
// sz_orders / sz_motoboy_pedidos (não re-derivamos a fórmula — evita drift).
//
// ── NOTA: embalar / definir motoboy / mudar status / trocar motoboy NÃO existem aqui ──
//
// O template motoboy.php remove explicitamente essas ações ("Troca de motoboy e
// mudança de status: somente no painel OL"). CLAUDE.md confina embalar/status/
// troca-motoboy à section motoboys-dia (OL-only). Não adicionar rota de embalar
// neste handler — violaria a regra OL-only. Ver orders.go (mesma decisão).
package handlers

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/portal-service/internal/auth"
	"github.com/senderzz/portal-service/internal/httpx"
)

// MotoboyHandler agrupa as dependências dos handlers de motoboy/COD.
// Construção idêntica a WebhookHandler/IntegrationsHandler/OrdersHandler para
// wiring uniforme no main.go.
type MotoboyHandler struct {
	Pool *pgxpool.Pool
}

// NOTA: o conjunto de status que caracterizam pedido motoboy/COD (motoboy.php:22-25)
// não é mais materializado em um set Go — a identificação de pedido motoboy na List
// é feita via EXISTS (sz_motoboy_pedidos WHERE wc_order_id = o.wp_order_id), que é a
// fonte de verdade migrada (evita falso-positivo por status reaproveitado).

// listMotoboyLimit é o teto da listagem de pedidos motoboy (espelha o LIMIT 200
// histórico). AUDIT PERF-list-endpoints-hard-limit: a List busca limit+1 para
// detectar truncamento (has_more) sem COUNT extra e devolve só o teto.
const listMotoboyLimit = 200

// statusCancelaveis — status em que o produtor pode CANCELAR (motoboy.php:212).
var statusCancelaveis = map[string]bool{"agendado": true, "embalado": true}

// statusReagendaveis — status em que o produtor pode REAGENDAR (motoboy.php:213).
// em_rota incluído (pedido do dono 2026-07-24): motoboy já saiu mas ainda pode
// precisar de nova data (endereço fechado, cliente pediu outro dia, etc.).
var statusReagendaveis = map[string]bool{
	"agendado": true, "embalado": true, "em_rota": true, "frustrado": true, "cancelado": true,
}

// statusUIReagCancel — set para a FLAG DE UI can_cancelar (status ∈ {agendado,
// embalado}). DELIBERADAMENTE separado de statusReagendaveis (guard da MUTAÇÃO de
// reagendar, que inclui frustrado/cancelado): estas flags são só dicas de exibição
// p/ o front e têm regra própria — não devem herdar o guard de escrita.
var statusUIReagCancel = map[string]bool{"agendado": true, "embalado": true}

// statusUIReagendar — set para a FLAG DE UI can_reagendar. Separado de
// statusUIReagCancel (2026-07-24, pedido do dono): em_rota deve permitir
// reagendar mas NÃO deve oferecer cancelar ali (statusCancelaveis não inclui
// em_rota — motoboy já saiu, cancelar nesse ponto é fluxo de frustração, não
// cancelamento simples).
var statusUIReagendar = map[string]bool{"agendado": true, "embalado": true, "em_rota": true}

// mbOrderRow — uma linha da tabela motoboy. Campos espelham os data-* / colunas
// de motoboy.php (PEDIDO, CLIENTE, PRODUTO, STATUS, VALOR, COMISSÃO/LÍQUIDO,
// DATA PEDIDO, ENTREGA) + o modal de detalhes.
type mbOrderRow struct {
	ID                int64  `json:"id"`                  // sz_orders.id
	WCOrderID         *int64 `json:"wc_order_id"`         // sz_orders.wp_order_id — usado nas mutações (data-order-id)
	MBPedidoID        *int64 `json:"mb_pedido_id"`        // sz_motoboy_pedidos.id (data-mb-pedido-id)
	MotoboyID         *int64 `json:"motoboy_id"`          // motoboy atribuído (data-mb-motoboy-id; 0/null = sem motoboy)
	Number            string `json:"number"`              // order_number (fallback wp_order_id)
	Status            string `json:"status"`              // status cru — badge é client-side
	ClienteNome       string `json:"cliente_nome"`        // billing.name
	ClienteTelefone   string `json:"cliente_telefone"`    // billing.telefone (strip +55 no front)
	ClienteCPF        string `json:"cliente_cpf"`         // billing CPF (data-cpf)
	ClienteEmail      string `json:"cliente_email"`       // billing email (data-email)
	Endereco          string `json:"endereco"`            // shipping/billing address (data-addr)
	Complemento       string `json:"complemento"`         // dest_complemento (data-complement)
	ComplementoLongo  bool   `json:"complemento_longo"`   // dest_complemento > 32 chars → destaque laranja
	ProductName       string `json:"product_name"`        // products_label (offer_name → 1º item)
	SenderzzOfferName string `json:"senderzz_offer_name"` // _senderzz_offer_name (kit p/ filtro)
	BaseProductName   string `json:"base_product_name"`   // 1º item WC (filtro Produto base)
	AffiliateName     string `json:"affiliate_name"`      // resolvido de o.affiliate_id
	// Financeiro — visibilidade por role (GOVERNANÇA).
	ValorBruto     float64  `json:"valor_bruto"`                 // sempre visível (VALOR)
	ValorCartao    float64  `json:"valor_cartao"`                // bruto * (1 + cc_fee_pct/100)
	TaxaTotal      *float64 `json:"taxa_total,omitempty"`        // taxa motoboy/Senderzz — só produtor
	ComissaoAfil   *float64 `json:"comissao_afiliado,omitempty"` // comissão afiliado (net) — visível p/ ambos
	ValorLiquido   *float64 `json:"valor_liquido,omitempty"`     // líquido do produtor — só produtor
	TaxaFrustracao *float64 `json:"taxa_frustracao,omitempty"`   // substitui comissão quando frustrado — só produtor
	// Datas.
	DateMachine  string  `json:"date_machine"`  // created_at (Y-m-d H:i:s) — filtro de data
	DeliveryDate *string `json:"delivery_date"` // reagendado_para | _sz_delivery_date (YYYY-MM-DD)
	// Flags de ação (já calculadas server-side; o front não decide sozinho).
	CanCancel  bool `json:"can_cancel"`
	CanResched bool `json:"can_resched"`
	// Flags de UI por STATUS (puras — sem gate de role; o front combina com is_affiliate/can_act).
	// can_reagendar/can_cancelar = status ∈ {agendado, embalado}; can_clone = status ∈ {frustrado, cancelado}.
	CanReagendar bool `json:"can_reagendar"`
	CanCancelar  bool `json:"can_cancelar"`
	CanClone     bool `json:"can_clone"`
}

// tableExists — guarda de migração graceful (idêntico aos demais handlers portal).
func (h *MotoboyHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// columnExists — guarda de coluna (algumas colunas financeiras vieram em schema-fixes-v460).
func (h *MotoboyHandler) columnExists(ctx context.Context, table, column string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.columns
			WHERE table_schema='public' AND table_name=$1 AND column_name=$2
		)`, table, column).Scan(&ok)
	return ok
}

// ccFeePct — lê senderzz_options['sz_motoboy_cc_fee_pct'] (default 0).
// Espelha get_option('sz_motoboy_cc_fee_pct', 0) de motoboy.php:399 e a leitura
// de admin motoboy_config.go (senderzz_options name/value, vírgula → ponto).
func (h *MotoboyHandler) ccFeePct(ctx context.Context) float64 {
	if !h.tableExists(ctx, "senderzz_options") {
		return 0
	}
	var raw string
	if err := h.Pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE name=$1`,
		"sz_motoboy_cc_fee_pct",
	).Scan(&raw); err != nil {
		return 0
	}
	raw = strings.TrimSpace(strings.ReplaceAll(raw, ",", "."))
	if raw == "" {
		return 0
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0
	}
	return f
}

// isAffiliateRole — espelha a detecção de role afiliado de orders.go.
func isAffiliateRole(role string) bool {
	return role == "afiliado" || role == "affiliate" || role == "afiliada" || role == "cliente"
}

// blankClientPII — APAGA (não mascara parcialmente) a PII do CLIENTE FINAL numa
// linha de pedido motoboy. Aplicado SÓ quando o usuário autenticado é AFILIADO.
//
// P1 LGPD — broken access control / Art. 6º III (minimização): o afiliado recebia
// nome + telefone + endereço residencial completo + CPF/email do cliente final.
// Os campos FINANCEIROS já são role-gated (nil + omitempty quando isAffiliate);
// esta função ESPELHA esse padrão para a PII — mas como o invariante aqui é a
// minimização ("afiliado vê só: nº do pedido, produto, comissão, status"), a
// regra é APAGAR de vez (string vazia), não revelar parte (mask parcial ainda
// vazaria PII e violaria a whitelist). Mantém o TIPO string ("" em vez de nil)
// para NÃO mudar o shape do JSON (a chave continua presente) — o front que já
// lê esses campos não quebra; vê só vazio.
//
// Complemento entra: faz parte do endereço residencial (ex.: "apto 42, bloco B").
// Deixá-lo visível enquanto se apaga o logradouro deixaria um buraco óbvio.
// ComplementoLongo (flag booleano de tamanho) perde o sentido → resetado.
//
// NÃO toca em nada financeiro (ValorBruto, ValorCartao, ComissaoAfil): isso é
// governado pelo gate financeiro separado e o afiliado PODE ver sua comissão.
// Produtor/operator nunca chegam aqui com isAffiliate=true → intactos.
func blankClientPII(row *mbOrderRow) {
	// Telefone, CPF e email são PII sensível — apagados p/ afiliado (LGPD Art.6 III).
	// Nome e endereço de entrega ficam visíveis: afiliado precisa rastrear entrega.
	row.ClienteTelefone = ""
	row.ClienteCPF = ""
	row.ClienteEmail = ""
}

// isProdutorRole — detecção canônica do role PRODUTOR (dono do programa de
// afiliados). Espelha o gate u.Role == "produtor" já usado em products.go /
// links_portal.go / orders.go. Aceita o sinônimo "producer" por robustez, mas
// o role canônico emitido por Portal_Auth é "produtor".
//
// SEC-RBAC-AFF-GOVERNANCE (auditoria): a governança do programa de afiliados
// (comissão padrão, auto-aprovação, aprovar/recusar/comissão de vínculo,
// convites) é EXCLUSIVA do produtor. Antes os handlers só barravam o afiliado
// (isAffiliateRole) — o OPERATOR (OL) passava no gate e podia escrever
// governança de afiliado de produtores. Esta detecção positiva fecha a brecha:
// quem NÃO é produtor (afiliado E operator/operador) é barrado.
func isProdutorRole(role string) bool {
	return role == "produtor" || role == "producer"
}

// ── GET /portal/motoboy ────────────────────────────────────────────────────────

// List retorna os pedidos motoboy/COD do usuário autenticado (limit 200),
// escopados por role. Apenas pedidos com pedido motoboy associado (existe linha
// em sz_motoboy_pedidos por wc_order_id) OU com status do conjunto motoboy.
//
// Envelope:
//
//	{ ok:true, data:[mbOrderRow...], total:N, role:"produtor",
//	  is_affiliate:false, can_act:true, cc_fee_pct:0 }
//
// can_act=false (afiliado/operator) → o front esconde os controles de ação e o
// financeiro restrito (mesma regra de $sz4mb_can_act no template).
func (h *MotoboyHandler) List(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	ctx := r.Context()

	// sz_orders ainda não migrada → 503 (graceful, espelha orders.go).
	if !h.tableExists(ctx, "sz_orders") {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "sz_orders ainda não migrada")
		return
	}

	isAffiliate := isAffiliateRole(u.Role)
	// can_act = só produtor sem parent_user_id muta (motoboy.php:18).
	canAct := u.Role == "produtor"

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
		// Operator (OL) e demais roles: pedidos motoboy do OL vivem em motoboys-dia
		// (escopo por class_ids ainda não migrado). Retorna vazio (fail-closed).
		slog.Info("[portal_motoboy] role sem escopo em sz_orders — lista vazia",
			"user_id", u.ID, "role", u.Role)
		httpx.WriteOK(w, map[string]any{
			"data": []mbOrderRow{}, "total": 0, "has_more": false, "limit": listMotoboyLimit,
			"role": u.Role, "is_affiliate": isAffiliate, "can_act": false,
			"cc_fee_pct": h.ccFeePct(ctx),
		})
		return
	}

	hasMeta := h.tableExists(ctx, "sz_order_meta")
	hasItems := h.tableExists(ctx, "sz_order_items")
	hasAddr := h.tableExists(ctx, "sz_order_addresses")
	hasPortalUsers := h.tableExists(ctx, "senderzz_portal_users")
	hasMotoboy := h.tableExists(ctx, "sz_motoboy_pedidos")
	ccFee := h.ccFeePct(ctx)

	// Sem sz_motoboy_pedidos não há como identificar/enriquecer pedidos motoboy →
	// lista vazia (graceful), evita devolver pedidos não-motoboy do produtor.
	if !hasMotoboy {
		httpx.WriteOK(w, map[string]any{
			"data": []mbOrderRow{}, "total": 0, "has_more": false, "limit": listMotoboyLimit,
			"role": u.Role, "is_affiliate": isAffiliate, "can_act": canAct,
			"cc_fee_pct": ccFee,
		})
		return
	}

	// Colunas financeiras de sz_orders podem não existir em schemas antigos.
	hasAffAmt := h.columnExists(ctx, "sz_orders", "affiliate_amount")
	hasSenderzzFee := h.columnExists(ctx, "sz_orders", "senderzz_fee")
	hasProducerNet := h.columnExists(ctx, "sz_orders", "producer_net")

	// _senderzz_offer_name (kit) + _senderzz_offer_value (substitui total quando > 0).
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

	// Nome do 1º item WC (produto base p/ filtro + fallback de products_label).
	produtoSel := "''::text AS produto_nome"
	if hasItems {
		produtoSel = `COALESCE((SELECT nome FROM sz_order_items
		              WHERE order_id = o.id ORDER BY id ASC LIMIT 1), '') AS produto_nome`
	}

	// Cliente (billing): nome, telefone, email, endereço.
	// Colunas reais de sz_order_addresses: nome,email,telefone,cep,logradouro,
	// numero,complemento,bairro,cidade,uf,pais (ver admin order_detail.go::loadFullAddress).
	// Endereço = logradouro, numero - bairro, cidade/uf (montado no SQL, billing-first).
	clienteNomeSel := "''::text AS cliente_nome"
	clienteTelSel := "''::text AS cliente_tel"
	clienteEmailSel := "''::text AS cliente_email"
	enderecoSel := "''::text AS endereco"
	if hasAddr {
		// Subquery por campo (single-value, billing-first) — sem fan-out.
		addrPick := func(col string) string {
			return `COALESCE((SELECT ` + col + ` FROM sz_order_addresses
			        WHERE order_id = o.id
			        ORDER BY CASE WHEN tipo='billing' THEN 0 ELSE 1 END, id ASC
			        LIMIT 1), '')`
		}
		clienteNomeSel = addrPick("nome") + " AS cliente_nome"
		if h.columnExists(ctx, "sz_order_addresses", "telefone") {
			clienteTelSel = addrPick("telefone") + " AS cliente_tel"
		}
		if h.columnExists(ctx, "sz_order_addresses", "email") {
			clienteEmailSel = addrPick("email") + " AS cliente_email"
		}
		// Endereço completo (data-addr do template): logradouro, número - bairro, cidade/uf.
		// Subquery única billing-first concatenando as colunas reais (sem fan-out).
		enderecoSel = `COALESCE((
		    SELECT TRIM(BOTH ', ' FROM
		      CONCAT_WS(', ',
		        NULLIF(TRIM(CONCAT_WS(', ', NULLIF(a.logradouro,''), NULLIF(a.numero,''))), ''),
		        NULLIF(a.bairro,''),
		        NULLIF(TRIM(CONCAT_WS('/', NULLIF(a.cidade,''), NULLIF(a.uf,''))), '/')
		      ))
		    FROM sz_order_addresses a
		    WHERE a.order_id = o.id
		    ORDER BY CASE WHEN a.tipo='shipping' THEN 0 ELSE 1 END, a.id ASC
		    LIMIT 1), '') AS endereco`
	}

	// CPF do cliente (data-cpf): vive em sz_order_meta no schema migrado, não em
	// sz_order_addresses. Espelha $sz4mb_cpf = billing_cpf do template. Subquery escalar.
	clienteCPFSel := "''::text AS cliente_cpf"
	if hasMeta {
		clienteCPFSel = `COALESCE((SELECT meta_value FROM sz_order_meta
		                 WHERE order_id = o.id
		                   AND meta_key IN ('_billing_cpf','billing_cpf','_sz_billing_cpf')
		                 ORDER BY CASE meta_key
		                            WHEN '_billing_cpf' THEN 0
		                            WHEN 'billing_cpf'  THEN 1
		                            ELSE 2 END
		                 LIMIT 1), '') AS cliente_cpf`
	}

	// Afiliado (display): subquery escalar prioridade wp_user_id > id (determinística).
	afiliadoSel := "''::text AS afiliado_nome"
	if hasPortalUsers {
		afiliadoSel = `COALESCE((SELECT pu.nome FROM senderzz_portal_users pu
		               WHERE pu.wp_user_id = o.affiliate_id OR pu.id = o.affiliate_id
		               ORDER BY CASE WHEN pu.wp_user_id = o.affiliate_id THEN 0 ELSE 1 END
		               LIMIT 1), '') AS afiliado_nome`
	}

	// Dados do pedido motoboy (id, motoboy_id, complemento, taxa, frustração,
	// data de entrega reagendada). Subqueries escalares por wc_order_id = o.wp_order_id.
	mbPedidoSel := `(SELECT mp.id FROM sz_motoboy_pedidos mp
	                 WHERE mp.wc_order_id = COALESCE(o.wp_order_id, o.id) ORDER BY mp.id DESC LIMIT 1) AS mb_pedido_id`
	// Status REAL do pedido motoboy (sz_motoboy_pedidos.status) — fonte das FLAGS DE UI.
	// É o MESMO campo (e MESMO ORDER BY mp.id DESC LIMIT 1) que resolveOwnedMotoboyPedido lê
	// nas mutações; o.status (sz_orders) diverge (reagendar só mexe em mp.status; cancelar
	// faz bridge p/ vocabulário 'cancelled'). Por isso as flags vêm de mp.status, não o.status.
	mbStatusSel := `COALESCE((SELECT mp.status FROM sz_motoboy_pedidos mp
	                WHERE mp.wc_order_id = COALESCE(o.wp_order_id, o.id) ORDER BY mp.id DESC LIMIT 1), '') AS mb_status`
	mbMotoboySel := `(SELECT mp.motoboy_id FROM sz_motoboy_pedidos mp
	                  WHERE mp.wc_order_id = COALESCE(o.wp_order_id, o.id) ORDER BY mp.id DESC LIMIT 1) AS mb_motoboy_id`
	mbComplSel := `COALESCE((SELECT mp.dest_complemento FROM sz_motoboy_pedidos mp
	               WHERE mp.wc_order_id = COALESCE(o.wp_order_id, o.id) ORDER BY mp.id DESC LIMIT 1), '') AS mb_complemento`
	mbTaxaSel := `COALESCE((SELECT mp.valor_taxa FROM sz_motoboy_pedidos mp
	              WHERE mp.wc_order_id = COALESCE(o.wp_order_id, o.id) ORDER BY mp.id DESC LIMIT 1), 0)::float AS mb_taxa`
	// Penalidade: valor_taxa_frustrado (sz_motoboy_pedidos) como fonte primária;
	// fallback em senderzz_affiliate_transactions type='penalty' quando zerado.
	mbTaxaFrustSel := "0::float AS mb_taxa_frustrado"
	hasAffTx := h.tableExists(ctx, "senderzz_affiliate_transactions")
	if h.columnExists(ctx, "sz_motoboy_pedidos", "valor_taxa_frustrado") {
		if hasAffTx {
			mbTaxaFrustSel = `COALESCE(
			    NULLIF((SELECT mp.valor_taxa_frustrado FROM sz_motoboy_pedidos mp
			            WHERE mp.wc_order_id = COALESCE(o.wp_order_id, o.id) ORDER BY mp.id DESC LIMIT 1), 0),
			    (SELECT at.amount FROM senderzz_affiliate_transactions at
			     WHERE at.order_id = COALESCE(o.wp_order_id, o.id) AND at.type='penalty' LIMIT 1),
			    0)::float AS mb_taxa_frustrado`
		} else {
			mbTaxaFrustSel = `COALESCE((SELECT mp.valor_taxa_frustrado FROM sz_motoboy_pedidos mp
			                  WHERE mp.wc_order_id = COALESCE(o.wp_order_id, o.id) ORDER BY mp.id DESC LIMIT 1), 0)::float AS mb_taxa_frustrado`
		}
	} else if hasAffTx {
		mbTaxaFrustSel = `COALESCE((SELECT at.amount FROM senderzz_affiliate_transactions at
		                  WHERE at.order_id = COALESCE(o.wp_order_id, o.id) AND at.type='penalty' LIMIT 1), 0)::float AS mb_taxa_frustrado`
	}

	// Data de entrega: reagendado_para (motoboy) → fallback _sz_delivery_date (meta).
	deliverySel := `(SELECT mp.reagendado_para::text FROM sz_motoboy_pedidos mp
	                 WHERE mp.wc_order_id = COALESCE(o.wp_order_id, o.id) AND mp.reagendado_para IS NOT NULL
	                 ORDER BY mp.id DESC LIMIT 1) AS delivery_date`
	if hasMeta {
		deliverySel = `COALESCE(
		    (SELECT mp.reagendado_para::text FROM sz_motoboy_pedidos mp
		      WHERE mp.wc_order_id = COALESCE(o.wp_order_id, o.id) AND mp.reagendado_para IS NOT NULL
		      ORDER BY mp.id DESC LIMIT 1),
		    NULLIF((SELECT meta_value FROM sz_order_meta
		            WHERE order_id = o.id AND meta_key='_sz_delivery_date' LIMIT 1), '')
		) AS delivery_date`
	}

	// Colunas financeiras de sz_orders (com guarda de existência).
	affAmtSel := "0::float AS aff_amount"
	if hasAffAmt {
		affAmtSel = "COALESCE(o.affiliate_amount,0)::float AS aff_amount"
	}
	senderzzFeeSel := "0::float AS senderzz_fee"
	if hasSenderzzFee {
		senderzzFeeSel = "COALESCE(o.senderzz_fee,0)::float AS senderzz_fee"
	}
	producerNetSel := "NULL::float AS producer_net"
	if hasProducerNet {
		producerNetSel = "COALESCE(o.producer_net,0)::float AS producer_net"
	}

	sqlQ := `SELECT o.id, o.wp_order_id,
	                COALESCE(NULLIF(o.order_number,''), o.wp_order_id::text) AS number,
	                COALESCE(o.status,'') AS status,
	                ` + clienteNomeSel + `,
	                ` + clienteTelSel + `,
	                ` + clienteCPFSel + `,
	                ` + clienteEmailSel + `,
	                ` + enderecoSel + `,
	                ` + produtoSel + `,
	                ` + offerNameSel + `,
	                ` + offerValueSel + `,
	                COALESCE(o.total,0)::float    AS total,
	                COALESCE(o.shipping,0)::float AS shipping,
	                ` + afiliadoSel + `,
	                ` + mbPedidoSel + `,
	                ` + mbStatusSel + `,
	                ` + mbMotoboySel + `,
	                ` + mbComplSel + `,
	                ` + mbTaxaSel + `,
	                ` + mbTaxaFrustSel + `,
	                ` + affAmtSel + `,
	                ` + senderzzFeeSel + `,
	                ` + producerNetSel + `,
	                o.created_at::text AS created_at,
	                ` + deliverySel + `
	         FROM sz_orders o
	         WHERE ` + whereScope + `
	           AND EXISTS (SELECT 1 FROM sz_motoboy_pedidos mp WHERE mp.wc_order_id = COALESCE(o.wp_order_id, o.id))
	         ORDER BY o.id DESC
	         LIMIT $2`

	// N+1: busca 1 a mais que o teto p/ detectar truncamento sem COUNT. // PERF-list-endpoints-hard-limit
	rows, err := h.Pool.Query(ctx, sqlQ, scopeArg, listMotoboyLimit+1)
	if err != nil {
		slog.Error("[portal_motoboy] erro ao listar pedidos", "user_id", u.ID, "role", u.Role, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer rows.Close()

	out := []mbOrderRow{}
	for rows.Next() {
		var (
			row           mbOrderRow
			wcOrderID     sql.NullInt64
			mbPedidoID    sql.NullInt64
			mbStatus      string
			mbMotoboyID   sql.NullInt64
			offerName     string
			offerValue    float64
			produtoNome   string
			total         float64
			shipping      float64
			complemento   string
			taxaMotoboy   float64
			taxaFrustrado float64
			affAmount     float64
			senderzzFee   float64
			producerNet   sql.NullFloat64
			delivery      sql.NullString
		)
		if err := rows.Scan(
			&row.ID, &wcOrderID, &row.Number, &row.Status,
			&row.ClienteNome, &row.ClienteTelefone, &row.ClienteCPF, &row.ClienteEmail,
			&row.Endereco, &produtoNome, &offerName, &offerValue,
			&total, &shipping, &row.AffiliateName,
			&mbPedidoID, &mbStatus, &mbMotoboyID, &complemento, &taxaMotoboy, &taxaFrustrado,
			&affAmount, &senderzzFee, &producerNet,
			&row.DateMachine, &delivery,
		); err != nil {
			slog.Error("[portal_motoboy] erro ao ler linha", "user_id", u.ID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao ler pedidos")
			return
		}

		if wcOrderID.Valid {
			v := wcOrderID.Int64
			row.WCOrderID = &v
		}
		if mbPedidoID.Valid {
			v := mbPedidoID.Int64
			row.MBPedidoID = &v
		}
		if mbMotoboyID.Valid && mbMotoboyID.Int64 > 0 {
			v := mbMotoboyID.Int64
			row.MotoboyID = &v
		}

		// products_label: offer_name vence; senão 1º item.
		row.SenderzzOfferName = offerName
		row.BaseProductName = produtoNome
		if offerName != "" {
			row.ProductName = offerName
		} else {
			row.ProductName = produtoNome
		}

		// Complemento + flag de destaque laranja (> 32 chars).
		row.Complemento = complemento
		row.ComplementoLongo = len([]rune(complemento)) > 32

		// ── P1 LGPD: minimização da PII do cliente p/ AFILIADO ───────────────
		// O afiliado vê só nº do pedido, produto, comissão e status — nunca a PII
		// do cliente final (nome, telefone, CPF, email, endereço, complemento).
		// Espelha o gate financeiro abaixo (que omite taxa/líquido p/ afiliado),
		// mas APAGA em vez de mascarar parcial (Art. 6º III). Produtor não cai aqui.
		if isAffiliate {
			blankClientPII(&row)
		}

		// Valor bruto: offer_value vence; senão max(0, total - shipping). Fiel a format_order.
		bruto := offerValue
		if bruto <= 0 {
			bruto = total - shipping
			if bruto < 0 {
				bruto = 0
			}
		}
		row.ValorBruto = bruto
		// Dois valores: cartão = bruto * (1 + cc_fee_pct/100).
		row.ValorCartao = bruto * (1 + ccFee/100)

		// ── Visibilidade financeira por role (GOVERNANÇA) ────────────────────
		// Comissão do afiliado é visível para ambos (afiliado vê a sua; produtor vê tudo).
		comm := affAmount
		row.ComissaoAfil = &comm

		if !isAffiliate {
			// Produtor vê taxa de entrega, líquido e taxa de frustração.
			// taxa exibida no template = taxa do motoboy (mp.valor_taxa).
			taxa := taxaMotoboy
			row.TaxaTotal = &taxa

			// Líquido: usa producer_net se disponível; senão deriva
			// bruto - taxa - comissão (≥ 0), espelhando $sz4mb_liq_raw.
			var liq float64
			if producerNet.Valid {
				liq = producerNet.Float64
			} else {
				liq = bruto - taxaMotoboy - affAmount
			}
			if liq < 0 {
				liq = 0
			}
			row.ValorLiquido = &liq

			// Frustrado: substitui comissão pela taxa de frustração (só produtor vê taxa/líquido).
			if row.Status == "frustrado" && taxaFrustrado > 0 {
				tf := taxaFrustrado
				row.TaxaFrustracao = &tf
			}
		} else {
			// Afiliado vê taxa de frustração (penalidade cobrada dele).
			if row.Status == "frustrado" && taxaFrustrado > 0 {
				tf := taxaFrustrado
				row.TaxaFrustracao = &tf
			}
		}
		// Afiliado: NÃO recebe TaxaTotal/ValorLiquido (campos omitempty).

		if delivery.Valid && delivery.String != "" {
			d := delivery.String
			row.DeliveryDate = &d
		}

		// Flags de ação — calculadas server-side (só produtor; status do template).
		row.CanCancel = canAct && statusCancelaveis[row.Status]
		row.CanResched = canAct && statusReagendaveis[row.Status]

		// Flags de UI por STATUS (puras — sem gate de role; o front decide com is_affiliate/can_act).
		// Fonte = mbStatus (sz_motoboy_pedidos.status), o MESMO campo que as mutações gateiam —
		// NÃO row.Status (sz_orders.status), que diverge (ver mbStatusSel acima).
		row.CanReagendar = statusUIReagendar[mbStatus]
		row.CanCancelar = statusUIReagCancel[mbStatus]
		row.CanClone = mbStatus == "frustrado" || mbStatus == "cancelado"

		out = append(out, row)
	}
	if rows.Err() != nil {
		slog.Error("[portal_motoboy] erro após iteração", "user_id", u.ID, "err", rows.Err())
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao processar pedidos")
		return
	}

	// has_more=true quando veio a linha extra → o front sabe que a lista foi
	// truncada (antes a truncagem em 200 era silenciosa). Devolve só o teto.
	// // PERF-list-endpoints-hard-limit
	hasMore := len(out) > listMotoboyLimit
	if hasMore {
		out = out[:listMotoboyLimit]
	}

	httpx.WriteOK(w, map[string]any{
		"data":         out,
		"total":        len(out),
		"has_more":     hasMore,
		"limit":        listMotoboyLimit,
		"role":         u.Role,
		"is_affiliate": isAffiliate,
		"can_act":      canAct,
		"cc_fee_pct":   ccFee,
	})
}

// ── Helpers de propriedade / mutação ───────────────────────────────────────────

// resolveOwnedMotoboyPedido — verifica que o pedido (identificado por wc_order_id)
// pertence ao PRODUTOR autenticado e retorna (sz_motoboy_pedidos.id, status atual).
// Fail-closed: 403 se não-produtor; 503 se sz_orders não migrada; 404 se não-dono.
//
// Propriedade = sz_orders.produtor_id = u.ID (CANONICAL produtor attribution).
// Trava de cross-attribution aplicada a ESCRITA — equivalente da regra de leitura.
func (h *MotoboyHandler) resolveOwnedMotoboyPedido(
	ctx context.Context, u *auth.PortalUser, wcOrderID int64,
) (mbPedidoID int64, status string, httpStatus int, errMsg string) {
	// Só produtor muta (motoboy.php: $sz4mb_can_act).
	if u.Role != "produtor" {
		return 0, "", http.StatusForbidden, "apenas o produtor pode alterar pedidos motoboy"
	}
	if !h.tableExists(ctx, "sz_orders") {
		return 0, "", http.StatusServiceUnavailable, "sz_orders ainda não migrada"
	}
	if !h.tableExists(ctx, "sz_motoboy_pedidos") {
		return 0, "", http.StatusServiceUnavailable, "sz_motoboy_pedidos ainda não migrada"
	}

	// Verifica propriedade: o.produtor_id = u.ID (equality estrita, nunca OR/IN).
	var ownerOK bool
	err := h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
		    SELECT 1 FROM sz_orders o
		    WHERE COALESCE(o.wp_order_id, o.id) = $1 AND o.produtor_id = $2
		 )`,
		wcOrderID, u.ID,
	).Scan(&ownerOK)
	if err != nil {
		slog.Error("[portal_motoboy] erro ao verificar propriedade", "user_id", u.ID, "wc_order_id", wcOrderID, "err", err)
		return 0, "", http.StatusInternalServerError, "erro interno"
	}
	if !ownerOK {
		return 0, "", http.StatusNotFound, "pedido não encontrado"
	}

	// Resolve o pedido motoboy correspondente (mais recente).
	var pid int64
	var st sql.NullString
	err = h.Pool.QueryRow(ctx,
		`SELECT id, COALESCE(status,'') FROM sz_motoboy_pedidos
		  WHERE wc_order_id = $1 ORDER BY id DESC LIMIT 1`,
		wcOrderID,
	).Scan(&pid, &st)
	if err == pgx.ErrNoRows {
		return 0, "", http.StatusNotFound, "pedido motoboy não encontrado"
	}
	if err != nil {
		slog.Error("[portal_motoboy] erro ao resolver pedido motoboy", "user_id", u.ID, "wc_order_id", wcOrderID, "err", err)
		return 0, "", http.StatusInternalServerError, "erro interno"
	}
	return pid, st.String, 0, ""
}

// writeMotoboyAudit — registra a ação em sz_motoboy_audit como o PRODUTOR do portal.
// actor_tipo='produtor', actor_id = u.WPUserID (id WP). Best-effort: nunca derruba a
// request. Delega ao helper compartilhado writeMotoboyAuditPortal (motoboy_shared.go)
// p/ não duplicar o INSERT — o afiliado usa o MESMO helper com actor_tipo='afiliado'.
func (h *MotoboyHandler) writeMotoboyAudit(
	ctx context.Context, u *auth.PortalUser, pedidoID int64, acao, deStatus, paraStatus string,
) {
	writeMotoboyAuditPortal(ctx, h.Pool, u, "produtor", pedidoID, acao, deStatus, paraStatus)
}

// parseWCOrderID — extrai e valida o {id} de path (wc_order_id).
func parseWCOrderID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// ── POST /portal/motoboy/{id}/cancelar ─────────────────────────────────────────

// Cancelar — cancela o pedido motoboy do produtor dono.
// {id} = wc_order_id. Só produtor; só status ∈ {agendado, embalado} (motoboy.php:212).
// szV2Confirm no front confirma antes de chamar.
func (h *MotoboyHandler) Cancelar(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	ctx := r.Context()

	wcOrderID, ok := parseWCOrderID(r)
	if !ok {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}

	mbPedidoID, status, httpStatus, errMsg := h.resolveOwnedMotoboyPedido(ctx, u, wcOrderID)
	if httpStatus != 0 {
		httpx.WriteErr(w, httpStatus, errMsg)
		return
	}

	// Guarda de status server-side (o guard do template é client-side).
	if !statusCancelaveis[status] {
		httpx.WriteErr(w, http.StatusUnprocessableEntity,
			"pedido não pode ser cancelado neste status ("+status+")")
		return
	}

	// BRIDGE (mesma tx) — espelha ol.go::MudarStatus + motoboy status_bridge.go.
	// Cancela o pedido motoboy ('cancelado') e ESPELHA em sz_orders ('cancelled') na
	// MESMA tx via helper compartilhado (motoboy_shared.go) — o afiliado usa o MESMO
	// helper. reports_portal.go conta "cancelados" por sz_orders.status IN
	// ('cancelled','cancelado'); o write de status É o estorno.
	szBridged, httpStatus2, errMsg2 := cancelMotoboyWithBridge(ctx, h.Pool, mbPedidoID, wcOrderID)
	if httpStatus2 != 0 {
		slog.Error("[portal_motoboy] erro ao cancelar", "user_id", u.ID, "mb_pedido_id", mbPedidoID, "wc_order_id", wcOrderID, "http", httpStatus2)
		httpx.WriteErr(w, httpStatus2, errMsg2)
		return
	}

	// Auditoria + log após o commit (igual ol.go; writeMotoboyAudit usa h.Pool, não a tx).
	h.writeMotoboyAudit(ctx, u, mbPedidoID, "cancelar", status, "cancelado")
	if szBridged {
		slog.Info("[bridge] sz_orders.status sincronizado",
			"scope", "portal_motoboy.Cancelar", "wc_order_id", wcOrderID,
			"motoboy_status", "cancelado", "sz_orders_status", "cancelled")
	}
	slog.Info("[portal_motoboy] pedido cancelado", "user_id", u.ID, "mb_pedido_id", mbPedidoID)

	httpx.WriteOK(w, map[string]any{
		"mensagem":     "pedido cancelado com sucesso",
		"mb_pedido_id": mbPedidoID,
		"wc_order_id":  wcOrderID,
		"new_status":   "cancelado",
	})
}

// ── POST /portal/motoboy/{id}/reagendar ────────────────────────────────────────

type reagendarMBBody struct {
	Data string `json:"data"` // YYYY-MM-DD (um dos 5 dias úteis do template)
}

// addBusinessDaysMB — soma n dias úteis a partir de base (pula sábado/domingo).
// Opera em UTC midnight para casar com o parse de "2006-01-02".
func addBusinessDaysMB(base time.Time, n int) time.Time {
	d := base
	for added := 0; added < n; {
		d = d.AddDate(0, 0, 1)
		if d.Weekday() != time.Saturday && d.Weekday() != time.Sunday {
			added++
		}
	}
	return d
}

// Reagendar — grava nova data de entrega no pedido motoboy do produtor dono.
// {id} = wc_order_id. Só produtor; só status ∈ {agendado, embalado, frustrado,
// cancelado} (motoboy.php:213). Data dentro de 5 dias úteis → status='agendado';
// além disso → 'pre_agendado' (mesma regra do admin orders.go).
func (h *MotoboyHandler) Reagendar(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	ctx := r.Context()

	wcOrderID, ok := parseWCOrderID(r)
	if !ok {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}

	var body reagendarMBBody
	if err := decodeJSONBody(r, &body); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "json inválido")
		return
	}
	chosen, perr := time.Parse("2006-01-02", strings.TrimSpace(body.Data))
	if perr != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "data inválida — use o formato YYYY-MM-DD")
		return
	}

	mbPedidoID, status, httpStatus, errMsg := h.resolveOwnedMotoboyPedido(ctx, u, wcOrderID)
	if httpStatus != 0 {
		httpx.WriteErr(w, httpStatus, errMsg)
		return
	}

	if !statusReagendaveis[status] {
		httpx.WriteErr(w, http.StatusUnprocessableEntity,
			"pedido não pode ser reagendado neste status ("+status+")")
		return
	}

	// GATE DE DATA compartilhado (past-check + ZONA SCHEDULE + split 5 dias úteis).
	// A validação por zona (DOW de funcionamento + cutoff) que faltava neste caminho
	// agora vem do helper compartilhado — fail-open quando o pedido não tem zona.
	newStatus, httpStatus, gateMsg := reagendarDateGate(ctx, h.Pool, mbPedidoID, chosen)
	if httpStatus != 0 {
		httpx.WriteErr(w, httpStatus, gateMsg)
		return
	}

	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx,
		`UPDATE sz_motoboy_pedidos
		    SET reagendado_para = $1, status = $2, updated_at = NOW()
		  WHERE id = $3`,
		body.Data, newStatus, mbPedidoID,
	)
	if err != nil {
		slog.Error("[portal_motoboy] erro ao reagendar", "user_id", u.ID, "mb_pedido_id", mbPedidoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.WriteErr(w, http.StatusNotFound, "pedido motoboy não encontrado")
		return
	}

	// BRIDGE reverso: reagendar pedido frustrado/cancelado reativa o motoboy mas
	// sz_orders.status ficava preso — reset p/ 'pending'.
	if status == "frustrado" || status == "cancelado" {
		if _, err := tx.Exec(ctx, `
			UPDATE sz_orders o
			   SET status = 'pending', updated_at = NOW()
			  FROM sz_motoboy_pedidos p
			 WHERE p.id = $1
			   AND COALESCE(o.wp_order_id, o.id) = p.wc_order_id
			   AND o.status <> 'pending'`,
			mbPedidoID,
		); err != nil {
			slog.Error("[portal_motoboy] erro ao sincronizar sz_orders", "mb_pedido_id", mbPedidoID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	h.writeMotoboyAudit(ctx, u, mbPedidoID, "reagendar", status, newStatus)
	slog.Info("[portal_motoboy] pedido reagendado", "user_id", u.ID, "mb_pedido_id", mbPedidoID, "para", body.Data)

	httpx.WriteOK(w, map[string]any{
		"mensagem":        "pedido reagendado com sucesso",
		"mb_pedido_id":    mbPedidoID,
		"wc_order_id":     wcOrderID,
		"reagendado_para": body.Data,
		"new_status":      newStatus,
	})
}

// ── POST /portal/motoboy/{id}/reagendar-clone ──────────────────────────────────

// ReagendarClone — CLONE de um pedido motoboy FRUSTRADO para reentrega, escopado ao
// PRODUTOR dono. {id} = wc_order_id (mesma convenção de Reagendar/Cancelar deste
// handler). ESPELHA admin OrdersHandler.ReagendarClone:
//   - só FRUSTRADO (re-check dentro da tx — TOCTOU);
//   - clona sz_motoboy_pedidos com wc_order_id sintético ≥ 900M (advisory lock);
//   - reseta o ciclo, valida ZONA + 5 dias úteis (via reagendarDateGate);
//   - NÃO cria nova venda (sz_orders) → clone ÓRFÃO (ver cloneFrustradoPedido);
//   - audita no ORIGINAL (de=frustrado) e no CLONE, actor_tipo='produtor'.
//
// Ownership: resolveOwnedMotoboyPedido (o.produtor_id = u.ID; 403 não-produtor, 404
// não-dono). NÃO há bridge sz_orders aqui: o clone é deliberadamente órfão (re-entrega
// não re-contabiliza receita — comentado em cloneFrustradoPedido).
func (h *MotoboyHandler) ReagendarClone(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	ctx := r.Context()

	wcOrderID, ok := parseWCOrderID(r)
	if !ok {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}

	var body reagendarMBBody
	if err := decodeJSONBody(r, &body); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "json inválido")
		return
	}
	chosen, perr := time.Parse("2006-01-02", strings.TrimSpace(body.Data))
	if perr != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "data inválida — use o formato YYYY-MM-DD")
		return
	}

	// Ownership do PRODUTOR (o.produtor_id = u.ID) + resolve mbPedidoID/status atual.
	mbPedidoID, status, httpStatus, errMsg := h.resolveOwnedMotoboyPedido(ctx, u, wcOrderID)
	if httpStatus != 0 {
		httpx.WriteErr(w, httpStatus, errMsg)
		return
	}

	// Guard de status server-side: clone SÓ p/ frustrado/cancelado (o re-check em tx confirma de novo).
	if status != "frustrado" && status != "cancelado" {
		httpx.WriteErr(w, http.StatusConflict,
			"apenas pedidos frustrados ou cancelados podem ser reagendados por clone (status atual: "+status+")")
		return
	}

	// GATE DE DATA compartilhado (past-check + ZONA + split 5 dias úteis) sobre o ORIGINAL.
	newStatus, gateStatus, gateMsg := reagendarDateGate(ctx, h.Pool, mbPedidoID, chosen)
	if gateStatus != 0 {
		httpx.WriteErr(w, gateStatus, gateMsg)
		return
	}

	cloneID, newWCOrderID, cloneStatus, cloneMsg := cloneFrustradoPedido(ctx, h.Pool, mbPedidoID, body.Data, newStatus)
	if cloneStatus != 0 {
		httpx.WriteErr(w, cloneStatus, cloneMsg)
		return
	}

	// Audit (best-effort): ORIGINAL (de=status) e CLONE, ambos actor_tipo='produtor'.
	writeMotoboyAuditPortal(ctx, h.Pool, u, "produtor", mbPedidoID, "reagendar_clone", status, status)
	writeMotoboyAuditPortal(ctx, h.Pool, u, "produtor", cloneID, "reagendar_clone", "", newStatus)
	slog.Info("[portal_motoboy] pedido frustrado clonado p/ reentrega",
		"user_id", u.ID, "origem_pedido_id", mbPedidoID, "clone_pedido_id", cloneID,
		"clone_wc_order_id", newWCOrderID, "para", body.Data)

	httpx.WriteOK(w, map[string]any{
		"mensagem":          "pedido frustrado reagendado por clone com sucesso",
		"origem_pedido_id":  mbPedidoID,
		"clone_pedido_id":   cloneID,
		"clone_wc_order_id": newWCOrderID,
		"reagendado_para":   body.Data,
		"new_status":        newStatus,
	})
}

// ── POST /portal/motoboy/bulk ─────────────────────────────────────────────────── // FEAT-PORTAL

// bulkActionBody — ação em lote sobre uma lista de wc_order_ids.
// action="cancel" é a única ação suportada: orders.go é READ-ONLY e mudança de
// status / troca de motoboy é OL-only (CLAUDE.md) — NÃO adicionar essas como bulk.
type bulkActionBody struct {
	Action string  `json:"action"`
	IDs    []int64 `json:"ids"` // wc_order_ids (o data-order-id do template é o WC order id)
}

// bulkResult — resultado por item (sucesso parcial: cada id é avaliado isoladamente).
type bulkResult struct {
	WCOrderID int64  `json:"wc_order_id"`
	OK        bool   `json:"ok"`
	Status    int    `json:"status"`            // HTTP status equivalente do item
	Message   string `json:"message,omitempty"` // erro PT-BR quando ok=false
	NewStatus string `json:"new_status,omitempty"`
}

// BulkAction aplica uma ação em lote (cancel) sobre uma lista de pedidos motoboy.
//
// MESMA AUTORIZAÇÃO DO SINGLE (constraint da task): cada id passa por
// resolveOwnedMotoboyPedido + a MESMA guarda de status do Cancelar single. NÃO usa
// UPDATE ... WHERE id = ANY(...) — isso burlaria a checagem de propriedade por item.
// Sucesso parcial: itens que falham (não-dono, status inválido) não bloqueiam os
// demais; cada um vem com ok/status/message no array results.
//
// NOTA (CLAUDE.md): a rota REST sz-portal/v2/motoboy/bulk-cancel foi REMOVIDA em
// v446 — esta vive no namespace senderzz/v1/portal, NÃO ressuscita aquele path.
func (h *MotoboyHandler) BulkAction(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	ctx := r.Context()

	var body bulkActionBody
	if err := decodeJSONBody(r, &body); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "json inválido")
		return
	}
	if body.Action != "cancel" {
		httpx.WriteErr(w, http.StatusBadRequest,
			"ação inválida — apenas 'cancel' é suportada em lote (mudança de status/motoboy é exclusiva do painel OL)")
		return
	}
	if len(body.IDs) == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "nenhum pedido informado")
		return
	}
	// Teto defensivo p/ não permitir lote arbitrariamente grande numa request.
	if len(body.IDs) > 200 {
		httpx.WriteErr(w, http.StatusBadRequest, "lote excede o limite de 200 pedidos")
		return
	}

	results := make([]bulkResult, 0, len(body.IDs))
	okCount := 0
	for _, wcOrderID := range body.IDs {
		res := h.bulkCancelOne(ctx, u, wcOrderID)
		if res.OK {
			okCount++
		}
		results = append(results, res)
	}

	slog.Info("[portal_motoboy] bulk cancel concluído",
		"user_id", u.ID, "total", len(body.IDs), "ok", okCount, "falhas", len(body.IDs)-okCount)

	httpx.WriteOK(w, map[string]any{
		"action":  body.Action,
		"total":   len(body.IDs),
		"ok":      okCount,
		"falhas":  len(body.IDs) - okCount,
		"results": results,
	})
}

// bulkCancelOne cancela UM pedido com a mesma autorização e o mesmo bridge do
// Cancelar single (resolveOwnedMotoboyPedido → guarda de status → tx motoboy+sz_orders).
// Retorna o resultado do item (nunca panica nem aborta o lote inteiro).
func (h *MotoboyHandler) bulkCancelOne(ctx context.Context, u *auth.PortalUser, wcOrderID int64) bulkResult {
	if wcOrderID <= 0 {
		return bulkResult{WCOrderID: wcOrderID, OK: false, Status: http.StatusBadRequest, Message: "id inválido"}
	}

	// MESMA autorização/propriedade do single.
	mbPedidoID, status, httpStatus, errMsg := h.resolveOwnedMotoboyPedido(ctx, u, wcOrderID)
	if httpStatus != 0 {
		return bulkResult{WCOrderID: wcOrderID, OK: false, Status: httpStatus, Message: errMsg}
	}
	// MESMA guarda de status do single.
	if !statusCancelaveis[status] {
		return bulkResult{WCOrderID: wcOrderID, OK: false, Status: http.StatusUnprocessableEntity,
			Message: "pedido não pode ser cancelado neste status (" + status + ")"}
	}

	// MESMO bridge (mesma tx): motoboy 'cancelado' + sz_orders 'cancelled'.
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		slog.Error("[portal_motoboy] bulk: erro ao iniciar tx", "user_id", u.ID, "wc_order_id", wcOrderID, "err", err)
		return bulkResult{WCOrderID: wcOrderID, OK: false, Status: http.StatusInternalServerError, Message: "erro interno"}
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	tag, err := tx.Exec(ctx,
		`UPDATE sz_motoboy_pedidos SET status = 'cancelado', updated_at = NOW() WHERE id = $1`,
		mbPedidoID,
	)
	if err != nil {
		slog.Error("[portal_motoboy] bulk: erro ao cancelar", "user_id", u.ID, "mb_pedido_id", mbPedidoID, "err", err)
		return bulkResult{WCOrderID: wcOrderID, OK: false, Status: http.StatusInternalServerError, Message: "erro interno"}
	}
	if tag.RowsAffected() == 0 {
		return bulkResult{WCOrderID: wcOrderID, OK: false, Status: http.StatusNotFound, Message: "pedido motoboy não encontrado"}
	}

	szTag, err := tx.Exec(ctx,
		`UPDATE sz_orders SET status = 'cancelled', updated_at = NOW()
		  WHERE COALESCE(wp_order_id, id) = $1 AND status <> 'cancelled'`,
		wcOrderID,
	)
	if err != nil {
		slog.Error("[portal_motoboy] bulk: falha no bridge sz_orders", "user_id", u.ID, "wc_order_id", wcOrderID, "err", err)
		return bulkResult{WCOrderID: wcOrderID, OK: false, Status: http.StatusInternalServerError, Message: "erro interno"}
	}

	if err = tx.Commit(ctx); err != nil {
		slog.Error("[portal_motoboy] bulk: falha ao commitar", "user_id", u.ID, "mb_pedido_id", mbPedidoID, "err", err)
		return bulkResult{WCOrderID: wcOrderID, OK: false, Status: http.StatusInternalServerError, Message: "erro interno"}
	}

	h.writeMotoboyAudit(ctx, u, mbPedidoID, "cancelar", status, "cancelado")
	if szTag.RowsAffected() > 0 {
		slog.Info("[bridge] sz_orders.status sincronizado",
			"scope", "portal_motoboy.BulkAction", "wc_order_id", wcOrderID,
			"motoboy_status", "cancelado", "sz_orders_status", "cancelled")
	}

	return bulkResult{WCOrderID: wcOrderID, OK: true, Status: http.StatusOK, NewStatus: "cancelado"}
}
