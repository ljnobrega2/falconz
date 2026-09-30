-- =============================================================================
-- Senderzz — Backfill de sz_order_meta._senderzz_delivery_mode (motoboy|correio)
--
-- BUG (2026-06-24): o filtro da Expedição (go/portal expedicao.go) exclui pedidos COD
--   checando o meta _senderzz_delivery_mode='motoboy' OU o nome do item conter
--   "motoboy". O checkout.go NÃO gravava esse meta e o nome do item passou a guardar a
--   BASE limpa (sem "— Motoboy") → pedidos COD vazavam pra tela de Expedição.
--
-- FIX de código: checkout.go agora grava _senderzz_delivery_mode em todo pedido novo.
-- ESTA migração backfilla os pedidos JÁ existentes que não têm o meta, classificando:
--   motoboy (COD) quando houver QUALQUER sinal de motoboy:
--     • linha em sz_motoboy_pedidos (fila COD), OU
--     • link de checkout do pedido com tipo='motoboy' (via meta checkout_token), OU
--     • nome do item do pedido contém "motoboy" (nomes legados "X — Motoboy").
--   senão correio (expedição).
--
-- Idempotente: só insere onde o meta ainda NÃO existe (NOT EXISTS). Re-rodar = no-op.
-- =============================================================================

INSERT INTO sz_order_meta (order_id, meta_key, meta_value)
SELECT o.id, '_senderzz_delivery_mode',
       CASE
         WHEN EXISTS (
                SELECT 1 FROM sz_motoboy_pedidos mp
                 WHERE mp.wc_order_id = COALESCE(o.wp_order_id, o.id)
              )
           OR EXISTS (
                SELECT 1 FROM sz_order_meta mt
                  JOIN senderzz_checkout_links cl ON cl.token = mt.meta_value
                 WHERE mt.order_id = o.id
                   AND mt.meta_key IN ('checkout_token', '_senderzz_checkout_link_id')
                   AND cl.tipo = 'motoboy'
              )
           OR EXISTS (
                SELECT 1 FROM sz_order_items oi
                 WHERE oi.order_id = o.id
                   AND oi.nome ILIKE '%motoboy%'
              )
         THEN 'motoboy'
         ELSE 'correio'
       END
  FROM sz_orders o
 WHERE NOT EXISTS (
         SELECT 1 FROM sz_order_meta m
          WHERE m.order_id = o.id AND m.meta_key = '_senderzz_delivery_mode'
       );
