// SEC-LABELS: testes do PIPELINE Melhor Envio no nível dos métodos do cliente
// (parse de resposta, validação de status HTTP, guards de entrada/saída).
//
// NENHUM teste aqui chama a ME externa real. O padrão do projeto ("sem rede e sem
// banco") é respeitado via um http.RoundTripper STUB injetado em HTTPClient.Transport:
// o RoundTripper devolve *http.Response canônicos sem abrir socket algum.
//
// ARMADILHA (P2-02): validateMEBaseURL roda em TODO doRequest e recusa scheme != https
// e hosts fora da whitelist — por isso um httptest.Server (127.0.0.1) NÃO serviria
// (seria bloqueado antes de qualquer resposta). A BaseURL é fixada no host oficial
// whitelisted (a validação só inspeciona a string); o RoundTripper intercepta o Do()
// e nunca toca a internet. Assim cobrimos o parse/erro do cliente isoladamente.
package me

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
)

// ─── infra de stub (sem rede) ────────────────────────────────────────────────

// roundTripFunc adapta uma função a http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// newStubClient devolve um MEClient cuja BaseURL passa no guard P2-02 (host oficial
// whitelisted) mas cujo transporte é o stub fornecido — nenhuma chamada de rede.
func newStubClient(rt roundTripFunc) *MEClient {
	return &MEClient{
		Token:      "stub-token",
		BaseURL:    "https://melhorenvio.com.br/api/v2",
		HTTPClient: &http.Client{Transport: rt},
	}
}

// respWith monta um *http.Response com status e corpo JSON (string) prontos para o stub.
func respWith(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

// ─── Calculate — parse de resposta + status ──────────────────────────────────

func TestCalculate_ParseOK(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	c := newStubClient(func(r *http.Request) (*http.Response, error) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		// A ME devolve um ARRAY de opções de serviço.
		return respWith(http.StatusOK, `[
			{"id":1,"name":"PAC","price":"23.45","delivery_time":7,"company":{"id":1,"name":"Correios"}},
			{"id":2,"name":"SEDEX","price":"41.90","delivery_time":2,"company":{"id":1,"name":"Correios"}}
		]`), nil
	})

	opts, err := c.Calculate(context.Background(), CalcRequest{
		FromCEP: "01001000", ToCEP: "20010000",
		Products: []CalcProduct{{Height: 11, Width: 15, Length: 20, Weight: 0.3, Quantity: 1}},
	})
	if err != nil {
		t.Fatalf("Calculate: erro inesperado: %v", err)
	}
	if len(opts) != 2 {
		t.Fatalf("Calculate: esperava 2 opções, obteve %d", len(opts))
	}
	// Parse correto dos campos (id, name, price decimal, delivery_time, company).
	if opts[0].ServiceID != 1 || opts[0].Name != "PAC" || opts[0].DeliveryDays != 7 {
		t.Fatalf("Calculate: opção[0] mal parseada: %+v", opts[0])
	}
	if !opts[0].Price.Equal(decimal.RequireFromString("23.45")) {
		t.Fatalf("Calculate: price[0] esperava 23.45, obteve %s", opts[0].Price)
	}
	if !opts[1].Price.Equal(decimal.RequireFromString("41.90")) {
		t.Fatalf("Calculate: price[1] esperava 41.90, obteve %s", opts[1].Price)
	}
	// Request bem-formado: GET /api/v2/shipment/calculate com Bearer token e query params.
	if gotMethod != http.MethodGet {
		t.Errorf("Calculate: método esperava GET, obteve %s", gotMethod)
	}
	if !strings.HasSuffix(gotPath, "/me/shipment/calculate") {
		t.Errorf("Calculate: path esperava .../me/shipment/calculate, obteve %s", gotPath)
	}
	if gotAuth != "Bearer stub-token" {
		t.Errorf("Calculate: Authorization esperava 'Bearer stub-token', obteve %q", gotAuth)
	}
}

func TestCalculate_EmptyArray(t *testing.T) {
	c := newStubClient(func(r *http.Request) (*http.Response, error) {
		return respWith(http.StatusOK, `[]`), nil
	})
	opts, err := c.Calculate(context.Background(), CalcRequest{FromCEP: "1", ToCEP: "2"})
	if err != nil {
		t.Fatalf("Calculate: array vazio não é erro, obteve: %v", err)
	}
	if len(opts) != 0 {
		t.Fatalf("Calculate: esperava 0 opções, obteve %d", len(opts))
	}
}

func TestCalculate_NonOKStatus(t *testing.T) {
	c := newStubClient(func(r *http.Request) (*http.Response, error) {
		return respWith(http.StatusUnauthorized, `{"message":"token inválido"}`), nil
	})
	_, err := c.Calculate(context.Background(), CalcRequest{FromCEP: "1", ToCEP: "2"})
	if err == nil {
		t.Fatal("Calculate: status 401 deveria propagar erro")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("Calculate: erro deveria mencionar o status 401, obteve: %v", err)
	}
}

func TestCalculate_MalformedJSON(t *testing.T) {
	c := newStubClient(func(r *http.Request) (*http.Response, error) {
		return respWith(http.StatusOK, `{not json`), nil
	})
	_, err := c.Calculate(context.Background(), CalcRequest{FromCEP: "1", ToCEP: "2"})
	if err == nil {
		t.Fatal("Calculate: JSON malformado deveria gerar erro de decode")
	}
}

func TestCalculate_TransportError(t *testing.T) {
	c := newStubClient(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New("conexão recusada")
	})
	_, err := c.Calculate(context.Background(), CalcRequest{FromCEP: "1", ToCEP: "2"})
	if err == nil {
		t.Fatal("Calculate: erro de transporte deveria propagar")
	}
}

// SSRF guard P2-02 ainda vale no nível do método: BaseURL fora da whitelist
// bloqueia ANTES de qualquer Do() — o token nunca é enviado.
func TestCalculate_BlockedBySSRFGuard(t *testing.T) {
	called := false
	c := newStubClient(func(r *http.Request) (*http.Response, error) {
		called = true
		return respWith(http.StatusOK, `[]`), nil
	})
	c.BaseURL = "https://evil.example.com/api/v2" // fora da whitelist
	_, err := c.Calculate(context.Background(), CalcRequest{FromCEP: "1", ToCEP: "2"})
	if err == nil {
		t.Fatal("Calculate: BaseURL fora da whitelist deveria falhar (P2-02)")
	}
	if called {
		t.Fatal("Calculate: o transporte foi chamado apesar do guard P2-02 (token vazaria)")
	}
}

// ─── CreateShipment — parse + guards ──────────────────────────────────────────

func TestCreateShipment_OK(t *testing.T) {
	c := newStubClient(func(r *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(r.URL.Path, "/cart") {
			t.Errorf("CreateShipment: path esperava .../cart, obteve %s", r.URL.Path)
		}
		return respWith(http.StatusOK, `{"id":"ship-abc-123"}`), nil
	})
	id, err := c.CreateShipment(context.Background(), MEOrder{ServiceID: 1})
	if err != nil {
		t.Fatalf("CreateShipment: erro inesperado: %v", err)
	}
	if id != "ship-abc-123" {
		t.Fatalf("CreateShipment: shipment_id esperava 'ship-abc-123', obteve %q", id)
	}
}

func TestCreateShipment_Accepts201Created(t *testing.T) {
	c := newStubClient(func(r *http.Request) (*http.Response, error) {
		return respWith(http.StatusCreated, `{"id":"ship-201"}`), nil
	})
	id, err := c.CreateShipment(context.Background(), MEOrder{ServiceID: 1})
	if err != nil {
		t.Fatalf("CreateShipment: 201 deveria ser aceito, obteve: %v", err)
	}
	if id != "ship-201" {
		t.Fatalf("CreateShipment: esperava 'ship-201', obteve %q", id)
	}
}

func TestCreateShipment_EmptyIDIsError(t *testing.T) {
	c := newStubClient(func(r *http.Request) (*http.Response, error) {
		// 200 mas sem id → o cliente NÃO pode prosseguir (etiqueta sem shipment).
		return respWith(http.StatusOK, `{"id":""}`), nil
	})
	_, err := c.CreateShipment(context.Background(), MEOrder{ServiceID: 1})
	if err == nil {
		t.Fatal("CreateShipment: id vazio deveria ser erro")
	}
	if !strings.Contains(err.Error(), "vazio") {
		t.Fatalf("CreateShipment: erro deveria mencionar 'vazio', obteve: %v", err)
	}
}

func TestCreateShipment_NonOKStatus(t *testing.T) {
	c := newStubClient(func(r *http.Request) (*http.Response, error) {
		return respWith(http.StatusUnprocessableEntity, `{"message":"endereço inválido"}`), nil
	})
	_, err := c.CreateShipment(context.Background(), MEOrder{ServiceID: 1})
	if err == nil {
		t.Fatal("CreateShipment: status 422 deveria propagar erro")
	}
	if !strings.Contains(err.Error(), "422") {
		t.Fatalf("CreateShipment: erro deveria mencionar 422, obteve: %v", err)
	}
}

// ─── GenerateLabel — parse objeto vs array + guard de URL vazia ───────────────

// TestGenerateLabel_ArrayTopLevel cobre o formato REAL da ME v2 (prod):
// [{"id":"...","label":"..."}] — array top-level.
func TestGenerateLabel_ArrayTopLevel(t *testing.T) {
	c := newStubClient(func(r *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(r.URL.Path, "/shipment/generate") {
			t.Errorf("GenerateLabel: path esperava .../shipment/generate, obteve %s", r.URL.Path)
		}
		// Formato real ME v2: array top-level.
		return respWith(http.StatusOK,
			`[{"id":"s1","label":"https://melhorenvio.com.br/shipment/preview/s1","status":"pending","tracking":null}]`), nil
	})
	url, err := c.GenerateLabel(context.Background(), "s1")
	if err != nil {
		t.Fatalf("GenerateLabel (array top-level): erro inesperado: %v", err)
	}
	if url != "https://melhorenvio.com.br/shipment/preview/s1" {
		t.Fatalf("GenerateLabel (array top-level): URL esperada, obteve %q", url)
	}
}

func TestGenerateLabel_ObjetoUnico(t *testing.T) {
	c := newStubClient(func(r *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(r.URL.Path, "/shipment/generate") {
			t.Errorf("GenerateLabel: path esperava .../shipment/generate, obteve %s", r.URL.Path)
		}
		return respWith(http.StatusOK, `{"id":"s1","label":"https://melhorenvio.com.br/label/top.pdf"}`), nil
	})
	url, err := c.GenerateLabel(context.Background(), "s1")
	if err != nil {
		t.Fatalf("GenerateLabel: erro inesperado: %v", err)
	}
	if url != "https://melhorenvio.com.br/label/top.pdf" {
		t.Fatalf("GenerateLabel: URL esperada do topo, obteve %q", url)
	}
}

func TestGenerateLabel_ArrayShipments(t *testing.T) {
	c := newStubClient(func(r *http.Request) (*http.Response, error) {
		return respWith(http.StatusOK,
			`{"shipments":[{"id":"s1","label":"https://melhorenvio.com.br/label/arr.pdf"}]}`), nil
	})
	url, err := c.GenerateLabel(context.Background(), "s1")
	if err != nil {
		t.Fatalf("GenerateLabel: erro inesperado: %v", err)
	}
	if url != "https://melhorenvio.com.br/label/arr.pdf" {
		t.Fatalf("GenerateLabel: URL esperada do array, obteve %q", url)
	}
}

func TestGenerateLabel_URLVaziaIsError(t *testing.T) {
	c := newStubClient(func(r *http.Request) (*http.Response, error) {
		// 200 mas nenhuma URL de etiqueta → o cliente trata como erro
		// (não há PDF a baixar; o job falharia adiante).
		return respWith(http.StatusOK, `{"id":"s1"}`), nil
	})
	_, err := c.GenerateLabel(context.Background(), "s1")
	if err == nil {
		t.Fatal("GenerateLabel: URL vazia deveria ser erro")
	}
	if !strings.Contains(err.Error(), "vazia") {
		t.Fatalf("GenerateLabel: erro deveria mencionar 'vazia', obteve: %v", err)
	}
}

func TestGenerateLabel_NonOKStatus(t *testing.T) {
	c := newStubClient(func(r *http.Request) (*http.Response, error) {
		return respWith(http.StatusBadGateway, `{"message":"ME indisponível"}`), nil
	})
	_, err := c.GenerateLabel(context.Background(), "s1")
	if err == nil {
		t.Fatal("GenerateLabel: status 502 deveria propagar erro")
	}
}

// ─── TrackShipment — parse + guards (código vazio, 404) ──────────────────────

func TestTrackShipment_OK(t *testing.T) {
	c := newStubClient(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v2/me/shipment/tracking" {
			t.Errorf("TrackShipment: esperava POST /me/shipment/tracking, obteve %s %s", r.Method, r.URL.Path)
		}
		return respWith(http.StatusOK, `{"BR123":{"status":"delivered"}}`), nil
	})
	status, err := c.TrackShipment(context.Background(), "BR123")
	if err != nil {
		t.Fatalf("TrackShipment: erro inesperado: %v", err)
	}
	if status != "delivered" {
		t.Fatalf("TrackShipment: status esperava 'delivered', obteve %q", status)
	}
}

func TestTrackShipment_CodigoVazioGuard(t *testing.T) {
	called := false
	c := newStubClient(func(r *http.Request) (*http.Response, error) {
		called = true
		return respWith(http.StatusOK, `{}`), nil
	})
	_, err := c.TrackShipment(context.Background(), "")
	if err == nil {
		t.Fatal("TrackShipment: código vazio deveria ser erro antes de qualquer request")
	}
	if called {
		t.Fatal("TrackShipment: não deveria ter chamado o transporte com código vazio")
	}
}

func TestTrackShipment_NotFound(t *testing.T) {
	c := newStubClient(func(r *http.Request) (*http.Response, error) {
		return respWith(http.StatusNotFound, `{"message":"não encontrado"}`), nil
	})
	_, err := c.TrackShipment(context.Background(), "BR404")
	if err == nil {
		t.Fatal("TrackShipment: 404 deveria propagar erro")
	}
	if !strings.Contains(err.Error(), "não encontrado") {
		t.Fatalf("TrackShipment: erro 404 deveria ser específico, obteve: %v", err)
	}
}

// ─── CancelShipment — guards + status aceitos ─────────────────────────────────

func TestCancelShipment_OK200(t *testing.T) {
	c := newStubClient(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodDelete {
			t.Errorf("CancelShipment: método esperava DELETE, obteve %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/cart/ship-1") {
			t.Errorf("CancelShipment: path esperava .../cart/ship-1, obteve %s", r.URL.Path)
		}
		return respWith(http.StatusOK, `{"ok":true}`), nil
	})
	if err := c.CancelShipment(context.Background(), "ship-1"); err != nil {
		t.Fatalf("CancelShipment: erro inesperado: %v", err)
	}
}

func TestCancelShipment_Accepts204(t *testing.T) {
	c := newStubClient(func(r *http.Request) (*http.Response, error) {
		return respWith(http.StatusNoContent, ``), nil
	})
	if err := c.CancelShipment(context.Background(), "ship-1"); err != nil {
		t.Fatalf("CancelShipment: 204 deveria ser aceito, obteve: %v", err)
	}
}

func TestCancelShipment_EmptyIDGuard(t *testing.T) {
	called := false
	c := newStubClient(func(r *http.Request) (*http.Response, error) {
		called = true
		return respWith(http.StatusOK, ``), nil
	})
	if err := c.CancelShipment(context.Background(), ""); err == nil {
		t.Fatal("CancelShipment: shipment_id vazio deveria ser erro")
	}
	if called {
		t.Fatal("CancelShipment: não deveria ter chamado o transporte com id vazio")
	}
}

func TestCancelShipment_NonOKStatus(t *testing.T) {
	c := newStubClient(func(r *http.Request) (*http.Response, error) {
		return respWith(http.StatusInternalServerError, `{"message":"erro ME"}`), nil
	})
	if err := c.CancelShipment(context.Background(), "ship-1"); err == nil {
		t.Fatal("CancelShipment: status 500 deveria propagar erro")
	}
}
