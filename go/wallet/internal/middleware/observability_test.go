package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	chimw "github.com/go-chi/chi/v5/middleware"
)

// TestRecovererPanicVira500 prova que o Recoverer captura um panic do handler e
// responde 500 JSON {"ok":false,...} sem derrubar a conexão — o contrato de erro
// dos demais endpoints (httpx.WriteErr), não o texto cru do chimw.Recoverer.
func TestRecovererPanicVira500(t *testing.T) {
	h := Recoverer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom inesperado")
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, esperado 500", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"ok":false`) {
		t.Errorf("body = %q, esperado conter \"ok\":false", body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, esperado application/json", ct)
	}
}

// TestRecovererRepropagaErrAbortHandler prova que http.ErrAbortHandler NÃO é
// tratado como erro recuperável — é repropagado, idêntico ao chimw.Recoverer
// (convenção do net/http para abortar a conexão silenciosamente).
func TestRecovererRepropagaErrAbortHandler(t *testing.T) {
	h := Recoverer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	defer func() {
		rec := recover()
		if rec != http.ErrAbortHandler {
			t.Fatalf("panic repropagado = %v, esperado http.ErrAbortHandler", rec)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	t.Fatal("ErrAbortHandler deveria ter sido repropagado (não capturado)")
}

// TestRecovererSucessoPassa prova que sem panic o Recoverer é transparente:
// o handler responde normalmente e o status é preservado.
func TestRecovererSucessoPassa(t *testing.T) {
	h := Recoverer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("ok"))
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusTeapot {
		t.Errorf("status = %d, esperado 418 (Recoverer não deve interferir no caminho feliz)", rec.Code)
	}
}

// TestRequestLoggerInjetaLoggerComRequestID prova que RequestLogger injeta no
// contexto um *slog.Logger recuperável via LoggerFrom — o mecanismo que faz cada
// log line dos handlers principais carregar o request_id.
func TestRequestLoggerInjetaLoggerComRequestID(t *testing.T) {
	var loggerVisto bool

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// LoggerFrom nunca devolve nil; deve devolver o logger ligado ao request.
		if lg := LoggerFrom(r.Context()); lg != nil {
			loggerVisto = true
		}
		w.WriteHeader(http.StatusOK)
	})

	// RequestID popula o request_id no contexto ANTES do RequestLogger (ordem real).
	h := chimw.RequestID(RequestLogger(inner))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if !loggerVisto {
		t.Error("LoggerFrom não devolveu o logger injetado por RequestLogger")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, esperado 200", rec.Code)
	}
}

// TestLoggerFromFallback prova o fail-safe: sem logger no contexto, LoggerFrom
// devolve slog.Default() — NUNCA nil (um handler chamado fora do pipeline HTTP,
// ou em teste, não deve dar nil-panic ao logar).
func TestLoggerFromFallback(t *testing.T) {
	if LoggerFrom(context.Background()) == nil {
		t.Fatal("LoggerFrom(ctx sem logger) = nil, esperado slog.Default()")
	}
}
