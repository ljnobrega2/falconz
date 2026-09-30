package handlers

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/senderzz/labels-service/internal/httpx"
	"github.com/senderzz/labels-service/internal/trackinghistory"
)

// AUDIT-2026-07-30 (dono): "preciso que o processo completo do pedido seja
// acompanhado via atualização do melhor envio - tb preciso de uma opção a
// nivel admin que consulte esses casos caso haja falha eu consiga executar
// manualmente". PostMEWebhook (me_webhook.go) já sincroniza sz_orders.status
// automaticamente a cada evento — este arquivo cobre o caso de FALHA (webhook
// não chegou, secret mudou, ME não reenviou): lista os pedidos onde o status
// da etiqueta ficou à frente do status do pedido, e permite forçar a sync
// consultando a ME diretamente (sem depender do webhook).

// InternalMEStatusLive — GET /internal/me-status/live/{order_id}.
// AUDIT-2026-07-30 #6 (dono: "última atualização direto do ME"): consulta a ME
// EM TEMPO REAL (read-only, SEM gravar nada) — pro rastreio público mostrar o
// status confirmado agora mesmo, sem depender de webhook/sync já ter rodado.
// Best-effort: chamador (orders-service) deve tratar erro/timeout como "sem
// dado ao vivo disponível", nunca falhar a página de rastreio por causa disso.
func (h *LabelHandler) InternalMEStatusLive(w http.ResponseWriter, r *http.Request) {
	orderID, err := strconv.ParseInt(chi.URLParam(r, "order_id"), 10, 64)
	if err != nil || orderID <= 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "order_id inválido")
		return
	}

	var shipmentID string
	if err := h.db.QueryRow(r.Context(),
		`SELECT me_shipment_id FROM wc_me_labels
		  WHERE wc_order_id = $1 AND status <> 'canceled'
		  ORDER BY id DESC LIMIT 1`,
		orderID,
	).Scan(&shipmentID); err != nil {
		httpx.WriteErr(w, http.StatusNotFound, "pedido sem etiqueta ME")
		return
	}

	meStatus, err := h.me.GetShipmentStatus(r.Context(), shipmentID)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadGateway, "falha ao consultar Melhor Envio")
		return
	}

	httpx.WriteOK(w, map[string]any{
		"ok": true, "order_id": orderID,
		"me_status": meStatus.Status, "checked_at": time.Now().Format(time.RFC3339),
	})
}

// InternalMEStatusEvents — GET /internal/me-status/events/{order_id}.
// AUDIT-2026-07-30 #8 (dono: mostrou print do painel público ME com evento
// "Adicionado no sistema 29 jul 10:22 — A etiqueta de envio já foi criada e o
// pacote está pronto para a coleta ou postagem"): confirmado que a ME TEM
// timestamps reais por etapa via POST /me/shipment/tracking (created_at/
// paid_at/generated_at/posted_at/delivered_at/canceled_at/expired_at) — ao
// contrário do que eu tinha concluído antes testando o path errado
// (GET /me/shipment/tracking/{code} não existe; o certo é POST com
// {"orders":[...]}). Read-only, best-effort.
func (h *LabelHandler) InternalMEStatusEvents(w http.ResponseWriter, r *http.Request) {
	orderID, err := strconv.ParseInt(chi.URLParam(r, "order_id"), 10, 64)
	if err != nil || orderID <= 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "order_id inválido")
		return
	}

	var shipmentID string
	if err := h.db.QueryRow(r.Context(),
		`SELECT me_shipment_id FROM wc_me_labels
		  WHERE wc_order_id = $1 AND status <> 'canceled'
		  ORDER BY id DESC LIMIT 1`,
		orderID,
	).Scan(&shipmentID); err != nil {
		httpx.WriteErr(w, http.StatusNotFound, "pedido sem etiqueta ME")
		return
	}

	// AUDIT-2026-07-30 #11 (dono confirmou "authorization_code... esse é o
	// rastreio jadlog"): GET /me/orders/{id} (GetShipmentStatus) tem TODOS os
	// campos que precisamos num só lugar — status, timestamps por etapa,
	// authorization_code (rastreio real, mais confiável que "tracking" que
	// fica null por mais tempo) e delivery_min/max (prazo em dias). Substitui
	// a chamada separada a GetShipmentTrackingEvents (POST /shipment/tracking).
	ev, err := h.me.GetShipmentStatus(r.Context(), shipmentID)
	if err != nil {
		slog.Error("[senderzz_labels] InternalMEStatusEvents", "order_id", orderID, "shipment_id", shipmentID, "err", err)
		httpx.WriteErr(w, http.StatusBadGateway, "falha ao consultar Melhor Envio")
		return
	}

	// AUDIT-2026-07-30 #12 (dono: "somente jadlog não tava pegando o resto tava
	// ok"): CONFIRMADO no dado cru — "tracking" e "authorization_code" NÃO são
	// intercambiáveis por transportadora. Loggi: tracking="LGI-...BR" (real,
	// certo) e authorization_code="IFFCV..." (outra coisa, NÃO é rastreio).
	// Jadlog: tracking="" (fica vazio até coleta física) e authorization_code
	// = código real (confirmado pelo dono). Por isso "tracking" tem prioridade
	// SEMPRE que vier preenchido — authorization_code só é fallback pro caso
	// Jadlog (onde tracking demora a chegar).
	tracking := ev.Tracking
	if strings.TrimSpace(tracking) == "" {
		tracking = ev.AuthorizationCode
	}
	if err := trackinghistory.Save(r.Context(), h.db, trackinghistory.Snapshot{
		ShipmentID: shipmentID, OrderID: orderID, Status: ev.Status,
		Tracking: ev.Tracking, AuthorizationCode: ev.AuthorizationCode,
		CreatedAt: ev.CreatedAt, PaidAt: ev.PaidAt, GeneratedAt: ev.GeneratedAt,
		PostedAt: ev.PostedAt, ReceivedAt: ev.ReceivedAt,
		DeliveredAt: ev.DeliveredAt, CanceledAt: ev.CanceledAt,
	}); err != nil {
		// Histórico é importante, mas uma falha de gravação não pode derrubar o
		// rastreio público que ainda pode ser servido com o dado live.
		slog.Warn("[senderzz_labels] InternalMEStatusEvents: falha ao gravar histórico", "order_id", orderID, "err", err)
	}

	httpx.WriteOK(w, map[string]any{
		"ok": true, "order_id": orderID, "status": ev.Status,
		"created_at": ev.CreatedAt, "paid_at": ev.PaidAt,
		"generated_at": ev.GeneratedAt, "posted_at": ev.PostedAt,
		"received_at":  ev.ReceivedAt,
		"delivered_at": ev.DeliveredAt, "canceled_at": ev.CanceledAt,
		"tracking": tracking, "delivery_min": ev.DeliveryMin, "delivery_max": ev.DeliveryMax,
	})
}

type meStatusMismatchRow struct {
	WCOrderID    int64  `json:"wc_order_id"`
	ShipmentID   string `json:"me_shipment_id"`
	LabelStatus  string `json:"label_status"`
	OrderStatus  string `json:"order_status"`
	TrackingCode string `json:"tracking_code"`
	UpdatedAt    string `json:"updated_at"`
}

// InternalMEStatusMismatches — GET /internal/me-status/mismatches
// Lista etiquetas cujo status (já mapeado pra sz_orders) está à frente do
// status atual do pedido — ou seja, casos onde a sync automática (webhook)
// não rodou ou falhou.
func (h *LabelHandler) InternalMEStatusMismatches(w http.ResponseWriter, r *http.Request) {
	// DISTINCT ON pega só a etiqueta MAIS RECENTE por pedido — candidatos
	// descartados no retry multi-transportadora (emit.go cancela o shipment
	// anterior ao tentar o próximo) não podem contar como "pedido frustrado".
	rows, err := h.db.Query(r.Context(), `
		SELECT DISTINCT ON (l.wc_order_id)
		       l.wc_order_id, l.me_shipment_id, l.status, o.status, COALESCE(l.tracking_code,''), l.updated_at::text
		  FROM wc_me_labels l
		  JOIN sz_orders o ON o.id = l.wc_order_id
		 WHERE o.status NOT IN ('cancelled','reembolsado')
		   AND l.status <> 'canceled'
		 ORDER BY l.wc_order_id, l.id DESC
		 LIMIT 500`)
	if err != nil {
		slog.Error("[senderzz_labels] InternalMEStatusMismatches: query", "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao consultar")
		return
	}
	defer rows.Close()

	out := []meStatusMismatchRow{}
	for rows.Next() {
		var m meStatusMismatchRow
		if err := rows.Scan(&m.WCOrderID, &m.ShipmentID, &m.LabelStatus, &m.OrderStatus, &m.TrackingCode, &m.UpdatedAt); err != nil {
			continue
		}
		target := mapMEStatusToOrderStatus(m.LabelStatus)
		if target == "" {
			continue
		}
		targetRank, ok1 := orderStatusRank[target]
		curRank, ok2 := orderStatusRank[m.OrderStatus]
		if ok1 && ok2 && curRank >= targetRank {
			continue // já sincronizado ou à frente — não é mismatch
		}
		out = append(out, m)
	}

	httpx.WriteOK(w, map[string]any{"ok": true, "mismatches": out})
}

// InternalMEStatusSync — POST /internal/me-status/sync/{order_id}
// Consulta a ME diretamente (não depende do webhook) e força a sincronização
// de sz_orders.status pro pedido informado, com o mesmo guard-rail de
// avanço-só do fluxo automático.
func (h *LabelHandler) InternalMEStatusSync(w http.ResponseWriter, r *http.Request) {
	orderID, err := strconv.ParseInt(chi.URLParam(r, "order_id"), 10, 64)
	if err != nil || orderID <= 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "order_id inválido")
		return
	}

	var shipmentID string
	if err := h.db.QueryRow(r.Context(),
		`SELECT me_shipment_id FROM wc_me_labels
		  WHERE wc_order_id = $1 AND status <> 'canceled'
		  ORDER BY id DESC LIMIT 1`,
		orderID,
	).Scan(&shipmentID); err != nil {
		httpx.WriteErr(w, http.StatusNotFound, "pedido sem etiqueta ME")
		return
	}

	meStatus, err := h.me.GetShipmentStatus(r.Context(), shipmentID)
	if err != nil {
		slog.Error("[senderzz_labels] InternalMEStatusSync: ME", "order_id", orderID, "shipment_id", shipmentID, "err", err)
		httpx.WriteErr(w, http.StatusBadGateway, "falha ao consultar Melhor Envio: "+err.Error())
		return
	}

	internalStatus := mapTrackingStatus(meStatus.Status)

	if _, err := h.db.Exec(r.Context(),
		`UPDATE wc_me_labels SET status = $1, updated_at = NOW()
		  WHERE me_shipment_id = $2 AND status NOT IN ('canceled', 'delivered')`,
		internalStatus, shipmentID,
	); err != nil {
		slog.Error("[senderzz_labels] InternalMEStatusSync: update label", "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao atualizar etiqueta")
		return
	}

	synced, prevStatus, err := syncOrderStatusFromME(r.Context(), h.db, orderID, meStatus.Status)
	if err != nil {
		slog.Error("[senderzz_labels] InternalMEStatusSync: sync order", "order_id", orderID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao sincronizar pedido")
		return
	}

	slog.Info("[senderzz_labels] InternalMEStatusSync manual", "order_id", orderID, "shipment_id", shipmentID,
		"me_status", meStatus.Status, "interno", internalStatus, "synced", synced, "prev_status", prevStatus)

	httpx.WriteOK(w, map[string]any{
		"ok": true, "order_id": orderID, "me_status": meStatus.Status,
		"status_interno": internalStatus, "synced": synced, "prev_status": prevStatus,
	})
}
