// Package dispatch — DISPATCHER do outbox de webhooks de pedido (sz_webhook_dispatch).
//
// O QUE FAZ: consome a tabela sz_webhook_outbox (enfileirada atomicamente pelo
// trigger trg_sz_webhook_outbox em sz_orders, na mudança de status) e ENTREGA
// cada evento via POST HTTP aos webhooks configurados do produtor em
// senderzz_portal_webhooks. Registra o resultado em senderzz_webhook_log e marca
// a linha do outbox como enviada (sent_at) ou agenda nova tentativa (backoff).
//
// SEGURANÇA (fail-closed):
//   - GATE de produção: só dispara se senderzz_options.webhook_dispatch_enabled ∈
//     {"1","true","yes"}. Default no banco = '0' (DESLIGADO). Com a flag desligada
//     NUNCA fazemos um único POST — retorna (0,nil) e loga.
//   - SSRF GUARD: resolve o host de cada URL e BLOQUEIA loopback/privado/link-local/
//     metadata/CGNAT antes de qualquer request (espelha
//     go/admin/internal/handlers/expedicao_webhooks.go::isBlockedIP/validatePublicURL —
//     copiado de propósito, não importamos cross-módulo). Exceção de teste:
//     WEBHOOK_ALLOW_LOOPBACK=1 libera loopback (só p/ smoke local).
//   - Assinatura: X-Senderzz-Signature: sha256=<hmac_sha256_hex(body, secret)>.
//
// IDEMPOTÊNCIA / CONCORRÊNCIA: a leitura do lote usa FOR UPDATE SKIP LOCKED dentro
// de UMA transação, então múltiplos runners (ou ticks sobrepostos) nunca entregam
// a mesma linha duas vezes. O HTTP roda dentro da tx — aceitável para este cron de
// instância única, LIMIT 50, 1×/min. Receptores devem deduplicar (entrega
// at-least-once em caso de retry).
//
// JANELA DE FRESCOR: só pega linhas criadas nas últimas
// webhook_dispatch_freshness_seconds (default 7200 = 2h). Evita inundar endpoints
// com backlog antigo quando a flag for ligada pela primeira vez.
package dispatch

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"syscall" // AUDIT-2026-06-21 H3 — Control hook do dialer (anti-rebinding)
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Constantes do dispatcher.
const (
	batchLimit            = 50               // máximo de linhas do outbox por execução
	defaultFreshnessSecs  = 7200             // 2h — janela de frescor default
	httpTimeout           = 10 * time.Second // timeout por POST (task: 10s)
	respBodyTruncateChars = 2000             // truncamento do response_body no log
	respBodyReadLimit     = 8 * 1024         // leitura máxima do body do receptor (anti-abuso)
)

// outboxRow — linha pendente do outbox carregada para processamento.
type outboxRow struct {
	id               int64
	orderID          int64
	eventType        string
	statusDe         string
	statusPara       string
	deliveredHookIDs []int64 // AUDIT-2026-07-30: webhooks que já confirmaram 2xx nesta linha
}

// matchedWebhook — webhook ativo que assina o evento da linha do outbox.
type matchedWebhook struct {
	id     int64
	url    string
	secret string
}

// Dispatch é o ponto de entrada chamado pelo runner do cron (assinatura
// run func(ctx, *pgxpool.Pool) (int, error)). Abre UMA transação, delega a
// dispatchTx e faz commit/rollback. Retorna o número de entregas 2xx.
//
// O gate de habilitação e TODA a lógica vivem em dispatchTx, lendo a flag NA
// MESMA transação — isso é o que permite o teste dirigir dispatchTx num tx que
// liga a flag e depois faz ROLLBACK, sem nunca tocar a flag de produção.
func Dispatch(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("[webhook] begin tx: %w", err)
	}
	// Rollback é no-op após Commit; serve de rede de segurança em qualquer return cedo.
	defer func() { _ = tx.Rollback(ctx) }()

	n, err := dispatchTx(ctx, tx)
	if err != nil {
		return n, err // tx será revertida pelo defer
	}
	if err := tx.Commit(ctx); err != nil {
		return n, fmt.Errorf("[webhook] commit tx: %w", err)
	}
	return n, nil
}

// dispatchTx contém a lógica completa, operando sobre uma conexão/transação `tx`.
// Lê o gate NA tx. Não faz commit/rollback — quem chama controla a transação.
func dispatchTx(ctx context.Context, tx pgx.Tx) (int, error) {
	// ── 1. GATE de produção (fail-closed) ────────────────────────────────────────
	if !dispatchEnabled(ctx, tx) {
		slog.Info("[webhook] dispatch desabilitado (webhook_dispatch_enabled=0)")
		return 0, nil
	}

	// ── 2. Janela de frescor ─────────────────────────────────────────────────────
	freshness := freshnessSeconds(ctx, tx)

	// ── 3. Carrega o lote pendente (FOR UPDATE SKIP LOCKED) ──────────────────────
	// IMPORTANTE (pgx): escaneamos TODAS as linhas e fechamos o Rows ANTES de
	// emitir qualquer outra query/Exec na mesma tx — uma conexão pgx não suporta
	// duas operações simultâneas ("conn busy").
	rows, err := tx.Query(ctx,
		`SELECT id, order_id, event_type,
		        COALESCE(status_de, ''), COALESCE(status_para, ''),
		        COALESCE(delivered_hook_ids, '{}')
		   FROM sz_webhook_outbox
		  WHERE sent_at IS NULL
		    AND attempts < max_attempts
		    AND next_attempt_at <= now()
		    AND created_at > now() - make_interval(secs => $1)
		  ORDER BY id
		  FOR UPDATE SKIP LOCKED
		  LIMIT `+strconv.Itoa(batchLimit), freshness)
	if err != nil {
		return 0, fmt.Errorf("[webhook] select outbox: %w", err)
	}
	pending := make([]outboxRow, 0, batchLimit)
	for rows.Next() {
		var r outboxRow
		if err := rows.Scan(&r.id, &r.orderID, &r.eventType, &r.statusDe, &r.statusPara, &r.deliveredHookIDs); err != nil {
			rows.Close()
			return 0, fmt.Errorf("[webhook] scan outbox: %w", err)
		}
		pending = append(pending, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("[webhook] rows outbox: %w", err)
	}

	if len(pending) == 0 {
		slog.Info("[webhook] nada pendente no outbox")
		return 0, nil
	}

	delivered := 0
	for _, row := range pending {
		n, err := processRow(ctx, tx, row)
		if err != nil {
			// Erro de DB ao processar uma linha não derruba o lote inteiro:
			// loga e segue (a linha fica para o próximo tick). Devolvemos o erro
			// só se quisermos abortar — aqui preferimos continuar.
			slog.Error("[webhook] erro ao processar linha do outbox", "outbox_id", row.id, "err", err)
			continue
		}
		delivered += n
	}
	return delivered, nil
}

// processRow entrega UMA linha do outbox a todos os webhooks que a assinam.
//
// Semântica (uma linha do outbox tem UM sent_at/attempts, mas pode casar vários
// webhooks):
//   - 0 webhooks casados → marca sent_at=now() (nada a entregar; não re-tenta).
//   - todos os POSTs 2xx → marca sent_at=now().
//   - qualquer falha (não-2xx, erro de rede, SSRF bloqueado) → bump attempts +
//     backoff quadrático + last_error; NÃO marca sent_at (re-tenta no próximo tick
//     até max_attempts). Entrega é at-least-once — receptor deve deduplicar.
//
// Retorna o número de webhooks que responderam 2xx nesta linha.
func processRow(ctx context.Context, tx pgx.Tx, row outboxRow) (int, error) {
	// 1) Descobre produtor/usuário do pedido + dados para o payload.
	od, err := loadOrder(ctx, tx, row.orderID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Pedido sumiu (FK não existe em sz_orders): nada a entregar, encerra a linha.
			if err := markSent(ctx, tx, row.id); err != nil {
				return 0, err
			}
			return 0, nil
		}
		return 0, err
	}

	// 2) Webhooks ativos que assinam este event_type (do produtor OU do user).
	//    Roteamento por produto: webhook com product_id só casa se o pedido contiver
	//    aquele produto; product_id NULL = todos os produtos do usuário.
	hooks, err := matchWebhooks(ctx, tx, od.produtorID, od.userID, row.eventType, row.orderID)
	if err != nil {
		return 0, err
	}

	// 3) Carrega endereço de entrega + itens + meta (CPF/tracking manual) + o
	// financeiro (líquido produtor) — já fora de qualquer Rows aberto.
	addr, _ := loadShippingAddress(ctx, tx, row.orderID)
	items, _ := loadItems(ctx, tx, row.orderID)
	meta, _ := loadOrderMeta(ctx, tx, row.orderID)
	fin, _ := loadFinancials(ctx, tx, row.orderID)

	// 3b) Bloco específico do tipo de fluxo (mutuamente exclusivo no payload).
	// CRITÉRIO: existência de linha em sz_motoboy_pedidos — NÃO payment_method
	// (COD é forma de pagamento, existe tanto em motoboy quanto em expedição
	// PAC/ME; o que decide o tipo de ENTREGA é ter ou não uma rota de motoboy).
	//   - motoboy: agendamento (data_entrega) — NUNCA tem rastreio (entrega é
	//     feita pelo motoboy próprio, não por transportadora rastreável).
	//   - exp (PAC/ME): rastreio real do Melhor Envio — NUNCA tem data de
	//     entrega prévia (só se sabe quando o Correios/transportadora entrega).
	var mot motoboyData
	var lab labelData
	isMotoboy := hasMotoboyRoute(ctx, tx, od.wcOrderKey)
	if isMotoboy {
		mot, _ = loadMotoboy(ctx, tx, od.wcOrderKey)
	} else {
		lab = loadLabel(ctx, tx, od.wcOrderKey, meta)
	}

	// Nenhum webhook casado → encerra a linha (não fica re-tentando à toa).
	if len(hooks) == 0 {
		if err := markSent(ctx, tx, row.id); err != nil {
			return 0, err
		}
		return 0, nil
	}

	// 4) Monta o body UMA vez (mesmo contrato p/ todos os webhooks da linha).
	body, err := json.Marshal(buildPayload(row, od, addr, items, meta, fin, mot, lab, isMotoboy))
	if err != nil {
		return 0, fmt.Errorf("marshal payload: %w", err)
	}

	// AUDIT-2026-07-30 MEDIUM: pula hooks que já confirmaram 2xx numa tentativa
	// anterior desta MESMA linha — sem isso, um retry (por causa de OUTRO hook
	// que falhou) reenviava o evento pra quem já tinha recebido com sucesso.
	already := make(map[int64]bool, len(row.deliveredHookIDs))
	for _, id := range row.deliveredHookIDs {
		already[id] = true
	}

	allOK := true
	deliveredOK := 0
	newlyDelivered := append([]int64(nil), row.deliveredHookIDs...)
	var lastErr string

	for _, h := range hooks {
		if already[h.id] {
			deliveredOK++
			continue
		}

		code, respBody, fireErr := fireWebhook(ctx, h, row.eventType, body)

		// Loga TODA tentativa (sucesso e falha) em senderzz_webhook_log.
		logDelivery(ctx, tx, h.id, row.eventType, body, code, respBody, fireErr)

		ok := fireErr == nil && code >= 200 && code < 300
		if ok {
			deliveredOK++
			newlyDelivered = append(newlyDelivered, h.id)
		} else {
			allOK = false
			if fireErr != nil {
				lastErr = fireErr.Error()
			} else {
				lastErr = fmt.Sprintf("HTTP %d", code)
			}
		}
	}

	// 5) Resolve a linha do outbox.
	if allOK {
		if err := markSent(ctx, tx, row.id); err != nil {
			return deliveredOK, err
		}
	} else {
		if err := bumpAttemptWithDelivered(ctx, tx, row.id, lastErr, newlyDelivered); err != nil {
			return deliveredOK, err
		}
	}
	return deliveredOK, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Gate / configuração
// ─────────────────────────────────────────────────────────────────────────────

// dispatchEnabled lê webhook_dispatch_enabled de senderzz_options NA tx.
// Habilitado só para {"1","true","yes"} (case-insensitive). Ausente/erro = DESLIGADO
// (fail-closed): jamais POSTa por engano.
func dispatchEnabled(ctx context.Context, tx pgx.Tx) bool {
	var v string
	err := tx.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE name = $1`, "webhook_dispatch_enabled").Scan(&v)
	if err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

// freshnessSeconds lê webhook_dispatch_freshness_seconds; default 7200 se ausente/inválido.
func freshnessSeconds(ctx context.Context, tx pgx.Tx) int {
	var v string
	err := tx.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE name = $1`, "webhook_dispatch_freshness_seconds").Scan(&v)
	if err != nil {
		return defaultFreshnessSecs
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		return defaultFreshnessSecs
	}
	return n
}

// ─────────────────────────────────────────────────────────────────────────────
// Leituras de pedido
// ─────────────────────────────────────────────────────────────────────────────

// orderData — dados do pedido necessários para o payload.
type orderData struct {
	id              int64
	wcOrderKey      int64 // COALESCE(wp_order_id, id) — chave p/ sz_motoboy_pedidos/wc_me_labels
	produtorID      int64
	userID          int64
	orderNumber     string
	status          string
	subtotal        float64
	shipping        float64
	total           float64
	gross           float64 // valor bruto do pedido (antes de descontos de plataforma)
	paymentMethod   string
	paymentStatus   string
	customerName    string
	billingEmail    string
	createdAt       time.Time
	updatedAt       time.Time
	shippingClass   string
	shippingClassID int64 // 0 se NULL
}

func loadOrder(ctx context.Context, tx pgx.Tx, orderID int64) (orderData, error) {
	var o orderData
	err := tx.QueryRow(ctx,
		`SELECT id, COALESCE(wp_order_id, id), produtor_id, user_id,
		        COALESCE(order_number,''), COALESCE(status,''),
		        COALESCE(subtotal,0), COALESCE(shipping,0), COALESCE(total,0),
		        COALESCE(gross, total, 0),
		        COALESCE(payment_method,''), COALESCE(payment_status,''),
		        COALESCE(customer_name,''), COALESCE(billing_email,''),
		        created_at, updated_at,
		        COALESCE(shipping_class,''), COALESCE(shipping_class_id, 0)
		   FROM sz_orders WHERE id = $1`, orderID).
		Scan(&o.id, &o.wcOrderKey, &o.produtorID, &o.userID,
			&o.orderNumber, &o.status,
			&o.subtotal, &o.shipping, &o.total, &o.gross,
			&o.paymentMethod, &o.paymentStatus,
			&o.customerName, &o.billingEmail,
			&o.createdAt, &o.updatedAt,
			&o.shippingClass, &o.shippingClassID)
	return o, err
}

// financialsData — recorte de sz_order_financials usado no payload (valores
// líquido/bruto do produtor). AUDIT-FINANCEIRO-2026-06-25: producer_net é coluna
// inerte (sem writer); a fonte viva é producer_net_live (generated column da
// migration 482) — por isso COALESCE(producer_net_live, liquido_produtor, 0).
// taxa_plataforma_produtor NÃO entra aqui de propósito — receita interna da
// plataforma, nunca deve ser exposta ao produtor/cliente via webhook.
type financialsData struct {
	liquidoProdutor float64
	taxaEntrega     float64
	deliveryFee     float64
}

func loadFinancials(ctx context.Context, tx pgx.Tx, orderID int64) (financialsData, error) {
	var f financialsData
	err := tx.QueryRow(ctx,
		`SELECT COALESCE(producer_net_live, liquido_produtor, 0),
		        COALESCE(taxa_entrega, 0),
		        COALESCE(delivery_fee, 0)
		   FROM sz_order_financials WHERE order_id = $1`, orderID).
		Scan(&f.liquidoProdutor, &f.taxaEntrega, &f.deliveryFee)
	return f, err
}

// motoboyData — agendamento do fluxo COD (sz_motoboy_pedidos). Chaveada por
// wcOrderKey (COALESCE(wp_order_id, sz_orders.id) — mesmo fallback usado em
// go/admin/internal/handlers/order_detail.go::loadMotoboySection, necessário pq
// pedidos nativos Go (checkout COD) têm wp_order_id NULL).
type motoboyData struct {
	statusMotoboy  string
	dataEntrega    time.Time
	dataEntregaSet bool
	reagendadoPara time.Time
	reagendadoSet  bool
}

// hasMotoboyRoute — true quando o pedido tem uma rota de ENTREGA por motoboy
// próprio (linha em sz_motoboy_pedidos). Isto — e NÃO payment_method — é o que
// distingue "cod" de "exp": COD é forma de pagamento e existe nos dois fluxos
// (motoboy paga na entrega, mas expedição PAC/ME também aceita COD); o tipo de
// entrega é decidido só por ter ou não rota de motoboy atribuída.
func hasMotoboyRoute(ctx context.Context, tx pgx.Tx, wcOrderKey int64) bool {
	var exists bool
	err := tx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM sz_motoboy_pedidos WHERE wc_order_id = $1)`,
		wcOrderKey).Scan(&exists)
	return err == nil && exists
}

func loadMotoboy(ctx context.Context, tx pgx.Tx, wcOrderKey int64) (motoboyData, error) {
	var m motoboyData
	var dataEntrega, reagendadoPara *time.Time
	err := tx.QueryRow(ctx,
		`SELECT COALESCE(status,''), data_entrega, reagendado_para
		   FROM sz_motoboy_pedidos WHERE wc_order_id = $1
		  ORDER BY id DESC LIMIT 1`, wcOrderKey).
		Scan(&m.statusMotoboy, &dataEntrega, &reagendadoPara)
	if err != nil {
		return m, err
	}
	if dataEntrega != nil {
		m.dataEntrega = *dataEntrega
		m.dataEntregaSet = true
	}
	if reagendadoPara != nil {
		m.reagendadoPara = *reagendadoPara
		m.reagendadoSet = true
	}
	return m, nil
}

// labelData — etiqueta/rastreio do fluxo expedição (wc_me_labels), com override
// manual de sz_order_meta::loadOrderMeta. Espelha a regra de precedência de
// go/admin/internal/handlers/order_detail.go::loadTrackingSection: meta._tracking_*
// VENCE (permite correção manual no portal); só cai para wc_me_labels quando o
// meta vier vazio.
type labelData struct {
	trackingCode string
	trackingURL  string
	carrier      string
}

func loadLabel(ctx context.Context, tx pgx.Tx, wcOrderKey int64, meta orderMeta) labelData {
	l := labelData{
		trackingCode: meta.trackingCode,
		trackingURL:  meta.trackingURL,
		carrier:      meta.trackingCarrier,
	}
	if l.trackingCode != "" && l.trackingURL != "" && l.carrier != "" {
		return l // meta já cobre tudo — não precisa consultar wc_me_labels
	}
	// NÃO selecionamos label_url/print_url aqui de propósito — são o link de
	// IMPRESSÃO da etiqueta (painel interno do Melhor Envio), nunca um link de
	// rastreio pro cliente final.
	var trackingCode, serviceName sql.NullString
	err := tx.QueryRow(ctx,
		`SELECT COALESCE(tracking_code,''), COALESCE(service_name,'')
		   FROM wc_me_labels WHERE wc_order_id = $1
		  ORDER BY id DESC LIMIT 1`, wcOrderKey).
		Scan(&trackingCode, &serviceName)
	if err != nil {
		return l // sem etiqueta ME — mantém só o que veio do meta (pode ser vazio)
	}
	if l.trackingCode == "" {
		l.trackingCode = trackingCode.String
	}
	// NUNCA usar label_url/print_url do ME como link_rastreamento — é o link de
	// IMPRESSÃO da etiqueta (painel interno do Melhor Envio), não um link de
	// rastreio pro cliente final. Sem override manual (_tracking_url), fica vazio.
	if l.carrier == "" {
		l.carrier = serviceName.String
	}
	return l
}

// addressData — endereço de entrega (tipo='shipping').
type addressData struct {
	nome        string
	email       string
	telefone    string
	cep         string
	logradouro  string
	numero      string
	complemento string
	bairro      string
	cidade      string
	uf          string
}

func loadShippingAddress(ctx context.Context, tx pgx.Tx, orderID int64) (addressData, error) {
	var a addressData
	err := tx.QueryRow(ctx,
		`SELECT COALESCE(nome,''), COALESCE(email,''), COALESCE(telefone,''),
		        COALESCE(cep,''), COALESCE(logradouro,''), COALESCE(numero,''),
		        COALESCE(complemento,''), COALESCE(bairro,''), COALESCE(cidade,''),
		        COALESCE(uf,'')
		   FROM sz_order_addresses
		  WHERE order_id = $1 AND tipo = 'shipping'
		  LIMIT 1`, orderID).
		Scan(&a.nome, &a.email, &a.telefone, &a.cep, &a.logradouro, &a.numero,
			&a.complemento, &a.bairro, &a.cidade, &a.uf)
	return a, err
}

// itemData — item do pedido, desmembrado (nome/SKU/qtd/preço unit separados —
// não concatenados numa string única) para o integrador conseguir montar nota
// fiscal/etiqueta sem parsear texto livre.
type itemData struct {
	nome       string
	sku        string
	quantidade int
	precoUnit  float64
	subtotal   float64
}

func loadItems(ctx context.Context, tx pgx.Tx, orderID int64) ([]itemData, error) {
	rows, err := tx.Query(ctx,
		`SELECT COALESCE(nome,''), COALESCE(sku,''), COALESCE(quantidade,1),
		        COALESCE(preco_unit,0), COALESCE(subtotal,0)
		   FROM sz_order_items WHERE order_id = $1 ORDER BY id`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []itemData{}
	for rows.Next() {
		var it itemData
		if err := rows.Scan(&it.nome, &it.sku, &it.quantidade, &it.precoUnit, &it.subtotal); err != nil {
			return out, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// orderMeta — subconjunto de sz_order_meta usado no payload (CPF + rastreio).
// Chaves espelham go/admin/internal/handlers/order_detail.go::loadTrackingSection
// (_billing_cpf, _tracking_code, _tracking_url, _tracking_carrier).
type orderMeta struct {
	cpf                  string
	trackingCode         string
	trackingURL          string
	trackingCarrier      string
	linkPagamentoCredito string
}

func loadOrderMeta(ctx context.Context, tx pgx.Tx, orderID int64) (orderMeta, error) {
	rows, err := tx.Query(ctx,
		`SELECT meta_key, meta_value FROM sz_order_meta
		  WHERE order_id = $1
		    AND meta_key IN ('_billing_cpf', '_tracking_code', '_tracking_url',
		                      '_tracking_carrier', '_link_pagamento_credito')`,
		orderID)
	if err != nil {
		return orderMeta{}, err
	}
	defer rows.Close()
	var m orderMeta
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return m, err
		}
		switch k {
		case "_billing_cpf":
			m.cpf = v
		case "_tracking_code":
			m.trackingCode = v
		case "_tracking_url":
			m.trackingURL = v
		case "_tracking_carrier":
			m.trackingCarrier = v
		case "_link_pagamento_credito":
			// PERSONALIZÁVEL: link de pagamento/crédito vinculado no checkout do
			// cliente (ex: link de retry de cartão, crédito parcial). Fonte é
			// sz_order_meta — quem gravar essa chave (checkout, admin, integração)
			// alimenta o webhook sem precisar mexer neste código de novo.
			m.linkPagamentoCredito = v
		}
	}
	return m, rows.Err()
}

// matchWebhooks busca webhooks ativos que assinam o evento. event_types vazio
// (jsonb_array_length=0) = assina TODOS; senão o operador jsonb `?` testa se o
// array contém a string do evento. (pgx repassa o `?` literal — verificado.)
//
// ROTEAMENTO POR PRODUTO: um webhook com product_id só casa se o pedido (orderID)
// contiver aquele produto em sz_order_items; product_id NULL = todos os produtos
// do usuário (comportamento original). O filtro vive no SQL — não em Go — porque
// loadItems não carrega produto_id (e não queremos forçar uma query extra).
func matchWebhooks(ctx context.Context, tx pgx.Tx, produtorID, userID int64, eventType string, orderID int64) ([]matchedWebhook, error) {
	rows, err := tx.Query(ctx,
		`SELECT id, url, COALESCE(secret,'')
		   FROM senderzz_portal_webhooks
		  WHERE user_id IN ($1, $2)
		    AND active
		    AND url <> ''
		    AND (jsonb_array_length(event_types) = 0 OR event_types ? $3)
		    AND (product_id IS NULL OR product_id IN (
		           SELECT produto_id FROM sz_order_items WHERE order_id = $4))
		  ORDER BY id`, produtorID, userID, eventType, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []matchedWebhook{}
	for rows.Next() {
		var h matchedWebhook
		if err := rows.Scan(&h.id, &h.url, &h.secret); err != nil {
			return out, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// ─────────────────────────────────────────────────────────────────────────────
// Atualizações do outbox
// ─────────────────────────────────────────────────────────────────────────────

func markSent(ctx context.Context, tx pgx.Tx, outboxID int64) error {
	_, err := tx.Exec(ctx,
		`UPDATE sz_webhook_outbox SET sent_at = now() WHERE id = $1`, outboxID)
	return err
}

// bumpAttempt incrementa a tentativa e reagenda com backoff quadrático.
// O RHS do SET é avaliado contra os valores ANTIGOS da linha, então attempts+1 é
// pré-incremento → next_attempt em 1,4,9,16,25,36 min. O predicado
// attempts < max_attempts (na SELECT do lote) cessa após 6 tentativas.
func bumpAttempt(ctx context.Context, tx pgx.Tx, outboxID int64, lastErr string) error {
	return bumpAttemptWithDelivered(ctx, tx, outboxID, lastErr, nil)
}

// bumpAttemptWithDelivered — igual a bumpAttempt, mas também persiste a lista
// atualizada de webhook_id que já confirmaram 2xx nesta linha (AUDIT-2026-07-30),
// pra que o próximo retry não reenvie a quem já recebeu com sucesso.
func bumpAttemptWithDelivered(ctx context.Context, tx pgx.Tx, outboxID int64, lastErr string, delivered []int64) error {
	if len(lastErr) > 1000 {
		lastErr = lastErr[:1000]
	}
	_, err := tx.Exec(ctx,
		`UPDATE sz_webhook_outbox
		    SET attempts           = attempts + 1,
		        next_attempt_at    = now() + make_interval(mins => power(attempts + 1, 2)),
		        last_error         = $2,
		        delivered_hook_ids = $3
		  WHERE id = $1`, outboxID, lastErr, delivered)
	return err
}

// ─────────────────────────────────────────────────────────────────────────────
// Log de entrega
// ─────────────────────────────────────────────────────────────────────────────

// logDelivery grava UMA linha em senderzz_webhook_log (sucesso ou falha).
// response_body é truncado em respBodyTruncateChars. Em caso de erro de rede,
// response_code fica NULL e o corpo carrega a mensagem de erro.
func logDelivery(ctx context.Context, tx pgx.Tx, webhookID int64, eventType string, payload []byte, code int, respBody string, fireErr error) {
	var codePtr *int
	body := respBody
	if fireErr != nil {
		body = "erro: " + fireErr.Error()
	} else {
		c := code
		codePtr = &c
	}
	if len(body) > respBodyTruncateChars {
		body = body[:respBodyTruncateChars]
	}
	// event_type tem limite varchar(50) na coluna.
	ev := eventType
	if len(ev) > 50 {
		ev = ev[:50]
	}
	_, err := tx.Exec(ctx,
		`INSERT INTO senderzz_webhook_log
		   (webhook_id, event_type, payload, response_code, response_body)
		 VALUES ($1, $2, $3::jsonb, $4, $5)`,
		webhookID, ev, string(payload), codePtr, body)
	if err != nil {
		slog.Error("[webhook] falha ao gravar senderzz_webhook_log", "webhook_id", webhookID, "err", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Disparo HTTP + assinatura + SSRF guard
// ─────────────────────────────────────────────────────────────────────────────

// fireWebhook faz o POST assinado. SSRF guard ANTES do request. Devolve
// (status_code, response_body, erro). Em SSRF bloqueado / erro de montagem /
// erro de rede, code=0 e err != nil.
func fireWebhook(ctx context.Context, h matchedWebhook, eventType string, body []byte) (int, string, error) {
	// SSRF GUARD — bloqueia destino interno/privado antes de qualquer conexão.
	if err := validatePublicURL(h.url); err != nil {
		return 0, "", fmt.Errorf("ssrf bloqueado: %w", err)
	}

	mac := hmac.New(sha256.New, []byte(h.secret))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	reqCtx, cancel := context.WithTimeout(ctx, httpTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, h.url, bytes.NewReader(body))
	if err != nil {
		return 0, "", fmt.Errorf("montar request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Senderzz-Event", eventType)
	req.Header.Set("X-Senderzz-Signature", sig)
	req.Header.Set("User-Agent", "Senderzz-Webhook-Dispatch/1.0")

	// AUDIT-2026-06-21 H3 — client endurecido: DialContext.Control rejeita o IP
	// interno NO MOMENTO da conexão (fecha DNS-rebinding/TOCTOU) e CheckRedirect
	// revalida cada salto de 302 com validatePublicURL (um 302 p/ IP interno NÃO
	// fura mais a validatePublicURL de cadastro). A pré-validação em (h.url) acima
	// permanece como defesa em profundidade.
	client := newSSRFSafeClient(httpTimeout)
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, respBodyReadLimit))
	return resp.StatusCode, string(b), nil
}

// isBlockedIP — true quando o IP pertence a faixa que NUNCA deve ser alvo de
// webhook externo (loopback, privado, link-local, metadata, CGNAT, não-especificado,
// multicast). Cobre IPv4 e IPv6. Cópia de
// go/admin/internal/handlers/expedicao_webhooks.go::isBlockedIP (não importamos
// cross-módulo — convenção da task).
//
// EXCEÇÃO DE TESTE: se WEBHOOK_ALLOW_LOOPBACK=1, loopback é permitido (smoke local).
// Lido via os.Getenv no momento da chamada — não cacheado — para que t.Setenv funcione.
func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true // não resolveu → fail-closed
	}
	if ip.IsLoopback() {
		// Exceção controlada por env só para smoke test local.
		if os.Getenv("WEBHOOK_ALLOW_LOOPBACK") == "1" {
			return false
		}
		return true
	}
	if ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return true
	}
	// 100.64.0.0/10 — CGNAT (RFC 6598), não coberto por IsPrivate().
	if v4 := ip.To4(); v4 != nil {
		if v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
			return true
		}
	}
	return false
}

// validatePublicURL valida scheme/host e RESOLVE o host, rejeitando destino
// interno/privado ANTES de qualquer request HTTP. Espelha
// expedicao_webhooks.go::validatePublicURL. Um host que resolve p/ múltiplos IPs
// é rejeitado se QUALQUER IP for bloqueado (evita rebinding parcial).
func validatePublicURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("url vazia")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("url inválida: %v", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("url deve começar com http:// ou https://")
	}
	// AUDIT-LGPD-2026-06-24 A1 (Art. 46/47/49) — TLS obrigatório no dispatch de
	// pedido. O payload carrega PII completa do cliente (blocos cliente/entrega: nome,
	// telefone, e-mail, endereço) — ver buildPayload. HMAC dá integridade, não
	// confidencialidade, então http:// em texto claro vazaria os dados pessoais na
	// rede. Exigimos https://; também fecha redirect-para-http (CheckRedirect revalida
	// por aqui). EXCEÇÃO: o mesmo WEBHOOK_ALLOW_LOOPBACK=1 que libera loopback no
	// isBlockedIP libera http no smoke local (httptest serve http://127.0.0.1);
	// produção NUNCA seta essa env, então produção sempre exige TLS.
	if u.Scheme != "https" && os.Getenv("WEBHOOK_ALLOW_LOOPBACK") != "1" {
		return fmt.Errorf("dispatch com dados pessoais exige https:// (http em texto claro bloqueado)")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("url sem host")
	}

	// Host já é IP literal? Valida direto, sem DNS.
	if lit := net.ParseIP(host); lit != nil {
		if isBlockedIP(lit) {
			return fmt.Errorf("destino interno/privado bloqueado: %s", host)
		}
		return nil
	}

	resCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIPAddr(resCtx, host)
	if err != nil {
		return fmt.Errorf("não foi possível resolver o host %q: %v", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("host %q não resolveu para nenhum IP", host)
	}
	for _, ipa := range ips {
		if isBlockedIP(ipa.IP) {
			return fmt.Errorf("destino interno/privado bloqueado: %s (%s)", host, ipa.IP)
		}
	}
	return nil
}

// AUDIT-2026-06-21 H3 — endurecimento do client (anti-rebinding + anti-redirect-SSRF).
// Espelha go/portal/internal/handlers/webhooks.go::{ssrfSafeControl,newSSRFSafeClient}
// e go/portal/internal/jobs/webhook_dispatcher.go. A validatePublicURL acima valida
// o host NO CADASTRO/PRÉ-REQUEST, mas sozinha NÃO cobre:
//   - DNS-rebinding (TOCTOU): o IP resolvido na validação pode diferir do IP do Dial.
//   - Redirect-SSRF: um 302 p/ http://169.254.169.254 (ou IP privado) era seguido
//     pelo client default sem revalidar — fura a validatePublicURL.
//
// Por isso: Control rejeita o IP no momento da conexão e CheckRedirect revalida
// cada salto. NÃO importamos cross-módulo (convenção da task).
var errBlockedInternalDest = errors.New("destino interno/privado bloqueado (SSRF)")

// ssrfSafeControl é o hook Control de net.Dialer: roda APÓS o resolve real do
// dialer, com o endereço já no formato ip:port. Rejeita a conexão se o IP for
// interno/privado — fecha a janela de DNS-rebinding que a validação de URL sozinha
// não cobre.
func ssrfSafeControl(_ string, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return errBlockedInternalDest // formato inesperado → fail-closed
	}
	if isBlockedIP(net.ParseIP(host)) {
		return errBlockedInternalDest
	}
	return nil
}

// newSSRFSafeClient devolve um *http.Client endurecido contra SSRF:
//   - DialContext.Control bloqueia IP interno NO MOMENTO da conexão (anti-rebinding).
//   - CheckRedirect revalida cada salto com validatePublicURL (anti-redirect-SSRF).
//
// timeout é o timeout total do request (mesmo papel do http.Client.Timeout antigo).
func newSSRFSafeClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   ssrfSafeControl,
	}
	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: timeout,
		DisableKeepAlives:     true,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			// Cada salto de redirect deve apontar p/ destino externo. O Control do
			// dialer ainda revalida o IP na conexão; isto barra cedo (e barra
			// redirects p/ esquemas/hosts internos antes de discar).
			if err := validatePublicURL(req.URL.String()); err != nil {
				return fmt.Errorf("redirect bloqueado: %w", err)
			}
			return nil
		},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Montagem do payload (mesmo contrato PT-BR de
// go/admin/internal/handlers/expedicao_webhooks.go::samplePayload)
// ─────────────────────────────────────────────────────────────────────────────

// buildPayload monta o JSON no MESMO contrato de samplePayload(), mas com dados
// REAIS do pedido. event = event_type da linha do outbox. status_ativo = true
// (consistente com o exemplo). PARIDADE DE CONTRATO: mantemos TODAS as chaves de
// topo de samplePayload() (rastreamento, link_rastreamento, transportadora,
// servico, classe_entrega, frete, etc.) e TODAS as subchaves do exemplo — onde os
// dados não vivem nas 3 tabelas (sz_orders/sz_order_addresses/sz_order_items) usamos
// zero-value coerente (string vazia, 0, []). Isso evita que uma integração testada
// via o botão "Testar" (que usa samplePayload) leia `undefined` em produção.
func buildPayload(row outboxRow, o orderData, a addressData, items []itemData, m orderMeta,
	fin financialsData, mot motoboyData, lab labelData, isMotoboy bool) map[string]any {
	itemsOut := make([]map[string]any, 0, len(items))
	quantidadeTotal := 0
	for _, it := range items {
		quantidadeTotal += it.quantidade
		itemsOut = append(itemsOut, map[string]any{
			"nome":               it.nome,
			"sku":                it.sku,
			"quantidade":         it.quantidade,
			"preco_unitario":     it.precoUnit,
			"preco_unit_format":  formatBRL(it.precoUnit),
			"subtotal":           it.subtotal,
			"subtotal_formatado": formatBRL(it.subtotal),
		})
	}
	if len(itemsOut) == 0 {
		itemsOut = []map[string]any{}
	}

	tipoPedido := "exp"
	if isMotoboy {
		tipoPedido = "cod"
	}

	payload := map[string]any{
		"event":        row.eventType,
		"status_ativo": true,
		"tipo_pedido":  tipoPedido, // "cod" (motoboy) | "exp" (expedição PAC/ME)
		"pedido": map[string]any{
			"id":                 o.id,
			"numero":             o.orderNumber,
			"status":             o.status,
			"status_anterior":    row.statusDe, // extra útil (não estava no sample)
			"subtotal":           o.subtotal,
			"subtotal_formatado": formatBRL(o.subtotal),
			"total":              o.total,
			"total_formatado":    formatBRL(o.total),
			"desconto":           0,
			"desconto_formatado": "",
			"metodo_pagamento":   o.paymentMethod,
			"status_pagamento":   o.paymentStatus, // extra útil (não estava no sample)
			"criado_em":          o.createdAt.Format(time.RFC3339),
			"atualizado_em":      o.updatedAt.Format(time.RFC3339),
			// Datas de marco não vivem nestas 3 tabelas → zero-value.
			"pago_em":     "",
			"enviado_em":  "",
			"entregue_em": "",
		},
		// AUDIT-FINANCEIRO-2026-06-25: liquido_produtor é o valor REAL a receber pelo
		// produtor (já descontadas taxa de plataforma + taxa de entrega). Vem de
		// producer_net_live (generated column), NÃO da coluna inerte producer_net.
		"financeiro": map[string]any{
			"valor_bruto":             o.gross,
			"valor_bruto_formatado":   formatBRL(o.gross),
			"valor_liquido_produtor":  fin.liquidoProdutor,
			"valor_liquido_formatado": formatBRL(fin.liquidoProdutor),
			// taxa_plataforma NUNCA vai no webhook — receita interna da plataforma,
			// não é informação pro produtor/cliente ver via integração externa.
			"taxa_entrega":           fin.taxaEntrega,
			"taxa_entrega_formatado": formatBRL(fin.taxaEntrega),
			"delivery_fee":           fin.deliveryFee,
			"delivery_fee_formatado": formatBRL(fin.deliveryFee),
		},
		"classe_entrega": map[string]any{
			"id":   o.shippingClassID,
			"nome": o.shippingClass,
			"slug": slugify(o.shippingClass),
		},
		"frete": map[string]any{
			"valor":            o.shipping,
			"valor_formatado":  formatBRL(o.shipping),
			"prazo_dias_uteis": 0,  // não disponível nas 3 tabelas
			"transportadora":   "", // idem
			"servico":          "", // idem
		},
		"cliente": map[string]any{
			"nome":              firstNonEmpty(a.nome, o.customerName),
			"telefone":          a.telefone,
			"telefone_completo": a.telefone, // sem normalização DDI nestas tabelas
			"email":             firstNonEmpty(a.email, o.billingEmail),
			"cpf":               m.cpf,
		},
		"entrega": map[string]any{
			"nome":        a.nome,
			"cep":         a.cep,
			"endereco":    a.logradouro,
			"numero":      a.numero,
			"complemento": a.complemento,
			"bairro":      a.bairro,
			"cidade":      a.cidade,
			"estado":      a.uf,
		},
		"itens":            itemsOut,
		"quantidade_itens": quantidadeTotal, // total de unidades, somado — não precisa contar array
		// PERSONALIZÁVEL: vem de sz_order_meta._link_pagamento_credito. Vazio até
		// alguém gravar essa chave pro pedido (checkout/admin/integração externa).
		"link_pagamento_credito": m.linkPagamentoCredito,
	}

	// Blocos MUTUAMENTE EXCLUSIVOS por tipo_pedido — nunca os dois juntos:
	//   - COD/motoboy: entrega própria, tem data agendada desde a criação, NUNCA
	//     gera código de rastreio rastreável externamente.
	//   - Expedição: rastreio real do Melhor Envio, só existe DEPOIS da etiqueta
	//     emitida; nunca tem data de entrega prevista de antemão.
	if isMotoboy {
		payload["agendamento"] = map[string]any{
			"status_motoboy":  mot.statusMotoboy,
			"data_entrega":    formatDateOrEmpty(mot.dataEntrega, mot.dataEntregaSet),
			"reagendado_para": formatDateOrEmpty(mot.reagendadoPara, mot.reagendadoSet),
		}
		payload["rastreamento"] = []string{}
		payload["link_rastreamento"] = ""
		payload["transportadora"] = ""
		payload["servico"] = ""
	} else {
		payload["rastreamento"] = trackingCodes(lab.trackingCode)
		payload["link_rastreamento"] = lab.trackingURL
		payload["transportadora"] = lab.carrier
		payload["servico"] = ""
	}

	return payload
}

// formatDateOrEmpty formata em RFC3339 quando set=true (ponteiro válido no banco);
// "" quando a coluna era NULL — evita "0001-01-01T00:00:00Z" (zero-value de
// time.Time) vazando pro integrador como se fosse uma data real.
func formatDateOrEmpty(t time.Time, set bool) string {
	if !set {
		return ""
	}
	return t.Format(time.RFC3339)
}

// trackingCodes empacota o código de rastreio único num array (mantém o
// contrato "rastreamento": []string usado desde samplePayload). Vazio → [].
func trackingCodes(code string) []string {
	if strings.TrimSpace(code) == "" {
		return []string{}
	}
	return []string{code}
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// formatBRL formata um valor como "R$ 1.234,56" (pt-BR). Espelha o *_formatado de
// samplePayload(). Valor zero → "R$ 0,00".
func formatBRL(v float64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	cents := int64(v*100 + 0.5)
	reais := cents / 100
	cent := cents % 100
	// Milhar com ponto.
	s := strconv.FormatInt(reais, 10)
	var b strings.Builder
	n := len(s)
	for i, c := range s {
		if i > 0 && (n-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	sign := ""
	if neg {
		sign = "-"
	}
	return fmt.Sprintf("%sR$ %s,%02d", sign, b.String(), cent)
}

// slugify deriva um slug do nome da classe (lowercase, espaços → hífens). Espelha
// a derivação de slug em expedicao_webhooks.go::Classes.
func slugify(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return strings.ToLower(strings.ReplaceAll(name, " ", "-"))
}
