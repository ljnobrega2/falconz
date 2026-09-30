-- =============================================================================
-- 425-webhook-tipo-produto.sql
-- Senderzz — Webhooks de COD (motoboy_*) + roteamento por PRODUTO.
--
-- CONTEXTO / PROBLEMA: senderzz_portal_webhooks só modelava webhooks de
--   EXPEDIÇÃO (eventos order_status_*). Os eventos de COD/motoboy (motoboy_*)
--   eram rejeitados com 400 no go/portal Create (allowedEventTypes), o que
--   forçava o frontend a BLOQUEAR a criação de webhook de COD. Esta migração
--   abre o caminho do banco para:
--     1. CLASSIFICAR o webhook por tipo: 'expedicao' (default, comportamento
--        atual) ou 'cod' (eventos motoboy_*).
--     2. ROTEAR por PRODUTO: um webhook pode opcionalmente fixar product_id;
--        o dispatcher (go/cron) só dispara se o pedido contiver aquele produto.
--        product_id NULL = todos os produtos do usuário (comportamento atual).
--
-- O QUE MUDA: 2 colunas novas em senderzz_portal_webhooks:
--   - tipo        varchar(20) NOT NULL DEFAULT 'expedicao'  (∈ {expedicao,cod})
--   - product_id  bigint      NULL                          (WooCommerce product ID)
--
-- CONVENÇÃO DE TOKEN: minúsculo sem espaço, igual ao resto do schema
--   ('expedicao', 'cod'). O CHECK nomeado espelha o estilo de
--   424-produto-aprovacao.sql (defesa em profundidade — o handler já valida).
--
-- IDEMPOTENTE: ADD COLUMN IF NOT EXISTS backfilla linhas existentes com o
--   default ('expedicao') sem reescrevê-las uma a uma; DROP/ADD da constraint
--   nomeada recria o mesmo conjunto. Re-rodar é no-op. NÃO toca dados reais.
-- =============================================================================

-- 1) Coluna `tipo` — classificação do webhook (expedicao|cod).
--    NOT NULL + DEFAULT 'expedicao' → toda linha pré-existente vira 'expedicao'
--    (comportamento idêntico ao de antes desta migração).
ALTER TABLE senderzz_portal_webhooks
    ADD COLUMN IF NOT EXISTS tipo varchar(20) NOT NULL DEFAULT 'expedicao';

-- 2) Coluna `product_id` — roteamento por produto (NULL = todos os produtos).
ALTER TABLE senderzz_portal_webhooks
    ADD COLUMN IF NOT EXISTS product_id bigint NULL;

-- 3) CHECK nomeado p/ `tipo` (idempotente — drop+add). Defesa em profundidade:
--    o go/portal Create já valida tipo ∈ {expedicao,cod}, mas o CHECK barra
--    qualquer INSERT/UPDATE direto fora do conjunto.
ALTER TABLE senderzz_portal_webhooks
    DROP CONSTRAINT IF EXISTS senderzz_portal_webhooks_tipo_check;

ALTER TABLE senderzz_portal_webhooks
    ADD CONSTRAINT senderzz_portal_webhooks_tipo_check
        CHECK (tipo IN ('expedicao', 'cod'));

-- 4) Índice parcial p/ o roteamento por produto no dispatcher (go/cron):
--    o match de webhooks por produto filtra por product_id quando não-NULL.
CREATE INDEX IF NOT EXISTS idx_webhooks_product
    ON senderzz_portal_webhooks (product_id)
    WHERE product_id IS NOT NULL;

COMMENT ON COLUMN senderzz_portal_webhooks.tipo IS
    'Classificação do webhook: expedicao (order_status_*) | cod (motoboy_*). Default expedicao.';
COMMENT ON COLUMN senderzz_portal_webhooks.product_id IS
    'Roteamento por produto (WooCommerce product ID). NULL = todos os produtos do usuário.';
