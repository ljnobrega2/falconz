package handlers

import (
	"testing"

	"github.com/senderzz/admin-service/internal/auth"
)

func TestCanPrintExpeditionBatchActor(t *testing.T) {
	tests := []struct {
		name  string
		actor *auth.Actor
		want  bool
	}{
		{name: "admin middleware legado", actor: nil, want: true},
		{name: "admin via dual auth", actor: &auth.Actor{Kind: auth.ActorAdmin}, want: true},
		{name: "operador", actor: &auth.Actor{Kind: auth.ActorKind("operator")}, want: true},
		{name: "operador pt-br", actor: &auth.Actor{Kind: auth.ActorKind("operador")}, want: true},
		{name: "produtor", actor: &auth.Actor{Kind: auth.ActorProdutor}, want: false},
		{name: "afiliado", actor: &auth.Actor{Kind: auth.ActorAfiliado}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := canPrintExpeditionBatchActor(tt.actor); got != tt.want {
				t.Fatalf("canPrintExpeditionBatchActor(%v) = %v; want %v", tt.actor, got, tt.want)
			}
		})
	}
}
