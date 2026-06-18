// Package statemachine implementa a máquina de estados de pedidos do Senderzz.
//
// Estados e transições válidas (espelha o fluxo do WooCommerce + status customizados):
//
//   pending      → processing | cancelled
//   processing   → aguardando | cancelled
//   aguardando   → em_separacao | on-hold | cancelled
//   on-hold      → aguardando | cancelled
//   em_separacao → embalado | cancelled
//   embalado     → enviado | frustrado | cancelled
//   enviado      → entregue | frustrado
//   entregue     → completo | reembolsado
//   completo     → reembolsado
//   frustrado    → aguardando | cancelled
//   cancelled    → [] (terminal)
//   reembolsado  → [] (terminal)
//
// A transição é atômica: UPDATE sz_orders + INSERT sz_order_status_history
// dentro de uma transação serializable. Falha se o status atual já mudou
// (read-modify-write com SELECT FOR UPDATE).
//
// Publicação de evento: por ora apenas log estruturado. Stub NATS preparado
// como comentário para Fase 7 (event bus). Ver publishEvent abaixo.
package statemachine

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ValidTransitions define as transições permitidas por status de origem.
// Estados terminais (cancelled, reembolsado) têm slice vazio — imutáveis.
var ValidTransitions = map[string][]string{
	"pending":      {"processing", "cancelled"},
	"processing":   {"aguardando", "cancelled"},
	"aguardando":   {"em_separacao", "on-hold", "cancelled"},
	"on-hold":      {"aguardando", "cancelled"},
	"em_separacao": {"embalado", "cancelled"},
	"embalado":     {"enviado", "frustrado", "cancelled"},
	"enviado":      {"entregue", "frustrado"},
	"entregue":     {"completo", "reembolsado"},
	"completo":     {"reembolsado"},
	"frustrado":    {"aguardando", "cancelled"},
	"cancelled":    {},
	"reembolsado":  {},
}

// TerminalStatuses lista os status de onde não há transição possível.
var TerminalStatuses = map[string]bool{
	"cancelled":   true,
	"reembolsado": true,
}

// CanTransition retorna true se a transição de `from` para `to` é permitida
// pela máquina de estados.
func CanTransition(from, to string) bool {
	targets, ok := ValidTransitions[from]
	if !ok {
		// Status desconhecido — fail-closed.
		return false
	}
	for _, t := range targets {
		if t == to {
			return true
		}
	}
	return false
}

// Transition executa a transição de status do pedido de forma atômica.
//
// Fluxo:
//  1. Inicia transação serializable.
//  2. SELECT FOR UPDATE em sz_orders para obter status atual e prevenir race.
//  3. Valida CanTransition(statusAtual, toStatus).
//  4. UPDATE sz_orders SET status = toStatus, updated_at = NOW().
//  5. INSERT sz_order_status_history (histórico imutável).
//  6. Commit.
//  7. Publica evento de mudança de status (log + stub NATS).
//
// Retorna erro se:
//   - pedido não encontrado
//   - transição inválida pela máquina de estados
//   - erro de banco de dados
func Transition(
	ctx context.Context,
	pool *pgxpool.Pool,
	orderID int64,
	toStatus string,
	actorID int64,
	actorTipo string,
	motivo string,
) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("[statemachine] falha ao iniciar transação: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Obtém status atual com lock para prevenir transições concorrentes.
	var currentStatus string
	err = tx.QueryRow(ctx,
		`SELECT status FROM sz_orders WHERE id = $1 FOR UPDATE`,
		orderID,
	).Scan(&currentStatus)
	if err == pgx.ErrNoRows {
		return fmt.Errorf("[statemachine] pedido %d não encontrado", orderID)
	}
	if err != nil {
		return fmt.Errorf("[statemachine] erro ao buscar status do pedido %d: %w", orderID, err)
	}

	// Valida a transição antes de qualquer escrita.
	if !CanTransition(currentStatus, toStatus) {
		return fmt.Errorf(
			"[statemachine] transição inválida para pedido %d: %q → %q",
			orderID, currentStatus, toStatus,
		)
	}

	// Atualiza o status do pedido. updated_at é gerenciado pelo trigger sz_set_updated_at.
	_, err = tx.Exec(ctx,
		`UPDATE sz_orders SET status = $1, updated_at = NOW() WHERE id = $2`,
		toStatus, orderID,
	)
	if err != nil {
		return fmt.Errorf("[statemachine] falha ao atualizar status do pedido %d: %w", orderID, err)
	}

	// Registra a transição no histórico imutável.
	_, err = tx.Exec(ctx,
		`INSERT INTO sz_order_status_history
		    (order_id, status_de, status_para, motivo, actor_id, actor_tipo, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, NOW())`,
		orderID, currentStatus, toStatus, motivo, actorID, actorTipo,
	)
	if err != nil {
		return fmt.Errorf("[statemachine] falha ao registrar histórico de status do pedido %d: %w", orderID, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("[statemachine] falha ao commitar transição do pedido %d: %w", orderID, err)
	}

	slog.Info("[statemachine] transição de status",
		"order_id", orderID,
		"de", currentStatus,
		"para", toStatus,
		"actor_id", actorID,
		"actor_tipo", actorTipo,
	)

	// Publica evento de mudança de status.
	// Por ora apenas log estruturado. Stub preparado para NATS (Fase 7).
	publishEvent(ctx, orderID, currentStatus, toStatus, actorID)

	return nil
}

// publishEvent notifica sobre a mudança de status do pedido.
//
// Implementação atual: log estruturado.
// TODO Fase 7 — substituir por publicação NATS:
//
//	nc.Publish("senderzz.orders.status_changed", payload)
//
// O evento deve conter: order_id, status_de, status_para, actor_id, timestamp.
func publishEvent(ctx context.Context, orderID int64, from, to string, actorID int64) {
	// Stub NATS — descomentar e implementar na Fase 7 (event bus).
	// payload, _ := json.Marshal(map[string]any{
	//     "order_id":   orderID,
	//     "status_de":  from,
	//     "status_para": to,
	//     "actor_id":   actorID,
	//     "ts":         time.Now().UTC(),
	// })
	// if err := nc.Publish("senderzz.orders.status_changed", payload); err != nil {
	//     slog.Error("[statemachine] falha ao publicar evento NATS", "order_id", orderID, "err", err)
	// }

	slog.Info("[statemachine/evento] status_changed",
		"order_id", orderID,
		"de", from,
		"para", to,
		"actor_id", actorID,
	)
	_ = ctx // usado quando NATS for ativado
}
