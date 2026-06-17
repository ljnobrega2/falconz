// Package handlers — endpoint admin para custódia física do estoque motoboy.
//
// Espelha sz_mb_tab_estoque_motoboy() em includes/motoboy/admin.php:1510 (§2.6)
// sobre Postgres. A tabela sz_motoboy_stock_custody guarda cada item físico
// confiado a um motoboy. Estados (physical_status):
//
//	reserved        — alocado no CD para o pedido, ainda não bipado
//	available       — voltou ao estoque após devolução vendável
//	with_motoboy    — em rota (motoboy bipou o QR de saída)
//	frustrated      — motoboy declarou entrega frustrada, aguardando bipagem
//	return_declared — motoboy declarou devolução, aguardando confirmação do OL
//	damaged         — OL confirmou condição não-vendável (avariado/extravio/...)
//	delivered       — entregue ao cliente final
//
// Surface:
//
//	GET  /motoboy-custodia/summary               — KPIs por physical_status (5 cards)
//	GET  /motoboy-custodia?status=&motoboy_id=&limit=300
//	GET  /motoboy-custodia/summary-by-motoboy    — agregação para o resumo
//	POST /motoboy-custodia/route-assist          body: {qr_code, motoboy_id}
//	POST /motoboy-custodia/return                multipart: qr_code, condition, note, photo
//
// Notas:
//   - A tabela sz_motoboy_stock_custody usa colunas reais "package_code",
//     "occurrence_type/note/photos". A API expõe esses campos como
//     "qr_code" / "ocorrencia_*" via aliases nos struct tags JSON.
//   - "occurrence_photos" é LONGTEXT no MySQL e TEXT no Postgres — guarda um
//     JSON array de URLs como string. NÃO usar cast ::jsonb (paridade com
//     meta_json em order_detail.go:781). Decode/encode manual em Go.
//   - Quando a tabela ainda não existe (instalação antiga sem custódia),
//     retorna 200 com payload vazio em GETs e 503 em POSTs.
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/auth"
	"github.com/senderzz/admin-service/internal/httpx"
)

type MotoboyCustodiaHandler struct{ Pool *pgxpool.Pool }

// ─── Tipos de resposta ───────────────────────────────────────────────────

// CustodiaKpi — par {qty, pedidos} de um physical_status para os 5 cards.
type CustodiaKpi struct {
	Qty     int64 `json:"qty"`
	Pedidos int64 `json:"pedidos"`
}

// CustodiaSummary — agregado por physical_status, mapeado para nomes amigáveis.
// "com_motoboy" → with_motoboy, "aguardando_ol" → return_declared, etc.
type CustodiaSummary struct {
	ComMotoboy   CustodiaKpi `json:"com_motoboy"`
	Frustrados   CustodiaKpi `json:"frustrados"`
	AguardandoOL CustodiaKpi `json:"aguardando_ol"`
	Avariados    CustodiaKpi `json:"avariados"`
	Reservados   CustodiaKpi `json:"reservados"`
}

// CustodiaItem — linha de sz_motoboy_stock_custody na listagem detalhada.
// "qr_code" e "ocorrencia_*" são aliases das colunas reais (ver header).
type CustodiaItem struct {
	ID              int64    `json:"id"`
	WcOrderID       int64    `json:"wc_order_id"`
	MotoboyID       *int64   `json:"motoboy_id"`
	MotoboyNome     string   `json:"motoboy_nome"`
	ProductID       int64    `json:"product_id"`
	ProductName     string   `json:"product_name"`
	Quantity        int64    `json:"quantity"`
	PhysicalStatus  string   `json:"physical_status"`
	StatusLabel     string   `json:"status_label"`
	QrCode          string   `json:"qr_code"`
	OcorrenciaTipo  string   `json:"ocorrencia_tipo"`
	OcorrenciaNota  string   `json:"ocorrencia_nota"`
	OcorrenciaFotos []string `json:"ocorrencia_fotos"`
	CostProduct     float64  `json:"cost_product"`
	UpdatedAt       string   `json:"updated_at"`
}

// CustodiaByMotoboy — agregação por motoboy para o resumo.
type CustodiaByMotoboy struct {
	MotoboyID         *int64  `json:"motoboy_id"`
	MotoboyNome       string  `json:"motoboy_nome"`
	Pedidos           int64   `json:"pedidos"`
	Unidades          int64   `json:"unidades"`
	Frustrados        int64   `json:"frustrados"`
	AguardandoOL      int64   `json:"aguardando_ol"`
	CustoCustodia     float64 `json:"custo_custodia"`
	UltimaMovimentacao string `json:"ultima_movimentacao"`
}

// ─── Helpers ─────────────────────────────────────────────────────────────

// tableExists — mesmo padrão usado em audit.go / motoboy_carteira.go.
func (h *MotoboyCustodiaHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// statusLabel mapeia physical_status → rótulo PT-BR (paridade com $label_map
// em admin.php:1592).
func statusLabel(s string) string {
	switch s {
	case "with_motoboy":
		return "Rota"
	case "frustrated":
		return "Frustrado"
	case "return_declared":
		return "Aguardando OL"
	case "damaged":
		return "Avariado"
	case "available":
		return "Disponível"
	case "reserved":
		return "Reservado"
	case "delivered":
		return "Entregue"
	default:
		return s
	}
}

// decodePhotos — occurrence_photos chega como string JSON. Tolerante a NULL/'',
// formato inválido, ou já ser array (defensivo). Retorna sempre []string.
func decodePhotos(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return []string{}
	}
	var arr []string
	if err := json.Unmarshal([]byte(raw), &arr); err == nil {
		return arr
	}
	// Pode ter chegado uma URL solta (instalações antigas).
	return []string{raw}
}

// uploadDir — caminho onde fotos de ocorrência são gravadas. Configurável via
// CUSTODY_UPLOAD_PATH (default: ./uploads/custody/).
func uploadDir() string {
	if v := strings.TrimSpace(os.Getenv("CUSTODY_UPLOAD_PATH")); v != "" {
		return v
	}
	return "./uploads/custody"
}

// uploadURL — prefixo público das fotos. Configurável via CUSTODY_UPLOAD_URL.
// Default: /uploads/custody/ (servido como estático pelo proxy/nginx).
func uploadURL() string {
	if v := strings.TrimSpace(os.Getenv("CUSTODY_UPLOAD_URL")); v != "" {
		return strings.TrimRight(v, "/") + "/"
	}
	return "/uploads/custody/"
}

// ─── Summary ─────────────────────────────────────────────────────────────

// Summary retorna os 5 KPIs cards agregando por physical_status numa única
// query (paridade com $totals em admin.php:1563).
//
// GET /motoboy-custodia/summary
func (h *MotoboyCustodiaHandler) Summary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := CustodiaSummary{}

	// Graceful degradation: tabela ausente = zeros.
	if !h.tableExists(ctx, "sz_motoboy_stock_custody") {
		httpx.JSON(w, 200, out)
		return
	}

	// Single GROUP BY → map em Go (advisor item 8).
	rows, err := h.Pool.Query(ctx,
		`SELECT physical_status,
		        COUNT(DISTINCT pedido_id)        AS pedidos,
		        COALESCE(SUM(quantity), 0)::bigint AS unidades
		   FROM sz_motoboy_stock_custody
		  GROUP BY physical_status`)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	for rows.Next() {
		var ps string
		var pedidos, unidades int64
		if err := rows.Scan(&ps, &pedidos, &unidades); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
		kpi := CustodiaKpi{Qty: unidades, Pedidos: pedidos}
		switch ps {
		case "with_motoboy":
			out.ComMotoboy = kpi
		case "frustrated":
			out.Frustrados = kpi
		case "return_declared":
			out.AguardandoOL = kpi
		case "damaged":
			out.Avariados = kpi
		case "reserved":
			out.Reservados = kpi
		}
	}
	httpx.JSON(w, 200, out)
}

// ─── List ────────────────────────────────────────────────────────────────

// List devolve a lista detalhada de pacotes em custódia/ocorrência.
// Default: mesmos status do PHP — with_motoboy, frustrated, return_declared,
// damaged. Filtro `status` aceita um único valor (sobrepõe o default).
// Filtro `motoboy_id` é exato.
//
// GET /motoboy-custodia?status=&motoboy_id=&limit=300
func (h *MotoboyCustodiaHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_motoboy_stock_custody") {
		httpx.JSON(w, 200, map[string]any{"items": []CustodiaItem{}, "count": 0})
		return
	}

	q := r.URL.Query()
	status := strings.TrimSpace(q.Get("status"))
	motoboyID, _ := strconv.ParseInt(q.Get("motoboy_id"), 10, 64)
	limit := clampLimit(q.Get("limit"), 300, 1000)

	// Whitelist de status — evita injection via param ainda que pgx escape.
	allowed := map[string]bool{
		"with_motoboy":    true,
		"frustrated":      true,
		"return_declared": true,
		"damaged":         true,
		"available":       true,
		"reserved":        true,
		"delivered":       true,
	}
	if status != "" && !allowed[status] {
		status = ""
	}

	// Query única com filtros opcionais via parâmetros nulos.
	// $1 status (vazio = default 4 estados), $2 motoboy_id (0 = ignora), $3 limit.
	rows, err := h.Pool.Query(ctx,
		`SELECT
		    c.id,
		    COALESCE(c.wc_order_id, 0),
		    c.motoboy_id,
		    COALESCE(m.nome, '')              AS motoboy_nome,
		    COALESCE(c.product_id, 0),
		    COALESCE(c.product_name, COALESCE(c.sku, 'Produto')) AS product_name,
		    COALESCE(c.quantity, 0)::bigint,
		    COALESCE(c.physical_status, ''),
		    COALESCE(c.package_code, '')      AS qr_code,
		    COALESCE(c.occurrence_type, '')   AS ocorrencia_tipo,
		    COALESCE(c.occurrence_note, '')   AS ocorrencia_nota,
		    COALESCE(c.occurrence_photos, '') AS ocorrencia_fotos_raw,
		    COALESCE(c.cost_product, 0)::float8,
		    COALESCE(c.updated_at::text, '')
		  FROM sz_motoboy_stock_custody c
		  LEFT JOIN sz_motoboys m ON m.id = c.motoboy_id
		  WHERE
		    (
		      ($1 = '' AND c.physical_status IN ('with_motoboy','frustrated','return_declared','damaged'))
		      OR ($1 <> '' AND c.physical_status = $1)
		    )
		    AND ($2 = 0 OR c.motoboy_id = $2)
		  ORDER BY c.updated_at DESC NULLS LAST, c.id DESC
		  LIMIT $3`,
		status, motoboyID, limit)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []CustodiaItem{}
	for rows.Next() {
		var it CustodiaItem
		var photosRaw string
		if err := rows.Scan(
			&it.ID, &it.WcOrderID, &it.MotoboyID, &it.MotoboyNome,
			&it.ProductID, &it.ProductName, &it.Quantity,
			&it.PhysicalStatus, &it.QrCode,
			&it.OcorrenciaTipo, &it.OcorrenciaNota, &photosRaw,
			&it.CostProduct, &it.UpdatedAt,
		); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
		it.StatusLabel = statusLabel(it.PhysicalStatus)
		it.OcorrenciaFotos = decodePhotos(photosRaw)
		out = append(out, it)
	}
	httpx.JSON(w, 200, map[string]any{"items": out, "count": len(out)})
}

// ─── SummaryByMotoboy ────────────────────────────────────────────────────

// SummaryByMotoboy retorna a agregação usada no card "Resumo por motoboy"
// (paridade com $summary em admin.php:1567).
//
// GET /motoboy-custodia/summary-by-motoboy
func (h *MotoboyCustodiaHandler) SummaryByMotoboy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_motoboy_stock_custody") {
		httpx.JSON(w, 200, map[string]any{"items": []CustodiaByMotoboy{}, "count": 0})
		return
	}

	rows, err := h.Pool.Query(ctx,
		`SELECT
		    c.motoboy_id,
		    COALESCE(m.nome, 'Sem motoboy')   AS motoboy_nome,
		    COUNT(DISTINCT c.pedido_id)::bigint AS pedidos,
		    COALESCE(SUM(c.quantity), 0)::bigint AS unidades,
		    COALESCE(SUM(CASE WHEN c.physical_status='frustrated' THEN c.quantity ELSE 0 END), 0)::bigint AS frustrados,
		    COALESCE(SUM(CASE WHEN c.physical_status='return_declared' THEN c.quantity ELSE 0 END), 0)::bigint AS aguardando_ol,
		    COALESCE(SUM(c.quantity * c.cost_product), 0)::float8 AS valor_custo,
		    COALESCE(MAX(c.updated_at)::text, '') AS ultima_movimentacao
		  FROM sz_motoboy_stock_custody c
		  LEFT JOIN sz_motoboys m ON m.id = c.motoboy_id
		  WHERE c.physical_status IN ('with_motoboy','frustrated','return_declared')
		  GROUP BY c.motoboy_id, m.nome
		  ORDER BY unidades DESC, motoboy_nome ASC`)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []CustodiaByMotoboy{}
	for rows.Next() {
		var it CustodiaByMotoboy
		if err := rows.Scan(
			&it.MotoboyID, &it.MotoboyNome,
			&it.Pedidos, &it.Unidades,
			&it.Frustrados, &it.AguardandoOL,
			&it.CustoCustodia, &it.UltimaMovimentacao,
		); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
		out = append(out, it)
	}
	httpx.JSON(w, 200, map[string]any{"items": out, "count": len(out)})
}

// ─── RouteAssist ─────────────────────────────────────────────────────────

// routeAssistBody — body de POST /motoboy-custodia/route-assist.
type routeAssistBody struct {
	QrCode    string `json:"qr_code"`
	MotoboyID int64  `json:"motoboy_id"`
}

// RouteAssist — inicia rota pelo QR como fallback do PWA do motoboy
// (paridade com sz_mb_custody_route_assist em admin.php:1521).
//
// Estado: UPDATE custody SET physical_status='with_motoboy', motoboy_id=$mid,
// route_at=NOW() WHERE package_code=$qr_code.
//
// Validações:
//  1. motoboy_id existe e está ativo
//  2. qr_code corresponde a um registro
//  3. registro está em estado bipável (reserved | available)
//
// Audit insert em sz_motoboy_audit quando a tabela existir.
//
// POST /motoboy-custodia/route-assist
func (h *MotoboyCustodiaHandler) RouteAssist(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_motoboy_stock_custody") {
		httpx.Err(w, 503, "table_missing", "controle de custódia ainda não inicializado")
		return
	}

	var body routeAssistBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	qr := strings.ToUpper(strings.TrimSpace(body.QrCode))
	if qr == "" {
		httpx.Err(w, 400, "bad_request", "qr_code obrigatório")
		return
	}
	if body.MotoboyID <= 0 {
		httpx.Err(w, 400, "bad_request", "motoboy_id obrigatório")
		return
	}

	// 1) motoboy ativo.
	var motoboyAtivo bool
	err := h.Pool.QueryRow(ctx,
		`SELECT COALESCE(ativo, TRUE) FROM sz_motoboys WHERE id=$1`, body.MotoboyID).Scan(&motoboyAtivo)
	if err != nil {
		httpx.Err(w, 404, "motoboy_nao_encontrado", "motoboy não encontrado")
		return
	}
	if !motoboyAtivo {
		httpx.Err(w, 409, "motoboy_inativo", "motoboy está inativo")
		return
	}

	// 2) registro existe + estado bipável.
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer tx.Rollback(ctx)

	var custodyID, pedidoID int64
	var deStatus string
	err = tx.QueryRow(ctx,
		`SELECT id, COALESCE(pedido_id,0), COALESCE(physical_status,'')
		   FROM sz_motoboy_stock_custody
		  WHERE UPPER(package_code) = $1
		  FOR UPDATE`,
		qr).Scan(&custodyID, &pedidoID, &deStatus)
	if err != nil {
		httpx.Err(w, 404, "qr_nao_encontrado", "QR não corresponde a nenhum pacote em custódia")
		return
	}
	if deStatus != "reserved" && deStatus != "available" {
		httpx.Err(w, 409, "estado_invalido",
			"pacote está em '"+deStatus+"' — só pode iniciar rota a partir de reserved/available")
		return
	}

	// 3) UPDATE: physical_status, motoboy_id, route_at, updated_at.
	_, err = tx.Exec(ctx,
		`UPDATE sz_motoboy_stock_custody
		    SET physical_status='with_motoboy',
		        motoboy_id=$1,
		        route_at=NOW(),
		        updated_at=NOW()
		  WHERE id=$2`,
		body.MotoboyID, custodyID)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// 4) Audit (best-effort).
	if h.tableExists(ctx, "sz_motoboy_audit") {
		adm := auth.FromCtx(ctx)
		var actorID *int64
		var adminEmail, adminNome string
		if adm != nil {
			tmp := adm.ID
			actorID = &tmp
			adminEmail = adm.Email
			adminNome = adm.Nome
		}
		meta := map[string]any{
			"source":      "admin_custody_route_assist",
			"admin_email": adminEmail,
			"admin_nome":  adminNome,
			"qr_code":     qr,
			"custody_id":  custodyID,
		}
		metaJSON, _ := json.Marshal(meta)
		_, _ = tx.Exec(ctx,
			`INSERT INTO sz_motoboy_audit
			   (pedido_id, motoboy_id, actor_tipo, actor_id,
			    acao, de_status, para_status, meta_json, created_at)
			 VALUES ($1, $2, 'admin', $3, 'custody_route_assist', $4, 'with_motoboy', $5, NOW())`,
			pedidoID, body.MotoboyID, actorID, deStatus, string(metaJSON))
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 200, map[string]any{
		"ok":         true,
		"custody_id": custodyID,
	})
}

// ─── Return (devolução) ──────────────────────────────────────────────────

// Return — registra devolução/ocorrência. Multipart form (foto opcional para
// vendavel, obrigatória para conditions destrutivas).
//
// Conditions aceitas:
//
//	vendavel    → physical_status='available'
//	avariado    → physical_status='damaged'
//	extravio    → physical_status='damaged'
//	perda       → physical_status='damaged'
//	violado     → physical_status='damaged'
//	divergente  → physical_status='damaged'
//
// Validações (paridade com sz_mbc_return_by_qr):
//  1. qr_code obrigatório
//  2. condition na whitelist
//  3. note obrigatória para avariado/extravio/perda
//  4. photo obrigatória para conditions destrutivas (todas exceto vendavel)
//  5. photo se enviada: MIME image/*, limite 8MB
//
// POST /motoboy-custodia/return  (multipart/form-data)
func (h *MotoboyCustodiaHandler) Return(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_motoboy_stock_custody") {
		httpx.Err(w, 503, "table_missing", "controle de custódia ainda não inicializado")
		return
	}

	// 32 MB de teto para o multipart total (foto + campos). r.MultipartForm
	// segura tudo em memória/disco temporário até o teto.
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		httpx.Err(w, 400, "bad_request", "multipart inválido: "+err.Error())
		return
	}

	qr := strings.ToUpper(strings.TrimSpace(r.FormValue("qr_code")))
	condition := strings.ToLower(strings.TrimSpace(r.FormValue("condition")))
	note := strings.TrimSpace(r.FormValue("note"))

	if qr == "" {
		httpx.Err(w, 400, "bad_request", "qr_code obrigatório")
		return
	}

	conditionToStatus := map[string]string{
		"vendavel":   "available",
		"avariado":   "damaged",
		"extravio":   "damaged",
		"perda":      "damaged",
		"violado":    "damaged",
		"divergente": "damaged",
	}
	target, ok := conditionToStatus[condition]
	if !ok {
		httpx.Err(w, 400, "bad_request",
			"condition inválida (use: vendavel|avariado|extravio|perda|violado|divergente)")
		return
	}

	// Note obrigatória para conditions críticas (paridade com sz_mbc_note_required).
	noteRequired := map[string]bool{"avariado": true, "extravio": true, "perda": true}
	if noteRequired[condition] && note == "" {
		httpx.Err(w, 400, "note_required",
			"relato (note) obrigatório para condition '"+condition+"'")
		return
	}

	// Photo obrigatória para todas as conditions destrutivas (vendavel é opt-in).
	photoRequired := condition != "vendavel"

	// Carrega o arquivo (se enviado) e valida MIME/tamanho.
	var photoURL string
	file, header, ferr := r.FormFile("photo")
	switch {
	case ferr == nil:
		defer file.Close()
		if header.Size > 8<<20 {
			httpx.Err(w, 413, "photo_too_large", "foto excede 8MB")
			return
		}
		mime := strings.ToLower(header.Header.Get("Content-Type"))
		if !strings.HasPrefix(mime, "image/") {
			httpx.Err(w, 400, "photo_invalid", "foto precisa ser image/* (recebido: "+mime+")")
			return
		}
		url, perr := h.persistPhoto(file, header.Filename)
		if perr != nil {
			httpx.Err(w, 500, "photo_save_error", perr.Error())
			return
		}
		photoURL = url

	case errors.Is(ferr, http.ErrMissingFile):
		// Arquivo simplesmente ausente — só é erro se a condition exige foto.
		if photoRequired {
			httpx.Err(w, 400, "photo_required",
				"foto obrigatória para condition '"+condition+"'")
			return
		}

	default:
		// Upload corrompido ou outro erro do multipart parser — não confundir
		// com "ausente". Retorna 400 com a mensagem real do parser.
		httpx.Err(w, 400, "photo_invalid", "erro ao ler foto: "+ferr.Error())
		return
	}

	// Transação para append em occurrence_photos sem race com outro update.
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer tx.Rollback(ctx)

	var custodyID, pedidoID int64
	var deStatus, existingPhotosRaw string
	err = tx.QueryRow(ctx,
		`SELECT id,
		        COALESCE(pedido_id,0),
		        COALESCE(physical_status,''),
		        COALESCE(occurrence_photos,'')
		   FROM sz_motoboy_stock_custody
		  WHERE UPPER(package_code) = $1
		  FOR UPDATE`,
		qr).Scan(&custodyID, &pedidoID, &deStatus, &existingPhotosRaw)
	if err != nil {
		httpx.Err(w, 404, "qr_nao_encontrado", "QR não corresponde a nenhum pacote em custódia")
		return
	}

	// Append da foto no array existente.
	photos := decodePhotos(existingPhotosRaw)
	if photoURL != "" {
		photos = append(photos, photoURL)
	}
	photosJSON, _ := json.Marshal(photos)

	// Carimbo do timestamp por condição: damaged_at vs returned_at.
	var stampCol string
	if target == "damaged" {
		stampCol = "damaged_at"
	} else {
		stampCol = "returned_at"
	}

	// UPDATE final. Usamos format-string para o nome da coluna (whitelist garante segurança).
	// occurrence_type=condition, occurrence_note=note (mesmo se vazio para vendavel).
	updSQL := `UPDATE sz_motoboy_stock_custody
	              SET physical_status=$1,
	                  occurrence_type=$2,
	                  occurrence_note=$3,
	                  occurrence_photos=$4,
	                  ` + stampCol + `=NOW(),
	                  updated_at=NOW()
	            WHERE id=$5`
	if _, err := tx.Exec(ctx, updSQL, target, condition, note, string(photosJSON), custodyID); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// Audit best-effort.
	if h.tableExists(ctx, "sz_motoboy_audit") {
		adm := auth.FromCtx(ctx)
		var actorID *int64
		var adminEmail, adminNome string
		if adm != nil {
			tmp := adm.ID
			actorID = &tmp
			adminEmail = adm.Email
			adminNome = adm.Nome
		}
		meta := map[string]any{
			"source":      "admin_custody_return",
			"admin_email": adminEmail,
			"admin_nome":  adminNome,
			"qr_code":     qr,
			"condition":   condition,
			"custody_id":  custodyID,
			"photo":       photoURL,
		}
		metaJSON, _ := json.Marshal(meta)
		_, _ = tx.Exec(ctx,
			`INSERT INTO sz_motoboy_audit
			   (pedido_id, motoboy_id, actor_tipo, actor_id,
			    acao, de_status, para_status, meta_json, created_at)
			 VALUES ($1, NULL, 'admin', $2, 'custody_return', $3, $4, $5, NOW())`,
			pedidoID, actorID, deStatus, target, string(metaJSON))
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 200, map[string]any{
		"ok":         true,
		"custody_id": custodyID,
	})
}

// persistPhoto grava o arquivo no disco e devolve a URL pública.
// Nome: {timestamp_ns}-{sanitized_filename}{ext}. Cria o diretório se ausente.
func (h *MotoboyCustodiaHandler) persistPhoto(src io.Reader, origName string) (string, error) {
	dir := uploadDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir uploads: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(origName))
	if ext == "" || len(ext) > 8 {
		ext = ".jpg"
	}
	// Sanitiza ext — só aceita extensões comuns.
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".heic":
		// OK
	default:
		ext = ".jpg"
	}

	name := fmt.Sprintf("custody-%d%s", time.Now().UnixNano(), ext)
	full := filepath.Join(dir, name)

	dst, err := os.Create(full)
	if err != nil {
		return "", fmt.Errorf("create uploads file: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		// Limpa arquivo parcial.
		_ = os.Remove(full)
		return "", fmt.Errorf("write uploads file: %w", err)
	}
	return uploadURL() + name, nil
}
