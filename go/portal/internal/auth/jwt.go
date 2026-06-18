// Package auth implementa autenticação JWT e middleware do Portal V2 em Go.
// Fase 5 da migração WP → Go.
//
// Dois tipos de token JWT são emitidos:
//   - partial_token: JWT de curta duração (5 min) emitido após credenciais válidas,
//     ANTES da verificação de 2FA. Contém claim "pending_2fa=true". Não pode ser
//     usado como Bearer em rotas protegidas (AuthPortalJWT rejeita tokens parciais).
//   - token JWT completo: emitido após 2FA confirmado. Contém session_token (raw)
//     para lookup na tabela senderzz_portal_sessions. Expira em 24h.
//
// Fail-closed: JWT_SECRET vazio → 503 em todas as rotas autenticadas.
package auth

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ── Tipos públicos ────────────────────────────────────────────────────────────

// PortalUser representa o usuário autenticado injetado no contexto via middleware.
type PortalUser struct {
	ID           int64
	WPUserID     int64
	Email        string
	Role         string // produtor | afiliado | operator
	Name         string
	ClassID      int64
	Status       string
	SessionToken string // token raw da sessão — usado apenas internamente pelo middleware
}

// PortalClaims são os claims do JWT completo (pós-2FA).
type PortalClaims struct {
	UserID       int64  `json:"user_id"`
	Email        string `json:"email"`
	Role         string `json:"role"`
	SessionToken string `json:"sid"` // token raw da sessão — para lookup em senderzz_portal_sessions
	PendingTwoFA bool   `json:"pending_2fa,omitempty"` // true = token parcial, não deve ser aceito por AuthPortalJWT
	jwt.RegisteredClaims
}

// PartialClaims são os claims do JWT parcial emitido após credenciais válidas
// mas ANTES da verificação de 2FA. Contém pending_2fa=true.
type PartialClaims struct {
	UserID int64 `json:"user_id"`
	Email  string `json:"email"`
	jwt.RegisteredClaims
}

// ctxKeyPortalUser é a chave opaca de contexto — evita colisões com outros pacotes.
type ctxKeyPortalUser struct{}

// FromContext extrai o PortalUser autenticado do context (nil se não autenticado).
func FromContext(ctx context.Context) *PortalUser {
	u, _ := ctx.Value(ctxKeyPortalUser{}).(*PortalUser)
	return u
}

// contextWithUser injeta o PortalUser no contexto.
func contextWithUser(ctx context.Context, u *PortalUser) context.Context {
	return context.WithValue(ctx, ctxKeyPortalUser{}, u)
}

// ── Emissão de tokens ─────────────────────────────────────────────────────────

// EmitJWT emite um JWT completo (pós-2FA) com validade de 24h.
// O claim "sid" carrega o token raw da sessão para lookup em senderzz_portal_sessions.
// Retorna erro se JWT_SECRET não estiver configurado.
func EmitJWT(userID int64, email, role, sessionToken string) (string, error) {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return "", fmt.Errorf("[jwt] JWT_SECRET não configurado — fail-closed")
	}

	now := time.Now()
	claims := PortalClaims{
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

// EmitPartialJWT emite um JWT parcial com validade de 5 minutos.
// Usado no fluxo de 2FA: emitido após credenciais válidas, antes do código de 2FA.
// O campo pending_2fa=true impede uso em rotas protegidas.
func EmitPartialJWT(userID int64, email string) (string, error) {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return "", fmt.Errorf("[jwt] JWT_SECRET não configurado — fail-closed")
	}

	now := time.Now()
	// Usamos PortalClaims com PendingTwoFA=true para o AuthPortalJWT rejeitar.
	claims := PortalClaims{
		UserID:       userID,
		Email:        email,
		PendingTwoFA: true,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   fmt.Sprintf("%d", userID),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute)),
			Issuer:    "senderzz-portal",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ParseJWT valida e parseia um JWT completo (HS256).
// Rejeita tokens parciais (PendingTwoFA=true) — chamador deve usar ParsePartialJWT.
// Retorna ErrTokenUnverifiable se JWT_SECRET não estiver configurado (fail-closed).
func ParseJWT(tokenStr string) (*PortalClaims, error) {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return nil, fmt.Errorf("[jwt] JWT_SECRET não configurado — fail-closed")
	}

	token, err := jwt.ParseWithClaims(tokenStr, &PortalClaims{}, func(t *jwt.Token) (any, error) {
		// Garante HS256 — rejeita RS256 e qualquer outro algoritmo.
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("[jwt] algoritmo inesperado: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{"HS256"}))

	if err != nil {
		return nil, fmt.Errorf("[jwt] token inválido: %w", err)
	}

	claims, ok := token.Claims.(*PortalClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("[jwt] claims inválidas")
	}

	// Rejeita token parcial — não pode ser usado como autenticação completa.
	if claims.PendingTwoFA {
		return nil, fmt.Errorf("[jwt] token parcial (2FA pendente) — usar /portal/login/2fa")
	}

	return claims, nil
}

// ParsePartialJWT valida e parseia um JWT parcial (PendingTwoFA=true).
// Usado exclusivamente no handler POST /portal/login/2fa.
func ParsePartialJWT(tokenStr string) (*PortalClaims, error) {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return nil, fmt.Errorf("[jwt] JWT_SECRET não configurado — fail-closed")
	}

	token, err := jwt.ParseWithClaims(tokenStr, &PortalClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("[jwt] algoritmo inesperado: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{"HS256"}))

	if err != nil {
		return nil, fmt.Errorf("[jwt] token parcial inválido: %w", err)
	}

	claims, ok := token.Claims.(*PortalClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("[jwt] claims inválidas no token parcial")
	}

	// Garante que é de fato parcial — rejeita token completo aqui.
	if !claims.PendingTwoFA {
		return nil, fmt.Errorf("[jwt] token não é parcial — esperado pending_2fa=true")
	}

	return claims, nil
}
