// Package db gerencia o pool de conexões Postgres para o serviço Afiliados.
//
// Variável de ambiente esperada: DATABASE_URL (formato DSN pgx / libpq)
// Exemplo: postgres://user:pass@localhost:5432/senderzz_db
//
// Tabelas principais (schema-affiliates.sql):
//   - senderzz_affiliates
//   - senderzz_affiliate_links
//   - senderzz_affiliate_commissions
//   - senderzz_affiliate_invites
//   - senderzz_cod_wallet
//   - senderzz_cod_ledger
//   - wp_senderzz_portal_sessions (para validação de sessão portal — prefixo wp_ preservado)
package db

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool é o pool de conexões compartilhado.
// Inicializado em Connect() e utilizado por todos os handlers via injeção de dependência.
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

	// AUDIT PERF-pool-sizing-inconsistent (IMPROVEMENT-PLAN-2026-06-18 P1-07):
	// tuning explícito do pool, igual a admin/portal. Antes este serviço usava o
	// default do pgx (≈max(4,NumCPU) MaxConns, sem min/idle/healthcheck), o que em
	// pico podia esfomear conexões no mesmo Postgres compartilhado. Só config de
	// pool — não toca lógica de ledger/comissão.
	cfg.MaxConns = 20
	cfg.MinConns = 2
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = 30 * time.Second

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
