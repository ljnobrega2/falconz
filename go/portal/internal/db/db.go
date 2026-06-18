// Package db gerencia o pool de conexões Postgres para o serviço Portal.
//
// Variável de ambiente esperada: DATABASE_URL (formato DSN pgx / libpq)
// Exemplo: postgres://user:pass@localhost:5432/senderzz_db
//
// Tabelas principais (schema em infra/postgres/schema-portal.sql):
//   - senderzz_portal_users      (usuários: produtores, afiliados, OLs)
//   - senderzz_portal_sessions   (sessões autenticadas — token raw + HMAC)
//   - senderzz_portal_2fa        (códigos 2FA por e-mail, TTL 10 min)
//   - senderzz_portal_webhooks   (webhooks configurados pelo usuário)
//   - senderzz_webhook_log       (histórico de disparos)
//   - senderzz_integration_log   (log de eventos de integração)
package db

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool é o pool de conexões compartilhado.
// Inicializado em Connect() e utilizado pelos handlers via injeção de dependência.
var Pool *pgxpool.Pool

// Connect cria e valida o pool de conexões usando DATABASE_URL.
// Deve ser chamado uma única vez durante a inicialização do servidor.
// Falha rápido se o banco estiver inacessível.
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
