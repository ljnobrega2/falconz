-- ============================================================================
-- 482 — Compat: coluna producer_net_live em sz_order_financials.
--
-- ROOT CAUSE (bug "drawer do pedido sem endereço/taxas"):
--   go/admin lê `f.producer_net_live` de sz_order_financials (order_detail.go:668,
--   orders.go:528, cod_livro.go, audit.go) — nome canônico da migration 478.
--   Mas 478 nunca pegou em prod: lá sz_order_financials é uma TABELA
--   trigger-mantida (sz_financials_refresh), não a VIEW da 478. A tabela expõe
--   o líquido como `liquido_produtor`, não `producer_net_live`.
--   => GET /orders/{id} dava 500 ("column f.producer_net_live does not exist"),
--      o front zerava `detail`, e o drawer caía no resumo simples (sem endereço
--      completo nem breakdown de taxas). NUNCA foi problema de frontend.
--
-- FIX: coluna GENERATED espelhando liquido_produtor (mesmo valor exato — a
--   fórmula do trigger, GREATEST(total - bruta - take_produtor - entrega, 0) só
--   quando 'entregue', é idêntica à producer_net_live da 478; 1587 = 63,54).
--   O trigger usa lista de colunas EXPLÍCITA (INSERT + ON CONFLICT), então a
--   coluna nova é ignorada por ele e recalculada automaticamente. Portal (que lê
--   liquido_produtor) fica intacto. Reversível: DROP COLUMN producer_net_live.
-- Idempotente.
-- ============================================================================
-- Guard: em instalação NOVA, 478 cria sz_order_financials como VIEW (a coluna
-- producer_net_live já sai calculada por producer_net_live AS ... ali mesmo) —
-- ALTER TABLE ADD COLUMN falha em view. Só roda o ALTER quando for TABELA de
-- verdade (drift de prod descrito acima: trigger sz_financials_refresh mantém
-- uma TABELA em vez da view da 478).
DO $$
BEGIN
  IF (SELECT relkind FROM pg_class WHERE relname = 'sz_order_financials') = 'r' THEN
    ALTER TABLE sz_order_financials
      ADD COLUMN IF NOT EXISTS producer_net_live numeric
      GENERATED ALWAYS AS (liquido_produtor) STORED;
  END IF;
END $$;
