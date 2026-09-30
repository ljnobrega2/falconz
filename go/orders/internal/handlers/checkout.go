// checkout.go — API de checkout nativo (COD) sem JWT.
//
// Namespace HTTP: /checkout-api (registrado na RAIZ do router, fora do grupo JWT
// e fora de /wp-json/senderzz/v1 — são endpoints PÚBLICOS chamados pela página de
// checkout do produtor/afiliado).
//
// Rotas implementadas:
//
//	GET  /checkout-api/offer?token=<t>            — resolve a oferta pública do link de checkout
//	GET  /checkout-api/schedule?token=<t>&cep=<c> — datas de entrega ofertáveis (motor em schedule.go)
//	POST /checkout-api/order                      — cria pedido COD a partir do link (preço SERVER-SIDE)
//	                                                + agendamento (data_entrega/tipo recomputados server-side)
//
// Segurança (corrige o HIGH de checkout):
//   - O preço é SEMPRE lido de senderzz_checkout_links.display_value no servidor.
//     Qualquer valor enviado pelo cliente é IGNORADO (equivalente a CRIT-01 do PHP).
//   - O endpoint /offer expõe apenas os campos públicos da oferta — nunca url,
//     post_id, price_label ou producer interno sensível.
//   - Idempotência: dedupe por (token + cpf) em janela curta (10 min) usando
//     sz_order_meta(checkout_idem) + pg_advisory_xact_lock para matar o double-click.
//
// Decisão de schema (auditoria 2026-06-18): o status inicial em sz_orders é 'pending'.
//
//	O CHECK de sz_orders.status NÃO aceita 'agendado' (esse é um status de
//	sz_motoboy_pedidos, etapa downstream fora do escopo desta task). Pedidos COD
//	existentes na base nascem 'pending' / payment_status 'pending' / payment_method 'cod'.
//
// Valores monetários: shopspring/decimal para evitar drift de float64.
package handlers

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log/slog"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/senderzz/orders-service/internal/freight"
	"github.com/senderzz/orders-service/internal/httpx"
)

// CheckoutHandler agrupa as dependências dos endpoints públicos de checkout.
type CheckoutHandler struct {
	db               *pgxpool.Pool
	labelsServiceURL string
}

// NewCheckoutHandler cria um CheckoutHandler com o pool fornecido.
func NewCheckoutHandler(db *pgxpool.Pool) *CheckoutHandler {
	// AUDIT-2026-07-30 #6 (dono: "última atualização direto do ME"): consulta
	// ao vivo (best-effort) no rastreio público — ver InternalMEStatusLive em
	// go/labels/internal/handlers/me_status_sync.go.
	return &CheckoutHandler{db: db, labelsServiceURL: strings.TrimSpace(os.Getenv("LABELS_SERVICE_URL"))}
}

// ── Constantes financeiras da comissão de afiliado (FEAT-AFF-COMMISSION) ──────
//
// A resolução da comissão acontece SÓ na criação do pedido com afiliado e é
// INSERT-only (nunca atualiza pedido existente). As constantes abaixo são LOCAIS
// a este pacote DE PROPÓSITO: o módulo go/orders não importa go/shared (tem seu
// próprio internal/httpx) — adicionar require+replace só por uma constante seria
// um trade ruim num caminho financeiro. A constante de taxa também existe, como
// doc-only, em go/portal/internal/handlers/fees.go (taxaTransacaoAfiliadoPct) —
// mantê-las separadas evita acoplar dois módulos pela DRY. Mantenha as duas em
// sincronia se a taxa mudar.
//
// taxaTransacaoAfiliadoStr: FALLBACK da taxa de transação do afiliado (4,99%)
// quando a option não está persistida, construído a partir de STRING (não
// float) — o caso de meio-centavo (ex.: bruta=150 → 150×0,0499 = 7,485) é
// sensível a drift de float e DEVE arredondar p/ 7,49.
//
// AUDIT-2026-07-30 CRITICAL (dono): "para PAD [expedição] não tem taxa de
// transação e essas taxas são personalizáveis e não fixas". Duas correções:
//  1. A taxa deixa de ser hardcoded — passa a ler senderzz_options
//     (sz_affiliate_transaction_fee_pct), mesma option que config_taxas.go já
//     grava no admin (ANTES disso o campo "editável" do admin era decorativo:
//     mudava a option mas o checkout nunca lia, sempre cobrava 4,99% fixo).
//  2. A taxa só se aplica a pedidos COD (motoboy) — PAD/expedição NÃO tem
//     taxa de transação nenhuma (nem do afiliado nem do produtor): a
//     comissão do afiliado vira a BRUTA cheia (sem desconto) e o líquido do
//     produtor não desconta producer_tx_rate. Ver bloco do INSERT abaixo.
const taxaTransacaoAfiliadoStr = "0.0499"

// resolveAffiliateTxRatePct lê sz_affiliate_transaction_fee_pct de
// senderzz_options (percentual, ex.: 4.99) com fallback ao valor histórico
// hardcoded. Best-effort: erro/ausência cai no fallback, nunca falha o pedido.
func (h *CheckoutHandler) resolveAffiliateTxRatePct(ctx context.Context, lg *slog.Logger) decimal.Decimal {
	fallback := decimal.RequireFromString(taxaTransacaoAfiliadoStr).Mul(decimal.NewFromInt(100))
	var raw string
	err := h.db.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE name = 'sz_affiliate_transaction_fee_pct'`,
	).Scan(&raw)
	if err != nil || strings.TrimSpace(raw) == "" {
		return fallback
	}
	pct, errP := decimal.NewFromString(strings.TrimSpace(raw))
	if errP != nil || pct.IsNegative() || pct.GreaterThan(decimal.NewFromInt(100)) {
		lg.Warn("[checkout] sz_affiliate_transaction_fee_pct inválida — usando fallback", "raw", raw)
		return fallback
	}
	return pct
}

// defaultAffiliateCommissionPct: fallback final da % de comissão (global = 10),
// quando nem a oferta nem o produtor definem uma. Espelha o default do WP
// (sz_aff_default_commission_pct) e de go/portal (defaultAffiliateCommissionPct).
const defaultAffiliateCommissionPct = 10.0

// resolveAffiliateCommissionPct resolve a % de comissão do afiliado para esta
// venda, na PRECEDÊNCIA (mais específico vence; 1ª positiva vence):
//
//	override_afiliado → oferta → padrão do produtor → global (10).
//
// Best-effort: todo erro degrada para o próximo elo (e no fim para o default) —
// NUNCA falha o pedido. Clamp 0..100.
//
//   - token:      token do checkout-link (a OFERTA usada nesta venda).
//   - producerID: producer_id da oferta (= PORTAL id do produtor dono), mesmo
//     id-space que keia _sz_aff_default_commission_pct em senderzz_portal_user_meta
//     (ver go/portal/affiliates_portal.go::producerDefaultCommissionPct).
//   - affiliateCustomPct: comissao_pct CUSTOM do vínculo apontado pelo aff_token
//     (senderzz_affiliates.comissao_pct). Só entra no cálculo no tier de override,
//     e SÓ quando a option-toggle sz_aff_per_affiliate_override estiver LIGADA.
func (h *CheckoutHandler) resolveAffiliateCommissionPct(ctx context.Context, lg *slog.Logger, token string, producerID int64, affiliateCustomPct float64) float64 {
	clamp := func(v float64) float64 {
		if v < 0 {
			return 0
		}
		if v > 100 {
			return 100
		}
		return v
	}

	// 0) OVERRIDE POR-AFILIADO (tier mais específico). Liga SÓ quando a option
	//    sz_aff_per_affiliate_override == '1' (toggle global, default '0' — migração
	//    429) E o afiliado tem comissao_pct custom > 0 no vínculo. Fail-open: se a
	//    leitura da option falhar, pulamos o override e caímos na oferta (o pedido
	//    NUNCA falha por causa disso, e o default '0' já mantém o tier desligado).
	if affiliateCustomPct > 0 && h.perAffiliateOverrideEnabled(ctx, lg) {
		return clamp(affiliateCustomPct)
	}

	// 1) % da OFERTA (senderzz_checkout_links.affiliate_commission_pct). Coluna com
	//    DEFAULT 0.00; >0 ⇒ a oferta dita. Erro (ex.: coluna ausente em banco fresco
	//    antes da migração 428) degrada para o produtor — fail-open.
	var offerPct float64
	errOffer := h.db.QueryRow(ctx,
		`SELECT affiliate_commission_pct
		   FROM senderzz_checkout_links
		  WHERE token = $1
		  LIMIT 1`,
		token,
	).Scan(&offerPct)
	if errOffer == nil && offerPct > 0 {
		return clamp(offerPct)
	}
	if errOffer != nil && errOffer != pgx.ErrNoRows {
		lg.Warn("[checkout] falha ao ler comissão da oferta (degrada p/ produtor)", "token", token, "err", errOffer)
	}

	// 2) Padrão do PRODUTOR (_sz_aff_default_commission_pct na meta do produtor).
	var prodRaw string
	errProd := h.db.QueryRow(ctx,
		`SELECT meta_value
		   FROM senderzz_portal_user_meta
		  WHERE user_id = $1 AND meta_key = '_sz_aff_default_commission_pct'
		  LIMIT 1`,
		producerID,
	).Scan(&prodRaw)
	if errProd == nil {
		if v, perr := strconv.ParseFloat(strings.Replace(strings.TrimSpace(prodRaw), ",", ".", 1), 64); perr == nil && v > 0 {
			return clamp(v)
		}
	} else if errProd != pgx.ErrNoRows {
		lg.Warn("[checkout] falha ao ler comissão padrão do produtor (degrada p/ global)", "producer_id", producerID, "err", errProd)
	}

	// 3) Padrão GLOBAL (senderzz_options.sz_aff_default_commission_pct) → fallback 10.
	var globalRaw string
	errGlobal := h.db.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE name = $1 LIMIT 1`,
		"sz_aff_default_commission_pct",
	).Scan(&globalRaw)
	if errGlobal == nil {
		if v, perr := strconv.ParseFloat(strings.Replace(strings.TrimSpace(globalRaw), ",", ".", 1), 64); perr == nil && v > 0 {
			return clamp(v)
		}
	}
	return defaultAffiliateCommissionPct
}

// perAffiliateOverrideEnabled lê o toggle global sz_aff_per_affiliate_override em
// senderzz_options (migração 429, default '0'). Quando '1', o tier de override
// por-afiliado (comissao_pct custom do vínculo) tem precedência sobre a oferta.
// Best-effort: qualquer erro (ou option ausente em banco antigo) ⇒ false — o tier
// de override fica DESLIGADO e a resolução cai na cadeia oferta → produtor → global,
// idêntica ao comportamento anterior. Aceita '1'/'true'/'yes'/'on' como ligado.
func (h *CheckoutHandler) perAffiliateOverrideEnabled(ctx context.Context, lg *slog.Logger) bool {
	var raw string
	err := h.db.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE name = $1 LIMIT 1`,
		"sz_aff_per_affiliate_override",
	).Scan(&raw)
	if err != nil {
		if err != pgx.ErrNoRows {
			lg.Warn("[checkout] falha ao ler toggle de override por-afiliado (tier desligado)", "err", err)
		}
		return false
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// reDigits remove tudo que não é dígito (usado para normalizar CPF/telefone/CEP).
var reDigits = regexp.MustCompile(`\D`)

// onlyDigits retorna apenas os dígitos de s.
func onlyDigits(s string) string {
	return reDigits.ReplaceAllString(s, "")
}

// hasFullName retorna true se s tem pelo menos 2 palavras com 2+ letras cada
// (nome + sobrenome) — bloqueia nome único, insuficiente para endereçar entrega.
func hasFullName(s string) bool {
	n := 0
	for _, w := range strings.Fields(strings.TrimSpace(s)) {
		if len([]rune(w)) >= 2 {
			n++
		}
	}
	return n >= 2
}

// nameParticlesLower — preposições/artigos de nome PT-BR que ficam minúsculas
// no meio do nome (ex: "Lucas de Jesus Nobrega"), exceto quando são a 1ª palavra.
var nameParticlesLower = map[string]bool{
	"de": true, "da": true, "do": true, "das": true, "dos": true, "e": true,
}

// titleCaseName normaliza o nome do cliente pra Title Case ("lucas de jesus
// nobrega" → "Lucas de Jesus Nobrega") — o checkout recebe o nome como o
// comprador digitou (sem forçar capitalização no client), então o back
// normaliza antes de persistir (mesmo texto vai pro motoboy/etiqueta).
func titleCaseName(s string) string {
	words := strings.Fields(strings.TrimSpace(s))
	for i, w := range words {
		lw := strings.ToLower(w)
		if i > 0 && nameParticlesLower[lw] {
			words[i] = lw
			continue
		}
		r := []rune(lw)
		if len(r) > 0 {
			r[0] = unicode.ToUpper(r[0])
		}
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}

// ── GET /checkout-api/offer ─────────────────────────────────────────────────

// offerResponse expõe SOMENTE os campos públicos da oferta.
// Nunca inclui url, post_id, price_label ou producer interno sensível.
type offerResponse struct {
	Nome       string `json:"nome"`
	Preco      string `json:"preco"` // = display_value, string fixa 2 casas
	ProdutorID int64  `json:"produtor_id"`
	Slug       string `json:"slug"`
	Tipo       string `json:"tipo"`
	Disponivel bool   `json:"disponivel"`
	// ImageURL: thumbnail do produto. Origem: sz_products.meta (chaves de imagem
	// sincronizadas do WP). A imagem real do produto vive como attachment do
	// WooCommerce (get_image_id) e NÃO está espelhada no Postgres; quando o sync
	// não a populou, devolvemos "" (string vazia, sempre presente — nunca omitida).
	ImageURL string `json:"image_url"`
	// FEAT-STOCK: disponibilidade somada nos CDs do produto (qty_available - qty_reserved).
	// Semântica de AVISO (não bloqueio): o front usa para "últimas unidades"/"esgotado".
	//   • ponteiro nil + omitempty ⇒ campo OMITIDO no JSON ⇒ produto SEM linha de
	//     estoque cadastrada = ILIMITADO (nunca bloqueia venda — regra do dono).
	//   • valor 0  ⇒ existe linha de estoque e sellable<=0 ⇒ esgotado (apenas aviso).
	//   • valor >0 ⇒ unidades vendáveis remanescentes.
	QtySellable *int64 `json:"qty_sellable,omitempty"`
	// CHECKOUT-BRANDING (white-label v1): marca do checkout DO PRODUTOR, lida de
	// senderzz_portal_user_meta (sz_brand_logo_url / sz_brand_primary_color),
	// keyed por producer_id (= PORTAL id). Best-effort: ausência ⇒ "" (omitido) ⇒
	// o front cai no fallback padrão FALK (logo falcão + azul #1E6FF2).
	//
	// As CHAVES JSON são `logo_url` e `cor` DE PROPÓSITO: o checkout-ui/api.ts já
	// as mapeia para offer.brand.{logo_url,accent} (pipeline white-label existente
	// em brand.ts → resolveBrand → accentCssVars). Reusar essas chaves evita criar
	// um caminho paralelo no front — a marca passa a renderizar automaticamente.
	BrandLogoURL      string `json:"logo_url,omitempty"`
	BrandPrimaryColor string `json:"cor,omitempty"`
	BrandName         string `json:"marca,omitempty"`
	BannerURL         string `json:"banner_url,omitempty"`
	BrandWhatsApp     string `json:"whatsapp,omitempty"`
	BrandTextColor    string `json:"texto,omitempty"`
	// FEAT-CHECKOUT-MULTI-ITEM: itens da composição do link (2+ produtos/variações
	// escolhidos pelo PRODUTOR ao criar o link — o comprador não escolhe nada aqui,
	// só vê a lista). Omitido quando a oferta é de produto único (comportamento
	// de sempre); o front (checkout-ui/src/api.ts) já sabia ler este campo.
	Itens []offerItem `json:"itens,omitempty"`
}

type offerItem struct {
	Nome string `json:"nome"`
	Qtd  int    `json:"qtd"`
}

// resolveProducerBrand lê a marca white-label do PRODUTOR (logo + cor primária)
// de senderzz_portal_user_meta, keyed por producerID (= PORTAL id, mesmo
// id-space de senderzz_checkout_links.producer_id). Best-effort: qualquer erro
// ou ausência ⇒ strings vazias — o GET /offer NUNCA falha por causa da marca e o
// front cai no fallback FALK. Chaves gravadas pelo portal (settings.go UpdateBrand):
//
//	sz_brand_logo_url       — URL https do logo
//	sz_brand_primary_color  — hex #RRGGBB / #RGB
func (h *CheckoutHandler) resolveProducerBrand(ctx context.Context, producerID, linkID int64) (logoURL, primaryColor, name, bannerURL, whatsapp, textColor string) {
	rows, err := h.db.Query(ctx,
		`SELECT meta_key, meta_value
		   FROM senderzz_portal_user_meta
		  WHERE user_id = $1
		    AND meta_key IN ('sz_brand_primary_color', 'sz_brand_name', 'sz_checkout_banner_url', 'sz_brand_whatsapp', 'sz_brand_text_color')`,
		producerID,
	)
	if err != nil {
		return "", "", "", "", "", ""
	}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return logoURL, primaryColor, name, bannerURL, whatsapp, textColor
		}
		switch k {
		case "sz_brand_primary_color":
			primaryColor = strings.TrimSpace(v)
		case "sz_brand_name":
			name = strings.TrimSpace(v)
		case "sz_checkout_banner_url":
			bannerURL = strings.TrimSpace(v)
		case "sz_brand_whatsapp":
			whatsapp = strings.TrimSpace(v)
		case "sz_brand_text_color":
			textColor = strings.TrimSpace(v)
		}
	}
	rows.Close()
	// Compatibilidade do produtor Egipzya: antes da configuração por perfil, o
	// checkout usava este número fixo. Mantemos somente para essa conta legada;
	// demais produtores sem número ficam sem botão de WhatsApp.
	if whatsapp == "" {
		var email string
		if err := h.db.QueryRow(ctx, `SELECT email FROM senderzz_portal_users WHERE id = $1`, producerID).Scan(&email); err == nil && strings.EqualFold(strings.TrimSpace(email), "sac@gestao.io") {
			whatsapp = "5511963486603"
		}
	}
	if linkID > 0 {
		var override string
		if err := h.db.QueryRow(ctx,
			`SELECT meta_value FROM senderzz_portal_user_meta WHERE user_id = $1 AND meta_key = $2 LIMIT 1`,
			producerID, fmt.Sprintf("_sz_checkout_banner_url:%d", linkID),
		).Scan(&override); err == nil {
			bannerURL = strings.TrimSpace(override)
		}
	}
	return logoURL, primaryColor, name, bannerURL, whatsapp, textColor
}

// GetOffer resolve uma oferta pública pelo token do link de checkout.
//
// Query param obrigatório: token
//
// Retorna 404 se o token não existir. Uma oferta é considerada "disponível"
// quando display_value > 0 (não há coluna de ativação na tabela espelho do WP).
func (h *CheckoutHandler) GetOffer(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "token é obrigatório")
		return
	}

	var (
		linkID       int64
		producerID   int64
		postID       int64
		displayValue string
		name, slug   string
		tipo         string
		compRaw      []byte
	)
	// BUYER-FACING: o nome mostrado ao cliente é a BASE (composição do produto), NUNCA
	// o sufixo de ESTÁGIO (Downsell/Remarketing) — esse é um rótulo INTERNO do funil do
	// produtor (decisão do dono 2026-06-24). base_name é populada na criação/migração 471;
	// COALESCE cai no name legado se vier vazia.
	err := h.db.QueryRow(r.Context(),
		`SELECT id, producer_id, post_id, display_value,
		        COALESCE(NULLIF(base_name, ''), name), slug, tipo, composition_items
		   FROM senderzz_checkout_links
		  WHERE token = $1
		  LIMIT 1`,
		token,
	).Scan(&linkID, &producerID, &postID, &displayValue, &name, &slug, &tipo, &compRaw)
	if err == pgx.ErrNoRows {
		httpx.WriteErr(w, http.StatusNotFound, "oferta não encontrada")
		return
	}
	if err != nil {
		httpx.LoggerFrom(r.Context()).Error("[checkout] falha ao buscar oferta", "token", token, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao buscar oferta")
		return
	}

	// image_url: resolve a partir de sz_products (mapeado por wp_post_id). A coluna
	// não existe — a imagem, se houver, está em sz_products.meta (jsonb). Best-effort:
	// nunca falha o /offer por isso; ausência ⇒ "".
	imageURL := h.resolveProductImage(r.Context(), postID)

	// FEAT-STOCK: leitura (aviso, NÃO bloqueio) da disponibilidade somada nos CDs.
	// nil ⇒ sem linha de estoque ⇒ ilimitado (campo omitido no JSON). Ver helper.
	qtySellable := h.resolveQtySellable(r.Context(), postID)

	// CHECKOUT-BRANDING: marca white-label do produtor (logo + cor). Best-effort —
	// nunca falha o /offer; ausência ⇒ "" e o front usa o fallback FALK.
	brandLogo, brandColor, brandName, bannerURL, brandWhatsApp, brandTextColor := h.resolveProducerBrand(r.Context(), producerID, linkID)

	// FEAT-CHECKOUT-MULTI-ITEM: composition_items → lista de itens exibida ao
	// comprador (só leitura — quem escolheu foi o produtor na criação do link).
	var itens []offerItem
	if len(compRaw) > 0 {
		var comp []struct {
			ProductID int64  `json:"product_id"`
			Qty       int    `json:"qty"`
			Variacao  string `json:"variacao"`
		}
		if err := json.Unmarshal(compRaw, &comp); err == nil && len(comp) > 1 {
			ids := make([]int64, len(comp))
			for i, c := range comp {
				ids[i] = c.ProductID
			}
			nomes := make(map[int64]string, len(comp))
			rows, errN := h.db.Query(r.Context(),
				`SELECT id, nome FROM sz_products WHERE id = ANY($1)`, ids,
			)
			if errN == nil {
				for rows.Next() {
					var id int64
					var nome string
					if rows.Scan(&id, &nome) == nil {
						nomes[id] = nome
					}
				}
				rows.Close()
			}
			for _, c := range comp {
				nome := nomes[c.ProductID]
				if strings.TrimSpace(c.Variacao) != "" {
					nome = nome + " — " + strings.TrimSpace(c.Variacao)
				}
				qtd := c.Qty
				if qtd < 1 {
					qtd = 1
				}
				itens = append(itens, offerItem{Nome: nome, Qtd: qtd})
			}
		}
	}

	preco, _ := decimal.NewFromString(displayValue)
	resp := offerResponse{
		Nome:              name,
		Preco:             preco.StringFixed(2),
		ProdutorID:        producerID,
		Slug:              slug,
		Tipo:              tipo,
		Disponivel:        preco.GreaterThan(decimal.Zero),
		ImageURL:          imageURL,
		QtySellable:       qtySellable,
		BrandLogoURL:      brandLogo,
		BrandPrimaryColor: brandColor,
		BrandName:         brandName,
		BannerURL:         bannerURL,
		BrandWhatsApp:     brandWhatsApp,
		BrandTextColor:    brandTextColor,
		Itens:             itens,
	}

	httpx.WriteOK(w, map[string]any{"oferta": resp})
}

// ── POST /checkout-api/order ────────────────────────────────────────────────

// checkoutOrderRequest é o payload do checkout público.
//
// IMPORTANTE: não há campo de preço aqui. O total é SEMPRE recalculado a partir
// de senderzz_checkout_links.display_value no servidor (corrige o HIGH).
type checkoutOrderRequest struct {
	Token        string `json:"token"`
	AffToken     string `json:"aff_token"` // opcional — token de afiliado (senderzz_affiliate_links.link_token)
	Ref          string `json:"ref"`       // opcional — token de rastreio ?r= (base64url(BE32(senderzz_affiliates.id)+salt)) — FEAT-AFF-ATTRIBUTION
	Nome         string `json:"nome"`
	Telefone     string `json:"telefone"`
	CPF          string `json:"cpf"`
	Email        string `json:"email"`
	CEP          string `json:"cep"`
	Logradouro   string `json:"logradouro"`
	Numero       string `json:"numero"`
	Complemento  string `json:"complemento"`
	Bairro       string `json:"bairro"`
	Cidade       string `json:"cidade"`
	UF           string `json:"uf"`
	CustomerNote string `json:"observacao"`

	// Agendamento de entrega (apenas ofertas tipo motoboy).
	// DataEntrega é a data escolhida (YYYY-MM-DD). Tipo ('agendamento'|'pre_agendado')
	// vindo do cliente é IGNORADO — recomputamos server-side (não confie no cliente).
	DataEntrega string `json:"data_entrega"`
	Tipo        string `json:"tipo"`

	// FEAT-FRETE: seleção de frete (apenas ofertas tipo correio). FreightID é o id
	// da opção escolhida pelo cliente. CRIT-01: o PREÇO do cliente é ignorado — o
	// servidor RE-cota o frete (mesma origem/dimensões/CEP) e usa O SEU preço para
	// a opção cujo id casa com FreightID. Se não casar → erro, não aceita.
	FreightID string `json:"freight_id"`

	// FEAT-LGPD: consentimento de política de privacidade no checkout.
	//   ConsentAccepted  — o cliente marcou o aceite da política (true).
	//   ConsentDocVersion — versão da política aceita (ex.: "2026-06-18"). Obrigatório
	//                       quando ConsentAccepted=true. Sem versão → input inválido.
	//   ConsentDocType    — tipo do documento; default 'privacy' quando vazio.
	// OPCIONAL no payload: pedidos sem consentimento NÃO são rejeitados (não quebra
	// o fluxo atual — o checkout-ui ainda não envia estes campos). Quando ENVIADO,
	// o input é validado (versão não-vazia) e o aceite é carimbado SERVER-SIDE
	// (ip/user-agent/timestamp nunca vêm do cliente). Ver bloco "consentimento".
	ConsentAccepted   bool   `json:"consent_accepted"`
	ConsentDocVersion string `json:"consent_doc_version"`
	ConsentDocType    string `json:"consent_doc_type"`
}

// decodeAffRefToken decodifica o token de rastreio do afiliado (?r=) de volta ao
// senderzz_affiliates.id (id do VÍNCULO). É o INVERSO de links_portal.go
// appendRefToken / WP sz_aff_decode_ref_token: o token é base64url SEM padding de
// pack('N', id) . salt[:4]; só os 4 PRIMEIROS bytes (uint32 big-endian) importam —
// o sufixo de salt é ignorado na decodificação (igual ao WP, que faz só
// unpack('N', substr(decoded,0,4))). Retorna (0,false) em qualquer token malformado
// — o chamador trata como "sem afiliado" e NÃO falha o pedido.
func decodeAffRefToken(token string) (int64, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return 0, false
	}
	// base64url → base64 std + re-padding (StdEncoding exige padding completo).
	std := strings.ReplaceAll(token, "-", "+")
	std = strings.ReplaceAll(std, "_", "/")
	if m := len(std) % 4; m != 0 {
		std += strings.Repeat("=", 4-m)
	}
	raw, err := base64.StdEncoding.DecodeString(std)
	if err != nil || len(raw) < 4 {
		return 0, false
	}
	id := uint32(raw[0])<<24 | uint32(raw[1])<<16 | uint32(raw[2])<<8 | uint32(raw[3])
	if id == 0 {
		return 0, false
	}
	return int64(id), true
}

// PostOrder cria um pedido COD a partir de um link de checkout público.
//
// Fluxo (tudo em uma transação):
//  1. Valida o link e lê display_value/producer_id SERVER-SIDE.
//  2. (opcional) Resolve o afiliado via aff_token → afiliado_id (wp_user_id).
//  3. Advisory lock por (token+cpf) para matar o double-click.
//  4. Idempotência: se já existe pedido com mesmo checkout_idem em < 10 min, retorna ele.
//  5. Insere sz_orders (status 'pending', payment_method 'cod', total = display_value).
//  6. Insere sz_order_items (1x a oferta).
//  7. Insere sz_order_addresses (shipping, com numero!).
//  8. Insere sz_order_meta (_billing_cpf, _billing_cellphone, checkout_idem, checkout_token).
//  9. Registra histórico inicial.
func (h *CheckoutHandler) PostOrder(w http.ResponseWriter, r *http.Request) {
	var req checkoutOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	req.Token = strings.TrimSpace(req.Token)
	if req.Token == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "token é obrigatório")
		return
	}
	// Exige nome + sobrenome (>=2 palavras com >=2 letras cada) — nome sozinho não é
	// endereçável na entrega/motoboy. Espelha a validação client-side (Checkout.tsx
	// nameOk); aqui é a rede de segurança contra POST direto na API.
	if !hasFullName(req.Nome) {
		httpx.WriteErr(w, http.StatusBadRequest, "informe nome e sobrenome do cliente")
		return
	}
	// Normaliza pra Title Case ANTES de persistir — flui pros dois INSERTs
	// (sz_order_addresses.nome e sz_motoboy_pedidos.dest_nome) e pra etiqueta.
	req.Nome = titleCaseName(req.Nome)
	// CHECKOUT-NO-CPF (2026-06-18): o cliente NÃO informa mais CPF no checkout.
	// O campo continua aceito no payload (compat de schema), porém é OPCIONAL: se
	// vier vazio, criamos o pedido normalmente. cpfDigits pode ser "" daqui pra frente.
	// A idempotência NÃO depende mais do CPF — usa o header Idempotency-Key (ver abaixo),
	// que dá uma chave por carregamento de oferta no browser, então compradores
	// distintos da MESMA oferta não colapsam no mesmo pedido.
	cpfDigits := onlyDigits(req.CPF) // "" quando o cliente não informa (esperado)
	// Endereço de entrega é crítico para o motoboy (COD entrega no endereço).
	if strings.TrimSpace(req.Numero) == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "número do endereço é obrigatório para entrega")
		return
	}

	// ── FEAT-LGPD: validação do input de consentimento (fail-closed no INPUT) ─
	// AUDIT-2026-06-21 #11/#24: consentimento é OBRIGATÓRIO e validado SERVER-SIDE,
	// não só pelo checkbox client-side. O único chamador de POST /checkout-api/order
	// é o checkout-ui (createOrder), que agora SEMPRE envia consent_accepted=true +
	// consent_doc_version. Quem desabilita JS ou chama o endpoint direto (vetor #24)
	// é rejeitado aqui — não dá para registrar "qual política" sem a versão. Isso
	// fecha a lacuna do Art. 8 (consentimento demonstrável): nenhum pedido nasce sem
	// prova de aceite (versão/ip/timestamp carimbados SERVER-SIDE, nunca do cliente).
	var (
		recordConsent  bool
		consentDocType string
		consentDocVer  string
	)
	// AUDIT-LGPD-2026-06-24 #M1 (Art. 8 §1/§2): o ônus de provar o consentimento é do
	// controlador, então o aceite NÃO pode ser inferido apenas da presença da versão —
	// o valor REAL de consent_accepted tem de ser true. Se o titular não aceitou
	// (consent_accepted=false ou ausente), REJEITAMOS o pedido ANTES de criá-lo e NÃO
	// gravamos nenhum consentimento afirmativo. Assim _sz_consent_privacy="1" só é
	// escrito quando o aceite é genuíno (garantido por esta rejeição).
	if !req.ConsentAccepted {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "É necessário aceitar a Política de Privacidade para finalizar.")
		return
	}
	consentDocVer = strings.TrimSpace(req.ConsentDocVersion)
	if consentDocVer == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "consentimento da política de privacidade é obrigatório (consent_doc_version)")
		return
	}
	consentDocType = strings.TrimSpace(req.ConsentDocType)
	if consentDocType == "" {
		consentDocType = "privacy"
	}
	// Limita os tamanhos às colunas de senderzz_consents (doc_type/doc_version VARCHAR(40)).
	if len(consentDocType) > 40 {
		consentDocType = consentDocType[:40]
	}
	if len(consentDocVer) > 40 {
		consentDocVer = consentDocVer[:40]
	}
	recordConsent = true

	ctx := r.Context()
	// OBSERVABILIDADE: logger decorado com request_id (injetado pelo slogMiddleware).
	// Todas as linhas de negócio deste handler usam lg.* para correlacionar com a
	// linha de acesso [http]. Fallback p/ slog.Default() quando sem middleware (testes).
	lg := httpx.LoggerFrom(ctx)

	// ── 1. Valida o link e lê o preço SERVER-SIDE ────────────────────────────
	var (
		producerID   int64
		postID       int64
		displayValue string
		linkName     string
		linkTipo     string
		compRaw      []byte
	)
	// BUYER-FACING snapshot: o item do pedido (sz_order_items.nome, dest_produto do
	// motoboy, rastreio público) guarda a BASE do produto, não o sufixo de ESTÁGIO
	// interno do funil. O produtor atribui a oferta pelo checkout_link_id na meta, não
	// pelo nome do item. COALESCE cai no name legado se base_name vier vazia.
	err := h.db.QueryRow(ctx,
		`SELECT producer_id, post_id, display_value,
		        COALESCE(NULLIF(base_name, ''), name), tipo, composition_items
		   FROM senderzz_checkout_links
		  WHERE token = $1
		  LIMIT 1`,
		req.Token,
	).Scan(&producerID, &postID, &displayValue, &linkName, &linkTipo, &compRaw)
	if err == pgx.ErrNoRows {
		httpx.WriteErr(w, http.StatusNotFound, "oferta não encontrada")
		return
	}
	if err != nil {
		lg.Error("[checkout] falha ao validar link", "token", req.Token, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	total, _ := decimal.NewFromString(displayValue)
	if total.LessThanOrEqual(decimal.Zero) {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "oferta indisponível")
		return
	}
	if strings.TrimSpace(linkName) == "" {
		linkName = "Pedido"
	}

	// ── 1b. Agendamento de entrega (apenas ofertas tipo motoboy) ─────────────
	// Resolve a zona pelo CEP e valida a data escolhida contra as datas que o
	// servidor REALMENTE oferta (mesma função do /schedule). Tanto a data quanto
	// o tipo (agendamento|pre_agendado) são recomputados aqui — não se confia no
	// cliente (CRIT-01: preço/regras sempre server-side).
	//
	// schedZona != nil  ⇒ criaremos sz_motoboy_pedidos depois do INSERT do pedido.
	// schedData/schedTipo guardam a data/classificação VALIDADAS server-side.
	var (
		schedZona *zoneInfo
		schedData string // YYYY-MM-DD validado
		schedTipo string // 'agendado' | 'pre_agendado' (status do pedido motoboy)
	)
	isMotoboy := strings.EqualFold(strings.TrimSpace(linkTipo), "motoboy")
	// FEAT-LINK-MISTO (2026-07-27): tipo='misto' não tem modo fixo — resolve AQUI,
	// server-side, pelo CEP enviado. CRIT-01: nunca confia no que o front decidiu
	// (o /resolve que o front chamou é só UX/preview) — mesma checagem (zona cobre
	// o CEP + tem data ofertável) que o /resolve e o /schedule usam. Sem zona/data
	// ⇒ isMotoboy fica false ⇒ cai no fluxo Expedição abaixo (CPF+frete), igual a
	// um link tipo='correio' normal.
	if strings.EqualFold(strings.TrimSpace(linkTipo), "misto") {
		destCEP := onlyDigits(req.CEP)
		if len(destCEP) != 8 {
			httpx.WriteErr(w, http.StatusUnprocessableEntity, "CEP de entrega inválido (8 dígitos)")
			return
		}
		zona, errZ := resolveZonaPorCEP(ctx, h.db, destCEP)
		if errZ != nil {
			lg.Error("[checkout] falha ao resolver modo do link misto", "err", errZ)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao resolver modo de entrega")
			return
		}
		if zona != nil {
			datas, errD := computeOfferableDates(ctx, h.db, zona)
			if errD == nil && len(datas) > 0 {
				isMotoboy = true
			}
		}
	}
	wantsSchedule := strings.TrimSpace(req.DataEntrega) != ""

	if isMotoboy && wantsSchedule {
		zona, errZ := resolveZonaPorCEP(ctx, h.db, req.CEP)
		if errZ != nil {
			// AUDIT-LGPD-2026-06-24 #B5 (Art. 6 III / Art. 46): NÃO logamos o CEP do cliente
			// (quasi-PII) em logs de aplicação. Registramos só uma flag indicando que o CEP
			// foi informado, suficiente para diagnóstico sem vazar o dado.
			lg.Error("[checkout] falha ao resolver zona", "cep_informado", onlyDigits(req.CEP) != "", "err", errZ)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao resolver zona de entrega")
			return
		}
		if zona == nil {
			httpx.WriteErr(w, http.StatusUnprocessableEntity, "fora de área de entrega")
			return
		}

		datas, errD := computeOfferableDates(ctx, h.db, zona)
		if errD != nil {
			lg.Error("[checkout] falha ao calcular datas de entrega", "err", errD)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao calcular datas de entrega")
			return
		}

		// A data DEVE estar entre as ofertadas. Caso contrário, rejeita (não
		// confiar no cliente). O tipo é o que o servidor classificou, não o
		// enviado no payload.
		chosen := strings.TrimSpace(req.DataEntrega)
		var matched *offeredDate
		for i := range datas {
			if datas[i].Data == chosen {
				matched = &datas[i]
				break
			}
		}
		if matched == nil {
			httpx.WriteErr(w, http.StatusUnprocessableEntity, "data de entrega indisponível — escolha uma das datas ofertadas")
			return
		}

		schedZona = zona
		schedData = matched.Data
		// Mapeia o tipo de oferta para o status do pedido motoboy:
		//   'agendamento'  → status 'agendado'
		//   'pre_agendado' → status 'pre_agendado'
		if matched.Tipo == "agendamento" {
			schedTipo = "agendado"
		} else {
			schedTipo = "pre_agendado"
		}
	}

	// ── 1c. Frete (apenas ofertas tipo correio) — FEAT-FRETE ─────────────────
	// subtotal = valor da OFERTA (display_value). Para correio somamos o frete
	// RECALCULADO server-side ao total.
	//   • motoboy: SEM cpf, SEM frete (total = oferta) — inalterado.
	//   • correio: EXIGE cpf (Correios pede CPF do destinatário) e freight_id.
	//     CRIT-01: re-cotamos o frete (mesma origem/dimensões/CEP), casamos o id e
	//     usamos O NOSSO preço — o preço enviado pelo cliente é ignorado. Se o id
	//     não casar → erro.
	subtotal := total // = display_value (valor da oferta)
	var (
		precoFrete   = decimal.Zero
		freteEscolha *freight.ServiceOption // opção casada (preço/empresa server-side)
	)
	if !isMotoboy {
		// CPF obrigatório p/ correio (volta o CPF removido em CHECKOUT-NO-CPF, só aqui).
		if cpfDigits == "" || len(cpfDigits) != 11 {
			httpx.WriteErr(w, http.StatusUnprocessableEntity, "CPF é obrigatório para envio por transportadora")
			return
		}
		freightID := strings.TrimSpace(req.FreightID)
		if freightID == "" {
			httpx.WriteErr(w, http.StatusUnprocessableEntity, "selecione uma opção de frete")
			return
		}
		destCEP := onlyDigits(req.CEP)
		if len(destCEP) != 8 {
			httpx.WriteErr(w, http.StatusUnprocessableEntity, "CEP de entrega inválido (8 dígitos)")
			return
		}

		// RE-cotação server-side: mesmas entradas resolvidas no GET /freight.
		origemCEP := h.resolveOrigemCEP(ctx, producerID)
		// ValorDeclarado (seguro) já vem padronizado em R$99 do resolvePackage —
		// NÃO sobrescrever com o subtotal aqui. Setar por subtotal fazia o preço
		// recotado (e gravado em sz_orders.shipping) divergir do que o GET /freight
		// mostrou ao cliente no checkout (mesma pkg, seguro diferente = preço ME
		// diferente). Ver freightInsuranceValueDefault em freight.go.
		pkg := h.resolvePackage(ctx, producerID, postID, linkName)

		opcoes, errF := freight.Calculate(ctx, origemCEP, destCEP, pkg)
		if errF != nil {
			lg.Error("[checkout] falha ao recotar frete", "token", req.Token, "err", errF)
			httpx.WriteErr(w, http.StatusUnprocessableEntity, "não foi possível cotar o frete para este CEP")
			return
		}
		opcoes = h.applyCarrierPreferences(ctx, producerID, opcoes)
		opcoes = h.applyFreightMarkup(ctx, postID, opcoes)
		opcoes = h.applyFixedFreight(ctx, producerID, opcoes)
		opcoes = h.applyCorreiosLock(ctx, producerID, opcoes)
		for i := range opcoes {
			if opcoes[i].ID == freightID {
				freteEscolha = &opcoes[i]
				break
			}
		}
		if freteEscolha == nil {
			// O id não casou com nenhuma opção re-cotada — não aceitar (CRIT-01).
			httpx.WriteErr(w, http.StatusUnprocessableEntity, "opção de frete inválida — recalcule o frete e tente novamente")
			return
		}

		// Preço do SERVIDOR (nunca o do cliente). decimal a partir do float arredondado.
		precoFrete = decimal.NewFromFloat(freteEscolha.Price).Round(2)
		total = subtotal.Add(precoFrete)
	}

	// ── 2. Resolve afiliado (best-effort — nunca falha o pedido por isso) ─────
	// Lê o vínculo APONTADO pelo aff_token (uma única linha sa). Além do
	// afiliado_id (= wp_user_id gravado em sz_orders.affiliate_id), captura também
	// a comissao_pct CUSTOM desse vínculo — usada SÓ no tier de override (ver 2b).
	// comissao_pct é NUMERIC → cast ::float8 (pgx não escaneia NUMERIC→float64
	// limpo num path financeiro; mesmo padrão de affiliates_portal.go:348).
	// AUDIT-2026-07-31 (dono): "pedido PAD não tem afiliado" — regra de negócio,
	// não só de taxa. PAD/expedição NUNCA atribui afiliado, mesmo que um
	// aff_token/?r= válido venha no request (produtor pode ter compartilhado um
	// link de afiliado por engano, ou o cliente arrastou a query de outra sessão).
	var affiliateID *int64
	var affiliateCustomPct float64 // comissao_pct do vínculo do aff_token (0 = sem custom)
	if isMotoboy && strings.TrimSpace(req.AffToken) != "" {
		at := strings.TrimSpace(req.AffToken)
		var afiliadoID int64
		var customPct float64
		errAff := h.db.QueryRow(ctx,
			`SELECT sa.afiliado_id, COALESCE(sa.comissao_pct, 0)::float8
			   FROM senderzz_affiliate_links al
			   JOIN senderzz_affiliates sa ON sa.id = al.affiliate_id
			  WHERE al.link_token = $1 AND al.active = TRUE
			  LIMIT 1`,
			at,
		).Scan(&afiliadoID, &customPct)
		if errAff == nil && afiliadoID > 0 {
			// affiliate_id em sz_orders é o wp_user_id (afiliado_id), NUNCA senderzz_affiliates.id.
			affiliateID = &afiliadoID
			affiliateCustomPct = customPct
		} else if errAff != nil && errAff != pgx.ErrNoRows {
			lg.Warn("[checkout] falha ao resolver afiliado (ignorado)", "aff_token", at, "err", errAff)
		}
	}

	// FEAT-AFF-ATTRIBUTION (2026-06-24): atribuição via token de rastreio ?r= — o
	// esquema FIEL ao WP (e o ÚNICO que o portal emite hoje: links_portal.go
	// appendRefToken). O ?r= codifica base64url(pack('N', senderzz_affiliates.id) .
	// salt[:4]); decodificamos os 4 PRIMEIROS bytes (BE uint32) = id do VÍNCULO e
	// resolvemos o afiliado ATIVO desse vínculo. SERVER-AUTHORITATIVE: a identidade e
	// a validação vêm 100% do banco — o front só repassa o token opaco; a comissão %
	// é resolvida pela precedência do bloco 2b (override→oferta→produtor→global),
	// NUNCA pelo cliente. Best-effort: jamais falha o pedido. Só roda se o aff_token
	// (legado, tabela vazia em prod) não resolveu antes.
	if isMotoboy && affiliateID == nil {
		if ref := strings.TrimSpace(req.Ref); ref != "" {
			if vinculoID, ok := decodeAffRefToken(ref); ok {
				var afiliadoID int64
				var customPct float64
				errRef := h.db.QueryRow(ctx,
					`SELECT afiliado_id, COALESCE(comissao_pct, 0)::float8
					   FROM senderzz_affiliates
					  WHERE id = $1
					    AND lower(status) IN ('active','ativo','aprovado','approved')
					  LIMIT 1`,
					vinculoID,
				).Scan(&afiliadoID, &customPct)
				if errRef == nil && afiliadoID > 0 {
					// Igual ao aff_token: sz_orders.affiliate_id = wp_user_id (afiliado_id).
					affiliateID = &afiliadoID
					affiliateCustomPct = customPct
				} else if errRef != nil && errRef != pgx.ErrNoRows {
					lg.Warn("[checkout] falha ao resolver afiliado via ?r= (ignorado)", "ref", ref, "vinculo_id", vinculoID, "err", errRef)
				}
			}
		}
	}

	// ── 2b. Comissão do afiliado (FEAT-AFF-COMMISSION) ────────────────────────
	// MODELO (dono) — PRECEDÊNCIA (mais específico vence, 1ª positiva vence):
	//   override_afiliado (toggle sz_aff_per_affiliate_override=ON E o afiliado tem
	//                      comissao_pct custom > 0 no vínculo senderzz_affiliates)
	//     → comissão da OFERTA (senderzz_checkout_links.affiliate_commission_pct > 0)
	//     → padrão do PRODUTOR (_sz_aff_default_commission_pct na meta do produtor)
	//     → global (sz_aff_default_commission_pct → 10).
	// O toggle nasce DESLIGADO (migração 429, default '0'): com ele off a resolução
	// é BYTE-IDÊNTICA ao comportamento anterior (oferta → produtor → global) — o tier
	// de override é puro no-op até o dono ligar. Por isso o golden #1587 e os pedidos
	// existentes não mudam (somado ao fato de a resolução ser INSERT-only).
	// Fórmula (FIEL ao WP/origin — ver includes/senderzz-affiliates.php:1554 +
	// sz_aff_split_transaction_fee_parts; valida contra golden #1587/#1380):
	//   bruta = round(total * pct/100, 2)            -- comissão BRUTA
	//   fee   = round(bruta * 0,0499, 2)             -- taxa transação (FEE-FIRST)
	//   net   = bruta - fee                          -- comissão LÍQUIDA do afiliado
	// Grava affiliate_amount = net (NUNCA a bruta) e transaction_fee = fee.
	//
	// GOLDEN/segurança: isto SÓ roda quando affiliateID != nil e é INSERT-only —
	// não há UPDATE em sz_orders, então os 41 pedidos existentes ficam intactos
	// POR CONSTRUÇÃO. Sem afiliado, ambos ficam no DEFAULT 0.00.
	//
	// FAIL-OPEN: a leitura da % é best-effort fora do SELECT crítico da oferta
	// (mesmo princípio do bloco de afiliado acima). Erro ⇒ pct cai no default; o
	// pedido NUNCA falha por causa da comissão. Isso também torna o deploy à prova
	// de ordem (se a coluna affiliate_commission_pct ainda não existir num banco
	// fresco, a query degrada e usamos o default — nunca um 500 de checkout).
	// AUDIT-2026-07-31 (dono): "pedido PAD não tem afiliado" — regra de negócio
	// (não só de taxa). affiliateID já é SEMPRE nil pra PAD/expedição (gate
	// acima, na resolução do afiliado) — esse bloco só roda de verdade pra
	// COD/motoboy. Golden #1587 (motoboy) preservado byte-a-byte.
	affiliateAmount := decimal.Zero
	transactionFee := decimal.Zero
	if affiliateID != nil {
		pct := h.resolveAffiliateCommissionPct(ctx, lg, req.Token, producerID, affiliateCustomPct)
		if pct > 0 {
			pctDec := decimal.NewFromFloat(pct)
			bruta := total.Mul(pctDec).Div(decimal.NewFromInt(100)).Round(2)
			taxaPct := h.resolveAffiliateTxRatePct(ctx, lg)
			taxa := taxaPct.Div(decimal.NewFromInt(100))
			fee := bruta.Mul(taxa).Round(2) // FEE-FIRST (golden #1587: 150×0,0499→7,49)
			net := bruta.Sub(fee)
			if net.IsNegative() {
				net = decimal.Zero
			}
			affiliateAmount = net
			transactionFee = fee
		}
	}

	// ── 3. produto_id: resolve via sz_resolve_product_id (mesma função usada por
	// resolveQtySellable) e devolve o wp_post_id canônico — convenção usada por
	// sz_stock.product_id e pelo nome exibido no admin (stockProductNameExpr).
	// AUDIT-STOCK-DUP-2026-07-08: o lookup anterior (só wp_post_id = $1) não cobria
	// postID = post_id de LINK de checkout (caso 3 de sz_resolve_product_id), então
	// caía no fallback produtoID = postID cru — cada link diferente criava uma linha
	// FANTASMA nova em sz_stock (produto "duplicado"/"sem nome") em vez de reservar
	// no produto real.
	produtoID := postID
	var mappedID int64
	errProd := h.db.QueryRow(ctx,
		`SELECT COALESCE(wp_post_id, id) FROM sz_products WHERE id = sz_resolve_product_id($1)`, postID,
	).Scan(&mappedID)
	if errProd == nil && mappedID > 0 {
		produtoID = mappedID
	}

	// CHECKOUT-NO-CPF (2026-06-18): a idempotência usa o header Idempotency-Key
	// (UUID por carregamento de oferta gerado no browser — ver checkout-ui/api.ts).
	// Double-clicks do mesmo formulário compartilham a chave; compradores distintos
	// da MESMA oferta têm chaves distintas (cada um abriu o link no seu browser).
	//
	// Fallback fail-safe: se o header vier ausente/vazio, geramos uma chave
	// aleatória por requisição — assim NUNCA caímos em "token-only" (que colapsaria
	// compradores distintos no mesmo pedido). Com chave aleatória a dedup não casa
	// e o advisory lock vira no-op inofensivo (1 lock por requisição).
	idemKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idemKey == "" {
		idemKey = randomIdemKey()
	}
	// Namespaceia por token para isolar a dedup entre ofertas diferentes.
	idemKey = req.Token + ":" + idemKey

	// ── Transação única para toda a criação ──────────────────────────────────
	tx, err := h.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		lg.Error("[checkout] falha ao iniciar transação", "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// 3b. Advisory lock por idemKey — serializa double-clicks concorrentes do mesmo cliente.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, advisoryLockKey(idemKey)); err != nil {
		lg.Error("[checkout] falha no advisory lock", "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// ── 4. Idempotência: pedido recente com mesmo idemKey ────────────────────
	var existingID int64
	var existingNumber string
	errIdem := tx.QueryRow(ctx,
		`SELECT o.id, o.order_number
		   FROM sz_orders o
		   JOIN sz_order_meta m ON m.order_id = o.id
		  WHERE m.meta_key = 'checkout_idem'
		    AND m.meta_value = $1
		    AND o.created_at > NOW() - INTERVAL '10 minutes'
		  ORDER BY o.id DESC
		  LIMIT 1`,
		idemKey,
	).Scan(&existingID, &existingNumber)
	if errIdem == nil && existingID > 0 {
		// Pedido já criado nesta janela — retorna o mesmo (idempotente).
		if err := tx.Commit(ctx); err != nil {
			lg.Error("[checkout] falha ao commitar (idempotente)", "err", err)
		}
		lg.Info("[checkout] pedido idempotente retornado",
			"order_id", existingID, "order_number", existingNumber, "token", req.Token)
		httpx.WriteOK(w, map[string]any{
			"order_id":      existingID,
			"order_number":  existingNumber,
			"tracking_code": signedTrackingCode(existingNumber), // code assinado anti-enumeração
			"idempotente":   true,
		})
		return
	}
	if errIdem != nil && errIdem != pgx.ErrNoRows {
		lg.Error("[checkout] falha na checagem de idempotência", "err", errIdem)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// ── 5. Gera order_number e insere sz_orders ──────────────────────────────
	var nextVal int64
	if err = tx.QueryRow(ctx,
		`SELECT nextval(pg_get_serial_sequence('sz_orders', 'id'))`,
	).Scan(&nextVal); err != nil {
		lg.Error("[checkout] falha ao gerar order_number", "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	orderNumber := fmt.Sprintf("SZ-%07d", nextVal)

	// FEAT-FRETE: subtotal = oferta, shipping = frete server-side (0 p/ motoboy),
	// total = subtotal + shipping. Para motoboy precoFrete=0 → comportamento idêntico.
	var orderID int64
	err = tx.QueryRow(ctx,
		// FEAT-AFF-COMMISSION: affiliate_amount (LÍQUIDA) e transaction_fee (take 4,99%)
		// gravados na criação. Sem afiliado: ambos = 0.00 (default histórico). A trigger
		// sz_revenue_capture_order LÊ estas colunas (não a tocamos) e escritura o take.
		//
		// AUDIT-2026-07-30 CRITICAL (dono): "PAD [expedição] não tem taxa de
		// transação" — delivery_fee (taxa fixa COD) e o desconto producer_tx_rate
		// no líquido do produtor SÓ se aplicam a COD/motoboy ($13). Antes o INSERT
		// era o MESMO pra ambos os modos: PAD estava sendo cobrado com a taxa de
		// entrega COD (23,98) fantasma E descontando taxa de transação do produtor
		// que não deveria existir pra esse fluxo. Motoboy (golden #1587) preserva
		// a fórmula byte-a-byte.
		`INSERT INTO sz_orders
		    (id, order_number, user_id, produtor_id, affiliate_id,
		     status, subtotal, shipping, total,
		     affiliate_amount, transaction_fee,
		     delivery_fee, producer_net,
		     payment_method, payment_status, currency,
		     customer_note, ip_address, user_agent,
		     created_at, updated_at)
		 OVERRIDING SYSTEM VALUE
		 VALUES ($1, $2, 0, $3, $4,
		         'pending', $5, $6, $7,
		         $11, $12,
		         CASE WHEN $13 THEN COALESCE((SELECT NULLIF(value,'')::numeric FROM senderzz_options WHERE name='sz_cod_delivery_fee'), 23.98)
		              ELSE 0 END,
		         CASE WHEN $13 THEN
		             GREATEST($7::numeric - ($11::numeric + $12::numeric)
		                 - COALESCE((SELECT NULLIF(value,'')::numeric FROM senderzz_options WHERE name='sz_cod_delivery_fee'), 23.98)
		                 - ROUND($7::numeric * sz_producer_tx_rate(), 2), 0)
		             ELSE
		             GREATEST($7::numeric - ($11::numeric + $12::numeric), 0)
		         END,
		         'cod', 'pending', 'BRL',
		         $8, $9, $10,
		         NOW(), NOW())
		 RETURNING id`,
		nextVal, orderNumber, producerID, affiliateID,
		subtotal.StringFixed(2),
		precoFrete.StringFixed(2),
		total.StringFixed(2),
		nullableStr(strings.TrimSpace(req.CustomerNote)),
		nullableStr(r.RemoteAddr),
		nullableStr(r.UserAgent()),
		affiliateAmount.StringFixed(2),
		transactionFee.StringFixed(2),
		isMotoboy,
	).Scan(&orderID)
	if err != nil {
		lg.Error("[checkout] falha ao inserir pedido", "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao criar pedido")
		return
	}

	// ── 6. Insere os itens do pedido ──────────────────────────────────────────
	// FEAT-CHECKOUT-MULTI-ITEM: quando o link tem composition_items (2+ produtos/
	// variações escolhidos pelo PRODUTOR na criação do link — o comprador não
	// escolhe nada aqui), insere UMA linha por produto da composição, dividindo o
	// subtotal proporcionalmente à quantidade (preço total do link continua o que o
	// produtor definiu; só rateamos entre as linhas p/ granularidade de relatório/
	// estoque). Sem composição (caso de sempre) mantém o comportamento antigo: 1
	// linha = a oferta inteira.
	type compLine struct {
		ProductID int64  `json:"product_id"`
		Qty       int    `json:"qty"`
		Variacao  string `json:"variacao"`
	}
	var comp []compLine
	if len(compRaw) > 0 {
		if err := json.Unmarshal(compRaw, &comp); err != nil {
			lg.Error("[checkout] composition_items inválido, ignorando", "token", req.Token, "err", err)
			comp = nil
		}
	}

	if len(comp) <= 1 {
		itemMeta, _ := json.Marshal(map[string]any{
			"checkout_link_token": req.Token,
			"checkout_post_id":    postID,
		})
		// FEAT-FRETE: o item é a OFERTA (1x), preço = subtotal (sem frete). O frete vai
		// na coluna sz_orders.shipping + meta, não no item. Motoboy: subtotal == total.
		_, err = tx.Exec(ctx,
			`INSERT INTO sz_order_items
			    (order_id, produto_id, nome, quantidade, preco_unit, subtotal, meta)
			 VALUES ($1, $2, $3, 1, $4, $4, $5)`,
			orderID, produtoID, linkName, subtotal.StringFixed(2), string(itemMeta),
		)
		if err != nil {
			lg.Error("[checkout] falha ao inserir item", "order_id", orderID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao inserir item do pedido")
			return
		}
	} else {
		totalQty := 0
		for _, c := range comp {
			if c.Qty < 1 {
				c.Qty = 1
			}
			totalQty += c.Qty
		}
		if totalQty <= 0 {
			totalQty = len(comp)
		}
		unit := subtotal.Div(decimal.NewFromInt(int64(totalQty))).Round(2)
		allocated := decimal.Zero
		for i, c := range comp {
			qty := c.Qty
			if qty < 1 {
				qty = 1
			}
			lineTotal := unit.Mul(decimal.NewFromInt(int64(qty)))
			if i == len(comp)-1 {
				// Última linha absorve o resto do arredondamento (soma bate com subtotal).
				lineTotal = subtotal.Sub(allocated)
			}
			allocated = allocated.Add(lineTotal)

			var lineProdutoID int64 = c.ProductID
			var mapped int64
			if errL := h.db.QueryRow(ctx,
				`SELECT COALESCE(wp_post_id, id) FROM sz_products WHERE id = sz_resolve_product_id($1)`, c.ProductID,
			).Scan(&mapped); errL == nil && mapped > 0 {
				lineProdutoID = mapped
			}

			lineName := linkName
			if strings.TrimSpace(c.Variacao) != "" {
				lineName = linkName + " — " + strings.TrimSpace(c.Variacao)
			}
			lineMeta, _ := json.Marshal(map[string]any{
				"checkout_link_token": req.Token,
				"checkout_post_id":    postID,
				"variacao":            c.Variacao,
			})
			if _, err = tx.Exec(ctx,
				`INSERT INTO sz_order_items
				    (order_id, produto_id, nome, quantidade, preco_unit, subtotal, meta)
				 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
				orderID, lineProdutoID, lineName, qty,
				lineTotal.Div(decimal.NewFromInt(int64(qty))).Round(2), lineTotal.StringFixed(2), string(lineMeta),
			); err != nil {
				lg.Error("[checkout] falha ao inserir item da composição", "order_id", orderID, "err", err)
				httpx.WriteErr(w, http.StatusInternalServerError, "erro ao inserir item do pedido")
				return
			}
		}
	}

	// ── 7. Insere o endereço de envio (com numero!) ──────────────────────────
	uf := strings.ToUpper(strings.TrimSpace(req.UF))
	if len(uf) > 2 {
		uf = uf[:2]
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO sz_order_addresses
		    (order_id, tipo, nome, email, telefone, cep,
		     logradouro, numero, complemento, bairro, cidade, uf, pais)
		 VALUES ($1, 'shipping', $2, $3, $4, $5,
		         $6, $7, $8, $9, $10, $11, 'BR')`,
		orderID,
		nullableStr(strings.TrimSpace(req.Nome)),
		nullableStr(strings.TrimSpace(req.Email)),
		nullableStr(onlyDigits(req.Telefone)),
		nullableStr(onlyDigits(req.CEP)),
		nullableStr(strings.TrimSpace(req.Logradouro)),
		nullableStr(strings.TrimSpace(req.Numero)),
		nullableStr(strings.TrimSpace(req.Complemento)),
		nullableStr(strings.TrimSpace(req.Bairro)),
		nullableStr(strings.TrimSpace(req.Cidade)),
		nullableStr(uf),
	)
	if err != nil {
		lg.Error("[checkout] falha ao inserir endereço", "order_id", orderID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao inserir endereço")
		return
	}

	// ── 8. Meta: CPF (opcional), telefone, idempotência e token ──────────────
	// sz_order_addresses NÃO tem coluna de CPF — quando informado, gravamos em meta
	// como _billing_cpf (mesma convenção do WP, vista em sz_order_meta).
	// CHECKOUT-NO-CPF (2026-06-18): _billing_cpf só é gravado se o cliente informou
	// CPF (condicional, igual ao _billing_cellphone). Sem CPF não há meta → o rastreio
	// público simplesmente não exibe a linha CPF (ver tracking.go: cliCPF fica nil).
	// CANAL do pedido (BUG-FIX 2026-06-24): motoboy = COD | correio = expedição. É o
	// sinal CANÔNICO que o filtro da Expedição (expedicao.go) usa p/ EXCLUIR pedidos COD.
	// Antes o filtro dependia do nome do item conter "— Motoboy"; como o item passou a
	// guardar a BASE limpa (sem sufixo), pedidos COD vazavam pra Expedição. Gravar o
	// delivery_mode explícito resolve de vez (independe do nome).
	deliveryMode := "correio"
	if isMotoboy {
		deliveryMode = "motoboy"
	}
	metaPairs := [][2]string{
		{"checkout_idem", idemKey},
		{"checkout_token", req.Token},
		{"_senderzz_delivery_mode", deliveryMode},
	}
	if cpfDigits != "" {
		metaPairs = append(metaPairs, [2]string{"_billing_cpf", cpfDigits})
	}
	if tel := onlyDigits(req.Telefone); tel != "" {
		metaPairs = append(metaPairs, [2]string{"_billing_cellphone", tel})
	}
	// Agendamento: grava também em meta para rastreabilidade no painel de pedidos.
	if schedZona != nil {
		metaPairs = append(metaPairs,
			[2]string{"_sz_delivery_date", schedData},
			[2]string{"_sz_delivery_tipo", schedTipo},
		)
	}
	// FEAT-FRETE: linha de frete (correio). Grava a opção CASADA server-side —
	// id/empresa/serviço/preço/prazo — em meta (a coluna sz_orders.shipping já tem
	// o preço). Rastreabilidade no painel + reemissão de etiqueta.
	if freteEscolha != nil {
		estimado := "0"
		if freteEscolha.Estimated {
			estimado = "1"
		}
		// _sz_freight_locked: "1" só quando o produtor tem frete travado (fixo por
		// transportadora e/ou bloqueio de Correios — freight.go applyFixedFreight/
		// applyCorreiosLock). AUDIT-2026-07-27: sem essa distinção, a emissão (emit.go)
		// não tem como saber se _sz_freight_price é o preço RAW da ME (produtor paga
		// o custo real) ou o preço FIXO/MARCADO prometido ao cliente (que já inclui
		// markup — cobrar isso do produtor sem querer é overcharge sistêmico). Ver
		// mesmo bug em emit.go: FreightPrice só sobrescreve o débito da carteira
		// quando travado; sem trava, carteira usa o preço RAW re-cotado na hora,
		// igual sempre foi (a margem do markup é receita da plataforma, não custo
		// repassado ao produtor).
		locked := "0"
		if freteEscolha.Locked {
			locked = "1"
		}
		metaPairs = append(metaPairs,
			[2]string{"_sz_freight_id", freteEscolha.ID},
			[2]string{"_sz_freight_company", freteEscolha.Company},
			[2]string{"_sz_freight_service", freteEscolha.Service},
			[2]string{"_sz_freight_price", precoFrete.StringFixed(2)},
			[2]string{"_sz_freight_days", strconv.Itoa(freteEscolha.DeliveryDays)},
			[2]string{"_sz_freight_estimated", estimado},
			[2]string{"_sz_freight_locked", locked},
		)
	}
	for _, kv := range metaPairs {
		if _, err = tx.Exec(ctx,
			`INSERT INTO sz_order_meta (order_id, meta_key, meta_value)
			 VALUES ($1, $2, $3)
			 ON CONFLICT (order_id, meta_key) DO UPDATE SET meta_value = EXCLUDED.meta_value`,
			orderID, kv[0], kv[1],
		); err != nil {
			lg.Error("[checkout] falha ao gravar meta", "order_id", orderID, "key", kv[0], "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao gravar dados do pedido")
			return
		}
	}

	// ── 9. Histórico inicial ─────────────────────────────────────────────────
	_, err = tx.Exec(ctx,
		`INSERT INTO sz_order_status_history
		    (order_id, status_de, status_para, motivo, actor_id, actor_tipo, created_at)
		 VALUES ($1, NULL, 'pending', 'pedido criado via checkout link', NULL, 'sistema', NOW())`,
		orderID,
	)
	if err != nil {
		lg.Error("[checkout] falha ao registrar histórico inicial", "order_id", orderID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// ── 9b. Consentimento LGPD (FEAT-LGPD) ───────────────────────────────────
	// Registrado em DUAS frentes, ambas dentro da MESMA transação do pedido:
	//
	//   (a) sz_order_meta — a PROVA por-pedido ("vinculado ao pedido"): versão,
	//       timestamp e ip carimbados server-side. É o registro primário, pois o
	//       checkout é ANÔNIMO (sz_orders.user_id = 0) e a tabela de consents não
	//       tem coluna order_id. O painel/auditoria lê daqui o consentimento do pedido.
	//
	//   (b) senderzz_consents — ledger coarse "esta versão da política foi aceita"
	//       (a task pede explicitamente "em senderzz_consents"). user_id = 0 (cliente
	//       anônimo do checkout — NUNCA um wp_user_id, que colidiria com usuários
	//       reais). A UNIQUE (user_id, doc_type, doc_version) faz com que aceites
	//       repetidos da MESMA versão por clientes anônimos colapsem numa só linha —
	//       por isso usamos ON CONFLICT … DO UPDATE para refrescar accepted_at/ip/ua
	//       sem estourar o pedido. A vinculação fina ao pedido vive em (a).
	//
	// Fail-closed no INPUT (validado acima), mas NÃO bloqueia pedidos sem consent.
	if recordConsent {
		nowSrc := []struct{ k, v string }{
			{"_sz_consent_privacy", "1"},
			{"_sz_consent_doc_type", consentDocType},
			{"_sz_consent_doc_version", consentDocVer},
			{"_sz_consent_ip", clientIP(r)},
		}
		for _, kv := range nowSrc {
			if _, err = tx.Exec(ctx,
				`INSERT INTO sz_order_meta (order_id, meta_key, meta_value)
				 VALUES ($1, $2, $3)
				 ON CONFLICT (order_id, meta_key) DO UPDATE SET meta_value = EXCLUDED.meta_value`,
				orderID, kv.k, kv.v,
			); err != nil {
				lg.Error("[checkout] falha ao gravar meta de consentimento",
					"order_id", orderID, "key", kv.k, "err", err)
				httpx.WriteErr(w, http.StatusInternalServerError, "erro ao registrar consentimento")
				return
			}
		}
		// _sz_consent_accepted_at: carimbo server-side. Gravado num INSERT próprio com
		// NOW() para que o valor venha do banco (nunca do cliente).
		if _, err = tx.Exec(ctx,
			`INSERT INTO sz_order_meta (order_id, meta_key, meta_value)
			 VALUES ($1, '_sz_consent_accepted_at', to_char(NOW(), 'YYYY-MM-DD"T"HH24:MI:SSOF'))
			 ON CONFLICT (order_id, meta_key) DO UPDATE SET meta_value = EXCLUDED.meta_value`,
			orderID,
		); err != nil {
			lg.Error("[checkout] falha ao gravar timestamp de consentimento",
				"order_id", orderID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao registrar consentimento")
			return
		}

		// Ledger senderzz_consents (user_id=0 = cliente anônimo do checkout).
		if _, err = tx.Exec(ctx,
			`INSERT INTO senderzz_consents (user_id, doc_type, doc_version, accepted_at, ip, user_agent)
			 VALUES (0, $1, $2, NOW(), $3, $4)
			 ON CONFLICT (user_id, doc_type, doc_version)
			 DO UPDATE SET accepted_at = EXCLUDED.accepted_at,
			               ip = EXCLUDED.ip,
			               user_agent = EXCLUDED.user_agent`,
			consentDocType, consentDocVer,
			nullableStr(clientIP(r)),
			nullableStr(r.UserAgent()),
		); err != nil {
			lg.Error("[checkout] falha ao gravar consentimento em senderzz_consents",
				"order_id", orderID, "doc_type", consentDocType, "doc_version", consentDocVer, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao registrar consentimento")
			return
		}
	}

	// ── 10. Fila operacional motoboy (apenas ofertas motoboy com agendamento) ─
	// Espelha sz_motoboy_criar_pedido() (router.php): cria a linha em
	// sz_motoboy_pedidos com data_entrega = data escolhida e status = 'agendado'
	// (agendamento) ou 'pre_agendado'. motoboy_id nasce NULL (v349 — a escolha é
	// do OL/admin). wc_order_id = id do pedido (chave natural confirmada: o range
	// de wc_order_id em sz_motoboy_pedidos coincide 1:1 com sz_orders.id).
	//
	// valor_pedido = total (valor que o motoboy cobra no COD).
	// valor_taxa   = default operacional 25.00 (= sz_motoboy_taxa_entrega no WP;
	//                o orders-service não lê wp_options).
	if schedZona != nil {
		ufMb := uf
		_, err = tx.Exec(ctx,
			`INSERT INTO sz_motoboy_pedidos
			    (wc_order_id, cd_id, zona_id, motoboy_id, status,
			     dest_nome, dest_telefone, dest_cep,
			     dest_endereco, dest_numero, dest_complemento,
			     dest_bairro, dest_cidade, dest_uf,
			     dest_produto, quantidade,
			     valor_pedido, valor_taxa,
			     data_entrega, ts_aprovado, created_at, updated_at)
			 VALUES ($1, $2, $3, NULL, $4,
			         $5, $6, $7,
			         $8, $9, $10,
			         $11, $12, $13,
			         $14, $15,
			         $16, '25.00',
			         $17, NOW(), NOW(), NOW())
			 ON CONFLICT (wc_order_id) DO NOTHING`,
			nextVal, schedZona.CDID, schedZona.ZonaID, schedTipo,
			nullableStr(strings.TrimSpace(req.Nome)),
			nullableStr(onlyDigits(req.Telefone)),
			onlyDigits(req.CEP), // dest_cep NOT NULL
			nullableStr(strings.TrimSpace(req.Logradouro)),
			nullableStr(strings.TrimSpace(req.Numero)),
			nullableStr(strings.TrimSpace(req.Complemento)),
			nullableStr(strings.TrimSpace(req.Bairro)),
			nullableStr(strings.TrimSpace(req.Cidade)),
			nullableStr(ufMb),
			nullableStr(linkName), // dest_produto: nome da oferta (1x)
			1,                     // quantidade: 1x a oferta do link
			total.StringFixed(2),
			schedData,
		)
		if err != nil {
			lg.Error("[checkout] falha ao criar pedido motoboy", "order_id", orderID, "wc_order_id", nextVal, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao agendar entrega")
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		lg.Error("[checkout] falha ao commitar criação do pedido", "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	lg.Info("[checkout] pedido COD criado",
		"order_id", orderID,
		"order_number", orderNumber,
		"produtor_id", producerID,
		"total", total.StringFixed(2),
		"token", req.Token,
		"data_entrega", schedData,
		"agendamento_tipo", schedTipo,
	)

	resp := map[string]any{
		"order_id":      orderID,
		"order_number":  orderNumber,
		"tracking_code": signedTrackingCode(orderNumber), // code assinado p/ link de rastreio anti-enumeração
	}
	if schedZona != nil {
		resp["data_entrega"] = schedData
		resp["tipo"] = schedTipo // 'agendado' | 'pre_agendado'
		// Front-end deve exibir popup quando pre_agendado (regra de confirmação).
		resp["requer_confirmacao"] = (schedTipo == "pre_agendado")
	}
	httpx.WriteOK(w, resp)
}

// resolveProductImage tenta resolver a URL da imagem (thumbnail) de um produto
// pelo post_id do link de checkout. O post_id do link é COALESCE(sp.wp_post_id,
// sp.id) (links_portal.go), então casamos pela MESMA chave efetiva — para produtos
// NATIVOS (Go, wp_post_id NULL) o link traz post_id=sp.id e o lookup por wp_post_id
// sozinho não casaria (a imagem cadastrada no admin não apareceria no checkout).
//
// IMPORTANTE: não há coluna de imagem em sz_products nem tabela espelho de
// attachments/postmeta no Postgres — a imagem real do produto é um attachment do
// WooCommerce (get_image_id/wp_get_attachment_image_url), que vive só no WP.
// Como rede de segurança para um sync futuro, lemos sz_products.meta (jsonb) e
// aceitamos as chaves usuais de imagem. Se nada existir, devolvemos "" (a task
// permite explicitamente image_url=” quando não houver fonte).
//
// Best-effort: qualquer erro de banco vira "" e é logado em nível debug — nunca
// propaga para o caller.
func (h *CheckoutHandler) resolveProductImage(ctx context.Context, postID int64) string {
	if postID <= 0 {
		return ""
	}
	// Procura em sz_products.meta as chaves de imagem mais comuns (a primeira não vazia
	// vence). COALESCE devolve NULL se nenhuma existir; tratamos como "".
	var url *string
	err := h.db.QueryRow(ctx,
		`SELECT COALESCE(
		          NULLIF(meta->>'image_url', ''),
		          NULLIF(meta->>'thumb_url', ''),
		          NULLIF(meta->>'thumbnail', ''),
		          NULLIF(meta->>'imagem', ''),
		          NULLIF(meta->>'image', '')
		        )
		   FROM sz_products
		  WHERE COALESCE(wp_post_id, id) = $1
		  LIMIT 1`,
		postID,
	).Scan(&url)
	if err != nil {
		if err != pgx.ErrNoRows {
			slog.Debug("[checkout] imagem do produto indisponível", "post_id", postID, "err", err)
		}
		return ""
	}
	if url == nil {
		return ""
	}
	return strings.TrimSpace(*url)
}

// resolveQtySellable lê (AVISO, não bloqueio) a disponibilidade vendável de um
// produto somada em TODOS os seus CDs: SUM(qty_available - qty_reserved).
//
// FEAT-STOCK. Regra do dono (não-destrutiva):
//   - Produto SEM nenhuma linha em sz_stock ⇒ retorna nil ⇒ tratado como ILIMITADO
//     (o campo é omitido no JSON; a venda NUNCA é bloqueada por falta de cadastro).
//   - Existe linha e o somatório vendável <= 0 ⇒ retorna 0 (esgotado — só aviso).
//   - Caso contrário ⇒ retorna o total vendável (>0).
//
// CHAVE DE PRODUTO: precisa casar com a chave usada na CRIAÇÃO do pedido. Em
// PostOrder, sz_order_items.produto_id = sz_products.id (mapeado por wp_post_id),
// com fallback = post_id. O gatilho de estoque (schema-fixes-v471-stock.sql)
// popula sz_stock.product_id a partir de sz_order_items.produto_id — logo aqui
// resolvemos o produto_id da MESMA forma, senão o lookup nunca casaria a linha.
//
// Best-effort: qualquer erro de banco vira nil (ilimitado) e é logado em debug —
// nunca propaga para o caller nem bloqueia a oferta.
func (h *CheckoutHandler) resolveQtySellable(ctx context.Context, postID int64) *int64 {
	if postID <= 0 {
		return nil
	}

	// ESTOQUE CANÔNICO POR PRODUTO (migração 480): resolve a chave (post_id da oferta,
	// wp_post_id ou sz_products.id) para o sz_products.id canônico via sz_resolve_product_id.
	// Assim ofertas COD (post órfão, ex. 1075) e FF (wp_post_id) do MESMO produto leem o
	// MESMO pool. Degrada ao post_id cru se a função ainda não existe (best-effort).
	produtoID := postID
	var canonID int64
	if err := h.db.QueryRow(ctx, `SELECT sz_resolve_product_id($1)`, postID).Scan(&canonID); err == nil && canonID > 0 {
		produtoID = canonID
	}

	// SUM devolve NULL quando NÃO há linha ⇒ sellable fica nil ⇒ ilimitado.
	// COUNT distingue "sem cadastro" (0 ⇒ ilimitado) de "cadastrado e zerado" (>0 ⇒ esgotado),
	// pois um SUM clampado a 0 por si só não permite separar os dois casos.
	// Soma pelas DUAS chaves do produto (sz_products.id E wp_post_id) — o estoque vivo
	// pode estar gravado por qualquer uma; posts órfãos/fantasma ficam de fora.
	var (
		sellable *int64
		rows     int64
	)
	err := h.db.QueryRow(ctx,
		`SELECT SUM(qty_available - qty_reserved)::bigint, COUNT(*)::bigint
		   FROM sz_stock
		  WHERE product_id = $1
		     OR product_id = (SELECT wp_post_id FROM sz_products WHERE id = $1)`,
		produtoID,
	).Scan(&sellable, &rows)
	if err != nil {
		slog.Debug("[checkout] estoque indisponível (tratado como ilimitado)", "post_id", postID, "produto_id", produtoID, "err", err)
		return nil
	}
	if rows == 0 || sellable == nil {
		// Sem linha cadastrada ⇒ ilimitado (campo omitido).
		return nil
	}
	// Existe linha: nunca devolve negativo — clampa em 0 (esgotado).
	if *sellable < 0 {
		zero := int64(0)
		return &zero
	}
	return sellable
}

// clientIP devolve o IP do cliente para registro de consentimento (FEAT-LGPD).
// O router aplica chimw.RealIP, então r.RemoteAddr já reflete o IP real (X-Forwarded-For/
// X-Real-IP resolvidos). Tira a porta quando presente (host:port) e limita a 64 chars
// (coluna senderzz_consents.ip VARCHAR(64)). Best-effort — nunca falha o pedido.
func clientIP(r *http.Request) string {
	addr := strings.TrimSpace(r.RemoteAddr)
	if addr == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(addr); err == nil && host != "" {
		addr = host
	}
	if len(addr) > 64 {
		addr = addr[:64]
	}
	return addr
}

// advisoryLockKey deriva uma chave int64 estável a partir da string de idempotência
// para uso em pg_advisory_xact_lock(bigint). FNV-64 garante distribuição uniforme.
func advisoryLockKey(s string) int64 {
	hsh := fnv.New64a()
	_, _ = hsh.Write([]byte(s))
	return int64(hsh.Sum64()) //nolint:gosec — wrap intencional para caber em bigint
}

// subtotalFloat converte um decimal de valor (R$) para float64 — usado para passar
// o valor declarado da mercadoria ao freight.Calculate (seguro). FEAT-FRETE.
func subtotalFloat(d decimal.Decimal) float64 {
	f, _ := d.Float64()
	return f
}

// randomIdemKey gera uma chave de idempotência aleatória, usada como FALLBACK
// quando o header Idempotency-Key vem ausente/vazio (CHECKOUT-NO-CPF, 2026-06-18).
// Uma chave aleatória por requisição garante que a dedup nunca colapse compradores
// distintos da mesma oferta — o pior caso é não deduplicar um double-click sem header.
// 16 bytes de crypto/rand em hex; em caso de erro improvável de leitura, usa o
// relógio (não-criptográfico, mas suficiente para unicidade prática).
func randomIdemKey() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("idem-%d", time.Now().UnixNano())
	}
	return "idem-" + hex.EncodeToString(b[:])
}
