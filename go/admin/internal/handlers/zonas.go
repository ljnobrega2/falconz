package handlers

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
	"strings"

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

// zonaCreate — corpo aceito no POST /zonas.
type zonaCreate struct {
	Nome              string  `json:"nome"`
	CDID              int64   `json:"cd_id"`
	Descricao         *string `json:"descricao"`
	DiasFuncionamento string  `json:"dias_funcionamento"`
	CutoffHorarios    *string `json:"cutoff_horarios"`
	Ativo             *bool   `json:"ativo"`
}

// POST /zonas
// Cria uma nova zona de entrega vinculada a um CD.
func (h *ZonasHandler) Create(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.zonaTableExists(ctx) {
		httpx.Err(w, 404, "not_found", "tabela de zonas indisponível")
		return
	}

	var in zonaCreate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	in.Nome = strings.TrimSpace(in.Nome)
	if in.Nome == "" {
		httpx.Err(w, 400, "bad_request", "nome é obrigatório")
		return
	}
	if in.CDID <= 0 {
		httpx.Err(w, 400, "bad_request", "cd_id é obrigatório")
		return
	}
	if strings.TrimSpace(in.DiasFuncionamento) == "" {
		in.DiasFuncionamento = "0,1,2,3,4,5,6"
	}
	ativo := true
	if in.Ativo != nil {
		ativo = *in.Ativo
	}

	var z zona
	err := h.Pool.QueryRow(ctx,
		`INSERT INTO sz_motoboy_zonas (cd_id, nome, descricao, dias_funcionamento, cutoff_horarios, ativo)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, cd_id, nome, descricao, dias_funcionamento, cutoff_horarios, ativo`,
		in.CDID, in.Nome, in.Descricao, in.DiasFuncionamento, in.CutoffHorarios, ativo).
		Scan(&z.ID, &z.CDID, &z.Nome, &z.Descricao, &z.DiasFuncionamento, &z.CutoffHorarios, &z.Ativo)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	httpx.JSON(w, 201, z)
}

// zonaUpdate — corpo aceito no PUT. Apenas campos editáveis seguros.
// id e created_at NUNCA são editáveis. Ponteiros = atualização parcial:
// um campo ausente no JSON mantém o valor atual no banco.
type zonaUpdate struct {
	Nome              *string `json:"nome"`
	CDID              *int64  `json:"cd_id"`
	Descricao         *string `json:"descricao"`
	DiasFuncionamento *string `json:"dias_funcionamento"`
	CutoffHorarios    *string `json:"cutoff_horarios"`
	Ativo             *bool   `json:"ativo"`
}

// PUT /zonas/{id}
// Atualiza os campos editáveis de uma zona de entrega.
// dias_funcionamento e cutoff_horarios são strings com formato fixo
// (CSV de dias 0-6 e JSON de horários) lidas também pelo router PHP do
// módulo motoboy — o front-end é responsável por enviá-las no formato correto.
func (h *ZonasHandler) Update(w http.ResponseWriter, r *http.Request) {
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

	var in zonaUpdate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}

	if in.Nome != nil && strings.TrimSpace(*in.Nome) == "" {
		httpx.Err(w, 400, "bad_request", "nome não pode ser vazio")
		return
	}

	// UPDATE parametrizado: COALESCE mantém o valor atual quando o campo
	// não foi enviado (ponteiro nil → NULL → COALESCE devolve a coluna).
	res, err := h.Pool.Exec(ctx,
		`UPDATE sz_motoboy_zonas SET
		    nome               = COALESCE($1, nome),
		    cd_id              = COALESCE($2, cd_id),
		    descricao          = COALESCE($3, descricao),
		    dias_funcionamento = COALESCE($4, dias_funcionamento),
		    cutoff_horarios    = COALESCE($5, cutoff_horarios),
		    ativo              = COALESCE($6, ativo)
		 WHERE id = $7`,
		trimPtr(in.Nome), in.CDID, in.Descricao, in.DiasFuncionamento, in.CutoffHorarios, in.Ativo, id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if res.RowsAffected() == 0 {
		httpx.Err(w, 404, "not_found", "zona não encontrada")
		return
	}

	// Retorna a zona já atualizada, no mesmo shape de List/Get.
	var z zona
	err = h.Pool.QueryRow(ctx,
		`SELECT id, COALESCE(cd_id,0), nome, descricao,
		        COALESCE(dias_funcionamento,'0,1,2,3,4,5,6'), cutoff_horarios, ativo
		 FROM sz_motoboy_zonas WHERE id=$1`, id).
		Scan(&z.ID, &z.CDID, &z.Nome, &z.Descricao, &z.DiasFuncionamento, &z.CutoffHorarios, &z.Ativo)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	httpx.JSON(w, 200, z)
}

// trimPtr aplica TrimSpace a um *string preservando o nil (para COALESCE).
func trimPtr(s *string) *string {
	if s == nil {
		return nil
	}
	t := strings.TrimSpace(*s)
	return &t
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

// validCepDigits normaliza e valida um CEP (8 dígitos).
func validCepDigits(s string) (string, bool) {
	d := reSomenteDigitos.ReplaceAllString(s, "")
	return d, len(d) == 8
}

type cepRangeCreate struct {
	ZonaID    int64  `json:"zona_id"`
	CepInicio string `json:"cep_inicio"`
	CepFim    string `json:"cep_fim"`
}

// POST /zonas/ceps
// Cria uma nova faixa de CEP vinculada a uma zona.
func (h *ZonasHandler) CepsCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.cepTableExists(ctx) {
		httpx.Err(w, 404, "not_found", "tabela de faixas de CEP indisponível")
		return
	}

	var in cepRangeCreate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	if in.ZonaID <= 0 {
		httpx.Err(w, 400, "bad_request", "zona_id é obrigatório")
		return
	}
	ini, ok1 := validCepDigits(in.CepInicio)
	fim, ok2 := validCepDigits(in.CepFim)
	if !ok1 || !ok2 {
		httpx.Err(w, 400, "bad_request", "cep_inicio e cep_fim devem ter 8 dígitos")
		return
	}
	if ini > fim {
		httpx.Err(w, 400, "bad_request", "cep_inicio deve ser menor ou igual a cep_fim")
		return
	}

	var c cepRange
	err := h.Pool.QueryRow(ctx,
		`INSERT INTO sz_motoboy_cep_zonas (zona_id, cep_inicio, cep_fim)
		 VALUES ($1, $2, $3)
		 RETURNING id, zona_id, cep_inicio, cep_fim`,
		in.ZonaID, ini, fim).
		Scan(&c.ID, &c.ZonaID, &c.CepInicio, &c.CepFim)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	httpx.JSON(w, 201, c)
}

// PUT /zonas/ceps/{id}
// Edita os limites de uma faixa de CEP existente.
func (h *ZonasHandler) CepsUpdate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	if !h.cepTableExists(ctx) {
		httpx.Err(w, 404, "not_found", "faixa de CEP não encontrada")
		return
	}

	var in cepRangeCreate
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	ini, ok1 := validCepDigits(in.CepInicio)
	fim, ok2 := validCepDigits(in.CepFim)
	if !ok1 || !ok2 {
		httpx.Err(w, 400, "bad_request", "cep_inicio e cep_fim devem ter 8 dígitos")
		return
	}
	if ini > fim {
		httpx.Err(w, 400, "bad_request", "cep_inicio deve ser menor ou igual a cep_fim")
		return
	}

	var zonaID *int64
	if in.ZonaID > 0 {
		zonaID = &in.ZonaID
	}
	res, err := h.Pool.Exec(ctx,
		`UPDATE sz_motoboy_cep_zonas SET
		    zona_id    = COALESCE($1, zona_id),
		    cep_inicio = $2,
		    cep_fim    = $3
		 WHERE id = $4`,
		zonaID, ini, fim, id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if res.RowsAffected() == 0 {
		httpx.Err(w, 404, "not_found", "faixa de CEP não encontrada")
		return
	}

	var c cepRange
	err = h.Pool.QueryRow(ctx,
		`SELECT id, zona_id, cep_inicio, cep_fim FROM sz_motoboy_cep_zonas WHERE id=$1`, id).
		Scan(&c.ID, &c.ZonaID, &c.CepInicio, &c.CepFim)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	httpx.JSON(w, 200, c)
}

// DELETE /zonas/ceps/{id}
// Remove uma faixa de CEP.
func (h *ZonasHandler) CepsDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	if !h.cepTableExists(ctx) {
		httpx.Err(w, 404, "not_found", "faixa de CEP não encontrada")
		return
	}
	tag, err := h.Pool.Exec(ctx, `DELETE FROM sz_motoboy_cep_zonas WHERE id = $1`, id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.Err(w, 404, "not_found", "faixa de CEP não encontrada")
		return
	}
	httpx.JSON(w, 200, map[string]any{"ok": true})
}

// DELETE /zonas/{id}
// Remove a zona e suas faixas de CEP (CASCADE via FK ou delete explícito).
func (h *ZonasHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	if !h.zonaTableExists(ctx) {
		httpx.Err(w, 404, "not_found", "zona não encontrada")
		return
	}
	// Remove faixas de CEP vinculadas (caso não haja FK CASCADE).
	_, _ = h.Pool.Exec(ctx, `DELETE FROM sz_motoboy_cep_zonas WHERE zona_id = $1`, id)
	tag, err := h.Pool.Exec(ctx, `DELETE FROM sz_motoboy_zonas WHERE id = $1`, id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.Err(w, 404, "not_found", "zona não encontrada")
		return
	}
	httpx.JSON(w, 200, map[string]any{"ok": true})
}
