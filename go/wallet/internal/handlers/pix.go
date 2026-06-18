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
//   1. recarga_id vem no query-param ou em payload.metadata.recarga_id.
//   2. Se ausente, faz lookup: SELECT id FROM tpc_recargas WHERE me_pix_id = $1.
//   3. Carrega a linha da recarga para obter user_id e valor esperado.
//   4. Valida divergência de valor (tolerância ±0.01).
//   5. Credita e marca a recarga como confirmada atomicamente.
//
// Idempotência:
//   - tpc_webhook_events com UNIQUE KEY uq_event_key garante que o mesmo evento
//     não seja creditado duas vezes.
//   - O crédito usa ON CONFLICT (user_id, referencia, tipo) DO NOTHING.
//
// Status aceitos como "pago": paid, approved (whitelist explícito — CRIT-04).
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
	"net/http"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/wallet-service/internal/httpx"
	"github.com/shopspring/decimal"
)

// PixHandler agrupa as dependências do handler PIX.
type PixHandler struct {
	db *pgxpool.Pool
}

// NewPixHandler cria um PixHandler com o pool de conexões fornecido.
func NewPixHandler(db *pgxpool.Pool) *PixHandler {
	return &PixHandler{db: db}
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
	ID       int64
	UserID   int64
	Valor    decimal.Decimal
	Status   string
	MePixID  string
}

// statusPago é o conjunto explícito de valores que indicam pagamento confirmado.
// Nunca usar strings.Contains — whitelist exato para evitar o bypass descrito em CRIT-04.
var statusPago = map[string]bool{
	"paid":     true,
	"approved": true,
}

// PostPixWebhook processa webhooks de confirmação de PIX.
//
// Fluxo:
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
	secret := os.Getenv("WEBHOOK_SECRET")
	if len(secret) < 32 {
		// Fail-closed: sem secret configurado o endpoint não opera.
		// 503 em vez de 401 para não revelar a existência da rota sem auth.
		slog.Error("[tpc_webhook_pix] WEBHOOK_SECRET não configurado ou muito curto",
			"ip", r.RemoteAddr,
		)
		httpx.WriteErr(w, http.StatusServiceUnavailable, "serviço temporariamente indisponível")
		return
	}

	// Lê o corpo bruto antes de qualquer parsing — obrigatório para validar HMAC.
	rawBody, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MiB max
	if err != nil {
		slog.Error("[tpc_webhook_pix] erro ao ler corpo", "err", err)
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
		slog.Warn("[tpc_webhook_pix] assinatura ausente", "ip", r.RemoteAddr)
		httpx.WriteErr(w, http.StatusUnauthorized, "assinatura inválida")
		return
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(rawBody)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	// Comparação em tempo constante (equivalente ao hash_equals do PHP).
	if !hmac.Equal([]byte(expected), []byte(sig)) {
		slog.Warn("[tpc_webhook_pix] assinatura inválida",
			"ip", r.RemoteAddr,
			"sig_prefix", safePrefix(sig, 16),
		)
		httpx.WriteErr(w, http.StatusUnauthorized, "assinatura inválida")
		return
	}

	// ── Parseia payload ───────────────────────────────────────────────────────
	var payload pixWebhookPayload
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		slog.Error("[tpc_webhook_pix] JSON inválido", "err", err)
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

	slog.Info("[tpc_webhook_pix] recebido",
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
			slog.Error("[tpc_webhook_pix] erro ao buscar recarga por me_pix_id", "pix_id", pixID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
			return
		}
	}

	if recargaID == 0 {
		slog.Warn("[tpc_webhook_pix] recarga_id e me_pix_id ausentes no payload", "pix_id", pixID)
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "sem recarga_id")
		return
	}

	recarga, err := h.loadRecarga(r.Context(), recargaID)
	if err == pgx.ErrNoRows {
		slog.Warn("[tpc_webhook_pix] recarga não encontrada", "recarga_id", recargaID)
		httpx.WriteErr(w, http.StatusNotFound, "recarga não encontrada")
		return
	}
	if err != nil {
		slog.Error("[tpc_webhook_pix] erro ao carregar recarga", "recarga_id", recargaID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// ── Idempotência de webhook ───────────────────────────────────────────────
	mePixIDParaEventKey := pixID
	if mePixIDParaEventKey == "" {
		mePixIDParaEventKey = recarga.MePixID
	}
	eventKey := buildPixEventKey(mePixIDParaEventKey, recargaID, rawBody)
	payloadHash := sha256hex(rawBody)

	alreadyProcessed, err := h.registerWebhookEvent(r.Context(), eventKey, "pix", "payment", payloadHash, recargaID, pixID, status)
	if err != nil {
		slog.Error("[tpc_webhook_pix] erro ao registrar evento", "event_key", eventKey, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	if alreadyProcessed {
		slog.Info("[tpc_webhook_pix] evento duplicado ignorado (idempotente)", "event_key", eventKey)
		httpx.WriteOK(w, map[string]any{"nota": "evento já processado"})
		return
	}

	// ── Valida status ─────────────────────────────────────────────────────────
	if status == "" {
		slog.Warn("[tpc_webhook_pix] status ausente", "recarga_id", recargaID)
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "status obrigatório para confirmar PIX")
		return
	}

	if !statusPago[status] {
		slog.Info("[tpc_webhook_pix] status não confirma pagamento", "status", status, "recarga_id", recargaID)
		httpx.WriteOK(w, map[string]any{"nota": "status não requer crédito"})
		return
	}

	// ── Valida divergência de valor ───────────────────────────────────────────
	// Tolerância ±0.01 (espelha o abs() < 0.01 do PHP).
	if !valor.IsZero() {
		diff := valor.Sub(recarga.Valor).Abs()
		tolerancia := decimal.NewFromFloat(0.01)
		if diff.GreaterThan(tolerancia) {
			slog.Warn("[tpc_webhook_pix] valor divergente",
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
		slog.Warn("[tpc_webhook_pix] me_pix_id divergente",
			"recarga_id", recargaID,
			"esperado", recarga.MePixID,
			"recebido", pixID,
		)
		httpx.WriteErr(w, http.StatusConflict, "me_pix_id divergente")
		return
	}

	// ── Confirma recarga: credita carteira e atualiza status ─────────────────
	referencia := fmt.Sprintf("recarga:%d", recargaID)
	if err := h.confirmarRecarga(r.Context(), recarga, referencia); err != nil {
		slog.Error("[tpc_webhook_pix] erro ao confirmar recarga",
			"recarga_id", recargaID,
			"user_id", recarga.UserID,
			"err", err,
		)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao confirmar recarga")
		return
	}

	slog.Info("[tpc_webhook_pix] recarga confirmada via webhook",
		"recarga_id", recargaID,
		"user_id", recarga.UserID,
		"valor", recarga.Valor.StringFixed(2),
		"pix_id", pixID,
	)
	httpx.WriteOK(w, map[string]any{"recarga_id": recargaID})
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

// confirmarRecarga credita a carteira e marca a recarga como confirmada atomicamente.
// Espelha tpc_confirmar_recarga() chamado com origin='webhook'.
//
// Atomicidade garantida por transação Serializable:
//  1. SELECT FOR UPDATE na linha da carteira.
//  2. Upsert saldo += valor.
//  3. INSERT tpc_transacoes tipo=credito, ON CONFLICT DO NOTHING (idempotente).
//  4. UPDATE tpc_recargas SET status='confirmado' WHERE id = $1 AND status != 'confirmado'.
func (h *PixHandler) confirmarRecarga(ctx context.Context, recarga recargaRow, referencia string) error {
	tx, err := h.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("iniciar transação: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Garante linha na carteira.
	if err := upsertCarteira(ctx, tx, recarga.UserID); err != nil {
		return fmt.Errorf("upsert carteira: %w", err)
	}

	saldo, _, err := lockCarteira(ctx, tx, recarga.UserID)
	if err != nil {
		return fmt.Errorf("lock carteira: %w", err)
	}

	novoSaldo := saldo.Add(recarga.Valor)

	_, err = tx.Exec(ctx,
		`UPDATE tpc_carteira SET saldo = $1 WHERE user_id = $2`,
		novoSaldo.StringFixed(2), recarga.UserID,
	)
	if err != nil {
		return fmt.Errorf("update carteira: %w", err)
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
		return fmt.Errorf("insert transacao: %w", err)
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
		return fmt.Errorf("update recarga: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	slog.Info("[tpc_webhook_pix] recarga creditada",
		"recarga_id", recarga.ID,
		"user_id", recarga.UserID,
		"tx_id", txID,
		"saldo", novoSaldo.StringFixed(2),
	)
	return nil
}

// registerWebhookEvent tenta inserir o evento em tpc_webhook_events.
// Retorna (true, nil) se o evento já existia (DO NOTHING → idempotente),
// ou (false, nil) se foi inserido agora pela primeira vez.
// Usa CommandTag.RowsAffected() para distinção atômica.
func (h *PixHandler) registerWebhookEvent(ctx context.Context, eventKey, source, eventType, payloadHash string, recargaID int64, meID, status string) (bool, error) {
	tag, err := h.db.Exec(ctx,
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
