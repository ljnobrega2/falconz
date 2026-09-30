-- AUDIT-2026-07-31 — decisão do dono: pedido frustrado NÃO gera taxa_entrega
-- no ledger da plataforma (senderzz_revenue). Antes (527) incluía frustrado
-- junto com completo/entregue. Exclui.

CREATE OR REPLACE FUNCTION sz_revenue_capture_order()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    prod_pct numeric;
    is_motoboy boolean;
BEGIN
    is_motoboy := EXISTS (SELECT 1 FROM sz_motoboy_pedidos m WHERE m.wc_order_id = COALESCE(NEW.wp_order_id, NEW.id));

    IF NEW.status IN ('completo','entregue') AND COALESCE(NEW.delivery_fee,0) > 0 THEN
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

-- Remove lançamentos já gravados de taxa_entrega pra pedidos frustrados
-- (não deveriam existir sob a regra nova).
DELETE FROM senderzz_revenue r
 WHERE r.component = 'taxa_entrega'
   AND EXISTS (SELECT 1 FROM sz_orders o WHERE o.id = r.order_id AND o.status = 'frustrado');

-- Reconciliação: esperado também exclui frustrado agora.
CREATE OR REPLACE VIEW sz_revenue_reconciliacao AS
WITH cfg AS (
    SELECT
        COALESCE((SELECT NULLIF(value,'')::numeric FROM senderzz_options
                   WHERE name = 'sz_producer_transaction_fee_pct'), 4.99) AS prod_pct
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
           COALESCE(SUM(o.delivery_fee), 0)::numeric(14,2)
    FROM sz_orders o
    WHERE o.status IN ('completo','entregue') AND COALESCE(o.delivery_fee,0) > 0
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
    'Reconciliação por componente. AUDIT-2026-07-31 (528): frustrado NÃO gera '
    'taxa_entrega (decisão do dono). divergencia = ledger − esperado. alerta = '
    'TRUE quando |divergencia| > R$1.';
