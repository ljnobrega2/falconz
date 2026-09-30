-- 506-product-sku-unique.sql
-- FEAT-VARIACAO seguiu criando SKU duplicado (nenhuma constraint impedia). SKU
-- precisa ser único no catálogo (case-insensitive), exceto produtos soft-deletados
-- (status='deleted' — ver productListableFilter em products.go) e sku NULL (produto
-- sem SKU ainda). Backend também valida antes do INSERT/UPDATE (mensagem amigável);
-- este índice é a rede de segurança contra corrida/API direta.

CREATE UNIQUE INDEX IF NOT EXISTS idx_sz_products_sku_unique
  ON sz_products (lower(sku))
  WHERE sku IS NOT NULL AND status IS DISTINCT FROM 'deleted';
