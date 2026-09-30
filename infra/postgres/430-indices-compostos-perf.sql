-- =============================================================================
-- Senderzz — Patch v430-perf: índices compostos (owner, status)
--
-- AUDIT-DEEP-2026-06-18.md §P2 (infra/postgres / índices):
--   "Sem índice composto (user_id,status) em sz_cod_wallet_transactions /
--    sz_cod_withdrawals / senderzz_affiliate_transactions; queries
--    History/Summary/audit fazem scan em tabela grande."
--
-- ESCOPO: SOMENTE otimização de LEITURA. Não altera dado, coluna, tipo ou
-- regra financeira — apenas adiciona índices secundários. Seguro de aplicar em
-- prod e idempotente (CREATE INDEX IF NOT EXISTS). Não usar CONCURRENTLY: o
-- runner (db-migrate.sh) aplica cada .sql em auto-commit/ON_ERROR_STOP e
-- CONCURRENTLY não roda dentro de bloco de transação implícito do -f.
--
-- Hoje só existem índices single-column (idx_cod_tx_user/_status,
-- idx_cod_wd_user/_status, idx_aff_tx_affiliate/_status). Os compostos abaixo
-- cobrem o par dono+status usado em:
--   - sz_cod_withdrawals     → portal wallet.go:1119 (WHERE user_id=$1 AND status IN (...))
--   - sz_cod_wallet_transactions → portal History (WHERE user_id=$1 …) + admin
--     cod_wallet_transactions.go (WHERE user_id/status dinâmico + LIMIT/OFFSET)
--   - senderzz_affiliate_transactions → portal Summary/History (JOIN por
--     affiliate_id + agregação por status; wallet.go:341)
-- =============================================================================

-- COD: carteira (livro razão) — par (user_id, status)
CREATE INDEX IF NOT EXISTS idx_cod_tx_user_status
    ON sz_cod_wallet_transactions (user_id, status);

-- COD: saques — par (user_id, status)
CREATE INDEX IF NOT EXISTS idx_cod_wd_user_status
    ON sz_cod_withdrawals (user_id, status);

-- Afiliados: livro razão — par (affiliate_id, status)
CREATE INDEX IF NOT EXISTS idx_aff_tx_affiliate_status
    ON senderzz_affiliate_transactions (affiliate_id, status);
