package handlers

// AUDIT TEST-HANDLERS-ZERO-COVERAGE: a recomendação lista alvos em
// motoboy/orders/portal/affiliates/admin — fora do escopo go/wallet. Dentro do
// wallet, a lacuna real são os helpers de invariante financeira sem teste:
//   - buildPixEventKey  → chave de idempotência do webhook PIX (S8 / uq_event_key)
//   - firstNonZeroDecimal → seleção de valor recalculado server-side
//   - pixSecurityToken / buildPixRedirectURL → auth HMAC do retorno PIX
// Os ledger ops DB-bound (PostReservar/Debitar/Creditar/Liberar) exigem um
// harness Postgres que este pacote não possui — testá-los aqui seria frágil
// (integração contra infra ausente). Cobrimos os puros, que são determinísticos.

import (
	"os"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
)

// TestBuildPixEventKey valida a prioridade da chave de idempotência (S8):
// me_pix_id > recarga_id > hash do body. Mesma entrada → mesma chave (idempotente).
func TestBuildPixEventKey(t *testing.T) {
	// 1. me_pix_id presente tem prioridade absoluta.
	if got := buildPixEventKey("ME-123", 99, []byte(`{"x":1}`)); got != "pix:ME-123" {
		t.Errorf("com me_pix_id: got %q, esperado 'pix:ME-123'", got)
	}

	// 2. sem me_pix_id mas com recarga_id → "pix:recarga:<id>".
	if got := buildPixEventKey("", 42, []byte(`{"x":1}`)); got != "pix:recarga:42" {
		t.Errorf("com recarga_id: got %q, esperado 'pix:recarga:42'", got)
	}

	// 3. sem identificadores → hash do body, prefixo "pix:", 48 chars de hash.
	body := []byte(`{"notificacao":"sem id"}`)
	got := buildPixEventKey("", 0, body)
	if !strings.HasPrefix(got, "pix:") {
		t.Fatalf("fallback de hash: got %q, esperado prefixo 'pix:'", got)
	}
	hashPart := strings.TrimPrefix(got, "pix:")
	if len(hashPart) != 48 {
		t.Errorf("fallback de hash: comprimento %d, esperado 48", len(hashPart))
	}

	// 4. Idempotência: mesmo body → mesma chave; body diferente → chave diferente.
	if buildPixEventKey("", 0, body) != got {
		t.Error("fallback de hash não é determinístico (mesma entrada → chaves diferentes)")
	}
	if buildPixEventKey("", 0, []byte(`{"notificacao":"OUTRO"}`)) == got {
		t.Error("fallback de hash colidiu para bodies diferentes")
	}
}

// TestFirstNonZeroDecimal valida a seleção do primeiro valor não-zero, usada no
// recálculo server-side de valor (nunca confiar num único valor do cliente).
func TestFirstNonZeroDecimal(t *testing.T) {
	d := func(s string) decimal.Decimal { return decimal.RequireFromString(s) }

	cases := []struct {
		nome string
		in   []decimal.Decimal
		want string
	}{
		{"primeiro não-zero", []decimal.Decimal{d("10.50"), d("20.00")}, "10.5"},
		{"pula zeros iniciais", []decimal.Decimal{decimal.Zero, d("0"), d("7.25")}, "7.25"},
		{"todos zero → zero", []decimal.Decimal{decimal.Zero, d("0")}, "0"},
		{"vazio → zero", []decimal.Decimal{}, "0"},
	}
	for _, c := range cases {
		if got := firstNonZeroDecimal(c.in...); !got.Equal(d(c.want)) {
			t.Errorf("%s: got %s, esperado %s", c.nome, got, c.want)
		}
	}
}

// TestPixSecurityToken valida que o token de retorno PIX é HMAC-SHA256 truncado
// a 32 chars, determinístico por recarga_id e ligado ao JWT_SECRET (fail-closed:
// segredo diferente → token diferente).
func TestPixSecurityToken(t *testing.T) {
	t.Setenv("JWT_SECRET", "segredo-de-teste-A")

	tok := pixSecurityToken(123)
	if len(tok) != 32 {
		t.Errorf("comprimento do token = %d, esperado 32", len(tok))
	}
	// Determinístico para o mesmo id+segredo.
	if pixSecurityToken(123) != tok {
		t.Error("token não-determinístico para mesmo recarga_id e segredo")
	}
	// recarga_id diferente → token diferente.
	if pixSecurityToken(124) == tok {
		t.Error("ids diferentes produziram o mesmo token")
	}
	// Segredo diferente → token diferente (não forjável sem o JWT_SECRET).
	t.Setenv("JWT_SECRET", "segredo-de-teste-B")
	if pixSecurityToken(123) == tok {
		t.Error("segredos diferentes produziram o mesmo token — HMAC não está ligado ao JWT_SECRET")
	}
}

// TestBuildPixRedirectURL valida a montagem da redirect_url: base absoluta
// quando WP_REST_BASE_URL está setada (exigida pela API ME), fallback relativo
// só em dev, e que recarga_id + sz_token entram na query.
func TestBuildPixRedirectURL(t *testing.T) {
	// Base absoluta configurada (produção/staging) — trailing slash deve ser removido.
	t.Setenv("WP_REST_BASE_URL", "https://app.senderzz.com.br/wp-json/")
	got := buildPixRedirectURL(55, "abcdef")
	want := "https://app.senderzz.com.br/wp-json/tp-carteira/v1/pix/retorno?recarga_id=55&sz_token=abcdef"
	if got != want {
		t.Errorf("URL absoluta:\n  got  %q\n  want %q", got, want)
	}

	// Sem base configurada → fallback relativo /wp-json (apenas dev).
	os.Unsetenv("WP_REST_BASE_URL")
	got = buildPixRedirectURL(55, "abcdef")
	want = "/wp-json/tp-carteira/v1/pix/retorno?recarga_id=55&sz_token=abcdef"
	if got != want {
		t.Errorf("URL fallback:\n  got  %q\n  want %q", got, want)
	}
}
