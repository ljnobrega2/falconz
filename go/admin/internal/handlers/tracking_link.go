// AUDIT-2026-07-31 (dono: "ao clicar no botão copiar rastreio direciona para
// o link de rastreio falk") — botão "Copiar rastreio" no admin copiava só o
// código bruto da transportadora (ex.: "612148759"), não um link. Constrói o
// mesmo link público assinado que o cliente recebe no ThankYou/webhook
// (checkout-ui /rastreio/{code}), replicando a assinatura HMAC de
// go/orders/internal/handlers/tracking_sign.go (mesmo SALT, mesmo formato —
// precisa bater porque o código é verificado do outro lado por
// verifyTrackingCode). Módulo admin não importa orders — réplica local
// pequena e auto-contida, mesmo padrão já usado em portal/orders.
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

func getenvSignAdmin(names ...string) string {
	for _, n := range names {
		if v := strings.TrimSpace(os.Getenv(n)); v != "" {
			return v
		}
	}
	return ""
}

// trackingSigAdmin — mesma fórmula de trackingSig (orders-service):
// strtoupper(hex(hmac_sha256(orderNumber, SALT))[:10]).
func trackingSigAdmin(orderNumber string) string {
	salt := getenvSignAdmin("WP_SALT_AUTH", "MOTOBOY_INTERNAL_SECRET", "TPC_WEBHOOK_SECRET")
	if salt == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(salt))
	mac.Write([]byte(orderNumber))
	full := hex.EncodeToString(mac.Sum(nil))
	return strings.ToUpper(full[:10])
}

// publicTrackingLink monta a URL pública completa de rastreio pro pedido.
// "" se orderNumber vazio ou SALT ausente (fail-closed, mesmo padrão de
// signedTrackingCode).
func publicTrackingLink(orderNumber string) string {
	if orderNumber == "" {
		return ""
	}
	sig := trackingSigAdmin(orderNumber)
	if sig == "" {
		return ""
	}
	base := strings.TrimRight(getenvSignAdmin("CHECKOUT_PUBLIC_URL"), "/")
	if base == "" {
		base = "https://app.falklog.com.br"
	}
	return base + "/checkout/rastreio/" + orderNumber + "-" + sig
}
