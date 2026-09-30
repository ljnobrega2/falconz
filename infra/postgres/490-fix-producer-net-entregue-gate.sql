-- ============================================================================
-- 490 — Corrige zeragem indevida de liquido_produtor (comissão produtor).
--
-- ROOT CAUSE: sz_order_financials em prod é mantida por trigger
-- (sz_financials_refresh, sem migration correspondente no repo — divergiu da
-- VIEW golden da 478). A função zerava liquido_produtor SEMPRE que o motoboy
-- não estivesse com status='entregue':
--
--   v_liquido := CASE WHEN v_status_mb = 'entregue' THEN GREATEST(...) ELSE 0 END;
--
-- A view golden (478-financial-canonical-view.sql) NUNCA gateou por status —
-- calcula GREATEST(total - bruta_afiliado - taxa_prod - delivery_fee, 0) sempre,
-- igual à comissão do afiliado (que não é gateada). Resultado do bug: qualquer
-- pedido "Agendado"/"Pendente"/etc (a maioria, já que só vira 'entregue' no
-- final) mostrava comissão produtor R$ 0,00 em TODAS as telas que leem
-- f.producer_net_live (Pedidos Motoboy, drawer do pedido, dashboard, Livro COD,
-- Audit Engine, Config Taxas, relatório do portal produtor) — mesmo com
-- afiliado e taxa Falk corretos e não-zerados no mesmo pedido.
--
-- FIX: remove o gate 'entregue'. Mantém zero só para motoboy cancelado
-- (pedido não vai ser entregue → produtor não tem direito a comissão desse
-- frete) — mesma exceção que a 478 já previa para status de pedido
-- frustrado/cancelado/reembolsado (ali por filtro na tela; aqui, direto na
-- função, já que é a única gente usa).
--
-- BACKFILL: recalcula liquido_produtor de todos os pedidos já existentes
-- (sz_financials_refresh é idempotente — INSERT...ON CONFLICT DO UPDATE).
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

  -- taxas configuráveis de senderzz_options
  SELECT value INTO v_opt_val FROM senderzz_options
  WHERE name = 'sz_producer_transaction_fee_pct' LIMIT 1;
  v_fee_prod := COALESCE(NULLIF(TRIM(v_opt_val),'')::NUMERIC / 100, 0.0499);

  SELECT value INTO v_opt_val FROM senderzz_options
  WHERE name = 'sz_affiliate_transaction_fee_pct' LIMIT 1;
  v_fee_afil := COALESCE(NULLIF(TRIM(v_opt_val),'')::NUMERIC / 100, 0.0499);

  -- motoboy
  SELECT COALESCE(mp.status,''), COALESCE(mp.valor_taxa,0), COALESCE(mp.valor_taxa_frustrado,0)
  INTO v_status_mb, v_taxa_entrega, v_taxa_frustrado
  FROM sz_motoboy_pedidos mp
  WHERE mp.wc_order_id = COALESCE(
    (SELECT wp_order_id FROM sz_orders WHERE id = p_order_id), p_order_id)
  ORDER BY mp.id DESC LIMIT 1;

  v_status_mb := COALESCE(v_status_mb, '');

  -- cálculo
  v_afil_bruta     := ROUND(v_afil_net / (1 - v_fee_afil), 2);
  v_taxa_plat_prod := ROUND(v_total    * v_fee_prod, 2);
  v_taxa_plat_afil := ROUND(v_afil_bruta * v_fee_afil, 2);
  -- AUDIT-2026-07-11: removido gate "só quando entregue" (divergência da golden
  -- 478) — comissão produtor agora é LIVE igual à do afiliado, exceto quando o
  -- motoboy já foi cancelado (pedido não vai ser entregue).
  v_liquido        := CASE WHEN v_status_mb IN ('cancelled','cancelado')
    THEN 0
    ELSE GREATEST(v_total - v_afil_bruta - v_taxa_plat_prod - v_taxa_entrega, 0)
  END;

  -- wallet
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
    total_pedido              = EXCLUDED.total_pedido,
    frete_comprador           = EXCLUDED.frete_comprador,
    comissao_afiliado_net    = EXCLUDED.comissao_afiliado_net,
    comissao_afiliado_bruta  = EXCLUDED.comissao_afiliado_bruta,
    fee_produtor_pct         = EXCLUDED.fee_produtor_pct,
    fee_afiliado_pct         = EXCLUDED.fee_afiliado_pct,
    taxa_plataforma_produtor = EXCLUDED.taxa_plataforma_produtor,
    taxa_plataforma_afiliado = EXCLUDED.taxa_plataforma_afiliado,
    taxa_entrega             = EXCLUDED.taxa_entrega,
    taxa_frustrado           = EXCLUDED.taxa_frustrado,
    liquido_produtor         = EXCLUDED.liquido_produtor,
    wallet_tx_id              = EXCLUDED.wallet_tx_id,
    wallet_creditado          = EXCLUDED.wallet_creditado,
    wallet_at                 = EXCLUDED.wallet_at,
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
