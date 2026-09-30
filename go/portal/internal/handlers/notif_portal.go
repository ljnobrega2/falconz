// Package handlers — handler de Push Notifications do Portal. // FEAT-NOTIF
//
// Namespace PRÓPRIO (sz-notif), MAS AUTENTICADO (sob requireAuth):
//
//	POST /wp-json/sz-notif/v1/subscribe    {endpoint, keys:{p256dh,auth}} → upsert
//	POST /wp-json/sz-notif/v1/unsubscribe  {endpoint}                     → delete
//	GET  /wp-json/sz-notif/v1/prefs                                       → ler prefs
//	POST /wp-json/sz-notif/v1/prefs        {prefs:{...}}                  → salvar prefs
//
// ── SEC-RBAC (auditoria 2026-06-18): IDOR fechado ──────────────────────────────
// ANTES estas rotas eram PÚBLICAS (fora de requireAuth) e liam o user_id do BODY/
// QUERY. Isso era um IDOR: qualquer chamador podia (a) assinar o push de OUTRO
// usuário (user_id=vítima + endpoint próprio → recebia os pushes da vítima) e
// (b) ler/SOBRESCREVER as prefs de qualquer user_id arbitrário. O PHP autoritativo
// (includes/senderzz-notifications.php) NUNCA confia no user_id do cliente: deriva
// o dono da SESSÃO (sz_notif_rest_get_user → wp_user_id) e devolve 401 sem sessão.
// Agora portamos isso fielmente: o dono é SEMPRE u.WPUserID do contexto autenticado
// (auth.FromContext) — o user_id do cliente é IGNORADO. As rotas vivem sob
// requireAuth (main.go), igual ao restante do portal.
//
// id-space: sz_push_subscriptions.user_id / sz_notif_prefs.user_id guardam o
// wp_user_id (o SENDER em senderzz-notifications.php lê WHERE user_id=wp_user_id).
// Por isso escopamos por u.WPUserID, NUNCA u.ID — escrever no id-space errado
// gravaria numa keyspace que o disparo de push nunca lê.
//
// FAIL-CLOSED: endpoint vazio em subscribe/unsubscribe → 400 (não toca o banco).
// Tabela ausente (42P01) numa escrita → 503 "não migrado" (NÃO finge sucesso).
//
// Tabelas: sz_push_subscriptions / sz_notif_prefs
// (infra/postgres/schema-fixes-v471-notif.sql).
package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/portal-service/internal/auth"
	"github.com/senderzz/portal-service/internal/httpx"
)

// NotifHandler agrupa as dependências do handler de push notifications.
// Construção idêntica aos demais — &handlers.NotifHandler{Pool: pool}.
type NotifHandler struct {
	Pool *pgxpool.Pool
}

// subscribeRequest é o body de POST /subscribe.
// keys espelha o objeto PushSubscription.toJSON().keys do navegador.
//
// SEC-RBAC: NÃO há campo user_id — o dono vem SEMPRE da sessão (u.WPUserID).
// O PWA (app-pwa.php) já envia só {endpoint, keys}; o React anexa Bearer e o
// user_id que enviava no body é ignorado (a sessão é a autoridade).
type subscribeRequest struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

// unsubscribeRequest é o body de POST /unsubscribe.
type unsubscribeRequest struct {
	Endpoint string `json:"endpoint"`
}

// prefsRequest é o body de POST /prefs.
// SEC-RBAC: NÃO há campo user_id — o dono vem da sessão (u.WPUserID).
type prefsRequest struct {
	Prefs json.RawMessage `json:"prefs"`
}

// ── POST /wp-json/sz-notif/v1/subscribe ──────────────────────────────────────── // FEAT-NOTIF

// Subscribe faz upsert da inscrição de push (chave natural = endpoint).
// SEC-RBAC: o dono é SEMPRE o usuário autenticado (u.WPUserID) — nunca o body
// (senão um atacante assinaria com user_id=vítima + endpoint próprio e receberia
// os pushes da vítima). Re-inscrição com o mesmo endpoint reescreve o dono para
// o autenticado (ON CONFLICT) — coerente com "endpoint é deste navegador".
func (h *NotifHandler) Subscribe(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	var req subscribeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}
	// Fail-closed: endpoint vazio → 400 (não toca o banco).
	if req.Endpoint == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "endpoint é obrigatório")
		return
	}

	var id int64
	err := h.Pool.QueryRow(r.Context(),
		`INSERT INTO sz_push_subscriptions (user_id, endpoint, p256dh, auth)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (endpoint) DO UPDATE
		    SET user_id = EXCLUDED.user_id,
		        p256dh  = EXCLUDED.p256dh,
		        auth    = EXCLUDED.auth
		 RETURNING id`,
		u.WPUserID, req.Endpoint, req.Keys.P256dh, req.Keys.Auth,
	).Scan(&id)
	if err != nil {
		if isUndefinedTable(err) {
			httpx.WriteErr(w, http.StatusServiceUnavailable, "push de notificações ainda não migrado neste ambiente")
			return
		}
		slog.Error("[notif] erro ao inscrever push", "endpoint", req.Endpoint, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	slog.Info("[notif] inscrição de push registrada", "id", id, "user_id", u.WPUserID)
	httpx.WriteOK(w, map[string]any{"id": id, "mensagem": "inscrição registrada"})
}

// ── POST /wp-json/sz-notif/v1/unsubscribe ────────────────────────────────────── // FEAT-NOTIF

// Unsubscribe remove a inscrição pelo endpoint. Endpoint inexistente → ok (idempotente).
// SEC-RBAC: escopa por user_id (sessão) — um usuário só remove a PRÓPRIA inscrição;
// sem o filtro, conhecer o endpoint de outro permitiria desativar os pushes dele.
func (h *NotifHandler) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	var req unsubscribeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}
	if req.Endpoint == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "endpoint é obrigatório")
		return
	}

	tag, err := h.Pool.Exec(r.Context(),
		`DELETE FROM sz_push_subscriptions WHERE endpoint = $1 AND user_id = $2`,
		req.Endpoint, u.WPUserID,
	)
	if err != nil {
		if isUndefinedTable(err) {
			httpx.WriteErr(w, http.StatusServiceUnavailable, "push de notificações ainda não migrado neste ambiente")
			return
		}
		slog.Error("[notif] erro ao desinscrever push", "endpoint", req.Endpoint, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	httpx.WriteOK(w, map[string]any{
		"mensagem":  "inscrição removida",
		"removidas": tag.RowsAffected(),
	})
}

// ── GET /wp-json/sz-notif/v1/prefs?user_id= ──────────────────────────────────── // FEAT-NOTIF

// Prefs (GET) lê as preferências do PRÓPRIO usuário autenticado.
// SEC-RBAC: o dono vem da sessão (u.WPUserID) — o ?user_id= do cliente é IGNORADO
// (antes lia as prefs de qualquer user_id arbitrário). Sem linha → prefs:{} (padrão).
func (h *NotifHandler) GetPrefs(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	userID := u.WPUserID

	var raw []byte
	err := h.Pool.QueryRow(r.Context(),
		`SELECT prefs FROM sz_notif_prefs WHERE user_id = $1`,
		userID,
	).Scan(&raw)
	if err != nil {
		// pgx.ErrNoRows ou tabela ausente → padrão {} (degradação graciosa na leitura).
		httpx.WriteOK(w, map[string]any{"user_id": userID, "prefs": json.RawMessage("{}")})
		return
	}

	httpx.WriteOK(w, map[string]any{"user_id": userID, "prefs": json.RawMessage(raw)})
}

// ── POST /wp-json/sz-notif/v1/prefs ──────────────────────────────────────────── // FEAT-NOTIF

// SavePrefs faz upsert das preferências do PRÓPRIO usuário autenticado (PK = user_id).
// SEC-RBAC: o dono vem da sessão (u.WPUserID) — antes o user_id do body permitia
// SOBRESCREVER as prefs de qualquer usuário (IDOR de escrita). O body só carrega prefs.
func (h *NotifHandler) SavePrefs(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	userID := u.WPUserID

	var req prefsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}
	// prefs ausente → {} (não nil — coluna é NOT NULL).
	if len(req.Prefs) == 0 {
		req.Prefs = json.RawMessage("{}")
	}
	// Valida que prefs é JSON válido antes de gravar (evita corromper a coluna jsonb).
	if !json.Valid(req.Prefs) {
		httpx.WriteErr(w, http.StatusBadRequest, "prefs deve ser um JSON válido")
		return
	}

	_, err := h.Pool.Exec(r.Context(),
		`INSERT INTO sz_notif_prefs (user_id, prefs, updated_at)
		 VALUES ($1, $2, NOW())
		 ON CONFLICT (user_id) DO UPDATE
		    SET prefs = EXCLUDED.prefs, updated_at = NOW()`,
		userID, req.Prefs,
	)
	if err != nil {
		if isUndefinedTable(err) {
			httpx.WriteErr(w, http.StatusServiceUnavailable, "push de notificações ainda não migrado neste ambiente")
			return
		}
		slog.Error("[notif] erro ao salvar prefs", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	slog.Info("[notif] prefs salvas", "user_id", userID)
	httpx.WriteOK(w, map[string]any{"user_id": userID, "prefs": req.Prefs})
}
