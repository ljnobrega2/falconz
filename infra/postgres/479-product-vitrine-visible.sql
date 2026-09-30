-- 479-product-vitrine-visible.sql
-- VISIBILIDADE NA VITRINE como ATRIBUTO DO PRODUTO (toggle por produto).
--
-- REGRA DO DONO (2026-06-25): admin (e produtor, via dado) controla se um produto
-- aparece na Vitrine do portal (storefront/showcase de afiliados). O toggle vive em
-- sz_products (cadastrado no menu/form do produto). A Vitrine (go/portal vitrine.go)
-- passa a filtrar por COALESCE(sp.vitrine_visible, true) = true — produto oculto não
-- aparece para afiliados/produtores na descoberta, mas continua no admin (a tela
-- Produtos NÃO filtra por vitrine_visible: o admin precisa vê-lo p/ religar o toggle).
--
-- COLUNA: vitrine_visible boolean NOT NULL DEFAULT true. Default true para que TODOS
-- os produtos legados continuem visíveis na vitrine após a migração (não esconde
-- catálogo pré-feature) — só some quem o admin/produtor desligar explicitamente.
--
-- IDEMPOTENTE: ADD COLUMN IF NOT EXISTS (reexecução não duplica coluna nem altera
-- valores já gravados).

ALTER TABLE sz_products
  ADD COLUMN IF NOT EXISTS vitrine_visible boolean NOT NULL DEFAULT true;
