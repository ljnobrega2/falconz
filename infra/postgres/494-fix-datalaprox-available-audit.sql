-- 494-fix-datalaprox-available-audit.sql
--
-- Correção pontual confirmada pelo dono (2026-07-11): contagem física do CD
-- Grande São Paulo hoje = 256 + 70 = 326 unidades recebidas no total. Descontando
-- as 63 unidades já entregues (SUM físico via kit-multiplier, confirmado em
-- 493-stock-kit-multiplier.sql), o disponível correto é 326 - 63 = 263.
-- qty_reserved (4) já estava correto (492/493) e NÃO muda aqui.
--
-- Produto Datalaprox (sz_products.id=2, wp_post_id=1278), CD Grande São Paulo
-- (cd_id=1). Correção pontual — não é uma regra geral, só este produto/CD.

BEGIN;

DO $$
DECLARE
    v_old_available integer;
    v_new_available integer := 263;
BEGIN
    SELECT qty_available INTO v_old_available
      FROM sz_stock WHERE product_id = 1278 AND cd_id = 1 AND variation_id = 0;

    IF v_old_available IS NULL THEN
        RAISE EXCEPTION '[494] linha sz_stock produto=1278 cd=1 não encontrada — abortando';
    END IF;

    UPDATE sz_stock
       SET qty_available = v_new_available,
           updated_at    = now()
     WHERE product_id = 1278 AND cd_id = 1 AND variation_id = 0;

    INSERT INTO sz_stock_movements
        (product_id, variation_id, cd_id, order_id, delta_available, delta_reserved, tipo, motivo)
    VALUES
        (1278, 0, 1, NULL, v_new_available - v_old_available, 0, 'ajuste',
         format('AUDIT-2026-07-11: contagem física confirmada pelo dono (256+70=326 recebidas - 63 entregues = 263). Era %s.', v_old_available));
END $$;

COMMIT;
