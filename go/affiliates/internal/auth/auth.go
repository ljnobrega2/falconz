// Package auth implementa os middlewares de autenticação do serviço Afiliados.
//
// Dois esquemas de auth:
//
//   - AuthPortalJWT — valida JWT Bearer HS256 emitido pelo PHP (tpc_jwt_encode()).
//     Injeta user_id (int64) e role (string) no contexto via ctxKeyPortalUser.
//     Claim "sub" = user_id, claim "role" = papel do usuário no portal.
//     Fail-closed: JWT_SECRET vazio retorna 503 (não 401).
//
//   - AuthPortalSession — valida sessão de portal via cookie senderzz_portal_session
//     ou header X-Senderzz-Token (mesmo esquema de auth.go do motoboy-service).
//     Alternativa ao JWT para clientes SPA que usam cookies de sessão.
//
// Nota de portabilidade: PortalUser.Role vem do JWT claim "role" (string) ou
// da coluna wp_senderzz_portal_users.role. Valores conhecidos: "produtor",
// "operator", "affiliate".
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/affiliates-service/internal/httpx"
)

// ── Context keys ──────────────────────────────────────────────────────────────

// Tipos opacos para context keys — evita colisões com pacotes externos.
type ctxKeyPortalUser struct{}

// PortalUser representa o usuário autenticado injetado no contexto.
type PortalUser struct {
	ID   int64
	Role string
}

// ── Helpers de extração de contexto ─────────────────────────────────────────

// PortalUserFromCtx retorna o PortalUser autenticado do contexto, ou nil.
func PortalUserFromCtx(ctx context.Context) *PortalUser {
	v, _ := ctx.Value(ctxKeyPortalUser{}).(*PortalUser)
	return v
}

// ── Middlewares ───────────────────────────────────────────────────────────────

// AuthPortalJWT é um middleware chi que valida o token Bearer HS256 emitido pelo WP.
//
// Fluxo:
//  1. Lê JWT_SECRET da env — se vazio retorna 503 (fail-closed).
//  2. Extrai o token do header "Authorization: Bearer <token>".
//  3. Valida assinatura e exp com golang-jwt.
//  4. Extrai claim "sub" (int64) e "role" (string), injeta PortalUser no contexto.
//
// O PHP emite tokens com payload: {sub: user_id, role: "produtor"|"operator"|"affiliate", iat, exp}.
func AuthPortalJWT(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secret := os.Getenv("JWT_SECRET")
		if secret == "" {
			// Configuração ausente — fail-closed: não processar sem secret.
			slog.Error("[auth] JWT_SECRET não configurado — rejeitando requisição",
				"path", r.URL.Path,
				"ip", r.RemoteAddr,
			)
			httpx.WriteErr(w, http.StatusServiceUnavailable, "serviço temporariamente indisponível")
			return
		}

		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			httpx.WriteErr(w, http.StatusUnauthorized, "token ausente ou formato inválido")
			return
		}
		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")

		// Valida e parseia o token HS256.
		token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
			// Garante que o algoritmo seja exatamente HS256 — rejeita RS256 etc.
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return []byte(secret), nil
		}, jwt.WithValidMethods([]string{"HS256"}))

		if err != nil || !token.Valid {
			slog.Warn("[auth] token JWT inválido",
				"err", err,
				"path", r.URL.Path,
				"ip", r.RemoteAddr,
			)
			httpx.WriteErr(w, http.StatusUnauthorized, "token inválido ou expirado")
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			httpx.WriteErr(w, http.StatusUnauthorized, "claims inválidas")
			return
		}

		// "sub" no PHP é o user_id int, serializado como número JSON.
		subRaw, exists := claims["sub"]
		if !exists {
			httpx.WriteErr(w, http.StatusUnauthorized, "claim sub ausente")
			return
		}

		// jwt.MapClaims decodifica números JSON como float64.
		subFloat, ok := subRaw.(float64)
		if !ok || subFloat <= 0 {
			httpx.WriteErr(w, http.StatusUnauthorized, "claim sub inválida")
			return
		}

		// Extrai role do token (opcional — padrão vazio se ausente).
		role := ""
		if roleRaw, exists := claims["role"]; exists {
			role, _ = roleRaw.(string)
		}

		user := &PortalUser{
			ID:   int64(subFloat),
			Role: role,
		}

		ctx := context.WithValue(r.Context(), ctxKeyPortalUser{}, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// AuthPortalSession valida a sessão do portal via cookie senderzz_portal_session
// ou header X-Senderzz-Token. Alternativa ao JWT para clientes SPA com cookies.
//
// Espelha AuthPortal de go/motoboy/internal/auth/auth.go:
//   - token_hash = HMAC-SHA256(token_raw, WP_SALT_AUTH)
//   - lookup em wp_senderzz_portal_sessions JOIN wp_senderzz_portal_users
//
// Requer env var WP_SALT_AUTH (equivalente a AUTH_SALT do WordPress).
func AuthPortalSession(pool *pgxpool.Pool) func(http.Handler) http.Handler {
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

// ── Helpers internos ──────────────────────────────────────────────────────────

func hmacSHA256Hex(message, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}
