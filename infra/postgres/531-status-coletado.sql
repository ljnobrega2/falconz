-- AUDIT-2026-07-31 (dono) — novo status 'coletado': operador logístico ou
-- admin marca quando o pacote é colocado no ponto de coleta (ação manual,
-- mesma tela de pedidos do admin — operador logístico já tem acesso via
-- DualAuth). Fluxo: embalado → coletado → enviado. 'enviado' sempre
-- sobrepõe 'coletado' (confirmação real da ME via rastreio vence).

ALTER TABLE sz_orders DROP CONSTRAINT IF EXISTS sz_orders_status_check;
ALTER TABLE sz_orders ADD CONSTRAINT sz_orders_status_check
    CHECK (status IN (
        'pending','processing','aguardando','on-hold','em_andamento',
        'em_separacao','embalado','coletado','enviado','entregue','completo',
        'cancelled','frustrado','reembolsado'
    ));
