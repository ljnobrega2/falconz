-- =============================================================================
-- Completa canais de ofertas antigas que nasceram somente como "misto".
--
-- O modelo anterior podia criar apenas a linha misto. Para produtores com
-- expedição ativa, a oferta precisa ter os três canais, mas a UI deve agrupá-los
-- numa única linha. Insere somente os tipos ausentes; nunca duplica nem altera
-- links existentes.
-- =============================================================================

WITH source_groups AS (
    SELECT DISTINCT ON (m.producer_id, m.post_id, m.base_name, m.display_value)
           m.id,
           m.producer_id,
           m.post_id,
           m.base_name,
           m.display_value,
           m.price_label,
           m.affiliate_visible,
           m.affiliate_commission_pct,
           m.composition_items,
           m.url,
           m.name
      FROM senderzz_checkout_links m
      JOIN senderzz_portal_users pu ON pu.id = m.producer_id
     WHERE m.tipo = 'misto'
       AND pu.settings ->> 'expedicao_ativa' = 'true'
     ORDER BY m.producer_id, m.post_id, m.base_name, m.display_value, m.id
), missing AS (
    SELECT s.*, v.tipo,
           md5(random()::text || clock_timestamp()::text || s.id::text || v.tipo) AS new_token
      FROM source_groups s
      CROSS JOIN (VALUES ('correio'::varchar), ('motoboy'::varchar)) v(tipo)
     WHERE NOT EXISTS (
             SELECT 1
               FROM senderzz_checkout_links e
              WHERE e.producer_id = s.producer_id
                AND e.post_id = s.post_id
                AND e.base_name = s.base_name
                AND e.display_value = s.display_value
                AND e.tipo = v.tipo
       )
)
INSERT INTO senderzz_checkout_links
       (producer_id, post_id, token, tipo, url, display_value, price_label,
        affiliate_visible, name, slug, affiliate_commission_pct, base_name,
        composition_items, created_at)
SELECT producer_id,
       post_id,
       new_token,
       tipo,
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
       CASE WHEN tipo = 'motoboy' THEN name || ' — Motoboy' ELSE name END,
       new_token,
       affiliate_commission_pct,
       base_name,
       composition_items,
       NOW()
  FROM missing;

