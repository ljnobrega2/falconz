// CODE-AUTH-03 — testes do wrapper bcrypt.
package auth

import (
	"errors"
	"testing"
)

func TestHashEVerificarSenha_Match(t *testing.T) {
	hash, err := HashSenha("s3nh@-forte")
	if err != nil {
		t.Fatalf("HashSenha falhou: %v", err)
	}
	if hash == "" {
		t.Fatal("hash vazio")
	}
	if err := VerificarSenha(hash, "s3nh@-forte"); err != nil {
		t.Errorf("esperava match, obtido: %v", err)
	}
}

func TestVerificarSenha_Mismatch(t *testing.T) {
	hash, _ := HashSenha("certa")
	err := VerificarSenha(hash, "errada")
	if !errors.Is(err, ErrSenhaIncorreta) {
		t.Errorf("esperava ErrSenhaIncorreta, obtido: %v", err)
	}
}

func TestVerificarSenha_HashVazio(t *testing.T) {
	if err := VerificarSenha("", "qualquer"); !errors.Is(err, ErrHashInvalido) {
		t.Errorf("esperava ErrHashInvalido para hash vazio, obtido: %v", err)
	}
}

func TestVerificarSenha_HashCorrompido(t *testing.T) {
	if err := VerificarSenha("não-é-um-hash-bcrypt", "qualquer"); !errors.Is(err, ErrHashInvalido) {
		t.Errorf("esperava ErrHashInvalido para hash corrompido, obtido: %v", err)
	}
}

func TestHashSenhaComCusto_12(t *testing.T) {
	// Admin onboarding usa custo 12 — deve produzir hash válido e verificável.
	hash, err := HashSenhaComCusto("admin-pass", 12)
	if err != nil {
		t.Fatalf("HashSenhaComCusto(12) falhou: %v", err)
	}
	if err := VerificarSenha(hash, "admin-pass"); err != nil {
		t.Errorf("esperava match com custo 12, obtido: %v", err)
	}
}
