// Testes unitários dos helpers PUROS de order_detail.go (sem DB, sem rede).
//
// O grosso do breakdown financeiro (fillFinanceiroAfiliado) lê senderzz_options
// via getOptionFloat → Postgres, então NÃO é puramente testável e fica de fora
// (per "sem exigir DB onde der"). O que é genuinamente puro e crítico:
//   - normalizeAffiliateStatus      — mapeamento de status legado → UI
//   - financeiroFrustratedStatuses  — conjunto de status estornados
//   - allowedForceStatus            — whitelist de transição manual do admin
//   - statusTimestampColumn         — coluna ts_* por status
//   - forceMotoboyStatusToSzOrder   — bridge motoboy → sz_orders (CHECK-safe)
//   - nullableString                — sql.NullString → any nil-safe
//
// package handlers (mesmo pacote, alcança não-exportados). Identificadores de
// fixture ficam locais às funções para evitar colisão no namespace de teste.
package handlers

import (
	"database/sql"
	"testing"
)

// ─── normalizeAffiliateStatus — legado → vocabulário da UI ───────────────────

func TestNormalizeAffiliateStatus(t *testing.T) {
	t.Run("fromTx=true passa direto (nomenclatura nova)", func(t *testing.T) {
		// Ao ler de senderzz_affiliate_transactions o status já está no vocabulário novo.
		for _, raw := range []string{"pending", "available", "paid", "cancelled", "reversed", "qualquer"} {
			if got := normalizeAffiliateStatus(raw, true); got != raw {
				t.Errorf("normalizeAffiliateStatus(%q, true) = %q, esperado passthrough %q", raw, got, raw)
			}
		}
	})

	t.Run("fromTx=false mapeia status PT legados", func(t *testing.T) {
		cases := map[string]string{
			"pendente":  "pending",
			"aprovada":  "available",
			"paga":      "paid",
			"estornada": "cancelled",
		}
		for raw, want := range cases {
			if got := normalizeAffiliateStatus(raw, false); got != want {
				t.Errorf("normalizeAffiliateStatus(%q, false) = %q, esperado %q", raw, got, want)
			}
		}
	})

	t.Run("fromTx=false desconhecido passa cru", func(t *testing.T) {
		for _, raw := range []string{"outro", "", "PENDENTE"} {
			if got := normalizeAffiliateStatus(raw, false); got != raw {
				t.Errorf("normalizeAffiliateStatus(%q, false) = %q, esperado passthrough %q", raw, got, raw)
			}
		}
	})
}

// ─── financeiroFrustratedStatuses — conjunto de status FRUSTRADOS ────────────

func TestFinanceiroFrustratedStatuses(t *testing.T) {
	frustrados := []string{"frustrado", "reembolsado"}
	for _, s := range frustrados {
		if !financeiroFrustratedStatuses[s] {
			t.Errorf("financeiroFrustratedStatuses[%q] = false, esperado true", s)
		}
	}
	// REGRA DO DONO (2026-06-22): CANCELADO NÃO é frustrado — tem tratamento próprio
	// (zera TUDO). Não pode estar no conjunto frustrado (senão viraria penalidade).
	for _, s := range []string{"cancelled", "cancelado"} {
		if financeiroFrustratedStatuses[s] {
			t.Errorf("financeiroFrustratedStatuses[%q] = true, esperado false (cancelado ≠ frustrado)", s)
		}
	}
	// Status normais NÃO podem estar no conjunto (senão zeraria o líquido do produtor).
	for _, s := range []string{"completo", "embalado", "pendente", "entregue", ""} {
		if financeiroFrustratedStatuses[s] {
			t.Errorf("financeiroFrustratedStatuses[%q] = true, esperado false (status normal)", s)
		}
	}
}

// ─── financeiroCancelledStatuses — conjunto de status CANCELADOS ─────────────

// TestFinanceiroCancelledStatuses trava a REGRA DO DONO (2026-06-22): venda cancelada
// zera TUDO. 'cancelled' (canônico em sz_orders) e 'cancelado' (dados migrados) ∈ set;
// frustrado e status normais ∉ set (não devem cair no caminho de zerar comissão).
func TestFinanceiroCancelledStatuses(t *testing.T) {
	cancelados := []string{"cancelled", "cancelado"}
	for _, s := range cancelados {
		if !financeiroCancelledStatuses[s] {
			t.Errorf("financeiroCancelledStatuses[%q] = false, esperado true", s)
		}
	}
	// Frustrado e status normais NÃO entram aqui — só cancelado zera comissão.
	for _, s := range []string{"frustrado", "reembolsado", "completo", "entregue", "pendente", ""} {
		if financeiroCancelledStatuses[s] {
			t.Errorf("financeiroCancelledStatuses[%q] = true, esperado false (não-cancelado)", s)
		}
	}
}

// ─── allowedForceStatus — whitelist de transição manual do admin ─────────────

// TestAllowedForceStatus trava a whitelist, incluindo em_rota para a operação
// administrativa do pedido motoboy.
func TestAllowedForceStatus(t *testing.T) {
	permitidos := []string{"agendado", "embalado", "em_rota", "entregue", "frustrado", "cancelado"}
	for _, s := range permitidos {
		if !allowedForceStatus[s] {
			t.Errorf("allowedForceStatus[%q] = false, esperado true", s)
		}
	}
	if len(allowedForceStatus) != len(permitidos) {
		t.Errorf("allowedForceStatus tem %d entradas, esperado %d", len(allowedForceStatus), len(permitidos))
	}
	// Status inventados ficam de fora.
	for _, s := range []string{"qualquer", "", "ENTREGUE"} {
		if allowedForceStatus[s] {
			t.Errorf("allowedForceStatus[%q] = true, esperado false", s)
		}
	}
}

// ─── statusTimestampColumn — coluna ts_* por status ──────────────────────────

func TestStatusTimestampColumn(t *testing.T) {
	cases := map[string]string{
		"embalado":  "ts_embalado",
		"entregue":  "ts_entregue",
		"frustrado": "ts_frustrado",
		// agendado e cancelado não têm coluna dedicada → "".
		"agendado":  "",
		"cancelado": "",
		"em_rota":   "ts_em_rota",
		"":          "",
	}
	for status, want := range cases {
		if got := statusTimestampColumn(status); got != want {
			t.Errorf("statusTimestampColumn(%q) = %q, esperado %q", status, got, want)
		}
	}
}

// ─── forceMotoboyStatusToSzOrder — bridge motoboy → sz_orders ────────────────

// TestForceMotoboyStatusToSzOrder trava o mapa para sz_orders.status (limitado ao
// CHECK constraint). 'agendado' não tem espelho (ok=false → bridge pulado).
func TestForceMotoboyStatusToSzOrder(t *testing.T) {
	cases := []struct {
		motoboy string
		wantSz  string
		wantOK  bool
	}{
		{motoboy: "entregue", wantSz: "completo", wantOK: true}, // COD entregue = Woo Completo
		{motoboy: "frustrado", wantSz: "frustrado", wantOK: true},
		{motoboy: "cancelado", wantSz: "cancelled", wantOK: true},
		{motoboy: "embalado", wantSz: "embalado", wantOK: true},
		{motoboy: "agendado", wantSz: "", wantOK: false},      // sem espelho
		{motoboy: "em_rota", wantSz: "enviado", wantOK: true}, // em trânsito no pedido principal
		{motoboy: "inventado", wantSz: "", wantOK: false},
	}
	for _, c := range cases {
		t.Run(c.motoboy, func(t *testing.T) {
			sz, ok := forceMotoboyStatusToSzOrder(c.motoboy)
			if sz != c.wantSz || ok != c.wantOK {
				t.Errorf("forceMotoboyStatusToSzOrder(%q) = (%q, %v), esperado (%q, %v)",
					c.motoboy, sz, ok, c.wantSz, c.wantOK)
			}
		})
	}
}

// ─── nullableString — sql.NullString → any nil-safe ──────────────────────────

func TestNullableString(t *testing.T) {
	// Inválido (NULL) → nil Go (vira NULL no INSERT do audit).
	if got := nullableString(sql.NullString{Valid: false, String: "ignorado"}); got != nil {
		t.Errorf("nullableString(invalid) = %v, esperado nil", got)
	}
	// Válido → a string.
	if got := nullableString(sql.NullString{Valid: true, String: "agendado"}); got != "agendado" {
		t.Errorf("nullableString(valid) = %v, esperado agendado", got)
	}
	// Válido vazio → string vazia (não nil — distinção importa pro de_status do audit).
	if got := nullableString(sql.NullString{Valid: true, String: ""}); got != "" {
		t.Errorf("nullableString(valid \"\") = %v, esperado \"\"", got)
	}
}
