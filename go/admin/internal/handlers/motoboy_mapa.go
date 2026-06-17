// Package handlers — endpoint admin para o mapa ao vivo do módulo Motoboy.
// Espelha sz_mb_tab_mapa() em includes/motoboy/admin.php:1761.
// Fornece posições GPS dos motoboys + status online/offline.
//
// Endpoint:
//
//	GET /motoboy-mapa/locations
//	  - Retorna lista de motoboys com lat/lng, contadores do dia e status online.
//	  - Tabelas ausentes → responde graciosamente com lista vazia.
package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

// MotoboyMapaHandler — handler para o mapa ao vivo de motoboys.
type MotoboyMapaHandler struct{ Pool *pgxpool.Pool }

// MotoboyLocation — posição e métricas de um motoboy para o mapa.
type MotoboyLocation struct {
	ID             int64    `json:"id"`
	Nome           string   `json:"nome"`
	ZonaNome       string   `json:"zona_nome"`
	CD             string   `json:"cd"`
	PedidosAbertos int64    `json:"pedidos_abertos"`
	EntreguesHoje  int64    `json:"entregues_hoje"`
	UltimoLat      *float64 `json:"ultimo_lat"`
	UltimoLng      *float64 `json:"ultimo_lng"`
	UltimoPing     *string  `json:"ultimo_ping"`
	Online         bool     `json:"online"`
}

// MapCenter — centro sugerido para o mapa (média dos online ou São Paulo default).
type MapCenter struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// MotoboyMapaResp — payload final retornado ao admin-ui.
type MotoboyMapaResp struct {
	Motoboys  []MotoboyLocation `json:"motoboys"`
	Center    MapCenter         `json:"center"`
	UpdatedAt string            `json:"updated_at"`
}

// tableExists — verifica existência da tabela no schema public.
// Cópia do helper de motoboy_dashboard.go para manter o handler auto-contido.
func (h *MotoboyMapaHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// Locations — handler principal.
// GET /motoboy-mapa/locations
func (h *MotoboyMapaHandler) Locations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// São Paulo como centro padrão quando não há motoboys online com GPS.
	const defaultLat = -23.55
	const defaultLng = -46.63

	resp := MotoboyMapaResp{
		Motoboys:  []MotoboyLocation{},
		Center:    MapCenter{Lat: defaultLat, Lng: defaultLng},
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	// Verificação de existência das tabelas — degradação graciosa.
	if !h.tableExists(ctx, "sz_motoboys") || !h.tableExists(ctx, "sz_motoboy_pedidos") {
		httpx.JSON(w, http.StatusOK, resp)
		return
	}

	// Fuso horário de São Paulo para calcular "hoje".
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		loc = time.UTC
	}
	hoje := time.Now().In(loc).Format("2006-01-02")

	// Verifica se a tabela de zonas existe para fazer o JOIN opcional.
	hasZonas := h.tableExists(ctx, "sz_motoboy_zonas")
	zonaJoin := ""
	zonaCol := "''::text AS zona_nome"
	if hasZonas {
		zonaJoin = " LEFT JOIN sz_motoboy_zonas z ON z.id = m.zona_id"
		zonaCol = "COALESCE(z.nome, '') AS zona_nome"
	}

	// Verifica se a tabela de CDs existe para fazer o JOIN opcional.
	hasCDs := h.tableExists(ctx, "sz_motoboy_cds")
	cdJoin := ""
	cdCol := "''::text AS cd"
	if hasCDs {
		cdJoin = " LEFT JOIN sz_motoboy_cds cd ON cd.id = m.cd_id"
		cdCol = "COALESCE(cd.nome, '') AS cd"
	}

	// ── Consulta principal ───────────────────────────────────────────────────
	// Junta motoboys com contadores do dia e última posição GPS.
	// online = TRUE se ultimo_ping >= NOW() - 5 minutos.
	query := `
		SELECT
			m.id,
			m.nome,
			` + zonaCol + `,
			` + cdCol + `,
			-- pedidos_abertos: embalado ou em_rota (motoboy ainda não entregou)
			COALESCE((
				SELECT COUNT(*)
				  FROM sz_motoboy_pedidos
				 WHERE motoboy_id = m.id
				   AND status IN ('embalado', 'em_rota')
			), 0)::bigint AS pedidos_abertos,
			-- entregues_hoje: status entregue no dia de hoje
			COALESCE((
				SELECT COUNT(*)
				  FROM sz_motoboy_pedidos
				 WHERE motoboy_id = m.id
				   AND status = 'entregue'
				   AND created_at::date = $1::date
			), 0)::bigint AS entregues_hoje,
			-- GPS: colunas opcionais — NULL se ainda não registrado
			m.ultimo_lat,
			m.ultimo_lng,
			m.ultimo_ping,
			-- online: ping nos últimos 5 minutos
			(m.ultimo_ping IS NOT NULL AND m.ultimo_ping >= NOW() - INTERVAL '5 minutes') AS online
		  FROM sz_motoboys m` + zonaJoin + cdJoin + `
		 WHERE m.ativo = TRUE
		 ORDER BY online DESC, m.nome ASC`

	rows, err := h.Pool.Query(ctx, query, hoje)
	if err != nil {
		// Falha silenciosa — retorna estrutura vazia para não quebrar o front.
		httpx.JSON(w, http.StatusOK, resp)
		return
	}
	defer rows.Close()

	var sumLat, sumLng float64
	var countOnlineGPS int64

	for rows.Next() {
		var mb MotoboyLocation
		// ultimo_ping como string ISO para serialização direta ao JSON.
		var ultimoPingRaw *time.Time

		if err := rows.Scan(
			&mb.ID,
			&mb.Nome,
			&mb.ZonaNome,
			&mb.CD,
			&mb.PedidosAbertos,
			&mb.EntreguesHoje,
			&mb.UltimoLat,
			&mb.UltimoLng,
			&ultimoPingRaw,
			&mb.Online,
		); err != nil {
			continue
		}

		// Formata o timestamp para RFC3339 se disponível.
		if ultimoPingRaw != nil {
			s := ultimoPingRaw.UTC().Format(time.RFC3339)
			mb.UltimoPing = &s
		}

		// Acumula lat/lng para calcular o centro apenas dos online com GPS.
		if mb.Online && mb.UltimoLat != nil && mb.UltimoLng != nil {
			sumLat += *mb.UltimoLat
			sumLng += *mb.UltimoLng
			countOnlineGPS++
		}

		resp.Motoboys = append(resp.Motoboys, mb)
	}

	// Centro: média dos motoboys online com GPS, ou São Paulo se nenhum.
	if countOnlineGPS > 0 {
		resp.Center = MapCenter{
			Lat: sumLat / float64(countOnlineGPS),
			Lng: sumLng / float64(countOnlineGPS),
		}
	}

	httpx.JSON(w, http.StatusOK, resp)
}
