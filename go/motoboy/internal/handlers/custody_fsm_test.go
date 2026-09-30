// FEAT-GEOFENCE companion: testes PUROS da máquina de estados de custódia/COD.
// Provam que o valor não se PERDE (caminho legal alcança cada terminal) nem se
// DUPLICA (terminal não reentra, transição não pula estados).
package handlers

import "testing"

// ── Caminhos felizes (valor resolvido exatamente uma vez) ────────────────────

// TestCaminhoEntrega: reserved → with_motoboy → delivered. O caminho COD normal
// onde o motoboy recebe o valor. Cada passo é legal e o terminal é alcançável.
func TestCaminhoEntrega(t *testing.T) {
	passos := []struct{ de, para CustodyState }{
		{CustodyReserved, CustodyWithMotoboy},
		{CustodyWithMotoboy, CustodyDelivered},
	}
	for _, p := range passos {
		if !PodeTransicionar(p.de, p.para) {
			t.Fatalf("transição legal %s→%s foi bloqueada", p.de, p.para)
		}
	}
	if !EhTerminal(CustodyDelivered) {
		t.Fatal("delivered deveria ser terminal")
	}
}

// TestCaminhoDevolucaoVendavel: with_motoboy → frustrated → return_declared →
// available. O pacote frustrado é declarado pelo motoboy e o OL confirma retorno
// vendável. Valor COD NÃO recebido; produto volta ao estoque.
func TestCaminhoDevolucaoVendavel(t *testing.T) {
	passos := []struct{ de, para CustodyState }{
		{CustodyWithMotoboy, CustodyFrustrated},
		{CustodyFrustrated, CustodyReturnDeclared},
		{CustodyReturnDeclared, CustodyAvailable},
	}
	for _, p := range passos {
		if !PodeTransicionar(p.de, p.para) {
			t.Fatalf("transição legal %s→%s foi bloqueada", p.de, p.para)
		}
	}
	if !EhTerminal(CustodyAvailable) {
		t.Fatal("available deveria ser terminal")
	}
}

// TestCaminhoDevolucaoAvariada: ... → return_declared → damaged (perda).
func TestCaminhoDevolucaoAvariada(t *testing.T) {
	if !PodeTransicionar(CustodyReturnDeclared, CustodyDamaged) {
		t.Fatal("return_declared→damaged deveria ser legal")
	}
	if !EhTerminal(CustodyDamaged) {
		t.Fatal("damaged deveria ser terminal")
	}
}

// TestCancelamento: reserved → cancelled (pedido cancelado antes de sair).
func TestCancelamento(t *testing.T) {
	if !PodeTransicionar(CustodyReserved, CustodyCancelled) {
		t.Fatal("reserved→cancelled deveria ser legal")
	}
	if !EhTerminal(CustodyCancelled) {
		t.Fatal("cancelled deveria ser terminal")
	}
}

// ── Anti-duplicação: terminais não reentram nem têm saída ────────────────────

// TestTerminalSemSaida: nenhum estado terminal pode transicionar para qualquer
// outro estado — incluindo ele mesmo. Garante que o valor já resolvido não é
// re-creditado/re-cobrado por uma reentrada.
func TestTerminalSemSaida(t *testing.T) {
	terminais := []CustodyState{CustodyDelivered, CustodyAvailable, CustodyDamaged, CustodyCancelled}
	todos := []CustodyState{
		CustodyReserved, CustodyWithMotoboy, CustodyDelivered, CustodyFrustrated,
		CustodyReturnDeclared, CustodyAvailable, CustodyDamaged, CustodyCancelled,
	}
	for _, term := range terminais {
		for _, dest := range todos {
			if PodeTransicionar(term, dest) {
				t.Errorf("terminal %s não deveria transicionar para %s (risco de duplicar valor)", term, dest)
			}
		}
	}
}

// TestEntregaNaoReentra: delivered→delivered bloqueado (não re-credita COD).
func TestEntregaNaoReentra(t *testing.T) {
	if PodeTransicionar(CustodyDelivered, CustodyDelivered) {
		t.Fatal("delivered→delivered deveria ser bloqueado (duplicaria o recebimento COD)")
	}
}

// ── Anti-perda: não pode pular estados ───────────────────────────────────────

// TestNaoPulaParaEntrega: reserved→delivered é ilegal (pularia with_motoboy).
// Pular significaria marcar entregue/recebido sem o pacote ter saído em rota.
func TestNaoPulaParaEntrega(t *testing.T) {
	if PodeTransicionar(CustodyReserved, CustodyDelivered) {
		t.Fatal("reserved→delivered deveria ser ilegal (pula with_motoboy)")
	}
}

// TestFrustradoNaoSaltaParaConfirmacao: frustrated→available e frustrated→
// damaged são ilegais — o OL só confirma a condição DEPOIS do motoboy declarar
// devolução (sz_mbc_return_not_declared). Pular perderia o passo return_declared.
func TestFrustradoNaoSaltaParaConfirmacao(t *testing.T) {
	if PodeTransicionar(CustodyFrustrated, CustodyAvailable) {
		t.Fatal("frustrated→available deveria ser ilegal (falta return_declared)")
	}
	if PodeTransicionar(CustodyFrustrated, CustodyDamaged) {
		t.Fatal("frustrated→damaged deveria ser ilegal (falta return_declared)")
	}
}

// TestNaoVoltaDeRotaParaReservado: with_motoboy→reserved é ilegal (não há
// "desfazer rota" na máquina de custódia; pacote já saiu com o motoboy).
func TestNaoVoltaDeRotaParaReservado(t *testing.T) {
	if PodeTransicionar(CustodyWithMotoboy, CustodyReserved) {
		t.Fatal("with_motoboy→reserved deveria ser ilegal")
	}
}

// ── Idempotência segura de return_declared ───────────────────────────────────

// TestReturnDeclaredIdempotente: return_declared→return_declared é permitido
// (espelha o early-return idempotente de sz_mbc_declare_return_by_qr) e marcado
// como Idempotente — não é avanço, não dispara nova movimentação de valor.
func TestReturnDeclaredIdempotente(t *testing.T) {
	if !PodeTransicionar(CustodyReturnDeclared, CustodyReturnDeclared) {
		t.Fatal("return_declared→return_declared deveria ser permitido (idempotente)")
	}
	res := AvaliarTransicaoCustodia(CustodyReturnDeclared, CustodyReturnDeclared)
	if !res.Permitida || !res.Idempotente {
		t.Fatalf("esperava permitida+idempotente, veio %+v", res)
	}
}

// TestIdempotenteNaoTerminalGenerico: estados não-terminais aceitam auto-laço.
func TestIdempotenteNaoTerminalGenerico(t *testing.T) {
	naoTerminais := []CustodyState{CustodyReserved, CustodyWithMotoboy, CustodyFrustrated, CustodyReturnDeclared}
	for _, s := range naoTerminais {
		if !PodeTransicionar(s, s) {
			t.Errorf("auto-laço idempotente em estado não-terminal %s deveria ser permitido", s)
		}
	}
}

// ── Veredito + motivo ────────────────────────────────────────────────────────

// TestAvaliarTransicaoMotivos: bloqueios trazem motivo PT-BR não-vazio e
// Permitida=false; legais trazem Permitida=true sem motivo.
func TestAvaliarTransicaoMotivos(t *testing.T) {
	// Legal.
	if r := AvaliarTransicaoCustodia(CustodyReserved, CustodyWithMotoboy); !r.Permitida || r.Motivo != "" {
		t.Errorf("reserved→with_motoboy deveria ser permitida sem motivo, veio %+v", r)
	}
	// Pula estado.
	if r := AvaliarTransicaoCustodia(CustodyReserved, CustodyDelivered); r.Permitida || r.Motivo == "" {
		t.Errorf("reserved→delivered deveria bloquear com motivo, veio %+v", r)
	}
	// Reentrada de terminal.
	if r := AvaliarTransicaoCustodia(CustodyDelivered, CustodyDelivered); r.Permitida || r.Motivo == "" {
		t.Errorf("delivered→delivered deveria bloquear com motivo, veio %+v", r)
	}
	// Saída de terminal.
	if r := AvaliarTransicaoCustodia(CustodyCancelled, CustodyReserved); r.Permitida || r.Motivo == "" {
		t.Errorf("cancelled→reserved deveria bloquear com motivo, veio %+v", r)
	}
}

// ── Mapa status pedido → physical custody (espelho do PHP) ────────────────────

// TestPedidoStatusParaCustody confere o mapa FIEL ao $map de sz_mbc_*.
func TestPedidoStatusParaCustody(t *testing.T) {
	casos := []struct {
		status string
		want   CustodyState
		ok     bool
	}{
		{"agendado", CustodyReserved, true},
		{"reagendado", CustodyReserved, true},
		{"embalado", CustodyReserved, true},
		{"em_rota", CustodyWithMotoboy, true},
		{"a_caminho", CustodyWithMotoboy, true},
		{"entregue", CustodyDelivered, true},
		{"frustrado", CustodyFrustrated, true},
		{"cancelado", CustodyCancelled, true},
		{"inexistente", "", false},
		{"", "", false},
	}
	for _, c := range casos {
		got, ok := PedidoStatusParaCustody(c.status)
		if ok != c.ok || got != c.want {
			t.Errorf("PedidoStatusParaCustody(%q)=(%q,%v), esperava (%q,%v)",
				c.status, got, ok, c.want, c.ok)
		}
	}
}

// TestTodoTerminalEhAlcancavel: prova que cada estado terminal de COD tem ao
// menos UM antecessor legal — ou seja, o valor sempre TEM um caminho para ser
// resolvido (não fica preso/perdido sem nunca alcançar um terminal).
func TestTodoTerminalEhAlcancavel(t *testing.T) {
	terminais := []CustodyState{CustodyDelivered, CustodyAvailable, CustodyDamaged, CustodyCancelled}
	todos := []CustodyState{
		CustodyReserved, CustodyWithMotoboy, CustodyFrustrated, CustodyReturnDeclared,
	}
	for _, term := range terminais {
		alcancavel := false
		for _, de := range todos {
			if PodeTransicionar(de, term) {
				alcancavel = true
				break
			}
		}
		if !alcancavel {
			t.Errorf("terminal %s não é alcançável por nenhuma transição legal (valor ficaria preso)", term)
		}
	}
}
