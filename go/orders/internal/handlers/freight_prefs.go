// freight_prefs.go — aplica ao resultado do freight.Calculate (ME real) o
// mecanismo de preferida/bloqueada por produtor DENTRO do teto do catálogo
// global habilitado pelo admin. Fecha o gap: calc.go nunca conhecia
// producer/class, então preferida/bloqueada nunca era aplicada na cotação real.
//
// Precedência (mesma da tela admin de expedição):
//  1. whitelist GLOBAL (senderzz_enabled_carriers_map, admin-service) — gate:
//     serviço fora dela nunca aparece, mesmo se "preferido" por um produtor.
//  2. bloqueadas do produtor (senderzz_blocked_carriers_map[class]) — remove.
//  3. preferidas do produtor (tp_preferida_map[class]) — restringe ao subconjunto,
//     se não vazio (vazio = "mais barata entre todas" as habilitadas/não-bloqueadas).
//
// Opções ESTIMADAS (fallback, sem token ME / ME indisponível) NUNCA são filtradas
// — não pertencem ao catálogo ME, então whitelist/preferida/bloqueada não se aplicam.
package handlers

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/senderzz/orders-service/internal/freight"
)

const (
	freightOptEnabledCarriers = "senderzz_enabled_carriers_map" // admin-service: carriersCatalogOptionKey
	freightOptPreferida       = "tp_preferida_map"
	freightOptBlocked         = "senderzz_blocked_carriers_map"
)

// applyCarrierPreferences filtra opts (retorno de freight.Calculate) pela
// whitelist global do admin e pelas preferências do produtor (por
// shipping_class_id). Best-effort: qualquer falha de leitura de option
// degrada para "sem restrição adicional" (fail-open, mesma filosofia do
// freight.Calculate — nunca quebra o checkout por causa de config ausente).
func (h *CheckoutHandler) applyCarrierPreferences(ctx context.Context, producerID int64, opts []freight.ServiceOption) []freight.ServiceOption {
	if len(opts) == 0 {
		return opts
	}

	enabled := h.loadEnabledServiceIDs(ctx)
	// Sem seleção global salva ainda → nenhuma opção ME passa (mesmo comportamento
	// de graceful-empty do catálogo admin/portal); fallback estimado não é afetado.
	classID := h.producerClassID(ctx, producerID)
	blocked := h.loadCarrierIDSet(ctx, freightOptBlocked, classID, "bloqueadas")
	preferred := h.loadCarrierIDSet(ctx, freightOptPreferida, classID, "permitidas")

	filtered := make([]freight.ServiceOption, 0, len(opts))
	for _, o := range opts {
		if o.Estimated {
			filtered = append(filtered, o)
			continue
		}
		id, err := strconv.Atoi(o.ID)
		if err != nil {
			continue
		}
		if !enabled[id] {
			continue
		}
		if blocked[id] {
			continue
		}
		if len(preferred) > 0 && !preferred[id] {
			continue
		}
		o.Preferred = preferred[id]
		filtered = append(filtered, o)
	}

	// Frete(s) preferido(s) sobem pro topo da lista (ordem relativa preservada
	// dentro de cada grupo — preferidas primeiro, depois o resto).
	if len(preferred) == 0 {
		return filtered
	}
	out := make([]freight.ServiceOption, 0, len(filtered))
	rest := make([]freight.ServiceOption, 0, len(filtered))
	for _, o := range filtered {
		id, err := strconv.Atoi(o.ID)
		if err == nil && preferred[id] {
			out = append(out, o)
		} else {
			rest = append(rest, o)
		}
	}
	return append(out, rest...)
}

// loadEnabledServiceIDs devolve o conjunto de service IDs habilitados
// GLOBALMENTE pelo admin (senderzz_enabled_carriers_map — company.enabled=true
// E service.enabled=true). Espelha o parse feito em go/portal freight.go.
func (h *CheckoutHandler) loadEnabledServiceIDs(ctx context.Context) map[int]bool {
	out := map[int]bool{}
	raw := h.optionRawGeneric(ctx, freightOptEnabledCarriers)
	if raw == "" {
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
	for _, c := range parsed {
		if !c.Enabled {
			continue
		}
		for sid, s := range c.Services {
			if !s.Enabled {
				continue
			}
			if id, err := strconv.Atoi(sid); err == nil {
				out[id] = true
			}
		}
	}
	return out
}

// producerClassID lê senderzz_portal_users.shipping_class_id do produtor.
// 0 se ausente/erro — chaves de classe "0" simplesmente não existirão nos
// mapas de preferida/bloqueada, o que equivale a "sem restrição".
func (h *CheckoutHandler) producerClassID(ctx context.Context, producerID int64) int {
	if producerID <= 0 {
		return 0
	}
	var classID *int
	if err := h.db.QueryRow(ctx,
		`SELECT shipping_class_id FROM senderzz_portal_users WHERE id = $1`, producerID).
		Scan(&classID); err != nil || classID == nil {
		return 0
	}
	return *classID
}

// loadCarrierIDSet lê senderzz_options[key][class_id][field] (array de ints) e
// devolve como set. Espelha loadMap/preferredIDs/blockedIDs de go/portal freight.go
// (implementação própria — orders-service é módulo Go distinto, sem import cross-service).
func (h *CheckoutHandler) loadCarrierIDSet(ctx context.Context, optKey string, classID int, field string) map[int]bool {
	out := map[int]bool{}
	if classID <= 0 {
		return out
	}
	raw := h.optionRawGeneric(ctx, optKey)
	if raw == "" {
		return out
	}
	var m map[string]map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return out
	}
	entry, ok := m[strconv.Itoa(classID)]
	if !ok {
		return out
	}
	raw2, ok := entry[field]
	if !ok {
		return out
	}
	var ids []int
	if err := json.Unmarshal(raw2, &ids); err != nil {
		return out
	}
	for _, id := range ids {
		out[id] = true
	}
	return out
}

// optionRawGeneric lê senderzz_options[key] cru (graceful "" se ausente/tabela
// não migrada).
func (h *CheckoutHandler) optionRawGeneric(ctx context.Context, key string) string {
	var raw string
	if err := h.db.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE name=$1`, key).Scan(&raw); err != nil {
		return ""
	}
	return raw
}
