// Package httpx fornece helpers de resposta JSON para todos os handlers.
// Contrato de resposta: {"ok": true, ...} / {"ok": false, "erro": "mensagem"}
package httpx

import (
	"encoding/json"
	"net/http"
)

// WriteOK serializa payload com ok=true e escreve status 200.
func WriteOK(w http.ResponseWriter, payload map[string]any) {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["ok"] = true
	writeJSON(w, http.StatusOK, payload)
}

// WriteErr serializa {"ok":false,"erro":msg} com o status HTTP fornecido.
func WriteErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"ok": false, "erro": msg})
}

// WriteNotImplemented responde 501 para rotas com stub ainda não implementado.
func WriteNotImplemented(w http.ResponseWriter, r *http.Request) {
	WriteErr(w, http.StatusNotImplemented, "não implementado")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
