// Package handlers — catálogo global de transportadoras (Melhor Envio) para
// expedição. Admin escolhe quais companies/services ficam visíveis para
// TODOS os produtores; a seleção é o teto — o mecanismo de preferida/bloqueada
// por produtor (go/portal freight.go) só pode restringir dentro dela.
//
// Rotas:
//
//	GET  /expedicao/carriers/live      → catálogo AO VIVO da ME (proxy p/ labels-service)
//	GET  /expedicao/carriers/catalog   → seleção salva (senderzz_enabled_carriers_map)
//	PUT  /expedicao/carriers/catalog   → salva seleção (companies+services habilitados)
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

type CarriersHandler struct {
	Pool             *pgxpool.Pool
	LabelsServiceURL string
}

const carriersCatalogOptionKey = "senderzz_enabled_carriers_map"

type carrierService struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type,omitempty"`
	Enabled bool   `json:"enabled"`
}

type carrierCompany struct {
	ID       int              `json:"id"`
	Name     string           `json:"name"`
	Picture  string           `json:"picture,omitempty"`
	Enabled  bool             `json:"enabled"`
	Services []carrierService `json:"services"`
}

// LiveCatalog busca o catálogo AO VIVO na ME (via labels-service) e marca cada
// company/service com o estado salvo em senderzz_enabled_carriers_map, para a
// UI renderizar checkboxes já pré-marcados.
func (h *CarriersHandler) LiveCatalog(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if h.LabelsServiceURL == "" {
		httpx.Err(w, 503, "labels_service_unavailable",
			"LABELS_SERVICE_URL não configurado; configure a variável de ambiente e reinicie o admin-service")
		return
	}

	url := h.LabelsServiceURL + "/internal/me/carriers"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		httpx.Err(w, 500, "internal_error", "erro ao contatar labels-service")
		return
	}
	if secret, ok := internalSecretHeader(); ok {
		req.Header.Set("X-Internal-Secret", secret)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		httpx.Err(w, 502, "labels_service_error", "falha ao contatar labels-service: "+err.Error())
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		httpx.Err(w, resp.StatusCode, "labels_service_error", string(body))
		return
	}

	var raw struct {
		Companies []struct {
			ID       int    `json:"id"`
			Name     string `json:"name"`
			Picture  string `json:"picture"`
			Services []struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
				Type string `json:"type"`
			} `json:"services"`
		} `json:"companies"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		httpx.Err(w, 502, "labels_service_error", "resposta inválida do labels-service")
		return
	}

	saved := h.loadCatalog(ctx)
	out := make([]carrierCompany, 0, len(raw.Companies))
	for _, c := range raw.Companies {
		savedCo, hasCo := saved[fmt.Sprint(c.ID)]
		company := carrierCompany{ID: c.ID, Name: c.Name, Picture: c.Picture, Enabled: hasCo && savedCo.Enabled}
		for _, s := range c.Services {
			enabled := false
			if hasCo {
				if se, ok := savedCo.Services[fmt.Sprint(s.ID)]; ok {
					enabled = se
				}
			}
			company.Services = append(company.Services, carrierService{ID: s.ID, Name: s.Name, Type: s.Type, Enabled: enabled})
		}
		out = append(out, company)
	}
	httpx.JSON(w, 200, map[string]any{"companies": out})
}

// Catalog devolve só a seleção salva (sem chamar a ME) — usado pelo portal
// indiretamente via FreightHandler.carriers() e pela própria tela admin ao
// recarregar sem precisar bater na ME de novo.
func (h *CarriersHandler) Catalog(w http.ResponseWriter, r *http.Request) {
	saved := h.loadCatalog(r.Context())
	httpx.JSON(w, 200, map[string]any{"companies": saved})
}

type saveCatalogService struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Enabled bool   `json:"enabled"`
}

type saveCatalogCompany struct {
	ID       int                  `json:"id"`
	Name     string               `json:"name"`
	Picture  string               `json:"picture"`
	Enabled  bool                 `json:"enabled"`
	Services []saveCatalogService `json:"services"`
}

// SaveCatalog persiste a seleção admin (quais companies/services aparecem para
// todos os produtores) em senderzz_options[senderzz_enabled_carriers_map].
func (h *CarriersHandler) SaveCatalog(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Companies []saveCatalogCompany `json:"companies"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}

	type storedService struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		Enabled bool   `json:"enabled"`
	}
	type storedCompany struct {
		Name     string                   `json:"name"`
		Picture  string                   `json:"picture"`
		Enabled  bool                     `json:"enabled"`
		Services map[string]storedService `json:"services"`
	}
	m := make(map[string]storedCompany, len(body.Companies))
	for _, c := range body.Companies {
		sc := storedCompany{Name: c.Name, Picture: c.Picture, Enabled: c.Enabled, Services: map[string]storedService{}}
		for _, s := range c.Services {
			sc.Services[fmt.Sprint(s.ID)] = storedService{Name: s.Name, Type: s.Type, Enabled: s.Enabled}
		}
		m[fmt.Sprint(c.ID)] = sc
	}
	raw, err := json.Marshal(m)
	if err != nil {
		httpx.Err(w, 500, "internal_error", "erro ao serializar catálogo")
		return
	}
	if !tableExistsCached(r.Context(), h.Pool, "senderzz_options") {
		httpx.Err(w, 503, "table_missing", "tabela senderzz_options não migrada")
		return
	}
	_, err = h.Pool.Exec(r.Context(),
		`INSERT INTO senderzz_options (name, value)
		 VALUES ($1, $2)
		 ON CONFLICT (name) DO UPDATE SET value = EXCLUDED.value`,
		carriersCatalogOptionKey, string(raw))
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	h.Catalog(w, r)
}

type loadedCarrierCompany struct {
	Enabled  bool
	Services map[string]bool
}

// loadCatalog lê a seleção salva do banco (graceful — vazio se não migrada/setada).
func (h *CarriersHandler) loadCatalog(ctx context.Context) map[string]loadedCarrierCompany {
	out := map[string]loadedCarrierCompany{}
	if !tableExistsCached(ctx, h.Pool, "senderzz_options") {
		return out
	}
	var raw string
	if err := h.Pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE name=$1`, carriersCatalogOptionKey).Scan(&raw); err != nil {
		return out
	}
	var parsed map[string]struct {
		Enabled  bool `json:"enabled"`
		Services map[string]struct {
			Enabled bool `json:"enabled"`
		} `json:"services"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return out
	}
	for id, c := range parsed {
		svc := map[string]bool{}
		for sid, s := range c.Services {
			svc[sid] = s.Enabled
		}
		out[id] = loadedCarrierCompany{Enabled: c.Enabled, Services: svc}
	}
	return out
}
