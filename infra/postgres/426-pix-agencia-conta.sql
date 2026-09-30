-- =============================================================================
-- 426-pix-agencia-conta.sql
-- Senderzz — colunas de AGÊNCIA e CONTA nas contas de saque PIX (COD).
--
-- POR QUÊ: o formulário PIX do Portal V2 (portal-ui/src/pages/Settings.tsx ::
--   addPixAccount) já ENVIA `agencia` e `conta` no body do POST
--   /portal/wallet/accounts, mas o espelho Postgres de sz_cod_withdraw_accounts
--   não tinha onde gravá-los — os campos eram silenciosamente descartados.
--   Aqui adicionamos as duas colunas (nullable, sem default) para que o handler
--   AddAccount (go/portal/internal/handlers/wallet.go) possa persistir o dado
--   bancário informado pelo titular.
--
-- DIVERGÊNCIA CONSCIENTE do WP (sz_cod_rest_save_pix grava bank_name/bank_code/
--   agency/account_number/account_type COM validação obrigatória): aqui só
--   espelhamos agencia + conta e mantemos AMBAS OPCIONAIS — o fluxo PIX do
--   Portal V2 não exige conta/agência para chave PIX, então não introduzimos
--   novos 422. Os demais campos bancários do WP não são portados.
--
-- IDEMPOTENTE: ADD COLUMN IF NOT EXISTS — re-rodar é no-op.
-- =============================================================================

ALTER TABLE sz_cod_withdraw_accounts
    ADD COLUMN IF NOT EXISTS agencia varchar(32);

ALTER TABLE sz_cod_withdraw_accounts
    ADD COLUMN IF NOT EXISTS conta varchar(64);
