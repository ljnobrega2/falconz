package handlers

import "testing"

// TestIsStatusPago valida o porte FIEL de tpc_pix_status_is_paid (pix.php:664-666).
// Os 13 estados pt/en devem ser aceitos; variações de caixa/espaço normalizadas.
func TestIsStatusPago(t *testing.T) {
	pagos := []string{
		"paid", "pago", "approved", "aprovado", "confirmed", "confirmado",
		"paid_out", "success", "completed", "concluido", "concluído",
		"authorized", "autorizado",
		"PAID", " Approved ", "Confirmado", // normalização trim+lower
	}
	for _, s := range pagos {
		if !isStatusPago(s) {
			t.Errorf("isStatusPago(%q) = false, esperado true", s)
		}
	}

	naoPagos := []string{"", "pending", "pendente", "cancelled", "analise", "waiting", "paidd"}
	for _, s := range naoPagos {
		if isStatusPago(s) {
			t.Errorf("isStatusPago(%q) = true, esperado false", s)
		}
	}
}

// TestIsStatusCancelado valida o porte de tpc_pix_status_is_cancelled (pix.php:668-670).
func TestIsStatusCancelado(t *testing.T) {
	cancelados := []string{
		"cancelled", "canceled", "cancelado", "expired", "expirado",
		"failed", "falhou", "refused", "recusado",
		"CANCELLED", " Expired ",
	}
	for _, s := range cancelados {
		if !isStatusCancelado(s) {
			t.Errorf("isStatusCancelado(%q) = false, esperado true", s)
		}
	}

	naoCancelados := []string{"", "paid", "pending", "analise", "approved"}
	for _, s := range naoCancelados {
		if isStatusCancelado(s) {
			t.Errorf("isStatusCancelado(%q) = true, esperado false", s)
		}
	}
}

// TestIsStatusAnalise valida o porte de tpc_pix_status_is_analysis (pix.php:672-677),
// incluindo o matching sem acento e o fallback por substring no payload bruto.
func TestIsStatusAnalise(t *testing.T) {
	// Whitelist direta (com e sem acento).
	analises := []string{"analysis", "analise", "análise", "em analise", "em análise", "under_review", "review", "aguardando analise", "aguardando análise"}
	for _, s := range analises {
		if !isStatusAnalise(s, nil) {
			t.Errorf("isStatusAnalise(%q) = false, esperado true", s)
		}
	}

	// Fallback por substring no payload (status genérico, marca no JSON).
	if !isStatusAnalise("processing", []byte(`{"detail":"Pagamento aguardando análise antifraude"}`)) {
		t.Error("isStatusAnalise fallback por 'aguardando analise' no payload falhou")
	}
	if !isStatusAnalise("processing", []byte(`{"detail":"under_review"}`)) {
		t.Error("isStatusAnalise fallback por 'under_review' no payload falhou")
	}

	// Não-análise: status comum sem marca no payload.
	if isStatusAnalise("paid", []byte(`{"status":"paid"}`)) {
		t.Error("isStatusAnalise(paid) = true, esperado false")
	}
}

// TestStatusLabel valida o porte do match() de tpc_endpoint_pix_status.
func TestStatusLabel(t *testing.T) {
	cases := map[string]string{
		"confirmado": "Confirmado",
		"analise":    "Em análise — aguardando confirmação do banco",
		"cancelado":  "Cancelado",
		"expirado":   "Expirado",
		"pendente":   "Aguardando pagamento",
		"qualquer":   "Aguardando pagamento",
	}
	for status, want := range cases {
		if got := statusLabel(status); got != want {
			t.Errorf("statusLabel(%q) = %q, esperado %q", status, got, want)
		}
	}
}
