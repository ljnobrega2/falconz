-- =============================================================================
-- 463-referral-capture.sql — CAPTURA da indicação no cadastro            // REF-PAYOUT
--
-- POR QUE: a migração 462 criou senderzz_portal_users.referred_by (quem indicou
--   cada usuário) + o ledger senderzz_referral_rewards + o cron sz_referral_payout().
--   FALTAVA capturar QUEM indicou no momento do CADASTRO público e propagar para
--   o portal_user na APROVAÇÃO do admin.
--
-- FLUXO (go/admin onboarding.go):
--   1. Signup (POST /onboarding/signup) cria uma linha em senderzz_onboarding_requests
--      (status 'pending'). Se o cadastro veio por /r/{referral_code}, o Go resolve o
--      indicador e grava o portal id dele aqui em referred_by.
--   2. approveOne (admin aprova) cria a linha em senderzz_portal_users e PROPAGA este
--      referred_by para lá — apenas se ainda NULL (imutável) e <> id (self barrado).
--
-- O QUE: adiciona senderzz_onboarding_requests.referred_by BIGINT (nasce NULL).
--   É o portal id do INDICADOR (senderzz_portal_users.id), o mesmo id-space de
--   senderzz_portal_users.referred_by (462). Sem FK — degradação graciosa quando
--   o indicador some / tabela ausente; a checagem de validade é no Go (signup).
--
-- Idempotente (ADD COLUMN IF NOT EXISTS). Seguro em base nova e com dados.
-- =============================================================================

ALTER TABLE senderzz_onboarding_requests
  ADD COLUMN IF NOT EXISTS referred_by BIGINT;

COMMENT ON COLUMN senderzz_onboarding_requests.referred_by IS
  'Portal id do INDICADOR (quem trouxe este cadastro via /r/{referral_code}). Capturado no signup; propagado a senderzz_portal_users.referred_by na aprovação. REF-PAYOUT 463.';
