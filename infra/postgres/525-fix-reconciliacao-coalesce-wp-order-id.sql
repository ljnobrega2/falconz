-- AUDIT-2026-07-31 — bug cosmético na minha própria view sz_revenue_reconciliacao
-- (521): o CTE "esperado" pra taxa_transacao_produtor usa
-- `m.wc_order_id = o.wp_order_id` SEM o fallback COALESCE(o.wp_order_id, o.id)
-- usado em todo resto do sistema (checkout.go, tracking.go, sz_financials_refresh,
-- etc). Pedidos nativos Go (wp_order_id NULL) com motoboy real ficavam de fora
-- da soma "esperado" — 8 pedidos em produção, R$75,59 de divergência falsa.
-- Não afeta dinheiro nenhum (é só o relatório de comparação), mas corrige o
-- número exibido.
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
    SELECT 'taxa_transacao_produtor'::varchar(40),
           COALESCE(SUM(ROUND(
               COALESCE(o.total,0) * COALESCE(
                   (SELECT (NULLIF(value,'')::jsonb ->> o.produtor_id::text)::numeric
                      FROM senderzz_options WHERE name='sz_producer_fee_rules'),
                   (SELECT prod_pct FROM cfg)
               ) / 100, 2)), 0)::numeric(14,2)
    FROM sz_orders o
    WHERE o.status IN ('completo','entregue') AND COALESCE(o.total,0) > 0
      AND EXISTS (SELECT 1 FROM sz_motoboy_pedidos m
                   WHERE m.wc_order_id = COALESCE(o.wp_order_id, o.id))
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
    'soma esperada derivada de sz_orders pela regra oficial. AUDIT-2026-07-31: '
    'fix do COALESCE(wp_order_id,id) no EXISTS de taxa_transacao_produtor (pedidos '
    'nativos Go ficavam de fora da soma esperada). divergencia = ledger − esperado. '
    'alerta = TRUE quando |divergencia| > R$1.';
