// Package handlers — endpoint admin para o visualizador de comprovantes
// de pagamento dos pedidos motoboy.
//
// Espelha o bloco "Comprovantes" da metabox do pedido WooCommerce
// (includes/motoboy/order-metabox.php:158-163) e expande para uma listagem
// global com filtros, usada no painel Go.
//
// Tabela principal: sz_motoboy_comprovantes (id, pedido_id, wc_order_id,
//   motoboy_id, tipo_pgto, foto_url, foto_path, baixa_por, created_at).
//   Ver includes/motoboy/database.php:351.
//
// Surface:
//
//	GET    /motoboy-comprovantes
//	       ?motoboy_id=&pedido_id=&tipo=&baixa_por=&date_from=&date_to=&limit=200
//	       → galeria filtrada (sz_motoboy_comprovantes + nome motoboy).
//	GET    /motoboy-comprovantes/stats
//	       ?date_from=&date_to=&motoboy_id=&zona_id=&status=&baixa_por=
//	       → 6 KPIs + contador sem_comp (via sz_motoboy_pedidos).
//	GET    /motoboy-comprovantes/export-csv
//	       ?date_from=&date_to=&motoboy_id=&zona_id=&status=&baixa_por=
//	       → CSV 21 colunas (espelha relatorios.php:70-93).
//	GET    /motoboy-comprovantes/{id}   → detalhe único
//	DELETE /motoboy-comprovantes/{id}   → soft-delete (zera foto_url/foto_path)
//	                                      exige header `X-Confirm: DELETE`.
//
// O soft-delete preserva linhas para auditoria — `baixa_por`, `created_at`,
// `tipo_pgto`, `pedido_id` continuam acessíveis para histórico. A listagem
// filtra `foto_url != ''` para esconder os removidos.
package handlers

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

type MotoboyComprovantesHandler struct{ Pool *pgxpool.Pool }

// ─── Tipos de resposta ───────────────────────────────────────────────────

// ComprovanteItem — linha de sz_motoboy_comprovantes + nome do motoboy.
type ComprovanteItem struct {
	ID          int64  `json:"id"`
	PedidoID    int64  `json:"pedido_id"`
	WcOrderID   int64  `json:"wc_order_id"`
	MotoboyID   int64  `json:"motoboy_id"`
	MotoboyNome string `json:"motoboy_nome"`
	TipoPgto    string `json:"tipo_pgto"`
	FotoURL     string `json:"foto_url"`
	FotoPath    string `json:"foto_path"`
	BaixaPor    string `json:"baixa_por"`
	CreatedAt   string `json:"created_at"`
}

// ─── Helpers ─────────────────────────────────────────────────────────────

func (h *MotoboyComprovantesHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// parseComprovanteID extrai chi URLParam "id" como int64 positivo.
// Local para não colidir com parseIDParam de cod_saques.go (mesmo package).
func parseComprovanteID(r *http.Request) (int64, bool) {
	s := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// allowedTipoPgto valida o filtro de tipo contra o enum aceito pela tabela.
// Retorna "" para "todos" (sem filtro).
func allowedTipoPgto(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "dinheiro", "pix", "cartao":
		return strings.ToLower(s)
	default:
		return ""
	}
}

// allowedBaixaPor valida o filtro baixa_por. Retorna "" para "todos".
func allowedBaixaPor(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "motoboy", "admin":
		return strings.ToLower(s)
	default:
		return ""
	}
}

// allowedStatusPedido valida o filtro de status do pedido.
// Retorna "" para "todos" (sem filtro).
func allowedStatusPedido(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "agendado", "embalado", "em_rota", "entregue", "frustrado", "cancelado":
		return strings.ToLower(s)
	default:
		return ""
	}
}

// fmtBRL formata um float como "1.234,56" (padrão pt-BR, sem prefixo R$).
// Espelha number_format($v,2,',','.') do PHP em relatorios.php:83-89.
func fmtBRL(v float64) string {
	// Produz "1234.56" via Sprintf, depois insere separadores pt-BR.
	s := fmt.Sprintf("%.2f", v)
	// Divide em parte inteira + decimal.
	dot := strings.LastIndex(s, ".")
	intPart := s[:dot]
	decPart := s[dot+1:]

	// Aplica separador de milhar "." a cada 3 dígitos da parte inteira.
	neg := strings.HasPrefix(intPart, "-")
	if neg {
		intPart = intPart[1:]
	}
	var out []byte
	for i, c := range []byte(intPart) {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			out = append(out, '.')
		}
		out = append(out, c)
	}
	result := string(out) + "," + decPart
	if neg {
		result = "-" + result
	}
	return result
}

// ─── List ────────────────────────────────────────────────────────────────

// List devolve comprovantes filtrados (galeria de fotos).
//
// GET /motoboy-comprovantes
//
//	?motoboy_id=&pedido_id=&tipo=&baixa_por=&date_from=&date_to=&limit=200
//
// Todos os filtros são opcionais. Filtra `foto_url != ''` para esconder
// soft-deletes.
func (h *MotoboyComprovantesHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_motoboy_comprovantes") {
		httpx.JSON(w, 200, map[string]any{"items": []ComprovanteItem{}, "count": 0})
		return
	}

	q := r.URL.Query()
	motoboyID, _ := strconv.ParseInt(q.Get("motoboy_id"), 10, 64)
	pedidoID, _ := strconv.ParseInt(q.Get("pedido_id"), 10, 64)
	wcOrderID, _ := strconv.ParseInt(q.Get("wc_order_id"), 10, 64)
	tipo := allowedTipoPgto(q.Get("tipo"))
	baixaPor := allowedBaixaPor(q.Get("baixa_por"))
	dateFrom := strings.TrimSpace(q.Get("date_from"))
	dateTo := strings.TrimSpace(q.Get("date_to"))
	limit := clampLimit(q.Get("limit"), 200, 500)

	// Sanitização das datas: aceita só YYYY-MM-DD; caso contrário, ignora.
	if dateFrom != "" && !isISODate(dateFrom) {
		dateFrom = ""
	}
	if dateTo != "" && !isISODate(dateTo) {
		dateTo = ""
	}

	// Query com filtros condicionais via "$N = 0 OR coluna = $N" / "$N = ''".
	// Mantemos uma única query parametrizada (sem string concat dinâmica)
	// para evitar SQL-injection acidental e simplificar manutenção.
	//
	// NOTA INTENCIONAL: baixa_por aqui filtra c.baixa_por (coluna do comprovante).
	// Stats/ExportCSV filtram mp.baixa_por (coluna do pedido — paridade com relatorios.php).
	// Os valores geralmente coincidem mas são colunas distintas.
	rows, err := h.Pool.Query(ctx,
		`SELECT
			c.id,
			COALESCE(c.pedido_id, 0),
			COALESCE(c.wc_order_id, 0),
			COALESCE(c.motoboy_id, 0),
			COALESCE(m.nome, '')              AS motoboy_nome,
			COALESCE(c.tipo_pgto, ''),
			COALESCE(c.foto_url, ''),
			COALESCE(c.foto_path, ''),
			COALESCE(c.baixa_por, ''),
			c.created_at::text
		   FROM sz_motoboy_comprovantes c
		   LEFT JOIN sz_motoboys m ON m.id = c.motoboy_id
		  WHERE COALESCE(c.foto_url, '') <> ''
		    AND ($1 = 0 OR c.motoboy_id   = $1)
		    AND ($2 = 0 OR c.pedido_id    = $2)
		    AND ($3 = 0 OR c.wc_order_id  = $3)
		    AND ($4 = '' OR c.tipo_pgto   = $4)
		    AND ($5 = '' OR c.baixa_por   = $5)
		    AND ($6 = '' OR c.created_at::date >= $6::date)
		    AND ($7 = '' OR c.created_at::date <= $7::date)
		  ORDER BY c.created_at DESC, c.id DESC
		  LIMIT $8`,
		motoboyID, pedidoID, wcOrderID, tipo, baixaPor, dateFrom, dateTo, limit)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []ComprovanteItem{}
	for rows.Next() {
		var it ComprovanteItem
		if err := rows.Scan(
			&it.ID, &it.PedidoID, &it.WcOrderID, &it.MotoboyID, &it.MotoboyNome,
			&it.TipoPgto, &it.FotoURL, &it.FotoPath, &it.BaixaPor, &it.CreatedAt,
		); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
		out = append(out, it)
	}
	httpx.JSON(w, 200, map[string]any{"items": out, "count": len(out)})
}

// ─── Stats (KPIs) ────────────────────────────────────────────────────────────

// ComprovantesStats — 6 KPIs + contador de entregas sem comprovante.
// Espelha relatorios.php:57-64 (sz_motoboy_pedidos + subquery COUNT comprovantes).
type ComprovantesStats struct {
	Total       int64   `json:"total"`
	Entregues   int64   `json:"entregues"`
	Frustrados  int64   `json:"frustrados"`
	EmRota      int64   `json:"em_rota"`
	Receita     float64 `json:"receita"`
	TaxaSucesso float64 `json:"taxa_sucesso"`
	SemComp     int64   `json:"sem_comp"` // entregues sem comprovante
}

// Stats retorna os 6 KPIs + sem_comp via sz_motoboy_pedidos.
//
// GET /motoboy-comprovantes/stats
//
//	?date_from=&date_to=&motoboy_id=&zona_id=&status=&baixa_por=
//
// Filtro de data muda a coluna dependendo do status:
//   - status=entregue  → ts_entregue
//   - status=frustrado → ts_frustrado
//   - outros/vazio     → created_at
//
// Espelha relatorios.php:30-36.
func (h *MotoboyComprovantesHandler) Stats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_motoboy_pedidos") {
		httpx.JSON(w, 200, ComprovantesStats{})
		return
	}

	q := r.URL.Query()
	motoboyID, _ := strconv.ParseInt(q.Get("motoboy_id"), 10, 64)
	zonaID, _ := strconv.ParseInt(q.Get("zona_id"), 10, 64)
	status := allowedStatusPedido(q.Get("status"))
	baixaPor := allowedBaixaPor(q.Get("baixa_por"))
	dateFrom := strings.TrimSpace(q.Get("date_from"))
	dateTo := strings.TrimSpace(q.Get("date_to"))

	if dateFrom != "" && !isISODate(dateFrom) {
		dateFrom = ""
	}
	if dateTo != "" && !isISODate(dateTo) {
		dateTo = ""
	}

	// Coluna de data conforme status (espelha relatorios.php:30-36).
	var dateCol string
	switch status {
	case "entregue":
		dateCol = "mp.ts_entregue"
	case "frustrado":
		dateCol = "mp.ts_frustrado"
	default:
		dateCol = "mp.created_at"
	}

	sql := fmt.Sprintf(`
		SELECT
			COUNT(*)                                                    AS total,
			COUNT(*) FILTER (WHERE mp.status = 'entregue')             AS entregues,
			COUNT(*) FILTER (WHERE mp.status = 'frustrado')            AS frustrados,
			COUNT(*) FILTER (WHERE mp.status = 'em_rota')              AS em_rota,
			COALESCE(SUM(mp.valor_pedido) FILTER (WHERE mp.status = 'entregue'), 0) AS receita,
			COUNT(*) FILTER (
				WHERE mp.status = 'entregue'
				  AND NOT EXISTS (
					SELECT 1 FROM sz_motoboy_comprovantes sc
					 WHERE sc.pedido_id = mp.id
					   AND COALESCE(sc.foto_url, '') <> ''
				  )
			) AS sem_comp
		  FROM sz_motoboy_pedidos mp
		 WHERE ($1 = '' OR %s >= ($1::date))
		   AND ($2 = '' OR %s <= ($2::date + INTERVAL '1 day'))
		   AND ($3 = 0 OR mp.motoboy_id = $3)
		   AND ($4 = 0 OR mp.zona_id    = $4)
		   AND ($5 = '' OR mp.status    = $5)
		   AND ($6 = '' OR mp.baixa_por = $6)`,
		dateCol, dateCol,
	)

	var st ComprovantesStats
	err := h.Pool.QueryRow(ctx, sql,
		dateFrom, dateTo, motoboyID, zonaID, status, baixaPor,
	).Scan(
		&st.Total, &st.Entregues, &st.Frustrados, &st.EmRota, &st.Receita, &st.SemComp,
	)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if st.Total > 0 {
		st.TaxaSucesso = float64(st.Entregues) / float64(st.Total) * 100
		// Arredonda para 1 casa decimal.
		st.TaxaSucesso = float64(int(st.TaxaSucesso*10+0.5)) / 10
	}
	httpx.JSON(w, 200, st)
}

// ─── Export CSV ──────────────────────────────────────────────────────────────

// relatorioPedidoRow — linha da query do relatório COD (sz_motoboy_pedidos + JOINs).
type relatorioPedidoRow struct {
	ID             int64
	WcOrderID      int64
	Status         string
	MotoboyNome    string
	ZonaNome       string
	CdNome         string
	DestNome       string
	DestCep        string
	DestCidade     string
	ValorPedido    float64
	ValorTaxa      float64
	PgtoDinheiro   float64
	PgtoPix        float64
	PgtoCartao     float64
	RecebedorNome  string
	RecebedorCPF   string
	RecebedorTipo  string
	BaixaPor       string
	BaixaAt        string
	NComp          int64
	CreatedAt      string
}

// ExportCSV gera um arquivo CSV com os dados do relatório COD.
//
// GET /motoboy-comprovantes/export-csv
//
//	?date_from=&date_to=&motoboy_id=&zona_id=&status=&baixa_por=
//
// Espelha relatorios.php:70-93: separador ";", UTF-8 BOM, 21 colunas.
// Filtro de data usa a mesma lógica de dateCol do Stats.
func (h *MotoboyComprovantesHandler) ExportCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_motoboy_pedidos") {
		httpx.Err(w, 503, "table_missing", "tabela sz_motoboy_pedidos não migrada")
		return
	}

	q := r.URL.Query()
	motoboyID, _ := strconv.ParseInt(q.Get("motoboy_id"), 10, 64)
	zonaID, _ := strconv.ParseInt(q.Get("zona_id"), 10, 64)
	status := allowedStatusPedido(q.Get("status"))
	baixaPor := allowedBaixaPor(q.Get("baixa_por"))
	dateFrom := strings.TrimSpace(q.Get("date_from"))
	dateTo := strings.TrimSpace(q.Get("date_to"))

	if dateFrom != "" && !isISODate(dateFrom) {
		dateFrom = ""
	}
	if dateTo != "" && !isISODate(dateTo) {
		dateTo = ""
	}

	var dateCol string
	switch status {
	case "entregue":
		dateCol = "mp.ts_entregue"
	case "frustrado":
		dateCol = "mp.ts_frustrado"
	default:
		dateCol = "mp.created_at"
	}

	qSQL := fmt.Sprintf(`
		SELECT
			mp.id,
			COALESCE(mp.wc_order_id, 0),
			COALESCE(mp.status, ''),
			COALESCE(mb.nome, '')         AS motoboy_nome,
			COALESCE(z.nome, '')          AS zona_nome,
			COALESCE(cd.nome, '')         AS cd_nome,
			COALESCE(mp.dest_nome, ''),
			COALESCE(mp.dest_cep, ''),
			COALESCE(mp.dest_cidade, ''),
			COALESCE(mp.valor_pedido, 0),
			COALESCE(mp.valor_taxa, 0),
			COALESCE(mp.pgto_dinheiro, 0),
			COALESCE(mp.pgto_pix, 0),
			COALESCE(mp.pgto_cartao, 0),
			COALESCE(mp.recebedor_nome, ''),
			COALESCE(mp.recebedor_cpf, ''),
			COALESCE(mp.recebedor_tipo, ''),
			COALESCE(mp.baixa_por, ''),
			COALESCE(mp.baixa_at::text, ''),
			(SELECT COUNT(*) FROM sz_motoboy_comprovantes sc WHERE sc.pedido_id = mp.id) AS n_comp,
			COALESCE(mp.created_at::text, '')
		  FROM sz_motoboy_pedidos mp
		  LEFT JOIN sz_motoboys mb ON mb.id = mp.motoboy_id
		  LEFT JOIN sz_motoboy_zonas z ON z.id = mp.zona_id
		  LEFT JOIN sz_motoboy_cds cd ON cd.id = mp.cd_id
		 WHERE ($1 = '' OR %s >= ($1::date))
		   AND ($2 = '' OR %s <= ($2::date + INTERVAL '1 day'))
		   AND ($3 = 0 OR mp.motoboy_id = $3)
		   AND ($4 = 0 OR mp.zona_id    = $4)
		   AND ($5 = '' OR mp.status    = $5)
		   AND ($6 = '' OR mp.baixa_por = $6)
		 ORDER BY mp.created_at DESC
		 LIMIT 5000`,
		dateCol, dateCol,
	)

	rows, err := h.Pool.Query(ctx, qSQL, dateFrom, dateTo, motoboyID, zonaID, status, baixaPor)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	var data []relatorioPedidoRow
	for rows.Next() {
		var rw relatorioPedidoRow
		if err := rows.Scan(
			&rw.ID, &rw.WcOrderID, &rw.Status,
			&rw.MotoboyNome, &rw.ZonaNome, &rw.CdNome,
			&rw.DestNome, &rw.DestCep, &rw.DestCidade,
			&rw.ValorPedido, &rw.ValorTaxa,
			&rw.PgtoDinheiro, &rw.PgtoPix, &rw.PgtoCartao,
			&rw.RecebedorNome, &rw.RecebedorCPF, &rw.RecebedorTipo,
			&rw.BaixaPor, &rw.BaixaAt,
			&rw.NComp, &rw.CreatedAt,
		); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
		data = append(data, rw)
	}

	fname := "relatorio-cod"
	if dateFrom != "" {
		fname += "-" + dateFrom
	}
	if dateTo != "" {
		fname += "-" + dateTo
	}
	fname += ".csv"

	w.Header().Set("Content-Type", "text/csv; charset=UTF-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fname))

	// BOM UTF-8 para Excel reconhecer acentos.
	_, _ = w.Write([]byte("\xEF\xBB\xBF"))

	cw := csv.NewWriter(w)
	cw.Comma = ';'

	// 21 colunas — espelha relatorios.php:74-91.
	_ = cw.Write([]string{
		"ID", "Pedido WC", "Status", "Motoboy", "Zona", "CD",
		"Destinatário", "CEP", "Cidade", "Valor", "Taxa",
		"Pgto Dinheiro", "Pgto PIX", "Pgto Cartão",
		"Recebedor", "CPF Recebedor", "Tipo Recebedor",
		"Baixa Por", "Data Baixa", "Comprovantes", "Data Criação",
	})

	for _, rw := range data {
		_ = cw.Write([]string{
			strconv.FormatInt(rw.ID, 10),
			strconv.FormatInt(rw.WcOrderID, 10),
			rw.Status,
			rw.MotoboyNome,
			rw.ZonaNome,
			rw.CdNome,
			rw.DestNome,
			rw.DestCep,
			rw.DestCidade,
			fmtBRL(rw.ValorPedido),
			fmtBRL(rw.ValorTaxa),
			fmtBRL(rw.PgtoDinheiro),
			fmtBRL(rw.PgtoPix),
			fmtBRL(rw.PgtoCartao),
			rw.RecebedorNome,
			rw.RecebedorCPF,
			rw.RecebedorTipo,
			rw.BaixaPor,
			rw.BaixaAt,
			strconv.FormatInt(rw.NComp, 10),
			rw.CreatedAt,
		})
	}
	cw.Flush()
}

// ─── Get ─────────────────────────────────────────────────────────────────

// Get retorna um comprovante por id.
// GET /motoboy-comprovantes/{id}
func (h *MotoboyComprovantesHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := parseComprovanteID(r)
	if !ok {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_motoboy_comprovantes") {
		httpx.Err(w, 503, "table_missing", "tabela sz_motoboy_comprovantes não migrada")
		return
	}

	var it ComprovanteItem
	err := h.Pool.QueryRow(ctx,
		`SELECT
			c.id,
			COALESCE(c.pedido_id, 0),
			COALESCE(c.wc_order_id, 0),
			COALESCE(c.motoboy_id, 0),
			COALESCE(m.nome, ''),
			COALESCE(c.tipo_pgto, ''),
			COALESCE(c.foto_url, ''),
			COALESCE(c.foto_path, ''),
			COALESCE(c.baixa_por, ''),
			c.created_at::text
		   FROM sz_motoboy_comprovantes c
		   LEFT JOIN sz_motoboys m ON m.id = c.motoboy_id
		  WHERE c.id = $1
		  LIMIT 1`, id).Scan(
		&it.ID, &it.PedidoID, &it.WcOrderID, &it.MotoboyID, &it.MotoboyNome,
		&it.TipoPgto, &it.FotoURL, &it.FotoPath, &it.BaixaPor, &it.CreatedAt,
	)
	if err != nil {
		httpx.Err(w, 404, "not_found", "comprovante não encontrado")
		return
	}
	httpx.JSON(w, 200, it)
}

// ─── Delete (soft) ───────────────────────────────────────────────────────

// Delete remove logicamente o comprovante: zera foto_url e foto_path.
// Mantém a linha para auditoria (id, pedido_id, baixa_por, created_at).
//
// DELETE /motoboy-comprovantes/{id}
// Requer header `X-Confirm: DELETE` (defesa contra DELETE acidental do cliente).
func (h *MotoboyComprovantesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if strings.ToUpper(strings.TrimSpace(r.Header.Get("X-Confirm"))) != "DELETE" {
		httpx.Err(w, 428, "confirm_required",
			"operação destrutiva requer header X-Confirm: DELETE")
		return
	}
	id, ok := parseComprovanteID(r)
	if !ok {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_motoboy_comprovantes") {
		httpx.Err(w, 503, "table_missing", "tabela sz_motoboy_comprovantes não migrada")
		return
	}

	tag, err := h.Pool.Exec(ctx,
		`UPDATE sz_motoboy_comprovantes
		 SET foto_url = '', foto_path = ''
		 WHERE id = $1`, id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.Err(w, 404, "not_found", "comprovante não encontrado")
		return
	}
	httpx.JSON(w, 200, map[string]any{"ok": true, "id": id, "deleted": "soft"})
}
