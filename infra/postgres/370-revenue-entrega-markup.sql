-- RECEITA v6 — CORREÇÃO da TAXA DE ENTREGA (pedido do DONO, confirmado 2026-06-19):
--   ERRADO (v5/360): bookava delivery_fee INTEIRO como 'taxa_entrega'. Mas o
--   delivery_fee (≈ R$24-25) é o que se COBRA do cliente; ao motoboy REPASSA-SE
--   ~R$18. A receita real da FALKZ é só o MARKUP = delivery_fee − repasse (≈ R$6),
--   não a taxa cheia. Bookar a taxa cheia inflava o faturamento de entrega.
--
--   CORRETO (v6/370): taxa_entrega = GREATEST(delivery_fee − repasse, 0).
--   O repasse é CONFIGURÁVEL via senderzz_options.motoboy_repasse_padrao
--   (default 18). GREATEST(...,0) protege contra fretes < repasse (markup negativo
--   nunca vira "receita negativa").
--
-- Este arquivo:
--   (a) garante a option motoboy_repasse_padrao = '18' (sem sobrescrever);
--   (b) CREATE OR REPLACE da função do trigger — carrega VERBATIM os 3 demais
--       componentes do v5 (taxa_afiliado_4_99, taxa_transacao_produtor e o
--       estorno cancelled/reembolsado). MUDA APENAS a expressão de amount do
--       componente taxa_entrega. NÃO toca em nenhum outro componente de receita;
--   (c) BACKFILL idempotente: re-deriva amount dos taxa_entrega já gravados a
--       partir de o.delivery_fee − repasse (não do amount atual) → 2ª passada
--       é no-op. Só toca linhas cujo ref casa com um sz_orders existente
--       (órfãos sem pedido correspondente ficam intocados).
--
-- Idempotente: option ON CONFLICT DO NOTHING; trigger CREATE OR REPLACE;
-- backfill UPDATE determinístico (deriva de delivery_fee − repasse).

-- (a) Option do repasse padrão ao motoboy (R$ por entrega). NÃO sobrescreve.
INSERT INTO senderzz_options (name, value, autoload)
VALUES ('motoboy_repasse_padrao', '18', 'yes')
ON CONFLICT (name) DO NOTHING;

-- (b) Trigger do pedido (v6): taxa_entrega = MARKUP. Demais componentes idênticos ao v5.
CREATE OR REPLACE FUNCTION sz_revenue_capture_order()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    prod_pct numeric;
    repasse  numeric;
BEGIN
    -- Taxa de ENTREGA = MARKUP (delivery_fee − repasse), não a taxa cheia.
    -- Realiza em completo/entregue/frustrado (igual ao v5).
    IF NEW.status IN ('completo','entregue','frustrado') AND COALESCE(NEW.delivery_fee,0) > 0 THEN
        repasse := COALESCE((SELECT NULLIF(value,'')::numeric FROM senderzz_options WHERE name='motoboy_repasse_padrao'), 18);
        INSERT INTO senderzz_revenue (order_id, produtor_id, component, base_amount, amount, ref, created_at)
        VALUES (NEW.id, NEW.produtor_id, 'taxa_entrega', COALESCE(NEW.total,0),
                GREATEST(NEW.delivery_fee - repasse, 0),
                'order_entrega:' || NEW.id, COALESCE(NEW.updated_at, NEW.created_at, NOW()))
        ON CONFLICT (component, ref) DO NOTHING;
    END IF;

    IF NEW.status IN ('completo','entregue') THEN
        -- 4,99% do afiliado (transaction_fee real). [VERBATIM v5 — NÃO ALTERAR]
        IF COALESCE(NEW.transaction_fee,0) > 0 THEN
            INSERT INTO senderzz_revenue (order_id, affiliate_id, component, base_amount, amount, ref, created_at)
            VALUES (NEW.id, NEW.affiliate_id, 'taxa_afiliado_4_99',
                    COALESCE(NEW.affiliate_amount,0) + NEW.transaction_fee, NEW.transaction_fee,
                    'order_aff:' || NEW.id, COALESCE(NEW.updated_at, NEW.created_at, NOW()))
            ON CONFLICT (component, ref) DO NOTHING;
        END IF;
        -- Taxa de transação do PRODUTOR = total × pct configurável. [VERBATIM v5 — NÃO ALTERAR]
        IF COALESCE(NEW.total,0) > 0 THEN
            prod_pct := COALESCE((SELECT NULLIF(value,'')::numeric FROM senderzz_options WHERE name='sz_producer_transaction_fee_pct'), 4.99);
            INSERT INTO senderzz_revenue (order_id, produtor_id, component, base_amount, amount, ref, created_at)
            VALUES (NEW.id, NEW.produtor_id, 'taxa_transacao_produtor', NEW.total,
                    ROUND((NEW.total * prod_pct / 100)::numeric, 2),
                    'order_prodtx:' || NEW.id, COALESCE(NEW.updated_at, NEW.created_at, NOW()))
            ON CONFLICT (component, ref) DO NOTHING;
        END IF;
    END IF;

    -- Estorno. [VERBATIM v5 — NÃO ALTERAR]
    IF NEW.status IN ('cancelled','reembolsado') THEN
        DELETE FROM senderzz_revenue WHERE order_id = NEW.id;
    END IF;
    RETURN NEW;
END; $$;

DROP TRIGGER IF EXISTS trg_sz_revenue_capture_order ON sz_orders;
CREATE TRIGGER trg_sz_revenue_capture_order
    AFTER INSERT OR UPDATE ON sz_orders
    FOR EACH ROW EXECUTE FUNCTION sz_revenue_capture_order();

-- (c) BACKFILL idempotente: corrige taxa_entrega já gravado p/ markup.
--     Deriva de o.delivery_fee − repasse (literal 18 = default da option) — NÃO
--     do amount atual → 2ª passada não muda nada. Só linhas com pedido existente.
UPDATE senderzz_revenue r
   SET amount = GREATEST(o.delivery_fee - 18, 0)
  FROM sz_orders o
 WHERE r.component = 'taxa_entrega'
   AND r.ref = 'order_entrega:' || o.id;
