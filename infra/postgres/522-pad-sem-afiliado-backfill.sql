-- AUDIT-2026-07-31 (dono): "pedido PAD não tem afiliado" — não é só a taxa
-- (521), é regra de negócio: PAD/expedição NUNCA deveria ter tido affiliate_id
-- atribuído. Código corrigido (checkout.go não atribui mais afiliado a pedido
-- PAD). Esta migração corrige o PASSADO — mas só sz_orders (os REGISTROS do
-- pedido), NÃO mexe em carteira/saldo de afiliado.
--
-- ESCOPO DELIBERADAMENTE LIMITADO: zera affiliate_id/affiliate_amount/
-- transaction_fee em sz_orders pra pedidos PAD que tinham afiliado atribuído
-- por engano. NÃO toca senderzz_affiliate_transactions, NÃO mexe em saldo de
-- carteira de afiliado, NÃO estorna comissão já paga. Se a comissão já foi
-- CREDITADA/PAGA de verdade pro afiliado (dinheiro já saiu ou já tá disponível
-- pra saque), isso é uma decisão caso-a-caso do dono — reverter automagicamente
-- saldo de terceiro é risco demais pra rodar às cegas numa migração.
--
-- PASSO 1 (rode isto ANTES, é só leitura): quantos pedidos PAD têm afiliado
-- atribuído em produção, e quanto de comissão isso representa?
--
--   SELECT o.id, o.wp_order_id, o.affiliate_id, o.affiliate_amount, o.transaction_fee,
--          o.status, o.created_at
--     FROM sz_orders o
--    WHERE o.affiliate_id IS NOT NULL
--      AND NOT EXISTS (SELECT 1 FROM sz_motoboy_pedidos m
--                       WHERE m.wc_order_id = COALESCE(o.wp_order_id, o.id))
--    ORDER BY o.created_at DESC;
--
-- Rode esse SELECT em produção, olhe a lista, DEPOIS decida se aplica o UPDATE
-- abaixo (e se precisa reverter algo em senderzz_affiliate_transactions
-- separadamente — isso fica de fora de propósito).

-- PASSO 2: corrige sz_orders (registro do pedido) — idempotente, só afeta
-- linhas que realmente têm o problema (affiliate_id setado sem ser motoboy).
UPDATE sz_orders o
   SET affiliate_id = NULL,
       affiliate_amount = 0,
       transaction_fee = 0
 WHERE o.affiliate_id IS NOT NULL
   AND NOT EXISTS (
         SELECT 1 FROM sz_motoboy_pedidos m
          WHERE m.wc_order_id = COALESCE(o.wp_order_id, o.id)
       );
