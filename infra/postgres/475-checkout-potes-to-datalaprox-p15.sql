-- =============================================================================
-- Senderzz — One-off: nomes legados "N Pote(s)" do produtor 15 → "N Datalaprox"
--
-- CONTEXTO: as ofertas migradas do WP do produtor 15 foram cadastradas com nome
--   GENÉRICO ("1 Pote", "3 Potes", "5 Potes") — sem o produto real. O dono confirmou
--   (2026-06-24) que TODAS as "N Pote(s)" são do produto Datalaprox e pediu a base no
--   formato "N Datalaprox" (sem repetir "pote"). Ofertas novas já saem certas (o modal
--   monta o nome a partir de sz_products.nome = "Datalaprox").
--
-- ESCOPO: SÓ producer_id = 15 e SÓ bases que casam exatamente "^[0-9]+ Potes?$"
--   (não toca "1 Datalaprox" já correto, nem Dorvax/teste, nem outros produtores).
--   "1 Pote" → "1 Datalaprox" FUNDE com o grupo "1 Datalaprox" já existente: o
--   re-rank por ROW_NUMBER(preço desc, id) mantém nome ÚNICO por oferta e o par
--   correio↔motoboy íntegro, mas preços repetidos do lixo legado (backtick, etc.)
--   ficam como Principal/Downsell/Remarketing N — o dono limpa depois (apagar dups).
--
-- Idempotente: re-derivar a base já-"Datalaprox" não recasa "Potes" → estável.
-- Toca só senderzz_checkout_links (ofertas); pedidos snapshotaram o nome na venda.
-- =============================================================================

WITH correios AS (
  SELECT id, producer_id, display_value, name AS old_name,
         regexp_replace(
           regexp_replace(
             regexp_replace(
               regexp_replace(
                 regexp_replace(
                   regexp_replace(name, ' — Motoboy$', ''),
                   ' \+ [0-9]+%$', ''),
                 ' R\$[0-9.]+,[0-9]{2}$', ''),
               '`+\s*$', ''),
             '( ([Pp]rincipal|[Pp]adr[ãa]o|[Dd]ownsell|[Uu]psell|[Rr]emarketing)( [0-9]+)?)+$', ''),
           '^([0-9]+) Potes?$', '\1 Datalaprox') AS nb
    FROM senderzz_checkout_links
   WHERE tipo <> 'motoboy' AND producer_id = 15
),
ranked AS (
  SELECT id, producer_id, display_value, old_name, nb,
         ROW_NUMBER() OVER (PARTITION BY producer_id, nb ORDER BY display_value DESC, id) AS rnk
    FROM correios
   WHERE nb <> ''
),
named AS (
  SELECT id, producer_id, old_name, nb,
         nb || CASE WHEN rnk = 1 THEN ' Principal'
                    WHEN rnk = 2 THEN ' Downsell'
                    WHEN rnk = 3 THEN ' Remarketing'
                    ELSE ' Remarketing ' || (rnk - 2)::text END AS new_name
    FROM ranked
),
upd_correio AS (
  UPDATE senderzz_checkout_links c
     SET name = n.new_name, base_name = n.nb
    FROM named n
   WHERE c.id = n.id
  RETURNING 1
)
UPDATE senderzz_checkout_links m
   SET name = n.new_name || ' — Motoboy', base_name = n.nb
  FROM named n
 WHERE m.tipo = 'motoboy'
   AND m.producer_id = n.producer_id
   AND m.name = n.old_name || ' — Motoboy';
