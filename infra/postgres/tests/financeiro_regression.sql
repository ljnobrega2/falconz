-- =============================================================================
-- SUÍTE DE REGRESSÃO FINANCEIRA DO FALK LOG
-- =============================================================================
-- Trava a FÓRMULA OFICIAL do dono contra a view `sz_order_financeiro` (fonte
-- única, definida em 390-blindagem-financeira.sql) e contra as options de taxa.
--
-- Rodar:
--   psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f infra/postgres/tests/financeiro_regression.sql
--   -> SAI 0 se TUDO passa; SAI !=0 (RAISE EXCEPTION) se QUALQUER valor divergir.
--
-- ESCOPO / SEGURANÇA:
--   - Só LÊ a view para o pedido real #1587 (read-only, sem tocar dado).
--   - Pedidos SINTÉTICOS (IDs 9990001+) entram num bloco BEGIN…ROLLBACK e NUNCA
--     são commitados. Nenhuma linha sintética vaza.
--   - NÃO altera Go/React. NÃO altera dado real. NÃO reinicia serviços.
--
-- FÓRMULA OFICIAL (fonte da verdade — muda só via admin nas options
--   `sz_producer_transaction_fee_pct` default 4.99 e `motoboy_repasse_padrao`
--   default 18; taxa do afiliado é fixa 4,99% gravada em transaction_fee):
--     bruta                   = affiliate_amount + transaction_fee
--     taxa_transacao_afiliado = transaction_fee
--     comissao_afil_liquida   = affiliate_amount
--     taxa_entrega            = delivery_fee
--     taxa_transacao_produtor = ROUND(total × pct/100, 2)
--     liquido_produtor        = total − bruta − delivery_fee − taxa_transacao_produtor
--   FRUST-V2 (422-frustracao-view.sql): FRUSTRADO ZERA TODO o financeiro NORMAL
--   do produtor E do afiliado (liquido_produtor, taxa_transacao_produtor,
--   taxa_entrega, comissao_afiliado_bruta/liquida, taxa_transacao_afiliado = 0).
--   A cobrança vira a PENALTY congelada (taxa_frustracao_produtor/_afiliado, lidas
--   das colunas frozen de sz_motoboy_pedidos). Zera quando:
--     EXISTS(motoboy status='frustrado' p/ COALESCE(wp_order_id,id))  -- FRUST-V2
--     OU status terminal-fail SEM entrega motoboy ativa (<> 'cancelado').
--
--   NOTA: na fórmula ANTERIOR um pedido motoboy 'frustrado' contava como "entrega
--   ativa" (<> 'cancelado') e NÃO zerava o produtor. FRUST-V2 inverte isso: motoboy
--   'frustrado' agora É o gatilho de cobrança. O cenário (c) abaixo (frustrado SEM
--   motoboy) zera tudo via o ramo terminal-fail; (d) usa 'em_rota' (entrega ativa,
--   sem frustração) → NÃO zera.
-- =============================================================================

\set ON_ERROR_STOP on

-- -----------------------------------------------------------------------------
-- PRÉ-CONDIÇÃO: travar as taxas configuráveis nos defaults oficiais.
-- Se o admin tiver mudado, TODOS os esperados (golden #1587 + sintéticos) mudam;
-- a suíte aborta em vez de validar contra números errados. Fechamos o buraco em
-- que "esperado lê a mesma option que a view lê" mascararia um drift.
-- -----------------------------------------------------------------------------
DO $precond$
DECLARE
    v_pct     numeric;
    v_repasse numeric;
BEGIN
    SELECT NULLIF(value,'')::numeric INTO v_pct
      FROM senderzz_options WHERE name = 'sz_producer_transaction_fee_pct';
    SELECT NULLIF(value,'')::numeric INTO v_repasse
      FROM senderzz_options WHERE name = 'motoboy_repasse_padrao';

    IF COALESCE(v_pct, 4.99) <> 4.99 THEN
        RAISE EXCEPTION
          '[regressao] PRECOND FALHOU: sz_producer_transaction_fee_pct=% (esperado 4.99). Esperados travados no default — aborta.',
          v_pct;
    END IF;
    IF COALESCE(v_repasse, 18) <> 18 THEN
        RAISE EXCEPTION
          '[regressao] PRECOND FALHOU: motoboy_repasse_padrao=% (esperado 18). Esperados travados no default — aborta.',
          v_repasse;
    END IF;

    RAISE NOTICE '[regressao] PRECOND OK: pct=4.99, repasse=18.';
END;
$precond$;


-- =============================================================================
-- BLOCO 1 — GOLDEN ANCHOR #1587 (pedido REAL, read-only)
-- =============================================================================
-- Lê a view para o pedido real e trava os 6 valores oficiais já provados.
-- Tolerância 0,01. O pedido #1587 tem id=1587 E wp_order_id=1587 (chaveamos por
-- wp_order_id, que é a chave de negócio que a view também usa no EXISTS).
DO $golden$
DECLARE
    r RECORD;
    -- valores GOLDEN provados (não derivados — constantes literais):
    g_bruta        numeric := 150.00;
    g_taxa_afil    numeric :=   7.49;
    g_liq_afil     numeric := 142.51;
    g_entrega      numeric :=  23.98;
    g_taxa_prod    numeric :=  12.48;
    g_liq_prod     numeric :=  63.54;
    -- take_falk = taxa_afil + taxa_prod + markup_entrega = 7.49 + 12.48 + max(23.98−18,0)=5.98 = 25.95.
    -- Único ponto que trava a aritmética do MARKUP de entrega (delivery−repasse, piso 0).
    g_take_falk    numeric :=  25.95;
    tol            numeric := 0.01;
BEGIN
    SELECT * INTO r FROM sz_order_financeiro WHERE wp_order_id = 1587;

    IF NOT FOUND THEN
        RAISE EXCEPTION '[regressao][golden#1587] pedido 1587 NÃO encontrado na view.';
    END IF;

    IF ABS(r.comissao_afiliado_bruta - g_bruta) > tol THEN
        RAISE EXCEPTION '[regressao][golden#1587] bruta=% esperado % (Δ=%).',
            r.comissao_afiliado_bruta, g_bruta, r.comissao_afiliado_bruta - g_bruta;
    END IF;
    IF ABS(r.taxa_transacao_afiliado - g_taxa_afil) > tol THEN
        RAISE EXCEPTION '[regressao][golden#1587] taxa_afiliado=% esperado % (Δ=%).',
            r.taxa_transacao_afiliado, g_taxa_afil, r.taxa_transacao_afiliado - g_taxa_afil;
    END IF;
    IF ABS(r.comissao_afiliado_liquida - g_liq_afil) > tol THEN
        RAISE EXCEPTION '[regressao][golden#1587] liquida_afiliado=% esperado % (Δ=%).',
            r.comissao_afiliado_liquida, g_liq_afil, r.comissao_afiliado_liquida - g_liq_afil;
    END IF;
    IF ABS(r.taxa_entrega - g_entrega) > tol THEN
        RAISE EXCEPTION '[regressao][golden#1587] entrega=% esperado % (Δ=%).',
            r.taxa_entrega, g_entrega, r.taxa_entrega - g_entrega;
    END IF;
    IF ABS(r.taxa_transacao_produtor - g_taxa_prod) > tol THEN
        RAISE EXCEPTION '[regressao][golden#1587] taxa_produtor=% esperado % (Δ=%).',
            r.taxa_transacao_produtor, g_taxa_prod, r.taxa_transacao_produtor - g_taxa_prod;
    END IF;
    IF ABS(r.liquido_produtor - g_liq_prod) > tol THEN
        RAISE EXCEPTION '[regressao][golden#1587] liquido_produtor=% esperado % (Δ=%).',
            r.liquido_produtor, g_liq_prod, r.liquido_produtor - g_liq_prod;
    END IF;
    -- trava extra (markup de entrega): não está entre os 6 valores do dono mas é
    -- a receita FALK real do pedido — locka delivery_fee − repasse.
    IF ABS(r.take_falk - g_take_falk) > tol THEN
        RAISE EXCEPTION '[regressao][golden#1587] take_falk=% esperado % (Δ=%).',
            r.take_falk, g_take_falk, r.take_falk - g_take_falk;
    END IF;

    RAISE NOTICE '[regressao][golden#1587] OK: bruta=% taxaAfil=% liqAfil=% entrega=% taxaProd=% liqProd=% takeFalk=%.',
        r.comissao_afiliado_bruta, r.taxa_transacao_afiliado, r.comissao_afiliado_liquida,
        r.taxa_entrega, r.taxa_transacao_produtor, r.liquido_produtor, r.take_falk;
END;
$golden$;


-- =============================================================================
-- BLOCO 2 — CENÁRIOS SINTÉTICOS (BEGIN…ROLLBACK — nada vaza)
-- =============================================================================
-- Insere pedidos com IDs altos (9990001+) e assere a view contra a fórmula
-- calculada à mão EM SQL (não relendo a option — usamos a constante 4.99/18
-- já travada no PRECOND). Cada divergência > 0,01 → RAISE EXCEPTION (aborta a
-- txn). O ROLLBACK final limpa o caminho verde.
BEGIN;

-- (a) COM afiliado, COD/motoboy (AUDIT-2026-07-30: taxa_transacao_produtor só
--     existe pra motoboy — precisa de linha ativa em sz_motoboy_pedidos).
--     total=200, affiliate_amount=100, transaction_fee=5 (4,99% bruta≈5),
--     delivery_fee=30. status entregável ('completo') — não zera.
--     bruta=105, taxa_afil=5, liq_afil=100, entrega=30,
--     taxa_prod=ROUND(200×4.99/100,2)=9.98, liq_prod=200−105−30−9.98=55.02.
INSERT INTO sz_orders
    (id, order_number, user_id, produtor_id, affiliate_id, status, wp_order_id,
     total, affiliate_amount, transaction_fee, delivery_fee)
OVERRIDING SYSTEM VALUE
VALUES
    (9990001, 'RGT-9990001', 1, 15, 28, 'completo', 9990001,
     200.00, 100.00, 5.00, 30.00);
INSERT INTO sz_motoboy_pedidos (id, wc_order_id, cd_id, zona_id, dest_cep, status)
OVERRIDING SYSTEM VALUE
VALUES (9990001, 9990001, 1, 1, '00000000', 'entregue');

-- (b) SEM afiliado, COD/motoboy.
--     total=200, affiliate_amount=0, transaction_fee=0, delivery_fee=30,
--     status='completo'. bruta=0, taxa_afil=0, liq_afil=0, entrega=30,
--     taxa_prod=9.98, liq_prod=200−0−30−9.98=160.02.
INSERT INTO sz_orders
    (id, order_number, user_id, produtor_id, affiliate_id, status, wp_order_id,
     total, affiliate_amount, transaction_fee, delivery_fee)
OVERRIDING SYSTEM VALUE
VALUES
    (9990002, 'RGT-9990002', 1, 15, NULL, 'completo', 9990002,
     200.00, 0.00, 0.00, 30.00);
INSERT INTO sz_motoboy_pedidos (id, wc_order_id, cd_id, zona_id, dest_cep, status)
OVERRIDING SYSTEM VALUE
VALUES (9990002, 9990002, 1, 1, '00000000', 'entregue');

-- (c) FRUSTRADO sem motoboy ativo (nenhuma linha em sz_motoboy_pedidos).
--     total=200, affiliate_amount=100, transaction_fee=5, delivery_fee=30,
--     status='frustrado'. FRUST-V2: ZERA TODO o financeiro NORMAL — produtor
--     (liq+taxa) E afiliado (bruta=0, taxa_afil=0, liq_afil=0) E taxa_entrega=0.
--     Sem motoboy frustrado → penalties congeladas = 0 → take_falk=0.
INSERT INTO sz_orders
    (id, order_number, user_id, produtor_id, affiliate_id, status, wp_order_id,
     total, affiliate_amount, transaction_fee, delivery_fee)
OVERRIDING SYSTEM VALUE
VALUES
    (9990003, 'RGT-9990003', 1, 15, 28, 'frustrado', 9990003,
     200.00, 100.00, 5.00, 30.00);

-- (d) FRUSTRADO COM motoboy ATIVO ('em_rota').
--     Mesmos valores de (c), mas existe pedido motoboy ativo casando
--     wc_order_id = wp_order_id (a view usa bare o.wp_order_id = m.wc_order_id).
--     => NÃO zera: taxa_prod=9.98, liq_prod=200−105−30−9.98=55.02.
INSERT INTO sz_orders
    (id, order_number, user_id, produtor_id, affiliate_id, status, wp_order_id,
     total, affiliate_amount, transaction_fee, delivery_fee)
OVERRIDING SYSTEM VALUE
VALUES
    (9990004, 'RGT-9990004', 1, 15, 28, 'frustrado', 9990004,
     200.00, 100.00, 5.00, 30.00);

-- entrega motoboy ATIVA para (d): wc_order_id casa com o wp_order_id 9990004.
INSERT INTO sz_motoboy_pedidos
    (id, wc_order_id, cd_id, zona_id, dest_cep, status)
OVERRIDING SYSTEM VALUE
VALUES
    (9990004, 9990004, 1, 1, '00000000', 'em_rota');

-- (e) COM afiliado, PAD/expedição (SEM linha em sz_motoboy_pedidos).
--     AUDIT-2026-07-30 (dono): "PAD não tem taxa de transação" — taxa_prod=0,
--     liq_prod = total − bruta − delivery_fee (SEM subtrair taxa_prod).
--     total=200, affiliate_amount=100, transaction_fee=5, delivery_fee=0
--     (PAD não usa a taxa fixa COD — frete real é rastreado à parte).
--     bruta=105, taxa_afil=5, liq_afil=100, entrega=0, taxa_prod=0,
--     liq_prod=200−105−0−0=95.00.
INSERT INTO sz_orders
    (id, order_number, user_id, produtor_id, affiliate_id, status, wp_order_id,
     total, affiliate_amount, transaction_fee, delivery_fee)
OVERRIDING SYSTEM VALUE
VALUES
    (9990005, 'RGT-9990005', 1, 15, 28, 'completo', 9990005,
     200.00, 100.00, 5.00, 0.00);


-- ----- ASSERÇÕES (calc-à-mão em SQL vs view; tolerância 0,01) -----
-- Cada cenário gera um conjunto (cenario, campo, got, esperado) e um único loop
-- assere |got − esperado| <= 0,01, com RAISE EXCEPTION na 1ª divergência. Os
-- esperados são constantes derivadas à mão dos inputs (pct=4.99 travado no
-- PRECOND), NÃO relidos da option — fecha o buraco do drift mascarado.
DO $cenarios$
DECLARE
    a   sz_order_financeiro%ROWTYPE;
    b   sz_order_financeiro%ROWTYPE;
    c   sz_order_financeiro%ROWTYPE;
    d   sz_order_financeiro%ROWTYPE;
    e   sz_order_financeiro%ROWTYPE;
    chk RECORD;
    pct numeric := 4.99;   -- travado no PRECOND
BEGIN
    SELECT * INTO a FROM sz_order_financeiro WHERE wp_order_id = 9990001;
    IF NOT FOUND THEN RAISE EXCEPTION '[regressao][a] linha 9990001 ausente na view.'; END IF;
    SELECT * INTO b FROM sz_order_financeiro WHERE wp_order_id = 9990002;
    IF NOT FOUND THEN RAISE EXCEPTION '[regressao][b] linha 9990002 ausente na view.'; END IF;
    SELECT * INTO c FROM sz_order_financeiro WHERE wp_order_id = 9990003;
    IF NOT FOUND THEN RAISE EXCEPTION '[regressao][c] linha 9990003 ausente na view.'; END IF;
    SELECT * INTO d FROM sz_order_financeiro WHERE wp_order_id = 9990004;
    IF NOT FOUND THEN RAISE EXCEPTION '[regressao][d] linha 9990004 ausente na view.'; END IF;
    SELECT * INTO e FROM sz_order_financeiro WHERE wp_order_id = 9990005;
    IF NOT FOUND THEN RAISE EXCEPTION '[regressao][e] linha 9990005 ausente na view.'; END IF;

    -- esperados: ver comentários dos INSERTs acima. ROUND(200×4.99/100,2)=9.98.
    FOR chk IN
        SELECT * FROM (VALUES
            -- (a) COM afiliado, completo → não zera
            ('[a] bruta',     a.comissao_afiliado_bruta,   105.00::numeric),
            ('[a] taxa_afil', a.taxa_transacao_afiliado,     5.00::numeric),
            ('[a] liq_afil',  a.comissao_afiliado_liquida, 100.00::numeric),
            ('[a] entrega',   a.taxa_entrega,               30.00::numeric),
            ('[a] taxa_prod', a.taxa_transacao_produtor,   ROUND(200.00 * pct / 100, 2)),
            ('[a] liq_prod',  a.liquido_produtor,          (200.00 - 105.00 - 30.00 - ROUND(200.00 * pct / 100, 2))),
            -- (b) SEM afiliado, completo → bruta=0, liq_prod=total−entrega−taxa_prod
            ('[b] bruta',     b.comissao_afiliado_bruta,     0.00::numeric),
            ('[b] taxa_afil', b.taxa_transacao_afiliado,     0.00::numeric),
            ('[b] liq_afil',  b.comissao_afiliado_liquida,   0.00::numeric),
            ('[b] entrega',   b.taxa_entrega,               30.00::numeric),
            ('[b] taxa_prod', b.taxa_transacao_produtor,   ROUND(200.00 * pct / 100, 2)),
            ('[b] liq_prod',  b.liquido_produtor,          (200.00 - 0.00 - 30.00 - ROUND(200.00 * pct / 100, 2))),
            -- (c) FRUSTRADO sem motoboy ativo → FRUST-V2 ZERA TODO o normal
            -- (produtor E afiliado E entrega). Penalties congeladas = 0 (sem motoboy).
            ('[c] bruta',     c.comissao_afiliado_bruta,     0.00::numeric),
            ('[c] taxa_afil', c.taxa_transacao_afiliado,     0.00::numeric),
            ('[c] liq_afil',  c.comissao_afiliado_liquida,   0.00::numeric),
            ('[c] entrega',   c.taxa_entrega,                0.00::numeric),
            ('[c] taxa_prod', c.taxa_transacao_produtor,     0.00::numeric),
            ('[c] liq_prod',  c.liquido_produtor,            0.00::numeric),
            -- (d) FRUSTRADO com motoboy ativo (em_rota) → NÃO zera
            ('[d] bruta',     d.comissao_afiliado_bruta,   105.00::numeric),
            ('[d] taxa_afil', d.taxa_transacao_afiliado,     5.00::numeric),
            ('[d] liq_afil',  d.comissao_afiliado_liquida, 100.00::numeric),
            ('[d] entrega',   d.taxa_entrega,               30.00::numeric),
            ('[d] taxa_prod', d.taxa_transacao_produtor,   ROUND(200.00 * pct / 100, 2)),
            ('[d] liq_prod',  d.liquido_produtor,          (200.00 - 105.00 - 30.00 - ROUND(200.00 * pct / 100, 2))),
            -- (e) PAD/expedição (sem motoboy) → SEM taxa de transação (dono)
            ('[e] bruta',     e.comissao_afiliado_bruta,   105.00::numeric),
            ('[e] taxa_afil', e.taxa_transacao_afiliado,     5.00::numeric),
            ('[e] liq_afil',  e.comissao_afiliado_liquida, 100.00::numeric),
            ('[e] entrega',   e.taxa_entrega,                0.00::numeric),
            ('[e] taxa_prod', e.taxa_transacao_produtor,     0.00::numeric),
            ('[e] liq_prod',  e.liquido_produtor,           95.00::numeric)
        ) AS t(label, got, esperado)
    LOOP
        IF chk.got IS NULL OR ABS(chk.got - chk.esperado) > 0.01 THEN
            RAISE EXCEPTION '[regressao]% got=% esperado % (Δ=%).',
                chk.label, chk.got, chk.esperado, COALESCE(chk.got, 0) - chk.esperado;
        END IF;
    END LOOP;

    -- flags 'frustrado' (a view zera produtor SÓ quando esta flag é TRUE):
    IF a.frustrado     THEN RAISE EXCEPTION '[regressao][a] flag frustrado=TRUE inesperada (completo).'; END IF;
    IF b.frustrado     THEN RAISE EXCEPTION '[regressao][b] flag frustrado=TRUE inesperada (completo).'; END IF;
    IF NOT c.frustrado THEN RAISE EXCEPTION '[regressao][c] flag frustrado=FALSE inesperada (frustrado sem motoboy ativo).'; END IF;
    IF d.frustrado     THEN RAISE EXCEPTION '[regressao][d] flag frustrado=TRUE inesperada (motoboy em_rota ativo mantém ativo).'; END IF;

    RAISE NOTICE '[regressao][a] COM afiliado OK (liq_prod=%).', a.liquido_produtor;
    RAISE NOTICE '[regressao][b] SEM afiliado OK (liq_prod=%).', b.liquido_produtor;
    RAISE NOTICE '[regressao][c] FRUSTRADO sem motoboy OK — FRUST-V2 zerou normal (bruta=%, entrega=%, taxa_prod=%, liq_prod=%).', c.comissao_afiliado_bruta, c.taxa_entrega, c.taxa_transacao_produtor, c.liquido_produtor;
    RAISE NOTICE '[regressao][d] FRUSTRADO com motoboy ativo OK (taxa_prod=%, liq_prod=%).', d.taxa_transacao_produtor, d.liquido_produtor;
    RAISE NOTICE '[regressao][e] PAD/expedição OK — sem taxa de transação (taxa_prod=%, liq_prod=%).', e.taxa_transacao_produtor, e.liquido_produtor;
    RAISE NOTICE '[regressao] TODOS os cenários sintéticos PASSARAM.';
END;
$cenarios$;

-- nada commitado: pedidos/motoboy sintéticos + qualquer linha de receita gerada
-- pelos triggers são descartados.
ROLLBACK;


-- =============================================================================
-- BLOCO 3 — GUARDA ANTI-VAZAMENTO (read-only, pós-rollback)
-- =============================================================================
-- Confirma que NENHUMA linha sintética (IDs 999xxxx) sobrou em sz_orders nem em
-- sz_motoboy_pedidos. Se o ROLLBACK falhou ou algo escapou, aborta.
DO $leak$
DECLARE
    n_orders  bigint;
    n_moto    bigint;
BEGIN
    SELECT count(*) INTO n_orders FROM sz_orders         WHERE id BETWEEN 9990000 AND 9999999;
    SELECT count(*) INTO n_moto   FROM sz_motoboy_pedidos WHERE id BETWEEN 9990000 AND 9999999;
    IF n_orders <> 0 OR n_moto <> 0 THEN
        RAISE EXCEPTION '[regressao][leak] VAZAMENTO: % pedido(s) e % motoboy(s) sintéticos sobraram.',
            n_orders, n_moto;
    END IF;
    RAISE NOTICE '[regressao][leak] OK: 0 linhas sintéticas em sz_orders e sz_motoboy_pedidos.';
END;
$leak$;

\echo '============================================================'
\echo '[regressao] SUÍTE FINANCEIRA PASSOU — golden #1587 + (a)(b)(c)(d) + anti-leak.'
\echo '============================================================'
