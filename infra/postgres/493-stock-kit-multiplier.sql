-- 493-stock-kit-multiplier.sql
--
-- BUG: sz_order_items.quantidade = número de LINHAS do pedido (sempre 1 pra
-- ofertas de kit/bundle), NÃO o número de unidades físicas. O nome do item
-- ("3 Datalaprox") embute o multiplicador só como TEXTO — sz_stock_apply_pedido
-- (210-fixes-v471-stock.sql) usava i.quantidade puro pra reservar/baixar
-- estoque, subcontando kits (reservava 1 unidade física quando o pedido move
-- 3). Achado ao investigar #1596 (agendado, "3 Datalaprox", quantidade=1).
--
-- FIX: extrai o multiplicador do nome do item via regex '^(\d+)\s' (mesmo
-- padrão que Orders.tsx já usa no front pra exibir "3 Datalaprox" — ver
-- extractLeadingQty em admin-ui/src/pages/Orders.tsx). Sem número no início
-- do nome ⇒ multiplicador 1 (produto normal, sem mudança de comportamento).
-- NÃO mexe em preço/financeiro (esses já usam quantidade=1 corretamente, o
-- kit já tem o preço total embutido em preco_unit/subtotal).

BEGIN;

CREATE OR REPLACE FUNCTION sz_stock_apply_pedido()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    v_old       text;
    v_item      record;
    v_delta     record;
    v_new_avail integer;
    v_new_res   integer;
    v_qty_fisica integer;
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
        -- Multiplicador de kit: "3 Datalaprox" -> 3. Sem número líder -> 1.
        v_qty_fisica := v_item.quantidade *
            COALESCE(NULLIF(substring(v_item.nome from '^(\d+)\s'), '')::integer, 1);

        SELECT * INTO v_delta FROM sz_stock_delta(v_old, NEW.status, v_qty_fisica);
        CONTINUE WHEN v_delta.d_available = 0 AND v_delta.d_reserved = 0;

        INSERT INTO sz_stock (product_id, cd_id, qty_available, qty_reserved)
        VALUES (v_item.produto_id, NEW.cd_id, 0, 0)
        ON CONFLICT (product_id, variation_id, cd_id) DO NOTHING;

        SELECT qty_available + v_delta.d_available,
               qty_reserved  + v_delta.d_reserved
          INTO v_new_avail, v_new_res
          FROM sz_stock
         WHERE product_id = v_item.produto_id AND variation_id = 0 AND cd_id = NEW.cd_id
         FOR UPDATE;

        IF v_new_res < 0 THEN
            RAISE WARNING '[sz_stock] reserva negativa evitada produto=% cd=% pedido=% (% -> %, calc=%)',
                v_item.produto_id, NEW.cd_id, NEW.id, v_old, NEW.status, v_new_res;
            v_new_res := 0;
        END IF;
        IF v_new_avail < 0 THEN
            RAISE WARNING '[sz_stock] disponivel negativo evitado produto=% cd=% pedido=% (% -> %, calc=%)',
                v_item.produto_id, NEW.cd_id, NEW.id, v_old, NEW.status, v_new_avail;
            v_new_avail := 0;
        END IF;

        UPDATE sz_stock
           SET qty_available = v_new_avail,
               qty_reserved  = v_new_res,
               updated_at    = now()
         WHERE product_id = v_item.produto_id AND variation_id = 0 AND cd_id = NEW.cd_id;

        INSERT INTO sz_stock_movements
            (product_id, variation_id, cd_id, order_id,
             delta_available, delta_reserved, tipo, motivo)
        VALUES
            (v_item.produto_id, 0, NEW.cd_id, NEW.wc_order_id,
             v_delta.d_available, v_delta.d_reserved,
             CASE
                 WHEN NEW.status = 'entregue'    THEN 'commit'
                 WHEN v_delta.d_reserved > 0     THEN 'reserva'
                 ELSE 'liberacao'
             END,
             format('pedido %s: %s -> %s (qty_fisica=%s)', NEW.id, v_old, NEW.status, v_qty_fisica));
    END LOOP;

    RETURN NEW;
END;
$$;

-- Reconciliação (mesma lógica de 492, agora com o multiplicador correto) —
-- corrige qty_reserved de TODAS as linhas usando a verdade recalculada. Cada
-- ajuste vira uma linha no ledger (tipo 'ajuste') pra manter a invariante
-- SUM(delta_reserved) = qty_reserved auditável (492 corrigiu a coluna direto,
-- sem ledger — quebrando essa invariante; este passe já nasce correto).
-- Tabela temp: WITH só vale pra 1 statement, e este passe precisa da mesma
-- "verdade" em 3 statements (ledger + 2 updates) dentro da mesma transação.
CREATE TEMP TABLE tmp_stock_truth AS
  SELECT i.produto_id AS product_id, mp.cd_id,
         SUM(i.quantidade * COALESCE(NULLIF(substring(i.nome from '^(\d+)\s'), '')::integer, 1))::integer AS qty_true
    FROM sz_motoboy_pedidos mp
    JOIN sz_orders o       ON COALESCE(o.wp_order_id, o.id) = mp.wc_order_id
    JOIN sz_order_items i  ON i.order_id = o.id
   WHERE mp.status IN ('agendado','aprovado','embalado','em_rota','a_caminho','reagendado')
   GROUP BY i.produto_id, mp.cd_id;

INSERT INTO sz_stock_movements (product_id, variation_id, cd_id, order_id, delta_available, delta_reserved, tipo, motivo)
SELECT s.product_id, 0, s.cd_id, NULL, 0, COALESCE(t.qty_true, 0) - s.qty_reserved, 'ajuste',
       format('AUDIT-2026-07-11: reconciliação qty_reserved (era %s, correto %s)', s.qty_reserved, COALESCE(t.qty_true, 0))
  FROM sz_stock s
  LEFT JOIN tmp_stock_truth t ON t.product_id = s.product_id AND t.cd_id = s.cd_id
 WHERE s.qty_reserved IS DISTINCT FROM COALESCE(t.qty_true, 0);

UPDATE sz_stock s
   SET qty_reserved = COALESCE(t.qty_true, 0),
       updated_at   = now()
  FROM tmp_stock_truth t
 WHERE s.product_id = t.product_id AND s.cd_id = t.cd_id AND s.variation_id = 0
   AND s.qty_reserved IS DISTINCT FROM COALESCE(t.qty_true, 0);

UPDATE sz_stock s
   SET qty_reserved = 0,
       updated_at   = now()
 WHERE s.qty_reserved <> 0
   AND NOT EXISTS (
     SELECT 1 FROM tmp_stock_truth t WHERE t.product_id = s.product_id AND t.cd_id = s.cd_id
   );

DROP TABLE tmp_stock_truth;

-- Correção retroativa de qty_available: pedidos JÁ 'entregue' com item de kit
-- (nome com número líder) deram baixa usando o trigger ANTIGO (sem
-- multiplicador) — baixaram 1 unidade por linha em vez do multiplicador real.
-- Corrige a diferença (déficit) uma única vez; entregas futuras já usam o
-- trigger corrigido acima. Registra cada ajuste no ledger (tipo 'ajuste') pra
-- manter a invariante SUM(delta_available) auditável.
DO $$
DECLARE
    v_row record;
    v_deficit integer;
BEGIN
    FOR v_row IN
        SELECT i.produto_id, mp.cd_id,
               SUM(i.quantidade * (COALESCE(NULLIF(substring(i.nome from '^(\d+)\s'), '')::integer, 1) - 1)) AS deficit
          FROM sz_order_items i
          JOIN sz_orders o ON i.order_id = o.id
          JOIN sz_motoboy_pedidos mp ON COALESCE(o.wp_order_id, o.id) = mp.wc_order_id
         WHERE mp.status = 'entregue'
           AND i.nome ~ '^\d+\s'
         GROUP BY i.produto_id, mp.cd_id
        HAVING SUM(i.quantidade * (COALESCE(NULLIF(substring(i.nome from '^(\d+)\s'), '')::integer, 1) - 1)) > 0
    LOOP
        v_deficit := v_row.deficit;

        UPDATE sz_stock
           SET qty_available = GREATEST(qty_available - v_deficit, 0),
               updated_at    = now()
         WHERE product_id = v_row.produto_id AND cd_id = v_row.cd_id AND variation_id = 0;

        IF NOT FOUND THEN
            RAISE WARNING '[sz_stock] correção de kit sem linha em sz_stock produto=% cd=% deficit=%',
                v_row.produto_id, v_row.cd_id, v_deficit;
            CONTINUE;
        END IF;

        INSERT INTO sz_stock_movements
            (product_id, variation_id, cd_id, order_id, delta_available, delta_reserved, tipo, motivo)
        VALUES
            (v_row.produto_id, 0, v_row.cd_id, NULL, -v_deficit, 0, 'ajuste',
             format('AUDIT-2026-07-11: correção retroativa baixa de kit (déficit=%s unidades)', v_deficit));
    END LOOP;
END $$;

COMMIT;
