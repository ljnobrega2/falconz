-- Adiciona coluna synced_at em sz_motoboy_pedidos (idempotente).
-- Rodar no PG antes de redeployar o motoboy Go service.
ALTER TABLE sz_motoboy_pedidos
    ADD COLUMN IF NOT EXISTS synced_at TIMESTAMP WITHOUT TIME ZONE NULL;

COMMENT ON COLUMN sz_motoboy_pedidos.synced_at IS
    'Timestamp da última replicação via double-write (internal.go). NULL = migrado via pgloader.';
