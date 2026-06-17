// Package handlers — endpoint admin para configuração de notificações push (PWA).
// Espelha sz_app_pwa_render_notifications_admin() em includes/senderzz-app-pwa.php:397
// (PHP legado). Persiste em senderzz_options(key,value) e lê administradores em
// senderzz_portal_users. Ver AUDIT-ADMIN-WP.md §14.
//
// Eventos suportados (autoritativo, sobrepõe a lista do PHP que não cobre todos):
//   agendamento_cod, em_rota_cod, completo_cod, frustrado_cod,
//   pedido_feito, enviado_pad, entregue,
//   label_gerada, cobranca_pendente, saldo_baixo, manutencao
//
// Os eventos "admin_motoboy" e "admin_expedicao" do PHP foram descartados — eles
// já eram filtrados no render() do PHP (strpos('admin_')===0; continue) e a task
// não os lista.
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

type NotificacoesPwaHandler struct{ Pool *pgxpool.Pool }

// ----- constantes / catálogo --------------------------------------------------

// eventList — ordem é load-bearing (afeta UI e desempate de status duplicado).
var eventList = []EventDef{
	{Key: "agendamento_cod", Label: "Agendamento COD", DefaultStatus: "wc-agendado"},
	{Key: "em_rota_cod", Label: "Em Rota COD", DefaultStatus: "wc-emrota"},
	{Key: "completo_cod", Label: "Completo COD", DefaultStatus: "wc-completo"},
	{Key: "frustrado_cod", Label: "Frustrado COD", DefaultStatus: "wc-frustrado"},
	{Key: "pedido_feito", Label: "Pedido feito", DefaultStatus: "wc-processing"},
	{Key: "enviado_pad", Label: "Pedido enviado", DefaultStatus: "wc-enviado"},
	{Key: "entregue", Label: "Pedido entregue", DefaultStatus: "wc-entregue"},
	{Key: "label_gerada", Label: "Etiqueta gerada", DefaultStatus: ""},
	{Key: "cobranca_pendente", Label: "Cobrança pendente", DefaultStatus: ""},
	{Key: "saldo_baixo", Label: "Saldo baixo", DefaultStatus: ""},
	{Key: "manutencao", Label: "Manutenção", DefaultStatus: ""},
}

// wcStatuses — lista hardcoded espelhando wc_get_order_statuses() do WP.
// Cada slug tem que estar no formato "wc-<x>" para casar com sz_app_pwa_normalize_wc_status_slug().
var wcStatuses = []WcStatus{
	{Slug: "wc-pending", Label: "Pagamento pendente"},
	{Slug: "wc-processing", Label: "Processando"},
	{Slug: "wc-on-hold", Label: "Em espera"},
	{Slug: "wc-completed", Label: "Concluído (Woo)"},
	{Slug: "wc-cancelled", Label: "Cancelado"},
	{Slug: "wc-refunded", Label: "Reembolsado"},
	{Slug: "wc-failed", Label: "Falhou"},
	{Slug: "wc-agendado", Label: "Agendado"},
	{Slug: "wc-emrota", Label: "Em rota"},
	{Slug: "wc-completo", Label: "Completo"},
	{Slug: "wc-frustrado", Label: "Frustrado"},
	{Slug: "wc-enviado", Label: "Enviado"},
	{Slug: "wc-entregue", Label: "Entregue"},
}

// variableCatalog — banco de variáveis com escopo por role (parity com
// sz_app_pwa_notification_allowed_vars_for_role). Producer e affiliate só veem
// número do pedido + sua própria comissão. Admin tem catálogo completo.
type variableDef struct {
	Key          string   `json:"key"`
	Label        string   `json:"label"`
	AvailableFor []string `json:"available_for"`
}

var variableCatalog = []variableDef{
	// Identificação do pedido (todos os roles).
	{Key: "numero_pedido", Label: "Número do pedido", AvailableFor: []string{"producer", "affiliate", "admin"}},
	{Key: "pedido_id", Label: "ID do pedido", AvailableFor: []string{"producer", "affiliate", "admin"}},
	// Comissões — produtor + admin.
	{Key: "comissao_produtor", Label: "Comissão produtor", AvailableFor: []string{"producer", "admin"}},
	{Key: "comissao_final_produtor", Label: "Comissão final produtor", AvailableFor: []string{"producer", "admin"}},
	// Comissões — afiliado + admin.
	{Key: "comissao_afiliado", Label: "Comissão afiliado", AvailableFor: []string{"affiliate", "admin"}},
	{Key: "comissao_final_afiliado", Label: "Comissão final afiliado", AvailableFor: []string{"affiliate", "admin"}},
	// Comissão genérica — todos.
	{Key: "comissao", Label: "Comissão (genérica)", AvailableFor: []string{"producer", "affiliate", "admin"}},
	// A partir daqui — admin only (vazamento de dados em produtor/afiliado).
	{Key: "cliente", Label: "Cliente", AvailableFor: []string{"admin"}},
	{Key: "valor_total_pedido", Label: "Valor total do pedido", AvailableFor: []string{"admin"}},
	{Key: "total", Label: "Total (alias)", AvailableFor: []string{"admin"}},
	{Key: "valor_envio", Label: "Valor do envio", AvailableFor: []string{"admin"}},
	{Key: "taxa_entrega_percentual_admin", Label: "Taxa entrega + percentual (admin)", AvailableFor: []string{"admin"}},
	{Key: "percentual_envio", Label: "Percentual de envio", AvailableFor: []string{"admin"}},
	{Key: "cidade", Label: "Cidade do cliente", AvailableFor: []string{"admin"}},
	{Key: "produto", Label: "Produto principal", AvailableFor: []string{"admin"}},
	{Key: "produtos", Label: "Lista de produtos", AvailableFor: []string{"admin"}},
	{Key: "comissao_admin_liquida", Label: "Comissão admin líquida (individual)", AvailableFor: []string{"admin"}},
	{Key: "comissao_admin_liquida_total", Label: "Comissão admin líquida total", AvailableFor: []string{"admin"}},
	{Key: "comissao_admin", Label: "Comissão admin", AvailableFor: []string{"admin"}},
	{Key: "comissao_admin_total", Label: "Comissão admin total", AvailableFor: []string{"admin"}},
	{Key: "fundo_operacional", Label: "Fundo operacional", AvailableFor: []string{"admin"}},
	{Key: "taxa_motoboy_admin", Label: "Taxa motoboy (admin)", AvailableFor: []string{"admin"}},
	{Key: "status", Label: "Status do pedido", AvailableFor: []string{"admin"}},
	{Key: "transportadora", Label: "Transportadora", AvailableFor: []string{"admin"}},
	{Key: "tipo_entrega", Label: "Tipo de entrega", AvailableFor: []string{"admin"}},
}

// ----- defaults ---------------------------------------------------------------

// defaultTemplate — usado quando o option `sz_app_pwa_notification_templates`
// está vazio ou não cobre um evento. Em PT-BR para parity com o PHP legado.
func defaultTemplate(key string) Template {
	switch key {
	case "agendamento_cod":
		return Template{
			Title: "📅 Agendamento",
			ProducerTitle: "📅 Agendamento", ProducerBody: "Pedido {{numero_pedido}} · Sua comissão é {{comissao_produtor}}.",
			AffiliateTitle: "📅 Agendamento", AffiliateBody: "Pedido {{numero_pedido}} · Sua comissão é {{comissao_afiliado}}.",
			AdminTitle: "📅 Agendamento · {{cliente}}", AdminBody: "Pedido {{numero_pedido}} · {{produto}} · {{cidade}}",
		}
	case "em_rota_cod":
		return Template{
			Title: "🏍️ Pedido Em Rota",
			ProducerTitle: "🏍️ Pedido Em Rota", ProducerBody: "Pedido {{numero_pedido}} · Sua comissão é {{comissao_produtor}}.",
			AffiliateTitle: "🏍️ Pedido Em Rota", AffiliateBody: "Pedido {{numero_pedido}} · Sua comissão é {{comissao_afiliado}}.",
			AdminTitle: "🏍️ Em rota · {{cliente}}", AdminBody: "Pedido {{numero_pedido}} · {{produto}} · {{tipo_entrega}}",
		}
	case "completo_cod":
		return Template{
			Title: "✅ Pedido Entregue",
			ProducerTitle: "✅ Pedido Entregue", ProducerBody: "Pedido {{numero_pedido}} entregue · Sua comissão é {{comissao_produtor}}.",
			AffiliateTitle: "✅ Pedido Entregue", AffiliateBody: "Pedido {{numero_pedido}} entregue · Sua comissão é {{comissao_afiliado}}.",
			AdminTitle: "✅ Completo · {{cliente}}", AdminBody: "Pedido {{numero_pedido}} completo · {{produto}}.",
		}
	case "frustrado_cod":
		return Template{
			Title: "❌ Pedido Frustrado",
			ProducerTitle: "❌ Pedido Frustrado", ProducerBody: "Pedido {{numero_pedido}} atualizado.",
			AffiliateTitle: "❌ Pedido Frustrado", AffiliateBody: "Pedido {{numero_pedido}} atualizado.",
			AdminTitle: "❌ Frustrado · {{cliente}}", AdminBody: "Pedido {{numero_pedido}} · {{produto}} · {{cidade}} · Status {{status}}.",
		}
	case "pedido_feito":
		return Template{
			Title: "🛒 Pedido Novo",
			ProducerTitle: "🛒 Pedido Novo", ProducerBody: "Pedido {{numero_pedido}} · Sua comissão é {{comissao_produtor}}.",
			AffiliateTitle: "🛒 Pedido Novo", AffiliateBody: "Pedido {{numero_pedido}} · Sua comissão é {{comissao_afiliado}}.",
			AdminTitle: "🛒 Pedido Novo · {{cliente}}", AdminBody: "Pedido {{numero_pedido}} · {{produto}} · Total {{valor_total_pedido}} · {{tipo_entrega}}",
		}
	case "enviado_pad":
		return Template{
			Title: "📦 Pedido Enviado",
			ProducerTitle: "📦 Pedido Enviado", ProducerBody: "Pedido {{numero_pedido}}.",
			AffiliateTitle: "📦 Pedido Enviado", AffiliateBody: "Pedido {{numero_pedido}}.",
			AdminTitle: "📦 Enviado · {{transportadora}}", AdminBody: "Pedido {{numero_pedido}} · {{produto}} · Envio {{valor_envio}} · Total {{valor_total_pedido}}",
		}
	case "entregue":
		return Template{
			Title: "✅ Pedido Entregue",
			ProducerTitle: "✅ Pedido Entregue", ProducerBody: "Pedido {{numero_pedido}} entregue.",
			AffiliateTitle: "✅ Pedido Entregue", AffiliateBody: "Pedido {{numero_pedido}} entregue.",
			AdminTitle: "✅ Entregue · {{cliente}}", AdminBody: "Pedido {{numero_pedido}} entregue · {{produto}} · Total {{valor_total_pedido}}.",
		}
	case "label_gerada":
		return Template{
			Title: "🏷️ Etiqueta gerada",
			ProducerTitle: "🏷️ Etiqueta gerada", ProducerBody: "Pedido {{numero_pedido}}: etiqueta pronta para impressão.",
			AffiliateTitle: "🏷️ Etiqueta gerada", AffiliateBody: "Pedido {{numero_pedido}}: etiqueta pronta.",
			AdminTitle: "🏷️ Etiqueta · {{transportadora}}", AdminBody: "Pedido {{numero_pedido}} · {{produto}} · etiqueta gerada.",
		}
	case "cobranca_pendente":
		return Template{
			Title: "💸 Cobrança pendente",
			ProducerTitle: "💸 Cobrança pendente", ProducerBody: "Existe cobrança pendente vinculada ao pedido {{numero_pedido}}.",
			AffiliateTitle: "💸 Cobrança pendente", AffiliateBody: "Pedido {{numero_pedido}} aguardando confirmação de pagamento.",
			AdminTitle: "💸 Cobrança pendente · {{cliente}}", AdminBody: "Pedido {{numero_pedido}} · Total {{valor_total_pedido}} aguardando confirmação.",
		}
	case "saldo_baixo":
		return Template{
			Title: "⚠️ Saldo baixo",
			ProducerTitle: "⚠️ Saldo baixo", ProducerBody: "Sua carteira está com saldo baixo. Faça uma recarga para evitar bloqueios.",
			AffiliateTitle: "⚠️ Saldo baixo", AffiliateBody: "Sua carteira está com saldo baixo.",
			AdminTitle: "⚠️ Saldo baixo · {{cliente}}", AdminBody: "Carteira com saldo baixo. Verifique recargas pendentes.",
		}
	case "manutencao":
		return Template{
			Title: "🛠️ Manutenção",
			ProducerTitle: "🛠️ Manutenção", ProducerBody: "Manutenção programada: alguns recursos podem ficar indisponíveis.",
			AffiliateTitle: "🛠️ Manutenção", AffiliateBody: "Manutenção programada na plataforma.",
			AdminTitle: "🛠️ Manutenção", AdminBody: "Janela de manutenção programada. Verifique o painel de status.",
		}
	}
	// Fallback genérico — não deve acontecer (event keys vêm do catálogo).
	return Template{
		Title:          "Notificação",
		ProducerTitle:  "Notificação",
		ProducerBody:   "Pedido {{numero_pedido}}.",
		AffiliateTitle: "Notificação",
		AffiliateBody:  "Pedido {{numero_pedido}}.",
		AdminTitle:     "Notificação · {{cliente}}",
		AdminBody:      "Pedido {{numero_pedido}}.",
	}
}

// defaultRecipients — produtor + afiliado ativos por padrão, admin off (parity
// com sz_app_pwa_default_notification_recipients).
func defaultRecipients() Recipients {
	return Recipients{Producer: 1, Affiliate: 1, Admin: 0}
}

// ----- payloads (DTOs) --------------------------------------------------------

type EventDef struct {
	Key           string `json:"key"`
	Label         string `json:"label"`
	DefaultStatus string `json:"default_status"`
}

type WcStatus struct {
	Slug  string `json:"slug"`
	Label string `json:"label"`
}

type Template struct {
	// Title e Body são campos genéricos de fallback usados por sz_app_pwa_apply_template()
	// (linha 713-714 do PHP) quando um campo role-específico está vazio.
	// Go deve preservá-los no round-trip para não quebrar o fallback do dispatcher PHP.
	Title string `json:"title"`
	Body  string `json:"body"`

	ProducerTitle  string `json:"producer_title"`
	ProducerBody   string `json:"producer_body"`
	AffiliateTitle string `json:"affiliate_title"`
	AffiliateBody  string `json:"affiliate_body"`
	AdminTitle     string `json:"admin_title"`
	AdminBody      string `json:"admin_body"`
}

type Recipients struct {
	Producer int `json:"producer"`
	Affiliate int `json:"affiliate"`
	Admin    int `json:"admin"`
}

type AdminUser struct {
	ID    int64  `json:"id"`
	Nome  string `json:"nome"`
	Email string `json:"email"`
}

// ----- helpers ----------------------------------------------------------------

// tableExists verifica presença de uma tabela no schema public.
func (h *NotificacoesPwaHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// getOptionJSON lê option e decodifica como JSON em out.
func (h *NotificacoesPwaHandler) getOptionJSON(ctx context.Context, key string, out any) {
	if !h.tableExists(ctx, "senderzz_options") {
		return
	}
	var raw string
	if err := h.Pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE "key"=$1`, key).Scan(&raw); err != nil {
		return
	}
	if strings.TrimSpace(raw) == "" {
		return
	}
	_ = json.Unmarshal([]byte(raw), out)
}

// upsertOption grava option (UPSERT em senderzz_options).
func (h *NotificacoesPwaHandler) upsertOption(ctx context.Context, key, value string) error {
	if !h.tableExists(ctx, "senderzz_options") {
		return nil
	}
	_, err := h.Pool.Exec(ctx,
		`INSERT INTO senderzz_options ("key", value)
		 VALUES ($1, $2)
		 ON CONFLICT ("key") DO UPDATE SET value = EXCLUDED.value`, key, value)
	return err
}

// upsertJSON grava option como JSON.
func (h *NotificacoesPwaHandler) upsertJSON(ctx context.Context, key string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return h.upsertOption(ctx, key, string(b))
}

// normalizeWcSlug espelha sz_app_pwa_normalize_wc_status_slug() — prefixa "wc-"
// se ausente; vazio mantém vazio.
func normalizeWcSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "wc-") {
		return s
	}
	return "wc-" + s
}

// validEventKey retorna true se a chave existe no catálogo.
func validEventKey(k string) bool {
	for _, e := range eventList {
		if e.Key == k {
			return true
		}
	}
	return false
}

// varPattern casa qualquer variável {{name}} num template.
var varPattern = regexp.MustCompile(`\{\{[a-zA-Z0-9_]+\}\}`)

// allowedVarsForRole espelha sz_app_pwa_notification_allowed_vars_for_role().
// Admin retorna nil (sem restrição). Producer e affiliate retornam conjunto fechado.
func allowedVarsForRole(role string) map[string]bool {
	commonOrder := []string{"{{numero_pedido}}", "{{pedido_id}}"}
	switch role {
	case "producer":
		allowed := map[string]bool{}
		for _, v := range append(commonOrder, "{{comissao_produtor}}", "{{comissao_final_produtor}}", "{{comissao}}") {
			allowed[v] = true
		}
		return allowed
	case "affiliate":
		allowed := map[string]bool{}
		for _, v := range append(commonOrder, "{{comissao_afiliado}}", "{{comissao_final_afiliado}}", "{{comissao}}") {
			allowed[v] = true
		}
		return allowed
	}
	return nil // admin — sem restrição
}

// restrictTemplateVars espelha sz_app_pwa_restrict_template_variables().
// Strips qualquer {{var}} não permitida para o role; admin passa sem filtro.
func restrictTemplateVars(template, role string) string {
	if template == "" || role == "admin" {
		return template
	}
	allowed := allowedVarsForRole(role)
	if allowed == nil {
		return template
	}
	return varPattern.ReplaceAllStringFunc(template, func(m string) string {
		if allowed[m] {
			return m
		}
		return ""
	})
}

// ----- GET /notificacoes-pwa/events ------------------------------------------

// GetEvents retorna o catálogo de eventos + lista de status WC disponíveis.
// O front usa wc_statuses para popular o select de status binding.
func (h *NotificacoesPwaHandler) GetEvents(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, 200, map[string]any{
		"items":       eventList,
		"wc_statuses": wcStatuses,
	})
}

// ----- GET /notificacoes-pwa/templates ---------------------------------------

// GetTemplates lê option `sz_app_pwa_notification_templates`. Defaults preenchem
// eventos ausentes (graceful degradation — primeira instalação).
func (h *NotificacoesPwaHandler) GetTemplates(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	saved := map[string]Template{}
	h.getOptionJSON(ctx, "sz_app_pwa_notification_templates", &saved)

	out := map[string]Template{}
	for _, ev := range eventList {
		def := defaultTemplate(ev.Key)
		if tpl, ok := saved[ev.Key]; ok {
			// Merge: campo vazio cai para o default.
			// Title/Body genéricos (fallback PHP) são preservados do salvo; default apenas se ausentes.
			if tpl.Title == "" {
				tpl.Title = def.Title
			}
			if tpl.Body == "" {
				tpl.Body = def.Body
			}
			if tpl.ProducerTitle == "" {
				tpl.ProducerTitle = def.ProducerTitle
			}
			if tpl.ProducerBody == "" {
				tpl.ProducerBody = def.ProducerBody
			}
			if tpl.AffiliateTitle == "" {
				tpl.AffiliateTitle = def.AffiliateTitle
			}
			if tpl.AffiliateBody == "" {
				tpl.AffiliateBody = def.AffiliateBody
			}
			if tpl.AdminTitle == "" {
				tpl.AdminTitle = def.AdminTitle
			}
			if tpl.AdminBody == "" {
				tpl.AdminBody = def.AdminBody
			}
			out[ev.Key] = tpl
		} else {
			out[ev.Key] = def
		}
	}
	httpx.JSON(w, 200, map[string]any{"templates": out})
}

// ----- POST /notificacoes-pwa/templates --------------------------------------

// SaveTemplates aceita body `{ templates: { event_key: Template } }` e
// substitui o option. Eventos desconhecidos são descartados (defesa contra
// chaves arbitrárias).
func (h *NotificacoesPwaHandler) SaveTemplates(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Templates map[string]Template `json:"templates"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	clean := map[string]Template{}
	for k, t := range body.Templates {
		if !validEventKey(k) {
			continue
		}
		// Espelha sz_app_pwa_restrict_template_variables() (linha 434-438 do PHP):
		// producer_title e producer_body só aceitam variáveis do conjunto do produtor;
		// affiliate_title e affiliate_body idem para afiliado. Admin passa sem filtro.
		// Isso previne vazamento de dados sensíveis (ex: {{cliente}}) em templates de
		// produtor/afiliado quando o administrador digita manualmente uma variável proibida.
		t.ProducerTitle = restrictTemplateVars(t.ProducerTitle, "producer")
		t.ProducerBody = restrictTemplateVars(t.ProducerBody, "producer")
		t.AffiliateTitle = restrictTemplateVars(t.AffiliateTitle, "affiliate")
		t.AffiliateBody = restrictTemplateVars(t.AffiliateBody, "affiliate")
		clean[k] = t
	}
	if err := h.upsertJSON(r.Context(), "sz_app_pwa_notification_templates", clean); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	httpx.JSON(w, 200, map[string]any{"ok": true})
}

// ----- GET /notificacoes-pwa/status-map --------------------------------------

// GetStatusMap retorna `{ event: "wc-slug" }`. Internamente o PHP grava
// `{ event: ["wc-slug"] }` (array de um) — convertemos no boundary do JSON.
func (h *NotificacoesPwaHandler) GetStatusMap(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// Aceita tanto `{event:[slug]}` (formato PHP legado) quanto `{event:slug}`.
	raw := map[string]json.RawMessage{}
	h.getOptionJSON(ctx, "sz_app_pwa_notification_status_map", &raw)

	out := map[string]string{}

	if len(raw) == 0 {
		// Sem option salvo → retorna defaults.
		for _, ev := range eventList {
			out[ev.Key] = ev.DefaultStatus
		}
		httpx.JSON(w, 200, out)
		return
	}

	for _, ev := range eventList {
		val, ok := raw[ev.Key]
		if !ok {
			out[ev.Key] = ""
			continue
		}
		// Tenta array primeiro.
		var arr []string
		if err := json.Unmarshal(val, &arr); err == nil {
			if len(arr) > 0 {
				out[ev.Key] = normalizeWcSlug(arr[0])
			} else {
				out[ev.Key] = ""
			}
			continue
		}
		// Tenta string.
		var s string
		if err := json.Unmarshal(val, &s); err == nil {
			out[ev.Key] = normalizeWcSlug(s)
			continue
		}
		out[ev.Key] = ""
	}
	httpx.JSON(w, 200, out)
}

// ----- POST /notificacoes-pwa/status-map -------------------------------------

// SaveStatusMap valida exclusão mútua: cada status pode mapear para um único
// evento. Conflito → 400 com `{ error: { code: "conflict", message, details } }`.
// Persiste no formato PHP-compatível (`{event:[slug]}`) para que o push handler
// PHP legado continue funcionando se ainda estiver ativo.
func (h *NotificacoesPwaHandler) SaveStatusMap(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	// Normaliza e valida exclusão mútua.
	claimed := map[string]string{} // slug → event que reivindicou
	clean := map[string][]string{}
	for _, ev := range eventList {
		slug := normalizeWcSlug(body[ev.Key])
		if slug == "" {
			clean[ev.Key] = []string{}
			continue
		}
		if other, used := claimed[slug]; used {
			httpx.JSON(w, 400, map[string]any{
				"error": map[string]any{
					"code":    "conflict",
					"message": "status " + slug + " já vinculado ao evento " + other,
					"details": map[string]string{
						"slug":        slug,
						"event_a":     other,
						"event_b":     ev.Key,
					},
				},
			})
			return
		}
		claimed[slug] = ev.Key
		clean[ev.Key] = []string{slug}
	}
	if err := h.upsertJSON(r.Context(), "sz_app_pwa_notification_status_map", clean); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	httpx.JSON(w, 200, map[string]any{"ok": true})
}

// ----- GET /notificacoes-pwa/recipients --------------------------------------

func (h *NotificacoesPwaHandler) GetRecipients(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	saved := map[string]Recipients{}
	h.getOptionJSON(ctx, "sz_app_pwa_notification_recipients", &saved)

	out := map[string]Recipients{}
	for _, ev := range eventList {
		if v, ok := saved[ev.Key]; ok {
			out[ev.Key] = v
		} else {
			out[ev.Key] = defaultRecipients()
		}
	}
	httpx.JSON(w, 200, map[string]any{"recipients": out})
}

// ----- POST /notificacoes-pwa/recipients -------------------------------------

func (h *NotificacoesPwaHandler) SaveRecipients(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Recipients map[string]Recipients `json:"recipients"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	clean := map[string]Recipients{}
	for _, ev := range eventList {
		v, ok := body.Recipients[ev.Key]
		if !ok {
			clean[ev.Key] = defaultRecipients()
			continue
		}
		// Normaliza valores não-booleanos (clamp 0/1).
		clean[ev.Key] = Recipients{
			Producer:  clampFlag(v.Producer),
			Affiliate: clampFlag(v.Affiliate),
			Admin:     clampFlag(v.Admin),
		}
	}
	if err := h.upsertJSON(r.Context(), "sz_app_pwa_notification_recipients", clean); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	httpx.JSON(w, 200, map[string]any{"ok": true})
}

// clampFlag normaliza um inteiro para 0 ou 1 (qualquer valor > 0 vira 1).
func clampFlag(v int) int {
	if v > 0 {
		return 1
	}
	return 0
}

// ----- GET /notificacoes-pwa/admin-recipients --------------------------------

// GetAdminRecipients devolve { event: [user_ids], available_admins: [...] }.
// available_admins puxa users role IN ('admin','super_admin') de
// senderzz_portal_users (admin do portal V2).
func (h *NotificacoesPwaHandler) GetAdminRecipients(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	saved := map[string][]int64{}
	h.getOptionJSON(ctx, "sz_app_pwa_admin_recipients", &saved)

	out := map[string][]int64{}
	for _, ev := range eventList {
		if v, ok := saved[ev.Key]; ok && len(v) > 0 {
			out[ev.Key] = v
		} else {
			out[ev.Key] = []int64{}
		}
	}

	available := []AdminUser{}
	if h.tableExists(ctx, "senderzz_portal_users") {
		rows, err := h.Pool.Query(ctx,
			`SELECT id, COALESCE(nome,''), COALESCE(email,'')
			 FROM senderzz_portal_users
			 WHERE role IN ('admin','super_admin') AND ativo = true
			 ORDER BY nome ASC, id ASC`)
		if err == nil {
			for rows.Next() {
				var a AdminUser
				if err := rows.Scan(&a.ID, &a.Nome, &a.Email); err == nil {
					available = append(available, a)
				}
			}
			rows.Close()
		}
	}
	httpx.JSON(w, 200, map[string]any{
		"admin_recipients": out,
		"available_admins": available,
	})
}

// ----- POST /notificacoes-pwa/admin-recipients -------------------------------

func (h *NotificacoesPwaHandler) SaveAdminRecipients(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AdminRecipients map[string][]int64 `json:"admin_recipients"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	clean := map[string][]int64{}
	for _, ev := range eventList {
		ids := body.AdminRecipients[ev.Key]
		if ids == nil {
			clean[ev.Key] = []int64{}
			continue
		}
		// Defesa: filtra IDs <= 0 e dedup.
		seen := map[int64]bool{}
		filtered := []int64{}
		for _, id := range ids {
			if id <= 0 || seen[id] {
				continue
			}
			seen[id] = true
			filtered = append(filtered, id)
		}
		clean[ev.Key] = filtered
	}
	if err := h.upsertJSON(r.Context(), "sz_app_pwa_admin_recipients", clean); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	httpx.JSON(w, 200, map[string]any{"ok": true})
}

// ----- GET /notificacoes-pwa/order-number-flags ------------------------------

// GetOrderNumberFlags devolve `{event: 0|1}`. Default = 1 para todos os eventos.
func (h *NotificacoesPwaHandler) GetOrderNumberFlags(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	saved := map[string]int{}
	h.getOptionJSON(ctx, "sz_app_pwa_notification_order_number_flags", &saved)

	out := map[string]int{}
	for _, ev := range eventList {
		if v, ok := saved[ev.Key]; ok {
			out[ev.Key] = clampFlag(v)
		} else {
			out[ev.Key] = 1 // default — incluir número do pedido.
		}
	}
	httpx.JSON(w, 200, out)
}

// ----- POST /notificacoes-pwa/order-number-flags -----------------------------

func (h *NotificacoesPwaHandler) SaveOrderNumberFlags(w http.ResponseWriter, r *http.Request) {
	var body map[string]int
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	clean := map[string]int{}
	for _, ev := range eventList {
		clean[ev.Key] = clampFlag(body[ev.Key])
	}
	if err := h.upsertJSON(r.Context(), "sz_app_pwa_notification_order_number_flags", clean); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	httpx.JSON(w, 200, map[string]any{"ok": true})
}

// ----- GET /notificacoes-pwa/variables ---------------------------------------

// GetVariables expõe o banco de variáveis com escopo por role. O front usa
// available_for para filtrar o dropdown de inserção em cada tab.
func (h *NotificacoesPwaHandler) GetVariables(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, 200, map[string]any{"items": variableCatalog})
}
