-- 505-product-parent-variacao.sql
-- FEAT-VARIACAO: permite criar uma variação de um produto existente a partir da
-- tela Novo Produto (dropdown "Baseado em"). Cada variação continua sendo uma
-- linha própria em sz_products (SKU e estoque independentes — mesmo padrão que
-- já existe hoje para produtos "soltos"), só ganha um vínculo opcional ao produto
-- base para exibição ("variação de X") e futura filtragem/agrupamento.
--
-- ON DELETE SET NULL: apagar o produto base não deve apagar as variações — elas
-- viram produtos "soltos" (mesmo comportamento de antes da feature existir).

ALTER TABLE sz_products
  ADD COLUMN IF NOT EXISTS parent_product_id bigint REFERENCES sz_products(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_sz_products_parent_product_id ON sz_products(parent_product_id);
