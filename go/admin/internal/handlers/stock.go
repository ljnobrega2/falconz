// Handler de ESTOQUE (sz_stock + sz_stock_movements). FEAT-STOCK
//
// GET  /stock?search=&low=&page=&per_page=
// POST /stock                  → UPSERT (entrada) por (product_id,variation_id,cd_id)
// POST /stock/{id}/ajuste      → ajuste manual (delta +/- com motivo obrigatório)
// GET  /stock/{id}/movements?limit=50
//
// Regras:
//   - qty_sellable = qty_available - qty_reserved (calculado na query; não é coluna)
//   - low=1 → só linhas onde qty_available - qty_reserved <= low_stock_threshold
//   - search casa product_name via ILIKE (nome vem de sz_products por wp_post_id)
//   - toda entrada/ajuste grava uma linha em sz_stock_movements (auditoria)
//
// IMPORTANTE — sz_stock_movements NÃO tem coluna stock_id. O ledger é chaveado por
// (product_id, variation_id, cd_id) + order_id. Para listar as movimentações de
// uma linha de estoque ({id}), resolvemos a tripla via sz_stock e filtramos o ledger
// por ela. A reserva/liberação/commit são gravadas pelo TRIGGER do banco — este
// handler só emite 'entrada' e 'ajuste' (NÃO tocar nos demais tipos).
//
// Degradação graciosa: se sz_stock não existir, List devolve lista vazia e os
// POST devolvem 503 (mesmo padrão de products.go).
package handlers

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/auth"
	"github.com/senderzz/admin-service/internal/httpx"
)

// StockHandler expõe a gestão de estoque por (produto, variação, CD). FEAT-STOCK
type StockHandler struct{ Pool *pgxpool.Pool }

// StockItem é o shape de uma linha de estoque retornada pela API.
//
// QtySellable é calculado (available - reserved) — não é coluna física.
// ProductName e CdNome vêm de subselects (sz_products / sz_motoboy_cds) com
// fallback ('#'||product_id / 'CD '||cd_id) quando não há vínculo correspondente.
type StockItem struct {
	ID                int64  `json:"id"`
	ProductID         int64  `json:"product_id"`
	VariationID       int64  `json:"variation_id"`
	ProductName       string `json:"product_name"`
	CdID              int64  `json:"cd_id"`
	CdNome            string `json:"cd_nome"`
	QtyAvailable      int64  `json:"qty_available"`
	QtyReserved       int64  `json:"qty_reserved"`
	QtySellable       int64  `json:"qty_sellable"`
	LowStockThreshold int64  `json:"low_stock_threshold"`
	UpdatedAt         string `json:"updated_at"`
}

// StockMovement é uma linha do livro de movimentações de estoque.
// OrderID é NULL para ajuste/entrada manual → *int64 no scan.
type StockMovement struct {
	ID             int64   `json:"id"`
	Tipo           string  `json:"tipo"`
	DeltaAvailable int64   `json:"delta_available"`
	DeltaReserved  int64   `json:"delta_reserved"`
	OrderID        *int64  `json:"order_id"`
	Motivo         *string `json:"motivo"`
	CreatedAt      string  `json:"created_at"`
}

// tableExists — checagem genérica para public.<name> (cache de processo).
func (h *StockHandler) tableExists(ctx context.Context, name string) bool {
	return tableExistsCached(ctx, h.Pool, name) // AUDIT-2026-06-18 Onda2 (go-infoschema-cache)
}

// stockClientIP extrai o IP de origem (r.RemoteAddr já normalizado pelo RealIP),
// removendo a porta defensivamente. Usado no rastro de auditoria do ajuste manual.
// AUDIT STOCK-no-adjustment-audit
func stockClientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// stockProductNameExpr — resolução do NOME do produto a partir de sz_stock.product_id.
//
// AUDIT FIX Stock "#14": a tela exibia `#14`/`#13` (id cru) porque o subselect só casava
// por wp_post_id. A chave de estoque é COALESCE(wp_post_id, id) (FEAT-SKU-ESTOQUE: produto
// WP-sincronizado entra por wp_post_id; produto nativo sem wp_post_id entra por id). Por
// isso casamos os DOIS caminhos, preferindo o match por wp_post_id (determinístico via
// ORDER BY CASE, espelhando o padrão de senderzz_portal_users no resto do código).
//
// Fallback (produto órfão — id de estoque sem linha em sz_products): rótulo PT-BR legível
// que PRESERVA o id para rastreabilidade do admin, em vez do cru "#14". sz_stock não tem
// coluna sku e sz_products.nome é NOT NULL quando o produto existe, então não há SKU para
// cair — o fallback honesto é "Produto sem nome (#id)".
const stockProductNameExpr = `COALESCE(
	            (SELECT nome FROM sz_products
	              WHERE wp_post_id = s.product_id OR id = s.product_id
	              ORDER BY CASE WHEN wp_post_id = s.product_id THEN 0 ELSE 1 END
	              LIMIT 1),
	            'Produto sem nome (#' || s.product_id || ')')`

// selectClause / fromClause — SELECT enriquecido reutilizado por List e getItem.
// product_name/cd_nome via subselect com LIMIT 1 + fallback (padrão do task spec).
const stockSelectClause = `
	    s.id,
	    s.product_id,
	    s.variation_id,
	    ` + stockProductNameExpr + ` AS product_name,
	    s.cd_id,
	    COALESCE((SELECT nome FROM sz_motoboy_cds WHERE id = s.cd_id LIMIT 1),
	             'CD ' || s.cd_id)           AS cd_nome,
	    s.qty_available,
	    s.qty_reserved,
	    (s.qty_available - s.qty_reserved)   AS qty_sellable,
	    s.low_stock_threshold,
	    s.updated_at::text                   AS updated_at`

// List — GET /stock  FEAT-STOCK
func (h *StockHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Degradação graciosa: tabela ainda não existe.
	if !h.tableExists(ctx, "sz_stock") {
		httpx.JSON(w, 200, map[string]any{"ok": true, "items": []StockItem{}, "total": int64(0)})
		return
	}

	q := r.URL.Query()
	search := strings.TrimSpace(q.Get("search"))
	low := q.Get("low") == "1"

	perPage, _ := strconv.Atoi(q.Get("per_page"))
	if perPage <= 0 || perPage > 200 {
		perPage = 50
	}
	page, _ := strconv.Atoi(q.Get("page"))
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * perPage

	lowFlag := 0
	if low {
		lowFlag = 1
	}

	// product_name é coluna DERIVADA (subselect) → não pode ser referenciada no
	// WHERE. Repetimos a MESMA subexpressão (stockProductNameExpr) no filtro de busca
	// para que busca e exibição não divirjam. low filtra colunas reais.
	// $1 = search, $2 = low flag, $3 = limit, $4 = offset.
	const whereClause = `
		 WHERE ($1 = '' OR ` + stockProductNameExpr + ` ILIKE '%' || $1 || '%')
		   AND ($2 = 0 OR (s.qty_available - s.qty_reserved) <= s.low_stock_threshold)`

	rows, err := h.Pool.Query(ctx,
		`SELECT`+stockSelectClause+`
		 FROM sz_stock s`+whereClause+`
		 ORDER BY s.id DESC
		 LIMIT $3 OFFSET $4`,
		search, lowFlag, perPage, offset)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	items := []StockItem{}
	for rows.Next() {
		var it StockItem
		if err := rows.Scan(
			&it.ID, &it.ProductID, &it.VariationID, &it.ProductName,
			&it.CdID, &it.CdNome, &it.QtyAvailable, &it.QtyReserved,
			&it.QtySellable, &it.LowStockThreshold, &it.UpdatedAt,
		); err != nil {
			httpx.Err(w, 500, "scan_error", err.Error())
			return
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		httpx.Err(w, 500, "rows_error", err.Error())
		return
	}

	// total respeita os MESMOS filtros (search + low) — não só a página.
	var total int64
	_ = h.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM sz_stock s`+whereClause,
		search, lowFlag).Scan(&total)

	httpx.JSON(w, 200, map[string]any{"ok": true, "items": items, "total": total})
}

// getItem recarrega uma linha de estoque (enriquecida) por id — usado para devolver
// o estado pós-mutação. Retorna ok=false se a linha não existe.
func (h *StockHandler) getItem(ctx context.Context, id int64) (StockItem, bool) {
	var it StockItem
	err := h.Pool.QueryRow(ctx,
		`SELECT`+stockSelectClause+`
		 FROM sz_stock s WHERE s.id = $1`, id).Scan(
		&it.ID, &it.ProductID, &it.VariationID, &it.ProductName,
		&it.CdID, &it.CdNome, &it.QtyAvailable, &it.QtyReserved,
		&it.QtySellable, &it.LowStockThreshold, &it.UpdatedAt,
	)
	if err != nil {
		return StockItem{}, false
	}
	return it, true
}

// entradaBody — payload da entrada/UPSERT manual.
type entradaBody struct {
	ProductID         int64 `json:"product_id"`
	VariationID       int64 `json:"variation_id"`
	CdID              int64 `json:"cd_id"`
	QtyAvailable      int64 `json:"qty_available"`
	LowStockThreshold int64 `json:"low_stock_threshold"`
}

// Create — POST /stock  FEAT-STOCK
//
// UPSERT por (product_id, variation_id, cd_id):
//   - linha nova: cria com qty_available informado; delta_available = qty_available.
//   - linha existente: qty_available passa a valer o valor informado (entrada =
//     contagem absoluta); delta_available = novo - antigo (pode ser negativo se o
//     operador corrigir para baixo). low_stock_threshold é atualizado quando > 0.
//
// Grava um movimento tipo='entrada' com motivo='entrada manual' e o delta aplicado.
// O movimento usa a tripla (product_id, variation_id, cd_id) — NÃO há stock_id.
func (h *StockHandler) Create(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_stock") {
		httpx.Err(w, 503, "table_not_found", "tabela sz_stock não existe — execute a migration de estoque")
		return
	}

	var b entradaBody
	if err := httpx.DecodeJSON(r, &b); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	if b.ProductID <= 0 {
		httpx.Err(w, 400, "validation", "campo product_id é obrigatório")
		return
	}
	if b.QtyAvailable < 0 {
		httpx.Err(w, 400, "validation", "qty_available não pode ser negativo")
		return
	}

	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck — no-op após Commit

	// Quantidade anterior (0 se for linha nova) para calcular o delta aplicado.
	// FOR UPDATE serializa entradas concorrentes na MESMA tripla — sem o lock, dois
	// Create idênticos poderiam ler o mesmo prevQty e registrar delta em dobro no
	// ledger (a qty final fica correta pelo UPSERT absoluto, mas o razão erraria).
	var prevQty int64
	err = tx.QueryRow(ctx,
		`SELECT qty_available FROM sz_stock
		 WHERE product_id=$1 AND variation_id=$2 AND cd_id=$3 FOR UPDATE`,
		b.ProductID, b.VariationID, b.CdID).Scan(&prevQty)
	if err != nil && err != pgx.ErrNoRows {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// UPSERT pela UNIQUE (product_id, variation_id, cd_id).
	var id int64
	if err := tx.QueryRow(ctx,
		`INSERT INTO sz_stock (product_id, variation_id, cd_id, qty_available, low_stock_threshold, updated_at)
		 VALUES ($1,$2,$3,$4,$5,NOW())
		 ON CONFLICT (product_id, variation_id, cd_id) DO UPDATE
		    SET qty_available       = EXCLUDED.qty_available,
		        low_stock_threshold = CASE WHEN EXCLUDED.low_stock_threshold > 0
		                                   THEN EXCLUDED.low_stock_threshold
		                                   ELSE sz_stock.low_stock_threshold END,
		        updated_at          = NOW()
		 RETURNING id`,
		b.ProductID, b.VariationID, b.CdID, b.QtyAvailable, b.LowStockThreshold).Scan(&id); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	delta := b.QtyAvailable - prevQty
	if _, merr := tx.Exec(ctx,
		`INSERT INTO sz_stock_movements
		    (product_id, variation_id, cd_id, delta_available, delta_reserved, tipo, motivo, created_at)
		 VALUES ($1,$2,$3,$4,0,'entrada','entrada manual',NOW())`,
		b.ProductID, b.VariationID, b.CdID, delta); merr != nil {
		httpx.Err(w, 500, "db_error", merr.Error())
		return
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	it, _ := h.getItem(ctx, id)
	httpx.JSON(w, 200, map[string]any{"ok": true, "item": it})
}

// ajusteBody — payload do ajuste manual.
type ajusteBody struct {
	DeltaAvailable int64  `json:"delta_available"`
	Motivo         string `json:"motivo"`
}

// Adjust — POST /stock/{id}/ajuste  FEAT-STOCK
//
// qty_available = GREATEST(0, qty_available + delta_available); grava movimento
// tipo='ajuste' com a tripla (product_id, variation_id, cd_id) da linha resolvida.
// Motivo é obrigatório (400 se vazio).
func (h *StockHandler) Adjust(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_stock") {
		httpx.Err(w, 503, "table_not_found", "tabela sz_stock não existe — execute a migration de estoque")
		return
	}

	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if id <= 0 {
		httpx.Err(w, 400, "validation", "id inválido")
		return
	}

	var b ajusteBody
	if err := httpx.DecodeJSON(r, &b); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	motivo := strings.TrimSpace(b.Motivo)
	if motivo == "" {
		httpx.Err(w, 400, "validation", "campo motivo é obrigatório")
		return
	}

	// AUDIT STOCK-no-adjustment-audit: o ajuste manual de estoque precisa registrar
	// QUEM ajustou. A tabela sz_stock_movements não tem coluna admin_id (DDL é
	// owned pela migration de estoque), então gravamos a identidade do admin
	// autenticado + IP de origem como prefixo assinado no campo livre `motivo`.
	// Sem admin = 401 já barrado pelo middleware (rota dentro do grupo auth).
	admin := auth.FromCtx(ctx)
	if admin != nil {
		motivo = fmt.Sprintf("[admin #%d %s | ip %s] %s",
			admin.ID, admin.Email, stockClientIP(r), motivo)
	}

	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Trava a linha; lê qty + tripla (necessária para o movimento, que não tem stock_id).
	var prev, productID, variationID, cdID int64
	if err := tx.QueryRow(ctx,
		`SELECT qty_available, product_id, variation_id, cd_id
		 FROM sz_stock WHERE id=$1 FOR UPDATE`, id).Scan(&prev, &productID, &variationID, &cdID); err != nil {
		if err == pgx.ErrNoRows {
			httpx.Err(w, 404, "not_found", "estoque não encontrado")
			return
		}
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	novo := prev + b.DeltaAvailable
	if novo < 0 {
		novo = 0
	}
	// delta efetivamente aplicado (difere do solicitado quando clampa em 0).
	deltaAplicado := novo - prev

	if _, err := tx.Exec(ctx,
		`UPDATE sz_stock SET qty_available=$1, updated_at=NOW() WHERE id=$2`, novo, id); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO sz_stock_movements
		    (product_id, variation_id, cd_id, delta_available, delta_reserved, tipo, motivo, created_at)
		 VALUES ($1,$2,$3,$4,0,'ajuste',$5,NOW())`,
		productID, variationID, cdID, deltaAplicado, motivo); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	it, _ := h.getItem(ctx, id)
	httpx.JSON(w, 200, map[string]any{"ok": true, "item": it})
}

// biparBody — payload da bipagem de ENTRADA de estoque. FEAT-SKU-ESTOQUE
//
// O OL bipa 1 unidade do produto (lê o SKU da etiqueta) e digita a QUANTIDADE.
// O SKU bipado precisa corresponder a um produto real do produtor — senão NÃO credita.
type biparBody struct {
	ProducerID  int64  `json:"producer_id"`
	SKU         string `json:"sku"`
	Quantity    int64  `json:"quantity"`
	ManualTyped bool   `json:"manual_typed"`
}

// Bipar — POST /admin/stock/bipar  FEAT-SKU-ESTOQUE
//
// DONO: "ol le produto para alimentar estoque indicando a quantidade bipando um só -
// validação de sku do produto com o que ta no pedido se for diferente nao deixa seguir".
//
// Fluxo:
//  1. valida o SKU bipado contra sz_products do produtor (producer_id = produtor_id).
//     SKU inexistente → 422 PT-BR "SKU não encontrado para este produtor." e NÃO credita.
//  2. credita sz_stock do produto pela quantity de forma ADITIVA (alimentar = somar,
//     NÃO é contagem absoluta como o Create/stock-take). Reusa o caminho de entrada:
//     UPSERT sz_stock + movimento tipo='entrada'. delta_available = +quantity.
//  3. SEMPRE grava sz_pack_scans (context='estoque') — nos DOIS ramos (achou/não achou),
//     pois a coluna matched existe justamente para registrar mismatches.
//
// Chave de estoque: COALESCE(wp_post_id, id) — é a MESMA chave que o trigger de reserva
// usa (sz_order_items.produto_id casa wp_post_id p/ produto WP-sincronizado e id p/
// produto nativo sem wp_post_id), garantindo que o crédito da bipagem abata as reservas.
// variation_id e cd_id ficam 0 (body não os informa) — defaults do schema.
func (h *StockHandler) Bipar(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_stock") {
		httpx.Err(w, 503, "table_not_found", "tabela sz_stock não existe — execute a migration de estoque")
		return
	}

	var b biparBody
	if err := httpx.DecodeJSON(r, &b); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	b.SKU = strings.TrimSpace(b.SKU)
	if b.ProducerID <= 0 {
		httpx.Err(w, 400, "validation", "campo producer_id é obrigatório")
		return
	}
	if b.SKU == "" {
		httpx.Err(w, 400, "validation", "campo sku é obrigatório")
		return
	}
	if b.Quantity <= 0 {
		httpx.Err(w, 400, "validation", "quantity deve ser maior que zero")
		return
	}

	// actor para o rastro de auditoria (mesmo padrão do Adjust).
	actor := ""
	if admin := auth.FromCtx(ctx); admin != nil {
		actor = fmt.Sprintf("admin #%d %s", admin.ID, admin.Email)
	}

	// 1) Valida o SKU bipado contra sz_products do produtor. produkt-SKU ILIKE (case-insensitive).
	//    productID = COALESCE(wp_post_id, id) → chave usada por sz_stock/reservas.
	var productID int64
	var skuFound string
	err := h.Pool.QueryRow(ctx,
		`SELECT COALESCE(wp_post_id, id), sku
		   FROM sz_products
		  WHERE produtor_id = $1 AND sku ILIKE $2
		  LIMIT 1`,
		b.ProducerID, b.SKU).Scan(&productID, &skuFound)

	// 1a) NÃO encontrou → grava o scan (matched=false) ANTES de devolver 422.
	if err == pgx.ErrNoRows {
		if h.tableExists(ctx, "sz_pack_scans") {
			_, _ = h.Pool.Exec(ctx,
				`INSERT INTO sz_pack_scans
				    (context, product_id, sku_scanned, sku_expected, matched, quantity, manual_typed, actor)
				 VALUES ('estoque', NULL, $1, NULL, false, $2, $3, $4)`,
				b.SKU, b.Quantity, b.ManualTyped, nullableStr(actor))
		}
		httpx.Err(w, 422, "sku_nao_encontrado", "SKU não encontrado para este produtor.")
		return
	}
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// 2) Encontrou → credita ADITIVAMENTE + movimento + scan numa única transação.
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck — no-op após Commit

	// UPSERT aditivo pela UNIQUE (product_id, variation_id, cd_id) — alimentar = somar.
	// RETURNING qty_available = novo saldo. (NÃO usar EXCLUDED.qty_available no UPDATE:
	// isso seria contagem absoluta/stock-take, o oposto de "alimentar estoque".)
	var novoSaldo int64
	if err := tx.QueryRow(ctx,
		`INSERT INTO sz_stock (product_id, variation_id, cd_id, qty_available, updated_at)
		 VALUES ($1, 0, 0, $2, NOW())
		 ON CONFLICT (product_id, variation_id, cd_id) DO UPDATE
		    SET qty_available = sz_stock.qty_available + EXCLUDED.qty_available,
		        updated_at    = NOW()
		 RETURNING qty_available`,
		productID, b.Quantity).Scan(&novoSaldo); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// Movimento de entrada — delta sempre = +quantity (tudo é estoque novo).
	if _, err := tx.Exec(ctx,
		`INSERT INTO sz_stock_movements
		    (product_id, variation_id, cd_id, delta_available, delta_reserved, tipo, motivo, created_at)
		 VALUES ($1, 0, 0, $2, 0, 'entrada', $3, NOW())`,
		productID, b.Quantity,
		fmt.Sprintf("bipagem entrada sku=%s%s", skuFound, actorSuffix(actor))); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// Scan matched=true. sku_expected = SKU real do produto achado.
	if h.tableExists(ctx, "sz_pack_scans") {
		if _, err := tx.Exec(ctx,
			`INSERT INTO sz_pack_scans
			    (context, product_id, sku_scanned, sku_expected, matched, quantity, manual_typed, actor)
			 VALUES ('estoque', $1, $2, $3, true, $4, $5, $6)`,
			productID, b.SKU, skuFound, b.Quantity, b.ManualTyped, nullableStr(actor)); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 200, map[string]any{"ok": true, "product_id": productID, "novo_saldo": novoSaldo})
}

// nullableStr devolve nil para string vazia (grava NULL no actor) ou a própria string.
func nullableStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// actorSuffix formata o sufixo " [actor]" para o motivo, ou "" se anônimo.
func actorSuffix(actor string) string {
	if actor == "" {
		return ""
	}
	return " [" + actor + "]"
}

// Movements — GET /stock/{id}/movements?limit=50  FEAT-STOCK
//
// sz_stock_movements não tem stock_id: resolvemos {id} → (product_id, variation_id,
// cd_id) via sz_stock e filtramos o ledger por essa tripla.
func (h *StockHandler) Movements(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_stock_movements") {
		httpx.JSON(w, 200, map[string]any{"ok": true, "movements": []StockMovement{}})
		return
	}

	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if id <= 0 {
		httpx.Err(w, 400, "validation", "id inválido")
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	// Resolve a tripla da linha de estoque. Linha inexistente → lista vazia.
	var productID, variationID, cdID int64
	if err := h.Pool.QueryRow(ctx,
		`SELECT product_id, variation_id, cd_id FROM sz_stock WHERE id=$1`, id).Scan(
		&productID, &variationID, &cdID); err != nil {
		if err == pgx.ErrNoRows {
			httpx.JSON(w, 200, map[string]any{"ok": true, "movements": []StockMovement{}})
			return
		}
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	rows, err := h.Pool.Query(ctx,
		`SELECT
		    id,
		    tipo,
		    delta_available,
		    delta_reserved,
		    order_id,
		    motivo,
		    created_at::text
		 FROM sz_stock_movements
		 WHERE product_id=$1 AND variation_id=$2 AND cd_id=$3
		 ORDER BY id DESC
		 LIMIT $4`, productID, variationID, cdID, limit)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	movements := []StockMovement{}
	for rows.Next() {
		var m StockMovement
		if err := rows.Scan(
			&m.ID, &m.Tipo, &m.DeltaAvailable, &m.DeltaReserved,
			&m.OrderID, &m.Motivo, &m.CreatedAt,
		); err != nil {
			httpx.Err(w, 500, "scan_error", err.Error())
			return
		}
		movements = append(movements, m)
	}
	if err := rows.Err(); err != nil {
		httpx.Err(w, 500, "rows_error", err.Error())
		return
	}

	httpx.JSON(w, 200, map[string]any{"ok": true, "movements": movements})
}
