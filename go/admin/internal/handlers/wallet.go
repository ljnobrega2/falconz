package handlers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
	adminwallet "github.com/senderzz/admin-service/internal/wallet"
)

type WalletHandler struct {
	Pool         *pgxpool.Pool
	WalletClient *adminwallet.Client
}

func (h *WalletHandler) tableExists(ctx context.Context, name string) bool {
	return tableExistsCached(ctx, h.Pool, name) // AUDIT-2026-06-18 Onda2 (go-infoschema-cache)
}

type carteira struct {
	ID             int64   `json:"id"`
	UserID         int64   `json:"user_id"`
	Nome           string  `json:"nome"`
	Email          string  `json:"email"`
	Saldo          float64 `json:"saldo"`
	SaldoReservado float64 `json:"saldo_reservado"`
	CreatedAt      string  `json:"created_at"`
}

type transacao struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"user_id"`
	Tipo       string  `json:"tipo"`
	Valor      float64 `json:"valor"`
	SaldoApos  float64 `json:"saldo_apos"`
	Descricao  string  `json:"descricao"`
	Referencia *string `json:"referencia"`
	OrderID    *int64  `json:"order_id"`
	Status     string  `json:"status"`
	CreatedAt  string  `json:"created_at"`
}

func (h *WalletHandler) ListCarteiras(w http.ResponseWriter, r *http.Request) {
	if !h.tableExists(r.Context(), "tpc_carteira") {
		httpx.JSON(w, 200, map[string]any{"items": []carteira{}})
		return
	}
	// Carteira Expedição (TPC) é EXCLUSIVA de produtor — filtra role='produtor'.
	//
	// JOIN CANÔNICO (MIGRAÇÃO 2026-07-28): tpc_carteira.user_id/tpc_transacoes.user_id/
	// tpc_recargas.user_id foram migrados de wp_user_id pro id NATIVO do portal
	// (senderzz_portal_users.id) — produtor 100% FALK (sem WordPress) nunca tem
	// wp_user_id e ficava com carteira permanentemente inacessível. Casamos
	// EXCLUSIVAMENTE por pu.id = c.user_id (chave única, sem colisão de id-space —
	// era esse cruzamento id×wp que o join antigo por wp_user_id evitava com um
	// problema pior: excluía todo produtor nativo).
	//
	// GROUP BY pu.id mantém UMA linha por produtor. user_id devolvido =
	// pu.id (chave canônica). Nome/email do registro do produtor.
	// ISOLAMENTO COD (FALK): saldo EXIBIDO = só Expedição (frete). O saldo armazenado
	// em tpc_carteira está contaminado por créditos COD que vazaram para tpc_transacoes
	// (referencia 'sz_cod_produtor_*'). Em vez de SUM(c.saldo), derivamos das transações
	// de frete CONFIRMADAS (credito − debito) excluindo COD — mesma lógica de
	// tpc_clientes.go. As linhas COD continuam nas telas próprias de COD.
	// NOTA: estes endpoints /wallet/* não têm consumidor de front hoje (CarteiraExpedicaoHub
	// usa TpcClientes/TpcTransacoes via /tpc-*), mas isolamos por defesa caso sejam religados.
	rows, err := h.Pool.Query(r.Context(),
		`SELECT MIN(c.id)                         AS id,
		        pu.id                             AS user_id,
		        COALESCE(MAX(pu.nome),  '')       AS nome,
		        COALESCE(MAX(pu.email), '')       AS email,
		        COALESCE(SUM(COALESCE((
		            SELECT COALESCE(SUM(CASE WHEN t.tipo='credito' THEN t.valor ELSE 0 END),0)
		                 - COALESCE(SUM(CASE WHEN t.tipo='reserva' THEN t.valor ELSE 0 END),0)
		            FROM tpc_transacoes t
		            WHERE t.user_id = c.user_id AND t.status = 'confirmado'
		              AND (t.referencia IS NULL OR t.referencia NOT LIKE 'sz_cod%')
		              AND COALESCE(t.descricao, '') NOT ILIKE '%cod%'
		        ), 0)), 0)                        AS saldo,
		        COALESCE(SUM(c.saldo_reservado),0) AS saldo_reservado,
		        MIN(c.created_at)::text           AS created_at
		 FROM tpc_carteira c
		 JOIN senderzz_portal_users pu
		   ON pu.id = c.user_id AND pu.role = 'produtor'
		 GROUP BY pu.id
		 ORDER BY saldo DESC LIMIT 200`)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()
	out := []carteira{}
	for rows.Next() {
		var c carteira
		_ = rows.Scan(&c.ID, &c.UserID, &c.Nome, &c.Email, &c.Saldo, &c.SaldoReservado, &c.CreatedAt)
		out = append(out, c)
	}
	httpx.JSON(w, 200, map[string]any{"items": out})
}

func (h *WalletHandler) ListTransacoes(w http.ResponseWriter, r *http.Request) {
	if !h.tableExists(r.Context(), "tpc_transacoes") {
		httpx.JSON(w, 200, map[string]any{"items": []transacao{}})
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	userID, _ := strconv.ParseInt(q.Get("user_id"), 10, 64)

	// ISOLAMENTO COD (FALK): só transações de Expedição (frete) — exclui COD
	// (referencia 'sz_cod_produtor_*' / descricao "Venda COD ..."). Ver wallet.go::ListCarteiras.
	rows, err := h.Pool.Query(r.Context(),
		`SELECT t.id, t.user_id, t.tipo, t.valor, t.saldo_apos, t.descricao, t.referencia, t.order_id, t.status, t.created_at::text
		 FROM tpc_transacoes t
		 WHERE ($1=0 OR t.user_id=$1)
		   AND (t.referencia IS NULL OR t.referencia NOT LIKE 'sz_cod%')
		   AND COALESCE(t.descricao, '') NOT ILIKE '%cod%'
		 ORDER BY t.id DESC LIMIT $2`, userID, limit)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()
	out := []transacao{}
	for rows.Next() {
		var t transacao
		_ = rows.Scan(&t.ID, &t.UserID, &t.Tipo, &t.Valor, &t.SaldoApos, &t.Descricao, &t.Referencia, &t.OrderID, &t.Status, &t.CreatedAt)
		out = append(out, t)
	}
	httpx.JSON(w, 200, map[string]any{"items": out})
}

// ListEstornosPendentes lista cancelamentos de etiqueta cujo estorno ainda
// não foi confirmado — carteira TPC intencionalmente NÃO creditada até ops
// confirmar que o reembolso caiu na conta pool da Melhor Envio.
// AUDIT-2026-07-28: ver EstornarPendente/ConfirmarEstorno no wallet-service.
func (h *WalletHandler) ListEstornosPendentes(w http.ResponseWriter, r *http.Request) {
	if !h.tableExists(r.Context(), "tpc_transacoes") {
		httpx.JSON(w, 200, map[string]any{"items": []transacao{}})
		return
	}
	rows, err := h.Pool.Query(r.Context(),
		`SELECT t.id, t.user_id, t.tipo, t.valor, t.saldo_apos, t.descricao, t.referencia, t.order_id, t.status, t.created_at::text
		 FROM tpc_transacoes t
		 WHERE t.tipo = 'credito' AND t.status = 'pendente' AND t.referencia LIKE 'cancel_label_%'
		 ORDER BY t.id ASC LIMIT 200`)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()
	out := []transacao{}
	for rows.Next() {
		var t transacao
		_ = rows.Scan(&t.ID, &t.UserID, &t.Tipo, &t.Valor, &t.SaldoApos, &t.Descricao, &t.Referencia, &t.OrderID, &t.Status, &t.CreatedAt)
		out = append(out, t)
	}
	httpx.JSON(w, 200, map[string]any{"items": out})
}

// PostConfirmarEstorno credita de fato o saldo de um estorno pendente —
// AÇÃO MANUAL DE OPS, só depois de checar no painel da Melhor Envio que o
// reembolso realmente caiu na conta pool. Nunca automático (ver AUDIT-2026-07-28:
// "nunca creditar sem ter caído na conta melhor envio").
func (h *WalletHandler) PostConfirmarEstorno(w http.ResponseWriter, r *http.Request) {
	if h.WalletClient == nil || !h.WalletClient.Enabled() {
		httpx.Err(w, 503, "wallet_disabled", "WALLET_SERVICE_URL não configurada")
		return
	}
	txID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if txID <= 0 {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	var userID int64
	var referencia string
	err := h.Pool.QueryRow(r.Context(),
		`SELECT user_id, referencia FROM tpc_transacoes
		  WHERE id = $1 AND tipo = 'credito' AND status = 'pendente'`,
		txID,
	).Scan(&userID, &referencia)
	if err != nil {
		httpx.Err(w, 404, "not_found", "estorno pendente não encontrado")
		return
	}
	if err := h.WalletClient.ConfirmarEstorno(r.Context(), userID, referencia); err != nil {
		httpx.Err(w, 502, "wallet_error", err.Error())
		return
	}
	httpx.JSON(w, 200, map[string]any{"ok": true})
}
