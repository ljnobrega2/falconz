-- =============================================================================
-- Senderzz v469 — Dimensões do produto (sz_products)
--
-- REGRA DE NEGÓCIO (do dono): "produto não deve ter preço, esse é definido no
--   link de oferta. Produtos têm SOMENTE altura, peso, comprimento e largura."
--
-- POR QUE: o preço de venda vive no link de oferta (senderzz_checkout_links),
--   não no produto. A coluna `preco` permanece na tabela (DEFAULT 0.00) por
--   compatibilidade com o auto-seed a partir de order_items (MAX(preco_unit)),
--   mas a UI não edita mais preço. Adicionamos aqui as quatro dimensões físicas
--   que o produto realmente carrega.
--
-- Todas NULLable: a origem WP (wp_postmeta _height/_width/_length/_weight) pode
--   ter metas vazias ou ausentes — nesses casos gravamos NULL (não inventar 0).
--
-- Não-destrutivo: só ADD COLUMN IF NOT EXISTS. Aplicar por ambiente antes de
--   rodar o migrador (infra/scripts/migrate-wp-to-pg.py) e antes do deploy do
--   go/admin v469+.
-- =============================================================================

ALTER TABLE sz_products ADD COLUMN IF NOT EXISTS altura      NUMERIC(8,2) NULL;  -- cm
ALTER TABLE sz_products ADD COLUMN IF NOT EXISTS largura     NUMERIC(8,2) NULL;  -- cm
ALTER TABLE sz_products ADD COLUMN IF NOT EXISTS comprimento NUMERIC(8,2) NULL;  -- cm
ALTER TABLE sz_products ADD COLUMN IF NOT EXISTS peso        NUMERIC(8,3) NULL;  -- kg
