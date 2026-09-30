// Package handlers implementa os handlers HTTP do labels-service.
//
// Rotas implementadas (sob /wp-json/wc-melhor-envio/v1):
//
//	GET    /labels          — lista etiquetas de um pedido (?order_id=N) ou com filtros
//	POST   /labels          — cria etiqueta: CRIT-01 recalcula preço via ME.Calculate
//	GET    /labels/{id}     — detalhes + status de rastreamento atual
//	DELETE /labels/{id}     — cancela etiqueta (ME + banco)
//	GET    /calculate       — proxy ME.Calculate com cache de 10 min
//
// DIVERGÊNCIA EM RELAÇÃO AO openapi-wc-melhor-envio-v1.yaml:
//
//	O OpenAPI usa /labels/{order_id} (por order_id) e /labels/{order_id}/process.
//	Este serviço usa /labels/{id} (por label_id numérico do banco) e
//	POST /labels com order_id no corpo, conforme especificado na task da Fase 4.
//	A razão é que múltiplas etiquetas podem existir para o mesmo pedido.
//	Ver openapi-wc-melhor-envio-v1.yaml para a interface PHP legada.
//
// CRIT-01: O preço da etiqueta é SEMPRE recalculado server-side via ME.Calculate.
// O handler rejeita qualquer campo "price" enviado pelo cliente.
//
// Autenticação:
//
//	Todas as rotas requerem JWT (middleware.AuthJWT).
//	GET /calculate — também protegido por JWT (checkouts autenticados via portal session).
//
// Integração com carteira (wallet-service):
//
//	Reserva saldo antes de CreateShipment e debita na confirmação (rollback em
//	qualquer falha). Ver PostLabel e go/labels/internal/wallet/client.go.
package handlers

import (
	"archive/zip"
	"bytes"
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
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/senderzz/labels-service/internal/httpx"
	"github.com/senderzz/labels-service/internal/jobs"
	"github.com/senderzz/labels-service/internal/me"
	"github.com/senderzz/labels-service/internal/middleware"
	"github.com/senderzz/labels-service/internal/wallet"
	"github.com/shopspring/decimal"
)

// LabelHandler agrupa as dependências dos handlers de etiquetas.
type LabelHandler struct {
	db     *pgxpool.Pool
	me     *me.MEClient
	queue  *asynq.Client
	wallet *wallet.Client // CRIT-01: reserva/débito/liberação de saldo na emissão.
	pdfDir string
}

// NewLabelHandler cria um LabelHandler com as dependências injetadas.
func NewLabelHandler(db *pgxpool.Pool, meClient *me.MEClient, queue *asynq.Client, walletClient *wallet.Client) *LabelHandler {
	pdfDir := os.Getenv("PDF_STORAGE_DIR")
	if pdfDir == "" {
		pdfDir = "/var/senderzz/labels"
	}
	return &LabelHandler{
		db:     db,
		me:     meClient,
		queue:  queue,
		wallet: walletClient,
		pdfDir: pdfDir,
	}
}

// liberarReservaRollback libera (best-effort) uma reserva de saldo pendente quando
// a emissão da etiqueta falha em qualquer ponto entre a reserva e o débito
// confirmado. SEC-LABELS-RESERVE: esta é a barreira anti-vazamento de reserva
// (E2E-label-wallet-reserve-leak) — em particular cobre o caso em que
// DebitarReserva falha por TIMEOUT do r.Context() (request HTTP expirando):
//
//   - Usa SEMPRE um context.Background() com timeout PRÓPRIO, NUNCA o r.Context()
//     do request — assim a liberação não herda o cancelamento que causou a falha
//     do débito. Sem isso, a reserva ficaria pendente para sempre (carteira trava).
//   - É best-effort e idempotente: tpc_liberar_reserva do lado do wallet ignora tx
//     não-pendente, então é seguro chamar mesmo que o débito tenha de fato sido
//     aplicado (resposta perdida no timeout) — nesse caso o wallet simplesmente
//     não libera de novo. Falha aqui só loga (reconciliação via cron), nunca
//     propaga: já estamos no caminho de erro.
//   - No-op para txID == 0 (wallet desativado / reserva nunca criada) ou wallet nil.
func (h *LabelHandler) liberarReservaRollback(reservaTxID int64, wcOrderID int) {
	if reservaTxID == 0 || h.wallet == nil {
		return
	}
	relCtx, relCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer relCancel()
	if relErr := h.wallet.LiberarReserva(relCtx, reservaTxID, "rollback emissão de etiqueta"); relErr != nil {
		// SEC-LABELS-RESERVE: best-effort — log claro para reconciliação manual/cron;
		// não há retry em-request por design (a reserva pendente é recuperável).
		slog.Error("[senderzz_labels] PostLabel: FALHA ao liberar reserva no rollback (CRIT-01/SEC-LABELS-RESERVE) — reconciliar manualmente",
			"order_id", wcOrderID,
			"reserva_tx_id", reservaTxID,
			"err", relErr,
		)
		return
	}
	slog.Info("[senderzz_labels] reserva liberada no rollback (CRIT-01/SEC-LABELS-RESERVE)",
		"order_id", wcOrderID, "reserva_tx_id", reservaTxID)
}

// ─── Tipos de request/response ────────────────────────────────────────────────

// labelRow representa uma linha de wc_me_labels para serialização JSON.
type labelRow struct {
	ID           int64            `json:"id"`
	WCOrderID    int64            `json:"wc_order_id"`
	MEShipmentID *string          `json:"me_shipment_id,omitempty"`
	MELabelID    *string          `json:"me_label_id,omitempty"`
	Status       string           `json:"status"`
	ServiceID    int              `json:"service_id"`
	ServiceName  *string          `json:"service_name,omitempty"`
	Price        *decimal.Decimal `json:"price,omitempty"`
	TrackingCode *string          `json:"tracking_code,omitempty"`
	LabelURL     *string          `json:"label_url,omitempty"`
	LabelPDFPath *string          `json:"label_pdf_path,omitempty"`
	FromCEP      *string          `json:"from_cep,omitempty"`
	ToCEP        *string          `json:"to_cep,omitempty"`
	WeightG      *int             `json:"weight_g,omitempty"`
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at"`
}

// createLabelRequest é o corpo esperado no POST /labels.
// CRIT-01: campo "price" é ignorado se presente — preço é sempre recalculado.
type createLabelRequest struct {
	WCOrderID int `json:"wc_order_id"`
	ServiceID int `json:"service_id"`
	// Dados necessários para recalcular via ME.Calculate (CRIT-01).
	FromCEP  string           `json:"from_cep"`
	ToCEP    string           `json:"to_cep"`
	Products []me.CalcProduct `json:"products"`
	// Endereços para CreateShipment.
	From me.MEAddress `json:"from_address"`
	To   me.MEAddress `json:"to_address"`
	// Volumes.
	Volumes []me.MEVolume `json:"volumes"`
	// Opções adicionais (seguro, coleta, AR, mãos próprias).
	Options me.MEOrderOptions `json:"options"`
}

// ─── GET /labels ─────────────────────────────────────────────────────────────

// GetLabels lista etiquetas com filtros opcionais via query params:
//
//	?order_id=N    — filtra por wc_order_id
//	?status=draft  — filtra por status
//	?limit=50      — quantidade máxima (default 50, máximo 100)
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

	// AUDIT-2026-06-21 #HIGH-1: escopo de dono. Não-admin só lista as próprias
	// etiquetas (owner_user_id = caller); admin (role="admin") lista todas. NULL
	// (legado sem origem em sz_orders) nunca casa "= $N" → invisível ao não-admin.
	if !middleware.IsAdmin(r.Context()) {
		query += fmt.Sprintf(" AND owner_user_id = $%d", argIdx)
		args = append(args, middleware.GetUserID(r.Context()))
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
// Fluxo (CRIT-01 — reserva de saldo fiel a includes/tpc/wallet.php):
//  1. Valida corpo da requisição.
//  2. CRIT-01: chama ME.Calculate para obter o preço real do serviço solicitado.
//     Rejeita se o service_id solicitado não está disponível.
//  3. CRIT-01: RESERVA o preço recalculado na carteira do caller ANTES do
//     CreateShipment (wallet.Reservar). O dono da carteira é o user_id do JWT
//     (SEC-GO-01), nunca um campo do corpo.
//  4. Chama ME.CreateShipment para criar o pedido no carrinho ME.
//  5. INSERT em wc_me_labels com status=draft inicialmente.
//  6. CRIT-01: DEBITA a reserva (wallet.DebitarReserva) — só após o INSERT, para
//     que uma falha de débito nunca deixe dinheiro "no limbo" (compensação =
//     liberar reserva + cancelar shipment + cancelar etiqueta; nunca estorno).
//  7. Enfileira job Asynq TypeGeneratePDF.
//
// Rollback automático: um defer libera a reserva (wallet.LiberarReserva) em
// QUALQUER caminho de erro após a reserva e antes do débito confirmado. O flag
// `debited` só vira true após DebitarReserva ter sucesso.
//
// Off-switch: se a reserva estiver desativada (WALLET_SERVICE_URL ausente, ver
// wallet/client.go), o fluxo segue SEM débito. Quando ATIVA, é fail-closed: falha
// na reserva aborta a criação (nunca emite etiqueta que não pode cobrar).
//
// Ativo em produção: endpoints /internal/reservar|debitar-reserva|liberar-reserva
// já implementados no wallet-service (go/wallet/internal/handlers/internal.go).
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

	// ── Enriquecimento de CPF/CNPJ do destinatário ────────────────────────────
	// ME exige document no To. Se o caller não passou, busca no banco:
	//   1. sz_order_meta._billing_cpf (CPF coletado no checkout)
	//   2. senderzz_portal_users.document via sz_orders.user_id
	// Pedidos COD tipicamente não têm CPF no meta → fallback ao cadastro do usuário.
	if req.To.Document == "" {
		var cpfMeta, cpfUser string
		_ = h.db.QueryRow(r.Context(),
			`SELECT COALESCE(
			    (SELECT meta_value FROM sz_order_meta
			      WHERE order_id=$1 AND meta_key='_billing_cpf' LIMIT 1),
			    ''
			 )`, req.WCOrderID,
		).Scan(&cpfMeta)
		if cpfMeta != "" {
			req.To.Document = cpfMeta
		} else {
			_ = h.db.QueryRow(r.Context(),
				`SELECT COALESCE(u.document, '')
				   FROM sz_orders o
				   JOIN senderzz_portal_users u ON u.id = o.user_id
				  WHERE o.id = $1`, req.WCOrderID,
			).Scan(&cpfUser)
			req.To.Document = cpfUser
		}
	}
	if req.To.Document == "" {
		slog.Warn("[senderzz_labels] PostLabel: CPF/CNPJ ausente — ME exige document no To",
			"order_id", req.WCOrderID)
		httpx.WriteErr(w, http.StatusUnprocessableEntity,
			"CPF/CNPJ do destinatário é obrigatório para emissão de etiqueta via Melhor Envio. "+
				"Solicite ao cliente que informe o CPF no cadastro.")
		return
	}

	// ── SEC-LABELS: idempotência — checagem de etiqueta existente ────────────
	// Gerar etiqueta 2× para o MESMO (wc_order_id, service_id) NÃO deve duplicar
	// shipment ME, débito de carteira nem linha em wc_me_labels. Esta checagem
	// pré-flight roda ANTES de qualquer efeito colateral (Calculate / Reservar /
	// CreateShipment / INSERT / DebitarReserva) — logo um retry/clique-duplo
	// retorna a etiqueta já criada sem gastar shipment ME nem cobrar de novo.
	//
	// A chave (wc_order_id, service_id) é a MESMA de reservaRef (linhas 303-308):
	// aquele comentário já descreve a tensão "1 etiqueta/pedido no fluxo atual".
	// Aqui a tensão é RESOLVIDA no caminho feliz — a reserva nem chega a ser feita
	// na segunda chamada.
	//
	// A REGRA de bloqueio vive APENAS em labelBlocksReissue (fonte única de verdade,
	// testável): a query busca a etiqueta MAIS RECENTE para (wc_order_id, service_id)
	// SEM filtrar por status, e o predicado decide. Status "vivo" (draft/released/
	// posted/delivered/lost) → short-circuit idempotente. Última etiqueta 'canceled'
	// (ex.: débito recusado) → NÃO bloqueia → segue o fluxo e reemite.
	//
	// NÃO inventa débito: apenas LÊ o estado atual e devolve o que já existe.
	// Defesa em profundidade adicional (índice único parcial) em
	// infra/postgres/060-labels.sql — ver relatório.
	{
		var existingID int64
		var existingStatus string
		var existingShipmentID *string
		var existingPriceStr *string
		var existingOwner *int64
		err := h.db.QueryRow(r.Context(),
			`SELECT id, status, me_shipment_id, price, owner_user_id
			   FROM wc_me_labels
			  WHERE wc_order_id = $1
			    AND service_id  = $2
			  ORDER BY id DESC
			  LIMIT 1`,
			req.WCOrderID, req.ServiceID,
		).Scan(&existingID, &existingStatus, &existingShipmentID, &existingPriceStr, &existingOwner)

		// AUDIT-2026-06-21 #HIGH-1: escopo de dono na checagem de idempotência.
		// wc_order_id é enumerável — sem este filtro um atacante iteraria order_id +
		// service_id e colheria shipment_id/price/status de etiquetas alheias por esta
		// resposta idempotente. Se uma etiqueta existe mas pertence a OUTRO dono (e o
		// caller não é admin): 404 (não vaza existência) e NÃO segue ao create path
		// (que reservaria a carteira do atacante, criaria shipment ME e colidiria em
		// uq_labels_order_service_alive). Etiqueta com owner NULL (legado) também só é
		// acessível ao admin — fail-closed.
		if err == nil && existingID > 0 && !middleware.IsAdmin(r.Context()) {
			caller := middleware.GetUserID(r.Context())
			if existingOwner == nil || *existingOwner != caller {
				slog.Warn("[senderzz_labels] PostLabel: etiqueta de outro dono — 404 (AUDIT-2026-06-21 #HIGH-1)",
					"order_id", req.WCOrderID, "service_id", req.ServiceID,
					"label_id", existingID, "caller", caller)
				httpx.WriteErr(w, http.StatusNotFound, "etiqueta não encontrada")
				return
			}
		}

		switch {
		case err == nil && existingID > 0 && labelBlocksReissue(existingStatus):
			// Etiqueta viva já existe — short-circuit idempotente.
			priceOut := ""
			if existingPriceStr != nil {
				priceOut = *existingPriceStr
			}
			shipmentOut := ""
			if existingShipmentID != nil {
				shipmentOut = *existingShipmentID
			}
			slog.Info("[senderzz_labels] PostLabel: etiqueta já existente — resposta idempotente (SEC-LABELS)",
				"order_id", req.WCOrderID,
				"service_id", req.ServiceID,
				"label_id", existingID,
				"status", existingStatus,
			)
			httpx.WriteOK(w, map[string]any{
				"label_id":    existingID,
				"shipment_id": shipmentOut,
				"status":      existingStatus,
				"price":       priceOut,
				"duplicate":   true,
			})
			return
		case err != nil && err != pgx.ErrNoRows:
			// Falha real de leitura — fail-closed: não cria às cegas (poderia duplicar).
			slog.Error("[senderzz_labels] PostLabel: erro na checagem de idempotência (SEC-LABELS)",
				"order_id", req.WCOrderID, "service_id", req.ServiceID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao verificar etiqueta existente")
			return
		}
		// Demais casos seguem o fluxo de criação:
		//   - pgx.ErrNoRows                      → nenhuma etiqueta para o par → cria.
		//   - última etiqueta 'canceled'         → !labelBlocksReissue → reemite.
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

	// ── CRIT-01: Reserva de saldo ANTES do CreateShipment ────────────────────
	// Dono da carteira = user_id do JWT do caller (SEC-GO-01). NUNCA de campo do
	// corpo — body-supplied owner permitiria debitar a carteira de terceiros.
	// AVISO SEC-GO-02: assume-se que o sub do JWT == dono da carteira
	// (produtor/OL). O contrato de token do cutover ainda é indefinido (ver
	// middleware/auth.go); quando definido, validar role/issuer aqui.
	walletUserID := middleware.GetUserID(r.Context())

	// reservaTxID/debited controlam o rollback no defer abaixo.
	var reservaTxID int64
	var debited bool

	// Idempotência (CRIT-01): referencia estável por (order_id, service_id).
	// tpc_reservar reusa a tx pendente quando a referencia coincide → retry-safe.
	// TENSÃO conhecida: duas etiquetas legítimas para o MESMO pedido+serviço
	// colapsariam na MESMA reserva. Aceitável por ora (1 etiqueta/pedido no fluxo
	// atual); quando múltiplas etiquetas/pedido+serviço forem suportadas, incluir
	// um discriminador (ex: id da etiqueta) na referencia. Ver TODO do pacote wallet.
	reservaRef := fmt.Sprintf("label_order_%d_svc_%d", req.WCOrderID, req.ServiceID)
	reservaDesc := fmt.Sprintf("Frete etiqueta pedido #%d (serviço %d)", req.WCOrderID, req.ServiceID)

	if h.wallet != nil && h.wallet.Enabled() {
		if walletUserID <= 0 {
			// Reserva ativa mas sem dono identificável — fail-closed.
			slog.Error("[senderzz_labels] PostLabel: reserva ativa porém user_id do JWT ausente (SEC-GO-01)",
				"order_id", req.WCOrderID)
			httpx.WriteErr(w, http.StatusUnauthorized, "usuário não identificado para débito de saldo")
			return
		}
		// CRIT-01: reserva SEMPRE o preço recalculado (serverPrice), nunca o do req.
		txID, err := h.wallet.Reservar(r.Context(), walletUserID, serverPrice, reservaDesc, reservaRef, int64(req.WCOrderID))
		if err != nil {
			slog.Error("[senderzz_labels] PostLabel: falha ao reservar saldo (CRIT-01)",
				"order_id", req.WCOrderID,
				"user_id", walletUserID,
				"price", serverPrice.StringFixed(2),
				"err", err,
			)
			// Fail-closed: sem reserva, não cria etiqueta (saldo insuficiente / wallet off).
			httpx.WriteErr(w, http.StatusPaymentRequired, "saldo insuficiente ou indisponível para emissão da etiqueta")
			return
		}
		reservaTxID = txID
		slog.Info("[senderzz_labels] saldo reservado (CRIT-01)",
			"order_id", req.WCOrderID,
			"user_id", walletUserID,
			"reserva_tx_id", reservaTxID,
			"price", serverPrice.StringFixed(2),
		)
	}

	// SEC-LABELS-RESERVE / CRIT-01 / E2E-label-wallet-reserve-leak: rollback automático.
	// Libera a reserva em QUALQUER falha posterior à reserva e anterior ao débito
	// confirmado (debited=true). Idempotente do lado do wallet (tpc_liberar_reserva
	// ignora tx não-pendente). VERIFICADO (E2E-label-wallet-reserve-leak): este defer
	// é registrado imediatamente após reservaTxID ser setado, SEM nenhum return entre
	// a reserva e o defer — logo a reserva nunca escapa sem rollback. Todos os
	// caminhos de erro pós-reserva (CreateShipment, INSERT, DebitarReserva — inclusive
	// quando DebitarReserva falha por TIMEOUT do r.Context()) retornam com
	// debited=false → a reserva é liberada. Sem vazamento de reserva.
	defer func() {
		if !debited {
			h.liberarReservaRollback(reservaTxID, req.WCOrderID)
		}
	}()

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
		// CRIT-01: reserva é liberada pelo defer de rollback (debited ainda false).
		httpx.WriteErr(w, http.StatusBadGateway, "falha ao criar pedido no Melhor Envio")
		return
	}

	// ── INSERT em wc_me_labels ───────────────────────────────────────────────
	var labelID int64
	// AUDIT-2026-06-21 #HIGH-1: grava owner_user_id = user_id do JWT do caller (mesma
	// origem do dono da carteira em SEC-GO-01, nunca campo do corpo). É o que escopa a
	// etiqueta por tenant nas leituras (GetLabel/GetLabels/DeleteLabel) e na idempotência.
	ownerUserID := middleware.GetUserID(r.Context())
	err = h.db.QueryRow(r.Context(),
		`INSERT INTO wc_me_labels
		     (wc_order_id, me_shipment_id, status, service_id, service_name, company_name,
		      price, from_cep, to_cep, owner_user_id, created_at, updated_at)
		 VALUES ($1, $2, 'draft', $3, $4, $5, $6, $7, $8, $9, NOW(), NOW())
		 RETURNING id`,
		req.WCOrderID,
		shipmentID,
		req.ServiceID,
		selectedOption.Name,
		selectedOption.Company.Name,
		serverPrice.StringFixed(2), // CRIT-01: sempre o preço calculado
		req.FromCEP,
		req.ToCEP,
		nullableOwner(ownerUserID), // #HIGH-1: NULL se não identificável (não cria etiqueta "de ninguém" com 0)
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
		// CRIT-01: reserva é liberada pelo defer de rollback (debited ainda false).
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao registrar etiqueta")
		return
	}

	// ── CRIT-01: Debita a reserva (sucesso) ──────────────────────────────────
	// Só APÓS o INSERT: assim uma falha de débito nunca deixa dinheiro "no limbo"
	// (compensação = liberar reserva via defer + cancelar shipment + cancelar
	// etiqueta; nunca estorno, pois o dinheiro ainda não saiu da reserva).
	if reservaTxID != 0 {
		if err := h.wallet.DebitarReserva(r.Context(), reservaTxID, shipmentID); err != nil {
			slog.Error("[senderzz_labels] PostLabel: falha ao debitar reserva (CRIT-01) — revertendo etiqueta",
				"order_id", req.WCOrderID,
				"label_id", labelID,
				"reserva_tx_id", reservaTxID,
				"shipment_id", shipmentID,
				"err", err,
			)
			// Compensação (debited continua false → defer libera a reserva):
			compCtx, compCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer compCancel()
			if cancelErr := h.me.CancelShipment(compCtx, shipmentID); cancelErr != nil {
				slog.Warn("[senderzz_labels] PostLabel: falha ao cancelar shipment após débito recusado",
					"shipment_id", shipmentID, "err", cancelErr)
			}
			if _, updErr := h.db.Exec(compCtx,
				`UPDATE wc_me_labels SET status = 'canceled', updated_at = NOW() WHERE id = $1`,
				labelID,
			); updErr != nil {
				slog.Warn("[senderzz_labels] PostLabel: falha ao cancelar etiqueta após débito recusado",
					"label_id", labelID, "err", updErr)
			}
			httpx.WriteErr(w, http.StatusPaymentRequired, "falha ao debitar saldo — etiqueta não emitida")
			return
		}
		debited = true // CRIT-01: a partir daqui o defer NÃO libera a reserva.
		slog.Info("[senderzz_labels] reserva debitada (CRIT-01)",
			"order_id", req.WCOrderID,
			"label_id", labelID,
			"reserva_tx_id", reservaTxID,
			"shipment_id", shipmentID,
		)
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

	// AUDIT-2026-06-21 #HIGH-1: escopo de dono. Não-admin só lê a própria etiqueta;
	// id fora de escopo (ou de outro dono, ou owner NULL legado) cai em pgx.ErrNoRows
	// → 404, sem vazar a existência. Admin (role="admin") lê qualquer id.
	query := `SELECT id, wc_order_id, me_shipment_id, me_label_id, status,
		        service_id, service_name, price, tracking_code,
		        label_url, label_pdf_path, from_cep, to_cep, weight_g,
		        created_at, updated_at
		 FROM wc_me_labels
		 WHERE id = $1`
	args := []any{labelID}
	if !middleware.IsAdmin(r.Context()) {
		query += " AND owner_user_id = $2"
		args = append(args, middleware.GetUserID(r.Context()))
	}
	row := h.db.QueryRow(r.Context(), query, args...)

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

	// Se tiver me_shipment_id, busca status atual na ME (read-only, não atualiza
	// banco aqui). AUDIT-2026-07-30 #9: TrackShipment espera o shipment_id da ME
	// (UUID interno), não o tracking_code da transportadora.
	var trackingStatus string
	if lbl.MEShipmentID != nil && *lbl.MEShipmentID != "" {
		ts, err := h.me.TrackShipment(r.Context(), *lbl.MEShipmentID)
		if err != nil {
			// Falha de rastreamento não bloqueia — retorna etiqueta com status do banco.
			slog.Warn("[senderzz_labels] GetLabel: falha ao rastrear",
				"label_id", labelID,
				"me_shipment_id", *lbl.MEShipmentID,
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
	// AUDIT-2026-06-21 #HIGH-1: escopo de dono. Não-admin só cancela a própria
	// etiqueta; id fora de escopo cai em pgx.ErrNoRows → 404 (não vaza existência nem
	// permite cancelar etiqueta alheia). Admin cancela qualquer id.
	var shipmentID *string
	var status string
	var wcOrderID int64
	var price *string
	var ownerUserID *int64
	delQuery := `SELECT me_shipment_id, status, wc_order_id, price, owner_user_id FROM wc_me_labels WHERE id = $1`
	delArgs := []any{labelID}
	if !middleware.IsAdmin(r.Context()) {
		delQuery += " AND owner_user_id = $2"
		delArgs = append(delArgs, middleware.GetUserID(r.Context()))
	}
	err = h.db.QueryRow(r.Context(), delQuery, delArgs...).Scan(&shipmentID, &status, &wcOrderID, &price, &ownerUserID)

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
	//
	// AUDIT-2026-07-27 (pedido do dono): "eliminar aquela etiqueta cancelada essa
	// função precisa estar funcionando" — dois bugs aqui. (1) só tentava
	// CancelShipment (DELETE /me/cart/{id}), que só funciona ANTES do checkout;
	// etiqueta cujo PDF já foi gerado já foi PAGA (checkout roda em
	// ProcessGeneratePDF) e a ME recusa cancelar carrinho de pedido pago — cai pro
	// endpoint correto (RequestCancelShipment, POST /me/shipment/cancel).
	// (2) NUNCA estornava a carteira TPC — cancelar etiqueta paga cancelava o
	// envio mas o dinheiro debitado sumia pro produtor.
	if shipmentID != nil && *shipmentID != "" {
		if err := h.me.CancelShipment(r.Context(), *shipmentID); err != nil {
			slog.Warn("[senderzz_labels] DeleteLabel: cancelamento de carrinho falhou, tentando cancelamento pós-pago",
				"label_id", labelID, "shipment_id", *shipmentID, "err", err)
			if err2 := h.me.RequestCancelShipment(r.Context(), *shipmentID); err2 != nil {
				slog.Error("[senderzz_labels] DeleteLabel: falha ao cancelar na ME (ambos os caminhos)",
					"label_id", labelID, "shipment_id", *shipmentID, "cart_err", err, "posted_err", err2)
				httpx.WriteErr(w, http.StatusBadGateway, "falha ao cancelar o envio")
				return
			}
		}
	}

	// Registra o estorno como PENDENTE — NÃO credita saldo agora (só se a
	// etiqueta chegou a debitar; price setado no INSERT de emit.go/PostLabel;
	// draft nunca chega aqui com price nulo na prática, mas o nil-guard cobre
	// o caso defensivamente).
	//
	// AUDIT-2026-07-28 (dono): "o cancelamento automaticamente cancela a
	// etiqueta no melhor envio e somente após o saldo ser reposto no melhor
	// envio, volta para o usuário [...] carteira ME e falk precisam estar
	// sincronizadas, nunca creditar sem ter caído na conta melhor envio." —
	// cancelar na ME acima já aconteceu; o crédito na carteira TPC fica
	// pendente até confirmação (ops confirma manualmente via
	// ConfirmarEstorno, ver /internal/confirmar-estorno no wallet-service).
	estornoPendente := false
	if price != nil && ownerUserID != nil {
		if valor, convErr := decimal.NewFromString(*price); convErr == nil && valor.GreaterThan(decimal.Zero) && h.wallet != nil && h.wallet.Enabled() {
			ref := fmt.Sprintf("cancel_label_%d", labelID)
			desc := fmt.Sprintf("Estorno etiqueta #%d cancelada (pedido #%d) — aguardando confirmação do reembolso", labelID, wcOrderID)
			if err := h.wallet.EstornarPendente(r.Context(), *ownerUserID, valor, desc, ref, wcOrderID); err != nil {
				slog.Error("[senderzz_labels] DeleteLabel: falha ao registrar estorno pendente — reconciliar manualmente",
					"label_id", labelID, "owner_user_id", *ownerUserID, "valor", valor.StringFixed(2), "err", err)
			} else {
				estornoPendente = true
			}
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

	resp := map[string]any{
		"label_id": labelID,
		"status":   "canceled",
	}
	if estornoPendente {
		resp["estorno_pendente"] = true
		resp["estorno_mensagem"] = "Etiqueta cancelada. O saldo será reposto automaticamente em até 2 a 24 horas, assim que o reembolso for confirmado."
	}
	httpx.WriteOK(w, resp)
}

// ─── GET /calculate ─────────────────────────────────────────────────────────

// GetCalculate é um proxy para ME.Calculate com cache de 10 minutos em wc_me_shipment_cache.
// Usado pelo checkout para exibir opções de frete sem chamar a ME API a cada request.
//
// CRIT-01: Este endpoint é apenas informativo (checkout). O preço real para cobrança
// é sempre recalculado em POST /labels antes de criar a etiqueta.
//
// Query params obrigatórios:
//
//	from_cep=XXXXXXXX
//	to_cep=XXXXXXXX
//	weight=0.5        (kg, float)
//	height=15         (cm, float)
//	width=20          (cm, float)
//	length=30         (cm, float)
//	value=150.00      (valor declarado, decimal)
func (h *LabelHandler) GetCalculate(w http.ResponseWriter, r *http.Request) {
	fromCEP := r.URL.Query().Get("from_cep")
	toCEP := r.URL.Query().Get("to_cep")
	if fromCEP == "" || toCEP == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "from_cep e to_cep são obrigatórios")
		return
	}

	// Monta um produto único a partir dos parâmetros para simplificar o cálculo no checkout.
	// SEC-LABELS: o defaulting de dimensões/peso foi extraído para normalizeDims
	// (função pura, testável sem rede). Valores ausentes/<=0 caem nos mínimos PAC.
	weightRaw, _ := strconv.ParseFloat(r.URL.Query().Get("weight"), 64)
	heightRaw, _ := strconv.ParseFloat(r.URL.Query().Get("height"), 64)
	widthRaw, _ := strconv.ParseFloat(r.URL.Query().Get("width"), 64)
	lengthRaw, _ := strconv.ParseFloat(r.URL.Query().Get("length"), 64)
	weight, height, width, length := normalizeDims(weightRaw, heightRaw, widthRaw, lengthRaw)

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
			"data":   cached,
			"cached": true,
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

// normalizeDims aplica os valores mínimos (padrão PAC dos Correios) a peso e
// dimensões quando o valor recebido é ausente ou <= 0.
//
// SEC-LABELS: função pura (sem rede) extraída de GetCalculate para ser testável.
// Mantém EXATAMENTE o comportamento original do handler: peso mínimo 0.3 kg,
// altura 11 cm, largura 15 cm, comprimento 20 cm. Valores positivos passam intactos.
func normalizeDims(weight, height, width, length float64) (w, h, wd, l float64) {
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
	return weight, height, width, length
}

// labelBlocksReissue decide, a partir do status de uma etiqueta existente, se ela
// BLOQUEIA a emissão de uma nova etiqueta para o mesmo (wc_order_id, service_id).
//
// SEC-LABELS: predicado puro (sem DB/rede) extraído para ser testável e para
// documentar a regra de idempotência do PostLabel. Qualquer status que represente
// uma etiqueta "viva" bloqueia a reemissão (draft já criada, released, posted,
// delivered, lost — todas correspondem a um shipment/cobrança já efetivados ou em
// trânsito). Apenas 'canceled' (e status desconhecido, por segurança permissiva
// controlada) NÃO bloqueia — uma etiqueta cancelada (ex.: débito recusado) pode
// ser reemitida.
func labelBlocksReissue(status string) bool {
	switch status {
	case "draft", "released", "posted", "delivered", "lost":
		return true
	case "canceled":
		return false
	default:
		// Status desconhecido: trata como bloqueante (fail-safe — não reemite às cegas
		// sobre um estado que não entendemos). A query pré-flight já exclui 'canceled',
		// então na prática só status válidos chegam aqui.
		return true
	}
}

// nullableOwner converte o user_id do caller em valor para a coluna owner_user_id:
// AUDIT-2026-06-21 #HIGH-1 — retorna nil (NULL no banco) quando o id é <= 0 (não
// identificável), evitando gravar uma etiqueta "de ninguém" com dono 0 que nenhum
// usuário real possui. Caso normal: > 0 → o próprio id.
func nullableOwner(userID int64) *int64 {
	if userID <= 0 {
		return nil
	}
	return &userID
}

// InternalEnqueueGenerate re-enfileira a geração de PDF para uma etiqueta existente.
// POST /internal/labels/{id}/enqueue-generate — sem JWT, somente rede Docker interna.
// Usado pelo admin-service após o operador clicar em "Gerar" para retriggar um job
// que falhou (ex.: Redis estava fora quando a etiqueta foi criada via POST /labels).
func (h *LabelHandler) InternalEnqueueGenerate(w http.ResponseWriter, r *http.Request) {
	id, err := parseLabelID(r)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}

	var shipmentID *string
	var status string
	err = h.db.QueryRow(r.Context(),
		`SELECT me_shipment_id, status FROM wc_me_labels WHERE id = $1`, id).
		Scan(&shipmentID, &status)
	if err == pgx.ErrNoRows {
		httpx.WriteErr(w, http.StatusNotFound, "etiqueta não encontrada")
		return
	}
	if err != nil {
		slog.Error("[senderzz_labels] InternalEnqueueGenerate: erro ao consultar", "label_id", id, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao consultar etiqueta")
		return
	}
	if shipmentID == nil || *shipmentID == "" {
		httpx.WriteErr(w, http.StatusUnprocessableEntity,
			"etiqueta sem me_shipment_id — não pode ser (re)gerada; use POST /labels para criar")
		return
	}
	if status == "delivered" || status == "canceled" {
		httpx.WriteErr(w, http.StatusConflict,
			fmt.Sprintf("etiqueta com status '%s' não pode ser regerada", status))
		return
	}

	// Insere registro de retry na fila durável.
	_, _ = h.db.Exec(r.Context(),
		`INSERT INTO wc_me_queue (label_id, action, scheduled_at, created_at)
		 VALUES ($1, 'generate_pdf', NOW(), NOW())`, id)

	if err := jobs.EnqueueGeneratePDF(h.queue, id, *shipmentID); err != nil {
		slog.Error("[senderzz_labels] InternalEnqueueGenerate: erro ao enfileirar",
			"label_id", id, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao enfileirar job de geração")
		return
	}

	slog.Info("[senderzz_labels] InternalEnqueueGenerate: job enfileirado",
		"label_id", id, "shipment_id", *shipmentID)
	httpx.WriteOK(w, map[string]any{"ok": true, "label_id": id, "queued": true})
}

// InternalListCarriers chama GET /me/shipment/companies na ME e devolve o
// catálogo completo (transportadoras + serviços) da conta da plataforma.
// Rota interna (rede Docker) — consumida pelo admin-service para montar a tela
// de seleção global de transportadoras (senderzz_enabled_carriers_map).
func (h *LabelHandler) InternalListCarriers(w http.ResponseWriter, r *http.Request) {
	companies, err := h.me.ListCompanies(r.Context())
	if err != nil {
		slog.Warn("[senderzz_labels] InternalListCarriers: falha ao consultar ME", "err", err)
		httpx.WriteErr(w, http.StatusBadGateway, "falha ao consultar catálogo de transportadoras na ME")
		return
	}
	httpx.WriteOK(w, map[string]any{"ok": true, "companies": companies})
}

// InternalServePDF — GET /internal/labels/{id}/pdf-file (pedido dono 2026-07-28:
// "deve baixar PDF e abrir ele, nada de Melhor Envio" — o link da ME abre a
// PÁGINA interativa deles (com toggles de DACE/romaneio), não o PDF puro. O PDF
// real já fica salvo em disco local pelo job de geração (label_pdf_path) — serve
// os bytes direto daqui, sem passar pela ME de novo. Rota interna (rede Docker),
// proxied pelo admin-service (que checa auth) — nunca exposta direto ao público.
func (h *LabelHandler) InternalServePDF(w http.ResponseWriter, r *http.Request) {
	id, err := parseLabelID(r)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var pdfPath *string
	var meShipmentID *string
	err = h.db.QueryRow(r.Context(),
		`SELECT label_pdf_path, me_shipment_id FROM wc_me_labels WHERE id = $1`, id,
	).Scan(&pdfPath, &meShipmentID)
	if err != nil {
		httpx.WriteErr(w, http.StatusNotFound, "etiqueta não encontrada")
		return
	}

	if pdfPath != nil && *pdfPath != "" {
		if f, openErr := os.Open(*pdfPath); openErr == nil {
			defer f.Close()
			w.Header().Set("Content-Type", "application/pdf")
			w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="etiqueta-%d.pdf"`, id))
			_, _ = io.Copy(w, f)
			return
		}
		// AUDIT-2026-07-28: label_pdf_path aponta pro filesystem do container —
		// antes de montar volume persistente, redeploy apagava PDFs já gerados
		// (DB continua achando que existe). Cai pro fallback abaixo em vez de 404.
		slog.Warn("[senderzz_labels] InternalServePDF: PDF referenciado no banco sumiu do disco — tentando reobter na ME",
			"label_id", id, "path", *pdfPath)
	}

	if meShipmentID == nil || *meShipmentID == "" {
		httpx.WriteErr(w, http.StatusNotFound, "PDF não encontrado (nem local, nem na ME)")
		return
	}
	// AUDIT-2026-07-28 (correção): /me/shipment/print (PrintLabel) só devolve a
	// URL da página HTML interativa da ME, que exige sessão de navegador. O
	// endpoint CERTO é GET /me/imprimir/{formato}/{id} (documentado em
	// docs.melhorenvio.com.br/reference/impressao-de-etiquetas-em-arquivo) —
	// devolve array com URL assinada S3 de PDF puro, sem sessão nenhuma
	// (confirmado ao vivo: 116KB, magic bytes %PDF-1.3). Baixa e cacheia local.
	fileURL, meErr := h.me.PrintFileURL(r.Context(), *meShipmentID, "pdf")
	if meErr != nil || fileURL == "" {
		slog.Error("[senderzz_labels] InternalServePDF: falha ao obter arquivo na ME", "err", meErr, "label_id", id)
		httpx.WriteErr(w, http.StatusNotFound, "PDF não encontrado — a etiqueta pode ter sido cancelada")
		return
	}
	pdfBytes, dlErr := h.me.DownloadFile(r.Context(), fileURL)
	if dlErr != nil {
		slog.Error("[senderzz_labels] InternalServePDF: falha ao baixar PDF", "err", dlErr, "label_id", id)
		httpx.WriteErr(w, http.StatusBadGateway, "falha ao baixar PDF da ME")
		return
	}
	if h.pdfDir != "" {
		if mkErr := os.MkdirAll(h.pdfDir, 0o755); mkErr == nil {
			localPath := filepath.Join(h.pdfDir, fmt.Sprintf("%d.pdf", id))
			if wErr := os.WriteFile(localPath, pdfBytes, 0o644); wErr == nil {
				_, _ = h.db.Exec(r.Context(), `UPDATE wc_me_labels SET label_pdf_path=$1 WHERE id=$2`, localPath, id)
			}
		}
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="etiqueta-%d.pdf"`, id))
	_, _ = w.Write(pdfBytes)
}

// InternalPrintBatch — POST /internal/labels/print-batch (pedido dono 2026-07-28).
//
// Recebe uma lista de wc_order_id (1 ou vários — individual e lote usam o MESMO
// endpoint) já com etiqueta EMITIDA (nunca gera etiqueta nova aqui — só recupera
// URLs de algo que já existe, "imprimir após separado sem gerar nova alteração
// de status"). Resolve wc_order_id → me_shipment_id em wc_me_labels e chama a ME
// em lote: PrintLabelsBatch (etiquetas) e/ou GenerateDeclaration (DACE simplificado),
// conforme want_labels/want_declaration. A ME já devolve 1 PDF combinado por chamada
// quando há múltiplos shipment_ids — não precisamos concatenar nada aqui.
type internalPrintBatchRequest struct {
	WCOrderIDs      []int64 `json:"wc_order_ids"`
	WantLabels      bool    `json:"want_labels"`
	WantDeclaration bool    `json:"want_declaration"`
}

type internalPrintBatchResponse struct {
	OK              bool    `json:"ok"`
	LabelURL        string  `json:"label_url,omitempty"`
	DeclarationURL  string  `json:"declaration_url,omitempty"`
	MissingOrderIDs []int64 `json:"missing_order_ids,omitempty"`
	Error           string  `json:"error,omitempty"`
}

func (h *LabelHandler) InternalPrintBatch(w http.ResponseWriter, r *http.Request) {
	var req internalPrintBatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "json inválido")
		return
	}
	if len(req.WCOrderIDs) == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "wc_order_ids vazio")
		return
	}
	if !req.WantLabels && !req.WantDeclaration {
		httpx.WriteErr(w, http.StatusBadRequest, "informe want_labels e/ou want_declaration")
		return
	}

	rows, err := h.db.Query(r.Context(),
		`SELECT wc_order_id, me_shipment_id
		   FROM wc_me_labels
		  WHERE wc_order_id = ANY($1)
		    AND me_shipment_id IS NOT NULL
		    AND status NOT IN ('canceled', 'lost')
		  ORDER BY id DESC`,
		req.WCOrderIDs,
	)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao buscar etiquetas")
		return
	}
	found := map[int64]string{} // wc_order_id -> shipment_id (1ª/mais recente linha, ORDER BY id DESC)
	for rows.Next() {
		var wcOrderID int64
		var shipmentID string
		if err := rows.Scan(&wcOrderID, &shipmentID); err != nil {
			continue
		}
		if _, exists := found[wcOrderID]; !exists {
			found[wcOrderID] = shipmentID
		}
	}
	rows.Close()

	if len(found) == 0 {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "nenhum dos pedidos tem etiqueta emitida")
		return
	}

	shipmentIDs := make([]string, 0, len(found))
	for _, sid := range found {
		shipmentIDs = append(shipmentIDs, sid)
	}
	var missing []int64
	for _, oid := range req.WCOrderIDs {
		if _, ok := found[oid]; !ok {
			missing = append(missing, oid)
		}
	}

	// AUDIT-2026-07-28: want_labels/want_declaration devolvem os BYTES do PDF
	// direto na resposta (via /me/imprimir/{pdf,dace/pdf}/{id}, sem sessão de
	// navegador), não mais uma URL pra abrir em nova aba. 1 shipment = PDF puro;
	// 2+ = ZIP com um arquivo por pedido. Curto-circuita a resposta JSON — o
	// caller nunca manda want_labels E want_declaration juntos numa chamada só.
	type pdfFile struct {
		name  string
		bytes []byte
	}
	fetchFiles := func(namePrefix string, fileURLFn func(ctx context.Context, shipmentID string) (string, error)) ([]pdfFile, error) {
		files := make([]pdfFile, 0, len(shipmentIDs))
		for wcOrderID, shipmentID := range found {
			fileURL, err := fileURLFn(r.Context(), shipmentID)
			if err != nil {
				return nil, fmt.Errorf("obter arquivo (shipment_id=%s): %w", shipmentID, err)
			}
			b, err := h.me.DownloadFile(r.Context(), fileURL)
			if err != nil {
				return nil, fmt.Errorf("baixar arquivo (shipment_id=%s): %w", shipmentID, err)
			}
			files = append(files, pdfFile{name: fmt.Sprintf("%s-%d.pdf", namePrefix, wcOrderID), bytes: b})
		}
		return files, nil
	}
	// AUDIT-2026-07-29 (dono): "empilhamento de arquivos ... vem tudo junto de
	// cada categoria etiqueta/dace" — 2+ pedidos NÃO viram mais um ZIP (um
	// arquivo por pedido pra abrir um por um); viram UM PDF só empilhado
	// (páginas concatenadas na ordem), pronto pra imprimir de uma vez.
	writeFiles := func(files []pdfFile, mergedName string) {
		if len(files) == 1 {
			w.Header().Set("Content-Type", "application/pdf")
			w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, files[0].name))
			_, _ = w.Write(files[0].bytes)
			return
		}
		readers := make([]io.ReadSeeker, 0, len(files))
		for _, f := range files {
			readers = append(readers, bytes.NewReader(f.bytes))
		}
		var merged bytes.Buffer
		if err := api.MergeRaw(readers, &merged, false, nil); err != nil {
			slog.Error("[senderzz_labels] InternalPrintBatch: falha ao empilhar PDFs, caindo pra ZIP", "err", err)
			w.Header().Set("Content-Type", "application/zip")
			w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, strings.TrimSuffix(mergedName, ".pdf")+".zip"))
			zw := zip.NewWriter(w)
			for _, f := range files {
				fw, ferr := zw.Create(f.name)
				if ferr != nil {
					continue
				}
				_, _ = fw.Write(f.bytes)
			}
			_ = zw.Close()
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, mergedName))
		_, _ = w.Write(merged.Bytes())
	}

	if req.WantLabels {
		files, err := fetchFiles("etiqueta", func(ctx context.Context, shipmentID string) (string, error) {
			return h.me.PrintFileURL(ctx, shipmentID, "pdf")
		})
		if err != nil {
			slog.Error("[senderzz_labels] InternalPrintBatch: falha ao recuperar etiqueta(s)", "err", err)
			httpx.WriteErr(w, http.StatusBadGateway, "falha ao recuperar etiqueta(s) na ME")
			return
		}
		writeFiles(files, "etiquetas.pdf")
		return
	}
	if req.WantDeclaration {
		files, err := fetchFiles("dace", func(ctx context.Context, shipmentID string) (string, error) {
			return h.me.DaceFileURL(ctx, shipmentID, "pdf")
		})
		if err != nil {
			slog.Error("[senderzz_labels] InternalPrintBatch: falha ao recuperar declaração(ões)", "err", err)
			httpx.WriteErr(w, http.StatusBadGateway, "falha ao recuperar declaração de conteúdo na ME")
			return
		}
		writeFiles(files, "declaracoes.pdf")
		return
	}
}

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

// sumProductsInsuranceValue soma insurance_value*quantity de cada item — é o
// valor real da mercadoria a segurar no shipment (options.insurance_value),
// nunca o preço do frete. Espelha o mesmo dado já usado por item em
// buildMEProducts (UnitaryValue: p.InsuranceValue).
func sumProductsInsuranceValue(products []me.CalcProduct) decimal.Decimal {
	total := decimal.Zero
	for _, p := range products {
		total = total.Add(p.InsuranceValue.Mul(decimal.NewFromInt(int64(p.Quantity))))
	}
	return total
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

// buildMEVolumes deriva o único pacote físico enviado à transportadora.
//
// As linhas de sz_order_items descrevem o conteúdo do pedido, não caixas
// separadas. Transformar cada linha em um volume fazia kits/composições virarem
// múltiplos pacotes e os Correios recusarem a emissão. Mantemos a maior dimensão
// informada em cada eixo e somamos peso × quantidade de todos os itens.
func buildMEVolumes(products []me.CalcProduct) []me.MEVolume {
	if len(products) == 0 {
		return []me.MEVolume{}
	}

	volume := me.MEVolume{}
	for _, p := range products {
		if p.Height > volume.Height {
			volume.Height = p.Height
		}
		if p.Width > volume.Width {
			volume.Width = p.Width
		}
		if p.Length > volume.Length {
			volume.Length = p.Length
		}

		quantity := p.Quantity
		if quantity < 1 {
			quantity = 1
		}
		volume.Weight += p.Weight * float64(quantity)
	}

	return []me.MEVolume{volume}
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

	// ── E2E-webhook-tracking-replay-no-dedup: idempotência fail-closed ───────
	// Espelha o webhook PIX (go/wallet/internal/handlers/pix.go::confirmarRecarga):
	// registra o evento em tpc_webhook_events (UNIQUE KEY uq_event_key) DENTRO da
	// MESMA transação do UPDATE em wc_me_labels e ANTES de aplicar o efeito. Se o
	// evento já foi processado (replay/retry do provedor), o ON CONFLICT DO NOTHING
	// detecta via RowsAffected()==0 → retorna idempotente SEM reexecutar o UPDATE e
	// SEM reenfileirar SyncTracking (evita update-loop / job duplicado no replay).
	//
	// A chave usa o me-status BRUTO (não o mapeado): transições legítimas distintas
	// (posted → in_transit → delivered) geram chaves distintas e processam; apenas
	// replays do MESMO (tracking_code, status) colapsam — exatamente o anti-loop.
	// Namespace "track:" evita colisão com as chaves "pix:" gravadas pelo wallet na
	// mesma tabela compartilhada (source="tracking" já previsto em schema-wallet.sql).
	eventKey := fmt.Sprintf("track:%s:%s", payload.TrackingCode, payload.Status)
	payloadHash := cacheKeyFrom(string(bodyBytes)) // SHA-256 hex do body cru.

	tx, err := h.db.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		slog.Error("[senderzz_labels] PostTrackingWebhook: erro ao iniciar transação",
			"tracking_code", payload.TrackingCode, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao processar webhook")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	// Registra a idempotência ANTES do efeito, na mesma transação (padrão PIX
	// go-pix-idem-tx): RowsAffected()==0 → evento já processado → rollback + return.
	evtTag, err := tx.Exec(r.Context(),
		`INSERT INTO tpc_webhook_events
		    (event_key, source, event_type, payload_hash, me_id, status)
		 VALUES ($1, 'tracking', 'status_update', $2, $3, $4)
		 ON CONFLICT (event_key) DO NOTHING`,
		eventKey, payloadHash, payload.TrackingCode, payload.Status,
	)
	if err != nil {
		slog.Error("[senderzz_labels] PostTrackingWebhook: erro ao registrar evento (idempotência)",
			"tracking_code", payload.TrackingCode, "event_key", eventKey, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao processar webhook")
		return
	}
	if evtTag.RowsAffected() == 0 {
		// Replay/retry: evento já processado. Responde idempotente sem reexecutar.
		slog.Info("[senderzz_labels] PostTrackingWebhook: evento duplicado ignorado (idempotente)",
			"tracking_code", payload.TrackingCode,
			"me_status", payload.Status,
			"event_key", eventKey,
		)
		httpx.WriteOK(w, map[string]any{
			"tracking_code": payload.TrackingCode,
			"status":        internalStatus,
			"updated":       int64(0),
			"duplicate":     true,
		})
		return
	}

	result, err := tx.Exec(r.Context(),
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

	// Commit: idempotência + UPDATE atômicos. Se cair antes daqui, NADA persiste
	// (nem o evento) → o retry do provedor reprocessa normalmente (sem suprimir).
	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("[senderzz_labels] PostTrackingWebhook: erro ao commitar transação",
			"tracking_code", payload.TrackingCode, "event_key", eventKey, "err", err)
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

// mapTrackingStatus mapeia status BRUTO da ME pro status canônico interno
// (enum de wc_me_labels: draft/released/posted/delivered/canceled/lost).
// Duplica a lógica de jobs.mapMEStatus para uso no handler webhook.
//
// AUDIT-2026-07-30 #4 (dono: "tem coisa muito errada" — rastreio público
// mostrando "Em Rota" pra pedido que a ME ainda dizia 'released'): o fallback
// antigo "default: posted" pra QUALQUER status desconhecido (mesmo bug já
// corrigido em mapMEStatusToOrderStatus pro sz_orders, mas esquecido aqui) fez
// wc_me_labels.status gravar 'posted' com a ME real ainda em 'released' —
// esse valor errado vazou pro rastreio público (tracking.go usa
// wc_me_labels.status=='posted' como proxy de "despachado"). Lista completa
// oficial de status ME (docs.melhorenvio.com.br/reference/webhooks): created,
// pending, released, generated, received, posted, delivered, cancelled,
// undelivered, paused, suspended — mapeada explicitamente, "released" (etiqueta
// paga, SEM despacho confirmado) nunca mais fallback pra "posted".
func mapTrackingStatus(meStatus string) string {
	switch meStatus {
	case "received", "posted", "in_transit", "out_for_delivery":
		return "posted"
	case "delivered":
		return "delivered"
	case "canceled", "cancelled", "returned", "undelivered":
		return "canceled"
	case "lost":
		return "lost"
	// created/pending/released/generated/waiting_pickup/paused/suspended —
	// etiqueta existe mas SEM despacho confirmado. "released" é status válido
	// no enum de wc_me_labels (nunca fabrica "posted" sem confirmação real).
	default:
		return "released"
	}
}

// orderStatusRank ordena o pipeline sz_orders.status pra sync a partir do ME
// NUNCA regredir (ex.: evento antigo reenviado pela ME não pode voltar um
// pedido já 'entregue' pra 'enviado'), e nunca mexer em estados terminais
// controlados por admin (cancelado/reembolsado têm fluxo próprio de estorno,
// ver ReconcileEstornosTick em balance.go).
var orderStatusRank = map[string]int{
	"pending": 0, "aguardando": 0, "on-hold": 0,
	"em_andamento": 1, "processing": 2, "em_separacao": 3, "embalado": 4,
	// AUDIT-2026-07-31 (dono): 'coletado' — operador logístico marca no ponto
	// de coleta, entre embalado e enviado. 'enviado' sempre sobrepõe (rank 5 > 4).
	"coletado": 4,
	// Jadlog pode confirmar "a caminho" depois de o Melhor Envio já ter
	// marcado o pedido como enviado. Mesmo nível evita regressão quando o
	// Melhor Envio reenviar o status antigo.
	"enviado": 5, "a_caminho": 5, "entregue": 6, "frustrado": 6, "completo": 6,
}

// mapMEStatusToOrderStatus converte o status BRUTO da ME (não o já-colapsado
// de mapTrackingStatus) pro status equivalente de sz_orders. AUDIT-2026-07-30
// (dono): "preciso que o processo completo do pedido seja acompanhado via
// atualização do melhor envio" — antes o webhook só atualizava wc_me_labels,
// nunca sz_orders.
//
// AUDIT-2026-07-30 #2 (bug real, pego em produção): mapTrackingStatus tem
// fallback "default: posted" (correto pra wc_me_labels, cujo enum não tem
// 'released'/'pending' — qualquer coisa desconhecida vira 'posted' ali de
// forma inofensiva). Reusar esse fallback aqui avançou 6 pedidos pra
// 'enviado' com a ME ainda em 'released'/'received' (etiqueta paga, SEM
// coleta física ainda) — revertido manualmente. Por isso aqui é allowlist
// explícita sobre o status BRUTO, nunca fallback otimista: só considera
// "enviado" o que a ME realmente marca como postado/em trânsito.
//
// AUDIT-2026-07-30 #3 (dono: "garanta que todas etapas novas do melhor envio
// estejam mapeadas... não queremos ter retrabalho"): lista COMPLETA e oficial
// de status ME (docs.melhorenvio.com.br/reference/webhooks) — created,
// pending, released, generated, received, posted, delivered, cancelled,
// undelivered, paused, suspended. Mapeadas todas explicitamente abaixo, nunca
// mais depender de fallback pra status desconhecido.
func mapMEStatusToOrderStatus(rawMEStatus string) string {
	switch rawMEStatus {
	// Etiqueta criada/paga/gerada mas SEM confirmação de despacho — não avança.
	case "created", "pending", "released", "generated":
		return ""
	// "received" = pacote recebido no ponto de distribuição da transportadora —
	// despacho real confirmado, mesmo sem tracking code ainda emitido.
	case "received", "posted", "in_transit", "out_for_delivery":
		return "enviado"
	case "delivered":
		return "entregue"
	case "canceled", "cancelled", "returned", "lost", "undelivered":
		return "frustrado"
	// "paused"/"suspended" = anomalia (retenção fiscal, endereço incompleto etc.)
	// — não é progresso normal, não avança sozinho; fica visível no painel de
	// mismatches pra ação manual do admin.
	case "paused", "suspended":
		return ""
	default:
		return ""
	}
}

// syncOrderStatusFromME aplica o novo status em sz_orders SE for avanço no
// pipeline (nunca regride, nunca mexe em cancelado/reembolsado). Retorna
// (aplicou, status_anterior, erro). Usada tanto pelo webhook automático quanto
// pelo endpoint manual de reconciliação admin.
// pgxQuerier é o subconjunto de *pgxpool.Pool/pgx.Tx usado por syncOrderStatusFromME
// — permite chamar tanto fora de transação (endpoint manual) quanto dentro
// (webhook, mesma tx que atualiza wc_me_labels).
type pgxQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func syncOrderStatusFromME(ctx context.Context, db pgxQuerier, wcOrderID int64, rawMEStatus string) (bool, string, error) {
	newStatus := mapMEStatusToOrderStatus(rawMEStatus)
	if newStatus == "" {
		return false, "", nil
	}
	newRank, ok := orderStatusRank[newStatus]
	if !ok {
		return false, "", nil
	}

	var curStatus string
	if err := db.QueryRow(ctx,
		`SELECT status FROM sz_orders WHERE id = $1`, wcOrderID,
	).Scan(&curStatus); err != nil {
		return false, "", err
	}

	if curStatus == "cancelled" || curStatus == "reembolsado" {
		return false, curStatus, nil
	}
	curRank, known := orderStatusRank[curStatus]
	if known && curRank >= newRank {
		return false, curStatus, nil
	}

	if _, err := db.Exec(ctx,
		`UPDATE sz_orders SET status = $1, updated_at = NOW() WHERE id = $2`,
		newStatus, wcOrderID,
	); err != nil {
		return false, curStatus, err
	}
	// AUDIT-2026-07-30 #6 (dono: "falta horários... direto do ME"): sz_orders
	// já tem uma tabela de histórico real (sz_order_status_history, populada
	// por vários handlers Go em checkout/admin/portal) que o rastreio público
	// usa pra mostrar o horário de cada etapa — mas essa sync automática via
	// webhook/reconciliação ME fazia UPDATE direto sem logar aqui. Sem esse
	// INSERT, pedidos avançados pelo ME nunca ganhavam timestamp real de
	// 'enviado'/'entregue' na história (só a aproximação via wc_me_labels).
	if _, err := db.Exec(ctx,
		`INSERT INTO sz_order_status_history (order_id, status_de, status_para, actor_tipo)
		 VALUES ($1, $2, $3, 'webhook')`,
		wcOrderID, curStatus, newStatus,
	); err != nil {
		// O status não deve ser perdido, mas a falha precisa aparecer no log para
		// não deixar a transição automática sem histórico silenciosamente.
		slog.Error("[senderzz_labels] falha ao registrar histórico de status vindo da ME",
			"order_id", wcOrderID, "status_de", curStatus, "status_para", newStatus, "err", err)
	}
	return true, curStatus, nil
}

// cacheKeyFrom gera cache key SHA-256 a partir de uma string.
// Usado internamente para keys que não passam pelo MEClient.
func cacheKeyFrom(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
