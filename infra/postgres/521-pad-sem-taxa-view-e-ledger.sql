-- AUDIT-2026-07-30 CRITICAL (dono): "para PAD [expedição] não tem taxa de
-- transação e essas taxas são personalizáveis e não fixas" — continuação de
-- 520 (sz_order_financials). Este arquivo corrige as DUAS peças que faltavam
-- do mesmo bug: a view "golden" sz_order_financeiro e o TRIGGER que grava a
-- receita real em senderzz_revenue.
--
-- IMPORTANTE: a base real de sz_order_financeiro NÃO é mais 390 (superada) —
-- é 422-frustracao-view.sql (FRUST-V2, com colunas taxa_frustracao_produtor/
-- afiliado anexadas no fim). Confirmado testando contra clone real do banco
-- dev (senderzz_audit_test) — uma primeira tentativa desta migração, escrita
-- a partir de 390, tentou fazer CREATE OR REPLACE removendo essas 2 colunas e
-- o Postgres corretamente recusou ("cannot drop columns from view"). Esta
-- versão preserva TODA a lógica FRUST-V2 (penalty congelada) intocada — só
-- adiciona o gate motoboy nos ramos NORMAIS (não-frustrado) de
-- taxa_transacao_produtor/liquido_produtor/take_falk.
--
-- Nota: taxa do PRODUTOR já é personalizável por produtor desde 508
-- (sz_producer_fee_rules, JSON por produtor_id) — isso já estava certo no
-- TRIGGER, mas a VIEW (422) nunca tinha adotado o override — só o global. Esta
-- migração também traz a view pra paridade com o trigger nesse ponto.
--
-- Critério canônico (mesmo de 520/tracking.go/422): EXISTS em
-- sz_motoboy_pedidos pelo wc_order_id = motoboy; ausência = PAD/expedição.

-- ── (1) VIEW sz_order_financeiro (base fiel: 422-frustracao-view.sql) ──────
CREATE OR REPLACE VIEW sz_order_financeiro AS
 SELECT o.id AS order_id,
    o.wp_order_id,
    o.status,
    o.produtor_id,
    o.affiliate_id,
    o.total AS valor_pedido,

    -- ----- bloco AFILIADO (zerado quando cobra frustração) — INTOCADO (422) --
    CASE WHEN (
        EXISTS (SELECT 1 FROM sz_motoboy_pedidos m
                 WHERE m.wc_order_id = COALESCE(o.wp_order_id, o.id)
                   AND m.status::text = 'frustrado'::text)
        OR (
          (o.status::text = ANY (ARRAY['frustrado'::character varying, 'cancelled'::character varying, 'reembolsado'::character varying]::text[]))
          AND NOT (EXISTS (SELECT 1 FROM sz_motoboy_pedidos m
                            WHERE m.wc_order_id = o.wp_order_id AND m.status::text <> 'cancelado'::text))
        )
      ) THEN 0::numeric(12,2)
      ELSE (COALESCE(o.affiliate_amount, 0::numeric) + COALESCE(o.transaction_fee, 0::numeric))::numeric(12,2)
    END AS comissao_afiliado_bruta,

    CASE WHEN (
        EXISTS (SELECT 1 FROM sz_motoboy_pedidos m
                 WHERE m.wc_order_id = COALESCE(o.wp_order_id, o.id)
                   AND m.status::text = 'frustrado'::text)
        OR (
          (o.status::text = ANY (ARRAY['frustrado'::character varying, 'cancelled'::character varying, 'reembolsado'::character varying]::text[]))
          AND NOT (EXISTS (SELECT 1 FROM sz_motoboy_pedidos m
                            WHERE m.wc_order_id = o.wp_order_id AND m.status::text <> 'cancelado'::text))
        )
      ) THEN 0::numeric(12,2)
      ELSE COALESCE(o.transaction_fee, 0::numeric)::numeric(12,2)
    END AS taxa_transacao_afiliado,

    CASE WHEN (
        EXISTS (SELECT 1 FROM sz_motoboy_pedidos m
                 WHERE m.wc_order_id = COALESCE(o.wp_order_id, o.id)
                   AND m.status::text = 'frustrado'::text)
        OR (
          (o.status::text = ANY (ARRAY['frustrado'::character varying, 'cancelled'::character varying, 'reembolsado'::character varying]::text[]))
          AND NOT (EXISTS (SELECT 1 FROM sz_motoboy_pedidos m
                            WHERE m.wc_order_id = o.wp_order_id AND m.status::text <> 'cancelado'::text))
        )
      ) THEN 0::numeric(12,2)
      ELSE COALESCE(o.affiliate_amount, 0::numeric)::numeric(12,2)
    END AS comissao_afiliado_liquida,

    -- ----- taxa de entrega (zerada quando cobra frustração) — INTOCADO (422) -
    CASE WHEN (
        EXISTS (SELECT 1 FROM sz_motoboy_pedidos m
                 WHERE m.wc_order_id = COALESCE(o.wp_order_id, o.id)
                   AND m.status::text = 'frustrado'::text)
        OR (
          (o.status::text = ANY (ARRAY['frustrado'::character varying, 'cancelled'::character varying, 'reembolsado'::character varying]::text[]))
          AND NOT (EXISTS (SELECT 1 FROM sz_motoboy_pedidos m
                            WHERE m.wc_order_id = o.wp_order_id AND m.status::text <> 'cancelado'::text))
        )
      ) THEN 0::numeric(12,2)
      ELSE COALESCE(o.delivery_fee, 0::numeric)::numeric(12,2)
    END AS taxa_entrega,

    -- ----- flag `frustrado` — INTOCADO (422) --------------------------------
    (
      EXISTS (SELECT 1 FROM sz_motoboy_pedidos m
               WHERE m.wc_order_id = COALESCE(o.wp_order_id, o.id)
                 AND m.status::text = 'frustrado'::text)
      OR (
        (o.status::text = ANY (ARRAY['frustrado'::character varying, 'cancelled'::character varying, 'reembolsado'::character varying]::text[]))
        AND NOT (EXISTS (SELECT 1 FROM sz_motoboy_pedidos m
                          WHERE m.wc_order_id = o.wp_order_id AND m.status::text <> 'cancelado'::text))
      )
    ) AS frustrado,

    -- ----- bloco PRODUTOR — AUDIT-2026-07-30: gate motoboy ADICIONADO ------
    -- Fora do gate motoboy (PAD/expedição) → 0, SEMPRE, independente de
    -- frustrado (PAD nunca teve essa taxa pra começo de conversa). Dentro do
    -- gate motoboy, preserva 100% a lógica FRUST-V2 (zera se frustrado sem
    -- entrega ativa) + override por produtor (paridade com o trigger 508).
    CASE
      WHEN NOT EXISTS (SELECT 1 FROM sz_motoboy_pedidos m WHERE m.wc_order_id = COALESCE(o.wp_order_id, o.id))
        THEN 0::numeric(12,2)
      WHEN (
        EXISTS (SELECT 1 FROM sz_motoboy_pedidos m
                 WHERE m.wc_order_id = COALESCE(o.wp_order_id, o.id)
                   AND m.status::text = 'frustrado'::text)
        OR (
          (o.status::text = ANY (ARRAY['frustrado'::character varying, 'cancelled'::character varying, 'reembolsado'::character varying]::text[]))
          AND NOT (EXISTS (SELECT 1 FROM sz_motoboy_pedidos m
                            WHERE m.wc_order_id = o.wp_order_id AND m.status::text <> 'cancelado'::text))
        )
      ) THEN 0::numeric(12,2)
      ELSE round(COALESCE(o.total, 0::numeric) * COALESCE(
                (SELECT (NULLIF(value,'')::jsonb ->> o.produtor_id::text)::numeric
                   FROM senderzz_options WHERE name='sz_producer_fee_rules'),
                ( SELECT NULLIF(senderzz_options.value, ''::text)::numeric
                    FROM senderzz_options
                   WHERE senderzz_options.name::text = 'sz_producer_transaction_fee_pct'::text),
                4.99) / 100::numeric, 2)::numeric(12,2)
    END AS taxa_transacao_produtor,

    -- AUDIT-2026-07-30: ordem IMPORTA — frustrado-zero tem que vencer ANTES do
    -- gate "sem motoboy" (achado ao vivo testando contra clone real: sem essa
    -- ordem, cenário (c) frustrado-sem-motoboy caía no ramo PAD-normal em vez
    -- de zerar, liq_prod=65 em vez de 0).
    CASE
      WHEN (
        EXISTS (SELECT 1 FROM sz_motoboy_pedidos m
                 WHERE m.wc_order_id = COALESCE(o.wp_order_id, o.id)
                   AND m.status::text = 'frustrado'::text)
        OR (
          (o.status::text = ANY (ARRAY['frustrado'::character varying, 'cancelled'::character varying, 'reembolsado'::character varying]::text[]))
          AND NOT (EXISTS (SELECT 1 FROM sz_motoboy_pedidos m
                            WHERE m.wc_order_id = o.wp_order_id AND m.status::text <> 'cancelado'::text))
        )
      ) THEN 0::numeric(12,2)
      WHEN NOT EXISTS (SELECT 1 FROM sz_motoboy_pedidos m WHERE m.wc_order_id = COALESCE(o.wp_order_id, o.id))
        THEN GREATEST(
                COALESCE(o.total, 0::numeric)
                - (COALESCE(o.affiliate_amount, 0::numeric) + COALESCE(o.transaction_fee, 0::numeric))
                - COALESCE(o.delivery_fee, 0::numeric), 0
             )::numeric(12,2)
      ELSE (COALESCE(o.total, 0::numeric)
            - (COALESCE(o.affiliate_amount, 0::numeric) + COALESCE(o.transaction_fee, 0::numeric))
            - COALESCE(o.delivery_fee, 0::numeric)
            - round(COALESCE(o.total, 0::numeric) * COALESCE(
                  (SELECT (NULLIF(value,'')::jsonb ->> o.produtor_id::text)::numeric
                     FROM senderzz_options WHERE name='sz_producer_fee_rules'),
                  ( SELECT NULLIF(senderzz_options.value, ''::text)::numeric
                      FROM senderzz_options
                     WHERE senderzz_options.name::text = 'sz_producer_transaction_fee_pct'::text),
                  4.99) / 100::numeric, 2))::numeric(12,2)
    END AS liquido_produtor,

    -- ----- take_falk — frustrado (cobra) INTOCADO; normal com gate motoboy -
    CASE WHEN (
        EXISTS (SELECT 1 FROM sz_motoboy_pedidos m
                 WHERE m.wc_order_id = COALESCE(o.wp_order_id, o.id)
                   AND m.status::text = 'frustrado'::text)
        OR (
          (o.status::text = ANY (ARRAY['frustrado'::character varying, 'cancelled'::character varying, 'reembolsado'::character varying]::text[]))
          AND NOT (EXISTS (SELECT 1 FROM sz_motoboy_pedidos m
                            WHERE m.wc_order_id = o.wp_order_id AND m.status::text <> 'cancelado'::text))
        )
      ) THEN (
        COALESCE((
            SELECT m.valor_taxa_frustrado
              FROM sz_motoboy_pedidos m
             WHERE m.wc_order_id = COALESCE(o.wp_order_id, o.id)
               AND m.status::text = 'frustrado'::text
             ORDER BY m.ts_frustrado DESC NULLS LAST, m.id DESC
             LIMIT 1
        ), 0::numeric)
        + COALESCE((
            SELECT m.valor_taxa_frustrado_afiliado
              FROM sz_motoboy_pedidos m
             WHERE m.wc_order_id = COALESCE(o.wp_order_id, o.id)
               AND m.status::text = 'frustrado'::text
             ORDER BY m.ts_frustrado DESC NULLS LAST, m.id DESC
             LIMIT 1
        ), 0::numeric)
      )::numeric(12,2)
      ELSE (COALESCE(o.transaction_fee, 0::numeric)
            + CASE WHEN EXISTS (SELECT 1 FROM sz_motoboy_pedidos m WHERE m.wc_order_id = COALESCE(o.wp_order_id, o.id))
                   THEN round(COALESCE(o.total, 0::numeric) * COALESCE(
                            (SELECT (NULLIF(value,'')::jsonb ->> o.produtor_id::text)::numeric
                               FROM senderzz_options WHERE name='sz_producer_fee_rules'),
                            ( SELECT NULLIF(senderzz_options.value, ''::text)::numeric
                                FROM senderzz_options
                               WHERE senderzz_options.name::text = 'sz_producer_transaction_fee_pct'::text),
                            4.99) / 100::numeric, 2)
                   ELSE 0::numeric
              END
            + GREATEST(COALESCE(o.delivery_fee, 0::numeric) - COALESCE(
                  ( SELECT NULLIF(senderzz_options.value, ''::text)::numeric
                      FROM senderzz_options
                     WHERE senderzz_options.name::text = 'motoboy_repasse_padrao'::text),
                  18::numeric), 0::numeric))::numeric(12,2)
    END AS take_falk,

    -- ----- penalties CONGELADAS — INTOCADO (422) ----------------------------
    COALESCE((
        SELECT m.valor_taxa_frustrado
          FROM sz_motoboy_pedidos m
         WHERE m.wc_order_id = COALESCE(o.wp_order_id, o.id)
           AND m.status::text = 'frustrado'::text
         ORDER BY m.ts_frustrado DESC NULLS LAST, m.id DESC
         LIMIT 1
    ), 0::numeric)::numeric(12,2) AS taxa_frustracao_produtor,

    COALESCE((
        SELECT m.valor_taxa_frustrado_afiliado
          FROM sz_motoboy_pedidos m
         WHERE m.wc_order_id = COALESCE(o.wp_order_id, o.id)
           AND m.status::text = 'frustrado'::text
         ORDER BY m.ts_frustrado DESC NULLS LAST, m.id DESC
         LIMIT 1
    ), 0::numeric)::numeric(12,2) AS taxa_frustracao_afiliado

   FROM sz_orders o;

COMMENT ON VIEW sz_order_financeiro IS
    'Fonte única (calc-everywhere) da regra de cálculo financeira por pedido. '
    'FRUST-V2 (422) preservado. AUDIT-2026-07-30: taxa_transacao_produtor/'
    'take_falk ZERADOS pra PAD/expedição (dono: "PAD não tem taxa de '
    'transação") — só COD/motoboy paga; override por produtor '
    '(sz_producer_fee_rules) agora também na view, paridade com o trigger.';

-- ── (2) TRIGGER sz_revenue_capture_order (ledger real, base: 508) ─────────
CREATE OR REPLACE FUNCTION sz_revenue_capture_order()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    prod_pct numeric;
    is_motoboy boolean;
BEGIN
    is_motoboy := EXISTS (SELECT 1 FROM sz_motoboy_pedidos m WHERE m.wc_order_id = NEW.wp_order_id);

    IF NEW.status IN ('completo','entregue','frustrado') AND COALESCE(NEW.delivery_fee,0) > 0 THEN
        INSERT INTO senderzz_revenue (order_id, produtor_id, component, base_amount, amount, ref, created_at)
        VALUES (NEW.id, NEW.produtor_id, 'taxa_entrega', COALESCE(NEW.total,0), NEW.delivery_fee,
                'order_entrega:' || NEW.id, COALESCE(NEW.updated_at, NEW.created_at, NOW()))
        ON CONFLICT (component, ref) DO NOTHING;
    END IF;

    IF NEW.status IN ('completo','entregue') THEN
        IF COALESCE(NEW.transaction_fee,0) > 0 THEN
            INSERT INTO senderzz_revenue (order_id, affiliate_id, component, base_amount, amount, ref, created_at)
            VALUES (NEW.id, NEW.affiliate_id, 'taxa_afiliado_4_99',
                    COALESCE(NEW.affiliate_amount,0) + NEW.transaction_fee, NEW.transaction_fee,
                    'order_aff:' || NEW.id, COALESCE(NEW.updated_at, NEW.created_at, NOW()))
            ON CONFLICT (component, ref) DO NOTHING;
        END IF;
        -- AUDIT-2026-07-30 CRITICAL: taxa de transação do PRODUTOR só existe
        -- pra COD/motoboy — PAD/expedição não paga (dono). Override por
        -- produtor (sz_producer_fee_rules, 508) preservado dentro do ramo
        -- motoboy.
        IF is_motoboy AND COALESCE(NEW.total,0) > 0 THEN
            prod_pct := COALESCE(
                (SELECT (NULLIF(value,'')::jsonb ->> NEW.produtor_id::text)::numeric
                   FROM senderzz_options WHERE name='sz_producer_fee_rules'),
                (SELECT NULLIF(value,'')::numeric FROM senderzz_options WHERE name='sz_producer_transaction_fee_pct'),
                4.99
            );
            INSERT INTO senderzz_revenue (order_id, produtor_id, component, base_amount, amount, ref, created_at)
            VALUES (NEW.id, NEW.produtor_id, 'taxa_transacao_produtor', NEW.total,
                    ROUND((NEW.total * prod_pct / 100)::numeric, 2),
                    'order_prodtx:' || NEW.id, COALESCE(NEW.updated_at, NEW.created_at, NOW()))
            ON CONFLICT (component, ref) DO NOTHING;
        END IF;
    END IF;

    IF NEW.status IN ('cancelled','reembolsado') THEN
        DELETE FROM senderzz_revenue WHERE order_id = NEW.id;
    END IF;
    RETURN NEW;
END; $$;

DROP TRIGGER IF EXISTS trg_sz_revenue_capture_order ON sz_orders;
CREATE TRIGGER trg_sz_revenue_capture_order
    AFTER INSERT OR UPDATE ON sz_orders
    FOR EACH ROW EXECUTE FUNCTION sz_revenue_capture_order();

-- ── (3) VIEW sz_revenue_reconciliacao (esperado vs realizado) ──────────────
-- Mesmo bug no CTE "esperado": somava taxa_transacao_produtor pra TODO pedido
-- completo/entregue, sem excluir PAD/expedição — geraria alerta de divergência
-- falso agora que o trigger (2) parou de gravar essa componente pra PAD.
-- Também troca cfg.prod_pct (global fixo) por override por produtor
-- (sz_producer_fee_rules) somado por linha, igual o trigger já faz.
CREATE OR REPLACE VIEW sz_revenue_reconciliacao AS
WITH cfg AS (
    SELECT
        COALESCE((SELECT NULLIF(value,'')::numeric FROM senderzz_options
                   WHERE name = 'sz_producer_transaction_fee_pct'), 4.99) AS prod_pct,
        COALESCE((SELECT NULLIF(value,'')::numeric FROM senderzz_options
                   WHERE name = 'motoboy_repasse_padrao'), 18)            AS repasse
),
esperado AS (
    SELECT 'taxa_afiliado_4_99'::varchar(40) AS component,
           COALESCE(SUM(o.transaction_fee), 0)::numeric(14,2) AS esperado
    FROM sz_orders o
    WHERE o.status IN ('completo','entregue') AND COALESCE(o.transaction_fee,0) > 0
    UNION ALL
    -- AUDIT-2026-07-30: SÓ pedidos motoboy/COD entram na soma esperada (PAD não
    -- tem taxa de transação); override por produtor (sz_producer_fee_rules)
    -- respeitado por linha, igual ao trigger.
    SELECT 'taxa_transacao_produtor'::varchar(40),
           COALESCE(SUM(ROUND(
               COALESCE(o.total,0) * COALESCE(
                   (SELECT (NULLIF(value,'')::jsonb ->> o.produtor_id::text)::numeric
                      FROM senderzz_options WHERE name='sz_producer_fee_rules'),
                   (SELECT prod_pct FROM cfg)
               ) / 100, 2)), 0)::numeric(14,2)
    FROM sz_orders o
    WHERE o.status IN ('completo','entregue') AND COALESCE(o.total,0) > 0
      AND EXISTS (SELECT 1 FROM sz_motoboy_pedidos m WHERE m.wc_order_id = o.wp_order_id)
    UNION ALL
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
    'AUDIT-2026-07-30: taxa_transacao_produtor esperada só soma pedidos motoboy/COD '
    '(PAD/expedição não tem taxa de transação) e respeita override por produtor. '
    'divergencia = ledger − esperado. alerta = TRUE quando |divergencia| > R$1.';
