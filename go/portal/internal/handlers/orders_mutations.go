// orders_mutations.go — MUTAÇÕES de pedido motoboy escopadas ao AFILIADO dono.
//
// 2ª onda do portal (REGRA DA TASK): o afiliado passa a poder REAGENDAR, REAGENDAR
// POR CLONE (só frustrado) e CANCELAR os pedidos motoboy DELE — com as MESMAS regras
// de negócio do produtor/admin (status válidos, 5 dias úteis, zona schedule, clone só
// frustrado), reutilizando os helpers compartilhados de motoboy_shared.go.
//
// Rotas (no OrdersHandler — orders.go é read-only; as mutações vivem aqui):
//
//	POST /portal/orders/{id}/reagendar        — reagenda (status agendado|embalado|frustrado|cancelado)
//	POST /portal/orders/{id}/reagendar-clone  — clona pedido FRUSTRADO p/ reentrega
//	POST /portal/orders/{id}/cancelar         — cancela (status agendado|embalado) + bridge sz_orders
//
// ── ESCOPO DE SEGURANÇA (fail-closed, equality estrita) ────────────────────────
//
// {id} = sz_orders.id (MESMA convenção do GET /portal/orders/{id} Detail — NÃO é
// wc_order_id como nas rotas /portal/motoboy/{id}/* do produtor; são DOIS id-spaces,
// cada um consistente dentro do seu namespace).
//
// PROPRIEDADE DO AFILIADO: o.affiliate_id = u.WPUserID (CANONICAL — id-space do
// afiliado em sz_orders é o wp_user_id, NUNCA o portal id; idêntico a orders.go List/
// Detail). O afiliado SÓ pode mutar pedido onde affiliate_id = u.WPUserID; qualquer
// outro → 404 (não 403 — não vaza existência, espelha Detail). Um afiliado NUNCA muta
// o pedido de outro afiliado nem o pedido de um produtor.
//
// resolveAffiliateOwnedPedido é um resolver SEPARADO do produtor: o
// resolveOwnedMotoboyPedido (motoboy_portal.go) hard-rejeita não-produtor com 403 —
// reusá-lo aqui faria TODA chamada de afiliado dar 403. Este resolver é keyed em
// affiliate_id/WPUserID e é a única porta de entrada das três mutações.
package handlers

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/senderzz/portal-service/internal/auth"
	"github.com/senderzz/portal-service/internal/httpx"
)

// isAffiliateRoleOrders — detecção de role afiliado (mesma de orders.go List/Detail).
func isAffiliateRoleOrders(role string) bool {
	return role == "afiliado" || role == "affiliate" || role == "afiliada" || role == "cliente"
}

// resolveAffiliateOwnedPedido — confirma que o pedido sz_orders.id pertence ao AFILIADO
// autenticado (o.affiliate_id = u.WPUserID) e devolve (wcOrderID, mbPedidoID, mbStatus).
//
// Fail-closed:
//   - role não-afiliado            → 403 (rota é exclusiva do afiliado dono);
//   - sz_orders/sz_motoboy ausente → 503 (graceful);
//   - pedido fora do escopo        → 404 (NÃO 403 — não vaza posse, espelha Detail);
//   - sem pedido motoboy associado → 404.
//
// SEGURANÇA: equality estrita o.id = $1 AND o.affiliate_id = $2 (NUNCA OR/IN). Um
// afiliado com WPUserID=28 não casa um pedido com affiliate_id=54 → 404.
func (h *OrdersHandler) resolveAffiliateOwnedPedido(
	ctx context.Context, u *auth.PortalUser, szOrderID int64,
) (wcOrderID, mbPedidoID int64, mbStatus string, httpStatus int, errMsg string) {
	if !isAffiliateRoleOrders(u.Role) {
		return 0, 0, "", http.StatusForbidden, "apenas o afiliado dono pode alterar este pedido"
	}
	if !h.tableExists(ctx, "sz_orders") {
		return 0, 0, "", http.StatusServiceUnavailable, "sz_orders ainda não migrada"
	}
	if !h.tableExists(ctx, "sz_motoboy_pedidos") {
		return 0, 0, "", http.StatusServiceUnavailable, "sz_motoboy_pedidos ainda não migrada"
	}

	// Propriedade do afiliado: o.id = $1 AND o.affiliate_id = u.WPUserID (estrito).
	var wcID sql.NullInt64
	err := h.Pool.QueryRow(ctx,
		`SELECT o.wp_order_id FROM sz_orders o
		  WHERE o.id = $1 AND o.affiliate_id = $2`,
		szOrderID, u.WPUserID,
	).Scan(&wcID)
	if err == pgx.ErrNoRows {
		// Fora do escopo OU inexistente → 404 (não diferencia: não vaza posse).
		return 0, 0, "", http.StatusNotFound, "pedido não encontrado"
	}
	if err != nil {
		slog.Error("[portal_orders] erro ao verificar propriedade afiliado", "user_id", u.ID, "order_id", szOrderID, "err", err)
		return 0, 0, "", http.StatusInternalServerError, "erro interno"
	}
	if !wcID.Valid || wcID.Int64 <= 0 {
		return 0, 0, "", http.StatusNotFound, "pedido sem wc_order_id"
	}
	wcOrderID = wcID.Int64

	// Resolve o pedido motoboy correspondente (mais recente) por wc_order_id.
	var pid int64
	var st sql.NullString
	err = h.Pool.QueryRow(ctx,
		`SELECT id, COALESCE(status,'') FROM sz_motoboy_pedidos
		  WHERE wc_order_id = $1 ORDER BY id DESC LIMIT 1`,
		wcOrderID,
	).Scan(&pid, &st)
	if err == pgx.ErrNoRows {
		return 0, 0, "", http.StatusNotFound, "pedido motoboy não encontrado"
	}
	if err != nil {
		slog.Error("[portal_orders] erro ao resolver pedido motoboy do afiliado", "user_id", u.ID, "wc_order_id", wcOrderID, "err", err)
		return 0, 0, "", http.StatusInternalServerError, "erro interno"
	}
	return wcOrderID, pid, st.String, 0, ""
}

// parseSzOrderID — extrai e valida o {id} de path (sz_orders.id).
func parseSzOrderID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// ── POST /portal/orders/{id}/reagendar (afiliado) ──────────────────────────────

// ReagendarAfiliado — grava nova data de entrega no pedido motoboy do AFILIADO dono.
// {id} = sz_orders.id. Mesmas regras do produtor: status ∈ {agendado, embalado,
// frustrado, cancelado}, gate de data compartilhado (past + ZONA + 5 dias úteis).
func (h *OrdersHandler) ReagendarAfiliado(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	ctx := r.Context()

	szOrderID, ok := parseSzOrderID(r)
	if !ok {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}

	var body reagendarMBBody
	if err := decodeJSONBody(r, &body); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "json inválido")
		return
	}
	chosen, perr := time.Parse("2006-01-02", strings.TrimSpace(body.Data))
	if perr != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "data inválida — use o formato YYYY-MM-DD")
		return
	}

	wcOrderID, mbPedidoID, status, httpStatus, errMsg := h.resolveAffiliateOwnedPedido(ctx, u, szOrderID)
	if httpStatus != 0 {
		httpx.WriteErr(w, httpStatus, errMsg)
		return
	}

	if !statusReagendaveis[status] {
		httpx.WriteErr(w, http.StatusUnprocessableEntity,
			"pedido não pode ser reagendado neste status ("+status+")")
		return
	}

	newStatus, gateStatus, gateMsg := reagendarDateGate(ctx, h.Pool, mbPedidoID, chosen)
	if gateStatus != 0 {
		httpx.WriteErr(w, gateStatus, gateMsg)
		return
	}

	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx,
		`UPDATE sz_motoboy_pedidos
		    SET reagendado_para = $1, status = $2, updated_at = NOW()
		  WHERE id = $3`,
		body.Data, newStatus, mbPedidoID,
	)
	if err != nil {
		slog.Error("[portal_orders] erro ao reagendar (afiliado)", "user_id", u.ID, "mb_pedido_id", mbPedidoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.WriteErr(w, http.StatusNotFound, "pedido motoboy não encontrado")
		return
	}

	// BRIDGE reverso: reagendar pedido frustrado/cancelado reativa o motoboy mas
	// sz_orders.status ficava preso — reset p/ 'pending'.
	if status == "frustrado" || status == "cancelado" {
		if _, err := tx.Exec(ctx, `
			UPDATE sz_orders o
			   SET status = 'pending', updated_at = NOW()
			  FROM sz_motoboy_pedidos p
			 WHERE p.id = $1
			   AND COALESCE(o.wp_order_id, o.id) = p.wc_order_id
			   AND o.status <> 'pending'`,
			mbPedidoID,
		); err != nil {
			slog.Error("[portal_orders] erro ao sincronizar sz_orders", "mb_pedido_id", mbPedidoID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	writeMotoboyAuditPortal(ctx, h.Pool, u, "afiliado", mbPedidoID, "reagendar", status, newStatus)
	slog.Info("[portal_orders] pedido reagendado por afiliado", "user_id", u.ID, "mb_pedido_id", mbPedidoID, "para", body.Data)

	httpx.WriteOK(w, map[string]any{
		"mensagem":        "pedido reagendado com sucesso",
		"order_id":        szOrderID,
		"mb_pedido_id":    mbPedidoID,
		"wc_order_id":     wcOrderID,
		"reagendado_para": body.Data,
		"new_status":      newStatus,
	})
}

// ── POST /portal/orders/{id}/reagendar-clone (afiliado) ────────────────────────

// ReagendarCloneAfiliado — CLONE de pedido motoboy FRUSTRADO do AFILIADO dono p/
// reentrega. {id} = sz_orders.id. Mesmas regras do produtor/admin: só frustrado,
// clone órfão (não cria sz_orders), zona + 5 dias úteis. Audit actor_tipo='afiliado'.
func (h *OrdersHandler) ReagendarCloneAfiliado(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	ctx := r.Context()

	szOrderID, ok := parseSzOrderID(r)
	if !ok {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}

	var body reagendarMBBody
	if err := decodeJSONBody(r, &body); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "json inválido")
		return
	}
	chosen, perr := time.Parse("2006-01-02", strings.TrimSpace(body.Data))
	if perr != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "data inválida — use o formato YYYY-MM-DD")
		return
	}

	_, mbPedidoID, status, httpStatus, errMsg := h.resolveAffiliateOwnedPedido(ctx, u, szOrderID)
	if httpStatus != 0 {
		httpx.WriteErr(w, httpStatus, errMsg)
		return
	}

	if status != "frustrado" && status != "cancelado" {
		httpx.WriteErr(w, http.StatusConflict,
			"apenas pedidos frustrados ou cancelados podem ser reagendados por clone (status atual: "+status+")")
		return
	}

	newStatus, gateStatus, gateMsg := reagendarDateGate(ctx, h.Pool, mbPedidoID, chosen)
	if gateStatus != 0 {
		httpx.WriteErr(w, gateStatus, gateMsg)
		return
	}

	cloneID, newWCOrderID, cloneStatus, cloneMsg := cloneFrustradoPedido(ctx, h.Pool, mbPedidoID, body.Data, newStatus)
	if cloneStatus != 0 {
		httpx.WriteErr(w, cloneStatus, cloneMsg)
		return
	}

	writeMotoboyAuditPortal(ctx, h.Pool, u, "afiliado", mbPedidoID, "reagendar_clone", status, status)
	writeMotoboyAuditPortal(ctx, h.Pool, u, "afiliado", cloneID, "reagendar_clone", "", newStatus)
	slog.Info("[portal_orders] pedido frustrado clonado p/ reentrega (afiliado)",
		"user_id", u.ID, "origem_pedido_id", mbPedidoID, "clone_pedido_id", cloneID,
		"clone_wc_order_id", newWCOrderID, "para", body.Data)

	httpx.WriteOK(w, map[string]any{
		"mensagem":          "pedido frustrado reagendado por clone com sucesso",
		"order_id":          szOrderID,
		"origem_pedido_id":  mbPedidoID,
		"clone_pedido_id":   cloneID,
		"clone_wc_order_id": newWCOrderID,
		"reagendado_para":   body.Data,
		"new_status":        newStatus,
	})
}

// ── POST /portal/orders/{id}/cancelar (afiliado) ───────────────────────────────

// CancelarAfiliado — cancela o pedido motoboy do AFILIADO dono. {id} = sz_orders.id.
// Só status ∈ {agendado, embalado}. MESMO bridge same-tx do produtor (motoboy
// 'cancelado' + sz_orders 'cancelled') — sem o bridge o pedido continuaria contando
// como receita ativa. Audit actor_tipo='afiliado'.
func (h *OrdersHandler) CancelarAfiliado(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	ctx := r.Context()

	szOrderID, ok := parseSzOrderID(r)
	if !ok {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}

	wcOrderID, mbPedidoID, status, httpStatus, errMsg := h.resolveAffiliateOwnedPedido(ctx, u, szOrderID)
	if httpStatus != 0 {
		httpx.WriteErr(w, httpStatus, errMsg)
		return
	}

	if !statusCancelaveis[status] {
		httpx.WriteErr(w, http.StatusUnprocessableEntity,
			"pedido não pode ser cancelado neste status ("+status+")")
		return
	}

	// Bridge same-tx via helper compartilhado (espelha producer Cancelar):
	// motoboy 'cancelado' + sz_orders 'cancelled' na MESMA tx.
	szBridged, bridgeStatus, bridgeMsg := cancelMotoboyWithBridge(ctx, h.Pool, mbPedidoID, wcOrderID)
	if bridgeStatus != 0 {
		slog.Error("[portal_orders] erro ao cancelar (afiliado)", "user_id", u.ID, "mb_pedido_id", mbPedidoID, "wc_order_id", wcOrderID, "http", bridgeStatus)
		httpx.WriteErr(w, bridgeStatus, bridgeMsg)
		return
	}

	writeMotoboyAuditPortal(ctx, h.Pool, u, "afiliado", mbPedidoID, "cancelar", status, "cancelado")
	if szBridged {
		slog.Info("[bridge] sz_orders.status sincronizado",
			"scope", "portal_orders.CancelarAfiliado", "wc_order_id", wcOrderID,
			"motoboy_status", "cancelado", "sz_orders_status", "cancelled")
	}
	slog.Info("[portal_orders] pedido cancelado por afiliado", "user_id", u.ID, "mb_pedido_id", mbPedidoID)

	httpx.WriteOK(w, map[string]any{
		"mensagem":     "pedido cancelado com sucesso",
		"order_id":     szOrderID,
		"mb_pedido_id": mbPedidoID,
		"wc_order_id":  wcOrderID,
		"new_status":   "cancelado",
	})
}
