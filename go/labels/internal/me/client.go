// Package me fornece o cliente HTTP para a API do Melhor Envio.
//
// CRIT-01: Calculate() SEMPRE deve ser chamado server-side antes de criar uma etiqueta.
// Nunca aceitar ou confiar em preço enviado pelo cliente — recalcular via /me/shipment/calculate.
//
// Variáveis de ambiente:
//   - ME_TOKEN    — OAuth token do Melhor Envio. Se vazio, log warning mas continua:
//     /calculate ainda funciona para cotação anônima; CRUD de etiquetas
//     falhará na chamada ME com 401.
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
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// P2-02: SSRF guard — a base URL da API ME (ME_BASE_URL) é controlada por env var.
// Se adulterada para um host arbitrário, o Bearer token do Melhor Envio (definido
// em todo doRequest) seria exfiltrado para o atacante. Por isso o scheme DEVE ser
// https e o host DEVE estar nesta whitelist; caso contrário a requisição falha
// (fail-closed) e o token nunca é enviado.
var meAllowedHosts = map[string]struct{}{
	"melhorenvio.com.br":         {},
	"sandbox.melhorenvio.com.br": {},
	"www.melhorenvio.com.br":     {},
}

// validateMEBaseURL valida a base URL da API ME contra a whitelist P2-02.
// Retorna erro descritivo (PT-BR) se o scheme não for https ou o host não estiver
// autorizado — nunca permite que o Bearer token seja enviado para destino arbitrário.
func validateMEBaseURL(baseURL string) error {
	if baseURL == "" {
		return fmt.Errorf("ME_BASE_URL vazia")
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("ME_BASE_URL inválida: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("ME_BASE_URL deve usar scheme https (recebido %q)", u.Scheme)
	}
	// u.Hostname() remove porta e colchetes de IPv6; comparação exata contra a whitelist.
	host := strings.ToLower(u.Hostname())
	if _, ok := meAllowedHosts[host]; !ok {
		return fmt.Errorf("host %q não autorizado para a API ME (P2-02 SSRF guard)", host)
	}
	return nil
}

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

	const defaultBaseURL = "https://melhorenvio.com.br/api/v2"
	baseURL := os.Getenv("ME_BASE_URL")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	// Remove trailing slash para evitar URLs duplas (ex: baseURL + "/me/..." → //me/...).
	baseURL = strings.TrimRight(baseURL, "/")

	// P2-02: valida a base URL contra a whitelist de SSRF já no startup.
	// Se ME_BASE_URL foi adulterada (scheme != https ou host fora da whitelist),
	// faz fail-safe para o default oficial e loga o erro — assim o Bearer token
	// nunca é enviado a um destino arbitrário. doRequest revalida a cada chamada
	// (fail-closed) como defesa em profundidade.
	if err := validateMEBaseURL(baseURL); err != nil {
		slog.Error("[senderzz_labels] ME_BASE_URL rejeitada (P2-02 SSRF guard) — usando default oficial",
			"err", err,
			"recebida", baseURL,
		)
		baseURL = defaultBaseURL
	}

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
	Height float64 `json:"height"`
	Width  float64 `json:"width"`
	Length float64 `json:"length"`
	Weight float64 `json:"weight"`
	// Valor declarado para seguro (Valor segurado pelo Melhor Envio).
	InsuranceValue decimal.Decimal `json:"insurance_value"`
	Quantity       int             `json:"quantity"`
}

// CalcRequest é o payload para GET /me/shipment/calculate (enviado como query params).
type CalcRequest struct {
	// FromCEP: CEP de origem (somente dígitos, 8 chars).
	FromCEP string `json:"from"`
	// ToCEP: CEP de destino (somente dígitos, 8 chars).
	ToCEP    string        `json:"to"`
	Products []CalcProduct `json:"products"`
}

// MECompany é a transportadora retornada no campo "company" do ME.
type MECompany struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Picture string `json:"picture,omitempty"`
}

// ServiceOption é uma opção de serviço retornada pelo /me/shipment/calculate.
type ServiceOption struct {
	ServiceID    int             `json:"id"`
	Name         string          `json:"name"`
	Price        decimal.Decimal `json:"price"`
	DeliveryDays int             `json:"delivery_time"`
	// Company: transportadora (objeto com id+name). Campo "error" presente quando indisponível.
	Company MECompany `json:"company"`
	Error   string    `json:"error,omitempty"`
}

// MEAddress representa um endereço no formato esperado pela ME API.
type MEAddress struct {
	Name       string `json:"name"`
	Phone      string `json:"phone,omitempty"`
	Email      string `json:"email,omitempty"`
	Document   string `json:"document,omitempty"`
	Address    string `json:"address"`
	Complement string `json:"complement,omitempty"`
	Number     string `json:"number"`
	District   string `json:"district,omitempty"`
	City       string `json:"city"`
	StateAbbr  string `json:"state_abbr"`
	CountryID  string `json:"country_id"`
	PostalCode string `json:"postal_code"`
}

// MEOrderProduct representa um produto no pedido para criação de etiqueta.
type MEOrderProduct struct {
	Name         string          `json:"name"`
	Quantity     int             `json:"quantity"`
	UnitaryValue decimal.Decimal `json:"unitary_value"`
	Weight       float64         `json:"weight"`
	// Dimensões em centímetros.
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	Length float64 `json:"length"`
}

// MEOrder é o payload para POST /me/cart (criação de etiqueta no carrinho ME).
type MEOrder struct {
	ServiceID int `json:"service"`
	// Agência (opcional; nil → ME escolhe a mais próxima).
	AgencyID *int             `json:"agency,omitempty"`
	From     MEAddress        `json:"from"`
	To       MEAddress        `json:"to"`
	Products []MEOrderProduct `json:"products"`
	Volumes  []MEVolume       `json:"volumes"`
	Options  MEOrderOptions   `json:"options"`
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
	Receipt bool `json:"receipt"`
	// OwnHand: mãos próprias.
	OwnHand bool `json:"own_hand"`
	// Collect: coleta na origem.
	Collect bool `json:"collect"`
	// NonCommercial: declaração de não-comercial.
	NonCommercial bool `json:"non_commercial"`
	// Invoice: nota fiscal (opcional).
	Invoice *MEInvoice `json:"invoice,omitempty"`
}

// MEInvoice representa os dados da nota fiscal do pedido.
type MEInvoice struct {
	Key string `json:"key"`
}

// meCalcPayload é o formato exato esperado pela ME API para cotação.
// A ME usa "from" e "to" como objetos com campo "postal_code".
type meCalcPayload struct {
	From struct {
		PostalCode string `json:"postal_code"`
	} `json:"from"`
	To struct {
		PostalCode string `json:"postal_code"`
	} `json:"to"`
	Products []CalcProduct `json:"products"`
}

// meShipmentResponse é o formato de retorno do POST /me/cart.
type meShipmentResponse struct {
	ID string `json:"id"`
}

// meGenerateResponse é o formato de retorno do POST /me/shipment/generate.
// A ME v2 retorna um array top-level: [{"id":"...","label":"..."}].
// Mantemos também os formatos alternativos (objeto único ou {shipments:[]})
// para compatibilidade com respostas de sandbox/legado.
type meGenerateResponse struct {
	// Formatos objeto (sandbox/legado): {id,label} ou {shipments:[{id,label}]}.
	Shipments []struct {
		ID       string `json:"id"`
		LabelURL string `json:"label"`
	} `json:"shipments"`
	ID       string `json:"id"`
	LabelURL string `json:"label"`
}

// meGenerateItem é um elemento do array retornado pelo POST /me/shipment/generate (v2 real).
type meGenerateItem struct {
	ID       string `json:"id"`
	LabelURL string `json:"label"`
}

// AUDIT-2026-07-30 #9: meTrackingResponse removido — era o shape do path GET
// que nunca existiu de verdade (ver TrackShipment). Response real é
// map[shipment_id]ShipmentTrackingEvents.

// MEBalanceCharge é a resposta do POST /me/balance (geração de PIX para recarga ME).
type MEBalanceCharge struct {
	ID          string          `json:"id"`
	Status      string          `json:"status"`
	Amount      decimal.Decimal `json:"amount"`
	Link        string          `json:"link"`
	QRCode      string          `json:"qr_code"`
	QRCodeImage string          `json:"qr_code_image"`
	Expiry      string          `json:"expiry"`
}

// MEBalanceInfo é a resposta do GET /me/balance (saldo atual da conta ME).
type MEBalanceInfo struct {
	Balance decimal.Decimal `json:"balance"`
}

// ─── Métodos do cliente ────────────────────────────────────────────────────────

// Calculate chama GET /me/shipment/calculate (endpoint autenticado) e retorna as opções.
//
// CRIT-01: Este método DEVE ser chamado server-side antes de criar qualquer etiqueta.
// O preço retornado aqui é o único valor autorizado a ser usado na criação/cobrança.
// Nunca confiar em preço enviado pelo cliente.
//
// O resultado é armazenado em cache (wc_me_shipment_cache, TTL 10 min) pelo handler.
// A ME API usa GET com query params no formato from[postal_code]=&products[N][field]=.
// Endpoint público sem /me/ existe mas não retorna preços negociados — usar /me/.
func (c *MEClient) Calculate(ctx context.Context, req CalcRequest) ([]ServiceOption, error) {
	q := url.Values{}
	q.Set("from[postal_code]", req.FromCEP)
	q.Set("to[postal_code]", req.ToCEP)
	for i, p := range req.Products {
		prefix := fmt.Sprintf("products[%d]", i)
		q.Set(prefix+"[id]", fmt.Sprintf("p%d", i))
		q.Set(prefix+"[width]", fmt.Sprintf("%g", p.Width))
		q.Set(prefix+"[height]", fmt.Sprintf("%g", p.Height))
		q.Set(prefix+"[length]", fmt.Sprintf("%g", p.Length))
		q.Set(prefix+"[weight]", fmt.Sprintf("%g", p.Weight))
		q.Set(prefix+"[insurance_value]", p.InsuranceValue.StringFixed(2))
		q.Set(prefix+"[quantity]", fmt.Sprintf("%d", p.Quantity))
	}

	resp, err := c.doRequest(ctx, http.MethodGet, "/me/shipment/calculate?"+q.Encode(), nil)
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

	resp, err := c.doRequest(ctx, http.MethodPost, "/me/cart", body)
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

// CheckoutShipment chama POST /me/shipment/checkout para pagar o envio usando saldo ME.
// Deve ser chamado ANTES de GenerateLabel — ME rejeita generate com "Envio não está pago"
// se o checkout não foi realizado.
func (c *MEClient) CheckoutShipment(ctx context.Context, shipmentID string) error {
	payload := map[string]any{
		"orders": []string{shipmentID},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("[senderzz_labels] CheckoutShipment: marshal: %w", err)
	}
	resp, err := c.doRequest(ctx, http.MethodPost, "/me/shipment/checkout", body)
	if err != nil {
		return fmt.Errorf("[senderzz_labels] CheckoutShipment: requisição: %w", err)
	}
	defer resp.Body.Close()
	// AUDIT-2026-07-27: ME também responde 204 (No Content) em checkout bem-sucedido
	// — só 200/201 aceitos derrubava um retry legítimo com "status 204: " (corpo
	// vazio, nem é erro de verdade).
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("[senderzz_labels] CheckoutShipment: ME retornou status %d: %s",
			resp.StatusCode, truncate(string(raw), 200))
	}
	slog.Info("[senderzz_labels] checkout ME concluído", "shipment_id", shipmentID)
	return nil
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

	resp, err := c.doRequest(ctx, http.MethodPost, "/me/shipment/generate", body)
	if err != nil {
		return "", fmt.Errorf("[senderzz_labels] GenerateLabel: requisição: %w", err)
	}
	defer resp.Body.Close()

	rawBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("[senderzz_labels] GenerateLabel: ME retornou status %d: %s",
			resp.StatusCode, truncate(string(rawBody), 200))
	}

	// ME v2 retorna array top-level: [{"id":"...","label":"..."}].
	// Tentamos o array primeiro (formato real); caímos no objeto se falhar (sandbox/legado).
	labelURL := pickLabelURLFromRaw(rawBody, shipmentID)
	if labelURL == "" {
		return "", fmt.Errorf("[senderzz_labels] GenerateLabel: ME retornou URL vazia para shipment_id=%s (raw: %s)",
			shipmentID, truncate(string(rawBody), 200))
	}

	slog.Info("[senderzz_labels] etiqueta gerada pela ME",
		"shipment_id", shipmentID,
		"label_url", labelURL,
	)

	return labelURL, nil
}

// PrintLabel chama POST /me/shipment/print para buscar a URL de uma etiqueta
// JÁ GERADA. AUDIT-2026-07-27: GenerateLabel é assíncrono do lado da ME — a 1ª
// chamada pode responder "encaminhado para geração" (sem URL ainda) e uma
// chamada seguinte a GenerateLabel no MESMO shipment retorna "O envio ja está
// gerado" (também sem URL, é mensagem de erro, não de sucesso). GenerateLabel
// só DISPARA a geração; PrintLabel é o endpoint que RECUPERA a URL de uma
// etiqueta que a ME já terminou de gerar (job #3/pedido 1633 ficava preso
// re-tentando GenerateLabel pra sempre porque nunca buscava a URL desse jeito).
func (c *MEClient) PrintLabel(ctx context.Context, shipmentID string) (string, error) {
	payload := map[string]any{
		"orders": []string{shipmentID},
		"mode":   "public",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("[senderzz_labels] PrintLabel: marshal payload: %w", err)
	}
	resp, err := c.doRequest(ctx, http.MethodPost, "/me/shipment/print", body)
	if err != nil {
		return "", fmt.Errorf("[senderzz_labels] PrintLabel: requisição: %w", err)
	}
	defer resp.Body.Close()

	rawBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("[senderzz_labels] PrintLabel: ME retornou status %d: %s",
			resp.StatusCode, truncate(string(rawBody), 200))
	}

	var out struct {
		URL string `json:"url"`
	}
	if jErr := json.Unmarshal(rawBody, &out); jErr != nil || out.URL == "" {
		return "", fmt.Errorf("[senderzz_labels] PrintLabel: URL vazia para shipment_id=%s (raw: %s)",
			shipmentID, truncate(string(rawBody), 200))
	}

	slog.Info("[senderzz_labels] etiqueta recuperada via print", "shipment_id", shipmentID, "label_url", out.URL)
	return out.URL, nil
}

// PrintLabelsBatch chama POST /me/shipment/print com VÁRIOS shipment_ids de
// uma vez — a ME já devolve UM PDF combinado com todas as etiquetas (mesmo
// endpoint de PrintLabel, só que com o array cheio em vez de 1 item). Pedido
// dono 2026-07-28: "abre etiquetas juntas para impressão" — não precisa
// concatenar PDF no nosso lado, a própria ME já faz isso.
func (c *MEClient) PrintLabelsBatch(ctx context.Context, shipmentIDs []string) (string, error) {
	if len(shipmentIDs) == 0 {
		return "", fmt.Errorf("[senderzz_labels] PrintLabelsBatch: nenhum shipment_id informado")
	}
	payload := map[string]any{
		"orders": shipmentIDs,
		"mode":   "public",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("[senderzz_labels] PrintLabelsBatch: marshal payload: %w", err)
	}
	resp, err := c.doRequest(ctx, http.MethodPost, "/me/shipment/print", body)
	if err != nil {
		return "", fmt.Errorf("[senderzz_labels] PrintLabelsBatch: requisição: %w", err)
	}
	defer resp.Body.Close()

	rawBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("[senderzz_labels] PrintLabelsBatch: ME retornou status %d: %s",
			resp.StatusCode, truncate(string(rawBody), 200))
	}
	var out struct {
		URL string `json:"url"`
	}
	if jErr := json.Unmarshal(rawBody, &out); jErr != nil || out.URL == "" {
		return "", fmt.Errorf("[senderzz_labels] PrintLabelsBatch: URL vazia (raw: %s)", truncate(string(rawBody), 200))
	}
	slog.Info("[senderzz_labels] etiquetas em lote recuperadas", "count", len(shipmentIDs), "label_url", out.URL)
	return out.URL, nil
}

// PrintFileURL chama GET /me/imprimir/{formato}/{id} — endpoint documentado
// (docs.melhorenvio.com.br/reference/impressao-de-etiquetas-em-arquivo) que
// devolve um ARRAY com URL assinada S3 (pdf/zpl/jpeg) do arquivo já pronto
// pra download direto, sem sessão de navegador/login na ME. AUDIT-2026-07-28:
// confirmado ao vivo — /me/shipment/print (usado por PrintLabel) só devolve
// a página HTML /imprimir/{code} que exige cookie de sessão logada; ESTE
// endpoint devolve bytes reais de PDF (testado: 116KB, magic bytes %PDF-1.3).
// id = shipment_id (mesmo usado em GenerateLabel/PrintLabel), NÃO wc_order_id.
func (c *MEClient) PrintFileURL(ctx context.Context, shipmentID, format string) (string, error) {
	path := "/me/imprimir/" + format + "/" + shipmentID
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", fmt.Errorf("[senderzz_labels] PrintFileURL: requisição: %w", err)
	}
	defer resp.Body.Close()

	rawBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("[senderzz_labels] PrintFileURL: ME retornou status %d: %s",
			resp.StatusCode, truncate(string(rawBody), 200))
	}

	var urls []string
	if jErr := json.Unmarshal(rawBody, &urls); jErr != nil || len(urls) == 0 {
		return "", fmt.Errorf("[senderzz_labels] PrintFileURL: resposta inesperada para shipment_id=%s (raw: %s)",
			shipmentID, truncate(string(rawBody), 200))
	}

	slog.Info("[senderzz_labels] arquivo de etiqueta recuperado", "shipment_id", shipmentID, "format", format)
	return urls[0], nil
}

// DaceFileURL chama GET /me/imprimir/dace/{formato}/{id} — mesmo padrão de
// PrintFileURL, endpoint irmão documentado (docs.melhorenvio.com.br/reference/
// impressao-dace) pra Declaração de Conteúdo Simplificada. Substitui
// GenerateDeclaration (GET /me/shipment/declaration), que devolvia a página
// HTML da home da ME em vez de JSON — endpoint certo pra arquivo é este.
func (c *MEClient) DaceFileURL(ctx context.Context, shipmentID, format string) (string, error) {
	path := "/me/imprimir/dace/" + format + "/" + shipmentID
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", fmt.Errorf("[senderzz_labels] DaceFileURL: requisição: %w", err)
	}
	defer resp.Body.Close()

	rawBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("[senderzz_labels] DaceFileURL: ME retornou status %d: %s",
			resp.StatusCode, truncate(string(rawBody), 200))
	}

	var urls []string
	if jErr := json.Unmarshal(rawBody, &urls); jErr != nil || len(urls) == 0 {
		return "", fmt.Errorf("[senderzz_labels] DaceFileURL: resposta inesperada para shipment_id=%s (raw: %s)",
			shipmentID, truncate(string(rawBody), 200))
	}

	slog.Info("[senderzz_labels] arquivo DACE recuperado", "shipment_id", shipmentID, "format", format)
	return urls[0], nil
}

// DownloadFile baixa bytes de uma URL de arquivo já assinada (S3, devolvida
// por PrintFileURL) e retorna o corpo bruto. SSRF guard: exige https e host
// dentro do domínio esperado do storage da ME — nunca segue URL manipulada
// pra host interno mesmo que a resposta da ME venha adulterada.
func (c *MEClient) DownloadFile(ctx context.Context, fileURL string) ([]byte, error) {
	u, err := url.Parse(fileURL)
	if err != nil || u.Scheme != "https" || !strings.HasSuffix(u.Hostname(), ".amazonaws.com") {
		return nil, fmt.Errorf("[senderzz_labels] DownloadFile: URL de arquivo suspeita (SSRF guard): %s", truncate(fileURL, 200))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, fmt.Errorf("[senderzz_labels] DownloadFile: criar requisição: %w", err)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("[senderzz_labels] DownloadFile: requisição: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("[senderzz_labels] DownloadFile: status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// GenerateDeclaration chama GET /me/shipment/declaration — Declaração de
// Conteúdo simplificada (DACE) da ME. AUDIT-2026-07-28: a rota só aceita
// GET/HEAD (confirmado ao vivo — POST devolve 405 "Supported methods: GET,
// HEAD"), diferente de print/generate (que são POST). Query string
// orders[]=<id>&mode=public — mesmos parâmetros, método diferente.
func (c *MEClient) GenerateDeclaration(ctx context.Context, shipmentIDs []string) (string, error) {
	if len(shipmentIDs) == 0 {
		return "", fmt.Errorf("[senderzz_labels] GenerateDeclaration: nenhum shipment_id informado")
	}
	q := url.Values{}
	for _, id := range shipmentIDs {
		q.Add("orders[]", id)
	}
	q.Set("mode", "public")
	path := "/me/shipment/declaration?" + q.Encode()
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", fmt.Errorf("[senderzz_labels] GenerateDeclaration: requisição: %w", err)
	}
	defer resp.Body.Close()

	rawBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("[senderzz_labels] GenerateDeclaration: ME retornou status %d: %s",
			resp.StatusCode, truncate(string(rawBody), 200))
	}
	var out struct {
		URL string `json:"url"`
	}
	if jErr := json.Unmarshal(rawBody, &out); jErr != nil || out.URL == "" {
		return "", fmt.Errorf("[senderzz_labels] GenerateDeclaration: URL vazia (raw: %s)", truncate(string(rawBody), 200))
	}
	slog.Info("[senderzz_labels] declaração de conteúdo gerada", "count", len(shipmentIDs), "url", out.URL)
	return out.URL, nil
}

// RequestCancelShipment chama POST /me/shipment/cancel — cancelamento de um
// envio JÁ CHECADO/PAGO (posterior ao carrinho). Distinto de CancelShipment
// (DELETE /me/cart/{id}, só funciona ANTES do checkout — usado no rollback de
// falha de emit.go). Etiqueta já emitida/gerada precisa deste endpoint; a ME
// processa a solicitação e credita o saldo pooled de volta (assíncrono do lado
// deles), mas o reembolso pro PRODUTOR na carteira TPC é nosso (ver Estornar).
func (c *MEClient) RequestCancelShipment(ctx context.Context, shipmentID string) error {
	if shipmentID == "" {
		return fmt.Errorf("[senderzz_labels] RequestCancelShipment: shipment_id vazio")
	}
	payload := map[string]any{
		"order": map[string]any{
			"id": shipmentID,
			// reason_id=2: único valor testado e aceito pela API real entre os
			// tentados (1,3,7-11 recusados com "reason_id selecionado é inválido";
			// 2,4,5 aceitos) — ME não documenta o catálogo completo publicamente.
			"reason_id":   2,
			"description": "Cancelado via FalkLog",
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("[senderzz_labels] RequestCancelShipment: marshal: %w", err)
	}
	resp, err := c.doRequest(ctx, http.MethodPost, "/me/shipment/cancel", body)
	if err != nil {
		return fmt.Errorf("[senderzz_labels] RequestCancelShipment: requisição: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("[senderzz_labels] RequestCancelShipment: ME retornou status %d: %s",
			resp.StatusCode, truncate(string(raw), 200))
	}
	slog.Info("[senderzz_labels] cancelamento solicitado à ME", "shipment_id", shipmentID)
	return nil
}

// TrackShipment chama POST /me/shipment/tracking e retorna o status atual.
// Usado pelo job ProcessSyncTracking para sincronizar wc_me_labels.status.
//
// AUDIT-2026-07-30 #9 (dono: "ARRUMA" — job quebrando com "invalid character
// '<'"): path antigo (GET /me/shipment/tracking/{code}, chamado com o CÓDIGO
// DE RASTREIO da transportadora) nunca existiu — a ME devolvia a página HTML
// do site, não JSON, todo job falhava. O endpoint real é POST com
// {"orders":[shipment_id]} (confirmado em produção, mesmo usado em
// GetShipmentTrackingEvents) e espera o me_shipment_id (UUID interno da ME),
// NÃO o código de rastreio do Jadlog/Loggi/Correios — testado com o código
// real e a ME devolve corpo vazio (não reconhece).
func (c *MEClient) TrackShipment(ctx context.Context, shipmentID string) (string, error) {
	if shipmentID == "" {
		return "", fmt.Errorf("[senderzz_labels] TrackShipment: shipment_id vazio")
	}
	payload, err := json.Marshal(map[string]any{"orders": []string{shipmentID}})
	if err != nil {
		return "", fmt.Errorf("[senderzz_labels] TrackShipment: montar payload: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPost, "/me/shipment/tracking", payload)
	if err != nil {
		return "", fmt.Errorf("[senderzz_labels] TrackShipment: requisição: %w", err)
	}
	defer resp.Body.Close()

	rawBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("[senderzz_labels] TrackShipment: ME retornou status %d: %s",
			resp.StatusCode, truncate(string(rawBody), 200))
	}

	var result map[string]ShipmentTrackingEvents
	if err := json.Unmarshal(rawBody, &result); err != nil {
		return "", fmt.Errorf("[senderzz_labels] TrackShipment: decode response: %w", err)
	}
	events, ok := result[shipmentID]
	if !ok {
		return "", fmt.Errorf("[senderzz_labels] TrackShipment: shipment_id %s não encontrado na ME", shipmentID)
	}

	slog.Info("[senderzz_labels] rastreamento atualizado",
		"shipment_id", shipmentID,
		"status", events.Status,
	)

	return events.Status, nil
}

// meOrderDetail é o formato de GET /me/orders/{id} — só os campos que usamos.
// A ME atribui o código de rastreio (campo "tracking") assim que a transportadora
// aceita a postagem; costuma já estar disponível logo após o checkout/geração da
// etiqueta (não precisa esperar sync de status).
//
// AUDIT-2026-07-29: "self_tracking" NÃO é o código real da transportadora —
// tentativa de usá-lo como fallback (achando que era um código Jadlog só
// "ainda não sincronizado") gravou um valor incorreto no pedido 1665 (dono
// confirmou ao vivo: o painel ME real mostra Jadlog com código NUMÉRICO
// próprio tipo "612148759", nada parecido com "ME...BR"). Revertido — usa
// só "tracking" mesmo, sem inventar fallback sem confirmação.
type meOrderDetail struct {
	Tracking string `json:"tracking"`
}

// GetShipmentTracking chama GET /me/orders/{shipmentID} e devolve o código de
// rastreio (campo "tracking") já atribuído pela transportadora, se houver.
// Retorna ("", nil) — não erro — quando a ME ainda não atribuiu código (etiqueta
// gerada mas não postada); chamador decide se tenta de novo depois.
func (c *MEClient) GetShipmentTracking(ctx context.Context, shipmentID string) (string, error) {
	if shipmentID == "" {
		return "", fmt.Errorf("[senderzz_labels] GetShipmentTracking: shipment_id vazio")
	}
	resp, err := c.doRequest(ctx, http.MethodGet, "/me/orders/"+shipmentID, nil)
	if err != nil {
		return "", fmt.Errorf("[senderzz_labels] GetShipmentTracking: requisição: %w", err)
	}
	defer resp.Body.Close()

	rawBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("[senderzz_labels] GetShipmentTracking: ME retornou status %d: %s",
			resp.StatusCode, truncate(string(rawBody), 200))
	}

	var detail meOrderDetail
	if err := json.Unmarshal(rawBody, &detail); err != nil {
		return "", fmt.Errorf("[senderzz_labels] GetShipmentTracking: decode response: %w", err)
	}

	if detail.Tracking != "" {
		slog.Info("[senderzz_labels] tracking obtido da ME", "shipment_id", shipmentID, "tracking_code", detail.Tracking)
	}
	return detail.Tracking, nil
}

// ShipmentStatus é o retorno de GetShipmentStatus: status atual + timestamp de
// cancelamento (quando houver), usados pra confirmar estorno por remessa.
// AUDIT-2026-07-30 #11 (dono confirmou: "authorization_code... esse é o
// rastreio jadlog"): GET /me/orders/{id} tem MUITO mais campos que os que
// usávamos (só status/canceled_at) — confirmado em produção contra o pedido
// 1665 (authorization_code="612148759" bate com o rastreio real Jadlog que o
// dono forneceu manualmente antes). "tracking" fica null por mais tempo que
// authorization_code, que já vem preenchido desde a emissão. delivery_min/max
// = prazo em dias direto da ME, dá pra calcular previsão de chegada real.
type ShipmentStatus struct {
	Status            string `json:"status"`
	Price             decimal.Decimal `json:"price"` // custo real cobrado pelo Melhor Envio
	AuthorizationCode string `json:"authorization_code"` // rastreio real da transportadora (Jadlog confirmado)
	Tracking          string `json:"tracking"`
	DeliveryMin       int    `json:"delivery_min"` // dias úteis, mínimo
	DeliveryMax       int    `json:"delivery_max"` // dias úteis, máximo
	CreatedAt         string `json:"created_at"`
	PaidAt            string `json:"paid_at"`
	GeneratedAt       string `json:"generated_at"`
	PostedAt          string `json:"posted_at"`
	ReceivedAt        string `json:"received_at"`
	DeliveredAt       string `json:"delivered_at"`
	CanceledAt        string `json:"canceled_at"`
}

// GetShipmentStatus chama GET /me/orders/{shipmentID} e devolve status +
// canceled_at. Usado pra confirmar estorno de cancelamento por remessa
// individual (status=="canceled" && canceled_at != "" == reembolso concluído
// na ME), em vez de inferir por delta de saldo agregado da conta.
func (c *MEClient) GetShipmentStatus(ctx context.Context, shipmentID string) (*ShipmentStatus, error) {
	if shipmentID == "" {
		return nil, fmt.Errorf("[senderzz_labels] GetShipmentStatus: shipment_id vazio")
	}
	resp, err := c.doRequest(ctx, http.MethodGet, "/me/orders/"+shipmentID, nil)
	if err != nil {
		return nil, fmt.Errorf("[senderzz_labels] GetShipmentStatus: requisição: %w", err)
	}
	defer resp.Body.Close()

	rawBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("[senderzz_labels] GetShipmentStatus: ME retornou status %d: %s",
			resp.StatusCode, truncate(string(rawBody), 200))
	}

	var status ShipmentStatus
	if err := json.Unmarshal(rawBody, &status); err != nil {
		return nil, fmt.Errorf("[senderzz_labels] GetShipmentStatus: decode response: %w", err)
	}
	return &status, nil
}

// ShipmentTrackingEvents — timestamps REAIS de cada etapa da etiqueta,
// confirmados via POST /me/shipment/tracking (payload {"orders":[shipment_id]}).
// Formato "YYYY-MM-DD HH:MM:SS" em UTC (sem timezone no JSON — converter no
// caller). Campos vazios ("") = etapa ainda não aconteceu, NUNCA fabricar.
type ShipmentTrackingEvents struct {
	Status              string `json:"status"`
	Tracking            string `json:"tracking"`
	MelhorEnvioTracking string `json:"melhorenvio_tracking"` // AUDIT-2026-07-29: código interno ME, NÃO é o código real da transportadora — nunca usar como tracking_code.
	CreatedAt           string `json:"created_at"`
	PaidAt              string `json:"paid_at"`
	GeneratedAt         string `json:"generated_at"`
	PostedAt            string `json:"posted_at"`
	DeliveredAt         string `json:"delivered_at"`
	CanceledAt          string `json:"canceled_at"`
	ExpiredAt           string `json:"expired_at"`
}

// GetShipmentTrackingEvents chama POST /me/shipment/tracking pra UM shipment e
// devolve os timestamps reais por etapa (confirmado em produção 2026-07-30:
// created_at/paid_at/generated_at/posted_at/delivered_at/canceled_at/expired_at
// — igual ao painel de rastreio público da ME, "Adicionado no sistema" etc.).
func (c *MEClient) GetShipmentTrackingEvents(ctx context.Context, shipmentID string) (*ShipmentTrackingEvents, error) {
	if shipmentID == "" {
		return nil, fmt.Errorf("[senderzz_labels] GetShipmentTrackingEvents: shipment_id vazio")
	}
	payload, err := json.Marshal(map[string]any{"orders": []string{shipmentID}})
	if err != nil {
		return nil, fmt.Errorf("[senderzz_labels] GetShipmentTrackingEvents: montar payload: %w", err)
	}
	resp, err := c.doRequest(ctx, http.MethodPost, "/me/shipment/tracking", payload)
	if err != nil {
		return nil, fmt.Errorf("[senderzz_labels] GetShipmentTrackingEvents: requisição: %w", err)
	}
	defer resp.Body.Close()

	rawBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("[senderzz_labels] GetShipmentTrackingEvents: ME retornou status %d: %s",
			resp.StatusCode, truncate(string(rawBody), 200))
	}

	var result map[string]ShipmentTrackingEvents
	if err := json.Unmarshal(rawBody, &result); err != nil {
		return nil, fmt.Errorf("[senderzz_labels] GetShipmentTrackingEvents: decode response: %w", err)
	}
	events, ok := result[shipmentID]
	if !ok {
		return nil, fmt.Errorf("[senderzz_labels] GetShipmentTrackingEvents: shipment_id %s ausente na resposta", shipmentID)
	}
	return &events, nil
}

// AddBalance chama POST /me/balance para gerar um PIX de recarga na conta ME.
// O produtor paga o PIX e o saldo ME da plataforma é creditado pela ME.
// amount deve ser > 0 (ex: decimal.NewFromFloat(50.0)).
//
// Payload e extração da resposta espelham FIELMENTE go/wallet/internal/melhorenvio
// (tpc_gerar_pix_me original): gateway/slug fixos ("yapay-transparente"/"pix") —
// só "gateway":"pix" dá 422 "campo gateway selecionado é inválido" (não é módulo
// habilitado na conta ME). A resposta da ME varia de nomes de campo por conta/plano,
// por isso a extração é multi-chave (primeira presente e não-vazia), igual ao wallet.
func (c *MEClient) AddBalance(ctx context.Context, amount decimal.Decimal) (*MEBalanceCharge, error) {
	payload := map[string]any{
		"gateway":      "yapay-transparente",
		"slug":         "pix",
		"value":        amount.StringFixed(2),
		"redirect_url": "https://app.falklog.com.br/portal/wallet-expedition",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("[senderzz_labels] AddBalance: marshal: %w", err)
	}
	resp, err := c.doRequest(ctx, http.MethodPost, "/me/balance", body)
	if err != nil {
		return nil, fmt.Errorf("[senderzz_labels] AddBalance: requisição: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil || data == nil {
		data = map[string]any{}
	}
	data = expandJSONStrings(data)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		msg := findFirstString(data, []string{"message", "error"})
		if msg == "" {
			msg = truncate(string(raw), 200)
		}
		return nil, fmt.Errorf("[senderzz_labels] AddBalance: ME retornou status %d: %s", resp.StatusCode, msg)
	}

	qrCode := findFirstString(data, []string{"copy_paste", "copyPaste", "copia_cola", "pix_code", "digitable", "emv", "brcode", "payload"})
	qrImage := normalizeImgSrc(findFirstString(data, []string{"qr_code", "qrcode", "qrCode", "qrcode_base64", "qr_code_base64", "image"}))
	link := findFirstString(data, []string{"link", "redirect", "url_payment", "url", "payment_url", "paymentUrl", "checkout_url"})
	// Fallback: quando a ME só devolve o `link` (URL do SVG/PNG do QR, sem campo
	// qr_code/qrcode separado — visto na prática, conta itau/vindi), usa o próprio
	// link como <img src>. Sem isso o front mostra "QR Code indisponível" mesmo
	// tendo um QR real disponível (só o copia-cola funcionava).
	if qrImage == "" && meHTTPRe.MatchString(link) {
		qrImage = link
	}
	charge := MEBalanceCharge{
		ID:          findFirstString(data, []string{"pix_id", "payment_id", "transaction_id", "transactionId", "id", "charge_id"}),
		Status:      findFirstString(data, []string{"status"}),
		Amount:      amount,
		Link:        link,
		QRCode:      qrCode,
		QRCodeImage: qrImage,
		Expiry:      findFirstString(data, []string{"expire_at", "expires_at", "expiration_date", "due_date"}),
	}
	if qrCode == "" && charge.ID == "" {
		return nil, fmt.Errorf("[senderzz_labels] AddBalance: resposta da ME sem código PIX reconhecível: %s", truncate(string(raw), 200))
	}
	slog.Info("[senderzz_labels] PIX de recarga ME gerado", "amount", amount.StringFixed(2), "charge_id", charge.ID)
	return &charge, nil
}

// ── Extração multi-chave da resposta ME (porte fiel de go/wallet/internal/melhorenvio) ──

func findFirstString(data any, keys []string) string {
	m, ok := data.(map[string]any)
	if !ok {
		return ""
	}
	for _, k := range keys {
		if v, exists := m[k]; exists {
			if s := meScalarToString(v); s != "" {
				return s
			}
		}
	}
	for _, v := range m {
		if sub, ok := v.(map[string]any); ok {
			if f := findFirstString(sub, keys); f != "" {
				return f
			}
		}
	}
	return ""
}

func meScalarToString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x))
		}
		return fmt.Sprintf("%v", x)
	case bool:
		if x {
			return "1"
		}
		return ""
	default:
		return ""
	}
}

func expandJSONStrings(data any) map[string]any {
	m, ok := data.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	for k, v := range m {
		m[k] = expandJSONStringsAny(v)
	}
	return m
}

func expandJSONStringsAny(data any) any {
	switch x := data.(type) {
	case map[string]any:
		for k, v := range x {
			x[k] = expandJSONStringsAny(v)
		}
		return x
	case []any:
		for i, v := range x {
			x[i] = expandJSONStringsAny(v)
		}
		return x
	case string:
		t := strings.TrimSpace(x)
		if t != "" && (strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[")) {
			var decoded any
			if err := json.Unmarshal([]byte(t), &decoded); err == nil {
				switch decoded.(type) {
				case map[string]any, []any:
					return expandJSONStringsAny(decoded)
				}
			}
		}
		return x
	default:
		return data
	}
}

var meBase64Re = regexp.MustCompile(`^[A-Za-z0-9+/=\r\n]+$`)
var meWhitespaceRe = regexp.MustCompile(`\s+`)
var meHTTPRe = regexp.MustCompile(`(?i)^https?://`)

// normalizeImgSrc espelha tpc_pix_normalize_img_src: aceita data-URI, URL http(s)
// ou base64 cru (nesse caso monta o data-URI pro <img src> funcionar direto).
func normalizeImgSrc(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if strings.HasPrefix(v, "data:image") {
		return v
	}
	if meHTTPRe.MatchString(v) {
		return v
	}
	if meBase64Re.MatchString(v) && len(v) > 80 {
		return "data:image/png;base64," + meWhitespaceRe.ReplaceAllString(v, "")
	}
	return v
}

// GetBalance chama GET /me/balance e retorna o saldo atual da conta ME da plataforma.
func (c *MEClient) GetBalance(ctx context.Context) (decimal.Decimal, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/me/balance", nil)
	if err != nil {
		return decimal.Zero, fmt.Errorf("[senderzz_labels] GetBalance: requisição: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return decimal.Zero, fmt.Errorf("[senderzz_labels] GetBalance: ME retornou status %d: %s",
			resp.StatusCode, truncate(string(raw), 200))
	}
	var info MEBalanceInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return decimal.Zero, fmt.Errorf("[senderzz_labels] GetBalance: decode response: %w", err)
	}
	return info.Balance, nil
}

// MECompanyService é um serviço (ex.: PAC, SEDEX) dentro de uma transportadora,
// retornado por GET /me/shipment/companies.
type MECompanyService struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// MECompanyFull é a transportadora com seu catálogo de serviços — devolvida por
// GET /me/shipment/companies. Usada só p/ montar o catálogo administrável
// (senderzz_enabled_carriers_map); a cotação em si usa Calculate.
type MECompanyFull struct {
	ID       int                `json:"id"`
	Name     string             `json:"name"`
	Picture  string             `json:"picture,omitempty"`
	Services []MECompanyService `json:"services"`
}

// ListCompanies chama GET /me/shipment/companies e devolve o catálogo completo
// de transportadoras + serviços da conta ME da plataforma. Usado só pela tela
// admin de expedição (não pelo checkout — Calculate é o caminho quente).
func (c *MEClient) ListCompanies(ctx context.Context) ([]MECompanyFull, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/me/shipment/companies", nil)
	if err != nil {
		return nil, fmt.Errorf("[senderzz_labels] ListCompanies: requisição: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("[senderzz_labels] ListCompanies: ME retornou status %d: %s",
			resp.StatusCode, truncate(string(raw), 200))
	}
	var out []MECompanyFull
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("[senderzz_labels] ListCompanies: decode response: %w", err)
	}
	return out, nil
}

// CancelShipment chama DELETE /me/cart/{id} para remover o pedido do carrinho ME.
// Deve ser chamado antes de marcar a etiqueta como canceled no banco.
func (c *MEClient) CancelShipment(ctx context.Context, shipmentID string) error {
	if shipmentID == "" {
		return fmt.Errorf("[senderzz_labels] CancelShipment: shipment_id vazio")
	}

	resp, err := c.doRequest(ctx, http.MethodDelete, "/me/cart/"+shipmentID, nil)
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
	// P2-02: revalida a base URL antes de cada chamada (fail-closed / defesa em
	// profundidade). Se inválida, NÃO faz o request — o Bearer token nunca é
	// enviado a um destino fora da whitelist.
	if err := validateMEBaseURL(c.BaseURL); err != nil {
		slog.Error("[senderzz_labels] base URL ME inválida — requisição bloqueada (P2-02 SSRF guard)",
			"err", err,
			"base_url", c.BaseURL,
		)
		return nil, fmt.Errorf("base URL ME inválida (P2-02 SSRF guard): %w", err)
	}

	reqURL := c.BaseURL + path

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
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

// pickLabelURLFromRaw extrai a URL da etiqueta do body cru do POST /me/shipment/generate.
//
// Ordem de tentativas (do mais provável ao menos):
//  1. Array top-level [{"id":"...","label":"..."}] — formato real ME v2 (prod).
//  2. Objeto único {"id":"...","label":"..."} — sandbox/legado.
//  3. Objeto com chave "shipments" {"shipments":[{"id":"...","label":"..."}]} — legado alternativo.
//
// Retorna "" se nenhuma URL for encontrada; o chamador trata como erro.
func pickLabelURLFromRaw(raw []byte, _ string) string {
	// 1. Tenta array top-level (formato padrão ME v2).
	var arr []meGenerateItem
	if json.Unmarshal(raw, &arr) == nil && len(arr) > 0 && arr[0].LabelURL != "" {
		return arr[0].LabelURL
	}
	// 2+3. Fallback: objeto único ou objeto com "shipments".
	var obj meGenerateResponse
	if json.Unmarshal(raw, &obj) == nil {
		return pickLabelURL(obj)
	}
	return ""
}

// pickLabelURL extrai a URL da etiqueta de um meGenerateResponse (formatos objeto).
// Prioriza o campo "label" no topo; cai em shipments[0].label.
// Mantida para os testes unitários existentes de client_test.go.
func pickLabelURL(resp meGenerateResponse) string {
	if resp.LabelURL != "" {
		return resp.LabelURL
	}
	if len(resp.Shipments) > 0 {
		return resp.Shipments[0].LabelURL
	}
	return ""
}

// truncate retorna os primeiros n caracteres da string s (para mensagens de erro seguras).
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
