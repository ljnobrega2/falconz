-- =============================================================================
-- 464-referral-payout-returns-int.sql — sz_referral_payout() RETURNS integer  // REF-PAYOUT
--
-- POR QUE: o runner Go (go/cron/cmd/server/main.go) chama TODOS os jobs SQL via
--   `pool.QueryRow(ctx, "SELECT sz_<fn>()").Scan(&count int)`. Os 6 demais jobs
--   (sz_cod_release_due, sz_affiliate_release_due, sz_cleanup_expired_sessions,
--   sz_cancel_preagendados_vencidos, sz_anonymize_old_pii, sz_anonymize_old_order_pii)
--   já RETURNAM integer no banco vivo. Só sz_referral_payout (462) ficou RETURNS void
--   → o Scan(&count) QUEBRA (não há coluna para escanear num void) e o job vira 'error'
--   a cada tick. Esta migração alinha o contrato: RETURNS integer = nº de recompensas
--   efetivamente creditadas nesta execução.
--
-- DROP antes de CREATE (obrigatório): Postgres NÃO permite CREATE OR REPLACE mudar o
--   tipo de retorno de uma função existente (void → integer dá
--   "ERROR: cannot change return type of existing function"). Nada depende da função
--   (só o runner a chama em runtime), então DROP simples, SEM CASCADE.
--
-- LÓGICA IDÊNTICA à 462 — mesma atribuição (sz_orders.produtor_id → referred_by),
--   mesmas taxas (COD 2,5% / frete 1%), mesmo status terminal (completo/entregue),
--   self-referral barrado, MESMA idempotência (UNIQUE ref + ON CONFLICT DO NOTHING).
--   A única mudança é envolver o INSERT num CTE com RETURNING e contar as linhas
--   REALMENTE inseridas: re-rodar credita 0 (ON CONFLICT não retorna nada) → count=0
--   no 2º tick, count=N novas no 1º. Idempotência preservada, agora observável.
--
-- Idempotente em base nova e com dados. Não toca affiliate_transactions nem revenue.
-- =============================================================================

-- (0) DROP obrigatório: muda o tipo de retorno (void → integer). Nada depende dela.
DROP FUNCTION IF EXISTS sz_referral_payout();

-- (1) Recria com RETURNS integer. CTE conta só as linhas EFETIVAMENTE inseridas
--     (RETURNING não emite linhas suprimidas pelo ON CONFLICT) → count = novos créditos.
CREATE OR REPLACE FUNCTION sz_referral_payout() RETURNS integer AS $fn$
    WITH ins AS (
        INSERT INTO senderzz_referral_rewards
            (order_id, referrer_id, referred_id, kind, base_amount, pct, amount, ref, created_at)
        SELECT o.id, u.referred_by, u.id, k.kind, COALESCE(o.total,0), k.pct,
               ROUND(COALESCE(o.total,0) * k.pct / 100.0, 2),
               'referral:order:' || o.id, COALESCE(o.created_at, NOW())
        FROM sz_orders o
        JOIN senderzz_portal_users u
             ON u.id = o.produtor_id
            AND u.referred_by IS NOT NULL
            AND u.referred_by <> u.id              -- self-referral barrado
        CROSS JOIN LATERAL (
            SELECT CASE WHEN EXISTS (SELECT 1 FROM sz_motoboy_pedidos m
                                      WHERE m.wc_order_id = COALESCE(o.wp_order_id, o.id))
                        THEN 'cod' ELSE 'frete' END AS kind,
                   CASE WHEN EXISTS (SELECT 1 FROM sz_motoboy_pedidos m
                                      WHERE m.wc_order_id = COALESCE(o.wp_order_id, o.id))
                        THEN 2.5 ELSE 1.0 END AS pct
        ) k
        WHERE o.status IN ('completo','entregue')  -- status terminal; nunca frustrado/cancelado/aguardando
          AND COALESCE(o.total,0) > 0
        ON CONFLICT (ref) DO NOTHING
        RETURNING 1
    )
    SELECT COALESCE(COUNT(*), 0)::int FROM ins;
$fn$ LANGUAGE sql;

COMMENT ON FUNCTION sz_referral_payout() IS
  'Cron de recompensa de indicação. RETURNS integer (nº de créditos novos). Idempotente (UNIQUE ref). Credita produtor indicado concluído: COD 2,5% / frete 1%. REF-PAYOUT 462/464.';
