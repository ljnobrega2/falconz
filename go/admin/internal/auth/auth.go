// Package auth — autenticação admin.
//
// Admin = usuário super-privilegiado, separado dos portal users.
// Login via email+senha (bcrypt). Token JWT HS256 com claim role=admin.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ctxKey int

const adminKey ctxKey = 1

type Admin struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
	Nome  string `json:"nome"`
}

type Claims struct {
	AdminID int64  `json:"sub"`
	Email   string `json:"email"`
	jwt.RegisteredClaims
}

func secret() []byte {
	s := os.Getenv("ADMIN_JWT_SECRET")
	if s == "" {
		s = os.Getenv("JWT_SECRET")
	}
	return []byte(s)
}

// IssueToken emite JWT válido por 12h.
func IssueToken(a Admin) (string, error) {
	claims := Claims{
		AdminID: a.ID,
		Email:   a.Email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(12 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "senderzz-admin",
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString(secret())
}

// Middleware valida Bearer JWT e injeta Admin no ctx.
func Middleware(pool *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			if !strings.HasPrefix(h, "Bearer ") {
				writeErr(w, http.StatusUnauthorized, "token ausente")
				return
			}
			tokStr := strings.TrimPrefix(h, "Bearer ")
			tok, err := jwt.ParseWithClaims(tokStr, &Claims{}, func(t *jwt.Token) (any, error) {
				if t.Method.Alg() != "HS256" {
					return nil, errors.New("alg inválido")
				}
				return secret(), nil
			})
			if err != nil || !tok.Valid {
				writeErr(w, http.StatusUnauthorized, "token inválido")
				return
			}
			cl := tok.Claims.(*Claims)
			var a Admin
			err = pool.QueryRow(r.Context(),
				`SELECT id, email, nome FROM senderzz_admin_users WHERE id=$1 AND ativo=TRUE`, cl.AdminID).
				Scan(&a.ID, &a.Email, &a.Nome)
			if err != nil {
				writeErr(w, http.StatusUnauthorized, "admin inválido")
				return
			}
			ctx := context.WithValue(r.Context(), adminKey, &a)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func FromCtx(ctx context.Context) *Admin {
	v, _ := ctx.Value(adminKey).(*Admin)
	return v
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": msg}})
}
