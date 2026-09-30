-- =============================================================================
-- 474-webhook-log-retention.sql
-- AUDIT-LGPD-CHECKOUT-2026-06-24 #A2 (Art.16 — eliminação após fim da finalidade).
--
-- senderzz_webhook_log (050-portal.sql) grava o payload VERBATIM de cada disparo
-- de webhook — incluindo PII de cliente/entrega (nome, telefone, e-mail, endereço)
-- — em toda tentativa, inclusive retries, e NÃO tinha nenhum TTL/expurgo. As únicas
-- deleções eram manuais e escopadas ao produtor (inalcançáveis pelo titular).
--
-- Esta migração cria sz_purge_webhook_log() que DELETA as linhas com mais de 30
-- dias (coluna de timestamp existente da tabela = created_at TIMESTAMPTZ). DELETE
-- é inerentemente idempotente (rodar 2x não erra; linhas já expurgadas não voltam)
-- e não há FK filha apontando p/ esta tabela. O cron em go/cron a chama diariamente.
-- =============================================================================

CREATE OR REPLACE FUNCTION sz_purge_webhook_log()
RETURNS integer LANGUAGE plpgsql AS $$
DECLARE n integer;
BEGIN
    -- Expurga logs de webhook com mais de 30 dias (PII no payload — Art.16).
    DELETE FROM senderzz_webhook_log
     WHERE created_at < NOW() - INTERVAL '30 days';
    GET DIAGNOSTICS n = ROW_COUNT;
    RETURN n;
END; $$;

COMMENT ON FUNCTION sz_purge_webhook_log() IS
  'LGPD Art.16 (#A2): deleta linhas de senderzz_webhook_log com mais de 30 dias — '
  'payload contém PII de cliente/entrega gravada verbatim em cada disparo/retry. '
  'Idempotente (DELETE por TTL; sem FK filha). Chamada diariamente via go/cron.';

-- Aplica o expurgo já no deploy (idempotente — não espera até 24h do cron).
SELECT sz_purge_webhook_log();
