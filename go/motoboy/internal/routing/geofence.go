// FEAT-GEOFENCE — gate puro de cerca geográfica para atribuição de pedido.
//
// Contexto: o roteador motoboy (CEP→CD→zona→motoboy) seleciona o courier por
// zona/CD/carga (sz_motoboy_selecionar_motoboy, includes/motoboy/router.php).
// A distância geográfica (Haversine) já é calculada em vários pontos — ETA
// (eta.go) e o cron sz_motoboy_geofence_check — mas NÃO existia um gate puro,
// determinístico e testável que decidisse "este ponto está DENTRO do raio da
// zona/CD?". Sem esse gate, a regra de cerca fica espalhada em SQL/cron e não
// pode ser coberta por teste unitário.
//
// Este arquivo adiciona esse gate. É PURO (sem I/O, sem banco): recebe as
// coordenadas e o raio (já lidos a montante pelo caller) e responde se a
// atribuição respeita a cerca. NÃO muda esquema, NÃO altera a seleção atual —
// é a peça testável que o caller pode plugar para ENDURECER a atribuição.
//
// Reutiliza Haversine (eta.go) — mesma fórmula/raio de sz_motoboy_distancia_metros().
package routing

// PontoGeo é um par lat/lng. Usado para expressar destino e centro da cerca.
type PontoGeo struct {
	Lat float64
	Lng float64
}

// DentroDoRaio reporta se o ponto destino está dentro (ou na borda) de uma
// cerca circular de raio raioMetros centrada em centro.
//
// FEAT-GEOFENCE: regra de inclusão = distância <= raio (borda conta como
// dentro, igual ao cron PHP que usa `$dist <= 500`).
//
// Semântica de raio não-positivo: raioMetros <= 0 significa "cerca desativada"
// → retorna true (não bloqueia). Isso espelha o comportamento defensivo do
// resto do módulo, onde valores ausentes/zerados degradam para "permitido" em
// vez de derrubar a operação. O caller que QUER cerca obrigatória deve validar
// raioMetros > 0 antes de chamar.
func DentroDoRaio(centro, destino PontoGeo, raioMetros float64) bool {
	if raioMetros <= 0 {
		return true // cerca desativada / não configurada → não bloqueia
	}
	dist := Haversine(centro.Lat, centro.Lng, destino.Lat, destino.Lng)
	return dist <= raioMetros
}

// GeofenceResult resume o veredito da cerca para a atribuição de um pedido.
type GeofenceResult struct {
	Permitido bool    // destino dentro do raio (ou cerca desativada)
	DistM     float64 // distância calculada centro→destino, em metros
	RaioM     float64 // raio efetivo aplicado (eco do input)
}

// AvaliarGeofence calcula a distância centro→destino e devolve o veredito
// completo (permitido + distância + raio aplicado). É o gate que o roteador
// deve consultar ANTES de confirmar a atribuição de um pedido a um motoboy/zona
// quando a zona/CD tem raio configurado.
//
// FEAT-GEOFENCE: PURO e determinístico — toda a I/O (ler centro do CD/zona, ler
// raio da option, ler dest_lat/dest_lng do pedido) fica no caller. Aqui só a
// decisão geográfica, que é o que precisa de cobertura de teste.
//
// Coordenadas zeradas no destino (dest_lat/dest_lng nascem NULL → 0 quando não
// geocodados) com cerca ativa resultam em distância enorme e Permitido=false:
// fail-closed proposital — pedido sem geocoding não "passa" a cerca por engano.
func AvaliarGeofence(centro, destino PontoGeo, raioMetros float64) GeofenceResult {
	if raioMetros <= 0 {
		// Cerca desativada: permite sem calcular distância (DistM informativo).
		return GeofenceResult{
			Permitido: true,
			DistM:     Haversine(centro.Lat, centro.Lng, destino.Lat, destino.Lng),
			RaioM:     raioMetros,
		}
	}
	dist := Haversine(centro.Lat, centro.Lng, destino.Lat, destino.Lng)
	return GeofenceResult{
		Permitido: dist <= raioMetros,
		DistM:     dist,
		RaioM:     raioMetros,
	}
}
