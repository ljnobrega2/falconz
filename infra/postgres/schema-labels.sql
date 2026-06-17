-- =============================================================================
-- Senderzz — Schema Postgres 16 para o módulo Labels / Melhor Envio (Fase 4)
-- Tabelas greenfield (não migradas de MySQL) — sem equivalente direto no WP.
-- Dados legados de etiquetas vivem em wp_postmeta / wp_wc_orders_meta como
-- _wc_melhor_envio_* e serão backfillados por script separado.
--
-- Diferença intencional em relação a schema-motoboy.sql:
--   Aqui usamos TIMESTAMPTZ (com timezone) em vez de TIMESTAMP WITHOUT TIME ZONE.
--   Motivo: estas tabelas são greenfield no Postgres (sem origem MySQL),
--   portanto não há risco do desvio de 3h que ocorre ao migrar DATETIME
--   wall-clock Brasília. O serviço Go usa time.UTC internamente; TIMESTAMPTZ
--   preserva a semântica correta em qualquer fuso. Ver comentário em
--   schema-motoboy.sql sobre a razão da exceção no módulo Motoboy.
--
-- Convenções:
--   BIGINT GENERATED ALWAYS AS IDENTITY — equivalente ao AUTO_INCREMENT do MySQL
--   CHECK (col IN (...)) em vez de tipo ENUM — portabilidade e migrations simples
--   UNIQUE em me_shipment_id: Postgres permite múltiplos NULLs → rascunhos (draft)
--   seguros; o handler insere NULL (não '') quando shipment_id ainda não existe.
-- =============================================================================

-- DROP em ordem reversa de FK para re-execução idempotente em dev / CI.
DROP TABLE IF EXISTS wc_me_shipment_cache CASCADE;
DROP TABLE IF EXISTS wc_me_queue          CASCADE;
DROP TABLE IF EXISTS wc_me_labels         CASCADE;

-- =============================================================================
-- wc_me_labels — Etiquetas de envio Melhor Envio por pedido WooCommerce
-- =============================================================================
CREATE TABLE wc_me_labels (
    id              BIGINT        NOT NULL GENERATED ALWAYS AS IDENTITY,
    wc_order_id     BIGINT        NOT NULL,
    -- wc_order_id: ID do pedido WooCommerce (wp_posts.ID ou wp_wc_orders.id em HPOS).
    -- Não há FK pois Postgres e WP/MySQL são bancos separados.
    me_shipment_id  VARCHAR(100)  NULL,
    -- me_shipment_id: ID do carrinho/pedido no ME (preenchido após CreateShipment).
    -- NULL enquanto status=draft. UNIQUE em coluna nullable: Postgres permite
    -- múltiplos NULLs, portanto várias etiquetas em rascunho coexistem sem conflito.
    me_label_id     VARCHAR(100)  NULL,
    -- me_label_id: ID da etiqueta gerada no ME (preenchido após GenerateLabel).
    status          VARCHAR(30)   NOT NULL DEFAULT 'draft'
                        CHECK (status IN ('draft','released','posted','delivered','canceled','lost')),
    service_id      INTEGER       NOT NULL DEFAULT 0,
    -- service_id: ID do serviço ME (transportadora). 0 = ainda não selecionado (draft).
    service_name    VARCHAR(100)  NULL,
    -- service_name: nome da transportadora retornado pela ME API (ex: "SEDEX").
    price           DECIMAL(10,2) NULL,
    -- CRIT-01: price é SEMPRE preenchido via /me/shipment/calculate (server-side).
    -- NUNCA aceitar ou persistir valor enviado pelo cliente. NULL enquanto em draft.
    tracking_code   VARCHAR(50)   NULL,
    label_url       TEXT          NULL,
    -- label_url: URL de visualização da etiqueta no painel ME.
    label_pdf_path  TEXT          NULL,
    -- label_pdf_path: caminho local após download do PDF (preenchido pelo job ProcessGeneratePDF).
    from_cep        VARCHAR(10)   NULL,
    -- from_cep: CEP de origem — armazenado para recalcular se necessário.
    to_cep          VARCHAR(10)   NULL,
    -- to_cep: CEP de destino — armazenado para recalcular e como chave de cache.
    weight_g        INTEGER       NULL,
    -- weight_g: peso total em gramas (soma dos produtos).
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),

    PRIMARY KEY (id),
    CONSTRAINT uq_me_shipment_id UNIQUE (me_shipment_id),
    CONSTRAINT chk_label_status  CHECK (status IN ('draft','released','posted','delivered','canceled','lost'))
);

COMMENT ON TABLE wc_me_labels IS 'Etiquetas de frete geradas via Melhor Envio API — criadas e rastreadas pelo labels-service (Fase 4)';
COMMENT ON COLUMN wc_me_labels.price IS 'CRIT-01: SEMPRE recalculado server-side via /me/shipment/calculate. Nunca persistir valor enviado pelo cliente.';
COMMENT ON COLUMN wc_me_labels.me_shipment_id IS 'NULL enquanto status=draft; UNIQUE com múltiplos NULLs permitidos pelo Postgres para rascunhos concorrentes.';

CREATE INDEX idx_labels_order     ON wc_me_labels (wc_order_id);
CREATE INDEX idx_labels_status    ON wc_me_labels (status);
CREATE INDEX idx_labels_track     ON wc_me_labels (tracking_code) WHERE tracking_code IS NOT NULL;
CREATE INDEX idx_labels_created   ON wc_me_labels (created_at);

-- =============================================================================
-- wc_me_queue — Fila durável de jobs para processamento assíncrono de etiquetas
-- Complementa o Asynq (Redis) com registro auditável no Postgres.
-- Permite retry rastreável e diagnóstico de falhas por label_id.
-- =============================================================================
CREATE TABLE wc_me_queue (
    id            BIGINT       NOT NULL GENERATED ALWAYS AS IDENTITY,
    label_id      BIGINT       NOT NULL,
    -- label_id: referência à etiqueta que originou o job.
    action        VARCHAR(30)  NOT NULL
                      CHECK (action IN ('generate_pdf','sync_tracking','cancel','retry')),
    attempts      INTEGER      NOT NULL DEFAULT 0,
    -- attempts: incrementado pelo worker a cada tentativa (incluindo falhas).
    last_error    TEXT         NULL,
    -- last_error: mensagem do último erro (para diagnóstico; não expor ao cliente).
    scheduled_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    -- scheduled_at: quando o job deve ser processado (NOW() para imediato).
    processed_at  TIMESTAMPTZ  NULL,
    -- processed_at: NULL = pendente; NOT NULL = finalizado (sucesso ou falha definitiva).
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    PRIMARY KEY (id),
    CONSTRAINT fk_queue_label FOREIGN KEY (label_id) REFERENCES wc_me_labels (id) ON DELETE CASCADE
);

COMMENT ON TABLE wc_me_queue IS 'Fila durável de jobs de etiquetas (complementa Asynq/Redis com registro auditável e rastreável)';
COMMENT ON COLUMN wc_me_queue.attempts   IS 'Incrementado pelo worker Asynq a cada tentativa';
COMMENT ON COLUMN wc_me_queue.processed_at IS 'NULL = pendente; NOT NULL = finalizado (sucesso ou falha definitiva)';

CREATE INDEX idx_queue_label     ON wc_me_queue (label_id);
CREATE INDEX idx_queue_action    ON wc_me_queue (action);
CREATE INDEX idx_queue_scheduled ON wc_me_queue (scheduled_at) WHERE processed_at IS NULL;
-- Índice parcial: apenas jobs pendentes para varredura eficiente pelo worker.

-- =============================================================================
-- wc_me_shipment_cache — Cache de resultados de /me/shipment/calculate (TTL 10 min)
-- Evita chamadas repetidas à ME API no checkout quando o carrinho não mudou.
-- TTL gerenciado pelo serviço Go; Postgres não expira linhas automaticamente.
-- Rotina periódica de purge remove linhas com expires_at < NOW().
-- =============================================================================
CREATE TABLE wc_me_shipment_cache (
    id          BIGINT        NOT NULL GENERATED ALWAYS AS IDENTITY,
    cache_key   VARCHAR(100)  NOT NULL,
    -- cache_key: hash (SHA-256, 64 chars) dos parâmetros de cálculo: from_cep + to_cep + produtos.
    payload     JSONB         NOT NULL,
    -- payload: resposta completa de /me/shipment/calculate serializada como JSONB.
    expires_at  TIMESTAMPTZ   NOT NULL,
    -- expires_at: NOW() + 10 min. Handler valida antes de usar e ignora se expirado.
    created_at  TIMESTAMPTZ   NOT NULL DEFAULT NOW(),

    PRIMARY KEY (id),
    CONSTRAINT uq_cache_key UNIQUE (cache_key)
);

COMMENT ON TABLE wc_me_shipment_cache IS 'Cache de 10 min dos resultados de /me/shipment/calculate para evitar chamadas repetidas no checkout';

CREATE INDEX idx_cache_expires ON wc_me_shipment_cache (expires_at);
-- Usado pela rotina de purge periódico de entradas expiradas (DELETE WHERE expires_at < NOW()).
