// tracking_sign.go — Token ASSINADO anti-enumeração + máscara de PII do rastreio.
//
// PROBLEMA (VAZAMENTO de PII): o rastreio público GET /checkout-api/order/{code}
// resolvia o pedido por order_number/id SEQUENCIAL. Como order_number == id em
// pedidos migrados (ex.: 1584), bastava varrer 1, 2, 3, … para enumerar TODOS os
// pedidos e raspar nome/telefone/CPF/e-mail/endereço de cada cliente.
//
// SOLUÇÃO:
//
//  1. TOKEN ASSINADO: o code público passa a ser "<order_number>-<sig>", com
//     sig = strtoupper(hex(hmac_sha256(order_number, SALT))[:10]). O endpoint GET
//     SEPARA order_number e sig (split no ÚLTIMO '-', pois order_number pode conter
//     '-' como em "SZ-0000052"), RECOMPUTA o sig e REJEITA (404) se não bater —
//     ANTES de qualquer consulta ao banco. Sem o sig correto não há varredura
//     sequencial: um bare "1584" (sem '-' ou com sig errado) devolve 404.
//
//     O POST /checkout-api/order retorna esse code em "tracking_code" para o
//     front montar o link de rastreio sem precisar da chave HMAC.
//
//  2. MÁSCARA de PII no JSON do rastreio: cpf/telefone/email mascarados. Nome e
//     endereço permanecem (o cliente precisa conferir a entrega).
//
// Chave HMAC: env (WP_SALT_AUTH / MOTOBOY_INTERNAL_SECRET / TPC_WEBHOOK_SECRET —
// getenvSign com fallback, mesmo padrão de motoboy_etiquetas.go / AuthPortal).
package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
)

// getenvSign retorna o primeiro env var não-vazio entre os nomes fornecidos.
// (Réplica local do getenv do módulo admin — não há helper compartilhado no
// módulo orders.)
func getenvSign(names ...string) string {
	for _, n := range names {
		if v := strings.TrimSpace(os.Getenv(n)); v != "" {
			return v
		}
	}
	return ""
}

// trackingSig calcula a assinatura anti-enumeração de um order_number.
//
// sig = strtoupper( hex( hmac_sha256(order_number, SALT) )[:10] ).
//
// IMPORTANTE: o seed é o order_number como STRING EXATA ("SZ-0000052" ou "1584").
// O mesmo valor é assinado na emissão (POST) e na verificação (GET) — só assim o
// split-no-último-'-' + recompute é simétrico. Retorna "" se SALT vazio (sig
// inverificável ⇒ todo rastreio cai em 404, fail-closed).
func trackingSig(orderNumber string) string {
	salt := getenvSign("WP_SALT_AUTH", "MOTOBOY_INTERNAL_SECRET", "TPC_WEBHOOK_SECRET")
	if salt == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(salt))
	mac.Write([]byte(orderNumber))
	full := hex.EncodeToString(mac.Sum(nil)) // 64 chars hex
	return strings.ToUpper(full[:10])
}

// signedTrackingCode monta o code público assinado "<order_number>-<sig>".
// Retorna "" se a assinatura não puder ser gerada (SALT ausente).
func signedTrackingCode(orderNumber string) string {
	sig := trackingSig(orderNumber)
	if sig == "" {
		return ""
	}
	return orderNumber + "-" + sig
}

// verifyTrackingCode separa o code recebido em order_number + sig, recomputa o
// sig sobre o order_number e compara em tempo constante.
//
// Split no ÚLTIMO '-': o order_number pode conter '-' ("SZ-0000052"), mas o sig
// (hex maiúsculo) nunca contém — então o último '-' delimita order_number | sig.
//
// Retorna (orderNumber, true) só quando o sig confere. Qualquer falha (sem '-',
// sig vazio, SALT ausente, sig errado) ⇒ ("", false) — o caller deve responder o
// MESMO 404 de "pedido não encontrado" para não vazar existência.
func verifyTrackingCode(code string) (string, bool) {
	code = strings.TrimSpace(code)
	idx := strings.LastIndex(code, "-")
	if idx <= 0 || idx >= len(code)-1 {
		// Sem '-', '-' no início, ou nada depois do '-' ⇒ inválido.
		// (Bare "1584" sequencial cai aqui — varredura bloqueada.)
		return "", false
	}
	orderNumber := code[:idx]
	gotSig := strings.ToUpper(code[idx+1:]) // URLs podem chegar minúsculas.

	wantSig := trackingSig(orderNumber)
	if wantSig == "" {
		// SALT ausente — fail-closed: nada é verificável.
		return "", false
	}
	if !hmac.Equal([]byte(gotSig), []byte(wantSig)) {
		return "", false
	}
	return orderNumber, true
}

// ── Máscara de PII ───────────────────────────────────────────────────────────

// maskCPF mascara um CPF preservando 3 primeiros + dígito 8 + 2 últimos.
// Ex.: "39000000705" → "390.***.**7-05". Se não tiver 11 dígitos, cai no
// formatCPF padrão (não há o que mascarar de forma previsível).
func maskCPF(cpf string) string {
	d := onlyDigits(cpf)
	if len(d) != 11 {
		return formatCPF(cpf)
	}
	return d[0:3] + ".***.**" + d[8:9] + "-" + d[9:11]
}

// maskPhonePtr mascara um telefone NACIONAL (já sem +55 — aplicar APÓS
// normalizePhone). Mantém DDD + 4 últimos dígitos; o miolo vira '*'.
//
// Ex.: "11976864006" (11 díg, celular) → "(11) *****-4006"
//
//	"1133336603"  (10 díg, fixo)     → "(11) ****-6603"
//
// Retorna o ponteiro original se nil ou se não houver dígitos suficientes para
// mascarar com segurança (mantém o comportamento defensivo do rastreio).
func maskPhonePtr(p *string) *string {
	if p == nil {
		return nil
	}
	d := onlyDigits(*p)
	if len(d) < 7 {
		// Curto demais para mascarar mantendo DDD + 4 finais — preserva como veio.
		return p
	}
	masked := "(" + d[0:2] + ") " + strings.Repeat("*", len(d)-6) + "-" + d[len(d)-4:]
	return &masked
}

// maskEmailPtr mascara um e-mail preservando os 3 primeiros chars do local part
// e o domínio inteiro. Ex.: "lucas@gmail.com" → "luc***@gmail.com".
//
// Se o local part tiver < 3 chars, mascara o que houver + '***' (não expõe o
// local inteiro). Sem '@' válido, devolve "***" (não vaza o valor cru).
// Retorna nil se o ponteiro for nil.
func maskEmailPtr(p *string) *string {
	if p == nil {
		return nil
	}
	e := strings.TrimSpace(*p)
	if e == "" {
		return p
	}
	at := strings.LastIndex(e, "@")
	if at <= 0 || at >= len(e)-1 {
		// Sem '@' utilizável — não devolve o valor cru.
		masked := "***"
		return &masked
	}
	local := e[:at]
	domain := e[at:] // inclui o '@'
	prefix := local
	if len(local) > 3 {
		prefix = local[:3]
	}
	masked := prefix + "***" + domain
	return &masked
}
