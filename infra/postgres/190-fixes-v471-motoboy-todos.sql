-- =============================================================================
-- schema-fixes-v471-motoboy-todos.sql
--
-- AUDIT CODE-MOTOBOY-TODOS-08 — DDL faltante referenciado por 8 comentários
-- "TODO: migration" nos handlers do go/motoboy. Os handlers já estão escritos
-- corretamente; só faltava o schema. Este arquivo consolida toda a DDL pendente
-- num único migration aditivo e idempotente.
--
-- Origem de cada bloco (arquivo:linha do TODO no go/motoboy):
--   handlers/motoboy_ops.go:193  → sz_motoboy_localizacoes
--   handlers/motoboy_ops.go:237  → sz_motoboy_fechamento (4 colunas de confirmação)
--   handlers/motoboy_ops.go:435  → sz_motoboy_comprovantes.foto_base64
--   handlers/motoboy_ops.go:567  → sz_motoboy_push_tokens
--   handlers/motoboy_ops.go:907  → sz_motoboy_pedidos (cpf_dispensado + 2 colunas)
--   handlers/internal.go:156     → índice único de auditoria (status_alterado/dia)
--   handlers/alan.go:33          → sz_motoboy_localizacoes (mesma tabela do Ping)
--   handlers/alan.go:262         → sz_alan_push_tokens
--
-- SEGURANÇA / OPERAÇÃO:
--   - Totalmente idempotente: CREATE TABLE IF NOT EXISTS / ADD COLUMN IF NOT EXISTS
--     / CREATE [UNIQUE] INDEX IF NOT EXISTS. Seguro reaplicar.
--   - Aplicar em STAGING e depois em PRODUÇÃO antes de remover os comentários
--     "TODO: migration" dos handlers (não remover os comentários até confirmar
--     que rodou — eles são o aviso de que o schema ainda pode estar incompleto).
--   - SEM CONCURRENTLY no índice único: CONCURRENTLY não roda dentro de bloco de
--     transação e a tabela de auditoria é pequena. CREATE UNIQUE INDEX simples
--     basta.
--   - O índice único de auditoria é PARCIAL (WHERE acao = 'status_alterado'):
--     espelha exatamente o escopo do dedup do handler (internal.go:159, que filtra
--     acao = 'status_alterado'). sz_motoboy_audit é um log append-only com muitos
--     valores de acao (ex.: 'qr_devolucao_declarada' em motoboy_ops.go:181) que
--     PODEM repetir legitimamente para o mesmo pedido/status/dia — um índice
--     table-wide quebraria esses inserts e poderia falhar ao aplicar sobre linhas
--     já duplicadas. Parcial = colisão em apply praticamente impossível (cobre só
--     as linhas que o WHERE NOT EXISTS já protege) e não restringe as demais ações.
-- =============================================================================

-- -----------------------------------------------------------------------------
-- 1. sz_motoboy_localizacoes — última posição GPS por motoboy.
--    Usada por POST /motoboy/ping (upsert) e GET /alan/localizacao (leitura).
--    handlers/motoboy_ops.go:201 (Ping) e handlers/alan.go:41 (Localizacao).
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS sz_motoboy_localizacoes (
    motoboy_id BIGINT           PRIMARY KEY REFERENCES sz_motoboys(id),
    lat        DOUBLE PRECISION NOT NULL,
    lng        DOUBLE PRECISION NOT NULL,
    updated_at TIMESTAMP        NOT NULL DEFAULT NOW()
);

-- -----------------------------------------------------------------------------
-- 2. sz_motoboy_fechamento — colunas de dupla confirmação (motoboy + Alan/OL).
--    handlers/motoboy_ops.go:244 (Fechamento) lê confirmado_motoboy / confirmado_alan.
-- -----------------------------------------------------------------------------
ALTER TABLE sz_motoboy_fechamento
    ADD COLUMN IF NOT EXISTS confirmado_motoboy    BOOLEAN   NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS confirmado_motoboy_at TIMESTAMP,
    ADD COLUMN IF NOT EXISTS confirmado_alan       BOOLEAN   NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS confirmado_alan_at    TIMESTAMP;

-- -----------------------------------------------------------------------------
-- 3. sz_motoboy_comprovantes.foto_base64 — bytes da foto em base64 (fallback ao
--    armazenamento por URL/path). handlers/motoboy_ops.go:439 (Comprovante).
--    NOTA: a tabela já tem foto_url/foto_path; foto_base64 é coluna NOVA aditiva.
-- -----------------------------------------------------------------------------
ALTER TABLE sz_motoboy_comprovantes
    ADD COLUMN IF NOT EXISTS foto_base64 TEXT;

-- -----------------------------------------------------------------------------
-- 4. sz_motoboy_push_tokens — tokens de push do app do motoboy (PWA/FCM).
--    handlers/motoboy_ops.go:578 (PushSubscribe).
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS sz_motoboy_push_tokens (
    id         BIGSERIAL  PRIMARY KEY,
    motoboy_id BIGINT     NOT NULL REFERENCES sz_motoboys(id),
    token      TEXT       NOT NULL,
    plataforma TEXT       NOT NULL DEFAULT 'fcm',
    created_at TIMESTAMP  NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP  NOT NULL DEFAULT NOW(),
    UNIQUE (token)
);

-- -----------------------------------------------------------------------------
-- 5. sz_motoboy_pedidos — dispensa de CPF na entrega (com motivo e timestamp).
--    handlers/motoboy_ops.go:913 (DispensarCPF).
-- -----------------------------------------------------------------------------
ALTER TABLE sz_motoboy_pedidos
    ADD COLUMN IF NOT EXISTS cpf_dispensado        BOOLEAN   NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS cpf_dispensado_motivo TEXT,
    ADD COLUMN IF NOT EXISTS cpf_dispensado_at     TIMESTAMP;

-- -----------------------------------------------------------------------------
-- 6. Índice único PARCIAL de auditoria de mudança de status (idempotência GAP 1-C).
--    handlers/internal.go:159 hoje usa INSERT ... WHERE NOT EXISTS (já idempotente),
--    filtrando acao = 'status_alterado'. Este índice é ADITIVO e PARCIAL (mesmo
--    filtro): garante no nível do banco apenas a unicidade dessa ação por dia, sem
--    afetar as demais ações append-only da auditoria. NÃO trocar o handler para
--    ON CONFLICT neste pacote — ON CONFLICT exige o índice já existindo em runtime;
--    se o migration não tiver rodado, quebraria o endpoint. Manter o WHERE NOT
--    EXISTS e usar este índice apenas como rede de segurança.
--    created_at::date é IMMUTABLE em TIMESTAMP WITHOUT TIME ZONE → expression index válido.
-- -----------------------------------------------------------------------------
CREATE UNIQUE INDEX IF NOT EXISTS uq_audit_status_dia
    ON sz_motoboy_audit (pedido_id, acao, para_status, (created_at::date))
    WHERE acao = 'status_alterado';

-- -----------------------------------------------------------------------------
-- 7. sz_alan_push_tokens — tokens de push do expedidor (Alan).
--    handlers/alan.go:272 (PushSubscribe).
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS sz_alan_push_tokens (
    id         BIGSERIAL  PRIMARY KEY,
    token      TEXT       NOT NULL,
    plataforma TEXT       NOT NULL DEFAULT 'fcm',
    created_at TIMESTAMP  NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP  NOT NULL DEFAULT NOW(),
    UNIQUE (token)
);
