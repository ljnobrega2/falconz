// Package handlers — endpoint admin somente leitura para visualização do modelo
// de PAPÉIS (roles) do Senderzz no full-Postgres.
//
// Atualizado 2026-06-18 (pedido do dono "atualizar conforme necessidade atual"):
// deixou de espelhar capabilities da era WordPress (manage_options/manage_woocommerce)
// e passou a refletir os 4 papéis atuais + admin, e o que cada um acessa.
// Papel é determinado por vínculo/produto em senderzz_portal_users.role, não por
// capability WP. Continua SOMENTE LEITURA.
package handlers

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

// CapabilitiesHandler expõe o modelo de papéis do Senderzz (read-only).
type CapabilitiesHandler struct {
	Pool *pgxpool.Pool
}

// ----- tipos de payload ---------------------------------------------------

// RoleAccess descreve um papel e o que ele acessa no Senderzz.
type RoleAccess struct {
	Role        string   `json:"role"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	// Fonte: como o papel é determinado no banco.
	Fonte string `json:"fonte"`
	// Acessos: o que o papel vê/faz.
	Acessos []string `json:"acessos"`
	// SemAcesso: o que o papel explicitamente NÃO acessa (vazio = sem restrição).
	SemAcesso []string `json:"sem_acesso"`
}

// CapabilitiesResponse payload completo de GET /capabilities.
type CapabilitiesResponse struct {
	Roles []RoleAccess `json:"roles"`
}

// ----- GET /capabilities -------------------------------------------------

// GetCapabilities retorna o modelo de papéis atual (estático, read-only).
func (h *CapabilitiesHandler) GetCapabilities(w http.ResponseWriter, r *http.Request) {
	out := CapabilitiesResponse{
		Roles: []RoleAccess{
			{
				Role:        "admin",
				Label:       "Administrador",
				Description: "Operador do painel administrativo (este painel). Acesso total.",
				Fonte:       "senderzz_admin_users (login e-mail + senha)",
				Acessos: []string{
					"Tudo: pedidos, financeiro (expedição/COD), afiliados, produtos",
					"Motoboy, expedição, zonas/CDs, etiquetas, comprovantes",
					"Configurações, auditoria, histórico de crons",
				},
				SemAcesso: []string{},
			},
			{
				Role:        "operator",
				Label:       "Operador Logístico (OL)",
				Description: "Opera a logística: motoboys do dia, status de entrega e fechamentos.",
				Fonte:       "senderzz_portal_users.role = 'operator'",
				Acessos: []string{
					"Motoboys do dia (KPIs, pedidos do dia)",
					"Mudar status do pedido / trocar motoboy",
					"Fechamentos e confirmação de repasse",
				},
				SemAcesso: []string{
					"Produtos / vitrine como dono",
					"Carteira de expedição do produtor",
				},
			},
			{
				Role:        "produtor",
				Label:       "Produtor",
				Description: "Dono de um produto (classe de entrega). Vê sua operação e seus afiliados.",
				Fonte:       "senderzz_portal_users.role = 'produtor' (possui produto/classe)",
				Acessos: []string{
					"Seus pedidos (e o afiliado que fez cada venda)",
					"Produtos, vitrine, ofertas / links de checkout",
					"Seus afiliados (comissões, vínculos)",
					"Carteira de expedição, webhooks/integrações, frete, localidades",
				},
				SemAcesso: []string{
					"Pedidos de outros produtores",
				},
			},
			{
				Role:        "afiliado",
				Label:       "Afiliado",
				Description: "Vinculado a ≥1 produtor. Vê só o que é dele.",
				Fonte:       "senderzz_portal_users.role = 'afiliado' (vínculo ativo)",
				Acessos: []string{
					"Vitrine e sua afiliação",
					"Seus links de checkout",
					"Somente os pedidos que ele mesmo originou",
					"Sua carteira / comissões",
				},
				SemAcesso: []string{
					"Produtos (não é dono)",
					"Expedição, motoboy, motoboys do dia",
					"Pedidos de outros afiliados",
				},
			},
			{
				Role:        "cliente",
				Label:       "Cliente",
				Description: "Comprador. Sem afiliação nem produto. Papel padrão.",
				Fonte:       "senderzz_portal_users.role = 'cliente' (padrão)",
				Acessos: []string{
					"Rastreio do próprio pedido",
				},
				SemAcesso: []string{
					"Painel (produtor/afiliado/operador)",
				},
			},
		},
	}

	httpx.JSON(w, 200, out)
}

// ----- GET /capabilities/users -------------------------------------------

// RoleCount é a contagem de usuários por papel.
type RoleCount struct {
	Role  string `json:"role"`
	Total int    `json:"total"`
}

// CapabilityUsersResponse é o payload de GET /capabilities/users.
type CapabilityUsersResponse struct {
	// Distribuicao: quantos usuários de portal há por papel (dado real do banco).
	Distribuicao []RoleCount `json:"distribuicao"`
	// AdminsAtivos: total de admins do painel ativos.
	AdminsAtivos int `json:"admins_ativos"`
	// Note explica a fonte dos dados.
	Note string `json:"note"`
}

// tableExistsCaps verifica se a tabela existe (graceful degradation).
func (h *CapabilitiesHandler) tableExistsCaps(ctx context.Context, name string) bool {
	return tableExistsCached(ctx, h.Pool, name) // AUDIT-2026-06-18 Onda2 (go-infoschema-cache)
}

// GetCapabilityUsers retorna a distribuição real de usuários por papel
// (senderzz_portal_users.role) + total de admins ativos.
func (h *CapabilitiesHandler) GetCapabilityUsers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	resp := CapabilityUsersResponse{
		Distribuicao: []RoleCount{},
		Note:         "Fonte: senderzz_portal_users.role (distribuição real) + senderzz_admin_users (admins do painel).",
	}

	if h.Pool == nil {
		httpx.JSON(w, 200, resp)
		return
	}

	if h.tableExistsCaps(ctx, "senderzz_portal_users") {
		rows, err := h.Pool.Query(ctx,
			`SELECT COALESCE(NULLIF(role,''), '(sem papel)') AS role, COUNT(*)
			   FROM senderzz_portal_users
			  GROUP BY 1
			  ORDER BY 2 DESC`)
		if err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
		defer rows.Close()
		for rows.Next() {
			var rc RoleCount
			if err := rows.Scan(&rc.Role, &rc.Total); err != nil {
				httpx.Err(w, 500, "scan_error", err.Error())
				return
			}
			resp.Distribuicao = append(resp.Distribuicao, rc)
		}
	}

	if h.tableExistsCaps(ctx, "senderzz_admin_users") {
		_ = h.Pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM senderzz_admin_users WHERE ativo=TRUE`).Scan(&resp.AdminsAtivos)
	}

	httpx.JSON(w, 200, resp)
}
