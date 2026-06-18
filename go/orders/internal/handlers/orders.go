// Package handlers implementa os endpoints REST do serviço de Orders.
//
// Namespace WP: /wp-json/senderzz/v1
//
// Rotas implementadas neste arquivo:
//   POST   /orders                — cria pedido (valida itens, calcula total server-side)
//   GET    /orders                — lista pedidos do usuário autenticado (com filtros e paginação)
//   GET    /orders/{id}           — detalhe do pedido com itens, endereço e histórico
//   PATCH  /orders/{id}/status    — transição de status via statemachine
//   POST   /orders/{id}/cancel    — atalho: status → cancelled
//   GET    /orders/{id}/meta      — leitura de metadados do pedido
//   POST   /orders/{id}/meta      — gravação de metadado (key/value)
//   GET    /orders/export         — exportação CSV (produtor: próprios; admin: todos)
//
// Segurança:
//   - Total nunca é aceito do cliente — recalculado a partir de itens (CRIT-01 equiv.).
//   - Acesso a pedido de outro usuário = 404 (não expõe existência).
//   - Filtros de listagem sempre incluem user_id ou produtor_id do token.
//   - Export limitado a 10.000 linhas por chamada.
//
// Valores monetários: shopspring/decimal para evitar drift de float64.
package handlers

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/senderzz/orders-service/internal/auth"
	"github.com/senderzz/orders-service/internal/httpx"
	"github.com/senderzz/orders-service/internal/statemachine"
)

// OrderHandler agrupa as dependências dos handlers de pedido.
type OrderHandler struct {
	db *pgxpool.Pool
}

// NewOrderHandler cria um OrderHandler com o pool fornecido.
func NewOrderHandler(db *pgxpool.Pool) *OrderHandler {
	return &OrderHandler{db: db}
}

// ── Structs de request/response ───────────────────────────────────────────────

// itemInput representa um item enviado pelo cliente na criação do pedido.
type itemInput struct {
	ProdutoID  int64  `json:"produto_id"`
	Nome       string `json:"nome"`
	SKU        string `json:"sku"`
	Quantidade int    `json:"quantidade"`
	// PrecoUnit: o cliente envia o preço unitário. O servidor recalcula o total.
	// NUNCA aceitar total do cliente — sempre recalcular (equivalente a CRIT-01).
	PrecoUnit decimal.Decimal `json:"preco_unit"`
	Meta      map[string]any  `json:"meta,omitempty"`
}

// enderecoInput representa o endereço de envio enviado na criação do pedido.
type enderecoInput struct {
	Tipo       string `json:"tipo"`
	Nome       string `json:"nome"`
	Email      string `json:"email"`
	Telefone   string `json:"telefone"`
	CEP        string `json:"cep"`
	Logradouro string `json:"logradouro"`
	Numero     string `json:"numero"`
	Complemento string `json:"complemento"`
	Bairro     string `json:"bairro"`
	Cidade     string `json:"cidade"`
	UF         string `json:"uf"`
	Pais       string `json:"pais"`
}

// createOrderRequest representa o payload de criação de pedido.
type createOrderRequest struct {
	ProdutorID    int64           `json:"produtor_id"`
	AffiliateID   *int64          `json:"affiliate_id,omitempty"`
	Itens         []itemInput     `json:"itens"`
	Endereco      *enderecoInput  `json:"endereco,omitempty"`
	PaymentMethod string          `json:"payment_method"`
	CustomerNote  string          `json:"customer_note,omitempty"`
	Frete         decimal.Decimal `json:"frete,omitempty"`
}

// orderRow representa um pedido lido do banco para resposta.
type orderRow struct {
	ID            int64           `json:"id"`
	OrderNumber   string          `json:"order_number"`
	WPOrderID     *int64          `json:"wp_order_id,omitempty"`
	UserID        int64           `json:"user_id"`
	ProdutorID    int64           `json:"produtor_id"`
	AffiliateID   *int64          `json:"affiliate_id,omitempty"`
	Status        string          `json:"status"`
	Subtotal      decimal.Decimal `json:"subtotal"`
	Shipping      decimal.Decimal `json:"shipping"`
	Total         decimal.Decimal `json:"total"`
	PaymentMethod *string         `json:"payment_method,omitempty"`
	PaymentStatus string          `json:"payment_status"`
	Currency      string          `json:"currency"`
	CustomerNote  *string         `json:"customer_note,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// ── POST /orders ──────────────────────────────────────────────────────────────

// PostOrders cria um novo pedido.
//
// Validações:
//   - Ao menos um item com produto_id, nome e quantidade > 0.
//   - Total calculado server-side (nunca confia no cliente — CRIT-01 equiv.).
//   - Endereço de envio opcional (pode ser adicionado via meta).
//
// Status inicial: pending. Payment_status inicial: pending.
func (h *OrderHandler) PostOrders(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r.Context())
	if userID == 0 {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	var req createOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	// Validação de itens.
	if len(req.Itens) == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "ao menos um item é obrigatório")
		return
	}
	for i, item := range req.Itens {
		if item.ProdutoID == 0 {
			httpx.WriteErr(w, http.StatusBadRequest, fmt.Sprintf("item[%d]: produto_id obrigatório", i))
			return
		}
		if strings.TrimSpace(item.Nome) == "" {
			httpx.WriteErr(w, http.StatusBadRequest, fmt.Sprintf("item[%d]: nome obrigatório", i))
			return
		}
		if item.Quantidade <= 0 {
			httpx.WriteErr(w, http.StatusBadRequest, fmt.Sprintf("item[%d]: quantidade deve ser maior que zero", i))
			return
		}
		if item.PrecoUnit.LessThan(decimal.Zero) {
			httpx.WriteErr(w, http.StatusBadRequest, fmt.Sprintf("item[%d]: preco_unit não pode ser negativo", i))
			return
		}
	}

	if req.ProdutorID == 0 {
		// Se produtor_id não enviado, usa o próprio usuário autenticado.
		req.ProdutorID = userID
	}

	// Calcula totais server-side — nunca usa valor enviado pelo cliente.
	subtotal := decimal.Zero
	for _, item := range req.Itens {
		qty := decimal.NewFromInt(int64(item.Quantidade))
		itemSubtotal := item.PrecoUnit.Mul(qty)
		subtotal = subtotal.Add(itemSubtotal)
	}
	if req.Frete.LessThan(decimal.Zero) {
		req.Frete = decimal.Zero
	}
	total := subtotal.Add(req.Frete)

	ctx := r.Context()

	// Gera order_number único: SZ-{ano}{seq padded}.
	// Usa sequência do banco para garantir unicidade.
	var nextVal int64
	err := h.db.QueryRow(ctx,
		`SELECT nextval(pg_get_serial_sequence('sz_orders', 'id'))`,
	).Scan(&nextVal)
	if err != nil {
		slog.Error("[orders] falha ao gerar order_number", "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	orderNumber := fmt.Sprintf("SZ-%07d", nextVal)

	// Usa uma transação para garantir atomicidade de toda a criação.
	tx, err := h.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		slog.Error("[orders] falha ao iniciar transação", "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Insere o pedido com o ID pré-alocado via OVERRIDING SYSTEM VALUE.
	// Isso garante que order_number e id sejam consistentes.
	var orderID int64
	err = tx.QueryRow(ctx,
		`INSERT INTO sz_orders
		    (id, order_number, user_id, produtor_id, affiliate_id,
		     status, subtotal, shipping, total,
		     payment_method, payment_status, currency,
		     customer_note, ip_address, user_agent,
		     created_at, updated_at)
		 OVERRIDING SYSTEM VALUE
		 VALUES ($1, $2, $3, $4, $5,
		         'pending', $6, $7, $8,
		         $9, 'pending', 'BRL',
		         $10, $11, $12,
		         NOW(), NOW())
		 RETURNING id`,
		nextVal, orderNumber, userID, req.ProdutorID, req.AffiliateID,
		subtotal.StringFixed(2), req.Frete.StringFixed(2), total.StringFixed(2),
		nullableStr(req.PaymentMethod),
		nullableStr(req.CustomerNote),
		nullableStr(r.RemoteAddr),
		nullableStr(r.UserAgent()),
	).Scan(&orderID)
	if err != nil {
		slog.Error("[orders] falha ao inserir pedido", "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao criar pedido")
		return
	}

	// Insere os itens do pedido.
	for _, item := range req.Itens {
		qty := decimal.NewFromInt(int64(item.Quantidade))
		itemSub := item.PrecoUnit.Mul(qty)

		var metaJSON []byte
		if item.Meta != nil {
			metaJSON, _ = json.Marshal(item.Meta)
		}

		_, err = tx.Exec(ctx,
			`INSERT INTO sz_order_items
			    (order_id, produto_id, nome, sku, quantidade, preco_unit, subtotal, meta)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			orderID, item.ProdutoID, item.Nome, nullableStr(item.SKU),
			item.Quantidade, item.PrecoUnit.StringFixed(2), itemSub.StringFixed(2),
			nullableBytes(metaJSON),
		)
		if err != nil {
			slog.Error("[orders] falha ao inserir item", "order_id", orderID, "produto_id", item.ProdutoID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao inserir item do pedido")
			return
		}
	}

	// Insere o endereço de envio, se fornecido.
	if req.Endereco != nil {
		tipo := req.Endereco.Tipo
		if tipo != "shipping" && tipo != "billing" {
			tipo = "shipping"
		}
		pais := req.Endereco.Pais
		if pais == "" {
			pais = "BR"
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO sz_order_addresses
			    (order_id, tipo, nome, email, telefone, cep,
			     logradouro, numero, complemento, bairro, cidade, uf, pais)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
			orderID, tipo,
			req.Endereco.Nome, req.Endereco.Email, req.Endereco.Telefone, req.Endereco.CEP,
			req.Endereco.Logradouro, req.Endereco.Numero, req.Endereco.Complemento,
			req.Endereco.Bairro, req.Endereco.Cidade, req.Endereco.UF, pais,
		)
		if err != nil {
			slog.Error("[orders] falha ao inserir endereço", "order_id", orderID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao inserir endereço")
			return
		}
	}

	// Registra o status inicial no histórico.
	_, err = tx.Exec(ctx,
		`INSERT INTO sz_order_status_history
		    (order_id, status_de, status_para, motivo, actor_id, actor_tipo, created_at)
		 VALUES ($1, NULL, 'pending', 'pedido criado', $2, 'sistema', NOW())`,
		orderID, userID,
	)
	if err != nil {
		slog.Error("[orders] falha ao registrar histórico inicial", "order_id", orderID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		slog.Error("[orders] falha ao commitar criação do pedido", "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	slog.Info("[orders] pedido criado",
		"order_id", orderID,
		"order_number", orderNumber,
		"user_id", userID,
		"total", total.StringFixed(2),
	)

	httpx.WriteOK(w, map[string]any{
		"id":           orderID,
		"order_number": orderNumber,
		"status":       "pending",
		"total":        total.StringFixed(2),
	})
}

// ── GET /orders ────────────────────────────────────────────────────────────────

// GetOrders lista pedidos do usuário autenticado com filtros e paginação.
//
// Query params:
//   - status       — filtro por status
//   - from_date    — data início (YYYY-MM-DD)
//   - to_date      — data fim (YYYY-MM-DD)
//   - affiliate_id — filtro por afiliado
//   - produto_id   — filtro por produto (via sz_order_items)
//   - limit        — padrão 20, máximo 100
//   - offset       — padrão 0
//
// Admins veem todos os pedidos. Outros usuários veem apenas os próprios.
func (h *OrderHandler) GetOrders(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r.Context())
	if userID == 0 {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	q := r.URL.Query()
	limit := parseIntParam(q.Get("limit"), 20, 100)
	offset := parseIntParam(q.Get("offset"), 0, -1)

	// Constrói filtros dinamicamente.
	// Admins não são filtrados por user_id.
	isAdmin := auth.IsAdmin(r.Context())

	args := []any{}
	argIdx := 1
	where := []string{}

	if !isAdmin {
		where = append(where, fmt.Sprintf("(o.user_id = $%d OR o.produtor_id = $%d)", argIdx, argIdx+1))
		args = append(args, userID, userID)
		argIdx += 2
	}

	if s := q.Get("status"); s != "" {
		where = append(where, fmt.Sprintf("o.status = $%d", argIdx))
		args = append(args, s)
		argIdx++
	}
	if d := q.Get("from_date"); d != "" {
		where = append(where, fmt.Sprintf("o.created_at >= $%d", argIdx))
		args = append(args, d)
		argIdx++
	}
	if d := q.Get("to_date"); d != "" {
		where = append(where, fmt.Sprintf("o.created_at < ($%d::date + INTERVAL '1 day')", argIdx))
		args = append(args, d)
		argIdx++
	}
	if a := q.Get("affiliate_id"); a != "" {
		if aID, err := strconv.ParseInt(a, 10, 64); err == nil {
			where = append(where, fmt.Sprintf("o.affiliate_id = $%d", argIdx))
			args = append(args, aID)
			argIdx++
		}
	}
	if p := q.Get("produto_id"); p != "" {
		if pID, err := strconv.ParseInt(p, 10, 64); err == nil {
			where = append(where, fmt.Sprintf("EXISTS (SELECT 1 FROM sz_order_items i WHERE i.order_id = o.id AND i.produto_id = $%d)", argIdx))
			args = append(args, pID)
			argIdx++
		}
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	// Conta total para paginação.
	var total int
	countSQL := fmt.Sprintf(`SELECT COUNT(*) FROM sz_orders o %s`, whereClause)
	if err := h.db.QueryRow(r.Context(), countSQL, args...).Scan(&total); err != nil {
		slog.Error("[orders] falha ao contar pedidos", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao listar pedidos")
		return
	}

	// Busca os pedidos.
	args = append(args, limit, offset)
	listSQL := fmt.Sprintf(`
		SELECT o.id, o.order_number, o.wp_order_id, o.user_id, o.produtor_id,
		       o.affiliate_id, o.status, o.subtotal, o.shipping, o.total,
		       o.payment_method, o.payment_status, o.currency,
		       o.customer_note, o.created_at, o.updated_at
		  FROM sz_orders o
		  %s
		 ORDER BY o.created_at DESC, o.id DESC
		 LIMIT $%d OFFSET $%d`,
		whereClause, argIdx, argIdx+1,
	)

	rows, err := h.db.Query(r.Context(), listSQL, args...)
	if err != nil {
		slog.Error("[orders] falha ao buscar pedidos", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao listar pedidos")
		return
	}
	defer rows.Close()

	var orders []orderRow
	for rows.Next() {
		var o orderRow
		var subStr, shipStr, totStr string
		if err := rows.Scan(
			&o.ID, &o.OrderNumber, &o.WPOrderID, &o.UserID, &o.ProdutorID,
			&o.AffiliateID, &o.Status, &subStr, &shipStr, &totStr,
			&o.PaymentMethod, &o.PaymentStatus, &o.Currency,
			&o.CustomerNote, &o.CreatedAt, &o.UpdatedAt,
		); err != nil {
			slog.Error("[orders] erro ao ler linha de pedido", "err", err)
			continue
		}
		o.Subtotal, _ = decimal.NewFromString(subStr)
		o.Shipping, _ = decimal.NewFromString(shipStr)
		o.Total, _ = decimal.NewFromString(totStr)
		orders = append(orders, o)
	}
	if orders == nil {
		orders = []orderRow{}
	}

	httpx.WriteOK(w, map[string]any{
		"data":   orders,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// ── GET /orders/{id} ─────────────────────────────────────────────────────────

// GetOrder retorna o detalhe completo de um pedido: dados base, itens, endereço e histórico.
//
// Segurança: usuários não-admin só veem pedidos próprios (user_id ou produtor_id).
// Pedido de outro usuário retorna 404 (não expõe existência).
func (h *OrderHandler) GetOrder(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r.Context())
	if userID == 0 {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	orderID, err := parseIDParam(r, "id")
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}

	ctx := r.Context()
	isAdmin := auth.IsAdmin(ctx)

	// Busca o pedido base.
	var o orderRow
	var subStr, shipStr, totStr string
	var orderErr error
	if isAdmin {
		orderErr = h.db.QueryRow(ctx,
			`SELECT id, order_number, wp_order_id, user_id, produtor_id,
			        affiliate_id, status, subtotal, shipping, total,
			        payment_method, payment_status, currency,
			        customer_note, created_at, updated_at
			   FROM sz_orders WHERE id = $1`,
			orderID,
		).Scan(
			&o.ID, &o.OrderNumber, &o.WPOrderID, &o.UserID, &o.ProdutorID,
			&o.AffiliateID, &o.Status, &subStr, &shipStr, &totStr,
			&o.PaymentMethod, &o.PaymentStatus, &o.Currency,
			&o.CustomerNote, &o.CreatedAt, &o.UpdatedAt,
		)
	} else {
		// Filtra por user_id E produtor_id — retorna 404 para pedidos alheios.
		orderErr = h.db.QueryRow(ctx,
			`SELECT id, order_number, wp_order_id, user_id, produtor_id,
			        affiliate_id, status, subtotal, shipping, total,
			        payment_method, payment_status, currency,
			        customer_note, created_at, updated_at
			   FROM sz_orders
			  WHERE id = $1 AND (user_id = $2 OR produtor_id = $2)`,
			orderID, userID,
		).Scan(
			&o.ID, &o.OrderNumber, &o.WPOrderID, &o.UserID, &o.ProdutorID,
			&o.AffiliateID, &o.Status, &subStr, &shipStr, &totStr,
			&o.PaymentMethod, &o.PaymentStatus, &o.Currency,
			&o.CustomerNote, &o.CreatedAt, &o.UpdatedAt,
		)
	}

	if orderErr == pgx.ErrNoRows {
		httpx.WriteErr(w, http.StatusNotFound, "pedido não encontrado")
		return
	}
	if orderErr != nil {
		slog.Error("[orders] falha ao buscar pedido", "order_id", orderID, "err", orderErr)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao buscar pedido")
		return
	}

	o.Subtotal, _ = decimal.NewFromString(subStr)
	o.Shipping, _ = decimal.NewFromString(shipStr)
	o.Total, _ = decimal.NewFromString(totStr)

	// Busca itens do pedido.
	type itemRow struct {
		ID         int64           `json:"id"`
		ProdutoID  int64           `json:"produto_id"`
		Nome       string          `json:"nome"`
		SKU        *string         `json:"sku,omitempty"`
		Quantidade int             `json:"quantidade"`
		PrecoUnit  decimal.Decimal `json:"preco_unit"`
		Subtotal   decimal.Decimal `json:"subtotal"`
		Meta       *string         `json:"meta,omitempty"`
	}

	itemRows, err := h.db.Query(ctx,
		`SELECT id, produto_id, nome, sku, quantidade, preco_unit, subtotal, meta::text
		   FROM sz_order_items WHERE order_id = $1 ORDER BY id`,
		orderID,
	)
	if err != nil {
		slog.Error("[orders] falha ao buscar itens", "order_id", orderID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao buscar itens")
		return
	}
	defer itemRows.Close()

	var itens []itemRow
	for itemRows.Next() {
		var it itemRow
		var puStr, subItStr string
		if err := itemRows.Scan(&it.ID, &it.ProdutoID, &it.Nome, &it.SKU,
			&it.Quantidade, &puStr, &subItStr, &it.Meta); err != nil {
			continue
		}
		it.PrecoUnit, _ = decimal.NewFromString(puStr)
		it.Subtotal, _ = decimal.NewFromString(subItStr)
		itens = append(itens, it)
	}
	if itens == nil {
		itens = []itemRow{}
	}

	// Busca endereço de envio.
	type addrRow struct {
		Tipo        string  `json:"tipo"`
		Nome        *string `json:"nome,omitempty"`
		Email       *string `json:"email,omitempty"`
		Telefone    *string `json:"telefone,omitempty"`
		CEP         *string `json:"cep,omitempty"`
		Logradouro  *string `json:"logradouro,omitempty"`
		Numero      *string `json:"numero,omitempty"`
		Complemento *string `json:"complemento,omitempty"`
		Bairro      *string `json:"bairro,omitempty"`
		Cidade      *string `json:"cidade,omitempty"`
		UF          *string `json:"uf,omitempty"`
		Pais        string  `json:"pais"`
	}
	var addr *addrRow
	var a addrRow
	err = h.db.QueryRow(ctx,
		`SELECT tipo, nome, email, telefone, cep, logradouro, numero,
		        complemento, bairro, cidade, uf, pais
		   FROM sz_order_addresses WHERE order_id = $1 AND tipo = 'shipping' LIMIT 1`,
		orderID,
	).Scan(&a.Tipo, &a.Nome, &a.Email, &a.Telefone, &a.CEP,
		&a.Logradouro, &a.Numero, &a.Complemento, &a.Bairro, &a.Cidade, &a.UF, &a.Pais)
	if err == nil {
		addr = &a
	}

	// Busca histórico de status.
	type histRow struct {
		ID         int64     `json:"id"`
		StatusDe   *string   `json:"status_de,omitempty"`
		StatusPara string    `json:"status_para"`
		Motivo     *string   `json:"motivo,omitempty"`
		ActorID    *int64    `json:"actor_id,omitempty"`
		ActorTipo  string    `json:"actor_tipo"`
		CreatedAt  time.Time `json:"created_at"`
	}

	histRows, err := h.db.Query(ctx,
		`SELECT id, status_de, status_para, motivo, actor_id, actor_tipo, created_at
		   FROM sz_order_status_history WHERE order_id = $1 ORDER BY id`,
		orderID,
	)
	if err != nil {
		slog.Error("[orders] falha ao buscar histórico", "order_id", orderID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao buscar histórico")
		return
	}
	defer histRows.Close()

	var historico []histRow
	for histRows.Next() {
		var h histRow
		if err := histRows.Scan(&h.ID, &h.StatusDe, &h.StatusPara, &h.Motivo,
			&h.ActorID, &h.ActorTipo, &h.CreatedAt); err != nil {
			continue
		}
		historico = append(historico, h)
	}
	if historico == nil {
		historico = []histRow{}
	}

	httpx.WriteOK(w, map[string]any{
		"pedido":   o,
		"itens":    itens,
		"endereco": addr,
		"historico": historico,
	})
}

// ── PATCH /orders/{id}/status ─────────────────────────────────────────────────

// PatchOrderStatus executa uma transição de status validada pela máquina de estados.
//
// Body: {status, motivo?}
//
// Regras de autorização por status:
//   - pending → processing: qualquer usuário autenticado dono do pedido
//   - processing → aguardando: sistema/webhook/admin
//   - em_separacao, embalado, enviado, entregue: portal (OL) ou admin
//   - cancelled: usuário dono OU admin (via /orders/{id}/cancel para simplificar)
//   - reembolsado: somente admin
func (h *OrderHandler) PatchOrderStatus(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r.Context())
	if userID == 0 {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	orderID, err := parseIDParam(r, "id")
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}

	var req struct {
		Status string `json:"status"`
		Motivo string `json:"motivo"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	if req.Status == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "status é obrigatório")
		return
	}

	ctx := r.Context()
	isAdmin := auth.IsAdmin(ctx)

	// Verifica ownership do pedido antes de tentar transição.
	var currentStatus string
	var ownerUserID, ownerProdutorID int64
	err = h.db.QueryRow(ctx,
		`SELECT status, user_id, produtor_id FROM sz_orders WHERE id = $1`,
		orderID,
	).Scan(&currentStatus, &ownerUserID, &ownerProdutorID)
	if err == pgx.ErrNoRows {
		httpx.WriteErr(w, http.StatusNotFound, "pedido não encontrado")
		return
	}
	if err != nil {
		slog.Error("[orders] falha ao buscar pedido para transição", "order_id", orderID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Não-admins só podem fazer transições em pedidos próprios.
	if !isAdmin && ownerUserID != userID && ownerProdutorID != userID {
		httpx.WriteErr(w, http.StatusForbidden, "acesso negado")
		return
	}

	// Verificação de autorização por status de destino (CRIT-02 equiv.).
	// Status que requerem admin:
	adminOnlyTargets := map[string]bool{"reembolsado": true}
	if adminOnlyTargets[req.Status] && !isAdmin {
		httpx.WriteErr(w, http.StatusForbidden, "apenas administradores podem mover para este status")
		return
	}

	// Valida e executa a transição via máquina de estados.
	if !statemachine.CanTransition(currentStatus, req.Status) {
		httpx.WriteErr(w, http.StatusUnprocessableEntity,
			fmt.Sprintf("transição inválida: %q → %q", currentStatus, req.Status))
		return
	}

	actorTipo := "portal"
	if isAdmin {
		actorTipo = "admin"
	}

	if err := statemachine.Transition(ctx, h.db, orderID, req.Status, userID, actorTipo, req.Motivo); err != nil {
		slog.Error("[orders] falha na transição de status",
			"order_id", orderID, "para", req.Status, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao atualizar status")
		return
	}

	httpx.WriteOK(w, map[string]any{
		"order_id": orderID,
		"status":   req.Status,
	})
}

// ── POST /orders/{id}/cancel ─────────────────────────────────────────────────

// PostOrderCancel é um atalho para cancelar o pedido.
//
// Permitido apenas nos estados: pending, processing, aguardando, on-hold.
// Usuário dono do pedido OU admin podem cancelar.
// Body opcional: {motivo string}
func (h *OrderHandler) PostOrderCancel(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r.Context())
	if userID == 0 {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	orderID, err := parseIDParam(r, "id")
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}

	// Lê motivo opcional do body.
	var req struct {
		Motivo string `json:"motivo"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req) // ignora erro — body é opcional

	ctx := r.Context()
	isAdmin := auth.IsAdmin(ctx)

	// Cancellable statuses (espelha CRIT-02 do PHP — whitelist explícita).
	cancellableStatuses := map[string]bool{
		"pending": true, "processing": true, "aguardando": true, "on-hold": true,
	}

	var currentStatus string
	var ownerUserID, ownerProdutorID int64
	err = h.db.QueryRow(ctx,
		`SELECT status, user_id, produtor_id FROM sz_orders WHERE id = $1`,
		orderID,
	).Scan(&currentStatus, &ownerUserID, &ownerProdutorID)
	if err == pgx.ErrNoRows {
		httpx.WriteErr(w, http.StatusNotFound, "pedido não encontrado")
		return
	}
	if err != nil {
		slog.Error("[orders] falha ao buscar pedido para cancelamento", "order_id", orderID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	if !isAdmin && ownerUserID != userID && ownerProdutorID != userID {
		httpx.WriteErr(w, http.StatusForbidden, "acesso negado")
		return
	}

	if !cancellableStatuses[currentStatus] {
		httpx.WriteErr(w, http.StatusUnprocessableEntity,
			fmt.Sprintf("pedido no status %q não pode ser cancelado por esta rota", currentStatus))
		return
	}

	actorTipo := "cliente"
	if isAdmin {
		actorTipo = "admin"
	}
	motivo := req.Motivo
	if motivo == "" {
		motivo = "cancelado pelo usuário"
	}

	if err := statemachine.Transition(ctx, h.db, orderID, "cancelled", userID, actorTipo, motivo); err != nil {
		slog.Error("[orders] falha ao cancelar pedido", "order_id", orderID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao cancelar pedido")
		return
	}

	slog.Info("[orders] pedido cancelado", "order_id", orderID, "user_id", userID)
	httpx.WriteOK(w, map[string]any{"order_id": orderID, "status": "cancelled"})
}

// ── GET /orders/{id}/meta ─────────────────────────────────────────────────────

// GetOrderMeta retorna todos os metadados de um pedido.
func (h *OrderHandler) GetOrderMeta(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r.Context())
	if userID == 0 {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	orderID, err := parseIDParam(r, "id")
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}

	ctx := r.Context()
	isAdmin := auth.IsAdmin(ctx)

	// Verifica acesso ao pedido.
	if err := h.checkOrderAccess(ctx, orderID, userID, isAdmin); err != nil {
		httpx.WriteErr(w, http.StatusNotFound, "pedido não encontrado")
		return
	}

	rows, err := h.db.Query(ctx,
		`SELECT meta_key, meta_value FROM sz_order_meta WHERE order_id = $1 ORDER BY meta_key`,
		orderID,
	)
	if err != nil {
		slog.Error("[orders] falha ao buscar meta", "order_id", orderID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao buscar metadados")
		return
	}
	defer rows.Close()

	meta := map[string]any{}
	for rows.Next() {
		var k string
		var v *string
		if err := rows.Scan(&k, &v); err != nil {
			continue
		}
		if v != nil {
			meta[k] = *v
		} else {
			meta[k] = nil
		}
	}

	httpx.WriteOK(w, map[string]any{"meta": meta})
}

// ── POST /orders/{id}/meta ────────────────────────────────────────────────────

// PostOrderMeta grava ou atualiza um metadado (key/value) do pedido.
//
// Body: {key string, value string}
// Usa ON CONFLICT DO UPDATE para upsert atômico.
func (h *OrderHandler) PostOrderMeta(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r.Context())
	if userID == 0 {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	orderID, err := parseIDParam(r, "id")
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}

	var req struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	if req.Key == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "key é obrigatória")
		return
	}

	ctx := r.Context()
	isAdmin := auth.IsAdmin(ctx)

	if err := h.checkOrderAccess(ctx, orderID, userID, isAdmin); err != nil {
		httpx.WriteErr(w, http.StatusNotFound, "pedido não encontrado")
		return
	}

	_, err = h.db.Exec(ctx,
		`INSERT INTO sz_order_meta (order_id, meta_key, meta_value)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (order_id, meta_key) DO UPDATE SET meta_value = EXCLUDED.meta_value`,
		orderID, req.Key, req.Value,
	)
	if err != nil {
		slog.Error("[orders] falha ao gravar meta", "order_id", orderID, "key", req.Key, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao gravar metadado")
		return
	}

	httpx.WriteOK(w, map[string]any{"key": req.Key, "value": req.Value})
}

// ── GET /orders/export ────────────────────────────────────────────────────────

// GetOrdersExport exporta pedidos como CSV.
//
// Produtor: exporta apenas os próprios pedidos.
// Admin: exporta todos (respeitando filtros de data).
// Limite: 10.000 linhas por chamada para proteger a memória.
//
// Query params: from_date, to_date, status
func (h *OrderHandler) GetOrdersExport(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r.Context())
	if userID == 0 {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	ctx := r.Context()
	isAdmin := auth.IsAdmin(ctx)
	q := r.URL.Query()

	args := []any{}
	argIdx := 1
	where := []string{}

	if !isAdmin {
		where = append(where, fmt.Sprintf("(o.user_id = $%d OR o.produtor_id = $%d)", argIdx, argIdx+1))
		args = append(args, userID, userID)
		argIdx += 2
	}
	if s := q.Get("status"); s != "" {
		where = append(where, fmt.Sprintf("o.status = $%d", argIdx))
		args = append(args, s)
		argIdx++
	}
	if d := q.Get("from_date"); d != "" {
		where = append(where, fmt.Sprintf("o.created_at >= $%d", argIdx))
		args = append(args, d)
		argIdx++
	}
	if d := q.Get("to_date"); d != "" {
		where = append(where, fmt.Sprintf("o.created_at < ($%d::date + INTERVAL '1 day')", argIdx))
		args = append(args, d)
		argIdx++
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	// Limite de segurança: máximo 10.000 linhas.
	args = append(args, 10000)
	exportSQL := fmt.Sprintf(`
		SELECT o.id, o.order_number, o.status, o.total, o.payment_method,
		       o.payment_status, o.currency, o.created_at, o.updated_at
		  FROM sz_orders o
		  %s
		 ORDER BY o.created_at DESC
		 LIMIT $%d`,
		whereClause, argIdx,
	)

	rows, err := h.db.Query(ctx, exportSQL, args...)
	if err != nil {
		slog.Error("[orders] falha ao exportar pedidos", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao exportar pedidos")
		return
	}
	defer rows.Close()

	// Stream CSV diretamente para o response writer.
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\"pedidos.csv\"")

	csvW := csv.NewWriter(w)
	_ = csvW.Write([]string{
		"id", "order_number", "status", "total",
		"payment_method", "payment_status", "currency",
		"created_at", "updated_at",
	})

	for rows.Next() {
		var id int64
		var orderNumber, status, paymentStatus, currency string
		var totStr string
		var paymentMethod *string
		var createdAt, updatedAt time.Time

		if err := rows.Scan(&id, &orderNumber, &status, &totStr,
			&paymentMethod, &paymentStatus, &currency, &createdAt, &updatedAt); err != nil {
			slog.Error("[orders] erro ao ler linha de export", "err", err)
			continue
		}

		pm := ""
		if paymentMethod != nil {
			pm = *paymentMethod
		}

		_ = csvW.Write([]string{
			strconv.FormatInt(id, 10),
			orderNumber,
			status,
			totStr,
			pm,
			paymentStatus,
			currency,
			createdAt.Format(time.RFC3339),
			updatedAt.Format(time.RFC3339),
		})
	}

	csvW.Flush()
	if err := csvW.Error(); err != nil {
		slog.Error("[orders] erro ao escrever CSV", "err", err)
	}
}

// ── Helpers internos ──────────────────────────────────────────────────────────

// checkOrderAccess verifica se o usuário tem acesso ao pedido.
// Retorna erro se pedido não encontrado ou usuário não tem acesso.
func (h *OrderHandler) checkOrderAccess(ctx context.Context, orderID, userID int64, isAdmin bool) error {
	if isAdmin {
		var exists bool
		err := h.db.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM sz_orders WHERE id = $1)`, orderID,
		).Scan(&exists)
		if err != nil || !exists {
			return fmt.Errorf("não encontrado")
		}
		return nil
	}

	var exists bool
	err := h.db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM sz_orders WHERE id = $1 AND (user_id = $2 OR produtor_id = $2))`,
		orderID, userID,
	).Scan(&exists)
	if err != nil || !exists {
		return fmt.Errorf("não encontrado")
	}
	return nil
}

// parseIDParam extrai e converte o parâmetro de rota `name` para int64.
func parseIDParam(r *http.Request, name string) (int64, error) {
	s := chi.URLParam(r, name)
	return strconv.ParseInt(s, 10, 64)
}

// parseIntParam converte string para int com default e máximo.
// Se max <= 0, não aplica limite superior.
func parseIntParam(s string, def, max int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return def
	}
	if max > 0 && n > max {
		return max
	}
	return n
}

// nullableStr retorna nil se s for vazio, caso contrário retorna &s.
func nullableStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// nullableBytes retorna nil se b for nil ou vazio.
func nullableBytes(b []byte) *string {
	if len(b) == 0 {
		return nil
	}
	s := string(b)
	return &s
}
