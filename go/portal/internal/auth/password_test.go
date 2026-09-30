// Testes de VerifyPassword (CRIT-WP-HASH).
//
// Os vetores "$wp$" e o pré-hash são KNOWN-ANSWER, gerados por um oráculo EXTERNO
// (não pelo próprio código Go que verificamos — senão um HMAC com chave errada
// passaria silenciosamente). Oráculos usados:
//
//	# hash WP 6.8 com senha conhecida "Senha12345":
//	php -r '$pw="Senha12345";
//	  echo "$wp".password_hash(base64_encode(hash_hmac("sha384",$pw,"wp-sha384",true)),
//	       PASSWORD_BCRYPT,["cost"=>10]);'
//	→ $wp$2y$10$jYAoS0AS/UrfNzu1TdGp1eILkfiSszzZvvYMPR36/1dn2Yn/K9Vrq
//
//	# pré-hash isolado (HMAC-SHA384 + base64) da mesma senha:
//	printf '%s' Senha12345 | openssl dgst -sha384 -hmac wp-sha384 -binary | base64
//	→ rqNSYcIw452f198nETz/rD5EiN+ub1cCFWJlXc/z7zG7j7C/xRmNH217KumHbR9q
package auth

import "testing"

const (
	// Vetor WP 6.8 ($wp$2y$10$...) — 63 chars, senha "Senha12345". Externo (php).
	wpHashVector = "$wp$2y$10$jYAoS0AS/UrfNzu1TdGp1eILkfiSszzZvvYMPR36/1dn2Yn/K9Vrq"
	wpVectorPwd  = "Senha12345"

	// Pré-hash esperado da senha acima — externo (openssl). Trava a chave/algoritmo.
	wpPreHashExpected = "rqNSYcIw452f198nETz/rD5EiN+ub1cCFWJlXc/z7zG7j7C/xRmNH217KumHbR9q"

	// Bcrypt puro ($2y$) da senha "Senha12345" — externo (php password_hash).
	pureBcryptVector = "$2y$12$yBcdf7fPUFUPrql.KC5dPu8bqJZbdbWpPzg4.fzyYiHq4fBGGYTOy"
	pureBcryptPwd    = "Senha12345" // hash de "Senha12345" (mesmo plaintext, cost 12)
)

// TestWPPreHash trava o pré-hash HMAC-SHA384+base64 contra o oráculo openssl.
// Se a chave "wp-sha384", o algoritmo SHA-384 ou o alfabeto base64 mudarem, quebra.
func TestWPPreHash(t *testing.T) {
	if got := wpPreHash(wpVectorPwd); got != wpPreHashExpected {
		t.Fatalf("wpPreHash(%q) = %q\n esperado (openssl) %q", wpVectorPwd, got, wpPreHashExpected)
	}
}

// TestVerifyPassword_WPFormat cobre o ramo "$wp$" — senha certa casa, errada não.
func TestVerifyPassword_WPFormat(t *testing.T) {
	if !VerifyPassword(wpHashVector, wpVectorPwd) {
		t.Error("VerifyPassword: hash $wp$ com senha correta deveria casar (true)")
	}
	if VerifyPassword(wpHashVector, "senhaErrada") {
		t.Error("VerifyPassword: hash $wp$ com senha errada NÃO deve casar (false)")
	}
	if VerifyPassword(wpHashVector, "") {
		t.Error("VerifyPassword: hash $wp$ com senha vazia deve falhar (false)")
	}
}

// TestVerifyPassword_PureBcrypt cobre o fallback bcrypt puro ($2y$).
func TestVerifyPassword_PureBcrypt(t *testing.T) {
	if !VerifyPassword(pureBcryptVector, pureBcryptPwd) {
		t.Error("VerifyPassword: bcrypt puro com senha correta deveria casar (true)")
	}
	if VerifyPassword(pureBcryptVector, "errada") {
		t.Error("VerifyPassword: bcrypt puro com senha errada NÃO deve casar (false)")
	}
}

// TestVerifyPassword_FailClosed cobre os casos de fail-closed: hash vazio, phpass
// legado ($P$/$H$, não-suportado) e prefixo desconhecido → sempre false.
func TestVerifyPassword_FailClosed(t *testing.T) {
	cases := []struct {
		name string
		hash string
		pwd  string
	}{
		{"hash vazio", "", "qualquer"},
		{"phpass $P$ (não-suportado)", "$P$Bxxxxxxxxxxxxxxxxxxxxxxxxxxxxx0", "senha"},
		{"phpass $H$ (não-suportado)", "$H$9xxxxxxxxxxxxxxxxxxxxxxxxxxxxx0", "senha"},
		{"prefixo desconhecido", "plaintext-sem-prefixo", "plaintext-sem-prefixo"},
		{"md5 cru", "5f4dcc3b5aa765d61d8327deb882cf99", "password"},
	}
	for _, c := range cases {
		if VerifyPassword(c.hash, c.pwd) {
			t.Errorf("VerifyPassword(%s) = true, esperado false (fail-closed)", c.name)
		}
	}
}
