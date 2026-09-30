// Testes do núcleo financeiro de antecipação/saque (withdrawal) da carteira COD.
//
// decidirValorAntecipacao é a tradução PURA das regras inline de PostAnticipate
// (cod.go) — sem DB, sem HTTP. Travar este comportamento garante que o valor
// debitado e os erros 402 ("saldo insuficiente" / "valor > saldo") não regridem.
//
// Comentários em PT-BR conforme convenção do projeto.
package handlers

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"
)

// ptr devolve um *decimal.Decimal a partir de uma string (helper de teste).
func ptr(s string) *decimal.Decimal {
	d := decimal.RequireFromString(s)
	return &d
}

// TestDecidirValorAntecipacao cobre todas as combinações saldo × valor solicitado.
func TestDecidirValorAntecipacao(t *testing.T) {
	cases := []struct {
		name     string
		saldo    string
		valorReq *decimal.Decimal
		want     string // valor a antecipar, StringFixed(2) — só quando wantErr==nil
		wantErr  error
	}{
		// ── Antecipação do saldo total (valor não informado) ──────────────────
		{name: "valor nil antecipa saldo total", saldo: "150.00", valorReq: nil, want: "150.00"},
		{name: "valor zero antecipa saldo total", saldo: "80.00", valorReq: ptr("0"), want: "80.00"},
		{name: "valor negativo tratado como total", saldo: "80.00", valorReq: ptr("-5"), want: "80.00"},

		// ── Antecipação parcial (valor ≤ saldo) ───────────────────────────────
		{name: "valor parcial dentro do saldo", saldo: "150.00", valorReq: ptr("50.00"), want: "50.00"},
		{name: "valor igual ao saldo", saldo: "150.00", valorReq: ptr("150.00"), want: "150.00"},
		{name: "valor parcial com centavos", saldo: "99.99", valorReq: ptr("33.33"), want: "33.33"},

		// ── Saldo insuficiente (≤ 0) ──────────────────────────────────────────
		{name: "saldo zero rejeita", saldo: "0.00", valorReq: nil, wantErr: errSaldoInsuficiente},
		{name: "saldo zero rejeita mesmo com valor", saldo: "0.00", valorReq: ptr("10.00"), wantErr: errSaldoInsuficiente},
		{name: "saldo negativo rejeita", saldo: "-1.00", valorReq: nil, wantErr: errSaldoInsuficiente},

		// ── Valor solicitado maior que o saldo ────────────────────────────────
		{name: "valor acima do saldo rejeita", saldo: "100.00", valorReq: ptr("100.01"), wantErr: errValorMaiorQueSaldo},
		{name: "valor muito acima do saldo rejeita", saldo: "10.00", valorReq: ptr("9999.00"), wantErr: errValorMaiorQueSaldo},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			saldo := decimal.RequireFromString(tc.saldo)
			got, err := decidirValorAntecipacao(saldo, tc.valorReq)

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("decidirValorAntecipacao(%s, %v): erro = %v, esperado %v",
						tc.saldo, tc.valorReq, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("decidirValorAntecipacao(%s, %v): erro inesperado: %v", tc.saldo, tc.valorReq, err)
			}
			if fixed := got.StringFixed(2); fixed != tc.want {
				t.Errorf("decidirValorAntecipacao(%s, %v) = %s, esperado %s",
					tc.saldo, tc.valorReq, fixed, tc.want)
			}
		})
	}
}

// TestDecidirValorNuncaExcedeSaldo é uma invariante de segurança financeira:
// o valor antecipado JAMAIS pode ultrapassar o saldo (evita saque a descoberto).
func TestDecidirValorNuncaExcedeSaldo(t *testing.T) {
	saldo := decimal.RequireFromString("250.00")
	for _, req := range []*decimal.Decimal{nil, ptr("0"), ptr("1.00"), ptr("250.00"), ptr("249.99")} {
		got, err := decidirValorAntecipacao(saldo, req)
		if err != nil {
			t.Fatalf("valor válido %v retornou erro: %v", req, err)
		}
		if got.GreaterThan(saldo) {
			t.Errorf("INVARIANTE QUEBRADA: antecipou %s > saldo %s (req=%v)", got, saldo, req)
		}
	}
}
