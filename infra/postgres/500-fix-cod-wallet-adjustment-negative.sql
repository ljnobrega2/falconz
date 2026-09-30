-- ============================================================================
-- 500 — Permite 'adjustment' negativo em sz_cod_wallet_transactions.
--
-- CONTEXTO (AUDIT-2026-07-14): estorno manual de crédito a maior (drift de
-- fórmula histórica, 12 pedidos do produtor 15, R$12,32) usa type='adjustment'
-- com gross/net negativos (débito). O CHECK de 499 só liberava negativo para
-- withdrawal/freight_transfer/anticipation — adjustment é o tipo correto p/
-- correções manuais pontuais (positivas OU negativas), então precisa do mesmo
-- tratamento.
--
-- Idempotente.
-- ============================================================================

ALTER TABLE sz_cod_wallet_transactions
  DROP CONSTRAINT IF EXISTS sz_cod_wtx_gross_nonneg;
ALTER TABLE sz_cod_wallet_transactions
  ADD CONSTRAINT sz_cod_wtx_gross_nonneg
  CHECK (gross >= 0 OR type IN ('withdrawal','freight_transfer','anticipation','adjustment'));

ALTER TABLE sz_cod_wallet_transactions
  DROP CONSTRAINT IF EXISTS sz_cod_wtx_net_nonneg;
ALTER TABLE sz_cod_wallet_transactions
  ADD CONSTRAINT sz_cod_wtx_net_nonneg
  CHECK (net >= 0 OR type IN ('withdrawal','freight_transfer','anticipation','adjustment'));
