-- =============================================================================
-- Senderzz v473 — Toggle de OVERRIDE de comissão por-afiliado
--   (sz_aff_per_affiliate_override em senderzz_options)
--
-- MODELO (confirmado pelo dono): a OFERTA dita a % de comissão do afiliado. O
--   override por-AFILIADO (senderzz_affiliates.comissao_pct do vínculo) é OPCIONAL
--   e fica atrás de um TOGGLE GLOBAL. PRECEDÊNCIA quando o toggle está LIGADO:
--     override_afiliado (comissao_pct custom > 0) > oferta > produtor > global(10).
--   DEFAULT do toggle = '0' (DESLIGADO) → "o LINK/oferta dita", precedência
--   idêntica ao comportamento anterior (oferta → produtor → global). Decisão
--   REVERSÍVEL: ligar/desligar é só trocar o value desta option.
--
-- POR QUE esta migração: senderzz_options é uma tabela kv genérica — NÃO há
--   mudança de schema. Esta migração apenas SEMEIA a option com '0' (idempotente)
--   para deixar o estado default EXPLÍCITO e auditável. A resolução no checkout Go
--   (go/orders checkout.go::perAffiliateOverrideEnabled) já trata a AUSÊNCIA da
--   option como DESLIGADO (fail-safe) — então esta migração NÃO é load-bearing;
--   é documentação executável do default.
--
-- SEGURANÇA / GOLDEN:
--   * Default '0' ⇒ tier de override é NO-OP ⇒ resolução byte-idêntica à anterior
--     ⇒ golden #1587 e os 41 pedidos existentes NÃO mudam (somado ao fato de a
--     resolução nova ser INSERT-only no checkout).
--   * NÃO toca sz_orders nem nenhuma coluna financeira. NÃO mexe em
--     carteira/COD/wallet.
--   * ON CONFLICT DO NOTHING: se o dono JÁ tiver ligado a option ('1') antes desta
--     migração rodar, ela NÃO sobrescreve a escolha dele.
--
-- Rodar manualmente no Postgres do VPS (idempotente — pode reexecutar).
-- =============================================================================

INSERT INTO senderzz_options (name, value, autoload)
VALUES ('sz_aff_per_affiliate_override', '0', 'yes')
ON CONFLICT (name) DO NOTHING;
