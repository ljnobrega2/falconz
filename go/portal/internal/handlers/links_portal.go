// Package handlers — handler da seção "Checkouts & Links" do Portal V2.
//
// Espelha templates/portal/v2/sections/links.php (Fase 6 — v432) — porta a tela
// de links de checkout auto-gerados para o backend Go, mantendo UX/UI, textos
// PT-BR e REGRAS idênticas ao WP.
//
// Rotas (namespace /wp-json/senderzz/v1):
//
//	GET    /portal/links                       — lista os links do usuário (escopo por role)
//	POST   /portal/links                       — CRIA uma oferta/checkout (só produtor)   // FEAT-PORTAL-SALES
//	POST   /portal/links/{id}/affiliate-toggle — liga/desliga visibilidade p/ afiliados (só produtor)
//	POST   /portal/links/{id}/commission       — atualiza comissão % do link (só produtor)
//	DELETE /portal/links/{id}                  — exclui o link (só produtor)
//
// ── ESCOPO POR ROLE (porte fiel de links.php) ─────────────────────────────────
//
//	Produtor: vê TODOS os links DELE — WHERE cl.producer_id = $1 ($1 = portal id).
//	          (espelha o WP user_id=portal->id; no espelho PG a coluna dona é
//	           producer_id = senderzz_portal_users.id — admin checkout_links.go
//	           confirma "producer_id → senderzz_portal_users.id — 38/38").
//	Afiliado: vê só os links Cash on Delivery que o PRODUTOR a que ele está
//	          vinculado liberou (affiliate_visible=TRUE, tipo='motoboy').
//	          Join canônico (NUNCA improvisar — espelha vitrine.go / Commissions):
//	            senderzz_affiliates a
//	              JOIN senderzz_portal_users p
//	                ON (p.id = a.produtor_id OR p.wp_user_id = a.produtor_id)
//	               AND p.role = 'produtor'
//	             WHERE p.id = cl.producer_id
//	               AND a.afiliado_id = u.wp_user_id   (NUNCA IN(u.id,u.wp_user_id))
//	               AND a.status = 'active'
//	          Usa EXISTS + subquery escalar (não JOIN direto cl↔a): um produtor
//	          pode ter >1 vínculo ativo com o afiliado (inclusive a linha de
//	          afiliação produto_id=0) → JOIN direto duplicaria cada link.
//	operator/desconhecido: cai no caminho do produtor (lista vazia se não dono) —
//	          fiel ao WP (a section só trata afiliado vs produtor).
//
// No ADMIN estes links NUNCA aparecem para o afiliado/produtor de outro — o
// escopo acima garante isso (cada lado vê só o que lhe pertence).
//
// ── LIMITAÇÕES DO ESPELHO PG (degradação graciosa, como vitrine.go) ───────────
//
//	O espelho PG (infra/postgres/schema-fixes-v467-checkout-links.sql) tem só 12
//	colunas: id, producer_id, post_id, token, tipo, url, display_value,
//	price_label, affiliate_visible, name, slug, created_at.
//
//	Colunas do WP AUSENTES no espelho — campos degradados (a UI deve tratar):
//	  - components_text          → não migrado; retornamos "" (card oculta a linha).
//	  - link_motoboy_id          → não migrado; SEM agrupamento principal+espelho
//	                               Motoboy. A lista é PLANA; cada link traz `tipo`
//	                               p/ a UI decidir o badge (Expedição/Motoboy).
//	                               O DELETE NÃO faz cascata de espelho (não há FK).
//	  - affiliate_commission_pct → a COLUNA não foi migrada para o espelho PG. Mas a
//	                               comissão por-oferta do produtor AGORA É persistível
//	                               de verdade (FEAT-PORTAL-SALES): gravamos em
//	                               senderzz_portal_user_meta na chave
//	                               offerCommissionMetaKey(link_id), keyed pelo PORTAL
//	                               id do produtor dono (a MESMA tabela/owner-scope que
//	                               affiliates_portal.go já usa p/ a comissão padrão).
//	                               O endpoint /commission grava e o List lê de volta —
//	                               não há mais 501. Quando infra adicionar a coluna ao
//	                               espelho, mover a escrita p/ a coluna sem mudar o
//	                               contrato JSON (affiliate_commission_pct).
//	                               Para o AFILIADO, a comissão exibida continua vindo do
//	                               vínculo (senderzz_affiliates.comissao_pct) — o teto
//	                               por-oferta do produtor é informativo no card dele.
//	  - user_id / updated_at     → não migrados; usamos producer_id como dono.
//
// ── GOVERNANÇA (regras de negócio já definidas — porte fiel) ──────────────────
//
//	Comissão de afiliado é definida pelo PRODUTOR. Toggle de visibilidade e
//	comissão são exclusivos do produtor (no WP: !is_aff && !is_sub). PortalUser
//	não carrega parent_user_id, então gateamos por role≠afiliado; o gate de
//	sub-usuário não é verificável pelo contexto (ver FLAG no fim).
package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/portal-service/internal/auth"
	"github.com/senderzz/portal-service/internal/httpx"
)

// LinksHandler agrupa as dependências dos handlers de links de checkout.
// Construção idêntica a WebhookHandler/IntegrationsHandler/VitrineHandler — o
// integrador monta com &handlers.LinksHandler{Pool: pool}.
type LinksHandler struct {
	Pool *pgxpool.Pool
}

// Tetos de listagem de links. AUDIT PERF-list-endpoints-hard-limit: as listas
// buscam limit+1 (N+1) para devolver has_more sem COUNT, eliminando truncamento
// silencioso (o front antes não sabia que a lista fora cortada).
const (
	listLinksLimit    = 100 // links do produtor
	listLinksAffLimit = 300 // links liberados ao afiliado
)

// ── Shape de resposta ─────────────────────────────────────────────────────────

// linkCard — um link de checkout na grade da section.
// Espelha os campos consumidos por links.php (card + badges + rodapé).
type linkCard struct {
	ID               int64   `json:"id"`
	Name             string  `json:"name"`              // cl.name → display_name no WP
	ComponentsText   string  `json:"components_text"`   // AUSENTE no espelho PG → sempre "" (degradado)
	Tipo             string  `json:"tipo"`              // correio | expedicao | motoboy (badge Expedição/Motoboy)
	URL              string  `json:"url"`               // link público; p/ afiliado = affiliate_url (com rastreio ?r=)
	AffiliateURL     string  `json:"affiliate_url"`     // só afiliado: url + ?r=token; produtor = ""
	PriceLabel       string  `json:"price_label"`       // "R$ 349,00"
	DisplayValue     float64 `json:"display_value"`     // preço numérico (fallback p/ price_label vazio)
	AffiliateVisible bool    `json:"affiliate_visible"` // só relevante p/ produtor
	CommissionPct    float64 `json:"affiliate_commission_pct"`
	CreatedAt        string  `json:"created_at"` // DD/MM/YYYY (fuso America/Sao_Paulo, formatado no SQL p/ exibição BR)
	BannerURL        string  `json:"banner_url,omitempty"`
}

type checkoutBannerRequest struct {
	BannerURL string `json:"banner_url"`
}

// commissionUpdateRequest é o body de POST /portal/links/{id}/commission.
type commissionUpdateRequest struct {
	CommissionPct float64 `json:"commission_pct"`
}

// affiliateToggleRequest é o body de POST /portal/links/{id}/affiliate-toggle.
type affiliateToggleRequest struct {
	Enabled bool `json:"enabled"`
}

// NOTA: a detecção de role afiliado reaproveita isAffiliateRole (definido em
// motoboy_portal.go: 'afiliado' | 'affiliate' | 'afiliada') — não redeclarar.

// ── GET /portal/links ─────────────────────────────────────────────────────────

// List retorna os links de checkout do usuário autenticado, escopados por role.
//
// Produtor: WHERE cl.producer_id = u.ID (portal id).
// Afiliado: links liberados (affiliate_visible) dos produtores aos quais está
//
//	vinculado (status='active'), tipo<>'motoboy', com a URL de rastreio.
//
// Degrada a lista vazia se uma tabela do espelho faltar (isUndefinedTable),
// como vitrine.go.
func (h *LinksHandler) List(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	ctx := r.Context()

	if isAffiliateRole(u.Role) {
		h.listForAffiliate(w, r, u)
		return
	}

	// ── Produtor (e operator/desconhecido): todos os links dele. ───────────────
	// $1 = portal id (producer_id no espelho). Ordena created_at DESC, id DESC
	// (espelha "ORDER BY created_at DESC, id ASC" do WP — usamos id DESC para
	// desempate estável e determinístico).
	// FEAT-AFF-COMMISSION: lê a comissão por-oferta direto da COLUNA dedicada
	// (affiliate_commission_pct, migração 428) — não há mais passo attach via meta.
	rows, err := h.Pool.Query(ctx,
		`SELECT cl.id, cl.name, cl.tipo, cl.url,
		        cl.price_label, cl.display_value, cl.affiliate_visible,
		        cl.affiliate_commission_pct,
		        COALESCE((SELECT meta_value FROM senderzz_portal_user_meta
		                   WHERE user_id = cl.producer_id
		                     AND meta_key = '_sz_checkout_banner_url:' || cl.id::text
		                   LIMIT 1), '') AS banner_url,
		        to_char(cl.created_at AT TIME ZONE 'America/Sao_Paulo', 'DD/MM/YYYY') AS created_at
		   FROM senderzz_checkout_links cl
		  WHERE cl.producer_id = $1
		  ORDER BY cl.created_at DESC, cl.id DESC
		  LIMIT $2`,
		u.ID, listLinksLimit+1, // N+1: detecta truncamento sem COUNT. // PERF-list-endpoints-hard-limit
	)
	if err != nil {
		if isUndefinedTable(err) {
			httpx.WriteOK(w, map[string]any{"data": []linkCard{}, "total": 0, "has_more": false, "limit": listLinksLimit, "role": u.Role, "is_affiliate": false})
			return
		}
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer rows.Close()

	out := []linkCard{}
	for rows.Next() {
		var c linkCard
		var createdAt string
		if err := rows.Scan(
			&c.ID, &c.Name, &c.Tipo, &c.URL,
			&c.PriceLabel, &c.DisplayValue, &c.AffiliateVisible,
			&c.CommissionPct,
			&c.BannerURL,
			&createdAt,
		); err != nil {
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao ler links")
			return
		}
		// SQL já devolve DD/MM/YYYY (fuso America/Sao_Paulo) — só exibição no card.
		c.CreatedAt = createdAt
		out = append(out, c)
	}
	if rows.Err() != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao processar links")
		return
	}

	hasMore := len(out) > listLinksLimit
	if hasMore {
		out = out[:listLinksLimit]
	}

	httpx.WriteOK(w, map[string]any{
		"data":         out,
		"total":        len(out),
		"has_more":     hasMore,
		"limit":        listLinksLimit,
		"role":         u.Role,
		"is_affiliate": false,
	})
}

// listForAffiliate retorna os links liberados ao afiliado.
//
// Join canônico (espelha vitrine.go / Commissions handler):
//
//	senderzz_affiliates a
//	  JOIN senderzz_portal_users p
//	    ON (p.id = a.produtor_id OR p.wp_user_id = a.produtor_id) AND p.role='produtor'
//	 WHERE p.id = cl.producer_id AND a.afiliado_id = u.wp_user_id AND a.status='active'
//
// A comissão exibida é a do VÍNCULO (a.comissao_pct), não do link (o link não a
// armazena no espelho). A URL recebe o token de rastreio ?r= (vínculo id),
// reproduzindo sz_aff_checkout_url_with_aff fielmente.
func (h *LinksHandler) listForAffiliate(w http.ResponseWriter, r *http.Request, u *auth.PortalUser) {
	ctx := r.Context()

	// Lê o salt de referência do afiliado (espelha get_option('sz_aff_ref_salt')).
	// Sem salt → token vazio; a UI ainda copia a url base (degradação graciosa).
	refSalt := h.affiliateRefSalt(ctx)

	rows, err := h.Pool.Query(ctx,
		`SELECT cl.id, cl.name, cl.tipo, cl.url,
		        cl.price_label, cl.display_value,
		        to_char(cl.created_at AT TIME ZONE 'America/Sao_Paulo', 'DD/MM/YYYY') AS created_at,
		        (
		            SELECT a.id
		              FROM senderzz_affiliates a
		              JOIN senderzz_portal_users p
		                ON (p.id = a.produtor_id OR p.wp_user_id = a.produtor_id)
		               AND p.role = 'produtor'
		             WHERE p.id = cl.producer_id
		               AND a.afiliado_id = $1
		               AND a.status = 'active'
		             ORDER BY a.id ASC
		             LIMIT 1
		        ) AS vinculo_id,
		        COALESCE((
		            SELECT a.comissao_pct
		              FROM senderzz_affiliates a
		              JOIN senderzz_portal_users p
		                ON (p.id = a.produtor_id OR p.wp_user_id = a.produtor_id)
		               AND p.role = 'produtor'
		             WHERE p.id = cl.producer_id
		               AND a.afiliado_id = $1
		               AND a.status = 'active'
		             ORDER BY a.comissao_pct DESC
		             LIMIT 1
		        ), 0) AS commission_pct
		   FROM senderzz_checkout_links cl
		  WHERE cl.affiliate_visible = TRUE
		    AND cl.tipo = 'motoboy'
		    AND EXISTS (
		            SELECT 1
		              FROM senderzz_affiliates a
		              JOIN senderzz_portal_users p
		                ON (p.id = a.produtor_id OR p.wp_user_id = a.produtor_id)
		               AND p.role = 'produtor'
		             WHERE p.id = cl.producer_id
		               AND a.afiliado_id = $1
		               AND a.status = 'active'
		        )
		  ORDER BY cl.created_at DESC, cl.id DESC
		  LIMIT $2`,
		u.WPUserID, listLinksAffLimit+1, // N+1: detecta truncamento sem COUNT. // PERF-list-endpoints-hard-limit
	)
	if err != nil {
		if isUndefinedTable(err) {
			httpx.WriteOK(w, map[string]any{"data": []linkCard{}, "total": 0, "has_more": false, "limit": listLinksAffLimit, "role": u.Role, "is_affiliate": true})
			return
		}
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer rows.Close()

	out := []linkCard{}
	for rows.Next() {
		var c linkCard
		var createdAt string
		var vinculoID *int64
		if err := rows.Scan(
			&c.ID, &c.Name, &c.Tipo, &c.URL,
			&c.PriceLabel, &c.DisplayValue,
			&createdAt, &vinculoID, &c.CommissionPct,
		); err != nil {
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao ler links")
			return
		}
		// SQL já devolve DD/MM/YYYY (fuso America/Sao_Paulo) — só exibição no card.
		c.CreatedAt = createdAt
		c.AffiliateVisible = true // só linhas liberadas chegam aqui

		// URL de rastreio do afiliado: url + ?r=token (token = vínculo id).
		// Espelha sz_aff_checkout_url_with_aff / sz_aff_encode_ref_token.
		if vinculoID != nil && *vinculoID > 0 && c.URL != "" {
			c.AffiliateURL = appendRefToken(c.URL, *vinculoID, refSalt)
		} else {
			c.AffiliateURL = c.URL
		}
		out = append(out, c)
	}
	if rows.Err() != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao processar links")
		return
	}

	hasMore := len(out) > listLinksAffLimit
	if hasMore {
		out = out[:listLinksAffLimit]
	}

	httpx.WriteOK(w, map[string]any{
		"data":         out,
		"total":        len(out),
		"has_more":     hasMore,
		"limit":        listLinksAffLimit,
		"role":         u.Role,
		"is_affiliate": true,
	})
}

// ── POST /portal/links ────────────────────────────────────────────────────────  // FEAT-PORTAL-SALES
//
// Create gera uma OFERTA/checkout do produtor — o "gerar link de checkout/oferta"
// que a UX-AUDIT (2.2 Produtor, ADD P0) apontava como ausente (sem endpoint em
// go/portal). É um INSERT REAL em senderzz_checkout_links (a tabela e todas as
// colunas existem no espelho PG, ver infra/postgres/150-fixes-v467-checkout-links.sql).
//
// GOVERNANÇA / id-space (porte fiel):
//   - SÓ o produtor cria ofertas (afiliado/operator → 403). Espelha o gate do
//     section products.php/links.php (!is_aff && !is_sub) — o admin go/admin mantém
//     checkout_links como SOMENTE-LEITURA de propósito ("ofertas são geradas no
//     fluxo de produto do portal" — checkout_links.go:223). Este é esse fluxo.
//   - producer_id = u.ID (PORTAL id — id-space canônico de cl.producer_id, igual ao
//     List/AffiliateToggle/Delete; NUNCA wp_user_id).
//   - post_id = wp_post_id do produto-alvo (liga a oferta ao produto, como o WP).
//     O produto deve pertencer ao produtor (guard de posse) — senão 404/403.
//   - REGRA DO DONO (#91, 2026-06): o produtor NÃO escolhe mais o tipo de envio.
//     A criação decide 1 ou 3 links pela EXPEDIÇÃO ATIVA do produtor, lida de
//     senderzz_portal_users.settings->>'expedicao_ativa' (AUDIT-2026-07-11: opt-in —
//     default INATIVA; só ativa quando a flag é EXPLICITAMENTE 'true', ligada pelo
//     admin em Produtores):
//   - Expedição ATIVA  → cria 3 links na MESMA transação:
//     1) correio:  name = "X"            url = /checkout/?sz=<tA>
//     2) motoboy:  name = "X — Motoboy"  url = /checkout/?sz=<tB>
//     3) misto:     name = "X — Link único" url = /checkout/?sz=<tC>
//     (o sufixo " — Motoboy", em-dash U+2014 + espaços, é o ÚNICO elo de
//     pareamento que products.go usa: cl.name || ' — Motoboy' → cod_url.)
//   - Expedição INATIVA → cria 1 link só:
//     1) motoboy:  name = "X"            url = /checkout/?sz=<tB>
//     (o fallback "só Cash on Delivery" de products.go lista tipo='motoboy'
//     direto e exibe o próprio name — sem sufixo, pois não há par a casar.)
//     tipo='motoboy' continua NÃO sendo aceito no body (nasce aqui automaticamente,
//     nunca por escolha do produtor). O campo Tipo do body é IGNORADO (compat).
//   - token = 32 hex (crypto/rand) — fiel ao formato real migrado do WP (md5-like).
//     Cada link recebe seu PRÓPRIO token (correio, motoboy e misto nunca compartilham sz=,
//     fiel aos dados de produção).
//   - url   = base pública do checkout FALK + ?sz=<token> — checkout-ui React
//     (/checkout/?sz=<token>). Base configurável via CHECKOUT_PUBLIC_URL.
//   - display_value = preço de venda (> 0); price_label = "R$ 1.234,56" (pt-BR).
//   - affiliate_visible default false (o produtor libera depois via AffiliateToggle).
//
// Esta é a peça que FECHA o loop de venda do produtor: cadastrar produto (products
// Create) → gerar oferta com preço (aqui) → liberar a afiliados (AffiliateToggle) →
// definir comissão (CommissionUpdate). Tudo persistente, sem stub.
type createLinkRequest struct {
	ProductID int64 `json:"product_id"` // sz_products.id do produto-alvo PRINCIPAL (de onde o modal abriu)
	// Composition — composição da oferta (REDESENHO 2026-06-23). Cada linha é
	// {product_id, qty, variacao?}. O FRONTEND NÃO digita mais o nome: o backend o
	// monta canonicamente a partir da composição + valor (+ % de comissão quando
	// precisa diferenciar de outra oferta de mesmo conjunto/valor). Quando ausente
	// (compat de wire antigo), cai no caminho legado: usa Name + ProductID.
	Composition      []compositionLine `json:"composition"`
	Name             string            `json:"name"`                     // LEGADO (compat): só usado se Composition vazia. O fluxo novo IGNORA — o nome é canônico.
	Price            float64           `json:"price"`                    // preço de venda (display_value)
	Tipo             string            `json:"tipo"`                     // IGNORADO (#91): o tipo é decidido pela expedição ativa do produtor, não pelo body. Mantido por compat de wire.
	AffiliateVisible bool              `json:"affiliate_visible"`        // libera p/ afiliados já na criação
	CommissionPct    float64           `json:"affiliate_commission_pct"` // comissão por-oferta (0..99)
}

// compositionLine — uma linha da composição da oferta (produto + quantidade [+ variação]).
// ProductID é sz_products.id (NÃO wp_post_id: produtos nativos Go têm wp_post_id NULL —
// products.go::Create grava wp_post_id=NULL; a chave estável é sp.id). Qty >= 1.
// Variacao é opcional (texto livre de sz_products.variacao; entra no nome p/ diferenciar
// quando o produto tem >1 variação).
type compositionLine struct {
	ProductID int64  `json:"product_id"`
	Qty       int    `json:"qty"`
	Variacao  string `json:"variacao"`
}

// Create cria uma nova oferta/checkout do produtor (INSERT real em senderzz_checkout_links).
func (h *LinksHandler) Create(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	// Só o produtor cria ofertas (afiliado promove, não cria).
	if u.Role != "produtor" {
		httpx.WriteErr(w, http.StatusForbidden, "apenas o produtor pode criar ofertas")
		return
	}

	var req createLinkRequest
	if err := decodeJSONBody(r, &req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}
	if req.Price <= 0 {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "Informe um preço de venda maior que zero.")
		return
	}
	// #93: a comissão do afiliado é LIMITADA a 99% (era 0..100 e o input do front
	// aceitava lixo como "50544444"). Clampe duro no backend autoritativo.
	if req.CommissionPct < 0 || req.CommissionPct > 99 {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "A comissão deve ficar entre 0% e 99%.")
		return
	}
	if req.ProductID <= 0 {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "Selecione o produto da oferta.")
		return
	}
	ctx := r.Context()

	// ── Normaliza a composição (REDESENHO 2026-06-23) ─────────────────────────────
	// Fluxo novo: o front manda Composition = [{product_id, qty, variacao?}]. O 1º
	// produto da composição (= o produto de onde o modal abriu) é o PRINCIPAL; a
	// oferta fica vinculada a ele via post_id (a tabela tem UM post_id — bundle de
	// produtos diferentes só aparece sob o principal: limitação assumida, sem coluna
	// nova). Compat: se Composition vier vazia (wire antigo), reconstrói 1 linha de
	// {ProductID, qty 1} — o nome canônico ainda sai certo.
	comp := req.Composition
	if len(comp) == 0 {
		comp = []compositionLine{{ProductID: req.ProductID, Qty: 1}}
	}
	// O principal é sempre a 1ª linha; força o ProductID a casar com ela (autoridade
	// no backend — o front pode ter omitido/divergido).
	req.ProductID = comp[0].ProductID

	// Coleta os ids para o guard de posse de TODAS as linhas (não só a principal) e
	// normaliza qty (>=1). productIDs preserva a ordem das linhas p/ montar o nome.
	productIDs := make([]int64, 0, len(comp))
	for i := range comp {
		if comp[i].ProductID <= 0 {
			httpx.WriteErr(w, http.StatusUnprocessableEntity, "Selecione o produto da oferta.")
			return
		}
		if comp[i].Qty < 1 {
			comp[i].Qty = 1
		}
		comp[i].Variacao = strings.TrimSpace(comp[i].Variacao)
		productIDs = append(productIDs, comp[i].ProductID)
	}

	// ── Guard de posse de TODAS as linhas + nomes + expedição numa só query ───────
	// Cada produto da composição deve pertencer ao produtor (produtor_id = u.ID) e
	// não estar soft-deletado. Lemos o nome canônico (sp.nome), o post_id principal
	// (COALESCE(wp_post_id, id)) e a flag de expedição do produtor. Montamos um mapa
	// id→{nome,postID} para reordenar pela composição e exigir count == len.
	type prodMeta struct {
		nome   string
		postID int64
	}
	prodByID := make(map[int64]prodMeta, len(productIDs))
	var expedicaoFlag *string
	rowsP, err := h.Pool.Query(ctx,
		`SELECT sp.id, sp.nome, COALESCE(sp.wp_post_id, sp.id),
		        pu.settings ->> 'expedicao_ativa'
		   FROM sz_products sp
		   JOIN senderzz_portal_users pu ON pu.id = sp.produtor_id
		  WHERE sp.id = ANY($1) AND sp.produtor_id = $2 AND sp.status IS DISTINCT FROM 'deleted'`,
		productIDs, u.ID,
	)
	if err != nil {
		if isUndefinedTable(err) {
			httpx.WriteErr(w, http.StatusServiceUnavailable, "recurso indisponível no momento")
			return
		}
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	for rowsP.Next() {
		var id int64
		var nome string
		var pid int64
		if err := rowsP.Scan(&id, &nome, &pid, &expedicaoFlag); err != nil {
			rowsP.Close()
			httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
			return
		}
		prodByID[id] = prodMeta{nome: nome, postID: pid}
	}
	rowsP.Close()
	if rowsP.Err() != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	// Posse: TODO produto da composição tem de existir e ser do produtor. Se algum
	// id não voltou, é de terceiro / inexistente / deletado → 404 (não vaza qual).
	for _, pid := range productIDs {
		if _, ok := prodByID[pid]; !ok {
			httpx.WriteErr(w, http.StatusNotFound, "produto não encontrado")
			return
		}
	}
	// post_id da oferta = o do PRODUTO PRINCIPAL (1ª linha da composição).
	postID := prodByID[req.ProductID].postID
	// AUDIT-2026-07-11: opt-IN — Expedição só ATIVA quando a flag é EXPLICITAMENTE
	// 'true'. Ausente/NULL/qualquer outro valor = inativa (nasce desativada).
	expedicaoAtiva := expedicaoFlag != nil && *expedicaoFlag == "true"

	priceLabel := brlLabel(req.Price)

	// ── Base do nome (autoridade no backend) ──────────────────────────────────────
	// REDESENHO 2026-06-24 (decisão do dono): o nome NÃO carrega mais o VALOR no fim
	// nem o % de COMISSÃO. A base é só a composição de produtos ("1 Datalaprox";
	// bundle "1 Avenobis Gotas + 1 Pomada"). O ESTÁGIO de funil (Downsell/Remarketing)
	// é atribuído por PREÇO no recompute, LOGO APÓS o INSERT (ver recomputeStageNames).
	names := make(map[int64]string, len(prodByID))
	for id, m := range prodByID {
		names[id] = m.nome
	}
	baseName := buildBaseName(comp, names)

	// ── Dedup por (base + preço) ──────────────────────────────────────────────────
	// O discriminador agora é o ESTÁGIO, atribuído pelo PREÇO. Duas ofertas de mesma
	// base e MESMO preço teriam o mesmo rank → mesmo estágio → indistinguíveis: por
	// isso bloqueamos. Preço DIFERENTE ⇒ vira um estágio distinto (cria normal). A
	// comissão NÃO entra mais no nome (vive só em affiliate_commission_pct).
	dupExists, err := h.offerExistsSamePrice(ctx, u.ID, baseName, req.Price)
	if err != nil {
		if isUndefinedTable(err) {
			httpx.WriteErr(w, http.StatusServiceUnavailable, "recurso indisponível no momento")
			return
		}
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	if dupExists {
		httpx.WriteErr(w, http.StatusConflict, "Já existe uma oferta deste produto com este mesmo preço.")
		return
	}

	// Tokens INDEPENDENTES por link (correio e motoboy nunca compartilham sz=).
	codToken, err := randomHex(16)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao gerar oferta")
		return
	}
	codURL := codCheckoutURL(codToken)

	// Monta a lista de links a inserir conforme a expedição ativa. Cada entrada
	// carrega tipo/url/token/name próprios. FEAT-AFF-COMMISSION: a comissão por-oferta
	// é a MESMA nos dois links (a oferta dita a % do afiliado).
	type linkRow struct {
		tipo  string
		url   string
		token string
		name  string
	}
	var rowsToInsert []linkRow
	if expedicaoAtiva {
		// 3 links individuais: Expedição, Cash on Delivery e o link único
		// (misto), que decide o modo pelo CEP sem o cliente escolher.
		expToken, errTok := randomHex(16)
		if errTok != nil {
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao gerar oferta")
			return
		}
		// Nomes PROVISÓRIOS (= base [+ sufixo Motoboy]); o estágio de funil real é
		// aplicado por PREÇO logo após o INSERT, em recomputeStageNames.
		mistoToken, errTok := randomHex(16)
		if errTok != nil {
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao gerar oferta")
			return
		}
		rowsToInsert = []linkRow{
			{tipo: "correio", url: checkoutURL("correio", expToken), token: expToken, name: baseName},
			// Sufixo " — Motoboy" (em-dash U+2014 + espaços) — ÚNICO elo de pareamento
			// (products.go casa por cl.name || ' — Motoboy'). Preservado pelo recompute.
			{tipo: "motoboy", url: codURL, token: codToken, name: baseName + " — Motoboy"},
			{tipo: "misto", url: checkoutURL("misto", mistoToken), token: mistoToken, name: baseName + " — Link único"},
		}
	} else {
		// Só Cash on Delivery (sem expedição). name SEM sufixo Motoboy: o fallback de
		// products.go lista tipo='motoboy' direto e exibe o próprio name; não há par.
		rowsToInsert = []linkRow{
			{tipo: "motoboy", url: codURL, token: codToken, name: baseName},
		}
	}

	// FEAT-CHECKOUT-MULTI-ITEM: persiste a composição ESTRUTURADA (antes só virava
	// texto em name/base_name). NULL quando é 1 produto só (oferta simples, igual
	// ao comportamento de sempre) — só grava array quando há de fato >1 linha.
	var compJSON []byte
	if len(comp) > 1 {
		compJSON, err = json.Marshal(comp)
		if err != nil {
			httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
			return
		}
	}

	// Transação: ou os 1-3 links entram juntos, ou nada (nunca meio-par).
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao criar oferta")
		return
	}
	defer tx.Rollback(ctx)

	var firstID int64
	var firstURL string
	for i, lr := range rowsToInsert {
		var id int64
		err = tx.QueryRow(ctx,
			`INSERT INTO senderzz_checkout_links
			     (producer_id, post_id, token, tipo, url, display_value, price_label,
			      affiliate_visible, name, slug, affiliate_commission_pct, base_name,
			      composition_items, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, NOW())
			 RETURNING id`,
			u.ID, postID, lr.token, lr.tipo, lr.url, req.Price, priceLabel,
			req.AffiliateVisible, lr.name, lr.token, req.CommissionPct, baseName,
			compJSON,
		).Scan(&id)
		if err != nil {
			if isUndefinedTable(err) {
				httpx.WriteErr(w, http.StatusServiceUnavailable, "recurso indisponível no momento")
				return
			}
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao criar oferta")
			return
		}
		// O "principal" devolvido é o 1º link da lista (Expedição quando há 3; senão o COD).
		if i == 0 {
			firstID = id
			firstURL = lr.url
		}
	}

	// Aplica o estágio de funil (Downsell/Remarketing) por PREÇO a TODAS as irmãs de
	// mesma base — inclui as recém-inseridas e as que mudaram de rank por causa delas.
	// Atômico com o INSERT (mesma tx).
	if err = recomputeStageNames(ctx, tx, u.ID, baseName); err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao nomear oferta")
		return
	}

	// O nome FINAL do link principal sai do recompute (rank de preço). Relê p/ devolver.
	var finalName string
	if err = tx.QueryRow(ctx,
		`SELECT name FROM senderzz_checkout_links WHERE id = $1`, firstID,
	).Scan(&finalName); err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao nomear oferta")
		return
	}

	if err = tx.Commit(ctx); err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao criar oferta")
		return
	}

	msg := "Oferta criada com sucesso."
	if expedicaoAtiva {
		msg = "Oferta criada com sucesso (Expedição + Cash on Delivery + Link único)."
	} else {
		msg = "Oferta criada com sucesso (Cash on Delivery)."
	}
	httpx.WriteOK(w, map[string]any{
		"id":                       firstID,
		"url":                      firstURL,
		"cod_url":                  codURL,
		"name":                     finalName, // nome canônico montado pelo backend (+ " + N%" se diferenciado)
		"price_label":              priceLabel,
		"affiliate_commission_pct": req.CommissionPct,
		"expedicao_ativa":          expedicaoAtiva,
		"links_criados":            len(rowsToInsert),
		"mensagem":                 msg,
	})
}

// ── REDESENHO 2026-06-24: nome por ESTÁGIO de funil (preço), sem valor/comissão ──

// buildBaseName monta a BASE do nome da oferta a partir da composição — SÓ a
// composição de produtos, SEM valor e SEM comissão (decisão do dono 2026-06-24):
//
//	juntar cada linha "{qtd} {nomeProduto}[ {variacao}]" com " + ".
//
// Ex.: comp=[{Datalaprox,1,"pote"}] → "1 Datalaprox pote"
//
//	(produto com 1 só variação → o front não manda variacao → "1 Datalaprox").
//
// bundle comp=[{Avenobis Gotas,1},{Pomada,1}] → "1 Avenobis Gotas + 1 Pomada".
//
// O sufixo de ESTÁGIO (Downsell/Remarketing) NÃO entra aqui — é aplicado por PREÇO
// em recomputeStageNames. Esta base é a chave estável que AGRUPA e PAREIA as irmãs.
func buildBaseName(comp []compositionLine, names map[int64]string) string {
	parts := make([]string, 0, len(comp))
	for _, l := range comp {
		nome := strings.TrimSpace(names[l.ProductID])
		seg := fmt.Sprintf("%d %s", l.Qty, nome)
		if v := strings.TrimSpace(l.Variacao); v != "" {
			seg += " " + v
		}
		parts = append(parts, seg)
	}
	return strings.TrimSpace(strings.Join(parts, " + "))
}

// stageSuffix devolve o sufixo de ESTÁGIO de funil conforme o rank de PREÇO da oferta
// entre as irmãs de mesma base (rank 1 = mais cara). REGRA DO DONO (2026-06-24, rev.):
//
//	rank 1 → " Principal"   (a mais cara — agora SEMPRE rotulada, pedido do dono)
//	rank 2 → " Downsell"
//	rank 3 → " Remarketing"
//	rank ≥4 → " Remarketing N"  com N = rank-2  (4ª = "Remarketing 2", 5ª = "Remarketing 3"…)
//
// Nome do PRODUTOR (link de checkout) = "{qtd} {produto}[ {variação}] {estágio}". O
// CLIENTE continua vendo só a BASE (sem estágio) via base_name. É o ESPELHO em Go da
// expressão CASE em recomputeStageNames (e nas migrações 471/472). Mantê-los idênticos
// é invariante — o teste de naming guarda os dois juntos.
func stageSuffix(rank int) string {
	switch {
	case rank <= 1:
		return " Principal"
	case rank == 2:
		return " Downsell"
	case rank == 3:
		return " Remarketing"
	default:
		return fmt.Sprintf(" Remarketing %d", rank-2)
	}
}

// offerExistsSamePrice diz se o produtor já tem uma oferta da MESMA base e MESMO
// preço (em qualquer canal — correio ou motoboy). É o dedup do novo modelo: mesma
// base + mesmo preço ⇒ mesmo rank ⇒ mesmo estágio ⇒ oferta indistinguível → bloqueia.
func (h *LinksHandler) offerExistsSamePrice(ctx context.Context, producerID int64, baseName string, price float64) (bool, error) {
	pr := math.Round(price*100) / 100 // casa com NUMERIC(12,2) de display_value.
	var exists bool
	err := h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
		   SELECT 1 FROM senderzz_checkout_links
		    WHERE producer_id = $1 AND base_name = $2 AND display_value = $3
		 )`,
		producerID, baseName, pr,
	).Scan(&exists)
	return exists, err
}

// recomputeStageNames recalcula o NOME de TODAS as ofertas (correio + motoboy) de uma
// mesma base do produtor, aplicando o estágio de funil por PREÇO (DENSE_RANK sobre os
// display_value distintos, decrescente — a mais cara é a Principal). Roda dentro da tx
// de Create/Delete: ao inserir/excluir uma irmã, os estágios das demais "sobem"/"descem"
// em lockstep, mantendo o nome ÚNICO por oferta (logo o pareamento por nome de
// products.go segue íntegro).
//
// O sufixo " — Motoboy" entra só num motoboy que TEM correio par no mesmo preço
// (produtor com expedição); produtor só-COD fica sem ele (fiel ao Create). A expressão
// CASE é o ESPELHO SQL de stageSuffix — manter as duas idênticas é invariante.
func recomputeStageNames(ctx context.Context, tx pgx.Tx, producerID int64, baseName string) error {
	if strings.TrimSpace(baseName) == "" {
		return nil
	}
	_, err := tx.Exec(ctx,
		`WITH ranked AS (
		   SELECT display_value,
		          DENSE_RANK() OVER (ORDER BY display_value DESC) AS rnk
		     FROM (
		       SELECT DISTINCT display_value
		         FROM senderzz_checkout_links
		        WHERE producer_id = $1 AND base_name = $2
		     ) d
		 )
		 UPDATE senderzz_checkout_links cl
		    SET name = cl.base_name
		             || CASE
		                  WHEN r.rnk = 1 THEN ' Principal'
		                  WHEN r.rnk = 2 THEN ' Downsell'
		                  WHEN r.rnk = 3 THEN ' Remarketing'
		                  ELSE ' Remarketing ' || (r.rnk - 2)::text
		                END
		             || CASE
		                  WHEN cl.tipo = 'motoboy' AND EXISTS (
		                         SELECT 1 FROM senderzz_checkout_links c2
		                          WHERE c2.producer_id   = cl.producer_id
		                            AND c2.base_name     = cl.base_name
		                            AND c2.display_value = cl.display_value
		                            AND c2.tipo <> 'motoboy'
		                       ) THEN ' — Motoboy'
		                  ELSE ''
		                END
		   FROM ranked r
		  WHERE cl.producer_id   = $1
		    AND cl.base_name     = $2
		    AND cl.display_value = r.display_value`,
		producerID, baseName,
	)
	return err
}

// BannerUpdate define o banner específico de um checkout. O banner global da
// marca continua sendo fallback quando esta configuração estiver vazia.
func (h *LinksHandler) BannerUpdate(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	if isAffiliateRole(u.Role) {
		httpx.WriteErr(w, http.StatusForbidden, "Sem permissão para este checkout.")
		return
	}
	linkID, ok := parseLinkID(r)
	if !ok {
		httpx.WriteErr(w, http.StatusBadRequest, "ID inválido.")
		return
	}
	var req checkoutBannerRequest
	if err := decodeJSONBody(r, &req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}
	banner := strings.TrimSpace(req.BannerURL)
	if banner != "" && !strings.HasPrefix(strings.ToLower(banner), "https://") && !strings.HasPrefix(banner, "/uploads/products/") {
		httpx.WriteErr(w, http.StatusBadRequest, "o banner deve ser uma URL https://")
		return
	}
	key := fmt.Sprintf("_sz_checkout_banner_url:%d", linkID)
	result, err := h.Pool.Exec(r.Context(),
		`INSERT INTO senderzz_portal_user_meta (user_id, meta_key, meta_value)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (user_id, meta_key) DO UPDATE SET meta_value = EXCLUDED.meta_value
		 WHERE EXISTS (SELECT 1 FROM senderzz_checkout_links WHERE id = $4 AND producer_id = $1)`,
		u.ID, key, banner, linkID)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao salvar banner")
		return
	}
	if result.RowsAffected() == 0 {
		httpx.WriteErr(w, http.StatusNotFound, "Checkout não encontrado.")
		return
	}
	httpx.WriteOK(w, map[string]any{"banner_url": banner, "message": "Banner do checkout atualizado."})
}

// ── POST /portal/links/{id}/affiliate-toggle ──────────────────────────────────

// AffiliateToggle liga/desliga a visibilidade do link para afiliados.
// Exclusivo do produtor (espelha ajax_checkout_link_affiliate_toggle).
func (h *LinksHandler) AffiliateToggle(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	if isAffiliateRole(u.Role) {
		httpx.WriteErr(w, http.StatusForbidden, "Sem permissão para este checkout.")
		return
	}

	linkID, ok := parseLinkID(r)
	if !ok {
		httpx.WriteErr(w, http.StatusBadRequest, "ID inválido.")
		return
	}

	var req affiliateToggleRequest
	if err := decodeJSONBody(r, &req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}

	// UPDATE escopado ao dono (producer_id = portal id). Garante posse.
	result, err := h.Pool.Exec(r.Context(),
		`UPDATE senderzz_checkout_links
		    SET affiliate_visible = $1
		  WHERE id = $2 AND producer_id = $3`,
		req.Enabled, linkID, u.ID,
	)
	if err != nil {
		if isUndefinedTable(err) {
			httpx.WriteErr(w, http.StatusServiceUnavailable, "recurso indisponível no momento")
			return
		}
		httpx.WriteErr(w, http.StatusInternalServerError, "Erro ao salvar.")
		return
	}
	if result.RowsAffected() == 0 {
		httpx.WriteErr(w, http.StatusForbidden, "Sem permissão para este checkout.")
		return
	}

	msg := "Oferta removida dos afiliados."
	if req.Enabled {
		msg = "Oferta liberada para afiliados."
	}
	httpx.WriteOK(w, map[string]any{"message": msg, "affiliate_visible": req.Enabled})
}

// ── POST /portal/links/{id}/commission ────────────────────────────────────────

// CommissionUpdate define a comissão % por-oferta do produtor. FEAT-AFF-COMMISSION:
// persiste na COLUNA dedicada senderzz_checkout_links.affiliate_commission_pct
// (migração 428) — antes era meta paliativa (_sz_offer_commission_pct:{id}). O List
// DO PRODUTOR lê de volta da mesma coluna e exibe no card da oferta.
//
// ESTE valor AGORA é CONSUMIDO no cálculo: o checkout Go (go/orders) lê a coluna
// para resolver a % da comissão da venda (cadeia oferta → produtor → global). A
// OFERTA dita a % do afiliado (modelo confirmado pelo dono). O repasse real flui
// por affiliate_amount, computado na criação do pedido a partir desta %.
//
// Posse: o link deve pertencer ao produtor (cl.producer_id = u.ID) — senão 404 (não
// vaza existência de oferta de terceiro).
func (h *LinksHandler) CommissionUpdate(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	if isAffiliateRole(u.Role) {
		httpx.WriteErr(w, http.StatusForbidden, "Sem permissão para este checkout.")
		return
	}

	linkID, ok := parseLinkID(r)
	if !ok {
		httpx.WriteErr(w, http.StatusBadRequest, "ID inválido.")
		return
	}

	var req commissionUpdateRequest
	if err := decodeJSONBody(r, &req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}
	// Validação de faixa (espelha o WP: 0..100).
	if req.CommissionPct < 0 || req.CommissionPct > 100 {
		httpx.WriteErr(w, http.StatusBadRequest, "A comissão deve ficar entre 0% e 100%.")
		return
	}
	ctx := r.Context()

	// UPDATE escopado ao dono (producer_id = portal id) na COLUNA dedicada. Atômico:
	// guard de posse + escrita numa só query. RowsAffected()==0 ⇒ não é do produtor
	// (ou não existe) → 404 sem vazar existência de oferta de terceiro.
	result, err := h.Pool.Exec(ctx,
		`UPDATE senderzz_checkout_links
		    SET affiliate_commission_pct = $1
		  WHERE id = $2 AND producer_id = $3`,
		req.CommissionPct, linkID, u.ID,
	)
	if err != nil {
		if isUndefinedTable(err) {
			httpx.WriteErr(w, http.StatusServiceUnavailable, "recurso indisponível no momento")
			return
		}
		httpx.WriteErr(w, http.StatusInternalServerError, "Erro ao salvar comissão.")
		return
	}
	if result.RowsAffected() == 0 {
		httpx.WriteErr(w, http.StatusNotFound, "Checkout não encontrado.")
		return
	}
	httpx.WriteOK(w, map[string]any{
		"message":                  "Comissão da oferta atualizada.",
		"affiliate_commission_pct": req.CommissionPct,
	})
}

// ── DELETE /portal/links/{id} ─────────────────────────────────────────────────

// Delete exclui o link do usuário. Exclusivo do produtor (espelha
// ajax_checkout_link_delete). SEM cascata de espelho Motoboy: o espelho PG não
// tem link_motoboy_id, então removemos apenas o link selecionado.
func (h *LinksHandler) Delete(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	if isAffiliateRole(u.Role) {
		httpx.WriteErr(w, http.StatusForbidden, "Sem permissão para este checkout.")
		return
	}

	linkID, ok := parseLinkID(r)
	if !ok {
		httpx.WriteErr(w, http.StatusBadRequest, "ID inválido.")
		return
	}

	ctx := r.Context()
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer tx.Rollback(ctx)

	// Lê a oferta-alvo escopada ao dono ANTES de excluir: precisamos da base p/ o
	// recompute e do nome p/ achar o espelho Motoboy. FOR UPDATE serializa contra
	// criações concorrentes na mesma base.
	var baseName string
	var delValue float64
	err = tx.QueryRow(ctx,
		`SELECT base_name, display_value::float8
		   FROM senderzz_checkout_links
		  WHERE id = $1 AND producer_id = $2
		  FOR UPDATE`,
		linkID, u.ID,
	).Scan(&baseName, &delValue)
	if err != nil {
		if err == pgx.ErrNoRows {
			httpx.WriteErr(w, http.StatusNotFound, "Link não encontrado.")
			return
		}
		if isUndefinedTable(err) {
			httpx.WriteErr(w, http.StatusServiceUnavailable, "recurso indisponível no momento")
			return
		}
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Exclui a oferta inteira (todos os canais da mesma base/preço). Assim a
	// representação visual de uma oferta em uma única linha não deixa canais
	// órfãos quando o produtor exclui essa linha.
	if _, err = tx.Exec(ctx,
		`DELETE FROM senderzz_checkout_links
		  WHERE producer_id = $1 AND base_name = $2 AND display_value = $3`,
		u.ID, baseName, delValue,
	); err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Re-ranqueia as irmãs restantes da base (os estágios "sobem": removida a
	// Principal, a antiga Downsell vira Principal).
	if err = recomputeStageNames(ctx, tx, u.ID, baseName); err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	if err = tx.Commit(ctx); err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	httpx.WriteOK(w, map[string]any{"message": "Checkout excluído."})
}

// ── Helpers internos ──────────────────────────────────────────────────────────

// parseLinkID extrai e valida o {id} da rota (>0).
func parseLinkID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// truncDate reduz "2026-06-18 12:00:00" a "2026-06-18" (espelha substr 0..10 do WP).
func truncDate(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

// affiliateRefSalt lê sz_aff_ref_salt de senderzz_options (espelha
// get_option('sz_aff_ref_salt')). Retorna "" se ausente — token degrada a vazio.
func (h *LinksHandler) affiliateRefSalt(ctx context.Context) string {
	var raw string
	err := h.Pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE name = $1`,
		"sz_aff_ref_salt",
	).Scan(&raw)
	if err != nil {
		return ""
	}
	return raw
}

// appendRefToken anexa ?r=<token> à url, reproduzindo fielmente
// sz_aff_checkout_url_with_aff + sz_aff_encode_ref_token do WP:
//
//	packed = pack('N', affiliate_id) . substr(salt, 0, 4)   // 4 bytes BE + 4 bytes salt
//	token  = rtrim(strtr(base64(packed), '+/', '-_'), '=')  // base64url sem padding
//
// affiliateID = senderzz_affiliates.id (PK do vínculo, NÃO wp_user_id).
func appendRefToken(rawURL string, affiliateID int64, salt string) string {
	if salt == "" {
		return rawURL
	}
	// pack('N', id): uint32 big-endian.
	id := uint32(affiliateID)
	packed := []byte{
		byte(id >> 24), byte(id >> 16), byte(id >> 8), byte(id),
	}
	// substr(salt, 0, 4) — primeiros 4 bytes do salt (ASCII hex no WP).
	saltPrefix := salt
	if len(saltPrefix) > 4 {
		saltPrefix = saltPrefix[:4]
	}
	packed = append(packed, []byte(saltPrefix)...)

	token := base64.StdEncoding.EncodeToString(packed)
	token = strings.ReplaceAll(token, "+", "-")
	token = strings.ReplaceAll(token, "/", "_")
	token = strings.TrimRight(token, "=")

	return addQueryArg(rawURL, "r", token)
}

// addQueryArg anexa key=value à querystring da url (espelha add_query_arg do WP).
// Preserva fragment; escolhe ? ou & conforme a presença de querystring.
func addQueryArg(rawURL, key, value string) string {
	frag := ""
	if i := strings.IndexByte(rawURL, '#'); i >= 0 {
		frag = rawURL[i:]
		rawURL = rawURL[:i]
	}
	sep := "?"
	if strings.IndexByte(rawURL, '?') >= 0 {
		sep = "&"
	}
	return rawURL + sep + key + "=" + url.QueryEscape(value) + frag
}

// ── FEAT-PORTAL-SALES: criação de oferta + comissão por-oferta ────────────────

// checkoutPublicBase é a base pública do checkout FALK (checkout-ui React,
// servido pelo gateway em /checkout/ → falk-checkout-ui). Default = domínio de
// produção FALK; configurável via CHECKOUT_PUBLIC_URL p/ não hardcodar (em dev,
// aponte para o tunnel que serve /checkout/). NÃO usar mais o WordPress legado
// senderzz (/checkouts/...): aquele renderiza o checkout antigo FunnelKit.
func checkoutPublicBase() string {
	base := strings.TrimRight(os.Getenv("CHECKOUT_PUBLIC_URL"), "/")
	if base == "" {
		base = "https://app.falklog.com.br"
	}
	return base
}

// checkoutURL monta a URL pública da oferta no checkout FALK (/checkout/?sz=<token>).
// O checkout-ui resolve TUDO pelo token via /checkout-api/offer (nome, preço e o
// `tipo` correio/expedição) — não há segmento por tipo na URL.
func checkoutURL(_tipo, token string) string {
	return checkoutPublicBase() + "/checkout/?sz=" + url.QueryEscape(token)
}

// codCheckoutURL monta a URL pública do checkout Cash on Delivery (motoboy). No
// checkout FALK é a MESMA base /checkout/?sz=<token>: o checkout-ui descobre que é
// motoboy pelo `tipo` da oferta (deliveryModeFromTipo) e mostra o fluxo COD 2-etapas
// + agendamento. Os artefatos WP legados (/codsfpc/, &szm=1) NÃO são mais usados.
// O pareamento por nome (cl.name || ' — Motoboy') em products.go depende SÓ do
// tipo='motoboy' + name — independe da URL.
func codCheckoutURL(token string) string {
	return checkoutPublicBase() + "/checkout/?sz=" + url.QueryEscape(token)
}

// brlLabel formata um valor como "R$ 1.234,56" (pt-BR), fiel ao price_label
// armazenado ("R$ 123,00"). Milhar com ponto, decimal com vírgula.
func brlLabel(v float64) string {
	if v < 0 {
		v = 0
	}
	cents := int64(math.Round(v * 100))
	intPart := cents / 100
	frac := cents % 100
	// Agrupa o inteiro em milhares com ponto.
	s := strconv.FormatInt(intPart, 10)
	var grouped strings.Builder
	n := len(s)
	for i, ch := range s {
		if i > 0 && (n-i)%3 == 0 {
			grouped.WriteByte('.')
		}
		grouped.WriteRune(ch)
	}
	return fmt.Sprintf("R$ %s,%02d", grouped.String(), frac)
}

// NOTA (FEAT-AFF-COMMISSION): a comissão por-oferta migrou da meta paliativa
// (_sz_offer_commission_pct:{id} em senderzz_portal_user_meta) para a COLUNA
// dedicada senderzz_checkout_links.affiliate_commission_pct (migração 428).
// Create/List/CommissionUpdate agora operam direto na coluna — os helpers
// offerCommissionMetaKey/saveOfferCommission/attachOfferCommissions foram
// removidos. O banco vivo não tinha nenhuma linha _sz_offer_commission_pct:* —
// nada a migrar/limpar.
