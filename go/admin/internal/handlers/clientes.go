// Handler de CLIENTES (senderzz_portal_users role='cliente').
//
// FEAT cliente/afiliado→produtor: o admin precisa ver os clientes e, ao cadastrar
// um produto para um deles, esse produto entra na fila de aprovação (a_aprovar) e,
// quando aprovado, o dono é PROMOVIDO a produtor (ver product_approval.go — NÃO
// alterado por este handler).
//
// Rotas (sob /wp-json/senderzz/v1/admin/, auth=middleware admin iss=senderzz-admin):
//
//	GET /clientes?q=&role=  → lista usuários do portal por papel + produtos_count
//
// O parâmetro `role` reaproveita este endpoint como FONTE DE DONOS para o seletor
// de produto do admin (Products.tsx):
//   - ""        → role='cliente' (default, a aba Clientes)
//   - "cliente" → idem
//   - "afiliado"/"produtor" → o papel exato
//   - "all"     → cliente + afiliado + produtor (qualquer dono elegível de produto)
//
// CHAVE: sz_products.produtor_id = senderzz_portal_users.id (portal id) — idêntico
// a producers.go / product_approval.go. produtos_count = COUNT sz_products do dono.
package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/auth"
	"github.com/senderzz/admin-service/internal/httpx"
)

// ClientesHandler expõe a lista de clientes (e, via ?role=, demais papéis donos).
type ClientesHandler struct{ Pool *pgxpool.Pool }

// cliente — uma linha da lista. Mesma estética dos demais usuários do admin.
type cliente struct {
	ID            int64  `json:"id"` // portal_users.id (= sz_products.produtor_id)
	Nome          string `json:"nome"`
	Email         string `json:"email"`
	Telefone      string `json:"telefone"`
	CPF           string `json:"cpf"`
	Role          string `json:"role"`
	CreatedAt     string `json:"created_at"`
	ProdutosCount int64  `json:"produtos_count"`
}

// List — GET /clientes?q=&role=
func (h *ClientesHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	search := strings.TrimSpace(q.Get("q"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 300 {
		limit = 200
	}
	offset, _ := strconv.Atoi(q.Get("offset"))

	// Mapeia ?role= → conjunto de papéis (ANY($1) cobre 1 ou N sem SQL dinâmico).
	var roles []string
	switch strings.TrimSpace(q.Get("role")) {
	case "all":
		roles = []string{"cliente", "afiliado", "produtor"}
	case "afiliado":
		roles = []string{"afiliado"}
	case "produtor":
		roles = []string{"produtor"}
	default: // "" ou "cliente"
		roles = []string{"cliente"}
	}

	rows, err := h.Pool.Query(r.Context(),
		`SELECT
		    u.id,
		    COALESCE(u.nome,'')  AS nome,
		    COALESCE(u.email,'') AS email,
		    -- AUDIT-2026-07-31 (dono: "clientes sem dados essenciais, só aparecem
		    -- depois de aprovados") — telefone/CPF nunca eram buscados aqui, só em
		    -- producers.go (role='produtor'). Mesmo fallback (meta portal → coluna
		    -- direta u.phone/u.document), sem depender de aprovação/promoção.
		    COALESCE(
		        NULLIF((SELECT meta_value FROM senderzz_portal_user_meta
		                 WHERE user_id = u.id AND meta_key = '_billing_phone' LIMIT 1), ''),
		        NULLIF(u.phone, ''),
		        ''
		    ) AS telefone,
		    COALESCE(
		        NULLIF((SELECT meta_value FROM senderzz_portal_user_meta
		                 WHERE user_id = u.id AND meta_key = '_billing_cpf' LIMIT 1), ''),
		        NULLIF(u.document, ''),
		        ''
		    ) AS cpf,
		    COALESCE(u.role,'')  AS role,
		    u.created_at::text   AS created_at,
		    -- Produtos do dono: sz_products.produtor_id = portal id (u.id), idêntico a producers.go.
		    COALESCE((
		        SELECT COUNT(*) FROM sz_products sp WHERE sp.produtor_id = u.id
		    ), 0) AS produtos_count
		 FROM senderzz_portal_users u
		 WHERE u.role::text = ANY($1)
		   AND u.ativo
		   AND ($2 = '' OR u.email ILIKE '%' || $2 || '%' OR u.nome ILIKE '%' || $2 || '%')
		 ORDER BY u.created_at DESC
		 LIMIT $3 OFFSET $4`, roles, search, limit, offset)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []cliente{}
	for rows.Next() {
		var c cliente
		if err := rows.Scan(&c.ID, &c.Nome, &c.Email, &c.Telefone, &c.CPF, &c.Role, &c.CreatedAt, &c.ProdutosCount); err != nil {
			httpx.Err(w, 500, "scan_error", err.Error())
			return
		}
		out = append(out, c)
	}

	var total int64
	_ = h.Pool.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM senderzz_portal_users
		 WHERE role::text = ANY($1)
		   AND ativo
		   AND ($2 = '' OR email ILIKE '%' || $2 || '%' OR nome ILIKE '%' || $2 || '%')`,
		roles, search).Scan(&total)

	httpx.JSON(w, 200, map[string]any{"items": out, "total": total})
}

// ─── DELETE /clientes/{id} ───────────────────────────────────────────────────
// SOFT-DELETE (convenção do projeto — espelha ProducersHandler.Delete). Atende a
// MESMA opção de exclusão do dono para CLIENTES e AFILIADOS: ambos são linhas de
// senderzz_portal_users e a lista de afiliados (affiliates.go: `u.id AS user_id`,
// `FROM senderzz_portal_users u`) devolve EXATAMENTE este id-space (portal id) —
// idêntico ao /clientes — então um único endpoint cobre os dois papéis.
//
// GATE DE PAPEL: `role IN ('cliente','afiliado')`. Produtor/admin/operator NUNCA
// são excluídos por esta rota (produtor tem o próprio fluxo em producers.go; admin/
// operator não têm exclusão por aqui). Se o id existir mas o papel não casar →
// RowsAffected=0 → 404 (segurança que o dono pediu; o err-toast no front é o
// comportamento correto, sem caso especial).
//
// HISTÓRICO FINANCEIRO PRESERVADO: NÃO tocamos senderzz_affiliate_transactions
// (ledger) — só desativamos a linha (ativo=false): some da listagem (List filtra
// `AND u.ativo`), perde acesso, e é REVERSÍVEL.
//
// CASCADE (igual ao produtor): hard-delete dos checkouts vinculados
// (DELETE FROM senderzz_checkout_links WHERE producer_id = id). producer_id é o
// portal_users.id — MESMO id-space do {id} aqui. Tudo numa transação: desativamos
// PRIMEIRO e só seguimos para o cascade se a linha existir (RowsAffected>0), senão
// rollback + 404 (não apagamos checkouts de um id inválido / de outro papel).
func (h *ClientesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	ctx := r.Context()

	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op após Commit

	// 1) Soft-delete do cliente/afiliado (ativo=false). Gate de papel + existência/404.
	ct, err := tx.Exec(ctx,
		`UPDATE senderzz_portal_users SET ativo = false
		  WHERE id = $1 AND role IN ('cliente','afiliado')`, id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if ct.RowsAffected() == 0 {
		httpx.Err(w, 404, "not_found", "cliente/afiliado não encontrado")
		return
	}

	// 2) Cascade: hard-delete dos checkouts vinculados (producer_id = portal id).
	delCt, err := tx.Exec(ctx,
		`DELETE FROM senderzz_checkout_links WHERE producer_id = $1`, id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// Trilha de accountability (best-effort, não bloqueia).
	if actor := auth.FromCtx(ctx); actor != nil {
		logPIIAccess(ctx, h.Pool, actor.ID, actor.Email, "cliente", id,
			[]string{"ativo"}, "delete", r.RemoteAddr)
	}
	httpx.JSON(w, 200, map[string]any{
		"ok":                true,
		"user_id":           id,
		"checkouts_deleted": delCt.RowsAffected(),
	})
}
