// Package handlers — endpoint admin para fechamento de motoboy (Alan + Repasse).
//
// Espelha o fluxo do plugin WP onde "Alan" (admin financeiro) confirma o
// fechamento diário do motoboy ANTES do repasse efetivo. Pré-requisitos:
//   1) gerar/regerar fechamento (calcula totais do dia)
//   2) Alan confirma (alan_confirmou=true)
//   3) Repasse confirmado (repasse_confirmado=true)
//
// Tabela: sz_motoboy_fechamento (UNIQUE motoboy_id+data_fechamento).
// Ver AUDIT-ADMIN-WP.md §25.2.
//
// Surface:
//   GET  /motoboy-fechamento                       — lista com filtros (motoboy_id, status, range)
//   GET  /motoboy-fechamento/summary               — KPIs
//   POST /motoboy-fechamento/generate              — upsert do dia
//   POST /motoboy-fechamento/sync-wallets          — reconstrói totais de fechamentos não confirmados
//   POST /motoboy-fechamento/{id}/alan-confirmar
//   POST /motoboy-fechamento/{id}/alan-desconfirmar
//   POST /motoboy-fechamento/{id}/repasse-confirmar
//   POST /motoboy-fechamento/{id}/repasse-desconfirmar
package handlers

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

type MotoboyFechamentoHandler struct{ Pool *pgxpool.Pool }

// ─── Tipos de resposta ───────────────────────────────────────────────────

// FechamentoItem — linha de sz_motoboy_fechamento + JOIN motoboy/cd.
type FechamentoItem struct {
	ID                int64    `json:"id"`
	MotoboyID         int64    `json:"motoboy_id"`
	MotoboyNome       string   `json:"motoboy_nome"`
	CDID              *int64   `json:"cd_id"`
	CDNome            string   `json:"cd_nome"`
	DataFechamento    string   `json:"data_fechamento"`
	TotalPedidos      int      `json:"total_pedidos"`
	TotalEntregues    int      `json:"total_entregues"`
	TotalFrustrados   int      `json:"total_frustrados"`
	TotalDinheiro     float64  `json:"total_dinheiro"`
	TotalPix          float64  `json:"total_pix"`
	TotalCartao       float64  `json:"total_cartao"`
	TotalARepassar    float64  `json:"total_a_repassar"`
	RepasseConfirmado bool     `json:"repasse_confirmado"`
	RepasseTS         *string  `json:"repasse_ts"`
	AlanConfirmou     bool     `json:"alan_confirmou"`
	AlanTS            *string  `json:"alan_ts"`
	Obs               string   `json:"obs"`
	CreatedAt         string   `json:"created_at"`
}

// FechamentoSummary — KPIs do período. Campos espelham os cards do front.
type FechamentoSummary struct {
	TotalPedidos     int     `json:"total_pedidos"`
	TotalARepassar   float64 `json:"total_a_repassar"`
	PendentesAlan    int     `json:"pendentes_alan"`
	PendentesRepasse int     `json:"pendentes_repasse"`
	Finalizados      int     `json:"finalizados"`
}

// ─── Helpers ─────────────────────────────────────────────────────────────

func (h *MotoboyFechamentoHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// parseDateRange resolve from/to de query string. Default: today-7d .. today.
// Aceita formato YYYY-MM-DD; data inválida cai pro default sem erro 4xx.
func parseDateRange(r *http.Request) (string, string) {
	q := r.URL.Query()
	from := strings.TrimSpace(q.Get("from"))
	to := strings.TrimSpace(q.Get("to"))
	today := time.Now()
	if to == "" {
		to = today.Format("2006-01-02")
	}
	if from == "" {
		from = today.AddDate(0, 0, -7).Format("2006-01-02")
	}
	return from, to
}

// statusToWhere converte o filtro de status do front em fragmento SQL boolean.
// Sempre retorna expressão segura (constante). Valor "all"/vazio → no-op.
func statusToWhere(status string) string {
	switch status {
	case "pendente_alan":
		return "f.alan_confirmou = false"
	case "pendente_repasse":
		return "f.alan_confirmou = true AND f.repasse_confirmado = false"
	case "finalizados":
		return "f.repasse_confirmado = true"
	default:
		return "1=1"
	}
}

// obsAppendSQL retorna o fragmento SQL que concatena uma nova entrada em obs
// preservando o histórico. Espera $1 = nova entrada (já com tag).
//   - Se obs vazio/NULL → grava só a nova entrada.
//   - Se obs já tem conteúdo → "<obs>\n<nova>".
//   - Se entrada vazia → mantém obs atual.
func obsAppendSQL() string {
	return `obs = CASE
		WHEN NULLIF($1,'') IS NULL THEN COALESCE(obs,'')
		WHEN COALESCE(obs,'') = ''  THEN $1
		ELSE obs || E'\n' || $1
	END`
}

// ─── List + Summary ──────────────────────────────────────────────────────

// List retorna fechamentos filtrados por range/motoboy/status.
// GET /motoboy-fechamento?from=&to=&motoboy_id=&status=&limit=200
func (h *MotoboyFechamentoHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_motoboy_fechamento") {
		httpx.JSON(w, 200, map[string]any{"items": []FechamentoItem{}, "count": 0})
		return
	}

	from, to := parseDateRange(r)
	q := r.URL.Query()
	motoboyID, _ := strconv.ParseInt(q.Get("motoboy_id"), 10, 64)
	status := q.Get("status")
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 200
	}

	// Filtro de status entra como WHERE additivo (string constante segura).
	// motoboy_id=0 ignora o filtro (paridade com behaviour padrão).
	sql := `
		SELECT f.id, f.motoboy_id,
		       COALESCE(m.nome,'') AS motoboy_nome,
		       f.cd_id,
		       COALESCE(c.nome,'') AS cd_nome,
		       f.data_fechamento::text,
		       COALESCE(f.total_pedidos,0),
		       COALESCE(f.total_entregues,0),
		       COALESCE(f.total_frustrados,0),
		       COALESCE(f.total_dinheiro,0),
		       COALESCE(f.total_pix,0),
		       COALESCE(f.total_cartao,0),
		       COALESCE(f.total_a_repassar,0),
		       COALESCE(f.repasse_confirmado,false),
		       f.repasse_ts::text,
		       COALESCE(f.alan_confirmou,false),
		       f.alan_ts::text,
		       COALESCE(f.obs,''),
		       f.created_at::text
		FROM sz_motoboy_fechamento f
		LEFT JOIN sz_motoboys    m ON m.id = f.motoboy_id
		LEFT JOIN sz_motoboy_cds c ON c.id = f.cd_id
		WHERE f.data_fechamento BETWEEN $1::date AND $2::date
		  AND ($3 = 0 OR f.motoboy_id = $3)
		  AND ` + statusToWhere(status) + `
		ORDER BY f.data_fechamento DESC, f.id DESC
		LIMIT $4`

	rows, err := h.Pool.Query(ctx, sql, from, to, motoboyID, limit)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []FechamentoItem{}
	for rows.Next() {
		var it FechamentoItem
		_ = rows.Scan(
			&it.ID, &it.MotoboyID, &it.MotoboyNome,
			&it.CDID, &it.CDNome,
			&it.DataFechamento,
			&it.TotalPedidos, &it.TotalEntregues, &it.TotalFrustrados,
			&it.TotalDinheiro, &it.TotalPix, &it.TotalCartao, &it.TotalARepassar,
			&it.RepasseConfirmado, &it.RepasseTS,
			&it.AlanConfirmou, &it.AlanTS,
			&it.Obs, &it.CreatedAt,
		)
		out = append(out, it)
	}
	httpx.JSON(w, 200, map[string]any{"items": out, "count": len(out)})
}

// Summary calcula KPIs do período. Tabela ausente → zeros.
// GET /motoboy-fechamento/summary?from=&to=
func (h *MotoboyFechamentoHandler) Summary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := FechamentoSummary{}

	if !h.tableExists(ctx, "sz_motoboy_fechamento") {
		httpx.JSON(w, 200, out)
		return
	}

	from, to := parseDateRange(r)

	// Single round-trip: SUM/COUNT condicionais via FILTER.
	err := h.Pool.QueryRow(ctx,
		`SELECT
		   COALESCE(SUM(total_pedidos),0)        AS total_pedidos,
		   COALESCE(SUM(total_a_repassar),0)     AS total_a_repassar,
		   COUNT(*) FILTER (WHERE alan_confirmou = false) AS pendentes_alan,
		   COUNT(*) FILTER (WHERE alan_confirmou = true  AND repasse_confirmado = false) AS pendentes_repasse,
		   COUNT(*) FILTER (WHERE repasse_confirmado = true) AS finalizados
		 FROM sz_motoboy_fechamento
		 WHERE data_fechamento BETWEEN $1::date AND $2::date`,
		from, to,
	).Scan(
		&out.TotalPedidos, &out.TotalARepassar,
		&out.PendentesAlan, &out.PendentesRepasse, &out.Finalizados,
	)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	httpx.JSON(w, 200, out)
}

// ─── Confirmações (Alan + Repasse) ───────────────────────────────────────

// AlanConfirmar — marca alan_confirmou=true, append obs.
// POST /motoboy-fechamento/{id}/alan-confirmar  body: {obs}
func (h *MotoboyFechamentoHandler) AlanConfirmar(w http.ResponseWriter, r *http.Request) {
	id, ok := parseFechamentoID(r)
	if !ok {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_motoboy_fechamento") {
		httpx.Err(w, 503, "table_missing", "tabela sz_motoboy_fechamento não migrada")
		return
	}

	var body struct {
		Obs string `json:"obs"`
	}
	_ = httpx.DecodeJSON(r, &body) // obs é opcional

	// Tag no obs facilita auditoria visual posterior.
	obsTag := ""
	if strings.TrimSpace(body.Obs) != "" {
		obsTag = "[ALAN OK] " + strings.TrimSpace(body.Obs)
	} else {
		obsTag = "[ALAN OK]"
	}

	tag, err := h.Pool.Exec(ctx,
		`UPDATE sz_motoboy_fechamento
		 SET alan_confirmou = true,
		     alan_ts = NOW(),
		     `+obsAppendSQL()+`,
		     updated_at = NOW()
		 WHERE id = $2`,
		obsTag, id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.Err(w, 404, "not_found", "fechamento não encontrado")
		return
	}
	httpx.JSON(w, 200, map[string]any{"ok": true, "id": id})
}

// AlanDesconfirmar — reverte confirmação de Alan. Bloqueado se repasse já confirmado.
// POST /motoboy-fechamento/{id}/alan-desconfirmar  body: {motivo}
func (h *MotoboyFechamentoHandler) AlanDesconfirmar(w http.ResponseWriter, r *http.Request) {
	id, ok := parseFechamentoID(r)
	if !ok {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_motoboy_fechamento") {
		httpx.Err(w, 503, "table_missing", "tabela sz_motoboy_fechamento não migrada")
		return
	}
	var body struct {
		Motivo string `json:"motivo"`
	}
	_ = httpx.DecodeJSON(r, &body)

	// Guard: não pode desconfirmar Alan se o repasse já foi confirmado.
	var repasseConfirmado bool
	err := h.Pool.QueryRow(ctx,
		`SELECT COALESCE(repasse_confirmado,false) FROM sz_motoboy_fechamento WHERE id=$1`, id,
	).Scan(&repasseConfirmado)
	if err != nil {
		httpx.Err(w, 404, "not_found", "fechamento não encontrado")
		return
	}
	if repasseConfirmado {
		httpx.Err(w, 409, "invalid_state", "repasse já confirmado — desconfirme o repasse antes")
		return
	}

	motivo := strings.TrimSpace(body.Motivo)
	tag := "[DESCONFIRMADO ALAN]"
	if motivo != "" {
		tag = "[DESCONFIRMADO ALAN: " + motivo + "]"
	}

	res, err := h.Pool.Exec(ctx,
		`UPDATE sz_motoboy_fechamento
		 SET alan_confirmou = false,
		     alan_ts = NULL,
		     `+obsAppendSQL()+`,
		     updated_at = NOW()
		 WHERE id = $2`, tag, id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if res.RowsAffected() == 0 {
		httpx.Err(w, 404, "not_found", "fechamento não encontrado")
		return
	}
	httpx.JSON(w, 200, map[string]any{"ok": true, "id": id})
}

// RepasseConfirmar — marca repasse_confirmado=true (exige Alan já confirmado).
// POST /motoboy-fechamento/{id}/repasse-confirmar  body: {obs}
func (h *MotoboyFechamentoHandler) RepasseConfirmar(w http.ResponseWriter, r *http.Request) {
	id, ok := parseFechamentoID(r)
	if !ok {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_motoboy_fechamento") {
		httpx.Err(w, 503, "table_missing", "tabela sz_motoboy_fechamento não migrada")
		return
	}
	var body struct {
		Obs string `json:"obs"`
	}
	_ = httpx.DecodeJSON(r, &body)

	// Pré-requisito: Alan precisa ter confirmado antes.
	var alanOK bool
	err := h.Pool.QueryRow(ctx,
		`SELECT COALESCE(alan_confirmou,false) FROM sz_motoboy_fechamento WHERE id=$1`, id,
	).Scan(&alanOK)
	if err != nil {
		httpx.Err(w, 404, "not_found", "fechamento não encontrado")
		return
	}
	if !alanOK {
		httpx.Err(w, 409, "invalid_state", "Alan precisa confirmar antes do repasse")
		return
	}

	obsTag := "[REPASSE OK]"
	if t := strings.TrimSpace(body.Obs); t != "" {
		obsTag = "[REPASSE OK] " + t
	}

	tag, err := h.Pool.Exec(ctx,
		`UPDATE sz_motoboy_fechamento
		 SET repasse_confirmado = true,
		     repasse_ts = NOW(),
		     `+obsAppendSQL()+`,
		     updated_at = NOW()
		 WHERE id = $2`, obsTag, id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.Err(w, 404, "not_found", "fechamento não encontrado")
		return
	}
	httpx.JSON(w, 200, map[string]any{"ok": true, "id": id})
}

// RepasseDesconfirmar — reverte repasse. Útil para corrigir lançamento.
// POST /motoboy-fechamento/{id}/repasse-desconfirmar  body: {motivo}
func (h *MotoboyFechamentoHandler) RepasseDesconfirmar(w http.ResponseWriter, r *http.Request) {
	id, ok := parseFechamentoID(r)
	if !ok {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_motoboy_fechamento") {
		httpx.Err(w, 503, "table_missing", "tabela sz_motoboy_fechamento não migrada")
		return
	}
	var body struct {
		Motivo string `json:"motivo"`
	}
	_ = httpx.DecodeJSON(r, &body)

	motivo := strings.TrimSpace(body.Motivo)
	tag := "[DESCONFIRMADO REPASSE]"
	if motivo != "" {
		tag = "[DESCONFIRMADO REPASSE: " + motivo + "]"
	}

	res, err := h.Pool.Exec(ctx,
		`UPDATE sz_motoboy_fechamento
		 SET repasse_confirmado = false,
		     repasse_ts = NULL,
		     `+obsAppendSQL()+`,
		     updated_at = NOW()
		 WHERE id = $2`, tag, id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if res.RowsAffected() == 0 {
		httpx.Err(w, 404, "not_found", "fechamento não encontrado")
		return
	}
	httpx.JSON(w, 200, map[string]any{"ok": true, "id": id})
}

// ─── Generate (UPSERT) ───────────────────────────────────────────────────

// Generate calcula totais do dia + UPSERT por (motoboy_id, data_fechamento).
// POST /motoboy-fechamento/generate  body: {motoboy_id, data_fechamento}
//
// Atenção: NÃO regera se alan_confirmou=true (protege fechamento auditado).
// As colunas de método de pagamento (total_dinheiro/pix/cartao) não são
// inferidas de sz_motoboy_pedidos pois o esquema dessa tabela não
// expõe o método de pagamento de forma estável entre versões — totais por
// método ficam zerados no upsert automático e podem ser preenchidos depois
// por flow específico.
func (h *MotoboyFechamentoHandler) Generate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_motoboy_fechamento") {
		httpx.Err(w, 503, "table_missing", "tabela sz_motoboy_fechamento não migrada")
		return
	}
	if !h.tableExists(ctx, "sz_motoboy_pedidos") {
		httpx.Err(w, 503, "table_missing", "tabela sz_motoboy_pedidos não migrada")
		return
	}

	var body struct {
		MotoboyID      int64  `json:"motoboy_id"`
		DataFechamento string `json:"data_fechamento"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	if body.MotoboyID <= 0 {
		httpx.Err(w, 400, "bad_request", "motoboy_id obrigatório")
		return
	}
	data := strings.TrimSpace(body.DataFechamento)
	if data == "" {
		data = time.Now().Format("2006-01-02")
	}

	// Guard: se já existe fechamento com Alan confirmado, bloqueia regeração.
	var alanOK bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT COALESCE(alan_confirmou,false)
		 FROM sz_motoboy_fechamento
		 WHERE motoboy_id=$1 AND data_fechamento=$2::date`,
		body.MotoboyID, data,
	).Scan(&alanOK)
	if alanOK {
		httpx.Err(w, 409, "alan_locked", "fechamento já confirmado por Alan; desconfirme antes de regerar")
		return
	}

	// Descobre cd_id padrão a partir do motoboy.
	var cdID *int64
	_ = h.Pool.QueryRow(ctx,
		`SELECT cd_id FROM sz_motoboys WHERE id=$1`, body.MotoboyID,
	).Scan(&cdID)

	// Totais agregados a partir de sz_motoboy_pedidos do dia.
	// Considera "entregue" como crédito a repassar; outros status apenas contam.
	var (
		totalPedidos    int
		totalEntregues  int
		totalFrustrados int
		totalARepassar  float64
	)
	err := h.Pool.QueryRow(ctx,
		`SELECT
		   COUNT(*) AS total_pedidos,
		   COUNT(*) FILTER (WHERE status = 'entregue')  AS total_entregues,
		   COUNT(*) FILTER (WHERE status = 'frustrado') AS total_frustrados,
		   COALESCE(SUM(valor) FILTER (WHERE status = 'entregue'), 0) AS total_a_repassar
		 FROM sz_motoboy_pedidos
		 WHERE motoboy_id = $1
		   AND created_at::date = $2::date`,
		body.MotoboyID, data,
	).Scan(&totalPedidos, &totalEntregues, &totalFrustrados, &totalARepassar)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// UPSERT: chave única (motoboy_id, data_fechamento).
	// Splits de método de pagamento ficam em 0 (ver doc acima).
	var id int64
	err = h.Pool.QueryRow(ctx,
		`INSERT INTO sz_motoboy_fechamento (
		   motoboy_id, cd_id, data_fechamento,
		   total_pedidos, total_entregues, total_frustrados,
		   total_dinheiro, total_pix, total_cartao, total_a_repassar,
		   repasse_confirmado, alan_confirmou,
		   created_at, updated_at
		 ) VALUES (
		   $1, $2, $3::date,
		   $4, $5, $6,
		   0, 0, 0, $7,
		   false, false,
		   NOW(), NOW()
		 )
		 ON CONFLICT (motoboy_id, data_fechamento) DO UPDATE SET
		   total_pedidos    = EXCLUDED.total_pedidos,
		   total_entregues  = EXCLUDED.total_entregues,
		   total_frustrados = EXCLUDED.total_frustrados,
		   total_a_repassar = EXCLUDED.total_a_repassar,
		   cd_id            = COALESCE(EXCLUDED.cd_id, sz_motoboy_fechamento.cd_id),
		   updated_at       = NOW()
		 RETURNING id`,
		body.MotoboyID, cdID, data,
		totalPedidos, totalEntregues, totalFrustrados,
		totalARepassar,
	).Scan(&id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 200, map[string]any{
		"ok":               true,
		"id":               id,
		"motoboy_id":       body.MotoboyID,
		"data_fechamento":  data,
		"total_pedidos":    totalPedidos,
		"total_entregues":  totalEntregues,
		"total_frustrados": totalFrustrados,
		"total_a_repassar": totalARepassar,
	})
}

// SyncWallets reconstrói os totais de fechamentos NÃO confirmados por Alan a partir
// de sz_motoboy_pedidos (equivalente ao sz_mbw_sync_all_data do plugin WP).
// Atualiza: total_pedidos, total_entregues, total_frustrados, total_a_repassar,
//           total_dinheiro (pgto_dinheiro), total_pix (pgto_pix), total_cartao (pgto_cartao).
//
// Guard: nunca sobrescreve fechamentos com alan_confirmou=true.
// POST /motoboy-fechamento/sync-wallets  body: {from?, to?}
func (h *MotoboyFechamentoHandler) SyncWallets(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_motoboy_fechamento") {
		httpx.Err(w, 503, "table_missing", "tabela sz_motoboy_fechamento não migrada")
		return
	}
	if !h.tableExists(ctx, "sz_motoboy_pedidos") {
		httpx.Err(w, 503, "table_missing", "tabela sz_motoboy_pedidos não migrada")
		return
	}

	var body struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	_ = httpx.DecodeJSON(r, &body) // campos opcionais

	// Usa parseDateRange como fallback para o range padrão.
	from, to := body.From, body.To
	if from == "" || to == "" {
		from, to = parseDateRange(r)
	}

	// Busca todos os fechamentos NÃO confirmados por Alan no período.
	rows, err := h.Pool.Query(ctx,
		`SELECT id, motoboy_id, data_fechamento::text
		 FROM sz_motoboy_fechamento
		 WHERE data_fechamento BETWEEN $1::date AND $2::date
		   AND COALESCE(alan_confirmou, false) = false`,
		from, to,
	)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	type fechRow struct {
		id             int64
		motoboyID      int64
		dataFechamento string
	}
	var fechamentos []fechRow
	for rows.Next() {
		var row fechRow
		if err := rows.Scan(&row.id, &row.motoboyID, &row.dataFechamento); err == nil {
			fechamentos = append(fechamentos, row)
		}
	}
	rows.Close()

	// Para cada fechamento, recalcula os totais a partir dos pedidos do dia.
	updated := 0
	for _, f := range fechamentos {
		var (
			totalPedidos    int
			totalEntregues  int
			totalFrustrados int
			totalARepassar  float64
			totalDinheiro   float64
			totalPix        float64
			totalCartao     float64
		)
		err := h.Pool.QueryRow(ctx,
			`SELECT
			   COUNT(*) AS total_pedidos,
			   COUNT(*) FILTER (WHERE status = 'entregue')  AS total_entregues,
			   COUNT(*) FILTER (WHERE status = 'frustrado') AS total_frustrados,
			   COALESCE(SUM(valor) FILTER (WHERE status = 'entregue'), 0) AS total_a_repassar,
			   COALESCE(SUM(pgto_dinheiro) FILTER (WHERE status = 'entregue'), 0) AS total_dinheiro,
			   COALESCE(SUM(pgto_pix)     FILTER (WHERE status = 'entregue'), 0) AS total_pix,
			   COALESCE(SUM(pgto_cartao)  FILTER (WHERE status = 'entregue'), 0) AS total_cartao
			 FROM sz_motoboy_pedidos
			 WHERE motoboy_id = $1
			   AND created_at::date = $2::date`,
			f.motoboyID, f.dataFechamento,
		).Scan(
			&totalPedidos, &totalEntregues, &totalFrustrados,
			&totalARepassar, &totalDinheiro, &totalPix, &totalCartao,
		)
		if err != nil {
			continue // degradação: pula esse fechamento sem abortar o lote
		}

		_, _ = h.Pool.Exec(ctx,
			`UPDATE sz_motoboy_fechamento SET
			   total_pedidos    = $1,
			   total_entregues  = $2,
			   total_frustrados = $3,
			   total_a_repassar = $4,
			   total_dinheiro   = $5,
			   total_pix        = $6,
			   total_cartao     = $7,
			   updated_at       = NOW()
			 WHERE id = $8`,
			totalPedidos, totalEntregues, totalFrustrados,
			totalARepassar, totalDinheiro, totalPix, totalCartao,
			f.id,
		)
		updated++
	}

	httpx.JSON(w, 200, map[string]any{
		"ok":               true,
		"fechamentos_sync": updated,
		"range_from":       from,
		"range_to":         to,
		"hint":             "Apenas fechamentos sem confirmação de Alan foram recalculados.",
	})
}

// parseFechamentoID extrai chi URLParam "id" como int64 positivo.
// Função local porque parseIDParam vive em cod_saques.go (mesmo package).
func parseFechamentoID(r *http.Request) (int64, bool) {
	s := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}
