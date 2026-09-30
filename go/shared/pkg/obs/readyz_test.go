package obs

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakePinger implementa Pinger devolvendo um erro fixo (nil = saudável).
type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

// Readyz responde 200 {ok:true,status:ready} quando o Ping é bem-sucedido.
func TestReadyz_OK(t *testing.T) {
	h := Readyz(fakePinger{err: nil}, "wallet")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d; esperava 200", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("corpo não-JSON: %v", err)
	}
	if ok, _ := body["ok"].(bool); !ok {
		t.Fatalf("ok=%v; esperava true", body["ok"])
	}
	if body["status"] != "ready" {
		t.Fatalf("status=%v; esperava 'ready'", body["status"])
	}
	if body["service"] != "wallet" {
		t.Fatalf("service=%v; esperava 'wallet'", body["service"])
	}
}

// Readyz responde 503 {ok:false,erro} quando o Ping falha.
func TestReadyz_Unavailable(t *testing.T) {
	h := Readyz(fakePinger{err: errors.New("dial recusado")}, "orders")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d; esperava 503", rec.Code)
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
