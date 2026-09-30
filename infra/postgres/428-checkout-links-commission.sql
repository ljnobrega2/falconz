-- =============================================================================
-- Senderzz v473 — Comissão por-OFERTA no espelho PG (senderzz_checkout_links)
--
-- MODELO (confirmado pelo dono): a afiliação é na OFERTA. A comissão que o
--   afiliado recebe é DITADA pelo CHECKOUT LINK (oferta); fallback = padrão do
--   PRODUTOR; fallback final = global (10%). O afiliado em si é identificado pelo
--   aff_token (senderzz_affiliate_links.link_token); a OFERTA dita a %.
--
-- POR QUE esta coluna: wp_senderzz_checkout_links já tem affiliate_commission_pct
--   (ver includes/portal/portal-helpers.php:371 — DECIMAL(10,2) NOT NULL DEFAULT
--   0.00), mas a coluna NÃO foi espelhada no PG em 150-fixes-v467-checkout-links.sql
--   (ficou na lista de colunas ignoradas). Sem ela, o checkout Go não consegue ler
--   a % por-oferta e o portal grava a comissão numa meta paliativa
--   (_sz_offer_commission_pct:{id}). Esta migração cria a COLUNA DEDICADA — fiel ao
--   nome da origem WP, para o migrador (PAIR já existe) backfillar quando rodar.
--
-- SEGURANÇA / GOLDEN:
--   * Coluna DEDICADA, idempotente (ADD COLUMN IF NOT EXISTS). NUNCA repurpor.
--   * Toca senderzz_checkout_links (OFERTAS), NÃO sz_orders (PEDIDOS) — os 41
--     pedidos existentes com affiliate_amount>0 ficam ESTRUTURALMENTE intocados.
--   * DEFAULT 0.00 = "sem % própria" → o checkout cai no padrão do produtor/global.
--     As 38 ofertas existentes lerão 0.00 até o migrador WP->PG rodar o backfill da
--     coluna (passo de VPS, fora desta migração). Aceitável: ofertas != pedidos.
--
-- Rodar manualmente no Postgres do VPS (idempotente — pode reexecutar).
-- =============================================================================

ALTER TABLE senderzz_checkout_links
    ADD COLUMN IF NOT EXISTS affiliate_commission_pct NUMERIC(10,2) NOT NULL DEFAULT 0.00;
