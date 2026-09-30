package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/motoboy-service/internal/geo"
	"github.com/senderzz/motoboy-service/internal/httpx"
	"github.com/senderzz/motoboy-service/internal/routing"
)

// TrackingHandler agrupa dependências para os handlers de rastreio público.
type TrackingHandler struct {
	Pool *pgxpool.Pool
}

// PedidoPublico é o corpo do rastreio público. Campos de mapa/ETA
// (ultimo_lat/lng, dest_lat/lng, eta) só são preenchidos quando status='em_rota'
// — fora desse estado saem omitidos (omitempty) e o frontend esconde o mapa.
type PedidoPublico struct {
	ID             int64   `json:"id"`
	WCOrderID      int64   `json:"wc_order_id"`
	Status         string  `json:"status"`
	DestNome       *string `json:"dest_nome"`
	DestCidade     *string `json:"dest_cidade"`
	DestUF         *string `json:"dest_uf"`
	DestProduto    *string `json:"dest_produto"`
	ReagendadoPara *string `json:"reagendado_para"`
	TsAprovado     *string `json:"ts_aprovado"`
	TsEmbalado     *string `json:"ts_embalado"`
	TsEmRota       *string `json:"ts_em_rota"`
	TsACaminho     *string `json:"ts_a_caminho"`
	TsEntregue     *string `json:"ts_entregue"`
	TsFrustrado    *string `json:"ts_frustrado"`
	CreatedAt      string  `json:"created_at"`

	// FEAT-ETA-WIRE: enriquecimento de mapa/ETA (somente em_rota).
	MotoboyID *int64   `json:"-"` // interno, nunca exposto ao público
	UltimoLat *float64 `json:"ultimo_lat,omitempty"`
	UltimoLng *float64 `json:"ultimo_lng,omitempty"`
	DestLat   *float64 `json:"dest_lat,omitempty"`
	DestLng   *float64 `json:"dest_lng,omitempty"`
	ETA       *string  `json:"eta,omitempty"`
}

// GetTracking responde GET /tracking/{order_id} — rastreio público sem auth.
//
// Retorna status atual + timestamps de cada etapa do pedido. Quando o pedido
// está 'em_rota', enriquece com a posição GPS do motoboy, as coordenadas do
// destino (geocode on-demand) e a PREVISÃO DE ENTREGA (ETA) calculada pelo motor
// de roteamento sobre todas as entregas em rota daquele motoboy. // FEAT-ETA-WIRE
func (h *TrackingHandler) GetTracking(w http.ResponseWriter, r *http.Request) {
	orderID, err := strconv.ParseInt(chi.URLParam(r, "order_id"), 10, 64)
	if err != nil || orderID <= 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "order_id inválido")
		return
	}

	var p PedidoPublico
	// NOTA DE MIGRAÇÃO: colunas ts_* e created_at são DATETIME sem timezone no MySQL
	// (wall-clock de Brasília). Após pgloader tornam-se TIMESTAMP WITHOUT TIME ZONE.
	// NÃO usar AT TIME ZONE — formatar diretamente como horário SP.
	// reagendado_para é DATE — usar TO_CHAR para string para evitar scan em time.Time.
	err = h.Pool.QueryRow(r.Context(),
		`SELECT
			id,
			wc_order_id,
			status,
			motoboy_id,
			dest_nome,
			dest_cidade,
			dest_uf,
			dest_produto,
			TO_CHAR(reagendado_para, 'YYYY-MM-DD'),
			TO_CHAR(ts_aprovado,  'YYYY-MM-DD"T"HH24:MI:SS"-03:00"'),
			TO_CHAR(ts_embalado,  'YYYY-MM-DD"T"HH24:MI:SS"-03:00"'),
			TO_CHAR(ts_em_rota,   'YYYY-MM-DD"T"HH24:MI:SS"-03:00"'),
			TO_CHAR(ts_a_caminho, 'YYYY-MM-DD"T"HH24:MI:SS"-03:00"'),
			TO_CHAR(ts_entregue,  'YYYY-MM-DD"T"HH24:MI:SS"-03:00"'),
			TO_CHAR(ts_frustrado, 'YYYY-MM-DD"T"HH24:MI:SS"-03:00"'),
			TO_CHAR(created_at,   'YYYY-MM-DD"T"HH24:MI:SS"-03:00"')
		 FROM sz_motoboy_pedidos
		WHERE wc_order_id = $1
		LIMIT 1`,
		orderID,
	).Scan(
		&p.ID, &p.WCOrderID, &p.Status, &p.MotoboyID,
		&p.DestNome, &p.DestCidade, &p.DestUF, &p.DestProduto,
		&p.ReagendadoPara,
		&p.TsAprovado, &p.TsEmbalado, &p.TsEmRota,
		&p.TsACaminho, &p.TsEntregue, &p.TsFrustrado,
		&p.CreatedAt,
	)
	if err != nil {
		// pgx retorna pgx.ErrNoRows — qualquer erro de scan = não encontrado.
		slog.Warn("[tracking] pedido não encontrado", "wc_order_id", orderID, "err", err)
		httpx.WriteErr(w, http.StatusNotFound, "pedido não encontrado")
		return
	}

	// SEGURANÇA (V-SEC-03 / anti-IDOR, auditoria 2026-06-18 SEC-TRACKING-PII-ENUM):
	// /tracking/{order_id} é público e enumerável por wc_order_id sequencial. SÓ a
	// timeline (status + timestamps das etapas) é pública; TODO dado pessoal do
	// destinatário (nome, cidade/UF, produto comprado) e os campos de mapa (GPS do
	// motoboy + endereço geocodado + ETA) exigem ?key= válido. Sem token válido,
	// nulamos a PII — fail-closed. Token = HMAC(WP_SALT_AUTH, "sz-track:"+wc_order_id).
	keyOk := validTrackKey(orderID, r.URL.Query().Get("key"))
	if !keyOk {
		// Mascara PII: o cliente legítimo recebe o link com ?key=; um id enumerado
		// só enxerga o andamento (status/etapas), nunca quem/onde/o quê.
		p.DestNome = nil
		p.DestCidade = nil
		p.DestUF = nil
		p.DestProduto = nil
	}

	// FEAT-ETA-WIRE: mapa + ETA só quando em rota, há motoboy atribuído e a key é válida.
	// Degrada bem: qualquer falha (sem GPS, geocode off, Nominatim fora) apenas
	// deixa os campos vazios — o frontend esconde o mapa/ETA e mantém a timeline.
	if keyOk && p.Status == "em_rota" && p.MotoboyID != nil && *p.MotoboyID > 0 {
		h.enriquecerEmRota(r.Context(), &p)
	}

	// Chave de resposta "tracking" — contrato consumido por templates/motoboy/tracking.php.
	httpx.WriteOK(w, map[string]any{"tracking": p})
}

// enriquecerEmRota preenche posição do motoboy, destino e ETA do pedido `p`.
//
// FEAT-ETA-WIRE: previsão = motor de roteamento (nearest-neighbor + ETA acumulada)
// sobre TODAS as entregas em rota do mesmo motoboy — "de acordo com a quantidade
// de entregas atribuídas àquele motoboy e o roteamento feito via sistema".
func (h *TrackingHandler) enriquecerEmRota(ctx context.Context, p *PedidoPublico) {
	// 1. GPS atual do motoboy (sz_motoboys.ultimo_lat/ultimo_lng).
	var mLat, mLng *float64
	if err := h.Pool.QueryRow(ctx,
		`SELECT ultimo_lat, ultimo_lng FROM sz_motoboys WHERE id = $1`, *p.MotoboyID,
	).Scan(&mLat, &mLng); err != nil || mLat == nil || mLng == nil {
		slog.Warn("[tracking] motoboy sem GPS — mapa indisponível",
			"motoboy_id", *p.MotoboyID, "err", err)
		return // sem GPS → frontend degrada (esconde mapa)
	}
	p.UltimoLat, p.UltimoLng = mLat, mLng

	// AUDIT PERF-geocode-serialization: orçamento único de tempo para TODO o
	// geocoding do enriquecimento (destino + paradas da rota). O Nominatim é
	// serializado por um mutex global de 1 req/s (política do provedor); em cache
	// frio isso bloquearia o caminho público /tracking. Limitamos a ~2,5s — o que
	// já estiver em cache/geocodado entra; o resto degrada de forma graciosa
	// (timeline sempre retorna rápido, o frontend esconde o mapa/ETA).
	geoCtx, cancelGeo := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer cancelGeo()

	// 2. Destino deste pedido (geocode on-demand, cacheado em dest_lat/dest_lng).
	if dLat, dLng, gerr := geo.GeocodePedido(geoCtx, h.Pool, p.ID); gerr == nil {
		p.DestLat, p.DestLng = &dLat, &dLng
	}

	// 3. IDs de todas as entregas em rota deste motoboy. Coleta os ids ANTES de
	//    geocodificar para não segurar a conexão do pool durante chamadas de rede.
	rows, err := h.Pool.Query(ctx,
		`SELECT id FROM sz_motoboy_pedidos WHERE motoboy_id = $1 AND status = 'em_rota'`,
		*p.MotoboyID,
	)
	if err != nil {
		slog.Warn("[tracking] falha ao listar entregas em rota", "motoboy_id", *p.MotoboyID, "err", err)
		return
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()

	// 4. Geocodifica cada parada sob o mesmo orçamento geoCtx (~2,5s) criado acima.
	// Em regime permanente (cache quente via salvarCoords) o loop é só leitura de
	// banco e não estoura o orçamento. Cache distribuído (Redis) / batch / ETA
	// assíncrona via WebSocket ficam para o roadmap — exigiriam infra/libs novas
	// (geocode.go proíbe libs extras). // AUDIT PERF-geocode-serialization
	stops := make([]routing.Stop, 0, len(ids))
	for _, id := range ids {
		// Orçamento esgotado → para de tentar geocodar paradas em cache frio.
		if geoCtx.Err() != nil {
			break
		}
		lat, lng, gerr := geo.GeocodePedido(geoCtx, h.Pool, id)
		if gerr != nil {
			continue // parada sem coordenada não entra na rota
		}
		stops = append(stops, routing.Stop{PedidoID: id, Lat: lat, Lng: lng})
	}
	if len(stops) == 0 {
		return
	}

	// 5. Ordena rota (nearest-neighbor) e estima ETA acumulada por parada.
	loc := saoPauloLoc()
	agora := time.Now().In(loc)
	ordenada := routing.OrdenarRota(*mLat, *mLng, stops)
	etaMap := routing.EstimarETA(*mLat, *mLng, ordenada, agora, 0, 0)
	if t, ok := etaMap[p.ID]; ok {
		hhmm := t.In(loc).Format("15:04")
		p.ETA = &hhmm
	}
}

// saoPauloLoc retorna o fuso de São Paulo; fallback fixo -03:00 se o tzdata
// não estiver presente (containers minimalistas sem zoneinfo).
func saoPauloLoc() *time.Location {
	if loc, err := time.LoadLocation("America/Sao_Paulo"); err == nil {
		return loc
	}
	return time.FixedZone("-03", -3*3600)
}

// validTrackKey valida o token de rastreio que libera os campos sensíveis
// (GPS do motoboy + coordenadas do destino) no rastreio público.
//
// SEGURANÇA: espelha a convenção de go/orders/tracking_sign.go —
//
//	token = hex( HMAC-SHA256("sz-track:"+wc_order_id, SALT) )[:16]
//
// É fail-closed: token vazio, SALT não configurado ou divergência → false (não
// libera GPS/endereço). O link de rastreio precisa carregar ?key=<token> gerado
// com o mesmo SALT do servidor. Comparação em tempo constante (hmac.Equal).
func validTrackKey(wcOrderID int64, provided string) bool {
	provided = strings.ToLower(strings.TrimSpace(provided))
	if provided == "" {
		return false
	}
	salt := trackSalt()
	if salt == "" {
		// Sem segredo configurado NÃO liberamos dados sensíveis (fail-closed).
		slog.Warn("[tracking] SALT de rastreio ausente — campos sensíveis bloqueados")
		return false
	}
	mac := hmac.New(sha256.New, []byte(salt))
	fmt.Fprintf(mac, "sz-track:%d", wcOrderID)
	expected := hex.EncodeToString(mac.Sum(nil))[:16]
	return hmac.Equal([]byte(provided), []byte(expected))
}

// trackSalt resolve o segredo de assinatura do rastreio a partir do ambiente.
// Mesma família de segredos usada pelos demais serviços (WP_SALT_AUTH etc.).
func trackSalt() string {
	for _, n := range []string{"WP_SALT_AUTH", "TPC_JWT_SECRET", "SENDERZZ_SECRET", "JWT_SECRET"} {
		if v := strings.TrimSpace(os.Getenv(n)); v != "" {
			return v
		}
	}
	return ""
}

// Reagendar responde POST /tracking/{order_id}/reagendar — V-SEC-03.
//
// IMPORTANTE: validação da order key WooCommerce (campo `key` do body) requer
// consulta à tabela wp_wc_orders ou wp_postmeta que NÃO está disponível no Postgres
// durante Fase 1 da migração. Esta proteção IDOR é crítica — não remover o stub
// até a chave ser migrada para o schema Go.
//
// TODO(V-SEC-03): implementar após migrar wc_order_key para sz_motoboy_pedidos
// ou disponibilizar mirror da tabela wp_wc_orders em Postgres.
func (h *TrackingHandler) Reagendar(w http.ResponseWriter, r *http.Request) {
	_, err := strconv.ParseInt(chi.URLParam(r, "order_id"), 10, 64)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "order_id inválido")
		return
	}

	// Stub fail-closed: retorna 501 até validação de order key estar disponível.
	// NÃO retornar 200 aqui — seria IDOR (protegido por V-SEC-03 no PHP).
	httpx.WriteErr(w, http.StatusNotImplemented,
		"reagendamento ainda não disponível neste endpoint — use o endpoint PHP temporariamente")
}
