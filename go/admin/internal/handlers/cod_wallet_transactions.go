// Package handlers — endpoint admin para o livro razão completo da
// carteira COD (sz_cod_wallet_transactions). É a contraparte "tx-level"
// do CodWalletProducerHandler, que mostra apenas saldos agregados.
//
// Surface:
//   GET /cod-wallet-transactions?user_id=&order_id=&type=&status=
//                                &q=&date_from=&date_to=
//                                &release_from=&release_to=
//                                &page=1&per_page=50
//   GET /cod-wallet-transactions/types
//   GET /cod-wallet-transactions/stats?user_id=&order_id=&type=&status=
//                                      &q=&date_from=&date_to=
//                                      &release_from=&release_to=
//   GET /cod-wallet-transactions/export-csv (mesmos filtros, cap 5000)
//
// Comentários em PT-BR para manter o padrão dos demais handlers.
package handlers

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/admin-service/internal/httpx"
)

// CodWalletTransactionsHandler agrupa endpoints da listagem global de tx COD.
type CodWalletTransactionsHandler struct{ Pool *pgxpool.Pool }

// ─── Tipos de resposta ───────────────────────────────────────────────────

// CodWalletTxRow — uma linha da tabela /cod-wallet-transactions.
// user_nome vem de senderzz_portal_users (JOIN por wp_user_id).
// release_at pode ser NULL (transações de saque não retêm).
type CodWalletTxRow struct {
	ID        int64   `json:"id"`
	UserID    int64   `json:"user_id"`
	UserNome  string  `json:"user_nome"`
	OrderID   *int64  `json:"order_id"`
	Type      string  `json:"type"`
	Status    string  `json:"status"`
	Gross     float64 `json:"gross"`
	Fee       float64 `json:"fee"`
	Net       float64 `json:"net"`
	ReleaseAt *string `json:"release_at"`
	CreatedAt string  `json:"created_at"`
}

// CodWalletStatsBucket — agrupamento (count + gross + fee + net) usado
// tanto por by_type quanto by_status.
type CodWalletStatsBucket struct {
	Key   string  `json:"-"`
	Count int64   `json:"count"`
	Gross float64 `json:"gross"`
	Fee   float64 `json:"fee"`
	Total float64 `json:"total"` // net — mantido como "total" para compat. fronte.
}

// CodWalletStatsTotals — totalizador global do escopo filtrado (resumo).
type CodWalletStatsTotals struct {
	Count int64   `json:"count"`
	Gross float64 `json:"gross"`
	Fee   float64 `json:"fee"`
	Net   float64 `json:"net"`
}

// ─── Helpers ─────────────────────────────────────────────────────────────

// tableExists — utilitário compartilhado com audit/affiliate_wallet. Graceful
// degradation: tabela ausente devolve resposta vazia em vez de 500.
func (h *CodWalletTransactionsHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// clampCodPage clampa page e per_page em ranges seguros (default 1 / 50).
// per_page máx 200 para evitar varredura acidental.
func clampCodPage(p, pp int) (int, int) {
	if p <= 0 {
		p = 1
	}
	if pp <= 0 {
		pp = 50
	}
	if pp > 200 {
		pp = 200
	}
	return p, pp
}

// ─── GET /cod-wallet-transactions ────────────────────────────────────────

// buildCodTxWhere monta o WHERE dinâmico dos filtros compartilhados por
// List, Stats e ExportCSV. Retorna (conds, args, hasUsersJoin).
//   q              — r.URL.Query()
//   hasUsers       — se senderzz_portal_users está disponível (para filtro q)
//   includeRelease — se deve incluir filtros release_from/release_to
func buildCodTxWhere(q interface {
	Get(string) string
}, hasUsers bool, includeRelease bool) ([]string, []any) {
	args := []any{}
	conds := []string{}

	if v, _ := strconv.ParseInt(q.Get("user_id"), 10, 64); v > 0 {
		args = append(args, v)
		conds = append(conds, "t.user_id = $"+strconv.Itoa(len(args)))
	}
	if v, _ := strconv.ParseInt(q.Get("order_id"), 10, 64); v > 0 {
		args = append(args, v)
		conds = append(conds, "t.order_id = $"+strconv.Itoa(len(args)))
	}
	if v := strings.TrimSpace(q.Get("type")); v != "" {
		args = append(args, v)
		conds = append(conds, "t.type = $"+strconv.Itoa(len(args)))
	}
	if v := strings.TrimSpace(q.Get("status")); v != "" {
		args = append(args, v)
		conds = append(conds, "t.status = $"+strconv.Itoa(len(args)))
	}
	// Busca textual por nome/email (q) — só ativa se portal_users disponível.
	if hasUsers {
		if v := strings.TrimSpace(q.Get("q")); v != "" {
			args = append(args, v)
			n := strconv.Itoa(len(args))
			conds = append(conds, "(p.nome ILIKE '%' || $"+n+" || '%' OR p.email ILIKE '%' || $"+n+" || '%')")
		}
	}
	// Datas de criação — usa parseDateFilter (audit_log.go).
	if v, ok := parseDateFilter(q.Get("date_from"), false); ok {
		args = append(args, v)
		conds = append(conds, "t.created_at >= $"+strconv.Itoa(len(args)))
	}
	if v, ok := parseDateFilter(q.Get("date_to"), true); ok {
		args = append(args, v)
		conds = append(conds, "t.created_at <= $"+strconv.Itoa(len(args)))
	}
	// Datas de liberação (release_at) — NULL é naturalmente excluído.
	if includeRelease {
		if v, ok := parseDateFilter(q.Get("release_from"), false); ok {
			args = append(args, v)
			conds = append(conds, "t.release_at >= $"+strconv.Itoa(len(args)))
		}
		if v, ok := parseDateFilter(q.Get("release_to"), true); ok {
			args = append(args, v)
			conds = append(conds, "t.release_at <= $"+strconv.Itoa(len(args)))
		}
	}
	return conds, args
}

// List devolve a página paginada de tx COD com os filtros opcionais.
// Filtros aceitos:
//   user_id      — número exato
//   order_id     — número exato
//   type         — string exata (whitelist client-side em /types)
//   status       — string exata
//   q            — busca nome/email (ILIKE, requer senderzz_portal_users)
//   date_from    — yyyy-mm-dd (inclusivo, created_at)
//   date_to      — yyyy-mm-dd (inclusivo, created_at)
//   release_from — yyyy-mm-dd (inclusivo, release_at)
//   release_to   — yyyy-mm-dd (inclusivo, release_at)
//   page         — 1-indexed, default 1
//   per_page     — default 50, máx 200
//
// O total da paginação vem via COUNT(*) OVER() (mesma query, sem round-trip
// extra). WHERE é montado dinamicamente via buildCodTxWhere.
func (h *CodWalletTransactionsHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	page, _ := strconv.Atoi(q.Get("page"))
	perPage, _ := strconv.Atoi(q.Get("per_page"))
	page, perPage = clampCodPage(page, perPage)

	if !h.tableExists(ctx, "sz_cod_wallet_transactions") {
		httpx.JSON(w, 200, map[string]any{
			"items": []CodWalletTxRow{}, "total": 0,
			"page": page, "per_page": perPage,
		})
		return
	}

	// JOIN com senderzz_portal_users é opcional (tabela pode não ter sido
	// migrada) — fallback devolve nome vazio.
	hasUsers := h.tableExists(ctx, "senderzz_portal_users")
	userJoin := ""
	nomeExpr := "''::text AS user_nome"
	if hasUsers {
		userJoin = `LEFT JOIN senderzz_portal_users p ON p.wp_user_id = t.user_id`
		nomeExpr = `COALESCE(NULLIF(p.nome,''), p.email, '') AS user_nome`
	}

	conds, args := buildCodTxWhere(q, hasUsers, true)

	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	// LIMIT/OFFSET vão por último.
	offset := (page - 1) * perPage
	args = append(args, perPage, offset)
	limitIdx := strconv.Itoa(len(args) - 1)
	offsetIdx := strconv.Itoa(len(args))

	sql := `
		SELECT
			t.id, t.user_id, ` + nomeExpr + `,
			t.order_id,
			COALESCE(t.type,''), COALESCE(t.status,''),
			COALESCE(t.gross,0), COALESCE(t.fee,0), COALESCE(t.net,0),
			t.release_at::text, t.created_at::text,
			COUNT(*) OVER() AS total_count
		FROM sz_cod_wallet_transactions t
		` + userJoin + `
		` + where + `
		ORDER BY t.id DESC
		LIMIT $` + limitIdx + ` OFFSET $` + offsetIdx

	rows, err := h.Pool.Query(ctx, sql, args...)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []CodWalletTxRow{}
	var total int64
	for rows.Next() {
		var x CodWalletTxRow
		var rowTotal int64
		if err := rows.Scan(
			&x.ID, &x.UserID, &x.UserNome,
			&x.OrderID,
			&x.Type, &x.Status,
			&x.Gross, &x.Fee, &x.Net,
			&x.ReleaseAt, &x.CreatedAt,
			&rowTotal,
		); err != nil {
			continue
		}
		total = rowTotal // todas as linhas trazem o mesmo total (window func)
		out = append(out, x)
	}

	httpx.JSON(w, 200, map[string]any{
		"items":    out,
		"total":    total,
		"page":     page,
		"per_page": perPage,
	})
}

// ─── GET /cod-wallet-transactions/types ──────────────────────────────────

// Types devolve o enum aceito de "type" para popular o filtro do frontend.
// A lista é estática (PHP não expõe esse enum) — refere-se aos valores que
// o PHP grava em sz_cod_wallet_transactions.type.
func (h *CodWalletTransactionsHandler) Types(w http.ResponseWriter, r *http.Request) {
	types := []string{
		"credit",
		"withdrawal",
		"manual_credit",
		"manual_debit",
		"refund",
		"adjustment",
	}
	httpx.JSON(w, 200, map[string]any{"items": types})
}

// ─── GET /cod-wallet-transactions/stats ──────────────────────────────────

// Stats devolve três blocos para o banner KPI: totals (resumo escopo),
// by_type e by_status — todos com gross/fee/net.
// Aceita os mesmos filtros de List para sincronizar o escopo com a tabela.
func (h *CodWalletTransactionsHandler) Stats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	empty := map[string]any{
		"totals":    CodWalletStatsTotals{},
		"by_type":   []map[string]any{},
		"by_status": []map[string]any{},
	}

	if !h.tableExists(ctx, "sz_cod_wallet_transactions") {
		httpx.JSON(w, 200, empty)
		return
	}

	hasUsers := h.tableExists(ctx, "senderzz_portal_users")
	userJoin := ""
	if hasUsers {
		userJoin = `LEFT JOIN senderzz_portal_users p ON p.wp_user_id = t.user_id`
	}

	conds, args := buildCodTxWhere(q, hasUsers, true)
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	// totals — resumo global do escopo filtrado.
	var totals CodWalletStatsTotals
	_ = h.Pool.QueryRow(ctx,
		`SELECT COUNT(*), COALESCE(SUM(t.gross),0), COALESCE(SUM(t.fee),0), COALESCE(SUM(t.net),0)
		 FROM sz_cod_wallet_transactions t
		 `+userJoin+`
		 `+where, args...).Scan(
		&totals.Count, &totals.Gross, &totals.Fee, &totals.Net)

	// by_type (alias "t2" para o agrupamento usar o mesmo join).
	typeRows, err := h.Pool.Query(ctx,
		`SELECT COALESCE(t.type,'') AS k,
		        COUNT(*),
		        COALESCE(SUM(t.gross),0),
		        COALESCE(SUM(t.fee),0),
		        COALESCE(SUM(t.net),0)
		 FROM sz_cod_wallet_transactions t
		 `+userJoin+`
		 `+where+`
		 GROUP BY t.type
		 ORDER BY COUNT(*) DESC`, args...)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	byType := []map[string]any{}
	for typeRows.Next() {
		var b CodWalletStatsBucket
		if err := typeRows.Scan(&b.Key, &b.Count, &b.Gross, &b.Fee, &b.Total); err == nil {
			byType = append(byType, map[string]any{
				"type":  b.Key,
				"count": b.Count,
				"gross": b.Gross,
				"fee":   b.Fee,
				"total": b.Total,
			})
		}
	}
	typeRows.Close()

	// by_status
	statusRows, err := h.Pool.Query(ctx,
		`SELECT COALESCE(t.status,'') AS k,
		        COUNT(*),
		        COALESCE(SUM(t.gross),0),
		        COALESCE(SUM(t.fee),0),
		        COALESCE(SUM(t.net),0)
		 FROM sz_cod_wallet_transactions t
		 `+userJoin+`
		 `+where+`
		 GROUP BY t.status
		 ORDER BY COUNT(*) DESC`, args...)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	byStatus := []map[string]any{}
	for statusRows.Next() {
		var b CodWalletStatsBucket
		if err := statusRows.Scan(&b.Key, &b.Count, &b.Gross, &b.Fee, &b.Total); err == nil {
			byStatus = append(byStatus, map[string]any{
				"status": b.Key,
				"count":  b.Count,
				"gross":  b.Gross,
				"fee":    b.Fee,
				"total":  b.Total,
			})
		}
	}
	statusRows.Close()

	httpx.JSON(w, 200, map[string]any{
		"totals":    totals,
		"by_type":   byType,
		"by_status": byStatus,
	})
}

// ─── GET /cod-wallet-transactions/export-csv ─────────────────────────────

// ExportCSV gera CSV com as transações COD filtradas (cap 5000 linhas).
// Mesmos filtros de List — o range selecionado na tela é espelhado no download.
// Separador ";", UTF-8 BOM para compatibilidade com Excel.
func (h *CodWalletTransactionsHandler) ExportCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if !h.tableExists(ctx, "sz_cod_wallet_transactions") {
		httpx.Err(w, 503, "table_missing", "tabela sz_cod_wallet_transactions não migrada")
		return
	}

	q := r.URL.Query()
	hasUsers := h.tableExists(ctx, "senderzz_portal_users")
	userJoin := ""
	nomeExpr := "''::text AS user_nome, ''::text AS user_email"
	if hasUsers {
		userJoin = `LEFT JOIN senderzz_portal_users p ON p.wp_user_id = t.user_id`
		nomeExpr = `COALESCE(NULLIF(p.nome,''), p.email, '') AS user_nome,
		             COALESCE(p.email, '') AS user_email`
	}

	conds, args := buildCodTxWhere(q, hasUsers, true)
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	rows, err := h.Pool.Query(ctx, `
		SELECT
			t.id, t.user_id, `+nomeExpr+`,
			COALESCE(t.order_id::text, ''),
			COALESCE(t.type,''), COALESCE(t.status,''),
			COALESCE(t.gross,0), COALESCE(t.fee,0), COALESCE(t.net,0),
			COALESCE(t.release_at::text, ''),
			t.created_at::text
		FROM sz_cod_wallet_transactions t
		`+userJoin+`
		`+where+`
		ORDER BY t.id DESC
		LIMIT 5000`, args...)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	// Nome do arquivo com período se preenchido.
	fname := "transacoes-cod"
	if df := q.Get("date_from"); df != "" {
		fname += "-" + df
	}
	if dt := q.Get("date_to"); dt != "" {
		fname += "-" + dt
	}
	fname += ".csv"

	w.Header().Set("Content-Type", "text/csv; charset=UTF-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fname))

	// BOM UTF-8 para Excel reconhecer acentos.
	_, _ = w.Write([]byte("\xEF\xBB\xBF"))

	cw := csv.NewWriter(w)
	cw.Comma = ';'

	_ = cw.Write([]string{
		"ID", "User ID", "Nome", "Email", "Order ID",
		"Tipo", "Status", "Gross", "Fee", "Net",
		"Liberação", "Criado em",
	})

	for rows.Next() {
		var (
			id        int64
			userID    int64
			userNome  string
			userEmail string
			orderID   string
			txType    string
			status    string
			gross     float64
			fee       float64
			net       float64
			releaseAt string
			createdAt string
		)
		if err := rows.Scan(
			&id, &userID, &userNome, &userEmail, &orderID,
			&txType, &status, &gross, &fee, &net,
			&releaseAt, &createdAt,
		); err != nil {
			continue
		}
		_ = cw.Write([]string{
			strconv.FormatInt(id, 10),
			strconv.FormatInt(userID, 10),
			userNome,
			userEmail,
			orderID,
			txType,
			status,
			fmt.Sprintf("%.2f", gross),
			fmt.Sprintf("%.2f", fee),
			fmt.Sprintf("%.2f", net),
			releaseAt,
			createdAt,
		})
	}
	cw.Flush()
}
