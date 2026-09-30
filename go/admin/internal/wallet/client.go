// Package wallet fornece um cliente HTTP serviço-a-serviço mínimo do
// admin-service para o wallet-service, usado SÓ pra confirmar estornos
// pendentes de cancelamento de expedição (ver AUDIT-2026-07-28).
//
// Espelha o padrão de go/labels/internal/wallet/client.go (HMAC-SHA256 via
// X-Internal-Sig, header assinado sobre o corpo bruto). Não duplicamos por
// import cross-módulo (convenção do projeto), só o pedaço que o admin precisa.
package wallet

import (
	"bytes"
	"context"
	"crypto/hmac"
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
)

type Client struct {
	BaseURL    string
	secret     string
	HTTPClient *http.Client
}

func NewClient() *Client {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("WALLET_SERVICE_URL")), "/")
	secret := os.Getenv("WALLET_INTERNAL_SECRET")
	if baseURL == "" {
		slog.Warn("[senderzz_admin] WALLET_SERVICE_URL ausente — confirmação de estorno pendente DESATIVADA")
	} else if secret == "" {
		slog.Error("[senderzz_admin] WALLET_SERVICE_URL configurada mas WALLET_INTERNAL_SECRET vazio — confirmação de estorno falhará (fail-closed)")
	}
	return &Client{BaseURL: baseURL, secret: secret, HTTPClient: &http.Client{Timeout: 15 * time.Second}}
}

func (c *Client) Enabled() bool { return c.BaseURL != "" }

type internalResp struct {
	OK   bool   `json:"ok"`
	Erro string `json:"erro"`
}

// ConfirmarEstorno credita de fato o saldo de um estorno de cancelamento de
// etiqueta antes registrado como pendente, DEPOIS de ops confirmar que o
// reembolso caiu na conta pool da Melhor Envio. Idempotente do lado do wallet.
func (c *Client) ConfirmarEstorno(ctx context.Context, userID int64, referencia string) error {
	if !c.Enabled() {
		return fmt.Errorf("[senderzz_admin] wallet.ConfirmarEstorno: WALLET_SERVICE_URL não configurada")
	}
	if c.secret == "" {
		return fmt.Errorf("[senderzz_admin] wallet.ConfirmarEstorno: WALLET_INTERNAL_SECRET ausente (fail-closed)")
	}
	body, err := json.Marshal(map[string]any{"user_id": userID, "referencia": referencia})
	if err != nil {
		return fmt.Errorf("[senderzz_admin] wallet.ConfirmarEstorno: erro ao serializar corpo: %w", err)
	}
	mac := hmac.New(sha256.New, []byte(c.secret))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/internal/confirmar-estorno", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("[senderzz_admin] wallet.ConfirmarEstorno: erro ao montar request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Sig", sig)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("[senderzz_admin] wallet.ConfirmarEstorno: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))

	var parsed internalResp
	_ = json.Unmarshal(respBody, &parsed)
	if !parsed.OK {
		msg := parsed.Erro
		if msg == "" {
			msg = fmt.Sprintf("status %d", resp.StatusCode)
		}
		return fmt.Errorf("[senderzz_admin] wallet.ConfirmarEstorno: %s", msg)
	}
	return nil
}
