// Package handlers — endpoint admin para Onboarding (cadastro de produtores).
//
// Paridade com `includes/senderzz-onboarding.php` (shortcode + aba admin)
// e com o wizard de setup inicial descrito em AUDIT-ADMIN-WP.md §11.
//
// Tabela principal: senderzz_onboarding_requests
//   id, nome, email, document, telefone, empresa, status (pending/approved/rejected),
//   token, created_at, approved_at, notes.
//
// Setup inicial:
//   - admin é INSERIDO em senderzz_admin_users (mesma tabela usada por auth/login)
//     porque o middleware JWT (internal/auth/auth.go) procura admins APENAS lá.
//   - portal_users (clientes) NÃO tem password_hash no admin Go — só o fluxo
//     "approve request" cria portal_user, sem precisar de senha (o usuário
//     vai recuperá-la pelo fluxo de e-mail do portal).
package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/auth"
	"github.com/senderzz/admin-service/internal/httpx"
	"golang.org/x/crypto/bcrypt"
)

type OnboardingHandler struct{ Pool *pgxpool.Pool }

// onboardingRequest é o registro espelhado da tabela.
type onboardingRequest struct {
	ID         int64   `json:"id"`
	Nome       string  `json:"nome"`
	Email      string  `json:"email"`
	Document   *string `json:"document"`
	Telefone   *string `json:"telefone"`
	Empresa    *string `json:"empresa"`
	Status     string  `json:"status"`
	Token      string  `json:"token"`
	CreatedAt  string  `json:"created_at"`
	ApprovedAt *string `json:"approved_at"`
	Notes      *string `json:"notes"`
}

func (h *OnboardingHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// ── Helpers ─────────────────────────────────────────────────────────────────

// onlyDigits remove tudo que não é dígito (espelha sz_onboarding_digits).
func onlyDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// validateCPF aplica o algoritmo brasileiro mod-11 com dígitos verificadores.
// Espelha sz_onboarding_validate_cpf (includes/senderzz-onboarding.php:26).
// NÃO é Luhn — é mod-11 com pesos decrescentes.
func validateCPF(cpf string) bool {
	cpf = onlyDigits(cpf)
	if len(cpf) != 11 {
		return false
	}
	// Rejeita sequências repetidas (000…000, 111…111, etc.).
	allSame := true
	for i := 1; i < 11; i++ {
		if cpf[i] != cpf[0] {
			allSame = false
			break
		}
	}
	if allSame {
		return false
	}
	// Calcula os 2 dígitos verificadores.
	for t := 9; t < 11; t++ {
		sum := 0
		for i := 0; i < t; i++ {
			d := int(cpf[i] - '0')
			sum += d * ((t + 1) - i)
		}
		digit := ((10 * sum) % 11) % 10
		if int(cpf[t]-'0') != digit {
			return false
		}
	}
	return true
}

// formatCPF retorna no formato XXX.XXX.XXX-XX (espelha sz_onboarding_format_cpf).
func formatCPF(cpf string) string {
	d := onlyDigits(cpf)
	if len(d) != 11 {
		return d
	}
	return d[0:3] + "." + d[3:6] + "." + d[6:9] + "-" + d[9:11]
}

// emailRegex — validação básica RFC-ish (mesmo padrão usado em outros lugares do admin).
var emailRegex = regexp.MustCompile(`^[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}$`)

// generateToken — 24 bytes random → 48 chars hex (crypto/rand).
func generateToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ── List / Get ──────────────────────────────────────────────────────────────

// List retorna requests filtrados por status.
// GET /onboarding/requests?status=pending|approved|rejected&limit=200
func (h *OnboardingHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	status := strings.ToLower(strings.TrimSpace(q.Get("status")))

	if !h.tableExists(ctx, "senderzz_onboarding_requests") {
		httpx.JSON(w, 200, map[string]any{"items": []onboardingRequest{}, "count": 0})
		return
	}

	var rows pgx.Rows
	var err error
	base := `SELECT id, nome, email, document, telefone, empresa, status, token,
	                created_at::text, approved_at::text, notes
	         FROM senderzz_onboarding_requests`
	if status != "" {
		rows, err = h.Pool.Query(ctx,
			base+` WHERE status=$1 ORDER BY created_at DESC LIMIT $2`, status, limit)
	} else {
		rows, err = h.Pool.Query(ctx,
			base+` ORDER BY created_at DESC LIMIT $1`, limit)
	}
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []onboardingRequest{}
	for rows.Next() {
		var x onboardingRequest
		if err := rows.Scan(&x.ID, &x.Nome, &x.Email, &x.Document, &x.Telefone,
			&x.Empresa, &x.Status, &x.Token, &x.CreatedAt, &x.ApprovedAt, &x.Notes); err != nil {
			httpx.Err(w, 500, "scan_error", err.Error())
			return
		}
		out = append(out, x)
	}
	httpx.JSON(w, 200, map[string]any{"items": out, "count": len(out)})
}

// Get retorna o detalhe de uma request.
// GET /onboarding/requests/{id}
func (h *OnboardingHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	if !h.tableExists(r.Context(), "senderzz_onboarding_requests") {
		httpx.Err(w, 503, "table_missing", "tabela senderzz_onboarding_requests ausente")
		return
	}
	var x onboardingRequest
	err = h.Pool.QueryRow(r.Context(),
		`SELECT id, nome, email, document, telefone, empresa, status, token,
		        created_at::text, approved_at::text, notes
		 FROM senderzz_onboarding_requests WHERE id=$1`, id).
		Scan(&x.ID, &x.Nome, &x.Email, &x.Document, &x.Telefone,
			&x.Empresa, &x.Status, &x.Token, &x.CreatedAt, &x.ApprovedAt, &x.Notes)
	if err != nil {
		httpx.Err(w, 404, "not_found", "solicitação não encontrada")
		return
	}
	httpx.JSON(w, 200, x)
}

// ── Create ──────────────────────────────────────────────────────────────────

type onbCreateReq struct {
	Nome     string `json:"nome"`
	Email    string `json:"email"`
	Document string `json:"document"`
	Telefone string `json:"telefone"`
	Empresa  string `json:"empresa"`
}

// Create insere nova solicitação (admin cria diretamente, bypass do shortcode).
// POST /onboarding/requests
func (h *OnboardingHandler) Create(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body onbCreateReq
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}

	body.Nome = strings.TrimSpace(body.Nome)
	body.Email = strings.ToLower(strings.TrimSpace(body.Email))
	body.Document = onlyDigits(body.Document)
	body.Telefone = strings.TrimSpace(body.Telefone)
	body.Empresa = strings.TrimSpace(body.Empresa)

	// ── Validação ──────────────────────────────────────────────────────────
	if body.Nome == "" {
		httpx.Err(w, 400, "validation", "nome é obrigatório")
		return
	}
	if !emailRegex.MatchString(body.Email) {
		httpx.Err(w, 400, "validation", "e-mail inválido")
		return
	}
	if body.Document != "" && !validateCPF(body.Document) {
		httpx.Err(w, 400, "validation", "CPF inválido")
		return
	}

	if !h.tableExists(ctx, "senderzz_onboarding_requests") {
		httpx.Err(w, 503, "table_missing", "tabela senderzz_onboarding_requests ausente; aplicar schema antes")
		return
	}

	// ── Dedup ──────────────────────────────────────────────────────────────
	// E-mail já existe em portal_users → conflito definitivo.
	if h.tableExists(ctx, "senderzz_portal_users") {
		var exists bool
		_ = h.Pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM senderzz_portal_users WHERE LOWER(email)=LOWER($1))`,
			body.Email).Scan(&exists)
		if exists {
			httpx.Err(w, 409, "duplicate", "e-mail já cadastrado em portal_users")
			return
		}
	}
	// Já existe request não-rejeitada para esse e-mail.
	var dupCount int
	_ = h.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM senderzz_onboarding_requests
		 WHERE LOWER(email)=LOWER($1) AND status <> 'rejected'`, body.Email).Scan(&dupCount)
	if dupCount > 0 {
		httpx.Err(w, 409, "duplicate", "solicitação pendente/aprovada já existe para esse e-mail")
		return
	}

	tok, err := generateToken()
	if err != nil {
		httpx.Err(w, 500, "rand_error", "falha ao gerar token")
		return
	}

	// Como a tabela tem UNIQUE KEY em email (incluindo rejected), se existir
	// um registro 'rejected' anterior, atualizamos em vez de inserir.
	var newID int64
	err = h.Pool.QueryRow(ctx,
		`INSERT INTO senderzz_onboarding_requests
		   (nome, email, document, telefone, empresa, status, token, created_at)
		 VALUES ($1, $2, NULLIF($3,''), NULLIF($4,''), NULLIF($5,''), 'pending', $6, NOW())
		 ON CONFLICT (email) DO UPDATE SET
		   nome       = EXCLUDED.nome,
		   document   = EXCLUDED.document,
		   telefone   = EXCLUDED.telefone,
		   empresa    = EXCLUDED.empresa,
		   status     = 'pending',
		   token      = EXCLUDED.token,
		   created_at = NOW(),
		   approved_at = NULL,
		   notes      = NULL
		 RETURNING id`,
		body.Nome, body.Email, body.Document, body.Telefone, body.Empresa, tok).Scan(&newID)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 201, map[string]any{"id": newID, "token": tok, "ok": true})
}

// ── helpers de markup / shipping_class ─────────────────────────────────────

// onbGetOption lê value bruto de senderzz_options. Retorna "" se tabela ou chave ausente.
func (h *OnboardingHandler) onbGetOption(ctx context.Context, key string) string {
	if !h.tableExists(ctx, "senderzz_options") {
		return ""
	}
	var raw string
	if err := h.Pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE "key"=$1`, key).Scan(&raw); err != nil {
		return ""
	}
	return raw
}

// onbUpsertOption persiste um par key/value em senderzz_options.
// Sem efeito se tabela ausente (degradação graciosa).
func (h *OnboardingHandler) onbUpsertOption(ctx context.Context, key, value string) {
	if !h.tableExists(ctx, "senderzz_options") {
		return
	}
	_, _ = h.Pool.Exec(ctx,
		`INSERT INTO senderzz_options ("key", value)
		 VALUES ($1, $2)
		 ON CONFLICT ("key") DO UPDATE SET value = EXCLUDED.value`, key, value)
}

// onbCreateShippingClass insere uma classe em senderzz_shipping_classes e retorna o ID.
// Retorna 0 se a tabela não existir (degradação graciosa — TODO original da linha 368).
// A tabela é populada por sync WP→Go; o INSERT aqui espelha wp_insert_term('product_shipping_class').
func (h *OnboardingHandler) onbCreateShippingClass(ctx context.Context, nome string, reqID int64) int64 {
	if !h.tableExists(ctx, "senderzz_shipping_classes") {
		return 0
	}
	// nome exposto na UI: "Nome Produtor (Senderzz #N)" — mesma convenção do PHP.
	classNome := nome + " (Senderzz #" + strconv.FormatInt(reqID, 10) + ")"
	var classID int64
	err := h.Pool.QueryRow(ctx,
		`INSERT INTO senderzz_shipping_classes (name)
		 VALUES ($1)
		 ON CONFLICT DO NOTHING
		 RETURNING id`,
		classNome).Scan(&classID)
	if err != nil {
		// Pode ter falhado o ON CONFLICT DO NOTHING (nome já existe): buscar o existente.
		_ = h.Pool.QueryRow(ctx,
			`SELECT id FROM senderzz_shipping_classes WHERE name=$1`, classNome).Scan(&classID)
	}
	return classID
}

// onbApplyMarkupDefault aplica o markup padrão (senderzz_markup_default) para a classe
// recém-criada, apenas se ainda não houver regra específica para ela.
// Espelha o bloco "4. Aplicar markup padrão global" de senderzz-onboarding.php:281-293.
func (h *OnboardingHandler) onbApplyMarkupDefault(ctx context.Context, classID int64) {
	if classID <= 0 || !h.tableExists(ctx, "senderzz_options") {
		return
	}
	// Ler regras atuais como map[string]any para preservar entradas com valores string
	// ("20" em vez de 20) gravadas pelo PHP legado — evita corrupção de outras classes.
	rulesRaw := strings.TrimSpace(h.onbGetOption(ctx, "senderzz_markup_rules"))
	rules := map[string]any{}
	if rulesRaw != "" {
		_ = json.Unmarshal([]byte(rulesRaw), &rules)
	}
	classKey := strconv.FormatInt(classID, 10)
	if _, exists := rules[classKey]; exists {
		// Já tem regra específica — não sobrescrever.
		return
	}
	// Ler default (senderzz_markup_default), usando mesmos fallbacks de expedicao_integracoes.go.
	pct := 20.0
	fixed := 3.99
	defaultRaw := strings.TrimSpace(h.onbGetOption(ctx, "senderzz_markup_default"))
	if defaultRaw != "" {
		var d struct {
			Pct   *float64 `json:"pct"`
			Fixed *float64 `json:"fixed"`
		}
		if err := json.Unmarshal([]byte(defaultRaw), &d); err == nil {
			if d.Pct != nil {
				pct = *d.Pct
			}
			if d.Fixed != nil {
				fixed = *d.Fixed
			}
		}
	}
	rules[classKey] = map[string]float64{"pct": pct, "fixed": fixed}
	updated, err := json.Marshal(rules)
	if err != nil {
		return
	}
	h.onbUpsertOption(ctx, "senderzz_markup_rules", string(updated))
}

// ── Approve ─────────────────────────────────────────────────────────────────

type onbNotesReq struct {
	Notes string `json:"notes"`
}

// Approve marca request como aprovada e cria portal_user.
// POST /onboarding/requests/{id}/approve
//
// Efeitos colaterais espelhados de sz_onboarding_approve (senderzz-onboarding.php):
//  1. Cria portal_user em senderzz_portal_users (com shipping_class_id quando disponível).
//  2. Cria shipping class em senderzz_shipping_classes (degradação graciosa se tabela ausente).
//  3. Aplica markup padrão (senderzz_markup_rules) para a classe recém-criada.
//  4. E-mail de boas-vindas com senha temporária NÃO é enviado — infraestrutura SMTP
//     não existe neste serviço Go. O campo "email_pending" é retornado como true para
//     que o cliente saiba que o e-mail precisa ser acionado manualmente ou via WP.
func (h *OnboardingHandler) Approve(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	var body onbNotesReq
	_ = httpx.DecodeJSON(r, &body)

	ctx := r.Context()
	if !h.tableExists(ctx, "senderzz_onboarding_requests") {
		httpx.Err(w, 503, "table_missing", "tabela senderzz_onboarding_requests ausente")
		return
	}

	// Buscar a request.
	var req onboardingRequest
	err = h.Pool.QueryRow(ctx,
		`SELECT id, nome, email, document, telefone, empresa, status, token,
		        created_at::text, approved_at::text, notes
		 FROM senderzz_onboarding_requests WHERE id=$1`, id).
		Scan(&req.ID, &req.Nome, &req.Email, &req.Document, &req.Telefone,
			&req.Empresa, &req.Status, &req.Token, &req.CreatedAt, &req.ApprovedAt, &req.Notes)
	if err != nil {
		httpx.Err(w, 404, "not_found", "solicitação não encontrada")
		return
	}
	if req.Status == "approved" {
		httpx.Err(w, 409, "already_approved", "solicitação já aprovada")
		return
	}

	// 1. Criar shipping class FORA da transação (sem FK para portal_user).
	// Degradação graciosa: retorna 0 se senderzz_shipping_classes não existir.
	classID := h.onbCreateShippingClass(ctx, req.Nome, req.ID)

	// Transação: cria portal_user + marca approved.
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.Err(w, 500, "tx_error", err.Error())
		return
	}
	defer tx.Rollback(ctx)

	// 2. Criar portal_user com shipping_class_id quando disponível.
	var portalUserID int64
	if h.tableExists(ctx, "senderzz_portal_users") {
		if classID > 0 {
			err = tx.QueryRow(ctx,
				`INSERT INTO senderzz_portal_users (email, nome, role, plano, ativo, shipping_class_id, created_at)
				 VALUES ($1, $2, 'produtor', 'free', TRUE, $3, NOW())
				 ON CONFLICT (email) DO UPDATE SET nome=EXCLUDED.nome, ativo=TRUE, shipping_class_id=EXCLUDED.shipping_class_id
				 RETURNING id`,
				req.Email, req.Nome, classID).Scan(&portalUserID)
		} else {
			err = tx.QueryRow(ctx,
				`INSERT INTO senderzz_portal_users (email, nome, role, plano, ativo, created_at)
				 VALUES ($1, $2, 'produtor', 'free', TRUE, NOW())
				 ON CONFLICT (email) DO UPDATE SET nome=EXCLUDED.nome, ativo=TRUE
				 RETURNING id`,
				req.Email, req.Nome).Scan(&portalUserID)
		}
		if err != nil {
			httpx.Err(w, 500, "db_error_portal_user", err.Error())
			return
		}
	}

	notes := strings.TrimSpace(body.Notes)
	var notesArg any
	if notes == "" {
		notesArg = nil
	} else {
		notesArg = notes
	}
	_, err = tx.Exec(ctx,
		`UPDATE senderzz_onboarding_requests
		 SET status='approved', approved_at=NOW(), notes=COALESCE($2, notes)
		 WHERE id=$1`, id, notesArg)
	if err != nil {
		httpx.Err(w, 500, "db_error_update", err.Error())
		return
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.Err(w, 500, "tx_commit", err.Error())
		return
	}

	// 3. Aplicar markup padrão para a classe (fora da tx — é um UPSERT tolerante a falha).
	h.onbApplyMarkupDefault(ctx, classID)

	// 4. E-mail NÃO enviado (sem infraestrutura SMTP neste serviço).
	//    email_pending=true indica que o admin deve acionar envio via WP ou manualmente.
	httpx.JSON(w, 200, map[string]any{
		"ok":             true,
		"portal_user_id": portalUserID,
		"class_id":       classID,
		"id":             id,
		"email_pending":  true,
	})
}

// Reject marca request como rejeitada.
// POST /onboarding/requests/{id}/reject
func (h *OnboardingHandler) Reject(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	var body onbNotesReq
	_ = httpx.DecodeJSON(r, &body)

	if !h.tableExists(r.Context(), "senderzz_onboarding_requests") {
		httpx.Err(w, 503, "table_missing", "tabela senderzz_onboarding_requests ausente")
		return
	}

	notes := strings.TrimSpace(body.Notes)
	if notes == "" {
		httpx.Err(w, 400, "validation", "motivo (notes) é obrigatório para rejeição")
		return
	}

	tag, err := h.Pool.Exec(r.Context(),
		`UPDATE senderzz_onboarding_requests
		 SET status='rejected', notes=$2
		 WHERE id=$1 AND status='pending'`, id, notes)
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

// ── Setup wizard ────────────────────────────────────────────────────────────

type setupStatusResp struct {
	AdminUsersCount         int64    `json:"admin_users_count"`
	METoken                 bool     `json:"me_token_configured"`
	WebhookSecret           bool     `json:"webhook_secret_configured"`
	JWTSecret               bool     `json:"jwt_secret_configured"`
	SchemasApplied          []string `json:"schemas_applied"`
	Ready                   bool     `json:"ready"`
	PendingSteps            []string `json:"pending_steps"`
}

// SetupStatus checa se a instalação inicial está completa.
// GET /onboarding/setup-status
func (h *OnboardingHandler) SetupStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := setupStatusResp{
		SchemasApplied: []string{},
		PendingSteps:   []string{},
	}

	// Conta admins. Se a tabela não existir, count=0 (precisa aplicar schema).
	if h.tableExists(ctx, "senderzz_admin_users") {
		_ = h.Pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM senderzz_admin_users WHERE ativo=TRUE`).Scan(&out.AdminUsersCount)
	}

	// Secrets via env vars.
	out.METoken = strings.TrimSpace(getenv("SENDERZZ_ME_TOKEN", "TPC_ME_TOKEN", "ME_TOKEN")) != ""
	out.WebhookSecret = strings.TrimSpace(getenv("MOTOBOY_INTERNAL_SECRET", "TPC_WEBHOOK_SECRET", "WP_SALT_AUTH")) != ""
	out.JWTSecret = strings.TrimSpace(getenv("ADMIN_JWT_SECRET", "JWT_SECRET", "TPC_JWT_SECRET")) != ""

	// Schemas: agrupados por subsistema.
	if h.tableExists(ctx, "sz_motoboy_pedidos") {
		out.SchemasApplied = append(out.SchemasApplied, "motoboy")
	}
	if h.tableExists(ctx, "tpc_carteira") {
		out.SchemasApplied = append(out.SchemasApplied, "wallet")
	}
	if h.tableExists(ctx, "senderzz_affiliates") {
		out.SchemasApplied = append(out.SchemasApplied, "affiliates")
	}
	if h.tableExists(ctx, "senderzz_portal_users") {
		out.SchemasApplied = append(out.SchemasApplied, "portal_users")
	}
	if h.tableExists(ctx, "senderzz_onboarding_requests") {
		out.SchemasApplied = append(out.SchemasApplied, "onboarding")
	}

	// Pending steps + ready.
	if out.AdminUsersCount == 0 {
		out.PendingSteps = append(out.PendingSteps, "create_first_admin")
	}
	if !out.METoken {
		out.PendingSteps = append(out.PendingSteps, "configure_me_token")
	}
	if !out.WebhookSecret {
		out.PendingSteps = append(out.PendingSteps, "configure_webhook_secret")
	}
	if !out.JWTSecret {
		out.PendingSteps = append(out.PendingSteps, "configure_jwt_secret")
	}
	if len(out.SchemasApplied) == 0 {
		out.PendingSteps = append(out.PendingSteps, "apply_schemas")
	}
	out.Ready = len(out.PendingSteps) == 0

	httpx.JSON(w, 200, out)
}

// getenv retorna o primeiro env var não-vazio entre os nomes fornecidos.
func getenv(names ...string) string {
	for _, n := range names {
		if v := strings.TrimSpace(os.Getenv(n)); v != "" {
			return v
		}
	}
	return ""
}

// ── CreateAdmin (setup inicial) ─────────────────────────────────────────────

type onbCreateAdminReq struct {
	Nome  string `json:"nome"`
	Email string `json:"email"`
	Senha string `json:"senha"`
}

// CreateAdmin cria o primeiro super_admin. Rejeita se já existir admin ativo.
// POST /onboarding/setup/create-admin
func (h *OnboardingHandler) CreateAdmin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body onbCreateAdminReq
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	body.Nome = strings.TrimSpace(body.Nome)
	body.Email = strings.ToLower(strings.TrimSpace(body.Email))

	if body.Nome == "" {
		httpx.Err(w, 400, "validation", "nome é obrigatório")
		return
	}
	if !emailRegex.MatchString(body.Email) {
		httpx.Err(w, 400, "validation", "e-mail inválido")
		return
	}
	if len(body.Senha) < 8 {
		httpx.Err(w, 400, "validation", "senha deve ter ao menos 8 caracteres")
		return
	}

	if !h.tableExists(ctx, "senderzz_admin_users") {
		httpx.Err(w, 503, "table_missing", "tabela senderzz_admin_users ausente; aplicar schema antes")
		return
	}

	// Trava: setup só roda quando não há admin ativo.
	var count int64
	_ = h.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM senderzz_admin_users WHERE ativo=TRUE`).Scan(&count)
	if count > 0 {
		httpx.Err(w, 409, "already_setup", "sistema já possui admin ativo; setup foi finalizado")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(body.Senha), 12)
	if err != nil {
		httpx.Err(w, 500, "bcrypt_error", err.Error())
		return
	}

	var newID int64
	err = h.Pool.QueryRow(ctx,
		`INSERT INTO senderzz_admin_users (nome, email, password_hash, role, ativo, created_at)
		 VALUES ($1, $2, $3, 'super_admin', TRUE, NOW())
		 RETURNING id`,
		body.Nome, body.Email, string(hash)).Scan(&newID)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// Emite JWT direto para o novo admin (mesma chave usada por auth.Middleware).
	adminClaims := auth.Admin{ID: newID, Email: body.Email, Nome: body.Nome}
	tok, err := auth.IssueToken(adminClaims)
	if err != nil {
		// Mesmo sem token o admin foi criado — devolve sucesso parcial.
		httpx.JSON(w, 201, map[string]any{
			"ok":        true,
			"id":        newID,
			"token":     "",
			"token_err": err.Error(),
		})
		return
	}

	httpx.JSON(w, 201, map[string]any{
		"ok":    true,
		"id":    newID,
		"token": tok,
		"admin": adminClaims,
	})
}
