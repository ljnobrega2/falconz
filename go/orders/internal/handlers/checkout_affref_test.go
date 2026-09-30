package handlers

import (
	"encoding/base64"
	"strings"
	"testing"
)

// encodeAffRefLikePortal replica EXATAMENTE o appendRefToken de
// go/portal/internal/handlers/links_portal.go (e o WP sz_aff_encode_ref_token):
//
//	packed = pack('N', id) . salt[:4]
//	token  = rtrim(strtr(base64(packed), '+/', '-_'), '=')
//
// É a contraparte de produção do decodeAffRefToken — o teste prova o round-trip.
func encodeAffRefLikePortal(id uint32, salt string) string {
	packed := []byte{byte(id >> 24), byte(id >> 16), byte(id >> 8), byte(id)}
	saltPrefix := salt
	if len(saltPrefix) > 4 {
		saltPrefix = saltPrefix[:4]
	}
	packed = append(packed, []byte(saltPrefix)...)
	token := base64.StdEncoding.EncodeToString(packed)
	token = strings.ReplaceAll(token, "+", "-")
	token = strings.ReplaceAll(token, "/", "_")
	return strings.TrimRight(token, "=")
}

// TestDecodeAffRefTokenRoundTrip prova que decodeAffRefToken inverte fielmente o
// encode do portal para uma variedade de ids de vínculo e de salts. É o invariante
// financeiro: o afiliado creditado tem que ser EXATAMENTE quem o ?r= aponta.
func TestDecodeAffRefTokenRoundTrip(t *testing.T) {
	ids := []uint32{1, 2, 42, 127, 128, 255, 256, 1000, 65535, 65536, 16777215, 16777216, 2147483647}
	// Salts de comprimentos diferentes — o sufixo NÃO pode afetar o id decodificado.
	salts := []string{"", "a", "ab", "abc", "abcd", "abcdef0123456789", "9f3c1aee"}

	for _, salt := range salts {
		for _, id := range ids {
			tok := encodeAffRefLikePortal(id, salt)
			got, ok := decodeAffRefToken(tok)
			if !ok {
				t.Fatalf("id=%d salt=%q: decode falhou para token %q", id, salt, tok)
			}
			if got != int64(id) {
				t.Fatalf("id=%d salt=%q: round-trip quebrou — got=%d token=%q", id, salt, got, tok)
			}
		}
	}
}

// TestDecodeAffRefTokenRejectsGarbage garante que entradas malformadas degradam
// para (0,false) — o chamador trata como "sem afiliado" e NUNCA quebra o pedido.
func TestDecodeAffRefTokenRejectsGarbage(t *testing.T) {
	bad := []string{
		"",          // vazio
		"   ",       // só espaço
		"!!!",       // não-base64
		"AAA",       // 3 bytes base64 → <4 bytes decodificados? "AAA" inválido sem padding handling → testa robustez
		"AA",        // poucos bytes
		"@#$%^&*",   // lixo
		"\x00\x01",  // controle
	}
	for _, b := range bad {
		if id, ok := decodeAffRefToken(b); ok {
			t.Fatalf("token lixo %q deveria falhar, mas retornou id=%d ok=true", b, id)
		}
	}
}

// TestDecodeAffRefTokenZeroIDRejected — id=0 não é vínculo válido; deve falhar
// (evita creditar um afiliado inexistente por um token de id zero).
func TestDecodeAffRefTokenZeroIDRejected(t *testing.T) {
	tok := encodeAffRefLikePortal(0, "abcd")
	if id, ok := decodeAffRefToken(tok); ok {
		t.Fatalf("id=0 deveria ser rejeitado, mas retornou id=%d ok=true", id)
	}
}
