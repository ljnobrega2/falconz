module github.com/senderzz/motoboy-service

go 1.22

require (
	github.com/go-chi/chi/v5 v5.0.12
	github.com/hibiken/asynq v0.24.1
	github.com/jackc/pgx/v5 v5.5.5
	github.com/golang-jwt/jwt/v5 v5.2.1
	golang.org/x/crypto v0.22.0
)

// Nota: asynq está declarado aqui para Fase 1 mas ainda não tem handlers de job.
// Executar `go mod tidy` depois de implementar o primeiro worker em internal/jobs/.
// Até lá, tidy irá remover asynq — readicionar manualmente se necessário antes de jobs.
