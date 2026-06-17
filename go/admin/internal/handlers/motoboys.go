package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
	"golang.org/x/crypto/bcrypt"
)

type MotoboysHandler struct{ Pool *pgxpool.Pool }

type motoboy struct {
	ID        int64   `json:"id"`
	Nome      string  `json:"nome"`
	Telefone  string  `json:"telefone"`
	CPF       string  `json:"cpf"`
	Email     string  `json:"email"`
	TipoPgto  string  `json:"tipo_pgto"`
	Ativo     bool    `json:"ativo"`
	CDID      *int64  `json:"cd_id"`
	ZonaID    *int64  `json:"zona_id"`
	TokenApp  string  `json:"token_app"`
	PinSet    bool    `json:"pin_set"`
	CreatedAt string  `json:"created_at"`
}

func (h *MotoboysHandler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.Pool.Query(r.Context(),
		`SELECT id, nome, COALESCE(telefone,''), COALESCE(cpf,''), COALESCE(email,''),
		        COALESCE(tipo_pgto,'autonomo'), ativo, cd_id, zona_id,
		        COALESCE(token_app,''), (pin_hash IS NOT NULL AND pin_hash <> ''),
		        created_at::text
		 FROM sz_motoboys WHERE ativo=true ORDER BY id DESC LIMIT 200`)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []motoboy{}
	for rows.Next() {
		var m motoboy
		_ = rows.Scan(&m.ID, &m.Nome, &m.Telefone, &m.CPF, &m.Email,
			&m.TipoPgto, &m.Ativo, &m.CDID, &m.ZonaID,
			&m.TokenApp, &m.PinSet, &m.CreatedAt)
		out = append(out, m)
	}
	httpx.JSON(w, 200, map[string]any{"items": out})
}

type motoboyInput struct {
	Nome     string  `json:"nome"`
	Telefone string  `json:"telefone"`
	CPF      string  `json:"cpf"`
	Email    string  `json:"email"`
	TipoPgto string  `json:"tipo_pgto"`
	Ativo    bool    `json:"ativo"`
	CDID     *int64  `json:"cd_id"`
	Pin      string  `json:"pin"`
	ZonaIDs  []int64 `json:"zona_ids"`
}

// gerarTokenApp gera token hexadecimal de 32 chars para o campo token_app.
func gerarTokenApp() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// salvarZonasMotoboy reescreve sz_motoboy_zona_pivot para o motoboy dado.
// Tabela pode não existir em instâncias mais antigas — graceful degradation.
func (h *MotoboysHandler) salvarZonasMotoboy(r *http.Request, tx pgx.Tx, motoboyID int64, zonaIDs []int64) {
	var ok bool
	_ = tx.QueryRow(r.Context(),
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name='sz_motoboy_zona_pivot'
		)`).Scan(&ok)
	if !ok {
		return
	}
	_, _ = tx.Exec(r.Context(), `DELETE FROM sz_motoboy_zona_pivot WHERE motoboy_id=$1`, motoboyID)
	for _, zid := range zonaIDs {
		if zid <= 0 {
			continue
		}
		_, _ = tx.Exec(r.Context(),
			`INSERT INTO sz_motoboy_zona_pivot (motoboy_id, zona_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
			motoboyID, zid)
	}
}

func (h *MotoboysHandler) Create(w http.ResponseWriter, r *http.Request) {
	var in motoboyInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	if in.Nome == "" {
		httpx.Err(w, 400, "bad_request", "nome obrigatório")
		return
	}

	// Valida e hash do PIN se fornecido
	var pinHash *string
	if in.Pin != "" {
		if len(in.Pin) < 4 {
			httpx.Err(w, 400, "bad_request", "PIN deve ter no mínimo 4 dígitos")
			return
		}
		ph, err := bcrypt.GenerateFromPassword([]byte(in.Pin), bcrypt.DefaultCost)
		if err != nil {
			httpx.Err(w, 500, "pin_error", "erro ao processar PIN")
			return
		}
		s := string(ph)
		pinHash = &s
	}

	// Gera token_app na criação (nunca na edição — igual ao WP)
	token, err := gerarTokenApp()
	if err != nil {
		httpx.Err(w, 500, "token_error", "erro ao gerar token")
		return
	}

	tipoPgto := in.TipoPgto
	if tipoPgto == "" {
		tipoPgto = "autonomo"
	}

	var zonaPrincipal *int64
	if len(in.ZonaIDs) > 0 && in.ZonaIDs[0] > 0 {
		zonaPrincipal = &in.ZonaIDs[0]
	}

	tx, err := h.Pool.Begin(r.Context())
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	var id int64
	err = tx.QueryRow(r.Context(),
		`INSERT INTO sz_motoboys (nome, telefone, cpf, email, tipo_pgto, ativo, cd_id, zona_id, token_app, pin_hash)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`,
		in.Nome, in.Telefone, in.CPF, in.Email, tipoPgto, in.Ativo, in.CDID, zonaPrincipal, token, pinHash).Scan(&id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	h.salvarZonasMotoboy(r, tx, id, in.ZonaIDs)

	if err := tx.Commit(r.Context()); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 201, map[string]any{"id": id})
}

func (h *MotoboysHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var in motoboyInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}

	tipoPgto := in.TipoPgto
	if tipoPgto == "" {
		tipoPgto = "autonomo"
	}

	var zonaPrincipal *int64
	if len(in.ZonaIDs) > 0 && in.ZonaIDs[0] > 0 {
		zonaPrincipal = &in.ZonaIDs[0]
	}

	tx, txErr := h.Pool.Begin(r.Context())
	if txErr != nil {
		httpx.Err(w, 500, "db_error", txErr.Error())
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	var execErr error
	// PIN: só atualiza se preenchido (blank = não apaga PIN existente)
	if in.Pin != "" {
		if len(in.Pin) < 4 {
			httpx.Err(w, 400, "bad_request", "PIN deve ter no mínimo 4 dígitos")
			return
		}
		ph, err := bcrypt.GenerateFromPassword([]byte(in.Pin), bcrypt.DefaultCost)
		if err != nil {
			httpx.Err(w, 500, "pin_error", "erro ao processar PIN")
			return
		}
		_, execErr = tx.Exec(r.Context(),
			`UPDATE sz_motoboys SET nome=$1, telefone=$2, cpf=$3, email=$4, tipo_pgto=$5,
			        ativo=$6, cd_id=$7, zona_id=$8, pin_hash=$9 WHERE id=$10`,
			in.Nome, in.Telefone, in.CPF, in.Email, tipoPgto, in.Ativo, in.CDID, zonaPrincipal, string(ph), id)
	} else {
		_, execErr = tx.Exec(r.Context(),
			`UPDATE sz_motoboys SET nome=$1, telefone=$2, cpf=$3, email=$4, tipo_pgto=$5,
			        ativo=$6, cd_id=$7, zona_id=$8 WHERE id=$9`,
			in.Nome, in.Telefone, in.CPF, in.Email, tipoPgto, in.Ativo, in.CDID, zonaPrincipal, id)
	}
	if execErr != nil {
		httpx.Err(w, 500, "db_error", execErr.Error())
		return
	}

	h.salvarZonasMotoboy(r, tx, id, in.ZonaIDs)

	if err := tx.Commit(r.Context()); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 200, map[string]any{"ok": true})
}

// Delete faz soft-delete (UPDATE ativo=false) para preservar histórico de pedidos,
// espelhando o comportamento do WP admin (UPDATE sz_motoboys SET ativo=0).
func (h *MotoboysHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	_, err := h.Pool.Exec(r.Context(), `UPDATE sz_motoboys SET ativo=FALSE WHERE id=$1`, id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	httpx.JSON(w, 200, map[string]any{"ok": true})
}

// GetZonas retorna os zona_ids vinculados ao motoboy via sz_motoboy_zona_pivot.
func (h *MotoboysHandler) GetZonas(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)

	// Graceful degradation se tabela pivot não existir
	var pivotExists bool
	_ = h.Pool.QueryRow(r.Context(),
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name='sz_motoboy_zona_pivot'
		)`).Scan(&pivotExists)
	if !pivotExists {
		httpx.JSON(w, 200, map[string]any{"zona_ids": []int64{}})
		return
	}

	rows, err := h.Pool.Query(r.Context(),
		`SELECT p.zona_id FROM sz_motoboy_zona_pivot p
		 INNER JOIN sz_motoboy_zonas z ON z.id = p.zona_id AND z.ativo = true
		 WHERE p.motoboy_id = $1 ORDER BY p.zona_id`, id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	ids := []int64{}
	for rows.Next() {
		var zid int64
		_ = rows.Scan(&zid)
		ids = append(ids, zid)
	}
	httpx.JSON(w, 200, map[string]any{"zona_ids": ids})
}

type motoboyDia struct {
	ID          int64   `json:"id"`
	Nome        string  `json:"nome"`
	Telefone    string  `json:"telefone"`
	Entregues   int64   `json:"entregues"`
	Frustrados  int64   `json:"frustrados"`
	EmRota      int64   `json:"em_rota"`
	Pendentes   int64   `json:"pendentes"`
	TotalR      float64 `json:"total_r"`
	CDNome      string  `json:"cd_nome"`
	ZonaNome    string  `json:"zona_nome"`
	TaxaSucesso float64 `json:"taxa_sucesso"`
}

func (h *MotoboysHandler) Dia(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	date := resolveDashboardDate(r.URL.Query().Get("date"))

	// Verifica existência de tabelas opcionais para graceful degradation
	hasZonas := false
	hasCDs := false
	{
		var ok bool
		_ = h.Pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_schema='public' AND table_name='sz_motoboy_zonas')`).Scan(&ok)
		hasZonas = ok
	}
	{
		var ok bool
		_ = h.Pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_schema='public' AND table_name='sz_motoboy_cds')`).Scan(&ok)
		hasCDs = ok
	}

	zonaJoin := ""
	zonaCol := "''::text AS zona_nome"
	if hasZonas {
		zonaJoin = " LEFT JOIN sz_motoboy_zonas z ON z.id = m.zona_id"
		zonaCol = "COALESCE(z.nome, '') AS zona_nome"
	}
	cdJoin := ""
	cdCol := "''::text AS cd_nome"
	if hasCDs {
		cdJoin = " LEFT JOIN sz_motoboy_cds cd ON cd.id = m.cd_id"
		cdCol = "COALESCE(cd.nome, '') AS cd_nome"
	}

	query := `
		SELECT m.id, m.nome, COALESCE(m.telefone,''),
		       COUNT(*) FILTER (WHERE p.status = 'entregue')::bigint,
		       COUNT(*) FILTER (WHERE p.status = 'frustrado')::bigint,
		       COUNT(*) FILTER (WHERE p.status IN ('em_rota','wc-em-rota','wc-em_rota','a_caminho'))::bigint,
		       COUNT(*) FILTER (WHERE p.status NOT IN ('entregue','frustrado','em_rota',
		             'wc-em-rota','wc-em_rota','a_caminho','cancelado'))::bigint,
		       COALESCE(SUM(p.valor), 0),
		       ` + cdCol + `,
		       ` + zonaCol + `,
		       CASE WHEN COUNT(p.id) > 0
		            THEN ROUND(
		                 (COUNT(*) FILTER (WHERE p.status='entregue'))::numeric
		                 / COUNT(p.id)::numeric * 100, 1)::float8
		            ELSE 0::float8
		       END AS taxa_sucesso
		  FROM sz_motoboys m` + cdJoin + zonaJoin + `
		  JOIN sz_motoboy_pedidos p ON p.motoboy_id = m.id
		 WHERE DATE(p.created_at) = $1::date
		 GROUP BY m.id, m.nome, m.telefone, cd_nome, zona_nome
		 ORDER BY COUNT(*) FILTER (WHERE p.status = 'entregue') DESC`

	rows, err := h.Pool.Query(ctx, query, date)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []motoboyDia{}
	for rows.Next() {
		var m motoboyDia
		_ = rows.Scan(&m.ID, &m.Nome, &m.Telefone, &m.Entregues, &m.Frustrados, &m.EmRota, &m.Pendentes, &m.TotalR, &m.CDNome, &m.ZonaNome, &m.TaxaSucesso)
		out = append(out, m)
	}

	// Contagem de pedidos sem motoboy atribuído no dia
	var semMotoboy int64
	_ = h.Pool.QueryRow(ctx,
		`SELECT COUNT(*)::bigint FROM sz_motoboy_pedidos
		  WHERE DATE(created_at) = $1::date
		    AND (motoboy_id IS NULL OR motoboy_id = 0)
		    AND status NOT IN ('cancelado','devolvido')`, date).Scan(&semMotoboy)

	httpx.JSON(w, 200, map[string]any{"items": out, "date": date, "sem_motoboy_count": semMotoboy})
}
