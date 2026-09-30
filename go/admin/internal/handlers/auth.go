package handlers

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/auth"
	"github.com/senderzz/admin-service/internal/httpx"
	"golang.org/x/crypto/bcrypt"
)

type AuthHandler struct{ Pool *pgxpool.Pool }

type loginReq struct {
	Email string `json:"email"`
	Senha string `json:"senha"`
}

// Login — LOGIN UNIFICADO (FEAT-RBAC-2026-06-21).
//
// Valida credenciais contra senderzz_admin_users E senderzz_portal_users:
//  1. Tenta admin_users primeiro. Se o e-mail existir nos dois, ADMIN VENCE.
//     Hash = bcrypt puro → token iss=senderzz-admin (auth.IssueToken).
//  2. Se não for admin, tenta portal_users. Senha validada do MESMO jeito que o
//     portal valida (VerifyPortalPassword: ramo "$wp$" do WP 6.8 + bcrypt puro).
//     Sucesso → cria sessão em senderzz_portal_sessions + token iss=senderzz-portal.
//     Sem a sessão o token portal seria MORTO (o middleware AuthPortalJWT valida o
//     claim "sid" contra a tabela).
//
// Resposta sempre inclui {token, role, user}. A role vem SEMPRE da linha do banco —
// nunca do body. Credenciais inválidas em qualquer ramo → 401 genérico (sem revelar
// se o e-mail existe em qual tabela).
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	if req.Email == "" || req.Senha == "" {
		httpx.Err(w, 400, "bad_request", "email/senha obrigatórios")
		return
	}

	// ── 1. Admin (vence em caso de e-mail duplicado) ──────────────────────────
	var id int64
	var nome, role, hash string
	err := h.Pool.QueryRow(r.Context(),
		`SELECT id, nome, role, password_hash FROM senderzz_admin_users WHERE LOWER(email)=LOWER($1) AND ativo=TRUE`, req.Email).
		Scan(&id, &nome, &role, &hash)
	if err == nil {
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Senha)) != nil {
			httpx.Err(w, 401, "invalid_credentials", "credenciais inválidas")
			return
		}
		a := auth.Admin{ID: id, Email: req.Email, Nome: nome}
		tok, terr := auth.IssueToken(a)
		if terr != nil {
			httpx.Err(w, 500, "token_error", "erro ao emitir token")
			return
		}
		if role == "" {
			role = "admin"
		}
		httpx.JSON(w, 200, map[string]any{"token": tok, "role": role, "admin": a, "user": a})
		return
	}

	// ── 2. Portal (produtor|afiliado|operator|cliente) ────────────────────────
	var pID int64
	var pNome, pRole, pHash string
	var pAtivo, p2FA bool
	perr := h.Pool.QueryRow(r.Context(),
		`SELECT id, nome, role, COALESCE(password_hash,''), ativo, twofa_enabled
		   FROM senderzz_portal_users WHERE LOWER(email)=LOWER($1)`, req.Email).
		Scan(&pID, &pNome, &pRole, &pHash, &pAtivo, &p2FA)
	if perr != nil {
		// E-mail não existe em nenhuma tabela → 401 genérico.
		httpx.Err(w, 401, "invalid_credentials", "credenciais inválidas")
		return
	}
	if !pAtivo {
		httpx.Err(w, 403, "inactive", "conta inativa")
		return
	}
	if !auth.VerifyPortalPassword(pHash, req.Senha) {
		httpx.Err(w, 401, "invalid_credentials", "credenciais inválidas")
		return
	}
	// 2FA fail-closed: o go/admin não tem o endpoint POST /portal/login/2fa, então
	// emitir um token parcial seria inútil. Conta com 2FA deve logar pelo portal.
	if p2FA {
		httpx.Err(w, 403, "requires_2fa", "conta com 2FA habilitado — faça login pelo portal")
		return
	}

	// Cria sessão portal + emite token iss=senderzz-portal (NUNCA iss=senderzz-admin).
	sid, serr := auth.IssuePortalSession(r.Context(), h.Pool, pID, r)
	if serr != nil {
		httpx.Err(w, 500, "session_error", "erro ao criar sessão")
		return
	}
	tok, terr := auth.IssuePortalToken(pID, req.Email, pRole, sid)
	if terr != nil {
		httpx.Err(w, 500, "token_error", "erro ao emitir token")
		return
	}
	user := map[string]any{"id": pID, "email": req.Email, "nome": pNome, "role": pRole}
	httpx.JSON(w, 200, map[string]any{"token": tok, "role": pRole, "user": user})
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	a := auth.FromCtx(r.Context())
	httpx.JSON(w, 200, a)
}
