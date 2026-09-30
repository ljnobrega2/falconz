// security_headers.go — middleware ADITIVO de cabeçalhos de segurança (defense-in-depth).
//
// AUDIT GO-HDR-01 (serviços Go não emitem nenhum header de segurança)
//
// # Problema
//
// Nenhum serviço Go (admin/portal/motoboy/labels/affiliates/cron) emitia
// cabeçalhos de segurança de transporte. Em PRODUÇÃO o nginx é o único ingress
// e injeta esses headers, então a falta no Go é apenas defense-in-depth (P2).
// No stack DEV vivo (cloudflared → Vite → Go) o nginx fica FORA do caminho de
// request e a URL HTTPS pública serve PII real. ATENÇÃO ao escopo: este middleware
// protege as RESPOSTAS dos serviços Go (JSON/CSV) — defense-in-depth da superfície
// de API. Ele NÃO governa o documento HTML do painel: no dev esse HTML é servido
// pelo Vite (:5173), que só proxeia /wp-json para o Go; CSP/X-Frame-Options numa
// resposta de API consumida via fetch NÃO impedem o framing da página que a buscou.
// Fechar o clickjacking do HTML exige nginx à frente do túnel ou headers no Vite
// (fora deste escopo). Aqui o header genuinamente load-bearing na API é o nosniff.
//
// # Solução
//
// SecurityHeaders devolve um middleware chi/net-http que seta um conjunto fixo de
// cabeçalhos de segurança em TODA resposta (inclusive /health, /readyz e
// preflights OPTIONS). É ADITIVO: nenhuma assinatura existente muda, nenhum
// call-site precisa ser tocado — basta plugar no router logo após RealIP, de modo
// a embrulhar Recoverer/Timeout/CORS/handlers.
//
// # Por que estes headers
//
//   - X-Content-Type-Options: nosniff        → bloqueia MIME sniffing.
//   - X-Frame-Options: DENY                  → bloqueia framing (clickjacking) em
//     browsers legados; redundante com frame-ancestors do CSP mas barato.
//   - Referrer-Policy: no-referrer           → não vaza URL (com query/PII) ao sair.
//   - Permissions-Policy: geolocation=(), microphone=(), camera=()
//     → desliga APIs sensíveis do browser para qualquer página servida pelo Go.
//   - Content-Security-Policy: default-src 'none'; frame-ancestors 'none'; base-uri 'none'
//     → estes serviços servem APENAS JSON/CSV (nenhum text/html). 'none' é o teto
//     mais restritivo e frame-ancestors 'none' é a defesa moderna de clickjacking.
//     Se algum serviço passar a servir HTML, relaxe o CSP NAQUELA rota.
//   - Strict-Transport-Security: max-age=63072000; includeSubDomains
//     → browsers IGNORAM HSTS sobre HTTP simples, então emitir incondicionalmente
//     é inócuo atrás do nginx (TLS) e protege o caminho dev via cloudflared (HTTPS).
//
// CSP de API (sem HTML) — não precisa de 'self'/'unsafe-inline'; 'none' é seguro.
package httpx

import "net/http"

// cspAPIDefault — CSP travado para serviços que respondem só JSON/CSV.
const cspAPIDefault = "default-src 'none'; frame-ancestors 'none'; base-uri 'none'"

// hstsDefault — 2 anos + subdomínios. Sem preload (decisão de domínio fora do escopo).
const hstsDefault = "max-age=63072000; includeSubDomains"

// SecurityHeaders devolve um middleware que adiciona cabeçalhos de segurança a
// todas as respostas. Os headers são setados ANTES de chamar o próximo handler,
// de forma que já estejam no cabeçalho mesmo quando o handler escreve o corpo
// diretamente (os headers só são "comprometidos" no primeiro WriteHeader/Write).
//
// Uso (logo após RealIP, antes de Recoverer/CORS):
//
//	r.Use(httpx.SecurityHeaders())
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
