// FEAT-GEOFENCE: testes do gate de cerca geográfica. Pacote puro, sem I/O.
package routing

import (
	"math"
	"testing"
)

// ── Vetores Haversine conhecidos (distâncias verificáveis) ──────────────────

// TestHaversineVetoresConhecidos confere a Haversine contra distâncias de
// referência calculadas com o mesmo raio (6371000 m). Estende a cobertura de
// eta_test.go com vetores adicionais (graus de longitude no equador e um par
// real São Paulo↔Rio).
func TestHaversineVetoresConhecidos(t *testing.T) {
	casos := []struct {
		nome                   string
		lat1, lng1, lat2, lng2 float64
		esperadoM              float64
		tolM                   float64
	}{
		{
			// 1 grau de longitude NO EQUADOR ≈ 1 grau de latitude ≈ 111.195 km.
			nome: "1grau_long_equador",
			lat1: 0, lng1: 0, lat2: 0, lng2: 1,
			esperadoM: 111194.9, tolM: 50,
		},
		{
			// 1 grau de longitude EM lat -23.55 (São Paulo) ≈ 111.195*cos(23.55°)
			// ≈ 101.93 km. cos(23.55°) ≈ 0.91685.
			nome: "1grau_long_sp",
			lat1: -23.55, lng1: -46.6, lat2: -23.55, lng2: -45.6,
			esperadoM: 101934, tolM: 300,
		},
		{
			// São Paulo (Sé) ↔ Rio de Janeiro (centro): ~360 km em linha reta.
			nome: "sp_rio",
			lat1: -23.5505, lng1: -46.6333, lat2: -22.9068, lng2: -43.1729,
			esperadoM: 360500, tolM: 5000,
		},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			d := Haversine(c.lat1, c.lng1, c.lat2, c.lng2)
			if math.Abs(d-c.esperadoM) > c.tolM {
				t.Fatalf("Haversine(%v,%v,%v,%v)=%.1fm, esperado ~%.1fm (tol %.0fm)",
					c.lat1, c.lng1, c.lat2, c.lng2, d, c.esperadoM, c.tolM)
			}
		})
	}
}

// TestHaversineSimetrica confere que a distância A→B é igual a B→A.
func TestHaversineSimetrica(t *testing.T) {
	ab := Haversine(-23.55, -46.63, -23.60, -46.70)
	ba := Haversine(-23.60, -46.70, -23.55, -46.63)
	if math.Abs(ab-ba) > 1e-6 {
		t.Fatalf("Haversine não simétrica: A→B=%.6f B→A=%.6f", ab, ba)
	}
}

// ── Gate dentro/fora do raio ────────────────────────────────────────────────

// TestDentroDoRaioDentroEFora prova o gate com um par cuja distância é
// conhecida (~1000 m ao norte) contra raios que o incluem e o excluem.
func TestDentroDoRaioDentroEFora(t *testing.T) {
	centro := PontoGeo{Lat: -23.5505, Lng: -46.6333}
	// ~1000 m ao norte (1 grau de latitude ≈ 111194.9 m).
	deltaLat := 1000.0 / 111194.9
	destino := PontoGeo{Lat: centro.Lat + deltaLat, Lng: centro.Lng}

	// Sanidade: distância real ≈ 1000 m.
	if d := Haversine(centro.Lat, centro.Lng, destino.Lat, destino.Lng); math.Abs(d-1000) > 5 {
		t.Fatalf("setup inválido: distância=%.2fm, esperava ~1000m", d)
	}

	if !DentroDoRaio(centro, destino, 1500) {
		t.Error("ponto a ~1000m deveria estar DENTRO de raio de 1500m")
	}
	if DentroDoRaio(centro, destino, 500) {
		t.Error("ponto a ~1000m deveria estar FORA de raio de 500m")
	}
}

// TestDentroDoRaioBordaInclusiva: distância exatamente == raio conta como
// dentro (regra `dist <= raio`, igual ao cron PHP `$dist <= 500`).
func TestDentroDoRaioBordaInclusiva(t *testing.T) {
	centro := PontoGeo{Lat: -23.5505, Lng: -46.6333}
	deltaLat := 1000.0 / 111194.9
	destino := PontoGeo{Lat: centro.Lat + deltaLat, Lng: centro.Lng}
	distReal := Haversine(centro.Lat, centro.Lng, destino.Lat, destino.Lng)

	// Raio == distância real → borda → deve ser permitido (inclusivo).
	if !DentroDoRaio(centro, destino, distReal) {
		t.Errorf("borda (raio==dist=%.4fm) deveria ser DENTRO (inclusivo)", distReal)
	}
	// Raio logo abaixo da distância → fora.
	if DentroDoRaio(centro, destino, distReal-1) {
		t.Errorf("raio %.4fm < dist %.4fm deveria ser FORA", distReal-1, distReal)
	}
}

// TestDentroDoRaioMesmoPonto: destino == centro está sempre dentro de qualquer
// raio positivo (distância 0).
func TestDentroDoRaioMesmoPonto(t *testing.T) {
	p := PontoGeo{Lat: -23.5505, Lng: -46.6333}
	if !DentroDoRaio(p, p, 1) {
		t.Error("mesmo ponto (dist 0) deveria estar dentro de qualquer raio > 0")
	}
}

// TestDentroDoRaioDesativado: raio <= 0 = cerca desativada → sempre permite,
// mesmo para um destino a centenas de km.
func TestDentroDoRaioDesativado(t *testing.T) {
	centro := PontoGeo{Lat: -23.5505, Lng: -46.6333}
	destino := PontoGeo{Lat: -22.9068, Lng: -43.1729} // Rio, ~360 km
	if !DentroDoRaio(centro, destino, 0) {
		t.Error("raio 0 (desativado) deveria permitir qualquer destino")
	}
	if !DentroDoRaio(centro, destino, -100) {
		t.Error("raio negativo (desativado) deveria permitir qualquer destino")
	}
}

// ── AvaliarGeofence (veredito completo) ─────────────────────────────────────

// TestAvaliarGeofenceVeredito confere os campos do resultado em ambos os lados
// da cerca: Permitido, DistM e RaioM coerentes.
func TestAvaliarGeofenceVeredito(t *testing.T) {
	centro := PontoGeo{Lat: -23.5505, Lng: -46.6333}
	deltaLat := 1000.0 / 111194.9
	destino := PontoGeo{Lat: centro.Lat + deltaLat, Lng: centro.Lng}

	// Dentro: raio 1500m.
	r := AvaliarGeofence(centro, destino, 1500)
	if !r.Permitido {
		t.Errorf("esperava Permitido com raio 1500m, dist=%.2fm", r.DistM)
	}
	if r.RaioM != 1500 {
		t.Errorf("RaioM=%.1f, esperava 1500", r.RaioM)
	}
	if math.Abs(r.DistM-1000) > 5 {
		t.Errorf("DistM=%.2f, esperava ~1000", r.DistM)
	}

	// Fora: raio 500m.
	r2 := AvaliarGeofence(centro, destino, 500)
	if r2.Permitido {
		t.Errorf("esperava bloqueio com raio 500m, dist=%.2fm", r2.DistM)
	}
}

// TestAvaliarGeofenceFailClosedSemGeocoding: destino com lat/lng zerados (não
// geocodado) e cerca ativa → distância enorme → bloqueia (fail-closed).
func TestAvaliarGeofenceFailClosedSemGeocoding(t *testing.T) {
	centro := PontoGeo{Lat: -23.5505, Lng: -46.6333}
	destino := PontoGeo{Lat: 0, Lng: 0} // não geocodado
	r := AvaliarGeofence(centro, destino, 5000)
	if r.Permitido {
		t.Errorf("destino sem geocoding (0,0) com cerca ativa deveria bloquear; dist=%.0fm", r.DistM)
	}
	if r.DistM <= 5000 {
		t.Errorf("distância (0,0)→SP deveria ser >> 5km, veio %.0fm", r.DistM)
	}
}
