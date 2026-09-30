-- 540-stock-motoboy-composition-source-cd.sql
--
-- Corrige duas divergências do estoque COD/motoboy:
--   1. sz_stock_apply_pedido ainda multiplicava quantidade pelo número inicial
--      do nome. Em linhas de composição ("3 Séruns + 2 Espumas — Sérum"), a
--      própria quantidade já é física; o cálculo antigo transformava 3 em 9.
--   2. reserva/commit usavam sempre o CD do pedido, mesmo quando o saldo físico
--      estava na linha de estoque único (cd_id=0). O commit era limitado a zero
--      e não reduzia o saldo global.
--
-- A função canônica sz_stock_physical_qty (534) resolve kit legado versus
-- composição. Reservas novas escolhem a linha com maior saldo vendável e as
-- transições seguintes reutilizam o CD gravado no movimento de reserva.

BEGIN;

CREATE OR REPLACE FUNCTION sz_stock_apply_pedido()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    v_old        text;
    v_item       record;
    v_delta      record;
    v_new_avail  integer;
    v_new_res    integer;
    v_qty_fisica integer;
    v_cd         bigint;
BEGIN
    v_old := CASE WHEN TG_OP = 'INSERT' THEN '' ELSE OLD.status END;

    IF TG_OP = 'UPDATE' AND v_old IS NOT DISTINCT FROM NEW.status THEN
        RETURN NEW;
    END IF;

    FOR v_item IN
        SELECT i.produto_id, i.quantidade, i.nome
          FROM sz_order_items i
          JOIN sz_orders o ON i.order_id = o.id
         WHERE COALESCE(o.wp_order_id, o.id) = NEW.wc_order_id
    LOOP
        v_qty_fisica := sz_stock_physical_qty(v_item.quantidade, v_item.nome);
        SELECT * INTO v_delta FROM sz_stock_delta(v_old, NEW.status, v_qty_fisica);
        CONTINUE WHEN v_delta.d_available = 0 AND v_delta.d_reserved = 0;

        IF v_delta.d_reserved > 0 THEN
            -- Nova reserva: estoque é compartilhado; usa a linha com maior saldo
            -- realmente vendável. Em empate, prefere o CD operacional do pedido.
            SELECT cd_id INTO v_cd
              FROM sz_stock
             WHERE product_id = v_item.produto_id AND variation_id = 0
             ORDER BY (qty_available - qty_reserved) DESC,
                      (cd_id = NEW.cd_id) DESC,
                      cd_id
             LIMIT 1;
        ELSE
            -- Commit/liberação: sempre volta à mesma linha onde reservou.
            SELECT cd_id INTO v_cd
              FROM sz_stock_movements
             WHERE order_id = NEW.wc_order_id
               AND product_id = v_item.produto_id
               AND tipo = 'reserva'
             ORDER BY id DESC
             LIMIT 1;
        END IF;

        IF v_cd IS NULL THEN
            SELECT cd_id INTO v_cd
              FROM sz_stock
             WHERE product_id = v_item.produto_id AND variation_id = 0
             ORDER BY (qty_available - qty_reserved) DESC, cd_id
             LIMIT 1;
        END IF;
        IF v_cd IS NULL THEN
            v_cd := COALESCE(NEW.cd_id, 0);
        END IF;

        INSERT INTO sz_stock (product_id, cd_id, qty_available, qty_reserved)
        VALUES (v_item.produto_id, v_cd, 0, 0)
        ON CONFLICT (product_id, variation_id, cd_id) DO NOTHING;

        SELECT qty_available + v_delta.d_available,
               qty_reserved  + v_delta.d_reserved
          INTO v_new_avail, v_new_res
          FROM sz_stock
         WHERE product_id = v_item.produto_id
           AND variation_id = 0
           AND cd_id = v_cd
         FOR UPDATE;

        IF v_new_res < 0 THEN
            RAISE WARNING '[sz_stock] reserva negativa evitada produto=% cd=% pedido=% (% -> %, calc=%)',
                v_item.produto_id, v_cd, NEW.id, v_old, NEW.status, v_new_res;
            v_new_res := 0;
        END IF;
        IF v_new_avail < 0 THEN
            RAISE WARNING '[sz_stock] disponível negativo evitado produto=% cd=% pedido=% (% -> %, calc=%)',
                v_item.produto_id, v_cd, NEW.id, v_old, NEW.status, v_new_avail;
            v_new_avail := 0;
        END IF;

        UPDATE sz_stock
           SET qty_available = v_new_avail,
               qty_reserved  = v_new_res,
               updated_at    = NOW()
         WHERE product_id = v_item.produto_id
           AND variation_id = 0
           AND cd_id = v_cd;

        INSERT INTO sz_stock_movements
            (product_id, variation_id, cd_id, order_id,
             delta_available, delta_reserved, tipo, motivo)
        VALUES
            (v_item.produto_id, 0, v_cd, NEW.wc_order_id,
             v_delta.d_available, v_delta.d_reserved,
             CASE
                 WHEN NEW.status = 'entregue' THEN 'commit'
                 WHEN v_delta.d_reserved > 0 THEN 'reserva'
                 ELSE 'liberacao'
             END,
             format('pedido %s: %s -> %s (qty_fisica=%s, estoque_cd=%s)',
                    NEW.id, v_old, NEW.status, v_qty_fisica, v_cd));
    END LOOP;

    RETURN NEW;
END;
$$;

-- Guardas executáveis contra a regressão que originou este ajuste.
DO $$
BEGIN
    IF sz_stock_physical_qty(3, '3 Egipzya Sérum + 2 Egipzya Espuma — Sérum') <> 3 THEN
        RAISE EXCEPTION 'sz_stock_physical_qty voltou a multiplicar composição';
    END IF;
    IF position('sz_stock_physical_qty' IN pg_get_functiondef('sz_stock_apply_pedido()'::regprocedure)) = 0 THEN
        RAISE EXCEPTION 'sz_stock_apply_pedido não usa a fórmula canônica';
    END IF;
END $$;

COMMIT;

