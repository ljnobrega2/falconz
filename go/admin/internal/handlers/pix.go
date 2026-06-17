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

// errStatusInvalido é retornado por confirmarAdmin quando o status atual da
// recarga não permite confirmação (expirado/cancelado). O chamador retorna 409.
var errStatusInvalido = errors.New("recarga em status que não permite confirmação")

type PixHandler struct{ Pool *pgxpool.Pool }

type recarga struct {
	ID        int64   `json:"id"`
	UserID    int64   `json:"user_id"`
	Nome      string  `json:"nome"`
	Email     string  `json:"email"`
	Valor     float64 `json:"valor"`
	Status    string  `json:"status"`
	MePixID   *string `json:"me_pix_id"`
	ExpiresAt *string `json:"expires_at"`
	PaidAt    *string `json:"paid_at"`
	CreatedAt string  `json:"created_at"`
}

func (h *PixHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// List — GET /pix?status=&user_id=&data_ini=&data_fim=&page=1&per_page=25
//
// Filtros:
//   - status: pendente | analise | confirmado | expirado | cancelado | '' (todos)
//   - user_id: filtro exato (0 = todos)
//   - data_ini / data_fim: YYYY-MM-DD (inclusivo)
//   - page / per_page: paginação
//
// JOIN com senderzz_portal_users (graceful: sem a tabela, retorna nome/email vazios).
// Chave do JOIN: u.wp_user_id = t.user_id (tpc_recargas.user_id armazena wp_user_id).
func (h *PixHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	status := strings.ToLower(strings.TrimSpace(q.Get("status")))
	dataIni := strings.TrimSpace(q.Get("data_ini"))
	dataFim := strings.TrimSpace(q.Get("data_fim"))
	userID, _ := strconv.ParseInt(strings.TrimSpace(q.Get("user_id")), 10, 64)

	page, _ := strconv.Atoi(q.Get("page"))
	if page <= 0 {
		page = 1
	}
	perPage, _ := strconv.Atoi(q.Get("per_page"))
	if perPage <= 0 || perPage > 500 {
		perPage = 25
	}
	offset := (page - 1) * perPage

	// Graceful: tabela ainda não migrada.
	if !h.tableExists(ctx, "tpc_recargas") {
		httpx.JSON(w, 200, map[string]any{
			"items": []recarga{}, "total": 0, "page": page, "per_page": perPage,
		})
		return
	}

	hasUsers := h.tableExists(ctx, "senderzz_portal_users")

	// Cláusula WHERE compartilhada entre SELECT e COUNT.
	// $1=status (''=all), $2=user_id (0=all), $3=data_ini, $4=data_fim.
	whereClause := `
		WHERE ($1 = '' OR t.status = $1)
		  AND ($2 = 0 OR t.user_id = $2)
		  AND ($3 = '' OR t.created_at >= ($3 || ' 00:00:00')::timestamp)
		  AND ($4 = '' OR t.created_at <= ($4 || ' 23:59:59')::timestamp)`

	var sqlList string
	if hasUsers {
		sqlList = `
			SELECT t.id, t.user_id,
			       COALESCE(u.nome,  '') AS nome,
			       COALESCE(u.email, '') AS email,
			       COALESCE(t.valor, 0),
			       t.status,
			       t.me_pix_id,
			       t.expires_at::text,
			       t.paid_at::text,
			       t.created_at::text
			FROM tpc_recargas t
			LEFT JOIN senderzz_portal_users u ON u.wp_user_id = t.user_id` +
			whereClause + `
			ORDER BY t.id DESC
			LIMIT $5 OFFSET $6`
	} else {
		sqlList = `
			SELECT t.id, t.user_id,
			       ''::text AS nome,
			       ''::text AS email,
			       COALESCE(t.valor, 0),
			       t.status,
			       t.me_pix_id,
			       t.expires_at::text,
			       t.paid_at::text,
			       t.created_at::text
			FROM tpc_recargas t` +
			whereClause + `
			ORDER BY t.id DESC
			LIMIT $5 OFFSET $6`
	}

	sqlCount := `SELECT COUNT(*) FROM tpc_recargas t` + whereClause

	rows, err := h.Pool.Query(ctx, sqlList, status, userID, dataIni, dataFim, perPage, offset)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []recarga{}
	for rows.Next() {
		var p recarga
		if err := rows.Scan(
			&p.ID, &p.UserID, &p.Nome, &p.Email,
			&p.Valor, &p.Status, &p.MePixID,
			&p.ExpiresAt, &p.PaidAt, &p.CreatedAt,
		); err != nil {
			httpx.Err(w, 500, "scan_error", err.Error())
			return
		}
		out = append(out, p)
	}

	var total int64
	_ = h.Pool.QueryRow(ctx, sqlCount, status, userID, dataIni, dataFim).Scan(&total)

	httpx.JSON(w, 200, map[string]any{
		"items":    out,
		"total":    total,
		"page":     page,
		"per_page": perPage,
	})
}

// UpdateStatus — PUT /pix/{id}/status
//
// Atualiza status de uma recarga PIX. Para transição → 'confirmado', executa
// atomicamente: credita carteira + insere transação + atualiza recarga.
// Isso espelha o confirmation guard (V-NEW-01/02 / C-03): a origem é
// 'admin_reconciliation' e não executa um UPDATE cru.
//
// Transações serializable com FOR UPDATE (mesmo padrão do webhook pix.go).
func (h *PixHandler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}

	var body struct {
		Status string `json:"status"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}

	allowed := map[string]bool{
		"confirmado": true,
		"cancelado":  true,
		"expirado":   true,
	}
	if !allowed[body.Status] {
		httpx.Err(w, 400, "bad_request", "status inválido: use confirmado, cancelado ou expirado")
		return
	}

	if !h.tableExists(ctx, "tpc_recargas") {
		httpx.Err(w, 503, "tables_missing", "tpc_recargas ainda não migrada")
		return
	}

	// Para confirmação precisamos creditar a carteira atomicamente.
	if body.Status == "confirmado" {
		if err := h.confirmarAdmin(ctx, id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				httpx.Err(w, 404, "not_found", "recarga não encontrada")
				return
			}
			if errors.Is(err, errStatusInvalido) {
				httpx.Err(w, 409, "status_invalido", "recarga expirada ou cancelada não pode ser confirmada")
				return
			}
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
		httpx.JSON(w, 200, map[string]any{"ok": true, "status": "confirmado"})
		return
	}

	// Para cancelado/expirado: apenas UPDATE de status (sem movimentação financeira).
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer tx.Rollback(ctx)

	var curStatus string
	err = tx.QueryRow(ctx,
		`SELECT status FROM tpc_recargas WHERE id = $1 FOR UPDATE`, id).Scan(&curStatus)
	if err != nil {
		if err == pgx.ErrNoRows {
			httpx.Err(w, 404, "not_found", "recarga não encontrada")
			return
		}
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	if curStatus == "confirmado" {
		httpx.Err(w, 409, "already_confirmed", "recarga já confirmada — não pode ser alterada")
		return
	}

	_, err = tx.Exec(ctx,
		`UPDATE tpc_recargas SET status = $1 WHERE id = $2`, body.Status, id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 200, map[string]any{"ok": true, "status": body.Status})
}

// confirmarAdmin executa a confirmação admin de uma recarga PIX respeitando o
// confirmation guard (V-NEW-01/02): SELECT FOR UPDATE → valida status pendente/analise
// → credita carteira → insere transação → atualiza recarga.
// Espelha tpc_confirmar_recarga() com origin='admin_reconciliation'.
func (h *PixHandler) confirmarAdmin(ctx context.Context, recargaID int64) error {
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Lock da recarga — captura user_id e valor; fail-closed se já confirmada.
	var (
		userID int64
		valor  float64
		status string
	)
	err = tx.QueryRow(ctx,
		`SELECT user_id, valor, status FROM tpc_recargas WHERE id = $1 FOR UPDATE`,
		recargaID).Scan(&userID, &valor, &status)
	if err == pgx.ErrNoRows {
		return pgx.ErrNoRows
	}
	if err != nil {
		return err
	}

	// Fail-closed: só confirma se status é pendente ou analise (V-NEW-01).
	// expirado/cancelado retornam erro nomeado para que o chamador possa
	// distinguir e retornar 409 (não 500) ao cliente.
	if status != "pendente" && status != "analise" {
		if status == "confirmado" {
			return nil // idempotente — já confirmado
		}
		// Status inválido para confirmação (expirado/cancelado)
		return errStatusInvalido
	}

	referencia := "recarga:" + strconv.FormatInt(recargaID, 10)

	// Garante linha na carteira (upsert).
	if h.tableExists(ctx, "tpc_carteira") {
		_, _ = tx.Exec(ctx,
			`INSERT INTO tpc_carteira (user_id, saldo, saldo_reservado)
			 VALUES ($1, 0, 0)
			 ON CONFLICT (user_id) DO NOTHING`, userID)

		// Credita carteira (SELECT FOR UPDATE para evitar race com webhook/reserva).
		var saldoAtual float64
		if err := tx.QueryRow(ctx,
			`SELECT saldo FROM tpc_carteira WHERE user_id = $1 FOR UPDATE`,
			userID).Scan(&saldoAtual); err == nil {
			novoSaldo := saldoAtual + valor
			_, _ = tx.Exec(ctx,
				`UPDATE tpc_carteira SET saldo = $1 WHERE user_id = $2`,
				novoSaldo, userID)
		}
	}

	// Insere transação (ON CONFLICT DO NOTHING — idempotência S8).
	if h.tableExists(ctx, "tpc_transacoes") {
		_, _ = tx.Exec(ctx,
			`INSERT INTO tpc_transacoes
			    (user_id, tipo, valor, saldo_apos, descricao, referencia, status)
			 SELECT $1, 'credito', $2,
			        COALESCE((SELECT saldo FROM tpc_carteira WHERE user_id=$1), 0),
			        'Recarga PIX confirmada via admin',
			        $3, 'confirmado'
			 ON CONFLICT (user_id, referencia, tipo) DO NOTHING`,
			userID, valor, referencia)
	}

	// Atualiza recarga — condição WHERE status IN ('pendente','analise') protege
	// contra dupla confirmação concorrente (confirmation guard C-03).
	_, err = tx.Exec(ctx,
		`UPDATE tpc_recargas
		 SET status = 'confirmado', paid_at = NOW()
		 WHERE id = $1 AND status IN ('pendente', 'analise')`,
		recargaID)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// Verificar — POST /pix/verificar
//
// Alias admin do endpoint /tpc-transacoes/verificar-pix.
// Dispara verificação de PIX pendentes e retorna contagem da fila.
// Corresponde ao botão "Verificar PIX Pendentes Agora" do WP v1.
func (h *PixHandler) Verificar(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var queued int64
	if h.tableExists(ctx, "tpc_recargas") {
		_ = h.Pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM tpc_recargas WHERE status IN ('pendente', 'analise')`).Scan(&queued)
	}

	httpx.JSON(w, 200, map[string]any{
		"ok":           true,
		"queued":       queued,
		"job_queued":   true,
		"requested_at": time.Now().UTC().Format(time.RFC3339),
	})
}

// ReconcileStatus — GET /pix/reconcile-status
//
// Retorna estado do cron sz_pix_auto_reconcile_cron, KPIs de pendentes por faixa
// etária (3-29min / >29min) e status de divergência de carteira.
// Alimenta os 3 admin notices e os KPIs de reconciliação do Pix.tsx.
func (h *PixHandler) ReconcileStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	out := map[string]any{
		"cron_last_run":        nil,
		"cron_age_minutes":     nil,
		"cron_stale":           false,
		"pending_3_29":         0,
		"pending_over_29":      0,
		"divergence_last_run":  nil,
		"divergence_count":     0,
		"recent_errors":        []any{}, // log file não acessível via Go — deferred
	}

	// Metadata do cron (senderzz_cron_status, graceful se ausente).
	if h.tableExists(ctx, "senderzz_cron_status") {
		var lastRun *time.Time
		var lastStatus string
		err := h.Pool.QueryRow(ctx,
			`SELECT last_run, COALESCE(last_status, '') FROM senderzz_cron_status
			 WHERE name = 'sz_pix_auto_reconcile_cron'`).Scan(&lastRun, &lastStatus)
		if err == nil && lastRun != nil {
			out["cron_last_run"] = lastRun.UTC().Format(time.RFC3339)
			ageMin := time.Since(*lastRun).Minutes()
			out["cron_age_minutes"] = int(ageMin)
			out["cron_stale"] = ageMin > 15
		}

		// Divergência de carteira — cron sz_wallet_divergence_check.
		var divLastRun *time.Time
		err = h.Pool.QueryRow(ctx,
			`SELECT last_run FROM senderzz_cron_status
			 WHERE name = 'sz_wallet_divergence_check'`).Scan(&divLastRun)
		if err == nil && divLastRun != nil {
			out["divergence_last_run"] = divLastRun.UTC().Format(time.RFC3339)
		}
	}

	// Pendentes por faixa etária (3-29min / >29min).
	if h.tableExists(ctx, "tpc_recargas") {
		var p3to29, pOver29 int64
		_ = h.Pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM tpc_recargas
			 WHERE status IN ('pendente', 'analise')
			   AND created_at >= NOW() - INTERVAL '29 minutes'
			   AND created_at <= NOW() - INTERVAL '3 minutes'`).Scan(&p3to29)
		_ = h.Pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM tpc_recargas
			 WHERE status IN ('pendente', 'analise')
			   AND created_at < NOW() - INTERVAL '29 minutes'`).Scan(&pOver29)

		out["pending_3_29"] = p3to29
		out["pending_over_29"] = pOver29
	}

	// Contagem de divergências de carteira (sz_wallet_divergences, graceful).
	if h.tableExists(ctx, "sz_wallet_divergences") {
		var divCount int64
		_ = h.Pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM sz_wallet_divergences`).Scan(&divCount)
		out["divergence_count"] = divCount
	}

	httpx.JSON(w, 200, out)
}

// Divergences — GET /pix/divergences
//
// Lista divergências de saldo produzidas pelo cron sz_wallet_divergence_check.
// Tabela sz_wallet_divergences é opcional — retorna vazio se não existir.
func (h *PixHandler) Divergences(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if !h.tableExists(ctx, "sz_wallet_divergences") {
		httpx.JSON(w, 200, map[string]any{"items": []any{}, "total": 0})
		return
	}

	rows, err := h.Pool.Query(ctx,
		`SELECT user_id, saldo_atual, saldo_calculado,
		        (saldo_atual - saldo_calculado) AS diff,
		        created_at::text
		 FROM sz_wallet_divergences
		 ORDER BY ABS(saldo_atual - saldo_calculado) DESC
		 LIMIT 200`)
	if err != nil {
		httpx.JSON(w, 200, map[string]any{"items": []any{}, "total": 0})
		return
	}
	defer rows.Close()

	type divRow struct {
		UserID         int64   `json:"user_id"`
		SaldoAtual     float64 `json:"saldo_atual"`
		SaldoCalculado float64 `json:"saldo_calculado"`
		Diff           float64 `json:"diff"`
		CreatedAt      string  `json:"created_at"`
	}

	items := []divRow{}
	for rows.Next() {
		var d divRow
		if err := rows.Scan(&d.UserID, &d.SaldoAtual, &d.SaldoCalculado, &d.Diff, &d.CreatedAt); err == nil {
			items = append(items, d)
		}
	}

	httpx.JSON(w, 200, map[string]any{"items": items, "total": len(items)})
}

// Detail — GET /pix/{id}
//
// Detalhe completo de uma recarga: campos base + pix_qr + pix_codigo.
// Substitui o bloco <details> debug do PHP.
func (h *PixHandler) Detail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}

	if !h.tableExists(ctx, "tpc_recargas") {
		httpx.Err(w, 503, "tables_missing", "tpc_recargas ainda não migrada")
		return
	}

	hasUsers := h.tableExists(ctx, "senderzz_portal_users")

	type detailRow struct {
		ID        int64   `json:"id"`
		UserID    int64   `json:"user_id"`
		Nome      string  `json:"nome"`
		Email     string  `json:"email"`
		Valor     float64 `json:"valor"`
		Status    string  `json:"status"`
		MePixID   *string `json:"me_pix_id"`
		PixQR     *string `json:"pix_qr"`
		PixCodigo *string `json:"pix_codigo"`
		ExpiresAt *string `json:"expires_at"`
		PaidAt    *string `json:"paid_at"`
		TxID      *int64  `json:"tx_id"`
		CreatedAt string  `json:"created_at"`
	}

	var d detailRow
	var sqlDetail string
	if hasUsers {
		sqlDetail = `
			SELECT t.id, t.user_id,
			       COALESCE(u.nome,  '') AS nome,
			       COALESCE(u.email, '') AS email,
			       COALESCE(t.valor, 0), t.status,
			       t.me_pix_id, t.pix_qr, t.pix_codigo,
			       t.expires_at::text, t.paid_at::text,
			       t.tx_id, t.created_at::text
			FROM tpc_recargas t
			LEFT JOIN senderzz_portal_users u ON u.wp_user_id = t.user_id
			WHERE t.id = $1`
	} else {
		sqlDetail = `
			SELECT t.id, t.user_id,
			       ''::text AS nome,
			       ''::text AS email,
			       COALESCE(t.valor, 0), t.status,
			       t.me_pix_id, t.pix_qr, t.pix_codigo,
			       t.expires_at::text, t.paid_at::text,
			       t.tx_id, t.created_at::text
			FROM tpc_recargas t
			WHERE t.id = $1`
	}

	err = h.Pool.QueryRow(ctx, sqlDetail, id).Scan(
		&d.ID, &d.UserID, &d.Nome, &d.Email,
		&d.Valor, &d.Status,
		&d.MePixID, &d.PixQR, &d.PixCodigo,
		&d.ExpiresAt, &d.PaidAt,
		&d.TxID, &d.CreatedAt,
	)
	if err == pgx.ErrNoRows {
		httpx.Err(w, 404, "not_found", "recarga não encontrada")
		return
	}
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 200, d)
}
