-- AUDIT-2026-07-30 MEDIUM: sz_webhook_outbox.processRow (go/cron) reenviava pra
-- TODOS os webhooks casados a cada retry, mesmo os que já tinham confirmado 2xx
-- numa tentativa anterior — só sent_at/attempts é rastreado por LINHA, não por
-- webhook individual. Cenário: linha casa 2 webhooks, A responde 200, B dá
-- timeout → próximo tick reenvia pra A de novo (que já processou o evento).
-- Fix: rastreia quais webhook_id já confirmaram 2xx nesta linha do outbox;
-- retries subsequentes pulam quem já confirmou.
ALTER TABLE sz_webhook_outbox
    ADD COLUMN IF NOT EXISTS delivered_hook_ids bigint[] NOT NULL DEFAULT '{}';
