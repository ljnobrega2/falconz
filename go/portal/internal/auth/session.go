// Package auth — helpers de sessão e middlewares de autenticação do Portal V2.
//
// Middlewares disponíveis:
//   - AuthPortalJWT  → valida Bearer JWT (HS256) + lookup de sessão no banco
//   - AuthRole(...)  → verifica role após autenticação
//
// Estratégia de lookup de sessão (V-SEC-01/02):
//   O JWT carrega o token raw da sessão em claims["sid"].
//   O banco armazena o HMAC-SHA256(token_raw, WP_SALT_AUTH) em token_hmac.
//   Por retrocompatibilidade com sessões PHP ainda ativas, a query aceita
//   WHERE token IN (token_raw, hmac) durante o período de migração.
//
// Fail-closed:
//   JWT_SECRET vazio   → 503 (não 401) — configuração ausente é erro de infra.
//   WP_SALT_AUTH vazio → 503 — sem salt não há como validar sessões PHP legadas.
//   Sessão expirada    → 401.
//   Role incorreto     → 403.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// hashToken reproduz o HMAC do PHP:
//
//	hash_hmac('sha256', $token, AUTH_SALT)
//
// Equivalente ao hmacSHA256Hex de motoboy/internal/auth/auth.go.
func hashToken(token string) string {
	salt := os.Getenv("WP_SALT_AUTH")
	mac := hmac.New(sha256.New, []byte(salt))
	mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
}

// writeAuthErr escreve uma resposta JSON de erro de autenticação.
func writeAuthErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"ok":false,"erro":"` + msg + `"}`))
}

// AuthPortalJWT é o middleware principal de autenticação do Portal V2.
//
// Fluxo:
//  1. Lê JWT_SECRET — 503 se vazio (fail-closed).
//  2. Lê WP_SALT_AUTH — 503 se vazio (necessário para validar sessões PHP legadas).
//  3. Extrai token de Authorization: Bearer <jwt> OU Cookie sz_portal_token.
//  4. Valida assinatura HS256 e expiração via ParseJWT (rejeita partial_token).
//  5. Extrai claims.SessionToken (claim "sid").
//  6. Busca sessão no banco: WHERE token IN (raw, hmac) AND expires_at > NOW().
//  7. Injeta PortalUser no contexto.
func AuthPortalJWT(pool *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Fail-closed: configuração ausente → 503.
			if os.Getenv("JWT_SECRET") == "" {
				slog.Error("[auth] JWT_SECRET não configurado — rejeitando requisição portal",
					"path", r.URL.Path, "ip", r.RemoteAddr)
				writeAuthErr(w, http.StatusServiceUnavailable, "serviço temporariamente indisponível")
				return
			}
			if os.Getenv("WP_SALT_AUTH") == "" {
				slog.Error("[auth] WP_SALT_AUTH não configurado — sessões PHP legadas não podem ser validadas",
					"path", r.URL.Path, "ip", r.RemoteAddr)
				writeAuthErr(w, http.StatusServiceUnavailable, "serviço temporariamente indisponível")
				return
			}

			// Extrai token JWT: cookie tem prioridade, header Authorization é fallback.
			rawJWT := ""
			if c, err := r.Cookie("sz_portal_token"); err == nil && c.Value != "" {
				rawJWT = c.Value
			}
			if rawJWT == "" {
				if ah := r.Header.Get("Authorization"); strings.HasPrefix(ah, "Bearer ") {
					rawJWT = strings.TrimPrefix(ah, "Bearer ")
				}
			}
			if rawJWT == "" {
				writeAuthErr(w, http.StatusUnauthorized, "token ausente")
				return
			}

			// Valida JWT — ParseJWT já rejeita tokens parciais (pending_2fa=true).
			claims, err := ParseJWT(rawJWT)
			if err != nil {
				slog.Warn("[auth] JWT portal inválido", "err", err, "path", r.URL.Path, "ip", r.RemoteAddr)
				writeAuthErr(w, http.StatusUnauthorized, "token inválido ou expirado")
				return
			}

			// Valida sessão no banco usando o token raw armazenado em claims["sid"].
			// Aceita tanto o token raw quanto o HMAC para compatibilidade durante migração.
			sessionRaw := claims.SessionToken
			sessionHMAC := hashToken(sessionRaw)

			var u PortalUser
			err = pool.QueryRow(r.Context(), `
				SELECT u.id, COALESCE(u.wp_user_id, 0), u.email, u.role,
				       u.name, COALESCE(u.shipping_class_id, 0), u.status
				  FROM senderzz_portal_sessions s
				  JOIN senderzz_portal_users u ON u.id = s.user_id
				 WHERE s.token IN ($1, $2)
				   AND s.expires_at > NOW()
				   AND u.status = 'active'
				 LIMIT 1`,
				sessionRaw, sessionHMAC,
			).Scan(&u.ID, &u.WPUserID, &u.Email, &u.Role, &u.Name, &u.ClassID, &u.Status)

			if err != nil {
				slog.Warn("[auth] sessão portal inválida ou expirada",
					"err", err, "user_id", claims.UserID, "ip", r.RemoteAddr)
				writeAuthErr(w, http.StatusUnauthorized, "sessão inválida ou expirada")
				return
			}

			u.SessionToken = sessionRaw
			ctx := contextWithUser(r.Context(), &u)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// AuthRole é um middleware que envolve AuthPortalJWT verificando o role do usuário.
// Retorna 403 se o role não estiver na lista permitida.
// Requer que AuthPortalJWT já tenha sido aplicado (PortalUser no contexto).
func AuthRole(roles ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]bool, len(roles))
	for _, r := range roles {
		allowed[r] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := FromContext(r.Context())
			if u == nil || !allowed[u.Role] {
				writeAuthErr(w, http.StatusForbidden, "permissão negada")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Session representa uma sessão no DB portal — usada pelos handlers de auth.
type Session struct {
	Token     string
	TokenHMAC string
	UserID    int64
}
