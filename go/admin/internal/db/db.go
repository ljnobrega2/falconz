package db

import (
	"context"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// New cria o pool pgx do serviço admin.
//
// AUDIT PERF-pool-sizing-inconsistent: tuning padronizado do pool entre os
// serviços Go (admin era o único que setava MaxConns; agora fixa o padrão alvo).
//   - MaxConns=20         → teto de conexões simultâneas (workload admin/dashboard).
//   - MinConns=2          → mantém conexões quentes p/ evitar latência de cold start.
//   - MaxConnLifetime=30m → recicla conexões (evita conexões zumbis no Postgres).
//   - MaxConnIdleTime=5m  → libera conexões ociosas, devolvendo recursos ao banco.
//   - HealthCheckPeriod=30s → poda conexões mortas antes de serem entregues.
func New(ctx context.Context) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 20
	cfg.MinConns = 2
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = 30 * time.Second
	return pgxpool.NewWithConfig(ctx, cfg)
}
