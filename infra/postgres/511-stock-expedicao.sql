-- 511-stock-expedicao.sql — estende reserva/débito de estoque (sz_stock) pra
-- pedidos Expedição (sz_orders: checkout-ui, admin, API), que até aqui NUNCA
-- tocavam estoque (só sz_motoboy_pedidos/COD tinha trigger — 210-fixes-v471-stock.sql).
--
-- AUDIT-2026-07-28: NÃO reusa sz_stock_delta (motoboy) — vocabulário de status é
-- diferente (agendado/aprovado/em_rota/pre_agendado/cancelado vs sz_orders:
-- pending/processing/embalado/enviado/entregue/cancelled/frustrado). Reusar
-- direto faria embalado→enviado "liberar" a reserva (enviado não tá no set
-- reservante do motoboy) e só recommitar em entregue — janela real de
-- disponível=true enquanto o pacote já saiu pra entrega. Função nova, set
-- próprio, sem essa lacuna.
--
-- Regra sz_orders:
--   RESERVAM  : pending, processing, aguardando, on-hold, em_separacao, embalado, enviado
--               (pedido Expedição É um pedido real desde a criação — sem o
--               conceito de "pré-agendado" do motoboy — reserva já na criação)
--   COMMIT    : entregue, completo   (baixa definitiva: -disponível, consome reserva)
--   LIBERAM   : cancelled, frustrado, reembolsado

CREATE OR REPLACE FUNCTION sz_stock_delta_orders(p_old text, p_new text, p_qty integer)
RETURNS TABLE(d_available integer, d_reserved integer)
LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE
    reserving  text[] := ARRAY['pending','processing','aguardando','on-hold','em_separacao','embalado','enviado'];
    committing text[] := ARRAY['entregue','completo'];
    was_res  boolean := p_old = ANY(reserving);
    will_res boolean := p_new = ANY(reserving);
BEGIN
    IF p_new = ANY(committing) THEN
        d_available := -p_qty;
        d_reserved  := CASE WHEN was_res THEN -p_qty ELSE 0 END;
    ELSIF (NOT was_res) AND will_res THEN
        d_available := 0;  d_reserved := p_qty;
    ELSIF was_res AND (NOT will_res) THEN
        d_available := 0;  d_reserved := -p_qty;
    ELSE
        d_available := 0;  d_reserved := 0;
    END IF;
    RETURN NEXT;
END;
$$;

CREATE OR REPLACE FUNCTION sz_stock_apply_order()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    v_old       text;
    v_item      record;
    v_delta     record;
    v_new_avail integer;
    v_new_res   integer;
    v_cd        bigint;
BEGIN
    v_old := CASE WHEN TG_OP = 'INSERT' THEN '' ELSE OLD.status END;

    IF TG_OP = 'UPDATE' AND v_old IS NOT DISTINCT FROM NEW.status THEN
        RETURN NEW;
    END IF;

    -- AUDIT-2026-07-28 CRÍTICO: sz_orders também contém pedidos COD/motoboy, que
    -- JÁ têm trigger próprio (trg_stock_pedido_ins/upd em sz_motoboy_pedidos,
    -- 210-fixes-v471-stock.sql) reagindo ao MESMO fulfillment físico. Sem esta
    -- guarda, um pedido COD dispararia OS DOIS triggers → dupla reserva/baixa do
    -- mesmo estoque. Só processa aqui pedidos SEM linha em sz_motoboy_pedidos
    -- (= Expedição de verdade: checkout-ui correio, admin, API).
    IF EXISTS (
        SELECT 1 FROM sz_motoboy_pedidos mp
         WHERE mp.wc_order_id = COALESCE(NEW.wp_order_id, NEW.id)
    ) THEN
        RETURN NEW;
    END IF;

    -- produto_id em sz_order_items JÁ é a chave canônica gravada na criação
    -- (checkout.go: COALESCE(wp_post_id,id) via sz_resolve_product_id; API
    -- orders: mesma convenção — ver api_orders.go).
    --
    -- CD: pedido Expedição não tem zona/CD associado (motoboy resolve por zona
    -- de entrega; aqui não existe esse contexto). Pedido dono 2026-07-28: hoje
    -- todo estoque é "Grande São Paulo" (1 CD relevante na prática), então
    -- resolve automático = CD com MAIS disponível pra aquele produto no
    -- momento da RESERVA (INSERT). Releases/commits (UPDATE) reusam o MESMO cd_id
    -- do movimento de reserva original (sz_stock_movements) — nunca re-escolhe,
    -- senão reserva num CD e libera em outro, corrompendo os dois.
    FOR v_item IN
        SELECT produto_id, quantidade FROM sz_order_items WHERE order_id = NEW.id
    LOOP
        SELECT * INTO v_delta FROM sz_stock_delta_orders(v_old, NEW.status, v_item.quantidade);
        CONTINUE WHEN v_delta.d_available = 0 AND v_delta.d_reserved = 0;

        IF TG_OP = 'INSERT' THEN
            -- Reserva nova: escolhe o CD com mais disponível agora. Sem linha
            -- nenhuma pro produto ainda → cd_id=0 (cria a primeira).
            SELECT cd_id INTO v_cd
              FROM sz_stock
             WHERE product_id = v_item.produto_id AND variation_id = 0
             ORDER BY (qty_available - qty_reserved) DESC
             LIMIT 1;
            IF v_cd IS NULL THEN
                v_cd := 0;
            END IF;
        ELSE
            -- Release/commit: reusa o cd_id da reserva original deste pedido+produto.
            SELECT cd_id INTO v_cd
              FROM sz_stock_movements
             WHERE order_id = NEW.id AND product_id = v_item.produto_id AND tipo = 'reserva'
             ORDER BY id ASC
             LIMIT 1;
            IF v_cd IS NULL THEN
                -- Nunca reservou aqui (ex.: pedido antigo pré-backfill) — não inventa
                -- CD, aplica no 0 (mesmo fallback do INSERT) só pra não perder o
                -- rastro do movimento; warning deixa visível pra auditoria manual.
                RAISE WARNING '[sz_stock] pedido % produto % sem reserva original — aplicando release/commit em cd_id=0',
                    NEW.id, v_item.produto_id;
                v_cd := 0;
            END IF;
        END IF;

        INSERT INTO sz_stock (product_id, cd_id, qty_available, qty_reserved)
        VALUES (v_item.produto_id, v_cd, 0, 0)
        ON CONFLICT (product_id, variation_id, cd_id) DO NOTHING;

        SELECT qty_available + v_delta.d_available,
               qty_reserved  + v_delta.d_reserved
          INTO v_new_avail, v_new_res
          FROM sz_stock
         WHERE product_id = v_item.produto_id AND variation_id = 0 AND cd_id = v_cd
         FOR UPDATE;

        IF v_new_res < 0 THEN
            RAISE WARNING '[sz_stock] reserva negativa evitada (orders) produto=% cd=% pedido=% (% -> %, calc=%)',
                v_item.produto_id, v_cd, NEW.id, v_old, NEW.status, v_new_res;
            v_new_res := 0;
        END IF;
        IF v_new_avail < 0 THEN
            RAISE WARNING '[sz_stock] disponivel negativo evitado (orders) produto=% cd=% pedido=% (% -> %, calc=%)',
                v_item.produto_id, v_cd, NEW.id, v_old, NEW.status, v_new_avail;
            v_new_avail := 0;
        END IF;

        UPDATE sz_stock
           SET qty_available = v_new_avail,
               qty_reserved  = v_new_res,
               updated_at    = now()
         WHERE product_id = v_item.produto_id AND variation_id = 0 AND cd_id = v_cd;

        INSERT INTO sz_stock_movements
            (product_id, variation_id, cd_id, order_id,
             delta_available, delta_reserved, tipo, motivo)
        VALUES
            (v_item.produto_id, 0, v_cd, NEW.id,
             v_delta.d_available, v_delta.d_reserved,
             CASE
                 WHEN NEW.status = ANY(ARRAY['entregue','completo']) THEN 'commit'
                 WHEN v_delta.d_reserved > 0                          THEN 'reserva'
                 ELSE 'liberacao'
             END,
             format('pedido sz_orders %s: %s -> %s', NEW.id, v_old, NEW.status));
    END LOOP;

    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_stock_order_ins ON sz_orders;
CREATE TRIGGER trg_stock_order_ins
    AFTER INSERT ON sz_orders
    FOR EACH ROW EXECUTE FUNCTION sz_stock_apply_order();

DROP TRIGGER IF EXISTS trg_stock_order_upd ON sz_orders;
CREATE TRIGGER trg_stock_order_upd
    AFTER UPDATE OF status ON sz_orders
    FOR EACH ROW
    WHEN (OLD.status IS DISTINCT FROM NEW.status)
    EXECUTE FUNCTION sz_stock_apply_order();

-- Backfill: pedidos Expedição JÁ existentes em status "reservante" nunca
-- passaram pelo trigger (só passa a existir daqui pra frente) — sem isso,
-- qty_reserved fica subestimado a partir de agora pros pedidos antigos ainda
-- em aberto. Aplica reserva "retroativa" só pra pedidos SEM nenhum movimento
-- de estoque prévio (evita duplicar se rodar a migration 2x).
DO $$
DECLARE
    v_order  record;
    v_item   record;
    v_delta  record;
    v_avail  integer;
    v_res    integer;
BEGIN
    FOR v_order IN
        SELECT o.id, o.status
          FROM sz_orders o
         WHERE o.status = ANY(ARRAY['pending','processing','aguardando','on-hold','em_separacao','embalado','enviado'])
           AND NOT EXISTS (SELECT 1 FROM sz_stock_movements m WHERE m.order_id = o.id)
           AND NOT EXISTS (SELECT 1 FROM sz_motoboy_pedidos mp WHERE mp.wc_order_id = COALESCE(o.wp_order_id, o.id))
    LOOP
        FOR v_item IN
            SELECT produto_id, quantidade FROM sz_order_items WHERE order_id = v_order.id
        LOOP
            SELECT * INTO v_delta FROM sz_stock_delta_orders('', v_order.status, v_item.quantidade);
            CONTINUE WHEN v_delta.d_reserved = 0;

            INSERT INTO sz_stock (product_id, cd_id, qty_available, qty_reserved)
            VALUES (v_item.produto_id, 0, 0, 0)
            ON CONFLICT (product_id, variation_id, cd_id) DO NOTHING;

            SELECT qty_reserved + v_delta.d_reserved INTO v_res
              FROM sz_stock WHERE product_id = v_item.produto_id AND variation_id = 0 AND cd_id = 0
             FOR UPDATE;
            IF v_res < 0 THEN v_res := 0; END IF;

            UPDATE sz_stock SET qty_reserved = v_res, updated_at = now()
             WHERE product_id = v_item.produto_id AND variation_id = 0 AND cd_id = 0;

            INSERT INTO sz_stock_movements
                (product_id, variation_id, cd_id, order_id, delta_available, delta_reserved, tipo, motivo)
            VALUES
                (v_item.produto_id, 0, 0, v_order.id, 0, v_delta.d_reserved, 'reserva',
                 format('backfill 511: pedido %s status atual %s (sem movimento prévio)', v_order.id, v_order.status));
        END LOOP;
    END LOOP;
END $$;
