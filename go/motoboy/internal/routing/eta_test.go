// FEAT-ETA: testes do motor de roteamento + ETA. Pacote puro, sem I/O.
package routing

import (
	"math"
	"testing"
	"time"
)

// São Paulo (aprox): origem ~ Praça da Sé. Pontos espalhados ao redor.
const (
	seLat = -23.5505
	seLng = -46.6333
)

func TestHaversineZero(t *testing.T) {
	if d := Haversine(seLat, seLng, seLat, seLng); d != 0 {
		t.Fatalf("distância de um ponto a ele mesmo deveria ser 0, veio %v", d)
	}
}

func TestHaversineConhecida(t *testing.T) {
	// ~1 grau de latitude ≈ 111.195 km no raio 6371000.
	d := Haversine(0, 0, 1, 0)
	esperado := 111194.9 // m
	if math.Abs(d-esperado) > 50 {
		t.Fatalf("Haversine(0,0,1,0) = %v, esperado ~%v", d, esperado)
	}
}

func TestOrdenarRotaNaoMuta(t *testing.T) {
	stops := []Stop{
		{PedidoID: 1, Lat: -23.60, Lng: -46.60}, // mais longe
		{PedidoID: 2, Lat: -23.551, Lng: -46.634}, // muito perto da origem
		{PedidoID: 3, Lat: -23.58, Lng: -46.62}, // intermediário
	}
	original := append([]Stop(nil), stops...)

	ordenada := OrdenarRota(seLat, seLng, stops)

	// não muta a entrada
	for i := range stops {
		if stops[i] != original[i] {
			t.Fatalf("OrdenarRota mutou o slice de entrada no índice %d", i)
		}
	}
	// nearest-neighbor: o pedido 2 (mais perto da origem) deve vir primeiro
	if ordenada[0].PedidoID != 2 {
		t.Fatalf("esperava pedido 2 primeiro, veio %d", ordenada[0].PedidoID)
	}
	if len(ordenada) != 3 {
		t.Fatalf("esperava 3 paradas, veio %d", len(ordenada))
	}
}

// TestOrdenarRotaSequenciaConhecida prova a SEQUÊNCIA completa de uma rota com
// vetor conhecido — é a propriedade que /motoboy/lote usa para atribuir
// `sequencia` (1..N) a cada parada. Origem na Sé; quatro paradas a distâncias
// crescentes ao norte (todas na mesma longitude) embaralhadas na entrada. O
// nearest-neighbor a partir da origem deve visitá-las da mais próxima à mais
// distante: 4 → 3 → 2 → 1.
func TestOrdenarRotaSequenciaConhecida(t *testing.T) {
	stops := []Stop{
		{PedidoID: 1, Lat: seLat + 0.040, Lng: seLng}, // mais longe (~4.4km)
		{PedidoID: 3, Lat: seLat + 0.020, Lng: seLng}, // ~2.2km
		{PedidoID: 2, Lat: seLat + 0.030, Lng: seLng}, // ~3.3km
		{PedidoID: 4, Lat: seLat + 0.010, Lng: seLng}, // mais perto (~1.1km)
	}
	ordenada := OrdenarRota(seLat, seLng, stops)

	querida := []int64{4, 3, 2, 1}
	if len(ordenada) != len(querida) {
		t.Fatalf("esperava %d paradas, veio %d", len(querida), len(ordenada))
	}
	for i, want := range querida {
		if ordenada[i].PedidoID != want {
			t.Fatalf("sequência[%d]=%d, esperava %d (ordem completa: %v)",
				i+1, ordenada[i].PedidoID, want, idsDe(ordenada))
		}
	}

	// A ETA tem de ser monotonicamente crescente ao longo da rota ordenada —
	// invariante que garante que `sequencia` e `eta` contam a mesma história.
	agora := time.Date(2026, 6, 18, 8, 0, 0, 0, time.UTC)
	eta := EstimarETA(seLat, seLng, ordenada, agora, 0, 0)
	var anterior time.Time
	for i, s := range ordenada {
		t1, ok := eta[s.PedidoID]
		if !ok {
			t.Fatalf("ETA ausente para pedido %d", s.PedidoID)
		}
		if i > 0 && !t1.After(anterior) {
			t.Fatalf("ETA não-crescente na parada %d (pedido %d): %v <= %v",
				i+1, s.PedidoID, t1, anterior)
		}
		anterior = t1
	}
}

func idsDe(stops []Stop) []int64 {
	ids := make([]int64, len(stops))
	for i, s := range stops {
		ids[i] = s.PedidoID
	}
	return ids
}

func TestOrdenarRotaVazia(t *testing.T) {
	r := OrdenarRota(seLat, seLng, nil)
	if r == nil {
		t.Fatal("OrdenarRota(nil) deve retornar slice não-nil")
	}
	if len(r) != 0 {
		t.Fatalf("esperava slice vazio, veio len=%d", len(r))
	}
}

func TestEstimarETAUnidades(t *testing.T) {
	// Parada a exatamente ~1000 m da origem a 22 km/h:
	// 1000 / (22/3.6) = 163.636... s. Sem serviço anterior (k=0).
	agora := time.Date(2026, 6, 18, 8, 0, 0, 0, time.UTC)

	// Acha um ponto ~1000m ao norte usando inversão aproximada de latitude.
	deltaLat := 1000.0 / 111194.9 // graus ≈ 1000 m
	stops := []Stop{{PedidoID: 42, Lat: seLat + deltaLat, Lng: seLng}}

	eta := EstimarETA(seLat, seLng, stops, agora, 22, 6)
	got, ok := eta[42]
	if !ok {
		t.Fatal("ETA do pedido 42 ausente")
	}
	delta := got.Sub(agora).Seconds()
	if math.Abs(delta-163.6) > 3 { // tolerância p/ aproximação de distância
		t.Fatalf("ETA primeira parada = +%.1fs, esperado ~163.6s", delta)
	}
}

func TestEstimarETAAcumuladaComServico(t *testing.T) {
	agora := time.Date(2026, 6, 18, 8, 0, 0, 0, time.UTC)
	stops := []Stop{
		{PedidoID: 1, Lat: seLat + 0.009, Lng: seLng}, // ~1km
		{PedidoID: 2, Lat: seLat + 0.018, Lng: seLng}, // +~1km
	}
	eta := EstimarETA(seLat, seLng, stops, agora, 22, 6)

	// 2ª parada deve estar depois da 1ª, e a diferença inclui 6 min de serviço
	// da parada anterior + tempo de viagem do trecho.
	dif := eta[2].Sub(eta[1])
	if dif <= 6*time.Minute {
		t.Fatalf("2ª parada deve incluir serviço (6min) + viagem; dif=%v", dif)
	}
}

func TestEstimarETADefaultsEEvitaDivZero(t *testing.T) {
	agora := time.Now()
	stops := []Stop{{PedidoID: 9, Lat: seLat + 0.01, Lng: seLng}}
	// velocidade 0 e minPorParada 0 → devem cair para defaults (22 / 6),
	// sem Inf/NaN na ETA.
	eta := EstimarETA(seLat, seLng, stops, agora, 0, 0)
	got := eta[9]
	if got.Before(agora) || math.IsInf(float64(got.Sub(agora)), 0) {
		t.Fatalf("ETA inválida com defaults: %v", got)
	}
}

func TestEstimarETAVazia(t *testing.T) {
	m := EstimarETA(seLat, seLng, nil, time.Now(), 22, 6)
	if m == nil {
		t.Fatal("EstimarETA(rota vazia) deve retornar mapa não-nil")
	}
	if len(m) != 0 {
		t.Fatalf("esperava mapa vazio, veio len=%d", len(m))
	}
}
