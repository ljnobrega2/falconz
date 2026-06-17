package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

type UsersHandler struct{ Pool *pgxpool.Pool }

type portalUser struct {
	ID          int64  `json:"id"`
	WPUserID    *int64 `json:"wp_user_id"`
	Email       string `json:"email"`
	Nome        string `json:"nome"`
	Role        string `json:"role"`
	Ativo       bool   `json:"ativo"`
	Plano       string `json:"plano"`
	CreatedAt   string `json:"created_at"`
}

func (h *UsersHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	search := q.Get("q")

	rows, err := h.Pool.Query(r.Context(),
		`SELECT id, wp_user_id, email, nome, role, ativo, plano, created_at::text
		 FROM senderzz_portal_users
		 WHERE ($1='' OR email ILIKE '%'||$1||'%' OR nome ILIKE '%'||$1||'%')
		 ORDER BY id DESC LIMIT $2 OFFSET $3`, search, limit, offset)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []portalUser{}
	for rows.Next() {
		var u portalUser
		if err := rows.Scan(&u.ID, &u.WPUserID, &u.Email, &u.Nome, &u.Role, &u.Ativo, &u.Plano, &u.CreatedAt); err != nil {
			httpx.Err(w, 500, "scan_error", err.Error())
			return
		}
		out = append(out, u)
	}

	var total int64
	_ = h.Pool.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM senderzz_portal_users
		 WHERE ($1='' OR email ILIKE '%'||$1||'%' OR nome ILIKE '%'||$1||'%')`, search).Scan(&total)

	httpx.JSON(w, 200, map[string]any{"items": out, "total": total, "limit": limit, "offset": offset})
}

func (h *UsersHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var u portalUser
	err := h.Pool.QueryRow(r.Context(),
		`SELECT id, wp_user_id, email, nome, role, ativo, plano, created_at::text
		 FROM senderzz_portal_users WHERE id=$1`, id).
		Scan(&u.ID, &u.WPUserID, &u.Email, &u.Nome, &u.Role, &u.Ativo, &u.Plano, &u.CreatedAt)
	if err != nil {
		httpx.Err(w, 404, "not_found", "usuário não encontrado")
		return
	}
	httpx.JSON(w, 200, u)
}

type userPatch struct {
	Nome  *string `json:"nome"`
	Role  *string `json:"role"`
	Ativo *bool   `json:"ativo"`
	Plano *string `json:"plano"`
}

func (h *UsersHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var p userPatch
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	_, err := h.Pool.Exec(r.Context(),
		`UPDATE senderzz_portal_users
		 SET nome  = COALESCE($1, nome),
		     role  = COALESCE($2, role),
		     ativo = COALESCE($3, ativo),
		     plano = COALESCE($4, plano)
		 WHERE id=$5`, p.Nome, p.Role, p.Ativo, p.Plano, id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	h.Get(w, r)
}
