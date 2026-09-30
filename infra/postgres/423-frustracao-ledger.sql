-- =============================================================================
-- 423-frustracao-ledger.sql — BOOKING da PENALTY DO PRODUTOR no livro-razão FALK.
--
-- // FRUST-LEDGER
--
-- MODELO (fonte da verdade — dono):
--   FRUSTRADO zera o financeiro NORMAL (produtor E afiliado). A ÚNICA cobrança é a
--   PENALTY de frustração, CONGELADA (snapshot) na baixa. Há DUAS penalties que
--   coexistem e são receita FALK:
--     - AFILIADO  → já bookada em senderzz_revenue como component 'taxa_frustrado'
--       (ref 'penalty:<afftx_id>') via trg_sz_revenue_capture_afftx
--       (senderzz_affiliate_transactions type='penalty'; ver 340-revenue-fees.sql).
--       ESTE arquivo NÃO TOCA o afiliado — disjunto por component E por prefixo de
--       ref (evita double-count por construção).
--     - PRODUTOR  → NÃO era bookado em lugar nenhum. ESTE arquivo o registra:
--       component NOVO 'taxa_frustrado_produtor', ref 'frust_prod:<wc_order_id>'.
--       O valor é o CONGELADO em sz_motoboy_pedidos.valor_taxa_frustrado
--       (REPURPOSE — ver 421-frustracao-freeze.sql), que a view sz_order_financeiro
--       já surfaceia como taxa_frustracao_produtor (422-frustracao-view.sql).
--
-- IDEMPOTÊNCIA:
--   - Backfill: INSERT … ON CONFLICT (component, ref) DO NOTHING. ref ancora no
--     wc_order_id (estável) → rodar 2× não duplica.
--   - View: CREATE OR REPLACE (mesma lista de colunas — só ADICIONA um component).
--
-- SELEÇÃO (espelha a view 422 — uma linha por pedido):
--   A view pega a frustrada MAIS RECENTE por pedido
--   (ORDER BY ts_frustrado DESC NULLS LAST, id DESC LIMIT 1). O backfill e o
--   esperado da reconciliação usam o MESMO critério via DISTINCT ON (wc_order_id),
--   para que ledger == esperado == o que a view lê (divergência 0).
--
-- DECISÃO (documentada): bookamos TODOS os pedidos frustrados, INCLUSIVE os de
--   penalty congelada = 0 (1ª frustração por telefone = isento). Isto é um desvio
--   deliberado da convenção `amount > 0` de 300/310/340: aqui queremos UMA linha de
--   auditoria por pedido frustrado (reconcilia 0=0, e o "Booke os 6 reais" do dono
--   é literal — os 6 hoje têm penalty produtor 0). amount=0 é permitido (não há
--   CHECK amount>0 em senderzz_revenue).
--
-- ESCOPO: só ADICIONA dados (6 linhas) + REDEFINE a view de reconciliação.
--   NÃO toca o booking do afiliado (340 / trg_sz_revenue_capture_afftx).
--   NÃO toca a view sz_order_financeiro (422). NÃO toca Go/React.
-- =============================================================================


-- =============================================================================
-- (1) BACKFILL — penalty CONGELADA do PRODUTOR → senderzz_revenue
-- =============================================================================
-- FROM sz_motoboy_pedidos (frustrado) é a fonte do CONGELADO. LEFT JOIN a sz_orders
-- (não INNER) para não derrubar um pedido motoboy sem match em sz_orders — ainda
-- assim contado no esperado; produtor_id ficaria NULL nesse caso (robustez; os 6
-- reais mapeiam todos p/ produtor 15). DISTINCT ON garante 1 linha por wc_order_id,
-- a frustrada mais recente — idêntico ao que a view lê.
INSERT INTO senderzz_revenue
    (order_id, affiliate_id, produtor_id, component, base_amount, amount, ref, created_at)
SELECT DISTINCT ON (m.wc_order_id)
       o.id                              AS order_id,
       NULL::bigint                      AS affiliate_id,   -- penalty do PRODUTOR (não afiliado)
       o.produtor_id                     AS produtor_id,
       'taxa_frustrado_produtor'         AS component,
       m.valor_taxa_frustrado            AS base_amount,
       m.valor_taxa_frustrado            AS amount,         -- penalty congelada = receita FALK
       'frust_prod:' || m.wc_order_id    AS ref,
       COALESCE(m.ts_frustrado, NOW())   AS created_at
FROM sz_motoboy_pedidos m
LEFT JOIN sz_orders o ON COALESCE(o.wp_order_id, o.id) = m.wc_order_id
WHERE m.status = 'frustrado'
ORDER BY m.wc_order_id, m.ts_frustrado DESC NULLS LAST, m.id DESC
ON CONFLICT (component, ref) DO NOTHING;


-- =============================================================================
-- (2) VIEW sz_revenue_reconciliacao — INCLUI taxa_frustrado_produtor
-- =============================================================================
-- Redefinição da view de 390-blindagem (linha ~184). As 3 branches originais
-- (taxa_afiliado_4_99 / taxa_transacao_produtor / taxa_entrega) ficam BYTE-A-BYTE
-- idênticas. Mudanças (apenas 2):
--   (a) NOVA branch de `esperado` para taxa_frustrado_produtor = SUM do CONGELADO
--       (DISTINCT ON wc_order_id, latest-wins) — mesma fonte/critério do backfill.
--   (b) 'taxa_frustrado_produtor' adicionado ao filtro do CTE `ledger`. SEM isto,
--       ledger_gravado leria 0 e fabricaria divergência.
-- Lista de colunas inalterada (componente, ledger_gravado, esperado, divergencia,
-- alerta) → CREATE OR REPLACE seguro.
--
-- ESCOPO da reconciliação continua: componentes derivados de pedido. O afiliado de
-- frustração ('taxa_frustrado', penalty:<afftx_id>) permanece FORA da reconciliação
-- (não deriva determinísticamente de sz_orders/sz_motoboy_pedidos aqui) — coerente
-- com o def anterior, que também não o reconciliava.
CREATE OR REPLACE VIEW sz_revenue_reconciliacao AS
WITH cfg AS (
    SELECT
        COALESCE((SELECT NULLIF(value,'')::numeric FROM senderzz_options
                   WHERE name = 'sz_producer_transaction_fee_pct'), 4.99) AS prod_pct,
        COALESCE((SELECT NULLIF(value,'')::numeric FROM senderzz_options
                   WHERE name = 'motoboy_repasse_padrao'), 18)            AS repasse
),
esperado AS (
    -- taxa_afiliado_4_99: realiza em completo/entregue
    SELECT 'taxa_afiliado_4_99'::varchar(40) AS component,
           COALESCE(SUM(o.transaction_fee), 0)::numeric(14,2) AS esperado
    FROM sz_orders o
    WHERE o.status IN ('completo','entregue') AND COALESCE(o.transaction_fee,0) > 0
    UNION ALL
    -- taxa_transacao_produtor: realiza em completo/entregue (round por pedido, depois soma)
    SELECT 'taxa_transacao_produtor'::varchar(40),
           COALESCE(SUM(ROUND(COALESCE(o.total,0) * (SELECT prod_pct FROM cfg) / 100, 2)), 0)::numeric(14,2)
    FROM sz_orders o
    WHERE o.status IN ('completo','entregue') AND COALESCE(o.total,0) > 0
    UNION ALL
    -- taxa_entrega: realiza em completo/entregue/frustrado — MARKUP (delivery − repasse, piso 0)
    SELECT 'taxa_entrega'::varchar(40),
           COALESCE(SUM(GREATEST(COALESCE(o.delivery_fee,0) - (SELECT repasse FROM cfg), 0)), 0)::numeric(14,2)
    FROM sz_orders o
    WHERE o.status IN ('completo','entregue','frustrado') AND COALESCE(o.delivery_fee,0) > 0
    UNION ALL
    -- // FRUST-LEDGER: taxa_frustrado_produtor = SUM do CONGELADO (penalty produtor),
    -- 1 linha por pedido frustrado (latest-wins, idêntico ao backfill/view). LÊ o
    -- congelado de sz_motoboy_pedidos — NÃO recalcula 1ª-vs-repeat. Por isso reconcilia
    -- exatamente com o ledger (mesma fonte, mesmo critério de seleção).
    SELECT 'taxa_frustrado_produtor'::varchar(40),
           COALESCE(SUM(f.penalty), 0)::numeric(14,2)
    FROM (
        SELECT DISTINCT ON (m.wc_order_id) m.valor_taxa_frustrado AS penalty
        FROM sz_motoboy_pedidos m
        WHERE m.status = 'frustrado'
        ORDER BY m.wc_order_id, m.ts_frustrado DESC NULLS LAST, m.id DESC
    ) f
),
ledger AS (
    SELECT component, COALESCE(SUM(amount),0)::numeric(14,2) AS gravado
    FROM senderzz_revenue
    WHERE component IN ('taxa_afiliado_4_99','taxa_transacao_produtor','taxa_entrega','taxa_frustrado_produtor')
    GROUP BY component
)
SELECT
    e.component                                            AS componente,
    COALESCE(l.gravado, 0)                                 AS ledger_gravado,
    e.esperado                                             AS esperado,
    (COALESCE(l.gravado, 0) - e.esperado)::numeric(14,2)   AS divergencia,
    (ABS(COALESCE(l.gravado, 0) - e.esperado) > 1.00)      AS alerta
FROM esperado e
LEFT JOIN ledger l ON l.component = e.component
ORDER BY e.component;

COMMENT ON VIEW sz_revenue_reconciliacao IS
    'Reconciliação por componente: soma gravada em senderzz_revenue (realizado) vs '
    'soma esperada derivada de pedido pela regra oficial. divergencia = ledger − esperado. '
    'alerta = TRUE quando |divergencia| > R$1. Componentes: afiliado/produtor/entrega + '
    'taxa_frustrado_produtor (penalty congelada do produtor, FRUST-LEDGER 423). '
    'A penalty de frustração do AFILIADO (taxa_frustrado) fica FORA desta reconciliação.';


-- =============================================================================
-- (3) PROVA (read-only) — lançamentos do produtor sem duplicar o afiliado
-- =============================================================================
\echo '--- [423] lançamentos taxa_frustrado_produtor (produtor_id setado, affiliate_id NULL) ---'
SELECT order_id, produtor_id, affiliate_id, component, base_amount, amount, ref, created_at
FROM senderzz_revenue
WHERE component = 'taxa_frustrado_produtor'
ORDER BY order_id;

\echo '--- [423] afiliado taxa_frustrado INTACTO (NÃO duplicado) — esperado 6 linhas / 44.00 ---'
SELECT count(*) AS linhas_aff, COALESCE(SUM(amount),0) AS soma_aff
FROM senderzz_revenue
WHERE component = 'taxa_frustrado';

\echo '--- [423] reconciliação (taxa_frustrado_produtor deve dar divergencia 0 / alerta f) ---'
SELECT * FROM sz_revenue_reconciliacao;
