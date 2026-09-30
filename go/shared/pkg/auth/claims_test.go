// CODE-AUTH-03 — testes do helper puro de JWT.
package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const segredoTeste = "segredo-de-teste-com-mais-de-32-caracteres!!"

// emitir gera um JWT HS256 de teste com controle total sobre exp/issuer/alg.
func emitir(t *testing.T, secret string, comExp bool, issuer string) string {
	t.Helper()
	rc := jwt.RegisteredClaims{
		Subject:  "42",
		IssuedAt: jwt.NewNumericDate(time.Now()),
		Issuer:   issuer,
	}
	if comExp {
		rc.ExpiresAt = jwt.NewNumericDate(time.Now().Add(time.Hour))
	}
	claims := Claims{UserID: 42, Email: "x@y.z", Role: "produtor", RegisteredClaims: rc}
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("falha ao emitir token de teste: %v", err)
	}
	return s
}

func TestParseHS256_Valido(t *testing.T) {
	tok := emitir(t, segredoTeste, true, "")
	c, err := ParseHS256(segredoTeste, tok, "")
	if err != nil {
		t.Fatalf("esperava token válido, erro: %v", err)
	}
	if c.Subject() != "42" {
		t.Errorf("sub esperado=42, obtido=%q", c.Subject())
	}
	if c.RoleOf() != "produtor" {
		t.Errorf("role esperado=produtor, obtido=%q", c.RoleOf())
	}
}

func TestParseHS256_Expirado(t *testing.T) {
	// Token já expirado (exp no passado).
	claims := Claims{
		UserID: 1, Role: "afiliado",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "1",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
		},
	}
	tok, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(segredoTeste))
	_, err := ParseHS256(segredoTeste, tok, "")
	if err == nil {
		t.Fatal("esperava erro para token expirado")
	}
	if !errors.Is(err, ErrTokenInvalido) {
		t.Errorf("esperava ErrTokenInvalido, obtido: %v", err)
	}
}

func TestParseHS256_SemExp_Rejeitado(t *testing.T) {
	// SEC-GO-02: token sem claim exp NÃO pode ser aceito como eterno.
	tok := emitir(t, segredoTeste, false, "")
	_, err := ParseHS256(segredoTeste, tok, "")
	if err == nil {
		t.Fatal("SEC-GO-02 falhou: token SEM exp foi aceito")
	}
	if !errors.Is(err, ErrTokenInvalido) {
		t.Errorf("esperava ErrTokenInvalido, obtido: %v", err)
	}
}

func TestParseHS256_SecretVazio_FailClosed(t *testing.T) {
	tok := emitir(t, segredoTeste, true, "")
	_, err := ParseHS256("", tok, "")
	if !errors.Is(err, ErrSecretVazio) {
		t.Errorf("esperava ErrSecretVazio (fail-closed), obtido: %v", err)
	}
}

func TestParseHS256_SecretErrado(t *testing.T) {
	tok := emitir(t, segredoTeste, true, "")
	_, err := ParseHS256("outro-segredo-completamente-diferente-aqui!!", tok, "")
	if !errors.Is(err, ErrTokenInvalido) {
		t.Errorf("esperava ErrTokenInvalido para assinatura inválida, obtido: %v", err)
	}
}

func TestParseHS256_IssuerCorreto(t *testing.T) {
	tok := emitir(t, segredoTeste, true, "senderzz-admin")
	if _, err := ParseHS256(segredoTeste, tok, "senderzz-admin"); err != nil {
		t.Fatalf("esperava aceitar issuer correto, erro: %v", err)
	}
}

func TestParseHS256_IssuerErrado_Rejeitado(t *testing.T) {
	// SEC-GO-04: token de outro serviço (issuer diferente) é rejeitado.
	tok := emitir(t, segredoTeste, true, "senderzz-portal")
	_, err := ParseHS256(segredoTeste, tok, "senderzz-admin")
	if !errors.Is(err, ErrTokenInvalido) {
		t.Errorf("esperava rejeitar issuer divergente, obtido: %v", err)
	}
}

func TestParseHS256_AlgNone_Rejeitado(t *testing.T) {
	// "alg":none não deve passar — WithValidMethods(HS256) barra.
	claims := Claims{RegisteredClaims: jwt.RegisteredClaims{
		Subject:   "1",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("falha ao gerar token alg:none: %v", err)
	}
	if _, err := ParseHS256(segredoTeste, tok, ""); !errors.Is(err, ErrTokenInvalido) {
		t.Errorf("esperava rejeitar alg:none, obtido: %v", err)
	}
}

// Parse (jwt.go) deve herdar as travas — sem exp via Parse também rejeita.
func TestParse_SemExp_Rejeitado(t *testing.T) {
	t.Setenv("JWT_SECRET", segredoTeste)
	tok := emitir(t, segredoTeste, false, "")
	if _, err := Parse(tok); err == nil {
		t.Fatal("Parse aceitou token sem exp — SEC-GO-02 não herdado")
	}
}
