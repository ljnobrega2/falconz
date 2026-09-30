-- =============================================================================
-- 240-fixes-v472-lgpd-sku.sql
-- Suporte de schema para 2 frentes (sessão 2026-06-18):
--   (A) LGPD: registro de consentimento versionado + trilha de acesso a PII.
--   (B) Bip/SKU: auditoria de leitura de código de barras na operação (em rota,
--       devolução de frustrado, embalagem expedição, alimentação de estoque OL).
--
-- A LÓGICA de validação (SKU lido vs SKU do pedido → trava se diferente) NÃO
-- vive aqui: os handlers Go comparam o SKU bipado contra sz_order_items.sku do
-- pedido. Estas tabelas são REGISTRO/observabilidade (accountability), não a
-- regra. Idempotentes (CREATE IF NOT EXISTS); seguras de re-rodar.
-- =============================================================================

-- ── (A) LGPD — consentimento versionado ───────────────────────────────────────
-- Aceite de política/termo com versão + timestamp + origem. UNIQUE garante 1
-- aceite por (titular, documento, versão) — re-aceitar a mesma versão é no-op.
CREATE TABLE IF NOT EXISTS senderzz_consents (
    id          BIGINT       NOT NULL GENERATED ALWAYS AS IDENTITY,
    user_id     BIGINT       NOT NULL,                 -- senderzz_portal_users.id
    doc_type    VARCHAR(40)  NOT NULL,                 -- privacy | terms | cookies | marketing
    doc_version VARCHAR(40)  NOT NULL,                 -- ex.: '2026-06-18' ou 'v1.2'
    accepted_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    ip          VARCHAR(64)  NULL,
    user_agent  TEXT         NULL,
    PRIMARY KEY (id)
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_consents_user_doc_ver
    ON senderzz_consents (user_id, doc_type, doc_version);
CREATE INDEX IF NOT EXISTS idx_consents_user ON senderzz_consents (user_id);

-- ── (A) LGPD — trilha de acesso a dados pessoais (accountability Art. 37) ──────
-- Quem (admin/operador) acessou PII de qual titular, quando e o quê. Append-only.
CREATE TABLE IF NOT EXISTS senderzz_pii_access_log (
    id            BIGINT       NOT NULL GENERATED ALWAYS AS IDENTITY,
    actor_user_id BIGINT       NULL,                   -- quem acessou (admin id)
    actor_email   VARCHAR(255) NULL,
    subject_type  VARCHAR(40)  NOT NULL,               -- customer | producer | affiliate | order
    subject_id    BIGINT       NULL,
    fields        TEXT         NULL,                    -- campos PII tocados (csv)
    action        VARCHAR(40)  NOT NULL DEFAULT 'view', -- view | export | edit
    ip            VARCHAR(64)  NULL,
    accessed_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS idx_pii_access_actor   ON senderzz_pii_access_log (actor_user_id);
CREATE INDEX IF NOT EXISTS idx_pii_access_subject ON senderzz_pii_access_log (subject_type, subject_id);
CREATE INDEX IF NOT EXISTS idx_pii_access_at      ON senderzz_pii_access_log (accessed_at DESC);

-- ── (B) Bip/SKU — auditoria de leitura de código de barras ─────────────────────
-- Cada bipada (ou digitação manual) na operação, com o veredito de validação
-- (SKU lido casou com o SKU esperado do pedido?). context separa os fluxos:
--   em_rota     — motoboy bipa p/ colocar pedido em rota (modalidade COD/PWA)
--   devolucao   — motoboy bipa p/ devolver após frustrado
--   expedicao   — embalagem na modalidade Expedição (operada pelo OL)
--   estoque     — OL bipa 1 unidade p/ alimentar estoque (qtd informada)
CREATE TABLE IF NOT EXISTS sz_pack_scans (
    id           BIGINT       NOT NULL GENERATED ALWAYS AS IDENTITY,
    context      VARCHAR(20)  NOT NULL,                -- em_rota | devolucao | expedicao | estoque
    wc_order_id  BIGINT       NULL,                    -- pedido (quando aplicável)
    product_id   BIGINT       NULL,                    -- sz_products.id (quando aplicável)
    sku_scanned  VARCHAR(120) NOT NULL,
    sku_expected VARCHAR(120) NULL,                    -- SKU esperado do pedido (p/ auditoria)
    matched      BOOLEAN      NOT NULL,                -- true = passou; false = bloqueou
    quantity     INTEGER      NOT NULL DEFAULT 1,      -- estoque: qtd informada (bipa 1, digita qtd)
    manual_typed BOOLEAN      NOT NULL DEFAULT FALSE,  -- true = digitou o código (fallback)
    actor        VARCHAR(120) NULL,                    -- motoboy/ol/admin identificador
    scanned_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS idx_pack_scans_order   ON sz_pack_scans (wc_order_id);
CREATE INDEX IF NOT EXISTS idx_pack_scans_context ON sz_pack_scans (context);
CREATE INDEX IF NOT EXISTS idx_pack_scans_matched ON sz_pack_scans (matched);
CREATE INDEX IF NOT EXISTS idx_pack_scans_at      ON sz_pack_scans (scanned_at DESC);
