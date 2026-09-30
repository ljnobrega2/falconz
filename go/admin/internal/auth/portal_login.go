// Package auth — login unificado: validação de senha portal + emissão de token portal.
//
// FEAT-RBAC-2026-06-21
//
// O /login do admin agora valida credenciais contra senderzz_admin_users E
// senderzz_portal_users. Este arquivo isola a parte "portal" que o módulo go/admin
// precisa replicar do go/portal (módulos Go separados, sem dependência cruzada):
//
//  1. VerifyPortalPassword — espelha go/portal/internal/auth/password.go::VerifyPassword
//     (CRIT-WP-HASH): suporta hashes WordPress 6.8+ ("$wp$") e bcrypt puro ("$2a/$2b/$2y").
//  2. IssuePortalSession — espelha go/portal/internal/handlers/auth.go::createSession:
//     grava uma linha em senderzz_portal_sessions (token raw + HMAC com WP_SALT_AUTH,
//     TTL 24h). Sem essa linha o JWT portal seria um token MORTO — o middleware
//     AuthPortalJWT do go/portal valida o claim "sid" contra essa tabela.
//  3. IssuePortalToken — espelha go/portal/internal/auth/jwt.go::EmitJWT:
//     JWT HS256 iss=senderzz-portal (NUNCA senderzz-admin — admin_users e portal_users
//     são sequências IDENTITY separadas; um token admin-iss para um portal user id N
//     autenticaria como admin #N — privesc). Assinado com JWT_SECRET (não ADMIN_JWT_SECRET).
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// wpHMACKey é a chave fixa do core do WordPress para o pré-hash SHA-384.
const wpHMACKey = "wp-sha384"

// wpPreHash reproduz base64_encode( hash_hmac('sha384', senha, 'wp-sha384', true) ).
// SHA-384 vive em crypto/sha512 (não há pacote crypto/sha384).
func wpPreHash(senha string) string {
	mac := hmac.New(sha512.New384, []byte(wpHMACKey))
	mac.Write([]byte(senha))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// VerifyPortalPassword verifica uma senha em texto puro contra o hash do portal.
// Função PURA, fail-closed. Espelho fiel de go/portal VerifyPassword (CRIT-WP-HASH):
//  1. "$wp$"  → WordPress 6.8+: bcrypt(pré-hash HMAC-SHA384) sobre hash[3:].
//     Remove SOMENTE "$wp" (3 chars) — o "$" de "$2y$" é do bcrypt.
//  2. "$2a$"/"$2b$"/"$2y$"  → bcrypt puro (contas criadas pelo Go).
// phpass "$P$"/"$H$" e hash vazio → false.
func VerifyPortalPassword(hash, senha string) bool {
	if hash == "" {
		return false // fail-closed: conta sem senha nativa não loga.
	}
	switch {
	case strings.HasPrefix(hash, "$wp$"):
		bcryptHash := hash[3:] // CRIT-WP-HASH: strip exatamente 3 chars.
		pre := wpPreHash(senha)
		return bcrypt.CompareHashAndPassword([]byte(bcryptHash), []byte(pre)) == nil
	case strings.HasPrefix(hash, "$2a$"),
		strings.HasPrefix(hash, "$2b$"),
		strings.HasPrefix(hash, "$2y$"):
		return bcrypt.CompareHashAndPassword([]byte(hash), []byte(senha)) == nil
	default:
		return false // phpass e qualquer outro prefixo → fail-closed.
	}
}

// portalClaims espelha go/portal PortalClaims (campos consumidos por ParseJWT/AuthPortalJWT).
type portalClaims struct {
	UserID       int64  `json:"user_id"`
	Email        string `json:"email"`
	Role         string `json:"role"`
	SessionToken string `json:"sid"`
	jwt.RegisteredClaims
}

// IssuePortalSession cria a sessão em senderzz_portal_sessions (espelho de createSession
// do go/portal) e devolve o token RAW para embutir no claim "sid". Sem essa linha o JWT
// portal não passa pelo middleware AuthPortalJWT (que valida "sid" contra a tabela).
func IssuePortalSession(ctx context.Context, pool *pgxpool.Pool, userID int64, r *http.Request) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("erro ao gerar token de sessão: %w", err)
	}
	tokenRaw := hex.EncodeToString(buf)

	// HMAC com WP_SALT_AUTH — idêntico ao go/portal (compat raw|hmac na validação).
	salt := os.Getenv("WP_SALT_AUTH")
	mac := hmac.New(sha256.New, []byte(salt))
	mac.Write([]byte(tokenRaw))
	tokenHMAC := hex.EncodeToString(mac.Sum(nil))

	ip := r.RemoteAddr
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ip = xff
	}

	_, err := pool.Exec(ctx,
		`INSERT INTO senderzz_portal_sessions
		    (user_id, token, token_hmac, ip, user_agent, expires_at)
		 VALUES ($1, $2, $3, $4, $5, NOW() + INTERVAL '24 hours')`,
		userID, tokenRaw, tokenHMAC, ip, r.UserAgent(),
	)
	if err != nil {
		return "", fmt.Errorf("erro ao inserir sessão portal: %w", err)
	}
	return tokenRaw, nil
}

// IssuePortalToken emite um JWT completo do PORTAL (iss=senderzz-portal, TTL 24h),
// assinado com JWT_SECRET — espelho de go/portal EmitJWT. NUNCA usar IssueToken (admin)
// aqui: o issuer senderzz-admin daria privesc por colisão de id entre as tabelas.
func IssuePortalToken(userID int64, email, role, sessionToken string) (string, error) {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return "", fmt.Errorf("[jwt] JWT_SECRET não configurado — fail-closed")
	}
	now := time.Now()
	claims := portalClaims{
		UserID:       userID,
		Email:        email,
		Role:         role,
		SessionToken: sessionToken,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   fmt.Sprintf("%d", userID),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(24 * time.Hour)),
			Issuer:    "senderzz-portal",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}
