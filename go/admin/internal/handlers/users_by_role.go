// Handler genérico para listar usuários do portal por role.
//
// GET /users-by-role?role=operator|admin → { items:[...], total }
//
// Usado pelas telas de gestão de OLs (operator) e de administradores. telefone
// vem de senderzz_portal_user_meta._billing_phone (keyed por PORTAL id = u.id —
// id-space já correto) com fallback p/ a coluna u.phone — MED37. status é
// derivado de portal_users.ativo (não há coluna status) — mesma convenção de
// producers.go / users-by-role.
package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

type UsersByRoleHandler struct{ Pool *pgxpool.Pool }

type userByRole struct {
	UserID    int64  `json:"user_id"` // portal_users.id
	Nome      string `json:"nome"`
	Email     string `json:"email"`
	Telefone  string `json:"telefone"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

// List — GET /users-by-role?role=
func (h *UsersByRoleHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	role := strings.TrimSpace(q.Get("role"))
	if role == "" {
		httpx.Err(w, 400, "bad_request", "parâmetro role é obrigatório")
		return
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	search := strings.TrimSpace(q.Get("q"))

	rows, err := h.Pool.Query(r.Context(),
		`SELECT
		    u.id AS user_id,
		    COALESCE(u.nome,'')  AS nome,
		    COALESCE(u.email,'') AS email,
		    -- MED37: telefone real. id-space JÁ correto (meta keyed por PORTAL id =
		    -- u.id — ver affiliates_portal.go upsertProducerMeta). A meta WC
		    -- _billing_phone raramente é populada no schema migrado; fallback p/ a
		    -- coluna direta u.phone (preenchida no onboarding/portal).
		    COALESCE(
		        NULLIF((SELECT meta_value FROM senderzz_portal_user_meta
		                 WHERE user_id = u.id AND meta_key = '_billing_phone' LIMIT 1), ''),
		        NULLIF(u.phone, ''),
		        ''
		    ) AS telefone,
		    CASE WHEN u.ativo THEN 'ativo' ELSE 'inativo' END AS status,
		    u.created_at::text AS created_at
		 FROM senderzz_portal_users u
		 WHERE u.role = $1
		   AND ($2 = '' OR u.email ILIKE '%' || $2 || '%' OR u.nome ILIKE '%' || $2 || '%')
		 ORDER BY u.created_at DESC
		 LIMIT $3 OFFSET $4`, role, search, limit, offset)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []userByRole{}
	for rows.Next() {
		var u userByRole
		_ = rows.Scan(&u.UserID, &u.Nome, &u.Email, &u.Telefone, &u.Status, &u.CreatedAt)
		out = append(out, u)
	}

	var total int64
	_ = h.Pool.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM senderzz_portal_users
		 WHERE role = $1
		   AND ($2 = '' OR email ILIKE '%' || $2 || '%' OR nome ILIKE '%' || $2 || '%')`,
		role, search).Scan(&total)

	httpx.JSON(w, 200, map[string]any{"items": out, "total": total})
}
