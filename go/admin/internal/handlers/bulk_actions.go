// Package handlers — endpoints de ações em lote para etiquetas.
// Espelha src/Admin/Bulk_Actions/Bulk_Pipeline.php (PHP legado) sobre Postgres.
// Enfileira geração de etiquetas via tabela wc_me_labels; processamento real
// é assíncrono (Asynq). Este handler apenas enfileira e consulta status.
package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/auth"
	"github.com/senderzz/admin-service/internal/httpx"
)

// BulkActionsHandler lida com ações em lote de etiquetas.
type BulkActionsHandler struct {
	Pool             *pgxpool.Pool
	LabelsServiceURL string
}

// tableExistsBulk verifica se tabela existe no schema public.
func (h *BulkActionsHandler) tableExistsBulk(ctx context.Context, name string) bool {
	return tableExistsCached(ctx, h.Pool, name) // AUDIT-2026-06-18 Onda2 (go-infoschema-cache)
}

// bulkOrder representa um pedido elegível para geração em lote.
//
// Fluxo de etiqueta MOTOBOY (sz_motoboy_pedidos.status): agendado → embalado →
// em_rota → ... A elegibilidade de "gerar etiqueta" é POR LINHA, derivada de
// mb_status:
//   - agendado            → pode GERAR etiqueta (transição p/ embalado)
//   - embalado            → pode IMPRIMIR (já gerada)
//   - em_rota ou posterior→ sem acesso à etiqueta
type bulkOrder struct {
	OrderID         int64   `json:"order_id"`
	CustomerName    string  `json:"customer_name"`
	Status          string  `json:"status"`
	ShippingClass   string  `json:"shipping_class"`
	ShippingClassID *int64  `json:"shipping_class_id"`
	LabelStatus     string  `json:"label_status"` // none|queued|processing|done|error|cancelled
	Total           float64 `json:"total"`
	CreatedAt       string  `json:"created_at"`

	// Motoboy — pedido vinculado em sz_motoboy_pedidos (pode não existir).
	PedidoID    *int64  `json:"pedido_id"`     // sz_motoboy_pedidos.id
	MotoboyID   *int64  `json:"motoboy_id"`    // motoboy atribuído (pode ser null)
	MotoboyNome string  `json:"motoboy_nome"`  // nome do motoboy atribuído
	MbStatus    string  `json:"mb_status"`     // status no fluxo motoboy ('' se sem pedido)
	PackageCode string  `json:"package_code"`  // QR HMAC p/ impressão (só embalado)
	// Dados de destino — preenchidos p/ permitir impressão da etiqueta no front.
	DestNome        string `json:"dest_nome"`
	DestEndereco    string `json:"dest_endereco"`
	DestNumero      string `json:"dest_numero"`
	DestComplemento string `json:"dest_complemento"`
	DestBairro      string `json:"dest_bairro"`
	DestCidade      string `json:"dest_cidade"`
	DestUF          string `json:"dest_uf"`
	DestCEP         string `json:"dest_cep"`
	DestTelefone    string `json:"dest_telefone"`
	DestProduto     string `json:"dest_produto"` // dest_produto puro (já vem "2x Nome", qtd + nome uma vez só)
}

// shippingClass representa uma classe de envio para o dropdown de filtro.
type shippingClass struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// queueItem representa o status de uma etiqueta na fila.
type queueItem struct {
	OrderID  int64   `json:"order_id"`
	Status   string  `json:"status"` // queued|processing|done|error
	PrintURL *string `json:"print_url"`
	Error    *string `json:"error"`
}

// ListOrders lista pedidos elegíveis para geração de etiqueta em lote.
// GET /bulk-actions/orders?status=&shipping_class=&date_from=&date_to=&limit=200
func (h *BulkActionsHandler) ListOrders(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	status := q.Get("status")
	shippingClass := q.Get("shipping_class")
	dateFrom := q.Get("date_from")
	dateTo := q.Get("date_to")

	// Verifica tabelas necessárias — degradação graciosa se ausentes.
	if !h.tableExistsBulk(ctx, "sz_orders") {
		httpx.JSON(w, 200, map[string]any{"items": []bulkOrder{}, "count": 0})
		return
	}

	hasLabels := h.tableExistsBulk(ctx, "wc_me_labels")
	hasMotoboyPed := h.tableExistsBulk(ctx, "sz_motoboy_pedidos")

	// Monta SELECT com JOIN opcional para label_status + dados do pedido motoboy.
	customerNameExpr := `COALESCE(o.customer_name, '') AS customer_name`
	if hasMotoboyPed {
		customerNameExpr = `COALESCE(NULLIF(o.customer_name,''), NULLIF(mp.dest_nome,''), '') AS customer_name`
	}
	var sb strings.Builder
	sb.WriteString(`
		SELECT
			o.id AS order_id,
			` + customerNameExpr + `,
			COALESCE(o.status, '') AS status,
			COALESCE(o.shipping_class, '') AS shipping_class,
			o.shipping_class_id,
			COALESCE(o.total, 0) AS total,
			o.created_at::text`)

	if hasLabels {
		sb.WriteString(`,
			COALESCE(l.status, 'none') AS label_status`)
	} else {
		sb.WriteString(`,
			'none'::text AS label_status`)
	}

	// Colunas do pedido motoboy (atribuição + status do fluxo de etiqueta).
	if hasMotoboyPed {
		sb.WriteString(`,
			mp.id              AS pedido_id,
			mp.motoboy_id      AS motoboy_id,
			COALESCE(mb.nome, '') AS motoboy_nome,
			COALESCE(mp.status, '') AS mb_status,
			COALESCE(mp.dest_nome, '')        AS dest_nome,
			COALESCE(mp.dest_endereco, '')    AS dest_endereco,
			COALESCE(mp.dest_numero, '')      AS dest_numero,
			COALESCE(mp.dest_complemento, '') AS dest_complemento,
			COALESCE(mp.dest_bairro, '')      AS dest_bairro,
			COALESCE(mp.dest_cidade, '')      AS dest_cidade,
			COALESCE(mp.dest_uf, '')          AS dest_uf,
			COALESCE(mp.dest_cep, '')         AS dest_cep,
			COALESCE(mp.dest_telefone, '')    AS dest_telefone,
			COALESCE(mp.dest_produto, '')     AS dest_produto`)
	} else {
		sb.WriteString(`,
			NULL::bigint AS pedido_id,
			NULL::bigint AS motoboy_id,
			''::text AS motoboy_nome,
			''::text AS mb_status,
			''::text AS dest_nome,
			''::text AS dest_endereco,
			''::text AS dest_numero,
			''::text AS dest_complemento,
			''::text AS dest_bairro,
			''::text AS dest_cidade,
			''::text AS dest_uf,
			''::text AS dest_cep,
			''::text AS dest_telefone,
			''::text AS dest_produto`)
	}

	sb.WriteString(`
		FROM sz_orders o`)

	if hasLabels {
		sb.WriteString(`
		LEFT JOIN wc_me_labels l ON l.wc_order_id = o.id`)
	}

	if hasMotoboyPed {
		// Mesma convenção de chave usada para wc_me_labels: wc_order_id = o.id.
		sb.WriteString(`
		LEFT JOIN sz_motoboy_pedidos mp ON mp.wc_order_id = o.id
		LEFT JOIN sz_motoboys        mb ON mb.id = mp.motoboy_id`)
	}

	sb.WriteString(`
		WHERE 1=1`)

	args := []any{}
	argN := 1

	// Filtro de data = DATA DE ENTREGA do pedido motoboy (data_entrega →
	// reagendado_para → created_at), não a data de venda (o.created_at).
	// Quando a tabela motoboy não existe (ou o pedido não tem linha mp), cai
	// para o.created_at::date para não derrubar pedidos WC gerais do resultado.
	// dateCol é whitelist interna (não vem de input) — sem risco de injeção.
	dateCol := "o.created_at"
	if hasMotoboyPed {
		dateCol = "COALESCE(mp.data_entrega, mp.reagendado_para, mp.created_at::date, o.created_at::date)"
	}

	if status != "" {
		sb.WriteString(fmt.Sprintf(` AND o.status = $%d`, argN))
		args = append(args, status)
		argN++
	}
	if shippingClass != "" {
		sb.WriteString(fmt.Sprintf(` AND o.shipping_class = $%d`, argN))
		args = append(args, shippingClass)
		argN++
	}
	if dateFrom != "" {
		sb.WriteString(fmt.Sprintf(` AND %s >= $%d`, dateCol, argN))
		args = append(args, dateFrom)
		argN++
	}
	if dateTo != "" {
		sb.WriteString(fmt.Sprintf(` AND %s <= $%d`, dateCol, argN))
		args = append(args, dateTo+" 23:59:59")
		argN++
	}

	sb.WriteString(fmt.Sprintf(` ORDER BY o.id DESC LIMIT $%d`, argN))
	args = append(args, limit)

	rows, err := h.Pool.Query(ctx, sb.String(), args...)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []bulkOrder{}
	for rows.Next() {
		var bo bulkOrder
		if err := rows.Scan(
			&bo.OrderID, &bo.CustomerName, &bo.Status,
			&bo.ShippingClass, &bo.ShippingClassID,
			&bo.Total, &bo.CreatedAt, &bo.LabelStatus,
			&bo.PedidoID, &bo.MotoboyID, &bo.MotoboyNome, &bo.MbStatus,
			&bo.DestNome, &bo.DestEndereco, &bo.DestNumero, &bo.DestComplemento,
			&bo.DestBairro, &bo.DestCidade, &bo.DestUF, &bo.DestCEP, &bo.DestTelefone,
			&bo.DestProduto,
		); err == nil {
			// Package code (QR HMAC) só faz sentido a partir de 'embalado' — é o
			// que o front usa p/ imprimir. Em 'agendado' a etiqueta ainda não existe.
			if bo.PedidoID != nil && bo.MbStatus == "embalado" {
				bo.PackageCode = motoboyPackageCode(*bo.PedidoID, bo.OrderID)
			}
			out = append(out, bo)
		}
	}

	httpx.JSON(w, 200, map[string]any{"items": out, "count": len(out)})
}

// motoboyGenerateRequest corpo do POST /bulk-actions/motoboy-generate-labels.
type motoboyGenerateRequest struct {
	OrderIDs  []int64 `json:"order_ids"`  // = sz_orders.id (== wc_order_id nesta base)
	MotoboyID *int64  `json:"motoboy_id"` // opcional — atribui motoboy aos pedidos
}

// GenerateMotoboyLabels gera etiquetas do fluxo MOTOBOY para os pedidos
// selecionados: transição agendado → embalado.
//
// Regras (espelham o fluxo logístico do painel):
//  1. Só age sobre pedidos em status 'agendado' (filtro de elegibilidade).
//     Pedidos em embalado/em_rota/etc. são ignorados (0 linhas) — isso é o que
//     BLOQUEIA gerar de novo (Req 4) no servidor, independente da UI.
//  2. Se motoboy_id informado, atribui ao(s) pedido(s) (COALESCE preserva o
//     existente quando não informado).
//  3. Marca sz_motoboy_pedidos.status='embalado' + ts_embalado=now() e
//     sz_orders.status='embalado' na mesma transação.
//
// POST /bulk-actions/motoboy-generate-labels
func (h *BulkActionsHandler) GenerateMotoboyLabels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Gate: admin ou operador logístico (DualAuth). Produtor/afiliado/cliente: 403.
	if actor := auth.ActorFromCtx(ctx); actor != nil && actor.Kind != auth.ActorAdmin {
		if actor.Kind != auth.ActorKind("operator") && actor.Kind != auth.ActorKind("operador") {
			httpx.Err(w, 403, "forbidden", "acesso restrito a admin e operador logístico")
			return
		}
	}

	var req motoboyGenerateRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Err(w, 400, "bad_request", "JSON inválido")
		return
	}
	if len(req.OrderIDs) == 0 {
		httpx.Err(w, 400, "bad_request", "order_ids não pode ser vazio")
		return
	}
	if len(req.OrderIDs) > 100 {
		httpx.Err(w, 400, "bad_request", "máximo de 100 pedidos por requisição")
		return
	}
	if req.MotoboyID != nil && *req.MotoboyID <= 0 {
		req.MotoboyID = nil
	}

	if !h.tableExistsBulk(ctx, "sz_motoboy_pedidos") {
		httpx.Err(w, 503, "table_missing", "tabela sz_motoboy_pedidos ainda não migrada")
		return
	}

	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Transiciona apenas os pedidos em 'agendado' — guard idempotente.
	rows, err := tx.Query(ctx,
		`UPDATE sz_motoboy_pedidos
		    SET status      = 'embalado',
		        ts_embalado = now(),
		        motoboy_id  = COALESCE($2, motoboy_id),
		        updated_at  = now()
		  WHERE wc_order_id = ANY($1)
		    AND status = 'agendado'
		  RETURNING wc_order_id`,
		req.OrderIDs, req.MotoboyID)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	embaladoIDs := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			embaladoIDs = append(embaladoIDs, id)
		}
	}
	rows.Close()

	// Reflete no pedido logístico (sz_orders) — check constraint aceita 'embalado'.
	if len(embaladoIDs) > 0 && h.tableExistsBulk(ctx, "sz_orders") {
		_, err = tx.Exec(ctx,
			`UPDATE sz_orders SET status = 'embalado'
			  WHERE id = ANY($1) AND status <> 'embalado'`,
			embaladoIDs)
		if err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	skipped := len(req.OrderIDs) - len(embaladoIDs)
	httpx.JSON(w, 200, map[string]any{
		"ok":            true,
		"embalado":      len(embaladoIDs),
		"skipped":       skipped, // não estavam em 'agendado' (já gerada ou sem pedido)
		"embalado_ids":  embaladoIDs,
	})
}

// generateLabelsRequest corpo do POST /bulk-actions/generate-labels.
type generateLabelsRequest struct {
	OrderIDs []int64 `json:"order_ids"`
	Mode     string  `json:"mode"` // with_pdf | no_pdf | print_batch
}

// GenerateLabels enfileira geração de etiquetas para os pedidos selecionados.
// Processamento real é assíncrono. Este endpoint apenas enfileira (INSERT/UPDATE).
// POST /bulk-actions/generate-labels
func (h *BulkActionsHandler) GenerateLabels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req generateLabelsRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Err(w, 400, "bad_request", "JSON inválido")
		return
	}

	// Valida order_ids.
	if len(req.OrderIDs) == 0 {
		httpx.Err(w, 400, "bad_request", "order_ids não pode ser vazio")
		return
	}
	if len(req.OrderIDs) > 100 {
		httpx.Err(w, 400, "bad_request", "máximo de 100 pedidos por requisição")
		return
	}

	// Valida modo.
	validModes := map[string]bool{"with_pdf": true, "no_pdf": true, "print_batch": true}
	if !validModes[req.Mode] {
		httpx.Err(w, 400, "bad_request", "mode inválido: use with_pdf, no_pdf ou print_batch")
		return
	}

	// Verifica tabela de etiquetas — degradação graciosa.
	if !h.tableExistsBulk(ctx, "wc_me_labels") {
		httpx.Err(w, 503, "table_missing", "tabela wc_me_labels ainda não migrada")
		return
	}

	queued := 0
	alreadyQueued := 0
	var errs []string

	now := time.Now()

	for _, orderID := range req.OrderIDs {
		// Verifica status atual para distinguir "já na fila" de "novo".
		var currentStatus string
		err := h.Pool.QueryRow(ctx,
			`SELECT status FROM wc_me_labels WHERE wc_order_id = $1`, orderID,
		).Scan(&currentStatus)

		if err == nil && (currentStatus == "queued" || currentStatus == "processing") {
			alreadyQueued++
			continue
		}

		// INSERT ... ON CONFLICT: atualiza para queued se já existir com outro status.
		_, err = h.Pool.Exec(ctx,
			`INSERT INTO wc_me_labels (wc_order_id, status, mode, created_at, updated_at)
			 VALUES ($1, 'queued', $2, $3, $3)
			 ON CONFLICT (wc_order_id)
			 DO UPDATE SET status = 'queued', mode = EXCLUDED.mode, updated_at = EXCLUDED.updated_at`,
			orderID, req.Mode, now)
		if err != nil {
			errs = append(errs, fmt.Sprintf("pedido %d: %s", orderID, err.Error()))
			continue
		}
		queued++
	}

	httpx.JSON(w, 200, map[string]any{
		"ok":            len(errs) == 0,
		"queued":        queued,
		"already_queued": alreadyQueued,
		"errors":        errs,
	})
}

// QueueStatus retorna o status de etiquetas para os pedidos informados.
// GET /bulk-actions/queue-status?order_ids=1,2,3
func (h *BulkActionsHandler) QueueStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	rawIDs := r.URL.Query().Get("order_ids")
	if rawIDs == "" {
		httpx.Err(w, 400, "bad_request", "order_ids é obrigatório")
		return
	}

	// Parse dos IDs separados por vírgula.
	parts := strings.Split(rawIDs, ",")
	ids := make([]int64, 0, len(parts))
	for _, p := range parts {
		id, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
		if err == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		httpx.Err(w, 400, "bad_request", "nenhum order_id válido")
		return
	}

	// Tabela ausente → retorna "none" para todos.
	if !h.tableExistsBulk(ctx, "wc_me_labels") {
		items := make([]queueItem, 0, len(ids))
		for _, id := range ids {
			items = append(items, queueItem{OrderID: id, Status: "none"})
		}
		httpx.JSON(w, 200, map[string]any{"items": items})
		return
	}

	// Monta cláusula IN com placeholders.
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	inClause := strings.Join(placeholders, ",")

	rows, err := h.Pool.Query(ctx,
		fmt.Sprintf(`
			SELECT wc_order_id, COALESCE(status,'none'), print_url, error_message
			FROM wc_me_labels
			WHERE wc_order_id IN (%s)`, inClause),
		args...)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	// Mapeia resultados por order_id.
	found := map[int64]queueItem{}
	for rows.Next() {
		var qi queueItem
		_ = rows.Scan(&qi.OrderID, &qi.Status, &qi.PrintURL, &qi.Error)
		found[qi.OrderID] = qi
	}

	// Retorna item para cada ID pedido — "none" para os não encontrados.
	items := make([]queueItem, 0, len(ids))
	for _, id := range ids {
		if qi, ok := found[id]; ok {
			items = append(items, qi)
		} else {
			items = append(items, queueItem{OrderID: id, Status: "none"})
		}
	}

	httpx.JSON(w, 200, map[string]any{"items": items})
}

// ShippingClasses lista classes de envio disponíveis para o filtro.
// GET /bulk-actions/shipping-classes
func (h *BulkActionsHandler) ShippingClasses(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Tabela ausente → lista vazia.
	if !h.tableExistsBulk(ctx, "sz_orders") {
		httpx.JSON(w, 200, map[string]any{"items": []shippingClass{}})
		return
	}

	// Tenta buscar de tabela dedicada; fallback para DISTINCT em sz_orders.
	hasSCTable := h.tableExistsBulk(ctx, "sz_shipping_classes")

	var items []shippingClass

	if hasSCTable {
		rows, err := h.Pool.Query(ctx,
			`SELECT id, COALESCE(name,'') FROM sz_shipping_classes ORDER BY name`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var sc shippingClass
				_ = rows.Scan(&sc.ID, &sc.Name)
				items = append(items, sc)
			}
		}
	} else {
		// Fallback: DISTINCT a partir de sz_orders.
		rows, err := h.Pool.Query(ctx,
			`SELECT DISTINCT
				COALESCE(shipping_class_id, 0) AS id,
				COALESCE(shipping_class, 'Padrão') AS name
			 FROM sz_orders
			 WHERE shipping_class IS NOT NULL AND shipping_class != ''
			 ORDER BY name`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var sc shippingClass
				_ = rows.Scan(&sc.ID, &sc.Name)
				items = append(items, sc)
			}
		}
	}

	if items == nil {
		items = []shippingClass{}
	}

	httpx.JSON(w, 200, map[string]any{"items": items})
}

// PrintBatch — POST /bulk-actions/print-batch (pedido dono 2026-07-28).
//
// Individual (1 pedido) e lote (vários) usam o MESMO endpoint — o front manda
// a lista de order_ids selecionados, aqui só um. Pega etiqueta(s) JÁ EMITIDA(S)
// (nunca gera nova, nunca muda status) e devolve etiquetas e/ou declaração de
// conteúdo (DACE simplificado), conforme want_labels/want_declaration. Proxy pro
// labels-service (POST /internal/labels/print-batch), que fala com a ME.
type printBatchRequest struct {
	OrderIDs        []int64 `json:"order_ids"`
	WantLabels      bool    `json:"want_labels"`
	WantDeclaration bool    `json:"want_declaration"`
}

// canPrintExpeditionBatchActor mantém a rota admin existente compatível (o
// auth.Middleware legado não injeta Actor, portanto actor=nil) e autoriza, no
// grupo DualAuth, somente admin e operador logístico. Produtor/afiliado/cliente
// nunca podem baixar etiquetas globais com PII de outros pedidos.
func canPrintExpeditionBatchActor(actor *auth.Actor) bool {
	return actor == nil || actor.Kind == auth.ActorAdmin ||
		actor.Kind == auth.ActorKind("operator") || actor.Kind == auth.ActorKind("operador")
}

func (h *BulkActionsHandler) PrintBatch(w http.ResponseWriter, r *http.Request) {
	if !canPrintExpeditionBatchActor(auth.ActorFromCtx(r.Context())) {
		httpx.Err(w, http.StatusForbidden, "forbidden", "acesso restrito a admin e operador logístico")
		return
	}

	if h.LabelsServiceURL == "" {
		httpx.Err(w, 503, "labels_service_unavailable",
			"LABELS_SERVICE_URL não configurado; configure a variável de ambiente e reinicie o admin-service")
		return
	}

	var req printBatchRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	if len(req.OrderIDs) == 0 {
		httpx.Err(w, 400, "validation", "selecione ao menos 1 pedido")
		return
	}
	if len(req.OrderIDs) > 100 {
		httpx.Err(w, 400, "validation", "máximo de 100 pedidos por impressão")
		return
	}
	if !req.WantLabels && !req.WantDeclaration {
		httpx.Err(w, 400, "validation", "escolha etiquetas e/ou declaração de conteúdo")
		return
	}

	body, err := json.Marshal(map[string]any{
		"wc_order_ids":     req.OrderIDs,
		"want_labels":      req.WantLabels,
		"want_declaration": req.WantDeclaration,
	})
	if err != nil {
		httpx.Err(w, 500, "internal_error", "erro ao montar requisição")
		return
	}

	url := h.LabelsServiceURL + "/internal/labels/print-batch"
	httpReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		httpx.Err(w, 500, "internal_error", "erro ao contatar labels-service")
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	// AUDIT-2026-07-30 HIGH: mesmo secret compartilhado de order_status_sync.go —
	// endpoint interno serve PDF de etiqueta com PII (endereço/nome do cliente).
	if secret, ok := internalSecretHeader(); ok {
		httpReq.Header.Set("X-Internal-Secret", secret)
	}
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		httpx.Err(w, 502, "labels_service_error", "falha ao contatar labels-service: "+err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		httpx.Err(w, resp.StatusCode, "labels_service_error", string(errBody))
		return
	}
	// AUDIT-2026-07-28: proxy puro — repassa bytes como vieram (PDF/ZIP reais
	// quando want_labels, JSON {declaration_url} quando só want_declaration).
	// Content-Type/Content-Disposition vêm do labels-service, não hardcoded.
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		w.Header().Set("Content-Disposition", cd)
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}
