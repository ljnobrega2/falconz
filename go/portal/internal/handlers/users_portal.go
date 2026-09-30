// Package handlers — handlers de sub-usuários (equipe) do Portal V2.
//
// Espelha templates/portal/v2/sections/users.php (UX) e a lógica executável de
// src/Portal/Portal_Page.php (ajax_create_sub_user / ajax_delete_sub_user /
// render_users). "Sub-usuários" = membros da equipe do produtor com acesso ao
// painel sob o mesmo escopo de classe de envio.
//
// Rotas (namespace /wp-json/senderzz/v1):
//
//	GET    /portal/users        — lista sub-usuários do usuário autenticado
//	POST   /portal/users        — cria sub-usuário (email, senha, nome, permissões)
//	DELETE /portal/users/{id}   — exclui sub-usuário (hard-delete, ownership)
//
// Escopo / segurança (núcleo):
//   - Só o usuário "principal" (parent_user_id NULL ou 0) gerencia sub-usuários
//     — espelha o gate empty($u->parent_user_id) do Portal_Page.php. Subconta → 403.
//   - Listagem filtra WHERE parent_user_id = u.ID — cada principal vê só os seus.
//   - Delete exige id=$1 AND parent_user_id = u.ID (ownership) — ninguém apaga
//     sub-usuário de outro principal. Isso garante, por construção, que um afiliado
//     nunca recebe/manipula dados do produtor (e vice-versa).
//
// Hash de senha: bcrypt (bcrypt.GenerateFromPassword) — compatível com o login em
// auth.go (bcrypt.CompareHashAndPassword). NÃO usar phpass/wp_hash_password aqui:
// o hash phpass não seria validável pelo fluxo de login Go.
//
// Whitelist de permissões (DT-CODE-02 — exact match, nunca substring):
//
//	approve, cancel, suspend, wallet, links — espelha $perms_list de render_users.
//	Qualquer chave fora da whitelist é rejeitada com 400.
//
// Schema PG: usa colunas nome (não "name"), ativo (boolean, não "status"),
// password_hash, parent_user_id (nullable), shipping_class_id, role, created_at.
// A coluna "permissions" é gravada apenas se existir (columnExists) — não há DDL
// de migração no repo. Se ausente, o sub-usuário é criado sem permissões granulares.
package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/senderzz/portal-service/internal/auth"
	"github.com/senderzz/portal-service/internal/httpx"
)

// UsersHandler agrupa as dependências dos handlers de sub-usuários.
type UsersHandler struct {
	Pool *pgxpool.Pool
}

// listSubUsersLimit é o teto da listagem de sub-usuários (espelha o LIMIT 100
// histórico). AUDIT PERF-list-endpoints-hard-limit: a List busca limit+1 para
// detectar truncamento (has_more) sem COUNT extra e devolve só o teto.
const listSubUsersLimit = 100

// allowedUserPermissions — whitelist de permissões aceitas.
// DT-CODE-02: exact match apenas — nunca substring. Espelha $perms_list de
// Portal_Page.php::render_users (approve/cancel/suspend/wallet/links).
var allowedUserPermissions = map[string]bool{
	"approve": true, // Autorizar envio
	"cancel":  true, // Cancelar pedido
	"suspend": true, // Perda / extravio
	"wallet":  true, // Ver carteira
	"links":   true, // Gerenciar links
}

// subUserResponse representa um sub-usuário na listagem.
type subUserResponse struct {
	ID          int64           `json:"id"`
	Nome        string          `json:"nome"`
	Email       string          `json:"email"`
	Role        string          `json:"role"`
	Ativo       bool            `json:"ativo"`
	Permissions json.RawMessage `json:"permissions,omitempty"`
	CreatedAt   string          `json:"created_at"`
}

// createSubUserRequest é o body de POST /portal/users.
type createSubUserRequest struct {
	Email       string   `json:"email"`
	Senha       string   `json:"senha"`
	Nome        string   `json:"nome"`
	Permissions []string `json:"permissions"`
}

// ── Helpers de schema (guardas graceful — idêntico aos demais handlers portal) ──

func (h *UsersHandler) columnExists(ctx context.Context, table, column string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.columns
			WHERE table_schema='public' AND table_name=$1 AND column_name=$2
		)`, table, column).Scan(&ok)
	return ok
}

// isManager retorna true se o usuário do portal pode gerenciar sub-usuários
// (não é subconta). Espelha o gate empty($u->parent_user_id) do Portal_Page.php.
// parent_user_id não está no PortalUser do contexto — lido aqui (idem wallet.go).
//
// Schema dev sem a coluna parent_user_id (espelho PG ainda não tem o modelo de
// sub-usuários): degrada para manager=TRUE — sem a coluna não há como ser
// subconta, então a conta é principal por padrão (igual ao tratamento da coluna
// 'permissions' em List). Isso evita o 403 falso que travava toda a tela Equipe.
func (h *UsersHandler) isManager(ctx context.Context, portalID int64) bool {
	if !h.columnExists(ctx, "senderzz_portal_users", "parent_user_id") {
		return true
	}
	var parent *int64
	err := h.Pool.QueryRow(ctx,
		`SELECT parent_user_id FROM senderzz_portal_users WHERE id = $1`,
		portalID,
	).Scan(&parent)
	if err != nil {
		return false
	}
	return parent == nil || *parent == 0
}

// ── GET /portal/users ──────────────────────────────────────────────────────────

// List retorna os sub-usuários criados pelo usuário autenticado.
// Filtra WHERE parent_user_id = u.ID — escopado pela sessão do portal.
func (h *UsersHandler) List(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	// Só o usuário principal gerencia equipe — subconta não tem acesso.
	if !h.isManager(r.Context(), u.ID) {
		httpx.WriteErr(w, http.StatusForbidden, "sem permissão")
		return
	}

	// Schema dev sem parent_user_id: não há sub-usuários a listar. Degrada para
	// lista vazia — NUNCA rodar a query sem o filtro de escopo (vazaria todos os
	// usuários do portal). Mantém a tela Equipe acessível (200) com 0 acessos.
	if !h.columnExists(r.Context(), "senderzz_portal_users", "parent_user_id") {
		httpx.WriteOK(w, map[string]any{"data": []subUserResponse{}, "total": 0, "has_more": false, "limit": listSubUsersLimit})
		return
	}

	hasPerms := h.columnExists(r.Context(), "senderzz_portal_users", "permissions")

	// Datas EXIBIDAS em formato BR (DD/MM/AAAA HH:MM) já no backend — convenção do
	// pacote (fmtDateTimeBR / to_char em wallet.go). created_at é só display na UI
	// (Users.tsx não parseia: sem new Date / sort / filtro), então formatamos aqui.
	query := `SELECT id, COALESCE(nome,''), email, role, ativo,
	                to_char(created_at AT TIME ZONE 'America/Sao_Paulo', 'DD/MM/YYYY HH24:MI') AS created_at
	            FROM senderzz_portal_users
	           WHERE parent_user_id = $1
	           ORDER BY senderzz_portal_users.created_at DESC
	           LIMIT $2`
	if hasPerms {
		query = `SELECT id, COALESCE(nome,''), email, role, ativo,
		                COALESCE(permissions::text, '{}'),
		                to_char(created_at AT TIME ZONE 'America/Sao_Paulo', 'DD/MM/YYYY HH24:MI') AS created_at
		           FROM senderzz_portal_users
		          WHERE parent_user_id = $1
		          ORDER BY senderzz_portal_users.created_at DESC
		          LIMIT $2`
	}

	// N+1: busca 1 a mais que o teto p/ detectar truncamento sem COUNT. // PERF-list-endpoints-hard-limit
	rows, err := h.Pool.Query(r.Context(), query, u.ID, listSubUsersLimit+1)
	if err != nil {
		slog.Error("[portal_users] erro ao listar sub-usuários", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer rows.Close()

	var result []subUserResponse
	for rows.Next() {
		var su subUserResponse
		if hasPerms {
			var permsRaw string
			if err := rows.Scan(&su.ID, &su.Nome, &su.Email, &su.Role, &su.Ativo, &permsRaw, &su.CreatedAt); err != nil {
				slog.Error("[portal_users] erro ao ler linha", "user_id", u.ID, "err", err)
				httpx.WriteErr(w, http.StatusInternalServerError, "erro ao ler usuários")
				return
			}
			if permsRaw == "" {
				permsRaw = "{}"
			}
			su.Permissions = json.RawMessage(permsRaw)
		} else {
			if err := rows.Scan(&su.ID, &su.Nome, &su.Email, &su.Role, &su.Ativo, &su.CreatedAt); err != nil {
				slog.Error("[portal_users] erro ao ler linha", "user_id", u.ID, "err", err)
				httpx.WriteErr(w, http.StatusInternalServerError, "erro ao ler usuários")
				return
			}
		}
		result = append(result, su)
	}
	if rows.Err() != nil {
		slog.Error("[portal_users] erro após iteração", "user_id", u.ID, "err", rows.Err())
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao processar usuários")
		return
	}

	if result == nil {
		result = []subUserResponse{}
	}

	// has_more=true quando veio a linha extra → o front sabe que a lista foi
	// truncada (antes a truncagem em 100 era silenciosa). Devolve só o teto. // PERF-list-endpoints-hard-limit
	hasMore := len(result) > listSubUsersLimit
	if hasMore {
		result = result[:listSubUsersLimit]
	}

	httpx.WriteOK(w, map[string]any{"data": result, "total": len(result), "has_more": hasMore, "limit": listSubUsersLimit})
}

// ── POST /portal/users ──────────────────────────────────────────────────────────

// Create cria um sub-usuário vinculado ao usuário autenticado.
// Espelha ajax_create_sub_user: senha >= 8 chars, e-mail único, herda
// shipping_class_id e role do principal, ativo = TRUE.
func (h *UsersHandler) Create(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	// Só o usuário principal cria sub-usuários — subconta não pode.
	if !h.isManager(r.Context(), u.ID) {
		httpx.WriteErr(w, http.StatusForbidden, "sem permissão")
		return
	}

	// Schema dev sem parent_user_id: criar sem o vínculo geraria uma conta
	// principal órfã (produtor solto que loga no portal) — recusa explícita em
	// vez de criar o órfão silenciosamente.
	if !h.columnExists(r.Context(), "senderzz_portal_users", "parent_user_id") {
		httpx.WriteErr(w, http.StatusServiceUnavailable,
			"criação de sub-usuários indisponível: schema sem parent_user_id")
		return
	}

	var req createSubUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	req.Nome = strings.TrimSpace(req.Nome)

	if req.Email == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "e-mail é obrigatório")
		return
	}
	if len(req.Senha) < 8 {
		httpx.WriteErr(w, http.StatusBadRequest, "senha mínima 8 caracteres")
		return
	}

	// Valida permissões contra whitelist — DT-CODE-02: exact match, nunca substring.
	for _, p := range req.Permissions {
		if !allowedUserPermissions[p] {
			httpx.WriteErr(w, http.StatusBadRequest,
				"permissão inválida: "+p+
					" — permitidas: approve, cancel, suspend, wallet, links")
			return
		}
	}

	// E-mail único (pré-check; a inserção também tolera corrida via 23505).
	var existing int64
	errDup := h.Pool.QueryRow(r.Context(),
		`SELECT id FROM senderzz_portal_users WHERE email = $1 LIMIT 1`,
		req.Email,
	).Scan(&existing)
	if errDup == nil {
		httpx.WriteErr(w, http.StatusConflict, "e-mail já cadastrado")
		return
	}
	if errDup != pgx.ErrNoRows {
		slog.Error("[portal_users] erro ao verificar e-mail", "user_id", u.ID, "err", errDup)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Herda role e shipping_class_id do principal — o sub-usuário opera no mesmo
	// escopo. (O PHP omite a role no insert, contando com default do MySQL; em PG
	// herdar é mais seguro e mantém o escopo por role coerente.)
	var ownerRole string
	var ownerClassID *int64
	errOwner := h.Pool.QueryRow(r.Context(),
		`SELECT role, shipping_class_id FROM senderzz_portal_users WHERE id = $1`,
		u.ID,
	).Scan(&ownerRole, &ownerClassID)
	if errOwner != nil {
		slog.Error("[portal_users] erro ao ler dados do principal", "user_id", u.ID, "err", errOwner)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Hash bcrypt — compatível com o login (auth.go: CompareHashAndPassword).
	hash, errHash := bcrypt.GenerateFromPassword([]byte(req.Senha), bcrypt.DefaultCost)
	if errHash != nil {
		slog.Error("[portal_users] erro ao gerar hash de senha", "user_id", u.ID, "err", errHash)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	hasPerms := h.columnExists(r.Context(), "senderzz_portal_users", "permissions")

	var id int64
	var err error
	if hasPerms {
		permsJSON := buildPermsJSON(req.Permissions)
		err = h.Pool.QueryRow(r.Context(),
			`INSERT INTO senderzz_portal_users
			    (email, nome, role, password_hash, shipping_class_id,
			     parent_user_id, permissions, ativo, twofa_enabled, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, TRUE, FALSE, NOW())
			 RETURNING id`,
			req.Email, req.Nome, ownerRole, string(hash), ownerClassID,
			u.ID, permsJSON,
		).Scan(&id)
	} else {
		err = h.Pool.QueryRow(r.Context(),
			`INSERT INTO senderzz_portal_users
			    (email, nome, role, password_hash, shipping_class_id,
			     parent_user_id, ativo, twofa_enabled, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6, TRUE, FALSE, NOW())
			 RETURNING id`,
			req.Email, req.Nome, ownerRole, string(hash), ownerClassID,
			u.ID,
		).Scan(&id)
	}
	if err != nil {
		// Corrida na unicidade de e-mail (constraint 23505).
		if strings.Contains(err.Error(), "23505") {
			httpx.WriteErr(w, http.StatusConflict, "e-mail já cadastrado")
			return
		}
		slog.Error("[portal_users] erro ao criar sub-usuário", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	slog.Info("[portal_users] sub-usuário criado", "user_id", u.ID, "sub_user_id", id, "email", req.Email)
	httpx.WriteOK(w, map[string]any{"id": id, "mensagem": "acesso criado com sucesso"})
}

// ── DELETE /portal/users/{id} ────────────────────────────────────────────────────

// Delete remove (hard-delete) um sub-usuário do usuário autenticado.
// Espelha ajax_delete_sub_user: confere ownership via parent_user_id antes de apagar.
func (h *UsersHandler) Delete(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	// Só o usuário principal exclui sub-usuários.
	if !h.isManager(r.Context(), u.ID) {
		httpx.WriteErr(w, http.StatusForbidden, "sem permissão")
		return
	}

	// SEGURANÇA: schema dev sem parent_user_id — sem essa coluna o DELETE perderia
	// o filtro de ownership (AND parent_user_id = $2) e viraria delete arbitrário
	// por id (qualquer usuário apagaria qualquer conta). Degrada para no-op 404,
	// NUNCA executa o DELETE sem escopo.
	if !h.columnExists(r.Context(), "senderzz_portal_users", "parent_user_id") {
		httpx.WriteErr(w, http.StatusNotFound, "usuário não encontrado")
		return
	}

	idStr := chi.URLParam(r, "id")
	subUserID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || subUserID <= 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}

	// Hard-delete com ownership: só apaga se parent_user_id = u.ID.
	result, err := h.Pool.Exec(r.Context(),
		`DELETE FROM senderzz_portal_users
		  WHERE id = $1 AND parent_user_id = $2`,
		subUserID, u.ID,
	)
	if err != nil {
		slog.Error("[portal_users] erro ao excluir sub-usuário", "user_id", u.ID, "sub_user_id", subUserID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	if result.RowsAffected() == 0 {
		httpx.WriteErr(w, http.StatusNotFound, "usuário não encontrado")
		return
	}

	slog.Info("[portal_users] sub-usuário excluído", "user_id", u.ID, "sub_user_id", subUserID)
	httpx.WriteOK(w, map[string]any{"mensagem": "acesso excluído com sucesso"})
}

// ── Helpers privados ──────────────────────────────────────────────────────────

// buildPermsJSON converte a lista de chaves de permissão em JSON {chave:true},
// espelhando o formato gravado por Portal_Page.php (json_decode(...permissions...)).
func buildPermsJSON(perms []string) string {
	m := make(map[string]bool, len(perms))
	for _, p := range perms {
		if allowedUserPermissions[p] {
			m[p] = true
		}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}
