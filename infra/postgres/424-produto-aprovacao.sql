-- =============================================================================
-- 424-produto-aprovacao.sql
-- Senderzz — Infraestrutura de APROVAÇÃO de produto (espelha a de afiliado).
--
-- REGRA DE NEGÓCIO (do dono): produto novo cadastrado pelo PRODUTOR nasce
--   "a aprovar" (pendente). O ADMIN aprova → 'aprovado'. Pode reprovar →
--   'reprovado'. Espelha o fluxo de aprovação de afiliado (pending → active),
--   mas a APROVAÇÃO de produto é do ADMIN (não do produtor).
--
-- O QUE MUDA: o CHECK de sz_products.status só admitia
--   ('active','inactive','draft','archived'). Aqui ele é RECONSTRUÍDO para
--   incluir os 3 novos tokens de aprovação ('a_aprovar','aprovado','reprovado')
--   E o 'deleted' (tombstone de soft-delete que o handler Delete já gravava —
--   o CHECK antigo NÃO o admitia, então o soft-delete violava a constraint:
--   bug latente corrigido aqui de quebra).
--
-- CONVENÇÃO DE TOKEN: a coluna usa tokens minúsculos sem espaço (draft,
--   archived). Por isso o pendente é 'a_aprovar' (underscore), NÃO 'a aprovar'
--   (com espaço — essa é a convenção da tabela de PEDIDOS sz_orders, outra
--   coluna/outro domínio). Mantém o id-space da coluna consistente.
--
-- IDEMPOTENTE: DROP CONSTRAINT IF EXISTS + ADD recriam a constraint nomeada com
--   o conjunto completo. Re-rodar é no-op (mesmo conjunto). Não toca dados.
-- =============================================================================

-- 1) CHECK reconstruído (drop+add da constraint nomeada — idempotente).
ALTER TABLE sz_products DROP CONSTRAINT IF EXISTS sz_products_status_check;

ALTER TABLE sz_products ADD CONSTRAINT sz_products_status_check
    CHECK (status IN (
        'active', 'inactive', 'draft', 'archived', 'deleted',
        'a_aprovar', 'aprovado', 'reprovado'
    ));

-- 2) DEFAULT do status passa a ser 'a_aprovar' (produto novo nasce pendente).
--    O INSERT do produtor (go/portal Create) já manda 'a_aprovar' explícito;
--    o DEFAULT cobre qualquer INSERT que omita a coluna (defesa em profundidade).
--    NÃO altera linhas existentes — só o default de futuros INSERT sem status.
ALTER TABLE sz_products ALTER COLUMN status SET DEFAULT 'a_aprovar';

-- 3) Índice parcial p/ a fila de aprovação do admin (GET produtos 'a_aprovar').
--    Acelera a listagem da fila sem custo nas demais queries.
CREATE INDEX IF NOT EXISTS idx_products_aprovacao
    ON sz_products (status)
    WHERE status = 'a_aprovar';
