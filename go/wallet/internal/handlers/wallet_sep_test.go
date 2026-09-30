// SEC-WALLET-SEP — testes da carteira de EXPEDIÇÃO/FRETE (tpc_*).
//
// Cobrem os helpers PUROS de dinheiro do go/wallet (sem rede, sem DB):
//   - saldoDisponivel  → regra de saldo disponível = max(0, saldo - reservado),
//                         incluindo aritmética de centavos e clamp em zero.
//   - valorMinimoRecarga → piso de validação de recarga (R$ 10,00).
//
// Estes testes blindam invariantes financeiras determinísticas. Os ledger ops
// DB-bound (PostReservar/Debitar/Creditar/Liberar) e o webhook exigem Postgres e
// são cobertos por testes de integração fora deste pacote.
//
// FRONTEIRA DE CARTEIRAS (SEC-WALLET-SEP): este pacote opera SOMENTE tpc_*
// (expedição). A carteira COD/motoboy (sz_cod_wallet_transactions, coluna net) é
// território do portal/motoboy e NUNCA deve ser lida/escrita aqui. Auditoria por
// grep confirma zero referências a sz_cod_wallet_transactions neste módulo.
package handlers

import (
	"testing"

	"github.com/shopspring/decimal"
)

// d é um atalho para construir Decimal a partir de string (centavos exatos).
func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

// TestSaldoDisponivel valida a fórmula disponivel = max(0, saldo - saldo_reservado).
//
// Cobre, num só lugar, parse de string → Decimal, subtração de centavos sem
// corrupção de ponto flutuante, clamp em zero e formatação StringFixed(2).
// É a regra usada por GET /carteira/saldo, POST /carteira/reservar e GET /me —
// a fonte única saldoDisponivel(). Qualquer drift quebra estes vetores.
func TestSaldoDisponivel(t *testing.T) {
	cases := []struct {
		nome      string
		saldo     string
		reservado string
		want      string // StringFixed(2)
	}{
		{"sem reserva", "100.00", "0.00", "100.00"},
		{"reserva parcial", "100.00", "30.00", "70.00"},
		{"centavos exatos", "100.10", "0.20", "99.90"},
		{"reserva igual ao saldo → zero", "50.00", "50.00", "0.00"},
		{"reserva maior que saldo → clamp em zero", "50.00", "80.00", "0.00"},
		{"saldo e reserva zerados", "0.00", "0.00", "0.00"},
		{"valor grande com milhar", "2500.00", "180.00", "2320.00"},
		// Subtração que em float64 (0.1-0.07) daria 0.029999... — decimal mantém 0.03.
		{"precisão de centavos sutil", "0.10", "0.07", "0.03"},
	}
	for _, c := range cases {
		got := saldoDisponivel(d(c.saldo), d(c.reservado)).StringFixed(2)
		if got != c.want {
			t.Errorf("%s: saldoDisponivel(%s, %s) = %s, esperado %s",
				c.nome, c.saldo, c.reservado, got, c.want)
		}
	}
}

// TestSaldoDisponivelNuncaNegativo é uma propriedade-chave de segurança: o saldo
// disponível JAMAIS pode ser negativo, qualquer que seja a relação saldo×reserva.
// Um disponível negativo, se vazasse para a comparação de PostReservar, poderia
// distorcer a checagem de saldo insuficiente. O clamp garante o fail-safe.
func TestSaldoDisponivelNuncaNegativo(t *testing.T) {
	pares := [][2]string{
		{"0.00", "0.01"},
		{"10.00", "999999.99"},
		{"0.00", "1000000.00"},
		{"1.00", "1.01"},
	}
	for _, p := range pares {
		got := saldoDisponivel(d(p[0]), d(p[1]))
		if got.IsNegative() {
			t.Errorf("saldoDisponivel(%s, %s) = %s — disponível NÃO pode ser negativo",
				p[0], p[1], got.StringFixed(2))
		}
	}
}

// TestValorMinimoRecarga ancora o piso de validação de recarga (R$ 10,00).
// Espelha tpc_criar_recarga ($valor < 10 → false). PostRecarregar rejeita com 400
// qualquer valor abaixo desse piso ANTES de tocar o banco ou emitir PIX.
func TestValorMinimoRecarga(t *testing.T) {
	if valorMinimoRecarga != 10.0 {
		t.Fatalf("valorMinimoRecarga = %v, esperado 10.0 (piso do WP)", valorMinimoRecarga)
	}

	// Vetores de aceitação/rejeição espelhando a checagem `req.Valor < valorMinimoRecarga`.
	cases := []struct {
		valor   float64
		aceitar bool
	}{
		{9.99, false},
		{0, false},
		{-5, false},
		{10.0, true},
		{10.01, true},
		{50, true},
		{1000.50, true},
	}
	for _, c := range cases {
		aceito := !(c.valor < valorMinimoRecarga)
		if aceito != c.aceitar {
			t.Errorf("valor %.2f: aceito=%v, esperado %v", c.valor, aceito, c.aceitar)
		}
	}
}
