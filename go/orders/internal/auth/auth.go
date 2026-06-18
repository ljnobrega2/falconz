// Package auth implementa os middlewares de autenticação do serviço Orders.
//
// AuthJWT — valida token Bearer HS256 emitido pelo PHP (tpc_jwt_encode) ou pelo
// próprio serviço Go. Injeta user_id e role no contexto.
//
// AuthPortal — valida sessão de portal via cookie senderzz_portal_session ou
// header X-Senderzz-Token. Compatível com Portal_Auth.php (HMAC-SHA256 + lookup
// em senderzz_portal_sessions). Mesma implementação do serviço Motoboy.
//
// Fail-closed em todos os middlewares:
//   - JWT_SECRET vazio → 503 (não expõe que está mal configurado)
//   - WP_SALT_AUTH vazio → 503
//   - Token ausente/inválido → 401
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

	"github.com/senderzz/orders-service/internal/httpx"
)

// ── Context keys ──────────────────────────────────────────────────────────────

// Tipos opacos para context keys — evita colisões com pacotes externos.
type ctxKeyUserID struct{}
type ctxKeyRole struct{}
type ctxKeyPortalUser struct{}

// PortalUser representa o usuário de portal autenticado via sessão.
type PortalUser struct {
	ID   int64
	Role string
}

// ── Helpers de extração de contexto ──────────────────────────────────────────

// GetUserID recupera o user_id injetado pelo middleware AuthJWT.
// Retorna 0 se o contexto não tiver sido populado (não deve ocorrer em rotas protegidas).
func GetUserID(ctx context.Context) int64 {
	v, _ := ctx.Value(ctxKeyUserID{}).(int64)
	return v
}

// GetRole recupera a role do usuário autenticado (ex: "admin", "producer", "affiliate").
// Retorna string vazia se ausente.
func GetRole(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyRole{}).(string)
	return v
}

// PortalUserFromCtx retorna o PortalUser autenticado do contexto, ou nil.
func PortalUserFromCtx(ctx context.Context) *PortalUser {
	v, _ := ctx.Value(ctxKeyPortalUser{}).(*PortalUser)
	return v
}

// IsAdmin retorna true se o usuário autenticado tem role admin ou manage_woocommerce.
func IsAdmin(ctx context.Context) bool {
	role := GetRole(ctx)
	return role == "admin" || role == "manage_woocommerce"
}

// ── AuthJWT ───────────────────────────────────────────────────────────────────

// AuthJWT é um middleware chi que valida o token Bearer HS256 emitido pelo WP ou
// pelo próprio serviço Go.
//
// Fluxo:
//  1. Lê JWT_SECRET da env — se vazio retorna 503 (fail-closed).
//  2. Extrai o token do header "Authorization: Bearer <token>".
//  3. Valida assinatura HS256 e exp com golang-jwt.
//  4. Extrai claim "sub" (int64 = user_id) e injeta no contexto.
//  5. Extrai claim "role" (string, opcional) e injeta no contexto.
func AuthJWT(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secret := os.Getenv("JWT_SECRET")
		if secret == "" {
			// Configuração ausente — fail-closed para não processar requests sem auth.
			slog.Error("[orders/auth] JWT_SECRET não configurado — rejeitando requisição",
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
		// O PHP emite: header={alg:HS256,typ:JWT}, payload={sub:user_id,iat:...,exp:...}
		token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
			// Garante que o algoritmo seja exatamente HS256 — rejeita RS256 etc.
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return []byte(secret), nil
		}, jwt.WithValidMethods([]string{"HS256"}))

		if err != nil || !token.Valid {
			slog.Warn("[orders/auth] token JWT inválido",
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

		userID := int64(subFloat)
		ctx := context.WithValue(r.Context(), ctxKeyUserID{}, userID)

		// Extrai role se presente no token (campo opcional — emitido pelo Go, não pelo PHP legado).
		if roleRaw, ok := claims["role"].(string); ok && roleRaw != "" {
			ctx = context.WithValue(ctx, ctxKeyRole{}, roleRaw)
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ── AuthPortal ────────────────────────────────────────────────────────────────

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
				slog.Error("[orders/auth] WP_SALT_AUTH não configurado — todas as sessões portal serão rejeitadas")
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
				slog.Warn("[orders/auth] sessão portal inválida ou expirada", "err", err)
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
