-- AUDIT-2026-07-31 CRITICAL — achado investigando a divergência da
-- reconciliação (sz_revenue_reconciliacao) após aplicar 521 em produção real.
--
-- BUG (pré-existente, presente desde a criação do trigger direto em produção,
-- preservado sem querer na reescrita fiel de 521): sz_revenue_capture_order
-- grava o componente 'taxa_entrega' com o valor CHEIO de NEW.delivery_fee
-- (ex.: 23,98), quando a regra documentada em TODO lugar (390-blindagem-
-- financeira.sql, 370-revenue-entrega-markup.sql, sz_revenue_reconciliacao)
-- é: taxa_entrega = MARKUP = GREATEST(delivery_fee - repasse, 0) (ex.:
-- 23,98-18=5,98). O motoboy fica com o repasse (18,00); só o markup (5,98) é
-- receita da PLATAFORMA. Confirmado: TODOS os 37 lançamentos de
-- taxa_entrega no ledger (incluindo o golden #1587) gravaram 23,98 em vez de
-- 5,98 — receita da plataforma superestimada em ~18,00 por pedido, sempre.
--
-- ESCOPO: só corrige o LEDGER da plataforma (senderzz_revenue) — é o livro
-- de receita da EMPRESA, não carteira de produtor/motoboy/afiliado. O
-- produtor/motoboy nunca foi cobrado a mais por causa disso (delivery_fee
-- continua sendo debitado normalmente do cliente/produtor via sz_orders,
-- isso não muda) — o bug é só no quanto a PLATAFORMA registrava como sua
-- própria receita.

-- ── (1) Fix do trigger: grava o markup, não o valor cheio ───────────────────
CREATE OR REPLACE FUNCTION sz_revenue_capture_order()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    prod_pct numeric;
    is_motoboy boolean;
    repasse numeric;
BEGIN
    is_motoboy := EXISTS (SELECT 1 FROM sz_motoboy_pedidos m WHERE m.wc_order_id = NEW.wp_order_id);

    IF NEW.status IN ('completo','entregue','frustrado') AND COALESCE(NEW.delivery_fee,0) > 0 THEN
        SELECT COALESCE((SELECT NULLIF(value,'')::numeric FROM senderzz_options
                          WHERE name='motoboy_repasse_padrao'), 18) INTO repasse;
        INSERT INTO senderzz_revenue (order_id, produtor_id, component, base_amount, amount, ref, created_at)
        VALUES (NEW.id, NEW.produtor_id, 'taxa_entrega', COALESCE(NEW.total,0),
                GREATEST(NEW.delivery_fee - repasse, 0),
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
        IF is_motoboy AND COALESCE(NEW.total,0) > 0 THEN
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

-- ── (2) Backfill: corrige os lançamentos JÁ gravados errado ─────────────────
-- Só ajusta 'amount' (a coluna que representa a receita reconhecida da
-- plataforma) — não mexe em order_id/produtor_id/ref (identidade do
-- lançamento preservada, é o mesmo evento, só o valor estava errado).
UPDATE senderzz_revenue r
   SET amount = GREATEST(o.delivery_fee - COALESCE(
                   (SELECT NULLIF(value,'')::numeric FROM senderzz_options WHERE name='motoboy_repasse_padrao'), 18
                 ), 0)
  FROM sz_orders o
 WHERE r.order_id = o.id
   AND r.component = 'taxa_entrega'
   AND r.amount <> GREATEST(o.delivery_fee - COALESCE(
                     (SELECT NULLIF(value,'')::numeric FROM senderzz_options WHERE name='motoboy_repasse_padrao'), 18
                   ), 0);
