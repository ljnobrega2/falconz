// Testes do parsing de valor de comissão no ingest de double-write.
//
// AUDIT TEST-AFFILIATE-COMMISSION-CALC-PARTIAL: a auditoria apontou que o "take"
// de 4,99% só era exercitado em meta read/write (PHP) e em script manual, sem
// guarda no caminho Go. O cálculo do take vive upstream (PHP + triggers em
// infra/postgres/schema-revenue*.sql) — o serviço de afiliados apenas REPLICA o
// valor já assinado por HMAC. Portanto o teste com sentido aqui não é
// reimplementar 300×0,0499 (seria tautológico, testaria só o shopspring/decimal),
// e sim travar o comportamento REAL do ingest: como o valor recebido é
// normalizado antes de ir para senderzz_affiliate_commissions.
//
// Comentários em PT-BR conforme convenção do projeto.
package handlers

import "testing"

// TestParseCommissionValor cobre os ramos do parsing usado em CommissionCreated.
func TestParseCommissionValor(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr bool
		// want é o valor persistido (StringFixed(2)), conferido só quando wantErr=false.
		want string
	}{
		// Default de string vazia → "0.00" (mantém ingest existente, não dispara 400).
		{name: "vazio vira zero", in: "", wantErr: false, want: "0.00"},

		// Take canônico do afiliado: comissão bruta 300,00 → take 4,99% = 14,97.
		// O PHP envia 14.97 pronto; o ingest deve preservar exatamente esse valor.
		{name: "take canonico 14.97", in: "14.97", wantErr: false, want: "14.97"},

		// Net do afiliado (300,00 − 14,97) trafega íntegro pelo ingest.
		{name: "net afiliado 285.03", in: "285.03", wantErr: false, want: "285.03"},

		// StringFixed(2) arredonda half-up na 3a casa — comportamento real do insert.
		{name: "arredonda half-up para cima", in: "14.999", wantErr: false, want: "15.00"},
		{name: "arredonda half-up exato", in: "14.995", wantErr: false, want: "15.00"},
		{name: "trunca casas extras para baixo", in: "14.971", wantErr: false, want: "14.97"},

		// Zero explícito é aceito (assimetria intencional vs. ledger COD que rejeita ≤0).
		{name: "zero explicito aceito", in: "0", wantErr: false, want: "0.00"},

		// Valores não-numéricos → erro → handler responde 400 e NÃO grava comissão.
		{name: "texto invalido", in: "abc", wantErr: true},
		{name: "moeda com simbolo invalida", in: "R$ 14,97", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseCommissionValor(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseCommissionValor(%q): esperava erro, obteve %s", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseCommissionValor(%q): erro inesperado: %v", tc.in, err)
			}
			// StringFixed(2) é exatamente o que vai para o INSERT (linha valor.StringFixed(2)).
			if fixed := got.StringFixed(2); fixed != tc.want {
				t.Errorf("parseCommissionValor(%q).StringFixed(2) = %q, esperado %q", tc.in, fixed, tc.want)
			}
		})
	}
}
