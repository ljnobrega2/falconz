-- =============================================================================
-- Senderzz — Patch consolidado v460 (auditoria admin 2026-06-17)
--
-- Cria tabelas faltantes e adiciona colunas faltantes que vários handlers
-- do admin Go referenciam. Idempotente — pode rodar várias vezes.
--
-- Origem: relatório de auditoria admin/agente em 2026-06-17. Erros corrigidos:
--   - relation "senderzz_affiliate_transactions" does not exist
--   - relation "sz_motoboy_ganhos" does not exist
--   - relation "senderzz_options" does not exist
--   - column o.customer_name / o.affiliate_amount / o.billing_email does not exist
--   - column mp.valor does not exist  (handler corrigido para mp.valor_pedido)
--   - column p.valor does not exist   (handler corrigido para p.valor_pedido)
-- =============================================================================

-- =============================================================================
-- sz_orders — colunas adicionais
-- Adicionadas como nullable / default 0 para não quebrar pedidos existentes.
-- Mantém retrocompatibilidade com fluxos legados (WC HPOS).
-- =============================================================================
ALTER TABLE sz_orders ADD COLUMN IF NOT EXISTS customer_name       VARCHAR(255)    NULL;
ALTER TABLE sz_orders ADD COLUMN IF NOT EXISTS billing_email       VARCHAR(255)    NULL;
ALTER TABLE sz_orders ADD COLUMN IF NOT EXISTS affiliate_amount    DECIMAL(10,2)   NOT NULL DEFAULT 0.00;
ALTER TABLE sz_orders ADD COLUMN IF NOT EXISTS senderzz_fee        DECIMAL(10,2)   NOT NULL DEFAULT 0.00;
ALTER TABLE sz_orders ADD COLUMN IF NOT EXISTS producer_net        DECIMAL(10,2)   NOT NULL DEFAULT 0.00;
ALTER TABLE sz_orders ADD COLUMN IF NOT EXISTS shipping_class      VARCHAR(100)    NULL;
ALTER TABLE sz_orders ADD COLUMN IF NOT EXISTS shipping_class_id   BIGINT          NULL;
-- gross = alias para total (handlers legados usam "gross"). Coluna gerada.
ALTER TABLE sz_orders ADD COLUMN IF NOT EXISTS gross               DECIMAL(10,2)   GENERATED ALWAYS AS (total) STORED;

-- =============================================================================
-- senderzz_options — chave/valor genérico (substitui wp_options)
-- Usado por config TPC, COD, motoboy, push, pwa, tracking, integrações.
-- =============================================================================
CREATE TABLE IF NOT EXISTS senderzz_options (
    name        VARCHAR(191)    NOT NULL,
    value       TEXT            NULL,
    autoload    VARCHAR(20)     NOT NULL DEFAULT 'yes',
    updated_at  TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    PRIMARY KEY (name)
);
COMMENT ON TABLE senderzz_options IS 'Key/value store (substitui wp_options do PHP).';

-- =============================================================================
-- senderzz_affiliate_transactions — livro razão de afiliados
-- Estrutura PHP legada. Schema Postgres já tem senderzz_affiliate_commissions
-- mas vários handlers Go esperam transactions com colunas amount/type/status.
-- Criamos a tabela própria para não quebrar — pode ser populada via gatilho
-- ou ETL em paralelo a commissions.
-- =============================================================================
CREATE TABLE IF NOT EXISTS senderzz_affiliate_transactions (
    id              BIGINT          NOT NULL GENERATED ALWAYS AS IDENTITY,
    affiliate_id    BIGINT          NOT NULL,
    order_id        BIGINT          NULL,
    type            VARCHAR(30)     NOT NULL DEFAULT 'commission'
                        CHECK (type IN ('commission','penalty','withdrawal','adjustment','refund')),
    amount          DECIMAL(10,2)   NOT NULL DEFAULT 0.00,
    status          VARCHAR(20)     NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending','approved','paid','reversed','cancelled')),
    description     TEXT            NULL,
    meta            JSONB           NULL,
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS idx_aff_tx_affiliate ON senderzz_affiliate_transactions (affiliate_id);
CREATE INDEX IF NOT EXISTS idx_aff_tx_order     ON senderzz_affiliate_transactions (order_id);
CREATE INDEX IF NOT EXISTS idx_aff_tx_status    ON senderzz_affiliate_transactions (status);
CREATE INDEX IF NOT EXISTS idx_aff_tx_created   ON senderzz_affiliate_transactions (created_at);
COMMENT ON TABLE senderzz_affiliate_transactions IS 'Livro razão genérico de afiliados (comissão/penalidade/saque/ajuste).';

-- =============================================================================
-- senderzz_affiliate_wallet — saldo cacheado por afiliado
-- =============================================================================
CREATE TABLE IF NOT EXISTS senderzz_affiliate_wallet (
    affiliate_id    BIGINT          NOT NULL,
    saldo_disponivel DECIMAL(10,2)  NOT NULL DEFAULT 0.00,
    saldo_pendente   DECIMAL(10,2)  NOT NULL DEFAULT 0.00,
    total_recebido   DECIMAL(10,2)  NOT NULL DEFAULT 0.00,
    total_estornado  DECIMAL(10,2)  NOT NULL DEFAULT 0.00,
    updated_at       TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    PRIMARY KEY (affiliate_id)
);
COMMENT ON TABLE senderzz_affiliate_wallet IS 'Saldo agregado por afiliado (cache de senderzz_affiliate_transactions).';

-- =============================================================================
-- senderzz_affiliate_withdrawals — solicitações de saque de afiliados
-- =============================================================================
CREATE TABLE IF NOT EXISTS senderzz_affiliate_withdrawals (
    id              BIGINT          NOT NULL GENERATED ALWAYS AS IDENTITY,
    affiliate_id    BIGINT          NOT NULL,
    amount          DECIMAL(10,2)   NOT NULL,
    status          VARCHAR(20)     NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending','approved','paid','rejected','cancelled')),
    pix_key         VARCHAR(200)    NULL,
    pix_type        VARCHAR(20)     NULL,
    proof_url       TEXT            NULL,
    notes           TEXT            NULL,
    requested_at    TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    processed_at    TIMESTAMPTZ     NULL,
    processed_by    BIGINT          NULL,
    PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS idx_aff_wd_affiliate ON senderzz_affiliate_withdrawals (affiliate_id);
CREATE INDEX IF NOT EXISTS idx_aff_wd_status    ON senderzz_affiliate_withdrawals (status);

-- =============================================================================
-- senderzz_portal_audit_log — log de eventos do portal/admin
-- =============================================================================
CREATE TABLE IF NOT EXISTS senderzz_portal_audit_log (
    id              BIGINT          NOT NULL GENERATED ALWAYS AS IDENTITY,
    user_id         BIGINT          NULL,
    action          VARCHAR(80)     NOT NULL,
    entity_type     VARCHAR(60)     NULL,
    entity_id       BIGINT          NULL,
    pedido_id       BIGINT          NULL,
    ip_address      VARCHAR(64)     NULL,
    user_agent      TEXT            NULL,
    meta            JSONB           NULL,
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS idx_audit_action     ON senderzz_portal_audit_log (action);
CREATE INDEX IF NOT EXISTS idx_audit_user       ON senderzz_portal_audit_log (user_id);
CREATE INDEX IF NOT EXISTS idx_audit_created    ON senderzz_portal_audit_log (created_at);
CREATE INDEX IF NOT EXISTS idx_audit_pedido     ON senderzz_portal_audit_log (pedido_id) WHERE pedido_id IS NOT NULL;

-- =============================================================================
-- senderzz_portal_user_meta — meta key/value por usuário do portal
-- =============================================================================
CREATE TABLE IF NOT EXISTS senderzz_portal_user_meta (
    id          BIGINT          NOT NULL GENERATED ALWAYS AS IDENTITY,
    user_id     BIGINT          NOT NULL,
    meta_key    VARCHAR(150)    NOT NULL,
    meta_value  TEXT            NULL,
    PRIMARY KEY (id),
    UNIQUE (user_id, meta_key)
);
CREATE INDEX IF NOT EXISTS idx_portal_user_meta_key ON senderzz_portal_user_meta (meta_key);

-- =============================================================================
-- senderzz_onboarding_requests — solicitações de onboarding
-- =============================================================================
CREATE TABLE IF NOT EXISTS senderzz_onboarding_requests (
    id              BIGINT          NOT NULL GENERATED ALWAYS AS IDENTITY,
    nome            VARCHAR(200)    NOT NULL,
    email           VARCHAR(200)    NOT NULL,
    telefone        VARCHAR(30)     NULL,
    empresa         VARCHAR(200)    NULL,
    status          VARCHAR(20)     NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending','approved','rejected','contacted')),
    meta            JSONB           NULL,
    notes           TEXT            NULL,
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    processed_at    TIMESTAMPTZ     NULL,
    processed_by    BIGINT          NULL,
    PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS idx_onb_status ON senderzz_onboarding_requests (status);

-- =============================================================================
-- senderzz_order_notes — notas livres por pedido (paralelo a sz_order_status_history)
-- =============================================================================
CREATE TABLE IF NOT EXISTS senderzz_order_notes (
    id          BIGINT          NOT NULL GENERATED ALWAYS AS IDENTITY,
    order_id    BIGINT          NOT NULL,
    user_id     BIGINT          NULL,
    note_type   VARCHAR(30)     NOT NULL DEFAULT 'internal'
                    CHECK (note_type IN ('internal','customer','system')),
    body        TEXT            NOT NULL,
    created_at  TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS idx_order_notes_order ON senderzz_order_notes (order_id);

-- =============================================================================
-- senderzz_producer_webhooks + logs — webhooks do produtor (Expedição)
-- =============================================================================
CREATE TABLE IF NOT EXISTS senderzz_producer_webhooks (
    id          BIGINT          NOT NULL GENERATED ALWAYS AS IDENTITY,
    producer_id BIGINT          NOT NULL,
    name        VARCHAR(120)    NOT NULL,
    url         TEXT            NOT NULL,
    secret      VARCHAR(120)    NULL,
    events      JSONB           NULL,
    active      BOOLEAN         NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS idx_prod_wh_producer ON senderzz_producer_webhooks (producer_id);
CREATE INDEX IF NOT EXISTS idx_prod_wh_active   ON senderzz_producer_webhooks (active);

CREATE TABLE IF NOT EXISTS senderzz_producer_webhook_logs (
    id              BIGINT          NOT NULL GENERATED ALWAYS AS IDENTITY,
    webhook_id      BIGINT          NULL,
    producer_id     BIGINT          NULL,
    event_type      VARCHAR(80)     NULL,
    payload         JSONB           NULL,
    response_code   INT             NULL,
    response_body   TEXT            NULL,
    success         BOOLEAN         NOT NULL DEFAULT FALSE,
    attempt         INT             NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS idx_prod_wh_logs_webhook ON senderzz_producer_webhook_logs (webhook_id);
CREATE INDEX IF NOT EXISTS idx_prod_wh_logs_created ON senderzz_producer_webhook_logs (created_at);

-- =============================================================================
-- senderzz_shipping_classes — classes de envio
-- =============================================================================
CREATE TABLE IF NOT EXISTS senderzz_shipping_classes (
    id          BIGINT          NOT NULL GENERATED ALWAYS AS IDENTITY,
    slug        VARCHAR(80)     NOT NULL,
    name        VARCHAR(120)    NOT NULL,
    description TEXT            NULL,
    active      BOOLEAN         NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id),
    UNIQUE (slug)
);

-- =============================================================================
-- sz_motoboy_ganhos — livro razão de ganhos por motoboy (Carteira Motoboy)
-- =============================================================================
CREATE TABLE IF NOT EXISTS sz_motoboy_ganhos (
    id              BIGINT          NOT NULL GENERATED ALWAYS AS IDENTITY,
    motoboy_id      BIGINT          NOT NULL,
    pedido_id       BIGINT          NULL,
    valor           DECIMAL(10,2)   NOT NULL DEFAULT 0.00,
    tipo            VARCHAR(30)     NOT NULL DEFAULT 'entrega'
                        CHECK (tipo IN ('entrega','frustrado','bonus','ajuste','estorno')),
    status          VARCHAR(20)     NOT NULL DEFAULT 'pendente'
                        CHECK (status IN ('pendente','disponivel','pago','cancelado')),
    referencia      VARCHAR(120)    NULL,
    description     TEXT            NULL,
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS idx_mb_ganhos_motoboy ON sz_motoboy_ganhos (motoboy_id);
CREATE INDEX IF NOT EXISTS idx_mb_ganhos_pedido  ON sz_motoboy_ganhos (pedido_id);
CREATE INDEX IF NOT EXISTS idx_mb_ganhos_status  ON sz_motoboy_ganhos (status);

-- =============================================================================
-- sz_motoboy_pagamentos — pagamentos efetuados a motoboys
-- =============================================================================
CREATE TABLE IF NOT EXISTS sz_motoboy_pagamentos (
    id              BIGINT          NOT NULL GENERATED ALWAYS AS IDENTITY,
    motoboy_id      BIGINT          NOT NULL,
    valor           DECIMAL(10,2)   NOT NULL,
    metodo          VARCHAR(30)     NOT NULL DEFAULT 'pix',
    pix_key         VARCHAR(200)    NULL,
    proof_url       TEXT            NULL,
    notes           TEXT            NULL,
    status          VARCHAR(20)     NOT NULL DEFAULT 'efetuado'
                        CHECK (status IN ('pendente','efetuado','estornado')),
    paid_at         TIMESTAMPTZ     NULL,
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS idx_mb_pgto_motoboy ON sz_motoboy_pagamentos (motoboy_id);

-- =============================================================================
-- COD: withdrawals + wallet transactions + withdraw accounts (módulo COD)
-- =============================================================================
CREATE TABLE IF NOT EXISTS sz_cod_withdrawals (
    id              BIGINT          NOT NULL GENERATED ALWAYS AS IDENTITY,
    producer_id     BIGINT          NOT NULL,
    amount          DECIMAL(10,2)   NOT NULL,
    status          VARCHAR(20)     NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending','approved','paid','rejected','cancelled')),
    pix_key         VARCHAR(200)    NULL,
    pix_type        VARCHAR(20)     NULL,
    proof_url       TEXT            NULL,
    notes           TEXT            NULL,
    requested_at    TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    processed_at    TIMESTAMPTZ     NULL,
    processed_by    BIGINT          NULL,
    PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS idx_cod_wd_producer ON sz_cod_withdrawals (producer_id);
CREATE INDEX IF NOT EXISTS idx_cod_wd_status   ON sz_cod_withdrawals (status);

CREATE TABLE IF NOT EXISTS sz_cod_wallet_transactions (
    id              BIGINT          NOT NULL GENERATED ALWAYS AS IDENTITY,
    producer_id     BIGINT          NOT NULL,
    order_id        BIGINT          NULL,
    type            VARCHAR(30)     NOT NULL DEFAULT 'cod_received'
                        CHECK (type IN ('cod_received','withdrawal','adjustment','refund','fee')),
    amount          DECIMAL(10,2)   NOT NULL,
    status          VARCHAR(20)     NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending','available','released','reversed')),
    description     TEXT            NULL,
    meta            JSONB           NULL,
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS idx_cod_tx_producer ON sz_cod_wallet_transactions (producer_id);
CREATE INDEX IF NOT EXISTS idx_cod_tx_order    ON sz_cod_wallet_transactions (order_id);
CREATE INDEX IF NOT EXISTS idx_cod_tx_status   ON sz_cod_wallet_transactions (status);
CREATE INDEX IF NOT EXISTS idx_cod_tx_created  ON sz_cod_wallet_transactions (created_at);

CREATE TABLE IF NOT EXISTS sz_cod_withdraw_accounts (
    id              BIGINT          NOT NULL GENERATED ALWAYS AS IDENTITY,
    producer_id     BIGINT          NOT NULL,
    name            VARCHAR(120)    NOT NULL,
    pix_key         VARCHAR(200)    NOT NULL,
    pix_type        VARCHAR(20)     NOT NULL,
    is_default      BOOLEAN         NOT NULL DEFAULT FALSE,
    active          BOOLEAN         NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS idx_cod_acc_producer ON sz_cod_withdraw_accounts (producer_id);

-- =============================================================================
-- sz_wallet_divergences — divergências entre saldo cacheado e razão
-- =============================================================================
CREATE TABLE IF NOT EXISTS sz_wallet_divergences (
    id              BIGINT          NOT NULL GENERATED ALWAYS AS IDENTITY,
    wallet_owner    VARCHAR(60)     NOT NULL,
    owner_id        BIGINT          NOT NULL,
    expected        DECIMAL(10,2)   NOT NULL,
    actual          DECIMAL(10,2)   NOT NULL,
    diff            DECIMAL(10,2)   NOT NULL,
    resolved        BOOLEAN         NOT NULL DEFAULT FALSE,
    notes           TEXT            NULL,
    detected_at     TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    resolved_at     TIMESTAMPTZ     NULL,
    PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS idx_wdiv_owner ON sz_wallet_divergences (wallet_owner, owner_id);

-- =============================================================================
-- sz_push_subscriptions + sz_notif_log — PWA push técnico
-- =============================================================================
CREATE TABLE IF NOT EXISTS sz_push_subscriptions (
    id              BIGINT          NOT NULL GENERATED ALWAYS AS IDENTITY,
    user_id         BIGINT          NULL,
    role            VARCHAR(40)     NULL,
    endpoint        TEXT            NOT NULL,
    p256dh          TEXT            NOT NULL,
    auth            TEXT            NOT NULL,
    ua              TEXT            NULL,
    active          BOOLEAN         NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id),
    UNIQUE (endpoint)
);
CREATE INDEX IF NOT EXISTS idx_push_sub_user ON sz_push_subscriptions (user_id);

CREATE TABLE IF NOT EXISTS sz_notif_log (
    id              BIGINT          NOT NULL GENERATED ALWAYS AS IDENTITY,
    event_key       VARCHAR(80)     NULL,
    recipient_user  BIGINT          NULL,
    title           VARCHAR(200)    NULL,
    body            TEXT            NULL,
    payload         JSONB           NULL,
    success         BOOLEAN         NOT NULL DEFAULT FALSE,
    error_msg       TEXT            NULL,
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS idx_notif_log_created ON sz_notif_log (created_at);
CREATE INDEX IF NOT EXISTS idx_notif_log_user    ON sz_notif_log (recipient_user);

-- =============================================================================
-- FIM v460
-- =============================================================================
