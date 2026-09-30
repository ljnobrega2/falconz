package handlers

import "testing"

// TestFormatBRLNumber valida o porte de number_format($v, 2, ',', '.') do PHP:
// decimal com vírgula, milhar com ponto.
func TestFormatBRLNumber(t *testing.T) {
	cases := map[float64]string{
		10:        "10,00",
		10.5:      "10,50",
		1234.5:    "1.234,50",
		1000000:   "1.000.000,00",
		0:         "0,00",
		999.99:    "999,99",
		1234567.8: "1.234.567,80",
	}
	for in, want := range cases {
		if got := formatBRLNumber(in); got != want {
			t.Errorf("formatBRLNumber(%v) = %q, esperado %q", in, got, want)
		}
	}
}

// TestFormatBRL valida o prefixo "R$ ".
func TestFormatBRL(t *testing.T) {
	if got := formatBRL(1234.5); got != "R$ 1.234,50" {
		t.Errorf("formatBRL(1234.5) = %q, esperado 'R$ 1.234,50'", got)
	}
}
