// Package handlers — rotas de execução (iniciar-rota, entregar, frustrar).
// Auth: X-MB-Token (token do motoboy).
package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/motoboy-service/internal/auth"
	"github.com/senderzz/motoboy-service/internal/httpx"
)

// RotaHandler implementa os endpoints de execução de rota.
type RotaHandler struct {
	Pool *pgxpool.Pool
}

// IniciarRota — POST /motoboy/iniciar-rota
// Body: {qr_code|package_code, pedido_id?}
//
// Port FIEL de sz_mb_api_iniciar_rota() + sz_mbc_start_route_by_qr():
//   - QR obrigatório (sem QR → 422 qr_required).
//   - assinatura HMAC regenerada por packageCode() e comparada por EqualFold
//     (uppercase, seed "pedido|wc", 14 hex MAIÚSCULOS).
//   - pedido só vai a rota quando 'embalado'.
//   - bloqueia se o pacote está atribuído a OUTRO motoboy.
//   - bloqueia se o motoboy tem pacote frustrado aguardando devolução/OL (custódia).
//   - confere pedido_id esperado quando enviado pelo PWA.
func (h *RotaHandler) IniciarRota(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	mb := auth.MotoboyfromCtx(ctx)
	if mb == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "Sessão do motoboy expirada. Faça login novamente.")
		return
	}

	var req struct {
		QRCode      string `json:"qr_code"`
		PackageCode string `json:"package_code"`
		PedidoID    int64  `json:"pedido_id"`
		// FEAT-SKU: código de barras do produto bipado pelo motoboy ao colocar
		// em rota. Opcional p/ compat (quem ainda não bipa não quebra). Quando
		// presente, o SKU lido tem que bater com algum item do pedido.
		SKUBipado   string `json:"sku_bipado"`
		ManualTyped bool   `json:"manual_typed"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	pkgCode := strings.TrimSpace(req.PackageCode)
	if pkgCode == "" {
		pkgCode = strings.TrimSpace(req.QRCode)
	}
	if pkgCode == "" {
		// Espelha o 422 qr_required do WP: rota só inicia bipando o QR da etiqueta.
		httpx.WriteJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"ok":          false,
			"erro":        "Para iniciar rota, leia o QR Code da etiqueta do pacote.",
			"em_rota":     0,
			"qr_required": true,
		})
		return
	}

	salt := wpSaltAuth()
	if salt == "" {
		slog.Error("[rota] WP_SALT_AUTH não configurado")
		httpx.WriteErr(w, http.StatusServiceUnavailable, "configuração ausente")
		return
	}

	parsed, ok := parsePackageCode(pkgCode)
	if !ok || parsed.PedidoID == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "QR Code da etiqueta inválido.")
		return
	}
	pedidoID := parsed.PedidoID
	wcOrderID := parsed.WCOrderID

	// Confere pedido_id esperado (PWA abre um pedido e bipa o QR dele).
	if req.PedidoID > 0 && pedidoID != req.PedidoID {
		httpx.WriteErr(w, http.StatusConflict, "QR Code não corresponde ao pedido aberto.")
		return
	}

	var status string
	var donoID *int64
	err := h.Pool.QueryRow(ctx,
		`SELECT status, motoboy_id, wc_order_id FROM sz_motoboy_pedidos WHERE id=$1`, pedidoID,
	).Scan(&status, &donoID, &wcOrderID)
	if err != nil {
		httpx.WriteErr(w, http.StatusNotFound, "Pedido Motoboy não encontrado.")
		return
	}

	// Regenera a assinatura esperada e compara (EqualFold = case-insensitive).
	expected := packageCode(pedidoID, wcOrderID, salt)
	if !strings.EqualFold(expected, parsed.Code) {
		slog.Warn("[rota] QR com HMAC inválido", "pedido_id", pedidoID)
		httpx.WriteErr(w, http.StatusConflict, "QR Code não confere com o pacote.")
		return
	}

	if status != "embalado" {
		httpx.WriteErr(w, http.StatusConflict, "Pedido só pode ir para rota quando estiver embalado.")
		return
	}
	// Bloqueia pacote atribuído a OUTRO motoboy (sz_mbc_start_route_by_qr).
	if donoID != nil && *donoID != 0 && *donoID != mb.ID {
		httpx.WriteErr(w, http.StatusConflict, "Este pacote está atribuído a outro motoboy.")
		return
	}
	// Custódia: motoboy com pacote frustrado aguardando devolução/OL não inicia nova rota.
	if custodyTablesExist(ctx, h.Pool) && custodyPendingReturnExists(ctx, h.Pool, mb.ID, pedidoID) {
		httpx.WriteErr(w, http.StatusConflict,
			"Este motoboy possui pacote frustrado aguardando devolução/confirmação do OL. Regularize antes de iniciar nova rota.")
		return
	}

	// FEAT-SKU: se o motoboy bipou o código de barras do produto, valida o SKU
	// contra os itens do pedido ANTES de mudar status. SKU diferente do pedido
	// → 422 e NÃO avança (regra do dono: "se for diferente nao deixa seguir").
	// Compat: sku_bipado vazio = não bipou → mantém o comportamento atual (não
	// valida, não registra scan).
	if strings.TrimSpace(req.SKUBipado) != "" {
		actor := fmt.Sprintf("motoboy:%d", mb.ID)
		match, err := validarSKUPedido(ctx, h.Pool, wcOrderID, req.SKUBipado)
		if err != nil {
			slog.Error("[FEAT-SKU] falha ao validar SKU (em_rota)", "pedido_id", pedidoID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao validar SKU do produto")
			return
		}
		// Fail-closed quando o pedido não tem SKU cadastrado: sem referência não
		// dá pra garantir que é o produto certo → bloqueia.
		ok := match.OK && match.HasExpected
		registrarPackScan(ctx, h.Pool, "em_rota", wcOrderID, match.ProductID,
			req.SKUBipado, match.ExpectedCSV, ok, req.ManualTyped, actor)
		if !ok {
			slog.Warn("[FEAT-SKU] SKU não confere (em_rota)", "pedido_id", pedidoID,
				"sku_bipado", req.SKUBipado, "esperado", match.ExpectedCSV, "tem_sku", match.HasExpected)
			httpx.WriteErr(w, http.StatusUnprocessableEntity, "SKU não confere com o pedido.")
			return
		}
	}

	// BRIDGE: motoboy + sz_orders na MESMA transação (em_rota → sz_orders.status='enviado').
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		slog.Error("[rota] falha ao iniciar transação", "pedido_id", pedidoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao iniciar rota")
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Assume o motoboy logado como responsável (sz_mbc_start_route_by_qr passa
	// motoboy_id na transição). ts_em_rota só na 1ª vez (COALESCE preserva valor).
	if _, err = tx.Exec(ctx, `
		UPDATE sz_motoboy_pedidos
		SET status='em_rota', motoboy_id=$2, ts_em_rota=COALESCE(ts_em_rota, NOW())
		WHERE id=$1`, pedidoID, mb.ID,
	); err != nil {
		slog.Error("[rota] falha ao iniciar rota", "pedido_id", pedidoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao iniciar rota")
		return
	}

	szRows, err := bridgeUpdatePedidoStatus(ctx, tx, pedidoID, "em_rota")
	if err != nil {
		slog.Error("[rota] falha no bridge sz_orders", "pedido_id", pedidoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao iniciar rota")
		return
	}

	if err = tx.Commit(ctx); err != nil {
		slog.Error("[rota] falha ao commitar rota", "pedido_id", pedidoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao iniciar rota")
		return
	}
	logBridge("rota.IniciarRota", pedidoID, "em_rota", "enviado", szRows)

	_, _ = h.Pool.Exec(ctx, `
		INSERT INTO sz_motoboy_audit (pedido_id, acao, de_status, para_status, created_at)
		VALUES ($1, 'rota_iniciada_qr', $2, 'em_rota', NOW())`,
		pedidoID, status,
	)

	slog.Info("[rota] rota iniciada via QR", "pedido_id", pedidoID, "wc_order_id", wcOrderID, "motoboy_id", mb.ID)
	// Resposta espelha o WP: {ok, em_rota:1, pedido_id}.
	httpx.WriteOK(w, map[string]any{"ok": true, "em_rota": 1, "pedido_id": pedidoID, "status": "em_rota"})
}

// ACaminho — POST /motoboy/a-caminho
// Body: {pedido_id}
//
// O motoboy marca que SAIU para ESTE pedido específico (próxima parada da rota).
// Sub-etapa operacional de em_rota: em_rota → a_caminho. No WP isto acontece
// automaticamente no /motoboy/ping quando o GPS chega a <=2km do destino
// (sz_mb_api_ping → sz_motoboy_mudar_status(...,'a_caminho')); este endpoint é a
// versão EXPLÍCITA acionada pelo botão "A caminho" do PWA — não depende do GPS
// estar a 2km, então o motoboy pode sinalizar a próxima parada manualmente.
//
// Regras (idempotente, valida estado anterior):
//   - pedido pertence ao motoboy autenticado (anti-IDOR, SEC-GO-03);
//   - já 'a_caminho' → idempotente (200, sem novo write);
//   - só promove a partir de 'em_rota'; qualquer outro estado → 409;
//   - ts_a_caminho gravado uma única vez (COALESCE preserva o 1º valor — espelha
//     o "timestamp de evento é histórico" de sz_motoboy_mudar_status);
//   - BRIDGE sz_orders na MESMA transação (a_caminho → 'enviado', idempotente:
//     se já estava 'enviado' por em_rota, não há novo write — Woo nunca regride).
func (h *RotaHandler) ACaminho(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PedidoID int64 `json:"pedido_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	if req.PedidoID == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "pedido_id obrigatório")
		return
	}

	ctx := r.Context()

	mb := auth.MotoboyfromCtx(ctx)
	if mb == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autorizado")
		return
	}

	var status string
	var donoID *int64
	err := h.Pool.QueryRow(ctx,
		`SELECT status, motoboy_id FROM sz_motoboy_pedidos WHERE id=$1`, req.PedidoID,
	).Scan(&status, &donoID)
	if err != nil {
		httpx.WriteErr(w, http.StatusNotFound, "Pedido não encontrado para este motoboy.")
		return
	}
	if donoID == nil || *donoID != mb.ID {
		httpx.WriteErr(w, http.StatusForbidden, "pedido não atribuído a este motoboy")
		return
	}
	// Idempotência: já saiu para esta parada → ok, sem novo write.
	if status == "a_caminho" {
		httpx.WriteOK(w, map[string]any{"ok": true, "status": "a_caminho", "idempotent": true})
		return
	}
	// Só promove de em_rota (valida estado anterior — não pula etapas).
	if status != "em_rota" {
		httpx.WriteErr(w, http.StatusConflict, "Pedido precisa estar em rota para marcar 'a caminho'.")
		return
	}

	// BRIDGE: motoboy + sz_orders na MESMA transação.
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		slog.Error("[rota] falha ao iniciar transação a-caminho", "pedido_id", req.PedidoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao marcar a caminho")
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// ts_a_caminho só na 1ª vez (COALESCE preserva valor histórico).
	if _, err = tx.Exec(ctx, `
		UPDATE sz_motoboy_pedidos
		SET status='a_caminho', ts_a_caminho=COALESCE(ts_a_caminho, NOW())
		WHERE id=$1`, req.PedidoID,
	); err != nil {
		slog.Error("[rota] falha ao marcar a caminho", "pedido_id", req.PedidoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao marcar a caminho")
		return
	}

	szRows, err := bridgeUpdatePedidoStatus(ctx, tx, req.PedidoID, "a_caminho")
	if err != nil {
		slog.Error("[rota] falha no bridge sz_orders (a-caminho)", "pedido_id", req.PedidoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao marcar a caminho")
		return
	}

	if err = tx.Commit(ctx); err != nil {
		slog.Error("[rota] falha ao commitar a-caminho", "pedido_id", req.PedidoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao marcar a caminho")
		return
	}
	logBridge("rota.ACaminho", req.PedidoID, "a_caminho", "enviado", szRows)

	_, _ = h.Pool.Exec(ctx, `
		INSERT INTO sz_motoboy_audit (pedido_id, acao, de_status, para_status, created_at)
		VALUES ($1, 'a_caminho', $2, 'a_caminho', NOW())`,
		req.PedidoID, status,
	)

	slog.Info("[rota] pedido a caminho", "pedido_id", req.PedidoID, "motoboy_id", mb.ID)
	httpx.WriteOK(w, map[string]any{"ok": true, "status": "a_caminho", "pedido_id": req.PedidoID})
}

// Entregar — POST /motoboy/entregar
// Body: {pedido_id, recebedor_nome, cpf, recebedor_tipo?, pgto_dinheiro, pgto_pix,
//
//	pgto_cartao, lat?, lng?, accuracy?}
//
// Port FIEL de sz_mb_api_entregar(): pedido em rota/a caminho, nome do recebedor
// >=3 chars, CPF válido (dígitos verificadores), match EXATO do valor pago vs
// valor do pedido, GPS operacional. GPS usa ultimo_lat/lng do motoboy como
// fallback quando lat/lng não vêm no corpo (igual ao PHP).
func (h *RotaHandler) Entregar(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PedidoID      int64    `json:"pedido_id"`
		RecebedorNome string   `json:"recebedor_nome"`
		CPF           string   `json:"cpf"`
		RecebedorTipo string   `json:"recebedor_tipo"`
		PgtoDinheiro  any      `json:"pgto_dinheiro"`
		PgtoPix       any      `json:"pgto_pix"`
		PgtoCartao    any      `json:"pgto_cartao"`
		Lat           *float64 `json:"lat"`
		Lng           *float64 `json:"lng"`
		Accuracy      *float64 `json:"accuracy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	if req.PedidoID == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "pedido_id obrigatório")
		return
	}

	ctx := r.Context()

	// SEC-GO-03: o pedido deve pertencer ao motoboy autenticado (anti-IDOR entre couriers).
	mb := auth.MotoboyfromCtx(ctx)
	if mb == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autorizado")
		return
	}

	var status string
	var donoID *int64
	var valorPedido float64
	err := h.Pool.QueryRow(ctx,
		`SELECT status, motoboy_id, COALESCE(valor_pedido,0) FROM sz_motoboy_pedidos WHERE id=$1`, req.PedidoID,
	).Scan(&status, &donoID, &valorPedido)
	if err != nil {
		httpx.WriteErr(w, http.StatusNotFound, "Pedido não encontrado para este motoboy.")
		return
	}
	if donoID == nil || *donoID != mb.ID {
		httpx.WriteErr(w, http.StatusForbidden, "pedido não atribuído a este motoboy")
		return
	}
	if status == "entregue" {
		httpx.WriteOK(w, map[string]any{"ok": true, "status": "entregue", "idempotent": true})
		return
	}
	// A baixa funciona em rota ou a caminho (sz_mb_api_entregar).
	if status != "em_rota" && status != "a_caminho" {
		httpx.WriteErr(w, http.StatusConflict, "Pedido precisa estar em rota/a caminho para ser entregue.")
		return
	}

	// Validações do recebedor.
	if nomeMuitoCurto(req.RecebedorNome) {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "Informe o nome do recebedor.")
		return
	}
	cpf := soNumeros.ReplaceAllString(req.CPF, "")
	if !validarCPF(cpf) {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "CPF do recebedor inválido.")
		return
	}
	recebedorTipo := strings.ToLower(strings.TrimSpace(req.RecebedorTipo))
	if recebedorTipo != "cliente" && recebedorTipo != "terceiro" {
		recebedorTipo = "cliente"
	}

	// Match EXATO do pagamento (sz_mb_api_entregar).
	dinheiro := parseMoney(req.PgtoDinheiro)
	pix := parseMoney(req.PgtoPix)
	cartao := parseMoney(req.PgtoCartao)
	valorPedido = round2(valorPedido)
	totalPago := round2(dinheiro + pix + cartao)
	if totalPago <= 0 {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "Informe o valor recebido.")
		return
	}
	if totalPago > valorPedido+0.009 {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "Valor recebido não pode exceder o valor do pedido.")
		return
	}
	if absFloat(totalPago-valorPedido) > 0.009 {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "Para entregar, o total recebido deve bater exatamente com o valor do pedido.")
		return
	}

	// GPS — usa lat/lng do corpo; fallback para ultimo_lat/lng do motoboy (igual ao PHP).
	lat, lng := h.gpsComFallback(ctx, mb.ID, req.Lat, req.Lng)
	if gpsOK, gpsMsg := validarGPSOperacional(lat, lng, req.Accuracy); !gpsOK {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, gpsMsg)
		return
	}

	// Trava de proximidade — GPS do motoboy precisa estar dentro do raio do
	// endereço de entrega (dest_lat/dest_lng). Fail-open quando o pedido não
	// tem coordenada de destino cadastrada (maioria da base hoje).
	var destLat, destLng sql.NullFloat64
	if err := h.Pool.QueryRow(ctx,
		`SELECT dest_lat, dest_lng FROM sz_motoboy_pedidos WHERE id=$1`, req.PedidoID,
	).Scan(&destLat, &destLng); err == nil && destLat.Valid && destLng.Valid {
		if dist := haversineMetersMb(destLat.Float64, destLng.Float64, lat, lng); dist > gpsDeliveryRadiusMetersMb {
			httpx.WriteErr(w, http.StatusUnprocessableEntity,
				fmt.Sprintf("GPS a %.0fm do endereço de entrega (máx. %.0fm) — aproxime-se para confirmar", dist, gpsDeliveryRadiusMetersMb))
			return
		}
	}

	// NOTA: sz_mb_api_entregar NÃO exige foto/comprovante na entrega (só a
	// frustração exige foto). Não há gate de comprovante aqui — porte FIEL.

	// BRIDGE: motoboy + sz_orders na MESMA transação (entregue → sz_orders.status='completo').
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		slog.Error("[rota] falha ao iniciar transação", "pedido_id", req.PedidoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao registrar entrega")
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err = tx.Exec(ctx, `
		UPDATE sz_motoboy_pedidos
		SET status='entregue', ts_entregue=NOW(), baixa_at=NOW(),
		    baixa_por='motoboy', baixa_motoboy_id=$2,
		    recebedor_nome=$3, recebedor_cpf=$4, recebedor_tipo=$5,
		    pgto_dinheiro=$6, pgto_pix=$7, pgto_cartao=$8,
		    entrega_lat=$9, entrega_lng=$10
		WHERE id=$1`,
		req.PedidoID, mb.ID,
		req.RecebedorNome, cpf, recebedorTipo,
		dinheiro, pix, cartao,
		lat, lng,
	); err != nil {
		slog.Error("[rota] falha ao registrar entrega", "pedido_id", req.PedidoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao registrar entrega")
		return
	}

	szRows, err := bridgeUpdatePedidoStatus(ctx, tx, req.PedidoID, "entregue")
	if err != nil {
		slog.Error("[rota] falha no bridge sz_orders", "pedido_id", req.PedidoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao registrar entrega")
		return
	}

	if err = tx.Commit(ctx); err != nil {
		slog.Error("[rota] falha ao commitar entrega", "pedido_id", req.PedidoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao registrar entrega")
		return
	}
	logBridge("rota.Entregar", req.PedidoID, "entregue", "completo", szRows)

	_, _ = h.Pool.Exec(ctx, `
		INSERT INTO sz_motoboy_audit (pedido_id, acao, de_status, para_status, meta, created_at)
		VALUES ($1, 'entregue', $2, 'entregue', $3, NOW())`,
		req.PedidoID, status, req.RecebedorNome,
	)

	slog.Info("[rota] pedido entregue", "pedido_id", req.PedidoID, "total", totalPago)
	httpx.WriteOK(w, map[string]any{"ok": true, "status": "entregue"})
}

// gpsComFallback retorna lat/lng do corpo ou, se ausentes, o último ping do
// motoboy (ultimo_lat/ultimo_lng), espelhando o fallback de sz_mb_api_entregar.
func (h *RotaHandler) gpsComFallback(ctx context.Context, motoboyID int64, latReq, lngReq *float64) (float64, float64) {
	var lat, lng float64
	if latReq != nil {
		lat = *latReq
	}
	if lngReq != nil {
		lng = *lngReq
	}
	if latReq == nil || lngReq == nil {
		var uLat, uLng *float64
		if err := h.Pool.QueryRow(ctx,
			`SELECT ultimo_lat, ultimo_lng FROM sz_motoboys WHERE id=$1`, motoboyID,
		).Scan(&uLat, &uLng); err == nil {
			if latReq == nil && uLat != nil {
				lat = *uLat
			}
			if lngReq == nil && uLng != nil {
				lng = *uLng
			}
		}
	}
	return lat, lng
}

// absFloat — valor absoluto auxiliar (evita dependência de math só por isto).
func absFloat(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// gpsDeliveryRadiusMetersMb — raio máximo entre o GPS do motoboy e o endereço
// de entrega para aceitar a confirmação (espelha order_detail.go do admin-service).
const gpsDeliveryRadiusMetersMb = 400.0

// haversineMetersMb — distância em metros entre duas coordenadas (raio da Terra 6371km).
func haversineMetersMb(lat1, lng1, lat2, lng2 float64) float64 {
	const earthRadiusM = 6371000.0
	toRad := func(d float64) float64 { return d * math.Pi / 180 }
	dLat := toRad(lat2 - lat1)
	dLng := toRad(lng2 - lng1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRad(lat1))*math.Cos(toRad(lat2))*math.Sin(dLng/2)*math.Sin(dLng/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusM * c
}

// Frustrar — POST /motoboy/frustrar
// Body: {pedido_id, motivo, observacao, foto_base64, lat?, lng?, accuracy?}
//
// Port FIEL de sz_mb_api_frustrar(): pedido em rota (somente), motivo E observacao
// obrigatórios, GPS operacional, foto base64 válida. Persiste a taxa de frustração
// congelada (sz_mbw_get_taxa_frustrado) e o isento (1ª frustração do pedido).
func (h *RotaHandler) Frustrar(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PedidoID   int64    `json:"pedido_id"`
		Motivo     string   `json:"motivo"`
		Observacao string   `json:"observacao"`
		FotoBase64 string   `json:"foto_base64"`
		Lat        *float64 `json:"lat"`
		Lng        *float64 `json:"lng"`
		Accuracy   *float64 `json:"accuracy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	if req.PedidoID == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "pedido_id obrigatório")
		return
	}

	ctx := r.Context()

	// SEC-GO-03: o pedido deve pertencer ao motoboy autenticado (anti-IDOR entre couriers).
	mb := auth.MotoboyfromCtx(ctx)
	if mb == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autorizado")
		return
	}

	var status string
	var donoID *int64
	var wcOrderID int64
	var destTelefone *string
	err := h.Pool.QueryRow(ctx,
		`SELECT status, motoboy_id, wc_order_id, dest_telefone FROM sz_motoboy_pedidos WHERE id=$1`, req.PedidoID,
	).Scan(&status, &donoID, &wcOrderID, &destTelefone)
	if err != nil {
		httpx.WriteErr(w, http.StatusNotFound, "Pedido não encontrado.")
		return
	}
	if donoID == nil || *donoID != mb.ID {
		httpx.WriteErr(w, http.StatusForbidden, "pedido não atribuído a este motoboy")
		return
	}
	// Frustrar só a partir de em_rota (sz_mb_api_frustrar).
	if status != "em_rota" {
		httpx.WriteErr(w, http.StatusConflict, "Pedido precisa estar em rota para ser frustrado.")
		return
	}
	if strings.TrimSpace(req.Motivo) == "" {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "Informe o motivo da tentativa frustrada.")
		return
	}
	if strings.TrimSpace(req.Observacao) == "" {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "Descreva o motivo da frustração.")
		return
	}

	lat, lng := h.gpsComFallback(ctx, mb.ID, req.Lat, req.Lng)
	if gpsOK, gpsMsg := validarGPSOperacional(lat, lng, req.Accuracy); !gpsOK {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, gpsMsg)
		return
	}
	if fotoOK, fotoMsg := validarFotoBase64(req.FotoBase64); !fotoOK {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, fotoMsg)
		return
	}

	tel := ""
	if destTelefone != nil {
		tel = *destTelefone
	}

	// BRIDGE: motoboy + sz_orders na MESMA transação (frustrado → sz_orders.status='frustrado').
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		slog.Error("[rota] falha ao iniciar transação", "pedido_id", req.PedidoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao registrar frustração")
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// CONGELAMENTO (snapshot) do financeiro de frustração, calculado DENTRO da tx
	// para leitura consistente. valor_taxa_frustrado é a TAXA DO MOTOBOY por
	// tentativa frustrada (sz_mbw_taxa_frustrado_mb_{id} > sz_mbw_taxa_frustrado >
	// default 5.00) — é o que motoboy_carteira/conciliação leem p/ PAGAR o
	// entregador. NÃO é penalty de produtor/afiliado: gravar penalty aqui CORROMPE
	// o pagamento do motoboy. frustrado_isento marca a 1ª frustração por TELEFONE.
	taxaFrust := taxaFrustrado(ctx, tx, mb.ID)
	isento := frustradoIsento(ctx, tx, wcOrderID, tel)

	if _, err = tx.Exec(ctx, `
		UPDATE sz_motoboy_pedidos
		SET status='frustrado', ts_frustrado=NOW(), baixa_at=NOW(),
		    baixa_por='motoboy', baixa_motoboy_id=$2,
		    frustrado_motivo=$3, frustrado_observacao=$4, frustrado_isento=$5,
		    valor_taxa_frustrado=$6, valor_taxa_frustrado_afiliado=0,
		    entrega_lat=$7, entrega_lng=$8
		WHERE id=$1`,
		req.PedidoID, mb.ID,
		req.Motivo, req.Observacao, isento,
		taxaFrust, lat, lng,
	); err != nil {
		slog.Error("[rota] falha ao registrar frustração", "pedido_id", req.PedidoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao registrar frustração")
		return
	}

	szRows, err := bridgeUpdatePedidoStatus(ctx, tx, req.PedidoID, "frustrado")
	if err != nil {
		slog.Error("[rota] falha no bridge sz_orders", "pedido_id", req.PedidoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao registrar frustração")
		return
	}

	if err = tx.Commit(ctx); err != nil {
		slog.Error("[rota] falha ao commitar frustração", "pedido_id", req.PedidoID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao registrar frustração")
		return
	}
	logBridge("rota.Frustrar", req.PedidoID, "frustrado", "frustrado", szRows)

	_, _ = h.Pool.Exec(ctx, `
		INSERT INTO sz_motoboy_audit (pedido_id, acao, de_status, para_status, meta, created_at)
		VALUES ($1, 'frustrado', $2, 'frustrado', $3, NOW())`,
		req.PedidoID, status, req.Motivo,
	)

	slog.Info("[rota] pedido frustrado", "pedido_id", req.PedidoID, "motivo", req.Motivo,
		"isento", isento, "taxa_frustrado_motoboy", taxaFrust)
	httpx.WriteOK(w, map[string]any{
		"ok": true, "status": "frustrado", "isento": isento,
		"taxa_frustrado_motoboy": taxaFrust,
	})
}
