// Package handlers — handler de Estoque (custódia física) do Portal V2.
//
// Rotas (namespace /wp-json/senderzz/v1):
//
//	GET /portal/stock — KPIs de custódia + grade de produtos + movimentações
//
// Espelha templates/portal/v2/sections/stock.php — porta a tela "Estoque" do
// portal para o backend Go mantendo UX/UI e REGRAS idênticas ao WP. A tela é
// READ-ONLY (o formulário de "Envio de reposição" do .php já é um placeholder
// "Em breve" — não há rota de escrita aqui).
//
// ── ESCOPO POR ROLE (canonical id-space — NUNCA improvisar) ───────────────────
// Idêntico a ProductsHandler (a tela de Estoque renderiza os MESMOS produtos do
// usuário com os MESMOS 5 KPIs de custódia — as duas telas precisam concordar):
//   - produtor : vê os SEUS produtos       → sp.produtor_id = u.ID  (portal id)
//   - afiliado : produtos a que está vinculado e aprovado →
//     senderzz_affiliates a JOIN onde a.afiliado_id = u.WPUserID  AND a.status='active'
//     (NUNCA IN(u.id,u.wp_user_id)). produto casa por a.produto_id = COALESCE(sp.wp_post_id, sp.id).
//   - operator : sem produtos próprios      → lista vazia (degradação graciosa).
//
// SEGURANÇA: por construção, afiliado NUNCA recebe dados do produtor. As queries
// são escopadas pelo vínculo do próprio afiliado (a.afiliado_id = u.WPUserID) e os
// KPIs de custódia são zerados (ver abaixo) — não existe número de custódia do
// produtor que possa vazar para o afiliado via esta API.
//
// ── KPIs FÍSICOS (Disponíveis/Reservados/Em rota/Entregues/Frustrados) ────────
// No WP esses números vêm de wp_sz_motoboy_stock_custody (ou do fallback
// Portal_Orders + kit_map). Essa tabela NÃO existe no Postgres migrado e
// sz_products NÃO possui coluna de quantidade de estoque — auditado contra
// infra/postgres/*.sql (grep por stock/custody/movement = 0 resultados).
// Portanto os 5 KPIs retornam 0 (degradação graciosa), EXATAMENTE como o
// ProductsHandler já decidiu (ver products.go header). Derivar esses números por
// um caminho diferente (sz_order_items → sz_motoboy_pedidos com expansão de kit)
// faria as duas telas mostrarem estoques divergentes para o mesmo produto — bug,
// não feature. A UI deve renderizar os cards normalmente com 0.
//
// ── MOVIMENTAÇÕES ─────────────────────────────────────────────────────────────
// No WP vêm de senderzz_stock_get_movement_rows (ledger de entradas/saídas). Não
// há tabela de movimentações no espelho Postgres → lista vazia (o front cai no
// empty-state "Nenhuma movimentação registrada"). A exportação CSV do .php é
// 100% client-side — não há rota de servidor para ela.
package handlers

import (
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/portal-service/internal/auth"
	"github.com/senderzz/portal-service/internal/httpx"
)

// StockHandler agrupa as dependências do handler de estoque.
// Construção idêntica a ProductsHandler/WebhookHandler — o integrador monta com
// &handlers.StockHandler{Pool: pool}.
type StockHandler struct {
	Pool *pgxpool.Pool
}

// ── Shapes de resposta ────────────────────────────────────────────────────────

// stockKPIs espelha os 5 cards de custódia física do topo da section.
// Zerados enquanto não houver tabela de custódia no Postgres (mesma degradação
// graciosa do ProductsHandler). Mantidos como agregados globais do usuário.
type stockKPIs struct {
	Available  int64 `json:"available"`  // Disponíveis (soma das quantidades em estoque)
	Reserved   int64 `json:"reserved"`   // Reservados (aguardando envio)
	Route      int64 `json:"route"`      // Em rota (com motoboy)
	Delivered  int64 `json:"delivered"`  // Entregues (confirmados)
	Frustrated int64 `json:"frustrated"` // Frustrados (retorno/frustração)
}

// stockItem é um produto na grade de estoque, espelhando as colunas da tabela
// "Produtos em estoque" da section: nome, SKU, status e os 4 valores por produto
// (Disponível/Reservado/Em rota/Frustrado) + Total.
type stockItem struct {
	ID          int64  `json:"id"`          // sz_products.id
	WPPostID    *int64 `json:"wp_post_id"`  // ID original WP (pode ser NULL)
	Name        string `json:"name"`        // sz_products.nome
	SKU         string `json:"sku"`         // "" quando ausente (UI mostra "—")
	StatusLabel string `json:"status_label"` // Disponível | Sob encomenda | Sem estoque
	StatusClass string `json:"status_class"` // success | warning | danger (mirror do .php)
	// Quantidades por produto — zeradas sem a tabela de custódia (ver header).
	Available  int64 `json:"available"`
	Reserved   int64 `json:"reserved"`
	Route      int64 `json:"route"`
	Frustrated int64 `json:"frustrated"`
	Total      int64 `json:"total"`
}

// stockMovement é uma linha do histórico de movimentações.
// Campos espelham as colunas/data-attrs da tabela de movimentações do .php.
// Sem ledger no Postgres a lista vem vazia — o tipo existe para o contrato do JSON.
type stockMovement struct {
	DateFmt        string `json:"date_fmt"`
	Direction      string `json:"direction"`       // in | out
	DirectionLabel string `json:"direction_label"` // Entrada | Saída
	SourceLabel    string `json:"source_label"`
	SourceMeta     string `json:"source_meta"`
	ProductName    string `json:"product_name"`
	Qty            int64  `json:"qty"`
	StockBefore    *int64 `json:"stock_before"`
	StockAfter     *int64 `json:"stock_after"`
	Status         string `json:"status"`
}

// ── GET /portal/stock ─────────────────────────────────────────────────────────

// List monta a tela de Estoque do usuário autenticado: KPIs agregados, a grade
// de produtos (escopada por role) e as movimentações.
//
// Envelope:
//
//	{ ok:true, data:{ kpis:{...}, products:[...], movements:[...] },
//	  total:N, role:"produtor", is_affiliate:false }
//
// Mantém o contrato de degradação graciosa do portal: tabela ausente (42P01) ou
// role sem escopo → grade vazia + KPIs/movimentações zerados (HTTP 200), nunca 500.
func (h *StockHandler) List(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	isAff := u.Role == "afiliado" || u.Role == "affiliate" || u.Role == "afiliada" || u.Role == "cliente"

	// operator não tem produtos próprios → grade vazia (degradação graciosa).
	// Movimentações e KPIs também ficam zerados — espelha o "Estoque indisponível"
	// do .php para quem não tem classe/produtos.
	if u.Role == "operator" {
		h.writeEmpty(w, u.Role, false)
		return
	}

	var (
		rows pgx.Rows
		err  error
	)

	if isAff {
		// AFILIADO: produtos vinculados e aprovados. Canonical join:
		// a.afiliado_id = u.WPUserID (NUNCA IN(u.id,u.wp_user_id)), status='active'.
		// produto casa por a.produto_id = COALESCE(sp.wp_post_id, sp.id).
		// Mesmo filtro de "não-produtos" e mesmo set do ProductsHandler — as duas
		// telas TÊM que mostrar exatamente os mesmos produtos.
		rows, err = h.Pool.Query(r.Context(),
			`SELECT DISTINCT
			        sp.id, sp.wp_post_id, sp.nome, sp.sku, sp.status
			   FROM sz_products sp
			   JOIN senderzz_affiliates a
			     ON a.produto_id = COALESCE(sp.wp_post_id, sp.id)
			    AND a.afiliado_id = $1
			    AND a.status = 'active'
			  WHERE sp.status IS DISTINCT FROM 'deleted'
			    AND sp.nome NOT ILIKE '%recarga%'
			    AND sp.nome NOT ILIKE '%frete interno%'
			    AND sp.nome NOT ILIKE '%carteira de frete%'
			  ORDER BY sp.nome ASC`,
			u.WPUserID,
		)
	} else {
		// PRODUTOR: vê os SEUS produtos. sz_products.produtor_id é portal_users.id.
		rows, err = h.Pool.Query(r.Context(),
			`SELECT sp.id, sp.wp_post_id, sp.nome, sp.sku, sp.status
			   FROM sz_products sp
			  WHERE sp.produtor_id = $1
			    AND sp.status IS DISTINCT FROM 'deleted'
			    AND sp.nome NOT ILIKE '%recarga%'
			    AND sp.nome NOT ILIKE '%frete interno%'
			    AND sp.nome NOT ILIKE '%carteira de frete%'
			  ORDER BY sp.nome ASC`,
			u.ID,
		)
	}
	if err != nil {
		// Tabela ausente (sz_products / senderzz_affiliates) → degradação graciosa.
		if isUndefinedTable(err) {
			h.writeEmpty(w, u.Role, isAff)
			return
		}
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer rows.Close()

	items := []stockItem{}
	for rows.Next() {
		var (
			it     stockItem
			sku    *string
			status string
		)
		if err := rows.Scan(&it.ID, &it.WPPostID, &it.Name, &sku, &status); err != nil {
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao ler produtos")
			return
		}
		if sku != nil {
			it.SKU = *sku
		}

		// Status de estoque. Sem coluna de quantidade no espelho PG, o produto
		// "ativo" é tratado como Disponível (mirror do instock → success do .php);
		// produtos não-ativos caem em "Sem estoque" (danger). Quantidades = 0
		// (ver header: KPIs de custódia zerados, consistentes com ProductsHandler).
		if status == "active" {
			it.StatusLabel = "Disponível"
			it.StatusClass = "success"
		} else {
			it.StatusLabel = "Sem estoque"
			it.StatusClass = "danger"
		}

		// KPIs por produto zerados (sem tabela de custódia). Total = soma deles.
		it.Available = 0
		it.Reserved = 0
		it.Route = 0
		it.Frustrated = 0
		it.Total = it.Available + it.Reserved + it.Route + it.Frustrated

		items = append(items, it)
	}
	if rows.Err() != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao processar produtos")
		return
	}

	// KPIs agregados = soma dos por-produto (todos 0 enquanto não houver custódia).
	kpis := stockKPIs{}
	for i := range items {
		kpis.Available += items[i].Available
		kpis.Reserved += items[i].Reserved
		kpis.Route += items[i].Route
		kpis.Delivered += 0 // "Entregues" não é por-produto na grade; agregado apenas.
		kpis.Frustrated += items[i].Frustrated
	}

	// Movimentações: sem ledger no Postgres → lista vazia (empty-state no front).
	movements := []stockMovement{}

	httpx.WriteOK(w, map[string]any{
		"data": map[string]any{
			"kpis":      kpis,
			"products":  items,
			"movements": movements,
		},
		"total":        len(items),
		"role":         u.Role,
		"is_affiliate": isAff,
	})
}

// writeEmpty escreve o envelope vazio padrão (grade/movimentações vazias, KPIs
// zerados) com HTTP 200 — usado em operator e nas degradações graciosas.
func (h *StockHandler) writeEmpty(w http.ResponseWriter, role string, isAff bool) {
	httpx.WriteOK(w, map[string]any{
		"data": map[string]any{
			"kpis":      stockKPIs{},
			"products":  []stockItem{},
			"movements": []stockMovement{},
		},
		"total":        0,
		"role":         role,
		"is_affiliate": isAff,
	})
}
