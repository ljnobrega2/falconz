// Package handlers implementa os handlers HTTP do Portal V2 Senderzz.
//
// Fluxo de autenticação:
//
//  1. POST /portal/login
//     → verifica e-mail + senha (bcrypt)
//     → gera código 2FA de 6 dígitos e salva em senderzz_portal_2fa (upsert, TTL 10 min)
//     → envia código por e-mail (slog em dev — produção usa SMTP externo)
//     → emite partial_token (JWT com pending_2fa=true, TTL 5 min)
//     → retorna {requires_2fa: true, partial_token}
//
//  2. POST /portal/login/2fa
//     → valida partial_token (ParsePartialJWT)
//     → verifica código 2FA (máx 5 tentativas — fail-closed)
//     → cria sessão em senderzz_portal_sessions (token raw + HMAC, TTL 24h)
//     → emite JWT completo (claims: user_id, email, role, sid=session_token)
//     → seta cookie HttpOnly sz_portal_token
//     → retorna {token: jwt}
//
//  3. POST /portal/logout → invalida sessão no banco
//  4. POST /portal/refresh → renova JWT se sessão válida
//  5. GET  /portal/me → dados do usuário autenticado
//
// Namespace: /wp-json/senderzz/v1
package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/senderzz/portal-service/internal/auth"
	"github.com/senderzz/portal-service/internal/httpx"
)

// AuthHandler agrupa as dependências dos handlers de autenticação.
type AuthHandler struct {
	Pool *pgxpool.Pool
}

// ── Requests/Responses ─────────────────────────────────────────────────────────

type loginRequest struct {
	Email string `json:"email"`
	Senha string `json:"senha"`
}

type login2FARequest struct {
	PartialToken string `json:"partial_token"`
	Codigo       string `json:"codigo"`
}

// ── POST /wp-json/senderzz/v1/portal/login ────────────────────────────────────

// Login verifica credenciais, gera 2FA e retorna partial_token.
// Se o usuário não tem 2FA ativado, emite JWT completo diretamente
// (retorna {requires_2fa: false, token: jwt}).
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}
	if req.Email == "" || req.Senha == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "e-mail e senha são obrigatórios")
		return
	}

	// Busca usuário por e-mail.
	var userID int64
	var nome, role, passwordHash string
	var ativo, twofaEnabled bool

	err := h.Pool.QueryRow(r.Context(),
		`SELECT id, nome, role, COALESCE(password_hash,''), ativo, twofa_enabled
		   FROM senderzz_portal_users
		  WHERE email = $1
		  LIMIT 1`,
		req.Email,
	).Scan(&userID, &nome, &role, &passwordHash, &ativo, &twofaEnabled)

	if err == pgx.ErrNoRows {
		// Resposta genérica — não revela se o e-mail existe (enumeração).
		httpx.WriteErr(w, http.StatusUnauthorized, "credenciais inválidas")
		return
	}
	if err != nil {
		slog.Error("[portal_login] erro ao buscar usuário", "email", req.Email, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Conta suspensa.
	if !ativo {
		httpx.WriteErr(w, http.StatusForbidden, "conta suspensa — contate o suporte")
		return
	}

	// Verifica senha via bcrypt.
	if passwordHash == "" {
		// Usuário sem senha Go nativa — autenticação exclusiva via WP (não implementada aqui).
		httpx.WriteErr(w, http.StatusUnauthorized, "credenciais inválidas")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Senha)); err != nil {
		slog.Info("[portal_login] senha incorreta", "user_id", userID)
		httpx.WriteErr(w, http.StatusUnauthorized, "credenciais inválidas")
		return
	}

	// Se 2FA desativado, emite JWT completo imediatamente com sessão nova.
	if !twofaEnabled {
		sessionToken, err := h.createSession(r.Context(), userID, r)
		if err != nil {
			slog.Error("[portal_login] erro ao criar sessão", "user_id", userID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
			return
		}
		jwtToken, err := auth.EmitJWT(userID, req.Email, role, sessionToken)
		if err != nil {
			slog.Error("[portal_login] erro ao emitir JWT", "user_id", userID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
			return
		}
		setPortalCookie(w, jwtToken)
		slog.Info("[portal_login] login sem 2FA", "user_id", userID, "role", role)
		httpx.WriteOK(w, map[string]any{
			"requires_2fa": false,
			"token":        jwtToken,
			"user": map[string]any{
				"id":    userID,
				"nome":  nome,
				"email": req.Email,
				"role":  role,
			},
		})
		return
	}

	// Gera código 2FA de 6 dígitos (crypto/rand).
	code, err := gerarCodigo2FA()
	if err != nil {
		slog.Error("[portal_login] erro ao gerar código 2FA", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Salva código 2FA — upsert (apenas um código ativo por usuário).
	_, err = h.Pool.Exec(r.Context(),
		`INSERT INTO senderzz_portal_2fa (user_id, code, expires_at, tentativas)
		 VALUES ($1, $2, NOW() + INTERVAL '10 minutes', 0)
		 ON CONFLICT (user_id)
		 DO UPDATE SET code = EXCLUDED.code,
		               expires_at = EXCLUDED.expires_at,
		               tentativas = 0`,
		userID, code,
	)
	if err != nil {
		slog.Error("[portal_login] erro ao salvar código 2FA", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Em produção, enviar via SMTP. Em dev, logar.
	enviarCodigo2FA(req.Email, code)

	// Emite partial_token (JWT curto, pending_2fa=true).
	partialToken, err := auth.EmitPartialJWT(userID, req.Email)
	if err != nil {
		slog.Error("[portal_login] erro ao emitir partial token", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	slog.Info("[portal_login] 2FA requerido — código enviado", "user_id", userID)
	httpx.WriteOK(w, map[string]any{
		"requires_2fa":  true,
		"partial_token": partialToken,
	})
}

// ── POST /wp-json/senderzz/v1/portal/login/2fa ────────────────────────────────

// Login2FA verifica o código de 2FA e emite JWT completo + sessão.
func (h *AuthHandler) Login2FA(w http.ResponseWriter, r *http.Request) {
	var req login2FARequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}
	if req.PartialToken == "" || req.Codigo == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "partial_token e codigo são obrigatórios")
		return
	}

	// Valida partial_token (rejeita tokens completos).
	claims, err := auth.ParsePartialJWT(req.PartialToken)
	if err != nil {
		slog.Warn("[portal_2fa] partial_token inválido", "err", err)
		httpx.WriteErr(w, http.StatusUnauthorized, "token de verificação inválido ou expirado")
		return
	}

	userID := claims.UserID

	// Busca código 2FA ativo no banco — transação para incrementar tentativas atomicamente.
	tx, err := h.Pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		slog.Error("[portal_2fa] erro ao iniciar transação", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	var storedCode string
	var expiresAt time.Time
	var tentativas int16

	err = tx.QueryRow(r.Context(),
		`SELECT code, expires_at, tentativas
		   FROM senderzz_portal_2fa
		  WHERE user_id = $1
		  FOR UPDATE`,
		userID,
	).Scan(&storedCode, &expiresAt, &tentativas)

	if err == pgx.ErrNoRows {
		tx.Rollback(r.Context()) //nolint:errcheck
		httpx.WriteErr(w, http.StatusUnauthorized, "código não encontrado — faça login novamente")
		return
	}
	if err != nil {
		slog.Error("[portal_2fa] erro ao buscar código 2FA", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Fail-closed: máximo de 5 tentativas.
	if tentativas >= 5 {
		tx.Rollback(r.Context()) //nolint:errcheck
		httpx.WriteErr(w, http.StatusTooManyRequests, "muitas tentativas — faça login novamente")
		return
	}

	// Código expirado.
	if time.Now().After(expiresAt) {
		tx.Rollback(r.Context()) //nolint:errcheck
		httpx.WriteErr(w, http.StatusUnauthorized, "código expirado — faça login novamente")
		return
	}

	// Incrementa tentativas antes de verificar (fail-closed: tentativa errada já conta).
	_, err = tx.Exec(r.Context(),
		`UPDATE senderzz_portal_2fa SET tentativas = tentativas + 1 WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		slog.Error("[portal_2fa] erro ao incrementar tentativas", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Verifica código (comparação constante para evitar timing attacks).
	if !compareSafe(req.Codigo, storedCode) {
		_ = tx.Commit(r.Context()) // commit o incremento de tentativas
		slog.Info("[portal_2fa] código incorreto", "user_id", userID, "tentativas", tentativas+1)
		httpx.WriteErr(w, http.StatusUnauthorized, "código incorreto")
		return
	}

	// Código válido — remove o registro de 2FA.
	_, err = tx.Exec(r.Context(),
		`DELETE FROM senderzz_portal_2fa WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		slog.Error("[portal_2fa] erro ao remover código 2FA", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("[portal_2fa] erro ao commit", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Busca dados do usuário para o JWT e cookie.
	var email, role string
	err = h.Pool.QueryRow(r.Context(),
		`SELECT email, role FROM senderzz_portal_users WHERE id = $1`,
		userID,
	).Scan(&email, &role)
	if err != nil {
		slog.Error("[portal_2fa] erro ao buscar usuário pós-2FA", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Cria sessão e emite JWT completo.
	sessionToken, err := h.createSession(r.Context(), userID, r)
	if err != nil {
		slog.Error("[portal_2fa] erro ao criar sessão", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	jwtToken, err := auth.EmitJWT(userID, email, role, sessionToken)
	if err != nil {
		slog.Error("[portal_2fa] erro ao emitir JWT", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	setPortalCookie(w, jwtToken)
	slog.Info("[portal_2fa] 2FA confirmado — sessão criada", "user_id", userID, "role", role)
	httpx.WriteOK(w, map[string]any{"token": jwtToken})
}

// ── POST /wp-json/senderzz/v1/portal/logout ───────────────────────────────────

// Logout invalida a sessão atual no banco.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	// Remove sessão pelo token raw.
	_, err := h.Pool.Exec(r.Context(),
		`DELETE FROM senderzz_portal_sessions WHERE token = $1 AND user_id = $2`,
		u.SessionToken, u.ID,
	)
	if err != nil {
		slog.Error("[portal_logout] erro ao invalidar sessão", "user_id", u.ID, "err", err)
		// Retorna ok mesmo assim — sessão expirará naturalmente.
	}

	// Limpa cookie.
	http.SetCookie(w, &http.Cookie{
		Name:     "sz_portal_token",
		Value:    "",
		MaxAge:   -1,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	slog.Info("[portal_logout] logout realizado", "user_id", u.ID)
	httpx.WriteOK(w, map[string]any{"mensagem": "logout realizado com sucesso"})
}

// ── POST /wp-json/senderzz/v1/portal/refresh ──────────────────────────────────

// Refresh renova o JWT se a sessão ainda estiver válida no banco.
// Prolonga a sessão por mais 24h a partir do momento do refresh.
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	// Prorroga a sessão por mais 24h.
	_, err := h.Pool.Exec(r.Context(),
		`UPDATE senderzz_portal_sessions
		    SET expires_at = NOW() + INTERVAL '24 hours'
		  WHERE token = $1 AND user_id = $2`,
		u.SessionToken, u.ID,
	)
	if err != nil {
		slog.Error("[portal_refresh] erro ao prorrogar sessão", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	newJWT, err := auth.EmitJWT(u.ID, u.Email, u.Role, u.SessionToken)
	if err != nil {
		slog.Error("[portal_refresh] erro ao emitir novo JWT", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	setPortalCookie(w, newJWT)
	httpx.WriteOK(w, map[string]any{"token": newJWT})
}

// ── GET /wp-json/senderzz/v1/portal/me ────────────────────────────────────────

// Me retorna os dados do usuário autenticado.
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	var nome, plano string
	var twofaEnabled bool
	var settingsRaw []byte

	err := h.Pool.QueryRow(r.Context(),
		`SELECT nome, plano, twofa_enabled, settings
		   FROM senderzz_portal_users
		  WHERE id = $1`,
		u.ID,
	).Scan(&nome, &plano, &twofaEnabled, &settingsRaw)

	if err != nil {
		slog.Error("[portal_me] erro ao buscar usuário", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	httpx.WriteOK(w, map[string]any{
		"id":            u.ID,
		"wp_user_id":    u.WPUserID,
		"email":         u.Email,
		"nome":          nome,
		"role":          u.Role,
		"plano":         plano,
		"twofa_enabled": twofaEnabled,
		"settings":      json.RawMessage(settingsRaw),
	})
}

// ── Helpers privados ──────────────────────────────────────────────────────────

// createSession cria uma nova sessão no banco e retorna o token raw.
// Armazena token raw + HMAC para compatibilidade durante migração WP → Go.
func (h *AuthHandler) createSession(ctx context.Context, userID int64, r *http.Request) (string, error) {
	// Gera token raw: 32 bytes aleatórios → hex (64 chars).
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("erro ao gerar token: %w", err)
	}
	tokenRaw := hex.EncodeToString(buf)

	// Calcula HMAC com WP_SALT_AUTH.
	salt := os.Getenv("WP_SALT_AUTH")
	mac := hmac.New(sha256.New, []byte(salt))
	mac.Write([]byte(tokenRaw))
	tokenHMAC := hex.EncodeToString(mac.Sum(nil))

	ip := r.RemoteAddr
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ip = xff
	}

	_, err := h.Pool.Exec(ctx,
		`INSERT INTO senderzz_portal_sessions
		    (user_id, token, token_hmac, ip, user_agent, expires_at)
		 VALUES ($1, $2, $3, $4, $5, NOW() + INTERVAL '24 hours')`,
		userID, tokenRaw, tokenHMAC, ip, r.UserAgent(),
	)
	if err != nil {
		return "", fmt.Errorf("erro ao inserir sessão: %w", err)
	}

	return tokenRaw, nil
}

// setPortalCookie seta o cookie HttpOnly sz_portal_token com o JWT.
func setPortalCookie(w http.ResponseWriter, jwtToken string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "sz_portal_token",
		Value:    jwtToken,
		Path:     "/",
		MaxAge:   86400, // 24h
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		// Secure: true em produção (nginx termina TLS antes do Go).
		// Descomentar quando o serviço estiver atrás de proxy HTTPS.
		// Secure: true,
	})
}

// gerarCodigo2FA gera um código de 6 dígitos usando crypto/rand.
func gerarCodigo2FA() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// enviarCodigo2FA envia o código por e-mail.
// Em dev: loga. Em produção: integrar com SMTP/SES externo.
func enviarCodigo2FA(email, code string) {
	// TODO: integrar com serviço de e-mail em produção (SendGrid, SES, etc.)
	slog.Info("[portal_2fa] código de verificação",
		"email", email,
		"code", code, // ATENÇÃO: remover em produção — não logar código em prod
		"instrucao", "integrar com SMTP externo para envio real em produção",
	)
}

// compareSafe compara duas strings com tempo constante para evitar timing attacks.
func compareSafe(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := 0; i < len(a); i++ {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}
