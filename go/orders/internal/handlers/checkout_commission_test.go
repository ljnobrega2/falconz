// checkout_commission_test.go — FEAT-AFF-COMMISSION.
//
// Prova ADVERSARIAL de que a resolução da comissão de afiliado na CRIAÇÃO do
// pedido (PostOrder) é correta e SEGURA:
//
//  1. A % da venda vem da OFERTA (senderzz_checkout_links.affiliate_commission_pct).
//  2. A fórmula é FEE-FIRST (fiel ao golden #1587 / origem WP):
//     bruta = round(total*pct/100,2); fee = round(bruta*0,0499,2); net = bruta-fee.
//  3. affiliate_amount = net (LÍQUIDA, NUNCA a bruta); transaction_fee = fee.
//  4. A trigger sz_revenue_capture_order escritura o take (= transaction_fee)
//     automaticamente — sem mudança de código na trigger.
//  5. GOLDEN GATE: a criação é INSERT-only — nenhum pedido EXISTENTE muda.
//  6. Cadeia de fallback: oferta=0 → padrão do produtor (meta) é usado.
//
// DB-gated: sem DATABASE_URL o teste é SKIPADO (go test local continua verde).
// Usa oferta tipo 'motoboy' SEM data_entrega → total=oferta, frete=0, sem CPF,
// sem chamadas externas (caminho hermético).
package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var commTestSeq atomic.Int64

// uniqToken gera um token curto e único por execução (≤80 chars do schema).
func uniqToken(prefix string) string {
	return prefix + strconv.FormatInt(time.Now().UnixNano()%0x1000000000, 36) +
		strconv.FormatInt(commTestSeq.Add(1), 36)
}

// uniqAfiliadoID gera um afiliado_id de teste único ENTRE execuções de `go test`.
// O contador commTestSeq reseta a cada processo, então sozinho colidia com linhas
// órfãs de runs anteriores que falharam antes do cleanup (unique
// uq_affiliates_produtor_afiliado_produto). Combinar nanotime + contador torna o id
// estável só dentro do processo e distinto entre processos. Faixa alta (>=9.9M) para
// não pisar em wp_user_id reais.
func uniqAfiliadoID() int64 {
	return 9_900_000 + (time.Now().UnixNano()%1_000_000)*1_000 + commTestSeq.Add(1)
}

// seedOfferMotoboy cria uma oferta motoboy com a comissão por-oferta informada e
// devolve o token. Limpa via t.Cleanup.
func seedOfferMotoboy(t *testing.T, pool *pgxpool.Pool, producerID int64, price float64, commPct float64) string {
	t.Helper()
	ctx := context.Background()
	token := uniqToken("ct")
	var id int64
	err := pool.QueryRow(ctx,
		`INSERT INTO senderzz_checkout_links
		     (producer_id, post_id, token, tipo, url, display_value, price_label,
		      affiliate_visible, name, slug, affiliate_commission_pct, created_at)
		 VALUES ($1, 0, $2, 'motoboy', '', $3, '', FALSE, 'Oferta Teste Comissão',
		         $2, $4, NOW())
		 RETURNING id`,
		producerID, token, price, commPct,
	).Scan(&id)
	if err != nil {
		t.Fatalf("seedOfferMotoboy: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM senderzz_checkout_links WHERE id=$1`, id)
	})
	return token
}

// seedAffiliateLink cria um senderzz_affiliates + senderzz_affiliate_links ativo e
// devolve (link_token, afiliadoID). Limpa via t.Cleanup.
func seedAffiliateLink(t *testing.T, pool *pgxpool.Pool, producerID int64) (string, int64) {
	t.Helper()
	ctx := context.Background()
	// afiliado_id é o wp_user_id do afiliado — id alto e único ENTRE runs (uniqAfiliadoID).
	afiliadoID := uniqAfiliadoID()
	var saID int64
	err := pool.QueryRow(ctx,
		`INSERT INTO senderzz_affiliates (produtor_id, afiliado_id, produto_id, status, comissao_pct, created_at)
		 VALUES ($1, $2, 0, 'active', 0, NOW())
		 RETURNING id`,
		producerID, afiliadoID,
	).Scan(&saID)
	if err != nil {
		t.Fatalf("seedAffiliateLink (senderzz_affiliates): %v", err)
	}
	linkToken := uniqToken("al")
	var alID int64
	err = pool.QueryRow(ctx,
		`INSERT INTO senderzz_affiliate_links (affiliate_id, link_token, produto_id, active, created_at)
		 VALUES ($1, $2, 0, TRUE, NOW())
		 RETURNING id`,
		saID, linkToken,
	).Scan(&alID)
	if err != nil {
		t.Fatalf("seedAffiliateLink (senderzz_affiliate_links): %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM senderzz_affiliate_links WHERE id=$1`, alID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM senderzz_affiliates WHERE id=$1`, saID)
	})
	return linkToken, afiliadoID
}

// seedAffiliateLinkPct é como seedAffiliateLink mas grava uma comissao_pct CUSTOM
// no vínculo senderzz_affiliates (usada no tier de override por-afiliado). Devolve
// (link_token, afiliadoID). Limpa via t.Cleanup.
func seedAffiliateLinkPct(t *testing.T, pool *pgxpool.Pool, producerID int64, customPct float64) (string, int64) {
	t.Helper()
	ctx := context.Background()
	afiliadoID := uniqAfiliadoID()
	var saID int64
	err := pool.QueryRow(ctx,
		`INSERT INTO senderzz_affiliates (produtor_id, afiliado_id, produto_id, status, comissao_pct, created_at)
		 VALUES ($1, $2, 0, 'active', $3, NOW())
		 RETURNING id`,
		producerID, afiliadoID, customPct,
	).Scan(&saID)
	if err != nil {
		t.Fatalf("seedAffiliateLinkPct (senderzz_affiliates): %v", err)
	}
	linkToken := uniqToken("al")
	var alID int64
	err = pool.QueryRow(ctx,
		`INSERT INTO senderzz_affiliate_links (affiliate_id, link_token, produto_id, active, created_at)
		 VALUES ($1, $2, 0, TRUE, NOW())
		 RETURNING id`,
		saID, linkToken,
	).Scan(&alID)
	if err != nil {
		t.Fatalf("seedAffiliateLinkPct (senderzz_affiliate_links): %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM senderzz_affiliate_links WHERE id=$1`, alID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM senderzz_affiliates WHERE id=$1`, saID)
	})
	return linkToken, afiliadoID
}

// setOverrideToggle liga/desliga sz_aff_per_affiliate_override e RESTAURA o valor
// anterior via t.Cleanup (não deixa estado vazado entre testes).
func setOverrideToggle(t *testing.T, pool *pgxpool.Pool, on bool) {
	t.Helper()
	ctx := context.Background()
	var prev string
	hadPrev := pool.QueryRow(ctx, `SELECT value FROM senderzz_options WHERE name='sz_aff_per_affiliate_override'`).Scan(&prev) == nil
	val := "0"
	if on {
		val = "1"
	}
	_, err := pool.Exec(ctx,
		`INSERT INTO senderzz_options (name, value, autoload) VALUES ('sz_aff_per_affiliate_override', $1, 'yes')
		 ON CONFLICT (name) DO UPDATE SET value=EXCLUDED.value`, val)
	if err != nil {
		t.Fatalf("setOverrideToggle: %v", err)
	}
	t.Cleanup(func() {
		if hadPrev {
			_, _ = pool.Exec(context.Background(),
				`UPDATE senderzz_options SET value=$1 WHERE name='sz_aff_per_affiliate_override'`, prev)
		} else {
			_, _ = pool.Exec(context.Background(),
				`DELETE FROM senderzz_options WHERE name='sz_aff_per_affiliate_override'`)
		}
	})
}

// affFeeFromOrder lê (affiliate_amount, transaction_fee) de um pedido como strings.
func affFeeFromOrder(t *testing.T, pool *pgxpool.Pool, orderID int64) (string, string) {
	t.Helper()
	var aff, fee string
	if err := pool.QueryRow(context.Background(),
		`SELECT affiliate_amount::text, transaction_fee::text FROM sz_orders WHERE id=$1`, orderID,
	).Scan(&aff, &fee); err != nil {
		t.Fatalf("leitura do pedido %d: %v", orderID, err)
	}
	return aff, fee
}

// createOrderForToken roda a rota real e devolve (orderID). Agenda cleanup.
func createOrderForToken(t *testing.T, pool *pgxpool.Pool, token, affToken string) int64 {
	t.Helper()
	h := NewCheckoutHandler(pool)
	req := httptest.NewRequest(http.MethodPost, "/checkout-api/order", bytes.NewReader(postOrderBody(token, affToken)))
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
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.OrderID == 0 {
		t.Fatalf("order_id ausente: %s", rec.Body.String())
	}
	cleanupOrderByNumber(t, pool, resp.OrderNumber)
	return resp.OrderID
}

// postOrderBody monta o JSON mínimo de um checkout motoboy (sem agendamento).
func postOrderBody(token, affToken string) []byte {
	b, _ := json.Marshal(map[string]any{
		"token":      token,
		"aff_token":  affToken,
		"nome":       "Cliente Teste",
		"telefone":   "11999990000",
		"cep":        "01001000",
		"logradouro": "Rua Teste",
		"numero":     "100",
		"bairro":     "Centro",
		"cidade":     "São Paulo",
		"uf":         "SP",
		// AUDIT-2026-06-21 #24: consentimento LGPD agora é obrigatório no POST /order
		// (fail-closed server-side, checkout.go:391-394). Sem consent_doc_version o
		// handler retorna 400 e todos os call sites de PostOrder quebram.
		"consent_accepted":    true,
		"consent_doc_version": "1.0",
	})
	return b
}

// cleanupOrderByNumber remove o pedido de teste criado (e TODAS as dependências
// FK de sz_orders) por order_number. Deleta filhos antes do pai. Surfaceia
// qualquer erro de DELETE via t.Errorf (não silencia — leak de pedido golden é
// exatamente o que NÃO queremos deixar passar).
func cleanupOrderByNumber(t *testing.T, pool *pgxpool.Pool, orderNumber string) {
	t.Cleanup(func() {
		ctx := context.Background()
		var id int64
		if err := pool.QueryRow(ctx, `SELECT id FROM sz_orders WHERE order_number=$1`, orderNumber).Scan(&id); err != nil {
			return // já removido ou nunca criado
		}
		// Filhos de sz_orders (FKs: items/meta/addresses/status_history/payments) +
		// efeitos de trigger (revenue/webhook_outbox). Ordem: filhos → pai.
		stmts := []string{
			`DELETE FROM senderzz_revenue WHERE order_id=$1`,
			`DELETE FROM sz_webhook_outbox WHERE order_id=$1`,
			`DELETE FROM sz_order_payments WHERE order_id=$1`,
			`DELETE FROM sz_order_status_history WHERE order_id=$1`,
			`DELETE FROM sz_order_addresses WHERE order_id=$1`,
			`DELETE FROM sz_order_items WHERE order_id=$1`,
			`DELETE FROM sz_order_meta WHERE order_id=$1`,
			`DELETE FROM sz_orders WHERE id=$1`,
		}
		for _, q := range stmts {
			if _, err := pool.Exec(ctx, q, id); err != nil {
				t.Errorf("cleanup pedido %d falhou em %q: %v", id, q, err)
			}
		}
	})
}

// TestCheckoutAffiliateCommission_OfferFeeFirst prova a fórmula fee-first e que a
// líquida (net) vai em affiliate_amount, a fee em transaction_fee, com a % vinda da
// OFERTA. Cenário espelha golden #1587: total=250, pct=60 → bruta=150, fee=7.49,
// net=142.51.
func TestCheckoutAffiliateCommission_OfferFeeFirst(t *testing.T) {
	pool := openTestDB(t)
	// Registra o Close PRIMEIRO ⇒ roda POR ÚLTIMO (t.Cleanup é LIFO), DEPOIS de
	// todas as limpezas de dados. Um `defer pool.Close()` fecharia o pool antes
	// das limpezas via t.Cleanup, fazendo-as falhar em silêncio (leak de pedidos).
	t.Cleanup(pool.Close)

	const producerID = int64(46) // produtor demo seedado
	const price = 250.0
	const pct = 60.0

	token := seedOfferMotoboy(t, pool, producerID, price, pct)
	affToken, afiliadoID := seedAffiliateLink(t, pool, producerID)

	h := NewCheckoutHandler(pool)
	req := httptest.NewRequest(http.MethodPost, "/checkout-api/order", bytes.NewReader(postOrderBody(token, affToken)))
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
	orderID := resp.OrderID
	if orderID == 0 {
		t.Fatalf("order_id ausente na resposta: %s", rec.Body.String())
	}
	cleanupOrderByNumber(t, pool, resp.OrderNumber)

	// ── Assert: colunas financeiras corretas (fee-first, líquida em affiliate_amount). ──
	var (
		gotAff   string
		gotFee   string
		gotTotal string
		gotAffID *int64
	)
	err := pool.QueryRow(context.Background(),
		`SELECT affiliate_amount::text, transaction_fee::text, total::text, affiliate_id
		   FROM sz_orders WHERE id=$1`, orderID,
	).Scan(&gotAff, &gotFee, &gotTotal, &gotAffID)
	if err != nil {
		t.Fatalf("leitura do pedido: %v", err)
	}
	// bruta = 250*60/100 = 150.00; fee = round(150*0.0499,2)=7.49; net=142.51.
	const wantAff = "142.51"
	const wantFee = "7.49"
	if gotAff != wantAff {
		t.Errorf("affiliate_amount: got %s want %s (deve ser a LÍQUIDA, não a bruta 150)", gotAff, wantAff)
	}
	if gotFee != wantFee {
		t.Errorf("transaction_fee: got %s want %s (fee-first round(150*0.0499,2))", gotFee, wantFee)
	}
	if gotAffID == nil || *gotAffID != afiliadoID {
		t.Errorf("affiliate_id: got %v want %d", gotAffID, afiliadoID)
	}

	// ── Assert: a trigger de receita escritura o take SÓ na ENTREGA. ──────────
	// A trigger LIVE sz_revenue_capture_order (override v5/v6) só lança o componente
	// 'taxa_afiliado_4_99' (= NEW.transaction_fee, ref 'order_aff:'||id) quando o
	// status vira 'completo'/'entregue' — NÃO na criação (status='pending'). Isto
	// PROVA que: (a) o pedido nasce sem booking prematuro; (b) quando entregue, o
	// take escriturado é EXATAMENTE o transaction_fee que NOSSO código gravou —
	// sem nenhuma mudança no código da trigger. base_amount = bruta = net + fee.
	//
	// Antes da entrega: zero linhas de afiliado (sanity).
	var preCnt int
	_ = pool.QueryRow(context.Background(),
		`SELECT count(*) FROM senderzz_revenue WHERE order_id=$1 AND component='taxa_afiliado_4_99'`, orderID,
	).Scan(&preCnt)
	if preCnt != 0 {
		t.Errorf("take de afiliado escriturado PREMATURAMENTE na criação (status pending): %d linhas", preCnt)
	}

	// Transiciona para 'entregue' (dispara a trigger AFTER UPDATE).
	if _, err := pool.Exec(context.Background(),
		`UPDATE sz_orders SET status='entregue' WHERE id=$1`, orderID); err != nil {
		t.Fatalf("transição p/ entregue: %v", err)
	}
	var revAmount, revBase string
	errRev := pool.QueryRow(context.Background(),
		`SELECT amount::text, base_amount::text FROM senderzz_revenue
		  WHERE order_id=$1 AND component='taxa_afiliado_4_99'
		  ORDER BY id DESC LIMIT 1`, orderID,
	).Scan(&revAmount, &revBase)
	if errRev == pgx.ErrNoRows {
		t.Errorf("trigger NÃO escriturou take do afiliado após entrega do pedido %d", orderID)
	} else if errRev != nil {
		t.Fatalf("leitura de senderzz_revenue: %v", errRev)
	} else {
		if revAmount != wantFee {
			t.Errorf("take afiliado escriturado: got %s want %s (= transaction_fee)", revAmount, wantFee)
		}
		if revBase != "150.00" { // base = affiliate_amount + transaction_fee = bruta
			t.Errorf("base_amount do take: got %s want 150.00 (bruta = net+fee)", revBase)
		}
	}
}

// TestCheckoutAffiliateCommission_FallbackToProducerDefault prova a cadeia de
// fallback: oferta com pct=0 → usa o padrão do PRODUTOR (_sz_aff_default_commission_pct).
func TestCheckoutAffiliateCommission_FallbackToProducerDefault(t *testing.T) {
	pool := openTestDB(t)
	// Registra o Close PRIMEIRO ⇒ roda POR ÚLTIMO (t.Cleanup é LIFO), DEPOIS de
	// todas as limpezas de dados. Um `defer pool.Close()` fecharia o pool antes
	// das limpezas via t.Cleanup, fazendo-as falhar em silêncio (leak de pedidos).
	t.Cleanup(pool.Close)

	// Produtor dedicado a este teste (PORTAL id alto e estável) p/ não pisar no demo.
	producerID := int64(9_800_000) + commTestSeq.Add(1)
	ctx := context.Background()
	// Define o padrão do produtor = 40%.
	_, err := pool.Exec(ctx,
		`INSERT INTO senderzz_portal_user_meta (user_id, meta_key, meta_value)
		 VALUES ($1, '_sz_aff_default_commission_pct', '40')
		 ON CONFLICT (user_id, meta_key) DO UPDATE SET meta_value=EXCLUDED.meta_value`,
		producerID,
	)
	if err != nil {
		t.Fatalf("seed meta produtor: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM senderzz_portal_user_meta WHERE user_id=$1 AND meta_key='_sz_aff_default_commission_pct'`, producerID)
	})

	const price = 100.0
	token := seedOfferMotoboy(t, pool, producerID, price, 0) // oferta SEM % própria
	affToken, _ := seedAffiliateLink(t, pool, producerID)

	h := NewCheckoutHandler(pool)
	req := httptest.NewRequest(http.MethodPost, "/checkout-api/order", bytes.NewReader(postOrderBody(token, affToken)))
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
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.OrderID == 0 {
		t.Fatalf("order_id ausente: %s", rec.Body.String())
	}
	cleanupOrderByNumber(t, pool, resp.OrderNumber)

	// pct=40 (produtor) → bruta=40.00; fee=round(40*0.0499,2)=2.00; net=38.00.
	var gotAff, gotFee string
	if err := pool.QueryRow(ctx,
		`SELECT affiliate_amount::text, transaction_fee::text FROM sz_orders WHERE id=$1`,
		resp.OrderID,
	).Scan(&gotAff, &gotFee); err != nil {
		t.Fatalf("leitura do pedido: %v", err)
	}
	if gotAff != "38.00" || gotFee != "2.00" {
		t.Errorf("fallback produtor: got aff=%s fee=%s want aff=38.00 fee=2.00", gotAff, gotFee)
	}
}

// TestCheckoutAffiliateCommission_GoldenUntouched é a GUARDA: cria um pedido NOVO
// com afiliado e confirma que NENHUM pedido EXISTENTE (snapshot pré-teste de
// affiliate_amount>0) mudou de affiliate_amount/transaction_fee. INSERT-only.
func TestCheckoutAffiliateCommission_GoldenUntouched(t *testing.T) {
	pool := openTestDB(t)
	// Registra o Close PRIMEIRO ⇒ roda POR ÚLTIMO (t.Cleanup é LIFO), DEPOIS de
	// todas as limpezas de dados. Um `defer pool.Close()` fecharia o pool antes
	// das limpezas via t.Cleanup, fazendo-as falhar em silêncio (leak de pedidos).
	t.Cleanup(pool.Close)
	ctx := context.Background()

	// Snapshot dos pedidos com afiliado ANTES.
	type snap struct{ aff, fee string }
	before := map[int64]snap{}
	rows, err := pool.Query(ctx,
		`SELECT id, affiliate_amount::text, transaction_fee::text FROM sz_orders WHERE affiliate_amount>0`)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	for rows.Next() {
		var id int64
		var s snap
		if err := rows.Scan(&id, &s.aff, &s.fee); err != nil {
			rows.Close()
			t.Fatalf("scan snapshot: %v", err)
		}
		before[id] = s
	}
	rows.Close()
	if len(before) == 0 {
		t.Skip("sem pedidos golden (affiliate_amount>0) no banco — nada a guardar")
	}

	// Cria um pedido NOVO com afiliado (mesma rota real).
	const producerID = int64(46)
	token := seedOfferMotoboy(t, pool, producerID, 197.0, 60.0)
	affToken, _ := seedAffiliateLink(t, pool, producerID)
	h := NewCheckoutHandler(pool)
	req := httptest.NewRequest(http.MethodPost, "/checkout-api/order", bytes.NewReader(postOrderBody(token, affToken)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", uniqToken("idem"))
	rec := httptest.NewRecorder()
	h.PostOrder(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PostOrder status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		OrderNumber string `json:"order_number"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	cleanupOrderByNumber(t, pool, resp.OrderNumber)

	// Re-lê os MESMOS ids do snapshot e compara — nenhum pode ter mudado.
	for id, s := range before {
		var aff, fee string
		if err := pool.QueryRow(ctx,
			`SELECT affiliate_amount::text, transaction_fee::text FROM sz_orders WHERE id=$1`, id,
		).Scan(&aff, &fee); err != nil {
			t.Fatalf("re-leitura pedido %d: %v", id, err)
		}
		if aff != s.aff || fee != s.fee {
			t.Errorf("GOLDEN VIOLADO pedido %d: antes (aff=%s fee=%s) depois (aff=%s fee=%s)",
				id, s.aff, s.fee, aff, fee)
		}
	}
	t.Logf("%d pedidos golden verificados", len(before))
}

// TestCheckoutAffiliateCommission_OverridePrecedence prova o TIER de override
// por-afiliado (FEAT-AFF-OVERRIDE, migração 429) na PRECEDÊNCIA confirmada pelo dono:
//
//	override_afiliado (toggle ON E comissao_pct custom>0) > comissão da OFERTA > ...
//
// Cenário ADVERSARIAL: oferta com affiliate_commission_pct=10 (a OFERTA dita 10%),
// mas o afiliado tem comissao_pct CUSTOM=60 no vínculo. total=250.
//   - toggle OFF (default): vence a OFERTA (10%) → bruta=25.00, fee=1.25, net=23.75.
//     PROVA que o comportamento anterior é PRESERVADO byte-a-byte quando o toggle off.
//   - toggle ON: vence o override (60%) → bruta=150, fee=7.49, net=142.51 (= golden #1587).
func TestCheckoutAffiliateCommission_OverridePrecedence(t *testing.T) {
	pool := openTestDB(t)
	// t.Cleanup(pool.Close) — registra o Close PRIMEIRO ⇒ roda POR ÚLTIMO (LIFO),
	// DEPOIS das limpezas de dados e da RESTAURAÇÃO do toggle. Um `defer pool.Close()`
	// fecharia o pool antes, fazendo o cleanup falhar em silêncio (toggle preso ON,
	// pedidos vazados).
	t.Cleanup(pool.Close)

	const producerID = int64(46) // produtor demo seedado
	const price = 250.0
	const offerPct = 10.0  // a OFERTA dita 10%
	const customPct = 60.0 // o afiliado tem 60% custom no vínculo

	// ── Caso A: toggle OFF → a OFERTA (10%) vence; override é no-op. ──────────
	setOverrideToggle(t, pool, false)
	tokenOff := seedOfferMotoboy(t, pool, producerID, price, offerPct)
	affTokenOff, _ := seedAffiliateLinkPct(t, pool, producerID, customPct)
	idOff := createOrderForToken(t, pool, tokenOff, affTokenOff)
	affOff, feeOff := affFeeFromOrder(t, pool, idOff)
	// pct=10 → bruta=25.00; fee=round(25*0.0499,2)=1.25; net=23.75.
	if affOff != "23.75" || feeOff != "1.25" {
		t.Errorf("toggle OFF deveria usar a OFERTA (10%%): got aff=%s fee=%s want aff=23.75 fee=1.25 — override NÃO pode agir com toggle off", affOff, feeOff)
	}

	// ── Caso B: toggle ON → o override por-afiliado (60%) vence a oferta. ─────
	setOverrideToggle(t, pool, true)
	tokenOn := seedOfferMotoboy(t, pool, producerID, price, offerPct)
	affTokenOn, _ := seedAffiliateLinkPct(t, pool, producerID, customPct)
	idOn := createOrderForToken(t, pool, tokenOn, affTokenOn)
	affOn, feeOn := affFeeFromOrder(t, pool, idOn)
	// pct=60 → bruta=150.00; fee=7.49; net=142.51 (igual ao golden #1587).
	if affOn != "142.51" || feeOn != "7.49" {
		t.Errorf("toggle ON deveria usar o OVERRIDE do afiliado (60%%): got aff=%s fee=%s want aff=142.51 fee=7.49", affOn, feeOn)
	}
}

// TestCheckoutAffiliateCommission_OverrideIgnoredWhenNoCustomPct prova que, mesmo com
// o toggle ON, o override NÃO age quando o afiliado NÃO tem comissao_pct custom
// (comissao_pct=0): a resolução cai na OFERTA. Guarda contra o override "roubar" a
// precedência da oferta para afiliados sem % própria.
func TestCheckoutAffiliateCommission_OverrideIgnoredWhenNoCustomPct(t *testing.T) {
	pool := openTestDB(t)
	// Close por t.Cleanup (LIFO) ⇒ roda após limpezas/restauração do toggle. Ver nota
	// em OverridePrecedence: `defer pool.Close()` fecharia o pool cedo demais.
	t.Cleanup(pool.Close)

	const producerID = int64(46)
	const price = 250.0
	const offerPct = 10.0

	setOverrideToggle(t, pool, true) // toggle LIGADO...
	token := seedOfferMotoboy(t, pool, producerID, price, offerPct)
	affToken, _ := seedAffiliateLink(t, pool, producerID) // ...mas afiliado SEM comissao_pct custom (0)
	id := createOrderForToken(t, pool, token, affToken)
	aff, fee := affFeeFromOrder(t, pool, id)
	// comissao_pct=0 ⇒ override não age ⇒ usa a OFERTA (10%): bruta=25.00, fee=1.25, net=23.75.
	if aff != "23.75" || fee != "1.25" {
		t.Errorf("override com comissao_pct=0 deveria cair na OFERTA: got aff=%s fee=%s want aff=23.75 fee=1.25", aff, fee)
	}
}
