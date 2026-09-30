// mint.go — emissão de JWT no MESMO formato que tpc_jwt_encode() do PHP
// (includes/tpc/rest-api.php:125). Port FIEL: header {alg:HS256,typ:JWT},
// payload {sub:user_id,iat,exp}, todas as 3 partes em base64url SEM padding,
// assinatura HMAC-SHA256. Tokens emitidos aqui validam tanto no AuthJWT deste
// serviço quanto no tpc_jwt_decode() do WP (interoperabilidade total).
package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"os"
	"strconv"
	"time"
)

// AccessTokenTTL espelha o default do filtro tpc_jwt_access_token_ttl: HOUR_IN_SECONDS.
const AccessTokenTTL = time.Hour

// ErrSecretAusente é retornado quando JWT_SECRET está vazio ou curto demais.
// Espelha o fail-closed do PHP (tpc_jwt_get_secret exige >= 32 chars).
var ErrSecretAusente = errors.New("JWT_SECRET ausente ou curto demais")

// MintJWT gera um JWT HS256 idêntico ao tpc_jwt_encode($user_id) do PHP.
//
// Fail-closed (CRIT-05): exige JWT_SECRET com pelo menos 32 caracteres — o mesmo
// piso do tpc_jwt_get_secret(); secret curto → erro, nunca assina com chave fraca.
func MintJWT(userID int64) (string, int64, error) {
	secret := os.Getenv("JWT_SECRET")
	if len(secret) < 32 {
		return "", 0, ErrSecretAusente
	}

	now := time.Now().Unix()
	exp := now + int64(AccessTokenTTL/time.Second)

	// JSON construído manualmente para reproduzir exatamente o output do PHP:
	// wp_json_encode mantém a ordem das chaves e sem espaços. sub/iat/exp são
	// números inteiros (não strings) — o middleware AuthJWT lê sub como número.
	header := base64URL([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload := base64URL([]byte(
		`{"sub":` + strconv.FormatInt(userID, 10) +
			`,"iat":` + strconv.FormatInt(now, 10) +
			`,"exp":` + strconv.FormatInt(exp, 10) + `}`,
	))

	signingInput := header + "." + payload
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	sig := base64URL(mac.Sum(nil))

	return signingInput + "." + sig, int64(AccessTokenTTL / time.Second), nil
}

// base64URL aplica base64url sem padding (igual a tpc_base64url do PHP).
func base64URL(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}
