-- =============================================================================
-- BLINDAGEM FINANCEIRA DO FALK LOG — fonte única de cálculo + guardas + reconciliação
-- =============================================================================
-- Motivação (auditoria 2026-06-19): a REGRA DE CÁLCULO oficial do dono estava
-- replicada em Go, React e nos triggers de receita. Cada cópia podia divergir.
-- Este arquivo cristaliza a regra DENTRO do banco como fonte única ("calc-everywhere"),
-- adiciona guardas (CHECK constraints) que impedem valores negativos de entrar no
-- ledger, e cria uma view de reconciliação para alertar divergências do faturamento.
--
-- ESCOPO: SÓ adiciona objetos novos (2 views + CHECK constraints aditivas).
--   NÃO altera nenhuma tabela/coluna/trigger/dado existente.
--   NÃO toca Go nem React. NÃO reinicia serviços.
--
-- IDEMPOTENTE: views via CREATE OR REPLACE; constraints só são adicionadas dentro
--   de um DO-block que (a) confere que a constraint ainda não existe e (b) confere
--   ZERO violações no dado atual. 2ª passada = no-op.
--
-- =============================================================================
-- REGRA DE CÁLCULO OFICIAL (do dono — implementada fiel nas views abaixo)
-- =============================================================================
--   bruta                     = affiliate_amount + transaction_fee
--                               (transaction_fee = taxa 4,99% do AFILIADO, valor REAL gravado)
--   taxa_transacao_afiliado   = transaction_fee
--   comissao_afiliado_liquida = affiliate_amount
--   taxa_entrega              = delivery_fee
--                               (markup FALK = delivery_fee − repasse;
--                                repasse vem de senderzz_options.motoboy_repasse_padrao, default 18)
--   taxa_transacao_produtor   = total × (sz_producer_transaction_fee_pct / 100), default pct 4,99
--   liquido_produtor          = total − bruta − delivery_fee − taxa_transacao_produtor
--
--   Frustrado/cancelled/reembolsado → liquido_produtor = 0 E taxa_transacao_produtor = 0,
--   MAS apenas se NÃO houver entrega motoboy ativa (entrega ativa = pedido motoboy
--   existente com status <> 'cancelado'). Com entrega ativa, o produtor ainda deve a
--   taxa/líquido normais (o serviço foi prestado).
--
--   take_falk (receita FALK por pedido) = taxa_transacao_afiliado
--                                       + taxa_transacao_produtor
--                                       + GREATEST(delivery_fee − repasse, 0)   (markup de entrega)
--
-- PROVA (pedido wp_order_id=1587): total 250 / affiliate_amount 142.51 / transaction_fee 7.49
--   / delivery_fee 23.98 → bruta 150, taxa_afiliado 7.49, líquida 142.51, entrega 23.98,
--   taxa_produtor 250×0,0499=12.48, líquido_produtor 250−150−23.98−12.48 = 63.54,
--   take_falk 7.49+12.48+max(23.98−18,0)=7.49+12.48+5.98 = 25.95.
-- =============================================================================


-- =============================================================================
-- (1) VIEW sz_order_financeiro — FONTE ÚNICA de cálculo por pedido (read-only)
-- =============================================================================
-- Aplica a REGRA OFICIAL acima, lendo as taxas configuráveis de senderzz_options
-- com COALESCE aos defaults. "entrega ativa" via EXISTS correlacionado em
-- sz_motoboy_pedidos (link wc_order_id → wp_order_id). EXISTS (não JOIN) porque
-- a relação é 1:N (reagendamentos/redeliveries) — JOIN multiplicaria linhas e
-- quebraria "uma linha por pedido".
CREATE OR REPLACE VIEW sz_order_financeiro AS
SELECT
    o.id                                                    AS order_id,
    o.wp_order_id                                           AS wp_order_id,
    o.status                                                AS status,
    o.produtor_id                                           AS produtor_id,
    o.affiliate_id                                          AS affiliate_id,

    -- valor cheio do pedido
    o.total                                                 AS valor_pedido,

    -- bloco AFILIADO
    (COALESCE(o.affiliate_amount,0) + COALESCE(o.transaction_fee,0))::numeric(12,2)
                                                            AS comissao_afiliado_bruta,
    COALESCE(o.transaction_fee,0)::numeric(12,2)            AS taxa_transacao_afiliado,
    COALESCE(o.affiliate_amount,0)::numeric(12,2)           AS comissao_afiliado_liquida,

    -- taxa de ENTREGA (valor cheio cobrado — o markup FALK entra só no take_falk)
    COALESCE(o.delivery_fee,0)::numeric(12,2)               AS taxa_entrega,

    -- flag de frustrado/cancelado/reembolsado SEM entrega motoboy ativa.
    -- "entrega ativa" = existe pedido motoboy não-cancelado para este wp_order_id.
    -- (escolha documentada: 1587 é 'pending', não exercita este ramo — sem caso de prova.)
    (
        o.status IN ('frustrado','cancelled','reembolsado')
        AND NOT EXISTS (
            SELECT 1 FROM sz_motoboy_pedidos m
            WHERE m.wc_order_id = o.wp_order_id
              AND m.status <> 'cancelado'
        )
    )                                                       AS frustrado,

    -- taxa de transação do PRODUTOR = total × pct (configurável). Zera quando
    -- frustrado-sem-entrega-ativa (CASE espelha a flag 'frustrado' acima).
    CASE
        WHEN o.status IN ('frustrado','cancelled','reembolsado')
             AND NOT EXISTS (
                 SELECT 1 FROM sz_motoboy_pedidos m
                 WHERE m.wc_order_id = o.wp_order_id
                   AND m.status <> 'cancelado'
             )
        THEN 0::numeric(12,2)
        ELSE ROUND(
                 COALESCE(o.total,0)
                 * COALESCE(
                     (SELECT NULLIF(value,'')::numeric FROM senderzz_options
                       WHERE name = 'sz_producer_transaction_fee_pct'),
                     4.99
                   ) / 100,
                 2
             )::numeric(12,2)
    END                                                     AS taxa_transacao_produtor,

    -- líquido do PRODUTOR = total − bruta − delivery_fee − taxa_transacao_produtor.
    -- Zera quando frustrado-sem-entrega-ativa (mesma condição da taxa).
    CASE
        WHEN o.status IN ('frustrado','cancelled','reembolsado')
             AND NOT EXISTS (
                 SELECT 1 FROM sz_motoboy_pedidos m
                 WHERE m.wc_order_id = o.wp_order_id
                   AND m.status <> 'cancelado'
             )
        THEN 0::numeric(12,2)
        ELSE (
            COALESCE(o.total,0)
            - (COALESCE(o.affiliate_amount,0) + COALESCE(o.transaction_fee,0))   -- − bruta
            - COALESCE(o.delivery_fee,0)                                          -- − taxa_entrega
            - ROUND(
                  COALESCE(o.total,0)
                  * COALESCE(
                      (SELECT NULLIF(value,'')::numeric FROM senderzz_options
                        WHERE name = 'sz_producer_transaction_fee_pct'),
                      4.99
                    ) / 100,
                  2
              )                                                                   -- − taxa_transacao_produtor
        )::numeric(12,2)
    END                                                     AS liquido_produtor,

    -- TAKE FALK = taxa_afiliado + taxa_produtor + markup de entrega (delivery − repasse, piso 0).
    (
        COALESCE(o.transaction_fee,0)
        + ROUND(
              COALESCE(o.total,0)
              * COALESCE(
                  (SELECT NULLIF(value,'')::numeric FROM senderzz_options
                    WHERE name = 'sz_producer_transaction_fee_pct'),
                  4.99
                ) / 100,
              2
          )
        + GREATEST(
              COALESCE(o.delivery_fee,0)
              - COALESCE(
                  (SELECT NULLIF(value,'')::numeric FROM senderzz_options
                    WHERE name = 'motoboy_repasse_padrao'),
                  18
                ),
              0
          )
    )::numeric(12,2)                                        AS take_falk
FROM sz_orders o;

COMMENT ON VIEW sz_order_financeiro IS
    'Fonte única (calc-everywhere) da regra de cálculo financeira por pedido. '
    'Read-only: aplica a regra oficial do dono. Taxas configuráveis lidas de '
    'senderzz_options com COALESCE aos defaults (produtor 4,99%, repasse R$18). '
    'frustrado/liquido_produtor=0 só quando frustrado/cancelado/reembolsado E sem '
    'entrega motoboy ativa (status motoboy <> cancelado).';


-- =============================================================================
-- (2) VIEW sz_revenue_reconciliacao — reconcilia ledger vs esperado (read-only)
-- =============================================================================
-- Por componente derivado de sz_orders, compara a soma JÁ GRAVADA em
-- senderzz_revenue (realizado) contra a soma ESPERADA pela regra oficial.
-- "divergencia" = ledger − esperado. Útil para alerta de divergência > R$1.
--
-- ATENÇÃO: o esperado segue a REGRA OFICIAL (gating de status do trigger v6),
-- NUNCA é ajustado para casar com o ledger atual. Divergências de backfill
-- antigo (ex.: taxa_entrega gravada cheia em vez de markup) DEVEM aparecer aqui.
--
-- Gating do esperado (espelha o trigger sz_revenue_capture_order v6):
--   taxa_afiliado_4_99      → SUM(transaction_fee)               em completo/entregue
--   taxa_transacao_produtor → SUM(ROUND(total×pct/100,2))        em completo/entregue (round por pedido)
--   taxa_entrega            → SUM(GREATEST(delivery_fee−repasse,0)) em completo/entregue/frustrado (MARKUP, não cheio)
--
-- Fora de escopo (não derivam de sz_orders): taxa_saque, taxa_antecipacao,
--   taxa_frustrado. Não entram nesta reconciliação.
CREATE OR REPLACE VIEW sz_revenue_reconciliacao AS
WITH cfg AS (
    SELECT
        COALESCE((SELECT NULLIF(value,'')::numeric FROM senderzz_options
                   WHERE name = 'sz_producer_transaction_fee_pct'), 4.99) AS prod_pct,
        COALESCE((SELECT NULLIF(value,'')::numeric FROM senderzz_options
                   WHERE name = 'motoboy_repasse_padrao'), 18)            AS repasse
),
esperado AS (
    -- taxa_afiliado_4_99: realiza em completo/entregue
    SELECT 'taxa_afiliado_4_99'::varchar(40) AS component,
           COALESCE(SUM(o.transaction_fee), 0)::numeric(14,2) AS esperado
    FROM sz_orders o
    WHERE o.status IN ('completo','entregue') AND COALESCE(o.transaction_fee,0) > 0
    UNION ALL
    -- taxa_transacao_produtor: realiza em completo/entregue (round por pedido, depois soma)
    SELECT 'taxa_transacao_produtor'::varchar(40),
           COALESCE(SUM(ROUND(COALESCE(o.total,0) * (SELECT prod_pct FROM cfg) / 100, 2)), 0)::numeric(14,2)
    FROM sz_orders o
    WHERE o.status IN ('completo','entregue') AND COALESCE(o.total,0) > 0
    UNION ALL
    -- taxa_entrega: realiza em completo/entregue/frustrado — MARKUP (delivery − repasse, piso 0)
    SELECT 'taxa_entrega'::varchar(40),
           COALESCE(SUM(GREATEST(COALESCE(o.delivery_fee,0) - (SELECT repasse FROM cfg), 0)), 0)::numeric(14,2)
    FROM sz_orders o
    WHERE o.status IN ('completo','entregue','frustrado') AND COALESCE(o.delivery_fee,0) > 0
),
ledger AS (
    SELECT component, COALESCE(SUM(amount),0)::numeric(14,2) AS gravado
    FROM senderzz_revenue
    WHERE component IN ('taxa_afiliado_4_99','taxa_transacao_produtor','taxa_entrega')
    GROUP BY component
)
SELECT
    e.component                                            AS componente,
    COALESCE(l.gravado, 0)                                 AS ledger_gravado,
    e.esperado                                             AS esperado,
    (COALESCE(l.gravado, 0) - e.esperado)::numeric(14,2)   AS divergencia,
    (ABS(COALESCE(l.gravado, 0) - e.esperado) > 1.00)      AS alerta
FROM esperado e
LEFT JOIN ledger l ON l.component = e.component
ORDER BY e.component;

COMMENT ON VIEW sz_revenue_reconciliacao IS
    'Reconciliação por componente: soma gravada em senderzz_revenue (realizado) vs '
    'soma esperada derivada de sz_orders pela regra oficial (gating do trigger v6). '
    'divergencia = ledger − esperado. alerta = TRUE quando |divergencia| > R$1. '
    'Escopo: só componentes derivados de pedido (afiliado/produtor/entrega).';


-- =============================================================================
-- (3) CHECK constraints SEGURAS E ADITIVAS — guarda de não-negatividade
-- =============================================================================
-- Cada constraint só é adicionada se: (a) ainda não existe (idempotência) E
-- (b) ZERO linhas violam o predicado no dado atual (segurança — não quebra dado).
-- Se houver violação, a constraint é PULADA e um NOTICE é emitido (documentação).
-- Postgres NÃO tem "ADD CONSTRAINT IF NOT EXISTS" — daí o DO-block manual.
DO $blindagem$
DECLARE
    -- (tabela, constraint, coluna): predicado sempre é "coluna >= 0".
    alvo        RECORD;
    n_violacoes BIGINT;
    ja_existe   BOOLEAN;
BEGIN
    FOR alvo IN
        SELECT * FROM (VALUES
            ('sz_orders',                  'sz_orders_delivery_fee_nonneg',     'delivery_fee'),
            ('sz_orders',                  'sz_orders_transaction_fee_nonneg',  'transaction_fee'),
            ('sz_orders',                  'sz_orders_affiliate_amount_nonneg', 'affiliate_amount'),
            ('sz_orders',                  'sz_orders_total_nonneg',            'total'),
            ('sz_cod_wallet_transactions', 'sz_cod_wtx_net_nonneg',             'net'),
            ('sz_cod_wallet_transactions', 'sz_cod_wtx_gross_nonneg',           'gross'),
            ('sz_cod_wallet_transactions', 'sz_cod_wtx_fee_nonneg',             'fee')
        ) AS t(tabela, constraint_nome, coluna)
    LOOP
        -- (a) idempotência: já existe a constraint?
        SELECT EXISTS (
            SELECT 1 FROM pg_constraint
            WHERE conname  = alvo.constraint_nome
              AND conrelid = alvo.tabela::regclass
        ) INTO ja_existe;

        IF ja_existe THEN
            RAISE NOTICE '[blindagem] % já existe em % — pulada (idempotente).',
                alvo.constraint_nome, alvo.tabela;
            CONTINUE;
        END IF;

        -- (b) segurança: conta violações no dado atual.
        EXECUTE format('SELECT count(*) FROM %I WHERE %I < 0', alvo.tabela, alvo.coluna)
            INTO n_violacoes;

        IF n_violacoes > 0 THEN
            RAISE NOTICE '[blindagem] % NÃO aplicada: % linha(s) com %.% < 0. DOCUMENTAR e investigar.',
                alvo.constraint_nome, n_violacoes, alvo.tabela, alvo.coluna;
            CONTINUE;
        END IF;

        -- aplica: 0 violações garantido.
        EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I CHECK (%I >= 0)',
                       alvo.tabela, alvo.constraint_nome, alvo.coluna);
        RAISE NOTICE '[blindagem] % aplicada em %.% (>= 0).',
            alvo.constraint_nome, alvo.tabela, alvo.coluna;
    END LOOP;
END;
$blindagem$;


-- =============================================================================
-- (4) PROPOSTA FUTURA (NÃO aplicada aqui — risco com o dono dormindo)
-- =============================================================================
-- IMUTABILIDADE DO LEDGER DE RECEITA: proibir UPDATE/DELETE de linhas de
-- senderzz_revenue já "bookadas" (receita realizada não deve ser editada nem
-- apagada manualmente — só estornos via trigger de pedido controlado).
--
-- Esboço (NÃO descomentar sem validação com o dono — vários triggers de receita
-- HOJE fazem DELETE legítimo no estorno de cancelled/reembolsado; uma trava cega
-- quebraria o fluxo de estorno existente):
--
--   CREATE OR REPLACE FUNCTION sz_revenue_imutavel()
--   RETURNS TRIGGER LANGUAGE plpgsql AS $$
--   BEGIN
--       RAISE EXCEPTION 'senderzz_revenue é imutável: % proibido (ref=%).',
--           TG_OP, COALESCE(OLD.ref, NEW.ref);
--   END; $$;
--   CREATE TRIGGER trg_sz_revenue_imutavel
--       BEFORE UPDATE OR DELETE ON senderzz_revenue
--       FOR EACH ROW EXECUTE FUNCTION sz_revenue_imutavel();
--
-- Antes de adotar: migrar os DELETEs de estorno para um caminho whitelistado
-- (ex.: session GUC tipo set_config('sz.revenue_estorno','1',true)) para que só
-- o estorno controlado possa remover linhas. Documentado como roadmap.
-- =============================================================================
