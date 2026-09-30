// CreditarEstornosVencidos — AUDIT-2026-07-28 (dono): cancelamento de
// expedição registra o estorno como PENDENTE (EstornarPendente, ver
// internal.go) e só credita de fato depois que o reembolso cai na conta pool
// da Melhor Envio. A API da ME não expõe status/webhook de "reembolso
// confirmado" (checado em docs.melhorenvio.com.br/reference/cancelamento-de-etiquetas
// — só documenta "se aprovado, o estorno ocorre em 12 horas", sem status
// consultável). Sem sinal em tempo real, a janela segura é uma folga
// generosa sobre o prazo documentado: 24h (dobro das 12h prometidas pela ME).
//
// Ops ainda pode confirmar manualmente antes disso via admin
// (POST /wallet/estornos-pendentes/{id}/confirmar) quando checar o saldo real
// na ME — este job só cobre o caso comum (ninguém confirmou a tempo).
package jobs

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
)

const TypeCreditarEstornosVencidos = "wallet:creditar-estornos-vencidos"

// janela de segurança sobre o prazo de 12h documentado pela Melhor Envio.
const estornoJanelaSeguraHoras = 24

type EstornoReconcileTask struct {
	Pool *pgxpool.Pool
}

func NewEstornoReconcileTask(pool *pgxpool.Pool) *EstornoReconcileTask {
	return &EstornoReconcileTask{Pool: pool}
}

func (t *EstornoReconcileTask) ProcessCreditarEstornosVencidos(ctx context.Context, _ *asynq.Task) error {
	n, err := t.CreditarVencidos(ctx)
	if err != nil {
		slog.Error("[tpc_estorno_reconcile] falha ao creditar estornos vencidos", "err", err)
		return err
	}
	if n > 0 {
		slog.Info("[tpc_estorno_reconcile] estornos vencidos creditados automaticamente", "total", n)
	}
	return nil
}

// CreditarVencidos credita (saldo += valor, status pendente→confirmado) todo
// estorno de cancelamento de etiqueta pendente há mais de estornoJanelaSeguraHoras.
// Cada linha processada na própria transação (isolamento por usuário, uma
// falha não trava as demais).
func (t *EstornoReconcileTask) CreditarVencidos(ctx context.Context) (int, error) {
	rows, err := t.Pool.Query(ctx,
		fmt.Sprintf(`SELECT id, user_id, valor FROM tpc_transacoes
		  WHERE tipo = 'credito' AND status = 'pendente'
		    AND referencia LIKE 'cancel_label_%%'
		    AND created_at < NOW() - INTERVAL '%d hours'
		  ORDER BY id ASC`, estornoJanelaSeguraHoras),
	)
	if err != nil {
		return 0, fmt.Errorf("consultar estornos vencidos: %w", err)
	}
	type pending struct {
		id     int64
		userID int64
		valor  string
	}
	var list []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.userID, &p.valor); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan estorno vencido: %w", err)
		}
		list = append(list, p)
	}
	rows.Close()

	credited := 0
	for _, p := range list {
		tx, err := t.Pool.Begin(ctx)
		if err != nil {
			slog.Error("[tpc_estorno_reconcile] falha ao iniciar transação", "tx_id", p.id, "err", err)
			continue
		}
		var updated int64
		err = tx.QueryRow(ctx,
			`UPDATE tpc_transacoes SET status = 'confirmado'
			  WHERE id = $1 AND status = 'pendente'
			  RETURNING id`,
			p.id,
		).Scan(&updated)
		if err != nil {
			tx.Rollback(ctx) //nolint:errcheck
			// Já confirmado por ops nesse meio-tempo — não é erro.
			continue
		}
		if _, err := tx.Exec(ctx,
			`UPDATE tpc_carteira SET saldo = saldo + $2::numeric WHERE user_id = $1`,
			p.userID, p.valor,
		); err != nil {
			tx.Rollback(ctx) //nolint:errcheck
			slog.Error("[tpc_estorno_reconcile] falha ao creditar carteira", "tx_id", p.id, "user_id", p.userID, "err", err)
			continue
		}
		if _, err := tx.Exec(ctx,
			`UPDATE tpc_transacoes SET saldo_apos = (SELECT saldo FROM tpc_carteira WHERE user_id = $1) WHERE id = $2`,
			p.userID, p.id,
		); err != nil {
			tx.Rollback(ctx) //nolint:errcheck
			slog.Error("[tpc_estorno_reconcile] falha ao atualizar saldo_apos", "tx_id", p.id, "err", err)
			continue
		}
		if err := tx.Commit(ctx); err != nil {
			slog.Error("[tpc_estorno_reconcile] falha ao confirmar transação", "tx_id", p.id, "err", err)
			continue
		}
		slog.Info("[tpc_estorno_reconcile] estorno creditado automaticamente (janela de segurança vencida)",
			"tx_id", p.id, "user_id", p.userID, "valor", p.valor)
		credited++
	}
	return credited, nil
}
