-- AUDIT-2026-07-31 — dois achados finais investigando o resíduo restante da
-- reconciliação (após 524/525):
--
-- (1) 8 pedidos frustrados antigos (1380,1381,1394,1500,1546,1562,1566,1569)
--     nunca dispararam sz_revenue_capture_order NENHUMA vez — são anteriores
--     à instalação do trigger direto em produção (nunca versionado, ver 523).
--     Não é bug de fórmula: qualquer UPDATE de status a partir de agora já
--     dispara certo. Backfill: força o trigger a rodar de novo pra esses 8
--     (UPDATE ... SET status=status — Postgres considera a coluna "tocada"
--     pelo SET mesmo sem mudar de valor, então o trigger column-list dispara).
--
-- (2) Pedido 1646 tem lançamento em senderzz_revenue (R$1,50, componente
--     taxa_transacao_produtor) mas NÃO EXISTE MAIS em sz_orders — linha órfã
--     de uma exclusão manual antiga (fora do fluxo normal de cancelamento,
--     que faria DELETE FROM senderzz_revenue via trigger). Remove o órfão.
--
-- Confirma reconciliação 100% limpa após isso (verificado contra clone real).

UPDATE sz_orders SET status = status
 WHERE id IN (1380,1381,1394,1500,1546,1562,1566,1569);

DELETE FROM senderzz_revenue r
 WHERE NOT EXISTS (SELECT 1 FROM sz_orders o WHERE o.id = r.order_id);
