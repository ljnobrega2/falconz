// Package geo resolve coordenadas (lat/lng) de pedidos do motoboy sob demanda.
//
// FEAT-GEO — Geocoding de destino.
//
// Problema: sz_motoboy_pedidos.dest_lat / dest_lng existem no schema mas
// nascem SEMPRE NULL. O pedido só carrega o endereço textual
// (CEP + rua + número + bairro + cidade + UF). Sem coordenadas o roteador
// (Haversine / geofence de zona) não consegue calcular distâncias.
//
// Estratégia: geocode on-demand + cache permanente no próprio registro.
//  1. Lê o pedido. Se dest_lat E dest_lng já estiverem preenchidos → retorna (cache HIT).
//  2. (Opcional) Completa o endereço via ViaCEP se a rua estiver vazia.
//  3. Monta o endereço, chama o Nominatim (OpenStreetMap) e pega o 1º resultado.
//  4. Grava lat/lng de volta em sz_motoboy_pedidos (UPDATE) — cache permanente.
//
// Falha de geocoding NUNCA quebra o caller: retorna (0, 0, err) e o chamador decide.
//
// Dependências: somente stdlib + pgx (driver já usado no go/motoboy).
// NÃO adicionar golang.org/x/time/rate nem outras libs — quebraria o build offline.
package geo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FEAT-GEO — constantes de configuração do provedor de geocoding.
const (
	// nominatimBase é o endpoint público de busca do Nominatim (OpenStreetMap).
	nominatimBase = "https://nominatim.openstreetmap.org/search"

	// nominatimUA é obrigatório pela política de uso do Nominatim.
	// Requisições sem User-Agent identificável são bloqueadas (HTTP 403).
	nominatimUA = "falkz-logistics/1.0"

	// viaCEPBase completa o endereço quando falta a rua (best-effort).
	viaCEPBase = "https://viacep.com.br/ws/"

	// httpTimeout é o timeout total por requisição externa.
	httpTimeout = 5 * time.Second

	// rateInterval respeita a política do Nominatim: no máximo 1 req/s.
	rateInterval = 1 * time.Second
)

// FEAT-GEO — limiter global de 1 req/s para o Nominatim.
//
// GeocodePedido é uma função standalone (sem struct), então o gate de taxa
// precisa viver em estado de pacote. Mutex + timestamp da última requisição
// garantem o espaçamento mínimo mesmo com chamadas concorrentes.
var (
	rateMu        sync.Mutex
	rateLastReq   time.Time
	geoHTTPClient = &http.Client{Timeout: httpTimeout}
)

// waitRate bloqueia até que tenha passado pelo menos rateInterval desde a
// última requisição ao Nominatim. Respeita o cancelamento do ctx.
func waitRate(ctx context.Context) error {
	rateMu.Lock()
	defer rateMu.Unlock()

	if !rateLastReq.IsZero() {
		elapsed := time.Since(rateLastReq)
		if elapsed < rateInterval {
			espera := rateInterval - elapsed
			select {
			case <-time.After(espera):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	rateLastReq = time.Now()
	return nil
}

// pedidoEndereco carrega os campos lidos do pedido para montar a query e
// decidir se o geocoding é necessário.
type pedidoEndereco struct {
	lat      *float64
	lng      *float64
	cep      string
	endereco *string // rua
	numero   *string
	bairro   *string
	cidade   *string
	uf       *string
}

// GeocodePedido resolve lat/lng do destino de um pedido do motoboy.
//
// FEAT-GEO — fluxo on-demand com cache:
//   - Se dest_lat E dest_lng já estiverem preenchidos → retorna direto (sem rede).
//   - Caso contrário, geocodifica via Nominatim e persiste o resultado.
//
// Em qualquer falha retorna (0, 0, err) — o caller decide como degradar.
func GeocodePedido(ctx context.Context, pool *pgxpool.Pool, pedidoID int64) (float64, float64, error) {
	if pool == nil {
		return 0, 0, fmt.Errorf("[geo] pool nil")
	}

	pe, err := carregarEndereco(ctx, pool, pedidoID)
	if err != nil {
		return 0, 0, err
	}

	// Cache HIT: ambos já preenchidos → retorna sem bater na rede.
	if pe.lat != nil && pe.lng != nil {
		return *pe.lat, *pe.lng, nil
	}

	// Best-effort: completa a rua via ViaCEP se estiver vazia.
	// Falha aqui NÃO interrompe o fluxo de geocoding.
	if strBlank(pe.endereco) {
		if rua := buscarRuaViaCEP(ctx, pe.cep); rua != "" {
			pe.endereco = &rua
		}
	}

	endereco := montarEndereco(pe)
	if endereco == "" {
		return 0, 0, fmt.Errorf("[geo] pedido %d sem endereço suficiente para geocoding", pedidoID)
	}

	lat, lng, err := geocodeNominatim(ctx, endereco)
	if err != nil {
		slog.Warn("[geo] falha ao geocodificar pedido", "pedido_id", pedidoID, "endereco", endereco, "err", err)
		return 0, 0, err
	}

	// Cache permanente: grava no próprio pedido.
	if err := salvarCoords(ctx, pool, pedidoID, lat, lng); err != nil {
		// Geocoding funcionou; só o cache falhou. Loga mas retorna as coords.
		slog.Warn("[geo] geocoding ok mas falhou ao gravar cache", "pedido_id", pedidoID, "err", err)
	}

	slog.Info("[geo] pedido geocodificado", "pedido_id", pedidoID, "lat", lat, "lng", lng)
	return lat, lng, nil
}

// carregarEndereco lê os campos de endereço + lat/lng do pedido.
func carregarEndereco(ctx context.Context, pool *pgxpool.Pool, pedidoID int64) (*pedidoEndereco, error) {
	var pe pedidoEndereco
	// dest_lat / dest_lng são DECIMAL(10,7) NULL → scan em *float64 (mesmo padrão de lote.go).
	err := pool.QueryRow(ctx,
		`SELECT
			dest_lat,
			dest_lng,
			dest_cep,
			dest_endereco,
			dest_numero,
			dest_bairro,
			dest_cidade,
			dest_uf
		 FROM sz_motoboy_pedidos
		WHERE id = $1`,
		pedidoID,
	).Scan(
		&pe.lat, &pe.lng,
		&pe.cep, &pe.endereco, &pe.numero,
		&pe.bairro, &pe.cidade, &pe.uf,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("[geo] pedido %d não encontrado", pedidoID)
		}
		return nil, fmt.Errorf("[geo] erro ao ler pedido %d: %w", pedidoID, err)
	}
	return &pe, nil
}

// salvarCoords grava lat/lng no pedido (cache permanente).
func salvarCoords(ctx context.Context, pool *pgxpool.Pool, pedidoID int64, lat, lng float64) error {
	_, err := pool.Exec(ctx,
		`UPDATE sz_motoboy_pedidos
		    SET dest_lat = $1,
		        dest_lng = $2
		  WHERE id = $3`,
		lat, lng, pedidoID,
	)
	if err != nil {
		return fmt.Errorf("[geo] UPDATE coords pedido %d: %w", pedidoID, err)
	}
	return nil
}

// montarEndereco compõe a string de busca a partir dos campos disponíveis.
// Ordem: rua, número - bairro - cidade - UF - CEP. Campos vazios são ignorados.
// Retorna "" se não houver nada além do país para buscar.
func montarEndereco(pe *pedidoEndereco) string {
	var ruaNum []string
	if r := strValue(pe.endereco); r != "" {
		ruaNum = append(ruaNum, r)
	}
	if n := strValue(pe.numero); n != "" {
		ruaNum = append(ruaNum, n)
	}

	var partes []string
	if linha := strings.Join(ruaNum, ", "); linha != "" {
		partes = append(partes, linha)
	}
	if b := strValue(pe.bairro); b != "" {
		partes = append(partes, b)
	}
	if c := strValue(pe.cidade); c != "" {
		partes = append(partes, c)
	}
	if u := strValue(pe.uf); u != "" {
		partes = append(partes, u)
	}
	if cep := formatCEP(pe.cep); cep != "" {
		partes = append(partes, cep)
	}

	return strings.Join(partes, " - ")
}

// nominatimResult espelha um item do array JSON do Nominatim.
//
// ATENÇÃO (FEAT-GEO): o Nominatim devolve lat/lon como STRINGS JSON
// (ex.: {"lat":"-23.5505","lon":"-46.6333"}), não como números.
// Decodificar em float64 causa erro de decode silencioso → fallback permanente.
// Por isso lemos como string e fazemos strconv.ParseFloat.
type nominatimResult struct {
	Lat string `json:"lat"`
	Lon string `json:"lon"`
}

// geocodeNominatim consulta o Nominatim e retorna lat/lng do 1º resultado.
func geocodeNominatim(ctx context.Context, endereco string) (float64, float64, error) {
	// Respeita o limite de 1 req/s antes de qualquer chamada.
	if err := waitRate(ctx); err != nil {
		return 0, 0, fmt.Errorf("[geo] rate limiter cancelado: %w", err)
	}

	q := url.Values{}
	q.Set("format", "json")
	q.Set("q", endereco)
	q.Set("countrycodes", "br")
	q.Set("limit", "1")
	endpoint := nominatimBase + "?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("[geo] erro ao montar request: %w", err)
	}
	// User-Agent obrigatório pela política do Nominatim.
	req.Header.Set("User-Agent", nominatimUA)
	req.Header.Set("Accept", "application/json")

	resp, err := geoHTTPClient.Do(req)
	if err != nil {
		return 0, 0, fmt.Errorf("[geo] erro HTTP Nominatim: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("[geo] Nominatim status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB de teto
	if err != nil {
		return 0, 0, fmt.Errorf("[geo] erro ao ler resposta Nominatim: %w", err)
	}

	var results []nominatimResult
	if err := json.Unmarshal(body, &results); err != nil {
		return 0, 0, fmt.Errorf("[geo] erro ao decodificar JSON Nominatim: %w", err)
	}
	if len(results) == 0 {
		return 0, 0, fmt.Errorf("[geo] Nominatim sem resultados para %q", endereco)
	}

	lat, err := strconv.ParseFloat(results[0].Lat, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("[geo] lat inválida %q: %w", results[0].Lat, err)
	}
	lng, err := strconv.ParseFloat(results[0].Lon, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("[geo] lng inválida %q: %w", results[0].Lon, err)
	}

	return lat, lng, nil
}

// viaCEPResult espelha o JSON do ViaCEP (apenas o campo logradouro nos interessa).
type viaCEPResult struct {
	Logradouro string `json:"logradouro"`
	Erro       bool   `json:"erro"`
}

// buscarRuaViaCEP tenta completar a rua a partir do CEP. Best-effort:
// qualquer falha retorna "" sem propagar erro (não bloqueia o geocoding).
func buscarRuaViaCEP(ctx context.Context, cepRaw string) string {
	cep := digitsOnly(cepRaw)
	if len(cep) != 8 {
		return ""
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, viaCEPBase+cep+"/json/", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", nominatimUA)
	req.Header.Set("Accept", "application/json")

	resp, err := geoHTTPClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return ""
	}

	var vc viaCEPResult
	if err := json.Unmarshal(body, &vc); err != nil {
		return ""
	}
	if vc.Erro {
		return ""
	}
	return strings.TrimSpace(vc.Logradouro)
}

// --- helpers ---

// strValue retorna o valor trimado de um *string, ou "" se nil.
func strValue(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

// strBlank reporta se o *string é nil ou só espaços.
func strBlank(s *string) bool {
	return strValue(s) == ""
}

// digitsOnly mantém apenas dígitos de uma string (sanitiza CEP).
func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// formatCEP devolve o CEP no formato NNNNN-NNN se tiver 8 dígitos, senão "".
func formatCEP(cepRaw string) string {
	cep := digitsOnly(cepRaw)
	if len(cep) != 8 {
		return ""
	}
	return cep[:5] + "-" + cep[5:]
}
