-- 533-stock-reconcile-kit-dedup.sql
-- 532 counted repeated reservation rows for the same order. Reconcile the
-- physical reservation total once per order, preserving the COD source too.

BEGIN;

DO $$
DECLARE r record;
BEGIN
  FOR r IN
    WITH exp AS (
      SELECT i.produto_id AS product_id, first_res.cd_id,
             SUM(sz_stock_physical_qty(i.quantidade, i.nome))::integer AS qty
        FROM sz_order_items i
        JOIN sz_orders o ON o.id=i.order_id
        JOIN LATERAL (
          SELECT m.cd_id FROM sz_stock_movements m
           WHERE m.order_id=o.id AND m.product_id=i.produto_id AND m.tipo='reserva'
           ORDER BY m.id LIMIT 1
        ) first_res ON true
       WHERE o.status = ANY(ARRAY['pending','processing','aguardando','on-hold','em_separacao','embalado','enviado'])
         AND NOT EXISTS (SELECT 1 FROM sz_motoboy_pedidos mp
                          WHERE mp.wc_order_id=COALESCE(o.wp_order_id,o.id))
       GROUP BY i.produto_id, first_res.cd_id
    ), cod AS (
      SELECT i.produto_id AS product_id, mp.cd_id,
             SUM(sz_stock_physical_qty(i.quantidade, i.nome))::integer AS qty
        FROM sz_motoboy_pedidos mp
        JOIN sz_orders o ON COALESCE(o.wp_order_id,o.id)=mp.wc_order_id
        JOIN sz_order_items i ON i.order_id=o.id
       WHERE mp.status = ANY(ARRAY['agendado','aprovado','embalado','em_rota','a_caminho','reagendado'])
       GROUP BY i.produto_id, mp.cd_id
    ), wanted AS (
      SELECT product_id, cd_id, SUM(qty)::integer AS qty FROM exp GROUP BY product_id,cd_id
      UNION ALL
      SELECT product_id, cd_id, SUM(qty)::integer FROM cod GROUP BY product_id,cd_id
    ), target AS (
      SELECT product_id,cd_id,SUM(qty)::integer AS qty FROM wanted GROUP BY product_id,cd_id
    ) SELECT s.product_id,s.cd_id,s.qty_reserved,target.qty-s.qty_reserved AS delta
        FROM sz_stock s JOIN target ON target.product_id=s.product_id AND target.cd_id=s.cd_id
       WHERE s.variation_id=0 AND s.qty_reserved<>target.qty
  LOOP
    UPDATE sz_stock SET qty_reserved=GREATEST(qty_reserved+r.delta,0),updated_at=now()
     WHERE product_id=r.product_id AND variation_id=0 AND cd_id=r.cd_id;
    INSERT INTO sz_stock_movements
      (product_id,variation_id,cd_id,order_id,delta_available,delta_reserved,tipo,motivo)
    VALUES (r.product_id,0,r.cd_id,NULL,0,r.delta,'ajuste',
            format('533: reconciliação física de kits (delta=%s)',r.delta));
  END LOOP;
END $$;

COMMIT;
