// Package handlers contém os handlers HTTP do serviço Motoboy.
package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/motoboy-service/internal/auth"
	"github.com/senderzz/motoboy-service/internal/geo"
	"github.com/senderzz/motoboy-service/internal/httpx"
	"github.com/senderzz/motoboy-service/internal/routing"
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

	// Taxa de cartão (uma vez por request) para expor valor_cartao por pedido.
	feePct := ccFeePct(r.Context(), h.Pool)

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
		  AND (
		    created_at::date = $2::date
		    OR status NOT IN ('entregue','frustrado','cancelado')
		  )
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
		ValorCartao    float64  `json:"valor_cartao"`
		ValorTaxa      float64  `json:"valor_taxa"`
		ReagendadoPara *string  `json:"reagendado_para"`
		CreatedAt      string   `json:"created_at"`
		UpdatedAt      string   `json:"updated_at"`

		// FEAT-ETA-WIRE: posição na rota otimizada (1..N, só p/ pedidos em
		// rota/a caminho) + previsão de chegada "HH:MM". Sequencia=0 e ETA=nil
		// quando o pedido não está em rota OU a roteirização não foi possível
		// (sem GPS do motoboy / destinos sem geocode) — degrada graciosamente.
		Sequencia int     `json:"sequencia"`
		ETA       *string `json:"eta,omitempty"`
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
		p.ValorCartao = valorCartao(p.ValorPedido, feePct)
		pedidos = append(pedidos, p)
	}
	if err := rows.Err(); err != nil {
		slog.Error("[lote] erro ao iterar rows", "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// FEAT-ETA-WIRE: ROTEIRIZAÇÃO DA ROTA DO DIA.
	//
	// Os pedidos EM ROTA (em_rota / a_caminho) são as paradas ativas da entrega.
	// Calcula a ordem ótima (nearest-neighbor a partir do GPS atual do motoboy) e
	// a ETA acumulada por parada via internal/routing, atribui `sequencia` (1..N)
	// e `eta` ("HH:MM") a cada um, e REORDENA o slice para que a lista já saia na
	// ordem da rota. Pedidos fora de rota mantêm sequencia=0 e vão ao final na
	// ordem original (created_at). Degrada graciosamente: sem GPS do motoboy ou
	// sem destinos geocodados → nada muda (ordem original, sem sequencia/eta).
	seqETA := h.rotaSequenciaETA(r.Context(), mb.ID)
	if len(seqETA) > 0 {
		for i := range pedidos {
			if info, ok := seqETA[pedidos[i].ID]; ok {
				pedidos[i].Sequencia = info.seq
				if info.eta != "" {
					eta := info.eta
					pedidos[i].ETA = &eta
				}
			}
		}
		// Reordena: paradas roteirizadas primeiro (por sequencia asc), depois o
		// resto na ordem original (created_at asc, preservada por sort estável).
		sort.SliceStable(pedidos, func(a, b int) bool {
			sa, sb := pedidos[a].Sequencia, pedidos[b].Sequencia
			switch {
			case sa > 0 && sb > 0:
				return sa < sb
			case sa > 0:
				return true // a está na rota, b não → a antes
			case sb > 0:
				return false
			default:
				return false // ambos fora de rota → mantém ordem original
			}
		})
	}

	httpx.WriteOK(w, map[string]any{
		"motoboy_id": mb.ID,
		"data":       hoje,
		"pedidos":    pedidos,
		"total":      len(pedidos),
	})
}

// rotaInfo agrega a posição na rota (1..N) e a ETA "HH:MM" de uma parada.
type rotaInfo struct {
	seq int
	eta string
}

// rotaSequenciaETA calcula, para o motoboy informado, a SEQUÊNCIA otimizada e a
// ETA de cada pedido EM ROTA (status em_rota / a_caminho) do dia.
//
// FEAT-ETA-WIRE: reusa o motor PURO internal/routing (mesmo usado no rastreio
// público em tracking.go):
//  1. lê o GPS atual do motoboy (sz_motoboys.ultimo_lat/ultimo_lng);
//  2. coleta os pedidos em rota e geocoda o destino de cada um (cache em
//     dest_lat/dest_lng), sob um orçamento de tempo único (~2,5s) para não
//     segurar o request em cache frio (Nominatim é serializado a 1 req/s);
//  3. ordena por nearest-neighbor a partir do GPS e estima a ETA acumulada.
//
// Retorna mapa vazio (não-nil) quando não há como roteirizar — sem GPS, sem
// pedidos em rota, ou nenhum destino geocodável. O caller trata isso como
// "sem roteirização" (ordem original, sem sequencia/eta), nunca como erro.
func (h *LoteHandler) rotaSequenciaETA(ctx context.Context, motoboyID int64) map[int64]rotaInfo {
	out := map[int64]rotaInfo{}

	// 1. GPS atual do motoboy.
	var mLat, mLng *float64
	if err := h.Pool.QueryRow(ctx,
		`SELECT ultimo_lat, ultimo_lng FROM sz_motoboys WHERE id=$1`, motoboyID,
	).Scan(&mLat, &mLng); err != nil || mLat == nil || mLng == nil {
		return out // sem GPS → sem roteirização (degrada)
	}

	// 2. Pedidos em rota do dia. Coleta os IDs antes de geocodar para não segurar
	// a conexão do pool durante chamadas de rede.
	rows, err := h.Pool.Query(ctx, `
		SELECT id FROM sz_motoboy_pedidos
		WHERE motoboy_id=$1 AND status IN ('em_rota','a_caminho')`,
		motoboyID,
	)
	if err != nil {
		slog.Warn("[lote] falha ao listar pedidos em rota p/ roteirização", "motoboy_id", motoboyID, "err", err)
		return out
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	if len(ids) == 0 {
		return out
	}

	// Orçamento único de geocoding (~2,5s) — mesma política de tracking.go.
	geoCtx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer cancel()

	stops := make([]routing.Stop, 0, len(ids))
	for _, id := range ids {
		if geoCtx.Err() != nil {
			break // orçamento esgotado → para de tentar cache frio
		}
		lat, lng, gerr := geo.GeocodePedido(geoCtx, h.Pool, id)
		if gerr != nil {
			continue // parada sem coordenada não entra na rota
		}
		stops = append(stops, routing.Stop{PedidoID: id, Lat: lat, Lng: lng})
	}
	if len(stops) == 0 {
		return out
	}

	// 3. Ordena (nearest-neighbor) e estima ETA acumulada por parada.
	loc := brLocation
	agora := time.Now().In(loc)
	ordenada := routing.OrdenarRota(*mLat, *mLng, stops)
	etaMap := routing.EstimarETA(*mLat, *mLng, ordenada, agora, 0, 0)
	for i, s := range ordenada {
		info := rotaInfo{seq: i + 1}
		if t, ok := etaMap[s.PedidoID]; ok {
			info.eta = t.In(loc).Format("15:04")
		}
		out[s.PedidoID] = info
	}
	return out
}
