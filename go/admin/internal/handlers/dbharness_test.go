// Harness compartilhado dos testes de INTEGRAÇÃO (com DB) do pacote handlers.
//
// Os *_test.go antigos (cod_wallet/order_detail/parse_helpers) são PUROS — não
// tocam banco. Estes helpers existem para os primeiros testes DB-backed
// (config_taxas_test.go, stock_bipar_test.go) e dão:
//
//   - testPoolOrSkip — pool pgx a partir de DATABASE_URL; SKIP silencioso se a env
//     estiver ausente/inacessível (mantém `go test` verde em máquina sem banco).
//   - mintAdmin      — insere um admin SINTÉTICO (id alto ≥ 990000000, OVERRIDING
//     SYSTEM VALUE pois id é GENERATED ALWAYS) e devolve um JWT válido emitido por
//     auth.IssueToken (iss=senderzz-admin, exp 12h — o que o auth.Middleware exige).
//     A linha é removida em t.Cleanup. O caller DEVE ter setado ADMIN_JWT_SECRET/
//     JWT_SECRET ANTES (auth.secret() lê o env no momento da assinatura/validação).
package handlers

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/auth"
)

var (
	dbPoolOnce sync.Once
	dbPool     *pgxpool.Pool
)

// testPoolOrSkip devolve um pool compartilhado (criado uma vez por processo de
// teste) ou pula o teste se DATABASE_URL não estiver configurada/acessível.
func testPoolOrSkip(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbPoolOnce.Do(func() {
		dsn := strings.TrimSpace(os.Getenv("DATABASE_URL"))
		if dsn == "" {
			return
		}
		cfg, err := pgxpool.ParseConfig(dsn)
		if err != nil {
			return
		}
		p, err := pgxpool.NewWithConfig(context.Background(), cfg)
		if err != nil {
			return
		}
		if err := p.Ping(context.Background()); err != nil { // conectividade real
			p.Close()
			return
		}
		dbPool = p
	})
	if dbPool == nil {
		t.Skip("DATABASE_URL ausente/inacessível — pulando teste DB-backed")
	}
	return dbPool
}

// mintAdmin insere um admin sintético ativo e devolve um JWT (sem o prefixo Bearer).
// O caller DEVE ter setado ADMIN_JWT_SECRET/JWT_SECRET antes (t.Setenv).
func mintAdmin(t *testing.T, pool *pgxpool.Pool, id int64, email string) string {
	t.Helper()
	ctx := context.Background()
	// Limpa resíduo de execução anterior interrompida; insere com id explícito.
	_, _ = pool.Exec(ctx, `DELETE FROM senderzz_admin_users WHERE id=$1`, id)
	if _, err := pool.Exec(ctx,
		`INSERT INTO senderzz_admin_users (id, email, nome, password_hash, role, ativo)
		 OVERRIDING SYSTEM VALUE
		 VALUES ($1, $2, 'Teste Admin', 'x', 'admin', TRUE)`,
		id, email); err != nil {
		t.Fatalf("mintAdmin insert: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM senderzz_admin_users WHERE id=$1`, id)
	})
	tok, err := auth.IssueToken(auth.Admin{ID: id, Email: email, Nome: "Teste Admin"})
	if err != nil {
		t.Fatalf("mintAdmin IssueToken: %v", err)
	}
	return tok
}
