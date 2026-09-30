package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/auth"
	"github.com/senderzz/admin-service/internal/httpx"
)

type OrdersHandler struct{ Pool *pgxpool.Pool }

// pedido — linha da tabela sz_motoboy_pedidos enriquecida com dados de clientes,
// produto, afiliado e sz_order_id (necessário para /audit/fix-order/{id}).
type pedido struct {
	ID                      int64   `json:"id"`
	WCOrderID               *int64  `json:"wc_order_id"`
	SzOrderID               *int64  `json:"sz_order_id"` // sz_orders.id — chave para audit-fix
	MotoboyID               *int64  `json:"motoboy_id"`
	Status                  string  `json:"status"`
	FinancialStatus         string  `json:"financial_status"`
	ScheduledPaymentDate    *string `json:"scheduled_payment_date"`
	Valor                   float64 `json:"valor"`
	ComissaoAfiliadoBruta   float64 `json:"comissao_afiliado_bruta"`
	ComissaoAfiliadoLiquida float64 `json:"comissao_afiliado_liquida"`
	ComissaoProdutor        float64 `json:"comissao_produtor"`
	TaxaFalk                float64 `json:"taxa_falk"`
	TaxaFrustracaoAfiliado  float64 `json:"taxa_frustracao_afiliado"`
	TaxaFrustracaoProdutor  float64 `json:"taxa_frustracao_produtor"`
	TaxaMotoboy             float64 `json:"taxa_motoboy"`   // mp.valor_taxa — repasse motoboy
	TaxaFrustrado           float64 `json:"taxa_frustrado"` // mp.valor_taxa_frustrado
	DestNome                string  `json:"dest_nome"`
	DestCEP                 string  `json:"dest_cep"`
	DestCidade              string  `json:"dest_cidade"`
	DestUF                  string  `json:"dest_uf"`
	ClienteNome             string  `json:"cliente_nome"`
	// BAIXO46: telefone do cliente. Fonte fiel = mp.dest_telefone (mesma origem
	// que dest_nome/dest_cep, já usada em bulk_actions.go e motoboy_etiquetas.go).
	ClienteTelefone string `json:"cliente_telefone"`
	Produto         string `json:"produto"`
	// ProdutoQtd — quantidade FÍSICA real do 1º item (fonte de verdade: sz_order_items,
	// mesma fórmula do trigger de estoque em 493-stock-kit-multiplier.sql:
	// quantidade * multiplicador extraído do nome ("3 Datalaprox" -> 3), com
	// fallback pra quantidade pura quando o nome não tem número líder (pedidos
	// antigos guardam a qtd real na coluna, não no texto — ver AUDIT-2026-07-11).
	// NUNCA derivar de oferta_nome (marketing, ex. "3 potes + 1 brinde" != qtd real).
	ProdutoQtd   int64  `json:"produto_qtd"`
	AfiliadoNome string `json:"afiliado_nome"`
	OfertaLink   string `json:"oferta_link"` // senderzz_affiliate_links.link_token (link de afiliado — hoje vazio)
	// Oferta de checkout = link do PRODUTOR que define o preço de venda.
	// Fonte fiel: sz_order_meta._senderzz_offer_* (gravados na conversão) +
	// JOIN por token em senderzz_checkout_links para a URL pública. Ver checkout_links.go.
	OfertaNome  string  `json:"oferta_nome"`  // _senderzz_offer_name
	OfertaValor float64 `json:"oferta_valor"` // _senderzz_offer_value (== checkout_links.display_value)
	OfertaURL   string  `json:"oferta_url"`   // senderzz_checkout_links.url (via token)
	// Variacao — variação REAL do item do pedido = ATRIBUTO DO PRODUTO (regra do dono
	// 2026-06-22, migration 461). Fonte fiel: sz_products.variacao do produto do 1º
	// item do pedido, via join sp.wp_post_id = oi.produto_id (mesma convenção de
	// products.go:356/447 — nos dados importados oi.produto_id é o WP post id, NÃO
	// sz_products.id). Datalaprox='pote' por regra do dono → todos os pedidos exibem
	// "pote". NUNCA derivar de oferta_nome (gerava lixo como "Padrão`"/"Remarketing").
	Variacao  string  `json:"variacao"`
	Comissao  float64 `json:"comissao"`
	CreatedAt string  `json:"created_at"`
	// DeliveryDate — data de entrega (YYYY-MM-DD). Espelha a lógica do go/portal
	// orders.go: meta _sz_delivery_date (sz_order_meta) → fallback
	// sz_motoboy_pedidos.reagendado_para → fallback mp.data_entrega. ADITIVO: campo
	// opcional/nullable; o admin-ui ignora campos extras (não quebra).
	DeliveryDate *string `json:"delivery_date"`
	MotoboyNome  string  `json:"motoboy_nome"`
	DestEndereco string  `json:"dest_endereco"`
	DestNumero   string  `json:"dest_numero"`
	DestComp     string  `json:"dest_complemento"`
	DestBairro   string  `json:"dest_bairro"`
	DestProduto  string  `json:"dest_produto"`
	PackageCode  string  `json:"package_code"`
}

func (h *OrdersHandler) tableExists(ctx context.Context, name string) bool {
	return tableExistsCached(ctx, h.Pool, name) // AUDIT-2026-06-18 Onda2 (go-infoschema-cache)
}

func (h *OrdersHandler) columnExists(ctx context.Context, table, column string) bool {
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

// actorOwnsMotoboyPedido — OWNERSHIP-GATE das MUTAÇÕES de pedido motoboy quando o
// chamador é um token de PORTAL (DualAuth). Resolve o DONO a partir do {id} da rota
// (= sz_motoboy_pedidos.id) → wc_order_id → sz_orders, e compara com a identidade
// AUTENTICADA (auth.ActorFromCtx). NUNCA confia em input do cliente para o escopo.
//
//   - admin / nil          → true (vê e muta tudo — comportamento atual inalterado).
//   - produtor             → true sse sz_orders.produtor_id  = Actor.PortalUserID.
//   - afiliado             → true sse sz_orders.affiliate_id = Actor.WPUserID.
//   - pedido órfão / inexistente / sem sz_orders pai → false (FAIL-CLOSED).
//
// O caller traduz false em 404 unificado (não revela existência de pedido alheio),
// ANTES de qualquer escrita. O JOIN colapsa os dois hops (mp→sz_orders) numa query.
func (h *OrdersHandler) actorOwnsMotoboyPedido(ctx context.Context, mbPedidoID int64) bool {
	actor := auth.ActorFromCtx(ctx)
	if actor == nil || actor.Kind == auth.ActorAdmin {
		return true // admin (ou rota sem DualAuth) — sem escopo.
	}
	// Operador logístico: mesma visão irrestrita do admin (espelha o ListMotoboy,
	// linha ~389 — "vê tudo, mesma visão do admin"). Antes caía no default abaixo
	// e era barrado (fail-closed) mesmo enxergando o pedido na lista — inconsistente:
	// via o pedido mas não conseguia reagendar/cancelar.
	if actor.Kind == auth.ActorKind("operator") || actor.Kind == auth.ActorKind("operador") {
		return true
	}
	var produtorID, affiliateID *int64
	err := h.Pool.QueryRow(ctx,
		`SELECT o.produtor_id, o.affiliate_id
		   FROM sz_motoboy_pedidos mp
		   JOIN sz_orders o ON COALESCE(o.wp_order_id, o.id) = mp.wc_order_id
		  WHERE mp.id = $1`, mbPedidoID).Scan(&produtorID, &affiliateID)
	if err != nil {
		// Sem linha sz_orders pai (clone órfão), pedido inexistente, ou sz_orders não
		// migrada: não há como PROVAR propriedade → fail-closed (nega ao portal).
		return false
	}
	switch actor.Kind {
	case auth.ActorProdutor:
		return produtorID != nil && *produtorID == actor.PortalUserID
	case auth.ActorAfiliado:
		return affiliateID != nil && *affiliateID == actor.WPUserID
	default:
		// Demais roles sem escopo definido nestas telas: fail-closed.
		return false
	}
}

// ListMotoboy — lista pedidos sz_motoboy_pedidos com filtros:
//   - status: status do pedido
//   - date: data de criação exata (YYYY-MM-DD) — legado
//   - data_ini / data_fim: intervalo de criação (YYYY-MM-DD, inclusivo) — MED14
//   - cidade: substring case-insensitive em dest_cidade
//   - s: busca por wc_order_id numérico, dest_nome, produto ou afiliado ILIKE — MED16
//   - stopped: "1" → pedidos parados há 24h+ em status operacional
//   - limit: máx 200, default 50
//
// GET /orders/motoboy
func (h *OrdersHandler) ListMotoboy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	status := q.Get("status")
	date := q.Get("date")
	// MED14: o front envia data_ini/data_fim (range), não 'date'. Ler ambos.
	dataIni := strings.TrimSpace(q.Get("data_ini"))
	dataFim := strings.TrimSpace(q.Get("data_fim"))
	cidade := q.Get("cidade")
	search := q.Get("s")
	stopped := q.Get("stopped") == "1" || q.Get("stopped") == "true"

	// JOIN opcional com sz_orders para resolver sz_order_id (auditoria), afiliado e produto.
	hasSzOrders := h.tableExists(ctx, "sz_orders")
	hasPortalUsers := h.tableExists(ctx, "senderzz_portal_users")

	// Monta SELECT dinâmico conforme tabelas disponíveis.
	//
	// Campos base: sempre de sz_motoboy_pedidos.
	// Campos enriquecidos: via LEFT JOIN quando tabelas existem.

	szOrderSel := "NULL::bigint AS sz_order_id"
	szOrderJoin := ""
	afiliadoSel := "''::text AS afiliado_nome"
	produtoSel := "''::text AS produto"
	produtoQtdSel := "1::bigint AS produto_qtd"
	ofertaSel := "''::text AS oferta_link"
	// Oferta de checkout (preço de venda do produtor): nome + valor vêm direto de
	// sz_order_meta (sem cast — o valor está literalmente no pedido); a URL vem de
	// senderzz_checkout_links via JOIN por token (varchar=varchar, sem ::bigint).
	ofertaNomeSel := "''::text AS oferta_nome"
	ofertaValorSel := "0::float AS oferta_valor"
	ofertaURLSel := "''::text AS oferta_url"
	// Variação = atributo do PRODUTO do 1º item (migration 461). Default literal
	// vazio; só vira subquery quando sz_orders+sz_order_items+sz_products existem
	// (precisa de o.id, que só existe com hasSzOrders / szOrderJoin).
	variacaoSel := "''::text AS variacao"

	// Motoboy nome — LEFT JOIN sempre (sz_motoboys existe na stack).
	hasMotoboys := h.tableExists(ctx, "sz_motoboys")
	motoboyJoin := ""
	motoboyNomeSel := "''::text AS motoboy_nome"
	if hasMotoboys {
		motoboyJoin = "LEFT JOIN sz_motoboys m ON m.id = mp.motoboy_id"
		motoboyNomeSel = "COALESCE(m.nome,'') AS motoboy_nome"
	}

	// Data de entrega — espelha go/portal/internal/handlers/orders.go (deliverySel):
	// meta _sz_delivery_date (sz_order_meta, keyed por o.id) → fallback
	// mp.reagendado_para → fallback mp.data_entrega. Default sem sz_orders: usa só as
	// colunas de mp (sempre presentes). Subquery escalar, sem fan-out. ::text → YYYY-MM-DD.
	deliverySel := "COALESCE(mp.reagendado_para, mp.data_entrega)::text AS delivery_date"
	deliveryOrderExpr := "COALESCE(mp.reagendado_para, mp.data_entrega)"

	// MED16: expressões "nuas" (sem alias) reutilizadas no WHERE de busca por
	// produto/afiliado. Default = literal vazio: o predicado OR fica inerte
	// quando as tabelas-fonte não existem (mesma guarda das colunas do SELECT).
	produtoExpr := "''::text"
	afiliadoExpr := "''::text"

	if hasSzOrders {
		szOrderSel = "o.id AS sz_order_id"
		szOrderJoin = "LEFT JOIN sz_orders o ON COALESCE(o.wp_order_id, o.id) = mp.wc_order_id"

		// Produto: primeiro item de sz_order_items via subquery escalar (sem fan-out).
		hasOrderItems := h.tableExists(ctx, "sz_order_items")
		if hasOrderItems {
			produtoExpr = "COALESCE((SELECT nome FROM sz_order_items WHERE order_id = o.id ORDER BY id ASC LIMIT 1), '')"
			produtoSel = produtoExpr + " AS produto"

			// Quantidade física real do 1º item. Em oferta simples legada, o nome
			// pode carregar o multiplicador ("3 Datalaprox") enquanto a coluna
			// quantidade vale 1. Em combo, porém, a coluna já guarda a quantidade
			// da linha (ex.: 3) e o nome completo também começa com "3" — não
			// multiplicar duas vezes (3 × 3 = 9).
			produtoQtdSel = `COALESCE((
			                   SELECT CASE
			                            WHEN position(' + ' IN nome) > 0 THEN quantidade
			                            ELSE quantidade * COALESCE(NULLIF(substring(nome from '^(\d+)\s'), '')::integer, 1)
			                          END
			                     FROM sz_order_items WHERE order_id = o.id ORDER BY id ASC LIMIT 1
			                 ), 1)::bigint AS produto_qtd`

			// Variação: atributo do PRODUTO do 1º item (migration 461). Join
			// sp.wp_post_id = oi.produto_id (convenção de products.go:356/447 —
			// oi.produto_id guarda o WP post id, não sz_products.id). Subquery
			// escalar (sem fan-out); LIMIT 1 evita erro de "more than one row"
			// quando há vários itens. Mesma ordenação "1º item" do produtoExpr.
			if h.tableExists(ctx, "sz_products") {
				variacaoSel = `COALESCE((SELECT sp.variacao
				                FROM sz_order_items oi
				                JOIN sz_products sp ON sp.wp_post_id = oi.produto_id
				                WHERE oi.order_id = o.id
				                ORDER BY oi.id ASC LIMIT 1), '') AS variacao`
			}
		}

		// Afiliado: subquery escalar com prioridade wp_user_id > id para evitar
		// fan-out de um LEFT JOIN com condição OR (dois usuários podem ter ids cruzados).
		if hasPortalUsers {
			afiliadoExpr = `COALESCE((SELECT pu.nome FROM senderzz_portal_users pu
			               WHERE pu.wp_user_id = o.affiliate_id OR pu.id = o.affiliate_id
			               ORDER BY CASE WHEN pu.wp_user_id = o.affiliate_id THEN 0 ELSE 1 END
			               LIMIT 1), '')`
			afiliadoSel = afiliadoExpr + " AS afiliado_nome"
		}

		// Oferta (link_token): pega o link ativo associado ao vínculo do afiliado com o produtor.
		// Subquery escalar para evitar fan-out (afiliado pode ter múltiplos links).
		if h.tableExists(ctx, "senderzz_affiliate_links") && h.tableExists(ctx, "senderzz_affiliates") {
			ofertaSel = `COALESCE((
			              SELECT al.link_token
			              FROM senderzz_affiliate_links al
			              JOIN senderzz_affiliates sa ON sa.id = al.affiliate_id
			              WHERE sa.afiliado_id = o.affiliate_id
			                AND (o.produtor_id IS NULL OR sa.produtor_id = o.produtor_id)
			              ORDER BY al.active DESC, al.id ASC
			              LIMIT 1), '') AS oferta_link`
		}

		// Oferta de checkout (PREÇO de venda) — fonte fiel migrada do WP.
		// Nome e valor lidos direto de sz_order_meta (chaveado por o.id, sem cast).
		if h.tableExists(ctx, "sz_order_meta") {
			ofertaNomeSel = `COALESCE((SELECT meta_value FROM sz_order_meta
			                  WHERE order_id = o.id AND meta_key='_senderzz_offer_name'
			                  LIMIT 1), '') AS oferta_nome`
			// _senderzz_offer_value é numérico; guarda regex evita 500 caso uma meta
			// venha não-numérica (cast-safe — não derruba a listagem inteira).
			ofertaValorSel = `COALESCE((SELECT CASE WHEN meta_value ~ '^[0-9]+(\.[0-9]+)?$'
			                                        THEN meta_value::numeric ELSE 0 END
			                   FROM sz_order_meta
			                   WHERE order_id = o.id AND meta_key='_senderzz_offer_value'
			                   LIMIT 1), 0)::float AS oferta_valor`

			// URL pública do link: JOIN por token (varchar=varchar). 100% fiel (token presente em 38/38).
			if h.tableExists(ctx, "senderzz_checkout_links") {
				ofertaURLSel = `COALESCE((SELECT cl.url FROM senderzz_checkout_links cl
				                 WHERE cl.token = (SELECT meta_value FROM sz_order_meta
				                                    WHERE order_id = o.id AND meta_key='_senderzz_offer_token'
				                                    LIMIT 1)
				                 LIMIT 1), '') AS oferta_url`
			}

			// Data de entrega com sz_orders disponível: meta _sz_delivery_date (keyed por
			// o.id) vence; senão cai nas colunas de mp (reagendado_para → data_entrega).
			// NULLIF('') evita meta vazia mascarar o fallback. Espelha go/portal orders.go.
			deliverySel = `COALESCE(
			    NULLIF((SELECT meta_value FROM sz_order_meta
			            WHERE order_id = o.id AND meta_key='_sz_delivery_date' LIMIT 1), ''),
			    mp.reagendado_para::text,
			    mp.data_entrega::text
			) AS delivery_date`
			deliveryOrderExpr = `COALESCE(
			    (SELECT CASE WHEN meta_value ~ '^\\d{4}-\\d{2}-\\d{2}$' THEN meta_value::date END
			       FROM sz_order_meta
			      WHERE order_id = o.id AND meta_key='_sz_delivery_date' LIMIT 1),
			    mp.reagendado_para,
			    mp.data_entrega
			)`
		}
	}

	// Constrói cláusulas WHERE dinâmicas.
	args := []any{}
	wheres := []string{}
	hasOrderFinancials := h.tableExists(ctx, "sz_order_financials")
	hasMotoboyFinancialStatus := h.columnExists(ctx, "sz_motoboy_pedidos", "financial_status") &&
		h.columnExists(ctx, "sz_motoboy_pedidos", "scheduled_payment_date")
	financialStatusSel := "''::text AS financial_status"
	scheduledPaymentSel := "NULL::text AS scheduled_payment_date"
	if hasMotoboyFinancialStatus {
		_, _ = h.Pool.Exec(ctx, `
			UPDATE sz_motoboy_pedidos
			   SET financial_status = 'vencido',
			       financial_status_updated_at = NOW()
			 WHERE financial_status = 'pagamento_agendado'
			   AND scheduled_payment_date < CURRENT_DATE`)
		financialStatusSel = "COALESCE(mp.financial_status,'') AS financial_status"
		scheduledPaymentSel = "mp.scheduled_payment_date::text AS scheduled_payment_date"
	}

	if stopped {
		// Parado: status operacional sem atualização há 24h+.
		wheres = append(wheres, "mp.status IN ('pendente','agendado','embalado','em_rota')")
		wheres = append(wheres, "mp.updated_at < NOW() - INTERVAL '24 hours'")
	} else {
		if status != "" {
			switch status {
			case "__logistico":
				wheres = append(wheres, "mp.status IN ('pre_agendado','agendado','embalado','em_rota','a_caminho')")
			case "__entregues":
				wheres = append(wheres, "mp.status = 'entregue'")
				if hasMotoboyFinancialStatus {
					wheres = append(wheres, "COALESCE(mp.financial_status,'') = ''")
				}
			case "__fin_pagamento_agendado":
				if hasMotoboyFinancialStatus {
					wheres = append(wheres, "mp.financial_status = 'pagamento_agendado'")
				} else {
					wheres = append(wheres, "FALSE")
				}
			case "__fin_vencido":
				if hasMotoboyFinancialStatus {
					wheres = append(wheres, "mp.financial_status = 'vencido'")
				} else {
					wheres = append(wheres, "FALSE")
				}
			case "__fin_concluido":
				if hasMotoboyFinancialStatus {
					wheres = append(wheres, "mp.financial_status = 'concluido'")
				} else {
					wheres = append(wheres, "FALSE")
				}
			default:
				args = append(args, status)
				wheres = append(wheres, "mp.status = $"+strconv.Itoa(len(args)))
			}
		}
		// 'date' (match exato) — legado, mantido por compatibilidade.
		if date != "" {
			args = append(args, date)
			wheres = append(wheres, "(mp.created_at AT TIME ZONE 'America/Sao_Paulo')::date = $"+strconv.Itoa(len(args))+"::date")
		}
		// MED14: data_ini/data_fim — intervalo inclusivo (espelha pix.go:83-85).
		if dataIni != "" {
			args = append(args, dataIni)
			wheres = append(wheres, "mp.created_at >= ($"+strconv.Itoa(len(args))+"::date)::timestamp AT TIME ZONE 'America/Sao_Paulo'")
		}
		if dataFim != "" {
			args = append(args, dataFim)
			wheres = append(wheres, "mp.created_at <= (($"+strconv.Itoa(len(args))+"::date + 1)::timestamp AT TIME ZONE 'America/Sao_Paulo' - interval '1 second')")
		}
		if cidade != "" {
			args = append(args, "%"+strings.ToLower(cidade)+"%")
			wheres = append(wheres, "LOWER(COALESCE(mp.dest_cidade,'')) LIKE $"+strconv.Itoa(len(args)))
		}
		if search != "" {
			// MED16: busca casa o placeholder "Pedido / produto / afiliado / nome".
			// LIKE em dest_nome + produto + afiliado (expressões nuas, idênticas às
			// do SELECT; inertes quando a tabela-fonte não existe). Match exato extra
			// por wc_order_id quando o termo é numérico.
			args = append(args, "%"+strings.ToLower(search)+"%")
			likeArg := "$" + strconv.Itoa(len(args))
			parts := []string{
				"LOWER(COALESCE(mp.dest_nome,'')) LIKE " + likeArg,
				"LOWER(" + produtoExpr + ") LIKE " + likeArg,
				"LOWER(" + afiliadoExpr + ") LIKE " + likeArg,
			}
			if wcID, errS := strconv.ParseInt(search, 10, 64); errS == nil && wcID > 0 {
				args = append(args, wcID)
				parts = append(parts, "mp.wc_order_id = $"+strconv.Itoa(len(args)))
			}
			wheres = append(wheres, "("+strings.Join(parts, " OR ")+")")
		}
		// Filtros ricos enviados pelo front (Orders.tsx:250-252): uf/produto/afiliado.
		// uf: match exato case-insensitive em mp.dest_uf (código de 2 letras vindo de
		// get_shipping_state()/get_billing_state() — router.php:493).
		if uf := strings.TrimSpace(q.Get("uf")); uf != "" {
			args = append(args, strings.ToLower(uf))
			wheres = append(wheres, "LOWER(COALESCE(mp.dest_uf,'')) = $"+strconv.Itoa(len(args)))
		}
		// produto/afiliado: ILIKE substring sobre as MESMAS expressões nuas do SELECT
		// (produtoExpr/afiliadoExpr). São subqueries escalares SEM placeholder, então
		// entram no WHERE sem perturbar a numeração. Default ''::text → predicado inerte
		// quando as tabelas-fonte não existem (idêntico ao bloco de busca acima).
		if produto := strings.TrimSpace(q.Get("produto")); produto != "" {
			args = append(args, "%"+strings.ToLower(produto)+"%")
			wheres = append(wheres, "LOWER("+produtoExpr+") LIKE $"+strconv.Itoa(len(args)))
		}
		if afiliado := strings.TrimSpace(q.Get("afiliado")); afiliado != "" {
			args = append(args, "%"+strings.ToLower(afiliado)+"%")
			wheres = append(wheres, "LOWER("+afiliadoExpr+") LIKE $"+strconv.Itoa(len(args)))
		}
	}

	// FEAT-RBAC-ORDERS-2026-06-24 — ESCOPO POR PAPEL (DualAuth).
	// Identidade AUTENTICADA via auth.ActorFromCtx (NUNCA query/body do cliente).
	//   - admin/nil    → sem escopo (vê tudo — comportamento atual inalterado).
	//   - produtor     → o.produtor_id = <portalUserID>   (id canônico do produtor).
	//   - afiliado     → o.affiliate_id = <wpUserID>       (sz_orders guarda o wp_user_id).
	// Equality estrita (nunca OR/IN) — idêntico ao escopo do go/portal motoboy_portal.go.
	// A coluna vive em sz_orders (alias o), só disponível com o LEFT JOIN szOrderJoin.
	// Sem sz_orders não há como provar propriedade → lista vazia (fail-closed) p/ portal.
	if actor := auth.ActorFromCtx(ctx); actor != nil {
		switch actor.Kind {
		case auth.ActorAdmin:
			// Admin vê tudo por padrão, mas pode filtrar por produtor específico
			// (?produtor_id=N) — usado pela tela Carteiras/Clientes ("ver pedidos
			// deste produtor"). Sem risco de escopo: admin já vê tudo, isto só
			// restringe a VISÃO dele mesmo, nunca amplia o de produtor/afiliado.
			if hasSzOrders {
				if pid, err := strconv.ParseInt(strings.TrimSpace(q.Get("produtor_id")), 10, 64); err == nil && pid > 0 {
					args = append(args, pid)
					wheres = append(wheres, "o.produtor_id = $"+strconv.Itoa(len(args)))
				}
			}
		case auth.ActorProdutor:
			if !hasSzOrders {
				httpx.JSON(w, 200, map[string]any{"items": []pedido{}})
				return
			}
			args = append(args, actor.PortalUserID)
			wheres = append(wheres, "o.produtor_id = $"+strconv.Itoa(len(args)))
		case auth.ActorAfiliado:
			if !hasSzOrders {
				// Mesmo com lista vazia, sinaliza o papel p/ o front esconder a coluna Afiliado.
				httpx.JSON(w, 200, map[string]any{"items": []pedido{}, "viewer_is_affiliate": true})
				return
			}
			args = append(args, actor.WPUserID)
			wheres = append(wheres, "o.affiliate_id = $"+strconv.Itoa(len(args)))
		case auth.ActorKind("operator"), auth.ActorKind("operador"):
			// Operador logístico: vê tudo, mesma visão do admin (sem filtro de escopo).
		default:
			// Demais roles de portal sem escopo nestas telas: fail-closed.
			httpx.JSON(w, 200, map[string]any{"items": []pedido{}})
			return
		}
	}

	whereSQL := ""
	if len(wheres) > 0 {
		whereSQL = "WHERE " + strings.Join(wheres, " AND ")
	}

	args = append(args, limit)
	limitArg := "$" + strconv.Itoa(len(args))

	// Coluna de comissão: de sz_orders se disponível, senão 0.
	comissaoSel := "0::float AS comissao"
	if hasSzOrders {
		comissaoSel = "COALESCE(o.affiliate_amount, 0) AS comissao"
	}

	// taxa_frustrado: valor_taxa_frustrado como primário; fallback em affiliate_transactions quando 0.
	taxaFrustSel := `COALESCE(mp.valor_taxa_frustrado,0)`
	if h.tableExists(ctx, "senderzz_affiliate_transactions") {
		taxaFrustSel = `COALESCE(NULLIF(mp.valor_taxa_frustrado,0),(SELECT at.amount FROM senderzz_affiliate_transactions at WHERE at.order_id=mp.wc_order_id AND at.type='penalty' LIMIT 1),0)`
	}

	taxaFrustAfiliadoSel := `0::float AS taxa_frustracao_afiliado`
	taxaFrustProdutorSel := `0::float AS taxa_frustracao_produtor`
	taxaFrustAfiliadoExpr := `0::float`
	taxaFrustProdutorExpr := `0::float`

	financialJoin := ""
	comissaoAfiliadoBrutaSel := "0::float AS comissao_afiliado_bruta"
	comissaoAfiliadoLiquidaSel := "0::float AS comissao_afiliado_liquida"
	comissaoProdutorSel := "0::float AS comissao_produtor"
	taxaFalkSel := "0::float AS taxa_falk"
	if hasOrderFinancials {
		financialJoin = "LEFT JOIN sz_order_financials f ON f.order_id = o.id"
		comissaoAfiliadoBrutaSel = `COALESCE(f.affiliate_bruta, 0)::float AS comissao_afiliado_bruta`
		comissaoAfiliadoLiquidaSel = `COALESCE(f.affiliate_liquida, o.affiliate_amount, 0)::float AS comissao_afiliado_liquida`
		comissaoProdutorSel = `COALESCE(f.producer_net_live, NULLIF(o.producer_net,0), (COALESCE(o.total,0) - COALESCE(o.affiliate_amount,0) - COALESCE(o.delivery_fee,0) - COALESCE(o.transaction_fee,0)), 0)::float AS comissao_produtor`
	}

	if hasSzOrders {
		taxaFrustAfiliadoExpr = `COALESCE(
			(SELECT at.amount
			   FROM senderzz_affiliate_transactions at
			  WHERE at.order_id = o.id
			    AND at.type = 'penalty'
			  ORDER BY at.id ASC
			  LIMIT 1),
			(SELECT CASE WHEN meta_value ~ '^[0-9]+(\.[0-9]+)?$'
			             THEN meta_value::numeric ELSE 0 END
			   FROM sz_order_meta
			  WHERE order_id = o.id
			    AND meta_key = '_sz_aff_frustration_penalty'
			  LIMIT 1),
			0
		)`
		taxaFrustProdutorExpr = `COALESCE(
			(SELECT CASE WHEN meta_value ~ '^[0-9]+(\.[0-9]+)?$'
			             THEN meta_value::numeric ELSE 0 END
			   FROM sz_order_meta
			  WHERE order_id = o.id
			    AND meta_key = '_sz_prod_frustration_penalty'
			  LIMIT 1),
			0
		)`
		taxaFrustAfiliadoSel = taxaFrustAfiliadoExpr + `::float AS taxa_frustracao_afiliado`
		taxaFrustProdutorSel = taxaFrustProdutorExpr + `::float AS taxa_frustracao_produtor`
	}

	if hasOrderFinancials {
		taxaFalkSel = `CASE
			WHEN COALESCE(o.status,'') IN ('frustrado', 'reembolsado') THEN
				(COALESCE(f.delivery_fee, 0)
				 + COALESCE(f.affiliate_take, 0)
				 + COALESCE(f.producer_take, 0)
				 + ` + taxaFrustAfiliadoExpr + `
				 + ` + taxaFrustProdutorExpr + `)
			ELSE
				(COALESCE(f.delivery_fee, 0)
				 + COALESCE(f.affiliate_take, 0)
				 + COALESCE(f.producer_take, 0))
		END::float AS taxa_falk`
	}

	if !hasOrderFinancials && hasSzOrders {
		taxaFalkSel = `CASE
			WHEN COALESCE(o.status,'') IN ('frustrado', 'reembolsado') THEN
				(` + taxaFrustAfiliadoExpr + ` + ` + taxaFrustProdutorExpr + `)
			ELSE 0
		END::float AS taxa_falk`
	}

	sqlQ := `SELECT mp.id, mp.wc_order_id, ` + szOrderSel + `,
	                mp.motoboy_id, COALESCE(mp.status,''), COALESCE(mp.valor_pedido,0),
	                ` + financialStatusSel + `,
	                ` + scheduledPaymentSel + `,
	                ` + comissaoAfiliadoBrutaSel + `,
	                ` + comissaoAfiliadoLiquidaSel + `,
	                ` + comissaoProdutorSel + `,
	                ` + taxaFalkSel + `,
	                ` + taxaFrustAfiliadoSel + `,
	                ` + taxaFrustProdutorSel + `,
	                COALESCE(mp.valor_taxa,0), ` + taxaFrustSel + `,
	                COALESCE(mp.dest_nome,''), COALESCE(mp.dest_cep,''),
	                COALESCE(mp.dest_cidade,''), COALESCE(mp.dest_uf,''),
	                COALESCE(mp.dest_nome,'') AS cliente_nome,
	                COALESCE(mp.dest_telefone,'') AS cliente_telefone,
	                ` + produtoSel + `, ` + produtoQtdSel + `, ` + afiliadoSel + `, ` + ofertaSel + `,
	                ` + ofertaNomeSel + `, ` + ofertaValorSel + `, ` + ofertaURLSel + `,
	                ` + variacaoSel + `, ` + comissaoSel + `,
	                mp.created_at::text,
	                ` + deliverySel + `,
	                ` + motoboyNomeSel + `,
	                COALESCE(mp.dest_endereco,''), COALESCE(mp.dest_numero,''),
	                COALESCE(mp.dest_complemento,''), COALESCE(mp.dest_bairro,''),
	                COALESCE(mp.dest_produto,'')
	         FROM sz_motoboy_pedidos mp
	         ` + szOrderJoin + `
	         ` + financialJoin + `
	         ` + motoboyJoin + `
	         ` + whereSQL + `
	         ORDER BY ` + deliveryOrderExpr + ` DESC NULLS LAST, mp.created_at DESC LIMIT ` + limitArg

	rows, err := h.Pool.Query(ctx, sqlQ, args...)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []pedido{}
	for rows.Next() {
		var p pedido
		var szOrdID sql.NullInt64
		var deliveryDate sql.NullString
		var scheduledPaymentDate sql.NullString
		_ = rows.Scan(
			&p.ID, &p.WCOrderID, &szOrdID,
			&p.MotoboyID, &p.Status, &p.Valor,
			&p.FinancialStatus,
			&scheduledPaymentDate,
			&p.ComissaoAfiliadoBruta,
			&p.ComissaoAfiliadoLiquida,
			&p.ComissaoProdutor,
			&p.TaxaFalk,
			&p.TaxaFrustracaoAfiliado,
			&p.TaxaFrustracaoProdutor,
			&p.TaxaMotoboy, &p.TaxaFrustrado,
			&p.DestNome, &p.DestCEP,
			&p.DestCidade, &p.DestUF, &p.ClienteNome,
			&p.ClienteTelefone,
			&p.Produto, &p.ProdutoQtd, &p.AfiliadoNome, &p.OfertaLink,
			&p.OfertaNome, &p.OfertaValor, &p.OfertaURL,
			&p.Variacao, &p.Comissao,
			&p.CreatedAt,
			&deliveryDate,
			&p.MotoboyNome,
			&p.DestEndereco, &p.DestNumero, &p.DestComp, &p.DestBairro, &p.DestProduto,
		)
		if szOrdID.Valid {
			p.SzOrderID = &szOrdID.Int64
		}
		if deliveryDate.Valid && deliveryDate.String != "" {
			d := deliveryDate.String
			p.DeliveryDate = &d
		}
		if scheduledPaymentDate.Valid && scheduledPaymentDate.String != "" {
			d := scheduledPaymentDate.String
			p.ScheduledPaymentDate = &d
		}
		if p.WCOrderID != nil && p.Status == "embalado" {
			p.PackageCode = motoboyPackageCode(p.ID, *p.WCOrderID)
		}
		out = append(out, p)
	}

	// viewer_is_affiliate — papel do viewer no ENVELOPE (ADITIVO; admin-ui ignora).
	// Permite ao front esconder a coluna Afiliado quando o viewer é AFILIADO.
	// Identidade AUTENTICADA via auth.ActorFromCtx (NUNCA query/body). Admin/produtor/nil = false.
	viewerIsAffiliate := false
	if actor := auth.ActorFromCtx(ctx); actor != nil && actor.Kind == auth.ActorAfiliado {
		viewerIsAffiliate = true
	}

	httpx.JSON(w, 200, map[string]any{
		"items":               out,
		"viewer_is_affiliate": viewerIsAffiliate,
	})
}

// AuditFix executa correção financeira de um pedido motoboy.
// O {id} na URL é sz_motoboy_pedidos.id; resolve sz_orders.id via wc_order_id antes de corrigir.
// POST /orders/motoboy/{id}/audit-fix
func (h *OrdersHandler) AuditFix(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	mbPedidoID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || mbPedidoID <= 0 {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	ctx := r.Context()

	if !h.tableExists(ctx, "sz_motoboy_pedidos") {
		httpx.Err(w, 503, "tables_missing", "sz_motoboy_pedidos ainda não migrada")
		return
	}

	// Resolve wc_order_id → sz_orders.id
	var wcOrderID sql.NullInt64
	_ = h.Pool.QueryRow(ctx,
		`SELECT wc_order_id FROM sz_motoboy_pedidos WHERE id = $1 LIMIT 1`, mbPedidoID,
	).Scan(&wcOrderID)

	if !wcOrderID.Valid || wcOrderID.Int64 <= 0 {
		httpx.Err(w, 404, "not_found", "pedido motoboy não encontrado ou sem wc_order_id")
		return
	}

	if !h.tableExists(ctx, "sz_orders") {
		httpx.Err(w, 503, "tables_missing", "sz_orders ainda não migrada")
		return
	}

	var szOrderID sql.NullInt64
	_ = h.Pool.QueryRow(ctx,
		`SELECT id FROM sz_orders WHERE COALESCE(wp_order_id, id) = $1 LIMIT 1`, wcOrderID.Int64,
	).Scan(&szOrderID)

	if !szOrderID.Valid || szOrderID.Int64 <= 0 {
		httpx.Err(w, 404, "not_found", "sz_orders sem linha para wp_order_id correspondente")
		return
	}

	orderID := szOrderID.Int64
	res := map[string]int{"missing_inserted": 0, "bad_transaction_updated": 0, "producer_wallet_inserted": 0, "producer_wallet_updated": 0}

	if h.tableExists(ctx, "senderzz_affiliate_transactions") {
		// BUG-FIX 2026-07-14: senderzz_affiliate_transactions.affiliate_id é o id
		// do VÍNCULO (senderzz_affiliates.id) — espelha o contrato documentado em
		// go/portal/internal/handlers/affiliate_wallet_portal.go:16 e o INSERT real
		// de go/motoboy/internal/handlers/status_bridge.go. A versão antiga gravava
		// o.affiliate_id CRU (que é wp_user_id do afiliado) — outro namespace —
		// corrompendo a leitura (JOIN sa.id = tx.affiliate_id nunca acha o vínculo
		// certo, ou pior, acha um vínculo de OUTRO afiliado cujo id coincide com
		// esse wp_user_id). Fix: junta senderzz_affiliates pra resolver o id certo.
		tag, e := h.Pool.Exec(ctx,
			`INSERT INTO senderzz_affiliate_transactions
			   (order_id, affiliate_id, type, status, amount, available_at, meta_json, created_at)
			 SELECT o.id, sa.id, 'commission', 'pending',
			        COALESCE(o.affiliate_amount,0),
			        NOW() + INTERVAL '1 day' * COALESCE(o.retention_days, 7),
			        jsonb_build_object('source','admin_audit_fix_order'),
			        NOW()
			 FROM sz_orders o
			 JOIN senderzz_affiliates sa
			   ON sa.afiliado_id = o.affiliate_id AND sa.produtor_id = o.produtor_id
			 LEFT JOIN senderzz_affiliate_transactions t
			   ON t.order_id = o.id AND t.type='commission' AND t.status <> 'cancelled'
			 WHERE o.id = $1
			   AND COALESCE(o.affiliate_id,0) > 0
			   AND COALESCE(o.affiliate_amount,0) > 0
			   AND t.id IS NULL`, orderID)
		if e == nil {
			res["missing_inserted"] = int(tag.RowsAffected())
		}

		tag, e = h.Pool.Exec(ctx,
			`UPDATE senderzz_affiliate_transactions t
			 SET amount = o.affiliate_amount,
			     meta_json = COALESCE(meta_json, '{}'::jsonb) || jsonb_build_object('source','admin_audit_fix_order','prev_amount', t.amount),
			     updated_at = NOW()
			 FROM sz_orders o
			 WHERE t.order_id = o.id
			   AND t.type='commission' AND t.status <> 'cancelled'
			   AND o.id = $1
			   AND ABS(COALESCE(o.affiliate_amount,0) - COALESCE(t.amount,0)) > 0.01`, orderID)
		if e == nil {
			res["bad_transaction_updated"] = int(tag.RowsAffected())
		}
	}

	if h.tableExists(ctx, "sz_cod_wallet_transactions") {
		// BUG-FIX 2026-07-14: type='credit' nunca existiu na tabela (real='cod_received') —
		// esse UPDATE nunca tocava nenhuma linha. Ver AUDIT-2026-07-14 (audit.go).
		tag, e := h.Pool.Exec(ctx,
			`INSERT INTO sz_cod_wallet_transactions
			   (user_id, order_id, type, status, amount, gross, net, fee, created_at, updated_at, description)
			 SELECT CASE WHEN pu.wp_user_id > 0 THEN pu.wp_user_id ELSE -pu.id END, o.id, 'cod_received', 'available',
			        f.liquido_produtor, f.liquido_produtor, 0, 0, NOW(), NOW(),
			        'Recebimento COD Motoboy do pedido #' || o.id || ' (líquido produtor) [audit_fix_order]'
			 FROM sz_orders o
			 JOIN sz_order_financials f ON f.order_id = o.id
			 JOIN senderzz_portal_users pu ON pu.id = o.produtor_id
			 LEFT JOIN sz_cod_wallet_transactions c
			   ON c.order_id = o.id AND c.type='cod_received' AND c.status <> 'reversed'
			 WHERE o.id = $1
			   AND COALESCE(o.produtor_id,0) > 0
			   AND COALESCE(f.liquido_produtor,0) > 0
			   AND c.id IS NULL`, orderID)
		if e == nil {
			res["producer_wallet_inserted"] = int(tag.RowsAffected())
		}

		// GUARDA (AUDIT-2026-07-14): só corrige pra cima, nunca clawback de saldo
		// já disponível/sacado (ver audit.go bloco 3b).
		tag, e = h.Pool.Exec(ctx,
			`UPDATE sz_cod_wallet_transactions c
			 SET gross = f.producer_net_live,
			     net = f.producer_net_live,
			     updated_at = NOW()
			 FROM sz_orders o
			 JOIN sz_order_financials f ON f.order_id = o.id
			 WHERE c.order_id = o.id AND c.type='cod_received' AND c.status <> 'reversed'
			   AND o.id = $1
			   AND f.producer_net_live > COALESCE(NULLIF(c.net,0), c.gross) + 0.01`, orderID)
		if e == nil {
			res["producer_wallet_updated"] = int(tag.RowsAffected())
		}
	}

	httpx.JSON(w, 200, map[string]any{
		"ok":                true,
		"motoboy_pedido_id": mbPedidoID,
		"sz_order_id":       orderID,
		"result":            res,
	})
}

// ── POST /orders/motoboy/{id}/reagendar ───────────────────────────────────────

type reagendarBody struct {
	Data string `json:"data"` // YYYY-MM-DD
}

// addBusinessDays — soma n dias úteis a partir de base (pula sábado/domingo).
// Opera em UTC midnight para casar com o parse de "2006-01-02".
func addBusinessDays(base time.Time, n int) time.Time {
	d := base
	for added := 0; added < n; {
		d = d.AddDate(0, 0, 1)
		if d.Weekday() != time.Saturday && d.Weekday() != time.Sunday {
			added++
		}
	}
	return d
}

// Reagendar — grava nova data de entrega no pedido motoboy.
// {id} = sz_motoboy_pedidos.id (direto, sem indireção via sz_orders).
// Regra: data > 5 dias ÚTEIS a partir de hoje → status='pre_agendado'
//
//	(aguarda confirmação do produtor/afiliado no painel deles); senão 'agendado'.
//
// POST /orders/motoboy/{id}/reagendar  body {data:"YYYY-MM-DD"}
func (h *OrdersHandler) Reagendar(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	mbPedidoID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || mbPedidoID <= 0 {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	ctx := r.Context()

	var body reagendarBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	// Valida formato YYYY-MM-DD (UTC midnight).
	chosen, perr := time.Parse("2006-01-02", strings.TrimSpace(body.Data))
	if perr != nil {
		httpx.Err(w, 400, "bad_request", "data inválida — use o formato YYYY-MM-DD")
		return
	}

	if !h.tableExists(ctx, "sz_motoboy_pedidos") {
		httpx.Err(w, 503, "tables_missing", "sz_motoboy_pedidos ainda não migrada")
		return
	}

	// OWNERSHIP-GATE (DualAuth): admin muta tudo; produtor/afiliado só pedido DELE.
	// 404 unificado ANTES de qualquer escrita — não revela existência de pedido alheio.
	if !h.actorOwnsMotoboyPedido(ctx, mbPedidoID) {
		httpx.Err(w, 404, "not_found", "pedido motoboy não encontrado")
		return
	}

	// today em UTC midnight para comparar no mesmo referencial do parse.
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if chosen.Before(today) {
		httpx.Err(w, 400, "bad_request", "data no passado não permitida")
		return
	}

	// Threshold = hoje + 5 dias úteis. Estritamente DEPOIS → pré-agendado.
	threshold := addBusinessDays(today, 5)
	newStatus := "agendado"
	if chosen.After(threshold) {
		newStatus = "pre_agendado"
	}

	// Captura status atual (para auditar a transição).
	var deStatus sql.NullString
	err = h.Pool.QueryRow(ctx,
		`SELECT status FROM sz_motoboy_pedidos WHERE id = $1`, mbPedidoID,
	).Scan(&deStatus)
	if err != nil {
		httpx.Err(w, 404, "not_found", "pedido motoboy não encontrado")
		return
	}

	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx,
		`UPDATE sz_motoboy_pedidos
		 SET reagendado_para = $1, status = $2, updated_at = NOW()
		 WHERE id = $3`,
		body.Data, newStatus, mbPedidoID)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.Err(w, 404, "not_found", "pedido motoboy não encontrado")
		return
	}

	// BRIDGE reverso: reagendar um pedido que estava frustrado/cancelado volta a
	// operar (agendado/pre_agendado) — sem isso sz_orders.status fica preso em
	// frustrado/cancelled mesmo com o motoboy já reativado. Reset p/ 'pending'
	// (mesmo estado inicial de um pedido antes de entrar em separação/rota).
	if deStatus.Valid && (deStatus.String == "frustrado" || deStatus.String == "cancelado") {
		if _, err := tx.Exec(ctx, `
			UPDATE sz_orders o
			   SET status = 'pending', updated_at = NOW()
			  FROM sz_motoboy_pedidos p
			 WHERE p.id = $1
			   AND COALESCE(o.wp_order_id, o.id) = p.wc_order_id
			   AND o.status <> 'pending'`,
			mbPedidoID,
		); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// Sincroniza _sz_delivery_date em sz_order_meta (display de data de entrega prioriza meta).
	// Best-effort: falha silenciosa — mp.reagendado_para já commitou.
	if h.tableExists(ctx, "sz_order_meta") {
		_, _ = h.Pool.Exec(ctx,
			`UPDATE sz_order_meta m
			    SET meta_value = $1
			   FROM sz_orders o
			  WHERE o.id = m.order_id
			    AND m.meta_key = '_sz_delivery_date'
			    AND COALESCE(o.wp_order_id, o.id) = (
			        SELECT wc_order_id FROM sz_motoboy_pedidos WHERE id = $2 LIMIT 1
			    )`,
			body.Data, mbPedidoID)
	}

	h.writeMotoboyAudit(ctx, mbPedidoID, "reagendar", deStatus, newStatus,
		map[string]any{"reagendado_para": body.Data})

	httpx.JSON(w, 200, map[string]any{
		"ok":                true,
		"motoboy_pedido_id": mbPedidoID,
		"reagendado_para":   body.Data,
		"new_status":        newStatus,
	})
}

// writeMotoboyAudit — registra uma ação em sz_motoboy_audit (best-effort).
// actor_tipo derivado da identidade AUTENTICADA (auth.ActorFromCtx): 'admin' |
// 'produtor' | 'afiliado'. actor_id: id do admin (JWT) quando admin; senão o
// WPUserID do portal (mesma convenção do go/portal writeMotoboyAuditPortal — agrupa
// o mesmo ator entre os dois serviços). meta_json é TEXT em Postgres — grava string
// crua sem cast ::jsonb. Falha silenciosa: nunca derruba a request (a mutação já
// commitou; o audit é best-effort por design).
//
// NOTA: os valores 'produtor'/'afiliado' exigem o CHECK relaxado de
// infra/postgres/470-audit-actor-tipo-portal.sql; sem aplicar essa migração o
// INSERT de portal é rejeitado pelo CHECK e a linha é silenciosamente descartada.
func (h *OrdersHandler) writeMotoboyAudit(ctx context.Context, pedidoID int64, acao string, deStatus sql.NullString, paraStatus string, meta map[string]any) {
	if !h.tableExists(ctx, "sz_motoboy_audit") {
		return
	}

	// actor_tipo pela identidade autenticada; default 'admin' (rota legada sem Actor).
	actorTipo := "admin"
	if actor := auth.ActorFromCtx(ctx); actor != nil {
		switch actor.Kind {
		case auth.ActorProdutor:
			actorTipo = "produtor"
		case auth.ActorAfiliado:
			actorTipo = "afiliado"
		default:
			actorTipo = "admin"
		}
	}

	if meta == nil {
		meta = map[string]any{}
	}

	// actor_id: admin → senderzz_admin_users.id (+ email/nome no meta para forense);
	// portal → WPUserID (mesma convenção do go/portal). adm é nil em token de portal.
	var actorID *int64
	if adm := auth.FromCtx(ctx); adm != nil {
		tmp := adm.ID
		actorID = &tmp
		meta["admin_email"] = adm.Email
		meta["admin_nome"] = adm.Nome
	} else if actor := auth.ActorFromCtx(ctx); actor != nil && actor.Kind != auth.ActorAdmin {
		tmp := actor.WPUserID
		actorID = &tmp
		meta["portal_user_id"] = actor.PortalUserID
		meta["portal_role"] = actor.Role
	}

	meta["source"] = "admin_orders"
	metaJSON, _ := json.Marshal(meta)
	var de any
	if deStatus.Valid {
		de = deStatus.String
	}
	_, _ = h.Pool.Exec(ctx,
		`INSERT INTO sz_motoboy_audit
		   (pedido_id, motoboy_id, actor_tipo, actor_id,
		    acao, de_status, para_status, meta_json, created_at)
		 VALUES ($1, NULL, $2, $3, $4, $5, $6, $7, NOW())`,
		pedidoID, actorTipo, actorID, acao, de, paraStatus, string(metaJSON),
	)
}

// ── POST /orders/motoboy/{id}/cancelar ────────────────────────────────────────

// Cancelar — muda status do pedido motoboy para 'cancelado'.
// {id} = sz_motoboy_pedidos.id (direto).
// POST /orders/motoboy/{id}/cancelar
func (h *OrdersHandler) Cancelar(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	mbPedidoID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || mbPedidoID <= 0 {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	ctx := r.Context()

	if !h.tableExists(ctx, "sz_motoboy_pedidos") {
		httpx.Err(w, 503, "tables_missing", "sz_motoboy_pedidos ainda não migrada")
		return
	}

	// OWNERSHIP-GATE (DualAuth): admin muta tudo; produtor/afiliado só pedido DELE.
	// 404 unificado ANTES de qualquer escrita — não revela existência de pedido alheio.
	if !h.actorOwnsMotoboyPedido(ctx, mbPedidoID) {
		httpx.Err(w, 404, "not_found", "pedido motoboy não encontrado")
		return
	}

	// Captura status atual (para auditar a transição).
	var deStatus sql.NullString
	err = h.Pool.QueryRow(ctx,
		`SELECT status FROM sz_motoboy_pedidos WHERE id = $1`, mbPedidoID,
	).Scan(&deStatus)
	if err != nil {
		httpx.Err(w, 404, "not_found", "pedido motoboy não encontrado")
		return
	}

	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx,
		`UPDATE sz_motoboy_pedidos
		 SET status = 'cancelado', updated_at = NOW()
		 WHERE id = $1`,
		mbPedidoID)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.Err(w, 404, "not_found", "pedido motoboy não encontrado")
		return
	}

	// BRIDGE motoboy → sz_orders (MESMA transação). Sem isso o rastreio marca
	// "cancelado" mas sz_orders.status fica "pending" pra sempre (ex.: pedido
	// 1605/Isaias). Espelha forceMotoboyStatusToSzOrder em order_detail.go.
	if szStatus, mapped := forceMotoboyStatusToSzOrder("cancelado"); mapped {
		if _, err := tx.Exec(ctx, `
			UPDATE sz_orders o
			   SET status = $2, updated_at = NOW()
			  FROM sz_motoboy_pedidos p
			 WHERE p.id = $1
			   AND COALESCE(o.wp_order_id, o.id) = p.wc_order_id
			   AND o.status <> $2`,
			mbPedidoID, szStatus,
		); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	h.writeMotoboyAudit(ctx, mbPedidoID, "cancelar", deStatus, "cancelado", nil)

	httpx.JSON(w, 200, map[string]any{
		"ok":                true,
		"motoboy_pedido_id": mbPedidoID,
		"new_status":        "cancelado",
	})
}

// ── POST /orders/motoboy/{id}/reagendar-clone ─────────────────────────────────

// ReagendarClone — CLONE de um pedido FRUSTRADO/CANCELADO pra reentrega (REGRA DO
// DONO 2026-07-21: idêntico ao original em TUDO — produto, afiliado, financeiro,
// comissão — só muda a data de entrega; SEM marcador de "reentrega" em lugar nenhum,
// é um pedido normal como outro qualquer). O ORIGINAL permanece INTACTO; cria-se uma
// NOVA venda (sz_orders + items + endereços + meta, clonados do original) e um NOVO
// sz_motoboy_pedidos vinculado a ela — não um órfão. Ao ser entregue, gera comissão
// normal pro produtor/afiliado (like qualquer pedido nascido no checkout).
//
// {id} = sz_motoboy_pedidos.id do original. Body {data:"YYYY-MM-DD"}. Só
// FRUSTRADO/CANCELADO, e só quando o original tem sz_orders vinculado (senão 409 —
// não dá pra clonar "idêntico" sem uma venda-fonte completa pra copiar).
func (h *OrdersHandler) ReagendarClone(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	mbPedidoID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || mbPedidoID <= 0 {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	ctx := r.Context()

	var body reagendarBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	chosen, perr := time.Parse("2006-01-02", strings.TrimSpace(body.Data))
	if perr != nil {
		httpx.Err(w, 400, "bad_request", "data inválida — use o formato YYYY-MM-DD")
		return
	}

	if !h.tableExists(ctx, "sz_motoboy_pedidos") {
		httpx.Err(w, 503, "tables_missing", "sz_motoboy_pedidos ainda não migrada")
		return
	}

	// OWNERSHIP-GATE (DualAuth): admin clona qualquer pedido; produtor/afiliado só
	// clona pedido DELE. Resolvido pelo ORIGINAL (que tem sz_orders pai). 404 unificado
	// ANTES de qualquer escrita (antes do tx.Begin) — não revela pedido alheio.
	if !h.actorOwnsMotoboyPedido(ctx, mbPedidoID) {
		httpx.Err(w, 404, "not_found", "pedido motoboy não encontrado")
		return
	}

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if chosen.Before(today) {
		httpx.Err(w, 400, "bad_request", "data no passado não permitida")
		return
	}

	// GATE POR ZONA (REGRA DO DONO 2026-06-23): a data escolhida tem de cair num DIA DE
	// FUNCIONAMENTO da zona do pedido E respeitar o CUTOFF do dia. Fail-open: pedido sem
	// zona_id / zona inexistente → permite tudo (loadZoneSchedule retorna HasSchedule=false).
	// chosenSP = meia-noite SP da data (DOW e cutoff avaliados no fuso correto).
	zsched := h.loadZoneSchedule(ctx, mbPedidoID)
	chosenSP := time.Date(chosen.Year(), chosen.Month(), chosen.Day(), 0, 0, 0, 0, tzSaoPauloAdmin)
	if ok, motivo := zsched.dateAllowed(chosenSP, time.Now()); !ok {
		httpx.Err(w, 400, "data_invalida", "Não é possível reagendar para esta data: "+motivo+".")
		return
	}

	// Mesma regra do Reagendar: > 5 dias úteis → pré-agendado; senão agendado.
	newStatus := "agendado"
	if chosen.After(addBusinessDays(today, 5)) {
		newStatus = "pre_agendado"
	}

	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Confirma que o original existe e está FRUSTRADO/CANCELADO (guard contra clone-de-clone/loop).
	var srcStatus string
	var origWCOrderID int64
	err = tx.QueryRow(ctx,
		`SELECT COALESCE(status,''), COALESCE(wc_order_id,0) FROM sz_motoboy_pedidos WHERE id = $1`, mbPedidoID,
	).Scan(&srcStatus, &origWCOrderID)
	if err != nil {
		httpx.Err(w, 404, "not_found", "pedido motoboy não encontrado")
		return
	}
	if srcStatus != "frustrado" && srcStatus != "cancelado" {
		httpx.Err(w, 409, "not_clonable",
			"apenas pedidos frustrados ou cancelados podem ser reagendados por clone (status atual: "+srcStatus+")")
		return
	}

	// Venda-fonte: sem ela não dá pra clonar "idêntico" (produto/afiliado/financeiro).
	var origOrderID int64
	err = tx.QueryRow(ctx,
		`SELECT id FROM sz_orders WHERE COALESCE(wp_order_id, id) = $1`, origWCOrderID,
	).Scan(&origOrderID)
	if err != nil {
		httpx.Err(w, 409, "no_source_order",
			"pedido original sem venda associada (sz_orders) — não é possível reagendar por cópia")
		return
	}

	// 1) Clona a VENDA (sz_orders): todas as colunas de negócio idênticas ao original;
	// reseta só o ciclo de vida (status/payment_status voltam a 'pending', novo id,
	// order_number gerado a partir do novo id, timestamps atuais).
	var newOrderID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO sz_orders (
		    order_number, wp_order_id, user_id, produtor_id, affiliate_id, status,
		    subtotal, shipping, total, payment_method, payment_status, currency,
		    customer_note, ip_address, user_agent, customer_name, billing_email,
		    affiliate_amount, senderzz_fee, producer_net, shipping_class, shipping_class_id,
		    delivery_fee, transaction_fee, created_at, updated_at
		)
		SELECT
		    '', wp_order_id, user_id, produtor_id, affiliate_id, 'pending',
		    subtotal, shipping, total, payment_method, 'pending', currency,
		    customer_note, ip_address, user_agent, customer_name, billing_email,
		    affiliate_amount, senderzz_fee, producer_net, shipping_class, shipping_class_id,
		    delivery_fee, transaction_fee, NOW(), NOW()
		FROM sz_orders WHERE id = $1
		RETURNING id`,
		origOrderID,
	).Scan(&newOrderID)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if _, err := tx.Exec(ctx,
		`UPDATE sz_orders SET order_number = 'SZ-' || lpad(id::text, 7, '0') WHERE id = $1`,
		newOrderID,
	); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// 2) Clona itens, endereços e meta (exceto _sz_delivery_date, que recebe a NOVA data).
	if _, err := tx.Exec(ctx, `
		INSERT INTO sz_order_items (order_id, produto_id, nome, sku, quantidade, preco_unit, subtotal, meta)
		SELECT $2, produto_id, nome, sku, quantidade, preco_unit, subtotal, meta
		FROM sz_order_items WHERE order_id = $1`,
		origOrderID, newOrderID,
	); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO sz_order_addresses (order_id, tipo, nome, email, telefone, cep, logradouro, numero, complemento, bairro, cidade, uf, pais)
		SELECT $2, tipo, nome, email, telefone, cep, logradouro, numero, complemento, bairro, cidade, uf, pais
		FROM sz_order_addresses WHERE order_id = $1`,
		origOrderID, newOrderID,
	); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO sz_order_meta (order_id, meta_key, meta_value)
		SELECT $2, meta_key, meta_value
		FROM sz_order_meta WHERE order_id = $1 AND meta_key <> '_sz_delivery_date'`,
		origOrderID, newOrderID,
	); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO sz_order_meta (order_id, meta_key, meta_value) VALUES ($1, '_sz_delivery_date', $2)`,
		newOrderID, body.Data,
	); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// 3) Clona o pedido MOTOBOY, vinculado à venda nova (wc_order_id = newOrderID —
	// checkout novo sempre grava wp_order_id NULL, então COALESCE(wp_order_id,id)=id).
	var newCloneID int64
	err = tx.QueryRow(ctx,
		`INSERT INTO sz_motoboy_pedidos (
		     wc_order_id, cd_id, zona_id, motoboy_id, status,
		     dest_nome, dest_telefone, dest_cep, dest_endereco, dest_numero,
		     dest_complemento, dest_produto, quantidade,
		     dest_bairro, dest_cidade, dest_uf, dest_lat, dest_lng,
		     valor_pedido, valor_taxa,
		     data_entrega, reagendado_para,
		     ts_aprovado, created_at, updated_at
		 )
		 SELECT
		     $2, cd_id, zona_id, NULL, $3,
		     dest_nome, dest_telefone, dest_cep, dest_endereco, dest_numero,
		     dest_complemento, dest_produto, quantidade,
		     dest_bairro, dest_cidade, dest_uf, dest_lat, dest_lng,
		     valor_pedido, valor_taxa,
		     $4::date, $4::date,
		     NOW(), NOW(), NOW()
		 FROM sz_motoboy_pedidos
		 WHERE id = $1
		 RETURNING id`,
		mbPedidoID, newOrderID, newStatus, body.Data,
	).Scan(&newCloneID)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// Audit (best-effort): registra a ação no ORIGINAL (de_status=srcStatus) e no CLONE.
	deOriginal := sql.NullString{String: srcStatus, Valid: true}
	h.writeMotoboyAudit(ctx, mbPedidoID, "reagendar_clone", deOriginal, srcStatus,
		map[string]any{"clone_pedido_id": newCloneID, "clone_wc_order_id": newOrderID, "nova_data": body.Data})
	h.writeMotoboyAudit(ctx, newCloneID, "reagendar_clone", sql.NullString{}, newStatus,
		map[string]any{"origem_pedido_id": mbPedidoID, "nova_data": body.Data})

	httpx.JSON(w, 201, map[string]any{
		"ok":                true,
		"origem_pedido_id":  mbPedidoID,
		"clone_pedido_id":   newCloneID,
		"clone_wc_order_id": newOrderID,
		"reagendado_para":   body.Data,
		"new_status":        newStatus,
	})
}
