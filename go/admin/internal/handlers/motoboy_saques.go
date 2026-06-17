// Package handlers — endpoint admin para a fila de saques / pagamentos
// de motoboy.
//
// Espelha sz_mb_tab_saques() em includes/motoboy/admin.php:2016 (§2.10)
// sobre Postgres. A tabela `sz_motoboy_pagamentos` é alimentada pelo PWA
// (pedido de saque, status='aguardando') e pelo admin/Alan na hora do
// repasse (status='pago').
//
// Schema esperado (ver includes/motoboy/database.php):
//
//	sz_motoboy_pagamentos
//	  id BIGINT, motoboy_id BIGINT,
//	  valor_total NUMERIC, data_pagamento DATE,
//	  status TEXT ('pago'|'aguardando'),
//	  obs TEXT, created_at TIMESTAMP
//
// Os campos `motoboy_nome` e `telefone` vêm do JOIN com `sz_motoboys`
// (no PHP era `LEFT JOIN m ON m.id = pg.motoboy_id`).
//
// Surface:
//
//	GET /motoboy-saques?motoboy_id=&status=&date_from=&date_to=&limit=200
//	GET /motoboy-saques/summary   — KPIs (totais pago/aguardando + contagens)
//
// Ordenação espelha PHP:
//
//	ORDER BY CASE status WHEN 'aguardando' THEN 0 WHEN 'pago' THEN 1 ELSE 2 END,
//	         created_at DESC, id DESC
package handlers

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

type MotoboySaquesHandler struct{ Pool *pgxpool.Pool }

// ─── Tipos de resposta ───────────────────────────────────────────────────

// SaqueItem — linha de sz_motoboy_pagamentos + JOIN motoboy.
type SaqueItem struct {
	ID            int64   `json:"id"`
	MotoboyID     int64   `json:"motoboy_id"`
	MotoboyNome   string  `json:"motoboy_nome"`
	Telefone      string  `json:"telefone"`
	ValorTotal    float64 `json:"valor_total"`
	DataPagamento string  `json:"data_pagamento"`
	Status        string  `json:"status"`
	Obs           string  `json:"obs"`
	CreatedAt     string  `json:"created_at"`
}

// SaquesSummary — KPIs globais (filtra pelo mesmo range/motoboy/status
// passado na query string).
type SaquesSummary struct {
	TotalPago        float64 `json:"total_pago"`
	TotalAguardando  float64 `json:"total_aguardando"`
	CountPago        int64   `json:"count_pago"`
	CountAguardando  int64   `json:"count_aguardando"`
}

// ─── Helpers ─────────────────────────────────────────────────────────────

func (h *MotoboySaquesHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// allowedSaqueStatus normaliza o filtro de status. Retorna "" para "todos".
func allowedSaqueStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "pago", "aguardando":
		return strings.ToLower(s)
	default:
		return ""
	}
}

// parseSaqueFilters extrai os filtros comuns entre List e Summary para
// manter as duas queries com o mesmo recorte.
type saqueFilters struct {
	motoboyID int64
	status    string
	dateFrom  string
	dateTo    string
}

func parseSaqueFilters(r *http.Request) saqueFilters {
	q := r.URL.Query()
	f := saqueFilters{}
	f.motoboyID, _ = strconv.ParseInt(q.Get("motoboy_id"), 10, 64)
	f.status = allowedSaqueStatus(q.Get("status"))
	f.dateFrom = strings.TrimSpace(q.Get("date_from"))
	f.dateTo = strings.TrimSpace(q.Get("date_to"))
	if f.dateFrom != "" && !isISODate(f.dateFrom) {
		f.dateFrom = ""
	}
	if f.dateTo != "" && !isISODate(f.dateTo) {
		f.dateTo = ""
	}
	return f
}

// ─── List ────────────────────────────────────────────────────────────────

// List retorna os pagamentos com filtros opcionais.
// GET /motoboy-saques?motoboy_id=&status=&date_from=&date_to=&limit=200
func (h *MotoboySaquesHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_motoboy_pagamentos") {
		httpx.JSON(w, 200, map[string]any{"items": []SaqueItem{}, "count": 0})
		return
	}

	f := parseSaqueFilters(r)
	limit := clampLimit(r.URL.Query().Get("limit"), 200, 500)

	// Filtro de data sobre data_pagamento (o que aparece na coluna "Data saque"
	// do PHP). created_at é só ordenação secundária.
	rows, err := h.Pool.Query(ctx,
		`SELECT
			pg.id,
			COALESCE(pg.motoboy_id, 0),
			COALESCE(m.nome, '')          AS motoboy_nome,
			COALESCE(m.telefone, '')      AS telefone,
			COALESCE(pg.valor_total, 0),
			pg.data_pagamento::text       AS data_pagamento,
			COALESCE(pg.status, ''),
			COALESCE(pg.obs, ''),
			pg.created_at::text           AS created_at
		   FROM sz_motoboy_pagamentos pg
		   LEFT JOIN sz_motoboys m ON m.id = pg.motoboy_id
		  WHERE ($1 = 0  OR pg.motoboy_id = $1)
		    AND ($2 = '' OR pg.status     = $2)
		    AND ($3 = '' OR pg.data_pagamento >= $3::date)
		    AND ($4 = '' OR pg.data_pagamento <= $4::date)
		  ORDER BY CASE pg.status
		           WHEN 'aguardando' THEN 0
		           WHEN 'pago'       THEN 1
		           ELSE 2 END ASC,
		           pg.created_at DESC, pg.id DESC
		  LIMIT $5`,
		f.motoboyID, f.status, f.dateFrom, f.dateTo, limit)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []SaqueItem{}
	for rows.Next() {
		var it SaqueItem
		if err := rows.Scan(
			&it.ID, &it.MotoboyID, &it.MotoboyNome, &it.Telefone,
			&it.ValorTotal, &it.DataPagamento, &it.Status, &it.Obs, &it.CreatedAt,
		); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
		out = append(out, it)
	}
	httpx.JSON(w, 200, map[string]any{"items": out, "count": len(out)})
}

// ─── Summary ─────────────────────────────────────────────────────────────

// Summary retorna KPIs do conjunto filtrado (mesmos filtros de List).
// GET /motoboy-saques/summary?motoboy_id=&status=&date_from=&date_to=
//
// status, se informado, ainda zera os agregadores do status oposto — o filtro
// é honrado para manter coerência com a lista mostrada.
func (h *MotoboySaquesHandler) Summary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := SaquesSummary{}

	if !h.tableExists(ctx, "sz_motoboy_pagamentos") {
		httpx.JSON(w, 200, out)
		return
	}

	f := parseSaqueFilters(r)

	err := h.Pool.QueryRow(ctx,
		`SELECT
		   COALESCE(SUM(valor_total) FILTER (WHERE status='pago'), 0)        AS total_pago,
		   COALESCE(SUM(valor_total) FILTER (WHERE status='aguardando'), 0)  AS total_aguardando,
		   COUNT(*) FILTER (WHERE status='pago')                              AS count_pago,
		   COUNT(*) FILTER (WHERE status='aguardando')                        AS count_aguardando
		 FROM sz_motoboy_pagamentos
		 WHERE ($1 = 0  OR motoboy_id     = $1)
		   AND ($2 = '' OR status         = $2)
		   AND ($3 = '' OR data_pagamento >= $3::date)
		   AND ($4 = '' OR data_pagamento <= $4::date)`,
		f.motoboyID, f.status, f.dateFrom, f.dateTo,
	).Scan(&out.TotalPago, &out.TotalAguardando, &out.CountPago, &out.CountAguardando)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	httpx.JSON(w, 200, out)
}
