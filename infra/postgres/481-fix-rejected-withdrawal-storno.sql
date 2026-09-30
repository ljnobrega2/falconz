-- 481: Estorna débitos de saques rejeitados que ficaram com status='available'
-- Causa: RejectProducer não cancelava a tx de wallet → saldo permanecia debitado
-- → produtor podia sacar novamente gerando saldo negativo.
--
-- Este script cancela todas as txs type='withdrawal' status='available' cujo
-- saque correspondente já está rejeitado. Execução idempotente.

UPDATE sz_cod_wallet_transactions wtx
SET    status      = 'reversed',
       updated_at  = NOW()
WHERE  wtx.type   = 'withdrawal'
  AND  wtx.status = 'available'
  AND  EXISTS (
       SELECT 1
       FROM   sz_cod_withdrawals wd
       WHERE  wd.user_id = wtx.user_id
         AND  wd.status  = 'rejected'
         AND  wtx.description LIKE '%#' || wd.id::text
       );

-- Verifica saldos ainda negativos após o fix (deve retornar 0 linhas):
-- SELECT user_id,
--        ROUND(SUM(CASE WHEN type = 'withdrawal' THEN gross
--                       ELSE COALESCE(NULLIF(net,0), gross) END)::numeric, 2) AS saldo
-- FROM   sz_cod_wallet_transactions
-- WHERE  status = 'available'
-- GROUP  BY user_id
-- HAVING SUM(CASE WHEN type = 'withdrawal' THEN gross
--                 ELSE COALESCE(NULLIF(net,0), gross) END) < 0;
