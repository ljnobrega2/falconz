// Package jobs implementa workers Asynq para processamento assíncrono do Portal V2.
//
// WebhookDispatcher:
//   - Recebe tarefas enfileiradas via EnqueueWebhookDispatch.
//   - Carrega a configuração do webhook no banco (URL, secret, active).
//   - Faz HTTP POST com assinatura HMAC-SHA256 no header X-Senderzz-Signature.
//   - Registra o resultado em senderzz_webhook_log.
//   - Retry automático 3x com backoff exponencial via Asynq (MaxRetry: 3).
//
// Assinatura do webhook (espelha Portal_Page.php::dispatch_webhook):
//   X-Senderzz-Signature: sha256=HMAC-SHA256(payload_json, webhook.secret)
//   Mesmo esquema do GitHub Webhooks — receptor valida com hmac.Equal.
//
// Enfileiramento:
//   EnqueueWebhookDispatch(client, webhookID, eventType, payloadJSON)
//   → task "portal:webhook_dispatch" com args JSON
package jobs

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// TypeWebhookDispatch é o nome da task Asynq para disparo de webhook.
	TypeWebhookDispatch = "portal:webhook_dispatch"

	// webhookTimeout é o timeout HTTP para o POST ao endpoint do cliente.
	webhookTimeout = 15 * time.Second

	// webhookMaxRetry é o número máximo de tentativas (Asynq gerencia backoff).
	webhookMaxRetry = 3
)

// webhookDispatchPayload é a estrutura serializada na task Asynq.
type webhookDispatchPayload struct {
	WebhookID int64  `json:"webhook_id"`
	EventType string `json:"event_type"`
	Payload   string `json:"payload"` // JSON string do evento
}

// WebhookDispatcher processa tarefas de disparo de webhook.
// Deve ser registrado no mux Asynq com TypeWebhookDispatch.
type WebhookDispatcher struct {
	Pool   *pgxpool.Pool
	Client *http.Client
}

// NewWebhookDispatcher cria um WebhookDispatcher com http.Client configurado.
func NewWebhookDispatcher(pool *pgxpool.Pool) *WebhookDispatcher {
	return &WebhookDispatcher{
		Pool: pool,
		Client: &http.Client{
			Timeout: webhookTimeout,
		},
	}
}

// ProcessTask implementa asynq.Handler para TypeWebhookDispatch.
// Fluxo:
//  1. Deserializa o payload da task.
//  2. Busca webhook no banco (URL, secret, active, user_id).
//  3. Webhook inativo → loga e retorna nil (sem retry).
//  4. HTTP POST com HMAC-SHA256 no header X-Senderzz-Signature.
//  5. Registra resultado em senderzz_webhook_log.
//  6. Resposta != 2xx → retorna error (Asynq faz retry com backoff).
func (d *WebhookDispatcher) ProcessTask(ctx context.Context, t *asynq.Task) error {
	var p webhookDispatchPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		slog.Error("[webhook_dispatcher] payload inválido", "err", err)
		return fmt.Errorf("payload inválido: %w", err) // não deve acontecer — retorna sem retry útil
	}

	// Carrega webhook do banco.
	var url, secret string
	var active bool
	var userID int64

	err := d.Pool.QueryRow(ctx,
		`SELECT url, COALESCE(secret,''), active, user_id
		   FROM senderzz_portal_webhooks
		  WHERE id = $1`,
		p.WebhookID,
	).Scan(&url, &secret, &active, &userID)

	if err == pgx.ErrNoRows {
		slog.Warn("[webhook_dispatcher] webhook não encontrado — tarefa descartada",
			"webhook_id", p.WebhookID)
		return nil // não retry — webhook foi deletado
	}
	if err != nil {
		slog.Error("[webhook_dispatcher] erro ao buscar webhook",
			"webhook_id", p.WebhookID, "err", err)
		return fmt.Errorf("erro ao buscar webhook: %w", err)
	}

	// Webhook inativo ou sem URL → descarta silenciosamente.
	if !active || url == "" {
		slog.Info("[webhook_dispatcher] webhook inativo ou sem URL — tarefa descartada",
			"webhook_id", p.WebhookID)
		return nil
	}

	// Calcula assinatura HMAC-SHA256 do payload.
	sig := calcularAssinatura(p.Payload, secret)

	// Monta requisição HTTP POST.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBufferString(p.Payload))
	if err != nil {
		slog.Error("[webhook_dispatcher] erro ao montar requisição",
			"webhook_id", p.WebhookID, "url", url, "err", err)
		return fmt.Errorf("erro ao montar requisição: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Senderzz-Webhook/1.0")
	req.Header.Set("X-Senderzz-Event", p.EventType)
	req.Header.Set("X-Senderzz-Signature", "sha256="+sig)
	req.Header.Set("X-Senderzz-Webhook-ID", fmt.Sprintf("%d", p.WebhookID))

	// Executa POST com timeout via contexto.
	resp, err := d.Client.Do(req)
	responseCode := 0
	responseBody := ""

	if err != nil {
		slog.Warn("[webhook_dispatcher] erro HTTP ao disparar webhook",
			"webhook_id", p.WebhookID, "url", url, "event", p.EventType, "err", err)
		// Registra falha no log antes de retornar o erro para Asynq fazer retry.
		d.registrarLog(ctx, p.WebhookID, p.EventType, p.Payload, 0, err.Error())
		return fmt.Errorf("erro HTTP: %w", err)
	}
	defer resp.Body.Close()

	responseCode = resp.StatusCode

	// Lê até 4KB do body de resposta para o log.
	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	responseBody = string(buf[:n])

	// Registra resultado no log.
	d.registrarLog(ctx, p.WebhookID, p.EventType, p.Payload, responseCode, responseBody)

	// Resposta fora de 2xx → Asynq fará retry (até webhookMaxRetry vezes com backoff exp).
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Warn("[webhook_dispatcher] resposta não-2xx",
			"webhook_id", p.WebhookID,
			"url", url,
			"event", p.EventType,
			"status", resp.StatusCode,
		)
		return fmt.Errorf("webhook retornou status %d", resp.StatusCode)
	}

	slog.Info("[webhook_dispatcher] webhook disparado com sucesso",
		"webhook_id", p.WebhookID,
		"url", url,
		"event", p.EventType,
		"status", resp.StatusCode,
	)
	return nil
}

// registrarLog insere um registro em senderzz_webhook_log.
// Erros de inserção são apenas logados — não devem causar retry da task principal.
func (d *WebhookDispatcher) registrarLog(ctx context.Context, webhookID int64, eventType, payload string, responseCode int, responseBody string) {
	payloadJSON, _ := json.Marshal(json.RawMessage(payload))

	var rcCode *int
	if responseCode > 0 {
		rc := responseCode
		rcCode = &rc
	}

	_, err := d.Pool.Exec(ctx,
		`INSERT INTO senderzz_webhook_log
		    (webhook_id, event_type, payload, response_code, response_body)
		 VALUES ($1, $2, $3, $4, $5)`,
		webhookID, eventType, payloadJSON, rcCode, responseBody,
	)
	if err != nil {
		slog.Error("[webhook_dispatcher] erro ao registrar log",
			"webhook_id", webhookID, "err", err)
	}
}

// calcularAssinatura calcula HMAC-SHA256(payload, secret) em hex.
// Segue o esquema sha256=<hex> (compatível com GitHub Webhooks).
func calcularAssinatura(payload, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// ── Enfileiramento ────────────────────────────────────────────────────────────

// EnqueueWebhookDispatch enfileira uma tarefa de disparo de webhook no Redis via Asynq.
//
// Parâmetros:
//   - client:     cliente Asynq (conectado ao Redis)
//   - webhookID:  ID do webhook em senderzz_portal_webhooks
//   - eventType:  tipo do evento (deve estar na whitelist DT-CODE-02)
//   - payloadJSON: corpo do evento em JSON
//
// A tarefa é enfileirada com MaxRetry=3. O Asynq aplica backoff exponencial
// automático entre as tentativas.
func EnqueueWebhookDispatch(client *asynq.Client, webhookID int64, eventType, payloadJSON string) error {
	p := webhookDispatchPayload{
		WebhookID: webhookID,
		EventType: eventType,
		Payload:   payloadJSON,
	}

	taskPayload, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("[webhook_dispatcher] erro ao serializar task payload: %w", err)
	}

	task := asynq.NewTask(TypeWebhookDispatch, taskPayload,
		asynq.MaxRetry(webhookMaxRetry),
		asynq.Queue("webhooks"),
		asynq.Timeout(webhookTimeout),
	)

	info, err := client.Enqueue(task)
	if err != nil {
		return fmt.Errorf("[webhook_dispatcher] erro ao enfileirar tarefa: %w", err)
	}

	slog.Info("[webhook_dispatcher] tarefa enfileirada",
		"task_id", info.ID,
		"webhook_id", webhookID,
		"event", eventType,
		"queue", info.Queue,
	)
	return nil
}
