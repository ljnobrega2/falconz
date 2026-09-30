-- 541 — Carteira de Expedição: uma única identidade financeira.
--
-- Regra canônica: tpc_carteira/tpc_transacoes.user_id =
-- senderzz_portal_users.id. O wp_user_id é apenas referência legada.
-- sz_wallet_freight_transfers.user_id continua sendo a chave da carteira COD
-- (necessária para estorno); portal_user_id passa a identificar o dono TPC.
--
-- Também consolida a carteira do Gabriel: a transferência #1, já aprovada e
-- creditada, é o único crédito ativo de Expedição (R$ 20,00). Lançamentos COD
-- vazados e confirmações antigas indevidas ficam cancelados, nunca apagados.

BEGIN;

ALTER TABLE sz_wallet_freight_transfers
    ADD COLUMN IF NOT EXISTS portal_user_id BIGINT;

UPDATE sz_wallet_freight_transfers t
   SET portal_user_id = COALESCE(
       (SELECT u.id
          FROM senderzz_portal_users u
         WHERE u.wp_user_id = NULLIF(t.wp_user_id, 0)
         LIMIT 1),
       (SELECT u.id
          FROM senderzz_portal_users u
         WHERE u.id = CASE WHEN t.user_id < 0 THEN -t.user_id ELSE t.user_id END
         LIMIT 1)
   )
 WHERE t.portal_user_id IS NULL;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM sz_wallet_freight_transfers WHERE portal_user_id IS NULL) THEN
        RAISE EXCEPTION '541: transferência sem portal_user_id canônico';
    END IF;
END $$;

ALTER TABLE sz_wallet_freight_transfers
    ALTER COLUMN portal_user_id SET NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
         WHERE conname = 'fk_wft_portal_user'
           AND conrelid = 'sz_wallet_freight_transfers'::regclass
    ) THEN
        ALTER TABLE sz_wallet_freight_transfers
            ADD CONSTRAINT fk_wft_portal_user
            FOREIGN KEY (portal_user_id) REFERENCES senderzz_portal_users(id);
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_wft_portal_user
    ON sz_wallet_freight_transfers (portal_user_id);

DO $$
DECLARE
    v_user_id BIGINT;
    v_transfer_count INTEGER;
    v_transfer_id BIGINT;
    v_transfer_amount NUMERIC(10,2);
    v_balance NUMERIC(10,2);
BEGIN
    SELECT id INTO v_user_id
      FROM senderzz_portal_users
     WHERE ativo = TRUE
       AND role = 'produtor'
       AND lower(email) = lower('Gabrielcamposvendas@gmail.com');

    IF v_user_id IS NULL THEN
        RAISE NOTICE '541: produtor Gabriel não existe neste ambiente; consolidação específica ignorada';
        RETURN;
    END IF;

    SELECT COUNT(*), COALESCE(SUM(amount), 0)
      INTO v_transfer_count, v_transfer_amount
      FROM sz_wallet_freight_transfers
     WHERE portal_user_id = v_user_id
       AND status = 'approved';

    IF v_transfer_count <> 1 OR v_transfer_amount <> 20.00 THEN
        RAISE EXCEPTION '541: esperado 1 transferência aprovada de R$20; count=%, total=%',
            v_transfer_count, v_transfer_amount;
    END IF;

    SELECT id INTO v_transfer_id
      FROM sz_wallet_freight_transfers
     WHERE portal_user_id = v_user_id
       AND status = 'approved'
       AND amount = 20.00;

    IF NOT EXISTS (
        SELECT 1
          FROM sz_wallet_freight_transfers w
          JOIN tpc_recargas r ON r.id = w.recarga_id
         WHERE w.portal_user_id = v_user_id
           AND w.status = 'approved'
           AND w.amount = 20.00
           AND r.status = 'confirmado'
           AND r.valor = 20.00
    ) THEN
        RAISE EXCEPTION '541: transferência aprovada não possui recarga confirmada de R$20';
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM tpc_transacoes
         WHERE user_id = v_user_id
           AND tipo = 'credito'
           AND status = 'confirmado'
           AND referencia = 'freight_transfer:' || v_transfer_id::text
           AND valor = 20.00
    ) THEN
        RAISE EXCEPTION '541: crédito canônico freight_transfer:% de R$20 ausente', v_transfer_id;
    END IF;

    -- Duplicação histórica do COD na carteira de frete: preserva as linhas,
    -- mas cancela sua participação no saldo TPC.
    UPDATE tpc_transacoes
       SET status = 'cancelado'
     WHERE user_id = v_user_id
       AND status = 'confirmado'
       AND (referencia LIKE 'sz_cod%'
            OR COALESCE(descricao, '') ILIKE 'Venda COD Senderzz%');

    -- As três confirmações antigas não compõem o saldo autorizado pelo dono.
    -- Cancela também o ajuste compensatório de R$35 para que reste apenas o
    -- crédito aprovado da transferência, sem criar um novo lançamento.
    UPDATE tpc_transacoes
       SET status = 'cancelado'
     WHERE user_id = v_user_id
       AND status = 'confirmado'
       AND referencia IN (
           'me-charge-1',
           'me-charge-2',
           'me-charge-3',
           'ajuste-estorno-2026-07-28-gabriel'
       );

    SELECT ROUND(
        COALESCE(SUM(CASE WHEN tipo = 'credito' THEN valor ELSE 0 END), 0)
        - COALESCE(SUM(CASE WHEN tipo IN ('reserva','debito') THEN valor ELSE 0 END), 0),
        2
    )
      INTO v_balance
      FROM tpc_transacoes
     WHERE user_id = v_user_id
       AND status = 'confirmado';

    IF v_balance <> 20.00 THEN
        RAISE EXCEPTION '541: saldo TPC canônico do Gabriel deveria ser R$20; encontrado %', v_balance;
    END IF;

    UPDATE tpc_carteira
       SET saldo = v_balance,
           saldo_reservado = 0
     WHERE user_id = v_user_id;

    IF NOT FOUND THEN
        RAISE EXCEPTION '541: carteira TPC nativa do Gabriel ausente';
    END IF;
END $$;

-- A transferência continua a mesma; apenas corrige o texto obsoleto depois da
-- aprovação, sem criar novo crédito ou novo débito.
UPDATE sz_cod_wallet_transactions tx
   SET description = replace(description, '(aguardando aprovação)', '(aprovada)'),
       updated_at = NOW()
  FROM sz_wallet_freight_transfers w
 WHERE w.status = 'approved'
   AND tx.user_id = w.user_id
   AND tx.type = 'freight_transfer'
   AND tx.description LIKE '%#' || w.id::text || '%'
   AND tx.description LIKE '%(aguardando aprovação)%';

COMMIT;
