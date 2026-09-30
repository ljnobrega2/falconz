// freight.go — Cotação de frete do checkout nativo (ofertas tipo correio).
//
// FEAT-FRETE. Endpoint público (sem JWT), irmão do /schedule (motoboy):
//
//	GET /checkout-api/freight?token=<t>&cep=<dest_cep>
//	  → 200 {ok:true, origin_cep, options:[{id,company,service,price,delivery_days,estimated}]}
//	  → oferta MOTOBOY → {ok:true, options:[]}  (motoboy não usa frete; agenda via /schedule)
//	  → token/cep inválidos → 4xx {ok:false, erro}
//
// O CÁLCULO em si vive em internal/freight (cliente ME + fallback estimado). Aqui
// só resolvemos as ENTRADAS server-side (origem, dimensões do produto) e formatamos
// a resposta. A MESMA resolução é reusada pelo POST /order na re-cotação (CRIT-01).
package handlers

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/senderzz/orders-service/internal/freight"
	"github.com/senderzz/orders-service/internal/httpx"
)

// defaultOrigemCEP é o CEP de origem usado quando o produtor/CD não tem CEP
// cadastrado (regra da task: estimativa a partir de SP). 01001000 = Praça da Sé/SP.
const defaultOrigemCEP = "01001000"

// reCEP8 extrai um CEP de 8 dígitos de um texto livre (ex.: endereço do CD).
var reCEP8 = regexp.MustCompile(`\b(\d{5})-?(\d{3})\b`)

// freightOptionJSON é o item de opção no contrato do endpoint.
type freightOptionJSON struct {
	ID           string  `json:"id"`
	Company      string  `json:"company"`
	Service      string  `json:"service"`
	Price        float64 `json:"price"`
	DeliveryDays int     `json:"delivery_days"`
	Estimated    bool    `json:"estimated"`
	Preferred    bool    `json:"preferred"`
	// Locked: frete travado pelo produtor (fixo por transportadora e/ou bloqueio
	// de Correios) — front esconde o seletor e só aplica o preço, sem escolha.
	Locked bool `json:"locked"`
}

// GetFreight cota o frete de uma oferta tipo correio para o CEP de destino.
func (h *CheckoutHandler) GetFreight(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	destCEP := onlyDigits(r.URL.Query().Get("cep"))
	if token == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "token é obrigatório")
		return
	}
	if len(destCEP) != 8 {
		httpx.WriteErr(w, http.StatusBadRequest, "CEP inválido (8 dígitos)")
		return
	}

	ctx := r.Context()

	// Resolve a oferta (tipo + dados para origem/dimensões).
	var (
		producerID int64
		postID     int64
		tipo       string
		offerName  string
	)
	err := h.db.QueryRow(ctx,
		`SELECT producer_id, post_id, tipo, name
		   FROM senderzz_checkout_links
		  WHERE token = $1
		  LIMIT 1`,
		token,
	).Scan(&producerID, &postID, &tipo, &offerName)
	if err == pgx.ErrNoRows {
		httpx.WriteErr(w, http.StatusNotFound, "oferta não encontrada")
		return
	}
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao buscar oferta")
		return
	}

	// Motoboy não usa frete — devolve lista vazia (o front cai no fluxo /schedule).
	if strings.EqualFold(strings.TrimSpace(tipo), "motoboy") {
		httpx.WriteOK(w, map[string]any{"options": []any{}})
		return
	}

	// Entradas server-side: origem (CD/produtor → default SP) e dimensões do produto.
	origemCEP := h.resolveOrigemCEP(ctx, producerID)
	pkg := h.resolvePackage(ctx, producerID, postID, offerName)

	opts, errCalc := freight.Calculate(ctx, origemCEP, destCEP, pkg)
	if errCalc != nil {
		// Calculate só erra com entrada inválida (CEP destino) — já validado acima,
		// mas mantemos o 422 defensivo.
		httpx.WriteErr(w, http.StatusUnprocessableEntity, errCalc.Error())
		return
	}
	opts = h.applyCarrierPreferences(ctx, producerID, opts)
	opts = h.applyFreightMarkup(ctx, postID, opts)
	opts = h.applyFixedFreight(ctx, producerID, opts)
	opts = h.applyCorreiosLock(ctx, producerID, opts)

	out := make([]freightOptionJSON, 0, len(opts))
	for _, o := range opts {
		out = append(out, freightOptionJSON{
			ID:           o.ID,
			Company:      o.Company,
			Service:      o.Service,
			Price:        o.Price,
			DeliveryDays: o.DeliveryDays,
			Estimated:    o.Estimated,
			Preferred:    o.Preferred,
			Locked:       o.Locked,
		})
	}

	httpx.WriteOK(w, map[string]any{
		"origin_cep": origemCEP,
		"options":    out,
	})
}

// ── GET /checkout-api/resolve (FEAT-LINK-MISTO) ─────────────────────────────
//
// Só faz sentido pra oferta tipo='misto': o front chama isso ao digitar o CEP
// pra descobrir se o pedido vai por COD (motoboy) ou Expedição (correio), SEM
// o cliente escolher (pedido dono 2026-07-27). Pra tipo != 'misto' devolve o
// modo fixo do link (atalho, evita o front precisar de um caso especial).
//
// A mesma resolução (zona cobre o CEP + tem data ofertável) é usada aqui, no
// GetSchedule (quando o front decide seguir motoboy) e no PostOrder (CRIT-01 —
// nunca confia no que o front decidiu, resolve de novo server-side).
func (h *CheckoutHandler) GetResolveMode(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	cep := onlyDigits(r.URL.Query().Get("cep"))
	if token == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "token é obrigatório")
		return
	}
	if len(cep) != 8 {
		httpx.WriteErr(w, http.StatusBadRequest, "CEP inválido (8 dígitos)")
		return
	}

	ctx := r.Context()
	var tipo string
	err := h.db.QueryRow(ctx,
		`SELECT tipo FROM senderzz_checkout_links WHERE token = $1 LIMIT 1`, token,
	).Scan(&tipo)
	if err == pgx.ErrNoRows {
		httpx.WriteErr(w, http.StatusNotFound, "oferta não encontrada")
		return
	}
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao buscar oferta")
		return
	}

	tipoNorm := strings.ToLower(strings.TrimSpace(tipo))
	if tipoNorm != "misto" {
		mode := "expedicao"
		if tipoNorm == "motoboy" {
			mode = "motoboy"
		}
		httpx.WriteOK(w, map[string]any{"mode": mode})
		return
	}

	mode := "expedicao"
	zona, errZ := resolveZonaPorCEP(ctx, h.db, cep)
	if errZ == nil && zona != nil {
		datas, errD := computeOfferableDates(ctx, h.db, zona)
		if errD == nil && len(datas) > 0 {
			mode = "motoboy"
		}
	}
	httpx.WriteOK(w, map[string]any{"mode": mode})
}

// ── Resolução de entradas server-side (reusada pelo POST /order) ─────────────

// resolveOrigemCEP resolve o CEP de origem do frete a partir do produtor.
//
// Não há coluna de CEP de origem no schema (sz_motoboy_cds só tem `endereco` livre,
// portal_users não tem CEP). Best-effort: tenta extrair um CEP de 8 dígitos do
// endereço de algum CD ativo (qualquer CD — não há vínculo produtor↔CD confiável
// nesta base). Se nada casar, usa o default SP (01001000). Nunca falha.
func (h *CheckoutHandler) resolveOrigemCEP(ctx context.Context, producerID int64) string {
	var endereco *string
	err := h.db.QueryRow(ctx,
		`SELECT endereco
		   FROM sz_motoboy_cds
		  WHERE ativo = TRUE AND endereco IS NOT NULL AND endereco <> ''
		  ORDER BY id
		  LIMIT 1`,
	).Scan(&endereco)
	if err == nil && endereco != nil {
		if m := reCEP8.FindStringSubmatch(*endereco); m != nil {
			return m[1] + m[2] // 8 dígitos
		}
	}
	return defaultOrigemCEP
}

// resolvePackage resolve as dimensões do volume da oferta.
//
// O link de checkout NÃO aponta para um produto específico (post_id = página de
// checkout do WP, não o produto). Resolvemos o produto best-effort pelo NOME da
// oferta dentro do mesmo produtor — quando casa, usamos altura/largura/comprimento/
// peso reais (sz_products, v469). Sem casamento (caso comum nesta base), o
// freight.Calculate aplica o default de encomenda pequena (16×11×2 cm, 0,3 kg).
// freightInsuranceValueDefault — valor declarado (seguro) padronizado no ME,
// pedido do dono (2026-07-24): fixo, não escala com o preço do produto. Antes
// GET /freight cotava com seguro 0 (resolvePackage nunca setava ValorDeclarado)
// e POST /order recotava com seguro = subtotal do pedido — preços DIVERGIAM
// entre o que o cliente via no checkout e o que era efetivamente cobrado/gravado
// (ex.: pedido 1638: checkout mostrou R$19,24, sz_orders.shipping gravou
// R$20,22). Fixar aqui (fonte única, os dois callers usam resolvePackage)
// resolve a divergência e cumpre o padrão pedido.
const freightInsuranceValueDefault = 99.0

func (h *CheckoutHandler) resolvePackage(ctx context.Context, producerID, postID int64, offerName string) freight.Package {
	pkg := freight.Package{ValorDeclarado: freightInsuranceValueDefault}

	// 1) Tenta mapear o produto por wp_post_id (raro: post_id costuma ser a página).
	if postID > 0 {
		if h.scanDims(ctx, `SELECT altura, largura, comprimento, peso FROM sz_products WHERE wp_post_id = $1 LIMIT 1`, &pkg, postID) {
			return pkg
		}
	}

	// 2) Casa pelo NOME da oferta dentro do produtor (ILIKE, ignora trailing junk).
	name := normalizeOfferName(offerName)
	if name != "" && producerID > 0 {
		if h.scanDims(ctx,
			`SELECT altura, largura, comprimento, peso
			   FROM sz_products
			  WHERE produtor_id = $1 AND nome ILIKE $2
			  ORDER BY id
			  LIMIT 1`,
			&pkg, producerID, name+"%") {
			return pkg
		}
	}

	// 3) Sem dimensões → pkg fica zerado (exceto ValorDeclarado); o Calculate
	// normaliza p/ default pequeno.
	return pkg
}

// scanDims preenche pkg com as dimensões da query (4 colunas NUMERIC nullable).
// Retorna true se achou a linha (mesmo com algumas dimensões NULL — o normalize
// do freight cobre buracos). false em ErrNoRows/erro.
func (h *CheckoutHandler) scanDims(ctx context.Context, q string, pkg *freight.Package, args ...any) bool {
	var alt, larg, comp, peso *float64
	if err := h.db.QueryRow(ctx, q, args...).Scan(&alt, &larg, &comp, &peso); err != nil {
		return false
	}
	if alt != nil {
		pkg.Altura = *alt
	}
	if larg != nil {
		pkg.Largura = *larg
	}
	if comp != nil {
		pkg.Comprimento = *comp
	}
	if peso != nil {
		pkg.Peso = *peso
	}
	return true
}

// normalizeOfferName limpa o nome da oferta p/ casar com sz_products.nome.
// Remove caracteres não alfanuméricos do fim (a base tem nomes como "1 Pote Padrão`"
// com crase espúria) e espaços nas bordas.
func normalizeOfferName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimRight(s, "`'\"´ -_.")
	return strings.TrimSpace(s)
}

// ── Markup de frete (senderzz_markup_default / senderzz_markup_rules) ──────────
// Mesma fonte de configuração do admin (go/admin/internal/handlers/expedicao_integracoes.go
// GetMarkup/PreviewMarkup) — orders-service não importa admin-service (módulo Go
// distinto), então a leitura é duplicada aqui, MESMA fórmula:
//
//	final = (base * (1 + pct/100)) + fixed
//
// Sem regra para a classe do produtor → usa o par default. Sem option nenhuma
// gravada → defaults do sistema (20% / R$3,99), idênticos ao admin.
const (
	freightMarkupDefaultPct   = 20.0
	freightMarkupDefaultFixed = 3.99
)

type freightMarkupPair struct {
	Pct   float64
	Fixed float64
}

func parseFreightMarkupFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f
	default:
		return 0
	}
}

// freightMarkupDefault lê a option senderzz_markup_default ({"pct","fixed"}).
func (h *CheckoutHandler) freightMarkupDefault(ctx context.Context) freightMarkupPair {
	raw := strings.TrimSpace(h.optionRawGeneric(ctx, "senderzz_markup_default"))
	if raw != "" {
		var pair struct {
			Pct   any `json:"pct"`
			Fixed any `json:"fixed"`
		}
		if err := json.Unmarshal([]byte(raw), &pair); err == nil {
			return freightMarkupPair{
				Pct:   parseFreightMarkupFloat(pair.Pct),
				Fixed: parseFreightMarkupFloat(pair.Fixed),
			}
		}
	}
	return freightMarkupPair{Pct: freightMarkupDefaultPct, Fixed: freightMarkupDefaultFixed}
}

// freightMarkupForClass lê senderzz_markup_rules[class_id], senão usa o default.
func (h *CheckoutHandler) freightMarkupForClass(ctx context.Context, classID int) freightMarkupPair {
	def := h.freightMarkupDefault(ctx)
	if classID <= 0 {
		return def
	}
	raw := strings.TrimSpace(h.optionRawGeneric(ctx, "senderzz_markup_rules"))
	if raw == "" {
		return def
	}
	var asObj map[string]struct {
		Pct   any `json:"pct"`
		Fixed any `json:"fixed"`
	}
	if err := json.Unmarshal([]byte(raw), &asObj); err != nil {
		return def
	}
	if v, ok := asObj[strconv.Itoa(classID)]; ok {
		pair := freightMarkupPair{Pct: parseFreightMarkupFloat(v.Pct), Fixed: parseFreightMarkupFloat(v.Fixed)}
		if pair.Pct > 0 || pair.Fixed > 0 {
			return pair
		}
	}
	return def
}

// applyFreightMarkup aplica o markup (pct/fixed configurado NO PRODUTO — postID —
// com fallback pro default global) em cima do preço CRU da transportadora. Chamado
// tanto no GET /freight (cotação exibida) quanto no POST /order (re-cotação
// CRIT-01) — mesma fonte, preço exibido sempre bate com o cobrado.
//
// AUDIT-2026-07-17: antes resolvia por senderzz_portal_users.shipping_class_id
// (1 config por PRODUTOR, ignorando qual produto da oferta). Trocado pra POR
// PRODUTO (postID = senderzz_checkout_links.post_id, chave real do produto na
// oferta) — espelha go/admin ExpedicaoIntegracoes.tsx, que agora configura markup
// por produto, não por "classe de entrega" solta. senderzz_markup_rules manteve o
// mesmo shape (map "<id>" → {pct,fixed}); a chave passou a ser product_id.
func (h *CheckoutHandler) applyFreightMarkup(ctx context.Context, postID int64, opts []freight.ServiceOption) []freight.ServiceOption {
	pair := h.freightMarkupForClass(ctx, int(postID))
	for i := range opts {
		final := (opts[i].Price * (1 + pair.Pct/100.0)) + pair.Fixed
		opts[i].Price = math.Round(final*100) / 100
	}
	return opts
}

// ── Frete fixo por transportadora, POR PRODUTOR (pedido dono 2026-07-27) ───────
//
// "FIXAR VALOR DE FRETE (POR PRODUTOR) POR CORREIOS E DEMAIS TRANSPORTADORAS
// EX: SE FOR CORREIOS FICA 25 SE FOR DEMAIS TRANSPORTADORAS 32 MAS
// PERSONALIZAVEL POR PRODUTOR" — preço ABSOLUTO, substitui (não soma) o markup
// pct/fixed quando o produtor tem override configurado. Guardado em
// senderzz_portal_users.settings->>'frete_fixo_correios' / 'frete_fixo_outras'
// (mesmo padrão de expedicao_ativa, sem migration — ver producers.go Update).
//
// Aplicado SÓ aqui no checkout (GET /freight e a re-cotação de POST /order,
// mesma fonte — CRIT-01). A emissão (go/labels emit.go) NÃO recalcula: lê o
// preço já gravado em sz_order_meta._sz_freight_price. Um único bucket "Correios
// vs demais" resolvido nesta função evita o produtor mudar a config entre a
// compra e a emissão e o cliente ver um preço enquanto a carteira é debitada
// outro (o bug de serviço/preço divergente resolvido nesta mesma sessão).
type fixedFreightPair struct {
	Correios *float64
	Outras   *float64
}

func (h *CheckoutHandler) producerFixedFreight(ctx context.Context, producerID int64) fixedFreightPair {
	var p fixedFreightPair
	if producerID <= 0 {
		return p
	}
	_ = h.db.QueryRow(ctx,
		`SELECT (settings->>'frete_fixo_correios')::numeric,
		        (settings->>'frete_fixo_outras')::numeric
		   FROM senderzz_portal_users
		  WHERE id = $1`, producerID,
	).Scan(&p.Correios, &p.Outras)
	return p
}

// isCorreios detecta a transportadora pelo nome da empresa (ex.: "Correios").
func isCorreios(company string) bool {
	return strings.Contains(strings.ToUpper(company), "CORREIOS")
}

func (h *CheckoutHandler) applyFixedFreight(ctx context.Context, producerID int64, opts []freight.ServiceOption) []freight.ServiceOption {
	pair := h.producerFixedFreight(ctx, producerID)
	if pair.Correios == nil && pair.Outras == nil {
		return opts
	}
	for i := range opts {
		if isCorreios(opts[i].Company) {
			if pair.Correios != nil {
				opts[i].Price = math.Round(*pair.Correios*100) / 100
				opts[i].Locked = true
			}
		} else if pair.Outras != nil {
			opts[i].Price = math.Round(*pair.Outras*100) / 100
			opts[i].Locked = true
		}
	}
	return opts
}

// ── Bloqueio de Correios + travamento na mais barata, POR PRODUTOR ─────────
//
// Pedido dono 2026-07-27: "um cliente vai usar somente transportadora privada
// [...] bloqueou correios [...] faz um modelo que deixa por padrão correios
// bloqueado e todos os outros liberados sem o cliente poder mexer" — dois
// efeitos, os DOIS aplicados aqui (mesma função, mesma ordem tanto no GET
// /freight quanto na re-cotação do POST /order):
//  1. remove qualquer opção Correios do resultado;
//  2. TRAVA a escolha: das opções restantes, mantém só a MAIS BARATA — não é
//     "mostra as privadas e deixa escolher", é "sistema escolhe, cliente não
//     mexe" (o próprio pedido do dono). O front, com 1 única opção na lista,
//     não dá pro cliente trocar de transportadora.
//
// Guardado em settings->>'bloqueio_correios' (mesmo padrão sem-migration de
// expedicao_ativa/frete_fixo_*).
func (h *CheckoutHandler) producerBloqueiaCorreios(ctx context.Context, producerID int64) bool {
	if producerID <= 0 {
		return false
	}
	var v bool
	_ = h.db.QueryRow(ctx,
		`SELECT COALESCE((settings->>'bloqueio_correios')::boolean, false)
		   FROM senderzz_portal_users WHERE id = $1`, producerID,
	).Scan(&v)
	return v
}

func (h *CheckoutHandler) applyCorreiosLock(ctx context.Context, producerID int64, opts []freight.ServiceOption) []freight.ServiceOption {
	if !h.producerBloqueiaCorreios(ctx, producerID) {
		return opts
	}
	allowed := make([]freight.ServiceOption, 0, len(opts))
	for _, o := range opts {
		if !isCorreios(o.Company) {
			allowed = append(allowed, o)
		}
	}
	if len(allowed) == 0 {
		return allowed
	}
	cheapest := allowed[0]
	for _, o := range allowed[1:] {
		if o.Price < cheapest.Price {
			cheapest = o
		}
	}
	cheapest.Preferred = true
	cheapest.Locked = true
	return []freight.ServiceOption{cheapest}
}
