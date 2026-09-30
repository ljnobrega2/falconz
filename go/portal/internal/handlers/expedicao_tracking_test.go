package handlers

import "testing"

func TestTrackingLabelIsActive(t *testing.T) {
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
		if got := trackingLabelIsActive(tt.status); got != tt.want {
			t.Errorf("trackingLabelIsActive(%q) = %v; esperava %v", tt.status, got, tt.want)
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
		{name: "Correios com serviço", carrier: "Correios SEDEX", codes: []string{"AA123456789BR"}, want: "https://app.melhorrastreio.com.br/AA123456789BR"},
		{name: "ignora código vazio", carrier: "Loggi Express", codes: []string{" ", "LG123BR"}, want: "https://app.melhorrastreio.com.br/LG123BR"},
		{name: "outra transportadora", carrier: "Jadlog", codes: []string{"JD123BR"}, want: ""},
		{name: "sem código", carrier: "Correios", codes: nil, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := melhorRastreioLink(tt.carrier, tt.codes); got != tt.want {
				t.Fatalf("melhorRastreioLink(%q, %v) = %q; esperava %q", tt.carrier, tt.codes, got, tt.want)
			}
		})
	}
}

func TestSelectTrackingCodes(t *testing.T) {
	got := selectTrackingCodes(nil, []string{"10089478468435"}, "entregue")
	if len(got) != 1 || got[0] != "10089478468435" {
		t.Fatalf("códigos históricos do pedido entregue = %v", got)
	}
	if got := selectTrackingCodes(nil, []string{"ANTIGO"}, "pending"); len(got) != 0 {
		t.Fatalf("pedido pendente expôs código cancelado: %v", got)
	}
}
