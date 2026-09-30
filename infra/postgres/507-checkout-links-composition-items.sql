-- 507-checkout-links-composition-items.sql
-- FEAT-CHECKOUT-MULTI-ITEM: até aqui a composição (2+ produtos/variações que o
-- produtor monta na tela Products.tsx ao criar o link, botão "+") só virava
-- texto em name/base_name (buildBaseName em links_portal.go) — o runtime de
-- compra (checkout.go) só conhecia post_id do produto principal e sempre
-- inseria 1 linha em sz_order_items. Esta coluna persiste a composição de
-- forma estruturada para o checkout inserir N itens de verdade no pedido.
--
-- Formato: array de objetos [{"product_id": 123, "qty": 1, "variacao": "M"}, ...]
-- NULL = oferta antiga/simples (1 produto, comportamento igual a hoje).

ALTER TABLE senderzz_checkout_links
  ADD COLUMN IF NOT EXISTS composition_items JSONB NULL;

COMMENT ON COLUMN senderzz_checkout_links.composition_items IS
  'Composição estruturada do link (produto+qty+variação por linha). NULL = oferta de produto único (legado).';
