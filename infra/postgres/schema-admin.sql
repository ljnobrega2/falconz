-- Schema admin: usuários super-privilegiados separados de portal_users.

-- Histórico de execuções de cron (manual trigger + future worker runs).
-- senderzz_cron_status guarda a última execução; esta tabela guarda todas.
CREATE TABLE IF NOT EXISTS senderzz_cron_runs (
    id            BIGINT NOT NULL GENERATED ALWAYS AS IDENTITY,
    name          VARCHAR(120) NOT NULL,
    started_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    duration_ms   BIGINT       NOT NULL DEFAULT 0,
    status        VARCHAR(30)  NOT NULL DEFAULT 'manual_trigger',
    message       TEXT         NOT NULL DEFAULT '',
    PRIMARY KEY (id)
);

CREATE INDEX IF NOT EXISTS idx_cron_runs_name_started ON senderzz_cron_runs (name, started_at DESC);

COMMENT ON TABLE senderzz_cron_runs IS 'Histórico de execuções de cron (manual trigger + jobs automáticos)';

CREATE TABLE IF NOT EXISTS senderzz_admin_users (
    id            BIGINT NOT NULL GENERATED ALWAYS AS IDENTITY,
    email         VARCHAR(255) NOT NULL UNIQUE,
    nome          VARCHAR(255) NOT NULL DEFAULT '',
    password_hash VARCHAR(255) NOT NULL,
    ativo         BOOLEAN NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id)
);

CREATE INDEX IF NOT EXISTS idx_admin_email_lower ON senderzz_admin_users (LOWER(email));

COMMENT ON TABLE senderzz_admin_users IS 'Usuários admin do painel UI (separados de portal_users)';
