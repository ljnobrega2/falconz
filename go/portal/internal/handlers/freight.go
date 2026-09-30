// Package handlers — handlers de Frete / Logística do Portal V2.
//
// Espelha templates/portal/v2/sections/freight.php (section "Frete") e os
// szactions set_preferred_carrier / set_blocked_carrier de Portal_Page.php.
//
// Rotas (namespace /wp-json/senderzz/v1):
//
//	GET  /portal/freight            — sender + catálogo de transportadoras + IDs preferidas/bloqueadas
//	POST /portal/freight/preferred  — salva modalidades favoritas (szaction=set_preferred_carrier)
//	POST /portal/freight/blocked    — salva transportadoras bloqueadas (szaction=set_blocked_carrier)
//
// Escopo do usuário:
//
//	A configuração de frete é escopada pela CLASSE PRIMÁRIA do produtor
//	($sz9fr_primary no PHP = senderzz_portal_users.shipping_class_id).
//	O class_id NUNCA vem do request — é lido fresh do banco a cada chamada
//	(espelha o recompute por render do PHP). Fallback para o claim do JWT
//	(auth.PortalUser.ClassID) só se a leitura da coluna falhar.
//
// Armazenamento (espelha Portal_Page.php::set_preferred_carrier_ids /
// set_blocked_carrier_ids — WP options mirroradas em senderzz_options como JSON):
//
//	senderzz_options['tp_preferida_map']            →
//	    {"<class_id>": {"modo":"mais_barata","permitidas":[ids]}, ...}
//	senderzz_options['senderzz_blocked_carriers_map'] →
//	    {"<class_id>": {"bloqueadas":[ids]}, ...}
//
//	Chaves do mapa são strings de class_id (paridade com o cast int→string do
//	JSON do WP, idêntico ao senderzz_markup_rules do admin expedicao_integracoes.go).
//	method_ids vazio → REMOVE a entrada da classe (reset). Não-vazio → grava.
//
//	pref_cheapest é DERIVADO (preferred_ids vazio = "mais barata entre todas"),
//	nunca um campo persistido — espelha o checkbox szv2-fr-pref-cheapest do PHP,
//	que vem checked( empty( $sz9fr_preferred_ids ) ).
//
//	O catálogo de transportadoras vem da seleção GLOBAL do admin
//	(senderzz_enabled_carriers_map, gravada em go/admin/internal/handlers/carriers.go
//	a partir do catálogo ao vivo da Melhor Envio). Sem seleção salva ainda →
//	lista vazia (graceful-empty), que dispara o empty-state fiel do PHP
//	("Nenhuma transportadora disponível").
package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/portal-service/internal/auth"
	"github.com/senderzz/portal-service/internal/httpx"
)

// FreightHandler agrupa as dependências dos handlers de frete.
// Construção idêntica a WebhookHandler — o integrador conecta da mesma forma.
type FreightHandler struct {
	Pool *pgxpool.Pool
}

// Chaves de option (espelham TP_PREFERIDA_OPTION / SENDERZZ_BLOCKED_OPTION do PHP).
const (
	freightOptPreferida = "tp_preferida_map"
	freightOptBlocked   = "senderzz_blocked_carriers_map"
	freightOptMEAddress = "woocommerce_wc-melhor-envio_settings"
)

// Mensagens PT-BR — porte fiel de Portal_Page.php (set_preferred_carrier_ids /
// set_blocked_carrier_ids). NÃO alterar os textos.
const (
	freightMsgPrefReset    = "Modalidades resetadas. O Senderzz voltará a recomendar automaticamente a opção mais barata disponível."
	freightMsgPrefSet      = "Modalidades de frete atualizadas."
	freightMsgBlockedReset = "Bloqueios removidos."
	freightMsgBlockedSet   = "Transportadoras bloqueadas atualizadas."
)

// ── Tipos de resposta / request ────────────────────────────────────────────────

// freightMethod é uma modalidade dentro de uma transportadora.
type freightMethod struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// freightCarrier agrupa modalidades por empresa (espelha o grouping do PHP).
type freightCarrier struct {
	Company string          `json:"company"`
	Methods []freightMethod `json:"methods"`
}

// freightSender são os dados do remetente exibidos no painel (oculto) fr-remetente.
type freightSender struct {
	Name      string `json:"name"`
	Document  string `json:"document"`
	Telephone string `json:"telephone"`
}

// freightGetResponse é o payload de GET /portal/freight.
type freightGetResponse struct {
	ClassID      int              `json:"class_id"`
	Sender       freightSender    `json:"sender"`
	Carriers     []freightCarrier `json:"carriers"`
	PreferredIDs []int            `json:"preferred_ids"`
	BlockedIDs   []int            `json:"blocked_ids"`
	PrefCheapest bool             `json:"pref_cheapest"`
}

// freightSaveRequest é o body de POST /portal/freight/{preferred,blocked}.
type freightSaveRequest struct {
	MethodIDs []int `json:"method_ids"`
}

// freightSaveResponse é o payload de retorno dos saves.
type freightSaveResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// ── GET /portal/freight ─────────────────────────────────────────────────────────

// Get retorna remetente + catálogo de transportadoras + preferidas/bloqueadas
// da classe primária do usuário autenticado.
func (h *FreightHandler) Get(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	classID := h.primaryClassID(r.Context(), u)

	preferred := h.preferredIDs(r.Context(), classID)
	blocked := h.blockedIDs(r.Context(), classID)

	resp := freightGetResponse{
		ClassID:      classID,
		Sender:       h.sender(r.Context()),
		Carriers:     h.carriers(r.Context()),
		PreferredIDs: preferred,
		BlockedIDs:   blocked,
		// pref_cheapest derivado: vazio = "mais barata entre todas" (checked).
		PrefCheapest: len(preferred) == 0,
	}

	httpx.WriteOK(w, map[string]any{
		"class_id":      resp.ClassID,
		"sender":        resp.Sender,
		"carriers":      resp.Carriers,
		"preferred_ids": resp.PreferredIDs,
		"blocked_ids":   resp.BlockedIDs,
		"pref_cheapest": resp.PrefCheapest,
	})
}

// ── POST /portal/freight/preferred ──────────────────────────────────────────────

// SavePreferred grava as modalidades favoritas da classe primária do usuário.
// Espelha szaction=set_preferred_carrier → set_preferred_carrier_ids.
func (h *FreightHandler) SavePreferred(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	// SEC-RBAC: subconta não altera a config de frete (gate do PHP — Portal_Page.php:3537).
	if h.isSubAccount(r.Context(), u.ID) {
		httpx.WriteErr(w, http.StatusForbidden, "Sem permissão.")
		return
	}

	var req freightSaveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}

	classID := h.primaryClassID(r.Context(), u)
	if classID <= 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "classe de envio primária não definida")
		return
	}

	ids := sanitizeMethodIDs(req.MethodIDs)

	// Carrega o mapa atual, edita só a chave da classe, regrava.
	m := h.loadMap(r.Context(), freightOptPreferida)
	key := classKey(classID)
	if len(ids) > 0 {
		// Mantém o modo "mais_barata" mas limita às modalidades escolhidas.
		m[key] = map[string]any{"modo": "mais_barata", "permitidas": ids}
	} else {
		// Sem seleção = usar todas e deixar o checkout escolher a mais barata.
		delete(m, key)
	}

	if err := h.saveMap(r.Context(), freightOptPreferida, m); err != nil {
		slog.Error("[portal_freight] erro ao salvar preferidas", "user_id", u.ID, "class_id", classID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	msg := freightMsgPrefSet
	if len(ids) == 0 {
		msg = freightMsgPrefReset
	}

	slog.Info("[portal_freight] preferidas atualizadas", "user_id", u.ID, "class_id", classID, "qtd", len(ids))
	httpx.WriteOK(w, map[string]any{"success": true, "message": msg})
}

// ── POST /portal/freight/blocked ────────────────────────────────────────────────

// SaveBlocked grava as transportadoras bloqueadas da classe primária do usuário.
// Espelha szaction=set_blocked_carrier → set_blocked_carrier_ids.
func (h *FreightHandler) SaveBlocked(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	// SEC-RBAC: subconta não altera a config de frete (gate do PHP — Portal_Page.php:3544).
	if h.isSubAccount(r.Context(), u.ID) {
		httpx.WriteErr(w, http.StatusForbidden, "Sem permissão.")
		return
	}

	var req freightSaveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}

	classID := h.primaryClassID(r.Context(), u)
	if classID <= 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "classe de envio primária não definida")
		return
	}

	ids := sanitizeMethodIDs(req.MethodIDs)

	m := h.loadMap(r.Context(), freightOptBlocked)
	key := classKey(classID)
	if len(ids) > 0 {
		m[key] = map[string]any{"bloqueadas": ids}
	} else {
		delete(m, key)
	}

	if err := h.saveMap(r.Context(), freightOptBlocked, m); err != nil {
		slog.Error("[portal_freight] erro ao salvar bloqueios", "user_id", u.ID, "class_id", classID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	msg := freightMsgBlockedSet
	if len(ids) == 0 {
		msg = freightMsgBlockedReset
	}

	slog.Info("[portal_freight] bloqueios atualizados", "user_id", u.ID, "class_id", classID, "qtd", len(ids))
	httpx.WriteOK(w, map[string]any{"success": true, "message": msg})
}

// ── Helpers de escopo / opções ──────────────────────────────────────────────────

// primaryClassID lê a classe de envio primária fresh do banco
// (espelha o recompute de $sz9fr_primary por render no PHP). Fallback para o
// claim do JWT se a coluna não puder ser lida.
func (h *FreightHandler) primaryClassID(ctx context.Context, u *auth.PortalUser) int {
	var classID *int64
	err := h.Pool.QueryRow(ctx,
		`SELECT shipping_class_id FROM senderzz_portal_users WHERE id = $1`,
		u.ID,
	).Scan(&classID)
	if err == nil && classID != nil && *classID > 0 {
		return int(*classID)
	}
	if err != nil && err != pgx.ErrNoRows {
		slog.Warn("[portal_freight] falha ao ler shipping_class_id — usando claim do JWT", "user_id", u.ID, "err", err)
	}
	if u.ClassID > 0 {
		return int(u.ClassID)
	}
	return 0
}

// preferredIDs devolve as modalidades preferidas da classe.
// Espelha senderzz_portal_preferred_carrier_ids(): map[class]['permitidas'].
func (h *FreightHandler) preferredIDs(ctx context.Context, classID int) []int {
	if classID <= 0 {
		return []int{}
	}
	m := h.loadMap(ctx, freightOptPreferida)
	entry, ok := m[classKey(classID)].(map[string]any)
	if !ok {
		return []int{}
	}
	return jsonNumbersToInts(entry["permitidas"])
}

// blockedIDs devolve as modalidades bloqueadas da classe.
// Espelha senderzz_portal_blocked_carrier_ids(): map[class]['bloqueadas'].
func (h *FreightHandler) blockedIDs(ctx context.Context, classID int) []int {
	if classID <= 0 {
		return []int{}
	}
	m := h.loadMap(ctx, freightOptBlocked)
	entry, ok := m[classKey(classID)].(map[string]any)
	if !ok {
		return []int{}
	}
	return jsonNumbersToInts(entry["bloqueadas"])
}

// freightOptEnabledCarriers é a mesma chave que o admin-service grava em
// go/admin/internal/handlers/carriers.go (carriersCatalogOptionKey) — seleção
// GLOBAL de transportadoras/serviços habilitados pelo admin. É o TETO: o
// produtor só pode preferir/bloquear dentro deste conjunto (preferredIDs/
// blockedIDs abaixo nunca ampliam o catálogo).
const freightOptEnabledCarriers = "senderzz_enabled_carriers_map"

// carriers devolve o catálogo de transportadoras agrupado por empresa, filtrado
// pela seleção global do admin (senderzz_enabled_carriers_map). Company/service
// desabilitados pelo admin não aparecem — nem para escolher como preferida, nem
// para ver como bloqueada. Sem seleção salva ainda → vazio (graceful empty-state).
func (h *FreightHandler) carriers(ctx context.Context) []freightCarrier {
	raw := h.optionRaw(ctx, freightOptEnabledCarriers)
	if raw == "" {
		return []freightCarrier{}
	}
	var parsed map[string]struct {
		Name     string `json:"name"`
		Enabled  bool   `json:"enabled"`
		Services map[string]struct {
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"`
		} `json:"services"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		slog.Warn("[senderzz_portal] carriers: falha ao decodificar senderzz_enabled_carriers_map", "err", err)
		return []freightCarrier{}
	}
	out := make([]freightCarrier, 0, len(parsed))
	for _, c := range parsed {
		if !c.Enabled {
			continue
		}
		fc := freightCarrier{Company: c.Name}
		for sid, s := range c.Services {
			if !s.Enabled {
				continue
			}
			id, err := strconv.Atoi(sid)
			if err != nil {
				continue
			}
			fc.Methods = append(fc.Methods, freightMethod{ID: id, Name: s.Name})
		}
		if len(fc.Methods) > 0 {
			out = append(out, fc)
		}
	}
	return out
}

// sender devolve os dados do remetente (painel oculto fr-remetente).
// Best-effort: lê o endereço global da conta Melhor Envio se presente em
// senderzz_options['woocommerce_wc-melhor-envio_settings']['address'].
// billing_phone (user_meta WP) não é mirrorado em Go → telephone do address.
func (h *FreightHandler) sender(ctx context.Context) freightSender {
	raw := h.optionRaw(ctx, freightOptMEAddress)
	if raw == "" {
		return freightSender{}
	}
	var settings struct {
		Address struct {
			Name            string `json:"name"`
			Document        string `json:"document"`
			CompanyDocument string `json:"company_document"`
			Phone           string `json:"phone"`
		} `json:"address"`
	}
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return freightSender{}
	}
	doc := settings.Address.Document
	if doc == "" {
		doc = settings.Address.CompanyDocument
	}
	return freightSender{
		Name:      settings.Address.Name,
		Document:  doc,
		Telephone: settings.Address.Phone,
	}
}

// ── Helpers de senderzz_options (JSON key/value) — espelha expedicao_integracoes.go ──

// tableExists verifica presença de uma tabela no schema public.
// Método local (admin-service e portal-service são módulos Go distintos —
// não dá para importar o helper do admin).
func (h *FreightHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// columnExists verifica presença de uma coluna (guard graceful — o espelho dev
// pode não ter parent_user_id ainda).
func (h *FreightHandler) columnExists(ctx context.Context, table, column string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.columns
			WHERE table_schema='public' AND table_name=$1 AND column_name=$2
		)`, table, column).Scan(&ok)
	return ok
}

// isSubAccount diz se o usuário do portal é subconta (parent_user_id != 0).
// SEC-RBAC: porta o gate do PHP set_preferred_carrier/set_blocked_carrier
// (Portal_Page.php:3537,3544): `if(!empty($u->parent_user_id)){... 'Sem permissão.'}`.
// Subconta NÃO altera a config de frete da classe (escrita compartilhada por classe).
// Schema dev sem a coluna → false (sem a coluna não há como ser subconta; igual ao
// tratamento de wallet.go/users_portal.go — não bloqueia a tela por ausência de schema).
func (h *FreightHandler) isSubAccount(ctx context.Context, portalID int64) bool {
	if !h.columnExists(ctx, "senderzz_portal_users", "parent_user_id") {
		return false
	}
	var parent *int64
	err := h.Pool.QueryRow(ctx,
		`SELECT parent_user_id FROM senderzz_portal_users WHERE id = $1`,
		portalID,
	).Scan(&parent)
	if err != nil {
		return false
	}
	return parent != nil && *parent != 0
}

// optionRaw lê o value bruto de uma option. Tabela ausente → "".
func (h *FreightHandler) optionRaw(ctx context.Context, key string) string {
	if !h.tableExists(ctx, "senderzz_options") {
		return ""
	}
	var raw string
	err := h.Pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE name=$1`, key).Scan(&raw)
	if err != nil {
		return ""
	}
	return raw
}

// loadMap carrega um option-mapa JSON keyed por class_id (string).
// Ausente/inválido → mapa vazio (nunca nil).
func (h *FreightHandler) loadMap(ctx context.Context, key string) map[string]any {
	raw := h.optionRaw(ctx, key)
	if raw == "" {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil || m == nil {
		return map[string]any{}
	}
	return m
}

// saveMap regrava o option-mapa (UPSERT em senderzz_options). Tabela ausente → no-op.
func (h *FreightHandler) saveMap(ctx context.Context, key string, m map[string]any) error {
	if !h.tableExists(ctx, "senderzz_options") {
		return nil
	}
	buf, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_, err = h.Pool.Exec(ctx,
		`INSERT INTO senderzz_options (name, value)
		 VALUES ($1, $2)
		 ON CONFLICT (name) DO UPDATE SET value = EXCLUDED.value`,
		key, string(buf))
	return err
}

// ── Helpers puros ───────────────────────────────────────────────────────────────

// classKey converte o class_id int em chave de mapa string
// (paridade com o cast int→string do JSON do WP).
func classKey(classID int) string {
	// strconv via fmt evitado — usamos json.Number-style direto.
	return intToStr(classID)
}

// sanitizeMethodIDs remove zeros/negativos e duplicatas, preservando ordem
// (espelha array_values(array_unique(array_filter(array_map('absint',…))))).
func sanitizeMethodIDs(in []int) []int {
	seen := make(map[int]bool, len(in))
	out := make([]int, 0, len(in))
	for _, v := range in {
		if v <= 0 || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// jsonNumbersToInts converte um valor JSON (slice de float64/json.Number/string)
// em []int, descartando inválidos. Sempre devolve slice não-nil.
func jsonNumbersToInts(v any) []int {
	out := []int{}
	arr, ok := v.([]any)
	if !ok {
		return out
	}
	for _, item := range arr {
		switch x := item.(type) {
		case float64:
			if int(x) > 0 {
				out = append(out, int(x))
			}
		case json.Number:
			if n, err := x.Int64(); err == nil && n > 0 {
				out = append(out, int(n))
			}
		case string:
			if n := strToInt(x); n > 0 {
				out = append(out, n)
			}
		}
	}
	return out
}

// intToStr/strToInt — conversões sem trazer strconv ao escopo de várias funções.
func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func strToInt(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
