-- 484: coluna evidence_url em sz_motoboy_pedidos para comprovantes de entregue/frustrado
-- Idempotente.
ALTER TABLE sz_motoboy_pedidos
  ADD COLUMN IF NOT EXISTS evidence_url TEXT;
