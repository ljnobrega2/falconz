package handlers

import (
	"context"
	"net/http"
	"regexp"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

// ZonasHandler — endpoints de leitura de zonas e faixas de CEP.
// Tabelas: sz_motoboy_zonas, sz_motoboy_cep_zonas.
type ZonasHandler struct{ Pool *pgxpool.Pool }

func (h *ZonasHandler) zonaTableExists(ctx context.Context) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name='sz_motoboy_zonas'
		)`).Scan(&ok)
	return ok
}

func (h *ZonasHandler) cepTableExists(ctx context.Context) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name='sz_motoboy_cep_zonas'
		)`).Scan(&ok)
	return ok
}

type zona struct {
	ID                int64   `json:"id"`
	CDID              int64   `json:"cd_id"`
	Nome              string  `json:"nome"`
	Descricao         *string `json:"descricao"`
	DiasFuncionamento string  `json:"dias_funcionamento"`
	CutoffHorarios    *string `json:"cutoff_horarios"`
	Ativo             bool    `json:"ativo"`
}

type cepRange struct {
	ID        int64  `json:"id"`
	ZonaID    int64  `json:"zona_id"`
	CepInicio string `json:"cep_inicio"`
	CepFim    string `json:"cep_fim"`
}

// reSomenteDigitos remove caracteres não numéricos de um CEP.
var reSomenteDigitos = regexp.MustCompile(`\D`)

// GET /zonas
func (h *ZonasHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.zonaTableExists(ctx) {
		httpx.JSON(w, 200, map[string]any{"items": []zona{}})
		return
	}

	rows, err := h.Pool.Query(ctx,
		`SELECT id, COALESCE(cd_id,0), nome, descricao,
		        COALESCE(dias_funcionamento,'0,1,2,3,4,5,6'), cutoff_horarios, ativo
		 FROM sz_motoboy_zonas ORDER BY cd_id, nome`)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []zona{}
	for rows.Next() {
		var z zona
		_ = rows.Scan(&z.ID, &z.CDID, &z.Nome, &z.Descricao, &z.DiasFuncionamento, &z.CutoffHorarios, &z.Ativo)
		out = append(out, z)
	}
	httpx.JSON(w, 200, map[string]any{"items": out})
}

// GET /zonas/{id}
func (h *ZonasHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	if !h.zonaTableExists(ctx) {
		httpx.Err(w, 404, "not_found", "zona não encontrada")
		return
	}

	var z zona
	err = h.Pool.QueryRow(ctx,
		`SELECT id, COALESCE(cd_id,0), nome, descricao,
		        COALESCE(dias_funcionamento,'0,1,2,3,4,5,6'), cutoff_horarios, ativo
		 FROM sz_motoboy_zonas WHERE id=$1`, id).
		Scan(&z.ID, &z.CDID, &z.Nome, &z.Descricao, &z.DiasFuncionamento, &z.CutoffHorarios, &z.Ativo)
	if err != nil {
		httpx.Err(w, 404, "not_found", "zona não encontrada")
		return
	}
	httpx.JSON(w, 200, z)
}

// GET /zonas/cep-check?cep=12345678
// Verifica a qual zona e dias de operação um CEP pertence.
// Retorna 200 com found=true|false; nunca 404 para facilitar o preview no React.
func (h *ZonasHandler) CepCheck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cep := reSomenteDigitos.ReplaceAllString(r.URL.Query().Get("cep"), "")
	if len(cep) != 8 {
		httpx.Err(w, 400, "bad_request", "CEP deve ter 8 dígitos")
		return
	}

	if !h.zonaTableExists(ctx) || !h.cepTableExists(ctx) {
		httpx.JSON(w, 200, map[string]any{"found": false})
		return
	}

	var zonaID int64
	var zonaNome, diasFuncionamento string
	var cutoffHorarios *string
	err := h.Pool.QueryRow(ctx,
		`SELECT z.id, z.nome,
		        COALESCE(z.dias_funcionamento,'0,1,2,3,4,5,6'),
		        z.cutoff_horarios
		 FROM sz_motoboy_cep_zonas cz
		 JOIN sz_motoboy_zonas z ON z.id = cz.zona_id AND z.ativo = true
		 WHERE cz.cep_inicio <= $1 AND cz.cep_fim >= $1
		 LIMIT 1`, cep).
		Scan(&zonaID, &zonaNome, &diasFuncionamento, &cutoffHorarios)
	if err != nil {
		// CEP fora das faixas cadastradas
		httpx.JSON(w, 200, map[string]any{"found": false})
		return
	}

	httpx.JSON(w, 200, map[string]any{
		"found":              true,
		"zona_id":            zonaID,
		"zona_nome":          zonaNome,
		"dias_funcionamento": diasFuncionamento,
		"cutoff_horarios":    cutoffHorarios,
	})
}

// GET /zonas/ceps
func (h *ZonasHandler) Ceps(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.cepTableExists(ctx) {
		httpx.JSON(w, 200, map[string]any{"items": []cepRange{}})
		return
	}

	rows, err := h.Pool.Query(ctx,
		`SELECT id, zona_id, COALESCE(cep_inicio,''), COALESCE(cep_fim,'')
		 FROM sz_motoboy_cep_zonas ORDER BY zona_id, cep_inicio`)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []cepRange{}
	for rows.Next() {
		var c cepRange
		_ = rows.Scan(&c.ID, &c.ZonaID, &c.CepInicio, &c.CepFim)
		out = append(out, c)
	}
	httpx.JSON(w, 200, map[string]any{"items": out})
}
