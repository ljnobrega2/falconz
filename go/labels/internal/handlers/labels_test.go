// SEC-LABELS: testes unitários dos helpers PUROS do pacote handlers.
//
// Nenhum teste aqui toca banco ou rede. Cobrem:
//   - labelBlocksReissue   — regra de idempotência (qual status bloqueia reemissão)
//   - normalizeDims        — defaulting de peso/dimensões (extraído de GetCalculate)
//   - validateHMAC         — validação da assinatura do webhook de rastreamento
//   - mapTrackingStatus    — mapeamento status ME → interno
//   - cacheKeyFrom         — hash determinístico
//   - buildMEProducts      — conversão CalcProduct → MEOrderProduct
//   - parseLabelID         — parsing/validação do {id} da rota
//
// A GUARDA de idempotência em si (a query pré-flight no PostLabel) NÃO é coberta
// aqui — depende de banco e está fora do escopo "sem rede". labelBlocksReissue é a
// parte pura/testável dessa regra; a guarda completa é validável só em integração.
package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/senderzz/labels-service/internal/me"
	"github.com/shopspring/decimal"
)

// ─── labelBlocksReissue — regra de idempotência (SEC-LABELS) ──────────────────

func TestLabelBlocksReissue(t *testing.T) {
	cases := []struct {
		status string
		want   bool
	}{
		{"draft", true},     // etiqueta já em criação → não duplicar
		{"released", true},  // PDF gerado → não duplicar
		{"posted", true},    // postada → não duplicar
		{"delivered", true}, // entregue → não duplicar
		{"lost", true},      // extraviada (já cobrada) → não duplicar
		{"canceled", false}, // cancelada (ex.: débito recusado) → PODE reemitir
		{"", true},          // status vazio/desconhecido → fail-safe bloqueia
		{"qualquer", true},  // status desconhecido → fail-safe bloqueia
	}
	for _, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			if got := labelBlocksReissue(tc.status); got != tc.want {
				t.Fatalf("labelBlocksReissue(%q): esperava %v, obteve %v", tc.status, tc.want, got)
			}
		})
	}
}

// ─── normalizeDims — defaulting de peso/dimensões (SEC-LABELS) ────────────────

func TestNormalizeDims(t *testing.T) {
	cases := []struct {
		name                        string
		w, h, wd, l                 float64
		wantW, wantH, wantWd, wantL float64
	}{
		{"todos zero → mínimos PAC", 0, 0, 0, 0, 0.3, 11, 15, 20},
		{"todos negativos → mínimos", -1, -5, -2, -9, 0.3, 11, 15, 20},
		{"todos positivos → intactos", 2.5, 30, 25, 40, 2.5, 30, 25, 40},
		{"misto: peso ok, resto zero", 1.2, 0, 0, 0, 1.2, 11, 15, 20},
		{"misto: só altura zero", 1.0, 0, 25, 40, 1.0, 11, 25, 40},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gw, gh, gwd, gl := normalizeDims(tc.w, tc.h, tc.wd, tc.l)
			if gw != tc.wantW || gh != tc.wantH || gwd != tc.wantWd || gl != tc.wantL {
				t.Fatalf("normalizeDims(%v,%v,%v,%v) = (%v,%v,%v,%v); esperava (%v,%v,%v,%v)",
					tc.w, tc.h, tc.wd, tc.l, gw, gh, gwd, gl, tc.wantW, tc.wantH, tc.wantWd, tc.wantL)
			}
		})
	}
}

// ─── validateHMAC — assinatura do webhook de rastreamento ─────────────────────

func TestValidateHMAC(t *testing.T) {
	secret := "segredo-de-teste"
	body := []byte(`{"tracking_code":"BR123","status":"delivered"}`)

	// Assinatura correta computada do mesmo jeito que o handler valida.
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	validHex := hex.EncodeToString(mac.Sum(nil))

	cases := []struct {
		name      string
		sigHeader string
		want      bool
	}{
		{"assinatura válida", "sha256=" + validHex, true},
		{"sem prefixo sha256=", validHex, false},
		{"prefixo errado", "sha1=" + validHex, false},
		{"hex incorreto", "sha256=deadbeef", false},
		{"vazio", "", false},
		{"só prefixo", "sha256=", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validateHMAC(body, tc.sigHeader, secret); got != tc.want {
				t.Fatalf("validateHMAC(%q): esperava %v, obteve %v", tc.sigHeader, tc.want, got)
			}
		})
	}

	// Corpo adulterado com assinatura válida do corpo original → rejeita.
	tampered := []byte(`{"tracking_code":"BR123","status":"canceled"}`)
	if validateHMAC(tampered, "sha256="+validHex, secret) {
		t.Fatal("validateHMAC: aceitou corpo adulterado (deveria rejeitar)")
	}
}

// ─── mapTrackingStatus — status ME → interno ──────────────────────────────────

func TestMapTrackingStatus(t *testing.T) {
	cases := map[string]string{
		"posted":           "posted",
		"in_transit":       "posted",
		"out_for_delivery": "posted",
		"waiting_pickup":   "released",
		"delivered":        "delivered",
		"canceled":         "canceled",
		"cancelled":        "canceled", // variante de grafia
		"returned":         "canceled",
		"lost":             "lost",
		"desconhecido":     "released", // sem despacho confirmado
		"":                 "released", // sem despacho confirmado
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			if got := mapTrackingStatus(in); got != want {
				t.Fatalf("mapTrackingStatus(%q): esperava %q, obteve %q", in, want, got)
			}
		})
	}
}

// ─── cacheKeyFrom — hash determinístico ───────────────────────────────────────

func TestCacheKeyFrom(t *testing.T) {
	a := cacheKeyFrom("payload-x")
	b := cacheKeyFrom("payload-x")
	if a != b {
		t.Fatalf("cacheKeyFrom não determinístico: %q != %q", a, b)
	}
	if len(a) != 64 {
		t.Fatalf("cacheKeyFrom: esperava 64 chars (sha256 hex), obteve %d", len(a))
	}
	if cacheKeyFrom("payload-y") == a {
		t.Fatal("cacheKeyFrom: entradas distintas geraram o mesmo hash")
	}

	// Vetor conhecido: SHA-256 de "" é o hash canônico do vazio.
	const emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got := cacheKeyFrom(""); got != emptySHA256 {
		t.Fatalf("cacheKeyFrom(\"\"): esperava %q, obteve %q", emptySHA256, got)
	}
}

// ─── buildMEProducts — conversão CalcProduct → MEOrderProduct ─────────────────

func TestBuildMEProducts(t *testing.T) {
	in := []me.CalcProduct{
		{
			Height:         11,
			Width:          15,
			Length:         20,
			Weight:         0.5,
			InsuranceValue: decimal.NewFromFloat(150.00),
			Quantity:       2,
		},
	}
	out := buildMEProducts(in)
	if len(out) != 1 {
		t.Fatalf("buildMEProducts: esperava 1 produto, obteve %d", len(out))
	}
	p := out[0]
	if p.Name != "Produto" {
		t.Errorf("Name: esperava %q, obteve %q", "Produto", p.Name)
	}
	if p.Quantity != 2 {
		t.Errorf("Quantity: esperava 2, obteve %d", p.Quantity)
	}
	if !p.UnitaryValue.Equal(decimal.NewFromFloat(150.00)) {
		t.Errorf("UnitaryValue: esperava 150.00, obteve %s", p.UnitaryValue)
	}
	if p.Weight != 0.5 || p.Width != 15 || p.Height != 11 || p.Length != 20 {
		t.Errorf("dimensões/peso não preservados: %+v", p)
	}

	// Lista vazia → slice vazia (não nil-panic).
	if got := buildMEProducts(nil); len(got) != 0 {
		t.Fatalf("buildMEProducts(nil): esperava 0 produtos, obteve %d", len(got))
	}
}

// ─── buildMEVolumes — um pedido deve virar um único pacote físico ────────────

func TestBuildMEVolumesConsolidatesProductsIntoSinglePackage(t *testing.T) {
	in := []me.CalcProduct{
		{
			Height:   10,
			Width:    3,
			Length:   3,
			Weight:   0.05,
			Quantity: 3,
		},
		{
			Height:   8,
			Width:    4,
			Length:   5,
			Weight:   0.05,
			Quantity: 1,
		},
	}

	out := buildMEVolumes(in)
	if len(out) != 1 {
		t.Fatalf("buildMEVolumes: esperava 1 volume consolidado, obteve %d", len(out))
	}

	volume := out[0]
	if volume.Weight != 0.2 {
		t.Errorf("Weight: esperava soma peso×quantidade 0.2 kg, obteve %v", volume.Weight)
	}
	if volume.Height != 10 || volume.Width != 4 || volume.Length != 5 {
		t.Errorf("dimensões: esperava maiores dimensões 10x4x5, obteve %+v", volume)
	}

	if got := buildMEVolumes(nil); len(got) != 0 {
		t.Fatalf("buildMEVolumes(nil): esperava 0 volumes, obteve %d", len(got))
	}
}

// ─── parseLabelID — parsing/validação do {id} da rota ─────────────────────────

func TestParseLabelID(t *testing.T) {
	cases := []struct {
		name    string
		id      string
		want    int64
		wantErr bool
	}{
		{"id válido", "42", 42, false},
		{"id grande", "9007199254740991", 9007199254740991, false},
		{"zero inválido", "0", 0, true},
		{"negativo inválido", "-3", 0, true},
		{"não numérico", "abc", 0, true},
		{"vazio", "", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Monta um *http.Request com o URLParam {id} no contexto do chi.
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("id", tc.id)
			r := httptest.NewRequest("GET", "/labels/"+tc.id, nil)
			r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

			got, err := parseLabelID(r)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseLabelID(%q): esperava erro, obteve nil (got=%d)", tc.id, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseLabelID(%q): erro inesperado: %v", tc.id, err)
			}
			if got != tc.want {
				t.Fatalf("parseLabelID(%q): esperava %d, obteve %d", tc.id, tc.want, got)
			}
		})
	}
}
