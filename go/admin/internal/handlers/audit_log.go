// Package handlers — endpoint admin AuditLogViewer.
// Espelha a tabela senderzz_portal_audit_log (PHP legado:
// includes/senderzz-audit-log.php) sobre Postgres. Lê o histórico de ações
// realizadas por usuários do portal — aprovações, cancelamentos, suporte,
// reprocessamento de etiqueta, alteração de senha, 2FA etc.
//
// Convenções:
//   - JOIN em senderzz_portal_users.id (portal_user_id guarda o id do PORTAL,
//     não o wp_user_id — diferente de tpc_carteira que usa wp_user_id).
//   - `meta` é JSONB no Postgres (no MySQL legado é LONGTEXT JSON). Lido como
//     texto para simplificar serialização — quem renderiza decide se faz
//     pretty-print.
//   - Graceful degradation: tabelas ausentes retornam listas vazias, não 500.
package handlers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

type AuditLogHandler struct{ Pool *pgxpool.Pool }

// auditLogRow — linha exibida na listagem e no detalhe.
type auditLogRow struct {
	ID            int64   `json:"id"`
	PortalUserID  int64   `json:"portal_user_id"`
	UserNome      string  `json:"user_nome"`
	UserEmail     string  `json:"user_email"`
	Action        string  `json:"action"`
	OrderID       *int64  `json:"order_id"`
	Meta          *string `json:"meta"`
	IP            string  `json:"ip"`
	CreatedAt     string  `json:"created_at"`
}

// actionStat — contagem por action para o stats banner.
type actionStat struct {
	Action string `json:"action"`
	Count  int64  `json:"count"`
}

func (h *AuditLogHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// parseDateFilter normaliza date_from/date_to.
// Aceita YYYY-MM-DD (assume 00:00 / 23:59 UTC) ou RFC3339. Vazio = sem filtro.
func parseDateFilter(s string, endOfDay bool) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	// YYYY-MM-DD
	if t, err := time.Parse("2006-01-02", s); err == nil {
		if endOfDay {
			t = t.Add(24*time.Hour - time.Second)
		}
		return t.UTC().Format("2006-01-02 15:04:05"), true
	}
	// RFC3339 (fallback)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC().Format("2006-01-02 15:04:05"), true
	}
	return "", false
}

// List — GET /audit-log?action=&portal_user_id=&order_id=&date_from=&date_to=&page=1&per_page=50
//
// Lista paginada com JOIN em portal_users para nome/email. Ordenação fixa por
// created_at DESC. Filtros são combinados com AND (todos opcionais).
func (h *AuditLogHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	action := strings.TrimSpace(q.Get("action"))
	portalUserID, _ := strconv.ParseInt(q.Get("portal_user_id"), 10, 64)
	orderID, _ := strconv.ParseInt(q.Get("order_id"), 10, 64)
	dateFrom, hasFrom := parseDateFilter(q.Get("date_from"), false)
	dateTo, hasTo := parseDateFilter(q.Get("date_to"), true)

	perPage, _ := strconv.Atoi(q.Get("per_page"))
	if perPage <= 0 || perPage > 500 {
		perPage = 50
	}
	page, _ := strconv.Atoi(q.Get("page"))
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * perPage

	// Tabela ausente → resposta vazia, não 500.
	if !h.tableExists(ctx, "senderzz_portal_audit_log") {
		httpx.JSON(w, 200, map[string]any{
			"items":    []auditLogRow{},
			"total":    0,
			"page":     page,
			"per_page": perPage,
		})
		return
	}

	hasUsers := h.tableExists(ctx, "senderzz_portal_users")

	// Monta WHERE dinâmico. Parâmetros indexados $1.. para evitar SQL injection.
	args := []any{}
	conds := []string{}

	if action != "" {
		args = append(args, action)
		conds = append(conds, "a.action = $"+strconv.Itoa(len(args)))
	}
	if portalUserID > 0 {
		args = append(args, portalUserID)
		conds = append(conds, "a.portal_user_id = $"+strconv.Itoa(len(args)))
	}
	if orderID > 0 {
		args = append(args, orderID)
		conds = append(conds, "a.order_id = $"+strconv.Itoa(len(args)))
	}
	if hasFrom {
		args = append(args, dateFrom)
		conds = append(conds, "a.created_at >= $"+strconv.Itoa(len(args)))
	}
	if hasTo {
		args = append(args, dateTo)
		conds = append(conds, "a.created_at <= $"+strconv.Itoa(len(args)))
	}

	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	// SELECT principal — meta convertido para text para evitar parsing pgx.
	var selectCols, fromJoin string
	if hasUsers {
		selectCols = `a.id, a.portal_user_id,
		              COALESCE(u.nome,  '') AS user_nome,
		              COALESCE(u.email, '') AS user_email,
		              a.action, a.order_id,
		              a.meta::text AS meta,
		              COALESCE(a.ip,'') AS ip,
		              a.created_at::text AS created_at`
		fromJoin = `FROM senderzz_portal_audit_log a
		            LEFT JOIN senderzz_portal_users u ON u.id = a.portal_user_id`
	} else {
		selectCols = `a.id, a.portal_user_id, ''::text, ''::text,
		              a.action, a.order_id,
		              a.meta::text AS meta,
		              COALESCE(a.ip,'') AS ip,
		              a.created_at::text AS created_at`
		fromJoin = `FROM senderzz_portal_audit_log a`
	}

	// Paginação: LIMIT/OFFSET como últimos params.
	args = append(args, perPage, offset)
	listSQL := "SELECT " + selectCols + " " + fromJoin + " " + where +
		" ORDER BY a.created_at DESC, a.id DESC " +
		"LIMIT $" + strconv.Itoa(len(args)-1) +
		" OFFSET $" + strconv.Itoa(len(args))

	rows, err := h.Pool.Query(ctx, listSQL, args...)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	items := []auditLogRow{}
	for rows.Next() {
		var a auditLogRow
		if err := rows.Scan(
			&a.ID, &a.PortalUserID, &a.UserNome, &a.UserEmail,
			&a.Action, &a.OrderID, &a.Meta, &a.IP, &a.CreatedAt,
		); err != nil {
			httpx.Err(w, 500, "scan_error", err.Error())
			return
		}
		items = append(items, a)
	}

	// COUNT total — usa os mesmos filtros (sem LIMIT/OFFSET).
	countArgs := args[:len(args)-2]
	countSQL := "SELECT COUNT(*) FROM senderzz_portal_audit_log a " + where
	if hasUsers && strings.Contains(where, "u.") {
		// Defesa — atualmente nenhum filtro toca u.*, mas garante JOIN se vier futuro.
		countSQL = "SELECT COUNT(*) FROM senderzz_portal_audit_log a " +
			"LEFT JOIN senderzz_portal_users u ON u.id = a.portal_user_id " + where
	}
	var total int64
	_ = h.Pool.QueryRow(ctx, countSQL, countArgs...).Scan(&total)

	httpx.JSON(w, 200, map[string]any{
		"items":    items,
		"total":    total,
		"page":     page,
		"per_page": perPage,
	})
}

// Actions — GET /audit-log/actions
//
// Devolve a lista distinta de valores de `action` em ordem alfabética. Usada
// para popular o dropdown de filtro no front. Tabela ausente → lista vazia.
func (h *AuditLogHandler) Actions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if !h.tableExists(ctx, "senderzz_portal_audit_log") {
		httpx.JSON(w, 200, map[string]any{"items": []string{}})
		return
	}

	rows, err := h.Pool.Query(ctx,
		`SELECT DISTINCT action
		 FROM senderzz_portal_audit_log
		 WHERE COALESCE(action,'') <> ''
		 ORDER BY action ASC`)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	items := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err == nil {
			items = append(items, s)
		}
	}
	httpx.JSON(w, 200, map[string]any{"items": items})
}

// Get — GET /audit-log/{id}
//
// Detalhe de um registro. Retorna o JSON meta completo (sem truncar) para o
// modal de detalhes do front.
func (h *AuditLogHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}

	if !h.tableExists(ctx, "senderzz_portal_audit_log") {
		httpx.Err(w, 503, "tables_missing", "senderzz_portal_audit_log ainda não migrada")
		return
	}

	hasUsers := h.tableExists(ctx, "senderzz_portal_users")

	var query string
	if hasUsers {
		query = `SELECT a.id, a.portal_user_id,
		                COALESCE(u.nome,  '') AS user_nome,
		                COALESCE(u.email, '') AS user_email,
		                a.action, a.order_id,
		                a.meta::text AS meta,
		                COALESCE(a.ip,'') AS ip,
		                a.created_at::text AS created_at
		         FROM senderzz_portal_audit_log a
		         LEFT JOIN senderzz_portal_users u ON u.id = a.portal_user_id
		         WHERE a.id = $1`
	} else {
		query = `SELECT a.id, a.portal_user_id, ''::text, ''::text,
		                a.action, a.order_id,
		                a.meta::text AS meta,
		                COALESCE(a.ip,'') AS ip,
		                a.created_at::text AS created_at
		         FROM senderzz_portal_audit_log a
		         WHERE a.id = $1`
	}

	var a auditLogRow
	err = h.Pool.QueryRow(ctx, query, id).Scan(
		&a.ID, &a.PortalUserID, &a.UserNome, &a.UserEmail,
		&a.Action, &a.OrderID, &a.Meta, &a.IP, &a.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Err(w, 404, "not_found", "registro não encontrado")
		return
	}
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 200, a)
}

// Stats — GET /audit-log/stats?date_from=&date_to=
//
// Agrega contagem por action no período. Ordenado por count DESC para o banner
// mostrar os tops primeiro. Retorna também o total geral.
func (h *AuditLogHandler) Stats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	dateFrom, hasFrom := parseDateFilter(q.Get("date_from"), false)
	dateTo, hasTo := parseDateFilter(q.Get("date_to"), true)

	if !h.tableExists(ctx, "senderzz_portal_audit_log") {
		httpx.JSON(w, 200, map[string]any{
			"items": []actionStat{},
			"total": 0,
		})
		return
	}

	// WHERE dinâmico só com datas (filtros adicionais não fazem sentido para stats).
	args := []any{}
	conds := []string{}
	if hasFrom {
		args = append(args, dateFrom)
		conds = append(conds, "created_at >= $"+strconv.Itoa(len(args)))
	}
	if hasTo {
		args = append(args, dateTo)
		conds = append(conds, "created_at <= $"+strconv.Itoa(len(args)))
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	sql := `SELECT action, COUNT(*) AS cnt
	        FROM senderzz_portal_audit_log ` + where + `
	        GROUP BY action
	        ORDER BY cnt DESC, action ASC`

	rows, err := h.Pool.Query(ctx, sql, args...)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	items := []actionStat{}
	var total int64
	for rows.Next() {
		var s actionStat
		if err := rows.Scan(&s.Action, &s.Count); err == nil {
			items = append(items, s)
			total += s.Count
		}
	}

	httpx.JSON(w, 200, map[string]any{
		"items": items,
		"total": total,
	})
}
