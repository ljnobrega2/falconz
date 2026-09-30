-- 514-order-status-em-andamento.sql
--
-- AUDIT-2026-07-28 (dono): pedido 1660 ficou 'processing' (=Aprovado na UI)
-- SEM nenhuma etiqueta emitida na ME (CreateShipment falhou, "Transportadora
-- não atende este trecho") — mostrava "Aprovado" pro operador como se tudo
-- tivesse dado certo, escondendo a falha real. Dono: "precisamos evitar que
-- pedido fique como aprovado sem emissão de etiqueta, deve existir o status
-- processando entre pendente e aprovado pra evitar falso aprovado."
--
-- Novo status 'em_andamento': saldo já verificado/reservado, tentando emitir
-- etiqueta agora. Só vira 'processing' (Aprovado de verdade) quando a
-- etiqueta é criada com sucesso na ME. Em falha, fica em 'em_andamento' com o
-- motivo real (sz_order_meta._sz_label_error) — nunca mais "aprovado" fantasma.
--
-- Idempotente: DROP CONSTRAINT IF EXISTS + ADD, roda de novo sem erro.
ALTER TABLE sz_orders DROP CONSTRAINT IF EXISTS sz_orders_status_check;
ALTER TABLE sz_orders ADD CONSTRAINT sz_orders_status_check
  CHECK (status IN (
    'pending', 'processing', 'aguardando', 'on-hold',
    'em_andamento',
    'em_separacao', 'embalado', 'enviado', 'entregue', 'completo',
    'cancelled', 'frustrado', 'reembolsado'
  ));
