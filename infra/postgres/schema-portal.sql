-- =============================================================================
-- Senderzz — Schema Postgres 16 para o Portal V2 (Fase 5 — Go Portal Service)
--
-- Convenções de migração (mesmas de schema-motoboy.sql):
--   BIGINT UNSIGNED          → BIGINT
--   TINYINT(1)               → BOOLEAN
--   DATETIME                 → TIMESTAMPTZ (portal usa UTC; timestamps de sessão/2FA
--                              devem ser comparados com NOW() — usar TIMESTAMPTZ é seguro)
--   AUTO_INCREMENT           → GENERATED ALWAYS AS IDENTITY
--   ENUM(...)                → VARCHAR CHECK (col IN (...))
--
-- Nota de compatibilidade:
--   Os serviços Go existentes (motoboy, wallet) consultam tabelas com prefixo "wp_":
--     wp_senderzz_portal_users, wp_senderzz_portal_sessions
--   Este schema usa o mesmo prefixo para manter retrocompatibilidade durante a migração.
--   Adicionar VIEW aliases sem prefixo quando o strangler-fig estiver completo.
--
-- Nota de segurança:
--   password_hash: bcrypt (custo ≥ 12). Nunca armazenar plaintext.
--   token_hmac: HMAC-SHA256(token_raw, WP_SALT_AUTH). Lookup aceita raw OU hmac
--     durante período de migração (V-SEC-01/02).
-- =============================================================================

-- DROP em ordem reversa de dependência para não violar FKs.
DROP TABLE IF EXISTS senderzz_integration_log      CASCADE;
DROP TABLE IF EXISTS senderzz_integration_settings CASCADE;
DROP TABLE IF EXISTS senderzz_webhook_log          CASCADE;
DROP TABLE IF EXISTS senderzz_portal_webhooks      CASCADE;
DROP TABLE IF EXISTS senderzz_portal_2fa           CASCADE;
DROP TABLE IF EXISTS senderzz_portal_sessions      CASCADE;
DROP TABLE IF EXISTS senderzz_portal_users         CASCADE;

-- Aliases antigos (wp_ prefix) — removidos em migração futura.
DROP VIEW IF EXISTS wp_senderzz_portal_users    CASCADE;
DROP VIEW IF EXISTS wp_senderzz_portal_sessions CASCADE;

-- =============================================================================
-- senderzz_portal_users — Usuários do Portal V2
-- No MySQL: wp_senderzz_portal_users
-- =============================================================================
CREATE TABLE senderzz_portal_users (
    id              BIGINT          NOT NULL GENERATED ALWAYS AS IDENTITY,
    wp_user_id      BIGINT          UNIQUE,
    -- wp_user_id: ID do WP_User correspondente (NULL se usuário criado nativamente no Go).
    email           VARCHAR(255)    NOT NULL,
    nome            VARCHAR(255)    NOT NULL DEFAULT '',
    role            VARCHAR(30)     NOT NULL DEFAULT 'produtor'
                        CHECK (role IN ('produtor', 'afiliado', 'operator')),
    ativo           BOOLEAN         NOT NULL DEFAULT TRUE,
    -- ativo: FALSE = conta suspensa. Equivale a status='inactive' no PHP.
    status          VARCHAR(20)     NOT NULL GENERATED ALWAYS AS (
                        CASE WHEN ativo THEN 'active' ELSE 'inactive' END
                    ) STORED,
    -- status: coluna gerada para retrocompatibilidade com queries do serviço motoboy
    --         (WHERE u.status = 'active'). Não escrever diretamente — alterar `ativo`.
    plano           VARCHAR(30)     NOT NULL DEFAULT 'free',
    -- plano: free | basic | pro | premium (gerenciado externamente / WP meta).
    password_hash   VARCHAR(255)    NULL,
    -- password_hash: bcrypt (custo ≥ 12). NULL = auth via WP apenas (sem login Go nativo).
    twofa_enabled   BOOLEAN         NOT NULL DEFAULT FALSE,
    -- twofa_enabled: ativado pelo usuário via POST /portal/settings/2fa.
    shipping_class_id BIGINT        NULL,
    -- shipping_class_id: ID da classe de frete WC preferida do usuário (lida pelo serviço motoboy).
    -- Coluna mantida aqui para retrocompatibilidade com session.go (u.shipping_class_id).
    name            VARCHAR(255)    NOT NULL GENERATED ALWAYS AS (nome) STORED,
    -- name: alias gerado de `nome` para compatibilidade com session.go (u.name).
    settings        JSONB           NOT NULL DEFAULT '{}',
    -- settings: configurações do usuário (PIX key, notification prefs, etc.)
    --   Schema esperado: {pix_key, pix_key_tipo, notify_email, notify_whatsapp}
    integrations    JSONB           NOT NULL DEFAULT '{}',
    -- integrations: flags de integração (espelha Portal_Page::integrations_toggle)
    --   Schema esperado: {active, paused, require_paid, ignore_duplicates, auto_cheapest}
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id),
    UNIQUE (email)
);

COMMENT ON TABLE senderzz_portal_users IS 'Usuários do Portal V2 Senderzz (produtores, afiliados, OLs)';
COMMENT ON COLUMN senderzz_portal_users.ativo IS 'FALSE = conta suspensa. Alterar ativo, não status (gerada)';
COMMENT ON COLUMN senderzz_portal_users.password_hash IS 'bcrypt custo>=12. NULL = auth via WP apenas';
COMMENT ON COLUMN senderzz_portal_users.twofa_enabled IS 'TRUE = 2FA por e-mail obrigatório no login';
COMMENT ON COLUMN senderzz_portal_users.settings IS 'Configurações JSON: {pix_key, pix_key_tipo, notify_email, notify_whatsapp}';
COMMENT ON COLUMN senderzz_portal_users.integrations IS 'Flags de integração: {active, paused, require_paid, ignore_duplicates, auto_cheapest}';

CREATE INDEX idx_portal_users_wp_user_id ON senderzz_portal_users (wp_user_id);
CREATE INDEX idx_portal_users_role       ON senderzz_portal_users (role);
CREATE INDEX idx_portal_users_ativo      ON senderzz_portal_users (ativo);

-- =============================================================================
-- senderzz_portal_sessions — Sessões de autenticação do Portal
-- No MySQL: wp_senderzz_portal_sessions
-- =============================================================================
CREATE TABLE senderzz_portal_sessions (
    id          BIGINT          NOT NULL GENERATED ALWAYS AS IDENTITY,
    user_id     BIGINT          NOT NULL,
    token       CHAR(64)        NOT NULL,
    -- token: token raw (hex aleatório 32 bytes). Armazenado durante período de migração.
    --        Em produção pura Go, armazenar apenas o HMAC (V-SEC-01).
    token_hmac  CHAR(64)        NOT NULL,
    -- token_hmac: HMAC-SHA256(token, WP_SALT_AUTH). Lookup primário após migração.
    ip          VARCHAR(64)     NULL,
    user_agent  TEXT            NULL,
    expires_at  TIMESTAMPTZ     NOT NULL,
    created_at  TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id),
    UNIQUE (token),
    UNIQUE (token_hmac),
    CONSTRAINT fk_sessions_user FOREIGN KEY (user_id) REFERENCES senderzz_portal_users (id) ON DELETE CASCADE
);

COMMENT ON TABLE senderzz_portal_sessions IS 'Sessões ativas do Portal V2. Token raw + HMAC para compatibilidade de migração';
COMMENT ON COLUMN senderzz_portal_sessions.token IS 'Token raw hex 64 chars (32 bytes). Lookup legado';
COMMENT ON COLUMN senderzz_portal_sessions.token_hmac IS 'HMAC-SHA256(token, WP_SALT_AUTH). Lookup primário';

CREATE INDEX idx_sessions_user_id   ON senderzz_portal_sessions (user_id);
CREATE INDEX idx_sessions_expires   ON senderzz_portal_sessions (expires_at);
-- Índice composto para lookup rápido por token OU hmac.
CREATE INDEX idx_sessions_token_any ON senderzz_portal_sessions (token, token_hmac);

-- =============================================================================
-- senderzz_portal_2fa — Códigos de 2FA por e-mail
-- No MySQL: wp_senderzz_portal_2fa
-- =============================================================================
CREATE TABLE senderzz_portal_2fa (
    id          BIGINT          NOT NULL GENERATED ALWAYS AS IDENTITY,
    user_id     BIGINT          NOT NULL,
    code        CHAR(6)         NOT NULL,
    -- code: 6 dígitos numéricos gerados com crypto/rand.
    expires_at  TIMESTAMPTZ     NOT NULL,
    -- expires_at: TTL de 10 minutos a partir da emissão.
    tentativas  SMALLINT        NOT NULL DEFAULT 0,
    -- tentativas: máximo 5 antes de invalidar o código (fail-closed).
    PRIMARY KEY (id),
    UNIQUE (user_id),
    -- UNIQUE(user_id): apenas um código ativo por vez. Upsert substitui o anterior.
    CONSTRAINT fk_2fa_user FOREIGN KEY (user_id) REFERENCES senderzz_portal_users (id) ON DELETE CASCADE
);

COMMENT ON TABLE senderzz_portal_2fa IS '2FA por e-mail: um código ativo por usuário, TTL 10 min, máx 5 tentativas';
COMMENT ON COLUMN senderzz_portal_2fa.tentativas IS 'Contagem de tentativas erradas. >= 5 → código invalidado (fail-closed)';

CREATE INDEX idx_2fa_expires ON senderzz_portal_2fa (expires_at);

-- =============================================================================
-- senderzz_portal_webhooks — Webhooks configurados pelo usuário
-- No MySQL: wp_senderzz_portal_webhooks
--
-- Soft-delete: url='', active=false (ver Portal_Page.php::ajax_webhooks_delete).
-- Hard-delete faz senderzz_pw_ensure_user_webhook_slots() recriar o slot.
-- Query de listagem: WHERE user_id = $1 AND url != ''.
-- =============================================================================
CREATE TABLE senderzz_portal_webhooks (
    id                  BIGINT          NOT NULL GENERATED ALWAYS AS IDENTITY,
    user_id             BIGINT          NOT NULL,
    url                 TEXT            NOT NULL DEFAULT '',
    secret              VARCHAR(64)     NULL,
    active              BOOLEAN         NOT NULL DEFAULT TRUE,
    event_types         JSONB           NOT NULL DEFAULT '[]',
    -- event_types: whitelist de eventos permitidos — DT-CODE-02.
    --   Valores válidos: order_status_enviado, entregue, cancelado, frustrado, em_rota, embalado.
    --   Qualquer outro valor é rejeitado no handler POST /portal/webhooks.
    shipping_class_id   INT             NULL,
    created_at          TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id),
    CONSTRAINT fk_webhooks_user FOREIGN KEY (user_id) REFERENCES senderzz_portal_users (id) ON DELETE CASCADE
);

COMMENT ON TABLE senderzz_portal_webhooks IS 'Webhooks configurados pelo usuário. Soft-delete: url=empty, active=false';
COMMENT ON COLUMN senderzz_portal_webhooks.event_types IS 'JSON array de eventos — DT-CODE-02 whitelist: order_status_enviado, entregue, cancelado, frustrado, em_rota, embalado';

CREATE INDEX idx_webhooks_user_id    ON senderzz_portal_webhooks (user_id);
CREATE INDEX idx_webhooks_active_url ON senderzz_portal_webhooks (user_id, active) WHERE url != '';

-- =============================================================================
-- senderzz_webhook_log — Histórico de disparos de webhook
-- =============================================================================
CREATE TABLE senderzz_webhook_log (
    id              BIGINT          NOT NULL GENERATED ALWAYS AS IDENTITY,
    webhook_id      BIGINT          NOT NULL,
    event_type      VARCHAR(50)     NOT NULL,
    payload         JSONB           NOT NULL DEFAULT '{}',
    response_code   INT             NULL,
    response_body   TEXT            NULL,
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id),
    CONSTRAINT fk_webhook_log_webhook FOREIGN KEY (webhook_id) REFERENCES senderzz_portal_webhooks (id) ON DELETE CASCADE
);

COMMENT ON TABLE senderzz_webhook_log IS 'Log de disparos de webhook (tentativas + resultados)';

CREATE INDEX idx_webhook_log_webhook_id ON senderzz_webhook_log (webhook_id);
CREATE INDEX idx_webhook_log_created    ON senderzz_webhook_log (created_at);

-- =============================================================================
-- senderzz_integration_log — Log de eventos de integração por usuário
-- =============================================================================
CREATE TABLE senderzz_integration_log (
    id          BIGINT          NOT NULL GENERATED ALWAYS AS IDENTITY,
    user_id     BIGINT          NOT NULL,
    event       VARCHAR(100)    NOT NULL,
    payload     JSONB           NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id),
    CONSTRAINT fk_int_log_user FOREIGN KEY (user_id) REFERENCES senderzz_portal_users (id) ON DELETE CASCADE
);

COMMENT ON TABLE senderzz_integration_log IS 'Log de eventos de integração por usuário do portal';

CREATE INDEX idx_int_log_user_id ON senderzz_integration_log (user_id);
CREATE INDEX idx_int_log_created ON senderzz_integration_log (created_at);

-- =============================================================================
-- Views de retrocompatibilidade para serviços motoboy e wallet (prefixo wp_)
-- Essas views permitem que os serviços existentes continuem funcionando sem
-- alteração enquanto o Portal Go ainda não é o ponto de escrita primário.
-- Remover após migração completa do PHP → Go.
-- =============================================================================
CREATE VIEW wp_senderzz_portal_users AS
    SELECT
        id,
        wp_user_id,
        email,
        nome,
        nome AS name,              -- alias para session.go (u.name)
        role,
        ativo,
        status,                    -- gerada: 'active'/'inactive'
        plano,
        shipping_class_id,
        twofa_enabled,
        settings,
        integrations,
        created_at
    FROM senderzz_portal_users;

COMMENT ON VIEW wp_senderzz_portal_users IS 'View de retrocompatibilidade — motoboy/wallet consultam wp_senderzz_portal_*';

CREATE VIEW wp_senderzz_portal_sessions AS
    SELECT
        id,
        user_id,
        token,
        token_hmac,
        ip,
        user_agent,
        expires_at,
        created_at
    FROM senderzz_portal_sessions;

COMMENT ON VIEW wp_senderzz_portal_sessions IS 'View de retrocompatibilidade — motoboy/wallet consultam wp_senderzz_portal_*';
