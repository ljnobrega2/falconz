-- Eventos detalhados da transportadora obtidos via 17TRACK.
CREATE TABLE IF NOT EXISTS wc_carrier_tracking_sync (
    tracking_code   VARCHAR(100) PRIMARY KEY,
    carrier_code    INTEGER NOT NULL,
    last_checked_at TIMESTAMPTZ NULL,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS wc_carrier_tracking_events (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tracking_code  VARCHAR(100) NOT NULL,
    event_hash     CHAR(64) NOT NULL,
    event_at       TIMESTAMPTZ NOT NULL,
    description    TEXT NOT NULL,
    location       VARCHAR(255) NOT NULL DEFAULT '',
    stage          VARCHAR(80) NOT NULL DEFAULT '',
    observed_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_carrier_tracking_event UNIQUE (tracking_code, event_hash)
);

CREATE INDEX IF NOT EXISTS idx_carrier_tracking_events_code_at
    ON wc_carrier_tracking_events (tracking_code, event_at DESC);
