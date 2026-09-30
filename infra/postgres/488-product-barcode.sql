-- 488-product-barcode.sql
-- CÓDIGO DE BARRAS como atributo do produto.
--
-- Regra do dono: o controle de operação do motoboy vai usar o código de barras
-- do produto, não apenas o SKU comercial. O campo vive em sz_products para novos
-- e antigos produtos; legacy rows ficam NULL até o admin preencher.
--
-- IDEMPOTENTE: ADD COLUMN IF NOT EXISTS.

ALTER TABLE sz_products
  ADD COLUMN IF NOT EXISTS barcode text NULL;
