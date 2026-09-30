-- Taxa de transação do produtor — override POR PRODUTOR (pedido do dono 2026-07-17):
--   até aqui sz_producer_transaction_fee_pct era ÚNICA global (todo produtor pagava a
--   mesma taxa). Agora aceita override individual em senderzz_options.sz_producer_fee_rules
--   ({"<produtor_id>": pct, ...}, JSON). Sem override → cai no global (4.99% default),
--   idêntico ao comportamento anterior — migração 100% retrocompatível.

INSERT INTO senderzz_options (name, value, autoload)
VALUES ('sz_producer_fee_rules', '{}', 'yes')
ON CONFLICT (name) DO NOTHING;

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
        IF COALESCE(NEW.transaction_fee,0) > 0 THEN
            INSERT INTO senderzz_revenue (order_id, affiliate_id, component, base_amount, amount, ref, created_at)
            VALUES (NEW.id, NEW.affiliate_id, 'taxa_afiliado_4_99',
                    COALESCE(NEW.affiliate_amount,0) + NEW.transaction_fee, NEW.transaction_fee,
                    'order_aff:' || NEW.id, COALESCE(NEW.updated_at, NEW.created_at, NOW()))
            ON CONFLICT (component, ref) DO NOTHING;
        END IF;
        -- Taxa de transação do PRODUTOR: override individual (sz_producer_fee_rules[produtor_id])
        -- com fallback pro global (sz_producer_transaction_fee_pct, default 4.99).
        IF COALESCE(NEW.total,0) > 0 THEN
            prod_pct := COALESCE(
                (SELECT (NULLIF(value,'')::jsonb ->> NEW.produtor_id::text)::numeric
                   FROM senderzz_options WHERE name='sz_producer_fee_rules'),
                (SELECT NULLIF(value,'')::numeric FROM senderzz_options WHERE name='sz_producer_transaction_fee_pct'),
                4.99
            );
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
