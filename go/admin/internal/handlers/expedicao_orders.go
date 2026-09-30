// Package handlers — lista admin-wide de pedidos de Expedição (frete, não-motoboy).
//
// Espelha go/portal/internal/handlers/expedicao.go (mesma fonte sz_orders, mesmo
// filtro anti-motoboy), mas SEM escopo por produtor (visão global do admin) e com
// coluna extra "produtor_nome" para o operador saber de quem é o pedido.
//
// Este arquivo é só leitura (listagem) — as mutações reais (Aprovar/Cancelar/
// Emitir etiqueta) vivem em order_detail.go, rotas nativas Go sem hook externo.
package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/auth"
	"github.com/senderzz/admin-service/internal/httpx"
)

type ExpedicaoOrdersHandler struct{ Pool *pgxpool.Pool }

// ListOperator expõe a visão global de Expedição para o operador logístico.
// A rota fica no grupo DualAuth; produtor/afiliado não podem alcançar a visão
// cross-producer do admin.
func (h *ExpedicaoOrdersHandler) ListOperator(w http.ResponseWriter, r *http.Request) {
	actor := auth.ActorFromCtx(r.Context())
	if actor == nil || (actor.Kind != auth.ActorAdmin && actor.Kind != auth.ActorKind("operator") && actor.Kind != auth.ActorKind("operador")) {
		httpx.Err(w, http.StatusForbidden, "forbidden", "acesso restrito ao operador logístico")
		return
	}
	h.List(w, r)
}

type expedicaoAdminRow struct {
	ID                int64    `json:"id"`
	WCOrderID         *int64   `json:"wc_order_id"`
	Number            string   `json:"number"`
	Status            string   `json:"status"`
	FinancialStatus   string   `json:"financial_status"`
	ScheduledPayment  string   `json:"scheduled_payment_date"`
	ClienteNome       string   `json:"cliente_nome"`
	ClienteTelefone   string   `json:"cliente_telefone"`
	ClienteCPF        string   `json:"cliente_cpf"`
	CheckoutLinkID    *int64   `json:"checkout_link_id"`
	ProdutorNome      string   `json:"produtor_nome"`
	ProductName       string   `json:"product_name"`
	SenderzzOfferName string   `json:"senderzz_offer_name"`
	AffiliateName     string   `json:"affiliate_name"`
	ShippingName      string   `json:"shipping_name"`
	TrackingCodes     []string `json:"tracking_codes"`
	// TrackingLink — AUDIT-2026-07-31 (dono: "copiar rastreio direciona pro link
	// FALK") — link público assinado (/checkout/rastreio/{code}), mesmo formato
	// que o cliente recebe no ThankYou/webhook. "" se order_number vazio ou SALT ausente.
	TrackingLink string `json:"tracking_link"`
	// LabelError — AUDIT-2026-07-28: motivo real da última falha de emissão
	// (sz_order_meta._sz_label_error), persistido em em_andamento. Vazio
	// quando não há falha pendente (limpo ao emitir com sucesso).
	LabelError       string  `json:"label_error"`
	ShippingTotalRaw float64 `json:"shipping_total_raw"` // frete cobrado (o.shipping)
	ProducerNet      float64 `json:"producer_net"`       // LÍQUIDO = valor do produto, o.subtotal (pedido do dono 2026-07-23: "líquido é o valor do produto")
	AffiliateComm    float64 `json:"affiliate_commission"`
	HasLabel         bool    `json:"has_label"`
	DateMachine      string  `json:"date_machine"`
	DeliveryDate     string  `json:"delivery_date"`
	UpdatedAt        string  `json:"updated_at"`
	DestCEP          string  `json:"dest_cep"`
	DestLogradouro   string  `json:"dest_logradouro"`
	DestNumero       string  `json:"dest_numero"`
	DestComplemento  string  `json:"dest_complemento"`
	DestBairro       string  `json:"dest_bairro"`
	DestCidade       string  `json:"dest_cidade"`
	DestUF           string  `json:"dest_uf"`
}

const listExpedicaoAdminLimit = 500

func (h *ExpedicaoOrdersHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

func (h *ExpedicaoOrdersHandler) columnExists(ctx context.Context, table, column string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			  FROM information_schema.columns
			 WHERE table_schema = 'public'
			   AND table_name = $1
			   AND column_name = $2
		)`, table, column).Scan(&ok)
	return ok
}

// GET /expedicao/orders — visão admin (todos os produtores) dos pedidos de frete.
func (h *ExpedicaoOrdersHandler) List(w http.ResponseWriter, r *http.Request) {
	// Sem cache — dono reportou dado "preso" mesmo após deploy; blinda contra
	// qualquer camada intermediária (CDN/proxy) guardando a resposta JSON.
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_orders") {
		httpx.JSON(w, 200, map[string]any{"items": []expedicaoAdminRow{}, "total": 0})
		return
	}

	hasMeta := h.tableExists(ctx, "sz_order_meta")
	hasItems := h.tableExists(ctx, "sz_order_items")
	hasAddr := h.tableExists(ctx, "sz_order_addresses")
	hasPortalUsers := h.tableExists(ctx, "senderzz_portal_users")
	hasLabels := h.tableExists(ctx, "wc_me_labels")
	hasFinancialStatus := h.columnExists(ctx, "sz_orders", "financial_status") &&
		h.columnExists(ctx, "sz_orders", "scheduled_payment_date")
	financialStatusSel := "''::text AS financial_status"
	scheduledPaymentSel := "NULL::text AS scheduled_payment_date"
	if hasFinancialStatus {
		_, _ = h.Pool.Exec(ctx, `
			UPDATE sz_orders
			   SET financial_status = 'vencido',
			       financial_status_updated_at = NOW(),
			       updated_at = NOW()
			 WHERE financial_status = 'pagamento_agendado'
			   AND scheduled_payment_date < CURRENT_DATE`)
		financialStatusSel = "COALESCE(o.financial_status,'') AS financial_status"
		scheduledPaymentSel = "COALESCE(o.scheduled_payment_date::text,'') AS scheduled_payment_date"
	}

	offerNameSel := "''::text AS offer_name"
	deliveryModeSel := "''::text AS delivery_mode"
	commSel := "0::float AS aff_commission"
	if hasMeta {
		offerNameSel = `COALESCE((SELECT meta_value FROM sz_order_meta
		                 WHERE order_id = o.id AND meta_key='_senderzz_offer_name'
		                 LIMIT 1), '') AS offer_name`
		deliveryModeSel = `COALESCE((SELECT meta_value FROM sz_order_meta
		                   WHERE order_id = o.id AND meta_key='_senderzz_delivery_mode'
		                   LIMIT 1), '') AS delivery_mode`
		commSel = `COALESCE((SELECT CASE WHEN meta_value ~ '^[0-9]+(\.[0-9]+)?$'
		                                 THEN meta_value::numeric ELSE 0 END
		            FROM sz_order_meta
		            WHERE order_id = o.id AND meta_key='_sz_aff_commission'
		            LIMIT 1), 0)::float AS aff_commission`
	}

	produtoSel := "''::text AS produto_nome"
	if hasItems {
		produtoSel = `COALESCE((SELECT nome FROM sz_order_items
		              WHERE order_id = o.id ORDER BY id ASC LIMIT 1), '') AS produto_nome`
	}

	clienteSel := "''::text AS cliente_nome"
	clienteTelSel := "''::text AS cliente_telefone"
	if hasAddr {
		clienteSel = `COALESCE((SELECT nome FROM sz_order_addresses
		              WHERE order_id = o.id
		              ORDER BY CASE WHEN tipo='billing' THEN 0 ELSE 1 END, id ASC
		              LIMIT 1), '') AS cliente_nome`
		clienteTelSel = `COALESCE((SELECT telefone FROM sz_order_addresses
		              WHERE order_id = o.id
		              ORDER BY CASE WHEN tipo='billing' THEN 0 ELSE 1 END, id ASC
		              LIMIT 1), '') AS cliente_telefone`
	}

	// ID do link de checkout que gerou o pedido — AUDIT-2026-07-29 (dono: "coloca
	// pra aparecer o id do checkout"). Mesma junção fiel de orders.go (token em
	// sz_order_meta._senderzz_offer_token → senderzz_checkout_links.token).
	checkoutLinkIDSel := "NULL::bigint AS checkout_link_id"
	if hasMeta && h.tableExists(ctx, "senderzz_checkout_links") {
		checkoutLinkIDSel = `(SELECT cl.id FROM senderzz_checkout_links cl
		                      WHERE cl.token = (SELECT meta_value FROM sz_order_meta
		                                         WHERE order_id = o.id AND meta_key='_senderzz_offer_token'
		                                         LIMIT 1)
		                      LIMIT 1) AS checkout_link_id`
	}

	// CPF do cliente — vive em sz_order_meta (_billing_cpf), não em sz_order_addresses.
	// AUDIT-2026-07-28 (dono: "cadê o CPF do cliente?") — nunca existia nessa
	// tela, só no portal (parity de drawer pedida antes deixou isso exposto).
	clienteCPFSel := "''::text AS cliente_cpf"
	if hasMeta {
		clienteCPFSel = `COALESCE((SELECT meta_value FROM sz_order_meta
		                 WHERE order_id = o.id AND meta_key='_billing_cpf' LIMIT 1), '') AS cliente_cpf`
	}

	afiliadoSel := "''::text AS afiliado_nome"
	produtorSel := "''::text AS produtor_nome"
	if hasPortalUsers {
		afiliadoSel = `COALESCE((SELECT pu.nome FROM senderzz_portal_users pu
		               WHERE pu.wp_user_id = o.affiliate_id OR pu.id = o.affiliate_id
		               ORDER BY CASE WHEN pu.wp_user_id = o.affiliate_id THEN 0 ELSE 1 END
		               LIMIT 1), '') AS afiliado_nome`
		produtorSel = `COALESCE((SELECT pu2.nome FROM senderzz_portal_users pu2
		               WHERE pu2.id = o.produtor_id
		               LIMIT 1), '') AS produtor_nome`
	}

	// Endereço de destino (tipo='shipping', fallback billing) — essencial pro
	// operador de Expedição (é o que muda o transporte), ausente na v1 do endpoint.
	addrCols := []string{"cep", "logradouro", "numero", "complemento", "bairro", "cidade", "uf"}
	destSel := make([]string, len(addrCols))
	for i, c := range addrCols {
		alias := "dest_" + c
		if hasAddr {
			destSel[i] = `COALESCE((SELECT ` + c + ` FROM sz_order_addresses
			              WHERE order_id = o.id
			              ORDER BY CASE WHEN tipo='shipping' THEN 0 ELSE 1 END, id ASC
			              LIMIT 1), '') AS ` + alias
		} else {
			destSel[i] = "''::text AS " + alias
		}
	}

	// Transportadora (carrier): AUDIT-2026-07-28 (dono, print real: pedidos
	// 1657-1661 mostravam "Jadlog" na coluna mas a etiqueta REAL emitida saiu
	// Correios/PAC) — a meta _sz_freight_company é só a COTAÇÃO/intenção do
	// checkout; se o pedido não passou pelo lock corretamente (bug já corrigido
	// no labels-service) ou a ME recusou o serviço cotado, a etiqueta REAL pode
	// sair de outra transportadora. Uma vez que a etiqueta existe,
	// wc_me_labels.service_name é o que FOI DE FATO cobrado/enviado — tem que
	// vencer. Meta só serve de fallback ANTES da etiqueta existir (pedido ainda
	// pendente/aprovado sem emissão).
	// AUDIT-2026-07-28: coluna mostrava o SERVIÇO (PAC/Express/.Package) em vez
	// da EMPRESA (Correios/Jadlog) — service_name é o serviço, company_name (nova
	// coluna, migration 515) é a empresa de fato. Etiquetas emitidas ANTES dessa
	// migration não têm company_name — fallback pro service_name (melhor que nada).
	labelSub := "NULL::text"
	if hasLabels {
		labelSub = `(SELECT COALESCE(NULLIF(l.company_name, ''), l.service_name) FROM wc_me_labels l
		            WHERE l.wc_order_id = COALESCE(o.wp_order_id, o.id)
		              AND l.status <> 'canceled'
		            ORDER BY l.id DESC LIMIT 1)`
	}
	carrierSel := "''::text AS carrier"
	if hasMeta {
		carrierSel = `COALESCE(
		              NULLIF(` + labelSub + `, ''),
		              NULLIF((SELECT meta_value FROM sz_order_meta
		                      WHERE order_id = o.id AND meta_key='_sz_freight_company' LIMIT 1), ''),
		              '') AS carrier`
	} else if hasLabels {
		carrierSel = `COALESCE(` + labelSub + `, '') AS carrier`
	}
	hasLabelSel := "false AS has_label"
	if hasLabels {
		hasLabelSel = `EXISTS(SELECT 1 FROM wc_me_labels l2
		               WHERE l2.wc_order_id = COALESCE(o.wp_order_id, o.id)
		                 AND l2.status <> 'canceled') AS has_label`
	}
	labelErrorSel := "''::text AS label_error"
	if hasMeta {
		labelErrorSel = `COALESCE((SELECT meta_value FROM sz_order_meta
		                 WHERE order_id = o.id AND meta_key = '_sz_label_error' LIMIT 1), '') AS label_error`
	}
	deliveryDateSel := "o.created_at::date::text AS delivery_date"
	if hasMeta {
		deliveryDateSel = `COALESCE(NULLIF((SELECT meta_value FROM sz_order_meta
		                 WHERE order_id = o.id AND meta_key='_sz_delivery_date' LIMIT 1), ''),
		                 o.created_at::date::text) AS delivery_date`
	}
	// status: AUDIT-2026-07-28 (dono: "cancelei e não estornou ainda, está em
	// Alerta, sempre que ocorrer isso deve ir pra Em cancelamento") — pedido
	// cancelado com estorno de etiqueta ainda PENDENTE (não confirmado que o
	// reembolso caiu na ME) mostra 'em_cancelamento' em vez de 'cancelled' puro
	// (que cai no grupo genérico "Alerta"/frustrado/reembolsado). Assim que o
	// estorno confirma (status='confirmado'), volta a mostrar 'cancelled' normal.
	statusSel := "COALESCE(o.status,'') AS status"
	if h.tableExists(ctx, "tpc_transacoes") {
		statusSel = `CASE
		               WHEN lower(regexp_replace(COALESCE(o.status,''), '^wc-', '')) = 'cancelled'
		                AND EXISTS (
		                    SELECT 1 FROM tpc_transacoes t
		                     WHERE t.order_id = COALESCE(o.wp_order_id, o.id)
		                       AND t.tipo = 'credito' AND t.status = 'pendente'
		                       AND t.referencia LIKE 'cancel_label_%'
		                )
		               THEN 'em_cancelamento'
		               ELSE COALESCE(o.status,'')
		             END AS status`
	}

	// Exclusão de pedidos MOTOBOY (Cash on Delivery) pela chave de junção real
	// (idêntica a orders.go admin): sz_motoboy_pedidos.wc_order_id = COALESCE(o.wp_order_id,
	// o.id). Pedidos nativos (sem WC) usam o próprio o.id como surrogate. A checagem
	// antiga por meta/nome de transportadora deixava vazar pedido COD sem etiqueta/meta
	// (55 de 59 pedidos de sz_orders eram COD e apareciam com frete R$0 na Expedição —
	// AUDIT 2026-07-23). O NOT EXISTS abaixo é a fonte de verdade.
	// number = wp_order_id se existir (pedido WC legado), senão o próprio o.id.
	// NUNCA order_number ("SZ-0001634") — esse é identificador interno, não o
	// número exibido ao operador (mesmo padrão de orderLabel() no Cash on Delivery).
	sqlQ := `SELECT o.id, o.wp_order_id,
	                COALESCE(o.wp_order_id::text, o.id::text) AS number,
	                COALESCE(o.order_number,'') AS order_number,
	                ` + statusSel + `,
	                ` + financialStatusSel + `,
	                ` + scheduledPaymentSel + `,
	                ` + clienteSel + `,
	                ` + clienteTelSel + `,
	                ` + clienteCPFSel + `,
	                ` + checkoutLinkIDSel + `,
	                ` + produtorSel + `,
	                ` + produtoSel + `,
	                ` + offerNameSel + `,
	                ` + afiliadoSel + `,
	                ` + carrierSel + `,
	                ` + hasLabelSel + `,
	                ` + labelErrorSel + `,
	                COALESCE(o.shipping,0)::float AS shipping,
	                COALESCE(o.subtotal,0)::float AS producer_net,
	                ` + commSel + `,
	                ` + deliveryModeSel + `,
	                ` + deliveryDateSel + `,
	                o.created_at::text AS created_at,
	                o.updated_at::text AS updated_at,
	                ` + strings.Join(destSel, ",\n\t                ") + `
	         FROM sz_orders o
	         WHERE NOT EXISTS (
	             SELECT 1 FROM sz_motoboy_pedidos mp
	             WHERE mp.wc_order_id = COALESCE(o.wp_order_id, o.id)
	         )
	         ORDER BY delivery_date DESC NULLS LAST, o.created_at DESC, o.id DESC
	         LIMIT $1`

	rows, err := h.Pool.Query(ctx, sqlQ, listExpedicaoAdminLimit+1)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	fetched := 0
	out := []expedicaoAdminRow{}
	for rows.Next() {
		fetched++
		if fetched > listExpedicaoAdminLimit {
			break
		}
		var (
			er           expedicaoAdminRow
			wcOrderID    sql.NullInt64
			offerName    string
			produtoNome  string
			deliveryMode string
			orderNumber  string
		)
		if err := rows.Scan(
			&er.ID, &wcOrderID, &er.Number, &orderNumber, &er.Status, &er.FinancialStatus, &er.ScheduledPayment,
			&er.ClienteNome, &er.ClienteTelefone, &er.ClienteCPF, &er.CheckoutLinkID, &er.ProdutorNome, &produtoNome, &offerName, &er.AffiliateName,
			&er.ShippingName, &er.HasLabel, &er.LabelError,
			&er.ShippingTotalRaw, &er.ProducerNet, &er.AffiliateComm, &deliveryMode,
			&er.DeliveryDate, &er.DateMachine, &er.UpdatedAt,
			&er.DestCEP, &er.DestLogradouro, &er.DestNumero, &er.DestComplemento,
			&er.DestBairro, &er.DestCidade, &er.DestUF,
		); err != nil {
			httpx.Err(w, 500, "db_error", "erro ao ler pedidos: "+err.Error())
			return
		}

		statusPlain := strings.TrimPrefix(strings.ToLower(er.Status), "wc-")
		er.Status = statusPlain

		// Filtra pedidos MOTOBOY — fora da Expedição (mesma regra do portal).
		hay := strings.ToLower(er.ShippingName)
		if strings.EqualFold(deliveryMode, "motoboy") || strings.Contains(hay, "motoboy") {
			continue
		}

		if wcOrderID.Valid {
			v := wcOrderID.Int64
			er.WCOrderID = &v
		}

		er.SenderzzOfferName = offerName
		if offerName != "" {
			er.ProductName = offerName
		} else {
			// Sem _senderzz_offer_name: cai no nome do 1º item. Em kits (o.id tem N
			// itens), cada linha de sz_order_items grava "<kit> — <variante>" (ex.:
			// "3 Egipzya Sérum + 1 Egipzya Espuma — Sérum") — o kit já lista os
			// componentes, então o sufixo " — <variante>" só duplica visualmente
			// (some virava "Sérum...Sérum"). Corta o sufixo, mantém só o nome do kit.
			if i := strings.LastIndex(produtoNome, " — "); i > 0 {
				produtoNome = produtoNome[:i]
			}
			er.ProductName = produtoNome
		}

		// AUDIT-2026-07-28 (dono, print real): coluna Rastreio sempre "—" pra
		// pedido NATIVO Falk (sem WooCommerce, wp_order_id NULL — caso de TODO
		// pedido criado via API) mesmo com tracking_code já preenchido em
		// wc_me_labels. Bug: usava wcOrderID (wp_order_id CRU, null nesses casos)
		// em vez da chave canônica COALESCE(wp_order_id, id) — mesma usada em
		// TODA outra junção com wc_me_labels neste arquivo (labelSub, has_label).
		wcOrderKey := er.ID
		if wcOrderID.Valid {
			wcOrderKey = wcOrderID.Int64
		}
		if hasLabels {
			er.TrackingCodes = h.loadTrackingCodes(ctx, wcOrderKey, statusPlain)
		}
		if er.TrackingCodes == nil {
			er.TrackingCodes = []string{}
		}

		// Loggi/Correios abrem no Melhor Rastreio; os demais continuam usando o
		// rastreio público assinado da Falk.
		er.TrackingLink = melhorRastreioLink(er.ShippingName, er.TrackingCodes)
		if er.TrackingLink == "" {
			er.TrackingLink = publicTrackingLink(orderNumber)
		}

		out = append(out, er)
	}
	if rows.Err() != nil {
		httpx.Err(w, 500, "db_error", "erro ao processar pedidos: "+rows.Err().Error())
		return
	}

	httpx.JSON(w, 200, map[string]any{
		"items":    out,
		"total":    len(out),
		"has_more": fetched > listExpedicaoAdminLimit,
	})
}

func (h *ExpedicaoOrdersHandler) loadTrackingCodes(ctx context.Context, wpOrderID int64, orderStatus string) []string {
	rows, err := h.Pool.Query(ctx,
		`SELECT tracking_code, status FROM wc_me_labels
		  WHERE wc_order_id = $1
		    AND tracking_code IS NOT NULL
		    AND tracking_code != ''
		  ORDER BY id DESC`,
		wpOrderID,
	)
	if err != nil {
		return []string{}
	}
	defer rows.Close()

	activeCodes := []string{}
	canceledCodes := []string{}
	for rows.Next() {
		var code, status string
		if err := rows.Scan(&code, &status); err != nil {
			continue
		}
		code = strings.TrimSpace(code)
		if code == "" {
			continue
		}
		if expedicaoTrackingLabelIsActive(status) {
			activeCodes = append(activeCodes, code)
		} else {
			canceledCodes = append(canceledCodes, code)
		}
	}
	return selectExpedicaoTrackingCodes(activeCodes, canceledCodes, orderStatus)
}

// selectExpedicaoTrackingCodes preserva a etiqueta ativa quando houver. Para
// pedidos já entregues, permite o código histórico da única etiqueta cancelada
// quando não existe substituta ativa — cancelamento não apaga o trajeto real.
func selectExpedicaoTrackingCodes(activeCodes, canceledCodes []string, orderStatus string) []string {
	if len(activeCodes) > 0 {
		return activeCodes
	}
	status := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(orderStatus)), "wc-")
	if status == "entregue" || status == "completo" {
		return canceledCodes
	}
	return []string{}
}

// expedicaoTrackingLabelIsActive é a defesa em profundidade da listagem: a
// consulta já exclui canceladas, mas dados históricos nunca devem voltar à UI.
func expedicaoTrackingLabelIsActive(status string) bool {
	return !strings.EqualFold(strings.TrimSpace(status), "canceled")
}
