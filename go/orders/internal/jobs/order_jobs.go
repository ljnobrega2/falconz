// Package jobs implementa os workers Asynq do serviço Orders.
//
// Workers:
//   - ProcessOrderExpiry  — expira pedidos em 'pending' sem pagamento por mais de 30 minutos.
//   - ProcessStatusSync   — durante a janela de migração, sincroniza sz_orders.status
//                          de volta para wp_wc_orders no WordPress (double-write reverso).
//
// Enfileiramento:
//   - EnqueueOrderExpiry  — agenda task de expiração via Asynq (Redis).
//   - EnqueueStatusSync   — agenda task de sincronização (usado pela statemachine ao transitar).
//
// Configuração:
//   - REDIS_URL  — DSN Redis para Asynq (ex: redis://localhost:6379)
//   - DATABASE_URL — pool Postgres (passado por injeção de dependência)
//
// Nota sobre double-write:
//   Durante a janela de migração (Fase 6), pedidos Go nativos e WC coexistem.
//   O ProcessStatusSync propaga mudanças de status do Go de volta ao WP via
//   UPDATE wp_wc_orders SET status = ... WHERE id = wp_order_id, usando
//   um prefixo "wc-" para o status WC (ex: "processing" → "wc-processing").
//   Após o cutover completo (Fase 7), esse worker deve ser desativado.
package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/orders-service/internal/statemachine"
)

// ── Nomes de task ─────────────────────────────────────────────────────────────

const (
	// TaskOrderExpiry é o nome da task de expiração de pedidos pendentes.
	TaskOrderExpiry = "orders:expiry"
	// TaskStatusSync é o nome da task de sincronização de status com WP.
	TaskStatusSync = "orders:status_sync"
)

// ── Payloads ──────────────────────────────────────────────────────────────────

// expiryPayload é o payload da task de expiração.
// Pode ser vazio (o worker processa todos os pedidos elegíveis).
type expiryPayload struct {
	// RunAt registra quando a task foi enfileirada (para auditoria em log).
	RunAt time.Time `json:"run_at"`
}

// statusSyncPayload é o payload da task de sincronização de status com WP.
type statusSyncPayload struct {
	// OrderID é o ID em sz_orders.
	OrderID int64 `json:"order_id"`
	// WPOrderID é o ID em wp_wc_orders (NULL para pedidos nativos Go — task é no-op).
	WPOrderID *int64 `json:"wp_order_id,omitempty"`
	// Status é o novo status em formato Go (ex: "processing", "aguardando").
	Status string `json:"status"`
}

// ── Enfileiramento ─────────────────────────────────────────────────────────────

// newAsynqClient cria um cliente Asynq usando REDIS_URL.
// Retorna erro se REDIS_URL não estiver configurada.
func newAsynqClient() (*asynq.Client, error) {
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		return nil, fmt.Errorf("[jobs] REDIS_URL não configurada")
	}
	opt, err := asynq.ParseRedisURI(redisURL)
	if err != nil {
		return nil, fmt.Errorf("[jobs] REDIS_URL inválida: %w", err)
	}
	return asynq.NewClient(opt), nil
}

// EnqueueOrderExpiry enfileira a task de expiração de pedidos pendentes.
// Deve ser chamada pelo cron (ex: a cada 5 minutos via scheduler Asynq).
func EnqueueOrderExpiry(ctx context.Context) error {
	client, err := newAsynqClient()
	if err != nil {
		return err
	}
	defer client.Close()

	payload, _ := json.Marshal(expiryPayload{RunAt: time.Now().UTC()})
	task := asynq.NewTask(TaskOrderExpiry, payload)

	info, err := client.EnqueueContext(ctx, task,
		asynq.Queue("orders"),
		asynq.MaxRetry(3),
		asynq.Timeout(60*time.Second),
		// Unicidade: apenas uma task de expiração ativa por vez (TTL 4 min).
		asynq.Unique(4*time.Minute),
	)
	if err != nil {
		return fmt.Errorf("[jobs] falha ao enfileirar expiração: %w", err)
	}

	slog.Info("[jobs] task de expiração enfileirada", "task_id", info.ID)
	return nil
}

// EnqueueStatusSync enfileira a task de sincronização de status com WP.
// Deve ser chamada pela statemachine após cada transição bem-sucedida.
// É no-op se wp_order_id for nil (pedido nativo Go sem origem WC).
func EnqueueStatusSync(ctx context.Context, orderID int64, wpOrderID *int64, status string) error {
	// Pedidos nativos Go não têm correspondente no WC — nada a sincronizar.
	if wpOrderID == nil {
		return nil
	}

	client, err := newAsynqClient()
	if err != nil {
		return err
	}
	defer client.Close()

	payload, _ := json.Marshal(statusSyncPayload{
		OrderID:   orderID,
		WPOrderID: wpOrderID,
		Status:    status,
	})
	task := asynq.NewTask(TaskStatusSync, payload)

	info, err := client.EnqueueContext(ctx, task,
		asynq.Queue("orders"),
		asynq.MaxRetry(5),
		asynq.Timeout(30*time.Second),
	)
	if err != nil {
		return fmt.Errorf("[jobs] falha ao enfileirar sync de status: %w", err)
	}

	slog.Info("[jobs] task de sync de status enfileirada",
		"task_id", info.ID,
		"order_id", orderID,
		"wp_order_id", *wpOrderID,
		"status", status,
	)
	return nil
}

// ── Workers ───────────────────────────────────────────────────────────────────

// ProcessOrderExpiry expira pedidos em 'pending' sem pagamento por mais de 30 minutos.
//
// Fluxo:
//  1. Busca pedidos em 'pending' com created_at < NOW() - 30min.
//  2. Para cada pedido, tenta Transition(pending → cancelled) via statemachine.
//  3. Registra motivo: "expiração automática — sem pagamento em 30 minutos".
//  4. Continua para o próximo pedido mesmo se um falhar (log do erro).
//
// Idempotência: pedidos já cancelados por outra causa não são afetados
// (statemachine rejeita transições de cancelled).
func ProcessOrderExpiry(ctx context.Context, pool *pgxpool.Pool, t *asynq.Task) error {
	var p expiryPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		slog.Warn("[jobs/expiry] payload inválido — processando sem contexto de data", "err", err)
	}

	slog.Info("[jobs/expiry] iniciando verificação de pedidos expirados")

	// Busca pedidos pending com mais de 30 minutos sem pagamento.
	rows, err := pool.Query(ctx,
		`SELECT id, order_number, payment_status
		   FROM sz_orders
		  WHERE status = 'pending'
		    AND payment_status = 'pending'
		    AND created_at < NOW() - INTERVAL '30 minutes'
		  ORDER BY created_at ASC
		  LIMIT 200`,
	)
	if err != nil {
		return fmt.Errorf("[jobs/expiry] falha ao buscar pedidos pendentes: %w", err)
	}
	defer rows.Close()

	type pendingOrder struct {
		ID          int64
		OrderNumber string
		PayStatus   string
	}

	var orders []pendingOrder
	for rows.Next() {
		var o pendingOrder
		if err := rows.Scan(&o.ID, &o.OrderNumber, &o.PayStatus); err != nil {
			slog.Error("[jobs/expiry] erro ao ler linha", "err", err)
			continue
		}
		orders = append(orders, o)
	}
	rows.Close()

	if len(orders) == 0 {
		slog.Info("[jobs/expiry] nenhum pedido pendente expirado encontrado")
		return nil
	}

	slog.Info("[jobs/expiry] pedidos a expirar", "count", len(orders))

	expirados := 0
	erros := 0
	for _, o := range orders {
		if !statemachine.CanTransition("pending", "cancelled") {
			continue
		}
		err := statemachine.Transition(ctx, pool, o.ID, "cancelled",
			0, "job", "expiração automática — sem pagamento em 30 minutos")
		if err != nil {
			slog.Error("[jobs/expiry] falha ao expirar pedido",
				"order_id", o.ID, "order_number", o.OrderNumber, "err", err)
			erros++
			continue
		}
		slog.Info("[jobs/expiry] pedido expirado",
			"order_id", o.ID, "order_number", o.OrderNumber)
		expirados++
	}

	slog.Info("[jobs/expiry] expiração concluída",
		"expirados", expirados, "erros", erros, "total", len(orders))

	// Retorna erro apenas se TODOS falharam (para permitir retry parcial).
	if erros == len(orders) {
		return fmt.Errorf("[jobs/expiry] todos os %d pedidos falharam ao expirar", erros)
	}
	return nil
}

// ProcessStatusSync sincroniza sz_orders.status com wp_wc_orders.status do WordPress.
//
// Propósito: double-write reverso durante a janela de migração (Fase 6).
// Garante que o WP continue mostrando o status atualizado enquanto os dois
// sistemas coexistem.
//
// Fluxo:
//  1. Lê wp_order_id e status do payload.
//  2. Mapeia status Go → prefixo "wc-{status}" (ex: processing → wc-processing).
//     Exceções: completo → completed, reembolsado → refunded, frustrado → failed.
//  3. Executa UPDATE wp_wc_orders SET status = $1 WHERE id = $2 (via pool compartilhado).
//     ATENÇÃO: o pool compartilhado deve apontar para o banco que contém AMBAS as tabelas
//     (MySQL migrado via pgloader para Postgres, ou acesso direto ao MySQL via dblink).
//     Alternativa: usar endpoint REST do WP via HTTP (mais seguro — implementar Fase 7).
//
// Desativação pós-cutover:
//   Após migração completa (Fase 7), comentar ou remover o registro desta task
//   no servidor Asynq. O worker é inofensivo se não houver pedidos com wp_order_id.
func ProcessStatusSync(ctx context.Context, pool *pgxpool.Pool, t *asynq.Task) error {
	var p statusSyncPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("[jobs/sync] payload inválido: %w", err)
	}

	// Pedidos nativos Go não têm wp_order_id — task é no-op.
	if p.WPOrderID == nil {
		slog.Info("[jobs/sync] pedido nativo Go sem wp_order_id — nada a sincronizar",
			"order_id", p.OrderID)
		return nil
	}

	// Mapeia status Go → status WooCommerce.
	wcStatus := mapStatusGoToWC(p.Status)

	// Atualiza wp_wc_orders no banco de destino (Postgres após migração).
	// Nota: se WP ainda estiver em MySQL separado, substituir por chamada HTTP
	// ao REST API do WP: PUT /wp-json/wc/v3/orders/{id} com status=wcStatus.
	result, err := pool.Exec(ctx,
		`UPDATE wp_wc_orders SET status = $1, date_updated_gmt = NOW() WHERE id = $2`,
		wcStatus, *p.WPOrderID,
	)
	if err != nil {
		return fmt.Errorf("[jobs/sync] falha ao sincronizar status com WP: %w", err)
	}
	if result.RowsAffected() == 0 {
		slog.Warn("[jobs/sync] wp_order_id não encontrado em wp_wc_orders",
			"order_id", p.OrderID, "wp_order_id", *p.WPOrderID)
		// Não é erro fatal — o pedido pode ter sido criado nativamente no Go.
		return nil
	}

	slog.Info("[jobs/sync] status sincronizado com WP",
		"order_id", p.OrderID,
		"wp_order_id", *p.WPOrderID,
		"go_status", p.Status,
		"wc_status", wcStatus,
	)
	return nil
}

// mapStatusGoToWC converte status do Go para o formato esperado pelo WooCommerce.
// WC usa prefixo "wc-" na maioria dos status customizados.
func mapStatusGoToWC(goStatus string) string {
	switch goStatus {
	case "pending":
		return "wc-pending"
	case "processing":
		return "wc-processing"
	case "on-hold":
		return "wc-on-hold"
	case "completo":
		// WC usa "completed" sem prefixo.
		return "completed"
	case "cancelled":
		return "wc-cancelled"
	case "reembolsado":
		// WC usa "refunded" sem prefixo.
		return "refunded"
	case "aguardando":
		return "wc-aguardando"
	case "em_separacao":
		return "wc-em_separacao"
	case "embalado":
		return "wc-embalado"
	case "enviado":
		return "wc-enviado"
	case "entregue":
		return "wc-entregue"
	case "frustrado":
		// WC não tem "frustrado" nativo — mapeia para failed.
		return "wc-frustrado"
	default:
		// Status desconhecido: prefixa com "wc-" e propaga para auditoria.
		slog.Warn("[jobs/sync] status desconhecido no mapeamento Go→WC", "status", goStatus)
		return "wc-" + goStatus
	}
}
