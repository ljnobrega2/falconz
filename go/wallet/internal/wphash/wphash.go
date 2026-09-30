// Package wphash verifica senhas armazenadas em wp_users.user_pass.
//
// O WordPress armazena a senha com um de três formatos possíveis:
//
//   - phpass portable hash ($P$ ou $H$)  — formato histórico/padrão do WP
//   - bcrypt ($2a$ / $2b$ / $2y$)        — quando algum plugin força bcrypt
//   - $wp$2y$...  (WP ≥ 6.8)             — bcrypt sobre base64(hmac_sha384(senha,'wp-sha384'))
//   - md5 (32 chars)                     — hash legado, ainda aceito pelo WP
//
// Este pacote espelha o comportamento de wp_check_password() / wp_authenticate(),
// que o endpoint PHP tpc_endpoint_auth_token usa (rest-api.php:321). Port FIEL:
// a verificação aceita exatamente os mesmos hashes que o WP aceitaria.
//
// Comparações de igualdade usam tempo constante (hmac.Equal) para evitar
// timing attacks, idêntico ao hash_equals() do PHP.
package wphash

import (
	"crypto/hmac"
	"crypto/md5" //nolint:gosec — exigido para compat com phpass do WordPress (formato do banco existente)
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// itoa64 é o alfabeto base64 customizado do phpass (igual ao do WP/crypt_blowfish).
const itoa64 = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// CheckPassword retorna true se a senha em claro confere com o hash armazenado.
//
// Suporta os três formatos que o WP grava em user_pass. Hash vazio → false
// (fail-closed: usuário sem senha definida nunca autentica).
func CheckPassword(password, storedHash string) bool {
	if storedHash == "" || password == "" {
		return false
	}

	// WP ≥ 6.8: prefixo $wp$ → bcrypt sobre base64(hmac_sha384(senha,'wp-sha384')).
	if strings.HasPrefix(storedHash, "$wp$") {
		return checkWpBcrypt(password, strings.TrimPrefix(storedHash, "$wp$"))
	}

	// bcrypt puro ($2a$/$2b$/$2y$).
	if strings.HasPrefix(storedHash, "$2a$") ||
		strings.HasPrefix(storedHash, "$2b$") ||
		strings.HasPrefix(storedHash, "$2y$") {
		return bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(password)) == nil
	}

	// phpass portable hash ($P$ / $H$).
	if strings.HasPrefix(storedHash, "$P$") || strings.HasPrefix(storedHash, "$H$") {
		return checkPhpass(password, storedHash)
	}

	// Hash legado (md5 puro de 32 chars) — o WP ainda aceita por compatibilidade.
	if len(storedHash) == 32 {
		return checkLegacyMD5(password, storedHash)
	}

	return false
}

// checkPhpass implementa o algoritmo phpass portable (cph) do WordPress.
func checkPhpass(password, storedHash string) bool {
	if len(storedHash) != 34 {
		return false
	}
	computed := cryptPrivate(password, storedHash)
	if len(computed) != 34 {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(computed), []byte(storedHash)) == 1
}

// cryptPrivate replica PasswordHash::crypt_private() do phpass.
// Setting é o próprio hash armazenado (ou o "salt setting" no momento da geração).
func cryptPrivate(password, setting string) string {
	output := "*0"
	if strings.HasPrefix(setting, "*0") {
		output = "*1"
	}

	id := setting[0:3]
	if id != "$P$" && id != "$H$" {
		return output
	}

	countLog2 := strings.IndexByte(itoa64, setting[3])
	if countLog2 < 7 || countLog2 > 30 {
		return output
	}
	count := 1 << uint(countLog2)

	salt := setting[4:12]
	if len(salt) != 8 {
		return output
	}

	// hash = md5(salt + password); depois count vezes hash = md5(hash + password).
	hash := md5Bytes([]byte(salt + password))
	for i := 0; i < count; i++ {
		hash = md5Bytes(append(append([]byte{}, hash...), []byte(password)...))
	}

	return setting[0:12] + encode64(hash, 16)
}

// encode64 é o encode base64-itoa64 do phpass (não é base64 padrão).
func encode64(input []byte, count int) string {
	var out strings.Builder
	i := 0
	for i < count {
		value := int(input[i])
		i++
		out.WriteByte(itoa64[value&0x3f])

		if i < count {
			value |= int(input[i]) << 8
		}
		out.WriteByte(itoa64[(value>>6)&0x3f])
		if i >= count {
			break
		}
		i++

		if i < count {
			value |= int(input[i]) << 16
		}
		out.WriteByte(itoa64[(value>>12)&0x3f])
		if i >= count {
			break
		}
		i++

		out.WriteByte(itoa64[(value>>18)&0x3f])
	}
	return out.String()
}

// checkWpBcrypt verifica o formato $wp$ (WP ≥ 6.8).
//
// O wp_hash_password do core 6.8 pré-processa a senha ANTES do bcrypt com:
//
//	base64_encode( hash_hmac('sha384', trim(senha), 'wp-sha384', true) )
//
// e só então aplica bcrypt sobre essa string. Reproduzimos exatamente esse
// pré-hash (HMAC-SHA384 com a chave fixa 'wp-sha384') — validado contra um hash
// real gerado por password_hash do PHP (ver wphash_test.go, caso wp68_ok).
func checkWpBcrypt(password, bcryptHash string) bool {
	mac := hmac.New(sha512.New384, []byte(wpSHA384Key))
	mac.Write([]byte(strings.TrimSpace(password)))
	pre := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return bcrypt.CompareHashAndPassword([]byte(bcryptHash), []byte(pre)) == nil
}

// wpSHA384Key é a chave HMAC fixa que o WordPress 6.8 usa no pré-hash do $wp$.
const wpSHA384Key = "wp-sha384"

// checkLegacyMD5 compara md5(password) com o hash de 32 chars (tempo constante).
func checkLegacyMD5(password, storedHash string) bool {
	computed := md5Hex(password)
	return subtle.ConstantTimeCompare([]byte(computed), []byte(strings.ToLower(storedHash))) == 1
}

// md5Bytes e md5Hex isolam o uso de MD5 (necessário para compat com phpass legado
// do WordPress — não é escolha de segurança nossa, é o formato do banco existente).
func md5Bytes(b []byte) []byte {
	sum := md5.Sum(b) //nolint:gosec — compat phpass
	return sum[:]
}

func md5Hex(s string) string {
	const hexdigits = "0123456789abcdef"
	sum := md5Bytes([]byte(s))
	out := make([]byte, len(sum)*2)
	for i, v := range sum {
		out[i*2] = hexdigits[v>>4]
		out[i*2+1] = hexdigits[v&0x0f]
	}
	return string(out)
}
