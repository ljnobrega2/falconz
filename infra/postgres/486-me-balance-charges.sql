-- 486: wc_me_balance_charges — rastreia recargas de saldo ME por produtor
-- Cada linha = 1 PIX gerado via POST /me/balance (conta Melhor Envio da plataforma).
-- O produtor paga o PIX e o saldo ME aumenta, habilitando emissão de etiquetas.
-- Status: pending → paid | expired | cancelled

CREATE TABLE IF NOT EXISTS wc_me_balance_charges (
    id            BIGINT       NOT NULL GENERATED ALWAYS AS IDENTITY,
    producer_id   INTEGER      NOT NULL,           -- senderzz_portal_users.id
    me_charge_id  TEXT,                            -- ID retornado pela ME API (pode ser NULL se ME não retornar)
    amount        NUMERIC(10,2) NOT NULL,
    status        TEXT         NOT NULL DEFAULT 'pending',  -- pending | paid | expired | cancelled
    qr_code       TEXT,                            -- código PIX copia-e-cola
    qr_code_image TEXT,                            -- base64 data:image/png;base64,...
    link          TEXT,                            -- link de pagamento ME
    expiry        TIMESTAMPTZ,                     -- validade do PIX
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    PRIMARY KEY (id),
    CONSTRAINT chk_me_balance_status CHECK (status IN ('pending','paid','expired','cancelled')),
    CONSTRAINT chk_me_balance_amount CHECK (amount > 0)
);

CREATE INDEX IF NOT EXISTS idx_me_balance_producer  ON wc_me_balance_charges (producer_id);
CREATE INDEX IF NOT EXISTS idx_me_balance_status    ON wc_me_balance_charges (status) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_me_balance_charge_id ON wc_me_balance_charges (me_charge_id) WHERE me_charge_id IS NOT NULL;

COMMENT ON TABLE wc_me_balance_charges IS
  'Recargas de saldo ME via PIX por produtor. A conta ME da plataforma é compartilhada; '
  'este log rastreia quem pagou quanto para fins de auditoria por producer_id.';
