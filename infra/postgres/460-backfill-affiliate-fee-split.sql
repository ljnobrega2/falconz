-- 460-backfill-affiliate-fee-split.sql
-- BACKFILL (idempotente) — split da taxa de transação do AFILIADO (4,99%) em pedidos
-- importados do WordPress que entraram com affiliate_amount = BRUTA e transaction_fee = 0.
--
-- CONTEXTO (#46 · varredura 2026-06-22): 4 pedidos `aguardando` (1581,1584,1585,1586)
-- guardavam a comissão BRUTA em affiliate_amount com transaction_fee=0 — o split do take
-- de 4,99% nunca rodou (artefato de import; o checkout em Go já splita correto, ver
-- orders/internal/handlers/checkout.go + checkout_commission_test.go). Sintoma: o drawer
-- de detalhe do pedido mostrava "Taxa transação afiliado (4,99%) R$ 0,00" e a líquida = bruta.
--
-- FIX: transaction_fee = ROUND(affiliate_amount*0.0499, 2); affiliate_amount = bruta − fee.
-- A BRUTA (= affiliate_amount + transaction_fee) NÃO muda → líquido do produtor intacto.
-- Só o líquido do AFILIADO (bruta→net) e a taxa exibida (0→4,99%) corrigem.
--
-- GUARD: nunca toca FRUSTRADO/CANCELADO (fee=0 é correto por design — regra do dono;
-- ver financeiroFrustratedStatuses/financeiroCancelledStatuses em order_detail.go).
-- IDEMPOTENTE: WHERE transaction_fee=0 → reexecução não reprocessa pedidos já splitados.
-- EFEITO ESPERADO em painéis: cod_livro/revenue (que somam transaction_fee de sz_orders)
-- sobem pelo take agora capturado (≈ R$ 29,74 nos 4 pedidos) — correto, é a fatia da plataforma.

UPDATE sz_orders
   SET transaction_fee  = ROUND(affiliate_amount * 0.0499, 2),
       affiliate_amount = affiliate_amount - ROUND(affiliate_amount * 0.0499, 2)
 WHERE COALESCE(affiliate_amount, 0) > 0
   AND COALESCE(transaction_fee, 0) = 0
   AND status NOT IN ('frustrado', 'reembolsado', 'cancelled', 'cancelado');
