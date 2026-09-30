-- =============================================================================
-- 401-fix-auditoria-infra.sql — fixes da auditoria (P1) que o agente não landou.
--   (1) Retenção/anonimização de PII de PEDIDO/ENTREGA (Art. 15/16 + Art. 6º LGPD).
--   (2) Limpeza de receita ÓRFÃ (refs → pedido inexistente) que inflava o ledger.
-- Idempotente, conservador, comentado. Núcleo financeiro/dado.
-- =============================================================================

-- ── (1) Retenção de PII de pedido/entrega (LGPD Art. 15/16 + minimização Art.6) ─
-- Anonimiza PII de pedidos FINALIZADOS há muito tempo (entregue/cancelado/frustrado
-- há > 2 anos), respeitando GUARDA FISCAL: NUNCA deleta linha nem toca em valores
-- financeiros — só mascara dados pessoais. Reusa sz_mask_email.
-- AUDIT-2026-06-21 #17: comentário anterior afirmava cobrir "cpf/endereço", mas
-- ESTA versão só fazia nome/telefone/email. A cobertura de CPF/endereço/CEP/coords
-- foi REDEFINIDA (CREATE OR REPLACE desta função) em 441-lgpd-retencao-cpf-endereco.sql.
-- Idempotente (não re-mascara já-mascarado).
CREATE OR REPLACE FUNCTION sz_anonymize_old_order_pii()
RETURNS integer LANGUAGE plpgsql AS $$
DECLARE n1 integer; n2 integer;
BEGIN
    -- Endereços de pedido (sz_order_addresses) de pedidos finalizados > 2 anos.
    UPDATE sz_order_addresses a
       SET nome        = '[anonimizado]',
           telefone    = '0000000000',
           email       = CASE WHEN a.email IS NOT NULL AND a.email <> '' THEN sz_mask_email(a.email) ELSE a.email END
      FROM sz_orders o
     WHERE a.order_id = o.id
       AND o.status IN ('entregue','completo','cancelled','cancelado','frustrado','reembolsado')
       AND COALESCE(o.created_at, NOW()) < NOW() - INTERVAL '2 years'
       AND a.nome <> '[anonimizado]';
    GET DIAGNOSTICS n1 = ROW_COUNT;

    -- Destinatário no pedido motoboy (dest_*) — mesma janela/critério.
    UPDATE sz_motoboy_pedidos m
       SET dest_nome      = '[anonimizado]',
           dest_telefone  = '0000000000'
      FROM sz_orders o
     WHERE COALESCE(o.wp_order_id, o.id) = m.wc_order_id
       AND o.status IN ('entregue','completo','cancelled','cancelado','frustrado','reembolsado')
       AND COALESCE(o.created_at, NOW()) < NOW() - INTERVAL '2 years'
       AND m.dest_nome <> '[anonimizado]';
    GET DIAGNOSTICS n2 = ROW_COUNT;

    RETURN n1 + n2;
END; $$;
COMMENT ON FUNCTION sz_anonymize_old_order_pii() IS
  'LGPD Art.15/16: anonimiza PII de pedidos finalizados > 2 anos; NÃO deleta linha nem toca valor financeiro (guarda fiscal). Idempotente. Chamar via go/cron junto de sz_anonymize_old_pii().';

-- ── (2) Limpeza de receita ÓRFÃ ───────────────────────────────────────────────
-- senderzz_revenue tem linhas cujo ref aponta p/ pedido que NÃO EXISTE em sz_orders
-- (resíduo das re-derivações da migração) — inflava o ledger (~R$89,77). Deleta SÓ
-- essas, com guarda explícita NOT EXISTS. Receita de pedido existente fica intacta.
DELETE FROM senderzz_revenue r
 WHERE (r.ref LIKE 'order:%' OR r.ref LIKE 'order_aff:%' OR r.ref LIKE 'order_entrega:%')
   AND r.ref ~ ':[0-9]+$'
   AND NOT EXISTS (
        SELECT 1 FROM sz_orders o
         WHERE o.id = split_part(r.ref, ':', 2)::bigint
            OR o.wp_order_id = split_part(r.ref, ':', 2)::bigint
   );

-- NOTA (divergência residual de VALOR, NÃO órfã): a view sz_revenue_reconciliacao
-- ainda pode acusar diferença entre ledger e esperado para pedidos QUE EXISTEM —
-- é valor booked pelo trigger em momento anterior ao backfill de taxas. Re-derivar
-- o ledger inteiro (DELETE + re-INSERT via triggers) corrige, mas é operação de
-- risco e fica DOCUMENTADA p/ execução supervisionada (não automática). A view de
-- reconciliação + alerta > R$1 já dá visibilidade contínua dessa divergência.
