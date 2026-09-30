// Package middleware fornece middlewares HTTP para o serviço de carteira.
//
// AuthJWT extrai o user_id do token Bearer (HS256) e o injeta no contexto.
// Compatível com os tokens emitidos pelo PHP via tpc_jwt_encode(), que usa
// claim "sub" = user_id e "exp" como timestamp Unix.
//
// Fail-closed: secret vazio retorna 503 (não 401) para evitar expor
// que o serviço está mal configurado. Secret presente mas token inválido = 401.
package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/senderzz/wallet-service/internal/httpx"
)

// ctxKeyUserID é a chave de contexto para o user_id autenticado.
type ctxKeyUserID struct{}

// GetUserID recupera o user_id injetado pelo middleware AuthJWT.
// Retorna 0 se o contexto não tiver sido populado (não deve ocorrer em rotas protegidas).
func GetUserID(ctx context.Context) int64 {
	v, _ := ctx.Value(ctxKeyUserID{}).(int64)
	return v
}

// WithUserID devolve um contexto com o user_id injetado — simétrico a GetUserID.
// É o MESMO valor que AuthJWT grava após validar o token; expô-lo permite que
// testes de integração exercitem os handlers protegidos sem forjar um JWT válido
// (a validação de token já é coberta pelos testes de AuthJWT). NÃO altera nenhum
// fluxo de produção: o pipeline HTTP continua populando o contexto só via AuthJWT.
func WithUserID(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, ctxKeyUserID{}, userID)
}

// AuthJWT é um middleware chi que valida o token Bearer HS256 emitido pelo WP.
//
// Fluxo:
//  1. Lê JWT_SECRET da env — se vazio retorna 503 (fail-closed).
//  2. Extrai o token do header "Authorization: Bearer <token>".
//  3. Valida assinatura e exp com golang-jwt.
//  4. Extrai claim "sub" (int64) e injeta no contexto.
func AuthJWT(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secret := os.Getenv("JWT_SECRET")
		if secret == "" {
			// Configuração ausente — fail-closed para não processar requests sem auth.
			slog.Error("[auth] JWT_SECRET não configurado — rejeitando requisição",
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
		//
		// SEC-GO-02: exige claim exp presente (WithExpirationRequired). O minter PHP
		// (tpc_jwt_encode) SEMPRE seta exp e o decoder PHP (tpc_jwt_decode) recusa
		// tokens sem exp — espelhamos esse fail-closed aqui para que um token sem exp
		// não seja aceito como "nunca expira". Behavior-preserving para tokens válidos.
		// Obs.: WithIssuer NÃO é aplicado de propósito — os tokens PHP não carregam
		// claim iss; exigi-lo rejeitaria todo token legado. Correção pertence ao minter.
		token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
			// Garante que o algoritmo seja exatamente HS256 — rejeita RS256 etc.
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return []byte(secret), nil
		}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())

		if err != nil || !token.Valid {
			slog.Warn("[auth] token JWT inválido",
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

		// "sub" no PHP é o user_id int, serializado como número JSON.
		subRaw, exists := claims["sub"]
		if !exists {
			httpx.WriteErr(w, http.StatusUnauthorized, "claim sub ausente")
			return
		}

		// jwt.MapClaims decodifica números JSON como float64.
		subFloat, ok := subRaw.(float64)
		if !ok || subFloat <= 0 {
			httpx.WriteErr(w, http.StatusUnauthorized, "claim sub inválida")
			return
		}

		userID := int64(subFloat)
		ctx := context.WithValue(r.Context(), ctxKeyUserID{}, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
