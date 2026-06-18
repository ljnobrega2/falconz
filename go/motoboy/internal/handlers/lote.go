// Package handlers contém os handlers HTTP do serviço Motoboy.
package handlers

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/motoboy-service/internal/auth"
	"github.com/senderzz/motoboy-service/internal/httpx"
)

// brLocation é o fuso horário canônico da operação — América/São_Paulo.
// Replicado de sz_motoboy_br_timezone() em PHP para manter consistência
// no filtro "DATE(created_at) = hoje" de /motoboy/lote.
var brLocation *time.Location

func init() {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		// Fallback rígido: UTC-3. Aceitável apenas se a imagem não tem tzdata.
		slog.Warn("[handlers] não foi possível carregar America/Sao_Paulo, usando UTC-3 fixo", "err", err)
		loc = time.FixedZone("BRT", -3*60*60)
	}
	brLocation = loc
}

// LoteHandler agrupa dependências para os handlers do grupo /motoboy.
type LoteHandler struct {
	Pool *pgxpool.Pool
}

// Lote responde GET /motoboy/lote — pedidos do dia do motoboy autenticado.
//
// "Dia" é calculado no fuso America/Sao_Paulo (mesma lógica de sz_motoboy_br_timezone em PHP).
// Retorna apenas pedidos cujo created_at cai no dia corrente de Brasília.
func (h *LoteHandler) Lote(w http.ResponseWriter, r *http.Request) {
	mb := auth.MotoboyfromCtx(r.Context())
	if mb == nil {
		// Não deve chegar aqui — AuthMotoboy middleware já retornou 401.
		httpx.WriteErr(w, http.StatusUnauthorized, "não autorizado")
		return
	}

	// "Hoje" em horário de Brasília.
	hoje := time.Now().In(brLocation).Format("2006-01-02")

	// NOTA DE MIGRAÇÃO: sz_motoboy_pedidos.created_at é DATETIME sem timezone no MySQL
	// (gerado por sz_motoboy_now_mysql() que grava horário de Brasília como wall-clock).
	// Após pgloader o tipo vira TIMESTAMP WITHOUT TIME ZONE contendo horário SP.
	// Portanto NÃO usar AT TIME ZONE — comparar diretamente como se fossem timestamps SP.
	// Se o schema Postgres for migrado para TIMESTAMPTZ/UTC, remover este comentário
	// e restaurar AT TIME ZONE 'America/Sao_Paulo' nas queries.
	rows, err := h.Pool.Query(r.Context(),
		`SELECT
			id,
			wc_order_id,
			status,
			dest_nome,
			dest_telefone,
			dest_cep,
			dest_endereco,
			dest_numero,
			dest_complemento,
			dest_bairro,
			dest_cidade,
			dest_uf,
			dest_lat,
			dest_lng,
			dest_produto,
			valor_pedido,
			valor_taxa,
			TO_CHAR(reagendado_para, 'YYYY-MM-DD'),
			created_at,
			updated_at
		 FROM sz_motoboy_pedidos
		WHERE motoboy_id = $1
		  AND DATE(created_at) = $2::date
		ORDER BY created_at ASC`,
		mb.ID, hoje,
	)
	if err != nil {
		slog.Error("[lote] erro ao buscar pedidos", "motoboy_id", mb.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer rows.Close()

	type Pedido struct {
		ID             int64    `json:"id"`
		WCOrderID      int64    `json:"wc_order_id"`
		Status         string   `json:"status"`
		DestNome       *string  `json:"dest_nome"`
		DestTelefone   *string  `json:"dest_telefone"`
		DestCEP        string   `json:"dest_cep"`
		DestEndereco   *string  `json:"dest_endereco"`
		DestNumero     *string  `json:"dest_numero"`
		DestCompl      *string  `json:"dest_complemento"`
		DestBairro     *string  `json:"dest_bairro"`
		DestCidade     *string  `json:"dest_cidade"`
		DestUF         *string  `json:"dest_uf"`
		DestLat        *float64 `json:"dest_lat"`
		DestLng        *float64 `json:"dest_lng"`
		DestProduto    *string  `json:"dest_produto"`
		ValorPedido    float64  `json:"valor_pedido"`
		ValorTaxa      float64  `json:"valor_taxa"`
		ReagendadoPara *string  `json:"reagendado_para"`
		CreatedAt      string   `json:"created_at"`
		UpdatedAt      string   `json:"updated_at"`
	}

	pedidos := []Pedido{}
	for rows.Next() {
		var p Pedido
		var createdAt, updatedAt time.Time
		if err := rows.Scan(
			&p.ID, &p.WCOrderID, &p.Status,
			&p.DestNome, &p.DestTelefone,
			&p.DestCEP, &p.DestEndereco, &p.DestNumero, &p.DestCompl,
			&p.DestBairro, &p.DestCidade, &p.DestUF,
			&p.DestLat, &p.DestLng, &p.DestProduto,
			&p.ValorPedido, &p.ValorTaxa,
			&p.ReagendadoPara,
			&createdAt, &updatedAt,
		); err != nil {
			slog.Error("[lote] erro ao scanear pedido", "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
			return
		}
		p.CreatedAt = createdAt.In(brLocation).Format("2006-01-02T15:04:05-03:00")
		p.UpdatedAt = updatedAt.In(brLocation).Format("2006-01-02T15:04:05-03:00")
		pedidos = append(pedidos, p)
	}
	if err := rows.Err(); err != nil {
		slog.Error("[lote] erro ao iterar rows", "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	httpx.WriteOK(w, map[string]any{
		"motoboy_id": mb.ID,
		"data":       hoje,
		"pedidos":    pedidos,
		"total":      len(pedidos),
	})
}
