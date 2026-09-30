-- 465-product-image-backfill.sql
-- IMAGEM DO PRODUTO = a imagem do WordPress (featured image / get_image_id()).
--
-- PROBLEMA (auditado no banco vivo 2026-06-23): a tela de PRODUTO do portal mostrava
-- a imagem em branco. sz_products NÃO tem coluna de imagem dedicada e sz_products.meta
-- estava NULL p/ TODOS os 4 produtos — o migrador WP→PG não trouxe a URL da foto (no
-- WP a imagem vem de wp_get_attachment_image_url(get_image_id()), um attachment que
-- não foi espelhado no Postgres; wp_posts/wp_postmeta NÃO existem neste PG).
--
-- FONTE REAL (confirmada): a imagem é resolvível pelo wp_post_id na origem WordPress
-- viva (app.senderzz.com.br) via WC Store API:
--   GET https://app.senderzz.com.br/wp-json/wc/store/v1/products/<wp_post_id> → images[0].src
-- Confirmado (HTTP 200, image/webp/jpeg):
--   1278 Datalaprox → .../uploads/2026/05/D_NQ_NP_2X_640161-MLB105500075800_012026-F.webp
--   1279 Dorvax     → .../uploads/2026/05/WhatsApp-Image-2026-05-22-at-11.31.56.jpeg
--   1444 Teste      → (sem imagem no WP)
--   189  Recarga    → (sem imagem no WP; nem é produto-real)
--
-- O QUE FAZ: grava a URL da foto em sz_products.meta sob a chave 'image' (o MESMO
-- contrato que go/portal vitrine.go e o novo /portal/products[/{id}] já leem via
-- COALESCE(meta->>'image_url', ..., meta->>'image', ...)). Com o meta populado, a
-- foto aparece na tela do produto SEM mudar o read-path (forward-compat já existente).
--
-- POR QUE backfill (e não fetch live no handler): manter o handler do portal sem
-- dependência HTTP externa (latência/acoplamento ao WP) — a URL é estável (arquivo
-- em /wp-content/uploads). Quando um sync WP→PG existir, ele reescreve a mesma chave.
--
-- IDEMPOTENTE: jsonb_set com create_missing=true sobre COALESCE(meta,'{}') — reexecução
-- só reescreve a chave 'image' (preserva qualquer outra chave futura do meta). Match por
-- wp_post_id (chave canônica; tolera reimport). Só toca quem tem URL conhecida.

UPDATE sz_products
   SET meta = jsonb_set(COALESCE(meta, '{}'::jsonb), '{image}', to_jsonb($img$https://app.senderzz.com.br/wp-content/uploads/2026/05/D_NQ_NP_2X_640161-MLB105500075800_012026-F.webp$img$::text), true),
       updated_at = NOW()
 WHERE wp_post_id = 1278;

UPDATE sz_products
   SET meta = jsonb_set(COALESCE(meta, '{}'::jsonb), '{image}', to_jsonb($img$https://app.senderzz.com.br/wp-content/uploads/2026/05/WhatsApp-Image-2026-05-22-at-11.31.56.jpeg$img$::text), true),
       updated_at = NOW()
 WHERE wp_post_id = 1279;
