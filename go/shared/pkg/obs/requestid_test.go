package obs

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// RequestID gera um id quando a requisição não traz X-Request-Id, ecoa no header
// da resposta e o disponibiliza no contexto via GetRequestID.
func TestRequestID_GeneratesWhenAbsent(t *testing.T) {
	var got string
	h := RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = GetRequestID(r.Context())
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rec, req)

	if got == "" {
		t.Fatal("request_id no contexto vazio; esperava id gerado")
	}
	if hdr := rec.Header().Get(RequestIDHeader); hdr != got {
		t.Fatalf("header de resposta %q != id do contexto %q", hdr, got)
	}
	if len(got) != 32 { // 16 bytes em hex
		t.Fatalf("id gerado tem %d chars, esperava 32", len(got))
	}
}

// RequestID reutiliza o X-Request-Id que chega na requisição (propagação).
func TestRequestID_ReusesIncoming(t *testing.T) {
	const incoming = "id-de-upstream-123"
	var got string
	h := RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = GetRequestID(r.Context())
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(RequestIDHeader, incoming)
	h.ServeHTTP(rec, req)

	if got != incoming {
		t.Fatalf("request_id %q; esperava reutilizar %q", got, incoming)
	}
	if hdr := rec.Header().Get(RequestIDHeader); hdr != incoming {
		t.Fatalf("header de resposta %q; esperava %q", hdr, incoming)
	}
}

// GetRequestID devolve "" para contexto sem id ou nil (sem panic).
func TestGetRequestID_Empty(t *testing.T) {
	if id := GetRequestID(nil); id != "" {
		t.Fatalf("nil ctx: id %q; esperava vazio", id)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if id := GetRequestID(req.Context()); id != "" {
		t.Fatalf("ctx sem middleware: id %q; esperava vazio", id)
	}
}
