-- 544-motoboy-financial-overdue-cron.sql
--
-- Job idempotente para o runner go/cron:
--   Pagamento Agendado + data anterior a hoje -> Vencido.

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

  IF EXISTS (
    SELECT 1
      FROM information_schema.columns
     WHERE table_schema = 'public'
       AND table_name = 'sz_orders'
       AND column_name = 'financial_status'
  ) THEN
    UPDATE sz_orders
       SET financial_status = 'vencido',
           financial_status_updated_at = NOW(),
           updated_at = NOW()
     WHERE financial_status = 'pagamento_agendado'
       AND scheduled_payment_date < CURRENT_DATE;

    GET DIAGNOSTICS v_orders_count = ROW_COUNT;
  END IF;

  RETURN v_count + v_orders_count;
END;
$$;

COMMENT ON FUNCTION sz_motoboy_financial_overdue_due() IS
  'Vence automaticamente pedidos entregues com Pagamento Agendado cuja data agendada passou. Idempotente; chamado pelo go/cron.';
