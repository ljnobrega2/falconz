// Testes dos helpers PUROS de parse de valor / normalização do webhook PIX.
//
// Lacuna real (auditada): isStatusPago (whitelist) e buildPixEventKey
// (idempotência de referência) JÁ têm cobertura dedicada em pix_status_test.go e
// pix_idempotency_test.go — NÃO duplicamos aqui. O que faltava era o "parse de
// valor": a desserialização decimal.Decimal do payload do webhook e a precedência
// amount→value→price→total→payment.amount (espelha tpc_webhook_extract_amount).
// Exercitamos a struct real pixWebhookPayload via json.Unmarshal (caminho real do
// PostPixWebhook), sem refatorar o handler.
//
// Escopo SEC-WALLET-SEP: helpers tpc_* (expedição/frete) apenas — zero referência
// a sz_cod_wallet_transactions (carteira COD/motoboy é território do portal).
package handlers

import (
	"encoding/json"
	"testing"

	"github.com/shopspring/decimal"
)

// TestPixWebhookPayloadValorParse valida o parse de valor do webhook PIX:
// decimal.Decimal aceita tanto a forma string ("10.50") quanto a numérica (10.5)
// e ambas convergem para o mesmo valor em centavos. É o coração do "parse de
// valor" — um drift aqui faria a validação de divergência (±0.01) comparar contra
// um valor errado.
func TestPixWebhookPayloadValorParse(t *testing.T) {
	cases := []struct {
		nome string
		body string
		want string // StringFixed(2) do campo Amount
	}{
		{"amount string", `{"amount":"10.50"}`, "10.50"},
		{"amount numerico", `{"amount":10.5}`, "10.50"},
		{"amount inteiro", `{"amount":100}`, "100.00"},
		{"amount centavos exatos", `{"amount":"99.99"}`, "99.99"},
		{"amount com milhar numerico", `{"amount":2500.00}`, "2500.00"},
		{"amount ausente → zero", `{"status":"paid"}`, "0.00"},
		// float64 puro perderia precisão em 0.1; decimal preserva.
		{"amount fracao delicada", `{"amount":"0.10"}`, "0.10"},
	}
	for _, c := range cases {
		var p pixWebhookPayload
		if err := json.Unmarshal([]byte(c.body), &p); err != nil {
			t.Fatalf("%s: Unmarshal falhou: %v", c.nome, err)
		}
		if got := p.Amount.StringFixed(2); got != c.want {
			t.Errorf("%s: Amount = %s, esperado %s", c.nome, got, c.want)
		}
	}
}

// TestPixWebhookValorPrecedencia valida a precedência de extração de valor:
// amount→value→price→total→payment.amount, pulando zeros. Espelha exatamente a
// lógica de PostPixWebhook (pix.go) que combina firstNonZeroDecimal com o fallback
// payment.amount. Cobre firstNonZeroDecimal com decimais REAIS desserializados de
// JSON (o teste existente usa só literais construídos), incluindo o caminho aninhado.
func TestPixWebhookValorPrecedencia(t *testing.T) {
	// extrai reproduz a precedência de PostPixWebhook sem tocar o handler:
	// firstNonZeroDecimal(amount,value,price,total) e, se zero, payment.amount.
	extrai := func(p pixWebhookPayload) decimal.Decimal {
		v := firstNonZeroDecimal(p.Amount, p.Value, p.Price, p.Total)
		if v.IsZero() && p.Payment != nil {
			v = p.Payment.Amount
		}
		return v
	}

	cases := []struct {
		nome string
		body string
		want string
	}{
		{"amount vence todos", `{"amount":"10.00","value":"20.00","price":"30.00","total":"40.00"}`, "10.00"},
		{"amount zero → cai em value", `{"amount":"0.00","value":"20.00"}`, "20.00"},
		{"value zero → cai em price", `{"value":0,"price":"30.00"}`, "30.00"},
		{"price zero → cai em total", `{"price":0,"total":"40.00"}`, "40.00"},
		{"todos topo zero → payment.amount", `{"amount":0,"payment":{"amount":"55.55"}}`, "55.55"},
		{"sem nenhum valor → zero", `{"status":"paid"}`, "0.00"},
		// payment.amount NÃO é usado se um valor de topo é não-zero.
		{"topo não-zero ignora payment.amount", `{"value":"7.00","payment":{"amount":"99.00"}}`, "7.00"},
	}
	for _, c := range cases {
		var p pixWebhookPayload
		if err := json.Unmarshal([]byte(c.body), &p); err != nil {
			t.Fatalf("%s: Unmarshal falhou: %v", c.nome, err)
		}
		if got := extrai(p).StringFixed(2); got != c.want {
			t.Errorf("%s: valor extraído = %s, esperado %s", c.nome, got, c.want)
		}
	}
}

// TestRemoveAccents valida a remoção de acentos usada no matching de status PIX
// (isStatusAnalise). Hoje só é exercitada indiretamente; aqui ancoramos os termos
// que precisam bater com a whitelist sem acento ("análise"→"analise" etc.).
func TestRemoveAccents(t *testing.T) {
	cases := map[string]string{
		"análise":            "analise",
		"em análise":         "em analise",
		"concluído":          "concluido",
		"transação":          "transacao",
		"ÁÉÍÓÚÇ":             "AEIOUC",
		"sem acento":         "sem acento",
		"aguardando análise": "aguardando analise",
		"":                   "",
	}
	for in, want := range cases {
		if got := removeAccents(in); got != want {
			t.Errorf("removeAccents(%q) = %q, esperado %q", in, got, want)
		}
	}
}

// TestSha256Hex valida o hash usado no payload_hash e no fallback de chave de
// idempotência: hex minúsculo de 64 chars, determinístico, e sensível a 1 byte.
func TestSha256Hex(t *testing.T) {
	h := sha256hex([]byte("payload-pix"))
	if len(h) != 64 {
		t.Errorf("comprimento do hash = %d, esperado 64", len(h))
	}
	if sha256hex([]byte("payload-pix")) != h {
		t.Error("sha256hex não-determinístico para a mesma entrada")
	}
	if sha256hex([]byte("payload-piy")) == h {
		t.Error("sha256hex colidiu para entradas diferentes (1 byte)")
	}
	// Vetor conhecido: SHA-256 de string vazia.
	if got := sha256hex(nil); got != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Errorf("sha256hex(nil) = %q, esperado o hash da string vazia", got)
	}
}

// TestSafePrefix valida o truncador de logs (nunca logar assinatura completa):
// adiciona "..." quando trunca, e devolve a string intacta quando cabe.
func TestSafePrefix(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"sha256=abcdef0123456789", 16, "sha256=abcdef012..."},
		{"curto", 16, "curto"},
		{"exato", 5, "exato"},   // len == n → sem reticências
		{"exatos", 5, "exato..."}, // len > n → trunca
		{"", 8, ""},
	}
	for _, c := range cases {
		if got := safePrefix(c.in, c.n); got != c.want {
			t.Errorf("safePrefix(%q, %d) = %q, esperado %q", c.in, c.n, got, c.want)
		}
	}
}

// TestExpiresEpochToISO valida a conversão do epoch de expires_at para ISO-8601
// UTC exibido no GET /recarga/{id}/pix: nil/<=0 → "" (sem data), positivo → string
// formatada. Helper puro hoje sem teste direto.
func TestExpiresEpochToISO(t *testing.T) {
	if got := expiresEpochToISO(nil); got != "" {
		t.Errorf("expiresEpochToISO(nil) = %q, esperado \"\"", got)
	}
	zero := int64(0)
	if got := expiresEpochToISO(&zero); got != "" {
		t.Errorf("expiresEpochToISO(0) = %q, esperado \"\"", got)
	}
	neg := int64(-5)
	if got := expiresEpochToISO(&neg); got != "" {
		t.Errorf("expiresEpochToISO(-5) = %q, esperado \"\" (epoch inválido)", got)
	}
	// 2021-01-01 00:00:00 UTC = 1609459200.
	ts := int64(1609459200)
	if got := expiresEpochToISO(&ts); got != "2021-01-01 00:00:00" {
		t.Errorf("expiresEpochToISO(1609459200) = %q, esperado '2021-01-01 00:00:00'", got)
	}
}
