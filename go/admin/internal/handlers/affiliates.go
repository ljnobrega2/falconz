package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/auth"
	"github.com/senderzz/admin-service/internal/httpx"
)

type AffiliatesHandler struct{ Pool *pgxpool.Pool }

type affiliate struct {
	UserID           int64   `json:"user_id"`
	Email            string  `json:"email"`
	Nome             string  `json:"nome"`
	Telefone         string  `json:"telefone"` // senderzz_portal_user_meta._billing_phone (WC convention)
	CPF              string  `json:"cpf"`      // senderzz_portal_user_meta._billing_cpf
	PixKey           string  `json:"pix_key"`  // u.settings->>'pix_key' ou meta _senderzz_pix_key / _pix_key
	AffiliateCode    string  `json:"affiliate_code"`
	ComissaoPct      float64 `json:"comissao_pct"`
	Status           string  `json:"status"`
	CreatedAt        string  `json:"created_at"`
	Vinculos         int64   `json:"vinculos"`
	LinksCount       int64   `json:"links_count"`        // total de links de checkout do afiliado
	TotalClicks      int64   `json:"total_clicks"`       // soma de clicks em todos os links
	TotalVendido30d  float64 `json:"total_vendido_30d"`  // SUM(sz_orders.total) últimos 30d
	TotalComissao30d float64 `json:"total_comissao_30d"` // SUM(affiliate_amount) últimos 30d
	PedidosCount30d  int64   `json:"pedidos_count_30d"`
	LastOrderAt      *string `json:"last_order_at"` // último pedido como afiliado (data ISO)
	// Enriquecimento (auditoria 2026-06-18 — drawer de detalhe do afiliado):
	//   - comissao_pendente / comissao_disponivel: agregados do LEDGER de
	//     transações por USUÁRIO (não da senderzz_affiliate_wallet, que é chaveada
	//     por vínculo a.id e mis-soma quando há vários vínculos por afiliado).
	//   - valor_vendido_total: SUM(sz_orders.total) all-time WHERE affiliate_id=wp_user_id.
	ComissaoPendente   float64 `json:"comissao_pendente"`
	ComissaoDisponivel float64 `json:"comissao_disponivel"`
	ValorVendidoTotal  float64 `json:"valor_vendido_total"`
}

func (h *AffiliatesHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	// busca textual por email ou nome do afiliado (?q=) — passado raw; SQL monta os wildcards
	search := strings.TrimSpace(q.Get("q"))
	// MED25/MED36: o front envia status + data_ini/data_fim mas a List nunca os bindava.
	//   - status='active'      → casa qualquer vínculo "confirmado" (active/ativo/aprovado/approved,
	//                            case-insensitive — legado PT-BR convive com EN, ver affiliate_rules.go:306).
	//   - status='sem_vinculo' → afiliado SEM nenhuma linha em senderzz_affiliates.
	//   - data_ini/data_fim    → recorte por u.created_at (data de cadastro do afiliado), inclusivo
	//                            (espelha orders.go:195/199). NÃO usar o COALESCE(MIN(a.created_at))
	//                            agregado — manteria o filtro fora do WHERE e exigiria HAVING.
	// Via EXISTS em vez de HAVING: mantém a count query como COUNT(*) simples com os MESMOS
	// predicados (sem subquery-wrap), evitando o footgun de divergência lista↔total.
	statusFilter := strings.TrimSpace(q.Get("status"))
	dataIni := strings.TrimSpace(q.Get("data_ini"))
	dataFim := strings.TrimSpace(q.Get("data_fim"))
	// FEAT-2026-06-22 (filtros ricos no espírito da tela de Pedidos):
	//   - vendas='com'/'sem' → afiliado COM ou SEM faturamento atribuído. Usa o
	//     MESMO predicado da coluna valor_vendido_total (o.affiliate_id = u.wp_user_id),
	//     senão o filtro discordaria da coluna e da ordenação.
	//   - valor_min → piso de faturamento all-time (SUM(sz_orders.total) >= N). Idem:
	//     mesma atribuição por wp_user_id da coluna exibida.
	//   - produtor → busca textual pelo nome do produtor a que o afiliado está vinculado
	//     (EXISTS + join canônico id/wp_user_id, espelha Detail/Links). Raw; SQL monta o wildcard.
	// Todos via EXISTS/subquery no WHERE (não HAVING) p/ manter a count query como
	// COUNT(*) simples com os MESMOS predicados (footgun lista↔total — ver bloco acima).
	vendasFilter := strings.TrimSpace(q.Get("vendas"))
	valorMin := strings.TrimSpace(q.Get("valor_min"))
	produtorQ := strings.TrimSpace(q.Get("produtor"))

	// FEAT-RBAC-ORDERS-2026-06-24 — ESCOPO POR PAPEL (DualAuth) numa listagem que é
	// EXPORT DE PII (CPF + chave PIX + telefone por afiliado). "Não 401" NÃO basta:
	// um token de portal sem escopo vazaria a base inteira. Escopo pela identidade
	// AUTENTICADA (auth.ActorFromCtx):
	//   - admin/nil → vê tudo (inalterado).
	//   - produtor  → só os afiliados VINCULADOS a ele (senderzz_affiliates, join
	//                 canônico id/wp_user_id — espelha affiliates.go:205 / products.go:140).
	//   - afiliado  → SÓ ele mesmo (u.wp_user_id = <wpUserID>).
	// rbacScope é fragmento WHERE sem bind-params (subquery literal por id numérico),
	// compartilhado entre o SELECT e o COUNT para nunca divergirem.
	rbacScope := ""
	if actor := auth.ActorFromCtx(r.Context()); actor != nil {
		switch actor.Kind {
		case auth.ActorAdmin:
			// vê tudo.
		case auth.ActorProdutor:
			pid := strconv.FormatInt(actor.PortalUserID, 10)
			wpid := strconv.FormatInt(actor.WPUserID, 10)
			rbacScope = `
		  AND EXISTS (
		      SELECT 1 FROM senderzz_affiliates sa
		       WHERE sa.afiliado_id = u.wp_user_id
		         AND (sa.produtor_id = ` + pid + ` OR sa.produtor_id = ` + wpid + `))`
		case auth.ActorAfiliado:
			rbacScope = ` AND u.wp_user_id = ` + strconv.FormatInt(actor.WPUserID, 10)
		default:
			// role de portal sem escopo → vazio (fail-closed). Nunca serve a base de PII.
			httpx.JSON(w, 200, map[string]any{"items": []affiliate{}, "total": int64(0), "limit": limit, "offset": offset})
			return
		}
	}

	// Lista TODOS os usuários com role='affiliate' em senderzz_portal_users,
	// com LEFT JOIN para agregar vínculos e link_token (afiliado pode existir
	// sem vínculo ativo — nesse caso status='sem_vinculo').
	//
	// Enriquecimento (auditoria 2026-06-17):
	//   - telefone / cpf / pix_key: meta sensíveis via senderzz_portal_user_meta
	//     (WC convention _billing_phone / _billing_cpf; PIX em u.settings->>'pix_key'
	//     com fallback para _senderzz_pix_key / _pix_key).
	//   - links_count / total_clicks: senderzz_affiliate_links (via vínculo a.id).
	//   - total_vendido_30d / total_comissao_30d / pedidos_count_30d / last_order_at:
	//     agregados de sz_orders WHERE affiliate_id IN (u.id, u.wp_user_id) — o
	//     wp_user_id é o atalho para pedidos legados que ainda gravam o ID WP.
	rows, err := h.Pool.Query(r.Context(),
		`SELECT
		    u.id AS user_id,
		    u.email,
		    COALESCE(u.nome, '') AS nome,
		    COALESCE((
		        SELECT meta_value FROM senderzz_portal_user_meta
		         WHERE user_id = u.id AND meta_key = '_billing_phone' LIMIT 1
		    ), '') AS telefone,
		    COALESCE((
		        SELECT meta_value FROM senderzz_portal_user_meta
		         WHERE user_id = u.id AND meta_key = '_billing_cpf' LIMIT 1
		    ), '') AS cpf,
		    COALESCE(
		        NULLIF(u.settings->>'pix_key',''),
		        (SELECT meta_value FROM senderzz_portal_user_meta
		           WHERE user_id = u.id AND meta_key IN ('_senderzz_pix_key','_pix_key')
		           ORDER BY meta_key DESC LIMIT 1),
		        ''
		    ) AS pix_key,
		    COALESCE(MAX(al.link_token), '') AS affiliate_code,
		    COALESCE(MAX(a.comissao_pct), 0)::float8 AS comissao_pct,
		    COALESCE(MAX(a.status), 'sem_vinculo') AS status,
		    COALESCE(MIN(a.created_at), u.created_at)::text AS created_at,
		    COUNT(DISTINCT a.id) AS vinculos,
		    -- Links: dois saltos (vínculo a.id → links.affiliate_id).
		    COALESCE((
		        SELECT COUNT(DISTINCT al2.id) FROM senderzz_affiliate_links al2
		          JOIN senderzz_affiliates a2 ON a2.id = al2.affiliate_id
		         WHERE a2.afiliado_id = u.id
		    ), 0) AS links_count,
		    COALESCE((
		        SELECT SUM(al2.clicks) FROM senderzz_affiliate_links al2
		          JOIN senderzz_affiliates a2 ON a2.id = al2.affiliate_id
		         WHERE a2.afiliado_id = u.id
		    ), 0) AS total_clicks,
		    -- 30d sales: sz_orders.affiliate_id armazena o wp_user_id do afiliado.
		    -- NÃO usar IN (u.id, u.wp_user_id): o id de portal de um afiliado colide
		    -- com o wp_user_id de outro (ex: Gabriel id=19/wp=28, Keven id=28) →
		    -- atribuía a venda a 2 afiliados. Casar só por wp_user_id.
		    COALESCE((
		        SELECT SUM(o.total)::float8 FROM sz_orders o
		         WHERE o.affiliate_id = u.wp_user_id
		           AND o.created_at >= NOW() - INTERVAL '30 days'
		    ), 0)::float8 AS total_vendido_30d,
		    COALESCE((
		        SELECT SUM(o.affiliate_amount)::float8 FROM sz_orders o
		         WHERE o.affiliate_id = u.wp_user_id
		           AND o.created_at >= NOW() - INTERVAL '30 days'
		    ), 0)::float8 AS total_comissao_30d,
		    COALESCE((
		        SELECT COUNT(*) FROM sz_orders o
		         WHERE o.affiliate_id = u.wp_user_id
		           AND o.created_at >= NOW() - INTERVAL '30 days'
		    ), 0) AS pedidos_count_30d,
		    (
		        SELECT MAX(o.created_at)::text FROM sz_orders o
		         WHERE o.affiliate_id = u.wp_user_id
		    ) AS last_order_at,
		    -- Comissão pendente / disponível: LEDGER por USUÁRIO. Casa o afiliado
		    -- pelo wp_user_id (a.afiliado_id) e soma t.amount por status. NÃO usar
		    -- senderzz_affiliate_wallet (chaveada por vínculo a.id → mis-soma).
		    COALESCE((
		        SELECT SUM(t.amount)::float8 FROM senderzz_affiliate_transactions t
		          JOIN senderzz_affiliates a2 ON a2.id = t.affiliate_id
		         WHERE a2.afiliado_id = u.wp_user_id AND t.status = 'pending' AND t.type = 'commission'
		    ), 0)::float8 AS comissao_pendente,
		    -- CRIT-A (AUDIT-CRIT-AB): disponível subtrai penalty (despesa gravada positiva no
		    -- PHP) como -ABS + piso GREATEST(0,...) — casa com o portal Summary (wallet.go) e
		    -- com o cache do WalletFix. Sem isso, a lista admin mostrava o sacável inflado
		    -- (afiliado wp28: R$51 que deveria ser R$0), divergindo do portal.
		    GREATEST(0, COALESCE((
		        SELECT SUM(CASE WHEN t.type = 'penalty' THEN -ABS(t.amount) ELSE t.amount END)::float8
		          FROM senderzz_affiliate_transactions t
		          JOIN senderzz_affiliates a2 ON a2.id = t.affiliate_id
		         WHERE a2.afiliado_id = u.wp_user_id AND t.status = 'approved'
		    ), 0))::float8 AS comissao_disponivel,
		    -- Valor vendido all-time: mesma regra de atribuição das vendas 30d
		    -- (affiliate_id = wp_user_id), porém sem recorte de data.
		    COALESCE((
		        SELECT SUM(o.total)::float8 FROM sz_orders o
		         WHERE o.affiliate_id = u.wp_user_id
		    ), 0)::float8 AS valor_vendido_total
		FROM senderzz_portal_users u
		LEFT JOIN senderzz_affiliates a ON a.afiliado_id = u.wp_user_id
		LEFT JOIN senderzz_affiliate_links al ON al.affiliate_id = a.id AND al.active = TRUE
		WHERE u.role = 'afiliado'
		  AND ($1 = '' OR u.email ILIKE '%' || $1 || '%' OR u.nome ILIKE '%' || $1 || '%')
		  -- MED25/MED36: status (active=confirmado / sem_vinculo) via EXISTS.
		  AND ($2 = ''
		       OR ($2 = 'active' AND EXISTS (
		             SELECT 1 FROM senderzz_affiliates a2
		              WHERE a2.afiliado_id = u.wp_user_id
		                AND LOWER(COALESCE(a2.status,'')) IN ('active','ativo','aprovado','approved')))
		       OR ($2 = 'sem_vinculo' AND NOT EXISTS (
		             SELECT 1 FROM senderzz_affiliates a3
		              WHERE a3.afiliado_id = u.wp_user_id)))
		  -- MED36: período por data de cadastro do afiliado (inclusivo).
		  AND ($3 = '' OR u.created_at >= ($3::date)::timestamp AT TIME ZONE 'America/Sao_Paulo')
		  AND ($4 = '' OR u.created_at <= (($4::date + 1)::timestamp AT TIME ZONE 'America/Sao_Paulo' - interval '1 second'))
		  -- FEAT-2026-06-22: com/sem vendas — MESMA atribuição da coluna (affiliate_id = wp_user_id).
		  AND ($5 = ''
		       OR ($5 = 'com' AND EXISTS (
		             SELECT 1 FROM sz_orders o WHERE o.affiliate_id = u.wp_user_id))
		       OR ($5 = 'sem' AND NOT EXISTS (
		             SELECT 1 FROM sz_orders o WHERE o.affiliate_id = u.wp_user_id)))
		  -- FEAT-2026-06-22: piso de faturamento all-time (idem atribuição por wp_user_id).
		  AND ($6 = '' OR (
		         SELECT COALESCE(SUM(o.total),0) FROM sz_orders o
		          WHERE o.affiliate_id = u.wp_user_id) >= $6::numeric)
		  -- FEAT-2026-06-22: produtor vinculado por nome (EXISTS + join canônico id/wp_user_id).
		  AND ($7 = '' OR EXISTS (
		         SELECT 1 FROM senderzz_affiliates a4
		           JOIN senderzz_portal_users p4
		             ON (p4.id = a4.produtor_id OR p4.wp_user_id = a4.produtor_id) AND p4.role = 'produtor'
		          WHERE a4.afiliado_id = u.wp_user_id
		            AND p4.nome ILIKE '%' || $7 || '%'))`+
			// FEAT-RBAC-ORDERS-2026-06-24 — escopo por papel (produtor→seus afiliados;
			// afiliado→só ele). Vazio para admin. Mesmo fragmento no COUNT abaixo.
			rbacScope+`
		GROUP BY u.id, u.email, u.nome, u.created_at, u.wp_user_id, u.settings
		-- FEAT-2026-06-22: ordena por FATURAMENTO all-time DESC (maior vendedor primeiro).
		-- O alias valor_vendido_total é single-valued por grupo (correlaciona em
		-- u.wp_user_id, chave de agrupamento). COALESCE 0 já joga sem-vendas pro fim
		-- (NULLS LAST automático). Desempate por u.id DESC (a.id não existe pós-GROUP).
		ORDER BY valor_vendido_total DESC, u.id DESC
		LIMIT $8 OFFSET $9`, search, statusFilter, dataIni, dataFim,
		vendasFilter, valorMin, produtorQ, limit, offset)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []affiliate{}
	for rows.Next() {
		var a affiliate
		_ = rows.Scan(&a.UserID, &a.Email, &a.Nome,
			&a.Telefone, &a.CPF, &a.PixKey,
			&a.AffiliateCode, &a.ComissaoPct, &a.Status, &a.CreatedAt, &a.Vinculos,
			&a.LinksCount, &a.TotalClicks,
			&a.TotalVendido30d, &a.TotalComissao30d, &a.PedidosCount30d,
			&a.LastOrderAt,
			&a.ComissaoPendente, &a.ComissaoDisponivel, &a.ValorVendidoTotal)
		out = append(out, a)
	}

	// Total = todos os usuários com role='affiliate' que casam com a busca + filtros.
	// MESMOS predicados da list (busca + status + período) — alias u necessário p/ as
	// subconsultas EXISTS resolverem u.wp_user_id. Mantém total↔itens coerentes.
	var total int64
	_ = h.Pool.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM senderzz_portal_users u
		 WHERE u.role = 'afiliado'
		   AND ($1 = '' OR u.email ILIKE '%' || $1 || '%' OR u.nome ILIKE '%' || $1 || '%')
		   AND ($2 = ''
		        OR ($2 = 'active' AND EXISTS (
		              SELECT 1 FROM senderzz_affiliates a2
		               WHERE a2.afiliado_id = u.wp_user_id
		                 AND LOWER(COALESCE(a2.status,'')) IN ('active','ativo','aprovado','approved')))
		        OR ($2 = 'sem_vinculo' AND NOT EXISTS (
		              SELECT 1 FROM senderzz_affiliates a3
		               WHERE a3.afiliado_id = u.wp_user_id)))
		   AND ($3 = '' OR u.created_at >= ($3::date)::timestamp AT TIME ZONE 'America/Sao_Paulo')
		   AND ($4 = '' OR u.created_at <= (($4::date + 1)::timestamp AT TIME ZONE 'America/Sao_Paulo' - interval '1 second'))
		   -- FEAT-2026-06-22: MESMOS predicados da list (vendas / valor_min / produtor) — total↔itens coerente.
		   AND ($5 = ''
		        OR ($5 = 'com' AND EXISTS (
		              SELECT 1 FROM sz_orders o WHERE o.affiliate_id = u.wp_user_id))
		        OR ($5 = 'sem' AND NOT EXISTS (
		              SELECT 1 FROM sz_orders o WHERE o.affiliate_id = u.wp_user_id)))
		   AND ($6 = '' OR (
		          SELECT COALESCE(SUM(o.total),0) FROM sz_orders o
		           WHERE o.affiliate_id = u.wp_user_id) >= $6::numeric)
		   AND ($7 = '' OR EXISTS (
		          SELECT 1 FROM senderzz_affiliates a4
		            JOIN senderzz_portal_users p4
		              ON (p4.id = a4.produtor_id OR p4.wp_user_id = a4.produtor_id) AND p4.role = 'produtor'
		           WHERE a4.afiliado_id = u.wp_user_id
		             AND p4.nome ILIKE '%' || $7 || '%'))`+
			// FEAT-RBAC-ORDERS-2026-06-24 — MESMO escopo por papel do SELECT (total↔itens coerente).
			rbacScope,
		search, statusFilter, dataIni, dataFim, vendasFilter, valorMin, produtorQ).Scan(&total)

	// AUDIT-2026-06-21 #8/#18 (LGPD-PII-AUDIT): a listagem de afiliados serve CPF e
	// chave PIX de cada titular em massa (acesso tipo-export). Registra na trilha de
	// accountability — escopo = nº de afiliados retornados. Best-effort, espelha
	// order_detail.go. Não bloqueia a request.
	ctx := r.Context()
	if actor := auth.FromCtx(ctx); actor != nil {
		logPIIAccess(ctx, h.Pool, actor.ID, actor.Email, "affiliate", int64(len(out)),
			[]string{"cpf", "pix_key", "telefone"}, "export", r.RemoteAddr)
	} else {
		logPIIAccess(ctx, h.Pool, 0, "", "affiliate", int64(len(out)),
			[]string{"cpf", "pix_key", "telefone"}, "export", r.RemoteAddr)
	}

	httpx.JSON(w, 200, map[string]any{"items": out, "total": total})
}

type affiliateLink struct {
	ID           int64   `json:"id"`
	ProdutorNome string  `json:"produtor_nome"`
	AfiliadoNome string  `json:"afiliado_nome"`
	ProdutoNome  *string `json:"produto_nome"`
	ComissaoPct  float64 `json:"comissao_pct"`
	Status       string  `json:"status"`
	CreatedAt    string  `json:"created_at"`
	LinkToken    string  `json:"link_token"` // oferta — token do link de checkout
	LinkURL      string  `json:"link_url"`   // url completa (quando disponível)
	LinkActive   bool    `json:"link_active"`
}

func (h *AffiliatesHandler) Links(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	status := strings.TrimSpace(q.Get("status"))
	// busca textual por nome de afiliado ou produtor (?q=)
	search := strings.TrimSpace(q.Get("q"))
	if search != "" {
		search = "%" + search + "%"
	}

	// Junta primeiro link ativo (token + url) por vínculo via subquery escalar
	// — evita fan-out quando o afiliado tem várias ofertas no mesmo vínculo.
	rows, err := h.Pool.Query(r.Context(),
		`SELECT a.id,
		        COALESCE(p.nome,'')  AS produtor_nome,
		        COALESCE(af.nome,'') AS afiliado_nome,
		        NULL::text           AS produto_nome,
		        COALESCE(a.comissao_pct, 0),
		        a.status,
		        a.created_at::text,
		        COALESCE((SELECT al.link_token FROM senderzz_affiliate_links al
		                  WHERE al.affiliate_id = a.id
		                  ORDER BY al.active DESC, al.id ASC LIMIT 1), '') AS link_token,
		        ''::text AS link_url,
		        COALESCE((SELECT al.active FROM senderzz_affiliate_links al
		                  WHERE al.affiliate_id = a.id
		                  ORDER BY al.active DESC, al.id ASC LIMIT 1), FALSE) AS link_active
		 FROM senderzz_affiliates a
		 LEFT JOIN senderzz_portal_users p  ON (p.id = a.produtor_id OR p.wp_user_id = a.produtor_id) AND p.role = 'produtor'
		 LEFT JOIN senderzz_portal_users af ON (af.wp_user_id = a.afiliado_id OR af.id = a.afiliado_id) AND af.role = 'afiliado'
		 WHERE ($1='' OR a.status=$1)
		   AND ($2='' OR COALESCE(p.nome,'')  ILIKE $2
		              OR COALESCE(af.nome,'') ILIKE $2)
		 ORDER BY a.id DESC LIMIT $3 OFFSET $4`, status, search, limit, offset)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []affiliateLink{}
	for rows.Next() {
		var l affiliateLink
		_ = rows.Scan(&l.ID, &l.ProdutorNome, &l.AfiliadoNome, &l.ProdutoNome,
			&l.ComissaoPct, &l.Status, &l.CreatedAt,
			&l.LinkToken, &l.LinkURL, &l.LinkActive)
		out = append(out, l)
	}

	var total int64
	_ = h.Pool.QueryRow(r.Context(),
		`SELECT COUNT(*)
		 FROM senderzz_affiliates a
		 LEFT JOIN senderzz_portal_users p  ON (p.id = a.produtor_id OR p.wp_user_id = a.produtor_id) AND p.role = 'produtor'
		 LEFT JOIN senderzz_portal_users af ON (af.wp_user_id = a.afiliado_id OR af.id = a.afiliado_id) AND af.role = 'afiliado'
		 WHERE ($1='' OR a.status=$1)
		   AND ($2='' OR COALESCE(p.nome,'')  ILIKE $2
		              OR COALESCE(af.nome,'') ILIKE $2)`,
		status, search).Scan(&total)

	httpx.JSON(w, 200, map[string]any{"items": out, "total": total})
}

type affiliateCommission struct {
	ID            int64   `json:"id"`
	OrderID       *int64  `json:"order_id"`     // sz_orders.id (pedido que gerou a comissão)
	OrderNumber   string  `json:"order_number"` // sz_orders.order_number (ex.: SZ-0001234)
	OrderTotal    float64 `json:"order_total"`  // sz_orders.total (valor total do pedido)
	OrderStatus   string  `json:"order_status"` // sz_orders.status
	AfiliadoNome  string  `json:"afiliado_nome"`
	AfiliadoEmail string  `json:"afiliado_email"`
	ProdutorNome  string  `json:"produtor_nome"`
	ProdutorEmail string  `json:"produtor_email"`
	ProdutoNome   *string `json:"produto_nome"`
	ProdutoID     *int64  `json:"produto_id"`   // primeiro item do pedido (sz_order_items)
	ComissaoPct   float64 `json:"comissao_pct"` // regra aplicada
	Valor         float64 `json:"valor"`        // t.amount = comissão LÍQUIDA (já net) — fallback do front
	// MODELO FINANCEIRO REAL (verificado — pedido 1570: total 276, amount 157,34, fee 8,26):
	//   - t.amount (= sz_orders.affiliate_amount) JÁ é a comissão LÍQUIDA.
	//   - sz_orders.transaction_fee é a taxa de 4,99% REAL do afiliado (take da plataforma).
	//   - comissao_bruta = amount + transaction_fee (= total × comissao_pct). Ex.: 157,34 + 8,26 = 165,60.
	//   - comissao_liquida = amount (NÃO recalcular; aplicar ×0,9501 sobre amount era o BUG: taxa em dobro).
	//   - taxa_transacao_afiliado = transaction_fee (NÃO derivar de amount).
	// Frustrado (penalty) / cancelado / revertido: taxa = 0 e líquida = bruta = amount.
	ComissaoBruta         float64 `json:"comissao_bruta"`
	ComissaoLiquida       float64 `json:"comissao_liquida"`
	TaxaTransacaoAfiliado float64 `json:"taxa_transacao_afiliado"`
	Tipo                  string  `json:"tipo"`
	StatusTx              string  `json:"status_tx"`  // pending / available / cancelled etc.
	LinkToken             string  `json:"link_token"` // link de afiliado (senderzz_affiliate_links — hoje vazio)
	LinkURL               string  `json:"link_url"`   // app.falklog.com.br/checkout/<token> (vazio se sem link)
	// Oferta de checkout (PREÇO de venda) — fonte fiel via metas do pedido + checkout_links.
	// Resolve pelo pedido que originou a comissão (t.order_id), não pelo vínculo do afiliado.
	OfertaNome  string  `json:"oferta_nome"`  // _senderzz_offer_name
	OfertaValor float64 `json:"oferta_valor"` // _senderzz_offer_value (== display_value)
	OfertaURL   string  `json:"oferta_url"`   // senderzz_checkout_links.url (via token)
	AvailableAt *string `json:"available_at"` // data prevista de liberação (senderzz_affiliate_transactions.available_at)
	CreatedAt   string  `json:"created_at"`
}

func (h *AffiliatesHandler) Commissions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset, _ := strconv.Atoi(q.Get("offset"))

	// ?status= filtra t.type (commission, penalty, approval…)
	tipo := strings.TrimSpace(q.Get("status"))
	if tipo == "all" {
		tipo = ""
	}
	dataIni := strings.TrimSpace(q.Get("data_ini"))
	dataFim := strings.TrimSpace(q.Get("data_fim"))
	// busca textual por nome de afiliado ou produtor (?q=)
	search := strings.TrimSpace(q.Get("q"))
	if search != "" {
		search = "%" + search + "%"
	}

	whereClause := `
		WHERE ($1='' OR t.type=$1)
		  AND ($2='' OR t.created_at >= ($2::date)::timestamp AT TIME ZONE 'America/Sao_Paulo')
		  AND ($3='' OR t.created_at <= (($3::date + 1)::timestamp AT TIME ZONE 'America/Sao_Paulo' - interval '1 second'))
		  AND ($4='' OR COALESCE(af.nome,'') ILIKE $4
		             OR COALESCE(p.nome,'')  ILIKE $4)`

	// Enriquecimento (auditoria 2026-06-17):
	//   - order_number / order_total / order_status: LEFT JOIN sz_orders por t.order_id.
	//   - afiliado_email / produtor_email: das junções em senderzz_portal_users.
	//   - produto (nome + id): primeiro item via subquery escalar (evita fan-out).
	//   - link_token / link_url: link mais ativo do vínculo; URL = base do checkout.
	//   - available_at: senderzz_affiliate_transactions.available_at (adicionada na v463).
	sqlList := `SELECT t.id,
		        t.order_id,
		        COALESCE(o.order_number,'') AS order_number,
		        COALESCE(o.total, 0)::float8 AS order_total,
		        COALESCE(o.status,'') AS order_status,
		        COALESCE(af.nome,'') AS afiliado_nome,
		        COALESCE(af.email,'') AS afiliado_email,
		        COALESCE(p.nome,'')  AS produtor_nome,
		        COALESCE(p.email,'') AS produtor_email,
		        COALESCE((
		          SELECT i.nome FROM sz_order_items i
		          WHERE i.order_id = t.order_id
		          ORDER BY i.id ASC LIMIT 1
		        ), NULL) AS produto_nome,
		        (
		          SELECT i.produto_id FROM sz_order_items i
		          WHERE i.order_id = t.order_id
		          ORDER BY i.id ASC LIMIT 1
		        ) AS produto_id,
		        COALESCE(a.comissao_pct, 0)::float8 AS comissao_pct,
		        COALESCE(t.amount, 0),
		        t.type,
		        COALESCE(t.status,'') AS status_tx,
		        COALESCE((
		          SELECT al.link_token FROM senderzz_affiliate_links al
		          WHERE al.affiliate_id = a.id
		          ORDER BY al.active DESC, al.id ASC LIMIT 1
		        ), '') AS link_token,
		        COALESCE((
		          SELECT CASE WHEN al.link_token <> ''
		                      THEN 'https://app.falklog.com.br/checkout/' || al.link_token
		                      ELSE '' END
		            FROM senderzz_affiliate_links al
		           WHERE al.affiliate_id = a.id
		           ORDER BY al.active DESC, al.id ASC LIMIT 1
		        ), '') AS link_url,
		        -- Oferta de checkout (preço de venda): fiel via metas do pedido + checkout_links.
		        COALESCE((SELECT m.meta_value FROM sz_order_meta m
		                  WHERE m.order_id = t.order_id AND m.meta_key='_senderzz_offer_name'
		                  LIMIT 1), '') AS oferta_nome,
		        -- Guarda regex evita 500 caso uma meta venha não-numérica (advisor: cast-safe).
		        COALESCE((SELECT CASE WHEN m.meta_value ~ '^[0-9]+(\.[0-9]+)?$'
		                              THEN m.meta_value::numeric ELSE 0 END
		                  FROM sz_order_meta m
		                  WHERE m.order_id = t.order_id AND m.meta_key='_senderzz_offer_value'
		                  LIMIT 1), 0)::float8 AS oferta_valor,
		        COALESCE((SELECT cl.url FROM senderzz_checkout_links cl
		                  WHERE cl.token = (SELECT m.meta_value FROM sz_order_meta m
		                                     WHERE m.order_id = t.order_id AND m.meta_key='_senderzz_offer_token'
		                                     LIMIT 1)
		                  LIMIT 1), '') AS oferta_url,
		        t.available_at::text AS available_at,
		        t.created_at::text,
		        -- transaction_fee REAL do afiliado (take 4,99% da plataforma). Append no fim
		        -- da SELECT p/ preservar alinhamento coluna↔Scan (footgun do pgx).
		        COALESCE(o.transaction_fee, 0)::float8 AS transaction_fee
		 FROM senderzz_affiliate_transactions t
		 JOIN senderzz_affiliates a  ON a.id = t.affiliate_id
		 LEFT JOIN senderzz_portal_users af ON af.wp_user_id = a.afiliado_id
		 LEFT JOIN senderzz_portal_users p  ON p.id = a.produtor_id OR p.wp_user_id = a.produtor_id
		 LEFT JOIN sz_orders o ON o.id = t.order_id` +
		whereClause + `
		 ORDER BY t.id DESC LIMIT $5 OFFSET $6`

	sqlCount := `SELECT COUNT(*)
		 FROM senderzz_affiliate_transactions t
		 JOIN senderzz_affiliates a  ON a.id = t.affiliate_id
		 LEFT JOIN senderzz_portal_users af ON af.wp_user_id = a.afiliado_id
		 LEFT JOIN senderzz_portal_users p  ON p.id = a.produtor_id OR p.wp_user_id = a.produtor_id` +
		whereClause

	rows, err := h.Pool.Query(r.Context(), sqlList, tipo, dataIni, dataFim, search, limit, offset)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []affiliateCommission{}
	for rows.Next() {
		var c affiliateCommission
		var txFee float64 // o.transaction_fee (último arg do Scan — espelha a SELECT)
		_ = rows.Scan(&c.ID, &c.OrderID,
			&c.OrderNumber, &c.OrderTotal, &c.OrderStatus,
			&c.AfiliadoNome, &c.AfiliadoEmail,
			&c.ProdutorNome, &c.ProdutorEmail,
			&c.ProdutoNome, &c.ProdutoID,
			&c.ComissaoPct, &c.Valor, &c.Tipo, &c.StatusTx,
			&c.LinkToken, &c.LinkURL,
			&c.OfertaNome, &c.OfertaValor, &c.OfertaURL,
			&c.AvailableAt, &c.CreatedAt, &txFee)
		// MODELO FINANCEIRO REAL (verificado em dados — pedido 1570): c.Valor (= t.amount =
		// sz_orders.affiliate_amount) JÁ é a comissão LÍQUIDA. A taxa de 4,99% REAL do afiliado
		// é sz_orders.transaction_fee (NÃO derivar de amount). BRUTA = amount + transaction_fee.
		// BUG anterior: tratava amount como BRUTA e aplicava ×0,9501 / ×0,0499 → taxa em dobro.
		// REGRA: a taxa só se exibe quando o lançamento é comissão NORMAL — tipo='commission'
		// E status NOT IN ('cancelled','reversed'). Para penalty (FRUSTRADO) ou cancelado/revertido,
		// taxa = 0 e líquida = bruta = amount (no penalty o amount já é a própria taxa de frustrado,
		// e o transaction_fee do pedido pertence à comissão original — não se aplica aqui).
		if c.Tipo == "commission" && c.StatusTx != "cancelled" && c.StatusTx != "reversed" {
			c.ComissaoLiquida = c.Valor       // amount = líquida (NÃO recalcular)
			c.TaxaTransacaoAfiliado = txFee   // sz_orders.transaction_fee (taxa REAL)
			c.ComissaoBruta = c.Valor + txFee // amount + transaction_fee (= total × comissao_pct)
		} else {
			c.TaxaTransacaoAfiliado = 0
			c.ComissaoLiquida = c.Valor
			c.ComissaoBruta = c.Valor // bruta = líquida = amount (sem fee)
		}
		out = append(out, c)
	}

	var total int64
	_ = h.Pool.QueryRow(r.Context(), sqlCount, tipo, dataIni, dataFim, search).Scan(&total)

	httpx.JSON(w, 200, map[string]any{"items": out, "total": total})
}

// ─── GET /affiliates/{id}/detail ─────────────────────────────────────────
// {id} = portal_users.id (o user_id servido por List → consumido pela UI).
// CRIT-2/4 (LGPD): a List devolve u.id; filtrar o Detail por u.wp_user_id dava
// 404 ou casava OUTRO afiliado cujo wp_user_id colidia com este portal id —
// vazando PII de terceiro. Filtra por u.id e DERIVA o wp_user_id p/ os vínculos.

// affiliateConta — conta PIX exibida no drawer do afiliado. Para AFILIADO a
// fonte é portal_users.settings (pix_key / pix_key_tipo), NÃO sz_cod_withdraw_accounts
// (essa tabela é exclusiva de produtores e está vazia).
type affiliateConta struct {
	Nome      string `json:"nome"`
	PixKey    string `json:"pix_key"`
	PixType   string `json:"pix_type"`
	IsDefault bool   `json:"is_default"`
}

// affiliateVinculo — vínculo do afiliado com um produtor (uma oferta/produto).
type affiliateVinculo struct {
	ProdutorNome string  `json:"produtor_nome"`
	ProdutoID    *int64  `json:"produto_id"`
	ProdutoNome  *string `json:"produto_nome"`
	ComissaoPct  float64 `json:"comissao_pct"`
	Status       string  `json:"status"`
}

// affiliateTaxas — resumo das TAXAS COBRADAS do afiliado pela plataforma (não as
// comissões que ele recebe). Três fontes, todas atribuídas pelo wp_user_id:
//   - taxa_transacao_total: take de 4,99% da plataforma = sz_orders.transaction_fee,
//     porém SÓ dos pedidos cuja comissão é NORMAL (type='commission' e status NOT IN
//     ('cancelled','reversed')). Join via senderzz_affiliate_transactions.order_id —
//     espelha EXATAMENTE o que a tela de Comissões (Commissions, l.531-538) exibe na
//     coluna de taxa. O SUM(transaction_fee) "flat" sobre todos os pedidos do afiliado
//     DIVERGE (verificado: Gabriel wp=28 → flat 129,63 vs via_commission 58,11): pedidos
//     de comissão cancelada/revertida ainda carregam transaction_fee, mas a taxa NÃO foi
//     efetivamente cobrada (a venda não se concretizou). Usar a forma via_commission.
//   - penalidades_frustracao_total: SUM(amount) de type='penalty' já COBRADAS
//     (status IN ('approved','paid')) — exclui pending/cancelled/reversed.
//   - taxa_saque_total: SUM(fee) de senderzz_affiliate_withdrawals já efetivados
//     (status IN ('approved','paid')). Tabela existe; hoje 0 linhas → soma 0.
type affiliateTaxas struct {
	TaxaTransacaoTotal         float64 `json:"taxa_transacao_total"`
	PenalidadesFrustracaoTotal float64 `json:"penalidades_frustracao_total"`
	TaxaSaqueTotal             float64 `json:"taxa_saque_total"`
	TotalTaxas                 float64 `json:"total_taxas"`
}

type affiliateDetail struct {
	UserID   int64              `json:"user_id"`
	Nome     string             `json:"nome"`
	Email    string             `json:"email"`
	Telefone string             `json:"telefone"`
	CPF      string             `json:"cpf"`
	PixKey   string             `json:"pix_key"`
	PixTipo  string             `json:"pix_tipo"`
	Contas   []affiliateConta   `json:"contas"`
	Vinculos []affiliateVinculo `json:"vinculos"`
	Taxas    affiliateTaxas     `json:"taxas"`
}

func (h *AffiliatesHandler) Detail(w http.ResponseWriter, r *http.Request) {
	// {id} = portal_users.id (o user_id servido pela List → usado pela UI).
	portalID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || portalID <= 0 {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	ctx := r.Context()

	d := affiliateDetail{
		UserID:   portalID,
		Contas:   []affiliateConta{},
		Vinculos: []affiliateVinculo{},
	}

	// Cabeçalho: dados do afiliado. Casa por u.id (portal id, igual à List). Metas
	// sensíveis (telefone/cpf) vivem em senderzz_portal_user_meta chaveadas pelo
	// PORTAL id (u.id). PIX (key + tipo) vem de u.settings JSONB com fallback para
	// as metas legadas. Deriva u.wp_user_id na mesma query — é a chave de vínculos
	// (senderzz_affiliates.afiliado_id = wp_user_id), usada no bloco abaixo.
	var wpUserID *int64
	err = h.Pool.QueryRow(ctx,
		`SELECT
		    u.wp_user_id,
		    COALESCE(u.nome,'')  AS nome,
		    COALESCE(u.email,'') AS email,
		    COALESCE((
		        SELECT meta_value FROM senderzz_portal_user_meta
		         WHERE user_id = u.id AND meta_key = '_billing_phone' LIMIT 1
		    ), '') AS telefone,
		    COALESCE((
		        SELECT meta_value FROM senderzz_portal_user_meta
		         WHERE user_id = u.id AND meta_key = '_billing_cpf' LIMIT 1
		    ), '') AS cpf,
		    COALESCE(
		        NULLIF(u.settings->>'pix_key',''),
		        (SELECT meta_value FROM senderzz_portal_user_meta
		           WHERE user_id = u.id AND meta_key IN ('_senderzz_pix_key','_pix_key')
		           ORDER BY meta_key DESC LIMIT 1),
		        ''
		    ) AS pix_key,
		    COALESCE(NULLIF(u.settings->>'pix_key_tipo',''), '') AS pix_tipo
		 FROM senderzz_portal_users u
		 WHERE u.id = $1 AND u.role = 'afiliado'
		 LIMIT 1`, portalID).
		Scan(&wpUserID, &d.Nome, &d.Email, &d.Telefone, &d.CPF, &d.PixKey, &d.PixTipo)
	if err != nil {
		httpx.Err(w, 404, "not_found", "afiliado não encontrado")
		return
	}

	// Conta PIX do afiliado: settings JSONB (não sz_cod_withdraw_accounts). Só
	// existe uma; só anexamos se houver pix_key resolvida.
	if d.PixKey != "" {
		d.Contas = append(d.Contas, affiliateConta{
			Nome:      d.Nome,
			PixKey:    d.PixKey,
			PixType:   d.PixTipo,
			IsDefault: true,
		})
	}

	// Vínculos: senderzz_affiliates WHERE afiliado_id = wp_user_id. Usa o wp_user_id
	// DERIVADO (não o portal id do path) — afiliado_id é wp_user_id em todo o código
	// (List l.145, Commissions l.404, producers l.277). NÃO espelhar o OR(id, wp_user_id)
	// do produtor: produtor_id guarda as duas convenções, afiliado_id não — um OR aqui
	// reintroduziria a colisão portal/wp (List l.102-105) e vazaria vínculos de terceiro.
	// wp_user_id NULL → 0 linhas (vínculos vazio), sem guard. Produtor via join canônico
	// (id OR wp_user_id) com role='produtor'. Produto via sz_products (wp_post_id = a.produto_id).
	rows, err := h.Pool.Query(ctx,
		`SELECT
		    COALESCE(MAX(p.nome),'') AS produtor_nome,
		    a.produto_id,
		    (SELECT sp.nome FROM sz_products sp WHERE sp.wp_post_id = a.produto_id LIMIT 1) AS produto_nome,
		    MAX(COALESCE(a.comissao_pct, 0))::float8 AS comissao_pct,
		    COALESCE(MAX(a.status),'') AS status
		 FROM senderzz_affiliates a
		 LEFT JOIN senderzz_portal_users p
		        ON (p.id = a.produtor_id OR p.wp_user_id = a.produtor_id) AND p.role = 'produtor'
		 WHERE a.afiliado_id = $1
		 GROUP BY a.produto_id
		 ORDER BY produtor_nome, a.produto_id`, wpUserID)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var v affiliateVinculo
		if err := rows.Scan(&v.ProdutorNome, &v.ProdutoID, &v.ProdutoNome, &v.ComissaoPct, &v.Status); err != nil {
			continue
		}
		d.Vinculos = append(d.Vinculos, v)
	}

	// Resumo de TAXAS COBRADAS do afiliado (3 fontes, atribuídas pelo wp_user_id —
	// ver doc da struct affiliateTaxas). wpUserID NULL → todas as somas zeram via
	// COALESCE (sem guard). A taxa de transação usa a forma via_commission (join por
	// order_id, exclui comissão cancelada/revertida) p/ casar com a tela de Comissões.
	// Falha aqui não derruba o detalhe — as taxas degradam para 0.
	_ = h.Pool.QueryRow(ctx,
		`SELECT
		    COALESCE((
		        SELECT SUM(o.transaction_fee) FROM senderzz_affiliate_transactions t
		          JOIN senderzz_affiliates a ON a.id = t.affiliate_id
		          JOIN sz_orders o ON o.id = t.order_id
		         WHERE a.afiliado_id = $1
		           AND t.type = 'commission'
		           AND t.status NOT IN ('cancelled','reversed')
		    ), 0)::float8 AS taxa_transacao_total,
		    COALESCE((
		        SELECT SUM(t.amount) FROM senderzz_affiliate_transactions t
		          JOIN senderzz_affiliates a ON a.id = t.affiliate_id
		         WHERE a.afiliado_id = $1
		           AND t.type = 'penalty'
		           AND t.status IN ('approved','paid')
		    ), 0)::float8 AS penalidades_frustracao_total,
		    COALESCE((
		        SELECT SUM(wd.fee) FROM senderzz_affiliate_withdrawals wd
		          JOIN senderzz_affiliates a ON a.id = wd.affiliate_id
		         WHERE a.afiliado_id = $1
		           AND wd.status IN ('approved','paid')
		    ), 0)::float8 AS taxa_saque_total`, wpUserID).
		Scan(&d.Taxas.TaxaTransacaoTotal, &d.Taxas.PenalidadesFrustracaoTotal, &d.Taxas.TaxaSaqueTotal)
	d.Taxas.TotalTaxas = d.Taxas.TaxaTransacaoTotal + d.Taxas.PenalidadesFrustracaoTotal + d.Taxas.TaxaSaqueTotal

	// AUDIT-2026-06-21 #8/#18 (LGPD-PII-AUDIT): o drawer de detalhe expõe CPF, chave
	// PIX e telefone do afiliado. Registra o acesso (subject = portal id do afiliado).
	// Best-effort, espelha order_detail.go. Não bloqueia a request.
	if actor := auth.FromCtx(ctx); actor != nil {
		logPIIAccess(ctx, h.Pool, actor.ID, actor.Email, "affiliate", portalID,
			[]string{"cpf", "pix_key", "telefone"}, "view", r.RemoteAddr)
	} else {
		logPIIAccess(ctx, h.Pool, 0, "", "affiliate", portalID,
			[]string{"cpf", "pix_key", "telefone"}, "view", r.RemoteAddr)
	}

	httpx.JSON(w, 200, d)
}

// ─── PUT /affiliates/{id} ────────────────────────────────────────────────
// (PUT, não PATCH: o CORS AllowedMethods do admin não inclui PATCH — todas as
// mutações do painel usam PUT, idem producers.go Update.)
// Edição administrativa do afiliado: CPF + telefone MANUAIS. Paridade com o
// produtor (producers.go Update), porém a FONTE é DIFERENTE: afiliado guarda
// cpf/telefone em senderzz_portal_user_meta (meta_key '_billing_cpf' /
// '_billing_phone'), chaveada pelo PORTAL id (user_id = u.id) — exatamente como
// a List/Detail LÊEM esses metas. NÃO derivar wp_user_id (footgun do projeto):
// a meta é inequivocamente keyed por portal id aqui.
//
// {id} = portal_users.id (o user_id servido pela List → consumido pela UI),
// MESMO param `{id}` do Detail/wallet-fix (chi panica em conflito de nome de
// wildcard no mesmo nó da árvore — usar `{id}`, não `{user_id}`).
//
// UPSERT atômico em senderzz_portal_user_meta. Constraint confirmada no DB:
// UNIQUE (user_id, meta_key) → ON CONFLICT (user_id, meta_key) é válido; as duas
// linhas do VALUES diferem em meta_key (sem "cannot affect row a second time").
// Sanitiza CPF/telefone para SÓ DÍGITOS (onlyDigits, onboarding.go) — campo
// limpo vira meta_value '' (não apaga a linha; a List já trata '' como vazio).
type affiliateUpdateReq struct {
	CPF   string `json:"cpf"`
	Phone string `json:"phone"`
}

func (h *AffiliatesHandler) Update(w http.ResponseWriter, r *http.Request) {
	portalID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || portalID <= 0 {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	var req affiliateUpdateReq
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	cpf := onlyDigits(req.CPF)
	phone := onlyDigits(req.Phone)
	ctx := r.Context()

	// Gate de existência/404 + role (paridade com producers.go Update, que faz
	// WHERE ... role='produtor'). A meta UPSERT não tem checagem de role própria
	// — esse pré-check garante que só escrevemos metas de um AFILIADO real.
	var exists bool
	err = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM senderzz_portal_users WHERE id = $1 AND role = 'afiliado')`,
		portalID).Scan(&exists)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if !exists {
		httpx.Err(w, 404, "not_found", "afiliado não encontrado")
		return
	}

	// UPSERT atômico das duas metas (cpf + telefone) — chaveadas pelo portal id.
	_, err = h.Pool.Exec(ctx,
		`INSERT INTO senderzz_portal_user_meta (user_id, meta_key, meta_value)
		 VALUES ($1, '_billing_cpf', $2), ($1, '_billing_phone', $3)
		 ON CONFLICT (user_id, meta_key)
		 DO UPDATE SET meta_value = EXCLUDED.meta_value`,
		portalID, cpf, phone)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// FIX-2026-07-03: o portal do afiliado (/portal/me → "Sua conta") lê CPF/telefone
	// das COLUNAS senderzz_portal_users.document/.phone, não das metas acima. Sem
	// espelhar aqui, o que o admin cadastra NÃO aparece pro afiliado. Só grava quando
	// o valor vem preenchido (não zera o existente se o admin deixar o campo em branco).
	_, err = h.Pool.Exec(ctx,
		`UPDATE senderzz_portal_users
		    SET document = CASE WHEN $2 <> '' THEN $2 ELSE document END,
		        phone    = CASE WHEN $3 <> '' THEN $3 ELSE phone END
		  WHERE id = $1`,
		portalID, cpf, phone)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// Trilha de accountability (best-effort): escrita de PII (CPF/telefone).
	if actor := auth.FromCtx(ctx); actor != nil {
		logPIIAccess(ctx, h.Pool, actor.ID, actor.Email, "affiliate", portalID,
			[]string{"cpf", "phone"}, "update", r.RemoteAddr)
	}

	httpx.JSON(w, 200, map[string]any{
		"ok":      true,
		"user_id": portalID,
		"cpf":     cpf,
		"phone":   phone,
	})
}
