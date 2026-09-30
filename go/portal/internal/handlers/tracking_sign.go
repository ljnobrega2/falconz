// tracking_sign.go — DUPLICATA PROPOSITAL de go/orders/internal/handlers/tracking_sign.go
// (módulos não se importam entre si, convenção do projeto). Só o necessário pra
// MONTAR o link assinado do rastreio público a partir do portal (produtor).
//
// AUDIT-2026-07-30 (dono: "ao invés de copiar o rastreio deve clicar nele e
// abrir o rastreamento falk") — botão "Copiar rastreio" na tela Expedição do
// portal vira "Abrir rastreio", levando direto pra página pública
// /checkout/rastreio/<order_number>-<sig>.
package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
)

const melhorRastreioBaseURL = "https://app.melhorrastreio.com.br"

func melhorRastreioLink(carrier string, trackingCodes []string) string {
	carrier = strings.ToLower(strings.TrimSpace(carrier))
	if !strings.Contains(carrier, "loggi") && !strings.Contains(carrier, "correios") {
		return ""
	}
	for _, rawCode := range trackingCodes {
		if code := strings.TrimSpace(rawCode); code != "" {
			return melhorRastreioBaseURL + "/" + url.PathEscape(code)
		}
	}
	return ""
}

func getenvSignPortal(names ...string) string {
	for _, n := range names {
		if v := strings.TrimSpace(os.Getenv(n)); v != "" {
			return v
		}
	}
	return ""
}

// signedTrackingCode monta "<order_number>-<sig>" — MESMO algoritmo/salt de
// go/orders (WP_SALT_AUTH), pra bater com a verificação em GetOrderTracking.
// Retorna "" se o salt não estiver configurado (nunca gera link quebrado).
func signedTrackingCode(orderNumber string) string {
	salt := getenvSignPortal("WP_SALT_AUTH", "MOTOBOY_INTERNAL_SECRET", "TPC_WEBHOOK_SECRET")
	if salt == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(salt))
	mac.Write([]byte(orderNumber))
	full := hex.EncodeToString(mac.Sum(nil))
	return orderNumber + "-" + strings.ToUpper(full[:10])
}
