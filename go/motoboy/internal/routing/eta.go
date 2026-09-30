// Package routing implementa o motor de ROTEAMENTO + ETA do módulo motoboy.
//
// FEAT-ETA: dado o GPS atual do motoboy (sz_motoboys.ultimo_lat/ultimo_lng) e os
// pedidos do dia já geocodados (dest_lat/dest_lng preenchidos a montante pelo
// pacote internal/geo via GeocodePedido), este pacote:
//
//   - calcula distância geográfica (Haversine, em metros);
//   - ordena a rota por vizinho-mais-próximo (nearest-neighbor) a partir da origem;
//   - estima a hora de chegada (ETA) acumulada por pedido.
//
// É um pacote PURO: nenhuma I/O de banco, nenhuma dependência de internal/geo.
// As coordenadas chegam já preenchidas nos Stops. Todas as funções são
// determinísticas e testáveis isoladamente.
package routing

import (
	"math"
	"time"
)

// ── Constantes / defaults ────────────────────────────────────────────────────

const (
	// raioTerraMetros espelha sz_motoboy_distancia_metros() em
	// includes/motoboy/router.php (raio médio da Terra, 6.371 km em metros).
	raioTerraMetros = 6371000.0

	// velocidadeUrbanaPadraoKmh: velocidade média de deslocamento urbano de moto.
	velocidadeUrbanaPadraoKmh = 22.0

	// minPorParadaPadrao: minutos gastos em cada parada (entrega/comprovante).
	minPorParadaPadrao = 6
)

// ── Tipos ──────────────────────────────────────────────────────────────────────

// Stop representa uma parada da rota: um pedido do dia com destino já geocodado.
// Lat/Lng devem ter sido preenchidos a montante (internal/geo.GeocodePedido).
type Stop struct {
	PedidoID int64
	Lat      float64
	Lng      float64
}

// ── Haversine ────────────────────────────────────────────────────────────────

// Haversine retorna a distância em METROS entre dois pontos GPS.
//
// FEAT-ETA: reimplementação direta de sz_motoboy_distancia_metros()
// (includes/motoboy/router.php) — mesmo raio (6371000 m) e mesma fórmula.
func Haversine(lat1, lng1, lat2, lng2 float64) float64 {
	d1 := deg2rad(lat2 - lat1)
	d2 := deg2rad(lng2 - lng1)
	a := math.Pow(math.Sin(d1/2), 2) +
		math.Cos(deg2rad(lat1))*math.Cos(deg2rad(lat2))*math.Pow(math.Sin(d2/2), 2)
	return raioTerraMetros * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func deg2rad(graus float64) float64 {
	return graus * math.Pi / 180
}

// ── Ordenação de rota (nearest-neighbor) ───────────────────────────────────────

// OrdenarRota ordena as paradas pela heurística do vizinho-mais-próximo,
// partindo da origem (GPS atual do motoboy: origemLat/origemLng).
//
// FEAT-ETA: a cada passo escolhe a parada não-visitada mais próxima do ponto
// corrente e avança até ela. NÃO muta o slice recebido — retorna um slice novo.
// Entrada nil/vazia → slice vazio (não-nil).
func OrdenarRota(origemLat, origemLng float64, stops []Stop) []Stop {
	n := len(stops)
	ordenada := make([]Stop, 0, n)
	if n == 0 {
		return ordenada
	}

	visitado := make([]bool, n)
	atualLat, atualLng := origemLat, origemLng

	for passo := 0; passo < n; passo++ {
		melhorIdx := -1
		melhorDist := math.MaxFloat64
		for i := 0; i < n; i++ {
			if visitado[i] {
				continue
			}
			d := Haversine(atualLat, atualLng, stops[i].Lat, stops[i].Lng)
			if d < melhorDist {
				melhorDist = d
				melhorIdx = i
			}
		}
		// melhorIdx é sempre >= 0 aqui (ainda há não-visitados nesta iteração).
		visitado[melhorIdx] = true
		ordenada = append(ordenada, stops[melhorIdx])
		atualLat, atualLng = stops[melhorIdx].Lat, stops[melhorIdx].Lng
	}

	return ordenada
}

// ── Estimativa de ETA ──────────────────────────────────────────────────────────

// EstimarETA calcula a hora de chegada (ETA) acumulada por pedido ao longo de
// uma rota JÁ ORDENADA (use OrdenarRota antes).
//
// FEAT-ETA: para a rota [s0, s1, s2, ...], partindo da origem (GPS do motoboy):
//
//	ETA[sk] = agora
//	          + tempo de viagem(origem → s0 → ... → sk)
//	          + minPorParada * k   (tempo de serviço das k paradas ANTERIORES)
//
// Ou seja, o tempo de serviço de uma parada conta apenas para as paradas
// seguintes — nada é somado pela própria chegada.
//
// Cadeia de unidades (ponto sensível): Haversine dá metros, velocidade é km/h,
// serviço é minutos, saída é time.Time:
//
//	segundos de viagem = dist_m / (velocidadeKmh / 3.6)
//	segundos de serviço = minPorParada * 60
//
// Parâmetros configuráveis com fallback: velocidadeKmh <= 0 usa 22 km/h;
// minPorParada <= 0 usa 6 min (a regra "<=0 → default" também blinda contra
// divisão por zero em velocidadeKmh). Rota vazia → mapa vazio (não-nil).
func EstimarETA(origemLat, origemLng float64, rotaOrdenada []Stop, agora time.Time, velocidadeKmh float64, minPorParada int) map[int64]time.Time {
	if velocidadeKmh <= 0 {
		velocidadeKmh = velocidadeUrbanaPadraoKmh
	}
	if minPorParada <= 0 {
		minPorParada = minPorParadaPadrao
	}

	eta := make(map[int64]time.Time, len(rotaOrdenada))

	metrosPorSegundo := velocidadeKmh / 3.6
	servicoPorParada := time.Duration(minPorParada) * time.Minute

	acumulado := time.Duration(0)
	atualLat, atualLng := origemLat, origemLng

	for k, s := range rotaOrdenada {
		// Tempo de viagem do ponto corrente até esta parada.
		distM := Haversine(atualLat, atualLng, s.Lat, s.Lng)
		segViagem := distM / metrosPorSegundo
		acumulado += time.Duration(segViagem * float64(time.Second))

		// Tempo de serviço das k paradas anteriores (não da atual).
		eta[s.PedidoID] = agora.Add(acumulado + time.Duration(k)*servicoPorParada)

		atualLat, atualLng = s.Lat, s.Lng
	}

	return eta
}
