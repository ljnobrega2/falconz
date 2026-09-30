// orders_test.go — AUDIT TEST-ORDERS-REFUND-UNGUARDED.
//
// Valida o gate de cancelamento de PostOrderCancel: um pedido PAGO não pode ser
// cancelado por esta rota (ficaria 'cancelled' terminal com payment_status='paid'
// e SEM rota cancelled→reembolsado). Espelha o CRIT-02 do PHP e o achado do auditor.
//
// Estratégia: teste de integração HTTP que passa pelo middleware real auth.AuthJWT
// (JWT HS256 mintado no teste) + handler real, contra um Postgres real. É DB-gated:
// sem DATABASE_URL o teste é SKIPADO (go test local sem banco continua verde); no CI
// (.github/workflows/go-services.yml job test-orders) o Postgres + schema-orders.sql
// estão presentes e o teste roda por completo.
package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/orders-service/internal/auth"
)

const testJWTSecret = "test-jwt-secret-ci"

// mintTestJWT cria um Bearer HS256 válido com sub=userID e role opcional,
// no MESMO formato que o minter PHP (claims sub/iat/exp). exp obrigatório
// (AuthJWT usa WithExpirationRequired).
func mintTestJWT(t *testing.T, userID int64, role string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"sub": userID,
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(1 * time.Hour).Unix(),
	}
	if role != "" {
		claims["role"] = role
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString([]byte(testJWTSecret))
	if err != nil {
		t.Fatalf("falha ao assinar JWT de teste: %v", err)
	}
	return signed
}

const testAdminJWTSecret = "test-admin-jwt-secret-ci"

// mintTestAdminJWT cria um Bearer HS256 de ADMIN válido: claim iss=senderzz-admin
// + role=admin, assinado com ADMIN_JWT_SECRET. Espelha go/admin auth.IssueToken.
// AuthJWT só honra role=admin quando iss==senderzz-admin E a assinatura casa com
// ADMIN_JWT_SECRET (SEC-GO-04) — um token assinado com JWT_SECRET afirmando admin
// é rebaixado. Por isso este minter usa secret/iss próprios.
//
// IMPORTANTE: o teste que usar este token deve setar JWT_SECRET (não-vazio) E
// ADMIN_JWT_SECRET, pois AuthJWT faz o gate 503 sobre JWT_SECRET vazio ANTES de
// ramificar para o secret de admin.
func mintTestAdminJWT(t *testing.T, userID int64) string {
	t.Helper()
	claims := jwt.MapClaims{
		"sub":  userID,
		"role": "admin",
		"iss":  "senderzz-admin",
		"iat":  time.Now().Unix(),
		"exp":  time.Now().Add(1 * time.Hour).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString([]byte(testAdminJWTSecret))
	if err != nil {
		t.Fatalf("falha ao assinar JWT admin de teste: %v", err)
	}
	return signed
}

// testOrdersRouter monta um router mínimo com a rota de cancelamento sob o
// middleware real AuthJWT — exatamente como em cmd/server/main.go.
func testOrdersRouter(pool *pgxpool.Pool) http.Handler {
	h := NewOrderHandler(pool)
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(auth.AuthJWT)
		r.Post("/orders/{id}/cancel", h.PostOrderCancel)
	})
	return r
}

// openTestDB conecta ao Postgres de teste ou SKIPA o teste se DATABASE_URL ausente.
func openTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dsn == "" {
		t.Skip("DATABASE_URL ausente — teste de integração de cancelamento pulado (roda no CI)")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("falha ao conectar ao Postgres de teste: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Fatalf("Postgres de teste inacessível: %v", err)
	}
	return pool
}

// seedOrderSeq garante order_number único e curto (≤20 chars, limite do schema).
var seedOrderSeq atomic.Int64

// seedOrder insere um pedido de teste e devolve seu id. Limpa via t.Cleanup.
func seedOrder(t *testing.T, pool *pgxpool.Pool, userID int64, status, paymentStatus string) int64 {
	t.Helper()
	ctx := context.Background()
	// order_number único por execução, dentro de VARCHAR(20).
	// Ex.: "SZTST-7f3a1c-3" (epoch nanos em base36 + contador).
	orderNumber := "SZTST-" +
		strconv.FormatInt(time.Now().UnixNano()%0x1000000, 36) + "-" +
		strconv.FormatInt(seedOrderSeq.Add(1), 10)
	var id int64
	err := pool.QueryRow(ctx,
		`INSERT INTO sz_orders (order_number, user_id, produtor_id, status, payment_status, total)
		 VALUES ($1, $2, $2, $3, $4, 100.00) RETURNING id`,
		orderNumber, userID, status, paymentStatus,
	).Scan(&id)
	if err != nil {
		t.Fatalf("falha ao inserir pedido de teste: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM sz_order_status_history WHERE order_id = $1`, id)
		_, _ = pool.Exec(context.Background(), `DELETE FROM sz_orders WHERE id = $1`, id)
	})
	return id
}

// doCancel executa POST /orders/{id}/cancel com o token informado e devolve o status HTTP.
func doCancel(t *testing.T, router http.Handler, orderID int64, token string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost,
		"/orders/"+strconv.FormatInt(orderID, 10)+"/cancel", strings.NewReader(`{"motivo":"teste"}`))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec.Code
}

// TestPostOrderCancel_PaidOrderRejected: o cerne do achado — pedido PAGO em status
// cancelável (pending) deve receber 409 (não pode cancelar; usar reembolso).
func TestPostOrderCancel_PaidOrderRejected(t *testing.T) {
	t.Setenv("JWT_SECRET", testJWTSecret)
	pool := openTestDB(t)
	defer pool.Close()
	router := testOrdersRouter(pool)

	const userID = int64(99001)
	orderID := seedOrder(t, pool, userID, "pending", "paid")
	token := mintTestJWT(t, userID, "")

	if code := doCancel(t, router, orderID, token); code != http.StatusConflict {
		t.Fatalf("pedido PAGO deveria retornar 409, retornou %d", code)
	}

	// O status NÃO pode ter mudado para 'cancelled'.
	var status string
	_ = pool.QueryRow(context.Background(),
		`SELECT status FROM sz_orders WHERE id = $1`, orderID).Scan(&status)
	if status != "pending" {
		t.Fatalf("pedido pago não deveria mudar de status; agora está %q", status)
	}
}

// TestPostOrderCancel_UnpaidPendingAllowed: pedido NÃO pago em pending é cancelado (200).
func TestPostOrderCancel_UnpaidPendingAllowed(t *testing.T) {
	t.Setenv("JWT_SECRET", testJWTSecret)
	pool := openTestDB(t)
	defer pool.Close()
	router := testOrdersRouter(pool)

	const userID = int64(99002)
	orderID := seedOrder(t, pool, userID, "pending", "pending")
	token := mintTestJWT(t, userID, "")

	if code := doCancel(t, router, orderID, token); code != http.StatusOK {
		t.Fatalf("pedido pendente não pago deveria cancelar (200), retornou %d", code)
	}
	var status string
	_ = pool.QueryRow(context.Background(),
		`SELECT status FROM sz_orders WHERE id = $1`, orderID).Scan(&status)
	if status != "cancelled" {
		t.Fatalf("pedido deveria estar 'cancelled', está %q", status)
	}
}

// TestPostOrderCancel_ProcessingClientForbidden: cliente (não admin) NÃO pode
// cancelar pedido em 'processing' (422) — espelha CRIT-02 (só pré-despacho).
func TestPostOrderCancel_ProcessingClientForbidden(t *testing.T) {
	t.Setenv("JWT_SECRET", testJWTSecret)
	pool := openTestDB(t)
	defer pool.Close()
	router := testOrdersRouter(pool)

	const userID = int64(99003)
	orderID := seedOrder(t, pool, userID, "processing", "pending")
	token := mintTestJWT(t, userID, "") // role vazio = cliente comum

	if code := doCancel(t, router, orderID, token); code != http.StatusUnprocessableEntity {
		t.Fatalf("cliente cancelando 'processing' deveria dar 422, deu %d", code)
	}
}

// TestPostOrderCancel_NotOwnerForbidden: usuário que não é dono nem produtor → 403.
func TestPostOrderCancel_NotOwnerForbidden(t *testing.T) {
	t.Setenv("JWT_SECRET", testJWTSecret)
	pool := openTestDB(t)
	defer pool.Close()
	router := testOrdersRouter(pool)

	orderID := seedOrder(t, pool, 99004, "pending", "pending")
	token := mintTestJWT(t, 99999, "") // outro usuário

	if code := doCancel(t, router, orderID, token); code != http.StatusForbidden {
		t.Fatalf("não-dono deveria dar 403, deu %d", code)
	}
}

// TestPostOrderCancel_NoToken: sem Bearer → 401 (AuthJWT).
func TestPostOrderCancel_NoToken(t *testing.T) {
	t.Setenv("JWT_SECRET", testJWTSecret)
	pool := openTestDB(t)
	defer pool.Close()
	router := testOrdersRouter(pool)

	orderID := seedOrder(t, pool, 99005, "pending", "pending")
	if code := doCancel(t, router, orderID, ""); code != http.StatusUnauthorized {
		t.Fatalf("sem token deveria dar 401, deu %d", code)
	}
}

// TestPostOrderCancel_AdminCancelsNonOwner: ADMIN cancela pedido de OUTRO usuário
// (não é dono nem produtor) → 200 + status vira 'cancelled' no banco. Prova que o
// gate de ownership é dispensado para admin. Pedido não-pago para não bater no 409.
func TestPostOrderCancel_AdminCancelsNonOwner(t *testing.T) {
	t.Setenv("JWT_SECRET", testJWTSecret)            // gate 503 exige JWT_SECRET não-vazio.
	t.Setenv("ADMIN_JWT_SECRET", testAdminJWTSecret) // token admin é verificado com este.
	pool := openTestDB(t)
	defer pool.Close()
	router := testOrdersRouter(pool)

	// Dono do pedido = 99010; admin (token) = 70001 — usuários distintos.
	orderID := seedOrder(t, pool, 99010, "pending", "pending")
	token := mintTestAdminJWT(t, 70001)

	if code := doCancel(t, router, orderID, token); code != http.StatusOK {
		t.Fatalf("admin cancelando pedido alheio deveria dar 200, deu %d", code)
	}
	var status string
	_ = pool.QueryRow(context.Background(),
		`SELECT status FROM sz_orders WHERE id = $1`, orderID).Scan(&status)
	if status != "cancelled" {
		t.Fatalf("pedido cancelado por admin deveria estar 'cancelled', está %q", status)
	}
}

// TestPostOrderCancel_AdminCancelsProcessing: ADMIN pode cancelar pedido em
// 'processing' (status que o handler adiciona à whitelist só para admin) → 200 +
// 'cancelled'. Par do TestPostOrderCancel_ProcessingClientForbidden (cliente=422),
// provando o ramo admin-only do conjunto cancelável.
func TestPostOrderCancel_AdminCancelsProcessing(t *testing.T) {
	t.Setenv("JWT_SECRET", testJWTSecret)
	t.Setenv("ADMIN_JWT_SECRET", testAdminJWTSecret)
	pool := openTestDB(t)
	defer pool.Close()
	router := testOrdersRouter(pool)

	orderID := seedOrder(t, pool, 99011, "processing", "pending")
	token := mintTestAdminJWT(t, 70002)

	if code := doCancel(t, router, orderID, token); code != http.StatusOK {
		t.Fatalf("admin cancelando 'processing' deveria dar 200, deu %d", code)
	}
	var status string
	_ = pool.QueryRow(context.Background(),
		`SELECT status FROM sz_orders WHERE id = $1`, orderID).Scan(&status)
	if status != "cancelled" {
		t.Fatalf("'processing' cancelado por admin deveria estar 'cancelled', está %q", status)
	}
}

// TestPostOrderCancel_DeliveredNotCancellable: pedido em status NÃO-permitido
// ('entregue') → 422 e o status NÃO muda no banco. Cobre o gate de whitelist
// (CRIT-02) na ponta "pós-despacho" e garante que nada foi mutado.
func TestPostOrderCancel_DeliveredNotCancellable(t *testing.T) {
	t.Setenv("JWT_SECRET", testJWTSecret)
	pool := openTestDB(t)
	defer pool.Close()
	router := testOrdersRouter(pool)

	const userID = int64(99012)
	orderID := seedOrder(t, pool, userID, "entregue", "pending")
	token := mintTestJWT(t, userID, "") // dono, cliente comum

	if code := doCancel(t, router, orderID, token); code != http.StatusUnprocessableEntity {
		t.Fatalf("cancelar 'entregue' deveria dar 422, deu %d", code)
	}
	var status string
	_ = pool.QueryRow(context.Background(),
		`SELECT status FROM sz_orders WHERE id = $1`, orderID).Scan(&status)
	if status != "entregue" {
		t.Fatalf("status não-cancelável NÃO deveria mudar; agora está %q", status)
	}
}

// TestPostOrderCancel_NotFound: pedido inexistente → 404. Usa um ID alto que não
// existe (sequência de teste nunca chega lá) com token válido do dono nominal.
func TestPostOrderCancel_NotFound(t *testing.T) {
	t.Setenv("JWT_SECRET", testJWTSecret)
	pool := openTestDB(t)
	defer pool.Close()
	router := testOrdersRouter(pool)

	const ghostID = int64(2147480000) // inexistente, dentro de int64
	// Garante que de fato não existe (defensivo contra dados residuais).
	_, _ = pool.Exec(context.Background(), `DELETE FROM sz_orders WHERE id = $1`, ghostID)

	token := mintTestJWT(t, 99013, "")
	if code := doCancel(t, router, ghostID, token); code != http.StatusNotFound {
		t.Fatalf("pedido inexistente deveria dar 404, deu %d", code)
	}
}

// TestPostOrderCancel_MotivoPersisted: o motivo enviado no body é persistido na
// linha de histórico da transição para 'cancelled'. doCancel envia {"motivo":"teste"}.
func TestPostOrderCancel_MotivoPersisted(t *testing.T) {
	t.Setenv("JWT_SECRET", testJWTSecret)
	pool := openTestDB(t)
	defer pool.Close()
	router := testOrdersRouter(pool)

	const userID = int64(99014)
	orderID := seedOrder(t, pool, userID, "pending", "pending")
	token := mintTestJWT(t, userID, "")

	if code := doCancel(t, router, orderID, token); code != http.StatusOK {
		t.Fatalf("cancelamento deveria dar 200, deu %d", code)
	}

	var motivo string
	err := pool.QueryRow(context.Background(),
		`SELECT motivo FROM sz_order_status_history
		  WHERE order_id = $1 AND status_para = 'cancelled'
		  ORDER BY id DESC LIMIT 1`, orderID).Scan(&motivo)
	if err != nil {
		t.Fatalf("falha ao ler histórico de cancelamento: %v", err)
	}
	if motivo != "teste" {
		t.Fatalf("motivo deveria ser 'teste' (do body), veio %q", motivo)
	}
}
