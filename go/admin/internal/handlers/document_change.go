// Package handlers — aprovação admin da troca de documento CPF ⇄ CNPJ.
//
// FEAT-DOC-CHANGE-2026-07-03. O titular pede a troca pelo portal
// (go/portal/.../document_change.go, tabela senderzz_document_change_request). Aqui o
// ADMIN revisa e decide. Ao APROVAR, o novo nome (razão social / nome civil) e o novo
// documento passam a valer em TODAS as menções ao usuário: o display do site lê
// senderzz_portal_users.nome (coluna `name` é alias gerado) e .document, então um único
// UPDATE propaga para todo o site dali pra frente. A chave PIX derivada do CPF antigo é
// LIMPA na aprovação (decisão do dono 2026-07-03) para forçar reconfiguração.
//
// Rotas (protegidas pelo middleware de admin):
//
//	GET  /document-changes                      — lista (filtro por status)
//	GET  /document-changes/{id}                 — detalhe
//	GET  /document-changes/{id}/attachment      — baixa o cartão CNPJ (PII, auth-only)
//	POST /document-changes/{id}/approve         — aplica a troca
//	POST /document-changes/{id}/reject          — rejeita (motivo obrigatório)
package handlers

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/admin-service/internal/auth"
	"github.com/senderzz/admin-service/internal/httpx"
)

type DocumentChangeHandler struct{ Pool *pgxpool.Pool }

func (h *DocumentChangeHandler) tableExists(ctx context.Context, name string) bool {
	return tableExistsCached(ctx, h.Pool, name)
}

// docChangeDir — mesmo diretório privado onde o PORTAL grava os anexos (volume
// falk_uploads compartilhado). Configurável via DOC_CHANGE_UPLOAD_PATH.
func docChangeDir() string {
	if v := strings.TrimSpace(os.Getenv("DOC_CHANGE_UPLOAD_PATH")); v != "" {
		return v
	}
	return "./uploads/doc-changes"
}

type docChangeItem struct {
	ID             int64   `json:"id"`
	UserID         int64   `json:"user_id"`
	UserEmail      string  `json:"user_email"`
	UserRole       string  `json:"user_role"`
	Direction      string  `json:"direction"`
	TargetNome     string  `json:"target_nome"`
	TargetDocument string  `json:"target_document"`
	Status         string  `json:"status"`
	HasAttachment  bool    `json:"has_attachment"`
	OldNome        string  `json:"old_nome"`
	OldDocument    string  `json:"old_document"`
	Notes          *string `json:"notes"`
	CreatedAt      string  `json:"created_at"`
	ReviewedAt     *string `json:"reviewed_at"`
}

const docChangeSelect = `
	SELECT d.id, d.user_id, COALESCE(u.email,''), d.role, d.direction,
	       d.target_nome, d.target_document, d.status,
	       (d.attachment_path IS NOT NULL AND d.attachment_path <> '') AS has_attachment,
	       d.old_nome, d.old_document, d.notes,
	       d.created_at::text, d.reviewed_at::text
	  FROM senderzz_document_change_request d
	  LEFT JOIN senderzz_portal_users u ON u.id = d.user_id`

func scanDocChange(row pgx.Row) (docChangeItem, error) {
	var x docChangeItem
	err := row.Scan(&x.ID, &x.UserID, &x.UserEmail, &x.UserRole, &x.Direction,
		&x.TargetNome, &x.TargetDocument, &x.Status, &x.HasAttachment,
		&x.OldNome, &x.OldDocument, &x.Notes, &x.CreatedAt, &x.ReviewedAt)
	return x, err
}

// List — GET /document-changes?status=pending|approved|rejected&limit=200
func (h *DocumentChangeHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "senderzz_document_change_request") {
		httpx.JSON(w, 200, map[string]any{"items": []docChangeItem{}, "count": 0})
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	status := strings.ToLower(strings.TrimSpace(q.Get("status")))

	var rows pgx.Rows
	var err error
	if status != "" {
		rows, err = h.Pool.Query(ctx,
			docChangeSelect+` WHERE d.status=$1 ORDER BY d.created_at DESC LIMIT $2`, status, limit)
	} else {
		rows, err = h.Pool.Query(ctx,
			docChangeSelect+` ORDER BY d.created_at DESC LIMIT $1`, limit)
	}
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []docChangeItem{}
	for rows.Next() {
		x, serr := scanDocChange(rows)
		if serr != nil {
			httpx.Err(w, 500, "scan_error", serr.Error())
			return
		}
		out = append(out, x)
	}
	httpx.JSON(w, 200, map[string]any{"items": out, "count": len(out)})
}

// Get — GET /document-changes/{id}
func (h *DocumentChangeHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	if !h.tableExists(r.Context(), "senderzz_document_change_request") {
		httpx.Err(w, 503, "table_missing", "tabela ausente")
		return
	}
	x, err := scanDocChange(h.Pool.QueryRow(r.Context(), docChangeSelect+` WHERE d.id=$1`, id))
	if err != nil {
		httpx.Err(w, 404, "not_found", "solicitação não encontrada")
		return
	}
	httpx.JSON(w, 200, x)
}

// Attachment — GET /document-changes/{id}/attachment
// Serve o cartão CNPJ (PII) SOMENTE via este endpoint autenticado, com
// Content-Disposition: attachment (nunca inline — impede render no browser).
func (h *DocumentChangeHandler) Attachment(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	var name string
	if err := h.Pool.QueryRow(r.Context(),
		`SELECT COALESCE(attachment_path,'') FROM senderzz_document_change_request WHERE id=$1`,
		id).Scan(&name); err != nil {
		httpx.Err(w, 404, "not_found", "solicitação não encontrada")
		return
	}
	name = filepath.Base(strings.TrimSpace(name)) // anti path-traversal
	if name == "" || name == "." || name == "/" {
		httpx.Err(w, 404, "not_found", "sem anexo")
		return
	}
	full := filepath.Join(docChangeDir(), name)
	f, ferr := os.Open(full)
	if ferr != nil {
		httpx.Err(w, 404, "not_found", "anexo não encontrado no armazenamento")
		return
	}
	defer f.Close()
	info, _ := f.Stat()
	mt := time.Time{}
	if info != nil {
		mt = info.ModTime()
	}

	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, name, mt, f)
}

// approveReq / rejectReq — corpo das decisões.
type docChangeDecisionReq struct {
	Notes string `json:"notes"`
}

// Approve — POST /document-changes/{id}/approve
// Aplica a troca: nome + documento em senderzz_portal_users; limpa a chave PIX
// derivada do CPF (força reconfiguração); marca o pedido approved; registra auditoria.
func (h *DocumentChangeHandler) Approve(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	var body docChangeDecisionReq
	_ = httpx.DecodeJSON(r, &body)

	ctx := r.Context()
	if !h.tableExists(ctx, "senderzz_document_change_request") {
		httpx.Err(w, 503, "table_missing", "tabela ausente")
		return
	}

	var adminID int64
	if admin := auth.FromCtx(ctx); admin != nil {
		adminID = admin.ID
	}

	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer tx.Rollback(ctx)

	// Carrega + trava o pedido (evita dupla aprovação em corrida).
	var userID int64
	var direction, targetNome, targetDoc, status string
	err = tx.QueryRow(ctx,
		`SELECT user_id, direction, target_nome, target_document, status
		   FROM senderzz_document_change_request
		  WHERE id=$1 FOR UPDATE`, id).
		Scan(&userID, &direction, &targetNome, &targetDoc, &status)
	if err != nil {
		if err == pgx.ErrNoRows {
			httpx.Err(w, 404, "not_found", "solicitação não encontrada")
			return
		}
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if status != "pending" {
		httpx.Err(w, 409, "invalid_state", "solicitação não está pendente")
		return
	}

	// Reconferência de unicidade no momento da aprovação (o cenário mudou desde o pedido).
	var takenBy int64
	_ = tx.QueryRow(ctx,
		`SELECT id FROM senderzz_portal_users
		  WHERE regexp_replace(COALESCE(document,''), '\D', '', 'g') = $1 AND id <> $2 LIMIT 1`,
		targetDoc, userID).Scan(&takenBy)
	if takenBy > 0 {
		httpx.Err(w, 409, "duplicate", "documento já está em uso por outra conta")
		return
	}

	// Captura o documento ATUAL (que vira "antigo") + a chave de carteira do usuário
	// (= wp_user_id quando existe, senão o id do portal — mesmo escopo que
	// codWalletKey usa em sz_cod_withdraw_accounts.user_id). Necessário para invalidar
	// as contas PIX de saque presas ao CPF/CNPJ antigo (abaixo).
	var oldDoc string
	var walletKey int64
	_ = tx.QueryRow(ctx,
		`SELECT regexp_replace(COALESCE(document,''), '\D', '', 'g'),
		        COALESCE(NULLIF(wp_user_id, 0), id)
		   FROM senderzz_portal_users WHERE id = $1`, userID).Scan(&oldDoc, &walletKey)

	// UPDATE central: nome + documento. A chave PIX do afiliado (settings.pix_key/
	// pix_key_tipo) é removida para forçar reconfiguração — o CPF antigo não vale mais.
	if _, err = tx.Exec(ctx,
		`UPDATE senderzz_portal_users
		    SET nome = $2,
		        document = $3,
		        settings = (COALESCE(settings,'{}'::jsonb) - 'pix_key' - 'pix_key_tipo')
		  WHERE id = $1`,
		userID, targetNome, targetDoc); err != nil {
		httpx.Err(w, 500, "db_error", "falha ao atualizar usuário: "+err.Error())
		return
	}

	// Contas PIX de saque do PRODUTOR vivem em sz_cod_withdraw_accounts (não em
	// settings). As presas ao documento ANTIGO (holder_cpf = doc antigo, OU a própria
	// chave PIX = o documento) ficam inválidas após a troca. Best-practice: DESATIVAR
	// (soft, active=false) — nunca DELETAR uma conta bancária salva — forçando o
	// produtor a recadastrar/reconfirmar. Best-effort e guardado por tableExists.
	if oldDoc != "" && h.tableExists(ctx, "sz_cod_withdraw_accounts") {
		_, _ = tx.Exec(ctx,
			`UPDATE sz_cod_withdraw_accounts
			    SET active = FALSE
			  WHERE user_id = $1
			    AND COALESCE(active, FALSE) IS TRUE
			    AND (
			         regexp_replace(COALESCE(holder_cpf,''), '\D', '', 'g') = $2
			      OR regexp_replace(COALESCE(pix_key,''),    '\D', '', 'g') = $2
			    )`, walletKey, oldDoc)
	}

	notes := strings.TrimSpace(body.Notes)
	var notesArg any
	if notes != "" {
		notesArg = notes
	}
	if _, err = tx.Exec(ctx,
		`UPDATE senderzz_document_change_request
		    SET status='approved', reviewed_at=NOW(), reviewer_admin_id=$2,
		        notes=COALESCE($3, notes)
		  WHERE id=$1`, id, adminID, notesArg); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// Auditoria (best-effort — dentro da tx para consistência).
	_, _ = tx.Exec(ctx,
		`INSERT INTO senderzz_portal_audit_log (user_id, action, entity_type, entity_id, meta, created_at)
		 VALUES ($1, 'document_change_approved', 'portal_user', $2,
		         jsonb_build_object('direction',$3::text,'new_document',$4::text,'admin_id',$5::bigint), NOW())`,
		adminID, userID, direction, targetDoc, adminID)

	if err := tx.Commit(ctx); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	httpx.JSON(w, 200, map[string]any{"ok": true, "id": id, "user_id": userID})
}

// Reject — POST /document-changes/{id}/reject (motivo obrigatório).
func (h *DocumentChangeHandler) Reject(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	var body docChangeDecisionReq
	_ = httpx.DecodeJSON(r, &body)
	notes := strings.TrimSpace(body.Notes)
	if notes == "" {
		httpx.Err(w, 400, "validation", "motivo (notes) é obrigatório para rejeição")
		return
	}
	if !h.tableExists(r.Context(), "senderzz_document_change_request") {
		httpx.Err(w, 503, "table_missing", "tabela ausente")
		return
	}
	var adminID int64
	if admin := auth.FromCtx(r.Context()); admin != nil {
		adminID = admin.ID
	}
	tag, err := h.Pool.Exec(r.Context(),
		`UPDATE senderzz_document_change_request
		    SET status='rejected', notes=$2, reviewed_at=NOW(), reviewer_admin_id=$3
		  WHERE id=$1 AND status='pending'`, id, notes, adminID)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.Err(w, 409, "invalid_state", "solicitação não está pendente")
		return
	}
	httpx.JSON(w, 200, map[string]any{"ok": true, "id": id})
}
