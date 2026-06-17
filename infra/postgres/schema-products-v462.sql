-- =============================================================================
-- Senderzz v462 — Tabela de produtos (sz_products)
-- Mirrors wp_posts (type=product) para o admin Go (Postgres-native).
-- Rodar manualmente no Postgres do VPS antes de subir a v462.
-- =============================================================================

CREATE TABLE IF NOT EXISTS sz_products (
    id              BIGINT          NOT NULL GENERATED ALWAYS AS IDENTITY,
    wp_post_id      BIGINT          NULL,              -- ID original do WP (NULL = nativo Go)
    produtor_id     BIGINT          NOT NULL,
    nome            VARCHAR(255)    NOT NULL,
    sku             VARCHAR(100)    NULL,
    preco           DECIMAL(10,2)   NOT NULL DEFAULT 0.00,
    descricao       TEXT            NULL,
    categoria       VARCHAR(100)    NULL,
    status          VARCHAR(20)     NOT NULL DEFAULT 'active'
                        CHECK (status IN ('active','inactive','draft','archived')),
    meta            JSONB           NULL,
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id)
);

-- wp_post_id é nullable; UNIQUE parcial exclui NULLs duplicados.
CREATE UNIQUE INDEX IF NOT EXISTS uq_products_wp_post_id
    ON sz_products (wp_post_id)
    WHERE wp_post_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_products_produtor ON sz_products (produtor_id);
CREATE INDEX IF NOT EXISTS idx_products_status   ON sz_products (status);
CREATE INDEX IF NOT EXISTS idx_products_nome     ON sz_products (nome);
