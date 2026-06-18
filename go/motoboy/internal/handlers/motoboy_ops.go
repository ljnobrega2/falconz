// Package handlers — operações do app motoboy, wallet, OL extras e endpoints públicos.
//
// Endpoints cobertos:
//   - POST /motoboy/devolver-qr
//   - POST /motoboy/ping
//   - GET  /motoboy/fechamento
//   - POST /motoboy/confirmar-repasse
//   - GET  /motoboy/pendentes-confirmacao
//   - POST /motoboy/comprovante        (multipart/form-data)
//   - GET  /motoboy/comprovantes/{order_id}
//   - POST /motoboy/push-subscribe
//   - GET  /wallet/saldo
//   - GET  /wallet/historico
//   - GET  /wallet/bancario
//   - POST /wallet/bancario
//   - GET  /ol/pedido-historico?pedido_id=N
//   - GET  /link-expedicao?sz=TOKEN
//   - POST /dispensar-cpf
package handlers

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/motoboy-service/internal/auth"
	"github.com/senderzz/motoboy-service/internal/httpx"
)

// MotoboOpsHandler agrupa operações do dia a dia do motoboy, wallet e extras.
type MotoboOpsHandler struct {
	Pool *pgxpool.Pool
}

// ── Operações do motoboy ──────────────────────────────────────────────────────

// DevolverQR — POST /motoboy/devolver-qr
// Body: {pedido_id}
// Reverte status embalado → agendado se o motoboy ainda não iniciou rota.
func (h *MotoboOpsHandler) DevolverQR(w http.ResponseWriter, r *http.Request) {
	mb := auth.MotoboyfromCtx(r.Context())
	if mb == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autorizado")
		return
	}

	var req struct {
		PedidoID int64 `json:"pedido_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	if req.PedidoID == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "pedido_id obrigatório")
		return
	}

	ctx := r.Context()

	var status string
	var motoboyID *int64
	err := h.Pool.QueryRow(ctx,
		`SELECT status, motoboy_id FROM sz_motoboy_pedidos WHERE id = $1`, req.PedidoID,
	).Scan(&status, &motoboyID)
	if err != nil {
		httpx.WriteErr(w, http.StatusNotFound, "pedido não encontrado")
		return
	}

	// Só pode devolver se ainda está embalado (rota não iniciada).
	if status != "embalado" {
		httpx.WriteErr(w, http.StatusConflict, "pedido não está embalado: "+status)
		return
	}
	// Garante que é do motoboy autenticado.
	if motoboyID == nil || *motoboyID != mb.ID {
		httpx.WriteErr(w, http.StatusForbidden, "pedido não pertence a este motoboy")
		return
	}

	_, err = h.Pool.Exec(ctx,
		`UPDATE sz_motoboy_pedidos SET status = 'agendado', motoboy_id = NULL WHERE id = $1`,
		req.PedidoID,
	)
	if err != nil {
		slog.Error("[motoboy] falha ao devolver QR", "pedido_id", req.PedidoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao devolver pedido")
		return
	}

	_, _ = h.Pool.Exec(ctx, `
		INSERT INTO sz_motoboy_audit (pedido_id, acao, de_status, para_status, meta, created_at)
		VALUES ($1, 'qr_devolvido', 'embalado', 'agendado', $2, NOW())`,
		req.PedidoID, mb.ID,
	)

	slog.Info("[motoboy] QR devolvido", "pedido_id", req.PedidoID, "motoboy_id", mb.ID)
	httpx.WriteOK(w, map[string]any{"ok": true, "status": "agendado"})
}

// Ping — POST /motoboy/ping
// Body: {lat, lng}
// Upsert sz_motoboy_localizacoes com lat/lng do motoboy.
//
// TODO: migration — criar tabela se não existir:
//
//	CREATE TABLE IF NOT EXISTS sz_motoboy_localizacoes (
//	  motoboy_id BIGINT PRIMARY KEY REFERENCES sz_motoboys(id),
//	  lat        DOUBLE PRECISION NOT NULL,
//	  lng        DOUBLE PRECISION NOT NULL,
//	  updated_at TIMESTAMP NOT NULL DEFAULT NOW()
//	);
func (h *MotoboOpsHandler) Ping(w http.ResponseWriter, r *http.Request) {
	mb := auth.MotoboyfromCtx(r.Context())
	if mb == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autorizado")
		return
	}

	var req struct {
		Lat float64 `json:"lat"`
		Lng float64 `json:"lng"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	ctx := r.Context()
	_, err := h.Pool.Exec(ctx, `
		INSERT INTO sz_motoboy_localizacoes (motoboy_id, lat, lng, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (motoboy_id) DO UPDATE SET lat = $2, lng = $3, updated_at = NOW()`,
		mb.ID, req.Lat, req.Lng,
	)
	if err != nil {
		slog.Error("[motoboy] falha ao salvar localização", "motoboy_id", mb.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao salvar localização")
		return
	}

	httpx.WriteOK(w, map[string]any{"ok": true})
}

// Fechamento — GET /motoboy/fechamento
// Retorna o fechamento atual do motoboy (entregues, frustrados, total_rs, repasse_rs).
// repasse_rs considera sz_motoboy_cc_fee_pct da tabela wp_options.
//
// TODO: migration — verificar colunas em sz_motoboy_fechamento:
//
//	ALTER TABLE sz_motoboy_fechamento
//	  ADD COLUMN IF NOT EXISTS confirmado_motoboy    BOOLEAN NOT NULL DEFAULT FALSE,
//	  ADD COLUMN IF NOT EXISTS confirmado_motoboy_at TIMESTAMP,
//	  ADD COLUMN IF NOT EXISTS confirmado_alan        BOOLEAN NOT NULL DEFAULT FALSE,
//	  ADD COLUMN IF NOT EXISTS confirmado_alan_at     TIMESTAMP;
func (h *MotoboOpsHandler) Fechamento(w http.ResponseWriter, r *http.Request) {
	mb := auth.MotoboyfromCtx(r.Context())
	if mb == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autorizado")
		return
	}

	ctx := r.Context()

	// Busca taxa de cartão do WP Options.
	var ccFeeStr string
	_ = h.Pool.QueryRow(ctx,
		`SELECT option_value FROM wp_options WHERE option_name = 'sz_motoboy_cc_fee_pct' LIMIT 1`,
	).Scan(&ccFeeStr)
	ccFeePct, _ := strconv.ParseFloat(ccFeeStr, 64)

	type fechamentoRow struct {
		ID                int64    `json:"id"`
		DataFechamento    string   `json:"data_fechamento"`
		Entregues         int      `json:"entregues"`
		Frustrados        int      `json:"frustrados"`
		TotalRS           float64  `json:"total_rs"`
		RepasseRS         float64  `json:"repasse_rs"`
		ConfirmadoMotoboy bool     `json:"confirmado_motoboy"`
		ConfirmadoAlan    bool     `json:"confirmado_alan"`
	}

	rows, err := h.Pool.Query(ctx, `
		SELECT
			id,
			TO_CHAR(created_at, 'YYYY-MM-DD') AS data_fechamento,
			COALESCE(entregues, 0),
			COALESCE(frustrados, 0),
			COALESCE(total_rs, 0),
			COALESCE(repasse_rs, 0),
			COALESCE(confirmado_motoboy, false),
			COALESCE(confirmado_alan, false)
		FROM sz_motoboy_fechamento
		WHERE motoboy_id = $1
		ORDER BY created_at DESC
		LIMIT 30`,
		mb.ID,
	)
	if err != nil {
		slog.Error("[motoboy] falha ao buscar fechamento", "motoboy_id", mb.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao buscar fechamento")
		return
	}
	defer rows.Close()

	var fechamentos []fechamentoRow
	for rows.Next() {
		var f fechamentoRow
		if err := rows.Scan(
			&f.ID, &f.DataFechamento,
			&f.Entregues, &f.Frustrados,
			&f.TotalRS, &f.RepasseRS,
			&f.ConfirmadoMotoboy, &f.ConfirmadoAlan,
		); err != nil {
			slog.Error("[motoboy] erro ao scanear fechamento", "err", err)
			continue
		}
		// Aplica taxa de cartão ao repasse se configurado.
		if ccFeePct > 0 && !f.ConfirmadoMotoboy {
			f.RepasseRS = f.TotalRS * (1 - ccFeePct/100)
		}
		fechamentos = append(fechamentos, f)
	}

	httpx.WriteOK(w, map[string]any{
		"motoboy_id":  mb.ID,
		"cc_fee_pct":  ccFeePct,
		"fechamentos": fechamentos,
	})
}

// ConfirmarRepasse — POST /motoboy/confirmar-repasse
// Body: {fechamento_id}
// Marca sz_motoboy_fechamento.confirmado_motoboy = true.
func (h *MotoboOpsHandler) ConfirmarRepasse(w http.ResponseWriter, r *http.Request) {
	mb := auth.MotoboyfromCtx(r.Context())
	if mb == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autorizado")
		return
	}

	var req struct {
		FechamentoID int64 `json:"fechamento_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	if req.FechamentoID == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "fechamento_id obrigatório")
		return
	}

	ctx := r.Context()

	tag, err := h.Pool.Exec(ctx, `
		UPDATE sz_motoboy_fechamento
		SET confirmado_motoboy = true, confirmado_motoboy_at = NOW()
		WHERE id = $1 AND motoboy_id = $2 AND confirmado_motoboy = false`,
		req.FechamentoID, mb.ID,
	)
	if err != nil {
		slog.Error("[motoboy] falha ao confirmar repasse", "fechamento_id", req.FechamentoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao confirmar repasse")
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.WriteErr(w, http.StatusNotFound, "fechamento não encontrado ou já confirmado")
		return
	}

	slog.Info("[motoboy] repasse confirmado", "fechamento_id", req.FechamentoID, "motoboy_id", mb.ID)
	httpx.WriteOK(w, map[string]any{"ok": true, "fechamento_id": req.FechamentoID})
}

// PendentesConfirmacao — GET /motoboy/pendentes-confirmacao
// Retorna pedidos entregues sem confirmação de repasse.
func (h *MotoboOpsHandler) PendentesConfirmacao(w http.ResponseWriter, r *http.Request) {
	mb := auth.MotoboyfromCtx(r.Context())
	if mb == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autorizado")
		return
	}

	ctx := r.Context()

	rows, err := h.Pool.Query(ctx, `
		SELECT
			p.id,
			p.wc_order_id,
			p.dest_nome,
			p.dest_produto,
			p.valor_pedido,
			TO_CHAR(p.ts_entregue, 'YYYY-MM-DD"T"HH24:MI:SS') AS ts_entregue
		FROM sz_motoboy_pedidos p
		WHERE p.motoboy_id = $1
		  AND p.status = 'entregue'
		  AND NOT EXISTS (
			SELECT 1 FROM sz_motoboy_fechamento f
			WHERE f.motoboy_id = p.motoboy_id
			  AND f.confirmado_motoboy = true
			  AND DATE(f.created_at) = DATE(p.ts_entregue)
		  )
		ORDER BY p.ts_entregue DESC`,
		mb.ID,
	)
	if err != nil {
		slog.Error("[motoboy] falha ao buscar pendentes de confirmação", "motoboy_id", mb.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao buscar pedidos")
		return
	}
	defer rows.Close()

	type pedidoPendente struct {
		ID          int64   `json:"id"`
		WCOrderID   int64   `json:"wc_order_id"`
		DestNome    *string `json:"dest_nome"`
		DestProduto *string `json:"dest_produto"`
		ValorPedido float64 `json:"valor_pedido"`
		TsEntregue  *string `json:"ts_entregue"`
	}

	var pedidos []pedidoPendente
	for rows.Next() {
		var p pedidoPendente
		if err := rows.Scan(
			&p.ID, &p.WCOrderID, &p.DestNome, &p.DestProduto, &p.ValorPedido, &p.TsEntregue,
		); err != nil {
			slog.Error("[motoboy] erro ao scanear pedido pendente", "err", err)
			continue
		}
		pedidos = append(pedidos, p)
	}

	httpx.WriteOK(w, map[string]any{"ok": true, "pedidos": pedidos, "total": len(pedidos)})
}

// Comprovante — POST /motoboy/comprovante
// multipart/form-data: {pedido_id, foto}
// Salva metadados + bytes base64 em sz_motoboy_comprovantes.
//
// TODO: migration — verificar/criar coluna foto_base64:
//
//	ALTER TABLE sz_motoboy_comprovantes
//	  ADD COLUMN IF NOT EXISTS foto_base64 TEXT;
func (h *MotoboOpsHandler) Comprovante(w http.ResponseWriter, r *http.Request) {
	mb := auth.MotoboyfromCtx(r.Context())
	if mb == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autorizado")
		return
	}

	// Limite de 10 MB para o upload.
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "erro ao processar form: "+err.Error())
		return
	}

	pedidoIDStr := r.FormValue("pedido_id")
	pedidoID, err := strconv.ParseInt(pedidoIDStr, 10, 64)
	if err != nil || pedidoID == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "pedido_id inválido")
		return
	}

	file, header, err := r.FormFile("foto")
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "campo 'foto' obrigatório")
		return
	}
	defer file.Close()

	fotoBytes, err := io.ReadAll(io.LimitReader(file, 10<<20))
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao ler foto")
		return
	}
	fotoB64 := base64.StdEncoding.EncodeToString(fotoBytes)

	ctx := r.Context()

	// Verifica que o pedido pertence ao motoboy.
	var pedidoMotoboyID *int64
	if err := h.Pool.QueryRow(ctx,
		`SELECT motoboy_id FROM sz_motoboy_pedidos WHERE id = $1`, pedidoID,
	).Scan(&pedidoMotoboyID); err != nil {
		httpx.WriteErr(w, http.StatusNotFound, "pedido não encontrado")
		return
	}
	if pedidoMotoboyID == nil || *pedidoMotoboyID != mb.ID {
		httpx.WriteErr(w, http.StatusForbidden, "pedido não pertence a este motoboy")
		return
	}

	var comprovanteID int64
	err = h.Pool.QueryRow(ctx, `
		INSERT INTO sz_motoboy_comprovantes
			(pedido_id, motoboy_id, foto_nome, foto_base64, created_at)
		VALUES ($1, $2, $3, $4, NOW())
		RETURNING id`,
		pedidoID, mb.ID, header.Filename, fotoB64,
	).Scan(&comprovanteID)
	if err != nil {
		slog.Error("[motoboy] falha ao salvar comprovante", "pedido_id", pedidoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao salvar comprovante")
		return
	}

	slog.Info("[motoboy] comprovante salvo", "comprovante_id", comprovanteID, "pedido_id", pedidoID, "bytes", len(fotoBytes))
	httpx.WriteOK(w, map[string]any{"ok": true, "comprovante_id": comprovanteID})
}

// Comprovantes — GET /motoboy/comprovantes/{order_id}
// Retorna lista de comprovantes de um pedido (por wc_order_id).
func (h *MotoboOpsHandler) Comprovantes(w http.ResponseWriter, r *http.Request) {
	mb := auth.MotoboyfromCtx(r.Context())
	if mb == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autorizado")
		return
	}

	orderIDStr := chi.URLParam(r, "order_id")
	orderID, err := strconv.ParseInt(orderIDStr, 10, 64)
	if err != nil || orderID == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "order_id inválido")
		return
	}

	ctx := r.Context()

	rows, err := h.Pool.Query(ctx, `
		SELECT
			c.id,
			c.pedido_id,
			c.foto_nome,
			TO_CHAR(c.created_at, 'YYYY-MM-DD"T"HH24:MI:SS') AS created_at
		FROM sz_motoboy_comprovantes c
		JOIN sz_motoboy_pedidos p ON p.id = c.pedido_id
		WHERE p.wc_order_id = $1 AND c.motoboy_id = $2
		ORDER BY c.created_at ASC`,
		orderID, mb.ID,
	)
	if err != nil {
		slog.Error("[motoboy] falha ao buscar comprovantes", "order_id", orderID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao buscar comprovantes")
		return
	}
	defer rows.Close()

	type comprovante struct {
		ID        int64  `json:"id"`
		PedidoID  int64  `json:"pedido_id"`
		FotoNome  string `json:"foto_nome"`
		CreatedAt string `json:"created_at"`
	}

	var comprovantes []comprovante
	for rows.Next() {
		var c comprovante
		if err := rows.Scan(&c.ID, &c.PedidoID, &c.FotoNome, &c.CreatedAt); err != nil {
			slog.Error("[motoboy] erro ao scanear comprovante", "err", err)
			continue
		}
		comprovantes = append(comprovantes, c)
	}

	httpx.WriteOK(w, map[string]any{"ok": true, "comprovantes": comprovantes, "total": len(comprovantes)})
}

// PushSubscribe — POST /motoboy/push-subscribe
// Body: {token, plataforma}
// Upsert sz_motoboy_push_tokens para o motoboy autenticado.
//
// TODO: migration — criar tabela se não existir:
//
//	CREATE TABLE IF NOT EXISTS sz_motoboy_push_tokens (
//	  id         BIGSERIAL PRIMARY KEY,
//	  motoboy_id BIGINT NOT NULL REFERENCES sz_motoboys(id),
//	  token      TEXT NOT NULL,
//	  plataforma TEXT NOT NULL DEFAULT 'fcm',
//	  created_at TIMESTAMP NOT NULL DEFAULT NOW(),
//	  updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
//	  UNIQUE (token)
//	);
func (h *MotoboOpsHandler) PushSubscribe(w http.ResponseWriter, r *http.Request) {
	mb := auth.MotoboyfromCtx(r.Context())
	if mb == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autorizado")
		return
	}

	var req struct {
		Token      string `json:"token"`
		Plataforma string `json:"plataforma"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	if req.Token == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "token obrigatório")
		return
	}
	if req.Plataforma == "" {
		req.Plataforma = "fcm"
	}

	ctx := r.Context()
	_, err := h.Pool.Exec(ctx, `
		INSERT INTO sz_motoboy_push_tokens (motoboy_id, token, plataforma, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (token) DO UPDATE SET motoboy_id = $1, plataforma = $3, updated_at = NOW()`,
		mb.ID, req.Token, req.Plataforma,
	)
	if err != nil {
		slog.Error("[motoboy] falha ao salvar push token", "motoboy_id", mb.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao salvar token")
		return
	}

	slog.Info("[motoboy] push token registrado", "motoboy_id", mb.ID, "plataforma", req.Plataforma)
	httpx.WriteOK(w, map[string]any{"msg": "Token registrado."})
}

// ── Wallet ────────────────────────────────────────────────────────────────────

// WalletSaldo — GET /wallet/saldo
// Saldo do motoboy: soma dos repasses não confirmados (fechamentos abertos).
func (h *MotoboOpsHandler) WalletSaldo(w http.ResponseWriter, r *http.Request) {
	mb := auth.MotoboyfromCtx(r.Context())
	if mb == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autorizado")
		return
	}

	ctx := r.Context()

	var saldoPendente, saldoTotal float64
	err := h.Pool.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(repasse_rs) FILTER (WHERE confirmado_alan = false), 0) AS saldo_pendente,
			COALESCE(SUM(repasse_rs), 0)                                        AS saldo_total
		FROM sz_motoboy_fechamento
		WHERE motoboy_id = $1`,
		mb.ID,
	).Scan(&saldoPendente, &saldoTotal)
	if err != nil {
		slog.Error("[wallet] falha ao buscar saldo", "motoboy_id", mb.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao buscar saldo")
		return
	}

	httpx.WriteOK(w, map[string]any{
		"motoboy_id":      mb.ID,
		"saldo_pendente":  saldoPendente,
		"saldo_total":     saldoTotal,
	})
}

// WalletHistorico — GET /wallet/historico
// Lista fechamentos do motoboy (histórico de repassses).
func (h *MotoboOpsHandler) WalletHistorico(w http.ResponseWriter, r *http.Request) {
	mb := auth.MotoboyfromCtx(r.Context())
	if mb == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autorizado")
		return
	}

	ctx := r.Context()

	rows, err := h.Pool.Query(ctx, `
		SELECT
			id,
			COALESCE(entregues, 0),
			COALESCE(frustrados, 0),
			COALESCE(total_rs, 0),
			COALESCE(repasse_rs, 0),
			COALESCE(confirmado_motoboy, false),
			COALESCE(confirmado_alan, false),
			TO_CHAR(created_at, 'YYYY-MM-DD') AS data_fechamento
		FROM sz_motoboy_fechamento
		WHERE motoboy_id = $1
		ORDER BY created_at DESC
		LIMIT 90`,
		mb.ID,
	)
	if err != nil {
		slog.Error("[wallet] falha ao buscar histórico", "motoboy_id", mb.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao buscar histórico")
		return
	}
	defer rows.Close()

	type fechamento struct {
		ID                int64   `json:"id"`
		Entregues         int     `json:"entregues"`
		Frustrados        int     `json:"frustrados"`
		TotalRS           float64 `json:"total_rs"`
		RepasseRS         float64 `json:"repasse_rs"`
		ConfirmadoMotoboy bool    `json:"confirmado_motoboy"`
		ConfirmadoAlan    bool    `json:"confirmado_alan"`
		DataFechamento    string  `json:"data_fechamento"`
	}

	var fechamentos []fechamento
	for rows.Next() {
		var f fechamento
		if err := rows.Scan(
			&f.ID, &f.Entregues, &f.Frustrados,
			&f.TotalRS, &f.RepasseRS,
			&f.ConfirmadoMotoboy, &f.ConfirmadoAlan,
			&f.DataFechamento,
		); err != nil {
			slog.Error("[wallet] erro ao scanear fechamento", "err", err)
			continue
		}
		fechamentos = append(fechamentos, f)
	}

	httpx.WriteOK(w, map[string]any{
		"motoboy_id":  mb.ID,
		"fechamentos": fechamentos,
		"total":       len(fechamentos),
	})
}

// WalletBancarioGet — GET /wallet/bancario
// Retorna dados bancários do motoboy (sz_motoboys.dados_bancarios JSON).
func (h *MotoboOpsHandler) WalletBancarioGet(w http.ResponseWriter, r *http.Request) {
	mb := auth.MotoboyfromCtx(r.Context())
	if mb == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autorizado")
		return
	}

	ctx := r.Context()

	var dadosBancariosJSON []byte
	err := h.Pool.QueryRow(ctx,
		`SELECT COALESCE(dados_bancarios, '{}') FROM sz_motoboys WHERE id = $1`, mb.ID,
	).Scan(&dadosBancariosJSON)
	if err != nil {
		slog.Error("[wallet] falha ao buscar dados bancários", "motoboy_id", mb.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao buscar dados")
		return
	}

	var dados map[string]any
	if err := json.Unmarshal(dadosBancariosJSON, &dados); err != nil {
		dados = map[string]any{}
	}

	httpx.WriteOK(w, map[string]any{"ok": true, "dados_bancarios": dados})
}

// WalletBancarioPost — POST /wallet/bancario
// Body: {banco, agencia, conta, tipo}
// Atualiza sz_motoboys.dados_bancarios.
func (h *MotoboOpsHandler) WalletBancarioPost(w http.ResponseWriter, r *http.Request) {
	mb := auth.MotoboyfromCtx(r.Context())
	if mb == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autorizado")
		return
	}

	var req struct {
		Banco   string `json:"banco"`
		Agencia string `json:"agencia"`
		Conta   string `json:"conta"`
		Tipo    string `json:"tipo"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	if req.Banco == "" || req.Conta == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "banco e conta obrigatórios")
		return
	}

	dadosJSON, err := json.Marshal(req)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao serializar dados")
		return
	}

	ctx := r.Context()
	_, err = h.Pool.Exec(ctx,
		`UPDATE sz_motoboys SET dados_bancarios = $1 WHERE id = $2`,
		dadosJSON, mb.ID,
	)
	if err != nil {
		slog.Error("[wallet] falha ao atualizar dados bancários", "motoboy_id", mb.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao salvar dados")
		return
	}

	slog.Info("[wallet] dados bancários atualizados", "motoboy_id", mb.ID)
	httpx.WriteOK(w, map[string]any{"msg": "Dados bancários atualizados."})
}

// ── OL extra ──────────────────────────────────────────────────────────────────

// PedidoHistorico — GET /ol/pedido-historico?pedido_id=N
// Retorna log de auditoria completo de um pedido.
func (h *MotoboOpsHandler) PedidoHistorico(w http.ResponseWriter, r *http.Request) {
	pedidoIDStr := r.URL.Query().Get("pedido_id")
	pedidoID, err := strconv.ParseInt(pedidoIDStr, 10, 64)
	if err != nil || pedidoID == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "pedido_id obrigatório")
		return
	}

	ctx := r.Context()

	// Verifica existência do pedido.
	var wcOrderID int64
	var status string
	if err := h.Pool.QueryRow(ctx,
		`SELECT wc_order_id, status FROM sz_motoboy_pedidos WHERE id = $1`, pedidoID,
	).Scan(&wcOrderID, &status); err != nil {
		httpx.WriteErr(w, http.StatusNotFound, "pedido não encontrado")
		return
	}

	rows, err := h.Pool.Query(ctx, `
		SELECT
			id,
			acao,
			COALESCE(de_status, '')   AS de_status,
			COALESCE(para_status, '') AS para_status,
			COALESCE(meta, '')        AS meta,
			TO_CHAR(created_at, 'YYYY-MM-DD"T"HH24:MI:SS') AS created_at
		FROM sz_motoboy_audit
		WHERE pedido_id = $1
		ORDER BY created_at ASC`,
		pedidoID,
	)
	if err != nil {
		slog.Error("[ol] falha ao buscar histórico do pedido", "pedido_id", pedidoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao buscar histórico")
		return
	}
	defer rows.Close()

	type auditLog struct {
		ID         int64  `json:"id"`
		Acao       string `json:"acao"`
		DeStatus   string `json:"de_status"`
		ParaStatus string `json:"para_status"`
		Meta       string `json:"meta"`
		CreatedAt  string `json:"created_at"`
	}

	var logs []auditLog
	for rows.Next() {
		var l auditLog
		if err := rows.Scan(&l.ID, &l.Acao, &l.DeStatus, &l.ParaStatus, &l.Meta, &l.CreatedAt); err != nil {
			slog.Error("[ol] erro ao scanear audit log", "err", err)
			continue
		}
		logs = append(logs, l)
	}

	httpx.WriteOK(w, map[string]any{
		"pedido_id":  pedidoID,
		"wc_order_id": wcOrderID,
		"status":     status,
		"historico":  logs,
		"total":      len(logs),
	})
}

// ── Endpoints públicos ────────────────────────────────────────────────────────

// LinkExpedicao — GET /link-expedicao?sz=TOKEN
// Valida sz contra sz_motoboys.token_expedicao e redireciona para a tela de embalar.
//
// TODO: definir URL base da tela de embalar via env var EXPEDICAO_URL (default: /motoboy-app/).
func (h *MotoboOpsHandler) LinkExpedicao(w http.ResponseWriter, r *http.Request) {
	sz := r.URL.Query().Get("sz")
	if sz == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "parâmetro sz obrigatório")
		return
	}

	ctx := r.Context()

	var motoboyID int64
	err := h.Pool.QueryRow(ctx,
		`SELECT id FROM sz_motoboys WHERE token_expedicao = $1 AND ativo = 1 LIMIT 1`, sz,
	).Scan(&motoboyID)
	if err != nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "link inválido ou expirado")
		return
	}

	// TODO: configurar via env var EXPEDICAO_URL se a URL base mudar.
	expedicaoURL := os.Getenv("EXPEDICAO_URL")
	if expedicaoURL == "" {
		expedicaoURL = "/motoboy-app/"
	}

	target := expedicaoURL + "?sz=" + sz
	slog.Info("[link-expedicao] redirecionando", "motoboy_id", motoboyID, "target", target)
	http.Redirect(w, r, target, http.StatusFound)
}

// DispensarCPF — POST /dispensar-cpf
// Auth: portal session (via authPortal middleware aplicado no caller).
// Body: {pedido_id, motivo}
// Marca CPF como dispensado nos metadados do pedido.
//
// TODO: migration — verificar/criar coluna cpf_dispensado em sz_motoboy_pedidos:
//
//	ALTER TABLE sz_motoboy_pedidos
//	  ADD COLUMN IF NOT EXISTS cpf_dispensado        BOOLEAN NOT NULL DEFAULT FALSE,
//	  ADD COLUMN IF NOT EXISTS cpf_dispensado_motivo TEXT,
//	  ADD COLUMN IF NOT EXISTS cpf_dispensado_at     TIMESTAMP;
func (h *MotoboOpsHandler) DispensarCPF(w http.ResponseWriter, r *http.Request) {
	// Auth já validada pelo middleware authPortal — user no contexto.
	portalUser := auth.PortalUserFromCtx(r.Context())
	if portalUser == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "sessão portal inválida")
		return
	}

	var req struct {
		PedidoID int64  `json:"pedido_id"`
		Motivo   string `json:"motivo"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	if req.PedidoID == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "pedido_id obrigatório")
		return
	}
	if req.Motivo == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "motivo obrigatório")
		return
	}

	ctx := r.Context()

	tag, err := h.Pool.Exec(ctx, `
		UPDATE sz_motoboy_pedidos
		SET cpf_dispensado = true,
		    cpf_dispensado_motivo = $2,
		    cpf_dispensado_at = NOW()
		WHERE id = $1 AND cpf_dispensado = false`,
		req.PedidoID, req.Motivo,
	)
	if err != nil {
		slog.Error("[dispensar-cpf] falha ao marcar CPF dispensado", "pedido_id", req.PedidoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao dispensar CPF")
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.WriteErr(w, http.StatusNotFound, "pedido não encontrado ou CPF já dispensado")
		return
	}

	_, _ = h.Pool.Exec(ctx, `
		INSERT INTO sz_motoboy_audit (pedido_id, acao, meta, created_at)
		VALUES ($1, 'cpf_dispensado', $2, NOW())`,
		req.PedidoID, req.Motivo,
	)

	slog.Info("[dispensar-cpf] CPF dispensado", "pedido_id", req.PedidoID, "portal_user", portalUser.ID)
	httpx.WriteOK(w, map[string]any{"ok": true, "pedido_id": req.PedidoID})
}
