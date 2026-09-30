// Teste de INTEGRAÇÃO (Postgres real) do fix P1 LGPD de minimização de PII no
// endpoint GET /portal/motoboy — o caminho 200 que o teste hermético (Pool=nil,
// gate-antes-do-DB) NÃO alcança, porque o mascaramento roda DEPOIS da query.
//
// Prova:
//   - AFILIADO  → a PII do cliente (nome/telefone/CPF/email/endereço/complemento)
//                 vem VAZIA, MAS a comissão do afiliado (financeiro permitido) segue
//                 presente → minimização sem quebrar o que o afiliado pode ver.
//   - PRODUTOR  → o MESMO pedido vem com a PII PREENCHIDA (não há regressão p/ quem
//                 PRECISA operar).
//
// Gate: pula (t.Skip) se DATABASE_URL não estiver setada — mesma convenção de
// lgpd_db_test.go. Isolamento: IDs sintéticos de namespace ALTO e único; t.Cleanup
// remove tudo (ON DELETE CASCADE de sz_order_addresses/sz_order_meta cobre os filhos);
// SEM t.Parallel; NÃO toca dados reais.
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/portal-service/internal/auth"
)

// piiTestBase — namespace sintético alto p/ todos os IDs deste teste (orders, users,
// wp_order_id, affiliate/produtor). Improvável em dados de produção/dev.
const piiTestBase int64 = 991000000

// getReqAs monta um GET com um PortalUser (id/wpUserID/role) no contexto, simulando
// o pós-AuthPortalJWT. List é GET e escopa por u.ID (produtor) / u.WPUserID (afiliado).
func getReqAs(id, wpUserID int64, role string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/portal/motoboy", nil)
	u := &auth.PortalUser{ID: id, WPUserID: wpUserID, Email: "syn-pii@lgpd.test", Role: role}
	return req.WithContext(auth.ContextWithUser(context.Background(), u))
}

// seedMotoboyPIIOrder cria UM pedido motoboy sintético com PII completa, atribuído a
// produtorID (produtor_id) E affiliateWP (affiliate_id), p/ ambos os roles o lerem.
// Retorna o sz_orders.id criado. Limpa em t.Cleanup (CASCADE cobre addr/meta).
func seedMotoboyPIIOrder(t *testing.T, pool *pgxpool.Pool, produtorID, affiliateWP, wpOrderID int64) {
	t.Helper()
	ctx := context.Background()
	orderNum := "PIITST" + strconv.FormatInt(wpOrderID%1000000, 10)

	cleanup := func() {
		// Apaga o motoboy_pedido por wc_order_id e o pedido por wp_order_id (CASCADE
		// remove addresses/meta). Best-effort, idempotente.
		_, _ = pool.Exec(ctx, `DELETE FROM sz_motoboy_pedidos WHERE wc_order_id = $1`, wpOrderID)
		_, _ = pool.Exec(ctx, `DELETE FROM sz_orders WHERE wp_order_id = $1`, wpOrderID)
	}
	cleanup()
	t.Cleanup(cleanup)

	// 1) Pedido com financeiro (affiliate_amount > 0 p/ provar que a comissão SOBRA).
	// sz_orders.status usa o conjunto INGLÊS (CHECK sz_orders_status_check) — 'em_rota'
	// é status de MOTOBOY (sz_motoboy_pedidos), não de sz_orders. Usamos 'processing'
	// (válido, e não-cancelado/não-frustrado → o gate financeiro do produtor produz
	// taxa/líquido normalmente). O status motoboy 'em_rota' fica em sz_motoboy_pedidos.
	var orderID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO sz_orders
		   (order_number, wp_order_id, user_id, produtor_id, affiliate_id,
		    status, subtotal, shipping, total, affiliate_amount, senderzz_fee, producer_net)
		 VALUES ($1, $2, $3, $4, $5, 'processing', 100, 0, 100, 9.99, 5, 85.01)
		 RETURNING id`,
		orderNum, wpOrderID, produtorID, produtorID, affiliateWP,
	).Scan(&orderID); err != nil {
		t.Fatalf("falha ao inserir sz_orders sintético: %v", err)
	}

	// 2) Endereço billing com a PII do cliente.
	if _, err := pool.Exec(ctx,
		`INSERT INTO sz_order_addresses
		   (order_id, tipo, nome, email, telefone, cep, logradouro, numero, complemento, bairro, cidade, uf)
		 VALUES ($1, 'billing', 'Maria da Silva', 'maria@gmail.com', '11976864006',
		         '01310100', 'Av Paulista', '1000', 'Apto 42 bloco B', 'Bela Vista', 'São Paulo', 'SP')`,
		orderID,
	); err != nil {
		t.Fatalf("falha ao inserir endereço sintético: %v", err)
	}

	// 3) CPF do cliente (vive em sz_order_meta).
	if _, err := pool.Exec(ctx,
		`INSERT INTO sz_order_meta (order_id, meta_key, meta_value) VALUES ($1, '_billing_cpf', '39000000705')`,
		orderID,
	); err != nil {
		t.Fatalf("falha ao inserir meta CPF sintético: %v", err)
	}

	// 4) Pedido motoboy (identifica o pedido como motoboy via EXISTS por wc_order_id).
	// cd_id/zona_id/dest_cep são NOT NULL — valores sintéticos (cd_id/zona_id altos não
	// precisam casar sz_motoboy_cds/zonas: a List não faz join neles).
	if _, err := pool.Exec(ctx,
		`INSERT INTO sz_motoboy_pedidos
		   (wc_order_id, cd_id, zona_id, status, dest_nome, dest_telefone, dest_cep,
		    dest_endereco, dest_complemento, valor_pedido, valor_taxa)
		 VALUES ($1, $2, $3, 'em_rota', 'Maria da Silva', '11976864006', '01310100',
		         'Av Paulista, 1000', 'Apto 42 bloco B', 100, 7)`,
		wpOrderID, piiTestBase+900, piiTestBase+901,
	); err != nil {
		t.Fatalf("falha ao inserir sz_motoboy_pedidos sintético: %v", err)
	}
}

// decodeMotoboyList extrai data[] do envelope { ok, data:[...] } da List.
func decodeMotoboyList(t *testing.T, body []byte) []mbOrderRow {
	t.Helper()
	var env struct {
		Data []mbOrderRow `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("falha ao decodificar envelope da List: %v; body=%s", err, string(body))
	}
	return env.Data
}

// findRowByNumber localiza a linha do pedido sintético pelo wp_order_id (que vira
// Number quando order_number é não-vazio — aqui é "PIITST...", então casa por WCOrderID).
func findRowByWC(rows []mbOrderRow, wcOrderID int64) *mbOrderRow {
	for i := range rows {
		if rows[i].WCOrderID != nil && *rows[i].WCOrderID == wcOrderID {
			return &rows[i]
		}
	}
	return nil
}

// TestMotoboyListAffiliateGetsPIIBlanked: o AFILIADO recebe o pedido SEM a PII do
// cliente (nome/telefone/CPF/email/endereço/complemento vazios) MAS COM a comissão
// (financeiro permitido). Prova viva do fix de minimização (Art. 6º III).
func TestMotoboyListAffiliateGetsPIIBlanked(t *testing.T) {
	pool := requireLGPDDB(t)

	produtorID := piiTestBase + 1
	affiliateWP := piiTestBase + 2
	wpOrderID := piiTestBase + 3
	seedMotoboyPIIOrder(t, pool, produtorID, affiliateWP, wpOrderID)

	h := &MotoboyHandler{Pool: pool}
	rec := httptest.NewRecorder()
	// Afiliado: id qualquer, WPUserID = affiliate_id do pedido (escopo o.affiliate_id = u.WPUserID).
	h.List(rec, getReqAs(piiTestBase+99, affiliateWP, "afiliado"))
	if rec.Code != http.StatusOK {
		t.Fatalf("List(afiliado) = %d, esperado 200; body=%s", rec.Code, rec.Body.String())
	}

	row := findRowByWC(decodeMotoboyList(t, rec.Body.Bytes()), wpOrderID)
	if row == nil {
		t.Fatalf("pedido sintético (wc=%d) não veio na lista do afiliado", wpOrderID)
	}

	// PII apagada.
	if row.ClienteNome != "" || row.ClienteTelefone != "" || row.ClienteCPF != "" ||
		row.ClienteEmail != "" || row.Endereco != "" || row.Complemento != "" {
		t.Errorf("PII do cliente NÃO foi apagada p/ afiliado: nome=%q tel=%q cpf=%q email=%q addr=%q compl=%q",
			row.ClienteNome, row.ClienteTelefone, row.ClienteCPF, row.ClienteEmail, row.Endereco, row.Complemento)
	}
	// Financeiro permitido: comissão segue presente (afiliado vê a SUA comissão).
	if row.ComissaoAfil == nil || *row.ComissaoAfil <= 0 {
		t.Errorf("ComissaoAfil = %v, esperado > 0 (financeiro permitido NÃO deve sumir)", row.ComissaoAfil)
	}
	// Financeiro restrito segue omitido p/ afiliado (gate financeiro já existente).
	if row.TaxaTotal != nil || row.ValorLiquido != nil {
		t.Errorf("financeiro restrito vazou p/ afiliado: TaxaTotal=%v ValorLiquido=%v", row.TaxaTotal, row.ValorLiquido)
	}
	// Permitidos: status e produto presentes.
	if row.Status == "" {
		t.Error("Status vazio p/ afiliado — deveria ver o status do pedido")
	}
}

// TestMotoboyListProdutorSeesPII: o MESMO pedido, lido pelo PRODUTOR dono, vem com a
// PII PREENCHIDA — garante que o fix NÃO quebrou quem precisa operar.
func TestMotoboyListProdutorSeesPII(t *testing.T) {
	pool := requireLGPDDB(t)

	produtorID := piiTestBase + 11
	affiliateWP := piiTestBase + 12
	wpOrderID := piiTestBase + 13
	seedMotoboyPIIOrder(t, pool, produtorID, affiliateWP, wpOrderID)

	h := &MotoboyHandler{Pool: pool}
	rec := httptest.NewRecorder()
	// Produtor: ID = produtor_id do pedido (escopo o.produtor_id = u.ID).
	h.List(rec, getReqAs(produtorID, piiTestBase+98, "produtor"))
	if rec.Code != http.StatusOK {
		t.Fatalf("List(produtor) = %d, esperado 200; body=%s", rec.Code, rec.Body.String())
	}

	row := findRowByWC(decodeMotoboyList(t, rec.Body.Bytes()), wpOrderID)
	if row == nil {
		t.Fatalf("pedido sintético (wc=%d) não veio na lista do produtor", wpOrderID)
	}

	// PII PREENCHIDA p/ o produtor (NÃO mascarada).
	if row.ClienteNome == "" {
		t.Error("ClienteNome vazio p/ produtor — o fix NÃO deve mascarar PII do produtor")
	}
	if row.ClienteTelefone == "" {
		t.Error("ClienteTelefone vazio p/ produtor")
	}
	if row.ClienteCPF == "" {
		t.Error("ClienteCPF vazio p/ produtor")
	}
	if row.Endereco == "" {
		t.Error("Endereco vazio p/ produtor")
	}
	// Produtor vê o financeiro restrito (taxa/líquido).
	if row.TaxaTotal == nil || row.ValorLiquido == nil {
		t.Errorf("financeiro restrito ausente p/ produtor: TaxaTotal=%v ValorLiquido=%v", row.TaxaTotal, row.ValorLiquido)
	}
}
