-- =============================================================================
-- Senderzz / FALK — SAQUE + ANTECIPAÇÃO da COMISSÃO do AFILIADO
--   (espelho do fluxo COD do produtor, agora para o ledger de afiliado)
--
-- CONTEXTO: hoje só o PRODUTOR COD saca/antecipa (sz_cod_*). O AFILIADO tem
--   comissão em senderzz_affiliate_transactions (ledger) + cache em
--   senderzz_affiliate_wallet, mas não tinha self-service de saque/antecipação.
--   Esta migração SEMEIA a governança (options/flags) e ALINHA o schema das
--   tabelas de afiliado para os dois handlers Go novos:
--     - WithdrawAffiliate   (POST /portal/affiliate-wallet/withdraw)
--     - AnticipateAffiliate (POST /portal/affiliate-wallet/anticipate)
--
-- TUDO IDEMPOTENTE (pode reexecutar). NÃO toca dinheiro de ninguém: só DDL
--   aditivo + seed de options + relaxamento aditivo de um CHECK + extensão
--   aditiva de um trigger de captura de receita.
--
-- Rodar manualmente no Postgres do VPS (o orquestrador aplica). SEM deploy.
-- =============================================================================

-- ── 1. Governança (senderzz_options) ────────────────────────────────────────
-- ON CONFLICT DO NOTHING (igual 429-aff-per-affiliate-override): se o dono já
-- tiver ajustado um valor antes desta migração, NÃO sobrescreve a escolha dele.
-- autoload 'yes' p/ o WP autoloadar (paridade com as demais options de afiliado).

-- Valor mínimo de saque do afiliado (R$). Espelha o R$10 do COD (sz_cod default).
INSERT INTO senderzz_options (name, value, autoload)
VALUES ('sz_aff_withdraw_min', '10.00', 'yes')
ON CONFLICT (name) DO NOTHING;

-- Taxa FIXA de saque do afiliado (R$). Espelha sz_cod_withdraw_fee (default 2.99).
INSERT INTO senderzz_options (name, value, autoload)
VALUES ('sz_aff_withdraw_fee', '2.99', 'yes')
ON CONFLICT (name) DO NOTHING;

-- Taxa PERCENTUAL de antecipação do afiliado (%). Espelha
-- sz_cod_anticipation_fee_pct (default 4.99) — aplicada sobre o bruto antecipado.
INSERT INTO senderzz_options (name, value, autoload)
VALUES ('sz_aff_anticipation_fee_pct', '4.99', 'yes')
ON CONFLICT (name) DO NOTHING;

-- Flags fail-OPEN (o dono quer habilitado): saque + antecipação do afiliado ON.
-- Espelha senderzz_dashboard_v2_withdraw_enabled (que no COD é default 'no').
INSERT INTO senderzz_options (name, value, autoload)
VALUES ('senderzz_dashboard_v2_aff_withdraw_enabled', 'yes', 'yes')
ON CONFLICT (name) DO NOTHING;

INSERT INTO senderzz_options (name, value, autoload)
VALUES ('senderzz_dashboard_v2_aff_anticipate_enabled', 'yes', 'yes')
ON CONFLICT (name) DO NOTHING;

-- ── 2. senderzz_affiliate_withdrawals.holder_cpf (ADD se faltar) ─────────────
-- O schema real (\d) NÃO tinha holder_cpf — o registro de saque do afiliado é um
-- registro de PAGAMENTO; o admin precisa do CPF do titular p/ conciliar o PIX no
-- repasse. Mesma justificativa de sz_cod_withdraw_accounts.holder_cpf.
-- IF NOT EXISTS = idempotente; nullable (saques antigos não têm o dado).
ALTER TABLE senderzz_affiliate_withdrawals
  ADD COLUMN IF NOT EXISTS holder_cpf VARCHAR(20);

-- ── 3. CHECK de senderzz_affiliate_transactions.type — RELAXA (aditivo) ──────
-- O CHECK real aceita: commission, penalty, withdrawal, adjustment, refund.
--   - 'withdrawal'       JÁ é aceito (o débito do saque é inserido pelo ADMIN no
--     approve — cod_saques.go/bulk_queues.go; o pedido no portal NÃO insere tx).
--   - 'anticipation_fee' NÃO era aceito → ADICIONA (o handler de antecipação do
--     portal insere uma tx type='anticipation_fee' com amount NEGATIVO = a taxa).
-- Aditivo: o novo CHECK é SUPERSET do antigo (nenhuma linha existente viola).
-- DROP IF EXISTS + ADD = idempotente (reexecutar recria com o mesmo conjunto).
ALTER TABLE senderzz_affiliate_transactions
  DROP CONSTRAINT IF EXISTS senderzz_affiliate_transactions_type_check;
ALTER TABLE senderzz_affiliate_transactions
  ADD CONSTRAINT senderzz_affiliate_transactions_type_check
  CHECK (type::text = ANY (ARRAY[
    'commission'::text,
    'penalty'::text,
    'withdrawal'::text,
    'adjustment'::text,
    'refund'::text,
    'anticipation_fee'::text
  ]));
-- NOTA: o CHECK de STATUS não é tocado — ele já aceita 'approved' (o débito do
-- saque e a taxa de antecipação entram como 'approved', coerente com o cache:
-- Summary trata status='approved' como DISPONÍVEL). NÃO existe status 'available'
-- nesta tabela (ao contrário do COD) — por isso usamos 'approved'.

-- ── 4. Captura de receita da ANTECIPAÇÃO (extensão aditiva do trigger afftx) ──
-- O saque do afiliado já tem captura: trg_sz_revenue_capture_affsaque book a
-- 'taxa_saque' quando o withdrawal vira 'approved'/'paid' com fee>0.
-- A ANTECIPAÇÃO, porém, NÃO cria linha em senderzz_affiliate_withdrawals — ela
-- só insere uma tx type='anticipation_fee' (amount = -taxa). Sem extensão, esse
-- take ficaria FORA do senderzz_revenue (meta R$1M/ano). Aqui estendemos o
-- trigger de tx (sz_revenue_capture_afftx) com um ramo ADITIVO p/ book a taxa de
-- antecipação como component='taxa_antecipacao'. CREATE OR REPLACE = idempotente;
-- preserva os ramos existentes (penalty → taxa_frustrado; cancel/reverse → delete).
CREATE OR REPLACE FUNCTION public.sz_revenue_capture_afftx()
  RETURNS trigger
  LANGUAGE plpgsql
AS $function$
BEGIN
    -- Ramo existente: penalidade (frustração) do afiliado → taxa_frustrado.
    IF NEW.type = 'penalty' AND NEW.status NOT IN ('cancelled','reversed') AND NEW.amount > 0 THEN
        INSERT INTO senderzz_revenue (order_id, affiliate_id, component, base_amount, amount, ref, created_at)
        SELECT NEW.order_id, a.afiliado_id, 'taxa_frustrado', NEW.amount, NEW.amount, 'penalty:' || NEW.id, NOW()
        FROM senderzz_affiliates a WHERE a.id = NEW.affiliate_id
        ON CONFLICT (component, ref) DO NOTHING;
    END IF;

    -- Ramo NOVO (aditivo): taxa de antecipação da comissão do afiliado.
    -- A tx grava amount NEGATIVO (= a taxa debitada do afiliado); o revenue é o
    -- valor ABSOLUTO. Captura quando a tx está 'approved'/'paid' (a antecipação
    -- nasce 'approved'). base_amount = a própria taxa (não temos o bruto aqui).
    IF NEW.type = 'anticipation_fee' AND NEW.status IN ('approved','paid') AND ABS(NEW.amount) > 0 THEN
        INSERT INTO senderzz_revenue (order_id, affiliate_id, component, base_amount, amount, ref, created_at)
        SELECT NEW.order_id, a.afiliado_id, 'taxa_antecipacao', ABS(NEW.amount), ABS(NEW.amount), 'antfee:' || NEW.id, NOW()
        FROM senderzz_affiliates a WHERE a.id = NEW.affiliate_id
        ON CONFLICT (component, ref) DO NOTHING;
    END IF;

    -- Ramo existente: estorno/cancelamento limpa a receita derivada da tx.
    IF NEW.status IN ('cancelled','reversed') THEN
        DELETE FROM senderzz_revenue WHERE ref IN ('afftx:' || NEW.id, 'penalty:' || NEW.id, 'antfee:' || NEW.id);
    END IF;

    RETURN NEW;
END; $function$;

-- =============================================================================
-- FIM. Resultado: options/flags semeadas, holder_cpf garantida, CHECK de type
-- aceita 'anticipation_fee', e a taxa de antecipação passa a ser capturada em
-- senderzz_revenue. Nenhuma linha financeira existente foi alterada.
-- =============================================================================
