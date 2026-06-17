-- =============================================================================
-- Senderzz — Schema Postgres 16 para o módulo Afiliados + Carteira COD (Fase 3)
-- Equivalente às tabelas wp_senderzz_affiliates*, wp_senderzz_cod_* do MySQL / WordPress.
--
-- Convenções de conversão MySQL → Postgres (idênticas ao schema-motoboy.sql):
--   BIGINT UNSIGNED          → BIGINT
--   TINYINT(1)               → BOOLEAN
--   DATETIME                 → TIMESTAMPTZ  (novos dados — PHP já escrevia UTC aqui)
--   AUTO_INCREMENT           → GENERATED ALWAYS AS IDENTITY
--   ENUM(...)                → VARCHAR(20) CHECK (col IN (...))
--   ON UPDATE CURRENT_TIMESTAMP → atualizado explicitamente pelo serviço Go
--
-- Nota sobre timezone:
--   Diferente do motoboy (que grava wall-clock Brasília em DATETIME sem tz),
--   as tabelas de afiliados e COD são criadas do zero no Postgres e usam
--   TIMESTAMPTZ (UTC). O serviço Go converte para America/Sao_Paulo na camada
--   de apresentação, se necessário.
-- =============================================================================

-- Garante re-executabilidade em dev (DROP em ordem reversa de FK).
DROP TABLE IF EXISTS senderzz_cod_ledger            CASCADE;
DROP TABLE IF EXISTS senderzz_cod_wallet            CASCADE;
DROP TABLE IF EXISTS senderzz_affiliate_commissions CASCADE;
DROP TABLE IF EXISTS senderzz_affiliate_links       CASCADE;
DROP TABLE IF EXISTS senderzz_affiliate_invites     CASCADE;
DROP TABLE IF EXISTS senderzz_affiliates            CASCADE;

-- =============================================================================
-- senderzz_affiliates — Vínculo produtor ↔ afiliado por produto
-- No MySQL: wp_senderzz_affiliates
-- =============================================================================
CREATE TABLE senderzz_affiliates (
    id           BIGINT       NOT NULL GENERATED ALWAYS AS IDENTITY,
    produtor_id  BIGINT       NOT NULL,
    -- produtor_id: wp_users.ID do produtor (dono do produto)
    afiliado_id  BIGINT       NOT NULL,
    -- afiliado_id: wp_users.ID do afiliado
    produto_id   BIGINT       NOT NULL,
    -- produto_id: WooCommerce product ID (wp_posts.ID)
    status       VARCHAR(20)  NOT NULL DEFAULT 'pending'
                     CHECK (status IN ('pending','active','paused','revoked')),
    -- pending = aguardando aprovação do produtor
    -- active  = vínculo ativo, comissões sendo geradas
    -- paused  = produtor pausou o vínculo
    -- revoked = produtor encerrou o vínculo
    comissao_pct DECIMAL(5,2) NOT NULL DEFAULT 0.00,
    -- comissao_pct: percentual de comissão (0.00–100.00)
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id),
    -- Unicidade: um afiliado só pode ter um vínculo ativo por produto+produtor.
    CONSTRAINT uq_affiliates_produtor_afiliado_produto
        UNIQUE (produtor_id, afiliado_id, produto_id)
);

COMMENT ON TABLE senderzz_affiliates IS 'Vínculos produtor–afiliado por produto (Fase 3 Strangler Fig)';
COMMENT ON COLUMN senderzz_affiliates.comissao_pct IS 'Percentual de comissão negociado entre produtor e afiliado';

CREATE INDEX idx_affiliates_produtor  ON senderzz_affiliates (produtor_id);
CREATE INDEX idx_affiliates_afiliado  ON senderzz_affiliates (afiliado_id);
CREATE INDEX idx_affiliates_produto   ON senderzz_affiliates (produto_id);
CREATE INDEX idx_affiliates_status    ON senderzz_affiliates (status);

-- =============================================================================
-- senderzz_affiliate_links — Links de checkout rastreados por afiliado
-- No MySQL: wp_senderzz_affiliate_links
-- =============================================================================
CREATE TABLE senderzz_affiliate_links (
    id           BIGINT       NOT NULL GENERATED ALWAYS AS IDENTITY,
    affiliate_id BIGINT       NOT NULL,
    -- affiliate_id: FK → senderzz_affiliates(id)
    link_token   VARCHAR(64)  NOT NULL,
    -- link_token: hex aleatório de 32 bytes (64 chars hex) único por link
    produto_id   BIGINT       NOT NULL,
    -- produto_id: WooCommerce product ID associado ao link
    active       BOOLEAN      NOT NULL DEFAULT TRUE,
    clicks       INTEGER      NOT NULL DEFAULT 0,
    -- clicks: contador de acessos ao link de checkout
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id),
    CONSTRAINT uq_affiliate_links_token UNIQUE (link_token),
    CONSTRAINT fk_affiliate_links_affiliate
        FOREIGN KEY (affiliate_id) REFERENCES senderzz_affiliates (id) ON DELETE CASCADE
);

COMMENT ON TABLE senderzz_affiliate_links IS 'Links de checkout rastreados gerados por afiliados';
COMMENT ON COLUMN senderzz_affiliate_links.link_token IS '32 bytes hex aleatório — identificador único do link';
COMMENT ON COLUMN senderzz_affiliate_links.clicks IS 'Contador de acessos ao link (incrementado no redirect)';

CREATE INDEX idx_aff_links_affiliate ON senderzz_affiliate_links (affiliate_id);
CREATE INDEX idx_aff_links_produto   ON senderzz_affiliate_links (produto_id);
CREATE INDEX idx_aff_links_active    ON senderzz_affiliate_links (active);

-- =============================================================================
-- senderzz_affiliate_commissions — Comissões por pedido
-- No MySQL: wp_senderzz_affiliate_commissions
-- =============================================================================
CREATE TABLE senderzz_affiliate_commissions (
    id           BIGINT       NOT NULL GENERATED ALWAYS AS IDENTITY,
    affiliate_id BIGINT       NOT NULL,
    -- affiliate_id: FK → senderzz_affiliates(id)
    order_id     BIGINT       NOT NULL,
    -- order_id: WooCommerce order ID (wp_posts.ID / wp_wc_orders.id)
    valor        DECIMAL(10,2) NOT NULL DEFAULT 0.00,
    -- valor: valor em R$ da comissão calculado no momento da venda
    status       VARCHAR(20)  NOT NULL DEFAULT 'pendente'
                     CHECK (status IN ('pendente','aprovada','paga','estornada')),
    -- pendente  = pedido pago mas comissão aguardando confirmação de entrega
    -- aprovada  = produtora aprovou o repasse
    -- paga      = comissão transferida ao afiliado
    -- estornada = pedido cancelado/devolvido, comissão revertida
    referencia   VARCHAR(100) NULL,
    -- referencia: chave de idempotência (ex: "comm_order_12345_aff_67")
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id),
    CONSTRAINT fk_commissions_affiliate
        FOREIGN KEY (affiliate_id) REFERENCES senderzz_affiliates (id) ON DELETE RESTRICT,
    -- Impede double-write de comissão para o mesmo pedido+afiliado.
    CONSTRAINT uq_commissions_affiliate_order UNIQUE (affiliate_id, order_id)
);

COMMENT ON TABLE senderzz_affiliate_commissions IS 'Comissões geradas por venda via link de afiliado';
COMMENT ON COLUMN senderzz_affiliate_commissions.referencia IS 'Chave de idempotência para double-write do PHP';

CREATE INDEX idx_commissions_affiliate ON senderzz_affiliate_commissions (affiliate_id);
CREATE INDEX idx_commissions_order     ON senderzz_affiliate_commissions (order_id);
CREATE INDEX idx_commissions_status    ON senderzz_affiliate_commissions (status);
CREATE INDEX idx_commissions_created   ON senderzz_affiliate_commissions (created_at);

-- =============================================================================
-- senderzz_affiliate_invites — Convites de afiliação enviados por produtores
-- No MySQL: wp_senderzz_affiliate_invites
-- =============================================================================
CREATE TABLE senderzz_affiliate_invites (
    id          BIGINT        NOT NULL GENERATED ALWAYS AS IDENTITY,
    produtor_id BIGINT        NOT NULL,
    -- produtor_id: wp_users.ID do produtor que enviou o convite
    email       VARCHAR(255)  NOT NULL,
    -- email: endereço do convidado
    token       VARCHAR(64)   NOT NULL,
    -- token: hex único para o link de aceite do convite
    expires_at  TIMESTAMPTZ   NOT NULL,
    -- expires_at: prazo de validade (padrão: +7 dias da criação)
    used_at     TIMESTAMPTZ   NULL,
    -- used_at: NULL = não utilizado; preenchido ao aceitar o convite
    created_at  TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id),
    CONSTRAINT uq_affiliate_invites_token UNIQUE (token)
);

COMMENT ON TABLE senderzz_affiliate_invites IS 'Convites de afiliação enviados por produtores (expiram em 7 dias)';
COMMENT ON COLUMN senderzz_affiliate_invites.used_at IS 'NULL = convite pendente; preenchido quando o afiliado aceitar';

CREATE INDEX idx_invites_produtor   ON senderzz_affiliate_invites (produtor_id);
CREATE INDEX idx_invites_email      ON senderzz_affiliate_invites (email);
CREATE INDEX idx_invites_expires_at ON senderzz_affiliate_invites (expires_at);

-- =============================================================================
-- senderzz_cod_wallet — Carteira COD (Cash on Delivery) por usuário
-- No MySQL: wp_senderzz_cod_wallet
--
-- Um registro por user_id (motoboy ou produtor que opera COD).
-- Saldo representa o total de valores COD coletados ainda não repassados.
-- =============================================================================
CREATE TABLE senderzz_cod_wallet (
    id         BIGINT        NOT NULL GENERATED ALWAYS AS IDENTITY,
    user_id    BIGINT        NOT NULL,
    -- user_id: wp_users.ID (motoboy ou produtor)
    saldo      DECIMAL(10,2) NOT NULL DEFAULT 0.00,
    -- saldo: valor acumulado de entregas COD pendentes de repasse
    created_at TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id),
    CONSTRAINT uq_cod_wallet_user UNIQUE (user_id)
);

COMMENT ON TABLE senderzz_cod_wallet IS 'Carteira COD por usuário — valores coletados na entrega aguardando repasse';
COMMENT ON COLUMN senderzz_cod_wallet.saldo IS 'Saldo atual de COD pendente de repasse (nunca negativo)';

CREATE INDEX idx_cod_wallet_user ON senderzz_cod_wallet (user_id);

-- =============================================================================
-- senderzz_cod_ledger — Livro-caixa da carteira COD
-- No MySQL: wp_senderzz_cod_ledger
--
-- Toda movimentação de saldo COD gera uma entrada aqui.
-- Idempotência via UNIQUE (user_id, referencia, tipo) — mesmo padrão
-- do tpc_transacoes na carteira de frete (CRIT-01 pattern).
-- =============================================================================
CREATE TABLE senderzz_cod_ledger (
    id         BIGINT        NOT NULL GENERATED ALWAYS AS IDENTITY,
    user_id    BIGINT        NOT NULL,
    tipo       VARCHAR(20)   NOT NULL
                   CHECK (tipo IN ('credito','debito','antecipacao')),
    -- credito     = valor COD coletado na entrega creditado ao motoboy/produtor
    -- debito      = repasse realizado (operador confirma recebimento do dinheiro)
    -- antecipacao = antecipação de saldo COD solicitada pelo usuário
    valor      DECIMAL(10,2) NOT NULL,
    -- valor: sempre positivo; o tipo define a direção do fluxo
    pedido_id  BIGINT        NULL,
    -- pedido_id: sz_motoboy_pedidos.id (NULL para antecipações e débitos manuais)
    descricao  VARCHAR(255)  NULL,
    referencia VARCHAR(100)  NULL,
    -- referencia: chave de idempotência — ex: "cod_delivery_pedido_42"
    created_at TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id),
    -- Idempotência: impede double-write do mesmo evento COD.
    CONSTRAINT uq_cod_ledger_ref UNIQUE (user_id, referencia, tipo)
);

COMMENT ON TABLE senderzz_cod_ledger IS 'Livro-caixa de movimentações COD — espelha padrão tpc_transacoes';
COMMENT ON COLUMN senderzz_cod_ledger.referencia IS 'Chave de idempotência — UNIQUE(user_id,referencia,tipo)';
COMMENT ON COLUMN senderzz_cod_ledger.tipo IS 'credito=entrega COD; debito=repasse; antecipacao=solicitação de adiantamento';

CREATE INDEX idx_cod_ledger_user      ON senderzz_cod_ledger (user_id);
CREATE INDEX idx_cod_ledger_pedido    ON senderzz_cod_ledger (pedido_id);
CREATE INDEX idx_cod_ledger_tipo      ON senderzz_cod_ledger (tipo);
CREATE INDEX idx_cod_ledger_created   ON senderzz_cod_ledger (created_at);
