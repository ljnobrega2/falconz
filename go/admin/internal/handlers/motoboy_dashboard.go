// Package handlers — endpoint admin para o dashboard do módulo Motoboy.
// Espelha sz_mb_tab_dashboard() em includes/motoboy/admin.php:224.
// Fornece KPIs do dia + ranking de motoboys ativos.
//
// Endpoint:
//
//	GET /motoboy-dashboard?date=YYYY-MM-DD
//	  - date é opcional; default = "hoje" em America/Sao_Paulo.
//	  - Tabelas ausentes → respondemos com zeros / ranking vazio (graceful).
package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

type MotoboyDashboardHandler struct{ Pool *pgxpool.Pool }

// MotoboyDashboardKPIs — contadores por status do dia + fechamentos pendentes.
// Espelha o $map gerado pelo PHP a partir do GROUP BY status.
type MotoboyDashboardKPIs struct {
	Agendados            int64 `json:"agendados"`
	Embalados            int64 `json:"embalados"`
	EmRota               int64 `json:"em_rota"`
	Entregues            int64 `json:"entregues"`
	Frustrados           int64 `json:"frustrados"`
	FechamentosPendentes int64 `json:"fechamentos_pendentes"`
}

// MotoboyRankingItem — linha do ranking de motoboys ativos no dia.
// Espelha o SELECT do PHP com JOIN sz_motoboys + sz_motoboy_cds + sz_motoboy_zonas.
type MotoboyRankingItem struct {
	MotoboyID    int64   `json:"motoboy_id"`
	MotoboyNome  string  `json:"motoboy_nome"`
	CDNome       string  `json:"cd_nome"`
	ZonaNome     string  `json:"zona_nome"`
	Pedidos      int64   `json:"pedidos"`
	Entregues    int64   `json:"entregues"`
	Frustrados   int64   `json:"frustrados"`
	TaxaSucesso  float64 `json:"taxa_sucesso"`
}

// MotoboyDashboardResp — payload final retornado ao admin-ui.
type MotoboyDashboardResp struct {
	Date    string               `json:"date"`
	KPIs    MotoboyDashboardKPIs `json:"kpis"`
	Ranking []MotoboyRankingItem `json:"ranking"`
}

// tableExists — verifica existência da tabela no schema public.
// Cópia do helper de audit.go para manter o handler auto-contido.
func (h *MotoboyDashboardHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// resolveDate — lê o query param "date" ou usa hoje em America/Sao_Paulo
// (igual ao PHP: DateTimeImmutable('now', new DateTimeZone('America/Sao_Paulo'))).
// Formato esperado: YYYY-MM-DD. Inválido cai no default.
func resolveDashboardDate(raw string) string {
	if raw != "" {
		if _, err := time.Parse("2006-01-02", raw); err == nil {
			return raw
		}
	}
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		// Fallback defensivo: UTC se o tz database não estiver instalado.
		loc = time.UTC
	}
	return time.Now().In(loc).Format("2006-01-02")
}

// Dashboard — handler principal.
// GET /motoboy-dashboard?date=YYYY-MM-DD
func (h *MotoboyDashboardHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	date := resolveDashboardDate(r.URL.Query().Get("date"))

	resp := MotoboyDashboardResp{
		Date:    date,
		KPIs:    MotoboyDashboardKPIs{},
		Ranking: []MotoboyRankingItem{},
	}

	// ── KPIs por status do dia ────────────────────────────────────────────
	// SELECT status, COUNT(*) FROM sz_motoboy_pedidos WHERE created_at::date = $1 GROUP BY status
	if h.tableExists(ctx, "sz_motoboy_pedidos") {
		rows, err := h.Pool.Query(ctx,
			`SELECT status, COUNT(*)::bigint
			   FROM sz_motoboy_pedidos
			  WHERE created_at::date = $1::date
			  GROUP BY status`, date)
		if err == nil {
			for rows.Next() {
				var status string
				var total int64
				if err := rows.Scan(&status, &total); err != nil {
					continue
				}
				switch status {
				case "agendado", "aprovado":
					// "aprovado" no enum equivale a agendado no PHP.
					resp.KPIs.Agendados += total
				case "embalado":
					resp.KPIs.Embalados += total
				case "em_rota", "a_caminho":
					// "a_caminho" mapeia para em_rota (vide sz_mb_admin_status_from_wc).
					resp.KPIs.EmRota += total
				case "entregue":
					resp.KPIs.Entregues += total
				case "frustrado":
					resp.KPIs.Frustrados += total
				}
			}
			rows.Close()
		}
	}

	// ── Fechamentos pendentes (Alan ainda não confirmou) ─────────────────
	// COUNT(*) FROM sz_motoboy_fechamento WHERE alan_confirmou=false AND data_fechamento < $1
	// PHP usa '<' (estrito) em admin.php:252 — exclui fechamentos do dia atual.
	// Mantemos a regra forte do PHP (entregues>0 AND a_repassar>0) para evitar
	// contar fechamentos "vazios", mas só se as colunas existirem.
	if h.tableExists(ctx, "sz_motoboy_fechamento") {
		_ = h.Pool.QueryRow(ctx,
			`SELECT COUNT(*)::bigint
			   FROM sz_motoboy_fechamento
			  WHERE alan_confirmou = FALSE
			    AND data_fechamento < $1::date
			    AND COALESCE(total_entregues, 0) > 0
			    AND COALESCE(total_a_repassar, 0) > 0`, date).
			Scan(&resp.KPIs.FechamentosPendentes)
	}

	// ── Ranking de motoboys ──────────────────────────────────────────────
	// "Primary zona" = sz_motoboys.zona_id (igual ao PHP). Pivot é só para
	// suporte a múltiplas zonas e não entra aqui.
	// Ordenação: entregues DESC (cliente reordena por taxa_sucesso na UI).
	if h.tableExists(ctx, "sz_motoboys") && h.tableExists(ctx, "sz_motoboy_pedidos") {
		hasZonas := h.tableExists(ctx, "sz_motoboy_zonas")
		hasCDs := h.tableExists(ctx, "sz_motoboy_cds")

		zonaJoin := ""
		zonaCol := "''::text AS zona_nome"
		if hasZonas {
			zonaJoin = " LEFT JOIN sz_motoboy_zonas z ON z.id = m.zona_id"
			zonaCol = "COALESCE(z.nome, '') AS zona_nome"
		}
		cdJoin := ""
		cdCol := "''::text AS cd_nome"
		if hasCDs {
			cdJoin = " LEFT JOIN sz_motoboy_cds cd ON cd.id = m.cd_id"
			cdCol = "COALESCE(cd.nome, '') AS cd_nome"
		}

		query := `
			SELECT m.id,
			       m.nome,
			       ` + cdCol + `,
			       ` + zonaCol + `,
			       COUNT(mp.id)::bigint AS pedidos,
			       COUNT(*) FILTER (WHERE mp.status = 'entregue')::bigint  AS entregues,
			       COUNT(*) FILTER (WHERE mp.status = 'frustrado')::bigint AS frustrados,
			       CASE WHEN COUNT(mp.id) > 0
			            THEN ROUND( (COUNT(*) FILTER (WHERE mp.status='entregue'))::numeric
			                         / COUNT(mp.id)::numeric * 100, 1)::float8
			            ELSE 0::float8
			       END AS taxa_sucesso
			  FROM sz_motoboys m` + cdJoin + zonaJoin + `
			  LEFT JOIN sz_motoboy_pedidos mp
			    ON mp.motoboy_id = m.id
			   AND mp.created_at::date = $1::date
			 WHERE m.ativo = TRUE
			 GROUP BY 1, 2, 3, 4
			 ORDER BY entregues DESC, pedidos DESC, m.nome ASC`

		rows, err := h.Pool.Query(ctx, query, date)
		if err == nil {
			for rows.Next() {
				var it MotoboyRankingItem
				if err := rows.Scan(
					&it.MotoboyID,
					&it.MotoboyNome,
					&it.CDNome,
					&it.ZonaNome,
					&it.Pedidos,
					&it.Entregues,
					&it.Frustrados,
					&it.TaxaSucesso,
				); err != nil {
					continue
				}
				resp.Ranking = append(resp.Ranking, it)
			}
			rows.Close()
		}
	}

	httpx.JSON(w, http.StatusOK, resp)
}
