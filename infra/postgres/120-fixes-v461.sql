-- =============================================================================
-- Senderzz — Patch v461 (motoboy ganhos/pagamentos colunas + checks)
--
-- Erros observados em produção (Saques Motoboy / Carteira Motoboy):
--   - column "valor_total" does not exist (SQLSTATE 42703)
--   - column "valor_pago" does not exist
--   - sz_motoboy_pagamentos.status precisa aceitar 'aguardando'/'pago'
-- =============================================================================

-- sz_motoboy_ganhos — colunas faltantes (FIFO de quitação)
ALTER TABLE sz_motoboy_ganhos ADD COLUMN IF NOT EXISTS valor_pago      DECIMAL(10,2)   NOT NULL DEFAULT 0.00;
ALTER TABLE sz_motoboy_ganhos ADD COLUMN IF NOT EXISTS pagamento_id    BIGINT          NULL;

-- sz_motoboy_pagamentos — colunas faltantes
ALTER TABLE sz_motoboy_pagamentos ADD COLUMN IF NOT EXISTS valor_total    DECIMAL(10,2)   NOT NULL DEFAULT 0.00;
ALTER TABLE sz_motoboy_pagamentos ADD COLUMN IF NOT EXISTS data_pagamento DATE            NULL;
ALTER TABLE sz_motoboy_pagamentos ADD COLUMN IF NOT EXISTS obs            TEXT            NULL;

-- Expandir CHECK do status (handlers usam 'aguardando'/'pago').
ALTER TABLE sz_motoboy_pagamentos DROP CONSTRAINT IF EXISTS sz_motoboy_pagamentos_status_check;
ALTER TABLE sz_motoboy_pagamentos
    ADD CONSTRAINT sz_motoboy_pagamentos_status_check
    CHECK (status IN ('pendente','efetuado','estornado','aguardando','pago'));

-- Índice útil pro Saques
CREATE INDEX IF NOT EXISTS idx_mb_pgto_data_status ON sz_motoboy_pagamentos (data_pagamento, status);

-- =============================================================================
-- FIM v461
-- =============================================================================
