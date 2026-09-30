// Package wallet fornece o cliente HTTP serviço-a-serviço para o wallet-service.
//
// CRIT-01 — Reserva de saldo na emissão de etiquetas:
//
//	O padrão financeiro fiel ao PHP (includes/tpc/wallet.php) é:
//	  tpc_reservar()        ANTES de ME.CreateShipment
//	  tpc_debitar_reserva() no SUCESSO da criação da etiqueta
//	  tpc_liberar_reserva() em QUALQUER falha (rollback)
//	Este cliente expõe os três passos como Reservar / DebitarReserva / LiberarReserva.
//	O valor reservado/debitado é SEMPRE o preço recalculado server-side
//	(ME.Calculate → selectedOption.Price), nunca o enviado pelo cliente.
//
// SEC-GO-01 (ver go/wallet/cmd/server/main.go:186-193) — modelo de autenticação:
//
//	reservar/debitar-reserva/creditar/liberar-reserva NÃO podem viver sob JWT de
//	usuário (debitariam a carteira do PRÓPRIO caller → mint de dinheiro). São
//	operações SERVIÇO-A-SERVIÇO, expostas SOMENTE via /internal/* do wallet-service,
//	autenticadas por HMAC-SHA256 (header X-Internal-Sig). O user_id (dono da
//	carteira) viaja DENTRO do corpo assinado por HMAC — o wallet confia no user_id
//	do body justamente porque o body é HMAC-confiável. O labels-service preenche
//	esse user_id a partir do JWT do caller (middleware.GetUserID), nunca de campo
//	livre da requisição.
//
// Os endpoints /internal/reservar, /internal/debitar-reserva e
// /internal/liberar-reserva JÁ EXISTEM no wallet-service (go/wallet/internal/handlers/
// internal.go), com SELECT ... FOR UPDATE + idempotência por (user_id, referencia, tipo).
// Ativo em produção via WALLET_SERVICE_URL + WALLET_INTERNAL_SECRET (labels-service).
//
// Variáveis de ambiente:
//   - WALLET_SERVICE_URL     — base URL do wallet-service (ex: http://wallet:8081).
//     Se vazia, o cliente fica DESATIVADO (Enabled()=false):
//     a emissão de etiqueta segue sem débito (comportamento
//     atual da Fase 4 — off-switch enquanto o endpoint não
//     está portado). Padrão strangler-fig do projeto.
//   - WALLET_INTERNAL_SECRET — mesmo secret HMAC das rotas /internal/* do wallet
//     (go/wallet/internal/handlers/internal.go). Se a URL
//     estiver configurada mas o secret vazio → fail-closed:
//     Reservar retorna erro e a etiqueta NÃO é criada.
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

	"github.com/shopspring/decimal"
)

// Client é o cliente serviço-a-serviço para as rotas /internal/* do wallet-service.
// Instanciar via NewClient().
type Client struct {
	BaseURL    string // WALLET_SERVICE_URL (sem trailing slash). Vazio → desativado.
	secret     string // WALLET_INTERNAL_SECRET (HMAC-SHA256).
	HTTPClient *http.Client
}

// NewClient cria o cliente a partir das variáveis de ambiente.
//
// Off-switch (strangler-fig): WALLET_SERVICE_URL vazia → Enabled()=false e o
// fluxo de etiqueta NÃO chama a carteira (mantém o comportamento da Fase 4).
// Quando a URL estiver configurada mas o secret vazio, as chamadas falham
// fail-closed (ver Reservar) — nunca emite etiqueta que não pode cobrar.
func NewClient() *Client {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("WALLET_SERVICE_URL")), "/")
	secret := os.Getenv("WALLET_INTERNAL_SECRET")

	if baseURL == "" {
		// CRIT-01: off-switch — sem WALLET_SERVICE_URL, reserva de saldo fica desativada
		// (endpoints já existem no wallet-service; isto é só a ausência da env var).
		slog.Warn("[senderzz_labels] WALLET_SERVICE_URL ausente — reserva de saldo (CRIT-01) DESATIVADA; etiquetas serão criadas sem débito automático")
	} else if secret == "" {
		// URL configurada porém sem secret: as chamadas vão falhar fail-closed.
		slog.Error("[senderzz_labels] WALLET_SERVICE_URL configurada mas WALLET_INTERNAL_SECRET vazio — reserva de saldo falhará (fail-closed)")
	}

	return &Client{
		BaseURL: baseURL,
		secret:  secret,
		HTTPClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// Enabled indica se a reserva de saldo está ativa (WALLET_SERVICE_URL configurada).
// O handler usa isto como off-switch: desativado → cria etiqueta sem débito.
func (c *Client) Enabled() bool {
	return c.BaseURL != ""
}

// ─── Tipos de request/response ──────────────────────────────────────────────

// reservarReq espelha o meta de tpc_reservar(user_id, valor, descricao, [meta]).
type reservarReq struct {
	UserID     int64  `json:"user_id"`
	Valor      string `json:"valor"` // CRIT-01: preço recalculado, StringFixed(2).
	Descricao  string `json:"descricao"`
	Referencia string `json:"referencia"` // idempotência: tpc_reservar reusa tx pendente.
	OrderID    int64  `json:"order_id"`
}

// debitarReq espelha tpc_debitar_reserva(tx_id, [meta]).
type debitarReq struct {
	TxID      int64  `json:"tx_id"`
	MEOrderID string `json:"me_order_id"` // shipment_id do ME (só existe após CreateShipment).
}

// liberarReq espelha tpc_liberar_reserva(tx_id, motivo).
type liberarReq struct {
	TxID   int64  `json:"tx_id"`
	Motivo string `json:"motivo"`
}

// genericResp é o envelope de resposta padrão dos serviços Go ({"ok":..,"erro":..}).
// tx_id presente apenas na resposta de Reservar.
type genericResp struct {
	OK   bool   `json:"ok"`
	Erro string `json:"erro"`
	TxID int64  `json:"tx_id"`
}

// ─── Métodos do cliente ──────────────────────────────────────────────────────

// Reservar reserva `valor` na carteira do `userID` ANTES de ME.CreateShipment.
// Retorna o tx_id da reserva (transação pendente) para débito/liberação posterior.
//
// CRIT-01: `valor` DEVE ser o preço recalculado server-side (ME.Calculate),
// nunca o enviado pelo cliente. `userID` DEVE vir do JWT do caller (SEC-GO-01).
//
// Fail-closed: secret vazio → erro (não reserva às cegas). O handler trata o erro
// abortando a criação da etiqueta — nunca emite etiqueta que não pode cobrar.
//
// `referencia` dá idempotência: tpc_reservar reutiliza a tx pendente existente
// quando a referencia coincide (retry-safe).
func (c *Client) Reservar(ctx context.Context, userID int64, valor decimal.Decimal, descricao, referencia string, orderID int64) (int64, error) {
	if userID <= 0 {
		return 0, fmt.Errorf("[senderzz_labels] wallet.Reservar: user_id inválido (%d) — dono da carteira indefinido (SEC-GO-01)", userID)
	}
	body := reservarReq{
		UserID:     userID,
		Valor:      valor.StringFixed(2),
		Descricao:  descricao,
		Referencia: referencia,
		OrderID:    orderID,
	}

	resp, err := c.doRequest(ctx, "/internal/reservar", body)
	if err != nil {
		return 0, fmt.Errorf("[senderzz_labels] wallet.Reservar: %w", err)
	}
	if !resp.OK || resp.TxID <= 0 {
		// Saldo insuficiente ou recusa do wallet — mensagem do erro do wallet.
		msg := resp.Erro
		if msg == "" {
			msg = "reserva recusada pelo wallet-service (saldo insuficiente?)"
		}
		return 0, fmt.Errorf("[senderzz_labels] wallet.Reservar: %s", msg)
	}
	return resp.TxID, nil
}

// DebitarReserva confirma o débito da reserva no SUCESSO da criação da etiqueta.
// Espelha tpc_debitar_reserva: move o valor de saldo_reservado para o débito
// efetivo (transação pendente → confirmada).
//
// meOrderID é o shipment_id do ME (preenchido após CreateShipment) — equivalente
// ao meta['me_order_id'] mesclado por tpc_debitar_reserva.
func (c *Client) DebitarReserva(ctx context.Context, txID int64, meOrderID string) error {
	if txID <= 0 {
		return fmt.Errorf("[senderzz_labels] wallet.DebitarReserva: tx_id inválido (%d)", txID)
	}
	body := debitarReq{TxID: txID, MEOrderID: meOrderID}

	resp, err := c.doRequest(ctx, "/internal/debitar-reserva", body)
	if err != nil {
		return fmt.Errorf("[senderzz_labels] wallet.DebitarReserva: %w", err)
	}
	if !resp.OK {
		msg := resp.Erro
		if msg == "" {
			msg = "débito de reserva recusado pelo wallet-service"
		}
		return fmt.Errorf("[senderzz_labels] wallet.DebitarReserva: %s", msg)
	}
	return nil
}

// Estornar credita de volta um valor JÁ DEBITADO (não uma reserva pendente —
// para isso é LiberarReserva). Usado no cancelamento de etiqueta JÁ EMITIDA:
// o débito virou confirmado no momento da emissão, então devolver o dinheiro é
// um NOVO crédito, referência própria (idempotente do lado do wallet-service).
func (c *Client) Estornar(ctx context.Context, userID int64, valor decimal.Decimal, descricao, referencia string, orderID int64) error {
	if userID <= 0 {
		return fmt.Errorf("[senderzz_labels] wallet.Estornar: user_id inválido (%d)", userID)
	}
	body := reservarReq{
		UserID:     userID,
		Valor:      valor.StringFixed(2),
		Descricao:  descricao,
		Referencia: referencia,
		OrderID:    orderID,
	}
	resp, err := c.doRequest(ctx, "/internal/estornar", body)
	if err != nil {
		return fmt.Errorf("[senderzz_labels] wallet.Estornar: %w", err)
	}
	if !resp.OK {
		msg := resp.Erro
		if msg == "" {
			msg = "estorno recusado pelo wallet-service"
		}
		return fmt.Errorf("[senderzz_labels] wallet.Estornar: %s", msg)
	}
	return nil
}

// EstornarPendente registra a INTENÇÃO de estorno (cancelamento de etiqueta)
// SEM creditar saldo. AUDIT-2026-07-28 (dono): "carteira ME e FALK precisam
// estar sincronizadas, nunca creditar sem ter caído na conta melhor envio" —
// o saldo só é creditado depois, via ConfirmarEstorno, quando o reembolso já
// tiver caído na conta pool da ME (hoje: confirmação manual de ops).
func (c *Client) EstornarPendente(ctx context.Context, userID int64, valor decimal.Decimal, descricao, referencia string, orderID int64) error {
	if userID <= 0 {
		return fmt.Errorf("[senderzz_labels] wallet.EstornarPendente: user_id inválido (%d)", userID)
	}
	body := reservarReq{
		UserID:     userID,
		Valor:      valor.StringFixed(2),
		Descricao:  descricao,
		Referencia: referencia,
		OrderID:    orderID,
	}
	resp, err := c.doRequest(ctx, "/internal/estornar-pendente", body)
	if err != nil {
		return fmt.Errorf("[senderzz_labels] wallet.EstornarPendente: %w", err)
	}
	if !resp.OK {
		msg := resp.Erro
		if msg == "" {
			msg = "estorno pendente recusado pelo wallet-service"
		}
		return fmt.Errorf("[senderzz_labels] wallet.EstornarPendente: %s", msg)
	}
	return nil
}

// LiberarReserva libera (estorna) uma reserva pendente em caso de falha.
// Espelha tpc_liberar_reserva: devolve o valor a saldo_reservado e marca a
// transação como cancelada. Idempotente do lado do wallet (tx já não-pendente
// não é liberada de novo).
//
// Usado no defer de rollback do handler — qualquer falha após a reserva e antes
// do débito confirmado dispara a liberação.
func (c *Client) LiberarReserva(ctx context.Context, txID int64, motivo string) error {
	if txID <= 0 {
		return fmt.Errorf("[senderzz_labels] wallet.LiberarReserva: tx_id inválido (%d)", txID)
	}
	body := liberarReq{TxID: txID, Motivo: motivo}

	resp, err := c.doRequest(ctx, "/internal/liberar-reserva", body)
	if err != nil {
		return fmt.Errorf("[senderzz_labels] wallet.LiberarReserva: %w", err)
	}
	if !resp.OK {
		msg := resp.Erro
		if msg == "" {
			msg = "liberação de reserva recusada pelo wallet-service"
		}
		return fmt.Errorf("[senderzz_labels] wallet.LiberarReserva: %s", msg)
	}
	return nil
}

// ─── helper interno ──────────────────────────────────────────────────────────

// doRequest serializa o body, assina-o com HMAC-SHA256 (WALLET_INTERNAL_SECRET)
// e faz POST para BaseURL+path. A assinatura vai no header X-Internal-Sig,
// formato hex puro — exatamente o que verifyHMAC do wallet-service valida
// (go/wallet/internal/handlers/internal.go: hex(HMAC-SHA256(secret, body))).
//
// Fail-closed: secret vazio → erro antes de qualquer envio (não há autenticação).
func (c *Client) doRequest(ctx context.Context, path string, payload any) (genericResp, error) {
	var out genericResp

	if c.secret == "" {
		// Sem secret não há como autenticar a chamada interna — fail-closed.
		return out, fmt.Errorf("WALLET_INTERNAL_SECRET vazio — chamada interna bloqueada (fail-closed)")
	}

	// Marshal UMA vez e assina EXATAMENTE esses bytes (o wallet valida o body cru).
	body, err := json.Marshal(payload)
	if err != nil {
		return out, fmt.Errorf("marshal payload: %w", err)
	}

	mac := hmac.New(sha256.New, []byte(c.secret))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return out, fmt.Errorf("criar requisição POST %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Internal-Sig", sig)
	req.Header.Set("User-Agent", "senderzz-labels-service/4.0")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return out, fmt.Errorf("requisição POST %s: %w", path, err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MB máximo

	if resp.StatusCode != http.StatusOK {
		// Inclui o corpo (truncado) no erro para diagnóstico no slog do handler.
		return out, fmt.Errorf("wallet-service retornou status %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}

	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("decode response %s: %w", path, err)
	}
	return out, nil
}

// truncate retorna os primeiros n caracteres de s (mensagens de erro seguras).
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
