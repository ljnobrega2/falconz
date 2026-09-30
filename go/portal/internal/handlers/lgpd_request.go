// Package handlers — LGPD: CANAL PÚBLICO de exercício dos direitos do titular.
//
// Rota (PÚBLICA, sem autenticação — namespace /wp-json/senderzz/v1):
//
//	POST /portal/data-request  — body {email, request_type, reason}
//
// ── Por que PÚBLICO (sem auth) ───────────────────────────────────────────────
// AUDIT LGPD-no-data-subject-channel (P0): a plataforma trata PII de pessoas que
// NÃO têm conta no portal — o CLIENTE FINAL/comprador, o RECEBEDOR da entrega e o
// MOTOBOY. A LGPD (Art. 18) assegura a QUALQUER titular o exercício de direitos
// (confirmação/acesso, correção, exclusão, portabilidade, revogação, oposição,
// informação sobre compartilhamento) — não só a quem tem login. Um canal só
// autenticado deixaria esses titulares sem via de exercício. Por isso o endpoint
// é público e identifica o titular pelo e-mail informado.
//
// O pedido NÃO entrega dado pessoal aqui (só ABRE o protocolo); a verificação de
// identidade e o atendimento são feitos depois pelo operador/DPO (status
// 'recebido' → 'em_andamento' → 'concluido'/'negado'), dentro do SLA de 15 dias
// embutido no schema (sla_deadline default). Logo, ser público não vaza PII: só
// registra a intenção do titular e devolve um protocolo de acompanhamento.
//
// ── Defesas (endpoint público) ───────────────────────────────────────────────
//   - Whitelist ESTRITA de request_type (casa o CHECK chk_dsr_request_type do
//     schema 380-lgpd-completo.sql) — input fora da lista = 400, nunca chega ao
//     banco para estourar o CHECK.
//   - Validação de formato de e-mail (reusa emailRegex de settings.go).
//   - Rate-limit por IP (janela fixa) — barra flood/abuso do canal aberto.
//   - reason limitado em tamanho (evita payload gigante num endpoint sem auth).
//   - user_id resolvido best-effort por e-mail (liga o pedido ao cadastro quando
//     existe; NULL = titular não cadastrado, como o schema prevê).
//   - status fixado em 'recebido' (default do schema); sla_deadline pelo default.
package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/portal-service/internal/httpx"
)

// DataRequestHandler agrupa as dependências do canal público do titular.
// Construção idêntica aos demais handlers (Pool); o rate-limiter é interno e
// preguiçoso (lazy) para não exigir mudança de wiring no main.go.
type DataRequestHandler struct {
	Pool *pgxpool.Pool

	rlOnce sync.Once
	rl     *dsrRateLimiter
}

// dsrAllowedTypes — whitelist ESTRITA dos tipos de pedido do titular. Casa 1:1
// com o CHECK chk_dsr_request_type de senderzz_data_subject_requests
// (380-lgpd-completo.sql): acesso, correcao, exclusao, portabilidade,
// revogacao_consentimento, oposicao, info_compartilhamento (Art. 18 + Art. 8º §5º).
var dsrAllowedTypes = map[string]bool{
	"acesso":                  true, // Art. 18 I/II — confirmação e acesso
	"correcao":                true, // Art. 18 III — correção
	"exclusao":                true, // Art. 18 VI — eliminação
	"portabilidade":           true, // Art. 18 V — portabilidade
	"revogacao_consentimento": true, // Art. 8º §5º — revogação
	"oposicao":                true, // Art. 18 §2º — oposição
	"info_compartilhamento":   true, // Art. 18 VII — info de compartilhamento
}

// dsrMaxReasonLen limita o tamanho da justificativa (endpoint público sem auth).
const dsrMaxReasonLen = 4000

// dataRequestBody é o body de POST /portal/data-request.
type dataRequestBody struct {
	Email       string `json:"email"`
	RequestType string `json:"request_type"`
	Reason      string `json:"reason"`
}

// DataRequest abre um pedido de exercício de direito do titular (Art. 18).
// PÚBLICO: identifica o titular pelo e-mail informado; não exige login. Grava em
// senderzz_data_subject_requests com status='recebido' e devolve o PROTOCOLO (id)
// para acompanhamento. NÃO entrega dado pessoal (só abre o protocolo).
func (h *DataRequestHandler) DataRequest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// ── Rate-limit por IP (canal aberto) ──────────────────────────────────────
	ip := loginClientIP(r)
	if !h.limiter().allow(ip) {
		slog.Warn("[portal_lgpd] rate-limit no canal do titular", "ip", ip)
		httpx.WriteErr(w, http.StatusTooManyRequests,
			"Muitas solicitações. Tente novamente em alguns minutos.")
		return
	}

	// ── Parse + validação de input ────────────────────────────────────────────
	var body dataRequestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}
	email := strings.ToLower(strings.TrimSpace(body.Email))
	reqType := strings.ToLower(strings.TrimSpace(body.RequestType))
	reason := strings.TrimSpace(body.Reason)

	if email == "" || !emailRegex.MatchString(email) {
		httpx.WriteErr(w, http.StatusBadRequest, "Informe um e-mail válido.")
		return
	}
	// Whitelist estrita — input fora da lista NUNCA chega ao banco.
	if !dsrAllowedTypes[reqType] {
		httpx.WriteErr(w, http.StatusBadRequest, "Tipo de solicitação inválido.")
		return
	}
	if len(reason) > dsrMaxReasonLen {
		reason = reason[:dsrMaxReasonLen]
	}

	// ── Resolução best-effort de user_id por e-mail ───────────────────────────
	// Liga o pedido ao cadastro quando existe; NULL = titular não cadastrado
	// (o schema prevê user_id NULLable exatamente para esse caso). Falha/ausência
	// → NULL, sem bloquear a abertura do protocolo.
	var userID *int64
	var uid int64
	if scanErr := h.Pool.QueryRow(ctx,
		`SELECT id FROM senderzz_portal_users WHERE lower(email) = $1 LIMIT 1`,
		email,
	).Scan(&uid); scanErr == nil && uid > 0 {
		userID = &uid
	}

	// reason vazio → NULL (coluna é NULLable; '' poluiria a tela operacional).
	var reasonArg *string
	if reason != "" {
		reasonArg = &reason
	}

	// ── INSERT — status e sla_deadline pelos DEFAULTs do schema ───────────────
	// status='recebido' (default) e sla_deadline=NOW()+15d (default) NÃO são
	// passados de propósito: a fonte da verdade é o schema (380-lgpd-completo.sql).
	var protocolo int64
	err := h.Pool.QueryRow(ctx,
		`INSERT INTO senderzz_data_subject_requests (user_id, email, request_type, reason)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id`,
		userID, email, reqType, reasonArg,
	).Scan(&protocolo)
	if err != nil {
		if isUndefinedTable(err) {
			httpx.WriteErr(w, http.StatusServiceUnavailable, "recurso indisponível no momento")
			return
		}
		slog.Error("[portal_lgpd] erro ao abrir pedido do titular",
			"request_type", reqType, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "Erro ao registrar a solicitação.")
		return
	}

	slog.Info("[portal_lgpd] pedido do titular registrado",
		"protocolo", protocolo, "request_type", reqType, "cadastrado", userID != nil)
	httpx.WriteOK(w, map[string]any{
		"protocolo":    protocolo,
		"status":       "recebido",
		"request_type": reqType,
		"message": "Solicitação recebida. Você receberá retorno no e-mail informado " +
			"dentro do prazo legal (até 15 dias). Guarde o número de protocolo.",
	})
}

// limiter inicializa preguiçosamente o rate-limiter (sem mudar o wiring do main).
func (h *DataRequestHandler) limiter() *dsrRateLimiter {
	h.rlOnce.Do(func() {
		h.rl = newDSRRateLimiter()
	})
	return h.rl
}

// ── Rate-limiter por IP (janela fixa) ─────────────────────────────────────────
//
// Espelha o loginRateLimiter (login_ratelimit.go): token-bucket de janela fixa,
// SEM dependência nova. Só o eixo IP (o e-mail é fornecido pelo requerente e não
// é um identificador confiável de origem num canal aberto). Default folgado para
// não travar uso legítimo de um formulário público; ainda barra flood óbvio.

const (
	dsrRLWindow  = 15 * time.Minute
	dsrRLIPMax   = 20 // pedidos por IP por janela (canal público — folgado)
	dsrRLMapSize = 4096
)

// dsrRLEntry — contador de um IP na janela atual.
type dsrRLEntry struct {
	count   int
	resetAt time.Time
}

// dsrRateLimiter limita pedidos do canal público por IP (janela fixa).
type dsrRateLimiter struct {
	mu     sync.Mutex
	byIP   map[string]*dsrRLEntry
	ipMax  int
	window time.Duration
}

func newDSRRateLimiter() *dsrRateLimiter {
	return &dsrRateLimiter{
		byIP:   make(map[string]*dsrRLEntry),
		ipMax:  dsrRLIPMax,
		window: dsrRLWindow,
	}
}

// allow registra uma tentativa do IP e devolve false se passou do teto na janela.
// Limpeza oportunista para o mapa não crescer indefinidamente.
func (l *dsrRateLimiter) allow(ip string) bool {
	if ip == "" {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	e, ok := l.byIP[ip]
	if !ok || now.After(e.resetAt) {
		l.byIP[ip] = &dsrRLEntry{count: 1, resetAt: now.Add(l.window)}
		if len(l.byIP) > dsrRLMapSize {
			for k, v := range l.byIP {
				if now.After(v.resetAt) {
					delete(l.byIP, k)
				}
			}
		}
		return true
	}
	if e.count >= l.ipMax {
		return false
	}
	e.count++
	return true
}
