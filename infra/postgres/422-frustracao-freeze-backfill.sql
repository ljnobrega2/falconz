-- =============================================================================
-- 422-frustracao-freeze-backfill.sql — BACKFILL único do financeiro CONGELADO
-- dos pedidos já frustrados (antes do congelamento existir).
--
-- Recomputa o snapshot CORRETO por ordem cronológica de (ts_frustrado, id):
--   - Produtor (valor_taxa_frustrado): 1ª frustração por TELEFONE normalizado do
--     cliente → prod_first (sz_prod_first_frustration_penalty, default 0);
--     2ª+ do MESMO telefone → prod_repeat (sz_aff_producer_frustration_penalty,
--     default 8).
--   - Afiliado (valor_taxa_frustrado_afiliado): só se o pedido tem afiliado
--     (sz_orders.affiliate_id via COALESCE(wp_order_id,id)=wc_order_id); paga em
--     TODAS (sem isenção) → aff_first (sz_aff_first_frustration_penalty, default 5)
--     na 1ª por telefone, aff_repeat (sz_aff_default_penalty_value, default 5) nas
--     demais. Hoje first==repeat==5.
--   - frustrado_isento = (1ª por telefone).
--
-- Os defaults são lidos de senderzz_options (COALESCE p/ default quando ausente).
-- Idempotente: reexecutar produz exatamente o mesmo resultado (a normalização e o
-- ranking por telefone são determinísticos; ts_frustrado/dest_telefone não mudam).
-- NÃO toca view nem ledger.
-- =============================================================================

WITH opt AS (
    SELECT
        COALESCE((SELECT value::numeric FROM senderzz_options WHERE name='sz_prod_first_frustration_penalty'),    0) AS prod_first,
        COALESCE((SELECT value::numeric FROM senderzz_options WHERE name='sz_aff_producer_frustration_penalty'),   8) AS prod_repeat,
        COALESCE((SELECT value::numeric FROM senderzz_options WHERE name='sz_aff_first_frustration_penalty'),      5) AS aff_first,
        COALESCE((SELECT value::numeric FROM senderzz_options WHERE name='sz_aff_default_penalty_value'),          5) AS aff_repeat
),
-- Normaliza o telefone (strip +55 quando 12/13 dígitos) e rankeia por telefone
-- na ordem cronológica (ts_frustrado, id). rank=1 → 1ª frustração daquele telefone.
ranked AS (
    SELECT
        p.id,
        p.wc_order_id,
        (CASE
           WHEN length(regexp_replace(p.dest_telefone, '\D', '', 'g')) IN (12, 13)
                AND left(regexp_replace(p.dest_telefone, '\D', '', 'g'), 2) = '55'
           THEN substring(regexp_replace(p.dest_telefone, '\D', '', 'g') FROM 3)
           ELSE regexp_replace(p.dest_telefone, '\D', '', 'g')
         END) AS tel_norm,
        row_number() OVER (
            PARTITION BY (CASE
               WHEN length(regexp_replace(p.dest_telefone, '\D', '', 'g')) IN (12, 13)
                    AND left(regexp_replace(p.dest_telefone, '\D', '', 'g'), 2) = '55'
               THEN substring(regexp_replace(p.dest_telefone, '\D', '', 'g') FROM 3)
               ELSE regexp_replace(p.dest_telefone, '\D', '', 'g')
             END)
            ORDER BY p.ts_frustrado, p.id
        ) AS tel_rank
    FROM sz_motoboy_pedidos p
    WHERE p.status = 'frustrado'
),
calc AS (
    SELECT
        r.id,
        -- telefone vazio → tratado como 1ª (isento): sem chave de agrupamento.
        (r.tel_norm = '' OR r.tel_rank = 1)                       AS isento,
        (o.affiliate_id IS NOT NULL AND o.affiliate_id > 0)       AS tem_afiliado
    FROM ranked r
    LEFT JOIN sz_orders o ON COALESCE(o.wp_order_id, o.id) = r.wc_order_id
)
UPDATE sz_motoboy_pedidos p
SET frustrado_isento              = c.isento,
    valor_taxa_frustrado          = CASE WHEN c.isento THEN opt.prod_first ELSE opt.prod_repeat END,
    valor_taxa_frustrado_afiliado = CASE
                                       WHEN NOT c.tem_afiliado THEN 0
                                       WHEN c.isento           THEN opt.aff_first
                                       ELSE opt.aff_repeat
                                     END
FROM calc c, opt
WHERE p.id = c.id;

-- Relatório dos 6 valores resultantes.
SELECT
    p.id,
    p.wc_order_id,
    p.dest_telefone,
    p.ts_frustrado,
    p.frustrado_isento,
    p.valor_taxa_frustrado          AS penalty_produtor,
    p.valor_taxa_frustrado_afiliado AS penalty_afiliado,
    o.affiliate_id
FROM sz_motoboy_pedidos p
LEFT JOIN sz_orders o ON COALESCE(o.wp_order_id, o.id) = p.wc_order_id
WHERE p.status = 'frustrado'
ORDER BY p.ts_frustrado, p.id;
