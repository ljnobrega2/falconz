// Package handlers — endpoint interno para double-write do PHP (Fase 2: Carteira + PIX).
//
// Recebe replicação assíncrona do WordPress durante janela de migração strangler fig.
// O MySQL/WordPress continua sendo source of truth. Este handler apenas espelha
// as escritas no Postgres para que o serviço Go possa servir leituras.
//
// Auth: HMAC-SHA256 via header X-Internal-Sig (env WALLET_INTERNAL_SECRET).
// Fail-closed: WALLET_INTERNAL_SECRET vazio → rotas retornam 503.
//
// Remover após cutover confirmado via infra/scripts/verify-migration.sh.
package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/wallet-service/internal/httpx"
)

// InternalHandler recebe payloads de double-write do PHP (Carteira + PIX).
type InternalHandler struct {
	Pool   *pgxpool.Pool
	secret string // WALLET_INTERNAL_SECRET
}

// NewInternalHandler cria um InternalHandler validando que o secret está configurado.
// Retorna (nil, nil) se WALLET_INTERNAL_SECRET não estiver definido — double-write
// desativado. O main.go deve verificar nil antes de registrar as rotas.
func NewInternalHandler(pool *pgxpool.Pool) (*InternalHandler, error) {
	s := os.Getenv("WALLET_INTERNAL_SECRET")
	if s == "" {
		// Double-write desativado — sem secret configurado.
		// Rotas /internal/* não serão registradas.
		slog.Warn("[tpc_internal] WALLET_INTERNAL_SECRET ausente — rotas /internal/* desativadas")
		return nil, nil
	}
	return &InternalHandler{Pool: pool, secret: s}, nil
}

// verifyHMAC valida o header X-Internal-Sig contra o body lido.
// Usa comparação em tempo constante (hmac.Equal) para evitar timing attacks.
func (h *InternalHandler) verifyHMAC(sig string, body []byte) bool {
	mac := hmac.New(sha256.New, []byte(h.secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(expected))
}

// readBodyInternal lê e limita o body a 1 MB.
func readBodyInternal(r *http.Request) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r.Body, 1<<20))
}

// ── POST /internal/transacoes ────────────────────────────────────────────────

// InserirTransacao recebe replicação de INSERT em wp_tpc_transacoes.
//
// Idempotência: ON CONFLICT (user_id, referencia, tipo) DO NOTHING.
// Se referencia for NULL ou vazia, a linha é inserida sem checar conflito
// (NULL != NULL no SQL — sem risco de falso-conflito).
func (h *InternalHandler) InserirTransacao(w http.ResponseWriter, r *http.Request) {
	body, err := readBodyInternal(r)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo inválido")
		return
	}

	if !h.verifyHMAC(r.Header.Get("X-Internal-Sig"), body) {
		slog.Warn("[tpc_internal] assinatura inválida em /internal/transacoes", "ip", r.RemoteAddr)
		httpx.WriteErr(w, http.StatusUnauthorized, "assinatura inválida")
		return
	}

	var p map[string]any
	if err := json.Unmarshal(body, &p); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	userID := internalToInt64(p["user_id"])
	tipo := internalToString(p["tipo"])
	valor := internalToString(p["valor"])
	if userID == 0 || tipo == "" || valor == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "user_id, tipo e valor são obrigatórios")
		return
	}

	referencia := internalToStringPtr(p["referencia"])
	descricao := internalToString(p["descricao"])
	status := internalToString(p["status"])
	if status == "" {
		status = "confirmado"
	}
	saldoApos := internalToString(p["saldo_apos"])
	if saldoApos == "" {
		saldoApos = "0.00"
	}
	orderID := internalToInt64Ptr(p["order_id"])
	meOrderID := internalToStringPtr(p["me_order_id"])

	ctx := r.Context()
	_, err = h.Pool.Exec(ctx,
		`INSERT INTO tpc_transacoes
		    (user_id, tipo, valor, saldo_apos, descricao, referencia,
		     order_id, me_order_id, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 ON CONFLICT (user_id, referencia, tipo) DO NOTHING`,
		userID, tipo, valor, saldoApos, descricao, referencia,
		orderID, meOrderID, status,
	)
	if err != nil {
		slog.Error("[tpc_internal] falha ao inserir transação",
			"user_id", userID, "tipo", tipo, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao salvar transação")
		return
	}

	slog.Info("[tpc_internal] transação replicada", "user_id", userID, "tipo", tipo, "referencia", referencia)
	httpx.WriteOK(w, map[string]any{"ok": true, "user_id": userID})
}

// ── POST /internal/recargas ──────────────────────────────────────────────────

// InserirRecarga recebe replicação de INSERT em wp_tpc_recargas.
//
// Idempotência: ON CONFLICT (me_pix_id) DO NOTHING.
// Se me_pix_id for NULL, usa fallback de ID do MySQL para não colidir.
func (h *InternalHandler) InserirRecarga(w http.ResponseWriter, r *http.Request) {
	body, err := readBodyInternal(r)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo inválido")
		return
	}

	if !h.verifyHMAC(r.Header.Get("X-Internal-Sig"), body) {
		slog.Warn("[tpc_internal] assinatura inválida em /internal/recargas", "ip", r.RemoteAddr)
		httpx.WriteErr(w, http.StatusUnauthorized, "assinatura inválida")
		return
	}

	var p map[string]any
	if err := json.Unmarshal(body, &p); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	userID := internalToInt64(p["user_id"])
	valor := internalToString(p["valor"])
	if userID == 0 || valor == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "user_id e valor são obrigatórios")
		return
	}

	status := internalToString(p["status"])
	if status == "" {
		status = "pendente"
	}
	mePixID := internalToStringPtr(p["me_pix_id"])
	pixQR := internalToStringPtr(p["pix_qr"])
	pixCodigo := internalToStringPtr(p["pix_codigo"])
	expiresAt := internalToStringPtr(p["expires_at"])

	ctx := r.Context()
	_, err = h.Pool.Exec(ctx,
		`INSERT INTO tpc_recargas
		    (user_id, valor, status, me_pix_id, pix_qr, pix_codigo, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6,
		     CASE WHEN $7::text IS NOT NULL
		          THEN $7::timestamptz
		          ELSE NOW() + INTERVAL '24 hours'
		     END)
		 ON CONFLICT (me_pix_id) DO NOTHING`,
		userID, valor, status, mePixID, pixQR, pixCodigo, expiresAt,
	)
	if err != nil {
		slog.Error("[tpc_internal] falha ao inserir recarga",
			"user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao salvar recarga")
		return
	}

	slog.Info("[tpc_internal] recarga replicada", "user_id", userID, "me_pix_id", mePixID)
	httpx.WriteOK(w, map[string]any{"ok": true, "user_id": userID})
}

// ── POST /internal/recargas/{id}/confirmar ───────────────────────────────────

// ConfirmarRecarga recebe replicação de confirmação de pagamento PIX.
//
// Atualiza status → 'confirmado' e paid_at no Postgres.
// NÃO credita a carteira aqui — o crédito já foi feito pelo handler PIX
// (pix.go / confirmarRecarga). Este endpoint apenas sincroniza o status
// da recarga para o Postgres refletir o MySQL.
func (h *InternalHandler) ConfirmarRecarga(w http.ResponseWriter, r *http.Request) {
	body, err := readBodyInternal(r)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo inválido")
		return
	}

	if !h.verifyHMAC(r.Header.Get("X-Internal-Sig"), body) {
		slog.Warn("[tpc_internal] assinatura inválida em /internal/recargas/{id}/confirmar", "ip", r.RemoteAddr)
		httpx.WriteErr(w, http.StatusUnauthorized, "assinatura inválida")
		return
	}

	// Extrai recarga_id do path parameter.
	recargaIDStr := chi.URLParam(r, "id")
	recargaID, err := strconv.ParseInt(recargaIDStr, 10, 64)
	if err != nil || recargaID == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "recarga id inválido no path")
		return
	}

	var p map[string]any
	if err := json.Unmarshal(body, &p); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	ctx := r.Context()
	tag, err := h.Pool.Exec(ctx,
		`UPDATE tpc_recargas
		 SET status = 'confirmado',
		     paid_at = NOW()
		 WHERE id = $1
		   AND status != 'confirmado'`,
		recargaID,
	)
	if err != nil {
		slog.Error("[tpc_internal] falha ao confirmar recarga",
			"recarga_id", recargaID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao confirmar recarga")
		return
	}

	rows := tag.RowsAffected()
	slog.Info("[tpc_internal] recarga confirmada replicada",
		"recarga_id", recargaID, "rows_affected", rows)
	httpx.WriteOK(w, map[string]any{"ok": true, "recarga_id": recargaID, "rows": rows})
}

// ── POST /internal/carteira/{user_id} ───────────────────────────────────────

// UpsertCarteira recebe replicação de UPDATE em wp_tpc_carteira.
//
// Faz UPSERT: se o usuário ainda não existe no Postgres, insere a linha.
// Se já existe, sobrescreve saldo e saldo_reservado com os valores do MySQL
// (eventual consistency durante a janela de migração).
func (h *InternalHandler) UpsertCarteira(w http.ResponseWriter, r *http.Request) {
	body, err := readBodyInternal(r)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo inválido")
		return
	}

	if !h.verifyHMAC(r.Header.Get("X-Internal-Sig"), body) {
		slog.Warn("[tpc_internal] assinatura inválida em /internal/carteira/{user_id}", "ip", r.RemoteAddr)
		httpx.WriteErr(w, http.StatusUnauthorized, "assinatura inválida")
		return
	}

	// Extrai user_id do path parameter.
	userIDStr := chi.URLParam(r, "user_id")
	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil || userID == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "user_id inválido no path")
		return
	}

	var p map[string]any
	if err := json.Unmarshal(body, &p); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	saldo := internalToString(p["saldo"])
	saldoReservado := internalToString(p["saldo_reservado"])
	if saldo == "" {
		saldo = "0.00"
	}
	if saldoReservado == "" {
		saldoReservado = "0.00"
	}

	ctx := r.Context()
	_, err = h.Pool.Exec(ctx,
		`INSERT INTO tpc_carteira (user_id, saldo, saldo_reservado)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (user_id) DO UPDATE
		     SET saldo           = EXCLUDED.saldo,
		         saldo_reservado = EXCLUDED.saldo_reservado`,
		userID, saldo, saldoReservado,
	)
	if err != nil {
		slog.Error("[tpc_internal] falha ao fazer upsert da carteira",
			"user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao atualizar carteira")
		return
	}

	slog.Info("[tpc_internal] carteira replicada",
		"user_id", userID, "saldo", saldo, "saldo_reservado", saldoReservado)
	httpx.WriteOK(w, map[string]any{"ok": true, "user_id": userID})
}

// RegisterInternalRoutes registra as rotas /internal/* no router da carteira.
// Só deve ser chamado se handler != nil (WALLET_INTERNAL_SECRET configurado).
// Fail-closed: se o handler for nil (secret ausente), o bloco /internal/* fica
// inacessível — retorna 503 para qualquer requisição não roteada.
func RegisterInternalRoutes(r chi.Router, h *InternalHandler) {
	r.Route("/internal", func(r chi.Router) {
		r.Post("/transacoes", h.InserirTransacao)
		r.Post("/recargas", h.InserirRecarga)
		r.Post("/recargas/{id}/confirmar", h.ConfirmarRecarga)
		r.Post("/carteira/{user_id}", h.UpsertCarteira)
		// Reserva de saldo para emissão de etiqueta Melhor Envio (labels-service CRIT-01).
		r.Post("/reservar", h.Reservar)
		r.Post("/debitar-reserva", h.DebitarReserva)
		r.Post("/liberar-reserva", h.LiberarReserva)
		r.Post("/estornar", h.Estornar)
		// AUDIT-2026-07-28 (dono): cancelamento de expedição NUNCA credita saldo
		// antes do reembolso realmente cair na conta pool da Melhor Envio (2-24h,
		// "aguardando análise" do lado ME). estornar-pendente registra a intenção
		// (status='pendente', saldo intocado); confirmar-estorno credita de fato
		// quando o reembolso ME for confirmado (hoje: ação manual de ops).
		r.Post("/estornar-pendente", h.EstornarPendente)
		r.Post("/confirmar-estorno", h.ConfirmarEstorno)
	})
}

// ── POST /internal/reservar ──────────────────────────────────────────────────

// Reservar reserva `valor` na carteira do `user_id` ANTES de ME.CreateShipment.
// Espelha tpc_reservar(): INSERT tipo='reserva' status='pendente' + move saldo→reservado.
// Idempotente por (user_id, referencia, tipo='reserva') — retry-safe.
// Fail-closed: saldo insuficiente → 402.
func (h *InternalHandler) Reservar(w http.ResponseWriter, r *http.Request) {
	body, err := readBodyInternal(r)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if !h.verifyHMAC(r.Header.Get("X-Internal-Sig"), body) {
		slog.Warn("[tpc_internal] assinatura inválida em /internal/reservar", "ip", r.RemoteAddr)
		httpx.WriteErr(w, http.StatusUnauthorized, "assinatura inválida")
		return
	}
	var p map[string]any
	if err := json.Unmarshal(body, &p); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	userID := internalToInt64(p["user_id"])
	valor := internalToString(p["valor"])
	descricao := internalToString(p["descricao"])
	referencia := internalToString(p["referencia"])
	orderID := internalToInt64Ptr(p["order_id"])
	if userID == 0 || valor == "" || referencia == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "user_id, valor e referencia são obrigatórios")
		return
	}

	ctx := r.Context()
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao iniciar transação")
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Verifica saldo disponível com lock (evita double-spend).
	var saldoAtual float64
	err = tx.QueryRow(ctx,
		`SELECT saldo FROM tpc_carteira WHERE user_id = $1 FOR UPDATE`, userID).
		Scan(&saldoAtual)
	if err != nil {
		slog.Error("[tpc_internal] Reservar: carteira não encontrada", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusNotFound, "carteira não encontrada para este usuário")
		return
	}

	// AD-HOC (2026-08-27, pedido do owner): user 51 (sac@gestao.io) pode completar
	// reserva puxando do pool de receita da plataforma (senderzz_revenue) quando o
	// saldo próprio não cobre. Hardcoded pra esse user_id só — não é flag geral.
	if userID == 51 {
		valorF, convErr := strconv.ParseFloat(valor, 64)
		if convErr != nil {
			httpx.WriteErr(w, http.StatusBadRequest, "valor inválido")
			return
		}
		if saldoAtual < valorF {
			falta := valorF - saldoAtual
			var poolDisponivel float64
			if err := tx.QueryRow(ctx,
				`SELECT COALESCE(SUM(amount),0) FROM senderzz_revenue`).Scan(&poolDisponivel); err != nil {
				httpx.WriteErr(w, http.StatusInternalServerError, "erro ao consultar pool global")
				return
			}
			if poolDisponivel < falta {
				slog.Warn("[tpc_internal] Reservar: pool global insuficiente", "user_id", userID, "falta", falta, "pool", poolDisponivel)
				httpx.WriteErr(w, http.StatusPaymentRequired, "saldo insuficiente")
				return
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO senderzz_revenue (produtor_id, component, base_amount, amount, ref)
				 VALUES ($1, 'pool_draw_user51', $2::numeric, -$2::numeric, $3)
				 ON CONFLICT (component, ref) DO NOTHING`,
				userID, falta, "pool_draw:"+referencia,
			); err != nil {
				slog.Error("[tpc_internal] Reservar: erro ao debitar pool global", "user_id", userID, "err", err)
				httpx.WriteErr(w, http.StatusInternalServerError, "erro ao debitar pool global")
				return
			}
			if _, err := tx.Exec(ctx,
				`UPDATE tpc_carteira SET saldo = saldo + $2::numeric WHERE user_id = $1`,
				userID, falta,
			); err != nil {
				slog.Error("[tpc_internal] Reservar: erro ao creditar saldo via pool global", "user_id", userID, "err", err)
				httpx.WriteErr(w, http.StatusInternalServerError, "erro ao creditar saldo via pool global")
				return
			}
			saldoAtual = valorF
		}
	}

	// INSERT idempotente: DO NOTHING em conflito (user_id, referencia, tipo='reserva').
	// Se retornar 0 linhas = reserva já existe → retorna tx_id existente SEM mover saldo.
	var txID int64
	err = tx.QueryRow(ctx,
		`INSERT INTO tpc_transacoes
		    (user_id, tipo, valor, saldo_apos, descricao, referencia, order_id, status)
		 VALUES ($1, 'reserva', $2, (
		     SELECT saldo - $2::numeric FROM tpc_carteira WHERE user_id = $1
		 ), $3, $4, $5, 'pendente')
		 ON CONFLICT (user_id, referencia, tipo) DO NOTHING
		 RETURNING id`,
		userID, valor, descricao, referencia, orderID,
	).Scan(&txID)
	if err != nil {
		// pgx retorna pgx.ErrNoRows quando DO NOTHING suprimiu o INSERT.
		if errors.Is(err, pgx.ErrNoRows) {
			// Conflito — verifica status da tx existente.
			var existingID int64
			var existingStatus string
			qErr := tx.QueryRow(ctx,
				`SELECT id, status FROM tpc_transacoes
				  WHERE user_id=$1 AND referencia=$2 AND tipo='reserva'`,
				userID, referencia,
			).Scan(&existingID, &existingStatus)
			if qErr != nil {
				httpx.WriteErr(w, http.StatusInternalServerError, "erro ao localizar reserva existente")
				return
			}
			if existingStatus == "pendente" {
				// Reserva viva — idempotente, sem mover saldo.
				if err := tx.Commit(ctx); err != nil {
					httpx.WriteErr(w, http.StatusInternalServerError, "erro ao confirmar")
					return
				}
				httpx.WriteOK(w, map[string]any{"ok": true, "tx_id": existingID, "idempotent": true})
				return
			}
			// Reserva cancelada (rollback anterior) — reseta para nova tentativa.
			// DELETE + re-INSERT permite reutilizar a mesma referência sem vazar saldo.
			if _, delErr := tx.Exec(ctx,
				`DELETE FROM tpc_transacoes WHERE id=$1`, existingID,
			); delErr != nil {
				httpx.WriteErr(w, http.StatusInternalServerError, "erro ao resetar reserva cancelada")
				return
			}
			// Re-INSERT: referência livre após DELETE.
			if reinsErr := tx.QueryRow(ctx,
				`INSERT INTO tpc_transacoes
				    (user_id, tipo, valor, saldo_apos, descricao, referencia, order_id, status)
				 VALUES ($1, 'reserva', $2, (
				     SELECT saldo - $2::numeric FROM tpc_carteira WHERE user_id = $1
				 ), $3, $4, $5, 'pendente')
				 RETURNING id`,
				userID, valor, descricao, referencia, orderID,
			).Scan(&txID); reinsErr != nil {
				httpx.WriteErr(w, http.StatusInternalServerError, "erro ao criar nova reserva")
				return
			}
			// Reset com sucesso — segue pro bloco de UPDATE saldo abaixo (NÃO cair
			// no erro genérico que tratava o pgx.ErrNoRows original como falha real).
		} else {
			// AUDIT-2026-07-27: erro real (não é o ErrNoRows do ON CONFLICT DO NOTHING)
			// — antes este slog.Error+WriteErr rodava também no caminho de SUCESSO do
			// reset acima (fallthrough sem return), fazendo /internal/reservar responder
			// 500 mesmo após recriar a reserva com êxito. "saldo insuficiente" falso
			// pra qualquer retry de emissão de etiqueta que batesse numa reserva
			// cancelada antes (ex.: pedido 1633 — 1ª tentativa falhou no ME, reserva
			// foi cancelada, 2ª tentativa nunca conseguia reservar de novo).
			slog.Error("[tpc_internal] Reservar: erro ao inserir transação", "user_id", userID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao registrar reserva")
			return
		}
	}

	// Move saldo → saldo_reservado (verifica novamente saldo suficiente sob lock).
	tag, err := tx.Exec(ctx,
		`UPDATE tpc_carteira
		    SET saldo           = saldo           - $2::numeric,
		        saldo_reservado = saldo_reservado + $2::numeric
		  WHERE user_id = $1
		    AND saldo >= $2::numeric`,
		userID, valor,
	)
	if err != nil {
		slog.Error("[tpc_internal] Reservar: erro ao mover saldo", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao reservar saldo")
		return
	}
	if tag.RowsAffected() == 0 {
		// saldo insuficiente
		slog.Warn("[tpc_internal] Reservar: saldo insuficiente", "user_id", userID, "valor", valor, "saldo", saldoAtual)
		httpx.WriteErr(w, http.StatusPaymentRequired, "saldo insuficiente")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao confirmar reserva")
		return
	}
	slog.Info("[tpc_internal] Reservar: saldo reservado",
		"user_id", userID, "valor", valor, "tx_id", txID, "referencia", referencia)
	httpx.WriteOK(w, map[string]any{"ok": true, "tx_id": txID})
}

// ── POST /internal/debitar-reserva ───────────────────────────────────────────

// DebitarReserva confirma o débito após criação bem-sucedida da etiqueta.
// Espelha tpc_debitar_reserva(): muda status pendente→confirmado + baixa saldo_reservado.
// Idempotente: se já confirmado, retorna ok sem modificar.
func (h *InternalHandler) DebitarReserva(w http.ResponseWriter, r *http.Request) {
	body, err := readBodyInternal(r)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if !h.verifyHMAC(r.Header.Get("X-Internal-Sig"), body) {
		slog.Warn("[tpc_internal] assinatura inválida em /internal/debitar-reserva", "ip", r.RemoteAddr)
		httpx.WriteErr(w, http.StatusUnauthorized, "assinatura inválida")
		return
	}
	var p map[string]any
	if err := json.Unmarshal(body, &p); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	txID := internalToInt64(p["tx_id"])
	meOrderID := internalToStringPtr(p["me_order_id"])
	if txID == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "tx_id é obrigatório")
		return
	}

	ctx := r.Context()
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao iniciar transação")
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var userID int64
	var valor float64
	var status string
	err = tx.QueryRow(ctx,
		`SELECT user_id, valor, status FROM tpc_transacoes
		  WHERE id = $1 AND tipo = 'reserva' FOR UPDATE`, txID).
		Scan(&userID, &valor, &status)
	if err != nil {
		slog.Error("[tpc_internal] DebitarReserva: transação não encontrada", "tx_id", txID, "err", err)
		httpx.WriteErr(w, http.StatusNotFound, "reserva não encontrada")
		return
	}
	if status == "confirmado" {
		// Já debitado (idempotente).
		_ = tx.Commit(ctx)
		httpx.WriteOK(w, map[string]any{"ok": true, "idempotent": true})
		return
	}
	if status != "pendente" {
		httpx.WriteErr(w, http.StatusConflict, "reserva não está pendente (status: "+status+")")
		return
	}

	_, err = tx.Exec(ctx,
		`UPDATE tpc_transacoes
		    SET status = 'confirmado', me_order_id = $2
		  WHERE id = $1`,
		txID, meOrderID,
	)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao confirmar débito")
		return
	}
	_, err = tx.Exec(ctx,
		`UPDATE tpc_carteira
		    SET saldo_reservado = saldo_reservado - $2
		  WHERE user_id = $1`,
		userID, valor,
	)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao baixar saldo_reservado")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao confirmar")
		return
	}
	slog.Info("[tpc_internal] DebitarReserva: débito confirmado",
		"user_id", userID, "tx_id", txID, "valor", valor)
	httpx.WriteOK(w, map[string]any{"ok": true})
}

// ── POST /internal/estornar ──────────────────────────────────────────────────

// Estornar credita de volta um valor JÁ DEBITADO (ex.: cancelamento de etiqueta
// depois da emissão — a reserva já virou débito confirmado, LiberarReserva não
// serve mais aqui, que só desfaz reserva PENDENTE). Idempotente por
// (user_id, referencia, tipo='credito') — igual ao padrão de Reservar.
//
// AUDIT-2026-07-27 (pedido do dono): "eliminar aquela etiqueta cancelada essa
// função precisa estar funcionando" — cancelar etiqueta emitida não devolvia
// dinheiro nenhum porque essa rota não existia.
func (h *InternalHandler) Estornar(w http.ResponseWriter, r *http.Request) {
	body, err := readBodyInternal(r)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if !h.verifyHMAC(r.Header.Get("X-Internal-Sig"), body) {
		slog.Warn("[tpc_internal] assinatura inválida em /internal/estornar", "ip", r.RemoteAddr)
		httpx.WriteErr(w, http.StatusUnauthorized, "assinatura inválida")
		return
	}
	var p map[string]any
	if err := json.Unmarshal(body, &p); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	userID := internalToInt64(p["user_id"])
	valor := internalToString(p["valor"])
	descricao := internalToString(p["descricao"])
	referencia := internalToString(p["referencia"])
	orderID := internalToInt64Ptr(p["order_id"])
	if userID == 0 || valor == "" || referencia == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "user_id, valor e referencia são obrigatórios")
		return
	}

	ctx := r.Context()
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao iniciar transação")
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var txID int64
	err = tx.QueryRow(ctx,
		`INSERT INTO tpc_transacoes
		    (user_id, tipo, valor, saldo_apos, descricao, referencia, order_id, status)
		 VALUES ($1, 'credito', $2, (
		     SELECT saldo + $2::numeric FROM tpc_carteira WHERE user_id = $1
		 ), $3, $4, $5, 'confirmado')
		 ON CONFLICT (user_id, referencia, tipo) DO NOTHING
		 RETURNING id`,
		userID, valor, descricao, referencia, orderID,
	).Scan(&txID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Já estornado antes (mesma referência) — idempotente, sem mover saldo de novo.
			if commitErr := tx.Commit(ctx); commitErr != nil {
				httpx.WriteErr(w, http.StatusInternalServerError, "erro ao confirmar")
				return
			}
			httpx.WriteOK(w, map[string]any{"ok": true, "idempotent": true})
			return
		}
		slog.Error("[tpc_internal] Estornar: erro ao inserir transação", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao registrar estorno")
		return
	}

	if _, err := tx.Exec(ctx,
		`UPDATE tpc_carteira SET saldo = saldo + $2::numeric WHERE user_id = $1`,
		userID, valor,
	); err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao creditar carteira")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao confirmar")
		return
	}
	slog.Info("[tpc_internal] Estornar: estorno confirmado", "user_id", userID, "tx_id", txID, "valor", valor)
	httpx.WriteOK(w, map[string]any{"ok": true, "tx_id": txID})
}

// ── POST /internal/estornar-pendente ─────────────────────────────────────────

// EstornarPendente registra a INTENÇÃO de estorno (cancelamento de etiqueta ME)
// SEM mexer no saldo — status='pendente', igual Reservar. O saldo só é creditado
// de fato em ConfirmarEstorno, quando o reembolso já tiver caído na conta pool
// da Melhor Envio. Idempotente por (user_id, referencia, tipo='credito').
func (h *InternalHandler) EstornarPendente(w http.ResponseWriter, r *http.Request) {
	body, err := readBodyInternal(r)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if !h.verifyHMAC(r.Header.Get("X-Internal-Sig"), body) {
		slog.Warn("[tpc_internal] assinatura inválida em /internal/estornar-pendente", "ip", r.RemoteAddr)
		httpx.WriteErr(w, http.StatusUnauthorized, "assinatura inválida")
		return
	}
	var p map[string]any
	if err := json.Unmarshal(body, &p); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	userID := internalToInt64(p["user_id"])
	valor := internalToString(p["valor"])
	descricao := internalToString(p["descricao"])
	referencia := internalToString(p["referencia"])
	orderID := internalToInt64Ptr(p["order_id"])
	if userID == 0 || valor == "" || referencia == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "user_id, valor e referencia são obrigatórios")
		return
	}

	ctx := r.Context()
	var txID int64
	err = h.Pool.QueryRow(ctx,
		`INSERT INTO tpc_transacoes
		    (user_id, tipo, valor, saldo_apos, descricao, referencia, order_id, status)
		 VALUES ($1, 'credito', $2, (
		     SELECT saldo FROM tpc_carteira WHERE user_id = $1
		 ), $3, $4, $5, 'pendente')
		 ON CONFLICT (user_id, referencia, tipo) DO NOTHING
		 RETURNING id`,
		userID, valor, descricao, referencia, orderID,
	).Scan(&txID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpx.WriteOK(w, map[string]any{"ok": true, "idempotent": true})
			return
		}
		slog.Error("[tpc_internal] EstornarPendente: erro ao inserir transação", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao registrar estorno pendente")
		return
	}
	slog.Info("[tpc_internal] EstornarPendente: estorno registrado, aguardando confirmação ME",
		"user_id", userID, "tx_id", txID, "valor", valor, "referencia", referencia)
	httpx.WriteOK(w, map[string]any{"ok": true, "tx_id": txID, "status": "pendente"})
}

// ── POST /internal/confirmar-estorno ─────────────────────────────────────────

// ConfirmarEstorno credita de fato o saldo de um estorno antes registrado como
// pendente (EstornarPendente), DEPOIS de confirmado que o reembolso caiu na
// conta pool da Melhor Envio. Idempotente: só age se ainda 'pendente'; chamada
// repetida na mesma referência não credita duas vezes.
func (h *InternalHandler) ConfirmarEstorno(w http.ResponseWriter, r *http.Request) {
	body, err := readBodyInternal(r)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if !h.verifyHMAC(r.Header.Get("X-Internal-Sig"), body) {
		slog.Warn("[tpc_internal] assinatura inválida em /internal/confirmar-estorno", "ip", r.RemoteAddr)
		httpx.WriteErr(w, http.StatusUnauthorized, "assinatura inválida")
		return
	}
	var p map[string]any
	if err := json.Unmarshal(body, &p); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	referencia := internalToString(p["referencia"])
	userID := internalToInt64(p["user_id"])
	if referencia == "" || userID == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "user_id e referencia são obrigatórios")
		return
	}

	ctx := r.Context()
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao iniciar transação")
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var txID int64
	var valor string
	err = tx.QueryRow(ctx,
		`UPDATE tpc_transacoes
		    SET status = 'confirmado'
		  WHERE user_id = $1 AND referencia = $2 AND tipo = 'credito' AND status = 'pendente'
		  RETURNING id, valor`,
		userID, referencia,
	).Scan(&txID, &valor)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Já confirmado antes, ou nunca existiu pendente com essa referência —
			// idempotente, não credita de novo.
			if commitErr := tx.Commit(ctx); commitErr != nil {
				httpx.WriteErr(w, http.StatusInternalServerError, "erro ao confirmar")
				return
			}
			httpx.WriteOK(w, map[string]any{"ok": true, "idempotent": true})
			return
		}
		slog.Error("[tpc_internal] ConfirmarEstorno: erro ao localizar pendente", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao confirmar estorno")
		return
	}

	if _, err := tx.Exec(ctx,
		`UPDATE tpc_carteira SET saldo = saldo + $2::numeric WHERE user_id = $1`,
		userID, valor,
	); err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao creditar carteira")
		return
	}
	if _, err := tx.Exec(ctx,
		`UPDATE tpc_transacoes SET saldo_apos = (SELECT saldo FROM tpc_carteira WHERE user_id = $1) WHERE id = $2`,
		userID, txID,
	); err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao atualizar saldo_apos")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao confirmar")
		return
	}
	slog.Info("[tpc_internal] ConfirmarEstorno: saldo creditado após confirmação ME",
		"user_id", userID, "tx_id", txID, "valor", valor, "referencia", referencia)
	httpx.WriteOK(w, map[string]any{"ok": true, "tx_id": txID})
}

// ── POST /internal/liberar-reserva ───────────────────────────────────────────

// LiberarReserva estorna uma reserva pendente (rollback de falha na emissão).
// Espelha tpc_liberar_reserva(): status→cancelado + devolve saldo ao disponível.
// Idempotente: se já cancelado, retorna ok sem modificar.
func (h *InternalHandler) LiberarReserva(w http.ResponseWriter, r *http.Request) {
	body, err := readBodyInternal(r)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if !h.verifyHMAC(r.Header.Get("X-Internal-Sig"), body) {
		slog.Warn("[tpc_internal] assinatura inválida em /internal/liberar-reserva", "ip", r.RemoteAddr)
		httpx.WriteErr(w, http.StatusUnauthorized, "assinatura inválida")
		return
	}
	var p map[string]any
	if err := json.Unmarshal(body, &p); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	txID := internalToInt64(p["tx_id"])
	if txID == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "tx_id é obrigatório")
		return
	}

	ctx := r.Context()
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao iniciar transação")
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var userID int64
	var valor float64
	var status string
	err = tx.QueryRow(ctx,
		`SELECT user_id, valor, status FROM tpc_transacoes
		  WHERE id = $1 AND tipo = 'reserva' FOR UPDATE`, txID).
		Scan(&userID, &valor, &status)
	if err != nil {
		slog.Error("[tpc_internal] LiberarReserva: transação não encontrada", "tx_id", txID, "err", err)
		httpx.WriteErr(w, http.StatusNotFound, "reserva não encontrada")
		return
	}
	if status == "cancelado" {
		// Já liberada (idempotente).
		_ = tx.Commit(ctx)
		httpx.WriteOK(w, map[string]any{"ok": true, "idempotent": true})
		return
	}
	if status != "pendente" {
		httpx.WriteErr(w, http.StatusConflict, "reserva já confirmada — não pode ser liberada (status: "+status+")")
		return
	}

	_, err = tx.Exec(ctx,
		`UPDATE tpc_transacoes SET status = 'cancelado' WHERE id = $1`, txID)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao cancelar reserva")
		return
	}
	_, err = tx.Exec(ctx,
		`UPDATE tpc_carteira
		    SET saldo           = saldo           + $2,
		        saldo_reservado = saldo_reservado - $2
		  WHERE user_id = $1`,
		userID, valor,
	)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao restaurar saldo")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao confirmar liberação")
		return
	}
	slog.Info("[tpc_internal] LiberarReserva: reserva liberada",
		"user_id", userID, "tx_id", txID, "valor", valor)
	httpx.WriteOK(w, map[string]any{"ok": true})
}

// ── helpers de conversão de tipos ────────────────────────────────────────────

// internalToInt64 converte float64, int64, string para int64.
// JSON decodifica números como float64 por padrão em map[string]any.
func internalToInt64(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case int64:
		return x
	case string:
		n, _ := strconv.ParseInt(x, 10, 64)
		return n
	}
	return 0
}

// internalToString retorna o valor como string, ou "" se nil.
func internalToString(v any) string {
	if v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

// internalToStringPtr retorna ponteiro para string, ou nil se vazio/nil.
func internalToStringPtr(v any) *string {
	s := internalToString(v)
	if s == "" {
		return nil
	}
	return &s
}

// internalToInt64Ptr retorna ponteiro para int64, ou nil se zero/nil.
func internalToInt64Ptr(v any) *int64 {
	n := internalToInt64(v)
	if n == 0 {
		return nil
	}
	return &n
}
