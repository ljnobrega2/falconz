// Package middleware fornece middlewares HTTP para o serviço de labels.
//
// AuthJWT extrai o user_id do token Bearer (HS256) e o injeta no contexto.
// Compatível com os tokens emitidos pelo PHP via tpc_jwt_encode(), que usa
// claim "sub" = user_id e "exp" como timestamp Unix.
//
// SEC-GO-09: WithExpirationRequired — token sem claim exp não é aceito como
// eterno. golang-jwt v5 NÃO exige exp por padrão; um token validamente assinado
// porém sem exp passaria para sempre. O decoder PHP (tpc_jwt_decode) já rejeita
// payload sem exp, então exigir aqui apenas alinha o comportamento — todos os
// emissores reais (tpc_jwt_encode, shared/auth.Emit, admin.IssueToken) definem exp.
//
// AUDIT-2026-06-21 #HIGH-1: escopo de dono APLICADO. wc_me_labels passou a ter
//   owner_user_id (migração 440-labels-owner-scope.sql, backfill via sz_orders.
//   produtor_id). Os handlers de etiqueta (GetLabel/GetLabels/DeleteLabel e a
//   checagem de idempotência do PostLabel) filtram por owner_user_id = GetUserID(ctx)
//   para não-admins; o admin (claim role="admin", emitido por admin.IssueToken) vê
//   tudo. Id fora de escopo retorna 404 (não vaza existência). Este middleware agora
//   propaga o claim "role" no contexto (GetRole) para essa decisão. O equivalente
//   PHP (src/Rest/Labels.php) restringe tudo a `manage_woocommerce`; aqui o produtor
//   dono enxerga só as próprias etiquetas e o admin enxerga todas.
//
//   O claim "role" é OPCIONAL: tokens sem role (emissores que ainda não o definem)
//   resolvem para "" → tratados como não-admin (fail-closed: escopados ao próprio
//   user_id, nunca elevados a admin).
//
// Fail-closed: JWT_SECRET vazio retorna 503 (não 401) para evitar expor
// que o serviço está mal configurado. Secret presente mas token inválido = 401.
//
// Espelha middleware/auth.go do wallet-service.
package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/senderzz/labels-service/internal/httpx"
)

// ctxKeyUserID é a chave de contexto para o user_id autenticado.
type ctxKeyUserID struct{}

// ctxKeyRole é a chave de contexto para o claim "role" do JWT (AUDIT-2026-06-21 #HIGH-1).
type ctxKeyRole struct{}

// GetUserID recupera o user_id injetado pelo middleware AuthJWT.
// Retorna 0 se o contexto não tiver sido populado (não deve ocorrer em rotas protegidas).
func GetUserID(ctx context.Context) int64 {
	v, _ := ctx.Value(ctxKeyUserID{}).(int64)
	return v
}

// GetRole recupera o claim "role" injetado pelo middleware AuthJWT.
// AUDIT-2026-06-21 #HIGH-1: "" quando o token não traz role (fail-closed → não-admin).
// Apenas role == "admin" (emitido por admin.IssueToken) recebe visão global de etiquetas.
func GetRole(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyRole{}).(string)
	return v
}

// IsAdmin é o predicado central de elevação: true só para o claim role="admin".
// Centralizado para que os handlers não repliquem a string literal.
func IsAdmin(ctx context.Context) bool {
	return GetRole(ctx) == "admin"
}

// AuthJWT é um middleware chi que valida o token Bearer HS256 emitido pelo WP.
//
// Fluxo:
//  1. Lê JWT_SECRET da env — se vazio retorna 503 (fail-closed).
//  2. Extrai o token do header "Authorization: Bearer <token>".
//  3. Valida assinatura, algoritmo (HS256) e exp (obrigatório) com golang-jwt.
//  4. Extrai claim "sub" (int64) e injeta no contexto.
func AuthJWT(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secret := os.Getenv("JWT_SECRET")
		if secret == "" {
			// Configuração ausente — fail-closed para não processar requests sem auth.
			slog.Error("[senderzz_labels] JWT_SECRET não configurado — rejeitando requisição",
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

		// Valida e parseia o token HS256.
		// O PHP emite: header={alg:HS256,typ:JWT}, payload={sub:user_id,iat:...,exp:...}
		token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
			// Garante que o algoritmo seja exatamente HS256 — rejeita RS256 etc.
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return []byte(secret), nil
		},
			jwt.WithValidMethods([]string{"HS256"}),
			// SEC-GO-09: não aceita token sem exp como eterno (golang-jwt v5 não
			// exige exp por padrão; o decoder PHP tpc_jwt_decode já o exige).
			jwt.WithExpirationRequired(),
		)

		if err != nil || !token.Valid {
			slog.Warn("[senderzz_labels] token JWT inválido",
				"err", err,
				"path", r.URL.Path,
				"ip", r.RemoteAddr,
			)
			httpx.WriteErr(w, http.StatusUnauthorized, "token inválido ou expirado")
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			httpx.WriteErr(w, http.StatusUnauthorized, "claims inválidas")
			return
		}

		// "sub" no PHP legado é o user_id serializado como número JSON (float64 no
		// MapClaims). Os emissores Go (go/portal EmitJWT) usam jwt.RegisteredClaims.
		// Subject, que o encoding/json sempre serializa como STRING (RFC 7519 —
		// StringOrURI) — aceita os dois formatos, senão todo token de portal falha
		// aqui com 401 (e o front interpreta 401 como sessão expirada = desloga).
		subRaw, exists := claims["sub"]
		if !exists {
			httpx.WriteErr(w, http.StatusUnauthorized, "claim sub ausente")
			return
		}

		var userID int64
		switch v := subRaw.(type) {
		case float64:
			userID = int64(v)
		case string:
			parsed, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				httpx.WriteErr(w, http.StatusUnauthorized, "claim sub inválida")
				return
			}
			userID = parsed
		default:
			httpx.WriteErr(w, http.StatusUnauthorized, "claim sub inválida")
			return
		}
		if userID <= 0 {
			httpx.WriteErr(w, http.StatusUnauthorized, "claim sub inválida")
			return
		}
		ctx := context.WithValue(r.Context(), ctxKeyUserID{}, userID)

		// AUDIT-2026-06-21 #HIGH-1: propaga o claim "role" (opcional) no contexto para
		// a decisão de escopo de dono nos handlers (admin vê tudo; demais escopados).
		// Ausente/tipo inesperado → "" → tratado como não-admin (fail-closed).
		if roleRaw, ok := claims["role"]; ok {
			if roleStr, ok := roleRaw.(string); ok {
				ctx = context.WithValue(ctx, ctxKeyRole{}, roleStr)
			}
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
