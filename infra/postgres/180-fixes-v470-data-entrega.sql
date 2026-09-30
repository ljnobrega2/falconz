-- Data de entrega agendada do pedido motoboy (separada de created_at = data do
-- pedido, e de reagendado_para = reagendamento). Usada pelo filtro de etiquetas
-- (filtra por data de ENTREGA) e pelo motor de agendamento do checkout (etapa 2).
-- NULL = sem agendamento explícito → cai para reagendado_para / created_at.
ALTER TABLE sz_motoboy_pedidos ADD COLUMN IF NOT EXISTS data_entrega date;
CREATE INDEX IF NOT EXISTS idx_mb_pedidos_data_entrega ON sz_motoboy_pedidos (data_entrega);
