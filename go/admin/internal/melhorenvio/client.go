// Package melhorenvio — cliente da API do Melhor Envio para emissão de PIX.
//
// Duplicado de go/wallet/internal/melhorenvio/client.go (serviços Go são
// módulos SEPARADOS, sem import cruzado — mesmo padrão já usado em
// go/labels/internal/me/client.go). Usado por TpcClientesHandler.CreateRecarga
// (go/admin/internal/handlers/tpc_clientes.go) para emitir PIX REAL (não mais
// stub) ao criar uma recarga manual de carteira de frete para um produtor.
//
// Configuração via env:
//   - ME_API_URL — base da API (default: https://melhorenvio.com.br/api/v2).
//   - ME_TOKEN   — token OAuth do Melhor Envio. SEM token → ErrSemToken.
package melhorenvio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

// ErrSemToken é retornado quando ME_TOKEN não está configurado.
var ErrSemToken = errors.New("token ME não configurado")

const userAgent = "Senderzz Logistics (suporte@app.senderzz.com.br)"
const defaultAPIBase = "https://melhorenvio.com.br/api/v2"

type Client struct {
	apiBase    string
	token      string
	httpClient *http.Client
}

func NewClient() *Client {
	base := strings.TrimRight(os.Getenv("ME_API_URL"), "/")
	if base == "" {
		base = defaultAPIBase
	}
	return &Client{
		apiBase:    base,
		token:      os.Getenv("ME_TOKEN"),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) HasToken() bool {
	return c.token != ""
}

// CalcProduct/CalcRequest/ServiceOption/MECompany — espelho de
// go/labels/internal/me/client.go (mesmo motivo: módulos separados, sem
// import cruzado). Usado só pela tela admin-only de auditoria de cotação
// (AUDIT-2026-07-29, dono: "mostre o custo de todos os fretes cotados e o
// serviço mais barato selecionado, SOMENTE NO ADMIN").
type CalcProduct struct {
	Height         float64 `json:"height"`
	Width          float64 `json:"width"`
	Length         float64 `json:"length"`
	Weight         float64 `json:"weight"`
	InsuranceValue float64 `json:"insurance_value"`
	Quantity       int     `json:"quantity"`
}

type CalcRequest struct {
	FromCEP  string
	ToCEP    string
	Products []CalcProduct
}

type MECompany struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type ServiceOption struct {
	ServiceID    int         `json:"id"`
	Name         string      `json:"name"`
	Price        json.Number `json:"price"`
	DeliveryDays int         `json:"delivery_time"`
	Company      MECompany   `json:"company"`
	Error        string      `json:"error,omitempty"`
}

// Calculate chama GET /me/shipment/calculate — mesma cotação usada na emissão
// real (labels-service), aqui só pra AUDITORIA/leitura (admin nunca cria
// shipment com isso).
func (c *Client) Calculate(ctx context.Context, req CalcRequest) ([]ServiceOption, error) {
	if c.token == "" {
		return nil, ErrSemToken
	}
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
		q.Set(prefix+"[insurance_value]", fmt.Sprintf("%.2f", p.InsuranceValue))
		q.Set(prefix+"[quantity]", fmt.Sprintf("%d", p.Quantity))
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.apiBase+"/me/shipment/calculate?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("montar requisição ME: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.token)
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", userAgent)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("chamada à API ME falhou: %w", err)
	}
	defer resp.Body.Close()

	rawResp, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ME erro HTTP %d: %s", resp.StatusCode, string(rawResp))
	}

	var options []ServiceOption
	if err := json.Unmarshal(rawResp, &options); err != nil {
		return nil, fmt.Errorf("decodificar resposta ME: %w", err)
	}
	return options, nil
}

// PixResult espelha o array de retorno de tpc_gerar_pix_me().
type PixResult struct {
	PixID     string
	QRSrc     string
	CopiaCola string
	Link      string
	ExpiraEm  string
	ExpiresTS int64
}

type pixCreateRequest struct {
	Gateway     string `json:"gateway"`
	Slug        string `json:"slug"`
	Value       string `json:"value"`
	RedirectURL string `json:"redirect_url"`
}

// GerarPix emite um PIX via POST /api/v2/me/balance.
func (c *Client) GerarPix(ctx context.Context, valor float64, redirectURL string) (*PixResult, error) {
	if c.token == "" {
		return nil, ErrSemToken
	}

	reqBody := pixCreateRequest{
		Gateway:     "yapay-transparente",
		Slug:        "pix",
		Value:       fmt.Sprintf("%.2f", valor),
		RedirectURL: redirectURL,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("serializar corpo PIX: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.apiBase+"/me/balance", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("montar requisição ME: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("chamada à API ME falhou: %w", err)
	}
	defer resp.Body.Close()

	rawResp, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var data map[string]any
	if err := json.Unmarshal(rawResp, &data); err != nil || data == nil {
		data = map[string]any{}
	}
	data = expandJSONStrings(data).(map[string]any)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		msg := findFirstString(data, []string{"message", "error"})
		if msg == "" {
			msg = "Erro ao gerar PIX."
		}
		return nil, fmt.Errorf("ME erro HTTP %d: %s", resp.StatusCode, msg)
	}

	pixID := findFirstString(data, []string{"pix_id", "payment_id", "transaction_id", "transactionId", "id", "charge_id"})
	qrCode := findFirstString(data, []string{"qr_code", "qrcode", "qrCode", "qrcode_base64", "qr_code_base64", "image"})
	copiaCola := findFirstString(data, []string{"copy_paste", "copyPaste", "copia_cola", "pix_code", "digitable", "emv", "brcode", "payload"})
	link := findFirstString(data, []string{"link", "redirect", "url_payment", "url", "payment_url", "paymentUrl", "checkout_url"})
	expiraEm := findFirstString(data, []string{"expire_at", "expires_at", "expiration_date", "due_date", "max_days_to_keep_waiting_payment"})
	if expiraEm == "" {
		expiraEm = defaultExpiration()
	}

	qrSrc := normalizeImgSrc(qrCode)
	if qrSrc == "" && copiaCola != "" {
		qrSrc = qrFromCopyPaste(copiaCola)
	}
	if qrSrc == "" && httpRe.MatchString(link) {
		qrSrc = link
	}

	return &PixResult{
		PixID:     pixID,
		QRSrc:     qrSrc,
		CopiaCola: copiaCola,
		Link:      link,
		ExpiraEm:  expiraEm,
		ExpiresTS: parseTimestamp(expiraEm),
	}, nil
}

func findFirstString(data any, keys []string) string {
	m, ok := data.(map[string]any)
	if !ok {
		return ""
	}
	for _, k := range keys {
		if v, exists := m[k]; exists {
			if s := scalarToString(v); s != "" {
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

func scalarToString(v any) string {
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

func expandJSONStrings(data any) any {
	switch x := data.(type) {
	case map[string]any:
		for k, v := range x {
			x[k] = expandJSONStrings(v)
		}
		return x
	case []any:
		for i, v := range x {
			x[i] = expandJSONStrings(v)
		}
		return x
	case string:
		t := strings.TrimSpace(x)
		if t != "" && (strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[")) {
			var decoded any
			if err := json.Unmarshal([]byte(t), &decoded); err == nil {
				switch decoded.(type) {
				case map[string]any, []any:
					return expandJSONStrings(decoded)
				}
			}
		}
		return x
	default:
		return data
	}
}

var base64Re = regexp.MustCompile(`^[A-Za-z0-9+/=\r\n]+$`)
var whitespaceRe = regexp.MustCompile(`\s+`)
var httpRe = regexp.MustCompile(`(?i)^https?://`)

func normalizeImgSrc(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if strings.HasPrefix(v, "data:image") {
		return v
	}
	if httpRe.MatchString(v) {
		return v
	}
	if base64Re.MatchString(v) && len(v) > 80 {
		return "data:image/png;base64," + whitespaceRe.ReplaceAllString(v, "")
	}
	return ""
}

func qrFromCopyPaste(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return "https://api.qrserver.com/v1/create-qr-code/?size=320x320&data=" + url.QueryEscape(s)
}

const pixValidadeMinutos = 15

func defaultExpiration() string {
	return time.Now().UTC().Add(pixValidadeMinutos * time.Minute).Format("2006-01-02T15:04:05Z")
}

func parseTimestamp(d string) int64 {
	d = strings.TrimSpace(d)
	if d == "" {
		return 0
	}
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, d); err == nil {
			return t.Unix()
		}
	}
	return 0
}
