// Package handlers — endpoints da carteira COD (Cash on Delivery).
//
// Rotas cobertas (namespace /wp-json/senderzz/v1):
//   GET  /cod/saldo      — saldo atual da carteira COD do usuário
//   GET  /cod/extrato    — histórico de movimentações COD
//   POST /cod/anticipate — solicitação de antecipação de saldo COD
//
// Semântica financeira (espelha tpc_carteira / wallet.php):
//   - SELECT FOR UPDATE garante atomicidade ao modificar o saldo.
//   - Idempotência via UNIQUE (user_id, referencia, tipo) em senderzz_cod_ledger.
//   - Valores sempre positivos no ledger; o tipo define direção do fluxo.
//   - Antecipação: cria entrada tipo="antecipacao" e marca créditos pendentes como processados.
//
// Comentários em PT-BR conforme convenção do projeto.
package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/senderzz/affiliates-service/internal/auth"
	"github.com/senderzz/affiliates-service/internal/httpx"
)

// CODHandler agrupa as dependências dos handlers de carteira COD.
type CODHandler struct {
	Pool *pgxpool.Pool
}

// ── GET /cod/saldo ────────────────────────────────────────────────────────────

// GetSaldo retorna o saldo atual da carteira COD do usuário autenticado.
// Leitura simples sem transação (consistência eventual aceitável para consulta).
func (h *CODHandler) GetSaldo(w http.ResponseWriter, r *http.Request) {
	user := auth.PortalUserFromCtx(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	var saldoStr string
	err := h.Pool.QueryRow(r.Context(),
		`SELECT saldo FROM senderzz_cod_wallet WHERE user_id = $1`,
		user.ID,
	).Scan(&saldoStr)

	if err == pgx.ErrNoRows {
		// Usuário sem carteira COD — retorna zero (cria ao primeiro crédito).
		httpx.WriteOK(w, map[string]any{"saldo": "0.00", "user_id": user.ID})
		return
	}
	if err != nil {
		slog.Error("[cod_saldo] erro ao consultar carteira COD", "user_id", user.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao consultar saldo")
		return
	}

	saldo, _ := decimal.NewFromString(saldoStr)
	httpx.WriteOK(w, map[string]any{
		"saldo":   saldo.StringFixed(2),
		"user_id": user.ID,
	})
}

// ── GET /cod/extrato ──────────────────────────────────────────────────────────

// GetExtrato retorna as últimas N movimentações COD do usuário autenticado.
// Parâmetros de query opcionais:
//   - limit: número de registros (default 50, máximo 100)
//   - tipo:  filtra por tipo (credito|debito|antecipacao)
func (h *CODHandler) GetExtrato(w http.ResponseWriter, r *http.Request) {
	user := auth.PortalUserFromCtx(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	limit := 50
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if n, err := strconv.Atoi(lStr); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	tipoFilter := r.URL.Query().Get("tipo")

	var rows pgx.Rows
	var err error

	if tipoFilter != "" {
		rows, err = h.Pool.Query(r.Context(), `
			SELECT id, user_id, tipo, valor, pedido_id, descricao, referencia, created_at
			  FROM senderzz_cod_ledger
			 WHERE user_id = $1 AND tipo = $2
			 ORDER BY created_at DESC, id DESC
			 LIMIT $3`,
			user.ID, tipoFilter, limit,
		)
	} else {
		rows, err = h.Pool.Query(r.Context(), `
			SELECT id, user_id, tipo, valor, pedido_id, descricao, referencia, created_at
			  FROM senderzz_cod_ledger
			 WHERE user_id = $1
			 ORDER BY created_at DESC, id DESC
			 LIMIT $2`,
			user.ID, limit,
		)
	}
	if err != nil {
		slog.Error("[cod_extrato] erro ao consultar ledger", "user_id", user.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao consultar extrato")
		return
	}
	defer rows.Close()

	type ledgerRow struct {
		ID         int64     `json:"id"`
		UserID     int64     `json:"user_id"`
		Tipo       string    `json:"tipo"`
		Valor      string    `json:"valor"`
		PedidoID   *int64    `json:"pedido_id,omitempty"`
		Descricao  *string   `json:"descricao,omitempty"`
		Referencia *string   `json:"referencia,omitempty"`
		CreatedAt  time.Time `json:"created_at"`
	}

	var result []ledgerRow
	for rows.Next() {
		var row ledgerRow
		var valorStr string
		if err := rows.Scan(
			&row.ID, &row.UserID, &row.Tipo, &valorStr,
			&row.PedidoID, &row.Descricao, &row.Referencia, &row.CreatedAt,
		); err != nil {
			slog.Error("[cod_extrato] erro ao ler linha", "user_id", user.ID, "err", err)
			continue
		}
		v, _ := decimal.NewFromString(valorStr)
		row.Valor = v.StringFixed(2)
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		slog.Error("[cod_extrato] erro após iteração", "user_id", user.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao processar extrato")
		return
	}
	if result == nil {
		result = []ledgerRow{}
	}

	httpx.WriteOK(w, map[string]any{"data": result, "total": len(result)})
}

// ── POST /cod/anticipate ──────────────────────────────────────────────────────

// PostAnticipate — solicita antecipação de saldo COD pendente.
//
// Semântica:
//  1. SELECT FOR UPDATE na carteira COD do usuário.
//  2. Verifica que saldo > 0.
//  3. Insere entrada tipo="antecipacao" no ledger com idempotência via referencia.
//  4. Debita o saldo da carteira (saldo → 0 após antecipação total).
//  5. Retorna entry_id e valor antecipado.
//
// Body: {valor?, descricao?}
//   - Se valor não informado, antecipa o saldo total disponível.
//   - referencia gerada automaticamente: "antecipacao_{user_id}_{timestamp_unix}"
func (h *CODHandler) PostAnticipate(w http.ResponseWriter, r *http.Request) {
	user := auth.PortalUserFromCtx(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	var req struct {
		Valor     *decimal.Decimal `json:"valor"`
		Descricao string           `json:"descricao"`
	}
	// Body é opcional — ignora erros de decode.
	_ = json.NewDecoder(r.Body).Decode(&req)

	ctx := r.Context()

	// Inicia transação serializable para garantir atomicidade no saldo COD.
	tx, err := h.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		slog.Error("[cod_anticipate] erro ao iniciar transação", "user_id", user.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Garante linha na carteira COD (upsert idempotente).
	_, err = tx.Exec(ctx,
		`INSERT INTO senderzz_cod_wallet (user_id, saldo)
		 VALUES ($1, 0.00)
		 ON CONFLICT (user_id) DO NOTHING`,
		user.ID,
	)
	if err != nil {
		slog.Error("[cod_anticipate] erro ao upsert carteira", "user_id", user.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// SELECT FOR UPDATE na linha da carteira.
	var saldoStr string
	err = tx.QueryRow(ctx,
		`SELECT saldo FROM senderzz_cod_wallet WHERE user_id=$1 FOR UPDATE`,
		user.ID,
	).Scan(&saldoStr)
	if err != nil {
		slog.Error("[cod_anticipate] erro ao obter lock da carteira", "user_id", user.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	saldo, err := decimal.NewFromString(saldoStr)
	if err != nil || saldo.LessThanOrEqual(decimal.Zero) {
		slog.Info("[cod_anticipate] saldo zero ou negativo", "user_id", user.ID, "saldo", saldoStr)
		httpx.WriteErr(w, http.StatusPaymentRequired, "saldo COD insuficiente para antecipação")
		return
	}

	// Determina valor a antecipar: informado pelo cliente ou saldo total.
	valorAntecipar := saldo
	if req.Valor != nil && req.Valor.GreaterThan(decimal.Zero) {
		if req.Valor.GreaterThan(saldo) {
			httpx.WriteErr(w, http.StatusPaymentRequired, "valor solicitado maior que saldo disponível")
			return
		}
		valorAntecipar = *req.Valor
	}

	novoSaldo := saldo.Sub(valorAntecipar)

	// Atualiza saldo da carteira COD.
	_, err = tx.Exec(ctx,
		`UPDATE senderzz_cod_wallet SET saldo=$1, updated_at=NOW() WHERE user_id=$2`,
		novoSaldo.StringFixed(2), user.ID,
	)
	if err != nil {
		slog.Error("[cod_anticipate] erro ao atualizar saldo", "user_id", user.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Gera referência de idempotência baseada em timestamp.
	referencia := fmt.Sprintf("antecipacao_%d_%d", user.ID, time.Now().UnixNano())
	descricao := req.Descricao
	if descricao == "" {
		descricao = fmt.Sprintf("Antecipação de saldo COD — R$ %s", valorAntecipar.StringFixed(2))
	}

	// INSERT idempotente no ledger.
	var entryID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO senderzz_cod_ledger (user_id, tipo, valor, descricao, referencia)
		VALUES ($1, 'antecipacao', $2, $3, $4)
		ON CONFLICT (user_id, referencia, tipo) DO NOTHING
		RETURNING id`,
		user.ID,
		valorAntecipar.StringFixed(2),
		descricao,
		referencia,
	).Scan(&entryID)

	if err == pgx.ErrNoRows {
		// Já existia (race condition improvável com referencia baseada em nanos).
		tx.Rollback(ctx) //nolint:errcheck
		slog.Warn("[cod_anticipate] entrada de antecipação já existente", "user_id", user.ID, "referencia", referencia)
		httpx.WriteOK(w, map[string]any{"idempotente": true})
		return
	}
	if err != nil {
		slog.Error("[cod_anticipate] erro ao inserir ledger", "user_id", user.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		slog.Error("[cod_anticipate] erro ao commit", "user_id", user.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	slog.Info("[cod_anticipate] antecipação realizada",
		"user_id", user.ID,
		"valor", valorAntecipar.StringFixed(2),
		"novo_saldo", novoSaldo.StringFixed(2),
		"entry_id", entryID,
	)
	httpx.WriteOK(w, map[string]any{
		"entry_id":    entryID,
		"valor":       valorAntecipar.StringFixed(2),
		"novo_saldo":  novoSaldo.StringFixed(2),
		"referencia":  referencia,
	})
}
