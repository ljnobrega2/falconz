// Package handlers — cotação de frete AO VIVO pra auditoria (admin-only).
//
// AUDIT-2026-07-29 (dono): "preciso que no admin mostre o custo de TODOS os
// fretes cotados e tb o serviço mais barato selecionado (TUDO ISSO SOMENTE NO
// ADMIN)" — sem isso, não dava pra conferir visualmente se a regra de bloqueio
// de Correios + escolha da mais barata (emit.go) estava funcionando; só se via
// o resultado final (1 serviço) já emitido. Recotação em tempo real (não um
// snapshot histórico) — reflete o preço ATUAL da ME, igual reemissão faria.
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/auth"
	"github.com/senderzz/admin-service/internal/httpx"
	"github.com/senderzz/admin-service/internal/melhorenvio"
)

type FreightQuotesHandler struct {
	Pool *pgxpool.Pool
	ME   *melhorenvio.Client
}

type freightQuoteOut struct {
	ServiceID      int     `json:"service_id"`
	Nome           string  `json:"nome"`
	Transportadora string  `json:"transportadora"`
	Preco          float64 `json:"preco"`
	PrazoDias      int     `json:"prazo_dias"`
	Bloqueado      bool    `json:"bloqueado"`    // Correios bloqueado no perfil do produtor
	Indisponivel   bool    `json:"indisponivel"` // ME devolveu erro pra essa opção (sem preço real)
	Selecionado    bool    `json:"selecionado"`  // é a mais barata DISPONÍVEL (o que a emissão real escolheria) — só quando fonte=ao_vivo
	Emitida        bool    `json:"emitida"`      // foi REALMENTE a transportadora/serviço emitido nesse pedido — só quando fonte=historico
}

// GetFreightQuotes — GET /orders/{id}/freight-quotes (admin/operador only).
func (h *FreightQuotesHandler) GetFreightQuotes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if actor := auth.ActorFromCtx(ctx); actor != nil && actor.Kind != auth.ActorAdmin {
		if actor.Kind != auth.ActorKind("operator") && actor.Kind != auth.ActorKind("operador") {
			httpx.Err(w, 403, "forbidden", "acesso restrito a admin e operador logístico")
			return
		}
	}
	orderID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || orderID <= 0 {
		httpx.Err(w, 400, "bad_request", "order_id inválido")
		return
	}

	// AUDIT-2026-07-30 (dono): "ajusta para mostrar a que realmente foi emitida
	// e sempre usar a cotação do momento da solicitação do pedido" — se já existe
	// etiqueta ATIVA emitida com snapshot salvo (migration 517), usa ELE (preço/opções
	// reais de quando o pedido foi processado), nunca recotação ao vivo (preço
	// muda dia a dia e não reflete o que realmente aconteceu). Recotação ao vivo
	// só continua fazendo sentido pra pedido AINDA sem etiqueta (força escolha).
	if out, meta, ok := h.loadHistoricQuotes(ctx, orderID); ok {
		httpx.JSON(w, 200, map[string]any{
			"ok":       true,
			"fonte":    "historico",
			"from_cep": meta.fromCEP, "to_cep": meta.toCEP,
			"cotacoes": out,
		})
		return
	}

	if h.ME == nil || !h.ME.HasToken() {
		httpx.Err(w, 503, "me_disabled", "ME_TOKEN não configurado")
		return
	}

	var produtorID int64
	var toCEP string
	err = h.Pool.QueryRow(ctx,
		`SELECT o.produtor_id, COALESCE(a.cep, '')
		   FROM sz_orders o
		   LEFT JOIN sz_order_addresses a ON a.order_id = o.id AND a.tipo = 'shipping'
		  WHERE o.id = $1
		  ORDER BY a.id ASC LIMIT 1`,
		orderID,
	).Scan(&produtorID, &toCEP)
	if err == pgx.ErrNoRows {
		httpx.Err(w, 404, "not_found", "pedido não encontrado")
		return
	}
	if err != nil {
		httpx.Err(w, 500, "db_error", "erro ao buscar pedido")
		return
	}
	toCEP = strings.ReplaceAll(toCEP, "-", "")
	if toCEP == "" {
		httpx.Err(w, 422, "unprocessable", "pedido sem CEP de destino cadastrado")
		return
	}

	fromCEP := h.loadOrigemCEP(ctx)
	if fromCEP == "" {
		httpx.Err(w, 422, "unprocessable", "CEP de origem não configurado em Frete → Melhor Envio")
		return
	}

	products, err := h.loadProducts(ctx, orderID)
	if err != nil {
		httpx.Err(w, 500, "db_error", "erro ao buscar produtos do pedido")
		return
	}
	if len(products) == 0 {
		httpx.Err(w, 422, "unprocessable", "pedido sem produtos com dimensões cadastradas")
		return
	}

	options, err := h.ME.Calculate(ctx, melhorenvio.CalcRequest{FromCEP: fromCEP, ToCEP: toCEP, Products: products})
	if err != nil {
		httpx.Err(w, 502, "me_error", "falha ao cotar frete na Melhor Envio: "+err.Error())
		return
	}

	bloqueiaCorreios := h.producerBloqueiaCorreios(ctx, produtorID)

	out := make([]freightQuoteOut, 0, len(options))
	for _, o := range options {
		preco, _ := strconv.ParseFloat(o.Price.String(), 64)
		isCorreios := strings.Contains(strings.ToUpper(o.Company.Name), "CORREIOS")
		out = append(out, freightQuoteOut{
			ServiceID:      o.ServiceID,
			Nome:           o.Name,
			Transportadora: o.Company.Name,
			Preco:          preco,
			PrazoDias:      o.DeliveryDays,
			Bloqueado:      isCorreios && bloqueiaCorreios,
			Indisponivel:   o.Error != "" || preco <= 0,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Preco < out[j].Preco })

	// Marca a mais barata DISPONÍVEL (não bloqueada, não indisponível) —
	// mesma regra de emit.go (candidatos ordenados por preço, primeiro válido).
	for i := range out {
		if !out[i].Bloqueado && !out[i].Indisponivel {
			out[i].Selecionado = true
			break
		}
	}

	httpx.JSON(w, 200, map[string]any{
		"ok":                true,
		"fonte":             "ao_vivo",
		"from_cep":          fromCEP,
		"to_cep":            toCEP,
		"bloqueia_correios": bloqueiaCorreios,
		"cotacoes":          out,
	})
}

type historicQuotesMeta struct {
	fromCEP string
	toCEP   string
}

// loadHistoricQuotes lê o snapshot salvo na emissão ATIVA (wc_me_labels.quotes_snapshot,
// migration 517) da etiqueta mais recente do pedido. Marca "emitida" no serviço
// REALMENTE usado (wc_me_labels.service_id), nunca "mais barata disponível hoje"
// — preço/disponibilidade da ME mudam dia a dia, então recotar ao vivo depois de
// já ter emitido mostra um resultado que pode nem ter existido na hora real.
func (h *FreightQuotesHandler) loadHistoricQuotes(ctx context.Context, orderID int64) ([]freightQuoteOut, historicQuotesMeta, bool) {
	var snapshot []byte
	var actualServiceID int
	var fromCEP, toCEP string
	err := h.Pool.QueryRow(ctx,
		`SELECT quotes_snapshot, service_id, COALESCE(from_cep,''), COALESCE(to_cep,'')
		   FROM wc_me_labels
		  WHERE wc_order_id = $1 AND quotes_snapshot IS NOT NULL
		    AND status NOT IN ('canceled', 'cancelled')
		  ORDER BY id DESC LIMIT 1`,
		orderID,
	).Scan(&snapshot, &actualServiceID, &fromCEP, &toCEP)
	if err != nil || len(snapshot) == 0 {
		return nil, historicQuotesMeta{}, false
	}

	var options []struct {
		ServiceID    int         `json:"id"`
		Name         string      `json:"name"`
		Price        json.Number `json:"price"`
		DeliveryDays int         `json:"delivery_time"`
		Company      struct {
			Name string `json:"name"`
		} `json:"company"`
		Error string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(snapshot, &options); err != nil {
		return nil, historicQuotesMeta{}, false
	}

	out := make([]freightQuoteOut, 0, len(options))
	for _, o := range options {
		preco, _ := strconv.ParseFloat(o.Price.String(), 64)
		out = append(out, freightQuoteOut{
			ServiceID:      o.ServiceID,
			Nome:           o.Name,
			Transportadora: o.Company.Name,
			Preco:          preco,
			PrazoDias:      o.DeliveryDays,
			Indisponivel:   o.Error != "" || preco <= 0,
			Emitida:        o.ServiceID == actualServiceID,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Preco < out[j].Preco })

	return out, historicQuotesMeta{fromCEP: fromCEP, toCEP: toCEP}, true
}

// loadOrigemCEP espelha go/labels/internal/handlers/emit.go::assembleLabelData
// (passo 3) — senderzz_options['woocommerce_wc-melhor-envio_settings'].
func (h *FreightQuotesHandler) loadOrigemCEP(ctx context.Context) string {
	var raw string
	_ = h.Pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE name='woocommerce_wc-melhor-envio_settings' LIMIT 1`,
	).Scan(&raw)
	if raw == "" {
		return ""
	}
	var settings struct {
		Address struct {
			PostalCode string `json:"postal_code"`
		} `json:"address"`
	}
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return ""
	}
	return strings.ReplaceAll(settings.Address.PostalCode, "-", "")
}

// loadProducts espelha o SELECT de emit.go::EmitOrderLabel (custo do produto,
// default R$10/unidade quando não preenchido — AUDIT-2026-07-28 insurance fix).
func (h *FreightQuotesHandler) loadProducts(ctx context.Context, orderID int64) ([]melhorenvio.CalcProduct, error) {
	rows, err := h.Pool.Query(ctx,
		`SELECT oi.quantidade,
		        COALESCE(p.altura, 15)::float, COALESCE(p.largura, 11)::float,
		        COALESCE(p.comprimento, 20)::float, COALESCE(p.peso, 0.3)::float,
		        COALESCE(NULLIF(p.custo, 0), 10)::float
		   FROM sz_order_items oi
		   JOIN sz_products p ON p.id = oi.produto_id
		  WHERE oi.order_id = $1`,
		orderID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []melhorenvio.CalcProduct
	for rows.Next() {
		var qty int
		var altura, largura, comprimento, peso, custo float64
		if err := rows.Scan(&qty, &altura, &largura, &comprimento, &peso, &custo); err != nil {
			continue
		}
		out = append(out, melhorenvio.CalcProduct{
			Height: altura, Width: largura, Length: comprimento, Weight: peso,
			InsuranceValue: custo, Quantity: qty,
		})
	}
	return out, nil
}

// producerBloqueiaCorreios espelha go/orders/internal/handlers/freight.go e
// go/labels/internal/handlers/emit.go (mesma coluna settings->>'bloqueio_correios',
// sem migration própria — convenção do projeto).
func (h *FreightQuotesHandler) producerBloqueiaCorreios(ctx context.Context, producerID int64) bool {
	var bloqueia bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT COALESCE((settings->>'bloqueio_correios')::boolean, false)
		   FROM senderzz_portal_users WHERE id = $1`,
		producerID,
	).Scan(&bloqueia)
	return bloqueia
}
