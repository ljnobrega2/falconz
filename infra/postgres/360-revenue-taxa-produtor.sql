-- RECEITA v5 — re-adiciona TAXA DE TRANSAÇÃO DO PRODUTOR (pedido do dono):
--   taxa de transação do produtor = valor_total × pct, CONFIGURÁVEL via menu.
--   Distinta da taxa do afiliado (transaction_fee = 4,99% da comissão). Esta incide
--   sobre o VALOR BRUTO (total) cobrado do produtor. Realiza em completo/entregue.
--   Rate em senderzz_options.sz_producer_transaction_fee_pct (default 4.99).

-- Option configurável (não sobrescreve se já existir).
INSERT INTO senderzz_options (name, value, autoload)
VALUES ('sz_producer_transaction_fee_pct', '4.99', 'yes')
ON CONFLICT (name) DO NOTHING;

-- Trigger do pedido (v5): taxa_entrega + taxa_afiliado_4_99 + taxa_transacao_produtor.
CREATE OR REPLACE FUNCTION sz_revenue_capture_order()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    prod_pct numeric;
BEGIN
    IF NEW.status IN ('completo','entregue','frustrado') AND COALESCE(NEW.delivery_fee,0) > 0 THEN
        INSERT INTO senderzz_revenue (order_id, produtor_id, component, base_amount, amount, ref, created_at)
        VALUES (NEW.id, NEW.produtor_id, 'taxa_entrega', COALESCE(NEW.total,0), NEW.delivery_fee,
                'order_entrega:' || NEW.id, COALESCE(NEW.updated_at, NEW.created_at, NOW()))
        ON CONFLICT (component, ref) DO NOTHING;
    END IF;

    IF NEW.status IN ('completo','entregue') THEN
        -- 4,99% do afiliado (transaction_fee real).
        IF COALESCE(NEW.transaction_fee,0) > 0 THEN
            INSERT INTO senderzz_revenue (order_id, affiliate_id, component, base_amount, amount, ref, created_at)
            VALUES (NEW.id, NEW.affiliate_id, 'taxa_afiliado_4_99',
                    COALESCE(NEW.affiliate_amount,0) + NEW.transaction_fee, NEW.transaction_fee,
                    'order_aff:' || NEW.id, COALESCE(NEW.updated_at, NEW.created_at, NOW()))
            ON CONFLICT (component, ref) DO NOTHING;
        END IF;
        -- Taxa de transação do PRODUTOR = total × pct configurável.
        IF COALESCE(NEW.total,0) > 0 THEN
            prod_pct := COALESCE((SELECT NULLIF(value,'')::numeric FROM senderzz_options WHERE name='sz_producer_transaction_fee_pct'), 4.99);
            INSERT INTO senderzz_revenue (order_id, produtor_id, component, base_amount, amount, ref, created_at)
            VALUES (NEW.id, NEW.produtor_id, 'taxa_transacao_produtor', NEW.total,
                    ROUND((NEW.total * prod_pct / 100)::numeric, 2),
                    'order_prodtx:' || NEW.id, COALESCE(NEW.updated_at, NEW.created_at, NOW()))
            ON CONFLICT (component, ref) DO NOTHING;
        END IF;
    END IF;

    IF NEW.status IN ('cancelled','reembolsado') THEN
        DELETE FROM senderzz_revenue WHERE order_id = NEW.id;
    END IF;
    RETURN NEW;
END; $$;

DROP TRIGGER IF EXISTS trg_sz_revenue_capture_order ON sz_orders;
CREATE TRIGGER trg_sz_revenue_capture_order
    AFTER INSERT OR UPDATE ON sz_orders
    FOR EACH ROW EXECUTE FUNCTION sz_revenue_capture_order();

-- Backfill taxa_transacao_produtor (completo/entregue).
INSERT INTO senderzz_revenue (order_id, produtor_id, component, base_amount, amount, ref, created_at)
SELECT o.id, o.produtor_id, 'taxa_transacao_produtor', COALESCE(o.total,0),
       ROUND((COALESCE(o.total,0) * COALESCE((SELECT NULLIF(value,'')::numeric FROM senderzz_options WHERE name='sz_producer_transaction_fee_pct'), 4.99) / 100)::numeric, 2),
       'order_prodtx:' || o.id, COALESCE(o.created_at, NOW())
FROM sz_orders o
WHERE o.status IN ('completo','entregue') AND COALESCE(o.total,0) > 0
ON CONFLICT (component, ref) DO NOTHING;
