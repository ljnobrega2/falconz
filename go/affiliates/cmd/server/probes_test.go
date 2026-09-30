// Testes dos probes de observabilidade: /health (liveness) e /readyz (readiness).
//
// OBSERVABILIDADE: o readyz é o probe canônico de readiness (pool.Ping → 200/503),
// separado do /health. Os handlers foram extraídos para receber um dbPinger,
// permitindo exercitar ambos os ramos (banco OK / banco fora) sem um Postgres real.
//
// Comentários em PT-BR conforme convenção do projeto.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakePinger implementa dbPinger devolvendo o erro configurado.
type fakePinger struct{ err error }

func (f fakePinger) Ping(_ context.Context) error { return f.err }

// TestReadyzPronto: banco respondendo Ping → 200 + {"ok":true,"status":"ready"}.
func TestReadyzPronto(t *testing.T) {
	rec := httptest.NewRecorder()
	readyzHandler(fakePinger{err: nil}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("readyz com banco OK: status = %d, esperado 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		OK     bool   `json:"ok"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("readyz: resposta não é JSON válido: %v (body=%s)", err, rec.Body.String())
	}
	if !resp.OK || resp.Status != "ready" {
		t.Errorf("readyz OK: esperava ok=true status=ready, obteve ok=%v status=%q", resp.OK, resp.Status)
	}
}

// TestReadyzBancoForaRetorna503: Ping falhando → 503 (não pronto para tráfego).
func TestReadyzBancoForaRetorna503(t *testing.T) {
	rec := httptest.NewRecorder()
	readyzHandler(fakePinger{err: errors.New("connection refused")}).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz com banco fora: status = %d, esperado 503; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("readyz 503: resposta não é JSON válido: %v (body=%s)", err, rec.Body.String())
	}
	if resp.OK {
		t.Errorf("readyz com banco fora deveria responder ok=false, obteve ok=true")
	}
}

// TestHealthPreservaComportamento: /health continua fazendo Ping → 200/503
// (separação semântica de /readyz, mas ambos checam o banco; /health não pode regredir).
func TestHealthPreservaComportamento(t *testing.T) {
	// Banco OK → 200.
	recOK := httptest.NewRecorder()
	healthHandler(fakePinger{err: nil}).ServeHTTP(recOK, httptest.NewRequest(http.MethodGet, "/health", nil))
	if recOK.Code != http.StatusOK {
		t.Errorf("health com banco OK: status = %d, esperado 200", recOK.Code)
	}

	// Banco fora → 503.
	recDown := httptest.NewRecorder()
	healthHandler(fakePinger{err: errors.New("down")}).ServeHTTP(recDown, httptest.NewRequest(http.MethodGet, "/health", nil))
	if recDown.Code != http.StatusServiceUnavailable {
		t.Errorf("health com banco fora: status = %d, esperado 503", recDown.Code)
	}
}
