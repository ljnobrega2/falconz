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
	"errors"
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
	return tableExistsCached(ctx, h.Pool, name) // AUDIT-2026-06-18 Onda2 (go-infoschema-cache)
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

// ── Signup (cadastro público auto-serviço) ──────────────────────────────────

type onbSignupReq struct {
	Nome     string `json:"nome"`
	Email    string `json:"email"`
	WhatsApp string `json:"whatsapp"`
	Document string `json:"documento"` // MED44: CPF/CNPJ enviado pelo cadastro público (Login.tsx).
	Senha    string `json:"senha"`
	// REF-PAYOUT 463: referral_code do INDICADOR (link /r/{code}). Opcional — quando
	// vier preenchido, o Signup resolve → portal id e grava em referred_by. Nunca
	// derruba o cadastro se não resolver.
	Ref string `json:"ref"`
}

// Signup — cadastro PÚBLICO. SEM APROVAÇÃO (regra do dono 2026-06-22): cria DIRETO
// um senderzz_portal_users ATIVO com role='cliente' (acesso COD + vitrine, igual
// afiliado; sem produtor/fulfillment) e AUTO-LOGA (retorna token portal). A linha em
// senderzz_onboarding_requests vira só TRILHA/auditoria (status 'approved'), não
// bloqueia mais o acesso. Promoção cliente→afiliado acontece quando um produtor
// aprova a afiliação (affiliates_portal.go). Mantém dedup, bcrypt(12), sanitização
// CPF/CNPJ e resolução de indicação (referred_by).
// POST /onboarding/signup (sem auth, rate-limited).
func (h *OnboardingHandler) Signup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body onbSignupReq
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	body.Nome = strings.TrimSpace(body.Nome)
	body.Email = strings.ToLower(strings.TrimSpace(body.Email))
	body.WhatsApp = strings.TrimSpace(body.WhatsApp)
	// MED44: só dígitos (CPF=11 / CNPJ=14). A contagem é validada no front (Login.tsx);
	// aqui apenas sanitizamos e persistimos — sem checksum p/ não rejeitar CNPJ.
	body.Document = onlyDigits(body.Document)

	if body.Nome == "" {
		httpx.Err(w, 400, "validation", "nome é obrigatório")
		return
	}
	if !emailRegex.MatchString(body.Email) {
		httpx.Err(w, 400, "validation", "e-mail inválido")
		return
	}
	if len(body.Senha) < 8 {
		httpx.Err(w, 400, "validation", "a senha deve ter ao menos 8 caracteres")
		return
	}
	if !h.tableExists(ctx, "senderzz_onboarding_requests") {
		httpx.Err(w, 503, "table_missing", "schema de onboarding ausente")
		return
	}

	// Dedup: e-mail já é usuário ativo, ou já tem solicitação não-rejeitada.
	if h.tableExists(ctx, "senderzz_portal_users") {
		var exists bool
		_ = h.Pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM senderzz_portal_users WHERE LOWER(email)=LOWER($1))`,
			body.Email).Scan(&exists)
		if exists {
			httpx.Err(w, 409, "duplicate", "e-mail já cadastrado")
			return
		}
	}
	var dup int
	_ = h.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM senderzz_onboarding_requests
		 WHERE LOWER(email)=LOWER($1) AND status <> 'rejected'`, body.Email).Scan(&dup)
	if dup > 0 {
		httpx.Err(w, 409, "duplicate", "já existe uma solicitação para esse e-mail")
		return
	}

	tok, err := generateToken()
	if err != nil {
		httpx.Err(w, 500, "rand_error", "falha ao gerar token")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(body.Senha), 12)
	if err != nil {
		httpx.Err(w, 500, "bcrypt_error", err.Error())
		return
	}

	// REF-PAYOUT 463: resolve o indicador (link /r/{code}). Best-effort — se o código
	// não resolver, refArg fica nil e o cadastro segue SEM indicação. NUNCA derruba o
	// signup. referral_code no banco é UPPER (md5 upper, 8 chars) → normalizamos.
	// pwArg-style: refArg = nil quando não há indicação válida.
	var refArg any
	if ref := strings.ToUpper(strings.TrimSpace(body.Ref)); ref != "" && h.tableExists(ctx, "senderzz_portal_users") {
		var referrerID int64
		if errRef := h.Pool.QueryRow(ctx,
			`SELECT id FROM senderzz_portal_users WHERE referral_code = UPPER($1) LIMIT 1`,
			ref).Scan(&referrerID); errRef == nil && referrerID > 0 {
			refArg = referrerID
		}
	}

	// (1) TRILHA/auditoria — registra a solicitação como já 'approved' (auto). NÃO
	// bloqueia acesso; serve só de log. ON CONFLICT mantém idempotência por e-mail.
	_, _ = h.Pool.Exec(ctx,
		`INSERT INTO senderzz_onboarding_requests
		   (nome, email, document, telefone, status, token, password_hash, referred_by, created_at, approved_at)
		 VALUES ($1, $2, NULLIF($3,''), NULLIF($4,''), 'approved', $5, $6, $7, NOW(), NOW())
		 ON CONFLICT (email) DO UPDATE SET
		   nome         = EXCLUDED.nome,
		   document     = EXCLUDED.document,
		   telefone     = EXCLUDED.telefone,
		   status       = 'approved',
		   token        = EXCLUDED.token,
		   password_hash= EXCLUDED.password_hash,
		   referred_by  = EXCLUDED.referred_by,
		   created_at   = NOW(),
		   approved_at  = NOW(),
		   notes        = NULL`,
		body.Nome, body.Email, body.Document, body.WhatsApp, tok, string(hash), refArg)

	// (2) Cria o usuário ATIVO direto como 'cliente'. referred_by = indicador (se houver).
	// Self-referral impossível no signup (o indicador é um usuário PRÉ-EXISTENTE).
	var newID int64
	err = h.Pool.QueryRow(ctx,
		`INSERT INTO senderzz_portal_users
		   (email, nome, role, plano, ativo, password_hash, document, phone, referred_by, created_at)
		 VALUES ($1, $2, 'cliente', 'free', TRUE, $3, NULLIF($4,''), NULLIF($5,''), $6, NOW())
		 RETURNING id`,
		body.Email, body.Nome, string(hash), body.Document, body.WhatsApp, refArg).Scan(&newID)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// (3) AUTO-LOGIN: sessão portal + token (iss=senderzz-portal). Best-effort — se a
	// emissão falhar, a conta já existe e o usuário pode logar normalmente.
	resp := map[string]any{"id": newID, "ok": true, "role": "cliente"}
	if sid, sErr := auth.IssuePortalSession(ctx, h.Pool, newID, r); sErr == nil {
		if jwtTok, tErr := auth.IssuePortalToken(newID, body.Email, "cliente", sid); tErr == nil {
			resp["token"] = jwtTok
			resp["user"] = map[string]any{"id": newID, "email": body.Email, "nome": body.Nome, "role": "cliente"}
		}
	}
	httpx.JSON(w, 201, resp)
}

// ── ResolveReferral (público — valida link /r/{code}) ───────────────────────

// ResolveReferral resolve um referral_code para o NOME do indicador, para a LP /
// landing validar o link /r/{code} e exibir "Indicado por …".
// GET /onboarding/referral/{code} — PÚBLICO, sem auth, rate-limited.
//
// referral_code no banco é UPPER (md5 upper, 8 chars) → normalizamos com UPPER.
// Privacidade/LGPD: devolve SOMENTE {ok, nome}. Nunca e-mail/telefone/PII.
// Degradação graciosa: código inexistente, dono inativo, OU tabela/coluna ausente
// → 200 {ok:false} (a LP segue sem indicação; nunca 404/500 que quebrem a landing).
func (h *OnboardingHandler) ResolveReferral(w http.ResponseWriter, r *http.Request) {
	code := strings.ToUpper(strings.TrimSpace(chi.URLParam(r, "code")))
	if code == "" || !h.tableExists(r.Context(), "senderzz_portal_users") {
		httpx.JSON(w, 200, map[string]any{"ok": false})
		return
	}

	var nome string
	err := h.Pool.QueryRow(r.Context(),
		`SELECT COALESCE(nome,'')
		   FROM senderzz_portal_users
		  WHERE referral_code = UPPER($1)
		    AND ativo = TRUE
		  LIMIT 1`, code).Scan(&nome)
	if err != nil {
		// Sem linha / dono inativo / coluna ausente → resposta neutra (não vaza estado).
		httpx.JSON(w, 200, map[string]any{"ok": false})
		return
	}

	httpx.JSON(w, 200, map[string]any{"ok": true, "nome": nome})
}

// ── helpers de markup / shipping_class ─────────────────────────────────────

// onbGetOption lê value bruto de senderzz_options. Retorna "" se tabela ou chave ausente.
func (h *OnboardingHandler) onbGetOption(ctx context.Context, key string) string {
	if !h.tableExists(ctx, "senderzz_options") {
		return ""
	}
	var raw string
	if err := h.Pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE name=$1`, key).Scan(&raw); err != nil {
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
		`INSERT INTO senderzz_options (name, value)
		 VALUES ($1, $2)
		 ON CONFLICT (name) DO UPDATE SET value = EXCLUDED.value`, key, value)
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
	// slug é NOT NULL + UNIQUE — reqID já é único, evita colisão sem precisar
	// normalizar acentos/espaços do nome livre.
	classSlug := "senderzz-" + strconv.FormatInt(reqID, 10)
	var classID int64
	err := h.Pool.QueryRow(ctx,
		`INSERT INTO senderzz_shipping_classes (slug, name)
		 VALUES ($1, $2)
		 ON CONFLICT DO NOTHING
		 RETURNING id`,
		classSlug, classNome).Scan(&classID)
	if err != nil {
		// Pode ter falhado o ON CONFLICT DO NOTHING (slug já existe): buscar o existente.
		_ = h.Pool.QueryRow(ctx,
			`SELECT id FROM senderzz_shipping_classes WHERE slug=$1`, classSlug).Scan(&classID)
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
	// FEAT-RBAC-2026-06-21: o admin escolhe o nível (RBAC) do usuário ao aprovar.
	// Default 'produtor' quando ausente. Validado em approveOne contra os destinos
	// existentes (admin → admin_users; produtor|afiliado|operator|cliente → portal_users).
	Role string `json:"role"`
}

// onbApproveRoles — roles aceitáveis na aprovação (FEAT-RBAC-2026-06-21).
// 'admin' → senderzz_admin_users. Os demais → senderzz_portal_users (CHECK permite
// apenas produtor|afiliado|operator|cliente). 'motoboy' NÃO tem destino aqui.
var onbApproveRoles = map[string]bool{
	"admin":    true,
	"produtor": true,
	"afiliado": true,
	"operator": true,
	"cliente":  true,
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

	// FEAT-RBAC-2026-06-21: role escolhido pelo admin (default 'produtor').
	// A role vem do body porque é o admin AUTENTICADO (identidade provada pelo
	// Middleware) ATRIBUINDO um nível a OUTRO usuário — operação privilegiada
	// pretendida, não auto-elevação. Validação fail-closed contra destinos reais.
	role := strings.ToLower(strings.TrimSpace(body.Role))
	if role == "" {
		role = "produtor"
	}
	if !onbApproveRoles[role] {
		httpx.Err(w, 400, "validation", "nível inválido (use admin|produtor|afiliado|operator|cliente)")
		return
	}

	ctx := r.Context()
	if !h.tableExists(ctx, "senderzz_onboarding_requests") {
		httpx.Err(w, 503, "table_missing", "tabela senderzz_onboarding_requests ausente")
		return
	}

	res, err := h.approveOne(ctx, id, strings.TrimSpace(body.Notes), role)
	if err != nil {
		switch err {
		case errOnbNotFound:
			httpx.Err(w, 404, "not_found", "solicitação não encontrada")
		case errOnbAlreadyApproved:
			httpx.Err(w, 409, "already_approved", "solicitação já aprovada")
		case errOnbNoPassword:
			httpx.Err(w, 400, "no_password", "solicitação sem senha definida — não é possível criar conta com login")
		default:
			httpx.Err(w, 500, "db_error", err.Error())
		}
		return
	}

	// 4. E-mail NÃO enviado (sem infraestrutura SMTP neste serviço).
	//    email_pending=true indica que o admin deve acionar envio via WP ou manualmente.
	httpx.JSON(w, 200, map[string]any{
		"ok":             true,
		"role":           role, // FEAT-RBAC-2026-06-21
		"portal_user_id": res.portalUserID,
		"admin_user_id":  res.adminUserID,
		"class_id":       res.classID,
		"id":             id,
		"email_pending":  true,
	})
}

// Sentinelas de aprovação — permitem que o caller (HTTP singular ou lote) mapeie
// o motivo do erro para o status correto sem inspecionar strings.
var (
	errOnbNotFound        = errors.New("solicitação não encontrada")
	errOnbAlreadyApproved = errors.New("solicitação já aprovada")
	// FEAT-RBAC-2026-06-21: role=admin exige password_hash na solicitação
	// (admin_users.password_hash é NOT NULL). Sem senha → não dá pra criar admin logável.
	errOnbNoPassword = errors.New("solicitação sem senha — não é possível criar conta com login")
)

// onbApproveResult — saída do core de aprovação (ids criados).
type onbApproveResult struct {
	portalUserID int64
	adminUserID  int64 // FEAT-RBAC-2026-06-21: preenchido quando role=admin.
	classID      int64
}

// approveCore — versão "fire and forget" do core de aprovação para uso em lote.
// Descarta os ids criados; propaga apenas o erro (errOnb* ou técnico).
// Lote sempre aprova como 'produtor' (default) — o destino RBAC granular é via HTTP singular.
func (h *OnboardingHandler) approveCore(ctx context.Context, id int64, notes string) error {
	_, err := h.approveOne(ctx, id, notes, "produtor")
	return err
}

// approveOne — CORE da aprovação de onboarding, compartilhado entre a handler HTTP
// singular (Approve) e o lote (BulkQueuesHandler.OnboardingApprove). Replica os
// efeitos colaterais de sz_onboarding_approve: shipping class + portal_user + markup.
// Idempotência: já-aprovada devolve errOnbAlreadyApproved; ausente, errOnbNotFound.
// FEAT-RBAC-2026-06-21: assinatura ganha `role` (nível escolhido pelo admin).
// role=admin → cria/ativa em senderzz_admin_users (exige password_hash na solicitação).
// demais (produtor|afiliado|operator|cliente) → senderzz_portal_users, gravando o
// password_hash salvo na solicitação (do /signup) p/ o usuário conseguir logar.
func (h *OnboardingHandler) approveOne(ctx context.Context, id int64, notes, role string) (onbApproveResult, error) {
	var res onbApproveResult

	if role == "" {
		role = "produtor"
	}

	// Buscar a request (inclui password_hash — necessário p/ criar conta logável).
	var req onboardingRequest
	var pwHash string
	err := h.Pool.QueryRow(ctx,
		`SELECT id, nome, email, document, telefone, empresa, status, token,
		        created_at::text, approved_at::text, notes, COALESCE(password_hash,'')
		 FROM senderzz_onboarding_requests WHERE id=$1`, id).
		Scan(&req.ID, &req.Nome, &req.Email, &req.Document, &req.Telefone,
			&req.Empresa, &req.Status, &req.Token, &req.CreatedAt, &req.ApprovedAt, &req.Notes, &pwHash)
	if err != nil {
		if err == pgx.ErrNoRows {
			return res, errOnbNotFound
		}
		return res, err
	}
	if req.Status == "approved" {
		return res, errOnbAlreadyApproved
	}

	// role=admin exige senha (admin_users.password_hash é NOT NULL).
	if role == "admin" && pwHash == "" {
		return res, errOnbNoPassword
	}

	// ── Destino ADMIN ─────────────────────────────────────────────────────────
	if role == "admin" {
		return h.approveAsAdmin(ctx, id, req, pwHash, notes)
	}

	// REF-PAYOUT 463: indicador capturado no signup (best-effort, FORA do SELECT
	// principal — se a coluna/migração 463 não existir, o erro é engolido e
	// referredBy fica nil → a aprovação segue normalmente, sem quebrar).
	var referredBy *int64
	_ = h.Pool.QueryRow(ctx,
		`SELECT referred_by FROM senderzz_onboarding_requests WHERE id=$1`, id).Scan(&referredBy)

	// ── Destino PORTAL (produtor|afiliado|operator|cliente) ───────────────────
	// 1. Criar shipping class FORA da transação (sem FK para portal_user).
	// Degradação graciosa: retorna 0 se senderzz_shipping_classes não existir.
	classID := h.onbCreateShippingClass(ctx, req.Nome, req.ID)
	res.classID = classID

	// Transação: cria portal_user + marca approved.
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		return res, err
	}
	defer tx.Rollback(ctx)

	// 2. Criar portal_user com o role escolhido + password_hash da solicitação.
	// pwArg = NULL quando não há senha (conta WP-only); COALESCE no conflito não
	// sobrescreve uma senha já existente com NULL.
	var pwArg any
	if pwHash == "" {
		pwArg = nil
	} else {
		pwArg = pwHash
	}
	var portalUserID int64
	if h.tableExists(ctx, "senderzz_portal_users") {
		if classID > 0 {
			err = tx.QueryRow(ctx,
				`INSERT INTO senderzz_portal_users (email, nome, role, plano, ativo, shipping_class_id, password_hash, created_at)
				 VALUES ($1, $2, $3, 'free', TRUE, $4, $5, NOW())
				 ON CONFLICT (email) DO UPDATE SET nome=EXCLUDED.nome, role=EXCLUDED.role, ativo=TRUE,
				     shipping_class_id=EXCLUDED.shipping_class_id,
				     password_hash=COALESCE(EXCLUDED.password_hash, senderzz_portal_users.password_hash)
				 RETURNING id`,
				req.Email, req.Nome, role, classID, pwArg).Scan(&portalUserID)
		} else {
			err = tx.QueryRow(ctx,
				`INSERT INTO senderzz_portal_users (email, nome, role, plano, ativo, password_hash, created_at)
				 VALUES ($1, $2, $3, 'free', TRUE, $4, NOW())
				 ON CONFLICT (email) DO UPDATE SET nome=EXCLUDED.nome, role=EXCLUDED.role, ativo=TRUE,
				     password_hash=COALESCE(EXCLUDED.password_hash, senderzz_portal_users.password_hash)
				 RETURNING id`,
				req.Email, req.Nome, role, pwArg).Scan(&portalUserID)
		}
		if err != nil {
			return res, err
		}
	}
	res.portalUserID = portalUserID

	var notesArg any
	if notes == "" {
		notesArg = nil
	} else {
		notesArg = notes
	}
	if _, err = tx.Exec(ctx,
		`UPDATE senderzz_onboarding_requests
		 SET status='approved', approved_at=NOW(), notes=COALESCE($2, notes)
		 WHERE id=$1`, id, notesArg); err != nil {
		return res, err
	}

	if err := tx.Commit(ctx); err != nil {
		return res, err
	}

	// 3. Aplicar markup padrão para a classe (fora da tx — é um UPSERT tolerante a falha).
	h.onbApplyMarkupDefault(ctx, classID)

	// 4. REF-PAYOUT 463: propaga o indicador (referred_by) para o portal_user recém-criado.
	// FORA da tx e best-effort — uma falha aqui NUNCA desfaz a criação do usuário (espelha
	// onbApplyMarkupDefault). IMUTÁVEL: só seta se ainda NULL (referred_by IS NULL).
	// SELF-REFERRAL barrado por $1 <> $2. Coluna/migração ausente → erro engolido.
	if referredBy != nil && *referredBy > 0 && portalUserID > 0 {
		_, _ = h.Pool.Exec(ctx,
			`UPDATE senderzz_portal_users
			    SET referred_by = $1
			  WHERE id = $2 AND referred_by IS NULL AND $1 <> $2`,
			*referredBy, portalUserID)
	}

	return res, nil
}

// approveAsAdmin — FEAT-RBAC-2026-06-21. Cria/ativa um admin em senderzz_admin_users
// usando o password_hash salvo na solicitação (bcrypt do /signup), e marca a request
// como approved. admin_users tem UNIQUE(email) → ON CONFLICT (email) é seguro.
// Não cria portal_user nem shipping class (admin não é produtor).
func (h *OnboardingHandler) approveAsAdmin(ctx context.Context, id int64, req onboardingRequest, pwHash, notes string) (onbApproveResult, error) {
	var res onbApproveResult

	if !h.tableExists(ctx, "senderzz_admin_users") {
		return res, errors.New("tabela senderzz_admin_users ausente")
	}

	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		return res, err
	}
	defer tx.Rollback(ctx)

	var adminID int64
	err = tx.QueryRow(ctx,
		`INSERT INTO senderzz_admin_users (nome, email, password_hash, role, ativo, created_at)
		 VALUES ($1, $2, $3, 'admin', TRUE, NOW())
		 ON CONFLICT (email) DO UPDATE SET nome=EXCLUDED.nome, password_hash=EXCLUDED.password_hash, ativo=TRUE
		 RETURNING id`,
		req.Nome, req.Email, pwHash).Scan(&adminID)
	if err != nil {
		return res, err
	}
	res.adminUserID = adminID

	var notesArg any
	if notes == "" {
		notesArg = nil
	} else {
		notesArg = notes
	}
	if _, err = tx.Exec(ctx,
		`UPDATE senderzz_onboarding_requests
		 SET status='approved', approved_at=NOW(), notes=COALESCE($2, notes)
		 WHERE id=$1`, id, notesArg); err != nil {
		return res, err
	}

	if err := tx.Commit(ctx); err != nil {
		return res, err
	}
	return res, nil
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

	// AUDIT-DEEP-2026-06-18 P2: integridade de cadastro. A trava acima só conta
	// admins ATIVOS — um registro com o MESMO e-mail mas inativo (ou criado numa
	// corrida) não seria pego, e o INSERT abaixo duplicaria o e-mail. Checagem
	// explícita de duplicado (case-insensitive) antes do INSERT → 409 claro.
	// (O UNIQUE em email é a defesa definitiva, mas exige migração de schema +
	//  varredura de duplicatas pré-existentes; fica como follow-up.)
	var emailExists bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM senderzz_admin_users WHERE LOWER(email)=LOWER($1))`,
		body.Email).Scan(&emailExists)
	if emailExists {
		httpx.Err(w, 409, "duplicate", "e-mail já cadastrado em senderzz_admin_users")
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
