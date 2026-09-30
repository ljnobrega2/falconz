package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/senderzz/labels-service/internal/httpx"
	"github.com/senderzz/labels-service/internal/trackinghistory"
)

// PostMEWebhook recebe eventos NATIVOS do Melhor Envio (painel Área Dev → Webhooks).
// URL pública: POST/GET /wp-json/wc-melhor-envio/v1/webhook/me
//
// Espelho fiel de includes/senderzz-me-webhook.php:
//   - GET ou POST vazio  → ACK 200 (o painel ME usa isso pra validar/salvar a URL).
//   - event ping/test    → 200 "pong" (sem exigir assinatura).
//   - eventos reais      → valida header x-me-signature = base64(HMAC-SHA256(body, secret)),
//     acha a etiqueta por me_shipment_id (data.id/order_id/shipment_id)
//     e atualiza o status (mapTrackingStatus). Idempotência via
//     tpc_webhook_events (mesma tabela do /webhook/tracking).
//
// Secret = ME_WEBHOOK_SECRET (o client_secret do app Melhor Envio). FAIL-CLOSED: sem
// secret, eventos ASSINADOS são recusados (mas o teste/ping ainda responde 200 para que
// a URL possa ser salva no painel ME enquanto o secret não é configurado).
func (h *LabelHandler) PostMEWebhook(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB máximo
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "erro ao ler corpo da requisição")
		return
	}

	// GET ou corpo vazio = teste do painel ME → ACK imediato (permite salvar a URL).
	if r.Method == http.MethodGet || len(strings.TrimSpace(string(bodyBytes))) == 0 {
		slog.Info("[senderzz_labels] webhook ME: teste/handshake", "method", r.Method, "ip", r.RemoteAddr)
		httpx.WriteOK(w, map[string]any{"ok": true, "note": "Webhook Melhor Envio ativo."})
		return
	}

	var p struct {
		Event string `json:"event"`
		Data  struct {
			ID         any    `json:"id"`
			OrderID    any    `json:"order_id"`
			ShipmentID any    `json:"shipment_id"`
			Protocol   string `json:"protocol"`
			Status     string `json:"status"`
			// AUDIT-2026-07-28 (dono): "transportadora e rastreio que já veio via
			// webhook mas não preencheu automaticamente" — a ME manda o código de
			// rastreio dentro do PRÓPRIO evento (campo "tracking", às vezes só
			// preenchido horas depois via reenvio do evento — docs.melhorenvio.com.br/
			// reference/webhooks). Esse handler só atualizava `status`, descartando
			// "tracking"/"self_tracking" do payload — corrigido abaixo.
			Tracking     string `json:"tracking"`
			SelfTracking string `json:"self_tracking"`
		} `json:"data"`
	}
	if err := json.Unmarshal(bodyBytes, &p); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "payload inválido")
		return
	}

	// ping/test → ACK sem processar nem exigir assinatura (espelha o PHP).
	switch strings.ToLower(strings.TrimSpace(p.Event)) {
	case "ping", "test", "webhook.ping":
		httpx.WriteOK(w, map[string]any{"ok": true, "note": "pong"})
		return
	}

	// Validação de assinatura — só processa evento real se autenticado.
	secret := strings.TrimSpace(os.Getenv("ME_WEBHOOK_SECRET"))
	if secret == "" {
		// Fail-closed: sem secret não dá pra confiar no evento. Não derruba a URL
		// (o teste/ping acima já passou), mas recusa o evento real.
		slog.Error("[senderzz_labels] webhook ME: ME_WEBHOOK_SECRET ausente — evento recusado", "ip", r.RemoteAddr)
		httpx.WriteErr(w, http.StatusServiceUnavailable, "webhook não configurado (secret ausente)")
		return
	}
	sig := r.Header.Get("x-me-signature")
	if !validateMEHMAC(bodyBytes, sig, secret) {
		slog.Warn("[senderzz_labels] webhook ME: assinatura inválida", "ip", r.RemoteAddr)
		httpx.WriteErr(w, http.StatusUnauthorized, "assinatura inválida")
		return
	}

	// AUDIT-2026-07-30 HIGH: removido o branch "balance.paid"/"balance.confirmed"
	// que só existia aqui — a ME não emite esse evento no painel de webhooks
	// configurável (confirmado por print do dono, ver balance.go:41-44), então
	// nunca disparava de verdade. Mas SE algum dia disparasse (replay, mudança
	// da ME), o branch só fazia `UPDATE ... SET status='paid'` sem chamar
	// confirmPendingCharge — não creditava tpc_carteira nem inseria
	// tpc_transacoes. A charge ficaria marcada 'paid' pra sempre sem o produtor
	// nunca ter recebido o dinheiro (ReconcileMEChargesTick só olha
	// status='pending', então nunca mais tentaria de novo). Confirmação real de
	// recarga PIX é e continua sendo SÓ via reconciliação ativa
	// (ReconcileMEChargesTick em balance.go), que é atômica e correta.

	meID := firstNonEmpty(toStr(p.Data.ID), toStr(p.Data.OrderID), toStr(p.Data.ShipmentID))
	if meID == "" {
		// Sem identificador → nada a fazer; ACK 200 p/ o ME não ficar reenviando.
		httpx.WriteOK(w, map[string]any{"ok": true, "note": "sem data.id — ignorado"})
		return
	}
	internalStatus := mapTrackingStatus(strings.TrimSpace(p.Data.Status))

	// Idempotência + UPDATE atômicos (padrão do /webhook/tracking).
	eventKey := fmt.Sprintf("me:%s:%s", meID, p.Data.Status)
	payloadHash := cacheKeyFrom(string(bodyBytes))

	tx, err := h.db.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao processar webhook")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	evtTag, err := tx.Exec(r.Context(),
		`INSERT INTO tpc_webhook_events
		    (event_key, source, event_type, payload_hash, me_id, status)
		 VALUES ($1, 'melhor_envio', $2, $3, $4, $5)
		 ON CONFLICT (event_key) DO NOTHING`,
		eventKey, firstNonEmpty(p.Event, "status_update"), payloadHash, meID, p.Data.Status,
	)
	if err != nil {
		slog.Error("[senderzz_labels] webhook ME: erro idempotência", "me_id", meID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao processar webhook")
		return
	}
	if evtTag.RowsAffected() == 0 {
		slog.Info("[senderzz_labels] webhook ME: evento duplicado ignorado", "me_id", meID, "status", p.Data.Status)
		httpx.WriteOK(w, map[string]any{"ok": true, "duplicate": true})
		return
	}

	trackingCode := firstNonEmpty(strings.TrimSpace(p.Data.Tracking), strings.TrimSpace(p.Data.SelfTracking))
	var wcOrderID int64
	res, err := tx.Exec(r.Context(),
		`UPDATE wc_me_labels
		    SET status = $1,
		        tracking_code = COALESCE(NULLIF($3, ''), tracking_code),
		        updated_at = NOW()
		  WHERE me_shipment_id = $2
		    AND status NOT IN ('canceled', 'delivered')`,
		internalStatus, meID, trackingCode,
	)
	if err != nil {
		slog.Error("[senderzz_labels] webhook ME: erro ao atualizar etiqueta", "me_id", meID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao processar webhook")
		return
	}
	updated := res.RowsAffected()
	var historyOrderID int64
	_ = tx.QueryRow(r.Context(),
		`SELECT wc_order_id FROM wc_me_labels WHERE me_shipment_id = $1`, meID,
	).Scan(&historyOrderID)
	if err := trackinghistory.Save(r.Context(), tx, trackinghistory.Snapshot{
		ShipmentID: meID, OrderID: historyOrderID, Status: p.Data.Status,
		Tracking: trackingCode,
	}); err != nil {
		slog.Warn("[senderzz_labels] webhook ME: falha ao gravar histórico", "me_id", meID, "err", err)
	}

	// AUDIT-2026-07-30 (dono): status do PEDIDO precisa acompanhar o rastreio ME
	// automaticamente (antes só wc_me_labels mudava, sz_orders ficava parado e
	// o produtor nunca recebia webhook de progresso). syncOrderStatusFromME só
	// avança o pipeline, nunca regride nem mexe em cancelado/reembolsado.
	var orderSynced bool
	var orderPrevStatus string
	if updated > 0 {
		// AUDIT-2026-07-30: só sincroniza sz_orders se este shipment for a
		// etiqueta ATIVA (mais recente) do pedido — emit.go cancela candidatos
		// descartados ao tentar a próxima transportadora no fallback, e um
		// evento "canceled" tardio desses NÃO pode marcar o pedido como
		// frustrado (pedido 1633: 3 canceled de retry, entrega seguiu normal).
		var isActive bool
		err := tx.QueryRow(r.Context(),
			`SELECT wc_order_id, (
			    SELECT l2.me_shipment_id FROM wc_me_labels l2
			     WHERE l2.wc_order_id = wc_me_labels.wc_order_id
			       AND l2.status <> 'canceled'
			     ORDER BY l2.id DESC LIMIT 1
			 ) = $1
			   FROM wc_me_labels WHERE me_shipment_id = $1`, meID,
		).Scan(&wcOrderID, &isActive)
		if err == nil && wcOrderID > 0 && isActive {
			orderSynced, orderPrevStatus, err = syncOrderStatusFromME(r.Context(), tx, wcOrderID, p.Data.Status)
			if err != nil {
				slog.Error("[senderzz_labels] webhook ME: erro ao sincronizar sz_orders.status", "me_id", meID, "wc_order_id", wcOrderID, "err", err)
				httpx.WriteErr(w, http.StatusInternalServerError, "erro ao processar webhook")
				return
			}
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao processar webhook")
		return
	}

	slog.Info("[senderzz_labels] webhook ME processado",
		"event", p.Event, "me_id", meID, "me_status", p.Data.Status,
		"interno", internalStatus, "linhas", updated,
		"wc_order_id", wcOrderID, "order_synced", orderSynced, "order_prev_status", orderPrevStatus)

	httpx.WriteOK(w, map[string]any{
		"ok": true, "me_id": meID, "status": internalStatus, "updated": updated,
		"wc_order_id": wcOrderID, "order_synced": orderSynced,
	})
}

// validateMEHMAC valida x-me-signature = base64(HMAC-SHA256(body, secret)).
// É o formato do Melhor Envio (base64, sem prefixo) — distinto do /webhook/tracking
// (que usa "sha256={hex}"). Comparação em tempo constante.
func validateMEHMAC(body []byte, sig, secret string) bool {
	if strings.TrimSpace(sig) == "" || secret == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(sig))
}

// toStr coage o id do payload ME (pode vir string, número ou UUID) para string.
func toStr(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	case float64:
		// ME pode mandar id numérico; sem casas decimais.
		return strconv.FormatInt(int64(t), 10)
	case json.Number:
		return t.String()
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", t))
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
