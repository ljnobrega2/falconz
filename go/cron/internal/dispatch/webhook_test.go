// Smoke/integração do dispatcher de webhooks.
//
// PRINCÍPIO DE SEGURANÇA DO TESTE: tudo roda dentro de UMA transação com ROLLBACK
// garantido (t.Cleanup). Nenhuma linha real é alterada e nenhum POST é enviado a
// webhook real — o destino é um httptest.Server em loopback, liberado só pela env
// WEBHOOK_ALLOW_LOOPBACK=1 (via t.Setenv). A flag de produção
// webhook_dispatch_enabled é ligada SÓ na tx (revertida no rollback); ao final o
// teste reabre uma conexão limpa do pool e confirma que continua '0' no banco.
//
// Requer DATABASE_URL apontando para o Postgres de DEV. Sem ela → t.Skip.
package dispatch

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDispatch_SmokeLoopback(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL não definida — pulando smoke de integração")
	}

	// Libera loopback SÓ neste teste (lido em runtime por isBlockedIP via os.Getenv).
	t.Setenv("WEBHOOK_ALLOW_LOOPBACK", "1")

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("conectar ao banco: %v", err)
	}
	defer pool.Close()

	// ── Mock receptor (loopback) — captura body + assinatura recebidos ───────────
	var (
		mu          sync.Mutex
		gotBody     []byte
		gotSig      string
		gotEvent    string
		gotCalls    int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotCalls++
		gotBody = b
		gotSig = r.Header.Get("X-Senderzz-Signature")
		gotEvent = r.Header.Get("X-Senderzz-Event")
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	// ── Transação com ROLLBACK garantido — nada toca dados reais ─────────────────
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	// ROLLBACK via defer registrado AQUI (depois do `defer pool.Close()` lá em cima):
	// defers rodam LIFO, então o rollback devolve a conexão da tx ao pool ANTES de
	// pool.Close() — senão pool.Close() bloqueia esperando a conexão checked-out
	// (deadlock). É no-op se já fizemos rollback explícito no corpo do teste.
	defer func() { _ = tx.Rollback(ctx) }()

	const secret = "smoke_secret_abc123"
	const eventType = "order_status_enviado"

	// 1) Usuário sintético do portal (FK de senderzz_portal_webhooks.user_id).
	var produtorID int64
	if err := tx.QueryRow(ctx,
		`INSERT INTO senderzz_portal_users (email, nome, role, ativo)
		 VALUES ($1, 'Smoke Produtor', 'produtor', true)
		 RETURNING id`,
		"smoke-webhook-"+time.Now().Format("150405.000000")+"@example.test",
	).Scan(&produtorID); err != nil {
		t.Fatalf("insert portal user: %v", err)
	}

	// 2) Pedido sintético (status 'enviado' casa o evento; não dispara revenue trigger).
	var orderID int64
	orderNum := "SMK" + time.Now().Format("150405000")
	if err := tx.QueryRow(ctx,
		`INSERT INTO sz_orders
		   (order_number, user_id, produtor_id, status, subtotal, shipping, total,
		    payment_method, payment_status, customer_name, billing_email)
		 VALUES ($1, $2, $2, 'enviado', 197.00, 29.90, 226.90,
		         'PIX', 'paid', 'Cliente Smoke', 'cliente@smoke.test')
		 RETURNING id`,
		orderNum, produtorID,
	).Scan(&orderID); err != nil {
		t.Fatalf("insert order: %v", err)
	}

	// 2b) Endereço + item (para o payload ter dados reais).
	if _, err := tx.Exec(ctx,
		`INSERT INTO sz_order_addresses
		   (order_id, tipo, nome, email, telefone, cep, logradouro, numero, bairro, cidade, uf)
		 VALUES ($1, 'shipping', 'Cliente Smoke', 'cliente@smoke.test', '11999999999',
		         '01001000', 'Rua Exemplo', '100', 'Centro', 'São Paulo', 'SP')`,
		orderID); err != nil {
		t.Fatalf("insert address: %v", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO sz_order_items (order_id, produto_id, nome, quantidade, preco_unit, subtotal)
		 VALUES ($1, 1, 'Produto Smoke', 1, 197.00, 197.00)`,
		orderID); err != nil {
		t.Fatalf("insert item: %v", err)
	}

	// 3) Webhook ativo apontando para o mock (event_types vazio = assina todos).
	var webhookID int64
	if err := tx.QueryRow(ctx,
		`INSERT INTO senderzz_portal_webhooks (user_id, url, secret, active, event_types)
		 VALUES ($1, $2, $3, true, '[]'::jsonb)
		 RETURNING id`,
		produtorID, srv.URL, secret,
	).Scan(&webhookID); err != nil {
		t.Fatalf("insert webhook: %v", err)
	}

	// 4) Linha do outbox pendente (created_at default now → dentro da janela).
	var outboxID int64
	if err := tx.QueryRow(ctx,
		`INSERT INTO sz_webhook_outbox (order_id, event_type, status_de, status_para)
		 VALUES ($1, $2, 'embalado', 'enviado')
		 RETURNING id`,
		orderID, eventType,
	).Scan(&outboxID); err != nil {
		t.Fatalf("insert outbox: %v", err)
	}

	// 5) LIGA a flag SÓ na tx (revertida no rollback).
	if _, err := tx.Exec(ctx,
		`UPDATE senderzz_options SET value = '1' WHERE name = 'webhook_dispatch_enabled'`,
	); err != nil {
		t.Fatalf("ligar flag na tx: %v", err)
	}
	// Garante que a opção exista (caso o ambiente não a tenha semeado).
	if _, err := tx.Exec(ctx,
		`INSERT INTO senderzz_options (name, value) VALUES ('webhook_dispatch_enabled','1')
		 ON CONFLICT (name) DO UPDATE SET value = '1'`,
	); err != nil {
		t.Fatalf("upsert flag na tx: %v", err)
	}

	// ── EXECUÇÃO: dirige o core diretamente na tx ────────────────────────────────
	delivered, err := dispatchTx(ctx, tx)
	if err != nil {
		t.Fatalf("dispatchTx erro: %v", err)
	}
	if delivered != 1 {
		t.Fatalf("esperava 1 entrega 2xx, obtive %d", delivered)
	}

	// (a) O mock recebeu exatamente 1 POST.
	mu.Lock()
	calls, body, sig, ev := gotCalls, gotBody, gotSig, gotEvent
	mu.Unlock()
	if calls != 1 {
		t.Fatalf("mock recebeu %d chamadas, esperava 1", calls)
	}
	if ev != eventType {
		t.Errorf("header X-Senderzz-Event = %q, esperava %q", ev, eventType)
	}

	// (b) Assinatura HMAC confere com o body recebido.
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	wantSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(wantSig)) {
		t.Errorf("assinatura HMAC não confere:\n  recebida=%s\n  esperada=%s", sig, wantSig)
	}

	// (b2) PARIDADE DE CONTRATO: o body deve conter TODAS as chaves de topo de
	// samplePayload() (expedicao_webhooks.go). Sem isso, uma integração testada via
	// botão "Testar" leria `undefined` em produção. (O mock só HMACa o que recebe,
	// então esta asserção explícita é o que de fato pega drift de contrato.)
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("body do POST não é JSON válido: %v", err)
	}
	wantTopKeys := []string{
		"event", "status_ativo", "pedido", "classe_entrega", "frete",
		"cliente", "entrega", "rastreamento", "link_rastreamento",
		"itens", "transportadora", "servico",
	}
	for _, k := range wantTopKeys {
		if _, ok := got[k]; !ok {
			t.Errorf("payload sem chave de topo %q (contrato samplePayload)", k)
		}
	}
	if got["event"] != eventType {
		t.Errorf("payload.event = %v, esperava %q", got["event"], eventType)
	}
	// Subchaves de pedido espelhadas do sample.
	if ped, ok := got["pedido"].(map[string]any); ok {
		for _, k := range []string{"id", "numero", "status", "subtotal", "total",
			"total_formatado", "metodo_pagamento", "criado_em"} {
			if _, ok := ped[k]; !ok {
				t.Errorf("payload.pedido sem chave %q", k)
			}
		}
	} else {
		t.Errorf("payload.pedido não é objeto")
	}
	// Dados reais chegaram (não só estrutura).
	if ent, ok := got["entrega"].(map[string]any); ok {
		if ent["cep"] != "01001000" {
			t.Errorf("payload.entrega.cep = %v, esperava dado real '01001000'", ent["cep"])
		}
	}

	// (c) senderzz_webhook_log ganhou 1 linha para este webhook (visível na tx).
	var logCount int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM senderzz_webhook_log WHERE webhook_id = $1`, webhookID,
	).Scan(&logCount); err != nil {
		t.Fatalf("contar webhook_log: %v", err)
	}
	if logCount != 1 {
		t.Errorf("senderzz_webhook_log tem %d linhas, esperava 1", logCount)
	}
	var logCode *int
	if err := tx.QueryRow(ctx,
		`SELECT response_code FROM senderzz_webhook_log WHERE webhook_id = $1`, webhookID,
	).Scan(&logCode); err != nil {
		t.Fatalf("ler response_code do log: %v", err)
	}
	if logCode == nil || *logCode != 200 {
		t.Errorf("response_code no log = %v, esperava 200", logCode)
	}

	// (d) outbox.sent_at preenchido.
	var sentAt *time.Time
	if err := tx.QueryRow(ctx,
		`SELECT sent_at FROM sz_webhook_outbox WHERE id = $1`, outboxID,
	).Scan(&sentAt); err != nil {
		t.Fatalf("ler sent_at do outbox: %v", err)
	}
	if sentAt == nil {
		t.Errorf("outbox.sent_at continua NULL, esperava preenchido")
	}

	// ── ROLLBACK explícito + confirmação da flag de produção ─────────────────────
	if err := tx.Rollback(ctx); err != nil && err != pgx.ErrTxClosed {
		t.Fatalf("rollback: %v", err)
	}

	// Conexão LIMPA do pool: a flag de produção deve continuar '0'.
	var prodFlag string
	if err := pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE name = 'webhook_dispatch_enabled'`,
	).Scan(&prodFlag); err != nil {
		t.Fatalf("ler flag de produção pós-rollback: %v", err)
	}
	if prodFlag != "0" {
		t.Fatalf("FLAG DE PRODUÇÃO VAZOU: webhook_dispatch_enabled = %q (esperava '0')", prodFlag)
	}
}

// TestDispatch_GateOff prova o fail-closed: com a flag desligada na tx, dispatchTx
// não entrega nada (0,nil) mesmo havendo linha pendente + webhook casado.
func TestDispatch_GateOff(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL não definida — pulando")
	}
	t.Setenv("WEBHOOK_ALLOW_LOOPBACK", "1")

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("conectar: %v", err)
	}
	defer pool.Close()

	var hit int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit++
		w.WriteHeader(200)
	}))
	defer srv.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	// Rollback via defer (LIFO) — devolve a conexão ao pool ANTES de pool.Close().
	defer func() { _ = tx.Rollback(ctx) }()

	// Garante flag DESLIGADA na tx.
	if _, err := tx.Exec(ctx,
		`INSERT INTO senderzz_options (name, value) VALUES ('webhook_dispatch_enabled','0')
		 ON CONFLICT (name) DO UPDATE SET value = '0'`); err != nil {
		t.Fatalf("desligar flag: %v", err)
	}

	var produtorID int64
	if err := tx.QueryRow(ctx,
		`INSERT INTO senderzz_portal_users (email, nome, role, ativo)
		 VALUES ($1, 'Gate Off', 'produtor', true) RETURNING id`,
		"smoke-gateoff-"+time.Now().Format("150405.000000")+"@example.test",
	).Scan(&produtorID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var orderID int64
	if err := tx.QueryRow(ctx,
		`INSERT INTO sz_orders (order_number, user_id, produtor_id, status, total)
		 VALUES ($1, $2, $2, 'enviado', 10.00) RETURNING id`,
		"GOFF"+time.Now().Format("150405000"), produtorID,
	).Scan(&orderID); err != nil {
		t.Fatalf("insert order: %v", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO senderzz_portal_webhooks (user_id, url, secret, active, event_types)
		 VALUES ($1, $2, 'x', true, '[]'::jsonb)`, produtorID, srv.URL); err != nil {
		t.Fatalf("insert webhook: %v", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO sz_webhook_outbox (order_id, event_type, status_para)
		 VALUES ($1, 'order_status_enviado', 'enviado')`, orderID); err != nil {
		t.Fatalf("insert outbox: %v", err)
	}

	n, err := dispatchTx(ctx, tx)
	if err != nil {
		t.Fatalf("dispatchTx erro: %v", err)
	}
	if n != 0 {
		t.Errorf("gate off deveria entregar 0, entregou %d", n)
	}
	if hit != 0 {
		t.Errorf("gate off NÃO deveria fazer POST, mas o mock recebeu %d", hit)
	}
}
