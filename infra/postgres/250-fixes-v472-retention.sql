-- =============================================================================
-- 250-fixes-v472-retention.sql
-- LGPD — RETENÇÃO / ANONIMIZAÇÃO de PII em registros MUITO antigos.
--
-- POR QUE: o art. 16 da LGPD permite (e o princípio da necessidade do art. 6º
--   exige) eliminar dados pessoais que já cumpriram sua finalidade. Mas a
--   guarda fiscal/contábil (Código Tributário, prazos de 5 anos) e o controle
--   de fraude financeira pedem CAUTELA. Por isso esta função é CONSERVADORA:
--
--     1. NUNCA deleta linha. Só MASCARA (UPDATE … = NULL / valor anonimizado).
--     2. NUNCA toca em tabela financeira (carteira, transações, recargas,
--        revenue, COD, comissões). PII aqui é só de telemetria de acesso.
--     3. Só age em registros cuja FINALIDADE já se esgotou há MUITO tempo:
--          - sessões EXPIRADAS há > 2 anos (não autenticam mais ninguém);
--          - trilha de acesso a PII com > 2 anos (accountability já cumprida —
--            o prazo de 2 anos cobre auditorias internas e o ciclo do RIPD).
--     4. Idempotente: o filtro exige o valor ainda PREENCHIDO (ip IS NOT NULL,
--        etc.); após o 1º mascaramento as linhas saem do conjunto. Rodar 2× é
--        no-op — a 2ª passada retorna 0.
--
-- ALVO PII (cada um justificado como seguro):
--   senderzz_portal_sessions.ip / .user_agent
--       → metadado técnico de uma sessão que JÁ EXPIROU há > 2 anos. Não há
--         valor de negócio, fiscal ou de segurança em reter o IP/UA de um login
--         que venceu há mais de dois anos. A linha permanece (contagem/auditoria
--         de volume), só os identificadores pessoais saem.
--   senderzz_pii_access_log.ip / .actor_email
--       → log de QUEM acessou PII de terceiros (accountability, art. 37). Após
--         2 anos o evento continua existindo (subject_type/subject_id/action/
--         accessed_at preservados p/ estatística), mas o IP e o e-mail do
--         operador são despersonalizados. NÃO apagamos a linha — a trilha
--         continua íntegra, só deixa de carregar PII viva.
--
-- DESIGN REVERSÍVEL: o mascaramento é determinístico e auto-descritivo
--   ('anon@redacted.lgpd', '0.0.0.0', '[redacted]') — fica óbvio na inspeção que
--   a linha foi anonimizada (não confundir com NULL original) e a operação é
--   auditável por COMMENT. Não há "des-anonimizar", como manda a LGPD, mas o
--   desenho deixa o efeito explícito e sem ambiguidade.
--
-- IDEMPOTENTE: CREATE OR REPLACE FUNCTION; sem efeito colateral além do UPDATE
--   guardado por filtro. Seguro re-rodar a cada execução do cron (go/cron).
-- =============================================================================

-- Janela de retenção da PII de telemetria, em anos. Conservador: 2 anos.
-- (Constante embutida na função; ajustável editando o literal abaixo.)

CREATE OR REPLACE FUNCTION sz_anonymize_old_pii()
RETURNS integer
LANGUAGE plpgsql
AS $$
DECLARE
    v_cutoff   timestamptz := NOW() - INTERVAL '2 years';
    n_sessions integer := 0;
    n_access   integer := 0;
    n_total    integer := 0;
BEGIN
    -- ── 1. Sessões de portal EXPIRADAS há > 2 anos ────────────────────────────
    -- Filtro por expires_at (não created_at): o que importa é há quanto tempo a
    -- sessão deixou de ser válida. Mascara só linhas que AINDA têm PII viva.
    -- Filtro exclui valores-sentinela já mascarados → 2ª passada conta 0 (idempotência real).
    UPDATE senderzz_portal_sessions
       SET ip         = CASE WHEN ip         IS NOT NULL THEN '0.0.0.0'    ELSE ip         END,
           user_agent = CASE WHEN user_agent IS NOT NULL THEN '[redacted]' ELSE user_agent END
     WHERE expires_at < v_cutoff
       AND ( (ip         IS NOT NULL AND ip         <> '0.0.0.0')
          OR (user_agent IS NOT NULL AND user_agent <> '[redacted]') );
    GET DIAGNOSTICS n_sessions = ROW_COUNT;

    -- ── 2. Trilha de acesso a PII com accessed_at > 2 anos ────────────────────
    -- A linha permanece (accountability); só IP e e-mail do operador saem.
    -- subject_type/subject_id/fields/action/accessed_at ficam intactos.
    UPDATE senderzz_pii_access_log
       SET ip          = CASE WHEN ip          IS NOT NULL THEN '0.0.0.0'             ELSE ip          END,
           actor_email = CASE WHEN actor_email IS NOT NULL THEN 'anon@redacted.lgpd'  ELSE actor_email END
     WHERE accessed_at < v_cutoff
       AND ( (ip          IS NOT NULL AND ip          <> '0.0.0.0')
          OR (actor_email IS NOT NULL AND actor_email <> 'anon@redacted.lgpd') );
    GET DIAGNOSTICS n_access = ROW_COUNT;

    n_total := n_sessions + n_access;

    -- Observabilidade: registra no log de execução de cron (tabela criada em
    -- 230-fixes-v472-crons.sql). Guardado por to_regclass p/ não falhar se a
    -- tabela ainda não existir num DB parcial.
    IF to_regclass('public.senderzz_cron_runs') IS NOT NULL THEN
        INSERT INTO senderzz_cron_runs (name, duration_ms, status, message)
        VALUES (
            'sz_anonymize_old_pii',
            0,
            'ok',
            format('sessoes=%s acessos_pii=%s (cutoff %s)', n_sessions, n_access, v_cutoff::date)
        );
    END IF;

    RETURN n_total;
END;
$$;

COMMENT ON FUNCTION sz_anonymize_old_pii() IS
    'LGPD: mascara PII (ip/user_agent/actor_email) de sessoes expiradas e trilha '
    'de acesso com > 2 anos. NUNCA deleta linha nem toca dado financeiro. '
    'Idempotente. Chamada pelo runner go/cron.';
