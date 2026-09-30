// Package handlers — recuperação de senha do admin (forgot / reset).
//
// AUDIT-2026-06-22 forgot-pw.
//
// Fluxo em dois passos, ambos PÚBLICOS (sem JWT) e rate-limited por IP:
//
//  1. POST /onboarding/forgot {email}
//     - SEMPRE responde 200 genérico (anti-enumeração): nunca revela se o e-mail
//       existe, é admin, ou se o envio falhou. O único não-200 é JSON inválido.
//     - Se o e-mail pertence a um admin ATIVO em senderzz_admin_users: gera um token
//       de 32 bytes (crypto/rand → 64 hex), grava SHA-256(token) em
//       senderzz_admin_password_resets com expires_at = now()+30min, e envia o link
//       {APP_BASE_URL}/reset?token=RAW por e-mail (best-effort; falha é só logada).
//     - Em APP_ENV=development o reset_url é devolvido no JSON (dev_reset_url) para
//       teste sem SMTP. Em produção a chave NUNCA aparece.
//
//  2. POST /onboarding/reset {token, new_password}
//     - Valida SHA-256(token) não-usado e não-expirado, consome o token (single-use
//       atômico) e grava a nova senha em senderzz_admin_users com bcrypt cost-12 —
//       EXATAMENTE o mesmo esquema que /login valida (auth.go:51
//       bcrypt.CompareHashAndPassword) e que CreateAdmin/Signup gravam. Fail-closed.
//
// O hash do token é SHA-256 puro (não HMAC): o token tem 256 bits de entropia
// crypto/rand, então HMAC não agregaria segurança. A regra única é write==read —
// forgot grava sha256(token) e reset busca por sha256(token). (O comentário da
// migração 450 menciona HMAC, mas a tabela não é lida por nenhum outro código.)
package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/senderzz/admin-service/internal/email"
	"github.com/senderzz/admin-service/internal/httpx"
	"golang.org/x/crypto/bcrypt"
)

// pwResetIsDev — APP_ENV=development (case-insensitive). email.isDev é unexportado,
// então replicamos aqui para gatear o dev_reset_url.
func pwResetIsDev() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "development")
}

// pwResetGenToken gera 32 bytes crypto/rand → 64 chars hex (token bruto do reset).
// NÃO reusa generateToken() (que são 24 bytes) — a spec do forgot-pw exige 32 bytes.
func pwResetGenToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// pwResetHashToken devolve SHA-256(token) em hex — o que é persistido na tabela.
// Usado tanto na gravação (forgot) quanto na busca (reset): write==read.
func pwResetHashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// ── Forgot ────────────────────────────────────────────────────────────────────

type pwForgotReq struct {
	Email string `json:"email"`
}

// genericForgotMsg — resposta única e indistinguível (anti-enumeração).
const genericForgotMsg = "Se o e-mail existir, enviamos as instruções."

// ForgotPassword — POST /onboarding/forgot {email}. Público, rate-limited por IP.
// SEMPRE 200 genérico exceto JSON inválido. Ver doc do pacote.
func (h *OnboardingHandler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body pwForgotReq
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	email_ := strings.ToLower(strings.TrimSpace(body.Email))

	// resp é a resposta padrão. Em DEV pode ganhar dev_reset_url; em prod nunca.
	resp := map[string]any{"ok": true, "message": genericForgotMsg}
	respondGeneric := func() { httpx.JSON(w, 200, resp) }

	// Validação de formato falha SILENCIOSA → 200 genérico (não vaza nada).
	if !emailRegex.MatchString(email_) {
		respondGeneric()
		return
	}
	// Tabelas ausentes → 200 genérico (fail-closed sem vazar estado de schema).
	if !h.tableExists(ctx, "senderzz_admin_users") ||
		!h.tableExists(ctx, "senderzz_admin_password_resets") {
		respondGeneric()
		return
	}

	// Busca o admin ATIVO. Não-existe / inativo → 200 genérico (sem revelar).
	var adminID int64
	var adminNome string
	err := h.Pool.QueryRow(ctx,
		`SELECT id, nome FROM senderzz_admin_users WHERE LOWER(email)=LOWER($1) AND ativo=TRUE`,
		email_).Scan(&adminID, &adminNome)
	if err != nil {
		respondGeneric()
		return
	}

	// Anti-bomba por VÍTIMA (complementa o rate-limit por IP do middleware): se já
	// existe um token recém-emitido (<60s) para este admin, não reenvia — evita que
	// um atacante encha a caixa de entrada de um e-mail conhecido. Falha 200 genérico.
	var recent bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM senderzz_admin_password_resets
		   WHERE admin_user_id=$1 AND created_at > NOW() - INTERVAL '60 seconds')`,
		adminID).Scan(&recent)
	if recent {
		respondGeneric()
		return
	}

	token, err := pwResetGenToken()
	if err != nil {
		// Falha de RNG: não dá pra emitir token. 200 genérico (não vaza nada).
		respondGeneric()
		return
	}
	tokenHash := pwResetHashToken(token)

	if _, err := h.Pool.Exec(ctx,
		`INSERT INTO senderzz_admin_password_resets
		   (admin_user_id, token_hash, expires_at, used, created_at)
		 VALUES ($1, $2, NOW() + INTERVAL '30 minutes', FALSE, NOW())`,
		adminID, tokenHash); err != nil {
		// Falha ao persistir → 200 genérico (não dá pra mandar link válido).
		respondGeneric()
		return
	}

	// Monta o link. APP_BASE_URL via env, fallback '' (link relativo /reset?token=).
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("APP_BASE_URL")), "/")
	resetURL := base + "/reset?token=" + token

	// Envio best-effort: NUNCA propaga falha ao usuário (anti-enumeração). Só loga.
	subject := "FalkLog — redefinição de senha do painel"
	htmlBody := pwResetEmailHTML(adminNome, resetURL)
	_ = email.Send(email_, subject, htmlBody) // erro já logado dentro de Send.

	// Em DEV, devolve o link p/ teste sem SMTP. Em produção, chave omitida.
	if pwResetIsDev() {
		resp["dev_reset_url"] = resetURL
	}
	respondGeneric()
}

// pwResetEmailHTML monta o corpo HTML do e-mail de reset (PT-BR).
func pwResetEmailHTML(nome, resetURL string) string {
	saudacao := "Olá"
	if strings.TrimSpace(nome) != "" {
		saudacao = "Olá, " + nome
	}
	return `<div style="font-family:Arial,Helvetica,sans-serif;font-size:15px;color:#0f172a;line-height:1.6">
  <p>` + saudacao + `,</p>
  <p>Recebemos um pedido para redefinir a senha do seu acesso ao painel <strong>FalkLog</strong>.</p>
  <p>Clique no botão abaixo para criar uma nova senha. O link é válido por <strong>30 minutos</strong> e só pode ser usado uma vez.</p>
  <p style="margin:24px 0">
    <a href="` + resetURL + `" style="background:#1E6FF2;color:#fff;text-decoration:none;padding:12px 22px;border-radius:8px;font-weight:600;display:inline-block">Redefinir minha senha</a>
  </p>
  <p style="font-size:13px;color:#64748b">Se o botão não funcionar, copie e cole este endereço no navegador:<br>
    <a href="` + resetURL + `" style="color:#1E6FF2">` + resetURL + `</a></p>
  <p style="font-size:13px;color:#64748b">Se você não solicitou esta redefinição, ignore este e-mail — sua senha permanece a mesma.</p>
  <p style="font-size:12px;color:#94a3b8;margin-top:28px">🦅 FalkLog · Logística de alta performance</p>
</div>`
}

// ── Reset ─────────────────────────────────────────────────────────────────────

type pwResetReq struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

// ResetPassword — POST /onboarding/reset {token, new_password}. Público, rate-limited.
// Consome o token (single-use atômico) e grava a nova senha (bcrypt cost-12, igual ao
// /login). Fail-closed: token inválido/usado/expirado ou senha curta → erro claro.
func (h *OnboardingHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body pwResetReq
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	token := strings.TrimSpace(body.Token)
	if token == "" {
		httpx.Err(w, 400, "validation", "token ausente")
		return
	}
	if len(body.NewPassword) < 8 {
		httpx.Err(w, 400, "validation", "a senha deve ter ao menos 8 caracteres")
		return
	}

	if !h.tableExists(ctx, "senderzz_admin_users") ||
		!h.tableExists(ctx, "senderzz_admin_password_resets") {
		httpx.Err(w, 503, "table_missing", "schema de reset de senha ausente")
		return
	}

	tokenHash := pwResetHashToken(token)

	// Hash da nova senha ANTES da transação (operação cara; bcrypt não precisa de tx).
	hash, err := bcrypt.GenerateFromPassword([]byte(body.NewPassword), 12)
	if err != nil {
		httpx.Err(w, 500, "bcrypt_error", err.Error())
		return
	}

	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer tx.Rollback(ctx)

	// Consumo ATÔMICO do token: só "queima" se ainda válido. RETURNING dá o dono.
	// Zero linhas ⇒ token inválido/usado/expirado ⇒ fail-closed (e a tx é revertida,
	// então nunca marcamos used sem trocar a senha).
	var adminID int64
	err = tx.QueryRow(ctx,
		`UPDATE senderzz_admin_password_resets
		    SET used = TRUE
		  WHERE token_hash = $1 AND used = FALSE AND expires_at > NOW()
		  RETURNING admin_user_id`, tokenHash).Scan(&adminID)
	if err != nil {
		if err == pgx.ErrNoRows {
			httpx.Err(w, 400, "invalid_token", "link inválido ou expirado — solicite uma nova redefinição")
			return
		}
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// Grava a nova senha no admin dono do token (mesmo esquema do /login: bcrypt).
	tag, err := tx.Exec(ctx,
		`UPDATE senderzz_admin_users SET password_hash = $2 WHERE id = $1 AND ativo = TRUE`,
		adminID, string(hash))
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if tag.RowsAffected() == 0 {
		// Admin removido/inativado entre o forgot e o reset → não troca senha de ninguém.
		httpx.Err(w, 400, "invalid_token", "conta indisponível — solicite uma nova redefinição")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// Invalida quaisquer outros tokens pendentes deste admin (defesa em profundidade;
	// fora da tx — best-effort). Já trocou a senha; tokens antigos não devem servir.
	_, _ = h.Pool.Exec(ctx,
		`UPDATE senderzz_admin_password_resets
		    SET used = TRUE
		  WHERE admin_user_id = $1 AND used = FALSE`, adminID)

	httpx.JSON(w, 200, map[string]any{"ok": true, "message": "Senha redefinida com sucesso. Você já pode entrar."})
}
