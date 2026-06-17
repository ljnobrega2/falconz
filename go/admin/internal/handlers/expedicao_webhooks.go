// Package handlers — endpoint admin para a tela "Expedição › Webhooks".
// Espelha senderzz_pw_admin_page() em includes/senderzz-producer-webhooks.php
// (referência: AUDIT-ADMIN-WP.md §1.4.3).
//
// Webhooks são configurados por classe de envio (shipping class) e disparados
// quando um pedido WooCommerce muda de status. Cada disparo é registrado em
// senderzz_producer_webhook_logs para auditoria/depuração.
//
// Tabelas envolvidas (Postgres, schema public):
//   - senderzz_producer_webhooks       (cadastro do webhook por classe)
//   - senderzz_producer_webhook_logs   (histórico de disparos)
//   - senderzz_shipping_classes        (resolve nome da classe — opcional)
//
// Graceful degradation: se qualquer tabela estiver ausente (migração ainda não
// rodou), o endpoint devolve resposta vazia em vez de 500. Convenção CLAUDE.md.
package handlers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

// ExpedicaoWebhooksHandler — handler da tela "Expedição › Webhooks".
type ExpedicaoWebhooksHandler struct{ Pool *pgxpool.Pool }

// ---------------------------------------------------------------------------
// Tipos de resposta
// ---------------------------------------------------------------------------

// ExpWebhookRow — linha da listagem de webhooks configurados, com agregados
// de execução (última execução, último status, último erro, falhas nas últimas 24h).
type ExpWebhookRow struct {
	ID           int64    `json:"id"`
	ClassID      int64    `json:"class_id"`
	ClassName    string   `json:"class_name"`
	URL          string   `json:"url"`
	Events       []string `json:"events"`
	Active       bool     `json:"active"`
	LastFiredAt  *string  `json:"last_fired_at"`
	LastStatus   *int     `json:"last_status"`
	LastError    *string  `json:"last_error"`
	FailCount24h int64    `json:"fail_count_24h"`
	CreatedAt    string   `json:"created_at"`
}

// ExpWebhookLog — linha do histórico de disparos exibido no drawer.
type ExpWebhookLog struct {
	ID                int64   `json:"id"`
	WebhookID         int64   `json:"webhook_id"`
	Payload           string  `json:"payload"`
	ResponseCode      *int    `json:"response_code"`
	ResponseBody      *string `json:"response_body"`
	Error             *string `json:"error"`
	FiredAt           string  `json:"fired_at"`
	ReprocessCount    int     `json:"reprocess_count"`
	LastReprocessedAt *string `json:"last_reprocessed_at"`
}

// expWebhookInput — payload de POST/PUT (criação e edição).
// ClassID/Events/Active/URL podem chegar nulos em PATCH parcial.
type expWebhookInput struct {
	ClassID *int64    `json:"class_id"`
	URL     *string   `json:"url"`
	Events  *[]string `json:"events"`
	Active  *bool     `json:"active"`
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// tableExists — mesma utilidade do padrão de affiliate_wallet.go: verifica
// presença da tabela no schema public antes de rodar a query.
func (h *ExpedicaoWebhooksHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// genSecret — gera segredo de 32 caracteres hex (16 bytes random).
// Usado tanto para o INSERT inicial quanto para regenerar.
func genSecret() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// validateURL — exige scheme http/https e host. Bloqueia URLs malformadas
// antes de chegarem ao DB; o disparador honra o que estiver salvo.
func validateURL(raw string) error {
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
	if u.Host == "" {
		return fmt.Errorf("url sem host")
	}
	return nil
}

// availableEvents — lista canônica de eventos que o produtor pode assinar.
// Espelha senderzz_pw_status_event_name() e o hook sz_motoboy_status_changed
// em senderzz-producer-webhooks.php. Formato real dos eventos:
//   - WooCommerce: order_status_{slug} (ex: order_status_enviado)
//   - Motoboy:     motoboy_{status}  (7 eventos de transição)
var availableEvents = []string{
	// WooCommerce — principais transições de status (slug sem prefixo wc-)
	"order_status_pending",
	"order_status_processing",
	"order_status_on-hold",
	"order_status_completed",
	"order_status_cancelled",
	"order_status_refunded",
	"order_status_failed",
	"order_status_enviado",
	"order_status_entregue",
	"order_status_pendente",
	"order_status_aguardando",
	// Motoboy — espelha senderzz_pw_order_status_changed sz_motoboy_status_changed
	"motoboy_embalado",
	"motoboy_em_rota",
	"motoboy_a_caminho",
	"motoboy_entregue",
	"motoboy_frustrado",
	"motoboy_reagendado",
	"motoboy_cancelado",
}

// samplePayload — payload-exemplo mostrado na UI e enviado no botão "Testar".
// Espelha senderzz_pw_payload_example() em senderzz-producer-webhooks.php.
// Todos os campos em PT-BR para manter compatibilidade com integrações existentes.
func samplePayload() map[string]any {
	now := time.Now().Format(time.RFC3339)
	return map[string]any{
		"event":       "order_status_enviado",
		"status_ativo": true,
		"pedido": map[string]any{
			"id":                 1,
			"numero":             "1",
			"status":             "enviado",
			"subtotal":           197.00,
			"subtotal_formatado": "R$ 197,00",
			"total":              226.90,
			"total_formatado":    "R$ 226,90",
			"desconto":           0,
			"desconto_formatado": "",
			"metodo_pagamento":   "PIX",
			"criado_em":          now,
			"atualizado_em":      now,
			"pago_em":            now,
			"enviado_em":         "",
			"entregue_em":        "",
		},
		"classe_entrega": map[string]any{
			"id":   10,
			"nome": "São Paulo",
			"slug": "sao-paulo",
		},
		"frete": map[string]any{
			"valor":             29.90,
			"valor_formatado":   "R$ 29,90",
			"prazo_dias_uteis":  3,
			"transportadora":    "Loggi",
			"servico":           "Express",
		},
		"cliente": map[string]any{
			"nome":              "Cliente Teste",
			"telefone":          "11999999999",
			"telefone_completo": "5511999999999",
			"email":             "cliente@email.com",
			"cpf":               "000.000.000-00",
		},
		"entrega": map[string]any{
			"nome":        "Cliente Teste",
			"cep":         "01001000",
			"endereco":    "Rua Exemplo",
			"numero":      "100",
			"complemento": "",
			"bairro":      "Centro",
			"cidade":      "São Paulo",
			"estado":      "SP",
		},
		"rastreamento":      []string{"BR123456789"},
		"link_rastreamento": "https://app.senderzz.com.br/rastreio/BR123456789",
		"itens": []map[string]any{
			{"nome": "Produto", "quantidade": 1, "subtotal": 197.00},
		},
		"transportadora": "Loggi",
		"servico":        "Express",
	}
}

// ---------------------------------------------------------------------------
// GET /expedicao-webhooks
// Lista todos os webhooks configurados, com nome da classe (LEFT JOIN
// opcional) e agregados de execução nas últimas 24h.
// ---------------------------------------------------------------------------
func (h *ExpedicaoWebhooksHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if !h.tableExists(ctx, "senderzz_producer_webhooks") {
		httpx.JSON(w, 200, map[string]any{"items": []ExpWebhookRow{}})
		return
	}

	hasClasses := h.tableExists(ctx, "senderzz_shipping_classes")
	hasLogs := h.tableExists(ctx, "senderzz_producer_webhook_logs")

	// Resolve nome da classe via LEFT JOIN. Sem tabela → string vazia.
	classJoin := ""
	classSelect := "''::text"
	if hasClasses {
		classJoin = "LEFT JOIN senderzz_shipping_classes c ON c.id = wb.class_id"
		classSelect = "COALESCE(c.name, '')"
	}

	// Agregados de log (last_fired_at / last_status / fail_count_24h).
	// Sem tabela de logs → NULL/0.
	lastFiredSQL := "NULL::timestamptz"
	lastStatusSQL := "NULL::int"
	failCountSQL := "0::bigint"
	if hasLogs {
		lastFiredSQL = `(
			SELECT MAX(lg.fired_at) FROM senderzz_producer_webhook_logs lg
			WHERE lg.webhook_id = wb.id
		)`
		lastStatusSQL = `(
			SELECT lg.response_code FROM senderzz_producer_webhook_logs lg
			WHERE lg.webhook_id = wb.id
			ORDER BY lg.fired_at DESC LIMIT 1
		)`
		// Considera "falha" quando response_code está fora de 2xx OU há error.
		failCountSQL = `COALESCE((
			SELECT COUNT(*) FROM senderzz_producer_webhook_logs lg
			WHERE lg.webhook_id = wb.id
			  AND lg.fired_at >= NOW() - INTERVAL '24 hours'
			  AND (lg.response_code IS NULL
			       OR lg.response_code < 200
			       OR lg.response_code >= 300
			       OR (lg.error IS NOT NULL AND lg.error <> ''))
		), 0)`
	}

	// last_error: lido da coluna last_error da tabela principal (atualizado pelo dispatcher PHP
	// em senderzz-producer-webhooks.php:617). Graceful degradation: coluna NULL se não existir.
	lastErrorSQL := "wb.last_error"

	sql := `
		SELECT
			wb.id,
			wb.class_id,
			` + classSelect + `         AS class_name,
			COALESCE(wb.url, '')        AS url,
			COALESCE(wb.events, '[]'::jsonb)::text AS events_json,
			COALESCE(wb.active, FALSE)  AS active,
			` + lastFiredSQL + `::text  AS last_fired_at,
			` + lastStatusSQL + `       AS last_status,
			` + lastErrorSQL + `        AS last_error,
			` + failCountSQL + `        AS fail_count_24h,
			wb.created_at::text         AS created_at
		FROM senderzz_producer_webhooks wb
		` + classJoin + `
		WHERE COALESCE(wb.url, '') <> ''
		ORDER BY wb.id DESC`

	rows, err := h.Pool.Query(ctx, sql)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []ExpWebhookRow{}
	for rows.Next() {
		var (
			row        ExpWebhookRow
			eventsJSON string
		)
		if err := rows.Scan(
			&row.ID, &row.ClassID, &row.ClassName,
			&row.URL, &eventsJSON, &row.Active,
			&row.LastFiredAt, &row.LastStatus, &row.LastError, &row.FailCount24h,
			&row.CreatedAt,
		); err != nil {
			continue
		}
		// Eventos vêm como JSONB::text → unmarshal pra []string.
		_ = json.Unmarshal([]byte(eventsJSON), &row.Events)
		if row.Events == nil {
			row.Events = []string{}
		}
		out = append(out, row)
	}

	httpx.JSON(w, 200, map[string]any{"items": out})
}

// ---------------------------------------------------------------------------
// POST /expedicao-webhooks
// Cria novo webhook com segredo HMAC auto-gerado (32 hex chars).
// Body: {class_id, url, events: [], active: bool}
// ---------------------------------------------------------------------------
func (h *ExpedicaoWebhooksHandler) Create(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if !h.tableExists(ctx, "senderzz_producer_webhooks") {
		httpx.Err(w, 503, "tables_missing", "tabela senderzz_producer_webhooks ausente")
		return
	}

	var in expWebhookInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	if in.URL == nil || in.ClassID == nil || in.Events == nil {
		httpx.Err(w, 400, "bad_request", "campos obrigatórios: class_id, url, events")
		return
	}
	if err := validateURL(*in.URL); err != nil {
		httpx.Err(w, 400, "bad_request", err.Error())
		return
	}

	// Active default = true.
	active := true
	if in.Active != nil {
		active = *in.Active
	}

	secret, err := genSecret()
	if err != nil {
		httpx.Err(w, 500, "internal", "falha ao gerar segredo")
		return
	}

	eventsJSON, _ := json.Marshal(*in.Events)

	var id int64
	err = h.Pool.QueryRow(ctx,
		`INSERT INTO senderzz_producer_webhooks
		   (class_id, url, secret, events, active, created_at, updated_at)
		 VALUES ($1, $2, $3, $4::jsonb, $5, NOW(), NOW())
		 RETURNING id`,
		*in.ClassID, *in.URL, secret, string(eventsJSON), active).Scan(&id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 201, map[string]any{
		"ok":     true,
		"id":     id,
		"secret": secret,
	})
}

// ---------------------------------------------------------------------------
// PUT /expedicao-webhooks/{id}
// Atualização parcial. Só altera campos enviados no body.
// ---------------------------------------------------------------------------
func (h *ExpedicaoWebhooksHandler) Update(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}

	if !h.tableExists(ctx, "senderzz_producer_webhooks") {
		httpx.Err(w, 503, "tables_missing", "tabela senderzz_producer_webhooks ausente")
		return
	}

	var in expWebhookInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}

	// Monta SET dinâmico. Usa $N posicional + slice de args na mesma ordem.
	sets := []string{}
	args := []any{}
	idx := 1

	if in.URL != nil {
		if err := validateURL(*in.URL); err != nil {
			httpx.Err(w, 400, "bad_request", err.Error())
			return
		}
		sets = append(sets, fmt.Sprintf("url = $%d", idx))
		args = append(args, *in.URL)
		idx++
	}
	if in.ClassID != nil {
		sets = append(sets, fmt.Sprintf("class_id = $%d", idx))
		args = append(args, *in.ClassID)
		idx++
	}
	if in.Events != nil {
		b, _ := json.Marshal(*in.Events)
		sets = append(sets, fmt.Sprintf("events = $%d::jsonb", idx))
		args = append(args, string(b))
		idx++
	}
	if in.Active != nil {
		sets = append(sets, fmt.Sprintf("active = $%d", idx))
		args = append(args, *in.Active)
		idx++
	}

	if len(sets) == 0 {
		httpx.JSON(w, 200, map[string]any{"ok": true, "noop": true})
		return
	}

	sets = append(sets, "updated_at = NOW()")
	args = append(args, id)
	sql := "UPDATE senderzz_producer_webhooks SET " +
		strings.Join(sets, ", ") +
		fmt.Sprintf(" WHERE id = $%d", idx)

	if _, err := h.Pool.Exec(ctx, sql, args...); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 200, map[string]any{"ok": true, "id": id})
}

// ---------------------------------------------------------------------------
// DELETE /expedicao-webhooks/{id}
// Soft delete: limpa url='' e active=false. Convenção CLAUDE.md
// (Portal V2 webhooks): hard-delete recriaria o slot via cron.
// ---------------------------------------------------------------------------
func (h *ExpedicaoWebhooksHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}

	if !h.tableExists(ctx, "senderzz_producer_webhooks") {
		httpx.Err(w, 503, "tables_missing", "tabela senderzz_producer_webhooks ausente")
		return
	}

	_, err = h.Pool.Exec(ctx,
		`UPDATE senderzz_producer_webhooks
		 SET url = '', active = FALSE, updated_at = NOW()
		 WHERE id = $1`, id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 200, map[string]any{"ok": true, "id": id})
}

// ---------------------------------------------------------------------------
// POST /expedicao-webhooks/{id}/test
// Dispara payload-exemplo na URL configurada com HMAC, mede tempo de
// resposta e devolve {response_code, response_time_ms, error}.
// NÃO grava em senderzz_producer_webhook_logs (teste idempotente).
// ---------------------------------------------------------------------------
func (h *ExpedicaoWebhooksHandler) Test(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}

	if !h.tableExists(ctx, "senderzz_producer_webhooks") {
		httpx.Err(w, 503, "tables_missing", "tabela senderzz_producer_webhooks ausente")
		return
	}

	var (
		whURL  string
		secret string
	)
	err = h.Pool.QueryRow(ctx,
		`SELECT COALESCE(url, ''), COALESCE(secret, '')
		 FROM senderzz_producer_webhooks WHERE id = $1`, id).Scan(&whURL, &secret)
	if err != nil {
		httpx.Err(w, 404, "not_found", "webhook não encontrado")
		return
	}
	if whURL == "" {
		httpx.Err(w, 400, "bad_request", "webhook sem url (soft-deleted?)")
		return
	}

	// Monta body assinado.
	body, _ := json.Marshal(samplePayload())
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	// Timeout curto para nunca travar a request do admin.
	httpCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(httpCtx, "POST", whURL, bytes.NewReader(body))
	if err != nil {
		httpx.JSON(w, 200, map[string]any{
			"response_code":    0,
			"response_time_ms": 0,
			"error":            err.Error(),
		})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Senderzz-Signature", sig)
	req.Header.Set("X-Senderzz-Event", "webhook.test")
	req.Header.Set("User-Agent", "Senderzz-Webhook-Tester/1.0")

	client := &http.Client{Timeout: 5 * time.Second}

	t0 := time.Now()
	resp, err := client.Do(req)
	elapsedMs := time.Since(t0).Milliseconds()

	if err != nil {
		httpx.JSON(w, 200, map[string]any{
			"response_code":    0,
			"response_time_ms": elapsedMs,
			"error":            err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	// Lê até 8KB do body para evitar abusos.
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024))

	httpx.JSON(w, 200, map[string]any{
		"response_code":    resp.StatusCode,
		"response_time_ms": elapsedMs,
		"response_body":    string(respBody),
		"error":            "",
	})
}

// ---------------------------------------------------------------------------
// GET /expedicao-webhooks/{id}/logs?limit=50
// Histórico de disparos do webhook. Ordenado por fired_at DESC.
// ---------------------------------------------------------------------------
func (h *ExpedicaoWebhooksHandler) Logs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 50
	}

	if !h.tableExists(ctx, "senderzz_producer_webhook_logs") {
		httpx.JSON(w, 200, map[string]any{"items": []ExpWebhookLog{}})
		return
	}

	rows, err := h.Pool.Query(ctx,
		`SELECT id, webhook_id,
		        COALESCE(payload::text, '{}') AS payload,
		        response_code, response_body, error,
		        fired_at::text,
		        COALESCE(reprocess_count, 0)     AS reprocess_count,
		        last_reprocessed_at::text
		 FROM senderzz_producer_webhook_logs
		 WHERE webhook_id = $1
		 ORDER BY fired_at DESC
		 LIMIT $2`, id, limit)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []ExpWebhookLog{}
	for rows.Next() {
		var l ExpWebhookLog
		if err := rows.Scan(&l.ID, &l.WebhookID, &l.Payload,
			&l.ResponseCode, &l.ResponseBody, &l.Error, &l.FiredAt,
			&l.ReprocessCount, &l.LastReprocessedAt); err != nil {
			continue
		}
		out = append(out, l)
	}

	httpx.JSON(w, 200, map[string]any{"items": out})
}

// ---------------------------------------------------------------------------
// GET /expedicao-webhooks/sample-payload
// Devolve o JSON-exemplo. Mostrado no <details> "Payload exemplo" da UI e
// reutilizado no botão "Testar".
// ---------------------------------------------------------------------------
func (h *ExpedicaoWebhooksHandler) SamplePayload(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, 200, samplePayload())
}

// ---------------------------------------------------------------------------
// GET /expedicao-webhooks/available-events
// Lista canônica de eventos que podem ser assinados no checkbox-multi-select.
// ---------------------------------------------------------------------------
func (h *ExpedicaoWebhooksHandler) AvailableEvents(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, 200, map[string]any{"items": availableEvents})
}

// ---------------------------------------------------------------------------
// GET /expedicao-webhooks/classes
// Lista classes de envio para o select do formulário.
// Espelha senderzz_pw_get_classes_for_select() em senderzz-producer-webhooks.php:
//   [{id:0, name:"Produtos sem classe", slug:"sem-classe"}, {id:N, name:"...", slug:"..."}]
// Graceful: tabela ausente → retorna apenas o item "Produtos sem classe" (id=0).
// ---------------------------------------------------------------------------

type expWebhookClass struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func (h *ExpedicaoWebhooksHandler) Classes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Item universal sempre presente, igual ao PHP (id=0 = "Produtos sem classe").
	out := []expWebhookClass{
		{ID: 0, Name: "Produtos sem classe", Slug: "sem-classe"},
	}

	if !h.tableExists(ctx, "senderzz_shipping_classes") {
		httpx.JSON(w, 200, map[string]any{"classes": out})
		return
	}

	// Seleciona apenas id e name (coluna slug pode não existir em todos os ambientes).
	// Slug é derivado do nome em lowercase com hífens para compatibilidade.
	rows, err := h.Pool.Query(ctx,
		`SELECT id, COALESCE(name,'')
		 FROM senderzz_shipping_classes
		 ORDER BY name ASC, id ASC`)
	if err != nil {
		// Degrada: retorna pelo menos o item universal.
		httpx.JSON(w, 200, map[string]any{"classes": out})
		return
	}
	defer rows.Close()

	for rows.Next() {
		var c expWebhookClass
		if err := rows.Scan(&c.ID, &c.Name); err != nil {
			continue
		}
		if c.Name == "" {
			c.Name = fmt.Sprintf("Classe #%d", c.ID)
		}
		// Slug derivado do nome: lowercase, espaços → hífens.
		c.Slug = strings.ToLower(strings.ReplaceAll(c.Name, " ", "-"))
		out = append(out, c)
	}

	httpx.JSON(w, 200, map[string]any{"classes": out})
}

// ---------------------------------------------------------------------------
// POST /expedicao-webhooks/{id}/reprocess
// Reprocessa um disparo específico usando o log_id do body.
// Espelha senderzz_pw_rest_reprocess() em senderzz-producer-webhooks.php.
// Incrementa reprocess_count e last_reprocessed_at na linha do log.
// Body: {log_id: number}
// ---------------------------------------------------------------------------

type expReprocessInput struct {
	LogID int64 `json:"log_id"`
}

func (h *ExpedicaoWebhooksHandler) Reprocess(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	webhookID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || webhookID <= 0 {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}

	if !h.tableExists(ctx, "senderzz_producer_webhooks") ||
		!h.tableExists(ctx, "senderzz_producer_webhook_logs") {
		httpx.Err(w, 503, "tables_missing", "tabelas necessárias ausentes")
		return
	}

	var in expReprocessInput
	if err := httpx.DecodeJSON(r, &in); err != nil || in.LogID <= 0 {
		httpx.Err(w, 400, "bad_request", "campo obrigatório: log_id")
		return
	}

	// Lê o webhook (URL + secret).
	var (
		whURL  string
		secret string
	)
	err = h.Pool.QueryRow(ctx,
		`SELECT COALESCE(url, ''), COALESCE(secret, '')
		 FROM senderzz_producer_webhooks WHERE id = $1`, webhookID).Scan(&whURL, &secret)
	if err != nil {
		httpx.Err(w, 404, "not_found", "webhook não encontrado")
		return
	}
	if whURL == "" {
		httpx.Err(w, 400, "bad_request", "webhook sem url (soft-deleted?)")
		return
	}

	// Lê o log (payload + event).
	var (
		payloadJSON string
		event       string
	)
	err = h.Pool.QueryRow(ctx,
		`SELECT COALESCE(payload::text, '{}'), COALESCE(event, 'pedido_atualizado')
		 FROM senderzz_producer_webhook_logs
		 WHERE id = $1 AND webhook_id = $2`, in.LogID, webhookID).Scan(&payloadJSON, &event)
	if err != nil {
		httpx.Err(w, 404, "not_found", "log não encontrado para este webhook")
		return
	}

	// Dispara com HMAC, timeout 5s.
	payloadBytes := []byte(payloadJSON)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payloadBytes)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	httpCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(httpCtx, "POST", whURL, bytes.NewReader(payloadBytes))
	if err != nil {
		httpx.Err(w, 500, "internal", "erro ao montar requisição: "+err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Senderzz-Signature", sig)
	req.Header.Set("X-Senderzz-Event", event)
	req.Header.Set("X-Senderzz-Reprocess", "true")
	req.Header.Set("User-Agent", "Senderzz-Webhook-Reprocess/1.0")

	client := &http.Client{Timeout: 5 * time.Second}
	t0 := time.Now()
	resp, fireErr := client.Do(req)
	elapsedMs := time.Since(t0).Milliseconds()

	var (
		statusCode int
		respBody   string
		errMsg     string
	)
	if fireErr != nil {
		errMsg = fireErr.Error()
	} else {
		defer resp.Body.Close()
		statusCode = resp.StatusCode
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024))
		respBody = string(b)
	}

	// Atualiza reprocess_count e last_reprocessed_at na linha do log.
	_, _ = h.Pool.Exec(ctx,
		`UPDATE senderzz_producer_webhook_logs
		 SET reprocess_count    = COALESCE(reprocess_count, 0) + 1,
		     last_reprocessed_at = NOW(),
		     response_code      = NULLIF($1, 0),
		     response_body      = NULLIF($2, '')
		 WHERE id = $3 AND webhook_id = $4`,
		statusCode, respBody, in.LogID, webhookID)

	ok := statusCode >= 200 && statusCode < 300 && fireErr == nil
	httpx.JSON(w, 200, map[string]any{
		"ok":               ok,
		"response_code":    statusCode,
		"response_time_ms": elapsedMs,
		"error":            errMsg,
	})
}
