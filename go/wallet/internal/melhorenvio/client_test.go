package melhorenvio

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestGerarPixSemToken garante que a falta de ME_TOKEN retorna ErrSemToken
// (erro claro, nunca panic) — requisito do porte em dev.
func TestGerarPixSemToken(t *testing.T) {
	c := &Client{apiBase: defaultAPIBase, token: ""}
	_, err := c.GerarPix(context.Background(), 50.0, "https://x/y")
	if !errors.Is(err, ErrSemToken) {
		t.Fatalf("esperado ErrSemToken, obtido: %v", err)
	}
}

// TestFindFirstString valida o porte de tpc_pix_find_first: busca rasa,
// primeira chave não-vazia, depois recursão em sub-mapas.
func TestFindFirstString(t *testing.T) {
	data := map[string]any{
		"foo": "",
		"bar": "valor-bar",
		"nested": map[string]any{
			"deep_id": "abc123",
		},
	}
	if got := findFirstString(data, []string{"foo", "bar"}); got != "valor-bar" {
		t.Errorf("findFirstString rasa: got %q, esperado 'valor-bar'", got)
	}
	if got := findFirstString(data, []string{"deep_id"}); got != "abc123" {
		t.Errorf("findFirstString recursiva: got %q, esperado 'abc123'", got)
	}
	// Número convertido para string (id numérico do gateway).
	num := map[string]any{"id": float64(98765)}
	if got := findFirstString(num, []string{"id"}); got != "98765" {
		t.Errorf("findFirstString numérico: got %q, esperado '98765'", got)
	}
	// Chave ausente → "".
	if got := findFirstString(data, []string{"inexistente"}); got != "" {
		t.Errorf("findFirstString ausente: got %q, esperado ''", got)
	}
}

// TestExpandJSONStrings valida o porte de tpc_pix_expand_json_strings:
// strings que são JSON aninhado são re-decodificadas, e a busca encontra
// chaves dentro delas.
func TestExpandJSONStrings(t *testing.T) {
	data := map[string]any{
		"payment": `{"transaction_id":"tx-42","qr_code":"https://img/qr.png"}`,
	}
	expanded := expandJSONStrings(data).(map[string]any)
	if got := findFirstString(expanded, []string{"transaction_id"}); got != "tx-42" {
		t.Errorf("expandJSONStrings: transaction_id got %q, esperado 'tx-42'", got)
	}
	if got := findFirstString(expanded, []string{"qr_code"}); got != "https://img/qr.png" {
		t.Errorf("expandJSONStrings: qr_code got %q", got)
	}
}

// TestNormalizeImgSrc valida o porte de tpc_pix_normalize_img_src.
func TestNormalizeImgSrc(t *testing.T) {
	// data:image passa direto.
	if got := normalizeImgSrc("data:image/png;base64,AAAA"); got != "data:image/png;base64,AAAA" {
		t.Errorf("data:image: got %q", got)
	}
	// URL http passa direto.
	if got := normalizeImgSrc("https://x/qr.png"); got != "https://x/qr.png" {
		t.Errorf("url: got %q", got)
	}
	// base64 longo (>80) vira data URI.
	b64 := strings.Repeat("A", 100)
	got := normalizeImgSrc(b64)
	if !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Errorf("base64 longo: got %q, esperado prefixo data:image", got)
	}
	// string curta/lixo → "".
	if got := normalizeImgSrc("xyz"); got != "" {
		t.Errorf("lixo curto: got %q, esperado ''", got)
	}
}

// TestQrFromCopyPaste valida o porte de tpc_pix_qr_from_copy_paste.
func TestQrFromCopyPaste(t *testing.T) {
	got := qrFromCopyPaste("00020126BR.GOV.BCB.PIX")
	if !strings.HasPrefix(got, "https://api.qrserver.com/v1/create-qr-code/") {
		t.Errorf("qrFromCopyPaste: got %q", got)
	}
	if qrFromCopyPaste("") != "" {
		t.Error("qrFromCopyPaste('') deveria retornar ''")
	}
}

// TestParseTimestamp valida o porte de tpc_pix_timestamp (string vazia → 0).
func TestParseTimestamp(t *testing.T) {
	if parseTimestamp("") != 0 {
		t.Error("parseTimestamp('') deveria ser 0")
	}
	if parseTimestamp("lixo-nao-data") != 0 {
		t.Error("parseTimestamp(lixo) deveria ser 0")
	}
	if parseTimestamp("2026-06-18T12:00:00Z") <= 0 {
		t.Error("parseTimestamp(ISO) deveria ser > 0")
	}
}
