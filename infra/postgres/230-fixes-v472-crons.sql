-- =============================================================================
-- 230-fixes-v472-crons.sql
-- Infra de CRON do site novo (Go): tabelas de status/execução + funções
-- idempotentes que o runner Go (go/cron) chama em intervalo.
--
-- POR QUE: o admin (go/admin/internal/handlers/cron_status.go) já LÊ
--   senderzz_cron_status (merge com o catálogo de 18 crons) e GRAVA last_run
--   em /crons/{name}/run, mas as tabelas NUNCA foram criadas no espelho PG
--   (tela mostrava todos os crons como last_status="never"). E não havia
--   nenhum RUNNER de fato — só o viewer. Este arquivo + go/cron fecham o ciclo.
--
-- PRINCÍPIO (auditoria 2026-06-18 / advisor): jobs IDEMPOTENTES — disparo duplo
--   é inofensivo. Cada função filtra pelo estado de origem (status + vencimento)
--   e só faz a transição devida; rodar 2× não dobra efeito. NUNCA deleta dado
--   financeiro (só muda status; sessão expirada é a única exclusão, e é segura).
--
-- DECISÃO DE NÃO-INVENTAR-MONEY-LOGIC (advisor): cada release usa uma coluna de
--   vencimento que JÁ EXISTE no schema (sz_cod_wallet_transactions.release_at e
--   senderzz_affiliate_transactions.available_at) — não fabricamos regra de
--   retenção nova. Consumidores existem: portal wallet.go Summary lê
--   status='available' (COD net) e status='approved' (afiliado).
--
-- SEGURANÇA DE TRIGGER (verificado na DB viva 2026-06-18):
--   - trg_sz_revenue_capture_codfee só registra receita quando type='fee';
--     liberar linhas type='cod_received' (pending→available) é no-op de receita.
--   - trg_sz_revenue_capture_afftx só age em type='penalty' ou status
--     cancelled/reversed; liberar commission (pending→approved) é no-op.
--   Logo nenhuma liberação dobra receita. Ambos os triggers usam
--   ON CONFLICT (component, ref) DO NOTHING de qualquer modo.
-- =============================================================================

-- ── Tabelas de observabilidade de cron (contrato do admin cron_status.go) ─────

CREATE TABLE IF NOT EXISTS senderzz_cron_status (
    name             VARCHAR(120)  NOT NULL,
    last_run         TIMESTAMPTZ   NULL,
    last_status      VARCHAR(20)   NOT NULL DEFAULT 'never',  -- never|ok|error|skipped
    last_message     TEXT          NULL,
    last_duration_ms BIGINT        NOT NULL DEFAULT 0,
    next_run         TIMESTAMPTZ   NULL,
    PRIMARY KEY (name)
);

CREATE TABLE IF NOT EXISTS senderzz_cron_runs (
    id          BIGINT        NOT NULL GENERATED ALWAYS AS IDENTITY,
    name        VARCHAR(120)  NOT NULL,
    started_at  TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    duration_ms BIGINT        NOT NULL DEFAULT 0,
    status      VARCHAR(20)   NOT NULL DEFAULT 'ok',
    message     TEXT          NULL,
    PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS idx_cron_runs_name    ON senderzz_cron_runs (name);
CREATE INDEX IF NOT EXISTS idx_cron_runs_started ON senderzz_cron_runs (started_at DESC);

-- ── 1. Libera COD: pending → available quando release_at venceu ────────────────
-- Consumidor: portal wallet.go Summary soma net WHERE status='available'.
-- Idempotente: só pega status='pending' E release_at<=now; 2ª passada não acha
-- mais nada. NÃO toca em net/amount (não "conserta" linhas legadas net=0 — só
-- transiciona o status, que é a ação devida do release).
CREATE OR REPLACE FUNCTION sz_cod_release_due()
RETURNS integer LANGUAGE plpgsql AS $$
DECLARE n integer;
BEGIN
    UPDATE sz_cod_wallet_transactions
       SET status = 'available', updated_at = NOW()
     WHERE status = 'pending'
       AND type   = 'cod_received'
       AND release_at IS NOT NULL
       AND release_at <= NOW();
    GET DIAGNOSTICS n = ROW_COUNT;
    RETURN n;
END; $$;

-- ── 2. Libera comissão de afiliado: pending → approved quando venceu ──────────
-- Consumidor: portal wallet.go Summary soma amount WHERE status='approved'.
-- available_at é a coluna de vencimento já existente. Idempotente igual a (1).
CREATE OR REPLACE FUNCTION sz_affiliate_release_due()
RETURNS integer LANGUAGE plpgsql AS $$
DECLARE n integer;
BEGIN
    UPDATE senderzz_affiliate_transactions
       SET status = 'approved', updated_at = NOW()
     WHERE status = 'pending'
       AND type   = 'commission'
       AND available_at IS NOT NULL
       AND available_at <= NOW();
    GET DIAGNOSTICS n = ROW_COUNT;
    RETURN n;
END; $$;

-- ── 3. Limpa sessões de portal expiradas ──────────────────────────────────────
-- Única exclusão dos crons, e é segura: sessão vencida não tem valor.
-- senderzz_portal_sessions.expires_at já existe (idx_sessions_expires).
CREATE OR REPLACE FUNCTION sz_cleanup_expired_sessions()
RETURNS integer LANGUAGE plpgsql AS $$
DECLARE n integer;
BEGIN
    DELETE FROM senderzz_portal_sessions WHERE expires_at < NOW();
    GET DIAGNOSTICS n = ROW_COUNT;
    RETURN n;
END; $$;

-- NOTA: o autocancel de pré-agendado já tem função própria em
-- 220-preagendado-autocancel.sql (sz_cancel_preagendados_vencidos()). O runner
-- go/cron a chama junto com estas três. Os crons de API externa (Melhor Envio
-- reconcile, recarga PIX) dependem de token ME e ficam como skip honesto no
-- runner DEV (sem token) — NÃO fingimos execução.
