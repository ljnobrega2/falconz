-- Adiciona TAXA DE ENTREGA ao livro-razão de receita (pedido do dono: aparecer
-- no Faturamento). Componente 'taxa_entrega' = sz_orders.delivery_fee.
-- ref = 'order_entrega:' || sz_orders.id (distinto de 'order:' do taxa_transacao_produtor,
-- então os dois componentes coexistem para o mesmo pedido).
-- Idempotente: UNIQUE(component, ref) + ON CONFLICT DO NOTHING.

-- (1) Estende o trigger do order: além de taxa_transacao_produtor, lança taxa_entrega.
CREATE OR REPLACE FUNCTION sz_revenue_capture_order()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.transaction_fee IS NOT NULL AND NEW.transaction_fee > 0 THEN
        INSERT INTO senderzz_revenue
            (order_id, produtor_id, component, base_amount, amount, ref, created_at)
        VALUES
            (NEW.id, NEW.produtor_id, 'taxa_transacao_produtor',
             COALESCE(NEW.total, 0), NEW.transaction_fee,
             'order:' || NEW.id, COALESCE(NEW.created_at, NOW()))
        ON CONFLICT (component, ref) DO NOTHING;
    END IF;

    IF NEW.delivery_fee IS NOT NULL AND NEW.delivery_fee > 0 THEN
        INSERT INTO senderzz_revenue
            (order_id, produtor_id, component, base_amount, amount, ref, created_at)
        VALUES
            (NEW.id, NEW.produtor_id, 'taxa_entrega',
             COALESCE(NEW.total, 0), NEW.delivery_fee,
             'order_entrega:' || NEW.id, COALESCE(NEW.created_at, NOW()))
        ON CONFLICT (component, ref) DO NOTHING;
    END IF;

    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_sz_revenue_capture_order ON sz_orders;
CREATE TRIGGER trg_sz_revenue_capture_order
    AFTER INSERT OR UPDATE ON sz_orders
    FOR EACH ROW
    EXECUTE FUNCTION sz_revenue_capture_order();

-- (2) Backfill taxa_entrega dos pedidos já existentes.
INSERT INTO senderzz_revenue (order_id, produtor_id, component, base_amount, amount, ref, created_at)
SELECT o.id, o.produtor_id, 'taxa_entrega', COALESCE(o.total,0), o.delivery_fee,
       'order_entrega:' || o.id, COALESCE(o.created_at, NOW())
FROM sz_orders o
WHERE o.delivery_fee > 0
ON CONFLICT (component, ref) DO NOTHING;
