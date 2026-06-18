// Package handlers — endpoints de afiliados.
//
// Rotas cobertas (namespace /wp-json/senderzz/v1):
//   GET    /affiliates            — lista vínculos do usuário autenticado
//   POST   /affiliates/request    — afiliado solicita vínculo a produtor+produto
//   POST   /affiliates/{id}/approve — produtor aprova solicitação
//   POST   /affiliates/{id}/revoke  — produtor revoga vínculo
//   GET    /affiliates/invites    — lista convites pendentes do produtor
//   POST   /affiliates/invites    — produtor cria convite (token 64-hex, expira 7d)
//   DELETE /affiliates/invites/{token} — produtor revoga convite
//   GET    /affiliates/commissions      — lista comissões (filter: status)
//   GET    /affiliates/commissions/summary — totais por status
//   GET    /affiliates/links      — lista links de checkout do afiliado
//   POST   /affiliates/links      — afiliado cria link (link_token 32 bytes hex)
//   DELETE /affiliates/links/{id} — desativa link
//
// Auth: AuthPortalJWT (JWT Bearer). Produtor e afiliado veem dados distintos
// dependendo do role extraído do token.
//
// Comentários em PT-BR conforme convenção do projeto.
package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/senderzz/affiliates-service/internal/auth"
	"github.com/senderzz/affiliates-service/internal/httpx"
)

// AffiliatesHandler agrupa as dependências dos handlers de afiliados.
type AffiliatesHandler struct {
	Pool *pgxpool.Pool
}

// ── GET /affiliates ───────────────────────────────────────────────────────────

// List retorna os vínculos do usuário autenticado.
//
//   - Produtor (role="produtor"): todos os vínculos onde produtor_id = user_id.
//   - Afiliado (role="affiliate"): todos os vínculos onde afiliado_id = user_id.
//   - Outros roles: retorna lista de vínculos como afiliado (comportamento seguro).
//
// Parâmetro de query opcional: status (filtra por status do vínculo).
func (h *AffiliatesHandler) List(w http.ResponseWriter, r *http.Request) {
	user := auth.PortalUserFromCtx(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	statusFilter := r.URL.Query().Get("status")

	// Monta query dependendo do role.
	var rows pgx.Rows
	var err error

	base := `SELECT id, produtor_id, afiliado_id, produto_id, status, comissao_pct, created_at, updated_at
	           FROM senderzz_affiliates`

	if user.Role == "produtor" {
		if statusFilter != "" {
			rows, err = h.Pool.Query(r.Context(),
				base+` WHERE produtor_id = $1 AND status = $2 ORDER BY created_at DESC`,
				user.ID, statusFilter)
		} else {
			rows, err = h.Pool.Query(r.Context(),
				base+` WHERE produtor_id = $1 ORDER BY created_at DESC`,
				user.ID)
		}
	} else {
		// Afiliado ou outro role: mostra onde é o afiliado.
		if statusFilter != "" {
			rows, err = h.Pool.Query(r.Context(),
				base+` WHERE afiliado_id = $1 AND status = $2 ORDER BY created_at DESC`,
				user.ID, statusFilter)
		} else {
			rows, err = h.Pool.Query(r.Context(),
				base+` WHERE afiliado_id = $1 ORDER BY created_at DESC`,
				user.ID)
		}
	}
	if err != nil {
		slog.Error("[affiliates] falha ao listar vínculos", "user_id", user.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao buscar vínculos")
		return
	}
	defer rows.Close()

	type affiliateRow struct {
		ID          int64           `json:"id"`
		ProdutorID  int64           `json:"produtor_id"`
		AfiliadoID  int64           `json:"afiliado_id"`
		ProdutoID   int64           `json:"produto_id"`
		Status      string          `json:"status"`
		ComissaoPct decimal.Decimal `json:"comissao_pct"`
		CreatedAt   time.Time       `json:"created_at"`
		UpdatedAt   time.Time       `json:"updated_at"`
	}

	var result []affiliateRow
	for rows.Next() {
		var row affiliateRow
		var pctStr string
		if err := rows.Scan(
			&row.ID, &row.ProdutorID, &row.AfiliadoID, &row.ProdutoID,
			&row.Status, &pctStr, &row.CreatedAt, &row.UpdatedAt,
		); err != nil {
			slog.Error("[affiliates] erro ao ler linha", "user_id", user.ID, "err", err)
			continue
		}
		row.ComissaoPct, _ = decimal.NewFromString(pctStr)
		result = append(result, row)
	}
	if result == nil {
		result = []affiliateRow{}
	}

	httpx.WriteOK(w, map[string]any{"data": result, "total": len(result)})
}

// ── POST /affiliates/request ──────────────────────────────────────────────────

// Request — afiliado solicita vínculo com um produtor para um produto específico.
// Body: {produtor_id, produto_id}
// O usuário autenticado é o afiliado; o vínculo começa com status="pending".
func (h *AffiliatesHandler) Request(w http.ResponseWriter, r *http.Request) {
	user := auth.PortalUserFromCtx(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	var req struct {
		ProdutorID int64 `json:"produtor_id"`
		ProdutoID  int64 `json:"produto_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	if req.ProdutorID == 0 || req.ProdutoID == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "produtor_id e produto_id são obrigatórios")
		return
	}

	// Impede auto-afiliação.
	if req.ProdutorID == user.ID {
		httpx.WriteErr(w, http.StatusBadRequest, "produtor não pode se afiliar ao próprio produto")
		return
	}

	ctx := r.Context()

	// INSERT idempotente — ON CONFLICT na UNIQUE (produtor_id, afiliado_id, produto_id).
	var id int64
	err := h.Pool.QueryRow(ctx, `
		INSERT INTO senderzz_affiliates (produtor_id, afiliado_id, produto_id, status)
		VALUES ($1, $2, $3, 'pending')
		ON CONFLICT (produtor_id, afiliado_id, produto_id) DO NOTHING
		RETURNING id`,
		req.ProdutorID, user.ID, req.ProdutoID,
	).Scan(&id)

	if err == pgx.ErrNoRows {
		// Vínculo já existe — busca o ID existente.
		var existingID int64
		var existingStatus string
		err2 := h.Pool.QueryRow(ctx,
			`SELECT id, status FROM senderzz_affiliates
			  WHERE produtor_id=$1 AND afiliado_id=$2 AND produto_id=$3`,
			req.ProdutorID, user.ID, req.ProdutoID,
		).Scan(&existingID, &existingStatus)
		if err2 != nil {
			slog.Error("[affiliates] erro ao buscar vínculo existente", "user_id", user.ID, "err", err2)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
			return
		}
		slog.Info("[affiliates] solicitação já existente (idempotente)",
			"afiliado_id", user.ID, "affiliate_id", existingID, "status", existingStatus)
		httpx.WriteOK(w, map[string]any{
			"affiliate_id": existingID,
			"status":       existingStatus,
			"idempotente":  true,
		})
		return
	}
	if err != nil {
		slog.Error("[affiliates] falha ao criar solicitação", "user_id", user.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao criar solicitação")
		return
	}

	slog.Info("[affiliates] solicitação criada",
		"affiliate_id", id, "afiliado_id", user.ID, "produtor_id", req.ProdutorID)
	httpx.WriteOK(w, map[string]any{"affiliate_id": id, "status": "pending"})
}

// ── POST /affiliates/{id}/approve ─────────────────────────────────────────────

// Approve — produtor aprova uma solicitação de afiliação pendente.
// Apenas o produtor dono do vínculo pode aprovar.
// Body opcional: {comissao_pct} para definir percentual no momento da aprovação.
func (h *AffiliatesHandler) Approve(w http.ResponseWriter, r *http.Request) {
	user := auth.PortalUserFromCtx(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}

	var req struct {
		ComissaoPct *decimal.Decimal `json:"comissao_pct"`
	}
	// Body é opcional — ignora erro de decode se body vazio.
	_ = json.NewDecoder(r.Body).Decode(&req)

	ctx := r.Context()

	// Verifica que o vínculo existe e pertence ao produtor autenticado.
	var currentStatus string
	var currentPct string
	err = h.Pool.QueryRow(ctx,
		`SELECT status, comissao_pct FROM senderzz_affiliates WHERE id=$1 AND produtor_id=$2`,
		id, user.ID,
	).Scan(&currentStatus, &currentPct)
	if err == pgx.ErrNoRows {
		httpx.WriteErr(w, http.StatusNotFound, "vínculo não encontrado ou sem permissão")
		return
	}
	if err != nil {
		slog.Error("[affiliates] erro ao buscar vínculo para aprovação", "id", id, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Idempotência: já ativo → retorna ok.
	if currentStatus == "active" {
		httpx.WriteOK(w, map[string]any{"affiliate_id": id, "status": "active", "idempotente": true})
		return
	}
	if currentStatus == "revoked" {
		httpx.WriteErr(w, http.StatusConflict, "vínculo já revogado — crie um novo vínculo")
		return
	}

	// Determina percentual de comissão (usa valor informado ou mantém o atual).
	newPct := currentPct
	if req.ComissaoPct != nil {
		newPct = req.ComissaoPct.StringFixed(2)
	}

	_, err = h.Pool.Exec(ctx,
		`UPDATE senderzz_affiliates
		    SET status='active', comissao_pct=$1, updated_at=NOW()
		  WHERE id=$2`,
		newPct, id,
	)
	if err != nil {
		slog.Error("[affiliates] falha ao aprovar vínculo", "id", id, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao aprovar vínculo")
		return
	}

	slog.Info("[affiliates] vínculo aprovado", "affiliate_id", id, "produtor_id", user.ID)
	httpx.WriteOK(w, map[string]any{"affiliate_id": id, "status": "active"})
}

// ── POST /affiliates/{id}/revoke ──────────────────────────────────────────────

// Revoke — produtor revoga um vínculo ativo. Comissões pendentes não são canceladas.
func (h *AffiliatesHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	user := auth.PortalUserFromCtx(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}

	ctx := r.Context()

	// Só o produtor pode revogar — valida propriedade.
	var currentStatus string
	err = h.Pool.QueryRow(ctx,
		`SELECT status FROM senderzz_affiliates WHERE id=$1 AND produtor_id=$2`,
		id, user.ID,
	).Scan(&currentStatus)
	if err == pgx.ErrNoRows {
		httpx.WriteErr(w, http.StatusNotFound, "vínculo não encontrado ou sem permissão")
		return
	}
	if err != nil {
		slog.Error("[affiliates] erro ao buscar vínculo para revogação", "id", id, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Idempotência: já revogado → ok.
	if currentStatus == "revoked" {
		httpx.WriteOK(w, map[string]any{"affiliate_id": id, "status": "revoked", "idempotente": true})
		return
	}

	_, err = h.Pool.Exec(ctx,
		`UPDATE senderzz_affiliates
		    SET status='revoked', updated_at=NOW()
		  WHERE id=$1`,
		id,
	)
	if err != nil {
		slog.Error("[affiliates] falha ao revogar vínculo", "id", id, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao revogar vínculo")
		return
	}

	slog.Info("[affiliates] vínculo revogado", "affiliate_id", id, "produtor_id", user.ID)
	httpx.WriteOK(w, map[string]any{"affiliate_id": id, "status": "revoked"})
}

// ── GET /affiliates/invites ───────────────────────────────────────────────────

// ListInvites — lista convites pendentes (não usados e não expirados) do produtor.
func (h *AffiliatesHandler) ListInvites(w http.ResponseWriter, r *http.Request) {
	user := auth.PortalUserFromCtx(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	rows, err := h.Pool.Query(r.Context(), `
		SELECT id, email, token, expires_at, used_at, created_at
		  FROM senderzz_affiliate_invites
		 WHERE produtor_id = $1
		   AND used_at IS NULL
		   AND expires_at > NOW()
		 ORDER BY created_at DESC`,
		user.ID,
	)
	if err != nil {
		slog.Error("[affiliates] falha ao listar convites", "user_id", user.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao buscar convites")
		return
	}
	defer rows.Close()

	type inviteRow struct {
		ID        int64      `json:"id"`
		Email     string     `json:"email"`
		Token     string     `json:"token"`
		ExpiresAt time.Time  `json:"expires_at"`
		UsedAt    *time.Time `json:"used_at"`
		CreatedAt time.Time  `json:"created_at"`
	}

	var result []inviteRow
	for rows.Next() {
		var row inviteRow
		if err := rows.Scan(&row.ID, &row.Email, &row.Token, &row.ExpiresAt, &row.UsedAt, &row.CreatedAt); err != nil {
			continue
		}
		result = append(result, row)
	}
	if result == nil {
		result = []inviteRow{}
	}

	httpx.WriteOK(w, map[string]any{"data": result, "total": len(result)})
}

// ── POST /affiliates/invites ──────────────────────────────────────────────────

// CreateInvite — produtor cria convite para um e-mail específico.
// Token único de 64 chars hex; expira em 7 dias.
// Body: {email}
func (h *AffiliatesHandler) CreateInvite(w http.ResponseWriter, r *http.Request) {
	user := auth.PortalUserFromCtx(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "email obrigatório")
		return
	}

	token, err := randomHex(32) // 32 bytes = 64 chars hex
	if err != nil {
		slog.Error("[affiliates] falha ao gerar token de convite", "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao gerar convite")
		return
	}

	expiresAt := time.Now().UTC().Add(7 * 24 * time.Hour)

	var id int64
	err = h.Pool.QueryRow(r.Context(), `
		INSERT INTO senderzz_affiliate_invites (produtor_id, email, token, expires_at)
		VALUES ($1, $2, $3, $4)
		RETURNING id`,
		user.ID, req.Email, token, expiresAt,
	).Scan(&id)
	if err != nil {
		slog.Error("[affiliates] falha ao criar convite", "user_id", user.ID, "email", req.Email, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao criar convite")
		return
	}

	slog.Info("[affiliates] convite criado", "invite_id", id, "produtor_id", user.ID, "email", req.Email)
	httpx.WriteOK(w, map[string]any{
		"invite_id":  id,
		"token":      token,
		"email":      req.Email,
		"expires_at": expiresAt,
	})
}

// ── DELETE /affiliates/invites/{token} ────────────────────────────────────────

// RevokeInvite — produtor revoga um convite pendente (marca expires_at no passado).
func (h *AffiliatesHandler) RevokeInvite(w http.ResponseWriter, r *http.Request) {
	user := auth.PortalUserFromCtx(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	token := chi.URLParam(r, "token")
	if token == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "token obrigatório")
		return
	}

	ctx := r.Context()

	// Verifica propriedade antes de revogar.
	var id int64
	err := h.Pool.QueryRow(ctx,
		`SELECT id FROM senderzz_affiliate_invites WHERE token=$1 AND produtor_id=$2 AND used_at IS NULL`,
		token, user.ID,
	).Scan(&id)
	if err == pgx.ErrNoRows {
		httpx.WriteErr(w, http.StatusNotFound, "convite não encontrado, já utilizado ou sem permissão")
		return
	}
	if err != nil {
		slog.Error("[affiliates] erro ao buscar convite para revogação", "token", token, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Revoga expiração para o passado (convite não pode mais ser aceito).
	_, err = h.Pool.Exec(ctx,
		`UPDATE senderzz_affiliate_invites SET expires_at=NOW() - INTERVAL '1 second' WHERE id=$1`,
		id,
	)
	if err != nil {
		slog.Error("[affiliates] falha ao revogar convite", "invite_id", id, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao revogar convite")
		return
	}

	slog.Info("[affiliates] convite revogado", "invite_id", id, "produtor_id", user.ID)
	httpx.WriteOK(w, map[string]any{"invite_id": id, "revogado": true})
}

// ── GET /affiliates/commissions ───────────────────────────────────────────────

// ListCommissions — lista comissões do usuário autenticado.
//
//   - Afiliado: todas as suas comissões.
//   - Produtor: todas as comissões dos seus afiliados.
//
// Parâmetro de query opcional: status (pendente|aprovada|paga|estornada).
// Parâmetro de query opcional: limit (default 50, máximo 200).
func (h *AffiliatesHandler) ListCommissions(w http.ResponseWriter, r *http.Request) {
	user := auth.PortalUserFromCtx(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	statusFilter := r.URL.Query().Get("status")
	limit := 50
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if n, err := strconv.Atoi(lStr); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}

	var rows pgx.Rows
	var err error

	base := `SELECT c.id, c.affiliate_id, c.order_id, c.valor, c.status, c.referencia, c.created_at, c.updated_at
	           FROM senderzz_affiliate_commissions c
	           JOIN senderzz_affiliates a ON a.id = c.affiliate_id`

	if user.Role == "produtor" {
		if statusFilter != "" {
			rows, err = h.Pool.Query(r.Context(),
				base+` WHERE a.produtor_id=$1 AND c.status=$2 ORDER BY c.created_at DESC LIMIT $3`,
				user.ID, statusFilter, limit)
		} else {
			rows, err = h.Pool.Query(r.Context(),
				base+` WHERE a.produtor_id=$1 ORDER BY c.created_at DESC LIMIT $2`,
				user.ID, limit)
		}
	} else {
		// Afiliado: suas próprias comissões.
		if statusFilter != "" {
			rows, err = h.Pool.Query(r.Context(),
				base+` WHERE a.afiliado_id=$1 AND c.status=$2 ORDER BY c.created_at DESC LIMIT $3`,
				user.ID, statusFilter, limit)
		} else {
			rows, err = h.Pool.Query(r.Context(),
				base+` WHERE a.afiliado_id=$1 ORDER BY c.created_at DESC LIMIT $2`,
				user.ID, limit)
		}
	}
	if err != nil {
		slog.Error("[affiliates] falha ao listar comissões", "user_id", user.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao buscar comissões")
		return
	}
	defer rows.Close()

	type commissionRow struct {
		ID          int64           `json:"id"`
		AffiliateID int64           `json:"affiliate_id"`
		OrderID     int64           `json:"order_id"`
		Valor       decimal.Decimal `json:"valor"`
		Status      string          `json:"status"`
		Referencia  *string         `json:"referencia,omitempty"`
		CreatedAt   time.Time       `json:"created_at"`
		UpdatedAt   time.Time       `json:"updated_at"`
	}

	var result []commissionRow
	for rows.Next() {
		var row commissionRow
		var valorStr string
		if err := rows.Scan(
			&row.ID, &row.AffiliateID, &row.OrderID, &valorStr,
			&row.Status, &row.Referencia, &row.CreatedAt, &row.UpdatedAt,
		); err != nil {
			continue
		}
		row.Valor, _ = decimal.NewFromString(valorStr)
		result = append(result, row)
	}
	if result == nil {
		result = []commissionRow{}
	}

	httpx.WriteOK(w, map[string]any{"data": result, "total": len(result)})
}

// ── GET /affiliates/commissions/summary ──────────────────────────────────────

// CommissionsSummary — retorna totais de comissão agrupados por status.
func (h *AffiliatesHandler) CommissionsSummary(w http.ResponseWriter, r *http.Request) {
	user := auth.PortalUserFromCtx(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	var rows pgx.Rows
	var err error

	base := `SELECT c.status, COUNT(*) AS qtd, COALESCE(SUM(c.valor), 0) AS total
	           FROM senderzz_affiliate_commissions c
	           JOIN senderzz_affiliates a ON a.id = c.affiliate_id`

	if user.Role == "produtor" {
		rows, err = h.Pool.Query(r.Context(),
			base+` WHERE a.produtor_id=$1 GROUP BY c.status`, user.ID)
	} else {
		rows, err = h.Pool.Query(r.Context(),
			base+` WHERE a.afiliado_id=$1 GROUP BY c.status`, user.ID)
	}
	if err != nil {
		slog.Error("[affiliates] falha ao sumarizar comissões", "user_id", user.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao buscar resumo")
		return
	}
	defer rows.Close()

	summary := map[string]map[string]any{
		"pendente":  {"qtd": 0, "total": "0.00"},
		"aprovada":  {"qtd": 0, "total": "0.00"},
		"paga":      {"qtd": 0, "total": "0.00"},
		"estornada": {"qtd": 0, "total": "0.00"},
	}

	for rows.Next() {
		var status, totalStr string
		var qtd int
		if err := rows.Scan(&status, &qtd, &totalStr); err != nil {
			continue
		}
		total, _ := decimal.NewFromString(totalStr)
		summary[status] = map[string]any{"qtd": qtd, "total": total.StringFixed(2)}
	}

	httpx.WriteOK(w, map[string]any{"summary": summary})
}

// ── GET /affiliates/links ─────────────────────────────────────────────────────

// ListLinks — lista links de checkout ativos do afiliado autenticado.
func (h *AffiliatesHandler) ListLinks(w http.ResponseWriter, r *http.Request) {
	user := auth.PortalUserFromCtx(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	rows, err := h.Pool.Query(r.Context(), `
		SELECT l.id, l.affiliate_id, l.link_token, l.produto_id, l.active, l.clicks, l.created_at
		  FROM senderzz_affiliate_links l
		  JOIN senderzz_affiliates a ON a.id = l.affiliate_id
		 WHERE a.afiliado_id = $1
		 ORDER BY l.created_at DESC`,
		user.ID,
	)
	if err != nil {
		slog.Error("[affiliates] falha ao listar links", "user_id", user.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao buscar links")
		return
	}
	defer rows.Close()

	type linkRow struct {
		ID          int64     `json:"id"`
		AffiliateID int64     `json:"affiliate_id"`
		LinkToken   string    `json:"link_token"`
		ProdutoID   int64     `json:"produto_id"`
		Active      bool      `json:"active"`
		Clicks      int       `json:"clicks"`
		CreatedAt   time.Time `json:"created_at"`
	}

	var result []linkRow
	for rows.Next() {
		var row linkRow
		if err := rows.Scan(
			&row.ID, &row.AffiliateID, &row.LinkToken, &row.ProdutoID,
			&row.Active, &row.Clicks, &row.CreatedAt,
		); err != nil {
			continue
		}
		result = append(result, row)
	}
	if result == nil {
		result = []linkRow{}
	}

	httpx.WriteOK(w, map[string]any{"data": result, "total": len(result)})
}

// ── POST /affiliates/links ────────────────────────────────────────────────────

// CreateLink — afiliado cria um link de checkout rastreado para um produto.
// Body: {affiliate_id, produto_id}
// O link_token é gerado automaticamente (32 bytes hex = 64 chars).
func (h *AffiliatesHandler) CreateLink(w http.ResponseWriter, r *http.Request) {
	user := auth.PortalUserFromCtx(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	var req struct {
		AffiliateID int64 `json:"affiliate_id"`
		ProdutoID   int64 `json:"produto_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	if req.AffiliateID == 0 || req.ProdutoID == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "affiliate_id e produto_id são obrigatórios")
		return
	}

	ctx := r.Context()

	// Garante que o vínculo pertence ao afiliado autenticado e está ativo.
	var vinculoExists bool
	err := h.Pool.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM senderzz_affiliates
			 WHERE id=$1 AND afiliado_id=$2 AND status='active'
		)`,
		req.AffiliateID, user.ID,
	).Scan(&vinculoExists)
	if err != nil || !vinculoExists {
		httpx.WriteErr(w, http.StatusForbidden, "vínculo não encontrado ou inativo")
		return
	}

	token, err := randomHex(32) // 32 bytes = 64 chars hex
	if err != nil {
		slog.Error("[affiliates] falha ao gerar link_token", "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao gerar link")
		return
	}

	var id int64
	err = h.Pool.QueryRow(ctx, `
		INSERT INTO senderzz_affiliate_links (affiliate_id, link_token, produto_id)
		VALUES ($1, $2, $3)
		RETURNING id`,
		req.AffiliateID, token, req.ProdutoID,
	).Scan(&id)
	if err != nil {
		slog.Error("[affiliates] falha ao criar link", "affiliate_id", req.AffiliateID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao criar link")
		return
	}

	slog.Info("[affiliates] link criado", "link_id", id, "afiliado_id", user.ID)
	httpx.WriteOK(w, map[string]any{
		"link_id":    id,
		"link_token": token,
		"produto_id": req.ProdutoID,
	})
}

// ── DELETE /affiliates/links/{id} ─────────────────────────────────────────────

// DeactivateLink — desativa um link de checkout do afiliado autenticado.
func (h *AffiliatesHandler) DeactivateLink(w http.ResponseWriter, r *http.Request) {
	user := auth.PortalUserFromCtx(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}

	ctx := r.Context()

	// Garante que o link pertence ao afiliado autenticado.
	var linkExists bool
	err = h.Pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM senderzz_affiliate_links l
			  JOIN senderzz_affiliates a ON a.id = l.affiliate_id
			 WHERE l.id=$1 AND a.afiliado_id=$2
		)`,
		id, user.ID,
	).Scan(&linkExists)
	if err != nil || !linkExists {
		httpx.WriteErr(w, http.StatusNotFound, "link não encontrado ou sem permissão")
		return
	}

	_, err = h.Pool.Exec(ctx,
		`UPDATE senderzz_affiliate_links SET active=false WHERE id=$1`,
		id,
	)
	if err != nil {
		slog.Error("[affiliates] falha ao desativar link", "link_id", id, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao desativar link")
		return
	}

	slog.Info("[affiliates] link desativado", "link_id", id, "afiliado_id", user.ID)
	httpx.WriteOK(w, map[string]any{"link_id": id, "active": false})
}
