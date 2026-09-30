// Testes unitários dos helpers puros (sem rede/DB) do pacote handlers.
//
// package handlers (mesmo pacote, não _test) → alcança funções não exportadas,
// no mesmo estilo de go/wallet/internal/handlers/recarga_fmt_test.go.
//
// Escopo: apenas o que roda sem Postgres real (parsing, validação de input,
// money helper, anonimização LGPD). Os handlers HTTP exigem *pgxpool.Pool e
// ficam fora deste arquivo de propósito.
package handlers

import (
	"net"
	"strings"
	"testing"

	"github.com/senderzz/portal-service/internal/auth"
)

// TestBlankBuyerNameForAffiliate cobre o gate LGPD do nome do comprador (orders.go,
// Fix #P1): afiliado → nome apagado (""), produtor/operator → nome preservado.
func TestBlankBuyerNameForAffiliate(t *testing.T) {
	if got := blankBuyerNameForAffiliate("João Comprador", true); got != "" {
		t.Errorf("afiliado deve receber nome apagado, got %q", got)
	}
	if got := blankBuyerNameForAffiliate("João Comprador", false); got != "João Comprador" {
		t.Errorf("não-afiliado deve preservar o nome, got %q", got)
	}
	// Já vazio + afiliado → continua vazio (idempotente).
	if got := blankBuyerNameForAffiliate("", true); got != "" {
		t.Errorf("vazio deve continuar vazio, got %q", got)
	}
}

// TestCodWalletKey cobre o fallback de chave da carteira COD (wallet.go, #22):
//   - WPUserID>0  → usa o wp_user_id (não quebra usuários com vínculo WP).
//   - WPUserID<=0 → usa -u.ID (negativo) p/ usuário nativo do portal.
// P0 anti-colisão: o espaço negativo (nativo) NUNCA cruza o espaço positivo (wp ids),
// então um nativo id=63 e um WP user wp_user_id=63 têm chaves distintas (63 vs -63).
func TestCodWalletKey(t *testing.T) {
	// Vínculo WP → chave = wp_user_id.
	if got := codWalletKey(&auth.PortalUser{ID: 12, WPUserID: 990003}); got != 990003 {
		t.Errorf("usuário WP-linkado: chave = %d, esperado 990003", got)
	}
	// Nativo (sem vínculo) → chave = -id portal.
	if got := codWalletKey(&auth.PortalUser{ID: 63, WPUserID: 0}); got != -63 {
		t.Errorf("usuário nativo: chave = %d, esperado -63", got)
	}
	// WPUserID negativo (defensivo) → também nativo.
	if got := codWalletKey(&auth.PortalUser{ID: 50, WPUserID: -1}); got != -50 {
		t.Errorf("WPUserID<0: chave = %d, esperado -50", got)
	}
	// P0 anti-colisão: nativo id=63 (chave -63) ≠ WP user wp_user_id=63 (chave 63).
	native := codWalletKey(&auth.PortalUser{ID: 63, WPUserID: 0})
	wp := codWalletKey(&auth.PortalUser{ID: 12, WPUserID: 63})
	if native == wp {
		t.Errorf("COLISÃO de fundos: nativo(%d) == WP(%d) — devem ser disjuntos", native, wp)
	}
}

// TestDeterministicInviteToken cobre o token FIXO de convite (#19, affiliates_portal.go):
// estável (mesmo produtor → mesmo token), 64-hex (cabe em VARCHAR(64)), e distinto por
// produtor. Usa um WP_SALT_AUTH fixo via t.Setenv p/ determinismo do vetor.
func TestDeterministicInviteToken(t *testing.T) {
	t.Setenv("WP_SALT_AUTH", "vetor-de-teste-salt")
	t.Setenv("JWT_SECRET", "") // garante que o caminho usa WP_SALT_AUTH

	a1 := deterministicInviteToken(42)
	a2 := deterministicInviteToken(42)
	if a1 != a2 {
		t.Errorf("token deve ser estável: %q != %q", a1, a2)
	}
	if len(a1) != 64 {
		t.Errorf("token deve ter 64 chars hex (HMAC-SHA256), got %d", len(a1))
	}
	for _, c := range a1 {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Fatalf("token deve ser hex puro, char inválido %q em %q", c, a1)
		}
	}
	// Distinto por produtor.
	if deterministicInviteToken(42) == deterministicInviteToken(43) {
		t.Error("produtores distintos devem gerar tokens distintos")
	}
}

// TestIsBlockedIP cobre o classificador de IP do guard SSRF (webhooks.go, P0).
// Interno/privado/loopback/link-local/CGNAT → bloqueado; público → permitido.
func TestIsBlockedIP(t *testing.T) {
	t.Setenv("WEBHOOK_ALLOW_LOOPBACK", "") // sem exceção de loopback no teste
	blocked := []string{
		"127.0.0.1",       // loopback
		"10.0.0.1",        // RFC1918
		"192.168.1.1",     // RFC1918
		"172.16.0.1",      // RFC1918
		"169.254.169.254", // link-local / metadata
		"100.64.0.1",      // CGNAT
		"0.0.0.0",         // unspecified
		"::1",             // loopback IPv6
	}
	for _, s := range blocked {
		if !isBlockedIP(net.ParseIP(s)) {
			t.Errorf("isBlockedIP(%s) = false, esperado true (interno)", s)
		}
	}
	allowed := []string{"8.8.8.8", "1.1.1.1", "93.184.216.34"} // públicos
	for _, s := range allowed {
		if isBlockedIP(net.ParseIP(s)) {
			t.Errorf("isBlockedIP(%s) = true, esperado false (público)", s)
		}
	}
	// nil (DNS não resolveu) → bloqueado (fail-closed).
	if !isBlockedIP(nil) {
		t.Error("isBlockedIP(nil) deve ser true (fail-closed)")
	}
}

// TestParseMoney cobre o helper de dinheiro pt-BR/US (wallet.go).
// Nota de comportamento: parseMoney só trata '.' como separador de milhar
// QUANDO há vírgula. "1.234" sem vírgula é lido como 1.234 (US decimal) — por
// isso não asserimos esse caso ambíguo aqui (ver TestParseMoneyDotIsDecimal).
func TestParseMoney(t *testing.T) {
	cases := map[string]float64{
		"10,50":        10.5,    // pt-BR: vírgula decimal
		"1.234,56":     1234.56, // pt-BR: ponto milhar + vírgula decimal
		"1234.56":      1234.56, // US: ponto decimal, sem vírgula
		"0,00":         0,
		"":             0,    // vazio → 0
		"abc":          0,    // não numérico → 0
		"  10,50  ":    10.5, // trim de espaços
		"1.000.000,00": 1000000,
	}
	for in, want := range cases {
		if got := parseMoney(in); got != want {
			t.Errorf("parseMoney(%q) = %v, esperado %v", in, got, want)
		}
	}
}

// TestParseMoneyDotIsDecimal documenta explicitamente que, sem vírgula, o ponto
// é tratado como separador decimal (US), não como milhar.
func TestParseMoneyDotIsDecimal(t *testing.T) {
	if got := parseMoney("1.234"); got != 1.234 {
		t.Errorf("parseMoney(\"1.234\") = %v, esperado 1.234 (ponto = decimal sem vírgula)", got)
	}
}

// TestOnlyDigits cobre o stripper de não-dígitos (wallet.go).
func TestOnlyDigits(t *testing.T) {
	cases := map[string]string{
		"123.456.789-00":  "12345678900",
		"(11) 99999-8888": "11999998888",
		"abc123def456":    "123456",
		"":                "",
		"sem-digitos":     "",
		"+55 41 0000":     "55410000",
	}
	for in, want := range cases {
		if got := onlyDigits(in); got != want {
			t.Errorf("onlyDigits(%q) = %q, esperado %q", in, got, want)
		}
	}
}

// TestValidCPF cobre o validador de CPF (wallet.go).
// Vetores válidos confirmados empiricamente contra a implementação.
func TestValidCPF(t *testing.T) {
	valid := []string{
		"52998224725",
		"11144477735",
		"390.533.447-05", // mascarado — onlyDigits normaliza antes de validar
	}
	for _, c := range valid {
		if !validCPF(c) {
			t.Errorf("validCPF(%q) = false, esperado true", c)
		}
	}

	invalid := []string{
		"00000000000",  // 11 dígitos iguais → rejeitado
		"11111111111",  // idem
		"12345678900",  // DV inconsistente
		"123",          // comprimento != 11
		"",             // vazio
		"5299822472",   // 10 dígitos
		"529982247250", // 12 dígitos
	}
	for _, c := range invalid {
		if validCPF(c) {
			t.Errorf("validCPF(%q) = true, esperado false", c)
		}
	}
}

// TestSanitizeMethodIDs cobre o dedup/filtro de IDs de método de frete (freight.go):
// remove <=0 e duplicatas preservando a ordem de primeira ocorrência.
func TestSanitizeMethodIDs(t *testing.T) {
	type tc struct {
		in   []int
		want []int
	}
	cases := []tc{
		{in: []int{1, 2, 3}, want: []int{1, 2, 3}},
		{in: []int{1, 1, 2, 2, 3}, want: []int{1, 2, 3}},
		{in: []int{0, -5, 4}, want: []int{4}},
		{in: []int{3, 1, 2, 1, 3}, want: []int{3, 1, 2}}, // ordem preservada
		{in: []int{}, want: []int{}},
		{in: []int{0, 0, 0}, want: []int{}},
	}
	for _, c := range cases {
		got := sanitizeMethodIDs(c.in)
		if len(got) != len(c.want) {
			t.Errorf("sanitizeMethodIDs(%v) = %v, esperado %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("sanitizeMethodIDs(%v) = %v, esperado %v", c.in, got, c.want)
				break
			}
		}
	}
}

// TestJSONNumbersToInts cobre a conversão de valores JSON → []int (freight.go).
func TestJSONNumbersToInts(t *testing.T) {
	// JSON decodifica números como float64 por padrão.
	got := jsonNumbersToInts([]any{float64(1), float64(2), float64(3)})
	want := []int{1, 2, 3}
	if len(got) != len(want) {
		t.Fatalf("jsonNumbersToInts = %v, esperado %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("jsonNumbersToInts[%d] = %d, esperado %d", i, got[i], want[i])
		}
	}

	// Entrada não-slice → slice vazio não-nil (nunca nil).
	if out := jsonNumbersToInts("não é slice"); out == nil || len(out) != 0 {
		t.Errorf("jsonNumbersToInts(string) = %v, esperado slice vazio não-nil", out)
	}
}

// TestLGPDAnonEmail cobre o gerador de e-mail neutro da anonimização LGPD
// (settings.go::lgpdAnonEmail — usado por DeleteAccount, AUDIT
// LGPD-no-data-deletion-api).
//
// O e-mail precisa ser único por id (a coluna email é UNIQUE NOT NULL), neutro
// (domínio .local não roteável — nunca vaza o e-mail real do titular) e estável
// (idempotência: re-executar a exclusão não pode colidir com o próprio registro).
func TestLGPDAnonEmail(t *testing.T) {
	cases := map[int64]string{
		1:   "removido+1@lgpd.local",
		7:   "removido+7@lgpd.local",
		999: "removido+999@lgpd.local",
	}
	for id, want := range cases {
		if got := lgpdAnonEmail(id); got != want {
			t.Errorf("lgpdAnonEmail(%d) = %q, esperado %q", id, got, want)
		}
	}

	// Unicidade: ids distintos → e-mails distintos (evita colisão no UNIQUE).
	if lgpdAnonEmail(1) == lgpdAnonEmail(2) {
		t.Error("lgpdAnonEmail deve produzir e-mails distintos para ids distintos")
	}
	// Determinismo/idempotência: mesma entrada → mesma saída.
	if lgpdAnonEmail(42) != lgpdAnonEmail(42) {
		t.Error("lgpdAnonEmail deve ser determinístico para o mesmo id")
	}
	// Não-vazamento: domínio neutro não roteável.
	for id := int64(1); id <= 3; id++ {
		if !strings.HasSuffix(lgpdAnonEmail(id), "@lgpd.local") {
			t.Errorf("lgpdAnonEmail(%d) deve terminar em @lgpd.local (domínio neutro)", id)
		}
	}
}
