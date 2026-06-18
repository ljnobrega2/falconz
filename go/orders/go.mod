module github.com/senderzz/orders-service

go 1.22

require (
	github.com/go-chi/chi/v5 v5.0.12
	github.com/golang-jwt/jwt/v5 v5.2.1
	github.com/hibiken/asynq v0.24.1
	github.com/jackc/pgx/v5 v5.5.5
	github.com/shopspring/decimal v1.3.1
)

// asynq: usado em internal/jobs para enfileirar ProcessOrderExpiry e ProcessStatusSync.
// Requer Redis (REDIS_URL) no ambiente de produção.
// Executar `go mod tidy` após implementar todos os workers para atualizar go.sum.
