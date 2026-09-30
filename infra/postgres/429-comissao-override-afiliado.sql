-- =============================================================================
-- Senderzz — Override de comissão POR-AFILIADO no path de criação de pedido (Go)
--
-- MODELO (confirmado pelo dono) — PRECEDÊNCIA da comissão do pedido NOVO
-- (mais específico vence):
--   comissão = override_afiliado   SE  (toggle sz_aff_per_affiliate_override LIGADO
--                                        E o afiliado tem comissao_pct custom > 0
--                                        no vínculo senderzz_affiliates)
--            ↳ senão: comissão do CHECKOUT LINK  (senderzz_checkout_links.affiliate_commission_pct > 0)
--            ↳ senão: comissão PADRÃO do PRODUTOR (_sz_aff_default_commission_pct)
--            ↳ senão: global (sz_aff_default_commission_pct) → 10.
--
-- POR QUE esta migração é SÓ a OPTION (e NÃO uma coluna nova):
--   * A "comissão do CHECKOUT LINK" já existe como COLUNA DEDICADA
--     senderzz_checkout_links.affiliate_commission_pct (migração 428). A spec original
--     pedia uma coluna comissao_pct em checkout_links ("427-comissao-link.sql"); ela
--     NÃO é necessária — seria repurposo/duplicata da coluna 428. DESVIO documentado.
--   * O "override por-afiliado" lê a % custom de senderzz_affiliates.comissao_pct
--     (coluna DEDICADA já existente, 070-affiliates.sql:46) — nada a criar lá.
--   * Resta apenas o TOGGLE que liga/desliga o tier de override. É uma config
--     global key/value, então vive em senderzz_options.
--
-- DEFAULT '0' (toggle DESLIGADO) — DELIBERADO: com o toggle off, a resolução do
--   pedido NOVO é BYTE-IDÊNTICA ao comportamento de hoje (link → produtor → global).
--   O tier de override é puro no-op até o dono ligar o toggle no WP admin > Opções.
--   Isso é o que garante o GATE GOLDEN por construção, somado ao fato de a resolução
--   ser INSERT-only (pedidos EXISTENTES nunca são re-derivados).
--
-- SEGURANÇA / GOLDEN:
--   * Idempotente: INSERT ... ON CONFLICT DO NOTHING. Pode reexecutar.
--   * Toca SOMENTE senderzz_options (1 linha de config) — NUNCA sz_orders. O pedido
--     golden #1587 (total 250 / affiliate_amount 142.51 / transaction_fee 7.49) e
--     todos os pedidos existentes ficam ESTRUTURALMENTE intocados.
--
-- Rodar manualmente no Postgres do VPS (idempotente — pode reexecutar).
-- =============================================================================

INSERT INTO senderzz_options (name, value, autoload)
VALUES ('sz_aff_per_affiliate_override', '0', 'yes')
ON CONFLICT (name) DO NOTHING;

COMMENT ON TABLE senderzz_options IS 'Key/value store (substitui wp_options do PHP).';
