// Package handlers — BRIDGE de status motoboy → pedido (sz_orders).
//
// Problema resolvido: o motoboy marca sz_motoboy_pedidos.status='entregue' mas
// sz_orders.status nunca mudava → o rastreio dizia "entregue" e o pedido dizia
// "embalado" (divergência que vira disputa / quebra de confiança).
//
// Solução (advisor: double-write DIRETO, NÃO NATS): após atualizar
// sz_motoboy_pedidos, na MESMA transação também atualiza sz_orders.status do
// pedido vinculado.
//
// Link entre as tabelas (canônico, já usado em go/admin/internal/handlers/orders.go):
//
//	sz_orders.wp_order_id = sz_motoboy_pedidos.wc_order_id
//
// Mapeamento motoboy → sz_orders.status (espelha senderzz_motoboy_status_to_woo_status()
// em includes/senderzz-woo-status-authority.php, restrito ao CHECK constraint de
// sz_orders.status em infra/postgres/schema-orders.sql):
//
//	entregue   → completo   (WP: entregue → completo)
//	frustrado  → frustrado
//	cancelado  → cancelled  (WP: cancelado → cancelled)
//	em_rota    → enviado    ("em trânsito"; em-rota NÃO é valor CHECK-válido em sz_orders)
//
// Valores CHECK-válidos de sz_orders.status:
//
//	pending, processing, aguardando, on-hold, em_separacao, embalado,
//	enviado, entregue, completo, cancelled, frustrado, reembolsado
package handlers

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5"
)

// motoboyStatusToSzOrder mapeia um status operacional do motoboy para o status
// equivalente em sz_orders (restrito ao CHECK constraint da tabela).
//
// Retorna ok=false para status sem equivalente CHECK-válido relevante ao bridge
// (ex.: 'agendado', 'pendente') — nesse caso o bridge é simplesmente pulado e
// só sz_motoboy_pedidos é atualizado.
func motoboyStatusToSzOrder(motoboyStatus string) (szStatus string, ok bool) {
	switch motoboyStatus {
	case "entregue":
		return "completo", true // COD Motoboy entregue = Woo "Completo".
	case "frustrado":
		return "frustrado", true
	case "cancelado":
		return "cancelled", true
	case "em_rota":
		return "enviado", true // em-rota/em_transito NÃO são CHECK-válidos; 'enviado' = em trânsito.
	case "a_caminho":
		// 'a_caminho' é uma SUB-ETAPA operacional de em_rota ("motoboy saiu para
		// ESTE pedido, próxima parada"). No PHP (sz_motoboy_mudar_status) o status
		// Woo NÃO regride — continua em trânsito. Mapeia para 'enviado' igual a
		// em_rota: o bridge é idempotente (AND o.status<>$2), então se já estava
		// 'enviado' não há novo write. Nunca volta o Woo para um estado anterior.
		return "enviado", true
	case "embalado":
		return "embalado", true // CHECK-válido; cobre MudarStatus(embalado) sem caso especial.
	default:
		return "", false
	}
}

// bridgeUpdatePedidoStatus aplica o BRIDGE de forma atômica:
//
//  1. Inicia uma transação.
//  2. Aplica os UPDATEs do motoboy (setUpdatesSQL deve ser o trecho após
//     "UPDATE sz_motoboy_pedidos SET ", contendo a cláusula SET completa, SEM o
//     WHERE — o WHERE id=$1 é adicionado aqui). args[0] DEVE ser o pedidoID.
//  3. Se o novo status motoboy mapeia para um status sz_orders CHECK-válido E a
//     tabela sz_orders existe (janela de migração), atualiza sz_orders.status do
//     pedido vinculado (idempotente: só muda se diferente).
//  4. Commit. Tudo na MESMA transação → nunca reintroduz a divergência.
//
// Casos tratados (conforme advisor):
//   - tabela sz_orders ausente → pula o write de sz_orders, comita o motoboy.
//   - nenhuma linha vinculada / status já no alvo → 0 linhas, sem erro.
//   - erro real de DB no sz_orders → rollback (motoboy pode retentar).
//
// Retorna o número de linhas de sz_orders atualizadas (0 ou 1) e erro.
func bridgeUpdatePedidoStatus(
	ctx context.Context,
	tx pgx.Tx,
	pedidoID int64,
	novoStatusMotoboy string,
) (szRows int64, err error) {
	szStatus, ok := motoboyStatusToSzOrder(novoStatusMotoboy)
	if !ok {
		return 0, nil // status sem equivalente em sz_orders — nada a espelhar.
	}

	// Guard janela de migração: to_regclass retorna NULL (sem abortar a tx) se a
	// tabela não existe. Durante a migração sz_orders pode genuinamente não existir.
	var reg *string
	if err := tx.QueryRow(ctx, `SELECT to_regclass('public.sz_orders')::text`).Scan(&reg); err != nil {
		return 0, err
	}
	if reg == nil {
		return 0, nil // sz_orders ainda não migrada — só o motoboy é atualizado.
	}

	// Idempotente: AND o.status <> $2 evita updates e trigger updated_at à toa.
	tag, err := tx.Exec(ctx, `
		UPDATE sz_orders o
		   SET status = $2, updated_at = NOW()
		  FROM sz_motoboy_pedidos p
		 WHERE p.id = $1
		   AND COALESCE(o.wp_order_id, o.id) = p.wc_order_id
		   AND o.status <> $2`,
		pedidoID, szStatus,
	)
	if err != nil {
		return 0, err
	}
	rows := tag.RowsAffected()

	// AUDIT-2026-07-11: entregue→completo é o único ponto que sabe que a
	// comissão do afiliado ficou DEVIDA. Antes disso nada gravava
	// senderzz_affiliate_transactions (o único writer era o webhook legado
	// /internal/commissions, do fluxo WP antigo, sem nenhum caller no checkout
	// novo) — carteira do afiliado ficava zerada mesmo com pedidos entregues.
	// Vínculo resolvido por (afiliado_id=o.affiliate_id, produtor_id=o.produtor_id),
	// mesma convenção de affiliate_wallet.go. INSERT-only, idempotente via
	// NOT EXISTS — nunca duplica se o bridge rodar 2x para o mesmo pedido.
	if rows > 0 && szStatus == "completo" {
		if _, errTx := tx.Exec(ctx, `
			INSERT INTO senderzz_affiliate_transactions
			    (order_id, affiliate_id, type, status, amount, description, available_at, meta_json, created_at, updated_at)
			SELECT o.id, sa.id, 'commission', 'pending', o.affiliate_amount,
			       'Comissão pedido #' || o.order_number,
			       NOW() + INTERVAL '30 days',
			       jsonb_build_object('source', 'bridge_entregue'),
			       NOW(), NOW()
			  FROM sz_orders o
			  JOIN sz_motoboy_pedidos p2 ON p2.id = $1
			  JOIN senderzz_affiliates sa
			    ON sa.afiliado_id = o.affiliate_id AND sa.produtor_id = o.produtor_id
			 WHERE COALESCE(o.wp_order_id, o.id) = p2.wc_order_id
			   AND COALESCE(o.affiliate_id, 0) > 0
			   AND COALESCE(o.affiliate_amount, 0) > 0
			   AND NOT EXISTS (
			         SELECT 1 FROM senderzz_affiliate_transactions t
			          WHERE t.order_id = o.id AND t.type = 'commission' AND t.status <> 'cancelled'
			       )`,
			pedidoID,
		); errTx != nil {
			return 0, errTx
		}
	}

	return rows, nil
}

// logBridge registra o resultado do bridge de forma uniforme.
func logBridge(scope string, pedidoID int64, novoStatusMotoboy, szStatus string, szRows int64) {
	if szRows > 0 {
		slog.Info("[bridge] sz_orders.status sincronizado",
			"scope", scope, "pedido_id", pedidoID,
			"motoboy_status", novoStatusMotoboy, "sz_orders_status", szStatus)
	}
}
