-- =============================================================================
-- Senderzz v467 — Tabela de links de checkout (senderzz_checkout_links)
-- Espelha wp_senderzz_checkout_links (ofertas/links de checkout do produtor).
--
-- POR QUE: o migrador (infra/scripts/migrate-wp-to-pg.py) já tem o PAIR
--   ("wp_senderzz_checkout_links","senderzz_checkout_links",{}) mas dava
--   SKIP por a tabela PG não existir. Com este CREATE TABLE o backfill passa
--   a migrar os links de checkout reais do WP.
--
-- IMPORTANTE — fidelidade de nomes: o migrador mapeia coluna por NOME (sem
--   renames neste PAIR), então cada coluna aqui usa exatamente o nome da coluna
--   de origem em wp_senderzz_checkout_links. Colunas da origem sem par aqui são
--   simplesmente ignoradas (payload, components_text, link_motoboy_id,
--   schema_version, shipping_class_id, affiliate_commission_pct, user_id,
--   updated_at) — não são lidas por nenhum handler do admin Go.
--
-- NOTA de leitura (auditoria 2026-06-18): nenhum handler Go lê esta tabela hoje.
--   A tela "Gestão de Links de Checkout" e as colunas "Oferta (link)" de
--   Comissões/Vínculos leem senderzz_affiliate_links.link_token. Esta tabela é
--   o espelho fiel da origem WP (fonte de verdade das ofertas do produtor),
--   disponível para uso futuro.
--
-- Rodar manualmente no Postgres do VPS antes de rodar o migrador.
-- =============================================================================

CREATE TABLE IF NOT EXISTS senderzz_checkout_links (
    id                BIGINT          NOT NULL GENERATED ALWAYS AS IDENTITY,
    producer_id       BIGINT          NOT NULL DEFAULT 0,   -- portal_users.id dono da oferta
    post_id           BIGINT          NOT NULL DEFAULT 0,   -- página de checkout (wfacp_checkout) no WP
    token             VARCHAR(80)     NOT NULL DEFAULT '',  -- token público do link
    tipo              VARCHAR(20)     NOT NULL DEFAULT 'correio',
    url               VARCHAR(512)    NOT NULL DEFAULT '',
    display_value     DECIMAL(12,2)   NOT NULL DEFAULT 0.00,
    price_label       VARCHAR(64)     NOT NULL DEFAULT '',
    affiliate_visible BOOLEAN         NOT NULL DEFAULT FALSE,
    name              VARCHAR(255)    NOT NULL DEFAULT '',
    slug              VARCHAR(255)    NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id)
);

-- token é único na origem WP (UNIQUE KEY token); UNIQUE parcial ignora vazios.
CREATE UNIQUE INDEX IF NOT EXISTS uq_checkout_links_token
    ON senderzz_checkout_links (token)
    WHERE token <> '';

CREATE INDEX IF NOT EXISTS idx_checkout_links_producer ON senderzz_checkout_links (producer_id);
CREATE INDEX IF NOT EXISTS idx_checkout_links_post     ON senderzz_checkout_links (post_id);
CREATE INDEX IF NOT EXISTS idx_checkout_links_visible  ON senderzz_checkout_links (affiliate_visible);
