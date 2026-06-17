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
	var id int64
	var nome, hash string
	err := h.Pool.QueryRow(r.Context(),
		`SELECT id, nome, password_hash FROM senderzz_admin_users WHERE LOWER(email)=LOWER($1) AND ativo=TRUE`, req.Email).
		Scan(&id, &nome, &hash)
	if err != nil {
		httpx.Err(w, 401, "invalid_credentials", "credenciais inválidas")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Senha)); err != nil {
		httpx.Err(w, 401, "invalid_credentials", "credenciais inválidas")
		return
	}
	a := auth.Admin{ID: id, Email: req.Email, Nome: nome}
	tok, err := auth.IssueToken(a)
	if err != nil {
		httpx.Err(w, 500, "token_error", "erro ao emitir token")
		return
	}
	httpx.JSON(w, 200, map[string]any{"token": tok, "admin": a})
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	a := auth.FromCtx(r.Context())
	httpx.JSON(w, 200, a)
}
