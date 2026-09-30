// Package auth — autenticação admin.
//
// Admin = usuário super-privilegiado, separado dos portal users.
// Login via email+senha (bcrypt). Token JWT HS256 com claim role=admin.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
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
			},
				jwt.WithValidMethods([]string{"HS256"}),
				// SEC-GO-04: exige iss=senderzz-admin. Mesmo que ADMIN_JWT_SECRET caia
				// no JWT_SECRET compartilhado, tokens de portal (iss=senderzz-portal) /
				// wallet / orders (iss vazio) são rejeitados — fecha o privesc por
				// confusão de token entre serviços.
				jwt.WithIssuer("senderzz-admin"),
				// SEC-GO-09: token sem exp não é aceito como eterno.
				jwt.WithExpirationRequired(),
			)
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

// P0-04: rate-limit opcional do POST /login — janela de 15min por IP.
// Defesa contra brute-force de credenciais. Mapa em memória + mutex, com janela
// deslizante por IP; chave = IP do RealIP (o middleware RealIP roda antes e já
// normalizou r.RemoteAddr). Excedeu → 429 (não chega ao handler, então uma
// tentativa legítima conta no máximo como 1 de 5 — não quebra login válido).
// A configuração explícita fica disponível via ADMIN_LOGIN_RATE_MAX. Sem valor,
// o limite é efetivamente desativado para não bloquear logins legítimos.
const (
	loginRateWindow = 15 * time.Minute
)

// loginRateMax é variável de pacote inicializada do ambiente no startup.
var loginRateMax = loadLoginRateMax()

// loadLoginRateMax lê ADMIN_LOGIN_RATE_MAX: se setado e parseável p/ int positivo,
// usa-o; caso contrário devolve um teto efetivamente infinito.
func loadLoginRateMax() int {
	if v := os.Getenv("ADMIN_LOGIN_RATE_MAX"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	// OWNER-2026-06-23: rate-limit de login DESLIGADO em definitivo (ordem do dono —
	// "muitas tentativas" travava logins legítimos). Default efetivamente infinito.
	return 1 << 30
}

// LoginRateLimit devolve um middleware com estado compartilhado (mapa+mutex
// capturados no closure, criados uma única vez quando montado em main.go).
func LoginRateLimit() func(http.Handler) http.Handler {
	var mu sync.Mutex
	hits := make(map[string][]time.Time)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r)
			now := time.Now()

			mu.Lock()
			// Poda timestamps fora da janela (mapa não cresce sem limite).
			win := hits[ip][:0]
			for _, t := range hits[ip] {
				if now.Sub(t) < loginRateWindow {
					win = append(win, t)
				}
			}
			if len(win) >= loginRateMax {
				hits[ip] = win
				mu.Unlock()
				slog.Warn("[auth] P0-04 rate-limit de login excedido", "ip", ip, "tentativas", len(win))
				w.Header().Set("Retry-After", "900")
				writeErr(w, http.StatusTooManyRequests, "muitas tentativas de login — tente novamente em alguns minutos")
				return
			}
			hits[ip] = append(win, now)
			mu.Unlock()

			next.ServeHTTP(w, r)
		})
	}
}

// clientIP extrai o IP de r.RemoteAddr (já normalizado pelo middleware RealIP),
// removendo a porta de forma defensiva.
func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
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
