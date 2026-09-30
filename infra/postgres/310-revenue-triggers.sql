-- =============================================================================
-- CAPTURA CONTÍNUA DE RECEITA FALKZ — TRIGGERS Postgres (forward-capture)
-- =============================================================================
-- Motivação: o backfill em schema-revenue.sql é ONE-SHOT — popula a receita já
-- existente, mas nada lança a receita de transações NOVAS (afiliado/produtor)
-- criadas DEPOIS do backfill. Estes triggers fazem a captura contínua: cada
-- INSERT/UPDATE relevante em sz_orders / senderzz_affiliate_transactions lança
-- (ou estorna) o lançamento correspondente em senderzz_revenue.
--
-- Componentes de receita (idênticos ao backfill — mesma fórmula, mesmo ref):
--   - taxa_transacao_produtor = sz_orders.transaction_fee
--                               ref = 'order:'  || sz_orders.id
--   - taxa_afiliado_4_99      = comissão do afiliado × 4,99% (só comissão normal;
--                               cancelado/revertido NÃO geram, e ESTORNAM se já
--                               lançado)
--                               ref = 'afftx:'  || senderzz_affiliate_transactions.id
--
-- Idempotência: UNIQUE(component, ref) em senderzz_revenue + ON CONFLICT DO NOTHING.
--   - O ref ancora na PK (imutável) → estável mesmo que outras colunas mudem.
--   - Primeira escrita vence: UPDATEs posteriores caem no ON CONFLICT DO NOTHING
--     (não re-apontam nem re-precificam o lançamento) — coerente com o backfill.
--
-- Re-runnável (convenção do repo): CREATE OR REPLACE FUNCTION + DROP TRIGGER IF
-- EXISTS. Aplicar com:  psql "$URL" -f schema-revenue-triggers.sql
-- Pré-requisito: senderzz_revenue já criada (schema-revenue.sql).
-- =============================================================================


-- ---------------------------------------------------------------------------
-- (1) sz_orders → taxa_transacao_produtor
--     AFTER INSERT OR UPDATE: se transaction_fee > 0, lança a taxa de transação
--     do produtor. SEM reversão (por spec — taxa de transação não estorna aqui).
--     NULL-safe: transaction_fee NULL → comparação NULL → false → não lança.
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION sz_revenue_capture_order()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.transaction_fee IS NOT NULL AND NEW.transaction_fee > 0 THEN
        INSERT INTO senderzz_revenue
            (order_id, produtor_id, component, base_amount, amount, ref, created_at)
        VALUES
            (NEW.id,
             NEW.produtor_id,
             'taxa_transacao_produtor',
             COALESCE(NEW.total, 0),
             NEW.transaction_fee,
             'order:' || NEW.id,
             COALESCE(NEW.created_at, NOW()))
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


-- ---------------------------------------------------------------------------
-- (2) senderzz_affiliate_transactions → taxa_afiliado_4_99
--     AFTER INSERT OR UPDATE:
--       - comissão válida (type='commission', status fora de cancelled/reversed,
--         amount>0) → LANÇA 4,99% sobre a comissão. Afiliado resolvido via
--         JOIN senderzz_affiliates (INSERT…SELECT auto-pula se não houver match,
--         espelhando o INNER JOIN do backfill).
--       - status cancelled/reversed → ESTORNA: DELETE do lançamento (ref=afftx:id).
--         DELETE-antes funciona com o UNIQUE: limpa a linha; se a tx voltar a um
--         status válido depois, o branch de lançamento re-booka.
--     NULL-safe: amount NULL → comparação NULL → false → não lança.
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION sz_revenue_capture_afftx()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.status IN ('cancelled', 'reversed') THEN
        -- REVERSÃO: estorna receita de comissão cancelada/revertida.
        DELETE FROM senderzz_revenue
        WHERE component = 'taxa_afiliado_4_99'
          AND ref = 'afftx:' || NEW.id;

    ELSIF NEW.type = 'commission'
          AND NEW.amount IS NOT NULL
          AND NEW.amount > 0 THEN
        -- LANÇAMENTO: 4,99% sobre a comissão. Afiliado via JOIN; sem match → no-op.
        INSERT INTO senderzz_revenue
            (order_id, affiliate_id, component, base_amount, amount, ref, created_at)
        SELECT NEW.order_id,
               a.afiliado_id,
               'taxa_afiliado_4_99',
               NEW.amount,
               ROUND((NEW.amount * 0.0499)::numeric, 2),
               'afftx:' || NEW.id,
               COALESCE(NEW.created_at, NOW())
        FROM senderzz_affiliates a
        WHERE a.id = NEW.affiliate_id
        ON CONFLICT (component, ref) DO NOTHING;
    END IF;

    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_sz_revenue_capture_afftx ON senderzz_affiliate_transactions;
CREATE TRIGGER trg_sz_revenue_capture_afftx
    AFTER INSERT OR UPDATE ON senderzz_affiliate_transactions
    FOR EACH ROW
    EXECUTE FUNCTION sz_revenue_capture_afftx();
