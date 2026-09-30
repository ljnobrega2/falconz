-- =============================================================================
-- 380-lgpd-completo.sql
-- LGPD (Lei 13.709/2018) — PEÇAS FALTANTES de conformidade no schema.
--
-- Complementa o que já existe (240-fixes-v472-lgpd-sku.sql: consentimento
-- versionado + trilha de acesso a PII; 250-fixes-v472-retention.sql: retenção/
-- anonimização). Aqui entram os direitos do titular e os deveres do controlador
-- que ainda não tinham suporte estrutural:
--
--   1. senderzz_data_subject_requests — canal do titular (Art. 18): pedidos de
--      acesso, correção, exclusão, portabilidade, revogação, oposição, info de
--      compartilhamento. Com SLA de 15 dias embutido (prazo da ANPD).
--   2. senderzz_data_breach_log — comunicação de incidente (Art. 48): registro
--      de vazamento/incidente + estado da notificação à ANPD e aos titulares.
--   3. senderzz_consents.revoked_at — revogação de consentimento (Art. 8º §5º):
--      revogar = setar revoked_at, NUNCA deletar a linha (auditabilidade).
--   4. sz_mask_cpf() / sz_mask_email() — máscara/pseudonimização p/ exibição não
--      privilegiada (minimização do Art. 6º). Puras, IMMUTABLE.
--   5. senderzz_processing_records — ROPA estruturado (Art. 37: registro das
--      operações de tratamento), com seed das atividades conhecidas.
--
-- Tudo ADITIVO e SEGURO: CREATE/ADD ... IF NOT EXISTS, CREATE OR REPLACE,
-- INSERT ... ON CONFLICT DO NOTHING. Idempotente — a 2ª passada não erra nem
-- duplica. NÃO altera/deleta dado existente. NÃO toca arquivos vizinhos.
-- =============================================================================


-- ── 1. CANAL DO TITULAR — Art. 18 (direitos do titular) ──────────────────────
-- Toda requisição de exercício de direito (do cadastrado OU de um titular não
-- cadastrado, identificado só pelo e-mail) vive aqui. O SLA de resposta é de
-- 15 dias: a LGPD/ANPD fixa esse prazo para o atendimento de boa parte dos
-- pedidos do titular (notadamente confirmação/acesso — Art. 19, II), e a
-- Resolução CD/ANPD nº 15/2024 reforça o prazo de 15 dias para a petição do
-- titular. Usamos 15 dias como meta padrão (sla_deadline) p/ priorização e
-- alerta operacional; pedidos mais complexos podem exigir prorrogação motivada.
CREATE TABLE IF NOT EXISTS senderzz_data_subject_requests (
    id            BIGINT       NOT NULL GENERATED ALWAYS AS IDENTITY,
    user_id       BIGINT       NULL,                   -- titular cadastrado (NULL = não-cadastrado)
    email         VARCHAR(255) NOT NULL,               -- contato do titular (sempre presente)
    request_type  VARCHAR(40)  NOT NULL,               -- ver CHECK abaixo (Art. 18 incisos)
    status        VARCHAR(20)  NOT NULL DEFAULT 'recebido', -- recebido|em_andamento|concluido|negado
    reason        TEXT         NULL,                   -- justificativa/descrição do pedido pelo titular
    sla_deadline  TIMESTAMPTZ  NOT NULL DEFAULT (NOW() + INTERVAL '15 days'), -- prazo LGPD (15 dias)
    requested_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    fulfilled_at  TIMESTAMPTZ  NULL,                   -- quando foi atendido/encerrado
    handled_by    BIGINT       NULL,                   -- admin/operador que tratou (id)
    response      TEXT         NULL,                   -- resposta dada ao titular (accountability)
    PRIMARY KEY (id),
    -- request_type cobre os direitos do Art. 18 (e a revogação do Art. 8º §5º).
    CONSTRAINT chk_dsr_request_type CHECK (request_type IN (
        'acesso',                   -- Art. 18, I/II — confirmação e acesso aos dados
        'correcao',                 -- Art. 18, III — correção de dados incompletos/desatualizados
        'exclusao',                 -- Art. 18, VI — eliminação de dados tratados com consentimento
        'portabilidade',            -- Art. 18, V — portabilidade a outro fornecedor
        'revogacao_consentimento',  -- Art. 8º §5º — revogação do consentimento
        'oposicao',                 -- Art. 18, §2º — oposição a tratamento por outra base legal
        'info_compartilhamento'     -- Art. 18, VII — info sobre entidades com quem se compartilhou
    )),
    CONSTRAINT chk_dsr_status CHECK (status IN (
        'recebido', 'em_andamento', 'concluido', 'negado'
    ))
);
CREATE INDEX IF NOT EXISTS idx_dsr_status   ON senderzz_data_subject_requests (status);
CREATE INDEX IF NOT EXISTS idx_dsr_user     ON senderzz_data_subject_requests (user_id);
-- Ordenado por prazo p/ a tela operacional priorizar o que vence primeiro.
CREATE INDEX IF NOT EXISTS idx_dsr_sla      ON senderzz_data_subject_requests (sla_deadline);
CREATE INDEX IF NOT EXISTS idx_dsr_email    ON senderzz_data_subject_requests (email);

COMMENT ON TABLE  senderzz_data_subject_requests IS
    'LGPD Art. 18: canal de exercício dos direitos do titular (acesso, correção, '
    'exclusão, portabilidade, revogação, oposição, info de compartilhamento). '
    'sla_deadline = NOW()+15 dias (prazo de resposta da ANPD).';
COMMENT ON COLUMN senderzz_data_subject_requests.sla_deadline IS
    'Prazo-meta de 15 dias para resposta ao titular (LGPD/ANPD). Usado para '
    'priorização e alerta; pedidos complexos admitem prorrogação motivada.';
COMMENT ON COLUMN senderzz_data_subject_requests.user_id IS
    'Titular cadastrado (senderzz_portal_users.id). NULL quando o requerente '
    'não é cadastrado e se identifica apenas pelo e-mail.';


-- ── 2. COMUNICAÇÃO DE INCIDENTE — Art. 48 (segurança e sigilo) ───────────────
-- Registro de incidente de segurança que possa acarretar risco/dano relevante
-- aos titulares. O Art. 48 impõe ao controlador o DEVER de comunicar à ANPD e
-- ao titular a ocorrência, em PRAZO RAZOÁVEL (a Resolução CD/ANPD nº 15/2024
-- fixou esse prazo em até 3 dias úteis da ciência). Esta tabela trilha o ciclo
-- do incidente e marca explicitamente QUANDO a ANPD e os titulares foram
-- notificados — para provar o cumprimento do dever (accountability).
CREATE TABLE IF NOT EXISTS senderzz_data_breach_log (
    id                   BIGINT       NOT NULL GENERATED ALWAYS AS IDENTITY,
    detected_at          TIMESTAMPTZ  NOT NULL DEFAULT NOW(),   -- ciência do incidente
    nature               TEXT         NOT NULL,                 -- natureza dos dados afetados
    affected_subjects    INTEGER      NULL,                     -- nº de titulares afetados (estimado)
    severity             VARCHAR(20)  NOT NULL DEFAULT 'media', -- baixa|media|alta|critica
    description          TEXT         NULL,                     -- o que aconteceu, vetor, escopo
    measures_taken       TEXT         NULL,                     -- medidas técnicas/admin de contenção
    notified_anpd_at     TIMESTAMPTZ  NULL,                     -- quando comunicou a ANPD (Art. 48)
    notified_subjects_at TIMESTAMPTZ  NULL,                     -- quando comunicou os titulares (Art. 48)
    status               VARCHAR(20)  NOT NULL DEFAULT 'aberto', -- aberto|investigando|notificado|encerrado
    created_by           BIGINT       NULL,                     -- quem registrou o incidente (id)
    created_at           TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id),
    CONSTRAINT chk_breach_severity CHECK (severity IN (
        'baixa', 'media', 'alta', 'critica'
    )),
    CONSTRAINT chk_breach_status CHECK (status IN (
        'aberto', 'investigando', 'notificado', 'encerrado'
    ))
);
CREATE INDEX IF NOT EXISTS idx_breach_status     ON senderzz_data_breach_log (status);
CREATE INDEX IF NOT EXISTS idx_breach_severity   ON senderzz_data_breach_log (severity);
CREATE INDEX IF NOT EXISTS idx_breach_detected   ON senderzz_data_breach_log (detected_at DESC);

COMMENT ON TABLE  senderzz_data_breach_log IS
    'LGPD Art. 48: registro de incidente de segurança com risco/dano relevante. '
    'O controlador DEVE comunicar à ANPD e aos titulares em prazo razoável '
    '(Resolução CD/ANPD 15/2024: até 3 dias úteis da ciência). As colunas '
    'notified_anpd_at / notified_subjects_at provam o cumprimento do dever.';
COMMENT ON COLUMN senderzz_data_breach_log.notified_anpd_at IS
    'Timestamp da comunicação à ANPD (NULL = ainda não notificada). Art. 48 §1º.';
COMMENT ON COLUMN senderzz_data_breach_log.notified_subjects_at IS
    'Timestamp da comunicação aos titulares afetados (NULL = ainda não notificados). Art. 48 §1º.';


-- ── 3. REVOGAÇÃO DE CONSENTIMENTO — Art. 8º §5º ─────────────────────────────
-- O titular pode revogar o consentimento a qualquer tempo. Modelamos a
-- revogação SEM apagar a linha (auditabilidade/accountability — Art. 37):
--   consentimento ATIVO   = revoked_at IS NULL
--   consentimento REVOGADO = revoked_at preenchido (data/hora da revogação)
-- Mantém o histórico de aceite intacto e prova quando deixou de valer.
-- ADD COLUMN IF NOT EXISTS é idempotente (2ª passada = no-op).
ALTER TABLE senderzz_consents
    ADD COLUMN IF NOT EXISTS revoked_at TIMESTAMPTZ NULL;

COMMENT ON COLUMN senderzz_consents.revoked_at IS
    'LGPD Art. 8º §5º: revogação do consentimento. NULL = ativo; preenchido = '
    'revogado (data/hora). Revogar = setar revoked_at; NUNCA deletar a linha '
    '(auditabilidade — Art. 37).';

-- Índice parcial p/ listar rápido só os consentimentos ATIVOS (revoked_at NULL).
CREATE INDEX IF NOT EXISTS idx_consents_ativos
    ON senderzz_consents (user_id, doc_type)
    WHERE revoked_at IS NULL;


-- ── 4. MÁSCARA / PSEUDONIMIZAÇÃO — Art. 6º (necessidade) / Art. 13 ───────────
-- Funções puras p/ exibir PII de forma minimizada em telas NÃO privilegiadas.
-- IMMUTABLE: dependem só do argumento, sem estado/IO — seguras p/ índices e
-- planejamento. Determinísticas.

-- CPF → '***.456.789-**' (revela só o miolo; mascara 1º bloco e dígitos
-- verificadores). Normaliza a entrada (aceita '123.456.789-00' ou '12345678900')
-- removendo tudo que não é dígito. Se não houver 11 dígitos, devolve um
-- placeholder seguro (não vaza a entrada original).
CREATE OR REPLACE FUNCTION sz_mask_cpf(p_cpf text)
RETURNS text
LANGUAGE sql
IMMUTABLE
AS $$
    SELECT CASE
        WHEN p_cpf IS NULL THEN NULL
        WHEN length(regexp_replace(p_cpf, '[^0-9]', '', 'g')) <> 11 THEN '***.***.***-**'
        ELSE '***.'
             || substr(regexp_replace(p_cpf, '[^0-9]', '', 'g'), 4, 3) || '.'
             || substr(regexp_replace(p_cpf, '[^0-9]', '', 'g'), 7, 3) || '-**'
    END;
$$;

-- AUDIT-2026-06-21 #30: SINALIZAÇÃO HONESTA. Esta primitiva NÃO tem chamador hoje
-- — toda exibição de CPF no sistema ainda é integral. Ela NÃO comprova, por si só,
-- conformidade de minimização: é uma primitiva DISPONÍVEL p/ wiring futuro (ex.:
-- repassar CPF mascarado a papéis inferiores no export do OL — exige edição no Go,
-- fora do escopo desta migração). A retenção (sz_anonymize_old_order_pii) ANULA o
-- CPF após 2 anos em vez de mascarar, então não usa esta função. NÃO remover sem
-- antes auditar dependências de exibição. (sz_mask_email, ao contrário, JÁ é usada
-- por sz_anonymize_old_order_pii — ver 401/441.)
COMMENT ON FUNCTION sz_mask_cpf(text) IS
    'LGPD: máscara de CPF p/ exibição não privilegiada (minimização, Art. 6º). '
    'Revela só o miolo: ''***.XXX.XXX-**''. Aceita CPF formatado ou só dígitos. '
    'Pura/IMMUTABLE. ATENÇÃO: SEM chamador atualmente — primitiva aguardando wiring '
    '(export de papel inferior). NÃO comprova conformidade até ser plugada.';

-- E-mail → 'a***@dominio.com' (revela só a 1ª letra da parte local). Preserva o
-- domínio (útil p/ triagem) e oculta o identificador pessoal. Robusta a entradas
-- sem '@' (devolve placeholder).
CREATE OR REPLACE FUNCTION sz_mask_email(p_email text)
RETURNS text
LANGUAGE sql
IMMUTABLE
AS $$
    SELECT CASE
        WHEN p_email IS NULL THEN NULL
        WHEN position('@' IN p_email) < 2 THEN '***'   -- sem '@' ou local vazio
        ELSE substr(split_part(p_email, '@', 1), 1, 1)
             || '***@'
             || split_part(p_email, '@', 2)
    END;
$$;

COMMENT ON FUNCTION sz_mask_email(text) IS
    'LGPD: máscara de e-mail p/ exibição não privilegiada (minimização, Art. 6º). '
    'Revela só a 1ª letra da parte local: ''a***@dominio.com''. Pura/IMMUTABLE.';


-- ── 5. ROPA ESTRUTURADO — Art. 37 (registro das operações de tratamento) ────
-- O controlador deve manter registro das operações de tratamento de dados
-- pessoais. Esta tabela é o ROPA (Records of Processing Activities) em forma
-- estruturada e consultável: cada linha é uma ATIVIDADE de tratamento com sua
-- finalidade, base legal (Art. 7º), categorias de dado, prazo de retenção e
-- com quem se compartilha. UNIQUE(atividade) garante seed idempotente.
CREATE TABLE IF NOT EXISTS senderzz_processing_records (
    id                BIGINT       NOT NULL GENERATED ALWAYS AS IDENTITY,
    atividade         VARCHAR(120) NOT NULL,            -- nome curto da operação (chave natural)
    finalidade        TEXT         NOT NULL,            -- por que se trata o dado (Art. 6º, I)
    base_legal        VARCHAR(120) NOT NULL,            -- hipótese do Art. 7º (ex.: execução de contrato)
    categorias_dados  TEXT         NOT NULL,            -- categorias de dado pessoal tratadas
    retencao          TEXT         NULL,                -- prazo/critério de retenção
    compartilhado_com TEXT         NULL,                -- operadores/terceiros que recebem o dado
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id),
    CONSTRAINT uq_processing_atividade UNIQUE (atividade)
);
CREATE INDEX IF NOT EXISTS idx_processing_base_legal ON senderzz_processing_records (base_legal);

COMMENT ON TABLE senderzz_processing_records IS
    'LGPD Art. 37: ROPA — registro estruturado das operações de tratamento '
    '(atividade, finalidade, base legal Art. 7º, categorias, retenção, '
    'compartilhamento). UNIQUE(atividade) torna o seed idempotente.';

-- Seed das operações conhecidas da plataforma. ON CONFLICT DO NOTHING torna o
-- INSERT idempotente: 2ª passada não duplica nem erra. Para ATUALIZAR um
-- registro, edite a linha diretamente no banco (não via re-run deste seed).
INSERT INTO senderzz_processing_records
    (atividade, finalidade, base_legal, categorias_dados, retencao, compartilhado_com)
VALUES
    ('checkout',
     'Processar a compra: identificar o comprador, calcular frete e emitir cobrança.',
     'Execução de contrato (Art. 7º, V)',
     'Nome, CPF, e-mail, telefone, endereço de entrega, dados do pedido.',
     'Pelo prazo contratual + guarda fiscal/contábil (CTN, 5 anos).',
     'Gateway de pagamento (PIX), Melhor Envio (cálculo de frete).'),

    ('entrega',
     'Realizar a entrega do pedido (transportadora ou motoboy próprio) e rastreio.',
     'Execução de contrato (Art. 7º, V)',
     'Nome, telefone, endereço de entrega, complemento, geolocalização da rota.',
     'Pelo ciclo da entrega + prazo de disputa/comprovação.',
     'Melhor Envio / transportadoras, motoboy designado, OL responsável.'),

    ('carteira',
     'Operar a carteira pré-paga: recargas via PIX, débitos de frete e saldo.',
     'Execução de contrato (Art. 7º, V) / obrigação legal fiscal (Art. 7º, II)',
     'Identificação do titular, chave/transações PIX, histórico de saldo.',
     'Guarda fiscal/contábil e antifraude (mínimo 5 anos).',
     'Gateway de pagamento / instituição financeira do PIX.'),

    ('afiliados',
     'Gerir programa de afiliados: vínculos, comissões e repasses (COD wallet).',
     'Execução de contrato (Art. 7º, V)',
     'Identificação do afiliado/produtor, dados bancários/PIX, valores de comissão.',
     'Pelo vínculo + guarda fiscal/contábil (5 anos).',
     'Instituição financeira para repasse; produtor/afiliado da contraparte.')
ON CONFLICT (atividade) DO NOTHING;
