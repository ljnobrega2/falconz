-- =============================================================================
-- 433 — Código de indicação PERMANENTE por usuário (referral_code)   // FEAT-RBAC-2026-06-21
--
-- POR QUE: senderzz_affiliate_invites (070-affiliates.sql) é um convite POR-EVENTO
--   (token de uso único, expira em 7 dias). Falta um identificador de indicação
--   ESTÁVEL por usuário, para o link fixo de convite/indicação:
--     falklog.com.br/r/{referral_code}
--   (o gateway/front mapeia /r/{code} → GET /wp-json/senderzz/v1/referral/{code}).
--
-- O QUE: adiciona senderzz_portal_users.referral_code VARCHAR(16) UNIQUE.
--   - Chaveado pelo PORTAL id (senderzz_portal_users.id), SEMPRE presente
--     (wp_user_id é NULLABLE para contas criadas nativamente no Go).
--   - 16 chars hex (8 bytes de crypto/rand → randomHex(8)) — não-adivinhável,
--     cabe exatamente em VARCHAR(16).
--   - Gerado LAZY no Go no 1º acesso a GET /affiliates/referral (cobre usuários
--     já existentes). A geração no momento da APROVAÇÃO (go/admin approveOne) é
--     complementar e fica fora deste escopo.
--
-- IMPORTANTE — VIEW de retrocompatibilidade:
--   A VIEW wp_senderzz_portal_users (050-portal.sql) tem lista de colunas EXPLÍCITA;
--   esta nova coluna NÃO aparece nela e NÃO é necessária para motoboy/wallet.
--   Não recriar/alterar a view aqui (fora de escopo).
--
-- FINANCEIRO (recompensa de indicação): NÃO há payout automático nesta migração.
--   A recompensa é LIDA das options sz_invite_reward_* (estrutura/leitura apenas).
--   Qualquer crédito real é GATED — decisão de negócio do dono, não implementada.
--
-- Idempotente (ADD COLUMN IF NOT EXISTS + índice único IF NOT EXISTS). Seguro em
-- base nova e em base com dados (a coluna nasce NULL; o Go preenche sob demanda).
-- =============================================================================

ALTER TABLE senderzz_portal_users
  ADD COLUMN IF NOT EXISTS referral_code VARCHAR(16);

COMMENT ON COLUMN senderzz_portal_users.referral_code IS
  'Código de indicação PERMANENTE por usuário (16 hex). Link fixo /r/{code}. Gerado lazy no 1º GET /affiliates/referral. FEAT-RBAC-2026-06-21.';

-- UNIQUE parcial: ignora linhas com referral_code NULL (usuários ainda sem código).
-- Permite múltiplos NULLs e garante unicidade dos códigos efetivamente gerados.
CREATE UNIQUE INDEX IF NOT EXISTS uq_portal_users_referral_code
  ON senderzz_portal_users (referral_code)
  WHERE referral_code IS NOT NULL;
