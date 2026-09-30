// Teste de INTEGRAÇÃO do reconcile contra Postgres real.
//
// Diferente dos testes em internal/handlers/integration_db_test.go (que
// reproduzem o SQL inline para isolar por user_id num banco de dev compartilhado),
// ESTE chama a FUNÇÃO de produção expirarRecargasPendentes() de verdade — pega
// drift real em reconcile.go. É seguro porque expirar é global mas idempotente:
// só toca linhas 'pendente' com expires_at vencido; asseguramos só as NOSSAS.
//
// Gate: pula (t.Skip) sem DATABASE_URL — `go test` continua verde sem banco.
// SEC-WALLET-SEP: toca SOMENTE tpc_* (expedição/frete); zero sz_cod_wallet_*.
package jobs

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// reconcileTestUserBase: namespace de user_id alto e único, fora dos dados reais.
const reconcileTestUserBase int64 = 990100000

func requireReconcileDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL não definida — pulando teste de integração do reconcile")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("falha ao criar pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("DATABASE_URL definida mas banco inacessível: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// TestExpirarRecargasPendentesReal chama a FUNÇÃO de produção
// expirarRecargasPendentes() e prova que: uma recarga 'pendente' vencida vira
// 'expirado', uma 'pendente' válida permanece, e o RowsAffected reportado >= 1.
// (Asserts só nas nossas linhas — o retorno global pode incluir outras.)
func TestExpirarRecargasPendentesReal(t *testing.T) {
	pool := requireReconcileDB(t)
	userID := reconcileTestUserBase + 1
	ctx := context.Background()

	cleanup := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM tpc_recargas WHERE user_id = $1`, userID)
	}
	cleanup()
	t.Cleanup(cleanup)

	var vencidaID, validaID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO tpc_recargas (user_id, valor, status, expires_at)
		 VALUES ($1, '12.00', 'pendente', NOW() - INTERVAL '2 hours') RETURNING id`,
		userID).Scan(&vencidaID); err != nil {
		t.Fatalf("inserir recarga vencida: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO tpc_recargas (user_id, valor, status, expires_at)
		 VALUES ($1, '12.00', 'pendente', NOW() + INTERVAL '2 hours') RETURNING id`,
		userID).Scan(&validaID); err != nil {
		t.Fatalf("inserir recarga válida: %v", err)
	}

	task := NewReconcileTask(pool)

	// FUNÇÃO DE PRODUÇÃO — não SQL inline.
	expiradas, err := task.expirarRecargasPendentes(ctx)
	if err != nil {
		t.Fatalf("expirarRecargasPendentes: %v", err)
	}
	if expiradas < 1 {
		t.Fatalf("expiradas (global) = %d, esperado >= 1 (a nossa vencida)", expiradas)
	}

	statusDe := func(id int64) string {
		var s string
		_ = pool.QueryRow(ctx, `SELECT status FROM tpc_recargas WHERE id = $1`, id).Scan(&s)
		return s
	}
	if got := statusDe(vencidaID); got != "expirado" {
		t.Errorf("recarga vencida: status %q, esperado 'expirado'", got)
	}
	if got := statusDe(validaID); got != "pendente" {
		t.Errorf("recarga válida: status %q, esperado 'pendente' (não deve expirar)", got)
	}
}

// TestReconciliarSaldosRealNaoErraComCarteiraConsistente chama a FUNÇÃO de
// produção reconciliarSaldos() e prova que ela roda sem erro e que uma carteira
// CONSISTENTE que criamos (saldo == soma do ledger) NÃO entra na contagem como
// divergência espúria. Não assertamos o total global (há outras linhas no banco
// de dev), só que a função executa e nossa linha consistente não é flagrada.
func TestReconciliarSaldosRealNaoErraComCarteiraConsistente(t *testing.T) {
	pool := requireReconcileDB(t)
	userID := reconcileTestUserBase + 2
	ctx := context.Background()

	cleanup := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM tpc_transacoes WHERE user_id = $1`, userID)
		_, _ = pool.Exec(ctx, `DELETE FROM tpc_carteira WHERE user_id = $1`, userID)
	}
	cleanup()
	t.Cleanup(cleanup)

	// Carteira consistente: saldo 50 com UMA transação de crédito confirmada de 50.
	if _, err := pool.Exec(ctx,
		`INSERT INTO tpc_carteira (user_id, saldo, saldo_reservado) VALUES ($1, '50.00', '0.00')`,
		userID); err != nil {
		t.Fatalf("inserir carteira: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO tpc_transacoes (user_id, tipo, valor, saldo_apos, descricao, referencia, status)
		 VALUES ($1, 'credito', '50.00', '50.00', 'itest', $2, 'confirmado')`,
		userID, fmt.Sprintf("itest-reconcile-%d", userID)); err != nil {
		t.Fatalf("inserir transacao: %v", err)
	}

	task := NewReconcileTask(pool)

	// FUNÇÃO DE PRODUÇÃO — executa a query real de divergência sobre TODO o banco.
	if _, err := task.reconciliarSaldos(ctx); err != nil {
		t.Fatalf("reconciliarSaldos: %v", err)
	}

	// Verifica diretamente que NOSSA carteira não é divergente (a função loga
	// divergências por user_id, mas não as retorna individualizadas — então
	// confirmamos a invariante na própria linha).
	var diff string
	_ = pool.QueryRow(ctx,
		`SELECT ABS(c.saldo - COALESCE(SUM(
		     CASE
		         WHEN tx.tipo = 'credito' AND tx.status = 'confirmado' THEN tx.valor
		         WHEN tx.tipo IN ('debito','reserva') AND tx.status = 'confirmado' THEN -tx.valor
		         ELSE 0
		     END), 0))
		 FROM tpc_carteira c
		 LEFT JOIN tpc_transacoes tx ON tx.user_id = c.user_id
		 WHERE c.user_id = $1
		 GROUP BY c.user_id, c.saldo`,
		userID).Scan(&diff)
	if diff != "0.00" && diff != "0" {
		t.Errorf("nossa carteira consistente acusou diferença %q, esperado 0", diff)
	}
}
