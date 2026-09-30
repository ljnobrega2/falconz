-- 431 — cadastro público auto-serviço (FALK)
-- Armazena a senha (bcrypt) escolhida no signup público em senderzz_onboarding_requests.
-- O admin aprova a solicitação e DEFINE O NÍVEL (RBAC); a conta é criada com este hash.
-- Idempotente. NÃO altera dado financeiro nem lógica existente.

ALTER TABLE senderzz_onboarding_requests
  ADD COLUMN IF NOT EXISTS password_hash TEXT;

COMMENT ON COLUMN senderzz_onboarding_requests.password_hash IS
  'bcrypt da senha escolhida no cadastro público (/onboarding/signup); usada ao aprovar e criar o usuário com o nível definido pelo admin.';
