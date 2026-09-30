-- =============================================================================
-- 451-fix-expedicao-webhook-reprocess.sql
-- ALTO-8/9: colunas de reprocessamento que os handlers Go de Expedição › Webhooks
-- (go/admin/internal/handlers/expedicao_webhooks.go) leem/escrevem mas que faltavam
-- na tabela senderzz_producer_webhook_logs (drift schema-vs-código).
--
--   - Logs()      SELECT reprocess_count, last_reprocessed_at  (linhas ~743-744)
--   - Reprocess() UPDATE  reprocess_count, last_reprocessed_at (linhas ~961-962)
--
-- Sem estas colunas a tela "Ver logs" e o botão "Reprocessar" devolviam 500.
-- Idempotente (ADD COLUMN IF NOT EXISTS) — seguro reaplicar.
-- =============================================================================

ALTER TABLE senderzz_producer_webhook_logs
    ADD COLUMN IF NOT EXISTS reprocess_count     INT DEFAULT 0;

ALTER TABLE senderzz_producer_webhook_logs
    ADD COLUMN IF NOT EXISTS last_reprocessed_at TIMESTAMPTZ;
