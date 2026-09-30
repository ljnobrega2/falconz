// CODE-HTTPX-02 — testes do formato canônico de resposta.
package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func decode(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("corpo não é JSON válido: %v — %s", err, body)
	}
	return m
}

func TestWriteOK_FormatoCanonico(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteOK(rec, map[string]any{"saldo": 100})

	if rec.Code != http.StatusOK {
		t.Errorf("status esperado=200, obtido=%d", rec.Code)
	}
	m := decode(t, rec.Body.Bytes())
	if m["ok"] != true {
		t.Errorf("esperava ok=true, obtido=%v", m["ok"])
	}
	if m["saldo"].(float64) != 100 {
		t.Errorf("campo extra perdido: saldo=%v", m["saldo"])
	}
}

func TestWriteOK_PayloadNil(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteOK(rec, nil)
	m := decode(t, rec.Body.Bytes())
	if m["ok"] != true {
		t.Errorf("payload nil deveria virar {ok:true}, obtido=%v", m)
	}
}

func TestWriteErr_FormatoCanonico(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteErr(rec, http.StatusBadRequest, "campo inválido")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status esperado=400, obtido=%d", rec.Code)
	}
	m := decode(t, rec.Body.Bytes())
	if m["ok"] != false {
		t.Errorf("esperava ok=false, obtido=%v", m["ok"])
	}
	if m["erro"] != "campo inválido" {
		t.Errorf("esperava erro=\"campo inválido\", obtido=%v", m["erro"])
	}
	if _, temError := m["error"]; temError {
		t.Errorf("formato canônico NÃO deve ter campo aninhado 'error': %v", m)
	}
}

func TestWriteErrDual_AmbosShapes(t *testing.T) {
	// Ponte de migração do admin: corpo carrega os DOIS formatos.
	rec := httptest.NewRecorder()
	WriteErrDual(rec, http.StatusUnauthorized, "token_invalido", "token inválido")

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status esperado=401, obtido=%d", rec.Code)
	}
	m := decode(t, rec.Body.Bytes())

	// Shape canônico.
	if m["ok"] != false || m["erro"] != "token inválido" {
		t.Errorf("shape canônico ausente: %v", m)
	}
	// Shape legado do admin (admin-ui lê body.error.message).
	errObj, ok := m["error"].(map[string]any)
	if !ok {
		t.Fatalf("shape legado 'error' ausente: %v", m)
	}
	if errObj["code"] != "token_invalido" || errObj["message"] != "token inválido" {
		t.Errorf("shape legado incorreto: %v", errObj)
	}
}

func TestWriteCreated_201(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteCreated(rec, map[string]any{"id": 7})
	if rec.Code != http.StatusCreated {
		t.Errorf("status esperado=201, obtido=%d", rec.Code)
	}
	m := decode(t, rec.Body.Bytes())
	if m["ok"] != true || m["id"].(float64) != 7 {
		t.Errorf("corpo inesperado: %v", m)
	}
}

func TestWriteNotImplemented_501(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteNotImplemented(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusNotImplemented {
		t.Errorf("status esperado=501, obtido=%d", rec.Code)
	}
	m := decode(t, rec.Body.Bytes())
	if m["ok"] != false {
		t.Errorf("esperava ok=false em 501, obtido=%v", m)
	}
}

func TestContentTypeJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteOK(rec, nil)
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type esperado JSON utf-8, obtido=%q", ct)
	}
}
