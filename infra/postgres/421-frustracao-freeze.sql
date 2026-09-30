-- =============================================================================
-- 421-frustracao-freeze.sql — CONGELAMENTO do financeiro de frustração.
--
-- Modelo confirmado pelo dono (fonte da verdade):
--   FRUSTRADO zera o financeiro NORMAL (produtor e afiliado). A ÚNICA cobrança é
--   a PENALTY de frustração, CONGELADA (snapshot) no momento da baixa — imune a
--   reagendamento/retroatividade. A VIEW LÊ o valor congelado, NÃO recalcula.
--
--   - Produtor: 1ª frustração por TELEFONE do cliente é GRÁTIS
--     (sz_prod_first_frustration_penalty, default 0); 2ª+ do MESMO telefone cobra
--     o repeat (sz_aff_producer_frustration_penalty, default 8).
--     A penalty CONGELADA do PRODUTOR reusa a coluna valor_taxa_frustrado
--     (REPURPOSE — antes guardava o fee do motoboy; agora = penalty do produtor).
--   - Afiliado (só se o pedido tem afiliado): paga em TODAS (sem isenção)
--     (aff_first/aff_repeat, default 5). A penalty CONGELADA do AFILIADO vai na
--     nova coluna valor_taxa_frustrado_afiliado.
--
-- Esta migração só ADICIONA a coluna do afiliado. O produtor reusa a coluna
-- existente valor_taxa_frustrado. Idempotente (IF NOT EXISTS).
--
-- NÃO toca view nem ledger (fase separada). O backfill dos 6 pedidos reais
-- frustrados vive em 422-frustracao-freeze-backfill.sql.
-- =============================================================================

ALTER TABLE sz_motoboy_pedidos
    ADD COLUMN IF NOT EXISTS valor_taxa_frustrado_afiliado numeric(12,2) NOT NULL DEFAULT 0;

COMMENT ON COLUMN sz_motoboy_pedidos.valor_taxa_frustrado_afiliado IS
  'Penalty de frustração do AFILIADO, CONGELADA na baixa (snapshot, imune a retroatividade). 0 quando o pedido não tem afiliado. A penalty do PRODUTOR reusa valor_taxa_frustrado.';

COMMENT ON COLUMN sz_motoboy_pedidos.valor_taxa_frustrado IS
  'REPURPOSE: penalty de frustração do PRODUTOR, CONGELADA na baixa (antes guardava o fee do motoboy). 0 na 1ª frustração por telefone (isento), repeat caso contrário.';
