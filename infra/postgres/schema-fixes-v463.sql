-- =============================================================================
-- Senderzz — Patch v463 (COD: producer_id → user_id + colunas gross/fee/net)
--
-- Erros em produção (Wallet Produtor COD / Transações COD / Saques COD):
--   - ERROR: column "user_id" does not exist (SQLSTATE 42703)
--   - ERROR: column "t.user_id" does not exist
-- Causa: schema-fixes-v460 criou as tabelas COD com `producer_id`, mas os
-- handlers Go (cod_wallet_producer.go, cod_wallet_transactions.go, cod_saques.go)
-- esperam `user_id`. Padronizamos em `user_id` (wp_user_id do produtor).
--
-- Também faltavam colunas que os handlers leem em sz_cod_wallet_transactions:
--   - gross / fee / net (decomposição do amount)
--   - release_at      (data de liberação para saque)
--
-- Migração idempotente: usa IF EXISTS / IF NOT EXISTS em todos os passos.
-- Atualiza tb índices renomeados.
-- =============================================================================

-- -----------------------------------------------------------------------------
-- 1) sz_cod_wallet_transactions — rename + new columns
-- -----------------------------------------------------------------------------
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema='public' AND table_name='sz_cod_wallet_transactions'
          AND column_name='producer_id'
    ) AND NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema='public' AND table_name='sz_cod_wallet_transactions'
          AND column_name='user_id'
    ) THEN
        ALTER TABLE sz_cod_wallet_transactions RENAME COLUMN producer_id TO user_id;
    END IF;
END $$;

ALTER TABLE sz_cod_wallet_transactions ADD COLUMN IF NOT EXISTS gross      DECIMAL(10,2) NOT NULL DEFAULT 0.00;
ALTER TABLE sz_cod_wallet_transactions ADD COLUMN IF NOT EXISTS fee        DECIMAL(10,2) NOT NULL DEFAULT 0.00;
ALTER TABLE sz_cod_wallet_transactions ADD COLUMN IF NOT EXISTS net        DECIMAL(10,2) NOT NULL DEFAULT 0.00;
ALTER TABLE sz_cod_wallet_transactions ADD COLUMN IF NOT EXISTS release_at TIMESTAMPTZ   NULL;

-- Backfill: se gross/net=0 e amount>0, copia do amount.
UPDATE sz_cod_wallet_transactions
   SET gross = COALESCE(NULLIF(gross,0), amount),
       net   = COALESCE(NULLIF(net,0),   amount)
 WHERE amount IS NOT NULL AND amount <> 0
   AND (gross = 0 OR net = 0);

-- Recria índices renomeados
DROP INDEX IF EXISTS idx_cod_tx_producer;
CREATE INDEX IF NOT EXISTS idx_cod_tx_user    ON sz_cod_wallet_transactions (user_id);
CREATE INDEX IF NOT EXISTS idx_cod_tx_release ON sz_cod_wallet_transactions (release_at);

-- -----------------------------------------------------------------------------
-- 2) sz_cod_withdrawals — rename producer_id → user_id
-- -----------------------------------------------------------------------------
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema='public' AND table_name='sz_cod_withdrawals'
          AND column_name='producer_id'
    ) AND NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema='public' AND table_name='sz_cod_withdrawals'
          AND column_name='user_id'
    ) THEN
        ALTER TABLE sz_cod_withdrawals RENAME COLUMN producer_id TO user_id;
    END IF;
END $$;

DROP INDEX IF EXISTS idx_cod_wd_producer;
CREATE INDEX IF NOT EXISTS idx_cod_wd_user ON sz_cod_withdrawals (user_id);

-- -----------------------------------------------------------------------------
-- 3) sz_cod_withdraw_accounts — rename producer_id → user_id
-- -----------------------------------------------------------------------------
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema='public' AND table_name='sz_cod_withdraw_accounts'
          AND column_name='producer_id'
    ) AND NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema='public' AND table_name='sz_cod_withdraw_accounts'
          AND column_name='user_id'
    ) THEN
        ALTER TABLE sz_cod_withdraw_accounts RENAME COLUMN producer_id TO user_id;
    END IF;
END $$;

DROP INDEX IF EXISTS idx_cod_acc_producer;
CREATE INDEX IF NOT EXISTS idx_cod_acc_user ON sz_cod_withdraw_accounts (user_id);

-- -----------------------------------------------------------------------------
-- 4) sz_order_meta — garantir colunas UTM/NFE/tracking expostas no OrderDetail
-- (idempotente; meta_key já existe — só uso aqui é normalizar índices)
-- -----------------------------------------------------------------------------
CREATE INDEX IF NOT EXISTS idx_order_meta_order_key ON sz_order_meta (order_id, meta_key);

-- -----------------------------------------------------------------------------
-- 5) sz_cod_withdrawals — colunas exigidas pelo cod_saques.go
-- -----------------------------------------------------------------------------
ALTER TABLE sz_cod_withdrawals ADD COLUMN IF NOT EXISTS fee          DECIMAL(10,2) NOT NULL DEFAULT 0.00;
ALTER TABLE sz_cod_withdrawals ADD COLUMN IF NOT EXISTS net          DECIMAL(10,2) NOT NULL DEFAULT 0.00;
ALTER TABLE sz_cod_withdrawals ADD COLUMN IF NOT EXISTS holder_name  VARCHAR(200) NULL;
ALTER TABLE sz_cod_withdrawals ADD COLUMN IF NOT EXISTS holder_cpf   VARCHAR(20)  NULL;
ALTER TABLE sz_cod_withdrawals ADD COLUMN IF NOT EXISTS admin_note   TEXT         NULL;
ALTER TABLE sz_cod_withdrawals ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ  NULL;
ALTER TABLE sz_cod_withdrawals ADD COLUMN IF NOT EXISTS updated_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW();

-- Backfill net = amount - fee quando net=0 e amount>0.
UPDATE sz_cod_withdrawals
   SET net = amount - COALESCE(fee,0)
 WHERE amount IS NOT NULL AND amount <> 0 AND net = 0;

-- -----------------------------------------------------------------------------
-- 6) sz_cod_withdraw_accounts — holder_name/holder_cpf
-- -----------------------------------------------------------------------------
ALTER TABLE sz_cod_withdraw_accounts ADD COLUMN IF NOT EXISTS holder_name VARCHAR(200) NULL;
ALTER TABLE sz_cod_withdraw_accounts ADD COLUMN IF NOT EXISTS holder_cpf  VARCHAR(20)  NULL;

-- -----------------------------------------------------------------------------
-- 7) sz_cod_wallet_transactions — updated_at
-- -----------------------------------------------------------------------------
ALTER TABLE sz_cod_wallet_transactions ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

-- -----------------------------------------------------------------------------
-- 8) senderzz_affiliate_withdrawals — colunas exigidas pelos handlers
-- -----------------------------------------------------------------------------
ALTER TABLE senderzz_affiliate_withdrawals ADD COLUMN IF NOT EXISTS fee         DECIMAL(10,2) NOT NULL DEFAULT 0.00;
ALTER TABLE senderzz_affiliate_withdrawals ADD COLUMN IF NOT EXISTS net_amount  DECIMAL(10,2) NOT NULL DEFAULT 0.00;
ALTER TABLE senderzz_affiliate_withdrawals ADD COLUMN IF NOT EXISTS bank_info   TEXT          NULL;
ALTER TABLE senderzz_affiliate_withdrawals ADD COLUMN IF NOT EXISTS admin_note  TEXT          NULL;
ALTER TABLE senderzz_affiliate_withdrawals ADD COLUMN IF NOT EXISTS decided_at  TIMESTAMPTZ   NULL;
ALTER TABLE senderzz_affiliate_withdrawals ADD COLUMN IF NOT EXISTS decided_by  BIGINT        NULL;

-- Backfill net_amount = amount - fee.
UPDATE senderzz_affiliate_withdrawals
   SET net_amount = amount - COALESCE(fee,0)
 WHERE amount IS NOT NULL AND amount <> 0 AND net_amount = 0;

-- -----------------------------------------------------------------------------
-- 9) senderzz_affiliate_wallet — rename saldo_* → balance/pending_balance + debt
-- Handlers (affiliate_wallet.go) usam balance/pending_balance/debt_amount.
-- -----------------------------------------------------------------------------
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema='public' AND table_name='senderzz_affiliate_wallet'
          AND column_name='saldo_disponivel'
    ) AND NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema='public' AND table_name='senderzz_affiliate_wallet'
          AND column_name='balance'
    ) THEN
        ALTER TABLE senderzz_affiliate_wallet RENAME COLUMN saldo_disponivel TO balance;
    END IF;

    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema='public' AND table_name='senderzz_affiliate_wallet'
          AND column_name='saldo_pendente'
    ) AND NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema='public' AND table_name='senderzz_affiliate_wallet'
          AND column_name='pending_balance'
    ) THEN
        ALTER TABLE senderzz_affiliate_wallet RENAME COLUMN saldo_pendente TO pending_balance;
    END IF;
END $$;

ALTER TABLE senderzz_affiliate_wallet ADD COLUMN IF NOT EXISTS balance         DECIMAL(10,2) NOT NULL DEFAULT 0.00;
ALTER TABLE senderzz_affiliate_wallet ADD COLUMN IF NOT EXISTS pending_balance DECIMAL(10,2) NOT NULL DEFAULT 0.00;
ALTER TABLE senderzz_affiliate_wallet ADD COLUMN IF NOT EXISTS debt_amount     DECIMAL(10,2) NOT NULL DEFAULT 0.00;

-- -----------------------------------------------------------------------------
-- 10) senderzz_affiliate_transactions — colunas available_at / meta_json / updated_at
-- (DO block protege contra tabela ausente em ambientes sem v460 aplicado.)
-- -----------------------------------------------------------------------------
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema='public' AND table_name='senderzz_affiliate_transactions'
    ) THEN
        ALTER TABLE senderzz_affiliate_transactions ADD COLUMN IF NOT EXISTS available_at TIMESTAMPTZ NULL;
        ALTER TABLE senderzz_affiliate_transactions ADD COLUMN IF NOT EXISTS meta_json    JSONB       NULL;
        ALTER TABLE senderzz_affiliate_transactions ADD COLUMN IF NOT EXISTS updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW();
    END IF;
END $$;

-- =============================================================================
-- FIM v463
-- =============================================================================
