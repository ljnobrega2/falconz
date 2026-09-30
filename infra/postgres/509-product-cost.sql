-- 509-product-cost.sql
-- FEAT-CRM-FINANCEIRO (pedido dono 2026-07-28): custo do produto (COGS) —
-- primeiro passo pavimentando um CRM financeiro em cima do sistema logístico.
-- Coluna PRIVADA: só produtor (dono do produto) + admin leem/gravam (go/portal
-- products.go escopa por produtor_id = caller; go/admin products.go é admin-only
-- por natureza da rota). NUNCA exposta em endpoints públicos/checkout (GetOffer,
-- /checkout-api/*) nem pra afiliado/operador — é dado financeiro sensível do
-- produtor, não catálogo.

ALTER TABLE sz_products
  ADD COLUMN IF NOT EXISTS custo NUMERIC(12,2) NULL;

COMMENT ON COLUMN sz_products.custo IS
  'Custo do produto (COGS) — privado do produtor+admin, nunca exposto em endpoints públicos/checkout.';
