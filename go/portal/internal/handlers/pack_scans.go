// Package handlers — registro de auditoria de bipagem (sz_pack_scans).
//
// sz_pack_scans é REGISTRO/observabilidade (accountability) — NÃO é a regra de
// validação. A regra (SKU bipado vs SKU do pedido → trava se diferente) vive nos
// handlers (ver expedicao_bipar.go). Aqui só persistimos cada leitura com o
// veredito (matched), inclusive as bloqueadas (matched=false), para a trilha.
//
// context separa os fluxos (ver 240-fixes-v472-lgpd-sku.sql):
//
//	em_rota   | devolucao | expedicao | estoque
package handlers

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// packScan — uma linha a registrar em sz_pack_scans. Ponteiros nas colunas
// nullable (wc_order_id, product_id, sku_expected) p/ gravar NULL quando ausente.
type packScan struct {
	Context     string  // em_rota | devolucao | expedicao | estoque
	WCOrderID   *int64  // pedido (quando aplicável)
	ProductID   *int64  // sz_products.id / item (quando aplicável)
	SKUScanned  string  // SKU lido (ou digitado)
	SKUExpected *string // SKU esperado do pedido (auditoria)
	Matched     bool    // true = passou; false = bloqueou
	Quantity    int     // estoque: qtd informada (bipa 1, digita qtd); demais = 1
	ManualTyped bool    // true = digitou o código (fallback)
	Actor       string  // identificador do operador (e-mail do portal)
}

// insertPackScan grava uma leitura em sz_pack_scans (best-effort).
// Erro só é logado — a auditoria não deve derrubar o fluxo operacional, mas
// também não silencia falhas (visibilidade via log). Quantity < 1 normaliza p/ 1.
func insertPackScan(ctx context.Context, pool *pgxpool.Pool, s packScan) {
	if s.Quantity < 1 {
		s.Quantity = 1
	}
	_, err := pool.Exec(ctx,
		`INSERT INTO sz_pack_scans
		    (context, wc_order_id, product_id, sku_scanned, sku_expected,
		     matched, quantity, manual_typed, actor)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		s.Context, s.WCOrderID, s.ProductID, s.SKUScanned, s.SKUExpected,
		s.Matched, s.Quantity, s.ManualTyped, nullIfEmpty(s.Actor),
	)
	if err != nil {
		slog.Warn("[pack_scans] falha ao registrar leitura (best-effort)",
			"context", s.Context, "wc_order_id", s.WCOrderID, "matched", s.Matched, "err", err)
	}
}

// recordScan — wrapper de método no ExpedicaoHandler para chamadas idiomáticas
// h.recordScan(ctx, packScan{...}) a partir do Bipar.
func (h *ExpedicaoHandler) recordScan(ctx context.Context, s packScan) {
	insertPackScan(ctx, h.Pool, s)
}

// nullIfEmpty devolve nil p/ string vazia (grava NULL em colunas nullable).
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
