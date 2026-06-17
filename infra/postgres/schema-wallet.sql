-- =============================================================================
-- Senderzz — Schema Postgres 16 para Carteira + PIX (Fase 2 Strangler Fig)
-- Equivalente às tabelas wp_tpc_* do MySQL / WordPress (prefixo wp_ removido)
--
-- Convenções de conversão MySQL → Postgres:
--   BIGINT UNSIGNED          → BIGINT  (Postgres não tem UNSIGNED)
--   DECIMAL(10,2)            → NUMERIC(10,2)  (exato, sem arredondamento float)
--   DATETIME                 → TIMESTAMPTZ DEFAULT NOW()
--       ATENÇÃO: diferente do módulo Motoboy, os campos financeiros (carteira,
--       transacoes, recargas) foram migrados para TIMESTAMPTZ pois:
--       a) operações financeiras exigem auditoria com fuso correto;
--       b) tpc_recargas.expires_at é comparado com NOW() em queries de expiração.
--       O serviço Go lê com UTC; camada de apresentação converte para America/Sao_Paulo.
--   LONGTEXT / TEXT          → TEXT      (TEXT em Postgres é ilimitado)
--   AUTO_INCREMENT           → GENERATED ALWAYS AS IDENTITY
--   ENUM(...)                → VARCHAR(20/50) CHECK (col IN (...))
--   ON DUPLICATE KEY UPDATE  → ON CONFLICT ... DO NOTHING / DO UPDATE
--
-- Garantias de idempotência (S8):
--   tpc_transacoes: UNIQUE(user_id, referencia, tipo) — mesma transação
--     não pode ser inserida duas vezes.
--   tpc_recargas: UNIQUE(me_pix_id) — idempotência por ID de cobrança PIX.
--   tpc_webhook_events: UNIQUE(event_key) — mesmo webhook nunca processado 2x.
--
-- Campos extras no Postgres (não existem no MySQL original):
--   tpc_transacoes.me_order_id   — ID do pedido ME associado à reserva/débito
--   tpc_transacoes.actor_id      — WP user_id de quem executou (admin manual)
--   tpc_transacoes.ip_address    — IP do request de origem (auditoria)
--   tpc_recargas.tx_id           — FK para tpc_transacoes após confirmação
--   tpc_webhook_events.*         — colunas extras de diagnóstico no Postgres
-- =============================================================================

-- Garante que o script pode ser re-executado em dev (DROP em ordem reversa de
-- dependência para não violar FKs).
DROP TABLE IF EXISTS tpc_webhook_events CASCADE;
DROP TABLE IF EXISTS tpc_recargas       CASCADE;
DROP TABLE IF EXISTS tpc_transacoes     CASCADE;
DROP TABLE IF EXISTS tpc_carteira       CASCADE;

-- =============================================================================
-- tpc_carteira — Saldo da carteira de frete por usuário
-- No MySQL: wp_tpc_carteira
--
-- Padrão de acesso (espelha wallet.php):
--   SELECT ... FOR UPDATE dentro de transação Serializable antes de qualquer
--   operação de débito/crédito. Nunca ler saldo fora de transação para fins
--   de operação financeira.
-- =============================================================================
CREATE TABLE tpc_carteira (
    id               BIGINT        NOT NULL GENERATED ALWAYS AS IDENTITY,
    user_id          BIGINT        NOT NULL,
    saldo            NUMERIC(10,2) NOT NULL DEFAULT 0.00,
    saldo_reservado  NUMERIC(10,2) NOT NULL DEFAULT 0.00,
    -- saldo_reservado: valor de débitos pendentes (reservados mas não confirmados).
    -- saldo_disponivel = saldo - saldo_reservado (calculado na consulta, não armazenado).
    created_at       TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id),
    CONSTRAINT uq_carteira_user UNIQUE (user_id),
    CONSTRAINT ck_carteira_saldo_nao_negativo CHECK (saldo >= 0),
    CONSTRAINT ck_carteira_reservado_nao_negativo CHECK (saldo_reservado >= 0)
);

COMMENT ON TABLE tpc_carteira IS 'Carteira de frete por usuário WordPress — Fase 2 (espelha wp_tpc_carteira)';
COMMENT ON COLUMN tpc_carteira.saldo IS 'Saldo total creditado (inclui montante reservado)';
COMMENT ON COLUMN tpc_carteira.saldo_reservado IS 'Parcela do saldo bloqueada por reservas pendentes (tipo=debito, status=pendente)';

CREATE INDEX idx_carteira_user_id ON tpc_carteira (user_id);

-- =============================================================================
-- tpc_transacoes — Ledger de movimentações da carteira
-- No MySQL: wp_tpc_transacoes
--
-- Tipos:
--   credito — crédito na carteira (recarga PIX confirmada, estorno, bônus)
--   debito  — débito ou reserva de valor (status=pendente=reserva, confirmado=debitado)
--   reserva — alias legado do PHP para debito/pendente; mantido no CHECK para
--             compatibilidade com dados históricos migrados do MySQL.
--
-- Semântica de reserva (espelha wallet.php):
--   tpc_reservar()        → INSERT tipo='debito' status='pendente', saldo_reservado += valor
--   tpc_debitar_reserva() → UPDATE status='confirmado', saldo -= valor, saldo_reservado -= valor
--   tpc_liberar_reserva() → UPDATE status='cancelado', saldo_reservado -= valor
--   tpc_movimentar(credito) → INSERT tipo='credito' status='confirmado', saldo += valor
-- =============================================================================
CREATE TABLE tpc_transacoes (
    id           BIGINT        NOT NULL GENERATED ALWAYS AS IDENTITY,
    user_id      BIGINT        NOT NULL,
    tipo         VARCHAR(10)   NOT NULL
                     CHECK (tipo IN ('credito', 'debito', 'reserva')),
    valor        NUMERIC(10,2) NOT NULL,
    saldo_apos   NUMERIC(10,2) NOT NULL DEFAULT 0.00,
    -- saldo_apos: saldo da carteira imediatamente após esta transação (snapshot).
    --   Para tipo=debito/reserva status=pendente: saldo_apos = saldo antes da reserva
    --   (reserva não altera o saldo total — espelha PHP).
    descricao    VARCHAR(255)  NOT NULL DEFAULT '',
    referencia   VARCHAR(100)  NULL,
    -- referencia: chave de idempotência externa (ex: "pedido:12345", "recarga:99").
    --   Junto com (user_id, tipo) forma a UNIQUE constraint de idempotência (S8).
    order_id     BIGINT        NULL,
    -- order_id: WC order_id associado (para rastreabilidade de débitos de frete)
    me_order_id  VARCHAR(100)  NULL,
    -- me_order_id: ID do pedido no Melhor Envio (string UUID)
    status       VARCHAR(20)   NOT NULL DEFAULT 'confirmado'
                     CHECK (status IN ('pendente', 'confirmado', 'cancelado')),
    actor_id     BIGINT        NULL,
    -- actor_id: WP user_id do admin que executou (movimentações manuais; NULL = sistema)
    ip_address   VARCHAR(64)   NULL,
    -- ip_address: IP de origem do request (auditoria; NULL para operações internas)
    created_at   TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id),
    -- S8 — idempotência DB-level: mesma referência + tipo por usuário = 1 linha.
    -- ON CONFLICT (user_id, referencia, tipo) DO NOTHING nos INSERTs.
    CONSTRAINT uq_transacao_ref UNIQUE (user_id, referencia, tipo),
    CONSTRAINT ck_transacao_valor_positivo CHECK (valor > 0)
);

COMMENT ON TABLE tpc_transacoes IS 'Ledger de movimentações da carteira — Fase 2 (espelha wp_tpc_transacoes)';
COMMENT ON COLUMN tpc_transacoes.tipo IS 'credito=entrada, debito/reserva=saída. reserva é alias legado de debito/pendente';
COMMENT ON COLUMN tpc_transacoes.referencia IS 'Chave de idempotência externa. UNIQUE com (user_id, tipo) — S8';
COMMENT ON COLUMN tpc_transacoes.saldo_apos IS 'Snapshot do saldo após a transação (para auditoria e reconciliação)';

CREATE INDEX idx_transacoes_user_id    ON tpc_transacoes (user_id);
CREATE INDEX idx_transacoes_user_status ON tpc_transacoes (user_id, status);
CREATE INDEX idx_transacoes_created    ON tpc_transacoes (created_at);
CREATE INDEX idx_transacoes_order_id   ON tpc_transacoes (order_id) WHERE order_id IS NOT NULL;
CREATE INDEX idx_transacoes_referencia ON tpc_transacoes (referencia) WHERE referencia IS NOT NULL;

-- =============================================================================
-- tpc_recargas — Recargas PIX da carteira (em andamento e histórico)
-- No MySQL: wp_tpc_recargas
--
-- Ciclo de vida:
--   pendente   → PIX criado, aguardando pagamento
--   confirmado → pagamento recebido (via webhook ou reconciliação)
--   expirado   → expires_at passou sem pagamento (marcado pelo job reconcile.go)
--   cancelado  → cancelado manualmente pelo admin
-- =============================================================================
CREATE TABLE tpc_recargas (
    id          BIGINT        NOT NULL GENERATED ALWAYS AS IDENTITY,
    user_id     BIGINT        NOT NULL,
    valor       NUMERIC(10,2) NOT NULL,
    status      VARCHAR(20)   NOT NULL DEFAULT 'pendente'
                    CHECK (status IN ('pendente', 'confirmado', 'expirado', 'cancelado')),
    me_pix_id   VARCHAR(100)  NULL,
    -- me_pix_id: ID da cobrança no Melhor Envio (UUID). UNIQUE para idempotência de webhook.
    pix_qr      TEXT          NULL,
    -- pix_qr: imagem QR code em base64 ou URL
    pix_codigo  TEXT          NULL,
    -- pix_codigo: "copia e cola" do PIX (linha digitável EMV)
    expires_at  TIMESTAMPTZ   NULL,
    -- expires_at: vencimento do PIX; job reconcile.go marca como 'expirado' após este momento
    paid_at     TIMESTAMPTZ   NULL,
    -- paid_at: momento em que o pagamento foi confirmado (webhook ou reconciliação)
    tx_id       BIGINT        NULL,
    -- tx_id: ID da transação em tpc_transacoes criada ao confirmar (rastreabilidade)
    created_at  TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id),
    CONSTRAINT uq_recarga_me_pix_id UNIQUE (me_pix_id),
    CONSTRAINT ck_recarga_valor_positivo CHECK (valor > 0)
);

COMMENT ON TABLE tpc_recargas IS 'Recargas PIX da carteira — Fase 2 (espelha wp_tpc_recargas)';
COMMENT ON COLUMN tpc_recargas.me_pix_id IS 'ID da cobrança no Melhor Envio. UNIQUE — garante idempotência do webhook PIX';
COMMENT ON COLUMN tpc_recargas.pix_codigo IS 'Código PIX copia-e-cola (EMV). Exibido ao usuário no portal';
COMMENT ON COLUMN tpc_recargas.tx_id IS 'FK lógica para tpc_transacoes.id criada após confirmação';

CREATE INDEX idx_recargas_user_id    ON tpc_recargas (user_id);
CREATE INDEX idx_recargas_status     ON tpc_recargas (status);
CREATE INDEX idx_recargas_expires_at ON tpc_recargas (expires_at) WHERE status = 'pendente';
-- Índice parcial: o job de expiração só consulta recargas pendentes com expires_at no passado.

-- =============================================================================
-- tpc_webhook_events — Idempotência de eventos webhook
-- No MySQL: wp_tpc_webhook_events (UNIQUE KEY uq_event_key)
--
-- Garante que o mesmo webhook PIX (ou outro evento) nunca seja processado duas
-- vezes, mesmo em cenários de retry do provedor ou falha parcial.
-- O campo payload armazena o hash SHA-256 do body raw (não o body completo)
-- para auditoria sem expor dados sensíveis.
-- =============================================================================
CREATE TABLE tpc_webhook_events (
    id           BIGINT       NOT NULL GENERATED ALWAYS AS IDENTITY,
    event_key    VARCHAR(100) NOT NULL,
    -- event_key: chave de idempotência (ex: "pix:<me_pix_id>" ou "pix:recarga:<id>").
    --   Corresponde ao uq_event_key do MySQL.
    payload      TEXT         NULL,
    -- payload: hash SHA-256 do body bruto (não o body em si — sem dados sensíveis).
    --   No MySQL armazenava o payload completo; aqui armazenamos apenas o hash.
    source       VARCHAR(50)  NULL,
    -- source: identificador da origem ("pix", "me_webhook", "tracking")
    event_type   VARCHAR(50)  NULL,
    -- event_type: tipo do evento dentro da source ("payment", "ping", "status_update")
    payload_hash VARCHAR(64)  NULL,
    -- payload_hash: SHA-256 hexadecimal do raw body (64 chars)
    recarga_id   BIGINT       NULL,
    -- recarga_id: ID da recarga associada (quando origin=pix)
    me_id        VARCHAR(100) NULL,
    -- me_id: ID do recurso no Melhor Envio (me_pix_id ou order_id)
    status       VARCHAR(20)  NULL,
    -- status: status informado no evento (paid, approved, etc.) — apenas para registro
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id),
    -- uq_event_key: garante idempotência — espelha UNIQUE KEY uq_event_key do MySQL
    CONSTRAINT uq_event_key UNIQUE (event_key)
);

COMMENT ON TABLE tpc_webhook_events IS 'Idempotência de eventos webhook — Fase 2 (espelha wp_tpc_webhook_events com UNIQUE KEY uq_event_key)';
COMMENT ON COLUMN tpc_webhook_events.event_key IS 'Chave de idempotência: "pix:<me_pix_id>" ou "pix:recarga:<id>". UNIQUE.';
COMMENT ON COLUMN tpc_webhook_events.payload IS 'Hash SHA-256 do body bruto (sem dados sensíveis)';

CREATE INDEX idx_webhook_events_source ON tpc_webhook_events (source);
CREATE INDEX idx_webhook_events_created ON tpc_webhook_events (created_at);
