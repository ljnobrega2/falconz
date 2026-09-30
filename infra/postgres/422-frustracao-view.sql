-- =============================================================================
-- 422-frustracao-view.sql — FRUST-V2: a view sz_order_financeiro passa a refletir
-- o modelo CONFIRMADO PELO DONO para pedidos frustrados.
--
-- // FRUST-V2
--
-- MODELO (fonte da verdade):
--   FRUSTRADO (entrega falhou / COD não coletado) ZERA todo o financeiro NORMAL
--   do produtor E do afiliado:
--     liquido_produtor = 0, taxa_transacao_produtor = 0, taxa_entrega = 0,
--     comissao_afiliado_bruta/liquida = 0, taxa_transacao_afiliado = 0.
--   A ÚNICA cobrança é a PENALTY de frustração, CONGELADA (snapshot) na baixa —
--   imune a reagendamento/retroatividade. A VIEW LÊ o valor congelado das colunas
--   de sz_motoboy_pedidos; NÃO recalcula a regra de 1ª-vs-repeat (isso é da camada
--   Go/backfill).
--     - taxa_frustracao_produtor  = frozen valor_taxa_frustrado          (penalty produtor)
--     - taxa_frustracao_afiliado  = frozen valor_taxa_frustrado_afiliado (penalty afiliado)
--   take_falk no frustrado = taxa_frustracao_produtor + taxa_frustracao_afiliado
--   (o normal zerou). COMPLETO/ENTREGUE permanece INALTERADO (#1587 golden).
--
-- GATILHO DE COBRANÇA (zera o normal):
--   cobrar_frustracao = EXISTS(sz_motoboy_pedidos m
--                              WHERE m.wc_order_id = COALESCE(o.wp_order_id,o.id)
--                                AND m.status = 'frustrado')
--   OU o pedido está em status terminal-fail SEM entrega motoboy ativa
--   (preserva o comportamento da view anterior p/ frustrado/cancelled/reembolsado
--   sem motoboy — cenário (c) da suíte de regressão).
--
-- A coluna `frustrado` (já existente) é redefinida para = a condição de zeragem,
-- de modo que a flag continua coerente: TRUE exatamente quando o normal é zerado.
--
-- Idempotente (CREATE OR REPLACE). Backup restaurável do def anterior em
-- infra/postgres/_backup_view_financeiro_v2.sql.
-- =============================================================================

CREATE OR REPLACE VIEW sz_order_financeiro AS
 SELECT id AS order_id,
    wp_order_id,
    status,
    produtor_id,
    affiliate_id,
    total AS valor_pedido,

    -- ----- bloco AFILIADO (zerado quando cobra frustração) -----
    CASE WHEN (
        EXISTS (SELECT 1 FROM sz_motoboy_pedidos m
                 WHERE m.wc_order_id = COALESCE(o.wp_order_id, o.id)
                   AND m.status::text = 'frustrado'::text)
        OR (
          (status::text = ANY (ARRAY['frustrado'::character varying, 'cancelled'::character varying, 'reembolsado'::character varying]::text[]))
          AND NOT (EXISTS (SELECT 1 FROM sz_motoboy_pedidos m
                            WHERE m.wc_order_id = o.wp_order_id AND m.status::text <> 'cancelado'::text))
        )
      ) THEN 0::numeric(12,2)
      ELSE (COALESCE(affiliate_amount, 0::numeric) + COALESCE(transaction_fee, 0::numeric))::numeric(12,2)
    END AS comissao_afiliado_bruta,

    CASE WHEN (
        EXISTS (SELECT 1 FROM sz_motoboy_pedidos m
                 WHERE m.wc_order_id = COALESCE(o.wp_order_id, o.id)
                   AND m.status::text = 'frustrado'::text)
        OR (
          (status::text = ANY (ARRAY['frustrado'::character varying, 'cancelled'::character varying, 'reembolsado'::character varying]::text[]))
          AND NOT (EXISTS (SELECT 1 FROM sz_motoboy_pedidos m
                            WHERE m.wc_order_id = o.wp_order_id AND m.status::text <> 'cancelado'::text))
        )
      ) THEN 0::numeric(12,2)
      ELSE COALESCE(transaction_fee, 0::numeric)::numeric(12,2)
    END AS taxa_transacao_afiliado,

    CASE WHEN (
        EXISTS (SELECT 1 FROM sz_motoboy_pedidos m
                 WHERE m.wc_order_id = COALESCE(o.wp_order_id, o.id)
                   AND m.status::text = 'frustrado'::text)
        OR (
          (status::text = ANY (ARRAY['frustrado'::character varying, 'cancelled'::character varying, 'reembolsado'::character varying]::text[]))
          AND NOT (EXISTS (SELECT 1 FROM sz_motoboy_pedidos m
                            WHERE m.wc_order_id = o.wp_order_id AND m.status::text <> 'cancelado'::text))
        )
      ) THEN 0::numeric(12,2)
      ELSE COALESCE(affiliate_amount, 0::numeric)::numeric(12,2)
    END AS comissao_afiliado_liquida,

    -- ----- taxa de entrega (zerada quando cobra frustração) -----
    CASE WHEN (
        EXISTS (SELECT 1 FROM sz_motoboy_pedidos m
                 WHERE m.wc_order_id = COALESCE(o.wp_order_id, o.id)
                   AND m.status::text = 'frustrado'::text)
        OR (
          (status::text = ANY (ARRAY['frustrado'::character varying, 'cancelled'::character varying, 'reembolsado'::character varying]::text[]))
          AND NOT (EXISTS (SELECT 1 FROM sz_motoboy_pedidos m
                            WHERE m.wc_order_id = o.wp_order_id AND m.status::text <> 'cancelado'::text))
        )
      ) THEN 0::numeric(12,2)
      ELSE COALESCE(delivery_fee, 0::numeric)::numeric(12,2)
    END AS taxa_entrega,

    -- ----- flag `frustrado` = condição de zeragem do normal -----
    (
      EXISTS (SELECT 1 FROM sz_motoboy_pedidos m
               WHERE m.wc_order_id = COALESCE(o.wp_order_id, o.id)
                 AND m.status::text = 'frustrado'::text)
      OR (
        (status::text = ANY (ARRAY['frustrado'::character varying, 'cancelled'::character varying, 'reembolsado'::character varying]::text[]))
        AND NOT (EXISTS (SELECT 1 FROM sz_motoboy_pedidos m
                          WHERE m.wc_order_id = o.wp_order_id AND m.status::text <> 'cancelado'::text))
      )
    ) AS frustrado,

    -- ----- bloco PRODUTOR (zerado quando cobra frustração) -----
    CASE WHEN (
        EXISTS (SELECT 1 FROM sz_motoboy_pedidos m
                 WHERE m.wc_order_id = COALESCE(o.wp_order_id, o.id)
                   AND m.status::text = 'frustrado'::text)
        OR (
          (status::text = ANY (ARRAY['frustrado'::character varying, 'cancelled'::character varying, 'reembolsado'::character varying]::text[]))
          AND NOT (EXISTS (SELECT 1 FROM sz_motoboy_pedidos m
                            WHERE m.wc_order_id = o.wp_order_id AND m.status::text <> 'cancelado'::text))
        )
      ) THEN 0::numeric(12,2)
      ELSE round(COALESCE(total, 0::numeric) * COALESCE(( SELECT NULLIF(senderzz_options.value, ''::text)::numeric AS "nullif"
         FROM senderzz_options
        WHERE senderzz_options.name::text = 'sz_producer_transaction_fee_pct'::text), 4.99) / 100::numeric, 2)::numeric(12,2)
    END AS taxa_transacao_produtor,

    CASE WHEN (
        EXISTS (SELECT 1 FROM sz_motoboy_pedidos m
                 WHERE m.wc_order_id = COALESCE(o.wp_order_id, o.id)
                   AND m.status::text = 'frustrado'::text)
        OR (
          (status::text = ANY (ARRAY['frustrado'::character varying, 'cancelled'::character varying, 'reembolsado'::character varying]::text[]))
          AND NOT (EXISTS (SELECT 1 FROM sz_motoboy_pedidos m
                            WHERE m.wc_order_id = o.wp_order_id AND m.status::text <> 'cancelado'::text))
        )
      ) THEN 0::numeric(12,2)
      ELSE (COALESCE(total, 0::numeric) - (COALESCE(affiliate_amount, 0::numeric) + COALESCE(transaction_fee, 0::numeric)) - COALESCE(delivery_fee, 0::numeric) - round(COALESCE(total, 0::numeric) * COALESCE(( SELECT NULLIF(senderzz_options.value, ''::text)::numeric AS "nullif"
         FROM senderzz_options
        WHERE senderzz_options.name::text = 'sz_producer_transaction_fee_pct'::text), 4.99) / 100::numeric, 2))::numeric(12,2)
    END AS liquido_produtor,

    -- ----- take_falk -----
    -- Frustrado (cobra): SÓ as penalties congeladas (normal zerou).
    -- Caso contrário: fórmula original (taxa_afil + taxa_prod + markup_entrega).
    CASE WHEN (
        EXISTS (SELECT 1 FROM sz_motoboy_pedidos m
                 WHERE m.wc_order_id = COALESCE(o.wp_order_id, o.id)
                   AND m.status::text = 'frustrado'::text)
        OR (
          (status::text = ANY (ARRAY['frustrado'::character varying, 'cancelled'::character varying, 'reembolsado'::character varying]::text[]))
          AND NOT (EXISTS (SELECT 1 FROM sz_motoboy_pedidos m
                            WHERE m.wc_order_id = o.wp_order_id AND m.status::text <> 'cancelado'::text))
        )
      ) THEN (
        COALESCE((
            SELECT m.valor_taxa_frustrado
              FROM sz_motoboy_pedidos m
             WHERE m.wc_order_id = COALESCE(o.wp_order_id, o.id)
               AND m.status::text = 'frustrado'::text
             ORDER BY m.ts_frustrado DESC NULLS LAST, m.id DESC
             LIMIT 1
        ), 0::numeric)
        + COALESCE((
            SELECT m.valor_taxa_frustrado_afiliado
              FROM sz_motoboy_pedidos m
             WHERE m.wc_order_id = COALESCE(o.wp_order_id, o.id)
               AND m.status::text = 'frustrado'::text
             ORDER BY m.ts_frustrado DESC NULLS LAST, m.id DESC
             LIMIT 1
        ), 0::numeric)
      )::numeric(12,2)
      ELSE (COALESCE(transaction_fee, 0::numeric) + round(COALESCE(total, 0::numeric) * COALESCE(( SELECT NULLIF(senderzz_options.value, ''::text)::numeric AS "nullif"
         FROM senderzz_options
        WHERE senderzz_options.name::text = 'sz_producer_transaction_fee_pct'::text), 4.99) / 100::numeric, 2) + GREATEST(COALESCE(delivery_fee, 0::numeric) - COALESCE(( SELECT NULLIF(senderzz_options.value, ''::text)::numeric AS "nullif"
         FROM senderzz_options
        WHERE senderzz_options.name::text = 'motoboy_repasse_padrao'::text), 18::numeric), 0::numeric))::numeric(12,2)
    END AS take_falk,

    -- ----- penalties CONGELADAS (LÊ o congelado, NÃO recalcula) -----
    -- Colunas NOVAS — anexadas no FIM p/ CREATE OR REPLACE preservar a ordem das
    -- colunas pré-existentes (Postgres só permite ADICIONAR colunas no final).
    -- Subquery escalar correlacionada (NÃO join) — uma só linha mesmo com
    -- múltiplas tentativas/reagendamentos. Pega a frustrada mais recente.
    COALESCE((
        SELECT m.valor_taxa_frustrado
          FROM sz_motoboy_pedidos m
         WHERE m.wc_order_id = COALESCE(o.wp_order_id, o.id)
           AND m.status::text = 'frustrado'::text
         ORDER BY m.ts_frustrado DESC NULLS LAST, m.id DESC
         LIMIT 1
    ), 0::numeric)::numeric(12,2) AS taxa_frustracao_produtor,

    COALESCE((
        SELECT m.valor_taxa_frustrado_afiliado
          FROM sz_motoboy_pedidos m
         WHERE m.wc_order_id = COALESCE(o.wp_order_id, o.id)
           AND m.status::text = 'frustrado'::text
         ORDER BY m.ts_frustrado DESC NULLS LAST, m.id DESC
         LIMIT 1
    ), 0::numeric)::numeric(12,2) AS taxa_frustracao_afiliado

   FROM sz_orders o;
