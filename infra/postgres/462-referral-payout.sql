-- =============================================================================
-- 462-referral-payout.sql — RECOMPENSA DE INDICAÇÃO ("indique e ganhe")  // REF-PAYOUT
--
-- REGRA DO DONO (2026-06-22): qualquer usuário tem um link individual de indicação
-- (/r/{referral_code}, já existente — 433/434). Quem se cadastra por ele fica
-- ASSOCIADO ao indicador (referred_by). Quando o INDICADO gera pedido concluído,
-- o INDICADOR recebe:
--   - 2,5% do valor do pedido  → pedido COD (pago na entrega = tem linha em sz_motoboy_pedidos)
--   - 1,0% do valor do pedido  → pedido por Expedição (frete = sem motoboy)
--
-- ATRIBUIÇÃO: o "dono" do pedido para fim de indicação = sz_orders.produtor_id
-- (o vendedor). Se esse produtor tem referred_by setado, o referred_by recebe.
--
-- LEDGER DEDICADO (NÃO usa senderzz_affiliate_transactions): evita o trigger de
-- take 4,99% (trg_sz_revenue_capture_afftx) e a tela Comissões. A recompensa é
-- BRUTA (sem take da plataforma).
--
-- IDEMPOTÊNCIA (crítico — o cron roda a cada tick): UNIQUE(ref),
-- ref = 'referral:order:{order_id}' → no MÁXIMO 1 crédito por pedido, para sempre.
-- INSERT ... ON CONFLICT (ref) DO NOTHING (espelha 423-frustracao-ledger).
--
-- STATUS TERMINAL: credita só em 'completo'/'entregue'. NUNCA frustrado/cancelado/
-- reembolsado/aguardando (espelha financeiroFrustratedStatuses/CancelledStatuses).
--
-- SELF-REFERRAL: barrado (referred_by <> id) — defesa também no Go no signup.
--
-- Idempotente em base nova e com dados. Não toca affiliate_transactions nem revenue.
-- =============================================================================

-- (1) Associação: quem indicou cada usuário (portal id do INDICADOR). Set 1× no
--     signup via /r/{code}; imutável depois (mudar = reescrever payout retroativo).
ALTER TABLE senderzz_portal_users
  ADD COLUMN IF NOT EXISTS referred_by BIGINT;

CREATE INDEX IF NOT EXISTS idx_portal_users_referred_by
  ON senderzz_portal_users (referred_by);

COMMENT ON COLUMN senderzz_portal_users.referred_by IS
  'Portal id do INDICADOR (quem trouxe este usuário via /r/{referral_code}). Set 1x no signup, imutável. REF-PAYOUT 462.';

-- (2) Ledger dedicado da recompensa de indicação.
CREATE TABLE IF NOT EXISTS senderzz_referral_rewards (
    id          BIGSERIAL    PRIMARY KEY,
    order_id    BIGINT,
    referrer_id BIGINT       NOT NULL,            -- portal id de quem RECEBE (indicador)
    referred_id BIGINT,                           -- portal id do indicado (produtor do pedido)
    kind        VARCHAR(10)  NOT NULL,            -- 'cod' (2,5%) | 'frete' (1%)
    base_amount NUMERIC(14,2) NOT NULL,           -- valor do pedido
    pct         NUMERIC(6,3) NOT NULL,            -- 2.5 | 1.0
    amount      NUMERIC(14,2) NOT NULL,           -- recompensa (BRUTA, sem take)
    status      VARCHAR(16)  NOT NULL DEFAULT 'available',
    ref         VARCHAR(64)  NOT NULL,            -- 'referral:order:{order_id}'
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_referral_reward_ref UNIQUE (ref)
);

CREATE INDEX IF NOT EXISTS idx_referral_rewards_referrer ON senderzz_referral_rewards (referrer_id);
CREATE INDEX IF NOT EXISTS idx_referral_rewards_order    ON senderzz_referral_rewards (order_id);

COMMENT ON TABLE senderzz_referral_rewards IS
  'Recompensa de indicação (indique e ganhe). BRUTA, sem take 4,99%. 1 linha por pedido (UNIQUE ref). REF-PAYOUT 462.';

-- (3) Função do CRON — idempotente (ON CONFLICT ref). Roda a cada tick: credita 1×
--     por pedido concluído de um produtor INDICADO. COD (tem motoboy) = 2,5%;
--     frete = 1%. Atribuição por sz_orders.produtor_id → referred_by. Provado:
--     rodar 2× não duplica (INSERT 0 N na 1ª, 0 0 na 2ª). Espelha sz_cod_release_due.
CREATE OR REPLACE FUNCTION sz_referral_payout() RETURNS void AS $fn$
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
    WHERE o.status IN ('completo','entregue')      -- status terminal; nunca frustrado/cancelado/aguardando
      AND COALESCE(o.total,0) > 0
    ON CONFLICT (ref) DO NOTHING;
$fn$ LANGUAGE sql;

COMMENT ON FUNCTION sz_referral_payout() IS
  'Cron de recompensa de indicação. Idempotente (UNIQUE ref). Credita produtor indicado concluído: COD 2,5% / frete 1%. REF-PAYOUT 462.';
