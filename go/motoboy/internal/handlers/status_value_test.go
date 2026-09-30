// Testes PUROS de integridade de status/valor: o mapa motoboy→sz_orders
// (bridge), a allowlist de status do OL, a taxa de cartão e os helpers de CEP/
// data da zona. White-box (package handlers). Sem I/O de banco — só as funções
// puras; as time-dependent (calcularDatasDisponiveis, CEPCache TTL) são testadas
// por INVARIANTES, sem fixar o relógio (refactor não permitido).
package handlers

import (
	"math"
	"testing"
	"time"
)

// ── motoboyStatusToSzOrder (bridge — evita divergência rastreio×pedido) ──────

func TestMotoboyStatusToSzOrder(t *testing.T) {
	casos := []struct {
		in   string
		want string
		ok   bool
	}{
		{"entregue", "completo", true}, // COD entregue → Woo "completo"
		{"frustrado", "frustrado", true},
		{"cancelado", "cancelled", true},
		{"em_rota", "enviado", true},   // em-rota não é CHECK-válido → 'enviado'
		{"a_caminho", "enviado", true}, // sub-etapa de em_rota → mesmo 'enviado' (Woo não regride)
		{"embalado", "embalado", true},
		{"agendado", "", false}, // sem equivalente relevante → bridge pulado
		{"pendente", "", false},
		{"", "", false},
	}
	for _, c := range casos {
		got, ok := motoboyStatusToSzOrder(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("motoboyStatusToSzOrder(%q)=(%q,%v), esperava (%q,%v)",
				c.in, got, ok, c.want, c.ok)
		}
	}
}

// TestMotoboyStatusToSzOrderCheckValidos: todo destino retornado precisa estar
// entre os valores CHECK-válidos de sz_orders.status (senão o UPDATE do bridge
// estoura o constraint e a transação inteira do OL faz rollback).
func TestMotoboyStatusToSzOrderCheckValidos(t *testing.T) {
	checkValidos := map[string]bool{
		"pending": true, "processing": true, "aguardando": true, "on-hold": true,
		"em_separacao": true, "embalado": true, "enviado": true, "entregue": true,
		"completo": true, "cancelled": true, "frustrado": true, "reembolsado": true,
	}
	for _, in := range []string{"entregue", "frustrado", "cancelado", "em_rota", "a_caminho", "embalado"} {
		sz, ok := motoboyStatusToSzOrder(in)
		if !ok {
			t.Fatalf("%q deveria mapear", in)
		}
		if !checkValidos[sz] {
			t.Errorf("motoboyStatusToSzOrder(%q)=%q NÃO é CHECK-válido em sz_orders", in, sz)
		}
	}
}

// ── statusWhitelist (OL só pode mover para status conhecidos) ────────────────

func TestStatusWhitelist(t *testing.T) {
	permitidos := []string{"agendado", "embalado", "em_rota", "a_caminho", "entregue", "frustrado", "cancelado"}
	for _, s := range permitidos {
		if !statusWhitelist[s] {
			t.Errorf("status %q deveria estar na whitelist do OL", s)
		}
	}
	negados := []string{"", "deletado", "qualquer", "DELETE", "completo", "enviado"}
	for _, s := range negados {
		if statusWhitelist[s] {
			t.Errorf("status %q NÃO deveria estar na whitelist (OL bloqueia)", s)
		}
	}
}

// TestWhitelistMapeiaPraBridge: todo status que o OL aceita e que tem semântica
// de bridge deve mapear via motoboyStatusToSzOrder; 'agendado' é o caso
// explícito que NÃO mapeia (bridge pulado de propósito) — garante coerência
// entre as duas tabelas de status.
func TestWhitelistMapeiaPraBridge(t *testing.T) {
	if _, ok := motoboyStatusToSzOrder("agendado"); ok {
		t.Error("'agendado' deveria ser pulado pelo bridge (sem equivalente)")
	}
	for _, s := range []string{"embalado", "em_rota", "a_caminho", "entregue", "frustrado", "cancelado"} {
		if !statusWhitelist[s] {
			t.Fatalf("pré-condição: %q deveria estar na whitelist", s)
		}
		if _, ok := motoboyStatusToSzOrder(s); !ok {
			t.Errorf("status %q (whitelist) deveria mapear no bridge", s)
		}
	}
}

// ── valorCartao (taxa de cartão sobre o COD) ─────────────────────────────────

func TestValorCartao(t *testing.T) {
	eps := 1e-9
	casos := []struct {
		valor, pct, want float64
	}{
		{100, 0, 100},  // pct 0 → próprio valor
		{100, 10, 110}, // +10%
		{50, 5, 52.5},  // +5%
		{0, 20, 0},     // valor 0 → 0
		{200, 2.99, 205.98},
	}
	for _, c := range casos {
		got := valorCartao(c.valor, c.pct)
		if math.Abs(got-c.want) > eps {
			t.Errorf("valorCartao(%.2f,%.2f)=%.4f, esperava %.4f", c.valor, c.pct, got, c.want)
		}
	}
}

// ── sanitizeCEP (zona.go) ────────────────────────────────────────────────────

func TestSanitizeCEP(t *testing.T) {
	casos := []struct{ in, want string }{
		{"01310-100", "01310100"},
		{"01310100", "01310100"},
		{" 0131 0100 ", "01310100"},
		{"abc", ""},
		{"", ""},
	}
	for _, c := range casos {
		if got := sanitizeCEP(c.in); got != c.want {
			t.Errorf("sanitizeCEP(%q)=%q, esperava %q", c.in, got, c.want)
		}
	}
}

// ── calcularDatasDisponiveis (time-dependent → testa INVARIANTES) ────────────

// TestCalcularDatasDisponiveisInvariantes: sem fixar o relógio, prova as
// propriedades estáveis do contrato — no máx 5 datas, todas FUTURAS, dentro de
// 14 dias, formato YYYY-MM-DD, e só em dias-de-funcionamento ativos.
func TestCalcularDatasDisponiveisInvariantes(t *testing.T) {
	// Dias ativos: seg(1), qua(3), sex(5).
	diasFunc := "1,3,5"
	datas := calcularDatasDisponiveis(diasFunc, nil)

	if len(datas) > 5 {
		t.Fatalf("no máx 5 datas, veio %d", len(datas))
	}
	hoje := time.Now().In(brLocation).Truncate(24 * time.Hour)
	ativos := map[time.Weekday]bool{time.Monday: true, time.Wednesday: true, time.Friday: true}
	for _, d := range datas {
		parsed, err := time.ParseInLocation("2006-01-02", d, brLocation)
		if err != nil {
			t.Fatalf("data %q não está no formato YYYY-MM-DD: %v", d, err)
		}
		if !parsed.After(hoje) {
			t.Errorf("data %q não é futura", d)
		}
		if parsed.Sub(hoje) > 15*24*time.Hour {
			t.Errorf("data %q está além de ~14 dias", d)
		}
		if !ativos[parsed.Weekday()] {
			t.Errorf("data %q cai em dia inativo (%s)", d, parsed.Weekday())
		}
	}
}

// TestCalcularDatasDisponiveisFallbackSegSab: dias_funcionamento vazio → fallback
// seg-sáb (1..6), nunca domingo.
func TestCalcularDatasDisponiveisFallbackSegSab(t *testing.T) {
	datas := calcularDatasDisponiveis("", nil)
	for _, d := range datas {
		parsed, err := time.ParseInLocation("2006-01-02", d, brLocation)
		if err != nil {
			t.Fatalf("data %q inválida: %v", d, err)
		}
		if parsed.Weekday() == time.Sunday {
			t.Errorf("fallback seg-sáb não deveria incluir domingo: %q", d)
		}
	}
}

// ── CEPCache (hit/miss; TTL não-injetável → não testa expiração) ─────────────

func TestCEPCacheHitMiss(t *testing.T) {
	c := NewCEPCache()

	// Miss: chave nunca setada.
	if _, ok := c.Get("00000000"); ok {
		t.Error("Get de chave ausente deveria ser miss")
	}

	// Hit: set → get devolve o MESMO valor.
	val := map[string]any{"zona_id": int64(7), "cd_nome": "CD Centro"}
	c.Set("01310100", val)
	got, ok := c.Get("01310100")
	if !ok {
		t.Fatal("Get após Set deveria ser hit")
	}
	if got["zona_id"] != int64(7) || got["cd_nome"] != "CD Centro" {
		t.Errorf("valor cacheado divergente: %+v", got)
	}

	// Chave diferente continua miss.
	if _, ok := c.Get("99999999"); ok {
		t.Error("Get de outra chave deveria ser miss")
	}
}

// TestSanitizeCEPMantemDoisCepsDistintos: garante que o cache não confunde CEPs
// (chaves distintas → entradas distintas). Sanidade de chaveamento.
func TestCEPCacheChavesDistintas(t *testing.T) {
	c := NewCEPCache()
	c.Set("01000000", map[string]any{"k": "a"})
	c.Set("02000000", map[string]any{"k": "b"})
	a, _ := c.Get("01000000")
	b, _ := c.Get("02000000")
	if a["k"] != "a" || b["k"] != "b" {
		t.Errorf("cache confundiu chaves: a=%v b=%v", a, b)
	}
}
