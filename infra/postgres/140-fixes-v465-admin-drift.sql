-- =============================================================================
-- schema-fixes-v465-admin-drift.sql
-- AUDIT-2026-06-18: colunas que os handlers Go do painel admin (go/admin) esperam
-- mas que faltavam nos schema-*.sql base (drift schema-vs-código). Detectado por
-- smoke-test e2e dos ~85 GET endpoints contra Postgres limpo (112 OK após este fix).
-- Idempotente (ADD COLUMN IF NOT EXISTS) — seguro reaplicar.
-- =============================================================================

-- Login do painel: role do admin (onboarding/create-admin insere 'super_admin')
ALTER TABLE senderzz_admin_users           ADD COLUMN IF NOT EXISTS role            VARCHAR(32)  NOT NULL DEFAULT 'admin';

-- Etiquetas: URL de impressão (labels.go SELECT print_url)
ALTER TABLE wc_me_labels                    ADD COLUMN IF NOT EXISTS print_url       TEXT;

-- Pedidos: taxas do livro COD (cod_livro.go)
ALTER TABLE sz_orders                       ADD COLUMN IF NOT EXISTS delivery_fee    NUMERIC(10,2) NOT NULL DEFAULT 0;
ALTER TABLE sz_orders                       ADD COLUMN IF NOT EXISTS transaction_fee NUMERIC(10,2) NOT NULL DEFAULT 0;

-- Saques COD/afiliado: timestamp de criação (cod_saques.go ORDER BY w.created_at)
ALTER TABLE sz_cod_withdrawals             ADD COLUMN IF NOT EXISTS created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW();
ALTER TABLE senderzz_affiliate_withdrawals ADD COLUMN IF NOT EXISTS created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW();

-- Onboarding: campos lidos pelo painel (onboarding.go)
ALTER TABLE senderzz_onboarding_requests   ADD COLUMN IF NOT EXISTS document        VARCHAR(64);
ALTER TABLE senderzz_onboarding_requests   ADD COLUMN IF NOT EXISTS token           VARCHAR(255);
ALTER TABLE senderzz_onboarding_requests   ADD COLUMN IF NOT EXISTS approved_at     TIMESTAMPTZ;

-- Audit log do portal: o código usa nomes EN (order_id, ip) além dos PT existentes
-- (pedido_id, ip_address). Adiciona os EN para o SELECT do painel não quebrar.
ALTER TABLE senderzz_portal_audit_log      ADD COLUMN IF NOT EXISTS portal_user_id  BIGINT;
ALTER TABLE senderzz_portal_audit_log      ADD COLUMN IF NOT EXISTS order_id        BIGINT;
ALTER TABLE senderzz_portal_audit_log      ADD COLUMN IF NOT EXISTS ip              VARCHAR(64);

-- Webhooks de produtor + logs (expedicao_webhooks.go)
ALTER TABLE senderzz_producer_webhooks     ADD COLUMN IF NOT EXISTS class_id        BIGINT;
ALTER TABLE senderzz_producer_webhooks     ADD COLUMN IF NOT EXISTS last_error      TEXT;
ALTER TABLE senderzz_producer_webhook_logs ADD COLUMN IF NOT EXISTS fired_at        TIMESTAMPTZ DEFAULT NOW();
ALTER TABLE senderzz_producer_webhook_logs ADD COLUMN IF NOT EXISTS error           TEXT;
