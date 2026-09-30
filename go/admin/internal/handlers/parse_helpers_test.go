// Testes unitários de helpers PUROS compartilhados (sem DB, sem rede):
//   - parseRate / parseRatePtr  (cod_taxas.go) — parse de taxa pt-BR/US, >=0,
//     usado por COD taxas E por order_detail (getOptionFloat → parseRate).
//   - onlyDigits / validateCPF  (onboarding.go) — normalização e validação BR.
//
// package handlers (mesmo pacote). Fixtures locais para não colidir no namespace.
package handlers

import (
	"encoding/json"
	"testing"
)

// ─── parseRate — number|string → float64 >= 0 (vírgula vira ponto) ───────────

func TestParseRate(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want float64
	}{
		{name: "nil → 0", in: nil, want: 0},
		{name: "float64 positivo", in: float64(4.99), want: 4.99},
		{name: "float64 negativo clampa 0", in: float64(-1), want: 0},
		{name: "int positivo", in: 7, want: 7},
		{name: "int negativo clampa 0", in: -3, want: 0},
		{name: "int64 positivo", in: int64(12), want: 12},
		{name: "string US decimal", in: "4.99", want: 4.99},
		{name: "string pt-BR vírgula decimal", in: "4,99", want: 4.99},
		{name: "string com espaços (trim)", in: "  10,5  ", want: 10.5},
		{name: "string vazia → 0", in: "", want: 0},
		{name: "string não numérica → 0", in: "abc", want: 0},
		{name: "string negativa clampa 0", in: "-2,50", want: 0},
		{name: "json.Number", in: json.Number("8.26"), want: 8.26},
		{name: "json.Number negativa clampa 0", in: json.Number("-1"), want: 0},
		{name: "tipo não suportado (bool) → 0", in: true, want: 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseRate(c.in); got != c.want {
				t.Errorf("parseRate(%v) = %v, esperado %v", c.in, got, c.want)
			}
		})
	}
}

// TestParseRatePtr cobre o ponteiro: nil para "não enviado / vazio", valor senão.
func TestParseRatePtr(t *testing.T) {
	t.Run("nil de entrada → nil", func(t *testing.T) {
		if got := parseRatePtr(nil); got != nil {
			t.Errorf("parseRatePtr(nil) = %v, esperado nil", got)
		}
	})
	t.Run("string vazia → nil (herda global)", func(t *testing.T) {
		if got := parseRatePtr(""); got != nil {
			t.Errorf("parseRatePtr(\"\") = %v, esperado nil", got)
		}
	})
	t.Run("string só-espaços → nil", func(t *testing.T) {
		if got := parseRatePtr("   "); got != nil {
			t.Errorf("parseRatePtr(\"   \") = %v, esperado nil", got)
		}
	})
	t.Run("valor presente → ponteiro pro valor", func(t *testing.T) {
		got := parseRatePtr("3,50")
		if got == nil {
			t.Fatal("parseRatePtr(\"3,50\") = nil, esperado ponteiro")
		}
		if *got != 3.5 {
			t.Errorf("*parseRatePtr(\"3,50\") = %v, esperado 3.5", *got)
		}
	})
	t.Run("zero é valor explícito (ponteiro não-nil)", func(t *testing.T) {
		got := parseRatePtr(float64(0))
		if got == nil || *got != 0 {
			t.Errorf("parseRatePtr(0.0) = %v, esperado ponteiro p/ 0", got)
		}
	})
}

// ─── onlyDigits / validateCPF (onboarding.go) ────────────────────────────────

func TestOnlyDigitsHandlers(t *testing.T) {
	cases := map[string]string{
		"123.456.789-00":  "12345678900",
		"(41) 99999-8888": "41999998888",
		"abc":             "",
		"":                "",
		"+55 41 0000":     "55410000",
	}
	for in, want := range cases {
		if got := onlyDigits(in); got != want {
			t.Errorf("onlyDigits(%q) = %q, esperado %q", in, got, want)
		}
	}
}

func TestValidateCPF(t *testing.T) {
	// Vetores válidos (mod-11) confirmados contra o algoritmo brasileiro.
	valid := []string{
		"52998224725",
		"11144477735",
		"390.533.447-05", // mascarado — onlyDigits normaliza antes
	}
	for _, c := range valid {
		if !validateCPF(c) {
			t.Errorf("validateCPF(%q) = false, esperado true", c)
		}
	}
	invalid := []string{
		"00000000000",  // todos iguais
		"11111111111",  // todos iguais
		"12345678900",  // DV inconsistente
		"123",          // comprimento errado
		"",             // vazio
		"5299822472",   // 10 dígitos
		"529982247250", // 12 dígitos
	}
	for _, c := range invalid {
		if validateCPF(c) {
			t.Errorf("validateCPF(%q) = true, esperado false", c)
		}
	}
}

// TestFormatCPF cobre a máscara XXX.XXX.XXX-XX (onboarding.go).
func TestFormatCPF(t *testing.T) {
	cases := map[string]string{
		"52998224725":    "529.982.247-25", // dígitos crus → mascarado
		"529.982.247-25": "529.982.247-25", // já mascarado, normaliza e re-formata
		"123":            "123",            // comprimento != 11 → devolve só dígitos
		"":               "",               // vazio → vazio
		"abc52998224725": "529.982.247-25", // descarta não-dígitos antes
	}
	for in, want := range cases {
		if got := formatCPF(in); got != want {
			t.Errorf("formatCPF(%q) = %q, esperado %q", in, got, want)
		}
	}
}
