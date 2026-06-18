// Package handlers — rotas de execução (iniciar-rota, entregar, frustrar).
// Auth: X-MB-Token (token do motoboy).
package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/motoboy-service/internal/httpx"
)

// qrPattern valida o código QR: SZ-{wc_order_id}-{pedido_id}-{hmac14}
var qrPattern = regexp.MustCompile(`^SZ-(\d+)-(\d+)-([0-9a-f]{14})$`)

// RotaHandler implementa os endpoints de execução de rota.
type RotaHandler struct {
	Pool *pgxpool.Pool
}

// IniciarRota — POST /motoboy/iniciar-rota
// Body: {qr_code}
// Valida HMAC14 com WP_SALT_AUTH, muda status para em_rota.
func (h *RotaHandler) IniciarRota(w http.ResponseWriter, r *http.Request) {
	var req struct {
		QRCode string `json:"qr_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	m := qrPattern.FindStringSubmatch(req.QRCode)
	if m == nil {
		httpx.WriteErr(w, http.StatusBadRequest, "código QR inválido")
		return
	}

	wcOrderID, _ := strconv.ParseInt(m[1], 10, 64)
	pedidoID, _ := strconv.ParseInt(m[2], 10, 64)
	receivedMAC := m[3]

	salt := os.Getenv("WP_SALT_AUTH")
	if salt == "" {
		slog.Error("[rota] WP_SALT_AUTH não configurado")
		httpx.WriteErr(w, http.StatusServiceUnavailable, "configuração ausente")
		return
	}

	// Mesmo algoritmo de sz_mbc_package_code() em PHP:
	// hmac_sha256(WP_SALT_AUTH, "{wc_order_id}-{pedido_id}") primeiros 14 chars
	mac := hmac.New(sha256.New, []byte(salt))
	mac.Write([]byte(fmt.Sprintf("%d-%d", wcOrderID, pedidoID)))
	expectedMAC := hex.EncodeToString(mac.Sum(nil))[:14]

	if !hmac.Equal([]byte(receivedMAC), []byte(expectedMAC)) {
		slog.Warn("[rota] QR com HMAC inválido", "pedido_id", pedidoID)
		httpx.WriteErr(w, http.StatusForbidden, "QR inválido ou expirado")
		return
	}

	ctx := r.Context()

	var status string
	err := h.Pool.QueryRow(ctx,
		`SELECT status FROM sz_motoboy_pedidos WHERE id=$1`, pedidoID,
	).Scan(&status)
	if err != nil {
		httpx.WriteErr(w, http.StatusNotFound, "pedido não encontrado")
		return
	}
	if status != "embalado" && status != "agendado" {
		httpx.WriteErr(w, http.StatusConflict, "pedido não está embalado/agendado: "+status)
		return
	}

	_, err = h.Pool.Exec(ctx, `
		UPDATE sz_motoboy_pedidos
		SET status='em_rota', ts_em_rota=NOW()
		WHERE id=$1`, pedidoID,
	)
	if err != nil {
		slog.Error("[rota] falha ao iniciar rota", "pedido_id", pedidoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao iniciar rota")
		return
	}

	_, _ = h.Pool.Exec(ctx, `
		INSERT INTO sz_motoboy_audit (pedido_id, acao, de_status, para_status, created_at)
		VALUES ($1, 'rota_iniciada_qr', $2, 'em_rota', NOW())`,
		pedidoID, status,
	)

	slog.Info("[rota] rota iniciada via QR", "pedido_id", pedidoID, "wc_order_id", wcOrderID)
	httpx.WriteOK(w, map[string]any{"ok": true, "pedido_id": pedidoID, "status": "em_rota"})
}

// Entregar — POST /motoboy/entregar
// Body: {pedido_id, recebedor_nome?, cpf?, pgto_tipo?}
func (h *RotaHandler) Entregar(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PedidoID     int64  `json:"pedido_id"`
		RecebedorNome string `json:"recebedor_nome"`
		CPF          string `json:"cpf"`
		PgtoTipo     string `json:"pgto_tipo"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	if req.PedidoID == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "pedido_id obrigatório")
		return
	}

	ctx := r.Context()

	var status string
	err := h.Pool.QueryRow(ctx,
		`SELECT status FROM sz_motoboy_pedidos WHERE id=$1`, req.PedidoID,
	).Scan(&status)
	if err != nil {
		httpx.WriteErr(w, http.StatusNotFound, "pedido não encontrado")
		return
	}
	if status == "entregue" {
		httpx.WriteOK(w, map[string]any{"ok": true, "status": "entregue", "idempotent": true})
		return
	}

	_, err = h.Pool.Exec(ctx, `
		UPDATE sz_motoboy_pedidos
		SET status='entregue', ts_entregue=NOW(), baixa_at=NOW()
		WHERE id=$1`, req.PedidoID,
	)
	if err != nil {
		slog.Error("[rota] falha ao registrar entrega", "pedido_id", req.PedidoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao registrar entrega")
		return
	}

	_, _ = h.Pool.Exec(ctx, `
		INSERT INTO sz_motoboy_audit (pedido_id, acao, de_status, para_status, meta, created_at)
		VALUES ($1, 'entregue', $2, 'entregue', $3, NOW())`,
		req.PedidoID, status, req.RecebedorNome,
	)

	slog.Info("[rota] pedido entregue", "pedido_id", req.PedidoID)
	httpx.WriteOK(w, map[string]any{"ok": true, "status": "entregue"})
}

// Frustrar — POST /motoboy/frustrar
// Body: {pedido_id, motivo}
func (h *RotaHandler) Frustrar(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PedidoID int64  `json:"pedido_id"`
		Motivo   string `json:"motivo"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	if req.PedidoID == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "pedido_id obrigatório")
		return
	}
	if req.Motivo == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "motivo obrigatório")
		return
	}

	ctx := r.Context()

	var status string
	err := h.Pool.QueryRow(ctx,
		`SELECT status FROM sz_motoboy_pedidos WHERE id=$1`, req.PedidoID,
	).Scan(&status)
	if err != nil {
		httpx.WriteErr(w, http.StatusNotFound, "pedido não encontrado")
		return
	}
	if status == "entregue" || status == "cancelado" {
		httpx.WriteErr(w, http.StatusConflict, "pedido já finalizado: "+status)
		return
	}

	_, err = h.Pool.Exec(ctx, `
		UPDATE sz_motoboy_pedidos
		SET status='frustrado', ts_frustrado=NOW(), baixa_at=NOW()
		WHERE id=$1`, req.PedidoID,
	)
	if err != nil {
		slog.Error("[rota] falha ao registrar frustração", "pedido_id", req.PedidoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao registrar frustração")
		return
	}

	_, _ = h.Pool.Exec(ctx, `
		INSERT INTO sz_motoboy_audit (pedido_id, acao, de_status, para_status, meta, created_at)
		VALUES ($1, 'frustrado', $2, 'frustrado', $3, NOW())`,
		req.PedidoID, status, req.Motivo,
	)

	slog.Info("[rota] pedido frustrado", "pedido_id", req.PedidoID, "motivo", req.Motivo)
	httpx.WriteOK(w, map[string]any{"ok": true, "status": "frustrado"})
}
