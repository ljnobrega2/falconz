package obs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

// RequestIDHeader é o cabeçalho HTTP de correlação. Mesmo nome usado pelo chi
// (chimw.RequestIDHeader = "X-Request-Id"), para interoperar com proxies/clients
// que já propagam esse header.
const RequestIDHeader = "X-Request-Id"

// ctxKey é o tipo da chave de contexto deste pacote — privado para evitar
// colisão com chaves de outros pacotes. Diferente da chave (também privada) do
// chi: por isso obs.GetRequestID e chimw.GetReqID NÃO são intercambiáveis.
type ctxKey int

const requestIDKey ctxKey = iota

// RequestID é o middleware de correlação de logs. Comportamento ("propaga"):
//
//   - se o cabeçalho X-Request-Id chega na requisição, REUTILIZA o valor;
//   - caso contrário, GERA um id novo (16 bytes aleatórios em hex);
//   - injeta o id no context.Context (recuperável via GetRequestID);
//   - ecoa o id no cabeçalho X-Request-Id da RESPOSTA (melhora sobre o chi, que
//     não escreve o header de volta).
//
// Assinatura func(http.Handler) http.Handler — plugável tanto em net/http
// stdlib quanto em chi (r.Use). Deve ser registrado ANTES dos middlewares que
// leem o request_id (Recover, access-log), senão eles veem id vazio.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set(RequestIDHeader, id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetRequestID devolve o request_id injetado por RequestID, ou "" se ausente.
// Usar em logs (slog) e na resposta de erro para correlacionar o trace.
func GetRequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// newRequestID gera um id de 32 hex chars (16 bytes via crypto/rand). Em caso de
// falha improvável do RNG, devolve um fallback fixo para nunca propagar id vazio.
func newRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "req-fallback"
	}
	return hex.EncodeToString(b)
}
