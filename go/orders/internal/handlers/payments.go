// Package handlers — endpoints de pagamento do serviço Orders.
//
// Rotas implementadas neste arquivo:
//   POST /orders/{id}/pay              — inicia pagamento (COD ou PIX)
//   POST /payments/webhook/{gateway}   — confirmação de pagamento do gateway
//   POST /orders/{id}/refund           — reembolso do pedido
//
// Gateways suportados:
//   - cod    : pagamento na entrega (COD = Cash On Delivery). Transita direto para processing.
//   - pix    : emissão de PIX via serviço Wallet (stub — chama wallet-service na Fase 7).
//   - wallet : débito da carteira de frete Senderzz.
//   - cartao : (stub) integração externa de cartão.
//
// Segurança de webhook:
//   - WEBHOOK_SECRET vazio → 503 (fail-closed — não processa sem validação HMAC).
//   - Idempotência via gateway_ref em sz_order_payments (UNIQUE parcial).
//   - Status aceitos como "pago": paid, approved (whitelist explícita — equiv. CRIT-04).
//
// Valores monetários: shopspring/decimal para evitar drift de float64.
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

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/senderzz/orders-service/internal/auth"
	"github.com/senderzz/orders-service/internal/httpx"
	"github.com/senderzz/orders-service/internal/statemachine"
)

// PaymentHandler agrupa dependências dos handlers de pagamento.
type PaymentHandler struct {
	db *pgxpool.Pool
}

// NewPaymentHandler cria um PaymentHandler com o pool fornecido.
func NewPaymentHandler(db *pgxpool.Pool) *PaymentHandler {
	return &PaymentHandler{db: db}
}

// ── POST /orders/{id}/pay ─────────────────────────────────────────────────────

// PostOrderPay inicia o pagamento de um pedido.
//
// Body: {gateway string, gateway_ref? string}
//
// Fluxo por gateway:
//   - cod    → INSERT sz_order_payments + Transition(pending→processing)
//   - pix    → INSERT sz_order_payments (status=pending) + stub de emissão PIX
//              (Fase 7: chamada ao wallet-service /recarregar)
//   - wallet → INSERT sz_order_payments + debita carteira (stub Fase 7)
//   - outros → INSERT sz_order_payments (status=pending) + instrução ao cliente
//
// O total do pagamento é sempre lido de sz_orders.total — nunca do cliente.
func (h *PaymentHandler) PostOrderPay(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r.Context())
	if userID == 0 {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	orderIDStr := chi.URLParam(r, "id")
	var orderID int64
	if _, err := fmt.Sscanf(orderIDStr, "%d", &orderID); err != nil || orderID == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "id de pedido inválido")
		return
	}

	var req struct {
		Gateway    string `json:"gateway"`
		GatewayRef string `json:"gateway_ref"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	if req.Gateway == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "gateway é obrigatório")
		return
	}

	// Gateways permitidos — whitelist explícita.
	allowedGateways := map[string]bool{"cod": true, "pix": true, "wallet": true, "cartao": true}
	if !allowedGateways[req.Gateway] {
		httpx.WriteErr(w, http.StatusBadRequest, "gateway inválido")
		return
	}

	ctx := r.Context()

	// Busca o pedido e valida acesso.
	var currentStatus string
	var orderTotal decimal.Decimal
	var ownerUserID, ownerProdutorID int64
	var totStr string
	err := h.db.QueryRow(ctx,
		`SELECT status, total, user_id, produtor_id FROM sz_orders WHERE id = $1`,
		orderID,
	).Scan(&currentStatus, &totStr, &ownerUserID, &ownerProdutorID)
	if err == pgx.ErrNoRows {
		httpx.WriteErr(w, http.StatusNotFound, "pedido não encontrado")
		return
	}
	if err != nil {
		slog.Error("[payments] falha ao buscar pedido", "order_id", orderID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	isAdmin := auth.IsAdmin(ctx)
	if !isAdmin && ownerUserID != userID && ownerProdutorID != userID {
		httpx.WriteErr(w, http.StatusForbidden, "acesso negado")
		return
	}

	// Pedido deve estar em pending para iniciar pagamento.
	if currentStatus != "pending" {
		httpx.WriteErr(w, http.StatusConflict,
			fmt.Sprintf("pedido no status %q não aceita novo pagamento", currentStatus))
		return
	}

	orderTotal, _ = decimal.NewFromString(totStr)

	// Insere registro de pagamento.
	var paymentID int64
	var gatewayRefPtr *string
	if req.GatewayRef != "" {
		gatewayRefPtr = &req.GatewayRef
	}

	err = h.db.QueryRow(ctx,
		`INSERT INTO sz_order_payments (order_id, gateway, gateway_ref, valor, status, created_at)
		 VALUES ($1, $2, $3, $4, 'pending', NOW())
		 RETURNING id`,
		orderID, req.Gateway, gatewayRefPtr, orderTotal.StringFixed(2),
	).Scan(&paymentID)
	if err != nil {
		slog.Error("[payments] falha ao criar pagamento", "order_id", orderID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao criar pagamento")
		return
	}

	slog.Info("[payments] pagamento iniciado",
		"order_id", orderID,
		"payment_id", paymentID,
		"gateway", req.Gateway,
		"valor", orderTotal.StringFixed(2),
	)

	switch req.Gateway {
	case "cod":
		// COD: pagamento confirmado na entrega — transita imediatamente para processing.
		if err := statemachine.Transition(ctx, h.db, orderID, "processing", userID, "sistema",
			"pagamento COD registrado"); err != nil {
			slog.Error("[payments] falha ao transitar COD para processing",
				"order_id", orderID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao processar pagamento COD")
			return
		}
		// Marca pagamento como aguardando (será confirmado na entrega).
		_, _ = h.db.Exec(ctx,
			`UPDATE sz_order_payments SET status = 'pending' WHERE id = $1`, paymentID)

		httpx.WriteOK(w, map[string]any{
			"payment_id": paymentID,
			"gateway":    "cod",
			"status":     "pending",
			"instrucao":  "pagamento será coletado na entrega pelo motoboy",
		})

	case "pix":
		// PIX: emite cobrança PIX via wallet-service (stub Fase 7).
		// TODO Fase 7 — chamar wallet-service POST /recarregar com valor e referência.
		pixInfo := emitirPixStub(ctx, orderID, paymentID, orderTotal)

		httpx.WriteOK(w, map[string]any{
			"payment_id":  paymentID,
			"gateway":     "pix",
			"status":      "pending",
			"pix_qr_code": pixInfo["qr_code"],
			"pix_copia_e_cola": pixInfo["copia_e_cola"],
			"expira_em":   pixInfo["expira_em"],
			"instrucao":   "escaneie o QR code ou copie o código PIX para pagar",
		})

	case "wallet":
		// Wallet: débita carteira de frete (stub Fase 7).
		// TODO Fase 7 — chamar wallet-service POST /carteira/reservar + /carteira/debitar-reserva.
		slog.Info("[payments] débito de carteira solicitado (stub Fase 7)",
			"order_id", orderID, "valor", orderTotal.StringFixed(2))

		httpx.WriteOK(w, map[string]any{
			"payment_id": paymentID,
			"gateway":    "wallet",
			"status":     "pending",
			"instrucao":  "débito de carteira pendente (Fase 7)",
		})

	default:
		// Outros gateways: retorna instrução genérica.
		httpx.WriteOK(w, map[string]any{
			"payment_id": paymentID,
			"gateway":    req.Gateway,
			"status":     "pending",
		})
	}
}

// ── POST /payments/webhook/{gateway} ──────────────────────────────────────────

// PostPaymentWebhook recebe confirmação de pagamento do gateway externo.
//
// Validação HMAC-SHA256 contra WEBHOOK_SECRET (fail-closed: secret vazio → 503).
// Status aceitos como "pago": paid, approved (whitelist — equiv. CRIT-04).
// Idempotência via gateway_ref em sz_order_payments.
//
// Payload esperado (flexível — adapta-se ao gateway):
//
//	{order_id?, payment_id?, gateway_ref?, status?, amount?}
func (h *PaymentHandler) PostPaymentWebhook(w http.ResponseWriter, r *http.Request) {
	webhookSecret := os.Getenv("WEBHOOK_SECRET")
	if webhookSecret == "" {
		// Fail-closed: sem secret configurado, não processa nenhum webhook.
		slog.Error("[payments/webhook] WEBHOOK_SECRET não configurado — rejeitando webhook",
			"gateway", chi.URLParam(r, "gateway"),
			"ip", r.RemoteAddr,
		)
		httpx.WriteErr(w, http.StatusServiceUnavailable, "serviço temporariamente indisponível")
		return
	}

	gateway := chi.URLParam(r, "gateway")
	if gateway == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "gateway não especificado")
		return
	}

	// Lê o corpo completo para validação HMAC antes de parsear.
	rawBody, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // máx 1 MB
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "erro ao ler corpo")
		return
	}

	// Valida assinatura HMAC-SHA256.
	sig := r.Header.Get("X-Signature")
	if sig == "" {
		sig = r.Header.Get("X-Hub-Signature")
	}
	expected := "sha256=" + hmacSHA256HexPayment(rawBody, webhookSecret)
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		slog.Warn("[payments/webhook] assinatura inválida",
			"gateway", gateway,
			"ip", r.RemoteAddr,
		)
		httpx.WriteErr(w, http.StatusUnauthorized, "assinatura inválida")
		return
	}

	// Parseia o payload.
	var payload map[string]any
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	// Extrai campos do payload (aceita múltiplos formatos de gateway).
	statusRaw := extractStr(payload, "status", "payment.status")
	gatewayRef := extractStr(payload, "id", "payment_id", "transaction_id", "payment.id")
	orderIDRaw := extractInt64(payload, "order_id", "metadata.order_id")

	// Whitelist de status de pagamento aprovado — NUNCA confiar em string genérica.
	approvedStatuses := map[string]bool{"paid": true, "approved": true}
	if !approvedStatuses[statusRaw] {
		slog.Info("[payments/webhook] status não aprovado — ignorando",
			"gateway", gateway, "status", statusRaw, "gateway_ref", gatewayRef)
		// Retorna 200 para o gateway não retentar (status legítimo mas não acionável).
		httpx.WriteOK(w, map[string]any{"ok": true, "acao": "ignorado", "status": statusRaw})
		return
	}

	if gatewayRef == "" && orderIDRaw == 0 {
		slog.Warn("[payments/webhook] payload sem referência identificável",
			"gateway", gateway, "payload", string(rawBody))
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "payload sem referência de pagamento")
		return
	}

	ctx := r.Context()

	// Busca o pagamento pelo gateway_ref ou order_id.
	var paymentID, linkedOrderID int64
	var currentPayStatus string
	var lookupErr error

	if gatewayRef != "" {
		lookupErr = h.db.QueryRow(ctx,
			`SELECT id, order_id, status FROM sz_order_payments
			  WHERE gateway = $1 AND gateway_ref = $2
			  ORDER BY id DESC LIMIT 1`,
			gateway, gatewayRef,
		).Scan(&paymentID, &linkedOrderID, &currentPayStatus)
	} else {
		lookupErr = h.db.QueryRow(ctx,
			`SELECT id, order_id, status FROM sz_order_payments
			  WHERE order_id = $1 AND gateway = $2
			  ORDER BY id DESC LIMIT 1`,
			orderIDRaw, gateway,
		).Scan(&paymentID, &linkedOrderID, &currentPayStatus)
	}

	if lookupErr == pgx.ErrNoRows {
		slog.Warn("[payments/webhook] pagamento não encontrado para referência",
			"gateway", gateway, "gateway_ref", gatewayRef, "order_id", orderIDRaw)
		httpx.WriteErr(w, http.StatusNotFound, "pagamento não encontrado")
		return
	}
	if lookupErr != nil {
		slog.Error("[payments/webhook] erro ao buscar pagamento", "err", lookupErr)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Idempotência: já confirmado → retorna ok sem reprocessar.
	if currentPayStatus == "paid" {
		slog.Info("[payments/webhook] pagamento já confirmado (idempotente)",
			"payment_id", paymentID, "order_id", linkedOrderID)
		httpx.WriteOK(w, map[string]any{"ok": true, "idempotente": true, "payment_id": paymentID})
		return
	}

	// Confirma o pagamento.
	_, err = h.db.Exec(ctx,
		`UPDATE sz_order_payments
		    SET status = 'paid', paid_at = NOW(), gateway_ref = COALESCE($1, gateway_ref)
		  WHERE id = $2`,
		nullableStrPayment(gatewayRef), paymentID,
	)
	if err != nil {
		slog.Error("[payments/webhook] falha ao confirmar pagamento",
			"payment_id", paymentID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao confirmar pagamento")
		return
	}

	// Atualiza payment_status do pedido.
	_, err = h.db.Exec(ctx,
		`UPDATE sz_orders SET payment_status = 'paid', updated_at = NOW() WHERE id = $1`,
		linkedOrderID,
	)
	if err != nil {
		slog.Error("[payments/webhook] falha ao atualizar payment_status do pedido",
			"order_id", linkedOrderID, "err", err)
		// Continua — a transição de status ainda deve ocorrer.
	}

	// Transita o pedido para processing (se ainda estiver em pending).
	var currentOrderStatus string
	_ = h.db.QueryRow(ctx, `SELECT status FROM sz_orders WHERE id = $1`, linkedOrderID).
		Scan(&currentOrderStatus)

	if statemachine.CanTransition(currentOrderStatus, "processing") {
		if err := statemachine.Transition(ctx, h.db, linkedOrderID, "processing", 0, "webhook",
			fmt.Sprintf("pagamento confirmado via webhook %s", gateway)); err != nil {
			slog.Error("[payments/webhook] falha ao transitar para processing",
				"order_id", linkedOrderID, "err", err)
			// Não retorna erro — o pagamento já foi confirmado. Log suficiente.
		}
	}

	slog.Info("[payments/webhook] pagamento confirmado",
		"payment_id", paymentID,
		"order_id", linkedOrderID,
		"gateway", gateway,
		"gateway_ref", gatewayRef,
	)

	httpx.WriteOK(w, map[string]any{
		"payment_id": paymentID,
		"order_id":   linkedOrderID,
		"status":     "paid",
	})
}

// ── POST /orders/{id}/refund ──────────────────────────────────────────────────

// PostOrderRefund marca o pedido como reembolsado e dispara o processo de reembolso.
//
// Apenas admins podem reembolsar. O gateway de reembolso é um stub na Fase 6.
// TODO Fase 7 — integrar com gateway real para estorno.
func (h *PaymentHandler) PostOrderRefund(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r.Context())
	if userID == 0 {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	if !auth.IsAdmin(r.Context()) {
		httpx.WriteErr(w, http.StatusForbidden, "apenas administradores podem solicitar reembolso")
		return
	}

	orderIDStr := chi.URLParam(r, "id")
	var orderID int64
	if _, err := fmt.Sscanf(orderIDStr, "%d", &orderID); err != nil || orderID == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "id de pedido inválido")
		return
	}

	var req struct {
		Motivo string `json:"motivo"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	ctx := r.Context()

	// Busca status atual do pedido.
	var currentStatus string
	err := h.db.QueryRow(ctx,
		`SELECT status FROM sz_orders WHERE id = $1`, orderID,
	).Scan(&currentStatus)
	if err == pgx.ErrNoRows {
		httpx.WriteErr(w, http.StatusNotFound, "pedido não encontrado")
		return
	}
	if err != nil {
		slog.Error("[payments] falha ao buscar pedido para reembolso", "order_id", orderID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	if !statemachine.CanTransition(currentStatus, "reembolsado") {
		httpx.WriteErr(w, http.StatusUnprocessableEntity,
			fmt.Sprintf("pedido no status %q não pode ser reembolsado", currentStatus))
		return
	}

	motivo := req.Motivo
	if motivo == "" {
		motivo = "reembolso solicitado pelo admin"
	}

	if err := statemachine.Transition(ctx, h.db, orderID, "reembolsado", userID, "admin", motivo); err != nil {
		slog.Error("[payments] falha ao transitar para reembolsado",
			"order_id", orderID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao processar reembolso")
		return
	}

	// Atualiza payment_status do pedido.
	_, _ = h.db.Exec(ctx,
		`UPDATE sz_orders SET payment_status = 'refunded', updated_at = NOW() WHERE id = $1`,
		orderID,
	)

	// Marca todos os pagamentos confirmados como refunded.
	_, _ = h.db.Exec(ctx,
		`UPDATE sz_order_payments SET status = 'refunded' WHERE order_id = $1 AND status = 'paid'`,
		orderID,
	)

	// Stub de estorno no gateway externo.
	// TODO Fase 7 — chamar gateway de estorno conforme payment_method do pedido.
	slog.Info("[payments] reembolso solicitado (stub gateway — implementar Fase 7)",
		"order_id", orderID, "user_id", userID, "motivo", motivo)

	httpx.WriteOK(w, map[string]any{
		"order_id": orderID,
		"status":   "reembolsado",
		"motivo":   motivo,
	})
}

// ── Stubs e helpers internos ──────────────────────────────────────────────────

// emitirPixStub é um stub de emissão de PIX para Fase 6.
// Fase 7: substituir por chamada HTTP ao wallet-service POST /recarregar.
func emitirPixStub(_ context.Context, orderID, paymentID int64, total decimal.Decimal) map[string]string {
	slog.Info("[payments] emissão PIX (stub Fase 6 — implementar Fase 7)",
		"order_id", orderID,
		"payment_id", paymentID,
		"valor", total.StringFixed(2),
	)
	return map[string]string{
		"qr_code":      "00020101...[stub — Fase 7]",
		"copia_e_cola": "00020101...[stub — Fase 7]",
		"expira_em":    "30min",
	}
}

// hmacSHA256HexPayment calcula HMAC-SHA256 do corpo do webhook.
func hmacSHA256HexPayment(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// extractStr tenta extrair uma string de um payload JSON usando caminhos alternativos.
// Suporta caminhos com ponto (ex: "payment.id").
func extractStr(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		parts := strings.SplitN(key, ".", 2)
		if len(parts) == 2 {
			if nested, ok := payload[parts[0]].(map[string]any); ok {
				if v, ok := nested[parts[1]].(string); ok && v != "" {
					return v
				}
			}
			continue
		}
		if v, ok := payload[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// extractInt64 tenta extrair um int64 de um payload JSON.
func extractInt64(payload map[string]any, keys ...string) int64 {
	for _, key := range keys {
		parts := strings.SplitN(key, ".", 2)
		if len(parts) == 2 {
			if nested, ok := payload[parts[0]].(map[string]any); ok {
				if v, ok := nested[parts[1]].(float64); ok && v > 0 {
					return int64(v)
				}
			}
			continue
		}
		if v, ok := payload[key].(float64); ok && v > 0 {
			return int64(v)
		}
	}
	return 0
}

// nullableStrPayment retorna nil se s for vazio, caso contrário retorna &s.
func nullableStrPayment(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
