// Package handlers — endpoint admin TpcClientes.
// Espelha a aba "Clientes" do PHP legado (includes/tpc/admin.php:254) sobre
// Postgres. Lista carteiras com nome/email do usuário (via JOIN portal_users),
// permite emitir/cancelar recargas PIX e oferece reset total das tabelas TPC.
//
// Convenções:
//   - Joins: tpc_carteira.user_id, tpc_transacoes.user_id e tpc_recargas.user_id
//     guardam o ID do WordPress (wp_user_id). Portanto JOIN em
//     senderzz_portal_users.wp_user_id (NUNCA em u.id, que é o ID do portal).
//   - Graceful degradation: se senderzz_portal_users não existir, retorna apenas
//     o user_id (sem nome/email) e o filtro `q` passa a casar contra user_id::text.
//   - Recarga real é emitida pelo wallet-service. Aqui só insere a linha pendente
//     em tpc_recargas + gera placeholders (qr_src/copia_cola/security_token) para
//     a UI exibir imediatamente — a confirmação chega pelo webhook PIX.
package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

type TpcClientesHandler struct{ Pool *pgxpool.Pool }

// clienteRow — linha exibida na listagem.
type clienteRow struct {
	UserID            int64   `json:"user_id"`
	Nome              string  `json:"nome"`
	Email             string  `json:"email"`
	Saldo             float64 `json:"saldo"`
	SaldoReservado    float64 `json:"saldo_reservado"`
	SaldoDisponivel   float64 `json:"saldo_disponivel"`
	TransacoesCount   int64   `json:"transacoes_count"`
	UltimaAtualizacao string  `json:"ultima_atualizacao"`
}

type clienteTransacao struct {
	ID        int64   `json:"id"`
	Tipo      string  `json:"tipo"`
	Valor     float64 `json:"valor"`
	SaldoApos float64 `json:"saldo_apos"`
	Descricao string  `json:"descricao"`
	Status    string  `json:"status"`
	CreatedAt string  `json:"created_at"`
}

type clienteRecarga struct {
	ID        int64   `json:"id"`
	Valor     float64 `json:"valor"`
	Status    string  `json:"status"`
	MePixID   *string `json:"me_pix_id"`
	QRSrc     *string `json:"qr_src"`
	CopiaCola *string `json:"copia_cola"`
	ExpiresAt *string `json:"expires_at"`
	CreatedAt string  `json:"created_at"`
}

func (h *TpcClientesHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// List — GET /tpc-clientes?q=&limit=100&page=1
//
// Lista carteiras com nome/email vindos de senderzz_portal_users via JOIN em
// wp_user_id. Filtro `q` casa email/nome (LIKE case-insensitive). Quando
// portal_users não existe, devolve apenas user_id e o filtro casa user_id::text.
func (h *TpcClientesHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * limit

	if !h.tableExists(ctx, "tpc_carteira") {
		httpx.JSON(w, 200, map[string]any{
			"items": []clienteRow{}, "total": 0, "page": page, "per_page": limit,
		})
		return
	}

	hasUsers := h.tableExists(ctx, "senderzz_portal_users")
	hasTx := h.tableExists(ctx, "tpc_transacoes")

	// Monta query dinâmica conforme a presença de cada tabela.
	// `tx_count` vem de subquery para não estourar GROUP BY.
	var (
		sqlList  string
		sqlTotal string
	)

	if hasUsers {
		// Filtro: email/nome via ILIKE.
		sqlList = `
			SELECT c.user_id,
			       COALESCE(u.nome,  '')             AS nome,
			       COALESCE(u.email, '')             AS email,
			       COALESCE(c.saldo, 0)              AS saldo,
			       COALESCE(c.saldo_reservado, 0)    AS saldo_reservado,
			       c.created_at::text                AS ultima
		`
		if hasTx {
			sqlList += `, COALESCE((SELECT COUNT(*) FROM tpc_transacoes t WHERE t.user_id = c.user_id), 0) AS tx_count`
		} else {
			sqlList += `, 0::bigint AS tx_count`
		}
		sqlList += `
			FROM tpc_carteira c
			LEFT JOIN senderzz_portal_users u ON u.wp_user_id = c.user_id
			WHERE ($1 = ''
			       OR u.email ILIKE '%'||$1||'%'
			       OR u.nome  ILIKE '%'||$1||'%'
			       OR c.user_id::text = $1)
			ORDER BY c.saldo DESC NULLS LAST, c.user_id DESC
			LIMIT $2 OFFSET $3`

		sqlTotal = `
			SELECT COUNT(*) FROM tpc_carteira c
			LEFT JOIN senderzz_portal_users u ON u.wp_user_id = c.user_id
			WHERE ($1 = ''
			       OR u.email ILIKE '%'||$1||'%'
			       OR u.nome  ILIKE '%'||$1||'%'
			       OR c.user_id::text = $1)`
	} else {
		// Sem portal_users: lista só por user_id; `q` casa user_id::text.
		sqlList = `
			SELECT c.user_id,
			       ''::text                        AS nome,
			       ''::text                        AS email,
			       COALESCE(c.saldo, 0)            AS saldo,
			       COALESCE(c.saldo_reservado, 0)  AS saldo_reservado,
			       c.created_at::text                AS ultima
		`
		if hasTx {
			sqlList += `, COALESCE((SELECT COUNT(*) FROM tpc_transacoes t WHERE t.user_id = c.user_id), 0) AS tx_count`
		} else {
			sqlList += `, 0::bigint AS tx_count`
		}
		sqlList += `
			FROM tpc_carteira c
			WHERE ($1 = '' OR c.user_id::text = $1)
			ORDER BY c.saldo DESC NULLS LAST, c.user_id DESC
			LIMIT $2 OFFSET $3`

		sqlTotal = `
			SELECT COUNT(*) FROM tpc_carteira c
			WHERE ($1 = '' OR c.user_id::text = $1)`
	}

	rows, err := h.Pool.Query(ctx, sqlList, q, limit, offset)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	items := []clienteRow{}
	for rows.Next() {
		var c clienteRow
		if err := rows.Scan(
			&c.UserID, &c.Nome, &c.Email,
			&c.Saldo, &c.SaldoReservado, &c.UltimaAtualizacao,
			&c.TransacoesCount,
		); err != nil {
			httpx.Err(w, 500, "scan_error", err.Error())
			return
		}
		c.SaldoDisponivel = c.Saldo - c.SaldoReservado
		items = append(items, c)
	}

	var total int64
	_ = h.Pool.QueryRow(ctx, sqlTotal, q).Scan(&total)

	httpx.JSON(w, 200, map[string]any{
		"items":    items,
		"total":    total,
		"page":     page,
		"per_page": limit,
	})
}

// Get — GET /tpc-clientes/{user_id}
//
// Detalhe de um cliente: carteira (saldo + reservado + disponivel), últimas 50
// transações e últimas 10 recargas. Joga 404 se não houver carteira.
func (h *TpcClientesHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, _ := strconv.ParseInt(chi.URLParam(r, "user_id"), 10, 64)
	if userID <= 0 {
		httpx.Err(w, 400, "bad_request", "user_id inválido")
		return
	}

	if !h.tableExists(ctx, "tpc_carteira") {
		httpx.Err(w, 503, "tables_missing", "tpc_carteira ainda não migrada")
		return
	}

	var (
		nome, email                        string
		saldo, reservado                   float64
		ultima                             string
	)

	hasUsers := h.tableExists(ctx, "senderzz_portal_users")

	var queryDetail string
	if hasUsers {
		queryDetail = `
			SELECT COALESCE(u.nome,  ''),
			       COALESCE(u.email, ''),
			       COALESCE(c.saldo, 0),
			       COALESCE(c.saldo_reservado, 0),
			       c.created_at::text
			FROM tpc_carteira c
			LEFT JOIN senderzz_portal_users u ON u.wp_user_id = c.user_id
			WHERE c.user_id = $1`
	} else {
		queryDetail = `
			SELECT ''::text, ''::text,
			       COALESCE(c.saldo, 0),
			       COALESCE(c.saldo_reservado, 0),
			       c.created_at::text
			FROM tpc_carteira c
			WHERE c.user_id = $1`
	}

	err := h.Pool.QueryRow(ctx, queryDetail, userID).
		Scan(&nome, &email, &saldo, &reservado, &ultima)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Err(w, 404, "not_found", "carteira não encontrada para esse user_id")
		return
	}
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	cliente := clienteRow{
		UserID:            userID,
		Nome:              nome,
		Email:             email,
		Saldo:             saldo,
		SaldoReservado:    reservado,
		SaldoDisponivel:   saldo - reservado,
		UltimaAtualizacao: ultima,
	}

	// Últimas 50 transações.
	txs := []clienteTransacao{}
	if h.tableExists(ctx, "tpc_transacoes") {
		rows, err := h.Pool.Query(ctx,
			`SELECT id, tipo, COALESCE(valor,0), COALESCE(saldo_apos,0),
			        COALESCE(descricao,''), COALESCE(status,''),
			        created_at::text
			 FROM tpc_transacoes
			 WHERE user_id = $1
			 ORDER BY id DESC LIMIT 50`, userID)
		if err == nil {
			for rows.Next() {
				var t clienteTransacao
				_ = rows.Scan(&t.ID, &t.Tipo, &t.Valor, &t.SaldoApos,
					&t.Descricao, &t.Status, &t.CreatedAt)
				txs = append(txs, t)
			}
			rows.Close()
			cliente.TransacoesCount = int64(len(txs))
		}
	}

	// Últimas 10 recargas.
	recargas := []clienteRecarga{}
	if h.tableExists(ctx, "tpc_recargas") {
		rows, err := h.Pool.Query(ctx,
			`SELECT id, COALESCE(valor,0), COALESCE(status,''),
			        me_pix_id, pix_qr, pix_codigo,
			        expires_at::text, created_at::text
			 FROM tpc_recargas
			 WHERE user_id = $1
			 ORDER BY id DESC LIMIT 10`, userID)
		if err == nil {
			for rows.Next() {
				var rec clienteRecarga
				_ = rows.Scan(&rec.ID, &rec.Valor, &rec.Status,
					&rec.MePixID, &rec.QRSrc, &rec.CopiaCola,
					&rec.ExpiresAt, &rec.CreatedAt)
				recargas = append(recargas, rec)
			}
			rows.Close()
		}
	}

	httpx.JSON(w, 200, map[string]any{
		"cliente":     cliente,
		"transacoes":  txs,
		"recargas":    recargas,
	})
}

// CreateRecarga — POST /tpc-clientes/{user_id}/recarga
//
// Body: {valor: float, motivo: string}
//
// Insere recarga 'pendente' em tpc_recargas e devolve placeholders de PIX (QR
// + copia-cola + security_token) para a UI exibir imediatamente. A emissão
// real do PIX é responsabilidade do wallet-service (POST /internal/recarga/
// create com HMAC) — esse handler apenas registra a linha e retorna stub.
func (h *TpcClientesHandler) CreateRecarga(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, _ := strconv.ParseInt(chi.URLParam(r, "user_id"), 10, 64)
	if userID <= 0 {
		httpx.Err(w, 400, "bad_request", "user_id inválido")
		return
	}

	var body struct {
		Valor  float64 `json:"valor"`
		Motivo string  `json:"motivo"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	if body.Valor < 10 || body.Valor > 100000 {
		httpx.Err(w, 400, "bad_request", "valor mínimo é R$ 10,00 e máximo R$ 100.000,00")
		return
	}

	if !h.tableExists(ctx, "tpc_recargas") {
		httpx.Err(w, 503, "tables_missing", "tpc_recargas ainda não migrada")
		return
	}

	// Token de segurança (placeholder até wallet-service emitir real).
	tokenBytes := make([]byte, 16)
	_, _ = rand.Read(tokenBytes)
	securityToken := hex.EncodeToString(tokenBytes)

	// Copia-e-cola placeholder — formato BRCode-like. Substituído pelo retorno
	// real de POST /internal/recarga/create quando wallet-service estiver up.
	copiaCola := "00020126360014BR.GOV.BCB.PIX0114SENDERZZ-STUB-" + securityToken[:12] +
		"5204000053039865802BR5910SENDERZZ6009SAO PAULO62070503***6304ABCD"

	// QR via api.qrserver.com (mesma estratégia da etiqueta motoboy).
	qrSrc := "https://api.qrserver.com/v1/create-qr-code/?size=320x320&data=" +
		url.QueryEscape(copiaCola)

	expiresAt := time.Now().UTC().Add(30 * time.Minute)
	mePixID := "stub-" + securityToken[:16]

	var recargaID int64
	err := h.Pool.QueryRow(ctx,
		`INSERT INTO tpc_recargas
		   (user_id, valor, status, me_pix_id, pix_qr, pix_codigo, expires_at, created_at)
		 VALUES ($1, $2, 'pendente', $3, $4, $5, $6, NOW())
		 RETURNING id`,
		userID, body.Valor, mePixID, qrSrc, copiaCola, expiresAt).Scan(&recargaID)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	// Nota: tpc_transacoes.tipo é restrito a (credito|debito|reserva) por CHECK
	// constraint. O motivo só é persistido implicitamente como contexto da recarga
	// — quando wallet-service confirmar o PIX, ele cria a transação real de
	// crédito. O motivo informado pelo admin pode ser registrado em auditoria
	// externa (TODO: senderzz_audit_log quando o handler for migrado).
	_ = body.Motivo

	httpx.JSON(w, 200, map[string]any{
		"ok":             true,
		"recarga_id":     recargaID,
		"user_id":        userID,
		"valor":          body.Valor,
		"qr_src":         qrSrc,
		"copia_cola":     copiaCola,
		"link":           qrSrc,
		"expires_at":     expiresAt.Format(time.RFC3339),
		"security_token": securityToken,
		"stub":           true, // sinaliza que é placeholder até wallet-service confirmar
	})
}

// CancelRecarga — POST /tpc-clientes/{user_id}/cancelar-recarga/{recarga_id}
//
// Marca a recarga como 'cancelado' somente se ainda estiver 'pendente' e
// pertencer ao user_id informado. Idempotente — não retorna erro se nada mudou.
func (h *TpcClientesHandler) CancelRecarga(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, _ := strconv.ParseInt(chi.URLParam(r, "user_id"), 10, 64)
	recargaID, _ := strconv.ParseInt(chi.URLParam(r, "recarga_id"), 10, 64)
	if userID <= 0 || recargaID <= 0 {
		httpx.Err(w, 400, "bad_request", "user_id/recarga_id inválidos")
		return
	}

	if !h.tableExists(ctx, "tpc_recargas") {
		httpx.Err(w, 503, "tables_missing", "tpc_recargas ainda não migrada")
		return
	}

	tag, err := h.Pool.Exec(ctx,
		`UPDATE tpc_recargas
		   SET status = 'cancelado'
		 WHERE id = $1 AND user_id = $2 AND status = 'pendente'`,
		recargaID, userID)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 200, map[string]any{
		"ok":            true,
		"recarga_id":    recargaID,
		"rows_affected": tag.RowsAffected(),
	})
}

// ResetWalletAll — POST /tpc-clientes/reset-wallet-all
//
// DANGER. Apaga TODOS os registros financeiros TPC:
//   - tpc_carteira
//   - tpc_transacoes
//   - tpc_recargas
//
// Exige body {"confirm":"RESETAR"} EXATO (case-sensitive). Tudo dentro de uma
// transação — se uma DELETE falhar, rollback total.
func (h *TpcClientesHandler) ResetWalletAll(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body struct {
		Confirm string `json:"confirm"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	// Comparação EXATA — sem trim, sem lowercase. Erro se digitou diferente.
	if body.Confirm != "RESETAR" {
		httpx.Err(w, 400, "confirmation_required",
			`envie {"confirm":"RESETAR"} (string exata, case-sensitive) para confirmar`)
		return
	}

	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer tx.Rollback(ctx)

	res := map[string]int64{
		"carteira_deleted":    0,
		"transacoes_deleted":  0,
		"recargas_deleted":    0,
		"order_meta_deleted":  0,
	}

	if h.tableExists(ctx, "tpc_carteira") {
		tag, err := tx.Exec(ctx, `DELETE FROM tpc_carteira`)
		if err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
		res["carteira_deleted"] = tag.RowsAffected()
	}

	if h.tableExists(ctx, "tpc_transacoes") {
		tag, err := tx.Exec(ctx, `DELETE FROM tpc_transacoes`)
		if err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
		res["transacoes_deleted"] = tag.RowsAffected()
	}

	if h.tableExists(ctx, "tpc_recargas") {
		tag, err := tx.Exec(ctx, `DELETE FROM tpc_recargas`)
		if err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
		res["recargas_deleted"] = tag.RowsAffected()
	}

	// Limpa metas de carteira em postmeta / wc_orders_meta (graceful: tabela pode não existir).
	// Espelha comportamento do WP: "limpa metas antigas de carteira nos pedidos (_senderzz_wallet_*)".
	if h.tableExists(ctx, "wp_postmeta") {
		tag, _ := tx.Exec(ctx,
			`DELETE FROM wp_postmeta WHERE meta_key LIKE '_senderzz_wallet_%'`)
		res["order_meta_deleted"] += tag.RowsAffected()
	}
	if h.tableExists(ctx, "wc_orders_meta") {
		tag, _ := tx.Exec(ctx,
			`DELETE FROM wc_orders_meta WHERE meta_key LIKE '_senderzz_wallet_%'`)
		res["order_meta_deleted"] += tag.RowsAffected()
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 200, map[string]any{
		"ok":     true,
		"result": res,
	})
}
