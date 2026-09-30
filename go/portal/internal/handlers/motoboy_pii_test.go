// Testes UNITÁRIOS (sem Postgres) dos helpers de minimização de PII p/ AFILIADO
// nos handlers de motoboy do Portal V2.
//
// P1 LGPD — broken access control / Art. 6º III (minimização): o afiliado recebia
// a PII do CLIENTE FINAL (nome + telefone + endereço residencial + CPF/email) nos
// endpoints GET /portal/motoboy e GET /portal/motoboys-dia. Os campos FINANCEIROS
// já eram role-gated; a PII NÃO. blankClientPII / blankMotoboyCardPII /
// blankMotoboyPedidoPII APAGAM essa PII (não mascaram parcial — minimização), e os
// handlers as chamam só quando isAffiliate.
//
// Estes testes rodam SEMPRE (sem DB): o mascaramento ocorre DEPOIS da query, então
// o padrão hermético-com-Pool-nil (gate-antes-do-DB) não o exercitaria. Por isso a
// lógica foi extraída em helpers PUROS, testáveis diretamente com structs preenchidas.
// O caminho via banco (afiliado vs produtor de verdade) está em motoboy_pii_db_test.go.
package handlers

import "testing"

// fullPIIRow — uma linha de pedido motoboy com TODA a PII + financeiro preenchidos.
func fullPIIRow() mbOrderRow {
	comm := 12.34
	taxa := 7.0
	liq := 80.0
	return mbOrderRow{
		ID:               1,
		Number:           "PED-123",
		Status:           "em_rota",
		ClienteNome:      "Maria da Silva",
		ClienteTelefone:  "11976864006",
		ClienteCPF:       "39000000705",
		ClienteEmail:     "maria@gmail.com",
		Endereco:         "Rua das Flores, 123 - Centro, São Paulo/SP",
		Complemento:      "Apto 42, bloco B, próximo à portaria",
		ComplementoLongo: true,
		ProductName:      "Kit Verão",
		ValorBruto:       100.0,
		ValorCartao:      105.0,
		ComissaoAfil:     &comm,
		TaxaTotal:        &taxa,
		ValorLiquido:     &liq,
	}
}

// TestBlankClientPIIApagaPIISensivel: blankClientPII zera os identificadores
// sensíveis, mas preserva nome/endereço necessários para acompanhar a entrega.
func TestBlankClientPIIApagaPIISensivel(t *testing.T) {
	row := fullPIIRow()
	blankClientPII(&row)

	if row.ClienteNome != "Maria da Silva" {
		t.Errorf("ClienteNome = %q, esperado preservado para operação", row.ClienteNome)
	}
	if row.ClienteTelefone != "" {
		t.Errorf("ClienteTelefone = %q, esperado vazio", row.ClienteTelefone)
	}
	if row.ClienteCPF != "" {
		t.Errorf("ClienteCPF = %q, esperado vazio", row.ClienteCPF)
	}
	if row.ClienteEmail != "" {
		t.Errorf("ClienteEmail = %q, esperado vazio", row.ClienteEmail)
	}
	if row.Endereco != "Rua das Flores, 123 - Centro, São Paulo/SP" {
		t.Errorf("Endereco = %q, esperado preservado para operação", row.Endereco)
	}
	if row.Complemento != "Apto 42, bloco B, próximo à portaria" {
		t.Errorf("Complemento = %q, esperado preservado para operação", row.Complemento)
	}
	if !row.ComplementoLongo {
		t.Error("ComplementoLongo = false, esperado true junto do complemento preservado")
	}
}

// TestBlankClientPIIPreservaPermitidos: o que o afiliado PODE ver — nº do pedido,
// produto, status — e TODO o financeiro (bruto, cartão, comissão) ficam INTACTOS.
// O escopo da minimização é a PII, não o financeiro (governado em separado).
func TestBlankClientPIIPreservaPermitidos(t *testing.T) {
	row := fullPIIRow()
	blankClientPII(&row)

	if row.Number != "PED-123" {
		t.Errorf("Number = %q, esperado intacto (afiliado vê o nº do pedido)", row.Number)
	}
	if row.Status != "em_rota" {
		t.Errorf("Status = %q, esperado intacto", row.Status)
	}
	if row.ProductName != "Kit Verão" {
		t.Errorf("ProductName = %q, esperado intacto (afiliado vê o produto)", row.ProductName)
	}
	if row.ValorBruto != 100.0 {
		t.Errorf("ValorBruto = %v, esperado 100 (financeiro NÃO é mexido aqui)", row.ValorBruto)
	}
	if row.ValorCartao != 105.0 {
		t.Errorf("ValorCartao = %v, esperado 105 (financeiro intacto)", row.ValorCartao)
	}
	if row.ComissaoAfil == nil || *row.ComissaoAfil != 12.34 {
		t.Errorf("ComissaoAfil = %v, esperado 12.34 (afiliado vê a SUA comissão)", row.ComissaoAfil)
	}
}

// TestBlankMotoboyCardPIIApagaNomeTelefone: no card de motoboys-dia, o afiliado não
// vê nome/telefone do motoboy; KPIs/ID/total permanecem (front precisa do ID).
func TestBlankMotoboyCardPIIApagaNomeTelefone(t *testing.T) {
	c := mdMotoboyCard{
		ID: 7, Nome: "João Motoboy", Telefone: "11999990000",
		Total: 5, Entregues: 3, Frustrados: 1, EmRota: 1, Pct: 60, TotalValor: 500.0,
	}
	blankMotoboyCardPII(&c)

	if c.Nome != "" {
		t.Errorf("Nome = %q, esperado vazio (afiliado não vê o motoboy)", c.Nome)
	}
	if c.Telefone != "" {
		t.Errorf("Telefone = %q, esperado vazio", c.Telefone)
	}
	// KPIs e identidade do card permanecem.
	if c.ID != 7 || c.Total != 5 || c.Entregues != 3 || c.Pct != 60 || c.TotalValor != 500.0 {
		t.Errorf("KPIs/ID do card foram alterados indevidamente: %+v", c)
	}
}

// TestBlankMotoboyPedidoPIIApagaDestNome: a linha de pedido do card perde só o
// dest_nome (cliente); nº do pedido, status e valor permanecem.
func TestBlankMotoboyPedidoPIIApagaDestNome(t *testing.T) {
	wc := int64(321)
	pr := mdPedidoRow{WCOrderID: &wc, DestNome: "Carlos Cliente", Status: "entregue", ValorPedido: 99.9}
	blankMotoboyPedidoPII(&pr)

	if pr.DestNome != "" {
		t.Errorf("DestNome = %q, esperado vazio (nome do cliente final)", pr.DestNome)
	}
	if pr.WCOrderID == nil || *pr.WCOrderID != 321 {
		t.Errorf("WCOrderID alterado indevidamente: %v", pr.WCOrderID)
	}
	if pr.Status != "entregue" {
		t.Errorf("Status = %q, esperado intacto", pr.Status)
	}
	if pr.ValorPedido != 99.9 {
		t.Errorf("ValorPedido = %v, esperado intacto", pr.ValorPedido)
	}
}
