-- 496-marcio-payout-manual.sql
--
-- AUDIT-2026-07-11: dono confirmou que o Disponível de Marcio Baranauskas Serrano
-- (afiliado_id=54, vínculo senderzz_affiliates.id=32) já foi pago POR FORA do
-- sistema (manual, fora do fluxo de saque). Debita o Disponível a zero SEM
-- cobrar a taxa de saque normal (não é um saque real pelo fluxo — já foi pago).
-- Correção pontual, não é regra geral.

BEGIN;

DO $$
DECLARE
    v_disponivel numeric;
BEGIN
    SELECT GREATEST(0, COALESCE(SUM(CASE
             WHEN status IN ('pending','cancelled') THEN 0
             WHEN type = 'penalty' THEN -ABS(amount)
             ELSE amount
           END), 0))
      INTO v_disponivel
      FROM senderzz_affiliate_transactions
     WHERE affiliate_id = 32;

    IF v_disponivel <= 0 THEN
        RAISE NOTICE '[496] Marcio já está com disponível <= 0 (%). Nada a fazer.', v_disponivel;
        RETURN;
    END IF;

    INSERT INTO senderzz_affiliate_transactions
        (order_id, affiliate_id, type, status, amount, description, meta_json, created_at, updated_at)
    VALUES
        (NULL, 32, 'withdrawal', 'paid', -v_disponivel,
         'Pago manualmente pelo admin fora do sistema — AUDIT-2026-07-11 (sem taxa de saque)',
         jsonb_build_object('source', 'manual_payout_496_2026-07-11'), NOW(), NOW());
END $$;

COMMIT;
