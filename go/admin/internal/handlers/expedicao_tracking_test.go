package handlers

import "testing"

func TestExpedicaoTrackingLabelIsActive(t *testing.T) {
	tests := []struct {
		status string
		want   bool
	}{
		{status: "released", want: true},
		{status: "posted", want: true},
		{status: "delivered", want: true},
		{status: "canceled", want: false},
		{status: " CANCELED ", want: false},
	}

	for _, tt := range tests {
		if got := expedicaoTrackingLabelIsActive(tt.status); got != tt.want {
			t.Errorf("expedicaoTrackingLabelIsActive(%q) = %v; esperava %v", tt.status, got, tt.want)
		}
	}
}

func TestMelhorRastreioLink(t *testing.T) {
	tests := []struct {
		name    string
		carrier string
		codes   []string
		want    string
	}{
		{name: "Loggi", carrier: "Loggi", codes: []string{"LGI-ME262BGOEP0BR"}, want: "https://app.melhorrastreio.com.br/LGI-ME262BGOEP0BR"},
		{name: "Correios", carrier: "Correios", codes: []string{"AA123456789BR"}, want: "https://app.melhorrastreio.com.br/AA123456789BR"},
		{name: "outra transportadora", carrier: "Jadlog", codes: []string{"JD123BR"}, want: ""},
		{name: "sem código", carrier: "Loggi", codes: nil, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := melhorRastreioLink(tt.carrier, tt.codes); got != tt.want {
				t.Fatalf("melhorRastreioLink(%q, %v) = %q; esperava %q", tt.carrier, tt.codes, got, tt.want)
			}
		})
	}
}

func TestSelectExpedicaoTrackingCodes(t *testing.T) {
	t.Run("entregue recupera histórico cancelado sem substituta", func(t *testing.T) {
		got := selectExpedicaoTrackingCodes(nil, []string{"10089478468435"}, "entregue")
		if len(got) != 1 || got[0] != "10089478468435" {
			t.Fatalf("códigos = %v", got)
		}
	})
	t.Run("etiqueta ativa sempre vence a cancelada", func(t *testing.T) {
		got := selectExpedicaoTrackingCodes([]string{"ATIVO"}, []string{"ANTIGO"}, "entregue")
		if len(got) != 1 || got[0] != "ATIVO" {
			t.Fatalf("códigos = %v", got)
		}
	})
	t.Run("pedido não entregue não expõe etiqueta cancelada", func(t *testing.T) {
		if got := selectExpedicaoTrackingCodes(nil, []string{"ANTIGO"}, "pending"); len(got) != 0 {
			t.Fatalf("códigos = %v", got)
		}
	})
}
