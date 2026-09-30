-- =============================================================================
-- Backfill seguro do Link único para ofertas antigas
--
-- As ofertas novas passaram a criar três linhas (correio, motoboy e misto), mas
-- ofertas antigas continuam com as linhas que já tinham. Este backfill adiciona
-- somente a linha misto quando existe o par correio + motoboy e ainda não existe
-- misto. Não altera, remove ou renomeia nenhuma linha existente.
--
-- É intencionalmente idempotente: reexecução encontra o misto já criado e não
-- insere outro.
-- =============================================================================

WITH candidates AS (
    SELECT DISTINCT ON (c.producer_id, c.post_id, c.base_name, c.display_value)
           c.id,
           c.producer_id,
           c.post_id,
           c.base_name,
           c.display_value,
           c.price_label,
           c.affiliate_visible,
           c.affiliate_commission_pct,
           c.composition_items,
           c.url,
           c.name
      FROM senderzz_checkout_links c
     WHERE c.tipo IN ('correio', 'expedicao')
       AND EXISTS (
             SELECT 1
               FROM senderzz_checkout_links mb
              WHERE mb.producer_id = c.producer_id
                AND mb.post_id = c.post_id
                AND mb.display_value = c.display_value
                AND mb.base_name = c.base_name
                AND mb.tipo = 'motoboy'
       )
       AND NOT EXISTS (
             SELECT 1
               FROM senderzz_checkout_links m
              WHERE m.producer_id = c.producer_id
                AND m.post_id = c.post_id
                AND m.display_value = c.display_value
                AND m.base_name = c.base_name
                AND m.tipo = 'misto'
       )
     ORDER BY c.producer_id, c.post_id, c.base_name, c.display_value, c.id
), generated AS (
    SELECT c.*,
           md5(random()::text || clock_timestamp()::text || c.id::text) AS new_token
      FROM candidates c
)
INSERT INTO senderzz_checkout_links
       (producer_id, post_id, token, tipo, url, display_value, price_label,
        affiliate_visible, name, slug, affiliate_commission_pct, base_name,
        composition_items, created_at)
SELECT producer_id,
       post_id,
       new_token,
       'misto',
       CASE
           WHEN url ~ '([?&]sz=)[^&#]*'
             THEN regexp_replace(url, '([?&]sz=)[^&#]*', '\1' || new_token)
           WHEN url LIKE '%?%'
             THEN url || '&sz=' || new_token
           ELSE url || '?sz=' || new_token
       END,
       display_value,
       price_label,
       affiliate_visible,
       name,
       new_token,
       affiliate_commission_pct,
       base_name,
       composition_items,
       NOW()
  FROM generated;

