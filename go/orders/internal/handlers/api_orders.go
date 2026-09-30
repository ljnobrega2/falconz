// api_orders.go — recebimento de pedidos via API (menu Integrações do portal).
//
//	POST /api/v1/orders
//	  Header: Authorization: Bearer <api_key>
//
// Autenticação por API key (não JWT) — 1 chave ativa por produtor, gerada via
// POST /portal/integrations/rotate (portal-service). O produtor da plataforma
// externa autentica com a própria chave; producer_id NUNCA vem do payload,
// sempre resolvido da chave (produtor A não pode faturar pedido em nome de B).
//
// Pedido entra SEMPRE como status='pending' — decisão do dono (2026-07-28):
// fica pro produtor aprovar/emitir etiqueta no painel normal (mesmo fluxo já
// existente pra Expedição), sem mexer em saldo/carteira ME automaticamente.
//
// Idempotência: external_order_id é OBRIGATÓRIO e único por produtor
// (UNIQUE(produtor_id, external_order_id) — migration 510). Reenvio com o
// mesmo external_order_id (retry de timeout da plataforma do cliente) devolve
// o pedido já criado, nunca duplica.
package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/senderzz/orders-service/internal/freight"
	"github.com/senderzz/orders-service/internal/httpx"
)

func decimalFromString(s string) (decimal.Decimal, error) {
	return decimal.NewFromString(strings.TrimSpace(s))
}

func errResp(msg string) map[string]any {
	return map[string]any{"ok": false, "erro": msg}
}

// hashAPIKey — sha256 hex, mesmo padrão de comparação usado pra todo secret
// neste sistema (nunca compara/armazena em claro).
func hashAPIKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// resolveAPIProducer extrai o Bearer do header, resolve producer_id ativo.
// Retorna 0 e escreve a resposta de erro se a chave for ausente/inválida.
func (h *CheckoutHandler) resolveAPIProducer(w http.ResponseWriter, r *http.Request) (int64, bool) {
	authHeader := r.Header.Get("Authorization")
	tok := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	if tok == "" || tok == authHeader {
		httpx.WriteErr(w, http.StatusUnauthorized, "header Authorization: Bearer <api_key> é obrigatório")
		return 0, false
	}

	var producerID int64
	err := h.db.QueryRow(r.Context(),
		`SELECT producer_id FROM senderzz_producer_api_keys
		  WHERE token_hash = $1 AND active = true`,
		hashAPIKey(tok),
	).Scan(&producerID)
	if err == pgx.ErrNoRows {
		httpx.WriteErr(w, http.StatusUnauthorized, "chave de API inválida")
		return 0, false
	}
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao validar chave")
		return 0, false
	}

	_, _ = h.db.Exec(r.Context(),
		`UPDATE senderzz_producer_api_keys SET last_used_at = NOW() WHERE producer_id = $1`,
		producerID,
	)
	return producerID, true
}

type apiOrderCustomer struct {
	Nome     string `json:"nome"`
	CPF      string `json:"cpf"`
	Telefone string `json:"telefone"`
}

type apiOrderAddress struct {
	CEP         string `json:"cep"`
	Logradouro  string `json:"logradouro"`
	Numero      string `json:"numero"`
	Complemento string `json:"complemento"`
	Bairro      string `json:"bairro"`
	Cidade      string `json:"cidade"`
	UF          string `json:"uf"`
}

// apiOrderRequest — AUDIT-2026-07-28 (pedido dono): substitui items[] livre
// por "token" de link de checkout JÁ CADASTRADO no portal — mesmo modelo do
// checkout nativo (senderzz_checkout_links). Produto(s)/preço/composição
// SEMPRE vêm do link, nunca do payload — plataforma externa não pode inventar
// produto/preço, só "vender uma oferta que o produtor já configurou".
//
// external_order_id é OPCIONAL (pedido dono): a plataforma do cliente não
// precisa gerenciar um ID próprio — o pedido já nasce com order_number
// automático (SZ-NNNNNNN, mesma sequência do site inteiro). Se vier, ainda
// serve pra idempotência (retry de timeout não duplica); se não vier, cada
// chamada cria um pedido novo — dedup vira responsabilidade do cliente.
//
// data_entrega é OPCIONAL, só relevante quando o CEP cai em zona de entrega
// motoboy (COD): AUDIT-2026-07-28 (pedido dono) — modo é decidido AUTOMÁTICO
// pelo CEP, igual link tipo='misto' do checkout nativo. Zona cobre o CEP (com
// data ofertável) ⇒ vira pedido COD; sem cobertura ⇒ Expedição (cota frete
// normal, ignora data_entrega se vier). Dentro do modo COD, se data_entrega
// não vier ou não bater com nenhuma ofertada, a PRIMEIRA data disponível é
// escolhida automaticamente (nunca falha o pedido por falta de data).
type apiOrderRequest struct {
	Token           string           `json:"token"`
	ExternalOrderID string           `json:"external_order_id"`
	DataEntrega     string           `json:"data_entrega"`
	Customer        apiOrderCustomer `json:"customer"`
	Address         apiOrderAddress  `json:"address"`
	CustomerNote    string           `json:"customer_note"`
}

// PostAPIOrder — POST /api/v1/orders.
func (h *CheckoutHandler) PostAPIOrder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	producerID, ok := h.resolveAPIProducer(w, r)
	if !ok {
		return
	}

	var req apiOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "json inválido")
		return
	}

	resp, status := h.createAPIOrder(ctx, producerID, req)
	h.logAPIOrderAttempt(ctx, producerID, req, resp, status)
	if status >= 400 {
		httpx.WriteErr(w, status, fmt.Sprintf("%v", resp["erro"]))
		return
	}
	httpx.WriteOK(w, resp)
}

// ReprocessLastAPIOrder — POST /internal/api-orders/reprocess-last. Rota
// INTERNA (rede Docker, nunca exposta no gateway público) — o portal-service
// chama isto depois de validar via JWT que quem pediu é o produtor dono
// (producer_id vem do body, confiado porque o caller já autenticou). Busca a
// ÚLTIMA tentativa que FALHOU (ok=false) em senderzz_integration_log pra este
// produtor e reprocessa com o MESMO payload original — útil quando a falha
// foi transitória (frete indisponível, zona sem data no momento, etc).
func (h *CheckoutHandler) ReprocessLastAPIOrder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body struct {
		ProducerID int64 `json:"producer_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ProducerID <= 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "producer_id inválido")
		return
	}

	var payloadRaw []byte
	err := h.db.QueryRow(ctx,
		`SELECT payload FROM senderzz_integration_log
		  WHERE user_id = $1 AND event = 'api_order' AND (payload->>'ok')::boolean = false
		  ORDER BY id DESC LIMIT 1`,
		body.ProducerID,
	).Scan(&payloadRaw)
	if err == pgx.ErrNoRows {
		httpx.WriteErr(w, http.StatusNotFound, "nenhuma tentativa de pedido via API falhou recentemente — nada pra reprocessar")
		return
	}
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao buscar última tentativa")
		return
	}

	var logged struct {
		Request apiOrderRequest `json:"request"`
	}
	if err := json.Unmarshal(payloadRaw, &logged); err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "payload da última tentativa corrompido")
		return
	}

	resp, status := h.createAPIOrder(ctx, body.ProducerID, logged.Request)
	h.logAPIOrderAttempt(ctx, body.ProducerID, logged.Request, resp, status)
	if status >= 400 {
		httpx.WriteErr(w, status, fmt.Sprintf("%v", resp["erro"]))
		return
	}
	httpx.WriteOK(w, resp)
}

// logAPIOrderAttempt — AUDIT-2026-07-28: log de toda tentativa (sucesso ou
// falha) em senderzz_integration_log (tabela já existente, reusada — evita
// migration nova). "Reprocessar último" (portal) lê a última linha com
// ok=false e tenta de novo com o MESMO payload, sem o cliente precisar
// reenviar. Best-effort: nunca falha o pedido por causa do log.
func (h *CheckoutHandler) logAPIOrderAttempt(ctx context.Context, producerID int64, req apiOrderRequest, resp map[string]any, status int) {
	payload, err := json.Marshal(map[string]any{
		"request":  req,
		"response": resp,
		"status":   status,
		"ok":       status < 400,
	})
	if err != nil {
		return
	}
	_, _ = h.db.Exec(ctx,
		`INSERT INTO senderzz_integration_log (user_id, event, payload) VALUES ($1, 'api_order', $2)`,
		producerID, payload,
	)
}

// createAPIOrder é o núcleo de criação (extraído de PostAPIOrder pra ser
// reusado por ReprocessLastAPIOrder sem duplicar lógica — mesma lição da
// noite: 2 caminhos derivando do mesmo código, não copiados). Devolve o corpo
// da resposta (sucesso ou erro) e o status HTTP correspondente.
func (h *CheckoutHandler) createAPIOrder(ctx context.Context, producerID int64, req apiOrderRequest) (map[string]any, int) {
	lg := httpx.LoggerFrom(ctx)

	req.Token = strings.TrimSpace(req.Token)
	if req.Token == "" {
		return errResp("token é obrigatório (token do link de checkout já cadastrado no portal)"), http.StatusBadRequest
	}
	req.ExternalOrderID = strings.TrimSpace(req.ExternalOrderID)
	destCEP := onlyDigits(req.Address.CEP)
	if len(destCEP) != 8 {
		return errResp("address.cep inválido (8 dígitos)"), http.StatusBadRequest
	}
	cpfDigits := onlyDigits(req.Customer.CPF)
	if cpfDigits != "" && !isValidCPF(cpfDigits) {
		return errResp("customer.cpf inválido"), http.StatusBadRequest
	}
	phoneDigits := onlyDigits(req.Customer.Telefone)
	if phoneDigits != "" && (len(phoneDigits) < 10 || len(phoneDigits) > 11) {
		return errResp("customer.telefone inválido (DDD+número, 10 ou 11 dígitos)"), http.StatusBadRequest
	}

	// Idempotência: mesmo produtor + mesmo external_order_id → devolve o já
	// criado. Só roda quando o cliente MANDOU external_order_id — sem ele, não
	// há chave de dedup, cada chamada cria pedido novo.
	if req.ExternalOrderID != "" {
		var existingID int64
		var existingNumber string
		errIdem := h.db.QueryRow(ctx,
			`SELECT id, order_number FROM sz_orders WHERE produtor_id = $1 AND external_order_id = $2`,
			producerID, req.ExternalOrderID,
		).Scan(&existingID, &existingNumber)
		if errIdem == nil {
			return map[string]any{
				"ok":           true,
				"order_id":     existingID,
				"order_number": existingNumber,
				"status":       "pending",
				"idempotente":  true,
			}, http.StatusOK
		}
		if errIdem != pgx.ErrNoRows {
			lg.Error("[api_orders] falha na checagem de idempotência", "err", errIdem)
			return errResp("erro interno"), http.StatusInternalServerError
		}
	}

	// Link: SEMPRE do produtor da chave (token de outro produtor = 404, nunca
	// vaza existência) — preço/produto lidos do servidor, igual checkout
	// nativo (CRIT-01). tipo='motoboy' PURO exige zona coberta (senão 422);
	// tipo='misto' decide automático pelo CEP; tipo='correio' sempre Expedição.
	var (
		postID       int64
		displayValue string
		linkName     string
		linkTipo     string
		compRaw      []byte
	)
	err := h.db.QueryRow(ctx,
		`SELECT post_id, display_value, COALESCE(NULLIF(base_name,''), name), tipo, composition_items
		   FROM senderzz_checkout_links
		  WHERE token = $1 AND producer_id = $2
		  LIMIT 1`,
		req.Token, producerID,
	).Scan(&postID, &displayValue, &linkName, &linkTipo, &compRaw)
	if err == pgx.ErrNoRows {
		return errResp("link de checkout não encontrado (token inválido ou não pertence a este produtor)"), http.StatusNotFound
	}
	if err != nil {
		lg.Error("[api_orders] falha ao validar link", "err", err, "token", req.Token)
		return errResp("erro interno"), http.StatusInternalServerError
	}
	subtotal, errDV := decimalFromString(displayValue)
	if errDV != nil {
		lg.Error("[api_orders] display_value inválido", "err", errDV, "token", req.Token)
		return errResp("erro interno"), http.StatusInternalServerError
	}

	produtoID := postID
	var mappedID int64
	if errProd := h.db.QueryRow(ctx,
		`SELECT COALESCE(wp_post_id, id) FROM sz_products WHERE id = sz_resolve_product_id($1)`, postID,
	).Scan(&mappedID); errProd == nil && mappedID > 0 {
		produtoID = mappedID
	}

	// ── Decide modo (mesma regra server-side de GetResolveMode/PostOrder
	// nativo — CRIT-01, nunca confia no que veio do payload): zona cobre o CEP
	// E tem data ofertável ⇒ COD/motoboy; senão ⇒ Expedição.
	isMotoboy := strings.EqualFold(strings.TrimSpace(linkTipo), "motoboy")
	var schedZona *zoneInfo
	if strings.EqualFold(strings.TrimSpace(linkTipo), "misto") {
		if zona, errZ := resolveZonaPorCEP(ctx, h.db, destCEP); errZ == nil && zona != nil {
			if datas, errD := computeOfferableDates(ctx, h.db, zona); errD == nil && len(datas) > 0 {
				isMotoboy = true
			}
		}
	}

	var (
		best          freight.ServiceOption
		freightLocked bool
		freightPrice  = decimal.Zero
		schedData     string
		schedTipo     string
	)
	total := subtotal

	if isMotoboy {
		zona, errZ := resolveZonaPorCEP(ctx, h.db, destCEP)
		if errZ != nil {
			lg.Error("[api_orders] falha ao resolver zona", "err", errZ)
			return errResp("erro ao resolver zona de entrega"), http.StatusInternalServerError
		}
		if zona == nil {
			return errResp("fora de área de entrega motoboy pra este CEP"), http.StatusUnprocessableEntity
		}
		datas, errD := computeOfferableDates(ctx, h.db, zona)
		if errD != nil || len(datas) == 0 {
			lg.Error("[api_orders] sem datas ofertáveis", "err", errD)
			return errResp("sem datas de entrega disponíveis pra esta zona"), http.StatusUnprocessableEntity
		}
		chosen := strings.TrimSpace(req.DataEntrega)
		var matched *offeredDate
		for i := range datas {
			if datas[i].Data == chosen {
				matched = &datas[i]
				break
			}
		}
		if matched == nil {
			// Sem data válida no payload: escolhe a primeira ofertada
			// automaticamente (pedido dono: "de sugestões e já aplica" — nunca
			// falha o pedido só por falta/erro de data).
			matched = &datas[0]
		}
		schedZona = zona
		schedData = matched.Data
		if matched.Tipo == "agendamento" {
			schedTipo = "agendado"
		} else {
			schedTipo = "pre_agendado"
		}
		// Motoboy: sem frete, sem CPF obrigatório (mesma regra do checkout nativo).
	} else {
		// Expedição: CPF do destinatário é obrigatório (Correios exige, mesma
		// regra do checkout nativo — "CPF é obrigatório para envio por
		// transportadora"). Motoboy não exige (regra nativa também).
		if cpfDigits == "" {
			return errResp("customer.cpf é obrigatório pra envio por transportadora (Expedição)"), http.StatusUnprocessableEntity
		}
		// Cota frete real, pega a mais barata. Mesma resolução server-side do
		// checkout nativo (origem do CD, dimensões reais do produto quando
		// casam pelo nome).
		origemCEP := h.resolveOrigemCEP(ctx, producerID)
		pkg := h.resolvePackage(ctx, producerID, postID, linkName)
		pkg.ValorDeclarado = subtotal.InexactFloat64()
		opts, errCalc := freight.Calculate(ctx, origemCEP, destCEP, pkg)
		if errCalc != nil || len(opts) == 0 {
			return errResp("não foi possível cotar frete pro CEP informado"), http.StatusUnprocessableEntity
		}
		opts = h.applyCarrierPreferences(ctx, producerID, opts)
		opts = h.applyFixedFreight(ctx, producerID, opts)
		opts = h.applyCorreiosLock(ctx, producerID, opts)
		if len(opts) == 0 {
			return errResp("nenhuma transportadora disponível pro CEP informado"), http.StatusUnprocessableEntity
		}
		best = opts[0]
		for _, o := range opts {
			if o.Price < best.Price {
				best = o
			}
		}
		freightLocked = best.Locked
		freightPrice = decimal.NewFromFloat(best.Price).Round(2)
		total = subtotal.Add(freightPrice)
	}

	tx, err := h.db.Begin(ctx)
	if err != nil {
		return errResp("erro interno"), http.StatusInternalServerError
	}
	defer tx.Rollback(ctx)

	var nextVal int64
	if err := tx.QueryRow(ctx, `SELECT nextval(pg_get_serial_sequence('sz_orders', 'id'))`).Scan(&nextVal); err != nil {
		lg.Error("[api_orders] falha ao gerar order_number", "err", err)
		return errResp("erro interno"), http.StatusInternalServerError
	}
	orderNumber := fmt.Sprintf("SZ-%07d", nextVal)

	// payment_method/status: COD é dinheiro na entrega (pago depois, não
	// prepago); Expedição via API pressupõe pagamento já resolvido na
	// plataforma do cliente (prepago).
	paymentMethod, paymentStatus := "api", "paid"
	if isMotoboy {
		paymentMethod, paymentStatus = "cod", "pending"
	}

	// delivery_fee: só existe pra COD/motoboy (custo operacional da entrega,
	// abatido do produtor no financeiro — "Taxa de entrega"). Mesmo valor
	// gravado em sz_motoboy_pedidos.valor_taxa (25.00) logo abaixo — fonte
	// única. Bug real encontrado ao vivo: sem isso, financeiro sempre mostrava
	// R$ 0,00 de taxa de entrega pra pedido COD via API. Expedição não usa
	// esse campo (frete real já rastreado via shipping/_sz_freight_price).
	deliveryFee := "0.00"
	if isMotoboy {
		deliveryFee = "25.00"
	}

	// AUDIT-2026-07-30 MEDIUM+CRITICAL: producer_net não era gravado aqui (ficava
	// 0 pelo default da coluna) — renda do produtor sub-reportada em toda order
	// criada via /api/v1/orders. API não tem conceito de afiliado, então a
	// fórmula reduz ao caso "sem afiliado". PAD/expedição (dono: "não tem taxa
	// de transação") NÃO desconta producer_tx_rate — só COD/motoboy desconta
	// (mesma regra aplicada em checkout.go). GREATEST(...,0) espelha checkout.go
	// pra nunca gravar negativo.
	var orderID int64
	err = tx.QueryRow(ctx,
		`INSERT INTO sz_orders
		    (id, order_number, user_id, produtor_id, external_order_id,
		     status, subtotal, shipping, total, delivery_fee, producer_net,
		     payment_method, payment_status, currency,
		     customer_note, customer_name,
		     created_at, updated_at)
		 OVERRIDING SYSTEM VALUE
		 VALUES ($1, $2, 0, $3, $4,
		         'pending', $5, $6, $7, $12,
		         CASE WHEN $13 THEN GREATEST($7::numeric - $12::numeric - ROUND($7::numeric * sz_producer_tx_rate(), 2), 0)
		              ELSE GREATEST($7::numeric - $12::numeric, 0)
		         END,
		         $10, $11, 'BRL',
		         $8, $9,
		         NOW(), NOW())
		 RETURNING id`,
		nextVal, orderNumber, producerID, nullableStr(req.ExternalOrderID),
		subtotal.StringFixed(2), freightPrice.StringFixed(2), total.StringFixed(2),
		nullableStr(strings.TrimSpace(req.CustomerNote)),
		nullableStr(strings.TrimSpace(req.Customer.Nome)),
		paymentMethod, paymentStatus,
		deliveryFee,
		isMotoboy,
	).Scan(&orderID)
	if err != nil {
		lg.Error("[api_orders] falha ao inserir pedido", "err", err)
		return errResp("erro interno"), http.StatusInternalServerError
	}

	// AUDIT-2026-07-28: mesma lógica de composição do checkout nativo
	// (checkout.go PostOrder, FEAT-CHECKOUT-MULTI-ITEM) — copiada, não
	// reimplementada do zero, pra não divergir do que o link realmente vende.
	// Sem composition_items (caso comum): 1 linha = a oferta inteira. Com
	// composition_items (2+ produtos escolhidos pelo PRODUTOR no link): 1 linha
	// por produto, subtotal rateado proporcionalmente à quantidade.
	type compLine struct {
		ProductID int64  `json:"product_id"`
		Qty       int    `json:"qty"`
		Variacao  string `json:"variacao"`
	}
	var comp []compLine
	if len(compRaw) > 0 {
		if errC := json.Unmarshal(compRaw, &comp); errC != nil {
			lg.Error("[api_orders] composition_items inválido, ignorando", "token", req.Token, "err", errC)
			comp = nil
		}
	}

	if len(comp) <= 1 {
		itemMeta, _ := json.Marshal(map[string]any{
			"checkout_link_token":   req.Token,
			"checkout_post_id":      postID,
			"api_external_order_id": req.ExternalOrderID,
		})
		_, err = tx.Exec(ctx,
			`INSERT INTO sz_order_items (order_id, produto_id, nome, quantidade, preco_unit, subtotal, meta)
			 VALUES ($1, $2, $3, 1, $4, $4, $5)`,
			orderID, produtoID, linkName, subtotal.StringFixed(2), string(itemMeta),
		)
		if err != nil {
			lg.Error("[api_orders] falha ao inserir item", "err", err)
			return errResp("erro interno"), http.StatusInternalServerError
		}
	} else {
		totalQty := 0
		for _, c := range comp {
			if c.Qty < 1 {
				c.Qty = 1
			}
			totalQty += c.Qty
		}
		if totalQty <= 0 {
			totalQty = len(comp)
		}
		unit := subtotal.Div(decimal.NewFromInt(int64(totalQty))).Round(2)
		allocated := decimal.Zero
		for i, c := range comp {
			qty := c.Qty
			if qty < 1 {
				qty = 1
			}
			lineTotal := unit.Mul(decimal.NewFromInt(int64(qty)))
			if i == len(comp)-1 {
				lineTotal = subtotal.Sub(allocated)
			}
			allocated = allocated.Add(lineTotal)

			lineProdutoID := c.ProductID
			var mapped int64
			if errL := h.db.QueryRow(ctx,
				`SELECT COALESCE(wp_post_id, id) FROM sz_products WHERE id = sz_resolve_product_id($1)`, c.ProductID,
			).Scan(&mapped); errL == nil && mapped > 0 {
				lineProdutoID = mapped
			}

			lineName := linkName
			if strings.TrimSpace(c.Variacao) != "" {
				lineName = linkName + " — " + strings.TrimSpace(c.Variacao)
			}
			lineMeta, _ := json.Marshal(map[string]any{
				"checkout_link_token": req.Token,
				"checkout_post_id":    postID,
				"variacao":            c.Variacao,
			})
			if _, err = tx.Exec(ctx,
				`INSERT INTO sz_order_items (order_id, produto_id, nome, quantidade, preco_unit, subtotal, meta)
				 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
				orderID, lineProdutoID, lineName, qty,
				lineTotal.Div(decimal.NewFromInt(int64(qty))).Round(2), lineTotal.StringFixed(2), string(lineMeta),
			); err != nil {
				lg.Error("[api_orders] falha ao inserir item da composição", "err", err)
				return errResp("erro interno"), http.StatusInternalServerError
			}
		}
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO sz_order_addresses
		    (order_id, tipo, nome, telefone, cep, logradouro, numero, complemento, bairro, cidade, uf)
		 VALUES ($1, 'shipping', $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		orderID,
		nullableStr(strings.TrimSpace(req.Customer.Nome)),
		nullableStr(phoneDigits),
		destCEP,
		nullableStr(strings.TrimSpace(req.Address.Logradouro)),
		nullableStr(strings.TrimSpace(req.Address.Numero)),
		nullableStr(strings.TrimSpace(req.Address.Complemento)),
		nullableStr(strings.TrimSpace(req.Address.Bairro)),
		nullableStr(strings.TrimSpace(req.Address.Cidade)),
		nullableStr(strings.ToUpper(strings.TrimSpace(req.Address.UF))),
	)
	if err != nil {
		lg.Error("[api_orders] falha ao inserir endereço", "err", err)
		return errResp("erro interno"), http.StatusInternalServerError
	}

	// COD/motoboy: fila operacional (sz_motoboy_pedidos), mesmo padrão do
	// checkout nativo (checkout.go, step 10) — motoboy_id nasce NULL (escolha
	// do OL/admin), valor_taxa fixo operacional 25.00. wc_order_id = order_id
	// (chave natural, range coincide 1:1 com sz_orders.id pra pedidos nativos).
	// A trigger DEFERRED de estoque (513) só vê este INSERT no COMMIT, não
	// importa se roda antes ou depois de sz_order_items — sem risco de dupla
	// reserva com o trigger nativo de sz_motoboy_pedidos.
	if isMotoboy {
		ufUpper := strings.ToUpper(strings.TrimSpace(req.Address.UF))
		_, err = tx.Exec(ctx,
			`INSERT INTO sz_motoboy_pedidos
			    (wc_order_id, cd_id, zona_id, motoboy_id, status,
			     dest_nome, dest_telefone, dest_cep,
			     dest_endereco, dest_numero, dest_complemento,
			     dest_bairro, dest_cidade, dest_uf,
			     dest_produto, quantidade,
			     valor_pedido, valor_taxa,
			     data_entrega, ts_aprovado, created_at, updated_at)
			 VALUES ($1, $2, $3, NULL, $4,
			         $5, $6, $7,
			         $8, $9, $10,
			         $11, $12, $13,
			         $14, 1,
			         $15, '25.00',
			         $16, NOW(), NOW(), NOW())
			 ON CONFLICT (wc_order_id) DO NOTHING`,
			orderID, schedZona.CDID, schedZona.ZonaID, schedTipo,
			nullableStr(strings.TrimSpace(req.Customer.Nome)),
			nullableStr(phoneDigits),
			destCEP,
			nullableStr(strings.TrimSpace(req.Address.Logradouro)),
			nullableStr(strings.TrimSpace(req.Address.Numero)),
			nullableStr(strings.TrimSpace(req.Address.Complemento)),
			nullableStr(strings.TrimSpace(req.Address.Bairro)),
			nullableStr(strings.TrimSpace(req.Address.Cidade)),
			nullableStr(ufUpper),
			nullableStr(linkName),
			total.StringFixed(2),
			schedData,
		)
		if err != nil {
			lg.Error("[api_orders] falha ao criar pedido motoboy", "err", err)
			return errResp("erro ao agendar entrega"), http.StatusInternalServerError
		}
	}

	// Meta: mesmo par _sz_freight_price/_sz_freight_locked que o checkout nativo
	// grava (emit.go de labels-service lê daqui pra decidir preço travado x
	// recotação) e _billing_cpf (padrão de todo pedido, mesmo storage-split já
	// documentado: portal lê coluna, aqui só meta mesmo — pedido não passa por
	// onboarding de portal_users). Frete só existe pro ramo Expedição — COD não
	// tem _sz_freight_* (mesma regra do checkout nativo, "motoboy: SEM frete").
	metaRows := [][2]string{
		{"_billing_cpf", onlyDigits(req.Customer.CPF)},
		{"_billing_cellphone", phoneDigits},
		{"_api_external_order_id", req.ExternalOrderID},
	}
	if !isMotoboy {
		metaRows = append(metaRows,
			[2]string{"_sz_freight_price", fmt.Sprintf("%.2f", best.Price)},
			[2]string{"_sz_freight_locked", boolToMetaFlag(freightLocked)},
			[2]string{"_sz_freight_company", best.Company},
		)
	}
	for _, kv := range metaRows {
		if kv[1] == "" {
			continue
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO sz_order_meta (order_id, meta_key, meta_value) VALUES ($1, $2, $3)`,
			orderID, kv[0], kv[1],
		)
		if err != nil {
			lg.Error("[api_orders] falha ao inserir meta", "err", err, "key", kv[0])
			return errResp("erro interno"), http.StatusInternalServerError
		}
	}

	if err := tx.Commit(ctx); err != nil {
		lg.Error("[api_orders] falha ao commitar", "err", err)
		return errResp("erro interno"), http.StatusInternalServerError
	}

	mode := "expedicao"
	if isMotoboy {
		mode = "cod"
	}
	lg.Info("[api_orders] pedido criado via API",
		"order_id", orderID, "order_number", orderNumber, "producer_id", producerID,
		"external_order_id", req.ExternalOrderID, "mode", mode)

	resp := map[string]any{
		"ok":           true,
		"order_id":     orderID,
		"order_number": orderNumber,
		"status":       "pending",
		"mode":         mode,
		"total":        total.InexactFloat64(),
	}
	if isMotoboy {
		resp["data_entrega"] = schedData
		// Exponha a classificação calculada pelo motor de agenda. O cliente
		// envia apenas data_entrega; nunca confiamos em um tipo vindo do payload.
		// `status` continua sendo o ciclo geral do pedido (pending), enquanto
		// `tipo` representa a modalidade operacional da entrega.
		resp["tipo"] = schedTipo
		resp["requer_confirmacao"] = schedTipo == "pre_agendado"
	} else {
		resp["shipping"] = best.Price
	}
	return resp, http.StatusOK
}

// isValidCPF valida dígitos verificadores (algoritmo padrão mod 11) e rejeita
// sequências repetidas (00000000000, 11111111111...) que passam no cálculo
// mas nunca são CPF real.
func isValidCPF(cpf string) bool {
	if len(cpf) != 11 {
		return false
	}
	allSame := true
	for i := 1; i < 11; i++ {
		if cpf[i] != cpf[0] {
			allSame = false
			break
		}
	}
	if allSame {
		return false
	}
	digits := make([]int, 11)
	for i, c := range cpf {
		if c < '0' || c > '9' {
			return false
		}
		digits[i] = int(c - '0')
	}
	calcCheck := func(upto int) int {
		sum := 0
		weight := upto + 1
		for i := 0; i < upto; i++ {
			sum += digits[i] * weight
			weight--
		}
		r := sum % 11
		if r < 2 {
			return 0
		}
		return 11 - r
	}
	return calcCheck(9) == digits[9] && calcCheck(10) == digits[10]
}

func boolToMetaFlag(b bool) string {
	if b {
		return "1"
	}
	return ""
}

// GetAPIZonas lista todas as zonas de entrega motoboy ativas com suas faixas
// de CEP — GET /api/v1/zonas. Mesma autenticação por API key da integração
// (Authorization: Bearer <api_key>, resolveAPIProducer) — zonas são infra
// global (por cd_id, não por produtor), então qualquer chave de produtor
// ativa serve pra consultar; o token só garante que é uma integração conhecida.
func (h *CheckoutHandler) GetAPIZonas(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.resolveAPIProducer(w, r); !ok {
		return
	}

	rows, err := h.db.Query(r.Context(),
		`SELECT z.id, z.cd_id, z.nome, COALESCE(z.descricao, ''), z.dias_funcionamento
		   FROM sz_motoboy_zonas z
		  WHERE z.ativo = TRUE
		  ORDER BY z.id`,
	)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao consultar zonas")
		return
	}
	type zonaCEP struct {
		CEPInicio string `json:"cep_inicio"`
		CEPFim    string `json:"cep_fim"`
	}
	type zonaOut struct {
		ID                int64     `json:"id"`
		CDID              int64     `json:"cd_id"`
		Nome              string    `json:"nome"`
		Descricao         string    `json:"descricao"`
		DiasFuncionamento string    `json:"dias_funcionamento"`
		FaixasCEP         []zonaCEP `json:"faixas_cep"`
	}
	var zonas []zonaOut
	ids := []int64{}
	byID := map[int64]*zonaOut{}
	for rows.Next() {
		var z zonaOut
		if err := rows.Scan(&z.ID, &z.CDID, &z.Nome, &z.Descricao, &z.DiasFuncionamento); err != nil {
			continue
		}
		z.FaixasCEP = []zonaCEP{}
		zonas = append(zonas, z)
		ids = append(ids, z.ID)
	}
	rows.Close()
	for i := range zonas {
		byID[zonas[i].ID] = &zonas[i]
	}

	if len(ids) > 0 {
		cepRows, err := h.db.Query(r.Context(),
			`SELECT zona_id, cep_inicio, cep_fim FROM sz_motoboy_cep_zonas
			  WHERE zona_id = ANY($1) ORDER BY zona_id, cep_inicio`,
			ids,
		)
		if err != nil {
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao consultar faixas de CEP")
			return
		}
		defer cepRows.Close()
		for cepRows.Next() {
			var zonaID int64
			var c zonaCEP
			if err := cepRows.Scan(&zonaID, &c.CEPInicio, &c.CEPFim); err != nil {
				continue
			}
			if z, ok := byID[zonaID]; ok {
				z.FaixasCEP = append(z.FaixasCEP, c)
			}
		}
	}

	if zonas == nil {
		zonas = []zonaOut{}
	}
	httpx.WriteOK(w, map[string]any{"ok": true, "zonas": zonas})
}
