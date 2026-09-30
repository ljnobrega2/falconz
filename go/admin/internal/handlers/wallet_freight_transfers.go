// Package handlers — fila de aprovação admin para transferências Carteira
// COD → Carteira de Expedição (TPC) pedidas pelo produtor em go/portal
// (wallet.go FreightTransfer).
//
// Débito da carteira COD já ocorreu na SOLICITAÇÃO (go/portal). O crédito na
// carteira de frete SÓ acontece depois que o admin realmente PAGOU um PIX real
// (Melhor Envio) — dinheiro sai da empresa (via PIX) e entra na carteira TPC
// do produtor. Por isso "Aprovar" NÃO credita na hora: fluxo em 2 passos:
//
//  1. GeneratePix  — POST .../generate-pix  → emite PIX real (ME) no valor da
//     transferência, vincula à recarga (sz_wallet_freight_transfers.recarga_id).
//     Devolve QR/copia-e-cola pro admin pagar. Transferência continua 'pending'.
//  2. ConfirmPix   — POST .../confirm-pix   → SÓ credita tpc_carteira depois
//     que o admin confirma que pagou (marca tpc_recargas 'confirmado' e a
//     transferência 'approved').
//
// Reject cancela o PIX gerado (se houver) e estorna o débito COD.
package handlers

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/auth"
	"github.com/senderzz/admin-service/internal/httpx"
	"github.com/senderzz/admin-service/internal/melhorenvio"
)

type WalletFreightTransfersHandler struct {
	Pool *pgxpool.Pool
}

var wftMeClient = melhorenvio.NewClient()

func (h *WalletFreightTransfersHandler) tableExists(ctx context.Context, name string) bool {
	return tableExistsCached(ctx, h.Pool, name)
}

func (h *WalletFreightTransfersHandler) decodeBody(w http.ResponseWriter, r *http.Request, out any) bool {
	if err := httpx.DecodeJSON(r, out); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return false
	}
	return true
}

// List — GET /wallet-freight-transfers?status=pending
func (h *WalletFreightTransfersHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_wallet_freight_transfers") {
		httpx.Err(w, 503, "tables_missing", "sz_wallet_freight_transfers ainda não migrada")
		return
	}
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "pending"
	}

	var rows pgx.Rows
	var err error
	if status == "all" {
		rows, err = h.Pool.Query(ctx,
			`SELECT t.id, t.user_id, t.wp_user_id, t.amount, t.status,
			        COALESCE(t.admin_note,''), t.requested_at, t.decided_at, t.decided_by,
			        t.recarga_id, COALESCE(rc.status,''), COALESCE(rc.pix_qr,''), COALESCE(rc.pix_codigo,''),
			        COALESCE(u.nome,''), COALESCE(u.email,'')
			   FROM sz_wallet_freight_transfers t
			   LEFT JOIN senderzz_portal_users u ON u.id = t.portal_user_id
			   LEFT JOIN tpc_recargas rc ON rc.id = t.recarga_id
			  ORDER BY t.requested_at DESC LIMIT 300`)
	} else {
		rows, err = h.Pool.Query(ctx,
			`SELECT t.id, t.user_id, t.wp_user_id, t.amount, t.status,
			        COALESCE(t.admin_note,''), t.requested_at, t.decided_at, t.decided_by,
			        t.recarga_id, COALESCE(rc.status,''), COALESCE(rc.pix_qr,''), COALESCE(rc.pix_codigo,''),
			        COALESCE(u.nome,''), COALESCE(u.email,'')
			   FROM sz_wallet_freight_transfers t
			   LEFT JOIN senderzz_portal_users u ON u.id = t.portal_user_id
			   LEFT JOIN tpc_recargas rc ON rc.id = t.recarga_id
			  WHERE t.status = $1
			  ORDER BY t.requested_at DESC LIMIT 300`, status)
	}
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	type row struct {
		ID          int64   `json:"id"`
		UserID      int64   `json:"user_id"`
		WPUserID    int64   `json:"wp_user_id"`
		Amount      float64 `json:"amount"`
		Status      string  `json:"status"`
		AdminNote   string  `json:"admin_note"`
		RequestedAt any     `json:"requested_at"`
		DecidedAt   any     `json:"decided_at"`
		DecidedBy   *int64  `json:"decided_by"`
		RecargaID   *int64  `json:"recarga_id"`
		RecargaStat string  `json:"recarga_status"`
		PixQR       string  `json:"pix_qr"`
		PixCodigo   string  `json:"pix_codigo"`
		Nome        string  `json:"nome"`
		Email       string  `json:"email"`
	}
	out := []row{}
	for rows.Next() {
		var it row
		if err := rows.Scan(&it.ID, &it.UserID, &it.WPUserID, &it.Amount, &it.Status,
			&it.AdminNote, &it.RequestedAt, &it.DecidedAt, &it.DecidedBy,
			&it.RecargaID, &it.RecargaStat, &it.PixQR, &it.PixCodigo,
			&it.Nome, &it.Email); err != nil {
			continue
		}
		out = append(out, it)
	}
	httpx.JSON(w, 200, map[string]any{"ok": true, "data": out})
}

// GeneratePix — POST /wallet-freight-transfers/{id}/generate-pix
//
// Emite um PIX real (Melhor Envio) no valor da transferência para o admin
// pagar. NÃO credita nada ainda — só depois de ConfirmPix. Se já existe um
// PIX pendente para esta transferência, reaproveita (idempotente).
func (h *WalletFreightTransfersHandler) GeneratePix(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(r)
	if !ok {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_wallet_freight_transfers") || !h.tableExists(ctx, "tpc_recargas") {
		httpx.Err(w, 503, "tables_missing", "tabelas ainda não migradas")
		return
	}
	if !wftMeClient.HasToken() {
		httpx.Err(w, 503, "me_sem_token", "ME_TOKEN não configurado — emissão de PIX indisponível")
		return
	}

	var portalUserID int64
	var amount float64
	var status string
	var recargaID *int64
	if err := h.Pool.QueryRow(ctx,
		`SELECT portal_user_id, amount, status, recarga_id FROM sz_wallet_freight_transfers WHERE id=$1`,
		id).Scan(&portalUserID, &amount, &status, &recargaID); err != nil {
		httpx.Err(w, 404, "not_found", "transferência não encontrada")
		return
	}
	if status != "pending" {
		httpx.Err(w, 409, "invalid_state", "transferência já foi decidida")
		return
	}

	// Já existe PIX pendente para esta transferência — reaproveita em vez de
	// gerar outro (evita PIX duplicado se o admin recarregar a tela).
	if recargaID != nil {
		var qr, codigo, rstatus string
		var expiresAt time.Time
		if err := h.Pool.QueryRow(ctx,
			`SELECT COALESCE(pix_qr,''), COALESCE(pix_codigo,''), status, expires_at
			   FROM tpc_recargas WHERE id=$1`, *recargaID).Scan(&qr, &codigo, &rstatus, &expiresAt); err == nil {
			if rstatus == "pendente" {
				httpx.JSON(w, 200, map[string]any{
					"ok": true, "id": id, "recarga_id": *recargaID,
					"qr_src": qr, "copia_cola": codigo, "expires_at": expiresAt.Format(time.RFC3339),
				})
				return
			}
		}
	}

	redirectURL := strings.TrimRight(os.Getenv("APP_BASE_URL"), "/") + "/tpc-recarga-retorno?wft=" + strconv.FormatInt(id, 10)

	pix, err := wftMeClient.GerarPix(ctx, amount, redirectURL)
	if err != nil {
		httpx.Err(w, 502, "me_erro", "Erro ao gerar PIX no Melhor Envio: "+err.Error())
		return
	}

	expiresAt := time.Now().UTC().Add(30 * time.Minute)
	if pix.ExpiresTS > 0 {
		expiresAt = time.Unix(pix.ExpiresTS, 0).UTC()
	}
	mePixID := pix.PixID
	if mePixID == "" {
		mePixID = "wft-" + strconv.FormatInt(id, 10) + "-" + strconv.FormatInt(time.Now().Unix(), 10)
	}

	var newRecargaID int64
	if err := h.Pool.QueryRow(ctx,
		`INSERT INTO tpc_recargas (user_id, valor, status, me_pix_id, pix_qr, pix_codigo, expires_at, created_at)
		 VALUES ($1, $2, 'pendente', $3, $4, $5, $6, NOW())
		 RETURNING id`,
		portalUserID, amount, mePixID, pix.QRSrc, pix.CopiaCola, expiresAt).Scan(&newRecargaID); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if _, err := h.Pool.Exec(ctx,
		`UPDATE sz_wallet_freight_transfers SET recarga_id=$1 WHERE id=$2::bigint`,
		newRecargaID, id); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 200, map[string]any{
		"ok": true, "id": id, "recarga_id": newRecargaID,
		"qr_src": pix.QRSrc, "copia_cola": pix.CopiaCola, "link": pix.Link,
		"expires_at": expiresAt.Format(time.RFC3339),
	})
}

// ConfirmPix — POST /wallet-freight-transfers/{id}/confirm-pix
//
// SÓ chamado depois que o admin efetivamente pagou o PIX gerado por
// GeneratePix. Credita tpc_carteira (chave portal_user_id nativa) + ledger tpc_transacoes
// + marca a recarga 'confirmado' + a transferência 'approved'. Exige que um
// PIX tenha sido gerado (recarga_id != NULL) — não permite pular a etapa.
func (h *WalletFreightTransfersHandler) ConfirmPix(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(r)
	if !ok {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_wallet_freight_transfers") || !h.tableExists(ctx, "tpc_carteira") {
		httpx.Err(w, 503, "tables_missing", "tabelas ainda não migradas")
		return
	}
	var body struct {
		AdminNote string `json:"admin_note"`
	}
	_ = h.decodeBody(w, r, &body)

	admin := auth.FromCtx(ctx)
	var adminID int64
	if admin != nil {
		adminID = admin.ID
	}

	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer tx.Rollback(ctx)

	var portalUserID int64
	var amount float64
	var status string
	var recargaID *int64
	if err := tx.QueryRow(ctx,
		`SELECT portal_user_id, amount, status, recarga_id FROM sz_wallet_freight_transfers WHERE id=$1 FOR UPDATE`,
		id).Scan(&portalUserID, &amount, &status, &recargaID); err != nil {
		httpx.Err(w, 404, "not_found", "transferência não encontrada")
		return
	}
	if status != "pending" {
		httpx.Err(w, 409, "invalid_state", "transferência já foi decidida")
		return
	}
	// P0 — não credita sem PIX real gerado e pago. Nunca pula a etapa de
	// GeneratePix (é o que garante que o dinheiro realmente saiu via PIX).
	if recargaID == nil {
		httpx.Err(w, 422, "pix_nao_gerado", "Gere o PIX antes de confirmar o pagamento.")
		return
	}
	var recargaStatus string
	if err := tx.QueryRow(ctx,
		`SELECT status FROM tpc_recargas WHERE id=$1 FOR UPDATE`, *recargaID).Scan(&recargaStatus); err != nil {
		httpx.Err(w, 404, "not_found", "recarga PIX não encontrada")
		return
	}
	if recargaStatus == "confirmado" {
		httpx.Err(w, 409, "invalid_state", "PIX já confirmado")
		return
	}
	if recargaStatus != "pendente" {
		httpx.Err(w, 422, "pix_invalido", "PIX não está mais pendente (expirado/cancelado). Gere um novo.")
		return
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO tpc_carteira (user_id, saldo, saldo_reservado)
		 VALUES ($1, 0, 0)
		 ON CONFLICT (user_id) DO NOTHING`, portalUserID); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// $3/$4 vão como STRING (não int64): um placeholder usado só dentro de um
	// cast ::text faz o Postgres anunciar o OID do parâmetro como text (25) —
	// pgx então precisa de um codec int64→text, que não existe (só há
	// int64→int8/bigint). "cannot find encode plan" é exatamente esse erro.
	// Passando já como string, some o cast e o codec casa direto (text→text).
	idStr := strconv.FormatInt(id, 10)
	if _, err := tx.Exec(ctx,
		`INSERT INTO tpc_transacoes (user_id, tipo, valor, saldo_apos, descricao, referencia, status, actor_id)
		 VALUES ($1, 'credito', $2,
		         (SELECT saldo FROM tpc_carteira WHERE user_id=$1) + $2,
		         'Transferência da carteira COD #'||$3||' (PIX confirmado)', 'freight_transfer:'||$3,
		         'confirmado', NULLIF($4::bigint,0))
		 ON CONFLICT (user_id, referencia, tipo) DO NOTHING`,
		portalUserID, amount, idStr, adminID); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	if _, err := tx.Exec(ctx,
		`UPDATE tpc_carteira SET saldo = saldo + $2 WHERE user_id = $1`,
		portalUserID, amount); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	if _, err := tx.Exec(ctx,
		`UPDATE tpc_recargas SET status='confirmado', paid_at=NOW() WHERE id=$1::bigint`,
		*recargaID); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	if _, err := tx.Exec(ctx,
		`UPDATE sz_wallet_freight_transfers
		 SET status='approved', decided_at=NOW(), decided_by=NULLIF($1::bigint,0), admin_note=NULLIF($2::text,'')
		 WHERE id=$3::bigint`, adminID, body.AdminNote, id); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	httpx.JSON(w, 200, map[string]any{"ok": true, "id": id, "status": "approved", "credited": amount})
}

// Reject — POST /wallet-freight-transfers/{id}/reject
func (h *WalletFreightTransfersHandler) Reject(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(r)
	if !ok {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_wallet_freight_transfers") {
		httpx.Err(w, 503, "tables_missing", "sz_wallet_freight_transfers ainda não migrada")
		return
	}
	var body struct {
		AdminNote string `json:"admin_note"`
	}
	_ = h.decodeBody(w, r, &body)

	admin := auth.FromCtx(ctx)
	var adminID int64
	if admin != nil {
		adminID = admin.ID
	}

	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer tx.Rollback(ctx)

	var codUserID int64
	var amount float64
	var status string
	var recargaID *int64
	if err := tx.QueryRow(ctx,
		`SELECT user_id, amount, status, recarga_id FROM sz_wallet_freight_transfers WHERE id=$1 FOR UPDATE`,
		id).Scan(&codUserID, &amount, &status, &recargaID); err != nil {
		httpx.Err(w, 404, "not_found", "transferência não encontrada")
		return
	}
	if status != "pending" {
		httpx.Err(w, 409, "invalid_state", "transferência já foi decidida")
		return
	}

	// Cancela o PIX gerado (se houver e ainda pendente) — nunca foi pago.
	if recargaID != nil {
		if _, err := tx.Exec(ctx,
			`UPDATE tpc_recargas SET status='cancelado' WHERE id=$1::bigint AND status='pendente'`,
			*recargaID); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
	}

	// Estorna o débito feito na Carteira COD quando o produtor solicitou.
	if _, err := tx.Exec(ctx,
		`INSERT INTO sz_cod_wallet_transactions
		    (user_id, type, status, amount, gross, fee, net, description, created_at, updated_at)
		 VALUES ($1, 'refund', 'available', $2, $2, 0, $2, $3, NOW(), NOW())`,
		codUserID, amount, "Estorno — transferência p/ carteira de frete #"+strconv.FormatInt(id, 10)+" rejeitada"); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	if _, err := tx.Exec(ctx,
		`UPDATE sz_wallet_freight_transfers
		 SET status='rejected', decided_at=NOW(), decided_by=NULLIF($1::bigint,0), admin_note=NULLIF($2::text,'')
		 WHERE id=$3::bigint`, adminID, body.AdminNote, id); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	httpx.JSON(w, 200, map[string]any{"ok": true, "id": id, "status": "rejected", "refunded": amount})
}
