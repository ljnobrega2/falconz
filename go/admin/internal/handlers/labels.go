// Package handlers — endpoints para gerenciamento de etiquetas Melhor Envio.
//
// Espelha:
//   - src/Rest/Labels.php (listagem pipeline)
//   - src/Analytics/Margin_Dashboard.php (relatório de margem)
//   - src/Admin/Bulk_Actions/ (ações individuais: gerar, cancelar, reversa)
//
// Endpoints:
//
//	GET  /labels                         → lista paginada (pipeline status + filtros)
//	GET  /labels/kpis                    → 4 KPIs: hoje/processando/entregue/cancelado
//	GET  /labels/margin-report           → relatório de margem (por período)
//	GET  /labels/margin-report/csv       → exportação CSV do relatório de margem
//	GET  /labels/{id}/pdf-url            → URL de download do PDF da etiqueta
//	POST /labels/{id}/generate           → enfileira geração da etiqueta
//	POST /labels/{id}/cancel             → solicita cancelamento da etiqueta
//	POST /labels/{id}/reverse            → enfileira etiqueta reversa
package handlers

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

type LabelsHandler struct{ Pool *pgxpool.Pool }

type label struct {
	ID           int64   `json:"id"`
	WCOrderID    int64   `json:"wc_order_id"`
	MEShipmentID *string `json:"me_shipment_id"`
	Status       string  `json:"status"`
	WCStatus     *string `json:"wc_status"`
	ServiceName  *string `json:"service_name"`
	Carrier      *string `json:"carrier"`
	TrackingCode *string `json:"tracking_code"`
	PrintURL     *string `json:"print_url"`
	CreatedAt    string  `json:"created_at"`
}

// tableExistsLabels verifica existência de tabela no schema public.
func (h *LabelsHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// List retorna lista paginada de etiquetas com filtros.
// GET /labels?status=&wc_status=&search=&limit=100&offset=0
func (h *LabelsHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	status := q.Get("status")
	wcStatus := q.Get("wc_status")
	search := strings.TrimSpace(q.Get("search"))

	ctx := r.Context()

	// Verifica se tabela de pedidos existe para JOIN de status WC e transportadora.
	hasOrders := h.tableExists(ctx, "sz_orders")
	hasOrdersMeta := h.tableExists(ctx, "sz_orders_meta") || h.tableExists(ctx, "wc_orders_meta")
	metaTbl := "sz_orders_meta"
	if !h.tableExists(ctx, "sz_orders_meta") && h.tableExists(ctx, "wc_orders_meta") {
		metaTbl = "wc_orders_meta"
	}

	// Monta query com JOINs opcionais para enriquecer a listagem.
	var sb strings.Builder
	sb.WriteString(`
		SELECT
			l.id,
			l.wc_order_id,
			l.me_shipment_id,
			COALESCE(l.status, 'draft') AS status,
			l.service_name,
			l.tracking_code,
			l.created_at::text,
			l.print_url`)

	if hasOrders {
		sb.WriteString(`,
			COALESCE(o.status, '') AS wc_status`)
	} else {
		sb.WriteString(`,
			''::text AS wc_status`)
	}

	if hasOrdersMeta {
		sb.WriteString(`,
			m_carrier.meta_value AS carrier`)
	} else {
		sb.WriteString(`,
			NULL::text AS carrier`)
	}

	sb.WriteString(`
		FROM wc_me_labels l`)

	if hasOrders {
		sb.WriteString(`
		LEFT JOIN sz_orders o ON o.id = l.wc_order_id`)
	}
	if hasOrdersMeta {
		sb.WriteString(`
		LEFT JOIN ` + metaTbl + ` m_carrier
			ON m_carrier.order_id = l.wc_order_id
			AND m_carrier.meta_key = '_senderzz_carrier_name'`)
	}

	sb.WriteString(`
		WHERE 1=1`)

	args := []any{}
	argN := 1

	if status != "" {
		sb.WriteString(fmt.Sprintf(` AND l.status = $%d`, argN))
		args = append(args, status)
		argN++
	}
	if wcStatus != "" && hasOrders {
		// Normaliza: remove prefixo "wc-" pois sz_orders armazena sem ele.
		plain := wcStatus
		if strings.HasPrefix(plain, "wc-") {
			plain = plain[3:]
		}
		sb.WriteString(fmt.Sprintf(` AND o.status = $%d`, argN))
		args = append(args, plain)
		argN++
	}
	if search != "" {
		// Busca por ID de pedido WC ou e-mail/nome do cliente.
		if hasOrders {
			sb.WriteString(fmt.Sprintf(` AND (
				l.wc_order_id::text = $%d
				OR LOWER(COALESCE(o.billing_email,'')) LIKE $%d
				OR LOWER(COALESCE(o.customer_name,'')) LIKE $%d
			)`, argN, argN+1, argN+1))
			args = append(args, search, "%"+strings.ToLower(search)+"%")
			argN += 2
		} else {
			sb.WriteString(fmt.Sprintf(` AND l.wc_order_id::text = $%d`, argN))
			args = append(args, search)
			argN++
		}
	}

	// Conta total antes de LIMIT/OFFSET.
	countSQL := "SELECT COUNT(*) FROM (" + sb.String() + ") _c"

	sb.WriteString(fmt.Sprintf(` ORDER BY l.id DESC LIMIT $%d OFFSET $%d`, argN, argN+1))
	args = append(args, limit, offset)

	rows, err := h.Pool.Query(ctx, sb.String(), args...)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []label{}
	for rows.Next() {
		var l label
		var wcSt string
		if err := rows.Scan(
			&l.ID, &l.WCOrderID, &l.MEShipmentID, &l.Status,
			&l.ServiceName, &l.TrackingCode, &l.CreatedAt, &l.PrintURL,
			&wcSt, &l.Carrier,
		); err == nil {
			if wcSt != "" {
				l.WCStatus = &wcSt
			}
			out = append(out, l)
		}
	}

	var total int64
	countArgs := args[:len(args)-2] // remove limit e offset
	_ = h.Pool.QueryRow(ctx, countSQL, countArgs...).Scan(&total)

	httpx.JSON(w, 200, map[string]any{"items": out, "total": total})
}

// labelsKPIs — 4 contadores do painel de etiquetas.
type labelsKPIs struct {
	Hoje        int64 `json:"hoje"`         // todos os status de expedição, hoje
	Processando int64 `json:"processando"`  // wc-processing
	Entregue    int64 `json:"entregue"`     // wc-entregue + wc-completed
	Cancelado   int64 `json:"cancelado"`    // wc-cancelled
}

// KPIs retorna os 4 contadores do painel de etiquetas.
// GET /labels/kpis
func (h *LabelsHandler) KPIs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if !h.tableExists(ctx, "sz_orders") {
		httpx.JSON(w, 200, labelsKPIs{})
		return
	}

	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		loc = time.UTC
	}
	today := time.Now().In(loc).Format("2006-01-02")

	// Statuses de expedição do dia (sz_orders armazena sem prefixo "wc-").
	expeditionStatuses := []string{"processing", "entregue", "completed", "cancelled", "pending", "on-hold"}
	placeholders := make([]string, len(expeditionStatuses))
	args := make([]any, len(expeditionStatuses))
	for i, s := range expeditionStatuses {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = s
	}
	inClause := strings.Join(placeholders, ",")

	var k labelsKPIs

	// Hoje: todos os statuses de expedição criados hoje.
	_ = h.Pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM sz_orders
		 WHERE status IN (%s)
		   AND created_at::date = $%d`, inClause, len(args)+1),
		append(args, today)...).Scan(&k.Hoje)

	// Processando: total sem filtro de data.
	_ = h.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM sz_orders WHERE status = 'processing'`,
	).Scan(&k.Processando)

	// Entregue: wc-entregue + wc-completed (sem "wc-" em sz_orders).
	_ = h.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM sz_orders WHERE status IN ('entregue','completed')`,
	).Scan(&k.Entregue)

	// Cancelado: wc-cancelled.
	_ = h.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM sz_orders WHERE status = 'cancelled'`,
	).Scan(&k.Cancelado)

	httpx.JSON(w, 200, k)
}

// marginReportRow — linha individual do relatório de margem.
type marginReportRow struct {
	Number    int64   `json:"number"`
	Date      string  `json:"date"`
	ClassName string  `json:"class_name"`
	Carrier   string  `json:"carrier"`
	Charged   float64 `json:"charged"`
	RealCost  float64 `json:"real_cost"`
	Margin    float64 `json:"margin"`
	Status    string  `json:"status"`
}

// byClassEntry — agrupamento por classe de entrega.
type byClassEntry struct {
	ClassName string  `json:"class_name"`
	Orders    int64   `json:"orders"`
	Charged   float64 `json:"charged"`
	Real      float64 `json:"real"`
	Margin    float64 `json:"margin"`
	PctMargin float64 `json:"pct_margin"`
}

// byCarrierEntry — agrupamento por transportadora.
type byCarrierEntry struct {
	Carrier string  `json:"carrier"`
	Orders  int64   `json:"orders"`
	Margin  float64 `json:"margin"`
}

// marginReportResponse — resposta do relatório de margem.
type marginReportResponse struct {
	DateFrom     string          `json:"date_from"`
	DateTo       string          `json:"date_to"`
	TotalOrders  int             `json:"total_orders"`
	TotalCharged float64         `json:"total_charged"`
	TotalReal    float64         `json:"total_real"`
	TotalMargin  float64         `json:"total_margin"`
	ByClass      []byClassEntry  `json:"by_class"`
	ByCarrier    []byCarrierEntry `json:"by_carrier"`
	Rows         []marginReportRow `json:"rows"`
}

// fetchMarginData executa a query de margem e agrega os resultados.
// Espelha Margin_Dashboard::fetch_data() do PHP.
func (h *LabelsHandler) fetchMarginData(ctx context.Context, dateFrom, dateTo string) marginReportResponse {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	if dateFrom == "" {
		dateFrom = fmt.Sprintf("%d-%02d-01", now.Year(), now.Month())
	}
	if dateTo == "" {
		dateTo = now.Format("2006-01-02")
	}

	resp := marginReportResponse{
		DateFrom:  dateFrom,
		DateTo:    dateTo,
		ByClass:   []byClassEntry{},
		ByCarrier: []byCarrierEntry{},
		Rows:      []marginReportRow{},
	}

	// Detecta tabelas disponíveis — espelha comportamento HPOS-aware do PHP.
	orderTbl := ""
	metaTbl := ""
	if h.tableExists(ctx, "sz_orders") && (h.tableExists(ctx, "sz_orders_meta") || h.tableExists(ctx, "wc_orders_meta")) {
		orderTbl = "sz_orders"
		if h.tableExists(ctx, "sz_orders_meta") {
			metaTbl = "sz_orders_meta"
		} else {
			metaTbl = "wc_orders_meta"
		}
	} else {
		// Degradação graciosa: sem mirror, sem relatório.
		return resp
	}

	// Valida datas.
	if !isISODate(dateFrom) || !isISODate(dateTo) {
		return resp
	}

	// Query com MAX(CASE WHEN) para evitar N+1 — espelha PHP.
	qSQL := `
		SELECT
			o.id                                                                    AS order_id,
			COALESCE(o.created_at::date::text, '')                           AS order_date,
			COALESCE(o.status, '')                                                  AS order_status,
			MAX(CASE WHEN m.meta_key = '_senderzz_service_fee'
				THEN CAST(m.meta_value AS NUMERIC(10,2)) END)                       AS margin,
			MAX(CASE WHEN m.meta_key = '_senderzz_shipping_charged'
				THEN CAST(m.meta_value AS NUMERIC(10,2)) END)                       AS charged,
			MAX(CASE WHEN m.meta_key = '_senderzz_shipping_real_cost'
				THEN CAST(m.meta_value AS NUMERIC(10,2)) END)                       AS real_cost,
			COALESCE(MAX(CASE WHEN m.meta_key = '_senderzz_product_shipping_class_name'
				THEN m.meta_value END), 'Sem classe')                               AS class_name,
			COALESCE(MAX(CASE WHEN m.meta_key = '_senderzz_carrier_name'
				THEN m.meta_value END), '')                                          AS carrier,
			COALESCE(MAX(CASE WHEN m.meta_key = '_senderzz_delivery_mode'
				THEN m.meta_value END), '')                                          AS delivery_mode,
			COALESCE(MAX(CASE WHEN m.meta_key = '_senderzz_motoboy_flow_status'
				THEN m.meta_value END), '')                                          AS motoboy_flow_status,
			COALESCE(MAX(CASE WHEN m.meta_key = '_senderzz_motoboy_status'
				THEN m.meta_value END), '')                                          AS motoboy_status
		FROM ` + orderTbl + ` o
		INNER JOIN ` + metaTbl + ` m ON m.order_id = o.id
			AND m.meta_key IN (
				'_senderzz_service_fee',
				'_senderzz_shipping_charged',
				'_senderzz_shipping_real_cost',
				'_senderzz_product_shipping_class_name',
				'_senderzz_carrier_name',
				'_senderzz_delivery_mode',
				'_senderzz_motoboy_flow_status',
				'_senderzz_motoboy_status'
			)
		WHERE o.created_at::date BETWEEN $1::date AND $2::date
		GROUP BY o.id, o.created_at, o.status
		HAVING MAX(CASE WHEN m.meta_key = '_senderzz_service_fee'
			THEN CAST(m.meta_value AS NUMERIC(10,2)) END) IS NOT NULL
		ORDER BY o.created_at DESC`

	rows, err := h.Pool.Query(ctx, qSQL, dateFrom, dateTo)
	if err != nil {
		return resp
	}
	defer rows.Close()

	// Mapas para agrupamentos (espelha by_class / by_carrier do PHP).
	type classAcc struct {
		orders  int64
		charged float64
		real    float64
		margin  float64
	}
	type carrierAcc struct {
		orders int64
		margin float64
	}
	byClass := map[string]*classAcc{}
	byCarrier := map[string]*carrierAcc{}

	for rows.Next() {
		var (
			orderID         int64
			orderDate       string
			orderStatus     string
			margin          *float64
			charged         *float64
			realCost        *float64
			className       string
			carrier         string
			deliveryMode    string
			motoboyFlowStat string
			motoboyStatus   string
		)
		if err := rows.Scan(
			&orderID, &orderDate, &orderStatus,
			&margin, &charged, &realCost,
			&className, &carrier,
			&deliveryMode, &motoboyFlowStat, &motoboyStatus,
		); err != nil {
			continue
		}

		// Filtro Motoboy/COD — espelha v333 do PHP.
		carrierLC := strings.ToLower(carrier)
		classLC := strings.ToLower(className)
		if strings.ToLower(deliveryMode) == "motoboy" ||
			motoboyFlowStat != "" ||
			motoboyStatus != "" ||
			strings.Contains(carrierLC, "motoboy") ||
			strings.Contains(carrierLC, "moto boy") ||
			strings.Contains(classLC, "motoboy") ||
			strings.Contains(classLC, "cod") {
			continue
		}

		ch := roundF2(safeF(charged))
		rc := roundF2(safeF(realCost))
		mg := roundF2(safeF(margin))

		// Remove prefixo "wc-" do status (sz_orders armazena sem ele, mas por segurança).
		status := orderStatus
		if strings.HasPrefix(status, "wc-") {
			status = status[3:]
		}

		resp.Rows = append(resp.Rows, marginReportRow{
			Number:    orderID,
			Date:      orderDate,
			ClassName: className,
			Carrier:   carrier,
			Charged:   ch,
			RealCost:  rc,
			Margin:    mg,
			Status:    status,
		})

		resp.TotalCharged += ch
		resp.TotalReal += rc
		resp.TotalMargin += mg

		if _, ok := byClass[className]; !ok {
			byClass[className] = &classAcc{}
		}
		byClass[className].orders++
		byClass[className].charged += ch
		byClass[className].real += rc
		byClass[className].margin += mg

		cKey := carrier
		if cKey == "" {
			cKey = "Sem transportadora"
		}
		if _, ok := byCarrier[cKey]; !ok {
			byCarrier[cKey] = &carrierAcc{}
		}
		byCarrier[cKey].orders++
		byCarrier[cKey].margin += mg
	}

	resp.TotalOrders = len(resp.Rows)
	resp.TotalCharged = roundF2(resp.TotalCharged)
	resp.TotalReal = roundF2(resp.TotalReal)
	resp.TotalMargin = roundF2(resp.TotalMargin)

	// Serializa by_class ordenado por margem desc.
	for cls, acc := range byClass {
		pct := 0.0
		if acc.real > 0 {
			pct = roundF1((acc.margin / acc.real) * 100)
		}
		resp.ByClass = append(resp.ByClass, byClassEntry{
			ClassName: cls,
			Orders:    acc.orders,
			Charged:   roundF2(acc.charged),
			Real:      roundF2(acc.real),
			Margin:    roundF2(acc.margin),
			PctMargin: pct,
		})
	}
	// Ordena by_class por margem desc.
	sortByClassDesc(resp.ByClass)

	for car, acc := range byCarrier {
		resp.ByCarrier = append(resp.ByCarrier, byCarrierEntry{
			Carrier: car,
			Orders:  acc.orders,
			Margin:  roundF2(acc.margin),
		})
	}
	// Ordena by_carrier por margem desc.
	sortByCarrierDesc(resp.ByCarrier)

	return resp
}

// MarginReport retorna o relatório de margem para o período.
// GET /labels/margin-report?date_from=YYYY-MM-DD&date_to=YYYY-MM-DD
func (h *LabelsHandler) MarginReport(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	dateFrom := strings.TrimSpace(q.Get("date_from"))
	dateTo := strings.TrimSpace(q.Get("date_to"))

	data := h.fetchMarginData(r.Context(), dateFrom, dateTo)
	httpx.JSON(w, 200, data)
}

// MarginReportCSV exporta o relatório de margem em CSV.
// GET /labels/margin-report/csv?date_from=YYYY-MM-DD&date_to=YYYY-MM-DD
// Espelha Margin_Dashboard::handle_export() e fmtBRL() de motoboy_comprovantes.go.
func (h *LabelsHandler) MarginReportCSV(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	dateFrom := strings.TrimSpace(q.Get("date_from"))
	dateTo := strings.TrimSpace(q.Get("date_to"))

	data := h.fetchMarginData(r.Context(), dateFrom, dateTo)

	fname := fmt.Sprintf("senderzz-margem-%s.csv", data.DateTo)
	w.Header().Set("Content-Type", "text/csv; charset=UTF-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fname))

	// BOM UTF-8 para Excel reconhecer acentos (espelha motoboy_comprovantes.go).
	_, _ = w.Write([]byte("\xEF\xBB\xBF"))

	cw := csv.NewWriter(w)
	cw.Comma = ';'

	_ = cw.Write([]string{"Pedido", "Data", "Classe Entrega", "Transportadora",
		"Cobrado R$", "Custo Real R$", "Margem R$", "Markup %", "Status"})

	for _, row := range data.Rows {
		markup := "—"
		if row.RealCost > 0 {
			markup = fmt.Sprintf("%.1f%%", (row.Margin/row.RealCost)*100)
		}
		_ = cw.Write([]string{
			strconv.FormatInt(row.Number, 10),
			row.Date,
			row.ClassName,
			row.Carrier,
			fmtBRL(row.Charged),
			fmtBRL(row.RealCost),
			fmtBRL(row.Margin),
			markup,
			row.Status,
		})
	}
	cw.Flush()
}

// parseLabelID extrai chi URLParam "id" como int64 positivo.
func parseLabelID(r *http.Request) (int64, bool) {
	s := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// pdfURLResponse — resposta do endpoint de URL de PDF.
type pdfURLResponse struct {
	ID       int64   `json:"id"`
	PrintURL *string `json:"print_url"`
}

// PDFUrl retorna a URL de download do PDF da etiqueta.
// GET /labels/{id}/pdf-url
// Lê print_url da tabela wc_me_labels; sem fallback para metadado WC (a coluna já
// é preenchida pelo worker PHP que processa a geração).
func (h *LabelsHandler) PDFUrl(w http.ResponseWriter, r *http.Request) {
	id, ok := parseLabelID(r)
	if !ok {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	ctx := r.Context()
	if !h.tableExists(ctx, "wc_me_labels") {
		httpx.Err(w, 503, "table_missing", "tabela wc_me_labels não migrada")
		return
	}

	var res pdfURLResponse
	err := h.Pool.QueryRow(ctx,
		`SELECT id, print_url FROM wc_me_labels WHERE id = $1`, id).
		Scan(&res.ID, &res.PrintURL)
	if err != nil {
		httpx.Err(w, 404, "not_found", "etiqueta não encontrada")
		return
	}
	if res.PrintURL == nil || *res.PrintURL == "" {
		httpx.Err(w, 404, "pdf_not_ready", "PDF ainda não gerado para esta etiqueta")
		return
	}
	httpx.JSON(w, 200, res)
}

// Generate enfileira geração de etiqueta para um pedido individual.
// POST /labels/{id}/generate
// Espelha padrão de bulk_actions.go: não chama ME API diretamente — atualiza
// status para 'queued' e o worker PHP processa.
func (h *LabelsHandler) Generate(w http.ResponseWriter, r *http.Request) {
	id, ok := parseLabelID(r)
	if !ok {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	ctx := r.Context()
	if !h.tableExists(ctx, "wc_me_labels") {
		httpx.Err(w, 503, "table_missing", "tabela wc_me_labels não migrada")
		return
	}

	now := time.Now()
	tag, err := h.Pool.Exec(ctx,
		`UPDATE wc_me_labels
		 SET status = 'queued', updated_at = $1
		 WHERE id = $2 AND status NOT IN ('queued','processing')`,
		now, id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if tag.RowsAffected() == 0 {
		// Verifica se a etiqueta existe.
		var exists bool
		_ = h.Pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM wc_me_labels WHERE id = $1)`, id).Scan(&exists)
		if !exists {
			httpx.Err(w, 404, "not_found", "etiqueta não encontrada")
			return
		}
		httpx.JSON(w, 200, map[string]any{"ok": false, "message": "etiqueta já está na fila ou em processamento"})
		return
	}

	var l label
	_ = h.Pool.QueryRow(ctx,
		`SELECT id, wc_order_id, me_shipment_id, status, service_name, tracking_code, created_at::text, print_url
		 FROM wc_me_labels WHERE id = $1`, id).
		Scan(&l.ID, &l.WCOrderID, &l.MEShipmentID, &l.Status,
			&l.ServiceName, &l.TrackingCode, &l.CreatedAt, &l.PrintURL)

	httpx.JSON(w, 200, map[string]any{"ok": true, "label": l})
}

// Cancel solicita cancelamento de uma etiqueta.
// POST /labels/{id}/cancel
// Atualiza status para 'cancel_requested' — o worker PHP processa o cancelamento
// na API Melhor Envio e atualiza para 'cancelled'.
func (h *LabelsHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	id, ok := parseLabelID(r)
	if !ok {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	ctx := r.Context()
	if !h.tableExists(ctx, "wc_me_labels") {
		httpx.Err(w, 503, "table_missing", "tabela wc_me_labels não migrada")
		return
	}

	now := time.Now()
	tag, err := h.Pool.Exec(ctx,
		`UPDATE wc_me_labels
		 SET status = 'cancel_requested', updated_at = $1
		 WHERE id = $2 AND status NOT IN ('cancel_requested','cancelled')`,
		now, id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		_ = h.Pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM wc_me_labels WHERE id = $1)`, id).Scan(&exists)
		if !exists {
			httpx.Err(w, 404, "not_found", "etiqueta não encontrada")
			return
		}
		httpx.JSON(w, 200, map[string]any{"ok": false, "message": "etiqueta já cancelada ou cancelamento já solicitado"})
		return
	}

	httpx.JSON(w, 200, map[string]any{"ok": true, "id": id, "status": "cancel_requested"})
}

// Reverse enfileira geração de etiqueta reversa para o pedido.
// POST /labels/{id}/reverse
// Insere nova linha em wc_me_labels com mode='reverse' e status='queued'.
// O worker PHP processa via process_reverse_shipping().
func (h *LabelsHandler) Reverse(w http.ResponseWriter, r *http.Request) {
	id, ok := parseLabelID(r)
	if !ok {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	ctx := r.Context()
	if !h.tableExists(ctx, "wc_me_labels") {
		httpx.Err(w, 503, "table_missing", "tabela wc_me_labels não migrada")
		return
	}

	// Busca o wc_order_id da etiqueta original.
	var wcOrderID int64
	err := h.Pool.QueryRow(ctx,
		`SELECT wc_order_id FROM wc_me_labels WHERE id = $1`, id).Scan(&wcOrderID)
	if err != nil {
		httpx.Err(w, 404, "not_found", "etiqueta não encontrada")
		return
	}

	now := time.Now()
	// Insere etiqueta reversa; ON CONFLICT para evitar duplicata por pedido+modo.
	var newID int64
	err = h.Pool.QueryRow(ctx,
		`INSERT INTO wc_me_labels (wc_order_id, status, mode, created_at, updated_at)
		 VALUES ($1, 'queued', 'reverse', $2, $2)
		 ON CONFLICT DO NOTHING
		 RETURNING id`, wcOrderID, now).Scan(&newID)
	if err != nil {
		// ON CONFLICT DO NOTHING não retorna linha — verifica se já existe.
		var existingID int64
		_ = h.Pool.QueryRow(ctx,
			`SELECT id FROM wc_me_labels WHERE wc_order_id = $1 AND mode = 'reverse' LIMIT 1`,
			wcOrderID).Scan(&existingID)
		if existingID > 0 {
			httpx.JSON(w, 200, map[string]any{"ok": false, "message": "etiqueta reversa já solicitada", "label_id": existingID})
			return
		}
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 200, map[string]any{"ok": true, "label_id": newID, "wc_order_id": wcOrderID, "status": "queued", "mode": "reverse"})
}

// ─── Helpers numéricos ───────────────────────────────────────────────────────

// safeF retorna o float64 apontado ou 0 se nil.
func safeF(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}

// roundF2 arredonda para 2 casas decimais.
func roundF2(f float64) float64 {
	return float64(int64(f*100+0.5)) / 100
}

// roundF1 arredonda para 1 casa decimal.
func roundF1(f float64) float64 {
	return float64(int64(f*10+0.5)) / 10
}

// sortByClassDesc ordena slice de byClassEntry por Margin desc (insertion sort — n pequeno).
func sortByClassDesc(s []byClassEntry) {
	for i := 1; i < len(s); i++ {
		key := s[i]
		j := i - 1
		for j >= 0 && s[j].Margin < key.Margin {
			s[j+1] = s[j]
			j--
		}
		s[j+1] = key
	}
}

// sortByCarrierDesc ordena slice de byCarrierEntry por Margin desc.
func sortByCarrierDesc(s []byCarrierEntry) {
	for i := 1; i < len(s); i++ {
		key := s[i]
		j := i - 1
		for j >= 0 && s[j].Margin < key.Margin {
			s[j+1] = s[j]
			j--
		}
		s[j+1] = key
	}
}
