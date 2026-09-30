// CODE-AUTH-03 — helper PURO de validação de claims JWT, compartilhado.
//
// # Por que existe
//
// A validação de JWT HS256 está duplicada e DIVERGENTE entre os serviços:
//
//	go/admin/internal/auth/auth.go     → ParseWithClaims + WithValidMethods(HS256)
//	                                     + WithExpirationRequired() + WithIssuer(...)
//	go/portal/internal/auth/jwt.go     → ParseWithClaims + WithValidMethods(HS256),
//	                                     porém SEM WithExpirationRequired (gap SEC-GO-02)
//	go/shared/pkg/auth/jwt.go (Parse)  → SEM WithValidMethods e SEM exp obrigatório
//
// ParseHS256 é o DENOMINADOR COMUM FIEL + as travas de segurança que faltavam:
//   - assinatura HS256 obrigatória (WithValidMethods) — rejeita "alg:none" e RS256;
//   - exp OBRIGATÓRIO (SEC-GO-02) — token sem exp não é aceito como eterno;
//   - issuer opcional (passe "" para não exigir) — admin exige "senderzz-admin",
//     portal usa "senderzz-portal" (SEC-GO-04 / confusão de token entre serviços).
//
// É PURO: recebe o secret por parâmetro (NÃO lê os.Getenv) e NÃO depende de pgx
// nem de net/http — mantém o módulo shared leve e 100% testável.
package auth

import (
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
)

// Erros sentinela — permitem ao chamador distinguir os casos (errors.Is).
var (
	// ErrSecretVazio: secret ausente. Fail-closed — o chamador deve responder 503.
	ErrSecretVazio = errors.New("auth: JWT_SECRET vazio")
	// ErrTokenInvalido: assinatura/exp/issuer inválidos ou alg inesperado. Responder 401.
	ErrTokenInvalido = errors.New("auth: token inválido")
)

// ParseHS256 valida um JWT HS256 e devolve as claims tipadas.
//
// secret : chave HMAC (NÃO lida do ambiente aqui — responsabilidade do chamador).
// token  : a string do JWT (sem o prefixo "Bearer ").
// issuer : se != "", exige claim iss == issuer; se == "", não verifica issuer.
//
// Garantias (CODE-AUTH-03 / SEC-GO-02):
//   - apenas HS256 é aceito (WithValidMethods);
//   - exp é obrigatório (WithExpirationRequired) e validado contra agora;
//   - secret vazio devolve ErrSecretVazio (fail-closed, NÃO valida nada).
func ParseHS256(secret, token, issuer string) (*Claims, error) {
	if secret == "" {
		return nil, ErrSecretVazio
	}

	opts := []jwt.ParserOption{
		jwt.WithValidMethods([]string{"HS256"}), // só HS256 — barra alg:none / RS256
		jwt.WithExpirationRequired(),            // SEC-GO-02: sem exp = rejeitado
	}
	if issuer != "" {
		opts = append(opts, jwt.WithIssuer(issuer)) // SEC-GO-04: cross-service confusion
	}

	tok, err := jwt.ParseWithClaims(token, &Claims{}, func(t *jwt.Token) (any, error) {
		// Redundante com WithValidMethods, mas explícito: rejeita não-HMAC.
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("%w: algoritmo inesperado %v", ErrTokenInvalido, t.Header["alg"])
		}
		return []byte(secret), nil
	}, opts...)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTokenInvalido, err)
	}

	claims, ok := tok.Claims.(*Claims)
	if !ok || !tok.Valid {
		return nil, ErrTokenInvalido
	}
	return claims, nil
}

// Subject devolve o claim "sub" das claims (string, ex.: id do usuário serializado).
// Atalho para extrair o sub sem o chamador navegar RegisteredClaims.
func (c *Claims) Subject() string {
	if c == nil {
		return ""
	}
	return c.RegisteredClaims.Subject
}

// RoleOf devolve o claim "role" (vazio se claims nil). Atalho de extração.
func (c *Claims) RoleOf() string {
	if c == nil {
		return ""
	}
	return c.Role
}
