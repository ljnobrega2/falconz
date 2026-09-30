-- RECEITA REALIZADA (v2) — correção do dono 2026-06-18:
--   "taxas só caem quando pedido é COMPLETO" + "considerar frustrados".
-- senderzz_revenue passa a conter SÓ receita REALIZADA:
--   • taxa_entrega + taxa_transacao_produtor: realizam quando status ∈ (completo, entregue, frustrado).
--   • taxa_afiliado_4_99: realiza SÓ quando status ∈ (completo, entregue) — frustrado não tem comissão.
-- Pedidos em aguardando/embalado/em_rota/a_caminho/pre_agendado = PENDENTE (previsão),
-- calculado on-the-fly pelo /revenue/summary (NÃO entra no ledger).

-- ── (1) Trigger do pedido: gate por status terminal ──────────────────────────
CREATE OR REPLACE FUNCTION sz_revenue_capture_order()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    -- Taxas (entrega + transação produtor): realizam em completo/entregue/frustrado.
    IF NEW.status IN ('completo','entregue','frustrado') THEN
        IF NEW.transaction_fee IS NOT NULL AND NEW.transaction_fee > 0 THEN
            INSERT INTO senderzz_revenue (order_id, produtor_id, component, base_amount, amount, ref, created_at)
            VALUES (NEW.id, NEW.produtor_id, 'taxa_transacao_produtor', COALESCE(NEW.total,0),
                    NEW.transaction_fee, 'order:' || NEW.id, COALESCE(NEW.updated_at, NEW.created_at, NOW()))
            ON CONFLICT (component, ref) DO NOTHING;
        END IF;
        IF NEW.delivery_fee IS NOT NULL AND NEW.delivery_fee > 0 THEN
            INSERT INTO senderzz_revenue (order_id, produtor_id, component, base_amount, amount, ref, created_at)
            VALUES (NEW.id, NEW.produtor_id, 'taxa_entrega', COALESCE(NEW.total,0),
                    NEW.delivery_fee, 'order_entrega:' || NEW.id, COALESCE(NEW.updated_at, NEW.created_at, NOW()))
            ON CONFLICT (component, ref) DO NOTHING;
        END IF;
    END IF;

    -- 4,99% do afiliado: realiza só em completo/entregue (frustrado não gera comissão).
    IF NEW.status IN ('completo','entregue') THEN
        INSERT INTO senderzz_revenue (order_id, affiliate_id, component, base_amount, amount, ref, created_at)
        SELECT NEW.id, a.afiliado_id, 'taxa_afiliado_4_99', t.amount,
               ROUND((t.amount * 0.0499)::numeric, 2), 'afftx:' || t.id,
               COALESCE(NEW.updated_at, NEW.created_at, NOW())
        FROM senderzz_affiliate_transactions t
        JOIN senderzz_affiliates a ON a.id = t.affiliate_id
        WHERE t.order_id = NEW.id AND t.type = 'commission'
          AND t.status NOT IN ('cancelled','reversed') AND t.amount > 0
        ON CONFLICT (component, ref) DO NOTHING;
    END IF;

    -- Estorno: pedido cancelado/reembolsado remove a receita já lançada dele.
    IF NEW.status IN ('cancelled','reembolsado') THEN
        DELETE FROM senderzz_revenue WHERE order_id = NEW.id;
    END IF;

    RETURN NEW;
END; $$;

DROP TRIGGER IF EXISTS trg_sz_revenue_capture_order ON sz_orders;
CREATE TRIGGER trg_sz_revenue_capture_order
    AFTER INSERT OR UPDATE ON sz_orders
    FOR EACH ROW EXECUTE FUNCTION sz_revenue_capture_order();

-- ── (2) Trigger da transação de afiliado: só realiza se o pedido está completo ─
CREATE OR REPLACE FUNCTION sz_revenue_capture_afftx()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.type = 'commission' AND NEW.status NOT IN ('cancelled','reversed') AND NEW.amount > 0 THEN
        INSERT INTO senderzz_revenue (order_id, affiliate_id, component, base_amount, amount, ref, created_at)
        SELECT NEW.order_id, a.afiliado_id, 'taxa_afiliado_4_99', NEW.amount,
               ROUND((NEW.amount * 0.0499)::numeric, 2), 'afftx:' || NEW.id, NOW()
        FROM senderzz_affiliates a
        WHERE a.id = NEW.affiliate_id
          AND EXISTS (SELECT 1 FROM sz_orders o WHERE o.id = NEW.order_id AND o.status IN ('completo','entregue'))
        ON CONFLICT (component, ref) DO NOTHING;
    END IF;
    IF NEW.status IN ('cancelled','reversed') THEN
        DELETE FROM senderzz_revenue WHERE component = 'taxa_afiliado_4_99' AND ref = 'afftx:' || NEW.id;
    END IF;
    RETURN NEW;
END; $$;

DROP TRIGGER IF EXISTS trg_sz_revenue_capture_afftx ON senderzz_affiliate_transactions;
CREATE TRIGGER trg_sz_revenue_capture_afftx
    AFTER INSERT OR UPDATE ON senderzz_affiliate_transactions
    FOR EACH ROW EXECUTE FUNCTION sz_revenue_capture_afftx();

-- ── (3) Rebuild do ledger: REALIZADO apenas ──────────────────────────────────
DELETE FROM senderzz_revenue;

INSERT INTO senderzz_revenue (order_id, produtor_id, component, base_amount, amount, ref, created_at)
SELECT o.id, o.produtor_id, 'taxa_transacao_produtor', COALESCE(o.total,0), o.transaction_fee,
       'order:' || o.id, COALESCE(o.created_at, NOW())
FROM sz_orders o
WHERE o.status IN ('completo','entregue','frustrado') AND o.transaction_fee > 0
ON CONFLICT (component, ref) DO NOTHING;

INSERT INTO senderzz_revenue (order_id, produtor_id, component, base_amount, amount, ref, created_at)
SELECT o.id, o.produtor_id, 'taxa_entrega', COALESCE(o.total,0), o.delivery_fee,
       'order_entrega:' || o.id, COALESCE(o.created_at, NOW())
FROM sz_orders o
WHERE o.status IN ('completo','entregue','frustrado') AND o.delivery_fee > 0
ON CONFLICT (component, ref) DO NOTHING;

INSERT INTO senderzz_revenue (order_id, affiliate_id, component, base_amount, amount, ref, created_at)
SELECT t.order_id, a.afiliado_id, 'taxa_afiliado_4_99', t.amount, ROUND((t.amount*0.0499)::numeric,2),
       'afftx:' || t.id, COALESCE(t.created_at, NOW())
FROM senderzz_affiliate_transactions t
JOIN senderzz_affiliates a ON a.id = t.affiliate_id
JOIN sz_orders o ON o.id = t.order_id
WHERE o.status IN ('completo','entregue')
  AND t.type = 'commission' AND t.status NOT IN ('cancelled','reversed') AND t.amount > 0
ON CONFLICT (component, ref) DO NOTHING;
