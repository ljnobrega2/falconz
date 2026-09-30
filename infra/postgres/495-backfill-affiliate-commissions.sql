-- 495-backfill-affiliate-commissions.sql
--
-- AUDIT-2026-07-11: pedidos completo com affiliate_amount>0 nunca geravam linha
-- em senderzz_affiliate_transactions — o único writer dessa tabela era o webhook
-- legado /internal/commissions (go/affiliates), sem nenhum caller no checkout novo
-- (go/orders) nem no bridge de entrega (go/motoboy). Achado via 2 usuários com
-- carteira zerada (Gilson/#1596 ainda pending — comissão ainda não é devida;
-- Nelson/#1595 completo — comissão devida e nunca lançada).
--
-- Este backfill cobre TODO pedido completo (histórico) com comissão devida e sem
-- lançamento. Resolve o vínculo senderzz_affiliates via
-- (afiliado_id=sz_orders.affiliate_id, produtor_id=sz_orders.produtor_id) — mesma
-- convenção usada em affiliate_wallet.go (JOIN senderzz_portal_users ON
-- u.wp_user_id = a.afiliado_id). Pedidos sem vínculo resolvível são pulados (não
-- há como atribuir a comissão com segurança) — auditado antes de aplicar: 0 casos
-- órfãos em prod (2026-07-11).
--
-- status='approved' direto (não 'pending' com retenção de 30d) — pedido já
-- completo há mais de 30d nos 2 casos conhecidos; se algum backfill futuro pegar
-- pedido completo há menos de 30d, ainda assim é dinheiro já ganho — mantemos
-- 'approved' e deixamos o ciclo normal de saque decidir liberação.

BEGIN;

INSERT INTO senderzz_affiliate_transactions
    (order_id, affiliate_id, type, status, amount, description, available_at, meta_json, created_at, updated_at)
SELECT
    o.id,
    sa.id,
    'commission',
    'approved',
    o.affiliate_amount,
    'Comissão pedido #' || o.order_number || ' (backfill AUDIT-2026-07-11)',
    o.updated_at,
    jsonb_build_object('source', 'backfill_495_2026-07-11', 'order_id', o.id),
    NOW(),
    NOW()
FROM sz_orders o
JOIN senderzz_affiliates sa
  ON sa.afiliado_id = o.affiliate_id AND sa.produtor_id = o.produtor_id
WHERE o.status = 'completo'
  AND COALESCE(o.affiliate_id, 0) > 0
  AND COALESCE(o.affiliate_amount, 0) > 0
  AND NOT EXISTS (
        SELECT 1 FROM senderzz_affiliate_transactions t
         WHERE t.order_id = o.id AND t.type = 'commission' AND t.status <> 'cancelled'
      );

COMMIT;
