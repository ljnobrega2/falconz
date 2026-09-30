// bodylimit.go — middleware ADITIVO de teto de tamanho do corpo da requisição.
//
// AUDIT DEPS-INPUT-01 (validação de input fraca / DoS por corpo ilimitado)
//
// # Problema
//
// Nenhum serviço Go usava http.MaxBytesReader. Todos os handlers decodificam
// json.NewDecoder(r.Body).Decode(...) sem teto de tamanho. Um cliente malicioso
// pode enviar um corpo arbitrariamente grande (ou um stream que nunca termina) e
// forçar alocação de memória ilimitada — negação de serviço trivial.
//
// # Solução
//
// LimitBody devolve um middleware chi/net-http que troca r.Body por
// http.MaxBytesReader. Quando o cliente excede o teto, o próximo Read no corpo
// devolve erro e a resposta vira 413. É ADITIVO: nenhuma assinatura existente
// muda, nenhum call-site precisa ser tocado — basta plugar no router.Use(...).
//
// # Uploads (multipart) preservados
//
// Rotas de upload (comprovantes de motoboy, comprovantes COD, etc.) usam
// r.ParseMultipartForm(N) com seu PRÓPRIO teto (10–32 MB). Aplicar um teto JSON
// pequeno no mesmo router quebraria esses uploads. Por isso LimitBody NÃO mexe
// no corpo quando o Content-Type é multipart/form-data — o ParseMultipartForm do
// handler continua sendo o limite efetivo do upload.
package httpx

import (
	"net/http"
	"strings"
)

// DefaultMaxBodyBytes — teto padrão para corpos NÃO-multipart (JSON, form-urlencoded).
// 1 MiB cobre com folga qualquer payload legítimo de API destes serviços.
const DefaultMaxBodyBytes int64 = 1 << 20 // 1 MiB

// LimitBody devolve um middleware que limita o tamanho do corpo da requisição a
// maxBytes para requisições NÃO-multipart. Se maxBytes <= 0, usa DefaultMaxBodyBytes.
//
// Uso (no início da cadeia de middlewares, depois de RealIP):
//
//	r.Use(httpx.LimitBody(httpx.DefaultMaxBodyBytes))
func LimitBody(maxBytes int64) func(http.Handler) http.Handler {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBodyBytes
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Uploads multipart têm seu próprio teto via ParseMultipartForm — não tocar.
			if ct := r.Header.Get("Content-Type"); strings.HasPrefix(ct, "multipart/") {
				next.ServeHTTP(w, r)
				return
			}
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}
