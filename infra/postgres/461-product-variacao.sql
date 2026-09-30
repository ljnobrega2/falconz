-- 461-product-variacao.sql
-- VARIAÇÃO como ATRIBUTO DO PRODUTO (não derivada do checkout/oferta).
--
-- REGRA DO DONO (2026-06-22): "todos os produtos devem conter o campo variação;
-- a variação do Datalaprox deve ser 'pote' em todos os pedidos; e já fique
-- cadastrada no menu do produto". Logo a variação vive em sz_products (cadastrada
-- no menu/form do produto) e a listagem de pedidos LÊ a variação do produto do
-- 1º item (orders.go, join sp.wp_post_id = oi.produto_id). NUNCA derivar de
-- oferta_nome (gerava lixo como "Padrão`"/"Remarketing").
--
-- COLUNA: variacao text NOT NULL DEFAULT '' — atributo livre por produto
-- (ex.: 'pote', 'cápsula', 'sachê'). Default '' (não NULL) porque o backend
-- (products.go) grava string e a listagem faz join cast-safe sem COALESCE de NULL.
--
-- BACKFILL: Datalaprox = 'pote' por regra do dono. Match por nome (sz_products.nome
-- ILIKE '%datalaprox%') — id=2 / wp_post_id=1278 neste banco; o ILIKE cobre o caso
-- e tolera reimport. Como todos os pedidos hoje são Datalaprox, todos passam a
-- exibir "pote" na coluna Variação da lista de pedidos.
--
-- IDEMPOTENTE: ADD COLUMN IF NOT EXISTS + UPDATE (reexecução só reescreve 'pote',
-- nunca duplica coluna nem altera outros produtos).

ALTER TABLE sz_products
  ADD COLUMN IF NOT EXISTS variacao text NOT NULL DEFAULT '';

UPDATE sz_products
   SET variacao = 'pote'
 WHERE nome ILIKE '%datalaprox%'
   AND variacao IS DISTINCT FROM 'pote';
