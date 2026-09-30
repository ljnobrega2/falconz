-- AUDIT-2026-07-31 CORREÇÃO DE ERRO MEU — reverte 524.
--
-- 524 mudou taxa_entrega no ledger (senderzz_revenue) de BRUTO pra MARKUP
-- (delivery_fee - repasse), achando que era bug. ERRADO: existe migration
-- 501-revenue-entrega-gross.sql (nunca aplicada em prod, mas presente no
-- repo) com comentário explícito "REVERSÃO: taxa_entrega volta a ser BRUTA
-- (pedido do DONO, confirmado 2026-07-14)". A função viva em produção
-- (508, antes de qualquer mudança minha hoje) já gravava NEW.delivery_fee
-- bruto — condizente com a decisão do dono, não com o "bug" que eu supus.
-- Reverto pra bruto. Mantenho meu gate is_motoboy em taxa_transacao_produtor
-- (regra separada, confirmada pelo dono nesta sessão: PAD nunca tem essa
-- taxa nem afiliado).
--
-- Também reverte 526: os 8 pedidos frustrados antigos (1380,1381,1394,1500,
-- 1546,1562,1566,1569) não tinham NENHUM lançamento no ledger antes de hoje
-- (trigger não existia quando foram criados). 526 forçou o trigger a rodar
-- pra eles, criando lançamentos taxa_entrega novos que não existiam no
-- backup pré-mudanças. Se pedido frustrado deve ou não gerar taxa_entrega
-- é pergunta em aberto (arquivos 502/503 não-aplicados discordam entre si
-- e da regra viva) — não decido isso sozinho. Volto esses 8 pro estado do
-- backup (sem lançamento), deixa a decisão pro dono.

CREATE OR REPLACE FUNCTION sz_revenue_capture_order()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    prod_pct numeric;
    is_motoboy boolean;
BEGIN
    is_motoboy := EXISTS (SELECT 1 FROM sz_motoboy_pedidos m WHERE m.wc_order_id = COALESCE(NEW.wp_order_id, NEW.id));

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

-- (2) Corrige as 37 linhas que 524 mudou pra markup: volta pra bruto.
UPDATE senderzz_revenue r
   SET amount = o.delivery_fee
  FROM sz_orders o
 WHERE r.order_id = o.id
   AND r.component = 'taxa_entrega'
   AND r.amount <> o.delivery_fee;

-- (3) Reverte backfill de 526 pros 8 pedidos frustrados antigos: volta ao
-- estado do backup (sem lançamento nenhum) até o dono decidir a regra.
DELETE FROM senderzz_revenue
 WHERE order_id IN (1380,1381,1394,1500,1546,1562,1566,1569);

-- (4) sz_revenue_reconciliacao (525) esperava markup — corrige "esperado"
-- de taxa_entrega de volta pra bruto, senão acusa divergência falsa contra
-- o valor certo agora. Frustrado incluído no esperado (bate com regra viva
-- do trigger acima), mas os 8 pedidos revertidos no item (3) ficam sem
-- lançamento no ledger até decisão do dono — então voltam a aparecer como
-- divergência real (esperado), não bug: é o gap documentado, propositalmente
-- não fechado.
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
    'soma esperada derivada de sz_orders pela regra oficial. AUDIT-2026-07-31 (527): '
    'taxa_entrega volta a BRUTA (reverte erro de 524/525). divergencia = ledger − '
    'esperado. alerta = TRUE quando |divergencia| > R$1. Divergência residual nos 8 '
    'pedidos frustrados antigos é esperada até decisão do dono (ver 527).';
