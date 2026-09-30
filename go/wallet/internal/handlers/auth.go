// auth.go — endpoints de autenticação do serviço de Carteira (tp-carteira/v1).
//
// Port FIEL de includes/tpc/rest-api.php:311-420 (M-02). Três rotas:
//
//	POST /auth/token                       — credenciais → JWT (anti-brute-force 10/5min/IP)
//	POST /auth/token-from-portal-session   — sessão do painel → JWT (HMAC + lookup)
//	GET  /me                               — perfil + saldo do usuário autenticado
//
// ── Decisão de migração (PORT-AUTH-01) ───────────────────────────────────────
// No WordPress, tpc_endpoint_auth_token usa wp_authenticate() contra wp_users
// (hash phpass). O schema Postgres de migração NÃO replica wp_users — a fonte de
// identidade no mundo Go é senderzz_portal_users (idêntico ao login do serviço
// Portal). Portanto /auth/token valida e-mail + senha contra senderzz_portal_users
// e emite o JWT sobre o wp_user_id vinculado (mesmo `sub` que o PHP emitiria via
// tpc_jwt_encode($user->ID)). A verificação de senha usa wphash.CheckPassword, que
// aceita bcrypt (hash nativo Go) E phpass/$wp$ (hash importado do WP) — assim um
// usuário migrado do WP autentica sem reset de senha. Comportamento observável
// (token, claims, formato) permanece idêntico ao do PHP.
//
// Anti-brute-force: o PHP usa transient tpc_auth_rl_<md5(ip)> com teto 10/5min.
// Aqui usamos mapa+mutex em memória com janela deslizante (mesmo padrão do
// serviço Admin, auth.LoginRateLimit) — 10 tentativas por IP em 5 minutos → 429.
package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/senderzz/wallet-service/internal/httpx"
	"github.com/senderzz/wallet-service/internal/middleware"
	"github.com/senderzz/wallet-service/internal/wphash"
)

// AuthHandler agrupa as dependências dos endpoints de autenticação.
type AuthHandler struct {
	db *pgxpool.Pool
	rl *authRateLimiter
}

// NewAuthHandler cria um AuthHandler com o pool fornecido e o rate-limiter de login.
func NewAuthHandler(db *pgxpool.Pool) *AuthHandler {
	return &AuthHandler{db: db, rl: newAuthRateLimiter()}
}

// ── Rate limiter (anti brute-force) ──────────────────────────────────────────
// Espelha o transient tpc_auth_rl_<md5(ip)> do PHP: 10 tentativas por IP / 5 min.

const (
	authRateMax    = 10              // tpc: $attempts >= 10 → 429
	authRateWindow = 5 * time.Minute // tpc: set_transient(..., 5 * MINUTE_IN_SECONDS)
)

type authRateLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func newAuthRateLimiter() *authRateLimiter {
	return &authRateLimiter{hits: make(map[string][]time.Time)}
}

// allow registra uma tentativa e retorna false se o teto da janela foi atingido.
// Idêntico à semântica do PHP: o contador incrementa ANTES de validar a senha
// (set_transient roda antes de wp_authenticate), e sucesso reseta o contador.
func (l *authRateLimiter) allow(ip string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	win := l.hits[ip][:0]
	for _, t := range l.hits[ip] {
		if now.Sub(t) < authRateWindow {
			win = append(win, t)
		}
	}
	if len(win) >= authRateMax {
		l.hits[ip] = win
		return false
	}
	l.hits[ip] = append(win, now)
	return true
}

// reset zera o contador do IP — chamado em login bem-sucedido (delete_transient).
func (l *authRateLimiter) reset(ip string) {
	l.mu.Lock()
	delete(l.hits, ip)
	l.mu.Unlock()
}

// clientIP é definido em pix.go (mesmo pacote handlers) — reutilizado aqui.

// ── POST /auth/token ──────────────────────────────────────────────────────────

type authTokenRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// PostAuthToken valida credenciais e emite um JWT. Port FIEL de
// tpc_endpoint_auth_token (rest-api.php:311). Ver PORT-AUTH-01 no topo do arquivo.
func (h *AuthHandler) PostAuthToken(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)

	// Rate limit: 10 tentativas por IP a cada 5 min (espelha o transient do PHP).
	if !h.rl.allow(ip) {
		slog.Warn("[tpc_auth] rate-limit de login excedido", "ip", ip)
		w.Header().Set("Retry-After", "300")
		httpx.WriteErr(w, http.StatusTooManyRequests, "Muitas tentativas. Aguarde alguns minutos.")
		return
	}

	var req authTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}

	// sanitize_user() do WP: corta espaços nas pontas. Mantemos o resto intacto
	// (o lookup é por e-mail no schema Go).
	username := strings.TrimSpace(req.Username)
	if username == "" || req.Password == "" {
		httpx.WriteErr(w, http.StatusUnauthorized, "Credenciais inválidas.")
		return
	}

	// Busca o usuário do painel por e-mail (identidade no mundo Go — PORT-AUTH-01).
	var (
		portalUserID int64
		wpUserID     *int64
		email        string
		nome         string
		role         string
		ativo        bool
		passwordHash string
	)
	err := h.db.QueryRow(r.Context(),
		`SELECT id, wp_user_id, email, COALESCE(nome,''), COALESCE(role,'cliente'),
		        ativo, COALESCE(password_hash,'')
		   FROM senderzz_portal_users
		  WHERE LOWER(email) = LOWER($1)
		  LIMIT 1`,
		username,
	).Scan(&portalUserID, &wpUserID, &email, &nome, &role, &ativo, &passwordHash)

	if err == pgx.ErrNoRows {
		// Resposta genérica — não revela existência do e-mail (anti-enumeração),
		// mesma mensagem que o PHP retorna para credencial inválida.
		httpx.WriteErr(w, http.StatusUnauthorized, "Credenciais inválidas.")
		return
	}
	if err != nil {
		slog.Error("[tpc_auth] erro ao buscar usuário", "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Conta suspensa: verificada ANTES da senha (igual ao serviço Portal) para não
	// vazar, via 403-vs-401, que a senha correta pertence a uma conta suspensa.
	if !ativo {
		httpx.WriteErr(w, http.StatusUnauthorized, "Credenciais inválidas.")
		return
	}

	// Verifica senha: aceita bcrypt (nativo Go) e phpass/$wp$ (importado do WP).
	if passwordHash == "" || !wphash.CheckPassword(req.Password, passwordHash) {
		httpx.WriteErr(w, http.StatusUnauthorized, "Credenciais inválidas.")
		return
	}

	// MIGRAÇÃO 2026-07-28: o JWT era emitido sobre wp_user_id (mundo WP legado) —
	// isso travava a carteira pra QUALQUER conta 100% nativa do FALK (sem WordPress),
	// já que produtor/afiliado criado direto no Go nunca tem wp_user_id. A carteira
	// (tpc_carteira/tpc_recargas/tpc_transacoes) foi migrada pra chavear por
	// senderzz_portal_users.id (portalUserID) — id nativo, sempre presente,
	// exclusivo por conta (sem colisão com o espaço de wp_user_id legado).
	token, expiresIn, err := middleware.MintJWT(portalUserID)
	if err != nil {
		slog.Error("[tpc_auth] falha ao emitir JWT", "err", err)
		httpx.WriteErr(w, http.StatusServiceUnavailable, "Configuração de autenticação ausente.")
		return
	}

	// Sucesso: reseta o contador de tentativas (delete_transient do PHP).
	h.rl.reset(ip)

	httpx.WriteOK(w, map[string]any{
		"token":      token,
		"user_id":    portalUserID,
		"nome":       firstNonEmpty(nome, email),
		"email":      email,
		"expires_in": expiresIn,
	})
}

// ── POST /auth/token-from-portal-session ─────────────────────────────────────

var sessionTokenRe = regexp.MustCompile(`^[a-f0-9]{64}$`)

// PostAuthTokenFromPortalSession gera um JWT a partir da sessão ativa do painel.
// Port FIEL de tpc_endpoint_auth_token_from_portal_session (rest-api.php:347).
//
// Fluxo:
//  1. Lê token do cookie senderzz_portal_session ou header X-Senderzz-Token.
//  2. Valida formato hex 64 chars (mesma regex do PHP).
//  3. Calcula HMAC-SHA256(token, WP_SALT_AUTH) — V-SEC-02; lookup aceita raw OU hash.
//  4. Valida expiração, status ativo e existência de wp_user_id.
//  5. Emite JWT sobre wp_user_id.
func (h *AuthHandler) PostAuthTokenFromPortalSession(w http.ResponseWriter, r *http.Request) {
	// Token: cookie tem prioridade; header X-Senderzz-Token é fallback (SPA).
	sessionToken := ""
	if c, err := r.Cookie("senderzz_portal_session"); err == nil {
		sessionToken = strings.TrimSpace(c.Value)
	}
	if sessionToken == "" {
		sessionToken = strings.TrimSpace(r.Header.Get("X-Senderzz-Token"))
	}

	if sessionToken == "" || !sessionTokenRe.MatchString(strings.ToLower(sessionToken)) {
		httpx.WriteErr(w, http.StatusUnauthorized, "Sessão do painel ausente ou inválida.")
		return
	}

	// V-SEC-02: sessões novas armazenam HMAC do token — comparar raw + hash.
	// hash_session_token() do PHP: hash_hmac('sha256', token, AUTH_SALT).
	sessionTokenHash, ok := hashSessionToken(sessionToken)
	if !ok {
		// WP_SALT_AUTH ausente — fail-closed (não validar HMAC com chave vazia).
		slog.Error("[tpc_auth] WP_SALT_AUTH não configurado — rejeitando sessão de painel")
		httpx.WriteErr(w, http.StatusServiceUnavailable, "Configuração de autenticação ausente.")
		return
	}

	// Lookup: o schema Postgres tem colunas separadas token (raw) e token_hmac.
	// Aceita raw OU hash em qualquer das duas colunas (compat sessões legacy).
	var (
		portalUserID    int64
		email           string
		nome            string
		role            string
		status          string
		wpUserID        *int64
		shippingClassID *int64
		expired         bool
	)
	err := h.db.QueryRow(r.Context(),
		`SELECT u.id, u.email, COALESCE(u.nome,''), COALESCE(u.role,'cliente'),
		        u.status, u.wp_user_id, u.shipping_class_id,
		        (s.expires_at IS NOT NULL AND s.expires_at < NOW()) AS expired
		   FROM senderzz_portal_sessions s
		   INNER JOIN senderzz_portal_users u ON u.id = s.user_id
		  WHERE s.token IN ($1, $2) OR s.token_hmac IN ($1, $2)
		  LIMIT 1`,
		sessionToken, sessionTokenHash,
	).Scan(&portalUserID, &email, &nome, &role, &status, &wpUserID, &shippingClassID, &expired)

	if err == pgx.ErrNoRows {
		httpx.WriteErr(w, http.StatusUnauthorized, "Sessão do painel inválida.")
		return
	}
	if err != nil {
		slog.Error("[tpc_auth] erro ao buscar sessão de painel", "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	if expired {
		httpx.WriteErr(w, http.StatusUnauthorized, "Sessão do painel expirada.")
		return
	}

	if status != "active" {
		httpx.WriteErr(w, http.StatusForbidden, "Usuário do painel inativo.")
		return
	}

	// MIGRAÇÃO 2026-07-28: carteira agora chaveia por portalUserID (id nativo do
	// portal), não mais wp_user_id — conta 100% FALK (sem WordPress) nunca tem
	// wp_user_id e ficava travada da carteira/PIX. Ver auth.go PostAuthLogin.
	token, expiresIn, err := middleware.MintJWT(portalUserID)
	if err != nil {
		slog.Error("[tpc_auth] falha ao emitir JWT da sessão de painel", "err", err)
		httpx.WriteErr(w, http.StatusServiceUnavailable, "Configuração de autenticação ausente.")
		return
	}

	scid := int64(0)
	if shippingClassID != nil {
		scid = *shippingClassID
	}

	httpx.WriteOK(w, map[string]any{
		"token":             token,
		"user_id":           portalUserID,
		"portal_user_id":    portalUserID,
		"shipping_class_id": scid,
		"nome":              firstNonEmpty(nome, email),
		"email":             email,
		"role":              firstNonEmpty(role, "client"),
		"expires_in":        expiresIn,
	})
}

// ── GET /me ───────────────────────────────────────────────────────────────────

// GetMe retorna o perfil do usuário autenticado (via JWT) + saldo da carteira.
// Espelha o conjunto de dados que o painel da carteira exibe após o login.
func (h *AuthHandler) GetMe(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == 0 {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	// MIGRAÇÃO 2026-07-28: userID do JWT agora É o id nativo do portal
	// (senderzz_portal_users.id), não mais wp_user_id.
	var (
		email string
		nome  string
		role  string
	)
	_ = h.db.QueryRow(r.Context(),
		`SELECT COALESCE(email,''), COALESCE(nome,''), COALESCE(role,'cliente')
		   FROM senderzz_portal_users
		  WHERE id = $1
		  LIMIT 1`,
		userID,
	).Scan(&email, &nome, &role)

	// Saldo: leitura simples da carteira (consistência eventual aceitável p/ perfil).
	saldo := decimal.Zero
	reservado := decimal.Zero
	var sStr, rStr string
	err := h.db.QueryRow(r.Context(),
		`SELECT saldo, saldo_reservado FROM tpc_carteira WHERE user_id = $1`,
		userID,
	).Scan(&sStr, &rStr)
	if err == nil {
		saldo, _ = decimal.NewFromString(sStr)
		reservado, _ = decimal.NewFromString(rStr)
	} else if err != pgx.ErrNoRows {
		slog.Error("[tpc_me] erro ao consultar carteira", "user_id", userID, "err", err)
	}
	disponivel := saldoDisponivel(saldo, reservado)

	httpx.WriteOK(w, map[string]any{
		"user_id":          userID,
		"nome":             firstNonEmpty(nome, email),
		"email":            email,
		"role":             firstNonEmpty(role, "client"),
		"saldo":            saldo.StringFixed(2),
		"saldo_reservado":  reservado.StringFixed(2),
		"saldo_disponivel": disponivel.StringFixed(2),
	})
}

// ── helpers ──────────────────────────────────────────────────────────────────

// firstNonEmpty retorna o primeiro argumento não-vazio (fallback de label).
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// hashSessionToken reproduz Portal_Auth::hash_session_token() do PHP:
// hash_hmac('sha256', token, AUTH_SALT). Lê WP_SALT_AUTH da env (equivalente a
// AUTH_SALT). Retorna (hash, false) se o salt estiver ausente — fail-closed.
func hashSessionToken(token string) (string, bool) {
	salt := os.Getenv("WP_SALT_AUTH")
	if salt == "" {
		return "", false
	}
	mac := hmac.New(sha256.New, []byte(salt))
	mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil)), true
}
