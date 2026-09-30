// Package handlers — handler de webhook PIX.
//
// PostPixWebhook valida a assinatura HMAC-SHA256 do payload recebido e,
// se status=paid ou approved, credita a carteira do usuário de forma idempotente.
//
// Protocolo de assinatura (espelha tpc_validar_assinatura_webhook em webhook.php):
//   - Header enviado pelo provedor: X-Signature (ou X-Hub-Signature)
//   - Formato: "sha256=" + hex(HMAC-SHA256(rawBody, WEBHOOK_SECRET))
//   - Fail-closed: WEBHOOK_SECRET vazio → 503 (não 401) para não expor misconfiguration.
//
// Resolução de user_id (espelha tpc_webhook_pix_handler em webhook.php):
//  1. recarga_id vem no query-param ou em payload.metadata.recarga_id.
//  2. Se ausente, faz lookup: SELECT id FROM tpc_recargas WHERE me_pix_id = $1.
//  3. Carrega a linha da recarga para obter user_id e valor esperado.
//  4. Valida divergência de valor (tolerância ±0.01).
//  5. Credita e marca a recarga como confirmada atomicamente.
//
// Idempotência:
//   - tpc_webhook_events com UNIQUE KEY uq_event_key garante que o mesmo evento
//     não seja creditado duas vezes.
//   - O crédito usa ON CONFLICT (user_id, referencia, tipo) DO NOTHING.
//
// Status (M-03 — whitelists explícitas, espelham pix.php:664-677):
//   - pago: paid/pago/approved/aprovado/confirmed/confirmado/paid_out/success/
//     completed/concluido/concluído/authorized/autorizado → credita.
//   - cancelado: cancelled/canceled/cancelado/expired/expirado/failed/falhou/
//     refused/recusado → cancela a recarga (não rejeita silencioso).
//   - analise: analysis/analise/under_review/review/... → mantém pendente, 200.
//
// Nunca usar strings.Contains nas whitelists — match exato (CRIT-04).
package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/wallet-service/internal/httpx"
	"github.com/shopspring/decimal"
)

// PixHandler agrupa as dependências do handler PIX.
type PixHandler struct {
	db *pgxpool.Pool
	rl *pixWebhookRateLimiter
}

// NewPixHandler cria um PixHandler com o pool de conexões fornecido.
func NewPixHandler(db *pgxpool.Pool) *PixHandler {
	return &PixHandler{db: db, rl: newPixWebhookRateLimiter()}
}

// ─── Rate limiter do webhook PIX (M-03) ───────────────────────────────────────
//
// Porte do rate-limit de tpc_webhook_pix_handler (webhook.php:60-65):
//   - chave por IP, janela de 60s, máximo 30 requisições → 429.
//   - aplicado ANTES da validação de assinatura (mesma ordem do WP).
//
// O WP usa transients (compartilhados entre processos via DB/cache); aqui o
// serviço Go é single-instance, então um contador em memória com mutex.
// Diferença sutil (imaterial — ambos limitam 30/min): o WP estende o TTL a cada
// escrita (reset só após 60s de silêncio); este usa janela fixa a partir da 1ª
// requisição. O teto de 30/min é idêntico.
const pixWebhookMaxPorMinuto = 30

type pixRLEntry struct {
	count   int
	resetAt time.Time
}

type pixWebhookRateLimiter struct {
	mu      sync.Mutex
	entries map[string]*pixRLEntry
}

func newPixWebhookRateLimiter() *pixWebhookRateLimiter {
	return &pixWebhookRateLimiter{entries: make(map[string]*pixRLEntry)}
}

// allow incrementa o contador do IP na janela atual e retorna false quando o
// limite (30/min) foi atingido — espelha "if ($cnt >= 30) return 429" do PHP.
func (l *pixWebhookRateLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	e, ok := l.entries[ip]
	if !ok || now.After(e.resetAt) {
		// Nova janela de 60s para este IP.
		l.entries[ip] = &pixRLEntry{count: 1, resetAt: now.Add(time.Minute)}
		// Limpeza oportunista de entradas expiradas (evita crescimento ilimitado do mapa).
		if len(l.entries) > 1024 {
			for k, v := range l.entries {
				if now.After(v.resetAt) {
					delete(l.entries, k)
				}
			}
		}
		return true
	}
	// Janela vigente: bloqueia se já atingiu o teto antes de incrementar (>= 30).
	if e.count >= pixWebhookMaxPorMinuto {
		return false
	}
	e.count++
	return true
}

// clientIP extrai o IP do request para chavear o rate limit.
// O chi RealIP middleware já normaliza r.RemoteAddr a partir de X-Forwarded-For/
// X-Real-IP; aqui apenas removemos a porta quando presente.
func clientIP(r *http.Request) string {
	addr := r.RemoteAddr
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	if addr == "" {
		return "unknown"
	}
	return addr
}

// pixWebhookPayload representa o corpo esperado do webhook PIX do Melhor Envio.
// Campos flexíveis para acomodar variações do payload (espelha tpc_webhook_pix_handler).
type pixWebhookPayload struct {
	ID        string          `json:"id"`
	PaymentID string          `json:"payment_id"`
	Status    string          `json:"status"`
	Amount    decimal.Decimal `json:"amount"`
	Value     decimal.Decimal `json:"value"`
	Price     decimal.Decimal `json:"price"`
	Total     decimal.Decimal `json:"total"`
	// Aninhamento alternativo: payload.payment.{id,status,amount}
	Payment *struct {
		ID     string          `json:"id"`
		Status string          `json:"status"`
		Amount decimal.Decimal `json:"amount"`
	} `json:"payment,omitempty"`
	// Metadados injetados por quem criou o PIX (recarga_id pode vir aqui).
	Metadata *struct {
		RecargaID int64 `json:"recarga_id"`
		UserID    int64 `json:"user_id"`
	} `json:"metadata,omitempty"`
}

// recargaRow representa os campos relevantes de tpc_recargas para o webhook.
type recargaRow struct {
	ID      int64
	UserID  int64
	Valor   decimal.Decimal
	Status  string
	MePixID string
}

// statusPago é o conjunto explícito de valores que indicam pagamento confirmado.
// Nunca usar strings.Contains — whitelist exato para evitar o bypass descrito em CRIT-04.
//
// M-03 — Porte FIEL de tpc_pix_status_is_paid (pix.php:664-666): o WP aceita 13
// estados que indicam pagamento concluído (pt/en). O Go aceitava apenas
// {paid,approved}, deixando passar silenciosamente estados válidos como
// "approved"/"aprovado"/"confirmed" etc. Aqui replicamos a lista completa.
var statusPago = map[string]bool{
	"paid":       true,
	"pago":       true,
	"approved":   true,
	"aprovado":   true,
	"confirmed":  true,
	"confirmado": true,
	"paid_out":   true,
	"success":    true,
	"completed":  true,
	"concluido":  true,
	"concluído":  true,
	"authorized": true,
	"autorizado": true,
}

// statusCancelado — Porte FIEL de tpc_pix_status_is_cancelled (pix.php:668-670).
// Estados que indicam que a cobrança falhou/foi cancelada/expirou. O webhook
// NÃO deve rejeitar silenciosamente esses eventos: cancela a recarga e responde 200.
var statusCancelado = map[string]bool{
	"cancelled": true,
	"canceled":  true,
	"cancelado": true,
	"expired":   true,
	"expirado":  true,
	"failed":    true,
	"falhou":    true,
	"refused":   true,
	"recusado":  true,
}

// statusAnalise — Porte FIEL de tpc_pix_status_is_analysis (pix.php:672-677).
// Estados de análise antifraude: o pagamento ainda não foi confirmado nem
// recusado. O WP marca a recarga como 'analise' (aguardando) e o status endpoint
// devolve em_analise=true / "Em análise…".
//
// DIVERGÊNCIA CONHECIDA: o CHECK de tpc_recargas.status (schema-wallet.sql) NÃO
// inclui 'analise', então aqui a recarga permanece 'pendente' (não há crédito,
// resposta 200 "analise" no webhook). Consequência: GET /recarga/{id}/pix retorna
// em_analise=false / "Aguardando pagamento" onde o WP retornaria true. Fidelidade
// total exige adicionar 'analise' ao CHECK em infra/postgres/schema-wallet.sql
// (fora deste pacote). Nenhum código escreve 'analise', então não há violação de
// CHECK em runtime — é apenas um gap de fidelidade, não um crash.
var statusAnalise = map[string]bool{
	"analysis":           true,
	"analise":            true,
	"em analise":         true,
	"under_review":       true,
	"review":             true,
	"aguardando analise": true,
}

// isStatusPago / isStatusCancelado / isStatusAnalise normalizam (trim+lower) e
// consultam as whitelists. Espelham as funções tpc_pix_status_is_* do PHP.
func isStatusPago(s string) bool {
	return statusPago[strings.ToLower(strings.TrimSpace(s))]
}

func isStatusCancelado(s string) bool {
	return statusCancelado[strings.ToLower(strings.TrimSpace(s))]
}

// isStatusAnalise espelha tpc_pix_status_is_analysis: whitelist normalizada +
// fallback por substring no payload bruto ("aguardando analise"/"under_review").
// remove_accents do PHP é aproximado aqui por removeAccents (cobre os termos da lista).
func isStatusAnalise(s string, rawPayload []byte) bool {
	n := strings.ToLower(strings.TrimSpace(removeAccents(s)))
	if statusAnalise[n] {
		return true
	}
	// Fallback do PHP: procura a marca no JSON inteiro (sem acento, minúsculo).
	h := strings.ToLower(removeAccents(string(rawPayload)))
	return strings.Contains(h, "aguardando analise") || strings.Contains(h, "under_review")
}

// removeAccents remove os acentos relevantes para o matching de status PIX.
// Cobre os caracteres presentes nas listas (á é í ó ú â ê ô ã õ ç) — suficiente
// para "análise"/"concluído"/"aguardando análise" baterem com a whitelist sem acento.
func removeAccents(s string) string {
	repl := strings.NewReplacer(
		"á", "a", "à", "a", "ã", "a", "â", "a", "ä", "a",
		"é", "e", "ê", "e", "è", "e", "ë", "e",
		"í", "i", "î", "i", "ì", "i", "ï", "i",
		"ó", "o", "ô", "o", "õ", "o", "ò", "o", "ö", "o",
		"ú", "u", "û", "u", "ù", "u", "ü", "u",
		"ç", "c",
		"Á", "A", "À", "A", "Ã", "A", "Â", "A", "Ä", "A",
		"É", "E", "Ê", "E", "È", "E", "Ë", "E",
		"Í", "I", "Î", "I", "Ì", "I", "Ï", "I",
		"Ó", "O", "Ô", "O", "Õ", "O", "Ò", "O", "Ö", "O",
		"Ú", "U", "Û", "U", "Ù", "U", "Ü", "U",
		"Ç", "C",
	)
	return repl.Replace(s)
}

// PostPixWebhook processa webhooks de confirmação de PIX.
//
// Fluxo:
//  0. Rate limit por IP (30/min) — antes de tudo (espelha webhook.php:60-65).
//  1. Lê WEBHOOK_SECRET — 503 se ausente/curto (fail-closed).
//  2. Lê rawBody antes de parsear (necessário para HMAC).
//  3. Valida assinatura via X-Signature / X-Hub-Signature.
//  4. Parseia JSON e normaliza campos multi-nível.
//  5. Resolve recarga_id → linha em tpc_recargas → user_id + valor esperado.
//  6. Registra idempotência em tpc_webhook_events (DO NOTHING se já existe).
//  7. Verifica status ∈ {paid, approved}.
//  8. Valida divergência de valor (±0.01).
//  9. Credita carteira e marca recarga como confirmada atomicamente.
func (h *PixHandler) PostPixWebhook(w http.ResponseWriter, r *http.Request) {
	// Rate limit por IP (30/min) — PRIMEIRO, antes da validação de assinatura,
	// espelhando a ordem de tpc_webhook_pix_handler (webhook.php:60-65).
	ip := clientIP(r)
	if !h.rl.allow(ip) {
		reqLog(r).Warn("[tpc_webhook_pix] rate limit atingido", "ip", ip)
		httpx.WriteErr(w, http.StatusTooManyRequests, "rate_limited")
		return
	}

	secret := os.Getenv("WEBHOOK_SECRET")
	if len(secret) < 32 {
		// Fail-closed: sem secret configurado o endpoint não opera.
		// 503 em vez de 401 para não revelar a existência da rota sem auth.
		reqLog(r).Error("[tpc_webhook_pix] WEBHOOK_SECRET não configurado ou muito curto",
			"ip", r.RemoteAddr,
		)
		httpx.WriteErr(w, http.StatusServiceUnavailable, "serviço temporariamente indisponível")
		return
	}

	// Lê o corpo bruto antes de qualquer parsing — obrigatório para validar HMAC.
	rawBody, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MiB max
	if err != nil {
		reqLog(r).Error("[tpc_webhook_pix] erro ao ler corpo", "err", err)
		httpx.WriteErr(w, http.StatusBadRequest, "erro ao ler requisição")
		return
	}

	// ── Valida assinatura HMAC-SHA256 ────────────────────────────────────────
	// Espelha tpc_validar_assinatura_webhook: X-Signature ou X-Hub-Signature.
	// Formato esperado: "sha256=<hex_lowercase>"
	sig := r.Header.Get("X-Signature")
	if sig == "" {
		sig = r.Header.Get("X-Hub-Signature")
	}
	if sig == "" {
		reqLog(r).Warn("[tpc_webhook_pix] assinatura ausente", "ip", r.RemoteAddr)
		httpx.WriteErr(w, http.StatusUnauthorized, "assinatura inválida")
		return
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(rawBody)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	// Comparação em tempo constante (equivalente ao hash_equals do PHP).
	if !hmac.Equal([]byte(expected), []byte(sig)) {
		reqLog(r).Warn("[tpc_webhook_pix] assinatura inválida",
			"ip", r.RemoteAddr,
			"sig_prefix", safePrefix(sig, 16),
		)
		httpx.WriteErr(w, http.StatusUnauthorized, "assinatura inválida")
		return
	}

	// ── Parseia payload ───────────────────────────────────────────────────────
	var payload pixWebhookPayload
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		reqLog(r).Error("[tpc_webhook_pix] JSON inválido", "err", err)
		httpx.WriteErr(w, http.StatusBadRequest, "payload inválido")
		return
	}

	// Normaliza campos que podem vir aninhados em "payment".
	pixID := strings.TrimSpace(payload.ID)
	if pixID == "" {
		pixID = strings.TrimSpace(payload.PaymentID)
	}
	if pixID == "" && payload.Payment != nil {
		pixID = strings.TrimSpace(payload.Payment.ID)
	}

	status := strings.TrimSpace(strings.ToLower(payload.Status))
	if status == "" && payload.Payment != nil {
		status = strings.TrimSpace(strings.ToLower(payload.Payment.Status))
	}

	// Extrai valor usando a mesma precedência do tpc_webhook_extract_amount do PHP:
	// amount → value → price → total → payment.amount
	valor := firstNonZeroDecimal(payload.Amount, payload.Value, payload.Price, payload.Total)
	if valor.IsZero() && payload.Payment != nil {
		valor = payload.Payment.Amount
	}

	// Extrai recarga_id do query param ou do metadata.
	var recargaID int64
	if qp := r.URL.Query().Get("recarga_id"); qp != "" {
		fmt.Sscanf(qp, "%d", &recargaID)
	}
	if recargaID == 0 && payload.Metadata != nil {
		recargaID = payload.Metadata.RecargaID
	}

	reqLog(r).Info("[tpc_webhook_pix] recebido",
		"pix_id", pixID,
		"status", status,
		"valor_webhook", valor.StringFixed(2),
		"recarga_id", recargaID,
	)

	// ── Resolve recarga ───────────────────────────────────────────────────────
	// Espelha PHP: se recarga_id ausente, busca por me_pix_id em tpc_recargas.
	if recargaID == 0 && pixID != "" {
		err := h.db.QueryRow(r.Context(),
			`SELECT id FROM tpc_recargas WHERE me_pix_id = $1 LIMIT 1`,
			pixID,
		).Scan(&recargaID)
		if err != nil && err != pgx.ErrNoRows {
			reqLog(r).Error("[tpc_webhook_pix] erro ao buscar recarga por me_pix_id", "pix_id", pixID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
			return
		}
	}

	if recargaID == 0 {
		reqLog(r).Warn("[tpc_webhook_pix] recarga_id e me_pix_id ausentes no payload", "pix_id", pixID)
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "sem recarga_id")
		return
	}

	recarga, err := h.loadRecarga(r.Context(), recargaID)
	if err == pgx.ErrNoRows {
		reqLog(r).Warn("[tpc_webhook_pix] recarga não encontrada", "recarga_id", recargaID)
		httpx.WriteErr(w, http.StatusNotFound, "recarga não encontrada")
		return
	}
	if err != nil {
		reqLog(r).Error("[tpc_webhook_pix] erro ao carregar recarga", "recarga_id", recargaID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// ── Prepara chave de idempotência ─────────────────────────────────────────
	// AUDIT-2026-06-18 Onda2 (go-pix-idem-tx): a chave é só CALCULADA aqui; o
	// INSERT em tpc_webhook_events foi movido para DENTRO da transação de crédito
	// (confirmarRecarga). Antes ele rodava no pool autocommit ANTES do crédito, e
	// um crash entre os dois deixava a recarga sem crédito e suprimia o retry.
	mePixIDParaEventKey := pixID
	if mePixIDParaEventKey == "" {
		mePixIDParaEventKey = recarga.MePixID
	}
	eventKey := buildPixEventKey(mePixIDParaEventKey, recargaID, rawBody)
	payloadHash := sha256hex(rawBody)

	// ── Valida status ─────────────────────────────────────────────────────────
	// M-03 — Porte FIEL da ordem de tpc_webhook_pix_handler (webhook.php:110-127):
	//   1. status vazio        → 422 "status obrigatório".
	//   2. status cancelado    → cancela a recarga, 200 "cancelada" (NÃO rejeita).
	//   3. status em análise   → marca/registra análise, 200 "analise" (NÃO rejeita).
	//   4. status não-pago     → 422 "status não confirma pagamento".
	// Antes o Go só checava {paid,approved} e respondia 200 "status não requer
	// crédito" para todo o resto — silenciava cancelamento e análise.
	if status == "" {
		reqLog(r).Warn("[tpc_webhook_pix] status ausente", "recarga_id", recargaID)
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "status obrigatório para confirmar PIX")
		return
	}

	// Cancelado/expirado/falhou: cancela a recarga (espelha tpc_cancelar_recarga).
	if isStatusCancelado(status) {
		if err := h.cancelarRecargaWebhook(r.Context(), recargaID); err != nil {
			reqLog(r).Error("[tpc_webhook_pix] erro ao cancelar recarga", "recarga_id", recargaID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao cancelar recarga")
			return
		}
		reqLog(r).Info("[tpc_webhook_pix] recarga cancelada via webhook", "recarga_id", recargaID, "status", status)
		httpx.WriteOK(w, map[string]any{"nota": "cancelada"})
		return
	}

	// Em análise antifraude: ainda não confirma nem rejeita.
	// O WP marca status='analise'; no Postgres o CHECK não inclui 'analise', então
	// a recarga permanece 'pendente' (aguardando) — o crédito só virá num evento
	// de pagamento posterior. Resposta 200 "analise" preserva o contrato do WP.
	if isStatusAnalise(status, rawBody) {
		reqLog(r).Info("[tpc_webhook_pix] recarga em análise (mantida pendente)", "recarga_id", recargaID, "status", status)
		httpx.WriteOK(w, map[string]any{"nota": "analise"})
		return
	}

	// Demais status que não confirmam pagamento → 422 (espelha o PHP).
	if !isStatusPago(status) {
		reqLog(r).Info("[tpc_webhook_pix] status não confirma pagamento", "status", status, "recarga_id", recargaID)
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "status não confirma pagamento")
		return
	}

	// ── Valida divergência de valor ───────────────────────────────────────────
	// Tolerância ±0.01 (espelha o abs() < 0.01 do PHP).
	if !valor.IsZero() {
		diff := valor.Sub(recarga.Valor).Abs()
		tolerancia := decimal.NewFromFloat(0.01)
		if diff.GreaterThan(tolerancia) {
			reqLog(r).Warn("[tpc_webhook_pix] valor divergente",
				"recarga_id", recargaID,
				"esperado", recarga.Valor.StringFixed(2),
				"recebido", valor.StringFixed(2),
			)
			httpx.WriteErr(w, http.StatusConflict, "valor divergente")
			return
		}
	}

	// Valida me_pix_id se ambos estão presentes (espelha hash_equals do PHP).
	if pixID != "" && recarga.MePixID != "" && pixID != recarga.MePixID {
		reqLog(r).Warn("[tpc_webhook_pix] me_pix_id divergente",
			"recarga_id", recargaID,
			"esperado", recarga.MePixID,
			"recebido", pixID,
		)
		httpx.WriteErr(w, http.StatusConflict, "me_pix_id divergente")
		return
	}

	// ── Confirma recarga: credita carteira e atualiza status ─────────────────
	// AUDIT-2026-06-18 Onda2 (go-pix-idem-tx): a idempotência (INSERT em
	// tpc_webhook_events) acontece DENTRO desta transação. alreadyProcessed=true
	// → evento já creditado anteriormente, resposta idempotente sem novo crédito.
	referencia := fmt.Sprintf("recarga:%d", recargaID)
	evento := pixWebhookEvent{
		EventKey:    eventKey,
		Source:      "pix",
		EventType:   "payment",
		PayloadHash: payloadHash,
		RecargaID:   recargaID,
		MeID:        pixID,
		Status:      status,
	}
	alreadyProcessed, err := h.confirmarRecarga(r.Context(), recarga, referencia, evento)
	if err != nil {
		reqLog(r).Error("[tpc_webhook_pix] erro ao confirmar recarga",
			"recarga_id", recargaID,
			"user_id", recarga.UserID,
			"err", err,
		)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao confirmar recarga")
		return
	}
	if alreadyProcessed {
		reqLog(r).Info("[tpc_webhook_pix] evento duplicado ignorado (idempotente)", "event_key", eventKey)
		httpx.WriteOK(w, map[string]any{"nota": "evento já processado"})
		return
	}

	reqLog(r).Info("[tpc_webhook_pix] recarga confirmada via webhook",
		"recarga_id", recargaID,
		"user_id", recarga.UserID,
		"valor", recarga.Valor.StringFixed(2),
		"pix_id", pixID,
	)
	httpx.WriteOK(w, map[string]any{"recarga_id": recargaID})
}

// cancelarRecargaWebhook marca a recarga como 'cancelado' quando o webhook PIX
// informa um status terminal de falha/cancelamento/expiração.
// Espelha tpc_cancelar_recarga: só altera se ainda estiver 'pendente'
// (idempotente — re-entregas do provedor não causam erro).
func (h *PixHandler) cancelarRecargaWebhook(ctx context.Context, recargaID int64) error {
	_, err := h.db.Exec(ctx,
		`UPDATE tpc_recargas
		 SET status = 'cancelado'
		 WHERE id = $1 AND status = 'pendente'`,
		recargaID,
	)
	return err
}

// loadRecarga carrega a linha de tpc_recargas pelo ID.
func (h *PixHandler) loadRecarga(ctx context.Context, recargaID int64) (recargaRow, error) {
	var r recargaRow
	var valorStr string
	err := h.db.QueryRow(ctx,
		`SELECT id, user_id, valor, status, COALESCE(me_pix_id, '')
		 FROM tpc_recargas
		 WHERE id = $1`,
		recargaID,
	).Scan(&r.ID, &r.UserID, &valorStr, &r.Status, &r.MePixID)
	if err != nil {
		return r, err
	}
	r.Valor, err = decimal.NewFromString(valorStr)
	return r, err
}

// pixWebhookEvent agrupa os campos de idempotência do evento de webhook,
// gravados em tpc_webhook_events DENTRO da transação de crédito.
// AUDIT-2026-06-18 Onda2 (go-pix-idem-tx).
type pixWebhookEvent struct {
	EventKey    string
	Source      string
	EventType   string
	PayloadHash string
	RecargaID   int64
	MeID        string
	Status      string
}

// confirmarRecarga credita a carteira e marca a recarga como confirmada atomicamente.
// Espelha tpc_confirmar_recarga() chamado com origin='webhook'.
//
// Retorna (alreadyProcessed, err): alreadyProcessed=true quando o evento já fora
// registrado (UNIQUE KEY de tpc_webhook_events disparou) — caso em que nada é
// creditado e o caller responde idempotente.
//
// Atomicidade garantida por transação Serializable:
//  0. INSERT tpc_webhook_events ON CONFLICT DO NOTHING — idempotência no MESMO tx
//     do crédito (AUDIT-2026-06-18 Onda2 go-pix-idem-tx). RowsAffected=0 → duplicado,
//     rollback e retorno antecipado ANTES de creditar.
//  1. SELECT FOR UPDATE na linha da carteira.
//  2. Upsert saldo += valor.
//  3. INSERT tpc_transacoes tipo=credito, ON CONFLICT DO NOTHING (idempotente).
//  4. UPDATE tpc_recargas SET status='confirmado' WHERE id = $1 AND status != 'confirmado'.
func (h *PixHandler) confirmarRecarga(ctx context.Context, recarga recargaRow, referencia string, evento pixWebhookEvent) (bool, error) {
	tx, err := h.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return false, fmt.Errorf("iniciar transação: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// AUDIT-2026-06-18 Onda2 (go-pix-idem-tx): registra a idempotência ANTES de
	// creditar, mas DENTRO desta transação. Se o evento já existe, faz rollback
	// (defer) e retorna sem creditar. Se o processo cair entre aqui e o commit,
	// nada foi persistido → o retry do provedor recredita normalmente.
	alreadyProcessed, err := registerWebhookEvent(ctx, tx,
		evento.EventKey, evento.Source, evento.EventType, evento.PayloadHash,
		evento.RecargaID, evento.MeID, evento.Status,
	)
	if err != nil {
		return false, fmt.Errorf("registrar evento: %w", err)
	}
	if alreadyProcessed {
		return true, nil
	}

	// Garante linha na carteira.
	if err := upsertCarteira(ctx, tx, recarga.UserID); err != nil {
		return false, fmt.Errorf("upsert carteira: %w", err)
	}

	saldo, _, err := lockCarteira(ctx, tx, recarga.UserID)
	if err != nil {
		return false, fmt.Errorf("lock carteira: %w", err)
	}

	novoSaldo := saldo.Add(recarga.Valor)

	_, err = tx.Exec(ctx,
		`UPDATE tpc_carteira SET saldo = $1 WHERE user_id = $2`,
		novoSaldo.StringFixed(2), recarga.UserID,
	)
	if err != nil {
		return false, fmt.Errorf("update carteira: %w", err)
	}

	// INSERT idempotente via ON CONFLICT (user_id, referencia, tipo) DO NOTHING.
	var txID int64
	err = tx.QueryRow(ctx,
		`INSERT INTO tpc_transacoes
		    (user_id, tipo, valor, saldo_apos, descricao, referencia, status)
		 VALUES ($1, 'credito', $2, $3, $4, $5, 'confirmado')
		 ON CONFLICT (user_id, referencia, tipo) DO NOTHING
		 RETURNING id`,
		recarga.UserID,
		recarga.Valor.StringFixed(2),
		novoSaldo.StringFixed(2),
		"Recarga PIX confirmada via webhook",
		referencia,
	).Scan(&txID)

	if err != nil && err != pgx.ErrNoRows {
		return false, fmt.Errorf("insert transacao: %w", err)
	}

	// Marca a recarga como confirmada, armazenando o tx_id.
	// A condição WHERE status != 'confirmado' evita dupla confirmação.
	_, err = tx.Exec(ctx,
		`UPDATE tpc_recargas
		 SET status = 'confirmado', tx_id = $1
		 WHERE id = $2 AND status != 'confirmado'`,
		txID, recarga.ID,
	)
	if err != nil {
		return false, fmt.Errorf("update recarga: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit: %w", err)
	}

	slog.Info("[tpc_webhook_pix] recarga creditada",
		"recarga_id", recarga.ID,
		"user_id", recarga.UserID,
		"tx_id", txID,
		"saldo", novoSaldo.StringFixed(2),
	)
	return false, nil
}

// registerWebhookEvent tenta inserir o evento em tpc_webhook_events.
// Retorna (true, nil) se o evento já existia (DO NOTHING → idempotente),
// ou (false, nil) se foi inserido agora pela primeira vez.
// Usa CommandTag.RowsAffected() para distinção atômica.
//
// AUDIT-2026-06-18 Onda2 (go-pix-idem-tx): o INSERT roda na MESMA transação
// (pgx.Tx) do crédito em confirmarRecarga — nunca mais no pool autocommit.
// Assim um crash entre "registrar evento" e "creditar" não deixa a recarga sem
// crédito nem suprime o retry: ou ambos commitam, ou ambos sofrem rollback.
func registerWebhookEvent(ctx context.Context, tx pgx.Tx, eventKey, source, eventType, payloadHash string, recargaID int64, meID, status string) (bool, error) {
	tag, err := tx.Exec(ctx,
		`INSERT INTO tpc_webhook_events
		    (event_key, source, event_type, payload_hash, recarga_id, me_id, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (event_key) DO NOTHING`,
		eventKey, source, eventType, payloadHash, recargaID, meID, status,
	)
	if err != nil {
		return false, err
	}
	// RowsAffected = 0 → UNIQUE KEY disparou → evento já processado.
	alreadyExists := tag.RowsAffected() == 0
	return alreadyExists, nil
}

// buildPixEventKey constrói a chave de idempotência.
// Espelha tpc_webhook_build_event_key em webhook.php:
//   - se me_pix_id disponível → "pix:<me_pix_id>"
//   - caso contrário → hash do body truncado a 48 chars
func buildPixEventKey(pixID string, recargaID int64, rawBody []byte) string {
	if pixID != "" {
		return fmt.Sprintf("pix:%s", pixID)
	}
	if recargaID > 0 {
		return fmt.Sprintf("pix:recarga:%d", recargaID)
	}
	return "pix:" + sha256hex(rawBody)[:48]
}

// firstNonZeroDecimal retorna o primeiro valor não-zero da lista.
func firstNonZeroDecimal(vals ...decimal.Decimal) decimal.Decimal {
	for _, v := range vals {
		if !v.IsZero() {
			return v
		}
	}
	return decimal.Zero
}

// sha256hex retorna o SHA-256 hexadecimal do input.
func sha256hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// safePrefix retorna os primeiros n bytes de s (para logs — nunca logar sig completo).
func safePrefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
