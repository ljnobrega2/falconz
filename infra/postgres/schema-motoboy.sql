-- =============================================================================
-- Senderzz — Schema Postgres 16 para o módulo Motoboy (Fase 1 Strangler Fig)
-- Equivalente às tabelas sz_motoboy_* do MySQL / WordPress (prefixo wp_ removido)
--
-- Convenções de conversão MySQL → Postgres:
--   BIGINT UNSIGNED          → BIGINT  (Postgres não tem UNSIGNED)
--   TINYINT(1)               → BOOLEAN
--   TINYINT (sem 1)          → SMALLINT
--   INT UNSIGNED             → INTEGER
--   DATETIME                 → TIMESTAMP WITHOUT TIME ZONE
--       ATENÇÃO: o PHP grava horário de Brasília (America/Sao_Paulo) em colunas
--       DATETIME sem timezone. Portanto usamos TIMESTAMP WITHOUT TIME ZONE
--       para preservar a semântica de wall-clock — não TIMESTAMPTZ, que
--       interpretaria o valor como UTC e deslocaria 3h em produção.
--       O serviço Go deve usar time.Local = America/Sao_Paulo ao ler essas colunas.
--   LONGTEXT / TEXT          → TEXT      (TEXT em Postgres é ilimitado)
--   AUTO_INCREMENT           → GENERATED ALWAYS AS IDENTITY
--   ENUM(...)                → VARCHAR(20/50) CHECK (col IN (...))
--       (CHECK em vez de tipo ENUM nativo do Postgres = portabilidade e migrations simples)
--   ON UPDATE CURRENT_TIMESTAMP → sem equivalente direto; usar trigger ou atualizar
--       explicitamente no código Go / pgloader não suporta nativament esse comportamento.
--   CURRENT_TIMESTAMP        → NOW()
--
-- Nota sobre sz_portal_tickets / sz_portal_ticket_msgs:
--   Essas tabelas vivem em database.php mas pertencem ao módulo Portal (Fase futura).
--   Não estão incluídas aqui para manter o escopo da Fase 1 restrito ao Motoboy.
-- =============================================================================

-- Garante que o script pode ser re-executado em ambiente dev (DROP em ordem reversa
-- de dependência para não violar FKs futuras).
DROP TABLE IF EXISTS sz_motoboy_audit        CASCADE;
DROP TABLE IF EXISTS sz_motoboy_fechamento   CASCADE;
DROP TABLE IF EXISTS sz_motoboy_comprovantes CASCADE;
DROP TABLE IF EXISTS sz_motoboy_pedidos      CASCADE;
DROP TABLE IF EXISTS sz_motoboy_zona_pivot   CASCADE;
DROP TABLE IF EXISTS sz_motoboys             CASCADE;
DROP TABLE IF EXISTS sz_motoboy_cep_zonas    CASCADE;
DROP TABLE IF EXISTS sz_motoboy_zonas        CASCADE;
DROP TABLE IF EXISTS sz_motoboy_cds          CASCADE;

-- =============================================================================
-- sz_motoboy_cds — Centros de Distribuição
-- No MySQL: wp_sz_motoboy_cds
-- =============================================================================
CREATE TABLE sz_motoboy_cds (
    id         BIGINT                   NOT NULL GENERATED ALWAYS AS IDENTITY,
    nome       VARCHAR(100)             NOT NULL,
    cidade     VARCHAR(100)             NOT NULL,
    uf         CHAR(2)                  NOT NULL DEFAULT 'SP',
    endereco   VARCHAR(255)             NULL,
    lat        DECIMAL(10,7)            NULL,
    lng        DECIMAL(10,7)            NULL,
    ativo      BOOLEAN                  NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP WITHOUT TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITHOUT TIME ZONE NOT NULL DEFAULT NOW(),
    -- updated_at: no MySQL havia ON UPDATE CURRENT_TIMESTAMP.
    -- No Postgres, o serviço Go deve setar explicitamente ao fazer UPDATE.
    PRIMARY KEY (id)
);

COMMENT ON TABLE sz_motoboy_cds IS 'Centros de Distribuição (CDs) do módulo Motoboy';
COMMENT ON COLUMN sz_motoboy_cds.ativo IS 'TRUE = CD ativo, FALSE = inativo';

-- =============================================================================
-- sz_motoboy_zonas — Zonas de entrega por CD
-- No MySQL: wp_sz_motoboy_zonas
-- =============================================================================
CREATE TABLE sz_motoboy_zonas (
    id                  BIGINT                   NOT NULL GENERATED ALWAYS AS IDENTITY,
    cd_id               BIGINT                   NOT NULL,
    nome                VARCHAR(100)             NOT NULL,
    descricao           VARCHAR(255)             NULL,
    dias_funcionamento  VARCHAR(20)              NOT NULL DEFAULT '1,2,3,4,5,6',
    -- dias_funcionamento: CSV de dias da semana (0=Dom, 1=Seg, ..., 6=Sáb)
    cutoff_horarios     TEXT                     NULL,
    -- cutoff_horarios: JSON {"0":"21:00","1":"21:00",...} por dia da semana
    ativo               BOOLEAN                  NOT NULL DEFAULT TRUE,
    created_at          TIMESTAMP WITHOUT TIME ZONE NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id)
);

COMMENT ON TABLE sz_motoboy_zonas IS 'Zonas de entrega associadas a CDs';
COMMENT ON COLUMN sz_motoboy_zonas.cutoff_horarios IS 'JSON: horário de corte por dia {"0":"21:00",...}';

CREATE INDEX idx_zonas_cd_id ON sz_motoboy_zonas (cd_id);

-- =============================================================================
-- sz_motoboy_cep_zonas — Faixas de CEP mapeadas para zonas
-- No MySQL: wp_sz_motoboy_cep_zonas
-- =============================================================================
CREATE TABLE sz_motoboy_cep_zonas (
    id         BIGINT                   NOT NULL GENERATED ALWAYS AS IDENTITY,
    zona_id    BIGINT                   NOT NULL,
    cep_inicio CHAR(8)                  NOT NULL,
    cep_fim    CHAR(8)                  NOT NULL,
    created_at TIMESTAMP WITHOUT TIME ZONE NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id)
);

COMMENT ON TABLE sz_motoboy_cep_zonas IS 'Faixas de CEP mapeadas para zonas de entrega';

CREATE INDEX idx_cep_zonas_zona_id ON sz_motoboy_cep_zonas (zona_id);
CREATE INDEX idx_cep_zonas_cep     ON sz_motoboy_cep_zonas (cep_inicio, cep_fim);

-- =============================================================================
-- sz_motoboy_zona_pivot — Motoboy pode atuar em múltiplas zonas (N:N)
-- No MySQL: wp_sz_motoboy_zona_pivot
-- =============================================================================
CREATE TABLE sz_motoboy_zona_pivot (
    motoboy_id BIGINT NOT NULL,
    zona_id    BIGINT NOT NULL,
    PRIMARY KEY (motoboy_id, zona_id)
);

COMMENT ON TABLE sz_motoboy_zona_pivot IS 'Tabela pivot: motoboy × zona (N:N)';

CREATE INDEX idx_zona_pivot_zona ON sz_motoboy_zona_pivot (zona_id);

-- =============================================================================
-- sz_motoboys — Motoboys cadastrados
-- No MySQL: wp_sz_motoboys
-- =============================================================================
CREATE TABLE sz_motoboys (
    id          BIGINT                   NOT NULL GENERATED ALWAYS AS IDENTITY,
    cd_id       BIGINT                   NOT NULL,
    zona_id     BIGINT                   NULL,
    nome        VARCHAR(100)             NOT NULL,
    telefone    VARCHAR(20)              NOT NULL,
    cpf         VARCHAR(14)              NULL,
    email       VARCHAR(100)             NULL,
    tipo_pgto   VARCHAR(20)              NOT NULL DEFAULT 'autonomo'
                    CHECK (tipo_pgto IN ('clt', 'pj', 'autonomo')),
    ativo       BOOLEAN                  NOT NULL DEFAULT TRUE,
    token_app   VARCHAR(64)              NULL,
    -- token_app: autenticação X-MB-Token no PWA do motoboy
    pin_hash    VARCHAR(255)             NULL,
    -- pin_hash: bcrypt do PIN de 4–8 dígitos (adicionado v63 via ALTER — incluído inline aqui)
    ultimo_lat  DECIMAL(10,7)            NULL,
    ultimo_lng  DECIMAL(10,7)            NULL,
    ultimo_ping TIMESTAMP WITHOUT TIME ZONE NULL,
    created_at  TIMESTAMP WITHOUT TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMP WITHOUT TIME ZONE NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id),
    UNIQUE (token_app)
);

COMMENT ON TABLE sz_motoboys IS 'Motoboys cadastrados na operação';
COMMENT ON COLUMN sz_motoboys.token_app  IS 'Token de autenticação enviado no header X-MB-Token';
COMMENT ON COLUMN sz_motoboys.pin_hash   IS 'bcrypt do PIN de acesso (4–8 dígitos). Coluna adicionada via ALTER v63 — incluída inline no schema Postgres.';

CREATE INDEX idx_motoboys_cd_id   ON sz_motoboys (cd_id);
CREATE INDEX idx_motoboys_zona_id ON sz_motoboys (zona_id);

-- =============================================================================
-- sz_motoboy_pedidos — Pedidos de entrega (tabela principal do módulo)
-- No MySQL: wp_sz_motoboy_pedidos
--
-- Nota sobre colunas adicionadas via ALTER no MySQL:
--   As colunas abaixo foram adicionadas por ALTER TABLE em versões posteriores
--   do plugin e estão incluídas inline neste CREATE para o Postgres:
--     - dest_complemento  (Migration: dest_complemento separado do número)
--     - dest_produto      (v252)
--     - quantidade        (v320)
--     - recebedor_tipo    (v252)
--     - baixa_por         (v252)
--     - baixa_admin_user_id (v252)
--     - baixa_motoboy_id  (v252)
--     - baixa_at          (v252)
--     - comprovantes_count (v252)
-- =============================================================================
CREATE TABLE sz_motoboy_pedidos (
    id                   BIGINT                   NOT NULL GENERATED ALWAYS AS IDENTITY,
    wc_order_id          BIGINT                   NOT NULL,
    cd_id                BIGINT                   NOT NULL,
    zona_id              BIGINT                   NOT NULL,
    motoboy_id           BIGINT                   NULL,
    status               VARCHAR(20)              NOT NULL DEFAULT 'agendado'
                             CHECK (status IN ('agendado','embalado','em_rota','entregue',
                                               'frustrado','cancelado','aprovado',
                                               'a_caminho','reagendado')),
    -- Status legados (aprovado, a_caminho, reagendado) mantidos no CHECK para
    -- não quebrar dados históricos migrados. Fluxo novo: agendado → embalado
    -- → em_rota → entregue | frustrado | cancelado.

    dest_nome            VARCHAR(150)             NULL,
    dest_telefone        VARCHAR(20)              NULL,
    dest_cep             CHAR(8)                  NOT NULL,
    dest_endereco        VARCHAR(255)             NULL,
    dest_numero          VARCHAR(20)              NULL,
    dest_complemento     VARCHAR(100)             NULL,
    -- dest_complemento: adicionado via ALTER (Migration: separado do número)
    dest_produto         VARCHAR(500)             NULL,
    -- dest_produto: resumo dos itens "nome x qty" (adicionado v252 via ALTER)
    quantidade           INTEGER                  NOT NULL DEFAULT 0,
    -- quantidade: adicionado v320 via ALTER; INT UNSIGNED → INTEGER (sem UNSIGNED no Postgres)
    dest_bairro          VARCHAR(100)             NULL,
    dest_cidade          VARCHAR(100)             NULL,
    dest_uf              CHAR(2)                  NULL,
    dest_lat             DECIMAL(10,7)            NULL,
    dest_lng             DECIMAL(10,7)            NULL,

    valor_pedido         DECIMAL(10,2)            NOT NULL DEFAULT 0.00,
    valor_taxa           DECIMAL(10,2)            NOT NULL DEFAULT 0.00,
    valor_taxa_frustrado DECIMAL(10,2)            NOT NULL DEFAULT 0.00,
    pgto_dinheiro        DECIMAL(10,2)            NOT NULL DEFAULT 0.00,
    pgto_pix             DECIMAL(10,2)            NOT NULL DEFAULT 0.00,
    pgto_cartao          DECIMAL(10,2)            NOT NULL DEFAULT 0.00,

    recebedor_cpf        VARCHAR(14)              NULL,
    recebedor_nome       VARCHAR(150)             NULL,
    -- recebedor_nome: adicionado v63 via ALTER — incluído inline aqui
    recebedor_tipo       VARCHAR(20)              NULL DEFAULT 'cliente'
                             CHECK (recebedor_tipo IS NULL OR recebedor_tipo IN ('cliente','terceiro')),
    -- recebedor_tipo: cliente=destinatário, terceiro=outra pessoa (adicionado v252)
    recebedor_assinatura TEXT                     NULL,
    -- recebedor_assinatura: LONGTEXT no MySQL → TEXT (ilimitado no Postgres)

    baixa_por            VARCHAR(20)              NULL DEFAULT 'motoboy'
                             CHECK (baixa_por IS NULL OR baixa_por IN ('motoboy','admin')),
    -- Quem deu a baixa (adicionado v252 via ALTER)
    baixa_admin_user_id  BIGINT                   NULL,
    -- WP user_id se baixa foi por admin (adicionado v252)
    baixa_motoboy_id     BIGINT                   NULL,
    -- Motoboy que recebe o valor — pode diferir de motoboy_id quando baixa por admin (v252)
    baixa_at             TIMESTAMP WITHOUT TIME ZONE NULL,
    -- Data/hora exata da baixa (adicionado v252)

    entrega_foto         VARCHAR(255)             NULL,
    -- Foto principal (legado — novas fotos usam sz_motoboy_comprovantes)
    comprovantes_count   SMALLINT                 NOT NULL DEFAULT 0,
    -- Qtd de comprovantes enviados (TINYINT → SMALLINT; adicionado v252)
    entrega_lat          DECIMAL(10,7)            NULL,
    entrega_lng          DECIMAL(10,7)            NULL,

    frustrado_motivo     VARCHAR(255)             NULL,
    frustrado_observacao TEXT                     NULL,
    frustrado_isento     BOOLEAN                  NOT NULL DEFAULT FALSE,

    reagendado_para      DATE                     NULL,

    -- Timestamps de transição de status (wall-clock Brasília, sem timezone)
    ts_aprovado          TIMESTAMP WITHOUT TIME ZONE NULL,
    ts_embalado          TIMESTAMP WITHOUT TIME ZONE NULL,
    ts_em_rota           TIMESTAMP WITHOUT TIME ZONE NULL,
    ts_a_caminho         TIMESTAMP WITHOUT TIME ZONE NULL,
    ts_entregue          TIMESTAMP WITHOUT TIME ZONE NULL,
    ts_frustrado         TIMESTAMP WITHOUT TIME ZONE NULL,

    created_at           TIMESTAMP WITHOUT TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMP WITHOUT TIME ZONE NOT NULL DEFAULT NOW(),
    synced_at            TIMESTAMP WITHOUT TIME ZONE NULL,
    -- synced_at: timestamp da última replicação via double-write (internal.go).
    -- NULL = migrado via pgloader (não passou pelo handler Go).

    PRIMARY KEY (id),
    UNIQUE (wc_order_id)
);

COMMENT ON TABLE sz_motoboy_pedidos IS 'Pedidos de entrega do módulo Motoboy (tabela central da Fase 1)';
COMMENT ON COLUMN sz_motoboy_pedidos.quantidade IS 'Total de itens no pedido (adicionado v320 via ALTER no MySQL)';
COMMENT ON COLUMN sz_motoboy_pedidos.recebedor_assinatura IS 'Assinatura do recebedor em base64/URL (LONGTEXT→TEXT)';

CREATE UNIQUE INDEX uk_pedidos_wc_order ON sz_motoboy_pedidos (wc_order_id);
CREATE INDEX idx_pedidos_motoboy_id     ON sz_motoboy_pedidos (motoboy_id);
CREATE INDEX idx_pedidos_status         ON sz_motoboy_pedidos (status);
CREATE INDEX idx_pedidos_cd_zona        ON sz_motoboy_pedidos (cd_id, zona_id);
CREATE INDEX idx_pedidos_created        ON sz_motoboy_pedidos (created_at);

-- =============================================================================
-- sz_motoboy_comprovantes — Fotos/comprovantes de entrega
-- No MySQL: wp_sz_motoboy_comprovantes
-- =============================================================================
CREATE TABLE sz_motoboy_comprovantes (
    id          BIGINT                   NOT NULL GENERATED ALWAYS AS IDENTITY,
    pedido_id   BIGINT                   NOT NULL,
    -- pedido_id: ID em sz_motoboy_pedidos
    wc_order_id BIGINT                   NOT NULL,
    motoboy_id  BIGINT                   NOT NULL,
    tipo_pgto   VARCHAR(20)              NOT NULL DEFAULT 'dinheiro',
    -- tipo_pgto: dinheiro | pix | cartao
    foto_url    VARCHAR(500)             NOT NULL,
    foto_path   VARCHAR(500)             NOT NULL,
    baixa_por   VARCHAR(20)              NOT NULL DEFAULT 'motoboy'
                    CHECK (baixa_por IN ('motoboy','admin')),
    created_at  TIMESTAMP WITHOUT TIME ZONE NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id)
);

COMMENT ON TABLE sz_motoboy_comprovantes IS 'Comprovantes de entrega (fotos) por pedido Motoboy';

CREATE INDEX idx_comprovantes_pedido ON sz_motoboy_comprovantes (pedido_id);
CREATE INDEX idx_comprovantes_order  ON sz_motoboy_comprovantes (wc_order_id);

-- =============================================================================
-- sz_motoboy_fechamento — Fechamento diário por motoboy
-- No MySQL: wp_sz_motoboy_fechamento
-- =============================================================================
CREATE TABLE sz_motoboy_fechamento (
    id                 BIGINT                   NOT NULL GENERATED ALWAYS AS IDENTITY,
    motoboy_id         BIGINT                   NOT NULL,
    cd_id              BIGINT                   NOT NULL,
    data_fechamento    DATE                     NOT NULL,
    total_pedidos      INTEGER                  NOT NULL DEFAULT 0,
    total_entregues    INTEGER                  NOT NULL DEFAULT 0,
    total_frustrados   INTEGER                  NOT NULL DEFAULT 0,
    total_dinheiro     DECIMAL(10,2)            NOT NULL DEFAULT 0.00,
    total_pix          DECIMAL(10,2)            NOT NULL DEFAULT 0.00,
    total_cartao       DECIMAL(10,2)            NOT NULL DEFAULT 0.00,
    total_a_repassar   DECIMAL(10,2)            NOT NULL DEFAULT 0.00,
    repasse_confirmado BOOLEAN                  NOT NULL DEFAULT FALSE,
    repasse_ts         TIMESTAMP WITHOUT TIME ZONE NULL,
    alan_confirmou     BOOLEAN                  NOT NULL DEFAULT FALSE,
    alan_ts            TIMESTAMP WITHOUT TIME ZONE NULL,
    obs                TEXT                     NULL,
    created_at         TIMESTAMP WITHOUT TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMP WITHOUT TIME ZONE NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id),
    UNIQUE (motoboy_id, data_fechamento)
);

COMMENT ON TABLE sz_motoboy_fechamento IS 'Fechamento financeiro diário por motoboy e CD';
COMMENT ON COLUMN sz_motoboy_fechamento.alan_confirmou IS 'TRUE = Alan (operador) confirmou o repasse';

CREATE INDEX idx_fechamento_cd_id ON sz_motoboy_fechamento (cd_id);
CREATE INDEX idx_fechamento_data  ON sz_motoboy_fechamento (data_fechamento);

-- =============================================================================
-- sz_motoboy_audit — Log de auditoria de ações no módulo Motoboy
-- No MySQL: wp_sz_motoboy_audit
-- =============================================================================
CREATE TABLE sz_motoboy_audit (
    id          BIGINT                   NOT NULL GENERATED ALWAYS AS IDENTITY,
    pedido_id   BIGINT                   NULL,
    motoboy_id  BIGINT                   NULL,
    actor_tipo  VARCHAR(20)              NOT NULL DEFAULT 'sistema'
                    CHECK (actor_tipo IN ('sistema','alan','motoboy','admin')),
    actor_id    BIGINT                   NULL,
    acao        VARCHAR(100)             NOT NULL,
    de_status   VARCHAR(50)             NULL,
    para_status VARCHAR(50)             NULL,
    meta_json   TEXT                     NULL,
    -- meta_json: LONGTEXT no MySQL → TEXT no Postgres (ilimitado)
    ip_address  VARCHAR(64)             NULL,
    created_at  TIMESTAMP WITHOUT TIME ZONE NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id)
);

COMMENT ON TABLE sz_motoboy_audit IS 'Auditoria de todas as ações do módulo Motoboy';
COMMENT ON COLUMN sz_motoboy_audit.meta_json IS 'Dados extras da ação em JSON (LONGTEXT→TEXT)';

CREATE INDEX idx_audit_pedido  ON sz_motoboy_audit (pedido_id);
CREATE INDEX idx_audit_motoboy ON sz_motoboy_audit (motoboy_id);
CREATE INDEX idx_audit_created ON sz_motoboy_audit (created_at);

-- =============================================================================
-- sz_motoboy_otps — OTPs temporários para auth do app motoboy (Go-only, não existe no MySQL)
-- =============================================================================
CREATE TABLE sz_motoboy_otps (
    motoboy_id  BIGINT       NOT NULL,
    otp         CHAR(6)      NOT NULL,
    expires_at  TIMESTAMP WITHOUT TIME ZONE NOT NULL,
    tentativas  SMALLINT     NOT NULL DEFAULT 0,
    PRIMARY KEY (motoboy_id),
    CONSTRAINT fk_otp_motoboy FOREIGN KEY (motoboy_id) REFERENCES sz_motoboys (id) ON DELETE CASCADE
);

COMMENT ON TABLE sz_motoboy_otps IS 'OTPs para autenticação do app motoboy (TTL 5min, purged on use)';

-- Índice para limpeza periódica de OTPs expirados.
CREATE INDEX idx_otps_expires ON sz_motoboy_otps (expires_at);
