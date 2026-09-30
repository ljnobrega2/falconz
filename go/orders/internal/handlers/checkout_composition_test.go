// checkout_composition_test.go — FEAT-CHECKOUT-MULTI-ITEM.
//
// Prova que um link de checkout com composition_items (2 produtos/variações,
// escolhidos pelo PRODUTOR na criação do link — NUNCA pelo comprador) gera
// sz_order_items COM UMA LINHA POR PRODUTO da composição, ratando o subtotal
// entre elas (soma sempre bate com o subtotal do pedido — sem sobra/falta por
// arredondamento). Cobre também o caminho legado (sem composition_items) para
// garantir que nada regrediu: 1 produto = 1 item, igual a sempre.
package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// seedProduct cria um produto mínimo do produtor e devolve seu sz_products.id.
func seedProduct(t *testing.T, pool *pgxpool.Pool, producerID int64, nome string) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(),
		`INSERT INTO sz_products (produtor_id, nome, status) VALUES ($1, $2, 'active') RETURNING id`,
		producerID, nome,
	).Scan(&id)
	if err != nil {
		t.Fatalf("seedProduct: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM sz_products WHERE id=$1`, id)
	})
	return id
}

// seedOfferMotoboyComposition cria uma oferta motoboy com composition_items
// estruturada (o caso novo). Devolve o token.
func seedOfferMotoboyComposition(t *testing.T, pool *pgxpool.Pool, producerID int64, price float64, comp []compositionLineTest) string {
	t.Helper()
	ctx := context.Background()
	token := uniqToken("ctc")
	compJSON, err := json.Marshal(comp)
	if err != nil {
		t.Fatalf("marshal composition: %v", err)
	}
	var id int64
	err = pool.QueryRow(ctx,
		`INSERT INTO senderzz_checkout_links
		     (producer_id, post_id, token, tipo, url, display_value, price_label,
		      affiliate_visible, name, slug, affiliate_commission_pct, composition_items, created_at)
		 VALUES ($1, 0, $2, 'motoboy', '', $3, '', FALSE, 'Oferta Teste Composição',
		         $2, 0, $4, NOW())
		 RETURNING id`,
		producerID, token, price, string(compJSON),
	).Scan(&id)
	if err != nil {
		t.Fatalf("seedOfferMotoboyComposition: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM senderzz_checkout_links WHERE id=$1`, id)
	})
	return token
}

// compositionLineTest espelha o compLine não-exportado de checkout.go (mesmo
// shape JSON: product_id/qty/variacao).
type compositionLineTest struct {
	ProductID int64  `json:"product_id"`
	Qty       int    `json:"qty"`
	Variacao  string `json:"variacao"`
}

func TestCheckoutComposition_TwoVariationsCreatesTwoItems(t *testing.T) {
	pool := openTestDB(t)
	t.Cleanup(pool.Close)

	const producerID = int64(46)
	prodM := seedProduct(t, pool, producerID, "Camiseta FALK")
	prodG := seedProduct(t, pool, producerID, "Camiseta FALK")

	token := seedOfferMotoboyComposition(t, pool, producerID, 200.0, []compositionLineTest{
		{ProductID: prodM, Qty: 1, Variacao: "M"},
		{ProductID: prodG, Qty: 1, Variacao: "G"},
	})

	h := NewCheckoutHandler(pool)
	req := httptest.NewRequest(http.MethodPost, "/checkout-api/order", bytes.NewReader(postOrderBody(token, "")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", uniqToken("idem"))
	rec := httptest.NewRecorder()
	h.PostOrder(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PostOrder status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		OrderID     int64  `json:"order_id"`
		OrderNumber string `json:"order_number"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resposta inválida: %v body=%s", err, rec.Body.String())
	}
	cleanupOrderByNumber(t, pool, resp.OrderNumber)

	rows, err := pool.Query(context.Background(),
		`SELECT nome, quantidade, subtotal::text FROM sz_order_items WHERE order_id=$1 ORDER BY id`,
		resp.OrderID,
	)
	if err != nil {
		t.Fatalf("query sz_order_items: %v", err)
	}
	defer rows.Close()

	type row struct {
		nome     string
		qtd      int
		subtotal string
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.nome, &r.qtd, &r.subtotal); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, r)
	}

	if len(got) != 2 {
		t.Fatalf("esperava 2 itens (1 por variação), veio %d: %+v", len(got), got)
	}

	sum := 0.0
	for _, r := range got {
		v, err := strconv.ParseFloat(r.subtotal, 64)
		if err != nil {
			t.Fatalf("parse subtotal %q: %v", r.subtotal, err)
		}
		sum += v
	}
	if sum != 200.0 {
		t.Fatalf("soma dos subtotais dos itens = %.2f, esperava 200.00 (subtotal do pedido)", sum)
	}

	if got[0].nome == got[1].nome {
		t.Fatalf("os 2 itens deveriam ter nomes distintos (variação M vs G), ambos vieram %q", got[0].nome)
	}
}

// TestCheckoutComposition_SingleItemLegacyUnaffected prova que uma oferta SEM
// composition_items (ou com só 1 linha) continua gerando exatamente 1
// sz_order_items — mesmo comportamento de antes desta feature.
func TestCheckoutComposition_SingleItemLegacyUnaffected(t *testing.T) {
	pool := openTestDB(t)
	t.Cleanup(pool.Close)

	const producerID = int64(46)
	token := seedOfferMotoboy(t, pool, producerID, 150.0, 0)

	h := NewCheckoutHandler(pool)
	req := httptest.NewRequest(http.MethodPost, "/checkout-api/order", bytes.NewReader(postOrderBody(token, "")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", uniqToken("idem"))
	rec := httptest.NewRecorder()
	h.PostOrder(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PostOrder status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		OrderID     int64  `json:"order_id"`
		OrderNumber string `json:"order_number"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resposta inválida: %v body=%s", err, rec.Body.String())
	}
	cleanupOrderByNumber(t, pool, resp.OrderNumber)

	var count int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM sz_order_items WHERE order_id=$1`, resp.OrderID,
	).Scan(&count); err != nil {
		t.Fatalf("count sz_order_items: %v", err)
	}
	if count != 1 {
		t.Fatalf("esperava 1 item (caminho legado), veio %d", count)
	}
}
