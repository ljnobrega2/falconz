// Package handlers implementa os handlers HTTP da carteira de frete Senderzz.
//
// Todos os handlers usam transações pgx com SELECT FOR UPDATE para garantir
// atomicidade idêntica ao comportamento do PHP (tpc_reservar / tpc_debitar_reserva
// / tpc_liberar_reserva em wallet.php).
//
// Semântica de reserva (espelha wallet.php):
//   - reservar       → saldo_reservado += valor (saldo inalterado), tx status=pendente, tipo=debito
//   - debitar_reserva → saldo -= valor; saldo_reservado -= valor, tx status=confirmado
//   - liberar_reserva → saldo_reservado -= valor, tx status=cancelado
//   - creditar        → saldo += valor, tx status=confirmado, tipo=credito
//
// Idempotência: INSERT ... ON CONFLICT (user_id, referencia, tipo) DO NOTHING;
// se 0 linhas inseridas → tx já existe → retorna o registro existente sem erro.
//
// Valores monetários: shopspring/decimal (nunca float64) para evitar corrupção
// de centavos em cálculos de ponto flutuante.
package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/wallet-service/internal/httpx"
	"github.com/senderzz/wallet-service/internal/middleware"
	"github.com/shopspring/decimal"
)

// WalletHandler agrupa as dependências dos handlers de carteira.
type WalletHandler struct {
	db *pgxpool.Pool
}

// NewWalletHandler cria um WalletHandler com o pool fornecido.
func NewWalletHandler(db *pgxpool.Pool) *WalletHandler {
	return &WalletHandler{db: db}
}

// ─── structs de request/response ────────────────────────────────────────────

type transacao struct {
	ID          int64           `json:"id"`
	UserID      int64           `json:"user_id"`
	Tipo        string          `json:"tipo"`
	Valor       decimal.Decimal `json:"valor"`
	SaldoApos   decimal.Decimal `json:"saldo_apos"`
	Descricao   string          `json:"descricao"`
	Referencia  *string         `json:"referencia,omitempty"`
	OrderID     *int64          `json:"order_id,omitempty"`
	MeOrderID   *string         `json:"me_order_id,omitempty"`
	Status      string          `json:"status"`
	ActorID     *int64          `json:"actor_id,omitempty"`
	IPAddress   *string         `json:"ip_address,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
}

type reservarRequest struct {
	Valor      decimal.Decimal `json:"valor"`
	Referencia string          `json:"referencia"`
	Descricao  string          `json:"descricao"`
}

type debitarReservaRequest struct {
	Referencia string `json:"referencia"`
}

type creditarRequest struct {
	Valor      decimal.Decimal `json:"valor"`
	Referencia string          `json:"referencia"`
	Descricao  string          `json:"descricao"`
}

type liberarReservaRequest struct {
	Referencia string `json:"referencia"`
	Motivo     string `json:"motivo"`
}

// ─── helpers internos ────────────────────────────────────────────────────────

// upsertCarteira garante que o usuário tem uma linha em tpc_carteira.
// Espelha o ON DUPLICATE KEY UPDATE id=id do PHP.
// Deve ser chamado dentro de uma transação com o lock da tabela já obtido
// (ou antes de obter o lock, pois o INSERT é idempotente).
func upsertCarteira(ctx context.Context, tx pgx.Tx, userID int64) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO tpc_carteira (user_id, saldo, saldo_reservado)
		 VALUES ($1, 0.00, 0.00)
		 ON CONFLICT (user_id) DO NOTHING`,
		userID,
	)
	return err
}

// lockCarteira obtém a linha da carteira com FOR UPDATE dentro de uma transação.
// Retorna saldo e saldo_reservado como Decimal.
func lockCarteira(ctx context.Context, tx pgx.Tx, userID int64) (saldo, reservado decimal.Decimal, err error) {
	var sStr, rStr string
	err = tx.QueryRow(ctx,
		`SELECT saldo, saldo_reservado
		 FROM tpc_carteira
		 WHERE user_id = $1
		 FOR UPDATE`,
		userID,
	).Scan(&sStr, &rStr)
	if err != nil {
		return decimal.Zero, decimal.Zero, err
	}
	saldo, err = decimal.NewFromString(sStr)
	if err != nil {
		return decimal.Zero, decimal.Zero, err
	}
	reservado, err = decimal.NewFromString(rStr)
	return
}

// ─── GET /carteira/saldo ─────────────────────────────────────────────────────

// GetSaldo retorna saldo, saldo_reservado e saldo_disponivel do usuário autenticado.
// Leitura simples sem transação (consistência eventual aceitável para consulta de saldo).
func (h *WalletHandler) GetSaldo(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == 0 {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	var sStr, rStr string
	err := h.db.QueryRow(r.Context(),
		`SELECT saldo, saldo_reservado
		 FROM tpc_carteira
		 WHERE user_id = $1`,
		userID,
	).Scan(&sStr, &rStr)

	if err == pgx.ErrNoRows {
		// Usuário sem carteira ainda — retorna zero sem criar a linha agora.
		httpx.WriteOK(w, map[string]any{
			"saldo":            "0.00",
			"saldo_reservado":  "0.00",
			"saldo_disponivel": "0.00",
		})
		return
	}
	if err != nil {
		slog.Error("[tpc_saldo] erro ao consultar carteira", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao consultar saldo")
		return
	}

	saldo, _ := decimal.NewFromString(sStr)
	reservado, _ := decimal.NewFromString(rStr)
	disponivel := decimal.Max(decimal.Zero, saldo.Sub(reservado))

	httpx.WriteOK(w, map[string]any{
		"saldo":            saldo.StringFixed(2),
		"saldo_reservado":  reservado.StringFixed(2),
		"saldo_disponivel": disponivel.StringFixed(2),
	})
}

// ─── GET /carteira/extrato?limit=50 ─────────────────────────────────────────

// GetExtrato retorna as últimas N transações do usuário autenticado.
// Parâmetro de query: limit (default 50, máximo 100).
func (h *WalletHandler) GetExtrato(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == 0 {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	limit := 50
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if n, err := strconv.Atoi(lStr); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	rows, err := h.db.Query(r.Context(),
		`SELECT id, user_id, tipo, valor, saldo_apos, descricao,
		        referencia, order_id, me_order_id, status,
		        actor_id, ip_address, created_at
		 FROM tpc_transacoes
		 WHERE user_id = $1
		 ORDER BY created_at DESC, id DESC
		 LIMIT $2`,
		userID, limit,
	)
	if err != nil {
		slog.Error("[tpc_extrato] erro ao consultar transações", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao consultar extrato")
		return
	}
	defer rows.Close()

	var txs []transacao
	for rows.Next() {
		var t transacao
		var valorStr, saldoAposStr string
		if err := rows.Scan(
			&t.ID, &t.UserID, &t.Tipo, &valorStr, &saldoAposStr,
			&t.Descricao, &t.Referencia, &t.OrderID, &t.MeOrderID,
			&t.Status, &t.ActorID, &t.IPAddress, &t.CreatedAt,
		); err != nil {
			slog.Error("[tpc_extrato] erro ao ler linha", "user_id", userID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao ler extrato")
			return
		}
		t.Valor, _ = decimal.NewFromString(valorStr)
		t.SaldoApos, _ = decimal.NewFromString(saldoAposStr)
		txs = append(txs, t)
	}
	if err := rows.Err(); err != nil {
		slog.Error("[tpc_extrato] erro após iteração", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao processar extrato")
		return
	}

	if txs == nil {
		txs = []transacao{}
	}

	httpx.WriteOK(w, map[string]any{
		"data":  txs,
		"total": len(txs),
	})
}

// ─── POST /carteira/reservar ─────────────────────────────────────────────────

// PostReservar reserva um valor na carteira do usuário autenticado.
//
// Semântica (espelha tpc_reservar em wallet.php):
//  1. Garante linha em tpc_carteira (upsert).
//  2. SELECT FOR UPDATE na linha da carteira.
//  3. Verifica saldo_disponivel = saldo - saldo_reservado >= valor.
//  4. saldo_reservado += valor (saldo inalterado).
//  5. INSERT em tpc_transacoes tipo=debito status=pendente.
//     Idempotência: ON CONFLICT (user_id, referencia, tipo) DO NOTHING.
//     Se 0 linhas inseridas → já existia → retorna tx existente.
func (h *WalletHandler) PostReservar(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == 0 {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	var req reservarRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}
	if req.Valor.LessThanOrEqual(decimal.Zero) {
		httpx.WriteErr(w, http.StatusBadRequest, "valor deve ser maior que zero")
		return
	}
	if req.Referencia == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "referencia é obrigatória")
		return
	}

	tx, err := h.db.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		slog.Error("[tpc_reservar] erro ao iniciar transação", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	// Garante linha na carteira antes de qualquer lock.
	if err := upsertCarteira(r.Context(), tx, userID); err != nil {
		slog.Error("[tpc_reservar] erro ao criar carteira", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	saldo, reservado, err := lockCarteira(r.Context(), tx, userID)
	if err != nil {
		slog.Error("[tpc_reservar] erro ao obter lock da carteira", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	disponivel := decimal.Max(decimal.Zero, saldo.Sub(reservado))
	if disponivel.LessThan(req.Valor) {
		slog.Info("[tpc_reservar] saldo insuficiente",
			"user_id", userID,
			"disponivel", disponivel.StringFixed(2),
			"solicitado", req.Valor.StringFixed(2),
		)
		httpx.WriteErr(w, http.StatusPaymentRequired, "saldo insuficiente")
		return
	}

	novoReservado := reservado.Add(req.Valor)

	// Atualiza saldo_reservado.
	_, err = tx.Exec(r.Context(),
		`UPDATE tpc_carteira
		 SET saldo_reservado = $1
		 WHERE user_id = $2`,
		novoReservado.StringFixed(2), userID,
	)
	if err != nil {
		slog.Error("[tpc_reservar] erro ao atualizar saldo_reservado", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Insere transação com idempotência via unique key (user_id, referencia, tipo).
	var txID int64
	err = tx.QueryRow(r.Context(),
		`INSERT INTO tpc_transacoes
		    (user_id, tipo, valor, saldo_apos, descricao, referencia, status)
		 VALUES ($1, 'debito', $2, $3, $4, $5, 'pendente')
		 ON CONFLICT (user_id, referencia, tipo) DO NOTHING
		 RETURNING id`,
		userID,
		req.Valor.StringFixed(2),
		saldo.StringFixed(2), // saldo_apos = saldo atual (reserva não altera saldo)
		req.Descricao,
		req.Referencia,
	).Scan(&txID)

	if err == pgx.ErrNoRows {
		// Já existia — busca o ID da transação pendente existente (idempotência).
		tx.Rollback(r.Context()) //nolint:errcheck
		var existingID int64
		err2 := h.db.QueryRow(r.Context(),
			`SELECT id FROM tpc_transacoes
			 WHERE user_id = $1 AND referencia = $2 AND tipo = 'debito' AND status = 'pendente'
			 LIMIT 1`,
			userID, req.Referencia,
		).Scan(&existingID)
		if err2 != nil {
			slog.Error("[tpc_reservar] erro ao buscar reserva existente", "user_id", userID, "referencia", req.Referencia, "err", err2)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
			return
		}
		slog.Info("[tpc_reservar] reserva já existente (idempotente)",
			"user_id", userID,
			"referencia", req.Referencia,
			"tx_id", existingID,
		)
		httpx.WriteOK(w, map[string]any{"transacao_id": existingID, "idempotente": true})
		return
	}
	if err != nil {
		slog.Error("[tpc_reservar] erro ao inserir transação", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("[tpc_reservar] erro ao commit", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	slog.Info("[tpc_reservar] reserva criada",
		"user_id", userID,
		"valor", req.Valor.StringFixed(2),
		"referencia", req.Referencia,
		"tx_id", txID,
	)
	httpx.WriteOK(w, map[string]any{"transacao_id": txID})
}

// ─── POST /carteira/debitar-reserva ─────────────────────────────────────────

// PostDebitarReserva confirma uma reserva pendente debitando de saldo e saldo_reservado.
//
// Semântica (espelha tpc_debitar_reserva em wallet.php):
//  1. SELECT FOR UPDATE na transação pelo referencia (tipo=debito, status=pendente).
//  2. Se status=confirmado → idempotente, retorna ok.
//  3. SELECT FOR UPDATE na carteira.
//  4. Valida saldo >= valor e saldo_reservado >= valor.
//  5. saldo -= valor; saldo_reservado -= valor.
//  6. tx status → confirmado; saldo_apos atualizado.
func (h *WalletHandler) PostDebitarReserva(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == 0 {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	var req debitarReservaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}
	if req.Referencia == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "referencia é obrigatória")
		return
	}

	tx, err := h.db.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		slog.Error("[tpc_debitar_reserva] erro ao iniciar transação", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	// Busca e trava a transação pendente.
	var txID int64
	var valorStr, status, tipo string
	err = tx.QueryRow(r.Context(),
		`SELECT id, valor, status, tipo
		 FROM tpc_transacoes
		 WHERE user_id = $1 AND referencia = $2 AND tipo = 'debito'
		 ORDER BY id DESC
		 LIMIT 1
		 FOR UPDATE`,
		userID, req.Referencia,
	).Scan(&txID, &valorStr, &status, &tipo)

	if err == pgx.ErrNoRows {
		httpx.WriteErr(w, http.StatusNotFound, "reserva não encontrada")
		return
	}
	if err != nil {
		slog.Error("[tpc_debitar_reserva] erro ao buscar transação", "user_id", userID, "referencia", req.Referencia, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Idempotência: já confirmada → retorna ok sem reprocessar.
	if status == "confirmado" {
		tx.Rollback(r.Context()) //nolint:errcheck
		slog.Info("[tpc_debitar_reserva] reserva já confirmada (idempotente)",
			"user_id", userID, "referencia", req.Referencia, "tx_id", txID)
		httpx.WriteOK(w, map[string]any{"transacao_id": txID, "idempotente": true})
		return
	}
	if status != "pendente" {
		httpx.WriteErr(w, http.StatusConflict, "reserva em status inválido: "+status)
		return
	}

	valor, err := decimal.NewFromString(valorStr)
	if err != nil {
		slog.Error("[tpc_debitar_reserva] valor inválido na transação", "tx_id", txID, "valor", valorStr)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Lock da carteira.
	saldo, reservado, err := lockCarteira(r.Context(), tx, userID)
	if err != nil {
		slog.Error("[tpc_debitar_reserva] erro ao obter lock da carteira", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Validações de segurança (espelha PHP: saldo < valor || reservado < valor → rollback).
	if saldo.LessThan(valor) || reservado.LessThan(valor) {
		slog.Warn("[tpc_debitar_reserva] saldo ou reserva insuficiente para confirmar débito",
			"user_id", userID,
			"saldo", saldo.StringFixed(2),
			"reservado", reservado.StringFixed(2),
			"valor", valor.StringFixed(2),
		)
		httpx.WriteErr(w, http.StatusPaymentRequired, "saldo insuficiente para confirmar débito")
		return
	}

	novoSaldo := saldo.Sub(valor)
	novoReservado := decimal.Max(decimal.Zero, reservado.Sub(valor))

	_, err = tx.Exec(r.Context(),
		`UPDATE tpc_carteira
		 SET saldo = $1, saldo_reservado = $2
		 WHERE user_id = $3`,
		novoSaldo.StringFixed(2), novoReservado.StringFixed(2), userID,
	)
	if err != nil {
		slog.Error("[tpc_debitar_reserva] erro ao atualizar carteira", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	_, err = tx.Exec(r.Context(),
		`UPDATE tpc_transacoes
		 SET status = 'confirmado', saldo_apos = $1
		 WHERE id = $2 AND status = 'pendente'`,
		novoSaldo.StringFixed(2), txID,
	)
	if err != nil {
		slog.Error("[tpc_debitar_reserva] erro ao confirmar transação", "tx_id", txID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("[tpc_debitar_reserva] erro ao commit", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	slog.Info("[tpc_debitar_reserva] reserva debitada",
		"user_id", userID,
		"referencia", req.Referencia,
		"valor", valor.StringFixed(2),
		"tx_id", txID,
	)
	httpx.WriteOK(w, map[string]any{"transacao_id": txID, "saldo": novoSaldo.StringFixed(2)})
}

// ─── POST /carteira/creditar ─────────────────────────────────────────────────

// PostCreditar credita um valor na carteira (ex: recarga PIX confirmada).
//
// Semântica (espelha tpc_movimentar tipo=credito em wallet.php):
//  1. Garante linha na carteira (upsert).
//  2. SELECT FOR UPDATE na carteira.
//  3. Verifica idempotência via referencia (dentro da transação).
//  4. saldo += valor.
//  5. INSERT tpc_transacoes tipo=credito status=confirmado.
func (h *WalletHandler) PostCreditar(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == 0 {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	var req creditarRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}
	if req.Valor.LessThanOrEqual(decimal.Zero) {
		httpx.WriteErr(w, http.StatusBadRequest, "valor deve ser maior que zero")
		return
	}
	if req.Referencia == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "referencia é obrigatória")
		return
	}

	tx, err := h.db.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		slog.Error("[tpc_creditar] erro ao iniciar transação", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	if err := upsertCarteira(r.Context(), tx, userID); err != nil {
		slog.Error("[tpc_creditar] erro ao criar carteira", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	saldo, _, err := lockCarteira(r.Context(), tx, userID)
	if err != nil {
		slog.Error("[tpc_creditar] erro ao obter lock da carteira", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	novoSaldo := saldo.Add(req.Valor)

	_, err = tx.Exec(r.Context(),
		`UPDATE tpc_carteira SET saldo = $1 WHERE user_id = $2`,
		novoSaldo.StringFixed(2), userID,
	)
	if err != nil {
		slog.Error("[tpc_creditar] erro ao atualizar saldo", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// INSERT idempotente via ON CONFLICT (user_id, referencia, tipo) DO NOTHING.
	var txID int64
	err = tx.QueryRow(r.Context(),
		`INSERT INTO tpc_transacoes
		    (user_id, tipo, valor, saldo_apos, descricao, referencia, status)
		 VALUES ($1, 'credito', $2, $3, $4, $5, 'confirmado')
		 ON CONFLICT (user_id, referencia, tipo) DO NOTHING
		 RETURNING id`,
		userID,
		req.Valor.StringFixed(2),
		novoSaldo.StringFixed(2),
		req.Descricao,
		req.Referencia,
	).Scan(&txID)

	if err == pgx.ErrNoRows {
		// Crédito já processado anteriormente — rollback e retorna id existente.
		tx.Rollback(r.Context()) //nolint:errcheck
		var existingID int64
		err2 := h.db.QueryRow(r.Context(),
			`SELECT id FROM tpc_transacoes
			 WHERE user_id = $1 AND referencia = $2 AND tipo = 'credito' AND status = 'confirmado'
			 LIMIT 1`,
			userID, req.Referencia,
		).Scan(&existingID)
		if err2 != nil {
			slog.Error("[tpc_creditar] erro ao buscar crédito existente", "user_id", userID, "referencia", req.Referencia, "err", err2)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
			return
		}
		slog.Info("[tpc_creditar] crédito já existente (idempotente)",
			"user_id", userID, "referencia", req.Referencia, "tx_id", existingID)
		httpx.WriteOK(w, map[string]any{"transacao_id": existingID, "idempotente": true})
		return
	}
	if err != nil {
		slog.Error("[tpc_creditar] erro ao inserir transação", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("[tpc_creditar] erro ao commit", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	slog.Info("[tpc_creditar] crédito realizado",
		"user_id", userID,
		"valor", req.Valor.StringFixed(2),
		"referencia", req.Referencia,
		"tx_id", txID,
	)
	httpx.WriteOK(w, map[string]any{
		"transacao_id": txID,
		"saldo":        novoSaldo.StringFixed(2),
	})
}

// ─── POST /carteira/liberar-reserva ─────────────────────────────────────────

// PostLiberarReserva cancela uma reserva pendente, devolvendo o valor ao saldo disponível.
//
// Semântica (espelha tpc_liberar_reserva em wallet.php):
//  1. SELECT FOR UPDATE na transação pelo referencia (tipo=debito, status=pendente).
//  2. SELECT FOR UPDATE na carteira.
//  3. saldo_reservado -= valor (mínimo 0).
//  4. tx status → cancelado.
func (h *WalletHandler) PostLiberarReserva(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == 0 {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	var req liberarReservaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}
	if req.Referencia == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "referencia é obrigatória")
		return
	}

	tx, err := h.db.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		slog.Error("[tpc_liberar_reserva] erro ao iniciar transação", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	// Busca e trava a transação pendente.
	var txID int64
	var valorStr, status string
	err = tx.QueryRow(r.Context(),
		`SELECT id, valor, status
		 FROM tpc_transacoes
		 WHERE user_id = $1 AND referencia = $2 AND tipo = 'debito'
		 ORDER BY id DESC
		 LIMIT 1
		 FOR UPDATE`,
		userID, req.Referencia,
	).Scan(&txID, &valorStr, &status)

	if err == pgx.ErrNoRows {
		httpx.WriteErr(w, http.StatusNotFound, "reserva não encontrada")
		return
	}
	if err != nil {
		slog.Error("[tpc_liberar_reserva] erro ao buscar transação", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Idempotência: já cancelada → ok.
	if status == "cancelado" {
		tx.Rollback(r.Context()) //nolint:errcheck
		slog.Info("[tpc_liberar_reserva] reserva já cancelada (idempotente)",
			"user_id", userID, "referencia", req.Referencia, "tx_id", txID)
		httpx.WriteOK(w, map[string]any{"transacao_id": txID, "idempotente": true})
		return
	}
	if status != "pendente" {
		httpx.WriteErr(w, http.StatusConflict, "reserva em status inválido: "+status)
		return
	}

	valor, err := decimal.NewFromString(valorStr)
	if err != nil {
		slog.Error("[tpc_liberar_reserva] valor inválido na transação", "tx_id", txID, "valor", valorStr)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Lock da carteira.
	_, reservado, err := lockCarteira(r.Context(), tx, userID)
	if err != nil {
		slog.Error("[tpc_liberar_reserva] erro ao obter lock da carteira", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	novoReservado := decimal.Max(decimal.Zero, reservado.Sub(valor))

	_, err = tx.Exec(r.Context(),
		`UPDATE tpc_carteira SET saldo_reservado = $1 WHERE user_id = $2`,
		novoReservado.StringFixed(2), userID,
	)
	if err != nil {
		slog.Error("[tpc_liberar_reserva] erro ao atualizar carteira", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Cancela a transação, preservando o motivo no campo descricao.
	descSuffix := ""
	if req.Motivo != "" {
		descSuffix = " | Liberado: " + req.Motivo
	}
	_, err = tx.Exec(r.Context(),
		`UPDATE tpc_transacoes
		 SET status = 'cancelado',
		     descricao = LEFT(descricao || $1, 255)
		 WHERE id = $2 AND status = 'pendente'`,
		descSuffix, txID,
	)
	if err != nil {
		slog.Error("[tpc_liberar_reserva] erro ao cancelar transação", "tx_id", txID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("[tpc_liberar_reserva] erro ao commit", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	slog.Info("[tpc_liberar_reserva] reserva liberada",
		"user_id", userID,
		"referencia", req.Referencia,
		"valor", valor.StringFixed(2),
		"tx_id", txID,
	)
	httpx.WriteOK(w, map[string]any{"transacao_id": txID})
}
