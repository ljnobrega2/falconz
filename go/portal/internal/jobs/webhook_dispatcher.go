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
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
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

// NewWebhookDispatcher cria um WebhookDispatcher com http.Client endurecido
// contra SSRF (DialContext.Control + CheckRedirect — ver newSSRFSafeJobsClient).
// P0 SSRF: o disparo assíncrono é o caminho de produção (Asynq) e precisa da MESMA
// defesa de egress do disparo síncrono (handlers.dispatchSync).
func NewWebhookDispatcher(pool *pgxpool.Pool) *WebhookDispatcher {
	return &WebhookDispatcher{
		Pool:   pool,
		Client: newSSRFSafeJobsClient(webhookTimeout),
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

	// P0 SSRF: revalida o destino ANTES de disparar. Barra linhas salvas antes do
	// patch de validação no Create e rebinding de DNS. Destino interno → descarta a
	// tarefa SEM retry (retry não muda o destino; só queimaria a fila).
	if err := validatePublicWebhookURL(url); err != nil {
		slog.Warn("[webhook_dispatcher] disparo bloqueado (SSRF) — tarefa descartada",
			"webhook_id", p.WebhookID, "url", url, "motivo", err.Error())
		d.registrarLog(ctx, p.WebhookID, p.EventType, p.Payload, 0, "bloqueado: destino interno/privado (SSRF)")
		return nil
	}

	// SEC-WEBHOOK-SECRET (AUDIT IMPROVEMENT-PLAN-2026-06-18 P1-11): assinar com
	// secret vazio/curto produz um HMAC de chave previsível — qualquer um forja a
	// assinatura. Fail-closed: secret < 16 chars → descarta a tarefa SEM retry
	// (retry não conserta o secret; só queimaria a fila) e registra no log. Cobre
	// também linhas salvas antes da validação min-16 no Create.
	if len(secret) < 16 {
		slog.Warn("[webhook_dispatcher] secret ausente/curto — disparo descartado (HMAC forjável)",
			"webhook_id", p.WebhookID, "secret_len", len(secret))
		d.registrarLog(ctx, p.WebhookID, p.EventType, p.Payload, 0, "descartado: secret ausente/curto (< 16 chars) — assinatura HMAC inválida")
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

// ─────────────────────────────────────────────────────────────────────────────
// P0 SSRF — defesa de egress do disparador ASSÍNCRONO (Asynq).
//
// Réplica local (convenção da task: NÃO importar cross-módulo/cross-package) da
// lógica de handlers/webhooks.go::{isBlockedIP, validatePublicWebhookURL,
// newSSRFSafeClient}. O dispatcher é o caminho de produção e precisa da MESMA
// proteção: validação de URL + DialContext.Control (anti-rebinding) + CheckRedirect
// (anti-redirect-SSRF). Espelha go/cron/internal/dispatch/webhook.go.
// ─────────────────────────────────────────────────────────────────────────────

var errBlockedInternalDest = errors.New("destino interno/privado bloqueado (SSRF)")

// isBlockedIP — true quando o IP é interno/privado (loopback/RFC1918/link-local/
// metadata/CGNAT/unspecified/multicast). WEBHOOK_ALLOW_LOOPBACK=1 libera loopback
// (smoke local) — lido no momento da chamada (não cacheado).
func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() {
		if os.Getenv("WEBHOOK_ALLOW_LOOPBACK") == "1" {
			return false
		}
		return true
	}
	if ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		if v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
			return true // CGNAT 100.64.0.0/10
		}
	}
	return false
}

// validatePublicWebhookURL valida scheme/host e resolve o host, rejeitando destino
// interno. Host multi-IP é rejeitado se QUALQUER IP for bloqueado.
func validatePublicWebhookURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("url vazia")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("url inválida: %v", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("url deve começar com http:// ou https://")
	}
	// AUDIT-LGPD-2026-06-24 A1 (Art. 46/47/49) — TLS obrigatório no dispatch de
	// pedido. O payload (p.Payload) carrega PII completa do cliente (nome, telefone,
	// e-mail, endereço). HMAC garante integridade, não confidencialidade, então
	// http:// em texto claro vazaria os dados pessoais na rede. Exigimos https://;
	// também fecha redirect-para-http (CheckRedirect revalida por aqui). EXCEÇÃO: o
	// mesmo WEBHOOK_ALLOW_LOOPBACK=1 que libera loopback no isBlockedIP libera http no
	// smoke local; produção NUNCA seta essa env, então produção sempre exige TLS.
	if u.Scheme != "https" && os.Getenv("WEBHOOK_ALLOW_LOOPBACK") != "1" {
		return fmt.Errorf("dispatch com dados pessoais exige https:// (http em texto claro bloqueado)")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("url sem host")
	}
	if lit := net.ParseIP(host); lit != nil {
		if isBlockedIP(lit) {
			return fmt.Errorf("destino interno/privado bloqueado: %s", host)
		}
		return nil
	}
	resCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIPAddr(resCtx, host)
	if err != nil {
		return fmt.Errorf("não foi possível resolver o host %q: %v", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("host %q não resolveu para nenhum IP", host)
	}
	for _, ipa := range ips {
		if isBlockedIP(ipa.IP) {
			return fmt.Errorf("destino interno/privado bloqueado: %s (%s)", host, ipa.IP)
		}
	}
	return nil
}

// ssrfSafeControl rejeita a conexão se o IP resolvido (no momento do Dial) for
// interno — fecha a janela de DNS-rebinding.
func ssrfSafeControl(_ string, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return errBlockedInternalDest
	}
	if isBlockedIP(net.ParseIP(host)) {
		return errBlockedInternalDest
	}
	return nil
}

// newSSRFSafeJobsClient devolve um *http.Client endurecido (Control + CheckRedirect).
func newSSRFSafeJobsClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   ssrfSafeControl,
	}
	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: timeout,
		DisableKeepAlives:     true,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			if err := validatePublicWebhookURL(req.URL.String()); err != nil {
				return fmt.Errorf("redirect bloqueado: %w", err)
			}
			return nil
		},
	}
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
	// AUDIT-DEEP-2026-06-18 P2: o asynqClient fica nil quando Redis está indisponível
	// (main.go só loga warning e segue). Guard explícito evita um nil panic se um
	// caller futuro chamar Enqueue sem Redis — degrada para erro tratável (o disparo
	// é melhor-esforço; o worker assíncrono só roda com Redis up).
	if client == nil {
		return fmt.Errorf("[webhook_dispatcher] cliente Asynq indisponível (Redis fora) — disparo não enfileirado")
	}

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
