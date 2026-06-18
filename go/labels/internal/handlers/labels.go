// Package handlers implementa os handlers HTTP do labels-service.
//
// Rotas implementadas (sob /wp-json/wc-melhor-envio/v1):
//   GET    /labels          — lista etiquetas de um pedido (?order_id=N) ou com filtros
//   POST   /labels          — cria etiqueta: CRIT-01 recalcula preço via ME.Calculate
//   GET    /labels/{id}     — detalhes + status de rastreamento atual
//   DELETE /labels/{id}     — cancela etiqueta (ME + banco)
//   GET    /calculate       — proxy ME.Calculate com cache de 10 min
//
// DIVERGÊNCIA EM RELAÇÃO AO openapi-wc-melhor-envio-v1.yaml:
//   O OpenAPI usa /labels/{order_id} (por order_id) e /labels/{order_id}/process.
//   Este serviço usa /labels/{id} (por label_id numérico do banco) e
//   POST /labels com order_id no corpo, conforme especificado na task da Fase 4.
//   A razão é que múltiplas etiquetas podem existir para o mesmo pedido.
//   Ver openapi-wc-melhor-envio-v1.yaml para a interface PHP legada.
//
// CRIT-01: O preço da etiqueta é SEMPRE recalculado server-side via ME.Calculate.
// O handler rejeita qualquer campo "price" enviado pelo cliente.
//
// Autenticação:
//   Todas as rotas requerem JWT (middleware.AuthJWT).
//   GET /calculate — também protegido por JWT (checkouts autenticados via portal session).
//
// Integração com carteira (wallet-service):
//   FASE 4 — TODO (Fase 5): reservar saldo antes de CreateShipment e debitar na
//   confirmação. Por ora, o handler cria a etiqueta sem débito automático.
//   Ver handler PostLabel — marcado com // TODO CRIT-01 wallet reservation.
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
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/labels-service/internal/httpx"
	"github.com/senderzz/labels-service/internal/jobs"
	"github.com/senderzz/labels-service/internal/me"
	"github.com/shopspring/decimal"
)

// LabelHandler agrupa as dependências dos handlers de etiquetas.
type LabelHandler struct {
	db    *pgxpool.Pool
	me    *me.MEClient
	queue *asynq.Client
}

// NewLabelHandler cria um LabelHandler com as dependências injetadas.
func NewLabelHandler(db *pgxpool.Pool, meClient *me.MEClient, queue *asynq.Client) *LabelHandler {
	return &LabelHandler{
		db:    db,
		me:    meClient,
		queue: queue,
	}
}

// ─── Tipos de request/response ────────────────────────────────────────────────

// labelRow representa uma linha de wc_me_labels para serialização JSON.
type labelRow struct {
	ID            int64           `json:"id"`
	WCOrderID     int64           `json:"wc_order_id"`
	MEShipmentID  *string         `json:"me_shipment_id,omitempty"`
	MELabelID     *string         `json:"me_label_id,omitempty"`
	Status        string          `json:"status"`
	ServiceID     int             `json:"service_id"`
	ServiceName   *string         `json:"service_name,omitempty"`
	Price         *decimal.Decimal `json:"price,omitempty"`
	TrackingCode  *string         `json:"tracking_code,omitempty"`
	LabelURL      *string         `json:"label_url,omitempty"`
	LabelPDFPath  *string         `json:"label_pdf_path,omitempty"`
	FromCEP       *string         `json:"from_cep,omitempty"`
	ToCEP         *string         `json:"to_cep,omitempty"`
	WeightG       *int            `json:"weight_g,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// createLabelRequest é o corpo esperado no POST /labels.
// CRIT-01: campo "price" é ignorado se presente — preço é sempre recalculado.
type createLabelRequest struct {
	WCOrderID int   `json:"wc_order_id"`
	ServiceID int   `json:"service_id"`
	// Dados necessários para recalcular via ME.Calculate (CRIT-01).
	FromCEP   string             `json:"from_cep"`
	ToCEP     string             `json:"to_cep"`
	Products  []me.CalcProduct   `json:"products"`
	// Endereços para CreateShipment.
	From      me.MEAddress       `json:"from_address"`
	To        me.MEAddress       `json:"to_address"`
	// Volumes.
	Volumes   []me.MEVolume      `json:"volumes"`
	// Opções adicionais (seguro, coleta, AR, mãos próprias).
	Options   me.MEOrderOptions  `json:"options"`
}

// ─── GET /labels ─────────────────────────────────────────────────────────────

// GetLabels lista etiquetas com filtros opcionais via query params:
//   ?order_id=N    — filtra por wc_order_id
//   ?status=draft  — filtra por status
//   ?limit=50      — quantidade máxima (default 50, máximo 100)
func (h *LabelHandler) GetLabels(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	query := `SELECT id, wc_order_id, me_shipment_id, me_label_id, status,
	                 service_id, service_name, price, tracking_code,
	                 label_url, label_pdf_path, from_cep, to_cep, weight_g,
	                 created_at, updated_at
	          FROM wc_me_labels
	          WHERE 1=1`
	args := []any{}
	argIdx := 1

	if orderIDStr := r.URL.Query().Get("order_id"); orderIDStr != "" {
		orderID, err := strconv.ParseInt(orderIDStr, 10, 64)
		if err != nil || orderID <= 0 {
			httpx.WriteErr(w, http.StatusBadRequest, "order_id inválido")
			return
		}
		query += fmt.Sprintf(" AND wc_order_id = $%d", argIdx)
		args = append(args, orderID)
		argIdx++
	}

	if status := r.URL.Query().Get("status"); status != "" {
		query += fmt.Sprintf(" AND status = $%d", argIdx)
		args = append(args, status)
		argIdx++
	}

	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := h.db.Query(r.Context(), query, args...)
	if err != nil {
		slog.Error("[senderzz_labels] GetLabels: erro ao consultar banco", "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao consultar etiquetas")
		return
	}
	defer rows.Close()

	var labels []labelRow
	for rows.Next() {
		lbl, err := scanLabelRow(rows)
		if err != nil {
			slog.Error("[senderzz_labels] GetLabels: erro ao ler linha", "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao ler etiquetas")
			return
		}
		labels = append(labels, lbl)
	}
	if err := rows.Err(); err != nil {
		slog.Error("[senderzz_labels] GetLabels: erro pós-iteração", "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao processar etiquetas")
		return
	}

	if labels == nil {
		labels = []labelRow{}
	}

	httpx.WriteOK(w, map[string]any{
		"data":  labels,
		"total": len(labels),
	})
}

// ─── POST /labels ────────────────────────────────────────────────────────────

// PostLabel cria uma nova etiqueta de envio.
//
// Fluxo:
//  1. Valida corpo da requisição.
//  2. CRIT-01: chama ME.Calculate para obter o preço real do serviço solicitado.
//     Rejeita se o service_id solicitado não está disponível.
//  3. Chama ME.CreateShipment para criar o pedido no carrinho ME.
//  4. INSERT em wc_me_labels com status=draft inicialmente.
//  5. Enfileira job Asynq TypeGeneratePDF.
//  6. Atualiza status para released após GenerateLabel (ou aguarda o job).
//
// TODO (Fase 5): reservar saldo via wallet-service antes do CreateShipment.
// Padrão: tpc_reservar → ME.CreateShipment → (sucesso) tpc_debitar_reserva
//                                           → (falha)   tpc_liberar_reserva
// Por ora, etiqueta é criada sem débito automático da carteira.
func (h *LabelHandler) PostLabel(w http.ResponseWriter, r *http.Request) {
	var req createLabelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}

	if req.WCOrderID <= 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "wc_order_id é obrigatório e deve ser maior que zero")
		return
	}
	if req.ServiceID <= 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "service_id é obrigatório e deve ser maior que zero")
		return
	}
	if req.FromCEP == "" || req.ToCEP == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "from_cep e to_cep são obrigatórios")
		return
	}
	if len(req.Products) == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "products não pode ser vazio")
		return
	}

	// ── CRIT-01: Recalcula preço server-side ─────────────────────────────────
	// Nunca usar preço enviado pelo cliente. Chamar ME.Calculate e encontrar
	// o service_id solicitado na lista de opções.
	calcReq := me.CalcRequest{
		FromCEP:  req.FromCEP,
		ToCEP:    req.ToCEP,
		Products: req.Products,
	}

	options, err := h.me.Calculate(r.Context(), calcReq)
	if err != nil {
		slog.Error("[senderzz_labels] PostLabel: falha ao calcular frete (CRIT-01)",
			"order_id", req.WCOrderID,
			"service_id", req.ServiceID,
			"err", err,
		)
		httpx.WriteErr(w, http.StatusBadGateway, "falha ao calcular frete via Melhor Envio")
		return
	}

	// Localiza o serviço solicitado no resultado do cálculo.
	var selectedOption *me.ServiceOption
	for i := range options {
		if options[i].ServiceID == req.ServiceID {
			selectedOption = &options[i]
			break
		}
	}
	if selectedOption == nil {
		slog.Warn("[senderzz_labels] PostLabel: service_id não disponível para este trecho",
			"order_id", req.WCOrderID,
			"service_id", req.ServiceID,
			"from_cep", req.FromCEP,
			"to_cep", req.ToCEP,
		)
		httpx.WriteErr(w, http.StatusUnprocessableEntity,
			fmt.Sprintf("serviço %d não disponível para o CEP de destino", req.ServiceID))
		return
	}

	// Preço recalculado (CRIT-01) — ignoramos completamente qualquer campo price do req.
	serverPrice := selectedOption.Price

	// ── CreateShipment ───────────────────────────────────────────────────────
	// Garante que insurance_value seja o price calculado (não o enviado pelo cliente).
	req.Options.InsuranceValue = serverPrice

	meOrder := me.MEOrder{
		ServiceID: req.ServiceID,
		From:      req.From,
		To:        req.To,
		Products:  buildMEProducts(req.Products),
		Volumes:   req.Volumes,
		Options:   req.Options,
	}

	shipmentID, err := h.me.CreateShipment(r.Context(), meOrder)
	if err != nil {
		slog.Error("[senderzz_labels] PostLabel: falha ao criar pedido no ME",
			"order_id", req.WCOrderID,
			"service_id", req.ServiceID,
			"err", err,
		)
		httpx.WriteErr(w, http.StatusBadGateway, "falha ao criar pedido no Melhor Envio")
		return
	}

	// ── INSERT em wc_me_labels ───────────────────────────────────────────────
	var labelID int64
	err = h.db.QueryRow(r.Context(),
		`INSERT INTO wc_me_labels
		     (wc_order_id, me_shipment_id, status, service_id, service_name,
		      price, from_cep, to_cep, created_at, updated_at)
		 VALUES ($1, $2, 'draft', $3, $4, $5, $6, $7, NOW(), NOW())
		 RETURNING id`,
		req.WCOrderID,
		shipmentID,
		req.ServiceID,
		selectedOption.Name,
		serverPrice.StringFixed(2), // CRIT-01: sempre o preço calculado
		req.FromCEP,
		req.ToCEP,
	).Scan(&labelID)
	if err != nil {
		slog.Error("[senderzz_labels] PostLabel: falha ao inserir etiqueta",
			"order_id", req.WCOrderID,
			"shipment_id", shipmentID,
			"err", err,
		)
		// Tenta cancelar o shipment na ME para não deixar item órfão no carrinho.
		cancelCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if cancelErr := h.me.CancelShipment(cancelCtx, shipmentID); cancelErr != nil {
			slog.Warn("[senderzz_labels] PostLabel: falha ao cancelar shipment orphan na ME",
				"shipment_id", shipmentID, "err", cancelErr)
		}
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao registrar etiqueta")
		return
	}

	// ── Registra job durável em wc_me_queue ─────────────────────────────────
	if _, qErr := h.db.Exec(r.Context(),
		`INSERT INTO wc_me_queue (label_id, action, scheduled_at, created_at)
		 VALUES ($1, 'generate_pdf', NOW(), NOW())`,
		labelID,
	); qErr != nil {
		slog.Warn("[senderzz_labels] PostLabel: falha ao registrar job na fila durável",
			"label_id", labelID, "err", qErr)
	}

	// ── Enfileira job Asynq para geração assíncrona do PDF ───────────────────
	if err := jobs.EnqueueGeneratePDF(h.queue, labelID, shipmentID); err != nil {
		// Falha no enqueue não cancela a criação — job durável em wc_me_queue
		// serve como fallback para reprocessamento manual.
		slog.Warn("[senderzz_labels] PostLabel: falha ao enfileirar GeneratePDF (job durável criado)",
			"label_id", labelID,
			"shipment_id", shipmentID,
			"err", err,
		)
	}

	slog.Info("[senderzz_labels] etiqueta criada",
		"label_id", labelID,
		"order_id", req.WCOrderID,
		"shipment_id", shipmentID,
		"service_id", req.ServiceID,
		"price", serverPrice.StringFixed(2),
	)

	httpx.WriteOK(w, map[string]any{
		"label_id":    labelID,
		"shipment_id": shipmentID,
		"status":      "draft",
		"price":       serverPrice.StringFixed(2), // CRIT-01: preço server-side
	})
}

// ─── GET /labels/{id} ────────────────────────────────────────────────────────

// GetLabel retorna os detalhes de uma etiqueta pelo seu ID interno (label_id).
// Se a etiqueta tiver tracking_code, também busca o status atual na ME API.
func (h *LabelHandler) GetLabel(w http.ResponseWriter, r *http.Request) {
	labelID, err := parseLabelID(r)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}

	row := h.db.QueryRow(r.Context(),
		`SELECT id, wc_order_id, me_shipment_id, me_label_id, status,
		        service_id, service_name, price, tracking_code,
		        label_url, label_pdf_path, from_cep, to_cep, weight_g,
		        created_at, updated_at
		 FROM wc_me_labels
		 WHERE id = $1`,
		labelID,
	)

	lbl, err := scanLabelRow(row)
	if err == pgx.ErrNoRows {
		httpx.WriteErr(w, http.StatusNotFound, "etiqueta não encontrada")
		return
	}
	if err != nil {
		slog.Error("[senderzz_labels] GetLabel: erro ao consultar banco",
			"label_id", labelID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao consultar etiqueta")
		return
	}

	// Se tiver tracking_code, busca status atual na ME (read-only, não atualiza banco aqui).
	var trackingStatus string
	if lbl.TrackingCode != nil && *lbl.TrackingCode != "" {
		ts, err := h.me.TrackShipment(r.Context(), *lbl.TrackingCode)
		if err != nil {
			// Falha de rastreamento não bloqueia — retorna etiqueta com status do banco.
			slog.Warn("[senderzz_labels] GetLabel: falha ao rastrear",
				"label_id", labelID,
				"tracking_code", *lbl.TrackingCode,
				"err", err,
			)
		} else {
			trackingStatus = ts
		}
	}

	result := map[string]any{
		"label": lbl,
	}
	if trackingStatus != "" {
		result["tracking_status_me"] = trackingStatus
	}

	httpx.WriteOK(w, result)
}

// ─── DELETE /labels/{id} ─────────────────────────────────────────────────────

// DeleteLabel cancela uma etiqueta: remove do carrinho ME e atualiza status=canceled.
// Etiquetas já em status=posted, delivered ou lost não podem ser canceladas.
func (h *LabelHandler) DeleteLabel(w http.ResponseWriter, r *http.Request) {
	labelID, err := parseLabelID(r)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}

	// Busca shipment_id e status atuais.
	var shipmentID *string
	var status string
	err = h.db.QueryRow(r.Context(),
		`SELECT me_shipment_id, status FROM wc_me_labels WHERE id = $1`,
		labelID,
	).Scan(&shipmentID, &status)

	if err == pgx.ErrNoRows {
		httpx.WriteErr(w, http.StatusNotFound, "etiqueta não encontrada")
		return
	}
	if err != nil {
		slog.Error("[senderzz_labels] DeleteLabel: erro ao consultar banco",
			"label_id", labelID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Status que não podem ser cancelados.
	nonCancelable := map[string]bool{
		"posted":    true,
		"delivered": true,
		"lost":      true,
		"canceled":  true,
	}
	if nonCancelable[status] {
		httpx.WriteErr(w, http.StatusConflict,
			fmt.Sprintf("etiqueta com status '%s' não pode ser cancelada", status))
		return
	}

	// Cancela no ME se tiver shipment_id.
	if shipmentID != nil && *shipmentID != "" {
		if err := h.me.CancelShipment(r.Context(), *shipmentID); err != nil {
			slog.Error("[senderzz_labels] DeleteLabel: falha ao cancelar na ME",
				"label_id", labelID,
				"shipment_id", *shipmentID,
				"err", err,
			)
			httpx.WriteErr(w, http.StatusBadGateway, "falha ao cancelar pedido no Melhor Envio")
			return
		}
	}

	// Atualiza status no banco.
	_, err = h.db.Exec(r.Context(),
		`UPDATE wc_me_labels
		    SET status = 'canceled', updated_at = NOW()
		  WHERE id = $1`,
		labelID,
	)
	if err != nil {
		slog.Error("[senderzz_labels] DeleteLabel: erro ao atualizar status",
			"label_id", labelID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao cancelar etiqueta")
		return
	}

	slog.Info("[senderzz_labels] etiqueta cancelada",
		"label_id", labelID,
		"shipment_id", shipmentID,
	)

	httpx.WriteOK(w, map[string]any{
		"label_id": labelID,
		"status":   "canceled",
	})
}

// ─── GET /calculate ─────────────────────────────────────────────────────────

// GetCalculate é um proxy para ME.Calculate com cache de 10 minutos em wc_me_shipment_cache.
// Usado pelo checkout para exibir opções de frete sem chamar a ME API a cada request.
//
// CRIT-01: Este endpoint é apenas informativo (checkout). O preço real para cobrança
// é sempre recalculado em POST /labels antes de criar a etiqueta.
//
// Query params obrigatórios:
//   from_cep=XXXXXXXX
//   to_cep=XXXXXXXX
//   weight=0.5        (kg, float)
//   height=15         (cm, float)
//   width=20          (cm, float)
//   length=30         (cm, float)
//   value=150.00      (valor declarado, decimal)
func (h *LabelHandler) GetCalculate(w http.ResponseWriter, r *http.Request) {
	fromCEP := r.URL.Query().Get("from_cep")
	toCEP := r.URL.Query().Get("to_cep")
	if fromCEP == "" || toCEP == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "from_cep e to_cep são obrigatórios")
		return
	}

	// Monta um produto único a partir dos parâmetros para simplificar o cálculo no checkout.
	weight, _ := strconv.ParseFloat(r.URL.Query().Get("weight"), 64)
	height, _ := strconv.ParseFloat(r.URL.Query().Get("height"), 64)
	width, _ := strconv.ParseFloat(r.URL.Query().Get("width"), 64)
	length, _ := strconv.ParseFloat(r.URL.Query().Get("length"), 64)
	if weight <= 0 {
		weight = 0.3
	}
	if height <= 0 {
		height = 11
	}
	if width <= 0 {
		width = 15
	}
	if length <= 0 {
		length = 20
	}

	insStr := r.URL.Query().Get("value")
	insuranceValue, _ := decimal.NewFromString(insStr)
	if insuranceValue.IsZero() {
		insuranceValue = decimal.NewFromFloat(1.00)
	}

	calcReq := me.CalcRequest{
		FromCEP: fromCEP,
		ToCEP:   toCEP,
		Products: []me.CalcProduct{
			{
				Height:         height,
				Width:          width,
				Length:         length,
				Weight:         weight,
				InsuranceValue: insuranceValue,
				Quantity:       1,
			},
		},
	}

	// Verifica cache antes de chamar a ME API.
	cacheKey := me.CalculateCacheKey(calcReq)
	if cached, ok := h.getFromCache(r.Context(), cacheKey); ok {
		slog.Info("[senderzz_labels] GetCalculate: cache hit", "cache_key", cacheKey[:8])
		httpx.WriteOK(w, map[string]any{
			"data":    cached,
			"cached":  true,
		})
		return
	}

	options, err := h.me.Calculate(r.Context(), calcReq)
	if err != nil {
		slog.Error("[senderzz_labels] GetCalculate: falha ao calcular frete",
			"from_cep", fromCEP,
			"to_cep", toCEP,
			"err", err,
		)
		httpx.WriteErr(w, http.StatusBadGateway, "falha ao calcular frete via Melhor Envio")
		return
	}

	// Armazena em cache por 10 minutos.
	h.storeInCache(r.Context(), cacheKey, options)

	slog.Info("[senderzz_labels] GetCalculate: cotação realizada",
		"from_cep", fromCEP,
		"to_cep", toCEP,
		"opcoes", len(options),
	)

	httpx.WriteOK(w, map[string]any{
		"data":   options,
		"cached": false,
	})
}

// ─── Cache helpers ───────────────────────────────────────────────────────────

// getFromCache verifica wc_me_shipment_cache por uma entrada válida (não expirada).
// Retorna os dados deserializados e true se encontrou; (nil, false) se miss ou expirado.
func (h *LabelHandler) getFromCache(ctx context.Context, cacheKey string) ([]me.ServiceOption, bool) {
	var payloadJSON []byte
	err := h.db.QueryRow(ctx,
		`SELECT payload
		 FROM wc_me_shipment_cache
		 WHERE cache_key = $1 AND expires_at > NOW()`,
		cacheKey,
	).Scan(&payloadJSON)

	if err != nil {
		// pgx.ErrNoRows ou qualquer erro = cache miss.
		return nil, false
	}

	var opts []me.ServiceOption
	if err := json.Unmarshal(payloadJSON, &opts); err != nil {
		slog.Warn("[senderzz_labels] getFromCache: falha ao deserializar cache",
			"cache_key", cacheKey, "err", err)
		return nil, false
	}

	return opts, true
}

// storeInCache armazena o resultado de Calculate em wc_me_shipment_cache com TTL de 10 min.
// Usa UPSERT (ON CONFLICT cache_key DO UPDATE) para atualizar entradas expiradas.
func (h *LabelHandler) storeInCache(ctx context.Context, cacheKey string, opts []me.ServiceOption) {
	payloadJSON, err := json.Marshal(opts)
	if err != nil {
		slog.Warn("[senderzz_labels] storeInCache: falha ao serializar", "err", err)
		return
	}

	_, err = h.db.Exec(ctx,
		`INSERT INTO wc_me_shipment_cache (cache_key, payload, expires_at, created_at)
		 VALUES ($1, $2, NOW() + INTERVAL '10 minutes', NOW())
		 ON CONFLICT (cache_key) DO UPDATE
		     SET payload    = EXCLUDED.payload,
		         expires_at = EXCLUDED.expires_at`,
		cacheKey, payloadJSON,
	)
	if err != nil {
		slog.Warn("[senderzz_labels] storeInCache: falha ao armazenar cache",
			"cache_key", cacheKey, "err", err)
	}
}

// ─── helpers internos ─────────────────────────────────────────────────────────

// parseLabelID extrai e valida o parâmetro {id} da rota chi.
func parseLabelID(r *http.Request) (int64, error) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("label id inválido: %q", idStr)
	}
	return id, nil
}

// scanLabelRow lê uma linha de wc_me_labels a partir de qualquer pgx.Rows ou pgx.Row.
// Interface unificada: ambos expõem Scan(*args).
type rowScanner interface {
	Scan(dest ...any) error
}

func scanLabelRow(row rowScanner) (labelRow, error) {
	var lbl labelRow
	var priceStr *string

	err := row.Scan(
		&lbl.ID,
		&lbl.WCOrderID,
		&lbl.MEShipmentID,
		&lbl.MELabelID,
		&lbl.Status,
		&lbl.ServiceID,
		&lbl.ServiceName,
		&priceStr,
		&lbl.TrackingCode,
		&lbl.LabelURL,
		&lbl.LabelPDFPath,
		&lbl.FromCEP,
		&lbl.ToCEP,
		&lbl.WeightG,
		&lbl.CreatedAt,
		&lbl.UpdatedAt,
	)
	if err != nil {
		return lbl, err
	}

	if priceStr != nil {
		p, err := decimal.NewFromString(*priceStr)
		if err == nil {
			lbl.Price = &p
		}
	}

	return lbl, nil
}

// buildMEProducts converte CalcProduct em MEOrderProduct para CreateShipment.
// Usa name genérico pois CalcProduct não tem nome (só dimensões/peso/valor).
func buildMEProducts(products []me.CalcProduct) []me.MEOrderProduct {
	result := make([]me.MEOrderProduct, 0, len(products))
	for _, p := range products {
		result = append(result, me.MEOrderProduct{
			Name:         "Produto",
			Quantity:     p.Quantity,
			UnitaryValue: p.InsuranceValue,
			Weight:       p.Weight,
			Width:        p.Width,
			Height:       p.Height,
			Length:       p.Length,
		})
	}
	return result
}

// ─── POST /webhook/tracking ─────────────────────────────────────────────────

// PostTrackingWebhook recebe eventos de rastreamento do Melhor Envio ou de
// transportadoras integradas e atualiza wc_me_labels.
//
// Segurança:
//   - Valida HMAC-SHA256 do corpo com TRACKING_WEBHOOK_SECRET.
//   - Fail-closed: secret vazio → 503 (não processa sem autenticação).
//   - Payload esperado: {"tracking_code": "...", "status": "..."}
//   - Enfileira job SyncTracking após atualização para rastreamento contínuo.
func (h *LabelHandler) PostTrackingWebhook(w http.ResponseWriter, r *http.Request) {
	secret := os.Getenv("TRACKING_WEBHOOK_SECRET")
	if secret == "" {
		// Fail-closed: sem secret configurado, não processar webhooks.
		slog.Error("[senderzz_labels] PostTrackingWebhook: TRACKING_WEBHOOK_SECRET não configurado — recusando",
			"ip", r.RemoteAddr)
		httpx.WriteErr(w, http.StatusServiceUnavailable, "serviço não configurado")
		return
	}

	// Lê o corpo para validação HMAC (deve ser lido antes de decodificar).
	bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB máximo
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "erro ao ler corpo da requisição")
		return
	}

	// Valida assinatura HMAC-SHA256.
	// Header esperado: X-Webhook-Signature: sha256={hex}
	sig := r.Header.Get("X-Webhook-Signature")
	if !validateHMAC(bodyBytes, sig, secret) {
		slog.Warn("[senderzz_labels] PostTrackingWebhook: assinatura inválida",
			"ip", r.RemoteAddr,
			"sig", sig,
		)
		httpx.WriteErr(w, http.StatusUnauthorized, "assinatura inválida")
		return
	}

	var payload struct {
		TrackingCode string `json:"tracking_code"`
		Status       string `json:"status"`
	}
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "payload inválido")
		return
	}

	if payload.TrackingCode == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "tracking_code é obrigatório")
		return
	}

	// Mapeia status ME para status interno e atualiza o banco.
	internalStatus := mapTrackingStatus(payload.Status)

	result, err := h.db.Exec(r.Context(),
		`UPDATE wc_me_labels
		    SET status     = $1,
		        updated_at = NOW()
		  WHERE tracking_code = $2
		    AND status NOT IN ('canceled', 'delivered')`,
		internalStatus, payload.TrackingCode,
	)
	if err != nil {
		slog.Error("[senderzz_labels] PostTrackingWebhook: erro ao atualizar banco",
			"tracking_code", payload.TrackingCode,
			"status", payload.Status,
			"err", err,
		)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao processar webhook")
		return
	}

	updated := result.RowsAffected()
	slog.Info("[senderzz_labels] PostTrackingWebhook processado",
		"tracking_code", payload.TrackingCode,
		"me_status", payload.Status,
		"interno_status", internalStatus,
		"linhas_atualizadas", updated,
	)

	// Enfileira SyncTracking para confirmar via ME API (double-check).
	if updated > 0 && h.queue != nil {
		// Busca o label_id para enfileirar o job.
		var labelID int64
		lookupErr := h.db.QueryRow(r.Context(),
			`SELECT id FROM wc_me_labels WHERE tracking_code = $1 LIMIT 1`,
			payload.TrackingCode,
		).Scan(&labelID)
		if lookupErr == nil {
			_ = jobs.EnqueueSyncTracking(h.queue, labelID, payload.TrackingCode)
		}
	}

	httpx.WriteOK(w, map[string]any{
		"tracking_code": payload.TrackingCode,
		"status":        internalStatus,
		"updated":       updated,
	})
}

// ─── helpers internos adicionais ─────────────────────────────────────────────

// validateHMAC verifica a assinatura HMAC-SHA256 do webhook.
// Formato do header: "sha256={hex}" (mesmo padrão GitHub/ME webhooks).
func validateHMAC(body []byte, sigHeader, secret string) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(sigHeader, prefix) {
		return false
	}
	expected := sigHeader[len(prefix):]

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	actual := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(actual), []byte(expected))
}

// mapTrackingStatus mapeia status de webhook para status canônico interno.
// Duplica a lógica de jobs.mapMEStatus para uso no handler webhook.
func mapTrackingStatus(meStatus string) string {
	switch meStatus {
	case "posted", "in_transit", "out_for_delivery", "waiting_pickup":
		return "posted"
	case "delivered":
		return "delivered"
	case "canceled", "returned", "cancelled":
		return "canceled"
	case "lost":
		return "lost"
	default:
		return "posted"
	}
}

// cacheKeyFrom gera cache key SHA-256 a partir de uma string.
// Usado internamente para keys que não passam pelo MEClient.
func cacheKeyFrom(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
