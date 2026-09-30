-- 535-me-tracking-history.sql
-- Histórico imutável de snapshots retornados pela Melhor Envio.
-- wc_me_labels guarda somente o estado atual; esta tabela preserva cada mudança
-- observada, inclusive a troca Jadlog authorization_code -> tracking definitivo.

CREATE TABLE IF NOT EXISTS wc_me_tracking_history (
    id                  BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    me_shipment_id      VARCHAR(100) NOT NULL,
    wc_order_id         BIGINT NULL,
    me_status           VARCHAR(40) NOT NULL DEFAULT '',
    tracking_code       VARCHAR(100) NOT NULL DEFAULT '',
    authorization_code  VARCHAR(100) NOT NULL DEFAULT '',
    created_at_me       VARCHAR(30) NOT NULL DEFAULT '',
    paid_at_me          VARCHAR(30) NOT NULL DEFAULT '',
    generated_at_me     VARCHAR(30) NOT NULL DEFAULT '',
    posted_at_me        VARCHAR(30) NOT NULL DEFAULT '',
    received_at_me      VARCHAR(30) NOT NULL DEFAULT '',
    delivered_at_me     VARCHAR(30) NOT NULL DEFAULT '',
    canceled_at_me      VARCHAR(30) NOT NULL DEFAULT '',
    observed_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    snapshot_hash       CHAR(64) NOT NULL,
    CONSTRAINT uq_me_tracking_history_snapshot UNIQUE (me_shipment_id, snapshot_hash)
);

CREATE INDEX IF NOT EXISTS idx_me_tracking_history_order
    ON wc_me_tracking_history (wc_order_id, observed_at);
CREATE INDEX IF NOT EXISTS idx_me_tracking_history_shipment
    ON wc_me_tracking_history (me_shipment_id, observed_at);

COMMENT ON TABLE wc_me_tracking_history IS
    'Snapshots imutáveis das etapas e códigos retornados pela Melhor Envio.';
