module github.com/senderzz/motoboy-service

go 1.22

require (
	github.com/go-chi/chi/v5 v5.0.12
	github.com/golang-jwt/jwt/v5 v5.2.1
	github.com/hibiken/asynq v0.24.1
	github.com/jackc/pgx/v5 v5.5.5
	golang.org/x/crypto v0.22.0
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20221227161230-091c0ba34f0a // indirect
	github.com/jackc/puddle/v2 v2.2.1 // indirect
	golang.org/x/sync v0.1.0 // indirect
	golang.org/x/text v0.14.0 // indirect
)

// Nota: asynq está declarado aqui para Fase 1 mas ainda não tem handlers de job.
// Executar `go mod tidy` depois de implementar o primeiro worker em internal/jobs/.
// Até lá, tidy irá remover asynq — readicionar manualmente se necessário antes de jobs.
