// Package handlers — handlers de integrações do Portal V2.
//
// Rotas (namespace /wp-json/senderzz/v1):
//
//	GET    /portal/integrations          — retorna flags + mapping de integração do usuário
//	POST   /portal/integrations/toggle    — alterna uma flag (whitelist de chaves)
//	DELETE /portal/integrations/logs      — limpa log de integração do usuário
//	POST   /portal/integrations/mapping   — salva mapping_json (campo → caminho no payload)
//	POST   /portal/integrations/rotate    — gera/rotaciona a API key do produtor (POST /api/v1/orders)
//	POST   /portal/integrations/reprocess — reprocessa a última tentativa de pedido via API que falhou
//
// As flags de integração são armazenadas em senderzz_portal_users.integrations (JSONB).
// Schema esperado: {active, paused, require_paid, ignore_duplicates, auto_cheapest,
// mapping_json}. O GET devolve o blob inteiro — flags + mapping_json — e o front
// (Integrations.tsx) hidrata o mapeamento de integrations.mapping_json.
//
// Whitelist de chaves permitidas (espelha Portal_Page.php integrations_toggle):
//
//	active, paused, require_paid, ignore_duplicates, auto_cheapest
//
// Nota "Pausar recebimento":
//
//	O toggle de pausa envia key=active com valor INVERTIDO no PHP:
//	checked → valor 0 (pausa), unchecked → valor 1 (ativa).
//	O handler Go aceita qualquer combinação key+value — a lógica de inversão
//	fica no frontend (espelha comportamento PHP).
package handlers

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/portal-service/internal/auth"
	"github.com/senderzz/portal-service/internal/httpx"
)

// IntegrationsHandler agrupa dependências dos handlers de integração.
type IntegrationsHandler struct {
	Pool             *pgxpool.Pool
	OrdersServiceURL string
}

// integrationsRoleAllowed — RBAC-2 (docs/INTEGRACOES-AUDITORIA.md /
// IMPROVEMENT-PLAN-2026-06-18 P1.7): a integração de recebimento de pedidos é
// EXCLUSIVA do produtor. Guard POSITIVO (isProdutorRole, definido em
// motoboy_portal.go) — afiliado E operator/OL são barrados, espelhando
// SEC-RBAC-AFF-GOVERNANCE (gate só-afiliado deixava o operator passar).
// Os handlers de ESCRITA (Toggle/SaveMapping/ClearLogs) usam este gate; a
// leitura (List/GET) permanece aberta a qualquer papel autenticado.
func integrationsRoleAllowed(role string) bool {
	return isProdutorRole(role)
}

// allowedIntegrationKeys — whitelist de chaves de integração permitidas.
// Espelha Portal_Page.php integrations_toggle: allowed = ['active','paused',
// 'require_paid','ignore_duplicates','auto_cheapest'].
var allowedIntegrationKeys = map[string]bool{
	"active":            true,
	"paused":            true,
	"require_paid":      true,
	"ignore_duplicates": true,
	"auto_cheapest":     true,
}

// integrationToggleRequest é o body de POST /portal/integrations/toggle.
type integrationToggleRequest struct {
	Key   string `json:"key"`
	Value any    `json:"value"` // bool ou int (0/1 do PHP)
}

// ── GET /portal/integrations ──────────────────────────────────────────────────

// List retorna as flags de integração do usuário autenticado.
func (h *IntegrationsHandler) List(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	var integrationsRaw []byte
	err := h.Pool.QueryRow(r.Context(),
		`SELECT integrations FROM senderzz_portal_users WHERE id = $1`,
		u.ID,
	).Scan(&integrationsRaw)
	if err != nil {
		slog.Error("[portal_integrations] erro ao buscar integrations", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	var tokenPrefix *string
	var rotatedAt *string
	_ = h.Pool.QueryRow(r.Context(),
		`SELECT token_prefix, rotated_at::text FROM senderzz_producer_api_keys
		  WHERE producer_id = $1 AND active = true`,
		u.ID,
	).Scan(&tokenPrefix, &rotatedAt)

	httpx.WriteOK(w, map[string]any{
		"integrations":  json.RawMessage(integrationsRaw),
		"has_api_key":   tokenPrefix != nil,
		"token_prefix":  tokenPrefix,
		"rotated_at":    rotatedAt,
		"endpoint_url":  apiOrdersEndpointURL,
	})
}

// ── POST /portal/integrations/toggle ─────────────────────────────────────────

// Toggle alterna uma flag de integração do usuário.
// Rejeita chaves não whitelistadas para evitar escrita arbitrária no JSON.
func (h *IntegrationsHandler) Toggle(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	if !integrationsRoleAllowed(u.Role) {
		httpx.WriteErr(w, http.StatusForbidden, "apenas o produtor gerencia integrações")
		return
	}

	var req integrationToggleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}
	if req.Key == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "key é obrigatória")
		return
	}

	// Whitelist — rejeita chaves não permitidas.
	if !allowedIntegrationKeys[req.Key] {
		httpx.WriteErr(w, http.StatusBadRequest,
			"chave inválida: "+req.Key+
				" — permitidas: active, paused, require_paid, ignore_duplicates, auto_cheapest")
		return
	}

	// Atualiza apenas a chave específica no JSONB usando jsonb_set.
	// Serializa o value em JSON para injeção segura.
	valueJSON, err := json.Marshal(req.Value)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "valor inválido")
		return
	}

	_, err = h.Pool.Exec(r.Context(),
		`UPDATE senderzz_portal_users
		    SET integrations = jsonb_set(
		            COALESCE(integrations, '{}'),
		            $1::text[],
		            $2::jsonb,
		            true
		        )
		  WHERE id = $3`,
		[]string{req.Key}, // path para jsonb_set como array Postgres
		valueJSON,
		u.ID,
	)
	if err != nil {
		slog.Error("[portal_integrations] erro ao atualizar flag", "user_id", u.ID, "key", req.Key, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Registra no log de integração.
	logPayload, _ := json.Marshal(map[string]any{"key": req.Key, "value": req.Value})
	_, _ = h.Pool.Exec(r.Context(),
		`INSERT INTO senderzz_integration_log (user_id, event, payload)
		 VALUES ($1, 'integration_toggle', $2)`,
		u.ID, logPayload,
	)

	slog.Info("[portal_integrations] flag atualizada", "user_id", u.ID, "key", req.Key, "value", req.Value)
	httpx.WriteOK(w, map[string]any{"key": req.Key, "value": req.Value})
}

// ── DELETE /portal/integrations/logs ─────────────────────────────────────────

// ClearLogs deleta todos os logs de integração do usuário autenticado.
// Espelha integrations_clear_logs de Portal_Page.php.
func (h *IntegrationsHandler) ClearLogs(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	if !integrationsRoleAllowed(u.Role) {
		httpx.WriteErr(w, http.StatusForbidden, "apenas o produtor gerencia integrações")
		return
	}

	result, err := h.Pool.Exec(r.Context(),
		`DELETE FROM senderzz_integration_log WHERE user_id = $1`,
		u.ID,
	)
	if err != nil {
		slog.Error("[portal_integrations] erro ao limpar logs", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	deleted := result.RowsAffected()
	slog.Info("[portal_integrations] logs limpos", "user_id", u.ID, "registros_deletados", deleted)
	httpx.WriteOK(w, map[string]any{"registros_deletados": deleted})
}

// ── POST /portal/integrations/rotate ─────────────────────────────────────────── // FEAT-PORTAL

// Rotate gera (ou re-gera) a API key do produtor pra receber pedidos via
// POST /api/v1/orders (orders-service). AUDIT-2026-07-28: migration 510 criou
// senderzz_producer_api_keys — 1 chave ativa por produtor, HASH sha256
// armazenado (nunca o valor em claro). O plaintext só existe nesta resposta,
// uma vez; depois disso é irrecuperável (mesmo padrão de todo secret do
// sistema — se perder, roda /rotate de novo).
func (h *IntegrationsHandler) Rotate(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	if !integrationsRoleAllowed(u.Role) {
		httpx.WriteErr(w, http.StatusForbidden, "apenas o produtor gerencia integrações")
		return
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		slog.Error("[portal_integrations] falha ao gerar chave", "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	plaintext := "sz_live_" + hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(plaintext))
	hash := hex.EncodeToString(sum[:])
	prefix := plaintext[:12]

	_, err := h.Pool.Exec(r.Context(),
		`INSERT INTO senderzz_producer_api_keys (producer_id, token_hash, token_prefix, active, rotated_at)
		 VALUES ($1, $2, $3, true, NOW())
		 ON CONFLICT (producer_id) DO UPDATE
		    SET token_hash = EXCLUDED.token_hash,
		        token_prefix = EXCLUDED.token_prefix,
		        active = true,
		        rotated_at = NOW()`,
		u.ID, hash, prefix,
	)
	if err != nil {
		slog.Error("[portal_integrations] falha ao salvar chave", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	logPayload, _ := json.Marshal(map[string]any{"token_prefix": prefix})
	_, _ = h.Pool.Exec(r.Context(),
		`INSERT INTO senderzz_integration_log (user_id, event, payload) VALUES ($1, 'api_key_rotate', $2)`,
		u.ID, logPayload,
	)

	slog.Info("[portal_integrations] chave de API rotacionada", "user_id", u.ID, "prefix", prefix)
	httpx.WriteOK(w, map[string]any{
		"token":        plaintext,
		"token_prefix": prefix,
		"endpoint_url": apiOrdersEndpointURL,
		"aviso":        "Guarde esta chave agora — ela não será mostrada de novo. Pra trocar, rotacione outra vez.",
	})
}

// apiOrdersEndpointURL — URL pública fixa do endpoint de recebimento de
// pedidos via API (orders-service, mesmo domínio do checkout). Fixo porque
// o contrato v1 é único (sem multi-tenant de path) — auth é 100% pela chave.
// AUDIT-2026-07-28: "api.falklog.com.br" NUNCA existiu (inventado sem checar
// DNS — NXDOMAIN, causou "Could not resolve host name" real testando ao
// vivo). Domínio público de verdade é app.falklog.com.br (ver /etc/cloudflared/
// falk.yml na VPS — só falklog.com.br/www/app/testes existem no túnel).
const apiOrdersEndpointURL = "https://app.falklog.com.br/api/v1/orders"

// ── POST /portal/integrations/reprocess ──────────────────────────────────────── // FEAT-PORTAL

// Reprocess reprocessaria o último payload de integração. NÃO MIGRADO → 501 PT-BR.
//
// POR QUE 501: a única fonte de "último payload" no espelho é
// senderzz_integration_log.payload, mas NÃO existe alvo de re-dispatch definido em
// Postgres (o pipeline de recebimento da integração — criar pedido a partir do
// payload — vive no WP/admin-ajax, não migrado). Sem um alvo fiel, reprocessar seria
// inventar comportamento; preferimos 501 explícito a fabricar um resultado.
// Reprocess pede pro orders-service reprocessar a ÚLTIMA tentativa de pedido
// via API (senderzz/producer_api_keys) que FALHOU pra este produtor — reusa o
// MESMO payload original, sem o cliente da integração precisar reenviar.
// AUDIT-2026-07-28: rota interna (rede Docker), producer_id = u.ID confiado
// porque JÁ validamos via JWT que quem chamou é o produtor dono.
func (h *IntegrationsHandler) Reprocess(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	if !integrationsRoleAllowed(u.Role) {
		httpx.WriteErr(w, http.StatusForbidden, "apenas o produtor gerencia integrações")
		return
	}
	if h.OrdersServiceURL == "" {
		httpx.WriteErr(w, http.StatusServiceUnavailable,
			"ORDERS_SERVICE_URL não configurado; configure a variável de ambiente e reinicie o portal-service")
		return
	}

	body, _ := json.Marshal(map[string]any{"producer_id": u.ID})
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost,
		strings.TrimRight(h.OrdersServiceURL, "/")+"/internal/api-orders/reprocess-last",
		bytes.NewReader(body))
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao montar requisição")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		slog.Error("[portal_integrations] falha ao contatar orders-service", "err", err)
		httpx.WriteErr(w, http.StatusBadGateway, "falha ao contatar orders-service: "+err.Error())
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	logPayload, _ := json.Marshal(map[string]any{"status": resp.StatusCode})
	_, _ = h.Pool.Exec(r.Context(),
		`INSERT INTO senderzz_integration_log (user_id, event, payload) VALUES ($1, 'reprocess_last', $2)`,
		u.ID, logPayload,
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(respBody)
}

// ── POST /portal/integrations/mapping ─────────────────────────────────────────── // FEAT-PORTAL

// mappingRequest — body de POST /portal/integrations/mapping.
// mapping_json: { campo_senderzz: caminho.no.payload } — mesmo formato que
// settings/integrations.php envia (szV2IntSaveMapping serializa o objeto).
type mappingRequest struct {
	MappingJSON map[string]string `json:"mapping_json"`
}

// SaveMapping persiste o mapeamento de campos (campo Senderzz → caminho no payload
// da plataforma) do usuário autenticado.
//
// PORTE FIEL (revisão AUDIT-2026-06-18): no WP o mapping vive na chave mapping_json
// da MESMA linha de integração do usuário que carrega token/active/flags
// (integrations.php lê $sz9in_int['mapping_json']). No espelho PG essa linha é o
// JSONB senderzz_portal_users.integrations — que JÁ guarda as flags (active,
// require_paid, ...). Gravar mapping_json ali é portar o desenho do WP, não inventar:
// é configuração forward (o WP a persiste independente de já ter recebido pedido).
// GET /portal/integrations devolve o blob inteiro, então o mapping volta no mesmo
// campo `integrations.mapping_json` que o front (Integrations.tsx) já hidrata.
//
// (rotate/reprocess seguem 501: dependem do token e do pipeline de recebimento —
//  infra que NÃO existe no espelho. Mapping é só armazenamento de config, que existe.)
func (h *IntegrationsHandler) SaveMapping(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	if !integrationsRoleAllowed(u.Role) {
		httpx.WriteErr(w, http.StatusForbidden, "apenas o produtor gerencia integrações")
		return
	}

	var req mappingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}

	// Remove entradas vazias (espelha o clean do front antes de enviar). Garante
	// objeto não-nil p/ serializar como {} (e não null) no JSONB.
	clean := map[string]string{}
	for k, v := range req.MappingJSON {
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k != "" && v != "" {
			clean[k] = v
		}
	}

	mappingJSON, err := json.Marshal(clean)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "mapeamento inválido")
		return
	}

	// jsonb_set na chave 'mapping_json' do blob de integração (cria o blob se ausente).
	_, err = h.Pool.Exec(r.Context(),
		`UPDATE senderzz_portal_users
		    SET integrations = jsonb_set(
		            COALESCE(integrations, '{}'),
		            '{mapping_json}',
		            $1::jsonb,
		            true
		        )
		  WHERE id = $2`,
		mappingJSON, u.ID,
	)
	if err != nil {
		slog.Error("[portal_integrations] erro ao salvar mapeamento", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Registra no log de auditoria (mesmo padrão de Toggle).
	logPayload, _ := json.Marshal(map[string]any{"mapping_json": clean})
	_, _ = h.Pool.Exec(r.Context(),
		`INSERT INTO senderzz_integration_log (user_id, event, payload)
		 VALUES ($1, 'integration_mapping', $2)`,
		u.ID, logPayload,
	)

	slog.Info("[portal_integrations] mapeamento salvo", "user_id", u.ID, "campos", len(clean))
	httpx.WriteOK(w, map[string]any{"mapping_json": clean})
}
