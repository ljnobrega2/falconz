// Package handlers — fees.go centraliza a matemática da TAXA DE TRANSAÇÃO de afiliado
// (4,99%). É a ÚNICA fonte da verdade dessa conta no serviço admin: qualquer handler
// que precise discriminar a comissão líquida do afiliado deve chamar estas funções,
// nunca repetir o literal 0.0499 inline.
//
// FÓRMULA OFICIAL DE COMISSÃO (não alterar a regra):
//
//   - Comissão afiliado BRUTA      = valor_total × comissao_pct
//   - Taxa transação afiliado      = BRUTA × 4,99% (0.0499)  → fatia da plataforma
//   - Comissão afiliado LÍQUIDA    = BRUTA × (1 − 0.0499)
//   - Comissão produtor LÍQUIDA    = valor_total − BRUTA − taxa_entrega − taxa_transacao_produtor
//   - Frustrado                    = substitui a comissão pela taxa de frustrado
//
// Exemplo oficial: 276 × 60% = 165,60 (BRUTA); 165,60 × 4,99% = 8,26 (taxa);
//
//	165,60 − 8,26 = 157,34 (LÍQUIDA do afiliado).
package handlers

// taxaTransacaoAfiliadoPct — percentual da taxa de transação do afiliado (4,99%),
// fatia retida pela plataforma sobre a comissão BRUTA. Constante única do módulo.
const taxaTransacaoAfiliadoPct = 0.0499

// comissaoLiquidaAfiliado — comissão LÍQUIDA do afiliado a partir da BRUTA.
// LÍQUIDA = BRUTA × (1 − 0.0499). Ex.: 165,60 → 157,34.
func comissaoLiquidaAfiliado(bruta float64) float64 {
	return bruta * (1 - taxaTransacaoAfiliadoPct)
}

// taxaTransacaoAfiliadoAff — taxa de transação (4,99%) sobre a comissão BRUTA do
// afiliado. É a fatia da plataforma. taxa = BRUTA × 0.0499. Ex.: 165,60 → 8,26.
func taxaTransacaoAfiliadoAff(bruta float64) float64 {
	return bruta * taxaTransacaoAfiliadoPct
}
