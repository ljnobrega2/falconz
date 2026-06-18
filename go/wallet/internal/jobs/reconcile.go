// Package jobs implementa workers Asynq para tarefas assíncronas da carteira.
//
// ReconcileTask faz duas verificações diárias:
//
//  1. Divergência de saldo — compara a soma das transações confirmadas em
//     tpc_transacoes com tpc_carteira.saldo por usuário. Divergências são
//     logadas com slog.Error para alerta operacional. Não corrige automaticamente
//     para evitar mascarar bugs — a correção exige revisão humana.
//
//  2. Recargas expiradas — marca como 'expirado' as linhas de tpc_recargas
//     onde status='pendente' AND expires_at < NOW(). Espelha a lógica de
//     tpc_expirar_recargas_antigas() em pix.php.
//
// Configuração:
//   - ASYNQ_REDIS_ADDR — endereço do Redis para Asynq (default: localhost:6379)
//   - ME_TOKEN         — token OAuth Melhor Envio (reservado para expansão futura)
package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TypeReconcile é o tipo de tarefa Asynq para reconciliação diária.
const TypeReconcile = "wallet:reconcile"

// reconcilePayload é o payload da tarefa (vazio por ora — sem parâmetros necessários).
type reconcilePayload struct {
	// Reservado para futura parametrização (ex: reconciliar só um user_id específico).
}

// ReconcileTask agrupa as dependências do worker de reconciliação.
type ReconcileTask struct {
	Pool        *pgxpool.Pool
	MEAPIToken  string // token ME para expansão futura (consultar saldo na API)
}

// NewReconcileTask cria um ReconcileTask com o pool de conexões fornecido.
func NewReconcileTask(pool *pgxpool.Pool) *ReconcileTask {
	return &ReconcileTask{
		Pool:       pool,
		MEAPIToken: os.Getenv("ME_TOKEN"),
	}
}

// ProcessReconcile é o handler Asynq chamado quando a tarefa TypeReconcile é processada.
//
// Executa em sequência:
//  1. reconciliarSaldos      — detecta divergências carteira vs ledger
//  2. expirarRecargasPendentes — marca recargas vencidas como 'expirado'
func (t *ReconcileTask) ProcessReconcile(ctx context.Context, task *asynq.Task) error {
	var payload reconcilePayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		// Payload vazio é válido — não é erro.
		slog.Debug("[tpc_reconcile] payload vazio ou inválido", "err", err)
	}

	slog.Info("[tpc_reconcile] iniciando reconciliação diária")

	// ── 1. Reconcilia saldos (ledger vs carteira) ────────────────────────────
	divergencias, err := t.reconciliarSaldos(ctx)
	if err != nil {
		slog.Error("[tpc_reconcile] falha ao reconciliar saldos", "err", err)
		// Não retorna erro — continua para expirar recargas mesmo se esta etapa falhar.
	} else {
		slog.Info("[tpc_reconcile] reconciliação de saldos concluída",
			"divergencias_encontradas", divergencias)
	}

	// ── 2. Expira recargas pendentes vencidas ────────────────────────────────
	expiradas, err := t.expirarRecargasPendentes(ctx)
	if err != nil {
		slog.Error("[tpc_reconcile] falha ao expirar recargas", "err", err)
		return fmt.Errorf("expirar recargas: %w", err)
	}

	slog.Info("[tpc_reconcile] recargas expiradas marcadas",
		"total_expiradas", expiradas)

	return nil
}

// reconciliarSaldos compara a soma das transações confirmadas com o saldo
// registrado em tpc_carteira para cada usuário.
//
// Lógica:
//   saldo_calculado = SUM(valor) WHERE tipo='credito' AND status='confirmado'
//                   - SUM(valor) WHERE tipo IN ('debito','reserva') AND status='confirmado'
//
// Qualquer diferença > R$ 0,01 é considerada divergência e logada como erro.
// Retorna o número de usuários com divergência encontrados.
func (t *ReconcileTask) reconciliarSaldos(ctx context.Context) (int, error) {
	// Query que calcula saldo esperado vs saldo atual para todos os usuários.
	rows, err := t.Pool.Query(ctx,
		`SELECT
		     c.user_id,
		     c.saldo                                          AS saldo_carteira,
		     COALESCE(SUM(
		         CASE
		             WHEN tx.tipo = 'credito' AND tx.status = 'confirmado' THEN tx.valor
		             WHEN tx.tipo IN ('debito','reserva') AND tx.status = 'confirmado' THEN -tx.valor
		             ELSE 0
		         END
		     ), 0)                                           AS saldo_calculado,
		     ABS(c.saldo - COALESCE(SUM(
		         CASE
		             WHEN tx.tipo = 'credito' AND tx.status = 'confirmado' THEN tx.valor
		             WHEN tx.tipo IN ('debito','reserva') AND tx.status = 'confirmado' THEN -tx.valor
		             ELSE 0
		         END
		     ), 0))                                         AS diferenca
		 FROM tpc_carteira c
		 LEFT JOIN tpc_transacoes tx ON tx.user_id = c.user_id
		 GROUP BY c.user_id, c.saldo
		 HAVING ABS(c.saldo - COALESCE(SUM(
		     CASE
		         WHEN tx.tipo = 'credito' AND tx.status = 'confirmado' THEN tx.valor
		         WHEN tx.tipo IN ('debito','reserva') AND tx.status = 'confirmado' THEN -tx.valor
		         ELSE 0
		     END
		 ), 0)) > 0.01`,
	)
	if err != nil {
		return 0, fmt.Errorf("query divergências: %w", err)
	}
	defer rows.Close()

	var divergencias int
	for rows.Next() {
		var userID int64
		var saldoCarteira, saldoCalculado, diferenca string
		if err := rows.Scan(&userID, &saldoCarteira, &saldoCalculado, &diferenca); err != nil {
			slog.Error("[tpc_reconcile] erro ao ler linha de divergência", "err", err)
			continue
		}

		// Loga como erro para que sistemas de observabilidade (Datadog, CloudWatch)
		// possam gerar alertas. Não corrige automaticamente.
		slog.Error("[tpc_reconcile] DIVERGÊNCIA DE SALDO DETECTADA",
			"user_id", userID,
			"saldo_em_carteira", saldoCarteira,
			"saldo_calculado_pelo_ledger", saldoCalculado,
			"diferenca_brl", diferenca,
			"acao", "revisão manual necessária — verificar tpc_transacoes vs tpc_carteira",
		)
		divergencias++
	}

	if err := rows.Err(); err != nil {
		return divergencias, fmt.Errorf("iteração rows divergências: %w", err)
	}

	return divergencias, nil
}

// expirarRecargasPendentes marca como 'expirado' todas as recargas
// em status='pendente' cujo expires_at já passou.
//
// Espelha tpc_expirar_recargas_antigas() em pix.php.
// Retorna o número de linhas atualizadas.
func (t *ReconcileTask) expirarRecargasPendentes(ctx context.Context) (int64, error) {
	tag, err := t.Pool.Exec(ctx,
		`UPDATE tpc_recargas
		 SET status = 'expirado'
		 WHERE status = 'pendente'
		   AND expires_at < NOW()`,
	)
	if err != nil {
		return 0, fmt.Errorf("update recargas expiradas: %w", err)
	}

	expiradas := tag.RowsAffected()
	if expiradas > 0 {
		slog.Info("[tpc_reconcile] recargas PIX expiradas marcadas",
			"count", expiradas)
	}

	return expiradas, nil
}

// ScheduleReconcile enfileira a tarefa de reconciliação no Asynq.
//
// Deve ser chamado uma vez ao iniciar o servidor para registrar a tarefa
// na fila. O Asynq scheduler (configurado externamente via cron) dispara
// a tarefa periodicamente (ex: diariamente às 03:00 BRT).
//
// Para uso como cron: configure o asynq.Scheduler no main.go com:
//   scheduler.Register("0 6 * * *", asynq.NewTask(jobs.TypeReconcile, nil))
// (06:00 UTC = 03:00 BRT)
func ScheduleReconcile(client *asynq.Client) error {
	payload, err := json.Marshal(reconcilePayload{})
	if err != nil {
		return fmt.Errorf("serializar payload reconcile: %w", err)
	}

	task := asynq.NewTask(TypeReconcile, payload,
		asynq.TaskID("wallet:reconcile:daily"), // ID fixo — garante no-duplicata na fila
		asynq.MaxRetry(2),
		asynq.Queue("critical"),
	)

	info, err := client.Enqueue(task)
	if err != nil {
		return fmt.Errorf("enfileirar tarefa reconcile: %w", err)
	}

	slog.Info("[tpc_reconcile] tarefa enfileirada",
		"task_id", info.ID,
		"queue", info.Queue,
	)
	return nil
}
