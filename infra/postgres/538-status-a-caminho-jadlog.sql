-- A Jadlog informa uma etapa intermediária própria depois da postagem.
-- Mantemos esse estado no pedido para o painel e o rastreio público refletirem
-- "A caminho" sem confundir com entrega concluída.
ALTER TABLE sz_orders DROP CONSTRAINT IF EXISTS sz_orders_status_check;
ALTER TABLE sz_orders ADD CONSTRAINT sz_orders_status_check
    CHECK (status IN (
        'pending','processing','aguardando','on-hold','em_andamento',
        'em_separacao','embalado','coletado','enviado','a_caminho',
        'entregue','completo','cancelled','frustrado','reembolsado'
    ));
