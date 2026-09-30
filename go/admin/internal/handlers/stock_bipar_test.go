// Testes de INTEGRAÇÃO (com DB) do SKU-bip de ENTRADA de estoque — stock.go Bipar.
//
// Regra do dono (FEAT-SKU-ESTOQUE): "ol le produto para alimentar estoque indicando
// a quantidade bipando um só - validação de sku do produto ... se for diferente nao
// deixa seguir". Estes testes travam o invariante REAL do handler, lido do código:
//
//   - O SKU bipado é validado contra sz_products do PRODUTOR (produtor_id + sku ILIKE),
//     NÃO contra o item de um pedido específico (não há pedido no context='estoque').
//   - SKU que CASA um produto do produtor → 200, credita sz_stock ADITIVAMENTE,
//     grava sz_pack_scans matched=true (context='estoque').
//   - SKU DIVERGENTE (não existe no catálogo do produtor) → 422 sku_nao_encontrado,
//     grava sz_pack_scans matched=false (product_id NULL) e NÃO credita estoque.
//   - manual_typed é PERSISTIDO mas o handler NÃO ramifica nele (fallback permitido,
//     mesmo caminho de validação).
//
// Auth: Bipar está no grupo autenticado (main.go) e usa auth.FromCtx p/ o actor.
// Montamos atrás do auth.Middleware como em main.go.
//
// Isolamento: producer_id e SKUs sintéticos altos (≥ 990000000); todas as linhas
// criadas (sz_products, sz_stock, sz_stock_movements, sz_pack_scans) são removidas
// em t.Cleanup. NÃO altera dados reais.
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/auth"
)

const (
	biparTestProducerID = int64(990000200)
	biparTestProductID  = int64(990000201) // id explícito de sz_products (chave de estoque = COALESCE(wp_post_id,id)=id)
	biparTestSKU        = "SKU-BIP-TEST-990"
)

// mountStockBipar monta o router atrás do auth.Middleware e prepara fixtures
// (produtor + produto com SKU conhecido). Devolve router, handler e o Bearer token.
func mountStockBipar(t *testing.T) (*chi.Mux, *StockHandler, string) {
	t.Helper()
	pool := testPoolOrSkip(t)
	seedBiparFixtures(t, pool)

	h := &StockHandler{Pool: pool}
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(pool))
		r.Post("/stock/bipar", h.Bipar)
	})
	token := mintAdmin(t, pool, 990000210, "bipar@test.local")
	return r, h, token
}

// seedBiparFixtures cria o produto sintético (sem wp_post_id → chave de estoque = id)
// e registra a limpeza de TODAS as linhas que os testes podem tocar.
func seedBiparFixtures(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	cleanup := func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM sz_pack_scans WHERE sku_scanned ILIKE 'SKU-BIP-TEST-%' OR product_id=$1`, biparTestProductID)
		_, _ = pool.Exec(c, `DELETE FROM sz_stock_movements WHERE product_id=$1`, biparTestProductID)
		_, _ = pool.Exec(c, `DELETE FROM sz_stock WHERE product_id=$1`, biparTestProductID)
		_, _ = pool.Exec(c, `DELETE FROM sz_products WHERE id=$1 OR produtor_id=$2`, biparTestProductID, biparTestProducerID)
	}
	cleanup()            // resíduo de execução interrompida
	t.Cleanup(cleanup)   // estado limpo ao final

	// Produto do produtor com SKU conhecido. wp_post_id NULL → COALESCE(wp_post_id,id)=id.
	if _, err := pool.Exec(ctx,
		`INSERT INTO sz_products (id, wp_post_id, produtor_id, nome, sku, status)
		 OVERRIDING SYSTEM VALUE
		 VALUES ($1, NULL, $2, 'Produto Bip Teste', $3, 'active')`,
		biparTestProductID, biparTestProducerID, biparTestSKU); err != nil {
		t.Fatalf("seed sz_products: %v", err)
	}
}

// scanRow lê a última linha de sz_pack_scans para um sku_scanned (mais recente).
type scanRow struct {
	productID   *int64
	skuScanned  string
	skuExpected *string
	matched     bool
	quantity    int64
	manualTyped bool
	context     string
}

func lastScan(t *testing.T, pool *pgxpool.Pool, skuScanned string) (scanRow, bool) {
	t.Helper()
	var s scanRow
	err := pool.QueryRow(context.Background(),
		`SELECT product_id, sku_scanned, sku_expected, matched, quantity, manual_typed, context
		   FROM sz_pack_scans
		  WHERE sku_scanned=$1
		  ORDER BY id DESC LIMIT 1`, skuScanned).
		Scan(&s.productID, &s.skuScanned, &s.skuExpected, &s.matched, &s.quantity, &s.manualTyped, &s.context)
	if err != nil {
		return scanRow{}, false
	}
	return s, true
}

// stockQty devolve qty_available do produto (0 + false se a linha não existe).
func stockQty(t *testing.T, pool *pgxpool.Pool, productID int64) (int64, bool) {
	t.Helper()
	var q int64
	err := pool.QueryRow(context.Background(),
		`SELECT qty_available FROM sz_stock WHERE product_id=$1 AND variation_id=0 AND cd_id=0`, productID).Scan(&q)
	if err != nil {
		return 0, false
	}
	return q, true
}

func postBipar(t *testing.T, r *chi.Mux, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/stock/bipar", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// ─── SKU que CASA → 200 + credita estoque + scan matched=true ────────────────

func TestStockBipar_SkuCasa_CreditaEScanMatched(t *testing.T) {
	t.Setenv("ADMIN_JWT_SECRET", "test-admin-secret-bipar")
	t.Setenv("JWT_SECRET", "test-admin-secret-bipar")
	r, h, token := mountStockBipar(t)

	body := fmt.Sprintf(`{"producer_id":%d,"sku":%q,"quantity":3,"manual_typed":false}`,
		biparTestProducerID, biparTestSKU)
	rec := postBipar(t, r, token, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("bip SKU válido = %d (%s), esperado 200", rec.Code, rec.Body.String())
	}
	var out struct {
		OK        bool  `json:"ok"`
		ProductID int64 `json:"product_id"`
		NovoSaldo int64 `json:"novo_saldo"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("resposta não decodifica: %v — body=%s", err, rec.Body.String())
	}
	if !out.OK || out.ProductID != biparTestProductID {
		t.Errorf("resposta = %+v, esperado ok=true product_id=%d", out, biparTestProductID)
	}
	if out.NovoSaldo != 3 {
		t.Errorf("novo_saldo = %d, esperado 3", out.NovoSaldo)
	}

	// Estoque creditado ADITIVAMENTE.
	if q, ok := stockQty(t, h.Pool, biparTestProductID); !ok || q != 3 {
		t.Errorf("sz_stock qty_available = (%d, ok=%v), esperado 3", q, ok)
	}

	// Scan registrado matched=true, context=estoque, product_id preenchido.
	s, ok := lastScan(t, h.Pool, biparTestSKU)
	if !ok {
		t.Fatalf("sz_pack_scans não gravou linha para sku %s", biparTestSKU)
	}
	if !s.matched {
		t.Errorf("scan.matched = false, esperado true")
	}
	if s.context != "estoque" {
		t.Errorf("scan.context = %q, esperado estoque", s.context)
	}
	if s.productID == nil || *s.productID != biparTestProductID {
		t.Errorf("scan.product_id = %v, esperado %d", s.productID, biparTestProductID)
	}
	if s.quantity != 3 {
		t.Errorf("scan.quantity = %d, esperado 3", s.quantity)
	}

	// Segunda bipada soma (alimentar = aditivo, não contagem absoluta).
	rec2 := postBipar(t, r, token, fmt.Sprintf(
		`{"producer_id":%d,"sku":%q,"quantity":2}`, biparTestProducerID, biparTestSKU))
	if rec2.Code != http.StatusOK {
		t.Fatalf("segunda bipada = %d (%s), esperado 200", rec2.Code, rec2.Body.String())
	}
	if q, ok := stockQty(t, h.Pool, biparTestProductID); !ok || q != 5 {
		t.Errorf("após 2ª bipada qty = (%d, ok=%v), esperado 5 (3+2 aditivo)", q, ok)
	}
}

// ─── SKU DIVERGENTE → 422 + scan matched=false + NÃO credita ─────────────────

func TestStockBipar_SkuDivergente_Bloqueia(t *testing.T) {
	t.Setenv("ADMIN_JWT_SECRET", "test-admin-secret-bipar")
	t.Setenv("JWT_SECRET", "test-admin-secret-bipar")
	r, h, token := mountStockBipar(t)

	const skuErrado = "SKU-BIP-TEST-INEXISTENTE"
	body := fmt.Sprintf(`{"producer_id":%d,"sku":%q,"quantity":4,"manual_typed":false}`,
		biparTestProducerID, skuErrado)
	rec := postBipar(t, r, token, body)

	// Bloqueio: o handler devolve 422 sku_nao_encontrado (confirmado no código).
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bip SKU divergente = %d (%s), esperado 422", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "sku_nao_encontrado") {
		t.Errorf("corpo do 422 = %s, esperado conter sku_nao_encontrado", rec.Body.String())
	}

	// NÃO credita estoque do produto do produtor (nenhum avanço indevido).
	if q, ok := stockQty(t, h.Pool, biparTestProductID); ok && q != 0 {
		t.Errorf("estoque não deveria ser creditado em bip divergente: qty=%d", q)
	}

	// Scan registrado mesmo no mismatch: matched=false, product_id NULL.
	s, ok := lastScan(t, h.Pool, skuErrado)
	if !ok {
		t.Fatalf("sz_pack_scans deveria registrar o mismatch (auditoria)")
	}
	if s.matched {
		t.Errorf("scan.matched = true, esperado false (mismatch)")
	}
	if s.productID != nil {
		t.Errorf("scan.product_id = %v, esperado NULL no mismatch", *s.productID)
	}
	if s.context != "estoque" {
		t.Errorf("scan.context = %q, esperado estoque", s.context)
	}
}

// ─── manual_typed (fallback digitado) é PERSISTIDO, sem ramificar comportamento ─

func TestStockBipar_ManualTyped_Persistido(t *testing.T) {
	t.Setenv("ADMIN_JWT_SECRET", "test-admin-secret-bipar")
	t.Setenv("JWT_SECRET", "test-admin-secret-bipar")
	r, h, token := mountStockBipar(t)

	// Digitar é fallback permitido: mesmo caminho de validação, sucesso normal.
	body := fmt.Sprintf(`{"producer_id":%d,"sku":%q,"quantity":1,"manual_typed":true}`,
		biparTestProducerID, biparTestSKU)
	rec := postBipar(t, r, token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("bip digitado (manual_typed) = %d (%s), esperado 200", rec.Code, rec.Body.String())
	}
	s, ok := lastScan(t, h.Pool, biparTestSKU)
	if !ok {
		t.Fatalf("scan não gravado")
	}
	if !s.manualTyped {
		t.Errorf("scan.manual_typed = false, esperado true (digitação registrada)")
	}
	if !s.matched {
		t.Errorf("scan.matched = false, esperado true (SKU válido digitado)")
	}
}
