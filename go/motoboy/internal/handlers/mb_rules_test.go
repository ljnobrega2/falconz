// Testes PUROS dos helpers de regra de negócio portados FIEL do WP (mb_rules.go).
// White-box (package handlers) para alcançar funções não-exportadas. Sem I/O de
// banco: cobrem apenas as funções puras (validação/parse/normalização) — as que
// dependem de banco (validarSKUPedido, taxaFrustrado, optionFloat) NÃO são
// exercitadas aqui, só suas sub-partes puras (normalizarSKU, round2).
//
// FEAT-SKU e QA-FIX permanecem intocados: estes testes só LEEM o comportamento
// das funções puras, caracterizando o contrato atual (não alteram regra alguma).
package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"testing"
)

// ── packageCode ↔ parsePackageCode (QR de etiqueta — auth que gateia a rota) ──

// expectedPackageCode reproduz a fórmula de packageCode() de forma INDEPENDENTE,
// servindo de oráculo: se a implementação mudar a derivação, o teste quebra.
// seed = "pedido|wc" (pedido PRIMEIRO, pipe); sig = HMAC-SHA256[:14] em MAIÚSCULO;
// string final = "SZ-{wc}-{pedido}-{SIG}" (wc PRIMEIRO, hífen).
func expectedPackageCode(pedidoID, wcOrderID int64, salt string) string {
	mac := hmac.New(sha256.New, []byte(salt))
	fmt.Fprintf(mac, "%d|%d", pedidoID, wcOrderID)
	sig := strings.ToUpper(hex.EncodeToString(mac.Sum(nil))[:14])
	return fmt.Sprintf("SZ-%d-%d-%s", wcOrderID, pedidoID, sig)
}

// TestPackageCodeFormato: o código gerado casa o oráculo (seed pipe, sig 14 hex
// MAIÚSCULO, ordem wc-pedido na string). É a assinatura que a rota.go regenera e
// compara — qualquer divergência quebraria o "bipar QR" de TODO motoboy.
func TestPackageCodeFormato(t *testing.T) {
	const salt = "salt-de-teste-32-bytes-ou-mais-xxxxx"
	const ped, wc int64 = 777, 12345

	got := packageCode(ped, wc, salt)
	want := expectedPackageCode(ped, wc, salt)
	if got != want {
		t.Fatalf("packageCode(%d,%d)=%q, esperava %q", ped, wc, got, want)
	}
	if !strings.HasPrefix(got, fmt.Sprintf("SZ-%d-%d-", wc, ped)) {
		t.Fatalf("string final deve ter wc PRIMEIRO: %q", got)
	}
	// A assinatura (último segmento) tem exatamente 14 hex MAIÚSCULOS.
	seg := strings.Split(got, "-")
	sig := seg[len(seg)-1]
	if len(sig) != 14 || sig != strings.ToUpper(sig) {
		t.Fatalf("sig deve ter 14 hex MAIÚSCULOS, veio %q (len=%d)", sig, len(sig))
	}
}

// TestPackageCodeRoundTrip: gerar → parsear recupera EXATAMENTE pedido_id e
// wc_order_id (a amarração que rota.go usa para abrir o pedido certo).
func TestPackageCodeRoundTrip(t *testing.T) {
	const salt = "outro-salt-de-teste-xxxxxxxxxxxxxxxx"
	casos := []struct{ ped, wc int64 }{
		{1, 1}, {777, 12345}, {999999, 88}, {42, 1000000},
	}
	for _, c := range casos {
		code := packageCode(c.ped, c.wc, salt)
		p, ok := parsePackageCode(code)
		if !ok {
			t.Fatalf("parsePackageCode(%q) falhou ao casar o padrão", code)
		}
		if p.PedidoID != c.ped || p.WCOrderID != c.wc {
			t.Fatalf("round-trip perdeu ids: gerou (ped=%d,wc=%d), parseou (ped=%d,wc=%d)",
				c.ped, c.wc, p.PedidoID, p.WCOrderID)
		}
	}
}

// TestParsePackageCodeNormalizaEAceita: aceita minúsculo + espaços (uppercase +
// trim antes do regex), mantendo o Code normalizado para a comparação EqualFold
// que rota.go faz contra a assinatura regenerada.
func TestParsePackageCodeNormaliza(t *testing.T) {
	const salt = "salt-norm-xxxxxxxxxxxxxxxxxxxxxxxxxx"
	code := packageCode(55, 9001, salt) // já MAIÚSCULO por construção
	variantes := []string{
		strings.ToLower(code), // tudo minúsculo
		"  " + code + "\n",    // com espaços/newline
		"\t" + strings.ToLower(code) + "  ",
	}
	for _, v := range variantes {
		p, ok := parsePackageCode(v)
		if !ok || p.PedidoID != 55 || p.WCOrderID != 9001 {
			t.Fatalf("variante normalizável %q deveria casar (ok=%v, ped=%d, wc=%d)",
				v, ok, p.PedidoID, p.WCOrderID)
		}
		if p.Code != strings.ToUpper(strings.TrimSpace(v)) {
			t.Fatalf("Code deveria ser normalizado (upper+trim): %q", p.Code)
		}
	}
}

// TestParsePackageCodeInvalido: strings sem o padrão SZ-…-…-… retornam ok=false.
func TestParsePackageCodeInvalido(t *testing.T) {
	invalidos := []string{
		"", "  ", "SZ-1-2", "SZ-1-2-XYZ", // sig não-hex
		"PREFIXO-1-2-AABBCC", // sem SZ-
		"SZ-A-2-AABBCCDD",    // wc não-numérico
		"qualquer-coisa",
		"SZ-1-2-AABBCC", // sig com 6 hex (< 8 exigidos)
	}
	for _, s := range invalidos {
		if _, ok := parsePackageCode(s); ok {
			t.Errorf("parsePackageCode(%q) deveria ser inválido", s)
		}
	}
}

// TestPackageCodeTamperRejeitado: alterar UM caractere da assinatura faz o código
// adulterado NÃO bater com a assinatura regenerada (EqualFold), espelhando o
// gate anti-falsificação de QR de rota.go. Também: salt diferente → sig diferente.
func TestPackageCodeTamperRejeitado(t *testing.T) {
	const salt = "salt-anti-tamper-xxxxxxxxxxxxxxxxxxx"
	const ped, wc int64 = 321, 7654
	legit := packageCode(ped, wc, salt)

	// Adultera o último caractere da assinatura (mantendo hex).
	runes := []rune(legit)
	last := len(runes) - 1
	if runes[last] == 'A' {
		runes[last] = 'B'
	} else {
		runes[last] = 'A'
	}
	tampered := string(runes)

	// O parse ainda extrai os ids (o regex casa), mas a assinatura regenerada
	// NÃO é igual à adulterada — é exatamente o que rota.go bloqueia.
	if strings.EqualFold(legit, tampered) {
		t.Fatal("código adulterado não deveria ser EqualFold do legítimo")
	}
	regen := packageCode(ped, wc, salt)
	if strings.EqualFold(regen, tampered) {
		t.Fatal("assinatura regenerada não pode bater com o QR adulterado (anti-falsificação)")
	}
	// E um QR gerado com OUTRO salt não confere com a regeneração do salt real.
	outro := packageCode(ped, wc, "salt-diferente-do-servidor-xxxxxxxx")
	if strings.EqualFold(regen, outro) {
		t.Fatal("QR de outro salt não pode conferir com a assinatura do servidor")
	}
}

// ── validarCPF (port FIEL do algoritmo de dígitos verificadores) ─────────────

func TestValidarCPF(t *testing.T) {
	validos := []string{
		"529.982.247-25", // CPF válido clássico (com máscara)
		"52998224725",    // mesmo, sem máscara
		"111.444.777-35", // válido conhecido
	}
	for _, c := range validos {
		if !validarCPF(c) {
			t.Errorf("validarCPF(%q) deveria ser true", c)
		}
	}

	invalidos := []string{
		"",                // vazio
		"123",             // curto
		"00000000000",     // todos iguais
		"11111111111",     // todos iguais
		"52998224724",     // último dígito errado
		"529.982.247-2X",  // não-dígito vira 10 dígitos → rejeitado
		"123456789012345", // longo demais
	}
	for _, c := range invalidos {
		if validarCPF(c) {
			t.Errorf("validarCPF(%q) deveria ser false", c)
		}
	}
}

// ── validarGPSOperacional (limites FIEL do PHP) ──────────────────────────────

func TestValidarGPSOperacional(t *testing.T) {
	acc := func(f float64) *float64 { return &f }

	// Coordenada válida em São Paulo, sem aferição de precisão.
	if ok, msg := validarGPSOperacional(-23.5505, -46.6333, nil); !ok {
		t.Errorf("SP válida deveria passar; msg=%q", msg)
	}
	// Precisão boa (<=150) passa.
	if ok, _ := validarGPSOperacional(-23.5505, -46.6333, acc(30)); !ok {
		t.Error("precisão 30m deveria passar")
	}

	// lat/lng ~zero (não geocodado) → inválido.
	if ok, _ := validarGPSOperacional(0, 0, nil); ok {
		t.Error("(0,0) deveria ser inválido (abs < 1e-6)")
	}
	// Fora do intervalo geográfico.
	if ok, _ := validarGPSOperacional(91, -46.6, nil); ok {
		t.Error("lat 91 (> 90) deveria ser inválido")
	}
	if ok, _ := validarGPSOperacional(-23.5, 181, nil); ok {
		t.Error("lng 181 (> 180) deveria ser inválido")
	}
	// Precisão insuficiente (> 150) → inválido.
	if ok, msg := validarGPSOperacional(-23.5505, -46.6333, acc(200)); ok || msg == "" {
		t.Errorf("precisão 200m deveria reprovar com mensagem; ok=%v msg=%q", ok, msg)
	}
}

// ── validarFotoBase64 (data-URI + tamanho min/max) ───────────────────────────

// makeFotoDataURI monta um data:image/png;base64,… com nBytes decodificados.
func makeFotoDataURI(nBytes int) string {
	raw := make([]byte, nBytes)
	for i := range raw {
		raw[i] = byte(i % 251)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw)
}

func TestValidarFotoBase64(t *testing.T) {
	// Válida: data-URI png com payload >= 3000 bytes decodificados.
	if ok, msg := validarFotoBase64(makeFotoDataURI(4000)); !ok {
		t.Errorf("foto png 4000B deveria ser válida; msg=%q", msg)
	}
	// JPEG também aceito.
	jpg := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(make([]byte, 4000))
	if ok, _ := validarFotoBase64(jpg); !ok {
		t.Error("foto jpeg 4000B deveria ser válida")
	}

	// Vazia → reprovada.
	if ok, _ := validarFotoBase64("   "); ok {
		t.Error("foto vazia deveria reprovar")
	}
	// Sem prefixo data:image → reprovada (protege contra payload não-imagem).
	if ok, _ := validarFotoBase64(base64.StdEncoding.EncodeToString(make([]byte, 4000))); ok {
		t.Error("base64 sem data-URI deveria reprovar")
	}
	// Formato não suportado (gif) → reprovado.
	gif := "data:image/gif;base64," + base64.StdEncoding.EncodeToString(make([]byte, 4000))
	if ok, _ := validarFotoBase64(gif); ok {
		t.Error("data:image/gif não está na allowlist → deveria reprovar")
	}
	// Pequena demais (< 3000 bytes decodificados) → reprovada.
	if ok, _ := validarFotoBase64(makeFotoDataURI(100)); ok {
		t.Error("foto < 3000B deveria reprovar")
	}
	// Grande demais (> 8 MiB) → reprovada.
	if ok, _ := validarFotoBase64(makeFotoDataURI(8*1024*1024 + 10)); ok {
		t.Error("foto > 8MiB deveria reprovar")
	}
}

// ── parseMoney / round2 (dinheiro — COD do motoboy) ──────────────────────────

func TestParseMoney(t *testing.T) {
	eps := 1e-9
	casos := []struct {
		in   any
		want float64
	}{
		{float64(10.5), 10.5},
		{int(7), 7},
		{int64(123), 123},
		{nil, 0},
		{-5.0, 0},                // negativo → max(0, …)
		{"R$ 1.234,56", 1234.56}, // string BR: remove R$/espaço/ponto-milhar, vírgula→ponto
		{"99,90", 99.90},
		{"1.000,00", 1000.00},
		{"", 0},
		{"lixo", 0}, // não-parseável → 0
	}
	for _, c := range casos {
		got := parseMoney(c.in)
		if math.Abs(got-c.want) > eps {
			t.Errorf("parseMoney(%v)=%.4f, esperava %.4f", c.in, got, c.want)
		}
	}
}

// TestRound2 CARACTERIZA o comportamento atual de round2 (int64(f*100+0.5)/100).
// Não posso alterar a regra de negócio, então fixo o que a função REALMENTE faz,
// inclusive os artefatos de representação binária em fronteiras .xx5 (ex.:
// 1.005*100 é 100.4999… em float64 → trunca para 1.00, NÃO 1.01). Os vetores
// "redondos" (longe da fronteira) cobrem o arredondamento normal; os de fronteira
// travam o comportamento corrente contra regressão silenciosa.
func TestRound2(t *testing.T) {
	eps := 1e-9
	casos := []struct{ in, want float64 }{
		// Longe de fronteira — arredondamento previsível.
		{1.004, 1.00},
		{2.675, 2.68},
		{99.901, 99.90},
		{10.555, 10.56},
		{0, 0},
		{2.5, 2.5}, // já com 1 casa, inalterado
		// Fronteira .xx5 — comportamento REAL (artefato de float, caracterizado).
		{1.005, 1.00}, // 1.005*100 ≈ 100.4999… → trunca p/ 100
		{1.015, 1.01}, // 1.015*100 ≈ 101.4999… → trunca p/ 101
		{-1.005, -1.00},
		{-2.675, -2.68},
	}
	for _, c := range casos {
		got := round2(c.in)
		if math.Abs(got-c.want) > eps {
			t.Errorf("round2(%v)=%.4f, esperava %.4f (comportamento atual)", c.in, got, c.want)
		}
	}
}

// ── normalizarTelefone (strip +55) / nomeMuitoCurto ──────────────────────────

func TestNormalizarTelefone(t *testing.T) {
	casos := []struct{ in, want string }{
		{"5511999998888", "11999998888"},   // 13 dígitos com 55 → strip
		{"551199998888", "1199998888"},     // 12 dígitos com 55 → strip
		{"11999998888", "11999998888"},     // 11 dígitos → mantém
		{"(11) 99999-8888", "11999998888"}, // máscara removida
		{"+55 11 99999-8888", "11999998888"},
		{"5599", "5599"}, // 4 dígitos: não tem 12/13 → mantém o 55
	}
	for _, c := range casos {
		if got := normalizarTelefone(c.in); got != c.want {
			t.Errorf("normalizarTelefone(%q)=%q, esperava %q", c.in, got, c.want)
		}
	}
}

func TestNomeMuitoCurto(t *testing.T) {
	if !nomeMuitoCurto("Al") {
		t.Error("'Al' (2 chars) deveria ser muito curto")
	}
	if !nomeMuitoCurto("   x  ") {
		t.Error("após trim 'x' (1 char) deveria ser muito curto")
	}
	if nomeMuitoCurto("Ana") {
		t.Error("'Ana' (3 chars) NÃO deveria ser muito curto")
	}
	// Acento conta como 1 rune (RuneCountInString, não len de bytes).
	if nomeMuitoCurto("Zé.") {
		t.Error("'Zé.' (3 runes) NÃO deveria ser muito curto")
	}
}

// ── selecionarPenalties (CONGELAMENTO de frustração — função PURA) ───────────
//
// Cobre as 4 combinações de (isento × temAfiliado). O backfill real só exercita
// o caminho isento=true (todos os 6 telefones distintos → 1ª por telefone), então
// é AQUI que travamos o caminho repeat/8 contra regressão silenciosa.

func TestSelecionarPenaltiesModeloDono(t *testing.T) {
	eps := 1e-9
	// Defaults do dono (senderzz_options): prod_first=0, prod_repeat=8,
	// aff_first=5, aff_repeat=5.
	cfg := penaltyConfig{ProdFirst: 0, ProdRepeat: 8, AffFirst: 5, AffRepeat: 5}

	casos := []struct {
		nome              string
		isento            bool
		temAfiliado       bool
		wantProd, wantAff float64
	}{
		// 1ª frustração por telefone, COM afiliado → produtor GRÁTIS, afiliado paga.
		{"isento+afiliado", true, true, 0, 5},
		// 1ª frustração por telefone, SEM afiliado → produtor grátis, afiliado 0.
		{"isento+sem_afiliado", true, false, 0, 0},
		// 2ª+ por telefone, COM afiliado → produtor cobra repeat(8), afiliado paga.
		{"repeat+afiliado", false, true, 8, 5},
		// 2ª+ por telefone, SEM afiliado → produtor cobra 8, afiliado 0.
		{"repeat+sem_afiliado", false, false, 8, 0},
	}
	for _, c := range casos {
		prod, aff := selecionarPenalties(cfg, c.isento, c.temAfiliado)
		if math.Abs(prod-c.wantProd) > eps || math.Abs(aff-c.wantAff) > eps {
			t.Errorf("%s: selecionarPenalties=(%.2f,%.2f), esperava (%.2f,%.2f)",
				c.nome, prod, aff, c.wantProd, c.wantAff)
		}
	}
}

// TestSelecionarPenaltiesAfiliadoSemIsencao: o afiliado paga em TODAS (sem
// isenção). Mesmo quando first != repeat, isento escolhe AffFirst e repeat
// escolhe AffRepeat — nunca ZERA por ser a 1ª (diferente do produtor).
func TestSelecionarPenaltiesAfiliadoSemIsencao(t *testing.T) {
	eps := 1e-9
	// first e repeat DIFERENTES p/ provar que a seleção usa o campo certo.
	cfg := penaltyConfig{ProdFirst: 0, ProdRepeat: 8, AffFirst: 3, AffRepeat: 9}

	// isento=true → afiliado usa AffFirst(3), NÃO 0.
	_, aff := selecionarPenalties(cfg, true, true)
	if math.Abs(aff-3) > eps {
		t.Errorf("afiliado na 1ª deveria pagar AffFirst=3 (sem isenção), veio %.2f", aff)
	}
	// isento=false → afiliado usa AffRepeat(9).
	_, aff = selecionarPenalties(cfg, false, true)
	if math.Abs(aff-9) > eps {
		t.Errorf("afiliado na repeat deveria pagar AffRepeat=9, veio %.2f", aff)
	}
}

// ── normalizarSKU (sub-parte pura de validarSKUPedido — FEAT-SKU) ────────────

func TestNormalizarSKU(t *testing.T) {
	casos := []struct{ in, want string }{
		{"abc-123", "ABC-123"},
		{"  sku-x  ", "SKU-X"},
		{"MixEd\n", "MIXED"},
		{"", ""},
	}
	for _, c := range casos {
		if got := normalizarSKU(c.in); got != c.want {
			t.Errorf("normalizarSKU(%q)=%q, esperava %q", c.in, got, c.want)
		}
	}
}
