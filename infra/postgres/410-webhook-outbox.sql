-- =============================================================================
-- 410-webhook-outbox.sql — Transactional Outbox p/ webhooks de saída (integrações).
--   Problema: senderzz_portal_webhooks é configurável mas NADA dispara em
--   transição de status de pedido (a "conexão" nunca conecta).
--   Solução (padrão outbox): um TRIGGER em sz_orders enfileira o evento na MESMA
--   transação do UPDATE de status — dual-write ATÔMICO, sem I/O externo no path
--   financeiro. Um dispatcher (go/cron) lê o outbox FORA da transação, enriquece o
--   payload e POSTa (HMAC, retry/backoff, SSRF-guard, log).
--
-- SEGURANÇA OPERACIONAL: o DISPATCH é gateado pela option `webhook_dispatch_enabled`
-- (default '0' = DESLIGADO). O enqueue sempre roda (captura eventos), mas nada é
-- enviado a endpoint real até o dono LIGAR a flag após revisar. O dispatcher também
-- só processa eventos FRESCOS (janela curta) p/ não inundar endpoints com backlog
-- antigo quando a flag for ligada. Idempotente.
-- =============================================================================

-- ── Tabela outbox ─────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS sz_webhook_outbox (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    order_id        bigint      NOT NULL,
    event_type      varchar(60) NOT NULL,          -- ex.: order_status_enviado
    status_de       varchar(40),
    status_para     varchar(40),
    attempts        integer     NOT NULL DEFAULT 0,
    max_attempts    integer     NOT NULL DEFAULT 6,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    sent_at         timestamptz,                    -- NULL = pendente
    last_error      text,
    created_at      timestamptz NOT NULL DEFAULT now()
);

-- Índice de poll do dispatcher: só pendentes prontos p/ nova tentativa.
CREATE INDEX IF NOT EXISTS idx_wh_outbox_pending
    ON sz_webhook_outbox (next_attempt_at)
    WHERE sent_at IS NULL;

COMMENT ON TABLE sz_webhook_outbox IS
  'Transactional outbox de webhooks de pedido. Populado por trigger em sz_orders (atômico). Consumido por go/cron job sz_webhook_dispatch (gateado por webhook_dispatch_enabled).';

-- ── Função + trigger de enqueue (atômico, em qualquer writer de status) ──────────
-- Roda DENTRO da transação do UPDATE → se a transição der rollback, o evento some
-- junto (sem evento fantasma) e vice-versa. Bulletproof: 1 INSERT condicional numa
-- tabela local nova; não referencia nada frágil. NUNCA deve abortar um UPDATE real.
CREATE OR REPLACE FUNCTION sz_webhook_outbox_enqueue() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO sz_webhook_outbox (order_id, event_type, status_de, status_para)
    VALUES (NEW.id, 'order_status_' || NEW.status, OLD.status, NEW.status);
    RETURN NEW;
END; $$;
COMMENT ON FUNCTION sz_webhook_outbox_enqueue() IS
  'Trigger: enfileira evento de mudança de status no outbox de webhook (mesma tx do UPDATE).';

DROP TRIGGER IF EXISTS trg_sz_webhook_outbox ON sz_orders;
CREATE TRIGGER trg_sz_webhook_outbox
    AFTER UPDATE OF status ON sz_orders
    FOR EACH ROW
    WHEN (NEW.status IS DISTINCT FROM OLD.status)
    EXECUTE FUNCTION sz_webhook_outbox_enqueue();

-- ── Flag de dispatch (DEFAULT DESLIGADO) ──────────────────────────────────────
-- Ligar SÓ após o dono revisar (publica dados a endpoint externo). Não sobrescreve
-- se já existir (respeita escolha do operador).
INSERT INTO senderzz_options (name, value, autoload)
VALUES ('webhook_dispatch_enabled', '0', 'yes')
ON CONFLICT (name) DO NOTHING;

-- Janela de frescor (segundos): dispatcher ignora eventos mais velhos que isto, p/
-- não inundar endpoints com backlog acumulado enquanto a flag estava desligada.
INSERT INTO senderzz_options (name, value, autoload)
VALUES ('webhook_dispatch_freshness_seconds', '7200', 'yes')
ON CONFLICT (name) DO NOTHING;
