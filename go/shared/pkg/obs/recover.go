package obs

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/senderzz/shared/pkg/httpx"
)

// Recover é a rede de proteção contra panics nos handlers: captura o panic,
// loga com slog estruturado (mensagem + stack trace + request_id) e responde
// 500 no envelope canônico {"ok":false,"erro":...} via httpx.WriteErr.
//
// Substitui o chimw.Recoverer (que despeja o stack cru em stderr, sem
// request_id e fora do nosso JSON). Deve ficar DEPOIS de RequestID (para ter o
// request_id) e, idealmente, depois do access-log (para o 500 ser capturado).
//
// http.ErrAbortHandler NÃO é tratado como erro: é o sinal idiomático do Go para
// abortar a resposta (ex.: reverse proxy) e deve continuar propagando — por isso
// é re-panicado, como faz o Recoverer do chi.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			// Deixa o abort idiomático seguir adiante.
			if rec == http.ErrAbortHandler {
				panic(rec)
			}
			slog.ErrorContext(r.Context(), "[recover] panic no handler",
				"panic", rec,
				"request_id", GetRequestID(r.Context()),
				"method", r.Method,
				"path", r.URL.Path,
				"stack", string(debug.Stack()),
			)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		}()
		next.ServeHTTP(w, r)
	})
}
