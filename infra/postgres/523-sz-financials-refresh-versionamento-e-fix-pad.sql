-- AUDIT-2026-07-31 CRITICAL — descoberta ao vivo (dono: "manda bala", rodando
-- 520/521 direto em prod pela primeira vez): sz_order_financials em produção
-- NÃO é a view de 478-financial-canonical-view.sql. É uma TABELA mantida por
-- 4 triggers (sz_orders/sz_motoboy_pedidos/sz_cod_wallet_transactions/
-- senderzz_options) chamando sz_financials_refresh(order_id) — sistema
-- inteiro construído DIRETO em produção em algum momento, NUNCA versionado
-- neste repo (só existe um comentário em 482-producer-net-live-compat.sql
-- avisando "482... a tabela é trigger-mantida", mas o DDL/função/triggers de
-- verdade nunca foram commitados). 520 tentou fazer CREATE OR REPLACE VIEW
-- num nome que já é TABELA — Postgres recusou (DROP VIEW numa tabela é erro),
-- abortou ANTES de tocar em qualquer dado. Nada foi perdido; esta migração é
-- a correção real.
--
-- Esta migração:
--   1. Versiona o sistema inteiro pela primeira vez (tabela + função +
--      4 triggers), IDEMPOTENTE (CREATE TABLE IF NOT EXISTS casa exatamente
--      com o schema real de prod — obtido via pg_dump direto do banco).
--   2. Corrige o MESMO bug de 520/521 (taxa de transação do produtor
--      incondicional) dentro de sz_financials_refresh: PAD/expedição (sem
--      linha em sz_motoboy_pedidos) não paga taxa_plataforma_produtor.
--   3. Roda sz_financials_refresh(id) pra TODOS os pedidos existentes,
--      recalculando com a fórmula corrigida (usa a função real, não um
--      UPDATE paralelo — garante que o dado bate 100% com a lógica viva).

-- ── (1) Tabela — idempotente, só cria se não existir (prod já tem) ─────────
-- AUDIT-2026-07-31: em ambientes que só rodaram 520/521 (dev — nunca teve o
-- sistema trigger-mantido de prod), sz_order_financials existe como VIEW.
-- A tabela real de produção (esta migração) é a fonte de verdade daqui pra
-- frente — remove a view antes de criar a tabela, senão o nome colide.
DO $$
BEGIN
  IF (SELECT relkind FROM pg_class WHERE relname = 'sz_order_financials') = 'v' THEN
    DROP VIEW sz_order_financials;
  END IF;
END $$;

CREATE TABLE IF NOT EXISTS sz_order_financials (
    order_id bigint NOT NULL PRIMARY KEY,
    produtor_id bigint,
    affiliate_id bigint,
    pedido_at timestamp with time zone,
    status_motoboy text DEFAULT ''::text NOT NULL,
    total_pedido numeric(10,2) DEFAULT 0 NOT NULL,
    frete_comprador numeric(10,2) DEFAULT 0 NOT NULL,
    comissao_afiliado_net numeric(10,2) DEFAULT 0 NOT NULL,
    comissao_afiliado_bruta numeric(10,2) DEFAULT 0 NOT NULL,
    fee_produtor_pct numeric(6,4) DEFAULT 0.0499 NOT NULL,
    fee_afiliado_pct numeric(6,4) DEFAULT 0.0499 NOT NULL,
    taxa_plataforma_produtor numeric(10,2) DEFAULT 0 NOT NULL,
    taxa_plataforma_afiliado numeric(10,2) DEFAULT 0 NOT NULL,
    taxa_entrega numeric(10,2) DEFAULT 0 NOT NULL,
    taxa_frustrado numeric(10,2) DEFAULT 0 NOT NULL,
    liquido_produtor numeric(10,2) DEFAULT 0 NOT NULL,
    wallet_tx_id bigint,
    wallet_creditado numeric(10,2),
    wallet_at timestamp with time zone,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                  WHERE table_name='sz_order_financials' AND column_name='producer_net_live') THEN
    ALTER TABLE sz_order_financials ADD COLUMN producer_net_live numeric GENERATED ALWAYS AS (liquido_produtor) STORED;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                  WHERE table_name='sz_order_financials' AND column_name='delivery_fee') THEN
    ALTER TABLE sz_order_financials ADD COLUMN delivery_fee numeric GENERATED ALWAYS AS (taxa_entrega) STORED;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                  WHERE table_name='sz_order_financials' AND column_name='affiliate_bruta') THEN
    ALTER TABLE sz_order_financials ADD COLUMN affiliate_bruta numeric GENERATED ALWAYS AS (comissao_afiliado_bruta) STORED;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                  WHERE table_name='sz_order_financials' AND column_name='producer_take') THEN
    ALTER TABLE sz_order_financials ADD COLUMN producer_take numeric GENERATED ALWAYS AS (taxa_plataforma_produtor) STORED;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                  WHERE table_name='sz_order_financials' AND column_name='affiliate_liquida') THEN
    ALTER TABLE sz_order_financials ADD COLUMN affiliate_liquida numeric GENERATED ALWAYS AS (comissao_afiliado_net) STORED;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                  WHERE table_name='sz_order_financials' AND column_name='affiliate_take') THEN
    ALTER TABLE sz_order_financials ADD COLUMN affiliate_take numeric GENERATED ALWAYS AS (taxa_plataforma_afiliado) STORED;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                  WHERE table_name='sz_order_financials' AND column_name='total') THEN
    ALTER TABLE sz_order_financials ADD COLUMN total numeric GENERATED ALWAYS AS (total_pedido) STORED;
  END IF;
END $$;

CREATE INDEX IF NOT EXISTS sz_order_financials_affiliate_id_idx ON sz_order_financials (affiliate_id);
CREATE INDEX IF NOT EXISTS sz_order_financials_produtor_id_idx ON sz_order_financials (produtor_id);
CREATE INDEX IF NOT EXISTS sz_order_financials_status_motoboy_idx ON sz_order_financials (status_motoboy);

-- ── (2) Função sz_financials_refresh — FIX: taxa produtor só em COD/motoboy ─
CREATE OR REPLACE FUNCTION sz_financials_refresh(p_order_id bigint)
RETURNS void LANGUAGE plpgsql AS $function$
DECLARE
  v_produtor_id    BIGINT;
  v_affiliate_id   BIGINT;
  v_pedido_at      TIMESTAMPTZ;
  v_status_mb      TEXT;
  v_total          NUMERIC(10,2);
  v_shipping       NUMERIC(10,2);
  v_afil_net       NUMERIC(10,2);
  v_afil_bruta     NUMERIC(10,2);
  v_fee_prod       NUMERIC(6,4);
  v_fee_afil       NUMERIC(6,4);
  v_taxa_plat_prod NUMERIC(10,2);
  v_taxa_plat_afil NUMERIC(10,2);
  v_taxa_entrega   NUMERIC(10,2);
  v_taxa_frustrado NUMERIC(10,2);
  v_liquido        NUMERIC(10,2);
  v_wallet_id      BIGINT;
  v_wallet_gross   NUMERIC(10,2);
  v_wallet_at      TIMESTAMPTZ;
  v_opt_val        TEXT;
  v_is_motoboy     BOOLEAN;
BEGIN
  SELECT o.produtor_id, o.affiliate_id, o.created_at,
         o.total, COALESCE(o.shipping,0), COALESCE(o.affiliate_amount,0)
  INTO v_produtor_id, v_affiliate_id, v_pedido_at,
       v_total, v_shipping, v_afil_net
  FROM sz_orders o WHERE o.id = p_order_id;

  IF NOT FOUND OR v_produtor_id IS NULL THEN RETURN; END IF;

  SELECT value INTO v_opt_val FROM senderzz_options
  WHERE name = 'sz_producer_transaction_fee_pct' LIMIT 1;
  v_fee_prod := COALESCE(NULLIF(TRIM(v_opt_val),'')::NUMERIC / 100, 0.0499);

  SELECT value INTO v_opt_val FROM senderzz_options
  WHERE name = 'sz_affiliate_transaction_fee_pct' LIMIT 1;
  v_fee_afil := COALESCE(NULLIF(TRIM(v_opt_val),'')::NUMERIC / 100, 0.0499);

  -- AUDIT-2026-07-31: v_is_motoboy via EXISTS explícito — NÃO infere de
  -- v_status_mb='' (uma linha real com status vazio, embora improvável,
  -- não pode ser confundida com "não existe linha").
  SELECT EXISTS (
    SELECT 1 FROM sz_motoboy_pedidos mp
     WHERE mp.wc_order_id = COALESCE((SELECT wp_order_id FROM sz_orders WHERE id = p_order_id), p_order_id)
  ) INTO v_is_motoboy;

  SELECT COALESCE(mp.status,''), COALESCE(mp.valor_taxa,0), COALESCE(mp.valor_taxa_frustrado,0)
  INTO v_status_mb, v_taxa_entrega, v_taxa_frustrado
  FROM sz_motoboy_pedidos mp
  WHERE mp.wc_order_id = COALESCE(
    (SELECT wp_order_id FROM sz_orders WHERE id = p_order_id), p_order_id)
  ORDER BY mp.id DESC LIMIT 1;

  -- AUDIT-2026-07-31 CRITICAL — segundo bug achado testando contra clone real
  -- de produção: quando não existe linha em sz_motoboy_pedidos (todo pedido
  -- PAD), o SELECT acima devolve v_taxa_entrega/v_taxa_frustrado NULL (só
  -- v_status_mb tinha COALESCE). O COALESCE(v_taxa_entrega,0) só rodava tarde
  -- demais, no INSERT (pra gravar a COLUNA) — a MULTIPLICAÇÃO/SUBTRAÇÃO de
  -- v_liquido usava a variável ainda NULL: `v_total - ... - v_taxa_entrega`
  -- vira NULL, e GREATEST(NULL, 0) em Postgres retorna 0 (GREATEST ignora
  -- NULL entre os argumentos). Resultado: liquido_produtor sempre foi 0,00
  -- pra TODO pedido PAD, sempre — confirmado comparando contra o backup de
  -- produção anterior a esta migração (mesmo valor 0,00 já lá, mesmo com
  -- total=327 e nenhum desconto real). Bug pré-existente, não introduzido
  -- pela correção de taxa desta sessão — só ficou visível ao testar de
  -- verdade contra dado real.
  v_taxa_entrega   := COALESCE(v_taxa_entrega, 0);
  v_taxa_frustrado := COALESCE(v_taxa_frustrado, 0);

  v_status_mb := COALESCE(v_status_mb, '');

  v_afil_bruta     := ROUND(v_afil_net / (1 - v_fee_afil), 2);
  -- AUDIT-2026-07-31 CRITICAL (dono): "PAD não tem taxa de transação" — só
  -- COD/motoboy desconta taxa_plataforma_produtor. Mesmo bug de 520/521,
  -- achado nesta tabela paralela (sz_order_financials, mantida por trigger,
  -- não a view de 478) rodando pela primeira vez contra prod real.
  v_taxa_plat_prod := CASE WHEN v_is_motoboy THEN ROUND(v_total * v_fee_prod, 2) ELSE 0 END;
  v_taxa_plat_afil := ROUND(v_afil_bruta * v_fee_afil, 2);
  -- AUDIT-2026-07-11 (491): 'frustrado' entra no gate de zero, junto com
  -- cancelado/cancelled (mesma regra documentada na golden 478).
  v_liquido        := CASE WHEN v_status_mb IN ('cancelled','cancelado','frustrado')
    THEN 0
    ELSE GREATEST(v_total - v_afil_bruta - v_taxa_plat_prod - v_taxa_entrega, 0)
  END;

  SELECT id, gross, created_at INTO v_wallet_id, v_wallet_gross, v_wallet_at
  FROM sz_cod_wallet_transactions
  WHERE order_id = p_order_id AND type = 'cod_received'
  ORDER BY id DESC LIMIT 1;

  INSERT INTO sz_order_financials (
    order_id, produtor_id, affiliate_id, pedido_at,
    status_motoboy, total_pedido, frete_comprador,
    comissao_afiliado_net, comissao_afiliado_bruta,
    fee_produtor_pct, fee_afiliado_pct,
    taxa_plataforma_produtor, taxa_plataforma_afiliado,
    taxa_entrega, taxa_frustrado,
    liquido_produtor,
    wallet_tx_id, wallet_creditado, wallet_at, updated_at
  ) VALUES (
    p_order_id, v_produtor_id, v_affiliate_id, v_pedido_at,
    v_status_mb, v_total, v_shipping,
    v_afil_net, v_afil_bruta,
    v_fee_prod, v_fee_afil,
    v_taxa_plat_prod, v_taxa_plat_afil,
    COALESCE(v_taxa_entrega,0), COALESCE(v_taxa_frustrado,0),
    v_liquido,
    v_wallet_id, v_wallet_gross, v_wallet_at, NOW()
  )
  ON CONFLICT (order_id) DO UPDATE SET
    produtor_id              = EXCLUDED.produtor_id,
    affiliate_id             = EXCLUDED.affiliate_id,
    pedido_at                = EXCLUDED.pedido_at,
    status_motoboy           = EXCLUDED.status_motoboy,
    total_pedido             = EXCLUDED.total_pedido,
    frete_comprador          = EXCLUDED.frete_comprador,
    comissao_afiliado_net    = EXCLUDED.comissao_afiliado_net,
    comissao_afiliado_bruta  = EXCLUDED.comissao_afiliado_bruta,
    fee_produtor_pct         = EXCLUDED.fee_produtor_pct,
    fee_afiliado_pct         = EXCLUDED.fee_afiliado_pct,
    taxa_plataforma_produtor = EXCLUDED.taxa_plataforma_produtor,
    taxa_plataforma_afiliado = EXCLUDED.taxa_plataforma_afiliado,
    taxa_entrega             = EXCLUDED.taxa_entrega,
    taxa_frustrado           = EXCLUDED.taxa_frustrado,
    liquido_produtor         = EXCLUDED.liquido_produtor,
    wallet_tx_id             = EXCLUDED.wallet_tx_id,
    wallet_creditado         = EXCLUDED.wallet_creditado,
    wallet_at                = EXCLUDED.wallet_at,
    updated_at               = NOW();
END;
$function$;

-- ── (3) Triggers — idempotente (CREATE OR REPLACE FUNCTION + DROP/CREATE TRIGGER) ─
CREATE OR REPLACE FUNCTION trg_sz_orders_financials() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN PERFORM sz_financials_refresh(NEW.id); RETURN NEW; END;
$$;

CREATE OR REPLACE FUNCTION trg_sz_motoboy_financials() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE v_order_id BIGINT;
BEGIN
  SELECT id INTO v_order_id FROM sz_orders
  WHERE wp_order_id = NEW.wc_order_id OR id = NEW.wc_order_id LIMIT 1;
  IF v_order_id IS NOT NULL THEN PERFORM sz_financials_refresh(v_order_id); END IF;
  RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION trg_sz_wallet_financials() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.type = 'cod_received' AND NEW.order_id IS NOT NULL THEN
    PERFORM sz_financials_refresh(NEW.order_id);
  END IF;
  RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION trg_sz_options_financials() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.name IN ('sz_producer_transaction_fee_pct','sz_affiliate_transaction_fee_pct') THEN
    PERFORM sz_financials_refresh(id) FROM sz_orders WHERE produtor_id IS NOT NULL;
  END IF;
  RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS sz_orders_financials_sync ON sz_orders;
CREATE TRIGGER sz_orders_financials_sync
  AFTER INSERT OR UPDATE OF total, shipping, affiliate_amount, status, produtor_id, affiliate_id
  ON sz_orders FOR EACH ROW EXECUTE FUNCTION trg_sz_orders_financials();

DROP TRIGGER IF EXISTS sz_motoboy_financials_sync ON sz_motoboy_pedidos;
CREATE TRIGGER sz_motoboy_financials_sync
  AFTER INSERT OR UPDATE OF status, valor_taxa, valor_taxa_frustrado
  ON sz_motoboy_pedidos FOR EACH ROW EXECUTE FUNCTION trg_sz_motoboy_financials();

DROP TRIGGER IF EXISTS sz_wallet_financials_sync ON sz_cod_wallet_transactions;
CREATE TRIGGER sz_wallet_financials_sync
  AFTER INSERT OR UPDATE OF gross, status
  ON sz_cod_wallet_transactions FOR EACH ROW EXECUTE FUNCTION trg_sz_wallet_financials();

DROP TRIGGER IF EXISTS sz_options_financials_sync ON senderzz_options;
CREATE TRIGGER sz_options_financials_sync
  AFTER INSERT OR UPDATE OF value
  ON senderzz_options FOR EACH ROW EXECUTE FUNCTION trg_sz_options_financials();

-- ── (4) Backfill: recalcula TODOS os pedidos existentes com a fórmula corrigida ─
SELECT sz_financials_refresh(id) FROM sz_orders WHERE produtor_id IS NOT NULL;
