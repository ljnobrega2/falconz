// tracking.go — Rastreio público do pedido (cliente final, SEM JWT).
//
// Namespace HTTP: /checkout-api/order/{code} (RAIZ do router, público — chamado
// pela página de rastreio do checkout FALKZ). O próprio {code} é o segredo: só
// quem recebeu o número do pedido consegue consultá-lo.
//
// Rota implementada:
//
//	GET /checkout-api/order/{code}
//	  → resolve o pedido por order_number (SZ-0000052) OU id (1584, pedidos
//	    migrados onde order_number == id) e devolve o estado de rastreio.
//
// CONTRATO (PT-BR):
//
//	{
//	  code, status,
//	  status_timeline: [{key,label,at}],     // at = RFC3339 ou null
//	  entrega: {tipo, data, destino_bairro, cidade, uf},
//	  itens:   [{nome, qtd, total, image_url}],
//	  cliente: {nome, telefone, email, cpf, endereco},  // endereco com número
//	                                                     // mascarado + complemento
//	                                                     // omitido (rastreio público)
//	  total
//	}
//
// FONTE DE VERDADE DO TIMELINE (decisão de schema, auditoria 2026-06-18):
//
//	sz_motoboy_pedidos carrega timestamps explícitos por estágio (ts_aprovado,
//	ts_embalado, ts_em_rota, ts_a_caminho, ts_entregue). Mapeamos:
//
//	  agendado      ← ts_aprovado   (fallback sz_orders.created_at sem linha motoboy)
//	  em_separacao  ← (SEM coluna de origem no schema) → at = null SEMPRE
//	  separado      ← ts_embalado
//	  em_rota       ← ts_em_rota
//	  a_caminho     ← ts_a_caminho  (pode ser null mesmo em pedido entregue — o
//	                                 estágio é às vezes pulado; o ladder NÃO é
//	                                 estritamente monotônico)
//	  completo      ← ts_entregue
//
//	'em_separacao' é o único estágio sem coluna de respaldo: a task permite
//	explicitamente at=null ("senão null/'sem previsão'"). NÃO inventamos timestamp.
//
//	Pedidos SEM linha motoboy (ofertas 'expedicao' ou migrados sem fila motoboy):
//	todos os ts ficam null; o `status` de topo vem de sz_orders.status, mapeado
//	para o mesmo vocabulário de chaves do ladder.
//
//	frustrado/cancelado NÃO são estágios do ladder — aparecem só no `status` de
//	topo; o timeline continua mostrando os ts do caminho feliz que existirem.
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/senderzz/orders-service/internal/httpx"
)

// ── Vocabulário do timeline ─────────────────────────────────────────────────

// trackingStage descreve um estágio do ladder de rastreio (chave + rótulo PT-BR).
type trackingStage struct {
	Key   string
	Label string
}

// AUDIT-2026-07-30 #5 (dono: "essa página de rastreio precisa ser de expedição
// - não mexer com coisa de COD... você tá confundindo COD com PAD"): motoboy/COD
// e expedição (ME) são fluxos DIFERENTES com vocabulário de status DIFERENTE —
// um ladder único (agendado/em_rota/a_caminho, vocabulário motoboy) estava sendo
// aplicado indevidamente a pedidos de expedição, que nem têm esses estágios.
// Vocabulário real de expedição (idêntico ao admin: ExpedicaoOrders.tsx
// FILTER_GROUPS/GROUP_ORDER) = pendente → em_andamento → aprovado → separado →
// enviado → a caminho → entregue, com cancelado/frustrado/reembolsado fora do ladder
// (banner terminal "alerta"). Cada tipo de entrega usa o SEU PRÓPRIO ladder.

// motoboyLadder — fluxo motoboy/COD.
var motoboyLadder = []trackingStage{
	{"agendado", "Agendado"},
	{"em_separacao", "Em separação"},
	{"separado", "Separado"},
	{"em_rota", "Em rota"},
	{"a_caminho", "A caminho"},
	{"completo", "Completo"},
}

// expedicaoLadder — fluxo expedição (ME), vocabulário próprio.
var expedicaoLadder = []trackingStage{
	{"pendente", "Pendente"},
	{"em_andamento", "Em andamento"},
	{"aprovado", "Aprovado"},
	{"separado", "Separado"},
	{"enviado", "Enviado"},
	{"a_caminho", "A caminho"},
	{"entregue", "Entregue"},
}

// statusToMotoboyTimelineKey mapeia o status bruto de sz_motoboy_pedidos pro
// ladder motoboy. Terminais (frustrado/cancelado) preservados fora do ladder.
func statusToMotoboyTimelineKey(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "agendado", "pre_agendado", "aprovado", "aguardando", "pending", "processing", "on_hold", "on-hold":
		return "agendado"
	case "em_separacao":
		return "em_separacao"
	case "embalado", "separado":
		return "separado"
	case "em_rota":
		return "em_rota"
	case "a_caminho":
		return "a_caminho"
	case "entregue", "completo", "concluido", "completed":
		return "completo"
	case "frustrado":
		return "frustrado"
	case "cancelado", "cancelled":
		return "cancelado"
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

// statusToExpedicaoTimelineKey mapeia o status bruto de sz_orders (fluxo
// expedição/ME) pro ladder expedição. MESMO vocabulário do admin
// (ExpedicaoOrders.tsx FILTER_GROUPS) — nenhuma chave inventada aqui.
func statusToExpedicaoTimelineKey(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "pending", "aguardando", "on-hold", "on_hold":
		return "pendente"
	case "em_andamento":
		return "em_andamento"
	case "processing":
		return "aprovado"
	case "em_separacao", "embalado", "coletado":
		return "separado"
	case "enviado":
		return "enviado"
	case "a_caminho":
		return "a_caminho"
	case "entregue", "completo":
		return "entregue"
	// terminais fora do ladder — banner "alerta" (mesmo agrupamento do admin).
	case "cancelled", "cancelado", "frustrado", "reembolsado", "em_cancelamento":
		return "alerta"
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

// buildTimeline monta os passos do ladder com o timestamp (se houver) de cada chave.
func buildTimeline(ladder []trackingStage, tsByKey map[string]*time.Time) []timelineStep {
	out := make([]timelineStep, 0, len(ladder))
	for _, st := range ladder {
		var at *string
		if ts := tsByKey[st.Key]; ts != nil && !ts.IsZero() {
			formatted := ts.Format(time.RFC3339)
			at = &formatted
		}
		out = append(out, timelineStep{Key: st.Key, Label: st.Label, At: at})
	}
	return out
}

// ── Estruturas do contrato ──────────────────────────────────────────────────

type timelineStep struct {
	Key   string  `json:"key"`
	Label string  `json:"label"`
	At    *string `json:"at"` // RFC3339 quando atingido; null caso contrário.
}

type trackingEntrega struct {
	Tipo            string  `json:"tipo"`                       // 'motoboy' | 'expedicao'
	Data            *string `json:"data"`                       // YYYY-MM-DD (data de entrega) ou null
	PrevisaoHorario string  `json:"previsao_horario,omitempty"` // horário exato estimado para Motoboy
	HorarioEntrega  string  `json:"horario_entrega,omitempty"`  // hora real quando marcado como entregue
	DestinoBairro   *string `json:"destino_bairro"`             // bairro de destino
	Cidade          *string `json:"cidade"`
	UF              *string `json:"uf"`
	// AUDIT-2026-07-30 #7 (dono: "falta mostrar a informação... que vem do
	// rastreio"): código de rastreio real da transportadora (wc_me_labels.
	// tracking_code) — nulo até a transportadora atribuir (comum em Jadlog/Loggi
	// até a coleta física). NUNCA nomeia a plataforma intermediária pro cliente
	// (regra do dono: "nada de falar Melhor Envio pro cliente final").
	CodigoRastreio *string `json:"codigo_rastreio"`
}

// previsaoHorarioMotoboy distribui a previsão de forma estável e pseudoaleatória
// por pedido, usando HMAC-SHA256. Retorna um minuto exato entre 15:01 e 17:59
// (179 possibilidades), nunca uma faixa.
func previsaoHorarioMotoboy(code string) string {
	sig := trackingSig(code)
	if len(sig) < 8 {
		return ""
	}
	seed, err := strconv.ParseUint(sig[:8], 16, 32)
	if err != nil {
		return ""
	}
	minute := 15*60 + 1 + int(seed%179)
	return fmt.Sprintf("%02d:%02d", minute/60, minute%60)
}

type trackingItem struct {
	Nome     string `json:"nome"`
	Qtd      int    `json:"qtd"`
	Total    string `json:"total"`     // subtotal do item, string fixa 2 casas
	ImageURL string `json:"image_url"` // sempre presente; "" quando sem fonte
}

type trackingCliente struct {
	Nome     *string `json:"nome"`
	Telefone *string `json:"telefone"`
	Email    *string `json:"email"`
	CPF      *string `json:"cpf"` // formatado 000.000.000-00 quando 11 dígitos
	Endereco *string `json:"endereco"`
}

// trackingMELive — status confirmado AGORA na ME (consulta ao vivo, read-only,
// best-effort). null quando indisponível (timeout/erro/sem etiqueta) — nunca
// bloqueia a página por causa disso. AUDIT-2026-07-30 #6 (dono: "última
// atualização direto do ME").
type trackingMELive struct {
	Status    string `json:"status"`
	CheckedAt string `json:"checked_at"`
}

type trackingCarrierEvent struct {
	At          string `json:"at"`
	Description string `json:"description"`
	Location    string `json:"location"`
	Stage       string `json:"stage"`
}

type trackingResponse struct {
	Code           string                 `json:"code"`
	Status         string                 `json:"status"` // chave de timeline ou terminal
	StatusTimeline []timelineStep         `json:"status_timeline"`
	Entrega        trackingEntrega        `json:"entrega"`
	Itens          []trackingItem         `json:"itens"`
	Cliente        trackingCliente        `json:"cliente"`
	Total          string                 `json:"total"` // string fixa 2 casas
	MELive         *trackingMELive        `json:"me_live"`
	CarrierEvents  []trackingCarrierEvent `json:"carrier_events"`
	Brand          trackingBrand          `json:"brand"`
}

type trackingBrand struct {
	Name      string `json:"name,omitempty"`
	LogoURL   string `json:"logo_url,omitempty"`
	Accent    string `json:"cor,omitempty"`
	BannerURL string `json:"banner_url,omitempty"`
	WhatsApp  string `json:"whatsapp,omitempty"`
	TextColor string `json:"texto,omitempty"`
}

// ── GET /checkout-api/order/{code} ──────────────────────────────────────────

// GetOrderTracking resolve um pedido pelo code e devolve seu estado de rastreio.
//
// Resolução: order_number = code (SZ-0000052 ou 1584). Quando code é só dígitos,
// também tenta id = code (pedidos migrados onde order_number == id). 404 se não achar.
func (h *CheckoutHandler) GetOrderTracking(w http.ResponseWriter, r *http.Request) {
	rawCode := strings.TrimSpace(chi.URLParam(r, "code"))
	if rawCode == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "code é obrigatório")
		return
	}

	// ── 0. Anti-enumeração: SEPARA order_number + sig, recomputa e valida ANTES
	// de tocar no banco. Sig inválido/ausente ⇒ MESMO 404 de "não encontrado"
	// (não vaza existência). Bloqueia varredura sequencial (1,2,3,… ⇒ 404).
	code, ok := verifyTrackingCode(rawCode)
	if !ok {
		httpx.WriteErr(w, http.StatusNotFound, "pedido não encontrado")
		return
	}
	ctx := r.Context()

	// ── 1. Resolve o pedido base ──────────────────────────────────────────────
	var (
		orderID      int64
		orderNumber  string
		produtorID   int64
		ordStatus    string
		totalStr     string
		custName     *string
		billingEmail *string
		linkTipo     *string // tipo do checkout link ('motoboy'|'correio'...) via meta token
	)
	// code numérico ⇒ também casa por id (order_number == id em pedidos migrados).
	codeDigits := onlyDigits(code)
	isNumeric := codeDigits == code && codeDigits != ""

	var err error
	if isNumeric {
		idVal, _ := strconv.ParseInt(codeDigits, 10, 64)
		err = h.db.QueryRow(ctx,
			`SELECT id, order_number, produtor_id, status, total, customer_name, billing_email
			   FROM sz_orders
			  WHERE order_number = $1 OR id = $2
			  ORDER BY id DESC
			  LIMIT 1`,
			code, idVal,
		).Scan(&orderID, &orderNumber, &produtorID, &ordStatus, &totalStr, &custName, &billingEmail)
	} else {
		err = h.db.QueryRow(ctx,
			`SELECT id, order_number, produtor_id, status, total, customer_name, billing_email
			   FROM sz_orders
			  WHERE order_number = $1
			  LIMIT 1`,
			code,
		).Scan(&orderID, &orderNumber, &produtorID, &ordStatus, &totalStr, &custName, &billingEmail)
	}
	if err == pgx.ErrNoRows {
		httpx.WriteErr(w, http.StatusNotFound, "pedido não encontrado")
		return
	}
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao buscar pedido")
		return
	}

	// ── 2. Linha motoboy (timeline + destino + data de entrega) — opcional ────
	// Pedidos 'expedicao' ou migrados sem fila motoboy NÃO têm linha aqui; nesse
	// caso todos os ts ficam null e o status de topo vem de sz_orders.status.
	var (
		mbStatus                                     *string
		tsAprovado, tsEmbalado, tsEmRota, tsACaminho *time.Time
		tsEntregue                                   *time.Time
		dataEntrega, reagendadoPara                  *time.Time
		mbBairro, mbCidade, mbUF                     *string
	)
	errMb := h.db.QueryRow(ctx,
		`SELECT status, ts_aprovado, ts_embalado, ts_em_rota, ts_a_caminho, ts_entregue,
		        data_entrega, reagendado_para, dest_bairro, dest_cidade, dest_uf
		   FROM sz_motoboy_pedidos
		  WHERE wc_order_id = $1
		  ORDER BY id DESC
		  LIMIT 1`,
		orderID,
	).Scan(&mbStatus, &tsAprovado, &tsEmbalado, &tsEmRota, &tsACaminho, &tsEntregue,
		&dataEntrega, &reagendadoPara, &mbBairro, &mbCidade, &mbUF)
	hasMotoboy := errMb == nil
	if errMb != nil && errMb != pgx.ErrNoRows {
		// Não falha o rastreio por isso — segue sem dados de motoboy.
		hasMotoboy = false
	}

	// ── 3. Endereço de envio (fonte primária de bairro/cidade/uf p/ qualquer pedido) ─
	var (
		addrNome, addrEmail, addrTel            *string
		addrCEP, addrLograd, addrNum, addrCompl *string
		addrBairro, addrCidade, addrUF          *string
	)
	_ = h.db.QueryRow(ctx,
		`SELECT nome, email, telefone, cep, logradouro, numero, complemento,
		        bairro, cidade, uf
		   FROM sz_order_addresses
		  WHERE order_id = $1 AND tipo = 'shipping'
		  ORDER BY id ASC
		  LIMIT 1`,
		orderID,
	).Scan(&addrNome, &addrEmail, &addrTel, &addrCEP, &addrLograd, &addrNum, &addrCompl,
		&addrBairro, &addrCidade, &addrUF)

	// ── 4. Itens ──────────────────────────────────────────────────────────────
	// LATERAL resolve UM único produto por item (prefere match por id; cai para
	// wp_post_id), evitando linhas duplicadas se algum id colidir com um wp_post_id
	// de outro produto. image_url vem de sz_products.meta; "" quando não há fonte
	// (a imagem real é attachment do WP, não espelhado no Postgres).
	itemRows, errIt := h.db.Query(ctx,
		`SELECT i.nome, i.quantidade, i.subtotal,
		        COALESCE(
		          NULLIF(p.meta->>'image_url', ''),
		          NULLIF(p.meta->>'thumb_url', ''),
		          NULLIF(p.meta->>'thumbnail', ''),
		          NULLIF(p.meta->>'imagem', ''),
		          NULLIF(p.meta->>'image', ''),
		          ''
		        ) AS image_url
		   FROM sz_order_items i
		   LEFT JOIN LATERAL (
		          SELECT meta
		            FROM sz_products sp
		           WHERE sp.id = i.produto_id OR sp.wp_post_id = i.produto_id
		           ORDER BY (sp.id = i.produto_id) DESC
		           LIMIT 1
		        ) p ON TRUE
		  WHERE i.order_id = $1
		  ORDER BY i.id`,
		orderID,
	)
	itens := []trackingItem{}
	if errIt == nil {
		defer itemRows.Close()
		for itemRows.Next() {
			var it trackingItem
			var sub string
			if err := itemRows.Scan(&it.Nome, &it.Qtd, &sub, &it.ImageURL); err != nil {
				continue
			}
			it.Total = decimalFixed2(sub)
			it.ImageURL = strings.TrimSpace(it.ImageURL)
			itens = append(itens, it)
		}
	}

	// ── 5. Meta: CPF + telefone + e-mail de cobrança e DATA DE ENTREGA ───────────
	// A data de entrega canônica vive em meta (_sz_delivery_date), não na coluna
	// data_entrega (vazia em pedidos migrados/recentes — ver nota Pedidos-V2 do
	// CLAUDE.md). Lemos aqui para usar no COALESCE do bloco entrega.
	var metaCPF, metaCel, metaEmail, metaDeliveryDate *string
	metaRows, errMeta := h.db.Query(ctx,
		`SELECT meta_key, meta_value
		   FROM sz_order_meta
		  WHERE order_id = $1
		    AND meta_key IN ('_billing_cpf','_billing_cellphone','_senderzz_customer_email',
		                     '_sz_delivery_date','_senderzz_delivery_date')`,
		orderID,
	)
	if errMeta == nil {
		defer metaRows.Close()
		for metaRows.Next() {
			var k, v string
			if err := metaRows.Scan(&k, &v); err != nil {
				continue
			}
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}
			switch k {
			case "_billing_cpf":
				vv := v
				metaCPF = &vv
			case "_billing_cellphone":
				vv := v
				metaCel = &vv
			case "_senderzz_customer_email":
				vv := v
				metaEmail = &vv
			case "_sz_delivery_date", "_senderzz_delivery_date":
				// _sz_delivery_date é o canônico; _senderzz_delivery_date é fallback
				// (só preenche se o canônico ainda não veio).
				if metaDeliveryDate == nil || k == "_sz_delivery_date" {
					vv := v
					metaDeliveryDate = &vv
				}
			}
		}
	}

	// ── 6. Monta o timeline ────────────────────────────────────────────────────
	// AUDIT-2026-07-30 #5 (dono): motoboy/COD e expedição (ME) são fluxos
	// DIFERENTES — cada um usa o SEU ladder (motoboyLadder/expedicaoLadder) e
	// os timestamps vêm de fontes DIFERENTES. hasMotoboy decide qual dos dois.
	var timeline []timelineStep
	var topStatus string
	var codigoRastreio *string
	var meLive *trackingMELive
	var meEstimatedDelivery *string
	var carrierEvents []trackingCarrierEvent

	if hasMotoboy {
		// Os ts_* são `timestamp without time zone` que GUARDAM hora-de-parede do
		// Brasil (o módulo PHP usa America/Sao_Paulo — ver
		// includes/motoboy/database.php:12 e sz_motoboy_normalize_legacy_utc_timestamps*).
		// O pgx lê coluna naive como UTC, então reinterpretamos a hora-de-parede em
		// America/Sao_Paulo antes de formatar, senão o rastreio mostraria +3h de erro.
		tsByKey := map[string]*time.Time{
			"agendado":     spWallClock(tsAprovado),
			"em_separacao": nil, // sem coluna de origem — at=null por design.
			"separado":     spWallClock(tsEmbalado),
			"em_rota":      spWallClock(tsEmRota),
			"a_caminho":    spWallClock(tsACaminho),
			"completo":     spWallClock(tsEntregue),
		}
		timeline = buildTimeline(motoboyLadder, tsByKey)

		topRaw := ordStatus
		if mbStatus != nil && strings.TrimSpace(*mbStatus) != "" {
			topRaw = *mbStatus
		}
		topStatus = statusToMotoboyTimelineKey(topRaw)
	} else {
		// Expedição: SEM linha sz_motoboy_pedidos, sem ts_* — fontes reais são
		// sz_orders.created_at (pendente) e wc_me_labels (separado/enviado/entregue,
		// única fonte de verdade do fluxo ME). Estágios intermediários sem coluna
		// própria (em_andamento/aprovado) ficam SEM data quando já superados (não
		// há histórico) — nunca fabricamos timestamp que não temos.
		tsByKey := map[string]*time.Time{
			"pendente": nil, "em_andamento": nil, "aprovado": nil,
			"separado": nil, "enviado": nil, "a_caminho": nil, "entregue": nil,
		}
		var createdAt time.Time
		if err := h.db.QueryRow(ctx,
			`SELECT created_at FROM sz_orders WHERE id = $1`, orderID,
		).Scan(&createdAt); err == nil {
			c := createdAt.In(tzSaoPaulo)
			tsByKey["pendente"] = &c
		}

		// Histórico persistido da etiqueta ME: a API retorna o estado atual, mas
		// cada snapshot salvo pelo labels-service preserva quando cada etapa foi
		// observada. Primeira ocorrência vence; consulta live abaixo só preenche
		// etapas ainda não gravadas.
		if histRows, err := h.db.Query(ctx, `
			SELECT created_at_me, posted_at_me, delivered_at_me
			  FROM wc_me_tracking_history
			 WHERE wc_order_id = $1
			 ORDER BY observed_at ASC, id ASC`, orderID); err == nil {
			for histRows.Next() {
				var created, posted, delivered string
				if err := histRows.Scan(&created, &posted, &delivered); err != nil {
					continue
				}
				if t := parseMETimeSP(created); t != nil && tsByKey["separado"] == nil {
					tsByKey["separado"] = t
				}
				if t := parseMETimeSP(posted); t != nil && tsByKey["enviado"] == nil {
					tsByKey["enviado"] = t
				}
				if t := parseMETimeSP(delivered); t != nil && tsByKey["entregue"] == nil {
					tsByKey["entregue"] = t
				}
			}
			histRows.Close()
		}

		// AUDIT-2026-07-30 #8 (dono: mostrou o painel público da ME com evento
		// real "Adicionado no sistema 29 jul 10:22"): consulta ao vivo (best-effort)
		// dos timestamps REAIS por etapa confirmados na ME — fonte MAIS precisa
		// que existe, prioridade máxima sobre histórico/aproximação abaixo.
		if h.labelsServiceURL != "" {
			if ev := h.fetchMEEvents(orderID); ev != nil {
				meLive = &trackingMELive{Status: ev.status, CheckedAt: time.Now().Format(time.RFC3339)}
				if t := parseMETimeSP(ev.createdAt); t != nil && tsByKey["separado"] == nil {
					tsByKey["separado"] = t
				}
				if t := parseMETimeSP(ev.postedAt); t != nil && tsByKey["enviado"] == nil {
					tsByKey["enviado"] = t
				}
				if t := parseMETimeSP(ev.deliveredAt); t != nil && tsByKey["entregue"] == nil {
					tsByKey["entregue"] = t
				}
				if strings.TrimSpace(ev.tracking) != "" {
					tr := ev.tracking
					codigoRastreio = &tr
				}
				// AUDIT-2026-07-30 #11 (dono: "coloca a previsão de chegada tb"):
				// delivery_min/max = prazo em dias úteis direto da ME, contado a
				// partir do despacho (posted_at) ou, se ainda não despachou, da
				// emissão da etiqueta (created_at). Só estima quando a ME informa
				// o prazo — nunca fabrica data sem essa base.
				baseStr := ev.postedAt
				if strings.TrimSpace(baseStr) == "" {
					baseStr = ev.createdAt
				}
				if base := parseMETimeSP(baseStr); base != nil && ev.deliveryMax > 0 {
					est := base.AddDate(0, 0, ev.deliveryMax).Format("2006-01-02")
					meEstimatedDelivery = &est
				}
			}
		}

		// AUDIT-2026-07-30 #6 (dono: "falta horários"): sz_order_status_history já
		// existe (populada por vários handlers Go — checkout/admin/portal/webhook
		// ME, ver AUDIT em syncOrderStatusFromME) e tem o timestamp REAL de cada
		// transição — fonte MAIS precisa que a aproximação via wc_me_labels
		// abaixo. Primeira ocorrência de cada chave (ASC) vence.
		histRows, errHist := h.db.Query(ctx,
			`SELECT status_para, created_at FROM sz_order_status_history
			  WHERE order_id = $1 ORDER BY created_at ASC`,
			orderID,
		)
		if errHist == nil {
			defer histRows.Close()
			for histRows.Next() {
				var statusPara string
				var changedAt time.Time
				if err := histRows.Scan(&statusPara, &changedAt); err != nil {
					continue
				}
				key := statusToExpedicaoTimelineKey(statusPara)
				if _, known := tsByKey[key]; known && tsByKey[key] == nil {
					c := changedAt.In(tzSaoPaulo)
					tsByKey[key] = &c
				}
			}
		}
		// Uma correção manual pode reverter um pedido que foi marcado como
		// entregue indevidamente. O histórico preserva a auditoria dessa
		// confirmação, mas não deve fazer a etapa "Entregue" aparecer como
		// concluída enquanto o status atual ainda não for terminal.
		if currKey := statusToExpedicaoTimelineKey(ordStatus); currKey != "entregue" {
			tsByKey["entregue"] = nil
		}

		var lblCreated, lblUpdated time.Time
		var lblStatus, lblTracking string
		errLbl := h.db.QueryRow(ctx,
			`SELECT status, created_at, updated_at, COALESCE(tracking_code, '') FROM wc_me_labels
			  WHERE wc_order_id = $1 AND status <> 'canceled'
			  ORDER BY id DESC LIMIT 1`,
			orderID,
		).Scan(&lblStatus, &lblCreated, &lblUpdated, &lblTracking)
		if errLbl == nil {
			// Etiqueta emitida = pedido separado, pronto pra postagem (fallback só
			// se o histórico real não já tiver essa etapa).
			if tsByKey["separado"] == nil {
				c := lblCreated.In(tzSaoPaulo)
				tsByKey["separado"] = &c
			}
			// "posted"/"delivered" = despachado de fato (mesmo mapeamento de
			// mapMEStatusToOrderStatus em go/labels/internal/handlers/labels.go).
			if (lblStatus == "posted" || lblStatus == "delivered") && tsByKey["enviado"] == nil {
				c := lblUpdated.In(tzSaoPaulo)
				tsByKey["enviado"] = &c
			}
			if lblStatus == "delivered" && tsByKey["entregue"] == nil {
				c := lblUpdated.In(tzSaoPaulo)
				tsByKey["entregue"] = &c
			}
			if codigoRastreio == nil && strings.TrimSpace(lblTracking) != "" {
				codigoRastreio = &lblTracking
			}
			if strings.TrimSpace(lblTracking) != "" {
				if rows, err := h.db.Query(ctx, `
					SELECT event_at, description, location, stage
					  FROM wc_carrier_tracking_events
					 WHERE tracking_code = $1
					 ORDER BY event_at DESC, id DESC
					 LIMIT 30`, lblTracking); err == nil {
					for rows.Next() {
						var at time.Time
						var e trackingCarrierEvent
						if err := rows.Scan(&at, &e.Description, &e.Location, &e.Stage); err == nil {
							e.At = at.Format(time.RFC3339)
							if strings.Contains(strings.ToLower(e.Description), "a caminho") && tsByKey["a_caminho"] == nil {
								c := at.In(tzSaoPaulo)
								tsByKey["a_caminho"] = &c
							}
							carrierEvents = append(carrierEvents, e)
						}
					}
					rows.Close()
				}
			}
		}
		// Estágio ATUAL sem coluna de origem própria (em_andamento/aprovado): usa
		// updated_at do pedido só pro estágio em que ele está AGORA — é o único
		// instante que temos garantia de ser real (última escrita na linha).
		currKey := statusToExpedicaoTimelineKey(ordStatus)
		if (currKey == "em_andamento" || currKey == "aprovado") && tsByKey[currKey] == nil {
			var updatedAt time.Time
			if err := h.db.QueryRow(ctx,
				`SELECT updated_at FROM sz_orders WHERE id = $1`, orderID,
			).Scan(&updatedAt); err == nil {
				c := updatedAt.In(tzSaoPaulo)
				tsByKey[currKey] = &c
			}
		}

		timeline = buildTimeline(expedicaoLadder, tsByKey)
		topStatus = currKey
	}

	// ── 8. Bloco entrega ───────────────────────────────────────────────────────
	// tipo = modo de entrega ('motoboy'|'expedicao'). Resolve pelo tipo do checkout
	// link (via meta checkout_token) com fallback: linha motoboy ⇒ 'motoboy'.
	linkTipo = h.resolveLinkTipo(ctx, orderID)
	entregaTipo := "expedicao"
	if linkTipo != nil && strings.EqualFold(strings.TrimSpace(*linkTipo), "motoboy") {
		entregaTipo = "motoboy"
	} else if hasMotoboy {
		entregaTipo = "motoboy"
	}

	// bairro/cidade/uf: endereço de envio é a fonte primária (presente p/ todos);
	// motoboy é fallback.
	bairro := coalescePtr(addrBairro, mbBairro)
	cidade := coalescePtr(addrCidade, mbCidade)
	uf := coalescePtr(addrUF, mbUF)

	// data de entrega: precedência reagendado_para > data_entrega (coluna) >
	// _sz_delivery_date (meta canônica, preenchida na maioria dos pedidos enquanto
	// as colunas ficam vazias). Sempre YYYY-MM-DD.
	var dataStr *string
	switch {
	case reagendadoPara != nil && !reagendadoPara.IsZero():
		d := reagendadoPara.Format("2006-01-02")
		dataStr = &d
	case dataEntrega != nil && !dataEntrega.IsZero():
		d := dataEntrega.Format("2006-01-02")
		dataStr = &d
	case metaDeliveryDate != nil:
		// Normaliza para YYYY-MM-DD (meta pode vir "2026-06-22" ou com hora).
		if d := normalizeDateYMD(*metaDeliveryDate); d != "" {
			dataStr = &d
		}
	case meEstimatedDelivery != nil:
		// AUDIT-2026-07-30 #11: sem data cadastrada — usa a previsão calculada
		// a partir do prazo real informado pela ME (delivery_max).
		dataStr = meEstimatedDelivery
	}

	entrega := trackingEntrega{
		Tipo:           entregaTipo,
		Data:           dataStr,
		DestinoBairro:  bairro,
		Cidade:         cidade,
		UF:             uf,
		CodigoRastreio: codigoRastreio,
	}
	if entregaTipo == "motoboy" {
		entrega.PrevisaoHorario = previsaoHorarioMotoboy(orderNumber)
		if entregue := spWallClock(tsEntregue); entregue != nil {
			entrega.HorarioEntrega = entregue.Format("15:04")
		}
	}

	// ── 9. Bloco cliente (PII MASCARADA) ────────────────────────────────────────
	// nome: address > customer_name. telefone: address > meta cellphone.
	// email: address > meta customer_email > billing_email. cpf: meta _billing_cpf.
	//
	// MÁSCARA: cpf/telefone/email são mascarados antes de sair no JSON (rastreio é
	// público). Nome e endereço ficam (o cliente precisa conferir a entrega).
	cliNome := coalescePtr(addrNome, custName)
	// telefone: resolve a fonte JÁ sem +55 (normalizePhone) e depois mascara o
	// número nacional → "(11) *****-4006".
	cliTel := maskPhonePtr(coalescePtr(normalizePhone(addrTel), normalizePhone(metaCel)))
	cliEmail := maskEmailPtr(coalescePtr(nonEmptyPtr(addrEmail), coalescePtr(metaEmail, nonEmptyPtr(billingEmail))))
	var cliCPF *string
	if metaCPF != nil {
		f := maskCPF(*metaCPF)
		cliCPF = &f
	}
	endereco := buildEndereco(addrLograd, addrNum, addrCompl, addrBairro, addrCidade, addrUF, addrCEP)

	cliente := trackingCliente{
		Nome:     cliNome,
		Telefone: cliTel,
		Email:    cliEmail,
		CPF:      cliCPF,
		Endereco: endereco,
	}

	brandLogo, brandColor, brandName, brandBanner, brandWhatsApp, brandTextColor := h.resolveProducerBrand(ctx, produtorID, 0)
	resp := trackingResponse{
		Code:           orderNumber,
		Status:         topStatus,
		StatusTimeline: timeline,
		Entrega:        entrega,
		Itens:          itens,
		Cliente:        cliente,
		Total:          decimalFixed2(totalStr),
		MELive:         meLive,
		CarrierEvents:  carrierEvents,
		Brand: trackingBrand{
			Name: brandName, LogoURL: brandLogo, Accent: brandColor,
			BannerURL: brandBanner, WhatsApp: brandWhatsApp, TextColor: brandTextColor,
		},
	}

	httpx.WriteOK(w, map[string]any{"rastreio": resp})
}

// meEventsRaw — resposta crua de GET /internal/me-status/events/{order_id}.
type meEventsRaw struct {
	status      string
	createdAt   string
	postedAt    string
	deliveredAt string
	tracking    string
	deliveryMin int
	deliveryMax int
}

// fetchMEEvents consulta GET /internal/me-status/events/{order_id} no
// labels-service — read-only, sem gravar nada. Timeout curto (2s) e fail-open:
// qualquer erro devolve nil, nunca quebra a página pública de rastreio.
// AUDIT-2026-07-30 #8 (dono: painel público ME mostra evento real "Adicionado
// no sistema" com data — GET /me/orders/{id} devolve created_at/posted_at/
// delivered_at reais por etapa, mais precisos que qualquer aproximação).
// AUDIT-2026-07-30 #11 (dono confirmou "authorization_code... esse é o
// rastreio jadlog"): tracking retornado já prioriza authorization_code (feito
// no labels-service); delivery_min/max = prazo em dias pra previsão real.
func (h *CheckoutHandler) fetchMEEvents(orderID int64) *meEventsRaw {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	url := fmt.Sprintf("%s/internal/me-status/events/%d", h.labelsServiceURL, orderID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var body struct {
		OK          bool   `json:"ok"`
		Status      string `json:"status"`
		CreatedAt   string `json:"created_at"`
		PostedAt    string `json:"posted_at"`
		DeliveredAt string `json:"delivered_at"`
		Tracking    string `json:"tracking"`
		DeliveryMin int    `json:"delivery_min"`
		DeliveryMax int    `json:"delivery_max"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || !body.OK {
		return nil
	}
	return &meEventsRaw{
		status: body.Status, createdAt: body.CreatedAt, postedAt: body.PostedAt,
		deliveredAt: body.DeliveredAt, tracking: body.Tracking,
		deliveryMin: body.DeliveryMin, deliveryMax: body.DeliveryMax,
	}
}

// parseMETimeSP parseia "YYYY-MM-DD HH:MM:SS" (UTC, formato da ME) e converte
// pra America/Sao_Paulo. "" ou formato inválido ⇒ nil (nunca fabrica timestamp).
func parseMETimeSP(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	t, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.UTC)
	if err != nil {
		return nil
	}
	c := t.In(tzSaoPaulo)
	return &c
}

// ── Helpers ─────────────────────────────────────────────────────────────────

// resolveLinkTipo lê o tipo do checkout link associado ao pedido (via meta
// checkout_token → senderzz_checkout_links.tipo). Best-effort; retorna nil se ausente.
func (h *CheckoutHandler) resolveLinkTipo(ctx context.Context, orderID int64) *string {
	var tipo string
	err := h.db.QueryRow(ctx,
		`SELECT cl.tipo
		   FROM sz_order_meta m
		   JOIN senderzz_checkout_links cl ON cl.token = m.meta_value
		  WHERE m.order_id = $1
		    AND m.meta_key IN ('checkout_token','_senderzz_offer_token')
		  ORDER BY m.id ASC
		  LIMIT 1`,
		orderID,
	).Scan(&tipo)
	if err != nil {
		return nil
	}
	tipo = strings.TrimSpace(tipo)
	if tipo == "" {
		return nil
	}
	return &tipo
}

// spWallClock reinterpreta um timestamp naive (lido pelo pgx como UTC) como
// hora-de-parede de America/Sao_Paulo. Usado nos ts_* do sz_motoboy_pedidos, que
// são `timestamp without time zone` guardando horário Brasil. Retorna nil se nil/zero.
func spWallClock(t *time.Time) *time.Time {
	if t == nil || t.IsZero() {
		return nil
	}
	r := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), tzSaoPaulo)
	return &r
}

// normalizeDateYMD extrai YYYY-MM-DD de uma string de data de meta (aceita
// "2026-06-22" ou "2026-06-22 00:00:00" etc.). Retorna "" se não parecer data.
func normalizeDateYMD(s string) string {
	s = strings.TrimSpace(s)
	if len(s) < 10 {
		return ""
	}
	head := s[:10]
	if _, err := time.Parse("2006-01-02", head); err != nil {
		return ""
	}
	return head
}

// decimalFixed2 normaliza uma string numérica para 2 casas (defensivo contra
// "197.0" / "197" / ""). Fallback "0.00".
func decimalFixed2(s string) string {
	d, err := decimal.NewFromString(strings.TrimSpace(s))
	if err != nil {
		return "0.00"
	}
	return d.StringFixed(2)
}

// coalescePtr devolve o primeiro ponteiro não-nil com conteúdo não vazio.
func coalescePtr(a, b *string) *string {
	if a != nil && strings.TrimSpace(*a) != "" {
		return a
	}
	if b != nil && strings.TrimSpace(*b) != "" {
		return b
	}
	return nil
}

// nonEmptyPtr devolve o ponteiro só se tiver conteúdo não vazio; senão nil.
func nonEmptyPtr(s *string) *string {
	if s != nil && strings.TrimSpace(*s) != "" {
		return s
	}
	return nil
}

// normalizePhone tira o +55 do telefone (12–13 dígitos começando com 55), igual
// ao $sz4mb_fmt_phone do portal. Retorna nil se vazio.
func normalizePhone(s *string) *string {
	if s == nil {
		return nil
	}
	d := onlyDigits(*s)
	if d == "" {
		return nil
	}
	if (len(d) == 12 || len(d) == 13) && strings.HasPrefix(d, "55") {
		d = d[2:]
	}
	return &d
}

// formatCPF formata 11 dígitos como 000.000.000-00; devolve o original se não tiver 11.
func formatCPF(cpf string) string {
	d := onlyDigits(cpf)
	if len(d) != 11 {
		return cpf
	}
	return d[0:3] + "." + d[3:6] + "." + d[6:9] + "-" + d[9:11]
}

// buildEndereco monta uma linha de endereço PT-BR a partir dos campos do shipping.
// Retorna nil se não houver nada útil.
//
// MÁSCARA LGPD (achado M4 — Art. 46/9): este endereço sai no rastreio PÚBLICO
// (sem JWT), então o número exato e o complemento são OCULTADOS — eles localizam
// a unidade precisa do titular. O comprador ainda confere a entrega pelo
// logradouro + bairro + cidade/UF + CEP. Formato resultante:
//
//	"Rua X, nº •• — Bairro, Cidade/UF — CEP 00000-000"
//
// (o "nº ••" só aparece quando havia número; o complemento nunca aparece).
func buildEndereco(lograd, num, compl, bairro, cidade, uf, cep *string) *string {
	deref := func(p *string) string {
		if p == nil {
			return ""
		}
		return strings.TrimSpace(*p)
	}
	rua := deref(lograd)
	n := deref(num)
	// compl propositalmente NÃO dereferenciado: o complemento (apto/bloco) é
	// omitido por completo da resposta pública (ver nota de máscara acima).
	_ = compl
	b := deref(bairro)
	cid := deref(cidade)
	u := deref(uf)
	z := deref(cep)

	// Parte 1: "Rua X, nº ••" — número mascarado, complemento omitido.
	var linha1 string
	if rua != "" {
		linha1 = rua
		if n != "" {
			linha1 += ", nº ••"
		}
	}
	// Parte 2: "Bairro, Cidade/UF"
	var linha2 []string
	if b != "" {
		linha2 = append(linha2, b)
	}
	cidUF := cid
	if u != "" {
		if cidUF != "" {
			cidUF += "/" + u
		} else {
			cidUF = u
		}
	}
	if cidUF != "" {
		linha2 = append(linha2, cidUF)
	}

	parts := []string{}
	if linha1 != "" {
		parts = append(parts, linha1)
	}
	if len(linha2) > 0 {
		parts = append(parts, strings.Join(linha2, ", "))
	}
	if z != "" {
		parts = append(parts, "CEP "+z)
	}
	if len(parts) == 0 {
		return nil
	}
	out := strings.Join(parts, " — ")
	return &out
}
