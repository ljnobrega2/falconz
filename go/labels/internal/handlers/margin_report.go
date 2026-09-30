package handlers

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/senderzz/labels-service/internal/httpx"
	"github.com/shopspring/decimal"
)

type marginLabel struct {
	OrderID     int64
	LabelID     int64
	ShipmentID  string
	Charged     decimal.Decimal
	OrderDate   string
	ServiceName string
}

type marginReportRow struct {
	Number   int64   `json:"number"`
	Date     string  `json:"date"`
	Carrier  string  `json:"carrier"`
	Charged  float64 `json:"charged"`
	RealCost float64 `json:"real_cost"`
	Margin   float64 `json:"margin"`
	Status   string  `json:"status"`
}

type marginReportResponse struct {
	DateFrom     string            `json:"date_from"`
	DateTo       string            `json:"date_to"`
	TotalOrders  int               `json:"total_orders"`
	TotalCharged float64           `json:"total_charged"`
	TotalReal    float64           `json:"total_real"`
	TotalMargin  float64           `json:"total_margin"`
	MarginPct    float64           `json:"margin_pct"`
	Unavailable  int               `json:"unavailable"`
	Rows         []marginReportRow `json:"rows"`
}

type resolvedMarginLabel struct {
	label    marginLabel
	status   string
	realCost decimal.Decimal
	err      error
}

// InternalMarginReport calcula o rendimento real da operacao de expedicao.
// O valor cobrado vem da etiqueta (preco fixo/markup cobrado do produtor) e o
// custo vem ao vivo do pedido na API do Melhor Envio. Remessas efetivamente
// canceladas na ME sao reembolsadas e, portanto, nao entram no resultado.
func (h *LabelHandler) InternalMarginReport(w http.ResponseWriter, r *http.Request) {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	dateFrom := strings.TrimSpace(r.URL.Query().Get("date_from"))
	dateTo := strings.TrimSpace(r.URL.Query().Get("date_to"))
	if dateFrom == "" {
		dateFrom = now.AddDate(0, 0, -30).Format("2006-01-02")
	}
	if dateTo == "" {
		dateTo = now.Format("2006-01-02")
	}
	if _, err := time.Parse("2006-01-02", dateFrom); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "date_from invalida")
		return
	}
	if _, err := time.Parse("2006-01-02", dateTo); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "date_to invalida")
		return
	}

	labels, err := h.marginLabels(r.Context(), dateFrom, dateTo)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao consultar etiquetas")
		return
	}

	resolved := h.resolveMarginLabels(r.Context(), labels)
	report := buildMarginReport(dateFrom, dateTo, resolved)
	httpx.WriteOK(w, map[string]any{"data": report})
}

func (h *LabelHandler) marginLabels(ctx context.Context, dateFrom, dateTo string) ([]marginLabel, error) {
	rows, err := h.db.Query(ctx, `
		SELECT l.wc_order_id, l.id, l.me_shipment_id, COALESCE(l.price, 0),
		       l.created_at::date::text,
		       COALESCE(l.company_name, l.service_name, '')
		  FROM wc_me_labels l
		 -- Lucro da expedição é reconhecido na emissão/compra da etiqueta, não na
		 -- criação do pedido. Uma etiqueta pode ser emitida semanas depois.
		 WHERE l.created_at >= $1::date
		   AND l.created_at < $2::date + interval '1 day'
		   AND l.me_shipment_id IS NOT NULL AND l.me_shipment_id <> ''
		 ORDER BY l.wc_order_id, l.id DESC
		 LIMIT 500`, dateFrom, dateTo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	labels := make([]marginLabel, 0)
	for rows.Next() {
		var item marginLabel
		if err := rows.Scan(&item.OrderID, &item.LabelID, &item.ShipmentID, &item.Charged, &item.OrderDate, &item.ServiceName); err != nil {
			return nil, err
		}
		labels = append(labels, item)
	}
	return labels, rows.Err()
}

func (h *LabelHandler) resolveMarginLabels(ctx context.Context, labels []marginLabel) []resolvedMarginLabel {
	results := make([]resolvedMarginLabel, len(labels))
	sem := make(chan struct{}, 5)
	var wg sync.WaitGroup
	for i, item := range labels {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			status, err := h.me.GetShipmentStatus(ctx, item.ShipmentID)
			results[i] = resolvedMarginLabel{label: item, err: err}
			if err == nil {
				results[i].status = strings.ToLower(strings.TrimSpace(status.Status))
				results[i].realCost = status.Price
			}
		}()
	}
	wg.Wait()
	return results
}

func buildMarginReport(dateFrom, dateTo string, resolved []resolvedMarginLabel) marginReportResponse {
	report := marginReportResponse{DateFrom: dateFrom, DateTo: dateTo, Rows: []marginReportRow{}}
	seenOrder := make(map[int64]bool)
	chargedTotal := decimal.Zero
	realTotal := decimal.Zero

	for _, item := range resolved {
		if item.err != nil {
			report.Unavailable++
			continue
		}
		if item.status == "canceled" || item.status == "cancelled" {
			continue
		}
		// Uma reemissao pode deixar varias tentativas no banco. Como a consulta
		// vem por label id desc, contabilizamos apenas a remessa ativa mais nova.
		if seenOrder[item.label.OrderID] {
			continue
		}
		seenOrder[item.label.OrderID] = true
		margin := item.label.Charged.Sub(item.realCost)
		chargedTotal = chargedTotal.Add(item.label.Charged)
		realTotal = realTotal.Add(item.realCost)
		charged, _ := item.label.Charged.Float64()
		realCost, _ := item.realCost.Float64()
		marginFloat, _ := margin.Float64()
		report.Rows = append(report.Rows, marginReportRow{
			Number: item.label.OrderID, Date: item.label.OrderDate,
			Carrier: item.label.ServiceName, Charged: charged,
			RealCost: realCost, Margin: marginFloat, Status: item.status,
		})
	}

	report.TotalOrders = len(report.Rows)
	report.TotalCharged, _ = chargedTotal.Float64()
	report.TotalReal, _ = realTotal.Float64()
	report.TotalMargin, _ = chargedTotal.Sub(realTotal).Float64()
	if !chargedTotal.IsZero() {
		report.MarginPct, _ = chargedTotal.Sub(realTotal).Div(chargedTotal).Mul(decimal.NewFromInt(100)).Round(2).Float64()
	}
	return report
}
