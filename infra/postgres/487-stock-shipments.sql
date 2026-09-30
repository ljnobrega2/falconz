-- =============================================================================
-- 487-stock-shipments.sql — CICLO DE REMESSA PRODUTOR → CD (FEAT-STOCK-SHIPMENTS)
--
-- Paridade com includes/senderzz-stock-shipments.php: legado tinha ciclo
-- pending→sent→confirm→deliver→conclude. FALK (go/admin/stock.go,
-- go/portal/stock_producer_portal.go) só tinha crédito direto/imediato em
-- sz_stock — sem rastro de "produtor despachou X, CD ainda não recebeu".
--
-- Ciclo simplificado (backend-only, sem UI nesta rodada):
--   pendente  → produtor cria a remessa (itens + quantidades) — NÃO credita estoque.
--   enviado   → produtor marca como despachada — NÃO credita estoque.
--   confirmado→ admin/CD confirma recebimento físico — credita sz_stock (tipo='entrada')
--               via a MESMA rota que Create/Bipar usam (não duplica lógica de crédito).
--   concluido → fecha a remessa (estado terminal, só housekeeping).
--
-- Idempotente: CREATE TABLE IF NOT EXISTS.
-- =============================================================================

CREATE TABLE IF NOT EXISTS sz_stock_shipments (
    id             BIGSERIAL PRIMARY KEY,
    producer_id    BIGINT      NOT NULL,
    cd_id          BIGINT      NOT NULL DEFAULT 0,
    status         VARCHAR(20) NOT NULL DEFAULT 'pendente'
        CHECK (status IN ('pendente', 'enviado', 'confirmado', 'concluido', 'cancelado')),
    motivo         TEXT        NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at        TIMESTAMPTZ NULL,
    confirmed_at   TIMESTAMPTZ NULL,
    concluded_at   TIMESTAMPTZ NULL,
    confirmed_by   BIGINT      NULL, -- admin_id de quem confirmou o recebimento
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_stock_shipments_producer ON sz_stock_shipments (producer_id);
CREATE INDEX IF NOT EXISTS idx_stock_shipments_status   ON sz_stock_shipments (status);

CREATE TABLE IF NOT EXISTS sz_stock_shipment_items (
    id             BIGSERIAL PRIMARY KEY,
    shipment_id    BIGINT      NOT NULL REFERENCES sz_stock_shipments(id) ON DELETE CASCADE,
    product_id     BIGINT      NOT NULL,
    variation_id   BIGINT      NOT NULL DEFAULT 0,
    quantity       INTEGER     NOT NULL CHECK (quantity > 0)
);
CREATE INDEX IF NOT EXISTS idx_stock_shipment_items_shipment ON sz_stock_shipment_items (shipment_id);
