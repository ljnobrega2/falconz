package wphash

import "testing"

// Vetores gerados pelo próprio WordPress/PHP (phpass PasswordHash, password_hash
// bcrypt, e wp_hash_password $wp$ do core 6.8 — base64(hmac_sha384(senha,'wp-sha384'))).
// Senha de referência: "Senderzz#2026". Garante paridade FIEL com wp_check_password.
func TestCheckPassword_WordPressVectors(t *testing.T) {
	const pw = "Senderzz#2026"
	cases := []struct {
		name, password, hash string
		want                 bool
	}{
		// phpass portable ($P$) — formato padrão histórico do WP.
		{"phpass_ok", pw, "$P$BDJ9hSX5cIivYOhP2zlsv5bhLYkO9m/", true},
		{"phpass_wrong", "wrongpass", "$P$BDJ9hSX5cIivYOhP2zlsv5bhLYkO9m/", false},
		// bcrypt puro ($2y$) — usado pelo schema Go nativo (portal_users.password_hash).
		{"bcrypt_ok", pw, "$2y$12$rMmidaaehFSTPBkklhnsROl7JolXqYirZhuV6M1JSFx5qiG47n7XS", true},
		{"bcrypt_wrong", "x", "$2y$12$rMmidaaehFSTPBkklhnsROl7JolXqYirZhuV6M1JSFx5qiG47n7XS", false},
		// $wp$ (WP 6.8) — bcrypt sobre base64(hmac_sha384(senha,'wp-sha384')).
		{"wp68_ok", pw, "$wp$$2y$12$LTOhzBQkv1QQxyGSYKadauPqlX7qYt27PNqXVnZsMS259jQrZgz5K", true},
		{"wp68_wrong", "x", "$wp$$2y$12$LTOhzBQkv1QQxyGSYKadauPqlX7qYt27PNqXVnZsMS259jQrZgz5K", false},
		// md5 legado (32 chars) — o WP ainda aceita por compatibilidade.
		{"md5_ok", pw, "e28d39abda5383e9ff0c9567c4a97100", true},
		{"md5_wrong", "x", "e28d39abda5383e9ff0c9567c4a97100", false},
		// fail-closed: hash ou senha vazios nunca autenticam.
		{"empty_hash", pw, "", false},
		{"empty_pw", "", "$P$BDJ9hSX5cIivYOhP2zlsv5bhLYkO9m/", false},
		// formato desconhecido → false.
		{"garbage", pw, "not-a-hash", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CheckPassword(c.password, c.hash); got != c.want {
				t.Fatalf("CheckPassword(%q, %q) = %v, want %v", c.password, c.hash, got, c.want)
			}
		})
	}
}
