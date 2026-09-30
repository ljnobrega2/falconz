package obs

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Recover converte um panic comum em 500 com o envelope canônico {ok:false,erro}.
func TestRecover_PanicTo500(t *testing.T) {
	h := Recover(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d; esperava 500", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("corpo não-JSON: %v", err)
	}
	if ok, _ := body["ok"].(bool); ok {
		t.Fatalf("ok=%v; esperava false", body["ok"])
	}
	if _, has := body["erro"]; !has {
		t.Fatalf("corpo sem campo 'erro': %v", body)
	}
}

// Recover NÃO engole http.ErrAbortHandler — re-panica (convenção do net/http).
func TestRecover_RepanicsAbortHandler(t *testing.T) {
	h := Recover(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	defer func() {
		rec := recover()
		if rec != http.ErrAbortHandler {
			t.Fatalf("recuperou %v; esperava re-panic de http.ErrAbortHandler", rec)
		}
	}()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rec, req)
	t.Fatal("ServeHTTP retornou sem re-panicar ErrAbortHandler")
}

// Sem panic, Recover é transparente: status e corpo do handler passam intactos.
func TestRecover_PassThrough(t *testing.T) {
	h := Recover(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("ok"))
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusTeapot {
		t.Fatalf("status %d; esperava 418 (passthrough)", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Fatalf("corpo %q; esperava 'ok'", rec.Body.String())
	}
}
