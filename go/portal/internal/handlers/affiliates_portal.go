// Package handlers — handler da seção "Afiliados" do Portal V2 (user-scoped).
//
// Espelha templates/portal/v2/sections/affiliates.php + os handlers WP
// sz_aff_panel_action_ajax (senderzz-affiliates.php) e Portal_Page::ajax_affiliate_action.
// Porta o programa de afiliados do PRODUTOR para o backend Go, mantendo UX/UI,
// textos PT-BR e GOVERNANÇA idênticos ao WP.
//
// Rotas (namespace /wp-json/senderzz/v1):
//
//	GET    /portal/affiliates                    — payload completo (escopo por sessão)
//	POST   /portal/affiliates/{id}/approve       — aprovar afiliado (produtor)
//	POST   /portal/affiliates/{id}/reject        — recusar afiliado (produtor)
//	POST   /portal/affiliates/{id}/commission    — { commission_pct } por vínculo (produtor)
//	DELETE /portal/affiliates/{id}               — excluir afiliado (produtor)
//	POST   /portal/affiliates/default-commission — { commission_pct } padrão (produtor)
//	POST   /portal/affiliates/auto-approve       — { enabled } aprovação automática (produtor)
//	POST   /portal/affiliates/invites            — gerar link de convite (degradado — sem espelho PG)
//	DELETE /portal/affiliates/invites/{id}       — revogar link de convite (degradado)
//
// O payload do GET casa 1:1 com portal-ui/src/pages/Affiliates.tsx (AffiliatesResp):
//
//	{ ok, pending[], approved[], default_commission_pct, auto_approve,
//	  repasse[], my_affiliations[], my_links[], invite_links[] }
//
// ── GOVERNANÇA (regras do dono — porte fiel) ──────────────────────────────────
//
//	Comissão padrão de afiliado = definida pelo PRODUTOR (senderzz_portal_user_meta
//	  '_sz_aff_default_commission_pct', fallback global sz_aff_default_commission_pct
//	  default 10). Aprovar/recusar = PRODUTOR. Comissão por-vínculo = PRODUTOR.
//	Aprovação automática = PRODUTOR (meta '_sz_aff_auto_approve').
//	Taxa de saque e dias de retenção = GLOBAL/ADMIN (produtor NÃO edita) — por isso
//	  NÃO aparecem aqui nem no React (mantido omitido de propósito).
//	Afiliado NÃO aprova/edita/exclui (gate por role≠afiliado; o backend ainda barra
//	  por ownership). Sub-conta não é verificável pelo PortalUser → gate só por role.
//
// ── id-space CANÔNICO (porte fiel — NUNCA improvisar) ─────────────────────────
//
//	senderzz_affiliates.afiliado_id = wp_user_id do afiliado (atribuição estrita —
//	  NUNCA OR/IN com portal id; colisão portal_id↔wp_user_id atribuiria a 2 pessoas).
//	senderzz_affiliates.produtor_id = id OU wp_user_id do produtor. O botão
//	  "Afiliar-me" (vitrine.go) grava produtor_id = wp_user_id; o WP grava por portal
//	  id. Logo, na ponta do PRODUTOR usamos sempre o OR-form
//	  (produtor_id = portalID OR produtor_id = wpUserID) — na listagem E nos guards
//	  de ownership de approve/reject/commission/delete. Sem o OR-form, vínculos
//	  recém-criados pela vitrine somem da aba Pendentes e não aprovam.
//
// ── ESPELHO PG (degradação graciosa, como links_portal.go / vitrine.go) ───────
//
//	senderzz_affiliates (schema-affiliates.sql) tem só: id, produtor_id, afiliado_id,
//	  produto_id, status(pending|active|paused|revoked), comissao_pct, created_at,
//	  updated_at. NÃO tem approved_at nem deleted_at (não SELECIONAR — 42703).
//	  → aba Pendentes = status 'pending'; aba Aprovados = status 'active'.
//	  → reject/delete = UPDATE status='revoked' (soft, dentro do CHECK; mantém a
//	    linha p/ a UNIQUE da vitrine bloquear re-solicitação, espelhando o
//	    deleted_at do WP que também preserva a linha).
//	Config do produtor: senderzz_portal_user_meta (UNIQUE user_id,meta_key),
//	  keyed por PORTAL id (u.ID) — round-trip com admin affiliates.go que LÊ por id.
//	Links de convite: WP usa sz_invite_links (MySQL); SEM espelho PG e infra fora de
//	  escopo → GET devolve invite_links:[]; POST/DELETE devolvem 501 PT-BR claro
//	  (NÃO fingir sucesso — espelha links_portal CommissionUpdate).
package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/portal-service/internal/auth"
	"github.com/senderzz/portal-service/internal/httpx"
)

// AffiliatesHandler agrupa as dependências dos handlers de afiliados.
// Construção idêntica a WebhookHandler/LinksHandler/VitrineHandler — o integrador
// monta com &handlers.AffiliatesHandler{Pool: pool}.
type AffiliatesHandler struct {
	Pool *pgxpool.Pool
}

// Tetos das sub-listas da seção Afiliados. AUDIT PERF-list-endpoints-hard-limit:
// cada helper busca limit+1 p/ sinalizar truncamento (has_more por sub-lista) sem
// COUNT. O envelope da List é multi-lista — por isso os flags são por sub-lista
// (pending/approved_has_more, repasse_has_more, my_affiliations_has_more,
// my_links_has_more), ACRESCENTADOS sem alterar as chaves existentes.
const (
	listAffiliatesLimit     = 200 // pending + approved (split de uma só query)
	listRepasseLimit        = 200 // repasse mensal por afiliado
	listMyAffiliationsLimit = 100 // "Minha afiliação"
	listAffLinksLimit       = 100 // links de venda do afiliado
)

// ── Shapes de resposta (casam com Affiliates.tsx) ─────────────────────────────

// affiliateRow — um afiliado nas abas Pendentes / Aprovados (visão produtor).
type affiliateRow struct {
	ID            int64   `json:"id"`             // senderzz_affiliates.id (PK do vínculo)
	Name          string  `json:"name"`           // display_name do afiliado (fallback email / "—")
	Email         string  `json:"email"`          // email do afiliado
	Status        string  `json:"status"`         // pending | active
	CommissionPct float64 `json:"commission_pct"` // comissao_pct do vínculo
	CreatedAt     string  `json:"created_at"`     // YYYY-MM-DD
	ApprovedAt    *string `json:"approved_at"`    // não migrado no espelho PG → sempre null
}

// myAffiliationRow — vínculo do usuário COMO afiliado de outros produtores.
type myAffiliationRow struct {
	ID            int64   `json:"id"`
	ProducerName  string  `json:"producer_name"`
	Status        string  `json:"status"`
	CommissionPct float64 `json:"commission_pct"`
	CreatedAt     string  `json:"created_at"`
	ApprovedAt    *string `json:"approved_at"` // não migrado → null
}

// affLink — link de venda disponível ao afiliado (reusa shape do React AffLink).
// FEAT-AFF-PRODUCT-VIEW (2026-06-24): + display_value (preço da oferta) e base_name
// ("{qtd} {produto}" sem estágio, migração 471) p/ o front agrupar por PRODUTO,
// filtrar por QUANTIDADE e exibir valor do link + valor da comissão (= preço × %).
type affLink struct {
	Name          string  `json:"name"`
	URL           string  `json:"url"`
	CommissionPct float64 `json:"commission_pct"`
	DisplayValue  float64 `json:"display_value"`
	BaseName      string  `json:"base_name"`
}

// inviteLinkRow — link de convite do produtor (casa com o type InviteLink do
// React Affiliates.tsx: { id, url, uses, created_at }). Espelho PG real =
// senderzz_affiliate_invites (a MESMA tabela que go/affiliates já usa). `uses`
// é 0/1 derivado de used_at (a tabela modela convite de uso único: used_at).
type inviteLinkRow struct {
	ID        int64  `json:"id"`
	URL       string `json:"url"`
	Uses      int    `json:"uses"`
	CreatedAt string `json:"created_at"`
}

// repasseRow — comissão mensal por afiliado (visão produtor; gated length>0 no React).
type repasseRow struct {
	Affiliate string  `json:"affiliate"`
	Month     string  `json:"month"` // YYYY-MM
	Total     float64 `json:"total"`
}

// commissionBody — body de POST /portal/affiliates/{id}/commission e default-commission.
type commissionBody struct {
	CommissionPct float64 `json:"commission_pct"`
}

// autoApproveBody — body de POST /portal/affiliates/auto-approve.
type autoApproveBody struct {
	Enabled bool `json:"enabled"`
}

// ── Helpers de option/meta (escopo de governança) ─────────────────────────────

// globalDefaultCommissionPct lê o option global sz_aff_default_commission_pct
// (senderzz_options) com fallback 10 e clamp 0..100 — espelha
// sz_aff_default_commission_pct() do WP. Reusa o parser do vitrine.go via
// VitrineHandler? Não — mantém local p/ evitar coupling; lógica idêntica.
func (h *AffiliatesHandler) globalDefaultCommissionPct(ctx context.Context) float64 {
	var raw string
	err := h.Pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE name = $1`,
		"sz_aff_default_commission_pct",
	).Scan(&raw)
	if err != nil {
		return defaultAffiliateCommissionPct
	}
	v, perr := strconv.ParseFloat(replaceCommaDot(raw), 64)
	if perr != nil || v <= 0 {
		return defaultAffiliateCommissionPct
	}
	if v > 100 {
		v = 100
	}
	return v
}

// producerDefaultCommissionPct lê a comissão padrão DO PRODUTOR
// (senderzz_portal_user_meta '_sz_aff_default_commission_pct', keyed por PORTAL id)
// com fallback para o global — espelha sz_aff_producer_default_commission_pct().
func (h *AffiliatesHandler) producerDefaultCommissionPct(ctx context.Context, portalID int64) float64 {
	var raw string
	err := h.Pool.QueryRow(ctx,
		`SELECT meta_value FROM senderzz_portal_user_meta
		  WHERE user_id = $1 AND meta_key = '_sz_aff_default_commission_pct'
		  LIMIT 1`,
		portalID,
	).Scan(&raw)
	if err != nil {
		return h.globalDefaultCommissionPct(ctx)
	}
	v, perr := strconv.ParseFloat(replaceCommaDot(raw), 64)
	if perr != nil {
		return h.globalDefaultCommissionPct(ctx)
	}
	if v < 0 {
		v = 0
	}
	if v > 100 {
		v = 100
	}
	return v
}

// perAffiliateOverrideEnabled lê o toggle GLOBAL sz_aff_per_affiliate_override em
// senderzz_options (default '0'). É a MESMA option que go/orders lê na resolução de
// comissão do checkout (orders/.../checkout.go perAffiliateOverrideEnabled) — por
// isso vive em senderzz_options (GLOBAL), NÃO em senderzz_portal_user_meta. Espelha
// o parser do go/orders: aceita '1'/'true'/'yes'/'on' como ligado; qualquer erro /
// option ausente ⇒ false (tier de override desligado). Sem portalID: é global.
func (h *AffiliatesHandler) perAffiliateOverrideEnabled(ctx context.Context) bool {
	var raw string
	err := h.Pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE name = $1 LIMIT 1`,
		"sz_aff_per_affiliate_override",
	).Scan(&raw)
	if err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// producerAutoApprove lê a flag de aprovação automática DO PRODUTOR
// (meta '_sz_aff_auto_approve' = '1', keyed por PORTAL id) — espelha
// sz_aff_is_producer_auto_approve().
func (h *AffiliatesHandler) producerAutoApprove(ctx context.Context, portalID int64) bool {
	var raw string
	err := h.Pool.QueryRow(ctx,
		`SELECT meta_value FROM senderzz_portal_user_meta
		  WHERE user_id = $1 AND meta_key = '_sz_aff_auto_approve'
		  LIMIT 1`,
		portalID,
	).Scan(&raw)
	if err != nil {
		return false
	}
	return raw == "1"
}

// upsertProducerMeta grava uma meta do produtor em senderzz_portal_user_meta,
// keyed por PORTAL id (UNIQUE user_id,meta_key) — round-trip com admin affiliates.go.
func (h *AffiliatesHandler) upsertProducerMeta(ctx context.Context, portalID int64, key, value string) error {
	_, err := h.Pool.Exec(ctx,
		`INSERT INTO senderzz_portal_user_meta (user_id, meta_key, meta_value)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (user_id, meta_key)
		 DO UPDATE SET meta_value = EXCLUDED.meta_value`,
		portalID, key, value,
	)
	return err
}

// requireProdutorGovernanceContext valida que o autenticado é PRODUTOR para
// escrever GOVERNANÇA do programa de afiliados (comissão padrão, auto-aprovação,
// aprovar/recusar/comissão de vínculo, exclusão, convites). Devolve o PortalUser
// quando ok; caso contrário escreve o erro HTTP e devolve nil.
//
// SEC-RBAC-AFF-GOVERNANCE (auditoria): a governança é EXCLUSIVA do produtor.
// Antes os gates barravam só o afiliado (isAffiliateRole) — o OPERATOR (OL)
// passava e podia escrever a governança de afiliados de produtores. Aqui o gate
// é POSITIVO (isProdutorRole): afiliado E operator/operador recebem 403.
// O role vem SEMPRE da sessão (auth.FromContext) — nunca de body/query.
func requireProdutorGovernanceContext(w http.ResponseWriter, r *http.Request) *auth.PortalUser {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return nil
	}
	// Gate positivo: só o produtor governa o programa de afiliados.
	// Barra afiliado E operator (OL) — ambos recebem 403.
	if !isProdutorRole(u.Role) {
		httpx.WriteErr(w, http.StatusForbidden, "Sem permissão.")
		return nil
	}
	return u
}

// ── GET /portal/affiliates ────────────────────────────────────────────────────

// List monta o payload completo da seção (escopado por sessão). Sempre devolve
// todas as chaves (pending/approved/my_affiliations/my_links/repasse/config) —
// o React renderiza o que precisa por role. Degrada a listas vazias / defaults
// se uma tabela do espelho ainda não existir (como vitrine.go / links_portal.go).
func (h *AffiliatesHandler) List(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	ctx := r.Context()

	resp := map[string]any{
		"pending":                []affiliateRow{},
		"approved":               []affiliateRow{},
		"my_affiliations":        []myAffiliationRow{},
		"my_links":               []affLink{},
		"repasse":                []repasseRow{},
		"invite_links":           []inviteLinkRow{}, // populado abaixo p/ produtor
		"default_commission_pct": h.producerDefaultCommissionPct(ctx, u.ID),
		"auto_approve":           h.producerAutoApprove(ctx, u.ID),
		// per_affiliate_override: toggle GLOBAL (senderzz_options) lido por go/orders
		// no checkout. Exposto aqui p/ o front renderizar o estado atual e permitir
		// alternar via POST /portal/affiliates/per-affiliate-override (produtor).
		"per_affiliate_override": h.perAffiliateOverrideEnabled(ctx),
		"role":                   u.Role,
		"is_affiliate":           isAffiliateRole(u.Role),
		// Envelope multi-lista: has_more POR sub-lista (acrescentado, não muda shape).
		// // PERF-list-endpoints-hard-limit
		"pending_has_more":         false,
		"approved_has_more":        false,
		"repasse_has_more":         false,
		"my_affiliations_has_more": false,
		"my_links_has_more":        false,
	}

	// ── Visão PRODUTOR: pendentes + aprovados dos vínculos DELE. ───────────────
	// OR-form no produtor (produtor_id = portalID OR wp_user_id) — ver doc do pacote.
	// Nome/email do afiliado via LEFT JOIN por wp_user_id SEM filtro de role (o
	// pendente pode ainda ser cliente/produtor — filtrar role apagaria o nome).
	// pending+approved saem de uma única query → o has_more truncado vale p/ os dois.
	pending, approved, paHasMore := h.listProducerAffiliates(ctx, u.ID, u.WPUserID)
	resp["pending"] = pending
	resp["approved"] = approved
	resp["pending_has_more"] = paHasMore
	resp["approved_has_more"] = paHasMore

	// ── Repasse mensal por afiliado (produtor) — opcional, gated length>0. ──────
	repasse, repasseHasMore := h.listRepasse(ctx, u.ID, u.WPUserID)
	resp["repasse"] = repasse
	resp["repasse_has_more"] = repasseHasMore

	// ── Visão "Minha afiliação": vínculos do usuário COMO afiliado de outros. ──
	// Atribuição ESTRITA por wp_user_id (afiliado_id) — nunca OR com portal id.
	myAff, myAffHasMore := h.listMyAffiliations(ctx, u.WPUserID)
	resp["my_affiliations"] = myAff
	resp["my_affiliations_has_more"] = myAffHasMore

	// ── Links de venda do afiliado (mesma fonte de links_portal.listForAffiliate).
	myLinks, myLinksHasMore := h.listAffiliateLinks(ctx, u)
	resp["my_links"] = myLinks
	resp["my_links_has_more"] = myLinksHasMore

	// ── Links de convite do produtor (ativos: não usados e não expirados). ──────
	// listInvites NÃO tem LIMIT (não trunca) → sem flag (non-destructive).
	resp["invite_links"] = h.listInvites(ctx, u)

	httpx.WriteOK(w, resp)
}

// listProducerAffiliates retorna (pendentes, aprovados, hasMore) dos vínculos do
// produtor. hasMore=true quando o fetch bateu no teto (pending+approved truncados).
// // PERF-list-endpoints-hard-limit
func (h *AffiliatesHandler) listProducerAffiliates(ctx context.Context, portalID, wpUserID int64) ([]affiliateRow, []affiliateRow, bool) {
	pending := []affiliateRow{}
	approved := []affiliateRow{}

	rows, err := h.Pool.Query(ctx,
		`SELECT a.id,
		        COALESCE(NULLIF(af.nome, ''), af.email, '—') AS name,
		        COALESCE(af.email, '')                       AS email,
		        a.status,
		        COALESCE(a.comissao_pct, 0)::float8          AS commission_pct,
		        a.created_at::text                           AS created_at
		   FROM senderzz_affiliates a
		   LEFT JOIN senderzz_portal_users af ON af.wp_user_id = a.afiliado_id
		  WHERE (a.produtor_id = $1 OR a.produtor_id = $2)
		    AND a.status IN ('pending', 'active')
		  ORDER BY CASE a.status WHEN 'pending' THEN 0 ELSE 1 END, a.created_at DESC
		  LIMIT $3`,
		portalID, wpUserID, listAffiliatesLimit+1, // N+1
	)
	if err != nil {
		return pending, approved, false // degrada (tabela ausente etc.)
	}
	defer rows.Close()

	fetched := 0
	for rows.Next() {
		fetched++
		if fetched > listAffiliatesLimit {
			break // sentinela: só sinaliza has_more, não inclui.
		}
		var ar affiliateRow
		var createdAt string
		if err := rows.Scan(&ar.ID, &ar.Name, &ar.Email, &ar.Status, &ar.CommissionPct, &createdAt); err != nil {
			return pending, approved, false
		}
		ar.CreatedAt = truncDate(createdAt)
		ar.ApprovedAt = nil // não migrado no espelho PG
		if ar.Status == "pending" {
			pending = append(pending, ar)
		} else {
			approved = append(approved, ar)
		}
	}
	return pending, approved, fetched > listAffiliatesLimit
}

// listRepasse soma a comissão (sz_orders.affiliate_amount) por afiliado + mês,
// para os pedidos do PRODUTOR. Espelha o "Repasse mensal por afiliado" do WP
// (agrupa por affiliate_name|YYYY-MM). Degrada a [] se a coluna/tabela faltar.
func (h *AffiliatesHandler) listRepasse(ctx context.Context, portalID, wpUserID int64) ([]repasseRow, bool) {
	out := []repasseRow{}
	rows, err := h.Pool.Query(ctx,
		`SELECT COALESCE(NULLIF(af.nome, ''), af.email, '—') AS affiliate,
		        to_char(o.created_at, 'YYYY-MM')             AS month,
		        COALESCE(SUM(o.affiliate_amount), 0)::float8 AS total
		   FROM sz_orders o
		   LEFT JOIN senderzz_portal_users af ON af.wp_user_id = o.affiliate_id
		  WHERE (o.produtor_id = $1 OR o.produtor_id = $2)
		    AND o.affiliate_id IS NOT NULL
		    AND COALESCE(o.affiliate_amount, 0) > 0
		  GROUP BY affiliate, to_char(o.created_at, 'YYYY-MM')
		  HAVING SUM(o.affiliate_amount) > 0
		  ORDER BY month DESC
		  LIMIT $3`,
		portalID, wpUserID, listRepasseLimit+1, // N+1 // PERF-list-endpoints-hard-limit
	)
	if err != nil {
		return out, false // degrada (coluna affiliate_amount ausente / tabela ausente)
	}
	defer rows.Close()
	for rows.Next() {
		var rr repasseRow
		if err := rows.Scan(&rr.Affiliate, &rr.Month, &rr.Total); err != nil {
			return out, false
		}
		out = append(out, rr)
	}
	hasMore := len(out) > listRepasseLimit
	if hasMore {
		out = out[:listRepasseLimit]
	}
	return out, hasMore
}

// listMyAffiliations retorna os vínculos do usuário COMO afiliado de outros
// produtores. Atribuição ESTRITA: afiliado_id = wp_user_id (nunca OR portal id).
// Produtor via join canônico (id OR wp_user_id) com role='produtor'.
func (h *AffiliatesHandler) listMyAffiliations(ctx context.Context, wpUserID int64) ([]myAffiliationRow, bool) {
	out := []myAffiliationRow{}
	if wpUserID == 0 {
		return out, false
	}
	rows, err := h.Pool.Query(ctx,
		`SELECT a.id,
		        COALESCE(NULLIF(p.nome, ''), p.email, '—') AS producer_name,
		        a.status,
		        COALESCE(a.comissao_pct, 0)::float8        AS commission_pct,
		        a.created_at::text                         AS created_at
		   FROM senderzz_affiliates a
		   LEFT JOIN senderzz_portal_users p
		          ON (p.id = a.produtor_id OR p.wp_user_id = a.produtor_id)
		         AND p.role = 'produtor'
		  WHERE a.afiliado_id = $1
		    AND a.status IN ('pending', 'active')
		  ORDER BY CASE a.status WHEN 'active' THEN 0 ELSE 1 END, a.created_at DESC
		  LIMIT $2`,
		wpUserID, listMyAffiliationsLimit+1, // N+1 // PERF-list-endpoints-hard-limit
	)
	if err != nil {
		return out, false
	}
	defer rows.Close()
	for rows.Next() {
		var mr myAffiliationRow
		var createdAt string
		if err := rows.Scan(&mr.ID, &mr.ProducerName, &mr.Status, &mr.CommissionPct, &createdAt); err != nil {
			return out, false
		}
		mr.CreatedAt = truncDate(createdAt)
		mr.ApprovedAt = nil
		out = append(out, mr)
	}
	hasMore := len(out) > listMyAffiliationsLimit
	if hasMore {
		out = out[:listMyAffiliationsLimit]
	}
	return out, hasMore
}

// listAffiliateLinks retorna os links de venda liberados ao usuário enquanto
// afiliado (affiliate_visible, tipo<>motoboy), dos produtores a que está
// vinculado (status='active'). Reproduz FIELMENTE links_portal.listForAffiliate
// no shape AffLink { name, url, commission_pct }, INCLUSIVE a url de rastreio
// com ?r=<token> (sz_aff_checkout_url_with_aff) — sem ela a venda do afiliado não
// é atribuída. token = id do VÍNCULO (senderzz_affiliates.id, PK), via
// appendRefToken (package-level, definido em links_portal.go).
func (h *AffiliatesHandler) listAffiliateLinks(ctx context.Context, u *auth.PortalUser) ([]affLink, bool) {
	out := []affLink{}
	if u.WPUserID == 0 {
		return out, false
	}

	// Salt de referência do afiliado (espelha get_option('sz_aff_ref_salt')).
	// Sem salt → token vazio; a url base ainda é copiável (degradação graciosa,
	// idêntica a links_portal.affiliateRefSalt).
	refSalt := ""
	_ = h.Pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE name = $1`,
		"sz_aff_ref_salt",
	).Scan(&refSalt)

	rows, err := h.Pool.Query(ctx,
		`SELECT cl.name,
		        cl.url,
		        cl.display_value,
		        COALESCE(cl.base_name, '') AS base_name,
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
		        ), 0)::float8 AS commission_pct
		   FROM senderzz_checkout_links cl
		  WHERE cl.affiliate_visible = TRUE
		    -- DONO 2026-06-24: link do AFILIADO = CASH ON DELIVERY (motoboy), NÃO expedição.
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
		u.WPUserID, listAffLinksLimit+1, // N+1 // PERF-list-endpoints-hard-limit
	)
	if err != nil {
		return out, false
	}
	defer rows.Close()
	for rows.Next() {
		var lk affLink
		var vinculoID *int64
		if err := rows.Scan(&lk.Name, &lk.URL, &lk.DisplayValue, &lk.BaseName, &vinculoID, &lk.CommissionPct); err != nil {
			return out, false
		}
		// url de rastreio do afiliado: url + ?r=token (token = id do vínculo).
		if vinculoID != nil && *vinculoID > 0 && lk.URL != "" {
			lk.URL = appendRefToken(lk.URL, *vinculoID, refSalt)
		}
		out = append(out, lk)
	}
	hasMore := len(out) > listAffLinksLimit
	if hasMore {
		out = out[:listAffLinksLimit]
	}
	return out, hasMore
}

// ── Ownership guard (produtor) ────────────────────────────────────────────────

// ownsAffiliate verifica que o vínculo {id} pertence ao produtor logado (OR-form)
// e está ativo (status<>'revoked'). Retorna (existe, status, afiliadoID).
func (h *AffiliatesHandler) ownsAffiliate(ctx context.Context, affID, portalID, wpUserID int64) (bool, string, int64) {
	var status string
	var afiliadoID int64
	err := h.Pool.QueryRow(ctx,
		`SELECT status, afiliado_id
		   FROM senderzz_affiliates
		  WHERE id = $1
		    AND (produtor_id = $2 OR produtor_id = $3)
		    AND status <> 'revoked'
		  LIMIT 1`,
		affID, portalID, wpUserID,
	).Scan(&status, &afiliadoID)
	if err != nil {
		return false, "", 0
	}
	return true, status, afiliadoID
}

// ── POST /portal/affiliates/{id}/approve ──────────────────────────────────────

// Approve aprova um afiliado pendente (status → 'active'). Exclusivo do produtor.
func (h *AffiliatesHandler) Approve(w http.ResponseWriter, r *http.Request) {
	h.transition(w, r, "active", "Afiliado aprovado com sucesso!")
}

// ── POST /portal/affiliates/{id}/reject ───────────────────────────────────────

// Reject recusa um afiliado (status → 'revoked', soft). Exclusivo do produtor.
func (h *AffiliatesHandler) Reject(w http.ResponseWriter, r *http.Request) {
	h.transition(w, r, "revoked", "Afiliado recusado.")
}

// transition aplica a mudança de status de um vínculo do produtor (approve/reject).
func (h *AffiliatesHandler) transition(w http.ResponseWriter, r *http.Request, newStatus, okMsg string) {
	// SEC-RBAC-AFF-GOVERNANCE: aprovar/recusar é EXCLUSIVO do produtor (barra
	// afiliado E operator). O ownership (ownsAffiliate) reforça por dono.
	u := requireProdutorGovernanceContext(w, r)
	if u == nil {
		return
	}
	affID, ok := parseAffiliateID(r)
	if !ok {
		httpx.WriteErr(w, http.StatusBadRequest, "ID inválido.")
		return
	}
	ctx := r.Context()

	owns, _, _ := h.ownsAffiliate(ctx, affID, u.ID, u.WPUserID)
	if !owns {
		httpx.WriteErr(w, http.StatusNotFound, "Afiliado não encontrado.")
		return
	}

	res, err := h.Pool.Exec(ctx,
		`UPDATE senderzz_affiliates
		    SET status = $1, updated_at = NOW()
		  WHERE id = $2 AND (produtor_id = $3 OR produtor_id = $4) AND status <> 'revoked'`,
		newStatus, affID, u.ID, u.WPUserID,
	)
	if err != nil {
		if isUndefinedTable(err) {
			httpx.WriteErr(w, http.StatusServiceUnavailable, "recurso indisponível no momento")
			return
		}
		httpx.WriteErr(w, http.StatusInternalServerError, "Erro ao processar afiliado.")
		return
	}
	if res.RowsAffected() == 0 {
		httpx.WriteErr(w, http.StatusNotFound, "Afiliado não encontrado.")
		return
	}

	// PROMOÇÃO cliente→afiliado: ao APROVAR (status 'active'), o usuário que entrou
	// como 'cliente' (signup sem aprovação) vira 'afiliado'. GUARD role='cliente' →
	// NUNCA rebaixa afiliado nem toca produtor/operator. Best-effort (não derruba o approve).
	if newStatus == "active" {
		_, _ = h.Pool.Exec(ctx,
			`UPDATE senderzz_portal_users
			    SET role = 'afiliado'
			  WHERE role = 'cliente'
			    AND wp_user_id = (SELECT afiliado_id FROM senderzz_affiliates WHERE id = $1)`,
			affID)
	}

	httpx.WriteOK(w, map[string]any{"message": okMsg})
}

// ── POST /portal/affiliates/{id}/commission ───────────────────────────────────

// Commission atualiza a comissão % de um vínculo aprovado. Exclusivo do produtor.
func (h *AffiliatesHandler) Commission(w http.ResponseWriter, r *http.Request) {
	// SEC-RBAC-AFF-GOVERNANCE: comissão por-vínculo é EXCLUSIVA do produtor
	// (barra afiliado E operator). Ownership reforça por dono.
	u := requireProdutorGovernanceContext(w, r)
	if u == nil {
		return
	}
	affID, ok := parseAffiliateID(r)
	if !ok {
		httpx.WriteErr(w, http.StatusBadRequest, "ID inválido.")
		return
	}
	var body commissionBody
	if err := decodeJSONBody(r, &body); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}
	if body.CommissionPct < 0 || body.CommissionPct > 100 {
		httpx.WriteErr(w, http.StatusBadRequest, "A comissão deve ficar entre 0% e 100%.")
		return
	}
	ctx := r.Context()

	res, err := h.Pool.Exec(ctx,
		`UPDATE senderzz_affiliates
		    SET comissao_pct = $1, updated_at = NOW()
		  WHERE id = $2 AND (produtor_id = $3 OR produtor_id = $4) AND status <> 'revoked'`,
		body.CommissionPct, affID, u.ID, u.WPUserID,
	)
	if err != nil {
		if isUndefinedTable(err) {
			httpx.WriteErr(w, http.StatusServiceUnavailable, "recurso indisponível no momento")
			return
		}
		httpx.WriteErr(w, http.StatusInternalServerError, "Erro ao salvar comissão.")
		return
	}
	if res.RowsAffected() == 0 {
		httpx.WriteErr(w, http.StatusNotFound, "Afiliado não encontrado.")
		return
	}
	httpx.WriteOK(w, map[string]any{"message": "Comissão atualizada.", "commission_pct": body.CommissionPct})
}

// ── DELETE /portal/affiliates/{id} ────────────────────────────────────────────

// Delete exclui (soft, status='revoked') um vínculo do produtor. Exclusivo do produtor.
func (h *AffiliatesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	// SEC-RBAC-AFF-GOVERNANCE: excluir vínculo é EXCLUSIVO do produtor (barra
	// afiliado E operator). Ownership reforça por dono.
	u := requireProdutorGovernanceContext(w, r)
	if u == nil {
		return
	}
	affID, ok := parseAffiliateID(r)
	if !ok {
		httpx.WriteErr(w, http.StatusBadRequest, "ID inválido.")
		return
	}
	ctx := r.Context()

	res, err := h.Pool.Exec(ctx,
		`UPDATE senderzz_affiliates
		    SET status = 'revoked', updated_at = NOW()
		  WHERE id = $1 AND (produtor_id = $2 OR produtor_id = $3) AND status <> 'revoked'`,
		affID, u.ID, u.WPUserID,
	)
	if err != nil {
		if isUndefinedTable(err) {
			httpx.WriteErr(w, http.StatusServiceUnavailable, "recurso indisponível no momento")
			return
		}
		httpx.WriteErr(w, http.StatusInternalServerError, "Erro ao excluir.")
		return
	}
	if res.RowsAffected() == 0 {
		httpx.WriteErr(w, http.StatusNotFound, "Afiliado não encontrado.")
		return
	}
	httpx.WriteOK(w, map[string]any{"message": "Afiliado removido."})
}

// ── POST /portal/affiliates/default-commission ────────────────────────────────

// DefaultCommission salva a comissão padrão do produtor (governança: do produtor).
// Persiste em senderzz_portal_user_meta keyed por portal id.
func (h *AffiliatesHandler) DefaultCommission(w http.ResponseWriter, r *http.Request) {
	// SEC-RBAC-AFF-GOVERNANCE: comissão padrão é EXCLUSIVA do produtor (barra
	// afiliado E operator).
	u := requireProdutorGovernanceContext(w, r)
	if u == nil {
		return
	}
	var body commissionBody
	if err := decodeJSONBody(r, &body); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}
	if body.CommissionPct < 0 || body.CommissionPct > 100 {
		httpx.WriteErr(w, http.StatusBadRequest, "A comissão deve ficar entre 0% e 100%.")
		return
	}
	// Grava com ponto decimal e 2 casas (espelha number_format(...,'.') do WP).
	val := strconv.FormatFloat(body.CommissionPct, 'f', 2, 64)
	if err := h.upsertProducerMeta(r.Context(), u.ID, "_sz_aff_default_commission_pct", val); err != nil {
		if isUndefinedTable(err) {
			httpx.WriteErr(w, http.StatusServiceUnavailable, "recurso indisponível no momento")
			return
		}
		httpx.WriteErr(w, http.StatusInternalServerError, "Erro ao salvar.")
		return
	}
	httpx.WriteOK(w, map[string]any{
		"message":        "Comissão padrão salva.",
		"commission_pct": body.CommissionPct,
	})
}

// ── POST /portal/affiliates/auto-approve ──────────────────────────────────────

// AutoApprove liga/desliga a aprovação automática do produtor (governança: do produtor).
func (h *AffiliatesHandler) AutoApprove(w http.ResponseWriter, r *http.Request) {
	// SEC-RBAC-AFF-GOVERNANCE: aprovação automática é EXCLUSIVA do produtor
	// (barra afiliado E operator).
	u := requireProdutorGovernanceContext(w, r)
	if u == nil {
		return
	}
	var body autoApproveBody
	if err := decodeJSONBody(r, &body); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}
	val := "0"
	if body.Enabled {
		val = "1"
	}
	if err := h.upsertProducerMeta(r.Context(), u.ID, "_sz_aff_auto_approve", val); err != nil {
		if isUndefinedTable(err) {
			httpx.WriteErr(w, http.StatusServiceUnavailable, "recurso indisponível no momento")
			return
		}
		httpx.WriteErr(w, http.StatusInternalServerError, "Erro ao alterar aprovação automática.")
		return
	}
	msg := "Aprovação automática desativada."
	if body.Enabled {
		msg = "Aprovação automática ativada."
	}
	httpx.WriteOK(w, map[string]any{"message": msg, "auto_approve": body.Enabled})
}

// ── POST /portal/affiliates/per-affiliate-override ────────────────────────────

// PerAffiliateOverride liga/desliga o toggle GLOBAL sz_aff_per_affiliate_override.
// Quando ON, a comissão custom por-vínculo (comissao_pct do afiliado) tem precedência
// sobre a oferta/link na resolução do checkout (go/orders). Quando OFF, a oferta manda.
//
// IMPORTANTE: é uma option GLOBAL em senderzz_options (a MESMA que go/orders lê), NÃO
// uma meta por-produtor. Por isso o setter faz UPSERT em senderzz_options (autoload
// 'yes' p/ o WP autoloadar), espelhando o padrão de checkout_commission_test.go. Gate
// de governança = produtor (barra afiliado E operator, igual a AutoApprove).
func (h *AffiliatesHandler) PerAffiliateOverride(w http.ResponseWriter, r *http.Request) {
	// SEC-RBAC-AFF-GOVERNANCE: toggle de override é governança EXCLUSIVA do produtor.
	u := requireProdutorGovernanceContext(w, r)
	if u == nil {
		return
	}
	var body autoApproveBody
	if err := decodeJSONBody(r, &body); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}
	val := "0"
	if body.Enabled {
		val = "1"
	}
	_, err := h.Pool.Exec(r.Context(),
		`INSERT INTO senderzz_options (name, value, autoload)
		 VALUES ('sz_aff_per_affiliate_override', $1, 'yes')
		 ON CONFLICT (name) DO UPDATE SET value = EXCLUDED.value`,
		val,
	)
	if err != nil {
		if isUndefinedTable(err) {
			httpx.WriteErr(w, http.StatusServiceUnavailable, "recurso indisponível no momento")
			return
		}
		httpx.WriteErr(w, http.StatusInternalServerError, "Erro ao alterar override de comissão.")
		return
	}
	msg := "Override de comissão por afiliado desativado."
	if body.Enabled {
		msg = "Override de comissão por afiliado ativado."
	}
	httpx.WriteOK(w, map[string]any{"message": msg, "per_affiliate_override": body.Enabled})
}

// ── Links de convite — espelho PG REAL (senderzz_affiliate_invites) ───────────
//
// FEAT-INVITES (auditoria 2026-06-18): o stub 501 anterior dizia "sz_invite_links
// não migrado". Mas o espelho PG correto JÁ EXISTE: senderzz_affiliate_invites
// (a mesma tabela que go/affiliates usa em /affiliates/invites — token 64-hex,
// expira 7d, used_at de uso único). O portal só nunca falava com ela. Aqui o
// produtor cria/lista/revoga convites genéricos compartilháveis; RedeemInvite
// fecha o ciclo (token → vínculo em senderzz_affiliates) p/ não ser URL morta.
//
// id-space: senderzz_affiliate_invites.produtor_id = PORTAL id (u.ID), igual ao
// go/affiliates CreateInvite (user.ID). email='' = convite genérico (não
// direcionado a um e-mail específico — o front não envia e-mail, é link público).

// inviteFarFuture — validade do convite DETERMINÍSTICO (#19). O link é FIXO e
// estável, então não expira na prática; usamos um horizonte distante p/ satisfazer
// a coluna expires_at NOT NULL e manter os filtros `expires_at > NOW()` existentes
// (listInvites/redeem) válidos sem reescrevê-los.
const inviteFarFuture = 100 * 365 * 24 * time.Hour

// deterministicInviteToken — #19: token FIXO por produtor = HMAC-SHA256(produtor_id,
// secret) em hex (64 chars, cabe em VARCHAR(64)). Estável: o MESMO produtor sempre
// gera o MESMO token → o link nunca muda (não exige "Gerar link"). A mensagem é o
// produtor_id (= PORTAL id, u.ID) — casa com a coluna produtor_id armazenada.
//
// Secret: WP_SALT_AUTH (reusa o mesmo salt das sessões; garantidamente não-vazio em
// handler autenticado — o middleware de auth 503 se ausente). Fallback JWT_SECRET por
// robustez. O secret NUNCA é logado/exposto; só o HMAC (não invertível) vai p/ o link.
func deterministicInviteToken(producerID int64) string {
	secret := os.Getenv("WP_SALT_AUTH")
	if secret == "" {
		secret = os.Getenv("JWT_SECRET")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(producerID, 10)))
	return hex.EncodeToString(mac.Sum(nil))
}

// ensureInviteRow garante que a linha do convite determinístico do produtor EXISTE
// (idempotente). É necessária porque o HMAC não é invertível: no redeem precisamos
// recuperar o produtor_id a partir do token, e isso só é possível lendo a linha.
// ON CONFLICT (token) DO NOTHING → chamar N vezes não cria N linhas (#19 idempotente).
// Devolve (id, created_at) da linha (existente ou recém-criada).
func (h *AffiliatesHandler) ensureInviteRow(ctx context.Context, producerID int64, token string) (int64, time.Time, error) {
	expiresAt := time.Now().UTC().Add(inviteFarFuture)
	var (
		id      int64
		created time.Time
	)
	// CTE: tenta inserir; se o token já existe (conflito), lê a linha existente.
	err := h.Pool.QueryRow(ctx, `
		WITH ins AS (
			INSERT INTO senderzz_affiliate_invites (produtor_id, email, token, expires_at)
			VALUES ($1, '', $2, $3)
			ON CONFLICT (token) DO NOTHING
			RETURNING id, created_at
		)
		SELECT id, created_at FROM ins
		UNION ALL
		SELECT id, created_at FROM senderzz_affiliate_invites WHERE token = $2
		LIMIT 1`,
		producerID, token, expiresAt,
	).Scan(&id, &created)
	return id, created, err
}

// listInvites devolve o link de convite FIXO (determinístico) do produtor (#19).
// SEMPRE devolve o MESMO link (token estável) — não depende de "Gerar link". Afiliado
// não tem programa próprio → lista vazia (espelha a UI: só produtor vê).
//
// Uses sempre 0: o link é reusável (deterministicamente fixo), não é convite de uso
// único — a contagem de uso perdeu o sentido (a dedup do vínculo é o NOT EXISTS do
// redeem). Mantemos o campo p/ não mudar o shape consumido pelo React.
func (h *AffiliatesHandler) listInvites(ctx context.Context, u *auth.PortalUser) []inviteLinkRow {
	out := []inviteLinkRow{}
	if isAffiliateRole(u.Role) {
		return out
	}
	token := deterministicInviteToken(u.ID)
	id, created, err := h.ensureInviteRow(ctx, u.ID, token)
	if err != nil {
		// Degradação graciosa: ainda devolvemos o link (URL é função pura do token);
		// só não temos id/created da linha (tabela ausente etc.) → não quebra a UI.
		out = append(out, inviteLinkRow{
			ID:        0,
			URL:       inviteURL(token),
			Uses:      0,
			CreatedAt: time.Now().UTC().Format("2006-01-02"),
		})
		return out
	}
	out = append(out, inviteLinkRow{
		ID:        id,
		URL:       inviteURL(token),
		Uses:      0,
		CreatedAt: created.Format("2006-01-02"),
	})
	return out
}

// CreateInvite — #19: agora IDEMPOTENTE. Como o token é determinístico (HMAC do
// produtor_id), não há mais "criar" um novo link — só garantimos que a linha existe
// e devolvemos o link FIXO. Dois POSTs (ou GETs) devolvem o MESMO link. Mantida a
// rota p/ compatibilidade com o front (botão "Gerar link" continua funcionando, só
// que agora é idempotente em vez de criar convites novos).
func (h *AffiliatesHandler) CreateInvite(w http.ResponseWriter, r *http.Request) {
	// SEC-RBAC-AFF-GOVERNANCE: gerar convite é EXCLUSIVO do produtor (barra
	// afiliado E operator). Só o produtor tem programa próprio.
	u := requireProdutorGovernanceContext(w, r)
	if u == nil {
		return
	}

	token := deterministicInviteToken(u.ID)
	id, created, err := h.ensureInviteRow(r.Context(), u.ID, token)
	if err != nil {
		if isUndefinedTable(err) {
			httpx.WriteErr(w, http.StatusServiceUnavailable, "recurso indisponível no momento")
			return
		}
		httpx.WriteErr(w, http.StatusInternalServerError, "Erro ao criar link de convite.")
		return
	}

	link := inviteLinkRow{
		ID:        id,
		URL:       inviteURL(token),
		Uses:      0,
		CreatedAt: created.Format("2006-01-02"),
	}
	// Devolve o link FIXO E a lista (o front aceita ambos). Idempotente (#19).
	httpx.WriteOK(w, map[string]any{
		"message":      "Link de convite disponível.",
		"link":         link,
		"invite_links": h.listInvites(r.Context(), u),
	})
}

// RevokeInvite — #19: no modelo de link DETERMINÍSTICO/FIXO, "revogar" não se aplica:
// o próximo GET/CreateInvite re-cria a MESMA linha (token = HMAC estável do produtor).
// Deletar a linha aqui seria efêmero (ressurge no init seguinte). Mantemos a rota p/
// não quebrar o front, mas ela é um NO-OP explícito (responde 200 com aviso claro)
// em vez de prometer uma revogação que o modelo não honra. Para "desligar" o programa
// de afiliação, o produtor usa a governança (auto-aprovação off / recusar vínculos),
// não a revogação do link.
func (h *AffiliatesHandler) RevokeInvite(w http.ResponseWriter, r *http.Request) {
	// SEC-RBAC-AFF-GOVERNANCE: ação de governança é EXCLUSIVA do produtor.
	u := requireProdutorGovernanceContext(w, r)
	if u == nil {
		return
	}
	_, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "ID inválido.")
		return
	}
	httpx.WriteOK(w, map[string]any{
		"message": "O link de convite é fixo e não pode ser revogado. Aprovação de afiliados é controlada na governança.",
		"noop":    true,
	})
}

// RedeemInvite — POST /portal/affiliates/invites/redeem  body {token}.
// FECHA O CICLO (anti-"URL morta"): o usuário autenticado resgata o token FIXO do
// produtor e vira afiliado PENDENTE (aguardando aprovação). Consumidor real =
// senderzz_affiliates, lido por listMyAffiliations / vitrine / comissões.
//
// #19 — LINK DETERMINÍSTICO/REUSÁVEL: o token NÃO é mais de uso único. Por isso o
// redeem NÃO consome used_at (isso quebraria todos os resgates futuros do mesmo link).
// A DEDUP passa a ser o INSERT idempotente (NOT EXISTS): resgatar 2x não cria 2
// vínculos. Validamos só token + expires_at>NOW() p/ recuperar o produtor_id.
//
// STATUS = 'pending' (#19: "cria afiliação status pending (aprovação)") — antes era
// 'active' (pré-aprovado). Agora o link fixo gera um vínculo PENDENTE que o produtor
// aprova na seção Afiliados (Approve). Mais seguro: link público não auto-aprova.
//
// SEGURANÇA:
//   - Anti-auto-afiliação: o produtor não pode resgatar o próprio convite.
//   - Sem duplicar vínculo: INSERT só se ainda não existe (NOT EXISTS; tabela sem UNIQUE).
//   - Concorrência: dois redeems simultâneos podem criar 2 'pending' (benigno — ambos
//     aguardam a MESMA aprovação manual; a corrida não dá fundos nem auto-aprova).
func (h *AffiliatesHandler) RedeemInvite(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}
	body.Token = strings.TrimSpace(body.Token)
	if body.Token == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "Token de convite obrigatório.")
		return
	}

	ctx := r.Context()
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "Erro interno.")
		return
	}
	defer tx.Rollback(ctx)

	// Recupera o produtor_id pelo token FIXO (NÃO consome used_at — link reusável).
	// produtor_id = PORTAL id do emissor. Sem linha → token inválido/expirado.
	var producerID int64
	err = tx.QueryRow(ctx, `
		SELECT produtor_id
		  FROM senderzz_affiliate_invites
		 WHERE token = $1 AND expires_at > NOW()
		 LIMIT 1`,
		body.Token,
	).Scan(&producerID)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "Convite inválido ou expirado.")
		return
	}

	// Anti-auto-afiliação: o produtor (id portal) não resgata o próprio convite.
	// produtor_id é PORTAL id; comparamos com u.ID (portal) e u.WPUserID (defensivo).
	if producerID == u.ID || producerID == u.WPUserID {
		httpx.WriteErr(w, http.StatusBadRequest, "Você não pode usar seu próprio convite.")
		return
	}

	// Comissão = padrão do produtor (mesma regra de aprovação de afiliado).
	pct := h.producerDefaultCommissionPct(ctx, producerID)

	// Vínculo: produtor_id = PORTAL id do emissor; afiliado_id = wp_user_id do
	// resgatador (atribuição ESTRITA por wp_user_id — id-space canônico).
	// produto_id = 0 (afiliação geral). status = 'pending' (#19: aguarda aprovação).
	// INSERT idempotente: só se ainda não existe vínculo (tabela sem UNIQUE) → resgatar
	// o link fixo N vezes não cria N vínculos.
	_, err = tx.Exec(ctx, `
		INSERT INTO senderzz_affiliates (produtor_id, afiliado_id, produto_id, status, comissao_pct, created_at, updated_at)
		SELECT $1, $2, 0, 'pending', $3, NOW(), NOW()
		 WHERE NOT EXISTS (
		     SELECT 1 FROM senderzz_affiliates
		      WHERE produtor_id = $1 AND afiliado_id = $2 AND produto_id = 0
		 )`,
		producerID, u.WPUserID, pct,
	)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "Erro ao registrar afiliação.")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "Erro ao concluir.")
		return
	}
	httpx.WriteOK(w, map[string]any{
		"message":        "Solicitação de afiliação enviada! Aguarde a aprovação do produtor.",
		"producer_id":    producerID,
		"commission_pct": pct,
		"status":         "pending",
	})
}

// randomHex gera n bytes aleatórios (crypto/rand) em hex. Igual ao helper de
// go/affiliates: 32 bytes → 64 chars. Usado por links_portal.go (token de link de
// venda do afiliado). NÃO é mais usado p/ convites (#19: token determinístico).
func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// inviteURL monta a URL pública compartilhável do convite. Base configurável via
// PORTAL_PUBLIC_URL (fallback ao domínio do app). O front captura ?convite= e
// chama POST /portal/affiliates/invites/redeem após login.
func inviteURL(token string) string {
	base := strings.TrimRight(os.Getenv("PORTAL_PUBLIC_URL"), "/")
	if base == "" {
		base = "https://app.falkz.com.br"
	}
	return base + "/portal/?convite=" + token
}

// ── GET /portal/affiliates/export.csv ────────────────────────────────────────── // FEAT-PORTAL

// ExportCSV exporta a lista de COMISSÕES como CSV (Content-Type text/csv).
// BYPASSA httpx.WriteOK — escreve text/csv via encoding/csv.
//
// Recorte por papel (mesma governança da seção Afiliados):
//   - PRODUTOR : repasse mensal por afiliado (h.listRepasse — comissão paga A SEUS
//     afiliados, agrupada por afiliado|mês). Colunas: afiliado, mes, total.
//   - AFILIADO : os GANHOS DELE por mês (comissão das vendas atribuídas ao seu
//     wp_user_id). Colunas: produtor, mes, total. Escopo estrito o.affiliate_id =
//     u.WPUserID (NUNCA vê dados de outro afiliado nem do produtor além do nome).
//   - OPERATOR/demais : CSV só com cabeçalho (sem escopo de comissão).
//
// SEGURANÇA: o afiliado nunca recebe o repasse a TERCEIROS — sua query é filtrada
// por affiliate_id = u.WPUserID. O produtor vê só os repasses dos próprios pedidos
// (produtor_id = portalID OR wpUserID). Nenhum vaza para o outro.
func (h *AffiliatesHandler) ExportCSV(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	ctx := r.Context()

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="comissoes.csv"`)
	w.WriteHeader(http.StatusOK)

	cw := csv.NewWriter(w)
	defer cw.Flush()

	if isAffiliateRole(u.Role) {
		// AFILIADO: ganhos dele por produtor + mês.
		_ = cw.Write([]string{"produtor", "mes", "total"})
		for _, row := range h.listAffiliateEarnings(ctx, u.WPUserID) {
			_ = cw.Write([]string{row.Affiliate, row.Month, fmt.Sprintf("%.2f", row.Total)})
		}
		return
	}

	if u.Role == "produtor" {
		// PRODUTOR: repasse por afiliado + mês (mesma fonte da seção Afiliados).
		// O CSV ignora o has_more (export mantém o mesmo recorte da seção). // PERF-list-endpoints-hard-limit
		_ = cw.Write([]string{"afiliado", "mes", "total"})
		repasse, _ := h.listRepasse(ctx, u.ID, u.WPUserID)
		for _, row := range repasse {
			_ = cw.Write([]string{row.Affiliate, row.Month, fmt.Sprintf("%.2f", row.Total)})
		}
		return
	}

	// operator/demais: sem escopo de comissão → só cabeçalho.
	_ = cw.Write([]string{"afiliado", "mes", "total"})
}

// listAffiliateEarnings soma a comissão (sz_orders.affiliate_amount) do AFILIADO por
// produtor + mês — escopo estrito o.affiliate_id = wpUserID (nunca vê outro afiliado).
// Espelha listRepasse, mas pela ótica do afiliado (agrupa por nome do PRODUTOR).
func (h *AffiliatesHandler) listAffiliateEarnings(ctx context.Context, wpUserID int64) []repasseRow {
	out := []repasseRow{}
	if wpUserID == 0 {
		return out
	}
	rows, err := h.Pool.Query(ctx,
		`SELECT COALESCE(NULLIF(p.nome, ''), p.email, '—')   AS producer,
		        to_char(o.created_at, 'YYYY-MM')             AS month,
		        COALESCE(SUM(o.affiliate_amount), 0)::float8 AS total
		   FROM sz_orders o
		   LEFT JOIN senderzz_portal_users p
		          ON (p.id = o.produtor_id OR p.wp_user_id = o.produtor_id)
		         AND p.role = 'produtor'
		  WHERE o.affiliate_id = $1
		    AND COALESCE(o.affiliate_amount, 0) > 0
		  GROUP BY producer, to_char(o.created_at, 'YYYY-MM')
		  HAVING SUM(o.affiliate_amount) > 0
		  ORDER BY month DESC
		  LIMIT 200`,
		wpUserID,
	)
	if err != nil {
		return out // degrada (coluna/tabela ausente)
	}
	defer rows.Close()
	for rows.Next() {
		var rr repasseRow
		if err := rows.Scan(&rr.Affiliate, &rr.Month, &rr.Total); err != nil {
			return out
		}
		out = append(out, rr)
	}
	return out
}

// ── Helpers internos ──────────────────────────────────────────────────────────

// parseAffiliateID extrai e valida o {id} da rota (>0). Reusa o padrão de
// parseLinkID (links_portal.go) mas isolado p/ clareza neste handler.
func parseAffiliateID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// replaceCommaDot normaliza "12,50" → "12.50" e remove espaços nas pontas.
// Espelha str_replace(',', '.', trim(...)) do WP nos campos de comissão
// (mesmo padrão de defaultCommissionPct em vitrine.go).
func replaceCommaDot(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), ",", ".")
}
