-- RECEITA v4 — CORREÇÃO do 4,99% (dados reais 2026-06-18):
--   sz_orders.affiliate_amount = comissão LÍQUIDA (já net) = affiliate_transactions.amount.
--   sz_orders.transaction_fee  = a 4,99% REAL do afiliado (take FALKZ). bruta = amount + transaction_fee = total×pct.
--   NÃO existe "taxa transação produtor" separada — era o transaction_fee mal-rotulado.
-- Erro v3: bookava taxa_transacao_produtor (=transaction_fee) E taxa_afiliado_4_99 (=amount×0,0499)
--   → MESMO 4,99% contado 2×. Correto: UM componente taxa_afiliado_4_99 = transaction_fee.

-- ── Trigger do pedido (v4): taxa_entrega + taxa_afiliado_4_99(=transaction_fee). SEM taxa_transacao_produtor.
CREATE OR REPLACE FUNCTION sz_revenue_capture_order()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    -- Taxa de entrega: realiza em completo/entregue/frustrado.
    IF NEW.status IN ('completo','entregue','frustrado') AND COALESCE(NEW.delivery_fee,0) > 0 THEN
        INSERT INTO senderzz_revenue (order_id, produtor_id, component, base_amount, amount, ref, created_at)
        VALUES (NEW.id, NEW.produtor_id, 'taxa_entrega', COALESCE(NEW.total,0), NEW.delivery_fee,
                'order_entrega:' || NEW.id, COALESCE(NEW.updated_at, NEW.created_at, NOW()))
        ON CONFLICT (component, ref) DO NOTHING;
    END IF;
    -- 4,99% do afiliado = transaction_fee (valor REAL armazenado). Só completo/entregue.
    IF NEW.status IN ('completo','entregue') AND COALESCE(NEW.transaction_fee,0) > 0 THEN
        INSERT INTO senderzz_revenue (order_id, affiliate_id, component, base_amount, amount, ref, created_at)
        VALUES (NEW.id, NEW.affiliate_id, 'taxa_afiliado_4_99',
                COALESCE(NEW.affiliate_amount,0) + NEW.transaction_fee, NEW.transaction_fee,
                'order_aff:' || NEW.id, COALESCE(NEW.updated_at, NEW.created_at, NOW()))
        ON CONFLICT (component, ref) DO NOTHING;
    END IF;
    -- Estorno.
    IF NEW.status IN ('cancelled','reembolsado') THEN
        DELETE FROM senderzz_revenue WHERE order_id = NEW.id;
    END IF;
    RETURN NEW;
END; $$;

DROP TRIGGER IF EXISTS trg_sz_revenue_capture_order ON sz_orders;
CREATE TRIGGER trg_sz_revenue_capture_order
    AFTER INSERT OR UPDATE ON sz_orders
    FOR EACH ROW EXECUTE FUNCTION sz_revenue_capture_order();

-- ── Trigger afftx (v4): só taxa_frustrado (penalty). REMOVE a comissão 4,99% (era recompute errado).
CREATE OR REPLACE FUNCTION sz_revenue_capture_afftx()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.type = 'penalty' AND NEW.status NOT IN ('cancelled','reversed') AND NEW.amount > 0 THEN
        INSERT INTO senderzz_revenue (order_id, affiliate_id, component, base_amount, amount, ref, created_at)
        SELECT NEW.order_id, a.afiliado_id, 'taxa_frustrado', NEW.amount, NEW.amount, 'penalty:' || NEW.id, NOW()
        FROM senderzz_affiliates a WHERE a.id = NEW.affiliate_id
        ON CONFLICT (component, ref) DO NOTHING;
    END IF;
    IF NEW.status IN ('cancelled','reversed') THEN
        DELETE FROM senderzz_revenue WHERE ref IN ('afftx:' || NEW.id, 'penalty:' || NEW.id);
    END IF;
    RETURN NEW;
END; $$;

DROP TRIGGER IF EXISTS trg_sz_revenue_capture_afftx ON senderzz_affiliate_transactions;
CREATE TRIGGER trg_sz_revenue_capture_afftx
    AFTER INSERT OR UPDATE ON senderzz_affiliate_transactions
    FOR EACH ROW EXECUTE FUNCTION sz_revenue_capture_afftx();

-- ── Rebuild: remove a dupla contagem do 4,99% e re-deriva correto ─────────────
DELETE FROM senderzz_revenue WHERE component IN ('taxa_transacao_produtor', 'taxa_afiliado_4_99');

INSERT INTO senderzz_revenue (order_id, affiliate_id, component, base_amount, amount, ref, created_at)
SELECT o.id, o.affiliate_id, 'taxa_afiliado_4_99',
       COALESCE(o.affiliate_amount,0) + o.transaction_fee, o.transaction_fee,
       'order_aff:' || o.id, COALESCE(o.created_at, NOW())
FROM sz_orders o
WHERE o.status IN ('completo','entregue') AND COALESCE(o.transaction_fee,0) > 0
ON CONFLICT (component, ref) DO NOTHING;
