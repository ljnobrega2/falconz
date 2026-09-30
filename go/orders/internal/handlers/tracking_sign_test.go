// tracking_sign_test.go — AUDIT TEST-TRACKING-KEY-NO-ROUNDTRIP.
//
// Cobre os gates anti-enumeração de PII do rastreio público
// (GET /checkout-api/order/{code}): trackingSig, signedTrackingCode e
// verifyTrackingCode. Sem estes testes, uma regressão que afrouxasse a
// verificação (ex.: comparar prefixo, aceitar bare ID) reabriria a varredura
// sequencial de pedidos sem ser detectada.
//
// Não requer banco — todas as funções sob teste são puras (HMAC + string).
package handlers

import (
	"strings"
	"testing"
)

// withSalt define WP_SALT_AUTH para o teste e restaura o valor original ao fim.
func withSalt(t *testing.T, salt string) {
	t.Helper()
	// Limpa os fallbacks para garantir que só WP_SALT_AUTH esteja em jogo.
	t.Setenv("MOTOBOY_INTERNAL_SECRET", "")
	t.Setenv("TPC_WEBHOOK_SECRET", "")
	t.Setenv("WP_SALT_AUTH", salt)
}

// TestTrackingSig_DeterministicWithSalt: com salt, o sig é estável, maiúsculo e
// tem 10 chars hex. Seeds diferentes produzem sigs diferentes.
func TestTrackingSig_DeterministicWithSalt(t *testing.T) {
	withSalt(t, "salt-de-teste-ci")

	sig1 := trackingSig("SZ-0000052")
	sig2 := trackingSig("SZ-0000052")
	if sig1 == "" {
		t.Fatal("sig vazio com salt presente — esperado HMAC válido")
	}
	if sig1 != sig2 {
		t.Fatalf("sig não determinístico: %q != %q", sig1, sig2)
	}
	if len(sig1) != 10 {
		t.Fatalf("sig deve ter 10 chars hex, tem %d (%q)", len(sig1), sig1)
	}
	if sig1 != strings.ToUpper(sig1) {
		t.Fatalf("sig deve ser maiúsculo, veio %q", sig1)
	}
	if other := trackingSig("1584"); other == sig1 {
		t.Fatal("seeds diferentes não deveriam colidir no sig")
	}
}

// TestTrackingSig_NoSaltFailClosed: sem salt, sig vazio (fail-closed → todo
// rastreio cai em 404).
func TestTrackingSig_NoSaltFailClosed(t *testing.T) {
	withSalt(t, "")
	if sig := trackingSig("SZ-0000052"); sig != "" {
		t.Fatalf("sem salt o sig deve ser vazio (fail-closed), veio %q", sig)
	}
	if code := signedTrackingCode("SZ-0000052"); code != "" {
		t.Fatalf("sem salt o code assinado deve ser vazio, veio %q", code)
	}
}

// TestVerifyTrackingCode_Roundtrip: code emitido por signedTrackingCode é aceito
// e devolve o order_number original — inclusive com '-' no order_number.
func TestVerifyTrackingCode_Roundtrip(t *testing.T) {
	withSalt(t, "salt-de-teste-ci")

	for _, on := range []string{"SZ-0000052", "1584", "SZ-0000052-A"} {
		code := signedTrackingCode(on)
		if code == "" {
			t.Fatalf("code vazio para order_number %q", on)
		}
		got, ok := verifyTrackingCode(code)
		if !ok {
			t.Fatalf("roundtrip falhou para %q (code=%q)", on, code)
		}
		if got != on {
			t.Fatalf("order_number recuperado errado: esperado %q, veio %q", on, got)
		}
	}
}

// TestVerifyTrackingCode_RejectsBareID: um ID sequencial sem sig ('-') é rejeitado
// (bloqueia a varredura 1,2,3,…).
func TestVerifyTrackingCode_RejectsBareID(t *testing.T) {
	withSalt(t, "salt-de-teste-ci")

	for _, bare := range []string{"1584", "1", "52", ""} {
		if _, ok := verifyTrackingCode(bare); ok {
			t.Fatalf("bare ID %q NÃO deveria ser aceito (sem sig)", bare)
		}
	}
}

// TestVerifyTrackingCode_RejectsWrongSig: order_number correto + sig errado/adulterado
// é rejeitado.
func TestVerifyTrackingCode_RejectsWrongSig(t *testing.T) {
	withSalt(t, "salt-de-teste-ci")

	if _, ok := verifyTrackingCode("SZ-0000052-DEADBEEF12"); ok {
		t.Fatal("sig errado NÃO deveria ser aceito")
	}
	// Pega o code legítimo e altera o último char do sig.
	code := signedTrackingCode("1584")
	tampered := code[:len(code)-1]
	if last := code[len(code)-1]; last == 'A' {
		tampered += "B"
	} else {
		tampered += "A"
	}
	if _, ok := verifyTrackingCode(tampered); ok {
		t.Fatalf("sig adulterado NÃO deveria ser aceito (code=%q → %q)", code, tampered)
	}
}

// TestVerifyTrackingCode_NoSaltFailClosed: sem salt, mesmo um code bem-formado é
// rejeitado (nada é verificável → fail-closed).
func TestVerifyTrackingCode_NoSaltFailClosed(t *testing.T) {
	withSalt(t, "")
	if _, ok := verifyTrackingCode("SZ-0000052-ABCDEF1234"); ok {
		t.Fatal("sem salt nenhum code deveria ser aceito (fail-closed)")
	}
}

// TestVerifyTrackingCode_MalformedEdges: edges de split (só '-', '-' no início,
// nada após '-') são rejeitados sem panic.
func TestVerifyTrackingCode_MalformedEdges(t *testing.T) {
	withSalt(t, "salt-de-teste-ci")

	for _, code := range []string{"-", "-ABC", "1584-", "   "} {
		if _, ok := verifyTrackingCode(code); ok {
			t.Fatalf("code malformado %q NÃO deveria ser aceito", code)
		}
	}
}

// TestVerifyTrackingCode_CaseInsensitiveSig: o SIG (não o order_number) pode chegar
// minúsculo na URL e ainda assim ser aceito (verifyTrackingCode faz ToUpper só no sig).
func TestVerifyTrackingCode_CaseInsensitiveSig(t *testing.T) {
	withSalt(t, "salt-de-teste-ci")

	const on = "SZ-0000052"
	code := signedTrackingCode(on)
	// Minúscula APENAS a porção do sig (após o último '-'). O order_number é seed
	// do HMAC, então alterar seu case quebraria a verificação (comportamento correto).
	idx := strings.LastIndex(code, "-")
	lowerSig := code[:idx+1] + strings.ToLower(code[idx+1:])
	got, ok := verifyTrackingCode(lowerSig)
	if !ok {
		t.Fatalf("sig minúsculo deveria ser aceito (code=%q)", lowerSig)
	}
	if got != on {
		t.Fatalf("order_number errado no case-insensitive: %q", got)
	}
}
