// Package handlers — endpoints Alan / Expedição.
// Auth: X-Alan-Token (token estático via env ALAN_TOKEN, validado por auth.AuthAlan).
//
// "Alan" é o operador de expedição — responsável por embalar pedidos,
// acompanhar localizações dos motoboys e confirmar fechamentos.
package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/motoboy-service/internal/httpx"
)

// AlanHandler agrupa dependências dos endpoints de expedição.
type AlanHandler struct {
	Pool *pgxpool.Pool
}

// Localizacao — GET /alan/localizacao
// Retorna todos os motoboys ativos com a última localização registrada.
//
// TODO: migration — criar tabela se não existir:
//
//	CREATE TABLE IF NOT EXISTS sz_motoboy_localizacoes (
//	  motoboy_id BIGINT PRIMARY KEY REFERENCES sz_motoboys(id),
//	  lat        DOUBLE PRECISION NOT NULL,
//	  lng        DOUBLE PRECISION NOT NULL,
//	  updated_at TIMESTAMP NOT NULL DEFAULT NOW()
//	);
func (h *AlanHandler) Localizacao(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	rows, err := h.Pool.Query(ctx, `
		SELECT
			m.id,
			m.nome,
			m.telefone,
			COALESCE(l.lat, 0)                       AS lat,
			COALESCE(l.lng, 0)                       AS lng,
			TO_CHAR(l.updated_at, 'YYYY-MM-DD"T"HH24:MI:SS') AS ultima_atualizacao
		FROM sz_motoboys m
		LEFT JOIN sz_motoboy_localizacoes l ON l.motoboy_id = m.id
		WHERE m.ativo = 1
		ORDER BY m.nome`)
	if err != nil {
		slog.Error("[alan] falha ao buscar localizações", "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao buscar localizações")
		return
	}
	defer rows.Close()

	type localizacao struct {
		MotoboyID         int64   `json:"motoboy_id"`
		Nome              string  `json:"nome"`
		Telefone          string  `json:"telefone"`
		Lat               float64 `json:"lat"`
		Lng               float64 `json:"lng"`
		UltimaAtualizacao *string `json:"ultima_atualizacao"`
	}

	var result []localizacao
	for rows.Next() {
		var l localizacao
		if err := rows.Scan(&l.MotoboyID, &l.Nome, &l.Telefone, &l.Lat, &l.Lng, &l.UltimaAtualizacao); err != nil {
			slog.Error("[alan] erro ao scanear localização", "err", err)
			continue
		}
		result = append(result, l)
	}

	httpx.WriteOK(w, map[string]any{"ok": true, "motoboys": result})
}

// Historico — GET /alan/historico/{motoboy_id}
// Retorna histórico de pedidos de um motoboy específico.
func (h *AlanHandler) Historico(w http.ResponseWriter, r *http.Request) {
	motoboyIDStr := chi.URLParam(r, "motoboy_id")
	motoboyID, err := strconv.ParseInt(motoboyIDStr, 10, 64)
	if err != nil || motoboyID == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "motoboy_id inválido")
		return
	}

	ctx := r.Context()

	// Verifica existência do motoboy.
	var nome string
	if err := h.Pool.QueryRow(ctx,
		`SELECT nome FROM sz_motoboys WHERE id = $1`, motoboyID,
	).Scan(&nome); err != nil {
		httpx.WriteErr(w, http.StatusNotFound, "motoboy não encontrado")
		return
	}

	rows, err := h.Pool.Query(ctx, `
		SELECT
			id,
			wc_order_id,
			status,
			dest_nome,
			dest_produto,
			valor_pedido,
			TO_CHAR(ts_aprovado, 'YYYY-MM-DD"T"HH24:MI:SS')  AS ts_aprovado,
			TO_CHAR(ts_entregue, 'YYYY-MM-DD"T"HH24:MI:SS')  AS ts_entregue,
			TO_CHAR(ts_frustrado, 'YYYY-MM-DD"T"HH24:MI:SS') AS ts_frustrado
		FROM sz_motoboy_pedidos
		WHERE motoboy_id = $1
		ORDER BY ts_aprovado DESC
		LIMIT 200`,
		motoboyID,
	)
	if err != nil {
		slog.Error("[alan] falha ao buscar histórico", "motoboy_id", motoboyID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao buscar histórico")
		return
	}
	defer rows.Close()

	type pedidoHist struct {
		ID          int64   `json:"id"`
		WCOrderID   int64   `json:"wc_order_id"`
		Status      string  `json:"status"`
		DestNome    *string `json:"dest_nome"`
		DestProduto *string `json:"dest_produto"`
		ValorPedido float64 `json:"valor_pedido"`
		TsAprovado  *string `json:"ts_aprovado"`
		TsEntregue  *string `json:"ts_entregue"`
		TsFrustrado *string `json:"ts_frustrado"`
	}

	var pedidos []pedidoHist
	for rows.Next() {
		var p pedidoHist
		if err := rows.Scan(
			&p.ID, &p.WCOrderID, &p.Status,
			&p.DestNome, &p.DestProduto, &p.ValorPedido,
			&p.TsAprovado, &p.TsEntregue, &p.TsFrustrado,
		); err != nil {
			slog.Error("[alan] erro ao scanear histórico", "err", err)
			continue
		}
		pedidos = append(pedidos, p)
	}

	httpx.WriteOK(w, map[string]any{
		"motoboy_id":   motoboyID,
		"motoboy_nome": nome,
		"pedidos":      pedidos,
		"total":        len(pedidos),
	})
}

// Etiquetas — GET /alan/etiquetas
// Retorna pedidos embalados (prontos para handoff) com QR codes completos.
// QR code = "SZ-{wc_order_id}-{pedido_id}-{hmac14}" — mesmo algoritmo de
// sz_mbc_package_code() no PHP (HMAC-SHA256 com WP_SALT_AUTH, primeiros 14 chars).
func (h *AlanHandler) Etiquetas(w http.ResponseWriter, r *http.Request) {
	// Fail-closed: sem WP_SALT_AUTH não é possível gerar QR válido.
	salt := os.Getenv("WP_SALT_AUTH")
	if salt == "" {
		slog.Error("[alan] WP_SALT_AUTH não configurado — etiquetas indisponíveis")
		httpx.WriteErr(w, http.StatusServiceUnavailable, "serviço não configurado")
		return
	}

	ctx := r.Context()

	rows, err := h.Pool.Query(ctx, `
		SELECT
			p.id,
			p.wc_order_id,
			p.dest_nome,
			p.dest_cep,
			p.dest_endereco,
			p.dest_numero,
			p.dest_complemento,
			p.dest_bairro,
			p.dest_cidade,
			p.dest_uf,
			p.dest_produto,
			p.valor_pedido,
			COALESCE(m.nome, '') AS motoboy_nome
		FROM sz_motoboy_pedidos p
		LEFT JOIN sz_motoboys m ON m.id = p.motoboy_id
		WHERE p.status = 'embalado'
		ORDER BY p.ts_aprovado ASC`)
	if err != nil {
		slog.Error("[alan] falha ao buscar etiquetas", "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao buscar etiquetas")
		return
	}
	defer rows.Close()

	type etiqueta struct {
		ID          int64   `json:"id"`
		WCOrderID   int64   `json:"wc_order_id"`
		DestNome    *string `json:"dest_nome"`
		DestCEP     string  `json:"dest_cep"`
		DestEndereco *string `json:"dest_endereco"`
		DestNumero  *string `json:"dest_numero"`
		DestCompl   *string `json:"dest_complemento"`
		DestBairro  *string `json:"dest_bairro"`
		DestCidade  *string `json:"dest_cidade"`
		DestUF      *string `json:"dest_uf"`
		DestProduto *string `json:"dest_produto"`
		ValorPedido float64 `json:"valor_pedido"`
		MotoboyNome string  `json:"motoboy_nome"`
		// QRCode completo — pronto para gerar imagem via api.qrserver.com
		// Mesmo formato de sz_mbc_package_code() no PHP.
		QRCode string `json:"qr_code"`
	}

	var result []etiqueta
	for rows.Next() {
		var e etiqueta
		if err := rows.Scan(
			&e.ID, &e.WCOrderID,
			&e.DestNome, &e.DestCEP, &e.DestEndereco, &e.DestNumero,
			&e.DestCompl, &e.DestBairro, &e.DestCidade, &e.DestUF,
			&e.DestProduto, &e.ValorPedido, &e.MotoboyNome,
		); err != nil {
			slog.Error("[alan] erro ao scanear etiqueta", "err", err)
			continue
		}
		// Mesmo algoritmo de sz_mbc_package_code() em PHP:
		// hmac_sha256(WP_SALT_AUTH, "{wc_order_id}-{pedido_id}") → primeiros 14 chars hex.
		mac := hmac.New(sha256.New, []byte(salt))
		mac.Write([]byte(fmt.Sprintf("%d-%d", e.WCOrderID, e.ID)))
		hmac14 := hex.EncodeToString(mac.Sum(nil))[:14]
		e.QRCode = fmt.Sprintf("SZ-%d-%d-%s", e.WCOrderID, e.ID, hmac14)
		result = append(result, e)
	}

	httpx.WriteOK(w, map[string]any{"ok": true, "etiquetas": result, "total": len(result)})
}

// PushSubscribe — POST /alan/push-subscribe
// Body: {token, plataforma}
// Upsert token de push para o expedidor Alan.
//
// TODO: migration — criar tabela se não existir:
//
//	CREATE TABLE IF NOT EXISTS sz_alan_push_tokens (
//	  id         BIGSERIAL PRIMARY KEY,
//	  token      TEXT NOT NULL,
//	  plataforma TEXT NOT NULL DEFAULT 'fcm',
//	  created_at TIMESTAMP NOT NULL DEFAULT NOW(),
//	  updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
//	  UNIQUE (token)
//	);
func (h *AlanHandler) PushSubscribe(w http.ResponseWriter, r *http.Request) {
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
		INSERT INTO sz_alan_push_tokens (token, plataforma, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (token) DO UPDATE SET plataforma = $2, updated_at = NOW()`,
		req.Token, req.Plataforma,
	)
	if err != nil {
		slog.Error("[alan] falha ao salvar push token", "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao salvar token")
		return
	}

	slog.Info("[alan] push token registrado", "plataforma", req.Plataforma)
	httpx.WriteOK(w, map[string]any{"msg": "Token registrado."})
}

// Pedidos — GET /alan/pedidos
// Retorna todos os pedidos de hoje (filtro opcional: ?status=).
func (h *AlanHandler) Pedidos(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	statusFiltro := r.URL.Query().Get("status")

	query := `
		SELECT
			p.id,
			p.wc_order_id,
			p.status,
			p.dest_nome,
			p.dest_produto,
			p.valor_pedido,
			COALESCE(m.nome, '') AS motoboy_nome,
			TO_CHAR(p.ts_aprovado, 'YYYY-MM-DD"T"HH24:MI:SS') AS ts_aprovado
		FROM sz_motoboy_pedidos p
		LEFT JOIN sz_motoboys m ON m.id = p.motoboy_id
		WHERE DATE(p.ts_aprovado) = CURRENT_DATE`

	args := []any{}
	if statusFiltro != "" {
		if !statusWhitelist[statusFiltro] {
			httpx.WriteErr(w, http.StatusBadRequest, "status inválido")
			return
		}
		query += " AND p.status = $1"
		args = append(args, statusFiltro)
	}
	query += " ORDER BY p.ts_aprovado ASC"

	rows, err := h.Pool.Query(ctx, query, args...)
	if err != nil {
		slog.Error("[alan] falha ao buscar pedidos", "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao buscar pedidos")
		return
	}
	defer rows.Close()

	type pedidoAlan struct {
		ID          int64   `json:"id"`
		WCOrderID   int64   `json:"wc_order_id"`
		Status      string  `json:"status"`
		DestNome    *string `json:"dest_nome"`
		DestProduto *string `json:"dest_produto"`
		ValorPedido float64 `json:"valor_pedido"`
		MotoboyNome string  `json:"motoboy_nome"`
		TsAprovado  *string `json:"ts_aprovado"`
	}

	var pedidos []pedidoAlan
	for rows.Next() {
		var p pedidoAlan
		if err := rows.Scan(
			&p.ID, &p.WCOrderID, &p.Status,
			&p.DestNome, &p.DestProduto, &p.ValorPedido,
			&p.MotoboyNome, &p.TsAprovado,
		); err != nil {
			slog.Error("[alan] erro ao scanear pedido", "err", err)
			continue
		}
		pedidos = append(pedidos, p)
	}

	httpx.WriteOK(w, map[string]any{"ok": true, "pedidos": pedidos, "total": len(pedidos)})
}

// Embalar — POST /alan/embalar
// Body: {pedido_id, motoboy_id?}
// Muda status agendado → embalado e opcionalmente atribui motoboy.
func (h *AlanHandler) Embalar(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PedidoID  int64  `json:"pedido_id"`
		MotoboyID *int64 `json:"motoboy_id"`
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

	var statusAtual string
	err := h.Pool.QueryRow(ctx,
		`SELECT status FROM sz_motoboy_pedidos WHERE id = $1`, req.PedidoID,
	).Scan(&statusAtual)
	if err != nil {
		httpx.WriteErr(w, http.StatusNotFound, "pedido não encontrado")
		return
	}
	if statusAtual != "agendado" {
		httpx.WriteErr(w, http.StatusConflict, "pedido não está em status agendado: "+statusAtual)
		return
	}

	// Atualiza status e opcionalmente atribui motoboy.
	_, err = h.Pool.Exec(ctx, `
		UPDATE sz_motoboy_pedidos
		SET status = 'embalado',
		    motoboy_id = COALESCE($2, motoboy_id)
		WHERE id = $1`,
		req.PedidoID, req.MotoboyID,
	)
	if err != nil {
		slog.Error("[alan] falha ao embalar pedido", "pedido_id", req.PedidoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao embalar pedido")
		return
	}

	_, _ = h.Pool.Exec(ctx, `
		INSERT INTO sz_motoboy_audit (pedido_id, acao, de_status, para_status, created_at)
		VALUES ($1, 'embalado_alan', 'agendado', 'embalado', NOW())`,
		req.PedidoID,
	)

	slog.Info("[alan] pedido embalado", "pedido_id", req.PedidoID, "motoboy_id", req.MotoboyID)
	httpx.WriteOK(w, map[string]any{"ok": true, "pedido_id": req.PedidoID, "status": "embalado"})
}

// ConfirmarFechamento — POST /alan/confirmar-fechamento
// Body: {fechamento_id}
// Marca sz_motoboy_fechamento.confirmado_alan = true.
func (h *AlanHandler) ConfirmarFechamento(w http.ResponseWriter, r *http.Request) {
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
		SET confirmado_alan = true, confirmado_alan_at = NOW()
		WHERE id = $1 AND confirmado_alan = false`,
		req.FechamentoID,
	)
	if err != nil {
		slog.Error("[alan] falha ao confirmar fechamento", "fechamento_id", req.FechamentoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao confirmar fechamento")
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.WriteErr(w, http.StatusNotFound, "fechamento não encontrado ou já confirmado")
		return
	}

	slog.Info("[alan] fechamento confirmado", "fechamento_id", req.FechamentoID)
	httpx.WriteOK(w, map[string]any{"ok": true, "fechamento_id": req.FechamentoID})
}

// Dashboard — GET /alan/dashboard
// KPIs do dia: entregues, frustrados, em_rota, pendentes, sem_motoboy, total_rs.
func (h *AlanHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var entregues, frustrados, emRota, pendentes, semMotoboy int
	var totalRS float64

	err := h.Pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status = 'entregue')                              AS entregues,
			COUNT(*) FILTER (WHERE status = 'frustrado')                             AS frustrados,
			COUNT(*) FILTER (WHERE status = 'em_rota')                               AS em_rota,
			COUNT(*) FILTER (WHERE status IN ('agendado','embalado'))                AS pendentes,
			COUNT(*) FILTER (WHERE status IN ('agendado','embalado') AND motoboy_id IS NULL) AS sem_motoboy,
			COALESCE(SUM(valor_pedido) FILTER (WHERE status = 'entregue'), 0)        AS total_rs
		FROM sz_motoboy_pedidos
		WHERE DATE(ts_aprovado) = CURRENT_DATE`,
	).Scan(&entregues, &frustrados, &emRota, &pendentes, &semMotoboy, &totalRS)
	if err != nil {
		slog.Error("[alan] falha ao buscar dashboard", "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao buscar dashboard")
		return
	}

	httpx.WriteOK(w, map[string]any{
		"entregues":   entregues,
		"frustrados":  frustrados,
		"em_rota":     emRota,
		"pendentes":   pendentes,
		"sem_motoboy": semMotoboy,
		"total_rs":    totalRS,
	})
}
