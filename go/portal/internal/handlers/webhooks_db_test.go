// Teste de INTEGRAÇÃO (Postgres real) que TRAVA o caminho 200 do Create de webhook
// de COD (eventos motoboy_*) + persistência/leitura de `tipo` e `product_id`.
//
// REGRESSÃO COBERTA: antes da migração 425-webhook-tipo-produto.sql, o Create
// rejeitava eventos motoboy_* com 400 (allowedEventTypes só tinha order_status_*),
// o que forçava o frontend a bloquear COD. Este teste prova que:
//   1. POST /portal/webhooks com event_type=motoboy_em_rota, tipo=cod, product_id=X
//      → 200 (não 400) e devolve o id criado.
//   2. A linha persiste tipo='cod' e product_id=X no banco.
//   3. GET /portal/webhooks devolve tipo e product_id do webhook criado.
//
// Gate: pula (t.Skip) quando DATABASE_URL não está setada — mesma convenção do
// lgpd_db_test.go. Isolamento: user_id sintético de namespace ALTO; o webhook é
// removido por CASCADE quando o portal_user sintético é deletado em t.Cleanup.
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/portal-service/internal/auth"
)

// requireWebhookDB abre um pool contra DATABASE_URL ou pula o teste se ausente.
func requireWebhookDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL não definida — pulando teste de integração de webhook COD")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("falha ao criar pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("DATABASE_URL definida mas banco inacessível: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// authWebhookReq monta um POST/GET com PortalUser (ID load-bearing: Create/List
// escopam por u.ID) no contexto, simulando o pós-AuthPortalJWT.
func authWebhookReq(method string, id int64, body string) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, "/portal/webhooks", nil)
	} else {
		r = httptest.NewRequest(method, "/portal/webhooks", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	u := &auth.PortalUser{ID: id, WPUserID: id, Email: "syn@webhook.test", Role: "produtor"}
	return r.WithContext(auth.ContextWithUser(context.Background(), u))
}

// TestCreateWebhookCODDBPersistsTipoProduct: webhook de COD (motoboy_em_rota,
// tipo=cod, product_id=X) → 200 + persiste tipo/product_id + GET os devolve.
func TestCreateWebhookCODDBPersistsTipoProduct(t *testing.T) {
	pool := requireWebhookDB(t)
	ctx := context.Background()

	// Usuário sintético do portal (FK de senderzz_portal_webhooks.user_id).
	var uid int64
	synEmail := "webhook-cod-test-991000001@webhook.test"
	if err := pool.QueryRow(ctx,
		`INSERT INTO senderzz_portal_users (email, nome, role, ativo)
		 VALUES ($1, 'Webhook COD Test', 'produtor', true)
		 RETURNING id`, synEmail,
	).Scan(&uid); err != nil {
		t.Fatalf("falha ao inserir portal_user sintético: %v", err)
	}
	// CASCADE: deletar o portal_user remove o webhook (fk_webhooks_user ON DELETE CASCADE).
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM senderzz_portal_users WHERE id = $1`, uid)
	})

	h := &WebhookHandler{Pool: pool}

	const wantProductID int64 = 778899
	// URL com host PÚBLICO resolvível: o Create agora valida egress (P0 SSRF) e
	// rejeita hosts não-resolvíveis/internos. example.com (RFC 2606) resolve p/ IP
	// público — o foco deste teste é a persistência de tipo/product_id, não a URL.
	body := `{"url":"https://example.com/cod-hook","secret":"s3cr3t",` +
		`"event_types":["motoboy_em_rota"],"tipo":"cod","product_id":778899}`

	// 1) Create COD → 200 (antes da migração/whitelist isto dava 400).
	rec := httptest.NewRecorder()
	h.Create(rec, authWebhookReq(http.MethodPost, uid, body))
	if rec.Code != http.StatusOK {
		t.Fatalf("Create(COD motoboy_em_rota, tipo=cod) = %d, esperado 200; body=%s", rec.Code, rec.Body.String())
	}

	var createResp struct {
		OK bool  `json:"ok"`
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("resposta do Create não é JSON: %v; body=%s", err, rec.Body.String())
	}
	if !createResp.OK || createResp.ID <= 0 {
		t.Fatalf("Create devolveu ok=%v id=%d, esperado ok=true id>0", createResp.OK, createResp.ID)
	}

	// 2) A linha persiste tipo='cod' e product_id=X.
	var gotTipo string
	var gotProduct *int64
	if err := pool.QueryRow(ctx,
		`SELECT tipo, product_id FROM senderzz_portal_webhooks WHERE id = $1`,
		createResp.ID,
	).Scan(&gotTipo, &gotProduct); err != nil {
		t.Fatalf("falha ao reler webhook persistido: %v", err)
	}
	if gotTipo != "cod" {
		t.Errorf("tipo persistido = %q, esperado 'cod'", gotTipo)
	}
	if gotProduct == nil || *gotProduct != wantProductID {
		t.Errorf("product_id persistido = %v, esperado %d", gotProduct, wantProductID)
	}

	// 3) GET /portal/webhooks devolve tipo e product_id.
	recList := httptest.NewRecorder()
	h.List(recList, authWebhookReq(http.MethodGet, uid, ""))
	if recList.Code != http.StatusOK {
		t.Fatalf("List = %d, esperado 200; body=%s", recList.Code, recList.Body.String())
	}
	var listResp struct {
		OK   bool `json:"ok"`
		Data []struct {
			ID        int64    `json:"id"`
			Tipo      string   `json:"tipo"`
			ProductID *int64   `json:"product_id"`
			EventTypes []string `json:"event_types"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recList.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("resposta do List não é JSON: %v; body=%s", err, recList.Body.String())
	}
	var found bool
	for _, wh := range listResp.Data {
		if wh.ID != createResp.ID {
			continue
		}
		found = true
		if wh.Tipo != "cod" {
			t.Errorf("List tipo = %q, esperado 'cod'", wh.Tipo)
		}
		if wh.ProductID == nil || *wh.ProductID != wantProductID {
			t.Errorf("List product_id = %v, esperado %d", wh.ProductID, wantProductID)
		}
		if len(wh.EventTypes) != 1 || wh.EventTypes[0] != "motoboy_em_rota" {
			t.Errorf("List event_types = %v, esperado [motoboy_em_rota]", wh.EventTypes)
		}
	}
	if !found {
		t.Errorf("webhook COD id=%d não apareceu no List", createResp.ID)
	}
}
