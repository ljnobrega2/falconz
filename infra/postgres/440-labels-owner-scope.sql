-- =============================================================================
-- AUDIT-2026-06-21 #HIGH-1 — Escopo por dono em wc_me_labels (IDOR cross-tenant)
--
-- PROBLEMA: wc_me_labels não tinha coluna de proprietário. O labels-service lia a
-- tabela apenas por wc_order_id (enumerável), permitindo que qualquer usuário
-- autenticado (produtor/OL/afiliado) enumerasse e lesse CEP do destinatário,
-- tracking e dados comerciais de TODOS os concorrentes (IDOR cross-tenant / PII LGPD).
--
-- FIX (parte DB): adiciona owner_user_id (nullable) e faz backfill do dono real a
-- partir de sz_orders (produtor_id) via wp_order_id = wc_order_id. O filtro de
-- escopo (AND owner_user_id = $N para não-admin; admin vê tudo; fora de escopo → 404)
-- é aplicado no labels-service (GetLabel/GetLabels/DeleteLabel/PostLabel idempotência).
--
-- NULLABLE de propósito: linhas legadas sem origem em sz_orders (wp_order_id NULL no
-- pedido nativo, ou pedido ainda não migrado) ficam com owner_user_id = NULL. Isso é
-- FAIL-CLOSED: NULL nunca casa o filtro "= $N" de um não-admin → invisível para ele;
-- apenas o admin (sem filtro) as enxerga. Nenhum vazamento cross-tenant.
--
-- Idempotente: ADD COLUMN IF NOT EXISTS + CREATE INDEX IF NOT EXISTS. O backfill
-- via UPDATE é seguro re-executar (só preenche linhas ainda NULL).
-- =============================================================================

ALTER TABLE wc_me_labels
    ADD COLUMN IF NOT EXISTS owner_user_id BIGINT NULL;

COMMENT ON COLUMN wc_me_labels.owner_user_id IS
    'AUDIT-2026-06-21 #HIGH-1: WP user_id do dono (produtor) da etiqueta. Usado para escopo multi-tenant no labels-service. NULL = legado sem origem em sz_orders → visível só para admin (fail-closed).';

-- Backfill: dono = produtor_id do pedido correspondente em sz_orders.
-- JOIN por wp_order_id (ID original do WooCommerce em sz_orders) = wc_order_id.
-- Só preenche linhas ainda NULL (re-execução segura). Pedidos nativos Go
-- (wp_order_id NULL) não casam — permanecem NULL (fail-closed, vide acima).
UPDATE wc_me_labels l
   SET owner_user_id = o.produtor_id
  FROM sz_orders o
 WHERE o.wp_order_id = l.wc_order_id
   AND o.produtor_id IS NOT NULL
   AND l.owner_user_id IS NULL;

-- Índice para o filtro de escopo por dono (WHERE owner_user_id = $N).
CREATE INDEX IF NOT EXISTS idx_labels_owner ON wc_me_labels (owner_user_id);
