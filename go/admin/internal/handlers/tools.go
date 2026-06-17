package handlers

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

type ToolsHandler struct{ Pool *pgxpool.Pool }

func (h *ToolsHandler) Stats(w http.ResponseWriter, r *http.Request) {
	type tableCount struct {
		Table string `json:"table"`
		Count int64  `json:"count"`
	}

	tables := []string{
		"senderzz_portal_users", "sz_motoboys", "sz_motoboy_pedidos",
		"sz_motoboy_cds", "tpc_carteira", "tpc_transacoes", "tpc_recargas",
		"senderzz_affiliates", "wc_me_labels", "senderzz_webhook_log",
		"senderzz_integration_log",
	}

	out := []tableCount{}
	for _, t := range tables {
		var count int64
		_ = h.Pool.QueryRow(r.Context(), `SELECT COUNT(*) FROM `+t).Scan(&count)
		out = append(out, tableCount{Table: t, Count: count})
	}
	httpx.JSON(w, 200, map[string]any{"tables": out})
}

func (h *ToolsHandler) InsertUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WPUserID *int64  `json:"wp_user_id"`
		Email    string  `json:"email"`
		Nome     string  `json:"nome"`
		Role     string  `json:"role"`
		Plano    string  `json:"plano"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	if body.Role == "" {
		body.Role = "produtor"
	}
	if body.Plano == "" {
		body.Plano = "free"
	}
	var id int64
	err := h.Pool.QueryRow(r.Context(),
		`INSERT INTO senderzz_portal_users (wp_user_id, email, nome, role, plano, ativo)
		 VALUES ($1,$2,$3,$4,$5,TRUE)
		 ON CONFLICT (email) DO UPDATE SET nome=EXCLUDED.nome, role=EXCLUDED.role
		 RETURNING id`,
		body.WPUserID, body.Email, body.Nome, body.Role, body.Plano).Scan(&id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	httpx.JSON(w, 201, map[string]any{"id": id, "ok": true})
}
