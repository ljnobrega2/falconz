package handlers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

type WalletHandler struct{ Pool *pgxpool.Pool }

func (h *WalletHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

type carteira struct {
	ID             int64   `json:"id"`
	UserID         int64   `json:"user_id"`
	Saldo          float64 `json:"saldo"`
	SaldoReservado float64 `json:"saldo_reservado"`
	CreatedAt      string  `json:"created_at"`
}

type transacao struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"user_id"`
	Tipo       string  `json:"tipo"`
	Valor      float64 `json:"valor"`
	SaldoApos  float64 `json:"saldo_apos"`
	Descricao  string  `json:"descricao"`
	Referencia *string `json:"referencia"`
	OrderID    *int64  `json:"order_id"`
	Status     string  `json:"status"`
	CreatedAt  string  `json:"created_at"`
}

func (h *WalletHandler) ListCarteiras(w http.ResponseWriter, r *http.Request) {
	if !h.tableExists(r.Context(), "tpc_carteira") {
		httpx.JSON(w, 200, map[string]any{"items": []carteira{}})
		return
	}
	rows, err := h.Pool.Query(r.Context(),
		`SELECT id, user_id, saldo, saldo_reservado, created_at::text
		 FROM tpc_carteira ORDER BY saldo DESC LIMIT 200`)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()
	out := []carteira{}
	for rows.Next() {
		var c carteira
		_ = rows.Scan(&c.ID, &c.UserID, &c.Saldo, &c.SaldoReservado, &c.CreatedAt)
		out = append(out, c)
	}
	httpx.JSON(w, 200, map[string]any{"items": out})
}

func (h *WalletHandler) ListTransacoes(w http.ResponseWriter, r *http.Request) {
	if !h.tableExists(r.Context(), "tpc_transacoes") {
		httpx.JSON(w, 200, map[string]any{"items": []transacao{}})
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	userID, _ := strconv.ParseInt(q.Get("user_id"), 10, 64)

	rows, err := h.Pool.Query(r.Context(),
		`SELECT id, user_id, tipo, valor, saldo_apos, descricao, referencia, order_id, status, created_at::text
		 FROM tpc_transacoes
		 WHERE ($1=0 OR user_id=$1)
		 ORDER BY id DESC LIMIT $2`, userID, limit)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()
	out := []transacao{}
	for rows.Next() {
		var t transacao
		_ = rows.Scan(&t.ID, &t.UserID, &t.Tipo, &t.Valor, &t.SaldoApos, &t.Descricao, &t.Referencia, &t.OrderID, &t.Status, &t.CreatedAt)
		out = append(out, t)
	}
	httpx.JSON(w, 200, map[string]any{"items": out})
}
