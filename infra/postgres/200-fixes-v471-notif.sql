-- =============================================================================
-- schema-fixes-v471-notif.sql  — PUSH NOTIFICATIONS (FEAT-NOTIF)
--
-- Suporte ao endpoint público /wp-json/sz-notif/v1 do go/portal:
--   POST /subscribe   {endpoint, keys:{p256dh,auth}, user_id?}  → upsert
--   POST /unsubscribe {endpoint}                                → delete
--   GET/POST /prefs?user_id=                                    → ler/salvar prefs
--
-- Fail-closed: endpoint vazio → 400 no handler (não chega ao banco).
--
-- Idempotente: CREATE TABLE IF NOT EXISTS — seguro reaplicar.
--
-- Notas de mapeamento (Web Push API):
--   endpoint : URL única do push service (FCM/Mozilla/etc.) — chave natural (UNIQUE).
--   p256dh   : chave pública ECDH (base64url) do subscriber.
--   auth     : segredo de autenticação (base64url) do subscriber.
--   user_id  : portal_users.id quando conhecido; NULL p/ subscriber anônimo
--              (pré-login — a UI pode assinar antes de identificar o usuário).
-- =============================================================================

CREATE TABLE IF NOT EXISTS sz_push_subscriptions (
    id          BIGSERIAL    PRIMARY KEY,
    user_id     BIGINT       NULL,
    endpoint    TEXT         UNIQUE NOT NULL,
    p256dh      TEXT         NULL,
    auth        TEXT         NULL,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_push_subs_user ON sz_push_subscriptions (user_id);

-- Preferências de notificação por usuário (JSONB livre — a UI define o schema:
-- {push:true, email:false, whatsapp:true, ...}). PK em user_id → 1 linha por usuário.
CREATE TABLE IF NOT EXISTS sz_notif_prefs (
    user_id     BIGINT       PRIMARY KEY,
    prefs       JSONB        NOT NULL DEFAULT '{}',
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);
