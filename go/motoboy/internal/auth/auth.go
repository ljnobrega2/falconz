// Package auth implementa os middlewares de autenticação dos 4 grupos do OpenAPI:
//
//   - motoboy_token  → Header X-MB-Token / Authorization: Bearer
//                      (lookup em sz_motoboys.token_app, que guarda o HASH
//                       HMAC-SHA256(token_raw, WP_SALT_AUTH) — AUDIT #6)
//   - alan_token     → Header X-Alan-Token (token estático em env ALAN_TOKEN)
//   - portal_session → Cookie senderzz_portal_session ou Header X-Senderzz-Token
//                      (token HMAC-SHA256 com WP_SALT_AUTH, lookup em senderzz_portal_sessions)
//   - público        → sem auth
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"log/slog"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/motoboy-service/internal/httpx"
)

// ── Context keys ─────────────────────────────────────────────────────────────

// Tipos opacos para context keys — evita colisões com pacotes externos.
type ctxKeyMotoboy struct{}
type ctxKeyPortalUser struct{}

// Motoboy representa o motoboy autenticado extraído do banco.
type Motoboy struct {
	ID     int64
	CdID   int64
	ZonaID *int64
	Nome   string
}

// PortalUser representa o usuário de portal autenticado via sessão.
type PortalUser struct {
	ID   int64
	Role string
}

// ── Helpers de extração de contexto ─────────────────────────────────────────

// MotoboyfromCtx retorna o Motoboy autenticado do contexto, ou nil.
func MotoboyfromCtx(ctx context.Context) *Motoboy {
	v, _ := ctx.Value(ctxKeyMotoboy{}).(*Motoboy)
	return v
}

// PortalUserFromCtx retorna o PortalUser autenticado do contexto, ou nil.
func PortalUserFromCtx(ctx context.Context) *PortalUser {
	v, _ := ctx.Value(ctxKeyPortalUser{}).(*PortalUser)
	return v
}

// ── Middlewares ───────────────────────────────────────────────────────────────

// AuthMotoboy valida o header X-MB-Token contra sz_motoboys.token_app no banco.
// Falha com 401 se token ausente, inválido ou motoboy inativo.
// O Motoboy autenticado é armazenado no contexto via ctxKeyMotoboy.
func AuthMotoboy(pool *pgxpool.Pool) func(http.Handler) http.Handler {
	// AUDIT-2026-06-21 #6: o token_app é guardado HASHEADO no banco —
	// HMAC-SHA256(token_raw, WP_SALT_AUTH) em hex. Mesma derivação do PHP
	// (sz_mb_hash_token_app em rest-api.php). O app envia o RAW; aqui hasheamos
	// e fazemos lookup pelo hash, aceitando também o RAW legado durante a transição.
	wpSalt := os.Getenv("WP_SALT_AUTH")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := r.Header.Get("X-MB-Token")
			// AUDIT-2026-06-21 #7: aceita também Authorization: Bearer (PWA envia ambos),
			// nunca query param.
			if token == "" {
				if h := r.Header.Get("Authorization"); len(h) > 7 && (h[:7] == "Bearer " || h[:7] == "bearer ") {
					token = h[7:]
				}
			}
			if token == "" {
				httpx.WriteErr(w, http.StatusUnauthorized, "token ausente")
				return
			}
			if wpSalt == "" {
				slog.Error("[auth] WP_SALT_AUTH não configurado — auth de motoboy rejeitada (fail-closed)")
				httpx.WriteErr(w, http.StatusServiceUnavailable, "serviço não configurado")
				return
			}

			tokenHash := hmacSHA256Hex(token, wpSalt)

			var mb Motoboy
			// QA-FIX P0: sz_motoboys.ativo É boolean no Postgres (o schema já migrou).
			// `ativo = 1` lançava "operator does not exist: boolean = integer" → a
			// query falhava → middleware 401 → app do motoboy inteiro fora após login.
			err := pool.QueryRow(r.Context(),
				`SELECT id, cd_id, zona_id, nome
				   FROM sz_motoboys
				  WHERE token_app IN ($1, $2) AND ativo = true
				  LIMIT 1`,
				tokenHash, token,
			).Scan(&mb.ID, &mb.CdID, &mb.ZonaID, &mb.Nome)
			if err != nil {
				slog.Warn("[auth] token_app inválido ou motoboy inativo", "err", err)
				httpx.WriteErr(w, http.StatusUnauthorized, "não autorizado")
				return
			}

			ctx := context.WithValue(r.Context(), ctxKeyMotoboy{}, &mb)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// AuthAlan valida o header X-Alan-Token contra a variável de ambiente ALAN_TOKEN.
//
// ENV-REQUIRED, fail-closed (SEGREDOS/AUTH-CONFIG): o segredo vem EXCLUSIVAMENTE
// de os.Getenv("ALAN_TOKEN") — sem fallback literal/hardcoded. Se a env estiver
// vazia, TODA requisição Alan recebe 503 (nunca "aberto por engano"). A
// comparação é constant-time (subtle.ConstantTimeCompare) para não vazar o
// segredo por timing.
//
// LIMITAÇÃO conhecida (P3, design): ALAN_TOKEN é um token único e compartilhado
// — autentica todas as chamadas de expedição sem identidade por chamador nem
// rotação parcial. Endurecimento futuro: tokens por-operador (lookup em tabela,
// como X-MB-Token). Mitigação atual: o gate fail-closed acima é o único controle
// de acesso à PII de expedição (nome/endereço do destinatário nas etiquetas).
// Por isso ele NUNCA pode degradar para "aberto" — manter o 503 em env vazia.
func AuthAlan() func(http.Handler) http.Handler {
	expectedToken := os.Getenv("ALAN_TOKEN")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if expectedToken == "" {
				slog.Error("[auth] ALAN_TOKEN não configurado — todas as requisições Alan serão rejeitadas")
				httpx.WriteErr(w, http.StatusServiceUnavailable, "serviço não configurado")
				return
			}
			token := r.Header.Get("X-Alan-Token")
			if subtle.ConstantTimeCompare([]byte(token), []byte(expectedToken)) != 1 {
				httpx.WriteErr(w, http.StatusUnauthorized, "não autorizado")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// AuthPortal valida a sessão do portal via cookie senderzz_portal_session
// ou header X-Senderzz-Token.
//
// Esquema (espelhado de src/Portal/Portal_Auth.php):
//   - O cookie contém o token_raw (hex aleatório).
//   - O banco armazena token_hash = HMAC-SHA256(token_raw, WP_SALT_AUTH).
//   - Por compatibilidade de migração o banco pode conter o token_raw também.
//   - A query aceita ambos: WHERE token IN (token_raw, token_hash).
//
// Requer env var WP_SALT_AUTH (equivalente a AUTH_SALT do WordPress).
func AuthPortal(pool *pgxpool.Pool) func(http.Handler) http.Handler {
	wpSalt := os.Getenv("WP_SALT_AUTH")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if wpSalt == "" {
				slog.Error("[auth] WP_SALT_AUTH não configurado — todas as sessões portal serão rejeitadas")
				httpx.WriteErr(w, http.StatusServiceUnavailable, "serviço não configurado")
				return
			}

			// Lê token: cookie tem prioridade, header é fallback para clientes SPA.
			tokenRaw := ""
			if c, err := r.Cookie("senderzz_portal_session"); err == nil {
				tokenRaw = c.Value
			}
			if tokenRaw == "" {
				tokenRaw = r.Header.Get("X-Senderzz-Token")
			}
			if tokenRaw == "" {
				httpx.WriteErr(w, http.StatusUnauthorized, "sessão ausente")
				return
			}

			tokenHash := hmacSHA256Hex(tokenRaw, wpSalt)

			// NOTA: nomes de tabela incluem o prefixo WP padrão (wp_).
			// pgloader preserva os nomes originais do MySQL. Se o prefixo WP for diferente
			// de "wp_", ajustar as constantes abaixo.
			// Tabelas: wp_senderzz_portal_sessions, wp_senderzz_portal_users.
			var user PortalUser
			err := pool.QueryRow(r.Context(),
				`SELECT u.id, COALESCE(u.role, '') AS role
				   FROM wp_senderzz_portal_sessions s
				   JOIN wp_senderzz_portal_users u ON u.id = s.user_id
				  WHERE s.token IN ($1, $2)
				    AND s.expires_at > NOW()
				    AND u.status = 'active'
				  LIMIT 1`,
				tokenRaw, tokenHash,
			).Scan(&user.ID, &user.Role)
			if err != nil {
				slog.Warn("[auth] sessão portal inválida ou expirada", "err", err)
				httpx.WriteErr(w, http.StatusUnauthorized, "sessão inválida ou expirada")
				return
			}

			ctx := context.WithValue(r.Context(), ctxKeyPortalUser{}, &user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAuth é um middleware genérico que retorna 401 se nenhum principal
// autenticado (Motoboy OU PortalUser) estiver no contexto.
// Usar apenas quando uma rota aceita múltiplos esquemas de auth.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if MotoboyfromCtx(r.Context()) == nil && PortalUserFromCtx(r.Context()) == nil {
			httpx.WriteErr(w, http.StatusUnauthorized, "não autorizado")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireRole retorna 403 se o PortalUser autenticado não tiver um dos papéis
// permitidos. Deve ser encadeado APÓS AuthPortal (que popula o PortalUser).
// SEC-GO-02: fecha o anti-padrão "qualquer sessão portal" nas rotas /ol/*.
func RequireRole(roles ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		allowed[role] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := PortalUserFromCtx(r.Context())
			if u == nil {
				httpx.WriteErr(w, http.StatusUnauthorized, "não autorizado")
				return
			}
			if _, ok := allowed[u.Role]; !ok {
				httpx.WriteErr(w, http.StatusForbidden, "acesso restrito ao operador logístico")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ── Helpers internos ──────────────────────────────────────────────────────────

func hmacSHA256Hex(message, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}
