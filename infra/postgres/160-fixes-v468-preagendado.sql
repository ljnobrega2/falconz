-- schema-fixes-v468-preagendado.sql
-- Amplia o CHECK de sz_motoboy_pedidos.status para aceitar 'pre_agendado'.
-- Necessário p/ reagendamento de pedido motoboy com data > 3 dias úteis (admin Orders.Reagendar).
-- Não-destrutivo: só adiciona valores permitidos. Aplicar por ambiente antes do deploy do go/admin v468+.
ALTER TABLE sz_motoboy_pedidos DROP CONSTRAINT IF EXISTS sz_motoboy_pedidos_status_check;
ALTER TABLE sz_motoboy_pedidos ADD CONSTRAINT sz_motoboy_pedidos_status_check
  CHECK (status IN ('agendado','embalado','em_rota','entregue','frustrado','cancelado','aprovado','a_caminho','reagendado','pre_agendado'));
