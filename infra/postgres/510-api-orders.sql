-- 510-api-orders.sql — infraestrutura de pedidos via API (menu Integrações do
-- portal, feature antes 501 "pipeline de recebimento não migrado").
--
-- senderzz_producer_api_keys: 1 chave ativa por produtor. Guarda HASH (sha256
-- hex), nunca o token em claro — mesmo padrão de outros secrets do sistema.
-- token_prefix (8 chars) é só pra exibição na UI (produtor reconhece qual
-- chave é sem expor o valor completo).
CREATE TABLE IF NOT EXISTS senderzz_producer_api_keys (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    producer_id   bigint NOT NULL UNIQUE,
    token_hash    char(64) NOT NULL UNIQUE,
    token_prefix  varchar(12) NOT NULL,
    active        boolean NOT NULL DEFAULT true,
    created_at    timestamptz NOT NULL DEFAULT now(),
    rotated_at    timestamptz,
    last_used_at  timestamptz
);
CREATE INDEX IF NOT EXISTS idx_producer_api_keys_hash ON senderzz_producer_api_keys(token_hash) WHERE active;

-- external_order_id: idempotência (mesma regra da task pro produtor autenticado
-- por API key — plataforma dele vai retentar em timeout, at-least-once garantido).
-- UNIQUE por produtor (não globalmente — 2 produtores podem ter external_order_id
-- iguais nas próprias plataformas sem colisão).
ALTER TABLE sz_orders ADD COLUMN IF NOT EXISTS external_order_id text;
CREATE UNIQUE INDEX IF NOT EXISTS idx_orders_external_id
    ON sz_orders(produtor_id, external_order_id)
    WHERE external_order_id IS NOT NULL;
