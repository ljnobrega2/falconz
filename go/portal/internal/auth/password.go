// Package auth — verificação de senha compatível com o WordPress.
//
// Contexto de cutover (CRIT-WP-HASH): os dados reais foram importados do WP e as
// senhas em senderzz_portal_users.password_hash usam o formato WordPress 6.8+ com
// prefixo "$wp$" — NÃO é bcrypt puro. O login em Go fazia
// bcrypt.CompareHashAndPassword direto contra esse hash e FALHAVA para 100% dos
// usuários reais (nenhum logava). Esta função reproduz fielmente o ramo "$wp$" do
// wp_check_password do core 6.8 e mantém o bcrypt puro como fallback para contas
// novas/demo criadas pelo próprio Go (users_portal.go grava $2y$).
//
// Formato WP 6.8 ("$wp$"):
//
//	stored = "$wp" + bcrypt( base64_std( HMAC_SHA384(key="wp-sha384", msg=senha) ) )
//
// O pré-hash HMAC-SHA384 (64 bytes em base64) resolve o teto de 72 bytes do bcrypt
// (senhas longas deixam de ser truncadas). A verificação no core é:
//
//	$pre   = base64_encode( hash_hmac('sha384', $senha, 'wp-sha384', true) );
//	$check = password_verify( $pre, substr($hash, 3) );  // substr 3 = remove "$wp"
//
// ATENÇÃO ao offset: removemos APENAS "$wp" (3 chars). O "$" que vem antes de "2y"
// pertence ao próprio hash bcrypt ($2y$...) — bater 4 chars quebraria o bcrypt. Os
// hashes reais no banco têm 63 chars ($wp$2y$10$... → 60 chars de bcrypt após o [3:]).
package auth

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/base64"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// wpHMACKey é a chave fixa do core do WordPress para o pré-hash SHA-384.
const wpHMACKey = "wp-sha384"

// wpPreHash reproduz base64_encode( hash_hmac('sha384', senha, 'wp-sha384', true) ).
// SHA-384 vive em crypto/sha512 (não há pacote crypto/sha384). O base64 é o padrão
// (StdEncoding, com padding), idêntico ao base64_encode do PHP. Saída: 64 chars —
// bem abaixo do limite de 72 bytes do bcrypt.
func wpPreHash(senha string) string {
	mac := hmac.New(sha512.New384, []byte(wpHMACKey))
	mac.Write([]byte(senha))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// VerifyPassword verifica uma senha em texto puro contra o hash armazenado.
// Função PURA (sem rede/DB) — recebe (hash, senha) e devolve bool. Fail-closed:
// qualquer formato não reconhecido (phpass $P$/$H$, hash vazio, prefixo desconhecido)
// retorna false.
//
// Ramos suportados, na ordem:
//  1. "$wp$"  → WordPress 6.8+: bcrypt(pré-hash HMAC-SHA384) sobre hash[3:].
//  2. "$2a$" / "$2b$" / "$2y$"  → bcrypt puro (contas novas/demo gravadas pelo Go).
//
// NÃO suportado (retorna false): phpass legado "$P$"/"$H$" (MD5 iterado — formato
// pré-WP-6.8). Se aparecer no banco, o usuário precisa redefinir a senha (o WP migra
// o hash no primeiro login bem-sucedido; aqui documentamos como não-suportado em vez
// de implementar MD5 inseguro).
func VerifyPassword(hash, senha string) bool {
	if hash == "" {
		return false // fail-closed: conta sem senha nativa não loga.
	}

	switch {
	case strings.HasPrefix(hash, "$wp$"):
		// Remove SOMENTE "$wp" (3 chars) — o "$" de "$2y$" é do bcrypt. // CRIT-WP-HASH
		bcryptHash := hash[3:]
		pre := wpPreHash(senha)
		return bcrypt.CompareHashAndPassword([]byte(bcryptHash), []byte(pre)) == nil

	case strings.HasPrefix(hash, "$2a$"),
		strings.HasPrefix(hash, "$2b$"),
		strings.HasPrefix(hash, "$2y$"):
		// Bcrypt puro — contas criadas pelo próprio Go (bcrypt.GenerateFromPassword).
		return bcrypt.CompareHashAndPassword([]byte(hash), []byte(senha)) == nil

	default:
		// phpass ($P$/$H$) e qualquer outro prefixo → fail-closed.
		return false
	}
}

// HashPassword gera hash no formato $wp$ (WordPress 6.8+ — idêntico ao ramo "$wp$"
// de VerifyPassword) para que a senha definida pelo reset funcione no login.
func HashPassword(senha string) (string, error) {
	pre := wpPreHash(senha)
	b, err := bcrypt.GenerateFromPassword([]byte(pre), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return "$wp" + string(b), nil
}
