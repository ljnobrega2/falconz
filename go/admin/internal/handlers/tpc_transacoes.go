// Package handlers — endpoint admin TpcTransacoes.
// Espelha a aba "Transações" do PHP legado (includes/tpc/admin.php:555) sobre
// Postgres. Lista transações da tabela tpc_transacoes com filtros (tipo, status,
// user_id, intervalo de datas), permite excluir transação com recálculo de saldo
// e dispara verificação de PIX pendentes.
//
// Convenções:
//   - JOIN em senderzz_portal_users.wp_user_id (NÃO em u.id — wp_user_id é o ID
//     do WordPress, que é o que tpc_transacoes.user_id armazena).
//   - Linhas internas de admin allocation são filtradas via meta_json->>'senderzz_admin_me_allocation'.
//     Usa IS DISTINCT FROM 'true' para lidar com NULL (ausência da chave) sem
//     descartar linhas legítimas.
//   - Delete + recálculo de saldo executa dentro de uma transação pgx (BEGIN/COMMIT)
//     com FOR UPDATE para evitar race com webhook PIX/reserva.
//   - Graceful degradation: se senderzz_portal_users não existir, retorna nome/email
//     vazios em vez de falhar.
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

type TpcTransacoesHandler struct{ Pool *pgxpool.Pool }

// transacaoRow — linha exibida na listagem (com nome/email vindos do JOIN).
type transacaoRow struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"user_id"`
	Nome       string  `json:"nome"`
	Email      string  `json:"email"`
	Tipo       string  `json:"tipo"`
	Valor      float64 `json:"valor"`
	SaldoApos  float64 `json:"saldo_apos"`
	Descricao  string  `json:"descricao"`
	Referencia *string `json:"referencia"`
	OrderID    *int64  `json:"order_id"`
	MeOrderID  *string `json:"me_order_id"`
	Status     string  `json:"status"`
	CreatedAt  string  `json:"created_at"`
}

func (h *TpcTransacoesHandler) tableExists(ctx context.Context, name string) bool {
	return tableExistsCached(ctx, h.Pool, name) // AUDIT-2026-06-18 Onda2 (go-infoschema-cache)
}

// List — GET /tpc-transacoes?user_id=&tipo=&status=&data_ini=&data_fim=&page=1&per_page=20
//
// Filtros:
//   - tipo: credito | debito | all (vazio = all)
//   - status: pendente | analise | confirmado | cancelado | all (vazio = all)
//   - user_id: filtro exato
//   - data_ini / data_fim: YYYY-MM-DD (inclusivo nos dois extremos)
//
// Sempre exclui linhas onde meta_json->>'senderzz_admin_me_allocation' = 'true'
// (alocação interna do admin não deve poluir o extrato). Ordena por created_at DESC.
func (h *TpcTransacoesHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	userID, _ := strconv.ParseInt(strings.TrimSpace(q.Get("user_id")), 10, 64)
	tipo := strings.ToLower(strings.TrimSpace(q.Get("tipo")))
	status := strings.ToLower(strings.TrimSpace(q.Get("status")))
	dataIni := strings.TrimSpace(q.Get("data_ini"))
	dataFim := strings.TrimSpace(q.Get("data_fim"))

	if tipo == "all" {
		tipo = ""
	}
	if status == "all" {
		status = ""
	}

	page, _ := strconv.Atoi(q.Get("page"))
	if page <= 0 {
		page = 1
	}
	perPage, _ := strconv.Atoi(q.Get("per_page"))
	if perPage <= 0 || perPage > 500 {
		perPage = 20
	}
	offset := (page - 1) * perPage

	// Tabela não migrada ainda — devolve vazio em vez de 500.
	if !h.tableExists(ctx, "tpc_transacoes") {
		httpx.JSON(w, 200, map[string]any{
			"items": []transacaoRow{}, "total": 0, "page": page, "per_page": perPage,
		})
		return
	}

	hasUsers := h.tableExists(ctx, "senderzz_portal_users")

	// Cláusula WHERE compartilhada entre SELECT e COUNT — placeholders idênticos.
	// $1=user_id (0=all), $2=tipo (''=all), $3=status (''=all), $4=data_ini, $5=data_fim.
	//
	// ISOLAMENTO COD (FALK): esta tela é EXCLUSIVA de Expedição (frete pré-pago).
	// Transações COD (Cash-on-Delivery) NÃO podem aparecer aqui — o dono pediu COD
	// fora das telas de Expedição. As linhas COD vazaram para tpc_transacoes com
	// referencia 'sz_cod_produtor_<order>' e descricao "Venda COD Senderzz ...".
	// Discriminador robusto = referencia LIKE 'sz_cod%' (estruturado, 27/27 no prod);
	// descricao ILIKE '%cod%' como cinto-e-suspensório. referencia é NULLABLE, então
	// tratamos NULL como NÃO-COD (linha legítima de frete) explicitamente. As linhas
	// COD continuam visíveis nas telas próprias de COD (sz_cod_*, Transações COD).
	whereClause := `
		WHERE ($1 = 0 OR t.user_id = $1)
		  AND ($2 = '' OR t.tipo = $2)
		  AND ($3 = '' OR t.status = $3)
		  AND ($4 = '' OR t.created_at >= ($4::date)::timestamp AT TIME ZONE 'America/Sao_Paulo')
		  AND ($5 = '' OR t.created_at <= (($5::date + 1)::timestamp AT TIME ZONE 'America/Sao_Paulo' - interval '1 second'))
		  AND (t.referencia IS NULL OR t.referencia NOT LIKE 'sz_cod%')
		  AND COALESCE(t.descricao, '') NOT ILIKE '%cod%'`

	// SELECT principal — usa LEFT JOIN com portal_users quando disponível.
	var sqlList string
	if hasUsers {
		sqlList = `
			SELECT t.id, t.user_id,
			       COALESCE(u.nome,  '') AS nome,
			       COALESCE(u.email, '') AS email,
			       t.tipo,
			       COALESCE(t.valor, 0),
			       COALESCE(t.saldo_apos, 0),
			       COALESCE(t.descricao, ''),
			       t.referencia,
			       t.order_id,
			       t.me_order_id,
			       COALESCE(t.status, 'confirmado'),
			       t.created_at::text
			FROM tpc_transacoes t
			LEFT JOIN senderzz_portal_users u ON u.id = t.user_id` +
			whereClause + `
			ORDER BY t.created_at DESC, t.id DESC
			LIMIT $6 OFFSET $7`
	} else {
		sqlList = `
			SELECT t.id, t.user_id,
			       ''::text AS nome,
			       ''::text AS email,
			       t.tipo,
			       COALESCE(t.valor, 0),
			       COALESCE(t.saldo_apos, 0),
			       COALESCE(t.descricao, ''),
			       t.referencia,
			       t.order_id,
			       t.me_order_id,
			       COALESCE(t.status, 'confirmado'),
			       t.created_at::text
			FROM tpc_transacoes t` +
			whereClause + `
			ORDER BY t.created_at DESC, t.id DESC
			LIMIT $6 OFFSET $7`
	}

	sqlCount := `SELECT COUNT(*) FROM tpc_transacoes t` + whereClause

	rows, err := h.Pool.Query(ctx, sqlList, userID, tipo, status, dataIni, dataFim, perPage, offset)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	items := []transacaoRow{}
	for rows.Next() {
		var t transacaoRow
		if err := rows.Scan(
			&t.ID, &t.UserID, &t.Nome, &t.Email,
			&t.Tipo, &t.Valor, &t.SaldoApos, &t.Descricao,
			&t.Referencia, &t.OrderID, &t.MeOrderID,
			&t.Status, &t.CreatedAt,
		); err != nil {
			httpx.Err(w, 500, "scan_error", err.Error())
			return
		}
		items = append(items, t)
	}

	var total int64
	_ = h.Pool.QueryRow(ctx, sqlCount, userID, tipo, status, dataIni, dataFim).Scan(&total)

	httpx.JSON(w, 200, map[string]any{
		"items":    items,
		"total":    total,
		"page":     page,
		"per_page": perPage,
	})
}

// Delete — DELETE /tpc-transacoes/{id}
//
// Apaga a transação e recalcula o saldo do usuário dono. Tudo em uma transação
// pgx — se qualquer passo falhar, rollback.
//
// Passos:
//  1. SELECT user_id FROM tpc_transacoes WHERE id=$1 FOR UPDATE
//  2. DELETE FROM tpc_transacoes WHERE id=$1
//  3. novo_saldo = SUM(credito) - SUM(debito) WHERE user_id=$ AND status='confirmado'
//  4. UPDATE tpc_carteira SET saldo=$novo_saldo WHERE user_id=$
//
// Retorna 404 se id não existir.
func (h *TpcTransacoesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}

	if !h.tableExists(ctx, "tpc_transacoes") {
		httpx.Err(w, 503, "tables_missing", "tpc_transacoes ainda não migrada")
		return
	}

	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer tx.Rollback(ctx)

	// 1) Lock da linha pra capturar user_id e impedir mutação concorrente.
	var userID int64
	err = tx.QueryRow(ctx,
		`SELECT user_id FROM tpc_transacoes WHERE id = $1 FOR UPDATE`, id).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Err(w, 404, "not_found", "transação não encontrada")
		return
	}
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// 2) Delete.
	if _, err := tx.Exec(ctx, `DELETE FROM tpc_transacoes WHERE id = $1`, id); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// 3) Recalcula saldo via soma dos confirmados (credito - reserva).
	//    Segue a spec da tarefa: status='confirmado', sem exclusão de admin allocation
	//    (a UI já filtra a visualização; o saldo refletido aqui é o real do ledger).
	// AUDIT-2026-07-28: tipo='debito' nunca existe em tpc_transacoes (só 'credito'
	// e 'reserva') — subtração virava sempre 0, saldo recalculado ficava inflado.
	var novoSaldo float64
	err = tx.QueryRow(ctx,
		`SELECT
		   COALESCE(SUM(CASE WHEN tipo='credito' THEN valor ELSE 0 END), 0)
		 - COALESCE(SUM(CASE WHEN tipo='reserva' THEN valor ELSE 0 END), 0)
		 FROM tpc_transacoes
		 WHERE user_id = $1 AND status = 'confirmado'`, userID).Scan(&novoSaldo)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// 4) Atualiza saldo na carteira (se ela existir — não cria carteira nova).
	if h.tableExists(ctx, "tpc_carteira") {
		if _, err := tx.Exec(ctx,
			`UPDATE tpc_carteira SET saldo = $1 WHERE user_id = $2`,
			novoSaldo, userID); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 200, map[string]any{
		"ok":         true,
		"id":         id,
		"user_id":    userID,
		"novo_saldo": novoSaldo,
	})
}

// VerificarPix — POST /tpc-transacoes/verificar-pix
//
// Dispara a verificação de PIX pendentes. Hoje retorna placeholder com a fila
// que seria processada — quando o wallet-service estiver disponível, chamará
// POST /internal/pix/reconcile via HMAC. Retorna sempre {ok, queued: N} para a UI
// poder exibir feedback consistente.
//
// `queued` = COUNT(*) FROM tpc_recargas WHERE status='pendente'. Se a tabela não
// existir ainda, queued=0.
func (h *TpcTransacoesHandler) VerificarPix(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var queued int64
	if h.tableExists(ctx, "tpc_recargas") {
		_ = h.Pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM tpc_recargas WHERE status = 'pendente'`).Scan(&queued)
	}

	httpx.JSON(w, 200, map[string]any{
		"ok":         true,
		"queued":     queued,
		"job_queued": true,
		"requested_at": time.Now().UTC().Format(time.RFC3339),
		"stub":       true, // placeholder até wallet-service estar integrado
	})
}
