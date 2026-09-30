-- =============================================================================
-- Senderzz — Normalização dos NOMES LEGADOS de oferta para a regra de ESTÁGIO
--
-- CONTEXTO: a migração 471 trocou o discriminador do nome (valor/comissão → estágio
--   de funil por preço). Mas as ofertas LEGADAS migradas do WP têm nomes livres
--   ("5 Potes Padrão", "5 Potes padrão", "3 Potes Upsell", "1 Pote Remarketing",
--   "3 Potes Padrão Downsell", "1 Pote Padrão`"…). Como o base_name da 471 manteve
--   o descritor legado, cada nome virou um GRUPO próprio → não re-estagiaram.
--
-- ESTA migração re-deriva a BASE REAL do produto (tira o descritor legado de
--   estágio: Padrão/padrão/Downsell/Upsell/Remarketing[ N] + backtick de lixo),
--   AGRUPA as ofertas irmãs do mesmo produto e re-estagia por PREÇO decrescente
--   (mais cara = Principal), igual à regra do dono:
--       rank 1 → base | 2 → " Downsell" | 3 → " Remarketing" | ≥4 → " Remarketing N"
--
-- DESEMPATE (preço igual no mesmo produto): a regra nova PROÍBE duas ofertas de
--   mesmo produto+preço (o dedup de Create bloqueia), mas os dados LEGADOS têm
--   alguns (ex.: dois "3 Potes" a R$297). Para NÃO gerar nome duplicado (quebraria
--   o pareamento correio↔motoboy por nome), desempatamos por id (ROW_NUMBER por
--   OFERTA, não DENSE_RANK por preço) → cada oferta recebe um estágio único. Esses
--   pares preço-igual ficam reportados p/ o dono decidir (apagar um / mudar preço).
--
-- PAREAMENTO: o nome do espelho motoboy é reescrito a partir do NOME ANTIGO do seu
--   correio (m.name = old_correio_name || ' — Motoboy'), preservado por CTE — então
--   o par continua íntegro com o nome novo. Ofertas SÓ-COD (motoboy sem correio par)
--   são tratadas à parte (o motoboy é o principal, sem sufixo Motoboy).
--
-- SEGURANÇA: idempotente (re-derivar+re-rankear dá o mesmo resultado). Toca só
--   senderzz_checkout_links (ofertas) — pedidos snapshotaram o nome na venda e ficam
--   intocados. NÃO apaga linhas.
-- =============================================================================

-- ── Ofertas COM expedição (correio + espelho motoboy pareado) ────────────────────
WITH correios AS (
  SELECT id, producer_id, display_value, name AS old_name,
         regexp_replace(
           regexp_replace(
             regexp_replace(
               regexp_replace(
                 regexp_replace(name, ' — Motoboy$', ''),
                 ' \+ [0-9]+%$', ''),
               ' R\$[0-9.]+,[0-9]{2}$', ''),
             '`+\s*$', ''),
           '( ([Pp]rincipal|[Pp]adr[ãa]o|[Dd]ownsell|[Uu]psell|[Rr]emarketing)( [0-9]+)?)+$', '') AS nb
    FROM senderzz_checkout_links
   WHERE tipo <> 'motoboy'
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

-- ── Ofertas SÓ-COD (motoboy principal, sem correio par) ──────────────────────────
WITH cods AS (
  SELECT id, producer_id, display_value, name AS old_name,
         regexp_replace(
           regexp_replace(
             regexp_replace(
               regexp_replace(name, ' \+ [0-9]+%$', ''),
               ' R\$[0-9.]+,[0-9]{2}$', ''),
             '`+\s*$', ''),
           '( ([Pp]rincipal|[Pp]adr[ãa]o|[Dd]ownsell|[Uu]psell|[Rr]emarketing)( [0-9]+)?)+$', '') AS nb
    FROM senderzz_checkout_links
   WHERE tipo = 'motoboy'
     AND name NOT LIKE '% — Motoboy'   -- não é espelho de um correio
),
ranked_cod AS (
  SELECT id, producer_id, display_value, nb,
         ROW_NUMBER() OVER (PARTITION BY producer_id, nb ORDER BY display_value DESC, id) AS rnk
    FROM cods
   WHERE nb <> ''
)
UPDATE senderzz_checkout_links m
   SET name = r.nb || CASE WHEN r.rnk = 1 THEN ' Principal'
                          WHEN r.rnk = 2 THEN ' Downsell'
                          WHEN r.rnk = 3 THEN ' Remarketing'
                          ELSE ' Remarketing ' || (r.rnk - 2)::text END,
       base_name = r.nb
  FROM ranked_cod r
 WHERE m.id = r.id;
