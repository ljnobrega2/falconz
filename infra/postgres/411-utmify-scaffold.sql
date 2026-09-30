-- =============================================================================
-- 411-utmify-scaffold.sql — ANDAIME (scaffold) da integração Utmify. INERTE.
--
-- Decisão honesta (não fingir integração): o stack NÃO tem o contrato/token REAL
-- da API Utmify do dono. Construir um dispatcher que "funciona" sem o contrato seria
-- código cara-ou-coroa. Então: cria-se só a CONFIG por produtor (token + on/off),
-- DESLIGADA, e documenta-se o que falta p/ ativar. O dispatcher futuro reaproveita o
-- MESMO outbox (sz_webhook_outbox) — Utmify é só mais um alvo de saída.
--
-- O QUE FALTA PARA ATIVAR (dono precisa fornecer/confirmar):
--   1. Endpoint real (Utmify: POST https://api.utmify.com.br/api-credentials/orders).
--   2. Header de auth real (x-api-token) + o token de cada produtor.
--   3. Contrato do corpo (orderId, platform, paymentMethod, status, customer{},
--      products[], trackingParameters{utm_*}, commission{*InCents}) — VALIDAR contra
--      a doc vigente da Utmify antes de enviar dinheiro/PII real.
--   4. Mapear status interno → status Utmify (waiting_payment/paid/refused/refunded).
-- Enquanto isso fica DESLIGADO (enabled=false). Ver docs/INTEGRACOES.md.
-- =============================================================================

CREATE TABLE IF NOT EXISTS senderzz_utmify_config (
    user_id    bigint PRIMARY KEY
               REFERENCES senderzz_portal_users(id) ON DELETE CASCADE,
    api_token  text,                              -- token Utmify do produtor (secreto)
    enabled    boolean     NOT NULL DEFAULT false, -- DESLIGADO até validar contrato
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE senderzz_utmify_config IS
  'Config Utmify por produtor (scaffold INERTE). Dispatcher reaproveita sz_webhook_outbox. Ativar só após validar contrato/token real — ver docs/INTEGRACOES.md.';
