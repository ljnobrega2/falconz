// Testes unitários dos helpers PUROS de afiliados (sem DB, sem HTTP).
//
// SEC-AFFILIATES: cobre os três helpers determinísticos que sustentam regras
// de segurança/financeiras do serviço:
//   - randomHex        — tamanho e charset do token de convite/link (entropia);
//   - comissaoPctValida — guarda de intervalo 0..100 usada em Approve (CWE-20);
//   - comissaoLiquida   — pino de regressão da alíquota canônica de 4,99%.
//
// Estilo seguindo internal_commission_test.go: tabela de casos, vetores claros,
// comentários em PT-BR conforme convenção do projeto.
package handlers

import (
	"regexp"
	"testing"

	"github.com/shopspring/decimal"
)

// reHex64 valida a representação hex de 32 bytes (64 chars, só 0-9a-f).
var reHex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

// TestRandomHexTamanhoEHex confere que randomHex(n) devolve 2n chars hex válidos.
func TestRandomHexTamanhoEHex(t *testing.T) {
	cases := []struct {
		name    string
		n       int
		wantLen int
	}{
		{name: "32 bytes = 64 hex (token de convite/link)", n: 32, wantLen: 64},
		{name: "16 bytes = 32 hex", n: 16, wantLen: 32},
		{name: "1 byte = 2 hex", n: 1, wantLen: 2},
		{name: "0 bytes = string vazia", n: 0, wantLen: 0},
	}

	hexOnly := regexp.MustCompile(`^[0-9a-f]*$`)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := randomHex(tc.n)
			if err != nil {
				t.Fatalf("randomHex(%d): erro inesperado: %v", tc.n, err)
			}
			if len(got) != tc.wantLen {
				t.Errorf("randomHex(%d): len = %d, esperado %d (%q)", tc.n, len(got), tc.wantLen, got)
			}
			if !hexOnly.MatchString(got) {
				t.Errorf("randomHex(%d): %q contém caractere não-hex", tc.n, got)
			}
		})
	}
}

// TestRandomHexCanonico32 trava o contrato de token de 32 bytes (64 hex) usado em
// CreateInvite e CreateLink — qualquer mudança de tamanho quebra aqui.
func TestRandomHexCanonico32(t *testing.T) {
	tok, err := randomHex(32)
	if err != nil {
		t.Fatalf("randomHex(32): erro inesperado: %v", err)
	}
	if !reHex64.MatchString(tok) {
		t.Errorf("randomHex(32) = %q; esperado 64 chars hex (token de convite/link)", tok)
	}
}

// TestRandomHexUnicidade garante entropia mínima: duas gerações não colidem.
// Não é prova criptográfica, mas pega um randomHex acidentalmente determinístico.
func TestRandomHexUnicidade(t *testing.T) {
	seen := make(map[string]struct{}, 64)
	for i := 0; i < 64; i++ {
		tok, err := randomHex(32)
		if err != nil {
			t.Fatalf("randomHex(32): erro inesperado na iteração %d: %v", i, err)
		}
		if _, dup := seen[tok]; dup {
			t.Fatalf("randomHex(32) gerou token duplicado na iteração %d: %q", i, tok)
		}
		seen[tok] = struct{}{}
	}
}

// TestComissaoPctValida cobre a guarda de intervalo 0..100 usada em Approve.
func TestComissaoPctValida(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{name: "zero é válido", in: "0", want: true},
		{name: "limite inferior 0.00", in: "0.00", want: true},
		{name: "valor típico 10%", in: "10", want: true},
		{name: "decimal 12.50", in: "12.50", want: true},
		{name: "limite superior 100", in: "100", want: true},
		{name: "limite superior 100.00", in: "100.00", want: true},

		{name: "acima do limite 100.01", in: "100.01", want: false},
		{name: "muito acima 999.99 (cap DECIMAL(5,2))", in: "999.99", want: false},
		{name: "negativo rejeitado", in: "-0.01", want: false},
		{name: "negativo grande rejeitado", in: "-10", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pct := decimal.RequireFromString(tc.in)
			if got := comissaoPctValida(pct); got != tc.want {
				t.Errorf("comissaoPctValida(%s) = %v, esperado %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestComissaoLiquida trava a fórmula líquida = bruta − 4,99%.
// Pino de regressão da alíquota canônica (não é caminho de produção — ver
// comentário em comissaoLiquida).
func TestComissaoLiquida(t *testing.T) {
	cases := []struct {
		name  string
		bruta string
		want  string // líquida esperada, StringFixed(2)
	}{
		// Vetor canônico documentado em internal_commission_test.go:
		// bruta 300,00 → take 14,97 → líquida 285,03.
		{name: "bruta 300,00 → líquida 285,03", bruta: "300.00", want: "285.03"},
		// bruta 100,00 → take 4,99 → líquida 95,01.
		{name: "bruta 100,00 → líquida 95,01", bruta: "100.00", want: "95.01"},
		// Zero não rende take.
		{name: "bruta 0,00 → líquida 0,00", bruta: "0.00", want: "0.00"},
		// bruta 1,00 → take 0,0499 → líquida 0,9501 → arredonda 0,95.
		{name: "bruta 1,00 → líquida 0,95", bruta: "1.00", want: "0.95"},
		// Borda de valor alto: bruta 100.000,00 → take 4.990,00 → líquida 95.010,00.
		// Garante que a alíquota escala sem overflow/perda de precisão (shopspring/decimal).
		{name: "bruta 100000,00 → líquida 95010,00", bruta: "100000.00", want: "95010.00"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bruta := decimal.RequireFromString(tc.bruta)
			got := comissaoLiquida(bruta).StringFixed(2)
			if got != tc.want {
				t.Errorf("comissaoLiquida(%s) = %s, esperado %s", tc.bruta, got, tc.want)
			}
		})
	}
}

// TestTakePctAfiliadoCanonico documenta explicitamente a alíquota travada.
func TestTakePctAfiliadoCanonico(t *testing.T) {
	if got := takePctAfiliado.StringFixed(2); got != "4.99" {
		t.Errorf("takePctAfiliado = %s, esperado 4.99 (alíquota canônica)", got)
	}
}
