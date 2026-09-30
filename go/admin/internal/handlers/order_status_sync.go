// AUDIT-2026-07-30 (dono): "preciso de uma opção a nivel admin que consulte
// esses casos caso haja falha eu consiga executar manualmente" — proxy pro
// labels-service, que já sincroniza sz_orders.status automaticamente a cada
// webhook da ME (me_webhook.go). Aqui é só o painel de fallback manual.
package handlers

import (
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/senderzz/admin-service/internal/httpx"
)

// MEStatusMismatches — GET /order-status-sync/mismatches
func (h *BulkActionsHandler) MEStatusMismatches(w http.ResponseWriter, r *http.Request) {
	if h.LabelsServiceURL == "" {
		httpx.Err(w, 503, "labels_service_unavailable", "LABELS_SERVICE_URL não configurado")
		return
	}
	proxyGET(w, r, h.LabelsServiceURL+"/internal/me-status/mismatches")
}

// MEStatusSync — POST /order-status-sync/{order_id}/sync
func (h *BulkActionsHandler) MEStatusSync(w http.ResponseWriter, r *http.Request) {
	if h.LabelsServiceURL == "" {
		httpx.Err(w, 503, "labels_service_unavailable", "LABELS_SERVICE_URL não configurado")
		return
	}
	orderID := chi.URLParam(r, "order_id")
	if orderID == "" {
		httpx.Err(w, 400, "validation", "order_id inválido")
		return
	}
	proxyPOST(w, r, h.LabelsServiceURL+"/internal/me-status/sync/"+orderID)
}

func proxyGET(w http.ResponseWriter, r *http.Request, url string) {
	httpReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, url, nil)
	if err != nil {
		httpx.Err(w, 500, "internal_error", "erro ao contatar labels-service")
		return
	}
	doProxy(w, httpReq)
}

func proxyPOST(w http.ResponseWriter, r *http.Request, url string) {
	httpReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, url, nil)
	if err != nil {
		httpx.Err(w, 500, "internal_error", "erro ao contatar labels-service")
		return
	}
	doProxy(w, httpReq)
}

// AUDIT-2026-07-30 HIGH: labels-service /internal/* passou a aceitar um
// secret compartilhado (X-Internal-Secret) — mesma env dos dois lados.
// Se LABELS_INTERNAL_SECRET não estiver setada aqui, simplesmente não manda
// o header (labels-service também é opt-in enquanto o secret não for
// configurado nos dois serviços — não quebra deploy existente).
func internalSecretHeader() (string, bool) {
	s := strings.TrimSpace(os.Getenv("LABELS_INTERNAL_SECRET"))
	if s == "" {
		return "", false
	}
	return s, true
}

func doProxy(w http.ResponseWriter, httpReq *http.Request) {
	if secret, ok := internalSecretHeader(); ok {
		httpReq.Header.Set("X-Internal-Secret", secret)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		httpx.Err(w, 502, "labels_service_error", "falha ao contatar labels-service: "+err.Error())
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
}
