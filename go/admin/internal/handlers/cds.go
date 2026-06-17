package handlers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

type CDsHandler struct{ Pool *pgxpool.Pool }

type cd struct {
	ID         int64    `json:"id"`
	Nome       string   `json:"nome"`
	Cidade     string   `json:"cidade"`
	UF         string   `json:"uf"`
	Endereco   *string  `json:"endereco"`
	Lat        *float64 `json:"lat"`
	Lng        *float64 `json:"lng"`
	Ativo      bool     `json:"ativo"`
	ZonaCount  int      `json:"zona_count"`
}

// zonaCountsByCd retorna mapa cd_id → contagem de zonas.
// Retorna mapa vazio com erro nil se a tabela não existir (degradação graciosa).
func (h *CDsHandler) zonaCountsByCd(ctx context.Context) (map[int64]int, error) {
	var exists bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name='sz_motoboy_zonas'
		)`).Scan(&exists)
	if !exists {
		return map[int64]int{}, nil
	}

	rows, err := h.Pool.Query(ctx,
		`SELECT cd_id, COUNT(*) FROM sz_motoboy_zonas WHERE cd_id IS NOT NULL GROUP BY cd_id`)
	if err != nil {
		return map[int64]int{}, nil // degradação graciosa
	}
	defer rows.Close()

	counts := map[int64]int{}
	for rows.Next() {
		var cdID int64
		var cnt int
		if err := rows.Scan(&cdID, &cnt); err == nil {
			counts[cdID] = cnt
		}
	}
	return counts, nil
}

func (h *CDsHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := h.Pool.Query(ctx,
		`SELECT id, nome, cidade, uf, endereco, lat, lng, ativo
		 FROM sz_motoboy_cds ORDER BY nome`)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()
	out := []cd{}
	for rows.Next() {
		var c cd
		_ = rows.Scan(&c.ID, &c.Nome, &c.Cidade, &c.UF, &c.Endereco, &c.Lat, &c.Lng, &c.Ativo)
		out = append(out, c)
	}

	// Enriquecer com contagem de zonas (degradação graciosa se tabela ausente)
	counts, _ := h.zonaCountsByCd(ctx)
	for i := range out {
		out[i].ZonaCount = counts[out[i].ID]
	}

	httpx.JSON(w, 200, map[string]any{"items": out})
}

func (h *CDsHandler) Create(w http.ResponseWriter, r *http.Request) {
	var c cd
	if err := httpx.DecodeJSON(r, &c); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	var id int64
	err := h.Pool.QueryRow(r.Context(),
		`INSERT INTO sz_motoboy_cds (nome, cidade, uf, endereco, lat, lng, ativo)
		 VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		c.Nome, c.Cidade, c.UF, c.Endereco, c.Lat, c.Lng, c.Ativo).Scan(&id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	c.ID = id
	httpx.JSON(w, 201, c)
}

func (h *CDsHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var c cd
	if err := httpx.DecodeJSON(r, &c); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	_, err := h.Pool.Exec(r.Context(),
		`UPDATE sz_motoboy_cds
		 SET nome=$1, cidade=$2, uf=$3, endereco=$4, lat=$5, lng=$6, ativo=$7, updated_at=NOW()
		 WHERE id=$8`, c.Nome, c.Cidade, c.UF, c.Endereco, c.Lat, c.Lng, c.Ativo, id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	c.ID = id
	httpx.JSON(w, 200, c)
}

func (h *CDsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	_, err := h.Pool.Exec(r.Context(),
		`UPDATE sz_motoboy_cds SET ativo=FALSE WHERE id=$1`, id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	httpx.JSON(w, 200, map[string]any{"ok": true})
}
