// Package handlers — handler de Sessões do Portal V2 (user-scoped). // FEAT-PORTAL
//
// Rotas (namespace /wp-json/senderzz/v1):
//
//	GET  /portal/sessions          — lista as sessões ativas do usuário autenticado
//	POST /portal/sessions/revoke-all — revoga TODAS as sessões do usuário EXCETO a atual
//
// Espelha o conceito de "Sessões ativas" do settings.php (szaction=list_sessions /
// revoke_all_sessions). A fonte é a tabela compartilhada senderzz_portal_sessions
// (lida via view wp_senderzz_portal_sessions pelos serviços motoboy/wallet) — por
// isso NÃO alteramos o schema dela (não há coluna last_seen): expomos created_at
// como melhor aproximação de "último acesso" disponível no espelho.
//
// ── is_current ────────────────────────────────────────────────────────────────
// O middleware AuthPortalJWT injeta o token RAW da sessão em u.SessionToken
// (session.go:126) e a coluna `token` guarda exatamente esse raw → comparação
// direta (row.token == u.SessionToken) identifica a sessão corrente sem precisar
// recalcular o HMAC. revoke-all deleta WHERE user_id=$1 AND token <> $rawAtual.
//
// Escopo: SEMPRE filtrado por s.user_id = u.ID e expires_at > NOW() (não vaza
// sessões de outro usuário nem mostra sessões expiradas).
package handlers

import (
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/portal-service/internal/auth"
	"github.com/senderzz/portal-service/internal/httpx"
)

// SessionsHandler agrupa as dependências do handler de sessões.
// Construção idêntica a WebhookHandler — o integrador monta com &handlers.SessionsHandler{Pool: pool}.
type SessionsHandler struct {
	Pool *pgxpool.Pool
}

// sessionRow — uma sessão ativa do usuário, espelhando os campos da tela.
// LastSeen reusa created_at (não há coluna last_seen na tabela compartilhada — ver header).
type sessionRow struct {
	ID        int64   `json:"id"`
	IP        *string `json:"ip"`
	UserAgent *string `json:"user_agent"`
	CreatedAt string  `json:"created_at"`
	LastSeen  string  `json:"last_seen"` // = created_at (proxy — sem coluna last_seen no espelho)
	ExpiresAt string  `json:"expires_at"`
	IsCurrent bool    `json:"is_current"`
}

// ── GET /portal/sessions ────────────────────────────────────────────────────── // FEAT-PORTAL

// List retorna as sessões ATIVAS (expires_at > NOW()) do usuário autenticado.
// is_current marca a sessão da requisição atual (row.token == u.SessionToken).
func (h *SessionsHandler) List(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	rows, err := h.Pool.Query(r.Context(),
		`SELECT id, ip, user_agent,
		        created_at::text, expires_at::text,
		        (token = $2) AS is_current
		   FROM senderzz_portal_sessions
		  WHERE user_id = $1
		    AND expires_at > NOW()
		  ORDER BY is_current DESC, created_at DESC`,
		u.ID, u.SessionToken,
	)
	if err != nil {
		slog.Error("[portal_sessions] erro ao listar sessões", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer rows.Close()

	out := []sessionRow{}
	for rows.Next() {
		var s sessionRow
		if err := rows.Scan(&s.ID, &s.IP, &s.UserAgent, &s.CreatedAt, &s.ExpiresAt, &s.IsCurrent); err != nil {
			slog.Error("[portal_sessions] erro ao ler linha", "user_id", u.ID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao ler sessões")
			return
		}
		s.LastSeen = s.CreatedAt // sem coluna last_seen no espelho — proxy de created_at.
		out = append(out, s)
	}
	if rows.Err() != nil {
		slog.Error("[portal_sessions] erro após iteração", "user_id", u.ID, "err", rows.Err())
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao processar sessões")
		return
	}

	httpx.WriteOK(w, map[string]any{"data": out, "total": len(out)})
}

// ── POST /portal/sessions/revoke-all ─────────────────────────────────────────── // FEAT-PORTAL

// RevokeAll deleta TODAS as sessões do usuário EXCETO a atual.
// A sessão corrente é preservada por token <> u.SessionToken (token raw).
func (h *SessionsHandler) RevokeAll(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	tag, err := h.Pool.Exec(r.Context(),
		`DELETE FROM senderzz_portal_sessions
		  WHERE user_id = $1 AND token <> $2`,
		u.ID, u.SessionToken,
	)
	if err != nil {
		slog.Error("[portal_sessions] erro ao revogar sessões", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	revoked := tag.RowsAffected()
	slog.Info("[portal_sessions] sessões revogadas (exceto a atual)", "user_id", u.ID, "revogadas", revoked)
	httpx.WriteOK(w, map[string]any{
		"mensagem":  "demais sessões encerradas com sucesso",
		"revogadas": revoked,
	})
}
