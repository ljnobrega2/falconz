// CODE-AUTH-03 — wrapper bcrypt compartilhado.
//
// O compare/hash de senha via bcrypt está espalhado entre os serviços:
//
//	admin/internal/handlers/auth.go       → bcrypt.CompareHashAndPassword
//	admin/internal/handlers/onboarding.go → bcrypt.GenerateFromPassword(.., 12)
//	portal/internal/handlers/auth.go      → bcrypt.CompareHashAndPassword
//	portal/internal/handlers/users_portal.go / settings.go → GenerateFromPassword(DefaultCost)
//
// Estes wrappers padronizam o uso e dão erros sentinela (errors.Is) para o caller
// distinguir "senha errada" de "hash corrompido", sem vazar detalhe pro usuário.
// Mantém o módulo shared leve: depende só de golang.org/x/crypto/bcrypt.
package auth

import (
	"errors"

	"golang.org/x/crypto/bcrypt"
)

// CustoPadrao é o custo bcrypt default (== bcrypt.DefaultCost, hoje 10).
// Usado por portal (login/cadastro). O admin onboarding usa custo 12 — passe
// HashSenhaComCusto(senha, 12) nesses pontos para manter o comportamento fiel.
const CustoPadrao = bcrypt.DefaultCost

var (
	// ErrSenhaIncorreta: a senha não casa com o hash. Responder 401 (genérico).
	ErrSenhaIncorreta = errors.New("auth: senha incorreta")
	// ErrHashInvalido: o hash armazenado não é um bcrypt válido (dado corrompido).
	ErrHashInvalido = errors.New("auth: hash bcrypt inválido")
)

// HashSenha gera o hash bcrypt da senha com o custo padrão (CustoPadrao).
func HashSenha(senha string) (string, error) {
	return HashSenhaComCusto(senha, CustoPadrao)
}

// HashSenhaComCusto gera o hash bcrypt com o custo informado.
// Use custo 12 onde o admin onboarding já usa 12; CustoPadrao no resto.
func HashSenhaComCusto(senha string, custo int) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(senha), custo)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

// VerificarSenha compara a senha em claro com o hash bcrypt armazenado.
//
// Retorno:
//   - nil                → senha correta.
//   - ErrSenhaIncorreta  → senha não casa (bcrypt.ErrMismatchedHashAndPassword).
//   - ErrHashInvalido    → hash vazio/corrompido (não é bcrypt válido).
//
// O chamador deve responder o MESMO erro genérico ("credenciais inválidas") para
// ErrSenhaIncorreta e ErrHashInvalido — não revelar qual dos dois ao usuário.
func VerificarSenha(hash, senha string) error {
	if hash == "" {
		return ErrHashInvalido
	}
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(senha))
	if err == nil {
		return nil
	}
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return ErrSenhaIncorreta
	}
	// Demais erros (hash com prefixo/custo inválido) = hash corrompido.
	return ErrHashInvalido
}
