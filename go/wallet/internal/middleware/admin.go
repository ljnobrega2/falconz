// admin.go — middleware de autorização ADMIN para as rotas /admin/* da carteira.
//
// Port FIEL do gate PHP `current_user_can('manage_woocommerce')`
// (includes/tpc/rest-api.php:67,73). No WordPress essas rotas exigem um ADMIN
// HUMANO logado (esquema OpenAPI: cookie wp_logged_in). No mundo Go não existe
// cookie WP — o análogo fiel de "admin humano logado" é o JWT de admin já emitido
// pelo serviço go/admin (auth.IssueToken): HS256, claim iss=senderzz-admin,
// secret ADMIN_JWT_SECRET (com fallback para JWT_SECRET).
//
// Por que admin-JWT e NÃO o HMAC interno (/internal/*):
//   - /internal/* (WALLET_INTERNAL_SECRET) modela um caller SERVIÇO-A-SERVIÇO
//     (double-write PHP→Go), não um admin humano — ator diferente.
//   - O verifyHMAC de internal.go assina o BODY; estas rotas são GET sem body
//     (HMAC("","")=constante) e o user_id vive no PATH (jamais coberto pela
//     assinatura) → uma assinatura capturada serviria para qualquer user_id.
//     Reusá-lo seria inseguro. O admin-JWT cobre user/exp/iss na assinatura.
//
// Liveness (revogação): além de assinatura+iss+exp, exige que o admin ainda esteja
// ATIVO — SELECT ... FROM senderzz_admin_users WHERE id=$1 AND ativo=TRUE — idêntico
// ao go/admin auth.Middleware. Sem isso, um JWT válido seria aceito por toda a TTL
// (12h) mesmo após o admin ser desativado, divergindo do gate PHP current_user_can
// (que reavalia a capacidade a cada request). senderzz_admin_users vive no MESMO
// banco senderzz que a carteira usa (infra/docker/docker-compose.yml: POSTGRES_DB
// único; tabela em infra/postgres/schema-admin.sql). Qualquer erro de lookup
// (admin inativo, inexistente, tabela ausente) → 401 (fail-closed).
//
// Fail-closed (SEC-GO-01 / espelha tpc_jwt_get_secret >= 32):
//   - secret ausente/curto  → 503 (não 401) — não revela rota sem auth.
//   - token inválido/sem iss → 401.
//   - iss != senderzz-admin  → 401 (impede privesc por confusão de token: um JWT
//     de usuário comum emitido por MintJWT NÃO carrega iss, então é recusado aqui
//     mesmo que assine com o mesmo JWT_SECRET — fecha o privesc usuário→admin).
//   - admin inativo/ausente  → 401 (revogação efetiva, espelha go/admin).
package middleware

import (
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/wallet-service/internal/httpx"
)

// adminJWTIssuer é o claim iss exigido — idêntico ao go/admin auth.IssueToken.
const adminJWTIssuer = "senderzz-admin"

// adminSecret resolve o secret do JWT de admin com o MESMO fallback do go/admin
// (auth.secret): ADMIN_JWT_SECRET tem prioridade; cai para JWT_SECRET se ausente.
func adminSecret() string {
	if s := os.Getenv("ADMIN_JWT_SECRET"); s != "" {
		return s
	}
	return os.Getenv("JWT_SECRET")
}

// AuthAdminJWT devolve o middleware chi que protege as rotas /admin/* da carteira.
//
// Espelha go/admin auth.Middleware (mesma assinatura/secret/issuer + lookup de
// liveness em senderzz_admin_users). Recebe o pool no closure (criado uma vez no
// main.go) para fazer o SELECT de revogação a cada request — o gate fiel ao PHP é
// "capacidade reavaliada por request", não só "token assinado".
func AuthAdminJWT(pool *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			secret := adminSecret()
			if len(secret) < 32 {
				// Configuração ausente/fraca — fail-closed para não processar sem auth.
				slog.Error("[auth_admin] ADMIN_JWT_SECRET/JWT_SECRET ausente ou curto — rejeitando requisição admin",
					"path", r.URL.Path,
					"ip", r.RemoteAddr,
				)
				httpx.WriteErr(w, http.StatusServiceUnavailable, "serviço temporariamente indisponível")
				return
			}

			authHeader := r.Header.Get("Authorization")
			if !strings.HasPrefix(authHeader, "Bearer ") {
				httpx.WriteErr(w, http.StatusUnauthorized, "token ausente ou formato inválido")
				return
			}
			tokenStr := strings.TrimPrefix(authHeader, "Bearer ")

			// Valida assinatura HS256 + iss=senderzz-admin + exp obrigatório.
			// WithIssuer rejeita tokens de usuário comum (MintJWT não seta iss) — fecha
			// o privesc usuário→admin mesmo quando ADMIN_JWT_SECRET == JWT_SECRET.
			token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, jwt.ErrSignatureInvalid
				}
				return []byte(secret), nil
			},
				jwt.WithValidMethods([]string{"HS256"}),
				jwt.WithIssuer(adminJWTIssuer), // SEC-GO-04
				jwt.WithExpirationRequired(),   // SEC-GO-09
			)

			if err != nil || !token.Valid {
				slog.Warn("[auth_admin] token admin inválido",
					"err", err,
					"path", r.URL.Path,
					"ip", r.RemoteAddr,
				)
				httpx.WriteErr(w, http.StatusUnauthorized, "token inválido ou expirado")
				return
			}

			// Extrai o claim "sub" (admin id). go/admin emite sub como número.
			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				httpx.WriteErr(w, http.StatusUnauthorized, "claims inválidas")
				return
			}
			subFloat, ok := claims["sub"].(float64)
			if !ok || subFloat <= 0 {
				httpx.WriteErr(w, http.StatusUnauthorized, "claim sub inválida")
				return
			}
			adminID := int64(subFloat)

			// Liveness/revogação: o admin precisa existir e estar ATIVO (espelha
			// go/admin auth.Middleware). Qualquer erro (inativo, inexistente, tabela
			// ausente) → 401 fail-closed; jamais segue para o handler.
			var exists int
			if err := pool.QueryRow(r.Context(),
				`SELECT 1 FROM senderzz_admin_users WHERE id = $1 AND ativo = TRUE`,
				adminID,
			).Scan(&exists); err != nil {
				slog.Warn("[auth_admin] admin inválido ou inativo",
					"admin_id", adminID,
					"path", r.URL.Path,
					"err", err,
				)
				httpx.WriteErr(w, http.StatusUnauthorized, "admin inválido")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
