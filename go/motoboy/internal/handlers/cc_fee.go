// Package handlers — helpers de taxa de cartão (valor com taxa para o motoboy cobrar).
//
// REGRA "VALOR COM TAXA CARTÃO":
// O motoboy pode cobrar dois valores no destino — o valor em dinheiro (valor_pedido)
// e o valor no cartão, acrescido da taxa configurada em wp_options.sz_motoboy_cc_fee_pct.
// Aqui só expomos o valor calculado; a exibição (riscado/💳) é responsabilidade do front.
package handlers

import (
	"context"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ccFeePct lê a taxa de cartão (em %) da option sz_motoboy_cc_fee_pct (wp_options).
// Default 3.29 — option ausente ou vazia usa a taxa padrão do negócio.
// Deve ser lida UMA vez por request e reaproveitada por linha (não consultar por pedido).
func ccFeePct(ctx context.Context, pool *pgxpool.Pool) float64 {
	var ccFeeStr string
	err := pool.QueryRow(ctx,
		`SELECT option_value FROM wp_options WHERE option_name = 'sz_motoboy_cc_fee_pct' LIMIT 1`,
	).Scan(&ccFeeStr)
	if err != nil {
		return 3.29
	}
	ccFeeStr = strings.TrimSpace(ccFeeStr)
	if ccFeeStr == "" {
		return 3.29
	}
	pct, _ := strconv.ParseFloat(ccFeeStr, 64)
	return pct
}

// valorCartao aplica a taxa de cartão sobre o valor em dinheiro.
// valor_cartao = valor * (1 + pct/100). Com pct == 0 retorna o próprio valor.
func valorCartao(valor, pct float64) float64 {
	return valor * (1 + pct/100)
}
