-- 543-motoboy-financial-status.sql
--
-- Separa o ciclo logístico do pós-entrega financeiro:
--   status                  = fluxo logístico existente (até entregue)
--   financial_status        = controle financeiro pós-entrega
--   scheduled_payment_date  = data combinada para pagamento futuro
--
-- Não altera pedidos antigos e não renomeia status logísticos.

ALTER TABLE sz_motoboy_pedidos
  ADD COLUMN IF NOT EXISTS financial_status varchar(32),
  ADD COLUMN IF NOT EXISTS scheduled_payment_date date,
  ADD COLUMN IF NOT EXISTS financial_status_updated_at timestamptz,
  ADD COLUMN IF NOT EXISTS financial_status_updated_by bigint;

ALTER TABLE sz_motoboy_pedidos
  DROP CONSTRAINT IF EXISTS sz_motoboy_pedidos_financial_status_check;

ALTER TABLE sz_motoboy_pedidos
  ADD CONSTRAINT sz_motoboy_pedidos_financial_status_check
  CHECK (
    financial_status IS NULL OR financial_status IN (
      'pagamento_agendado',
      'vencido',
      'concluido'
    )
  );

CREATE INDEX IF NOT EXISTS idx_motoboy_financial_status
  ON sz_motoboy_pedidos (financial_status)
  WHERE financial_status IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_motoboy_scheduled_payment_date
  ON sz_motoboy_pedidos (scheduled_payment_date)
  WHERE scheduled_payment_date IS NOT NULL;

COMMENT ON COLUMN sz_motoboy_pedidos.financial_status IS
  'Status financeiro pós-entrega: pagamento_agendado, vencido, concluido. NULL = ainda sem controle financeiro separado.';

COMMENT ON COLUMN sz_motoboy_pedidos.scheduled_payment_date IS
  'Data combinada para pagamento após entrega. Usada por pagamento_agendado e vencimento automático.';

COMMENT ON COLUMN sz_motoboy_pedidos.financial_status_updated_at IS
  'Data/hora da última alteração do status financeiro pós-entrega.';

COMMENT ON COLUMN sz_motoboy_pedidos.financial_status_updated_by IS
  'Admin/usuário que fez a última alteração manual do status financeiro.';

UPDATE sz_motoboy_pedidos
   SET financial_status = 'vencido',
       financial_status_updated_at = COALESCE(financial_status_updated_at, NOW())
 WHERE financial_status = 'pagamento_agendado'
   AND scheduled_payment_date < CURRENT_DATE;
