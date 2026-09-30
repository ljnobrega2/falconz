-- ============================================================================
-- 499 — Corrige CHECK constraints de sz_cod_wallet_transactions que bloqueavam
-- a antecipação de recebíveis do portal (self-service do produtor).
--
-- ROOT CAUSE (AUDIT-2026-07-14): go/portal/internal/handlers/wallet.go insere
-- type='anticipation' / 'anticipation_credit' com status='approved', mas o
-- CHECK vigente (type_check) só permite
-- {cod_received,withdrawal,adjustment,refund,fee,freight_transfer} e o
-- status_check só permite {pending,available,released,reversed}. Todo INSERT
-- dessa feature violava constraint → 500 pro produtor. Nenhuma linha
-- type='anticipation*' existe na tabela (confirmado — feature nunca funcionou
-- em produção, mesmo com código presente há tempo).
--
-- Também amplia gross_nonneg/net_nonneg: o débito 'anticipation' grava
-- gross/net NEGATIVOS (mesmo padrão de withdrawal/freight_transfer).
--
-- Idempotente (DROP/ADD sempre seguro de re-rodar).
-- ============================================================================

ALTER TABLE sz_cod_wallet_transactions
  DROP CONSTRAINT IF EXISTS sz_cod_wallet_transactions_type_check;
ALTER TABLE sz_cod_wallet_transactions
  ADD CONSTRAINT sz_cod_wallet_transactions_type_check
  CHECK (type IN ('cod_received','withdrawal','adjustment','refund','fee',
                   'freight_transfer','anticipation','anticipation_credit'));

ALTER TABLE sz_cod_wallet_transactions
  DROP CONSTRAINT IF EXISTS sz_cod_wallet_transactions_status_check;
ALTER TABLE sz_cod_wallet_transactions
  ADD CONSTRAINT sz_cod_wallet_transactions_status_check
  CHECK (status IN ('pending','available','released','reversed','approved',
                     'anticipation_pending'));

ALTER TABLE sz_cod_wallet_transactions
  DROP CONSTRAINT IF EXISTS sz_cod_wtx_gross_nonneg;
ALTER TABLE sz_cod_wallet_transactions
  ADD CONSTRAINT sz_cod_wtx_gross_nonneg
  CHECK (gross >= 0 OR type IN ('withdrawal','freight_transfer','anticipation'));

ALTER TABLE sz_cod_wallet_transactions
  DROP CONSTRAINT IF EXISTS sz_cod_wtx_net_nonneg;
ALTER TABLE sz_cod_wallet_transactions
  ADD CONSTRAINT sz_cod_wtx_net_nonneg
  CHECK (net >= 0 OR type IN ('withdrawal','freight_transfer','anticipation'));
