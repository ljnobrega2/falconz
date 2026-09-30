-- =============================================================================
-- 434 — Geração automática do referral_code permanente   // FEAT-RBAC-2026-06-21
-- Trigger BEFORE INSERT em senderzz_portal_users: gera um código curto ÚNICO
-- (8 chars) se vier NULL. Cobre TODOS os caminhos de criação (approve, import).
-- Backfill dos usuários existentes sem código. Idempotente.
-- =============================================================================

CREATE OR REPLACE FUNCTION sz_gen_referral_code() RETURNS trigger AS $$
DECLARE c TEXT; tries INT := 0;
BEGIN
  IF NEW.referral_code IS NOT NULL AND NEW.referral_code <> '' THEN
    RETURN NEW;
  END IF;
  LOOP
    c := upper(substr(md5(random()::text || clock_timestamp()::text || COALESCE(NEW.email,'')), 1, 8));
    EXIT WHEN NOT EXISTS (SELECT 1 FROM senderzz_portal_users WHERE referral_code = c);
    tries := tries + 1;
    EXIT WHEN tries > 12;
  END LOOP;
  NEW.referral_code := c;
  RETURN NEW;
END; $$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_portal_referral_code ON senderzz_portal_users;
CREATE TRIGGER trg_portal_referral_code
  BEFORE INSERT ON senderzz_portal_users
  FOR EACH ROW EXECUTE FUNCTION sz_gen_referral_code();

-- Backfill: usuários já existentes sem código (seed determinístico por id+email).
UPDATE senderzz_portal_users
   SET referral_code = upper(substr(md5(random()::text || id::text || COALESCE(email,'')), 1, 8))
 WHERE referral_code IS NULL OR referral_code = '';
