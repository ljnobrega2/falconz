-- 489: Reaplica o fix de 481 com o valor CORRETO de status.
--
-- 481 rodou em prod com status='cancelled' (valor fora do enum aceito por
-- sz_cod_wallet_transactions — ver constraint) ANTES do arquivo ser corrigido
-- localmente para 'reversed' (mesmo valor usado por cod_saques.go MarkProducerPaid
-- e pelo restante do código). Como 481 já está registrada em schema_migrations,
-- ela NUNCA re-executa — a correção para 'reversed' nunca chegou a prod. Esta
-- migration repete o mesmo UPDATE com o valor certo. Idempotente (WHERE já
-- exclui linhas que não precisam mudar).
--
-- Causa original (ver 481): RejectProducer não cancelava a tx de wallet → saldo
-- permanecia debitado → produtor podia sacar novamente gerando saldo negativo.

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
