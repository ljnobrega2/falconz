// Package handlers — endpoint interno para double-write do PHP (Fase 3).
//
// Recebe replicação assíncrona do WordPress durante a janela de migração.
// Auth: HMAC-SHA256 via header X-Internal-Sig (env AFFILIATES_INTERNAL_SECRET).
//
// Rotas:
//   POST /internal/commissions        — replica comissões criadas no PHP
//   POST /internal/cod                — replica entradas no ledger COD (não altera saldo)
//   POST /internal/carteira/{user_id} — UPSERT saldo na carteira COD
//
// Fail-closed: secret vazio → 503 em todas as rotas (espelha auth.go JWT_SECRET).
// Idempotência: ON CONFLICT DO NOTHING em todas as rotas.
//
// Remover após cutover confirmado (strangler fig completo).
// Comentários em PT-BR conforme convenção do projeto.
package handlers

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/senderzz/affiliates-service/internal/httpx"
)

// InternalHandler recebe payloads de double-write do PHP.
// Sempre instanciado — fail-closed via verificação de secret em cada handler.
type InternalHandler struct {
	Pool   *pgxpool.Pool
	secret string // AFFILIATES_INTERNAL_SECRET (pode ser vazio → 503 em todas as rotas)
}

// NewInternalHandler cria o handler lendo AFFILIATES_INTERNAL_SECRET da env.
// Retorna sempre um handler não-nil — secret vazio resulta em 503 nas rotas
// (fail-closed, espelha o comportamento de AuthPortalJWT com JWT_SECRET vazio).
func NewInternalHandler(pool *pgxpool.Pool) (*InternalHandler, error) {
	return &InternalHandler{
		Pool:   pool,
		secret: os.Getenv("AFFILIATES_INTERNAL_SECRET"),
	}, nil
}

// verifyHMAC valida X-Internal-Sig contra o body lido.
// Retorna false se secret estiver vazio (fail-closed).
func (h *InternalHandler) verifyHMAC(sig string, body []byte) bool {
	if h.secret == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(h.secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(expected))
}

// checkSecret retorna false e escreve 503 se o secret não estiver configurado.
// Deve ser chamado no início de cada handler antes da leitura do body.
func (h *InternalHandler) checkSecret(w http.ResponseWriter) bool {
	if h.secret == "" {
		slog.Error("[internal] AFFILIATES_INTERNAL_SECRET não configurado — rejeitando requisição")
		httpx.WriteErr(w, http.StatusServiceUnavailable, "double-write não configurado")
		return false
	}
	return true
}

// readBody lê e limita o body a 1 MB.
func readBody(r *http.Request) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r.Body, 1<<20))
}

// ── POST /internal/commissions ────────────────────────────────────────────────

// CommissionCreated replica uma comissão criada no PHP para o Postgres.
//
// Payload esperado:
//
//	{
//	  "affiliate_id": 123,
//	  "order_id":     456,
//	  "valor":        "25.00",
//	  "status":       "pendente"
//	}
//
// Idempotência: ON CONFLICT (affiliate_id, order_id) DO NOTHING.
func (h *InternalHandler) CommissionCreated(w http.ResponseWriter, r *http.Request) {
	if !h.checkSecret(w) {
		return
	}

	body, err := readBody(r)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if !h.verifyHMAC(r.Header.Get("X-Internal-Sig"), body) {
		slog.Warn("[internal/commissions] assinatura HMAC inválida", "ip", r.RemoteAddr)
		httpx.WriteErr(w, http.StatusUnauthorized, "assinatura inválida")
		return
	}

	var p map[string]any
	if err := json.Unmarshal(body, &p); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	affiliateID := toInt64(p["affiliate_id"])
	orderID := toInt64(p["order_id"])
	if affiliateID == 0 || orderID == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "affiliate_id e order_id são obrigatórios")
		return
	}

	valorStr := toString(p["valor"])
	if valorStr == "" {
		valorStr = "0.00"
	}
	valor, err := decimal.NewFromString(valorStr)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "valor inválido")
		return
	}

	status := toString(p["status"])
	if status == "" {
		status = "pendente"
	}

	ctx := r.Context()
	_, err = h.Pool.Exec(ctx, `
		INSERT INTO senderzz_affiliate_commissions
		    (affiliate_id, order_id, valor, status)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (affiliate_id, order_id) DO NOTHING`,
		affiliateID,
		orderID,
		valor.StringFixed(2),
		status,
	)
	if err != nil {
		slog.Error("[internal/commissions] falha ao inserir comissão",
			"affiliate_id", affiliateID, "order_id", orderID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao salvar comissão")
		return
	}

	slog.Info("[internal/commissions] comissão replicada",
		"affiliate_id", affiliateID, "order_id", orderID, "valor", valor.StringFixed(2))
	httpx.WriteOK(w, map[string]any{"affiliate_id": affiliateID, "order_id": orderID})
}

// ── POST /internal/cod ────────────────────────────────────────────────────────

// CODLedger replica uma entrada no ledger COD gerada pelo PHP.
// Não altera o saldo da carteira — use /internal/carteira/{user_id} para isso.
//
// Payload esperado:
//
//	{
//	  "user_id":    789,
//	  "valor":      "50.00",
//	  "pedido_id":  42,
//	  "tipo":       "credito",
//	  "descricao":  "COD entrega pedido #42",
//	  "referencia": "cod_delivery_pedido_42"
//	}
//
// Idempotência: ON CONFLICT (user_id, referencia, tipo) DO NOTHING.
func (h *InternalHandler) CODLedger(w http.ResponseWriter, r *http.Request) {
	if !h.checkSecret(w) {
		return
	}

	body, err := readBody(r)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if !h.verifyHMAC(r.Header.Get("X-Internal-Sig"), body) {
		slog.Warn("[internal/cod] assinatura HMAC inválida", "ip", r.RemoteAddr)
		httpx.WriteErr(w, http.StatusUnauthorized, "assinatura inválida")
		return
	}

	var p map[string]any
	if err := json.Unmarshal(body, &p); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	userID := toInt64(p["user_id"])
	if userID == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "user_id obrigatório")
		return
	}

	valorStr := toString(p["valor"])
	if valorStr == "" {
		valorStr = "0.00"
	}
	valor, err := decimal.NewFromString(valorStr)
	if err != nil || valor.LessThanOrEqual(decimal.Zero) {
		httpx.WriteErr(w, http.StatusBadRequest, "valor inválido ou zero")
		return
	}

	tipo := toString(p["tipo"])
	if tipo == "" {
		tipo = "credito"
	}

	pedidoID := toInt64(p["pedido_id"])
	descricao := toString(p["descricao"])
	referencia := toString(p["referencia"])
	if referencia == "" {
		referencia = fmt.Sprintf("cod_%s_pedido_%d", tipo, pedidoID)
	}

	// pedido_id é nullable na tabela — converte zero para nil.
	var pedidoIDPtr *int64
	if pedidoID != 0 {
		pedidoIDPtr = &pedidoID
	}

	ctx := r.Context()

	// INSERT simples com idempotência — saldo gerenciado via /internal/carteira/{user_id}.
	_, err = h.Pool.Exec(ctx, `
		INSERT INTO senderzz_cod_ledger
		    (user_id, tipo, valor, pedido_id, descricao, referencia)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (user_id, referencia, tipo) DO NOTHING`,
		userID,
		tipo,
		valor.StringFixed(2),
		pedidoIDPtr,
		descricao,
		referencia,
	)
	if err != nil {
		slog.Error("[internal/cod] falha ao inserir ledger", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao salvar entrada COD")
		return
	}

	slog.Info("[internal/cod] entrada COD replicada",
		"user_id", userID, "tipo", tipo, "valor", valor.StringFixed(2), "referencia", referencia)
	httpx.WriteOK(w, map[string]any{"user_id": userID, "referencia": referencia})
}

// ── POST /internal/carteira/{user_id} ────────────────────────────────────────

// CarteiraUpsert realiza UPSERT do saldo na carteira COD do usuário.
// PHP chama este endpoint após registrar o crédito no ledger via /internal/cod.
//
// Payload esperado:
//
//	{"saldo": "123.45"}
//
// UPSERT: cria linha se não existir; sobrescreve saldo se já existir.
// Idempotente por natureza — UPSERT com valor determinístico do PHP.
func (h *InternalHandler) CarteiraUpsert(w http.ResponseWriter, r *http.Request) {
	if !h.checkSecret(w) {
		return
	}

	body, err := readBody(r)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if !h.verifyHMAC(r.Header.Get("X-Internal-Sig"), body) {
		slog.Warn("[internal/carteira] assinatura HMAC inválida", "ip", r.RemoteAddr)
		httpx.WriteErr(w, http.StatusUnauthorized, "assinatura inválida")
		return
	}

	// user_id vem do path — validado aqui para logar antes de parsear o body.
	userIDStr := chi.URLParam(r, "user_id")
	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil || userID <= 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "user_id inválido")
		return
	}

	var p map[string]any
	if err := json.Unmarshal(body, &p); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	saldoStr := toString(p["saldo"])
	if saldoStr == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "saldo obrigatório")
		return
	}
	saldo, err := decimal.NewFromString(saldoStr)
	if err != nil || saldo.IsNegative() {
		httpx.WriteErr(w, http.StatusBadRequest, "saldo inválido")
		return
	}

	ctx := r.Context()

	// UPSERT: cria linha ou sobrescreve saldo existente.
	_, err = h.Pool.Exec(ctx, `
		INSERT INTO senderzz_cod_wallet (user_id, saldo)
		VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE
		    SET saldo = EXCLUDED.saldo,
		        updated_at = NOW()`,
		userID,
		saldo.StringFixed(2),
	)
	if err != nil {
		slog.Error("[internal/carteira] falha ao upsert carteira", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao atualizar carteira")
		return
	}

	slog.Info("[internal/carteira] saldo upsertado", "user_id", userID, "saldo", saldo.StringFixed(2))
	httpx.WriteOK(w, map[string]any{"user_id": userID, "saldo": saldo.StringFixed(2)})
}

// RegisterInternalRoutes registra as rotas /internal/* no router.
// O handler é sempre não-nil; secret vazio resulta em 503 dentro dos handlers.
func RegisterInternalRoutes(r chi.Router, h *InternalHandler) {
	r.Route("/internal", func(r chi.Router) {
		r.Post("/commissions", h.CommissionCreated)
		r.Post("/cod", h.CODLedger)
		r.Post("/carteira/{user_id}", h.CarteiraUpsert)
	})
}

// ── helpers de conversão ──────────────────────────────────────────────────────
// Espelha os helpers de go/motoboy/internal/handlers/internal.go

func toInt64(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case int64:
		return x
	case string:
		n, _ := strconv.ParseInt(x, 10, 64)
		return n
	}
	return 0
}

func toString(v any) string {
	if v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

// ── helper de geração de token aleatório ─────────────────────────────────────

// randomHex gera n bytes aleatórios e retorna sua representação hex.
// Usado para link_token (32 bytes = 64 chars) e invite token (32 bytes = 64 chars).
func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("falha ao gerar bytes aleatórios: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// toFloat64 converte any → float64 (usado apenas para logging).
func toFloat64(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case string:
		f, _ := strconv.ParseFloat(x, 64)
		return f
	}
	return 0
}

// toTime converte string "2006-01-02 15:04:05" ou RFC3339 para *time.Time.
func toTime(v any) *time.Time {
	s := toString(v)
	if s == "" || s == "0000-00-00 00:00:00" {
		return nil
	}
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return &t
		}
	}
	return nil
}

// nullableInt64 converte any → *int64 (nil se zero).
func nullableInt64(v any) *int64 {
	n := toInt64(v)
	if n == 0 {
		return nil
	}
	return &n
}

// _ suprime unused import warnings durante desenvolvimento.
var _ = toFloat64
var _ = toTime
var _ = nullableInt64
