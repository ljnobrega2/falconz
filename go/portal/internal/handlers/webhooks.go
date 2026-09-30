// Package handlers — handlers de webhooks do Portal V2.
//
// Rotas (namespace /wp-json/senderzz/v1):
//
//	GET  /portal/webhooks                — lista webhooks do usuário (url != '')
//	POST /portal/webhooks                — cria/atualiza webhook
//	DELETE /portal/webhooks/{id}         — soft-delete (url='', active=false)
//	GET  /portal/webhooks/{id}/history   — histórico de disparos
//	POST /portal/webhooks/clear-history  — limpa logs de webhook
//
// Soft-delete (espelha Portal_Page.php::ajax_webhooks_delete):
//
//	Hard-delete faz senderzz_pw_ensure_user_webhook_slots() recriar o slot.
//	Soft-delete: url='', active=false. Query de listagem filtra url != ''.
//
// Whitelist de event_types (DT-CODE-02):
//
//	Expedição (tipo=expedicao): order_status_enviado, order_status_entregue,
//	    order_status_cancelado, order_status_frustrado, order_status_em_rota,
//	    order_status_embalado, order_status_coletado
//	COD/Motoboy (tipo=cod): motoboy_agendado, motoboy_pre_agendado,
//	    motoboy_embalado, motoboy_enviado, motoboy_coletado, motoboy_completo,
//	    motoboy_frustrado, motoboy_a_caminho, motoboy_em_rota
//	Qualquer outro valor é rejeitado com 400.
//
// Campos `tipo` (expedicao|cod) e `product_id` (roteamento por produto, NULL=todos)
// são aceitos no Create e devolvidos no List. O dispatcher (go/cron) usa product_id
// para rotear o disparo por produto do pedido.
package handlers

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
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/portal-service/internal/auth"
	"github.com/senderzz/portal-service/internal/httpx"
)

// webhookTestTimeout — timeout do POST síncrono de teste/reenvio (feedback imediato).
const webhookTestTimeout = 15 * time.Second

// WebhookHandler agrupa as dependências dos handlers de webhook.
type WebhookHandler struct {
	Pool *pgxpool.Pool
}

// allowedEventTypes — whitelist de tipos de evento aceitos.
// DT-CODE-02: não usar substring match — exact match apenas.
// Substring match é o bypass CRIT-04 (ver CHANGELOG-SECURITY.md).
//
// Dois conjuntos coexistem:
//   - EXPEDIÇÃO (order_status_*): webhooks tipo='expedicao' (comportamento original).
//   - COD/MOTOBOY (motoboy_*):    webhooks tipo='cod'. Antes davam 400 aqui, o que
//     forçava o frontend a bloquear COD. Os 9 eventos espelham os status canônicos
//     do dono (agendado/pré-agendado/embalado/enviado/coletado/completo/frustrado/
//     a caminho/em rota). NÃO acoplado ao campo `tipo` — a whitelist só garante
//     que o nome do evento é conhecido; a classificação fica em `tipo`.
var allowedEventTypes = map[string]bool{
	// Expedição — order_status_*
	"order_status_enviado":   true,
	"order_status_entregue":  true,
	"order_status_cancelado": true,
	"order_status_frustrado": true,
	"order_status_em_rota":   true,
	"order_status_embalado":  true,
	// AUDIT-2026-07-31 (dono): status 'coletado' (531-status-coletado.sql) —
	// evento novo do outbox (sz_webhook_outbox_enqueue gera
	// order_status_coletado automaticamente pra QUALQUER status novo, mas a
	// whitelist de assinatura é separada e ficou pra trás).
	"order_status_coletado": true,
	// COD/Motoboy — motoboy_*
	"motoboy_agendado":     true,
	"motoboy_pre_agendado": true,
	"motoboy_embalado":     true,
	"motoboy_enviado":      true,
	"motoboy_coletado":     true,
	"motoboy_completo":     true,
	"motoboy_frustrado":    true,
	"motoboy_a_caminho":    true,
	"motoboy_em_rota":      true,
}

// allowedWebhookTipos — classificação válida do webhook. Default '' → 'expedicao'.
var allowedWebhookTipos = map[string]bool{
	"expedicao": true,
	"cod":       true,
}

// webhookResponse representa um webhook na listagem.
type webhookResponse struct {
	ID              int64    `json:"id"`
	URL             string   `json:"url"`
	Active          bool     `json:"active"`
	EventTypes      []string `json:"event_types"`
	ShippingClassID *int     `json:"shipping_class_id,omitempty"`
	Tipo            string   `json:"tipo"`                  // expedicao | cod
	ProductID       *int64   `json:"product_id"`            // NULL = todos os produtos
	CreatedAt       string   `json:"created_at"`
	UpdatedAt       string   `json:"updated_at"`
}

// webhookLogEntry representa uma entrada no histórico de disparos.
type webhookLogEntry struct {
	ID           int64   `json:"id"`
	EventType    string  `json:"event_type"`
	ResponseCode *int    `json:"response_code"`
	ResponseBody *string `json:"response_body,omitempty"`
	CreatedAt    string  `json:"created_at"`
}

// createWebhookRequest é o body de POST /portal/webhooks.
type createWebhookRequest struct {
	URL             string   `json:"url"`
	Secret          string   `json:"secret"`
	Active          *bool    `json:"active"`
	EventTypes      []string `json:"event_types"`
	ShippingClassID *int     `json:"shipping_class_id"`
	Tipo            string   `json:"tipo"`       // expedicao | cod — vazio vira 'expedicao'
	ProductID       *int64   `json:"product_id"` // roteamento por produto (NULL = todos)
}

// ── GET /portal/webhooks ──────────────────────────────────────────────────────

// List retorna os webhooks configurados pelo usuário autenticado.
// Filtra slots com url=” (soft-deleted).
func (h *WebhookHandler) List(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	rows, err := h.Pool.Query(r.Context(),
		`SELECT id, url, active, event_types,
		        shipping_class_id, tipo, product_id, created_at, updated_at
		   FROM senderzz_portal_webhooks
		  WHERE user_id = $1
		    AND url != ''
		  ORDER BY id ASC`,
		u.ID,
	)
	if err != nil {
		slog.Error("[portal_webhooks] erro ao listar webhooks", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer rows.Close()

	var result []webhookResponse
	for rows.Next() {
		var wh webhookResponse
		var etJSON []byte
		// created_at/updated_at são TIMESTAMPTZ no Postgres — pgx NÃO escaneia
		// timestamptz direto em string (era a causa do 500 quando havia linhas).
		// Escaneamos em time.Time e formatamos em RFC3339 (que o dtTime do front
		// — via _parse/new Date — interpreta de forma confiável).
		var createdAt, updatedAt time.Time
		err := rows.Scan(
			&wh.ID, &wh.URL, &wh.Active, &etJSON,
			&wh.ShippingClassID, &wh.Tipo, &wh.ProductID, &createdAt, &updatedAt,
		)
		if err != nil {
			slog.Error("[portal_webhooks] erro ao ler linha", "user_id", u.ID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao ler webhooks")
			return
		}
		wh.CreatedAt = createdAt.Format(time.RFC3339)
		wh.UpdatedAt = updatedAt.Format(time.RFC3339)
		if err := json.Unmarshal(etJSON, &wh.EventTypes); err != nil {
			wh.EventTypes = []string{}
		}
		result = append(result, wh)
	}
	if rows.Err() != nil {
		slog.Error("[portal_webhooks] erro após iteração", "user_id", u.ID, "err", rows.Err())
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao processar webhooks")
		return
	}

	if result == nil {
		result = []webhookResponse{}
	}

	httpx.WriteOK(w, map[string]any{"data": result, "total": len(result)})
}

// ── POST /portal/webhooks ────────────────────────────────────────────────────

// Create cria ou atualiza um webhook.
// Valida a whitelist de event_types (DT-CODE-02) antes de persistir.
func (h *WebhookHandler) Create(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	var req createWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}
	if req.URL == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "url é obrigatória")
		return
	}

	// P0 SSRF: valida a URL ANTES de salvar — scheme http(s), host presente, e o
	// host NÃO pode resolver para um IP interno/privado (loopback/RFC1918/link-local/
	// metadata/CGNAT). Sem isto, um webhook apontado a 127.0.0.1 / 169.254.169.254
	// seria persistido e depois disparado pelo dispatcher como proxy interno.
	if err := validatePublicWebhookURL(req.URL); err != nil {
		slog.Warn("[portal_webhooks] URL de webhook rejeitada (SSRF)", "user_id", u.ID, "url", req.URL, "motivo", err.Error())
		httpx.WriteErr(w, http.StatusBadRequest, "URL de destino inválida: "+err.Error())
		return
	}

	// SEC-WEBHOOK-SECRET (AUDIT IMPROVEMENT-PLAN-2026-06-18 P1-11): um secret
	// não-vazio porém curto (< 16 chars) gera uma assinatura HMAC fraca/forjável.
	// Rejeitamos secret fraco no cadastro (mensagem clara ao usuário). Secret VAZIO
	// continua aceito (slots auto-criados / webhook ainda sem secret), mas o
	// dispatcher fail-closa em qualquer secret < 16 — então nada é disparado
	// assinado com chave previsível.
	if req.Secret != "" && len(req.Secret) < 16 {
		httpx.WriteErr(w, http.StatusBadRequest,
			"secret muito curto — use ao menos 16 caracteres para uma assinatura HMAC segura")
		return
	}

	// Valida event_types contra whitelist — DT-CODE-02: exact match, nunca substring.
	// A whitelist cobre os dois conjuntos (order_status_* de expedição + motoboy_* de COD).
	for _, et := range req.EventTypes {
		if !allowedEventTypes[et] {
			httpx.WriteErr(w, http.StatusBadRequest,
				"tipo de evento inválido: "+et+
					" — expedição: order_status_enviado, order_status_entregue, order_status_cancelado, order_status_frustrado, order_status_em_rota, order_status_embalado, order_status_coletado"+
					" — cod: motoboy_agendado, motoboy_pre_agendado, motoboy_embalado, motoboy_enviado, motoboy_coletado, motoboy_completo, motoboy_frustrado, motoboy_a_caminho, motoboy_em_rota")
			return
		}
	}

	// `tipo` classifica o webhook (expedicao|cod). Vazio → 'expedicao' (default do schema).
	// Independente de event_types/product_id: a validação é só de pertinência ao conjunto.
	tipo := req.Tipo
	if tipo == "" {
		tipo = "expedicao"
	}
	if !allowedWebhookTipos[tipo] {
		httpx.WriteErr(w, http.StatusBadRequest,
			"tipo inválido: "+tipo+" — permitidos: expedicao, cod")
		return
	}

	etJSON, _ := json.Marshal(req.EventTypes)

	active := true
	if req.Active != nil {
		active = *req.Active
	}

	var id int64
	err := h.Pool.QueryRow(r.Context(),
		`INSERT INTO senderzz_portal_webhooks
		    (user_id, url, secret, active, event_types, shipping_class_id, tipo, product_id, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
		 RETURNING id`,
		u.ID, req.URL, req.Secret, active, etJSON, req.ShippingClassID, tipo, req.ProductID,
	).Scan(&id)
	if err != nil {
		slog.Error("[portal_webhooks] erro ao criar webhook", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	slog.Info("[portal_webhooks] webhook criado", "user_id", u.ID, "webhook_id", id, "url", req.URL)
	httpx.WriteOK(w, map[string]any{"id": id, "mensagem": "webhook criado com sucesso"})
}

// ── DELETE /portal/webhooks/{id} ─────────────────────────────────────────────

// Delete realiza soft-delete do webhook: url=”, active=false.
// Não usa hard-delete pois senderzz_pw_ensure_user_webhook_slots() recriaria o slot.
func (h *WebhookHandler) Delete(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	idStr := chi.URLParam(r, "id")
	webhookID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || webhookID <= 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}

	// Soft-delete: url='', active=false. Garante que o webhook pertence ao usuário.
	result, err := h.Pool.Exec(r.Context(),
		`UPDATE senderzz_portal_webhooks
		    SET url = '', active = false, updated_at = NOW()
		  WHERE id = $1 AND user_id = $2`,
		webhookID, u.ID,
	)
	if err != nil {
		slog.Error("[portal_webhooks] erro ao deletar webhook", "user_id", u.ID, "webhook_id", webhookID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	if result.RowsAffected() == 0 {
		httpx.WriteErr(w, http.StatusNotFound, "webhook não encontrado")
		return
	}

	slog.Info("[portal_webhooks] soft-delete aplicado", "user_id", u.ID, "webhook_id", webhookID)
	httpx.WriteOK(w, map[string]any{"mensagem": "webhook removido com sucesso"})
}

// ── GET /portal/webhooks/{id}/history ────────────────────────────────────────

// History retorna o histórico de disparos do webhook.
func (h *WebhookHandler) History(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	idStr := chi.URLParam(r, "id")
	webhookID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || webhookID <= 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}

	// Garante que o webhook pertence ao usuário antes de retornar o histórico.
	// Carrega também a URL para decidir se podemos refletir o response_body bruto.
	var ownerID int64
	var whURL string
	errOwner := h.Pool.QueryRow(r.Context(),
		`SELECT user_id, COALESCE(url,'') FROM senderzz_portal_webhooks WHERE id = $1`,
		webhookID,
	).Scan(&ownerID, &whURL)
	if errOwner == pgx.ErrNoRows {
		httpx.WriteErr(w, http.StatusNotFound, "webhook não encontrado")
		return
	}
	if errOwner != nil {
		slog.Error("[portal_webhooks] erro ao verificar ownership", "user_id", u.ID, "webhook_id", webhookID, "err", errOwner)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	if ownerID != u.ID {
		httpx.WriteErr(w, http.StatusNotFound, "webhook não encontrado")
		return
	}

	// P0 SSRF (leitura): o response_body no log é a resposta CRUA do endpoint de
	// destino. Se o destino NÃO for externo (URL apontando p/ interno, ou linha
	// soft-deletada com url=''), refletir esse corpo vira um primitivo de leitura
	// SSRF (ler resposta de serviço interno via histórico). Só devolvemos o corpo
	// bruto quando o destino resolve EXCLUSIVAMENTE p/ IP público; caso contrário,
	// omitimos (response_body=nil) mantendo response_code/event_type p/ diagnóstico.
	reflectBody := destIsExternal(whURL)

	limit := 50
	// N+1: busca 1 a mais que o teto p/ detectar truncamento sem COUNT. // PERF-list-endpoints-hard-limit
	rows, err := h.Pool.Query(r.Context(),
		`SELECT id, event_type, response_code, response_body, created_at
		   FROM senderzz_webhook_log
		  WHERE webhook_id = $1
		  ORDER BY created_at DESC
		  LIMIT $2`,
		webhookID, limit+1,
	)
	if err != nil {
		slog.Error("[portal_webhooks] erro ao buscar histórico", "webhook_id", webhookID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer rows.Close()

	var entries []webhookLogEntry
	for rows.Next() {
		var e webhookLogEntry
		// created_at é TIMESTAMPTZ — mesmo motivo do List: escanear em time.Time
		// e formatar em RFC3339 (evita o 500 que ocorria quando havia disparos).
		var createdAt time.Time
		if err := rows.Scan(&e.ID, &e.EventType, &e.ResponseCode, &e.ResponseBody, &createdAt); err != nil {
			slog.Error("[portal_webhooks] erro ao ler log", "webhook_id", webhookID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao ler histórico")
			return
		}
		e.CreatedAt = createdAt.Format(time.RFC3339)
		// P0 SSRF: omite o corpo bruto quando o destino não é externo (anti-leitura).
		if !reflectBody {
			e.ResponseBody = nil
		}
		entries = append(entries, e)
	}

	if entries == nil {
		entries = []webhookLogEntry{}
	}

	// has_more=true quando veio a linha extra → o front sabe que o histórico foi
	// truncado (antes a truncagem em 50 era silenciosa). Devolve só o teto. // PERF-list-endpoints-hard-limit
	hasMore := len(entries) > limit
	if hasMore {
		entries = entries[:limit]
	}

	httpx.WriteOK(w, map[string]any{"data": entries, "total": len(entries), "has_more": hasMore, "limit": limit})
}

// ── POST /portal/webhooks/clear-history ──────────────────────────────────────

// ClearHistory deleta todos os logs dos webhooks do usuário autenticado.
func (h *WebhookHandler) ClearHistory(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	// Body pode conter webhook_id opcional para limpar só um webhook.
	var body struct {
		WebhookID *int64 `json:"webhook_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	var deleted int64

	if body.WebhookID != nil {
		// Garante que o webhook pertence ao usuário.
		var ownerID int64
		errScan := h.Pool.QueryRow(r.Context(),
			`SELECT user_id FROM senderzz_portal_webhooks WHERE id = $1`,
			*body.WebhookID,
		).Scan(&ownerID)
		if errScan == pgx.ErrNoRows || ownerID != u.ID {
			httpx.WriteErr(w, http.StatusNotFound, "webhook não encontrado")
			return
		}
		if errScan != nil {
			slog.Error("[portal_webhooks] erro ao verificar ownership do webhook", "user_id", u.ID, "err", errScan)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
			return
		}

		result, err2 := h.Pool.Exec(r.Context(),
			`DELETE FROM senderzz_webhook_log WHERE webhook_id = $1`,
			*body.WebhookID,
		)
		if err2 != nil {
			slog.Error("[portal_webhooks] erro ao limpar log por webhook_id", "user_id", u.ID, "err", err2)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
			return
		}
		deleted = result.RowsAffected()
	} else {
		// Limpa todos os logs dos webhooks do usuário.
		result, err2 := h.Pool.Exec(r.Context(),
			`DELETE FROM senderzz_webhook_log
			  WHERE webhook_id IN (
			      SELECT id FROM senderzz_portal_webhooks WHERE user_id = $1
			  )`,
			u.ID,
		)
		if err2 != nil {
			slog.Error("[portal_webhooks] erro ao limpar todos os logs", "user_id", u.ID, "err", err2)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
			return
		}
		deleted = result.RowsAffected()
	}

	slog.Info("[portal_webhooks] histórico limpo", "user_id", u.ID, "registros_deletados", deleted)
	httpx.WriteOK(w, map[string]any{"registros_deletados": deleted})
}

// ── POST /portal/webhooks/{id}/test ──────────────────────────────────────────── // FEAT-PORTAL

// Test dispara um POST de teste SÍNCRONO ao webhook configurado do usuário e
// devolve o response_code para feedback imediato na UI.
//
// POR QUE SÍNCRONO (e não enqueue via Asynq): o client Asynq não está fiado aos
// handlers (main.go: `_ = asynqClient`) e o worker só roda com Redis up — enfileirar
// um "teste" no-op silenciosamente quando Redis está fora seria enganoso. Aqui
// fazemos o POST direto, reusando o MESMO esquema de assinatura do dispatcher
// (X-Senderzz-Signature: sha256=HMAC-SHA256(payload, secret)) e logamos em
// senderzz_webhook_log — exatamente como o disparo assíncrono.
func (h *WebhookHandler) Test(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	webhookID, ok := parsePathID(r)
	if !ok {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}

	url, secret, active, found := h.loadOwnedWebhook(r, u.ID, webhookID)
	if !found {
		httpx.WriteErr(w, http.StatusNotFound, "webhook não encontrado")
		return
	}
	if url == "" || !active {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "webhook inativo ou sem URL configurada")
		return
	}

	// Payload de teste (espelha o evento mínimo do dispatcher).
	payload, _ := json.Marshal(map[string]any{
		"event":   "webhook.test",
		"test":    true,
		"message": "Disparo de teste do Senderzz",
		"sent_at": time.Now().UTC().Format(time.RFC3339),
	})

	// NOTA SEGURANÇA: a URL do webhook é controlada pelo usuário → NÃO refletimos o
	// corpo da resposta do endpoint na API (evita um primitivo de leitura SSRF: apontar
	// o webhook a um endereço interno e ler a resposta). O body fica só no log interno
	// (senderzz_webhook_log) p/ diagnóstico. Devolvemos apenas o response_code + ok.
	code, ok2xx, derr := h.dispatchSync(r, webhookID, "webhook.test", string(payload), url, secret)
	if derr != "" {
		httpx.WriteErr(w, http.StatusBadGateway, "falha ao contatar o endpoint: "+derr)
		return
	}

	slog.Info("[portal_webhooks] webhook de teste disparado", "user_id", u.ID, "webhook_id", webhookID, "status", code)
	httpx.WriteOK(w, map[string]any{
		"mensagem":      "webhook de teste disparado",
		"response_code": code,
		"success":       ok2xx,
	})
}

// ── POST /portal/webhooks/{id}/resend ────────────────────────────────────────── // FEAT-PORTAL

// Resend reenvia o ÚLTIMO payload registrado em senderzz_webhook_log para o webhook
// do usuário (mesmo POST síncrono do Test). Sem histórico → 404.
func (h *WebhookHandler) Resend(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	webhookID, ok := parsePathID(r)
	if !ok {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}

	url, secret, active, found := h.loadOwnedWebhook(r, u.ID, webhookID)
	if !found {
		httpx.WriteErr(w, http.StatusNotFound, "webhook não encontrado")
		return
	}
	if url == "" || !active {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "webhook inativo ou sem URL configurada")
		return
	}

	// Carrega o último payload + event_type do histórico do webhook.
	var eventType string
	var payloadRaw []byte
	err := h.Pool.QueryRow(r.Context(),
		`SELECT event_type, payload FROM senderzz_webhook_log
		  WHERE webhook_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1`,
		webhookID,
	).Scan(&eventType, &payloadRaw)
	if err == pgx.ErrNoRows {
		httpx.WriteErr(w, http.StatusNotFound, "nenhum disparo anterior para reenviar")
		return
	}
	if err != nil {
		slog.Error("[portal_webhooks] erro ao carregar último payload", "user_id", u.ID, "webhook_id", webhookID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Mesma nota de segurança do Test: não refletimos o corpo da resposta do endpoint
	// (URL controlada pelo usuário → primitivo de leitura SSRF). Só response_code + ok.
	code, ok2xx, derr := h.dispatchSync(r, webhookID, eventType, string(payloadRaw), url, secret)
	if derr != "" {
		httpx.WriteErr(w, http.StatusBadGateway, "falha ao contatar o endpoint: "+derr)
		return
	}

	slog.Info("[portal_webhooks] webhook reenviado", "user_id", u.ID, "webhook_id", webhookID, "event", eventType, "status", code)
	httpx.WriteOK(w, map[string]any{
		"mensagem":      "webhook reenviado",
		"event_type":    eventType,
		"response_code": code,
		"success":       ok2xx,
	})
}

// ── Helpers internos (test/resend) ─────────────────────────────────────────────

// parsePathID extrai e valida o {id} (>0) da rota.
func parsePathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// loadOwnedWebhook carrega url/secret/active de um webhook GARANTINDO que pertence
// ao usuário (user_id = ownerID). found=false → não existe OU não é do usuário (404).
func (h *WebhookHandler) loadOwnedWebhook(r *http.Request, ownerID, webhookID int64) (url, secret string, active, found bool) {
	err := h.Pool.QueryRow(r.Context(),
		`SELECT url, COALESCE(secret,''), active
		   FROM senderzz_portal_webhooks
		  WHERE id = $1 AND user_id = $2`,
		webhookID, ownerID,
	).Scan(&url, &secret, &active)
	if err != nil {
		return "", "", false, false
	}
	return url, secret, active, true
}

// dispatchSync faz um POST síncrono ao endpoint do webhook com o MESMO esquema do
// jobs.WebhookDispatcher (HMAC-SHA256 em X-Senderzz-Signature) e registra o
// resultado em senderzz_webhook_log. Retorna (response_code, ok2xx, errMsg);
// errMsg != "" indica falha de rede/timeout (não houve resposta HTTP).
//
// SEGURANÇA: o corpo da resposta do endpoint é lido APENAS para o log interno e
// NÃO é retornado ao chamador — a URL é controlada pelo usuário (primitivo SSRF
// de leitura). O caller só recebe o status code + flag de 2xx.
func (h *WebhookHandler) dispatchSync(r *http.Request, webhookID int64, eventType, payload, urlStr, secret string) (int, bool, string) {
	// P0 SSRF (defesa em profundidade): revalida o destino NO MOMENTO do disparo.
	// O Create já valida, mas (a) linhas salvas antes deste patch podem ter destino
	// interno e (b) o DNS pode ter mudado desde o cadastro. Barrar aqui evita que o
	// POST síncrono (Test/Resend) seja usado como proxy de leitura interno.
	if err := validatePublicWebhookURL(urlStr); err != nil {
		slog.Warn("[portal_webhooks] disparo bloqueado (SSRF)", "webhook_id", webhookID, "url", urlStr, "motivo", err.Error())
		return 0, false, err.Error()
	}

	sig := hmacSha256Hex(payload, secret)

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, urlStr, bytes.NewBufferString(payload))
	if err != nil {
		return 0, false, err.Error()
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Senderzz-Webhook/1.0")
	req.Header.Set("X-Senderzz-Event", eventType)
	req.Header.Set("X-Senderzz-Signature", "sha256="+sig)
	req.Header.Set("X-Senderzz-Webhook-ID", fmt.Sprintf("%d", webhookID))

	// Client endurecido: DialContext.Control bloqueia rebinding p/ IP interno e
	// CheckRedirect barra redirect p/ destino interno.
	client := newSSRFSafeClient(webhookTestTimeout)
	resp, err := client.Do(req)
	if err != nil {
		h.logDispatch(r, webhookID, eventType, payload, 0, err.Error())
		return 0, false, err.Error()
	}
	defer resp.Body.Close()

	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n]) // só p/ o log interno — não retornado ao chamador.

	h.logDispatch(r, webhookID, eventType, payload, resp.StatusCode, body)
	ok2xx := resp.StatusCode >= 200 && resp.StatusCode < 300
	return resp.StatusCode, ok2xx, ""
}

// logDispatch registra o disparo síncrono em senderzz_webhook_log (best-effort).
func (h *WebhookHandler) logDispatch(r *http.Request, webhookID int64, eventType, payload string, code int, body string) {
	payloadJSON, _ := json.Marshal(json.RawMessage(payload))
	var rcCode *int
	if code > 0 {
		rc := code
		rcCode = &rc
	}
	_, err := h.Pool.Exec(r.Context(),
		`INSERT INTO senderzz_webhook_log
		    (webhook_id, event_type, payload, response_code, response_body)
		 VALUES ($1, $2, $3, $4, $5)`,
		webhookID, eventType, payloadJSON, rcCode, body,
	)
	if err != nil {
		slog.Error("[portal_webhooks] erro ao registrar log de disparo síncrono", "webhook_id", webhookID, "err", err)
	}
}

// hmacSha256Hex calcula HMAC-SHA256(payload, secret) em hex — mesmo esquema do
// jobs.WebhookDispatcher.calcularAssinatura (sha256=<hex>, compatível com GitHub).
func hmacSha256Hex(payload, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// ─────────────────────────────────────────────────────────────────────────────
// P0 SSRF — defesa de egress para webhooks do Portal.
//
// O guard de egress EXISTE em go/admin (expedicao_webhooks.go) e go/cron
// (dispatch/webhook.go) mas ESTAVA AUSENTE aqui. A URL do webhook é controlada
// pelo usuário; sem validação, um afiliado/produtor poderia apontar o webhook
// para serviços internos (loopback 127.0.0.1, RFC1918 10./192.168./172.16-31.,
// link-local/metadata 169.254.169.254, CGNAT 100.64/10) e usar o servidor como
// proxy de leitura/scanner interno.
//
// Replicamos a lógica localmente (convenção da task: NÃO importar cross-módulo).
// Diferente de admin/cron — que só validam a URL no momento do cadastro/disparo —
// aqui adicionamos DUAS defesas de runtime que fecham o gap de DNS-rebinding
// (TOCTOU entre o LookupIPAddr da validação e o Dial real) e de redirect:
//   - DialContext.Control(): rejeita o IP NO MOMENTO da conexão (após o resolve
//     real do dialer), então um host que troca de IP entre validação e disparo
//     ainda é bloqueado.
//   - CheckRedirect: rejeita redirect cujo Location aponte p/ destino interno.
// ─────────────────────────────────────────────────────────────────────────────

// errBlockedInternalDest — sinaliza tentativa de conexão a destino interno/privado.
var errBlockedInternalDest = errors.New("destino interno/privado bloqueado (SSRF)")

// isBlockedIP — true quando o IP pertence a faixa que NUNCA deve ser alvo de
// webhook externo (loopback, privado, link-local, metadata, CGNAT, não-especificado,
// multicast). Cobre IPv4 e IPv6. Réplica local de
// go/admin/internal/handlers/expedicao_webhooks.go::isBlockedIP (não importamos
// cross-módulo — convenção da task).
//
// EXCEÇÃO DE TESTE: se WEBHOOK_ALLOW_LOOPBACK=1, loopback é permitido (smoke local).
// Lido via os.Getenv no momento da chamada — não cacheado — espelha o cron.
func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true // não resolveu → fail-closed
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
	// 100.64.0.0/10 — CGNAT (RFC 6598), não coberto por IsPrivate().
	if v4 := ip.To4(); v4 != nil {
		if v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
			return true
		}
	}
	return false
}

// validatePublicWebhookURL valida scheme/host e RESOLVE o host, rejeitando destino
// interno/privado ANTES de qualquer request HTTP. Espelha
// expedicao_webhooks.go::validatePublicURL / cron webhook.go::validatePublicURL.
// Um host que resolve p/ múltiplos IPs é rejeitado se QUALQUER IP for bloqueado
// (evita rebinding parcial). Renomeada (sufixo Webhook) p/ não colidir caso o
// pacote ganhe outra validatePublicURL no futuro.
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
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("url sem host")
	}

	// Host já é IP literal? Valida direto, sem DNS.
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

// destIsExternal — true quando o destino da URL resolve EXCLUSIVAMENTE para IP
// público (passa em validatePublicWebhookURL). Usado pelo /history p/ decidir se
// pode refletir o response_body bruto (só destino externo) ou se deve omiti-lo
// (destino interno → primitivo de leitura SSRF).
func destIsExternal(raw string) bool {
	return validatePublicWebhookURL(raw) == nil
}

// ssrfSafeControl é o hook Control de net.Dialer: roda APÓS o resolve real do
// dialer, com o endereço já no formato ip:port. Rejeita a conexão se o IP for
// interno/privado — fecha a janela de DNS-rebinding (TOCTOU) que a validação de
// cadastro sozinha não cobre.
func ssrfSafeControl(_ string, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return errBlockedInternalDest // formato inesperado → fail-closed
	}
	ip := net.ParseIP(host)
	if isBlockedIP(ip) {
		return errBlockedInternalDest
	}
	return nil
}

// newSSRFSafeClient devolve um *http.Client endurecido contra SSRF:
//   - DialContext.Control bloqueia IP interno NO MOMENTO da conexão (anti-rebinding).
//   - CheckRedirect rejeita redirect cujo destino seja interno (anti-redirect-SSRF).
//
// timeout é o timeout total do request (mesmo papel do http.Client.Timeout atual).
func newSSRFSafeClient(timeout time.Duration) *http.Client {
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
			// Cada salto de redirect deve apontar p/ destino externo. O Control do
			// dialer ainda revalida o IP na conexão; isto barra cedo (e barra
			// redirects p/ esquemas/hosts internos antes de discar).
			if err := validatePublicWebhookURL(req.URL.String()); err != nil {
				return fmt.Errorf("redirect bloqueado: %w", err)
			}
			return nil
		},
	}
}
