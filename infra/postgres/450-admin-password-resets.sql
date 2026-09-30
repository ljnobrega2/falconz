-- 450 — recuperação de senha do admin (FALK)
-- AUDIT-2026-06-22 forgot-pw.
--
-- Tabela de tokens de reset de senha para senderzz_admin_users. O token é gerado
-- com crypto/rand (32 bytes → 64 hex) e SÓ O HASH (HMAC-SHA256 com WP_SALT_AUTH) é
-- persistido aqui — o token bruto vive apenas no link enviado por e-mail. Expiry
-- curto (30 min) gravado em expires_at; single-use via flag `used`.
--
-- Idempotente. NÃO altera tabela já existente nem dado financeiro.

CREATE TABLE IF NOT EXISTS senderzz_admin_password_resets (
    id            BIGINT NOT NULL GENERATED ALWAYS AS IDENTITY,
    admin_user_id BIGINT NOT NULL,
    token_hash    VARCHAR(128) NOT NULL,
    expires_at    TIMESTAMPTZ  NOT NULL,
    used          BOOLEAN      NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id),
    CONSTRAINT fk_admin_pw_reset_user
        FOREIGN KEY (admin_user_id) REFERENCES senderzz_admin_users (id) ON DELETE CASCADE
);

-- Lookup por hash do token (consumo na validação do reset).
CREATE INDEX IF NOT EXISTS idx_admin_pw_reset_token_hash
    ON senderzz_admin_password_resets (token_hash);

-- Limpeza/auditoria por usuário.
CREATE INDEX IF NOT EXISTS idx_admin_pw_reset_user
    ON senderzz_admin_password_resets (admin_user_id);

COMMENT ON TABLE senderzz_admin_password_resets IS
  'Tokens de reset de senha do admin (forgot-pw). Guarda só o HMAC-SHA256 do token; expiry 30min; single-use via used.';
