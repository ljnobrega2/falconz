// observability.go — middlewares de observabilidade do serviço de carteira.
//
// Três peças, todas slog-estruturado (JSON), consistentes com o handler padrão
// definido em cmd/server/main.go:
//
//  1. RequestLogger — substitui o chimw.Logger (que emite texto não-JSON). Para
//     cada request: gera/propaga request_id (via chimw.RequestID, que deve rodar
//     ANTES), injeta no contexto um *slog.Logger já com "request_id" e loga uma
//     linha de acesso ao final (método, rota, status, latência, bytes).
//
//  2. LoggerFrom — recupera o *slog.Logger ligado ao request (com request_id).
//     Os handlers principais usam-no para que CADA log line carregue o request_id
//     sem precisar repassá-lo manualmente. Fallback: slog.Default().
//
//  3. Recoverer — recupera panics, loga stack em nível ERROR (com request_id) e
//     responde 500 JSON {"ok":false,"erro":...}. Substitui o chimw.Recoverer para
//     que o 500 saia no mesmo formato JSON dos demais erros (httpx.WriteErr) e o
//     log carregue o request_id. NÃO altera nenhuma lógica de negócio — só captura
//     falhas inesperadas que de outra forma derrubariam a conexão.
//
// Ordem obrigatória em main.go (preservada):
//
//	RequestID → RealIP → RequestLogger → Recoverer → rotas
//
// RequestLogger DEVE ficar FORA (antes) do Recoverer para que um panic recuperado
// como 500 ainda gere a linha de acesso com o status correto.
package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/senderzz/wallet-service/internal/httpx"
)

// ctxKeyLogger é a chave de contexto para o *slog.Logger ligado ao request.
type ctxKeyLogger struct{}

// LoggerFrom recupera o *slog.Logger injetado por RequestLogger (já com o campo
// "request_id"). Se o contexto não tiver um logger (ex: chamada fora do pipeline
// HTTP, ou teste), devolve slog.Default() — nunca nil.
func LoggerFrom(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(ctxKeyLogger{}).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}

// RequestLogger é um middleware chi que loga cada requisição em JSON estruturado
// e injeta no contexto um *slog.Logger pré-anotado com o request_id.
//
// Deve rodar DEPOIS de chimw.RequestID (para que GetReqID já esteja populado) e
// ANTES do Recoverer (para registrar o acesso mesmo quando o handler entra em
// panic e é recuperado como 500).
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		reqID := chimw.GetReqID(r.Context())

		// Logger ligado ao request: todo log line dos handlers que usarem
		// LoggerFrom(ctx) carrega o request_id automaticamente.
		reqLogger := slog.Default()
		if reqID != "" {
			reqLogger = reqLogger.With("request_id", reqID)
		}
		ctx := context.WithValue(r.Context(), ctxKeyLogger{}, reqLogger)

		// WrapResponseWriter captura status e bytes escritos (igual ao chimw.Logger).
		ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)

		next.ServeHTTP(ww, r.WithContext(ctx))

		reqLogger.LogAttrs(r.Context(), slog.LevelInfo, "[http] request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", ww.Status()),
			slog.Int("bytes", ww.BytesWritten()),
			slog.Int64("latency_ms", time.Since(start).Milliseconds()),
			slog.String("remote_ip", r.RemoteAddr),
		)
	})
}

// Recoverer captura panics nos handlers, loga em nível ERROR (com request_id e
// stack trace) e responde 500 JSON. Substitui o chimw.Recoverer para emitir o
// erro no mesmo contrato JSON dos demais (httpx.WriteErr) e anexar o request_id.
//
// http.ErrAbortHandler é repropagado (convenção do net/http: aborta a conexão
// silenciosamente sem ser tratado como erro real) — idêntico ao chimw.Recoverer.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler {
					// Não é um erro recuperável — net/http espera que se repropague.
					panic(rec)
				}
				LoggerFrom(r.Context()).Error("[http] panic recuperado",
					"panic", rec,
					"method", r.Method,
					"path", r.URL.Path,
					"stack", string(debug.Stack()),
				)
				httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
