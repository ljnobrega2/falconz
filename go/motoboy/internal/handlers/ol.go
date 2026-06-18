// Package handlers — endpoints OL (Operador Logístico).
// Auth: portal session ou manage_woocommerce (via auth.AuthPortal).
package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/motoboy-service/internal/httpx"
)

// statusWhitelist lista os status válidos para mudança via OL.
var statusWhitelist = map[string]bool{
	"agendado": true, "embalado": true, "em_rota": true,
	"entregue": true, "frustrado": true, "cancelado": true,
}

// OLHandler implementa os endpoints de Operador Logístico.
type OLHandler struct {
	Pool *pgxpool.Pool
}

// MudarStatus — POST /ol/mudar-status
// Body: {pedido_id, status, motivo?}
func (h *OLHandler) MudarStatus(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PedidoID int64  `json:"pedido_id"`
		Status   string `json:"status"`
		Motivo   string `json:"motivo"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	if req.PedidoID == 0 || req.Status == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "pedido_id e status obrigatórios")
		return
	}
	if !statusWhitelist[req.Status] {
		httpx.WriteErr(w, http.StatusBadRequest, "status inválido")
		return
	}

	ctx := r.Context()

	// Busca status atual para auditoria e validação.
	var deStatus string
	err := h.Pool.QueryRow(ctx,
		`SELECT status FROM sz_motoboy_pedidos WHERE id=$1`, req.PedidoID,
	).Scan(&deStatus)
	if err != nil {
		httpx.WriteErr(w, http.StatusNotFound, "pedido não encontrado")
		return
	}

	_, err = h.Pool.Exec(ctx,
		`UPDATE sz_motoboy_pedidos SET status=$1 WHERE id=$2`,
		req.Status, req.PedidoID,
	)
	if err != nil {
		slog.Error("[ol] falha ao mudar status", "pedido_id", req.PedidoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao atualizar status")
		return
	}

	_, _ = h.Pool.Exec(ctx, `
		INSERT INTO sz_motoboy_audit (pedido_id, acao, de_status, para_status, meta, created_at)
		VALUES ($1, 'status_alterado_ol', $2, $3, $4, NOW())`,
		req.PedidoID, deStatus, req.Status, req.Motivo,
	)

	slog.Info("[ol] status alterado", "pedido_id", req.PedidoID, "de", deStatus, "para", req.Status)
	httpx.WriteOK(w, map[string]any{"ok": true, "status": req.Status})
}

// TrocarMotoboy — POST /ol/trocar-motoboy
// Body: {pedido_id, motoboy_id}
func (h *OLHandler) TrocarMotoboy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PedidoID  int64 `json:"pedido_id"`
		MotoboyID int64 `json:"motoboy_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	if req.PedidoID == 0 || req.MotoboyID == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "pedido_id e motoboy_id obrigatórios")
		return
	}

	ctx := r.Context()

	// Verifica motoboy ativo.
	var ativo bool
	err := h.Pool.QueryRow(ctx,
		`SELECT ativo FROM sz_motoboys WHERE id=$1`, req.MotoboyID,
	).Scan(&ativo)
	if err != nil || !ativo {
		httpx.WriteErr(w, http.StatusBadRequest, "motoboy não encontrado ou inativo")
		return
	}

	_, err = h.Pool.Exec(ctx,
		`UPDATE sz_motoboy_pedidos SET motoboy_id=$1 WHERE id=$2`,
		req.MotoboyID, req.PedidoID,
	)
	if err != nil {
		slog.Error("[ol] falha ao trocar motoboy", "pedido_id", req.PedidoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao trocar motoboy")
		return
	}

	_, _ = h.Pool.Exec(ctx, `
		INSERT INTO sz_motoboy_audit (pedido_id, acao, meta, created_at)
		VALUES ($1, 'motoboy_trocado_ol', $2, NOW())`,
		req.PedidoID, req.MotoboyID,
	)

	slog.Info("[ol] motoboy trocado", "pedido_id", req.PedidoID, "motoboy_id", req.MotoboyID)
	httpx.WriteOK(w, map[string]any{"ok": true})
}

// MotoboysDodia — GET /ol/motoboys-do-dia
// Retorna pedidos do dia agrupados por motoboy com KPIs.
func (h *OLHandler) MotoboysDoDia(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	rows, err := h.Pool.Query(ctx, `
		SELECT
			COALESCE(m.id, 0)             AS motoboy_id,
			COALESCE(m.nome, 'Sem motoboy') AS motoboy_nome,
			COUNT(*)                       AS total,
			COUNT(*) FILTER (WHERE p.status = 'entregue')  AS entregues,
			COUNT(*) FILTER (WHERE p.status = 'frustrado') AS frustrados,
			COUNT(*) FILTER (WHERE p.status = 'em_rota')   AS em_rota,
			COUNT(*) FILTER (WHERE p.status = 'agendado' OR p.status = 'embalado') AS pendentes,
			COALESCE(SUM(p.valor_pedido) FILTER (WHERE p.status = 'entregue'), 0) AS total_entregue_rs
		FROM sz_motoboy_pedidos p
		LEFT JOIN sz_motoboys m ON m.id = p.motoboy_id
		WHERE p.ts_aprovado::date = CURRENT_DATE
		GROUP BY m.id, m.nome
		ORDER BY motoboy_nome`)
	if err != nil {
		slog.Error("[ol] falha ao buscar motoboys do dia", "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao buscar dados")
		return
	}
	defer rows.Close()

	type motoboyKPI struct {
		MotoboyID       int64   `json:"motoboy_id"`
		MotoboyNome     string  `json:"motoboy_nome"`
		Total           int     `json:"total"`
		Entregues       int     `json:"entregues"`
		Frustrados      int     `json:"frustrados"`
		EmRota          int     `json:"em_rota"`
		Pendentes       int     `json:"pendentes"`
		TotalEntregueRS float64 `json:"total_entregue_rs"`
	}

	var result []motoboyKPI
	for rows.Next() {
		var k motoboyKPI
		if err := rows.Scan(
			&k.MotoboyID, &k.MotoboyNome,
			&k.Total, &k.Entregues, &k.Frustrados, &k.EmRota, &k.Pendentes,
			&k.TotalEntregueRS,
		); err != nil {
			continue
		}
		result = append(result, k)
	}

	httpx.WriteOK(w, map[string]any{"ok": true, "motoboys": result})
}

// Motoboys — GET /ol/motoboys
// Lista todos os motoboys ativos.
func (h *OLHandler) Motoboys(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	rows, err := h.Pool.Query(ctx, `
		SELECT id, nome, telefone, ativo
		FROM sz_motoboys
		WHERE ativo = true
		ORDER BY nome`)
	if err != nil {
		slog.Error("[ol] falha ao listar motoboys", "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao buscar motoboys")
		return
	}
	defer rows.Close()

	type motoboy struct {
		ID       int64  `json:"id"`
		Nome     string `json:"nome"`
		Telefone string `json:"telefone"`
		Ativo    bool   `json:"ativo"`
	}

	var result []motoboy
	for rows.Next() {
		var m motoboy
		if err := rows.Scan(&m.ID, &m.Nome, &m.Telefone, &m.Ativo); err != nil {
			continue
		}
		result = append(result, m)
	}

	httpx.WriteOK(w, map[string]any{"ok": true, "motoboys": result})
}
