// Testes do REDESENHO 2026-06-24 — nome de oferta por ESTÁGIO de funil (preço).
//
// Cobre:
//   - stageSuffix (puro, sem DB): o mapa rank→sufixo (Principal/Downsell/Remarketing/N).
//   - recomputeStageNames (Postgres real): o rank por PREÇO decrescente, a aplicação
//     do sufixo a correio E ao espelho motoboy (' — Motoboy' só quando há correio par),
//     o re-rank ao inserir uma oferta mais cara, e o caso só-COD (sem sufixo Motoboy).
//
// É o GUARDA que mantém stageSuffix (Go) ≡ expressão CASE (SQL em recomputeStageNames
// e na migração 471): os nomes esperados são montados COM stageSuffix, então qualquer
// divergência entre Go e SQL quebra o teste.
//
// Gate: pula (t.Skip) se DATABASE_URL não estiver setada — mesma convenção dos demais
// *_db_test.go. Isolamento: producer_id sintético de namespace ALTO; tudo roda numa
// única tx com ROLLBACK no fim → zero resíduo no banco (nada a limpar).
package handlers

import (
	"context"
	"fmt"
	"testing"
)

// TestStageSuffix trava o mapa rank→sufixo (decisão do dono 2026-06-24).
func TestStageSuffix(t *testing.T) {
	cases := []struct {
		rank int
		want string
	}{
		{0, " Principal"}, // defensivo (rank inválido ≤1 → Principal)
		{1, " Principal"},
		{2, " Downsell"},
		{3, " Remarketing"},
		{4, " Remarketing 2"},
		{5, " Remarketing 3"},
		{6, " Remarketing 4"},
	}
	for _, c := range cases {
		if got := stageSuffix(c.rank); got != c.want {
			t.Errorf("stageSuffix(%d) = %q, want %q", c.rank, got, c.want)
		}
	}
}

// namingTestProducer — namespace de producer_id sintético, alto e improvável.
const namingTestProducer int64 = 990500000

// TestRecomputeStageNamesByPrice exercita o recompute contra o banco real.
func TestRecomputeStageNamesByPrice(t *testing.T) {
	pool := requireLGPDDB(t) // reusa o gate DATABASE_URL dos testes LGPD.
	ctx := context.Background()
	base := "1 ProdutoTesteStage"

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer tx.Rollback(ctx) // sempre desfaz — nada persiste.

	// insertPair insere o par correio+motoboy de uma oferta (preço único na base).
	tokenSeq := 0
	insertPair := func(price float64) {
		t.Helper()
		for _, tipo := range []string{"correio", "motoboy"} {
			tokenSeq++
			tok := fmt.Sprintf("stage-test-%d-%d", namingTestProducer, tokenSeq)
			if _, err := tx.Exec(ctx,
				`INSERT INTO senderzz_checkout_links
				   (producer_id, post_id, token, tipo, url, display_value, price_label,
				    affiliate_visible, name, slug, base_name, created_at)
				 VALUES ($1, 0, $2, $3, '', $4, '', false, $5, $2, $6, NOW())`,
				namingTestProducer, tok, tipo, price, base, base,
			); err != nil {
				t.Fatalf("insert %s @ %.2f: %v", tipo, price, err)
			}
		}
	}

	// nameAt lê o name de um (tipo, preço) na base sintética.
	nameAt := func(tipo string, price float64) string {
		t.Helper()
		var n string
		if err := tx.QueryRow(ctx,
			`SELECT name FROM senderzz_checkout_links
			  WHERE producer_id = $1 AND base_name = $2 AND tipo = $3 AND display_value = $4`,
			namingTestProducer, base, tipo, price,
		).Scan(&n); err != nil {
			t.Fatalf("nameAt(%s, %.2f): %v", tipo, price, err)
		}
		return n
	}

	// expectRank verifica correio E motoboy de um preço conforme o rank esperado.
	expectRank := func(price float64, rank int) {
		t.Helper()
		wantCorreio := base + stageSuffix(rank)
		if got := nameAt("correio", price); got != wantCorreio {
			t.Errorf("correio @ %.2f (rank %d) = %q, want %q", price, rank, got, wantCorreio)
		}
		wantMotoboy := wantCorreio + " — Motoboy"
		if got := nameAt("motoboy", price); got != wantMotoboy {
			t.Errorf("motoboy @ %.2f (rank %d) = %q, want %q", price, rank, got, wantMotoboy)
		}
	}

	// ── Cenário 1: 197 / 147 / 127 → Principal / Downsell / Remarketing ───────────
	insertPair(197)
	insertPair(147)
	insertPair(127)
	if err := recomputeStageNames(ctx, tx, namingTestProducer, base); err != nil {
		t.Fatalf("recompute cenário 1: %v", err)
	}
	expectRank(197, 1) // Principal (sem sufixo)
	expectRank(147, 2) // Downsell
	expectRank(127, 3) // Remarketing

	// ── Cenário 2: + 4ª oferta mais barata (97) → Remarketing 2 ───────────────────
	insertPair(97)
	if err := recomputeStageNames(ctx, tx, namingTestProducer, base); err != nil {
		t.Fatalf("recompute cenário 2: %v", err)
	}
	expectRank(197, 1)
	expectRank(147, 2)
	expectRank(127, 3)
	expectRank(97, 4) // Remarketing 2

	// ── Cenário 3: + oferta MAIS CARA (247) → ela vira Principal e todas descem ────
	insertPair(247)
	if err := recomputeStageNames(ctx, tx, namingTestProducer, base); err != nil {
		t.Fatalf("recompute cenário 3: %v", err)
	}
	expectRank(247, 1) // nova Principal
	expectRank(197, 2) // antiga Principal → Downsell
	expectRank(147, 3) // → Remarketing
	expectRank(127, 4) // → Remarketing 2
	expectRank(97, 5)  // → Remarketing 3

	// ── Cenário 4: DELETE do par 247 → estágios "sobem" de volta ──────────────────
	if _, err := tx.Exec(ctx,
		`DELETE FROM senderzz_checkout_links
		  WHERE producer_id = $1 AND base_name = $2 AND display_value = 247`,
		namingTestProducer, base,
	); err != nil {
		t.Fatalf("delete 247: %v", err)
	}
	if err := recomputeStageNames(ctx, tx, namingTestProducer, base); err != nil {
		t.Fatalf("recompute cenário 4: %v", err)
	}
	expectRank(197, 1)
	expectRank(147, 2)
	expectRank(127, 3)
	expectRank(97, 4)

	// ── Cenário 5: só-COD (motoboy sem correio par) → SEM sufixo ' — Motoboy' ──────
	baseCod := "1 ProdutoSoCOD"
	for _, price := range []float64{89, 69} {
		tokenSeq++
		tok := fmt.Sprintf("stage-cod-%d-%d", namingTestProducer, tokenSeq)
		if _, err := tx.Exec(ctx,
			`INSERT INTO senderzz_checkout_links
			   (producer_id, post_id, token, tipo, url, display_value, price_label,
			    affiliate_visible, name, slug, base_name, created_at)
			 VALUES ($1, 0, $2, 'motoboy', '', $3, '', false, $4, $2, $4, NOW())`,
			namingTestProducer, tok, price, baseCod,
		); err != nil {
			t.Fatalf("insert só-COD @ %.2f: %v", price, err)
		}
	}
	if err := recomputeStageNames(ctx, tx, namingTestProducer, baseCod); err != nil {
		t.Fatalf("recompute cenário 5: %v", err)
	}
	var nameCod89, nameCod69 string
	if err := tx.QueryRow(ctx,
		`SELECT name FROM senderzz_checkout_links WHERE producer_id=$1 AND base_name=$2 AND display_value=89`,
		namingTestProducer, baseCod,
	).Scan(&nameCod89); err != nil {
		t.Fatalf("read só-COD 89: %v", err)
	}
	if err := tx.QueryRow(ctx,
		`SELECT name FROM senderzz_checkout_links WHERE producer_id=$1 AND base_name=$2 AND display_value=69`,
		namingTestProducer, baseCod,
	).Scan(&nameCod69); err != nil {
		t.Fatalf("read só-COD 69: %v", err)
	}
	// Sem correio par → motoboy é o principal/downsell SEM ' — Motoboy'.
	if want := baseCod + " Principal"; nameCod89 != want {
		t.Errorf("só-COD principal = %q, want %q (sem sufixo Motoboy)", nameCod89, want)
	}
	if want := baseCod + " Downsell"; nameCod69 != want {
		t.Errorf("só-COD downsell = %q, want %q (sem sufixo Motoboy)", nameCod69, want)
	}
}
