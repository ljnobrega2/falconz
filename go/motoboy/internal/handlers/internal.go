// Package handlers — endpoint interno para double-write do PHP.
// Recebe replicação assíncrona do WordPress durante janela de migração (Fase 1).
// Auth: HMAC-SHA256 via header X-Internal-Sig (env MOTOBOY_INTERNAL_SECRET).
// Remover após cutover confirmado.
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
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/motoboy-service/internal/httpx"
)

// InternalHandler recebe payloads de double-write do PHP.
type InternalHandler struct {
	Pool   *pgxpool.Pool
	secret string // MOTOBOY_INTERNAL_SECRET
}

// NewInternalHandler cria um handler validando que o secret está configurado.
func NewInternalHandler(pool *pgxpool.Pool) (*InternalHandler, error) {
	s := os.Getenv("MOTOBOY_INTERNAL_SECRET")
	if s == "" {
		return nil, nil // double-write desativado — sem secret
	}
	return &InternalHandler{Pool: pool, secret: s}, nil
}

// verifyHMAC valida X-Internal-Sig contra o body lido.
func (h *InternalHandler) verifyHMAC(sig string, body []byte) bool {
	mac := hmac.New(sha256.New, []byte(h.secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(expected))
}

// readBody lê e limita o body a 1 MB.
func readBody(r *http.Request) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r.Body, 1<<20))
}

// PedidoCriado recebe replicação de insert em sz_motoboy_pedidos.
// POST /internal/pedidos
func (h *InternalHandler) PedidoCriado(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo inválido")
		return
	}

	if !h.verifyHMAC(r.Header.Get("X-Internal-Sig"), body) {
		httpx.WriteErr(w, http.StatusUnauthorized, "assinatura inválida")
		return
	}

	var p map[string]any
	if err := json.Unmarshal(body, &p); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	pedidoID := toInt64(p["id"])
	wcOrderID := toInt64(p["wc_order_id"])
	if pedidoID == 0 || wcOrderID == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "id e wc_order_id obrigatórios")
		return
	}

	ctx := r.Context()
	_, err = h.Pool.Exec(ctx, `
		INSERT INTO sz_motoboy_pedidos (
			id, wc_order_id, cd_id, zona_id, motoboy_id, status,
			dest_nome, dest_telefone, dest_cep, dest_endereco,
			dest_numero, dest_complemento, dest_bairro, dest_cidade,
			dest_uf, dest_produto, quantidade,
			valor_pedido, valor_taxa, ts_aprovado, synced_at
		) OVERRIDING SYSTEM VALUE
		VALUES (
			$1,$2,$3,$4,$5,$6,
			$7,$8,$9,$10,
			$11,$12,$13,$14,
			$15,$16,$17,
			$18,$19,$20, NOW()
		)
		ON CONFLICT (id) DO NOTHING`,
		pedidoID, wcOrderID,
		toInt64(p["cd_id"]), toInt64(p["zona_id"]), nullableInt(p["motoboy_id"]), toString(p["status"]),
		toString(p["dest_nome"]), toString(p["dest_telefone"]), toString(p["dest_cep"]), toString(p["dest_endereco"]),
		toString(p["dest_numero"]), toString(p["dest_complemento"]), toString(p["dest_bairro"]), toString(p["dest_cidade"]),
		toString(p["dest_uf"]), toString(p["dest_produto"]), toInt64(p["quantidade"]),
		toFloat64(p["valor_pedido"]), toFloat64(p["valor_taxa"]), toTime(p["ts_aprovado"]),
	)
	if err != nil {
		slog.Error("[internal] falha ao inserir pedido", "pedido_id", pedidoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao salvar pedido")
		return
	}

	slog.Info("[internal] pedido replicado", "pedido_id", pedidoID, "wc_order_id", wcOrderID)
	httpx.WriteOK(w, map[string]any{"ok": true, "pedido_id": pedidoID})
}

// StatusChanged recebe replicação de mudança de status.
// POST /internal/pedidos/{pedido_id}/status
func (h *InternalHandler) StatusChanged(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo inválido")
		return
	}

	if !h.verifyHMAC(r.Header.Get("X-Internal-Sig"), body) {
		httpx.WriteErr(w, http.StatusUnauthorized, "assinatura inválida")
		return
	}

	var p map[string]any
	if err := json.Unmarshal(body, &p); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	pedidoID := toInt64(p["pedido_id"])
	statusNew := toString(p["status_new"])
	if pedidoID == 0 || statusNew == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "pedido_id e status_new obrigatórios")
		return
	}

	ctx := r.Context()
	_, err = h.Pool.Exec(ctx, `
		UPDATE sz_motoboy_pedidos
		SET status=$1, motoboy_id=COALESCE($2, motoboy_id), synced_at=NOW()
		WHERE id=$3`,
		statusNew, nullableInt(p["motoboy_id"]), pedidoID,
	)
	if err != nil {
		slog.Error("[internal] falha ao atualizar status", "pedido_id", pedidoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao atualizar status")
		return
	}

	// Registra na tabela de auditoria Go — idempotente para replay (GAP 1-C).
	// WHERE NOT EXISTS evita duplicata no mesmo dia sem exigir UNIQUE constraint.
	// TODO: migration — adicionar índice único para substituir pelo ON CONFLICT:
	//   CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_audit_status_dia
	//     ON sz_motoboy_audit (pedido_id, acao, para_status, (created_at::date));
	_, _ = h.Pool.Exec(ctx, `
		INSERT INTO sz_motoboy_audit (pedido_id, acao, de_status, para_status, created_at)
		SELECT $1, 'status_alterado', $2, $3, NOW()
		WHERE NOT EXISTS (
			SELECT 1 FROM sz_motoboy_audit
			WHERE pedido_id = $1
			  AND acao = 'status_alterado'
			  AND para_status = $3
			  AND created_at::date = CURRENT_DATE
		)`,
		pedidoID, toString(p["status_old"]), statusNew,
	)

	slog.Info("[internal] status replicado", "pedido_id", pedidoID, "status", statusNew)
	httpx.WriteOK(w, map[string]any{"ok": true})
}

// MotoboyTrocado recebe replicação de troca de motoboy.
// POST /internal/pedidos/{pedido_id}/motoboy
func (h *InternalHandler) MotoboyTrocado(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo inválido")
		return
	}

	if !h.verifyHMAC(r.Header.Get("X-Internal-Sig"), body) {
		httpx.WriteErr(w, http.StatusUnauthorized, "assinatura inválida")
		return
	}

	var p map[string]any
	if err := json.Unmarshal(body, &p); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	pedidoID := toInt64(p["pedido_id"])
	if pedidoID == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "pedido_id obrigatório")
		return
	}

	ctx := r.Context()
	_, err = h.Pool.Exec(ctx, `
		UPDATE sz_motoboy_pedidos SET motoboy_id=$1, synced_at=NOW() WHERE id=$2`,
		nullableInt(p["motoboy_id"]), pedidoID,
	)
	if err != nil {
		slog.Error("[internal] falha ao trocar motoboy", "pedido_id", pedidoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao trocar motoboy")
		return
	}

	slog.Info("[internal] motoboy trocado", "pedido_id", pedidoID, "motoboy_id", p["motoboy_id"])
	httpx.WriteOK(w, map[string]any{"ok": true})
}

// ComprovanteSalvo recebe notificação de comprovante salvo no MySQL.
// POST /internal/comprovantes
func (h *InternalHandler) ComprovanteSalvo(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo inválido")
		return
	}

	if !h.verifyHMAC(r.Header.Get("X-Internal-Sig"), body) {
		httpx.WriteErr(w, http.StatusUnauthorized, "assinatura inválida")
		return
	}

	var p map[string]any
	if err := json.Unmarshal(body, &p); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	slog.Info("[internal] comprovante notificado",
		"comprovante_id", toInt64(p["comprovante_id"]),
		"pedido_id", toInt64(p["pedido_id"]),
	)
	httpx.WriteOK(w, map[string]any{"ok": true})
}

// RegisterInternalRoutes registra as rotas /internal/* no router.
// Só deve ser chamado se handler != nil (secret configurado).
func RegisterInternalRoutes(r chi.Router, h *InternalHandler) {
	r.Route("/internal", func(r chi.Router) {
		r.Post("/pedidos", h.PedidoCriado)
		r.Post("/pedidos/{pedido_id}/status", h.StatusChanged)
		r.Post("/pedidos/{pedido_id}/motoboy", h.MotoboyTrocado)
		r.Post("/comprovantes", h.ComprovanteSalvo)
	})
}

// ── helpers de conversão ──────────────────────────────────────────────────────

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

func toString(v any) string {
	if v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

func nullableInt(v any) *int64 {
	n := toInt64(v)
	if n == 0 {
		return nil
	}
	return &n
}

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
