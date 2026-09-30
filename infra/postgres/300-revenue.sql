-- =============================================================================
-- LIVRO-RAZÃO DE RECEITA DA PLATAFORMA (FALKZ) — senderzz_revenue
-- =============================================================================
-- Motivação (auditoria 2026-06-18): o "take" da FALKZ (4,99% sobre a comissão
-- do afiliado + taxa de transação do produtor) só era SUBTRAÍDO no display, nunca
-- LANÇADO como receita. Sem isto, faturamento (meta R$1.000.000/ano) é
-- imensurável e não-capturado. Esta tabela é o livro-razão de receita.
--
-- Componentes confirmados como receita FALKZ (dono, 2026-06-18):
--   - taxa_afiliado_4_99      = comissão bruta do afiliado × 4,99% (só comissão
--                               normal; penalty/cancelado/revertido NÃO geram).
--   - taxa_transacao_produtor = sz_orders.transaction_fee.
-- Fora de receita: delivery_fee (repasse ao entregador), markup frete, taxa cartão.
-- =============================================================================

CREATE TABLE IF NOT EXISTS senderzz_revenue (
    id           BIGSERIAL PRIMARY KEY,
    order_id     BIGINT,                       -- sz_orders.id (origem)
    affiliate_id BIGINT,                       -- wp_user_id do afiliado (se aplicável)
    produtor_id  BIGINT,                       -- portal_users.id do produtor (se aplicável)
    component    VARCHAR(40) NOT NULL,         -- 'taxa_afiliado_4_99' | 'taxa_transacao_produtor'
    base_amount  NUMERIC(12,2) NOT NULL DEFAULT 0,  -- base sobre a qual incidiu
    amount       NUMERIC(12,2) NOT NULL,            -- RECEITA FALKZ deste lançamento
    ref          VARCHAR(80) NOT NULL,         -- origem idempotente (afftx:<id> | order:<id>)
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Idempotência: a mesma origem nunca é bookada 2x (evita double-count de receita).
    CONSTRAINT uq_revenue_component_ref UNIQUE (component, ref)
);

CREATE INDEX IF NOT EXISTS idx_revenue_order     ON senderzz_revenue (order_id);
CREATE INDEX IF NOT EXISTS idx_revenue_component ON senderzz_revenue (component);
CREATE INDEX IF NOT EXISTS idx_revenue_created   ON senderzz_revenue (created_at);
CREATE INDEX IF NOT EXISTS idx_revenue_produtor  ON senderzz_revenue (produtor_id);

-- ---------------------------------------------------------------------------
-- BACKFILL (idempotente via ON CONFLICT) — popula dos dados já existentes.
-- ---------------------------------------------------------------------------

-- (1) Taxa 4,99% sobre comissão do afiliado — só comissão normal (não penalty,
--     não cancelado/revertido). Afiliado via senderzz_affiliates.afiliado_id (wp_user_id).
INSERT INTO senderzz_revenue (order_id, affiliate_id, component, base_amount, amount, ref, created_at)
SELECT t.order_id,
       a.afiliado_id,
       'taxa_afiliado_4_99',
       t.amount,
       ROUND((t.amount * 0.0499)::numeric, 2),
       'afftx:' || t.id,
       COALESCE(t.created_at, NOW())
FROM senderzz_affiliate_transactions t
JOIN senderzz_affiliates a ON a.id = t.affiliate_id
WHERE t.type = 'commission'
  AND t.status NOT IN ('cancelled', 'reversed')
  AND t.amount > 0
ON CONFLICT (component, ref) DO NOTHING;

-- (2) Taxa de transação do produtor — sz_orders.transaction_fee.
INSERT INTO senderzz_revenue (order_id, produtor_id, component, base_amount, amount, ref, created_at)
SELECT o.id,
       o.produtor_id,
       'taxa_transacao_produtor',
       o.total,
       o.transaction_fee,
       'order:' || o.id,
       COALESCE(o.created_at, NOW())
FROM sz_orders o
WHERE o.transaction_fee > 0
ON CONFLICT (component, ref) DO NOTHING;
