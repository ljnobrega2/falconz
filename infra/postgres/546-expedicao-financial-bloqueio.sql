-- 546-expedicao-financial-bloqueio.sql
--
-- Adiciona "bloqueio" ao estado financeiro de Expedição. "vencido" continua
-- permitido no banco porque e preenchido automaticamente quando a data agendada
-- passa sem o pedido virar concluido/pago.

ALTER TABLE sz_orders
  DROP CONSTRAINT IF EXISTS sz_orders_financial_status_check;

ALTER TABLE sz_orders
  ADD CONSTRAINT sz_orders_financial_status_check
  CHECK (
    financial_status IS NULL OR
    financial_status IN ('pagamento_agendado', 'bloqueio', 'vencido', 'concluido')
  );
