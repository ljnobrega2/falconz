-- 532-stock-expedicao-kit-multiplier.sql
--
-- Expedição stores a kit as one order-item row (quantidade=1), while the
-- physical quantity is encoded at the start of the item name: "6 MelaVitta".
-- The expedition stock triggers were reserving only quantidade, so kits were
-- under-counted. COD/motoboy remains outside these triggers.

BEGIN;

CREATE OR REPLACE FUNCTION sz_stock_physical_qty(p_qty integer, p_name text)
RETURNS integer
LANGUAGE sql IMMUTABLE AS $$
  SELECT GREATEST(1, COALESCE(NULLIF(substring(COALESCE(p_name, '') from '^([0-9]+)\s'), '')::integer, 1))
         * GREATEST(1, COALESCE(p_qty, 1));
$$;

CREATE OR REPLACE FUNCTION sz_stock_apply_order_item_ins()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    v_order  record;
    v_delta  record;
    v_cd     bigint;
    v_avail  integer;
    v_res    integer;
    v_qty_fisica integer;
BEGIN
    SELECT id, status, wp_order_id INTO v_order FROM sz_orders WHERE id = NEW.order_id;
    IF NOT FOUND THEN RETURN NEW; END IF;

    -- COD/motoboy has its own stock trigger. This function is Expedição only.
    IF EXISTS (
        SELECT 1 FROM sz_motoboy_pedidos mp
         WHERE mp.wc_order_id = COALESCE(v_order.wp_order_id, v_order.id)
    ) THEN RETURN NEW; END IF;

    v_qty_fisica := sz_stock_physical_qty(NEW.quantidade, NEW.nome);
    SELECT * INTO v_delta FROM sz_stock_delta_orders('', v_order.status, v_qty_fisica);
    IF v_delta.d_available = 0 AND v_delta.d_reserved = 0 THEN RETURN NEW; END IF;

    SELECT cd_id INTO v_cd FROM sz_stock
     WHERE product_id = NEW.produto_id AND variation_id = 0
     ORDER BY (qty_available - qty_reserved) DESC LIMIT 1;
    IF v_cd IS NULL THEN v_cd := 0; END IF;

    INSERT INTO sz_stock (product_id, cd_id, qty_available, qty_reserved)
    VALUES (NEW.produto_id, v_cd, 0, 0)
    ON CONFLICT (product_id, variation_id, cd_id) DO NOTHING;

    SELECT qty_available + v_delta.d_available, qty_reserved + v_delta.d_reserved
      INTO v_avail, v_res
      FROM sz_stock
     WHERE product_id = NEW.produto_id AND variation_id = 0 AND cd_id = v_cd
     FOR UPDATE;
    v_res := GREATEST(v_res, 0);
    v_avail := GREATEST(v_avail, 0);

    UPDATE sz_stock SET qty_available = v_avail, qty_reserved = v_res, updated_at = now()
     WHERE product_id = NEW.produto_id AND variation_id = 0 AND cd_id = v_cd;
    INSERT INTO sz_stock_movements
        (product_id, variation_id, cd_id, order_id, delta_available, delta_reserved, tipo, motivo)
    VALUES (NEW.produto_id, 0, v_cd, v_order.id, v_delta.d_available, v_delta.d_reserved,
            CASE WHEN v_delta.d_reserved > 0 THEN 'reserva' ELSE 'liberacao' END,
            format('pedido sz_orders %s: item criado (status %s, qty_fisica=%s)',
                   v_order.id, v_order.status, v_qty_fisica));
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION sz_stock_apply_order()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    v_old text;
    v_item record;
    v_delta record;
    v_new_avail integer;
    v_new_res integer;
    v_cd bigint;
    v_qty_fisica integer;
BEGIN
    v_old := CASE WHEN TG_OP = 'INSERT' THEN '' ELSE OLD.status END;
    IF TG_OP = 'UPDATE' AND v_old IS NOT DISTINCT FROM NEW.status THEN RETURN NEW; END IF;

    -- COD/motoboy is handled by sz_motoboy_pedidos, never twice here.
    IF EXISTS (
        SELECT 1 FROM sz_motoboy_pedidos mp
         WHERE mp.wc_order_id = COALESCE(NEW.wp_order_id, NEW.id)
    ) THEN RETURN NEW; END IF;

    FOR v_item IN
        SELECT produto_id, quantidade, nome FROM sz_order_items WHERE order_id = NEW.id
    LOOP
        v_qty_fisica := sz_stock_physical_qty(v_item.quantidade, v_item.nome);
        SELECT * INTO v_delta FROM sz_stock_delta_orders(v_old, NEW.status, v_qty_fisica);
        CONTINUE WHEN v_delta.d_available = 0 AND v_delta.d_reserved = 0;

        IF TG_OP = 'INSERT' THEN
            SELECT cd_id INTO v_cd FROM sz_stock
             WHERE product_id = v_item.produto_id AND variation_id = 0
             ORDER BY (qty_available - qty_reserved) DESC LIMIT 1;
            IF v_cd IS NULL THEN v_cd := 0; END IF;
        ELSE
            SELECT cd_id INTO v_cd FROM sz_stock_movements
             WHERE order_id = NEW.id AND product_id = v_item.produto_id AND tipo = 'reserva'
             ORDER BY id ASC LIMIT 1;
            IF v_cd IS NULL THEN v_cd := 0; END IF;
        END IF;

        INSERT INTO sz_stock (product_id, cd_id, qty_available, qty_reserved)
        VALUES (v_item.produto_id, v_cd, 0, 0)
        ON CONFLICT (product_id, variation_id, cd_id) DO NOTHING;
        SELECT qty_available + v_delta.d_available, qty_reserved + v_delta.d_reserved
          INTO v_new_avail, v_new_res FROM sz_stock
         WHERE product_id = v_item.produto_id AND variation_id = 0 AND cd_id = v_cd FOR UPDATE;
        v_new_res := GREATEST(v_new_res, 0);
        v_new_avail := GREATEST(v_new_avail, 0);
        UPDATE sz_stock SET qty_available=v_new_avail, qty_reserved=v_new_res, updated_at=now()
         WHERE product_id=v_item.produto_id AND variation_id=0 AND cd_id=v_cd;
        INSERT INTO sz_stock_movements
            (product_id, variation_id, cd_id, order_id, delta_available, delta_reserved, tipo, motivo)
        VALUES (v_item.produto_id, 0, v_cd, NEW.id, v_delta.d_available, v_delta.d_reserved,
                CASE WHEN NEW.status = ANY(ARRAY['entregue','completo']) THEN 'commit'
                     WHEN v_delta.d_reserved > 0 THEN 'reserva' ELSE 'liberacao' END,
                format('pedido sz_orders %s: %s -> %s (qty_fisica=%s)',
                       NEW.id, v_old, NEW.status, v_qty_fisica));
    END LOOP;
    RETURN NEW;
END;
$$;

-- Reconcile only active Expedição reservations. COD reservations are preserved.
DO $$
DECLARE r record;
BEGIN
  FOR r IN
    WITH order_truth AS (
      SELECT DISTINCT ON (o.id, i.produto_id)
             o.id AS order_id,
             i.produto_id AS product_id,
             m.cd_id,
             sz_stock_physical_qty(i.quantidade, i.nome) AS qty
        FROM sz_order_items i
        JOIN sz_orders o ON o.id=i.order_id
        JOIN sz_stock_movements m ON m.order_id=o.id AND m.product_id=i.produto_id AND m.tipo='reserva'
       WHERE o.status = ANY(ARRAY['pending','processing','aguardando','on-hold','em_separacao','embalado','enviado'])
         AND NOT EXISTS (SELECT 1 FROM sz_motoboy_pedidos mp
                          WHERE mp.wc_order_id=COALESCE(o.wp_order_id,o.id))
       ORDER BY o.id, i.produto_id, m.id
    ), truth AS (
      SELECT product_id, cd_id, SUM(qty)::integer AS wanted,
             COUNT(*)::integer AS recorded
        FROM order_truth GROUP BY product_id, cd_id
    ) SELECT product_id,cd_id,wanted-recorded AS delta FROM truth WHERE wanted<>recorded
  LOOP
    UPDATE sz_stock SET qty_reserved=GREATEST(qty_reserved+r.delta,0),updated_at=now()
     WHERE product_id=r.product_id AND variation_id=0 AND cd_id=r.cd_id;
    INSERT INTO sz_stock_movements
      (product_id,variation_id,cd_id,order_id,delta_available,delta_reserved,tipo,motivo)
    VALUES (r.product_id,0,r.cd_id,NULL,0,r.delta,'ajuste',
            format('532: reconciliação kit expedição (delta=%s)',r.delta));
  END LOOP;
END $$;

COMMIT;
