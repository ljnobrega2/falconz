// Package melhorenvio — cliente da API do Melhor Envio para emissão de PIX.
//
// Porte FIEL de tpc_gerar_pix_me() / tpc_me_api_base() / tpc_me_token()
// (includes/tpc/pix.php). A emissão de PIX usa o endpoint POST /api/v2/me/balance
// com gateway "yapay-transparente" e slug "pix" — exatamente como o WordPress.
//
// Configuração via env (espelha cmd/server/main.go):
//   - ME_API_URL — base da API (default: https://melhorenvio.com.br/api/v2).
//     Em sandbox, configure https://sandbox.melhorenvio.com.br/api/v2.
//   - ME_TOKEN   — token OAuth do Melhor Envio. SEM token → ErrSemToken
//     (erro claro, nunca panic — requisito do porte em dev).
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
// Espelha o WP_Error('sem_token', 'Token ME não configurado.') do PHP.
// O caller deve responder com erro claro (não panic) — em dev sem credencial
// a recarga é cancelada e o usuário recebe a mensagem.
var ErrSemToken = errors.New("token ME não configurado")

// userAgent espelha o User-Agent enviado pelo WP em todas as chamadas ME.
const userAgent = "Senderzz Logistics (suporte@app.senderzz.com.br)"

// defaultAPIBase é o default quando ME_API_URL não está definida.
// Espelha o default documentado em cmd/server/main.go.
const defaultAPIBase = "https://melhorenvio.com.br/api/v2"

// Client encapsula a configuração da API do Melhor Envio.
type Client struct {
	apiBase    string
	token      string
	httpClient *http.Client
}

// NewClient cria um Client lendo ME_API_URL e ME_TOKEN do ambiente.
// Não falha se o token estiver ausente — a falta é detectada em GerarPix
// (ErrSemToken), permitindo que o serviço suba mesmo sem credencial em dev.
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

// HasToken indica se há token configurado (para checagem antecipada pelo caller).
func (c *Client) HasToken() bool {
	return c.token != ""
}

// PixResult espelha o array de retorno de tpc_gerar_pix_me().
type PixResult struct {
	PixID     string // me_pix_id (id/payment_id/transaction_id/charge_id)
	QRSrc     string // imagem QR normalizada (data:image ou URL)
	CopiaCola string // payload EMV "copia e cola"
	Link      string // URL de pagamento (redirect)
	ExpiraEm  string // string de expiração crua retornada pela ME
	ExpiresTS int64  // timestamp Unix da expiração (0 se não parseável)
}

// pixCreateRequest é o corpo do POST /me/balance (espelha o wp_json_encode do PHP).
type pixCreateRequest struct {
	Gateway     string `json:"gateway"`
	Slug        string `json:"slug"`
	Value       string `json:"value"`
	RedirectURL string `json:"redirect_url"`
}

// GerarPix emite um PIX via POST /api/v2/me/balance.
//
// Porte FIEL de tpc_gerar_pix_me(): mesmo gateway, slug, formato de value
// (2 casas decimais, ponto decimal) e mesma extração multi-chave dos campos
// de retorno (tpc_pix_find_first sobre as mesmas listas de chaves).
//
// redirectURL é a URL para onde a ME redireciona o browser após o pagamento.
// O caller monta com o token de segurança HMAC (espelha tpc_pix_gerar_token).
func (c *Client) GerarPix(ctx context.Context, valor float64, redirectURL string) (*PixResult, error) {
	if c.token == "" {
		return nil, ErrSemToken
	}

	reqBody := pixCreateRequest{
		Gateway:     "yapay-transparente",
		Slug:        "pix",
		Value:       fmt.Sprintf("%.2f", valor), // number_format($valor, 2, '.', '')
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
		// is_array($data) || $data = [] no PHP — segue com mapa vazio.
		data = map[string]any{}
	}
	// tpc_pix_expand_json_strings: campos que vêm como string JSON são re-decodificados.
	data = expandJSONStrings(data).(map[string]any)

	// $code !== 200 && $code !== 201 → WP_Error('me_erro_<code>', ...)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		msg := findFirstString(data, []string{"message", "error"})
		if msg == "" {
			msg = "Erro ao gerar PIX."
		}
		return nil, fmt.Errorf("ME erro HTTP %d: %s", resp.StatusCode, msg)
	}

	// Extração multi-chave idêntica ao PHP (tpc_pix_find_first).
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

	return &PixResult{
		PixID:     pixID,
		QRSrc:     qrSrc,
		CopiaCola: copiaCola,
		Link:      link,
		ExpiraEm:  expiraEm,
		ExpiresTS: parseTimestamp(expiraEm),
	}, nil
}

// ─── helpers — porte fiel de pix.php ──────────────────────────────────────────

// findFirstString espelha tpc_pix_find_first: procura a primeira chave presente
// e não-vazia (busca rasa, depois recursiva em sub-arrays). Converte para string.
func findFirstString(data any, keys []string) string {
	m, ok := data.(map[string]any)
	if !ok {
		return ""
	}
	// Busca rasa nas chaves desejadas.
	for _, k := range keys {
		if v, exists := m[k]; exists {
			if s := scalarToString(v); s != "" {
				return s
			}
		}
	}
	// Busca recursiva em sub-mapas (foreach($data as $v) if is_array).
	for _, v := range m {
		if sub, ok := v.(map[string]any); ok {
			if f := findFirstString(sub, keys); f != "" {
				return f
			}
		}
	}
	return ""
}

// scalarToString converte valores escalares do JSON (string/number/bool) para
// string, retornando "" para nil/array. Espelha o cast (string) do PHP.
func scalarToString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		// Números inteiros sem casas decimais: evita "123.000000".
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

// expandJSONStrings espelha tpc_pix_expand_json_strings: strings que são JSON
// (começam com '{' ou '[') são re-decodificadas recursivamente.
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

// normalizeImgSrc espelha tpc_pix_normalize_img_src.
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

// qrFromCopyPaste espelha tpc_pix_qr_from_copy_paste.
func qrFromCopyPaste(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return "https://api.qrserver.com/v1/create-qr-code/?size=220x220&data=" + url.QueryEscape(s)
}

// pixValidadeMinutos espelha tpc_pix_validade_minutos (sempre 15 min).
const pixValidadeMinutos = 15

// defaultExpiration espelha tpc_pix_default_expiration (now + 15 min, formato ISO Z).
func defaultExpiration() string {
	return time.Now().UTC().Add(pixValidadeMinutos * time.Minute).Format("2006-01-02T15:04:05Z")
}

// parseTimestamp espelha tpc_pix_timestamp: tenta parsear a data e retorna o
// Unix timestamp, ou 0 se não for parseável (strtotime falha → 0).
func parseTimestamp(d string) int64 {
	d = strings.TrimSpace(d)
	if d == "" {
		return 0
	}
	// Formatos mais comuns retornados por gateways PIX.
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
