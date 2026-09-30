-- =============================================================================
-- Senderzz — Nome de oferta por ESTÁGIO de funil (senderzz_checkout_links)
--
-- DECISÃO DO DONO (2026-06-24): o nome da oferta NÃO carrega mais o VALOR no fim
--   ("… R$197,00") nem o % de COMISSÃO ("… + 50%"). O discriminador entre ofertas
--   de mesmo produto passa a ser o ESTÁGIO DE FUNIL, atribuído pelo PREÇO (rank
--   decrescente — a mais cara é a principal):
--       rank 1 (preço mais alto) → base + " Principal"   (sempre rotulada)
--       rank 2                    → base + " Downsell"
--       rank 3                    → base + " Remarketing"
--       rank ≥4                   → base + " Remarketing N"   (N = rank-2:
--                                   4ª oferta = "Remarketing 2", 5ª = "Remarketing 3", …)
--
-- POR QUE base_name: o pareamento correio↔motoboy continua por NOME
--   (products.go: mb.name = cl.name || ' — Motoboy') e o nome precisa ficar ÚNICO
--   por oferta. Como o valor saiu do nome, é a BASE da composição (sem valor, sem
--   estágio, sem comissão, sem sufixo Motoboy) que AGRUPA as ofertas irmãs e dá o
--   rank de preço. Guardamos essa base numa COLUNA dedicada para agrupar/parear sem
--   depender de strip de string em runtime.
--
-- SUFIXO " — Motoboy": é só o elo de pareamento do espelho COD ao link de
--   expedição. Por isso só é aplicado a um motoboy que TEM um correio par no mesmo
--   (producer_id, base_name, display_value). Produtor SÓ-COD (sem expedição) tem o
--   motoboy como principal e NÃO leva o sufixo (fiel ao fluxo de Create).
--
-- SEGURANÇA / GOLDEN:
--   * Coluna DEDICADA, idempotente (ADD COLUMN IF NOT EXISTS). NUNCA repurpor.
--   * Toca senderzz_checkout_links (OFERTAS), NÃO sz_orders (PEDIDOS) — pedidos já
--     gravaram o nome da oferta no momento da venda (sz_order_items, dest_produto):
--     são SNAPSHOT histórico e ficam ESTRUTURALMENTE intocados.
--   * Re-execução é segura (backfill + recompute determinísticos → mesmo resultado).
--
-- Rodar manualmente no Postgres do VPS (idempotente — pode reexecutar).
-- =============================================================================

ALTER TABLE senderzz_checkout_links
    ADD COLUMN IF NOT EXISTS base_name VARCHAR(255) NOT NULL DEFAULT '';

-- ── 1) Backfill base_name a partir do nome LEGADO ────────────────────────────────
-- Tira, em ordem: sufixo Motoboy → sufixo de comissão " + N%" → sufixo de valor
-- " R$1.234,56". O que sobra é a base da composição ("1 Datalaprox", "3 Potes
-- Padrão", "1 Avenobis Gotas + 1 Pomada"). Nomes sem esses sufixos ficam intactos.
UPDATE senderzz_checkout_links
   SET base_name = regexp_replace(
                     regexp_replace(
                       regexp_replace(name, ' — Motoboy$', ''),
                       ' \+ [0-9]+%$', ''
                     ),
                     ' R\$[0-9.]+,[0-9]{2}$', ''
                   )
 WHERE base_name = '';

-- Índice de agrupamento/pareamento por (producer, base) — usado no recompute e dedup.
CREATE INDEX IF NOT EXISTS idx_checkout_links_producer_base
    ON senderzz_checkout_links (producer_id, base_name);

-- ── 2) Recompute do NOME por estágio (rank de preço decrescente) ─────────────────
-- DENSE_RANK sobre os display_value DISTINTOS de cada (producer_id, base_name) →
-- correio e motoboy da MESMA oferta (mesmo display_value) recebem o MESMO rank e,
-- portanto, o mesmo nome de estágio. O sufixo " — Motoboy" entra só quando existe um
-- correio par no mesmo preço (produtor com expedição); produtor só-COD fica sem ele.
WITH ranked AS (
  SELECT producer_id, base_name, display_value,
         DENSE_RANK() OVER (
           PARTITION BY producer_id, base_name
           ORDER BY display_value DESC
         ) AS rnk
    FROM (
      SELECT DISTINCT producer_id, base_name, display_value
        FROM senderzz_checkout_links
       WHERE base_name <> ''
    ) d
)
UPDATE senderzz_checkout_links cl
   SET name = cl.base_name
            || CASE
                 WHEN r.rnk = 1 THEN ' Principal'
                 WHEN r.rnk = 2 THEN ' Downsell'
                 WHEN r.rnk = 3 THEN ' Remarketing'
                 ELSE ' Remarketing ' || (r.rnk - 2)::text
               END
            || CASE
                 WHEN cl.tipo = 'motoboy' AND EXISTS (
                        SELECT 1 FROM senderzz_checkout_links c2
                         WHERE c2.producer_id   = cl.producer_id
                           AND c2.base_name     = cl.base_name
                           AND c2.display_value = cl.display_value
                           AND c2.tipo <> 'motoboy'
                      ) THEN ' — Motoboy'
                 ELSE ''
               END
  FROM ranked r
 WHERE cl.producer_id   = r.producer_id
   AND cl.base_name     = r.base_name
   AND cl.display_value = r.display_value
   AND cl.base_name <> '';
