// Package freight — cotação de frete (correio/transportadora) do checkout nativo.
//
// FEAT-FRETE. Cliente de cotação usado por:
//   - GET  /checkout-api/freight  (lista opções ao cliente — schedule.go irmão)
//   - POST /checkout-api/order    (RE-cotação server-side; CRIT-01: nunca confiar
//     no preço/opção enviados pelo cliente)
//
// Estratégia (fail-OPEN para não quebrar o checkout):
//  1. Tenta o Melhor Envio (POST ${ME_BASE_URL}/me/shipment/calculate, Bearer
//     ME_TOKEN) quando há token REAL configurado.
//  2. Em 401 / erro / timeout / token placeholder / lista vazia → FALLBACK
//     estimado: 2 opções ("Econômico" e "Expresso") com preço = peso×tarifa+base
//     e prazos plausíveis, todas com Estimated=true.
//
// DETERMINISMO (essencial para o CRIT-01): a função é PURA em relação às entradas
// (origemCEP, destCEP, pkg). Chamá-la duas vezes com os mesmos argumentos devolve
// as MESMAS opções (mesmos IDs e preços). Assim o /freight (cotação exibida) e o
// /order (re-cotação) batem o freight_id e o servidor reusa SEU preço, não o do
// cliente. Os IDs do fallback são strings fixas ("est-eco"/"est-exp"); os do ME
// são o id do serviço ME como string ("1","2",…).
//
// Stdlib apenas (net/http, encoding/json, math…). Sem libs novas, sem pgx aqui
// (o handler é quem toca o banco).
//
// Valores monetários: float64 em REAIS no contrato externo (o contrato do
// endpoint pede price:number). O arredondamento é a 2 casas (centavos).
package freight

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Package descreve o volume a cotar. Dimensões em cm, peso em kg.
type Package struct {
	Altura      float64 // cm
	Largura     float64 // cm
	Comprimento float64 // cm
	Peso        float64 // kg
	// ValorDeclarado é o valor da mercadoria (R$) p/ seguro. Usado pelo ME;
	// no fallback não influencia o preço estimado.
	ValorDeclarado float64
}

// ServiceOption é uma opção de frete já no formato do contrato do endpoint.
//
//	id           — string estável (ME: id do serviço; fallback: "est-eco"/"est-exp").
//	company      — transportadora (ex.: "Correios", "Jadlog"; fallback: "Senderzz").
//	service      — nome do serviço (ex.: "PAC", "SEDEX"; fallback: "Econômico"/"Expresso").
//	price        — preço em REAIS (2 casas), número.
//	delivery_days— prazo estimado em dias.
//	estimated    — true quando veio do FALLBACK (ME indisponível/401).
type ServiceOption struct {
	ID           string  `json:"id"`
	Company      string  `json:"company"`
	Service      string  `json:"service"`
	Price        float64 `json:"price"`
	DeliveryDays int     `json:"delivery_days"`
	Estimated    bool    `json:"estimated"`
	// Preferred: transportadora marcada como preferida pelo produtor (senderzz_options
	// tp_preferida_map). Setado em applyCarrierPreferences (freight_prefs.go); só pra
	// exibição (badge "Indicado" + ordenação no topo) — nunca entra no cálculo de preço.
	Preferred bool `json:"-"`
	// Locked: produtor tem frete travado (fixo por transportadora e/ou bloqueio de
	// Correios) — pedido dono 2026-07-27: "sem o cliente poder mexer". Setado em
	// applyFixedFreight/applyCorreiosLock (freight.go, orders-service). O front usa
	// isso pra ESCONDER o seletor de frete inteiro (nem card clicável mostra) — só
	// aplica o preço já travado no total, sem UI de escolha.
	Locked bool `json:"-"`
}

// meAllowedHosts: mesma whitelist SSRF do go/labels (P2-02). O Bearer ME_TOKEN só
// é enviado se ME_BASE_URL for https E host autorizado; caso contrário caímos no
// fallback (nunca exfiltra o token p/ destino arbitrário).
var meAllowedHosts = map[string]struct{}{
	"melhorenvio.com.br":         {},
	"sandbox.melhorenvio.com.br": {},
	"www.melhorenvio.com.br":     {},
}

// httpClient compartilhado com timeout curto — o checkout não pode pendurar
// esperando o ME. Em timeout caímos no fallback.
var httpClient = &http.Client{Timeout: 8 * time.Second}

// Calculate cota o frete origem→destino para o volume informado.
//
// Sempre retorna pelo menos as 2 opções estimadas em caso de qualquer falha do ME
// — nunca devolve erro de cotação (fail-OPEN). Só retorna erro para entradas
// claramente inválidas (CEP sem 8 dígitos), que o handler traduz em 4xx.
func Calculate(ctx context.Context, origemCEP, destCEP string, pkg Package) ([]ServiceOption, error) {
	origem := digits(origemCEP)
	dest := digits(destCEP)
	if len(dest) != 8 {
		return nil, fmt.Errorf("CEP de destino inválido (8 dígitos)")
	}
	if len(origem) != 8 {
		// Origem ausente/ inválida → usa default SP (regra da task). Não é erro.
		origem = "01001000"
	}
	pkg = normalizePackage(pkg)

	// 1) Tenta o Melhor Envio quando há token REAL.
	if opts, ok := tryME(ctx, origem, dest, pkg); ok && len(opts) > 0 {
		return opts, nil
	}

	// 2) Fallback estimado (ME 401/indisponível/placeholder/vazio).
	return fallbackOptions(origem, dest, pkg), nil
}

// ── Melhor Envio ─────────────────────────────────────────────────────────────

// meCalcOption espelha um item do array devolvido por /me/shipment/calculate.
// O ME devolve `company` como OBJETO {id,name} e, quando o serviço não atende a
// rota, preenche `error` (string) — esses itens são descartados.
type meCalcOption struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Price        string `json:"price"`         // ME devolve string ("19.90")
	DeliveryTime int    `json:"delivery_time"` // dias úteis
	Company      struct {
		Name string `json:"name"`
	} `json:"company"`
	Error string `json:"error"`
}

// tryME chama o Melhor Envio. Retorna (opções, true) só em sucesso real (HTTP 200
// + ao menos uma opção sem `error`). Qualquer outra situação ⇒ (nil, false) p/ cair
// no fallback. NUNCA propaga erro — é best-effort.
func tryME(ctx context.Context, origem, dest string, pkg Package) ([]ServiceOption, bool) {
	token := strings.TrimSpace(os.Getenv("ME_TOKEN"))
	// Token vazio ou placeholder de dev → nem tenta (vai direto ao fallback).
	if token == "" || isPlaceholderToken(token) {
		slog.Debug("[senderzz_frete] ME_TOKEN ausente/placeholder — usando fallback estimado")
		return nil, false
	}

	base := strings.TrimRight(strings.TrimSpace(os.Getenv("ME_BASE_URL")), "/")
	if base == "" {
		base = "https://melhorenvio.com.br/api/v2"
	}
	if !meBaseURLOK(base) {
		// SSRF guard (P2-02): host não autorizado → não envia o token, usa fallback.
		slog.Warn("[senderzz_frete] ME_BASE_URL não autorizada — fallback", "base", base)
		return nil, false
	}

	payload := map[string]any{
		"from": map[string]string{"postal_code": origem},
		"to":   map[string]string{"postal_code": dest},
		"products": []map[string]any{{
			"id":              "1",
			"width":           pkg.Largura,
			"height":          pkg.Altura,
			"length":          pkg.Comprimento,
			"weight":          pkg.Peso,
			"insurance_value": round2(pkg.ValorDeclarado),
			"quantity":        1,
		}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		slog.Warn("[senderzz_frete] falha ao montar payload ME — fallback", "err", err)
		return nil, false
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/me/shipment/calculate", bytes.NewReader(body))
	if err != nil {
		slog.Warn("[senderzz_frete] falha ao criar request ME — fallback", "err", err)
		return nil, false
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "senderzz-orders-service/6.0")

	resp, err := httpClient.Do(req)
	if err != nil {
		slog.Warn("[senderzz_frete] ME indisponível — fallback estimado", "err", err)
		return nil, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		slog.Warn("[senderzz_frete] ME retornou status não-200 — fallback estimado",
			"status", resp.StatusCode, "corpo", strings.TrimSpace(string(raw)))
		return nil, false
	}

	var raw []meCalcOption
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		slog.Warn("[senderzz_frete] falha ao decodificar resposta ME — fallback", "err", err)
		return nil, false
	}

	out := make([]ServiceOption, 0, len(raw))
	for _, o := range raw {
		// Serviço sem rota válida vem com `error` preenchido (e geralmente sem preço).
		if strings.TrimSpace(o.Error) != "" {
			continue
		}
		price, perr := strconv.ParseFloat(strings.TrimSpace(o.Price), 64)
		if perr != nil || price <= 0 {
			continue
		}
		company := strings.TrimSpace(o.Company.Name)
		if company == "" {
			company = "Transportadora"
		}
		out = append(out, ServiceOption{
			ID:           strconv.Itoa(o.ID), // id do serviço ME como string (estável)
			Company:      company,
			Service:      strings.TrimSpace(o.Name),
			Price:        round2(price),
			DeliveryDays: o.DeliveryTime,
			Estimated:    false,
		})
	}
	if len(out) == 0 {
		return nil, false
	}
	// LGPD (B5): não logamos o CEP de destino do cliente (dado pessoal). Mantém origem e contagem de opções.
	slog.Info("[senderzz_frete] cotação ME concluída", "origem", origem, "opcoes", len(out))
	return out, true
}

// ── Fallback estimado ────────────────────────────────────────────────────────

// Tarifas do fallback (R$). Modelo simples e transparente: base fixa + peso×R$/kg.
// Calibrado para ficar na ordem de grandeza de PAC/SEDEX de encomenda pequena.
const (
	fbBaseEco  = 15.90 // base Econômico
	fbBaseExp  = 27.90 // base Expresso
	fbPorKgEco = 4.50  // R$/kg Econômico
	fbPorKgExp = 7.50  // R$/kg Expresso
)

// fallbackOptions devolve as 2 opções estimadas, sempre Estimated=true.
// Preço = base + peso_cobrável×tarifa, arredondado a centavos. Determinístico.
func fallbackOptions(origem, dest string, pkg Package) []ServiceOption {
	peso := pesoCobravel(pkg)
	eco := round2(fbBaseEco + peso*fbPorKgEco)
	exp := round2(fbBaseExp + peso*fbPorKgExp)
	// LGPD (B5): não logamos o CEP de destino do cliente (dado pessoal). Mantém origem e peso cobrável.
	slog.Info("[senderzz_frete] cotação ESTIMADA (fallback)", "origem", origem, "peso_cobravel", peso)
	return []ServiceOption{
		{
			ID:           "est-eco",
			Company:      "Senderzz",
			Service:      "Econômico",
			Price:        eco,
			DeliveryDays: 8,
			Estimated:    true,
		},
		{
			ID:           "est-exp",
			Company:      "Senderzz",
			Service:      "Expresso",
			Price:        exp,
			DeliveryDays: 3,
			Estimated:    true,
		},
	}
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// pesoCobravel devolve o maior entre o peso real e o peso cúbico (volumétrico).
// Peso cúbico padrão de encomenda: (C×L×A em cm) / 6000. Mínimo de 0,3 kg para
// não cobrar frete irrisório de itens minúsculos.
func pesoCobravel(pkg Package) float64 {
	cubico := (pkg.Comprimento * pkg.Largura * pkg.Altura) / 6000.0
	peso := math.Max(pkg.Peso, cubico)
	if peso < 0.3 {
		peso = 0.3
	}
	return peso
}

// normalizePackage clampa dimensões/peso para valores plausíveis quando o produto
// não tem dimensões cadastradas (NULL no banco → 0 aqui). Default = encomenda
// pequena (16×11×2 cm, 0,3 kg), suficiente para uma cotação estimada coerente.
func normalizePackage(p Package) Package {
	if p.Comprimento <= 0 {
		p.Comprimento = 16
	}
	if p.Largura <= 0 {
		p.Largura = 11
	}
	if p.Altura <= 0 {
		p.Altura = 2
	}
	if p.Peso <= 0 {
		p.Peso = 0.3
	}
	if p.ValorDeclarado < 0 {
		p.ValorDeclarado = 0
	}
	return p
}

// isPlaceholderToken reconhece os tokens de dev que NÃO devem bater no ME real.
func isPlaceholderToken(t string) bool {
	t = strings.ToLower(strings.TrimSpace(t))
	switch t {
	case "dummy-replace-me", "dummy", "placeholder", "replace-me", "changeme":
		return true
	}
	return strings.Contains(t, "replace") || strings.Contains(t, "dummy")
}

// meBaseURLOK valida https + host na whitelist (P2-02 SSRF guard).
func meBaseURLOK(base string) bool {
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "https" {
		return false
	}
	_, ok := meAllowedHosts[strings.ToLower(u.Hostname())]
	return ok
}

// digits remove tudo que não é dígito (CEP).
func digits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// round2 arredonda a 2 casas (centavos).
func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
