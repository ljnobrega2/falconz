BEGIN;
-- Relax: receitas continuam não-negativas; só WITHDRAWAL (débito) pode ser negativo.
-- Alinha com o código de saque (que insere débito negativo) e mantém a integridade dos recebíveis.
ALTER TABLE sz_cod_wallet_transactions DROP CONSTRAINT IF EXISTS sz_cod_wtx_gross_nonneg;
ALTER TABLE sz_cod_wallet_transactions ADD  CONSTRAINT sz_cod_wtx_gross_nonneg CHECK (gross >= 0 OR type = 'withdrawal');
ALTER TABLE sz_cod_wallet_transactions DROP CONSTRAINT IF EXISTS sz_cod_wtx_net_nonneg;
ALTER TABLE sz_cod_wallet_transactions ADD  CONSTRAINT sz_cod_wtx_net_nonneg  CHECK (net   >= 0 OR type = 'withdrawal');
-- Débito canônico do saque manual (status='available' → reduz o disponível do Summary).
INSERT INTO sz_cod_wallet_transactions (user_id, type, status, gross, net, fee, amount, description, created_at, updated_at)
VALUES (21,'withdrawal','available',-1247.99,-1247.99,2.99,-1247.99,
        'Saque manual pago por fora: R$1245,00 + taxa de saque R$2,99 — autorizado dono 2026-06-24', NOW(), NOW())
RETURNING id AS tx_debito;
-- Registro do saque (auditoria).
INSERT INTO sz_cod_withdrawals (user_id, amount, fee, net, status, notes, requested_at, processed_at, completed_at, updated_at)
VALUES (21,1247.99,2.99,1245.00,'paid','Pago por fora (manual), autorizado dono 2026-06-24', NOW(), NOW(), NOW(), NOW())
RETURNING id AS saque_audit;
COMMIT;
SELECT COALESCE(SUM(CASE WHEN status='available' THEN COALESCE(NULLIF(net,0),gross) ELSE 0 END),0) AS novo_disponivel
FROM sz_cod_wallet_transactions WHERE user_id=21;
