-- 432 — UNIQUE(email) em senderzz_onboarding_requests (FALK)
-- O handler Create (admin) e Signup (público) usam ON CONFLICT (email); exige
-- esta constraint. No schema Postgres ela não fora criada. Idempotente e seguro
-- em base nova; se houver e-mails duplicados pré-existentes, é ignorado (não quebra).

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'uq_onboarding_requests_email'
  ) THEN
    BEGIN
      ALTER TABLE senderzz_onboarding_requests
        ADD CONSTRAINT uq_onboarding_requests_email UNIQUE (email);
    EXCEPTION WHEN others THEN
      RAISE NOTICE '[432] UNIQUE(email) não aplicada (dups pré-existentes?): %', SQLERRM;
    END;
  END IF;
END $$;
