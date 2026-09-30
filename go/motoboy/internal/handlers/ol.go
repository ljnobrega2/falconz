// Package handlers — endpoints OL (Operador Logístico).
// Auth: portal session ou manage_woocommerce (via auth.AuthPortal).
package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/motoboy-service/internal/auth"
	"github.com/senderzz/motoboy-service/internal/httpx"
)

// statusWhitelist lista os status válidos para mudança via OL.
// Espelha o $allowed de sz_mb_api_ol_mudar_status() (rest-api.php:413), inclusive
// 'a_caminho' (sub-etapa operacional de em_rota: "saiu para a próxima parada").
var statusWhitelist = map[string]bool{
	"agendado": true, "embalado": true, "em_rota": true, "a_caminho": true,
	"entregue": true, "frustrado": true, "cancelado": true,
}

// OLHandler implementa os endpoints de Operador Logístico.
type OLHandler struct {
	Pool *pgxpool.Pool
}

// operatorScopedCDs resolve os CDs (centros de distribuição) que o operador
// logístico `userID` tem permissão de operar.
//
// SEC-GO-IDOR-CD (P2, auditoria de ataque 2026-06-20): os endpoints /ol/* são
// hoje protegidos só por RequireRole("operator") — QUALQUER operador pode
// mudar status / trocar motoboy / listar de QUALQUER pedido, de QUALQUER CD.
// Hoje NÃO é explorável (existe 1 único CD), mas vira IDOR cross-operador assim
// que entrar um 2º CD com um 2º operador.
//
// FIX DEFENSIVO (fail-OPEN por enquanto, de propósito): não existe ainda binding
// operador→CD no schema (não há tabela sz_operador_cds, nem coluna/meta de cd em
// senderzz_portal_users). Enquanto o binding não existir, retornamos (nil,false)
// = "sem escopo" e os handlers NÃO aplicam filtro — preserva 100% do fluxo de 1 CD.
//
// QUANDO O BINDING ENTRAR: implementar a consulta abaixo (ex.: SELECT cd_id FROM
// sz_operador_cds WHERE operador_user_id=$1) e retornar (cds,true). A partir daí
// o filtro `AND cd_id = ANY($cds)` já cabeado nos 3 handlers passa a valer e o
// IDOR cross-operador fecha SEM mais mexer nos handlers — só neste ponto.
//
// scoped=false  → operador sem restrição de CD (estado atual, 1 CD).
// scoped=true   → aplicar `AND cd_id = ANY(cds)` em toda query sobre pedidos.
func operatorScopedCDs(ctx context.Context, q queryer, userID int64) (cds []int64, scoped bool) {
	// FLIP-POINT: ao introduzir o binding operador→CD, trocar este retorno fixo
	// pela consulta real. Mantido como no-op deliberado (ver doc acima).
	_ = ctx
	_ = q
	_ = userID
	return nil, false
}

// olScopeFromReq extrai o user_id do operador autenticado da request e resolve
// seus CDs permitidos. Encapsula operatorScopedCDs para uso direto nos handlers.
// Retorna scoped=false quando não há operador no contexto (defensivo) ou quando
// não há binding de CD — em ambos os casos os handlers NÃO filtram por CD.
func (h *OLHandler) olScopeFromReq(r *http.Request) (cds []int64, scoped bool) {
	u := auth.PortalUserFromCtx(r.Context())
	if u == nil {
		return nil, false
	}
	return operatorScopedCDs(r.Context(), h.Pool, u.ID)
}

// MudarStatus — POST /ol/mudar-status
// Body: {pedido_id, status, motivo?}
func (h *OLHandler) MudarStatus(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PedidoID int64  `json:"pedido_id"`
		Status   string `json:"status"`
		Motivo   string `json:"motivo"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	if req.PedidoID == 0 || req.Status == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "pedido_id e status obrigatórios")
		return
	}
	if !statusWhitelist[req.Status] {
		httpx.WriteErr(w, http.StatusBadRequest, "status inválido")
		return
	}

	ctx := r.Context()

	// Busca status atual + motoboy para auditoria e validação. wc_order_id e
	// dest_telefone são necessários quando o OL muda para 'frustrado' (cálculo +
	// congelamento das penalties — espelha a baixa do motoboy em rota.go).
	var deStatus string
	var donoID *int64
	var wcOrderID int64
	var destTelefone *string
	err := h.Pool.QueryRow(ctx,
		`SELECT status, motoboy_id, wc_order_id, dest_telefone FROM sz_motoboy_pedidos WHERE id=$1`, req.PedidoID,
	).Scan(&deStatus, &donoID, &wcOrderID, &destTelefone)
	if err != nil {
		httpx.WriteErr(w, http.StatusNotFound, "pedido não encontrado")
		return
	}

	// PROIBIR DESPACHO SEM MOTOBOY: em_rota/a_caminho/entregue exigem motoboy
	// atribuído (não há "a caminho" de um pedido sem entregador).
	if (req.Status == "em_rota" || req.Status == "a_caminho" || req.Status == "entregue") && donoID == nil {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "Pedido sem motoboy: defina o motoboy antes de despachar.")
		return
	}

	// BAIXA EXIGE FOTO: concluir como entregue exige ao menos 1 comprovante (foto).
	if req.Status == "entregue" {
		var totalComprovantes int
		if err := h.Pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM sz_motoboy_comprovantes WHERE pedido_id=$1`, req.PedidoID,
		).Scan(&totalComprovantes); err != nil {
			slog.Error("[ol] falha ao contar comprovantes", "pedido_id", req.PedidoID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao validar comprovante")
			return
		}
		if totalComprovantes == 0 {
			httpx.WriteErr(w, http.StatusUnprocessableEntity, "Baixa exige foto do comprovante.")
			return
		}
	}

	// BRIDGE: motoboy + sz_orders na MESMA transação. O helper mapeia o status
	// (entregue→completo, frustrado→frustrado, cancelado→cancelled, em_rota→enviado,
	// embalado→embalado) e silenciosamente pula 'agendado' (sem equivalente em escopo).
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		slog.Error("[ol] falha ao iniciar transação", "pedido_id", req.PedidoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao atualizar status")
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if req.Status == "frustrado" {
		// CONGELAMENTO (snapshot) do financeiro de frustração quando o OL marca
		// frustrado — MESMAS colunas da baixa do motoboy (rota.go). Calculado DENTRO
		// da tx p/ leitura consistente. valor_taxa_frustrado é a TAXA DO MOTOBOY por
		// tentativa frustrada (sz_mbw_taxa_frustrado_mb_{id} > sz_mbw_taxa_frustrado >
		// default 5.00) — é o que a conciliação lê p/ PAGAR o entregador, NUNCA penalty
		// de produtor/afiliado. frustrado_isento marca a 1ª frustração por TELEFONE.
		// ts_frustrado=NOW() (re-stamp a cada marcação, igual a rota.go).
		tel := ""
		if destTelefone != nil {
			tel = *destTelefone
		}
		// donoID pode ser nil quando o OL frustra um pedido sem motoboy atribuído;
		// taxaFrustrado(…, 0) cai no global → default 5.00 (seguro).
		var mbID int64
		if donoID != nil {
			mbID = *donoID
		}
		taxaFrust := taxaFrustrado(ctx, tx, mbID)
		isento := frustradoIsento(ctx, tx, wcOrderID, tel)
		if _, err = tx.Exec(ctx, `
			UPDATE sz_motoboy_pedidos
			SET status='frustrado', ts_frustrado=NOW(),
			    frustrado_isento=$2, valor_taxa_frustrado=$3, valor_taxa_frustrado_afiliado=0
			WHERE id=$1`,
			req.PedidoID, isento, taxaFrust,
		); err != nil {
			slog.Error("[ol] falha ao mudar status (frustrado/freeze)", "pedido_id", req.PedidoID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao atualizar status")
			return
		}
		slog.Info("[ol] frustrado congelado", "pedido_id", req.PedidoID, "isento", isento,
			"taxa_frustrado_motoboy", taxaFrust)
	} else if _, err = tx.Exec(ctx,
		`UPDATE sz_motoboy_pedidos SET status=$1 WHERE id=$2`,
		req.Status, req.PedidoID,
	); err != nil {
		slog.Error("[ol] falha ao mudar status", "pedido_id", req.PedidoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao atualizar status")
		return
	}

	szRows, err := bridgeUpdatePedidoStatus(ctx, tx, req.PedidoID, req.Status)
	if err != nil {
		slog.Error("[ol] falha no bridge sz_orders", "pedido_id", req.PedidoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao atualizar status")
		return
	}

	if err = tx.Commit(ctx); err != nil {
		slog.Error("[ol] falha ao commitar mudança de status", "pedido_id", req.PedidoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao atualizar status")
		return
	}
	if szMapped, ok := motoboyStatusToSzOrder(req.Status); ok {
		logBridge("ol.MudarStatus", req.PedidoID, req.Status, szMapped, szRows)
	}

	_, _ = h.Pool.Exec(ctx, `
		INSERT INTO sz_motoboy_audit (pedido_id, acao, de_status, para_status, meta, created_at)
		VALUES ($1, 'status_alterado_ol', $2, $3, $4, NOW())`,
		req.PedidoID, deStatus, req.Status, req.Motivo,
	)

	slog.Info("[ol] status alterado", "pedido_id", req.PedidoID, "de", deStatus, "para", req.Status)
	httpx.WriteOK(w, map[string]any{"ok": true, "status": req.Status})
}

// TrocarMotoboy — POST /ol/trocar-motoboy
// Body: {pedido_id, motoboy_id}
func (h *OLHandler) TrocarMotoboy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PedidoID  int64 `json:"pedido_id"`
		MotoboyID int64 `json:"motoboy_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	if req.PedidoID == 0 || req.MotoboyID == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "pedido_id e motoboy_id obrigatórios")
		return
	}

	ctx := r.Context()

	// Verifica motoboy ativo.
	var ativo bool
	err := h.Pool.QueryRow(ctx,
		`SELECT ativo FROM sz_motoboys WHERE id=$1`, req.MotoboyID,
	).Scan(&ativo)
	if err != nil || !ativo {
		httpx.WriteErr(w, http.StatusBadRequest, "motoboy não encontrado ou inativo")
		return
	}

	_, err = h.Pool.Exec(ctx,
		`UPDATE sz_motoboy_pedidos SET motoboy_id=$1 WHERE id=$2`,
		req.MotoboyID, req.PedidoID,
	)
	if err != nil {
		slog.Error("[ol] falha ao trocar motoboy", "pedido_id", req.PedidoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao trocar motoboy")
		return
	}

	_, _ = h.Pool.Exec(ctx, `
		INSERT INTO sz_motoboy_audit (pedido_id, acao, meta, created_at)
		VALUES ($1, 'motoboy_trocado_ol', $2, NOW())`,
		req.PedidoID, req.MotoboyID,
	)

	slog.Info("[ol] motoboy trocado", "pedido_id", req.PedidoID, "motoboy_id", req.MotoboyID)
	httpx.WriteOK(w, map[string]any{"ok": true})
}

// MotoboysDodia — GET /ol/motoboys-do-dia
// Retorna pedidos do dia agrupados por motoboy com KPIs.
func (h *OLHandler) MotoboysDoDia(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	rows, err := h.Pool.Query(ctx, `
		SELECT
			COALESCE(m.id, 0)             AS motoboy_id,
			COALESCE(m.nome, 'Sem motoboy') AS motoboy_nome,
			COUNT(*)                       AS total,
			COUNT(*) FILTER (WHERE p.status = 'entregue')  AS entregues,
			COUNT(*) FILTER (WHERE p.status = 'frustrado') AS frustrados,
			COUNT(*) FILTER (WHERE p.status = 'em_rota')   AS em_rota,
			COUNT(*) FILTER (WHERE p.status = 'agendado' OR p.status = 'embalado') AS pendentes,
			COALESCE(SUM(p.valor_pedido) FILTER (WHERE p.status = 'entregue'), 0) AS total_entregue_rs
		FROM sz_motoboy_pedidos p
		LEFT JOIN sz_motoboys m ON m.id = p.motoboy_id
		WHERE p.ts_aprovado::date = CURRENT_DATE
		GROUP BY m.id, m.nome
		ORDER BY motoboy_nome`)
	if err != nil {
		slog.Error("[ol] falha ao buscar motoboys do dia", "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao buscar dados")
		return
	}
	defer rows.Close()

	type motoboyKPI struct {
		MotoboyID       int64   `json:"motoboy_id"`
		MotoboyNome     string  `json:"motoboy_nome"`
		Total           int     `json:"total"`
		Entregues       int     `json:"entregues"`
		Frustrados      int     `json:"frustrados"`
		EmRota          int     `json:"em_rota"`
		Pendentes       int     `json:"pendentes"`
		TotalEntregueRS float64 `json:"total_entregue_rs"`
	}

	var result []motoboyKPI
	for rows.Next() {
		var k motoboyKPI
		if err := rows.Scan(
			&k.MotoboyID, &k.MotoboyNome,
			&k.Total, &k.Entregues, &k.Frustrados, &k.EmRota, &k.Pendentes,
			&k.TotalEntregueRS,
		); err != nil {
			continue
		}
		result = append(result, k)
	}

	httpx.WriteOK(w, map[string]any{"ok": true, "motoboys": result})
}

// Motoboys — GET /ol/motoboys
// Lista todos os motoboys ativos.
func (h *OLHandler) Motoboys(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	rows, err := h.Pool.Query(ctx, `
		SELECT id, nome, telefone, ativo
		FROM sz_motoboys
		WHERE ativo = true
		ORDER BY nome`)
	if err != nil {
		slog.Error("[ol] falha ao listar motoboys", "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao buscar motoboys")
		return
	}
	defer rows.Close()

	type motoboy struct {
		ID       int64  `json:"id"`
		Nome     string `json:"nome"`
		Telefone string `json:"telefone"`
		Ativo    bool   `json:"ativo"`
	}

	var result []motoboy
	for rows.Next() {
		var m motoboy
		if err := rows.Scan(&m.ID, &m.Nome, &m.Telefone, &m.Ativo); err != nil {
			continue
		}
		result = append(result, m)
	}

	httpx.WriteOK(w, map[string]any{"ok": true, "motoboys": result})
}
