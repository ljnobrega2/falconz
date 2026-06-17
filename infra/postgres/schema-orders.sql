-- =============================================================================
-- Senderzz — Schema Postgres 16 para o módulo Orders (Fase 6 Strangler Fig)
-- Substitui wp_wc_orders + wp_wc_orders_meta + wp_posts (para pedidos) do WooCommerce.
--
-- Convenções MySQL → Postgres (mesmas da schema-motoboy.sql):
--   BIGINT UNSIGNED          → BIGINT
--   AUTO_INCREMENT           → GENERATED ALWAYS AS IDENTITY
--   ENUM(...)                → VARCHAR(N) CHECK (col IN (...))
--   DATETIME                 → TIMESTAMPTZ (pedidos Go são nativos — sem legado wall-clock)
--   DECIMAL(10,2)            → DECIMAL(10,2) (mantido para compatibilidade monetária)
--   ON UPDATE CURRENT_TIMESTAMP → trigger set_updated_at (ver abaixo)
--
-- Nota: wp_order_id NULLABLE em sz_orders — NULL = pedido nativo Go (sem origem WC).
--       Durante a janela de migração, wp_order_id identifica pedidos migrados de WC.
-- =============================================================================

-- DROP em ordem reversa de FK para re-execução em dev.
DROP TABLE IF EXISTS sz_order_payments        CASCADE;
DROP TABLE IF EXISTS sz_order_status_history  CASCADE;
DROP TABLE IF EXISTS sz_order_addresses       CASCADE;
DROP TABLE IF EXISTS sz_order_meta            CASCADE;
DROP TABLE IF EXISTS sz_order_items           CASCADE;
DROP TABLE IF EXISTS sz_orders                CASCADE;

-- =============================================================================
-- Função e trigger para updated_at automático
-- =============================================================================
CREATE OR REPLACE FUNCTION sz_set_updated_at()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$;

-- =============================================================================
-- sz_orders — Tabela central de pedidos
-- Substitui: wp_wc_orders (HPOS) + metadados básicos de wp_posts (orders)
-- =============================================================================
CREATE TABLE sz_orders (
    id              BIGINT          NOT NULL GENERATED ALWAYS AS IDENTITY,
    -- order_number: número de exibição ao cliente (ex: "SZ-0001234").
    order_number    VARCHAR(20)     NOT NULL,
    -- wp_order_id: ID original do WooCommerce. NULL para pedidos nativos Go.
    wp_order_id     BIGINT          NULL,
    user_id         BIGINT          NOT NULL,
    -- produtor_id: wp_user_id do lojista/produtor dono do pedido.
    produtor_id     BIGINT          NOT NULL,
    affiliate_id    BIGINT          NULL,

    status          VARCHAR(30)     NOT NULL DEFAULT 'pending'
                        CHECK (status IN (
                            'pending','processing','aguardando','on-hold',
                            'em_separacao','embalado','enviado','entregue',
                            'completo','cancelled','frustrado','reembolsado'
                        )),

    subtotal        DECIMAL(10,2)   NOT NULL DEFAULT 0.00,
    shipping        DECIMAL(10,2)   NOT NULL DEFAULT 0.00,
    total           DECIMAL(10,2)   NOT NULL DEFAULT 0.00,

    payment_method  VARCHAR(50)     NULL,
    payment_status  VARCHAR(20)     NOT NULL DEFAULT 'pending'
                        CHECK (payment_status IN ('pending','paid','failed','refunded')),

    currency        CHAR(3)         NOT NULL DEFAULT 'BRL',
    customer_note   TEXT            NULL,
    ip_address      VARCHAR(64)     NULL,
    user_agent      TEXT            NULL,

    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),

    PRIMARY KEY (id),
    UNIQUE (order_number),
    -- wp_order_id UNIQUE mas NULL é permitido (múltiplos NULLs OK em Postgres UNIQUE).
    UNIQUE (wp_order_id)
);

COMMENT ON TABLE sz_orders IS 'Pedidos nativos Go — substitui wp_wc_orders (HPOS). Fase 6.';
COMMENT ON COLUMN sz_orders.wp_order_id IS 'ID original do WooCommerce. NULL = pedido nativo Go pós-migração.';
COMMENT ON COLUMN sz_orders.order_number IS 'Número de exibição ao cliente (ex: SZ-0001234). Gerado na criação.';
COMMENT ON COLUMN sz_orders.produtor_id IS 'WP user_id do lojista dono do pedido (equivalente a _customer_user em WC).';

CREATE INDEX idx_orders_user_id     ON sz_orders (user_id);
CREATE INDEX idx_orders_produtor    ON sz_orders (produtor_id);
CREATE INDEX idx_orders_affiliate   ON sz_orders (affiliate_id) WHERE affiliate_id IS NOT NULL;
CREATE INDEX idx_orders_status      ON sz_orders (status);
CREATE INDEX idx_orders_payment_st  ON sz_orders (payment_status);
CREATE INDEX idx_orders_created     ON sz_orders (created_at);
CREATE INDEX idx_orders_wp_order    ON sz_orders (wp_order_id) WHERE wp_order_id IS NOT NULL;

-- Trigger updated_at em sz_orders.
CREATE TRIGGER trg_orders_updated_at
    BEFORE UPDATE ON sz_orders
    FOR EACH ROW EXECUTE FUNCTION sz_set_updated_at();

-- =============================================================================
-- sz_order_items — Itens do pedido
-- Substitui: wp_woocommerce_order_items + wp_woocommerce_order_itemmeta
-- =============================================================================
CREATE TABLE sz_order_items (
    id              BIGINT          NOT NULL GENERATED ALWAYS AS IDENTITY,
    order_id        BIGINT          NOT NULL,
    produto_id      BIGINT          NOT NULL,
    nome            VARCHAR(255)    NOT NULL,
    sku             VARCHAR(100)    NULL,
    quantidade      INT             NOT NULL DEFAULT 1 CHECK (quantidade > 0),
    preco_unit      DECIMAL(10,2)   NOT NULL DEFAULT 0.00,
    subtotal        DECIMAL(10,2)   NOT NULL DEFAULT 0.00,
    -- meta: dados extras do item em JSON (variações de produto, atributos, etc.).
    meta            JSONB           NULL,

    PRIMARY KEY (id),
    CONSTRAINT fk_order_items_order
        FOREIGN KEY (order_id) REFERENCES sz_orders (id) ON DELETE CASCADE
);

COMMENT ON TABLE sz_order_items IS 'Itens de pedido — substitui wp_woocommerce_order_items + itemmeta.';
COMMENT ON COLUMN sz_order_items.meta IS 'Dados extras do item (variações, atributos) como JSONB.';

CREATE INDEX idx_order_items_order   ON sz_order_items (order_id);
CREATE INDEX idx_order_items_produto ON sz_order_items (produto_id);

-- =============================================================================
-- sz_order_meta — Meta genérica do pedido (key/value)
-- Substitui: wp_wc_orders_meta (HPOS) e postmeta filtrada por orders
-- =============================================================================
CREATE TABLE sz_order_meta (
    id          BIGINT          NOT NULL GENERATED ALWAYS AS IDENTITY,
    order_id    BIGINT          NOT NULL,
    meta_key    VARCHAR(255)    NOT NULL,
    meta_value  TEXT            NULL,

    PRIMARY KEY (id),
    -- Garante unicidade de (order_id, meta_key) para comportar ON CONFLICT DO UPDATE.
    UNIQUE (order_id, meta_key),
    CONSTRAINT fk_order_meta_order
        FOREIGN KEY (order_id) REFERENCES sz_orders (id) ON DELETE CASCADE
);

COMMENT ON TABLE sz_order_meta IS 'Meta genérica de pedido — substitui wp_wc_orders_meta e wp_postmeta (orders).';

CREATE INDEX idx_order_meta_key ON sz_order_meta (meta_key);

-- =============================================================================
-- sz_order_addresses — Endereço de envio/cobrança
-- Substitui: campos address_* em wp_wc_orders (HPOS) + postmeta de endereço
-- Uma linha por pedido (tipo = 'shipping' ou 'billing').
-- =============================================================================
CREATE TABLE sz_order_addresses (
    id          BIGINT          NOT NULL GENERATED ALWAYS AS IDENTITY,
    order_id    BIGINT          NOT NULL,
    tipo        VARCHAR(10)     NOT NULL DEFAULT 'shipping'
                    CHECK (tipo IN ('shipping','billing')),

    nome        VARCHAR(255)    NULL,
    email       VARCHAR(255)    NULL,
    telefone    VARCHAR(20)     NULL,
    cep         VARCHAR(10)     NULL,
    logradouro  VARCHAR(255)    NULL,
    numero      VARCHAR(20)     NULL,
    complemento VARCHAR(100)    NULL,
    bairro      VARCHAR(100)    NULL,
    cidade      VARCHAR(100)    NULL,
    uf          CHAR(2)         NULL,
    pais        CHAR(2)         NOT NULL DEFAULT 'BR',

    PRIMARY KEY (id),
    -- Pedido tem um único endereço de cada tipo.
    UNIQUE (order_id, tipo),
    CONSTRAINT fk_order_addr_order
        FOREIGN KEY (order_id) REFERENCES sz_orders (id) ON DELETE CASCADE
);

COMMENT ON TABLE sz_order_addresses IS 'Endereços de envio e cobrança por pedido.';

CREATE INDEX idx_order_addr_order ON sz_order_addresses (order_id);

-- =============================================================================
-- sz_order_status_history — Histórico de transições de status
-- Substitui: comentários de nota do WooCommerce + wc_order_status_history
-- =============================================================================
CREATE TABLE sz_order_status_history (
    id          BIGINT          NOT NULL GENERATED ALWAYS AS IDENTITY,
    order_id    BIGINT          NOT NULL,
    status_de   VARCHAR(30)     NULL,
    status_para VARCHAR(30)     NOT NULL,
    motivo      TEXT            NULL,
    actor_id    BIGINT          NULL,
    -- actor_tipo: quem fez a transição (sistema, portal, webhook, admin, cliente).
    actor_tipo  VARCHAR(20)     NOT NULL DEFAULT 'sistema'
                    CHECK (actor_tipo IN ('sistema','portal','webhook','admin','cliente','job')),
    created_at  TIMESTAMPTZ     NOT NULL DEFAULT NOW(),

    PRIMARY KEY (id),
    CONSTRAINT fk_order_history_order
        FOREIGN KEY (order_id) REFERENCES sz_orders (id) ON DELETE CASCADE
);

COMMENT ON TABLE sz_order_status_history IS 'Histórico imutável de transições de status de pedido.';

CREATE INDEX idx_order_hist_order   ON sz_order_status_history (order_id);
CREATE INDEX idx_order_hist_created ON sz_order_status_history (created_at);

-- =============================================================================
-- sz_order_payments — Pagamentos associados ao pedido
-- Substitui: postmeta de pagamento do WC (_transaction_id, _paid_date, etc.)
-- =============================================================================
CREATE TABLE sz_order_payments (
    id          BIGINT          NOT NULL GENERATED ALWAYS AS IDENTITY,
    order_id    BIGINT          NOT NULL,
    -- gateway: cod | pix | cartao | wallet | melhorenvio
    gateway     VARCHAR(50)     NOT NULL,
    -- gateway_ref: ID externo da transação no gateway (ex: ID do PIX no ME).
    gateway_ref VARCHAR(100)    NULL,
    valor       DECIMAL(10,2)   NOT NULL DEFAULT 0.00,
    status      VARCHAR(20)     NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending','paid','failed','refunded')),
    paid_at     TIMESTAMPTZ     NULL,
    created_at  TIMESTAMPTZ     NOT NULL DEFAULT NOW(),

    PRIMARY KEY (id),
    CONSTRAINT fk_order_payments_order
        FOREIGN KEY (order_id) REFERENCES sz_orders (id) ON DELETE CASCADE
);

COMMENT ON TABLE sz_order_payments IS 'Pagamentos por pedido. Suporta múltiplos pagamentos parciais.';
COMMENT ON COLUMN sz_order_payments.gateway_ref IS 'Referência externa no gateway (PIX ID, transaction_id do cartão, etc.).';

CREATE INDEX idx_order_pay_order   ON sz_order_payments (order_id);
CREATE INDEX idx_order_pay_gw_ref  ON sz_order_payments (gateway_ref) WHERE gateway_ref IS NOT NULL;
CREATE INDEX idx_order_pay_status  ON sz_order_payments (status);

-- =============================================================================
-- Recalibra sequências (seguro executar em qualquer estado do banco)
-- =============================================================================
SELECT setval(pg_get_serial_sequence('sz_orders',               'id'), COALESCE((SELECT MAX(id) FROM sz_orders),               1));
SELECT setval(pg_get_serial_sequence('sz_order_items',          'id'), COALESCE((SELECT MAX(id) FROM sz_order_items),           1));
SELECT setval(pg_get_serial_sequence('sz_order_meta',           'id'), COALESCE((SELECT MAX(id) FROM sz_order_meta),            1));
SELECT setval(pg_get_serial_sequence('sz_order_addresses',      'id'), COALESCE((SELECT MAX(id) FROM sz_order_addresses),       1));
SELECT setval(pg_get_serial_sequence('sz_order_status_history', 'id'), COALESCE((SELECT MAX(id) FROM sz_order_status_history),  1));
SELECT setval(pg_get_serial_sequence('sz_order_payments',       'id'), COALESCE((SELECT MAX(id) FROM sz_order_payments),        1));
