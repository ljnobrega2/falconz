// Package handlers — endpoint admin somente leitura para visualização de capabilities e escopos.
// Espelha senderzz-access-scope.php + senderzz_admin_capability_guard() (PHP legado).
// Dados estáticos para estrutura de capabilities; DB usado para listar usuários admin ativos
// (substituto de wp_usermeta que reside no MySQL e não é espelhado neste serviço PG).
package handlers

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

// CapabilitiesHandler expõe a estrutura de capabilities e escopos do Senderzz (read-only).
type CapabilitiesHandler struct {
	Pool *pgxpool.Pool
}

// ----- tipos de payload ---------------------------------------------------

// GuardConfig descreve o mecanismo de auto-grant de capabilities.
type GuardConfig struct {
	// Trigger é a capability WordPress que dispara o auto-grant.
	Trigger string `json:"trigger"`
	// AutoGrants lista capabilities concedidas automaticamente a quem tem Trigger.
	AutoGrants []string `json:"auto_grants"`
}

// CustomCapability descreve uma capability customizada registrada pelo plugin.
type CustomCapability struct {
	Cap          string   `json:"cap"`
	Description  string   `json:"description"`
	DefaultRoles []string `json:"default_roles"`
}

// ScopeType descreve um tipo de escopo de acesso ao portal/módulos.
type ScopeType struct {
	Scope       string `json:"scope"`
	Description string `json:"description"`
	Check       string `json:"check"`
}

// CapabilitiesResponse payload completo de GET /capabilities.
type CapabilitiesResponse struct {
	Guard             GuardConfig        `json:"guard"`
	CustomCapabilities []CustomCapability `json:"custom_capabilities"`
	ScopeTypes        []ScopeType        `json:"scope_types"`
}

// ----- GET /capabilities -------------------------------------------------

// GetCapabilities retorna a estrutura estática de capabilities e escopos.
// Somente leitura — para alterar edite senderzz-access-scope.php ou senderzz_admin_capability_guard().
func (h *CapabilitiesHandler) GetCapabilities(w http.ResponseWriter, r *http.Request) {
	out := CapabilitiesResponse{
		// Guard automático: qualquer usuário com manage_options recebe todas as capabilities abaixo.
		// Espelha senderzz_admin_capability_guard() em senderzz-logistics.php.
		Guard: GuardConfig{
			Trigger: "manage_options",
			AutoGrants: []string{
				"manage_woocommerce",
				"view_woocommerce_reports",
				"edit_shop_orders",
				"read_shop_order",
				"senderzz_admin",
				"senderzz_manage",
				"senderzz_manage_motoboy",
				"senderzz_manage_finance",
			},
		},

		// Capabilities customizadas registradas em senderzz-access-scope.php.
		CustomCapabilities: []CustomCapability{
			{
				Cap:          "senderzz_admin",
				Description:  "Acesso completo ao painel Senderzz",
				DefaultRoles: []string{"administrator"},
			},
			{
				Cap:          "senderzz_manage",
				Description:  "Gerenciamento geral da operação",
				DefaultRoles: []string{"administrator", "shop_manager"},
			},
			{
				Cap:          "senderzz_manage_motoboy",
				Description:  "Gerenciar módulo Motoboy (pedidos, motoboys, zonas)",
				DefaultRoles: []string{"administrator"},
			},
			{
				Cap:          "senderzz_manage_finance",
				Description:  "Acesso a carteiras, PIX, transações financeiras",
				DefaultRoles: []string{"administrator"},
			},
		},

		// Escopos de acesso — como o portal detecta o nível de cada usuário.
		// Espelha senderzz-access-scope.php + Portal_Auth.php.
		ScopeTypes: []ScopeType{
			{
				Scope:       "admin",
				Description: "Acesso total a todos os pedidos e módulos",
				Check:       "manage_woocommerce",
			},
			{
				Scope:       "producer",
				Description: "Vê apenas seus próprios pedidos e produção",
				Check:       "portal_role=client",
			},
			{
				Scope:       "affiliate",
				Description: "Vê pedidos onde é o afiliado vinculado",
				Check:       "portal_role=affiliate",
			},
			{
				Scope:       "operator",
				Description: "OL — acesso ao painel motoboy-dia",
				Check:       "portal_role=operator",
			},
			{
				Scope:       "motoboy",
				Description: "Acesso ao PWA motoboy via token de sessão",
				Check:       "sz_motoboys.token_app",
			},
		},
	}

	httpx.JSON(w, 200, out)
}

// ----- GET /capabilities/users -------------------------------------------

// capabilityUser representa um usuário que detém capabilities Senderzz neste serviço.
// Nota: wp_usermeta (MySQL) não está espelhada neste serviço Postgres. Os usuários listados
// aqui são os admins do painel (senderzz_admin_users WHERE ativo=true); pela regra
// senderzz_admin_capability_guard(), todo usuário com manage_options recebe automaticamente
// as 8 capabilities listadas em auto_grants — logo cada admin ativo detém todas elas.
type capabilityUser struct {
	Email        string   `json:"email"`
	Nome         string   `json:"nome"`
	Capabilities []string `json:"capabilities"`
}

// CapabilityUsersResponse é o payload de GET /capabilities/users.
type CapabilityUsersResponse struct {
	Users []capabilityUser `json:"users"`
	// Note explica o escopo dos dados para consumidores da API.
	Note string `json:"note"`
}

// allAutoGrants são as capabilities concedidas a qualquer admin (manage_options → auto-grant).
// Espelha GuardConfig.AutoGrants acima — mantidos em sincronia manualmente.
var allAutoGrants = []string{
	"manage_woocommerce",
	"view_woocommerce_reports",
	"edit_shop_orders",
	"read_shop_order",
	"senderzz_admin",
	"senderzz_manage",
	"senderzz_manage_motoboy",
	"senderzz_manage_finance",
}

// tableExistsCaps verifica se a tabela existe no schema public (graceful degradation).
func (h *CapabilitiesHandler) tableExistsCaps(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// GetCapabilityUsers lista os usuários admin ativos e as capabilities que detêm.
// Como wp_usermeta não está espelhada neste serviço PG, a fonte é senderzz_admin_users.
// Cada admin ativo possui manage_options e, portanto, todas as 8 auto-grant capabilities.
func (h *CapabilitiesHandler) GetCapabilityUsers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	note := "Fonte: senderzz_admin_users (admins do painel). " +
		"wp_usermeta (MySQL) não está espelhada neste serviço. " +
		"Todo admin ativo possui manage_options e recebe as 8 capabilities por auto-grant."

	if h.Pool == nil || !h.tableExistsCaps(ctx, "senderzz_admin_users") {
		httpx.JSON(w, 200, CapabilityUsersResponse{Users: []capabilityUser{}, Note: note})
		return
	}

	rows, err := h.Pool.Query(ctx,
		`SELECT email, nome FROM senderzz_admin_users WHERE ativo=TRUE ORDER BY id`)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []capabilityUser{}
	for rows.Next() {
		var u capabilityUser
		if err := rows.Scan(&u.Email, &u.Nome); err != nil {
			httpx.Err(w, 500, "scan_error", err.Error())
			return
		}
		u.Capabilities = allAutoGrants
		out = append(out, u)
	}

	httpx.JSON(w, 200, CapabilityUsersResponse{Users: out, Note: note})
}
