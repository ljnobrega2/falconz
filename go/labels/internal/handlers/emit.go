// Package handlers — emissão server-side de etiqueta (1-click e auto-emit).
//
// Rotas (namespace /wp-json/wc-melhor-envio/v1):
//
//	GET  /labels/prepare/{order_id}  — monta payload a partir do banco; retorna para preview
//	POST /labels/emit/{order_id}     — monta e emite etiqueta em 1 clique
//
// order_id = sz_orders.id (ID portal, NÃO wp_order_id). A montagem busca:
//
//	from_cep / from_address : senderzz_options['woocommerce_wc-melhor-envio_settings']['address']
//	to_cep   / to_address   : sz_order_addresses WHERE order_id=$1 AND tipo='shipping'
//	service_id              : tp_preferida_map[class_id][0] (primeiro preferido) ou 0 (ME escolhe)
//	products                : sz_order_items JOIN sz_products (altura/largura/comprimento/peso)
//
// Autenticação: JWT (middleware.AuthJWT).
// Ownership : sz_orders.produtor_id = JWT caller ID.
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/senderzz/labels-service/internal/httpx"
	"github.com/senderzz/labels-service/internal/jobs"
	"github.com/senderzz/labels-service/internal/me"
	"github.com/senderzz/labels-service/internal/middleware"
	"github.com/shopspring/decimal"
)

// orderLabelData é o payload pré-montado devolvido por PrepareOrderLabel.
type orderLabelData struct {
	PortalOrderID int64            `json:"portal_order_id"`
	WCOrderID     int64            `json:"wc_order_id"`
	ServiceID     int              `json:"service_id"`
	FromCEP       string           `json:"from_cep"`
	ToCEP         string           `json:"to_cep"`
	FromAddress   me.MEAddress     `json:"from_address"`
	ToAddress     me.MEAddress     `json:"to_address"`
	Products      []me.CalcProduct `json:"products"`
	Warnings      []string         `json:"warnings,omitempty"`
	// FreightPrice: preço TRAVADO no checkout (sz_order_meta._sz_freight_price,
	// mesma linha que fixa ServiceID acima). Emissão DEBITA/RESERVA este valor —
	// nunca recalcula via ME.Calculate — pra checkout e cobrança nunca divergirem
	// (frete fixo por transportadora, AUDIT-2026-07-27). nil = sem preço travado
	// (checkout antigo/sem meta), cai no preço do ME.Calculate como antes.
	FreightPrice *decimal.Decimal `json:"freight_price,omitempty"`
}

var labelEmitAllowedOrderStatuses = map[string]bool{
	"pending": true, "aguardando": true, "on-hold": true,
	"processing": true, "em_separacao": true, "embalado": true,
	// Estado técnico usado enquanto o botão Aprovar já iniciou a emissão.
	"em_andamento": true,
	// Alerta operacional: pedido sem etiqueta que falhou/caiu em alerta precisa
	// continuar reprocessável pela expedição.
	"cancelled": true, "cancelado": true, "frustrado": true,
}

func normalizeOrderStatusForLabel(status string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(status)), "wc-")
}

// PrepareOrderLabel monta o payload de emissão a partir do banco e retorna
// para o frontend revisar antes de confirmar (GET /labels/prepare/{order_id}).
func (h *LabelHandler) PrepareOrderLabel(w http.ResponseWriter, r *http.Request) {
	callerID := middleware.GetUserID(r.Context())
	orderID, err := parsePortalOrderID(r)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}

	data, warn, httpErr := h.assembleLabelData(r.Context(), orderID, callerID)
	if httpErr != nil {
		httpx.WriteErr(w, httpErr.code, httpErr.message)
		return
	}
	data.Warnings = warn
	httpx.WriteOK(w, map[string]any{"ok": true, "data": data})
}

// EmitOrderLabel monta e emite a etiqueta em um clique (POST /labels/emit/{order_id}).
func (h *LabelHandler) EmitOrderLabel(w http.ResponseWriter, r *http.Request) {
	callerID := middleware.GetUserID(r.Context())
	orderID, err := parsePortalOrderID(r)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}

	data, _, httpErr := h.assembleLabelData(r.Context(), orderID, callerID)
	if httpErr != nil {
		httpx.WriteErr(w, httpErr.code, httpErr.message)
		return
	}

	// AUDIT-2026-07-27: ServiceID<=0 (nenhuma modalidade preferida configurada em
	// tp_preferida_map) NÃO bloqueia mais — o fallback "pega a mais barata
	// disponível" já existe abaixo (selectedOption==nil → options[0]), mesmo
	// texto de aviso que PrepareOrderLabel já mostra. Bloquear aqui duplicava a
	// regra e travava emissão automática (auto-emit no Aprovar) só por falta de
	// preferência configurada, quando dava pra seguir sozinho.
	if data.FromCEP == "" || data.ToCEP == "" {
		httpx.WriteErr(w, http.StatusUnprocessableEntity,
			"CEP de origem ou destino ausente; configure o endereço de envio")
		return
	}
	if len(data.Products) == 0 {
		httpx.WriteErr(w, http.StatusUnprocessableEntity,
			"nenhum produto com dimensões encontrado; preencha dimensões do produto antes de emitir")
		return
	}

	req := createLabelRequest{
		WCOrderID: int(data.WCOrderID),
		ServiceID: data.ServiceID,
		FromCEP:   data.FromCEP,
		ToCEP:     data.ToCEP,
		From:      data.FromAddress,
		To:        data.ToAddress,
		Products:  data.Products,
	}

	// Enriquece CPF do destinatário (igual ao PostLabel).
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
				  WHERE o.id = $1`, data.PortalOrderID,
			).Scan(&cpfUser)
			req.To.Document = cpfUser
		}
	}
	if req.To.Document == "" {
		slog.Warn("[senderzz_labels] EmitOrderLabel: CPF ausente", "order_id", req.WCOrderID)
		httpx.WriteErr(w, http.StatusUnprocessableEntity,
			"CPF/CNPJ do destinatário é obrigatório para emissão; solicite ao cliente que informe o CPF")
		return
	}

	// Idempotência: etiqueta já existe para esse par (wc_order_id, service_id)?
	{
		var existingID int64
		var existingStatus string
		var existingShipID *string
		var existingPrice *string
		var existingOwner *int64
		idErr := h.db.QueryRow(r.Context(),
			`SELECT id, status, me_shipment_id, price, owner_user_id
			   FROM wc_me_labels
			  WHERE wc_order_id = $1 AND service_id = $2
			  ORDER BY id DESC LIMIT 1`,
			req.WCOrderID, req.ServiceID,
		).Scan(&existingID, &existingStatus, &existingShipID, &existingPrice, &existingOwner)
		if idErr == nil && existingID > 0 {
			if existingOwner == nil || *existingOwner != callerID {
				httpx.WriteErr(w, http.StatusNotFound, "pedido não encontrado")
				return
			}
			if labelBlocksReissue(existingStatus) {
				priceOut := ""
				if existingPrice != nil {
					priceOut = *existingPrice
				}
				shipOut := ""
				if existingShipID != nil {
					shipOut = *existingShipID
				}
				httpx.WriteOK(w, map[string]any{
					"label_id":    existingID,
					"shipment_id": shipOut,
					"status":      existingStatus,
					"price":       priceOut,
					"duplicate":   true,
				})
				return
			}
		} else if idErr != nil && idErr != pgx.ErrNoRows {
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao verificar etiqueta existente")
			return
		}
	}

	// Calculate frete via ME.
	calcReq := me.CalcRequest{
		FromCEP:  req.FromCEP,
		ToCEP:    req.ToCEP,
		Products: req.Products,
	}
	options, err := h.me.Calculate(r.Context(), calcReq)
	if err != nil {
		slog.Error("[senderzz_labels] EmitOrderLabel: falha ao calcular frete",
			"order_id", req.WCOrderID, "err", err)
		httpx.WriteErr(w, http.StatusBadGateway, "falha ao calcular frete via Melhor Envio")
		return
	}

	// AUDIT-2026-07-28 (dono): "diversos pedidos feito via api caíram no
	// Correios ao invés de ir para o mais barato respeitando a regra do
	// perfil (Correio bloqueado)". A regra "bloqueio_correios" só era aplicada
	// em go/orders (checkout nativo → grava _sz_freight_id); pedido que chega
	// aqui SEM esse meta (import/API que não passou pelo checkout) caía direto
	// no fallback tp_preferida_map ou options[0] cru — nenhum dos dois sabe
	// dessa regra. Aplicamos aqui em defesa (módulos não se importam entre si,
	// convenção do projeto): se o produtor bloqueia Correios, essas opções nem
	// entram na disputa de serviceID/fallback abaixo.
	if h.producerBloqueiaCorreios(r.Context(), callerID) {
		options = filterOutCorreios(options)
		if len(options) == 0 {
			httpx.WriteErr(w, http.StatusUnprocessableEntity,
				"nenhuma modalidade de frete disponível para o CEP de destino (Correios bloqueado no perfil)")
			return
		}
	}

	var selectedOption *me.ServiceOption
	for i := range options {
		if options[i].ServiceID == req.ServiceID {
			selectedOption = &options[i]
			break
		}
	}
	if selectedOption == nil {
		// Se serviço preferido indisponível para o trecho, tenta o mais barato
		// disponível de fato — options[] vem na ordem que a ME devolveu (NÃO
		// ordenada por preço), então precisa comparar, não só pegar [0].
		if len(options) > 0 {
			cheapest := &options[0]
			for i := range options {
				if options[i].Price.LessThan(cheapest.Price) {
					cheapest = &options[i]
				}
			}
			selectedOption = cheapest
			req.ServiceID = selectedOption.ServiceID
		} else {
			httpx.WriteErr(w, http.StatusUnprocessableEntity,
				"nenhuma modalidade de frete disponível para o CEP de destino")
			return
		}
	}

	// AUDIT-2026-07-28 (dono, pedido 1660/Cleni): a ME às vezes ACEITA um
	// serviço na cotação (Calculate) e RECUSA o mesmo serviço na criação real
	// (CreateShipment: "Transportadora não atende este trecho") — inconsistência
	// do lado da ME, não um erro nosso. Autorizado pelo dono: "tentar todas as
	// transportadoras privadas até esgotar" — monta a lista de candidatos
	// (preferido/travado do checkout primeiro, depois os demais do mais barato
	// pro mais caro) e tenta criar o envio em cada um até um dar certo ou
	// esgotar a lista. Cada candidato tem sua PRÓPRIA reserva de saldo
	// (referencia por serviço) — reserva liberada antes de tentar o próximo,
	// nunca duas reservas vivas ao mesmo tempo pro mesmo pedido.
	candidates := make([]me.ServiceOption, 0, len(options))
	candidates = append(candidates, *selectedOption)
	rest := make([]me.ServiceOption, 0, len(options))
	for _, o := range options {
		if o.ServiceID != selectedOption.ServiceID {
			rest = append(rest, o)
		}
	}
	sort.Slice(rest, func(i, j int) bool { return rest[i].Price.LessThan(rest[j].Price) })
	candidates = append(candidates, rest...)

	req.Options.InsuranceValue = sumProductsInsuranceValue(req.Products)
	volumes := req.Volumes
	if len(volumes) == 0 {
		// AUDIT-2026-07-27: emissão 1-click (auto-emit no Aprovar) nunca populava
		// "volumes" — campo OBRIGATÓRIO da API ME, separado de "products" (esse só
		// carrega valor/quantidade p/ seguro). Sem volumes, a ME recebe dimensões
		// zeradas e recusa com 422 ("peso deve ser entre 0.01 e 1000" etc.) mesmo
		// com produto corretamente dimensionado no banco (visto no pedido #1633).
		// Deriva 1 volume por linha de produto, mesmas dimensões já calculadas.
		volumes = buildMEVolumes(req.Products)
	}

	// Preço FIXO cobrado do produtor, igual pra qualquer transportadora que
	// acabe sendo usada (checkout promete um valor fechado ao cliente/produtor;
	// FALK absorve a diferença pro custo real da ME, seja qual for). Trava no
	// preço do checkout se existir; senão, no preço do candidato originalmente
	// selecionado (primeira cotação, antes de qualquer fallback). NUNCA usa o
	// preço real de cada transportadora tentada — achado ao vivo (pedido 1660):
	// fallback pra 2ª transportadora cobrou R$26,90 (preço real dela) em vez de
	// manter o R$30 fixo, quebrando a promessa de custo estável pro produtor.
	fixedPrice := selectedOption.Price
	if data.FreightPrice != nil {
		fixedPrice = *data.FreightPrice
	}

	var reservaTxID int64
	var debited bool
	var shipmentID string
	var serverPrice decimal.Decimal
	var lastErr error
	tried := 0
	for i := range candidates {
		cand := &candidates[i]

		candPrice := fixedPrice
		// AD-HOC (2026-08-27, pedido do owner): user 51 (sac@gestao.io) não tem
		// margem de lucro no frete — sempre reserva/debita o valor BRUTO real da
		// transportadora (cand.Price), não o fixedPrice com markup do checkout.
		// Hardcoded pra esse user_id só.
		if callerID == 51 {
			candPrice = cand.Price
		}
		// AUDIT-2026-07-28 (achado ao vivo, pedido 1660): a ME às vezes devolve
		// opção com price<=0 (serviço sem cotação real pro trecho) — indica
		// candidato sem cotação válida pro trecho, mesmo cobrando o preço FIXO
		// do produtor (candPrice acima). Checa o preço REAL da cotação
		// (cand.Price), não o fixo, senão nunca pula candidato inválido.
		if cand.Price.LessThanOrEqual(decimal.Zero) {
			slog.Warn("[senderzz_labels] EmitOrderLabel: candidato com preço inválido, pulando",
				"order_id", req.WCOrderID, "service_id", cand.ServiceID, "price", cand.Price.String())
			continue
		}
		tried++

		reservaRef := fmt.Sprintf("label_order_%d_svc_%d", req.WCOrderID, cand.ServiceID)
		reservaDesc := fmt.Sprintf("Frete etiqueta pedido #%d (serviço %d)", req.WCOrderID, cand.ServiceID)
		var candReservaTxID int64
		if h.wallet != nil && h.wallet.Enabled() {
			txID, reservaErr := h.wallet.Reservar(r.Context(), callerID, candPrice, reservaDesc, reservaRef, int64(req.WCOrderID))
			if reservaErr != nil {
				slog.Error("[senderzz_labels] EmitOrderLabel: falha ao reservar saldo",
					"order_id", req.WCOrderID, "service_id", cand.ServiceID, "err", reservaErr)
				httpx.WriteErr(w, http.StatusPaymentRequired,
					"saldo insuficiente ou indisponível para emissão da etiqueta")
				return
			}
			candReservaTxID = txID
		}

		// AUDIT-2026-07-27: saldo REAL da conta pool na ME pode estar defasado do
		// que a carteira do produtor (TPC, interna) mostra — estornos de
		// cancelamento ficam "aguardando análise" do lado da ME por um tempo antes
		// de voltar pro saldo real. Falha ANTES de gastar/debitar qualquer coisa.
		if meBalance, balErr := h.me.GetBalance(r.Context()); balErr == nil {
			if meBalance.LessThan(cand.Price) {
				slog.Error("[senderzz_labels] EmitOrderLabel: saldo real da ME insuficiente",
					"order_id", req.WCOrderID, "me_balance", meBalance.String(), "preciso", cand.Price.String())
				h.liberarReservaRollback(candReservaTxID, req.WCOrderID)
				httpx.WriteErr(w, http.StatusPaymentRequired,
					"saldo da conta Melhor Envio insuficiente no momento (pode estar aguardando análise de estorno); tente novamente em instantes ou recarregue via PIX")
				return
			}
		} else {
			slog.Warn("[senderzz_labels] EmitOrderLabel: não foi possível checar saldo real da ME antes de emitir",
				"order_id", req.WCOrderID, "err", balErr)
		}

		meOrder := me.MEOrder{
			ServiceID: cand.ServiceID,
			From:      req.From,
			To:        req.To,
			Products:  buildMEProducts(req.Products),
			Volumes:   volumes,
			Options:   req.Options,
		}
		sid, shipErr := h.me.CreateShipment(r.Context(), meOrder)
		if shipErr != nil {
			slog.Error("[senderzz_labels] EmitOrderLabel: falha ao criar shipment no ME, tentando próxima transportadora",
				"order_id", req.WCOrderID, "service_id", cand.ServiceID, "candidato", i+1, "total_candidatos", len(candidates), "err", shipErr)
			h.liberarReservaRollback(candReservaTxID, req.WCOrderID)
			lastErr = shipErr
			continue
		}

		// Sucesso — fixa os valores deste candidato pro resto da função.
		selectedOption = cand
		req.ServiceID = cand.ServiceID
		serverPrice = candPrice
		reservaTxID = candReservaTxID
		shipmentID = sid
		lastErr = nil
		break
	}
	if lastErr != nil || shipmentID == "" {
		msg := "falha ao criar pedido no Melhor Envio"
		if lastErr != nil {
			// AUDIT-2026-07-28 (dono, pedido 1660/Cleni): mensagem genérica
			// escondia o motivo real — texto de rota/capacidade de
			// transportadora é seguro de expor (não é dado financeiro interno).
			msg += ": " + meErrorDetail(lastErr)
		}
		if tried > 1 {
			msg += fmt.Sprintf(" (tentadas %d transportadoras)", tried)
		}
		httpx.WriteErr(w, http.StatusBadGateway, msg)
		return
	}
	defer func() {
		if !debited {
			h.liberarReservaRollback(reservaTxID, req.WCOrderID)
		}
	}()

	// AUDIT-2026-07-30 (dono): "ajusta para mostrar a que realmente foi emitida
	// e sempre usar a cotação do momento da solicitação do pedido" — snapshot
	// de TODAS as opções cotadas nesta emissão (options, antes do filtro de
	// candidatos), pra GetFreightQuotes (freight_quotes.go) parar de recotar
	// AO VIVO (preço muda dia a dia) e mostrar o que existia de fato na hora.
	quotesSnapshot, _ := json.Marshal(options)

	// INSERT em wc_me_labels.
	var labelID int64
	insertErr := h.db.QueryRow(r.Context(),
		`INSERT INTO wc_me_labels
		     (wc_order_id, me_shipment_id, status, service_id, service_name, company_name,
		      price, from_cep, to_cep, owner_user_id, quotes_snapshot, created_at, updated_at)
		 VALUES ($1, $2, 'draft', $3, $4, $5, $6, $7, $8, $9, $10, NOW(), NOW())
		 RETURNING id`,
		req.WCOrderID,
		shipmentID,
		req.ServiceID,
		selectedOption.Name,
		selectedOption.Company.Name,
		serverPrice.StringFixed(2),
		req.FromCEP,
		req.ToCEP,
		nullableOwner(callerID),
		quotesSnapshot,
	).Scan(&labelID)
	if insertErr != nil {
		slog.Error("[senderzz_labels] EmitOrderLabel: falha ao inserir etiqueta",
			"order_id", req.WCOrderID, "err", insertErr)
		cancelCtx, cancelCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelCancel()
		_ = h.me.CancelShipment(cancelCtx, shipmentID)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao registrar etiqueta")
		return
	}

	// Débito de saldo (CRIT-01).
	if h.wallet != nil && h.wallet.Enabled() && reservaTxID > 0 {
		debitCtx, debitCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer debitCancel()
		if debitErr := h.wallet.DebitarReserva(debitCtx, reservaTxID,
			fmt.Sprintf("Etiqueta #%d emitida (pedido #%d)", labelID, req.WCOrderID)); debitErr != nil {
			slog.Error("[senderzz_labels] EmitOrderLabel: falha ao debitar reserva (CRIT-01)",
				"label_id", labelID, "reserva_tx_id", reservaTxID, "err", debitErr)
		} else {
			debited = true
		}
	} else {
		debited = true
	}

	// Enfileira geração do PDF.
	if enqErr := jobs.EnqueueGeneratePDF(h.queue, labelID, shipmentID); enqErr != nil {
		slog.Warn("[senderzz_labels] EmitOrderLabel: falha ao enfileirar PDF",
			"label_id", labelID, "err", enqErr)
	}

	slog.Info("[senderzz_labels] etiqueta emitida (1-click)",
		"label_id", labelID,
		"order_id", req.WCOrderID,
		"service_id", req.ServiceID,
		"price", serverPrice.StringFixed(2),
	)
	httpx.WriteOK(w, map[string]any{
		"label_id":     labelID,
		"shipment_id":  shipmentID,
		"status":       "draft",
		"price":        serverPrice.StringFixed(2),
		"service_id":   req.ServiceID,
		"service_name": selectedOption.Name,
	})
}

// ── helpers ──────────────────────────────────────────────────────────────────

type httpEmitError struct {
	code    int
	message string
}

// assembleLabelData monta os dados de emissão a partir do banco.
// Retorna warnings para campos ausentes mas não bloqueantes.
// producerBloqueiaCorreios espelha go/orders/internal/handlers/freight.go::
// producerBloqueiaCorreios (settings->>'bloqueio_correios' em
// senderzz_portal_users) — mesma coluna, sem migration própria, mesmo padrão.
// Não importamos cross-módulo (convenção do projeto): duplicado aqui de propósito.
// meErrorDetail extrai o campo "error" do corpo JSON que a ME devolve em
// falhas (embutido dentro do texto de erro Go por CreateShipment/etc, formato
// "...: ME retornou status 500: {\"error\":\"...\"}"). Sem isso, o texto real
// (ex.: "Transportadora não atende este trecho") ficava só no log, nunca na
// resposta HTTP — admin/produtor via mensagem genérica sem saber o motivo.
func meErrorDetail(err error) string {
	msg := err.Error()
	idx := strings.Index(msg, "{")
	if idx < 0 {
		return msg
	}
	var parsed struct {
		Error string `json:"error"`
	}
	if jErr := json.Unmarshal([]byte(msg[idx:]), &parsed); jErr == nil && parsed.Error != "" {
		return parsed.Error
	}
	return msg
}

func (h *LabelHandler) producerBloqueiaCorreios(ctx context.Context, producerID int64) bool {
	var bloqueia bool
	_ = h.db.QueryRow(ctx,
		`SELECT COALESCE((settings->>'bloqueio_correios')::boolean, false)
		   FROM senderzz_portal_users WHERE id = $1`,
		producerID,
	).Scan(&bloqueia)
	return bloqueia
}

// isCorreiosCompany detecta a transportadora pelo nome (mesma heurística de
// go/orders/internal/handlers/freight.go::isCorreios).
func isCorreiosCompany(name string) bool {
	return strings.Contains(strings.ToUpper(name), "CORREIOS")
}

func filterOutCorreios(options []me.ServiceOption) []me.ServiceOption {
	out := make([]me.ServiceOption, 0, len(options))
	for _, o := range options {
		if isCorreiosCompany(o.Company.Name) {
			continue
		}
		out = append(out, o)
	}
	return out
}

func (h *LabelHandler) assembleLabelData(
	ctx context.Context, portalOrderID int64, callerID int64,
) (*orderLabelData, []string, *httpEmitError) {
	var warnings []string

	// 1. Ownership + wc_order_id (chave usada em wc_me_labels/sz_order_meta).
	// AUDIT-2026-07-27: pedido NATIVO Falk (sem WooCommerce) tem wp_order_id NULL —
	// Scan direto em int64 quebrava com 500 genérico "erro ao buscar pedido" pra
	// TODO pedido nativo (ex.: #1636, produtor reportou "não consegue emitir
	// etiqueta"). COALESCE(wp_order_id, id) = mesmo surrogate usado em todo o
	// resto do código (orders.go, expedicao_orders.go, me_webhook.go).
	var wpOrderID int64
	var orderStatus string
	err := h.db.QueryRow(ctx,
		`SELECT COALESCE(wp_order_id, id), COALESCE(status, '') FROM sz_orders
		  WHERE id = $1 AND produtor_id = $2`,
		portalOrderID, callerID,
	).Scan(&wpOrderID, &orderStatus)
	if err == pgx.ErrNoRows {
		return nil, nil, &httpEmitError{http.StatusNotFound, "pedido não encontrado"}
	}
	if err != nil {
		return nil, nil, &httpEmitError{http.StatusInternalServerError, "erro ao buscar pedido"}
	}
	if !labelEmitAllowedOrderStatuses[normalizeOrderStatusForLabel(orderStatus)] {
		return nil, nil, &httpEmitError{http.StatusConflict, "pedido precisa estar pendente, aprovado ou separado pra emitir etiqueta"}
	}

	// 2. Endereço de destino (shipping)
	var toAddr me.MEAddress
	var toCEP string
	{
		var nome, email, tel, logradouro, numero, complemento, bairro, cidade, uf string
		var cep *string
		aErr := h.db.QueryRow(ctx,
			`SELECT COALESCE(nome,''), COALESCE(email,''), COALESCE(telefone,''),
			        cep, COALESCE(logradouro,''), COALESCE(numero,''),
			        COALESCE(complemento,''), COALESCE(bairro,''), COALESCE(cidade,''), COALESCE(uf,'')
			   FROM sz_order_addresses
			  WHERE order_id = $1 AND tipo = 'shipping'
			  LIMIT 1`,
			portalOrderID,
		).Scan(&nome, &email, &tel, &cep, &logradouro, &numero, &complemento, &bairro, &cidade, &uf)
		if aErr == nil {
			if cep != nil {
				toCEP = strings.ReplaceAll(*cep, "-", "")
			}
			toAddr = me.MEAddress{
				Name:       nome,
				Email:      email,
				Phone:      tel,
				Address:    logradouro,
				Number:     numero,
				Complement: complemento,
				District:   bairro,
				City:       cidade,
				StateAbbr:  uf,
				CountryID:  "BR",
				PostalCode: toCEP,
			}
		} else if aErr != pgx.ErrNoRows {
			warnings = append(warnings, "não foi possível buscar endereço de destino")
		} else {
			warnings = append(warnings, "endereço de destino não encontrado")
		}
	}

	// 3. Endereço de origem (senderzz_options['woocommerce_wc-melhor-envio_settings'])
	var fromCEP string
	var fromAddr me.MEAddress
	{
		var rawOpt string
		_ = h.db.QueryRow(ctx,
			`SELECT value FROM senderzz_options WHERE name='woocommerce_wc-melhor-envio_settings' LIMIT 1`,
		).Scan(&rawOpt)
		if rawOpt != "" {
			var meSettings struct {
				Address struct {
					Name            string `json:"name"`
					Document        string `json:"document"`
					CompanyDocument string `json:"company_document"`
					Phone           string `json:"phone"`
					Email           string `json:"email"`
					Address         string `json:"address"`
					Number          string `json:"number"`
					Complement      string `json:"complement"`
					District        string `json:"district"`
					City            string `json:"city"`
					StateAbbr       string `json:"state_abbr"`
					PostalCode      string `json:"postal_code"`
				} `json:"address"`
			}
			if jErr := json.Unmarshal([]byte(rawOpt), &meSettings); jErr == nil {
				fromCEP = strings.ReplaceAll(meSettings.Address.PostalCode, "-", "")
				doc := meSettings.Address.Document
				if doc == "" {
					doc = meSettings.Address.CompanyDocument
				}
				fromAddr = me.MEAddress{
					Name:       meSettings.Address.Name,
					Document:   doc,
					Phone:      meSettings.Address.Phone,
					Email:      meSettings.Address.Email,
					Address:    meSettings.Address.Address,
					Number:     meSettings.Address.Number,
					Complement: meSettings.Address.Complement,
					District:   meSettings.Address.District,
					City:       meSettings.Address.City,
					StateAbbr:  meSettings.Address.StateAbbr,
					CountryID:  "BR",
					PostalCode: fromCEP,
				}
			}
		}
		if fromCEP == "" {
			warnings = append(warnings, "CEP de origem não configurado em Frete → Melhor Envio")
		}
	}

	// 4. Service ID preferido
	serviceID := 0
	{
		// AUDIT-2026-07-27: PRIORIDADE MÁXIMA — o serviço já escolhido/cotado no
		// CHECKOUT (sz_order_meta._sz_freight_id, gravado em go/orders/checkout.go
		// no momento da compra, mesma fonte que expedicao_orders.go usa pra exibir
		// a coluna "Transportadora"). Sem isso, o fallback "pega a mais barata
		// disponível" (abaixo) reconsultava preço/transportadora NA HORA da
		// emissão — podendo escolher operadora e valor DIFERENTES do que foi
		// cotado/prometido ao cliente no checkout (pedido #1633: cliente viu
		// Jadlog R$16,93/20,89 na tela, etiqueta saiu Correios PAC R$28,54 —
		// carrier e preço errados, "descontou errado" — dono flagrou).
		var freightIDRaw string
		_ = h.db.QueryRow(ctx,
			`SELECT meta_value FROM sz_order_meta
			  WHERE order_id = $1 AND meta_key = '_sz_freight_id' LIMIT 1`,
			portalOrderID,
		).Scan(&freightIDRaw)
		if fid, convErr := strconv.Atoi(strings.TrimSpace(freightIDRaw)); convErr == nil && fid > 0 {
			serviceID = fid
		}

		// Fallback (só quando o checkout não gravou _sz_freight_id): preferência
		// de classe de envio do produtor.
		if serviceID == 0 {
			var classID int
			_ = h.db.QueryRow(ctx,
				`SELECT COALESCE(shipping_class_id, 0) FROM senderzz_portal_users WHERE id = $1`,
				callerID,
			).Scan(&classID)
			if classID > 0 {
				var prefRaw string
				_ = h.db.QueryRow(ctx,
					`SELECT value FROM senderzz_options WHERE name='tp_preferida_map' LIMIT 1`,
				).Scan(&prefRaw)
				if prefRaw != "" {
					// prefMap: {class_id: {permitidas: [id1, id2, ...]}}
					var prefMap map[string]struct {
						Permitidas []int `json:"permitidas"`
					}
					if jErr := json.Unmarshal([]byte(prefRaw), &prefMap); jErr == nil {
						key := strconv.Itoa(classID)
						if entry, ok := prefMap[key]; ok && len(entry.Permitidas) > 0 {
							serviceID = entry.Permitidas[0]
						}
					}
				}
			}
		}
		if serviceID == 0 {
			warnings = append(warnings, "nenhuma modalidade de frete preferida configurada; a mais barata disponível será escolhida automaticamente")
		}
	}

	// 4b. Preço travado no checkout (mesma meta do serviceID acima). SÓ sobrescreve
	// o débito da carteira quando _sz_freight_locked='1' (produtor com frete fixo
	// por transportadora e/ou bloqueio de Correios — freight.go applyFixedFreight/
	// applyCorreiosLock). AUDIT-2026-07-27: _sz_freight_price é o preço MOSTRADO AO
	// CLIENTE (já com markup, quando não travado) — usar ele como débito pra TODO
	// produtor causava overcharge sistêmico (carteira debitando o preço com margem
	// em vez do custo real da ME). Sem trava, a carteira volta a usar o preço RAW
	// re-cotado na ME (serverPrice = selectedOption.Price, abaixo) — igual sempre foi.
	var freightPrice *decimal.Decimal
	{
		var priceRaw, lockedRaw string
		_ = h.db.QueryRow(ctx,
			`SELECT
			    COALESCE((SELECT meta_value FROM sz_order_meta WHERE order_id=$1 AND meta_key='_sz_freight_price' LIMIT 1), ''),
			    COALESCE((SELECT meta_value FROM sz_order_meta WHERE order_id=$1 AND meta_key='_sz_freight_locked' LIMIT 1), '')`,
			portalOrderID,
		).Scan(&priceRaw, &lockedRaw)
		if strings.TrimSpace(lockedRaw) == "1" {
			if p, convErr := decimal.NewFromString(strings.TrimSpace(priceRaw)); convErr == nil && p.GreaterThan(decimal.Zero) {
				freightPrice = &p
			}
		}
	}

	// 5. Produtos com dimensões
	var products []me.CalcProduct
	{
		// BUG-FIX 2026-07-28: InsuranceValue vinha de oi.preco_unit (preço de VENDA
		// ao cliente) — segurava a mercadoria pelo preço de venda, não pelo custo
		// real do produtor. Troca pra p.custo; sem custo cadastrado (NULL ou 0),
		// usa piso de R$10/unidade (nunca poderia vir vazio pra ME, e nunca deve
		// usar o preço de venda como substituto).
		rows, qErr := h.db.Query(ctx,
			`SELECT oi.quantidade,
			        COALESCE(p.peso, 0)::float,
			        COALESCE(p.altura, 0)::float,
			        COALESCE(p.largura, 0)::float,
			        COALESCE(p.comprimento, 0)::float,
			        COALESCE(NULLIF(p.custo, 0), 10)::float
			   FROM sz_order_items oi
			   JOIN sz_products p ON p.id = oi.produto_id
			  WHERE oi.order_id = $1`,
			portalOrderID,
		)
		if qErr == nil {
			defer rows.Close()
			for rows.Next() {
				var qty int
				var weight, height, width, length, custo float64
				if sErr := rows.Scan(&qty, &weight, &height, &width, &length, &custo); sErr != nil {
					continue
				}
				if weight <= 0 || height <= 0 || width <= 0 || length <= 0 {
					warnings = append(warnings, "produto sem dimensões completas; preencha as dimensões no cadastro do produto")
					continue
				}
				w2, h2, wd2, l2 := normalizeDims(weight, height, width, length)
				products = append(products, me.CalcProduct{
					Weight:         w2,
					Height:         h2,
					Width:          wd2,
					Length:         l2,
					Quantity:       qty,
					InsuranceValue: decimal.NewFromFloat(custo),
				})
			}
		} else {
			warnings = append(warnings, "não foi possível buscar itens do pedido")
		}
		if len(products) == 0 {
			warnings = append(warnings, "nenhum produto com dimensões encontrado")
		}
	}

	return &orderLabelData{
		PortalOrderID: portalOrderID,
		WCOrderID:     wpOrderID,
		ServiceID:     serviceID,
		FromCEP:       fromCEP,
		ToCEP:         toCEP,
		FromAddress:   fromAddr,
		ToAddress:     toAddr,
		Products:      products,
		FreightPrice:  freightPrice,
	}, warnings, nil
}

func parsePortalOrderID(r *http.Request) (int64, error) {
	raw := chi.URLParam(r, "order_id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("order_id inválido")
	}
	return id, nil
}
