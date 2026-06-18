// Package handlers — endpoint interno para double-write do PHP (Fase 2: Carteira + PIX).
//
// Recebe replicação assíncrona do WordPress durante janela de migração strangler fig.
// O MySQL/WordPress continua sendo source of truth. Este handler apenas espelha
// as escritas no Postgres para que o serviço Go possa servir leituras.
//
// Auth: HMAC-SHA256 via header X-Internal-Sig (env WALLET_INTERNAL_SECRET).
// Fail-closed: WALLET_INTERNAL_SECRET vazio → rotas retornam 503.
//
// Remover após cutover confirmado via infra/scripts/verify-migration.sh.
package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/wallet-service/internal/httpx"
)

// InternalHandler recebe payloads de double-write do PHP (Carteira + PIX).
type InternalHandler struct {
	Pool   *pgxpool.Pool
	secret string // WALLET_INTERNAL_SECRET
}

// NewInternalHandler cria um InternalHandler validando que o secret está configurado.
// Retorna (nil, nil) se WALLET_INTERNAL_SECRET não estiver definido — double-write
// desativado. O main.go deve verificar nil antes de registrar as rotas.
func NewInternalHandler(pool *pgxpool.Pool) (*InternalHandler, error) {
	s := os.Getenv("WALLET_INTERNAL_SECRET")
	if s == "" {
		// Double-write desativado — sem secret configurado.
		// Rotas /internal/* não serão registradas.
		slog.Warn("[tpc_internal] WALLET_INTERNAL_SECRET ausente — rotas /internal/* desativadas")
		return nil, nil
	}
	return &InternalHandler{Pool: pool, secret: s}, nil
}

// verifyHMAC valida o header X-Internal-Sig contra o body lido.
// Usa comparação em tempo constante (hmac.Equal) para evitar timing attacks.
func (h *InternalHandler) verifyHMAC(sig string, body []byte) bool {
	mac := hmac.New(sha256.New, []byte(h.secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(expected))
}

// readBodyInternal lê e limita o body a 1 MB.
func readBodyInternal(r *http.Request) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r.Body, 1<<20))
}

// ── POST /internal/transacoes ────────────────────────────────────────────────

// InserirTransacao recebe replicação de INSERT em wp_tpc_transacoes.
//
// Idempotência: ON CONFLICT (user_id, referencia, tipo) DO NOTHING.
// Se referencia for NULL ou vazia, a linha é inserida sem checar conflito
// (NULL != NULL no SQL — sem risco de falso-conflito).
func (h *InternalHandler) InserirTransacao(w http.ResponseWriter, r *http.Request) {
	body, err := readBodyInternal(r)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo inválido")
		return
	}

	if !h.verifyHMAC(r.Header.Get("X-Internal-Sig"), body) {
		slog.Warn("[tpc_internal] assinatura inválida em /internal/transacoes", "ip", r.RemoteAddr)
		httpx.WriteErr(w, http.StatusUnauthorized, "assinatura inválida")
		return
	}

	var p map[string]any
	if err := json.Unmarshal(body, &p); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	userID := internalToInt64(p["user_id"])
	tipo := internalToString(p["tipo"])
	valor := internalToString(p["valor"])
	if userID == 0 || tipo == "" || valor == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "user_id, tipo e valor são obrigatórios")
		return
	}

	referencia := internalToStringPtr(p["referencia"])
	descricao := internalToString(p["descricao"])
	status := internalToString(p["status"])
	if status == "" {
		status = "confirmado"
	}
	saldoApos := internalToString(p["saldo_apos"])
	if saldoApos == "" {
		saldoApos = "0.00"
	}
	orderID := internalToInt64Ptr(p["order_id"])
	meOrderID := internalToStringPtr(p["me_order_id"])

	ctx := r.Context()
	_, err = h.Pool.Exec(ctx,
		`INSERT INTO tpc_transacoes
		    (user_id, tipo, valor, saldo_apos, descricao, referencia,
		     order_id, me_order_id, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 ON CONFLICT (user_id, referencia, tipo) DO NOTHING`,
		userID, tipo, valor, saldoApos, descricao, referencia,
		orderID, meOrderID, status,
	)
	if err != nil {
		slog.Error("[tpc_internal] falha ao inserir transação",
			"user_id", userID, "tipo", tipo, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao salvar transação")
		return
	}

	slog.Info("[tpc_internal] transação replicada", "user_id", userID, "tipo", tipo, "referencia", referencia)
	httpx.WriteOK(w, map[string]any{"ok": true, "user_id": userID})
}

// ── POST /internal/recargas ──────────────────────────────────────────────────

// InserirRecarga recebe replicação de INSERT em wp_tpc_recargas.
//
// Idempotência: ON CONFLICT (me_pix_id) DO NOTHING.
// Se me_pix_id for NULL, usa fallback de ID do MySQL para não colidir.
func (h *InternalHandler) InserirRecarga(w http.ResponseWriter, r *http.Request) {
	body, err := readBodyInternal(r)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo inválido")
		return
	}

	if !h.verifyHMAC(r.Header.Get("X-Internal-Sig"), body) {
		slog.Warn("[tpc_internal] assinatura inválida em /internal/recargas", "ip", r.RemoteAddr)
		httpx.WriteErr(w, http.StatusUnauthorized, "assinatura inválida")
		return
	}

	var p map[string]any
	if err := json.Unmarshal(body, &p); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	userID := internalToInt64(p["user_id"])
	valor := internalToString(p["valor"])
	if userID == 0 || valor == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "user_id e valor são obrigatórios")
		return
	}

	status := internalToString(p["status"])
	if status == "" {
		status = "pendente"
	}
	mePixID := internalToStringPtr(p["me_pix_id"])
	pixQR := internalToStringPtr(p["pix_qr"])
	pixCodigo := internalToStringPtr(p["pix_codigo"])
	expiresAt := internalToStringPtr(p["expires_at"])

	ctx := r.Context()
	_, err = h.Pool.Exec(ctx,
		`INSERT INTO tpc_recargas
		    (user_id, valor, status, me_pix_id, pix_qr, pix_codigo, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6,
		     CASE WHEN $7::text IS NOT NULL
		          THEN $7::timestamptz
		          ELSE NOW() + INTERVAL '24 hours'
		     END)
		 ON CONFLICT (me_pix_id) DO NOTHING`,
		userID, valor, status, mePixID, pixQR, pixCodigo, expiresAt,
	)
	if err != nil {
		slog.Error("[tpc_internal] falha ao inserir recarga",
			"user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao salvar recarga")
		return
	}

	slog.Info("[tpc_internal] recarga replicada", "user_id", userID, "me_pix_id", mePixID)
	httpx.WriteOK(w, map[string]any{"ok": true, "user_id": userID})
}

// ── POST /internal/recargas/{id}/confirmar ───────────────────────────────────

// ConfirmarRecarga recebe replicação de confirmação de pagamento PIX.
//
// Atualiza status → 'confirmado' e paid_at no Postgres.
// NÃO credita a carteira aqui — o crédito já foi feito pelo handler PIX
// (pix.go / confirmarRecarga). Este endpoint apenas sincroniza o status
// da recarga para o Postgres refletir o MySQL.
func (h *InternalHandler) ConfirmarRecarga(w http.ResponseWriter, r *http.Request) {
	body, err := readBodyInternal(r)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo inválido")
		return
	}

	if !h.verifyHMAC(r.Header.Get("X-Internal-Sig"), body) {
		slog.Warn("[tpc_internal] assinatura inválida em /internal/recargas/{id}/confirmar", "ip", r.RemoteAddr)
		httpx.WriteErr(w, http.StatusUnauthorized, "assinatura inválida")
		return
	}

	// Extrai recarga_id do path parameter.
	recargaIDStr := chi.URLParam(r, "id")
	recargaID, err := strconv.ParseInt(recargaIDStr, 10, 64)
	if err != nil || recargaID == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "recarga id inválido no path")
		return
	}

	var p map[string]any
	if err := json.Unmarshal(body, &p); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	ctx := r.Context()
	tag, err := h.Pool.Exec(ctx,
		`UPDATE tpc_recargas
		 SET status = 'confirmado',
		     paid_at = NOW()
		 WHERE id = $1
		   AND status != 'confirmado'`,
		recargaID,
	)
	if err != nil {
		slog.Error("[tpc_internal] falha ao confirmar recarga",
			"recarga_id", recargaID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao confirmar recarga")
		return
	}

	rows := tag.RowsAffected()
	slog.Info("[tpc_internal] recarga confirmada replicada",
		"recarga_id", recargaID, "rows_affected", rows)
	httpx.WriteOK(w, map[string]any{"ok": true, "recarga_id": recargaID, "rows": rows})
}

// ── POST /internal/carteira/{user_id} ───────────────────────────────────────

// UpsertCarteira recebe replicação de UPDATE em wp_tpc_carteira.
//
// Faz UPSERT: se o usuário ainda não existe no Postgres, insere a linha.
// Se já existe, sobrescreve saldo e saldo_reservado com os valores do MySQL
// (eventual consistency durante a janela de migração).
func (h *InternalHandler) UpsertCarteira(w http.ResponseWriter, r *http.Request) {
	body, err := readBodyInternal(r)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo inválido")
		return
	}

	if !h.verifyHMAC(r.Header.Get("X-Internal-Sig"), body) {
		slog.Warn("[tpc_internal] assinatura inválida em /internal/carteira/{user_id}", "ip", r.RemoteAddr)
		httpx.WriteErr(w, http.StatusUnauthorized, "assinatura inválida")
		return
	}

	// Extrai user_id do path parameter.
	userIDStr := chi.URLParam(r, "user_id")
	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil || userID == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "user_id inválido no path")
		return
	}

	var p map[string]any
	if err := json.Unmarshal(body, &p); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	saldo := internalToString(p["saldo"])
	saldoReservado := internalToString(p["saldo_reservado"])
	if saldo == "" {
		saldo = "0.00"
	}
	if saldoReservado == "" {
		saldoReservado = "0.00"
	}

	ctx := r.Context()
	_, err = h.Pool.Exec(ctx,
		`INSERT INTO tpc_carteira (user_id, saldo, saldo_reservado)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (user_id) DO UPDATE
		     SET saldo           = EXCLUDED.saldo,
		         saldo_reservado = EXCLUDED.saldo_reservado`,
		userID, saldo, saldoReservado,
	)
	if err != nil {
		slog.Error("[tpc_internal] falha ao fazer upsert da carteira",
			"user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao atualizar carteira")
		return
	}

	slog.Info("[tpc_internal] carteira replicada",
		"user_id", userID, "saldo", saldo, "saldo_reservado", saldoReservado)
	httpx.WriteOK(w, map[string]any{"ok": true, "user_id": userID})
}

// RegisterInternalRoutes registra as rotas /internal/* no router da carteira.
// Só deve ser chamado se handler != nil (WALLET_INTERNAL_SECRET configurado).
// Fail-closed: se o handler for nil (secret ausente), o bloco /internal/* fica
// inacessível — retorna 503 para qualquer requisição não roteada.
func RegisterInternalRoutes(r chi.Router, h *InternalHandler) {
	r.Route("/internal", func(r chi.Router) {
		r.Post("/transacoes", h.InserirTransacao)
		r.Post("/recargas", h.InserirRecarga)
		r.Post("/recargas/{id}/confirmar", h.ConfirmarRecarga)
		r.Post("/carteira/{user_id}", h.UpsertCarteira)
	})
}

// ── helpers de conversão de tipos ────────────────────────────────────────────

// internalToInt64 converte float64, int64, string para int64.
// JSON decodifica números como float64 por padrão em map[string]any.
func internalToInt64(v any) int64 {
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

// internalToString retorna o valor como string, ou "" se nil.
func internalToString(v any) string {
	if v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

// internalToStringPtr retorna ponteiro para string, ou nil se vazio/nil.
func internalToStringPtr(v any) *string {
	s := internalToString(v)
	if s == "" {
		return nil
	}
	return &s
}

// internalToInt64Ptr retorna ponteiro para int64, ou nil se zero/nil.
func internalToInt64Ptr(v any) *int64 {
	n := internalToInt64(v)
	if n == 0 {
		return nil
	}
	return &n
}
