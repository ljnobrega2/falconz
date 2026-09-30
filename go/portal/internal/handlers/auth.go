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
//
//  4. POST /portal/refresh → renova JWT se sessão válida
//
//  5. GET  /portal/me → dados do usuário autenticado
//
// Namespace: /wp-json/senderzz/v1
package handlers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/portal-service/internal/auth"
	"github.com/senderzz/portal-service/internal/httpx"
)

// AuthHandler agrupa as dependências dos handlers de autenticação.
type AuthHandler struct {
	Pool *pgxpool.Pool
}

// loginRL é o rate limiter de login, compartilhado por todas as instâncias do
// handler. Singleton para não exigir mudança no construtor (main.go monta o
// AuthHandler só com Pool). // SEC-LOGIN-RATE-LIMIT-NONE
var loginRL = sync.OnceValue(newLoginRateLimiter)

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

	// Rate-limit ANTES do bcrypt (custoso): trava força bruta por IP e por e-mail.
	// 429 não revela se o e-mail existe (mesma resposta para qualquer e-mail).
	// AUDIT SEC-LOGIN-RATE-LIMIT-NONE.
	if !loginRL().allow(loginClientIP(r), req.Email) {
		slog.Warn("[portal_login] rate-limit excedido", "ip", loginClientIP(r))
		httpx.WriteErr(w, http.StatusTooManyRequests,
			"muitas tentativas de login — aguarde alguns minutos e tente novamente")
		return
	}

	// Busca usuário por e-mail.
	var userID int64
	var nome, role, passwordHash string
	var ativo, twofaEnabled bool

	// LOGIN-UNICO bugfix: comparação de e-mail case-insensitive. Antes `email = $1`
	// fazia "Gabriel@x.com" logar e "gabriel@x.com" dar 401. A página única de login
	// normaliza o e-mail (trim+lowercase) no front, mas o backend também é blindado
	// para casar com o /admin/login (que já usa LOWER(email)=LOWER($1)).
	err := h.Pool.QueryRow(r.Context(),
		`SELECT id, nome, role, COALESCE(password_hash,''), ativo, twofa_enabled
		   FROM senderzz_portal_users
		  WHERE LOWER(email) = LOWER($1)
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

	// Verifica senha. // CRIT-WP-HASH
	// auth.VerifyPassword aceita o formato WP 6.8+ ("$wp$" = bcrypt sobre pré-hash
	// HMAC-SHA384) — formato dos hashes reais importados no cutover — E bcrypt puro
	// ($2y$) das contas criadas pelo próprio Go. bcrypt direto falhava em 100% dos
	// usuários reais (hash "$wp$" nunca casava).
	if passwordHash == "" {
		// Usuário sem senha Go nativa — autenticação exclusiva via WP (não implementada aqui).
		httpx.WriteErr(w, http.StatusUnauthorized, "credenciais inválidas")
		return
	}
	if !auth.VerifyPassword(passwordHash, req.Senha) {
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
	// AUDIT-2026-06-21 #19: o contador de tentativas é DURÁVEL por janela. Antes,
	// o re-login zerava `tentativas` (DO UPDATE SET tentativas = 0) → +5 palpites a
	// cada ciclo de login. Agora só zera quando a janela anterior já expirou; dentro
	// de uma janela ativa o contador é preservado, mantendo o teto fail-closed de 5.
	_, err = h.Pool.Exec(r.Context(),
		`INSERT INTO senderzz_portal_2fa (user_id, code, expires_at, tentativas)
		 VALUES ($1, $2, NOW() + INTERVAL '10 minutes', 0)
		 ON CONFLICT (user_id)
		 DO UPDATE SET code = EXCLUDED.code,
		               expires_at = EXCLUDED.expires_at,
		               tentativas = CASE WHEN senderzz_portal_2fa.expires_at < NOW()
		                                 THEN 0
		                                 ELSE senderzz_portal_2fa.tentativas END`,
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

	// Limpa cookie. SEC-P1-COOKIE-SECURE: mesmos atributos do set (HttpOnly+Secure+
	// SameSite=Lax) — o clear deve casar o set p/ o browser de fato remover o cookie.
	http.SetCookie(w, &http.Cookie{
		Name:     "sz_portal_token",
		Value:    "",
		MaxAge:   -1,
		Path:     "/",
		HttpOnly: true,
		Secure:   secureCookies(),
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
	var shippingClassID *int64 // NULL → null no JSON (produtor sem classe de frete)
	// document (CPF) e phone vêm do cadastro (migração 427). NULL → "" no JSON.
	// O Portal V2 (Settings.tsx) usa me.document p/ auto-preencher a chave PIX
	// quando tipo='cpf' e exibe me.phone na aba Conta.
	var document, phone string

	err := h.Pool.QueryRow(r.Context(),
		`SELECT nome, plano, twofa_enabled, settings, shipping_class_id,
		        COALESCE(document, ''), COALESCE(phone, '')
		   FROM senderzz_portal_users
		  WHERE id = $1`,
		u.ID,
	).Scan(&nome, &plano, &twofaEnabled, &settingsRaw, &shippingClassID, &document, &phone)

	if err != nil {
		slog.Error("[portal_me] erro ao buscar usuário", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// vitrineHasProducts: EXISTS site-wide (qualquer produtor), não escopado a este
	// usuário — pedido do dono 2026-07-11: se NENHUM produto está visível na vitrine
	// (em toda a plataforma), o item de nav "Vitrine" some pra todo mundo no portal
	// (produtor/afiliado/cliente). Só o admin (admin-ui, gerencia produtos direto)
	// continua enxergando/mexendo — sem gate aqui, é outro app.
	// ESPELHA EXATAMENTE os filtros de vitrine.go List() (senão o gate mente): produtor
	// tem que ser role='produtor' ATIVO, status fora do ciclo de aprovação pendente/
	// reprovado, vitrine_visible, e nome fora das exclusões (recarga/carteira de frete/
	// kit/combo/pacote/pack/bundle/conjunto — produtos "utilitários" que não são
	// catálogo de venda real).
	var vitrineHasProducts bool
	_ = h.Pool.QueryRow(r.Context(),
		`SELECT EXISTS(
		   SELECT 1 FROM sz_products sp
		   JOIN senderzz_portal_users pu ON pu.id = sp.produtor_id
		    AND pu.role = 'produtor' AND pu.ativo
		    WHERE sp.status IS DISTINCT FROM 'deleted'
		      AND sp.status NOT IN ('a_aprovar', 'reprovado')
		      AND COALESCE(sp.vitrine_visible, true) = true
		      AND sp.nome NOT ILIKE '%recarga%'
		      AND sp.nome NOT ILIKE '%carteira de frete%'
		      AND sp.nome NOT ILIKE '%frete interno%'
		      AND sp.nome !~* '\m(kit|combo|pacote|pack|bundle|conjunto)\M'
		 )`,
	).Scan(&vitrineHasProducts)

	httpx.WriteOK(w, map[string]any{
		"id":            u.ID,
		"wp_user_id":    u.WPUserID,
		"email":         u.Email,
		"nome":          nome,
		"role":          u.Role,
		"plano":         plano,
		"twofa_enabled": twofaEnabled,
		// shipping_class_id desbloqueia a sub-aba "Carteira de Expedição" e o item
		// de nav "expedicao" no frontend (hasExp / hasShippingClass). null quando
		// o produtor não tem classe de frete definida.
		"shipping_class_id":    shippingClassID,
		"vitrine_has_products": vitrineHasProducts,
		// document (CPF) + phone do cadastro — o front PIX (Settings.tsx) lê estes
		// campos. String vazia quando o usuário ainda não tem o dado preenchido.
		"document": document,
		"phone":    phone,
		"settings": json.RawMessage(settingsRaw),
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

// secureCookies decide a flag Secure do cookie de sessão. SEC-P1-COOKIE-SECURE:
// o app é servido por HTTPS (cloudflare tunnel / prod atrás de proxy TLS), então
// Secure DEVE estar ligado. DEFAULT SEGURO (Secure ON): só desliga explicitamente
// com SECURE_COOKIES='0' (escape hatch p/ dev em http://localhost, onde um cookie
// Secure não é enviado pelo browser). Qualquer outro valor (ou ausência) ⇒ true.
func secureCookies() bool {
	return os.Getenv("SECURE_COOKIES") != "0"
}

// setPortalCookie seta o cookie HttpOnly+Secure sz_portal_token com o JWT.
// SEC-P1-COOKIE-SECURE: HttpOnly (sem acesso por JS) + SameSite=Lax (mitiga CSRF) +
// Secure (só trafega sob HTTPS) — Secure controlado por secureCookies() (default ON).
func setPortalCookie(w http.ResponseWriter, jwtToken string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "sz_portal_token",
		Value:    jwtToken,
		Path:     "/",
		MaxAge:   86400, // 24h
		HttpOnly: true,
		Secure:   secureCookies(),
		SameSite: http.SameSiteLaxMode,
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

// enviarCodigo2FA envia o código de verificação por e-mail via Resend.
// Em dev (APP_ENV=development/dev/test/local/staging): loga o código — não envia.
// Em produção: chama a API Resend (RESEND_API_KEY + MAIL_FROM obrigatórios).
//
// SEC-OTP-LOG-GATE: código NUNCA logado em produção.
func enviarCodigo2FA(to, code string) {
	if appEnvIsDev() {
		slog.Info("[portal_2fa] código de verificação (dev)",
			"email", to,
			"code", code,
		)
		return
	}

	apiKey := os.Getenv("RESEND_API_KEY")
	from := os.Getenv("MAIL_FROM")
	if apiKey == "" || from == "" {
		slog.Error("[portal_2fa] RESEND_API_KEY ou MAIL_FROM não configurados — e-mail não enviado", "email", to)
		return
	}

	body, _ := json.Marshal(map[string]any{
		"from":    from,
		"to":      []string{to},
		"subject": "Seu código de acesso Falklog",
		"html": fmt.Sprintf(`<div style="font-family:sans-serif;max-width:480px;margin:auto">
<h2 style="color:#f97316">Falklog</h2>
<p>Seu código de verificação é:</p>
<div style="font-size:36px;font-weight:700;letter-spacing:8px;color:#111;padding:16px 0">%s</div>
<p style="color:#666;font-size:13px">Válido por 10 minutos. Não compartilhe este código.</p>
</div>`, code),
	})

	req, err := http.NewRequest("POST", "https://api.resend.com/emails", bytes.NewReader(body))
	if err != nil {
		slog.Error("[portal_2fa] erro ao criar request Resend", "err", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("[portal_2fa] erro ao chamar Resend", "email", to, "err", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		slog.Error("[portal_2fa] Resend retornou erro", "email", to, "status", resp.StatusCode, "body", string(b))
		return
	}
	slog.Info("[portal_2fa] código enviado via Resend", "email", to)
}

// appEnvIsDev retorna true somente para ambientes de desenvolvimento/teste
// conhecidos. Qualquer outro valor (inclusive APP_ENV ausente ou "production")
// é tratado como NÃO-dev — fail-closed para o log do código 2FA.
func appEnvIsDev() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV"))) {
	case "development", "dev", "test", "local", "staging":
		return true
	default:
		return false
	}
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

// ── POST /portal/forgot-password ───────────────────────────────────────────────
// Gera token de reset e envia e-mail com link. Resposta genérica (não revela se
// o e-mail existe). Token válido por 30 minutos.
func (h *AuthHandler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "e-mail é obrigatório")
		return
	}
	// AUDIT-2026-07-31 MEDIUM: rate-limit por IP+e-mail (era irrestrito).
	if !forgotPasswordAllow(loginClientIP(r), strings.ToLower(strings.TrimSpace(req.Email))) {
		httpx.WriteErr(w, http.StatusTooManyRequests, "muitas tentativas — aguarde e tente novamente")
		return
	}
	ctx := r.Context()

	// Cria tabela se não existir (auto-migrate graciosa).
	_, _ = h.Pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS senderzz_portal_password_resets (
			id         BIGSERIAL PRIMARY KEY,
			email      TEXT NOT NULL,
			token      TEXT NOT NULL UNIQUE,
			expires_at TIMESTAMPTZ NOT NULL,
			used       BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`)

	// Verifica se e-mail existe (silencioso — resposta idêntica se não existir).
	var userID int64
	var nome string
	err := h.Pool.QueryRow(ctx,
		`SELECT id, COALESCE(nome,'') FROM senderzz_portal_users WHERE LOWER(email)=LOWER($1) AND ativo=true LIMIT 1`,
		req.Email,
	).Scan(&userID, &nome)
	if err != nil {
		// Não revela se e-mail existe.
		httpx.WriteOK(w, map[string]any{"ok": true, "msg": "Se o e-mail estiver cadastrado, você receberá as instruções."})
		return
	}

	// Gera token seguro (32 bytes hex).
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	token := hex.EncodeToString(tokenBytes)

	_, err = h.Pool.Exec(ctx,
		`INSERT INTO senderzz_portal_password_resets (email, token, expires_at)
		 VALUES ($1, $2, NOW() + INTERVAL '30 minutes')
		 ON CONFLICT (token) DO NOTHING`,
		strings.ToLower(req.Email), token,
	)
	if err != nil {
		slog.Error("[portal_forgot] erro ao salvar token", "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	go enviarEmailReset(req.Email, nome, token)

	httpx.WriteOK(w, map[string]any{"ok": true, "msg": "Se o e-mail estiver cadastrado, você receberá as instruções."})
}

// ── POST /portal/reset-password ────────────────────────────────────────────────
// Valida token e redefine a senha.
func (h *AuthHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
		Senha string `json:"senha"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" || req.Senha == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "token e senha são obrigatórios")
		return
	}
	if len(req.Senha) < 6 {
		httpx.WriteErr(w, http.StatusBadRequest, "a senha deve ter ao menos 6 caracteres")
		return
	}
	ctx := r.Context()

	var email string
	err := h.Pool.QueryRow(ctx,
		`SELECT email FROM senderzz_portal_password_resets
		 WHERE token=$1 AND used=FALSE AND expires_at > NOW() LIMIT 1`,
		req.Token,
	).Scan(&email)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "link inválido ou expirado")
		return
	}

	hash, err := auth.HashPassword(req.Senha)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx,
		`UPDATE senderzz_portal_users SET password_hash=$1 WHERE LOWER(email)=LOWER($2)`,
		hash, email,
	)
	if err != nil {
		slog.Error("[portal_reset] erro ao atualizar senha", "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	_, _ = tx.Exec(ctx,
		`UPDATE senderzz_portal_password_resets SET used=TRUE WHERE token=$1`,
		req.Token,
	)
	if err := tx.Commit(ctx); err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	slog.Info("[portal_reset] senha redefinida", "email", email)
	httpx.WriteOK(w, map[string]any{"ok": true})
}

// enviarEmailReset envia o link de redefinição de senha via Resend.
func enviarEmailReset(to, nome, token string) {
	appURL := os.Getenv("APP_URL")
	if appURL == "" {
		appURL = "https://app.falklog.com.br"
	}
	// AUDIT-2026-07-31 (dono): portal-ui roda sob basename "/portal" (main.tsx) —
	// faltava o prefixo, o link caía em "/reset-password" (fora de qualquer rota
	// do domínio raiz, 404/redirect) em vez de "/portal/reset-password" (a rota
	// real, ver App.tsx).
	link := appURL + "/portal/reset-password?token=" + token

	if appEnvIsDev() {
		slog.Info("[portal_reset] link de redefinição (dev)", "email", to, "link", link)
		return
	}

	apiKey := os.Getenv("RESEND_API_KEY")
	from := os.Getenv("MAIL_FROM")
	if apiKey == "" || from == "" {
		slog.Error("[portal_reset] RESEND_API_KEY ou MAIL_FROM não configurados", "email", to)
		return
	}

	saudacao := "Olá"
	if nome != "" {
		saudacao = "Olá, " + nome
	}

	body, _ := json.Marshal(map[string]any{
		"from":    from,
		"to":      []string{to},
		"subject": "Redefinição de senha — Falklog",
		"html": fmt.Sprintf(`<div style="font-family:sans-serif;max-width:480px;margin:auto">
<h2 style="color:#f97316">Falklog</h2>
<p>%s!</p>
<p>Recebemos uma solicitação para redefinir a senha da sua conta.</p>
<p style="margin:24px 0">
  <a href="%s" style="display:inline-block;padding:14px 28px;background:#1E6FF2;color:#fff;border-radius:10px;text-decoration:none;font-weight:700;font-size:15px">
    Redefinir minha senha
  </a>
</p>
<p style="color:#666;font-size:13px">Este link expira em 30 minutos. Se você não solicitou a redefinição, ignore este e-mail.</p>
<p style="color:#aaa;font-size:12px">Ou copie e cole no navegador:<br>%s</p>
</div>`, saudacao, link, link),
	})

	req, err := http.NewRequest("POST", "https://api.resend.com/emails", bytes.NewReader(body))
	if err != nil {
		slog.Error("[portal_reset] erro ao criar request Resend", "err", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("[portal_reset] erro ao chamar Resend", "email", to, "err", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		slog.Error("[portal_reset] Resend erro", "email", to, "status", resp.StatusCode, "body", string(b))
		return
	}
	slog.Info("[portal_reset] e-mail de redefinição enviado", "email", to)
}
