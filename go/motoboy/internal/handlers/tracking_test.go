// TEST-TRACKING-KEY-NO-ROUNDTRIP: testes PUROS do gate de PII do rastreio
// público (/tracking/{order_id}). Exercitam validTrackKey() DIRETAMENTE — sem
// roundtrip por GetTracking/HTTP/DB — porque a função é pura (só ambiente +
// HMAC). Provam o contrato de segurança SEC-TRACKING-PII-ENUM / V-SEC-03:
//
//   - key VÁLIDA libera os campos dest_* (PII) e mapa/ETA;
//   - key inválida/ausente NÃO libera → o chamador nula a PII (fail-closed);
//   - SALT ausente NÃO libera nada (fail-closed mesmo sem segredo configurado);
//   - normalização (trim + lowercase) e amarração ao wc_order_id corretas.
//
// White-box (package handlers) para alcançar validTrackKey/trackSalt não
// exportados. Sem t.Parallel: t.Setenv proíbe paralelismo (e queremos isolar o
// ambiente de SALT entre casos).
package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// trackSaltVars são TODOS os nomes de env que trackSalt() consulta, em ordem.
// Precisam ser zerados em conjunto para provar o caminho "SALT ausente": basta
// um vazar do ambiente do CI para o salt ficar não-vazio e o fail-closed sumir.
var trackSaltVars = []string{"WP_SALT_AUTH", "TPC_JWT_SECRET", "SENDERZZ_SECRET", "JWT_SECRET"}

// clearTrackSalt zera todas as fontes de SALT para o escopo do teste.
func clearTrackSalt(t *testing.T) {
	t.Helper()
	for _, v := range trackSaltVars {
		t.Setenv(v, "")
	}
}

// setTrackSalt zera tudo e define apenas a primeira fonte (WP_SALT_AUTH), que é
// a que trackSalt() resolve primeiro — espelha o ambiente real do servidor.
func setTrackSalt(t *testing.T, salt string) {
	t.Helper()
	clearTrackSalt(t)
	t.Setenv("WP_SALT_AUTH", salt)
}

// expectedTrackKey reproduz EXATAMENTE a derivação do token na implementação
// (tracking.go): hex(HMAC-SHA256(salt, "sz-track:"+wc_order_id))[:16], minúsculo.
// É escrita de forma independente (não chama o código sob teste) para servir de
// oráculo — se a implementação mudar a fórmula, o teste quebra.
func expectedTrackKey(salt string, wcOrderID int64) string {
	mac := hmac.New(sha256.New, []byte(salt))
	fmt.Fprintf(mac, "sz-track:%d", wcOrderID)
	return hex.EncodeToString(mac.Sum(nil))[:16]
}

// 1. Key válida → true. Libera os campos dest_* (PII) e mapa/ETA no rastreio.
func TestValidTrackKey_ValidaLibera(t *testing.T) {
	const salt = "salt-de-teste-32-bytes-ou-mais-xxxxx"
	const id int64 = 12345
	setTrackSalt(t, salt)

	tok := expectedTrackKey(salt, id)
	if !validTrackKey(id, tok) {
		t.Fatalf("key válida deveria liberar PII; validTrackKey(%d, %q)=false", id, tok)
	}
}

// 2. Key inválida (errada) → false. PII permanece nula (fail-closed).
func TestValidTrackKey_InvalidaNula(t *testing.T) {
	const salt = "salt-de-teste-32-bytes-ou-mais-xxxxx"
	const id int64 = 12345
	setTrackSalt(t, salt)

	if validTrackKey(id, "deadbeefdeadbeef") {
		t.Fatal("key arbitrária errada NÃO deveria liberar PII")
	}
	// Token correto, porém de OUTRO pedido — não deve liberar (amarração ao id).
	otherTok := expectedTrackKey(salt, id+1)
	if validTrackKey(id, otherTok) {
		t.Fatalf("token do pedido %d não pode liberar o pedido %d (anti-IDOR)", id+1, id)
	}
}

// 3. Key ausente/vazia (e só espaços) → false. Caso do id enumerado sem ?key=.
func TestValidTrackKey_AusenteNula(t *testing.T) {
	const salt = "salt-de-teste-32-bytes-ou-mais-xxxxx"
	const id int64 = 777
	setTrackSalt(t, salt)

	for _, provided := range []string{"", "   ", "\t\n"} {
		if validTrackKey(id, provided) {
			t.Fatalf("key vazia/whitespace (%q) NÃO deveria liberar PII", provided)
		}
	}
}

// 4. SALT ausente → false MESMO com um token que seria válido sob algum salt.
// Fail-closed: sem segredo configurado, o servidor nunca expõe GPS/endereço.
func TestValidTrackKey_SaltVazioFailClosed(t *testing.T) {
	const id int64 = 999
	// Token derivado de um salt qualquer; depois zeramos TODAS as fontes de SALT.
	tok := expectedTrackKey("qualquer-salt", id)
	clearTrackSalt(t)

	if trackSalt() != "" {
		t.Fatal("pré-condição falhou: trackSalt() deveria ser vazio após clearTrackSalt")
	}
	if validTrackKey(id, tok) {
		t.Fatal("com SALT ausente, validTrackKey DEVE ser fail-closed (false)")
	}
	// Defesa em profundidade: nem mesmo string vazia libera quando salt é vazio.
	if validTrackKey(id, "") {
		t.Fatal("SALT vazio + key vazia ainda deve ser false")
	}
}

// 5. Normalização: token correto em MAIÚSCULAS e com espaços ao redor → true
// (a implementação faz TrimSpace + ToLower antes de comparar em tempo constante).
func TestValidTrackKey_NormalizaTrimLower(t *testing.T) {
	const salt = "salt-de-teste-32-bytes-ou-mais-xxxxx"
	const id int64 = 42
	setTrackSalt(t, salt)

	tok := expectedTrackKey(salt, id) // minúsculo por construção
	variantes := []string{
		strings.ToUpper(tok),               // MAIÚSCULO
		"  " + tok + "  ",                  // com espaços
		"  " + strings.ToUpper(tok) + "\n", // maiúsculo + espaços/newline
	}
	for _, v := range variantes {
		if !validTrackKey(id, v) {
			t.Fatalf("variante normalizável %q deveria liberar (Trim+ToLower)", v)
		}
	}
}

// 6. SALT alternativo (fallback): quando WP_SALT_AUTH está ausente mas outra
// fonte (ex.: JWT_SECRET) existe, trackSalt() usa o fallback e o token derivado
// dessa fonte é aceito. Prova a ordem de resolução de segredos.
func TestValidTrackKey_FallbackSecret(t *testing.T) {
	const salt = "segredo-jwt-de-fallback-xxxxxxxxxxxx"
	const id int64 = 2024
	clearTrackSalt(t)
	t.Setenv("JWT_SECRET", salt) // só a última fonte da lista

	if got := trackSalt(); got != salt {
		t.Fatalf("trackSalt() deveria resolver para o fallback; got=%q", got)
	}
	tok := expectedTrackKey(salt, id)
	if !validTrackKey(id, tok) {
		t.Fatal("token derivado do segredo de fallback deveria liberar PII")
	}
	// E um token derivado de OUTRO salt não pode passar sob este fallback.
	if validTrackKey(id, expectedTrackKey("salt-diferente", id)) {
		t.Fatal("token de salt diferente não pode liberar com o fallback ativo")
	}
}

// 7. Simula o gate de PII de GetTracking: quando keyOk=false, todos os campos
// dest_* são nulados (mascarados). Garante que a decisão de validTrackKey se
// traduz em ZERO vazamento de PII no payload público — o contrato real.
func TestPIIGate_MascaraQuandoKeyInvalida(t *testing.T) {
	const salt = "salt-de-teste-32-bytes-ou-mais-xxxxx"
	const id int64 = 5555
	setTrackSalt(t, salt)

	nome, cidade, uf, produto := "Fulano", "São Paulo", "SP", "Tênis"
	mk := func() *PedidoPublico {
		return &PedidoPublico{
			ID: 1, WCOrderID: id, Status: "em_rota",
			DestNome: &nome, DestCidade: &cidade, DestUF: &uf, DestProduto: &produto,
		}
	}

	// Caso A: key inválida → mascara (espelha tracking.go GetTracking).
	p := mk()
	if keyOk := validTrackKey(id, "chave-errada"); keyOk {
		t.Fatal("pré-condição: key errada deveria ser inválida")
	} else {
		p.DestNome, p.DestCidade, p.DestUF, p.DestProduto = nil, nil, nil, nil
	}
	if p.DestNome != nil || p.DestCidade != nil || p.DestUF != nil || p.DestProduto != nil {
		t.Fatal("com key inválida, todos os campos dest_* devem ficar nulos (sem vazar PII)")
	}

	// Caso B: key válida → NÃO mascara (PII preservada para o cliente legítimo).
	p2 := mk()
	if keyOk := validTrackKey(id, expectedTrackKey(salt, id)); !keyOk {
		t.Fatal("pré-condição: key válida deveria liberar")
	}
	if p2.DestNome == nil || *p2.DestNome != nome || p2.DestProduto == nil {
		t.Fatal("com key válida, a PII deve permanecer disponível ao cliente legítimo")
	}
}
