package handlers

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/motoboy-service/internal/httpx"
)

// TrackingHandler agrupa dependências para os handlers de rastreio público.
type TrackingHandler struct {
	Pool *pgxpool.Pool
}

// GetTracking responde GET /tracking/{order_id} — rastreio público sem auth.
//
// Retorna status atual + timestamps de cada etapa do pedido.
func (h *TrackingHandler) GetTracking(w http.ResponseWriter, r *http.Request) {
	orderID, err := strconv.ParseInt(chi.URLParam(r, "order_id"), 10, 64)
	if err != nil || orderID <= 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "order_id inválido")
		return
	}

	type PedidoPublico struct {
		ID             int64    `json:"id"`
		WCOrderID      int64    `json:"wc_order_id"`
		Status         string   `json:"status"`
		DestNome       *string  `json:"dest_nome"`
		DestCidade     *string  `json:"dest_cidade"`
		DestUF         *string  `json:"dest_uf"`
		DestProduto    *string  `json:"dest_produto"`
		ReagendadoPara *string  `json:"reagendado_para"`
		TsAprovado     *string  `json:"ts_aprovado"`
		TsEmbalado     *string  `json:"ts_embalado"`
		TsEmRota       *string  `json:"ts_em_rota"`
		TsACaminho     *string  `json:"ts_a_caminho"`
		TsEntregue     *string  `json:"ts_entregue"`
		TsFrustrado    *string  `json:"ts_frustrado"`
		CreatedAt      string   `json:"created_at"`
	}

	var p PedidoPublico
	// NOTA DE MIGRAÇÃO: colunas ts_* e created_at são DATETIME sem timezone no MySQL
	// (wall-clock de Brasília). Após pgloader tornam-se TIMESTAMP WITHOUT TIME ZONE.
	// NÃO usar AT TIME ZONE — formatar diretamente como horário SP.
	// reagendado_para é DATE — usar TO_CHAR para string para evitar scan em time.Time.
	err = h.Pool.QueryRow(r.Context(),
		`SELECT
			id,
			wc_order_id,
			status,
			dest_nome,
			dest_cidade,
			dest_uf,
			dest_produto,
			TO_CHAR(reagendado_para, 'YYYY-MM-DD'),
			TO_CHAR(ts_aprovado,  'YYYY-MM-DD"T"HH24:MI:SS"-03:00"'),
			TO_CHAR(ts_embalado,  'YYYY-MM-DD"T"HH24:MI:SS"-03:00"'),
			TO_CHAR(ts_em_rota,   'YYYY-MM-DD"T"HH24:MI:SS"-03:00"'),
			TO_CHAR(ts_a_caminho, 'YYYY-MM-DD"T"HH24:MI:SS"-03:00"'),
			TO_CHAR(ts_entregue,  'YYYY-MM-DD"T"HH24:MI:SS"-03:00"'),
			TO_CHAR(ts_frustrado, 'YYYY-MM-DD"T"HH24:MI:SS"-03:00"'),
			TO_CHAR(created_at,   'YYYY-MM-DD"T"HH24:MI:SS"-03:00"')
		 FROM sz_motoboy_pedidos
		WHERE wc_order_id = $1
		LIMIT 1`,
		orderID,
	).Scan(
		&p.ID, &p.WCOrderID, &p.Status,
		&p.DestNome, &p.DestCidade, &p.DestUF, &p.DestProduto,
		&p.ReagendadoPara,
		&p.TsAprovado, &p.TsEmbalado, &p.TsEmRota,
		&p.TsACaminho, &p.TsEntregue, &p.TsFrustrado,
		&p.CreatedAt,
	)
	if err != nil {
		// pgx retorna pgx.ErrNoRows — qualquer erro de scan = não encontrado.
		slog.Warn("[tracking] pedido não encontrado", "wc_order_id", orderID, "err", err)
		httpx.WriteErr(w, http.StatusNotFound, "pedido não encontrado")
		return
	}

	httpx.WriteOK(w, map[string]any{"pedido": p})
}

// Reagendar responde POST /tracking/{order_id}/reagendar — V-SEC-03.
//
// IMPORTANTE: validação da order key WooCommerce (campo `key` do body) requer
// consulta à tabela wp_wc_orders ou wp_postmeta que NÃO está disponível no Postgres
// durante Fase 1 da migração. Esta proteção IDOR é crítica — não remover o stub
// até a chave ser migrada para o schema Go.
//
// TODO(V-SEC-03): implementar após migrar wc_order_key para sz_motoboy_pedidos
// ou disponibilizar mirror da tabela wp_wc_orders em Postgres.
func (h *TrackingHandler) Reagendar(w http.ResponseWriter, r *http.Request) {
	_, err := strconv.ParseInt(chi.URLParam(r, "order_id"), 10, 64)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "order_id inválido")
		return
	}

	// Stub fail-closed: retorna 501 até validação de order key estar disponível.
	// NÃO retornar 200 aqui — seria IDOR (protegido por V-SEC-03 no PHP).
	httpx.WriteErr(w, http.StatusNotImplemented,
		"reagendamento ainda não disponível neste endpoint — use o endpoint PHP temporariamente")
}
