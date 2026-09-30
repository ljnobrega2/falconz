-- AUDIT-2026-07-31 — versiona sz_wallet_freight_transfers (transferência
-- Carteira COD → Expedição). Feature JÁ existe e funciona em produção,
-- construída direto no banco sem passar por migration versionada (mesmo
-- padrão do sz_order_financials, ver 523). Os rascunhos 497/498 estavam
-- desatualizados (schema real tem coluna recarga_id + FK p/ tpc_recargas,
-- e não tem a constraint gross_nonneg que 497 tentava adicionar — por isso
-- 497 falhava contra dado real). Este arquivo apenas documenta/versiona o
-- que já está rodando, fiel ao extraído de produção. Idempotente.

CREATE TABLE IF NOT EXISTS sz_wallet_freight_transfers (
    id           BIGINT        NOT NULL GENERATED ALWAYS AS IDENTITY,
    user_id      BIGINT        NOT NULL,
    wp_user_id   BIGINT        NOT NULL,
    amount       NUMERIC(10,2) NOT NULL CHECK (amount > 0),
    status       VARCHAR(20)   NOT NULL DEFAULT 'pending'
                     CHECK (status IN ('pending', 'approved', 'rejected')),
    admin_note   TEXT          NULL,
    requested_at TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    decided_at   TIMESTAMPTZ   NULL,
    decided_by   BIGINT        NULL,
    recarga_id   BIGINT        NULL REFERENCES tpc_recargas(id),
    PRIMARY KEY (id)
);

CREATE INDEX IF NOT EXISTS idx_wft_user   ON sz_wallet_freight_transfers (user_id);
CREATE INDEX IF NOT EXISTS idx_wft_status ON sz_wallet_freight_transfers (status);

ALTER TABLE sz_cod_wallet_transactions DROP CONSTRAINT IF EXISTS sz_cod_wallet_transactions_type_check;
ALTER TABLE sz_cod_wallet_transactions ADD CONSTRAINT sz_cod_wallet_transactions_type_check
    CHECK (type IN ('cod_received','withdrawal','adjustment','refund','fee','freight_transfer'));

ALTER TABLE sz_cod_wallet_transactions DROP CONSTRAINT IF EXISTS sz_cod_wtx_net_nonneg;
ALTER TABLE sz_cod_wallet_transactions ADD  CONSTRAINT sz_cod_wtx_net_nonneg
    CHECK (net >= 0 OR type IN ('withdrawal','freight_transfer','anticipation','adjustment'));
