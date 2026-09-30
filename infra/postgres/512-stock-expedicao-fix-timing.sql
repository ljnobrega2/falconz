-- 512-stock-expedicao-fix-timing.sql — corrige bug de ORDEM descoberto ao vivo
-- logo após aplicar 511: todo caminho de criação de pedido (checkout.go nativo
-- E api_orders.go) insere sz_orders PRIMEIRO, sz_order_items DEPOIS, na MESMA
-- transação. trg_stock_order_ins (AFTER INSERT ON sz_orders) disparava ANTES
-- dos itens existirem → loop `SELECT ... FROM sz_order_items WHERE order_id=NEW.id`
-- sempre vazio → reserva inicial NUNCA acontecia (confirmado ao vivo: pedido
-- 1646 criado via API, zero movimento gravado).
--
-- Fix: reserva INICIAL passa a disparar em AFTER INSERT ON sz_order_items (a
-- linha do pedido JÁ existe nesse ponto — FK garante isso — então dá pra ler
-- o status atual via JOIN). Transições de STATUS (aprovar/embalar/enviar/...)
-- continuam no trigger de sz_orders (trg_stock_order_upd, inalterado) — nesse
-- ponto os itens já existem há muito tempo, sem problema de ordem.

-- Remove o disparo de INSERT em sz_orders (não serve mais — substituído pelo de items).
DROP TRIGGER IF EXISTS trg_stock_order_ins ON sz_orders;

CREATE OR REPLACE FUNCTION sz_stock_apply_order_item_ins()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    v_order  record;
    v_delta  record;
    v_cd     bigint;
    v_avail  integer;
    v_res    integer;
BEGIN
    SELECT id, status, wp_order_id INTO v_order FROM sz_orders WHERE id = NEW.order_id;
    IF NOT FOUND THEN
        RETURN NEW; -- não deveria acontecer (FK), defensivo
    END IF;

    -- Mesma guarda de 511: pedido COD/motoboy já tem trigger próprio.
    IF EXISTS (
        SELECT 1 FROM sz_motoboy_pedidos mp
         WHERE mp.wc_order_id = COALESCE(v_order.wp_order_id, v_order.id)
    ) THEN
        RETURN NEW;
    END IF;

    SELECT * INTO v_delta FROM sz_stock_delta_orders('', v_order.status, NEW.quantidade);
    IF v_delta.d_available = 0 AND v_delta.d_reserved = 0 THEN
        RETURN NEW;
    END IF;

    -- CD com mais disponível agora (mesma regra de 511 — todo estoque hoje é
    -- "Grande São Paulo", 1 CD relevante na prática; escolha é só defensiva
    -- pra quando existir mais de um).
    SELECT cd_id INTO v_cd
      FROM sz_stock
     WHERE product_id = NEW.produto_id AND variation_id = 0
     ORDER BY (qty_available - qty_reserved) DESC
     LIMIT 1;
    IF v_cd IS NULL THEN
        v_cd := 0;
    END IF;

    INSERT INTO sz_stock (product_id, cd_id, qty_available, qty_reserved)
    VALUES (NEW.produto_id, v_cd, 0, 0)
    ON CONFLICT (product_id, variation_id, cd_id) DO NOTHING;

    SELECT qty_available + v_delta.d_available, qty_reserved + v_delta.d_reserved
      INTO v_avail, v_res
      FROM sz_stock
     WHERE product_id = NEW.produto_id AND variation_id = 0 AND cd_id = v_cd
     FOR UPDATE;

    IF v_res < 0 THEN
        RAISE WARNING '[sz_stock] reserva negativa evitada (item_ins) produto=% cd=% pedido=%', NEW.produto_id, v_cd, v_order.id;
        v_res := 0;
    END IF;
    IF v_avail < 0 THEN
        RAISE WARNING '[sz_stock] disponivel negativo evitado (item_ins) produto=% cd=% pedido=%', NEW.produto_id, v_cd, v_order.id;
        v_avail := 0;
    END IF;

    UPDATE sz_stock SET qty_available = v_avail, qty_reserved = v_res, updated_at = now()
     WHERE product_id = NEW.produto_id AND variation_id = 0 AND cd_id = v_cd;

    INSERT INTO sz_stock_movements
        (product_id, variation_id, cd_id, order_id, delta_available, delta_reserved, tipo, motivo)
    VALUES
        (NEW.produto_id, 0, v_cd, v_order.id, v_delta.d_available, v_delta.d_reserved,
         CASE WHEN v_delta.d_reserved > 0 THEN 'reserva' ELSE 'liberacao' END,
         format('pedido sz_orders %s: item criado (status %s)', v_order.id, v_order.status));

    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_stock_order_item_ins ON sz_order_items;
CREATE TRIGGER trg_stock_order_item_ins
    AFTER INSERT ON sz_order_items
    FOR EACH ROW EXECUTE FUNCTION sz_stock_apply_order_item_ins();

-- Corrige a reserva "fantasma" do pedido 1646 (criado nos minutos entre 511 e
-- este fix — trigger não disparou pra ele, ficou sem movimento). Idempotente:
-- só aplica se ainda não tem nenhum movimento de estoque.
DO $$
DECLARE
    v_order record;
    v_item  record;
    v_delta record;
    v_cd    bigint;
    v_avail integer;
    v_res   integer;
BEGIN
    FOR v_order IN
        SELECT o.id, o.status, o.wp_order_id
          FROM sz_orders o
         WHERE o.status = ANY(ARRAY['pending','processing','aguardando','on-hold','em_separacao','embalado','enviado'])
           AND NOT EXISTS (SELECT 1 FROM sz_stock_movements m WHERE m.order_id = o.id)
           AND NOT EXISTS (SELECT 1 FROM sz_motoboy_pedidos mp WHERE mp.wc_order_id = COALESCE(o.wp_order_id, o.id))
    LOOP
        FOR v_item IN SELECT produto_id, quantidade FROM sz_order_items WHERE order_id = v_order.id LOOP
            SELECT * INTO v_delta FROM sz_stock_delta_orders('', v_order.status, v_item.quantidade);
            CONTINUE WHEN v_delta.d_reserved = 0;

            SELECT cd_id INTO v_cd FROM sz_stock
             WHERE product_id = v_item.produto_id AND variation_id = 0
             ORDER BY (qty_available - qty_reserved) DESC LIMIT 1;
            IF v_cd IS NULL THEN v_cd := 0; END IF;

            INSERT INTO sz_stock (product_id, cd_id, qty_available, qty_reserved)
            VALUES (v_item.produto_id, v_cd, 0, 0)
            ON CONFLICT (product_id, variation_id, cd_id) DO NOTHING;

            SELECT qty_reserved + v_delta.d_reserved INTO v_res
              FROM sz_stock WHERE product_id = v_item.produto_id AND variation_id = 0 AND cd_id = v_cd
             FOR UPDATE;
            IF v_res < 0 THEN v_res := 0; END IF;

            UPDATE sz_stock SET qty_reserved = v_res, updated_at = now()
             WHERE product_id = v_item.produto_id AND variation_id = 0 AND cd_id = v_cd;

            INSERT INTO sz_stock_movements
                (product_id, variation_id, cd_id, order_id, delta_available, delta_reserved, tipo, motivo)
            VALUES
                (v_item.produto_id, 0, v_cd, v_order.id, 0, v_delta.d_reserved, 'reserva',
                 format('backfill 512: pedido %s status atual %s (criado na janela sem trigger)', v_order.id, v_order.status));
        END LOOP;
    END LOOP;
END $$;
