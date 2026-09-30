-- 492-reconcile-stock-reserved.sql
--
-- BUG: sz_stock.qty_reserved drifta de "reservas órfãs" — pedidos que passaram
-- por status reservante (agendado/aprovado/embalado/em_rota/a_caminho/reagendado)
-- e depois foram pra status terminal (entregue/frustrado/cancelado/...) SEM que
-- o trigger sz_stock_apply_pedido liberasse a reserva corretamente (join antigo
-- via wc_order_id já teve histórico de no-op silencioso pra pedidos nativos Go —
-- ver comentário em 210-fixes-v471-stock.sql). Achado: produto #1278/CD 1 com
-- qty_reserved=5 mas SUM(delta_reserved) do ledger=4 e ZERO pedidos atualmente
-- em status reservante — a verdade é 0, não 4 nem 5.
--
-- FIX: recomputa qty_reserved em TODAS as linhas de sz_stock a partir da fonte
-- de verdade (pedidos REALMENTE em status reservante agora), não a partir do
-- ledger (que também está sujeito ao mesmo vazamento). Regra de reserva em si
-- NÃO muda — mantém os 6 status (agendado, aprovado, embalado, em_rota,
-- a_caminho, reagendado), decisão do dono de não estreitar (risco de oversell
-- em a_caminho/reagendado).

BEGIN;

WITH truth AS (
  SELECT i.produto_id AS product_id, mp.cd_id, SUM(i.quantidade)::integer AS qty_true
    FROM sz_motoboy_pedidos mp
    JOIN sz_orders o       ON COALESCE(o.wp_order_id, o.id) = mp.wc_order_id
    JOIN sz_order_items i  ON i.order_id = o.id
   WHERE mp.status IN ('agendado','aprovado','embalado','em_rota','a_caminho','reagendado')
   GROUP BY i.produto_id, mp.cd_id
)
UPDATE sz_stock s
   SET qty_reserved = COALESCE(t.qty_true, 0),
       updated_at   = now()
  FROM truth t
 WHERE s.product_id = t.product_id AND s.cd_id = t.cd_id AND s.variation_id = 0
   AND s.qty_reserved IS DISTINCT FROM COALESCE(t.qty_true, 0);

-- Linhas de sz_stock SEM nenhum pedido reservante ativo (não cobertas pelo JOIN
-- acima) também precisam zerar — o UPDATE FROM só toca linhas com match em truth.
UPDATE sz_stock s
   SET qty_reserved = 0,
       updated_at   = now()
 WHERE s.qty_reserved <> 0
   AND NOT EXISTS (
     SELECT 1
       FROM sz_motoboy_pedidos mp
       JOIN sz_orders o      ON COALESCE(o.wp_order_id, o.id) = mp.wc_order_id
       JOIN sz_order_items i ON i.order_id = o.id
      WHERE i.produto_id = s.product_id
        AND mp.cd_id = s.cd_id
        AND mp.status IN ('agendado','aprovado','embalado','em_rota','a_caminho','reagendado')
   );

COMMIT;
