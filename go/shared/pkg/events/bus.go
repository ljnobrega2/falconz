// Package events — event bus stub para inter-service communication.
//
// Durante a migração (Fases 1-6), eventos são publicados como log estruturado
// via slog. Após cutover completo, substituir por NATS JetStream ou Redis Streams.
//
// Padrão de uso:
//
//	bus := events.NewBus()
//	bus.Publish(ctx, "order.status_changed", map[string]any{
//	    "order_id": 123,
//	    "from":     "aguardando",
//	    "to":       "em_separacao",
//	})
package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
)

// Handler processa um evento.
type Handler func(ctx context.Context, event Event) error

// Event representa um evento publicado no bus.
type Event struct {
	// Topic ex: "order.status_changed", "pix.confirmed", "motoboy.entregue"
	Topic   string
	Payload map[string]any
}

// Bus é o event bus. Stub local: entrega síncrona via slog + handlers registrados.
// Em produção: trocar por NATS JetStream client.
type Bus struct {
	mu       sync.RWMutex
	handlers map[string][]Handler
}

// NewBus cria um Bus.
func NewBus() *Bus {
	return &Bus{handlers: make(map[string][]Handler)}
}

// Subscribe registra um handler para um tópico.
func (b *Bus) Subscribe(topic string, h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[topic] = append(b.handlers[topic], h)
}

// Publish publica um evento. Handlers registrados rodam em goroutines separadas.
// Erros são logados mas não bloqueiam o caller.
func (b *Bus) Publish(ctx context.Context, topic string, payload map[string]any) {
	data, _ := json.Marshal(payload)
	slog.InfoContext(ctx, "[events] publicado", "topic", topic, "payload", string(data))

	b.mu.RLock()
	handlers := append([]Handler(nil), b.handlers[topic]...)
	b.mu.RUnlock()

	for _, h := range handlers {
		go func(fn Handler) {
			if err := fn(ctx, Event{Topic: topic, Payload: payload}); err != nil {
				slog.ErrorContext(ctx, "[events] handler error", "topic", topic, "err", err)
			}
		}(h)
	}
}

// Tópicos canônicos — usar estas constantes em vez de strings literais.
const (
	TopicOrderCreated       = "order.created"
	TopicOrderStatusChanged = "order.status_changed"
	TopicOrderPaid          = "order.paid"
	TopicOrderCancelled     = "order.cancelled"
	TopicPixConfirmed       = "pix.confirmed"
	TopicPixExpired         = "pix.expired"
	TopicMotoboyEntregue    = "motoboy.entregue"
	TopicMotoboyFrustrado   = "motoboy.frustrado"
	TopicLabelGenerated     = "label.generated"
	TopicLabelCancelled     = "label.cancelled"
	TopicAffiliateLinked    = "affiliate.linked"
	TopicCommissionCreated  = "commission.created"
	TopicCODCredited        = "cod.credited"
)
