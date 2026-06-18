// Package me fornece o cliente HTTP para a API do Melhor Envio.
//
// CRIT-01: Calculate() SEMPRE deve ser chamado server-side antes de criar uma etiqueta.
// Nunca aceitar ou confiar em preço enviado pelo cliente — recalcular via /me/shipment/calculate.
//
// Variáveis de ambiente:
//   - ME_TOKEN    — OAuth token do Melhor Envio. Se vazio, log warning mas continua:
//                   /calculate ainda funciona para cotação anônima; CRUD de etiquetas
//                   falhará na chamada ME com 401.
//   - ME_BASE_URL — URL base da API (padrão: https://melhorenvio.com.br/api/v2)
//
// Todos os métodos recebem context.Context e propagam timeouts/cancelamentos.
// O http.Client interno usa Timeout: 30s; chamadores podem reduzir via ctx.
//
// Erros: wrapeados com contexto descritivo para diagnóstico no slog do handler.
package me

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// MEClient é o cliente para a API do Melhor Envio.
// Instanciar via NewMEClient().
type MEClient struct {
	Token      string
	BaseURL    string
	HTTPClient *http.Client
}

// NewMEClient cria um MEClient a partir das variáveis de ambiente.
//
// Fail-closed parcial: ME_TOKEN vazio gera aviso mas NÃO impede a inicialização.
// O endpoint /calculate ainda funciona para cotação anônima (sem token).
// Operações de CRUD de etiquetas falharão com 401 na chamada à ME API.
func NewMEClient() *MEClient {
	token := os.Getenv("ME_TOKEN")
	if token == "" {
		// Aviso operacional — não fatal. /calculate ainda funciona para cotação.
		slog.Warn("[senderzz_labels] ME_TOKEN não configurado — CRUD de etiquetas indisponível, cotação ainda funciona")
	}

	baseURL := os.Getenv("ME_BASE_URL")
	if baseURL == "" {
		baseURL = "https://melhorenvio.com.br/api/v2"
	}
	// Remove trailing slash para evitar URLs duplas (ex: baseURL + "/me/..." → //me/...).
	baseURL = strings.TrimRight(baseURL, "/")

	return &MEClient{
		Token:   token,
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ─── Tipos de request/response ────────────────────────────────────────────────

// CalcProduct representa um produto para cálculo de frete.
type CalcProduct struct {
	// Dimensões em centímetros, peso em kg.
	Height   float64 `json:"height"`
	Width    float64 `json:"width"`
	Length   float64 `json:"length"`
	Weight   float64 `json:"weight"`
	// Valor declarado para seguro (Valor segurado pelo Melhor Envio).
	InsuranceValue decimal.Decimal `json:"insurance_value"`
	Quantity       int             `json:"quantity"`
}

// CalcRequest é o payload para POST /me/shipment/calculate.
type CalcRequest struct {
	// FromCEP: CEP de origem (somente dígitos, 8 chars).
	FromCEP  string        `json:"from"`
	// ToCEP: CEP de destino (somente dígitos, 8 chars).
	ToCEP    string        `json:"to"`
	Products []CalcProduct `json:"products"`
}

// ServiceOption é uma opção de serviço retornada pelo /me/shipment/calculate.
type ServiceOption struct {
	ServiceID    int             `json:"id"`
	Name         string          `json:"name"`
	Price        decimal.Decimal `json:"price"`
	DeliveryDays int             `json:"delivery_time"`
	// Carrier: transportadora (ex: "Correios", "Jadlog").
	Carrier string `json:"company"`
}

// MEAddress representa um endereço no formato esperado pela ME API.
type MEAddress struct {
	Name        string `json:"name"`
	Phone       string `json:"phone,omitempty"`
	Email       string `json:"email,omitempty"`
	Document    string `json:"document,omitempty"`
	Address     string `json:"address"`
	Complement  string `json:"complement,omitempty"`
	Number      string `json:"number"`
	District    string `json:"district,omitempty"`
	City        string `json:"city"`
	StateAbbr   string `json:"state_abbr"`
	CountryID   string `json:"country_id"`
	PostalCode  string `json:"postal_code"`
}

// MEOrderProduct representa um produto no pedido para criação de etiqueta.
type MEOrderProduct struct {
	Name           string          `json:"name"`
	Quantity       int             `json:"quantity"`
	UnitaryValue   decimal.Decimal `json:"unitary_value"`
	Weight         float64         `json:"weight"`
	// Dimensões em centímetros.
	Width          float64         `json:"width"`
	Height         float64         `json:"height"`
	Length         float64         `json:"length"`
}

// MEOrder é o payload para POST /me/cart (criação de etiqueta no carrinho ME).
type MEOrder struct {
	ServiceID       int              `json:"service"`
	// Agência (opcional; nil → ME escolhe a mais próxima).
	AgencyID        *int             `json:"agency,omitempty"`
	From            MEAddress        `json:"from"`
	To              MEAddress        `json:"to"`
	Products        []MEOrderProduct `json:"products"`
	Volumes         []MEVolume       `json:"volumes"`
	Options         MEOrderOptions   `json:"options"`
}

// MEVolume representa as dimensões do volume a ser enviado.
type MEVolume struct {
	Height float64 `json:"height"`
	Width  float64 `json:"width"`
	Length float64 `json:"length"`
	Weight float64 `json:"weight"`
}

// MEOrderOptions configura opções adicionais do envio.
type MEOrderOptions struct {
	// InsuranceValue: valor para seguro. CRIT-01: calculado server-side.
	InsuranceValue decimal.Decimal `json:"insurance_value"`
	// Receipt: aviso de recebimento (AR).
	Receipt        bool            `json:"receipt"`
	// OwnHand: mãos próprias.
	OwnHand        bool            `json:"own_hand"`
	// Collect: coleta na origem.
	Collect        bool            `json:"collect"`
	// NonCommercial: declaração de não-comercial.
	NonCommercial  bool            `json:"non_commercial"`
	// Invoice: nota fiscal (opcional).
	Invoice        *MEInvoice      `json:"invoice,omitempty"`
}

// MEInvoice representa os dados da nota fiscal do pedido.
type MEInvoice struct {
	Key string `json:"key"`
}

// meCalcPayload é o formato exato esperado pela ME API para cotação.
// A ME usa "from" e "to" como objetos com campo "postal_code".
type meCalcPayload struct {
	From     struct{ PostalCode string `json:"postal_code"` } `json:"from"`
	To       struct{ PostalCode string `json:"postal_code"` } `json:"to"`
	Products []CalcProduct `json:"products"`
}

// meShipmentResponse é o formato de retorno do POST /me/cart.
type meShipmentResponse struct {
	ID string `json:"id"`
}

// meGenerateResponse é o formato de retorno do POST /me/shipment/generate.
type meGenerateResponse struct {
	// Pode retornar um array de etiquetas ou um objeto único.
	Shipments []struct {
		ID       string `json:"id"`
		LabelURL string `json:"label"`
	} `json:"shipments"`
	// Formato alternativo (objeto único).
	ID       string `json:"id"`
	LabelURL string `json:"label"`
}

// meTrackingResponse é o formato do GET /me/shipment/tracking/{code}.
type meTrackingResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// ─── Métodos do cliente ────────────────────────────────────────────────────────

// Calculate chama POST /me/shipment/calculate e retorna as opções de serviço disponíveis.
//
// CRIT-01: Este método DEVE ser chamado server-side antes de criar qualquer etiqueta.
// O preço retornado aqui é o único valor autorizado a ser usado na criação/cobrança.
// Nunca confiar em preço enviado pelo cliente.
//
// O resultado é armazenado em cache (wc_me_shipment_cache, TTL 10 min) pelo handler.
func (c *MEClient) Calculate(ctx context.Context, req CalcRequest) ([]ServiceOption, error) {
	payload := meCalcPayload{
		Products: req.Products,
	}
	payload.From.PostalCode = req.FromCEP
	payload.To.PostalCode = req.ToCEP

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("[senderzz_labels] Calculate: marshal payload: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPost, "/shipment/calculate", body)
	if err != nil {
		return nil, fmt.Errorf("[senderzz_labels] Calculate: requisição: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		rawErr, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("[senderzz_labels] Calculate: ME retornou status %d: %s",
			resp.StatusCode, truncate(string(rawErr), 200))
	}

	var options []ServiceOption
	if err := json.NewDecoder(resp.Body).Decode(&options); err != nil {
		return nil, fmt.Errorf("[senderzz_labels] Calculate: decode response: %w", err)
	}

	slog.Info("[senderzz_labels] cotação ME concluída",
		"from_cep", req.FromCEP,
		"to_cep", req.ToCEP,
		"opcoes", len(options),
	)

	return options, nil
}

// CalculateCacheKey gera a chave de cache SHA-256 para os parâmetros de cálculo.
// Usada pelo handler para verificar/armazenar em wc_me_shipment_cache.
func CalculateCacheKey(req CalcRequest) string {
	data, _ := json.Marshal(req)
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// CreateShipment chama POST /me/cart para adicionar o pedido ao carrinho ME.
// Retorna o shipment_id (ID do item no carrinho) que deve ser armazenado em
// wc_me_labels.me_shipment_id.
//
// CRIT-01: order.Options.InsuranceValue deve ser calculado a partir do resultado
// de Calculate(), nunca de valor enviado pelo cliente.
func (c *MEClient) CreateShipment(ctx context.Context, order MEOrder) (string, error) {
	body, err := json.Marshal(order)
	if err != nil {
		return "", fmt.Errorf("[senderzz_labels] CreateShipment: marshal payload: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPost, "/cart", body)
	if err != nil {
		return "", fmt.Errorf("[senderzz_labels] CreateShipment: requisição: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		rawErr, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("[senderzz_labels] CreateShipment: ME retornou status %d: %s",
			resp.StatusCode, truncate(string(rawErr), 200))
	}

	var result meShipmentResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("[senderzz_labels] CreateShipment: decode response: %w", err)
	}

	if result.ID == "" {
		return "", fmt.Errorf("[senderzz_labels] CreateShipment: ME retornou shipment_id vazio")
	}

	slog.Info("[senderzz_labels] pedido adicionado ao carrinho ME",
		"shipment_id", result.ID,
		"service_id", order.ServiceID,
	)

	return result.ID, nil
}

// GenerateLabel chama POST /me/shipment/generate para gerar a etiqueta.
// Retorna a URL da etiqueta gerada.
// O download do PDF é feito assincronamente pelo job ProcessGeneratePDF.
func (c *MEClient) GenerateLabel(ctx context.Context, shipmentID string) (string, error) {
	// A ME API espera um array de IDs para geração em lote.
	payload := map[string]any{
		"orders": []string{shipmentID},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("[senderzz_labels] GenerateLabel: marshal payload: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPost, "/shipment/generate", body)
	if err != nil {
		return "", fmt.Errorf("[senderzz_labels] GenerateLabel: requisição: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		rawErr, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("[senderzz_labels] GenerateLabel: ME retornou status %d: %s",
			resp.StatusCode, truncate(string(rawErr), 200))
	}

	var result meGenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("[senderzz_labels] GenerateLabel: decode response: %w", err)
	}

	// Normaliza: ME pode retornar objeto único ou array de shipments.
	labelURL := result.LabelURL
	if labelURL == "" && len(result.Shipments) > 0 {
		labelURL = result.Shipments[0].LabelURL
	}
	if labelURL == "" {
		return "", fmt.Errorf("[senderzz_labels] GenerateLabel: ME retornou URL vazia para shipment_id=%s", shipmentID)
	}

	slog.Info("[senderzz_labels] etiqueta gerada pela ME",
		"shipment_id", shipmentID,
		"label_url", labelURL,
	)

	return labelURL, nil
}

// TrackShipment chama GET /me/shipment/tracking/{code} e retorna o status atual.
// Usado pelo job ProcessSyncTracking para sincronizar wc_me_labels.status.
func (c *MEClient) TrackShipment(ctx context.Context, trackingCode string) (string, error) {
	if trackingCode == "" {
		return "", fmt.Errorf("[senderzz_labels] TrackShipment: tracking_code vazio")
	}

	resp, err := c.doRequest(ctx, http.MethodGet,
		"/shipment/tracking/"+trackingCode, nil)
	if err != nil {
		return "", fmt.Errorf("[senderzz_labels] TrackShipment: requisição: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("[senderzz_labels] TrackShipment: código %s não encontrado na ME", trackingCode)
	}
	if resp.StatusCode != http.StatusOK {
		rawErr, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("[senderzz_labels] TrackShipment: ME retornou status %d: %s",
			resp.StatusCode, truncate(string(rawErr), 200))
	}

	var result meTrackingResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("[senderzz_labels] TrackShipment: decode response: %w", err)
	}

	slog.Info("[senderzz_labels] rastreamento atualizado",
		"tracking_code", trackingCode,
		"status", result.Status,
	)

	return result.Status, nil
}

// CancelShipment chama DELETE /me/cart/{id} para remover o pedido do carrinho ME.
// Deve ser chamado antes de marcar a etiqueta como canceled no banco.
func (c *MEClient) CancelShipment(ctx context.Context, shipmentID string) error {
	if shipmentID == "" {
		return fmt.Errorf("[senderzz_labels] CancelShipment: shipment_id vazio")
	}

	resp, err := c.doRequest(ctx, http.MethodDelete, "/cart/"+shipmentID, nil)
	if err != nil {
		return fmt.Errorf("[senderzz_labels] CancelShipment: requisição: %w", err)
	}
	defer resp.Body.Close()

	// ME retorna 200 ou 204 em cancelamento bem-sucedido.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		rawErr, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("[senderzz_labels] CancelShipment: ME retornou status %d: %s",
			resp.StatusCode, truncate(string(rawErr), 200))
	}

	slog.Info("[senderzz_labels] pedido cancelado na ME", "shipment_id", shipmentID)
	return nil
}

// ─── helper interno ────────────────────────────────────────────────────────────

// doRequest executa uma requisição HTTP autenticada para a ME API.
// Define o header Authorization: Bearer {token} em toda requisição.
// body pode ser nil para métodos sem corpo (GET, DELETE).
func (c *MEClient) doRequest(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	url := c.BaseURL + path

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("criar requisição %s %s: %w", method, path, err)
	}

	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// User-Agent identifica o serviço nas métricas da ME API.
	req.Header.Set("User-Agent", "senderzz-labels-service/4.0")

	return c.HTTPClient.Do(req)
}

// truncate retorna os primeiros n caracteres da string s (para mensagens de erro seguras).
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
