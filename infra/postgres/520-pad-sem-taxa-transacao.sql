-- AUDIT-2026-07-30 CRITICAL (dono): "para PAD [expedição] não tem taxa de
-- transação e essas taxas são personalizáveis e não fixas".
--
-- A view canônica sz_order_financials (478-financial-canonical-view.sql)
-- aplicava producer_tx_rate() e delivery_fee (taxa fixa COD, 23,98) em TODO
-- pedido, sem distinguir COD/motoboy de PAD/expedição — os mesmos dois bugs
-- que existiam nos INSERTs de checkout.go e api_orders.go (corrigidos nesta
-- sessão). Resultado: faturamento/líquido do produtor reportado errado pra
-- todo pedido PAD com afiliado ou com producer_tx_rate configurada > 0 —
-- cobrava taxa de entrega COD fantasma + descontava taxa de transação que
-- não deveria existir nesse fluxo.
--
-- Critério canônico motoboy vs expedição (mesmo usado em tracking.go/
-- 511-stock-expedicao.sql): EXISTS linha em sz_motoboy_pedidos pelo
-- wc_order_id — NÃO payment_method (COD é forma de pagamento, existe nos
-- dois fluxos).
CREATE OR REPLACE FUNCTION sz_order_financials_is_motoboy(order_id bigint, wp_order_id bigint)
RETURNS boolean LANGUAGE sql STABLE AS $$
  SELECT EXISTS (
    SELECT 1 FROM sz_motoboy_pedidos mp
     WHERE mp.wc_order_id = COALESCE(wp_order_id, order_id)
  )
$$;

-- AUDIT-2026-07-31: descoberto rodando contra produção real — em prod,
-- sz_order_financials NÃO é uma view, é uma TABELA mantida por trigger
-- (sistema sz_financials_refresh, nunca versionado neste repo até a migração
-- 523, que documenta e corrige esse sistema de verdade). DROP VIEW numa
-- tabela é erro fatal — guarda: só cria/substitui a VIEW se o objeto NÃO
-- existir ainda ou já for uma view (relkind='v'). Se já for tabela, esta
-- migração vira no-op aqui — 523 cuida da correção equivalente na tabela.
DO $$
DECLARE
  v_kind "char";
BEGIN
  SELECT relkind INTO v_kind FROM pg_class WHERE relname = 'sz_order_financials';
  IF v_kind IS NULL OR v_kind = 'v' THEN
    EXECUTE 'DROP VIEW IF EXISTS sz_order_financials';
    EXECUTE $view$
      CREATE VIEW sz_order_financials AS
      SELECT
        o.id                                                            AS order_id,
        o.produtor_id,
        o.affiliate_id,
        o.status,
        o.payment_method,
        o.created_at,
        o.total,
        (COALESCE(o.affiliate_amount,0) + COALESCE(o.transaction_fee,0)) AS affiliate_bruta,
        COALESCE(o.affiliate_amount,0)                                  AS affiliate_liquida,
        COALESCE(o.transaction_fee,0)                                   AS affiliate_take,
        COALESCE(o.delivery_fee,0)                                      AS delivery_fee,
        CASE WHEN sz_order_financials_is_motoboy(o.id, o.wp_order_id)
             THEN ROUND(o.total * sz_producer_tx_rate(), 2)
             ELSE 0
        END                                                              AS producer_take,
        (COALESCE(o.transaction_fee,0) +
         CASE WHEN sz_order_financials_is_motoboy(o.id, o.wp_order_id)
              THEN ROUND(o.total * sz_producer_tx_rate(), 2)
              ELSE 0
         END)                                                            AS senderzz_take,
        GREATEST(
           o.total
           - (COALESCE(o.affiliate_amount,0) + COALESCE(o.transaction_fee,0))
           - COALESCE(o.delivery_fee,0)
           - CASE WHEN sz_order_financials_is_motoboy(o.id, o.wp_order_id)
                  THEN ROUND(o.total * sz_producer_tx_rate(), 2)
                  ELSE 0
             END, 0)                                                     AS producer_net_live
      FROM sz_orders o
    $view$;
  END IF;
END $$;
