// Package db gerencia o pool de conexões Postgres para o serviço Labels/Melhor Envio.
//
// Variável de ambiente esperada: DATABASE_URL (formato DSN pgx / libpq)
// Exemplo: postgres://user:pass@localhost:5432/senderzz_db
//
// Tabelas principais (criadas por infra/postgres/schema-labels.sql):
//   - wc_me_labels          (etiquetas de envio por pedido WooCommerce)
//   - wc_me_queue           (fila durável de jobs assíncronos)
//   - wc_me_shipment_cache  (cache de 10 min das cotações ME)
package db

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool é o pool de conexões compartilhado.
// Inicializado em Connect() e utilizado por todos os handlers via injeção de dependência.
var Pool *pgxpool.Pool

// Connect cria e valida o pool de conexões usando DATABASE_URL.
// Deve ser chamado uma única vez durante a inicialização do servidor.
// Retorna erro imediato se DATABASE_URL não estiver definida ou se o banco
// estiver inacessível — fail-fast na inicialização.
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
