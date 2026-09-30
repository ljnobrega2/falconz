// Package handlers — troca de documento CPF ⇄ CNPJ do titular da conta.
//
// FEAT-DOC-CHANGE-2026-07-03. O usuário (produtor | afiliado | cliente) abre um
// drawer no portal e solicita a troca do tipo de documento da PRÓPRIA conta:
//
//	cpf_to_cnpj — razão social + CNPJ + anexo do cartão CNPJ (PII).
//	cnpj_to_cpf — nome civil  + CPF  (sem anexo — decisão do dono 2026-07-03).
//
// A solicitação nasce 'pending' em senderzz_document_change_request e só é aplicada
// quando o ADMIN aprova (go/admin/.../document_change.go). Aqui só criamos/consultamos
// o pedido — NUNCA mexemos em senderzz_portal_users.nome/document (isso é privilégio
// do admin na aprovação).
//
// Rotas (protegidas por JWT do portal):
//
//	GET  /portal/account/document-change  — situação atual + pedido pendente (se houver)
//	POST /portal/account/document-change  — cria o pedido (multipart; anexo em cpf_to_cnpj)
//
// O anexo do cartão CNPJ é PII → gravado em DOC_CHANGE_UPLOAD_PATH (volume privado,
// SEM rota estática). É servido apenas pelo endpoint admin autenticado.
package handlers

import (
	crand "crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/portal-service/internal/auth"
	"github.com/senderzz/portal-service/internal/httpx"
)

// DocumentChangeHandler agrupa dependências do fluxo de troca de documento.
type DocumentChangeHandler struct{ Pool *pgxpool.Pool }

// docChangeUploadDir — diretório PRIVADO dos anexos de cartão CNPJ.
// Configurável via DOC_CHANGE_UPLOAD_PATH. Default: ./uploads/doc-changes.
// NUNCA é exposto por file-server (é PII) — só o endpoint admin autenticado lê.
func docChangeUploadDir() string {
	if v := strings.TrimSpace(os.Getenv("DOC_CHANGE_UPLOAD_PATH")); v != "" {
		return v
	}
	return "./uploads/doc-changes"
}

// validCNPJ valida um CNPJ brasileiro (14 dígitos + 2 dígitos verificadores mod-11).
// Rejeita comprimento != 14 e sequências repetidas. Espelha o algoritmo padrão da RFB.
func validCNPJ(cnpj string) bool {
	cnpj = onlyDigits(cnpj)
	if len(cnpj) != 14 {
		return false
	}
	allSame := true
	for i := 1; i < 14; i++ {
		if cnpj[i] != cnpj[0] {
			allSame = false
			break
		}
	}
	if allSame {
		return false
	}
	// Pesos dos dois dígitos verificadores (mod-11 com janela deslizante).
	weights1 := []int{5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}
	weights2 := []int{6, 5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}
	calc := func(weights []int, upto int) int {
		sum := 0
		for i := 0; i < upto; i++ {
			sum += int(cnpj[i]-'0') * weights[i]
		}
		r := sum % 11
		if r < 2 {
			return 0
		}
		return 11 - r
	}
	if calc(weights1, 12) != int(cnpj[12]-'0') {
		return false
	}
	if calc(weights2, 13) != int(cnpj[13]-'0') {
		return false
	}
	return true
}

// docChangeRow — projeção da linha de senderzz_document_change_request para o JSON.
type docChangeRow struct {
	ID             int64   `json:"id"`
	Direction      string  `json:"direction"`
	TargetNome     string  `json:"target_nome"`
	TargetDocument string  `json:"target_document"`
	Status         string  `json:"status"`
	HasAttachment  bool    `json:"has_attachment"`
	Notes          *string `json:"notes"`
	CreatedAt      string  `json:"created_at"`
	ReviewedAt     *string `json:"reviewed_at"`
}

// Get retorna o documento atual do titular + o pedido mais recente (se houver).
// GET /portal/account/document-change
func (h *DocumentChangeHandler) Get(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	var document string
	if err := h.Pool.QueryRow(r.Context(),
		`SELECT COALESCE(document, '') FROM senderzz_portal_users WHERE id = $1`, u.ID,
	).Scan(&document); err != nil {
		slog.Error("[doc_change] falha ao ler document", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	document = onlyDigits(document)
	curType := "" // "cpf" | "cnpj" | "" (sem documento)
	switch len(document) {
	case 11:
		curType = "cpf"
	case 14:
		curType = "cnpj"
	}

	resp := map[string]any{
		"current_document": document,
		"current_type":     curType,
		"pending":          nil,
	}

	// Pedido mais recente (qualquer status) — o front destaca 'pending'/'rejected'.
	var row docChangeRow
	var attach *string
	err := h.Pool.QueryRow(r.Context(),
		`SELECT id, direction, target_nome, target_document, status,
		        attachment_path, notes, created_at::text, reviewed_at::text
		   FROM senderzz_document_change_request
		  WHERE user_id = $1
		  ORDER BY created_at DESC
		  LIMIT 1`, u.ID,
	).Scan(&row.ID, &row.Direction, &row.TargetNome, &row.TargetDocument, &row.Status,
		&attach, &row.Notes, &row.CreatedAt, &row.ReviewedAt)
	if err == nil {
		row.HasAttachment = attach != nil && *attach != ""
		resp["pending"] = row
	} else if err != pgx.ErrNoRows {
		slog.Error("[doc_change] falha ao ler pedido", "user_id", u.ID, "err", err)
	}

	httpx.WriteOK(w, resp)
}

// Create cria uma solicitação de troca de documento (multipart).
// POST /portal/account/document-change
//
//	campos: direction, target_nome, target_document
//	arquivo (só cpf_to_cnpj): attachment (cartão CNPJ — jpeg/png/pdf)
func (h *DocumentChangeHandler) Create(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	// 24 MB de teto de parse (anexo + campos). O limite real (16MB) é do header.Size.
	if err := r.ParseMultipartForm(24 << 20); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "envio inválido: "+err.Error())
		return
	}

	direction := strings.TrimSpace(r.FormValue("direction"))
	targetNome := strings.TrimSpace(r.FormValue("target_nome"))
	targetDoc := onlyDigits(r.FormValue("target_document"))

	if direction != "cpf_to_cnpj" && direction != "cnpj_to_cpf" {
		httpx.WriteErr(w, http.StatusBadRequest, "direção inválida")
		return
	}
	if len(targetNome) < 2 {
		label := "nome civil"
		if direction == "cpf_to_cnpj" {
			label = "razão social"
		}
		httpx.WriteErr(w, http.StatusBadRequest, "informe a "+label)
		return
	}

	// Validação do novo documento por direção.
	if direction == "cpf_to_cnpj" {
		if !validCNPJ(targetDoc) {
			httpx.WriteErr(w, http.StatusBadRequest, "CNPJ inválido")
			return
		}
	} else {
		if !validCPF(targetDoc) {
			httpx.WriteErr(w, http.StatusBadRequest, "CPF inválido")
			return
		}
	}

	// Coerência com o documento atual: só faz sentido virar CNPJ se hoje é CPF (ou
	// vazio), e vice-versa. Também bloqueia trocar por um documento idêntico.
	var curDoc string
	if err := h.Pool.QueryRow(r.Context(),
		`SELECT COALESCE(document, '') FROM senderzz_portal_users WHERE id = $1`, u.ID,
	).Scan(&curDoc); err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	curDoc = onlyDigits(curDoc)
	if curDoc == targetDoc {
		httpx.WriteErr(w, http.StatusBadRequest, "o documento informado é igual ao atual")
		return
	}
	if direction == "cpf_to_cnpj" && len(curDoc) == 14 {
		httpx.WriteErr(w, http.StatusBadRequest, "sua conta já é um CNPJ")
		return
	}
	if direction == "cnpj_to_cpf" && len(curDoc) == 11 {
		httpx.WriteErr(w, http.StatusBadRequest, "sua conta já é um CPF")
		return
	}

	// Unicidade: o documento novo não pode pertencer a OUTRO usuário.
	var takenBy int64
	_ = h.Pool.QueryRow(r.Context(),
		`SELECT id FROM senderzz_portal_users
		  WHERE regexp_replace(COALESCE(document,''), '\D', '', 'g') = $1 AND id <> $2
		  LIMIT 1`, targetDoc, u.ID).Scan(&takenBy)
	if takenBy > 0 {
		httpx.WriteErr(w, http.StatusConflict, "este documento já está em uso por outra conta")
		return
	}

	// Já existe pedido pendente? (o índice único também garante, mas damos erro claro)
	var pendingID int64
	_ = h.Pool.QueryRow(r.Context(),
		`SELECT id FROM senderzz_document_change_request
		  WHERE user_id = $1 AND status = 'pending' LIMIT 1`, u.ID).Scan(&pendingID)
	if pendingID > 0 {
		httpx.WriteErr(w, http.StatusConflict, "você já tem uma solicitação em análise")
		return
	}

	// Anexo: obrigatório em cpf_to_cnpj (cartão CNPJ), proibido em cnpj_to_cpf.
	var attachmentName string
	if direction == "cpf_to_cnpj" {
		name, ferr := h.saveAttachment(r, u.ID)
		if ferr != nil {
			httpx.WriteErr(w, ferr.code, ferr.msg)
			return
		}
		attachmentName = name
	}

	// Snapshot do estado atual (nome + documento) para trilha/reversão.
	var oldNome, oldDoc string
	_ = h.Pool.QueryRow(r.Context(),
		`SELECT COALESCE(nome,''), COALESCE(document,'') FROM senderzz_portal_users WHERE id = $1`,
		u.ID).Scan(&oldNome, &oldDoc)

	var newID int64
	err := h.Pool.QueryRow(r.Context(),
		`INSERT INTO senderzz_document_change_request
		   (user_id, role, direction, target_nome, target_document,
		    attachment_path, old_nome, old_document, status, created_at)
		 VALUES ($1, $2, $3, $4, $5, NULLIF($6,''), $7, $8, 'pending', NOW())
		 RETURNING id`,
		u.ID, u.Role, direction, targetNome, targetDoc,
		attachmentName, oldNome, onlyDigits(oldDoc)).Scan(&newID)
	if err != nil {
		// Corrida no índice único parcial → 409.
		if strings.Contains(err.Error(), "uq_doc_change_one_pending") {
			if attachmentName != "" {
				_ = os.Remove(filepath.Join(docChangeUploadDir(), attachmentName))
			}
			httpx.WriteErr(w, http.StatusConflict, "você já tem uma solicitação em análise")
			return
		}
		slog.Error("[doc_change] insert falhou", "user_id", u.ID, "err", err)
		if attachmentName != "" {
			_ = os.Remove(filepath.Join(docChangeUploadDir(), attachmentName))
		}
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao registrar solicitação")
		return
	}

	httpx.WriteOK(w, map[string]any{"ok": true, "id": newID, "status": "pending"})
}

// fileErr — erro tipado da gravação do anexo (código HTTP + mensagem).
type fileErr struct {
	code int
	msg  string
}

// saveAttachment lê o campo multipart "attachment", valida por MAGIC BYTES
// (jpeg/png/pdf), grava em docChangeUploadDir com nome não-previsível (crypto/rand)
// e devolve só o NOME do arquivo (não o caminho — o admin resolve o dir).
func (h *DocumentChangeHandler) saveAttachment(r *http.Request, userID int64) (string, *fileErr) {
	file, header, ferr := r.FormFile("attachment")
	if ferr != nil {
		return "", &fileErr{http.StatusBadRequest, "anexo do cartão CNPJ é obrigatório"}
	}
	defer file.Close()

	if header.Size > 16<<20 {
		return "", &fileErr{http.StatusRequestEntityTooLarge, "anexo excede 16MB"}
	}

	sniff := make([]byte, 512)
	n, rerr := io.ReadFull(file, sniff)
	if rerr != nil && rerr != io.ErrUnexpectedEOF && rerr != io.EOF {
		return "", &fileErr{http.StatusBadRequest, "não foi possível ler o anexo: " + rerr.Error()}
	}
	mime := http.DetectContentType(sniff[:n])
	if _, serr := file.Seek(0, io.SeekStart); serr != nil {
		return "", &fileErr{http.StatusInternalServerError, "falha ao reposicionar o arquivo"}
	}

	var ext string
	switch mime {
	case "image/jpeg":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	case "application/pdf":
		ext = ".pdf"
	default:
		return "", &fileErr{http.StatusBadRequest, "o cartão CNPJ precisa ser JPEG, PNG ou PDF (detectado: " + mime + ")"}
	}

	dir := docChangeUploadDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", &fileErr{http.StatusInternalServerError, fmt.Sprintf("mkdir uploads: %v", err)}
	}

	rb := make([]byte, 16)
	if _, err := crand.Read(rb); err != nil {
		return "", &fileErr{http.StatusInternalServerError, "falha ao gerar nome seguro"}
	}
	name := fmt.Sprintf("doc-%d-%s%s", userID, hex.EncodeToString(rb), ext)
	full := filepath.Join(dir, name)

	dst, err := os.Create(full)
	if err != nil {
		return "", &fileErr{http.StatusInternalServerError, fmt.Sprintf("criar arquivo: %v", err)}
	}
	defer dst.Close()
	if _, err := io.Copy(dst, file); err != nil {
		_ = os.Remove(full)
		return "", &fileErr{http.StatusInternalServerError, fmt.Sprintf("gravar arquivo: %v", err)}
	}
	return name, nil
}
