module github.com/senderzz/affiliates-service

go 1.22

require (
	github.com/go-chi/chi/v5 v5.0.12
	github.com/golang-jwt/jwt/v5 v5.2.1
	github.com/hibiken/asynq v0.24.1
	github.com/jackc/pgx/v5 v5.5.5
	github.com/shopspring/decimal v1.3.1
)

// Nota: asynq declarado para Fase 3 mas sem workers implementados ainda.
// Executar `go mod tidy` após implementar o primeiro job em internal/jobs/.
// Até lá, tidy irá remover asynq — readicionar manualmente se necessário antes de jobs.
