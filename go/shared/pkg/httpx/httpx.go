// Package httpx — helpers de resposta JSON CANÔNICOS para todos os serviços Go.
//
// CODE-ARCH-01 / CODE-HTTPX-02
//
// # Problema (CODE-HTTPX-02)
//
// Hoje existem DOIS formatos de erro divergentes entre os serviços:
//
//	Portal  / Orders / Motoboy / Wallet  → {"ok":false,"erro":"mensagem"}  (e {"ok":true,...})
//	Admin                                → {"error":{"code":"x","message":"y"}}
//
// Quatro dos cinco serviços (e o front do Portal V2) já consomem o envelope
// {"ok","erro"}. Por isso ESTE é o formato canônico — migrar 4 serviços para o
// formato do admin quebraria o front do portal e exigiria muito mais reescrita.
//
// # Formato canônico
//
//	Sucesso : {"ok":true, ...campos...}                 HTTP 2xx
//	Erro    : {"ok":false,"erro":"mensagem em PT-BR"}   HTTP 4xx/5xx
//
// O envelope é o mesmo de github.com/senderzz/shared/pkg/contract.Response
// (campos OK + Erro). Este pacote serializa o envelope direto em map[string]any
// para permitir campos extras no sucesso (ex.: 422 {ok:false, erro, qr_required}).
//
// # Migração do admin (NÃO quebrar o front)
//
// O front admin (admin-ui/src/api.ts) lê o erro como `body?.error?.message`.
// Logo, trocar o admin direto para {"ok","erro"} QUEBRARIA o SPA. A migração é
// feita em duas etapas, com WriteErrDual como ponte:
//
//  1. Backend admin troca httpx.Err(...) por httpx.WriteErrDual(...), que emite
//     AMBOS os shapes no mesmo corpo: {"ok":false,"erro":msg,"error":{...}}.
//     O front antigo continua lendo .error.message; nada quebra.
//  2. admin-ui passa a ler `body?.erro` (igual aos demais serviços).
//  3. Quando o front estiver migrado, o admin troca WriteErrDual por WriteErr e
//     o campo "error" aninhado some — convergência total no formato canônico.
//
// Enquanto isso, os outros 4 serviços já adotam WriteOK/WriteErr diretamente.
package httpx

import (
	"encoding/json"
	"net/http"
)

// WriteJSON serializa v como JSON com o status fornecido. Base dos demais helpers.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteOK serializa o payload com ok=true e escreve HTTP 200.
// payload nil vira objeto vazio. Campos extras do chamador são preservados.
func WriteOK(w http.ResponseWriter, payload map[string]any) {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["ok"] = true
	WriteJSON(w, http.StatusOK, payload)
}

// WriteCreated é igual a WriteOK porém com HTTP 201 (recurso criado).
func WriteCreated(w http.ResponseWriter, payload map[string]any) {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["ok"] = true
	WriteJSON(w, http.StatusCreated, payload)
}

// WriteErr serializa {"ok":false,"erro":msg} com o status HTTP fornecido.
// Formato canônico de erro — adotado por portal/orders/motoboy/wallet.
func WriteErr(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, ErrBody(msg))
}

// WriteNotImplemented responde 501 para rotas com stub ainda não implementado.
func WriteNotImplemented(w http.ResponseWriter, r *http.Request) {
	WriteErr(w, http.StatusNotImplemented, "não implementado")
}

// ErrBody devolve o corpo canônico de erro como map (testável sem ResponseWriter).
func ErrBody(msg string) map[string]any {
	return map[string]any{"ok": false, "erro": msg}
}

// ── Ponte de migração do admin (CODE-HTTPX-02) ───────────────────────────────

// WriteErrDual emite o erro nos DOIS shapes no mesmo corpo:
//
//	{"ok":false,"erro":msg,"error":{"code":code,"message":msg}}
//
// É a ponte temporária para migrar o serviço admin sem quebrar o front que ainda
// lê body.error.message. Remover assim que admin-ui passar a ler body.erro.
func WriteErrDual(w http.ResponseWriter, status int, code, msg string) {
	WriteJSON(w, status, DualErrBody(code, msg))
}

// DualErrBody devolve o corpo de erro com ambos os shapes (canônico + legado admin).
func DualErrBody(code, msg string) map[string]any {
	return map[string]any{
		"ok":   false,
		"erro": msg,
		"error": map[string]string{ // shape legado do admin — remover pós-migração do front
			"code":    code,
			"message": msg,
		},
	}
}

// DecodeJSON decodifica o corpo da requisição em out. Conveniência para handlers.
func DecodeJSON(r *http.Request, out any) error {
	return json.NewDecoder(r.Body).Decode(out)
}
