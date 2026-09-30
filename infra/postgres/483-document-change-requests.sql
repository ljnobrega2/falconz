-- =============================================================================
-- 483 — Solicitação de troca de documento (CPF ⇄ CNPJ) com aprovação do admin.
--
-- FEAT-DOC-CHANGE-2026-07-03. O usuário (produtor | afiliado | cliente) pede, pelo
-- portal, a troca do tipo de documento da própria conta:
--   • cpf_to_cnpj — informa RAZÃO SOCIAL + CNPJ + anexo do cartão CNPJ (PII, privado).
--   • cnpj_to_cpf — informa NOME CIVIL + CPF (sem anexo — decisão do dono 2026-07-03).
--
-- A solicitação nasce 'pending'. O admin aprova → o serviço admin faz UPDATE em
-- senderzz_portal_users.nome (= razão social / nome civil) + document (= novo doc),
-- e o novo nome passa a valer em TODAS as menções ao usuário dali pra frente
-- (o display do site lê senderzz_portal_users.nome — coluna `name` é alias gerado).
-- Snapshots históricos (ex.: holder_cpf de saques já pagos) NÃO são reescritos:
-- são registros do que foi pago, não menções vivas ao usuário.
--
-- Anexo do cartão CNPJ é PII → NÃO vai para o file-server público /uploads/products.
-- É gravado em /app/uploads/doc-changes (volume falk_uploads, sem rota estática) e
-- servido só pelo endpoint admin autenticado (GET /document-changes/{id}/attachment).
-- =============================================================================

CREATE TABLE IF NOT EXISTS senderzz_document_change_request (
    id                BIGINT       NOT NULL GENERATED ALWAYS AS IDENTITY,
    user_id           BIGINT       NOT NULL,
    -- user_id: senderzz_portal_users.id do solicitante (titular da conta).
    role              VARCHAR(30)  NOT NULL DEFAULT '',
    -- role: snapshot do papel no momento do pedido (produtor|afiliado|cliente|operator).
    direction         VARCHAR(20)  NOT NULL
                          CHECK (direction IN ('cpf_to_cnpj', 'cnpj_to_cpf')),
    target_nome       VARCHAR(255) NOT NULL,
    -- target_nome: razão social (cpf_to_cnpj) OU nome civil (cnpj_to_cpf).
    target_document   VARCHAR(20)  NOT NULL,
    -- target_document: só dígitos. 14 (CNPJ) em cpf_to_cnpj; 11 (CPF) em cnpj_to_cpf.
    attachment_path   VARCHAR(255),
    -- attachment_path: nome do arquivo do cartão CNPJ em /app/uploads/doc-changes.
    -- Preenchido só em cpf_to_cnpj. NUNCA uma URL pública — servido via endpoint admin.
    status            VARCHAR(20)  NOT NULL DEFAULT 'pending'
                          CHECK (status IN ('pending', 'approved', 'rejected')),
    old_nome          VARCHAR(255) NOT NULL DEFAULT '',
    old_document      VARCHAR(20)  NOT NULL DEFAULT '',
    -- old_*: snapshot ANTES da troca (trilha + base p/ eventual reversão).
    notes             TEXT,
    -- notes: motivo da rejeição (obrigatório no reject) ou observação do admin.
    reviewer_admin_id BIGINT,
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    reviewed_at       TIMESTAMPTZ,
    PRIMARY KEY (id)
);

COMMENT ON TABLE senderzz_document_change_request IS
    'Solicitações de troca CPF⇄CNPJ do titular da conta, com aprovação do admin (FEAT-DOC-CHANGE-2026-07-03).';

CREATE INDEX IF NOT EXISTS idx_doc_change_user   ON senderzz_document_change_request (user_id);
CREATE INDEX IF NOT EXISTS idx_doc_change_status ON senderzz_document_change_request (status);

-- Um pedido PENDENTE por usuário (evita fila dupla). Índice parcial: aprovados/
-- rejeitados históricos não bloqueiam novos pedidos.
CREATE UNIQUE INDEX IF NOT EXISTS uq_doc_change_one_pending
    ON senderzz_document_change_request (user_id)
    WHERE status = 'pending';
