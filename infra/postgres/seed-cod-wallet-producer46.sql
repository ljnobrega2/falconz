-- =============================================================================
-- seed-cod-wallet-producer46.sql
-- Semeia a CARTEIRA COD (Cash on Delivery) do produtor demo 46.
--
-- id-space: a carteira COD é keyed por WP_USER_ID. O produtor demo (portal id
--   46) tem wp_user_id = 990001 → user_id = 990001 (NÃO 46). Espelha a regra de
--   wallet.go Summary (WHERE user_id = u.WPUserID).
--
-- POR QUE: /portal/wallet/summary mostrava 0 porque (a) o demo não tinha
--   linhas COD próprias e (b) as 22 linhas legadas (produtor 21) têm net=0.00
--   (split fee/net nunca calculado). Aqui semeamos linhas com net REAL para a
--   tela exibir Disponível/Pendente corretos e demonstrar o release cron
--   (sz_cod_release_due): linhas pending com release_at já vencido viram
--   available na próxima passada do runner; linhas com release_at futuro
--   permanecem pending até vencer.
--
-- IDEMPOTENTE: limpa o intervalo de order_id reservado p/ o seed (990001-990099)
--   antes de inserir. Re-rodar não duplica.
--
-- type='cod_received' → NÃO dispara o trigger de receita (só type='fee' o faz),
--   então semear não cria receita fantasma.
-- =============================================================================

BEGIN;

DELETE FROM sz_cod_wallet_transactions
 WHERE user_id = 990001
   AND order_id BETWEEN 990001 AND 990099;

-- Disponível (já liberado): status='available', net real, release_at no passado.
INSERT INTO sz_cod_wallet_transactions
    (user_id, order_id, type, amount, status, description, gross, fee, net, release_at, created_at, updated_at)
VALUES
    (990001, 990001, 'cod_received', 320.00, 'available', 'Recebimento COD entrega #990001 (liberado)', 320.00, 0.00, 320.00, NOW() - INTERVAL '8 days',  NOW() - INTERVAL '15 days', NOW() - INTERVAL '8 days'),
    (990001, 990002, 'cod_received', 185.50, 'available', 'Recebimento COD entrega #990002 (liberado)', 185.50, 0.00, 185.50, NOW() - INTERVAL '6 days',  NOW() - INTERVAL '13 days', NOW() - INTERVAL '6 days'),
    (990001, 990003, 'cod_received', 249.90, 'available', 'Recebimento COD entrega #990003 (liberado)', 249.90, 0.00, 249.90, NOW() - INTERVAL '4 days',  NOW() - INTERVAL '11 days', NOW() - INTERVAL '4 days'),
    (990001, 990004, 'cod_received', 412.00, 'available', 'Recebimento COD entrega #990004 (liberado)', 412.00, 0.00, 412.00, NOW() - INTERVAL '2 days',  NOW() - INTERVAL  '9 days', NOW() - INTERVAL '2 days');

-- Pendente VENCIDO (release_at no passado mas ainda status='pending'): o runner
-- sz_cod_release_due() vai liberar estas na próxima execução (prova do cron).
INSERT INTO sz_cod_wallet_transactions
    (user_id, order_id, type, amount, status, description, gross, fee, net, release_at, created_at, updated_at)
VALUES
    (990001, 990010, 'cod_received', 138.00, 'pending', 'Recebimento COD entrega #990010 (a liberar)', 138.00, 0.00, 138.00, NOW() - INTERVAL '1 day', NOW() - INTERVAL '8 days', NOW() - INTERVAL '1 day'),
    (990001, 990011, 'cod_received',  96.40, 'pending', 'Recebimento COD entrega #990011 (a liberar)',  96.40, 0.00,  96.40, NOW() - INTERVAL '1 day', NOW() - INTERVAL '8 days', NOW() - INTERVAL '1 day');

-- Pendente em retenção (release_at FUTURO): permanece pending até vencer.
INSERT INTO sz_cod_wallet_transactions
    (user_id, order_id, type, amount, status, description, gross, fee, net, release_at, created_at, updated_at)
VALUES
    (990001, 990020, 'cod_received', 274.30, 'pending', 'Recebimento COD entrega #990020 (retenção D+7)', 274.30, 0.00, 274.30, NOW() + INTERVAL '5 days', NOW() - INTERVAL '2 days', NOW() - INTERVAL '2 days'),
    (990001, 990021, 'cod_received', 159.80, 'pending', 'Recebimento COD entrega #990021 (retenção D+7)', 159.80, 0.00, 159.80, NOW() + INTERVAL '6 days', NOW() - INTERVAL '1 day',  NOW() - INTERVAL '1 day');

COMMIT;
