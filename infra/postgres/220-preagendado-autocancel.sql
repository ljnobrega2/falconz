-- =============================================================================
-- schema-preagendado-autocancel.sql
-- AUTO-CANCELAMENTO de pedidos pré-agendados não confirmados (módulo Motoboy).
--
-- CONTEXTO (motor de agendamento — regras confirmadas pelo dono):
--   Ao escolher uma data de entrega no checkout, o pedido nasce:
--     - 'agendado'      → dentro da cota de agendamento garantido da zona, OU
--     - 'pre_agendado'  → fora dessa cota (data mais distante).
--   Pedido 'pre_agendado' EXIGE confirmação do cliente até 3 DIAS DE ENTREGA
--   antes da data_entrega. Se NÃO confirmar até esse prazo, o pedido é
--   CANCELADO automaticamente por esta função.
--
-- "3 DIAS DE ENTREGA antes" = contagem em DIAS DE ENTREGA da zona (não dias
--   corridos). Conta-se para trás a partir do dia anterior à data_entrega,
--   pulando os dias em que a zona NÃO entrega, e pega-se o 3º dia de entrega
--   anterior. Esse é o prazo final de confirmação. Se a data de hoje (horário
--   de Brasília) já passou desse prazo, o pré-agendamento venceu.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- CONVENÇÃO DE DIAS DA SEMANA — IMPORTANTE (leitura obrigatória):
--   sz_motoboy_zonas.dias_funcionamento é um CSV de dias da semana no padrão
--   0=Domingo, 1=Segunda, ..., 6=Sábado (mesmo padrão de PHP date('w')).
--   Isso foi confirmado no código-fonte:
--     - includes/motoboy/router.php (sanitize aceita só 0-6; $target->format('w'))
--     - go/motoboy/internal/handlers/zona.go ("CSV de 0-6, domingo=0")
--     - schema-motoboy.sql linha 74 ("0=Dom, 1=Seg, ..., 6=Sáb")
--   Por isso, DELIBERADAMENTE usamos EXTRACT(DOW FROM d) (0=Dom..6=Sáb) e NÃO
--   EXTRACT(ISODOW) (1=Seg..7=Dom). DOW e ISODOW só divergem no domingo
--   (DOW=0 vs ISODOW=7); como o CSV é restrito a 0-6, ISODOW JAMAIS casaria
--   uma zona que entrega aos domingos (dias_funcionamento contendo '0') — seria
--   bug garantido. DOW = PHP date('w'), 1:1 com a operação real.
-- ─────────────────────────────────────────────────────────────────────────────
--
-- COMO RODAR (não há scheduler Go para isto ainda — chamar via cron):
--   SELECT sz_cancel_preagendados_vencidos();
--   (ex.: cron diário do Postgres / pg_cron / cron do SO via psql.)
--   A função retorna a QUANTIDADE de pedidos cancelados na execução.
--
-- IDEMPOTÊNCIA / SEGURANÇA:
--   - Só faz UPDATE de status para 'cancelado'. NUNCA deleta dados.
--   - Só atinge pedidos status='pre_agendado' não confirmados cujo prazo venceu.
--     Rodar de novo no mesmo dia não recancela (já não estão mais 'pre_agendado').
--   - Pedidos sem data_entrega ou sem zona resolvível são IGNORADOS (não há
--     prazo computável → não cancela).
--   - "hoje" usa horário de Brasília (America/Sao_Paulo), coerente com o
--     wall-clock gravado pelo PHP (schema-motoboy.sql, nota de timezone).
--
-- Aplicar por ambiente antes do cron entrar em produção. Não-destrutivo.
-- =============================================================================

-- -----------------------------------------------------------------------------
-- 1) Coluna de confirmação do pré-agendamento.
--    false = cliente ainda não confirmou (sujeito a auto-cancelamento).
--    true  = cliente confirmou (nunca auto-cancelado por esta função).
-- -----------------------------------------------------------------------------
ALTER TABLE sz_motoboy_pedidos
    ADD COLUMN IF NOT EXISTS preagendado_confirmado boolean NOT NULL DEFAULT false;

COMMENT ON COLUMN sz_motoboy_pedidos.preagendado_confirmado IS
    'Pré-agendamento confirmado pelo cliente. false = sujeito a auto-cancelamento se o prazo (3 dias de entrega antes de data_entrega) vencer.';

-- Observação: o CHECK de status já aceita 'pre_agendado' (schema-fixes-v468)
-- e 'cancelado' (schema-motoboy original). Nenhuma alteração de constraint aqui.

-- -----------------------------------------------------------------------------
-- 2) Função de auto-cancelamento.
--    Para cada pedido 'pre_agendado' não confirmado:
--      a) resolve a zona (já gravada em zona_id) e seu dias_funcionamento;
--      b) calcula o 3º DIA DE ENTREGA anterior a data_entrega (= prazo limite
--         de confirmação) usando a convenção DOW (0=Dom..6=Sáb);
--      c) se hoje (Brasília) > prazo limite → muda status para 'cancelado'.
--    Retorna a quantidade de pedidos cancelados.
-- -----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION sz_cancel_preagendados_vencidos()
RETURNS integer
LANGUAGE plpgsql
AS $$
DECLARE
    v_hoje        date := (NOW() AT TIME ZONE 'America/Sao_Paulo')::date;
    v_cancelados  integer := 0;
BEGIN
    WITH alvos AS (
        SELECT
            p.id,
            -- 3º DIA DE ENTREGA anterior a data_entrega (prazo de confirmação).
            -- Conta para trás a partir do dia anterior a data_entrega, filtrando
            -- apenas dias em que a zona entrega (DOW ∈ dias_funcionamento), e pega
            -- o 3º (OFFSET 2). data_entrega em si é excluída (começa em -1 dia).
            (
                SELECT d::date
                FROM generate_series(
                        p.data_entrega - 1,
                        p.data_entrega - 90,   -- janela ampla p/ cobrir zonas de 1 dia/semana
                        INTERVAL '-1 day'
                     ) AS d
                WHERE EXTRACT(DOW FROM d)::int = ANY(
                        -- dias_funcionamento da zona como int[]; default operacional 1..6
                        string_to_array(
                            COALESCE(NULLIF(z.dias_funcionamento, ''), '1,2,3,4,5,6'),
                            ','
                        )::int[]
                      )
                ORDER BY d DESC
                OFFSET 2
                LIMIT 1
            ) AS prazo_confirmacao
        FROM sz_motoboy_pedidos p
        JOIN sz_motoboy_zonas   z ON z.id = p.zona_id
        WHERE p.status = 'pre_agendado'
          AND p.preagendado_confirmado = false
          AND p.data_entrega IS NOT NULL   -- sem data → sem prazo computável → ignora
    ),
    vencidos AS (
        SELECT id
        FROM alvos
        WHERE prazo_confirmacao IS NOT NULL  -- zona sem dia de entrega válido no range → ignora
          AND v_hoje > prazo_confirmacao     -- prazo de confirmação já passou
    ),
    atualizados AS (
        UPDATE sz_motoboy_pedidos p
           SET status     = 'cancelado',
               -- wall-clock Brasília (coerente com schema-motoboy.sql e com v_hoje)
               updated_at = (NOW() AT TIME ZONE 'America/Sao_Paulo')
          FROM vencidos v
         WHERE p.id = v.id
        RETURNING p.id
    )
    SELECT COUNT(*) INTO v_cancelados FROM atualizados;

    RAISE NOTICE '[sz_motoboy] auto-cancel pre_agendado: % pedido(s) cancelado(s) em % (America/Sao_Paulo).',
        v_cancelados, v_hoje;

    RETURN v_cancelados;
END;
$$;

COMMENT ON FUNCTION sz_cancel_preagendados_vencidos() IS
    'Cancela pedidos pre_agendado não confirmados cujo prazo (3 dias de entrega antes de data_entrega) venceu. Idempotente, só UPDATE status. Chamar por cron: SELECT sz_cancel_preagendados_vencidos();';
