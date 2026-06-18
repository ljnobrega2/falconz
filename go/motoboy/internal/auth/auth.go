// Package auth implementa os middlewares de autenticação dos 4 grupos do OpenAPI:
//
//   - motoboy_token  → Header X-MB-Token (sz_motoboys.token_app)
//   - alan_token     → Header X-Alan-Token (token estático em env ALAN_TOKEN)
//   - portal_session → Cookie senderzz_portal_session ou Header X-Senderzz-Token
//                      (token HMAC-SHA256 com WP_SALT_AUTH, lookup em senderzz_portal_sessions)
//   - público        → sem auth
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"log/slog"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/motoboy-service/internal/httpx"
)

// ── Context keys ─────────────────────────────────────────────────────────────

// Tipos opacos para context keys — evita colisões com pacotes externos.
type ctxKeyMotoboy struct{}
type ctxKeyPortalUser struct{}

// Motoboy representa o motoboy autenticado extraído do banco.
type Motoboy struct {
	ID     int64
	CdID   int64
	ZonaID *int64
	Nome   string
}

// PortalUser representa o usuário de portal autenticado via sessão.
type PortalUser struct {
	ID   int64
	Role string
}

// ── Helpers de extração de contexto ─────────────────────────────────────────

// MotoboyfromCtx retorna o Motoboy autenticado do contexto, ou nil.
func MotoboyfromCtx(ctx context.Context) *Motoboy {
	v, _ := ctx.Value(ctxKeyMotoboy{}).(*Motoboy)
	return v
}

// PortalUserFromCtx retorna o PortalUser autenticado do contexto, ou nil.
func PortalUserFromCtx(ctx context.Context) *PortalUser {
	v, _ := ctx.Value(ctxKeyPortalUser{}).(*PortalUser)
	return v
}

// ── Middlewares ───────────────────────────────────────────────────────────────

// AuthMotoboy valida o header X-MB-Token contra sz_motoboys.token_app no banco.
// Falha com 401 se token ausente, inválido ou motoboy inativo.
// O Motoboy autenticado é armazenado no contexto via ctxKeyMotoboy.
func AuthMotoboy(pool *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := r.Header.Get("X-MB-Token")
			if token == "" {
				httpx.WriteErr(w, http.StatusUnauthorized, "token ausente")
				return
			}

			var mb Motoboy
			// ativo é TINYINT(1) no MySQL → smallint no Postgres após pgloader.
			// Comparar contra 1 (não true) até o schema ser migrado para boolean.
			err := pool.QueryRow(r.Context(),
				`SELECT id, cd_id, zona_id, nome
				   FROM sz_motoboys
				  WHERE token_app = $1 AND ativo = 1
				  LIMIT 1`,
				token,
			).Scan(&mb.ID, &mb.CdID, &mb.ZonaID, &mb.Nome)
			if err != nil {
				slog.Warn("[auth] token_app inválido ou motoboy inativo", "err", err)
				httpx.WriteErr(w, http.StatusUnauthorized, "não autorizado")
				return
			}

			ctx := context.WithValue(r.Context(), ctxKeyMotoboy{}, &mb)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// AuthAlan valida o header X-Alan-Token contra a variável de ambiente ALAN_TOKEN.
// Responde 401 se ausente ou não corresponder.
func AuthAlan() func(http.Handler) http.Handler {
	expectedToken := os.Getenv("ALAN_TOKEN")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if expectedToken == "" {
				slog.Error("[auth] ALAN_TOKEN não configurado — todas as requisições Alan serão rejeitadas")
				httpx.WriteErr(w, http.StatusServiceUnavailable, "serviço não configurado")
				return
			}
			token := r.Header.Get("X-Alan-Token")
			if subtle.ConstantTimeCompare([]byte(token), []byte(expectedToken)) != 1 {
				httpx.WriteErr(w, http.StatusUnauthorized, "não autorizado")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// AuthPortal valida a sessão do portal via cookie senderzz_portal_session
// ou header X-Senderzz-Token.
//
// Esquema (espelhado de src/Portal/Portal_Auth.php):
//   - O cookie contém o token_raw (hex aleatório).
//   - O banco armazena token_hash = HMAC-SHA256(token_raw, WP_SALT_AUTH).
//   - Por compatibilidade de migração o banco pode conter o token_raw também.
//   - A query aceita ambos: WHERE token IN (token_raw, token_hash).
//
// Requer env var WP_SALT_AUTH (equivalente a AUTH_SALT do WordPress).
func AuthPortal(pool *pgxpool.Pool) func(http.Handler) http.Handler {
	wpSalt := os.Getenv("WP_SALT_AUTH")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if wpSalt == "" {
				slog.Error("[auth] WP_SALT_AUTH não configurado — todas as sessões portal serão rejeitadas")
				httpx.WriteErr(w, http.StatusServiceUnavailable, "serviço não configurado")
				return
			}

			// Lê token: cookie tem prioridade, header é fallback para clientes SPA.
			tokenRaw := ""
			if c, err := r.Cookie("senderzz_portal_session"); err == nil {
				tokenRaw = c.Value
			}
			if tokenRaw == "" {
				tokenRaw = r.Header.Get("X-Senderzz-Token")
			}
			if tokenRaw == "" {
				httpx.WriteErr(w, http.StatusUnauthorized, "sessão ausente")
				return
			}

			tokenHash := hmacSHA256Hex(tokenRaw, wpSalt)

			// NOTA: nomes de tabela incluem o prefixo WP padrão (wp_).
			// pgloader preserva os nomes originais do MySQL. Se o prefixo WP for diferente
			// de "wp_", ajustar as constantes abaixo.
			// Tabelas: wp_senderzz_portal_sessions, wp_senderzz_portal_users.
			var user PortalUser
			err := pool.QueryRow(r.Context(),
				`SELECT u.id, COALESCE(u.role, '') AS role
				   FROM wp_senderzz_portal_sessions s
				   JOIN wp_senderzz_portal_users u ON u.id = s.user_id
				  WHERE s.token IN ($1, $2)
				    AND s.expires_at > NOW()
				    AND u.status = 'active'
				  LIMIT 1`,
				tokenRaw, tokenHash,
			).Scan(&user.ID, &user.Role)
			if err != nil {
				slog.Warn("[auth] sessão portal inválida ou expirada", "err", err)
				httpx.WriteErr(w, http.StatusUnauthorized, "sessão inválida ou expirada")
				return
			}

			ctx := context.WithValue(r.Context(), ctxKeyPortalUser{}, &user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAuth é um middleware genérico que retorna 401 se nenhum principal
// autenticado (Motoboy OU PortalUser) estiver no contexto.
// Usar apenas quando uma rota aceita múltiplos esquemas de auth.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if MotoboyfromCtx(r.Context()) == nil && PortalUserFromCtx(r.Context()) == nil {
			httpx.WriteErr(w, http.StatusUnauthorized, "não autorizado")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ── Helpers internos ──────────────────────────────────────────────────────────

func hmacSHA256Hex(message, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}
