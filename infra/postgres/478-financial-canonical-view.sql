-- ============================================================================
-- 478 — Estrutura financeira canônica (AUDIT-FINANCEIRO-2026-06-25).
--
-- Resolve o bug P0 "LÍQUIDO PRODUTOR errado": producer_net era coluna inerte
-- (sem writer, sem backfill) → telas liam valor stale (não descontava o afiliado).
--
-- Entrega:
--   1. sz_cod_delivery_fee — UMA variável global de taxa de entrega COD (editável
--      no menu Taxas; default 23.98 = valor do WP). Decisão do dono 2026-06-25.
--   2. sz_producer_tx_rate() — taxa de transação do produtor (lê sz_producer_transaction_fee_pct,
--      default 4,99%) como FRAÇÃO. Mantém a view consistente com o trigger de receita.
--   3. VIEW sz_order_financials — decomposição GOLDEN por pedido (fonte única).
--   4. Backfills SEGUROS: delivery_fee em pedidos COD sem taxa; producer_net vira
--      cache correto (nunca mais fonte de verdade — as telas leem a view).
-- Idempotente (ON CONFLICT / CREATE OR REPLACE / UPDATE só toca o que falta).
-- ============================================================================

-- 1) Variável única de taxa de entrega COD (editável no menu Taxas).
INSERT INTO senderzz_options (name, value)
VALUES ('sz_cod_delivery_fee', '23.98')
ON CONFLICT (name) DO NOTHING;

-- 2) Taxa de transação do produtor como FRAÇÃO (ex.: 4.99 -> 0.0499).
CREATE OR REPLACE FUNCTION sz_producer_tx_rate() RETURNS numeric
LANGUAGE sql STABLE AS $$
  SELECT COALESCE(
    (SELECT NULLIF(value,'')::numeric FROM senderzz_options WHERE name='sz_producer_transaction_fee_pct'),
    4.99) / 100.0
$$;

-- 3) Fonte ÚNICA de verdade: decomposição golden por pedido.
--    Invariante (quando líquido>=0): total = bruta + delivery_fee + producer_take + producer_net.
--    Cada tela aplica seu PRÓPRIO filtro de status (frustrado/recebido) e keyspace por cima.
DROP VIEW IF EXISTS sz_order_financials;
CREATE VIEW sz_order_financials AS
SELECT
  o.id                                                            AS order_id,
  o.produtor_id,                                                  -- keyspace: portal id
  o.affiliate_id,                                                 -- keyspace: wp_user_id
  o.status,
  o.payment_method,
  o.created_at,
  o.total,
  (COALESCE(o.affiliate_amount,0) + COALESCE(o.transaction_fee,0)) AS affiliate_bruta,   -- 150,00
  COALESCE(o.affiliate_amount,0)                                  AS affiliate_liquida,   -- 142,51
  COALESCE(o.transaction_fee,0)                                   AS affiliate_take,      -- 7,49 (4,99% da bruta)
  COALESCE(o.delivery_fee,0)                                      AS delivery_fee,        -- 23,98
  ROUND(o.total * sz_producer_tx_rate(), 2)                       AS producer_take,       -- 12,48 (4,99% do total)
  (COALESCE(o.transaction_fee,0) + ROUND(o.total * sz_producer_tx_rate(), 2)) AS senderzz_take, -- 19,97
  GREATEST(
     o.total
     - (COALESCE(o.affiliate_amount,0) + COALESCE(o.transaction_fee,0))
     - COALESCE(o.delivery_fee,0)
     - ROUND(o.total * sz_producer_tx_rate(), 2), 0)              AS producer_net_live     -- 63,54
FROM sz_orders o;

-- 4a) Backfill delivery_fee em pedidos COD que ficaram sem taxa (ex.: criados pelo
--     checkout Go, que ainda não gravava delivery_fee). Usa a variável única.
UPDATE sz_orders
   SET delivery_fee = (SELECT NULLIF(value,'')::numeric FROM senderzz_options WHERE name='sz_cod_delivery_fee')
 WHERE payment_method = 'cod'
   AND COALESCE(delivery_fee,0) = 0;

-- 4b) producer_net vira CACHE correto (= a view). Só pedidos não-frustrados;
--     frustrado/estornado o produtor não recebe (a tela já filtra, o cache fica 0).
UPDATE sz_orders o
   SET producer_net = f.producer_net_live
  FROM sz_order_financials f
 WHERE f.order_id = o.id
   AND o.status NOT IN ('frustrado','cancelled','cancelado','reembolsado','refunded')
   AND ABS(COALESCE(o.producer_net,0) - f.producer_net_live) > 0.01;
