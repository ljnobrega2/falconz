// Package auth — JWT helpers compartilhados entre serviços.
// Todos os serviços usam o mesmo JWT_SECRET e o mesmo formato de claims.
package auth

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims é o payload JWT padrão Senderzz.
type Claims struct {
	UserID int64  `json:"user_id"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

type contextKey string

const claimsKey contextKey = "sz_claims"

// Emit gera um JWT assinado com JWT_SECRET do ambiente.
func Emit(userID int64, email, role string, ttl time.Duration) (string, error) {
	secret := os.Getenv("JWT_SECRET")
	if len(secret) < 32 {
		return "", fmt.Errorf("JWT_SECRET ausente ou curto demais")
	}
	claims := Claims{
		UserID: userID,
		Email:  email,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   fmt.Sprintf("%d", userID),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

// Parse valida e retorna as claims de um JWT lendo o secret do ambiente.
// Conveniência sobre ParseHS256 (claims.go): delega a validação pura para lá,
// herdando WithValidMethods(HS256) + exp obrigatório (SEC-GO-02 / CODE-AUTH-03).
// Não exige issuer (passa ""). Para exigir issuer, use ParseHS256 diretamente.
func Parse(tokenStr string) (*Claims, error) {
	return ParseHS256(os.Getenv("JWT_SECRET"), tokenStr, "")
}

// Middleware HTTP que valida JWT e injeta claims no contexto.
// Aceita Authorization: Bearer <token> ou Cookie sz_portal_token.
func Middleware(next http.Handler) http.Handler {
	secret := os.Getenv("JWT_SECRET")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if secret == "" {
			http.Error(w, `{"ok":false,"erro":"configuração ausente"}`, http.StatusServiceUnavailable)
			return
		}

		tokenStr := extractToken(r)
		if tokenStr == "" {
			http.Error(w, `{"ok":false,"erro":"token ausente"}`, http.StatusUnauthorized)
			return
		}

		claims, err := Parse(tokenStr)
		if err != nil {
			http.Error(w, `{"ok":false,"erro":"token inválido"}`, http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), claimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireRole devolve middleware que só deixa passar os roles listados.
func RequireRole(roles ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]bool, len(roles))
	for _, r := range roles {
		allowed[r] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c := GetClaims(r.Context())
			if c == nil || !allowed[c.Role] {
				http.Error(w, `{"ok":false,"erro":"acesso negado"}`, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// GetClaims retorna as claims do contexto (nil se não autenticado).
func GetClaims(ctx context.Context) *Claims {
	c, _ := ctx.Value(claimsKey).(*Claims)
	return c
}

func extractToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	if c, err := r.Cookie("sz_portal_token"); err == nil {
		return c.Value
	}
	return ""
}
