// Package db gerencia o pool de conexões Postgres para o serviço Motoboy.
//
// Variável de ambiente esperada: DATABASE_URL (formato DSN pgx / libpq)
// Exemplo: postgres://user:pass@localhost:5432/senderzz_db
//
// Tabelas principais (migradas de MySQL com pgloader):
//   - sz_motoboys
//   - sz_motoboy_pedidos
//   - sz_motoboy_cds
//   - sz_motoboy_zonas
//   - sz_motoboy_cep_zonas
//   - sz_motoboy_zona_pivot
//   - sz_motoboy_comprovantes
//   - sz_motoboy_fechamento
//   - sz_motoboy_audit
//   - senderzz_portal_sessions  (para AuthPortal)
package db

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool é o pool de conexões compartilhado.
// Inicializado em Connect() e utilizado por todos os handlers via injeção de contexto.
var Pool *pgxpool.Pool

// Connect cria e valida o pool de conexões usando DATABASE_URL.
// Deve ser chamado uma única vez durante a inicialização do servidor.
func Connect(ctx context.Context) (*pgxpool.Pool, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return nil, fmt.Errorf("[db] DATABASE_URL não definida")
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("[db] erro ao parsear DATABASE_URL: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("[db] falha ao criar pool: %w", err)
	}

	// Valida conexão imediatamente para falhar rápido na inicialização.
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("[db] banco inacessível: %w", err)
	}

	Pool = pool
	return pool, nil
}
