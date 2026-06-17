// Package handlers — endpoint admin para tela OrderDetail consolidada.
//
// Substitui 3 metaboxes do plugin WordPress:
//   - includes/motoboy/order-metabox.php          (Operação Motoboy COD)
//   - includes/senderzz-affiliates.php:5636       (Resumo afiliado)
//   - src/Admin/Orders/Metabox.php                (Etiqueta ME)
//
// Endpoints (ver main.go para wiring):
//   GET  /orders/{id}                       → payload consolidado
//   POST /orders/{id}/force-motoboy-status  → muda status manual (admin)
//   POST /orders/{id}/note                  → anota observação interna
//
// Graceful degradation via tableExists: cada seção retorna {exists:false}
// quando a tabela ou linha de origem não existe — nunca derruba a request.
//
// Convenção sz_orders.id (PK) ↔ sz_motoboy_pedidos.wc_order_id ↔
//                       sz_orders.wp_order_id (ID original do WooCommerce).
// O parâmetro {id} da URL é sz_orders.id; o wp_order_id é resolvido a partir
// dele e usado nos JOINs com tabelas keyed pelo ID do WC.
package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/auth"
	"github.com/senderzz/admin-service/internal/httpx"
)

type OrderDetailHandler struct{ Pool *pgxpool.Pool }

func (h *OrderDetailHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
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

type orderHead struct {
	ID              int64            `json:"id"`
	WCOrderID       *int64           `json:"wc_order_id"`
	OrderNumber     string           `json:"order_number"`
	Status          string           `json:"status"`
	CreatedAt       string           `json:"created_at"`
	Customer        orderCustomer    `json:"customer"`
	Address         orderAddress     `json:"address"`
	Items           []orderItem      `json:"items"`
	ItemsFull       []orderItemFull  `json:"items_full"`
	EnderecoEnvio   orderFullAddress `json:"endereco_envio"`
	EnderecoCobranca orderFullAddress `json:"endereco_cobranca"`
	Produtor        orderProducer    `json:"produtor"`
	Taxas           orderFees        `json:"taxas"`
	Totals          orderTotals      `json:"totals"`
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
	Exists              bool           `json:"exists"`
	MotoboyID           *int64         `json:"motoboy_id"`
	MotoboyNome         string         `json:"motoboy_nome"`
	MotoboyTelefone     string         `json:"motoboy_telefone"`
	MotoboyPlaca        string         `json:"motoboy_placa"`
	CDNome              string         `json:"cd_nome"`
	ZonaNome            string         `json:"zona_nome"`
	Status              string         `json:"status"`
	DestProduto         string         `json:"dest_produto"`
	ValorPedido         float64        `json:"valor_pedido"`
	PgtoDinheiro        float64        `json:"pgto_dinheiro"`
	PgtoPix             float64        `json:"pgto_pix"`
	PgtoCartao          float64        `json:"pgto_cartao"`
	RecebedorNome       string         `json:"recebedor_nome"`
	RecebedorTipo       string         `json:"recebedor_tipo"`
	RecebedorCPF        string         `json:"recebedor_cpf"`
	BaixaPor            string         `json:"baixa_por"`
	BaixaAdminUserID    *int64         `json:"baixa_admin_user_id"`
	BaixaMotoboyID      *int64         `json:"baixa_motoboy_id"`
	BaixaAt             *string        `json:"baixa_at"`
	EntregaLat          *float64       `json:"entrega_lat"`
	EntregaLng          *float64       `json:"entrega_lng"`
	FrustradoMotivo     string         `json:"frustrado_motivo"`
	FrustradoObservacao string         `json:"frustrado_observacao"`
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
	Exists           bool         `json:"exists"`
	// LabelID: wc_me_labels.id — usado pelo endpoint /labels/{id}/cancel.
	LabelID          *int64       `json:"label_id"`
	InvoiceID        *string      `json:"invoice_id"`
	CustomServiceID  *string      `json:"custom_service_id"`
	ItemID           *string      `json:"item_id"`
	MEOrderID        *string      `json:"me_order_id"`
	PrintURL         *string      `json:"print_url"`
	PDFLocalURL      *string      `json:"pdf_local_url"`
	Status           string       `json:"status"`
	Error            *string      `json:"error"`
	GeneratedAt      *string      `json:"generated_at"`
	BoughtShipping   *string      `json:"bought_shipping"`
	ServiceName      *string      `json:"service_name"`
	TrackingCode     *string      `json:"tracking_code"`
	Reverse          labelReverse `json:"reverse"`
}

type orderDetailPayload struct {
	Order     orderHead        `json:"order"`
	Motoboy   motoboySection   `json:"motoboy"`
	Affiliate affiliateSection `json:"affiliate"`
	Label     labelSection     `json:"label"`
	Marketing orderMarketing   `json:"marketing"`
	Fiscal    orderFiscal      `json:"fiscal"`
	Tracking  orderTracking    `json:"tracking"`
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

	// 3) Seções secundárias — todas opcionais.
	// Para JOINs com tabelas que ainda chaveiam por WC order id (sz_motoboy_pedidos
	// e wc_me_labels), usamos wp_order_id quando disponível; senão, caem para 0
	// e os exists ficam false.
	wcOrderID := int64(0)
	if wpOrderID != nil {
		wcOrderID = *wpOrderID
	}

	mot := h.loadMotoboySection(ctx, wcOrderID)
	aff := h.loadAffiliateSection(ctx, id)
	lab := h.loadLabelSection(ctx, wcOrderID)

	// 4) Blocos sz_order_meta — marketing, fiscal, tracking. Tudo opcional.
	mk := h.loadMarketingSection(ctx, id)
	fs := h.loadFiscalSection(ctx, id)
	// Tracking: meta wins, label-fallback. Recebe TrackingCode da label como fallback.
	var labelTC string
	if lab.TrackingCode != nil {
		labelTC = *lab.TrackingCode
	}
	tr := h.loadTrackingSection(ctx, id, labelTC)

	httpx.JSON(w, 200, orderDetailPayload{
		Order:     head,
		Motoboy:   mot,
		Affiliate: aff,
		Label:     lab,
		Marketing: mk,
		Fiscal:    fs,
		Tracking:  tr,
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
func (h *OrderDetailHandler) loadMarketingSection(ctx context.Context, orderID int64) orderMarketing {
	keys := []string{
		"_utm_source", "_utm_medium", "_utm_campaign",
		"_utm_term", "_utm_content", "_referrer", "_landing_page",
	}
	m := h.loadOrderMetaMap(ctx, orderID, keys)
	return orderMarketing{
		UTMSource:   m["_utm_source"],
		UTMMedium:   m["_utm_medium"],
		UTMCampaign: m["_utm_campaign"],
		UTMTerm:     m["_utm_term"],
		UTMContent:  m["_utm_content"],
		Referrer:    m["_referrer"],
		Landing:     m["_landing_page"],
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

// loadTrackingSection — código, URL e transportadora do rastreio consolidado.
// Regra de precedência: sz_order_meta vence wc_me_labels (permite override manual
// no portal V2 sem regerar etiqueta). labelTC é fallback recebido do caller.
func (h *OrderDetailHandler) loadTrackingSection(ctx context.Context, orderID int64, labelTC string) orderTracking {
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
	var status, createdAt, orderNumber sql.NullString

	err := h.Pool.QueryRow(ctx,
		`SELECT id, wp_order_id, COALESCE(order_number,''),
		        produtor_id, status, created_at::text,
		        COALESCE(subtotal,0), COALESCE(shipping,0), COALESCE(total,0),
		        COALESCE(affiliate_amount,0), COALESCE(senderzz_fee,0), COALESCE(producer_net,0)
		 FROM sz_orders WHERE id = $1`, id,
	).Scan(&head.ID, &wpOrderID, &orderNumber, &produtorID, &status, &createdAt,
		&sub, &ship, &total, &affAmt, &fee, &net)
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
	return addr
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
	if hasAffTx {
		_ = h.Pool.QueryRow(ctx,
			`SELECT affiliate_id, COALESCE(amount,0), COALESCE(status,'')
			 FROM senderzz_affiliate_transactions
			 WHERE order_id = $1 AND type = 'commission'
			 ORDER BY id ASC LIMIT 1`, orderID,
		).Scan(&affID, &amount, &status)
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
		_ = h.Pool.QueryRow(ctx,
			`SELECT produtor_id, afiliado_id, COALESCE(comissao_pct,0)
			 FROM senderzz_affiliates
			 WHERE id = $1 LIMIT 1`, affID.Int64,
		).Scan(&prodID, &afiliadoWP, &pct)
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
//   me_order_id      ← me_shipment_id
//   item_id          ← me_label_id
//   print_url        ← label_url
//   pdf_local_url    ← label_pdf_path
//   invoice_id, custom_service_id, bought_shipping, reverse → null (sem coluna nativa)
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
	)
	err := h.Pool.QueryRow(ctx,
		`SELECT id, me_shipment_id, me_label_id, COALESCE(status,''),
		        service_name, tracking_code, label_url, label_pdf_path,
		        created_at::text
		 FROM wc_me_labels
		 WHERE wc_order_id = $1
		 ORDER BY id DESC LIMIT 1`, wcOrderID,
	).Scan(&labelDBID, &meShipment, &meLabel, &status, &serviceName, &trackingCode,
		&labelURL, &labelPDF, &createdAt)
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
	// invoice_id / custom_service_id / bought_shipping / error / reverse não existem
	// no schema Postgres — eram postmeta no WP. Ficam null intencionalmente.
	return out
}

// ── POST /orders/{id}/force-motoboy-status ────────────────────────────────────

type forceStatusBody struct {
	TargetStatus string `json:"target_status"`
}

// allowedForceStatus — em_rota é exclusividade do motoboy via PWA (QR Code).
var allowedForceStatus = map[string]bool{
	"agendado":  true,
	"embalado":  true,
	"entregue":  true,
	"frustrado": true,
	"cancelado": true,
}

// statusTimestampColumn — coluna ts_* a atualizar em sz_motoboy_pedidos por status.
// agendado e cancelado não têm coluna dedicada — só batem updated_at.
func statusTimestampColumn(status string) string {
	switch status {
	case "embalado":
		return "ts_embalado"
	case "entregue":
		return "ts_entregue"
	case "frustrado":
		return "ts_frustrado"
	}
	return ""
}

// ForceMotoboyStatus — admin força transição manual. Em rota bloqueado.
// Wrappa UPDATE + audit em transação. actor_tipo='admin', actor_id do JWT.
func (h *OrderDetailHandler) ForceMotoboyStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
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
	if target == "em_rota" {
		httpx.Err(w, 400, "forbidden_status", "em_rota é exclusivo do motoboy via QR Code no PWA")
		return
	}
	if !allowedForceStatus[target] {
		httpx.Err(w, 400, "bad_request",
			"target_status inválido — aceitos: agendado, embalado, entregue, frustrado, cancelado")
		return
	}

	if !h.tableExists(ctx, "sz_orders") || !h.tableExists(ctx, "sz_motoboy_pedidos") {
		httpx.Err(w, 503, "tables_missing", "tabelas de pedidos motoboy ainda não migradas")
		return
	}

	// Resolve wp_order_id (chave de sz_motoboy_pedidos.wc_order_id).
	var wpOrderID sql.NullInt64
	err = h.Pool.QueryRow(ctx,
		`SELECT wp_order_id FROM sz_orders WHERE id = $1`, id,
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

func nullableString(s sql.NullString) any {
	if !s.Valid {
		return nil
	}
	return s.String
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
			"Notes table not yet migrated — crie senderzz_order_notes(order_id, note, author_id, created_at)")
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
		`INSERT INTO senderzz_order_notes (order_id, note, author_id, created_at)
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
	ID         int64   `json:"id"`
	Note       string  `json:"note"`
	AuthorID   *int64  `json:"author_id"`
	AuthorNome string  `json:"author_nome"`
	CreatedAt  string  `json:"created_at"`
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
		`SELECT n.id, COALESCE(n.note,''), n.author_id, COALESCE(u.nome,''),
		        n.created_at::text
		 FROM senderzz_order_notes n
		 LEFT JOIN senderzz_admin_users u ON u.id = n.author_id
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
