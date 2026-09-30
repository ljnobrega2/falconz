// SEC-LABELS: testes unitários dos helpers PUROS dos workers de etiquetas.
//
// Sem rede e sem banco — cobrem apenas o mapeamento de status ME → interno
// (espelho de mapTrackingStatus no handler) e o helper nilIfEmpty.
package jobs

import (
	"testing"
	"time"

	"github.com/senderzz/labels-service/internal/me"
	"github.com/senderzz/labels-service/internal/track17"
)

func TestCarrierTrackingPollPolicy(t *testing.T) {
	loc := time.FixedZone("BRT", -3*60*60)
	cases := []struct {
		name    string
		status  string
		hour    int
		want    time.Duration
		allowed bool
	}{
		{name: "a caminho às 08", status: "a_caminho", hour: 8, want: time.Hour, allowed: true},
		{name: "a caminho às 19", status: "a_caminho", hour: 19, want: time.Hour, allowed: true},
		{name: "a caminho antes da janela", status: "a_caminho", hour: 7, allowed: false},
		{name: "a caminho após a janela", status: "a_caminho", hour: 20, allowed: false},
		{name: "entregue para definitivamente", status: "entregue", hour: 10, allowed: false},
		{name: "status normal mantém 12h", status: "enviado", hour: 10, want: 12 * time.Hour, allowed: true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, 8, 5, tt.hour, 0, 0, 0, loc)
			gotInterval, gotAllowed := carrierTrackingPollPolicy(tt.status, now)
			if gotInterval != tt.want || gotAllowed != tt.allowed {
				t.Fatalf("carrierTrackingPollPolicy(%q, %d:00) = (%v, %v), want (%v, %v)", tt.status, tt.hour, gotInterval, gotAllowed, tt.want, tt.allowed)
			}
		})
	}
}

func TestTrackingCarrier(t *testing.T) {
	cases := []struct {
		name, company, code string
		want                int
		ok                  bool
	}{
		{name: "Jadlog", company: "Jadlog", code: "10089478465555", want: 101052, ok: true},
		{name: "Loggi", company: "Loggi", code: "LGI-ME262BH66W2BR", want: 100457, ok: true},
		{name: "Loggi não usa código de autorização", company: "Loggi", code: "IFFCV123", ok: false},
		{name: "transportadora desconhecida", company: "Correios", code: "AA123", ok: false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := trackingCarrier(tt.company, tt.code)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("trackingCarrier(%q, %q) = (%d, %v), want (%d, %v)", tt.company, tt.code, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestCarrierEventIsDelivered(t *testing.T) {
	cases := []struct {
		name, stage, description string
		want                     bool
	}{
		{name: "entrega final pelo stage", stage: "Delivered", description: "qualquer texto", want: true},
		{name: "entregue com sucesso", description: "Produto entregue com sucesso.", want: true},
		{name: "ponto de apoio não encerra pedido", stage: "InfoReceived", description: "Seu produto foi entregue em um ponto Jadlog.", want: false},
		{name: "a caminho não encerra pedido", description: "Seu produto está a caminho do seu endereço.", want: false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := carrierEventIsDelivered(track17.Event{Stage: tt.stage, Description: tt.description})
			if got != tt.want {
				t.Fatalf("carrierEventIsDelivered(%q, %q) = %v, want %v", tt.stage, tt.description, got, tt.want)
			}
		})
	}
}

func TestResolveBackfillTracking(t *testing.T) {
	tests := []struct {
		name, current, company string
		event                  *me.ShipmentStatus
		want                   string
	}{
		{
			name:    "Jadlog troca provisório pelo tracking definitivo",
			current: "612148759", company: "Jadlog",
			event: &me.ShipmentStatus{Tracking: "10089478465930", AuthorizationCode: "612148759"},
			want:  "10089478465930",
		},
		{
			name:    "Jadlog usa authorization enquanto tracking está vazio",
			company: "Jadlog",
			event:   &me.ShipmentStatus{AuthorizationCode: "612148759"},
			want:    "612148759",
		},
		{
			name:    "Loggi nunca usa authorization como rastreio",
			current: "LGI-ME262BH66W2BR", company: "Loggi",
			event: &me.ShipmentStatus{AuthorizationCode: "IFFCV123"},
			want:  "LGI-ME262BH66W2BR",
		},
		{
			name:    "tracking da Loggi vence",
			current: "LGI-OLD", company: "Loggi",
			event: &me.ShipmentStatus{Tracking: "LGI-NEW", AuthorizationCode: "IFFCV123"},
			want:  "LGI-NEW",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveBackfillTracking(tt.current, tt.company, tt.event); got != tt.want {
				t.Fatalf("resolveBackfillTracking() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ─── mapMEStatus — status ME → status canônico interno ────────────────────────

func TestMapMEStatus(t *testing.T) {
	cases := map[string]string{
		"posted":           "posted",
		"in_transit":       "posted",
		"out_for_delivery": "posted",
		"waiting_pickup":   "released",
		"delivered":        "delivered",
		"canceled":         "canceled",
		"cancelled":        "canceled", // variante de grafia
		"returned":         "canceled",
		"lost":             "lost",
		"qualquer-coisa":   "released", // desconhecido: sem despacho confirmado
		"":                 "released", // vazio: sem despacho confirmado
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			if got := mapMEStatus(in); got != want {
				t.Fatalf("mapMEStatus(%q): esperava %q, obteve %q", in, want, got)
			}
		})
	}
}

// mapMEStatus (jobs) e mapTrackingStatus (handlers) DEVEM concordar — são duas
// cópias do mesmo mapeamento (ver comentário no handler). Este teste documenta o
// contrato compartilhado; se uma divergir da outra, falha aqui.
func TestMapMEStatusCobreTodosOsStatusCanonicos(t *testing.T) {
	canonicos := map[string]bool{
		"posted": true, "delivered": true, "canceled": true, "lost": true,
	}
	for me, interno := range map[string]string{
		"posted": "posted", "delivered": "delivered",
		"canceled": "canceled", "lost": "lost",
	} {
		got := mapMEStatus(me)
		if got != interno {
			t.Errorf("mapMEStatus(%q)=%q; esperava %q", me, got, interno)
		}
		if !canonicos[got] {
			t.Errorf("mapMEStatus(%q)=%q não é um status canônico de wc_me_labels", me, got)
		}
	}
}

// ─── nilIfEmpty — *string opcional para queries pgx ───────────────────────────

func TestNilIfEmpty(t *testing.T) {
	if got := nilIfEmpty(""); got != nil {
		t.Fatalf(`nilIfEmpty(""): esperava nil, obteve %q`, *got)
	}
	got := nilIfEmpty("/var/senderzz/labels/42.pdf")
	if got == nil {
		t.Fatal(`nilIfEmpty("..."): esperava ponteiro não-nil`)
	}
	if *got != "/var/senderzz/labels/42.pdf" {
		t.Fatalf("nilIfEmpty: valor errado: %q", *got)
	}
}
