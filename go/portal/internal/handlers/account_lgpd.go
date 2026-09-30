// Package handlers — LGPD: direito de acesso/portabilidade + consentimento.
//
// Rotas (namespace /wp-json/senderzz/v1), registradas perto de /portal/account/delete:
//
//	GET  /portal/account/export  — "baixar meus dados" (Art. 18, II/V — acesso/portabilidade)
//	POST /portal/account/consent — registra aceite versionado de política/termo (idempotente)
//
// ── Por que métodos de SettingsHandler ─────────────────────────────────────────
// SettingsHandler já é o dono de /portal/account/* (email, password, delete) e já
// está cabeado no main.go (só Pool). Manter aqui evita um handler novo no wiring.
//
// ── Escopo (fail-closed, equality estrita — NUNCA OR/IN no recorte do titular) ──
// TODO dado exportado é do PRÓPRIO usuário autenticado (auth.FromContext):
//   - perfil/settings/integrations : senderzz_portal_users.id = u.ID
//   - pedidos (resumo)             : recorte canônico de orders.go
//       produtor → o.produtor_id = u.ID (portal id)
//       afiliado → o.affiliate_id = u.WPUserID (wp_user_id, NUNCA IN(id,wp_user_id))
//   - afiliações (como afiliado)   : senderzz_affiliates.afiliado_id = u.WPUserID
//   - sessões ativas               : senderzz_portal_sessions.user_id = u.ID
//
// ── O que NUNCA sai na exportação (PII própria != credenciais/segredos) ─────────
//   - password_hash (perfil)         — credencial, nunca exportada.
//   - token / token_hmac (sessões)   — material de sessão; só metadados (ip/ua/datas).
//   - secrets de integração          — o blob `integrations` do espelho PG NÃO guarda
//     secret (rotate é 501 por isso), mas defensivamente removemos qualquer chave
//     contendo "secret"/"token"/"password" antes de serializar (future-proof).
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/portal-service/internal/auth"
	"github.com/senderzz/portal-service/internal/httpx"
)

// ── Shapes da exportação ───────────────────────────────────────────────────────

// exportProfile espelha o perfil do titular SEM credenciais (password_hash fora).
type exportProfile struct {
	ID            int64   `json:"id"`
	WPUserID      *int64  `json:"wp_user_id"`
	Email         string  `json:"email"`
	Nome          string  `json:"nome"`
	Role          string  `json:"role"`
	Ativo         bool    `json:"ativo"`
	Status        string  `json:"status"`
	Plano         string  `json:"plano"`
	TwoFAEnabled  bool    `json:"twofa_enabled"`
	ShippingClass *int64  `json:"shipping_class_id"`
	CreatedAt     string  `json:"created_at"`
}

// exportOrder — resumo de um pedido do titular (não o pedido inteiro: só o que é
// PII/portabilidade útil ao titular). Recorte de propriedade idêntico a orders.go.
type exportOrder struct {
	ID          int64   `json:"id"`
	WCOrderID   *int64  `json:"wc_order_id"`
	Number      string  `json:"number"`
	Status      string  `json:"status"`
	Total       float64 `json:"total"`
	Shipping    float64 `json:"shipping"`
	PaymentStat string  `json:"payment_status"`
	CreatedAt   string  `json:"created_at"`
}

// ── GET /portal/account/export ─────────────────────────────────────────────────

// AccountExport monta o pacote "baixar meus dados" do titular autenticado:
// perfil (sem password_hash), settings, integrations (sem secrets), pedidos
// (resumo), afiliações (como afiliado) e sessões ativas (sem token raw).
//
// Registra a ação em senderzz_pii_access_log (action='export', subject_type='self')
// em best-effort: falha no log NÃO impede a entrega do dado ao titular (o direito
// de acesso prevalece; o log é accountability complementar).
func (h *SettingsHandler) AccountExport(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	ctx := r.Context()

	// ── Perfil + settings + integrations (sem password_hash) ──────────────────
	var (
		prof            exportProfile
		wpUserID        *int64
		shippingClass   *int64
		settingsRaw     []byte
		integrationsRaw []byte
	)
	err := h.Pool.QueryRow(ctx,
		`SELECT id, wp_user_id, email, nome, role, ativo, status, plano,
		        twofa_enabled, shipping_class_id, created_at::text,
		        settings, integrations
		   FROM senderzz_portal_users
		  WHERE id = $1`,
		u.ID,
	).Scan(
		&prof.ID, &wpUserID, &prof.Email, &prof.Nome, &prof.Role, &prof.Ativo,
		&prof.Status, &prof.Plano, &prof.TwoFAEnabled, &shippingClass, &prof.CreatedAt,
		&settingsRaw, &integrationsRaw,
	)
	if err != nil {
		slog.Error("[portal_lgpd] erro ao carregar perfil p/ exportação", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	prof.WPUserID = wpUserID
	prof.ShippingClass = shippingClass

	// integrations: remove defensivamente qualquer chave sensível antes de exportar.
	integrationsClean := redactSensitiveJSON(integrationsRaw)

	// ── Pedidos (resumo) — recorte canônico de orders.go ──────────────────────
	orders := h.exportOrders(ctx, u)

	// ── Afiliações (como afiliado) — afiliado_id = wp_user_id ─────────────────
	affiliations := h.exportAffiliations(ctx, u)

	// ── Sessões ativas (sem token raw) ────────────────────────────────────────
	sessions := h.exportSessions(ctx, u)

	// Registro de acesso a PII (best-effort — não bloqueia a entrega do dado).
	h.logPIIAccess(ctx, r, u, "export")

	httpx.WriteOK(w, map[string]any{
		"data": map[string]any{
			"profile":      prof,
			"settings":     rawOrEmpty(settingsRaw),
			"integrations": integrationsClean,
			"orders":       orders,
			"affiliations": affiliations,
			"sessions":     sessions,
		},
		"exported_at": time.Now().UTC().Format(time.RFC3339),
		"titular":     u.ID,
	})
}

// exportOrders lê os pedidos do titular com o MESMO recorte de propriedade de
// orders.go (produtor→produtor_id=ID, afiliado→affiliate_id=WPUserID). Resumo:
// id, número, status, totais, pagamento, data. Degradação graciosa (tabela
// ausente / role sem escopo → lista vazia).
func (h *SettingsHandler) exportOrders(ctx context.Context, u *auth.PortalUser) []exportOrder {
	out := []exportOrder{}
	if !tableExistsPool(ctx, h.Pool, "sz_orders") {
		return out
	}

	isAffiliate := u.Role == "afiliado" || u.Role == "affiliate" || u.Role == "afiliada" || u.Role == "cliente"
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
		// operator/demais: escopo por class_ids não migrado → fail-closed (vazio).
		return out
	}

	rows, err := h.Pool.Query(ctx,
		`SELECT o.id, o.wp_order_id,
		        COALESCE(NULLIF(o.order_number,''), o.wp_order_id::text) AS number,
		        COALESCE(o.status,'')          AS status,
		        COALESCE(o.total,0)::float8    AS total,
		        COALESCE(o.shipping,0)::float8 AS shipping,
		        COALESCE(o.payment_status,'')  AS payment_status,
		        o.created_at::text             AS created_at
		   FROM sz_orders o `+whereSQL+`
		  ORDER BY o.id DESC
		  LIMIT 1000`,
		scopeArg,
	)
	if err != nil {
		slog.Error("[portal_lgpd] erro ao exportar pedidos", "user_id", u.ID, "err", err)
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var eo exportOrder
		var wc *int64
		if err := rows.Scan(&eo.ID, &wc, &eo.Number, &eo.Status, &eo.Total,
			&eo.Shipping, &eo.PaymentStat, &eo.CreatedAt); err != nil {
			return out
		}
		eo.WCOrderID = wc
		eo.Status = strings.TrimPrefix(strings.ToLower(eo.Status), "wc-")
		out = append(out, eo)
	}
	return out
}

// exportAffiliations reusa a query canônica de "Minha afiliação"
// (afiliado_id = wp_user_id). Mantida local — não acopla AffiliatesHandler.
func (h *SettingsHandler) exportAffiliations(ctx context.Context, u *auth.PortalUser) []map[string]any {
	out := []map[string]any{}
	if u.WPUserID == 0 || !tableExistsPool(ctx, h.Pool, "senderzz_affiliates") {
		return out
	}
	rows, err := h.Pool.Query(ctx,
		`SELECT a.id,
		        COALESCE(NULLIF(p.nome,''), p.email, '—') AS producer_name,
		        a.status,
		        COALESCE(a.comissao_pct,0)::float8        AS commission_pct,
		        a.created_at::text                        AS created_at
		   FROM senderzz_affiliates a
		   LEFT JOIN senderzz_portal_users p
		          ON (p.id = a.produtor_id OR p.wp_user_id = a.produtor_id)
		         AND p.role = 'produtor'
		  WHERE a.afiliado_id = $1
		  ORDER BY a.created_at DESC
		  LIMIT 500`,
		u.WPUserID,
	)
	if err != nil {
		slog.Error("[portal_lgpd] erro ao exportar afiliações", "user_id", u.ID, "err", err)
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id            int64
			producerName  string
			status        string
			commissionPct float64
			createdAt     string
		)
		if err := rows.Scan(&id, &producerName, &status, &commissionPct, &createdAt); err != nil {
			return out
		}
		out = append(out, map[string]any{
			"id":             id,
			"producer_name":  producerName,
			"status":         status,
			"commission_pct": commissionPct,
			"created_at":     createdAt,
		})
	}
	return out
}

// exportSessions reusa o recorte de sessions_portal.go (sem token raw — só
// metadados: ip, user_agent, datas, is_current). NUNCA expõe token/token_hmac.
func (h *SettingsHandler) exportSessions(ctx context.Context, u *auth.PortalUser) []map[string]any {
	out := []map[string]any{}
	rows, err := h.Pool.Query(ctx,
		`SELECT id, ip, user_agent, created_at::text, expires_at::text,
		        (token = $2) AS is_current
		   FROM senderzz_portal_sessions
		  WHERE user_id = $1
		    AND expires_at > NOW()
		  ORDER BY is_current DESC, created_at DESC`,
		u.ID, u.SessionToken,
	)
	if err != nil {
		slog.Error("[portal_lgpd] erro ao exportar sessões", "user_id", u.ID, "err", err)
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id         int64
			ip         *string
			ua         *string
			createdAt  string
			expiresAt  string
			isCurrent  bool
		)
		if err := rows.Scan(&id, &ip, &ua, &createdAt, &expiresAt, &isCurrent); err != nil {
			return out
		}
		out = append(out, map[string]any{
			"id":         id,
			"ip":         ip,
			"user_agent": ua,
			"created_at": createdAt,
			"expires_at": expiresAt,
			"is_current": isCurrent,
		})
	}
	return out
}

// ── POST /portal/account/consent ───────────────────────────────────────────────

// consentRequest é o body de POST /portal/account/consent.
type consentRequest struct {
	DocType    string `json:"doc_type"`
	DocVersion string `json:"doc_version"`
}

// AccountConsent registra o aceite versionado de uma política/termo pelo titular.
//
// Idempotente: INSERT ... ON CONFLICT (user_id, doc_type, doc_version) DO NOTHING
// (casa o índice único uq_consents_user_doc_ver). Como DO NOTHING não retorna
// linha em conflito, fazemos SELECT do accepted_at depois — re-aceitar a MESMA
// versão devolve o accepted_at original (não cria duplicata, não erra).
func (h *SettingsHandler) AccountConsent(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	ctx := r.Context()

	var req consentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}
	docType := strings.TrimSpace(req.DocType)
	docVersion := strings.TrimSpace(req.DocVersion)
	if docType == "" || docVersion == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "Informe doc_type e doc_version.")
		return
	}

	ip := loginClientIP(r)
	ua := r.UserAgent()

	// INSERT idempotente — não falha em re-aceite da mesma versão.
	_, err := h.Pool.Exec(ctx,
		`INSERT INTO senderzz_consents (user_id, doc_type, doc_version, accepted_at, ip, user_agent)
		 VALUES ($1, $2, $3, NOW(), $4, $5)
		 ON CONFLICT (user_id, doc_type, doc_version) DO NOTHING`,
		u.ID, docType, docVersion, ip, ua,
	)
	if err != nil {
		slog.Error("[portal_lgpd] erro ao registrar consentimento",
			"user_id", u.ID, "doc_type", docType, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao registrar consentimento")
		return
	}

	// Lê o accepted_at efetivo (novo ou pré-existente — DO NOTHING não retorna linha).
	var acceptedAt string
	err = h.Pool.QueryRow(ctx,
		`SELECT accepted_at::text FROM senderzz_consents
		  WHERE user_id = $1 AND doc_type = $2 AND doc_version = $3
		  LIMIT 1`,
		u.ID, docType, docVersion,
	).Scan(&acceptedAt)
	if err != nil {
		// Linha deveria existir após o INSERT/conflict — se não, é erro interno real.
		slog.Error("[portal_lgpd] erro ao ler accepted_at do consentimento",
			"user_id", u.ID, "doc_type", docType, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao registrar consentimento")
		return
	}

	slog.Info("[portal_lgpd] consentimento registrado",
		"user_id", u.ID, "doc_type", docType, "doc_version", docVersion)
	httpx.WriteOK(w, map[string]any{
		"ok":          true,
		"accepted_at": acceptedAt,
		"doc_type":    docType,
		"doc_version": docVersion,
	})
}

// ── POST /portal/account/consent/revoke ─────────────────────────────────────────

// AccountConsentRevoke registra a REVOGAÇÃO de um consentimento do titular
// (LGPD Art. 8º §5º — "é assegurado ao titular o direito de revogar o
// consentimento a qualquer momento").
//
// MODELO (porte fiel ao schema 380-lgpd-completo.sql):
//
//	revogar = setar revoked_at = NOW() na linha existente — NUNCA deletar.
//	A linha permanece para auditabilidade/accountability (Art. 37) e prova
//	QUANDO o consentimento deixou de valer (revoked_at IS NULL = ativo).
//
// Escopo estrito (fail-closed): só revoga consentimento do PRÓPRIO titular
// (user_id = id da sessão; nunca arbitrário) e da versão exata informada
// (doc_type + doc_version). Idempotente: revogar de novo a mesma versão já
// revogada NÃO re-escreve revoked_at (WHERE revoked_at IS NULL) e devolve o
// timestamp original — não erra nem move a data da revogação.
//
// 404 PT-BR quando não existe consentimento ATIVO daquele (doc_type, doc_version)
// para o titular (nunca aceito, ou já revogado).
func (h *SettingsHandler) AccountConsentRevoke(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	ctx := r.Context()

	var req consentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}
	docType := strings.TrimSpace(req.DocType)
	docVersion := strings.TrimSpace(req.DocVersion)
	if docType == "" || docVersion == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "Informe doc_type e doc_version.")
		return
	}

	// UPDATE de revogação — só linha ATIVA (revoked_at IS NULL) do PRÓPRIO titular.
	// RETURNING revoked_at devolve o timestamp gravado. Idempotência: se já estava
	// revogada, 0 linhas afetadas → caímos no SELECT do revoked_at pré-existente.
	var revokedAt string
	err := h.Pool.QueryRow(ctx,
		`UPDATE senderzz_consents
		    SET revoked_at = NOW()
		  WHERE user_id = $1 AND doc_type = $2 AND doc_version = $3
		    AND revoked_at IS NULL
		RETURNING revoked_at::text`,
		u.ID, docType, docVersion,
	).Scan(&revokedAt)

	if err != nil {
		// Sem linha atingida pelo UPDATE: ou não existe o consentimento, ou já
		// estava revogado. Distinguimos os dois com um SELECT do estado atual.
		var existingRevoked *string
		selErr := h.Pool.QueryRow(ctx,
			`SELECT revoked_at::text FROM senderzz_consents
			  WHERE user_id = $1 AND doc_type = $2 AND doc_version = $3
			  LIMIT 1`,
			u.ID, docType, docVersion,
		).Scan(&existingRevoked)
		if selErr == nil && existingRevoked != nil {
			// Já estava revogado — idempotente: devolve o timestamp original (200).
			slog.Info("[portal_lgpd] consentimento já estava revogado (idempotente)",
				"user_id", u.ID, "doc_type", docType, "doc_version", docVersion)
			httpx.WriteOK(w, map[string]any{
				"ok":          true,
				"revoked_at":  *existingRevoked,
				"doc_type":    docType,
				"doc_version": docVersion,
				"message":     "Consentimento já estava revogado.",
			})
			return
		}
		if selErr != nil && !errors.Is(selErr, pgx.ErrNoRows) {
			// Erro real de banco (não é "linha ausente").
			slog.Error("[portal_lgpd] erro ao verificar consentimento p/ revogação",
				"user_id", u.ID, "doc_type", docType, "err", selErr)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao revogar consentimento")
			return
		}
		// Não existe consentimento daquele doc/versão para o titular.
		httpx.WriteErr(w, http.StatusNotFound, "Consentimento não encontrado para revogar.")
		return
	}

	slog.Info("[portal_lgpd] consentimento revogado",
		"user_id", u.ID, "doc_type", docType, "doc_version", docVersion)
	httpx.WriteOK(w, map[string]any{
		"ok":          true,
		"revoked_at":  revokedAt,
		"doc_type":    docType,
		"doc_version": docVersion,
		"message":     "Consentimento revogado.",
	})
}

// ── Helpers LGPD ────────────────────────────────────────────────────────────────

// logPIIAccess grava a trilha de acesso a PII (accountability Art. 37).
// Best-effort: erro só é logado — nunca quebra o fluxo do titular.
func (h *SettingsHandler) logPIIAccess(ctx context.Context, r *http.Request, u *auth.PortalUser, action string) {
	_, err := h.Pool.Exec(ctx,
		`INSERT INTO senderzz_pii_access_log
		    (actor_user_id, actor_email, subject_type, subject_id, fields, action, ip)
		 VALUES ($1, $2, 'self', $3, $4, $5, $6)`,
		u.ID, u.Email, u.ID,
		"profile,settings,integrations,orders,affiliations,sessions",
		action, loginClientIP(r),
	)
	if err != nil {
		slog.Warn("[portal_lgpd] falha ao registrar pii_access_log (best-effort)",
			"user_id", u.ID, "action", action, "err", err)
	}
}

// redactSensitiveJSON devolve o blob JSON com chaves sensíveis removidas no nível
// raiz (secret/token/password). O espelho PG de integrations não guarda secret
// hoje, mas isto é defesa em profundidade (future-proof). Falha de parse → '{}'.
func redactSensitiveJSON(raw []byte) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		// Não é objeto JSON (ou inválido) → não expõe nada inesperado.
		return json.RawMessage(`{}`)
	}
	for k := range m {
		lk := strings.ToLower(k)
		if strings.Contains(lk, "secret") || strings.Contains(lk, "token") || strings.Contains(lk, "password") {
			delete(m, k)
		}
	}
	clean, err := json.Marshal(m)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(clean)
}

// rawOrEmpty devolve o JSON bruto ou '{}' quando vazio (settings nunca é NULL no
// schema, mas defensivo p/ contrato estável de JSON).
func rawOrEmpty(raw []byte) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(raw)
}

// tableExistsPool — guarda de migração graceful sem handler-específico (reusa o
// mesmo SELECT EXISTS de orders.go/expedicao.go a partir do Pool do SettingsHandler).
func tableExistsPool(ctx context.Context, pool *pgxpool.Pool, name string) bool {
	var ok bool
	_ = pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}
