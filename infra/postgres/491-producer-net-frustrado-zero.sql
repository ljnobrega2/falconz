-- ============================================================================
-- 491 — liquido_produtor também zera em pedido frustrado (não só cancelado).
--
-- 490 removeu o gate "só entregue" mas só zerou p/ motoboy cancelado/cancelled.
-- A golden 478 documenta explicitamente (linha 65): "frustrado/estornado o
-- produtor não recebe" — faltou incluir 'frustrado' no CASE de 490. Sem isso,
-- pedidos frustrados passaram a mostrar comissão produtor > 0 (regressão nova,
-- introduzida pela própria 490).
--
-- Idempotente (CREATE OR REPLACE FUNCTION + backfill sempre seguro de re-rodar).
-- ============================================================================

CREATE OR REPLACE FUNCTION public.sz_financials_refresh(p_order_id bigint)
 RETURNS void
 LANGUAGE plpgsql
AS $function$
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

  SELECT COALESCE(mp.status,''), COALESCE(mp.valor_taxa,0), COALESCE(mp.valor_taxa_frustrado,0)
  INTO v_status_mb, v_taxa_entrega, v_taxa_frustrado
  FROM sz_motoboy_pedidos mp
  WHERE mp.wc_order_id = COALESCE(
    (SELECT wp_order_id FROM sz_orders WHERE id = p_order_id), p_order_id)
  ORDER BY mp.id DESC LIMIT 1;

  v_status_mb := COALESCE(v_status_mb, '');

  v_afil_bruta     := ROUND(v_afil_net / (1 - v_fee_afil), 2);
  v_taxa_plat_prod := ROUND(v_total    * v_fee_prod, 2);
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
    updated_at                = NOW();
END;
$function$;

-- BACKFILL: recalcula todos os pedidos existentes com a fórmula corrigida.
DO $$
DECLARE
  r RECORD;
BEGIN
  FOR r IN SELECT id FROM sz_orders WHERE produtor_id IS NOT NULL LOOP
    PERFORM sz_financials_refresh(r.id);
  END LOOP;
END $$;
