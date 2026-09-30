// Testes PUROS dos helpers de montagem de endereço/CEP do pacote geo
// (geocode.go). White-box (package geo) para alcançar funções não-exportadas.
// Sem rede e sem banco — só as funções determinísticas: montarEndereco,
// formatCEP, digitsOnly, strValue, strBlank. As funções com I/O (GeocodePedido,
// geocodeNominatim, buscarRuaViaCEP) NÃO são exercitadas aqui.
package geo

import "testing"

// strP ajuda a montar *string nos casos de teste.
func strP(s string) *string { return &s }

// ── digitsOnly ───────────────────────────────────────────────────────────────

func TestDigitsOnly(t *testing.T) {
	casos := []struct{ in, want string }{
		{"01310-100", "01310100"},
		{"abc123def456", "123456"},
		{"  1 2 3  ", "123"},
		{"sem digito", ""},
		{"", ""},
		{"99999999", "99999999"},
	}
	for _, c := range casos {
		if got := digitsOnly(c.in); got != c.want {
			t.Errorf("digitsOnly(%q)=%q, esperava %q", c.in, got, c.want)
		}
	}
}

// ── formatCEP ────────────────────────────────────────────────────────────────

func TestFormatCEP(t *testing.T) {
	casos := []struct{ in, want string }{
		{"01310100", "01310-100"},   // 8 dígitos → NNNNN-NNN
		{"01310-100", "01310-100"},  // já com hífen → re-sanitiza e formata
		{" 01310100 ", "01310-100"}, // espaços
		{"0131010", ""},             // 7 dígitos → ""
		{"013101000", ""},           // 9 dígitos → ""
		{"abc", ""},                 // sem dígitos → ""
		{"", ""},
	}
	for _, c := range casos {
		if got := formatCEP(c.in); got != c.want {
			t.Errorf("formatCEP(%q)=%q, esperava %q", c.in, got, c.want)
		}
	}
}

// ── strValue / strBlank ──────────────────────────────────────────────────────

func TestStrValue(t *testing.T) {
	if got := strValue(nil); got != "" {
		t.Errorf("strValue(nil)=%q, esperava \"\"", got)
	}
	if got := strValue(strP("  São Paulo  ")); got != "São Paulo" {
		t.Errorf("strValue deveria trimar; veio %q", got)
	}
	if got := strValue(strP("")); got != "" {
		t.Errorf("strValue(\"\")=%q, esperava \"\"", got)
	}
}

func TestStrBlank(t *testing.T) {
	if !strBlank(nil) {
		t.Error("strBlank(nil) deveria ser true")
	}
	if !strBlank(strP("   ")) {
		t.Error("strBlank(\"   \") deveria ser true (só espaços)")
	}
	if strBlank(strP("SP")) {
		t.Error("strBlank(\"SP\") deveria ser false")
	}
}

// ── montarEndereco (concatenação para geocoding) ─────────────────────────────

func TestMontarEnderecoCompleto(t *testing.T) {
	pe := &pedidoEndereco{
		endereco: strP("Av. Paulista"),
		numero:   strP("1000"),
		bairro:   strP("Bela Vista"),
		cidade:   strP("São Paulo"),
		uf:       strP("SP"),
		cep:      "01310-100",
	}
	got := montarEndereco(pe)
	want := "Av. Paulista, 1000 - Bela Vista - São Paulo - SP - 01310-100"
	if got != want {
		t.Errorf("montarEndereco completo=%q, esperava %q", got, want)
	}
}

// TestMontarEnderecoParcial: campos ausentes são pulados (sem hífens/vírgulas
// pendurados), preservando a ordem rua,num - bairro - cidade - uf - cep.
func TestMontarEnderecoParcial(t *testing.T) {
	// Sem número e sem bairro: rua sozinha, depois cidade/uf/cep.
	pe := &pedidoEndereco{
		endereco: strP("Rua das Flores"),
		cidade:   strP("Campinas"),
		uf:       strP("SP"),
		cep:      "13010000",
	}
	got := montarEndereco(pe)
	want := "Rua das Flores - Campinas - SP - 13010-000"
	if got != want {
		t.Errorf("montarEndereco parcial=%q, esperava %q", got, want)
	}
}

// TestMontarEnderecoSoNumeroSemRua: número sem rua entra sozinho na 1ª linha.
func TestMontarEnderecoSoNumeroSemRua(t *testing.T) {
	pe := &pedidoEndereco{
		numero: strP("250"),
		cidade: strP("Santos"),
		uf:     strP("SP"),
	}
	got := montarEndereco(pe)
	want := "250 - Santos - SP"
	if got != want {
		t.Errorf("montarEndereco só número=%q, esperava %q", got, want)
	}
}

// TestMontarEnderecoVazio: nenhum campo → string vazia (caller trata como "sem
// endereço suficiente para geocoding").
func TestMontarEnderecoVazio(t *testing.T) {
	if got := montarEndereco(&pedidoEndereco{}); got != "" {
		t.Errorf("montarEndereco vazio=%q, esperava \"\"", got)
	}
	// CEP inválido (não-8-dígitos) é ignorado por formatCEP → continua vazio.
	if got := montarEndereco(&pedidoEndereco{cep: "123"}); got != "" {
		t.Errorf("montarEndereco com CEP inválido=%q, esperava \"\"", got)
	}
}

// TestMontarEnderecoIgnoraBrancos: campos só-espaços são tratados como ausentes
// (strValue trima → "" → pulado).
func TestMontarEnderecoIgnoraBrancos(t *testing.T) {
	pe := &pedidoEndereco{
		endereco: strP("  "),
		numero:   strP(""),
		cidade:   strP(" Recife "),
		uf:       strP("PE"),
	}
	got := montarEndereco(pe)
	want := "Recife - PE"
	if got != want {
		t.Errorf("montarEndereco com brancos=%q, esperava %q", got, want)
	}
}
