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

// inviteTTL é o prazo de validade de um convite de afiliação: 7 dias.
// Extraído como constante de pacote (sem mudança de comportamento — antes era o
// literal inline `7 * 24 * time.Hour` em CreateInvite) para travar o prazo contra
// regressão acidental via teste (TestInviteTTLCanonico), no mesmo espírito de
// takePctAfiliado. ListInvites/RevokeInvite continuam usando o critério
// expires_at > NOW() no SQL — esta constante define a janela na criação.
const inviteTTL = 7 * 24 * time.Hour

// SEC-AFFILIATES: o REDEEM canônico de convite (resgate → cria vínculo em
// senderzz_affiliates) vive NO PORTAL (PHP), não neste serviço. Decisão e justificativa:
//
//  1. A tabela senderzz_affiliate_invites (070-affiliates.sql) NÃO tem coluna produto_id,
//     mas senderzz_affiliates exige produto_id NOT NULL e o próprio Request() rejeita
//     produto_id == 0. Logo, transformar um convite em vínculo exige resolver o produto
//     a partir de regras de negócio que pertencem ao portal (produto/oferta do produtor)
//     — não há mapeamento convite→produto neste schema. Inserir produto_id=0 criaria uma
//     linha que viola o invariante do sistema (algo que nenhum Request() consegue gerar).
//  2. Durante o strangler-fig, manter UM único write-path canônico para o redeem é a
//     escolha defensiva: duplicar a lógica de uso-único aqui arriscaria divergência
//     (anti-auto-afiliação, escolha de produto, criação de conta) com o PHP.
//  3. go/affiliates permanece dono de CreateInvite/ListInvites/RevokeInvite (token, prazo,
//     ownership) — apenas o resgate fica no portal. Quando o cutover mover o redeem para Go,
//     a forma consistente é: UPDATE senderzz_affiliate_invites SET used_at=NOW()
//     WHERE token=$1 AND used_at IS NULL AND expires_at>NOW() RETURNING produtor_id (uso
//     único atômico), barrar auto-afiliação (produtor_id != afiliado), e
//     INSERT senderzz_affiliates ... WHERE NOT EXISTS — porém só após a tabela ganhar
//     produto_id (ou uma regra explícita de resolução de produto). Até lá, NÃO adicionamos
//     redeem aqui para não gravar vínculo inconsistente.

// SEC-AFFILIATES: valida que um percentual de comissão está no intervalo [0,100].
// O schema usa DECIMAL(5,2) SEM CHECK (aceitaria 999.99) e Approve() gravava o pct
// informado pelo cliente sem qualquer limite — esta guarda fecha esse vão (CWE-20).
// Retorna false quando o valor é negativo ou maior que 100.
func comissaoPctValida(pct decimal.Decimal) bool {
	return pct.GreaterThanOrEqual(decimal.Zero) &&
		pct.LessThanOrEqual(decimal.NewFromInt(100))
}

// takePctAfiliado é o "take" canônico da Senderzz sobre a comissão BRUTA do afiliado:
// 4,99% (ver project_commission_formula / senderzz_revenue). O valor canônico é
// originado e assinado upstream (PHP + triggers Postgres) — aqui ele serve apenas como
// constante de referência para travar a fórmula contra regressão acidental.
var takePctAfiliado = decimal.RequireFromString("4.99")

// comissaoLiquida calcula a comissão líquida do afiliado = bruta − (bruta × 4,99%),
// arredondada a 2 casas (mesma precisão de senderzz_affiliate_commissions.valor).
//
// SEC-AFFILIATES: NÃO é o caminho de produção (a comissão chega pronta via double-write,
// ver parseCommissionValor em internal.go) — é um pino de regressão da TAXA canônica de
// 4,99%. Se alguém alterar a alíquota sem intenção, o teste TestComissaoLiquida quebra.
func comissaoLiquida(bruta decimal.Decimal) decimal.Decimal {
	take := bruta.Mul(takePctAfiliado).Div(decimal.NewFromInt(100))
	return bruta.Sub(take).Round(2)
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

	// SEC-IDOR-affiliate-list-endpoints: escopo SEMPRE pelo dono da sessão (user.ID,
	// extraído do JWT/sessão), nunca por id arbitrário do path/query. Não aceitamos
	// produtor_id/afiliado_id do cliente. O branch por role apenas escolhe a COLUNA
	// de propriedade (produtor_id OU afiliado_id) — ambas comparadas a user.ID; trocar
	// de role só alterna entre "linhas onde sou produtor" e "linhas onde sou afiliado",
	// ambas legítimas do próprio usuário. NUNCA fazer OR-join (regra de id-space do
	// CLAUDE.md): afiliado=wp_user_id, produtor=wp_user_id nesta tabela (070-affiliates.sql).
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
		// SEC-AFFILIATES: valida o intervalo 0..100 antes de gravar — o schema não
		// tem CHECK e este era o único ponto onde o cliente injeta o pct.
		if !comissaoPctValida(*req.ComissaoPct) {
			httpx.WriteErr(w, http.StatusBadRequest, "comissao_pct deve estar entre 0 e 100")
			return
		}
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

	// SEC-IDOR-affiliate-list-endpoints: filtra exclusivamente por produtor_id=user.ID
	// (dono da sessão). Sem parâmetro de produtor no path/query → não há como listar
	// convites de outro produtor.
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

	expiresAt := time.Now().UTC().Add(inviteTTL)

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

	// SEC-IDOR-affiliate-list-endpoints: o JOIN nunca afrouxa o escopo — o WHERE sempre
	// fixa a.produtor_id=user.ID (produtor) OU a.afiliado_id=user.ID (afiliado), ambos
	// o dono da sessão. Nenhum id de produtor/afiliado é aceito do path/query.
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

	// SEC-IDOR-affiliate-list-endpoints: resumo agregado também escopado por user.ID
	// da sessão (produtor_id OU afiliado_id) — sem aceitar id de terceiro do cliente.
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

	// SEC-IDOR-affiliate-list-endpoints: o JOIN com senderzz_affiliates fixa
	// a.afiliado_id=user.ID (dono da sessão), impedindo listar links de outro afiliado/
	// produtor. Nenhum affiliate_id é aceito do path/query nesta listagem.
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
