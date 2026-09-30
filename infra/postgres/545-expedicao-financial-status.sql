-- 545-expedicao-financial-status.sql
--
-- Status financeiro pós-entrega para pedidos de Expedição (sz_orders).
-- Mantém o status logístico em sz_orders.status; o financeiro fica separado.

ALTER TABLE sz_orders
  ADD COLUMN IF NOT EXISTS financial_status varchar(32),
  ADD COLUMN IF NOT EXISTS scheduled_payment_date date,
  ADD COLUMN IF NOT EXISTS financial_status_updated_at timestamptz,
  ADD COLUMN IF NOT EXISTS financial_status_updated_by bigint;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
      FROM pg_constraint
     WHERE conname = 'sz_orders_financial_status_check'
       AND conrelid = 'sz_orders'::regclass
  ) THEN
    ALTER TABLE sz_orders
      ADD CONSTRAINT sz_orders_financial_status_check
      CHECK (
        financial_status IS NULL OR
        financial_status IN ('pagamento_agendado', 'vencido', 'concluido')
      );
  END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_sz_orders_financial_status
  ON sz_orders(financial_status);

CREATE INDEX IF NOT EXISTS idx_sz_orders_scheduled_payment_date
  ON sz_orders(scheduled_payment_date)
  WHERE financial_status = 'pagamento_agendado';

UPDATE sz_orders
   SET financial_status = 'vencido',
       financial_status_updated_at = NOW(),
       updated_at = NOW()
 WHERE financial_status = 'pagamento_agendado'
   AND scheduled_payment_date < CURRENT_DATE;

CREATE OR REPLACE FUNCTION sz_motoboy_financial_overdue_due()
RETURNS integer
LANGUAGE plpgsql
AS $$
DECLARE
  v_count integer := 0;
  v_orders_count integer := 0;
BEGIN
  UPDATE sz_motoboy_pedidos
     SET financial_status = 'vencido',
         financial_status_updated_at = NOW(),
         updated_at = NOW()
   WHERE financial_status = 'pagamento_agendado'
     AND scheduled_payment_date < CURRENT_DATE;

  GET DIAGNOSTICS v_count = ROW_COUNT;

  UPDATE sz_orders
     SET financial_status = 'vencido',
         financial_status_updated_at = NOW(),
         updated_at = NOW()
   WHERE financial_status = 'pagamento_agendado'
     AND scheduled_payment_date < CURRENT_DATE;

  GET DIAGNOSTICS v_orders_count = ROW_COUNT;

  RETURN v_count + v_orders_count;
END;
$$;
