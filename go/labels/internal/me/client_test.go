// SEC-LABELS: testes unitários dos helpers PUROS do cliente Melhor Envio.
//
// Nenhum destes testes faz I/O de rede — em DEV não há token ME e a API externa
// NÃO deve ser chamada. Cobrimos apenas lógica determinística: o guard SSRF da
// base URL (P2-02), o parsing da resposta de geração de etiqueta (objeto vs array),
// a geração determinística da chave de cache e o truncamento seguro de mensagens.
package me

import (
	"strings"
	"testing"
)

// ─── validateMEBaseURL — P2-02 SSRF guard (vetores mais fortes) ───────────────

func TestValidateMEBaseURL(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
		wantErr bool
	}{
		{"host oficial https", "https://melhorenvio.com.br/api/v2", false},
		{"host www https", "https://www.melhorenvio.com.br/api/v2", false},
		{"sandbox https", "https://sandbox.melhorenvio.com.br/api/v2", false},
		{"host oficial com porta", "https://melhorenvio.com.br:443/api/v2", false},
		{"host maiusculas (case-insensitive)", "https://MelhorEnvio.COM.BR/api/v2", false},

		{"vazia", "", true},
		{"scheme http (sem TLS) recusado", "http://melhorenvio.com.br/api/v2", true},
		{"host arbitrario recusado (exfil token)", "https://evil.example.com/api/v2", true},
		{"subdominio nao-whitelisted recusado", "https://api.melhorenvio.com.br/api/v2", true},
		{"host parecido nao whitelisted", "https://melhorenvio.com.br.evil.com/api", true},
		{"scheme ausente", "melhorenvio.com.br/api/v2", true},
		{"url malformada", "https://%zz", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateMEBaseURL(tc.baseURL)
			if tc.wantErr && err == nil {
				t.Fatalf("validateMEBaseURL(%q): esperava erro, obteve nil", tc.baseURL)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validateMEBaseURL(%q): esperava nil, obteve %v", tc.baseURL, err)
			}
		})
	}
}

// ─── pickLabelURL — parsing de resposta ME (objeto vs array) ──────────────────

func TestPickLabelURL(t *testing.T) {
	cases := []struct {
		name string
		resp meGenerateResponse
		want string
	}{
		{
			name: "objeto unico (campo label no topo)",
			resp: meGenerateResponse{ID: "abc", LabelURL: "https://me/label/top.pdf"},
			want: "https://me/label/top.pdf",
		},
		{
			name: "array shipments (topo vazio)",
			resp: meGenerateResponse{
				Shipments: []struct {
					ID       string `json:"id"`
					LabelURL string `json:"label"`
				}{
					{ID: "s1", LabelURL: "https://me/label/arr.pdf"},
				},
			},
			want: "https://me/label/arr.pdf",
		},
		{
			name: "topo tem prioridade sobre array",
			resp: meGenerateResponse{
				LabelURL: "https://me/label/top.pdf",
				Shipments: []struct {
					ID       string `json:"id"`
					LabelURL string `json:"label"`
				}{
					{ID: "s1", LabelURL: "https://me/label/arr.pdf"},
				},
			},
			want: "https://me/label/top.pdf",
		},
		{
			name: "vazio total → string vazia",
			resp: meGenerateResponse{},
			want: "",
		},
		{
			name: "array com primeiro vazio → vazio (não varre o resto)",
			resp: meGenerateResponse{
				Shipments: []struct {
					ID       string `json:"id"`
					LabelURL string `json:"label"`
				}{
					{ID: "s1", LabelURL: ""},
				},
			},
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pickLabelURL(tc.resp); got != tc.want {
				t.Fatalf("pickLabelURL: esperava %q, obteve %q", tc.want, got)
			}
		})
	}
}

// ─── CalculateCacheKey — determinismo ─────────────────────────────────────────

func TestCalculateCacheKeyDeterministic(t *testing.T) {
	req := CalcRequest{
		FromCEP: "01001000",
		ToCEP:   "20010000",
		Products: []CalcProduct{
			{Height: 11, Width: 15, Length: 20, Weight: 0.3, Quantity: 1},
		},
	}

	k1 := CalculateCacheKey(req)
	k2 := CalculateCacheKey(req)
	if k1 != k2 {
		t.Fatalf("CalculateCacheKey não determinístico: %q != %q", k1, k2)
	}
	// SHA-256 hex tem 64 caracteres.
	if len(k1) != 64 {
		t.Fatalf("CalculateCacheKey: esperava 64 chars (sha256 hex), obteve %d (%q)", len(k1), k1)
	}

	// Entrada diferente → chave diferente.
	req2 := req
	req2.ToCEP = "30110000"
	if CalculateCacheKey(req2) == k1 {
		t.Fatalf("CalculateCacheKey: entradas distintas geraram a mesma chave")
	}
}

// ─── truncate — limite seguro de mensagens ────────────────────────────────────

func TestTruncate(t *testing.T) {
	cases := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{"menor que n", "abc", 5, "abc"},
		{"exatamente n (boundary)", "abcde", 5, "abcde"},
		{"maior que n", "abcdef", 5, "abcde..."},
		{"vazio", "", 3, ""},
		{"n zero", "abc", 0, "..."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := truncate(tc.s, tc.n); got != tc.want {
				t.Fatalf("truncate(%q,%d): esperava %q, obteve %q", tc.s, tc.n, tc.want, got)
			}
		})
	}
}

// Garante que truncate nunca retorna mais que n+3 (n + "...") chars.
func TestTruncateNeverExceedsBudget(t *testing.T) {
	long := strings.Repeat("x", 1000)
	got := truncate(long, 200)
	if len(got) != 203 { // 200 + len("...")
		t.Fatalf("truncate: esperava 203 chars, obteve %d", len(got))
	}
}
