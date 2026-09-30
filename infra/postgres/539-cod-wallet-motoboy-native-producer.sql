-- 539 — Crédito COD automático para produtores nativos FALK
--
-- Produtores criados no portal não possuem wp_user_id. A carteira COD usa
-- wp_user_id para contas legadas e -senderzz_portal_users.id para contas nativas
-- (namespace negativo, sem colisão com IDs WordPress positivos).
-- O crédito nasce pendente e fica disponível após D+7 da entrega.

CREATE OR REPLACE FUNCTION sz_cod_wallet_user_key(p_portal_user_id BIGINT)
RETURNS BIGINT
LANGUAGE SQL
STABLE
AS $$
  SELECT CASE
    WHEN COALESCE(wp_user_id, 0) > 0 THEN wp_user_id
    ELSE -id
  END
  FROM senderzz_portal_users
  WHERE id = p_portal_user_id
$$;

CREATE OR REPLACE FUNCTION trg_sz_motoboy_cod_wallet_credit()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  v_order_id BIGINT;
  v_produtor_id BIGINT;
  v_wallet_user_id BIGINT;
  v_net NUMERIC(10,2);
  v_delivered_at TIMESTAMPTZ;
BEGIN
  IF NEW.status <> 'entregue' THEN
    RETURN NEW;
  END IF;

  SELECT o.id, o.produtor_id, sz_cod_wallet_user_key(o.produtor_id),
         COALESCE(f.producer_net_live, f.liquido_produtor, 0),
         COALESCE(NEW.ts_entregue, NEW.baixa_at, NOW())
    INTO v_order_id, v_produtor_id, v_wallet_user_id, v_net, v_delivered_at
    FROM sz_orders o
    JOIN sz_order_financials f ON f.order_id = o.id
   WHERE COALESCE(o.wp_order_id, o.id) = NEW.wc_order_id
     AND o.produtor_id IS NOT NULL
   ORDER BY o.id DESC
   LIMIT 1;

  IF v_order_id IS NULL OR v_wallet_user_id IS NULL OR v_net <= 0 THEN
    RETURN NEW;
  END IF;

  INSERT INTO sz_cod_wallet_transactions
    (user_id, order_id, type, status, amount, gross, net, fee,
     release_at, created_at, updated_at, description)
  SELECT v_wallet_user_id, v_order_id, 'cod_received', 'pending',
         v_net, v_net, v_net, 0,
         v_delivered_at + INTERVAL '7 days', NOW(), NOW(),
         'Recebimento COD Motoboy do pedido #' || v_order_id || ' (D+7)'
   WHERE NOT EXISTS (
     SELECT 1
       FROM sz_cod_wallet_transactions w
      WHERE w.order_id = v_order_id
        AND w.type = 'cod_received'
        AND w.status <> 'reversed'
   );

  RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS sz_motoboy_cod_wallet_credit ON sz_motoboy_pedidos;
CREATE TRIGGER sz_motoboy_cod_wallet_credit
AFTER INSERT OR UPDATE OF status, ts_entregue, baixa_at
ON sz_motoboy_pedidos
FOR EACH ROW
WHEN (NEW.status = 'entregue')
EXECUTE FUNCTION trg_sz_motoboy_cod_wallet_credit();

-- Backfill idempotente dos pedidos motoboy já entregues sem lançamento.
INSERT INTO sz_cod_wallet_transactions
  (user_id, order_id, type, status, amount, gross, net, fee,
   release_at, created_at, updated_at, description)
SELECT sz_cod_wallet_user_key(o.produtor_id), o.id, 'cod_received',
       CASE WHEN COALESCE(mp.ts_entregue, mp.baixa_at, o.updated_at) + INTERVAL '7 days' <= NOW()
            THEN 'available' ELSE 'pending' END,
       f.producer_net_live, f.producer_net_live, f.producer_net_live, 0,
       COALESCE(mp.ts_entregue, mp.baixa_at, o.updated_at) + INTERVAL '7 days',
       NOW(), NOW(),
       'Recebimento COD Motoboy do pedido #' || o.id || ' (backfill 539)'
  FROM sz_orders o
  JOIN sz_order_financials f ON f.order_id = o.id
  JOIN sz_motoboy_pedidos mp ON mp.wc_order_id = COALESCE(o.wp_order_id, o.id)
 WHERE mp.status = 'entregue'
   AND o.produtor_id IS NOT NULL
   AND f.producer_net_live > 0
   AND NOT EXISTS (
     SELECT 1 FROM sz_cod_wallet_transactions w
      WHERE w.order_id = o.id
        AND w.type = 'cod_received'
        AND w.status <> 'reversed'
   );
