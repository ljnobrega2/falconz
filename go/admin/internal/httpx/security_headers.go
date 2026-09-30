// security_headers.go — middleware ADITIVO de cabeçalhos de segurança (defense-in-depth).
//
// AUDIT GO-HDR-01.
//
// Espelha go/shared/pkg/httpx/security_headers.go — NÃO importável direto porque o
// build Docker de cada serviço usa contexto per-serviço (../../go/admin), e o módulo
// shared (não publicado) ficaria fora do contexto / exigiria replace que quebra o
// `COPY . .` do Dockerfile. Manter em sincronia com a versão de shared.
//
// PROD: o nginx é o único ingress e já injeta estes headers → aqui é só
// defense-in-depth. DEV (cloudflared → Vite → Go, nginx fora do caminho): protege
// as RESPOSTAS Go (JSON/CSV); NÃO governa o documento HTML do painel (servido pelo
// Vite, não pelo Go) — fechar o clickjacking do HTML exige nginx à frente do túnel
// ou headers no Vite (fora deste escopo). O admin-service responde apenas JSON/CSV
// (nenhum text/html), então o CSP travado `default-src 'none'` é seguro.
package httpx

import "net/http"

const cspAPIDefault = "default-src 'none'; frame-ancestors 'none'; base-uri 'none'"
const hstsDefault = "max-age=63072000; includeSubDomains"

// SecurityHeaders devolve um middleware que adiciona cabeçalhos de segurança a
// todas as respostas (inclusive /healthz e preflights OPTIONS). Plugar logo após
// RealIP, antes de Recoverer/CORS.
func SecurityHeaders() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
			h.Set("Content-Security-Policy", cspAPIDefault)
			h.Set("Strict-Transport-Security", hstsDefault)
			next.ServeHTTP(w, r)
		})
	}
}
