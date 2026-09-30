-- RECEITA v3 (pedido do dono 2026-06-18): além de taxa_entrega / taxa_transacao_produtor
-- / taxa_afiliado_4_99, o faturamento passa a incluir:
--   • taxa_frustrado    = taxa de pedido frustrado (senderzz_affiliate_transactions type='penalty').
--   • taxa_saque        = fee de saque (afiliado + COD produtor), realizado em approved/paid.
--   • taxa_antecipacao  = fee de antecipação COD (sz_cod_wallet_transactions type='fee').
-- Idempotência: UNIQUE(component, ref). refs com prefixo distinto por fonte.

-- ── (A) FRUSTRADO: estende o trigger de affiliate_transactions ───────────────
CREATE OR REPLACE FUNCTION sz_revenue_capture_afftx()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    -- comissão 4,99% (só realiza se o pedido vinculado está completo/entregue)
    IF NEW.type = 'commission' AND NEW.status NOT IN ('cancelled','reversed') AND NEW.amount > 0 THEN
        INSERT INTO senderzz_revenue (order_id, affiliate_id, component, base_amount, amount, ref, created_at)
        SELECT NEW.order_id, a.afiliado_id, 'taxa_afiliado_4_99', NEW.amount,
               ROUND((NEW.amount * 0.0499)::numeric, 2), 'afftx:' || NEW.id, NOW()
        FROM senderzz_affiliates a
        WHERE a.id = NEW.affiliate_id
          AND EXISTS (SELECT 1 FROM sz_orders o WHERE o.id = NEW.order_id AND o.status IN ('completo','entregue'))
        ON CONFLICT (component, ref) DO NOTHING;
    END IF;
    -- taxa de frustrado (penalty) = receita FALKZ
    IF NEW.type = 'penalty' AND NEW.status NOT IN ('cancelled','reversed') AND NEW.amount > 0 THEN
        INSERT INTO senderzz_revenue (order_id, affiliate_id, component, base_amount, amount, ref, created_at)
        SELECT NEW.order_id, a.afiliado_id, 'taxa_frustrado', NEW.amount, NEW.amount, 'penalty:' || NEW.id, NOW()
        FROM senderzz_affiliates a WHERE a.id = NEW.affiliate_id
        ON CONFLICT (component, ref) DO NOTHING;
    END IF;
    -- estorno
    IF NEW.status IN ('cancelled','reversed') THEN
        DELETE FROM senderzz_revenue WHERE ref IN ('afftx:' || NEW.id, 'penalty:' || NEW.id);
    END IF;
    RETURN NEW;
END; $$;

DROP TRIGGER IF EXISTS trg_sz_revenue_capture_afftx ON senderzz_affiliate_transactions;
CREATE TRIGGER trg_sz_revenue_capture_afftx
    AFTER INSERT OR UPDATE ON senderzz_affiliate_transactions
    FOR EACH ROW EXECUTE FUNCTION sz_revenue_capture_afftx();

-- ── (B) SAQUE afiliado: fee → taxa_saque ─────────────────────────────────────
CREATE OR REPLACE FUNCTION sz_revenue_capture_affsaque()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status IN ('approved','paid') AND COALESCE(NEW.fee,0) > 0 THEN
        INSERT INTO senderzz_revenue (affiliate_id, component, base_amount, amount, ref, created_at)
        SELECT a.afiliado_id, 'taxa_saque', NEW.amount, NEW.fee, 'affsaque:' || NEW.id, NOW()
        FROM senderzz_affiliates a WHERE a.id = NEW.affiliate_id
        ON CONFLICT (component, ref) DO NOTHING;
    END IF;
    RETURN NEW;
END; $$;

DROP TRIGGER IF EXISTS trg_sz_revenue_capture_affsaque ON senderzz_affiliate_withdrawals;
CREATE TRIGGER trg_sz_revenue_capture_affsaque
    AFTER INSERT OR UPDATE ON senderzz_affiliate_withdrawals
    FOR EACH ROW EXECUTE FUNCTION sz_revenue_capture_affsaque();

-- ── (C) SAQUE COD produtor: fee → taxa_saque ─────────────────────────────────
CREATE OR REPLACE FUNCTION sz_revenue_capture_codsaque()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status IN ('approved','paid') AND COALESCE(NEW.fee,0) > 0 THEN
        INSERT INTO senderzz_revenue (component, base_amount, amount, ref, created_at)
        VALUES ('taxa_saque', NEW.amount, NEW.fee, 'codsaque:' || NEW.id, NOW())
        ON CONFLICT (component, ref) DO NOTHING;
    END IF;
    RETURN NEW;
END; $$;

DROP TRIGGER IF EXISTS trg_sz_revenue_capture_codsaque ON sz_cod_withdrawals;
CREATE TRIGGER trg_sz_revenue_capture_codsaque
    AFTER INSERT OR UPDATE ON sz_cod_withdrawals
    FOR EACH ROW EXECUTE FUNCTION sz_revenue_capture_codsaque();

-- ── (D) ANTECIPAÇÃO COD: fee → taxa_antecipacao ──────────────────────────────
CREATE OR REPLACE FUNCTION sz_revenue_capture_codfee()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.type = 'fee' AND NEW.status NOT IN ('reversed') AND COALESCE(NEW.fee, NEW.gross, 0) > 0 THEN
        INSERT INTO senderzz_revenue (component, base_amount, amount, ref, created_at)
        VALUES ('taxa_antecipacao', COALESCE(NEW.gross,0), COALESCE(NULLIF(NEW.fee,0), NEW.gross), 'codfee:' || NEW.id, NOW())
        ON CONFLICT (component, ref) DO NOTHING;
    END IF;
    RETURN NEW;
END; $$;

DROP TRIGGER IF EXISTS trg_sz_revenue_capture_codfee ON sz_cod_wallet_transactions;
CREATE TRIGGER trg_sz_revenue_capture_codfee
    AFTER INSERT OR UPDATE ON sz_cod_wallet_transactions
    FOR EACH ROW EXECUTE FUNCTION sz_revenue_capture_codfee();

-- ── BACKFILL dos 3 componentes (idempotente) ─────────────────────────────────
INSERT INTO senderzz_revenue (order_id, affiliate_id, component, base_amount, amount, ref, created_at)
SELECT t.order_id, a.afiliado_id, 'taxa_frustrado', t.amount, t.amount, 'penalty:' || t.id, COALESCE(t.created_at, NOW())
FROM senderzz_affiliate_transactions t JOIN senderzz_affiliates a ON a.id = t.affiliate_id
WHERE t.type = 'penalty' AND t.status NOT IN ('cancelled','reversed') AND t.amount > 0
ON CONFLICT (component, ref) DO NOTHING;

INSERT INTO senderzz_revenue (affiliate_id, component, base_amount, amount, ref, created_at)
SELECT a.afiliado_id, 'taxa_saque', w.amount, w.fee, 'affsaque:' || w.id, COALESCE(w.requested_at, NOW())
FROM senderzz_affiliate_withdrawals w JOIN senderzz_affiliates a ON a.id = w.affiliate_id
WHERE w.status IN ('approved','paid') AND COALESCE(w.fee,0) > 0
ON CONFLICT (component, ref) DO NOTHING;

INSERT INTO senderzz_revenue (component, base_amount, amount, ref, created_at)
SELECT 'taxa_saque', amount, fee, 'codsaque:' || id, NOW()
FROM sz_cod_withdrawals
WHERE status IN ('approved','paid') AND COALESCE(fee,0) > 0
ON CONFLICT (component, ref) DO NOTHING;

INSERT INTO senderzz_revenue (component, base_amount, amount, ref, created_at)
SELECT 'taxa_antecipacao', COALESCE(gross,0), COALESCE(NULLIF(fee,0), gross), 'codfee:' || id, COALESCE(created_at, NOW())
FROM sz_cod_wallet_transactions
WHERE type = 'fee' AND status NOT IN ('reversed') AND COALESCE(fee, gross, 0) > 0
ON CONFLICT (component, ref) DO NOTHING;
