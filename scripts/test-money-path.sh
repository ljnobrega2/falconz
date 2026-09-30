#!/usr/bin/env bash
# =============================================================================
# test-money-path.sh — VALIDAÇÃO FIM-A-FIM DO CAMINHO DO DINHEIRO (RECEITA FALKZ)
# =============================================================================
# O que faz: dentro de UMA transação Postgres (BEGIN … ROLLBACK), insere um
# pedido sintético + uma comissão de afiliado real e verifica que os TRIGGERS de
# captura de receita (310-revenue-triggers.sql) lançaram corretamente em
# senderzz_revenue. NÃO polui dados — tudo é desfeito no ROLLBACK final.
#
# Pré-requisitos:
#   - Postgres de dev no ar (localhost:5432, infra/docker/.env).
#   - senderzz_revenue criada (300-revenue.sql).
#   - Triggers aplicados (310-revenue-triggers.sql) — trg_sz_revenue_capture_*.
#   - Pelo menos 1 linha real em senderzz_affiliates.
#
# Asserts (PASS/FAIL impresso por linha) — testam CONTRATOS, não constantes:
#   1. taxa_afiliado_4_99      == sz_orders.transaction_fee        [REAL — trigger order-path]
#   2. taxa_entrega            == sz_orders.delivery_fee           [REAL — trigger order-path]
#   3. taxa_transacao_produtor == ROUND(total*pct/100,2), pct de
#      senderzz_options.sz_producer_transaction_fee_pct           [REAL — trigger order-path]
#   4. producer_net = total - affiliate_amount - delivery_fee - transaction_fee
#                                                                  [SINTÉTICO — consistência]
#
# AUDIT TEST-MONEY-PATH-CI-GAP (reescrito): a versão antiga assumia semântica pré-v4
# (status='pending', ref='order:'/'afftx:', taxa_transacao_produtor=transaction_fee)
# → FALHAVA contra os triggers v4/v5 atuais. Agora: pedido nasce 'entregue' (gate de
# receita realizada), refs corretos (order_aff/order_entrega/order_prodtx), e cada
# assert confere o CONTRATO documentado (v4/v5 headers), não números copiados do trigger.
#
# Por que SÓ o order-path (sem inserir comissão): no modelo ATUAL (v4/350-revenue-fix499)
# a taxa_afiliado_4_99 é lançada UNICAMENTE pelo trigger do PEDIDO (ref=order_aff:<id>).
# O trigger de affiliate_transactions só lança taxa_frustrado (type='penalty', ref=penalty:<id>)
# — NÃO duplica o 4,99% (v4 consolidou tudo no order-trigger justamente p/ matar o
# double-count do v3). Verificado empiricamente: 1 pedido+1 comissão → 1 linha de
# taxa_afiliado_4_99. Logo o order-path é o caminho canônico e inequívoco a testar.
#
# Valores auto-consistentes: total=500, delivery=50, affiliate_amount(líquida)=285.03,
# transaction_fee(take 4,99% da bruta 300)=14.97 → producer_net=150.00. Todos reconciliam.
# =============================================================================

set -euo pipefail

# --- Conexão ---------------------------------------------------------------
# AUDIT TEST-MONEY-PATH-CI-GAP: honra $DATABASE_URL quando definido (igual
# db-migrate.sh / backup-postgres.sh) — assim o CI aponta p/ o DB de teste sem
# depender do .env. Sem ele, monta a URL a partir de infra/docker/.env (dev local).
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="${SCRIPT_DIR}/../infra/docker/.env"

if [[ -n "${DATABASE_URL:-}" ]]; then
    PG_URL="$DATABASE_URL"
else
    if [[ ! -f "$ENV_FILE" ]]; then
        echo "ERRO: \$DATABASE_URL não definido e infra/docker/.env não existe em ${ENV_FILE}" >&2
        exit 1
    fi
    # Lê POSTGRES_PASSWORD de infra/docker/.env (sem dar source no arquivo inteiro).
    PG_PASS="$(grep -E '^POSTGRES_PASSWORD=' "$ENV_FILE" | head -n1 | cut -d'=' -f2-)"
    if [[ -z "${PG_PASS:-}" ]]; then
        echo "ERRO: POSTGRES_PASSWORD vazio/ausente em ${ENV_FILE}" >&2
        exit 1
    fi
    PG_URL="postgresql://senderzz:${PG_PASS}@localhost:5432/senderzz?sslmode=disable"
fi

# Sufixo único p/ order_number (evita colisão no UNIQUE mesmo entre runs).
# order_number é VARCHAR(20); prefixo 'SZ-MP-' (6) deixa 14 chars → usamos os
# últimos 14 de epoch+pid (suficiente p/ unicidade entre runs).
UNIQ="$(printf '%s' "$(date +%s)$$" | tail -c 14)"

echo "== test-money-path.sh =="
# Mascara a senha da URL no log (não vaza credencial).
echo "DB: $(printf '%s' "$PG_URL" | sed -E 's#(://[^:/@]+):[^@]*@#\1:***@#')  |  run-id: ${UNIQ}"
echo

# --- Tudo numa ÚNICA sessão psql (BEGIN…ROLLBACK atômico) -------------------
# ON_ERROR_STOP=1: qualquer erro aborta a sessão → ROLLBACK automático no disconnect.
psql "$PG_URL" \
    --no-psqlrc \
    --set=ON_ERROR_STOP=1 \
    --set=uniq="${UNIQ}" \
    -v VERBOSITY=terse <<'SQL'

\timing off
\pset pager off

BEGIN;

-- ---------------------------------------------------------------------------
-- 0) Escolhe um afiliado REAL (a linha de menor id existente) e seu produtor.
--    Falha cedo (e claro) se a tabela estiver vazia.
-- ---------------------------------------------------------------------------
SELECT id           AS aff_id,
       afiliado_id  AS aff_wp_user,
       produtor_id  AS aff_produtor
FROM senderzz_affiliates
ORDER BY id
LIMIT 1
\gset

\if :{?aff_id}
\else
  \echo 'ERRO: senderzz_affiliates está vazia — não há afiliado real para ligar a comissão.'
  \quit 3
\endif

\echo '-- Afiliado real usado:'
\echo '   senderzz_affiliates.id =' :aff_id '| afiliado_id(wp_user) =' :aff_wp_user '| produtor_id =' :aff_produtor

-- ---------------------------------------------------------------------------
-- 1) Insere o PEDIDO sintético em status TERMINAL ('entregue'). id é IDENTITY →
--    capturamos via RETURNING. produtor_id = produtor do afiliado (coerente).
--
--    AUDIT TEST-MONEY-PATH-CI-GAP: o gate de receita REALIZADA (330-revenue-realized
--    + 360-revenue-taxa-produtor) SÓ lança quando o status é terminal (completo/
--    entregue/frustrado). A versão antiga usava status='pending' → trigger NÃO
--    disparava → "linha ausente" → FAIL. Por isso nasce 'entregue'.
--
--    Valores AUTO-CONSISTENTES (reconciliam com os contratos do trigger v4/v5):
--      total=500.00, delivery_fee=50.00,
--      affiliate_amount(=comissão LÍQUIDA do afiliado) = 285.03,
--      transaction_fee (=take 4,99% do afiliado) = ROUND(285.03*0.0499,2)? NÃO:
--        contrato v4: taxa_afiliado_4_99 = transaction_fee (já é o take). Usamos
--        transaction_fee=14.97 (=ROUND(300*0.0499,2), o 4,99% da bruta 300).
--      producer_net = total - affiliate_amount - delivery_fee - transaction_fee
--                   = 500 - 285.03 - 50 - 14.97 = 150.00.
-- ---------------------------------------------------------------------------
INSERT INTO sz_orders
    (order_number, user_id, produtor_id, affiliate_id, status, payment_status,
     subtotal, shipping, total,
     affiliate_amount, senderzz_fee, producer_net, delivery_fee, transaction_fee)
VALUES
    ('SZ-MP-' || :'uniq', 999999, :aff_produtor, :aff_id, 'entregue', 'paid',
     480.00, 20.00, 500.00,
     285.03, 0.00, 150.00, 50.00, 14.97)
RETURNING id AS oid
\gset

\echo '-- Pedido sintético (entregue) inserido: sz_orders.id =' :oid
\echo '-- Os 3 componentes de receita são lançados pelo TRIGGER do pedido (order-path).'
\echo

-- ===========================================================================
-- ASSERT 1 [REAL — contrato] — taxa_afiliado_4_99 == sz_orders.transaction_fee
--   (v4: "UM componente taxa_afiliado_4_99 = transaction_fee"). ref=order_aff:<id>.
--   NOTA (não-infra): existe TAMBÉM um trigger sobre senderzz_affiliate_transactions
--   (350-revenue-fees) que lança taxa_afiliado_4_99 com ref=afftx:<txid>. Aqui NÃO
--   inserimos comissão → testamos só o order-path (ref=order_aff) sem ambiguidade.
-- ===========================================================================
SELECT
    CASE WHEN r.amount IS NOT NULL AND r.amount = o.transaction_fee THEN 'PASS' ELSE 'FAIL' END AS resultado,
    'ASSERT 1 [REAL] taxa_afiliado_4_99 = transaction_fee' AS assert,
    o.transaction_fee AS esperado,
    COALESCE(r.amount::text, '(linha ausente)') AS obtido,
    'ref=order_aff:' || :oid AS ref
FROM sz_orders o
LEFT JOIN senderzz_revenue r
       ON r.component = 'taxa_afiliado_4_99'
      AND r.ref = 'order_aff:' || :oid
WHERE o.id = :oid
\gset a1_
\echo :a1_resultado '|' :a1_assert '| esperado=' :a1_esperado '| obtido=' :a1_obtido '|' :a1_ref

-- ===========================================================================
-- ASSERT 2 [REAL — contrato] — taxa_entrega == sz_orders.delivery_fee. ref=order_entrega:<id>.
-- ===========================================================================
SELECT
    CASE WHEN r.amount IS NOT NULL AND r.amount = o.delivery_fee THEN 'PASS' ELSE 'FAIL' END AS resultado,
    'ASSERT 2 [REAL] taxa_entrega = delivery_fee' AS assert,
    o.delivery_fee AS esperado,
    COALESCE(r.amount::text, '(linha ausente)') AS obtido,
    'ref=order_entrega:' || :oid AS ref
FROM sz_orders o
LEFT JOIN senderzz_revenue r
       ON r.component = 'taxa_entrega'
      AND r.ref = 'order_entrega:' || :oid
WHERE o.id = :oid
\gset a2_
\echo :a2_resultado '|' :a2_assert '| esperado=' :a2_esperado '| obtido=' :a2_obtido '|' :a2_ref

-- ===========================================================================
-- ASSERT 3 [REAL — contrato] — taxa_transacao_produtor == ROUND(total*pct/100,2),
--   pct lido de senderzz_options.sz_producer_transaction_fee_pct (NÃO hardcode).
--   ref=order_prodtx:<id>. (v5: taxa do produtor sobre o VALOR BRUTO, configurável.)
-- ===========================================================================
SELECT
    CASE WHEN r.amount IS NOT NULL
          AND r.amount = ROUND((o.total * COALESCE(
                (SELECT NULLIF(value,'')::numeric FROM senderzz_options WHERE name='sz_producer_transaction_fee_pct'),
                4.99) / 100)::numeric, 2)
         THEN 'PASS' ELSE 'FAIL' END AS resultado,
    'ASSERT 3 [REAL] taxa_transacao_produtor = total*pct/100' AS assert,
    ROUND((o.total * COALESCE(
        (SELECT NULLIF(value,'')::numeric FROM senderzz_options WHERE name='sz_producer_transaction_fee_pct'),
        4.99) / 100)::numeric, 2) AS esperado,
    COALESCE(r.amount::text, '(linha ausente)') AS obtido,
    'ref=order_prodtx:' || :oid AS ref
FROM sz_orders o
LEFT JOIN senderzz_revenue r
       ON r.component = 'taxa_transacao_produtor'
      AND r.ref = 'order_prodtx:' || :oid
WHERE o.id = :oid
\gset a3_
\echo :a3_resultado '|' :a3_assert '| esperado=' :a3_esperado '| obtido=' :a3_obtido '|' :a3_ref

-- ===========================================================================
-- ASSERT 4 [SINTÉTICO — consistência] — líquido produtor (producer_net)
--   = total - affiliate_amount - delivery_fee - transaction_fee. Nada no banco
--   CALCULA producer_net; recalcula a fórmula e confere com a coluna sintética.
-- ===========================================================================
SELECT
    CASE WHEN o.producer_net = (o.total - o.affiliate_amount - o.delivery_fee - o.transaction_fee)
         THEN 'PASS' ELSE 'FAIL' END AS resultado,
    'ASSERT 4 [SINTETICO] producer_net' AS assert,
    (o.total - o.affiliate_amount - o.delivery_fee - o.transaction_fee) AS esperado,
    o.producer_net AS obtido,
    'total=' || o.total || ' aff=' || o.affiliate_amount
        || ' delivery=' || o.delivery_fee || ' txfee=' || o.transaction_fee AS ref
FROM sz_orders o
WHERE o.id = :oid
\gset a4_
\echo :a4_resultado '|' :a4_assert '| esperado=' :a4_esperado '| obtido=' :a4_obtido '|' :a4_ref

\echo
\echo '-- DESFAZENDO TUDO (ROLLBACK): nenhum dado sintético é persistido.'
ROLLBACK;

-- Exit status: se QUALQUER assert falhou, sai com código != 0 (CI/wrappers leem
-- vermelho como vermelho). O ROLLBACK acima já ocorreu — nada persiste de todo
-- jeito. Avaliado após o ROLLBACK pois \quit encerra a sessão imediatamente.
SELECT CASE
         WHEN :'a1_resultado' = 'PASS'
          AND :'a2_resultado' = 'PASS'
          AND :'a3_resultado' = 'PASS'
          AND :'a4_resultado' = 'PASS'
         THEN 0 ELSE 1 END AS rc
\gset
\if :rc
  \echo '-- RESULTADO: 1+ assert FALHOU.'
  \quit 1
\else
  \echo '-- RESULTADO: todos os asserts passaram.'
\endif

SQL

echo
echo "== fim (transação revertida — banco intacto) =="
