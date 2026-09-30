// Package handlers — endpoint admin para tela OrderDetail consolidada.
//
// Substitui 3 metaboxes do plugin WordPress:
//   - includes/motoboy/order-metabox.php          (Operação Motoboy COD)
//   - includes/senderzz-affiliates.php:5636       (Resumo afiliado)
//   - src/Admin/Orders/Metabox.php                (Etiqueta ME)
//
// Endpoints (ver main.go para wiring):
//
//	GET  /orders/{id}                       → payload consolidado
//	POST /orders/{id}/force-motoboy-status  → muda status manual (admin)
//	POST /orders/{id}/note                  → anota observação interna
//
// Graceful degradation via tableExists: cada seção retorna {exists:false}
// quando a tabela ou linha de origem não existe — nunca derruba a request.
//
// Convenção sz_orders.id (PK) ↔ sz_motoboy_pedidos.wc_order_id ↔
//
//	sz_orders.wp_order_id (ID original do WooCommerce).
//
// O parâmetro {id} da URL é sz_orders.id; o wp_order_id é resolvido a partir
// dele e usado nos JOINs com tabelas keyed pelo ID do WC.
package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/auth"
	"github.com/senderzz/admin-service/internal/httpx"
)

type OrderDetailHandler struct{ Pool *pgxpool.Pool }

func (h *OrderDetailHandler) tableExists(ctx context.Context, name string) bool {
	return tableExistsCached(ctx, h.Pool, name) // AUDIT-2026-06-18 Onda2 (go-infoschema-cache)
}

func (h *OrderDetailHandler) columnExists(ctx context.Context, table, column string) bool {
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

// getOptionFloat lê uma option float de senderzz_options com fallback.
// Aceita vírgula decimal (parseRate normaliza). Tabela/linha ausente → def.
// Espelha CodTaxasHandler.getOptionFloat — usado pela taxa de transação do produtor.
func (h *OrderDetailHandler) getOptionFloat(ctx context.Context, key string, def float64) float64 {
	if !h.tableExists(ctx, "senderzz_options") {
		return def
	}
	var raw string
	if err := h.Pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE name=$1`, key).Scan(&raw); err != nil {
		return def
	}
	return parseRate(raw)
}

// getOptionString lê uma option string de senderzz_options com fallback.
// Distinta de getOptionFloat: NÃO passa por parseRate (que transformaria "none" em 0).
// Usada pelo tipo da taxa de frustração (sz_frustration_fee_type: none|fixed|percent).
// Tabela/linha ausente → def.
func (h *OrderDetailHandler) getOptionString(ctx context.Context, key, def string) string {
	if !h.tableExists(ctx, "senderzz_options") {
		return def
	}
	var raw string
	if err := h.Pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE name=$1`, key).Scan(&raw); err != nil {
		return def
	}
	if raw == "" {
		return def
	}
	return raw
}

// getOptionBool lê uma option booleana de senderzz_options com fallback.
// Aceita 1/true/yes/on. Ausente/inválida → def.
func (h *OrderDetailHandler) getOptionBool(ctx context.Context, key string, def bool) bool {
	if !h.tableExists(ctx, "senderzz_options") {
		return def
	}
	var raw string
	if err := h.Pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE name=$1`, key).Scan(&raw); err != nil {
		return def
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}

// ── Tipos do payload ──────────────────────────────────────────────────────────

type orderCustomer struct {
	Nome     string `json:"nome"`
	Email    string `json:"email"`
	Telefone string `json:"telefone"`
	CPF      string `json:"cpf"`
	RG       string `json:"rg"`
}

// orderMarketing — bloco UTM / referrer / landing extraído de sz_order_meta.
// Tipicamente populado pelo plugin WP a partir dos cookies/sessão na conversão.
type orderMarketing struct {
	UTMSource   string `json:"utm_source"`
	UTMMedium   string `json:"utm_medium"`
	UTMCampaign string `json:"utm_campaign"`
	UTMTerm     string `json:"utm_term"`
	UTMContent  string `json:"utm_content"`
	Referrer    string `json:"referrer"`
	Landing     string `json:"landing_page"`
}

// orderFiscal — bloco NF-e a partir de sz_order_meta (chave/numero/serie/status/url).
// Não expõe _nfe_xml nem _cfe_id (estes são lidos só pra validar presença, se necessário).
type orderFiscal struct {
	NFeChave  string `json:"nfe_chave"`
	NFeNumero string `json:"nfe_numero"`
	NFeSerie  string `json:"nfe_serie"`
	NFeURL    string `json:"nfe_url"`
	NFeStatus string `json:"nfe_status"`
}

// orderOffer — oferta de checkout que originou o pedido. É o LINK DO PRODUTOR que
// define o PREÇO de venda. Fonte fiel migrada do WP:
//   - nome/valor/token: sz_order_meta._senderzz_offer_name/_value/_token (38/38 presentes)
//   - url/price_label/tipo/produtor: senderzz_checkout_links (JOIN por token, 38/38)
//
// Vazio quando o pedido não tem metas de oferta (degradação graciosa).
type orderOffer struct {
	Exists       bool    `json:"exists"`
	LinkID       *int64  `json:"link_id"`       // senderzz_checkout_links.id
	Nome         string  `json:"nome"`          // _senderzz_offer_name
	Valor        float64 `json:"valor"`         // _senderzz_offer_value (== display_value)
	PriceLabel   string  `json:"price_label"`   // checkout_links.price_label ("R$ 349,00")
	Token        string  `json:"token"`         // _senderzz_offer_token
	URL          string  `json:"url"`           // checkout_links.url (link público)
	Tipo         string  `json:"tipo"`          // checkout_links.tipo (correio/motoboy)
	ProdutorNome string  `json:"produtor_nome"` // senderzz_portal_users.nome (via producer_id=id)
}

// orderTracking — código de rastreio consolidado.
// Regra: sz_order_meta._tracking_code vence (permite override manual no portal),
// só cai pra wc_me_labels.tracking_code se meta vier vazio.
type orderTracking struct {
	Code    string `json:"code"`
	URL     string `json:"url"`
	Carrier string `json:"carrier"`
}

type orderAddress struct {
	Endereco    string `json:"endereco"`
	Numero      string `json:"numero"`
	Complemento string `json:"complemento"`
	Bairro      string `json:"bairro"`
	Cidade      string `json:"cidade"`
	UF          string `json:"uf"`
	CEP         string `json:"cep"`
}

type orderItem struct {
	Produto   string  `json:"produto"`
	Qty       int     `json:"qty"`
	PrecoUnit float64 `json:"preco_unit"`
	Subtotal  float64 `json:"subtotal"`
}

// orderItemFull — dump completo de sz_order_items para a UI consolidada.
type orderItemFull struct {
	ID        int64           `json:"id"`
	ProdutoID int64           `json:"produto_id"`
	Nome      string          `json:"nome"`
	SKU       string          `json:"sku"`
	Qty       int             `json:"quantidade"`
	PrecoUnit float64         `json:"preco_unit"`
	Subtotal  float64         `json:"subtotal"`
	Meta      json.RawMessage `json:"meta"`
}

// orderFullAddress — endereço completo (nome/email/telefone + logradouro etc.).
// Distinto de orderAddress (legado, sem identificação do destinatário).
type orderFullAddress struct {
	Exists      bool   `json:"exists"`
	Nome        string `json:"nome"`
	Email       string `json:"email"`
	Telefone    string `json:"telefone"`
	CEP         string `json:"cep"`
	Logradouro  string `json:"logradouro"`
	Numero      string `json:"numero"`
	Complemento string `json:"complemento"`
	Bairro      string `json:"bairro"`
	Cidade      string `json:"cidade"`
	UF          string `json:"uf"`
	Pais        string `json:"pais"`
}

type orderProducer struct {
	ID    *int64 `json:"id"`
	Nome  string `json:"nome"`
	Email string `json:"email"`
}

type orderFees struct {
	SenderzzFee  float64 `json:"senderzz_fee"`
	Shipping     float64 `json:"shipping"`
	ProducerNet  float64 `json:"producer_net"`
	Gross        float64 `json:"gross"`
	AffiliateAmt float64 `json:"affiliate_amount"`
}

type orderTotals struct {
	Subtotal float64 `json:"subtotal"`
	Shipping float64 `json:"shipping"`
	Discount float64 `json:"discount"`
	Total    float64 `json:"total"`
}

// orderFinanceiro — breakdown financeiro DISCRIMINADO do pedido, role-scoped na UI.
// É o contrato canônico para o front. MODELO FINANCEIRO REAL (verificado com dados —
// pedido 1570: total 276, affiliate_amount 157,34, transaction_fee 8,26):
//
//   - valor_pedido              = sz_orders.total (valor da venda)
//   - taxa_transacao_afiliado   = sz_orders.transaction_fee (a taxa de 4,99% REAL do
//     afiliado, fatia da plataforma — ex.: 8,26)
//   - comissao_afiliado_liquida = sz_orders.affiliate_amount (JÁ líquida; NÃO recalcular,
//     NÃO aplicar ×0.9501 — ex.: 157,34)
//   - comissao_afiliado_bruta   = affiliate_amount + transaction_fee (= valor × comissao_pct;
//     ex.: 157,34 + 8,26 = 165,60 = 276×60%)
//   - taxa_entrega              = sz_orders.delivery_fee
//   - taxa_transacao_produtor   = valor × sz_producer_transaction_fee_pct (senderzz_options,
//     default 4,99%). Fatia da plataforma sobre o produtor; distinta da transaction_fee do
//     afiliado. NÃO reaplicar ×0.9501.
//   - liquido_produtor          = valor − BRUTA − taxa_entrega − taxa_transacao_produtor
//     (ex.: 276 − 157,34 − 8,26 − 23,98 − 13,77 = 72,65; com pct=4,99% → 276×0,0499=13,77)
//
// Frustrado: substitui_por_frustrado=true e o valor_frustrado_* assume o lugar da
// comissão (penalidade); taxa = 0, sem comissão. exibir_para informa o escopo de
// visibilidade (admin vê tudo; afiliado só vê a sua líquida; produtor vê tudo).
type orderFinanceiro struct {
	ValorPedido             float64 `json:"valor_pedido"`
	ComissaoPct             float64 `json:"comissao_pct"`
	ComissaoAfiliadoBruta   float64 `json:"comissao_afiliado_bruta"`
	TaxaTransacaoAfiliado   float64 `json:"taxa_transacao_afiliado"`
	ComissaoAfiliadoLiquida float64 `json:"comissao_afiliado_liquida"`
	TaxaEntrega             float64 `json:"taxa_entrega"`
	TaxaTransacaoProdutor   float64 `json:"taxa_transacao_produtor"`
	LiquidoProdutor         float64 `json:"liquido_produtor"`
	// Frustrado: quando o pedido está estornado, a comissão é substituída pela taxa de frustrado.
	Frustrado              bool    `json:"frustrado"`
	ValorFrustradoAfiliado float64 `json:"valor_frustrado_afiliado"`
	ValorFrustradoProdutor float64 `json:"valor_frustrado_produtor"`
	// Taxa de frustração — DESPESA/custo explícito do frustrado (valor POSITIVO; o front
	// exibe como negativo). REGRA DO DONO (configurável): pedido ANTIGO usa o valor JÁ
	// cobrado, armazenado no meta (_sz_aff_frustration_penalty / _sz_prod_frustration_penalty —
	// NÃO recomputar); pedido NOVO computa da config sz_frustration_fee_type/value. Preenchido
	// SÓ no bloco FRUSTRADO de fillFinanceiroAfiliado (CANCELADO zera tudo e não preenche).
	TaxaFrustracaoAfiliado float64 `json:"taxa_frustracao_afiliado"`
	TaxaFrustracaoProdutor float64 `json:"taxa_frustracao_produtor"`
	// PrejuizoAfiliado / PrejuizoProdutor — PERDA EXPLÍCITA de cada parte no frustrado
	// (REGRA DO DONO 2026-06-22: "stats frustrado não mostra o PREJUÍZO de cada parte").
	// O front exibe estes campos como linha de DESPESA (valor negativo, cor danger).
	// Valores POSITIVOS aqui (= magnitude da perda); sinal/cor é responsabilidade da UI.
	//   - PrejuizoAfiliado = ComissaoAfiliadoBruta + TaxaFrustracaoAfiliado. No frustrado a
	//     comissão deixa de ser receita e vira PENALIDADE/DESPESA (CONTEXTO do dono); a taxa
	//     de frustração é um adicional opcional (config — 0 por default). Como no frustrado
	//     bruta == líquida == aff.CommissionAmount (taxa afiliado zerada), a base é inequívoca.
	//   - PrejuizoProdutor = TaxaEntrega + TaxaFrustracaoProdutor. Sem venda, a taxa de entrega
	//     já gasta é perda; a taxa de frustração do produtor é adicional opcional (config).
	//     NÃO inclui a comissão do afiliado (partes distintas — não somar).
	// Preenchidos SÓ no bloco FRUSTRADO; 0 em pedido normal/cancelado.
	PrejuizoAfiliado float64 `json:"prejuizo_afiliado"`
	PrejuizoProdutor float64 `json:"prejuizo_produtor"`
}

type orderHead struct {
	ID               int64            `json:"id"`
	WCOrderID        *int64           `json:"wc_order_id"`
	OrderNumber      string           `json:"order_number"`
	Status           string           `json:"status"`
	CreatedAt        string           `json:"created_at"`
	Customer         orderCustomer    `json:"customer"`
	Address          orderAddress     `json:"address"`
	Items            []orderItem      `json:"items"`
	ItemsFull        []orderItemFull  `json:"items_full"`
	EnderecoEnvio    orderFullAddress `json:"endereco_envio"`
	EnderecoCobranca orderFullAddress `json:"endereco_cobranca"`
	Produtor         orderProducer    `json:"produtor"`
	Taxas            orderFees        `json:"taxas"`
	Totals           orderTotals      `json:"totals"`
	Financeiro       orderFinanceiro  `json:"financeiro"`
}

type comprovante struct {
	ID        int64  `json:"id"`
	TipoPgto  string `json:"tipo_pgto"`
	FotoURL   string `json:"foto_url"`
	BaixaPor  string `json:"baixa_por"`
	CreatedAt string `json:"created_at"`
}

type motoboyAudit struct {
	ID         int64           `json:"id"`
	ActorTipo  string          `json:"actor_tipo"`
	ActorID    *int64          `json:"actor_id"`
	Acao       string          `json:"acao"`
	DeStatus   *string         `json:"de_status"`
	ParaStatus *string         `json:"para_status"`
	MetaJSON   json.RawMessage `json:"meta_json"`
	CreatedAt  string          `json:"created_at"`
}

type motoboySection struct {
	Exists              bool     `json:"exists"`
	PedidoID            *int64   `json:"pedido_id"` // AUDIT-2026-06-22: id sz_motoboy_pedidos p/ reagendar/cancelar
	MotoboyID           *int64   `json:"motoboy_id"`
	MotoboyNome         string   `json:"motoboy_nome"`
	MotoboyTelefone     string   `json:"motoboy_telefone"`
	MotoboyPlaca        string   `json:"motoboy_placa"`
	CDNome              string   `json:"cd_nome"`
	ZonaNome            string   `json:"zona_nome"`
	Status              string   `json:"status"`
	DestProduto         string   `json:"dest_produto"`
	ValorPedido         float64  `json:"valor_pedido"`
	PgtoDinheiro        float64  `json:"pgto_dinheiro"`
	PgtoPix             float64  `json:"pgto_pix"`
	PgtoCartao          float64  `json:"pgto_cartao"`
	RecebedorNome       string   `json:"recebedor_nome"`
	RecebedorTipo       string   `json:"recebedor_tipo"`
	RecebedorCPF        string   `json:"recebedor_cpf"`
	BaixaPor            string   `json:"baixa_por"`
	BaixaAdminUserID    *int64   `json:"baixa_admin_user_id"`
	BaixaMotoboyID      *int64   `json:"baixa_motoboy_id"`
	BaixaAt             *string  `json:"baixa_at"`
	EntregaLat          *float64 `json:"entrega_lat"`
	EntregaLng          *float64 `json:"entrega_lng"`
	FrustradoMotivo     string   `json:"frustrado_motivo"`
	FrustradoObservacao string   `json:"frustrado_observacao"`
	// RepasseConfirmado / RepasseTS — confirmação do motoboy via PWA (equiv. WP: _sz_mb_confirmacao_repasse_confirmado).
	RepasseConfirmado bool           `json:"repasse_confirmado"`
	RepasseTS         *string        `json:"repasse_ts"`
	Comprovantes      []comprovante  `json:"comprovantes"`
	Audit             []motoboyAudit `json:"audit"`
}

type affiliateSection struct {
	Exists           bool    `json:"exists"`
	AffiliateID      *int64  `json:"affiliate_id"`
	AffiliateNome    string  `json:"affiliate_nome"`
	AffiliateEmail   string  `json:"affiliate_email"`
	CommissionPct    float64 `json:"commission_pct"`
	CommissionAmount float64 `json:"commission_amount"`
	StatusTransacao  string  `json:"status_transacao"`
	ProducerID       *int64  `json:"producer_id"`
	ProducerNome     string  `json:"producer_nome"`
}

type labelReverse struct {
	ItemID  *string `json:"item_id"`
	OrderID *string `json:"order_id"`
}

type labelSection struct {
	Exists bool `json:"exists"`
	// LabelID: wc_me_labels.id — usado pelo endpoint /labels/{id}/cancel.
	LabelID         *int64       `json:"label_id"`
	InvoiceID       *string      `json:"invoice_id"`
	CustomServiceID *string      `json:"custom_service_id"`
	ItemID          *string      `json:"item_id"`
	MEOrderID       *string      `json:"me_order_id"`
	PrintURL        *string      `json:"print_url"`
	PDFLocalURL     *string      `json:"pdf_local_url"`
	Status          string       `json:"status"`
	Error           *string      `json:"error"`
	GeneratedAt     *string      `json:"generated_at"`
	BoughtShipping  *string      `json:"bought_shipping"`
	ServiceName     *string      `json:"service_name"`
	TrackingCode    *string      `json:"tracking_code"`
	Reverse         labelReverse `json:"reverse"`
	// PriceReal — pedido dono 2026-07-28: wc_me_labels.price = o que a ME
	// REALMENTE cobrou (custo real da etiqueta). Junto com FreteCobrado
	// (order.totals.shipping, já exposto) dá a margem de frete real — não
	// confundir com "taxa de entrega" (delivery_fee, fórmula COD/motoboy,
	// alheia ao custo real de Expedição). nil = sem etiqueta ainda.
	PriceReal *float64 `json:"price_real"`
}

type orderDetailPayload struct {
	Order     orderHead        `json:"order"`
	Motoboy   motoboySection   `json:"motoboy"`
	Affiliate affiliateSection `json:"affiliate"`
	Label     labelSection     `json:"label"`
	Marketing orderMarketing   `json:"marketing"`
	Fiscal    orderFiscal      `json:"fiscal"`
	Tracking  orderTracking    `json:"tracking"`
	Offer     orderOffer       `json:"offer"`
}

// ── GET /orders/{id} ──────────────────────────────────────────────────────────

// Get retorna o payload consolidado do pedido (head + motoboy + afiliado + label).
// 404 se sz_orders não tem linha para esse id. Cada seção é independente e graceful.
func (h *OrderDetailHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "order_id inválido")
		return
	}

	if !h.tableExists(ctx, "sz_orders") {
		httpx.Err(w, 503, "tables_missing", "sz_orders ainda não migrada")
		return
	}

	// FEAT-RBAC-ORDERS-2026-06-24 — ESCOPO POR PAPEL no DETALHE (DualAuth).
	// Pré-check de PROPRIEDADE pela identidade AUTENTICADA (auth.ActorFromCtx), ANTES
	// de carregar qualquer PII do pedido. Cobre "não é seu" e "não existe" num só 404
	// (não revela existência de pedido de outro dono). Admin/nil → sem checagem.
	//   - produtor → exige sz_orders.produtor_id = <portalUserID>.
	//   - afiliado → exige sz_orders.affiliate_id = <wpUserID>.
	if actor := auth.ActorFromCtx(ctx); actor != nil && actor.Kind != auth.ActorAdmin &&
		actor.Kind != auth.ActorKind("operator") && actor.Kind != auth.ActorKind("operador") {
		var produtorID, affiliateID *int64
		err := h.Pool.QueryRow(ctx,
			`SELECT produtor_id, affiliate_id FROM sz_orders WHERE id=$1`, id).
			Scan(&produtorID, &affiliateID)
		owns := false
		if err == nil {
			switch actor.Kind {
			case auth.ActorProdutor:
				owns = produtorID != nil && *produtorID == actor.PortalUserID
			case auth.ActorAfiliado:
				owns = affiliateID != nil && *affiliateID == actor.WPUserID
			}
		}
		if !owns {
			// 404 unificado: pedido inexistente OU fora do escopo do chamador.
			httpx.Err(w, 404, "not_found", "pedido não encontrado")
			return
		}
	}

	// 1) Cabeçalho do pedido + wp_order_id (chave para tabelas legadas).
	head, wpOrderID, err := h.loadOrderHead(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			httpx.Err(w, 404, "not_found", "pedido não encontrado")
			return
		}
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// 2) Itens e address. Falha silenciosa caso as tabelas sejam opcionais.
	head.Items = h.loadOrderItems(ctx, id)
	head.ItemsFull = h.loadOrderItemsFull(ctx, id)
	head.Address = h.loadOrderAddress(ctx, id)
	head.Customer = h.loadOrderCustomer(ctx, id)
	head.EnderecoEnvio = h.loadFullAddress(ctx, id, "shipping")
	head.EnderecoCobranca = h.loadFullAddress(ctx, id, "billing")

	// LGPD-PII-AUDIT: abrir o detalhe do pedido expõe PII do cliente
	// (nome/email/telefone/CPF/RG via loadOrderCustomer + endereços). Registra o
	// acesso na trilha de accountability. Best-effort — não bloqueia a request.
	if actor := auth.FromCtx(ctx); actor != nil {
		logPIIAccess(ctx, h.Pool, actor.ID, actor.Email, "customer", id,
			[]string{"nome", "email", "telefone", "cpf", "rg"}, "view", r.RemoteAddr)
	} else {
		logPIIAccess(ctx, h.Pool, 0, "", "customer", id,
			[]string{"nome", "email", "telefone", "cpf", "rg"}, "view", r.RemoteAddr)
	}

	// 3) Seções secundárias — todas opcionais.
	// Para JOINs com tabelas que ainda chaveiam por WC order id (sz_motoboy_pedidos
	// e wc_me_labels), usamos wp_order_id quando disponível; senão, caem para 0
	// e os exists ficam false.
	wcOrderID := int64(0)
	if wpOrderID != nil {
		wcOrderID = *wpOrderID
	}

	// Pedidos nativos Go (checkout COD) têm wp_order_id NULL, mas a linha em
	// sz_motoboy_pedidos referencia wc_order_id == sz_orders.id. Sem este fallback o
	// pedido COD nativo sumia da seção motoboy (wcOrderID=0 → loadMotoboySection vazia).
	// Etiquetas (wc_me_labels) ficam fora deste escopo — usam só wp_order_id.
	motoboyKey := wcOrderID
	if motoboyKey == 0 {
		motoboyKey = id
	}

	mot := h.loadMotoboySection(ctx, motoboyKey)
	aff := h.loadAffiliateSection(ctx, id)
	// BUG-FIX 2026-07-28: wc_me_labels TAMBÉM chaveia por COALESCE(wp_order_id, id)
	// (mesmo surrogate que sz_motoboy_pedidos — confirmado: pedido 1633, nativo,
	// wp_order_id NULL, tem etiqueta real em wc_me_labels.wc_order_id=1633, o
	// PRÓPRIO id). Usar só wcOrderID (raw wp_order_id) fazia TODO pedido nativo
	// mostrar "sem etiqueta" mesmo com etiqueta emitida — o card de Etiqueta ME
	// nunca aparecia (nem os botões de imprimir/separar/declaração) pra nenhum
	// pedido FALK nativo (a maioria, hoje). motoboyKey já tem o fallback certo.
	lab := h.loadLabelSection(ctx, motoboyKey)

	// Finaliza o breakdown financeiro com a parte do AFILIADO (precisa do comissao_pct
	// do vínculo, resolvido em loadAffiliateSection). Centralizado em fees.go.
	h.fillFinanceiroAfiliado(ctx, &head, aff)

	// 4) Blocos sz_order_meta — marketing, fiscal, tracking. Tudo opcional.
	mk := h.loadMarketingSection(ctx, id)
	fs := h.loadFiscalSection(ctx, id)
	// Tracking: meta wins, label-fallback. Recebe TrackingCode da label como fallback.
	var labelTC string
	if lab.TrackingCode != nil {
		labelTC = *lab.TrackingCode
	}
	tr := h.loadTrackingSection(ctx, id, labelTC, head.OrderNumber)
	// Oferta de checkout (preço de venda do produtor) — fonte fiel via metas + checkout_links.
	of := h.loadOfferSection(ctx, id)

	httpx.JSON(w, 200, orderDetailPayload{
		Order:     head,
		Motoboy:   mot,
		Affiliate: aff,
		Label:     lab,
		Marketing: mk,
		Fiscal:    fs,
		Tracking:  tr,
		Offer:     of,
	})
}

// loadOrderMetaMap — lê várias chaves de sz_order_meta em UMA query.
// Retorna map[meta_key]meta_value; chaves ausentes simplesmente não aparecem.
// pgx/v5 aceita []string nativo pra ANY($2::text[]) — sem wrapper necessário.
func (h *OrderDetailHandler) loadOrderMetaMap(ctx context.Context, orderID int64, keys []string) map[string]string {
	out := map[string]string{}
	if !h.tableExists(ctx, "sz_order_meta") || len(keys) == 0 {
		return out
	}
	rows, err := h.Pool.Query(ctx,
		`SELECT meta_key, COALESCE(meta_value,'')
		 FROM sz_order_meta
		 WHERE order_id = $1 AND meta_key = ANY($2::text[])`,
		orderID, keys)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err == nil {
			out[k] = v
		}
	}
	return out
}

// loadMarketingSection — UTM + referrer + landing.
// Lê tanto as chaves diretas (_utm_*) quanto as do WooCommerce Order Attribution
// (_wc_order_attribution_utm_*), que é o formato real migrado neste banco.
func (h *OrderDetailHandler) loadMarketingSection(ctx context.Context, orderID int64) orderMarketing {
	keys := []string{
		"_utm_source", "_utm_medium", "_utm_campaign",
		"_utm_term", "_utm_content", "_referrer", "_landing_page",
		"_wc_order_attribution_utm_source", "_wc_order_attribution_utm_medium",
		"_wc_order_attribution_utm_campaign", "_wc_order_attribution_utm_term",
		"_wc_order_attribution_utm_content", "_wc_order_attribution_referrer",
		"_wc_order_attribution_session_entry",
	}
	m := h.loadOrderMetaMap(ctx, orderID, keys)
	// firstNonEmpty — prioridade chave direta > chave WooCommerce attribution.
	firstNonEmpty := func(a, b string) string {
		if v := m[a]; v != "" {
			return v
		}
		return m[b]
	}
	return orderMarketing{
		UTMSource:   firstNonEmpty("_utm_source", "_wc_order_attribution_utm_source"),
		UTMMedium:   firstNonEmpty("_utm_medium", "_wc_order_attribution_utm_medium"),
		UTMCampaign: firstNonEmpty("_utm_campaign", "_wc_order_attribution_utm_campaign"),
		UTMTerm:     firstNonEmpty("_utm_term", "_wc_order_attribution_utm_term"),
		UTMContent:  firstNonEmpty("_utm_content", "_wc_order_attribution_utm_content"),
		Referrer:    firstNonEmpty("_referrer", "_wc_order_attribution_referrer"),
		Landing:     firstNonEmpty("_landing_page", "_wc_order_attribution_session_entry"),
	}
}

// loadFiscalSection — NF-e (chave, número, série, URL, status). _nfe_xml e _cfe_id
// estão na lista de keys só para detecção futura — não são expostos no payload.
func (h *OrderDetailHandler) loadFiscalSection(ctx context.Context, orderID int64) orderFiscal {
	keys := []string{
		"_nfe_chave", "_nfe_numero", "_nfe_serie",
		"_nfe_url", "_nfe_xml", "_nfe_status", "_cfe_id",
	}
	m := h.loadOrderMetaMap(ctx, orderID, keys)
	return orderFiscal{
		NFeChave:  m["_nfe_chave"],
		NFeNumero: m["_nfe_numero"],
		NFeSerie:  m["_nfe_serie"],
		NFeURL:    m["_nfe_url"],
		NFeStatus: m["_nfe_status"],
	}
}

// loadOfferSection — oferta de checkout (link do produtor que define o preço de venda).
// Nome/valor/token saem direto das metas migradas; url/price_label/tipo/produtor vêm
// de senderzz_checkout_links via JOIN por token (varchar=varchar, sem cast). Tudo opcional.
func (h *OrderDetailHandler) loadOfferSection(ctx context.Context, orderID int64) orderOffer {
	m := h.loadOrderMetaMap(ctx, orderID, []string{
		"_senderzz_offer_name", "_senderzz_offer_value", "_senderzz_offer_token",
	})
	out := orderOffer{
		Nome:  m["_senderzz_offer_name"],
		Token: m["_senderzz_offer_token"],
	}
	if v := m["_senderzz_offer_value"]; v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			out.Valor = f
		}
	}
	if out.Nome != "" || out.Valor > 0 || out.Token != "" {
		out.Exists = true
	}

	// Enriquecimento via checkout_links por token (link público, price_label, tipo, produtor).
	if out.Token != "" && h.tableExists(ctx, "senderzz_checkout_links") {
		var (
			id           int64
			url          string
			priceLabel   string
			tipo         string
			displayValue float64
			produtor     string
		)
		err := h.Pool.QueryRow(ctx,
			`SELECT cl.id, COALESCE(cl.url,''), COALESCE(cl.price_label,''),
			        COALESCE(cl.tipo,''), COALESCE(cl.display_value,0),
			        COALESCE(pu.nome,'')
			   FROM senderzz_checkout_links cl
			   LEFT JOIN senderzz_portal_users pu ON pu.id = cl.producer_id
			  WHERE cl.token = $1
			  LIMIT 1`,
			out.Token).Scan(&id, &url, &priceLabel, &tipo, &displayValue, &produtor)
		if err == nil {
			out.Exists = true
			out.LinkID = &id
			out.URL = url
			out.PriceLabel = priceLabel
			out.Tipo = tipo
			out.ProdutorNome = produtor
			// Se a meta de valor veio vazia, cai para display_value do link (fonte canônica do preço).
			if out.Valor == 0 && displayValue > 0 {
				out.Valor = displayValue
			}
		}
	}
	return out
}

// loadTrackingSection — código, URL e transportadora do rastreio consolidado.
// Regra de precedência: sz_order_meta vence wc_me_labels (permite override manual
// no portal V2 sem regerar etiqueta). labelTC é fallback recebido do caller.
func (h *OrderDetailHandler) loadTrackingSection(ctx context.Context, orderID int64, labelTC, orderNumber string) orderTracking {
	keys := []string{"_tracking_code", "_tracking_url", "_tracking_carrier"}
	m := h.loadOrderMetaMap(ctx, orderID, keys)
	tr := orderTracking{
		Code:    m["_tracking_code"],
		URL:     m["_tracking_url"],
		Carrier: m["_tracking_carrier"],
	}
	// Fallback no code se meta vier vazia.
	if tr.Code == "" && labelTC != "" {
		tr.Code = labelTC
	}
	// Override manual continua vencendo. Sem override, expedições Loggi/Correios
	// abrem no Melhor Rastreio; os demais pedidos usam a página pública assinada
	// da Falk.
	if tr.URL == "" {
		tr.URL = melhorRastreioLink(tr.Carrier, []string{tr.Code})
		if tr.URL == "" {
			tr.URL = publicTrackingLink(orderNumber)
		}
	}
	return tr
}

// loadOrderHead — cabeçalho a partir de sz_orders. Retorna ErrNoRows se ausente.
// Lê também produtor_id + colunas financeiras (affiliate_amount, senderzz_fee,
// producer_net) — adicionadas via schema-fixes-v460.
func (h *OrderDetailHandler) loadOrderHead(ctx context.Context, id int64) (orderHead, *int64, error) {
	var head orderHead
	var wpOrderID, produtorID sql.NullInt64
	var sub, ship, total sql.NullFloat64
	var affAmt, fee, net sql.NullFloat64
	var deliveryFee, transactionFee sql.NullFloat64
	var status, createdAt, orderNumber sql.NullString

	err := h.Pool.QueryRow(ctx,
		`SELECT o.id, o.wp_order_id, COALESCE(o.order_number,''),
	        o.produtor_id, o.status, o.created_at::text,
	        COALESCE(o.subtotal,0), COALESCE(o.shipping,0), COALESCE(o.total,0),
	        COALESCE(o.affiliate_amount,0), COALESCE(o.senderzz_fee,0), COALESCE(f.producer_net_live, NULLIF(o.producer_net,0), (COALESCE(o.total,0) - COALESCE(o.affiliate_amount,0) - COALESCE(o.delivery_fee,0) - COALESCE(o.transaction_fee,0)), 0),
	        COALESCE(o.delivery_fee,0), COALESCE(o.transaction_fee,0)
		 FROM sz_orders o
		 LEFT JOIN sz_order_financials f ON f.order_id = o.id
		 WHERE o.id = $1`, id,
	).Scan(&head.ID, &wpOrderID, &orderNumber, &produtorID, &status, &createdAt,
		&sub, &ship, &total, &affAmt, &fee, &net, &deliveryFee, &transactionFee)
	if err != nil {
		return head, nil, err
	}
	if status.Valid {
		head.Status = status.String
	}
	if createdAt.Valid {
		head.CreatedAt = createdAt.String
	}
	if orderNumber.Valid {
		head.OrderNumber = orderNumber.String
	}
	// discount: sz_orders não possui coluna discount — deriva aritméticamente.
	// Se subtotal+shipping > total, a diferença é desconto aplicado.
	computedDiscount := sub.Float64 + ship.Float64 - total.Float64
	if computedDiscount < 0 {
		computedDiscount = 0
	}
	head.Totals = orderTotals{
		Subtotal: sub.Float64,
		Shipping: ship.Float64,
		Discount: computedDiscount,
		Total:    total.Float64,
	}
	head.Taxas = orderFees{
		SenderzzFee:  fee.Float64,
		Shipping:     ship.Float64,
		ProducerNet:  net.Float64,
		Gross:        total.Float64,
		AffiliateAmt: affAmt.Float64,
	}
	// Breakdown financeiro — parte do valor + taxa de entrega. transaction_fee é a taxa
	// de 4,99% do AFILIADO; é colocado PROVISORIAMENTE em TaxaTransacaoProdutor e
	// re-rotulado por fillFinanceiroAfiliado() (Get()), que o move para TaxaTransacaoAfiliado
	// e em seguida sobrescreve TaxaTransacaoProdutor com a taxa REAL do produtor
	// (total × sz_producer_transaction_fee_pct, default 4,99%).
	head.Financeiro = orderFinanceiro{
		ValorPedido:           total.Float64,
		TaxaEntrega:           deliveryFee.Float64,
		TaxaTransacaoProdutor: transactionFee.Float64,
	}
	// Produtor — wp_user_id em produtor_id. Resolve nome/email via portal_users.
	if produtorID.Valid && produtorID.Int64 > 0 {
		v := produtorID.Int64
		head.Produtor.ID = &v
		if h.tableExists(ctx, "senderzz_portal_users") {
			_ = h.Pool.QueryRow(ctx,
				`SELECT COALESCE(nome,''), COALESCE(email,'')
				 FROM senderzz_portal_users
				 WHERE wp_user_id = $1 OR id = $1
				 ORDER BY CASE WHEN wp_user_id = $1 THEN 0 ELSE 1 END
				 LIMIT 1`, produtorID.Int64,
			).Scan(&head.Produtor.Nome, &head.Produtor.Email)
		}
	}
	if wpOrderID.Valid {
		v := wpOrderID.Int64
		head.WCOrderID = &v
		return head, head.WCOrderID, nil
	}
	return head, nil, nil
}

// financeiroFrustratedStatuses — status FRUSTRADOS (a comissão vira penalidade de
// frustração; o líquido do produtor é zerado, mas a despesa de frustração é exposta).
// Mantido como mapa para checagem em Go (o livro COD usa lista SQL).
// REGRA DO DONO (2026-06-22): CANCELADO NÃO entra aqui — tem tratamento próprio em
// financeiroCancelledStatuses (zera TUDO, sem penalidade). 'reembolsado' permanece no
// caminho frustrado (status não nomeado pela regra do dono — não mexer).
var financeiroFrustratedStatuses = map[string]bool{
	"frustrado":   true,
	"reembolsado": true,
}

// financeiroCancelledStatuses — status CANCELADOS. REGRA DO DONO (2026-06-22): venda
// cancelada = SEM comissão. Zera TUDO (comissão bruta/líquida, taxa de transação do
// afiliado E do produtor, líquido do produtor) — distinto do FRUSTRADO, que mantém a
// comissão como penalidade. head.Status (== sz_orders.status) usa 'cancelled' como valor
// canônico (ver bridge motoboy→sz_orders); 'cancelado' coberto para dados migrados do WP.
var financeiroCancelledStatuses = map[string]bool{
	"cancelled": true,
	"cancelado": true,
}

// fillFinanceiroAfiliado — finaliza head.Financeiro com a parte da comissão do afiliado.
//
// MODELO FINANCEIRO REAL (verificado com dados — pedido 1570: total 276,
// affiliate_amount 157,34, transaction_fee 8,26; bruta = 165,60 = 276×60%):
//
//   - sz_orders.affiliate_amount  = senderzz_affiliate_transactions.amount = comissão
//     LÍQUIDA do afiliado (JÁ net) — chega aqui em aff.CommissionAmount.
//   - sz_orders.transaction_fee   = a taxa de 4,99% REAL do afiliado (fatia da plataforma).
//     loadOrderHead carrega esse valor em fin.TaxaTransacaoProdutor — é a taxa do AFILIADO,
//     não do produtor (este modelo não tem taxa de transação separada do produtor).
//   - BRUTA   = affiliate_amount + transaction_fee (= total × comissao_pct).
//   - LÍQUIDA = affiliate_amount (NÃO recalcular, NÃO aplicar ×0.9501 — esse era o BUG:
//     aplicava 4,99% de novo sobre um valor já líquido, dupla cobrança).
//   - taxa_transacao_produtor = total × sz_producer_transaction_fee_pct (senderzz_options,
//     default 4,99%). Fatia da plataforma sobre o produtor — distinta da taxa do afiliado.
//   - LÍQUIDO produtor = total − BRUTA − taxa_entrega − taxa_transacao_produtor =
//     total − affiliate_amount − transaction_fee − taxa_entrega − (total × pct).
//
// Quando o pedido está FRUSTRADO, a comissão é substituída pela taxa de frustrado (lida
// das metas) e o líquido do produtor é zerado: taxa = 0, comissão = penalidade.
// Quando o pedido está CANCELADO (REGRA DO DONO 2026-06-22), zera TUDO e retorna cedo —
// sem comissão e sem penalidade (ver financeiroCancelledStatuses).
func (h *OrderDetailHandler) fillFinanceiroAfiliado(ctx context.Context, head *orderHead, aff affiliateSection) {
	fin := &head.Financeiro
	fin.ComissaoPct = aff.CommissionPct // mantido só para exibição do % do vínculo

	// CANCELADO (REGRA DO DONO 2026-06-22): venda cancelada = SEM comissão. Zera TUDO e
	// retorna ANTES de qualquer cálculo de comissão/taxa. NÃO é frustrado (Frustrado=false):
	// não há penalidade de frustração, ao contrário do bloco frustrado abaixo. Os 5 campos
	// precisam ser zerados EXPLICITAMENTE — loadOrderHead já pré-preencheu TaxaTransacaoProdutor
	// com sz_orders.transaction_fee, então o zero-default do struct não basta (vazaria a taxa).
	if financeiroCancelledStatuses[head.Status] {
		fin.ComissaoAfiliadoBruta = 0
		fin.ComissaoAfiliadoLiquida = 0
		fin.TaxaTransacaoAfiliado = 0
		fin.TaxaTransacaoProdutor = 0
		fin.LiquidoProdutor = 0
		fin.Frustrado = false
		return
	}

	// taxaAfiliado = sz_orders.transaction_fee (a taxa de 4,99% REAL do afiliado), carregado
	// por loadOrderHead em fin.TaxaTransacaoProdutor. Captura ANTES de zerar o campo abaixo,
	// pois neste modelo NÃO há taxa de transação separada do produtor (guard query 2026-06-18:
	// transaction_fee>0 sempre implica affiliate_amount>0).
	taxaAfiliado := fin.TaxaTransacaoProdutor

	// LÍQUIDA = affiliate_amount armazenado (aff.CommissionAmount), que JÁ é líquido.
	// Não aplicar comissaoLiquidaAfiliado()/×0.9501 — seria a dupla cobrança de 4,99%.
	liquida := aff.CommissionAmount

	// REGRA: a taxa de transação (4,99%) e a BRUTA cheia só se aplicam quando a comissão é
	// NORMAL. Discriminador combina DOIS sinais: a query de loadAffiliateSection já filtra
	// type='commission', então penalty nunca chega como linha — o frustrado vem pelo STATUS
	// DO PEDIDO. Condição: não-frustrado E status da transação fora de ('cancelled','reversed').
	// Para frustrado/revertido: taxa = 0, BRUTA = LÍQUIDA (sem a fatia de 4,99%).
	// (CANCELADO já retornou cedo acima — não passa por aqui.)
	aplicaTaxaAfiliado := !financeiroFrustratedStatuses[head.Status] &&
		aff.StatusTransacao != "cancelled" && aff.StatusTransacao != "reversed"
	var bruta float64
	if aplicaTaxaAfiliado {
		bruta = liquida + taxaAfiliado // = affiliate_amount + transaction_fee
		fin.TaxaTransacaoAfiliado = taxaAfiliado
		fin.ComissaoAfiliadoLiquida = liquida
	} else {
		bruta = liquida
		fin.TaxaTransacaoAfiliado = 0
		fin.ComissaoAfiliadoLiquida = liquida
	}
	fin.ComissaoAfiliadoBruta = bruta

	// taxa_transacao_produtor = total × sz_producer_transaction_fee_pct (senderzz_options,
	// default 4,99%). Fatia da plataforma sobre o produtor, distinta da taxa do AFILIADO
	// (capturada em taxaAfiliado / exibida em TaxaTransacaoAfiliado). Sobrescreve o valor
	// provisório (transaction_fee do afiliado) que loadOrderHead deixou em TaxaTransacaoProdutor
	// — taxaAfiliado já foi salvo acima, então não há subtração dupla no líquido abaixo.
	// pct é percentual (4,99 → 4,99%), por isso ÷100; NÃO reaplicar ×0.9501.
	producerPct := h.getOptionFloat(ctx, "sz_producer_transaction_fee_pct", 4.99)
	fin.TaxaTransacaoProdutor = fin.ValorPedido * producerPct / 100

	// LÍQUIDO produtor = valor − BRUTA(inteira) − taxa_entrega − taxa_transacao_produtor.
	// Com BRUTA = affiliate_amount + transaction_fee, dá total − affiliate_amount −
	// transaction_fee − taxa_entrega − (total × pct).
	fin.LiquidoProdutor = fin.ValorPedido - bruta - fin.TaxaEntrega - fin.TaxaTransacaoProdutor

	// Frustrado: substitui a comissão pela taxa de frustrado e zera o líquido do produtor.
	// Também zera a taxa de transação do produtor: sem líquido, não há fatia da plataforma.
	if financeiroFrustratedStatuses[head.Status] {
		fin.Frustrado = true
		fin.LiquidoProdutor = 0
		fin.TaxaTransacaoProdutor = 0

		// Config da taxa de frustração (REGRA DO DONO — configurável).
		// type: none|fixed|percent (default none); value: valor base (default 0).
		// none → 0; fixed → value; percent → ValorPedido(total) × value / 100.
		feeType := h.getOptionString(ctx, "sz_frustration_fee_type", "none")
		feeValue := h.getOptionFloat(ctx, "sz_frustration_fee_value", 0)
		computeFrustrationFee := func() float64 {
			switch feeType {
			case "fixed":
				return feeValue
			case "percent":
				return fin.ValorPedido * feeValue / 100
			default: // "none" e qualquer valor inesperado
				return 0
			}
		}

		// TAXA DE FRUSTRAÇÃO COBRADA DE CADA PARTE (REGRA DO DONO 2026-06-23):
		// expor a taxa REAL que foi cobrada do afiliado e do produtor, congelada na baixa.
		//
		// AFILIADO — fonte da verdade = linha type='penalty' em
		// senderzz_affiliate_transactions (o débito de fato lançado no razão do afiliado;
		// ex.: pedidos 1380/1394/1500 → 5,00; 1546/1562 → 12,00). PRECEDÊNCIA:
		//   1) penalty transaction (valor COBRADO, autoritativo);
		//   2) meta _sz_aff_frustration_penalty (pedido antigo sem linha de penalty);
		//   3) config (pedido novo sem nenhum dos dois).
		// NÃO usar mp.valor_taxa_frustrado_afiliado — esse é o fee do MOTOBOY (rota.go:589),
		// não a penalidade do afiliado.
		//
		// PRODUTOR — sem linha de penalty própria no razão; mantém a precedência meta →
		// config. _sz_prod_frustration_penalty é o valor cobrado armazenado (0 nos dados reais).
		m := h.loadOrderMetaMap(ctx, head.ID,
			[]string{"_sz_aff_frustration_penalty", "_sz_prod_frustration_penalty"})
		if penalty, ok := h.loadAffiliatePenalty(ctx, head.ID); ok {
			fin.ValorFrustradoAfiliado = penalty
			fin.TaxaFrustracaoAfiliado = penalty // COBRADO: linha penalty do razão (autoritativo).
		} else if v := m["_sz_aff_frustration_penalty"]; v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				fin.ValorFrustradoAfiliado = f
				fin.TaxaFrustracaoAfiliado = f // ANTIGO: usa o valor já cobrado armazenado na meta.
			}
		} else {
			fin.TaxaFrustracaoAfiliado = computeFrustrationFee() // NOVO: da config.
		}
		if v := m["_sz_prod_frustration_penalty"]; v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				fin.ValorFrustradoProdutor = f
				fin.TaxaFrustracaoProdutor = f // ANTIGO: usa o valor já cobrado armazenado.
			}
		}

		// PREJUÍZO EXPLÍCITO de cada parte (REGRA DO DONO 2026-06-22). Computado por ÚLTIMO,
		// depois que ComissaoAfiliadoBruta (linha ~781), TaxaEntrega (intocada no frustrado)
		// e TaxaFrustracao* já estão resolvidos.
		//   afiliado = comissão bruta (que vira penalidade) + taxa de frustração do afiliado.
		//   produtor = taxa de entrega já gasta (sem venda) + taxa de frustração do produtor.
		// Valores POSITIVOS (magnitude); o front exibe negativo/danger. Pedido 1569 (frustrado,
		// total 147, bruta 83,80, entrega 23,98, taxas de frustração 0 pois config=none):
		//   prejuizo_afiliado = 83,80 + 0 = 83,80 ; prejuizo_produtor = 23,98 + 0 = 23,98.
		fin.PrejuizoAfiliado = fin.ComissaoAfiliadoBruta + fin.TaxaFrustracaoAfiliado
		fin.PrejuizoProdutor = fin.TaxaEntrega + fin.TaxaFrustracaoProdutor
	}
}

// loadOrderItemsFull — itens completos com produto_id, sku, meta JSONB.
// Usado pela seção "Itens do pedido" da UI consolidada.
func (h *OrderDetailHandler) loadOrderItemsFull(ctx context.Context, orderID int64) []orderItemFull {
	items := []orderItemFull{}
	if !h.tableExists(ctx, "sz_order_items") {
		return items
	}
	rows, err := h.Pool.Query(ctx,
		`SELECT id, COALESCE(produto_id,0), COALESCE(nome,''), COALESCE(sku,''),
		        COALESCE(quantidade,0), COALESCE(preco_unit,0), COALESCE(subtotal,0),
		        COALESCE(meta::text, '')
		 FROM sz_order_items
		 WHERE order_id = $1
		 ORDER BY id ASC`, orderID)
	if err != nil {
		return items
	}
	defer rows.Close()
	for rows.Next() {
		var it orderItemFull
		var metaStr string
		if err := rows.Scan(&it.ID, &it.ProdutoID, &it.Nome, &it.SKU,
			&it.Qty, &it.PrecoUnit, &it.Subtotal, &metaStr); err == nil {
			if metaStr != "" {
				it.Meta = json.RawMessage(metaStr)
			} else {
				it.Meta = json.RawMessage(`null`)
			}
			items = append(items, it)
		}
	}
	return items
}

// loadFullAddress — busca endereço completo para um tipo ('shipping' ou 'billing').
// Inclui nome/email/telefone — para as seções "Endereço de entrega/cobrança".
func (h *OrderDetailHandler) loadFullAddress(ctx context.Context, orderID int64, tipo string) orderFullAddress {
	addr := orderFullAddress{Pais: "BR"}
	if !h.tableExists(ctx, "sz_order_addresses") {
		return addr
	}
	var nome, email, tel, cep, log, num, comp, bairro, cid, uf, pais sql.NullString
	err := h.Pool.QueryRow(ctx,
		`SELECT COALESCE(nome,''), COALESCE(email,''), COALESCE(telefone,''),
		        COALESCE(cep,''), COALESCE(logradouro,''), COALESCE(numero,''),
		        COALESCE(complemento,''), COALESCE(bairro,''), COALESCE(cidade,''),
		        COALESCE(uf,''), COALESCE(pais,'BR')
		 FROM sz_order_addresses
		 WHERE order_id = $1 AND tipo = $2
		 LIMIT 1`, orderID, tipo,
	).Scan(&nome, &email, &tel, &cep, &log, &num, &comp, &bairro, &cid, &uf, &pais)
	if err != nil {
		return addr
	}
	addr.Exists = true
	addr.Nome, addr.Email, addr.Telefone = nome.String, email.String, tel.String
	addr.CEP, addr.Logradouro, addr.Numero = cep.String, log.String, num.String
	addr.Complemento, addr.Bairro, addr.Cidade = comp.String, bairro.String, cid.String
	addr.UF, addr.Pais = uf.String, pais.String
	// BUGFIX número: em pedidos migrados do WP a coluna numero às vezes vem vazia
	// (o número ficou na meta _shipping_number / _billing_number do WooCommerce).
	// Recupera da meta quando a coluna do endereço não trouxe valor.
	if addr.Numero == "" {
		addr.Numero = h.addressNumberFallback(ctx, orderID, tipo)
	}
	if addr.Logradouro == "" {
		addr.Logradouro = h.addressStreetFallback(ctx, orderID, tipo)
	}
	if addr.Bairro == "" {
		addr.Bairro = h.addressNeighborhoodFallback(ctx, orderID, tipo)
	}
	return addr
}

// addressStreetFallback — recupera logradouro de WP postmeta (_shipping_address_1).
func (h *OrderDetailHandler) addressStreetFallback(ctx context.Context, orderID int64, tipo string) string {
	keys := []string{"_shipping_address_1", "_billing_address_1"}
	if tipo == "billing" {
		keys = []string{"_billing_address_1", "_shipping_address_1"}
	}
	m := h.loadOrderMetaMap(ctx, orderID, keys)
	for _, k := range keys {
		if v := m[k]; v != "" {
			return v
		}
	}
	return ""
}

// addressNeighborhoodFallback — recupera bairro de WP postmeta (_shipping_neighborhood).
func (h *OrderDetailHandler) addressNeighborhoodFallback(ctx context.Context, orderID int64, tipo string) string {
	keys := []string{"_shipping_neighborhood", "_billing_neighborhood", "_shipping_district", "_billing_district"}
	if tipo == "billing" {
		keys = []string{"_billing_neighborhood", "_shipping_neighborhood", "_billing_district", "_shipping_district"}
	}
	m := h.loadOrderMetaMap(ctx, orderID, keys)
	for _, k := range keys {
		if v := m[k]; v != "" {
			return v
		}
	}
	return ""
}

// addressNumberFallback — recupera o número do endereço das metas do WooCommerce
// quando sz_order_addresses.numero veio vazio. tipo='shipping' → _shipping_number;
// 'billing' → _billing_number (com fallback cruzado). Retorna "" se nada encontrado.
func (h *OrderDetailHandler) addressNumberFallback(ctx context.Context, orderID int64, tipo string) string {
	keys := []string{"_shipping_number", "_billing_number", "_shipping_house_number", "_billing_house_number"}
	if tipo == "billing" {
		keys = []string{"_billing_number", "_shipping_number", "_billing_house_number", "_shipping_house_number"}
	}
	m := h.loadOrderMetaMap(ctx, orderID, keys)
	for _, k := range keys {
		if v := m[k]; v != "" {
			return v
		}
	}
	return ""
}

// loadOrderItems — itens de sz_order_items. Tabela ausente → slice vazio.
func (h *OrderDetailHandler) loadOrderItems(ctx context.Context, orderID int64) []orderItem {
	items := []orderItem{}
	if !h.tableExists(ctx, "sz_order_items") {
		return items
	}
	rows, err := h.Pool.Query(ctx,
		`SELECT COALESCE(nome,''),
		        COALESCE(quantidade,0),
		        COALESCE(preco_unit,0),
		        COALESCE(subtotal,0)
		 FROM sz_order_items
		 WHERE order_id = $1
		 ORDER BY id ASC`, orderID)
	if err != nil {
		return items
	}
	defer rows.Close()
	for rows.Next() {
		var it orderItem
		if err := rows.Scan(&it.Produto, &it.Qty, &it.PrecoUnit, &it.Subtotal); err == nil {
			items = append(items, it)
		}
	}
	return items
}

// loadOrderAddress — endereço de entrega (shipping). Fallback p/ billing se ausente.
func (h *OrderDetailHandler) loadOrderAddress(ctx context.Context, orderID int64) orderAddress {
	addr := orderAddress{}
	if !h.tableExists(ctx, "sz_order_addresses") {
		return addr
	}
	// Preferência: shipping → billing.
	row := h.Pool.QueryRow(ctx,
		`SELECT COALESCE(logradouro,''), COALESCE(numero,''), COALESCE(complemento,''),
		        COALESCE(bairro,''), COALESCE(cidade,''), COALESCE(uf,''), COALESCE(cep,'')
		 FROM sz_order_addresses
		 WHERE order_id = $1
		 ORDER BY CASE WHEN tipo='shipping' THEN 0 ELSE 1 END, id ASC
		 LIMIT 1`, orderID)
	_ = row.Scan(&addr.Endereco, &addr.Numero, &addr.Complemento,
		&addr.Bairro, &addr.Cidade, &addr.UF, &addr.CEP)
	// BUGFIX número: fallback para meta do WP (preferência shipping) quando vazio.
	if addr.Numero == "" {
		addr.Numero = h.addressNumberFallback(ctx, orderID, "shipping")
	}
	return addr
}

// loadOrderCustomer — cliente vindo do endereço de entrega ou portal_users.
func (h *OrderDetailHandler) loadOrderCustomer(ctx context.Context, orderID int64) orderCustomer {
	c := orderCustomer{}
	if h.tableExists(ctx, "sz_order_addresses") {
		_ = h.Pool.QueryRow(ctx,
			`SELECT COALESCE(nome,''), COALESCE(email,''), COALESCE(telefone,'')
			 FROM sz_order_addresses
			 WHERE order_id = $1
			 ORDER BY CASE WHEN tipo='billing' THEN 0 ELSE 1 END, id ASC
			 LIMIT 1`, orderID,
		).Scan(&c.Nome, &c.Email, &c.Telefone)
	}
	// CPF e RG são opcionais — busca em sz_order_meta. Uma query só, retorna mapa.
	if h.tableExists(ctx, "sz_order_meta") {
		keys := []string{"_billing_cpf", "_billing_doc", "cpf", "document", "_billing_rg", "rg"}
		m := h.loadOrderMetaMap(ctx, orderID, keys)
		// CPF: primeira chave não-vazia segue a ordem original (cpf canonical).
		for _, k := range []string{"_billing_cpf", "_billing_doc", "cpf", "document"} {
			if v := m[k]; v != "" {
				c.CPF = v
				break
			}
		}
		// RG: chaves diretas.
		for _, k := range []string{"_billing_rg", "rg"} {
			if v := m[k]; v != "" {
				c.RG = v
				break
			}
		}
	}
	return c
}

// loadMotoboySection — toda informação Motoboy COD para o WC order id.
// Retorna exists=false se a tabela não existe, se wcOrderID==0 ou se não há linha.
func (h *OrderDetailHandler) loadMotoboySection(ctx context.Context, wcOrderID int64) motoboySection {
	out := motoboySection{Comprovantes: []comprovante{}, Audit: []motoboyAudit{}}
	if wcOrderID <= 0 || !h.tableExists(ctx, "sz_motoboy_pedidos") {
		return out
	}

	// Detecta presença das tabelas auxiliares para montar a query principal.
	hasMotoboys := h.tableExists(ctx, "sz_motoboys")
	hasCDs := h.tableExists(ctx, "sz_motoboy_cds")
	hasZonas := h.tableExists(ctx, "sz_motoboy_zonas")

	motNome := `''::text AS motoboy_nome, ''::text AS motoboy_tel, ''::text AS motoboy_placa`
	motJoin := ""
	if hasMotoboys {
		motNome = `COALESCE(m.nome,'') AS motoboy_nome,
		           COALESCE(m.telefone,'') AS motoboy_tel,
		           COALESCE(m.placa,'') AS motoboy_placa`
		motJoin = `LEFT JOIN sz_motoboys m ON m.id = mp.motoboy_id`
	}
	cdNome := `''::text AS cd_nome`
	cdJoin := ""
	if hasCDs {
		cdNome = `COALESCE(c.nome,'') AS cd_nome`
		cdJoin = `LEFT JOIN sz_motoboy_cds c ON c.id = mp.cd_id`
	}
	zonaNome := `''::text AS zona_nome`
	zonaJoin := ""
	if hasZonas {
		zonaNome = `COALESCE(z.nome,'') AS zona_nome`
		zonaJoin = `LEFT JOIN sz_motoboy_zonas z ON z.id = mp.zona_id`
	}

	var pedidoID int64
	err := h.Pool.QueryRow(ctx,
		`SELECT mp.id, mp.motoboy_id, `+motNome+`, `+cdNome+`, `+zonaNome+`,
		        COALESCE(mp.status,''),
		        COALESCE(mp.dest_produto,''),
		        COALESCE(mp.valor_pedido,0),
		        COALESCE(mp.pgto_dinheiro,0),
		        COALESCE(mp.pgto_pix,0),
		        COALESCE(mp.pgto_cartao,0),
		        COALESCE(mp.recebedor_nome,''),
		        COALESCE(mp.recebedor_tipo,''),
		        COALESCE(mp.recebedor_cpf,''),
		        COALESCE(mp.baixa_por,''),
		        mp.baixa_admin_user_id,
		        mp.baixa_motoboy_id,
		        mp.baixa_at::text,
		        mp.entrega_lat,
		        mp.entrega_lng,
		        COALESCE(mp.frustrado_motivo,''),
		        COALESCE(mp.frustrado_observacao,''),
		        COALESCE(mp.repasse_confirmado, FALSE),
		        mp.repasse_ts::text
		 FROM sz_motoboy_pedidos mp
		 `+motJoin+`
		 `+cdJoin+`
		 `+zonaJoin+`
		 WHERE mp.wc_order_id = $1
		 LIMIT 1`, wcOrderID,
	).Scan(
		&pedidoID, &out.MotoboyID, &out.MotoboyNome, &out.MotoboyTelefone, &out.MotoboyPlaca,
		&out.CDNome, &out.ZonaNome,
		&out.Status, &out.DestProduto,
		&out.ValorPedido, &out.PgtoDinheiro, &out.PgtoPix, &out.PgtoCartao,
		&out.RecebedorNome, &out.RecebedorTipo, &out.RecebedorCPF,
		&out.BaixaPor, &out.BaixaAdminUserID, &out.BaixaMotoboyID, &out.BaixaAt,
		&out.EntregaLat, &out.EntregaLng,
		&out.FrustradoMotivo, &out.FrustradoObservacao,
		&out.RepasseConfirmado, &out.RepasseTS,
	)
	if err != nil {
		return out
	}
	out.Exists = true
	out.PedidoID = &pedidoID // AUDIT-2026-06-22: expõe id p/ reagendar/cancelar no front

	// Comprovantes — chaveados por pedido_id, fallback para wc_order_id.
	if h.tableExists(ctx, "sz_motoboy_comprovantes") {
		rows, err := h.Pool.Query(ctx,
			`SELECT id, COALESCE(tipo_pgto,''), COALESCE(foto_url,''),
			        COALESCE(baixa_por,''), created_at::text
			 FROM sz_motoboy_comprovantes
			 WHERE pedido_id = $1 OR wc_order_id = $2
			 ORDER BY created_at ASC`, pedidoID, wcOrderID)
		if err == nil {
			for rows.Next() {
				var c comprovante
				if err := rows.Scan(&c.ID, &c.TipoPgto, &c.FotoURL, &c.BaixaPor, &c.CreatedAt); err == nil {
					out.Comprovantes = append(out.Comprovantes, c)
				}
			}
			rows.Close()
		}
	}

	// Auditoria — sempre por pedido_id, ordem cronológica.
	if h.tableExists(ctx, "sz_motoboy_audit") {
		// meta_json é TEXT em Postgres — sem cast necessário.
		rows, err := h.Pool.Query(ctx,
			`SELECT id, COALESCE(actor_tipo,''), actor_id, COALESCE(acao,''),
			        de_status, para_status,
			        COALESCE(meta_json, ''),
			        created_at::text
			 FROM sz_motoboy_audit
			 WHERE pedido_id = $1
			 ORDER BY created_at ASC, id ASC`, pedidoID)
		if err == nil {
			for rows.Next() {
				var a motoboyAudit
				var metaStr string
				if err := rows.Scan(&a.ID, &a.ActorTipo, &a.ActorID, &a.Acao,
					&a.DeStatus, &a.ParaStatus, &metaStr, &a.CreatedAt); err == nil {
					if metaStr != "" {
						a.MetaJSON = json.RawMessage(metaStr)
					} else {
						a.MetaJSON = json.RawMessage(`null`)
					}
					out.Audit = append(out.Audit, a)
				}
			}
			rows.Close()
		}
	}

	return out
}

// loadAffiliateSection — afiliado + comissão para o pedido.
// Tenta primeiro senderzz_affiliate_transactions (esperada pelo audit engine);
// se não existir, cai para senderzz_affiliate_commissions com mapeamento de status.
func (h *OrderDetailHandler) loadAffiliateSection(ctx context.Context, orderID int64) affiliateSection {
	out := affiliateSection{}

	hasAffTx := h.tableExists(ctx, "senderzz_affiliate_transactions")
	hasAffComm := h.tableExists(ctx, "senderzz_affiliate_commissions")
	hasAffiliates := h.tableExists(ctx, "senderzz_affiliates")
	hasUsers := h.tableExists(ctx, "senderzz_portal_users")

	// Tabela base: tx se existir, senão commissions.
	var (
		affID  sql.NullInt64
		amount sql.NullFloat64
		status sql.NullString
	)
	fromTx := false
	if hasAffTx {
		_ = h.Pool.QueryRow(ctx,
			`SELECT affiliate_id, COALESCE(amount,0), COALESCE(status,'')
			 FROM senderzz_affiliate_transactions
			 WHERE order_id = $1 AND type = 'commission'
			 ORDER BY id ASC LIMIT 1`, orderID,
		).Scan(&affID, &amount, &status)
		fromTx = affID.Valid && affID.Int64 > 0
	} else if hasAffComm {
		_ = h.Pool.QueryRow(ctx,
			`SELECT affiliate_id, COALESCE(valor,0), COALESCE(status,'')
			 FROM senderzz_affiliate_commissions
			 WHERE order_id = $1
			 ORDER BY id ASC LIMIT 1`, orderID,
		).Scan(&affID, &amount, &status)
	}

	// Fallback: ler affiliate_id + affiliate_amount direto de sz_orders
	// caso nenhum livro razão tenha registro ainda (pedido novo).
	if !affID.Valid || affID.Int64 <= 0 {
		var oAffID sql.NullInt64
		var oAmt sql.NullFloat64
		_ = h.Pool.QueryRow(ctx,
			`SELECT affiliate_id, COALESCE(affiliate_amount,0)
			 FROM sz_orders WHERE id = $1`, orderID,
		).Scan(&oAffID, &oAmt)
		if oAffID.Valid && oAffID.Int64 > 0 {
			affID = oAffID
			amount = oAmt
			fromTx = false
		}
	}

	if !affID.Valid || affID.Int64 <= 0 {
		return out
	}

	out.Exists = true
	out.AffiliateID = &affID.Int64
	out.CommissionAmount = amount.Float64
	out.StatusTransacao = normalizeAffiliateStatus(status.String, hasAffTx)

	// Dados do vínculo + produtor + nomes.
	if hasAffiliates {
		var prodID sql.NullInt64
		var afiliadoWP sql.NullInt64
		var pct sql.NullFloat64
		if fromTx {
			_ = h.Pool.QueryRow(ctx,
				`SELECT produtor_id, afiliado_id, COALESCE(comissao_pct,0)
				 FROM senderzz_affiliates
				 WHERE id = $1 LIMIT 1`, affID.Int64,
			).Scan(&prodID, &afiliadoWP, &pct)
		} else {
			_ = h.Pool.QueryRow(ctx,
				`SELECT produtor_id, afiliado_id, COALESCE(comissao_pct,0)
				 FROM senderzz_affiliates
				 WHERE afiliado_id = $1
				 ORDER BY id ASC
				 LIMIT 1`, affID.Int64,
			).Scan(&prodID, &afiliadoWP, &pct)
		}
		if pct.Valid {
			out.CommissionPct = pct.Float64
		}
		if prodID.Valid && prodID.Int64 > 0 {
			pidCopy := prodID.Int64
			out.ProducerID = &pidCopy
			if hasUsers {
				_ = h.Pool.QueryRow(ctx,
					`SELECT COALESCE(nome,'')
					 FROM senderzz_portal_users
					 WHERE wp_user_id = $1 OR id = $1
					 ORDER BY CASE WHEN wp_user_id = $1 THEN 0 ELSE 1 END
					 LIMIT 1`, prodID.Int64,
				).Scan(&out.ProducerNome)
			}
		}
		if afiliadoWP.Valid && afiliadoWP.Int64 > 0 && hasUsers {
			_ = h.Pool.QueryRow(ctx,
				`SELECT COALESCE(nome,''), COALESCE(email,'')
				 FROM senderzz_portal_users
				 WHERE wp_user_id = $1 OR id = $1
				 ORDER BY CASE WHEN wp_user_id = $1 THEN 0 ELSE 1 END
				 LIMIT 1`, afiliadoWP.Int64,
			).Scan(&out.AffiliateNome, &out.AffiliateEmail)
		}
	}

	return out
}

// loadAffiliatePenalty — lê a TAXA DE FRUSTRAÇÃO cobrada do afiliado a partir da linha
// type='penalty' em senderzz_affiliate_transactions (o débito de fato lançado no razão
// quando o pedido frustrou). Retorna (valor, true) só quando há uma penalidade efetiva
// (>0, status não revertido/cancelado). Tabela ausente / sem linha → (0, false), para o
// caller cair na meta / config. order_id aqui é sz_orders.id (== head.ID).
func (h *OrderDetailHandler) loadAffiliatePenalty(ctx context.Context, orderID int64) (float64, bool) {
	if !h.tableExists(ctx, "senderzz_affiliate_transactions") {
		return 0, false
	}
	var amount sql.NullFloat64
	err := h.Pool.QueryRow(ctx,
		`SELECT COALESCE(amount,0)
		   FROM senderzz_affiliate_transactions
		  WHERE order_id = $1 AND type = 'penalty'
		    AND status NOT IN ('reversed','cancelled','estornada')
		  ORDER BY id ASC LIMIT 1`, orderID,
	).Scan(&amount)
	if err != nil || !amount.Valid || amount.Float64 <= 0 {
		return 0, false
	}
	return amount.Float64, true
}

// normalizeAffiliateStatus — mapeia status legados (pendente/aprovada/paga/estornada)
// para vocabulário esperado pela UI (pending/available/paid/cancelled). Quando lendo
// de senderzz_affiliate_transactions (nomenclatura nova), passa direto.
func normalizeAffiliateStatus(raw string, fromTx bool) string {
	if fromTx {
		return raw
	}
	switch raw {
	case "pendente":
		return "pending"
	case "aprovada":
		return "available"
	case "paga":
		return "paid"
	case "estornada":
		return "cancelled"
	}
	return raw
}

// loadLabelSection — etiqueta Melhor Envio. Tabela ausente / sem linha → exists=false.
// Mapeamento spec → schema-labels.sql:
//
//	me_order_id      ← me_shipment_id
//	item_id          ← me_label_id
//	print_url        ← label_url
//	pdf_local_url    ← label_pdf_path
//	invoice_id, custom_service_id, bought_shipping, reverse → null (sem coluna nativa)
func (h *OrderDetailHandler) loadLabelSection(ctx context.Context, wcOrderID int64) labelSection {
	out := labelSection{Reverse: labelReverse{}}
	if wcOrderID <= 0 || !h.tableExists(ctx, "wc_me_labels") {
		return out
	}
	var (
		labelDBID    sql.NullInt64
		meShipment   sql.NullString
		meLabel      sql.NullString
		status       sql.NullString
		serviceName  sql.NullString
		trackingCode sql.NullString
		labelURL     sql.NullString
		labelPDF     sql.NullString
		createdAt    sql.NullString
		priceReal    sql.NullFloat64
	)
	err := h.Pool.QueryRow(ctx,
		`SELECT id, me_shipment_id, me_label_id, COALESCE(status,''),
		        service_name, tracking_code, label_url, label_pdf_path,
		        created_at::text, price::float8
		 FROM wc_me_labels
		 WHERE wc_order_id = $1 AND status <> 'canceled'
		 ORDER BY id DESC LIMIT 1`, wcOrderID,
	).Scan(&labelDBID, &meShipment, &meLabel, &status, &serviceName, &trackingCode,
		&labelURL, &labelPDF, &createdAt, &priceReal)
	if err != nil {
		return out
	}
	out.Exists = true
	// LabelID exposto para o botão cancelar na UI — chama /labels/{label_id}/cancel.
	if labelDBID.Valid {
		v := labelDBID.Int64
		out.LabelID = &v
	}
	if status.Valid {
		out.Status = status.String
	}
	if meShipment.Valid {
		v := meShipment.String
		out.MEOrderID = &v
	}
	if meLabel.Valid {
		v := meLabel.String
		out.ItemID = &v
	}
	if labelURL.Valid {
		v := labelURL.String
		out.PrintURL = &v
	}
	if labelPDF.Valid {
		v := labelPDF.String
		out.PDFLocalURL = &v
	}
	if serviceName.Valid {
		v := serviceName.String
		out.ServiceName = &v
	}
	if trackingCode.Valid {
		v := trackingCode.String
		out.TrackingCode = &v
	}
	if createdAt.Valid {
		v := createdAt.String
		out.GeneratedAt = &v
	}
	if priceReal.Valid {
		v := priceReal.Float64
		out.PriceReal = &v
	}
	// invoice_id / custom_service_id / bought_shipping / error / reverse não existem
	// no schema Postgres — eram postmeta no WP. Ficam null intencionalmente.
	return out
}

// ── POST /orders/{id}/force-motoboy-status ────────────────────────────────────

type forceStatusBody struct {
	TargetStatus  string   `json:"target_status"`
	Motivo        string   `json:"motivo"`
	Observacao    string   `json:"observacao"`
	ComprovURL    string   `json:"comprov_url"`
	BarcodeBipado string   `json:"barcode_bipado"`
	GpsLat        *float64 `json:"gps_lat"`
	GpsLng        *float64 `json:"gps_lng"`
}

type financialStatusBody struct {
	FinancialStatus      string `json:"financial_status"`
	ScheduledPaymentDate string `json:"scheduled_payment_date"`
	Observacao           string `json:"observacao"`
}

// gpsDeliveryRadiusMeters — distância máxima entre o GPS do dispositivo e o
// endereço de entrega para aceitar a confirmação de "entregue". Acima disso,
// 422 gps_too_far (o motoboy/OL precisa estar fisicamente perto do destino).
const gpsDeliveryRadiusMeters = 400.0

// haversineMeters — distância em metros entre duas coordenadas (raio da Terra
// 6371km). Usado só para o gate de proximidade de entrega — precisão de
// alguns metros é irrelevante para um raio de centenas de metros.
func haversineMeters(lat1, lng1, lat2, lng2 float64) float64 {
	const earthRadiusM = 6371000.0
	toRad := func(d float64) float64 { return d * math.Pi / 180 }
	dLat := toRad(lat2 - lat1)
	dLng := toRad(lng2 - lng1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRad(lat1))*math.Cos(toRad(lat2))*math.Sin(dLng/2)*math.Sin(dLng/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusM * c
}

type changeMotoboyBody struct {
	MotoboyID int64 `json:"motoboy_id"`
}

var allowedForceStatus = map[string]bool{
	"agendado":  true,
	"embalado":  true,
	"em_rota":   true,
	"entregue":  true,
	"frustrado": true,
	"cancelado": true,
}

var allowedFinancialStatus = map[string]bool{
	"pagamento_agendado": true,
	"vencido":            true,
	"concluido":          true,
}

func normalizePackCode(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

func (h *OrderDetailHandler) loadExpectedPackCodes(ctx context.Context, orderID int64) ([]string, error) {
	if !h.tableExists(ctx, "sz_order_items") {
		return nil, nil
	}
	rows, err := h.Pool.Query(ctx, `
		SELECT DISTINCT COALESCE(NULLIF(sp.barcode,''), NULLIF(sp.sku,'')) AS code
		  FROM sz_order_items oi
		  LEFT JOIN sz_products sp
		    ON sp.id = oi.produto_id OR sp.wp_post_id = oi.produto_id
		 WHERE oi.order_id = $1
		   AND COALESCE(NULLIF(sp.barcode,''), NULLIF(sp.sku,'')) IS NOT NULL
	`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	codes := []string{}
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err == nil {
			code = strings.TrimSpace(code)
			if code != "" {
				codes = append(codes, normalizePackCode(code))
			}
		}
	}
	return codes, nil
}

// statusTimestampColumn — coluna ts_* a atualizar em sz_motoboy_pedidos por status.
// agendado e cancelado não têm coluna dedicada — só batem updated_at.
func statusTimestampColumn(status string) string {
	switch status {
	case "embalado":
		return "ts_embalado"
	case "em_rota":
		return "ts_em_rota"
	case "entregue":
		return "ts_entregue"
	case "frustrado":
		return "ts_frustrado"
	}
	return ""
}

// forceMotoboyStatusToSzOrder mapeia o status forçado pelo admin para o status
// equivalente em sz_orders (restrito ao CHECK constraint de sz_orders.status —
// ver infra/postgres/schema-orders.sql:52). Espelha motoboyStatusToSzOrder() de
// go/motoboy/internal/handlers/status_bridge.go (o admin-service não pode
// importar o handler do motoboy-service, então a lógica é replicada aqui).
//
// Retorna ok=false para 'agendado' (sem equivalente CHECK-válido relevante) —
// nesse caso o bridge é pulado e só sz_motoboy_pedidos é atualizado.
func forceMotoboyStatusToSzOrder(motoboyStatus string) (szStatus string, ok bool) {
	switch motoboyStatus {
	case "entregue":
		return "completo", true // COD Motoboy entregue = Woo "Completo".
	case "frustrado":
		return "frustrado", true
	case "cancelado":
		return "cancelled", true
	case "embalado":
		return "embalado", true
	case "em_rota":
		return "enviado", true // sz_orders não aceita em_rota; enviado representa o trânsito.
	default:
		return "", false // 'agendado' (e qualquer outro) → sem espelho.
	}
}

// ForceMotoboyStatus — admin força transição manual.
// Wrappa UPDATE + audit em transação. actor_tipo='admin', actor_id do JWT.
func (h *OrderDetailHandler) ForceMotoboyStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Gate: portal users só OL podem forçar status (produtor/afiliado → 403).
	if actor := auth.ActorFromCtx(ctx); actor != nil && actor.Kind != auth.ActorAdmin {
		if actor.Kind != auth.ActorKind("operator") && actor.Kind != auth.ActorKind("operador") {
			httpx.Err(w, 403, "forbidden", "acesso restrito a admin e operador logístico")
			return
		}
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "order_id inválido")
		return
	}

	var body forceStatusBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	target := body.TargetStatus
	if !allowedForceStatus[target] {
		httpx.Err(w, 400, "bad_request",
			"target_status inválido — aceitos: agendado, embalado, em_rota, entregue, frustrado, cancelado")
		return
	}

	validatePackBarcode := h.getOptionBool(ctx, "sz_pack_barcode_validation_enabled", true)

	if !h.tableExists(ctx, "sz_orders") || !h.tableExists(ctx, "sz_motoboy_pedidos") {
		httpx.Err(w, 503, "tables_missing", "tabelas de pedidos motoboy ainda não migradas")
		return
	}

	// Resolve a chave de sz_motoboy_pedidos.wc_order_id. Pedidos nativos Go (checkout
	// COD) têm wp_order_id NULL e sz_motoboy_pedidos.wc_order_id == sz_orders.id, então
	// usa-se COALESCE(wp_order_id, id) — sem ele, o guard abaixo retornava 409 e o force
	// status nunca chegava ao motoboy COD nativo (BUG id-space 2026-06-24).
	var wpOrderID sql.NullInt64
	err = h.Pool.QueryRow(ctx,
		`SELECT COALESCE(wp_order_id, id) FROM sz_orders WHERE id = $1`, id,
	).Scan(&wpOrderID)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Err(w, 404, "not_found", "pedido não encontrado")
		return
	}
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if !wpOrderID.Valid || wpOrderID.Int64 <= 0 {
		httpx.Err(w, 409, "missing_wc_link",
			"pedido nativo Go sem wp_order_id — sem registro motoboy associado")
		return
	}

	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer tx.Rollback(ctx)

	// Pega de_status atual + pedido_id em uma volta — FOR UPDATE evita corrida.
	var pedidoID int64
	var deStatus sql.NullString
	err = tx.QueryRow(ctx,
		`SELECT id, status FROM sz_motoboy_pedidos
		 WHERE wc_order_id = $1 LIMIT 1 FOR UPDATE`, wpOrderID.Int64,
	).Scan(&pedidoID, &deStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Err(w, 404, "not_found",
			"pedido motoboy não encontrado — use o fluxo de criação antes de forçar status")
		return
	}
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// Regra de transição: embalado só pode ir para em_rota.
	if deStatus.Valid && deStatus.String == "embalado" && (target == "entregue" || target == "frustrado") {
		httpx.Err(w, 422, "transition_blocked",
			"pedido embalado deve ir para em_rota antes de entregue/frustrado")
		return
	}

	if target == "embalado" && validatePackBarcode {
		scanned := normalizePackCode(body.BarcodeBipado)
		if scanned == "" {
			httpx.Err(w, 422, "barcode_required", "bipe o código de barras do produto antes de marcar como embalado")
			return
		}
		expectedCodes, err := h.loadExpectedPackCodes(ctx, id)
		if err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
		if len(expectedCodes) == 0 {
			httpx.Err(w, 422, "barcode_missing", "cadastre o código de barras dos produtos deste pedido antes de marcar como embalado")
			return
		}
		okBarcode := false
		for _, code := range expectedCodes {
			if code == scanned {
				okBarcode = true
				break
			}
		}
		if !okBarcode {
			httpx.Err(w, 422, "barcode_mismatch", "código de barras não confere com o pedido")
			return
		}
	}

	// Comprovante obrigatório para entregue e frustrado.
	if (target == "entregue" || target == "frustrado") && body.ComprovURL == "" {
		httpx.Err(w, 422, "evidence_required",
			"comprovante obrigatório para registrar "+target)
		return
	}

	// Trava GPS — "entregue" exige localização do dispositivo dentro do raio do
	// endereço de entrega. Admin (correção remota via painel) fica isento; o
	// operador logístico em campo precisa estar fisicamente perto do destino.
	if target == "entregue" {
		actor := auth.ActorFromCtx(ctx)
		if actor == nil || actor.Kind != auth.ActorAdmin {
			if body.GpsLat == nil || body.GpsLng == nil {
				httpx.Err(w, 422, "gps_required", "localização GPS obrigatória para confirmar entrega")
				return
			}
			var entLat, entLng sql.NullFloat64
			_ = tx.QueryRow(ctx,
				`SELECT entrega_lat, entrega_lng FROM sz_motoboy_pedidos WHERE id = $1`, pedidoID,
			).Scan(&entLat, &entLng)
			if entLat.Valid && entLng.Valid {
				dist := haversineMeters(entLat.Float64, entLng.Float64, *body.GpsLat, *body.GpsLng)
				if dist > gpsDeliveryRadiusMeters {
					httpx.Err(w, 422, "gps_too_far",
						fmt.Sprintf("GPS a %.0fm do endereço de entrega (máx. %.0fm) — aproxime-se para confirmar", dist, gpsDeliveryRadiusMeters))
					return
				}
			}
		}
	}

	// UPDATE: status + updated_at + (opcional) ts_<estado>.
	tsCol := statusTimestampColumn(target)
	var updateSQL string
	if tsCol != "" {
		updateSQL = `UPDATE sz_motoboy_pedidos
		             SET status = $1,
		                 updated_at = NOW(),
		                 ` + tsCol + ` = COALESCE(` + tsCol + `, NOW())
		             WHERE id = $2`
	} else {
		updateSQL = `UPDATE sz_motoboy_pedidos
		             SET status = $1,
		                 updated_at = NOW()
		             WHERE id = $2`
	}
	if _, err := tx.Exec(ctx, updateSQL, target, pedidoID); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// Uma baixa administrativa precisa fechar também os campos operacionais usados
	// por carteira, conciliação e relatórios. Mantém o motoboy atribuído como o
	// responsável físico pela entrega, enquanto baixa_por registra quem corrigiu o
	// estado no sistema.
	if target == "entregue" || target == "frustrado" {
		if _, err := tx.Exec(ctx, `
			UPDATE sz_motoboy_pedidos
			   SET baixa_at = COALESCE(baixa_at, NOW()),
			       baixa_por = 'admin',
			       baixa_motoboy_id = COALESCE(baixa_motoboy_id, motoboy_id)
			 WHERE id = $1`, pedidoID); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
	}

	if target == "frustrado" && (body.Motivo != "" || body.Observacao != "") {
		_, _ = tx.Exec(ctx, `
			UPDATE sz_motoboy_pedidos
			   SET frustrado_motivo     = COALESCE(NULLIF($2,''), frustrado_motivo),
			       frustrado_observacao = COALESCE(NULLIF($3,''), frustrado_observacao)
			 WHERE id = $1`,
			pedidoID, body.Motivo, body.Observacao)
	}

	// Grava evidence_url (comprovante de entrega/frustração) quando enviada.
	if body.ComprovURL != "" {
		_, _ = tx.Exec(ctx,
			`UPDATE sz_motoboy_pedidos SET evidence_url = $2 WHERE id = $1`,
			pedidoID, body.ComprovURL)
	}

	// BRIDGE motoboy → sz_orders (MESMA transação). Antes, ForceMotoboyStatus
	// atualizava só sz_motoboy_pedidos → sz_orders.status ficava divergente
	// (rastreio "entregue" vs pedido "embalado") e a receita NÃO disparava, pois
	// ela é DERIVADA de sz_orders.status (ex.: audit.go conta WHERE o.status IN
	// ('completo',...)). Espelha bridgeUpdatePedidoStatus() de
	// go/motoboy/internal/handlers/status_bridge.go (mesma lógica, inline porque
	// o admin-service não importa o pacote do motoboy-service).
	//
	// Idempotente (AND o.status <> $2) e fail-closed: erro real de DB → o defer
	// tx.Rollback() reverte TUDO (motoboy + sz_orders), nunca commita divergência.
	// O JOIN usa o.wp_order_id = p.wc_order_id (link canônico). 'agendado' não
	// mapeia → bridge pulado, só o motoboy é atualizado.
	if szStatus, mapped := forceMotoboyStatusToSzOrder(target); mapped {
		if _, err := tx.Exec(ctx, `
			UPDATE sz_orders o
			   SET status = $2, updated_at = NOW()
			  FROM sz_motoboy_pedidos p
			 WHERE p.id = $1
			   AND COALESCE(o.wp_order_id, o.id) = p.wc_order_id
			   AND o.status <> $2`,
			pedidoID, szStatus,
		); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
	}

	// Audit insert (se a tabela existir).
	if h.tableExists(ctx, "sz_motoboy_audit") {
		adm := auth.FromCtx(ctx)
		var actorID *int64
		var adminEmail, adminNome string
		if adm != nil {
			tmp := adm.ID
			actorID = &tmp
			adminEmail = adm.Email
			adminNome = adm.Nome
		}
		meta := map[string]any{
			"source":      "admin_order_detail",
			"admin_email": adminEmail,
			"admin_nome":  adminNome,
			"order_id":    id,
		}
		metaJSON, _ := json.Marshal(meta)
		// meta_json é TEXT em Postgres (schema-motoboy.sql:341) — grava string crua,
		// sem cast ::jsonb (falharia em runtime).
		_, _ = tx.Exec(ctx,
			`INSERT INTO sz_motoboy_audit
			   (pedido_id, motoboy_id, actor_tipo, actor_id,
			    acao, de_status, para_status, meta_json, created_at)
			 VALUES ($1, NULL, 'admin', $2, $3, $4, $5, $6, NOW())`,
			pedidoID, actorID, "force_status",
			nullableString(deStatus), target, string(metaJSON),
		)
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 200, map[string]any{
		"ok":         true,
		"new_status": target,
		"pedido_id":  pedidoID,
	})
}

// UpdateFinancialStatus — altera apenas o status financeiro pós-entrega de Expedição.
// Não toca em sz_orders.status: "entregue/completo" permanece logístico, e
// pagamento_agendado/vencido/concluido vivem no campo separado financial_status.
func (h *OrderDetailHandler) UpdateFinancialStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "order_id inválido")
		return
	}

	var body financialStatusBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	target := strings.TrimSpace(strings.ToLower(body.FinancialStatus))
	if !allowedFinancialStatus[target] {
		httpx.Err(w, 400, "bad_request", "financial_status inválido — aceitos: pagamento_agendado, vencido, concluido")
		return
	}
	if target == "pagamento_agendado" && strings.TrimSpace(body.ScheduledPaymentDate) == "" {
		httpx.Err(w, 422, "scheduled_payment_date_required", "data agendada de pagamento obrigatória")
		return
	}
	if body.ScheduledPaymentDate != "" {
		if _, err := time.Parse("2006-01-02", body.ScheduledPaymentDate); err != nil {
			httpx.Err(w, 400, "bad_request", "scheduled_payment_date deve estar em YYYY-MM-DD")
			return
		}
	}

	if !h.tableExists(ctx, "sz_orders") {
		httpx.Err(w, 503, "tables_missing", "tabela de pedidos ainda não migrada")
		return
	}
	if !h.columnExists(ctx, "sz_orders", "financial_status") ||
		!h.columnExists(ctx, "sz_orders", "scheduled_payment_date") {
		httpx.Err(w, 503, "schema_missing", "campos financeiros pós-entrega de Expedição ainda não migrados")
		return
	}

	var wpOrderID sql.NullInt64
	var produtorID *int64
	var orderStatus sql.NullString
	err = h.Pool.QueryRow(ctx,
		`SELECT COALESCE(wp_order_id, id), produtor_id, status FROM sz_orders WHERE id = $1`, id,
	).Scan(&wpOrderID, &produtorID, &orderStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Err(w, 404, "not_found", "pedido não encontrado")
		return
	}
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	if actor := auth.ActorFromCtx(ctx); actor != nil && actor.Kind != auth.ActorAdmin {
		isOperator := actor.Kind == auth.ActorKind("operator") || actor.Kind == auth.ActorKind("operador")
		isOwnerProducer := actor.Kind == auth.ActorProdutor && produtorID != nil && *produtorID == actor.PortalUserID
		if !isOperator && !isOwnerProducer {
			httpx.Err(w, 403, "forbidden", "acesso restrito ao admin, operador ou produtor dono do pedido")
			return
		}
	}

	if h.tableExists(ctx, "sz_motoboy_pedidos") {
		var isMotoboy bool
		_ = h.Pool.QueryRow(ctx,
			`SELECT EXISTS (
				SELECT 1 FROM sz_motoboy_pedidos
				 WHERE wc_order_id = $1
			)`, wpOrderID.Int64).Scan(&isMotoboy)
		if isMotoboy {
			httpx.Err(w, 422, "cod_not_supported", "status financeiro pós-entrega é exclusivo da Expedição")
			return
		}
	}

	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer tx.Rollback(ctx)

	var oldFinancialStatus sql.NullString
	err = tx.QueryRow(ctx,
		`SELECT financial_status
		   FROM sz_orders
		  WHERE id = $1
		  FOR UPDATE`, id,
	).Scan(&oldFinancialStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Err(w, 404, "not_found", "pedido não encontrado")
		return
	}
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	logisticsStatus := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(orderStatus.String)), "wc-")
	if logisticsStatus != "entregue" && logisticsStatus != "completo" && logisticsStatus != "completed" {
		httpx.Err(w, 422, "not_delivered", "status financeiro só pode ser aplicado após Entregue")
		return
	}

	adm := auth.FromCtx(ctx)
	actor := auth.ActorFromCtx(ctx)
	var actorID *int64
	var adminEmail, adminNome string
	if adm != nil {
		tmp := adm.ID
		actorID = &tmp
		adminEmail = adm.Email
		adminNome = adm.Nome
	} else if actor != nil {
		tmp := actor.PortalUserID
		if tmp == 0 {
			tmp = actor.WPUserID
		}
		if tmp > 0 {
			actorID = &tmp
		}
	}

	var dateArg any
	switch target {
	case "pagamento_agendado":
		dateArg = body.ScheduledPaymentDate
	case "vencido":
		if body.ScheduledPaymentDate != "" {
			dateArg = body.ScheduledPaymentDate
		} else {
			dateArg = nil
		}
	case "concluido":
		dateArg = nil
	}

	if target == "vencido" && dateArg == nil {
		_, err = tx.Exec(ctx, `
			UPDATE sz_orders
			   SET financial_status = $1,
			       scheduled_payment_date = scheduled_payment_date,
			       financial_status_updated_at = NOW(),
			       financial_status_updated_by = $2,
			       updated_at = NOW()
			 WHERE id = $3`, target, actorID, id)
	} else {
		_, err = tx.Exec(ctx, `
			UPDATE sz_orders
			   SET financial_status = $1,
			       scheduled_payment_date = $2::date,
			       financial_status_updated_at = NOW(),
			       financial_status_updated_by = $3,
			       updated_at = NOW()
			 WHERE id = $4`, target, dateArg, actorID, id)
	}
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	if h.tableExists(ctx, "senderzz_portal_audit_log") {
		actorTipo := "admin"
		if adm == nil && actor != nil && actor.Kind != "" {
			actorTipo = string(actor.Kind)
		}
		meta := map[string]any{
			"source":                 "admin_order_detail",
			"admin_email":            adminEmail,
			"admin_nome":             adminNome,
			"actor_kind":             actorTipo,
			"order_id":               id,
			"logistics_status":       logisticsStatus,
			"scheduled_payment_date": body.ScheduledPaymentDate,
			"observacao":             body.Observacao,
		}
		metaJSON, _ := json.Marshal(meta)
		_, _ = tx.Exec(ctx,
			`INSERT INTO senderzz_portal_audit_log
			   (user_id, action, entity_type, entity_id, meta, created_at)
			 VALUES ($1, $2, 'sz_orders', $3, $4, NOW())`,
			actorID, "expedicao_financial_status", id, string(metaJSON),
		)
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 200, map[string]any{
		"ok":                     true,
		"financial_status":       target,
		"scheduled_payment_date": body.ScheduledPaymentDate,
		"order_id":               id,
	})
}

// ChangeMotoboy — troca o motoboy atribuído sem alterar o status do pedido.
// Fluxo usado quando o pedido já está embalado/em rota/a caminho e a atribuição
// precisa ser corrigida sem reabrir o processo.
func (h *OrderDetailHandler) ChangeMotoboy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Gate idêntico ao ForceMotoboyStatus.
	if actor := auth.ActorFromCtx(ctx); actor != nil && actor.Kind != auth.ActorAdmin {
		if actor.Kind != auth.ActorKind("operator") && actor.Kind != auth.ActorKind("operador") {
			httpx.Err(w, 403, "forbidden", "acesso restrito a admin e operador logístico")
			return
		}
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "order_id inválido")
		return
	}

	var body changeMotoboyBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	if body.MotoboyID <= 0 {
		httpx.Err(w, 400, "bad_request", "motoboy_id obrigatório")
		return
	}

	if !h.tableExists(ctx, "sz_orders") || !h.tableExists(ctx, "sz_motoboy_pedidos") {
		httpx.Err(w, 503, "tables_missing", "tabelas de pedidos motoboy ainda não migradas")
		return
	}

	var wpOrderID sql.NullInt64
	err = h.Pool.QueryRow(ctx,
		`SELECT COALESCE(wp_order_id, id) FROM sz_orders WHERE id = $1`, id,
	).Scan(&wpOrderID)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Err(w, 404, "not_found", "pedido não encontrado")
		return
	}
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if !wpOrderID.Valid || wpOrderID.Int64 <= 0 {
		httpx.Err(w, 409, "missing_wc_link",
			"pedido nativo Go sem wp_order_id — sem registro motoboy associado")
		return
	}

	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer tx.Rollback(ctx)

	var pedidoID int64
	var currentStatus sql.NullString
	err = tx.QueryRow(ctx,
		`SELECT id, status
		   FROM sz_motoboy_pedidos
		  WHERE wc_order_id = $1
		  LIMIT 1
		  FOR UPDATE`, wpOrderID.Int64,
	).Scan(&pedidoID, &currentStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Err(w, 404, "not_found", "pedido motoboy não encontrado")
		return
	}
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if !currentStatus.Valid {
		httpx.Err(w, 422, "invalid_status", "pedido sem status de motoboy válido")
		return
	}
	switch currentStatus.String {
	case "embalado", "em_rota", "a_caminho":
	default:
		httpx.Err(w, 422, "invalid_status", "pedido deve estar embalado, em rota ou a caminho para trocar o motoboy")
		return
	}

	var ativo bool
	err = tx.QueryRow(ctx, `SELECT ativo FROM sz_motoboys WHERE id=$1`, body.MotoboyID).Scan(&ativo)
	if err != nil || !ativo {
		httpx.Err(w, 400, "bad_request", "motoboy não encontrado ou inativo")
		return
	}

	if _, err = tx.Exec(ctx, `
		UPDATE sz_motoboy_pedidos
		   SET motoboy_id = $1,
		       updated_at = NOW()
		 WHERE id = $2`,
		body.MotoboyID, pedidoID,
	); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	_, _ = tx.Exec(ctx, `
		INSERT INTO sz_motoboy_audit (pedido_id, acao, de_status, para_status, meta_json, created_at)
		VALUES ($1, 'motoboy_trocado_admin', $2, $2, $3, NOW())`,
		pedidoID, currentStatus.String, body.MotoboyID,
	)

	if err := tx.Commit(ctx); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 200, map[string]any{
		"ok":         true,
		"pedido_id":  pedidoID,
		"motoboy_id": body.MotoboyID,
		"status":     currentStatus.String,
	})
}

func nullableString(s sql.NullString) any {
	if !s.Valid {
		return nil
	}
	return s.String
}

// ── POST /orders/{id}/upload-evidence ────────────────────────────────────────
// Recebe comprovante (foto/PDF) via multipart e retorna a URL pública.
// Campo: evidence_file (jpeg/png/pdf, ≤ 16MB).
func (h *OrderDetailHandler) UploadEvidence(w http.ResponseWriter, r *http.Request) {
	// Gate idêntico ao ForceMotoboyStatus.
	if actor := auth.ActorFromCtx(r.Context()); actor != nil && actor.Kind != auth.ActorAdmin {
		if actor.Kind != auth.ActorKind("operator") && actor.Kind != auth.ActorKind("operador") {
			httpx.Err(w, 403, "forbidden", "acesso restrito a admin e operador logístico")
			return
		}
	}

	idStr := chi.URLParam(r, "id")
	if _, err := strconv.ParseInt(idStr, 10, 64); err != nil {
		httpx.Err(w, 400, "bad_request", "order_id inválido")
		return
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		httpx.Err(w, 400, "bad_request", "multipart inválido: "+err.Error())
		return
	}
	file, header, ferr := r.FormFile("evidence_file")
	if ferr != nil {
		httpx.Err(w, 400, "file_missing", "campo evidence_file obrigatório")
		return
	}
	defer file.Close()

	if header.Size > 16<<20 {
		httpx.Err(w, 413, "file_too_large", "arquivo excede 16MB")
		return
	}

	sniff := make([]byte, 512)
	n, rerr := io.ReadFull(file, sniff)
	if rerr != nil && rerr != io.ErrUnexpectedEOF && rerr != io.EOF {
		httpx.Err(w, 400, "file_invalid", "não foi possível ler o arquivo: "+rerr.Error())
		return
	}
	mime := http.DetectContentType(sniff[:n])
	if _, serr := file.Seek(0, io.SeekStart); serr != nil {
		httpx.Err(w, 500, "upload_error", "falha ao reposicionar o arquivo: "+serr.Error())
		return
	}

	var ext string
	switch mime {
	case "image/jpeg":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	case "application/pdf":
		ext = ".pdf"
	default:
		httpx.Err(w, 400, "file_invalid", "tipo não suportado — envie jpeg, png ou pdf")
		return
	}

	dir := orderEvidenceDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		httpx.Err(w, 500, "upload_error", "falha ao criar diretório: "+err.Error())
		return
	}

	fname := fmt.Sprintf("order-%s-%d%s", idStr, time.Now().UnixMilli(), ext)
	dst, err := os.Create(filepath.Join(dir, fname))
	if err != nil {
		httpx.Err(w, 500, "upload_error", "falha ao criar arquivo: "+err.Error())
		return
	}
	defer dst.Close()
	if _, err := io.Copy(dst, file); err != nil {
		httpx.Err(w, 500, "upload_error", "falha ao gravar arquivo: "+err.Error())
		return
	}

	url := orderEvidenceURLPrefix() + fname
	httpx.JSON(w, 200, map[string]any{"ok": true, "url": url})
}

func orderEvidenceDir() string {
	if v := strings.TrimSpace(os.Getenv("ORDER_EVIDENCE_PATH")); v != "" {
		return v
	}
	return "./uploads/order-evidence"
}

func orderEvidenceURLPrefix() string {
	if v := strings.TrimSpace(os.Getenv("ORDER_EVIDENCE_URL")); v != "" {
		return strings.TrimRight(v, "/") + "/"
	}
	return "/uploads/order-evidence/"
}

// ── POST /orders/{id}/note ────────────────────────────────────────────────────

type noteBody struct {
	Note string `json:"note"`
}

// Note — INSERT em senderzz_order_notes. Se a tabela não existe → 503.
// Não cria a tabela automaticamente (a migration é responsabilidade externa).
func (h *OrderDetailHandler) Note(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "order_id inválido")
		return
	}
	var body noteBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	if len(body.Note) == 0 {
		httpx.Err(w, 400, "bad_request", "campo note obrigatório")
		return
	}
	if len(body.Note) > 5000 {
		httpx.Err(w, 400, "bad_request", "note muito longa (máx 5000 chars)")
		return
	}

	if !h.tableExists(ctx, "senderzz_order_notes") {
		httpx.Err(w, 503, "tables_missing",
			"Notes table not yet migrated — crie senderzz_order_notes(order_id, body, user_id, note_type, created_at)")
		return
	}

	adm := auth.FromCtx(ctx)
	var authorID *int64
	if adm != nil {
		tmp := adm.ID
		authorID = &tmp
	}

	var noteID int64
	err = h.Pool.QueryRow(ctx,
		// Colunas reais da tabela (schema-fixes-v460.sql): body, user_id, note_type.
		// note_type usa o DEFAULT 'internal' (anotação interna do admin).
		`INSERT INTO senderzz_order_notes (order_id, body, user_id, created_at)
		 VALUES ($1, $2, $3, NOW())
		 RETURNING id`,
		id, body.Note, authorID,
	).Scan(&noteID)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 201, map[string]any{
		"ok":      true,
		"note_id": noteID,
	})
}

// ── GET /orders/{id}/notes ────────────────────────────────────────────────────

type orderNote struct {
	ID         int64  `json:"id"`
	Note       string `json:"note"`
	AuthorID   *int64 `json:"author_id"`
	AuthorNome string `json:"author_nome"`
	CreatedAt  string `json:"created_at"`
}

// Notes — lista anotações de um pedido em senderzz_order_notes.
// 503 se a tabela ainda não foi migrada (graceful). 404 se order inexistente.
func (h *OrderDetailHandler) Notes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "order_id inválido")
		return
	}

	if !h.tableExists(ctx, "senderzz_order_notes") {
		httpx.JSON(w, 200, map[string]any{"notes": []struct{}{}, "total": 0, "migrated": false})
		return
	}

	rows, err := h.Pool.Query(ctx,
		`SELECT n.id, COALESCE(n.body,''), n.user_id, COALESCE(u.nome,''),
		        n.created_at::text
		 FROM senderzz_order_notes n
		 LEFT JOIN senderzz_admin_users u ON u.id = n.user_id
		 WHERE n.order_id = $1
		 ORDER BY n.created_at ASC, n.id ASC`, id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	notes := []orderNote{}
	for rows.Next() {
		var n orderNote
		if err := rows.Scan(&n.ID, &n.Note, &n.AuthorID, &n.AuthorNome, &n.CreatedAt); err == nil {
			notes = append(notes, n)
		}
	}

	httpx.JSON(w, 200, map[string]any{"notes": notes, "total": len(notes), "migrated": true})
}

// LabelPDF — GET /orders/{id}/label-pdf (pedido dono 2026-07-28).
//
// Serve o PDF REAL da etiqueta (baixado localmente pelo job de geração) —
// "deve baixar PDF e abrir ele, nada de Melhor Envio" (o link da ME abre a
// página interativa deles com toggles, não o PDF puro). Proxy autenticado pro
// labels-service (rota interna, sem exposição pública). Front chama via
// fetch() (não <a href> puro — precisa do Bearer do admin) e abre o blob.
func (h *OrderDetailHandler) LabelPDF(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "order_id inválido")
		return
	}

	var wpOrderID *int64
	_ = h.Pool.QueryRow(ctx, `SELECT wp_order_id FROM sz_orders WHERE id = $1`, id).Scan(&wpOrderID)
	wcOrderID := id
	if wpOrderID != nil && *wpOrderID > 0 {
		wcOrderID = *wpOrderID
	}

	var labelID int64
	err = h.Pool.QueryRow(ctx,
		`SELECT id FROM wc_me_labels
		  WHERE wc_order_id = $1 AND status <> 'canceled'
		  ORDER BY id DESC LIMIT 1`, wcOrderID,
	).Scan(&labelID)
	if err != nil {
		httpx.Err(w, 404, "not_found", "etiqueta não encontrada pra este pedido")
		return
	}

	base := strings.TrimRight(os.Getenv("LABELS_SERVICE_URL"), "/")
	if base == "" {
		base = "http://labels-service:8084"
	}
	url := fmt.Sprintf("%s/internal/labels/%d/pdf-file", base, labelID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		httpx.Err(w, 500, "internal_error", "erro ao contatar labels-service")
		return
	}
	if secret, ok := internalSecretHeader(); ok {
		req.Header.Set("X-Internal-Secret", secret)
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		httpx.Err(w, 502, "labels_service_error", "falha ao contatar labels-service: "+err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		httpx.Err(w, resp.StatusCode, "labels_service_error", string(body))
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", resp.Header.Get("Content-Disposition"))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, resp.Body)
}

// Approve — POST /orders/{id}/approve (pedido dono 2026-07-28).
//
// Espelho EXATO do go/portal ExpedicaoHandler.Approve (produtor via portal) —
// só a checagem de posse muda: lá exige produtor_id = caller; aqui admin/
// operador pode aprovar QUALQUER produtor (produtorID vem da PRÓPRIA linha do
// pedido, nunca do request). O saldo checado/debitado é sempre o do produtor
// DONO do pedido (não existe carteira "do admin"). Módulos Go distintos (admin
// vs portal), sem import cross-serviço — duplicado de propósito, mesma lógica.
func (h *OrderDetailHandler) Approve(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if actor := auth.ActorFromCtx(ctx); actor != nil && actor.Kind != auth.ActorAdmin {
		if actor.Kind != auth.ActorKind("operator") && actor.Kind != auth.ActorKind("operador") {
			httpx.Err(w, 403, "forbidden", "acesso restrito a admin e operador logístico")
			return
		}
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "order_id inválido")
		return
	}

	var produtorID int64
	var frete float64
	err = h.Pool.QueryRow(ctx,
		`SELECT produtor_id, COALESCE(shipping, 0)::float
		   FROM sz_orders
		  WHERE id = $1
		    AND lower(regexp_replace(status, '^wc-', '')) IN ('pending', 'aguardando', 'on-hold')`,
		id,
	).Scan(&produtorID, &frete)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Err(w, 409, "conflict", "pedido não encontrado ou não está mais pendente")
		return
	}
	if err != nil {
		httpx.Err(w, 500, "db_error", "erro ao aprovar pedido")
		return
	}

	var saldo, reservado float64
	saldoErr := h.Pool.QueryRow(ctx,
		`SELECT saldo::float, saldo_reservado::float FROM tpc_carteira WHERE user_id = $1`,
		produtorID,
	).Scan(&saldo, &reservado)
	if saldoErr != nil && !errors.Is(saldoErr, pgx.ErrNoRows) {
		httpx.Err(w, 500, "db_error", "erro ao verificar saldo do produtor")
		return
	}
	disponivel := saldo - reservado
	if disponivel < 0 {
		disponivel = 0
	}
	if disponivel < frete {
		httpx.Err(w, 402, "insufficient_balance",
			fmt.Sprintf("saldo do produtor insuficiente para aprovar (disponível R$ %.2f, frete R$ %.2f).", disponivel, frete))
		return
	}

	// AUDIT-2026-07-28 (dono, pedido 1660/Cleni): "precisamos evitar que pedido
	// fique como aprovado sem emissão de etiqueta" — antes o status virava
	// 'processing' (Aprovado) ANTES de saber se a etiqueta seria emitida; se a
	// ME recusasse (ex.: "transportadora não atende este trecho"), o pedido
	// ficava mostrando "Aprovado" com etiqueta nenhuma, sem aviso visível. Fix:
	// 'em_andamento' primeiro (saldo já verificado, tentando emitir agora); só
	// vira 'processing' de verdade DEPOIS que a etiqueta é criada com sucesso.
	var newStatus string
	err = h.Pool.QueryRow(ctx,
		`UPDATE sz_orders SET status = 'em_andamento', updated_at = NOW()
		  WHERE id = $1 AND produtor_id = $2
		    AND lower(regexp_replace(status, '^wc-', '')) IN ('pending', 'aguardando', 'on-hold')
		 RETURNING status`,
		id, produtorID,
	).Scan(&newStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Err(w, 409, "conflict", "pedido não encontrado ou não está mais pendente")
		return
	}
	if err != nil {
		httpx.Err(w, 500, "db_error", "erro ao aprovar pedido")
		return
	}

	actorID := int64(0)
	if a := auth.ActorFromCtx(ctx); a != nil && a.PortalUserID > 0 {
		actorID = a.PortalUserID // operador logístico (portal user)
	} else if adm := auth.FromCtx(ctx); adm != nil {
		actorID = adm.ID // admin
	}
	_, _ = h.Pool.Exec(ctx,
		`INSERT INTO sz_order_status_history
		    (order_id, status_de, status_para, motivo, actor_id, actor_tipo, created_at)
		 VALUES ($1, NULL, 'em_andamento', 'aprovado pelo admin/operador logístico (saldo do produtor verificado), emitindo etiqueta', $2, 'admin', NOW())`,
		id, actorID,
	)

	labelID, labelWarning := h.autoEmitLabel(ctx, id, produtorID)

	resp := map[string]any{"ok": true, "status": newStatus}
	if labelID > 0 {
		// Etiqueta criada de verdade — SÓ AGORA vira 'processing' (Aprovado).
		if _, err := h.Pool.Exec(ctx,
			`UPDATE sz_orders SET status = 'processing', updated_at = NOW() WHERE id = $1`, id,
		); err != nil {
			slog.Error("[senderzz_admin] Approve: falha ao promover para processing após etiqueta", "order_id", id, "err", err)
		} else {
			_, _ = h.Pool.Exec(ctx,
				`INSERT INTO sz_order_status_history
				    (order_id, status_de, status_para, motivo, actor_id, actor_tipo, created_at)
				 VALUES ($1, 'em_andamento', 'processing', 'etiqueta emitida com sucesso', $2, 'admin', NOW())`,
				id, actorID,
			)
			_, _ = h.Pool.Exec(ctx, `DELETE FROM sz_order_meta WHERE order_id = $1 AND meta_key = '_sz_label_error'`, id)
			resp["status"] = "processing"
			resp["label_id"] = labelID
		}
	} else {
		// Falhou — fica em 'em_andamento' (NUNCA "Aprovado" fantasma). Motivo
		// real persistido pra aparecer na tela mesmo depois de recarregar.
		_, _ = h.Pool.Exec(ctx,
			`INSERT INTO sz_order_meta (order_id, meta_key, meta_value)
			 VALUES ($1, '_sz_label_error', $2)
			 ON CONFLICT (order_id, meta_key) DO UPDATE SET meta_value = EXCLUDED.meta_value`,
			id, labelWarning,
		)
	}
	if labelWarning != "" {
		resp["label_warning"] = labelWarning
	}
	httpx.JSON(w, 200, resp)
}

// mintProducerJWT emite um JWT interno pro produtor DONO do pedido — mesma
// forma de claim que go/portal auth.EmitJWT (sub/role/iss, mesmo JWT_SECRET
// compartilhado entre serviços), usado só pra autenticar a chamada interna ao
// labels-service. Duplicado de propósito (módulo Go distinto, sem import
// cross-serviço) — ver comentário do Approve acima.
func mintProducerJWT(producerID int64) (string, error) {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return "", fmt.Errorf("JWT_SECRET não configurado")
	}
	now := time.Now()
	claims := jwt.MapClaims{
		"user_id": producerID,
		"role":    "produtor",
		"sub":     strconv.FormatInt(producerID, 10),
		"iat":     now.Unix(),
		"exp":     now.Add(15 * time.Minute).Unix(),
		"iss":     "senderzz-admin",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// autoEmitLabel — espelho de go/portal ExpedicaoHandler.autoEmitLabel. Chama
// POST /labels/emit/{order_id} no labels-service com um JWT interno recém-
// emitido pro PRODUTOR dono do pedido (nunca pro admin — a etiqueta/débito
// tem que ficar atribuída a quem realmente é dono, owner_user_id no
// labels-service). Best-effort: falha aqui não desfaz a aprovação já
// comprometida (status/débito) — fica visível via label_warning, reprocessável
// depois (POST /labels/emit/{id} de novo é idempotente).
func (h *OrderDetailHandler) autoEmitLabel(ctx context.Context, orderID int64, producerID int64) (int64, string) {
	base := strings.TrimRight(os.Getenv("LABELS_SERVICE_URL"), "/")
	if base == "" {
		base = "http://labels-service:8084"
	}

	tok, err := mintProducerJWT(producerID)
	if err != nil {
		return 0, "etiqueta não emitida automaticamente (erro interno) — peça pra ops emitir manualmente"
	}

	url := base + "/wp-json/wc-melhor-envio/v1/labels/emit/" + strconv.FormatInt(orderID, 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(nil))
	if err != nil {
		return 0, "etiqueta não emitida automaticamente (erro interno)"
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 25 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
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
		return 0, "etiqueta não emitida automaticamente: " + msg
	}
	return out.LabelID, ""
}

// CancelExpedicao — POST /orders/{id}/cancel-expedicao (AUDIT-2026-07-28,
// dono: "Cancelar não aparece no nível admin"). Espelha
// go/portal/internal/handlers/expedicao.go::Cancel, mas SEM ownership-gate de
// produtor (admin cancela pedido de qualquer produtor) — mesma janela de
// status, mesmo cancelamento de etiqueta na ME + estorno PENDENTE (nunca
// credita a carteira na hora, só depois que o reembolso cair na ME de fato —
// ver labels-service DeleteLabel/EstornarPendente).
func (h *OrderDetailHandler) CancelExpedicao(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if actor := auth.ActorFromCtx(ctx); actor != nil && actor.Kind != auth.ActorAdmin {
		if actor.Kind != auth.ActorKind("operator") && actor.Kind != auth.ActorKind("operador") {
			httpx.Err(w, 403, "forbidden", "acesso restrito a admin e operador logístico")
			return
		}
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "order_id inválido")
		return
	}

	var produtorID int64
	var wpOrderID sql.NullInt64
	var newStatus string
	err = h.Pool.QueryRow(ctx,
		`UPDATE sz_orders SET status = 'cancelled', updated_at = NOW()
		  WHERE id = $1
		    AND lower(regexp_replace(status, '^wc-', '')) IN ('pending', 'aguardando', 'on-hold', 'em_andamento', 'processing', 'em_separacao', 'embalado')
		 RETURNING produtor_id, wp_order_id, status`,
		id,
	).Scan(&produtorID, &wpOrderID, &newStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Err(w, 409, "conflict", "pedido não encontrado ou não está mais em um status cancelável")
		return
	}
	if err != nil {
		httpx.Err(w, 500, "db_error", "erro ao cancelar pedido")
		return
	}

	actorID := int64(0)
	if a := auth.ActorFromCtx(ctx); a != nil && a.PortalUserID > 0 {
		actorID = a.PortalUserID
	} else if adm := auth.FromCtx(ctx); adm != nil {
		actorID = adm.ID
	}
	_, _ = h.Pool.Exec(ctx,
		`INSERT INTO sz_order_status_history
		    (order_id, status_de, status_para, motivo, actor_id, actor_tipo, created_at)
		 VALUES ($1, NULL, 'cancelled', 'cancelado pelo admin/operador logístico', $2, 'admin', NOW())`,
		id, actorID,
	)

	wcOrderKey := wpOrderID.Int64
	if wcOrderKey == 0 {
		wcOrderKey = id
	}
	var labelID int64
	labelErr := h.Pool.QueryRow(ctx,
		`SELECT id FROM wc_me_labels
		  WHERE wc_order_id = $1 AND status NOT IN ('canceled', 'delivered')
		  ORDER BY id DESC LIMIT 1`,
		wcOrderKey,
	).Scan(&labelID)

	labelWarning := ""
	estornoMsg := ""
	if labelErr == nil && labelID > 0 {
		labelWarning, estornoMsg = h.cancelLabelAsProducer(ctx, labelID, produtorID)
	} else if labelErr != nil && !errors.Is(labelErr, pgx.ErrNoRows) {
		slog.Warn("[senderzz_admin] CancelExpedicao: falha ao consultar etiqueta pro estorno", "order_id", id, "err", labelErr)
	}

	resp := map[string]any{"ok": true, "status": newStatus}
	if labelWarning != "" {
		resp["label_warning"] = labelWarning
	}
	if estornoMsg != "" {
		resp["estorno_mensagem"] = estornoMsg
	}
	httpx.JSON(w, 200, resp)
}

// cancelLabelAsProducer chama DELETE /labels/{id} no labels-service com um JWT
// interno do PRODUTOR dono do pedido (mintProducerJWT) — espelha
// go/portal/internal/handlers/expedicao.go::cancelLabel. Best-effort: erro aqui
// vira aviso, não desfaz o cancelamento do pedido (já comprometido acima).
func (h *OrderDetailHandler) cancelLabelAsProducer(ctx context.Context, labelID int64, producerID int64) (string, string) {
	base := strings.TrimRight(os.Getenv("LABELS_SERVICE_URL"), "/")
	if base == "" {
		base = "http://labels-service:8084"
	}

	tok, err := mintProducerJWT(producerID)
	if err != nil {
		return "pedido cancelado, mas a etiqueta não pôde ser cancelada automaticamente (erro interno) — peça pra ops cancelar manualmente", ""
	}

	url := base + "/wp-json/wc-melhor-envio/v1/labels/" + strconv.FormatInt(labelID, 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return "pedido cancelado, mas a etiqueta não pôde ser cancelada automaticamente (erro interno)", ""
	}
	req.Header.Set("Authorization", "Bearer "+tok)

	client := &http.Client{Timeout: 25 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "pedido cancelado, mas a etiqueta não pôde ser cancelada automaticamente (labels-service indisponível) — peça pra ops cancelar manualmente", ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))

	if resp.StatusCode != http.StatusOK {
		slog.Error("[senderzz_admin] cancelLabelAsProducer: labels-service recusou", "label_id", labelID, "status", resp.StatusCode, "body", string(body))
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

// EmitLabelRetry — POST /orders/{id}/emit-label (AUDIT-2026-07-28, dono:
// pedido 1660 aprovado sem NENHUMA etiqueta na ME e sem sz_order_status_history
// — ficou "processing" por fora do fluxo normal de Aprovar, então autoEmitLabel
// nunca rodou). Reprocessa a emissão pra pedido já aprovado sem etiqueta —
// idempotente (POST /labels/emit/{id} no labels-service já é idempotente por
// design: se já existe etiqueta pro par wc_order_id+service_id, devolve a
// existente em vez de duplicar).
func (h *OrderDetailHandler) EmitLabelRetry(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if actor := auth.ActorFromCtx(ctx); actor != nil && actor.Kind != auth.ActorAdmin {
		if actor.Kind != auth.ActorKind("operator") && actor.Kind != auth.ActorKind("operador") {
			httpx.Err(w, 403, "forbidden", "acesso restrito a admin e operador logístico")
			return
		}
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "order_id inválido")
		return
	}

	var produtorID int64
	var status string
	err = h.Pool.QueryRow(ctx,
		`SELECT produtor_id, lower(regexp_replace(status, '^wc-', ''))
		   FROM sz_orders WHERE id = $1`,
		id,
	).Scan(&produtorID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Err(w, 404, "not_found", "pedido não encontrado")
		return
	}
	if err != nil {
		httpx.Err(w, 500, "db_error", "erro ao buscar pedido")
		return
	}
	// Emissão manual só pode partir de pendente, aprovado ou separado. O estado
	// técnico em_andamento é reservado ao fluxo interno do botão Aprovar.
	if !preEmitStatuses[status] {
		httpx.Err(w, 409, "conflict", "pedido precisa estar pendente, aprovado ou separado pra emitir etiqueta")
		return
	}

	resp := h.emitAndPromote(ctx, id, produtorID, status)
	httpx.JSON(w, 200, resp)
}

// preEmitStatuses — status a partir dos quais é permitido emitir etiqueta
// manualmente (EmitLabelRetry/ForceCarrierEmit): pendente, aprovado, separado
// ou alerta operacional sem etiqueta.
var preEmitStatuses = map[string]bool{
	"pending": true, "aguardando": true, "on-hold": true,
	"processing": true, "em_separacao": true, "embalado": true,
	"cancelled": true, "cancelado": true, "frustrado": true,
}

// statusesPreProcessing — de onde emitAndPromote promove pra 'processing'
// após sucesso (qualquer status ainda não aprovado de fato).
var statusesPreProcessing = map[string]bool{
	"pending": true, "aguardando": true, "on-hold": true, "em_andamento": true,
	"cancelled": true, "cancelado": true, "frustrado": true,
}

// emitAndPromote chama autoEmitLabel e, em caso de sucesso, promove o pedido
// pra 'processing' (Aprovado de verdade) — mesma lógica de Approve/
// EmitLabelRetry, extraída pra ser reutilizada por ForceCarrierEmit —
// AUDIT-2026-07-29. Em falha, persiste o motivo em sz_order_meta._sz_label_error.
func (h *OrderDetailHandler) emitAndPromote(ctx context.Context, id int64, produtorID int64, status string) map[string]any {
	labelID, labelWarning := h.autoEmitLabel(ctx, id, produtorID)
	resp := map[string]any{"ok": true}
	if labelID > 0 {
		resp["label_id"] = labelID
		_, _ = h.Pool.Exec(ctx, `DELETE FROM sz_order_meta WHERE order_id = $1 AND meta_key = '_sz_label_error'`, id)
		if statusesPreProcessing[status] {
			if _, err := h.Pool.Exec(ctx,
				`UPDATE sz_orders SET status = 'processing', updated_at = NOW() WHERE id = $1`, id,
			); err == nil {
				_, _ = h.Pool.Exec(ctx,
					`INSERT INTO sz_order_status_history
					    (order_id, status_de, status_para, motivo, actor_id, actor_tipo, created_at)
					 VALUES ($1, $2, 'processing', 'etiqueta emitida com sucesso (reprocessado manualmente)', 0, 'admin', NOW())`,
					id, status,
				)
				resp["status"] = "processing"
			}
		}
	} else if labelWarning != "" {
		_, _ = h.Pool.Exec(ctx,
			`INSERT INTO sz_order_meta (order_id, meta_key, meta_value)
			 VALUES ($1, '_sz_label_error', $2)
			 ON CONFLICT (order_id, meta_key) DO UPDATE SET meta_value = EXCLUDED.meta_value`,
			id, labelWarning,
		)
	}
	if labelWarning != "" {
		resp["label_warning"] = labelWarning
	}
	return resp
}

// ForceCarrierEmit — POST /orders/{id}/force-carrier {"service_id": N}
// (AUDIT-2026-07-29, dono: "botão pra dar andamento em alguma das cotações,
// pro admin, em caso do pedido travar em Em Andamento"). Grava o service_id
// escolhido em sz_order_meta._sz_freight_id — PRIORIDADE MÁXIMA que
// assembleLabelData (emit.go) já respeita (mesmo campo que o checkout grava)
// — e reprocessa a emissão com esse serviço específico, ignorando a escolha
// automática (mais barata disponível).
func (h *OrderDetailHandler) ForceCarrierEmit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if actor := auth.ActorFromCtx(ctx); actor != nil && actor.Kind != auth.ActorAdmin {
		if actor.Kind != auth.ActorKind("operator") && actor.Kind != auth.ActorKind("operador") {
			httpx.Err(w, 403, "forbidden", "acesso restrito a admin e operador logístico")
			return
		}
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "order_id inválido")
		return
	}
	var body struct {
		ServiceID int `json:"service_id"`
	}
	if jerr := json.NewDecoder(r.Body).Decode(&body); jerr != nil || body.ServiceID <= 0 {
		httpx.Err(w, 400, "bad_request", "service_id inválido")
		return
	}

	var produtorID int64
	var status string
	err = h.Pool.QueryRow(ctx,
		`SELECT produtor_id, lower(regexp_replace(status, '^wc-', ''))
		   FROM sz_orders WHERE id = $1`,
		id,
	).Scan(&produtorID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Err(w, 404, "not_found", "pedido não encontrado")
		return
	}
	if err != nil {
		httpx.Err(w, 500, "db_error", "erro ao buscar pedido")
		return
	}
	// AUDIT-2026-07-29 (dono: "pedido precisa estar aprovado ou separado pra
	// emitir etiqueta - ajusta isso") — pending/aguardando/on-hold também
	// podem emitir direto (sem passar pelo botão Aprovar antes): a checagem de
	// saldo real continua acontecendo dentro de autoEmitLabel/EmitOrderLabel
	// (wallet.Reservar recusa se não tiver saldo), então não abre brecha
	// financeira — só remove a exigência de já estar em 'processing' antes.
	if !preEmitStatuses[status] {
		httpx.Err(w, 409, "conflict", "pedido precisa estar pendente, aprovado ou separado pra emitir etiqueta")
		return
	}
	if h.tableExists(ctx, "wc_me_labels") {
		var hasLabel bool
		_ = h.Pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM wc_me_labels l
			  JOIN sz_orders o ON o.id = $1
			  WHERE l.wc_order_id = COALESCE(o.wp_order_id, o.id) AND l.status NOT IN ('canceled','delivered'))`,
			id,
		).Scan(&hasLabel)
		if hasLabel {
			httpx.Err(w, 409, "conflict", "pedido já tem etiqueta emitida — cancele antes de forçar outra transportadora")
			return
		}
	}

	_, err = h.Pool.Exec(ctx,
		`INSERT INTO sz_order_meta (order_id, meta_key, meta_value)
		 VALUES ($1, '_sz_freight_id', $2)
		 ON CONFLICT (order_id, meta_key) DO UPDATE SET meta_value = EXCLUDED.meta_value`,
		id, strconv.Itoa(body.ServiceID),
	)
	if err != nil {
		httpx.Err(w, 500, "db_error", "erro ao salvar transportadora escolhida")
		return
	}

	resp := h.emitAndPromote(ctx, id, produtorID, status)
	httpx.JSON(w, 200, resp)
}

// EditOrderFields — PATCH /orders/{id}/fields (AUDIT-2026-07-29, dono: "tb
// possibilidade de editar campos do pedido, somente antes de gerar etiqueta").
// Edita nome/CPF/telefone/endereço de destino — SÓ permitido enquanto não
// existe etiqueta emitida (mudar CEP depois da etiqueta gerada não afeta o
// envio real, cria divergência entre o que a ME tem e o que o painel mostra).
func (h *OrderDetailHandler) EditOrderFields(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if actor := auth.ActorFromCtx(ctx); actor != nil && actor.Kind != auth.ActorAdmin {
		if actor.Kind != auth.ActorKind("operator") && actor.Kind != auth.ActorKind("operador") {
			httpx.Err(w, 403, "forbidden", "acesso restrito a admin e operador logístico")
			return
		}
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "order_id inválido")
		return
	}

	var body struct {
		ClienteNome     *string `json:"cliente_nome"`
		ClienteTelefone *string `json:"cliente_telefone"`
		ClienteCPF      *string `json:"cliente_cpf"`
		CEP             *string `json:"cep"`
		Logradouro      *string `json:"logradouro"`
		Numero          *string `json:"numero"`
		Complemento     *string `json:"complemento"`
		Bairro          *string `json:"bairro"`
		Cidade          *string `json:"cidade"`
		UF              *string `json:"uf"`
	}
	if jerr := json.NewDecoder(r.Body).Decode(&body); jerr != nil {
		httpx.Err(w, 400, "bad_request", "corpo inválido")
		return
	}

	if h.tableExists(ctx, "wc_me_labels") {
		var hasLabel bool
		_ = h.Pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM wc_me_labels l
			  JOIN sz_orders o ON o.id = $1
			  WHERE l.wc_order_id = COALESCE(o.wp_order_id, o.id) AND l.status NOT IN ('canceled','delivered'))`,
			id,
		).Scan(&hasLabel)
		if hasLabel {
			httpx.Err(w, 409, "conflict", "pedido já tem etiqueta emitida — edição bloqueada")
			return
		}
	}

	var orderExists bool
	if err := h.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sz_orders WHERE id = $1)`, id).Scan(&orderExists); err != nil || !orderExists {
		httpx.Err(w, 404, "not_found", "pedido não encontrado")
		return
	}

	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.Err(w, 500, "db_error", "erro ao iniciar transação")
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if body.ClienteCPF != nil {
		digits := onlyDigitsOrderDetail(*body.ClienteCPF)
		_, _ = tx.Exec(ctx,
			`INSERT INTO sz_order_meta (order_id, meta_key, meta_value) VALUES ($1, '_billing_cpf', $2)
			 ON CONFLICT (order_id, meta_key) DO UPDATE SET meta_value = EXCLUDED.meta_value`,
			id, digits,
		)
	}

	hasAddrEdit := body.ClienteNome != nil || body.ClienteTelefone != nil || body.CEP != nil ||
		body.Logradouro != nil || body.Numero != nil || body.Complemento != nil ||
		body.Bairro != nil || body.Cidade != nil || body.UF != nil
	if hasAddrEdit {
		var addrExists bool
		_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sz_order_addresses WHERE order_id=$1 AND tipo='shipping')`, id).Scan(&addrExists)
		if !addrExists {
			_, err = tx.Exec(ctx,
				`INSERT INTO sz_order_addresses (order_id, tipo, nome, telefone, cep, logradouro, numero, complemento, bairro, cidade, uf)
				 VALUES ($1, 'shipping', '', '', '', '', '', '', '', '', '')`,
				id,
			)
			if err != nil {
				httpx.Err(w, 500, "db_error", "erro ao criar endereço de destino")
				return
			}
		}
		set := []string{}
		args := []any{id}
		add := func(col string, v *string) {
			if v == nil {
				return
			}
			args = append(args, *v)
			set = append(set, fmt.Sprintf("%s = $%d", col, len(args)))
		}
		add("nome", body.ClienteNome)
		add("telefone", body.ClienteTelefone)
		add("cep", body.CEP)
		add("logradouro", body.Logradouro)
		add("numero", body.Numero)
		add("complemento", body.Complemento)
		add("bairro", body.Bairro)
		add("cidade", body.Cidade)
		add("uf", body.UF)
		if len(set) > 0 {
			q := "UPDATE sz_order_addresses SET " + strings.Join(set, ", ") + " WHERE order_id = $1 AND tipo = 'shipping'"
			if _, err := tx.Exec(ctx, q, args...); err != nil {
				httpx.Err(w, 500, "db_error", "erro ao atualizar endereço")
				return
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.Err(w, 500, "db_error", "erro ao confirmar edição")
		return
	}
	httpx.JSON(w, 200, map[string]any{"ok": true})
}

// onlyDigitsOrderDetail mantém só dígitos (CPF sem máscara antes de gravar).
func onlyDigitsOrderDetail(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// MarkPacked — POST /orders/{id}/mark-packed (pedido dono 2026-07-28).
//
// Transição sz_orders.status → 'embalado' pra pedidos EXPEDIÇÃO (Melhor Envio) —
// motoboy/COD já tem seu próprio fluxo de separação via
// /bulk-actions/motoboy-generate-labels + ForceMotoboyStatus, não passa por aqui.
// Disparado pelo botão "Imprimir" no drawer do pedido (admin/operador): imprimir
// a etiqueta já separa o pedido automaticamente, sem passo manual extra.
//
// 'embalado' já é um valor válido do CHECK de sz_orders.status (compartilhado
// com o vocabulário motoboy — ver tracking.go: "embalado"/"separado" mapeiam pro
// mesmo estágio da timeline) — não precisou de migration nova.
//
// Idempotente: chamar de novo com o pedido já 'embalado' (ou além, ex. 'enviado')
// não regride o status — só avança de estados PRÉ-separação.
func (h *OrderDetailHandler) MarkPacked(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if actor := auth.ActorFromCtx(ctx); actor != nil && actor.Kind != auth.ActorAdmin {
		if actor.Kind != auth.ActorKind("operator") && actor.Kind != auth.ActorKind("operador") {
			httpx.Err(w, 403, "forbidden", "acesso restrito a admin e operador logístico")
			return
		}
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "order_id inválido")
		return
	}

	ct, err := h.Pool.Exec(ctx,
		`UPDATE sz_orders
		    SET status = 'embalado', updated_at = NOW()
		  WHERE id = $1
		    AND status IN ('pending','processing','aguardando','on-hold')`,
		id,
	)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if ct.RowsAffected() == 0 {
		// Não é erro — idempotente. Pode já estar 'embalado' ou além (enviado/
		// entregue), ou o id não existir. Devolve o status atual pro front decidir.
		var cur string
		_ = h.Pool.QueryRow(ctx, `SELECT status FROM sz_orders WHERE id = $1`, id).Scan(&cur)
		httpx.JSON(w, 200, map[string]any{"ok": true, "status": cur, "changed": false})
		return
	}
	httpx.JSON(w, 200, map[string]any{"ok": true, "status": "embalado", "changed": true})
}

// MarkCollected — AUDIT-2026-07-31 (dono): marca 'coletado' quando o operador
// logístico ou admin coloca o pacote no ponto de coleta (ação manual). Mesmo
// gate de MarkPacked (admin + operador logístico). 'enviado' sempre sobrepõe
// 'coletado' — ver label_jobs.go ProcessSyncTracking (mirror de 'posted' aceita
// origem embalado OU coletado, então a confirmação real da ME sempre avança).
func (h *OrderDetailHandler) MarkCollected(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if actor := auth.ActorFromCtx(ctx); actor != nil && actor.Kind != auth.ActorAdmin {
		if actor.Kind != auth.ActorKind("operator") && actor.Kind != auth.ActorKind("operador") {
			httpx.Err(w, 403, "forbidden", "acesso restrito a admin e operador logístico")
			return
		}
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "order_id inválido")
		return
	}

	ct, err := h.Pool.Exec(ctx,
		`UPDATE sz_orders
		    SET status = 'coletado', updated_at = NOW()
		  WHERE id = $1
		    AND status = 'embalado'`,
		id,
	)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if ct.RowsAffected() == 0 {
		var cur string
		_ = h.Pool.QueryRow(ctx, `SELECT status FROM sz_orders WHERE id = $1`, id).Scan(&cur)
		httpx.JSON(w, 200, map[string]any{"ok": true, "status": cur, "changed": false})
		return
	}
	_, _ = h.Pool.Exec(ctx,
		`INSERT INTO sz_order_status_history (order_id, status_de, status_para, actor_tipo)
		 VALUES ($1, 'embalado', 'coletado', 'admin')`,
		id,
	)
	httpx.JSON(w, 200, map[string]any{"ok": true, "status": "coletado", "changed": true})
}
