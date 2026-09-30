// Handler de PRODUTORES (senderzz_portal_users WHERE role='produtor').
//
// GET /producers                  → lista produtores + KPIs (vendas / afiliados / comissão pendente)
// GET /producers/{user_id}/detail → drawer de detalhe (contas PIX + afiliados vinculados)
//
// CHAVES DE ATRIBUIÇÃO (espelhadas dos handlers já validados — NÃO derivar novas):
//   - sz_orders.produtor_id          = senderzz_portal_users.id  (portal id) — ver cod_livro.go ProducersSummary.
//   - sz_products.produtor_id        = senderzz_portal_users.id  (portal id) — ver products.go (pu.id = sp.produtor_id).
//   - senderzz_affiliates.produtor_id = id OU wp_user_id (join canônico, role='produtor') — ver affiliates.go Commissions.
//   - sz_cod_withdraw_accounts.user_id = wp_user_id (produtores) — ver cod_wallet_producer.go.
//
// Misturar essas três convenções é a classe de bug recorrente (nomes vazios / cross-attribution).
package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/auth"
	"github.com/senderzz/admin-service/internal/httpx"
)

type ProducersHandler struct{ Pool *pgxpool.Pool }

type producer struct {
	UserID                    int64   `json:"user_id"` // portal_users.id
	Nome                      string  `json:"nome"`
	Email                     string  `json:"email"`
	Telefone                  string  `json:"telefone"`                    // meta _billing_phone (portal id) → fallback u.phone — MED37
	CPF                       string  `json:"cpf"`                         // meta _billing_cpf (portal id) → u.document → sz_cod_withdraw_accounts.holder_cpf — MED37
	PixKey                    string  `json:"pix_key"`                     // sz_cod_withdraw_accounts (conta padrão) com fallback settings/meta
	ValorVendidoTotal         float64 `json:"valor_vendido_total"`         // SUM(sz_orders.total) all-time WHERE produtor_id = u.id
	ComissaoPendenteAfiliados float64 `json:"comissao_pendente_afiliados"` // ledger pendente dos afiliados deste produtor
	AfiliadosCount            int64   `json:"afiliados_count"`
	ProdutosCount             int64   `json:"produtos_count"`
	Status                    string  `json:"status"`
	CreatedAt                 string  `json:"created_at"`
	// Flag de expedição por produtor (sem migration): senderzz_portal_users.settings->>'expedicao_ativa'.
	// AUDIT-2026-07-11: opt-IN — default FALSE quando ausente (nasce desativada, admin liga explicitamente).
	ExpedicaoAtiva bool `json:"expedicao_ativa"`
}

// List — GET /producers
func (h *ProducersHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	search := strings.TrimSpace(q.Get("q"))

	// Subqueries escalares correlacionam só por u.id / u.wp_user_id (já no GROUP
	// BY implícito da linha) — sem fan-out, sem GROUP BY extra. Aliases internos
	// distintos (a2 / sp / t) para não colidir com nada do escopo externo.
	rows, err := h.Pool.Query(r.Context(),
		`SELECT
		    u.id AS user_id,
		    COALESCE(u.nome,'')  AS nome,
		    COALESCE(u.email,'') AS email,
		    -- MED37: telefone/CPF reais. id-space JÁ correto (meta keyed por PORTAL
		    -- id = u.id — ver affiliates_portal.go upsertProducerMeta; a meta WC
		    -- raramente é populada no schema migrado). Fallbacks p/ as colunas
		    -- diretas (u.phone / u.document) e, para CPF, p/ holder_cpf da conta de
		    -- saque COD (sz_cod_withdraw_accounts é chaveada por wp_user_id — mesma
		    -- fonte já usada abaixo para pix_key).
		    COALESCE(
		        NULLIF((SELECT meta_value FROM senderzz_portal_user_meta
		                 WHERE user_id = u.id AND meta_key = '_billing_phone' LIMIT 1), ''),
		        NULLIF(u.phone, ''),
		        ''
		    ) AS telefone,
		    COALESCE(
		        NULLIF((SELECT meta_value FROM senderzz_portal_user_meta
		                 WHERE user_id = u.id AND meta_key = '_billing_cpf' LIMIT 1), ''),
		        NULLIF(u.document, ''),
		        NULLIF((SELECT holder_cpf FROM sz_cod_withdraw_accounts
		                 WHERE user_id = u.wp_user_id AND active = TRUE
		                 ORDER BY is_default DESC, id ASC LIMIT 1), ''),
		        ''
		    ) AS cpf,
		    -- PIX: conta padrão do produtor (sz_cod_withdraw_accounts é chaveada por
		    -- wp_user_id) com fallback para settings JSONB / metas legadas.
		    COALESCE(
		        NULLIF((SELECT pix_key FROM sz_cod_withdraw_accounts
		                 WHERE user_id = u.wp_user_id AND active = TRUE
		                 ORDER BY is_default DESC, id ASC LIMIT 1), ''),
		        NULLIF(u.settings->>'pix_key',''),
		        (SELECT meta_value FROM senderzz_portal_user_meta
		           WHERE user_id = u.id AND meta_key IN ('_senderzz_pix_key','_pix_key')
		           ORDER BY meta_key DESC LIMIT 1),
		        ''
		    ) AS pix_key,
		    -- Valor vendido all-time: sz_orders.produtor_id = portal id (u.id).
		    COALESCE((
		        SELECT SUM(o.total)::float8 FROM sz_orders o
		         WHERE o.produtor_id = u.id
		    ), 0)::float8 AS valor_vendido_total,
		    -- Comissão pendente dos afiliados deste produtor: LEDGER por vínculo do
		    -- produtor (join canônico id OR wp_user_id), status='pending'.
		    COALESCE((
		        SELECT SUM(t.amount)::float8
		          FROM senderzz_affiliate_transactions t
		          JOIN senderzz_affiliates a2 ON a2.id = t.affiliate_id
		         WHERE (a2.produtor_id = u.id OR a2.produtor_id = u.wp_user_id)
		           AND t.status = 'pending'
		    ), 0)::float8 AS comissao_pendente_afiliados,
		    -- Afiliados distintos vinculados (join canônico do produtor).
		    COALESCE((
		        SELECT COUNT(DISTINCT a2.afiliado_id)
		          FROM senderzz_affiliates a2
		         WHERE a2.produtor_id = u.id OR a2.produtor_id = u.wp_user_id
		    ), 0) AS afiliados_count,
		    -- Produtos do produtor: sz_products.produtor_id = portal id (u.id).
		    COALESCE((
		        SELECT COUNT(*) FROM sz_products sp WHERE sp.produtor_id = u.id
		    ), 0) AS produtos_count,
		    CASE WHEN u.ativo THEN 'ativo' ELSE 'inativo' END AS status,
		    u.created_at::text AS created_at,
		    -- Flag de expedição (sem migration): settings->>'expedicao_ativa'. Default
		    -- FALSE (opt-in) quando a chave está ausente/NULL — AUDIT-2026-07-11.
		    COALESCE((u.settings->>'expedicao_ativa')::boolean, false) AS expedicao_ativa
		 FROM senderzz_portal_users u
		 WHERE u.role = 'produtor'
		   AND u.ativo
		   AND ($1 = '' OR u.email ILIKE '%' || $1 || '%' OR u.nome ILIKE '%' || $1 || '%')
		 ORDER BY u.created_at DESC
		 LIMIT $2 OFFSET $3`, search, limit, offset)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []producer{}
	for rows.Next() {
		var p producer
		_ = rows.Scan(&p.UserID, &p.Nome, &p.Email,
			&p.Telefone, &p.CPF, &p.PixKey,
			&p.ValorVendidoTotal, &p.ComissaoPendenteAfiliados,
			&p.AfiliadosCount, &p.ProdutosCount,
			&p.Status, &p.CreatedAt, &p.ExpedicaoAtiva)
		out = append(out, p)
	}

	var total int64
	_ = h.Pool.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM senderzz_portal_users
		 WHERE role = 'produtor'
		   AND ativo
		   AND ($1 = '' OR email ILIKE '%' || $1 || '%' OR nome ILIKE '%' || $1 || '%')`,
		search).Scan(&total)

	// AUDIT-2026-06-21 #8/#18 (LGPD-PII-AUDIT): a listagem de produtores serve CPF e
	// chave PIX de cada titular em massa (acesso tipo-export). Registra na trilha de
	// accountability — escopo = nº de produtores retornados. Best-effort, espelha
	// order_detail.go. Não bloqueia a request.
	ctx := r.Context()
	if actor := auth.FromCtx(ctx); actor != nil {
		logPIIAccess(ctx, h.Pool, actor.ID, actor.Email, "producer", int64(len(out)),
			[]string{"cpf", "pix_key", "telefone"}, "export", r.RemoteAddr)
	} else {
		logPIIAccess(ctx, h.Pool, 0, "", "producer", int64(len(out)),
			[]string{"cpf", "pix_key", "telefone"}, "export", r.RemoteAddr)
	}

	httpx.JSON(w, 200, map[string]any{"items": out, "total": total})
}

// ─── GET /producers/{user_id}/detail ─────────────────────────────────────
// {user_id} = portal_users.id (produtor).

// producerConta — conta PIX do produtor (sz_cod_withdraw_accounts WHERE user_id=wp_user_id).
type producerConta struct {
	Nome      string `json:"nome"`
	PixKey    string `json:"pix_key"`
	PixType   string `json:"pix_type"`
	IsDefault bool   `json:"is_default"`
}

// producerAfiliado — afiliado vinculado ao produtor (uma oferta/produto).
type producerAfiliado struct {
	AfiliadoNome string  `json:"afiliado_nome"`
	ProdutoID    *int64  `json:"produto_id"`
	ProdutoNome  *string `json:"produto_nome"`
	ComissaoPct  float64 `json:"comissao_pct"`
	Status       string  `json:"status"`
}

type producerDetail struct {
	UserID   int64  `json:"user_id"`
	Nome     string `json:"nome"`
	Email    string `json:"email"`
	Telefone string `json:"telefone"`
	CPF      string `json:"cpf"`
	PixKey   string `json:"pix_key"`
	PixTipo  string `json:"pix_tipo"`
	// Flag de expedição por produtor — espelha o campo da List (settings->>'expedicao_ativa', default FALSE/opt-in).
	ExpedicaoAtiva bool `json:"expedicao_ativa"`
	// Frete fixo por transportadora (nil = sem override, usa markup normal).
	FreteFixoCorreios *float64           `json:"frete_fixo_correios"`
	FreteFixoOutras   *float64           `json:"frete_fixo_outras"`
	BloqueioCorreios  bool               `json:"bloqueio_correios"`
	LinkMistoAtivo    bool               `json:"link_misto_ativo"`
	Contas            []producerConta    `json:"contas"`
	Afiliados         []producerAfiliado `json:"afiliados"`
}

func (h *ProducersHandler) Detail(w http.ResponseWriter, r *http.Request) {
	// {user_id} = portal_users.id (produtor) — assimetria proposital com o detalhe
	// do afiliado, que usa wp_user_id.
	portalID, err := strconv.ParseInt(chi.URLParam(r, "user_id"), 10, 64)
	if err != nil || portalID <= 0 {
		httpx.Err(w, 400, "bad_request", "user_id inválido")
		return
	}
	ctx := r.Context()

	d := producerDetail{
		UserID:    portalID,
		Contas:    []producerConta{},
		Afiliados: []producerAfiliado{},
	}

	// Cabeçalho. Resolve wp_user_id (chave de sz_cod_withdraw_accounts) na mesma
	// query para o bloco de contas abaixo.
	var wpUserID *int64
	err = h.Pool.QueryRow(ctx,
		`SELECT
		    u.wp_user_id,
		    COALESCE(u.nome,'')  AS nome,
		    COALESCE(u.email,'') AS email,
		    -- MED37: mesmos fallbacks da List (telefone/CPF). id-space já correto.
		    COALESCE(
		        NULLIF((SELECT meta_value FROM senderzz_portal_user_meta
		                 WHERE user_id = u.id AND meta_key = '_billing_phone' LIMIT 1), ''),
		        NULLIF(u.phone, ''),
		        ''
		    ) AS telefone,
		    COALESCE(
		        NULLIF((SELECT meta_value FROM senderzz_portal_user_meta
		                 WHERE user_id = u.id AND meta_key = '_billing_cpf' LIMIT 1), ''),
		        NULLIF(u.document, ''),
		        NULLIF((SELECT holder_cpf FROM sz_cod_withdraw_accounts
		                 WHERE user_id = u.wp_user_id AND active = TRUE
		                 ORDER BY is_default DESC, id ASC LIMIT 1), ''),
		        ''
		    ) AS cpf,
		    COALESCE(NULLIF(u.settings->>'pix_key',''), '')      AS pix_key,
		    COALESCE(NULLIF(u.settings->>'pix_key_tipo',''), '') AS pix_tipo,
		    -- Flag de expedição (sem migration): default FALSE (opt-in) quando ausente — AUDIT-2026-07-11.
		    COALESCE((u.settings->>'expedicao_ativa')::boolean, false) AS expedicao_ativa,
		    (u.settings->>'frete_fixo_correios')::numeric AS frete_fixo_correios,
		    (u.settings->>'frete_fixo_outras')::numeric   AS frete_fixo_outras,
		    COALESCE((u.settings->>'bloqueio_correios')::boolean, false) AS bloqueio_correios,
		    COALESCE((u.settings->>'link_misto_ativo')::boolean, false) AS link_misto_ativo
		 FROM senderzz_portal_users u
		 WHERE u.id = $1 AND u.role = 'produtor'
		 LIMIT 1`, portalID).
		Scan(&wpUserID, &d.Nome, &d.Email, &d.Telefone, &d.CPF, &d.PixKey, &d.PixTipo, &d.ExpedicaoAtiva,
			&d.FreteFixoCorreios, &d.FreteFixoOutras, &d.BloqueioCorreios, &d.LinkMistoAtivo)
	if err != nil {
		httpx.Err(w, 404, "not_found", "produtor não encontrado")
		return
	}

	// Contas PIX: sz_cod_withdraw_accounts (chaveada por wp_user_id, produtores).
	if wpUserID != nil && h.tableExists(ctx, "sz_cod_withdraw_accounts") {
		accRows, accErr := h.Pool.Query(ctx,
			`SELECT
			    COALESCE(holder_name,''),
			    COALESCE(pix_key,''),
			    COALESCE(pix_type,''),
			    COALESCE(is_default, false) IS TRUE
			 FROM sz_cod_withdraw_accounts
			 WHERE user_id = $1 AND active = TRUE
			 ORDER BY is_default DESC, id ASC`, *wpUserID)
		if accErr == nil {
			for accRows.Next() {
				var c producerConta
				if scanErr := accRows.Scan(&c.Nome, &c.PixKey, &c.PixType, &c.IsDefault); scanErr == nil {
					d.Contas = append(d.Contas, c)
				}
			}
			accRows.Close()
		}
	}
	// Se nenhuma conta cadastrada e há pix_key em settings, expõe como fallback.
	if len(d.Contas) == 0 && d.PixKey != "" {
		d.Contas = append(d.Contas, producerConta{
			Nome:      d.Nome,
			PixKey:    d.PixKey,
			PixType:   d.PixTipo,
			IsDefault: true,
		})
	}
	// Paridade com a List: produtor cadastra PIX em sz_cod_withdraw_accounts (não
	// em settings). Se o pix_key de topo veio vazio do settings mas há conta, sobe
	// a primeira (default) para os campos de topo — senão o header mostra '' com
	// o dado visível logo abaixo em contas[].
	if d.PixKey == "" && len(d.Contas) > 0 {
		d.PixKey = d.Contas[0].PixKey
		d.PixTipo = d.Contas[0].PixType
	}

	// Afiliados vinculados: senderzz_affiliates por join canônico do produtor,
	// JOIN portal_users do afiliado por wp_user_id. Produto via sz_products.
	rows, err := h.Pool.Query(ctx,
		`SELECT
		    COALESCE(af.nome,'') AS afiliado_nome,
		    a.produto_id,
		    (SELECT sp.nome FROM sz_products sp WHERE sp.wp_post_id = a.produto_id LIMIT 1) AS produto_nome,
		    COALESCE(a.comissao_pct, 0)::float8 AS comissao_pct,
		    COALESCE(a.status,'') AS status
		 FROM senderzz_affiliates a
		 JOIN senderzz_portal_users af ON af.wp_user_id = a.afiliado_id
		 WHERE (a.produtor_id = $1 OR a.produtor_id = $2)
		 ORDER BY a.id DESC`, portalID, wpUserID)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var af producerAfiliado
		if err := rows.Scan(&af.AfiliadoNome, &af.ProdutoID, &af.ProdutoNome, &af.ComissaoPct, &af.Status); err != nil {
			continue
		}
		d.Afiliados = append(d.Afiliados, af)
	}

	// AUDIT-2026-06-21 #8/#18 (LGPD-PII-AUDIT): o drawer de detalhe expõe CPF, chave
	// PIX e contas bancárias do produtor. Registra o acesso (subject = portal id do
	// produtor). Best-effort, espelha order_detail.go. Não bloqueia a request.
	if actor := auth.FromCtx(ctx); actor != nil {
		logPIIAccess(ctx, h.Pool, actor.ID, actor.Email, "producer", portalID,
			[]string{"cpf", "pix_key", "telefone", "contas"}, "view", r.RemoteAddr)
	} else {
		logPIIAccess(ctx, h.Pool, 0, "", "producer", portalID,
			[]string{"cpf", "pix_key", "telefone", "contas"}, "view", r.RemoteAddr)
	}

	httpx.JSON(w, 200, d)
}

// ─── PUT /producers/{user_id} ────────────────────────────────────────────
// (PUT, não PATCH: o CORS AllowedMethods do admin não inclui PATCH e todas as
// mutações do painel usam PUT — ver users/cds/products em main.go.)
// Edição administrativa do produtor: CPF (document) + telefone (phone) MANUAIS
// (#84 — sem esses dados o saque do produtor mostra "CPF do cadastro indisponível",
// pois document/phone vêm vazios da migração) e flag de EXPEDIÇÃO por produtor
// (M — settings->>'expedicao_ativa', sem migration).
//
// {user_id} = portal_users.id (produtor) — mesma chave do Detail/Delete.
// Único statement: grava as colunas diretas document/phone e o flag dentro de
// settings via jsonb_set (cria a chave se não existir, COALESCE protege settings
// NULL). NULLIF($,”) guarda NULL quando o campo vem vazio (mantém a coluna limpa
// em vez de string vazia).
type producerUpdateReq struct {
	Document       string `json:"document"` // CPF
	Phone          string `json:"phone"`    // telefone
	ExpedicaoAtiva bool   `json:"expedicao_ativa"`
	// Frete fixo por transportadora, POR PRODUTOR (pedido dono 2026-07-27):
	// preço ABSOLUTO (substitui o markup pct/fixed), aplicado no checkout (fonte:
	// go/orders/internal/handlers/freight.go applyFixedFreight) e lido — não
	// recalculado — na emissão (go/labels/internal/handlers/emit.go) via
	// sz_order_meta._sz_freight_price, pra checkout e cobrança nunca divergirem.
	// nil = sem override (mantém markup normal); ponteiro pra distinguir de 0.
	FreteFixoCorreios *float64 `json:"frete_fixo_correios"`
	FreteFixoOutras   *float64 `json:"frete_fixo_outras"`
	// Bloqueia Correios e trava a escolha na mais barata entre as demais
	// transportadoras (settings->>'bloqueio_correios'). Pedido dono 2026-07-27:
	// produtor só trabalha com transportadora privada, cliente não pode escolher.
	// Ver go/orders/internal/handlers/freight.go applyCorreiosLock.
	BloqueioCorreios bool `json:"bloqueio_correios"`
	// Link misto (pedido dono 2026-07-27): opt-in, nasce desativado. Ativo → o
	// portal cria UM link só por oferta (tipo='misto') que decide COD ou Expedição
	// pelo CEP no checkout, sem o cliente escolher. Ver go/orders freight.go
	// resolveDeliveryMode e go/portal links_portal.go PostOfferLink.
	LinkMistoAtivo bool `json:"link_misto_ativo"`
}

func (h *ProducersHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "user_id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "user_id inválido")
		return
	}
	var req producerUpdateReq
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	req.Document = strings.TrimSpace(req.Document)
	req.Phone = strings.TrimSpace(req.Phone)

	ct, err := h.Pool.Exec(r.Context(),
		`UPDATE senderzz_portal_users
		    SET document = NULLIF($1, ''),
		        phone    = NULLIF($2, ''),
		        settings = jsonb_set(
		                     jsonb_set(
		                       jsonb_set(
		                         jsonb_set(
		                           jsonb_set(
		                             COALESCE(settings, '{}'::jsonb),
		                             '{expedicao_ativa}',
		                             to_jsonb($3::boolean),
		                             true),
		                           '{frete_fixo_correios}',
		                           CASE WHEN $5::numeric IS NULL THEN COALESCE(settings->'frete_fixo_correios', 'null'::jsonb) ELSE to_jsonb($5::numeric) END,
		                           true),
		                         '{frete_fixo_outras}',
		                         CASE WHEN $6::numeric IS NULL THEN COALESCE(settings->'frete_fixo_outras', 'null'::jsonb) ELSE to_jsonb($6::numeric) END,
		                         true),
		                       '{bloqueio_correios}',
		                       to_jsonb($7::boolean),
		                       true),
		                     '{link_misto_ativo}',
		                     to_jsonb($8::boolean),
		                     true)
		  WHERE id = $4 AND role = 'produtor'`,
		req.Document, req.Phone, req.ExpedicaoAtiva, id, req.FreteFixoCorreios, req.FreteFixoOutras, req.BloqueioCorreios, req.LinkMistoAtivo)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if ct.RowsAffected() == 0 {
		httpx.Err(w, 404, "not_found", "produtor não encontrado")
		return
	}

	// BOOTSTRAP carteira TPC ao ligar Expedição: sem isso o produtor liga o flag,
	// tenta recarregar, e a tela "Recarga de frete" trabalha sobre uma linha que
	// não existe (dono reportou 2026-07-27: "nem tem carteira feita"). tpc_carteira
	// é keyed por c.user_id = portal_users.id (produtor nativo Falk sem wp_user_id)
	// — mesmo id-space usado no restante deste handler (id = portal_users.id).
	// ON CONFLICT idempotente: liga/desliga o flag repetidas vezes não duplica linha
	// nem zera saldo existente.
	if req.ExpedicaoAtiva {
		if _, err := h.Pool.Exec(r.Context(),
			`INSERT INTO tpc_carteira (user_id, saldo, saldo_reservado)
			 VALUES ($1, 0, 0)
			 ON CONFLICT (user_id) DO NOTHING`,
			id,
		); err != nil {
			slog.Error("[admin_producers] bootstrap tpc_carteira falhou", "user_id", id, "err", err)
		}
		if err := backfillExpedicaoLinks(r.Context(), h.Pool, id); err != nil {
			slog.Error("[admin_producers] backfill links expedição falhou", "user_id", id, "err", err)
		}
	}

	// Trilha de accountability (best-effort): escrita de PII (CPF/telefone).
	ctx := r.Context()
	if actor := auth.FromCtx(ctx); actor != nil {
		logPIIAccess(ctx, h.Pool, actor.ID, actor.Email, "producer", id,
			[]string{"document", "phone", "expedicao_ativa"}, "update", r.RemoteAddr)
	}

	httpx.JSON(w, 200, map[string]any{
		"ok":              true,
		"user_id":         id,
		"document":        req.Document,
		"phone":           req.Phone,
		"expedicao_ativa": req.ExpedicaoAtiva,
	})
}

// ─── DELETE /producers/{user_id} ─────────────────────────────────────────
// SOFT-DELETE (convenção do projeto — ver CLAUDE.md "Webhook delete = soft-delete").
// Um produtor cruza TRÊS id-spaces (pedidos por u.id; afiliados por id OU wp_user_id;
// produtos/COD por chave própria — ver header deste arquivo). Hard-DELETE arrisca
// violação de FK / órfãos e é IRREVERSÍVEL, destruindo histórico financeiro. Aqui
// apenas desativamos (u.ativo=false): o produtor some da listagem (List filtra
// `AND u.ativo`), o ledger/pedidos permanecem intactos e é reversível.
//
// CASCADE (N parte a): ao excluir o produtor também APAGAMOS os checkouts dele
// (DELETE FROM senderzz_checkout_links WHERE producer_id = id). producer_id é o
// portal_users.id — MESMO id-space do {user_id} aqui (confirmado no schema:
// producer_id bigint = u.id). O produtor permanece em SOFT-delete (ativo=false):
// a justificativa de FK/histórico financeiro acima continua valendo para a linha
// do usuário; apenas os checkouts (sem valor histórico financeiro) são removidos
// em hard-delete. Tudo numa transação — a ordem importa: desativamos o produtor
// PRIMEIRO e só seguimos para o cascade se a linha existir (RowsAffected>0), senão
// rollback + 404 (não apagamos checkouts de um id inválido).
func (h *ProducersHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "user_id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "user_id inválido")
		return
	}
	ctx := r.Context()

	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op após Commit

	// 1) Soft-delete do produtor (ativo=false). Gate de existência/404.
	ct, err := tx.Exec(ctx,
		`UPDATE senderzz_portal_users SET ativo = false
		  WHERE id = $1 AND role = 'produtor'`, id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if ct.RowsAffected() == 0 {
		httpx.Err(w, 404, "not_found", "produtor não encontrado")
		return
	}

	// 2) Cascade: hard-delete dos checkouts vinculados (producer_id = portal id).
	delCt, err := tx.Exec(ctx,
		`DELETE FROM senderzz_checkout_links WHERE producer_id = $1`, id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// Trilha de accountability (best-effort, não bloqueia).
	if actor := auth.FromCtx(ctx); actor != nil {
		logPIIAccess(ctx, h.Pool, actor.ID, actor.Email, "producer", id,
			[]string{"ativo"}, "delete", r.RemoteAddr)
	}
	httpx.JSON(w, 200, map[string]any{
		"ok":                true,
		"user_id":           id,
		"checkouts_deleted": delCt.RowsAffected(),
	})
}

// tableExists — checagem genérica (espelha cod_wallet_producer.go). Evita 500
// quando sz_cod_withdraw_accounts ainda não foi migrada do MySQL.
func (h *ProducersHandler) tableExists(ctx context.Context, name string) bool {
	return tableExistsCached(ctx, h.Pool, name) // AUDIT-2026-06-18 Onda2 (go-infoschema-cache)
}

// backfillExpedicaoLinks — AUDIT-2026-07-27: quando um produtor liga Expedição
// DEPOIS de já ter ofertas Cash on Delivery, o par "correio" (Expedição) só era
// criado para ofertas NOVAS (go/portal links_portal.go Create, quando
// expedicaoAtiva já está true no momento da criação). Ofertas existentes ficavam
// só com o link motoboy — dono reportou (2026-07-27): "ele já tem links de cash
// on delivery... preciso que seja revisto". Aqui espelhamos o MESMO par que
// Create() monta (link correio irmão do motoboy, nome = base_name, sufixo
// " — Motoboy" no motoboy pra pareamento — ver products.go: mb.name = cl.name ||
// ' — Motoboy'), mas em backfill: para toda oferta motoboy do produtor SEM par
// correio ainda.
//
// URL idêntica ao portal: /checkout/?sz=<token> (checkout-ui resolve tipo pelo
// backend, não pela URL — ver checkoutURL/codCheckoutURL em links_portal.go).
func backfillExpedicaoLinks(ctx context.Context, pool *pgxpool.Pool, producerID int64) error {
	rows, err := pool.Query(ctx,
		`SELECT m.id, m.post_id, m.display_value, m.price_label, m.affiliate_visible,
		        m.affiliate_commission_pct, m.base_name, m.composition_items, m.name
		   FROM senderzz_checkout_links m
		  WHERE m.producer_id = $1 AND m.tipo = 'motoboy'
		    AND NOT EXISTS (
		        SELECT 1 FROM senderzz_checkout_links c
		         WHERE c.producer_id = m.producer_id
		           AND c.tipo = 'correio'
		           AND c.base_name = m.base_name
		    )`,
		producerID,
	)
	if err != nil {
		return err
	}
	type motoboyRow struct {
		id                     int64
		postID                 int64
		displayValue           float64
		priceLabel             string
		affiliateVisible       bool
		affiliateCommissionPct float64
		baseName               string
		compositionItems       []byte
		name                   string
	}
	var toBackfill []motoboyRow
	for rows.Next() {
		var m motoboyRow
		if err := rows.Scan(&m.id, &m.postID, &m.displayValue, &m.priceLabel, &m.affiliateVisible,
			&m.affiliateCommissionPct, &m.baseName, &m.compositionItems, &m.name); err == nil {
			toBackfill = append(toBackfill, m)
		}
	}
	rows.Close()

	base := strings.TrimRight(os.Getenv("CHECKOUT_PUBLIC_URL"), "/")
	if base == "" {
		base = "https://app.falklog.com.br"
	}

	for _, m := range toBackfill {
		if m.baseName == "" {
			continue // sem base_name não há como parear com segurança — pula.
		}
		token, err := randomHexToken(16)
		if err != nil {
			slog.Error("[admin_producers] backfillExpedicaoLinks: token falhou", "motoboy_id", m.id, "err", err)
			continue
		}
		checkoutURL := base + "/checkout/?sz=" + url.QueryEscape(token)

		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO senderzz_checkout_links
			     (producer_id, post_id, token, tipo, url, display_value, price_label,
			      affiliate_visible, name, slug, affiliate_commission_pct, base_name,
			      composition_items, created_at)
			 VALUES ($1, $2, $3, 'correio', $4, $5, $6, $7, $8, $3, $9, $10, $11, NOW())`,
			producerID, m.postID, token, checkoutURL, m.displayValue, m.priceLabel,
			m.affiliateVisible, m.baseName, m.affiliateCommissionPct, m.baseName, m.compositionItems,
		); err != nil {
			_ = tx.Rollback(ctx)
			slog.Error("[admin_producers] backfillExpedicaoLinks: insert correio falhou", "motoboy_id", m.id, "err", err)
			continue
		}
		// Sufixo de pareamento — só renomeia se ainda não tiver (idempotente).
		if !strings.HasSuffix(m.name, " — Motoboy") {
			if _, err := tx.Exec(ctx,
				`UPDATE senderzz_checkout_links SET name = $1 WHERE id = $2`,
				m.baseName+" — Motoboy", m.id,
			); err != nil {
				_ = tx.Rollback(ctx)
				slog.Error("[admin_producers] backfillExpedicaoLinks: rename motoboy falhou", "motoboy_id", m.id, "err", err)
				continue
			}
		}
		if err := tx.Commit(ctx); err != nil {
			slog.Error("[admin_producers] backfillExpedicaoLinks: commit falhou", "motoboy_id", m.id, "err", err)
		}
	}
	return nil
}

// randomHexToken — mesmo padrão de randomHex em go/portal/affiliates_portal.go
// (token de link, 16 bytes = 32 chars hex).
func randomHexToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
