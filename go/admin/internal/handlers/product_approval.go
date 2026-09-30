// Handler da FILA DE APROVAÇÃO DE PRODUTO (sz_products).
//
// Espelha a aprovação de afiliado que já existe (go/portal affiliates_portal.go:
// pending → active), mas a APROVAÇÃO de produto é do ADMIN (não do produtor):
// produto novo nasce 'a_aprovar' (ver go/portal products.go Create + migração
// infra/postgres/424-produto-aprovacao.sql) e o admin aprova/reprova aqui.
//
// Rotas (sob /wp-json/senderzz/v1/admin/, auth=middleware admin iss=senderzz-admin):
//
//	GET  /products/approval-queue   → lista produtos pendentes (status='a_aprovar')
//	POST /products/{id}/approve     → status → 'active'
//	POST /products/{id}/reject      → status → 'reprovado'
//
// CONVENÇÃO DE STATUS (token minúsculo sem espaço — id-space da coluna):
//   - 'a_aprovar' → pendente (na fila)        ← produto novo nasce aqui
//   - 'active'    → aprovado pelo admin (AUDIT-2026-07-14: unificado com o
//                    status de produto criado direto — 'aprovado' era um
//                    token duplicado pro mesmo estado "vendável", exibia 2
//                    nomes diferentes pra mesma coisa no admin; 'aprovado'
//                    fica só como valor legado em linhas antigas não migradas)
//   - 'reprovado' → reprovado pelo admin
//
// CHAVE DE ATRIBUIÇÃO: sz_products.produtor_id = senderzz_portal_users.id (portal
// id) — LEFT JOIN pu.id = sp.produtor_id, idêntico ao products.go. NÃO derivar
// nova chave (classe de bug de nomes vazios / cross-attribution).
package handlers

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

// ProductApprovalHandler expõe a fila de aprovação de produto.
type ProductApprovalHandler struct{ Pool *pgxpool.Pool }

// tableExists — checagem genérica para qualquer tabela public.<name>
// (degradação graciosa durante a janela de migração, como products.go).
func (h *ProductApprovalHandler) tableExists(ctx context.Context, name string) bool {
	return tableExistsCached(ctx, h.Pool, name) // AUDIT-2026-06-18 Onda2 (go-infoschema-cache)
}

// pendingProduct — uma linha da fila de aprovação.
//
// Carrega TODAS as informações inseridas pelo produtor (nome, sku, barcode, categoria,
// descrição, dimensões físicas) para o admin avaliar no drawer de detalhe sem
// um segundo round-trip. As dimensões são *float64 (NULL quando o produtor não
// informou — não inventamos 0), idênticas ao products.go List (migração v469).
// A imagem/rótulo, se houver, vive em sz_products.meta (jsonb) — não há coluna
// dedicada; expomos best-effort via imagem_url (primeira chave de imagem não vazia).
type pendingProduct struct {
	ID           int64    `json:"id"`
	WPPostID     *int64   `json:"wp_post_id"`
	ProdutorID   int64    `json:"produtor_id"`
	ProdutorNome string   `json:"produtor_nome"`
	Nome         string   `json:"nome"`
	SKU          *string  `json:"sku"`
	Barcode      *string  `json:"barcode"`
	Categoria    *string  `json:"categoria"`
	Descricao    *string  `json:"descricao"`
	Altura       *float64 `json:"altura"`      // cm
	Largura      *float64 `json:"largura"`     // cm
	Comprimento  *float64 `json:"comprimento"` // cm
	Peso         *float64 `json:"peso"`        // kg
	ImagemURL    *string  `json:"imagem_url"`  // best-effort de sz_products.meta (jsonb)
	Status       string   `json:"status"`
	CreatedAt    string   `json:"created_at"`
}

// ApprovalQueue — GET /products/approval-queue
//
// Lista os produtos pendentes de aprovação (status='a_aprovar') com produtor /
// nome / sku. Degrada a lista vazia se a tabela ainda não existir (migração
// pendente). Filtra os "não-produtos" (recarga / frete interno / carteira de
// frete) espelhando products.go.
func (h *ProductApprovalHandler) ApprovalQueue(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_products") {
		httpx.JSON(w, 200, map[string]any{"items": []pendingProduct{}, "total": int64(0)})
		return
	}

	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	search := strings.TrimSpace(q.Get("q"))

	rows, err := h.Pool.Query(ctx,
		`SELECT
		    sp.id,
		    sp.wp_post_id,
		    sp.produtor_id,
		    COALESCE(pu.nome, pu.email, '') AS produtor_nome,
		    sp.nome,
		    sp.sku,
		    sp.barcode,
		    sp.categoria,
		    sp.descricao,
		    -- dimensões físicas (NULLable → *float8 no scan; não usamos COALESCE p/ não inventar 0)
		    sp.altura::float8,
		    sp.largura::float8,
		    sp.comprimento::float8,
		    sp.peso::float8,
		    -- imagem/rótulo best-effort: primeira chave de imagem não vazia em meta (jsonb).
		    -- meta pode ser NULL; o COALESCE de chaves devolve NULL quando nenhuma existe.
		    -- Conjunto de chaves alinhado a go/orders checkout.go resolveProductImage
		    -- (image_url/thumb_url/thumbnail/imagem/image), + imagem_url/label_url extras.
		    COALESCE(
		        NULLIF(sp.meta->>'image_url', ''),
		        NULLIF(sp.meta->>'thumb_url', ''),
		        NULLIF(sp.meta->>'thumbnail', ''),
		        NULLIF(sp.meta->>'imagem', ''),
		        NULLIF(sp.meta->>'image', ''),
		        NULLIF(sp.meta->>'imagem_url', ''),
		        NULLIF(sp.meta->>'label_url', '')
		    ) AS imagem_url,
		    sp.status,
		    sp.created_at::text
		 FROM sz_products sp
		 LEFT JOIN senderzz_portal_users pu ON pu.id = sp.produtor_id
		 WHERE sp.status = 'a_aprovar'
		   AND ($1 = '' OR sp.nome ILIKE '%' || $1 || '%')
		   AND sp.nome NOT ILIKE '%recarga%'
		   AND sp.nome NOT ILIKE '%frete interno%'
		   AND sp.nome NOT ILIKE '%carteira de frete%'
		 ORDER BY sp.created_at ASC, sp.id ASC
		 LIMIT $2 OFFSET $3`,
		search, limit, offset)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	items := []pendingProduct{}
	for rows.Next() {
		var p pendingProduct
		if err := rows.Scan(
			&p.ID, &p.WPPostID, &p.ProdutorID, &p.ProdutorNome,
			&p.Nome, &p.SKU, &p.Barcode, &p.Categoria, &p.Descricao,
			&p.Altura, &p.Largura, &p.Comprimento, &p.Peso, &p.ImagemURL,
			&p.Status, &p.CreatedAt,
		); err != nil {
			httpx.Err(w, 500, "scan_error", err.Error())
			return
		}
		items = append(items, p)
	}

	var total int64
	_ = h.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM sz_products sp
		 WHERE sp.status = 'a_aprovar'
		   AND ($1 = '' OR sp.nome ILIKE '%' || $1 || '%')
		   AND sp.nome NOT ILIKE '%recarga%'
		   AND sp.nome NOT ILIKE '%frete interno%'
		   AND sp.nome NOT ILIKE '%carteira de frete%'`,
		search).Scan(&total)

	httpx.JSON(w, 200, map[string]any{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// transition aplica a mudança de status de um produto pendente (approve/reject).
// Só age sobre linhas que estão 'a_aprovar' (evita re-aprovar/reprovar algo já
// decidido ou mexer em produto ativo legado). Idempotência defensiva via
// RowsAffected==0 → 404 (já saiu da fila).
func (h *ProductApprovalHandler) transition(w http.ResponseWriter, r *http.Request, newStatus, okMsg string, promoteOwner bool) {
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_products") {
		httpx.Err(w, 503, "table_not_found", "tabela sz_products não existe — execute a migração 424-produto-aprovacao.sql")
		return
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}

	// RETURNING produtor_id (dono do produto = portal user id) p/ a promoção de papel.
	var ownerID int64
	err = h.Pool.QueryRow(ctx,
		`UPDATE sz_products
		    SET status = $1, updated_at = NOW()
		  WHERE id = $2 AND status = 'a_aprovar'
		 RETURNING COALESCE(produtor_id, 0)`,
		newStatus, id).Scan(&ownerID)
	if err == pgx.ErrNoRows {
		httpx.Err(w, 404, "not_found", "produto não encontrado na fila de aprovação")
		return
	}
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// FEAT cliente/afiliado→produtor: ao APROVAR o 1º produto próprio, o dono vira
	// produtor (promoção de papel). Só sobe quem é cliente/afiliado — nunca rebaixa
	// admin/operator nem mexe em quem já é produtor. Idempotente (RowsAffected 0 = já era).
	promoted := false
	if promoteOwner && ownerID > 0 && h.tableExists(ctx, "senderzz_portal_users") {
		pr, e := h.Pool.Exec(ctx,
			`UPDATE senderzz_portal_users
			    SET role = 'produtor'
			  WHERE id = $1 AND role IN ('cliente','afiliado')`,
			ownerID)
		if e == nil && pr.RowsAffected() > 0 {
			promoted = true
		}
	}

	httpx.JSON(w, 200, map[string]any{
		"ok": true, "id": id, "status": newStatus, "mensagem": okMsg,
		"owner_id": ownerID, "promoted_to_produtor": promoted,
	})
}

// Approve — POST /products/{id}/approve  (status → 'active')
// Aprovar PROMOVE o dono (cliente/afiliado → produtor) — pedido do dono 2026-06-26.
func (h *ProductApprovalHandler) Approve(w http.ResponseWriter, r *http.Request) {
	h.transition(w, r, "active", "Produto aprovado.", true)
}

// Reject — POST /products/{id}/reject  (status → 'reprovado')
func (h *ProductApprovalHandler) Reject(w http.ResponseWriter, r *http.Request) {
	h.transition(w, r, "reprovado", "Produto reprovado.", false)
}
