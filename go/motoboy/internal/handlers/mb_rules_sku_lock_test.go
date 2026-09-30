// FEAT-SKU — TRAVA do SKU-bip (regressão end-to-end com banco).
//
// Regra do dono: "validação de sku do produto com o que ta no pedido se for
// diferente nao deixa seguir". Ao iniciar rota (e ao devolver após frustrado) o
// motoboy bipa o código de barras do produto; o SKU lido TEM que casar com algum
// item do pedido. Se divergir → 422 e o status do pedido NÃO avança. Digitar
// (manual_typed=true) é só um MÉTODO de entrada permitido — NÃO é bypass: o SKU
// digitado ainda precisa bater com o item.
//
// Estes testes exercitam o HANDLER COMPLETO (rota.go IniciarRota / motoboy_ops.go
// DevolverQR) através do middleware real auth.AuthMotoboy, contra o Postgres real
// — é o único nível em que "o status avança" / "não avança" pode ser observado.
// As funções puras de mb_rules.go (normalizarSKU etc.) já têm cobertura em
// mb_rules_test.go; aqui cobrimos só o buraco: SKU-bipado-vs-item-de-pedido +
// efeito no status + trilha sz_pack_scans.
//
// Isolamento: IDs sintéticos altos (>= 990000000), um pedido novo por cenário,
// limpeza via t.Cleanup (DELETE pelos ids sintéticos). NÃO altera dados reais.
// SKIP silencioso quando DATABASE_URL ausente ou banco inacessível.
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/motoboy-service/internal/auth"
)

// skuSalt é o WP_SALT_AUTH usado para gerar/regenerar o QR da etiqueta nos testes
// (o mesmo segredo que packageCode() consome via wpSaltAuth()).
const skuSalt = "salt-feat-sku-trava-32-bytes-ou-mais-xxxx"

// Faixa de IDs sintéticos — bem acima de qualquer dado real para evitar colisão.
const skuBaseID int64 = 990000000

// skuTestPool conecta ao Postgres de DATABASE_URL. Retorna nil quando a env não
// está definida ou a conexão falha — o caller faz t.Skip (SKIP silencioso, não
// quebra o build/CI sem banco).
func skuTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dsn == "" {
		t.Skip("DATABASE_URL não definida — pulando teste de integração da trava SKU")
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("DATABASE_URL definida mas pool falhou (%v) — pulando", err)
		return nil
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("DATABASE_URL definida mas Ping falhou (%v) — pulando", err)
		return nil
	}
	t.Cleanup(pool.Close)
	return pool
}

// skuFixtureIDs agrupa os ids sintéticos de um cenário (um conjunto por pedido).
type skuFixtureIDs struct {
	motoboyID int64
	orderID   int64 // sz_orders.id
	wcOrderID int64 // sz_orders.wp_order_id == sz_motoboy_pedidos.wc_order_id
	pedidoID  int64 // sz_motoboy_pedidos.id
}

// seedSKUFixture cria motoboy + sz_orders + (opcional) item com SKU + pedido
// motoboy 'embalado' atribuído ao motoboy. itemSKU=="" → pedido SEM SKU
// cadastrado (cenário fail-closed). Registra a limpeza via t.Cleanup.
//
// O JOIN de validarSKUPedido casa por COALESCE(o.wp_order_id, o.id)=wc_order_id;
// definimos wp_order_id = wcOrderID para amarrar item↔pedido motoboy. O bridge
// (em_rota→enviado) casa por o.wp_order_id = p.wc_order_id — mesma amarração.
func seedSKUFixture(t *testing.T, pool *pgxpool.Pool, offset int64, itemSKU string) skuFixtureIDs {
	t.Helper()
	ctx := context.Background()
	ids := skuFixtureIDs{
		motoboyID: skuBaseID + offset,
		orderID:   skuBaseID + offset,
		wcOrderID: skuBaseID + offset,
		pedidoID:  skuBaseID + offset,
	}
	tokenApp := fmt.Sprintf("tok-sku-%d", ids.motoboyID)

	// Limpeza registrada ANTES dos inserts: roda mesmo se um insert falhar no meio.
	// Ordem: filhos antes dos pais. sz_order_items cai por CASCADE ao deletar a
	// order, mas deletamos explícito por robustez. sz_pack_scans/audit são escritos
	// pelo handler (sucesso E bloqueio gravam scan; sucesso grava audit).
	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = pool.Exec(cctx, `DELETE FROM sz_pack_scans WHERE wc_order_id=$1`, ids.wcOrderID)
		_, _ = pool.Exec(cctx, `DELETE FROM sz_motoboy_audit WHERE pedido_id=$1`, ids.pedidoID)
		_, _ = pool.Exec(cctx, `DELETE FROM sz_motoboy_pedidos WHERE id=$1`, ids.pedidoID)
		_, _ = pool.Exec(cctx, `DELETE FROM sz_order_items WHERE order_id=$1`, ids.orderID)
		_, _ = pool.Exec(cctx, `DELETE FROM sz_orders WHERE id=$1`, ids.orderID)
		_, _ = pool.Exec(cctx, `DELETE FROM sz_motoboys WHERE id=$1`, ids.motoboyID)
	})

	if _, err := pool.Exec(ctx, `
		INSERT INTO sz_motoboys (id, cd_id, nome, telefone, token_app, ativo)
		OVERRIDING SYSTEM VALUE
		VALUES ($1, 1, 'MB Sintetico SKU', '11999990000', $2, true)`,
		ids.motoboyID, tokenApp,
	); err != nil {
		t.Fatalf("seed sz_motoboys falhou: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO sz_orders (id, order_number, wp_order_id, user_id, produtor_id, status)
		OVERRIDING SYSTEM VALUE
		VALUES ($1, $2, $3, 1, 1, 'embalado')`,
		ids.orderID, fmt.Sprintf("SKU-%d", ids.orderID), ids.wcOrderID,
	); err != nil {
		t.Fatalf("seed sz_orders falhou: %v", err)
	}

	if itemSKU != "" {
		if _, err := pool.Exec(ctx, `
			INSERT INTO sz_order_items (order_id, produto_id, nome, sku, quantidade)
			VALUES ($1, $2, 'Produto Sintetico', $3, 1)`,
			ids.orderID, skuBaseID+offset, itemSKU,
		); err != nil {
			t.Fatalf("seed sz_order_items falhou: %v", err)
		}
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO sz_motoboy_pedidos (id, wc_order_id, cd_id, zona_id, motoboy_id, status, dest_cep)
		OVERRIDING SYSTEM VALUE
		VALUES ($1, $2, 1, 1, $3, 'embalado', '01310100')`,
		ids.pedidoID, ids.wcOrderID, ids.motoboyID,
	); err != nil {
		t.Fatalf("seed sz_motoboy_pedidos falhou: %v", err)
	}

	return ids
}

// callIniciarRota dispara POST /motoboy/iniciar-rota através do middleware real
// auth.AuthMotoboy (header X-MB-Token) — exatamente como em produção. Retorna o
// status HTTP e o corpo decodificado.
func callIniciarRota(t *testing.T, pool *pgxpool.Pool, tokenApp string, body map[string]any) (int, map[string]any) {
	t.Helper()
	t.Setenv("WP_SALT_AUTH", skuSalt)

	h := &RotaHandler{Pool: pool}
	handler := auth.AuthMotoboy(pool)(http.HandlerFunc(h.IniciarRota))

	payload, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/motoboy/iniciar-rota", strings.NewReader(string(payload)))
	req.Header.Set("X-MB-Token", tokenApp)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// statusDoPedido lê o status atual do pedido motoboy (prova de avanço/não-avanço).
func statusDoPedido(t *testing.T, pool *pgxpool.Pool, pedidoID int64) string {
	t.Helper()
	var status string
	if err := pool.QueryRow(context.Background(),
		`SELECT status FROM sz_motoboy_pedidos WHERE id=$1`, pedidoID,
	).Scan(&status); err != nil {
		t.Fatalf("falha ao ler status do pedido %d: %v", pedidoID, err)
	}
	return status
}

// packScan resume a última leitura gravada em sz_pack_scans para um pedido.
type packScan struct {
	context     string
	skuScanned  string
	skuExpected *string
	matched     bool
	manualTyped bool
	productID   *int64
	actor       *string
	wcOrderID   *int64
}

// ultimoPackScan lê a leitura mais recente do pedido (ou ok=false se não houver).
func ultimoPackScan(t *testing.T, pool *pgxpool.Pool, wcOrderID int64) (packScan, bool) {
	t.Helper()
	var ps packScan
	err := pool.QueryRow(context.Background(), `
		SELECT context, sku_scanned, sku_expected, matched, manual_typed, product_id, actor, wc_order_id
		  FROM sz_pack_scans
		 WHERE wc_order_id=$1
		 ORDER BY id DESC
		 LIMIT 1`, wcOrderID,
	).Scan(&ps.context, &ps.skuScanned, &ps.skuExpected, &ps.matched, &ps.manualTyped, &ps.productID, &ps.actor, &ps.wcOrderID)
	if err != nil {
		return packScan{}, false
	}
	return ps, true
}

// ── Cenário 1: SKU CASA o item → avança (em_rota) + scan matched=true ────────
//
// Também é a prova de que a FIXTURE está bem cabeada (item↔pedido↔order). Se o
// JOIN não achasse o item, HasExpected=false e o handler bloquearia (fail-closed)
// — o 200 + status em_rota aqui garante que o match real está acontecendo, não um
// falso-positivo de fail-closed.
func TestSkuLock_IniciarRota_Casa_Avanca(t *testing.T) {
	pool := skuTestPool(t)
	if pool == nil {
		return
	}
	const sku = "SKU-CASA-001"
	ids := seedSKUFixture(t, pool, 11, sku)
	tokenApp := fmt.Sprintf("tok-sku-%d", ids.motoboyID)

	qr := packageCode(ids.pedidoID, ids.wcOrderID, skuSalt)

	code, out := callIniciarRota(t, pool, tokenApp, map[string]any{
		"package_code": qr,
		"pedido_id":    ids.pedidoID,
		"sku_bipado":   sku,
		"manual_typed": false,
	})

	if code != http.StatusOK {
		t.Fatalf("SKU casando deveria avançar (200), veio %d: %v", code, out)
	}
	if got := statusDoPedido(t, pool, ids.pedidoID); got != "em_rota" {
		t.Fatalf("status deveria avançar para em_rota, está %q", got)
	}

	ps, ok := ultimoPackScan(t, pool, ids.wcOrderID)
	if !ok {
		t.Fatal("deveria existir um sz_pack_scans para a leitura que casou")
	}
	if !ps.matched {
		t.Error("scan da leitura que casou deveria ter matched=true")
	}
	if ps.context != "em_rota" {
		t.Errorf("context do scan deveria ser 'em_rota', veio %q", ps.context)
	}
	if ps.wcOrderID == nil || *ps.wcOrderID != ids.wcOrderID {
		t.Errorf("scan deveria ter wc_order_id=%d, veio %v", ids.wcOrderID, ps.wcOrderID)
	}
	if ps.skuExpected == nil || !strings.Contains(strings.ToUpper(*ps.skuExpected), sku) {
		t.Errorf("sku_expected deveria conter o SKU do pedido (%s), veio %v", sku, ps.skuExpected)
	}
	if ps.productID == nil {
		t.Error("scan que casou deveria registrar product_id do item")
	}
	if ps.manualTyped {
		t.Error("manual_typed deveria ser false (leitura por câmera)")
	}
}

// ── Cenário 2: SKU DIVERGE → BLOQUEIA (422) + status NÃO avança + matched=false
//
// Este é o coração da TRAVA. Se este teste falhar (status virou em_rota apesar do
// SKU errado) → a trava NÃO funciona = achado CRÍTICO.
func TestSkuLock_IniciarRota_Diverge_Bloqueia(t *testing.T) {
	pool := skuTestPool(t)
	if pool == nil {
		return
	}
	const skuPedido = "SKU-CORRETO-002"
	const skuErrado = "SKU-OUTRO-PRODUTO-999"
	ids := seedSKUFixture(t, pool, 12, skuPedido)
	tokenApp := fmt.Sprintf("tok-sku-%d", ids.motoboyID)

	qr := packageCode(ids.pedidoID, ids.wcOrderID, skuSalt)

	code, out := callIniciarRota(t, pool, tokenApp, map[string]any{
		"package_code": qr,
		"pedido_id":    ids.pedidoID,
		"sku_bipado":   skuErrado,
		"manual_typed": false,
	})

	if code != http.StatusUnprocessableEntity {
		t.Fatalf("ACHADO CRÍTICO se !=422: SKU divergente deveria BLOQUEAR (422), veio %d: %v", code, out)
	}
	if got := statusDoPedido(t, pool, ids.pedidoID); got != "embalado" {
		t.Fatalf("ACHADO CRÍTICO: status NÃO pode avançar com SKU divergente; deveria seguir 'embalado', está %q", got)
	}

	ps, ok := ultimoPackScan(t, pool, ids.wcOrderID)
	if !ok {
		t.Fatal("a leitura divergente deveria ser registrada em sz_pack_scans (auditoria)")
	}
	if ps.matched {
		t.Error("scan de SKU divergente deveria ter matched=false")
	}
	if ps.context != "em_rota" {
		t.Errorf("context do scan deveria ser 'em_rota', veio %q", ps.context)
	}
	if ps.skuExpected == nil || !strings.Contains(strings.ToUpper(*ps.skuExpected), skuPedido) {
		t.Errorf("sku_expected deveria listar o SKU correto do pedido (%s), veio %v", skuPedido, ps.skuExpected)
	}
	if strings.ToUpper(ps.skuScanned) != skuErrado {
		t.Errorf("sku_scanned deveria registrar o que foi bipado (%s), veio %q", skuErrado, ps.skuScanned)
	}
}

// ── Cenário 3: entrada DIGITADA (manual_typed=true) com SKU CORRETO → permitida
//
// "Digitar é fallback permitido": é um MÉTODO de entrada, não um bypass. O SKU
// digitado ainda precisa casar — aqui casa → avança e persiste manual_typed=true.
func TestSkuLock_IniciarRota_DigitadoCorreto_Permitido(t *testing.T) {
	pool := skuTestPool(t)
	if pool == nil {
		return
	}
	const sku = "SKU-DIGITADO-003"
	ids := seedSKUFixture(t, pool, 13, sku)
	tokenApp := fmt.Sprintf("tok-sku-%d", ids.motoboyID)

	qr := packageCode(ids.pedidoID, ids.wcOrderID, skuSalt)

	// Digita em minúsculas/espacos — normalizarSKU (trim+upper) deve casar.
	code, out := callIniciarRota(t, pool, tokenApp, map[string]any{
		"package_code": qr,
		"pedido_id":    ids.pedidoID,
		"sku_bipado":   "  sku-digitado-003  ",
		"manual_typed": true,
	})

	if code != http.StatusOK {
		t.Fatalf("SKU digitado CORRETO deveria avançar (200), veio %d: %v", code, out)
	}
	if got := statusDoPedido(t, pool, ids.pedidoID); got != "em_rota" {
		t.Fatalf("status deveria avançar para em_rota com SKU digitado correto, está %q", got)
	}

	ps, ok := ultimoPackScan(t, pool, ids.wcOrderID)
	if !ok {
		t.Fatal("deveria existir sz_pack_scans para a entrada digitada")
	}
	if !ps.matched {
		t.Error("entrada digitada correta deveria ter matched=true")
	}
	if !ps.manualTyped {
		t.Error("manual_typed deveria persistir TRUE para entrada digitada (fallback)")
	}
}

// ── Cenário 4 (borda): pedido SEM SKU cadastrado → fail-closed (422 + matched=false)
//
// Sem referência de SKU no pedido não há como conferir que é o produto certo;
// a regra é fail-closed (rota.go: ok := match.OK && match.HasExpected). Bloqueia,
// não avança, e registra matched=false com sku_expected NULL. Propriedade de
// segurança explícita — não deixar passar "na dúvida".
func TestSkuLock_IniciarRota_PedidoSemSku_FailClosed(t *testing.T) {
	pool := skuTestPool(t)
	if pool == nil {
		return
	}
	ids := seedSKUFixture(t, pool, 14, "") // sem item/SKU
	tokenApp := fmt.Sprintf("tok-sku-%d", ids.motoboyID)

	qr := packageCode(ids.pedidoID, ids.wcOrderID, skuSalt)

	code, out := callIniciarRota(t, pool, tokenApp, map[string]any{
		"package_code": qr,
		"pedido_id":    ids.pedidoID,
		"sku_bipado":   "QUALQUER-SKU",
		"manual_typed": false,
	})

	if code != http.StatusUnprocessableEntity {
		t.Fatalf("pedido sem SKU deveria ser fail-closed (422), veio %d: %v", code, out)
	}
	if got := statusDoPedido(t, pool, ids.pedidoID); got != "embalado" {
		t.Fatalf("fail-closed: status NÃO pode avançar; deveria seguir 'embalado', está %q", got)
	}

	ps, ok := ultimoPackScan(t, pool, ids.wcOrderID)
	if !ok {
		t.Fatal("fail-closed deveria registrar a tentativa em sz_pack_scans")
	}
	if ps.matched {
		t.Error("fail-closed (pedido sem SKU) deveria ter matched=false")
	}
	if ps.skuExpected != nil {
		t.Errorf("sem SKU cadastrado, sku_expected deveria ser NULL, veio %v", *ps.skuExpected)
	}
}
