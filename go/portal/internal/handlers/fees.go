// Package handlers — fees.go documenta a matemática da TAXA DE TRANSAÇÃO de afiliado
// (4,99%) no SERVIÇO DE PORTAL.
//
// ── MODELO FINANCEIRO REAL (verificado com dados — pedido 1570) ─────────────────
//
// As colunas de sz_orders JÁ vêm com os valores FINAIS calculados na origem (WP):
//
//   - sz_orders.affiliate_amount = senderzz_affiliate_transactions.amount
//     = comissão LÍQUIDA do afiliado (JÁ net). NÃO é a bruta.
//   - sz_orders.transaction_fee  = a taxa de transação REAL do afiliado (4,99%),
//     fatia (take) da plataforma. É o valor a EXIBIR como "Taxa transação afiliado".
//   - Comissão afiliado BRUTA    = affiliate_amount + transaction_fee (= total × pct).
//   - Líquido produtor           = total − BRUTA − taxa_entrega
//                                = total − affiliate_amount − transaction_fee − taxa_entrega.
//   - Frustrado/cancelado/reembol. = sem taxa (taxa=0); a comissão é substituída pela
//     taxa de frustrado (penalidade lida das metas) e o líquido do produtor é zerado.
//
// Exemplo oficial (pedido 1570): total 276,00; affiliate_amount 157,34 (LÍQUIDA);
// transaction_fee 8,26 (taxa 4,99%); BRUTA = 157,34 + 8,26 = 165,60 = 276 × 60%;
// taxa_entrega 23,98 → líquido produtor = 276 − 165,60 − 23,98 = 86,42.
//
// ⚠️ NÃO recalcular a líquida via BRUTA × (1 − 0,0499): isso era DUPLA COBRANÇA —
// aplicava 4,99% de novo sobre um valor (affiliate_amount) que já estava LÍQUIDO.
// A líquida É o affiliate_amount armazenado; a taxa É o transaction_fee armazenado.
package handlers

// taxaTransacaoAfiliadoPct — percentual da taxa de transação do afiliado (4,99%),
// fatia retida pela plataforma sobre a comissão BRUTA. Mantido só para REFERÊNCIA /
// auditoria do percentual (transaction_fee / bruta ≈ 0.0499). NÃO usar para recalcular
// taxa ou líquida — esses valores vêm prontos de sz_orders (ver doc do pacote).
const taxaTransacaoAfiliadoPct = 0.0499

// _ — sentinela de uso da constante (mantém o linter quieto sem reintroduzir a conta
// que causava a dupla cobrança). taxaTransacaoAfiliadoPct é doc/auditoria-only.
var _ = taxaTransacaoAfiliadoPct
