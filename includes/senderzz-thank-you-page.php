<?php
/**
 * Senderzz — Página de Obrigado white-label para WooCommerce/FunnelKit.
 * Base v10 estável + UX Motoboy/Expedição isolada.
 */
if ( ! defined( 'ABSPATH' ) ) { exit; }


// Status Motoboy: Agendado
add_action( 'init', function(): void {
    if ( ! function_exists( 'register_post_status' ) ) { return; }
    $senderzz_statuses = [
        'wc-agendado' => 'Agendado',
        'wc-reagendado' => 'Reagendado',
        'wc-completo' => 'Completo',
        'wc-entregue' => 'Entregue',
    ];
    foreach ( $senderzz_statuses as $status_key => $status_label ) {
        register_post_status( $status_key, [
            'label'                     => $status_label,
            'public'                    => true,
            'exclude_from_search'       => false,
            'show_in_admin_all_list'    => true,
            'show_in_admin_status_list' => true,
            'label_count'               => _n_noop( $status_label . ' <span class="count">(%s)</span>', $status_label . ' <span class="count">(%s)</span>' ),
        ] );
    }
}, 9 );
add_filter( 'wc_order_statuses', function( array $statuses ): array {
    $new = [];
    foreach ( $statuses as $key => $label ) {
        $new[$key] = $label;
        if ( $key === 'wc-on-hold' && ! isset( $new['wc-agendado'] ) ) { $new['wc-agendado'] = 'Agendado'; }
        if ( $key === 'wc-agendado' && ! isset( $new['wc-reagendado'] ) ) { $new['wc-reagendado'] = 'Reagendado'; }
        if ( $key === 'wc-processing' && ! isset( $new['wc-completo'] ) ) { $new['wc-completo'] = 'Completo'; }
        if ( $key === 'wc-enviado' && ! isset( $new['wc-entregue'] ) ) { $new['wc-entregue'] = 'Entregue'; }
    }
    if ( ! isset( $new['wc-agendado'] ) ) { $new['wc-agendado'] = 'Agendado'; }
    if ( ! isset( $new['wc-reagendado'] ) ) { $new['wc-reagendado'] = 'Reagendado'; }
    if ( ! isset( $new['wc-completo'] ) ) { $new['wc-completo'] = 'Completo'; }
    if ( ! isset( $new['wc-entregue'] ) ) { $new['wc-entregue'] = 'Entregue'; }
    return $new;
}, 20 );

add_action( 'template_redirect', 'senderzz_render_thank_you_page', 999 );

function senderzz_render_thank_you_page(): void {
    if ( is_admin() || wp_doing_ajax() || ( defined( 'REST_REQUEST' ) && REST_REQUEST ) ) {
        return;
    }

    $uri = isset( $_SERVER['REQUEST_URI'] ) ? (string) wp_unslash( $_SERVER['REQUEST_URI'] ) : '';
    $is_senderzz_thankyou = false;

    if ( function_exists( 'is_order_received_page' ) && is_order_received_page() ) {
        $is_senderzz_thankyou = true;
    }

    if ( false !== strpos( $uri, '/order-confirmed/' ) || false !== strpos( $uri, '/checkout/order-received/' ) ) {
        $is_senderzz_thankyou = true;
    }

    if ( ! $is_senderzz_thankyou || ! function_exists( 'wc_get_order' ) ) {
        return;
    }

    $order_id = absint( $_GET['order_id'] ?? $_GET['order'] ?? get_query_var( 'order-received' ) );
    $key      = sanitize_text_field( wp_unslash( $_GET['key'] ?? '' ) );

    if ( ! $order_id || ! $key ) {
        return;
    }

    $order = wc_get_order( $order_id );
    if ( ! $order || ! hash_equals( (string) $order->get_order_key(), (string) $key ) ) {
        return;
    }

    status_header( 200 );
    nocache_headers();

    $logo = senderzz_thank_you_logo_url();
    $name = trim( (string) $order->get_billing_first_name() . ' ' . (string) $order->get_billing_last_name() );
    if ( '' === $name ) { $name = 'Cliente'; }

    $order_number   = $order->get_order_number();
    $products_total_amount = senderzz_thank_you_products_total_amount( $order );
    $shipping_total_amount = senderzz_thank_you_shipping_total_amount( $order );
    $products_total = senderzz_thank_you_money( $products_total_amount, $order );
    $order_total    = senderzz_thank_you_money( (float) $order->get_total(), $order );
    $shipping_total = senderzz_thank_you_money( $shipping_total_amount, $order );
    $producer_total = senderzz_thank_you_money( $products_total_amount, $order );
    $offer_name     = senderzz_thank_you_offer_name( $order );
    $is_motoboy     = senderzz_thank_you_is_motoboy( $order );
    $delivery_type  = $is_motoboy ? 'Motoboy' : 'Expedição';
    $delivery_icon  = $is_motoboy ? '🏍️' : '🚚';
    $payment_label  = senderzz_thank_you_payment_label( $order, $is_motoboy );
    if ( ! $is_motoboy && 'Cash On Delivery' === $payment_label ) {
        $payment_label = 'Pagamento Online';
    }
    $tracking       = senderzz_thank_you_tracking_url( $order );
    $delivery_text  = senderzz_thank_you_delivery_text_for_mode( $order, $is_motoboy );
    $date_cards     = senderzz_thank_you_date_cards( $order );
    $fee_rows       = senderzz_thank_you_fee_rows( $order, $is_motoboy );
    // V96: na confirmação do cliente, o valor exibido é sempre o valor do link/produtos.
    // Taxas/frete não são explicitadas nesta tela.
    $display_order_total = $products_total;
    $display_total_amount = $products_total_amount;

    ?><!doctype html>
<html <?php language_attributes(); ?>>
<head>
    <meta charset="<?php bloginfo( 'charset' ); ?>">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>Pedido confirmado — Senderzz</title>
    <?php wp_head(); ?>
    <style>
        :root{--sz-orange:#ff6b00;--sz-orange-2:#ff8a00;--sz-bg:#fff;--sz-card:#fff;--sz-text:#191714;--sz-muted:#68625c;--sz-line:#ece7e1;--sz-soft:#fff3ea;--sz-shadow:0 22px 70px rgba(25,23,20,.09);--sz-radius:24px}
        *{box-sizing:border-box}html,body{margin:0;min-height:100%;font-family:var(--sz-font);background:#fff!important;color:var(--sz-text)}
        body:before{content:"";position:fixed;inset:0;background:radial-gradient(circle at 16% 0%,rgba(255,107,0,.07),transparent 32%),radial-gradient(circle at 94% 10%,rgba(255,138,0,.05),transparent 25%);pointer-events:none}
        .sz-thanks-wrap{position:relative;min-height:100vh;padding:26px 16px}.sz-shell{width:min(1280px,100%);margin:0 auto}.sz-panel{display:grid;grid-template-columns:.82fr 1.18fr;gap:18px;align-items:stretch}.sz-side,.sz-main,.sz-detail{background:#fff;border:1px solid var(--sz-line);border-radius:var(--sz-radius);box-shadow:var(--sz-shadow)}
        .sz-side{padding:30px;background:linear-gradient(160deg,#191714,#292018 72%,#ff6b00 165%);color:#fff;display:flex;flex-direction:column;justify-content:space-between;min-height:430px}.sz-logo{height:52px;max-width:215px;object-fit:contain;object-position:left center;filter:drop-shadow(0 8px 22px rgba(0,0,0,.16))}.sz-brand-fallback{font-weight:700;font-size:var(--sz-text-3xl);letter-spacing:-.02em;color:#fff}.sz-side h1{font-size:var(--sz-text-hero);line-height:1.02;letter-spacing:-.02em;margin:36px 0 12px}.sz-side p{margin:0;color:rgba(255,255,255,.76);font-size:var(--sz-text-lg);line-height:1.65}.sz-pill{display:inline-flex;align-items:center;gap:8px;width:max-content;margin-top:22px;padding:10px 13px;border-radius:999px;background:rgba(255,255,255,.11);border:1px solid rgba(255,255,255,.15);font-size:var(--sz-text-base);font-weight:700}.sz-dot{width:9px;height:9px;border-radius:50%;background:var(--sz-orange);box-shadow:0 0 0 5px rgba(255,107,0,.18)}
        .sz-main{padding:34px;display:grid;grid-template-columns:1.15fr .85fr;gap:26px}.sz-kicker{display:inline-flex;width:max-content;align-items:center;gap:8px;padding:8px 12px;border-radius:999px;background:var(--sz-soft);color:#bd4e00;font-size:var(--sz-text-meta);font-weight:700;text-transform:none;letter-spacing:.02em}.sz-title{font-size:var(--sz-text-hero);line-height:1.08;letter-spacing:-.02em;margin:18px 0 10px}.sz-sub{color:var(--sz-muted);font-size:var(--sz-text-lg);line-height:1.6;margin:0 0 22px}.sz-sub strong{color:var(--sz-orange)}
        .sz-info{display:grid;grid-template-columns:1fr 1fr;gap:12px;margin:8px 0 18px}.sz-box{position:relative;border:1px solid var(--sz-line);background:#fff;border-radius:18px;padding:16px;min-height:88px}.sz-box small{display:block;color:var(--sz-muted);font-size:var(--sz-text-meta);margin-bottom:7px}.sz-box strong{display:block;font-size:var(--sz-text-lg);line-height:1.3;color:var(--sz-orange)}.sz-box em{display:block;margin-top:4px;color:#191714;font-size:var(--sz-text-meta);font-style:normal;font-weight:700}.sz-callouts{display:grid;gap:12px}.sz-callout{display:flex;gap:12px;align-items:flex-start;border:1px solid var(--sz-line);border-radius:20px;background:#fff;padding:16px}.sz-ico{width:32px;height:32px;flex:0 0 32px;border-radius:11px;display:grid;place-items:center;background:linear-gradient(135deg,var(--sz-orange),var(--sz-orange-2));color:#fff;font-weight:700}.sz-callout b{display:block;font-size:var(--sz-text-md);margin-bottom:3px}.sz-callout span{display:block;color:var(--sz-muted);font-size:var(--sz-text-base);line-height:1.45}.sz-callout--warning{border-color:#ffd5bd;background:linear-gradient(135deg,#fff7f1,#fff)}.sz-callout--warning .sz-ico{background:linear-gradient(135deg,#ff8a00,#ff6b00)}
        .sz-date-panel{border:1px solid var(--sz-line);border-radius:20px;padding:18px;background:#fff}.sz-date-title{display:flex;align-items:center;gap:10px;font-weight:700;text-transform:none;font-size:var(--sz-text-base);letter-spacing:.02em;margin-bottom:14px}.sz-date-title span{font-size:var(--sz-text-xl);color:var(--sz-orange)}.sz-date-grid{display:grid;grid-template-columns:repeat(3,1fr);gap:10px}.sz-date-card{border:1.5px solid var(--sz-orange);border-radius:12px;min-height:96px;display:grid;place-items:center;text-align:center;background:#fff;color:#111}.sz-date-card.is-active{border-color:transparent;background:linear-gradient(135deg,var(--sz-orange),var(--sz-orange-2));color:#fff}.sz-date-card b{display:block;font-size:var(--sz-text-base);text-transform:none}.sz-date-card strong{display:block;font-size:var(--sz-text-3xl);line-height:1.1;margin:3px 0}.sz-date-card small{display:block;color:inherit;font-weight:700}.sz-date-highlight{margin-top:13px;border:1px solid #ffd5bd;border-radius:16px;background:linear-gradient(135deg,#fff7f1,#fff);padding:15px}.sz-date-highlight b{display:inline-flex;color:var(--sz-orange);font-size:var(--sz-text-meta);text-transform:none;background:#fff3ea;border-radius:999px;padding:4px 8px;margin-bottom:7px}.sz-date-highlight p{margin:0 0 5px;color:#433b34;font-size:var(--sz-text-base)}.sz-date-highlight strong{display:block;color:var(--sz-orange);font-size:var(--sz-text-xl);line-height:1.2}.sz-date-highlight small{display:block;margin-top:7px;color:var(--sz-muted);font-size:var(--sz-text-sm)}.sz-date-highlight--single{margin-top:0}.sz-prazo-panel{display:flex;flex-direction:column;justify-content:center}.sz-expedition-note{margin-top:14px;border:1px solid var(--sz-line);border-radius:14px;padding:14px;color:var(--sz-muted);font-size:var(--sz-text-base);line-height:1.45;background:#fff}
        .sz-tabs{display:flex;justify-content:center;gap:8px;margin:16px auto;max-width:620px}.sz-tab{flex:1;min-height:44px;border:1px solid var(--sz-line);border-radius:10px;background:#f8f8f8;color:#777;font-weight:700;text-transform:none;letter-spacing:.02em;padding:0 14px}.sz-tab.is-active{background:linear-gradient(135deg,var(--sz-orange),var(--sz-orange-2));color:#fff;border-color:transparent}.sz-detail{padding:22px}.sz-detail h2{display:flex;align-items:center;gap:10px;margin:0 0 16px;color:var(--sz-orange);font-size:var(--sz-text-lg)}.sz-resume{display:grid;grid-template-columns:repeat(4,1fr);border:1px solid var(--sz-line);border-radius:16px;margin-bottom:18px;overflow:hidden}.sz-resume-item{display:flex;align-items:center;gap:12px;padding:16px;border-right:1px solid var(--sz-line);min-height:86px}.sz-resume-item:last-child{border-right:0}.sz-resume-item .ri{font-size:var(--sz-text-3xl)}.sz-resume-item small{display:block;font-size:var(--sz-text-sm);color:#443f3a;margin-bottom:6px}.sz-resume-item strong{display:block;font-size:var(--sz-text-md);line-height:1.3}.sz-resume-item .accent{color:var(--sz-orange)}.sz-break-grid{display:grid;grid-template-columns:1.35fr .9fr;gap:18px}.sz-fees,.sz-totalbox{border:1px solid var(--sz-line);border-radius:16px;background:#fff;padding:16px}.sz-fees h3{font-size:var(--sz-text-lg);margin:0 0 10px}.sz-fee-row{display:flex;align-items:center;justify-content:space-between;gap:16px;border-bottom:1px solid var(--sz-line);padding:10px 0;font-size:var(--sz-text-md)}.sz-fee-row:last-child{border-bottom:0}.sz-fee-row.total{font-weight:700;color:var(--sz-orange);padding-top:14px}.sz-totalbox{display:flex;flex-direction:column;justify-content:center;gap:14px}.sz-tline{display:flex;justify-content:space-between;gap:14px}.sz-tline.final{border-top:1px solid var(--sz-line);padding-top:18px;margin-top:4px;color:var(--sz-orange);font-weight:700}.sz-tline.final strong{font-size:var(--sz-text-xl)}.sz-actions{display:flex;gap:12px;flex-wrap:wrap;margin-top:16px}.sz-btn{text-decoration:none;display:inline-flex;align-items:center;justify-content:center;min-height:46px;padding:0 20px;border-radius:999px;font-weight:700;font-size:var(--sz-text-md)}.sz-btn-secondary{background:#fff;color:var(--sz-text);border:1px solid var(--sz-line)}.sz-foot{text-align:center;color:var(--sz-muted);font-size:var(--sz-text-meta);line-height:1.5;margin:20px 0 0}

        /* Senderzz v16.6 — Thank you compacta: 1 viewport, sem redundância */
        .sz-compact-shell{max-width:1180px}.sz-confirm-card{display:grid;grid-template-columns:.72fr 1.28fr;gap:18px;min-height:calc(100vh - 52px);align-items:center}.sz-confirm-brand,.sz-confirm-main{border:1px solid var(--sz-line);border-radius:28px;box-shadow:var(--sz-shadow)}.sz-confirm-brand{min-height:560px;padding:34px;background:linear-gradient(160deg,#191714,#292018 72%,#ff6b00 165%);color:#fff;display:flex;flex-direction:column;justify-content:space-between}.sz-confirm-brand h1{font-size:var(--sz-text-hero);line-height:1;letter-spacing:-.02em;margin:28px 0 12px}.sz-confirm-brand p{max-width:390px;margin:0;color:rgba(255,255,255,.74);font-size:var(--sz-text-lg);line-height:1.6}.sz-kicker-dark{background:rgba(255,255,255,.12)!important;color:#fff!important;border:1px solid rgba(255,255,255,.12)}.sz-confirm-main{background:#fff;padding:28px;display:grid;gap:16px}.sz-confirm-head{display:flex;justify-content:space-between;align-items:flex-start;gap:18px}.sz-confirm-head h2{font-size:var(--sz-text-hero);line-height:1.03;letter-spacing:-.02em;margin:14px 0 8px}.sz-confirm-head p{margin:0;color:var(--sz-muted);font-size:var(--sz-text-lg);line-height:1.55}.sz-confirm-head strong{color:var(--sz-orange)}.sz-track-btn{text-decoration:none;display:inline-flex;align-items:center;justify-content:center;min-height:42px;padding:0 18px;border-radius:999px;background:linear-gradient(135deg,var(--sz-orange),var(--sz-orange-2));color:#fff;font-size:var(--sz-text-base);font-weight:700;box-shadow:0 12px 24px rgba(255,107,0,.16)}.sz-status-grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:10px}.sz-status-card{border:1px solid var(--sz-line);border-radius:16px;padding:14px;min-height:82px;background:#fff}.sz-status-card small,.sz-money-strip small,.sz-line-card small{display:block;color:#8b8580;font-size:var(--sz-text-sm);text-transform:none;letter-spacing:.02em;font-weight:700;margin-bottom:6px}.sz-status-card strong{display:block;color:#191714;font-size:var(--sz-text-md);line-height:1.25}.sz-status-card:nth-child(2) strong,.sz-status-card:nth-child(3) strong,.sz-status-card:nth-child(4) strong{color:var(--sz-orange)}.sz-money-strip{display:grid;grid-template-columns:1fr 1fr 1.15fr;gap:10px;border:1px solid var(--sz-line);border-radius:18px;padding:10px;background:#fff7f1}.sz-money-strip>div{border-radius:14px;background:#fff;padding:14px}.sz-money-strip strong{font-size:var(--sz-text-lg)}.sz-money-strip .final{background:linear-gradient(135deg,var(--sz-orange),var(--sz-orange-2));color:#fff}.sz-money-strip .final small{color:rgba(255,255,255,.82)}.sz-money-strip .final strong{display:block;font-size:var(--sz-text-3xl);line-height:1.1}.sz-compact-lines{display:grid;gap:10px}.sz-customer-grid{display:grid;grid-template-columns:1fr .7fr 1.6fr;gap:10px}.sz-line-card{border:1px solid var(--sz-line);border-radius:16px;padding:13px 14px;background:#fff}.sz-line-card strong{display:block;font-size:var(--sz-text-md);line-height:1.35;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.sz-fee-mini{border:1px solid var(--sz-line);border-radius:16px;padding:13px 14px;background:#fff}.sz-fee-mini small{display:block;color:#8b8580;font-size:var(--sz-text-sm);text-transform:none;letter-spacing:.02em;font-weight:700;margin-bottom:6px}.sz-fee-mini-row{display:flex;justify-content:space-between;gap:14px;font-size:var(--sz-text-base);line-height:1.35;padding:4px 0;border-top:1px solid #f3eee9}.sz-fee-mini-row:first-of-type{border-top:0}.sz-fee-mini-row strong{white-space:nowrap;color:var(--sz-orange)}.sz-final-note{display:flex;align-items:center;gap:10px;border:1px solid #ffd5bd;background:linear-gradient(135deg,#fff7f1,#fff);border-radius:16px;padding:14px;color:#5b4a40;font-size:var(--sz-text-base);line-height:1.45}.sz-final-note span{width:28px;height:28px;border-radius:10px;display:grid;place-items:center;flex:0 0 28px;background:linear-gradient(135deg,var(--sz-orange),var(--sz-orange-2));color:#fff;font-weight:700}.sz-menu-panels,.sz-tabs,.sz-delivery-menu{display:none!important}

        /* Senderzz v16.8 REAL — ordem dos cards e valores corretos */
        .sz-confirm-main{gap:14px}
        .sz-status-grid--top{grid-template-columns:1.35fr .8fr}
        .sz-status-grid--delivery{grid-template-columns:1fr 1fr}
        .sz-status-grid--top .sz-status-card:first-child strong{color:#191714}
        .sz-status-grid--top .sz-status-card:nth-child(2) strong,
        .sz-status-grid--delivery .sz-status-card:nth-child(2) strong{color:var(--sz-orange)}
        .sz-status-card{min-height:76px}
        .sz-customer-grid{grid-template-columns:1fr .72fr 2.35fr}
        .sz-line-card strong{font-size:var(--sz-text-md)}
        .sz-fee-mini--producer{background:linear-gradient(135deg,#fff7f1,#fff);border-color:#ffd5bd}
        .sz-fee-mini-row--producer{margin-top:4px;padding-top:10px;border-top:1px solid #ffd5bd;font-weight:700}
        .sz-fee-mini-row--producer strong{font-size:var(--sz-text-lg)}
        @media(max-width:980px){.sz-status-grid--top,.sz-status-grid--delivery,.sz-customer-grid{grid-template-columns:1fr}.sz-status-card{min-height:72px}}

        @media(max-width:980px){.sz-confirm-card{grid-template-columns:1fr;min-height:auto}.sz-confirm-brand{min-height:260px}.sz-status-grid{grid-template-columns:1fr 1fr}.sz-thanks-wrap{padding:14px}.sz-confirm-head{display:block}.sz-track-btn{margin-top:14px}.sz-money-strip,.sz-customer-grid{grid-template-columns:1fr}.sz-line-card strong{white-space:normal}.sz-confirm-brand h1,.sz-confirm-head h2{font-size:var(--sz-text-hero)}}

        .woocommerce,.wfacp_main_form,.wfty_wrap,.entry-title,.site-header,.site-footer{display:none!important}
        .sz-fee-mini,.sz-fees,.sz-totalbox,.sz-break-grid{display:none!important}.sz-status-grid--top-clean{grid-template-columns:1.35fr .8fr!important}

        .sz-delivery-menu{display:grid;grid-template-columns:1fr 1fr;gap:8px;margin:18px auto 10px;max-width:620px}.sz-menu-btn{min-height:46px;border:1px solid var(--sz-line);border-radius:12px;background:#f8f8f8;color:#777;font-weight:700;text-transform:none;letter-spacing:.02em;padding:0 16px;cursor:pointer;transition:.18s ease}.sz-menu-btn.is-active{background:linear-gradient(135deg,var(--sz-orange),var(--sz-orange-2));color:#fff;border-color:transparent;box-shadow:0 12px 24px rgba(255,107,0,.18)}.sz-menu-panels{display:grid;gap:16px}.sz-menu-panel{display:none}.sz-menu-panel.is-active{display:block}.sz-detail-head{display:flex;align-items:center;justify-content:space-between;gap:14px;margin-bottom:16px}.sz-detail h2{margin-bottom:0}.sz-mode-badge{display:inline-flex;align-items:center;gap:8px;border:1px solid #ffd5bd;background:#fff3ea;color:var(--sz-orange);border-radius:999px;padding:8px 12px;font-size:var(--sz-text-meta);font-weight:700;text-transform:none}.sz-list-title{font-size:var(--sz-text-lg);font-weight:700;margin:0 0 10px}.sz-fee-row span{display:inline-flex;align-items:center;gap:8px}.sz-panel-note{margin-top:12px;border:1px solid #ffd5bd;background:linear-gradient(135deg,#fff7f1,#fff);border-radius:16px;padding:13px 14px;color:#5b4a40;font-size:var(--sz-text-base);line-height:1.45}.sz-panel-note strong{color:var(--sz-orange)}


        .sz-accordions{display:grid;gap:10px;margin-top:14px}.sz-acc{border:1px solid var(--sz-line);border-radius:16px;background:#fff;overflow:hidden}.sz-acc summary{list-style:none;cursor:pointer;display:flex;align-items:center;justify-content:space-between;gap:12px;padding:14px 16px;font-weight:700;color:#191714}.sz-acc summary::-webkit-details-marker{display:none}.sz-acc summary:after{content:"⌄";font-size:var(--sz-text-lg);color:#5f5a55;transition:.18s ease}.sz-acc[open] summary:after{transform:rotate(180deg)}.sz-acc-body{border-top:1px solid var(--sz-line);padding:14px 16px}.sz-kv-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:12px}.sz-kv{min-width:0}.sz-kv small{display:block;color:#7a736c;font-size:var(--sz-text-sm);text-transform:none;letter-spacing:.02em;margin-bottom:5px}.sz-kv strong{display:block;font-size:var(--sz-text-md);line-height:1.35;color:#191714;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.sz-kv--wide{grid-column:1/-1}.sz-kv--wide strong{white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.sz-items-table{width:100%;border-collapse:collapse;font-size:var(--sz-text-base)}.sz-items-table th{color:#7a736c;text-transform:none;letter-spacing:.02em;font-size:var(--sz-text-sm);text-align:left;border-bottom:1px solid var(--sz-line);padding:9px 6px}.sz-items-table td{border-bottom:1px solid var(--sz-line);padding:11px 6px;vertical-align:middle}.sz-items-table tr:last-child td{border-bottom:0}.sz-items-table .num{text-align:right;white-space:nowrap}.sz-items-table .prod{font-weight:700}.sz-obs{color:#5f5a55;font-size:var(--sz-text-base);line-height:1.5}.sz-fees-inline{display:grid;gap:0}.sz-fees-inline .sz-fee-row:first-child{padding-top:0}.sz-fees-inline .sz-fee-row:last-child{padding-bottom:0}

        @media(max-width:980px){.sz-panel,.sz-main,.sz-break-grid,.sz-kv-grid{grid-template-columns:1fr}.sz-side{min-height:auto}.sz-resume{grid-template-columns:1fr 1fr}.sz-resume-item:nth-child(2){border-right:0}.sz-resume-item:nth-child(1),.sz-resume-item:nth-child(2){border-bottom:1px solid var(--sz-line)}}
        @media(max-width:650px){.sz-thanks-wrap{padding:14px 10px}.sz-delivery-menu{grid-template-columns:1fr}.sz-side,.sz-main,.sz-detail{padding:20px;border-radius:18px}.sz-side h1,.sz-title{font-size:var(--sz-text-3xl)}.sz-info,.sz-date-grid,.sz-resume{grid-template-columns:1fr}.sz-resume-item{border-right:0;border-bottom:1px solid var(--sz-line)}.sz-resume-item:last-child{border-bottom:0}.sz-tabs{position:sticky;top:8px;z-index:5}.sz-date-card{min-height:78px}}
    </style>
</head>
<body <?php body_class( 'senderzz-thank-you-page' ); ?>>
<?php wp_body_open(); ?>
<main class="sz-thanks-wrap">
    <section class="sz-shell sz-compact-shell" aria-label="Pedido confirmado">
        <article class="sz-confirm-card">
            <aside class="sz-confirm-brand">
                <?php if ( $logo ) : ?><img class="sz-logo" src="<?php echo esc_url( $logo ); ?>" alt="Senderzz"><?php else : ?><div class="sz-brand-fallback">SENDERZZ</div><?php endif; ?>
                <div>
                    <span class="sz-kicker sz-kicker-dark">Confirmação segura</span>
                    <h1>Pedido confirmado.</h1>
                    <p>Recebemos sua compra e vamos seguir com conferência, separação e envio.</p>
                </div>
            </aside>

            <div class="sz-confirm-main">
                <header class="sz-confirm-head">
                    <div>
                        <span class="sz-kicker">Obrigado pela compra</span>
                        <h2>Tudo certo, <?php echo esc_html( $name ); ?>.</h2>
                        <p>Pedido <strong>#<?php echo esc_html( $order_number ); ?></strong> confirmado no valor de <strong><?php echo wp_kses_post( $display_order_total ); ?></strong>.</p>
                    </div>
                    <?php if ( $tracking ) : ?><a class="sz-track-btn" href="<?php echo esc_url( $tracking ); ?>">Acompanhar</a><?php endif; ?>
                </header>

                <div class="sz-status-grid sz-status-grid--top sz-status-grid--top-clean">
                    <div class="sz-status-card"><small>Oferta</small><strong><?php echo esc_html( $offer_name ); ?></strong></div>
                    <div class="sz-status-card"><small>Valor total do pedido</small><strong><?php echo wp_kses_post( $display_order_total ); ?></strong></div>
                </div>

                <div class="sz-status-grid sz-status-grid--delivery">
                    <div class="sz-status-card"><small>Entrega</small><strong><?php echo esc_html( $delivery_icon . ' ' . $delivery_type ); ?></strong></div>
                    <div class="sz-status-card"><small><?php echo esc_html( $is_motoboy ? 'Data' : 'Prazo' ); ?></small><strong><?php echo esc_html( $delivery_text ); ?></strong></div>
                </div>

                <div class="sz-compact-lines">
                    <div class="sz-customer-grid">
                        <div class="sz-line-card"><small>Nome</small><strong><?php echo esc_html( senderzz_thank_you_customer_name( $order ) ); ?></strong></div>
                        <div class="sz-line-card"><small>Telefone</small><strong><?php echo esc_html( senderzz_thank_you_customer_phone( $order ) ); ?></strong></div>
                        <div class="sz-line-card"><small>Endereço</small><strong><?php echo esc_html( senderzz_thank_you_single_line_address( $order ) ); ?></strong></div>
                    </div>
                </div>

                <div class="sz-final-note">
                    <span>✓</span>
                    <?php echo esc_html( $is_motoboy ? 'Guarde esta página. A entrega seguirá pela data escolhida no checkout.' : 'Você receberá as atualizações assim que o pedido avançar para separação e postagem.' ); ?>
                </div>
            </div>
        </article>
    </section>
</main>
<div style="display:none" aria-hidden="true">
    <?php
    /** Mantém compatibilidade com pixels, integrações e eventos do Woo/FunnelKit. */
    do_action( 'woocommerce_thankyou_' . $order->get_payment_method(), $order->get_id() );
    do_action( 'woocommerce_thankyou', $order->get_id() );
    ?>
</div>
<?php wp_footer(); ?>
</body>
</html><?php
    exit;
}

function senderzz_thank_you_logo_url(): string {
    $base_path = defined( 'TPC_PATH' ) ? TPC_PATH : dirname( __DIR__ ) . '/';
    $base_url  = defined( 'TPC_URL' ) ? TPC_URL : plugin_dir_url( dirname( __DIR__ ) . '/senderzz-logistics.php' );

    $candidates = [
        'assets/images/senderzz-logo.png',
        'assets/img/senderzz-logo.png',
        'assets/images/senderzz-logo-1024.png',
        'assets/images/senderzz-logo-512.png',
    ];

    foreach ( $candidates as $rel ) {
        if ( file_exists( trailingslashit( $base_path ) . $rel ) ) {
            return trailingslashit( $base_url ) . $rel;
        }
    }

    return '';
}

function senderzz_thank_you_money( float $amount, WC_Order $order ): string {
    $currency = strtoupper( (string) $order->get_currency() );
    if ( 'BRL' === $currency || '' === $currency ) {
        return 'R$ ' . number_format( $amount, 2, ',', '.' );
    }
    if ( function_exists( 'wc_price' ) ) {
        return wc_price( $amount, [ 'currency' => $order->get_currency() ] );
    }
    return number_format( $amount, 2, ',', '.' );
}

function senderzz_thank_you_products_total_amount( WC_Order $order ): float {
    $amount = (float) $order->get_total() - senderzz_thank_you_shipping_total_amount( $order );
    return max( 0, $amount );
}

function senderzz_thank_you_products_total( WC_Order $order ): string {
    return senderzz_thank_you_money( senderzz_thank_you_products_total_amount( $order ), $order );
}

function senderzz_thank_you_shipping_total_amount( WC_Order $order ): float {
    return (float) $order->get_shipping_total() + (float) $order->get_shipping_tax();
}

function senderzz_thank_you_is_motoboy( WC_Order $order ): bool {
    foreach ( $order->get_items( 'shipping' ) as $item ) {
        $method_id = strtolower( (string) $item->get_method_id() );
        $name      = strtolower( (string) $item->get_name() );
        if ( false !== strpos( $method_id, 'motoboy' ) || false !== strpos( $method_id, 'sz_motoboy' ) || false !== strpos( $name, 'motoboy' ) ) {
            return true;
        }
    }
    $delivery_mode = strtolower( (string) $order->get_meta( '_sz_delivery_mode' ) . ' ' . (string) $order->get_meta( '_senderzz_delivery_mode' ) );
    return false !== strpos( $delivery_mode, 'motoboy' );
}

function senderzz_thank_you_payment_label( WC_Order $order, bool $is_motoboy ): string {
    $raw = trim( (string) $order->get_payment_method_title() );
    $haystack = strtolower( $raw . ' ' . (string) $order->get_payment_method() );
    if ( $is_motoboy || preg_match( '/pay after|cash on delivery|cod|entrega|ap[oó]s/', $haystack ) ) {
        return 'Cash On Delivery';
    }
    return $raw ?: 'Pagamento confirmado';
}

function senderzz_thank_you_shipping_name( WC_Order $order ): string {
    $method = trim( (string) $order->get_shipping_method() );
    return $method ?: 'Expedição Senderzz';
}


function senderzz_thank_you_delivery_text_for_mode( WC_Order $order, bool $is_motoboy ): string {
    if ( $is_motoboy ) {
        return senderzz_thank_you_delivery_text( $order );
    }

    return senderzz_thank_you_delivery_estimate_text( $order );
}

function senderzz_thank_you_delivery_estimate_text( WC_Order $order ): string {
    $delivery_time = '';
    $method_name   = '';

    foreach ( $order->get_items( 'shipping' ) as $item ) {
        $method_name = $method_name ?: (string) $item->get_name();
        foreach ( [ 'melhorenvio_delivery_time', '_melhorenvio_delivery_time', 'delivery_time', '_delivery_time', 'prazo', 'Prazo' ] as $key ) {
            $value = trim( (string) $item->get_meta( $key ) );
            if ( '' !== $value ) {
                $delivery_time = $value;
                break 2;
            }
        }
    }

    if ( '' === $delivery_time ) {
        $delivery_time = trim( (string) $order->get_meta( '_senderzz_delivery_time' ) );
    }

    if ( '' !== $delivery_time ) {
        $delivery_time = preg_replace( '/\s+/', ' ', $delivery_time );
        if ( preg_match( '/^\d+$/', $delivery_time ) ) {
            return 'até ' . $delivery_time . ' dias úteis';
        }
        if ( false === stripos( $delivery_time, 'dia' ) ) {
            return 'até ' . $delivery_time . ' dias úteis';
        }
        return preg_replace( '/^de\s+/i', '', $delivery_time );
    }

    if ( '' !== $method_name ) {
        return 'conforme o método escolhido: ' . $method_name;
    }

    return 'conforme o método de entrega escolhido';
}

function senderzz_thank_you_delivery_text( WC_Order $order ): string {
    $date = senderzz_thank_you_delivery_date_text( $order );
    if ( '' !== $date ) {
        return $date;
    }

    $delivery_time = '';
    $method_name   = '';

    foreach ( $order->get_items( 'shipping' ) as $item ) {
        $method_name = $method_name ?: (string) $item->get_name();

        foreach ( [ 'melhorenvio_delivery_time', '_melhorenvio_delivery_time', 'delivery_time', '_delivery_time', 'prazo', 'Prazo' ] as $key ) {
            $value = trim( (string) $item->get_meta( $key ) );
            if ( '' !== $value ) {
                $delivery_time = $value;
                break 2;
            }
        }
    }

    if ( '' === $delivery_time ) {
        $delivery_time = trim( (string) $order->get_meta( '_senderzz_delivery_time' ) );
    }

    if ( '' !== $delivery_time ) {
        $delivery_time = preg_replace( '/\s+/', ' ', $delivery_time );
        if ( preg_match( '/^\d+$/', $delivery_time ) ) {
            return 'de até ' . $delivery_time . ' dias úteis';
        }
        if ( false === stripos( $delivery_time, 'dia' ) ) {
            return 'de até ' . $delivery_time . ' dias úteis';
        }
        return $delivery_time;
    }

    if ( '' !== $method_name ) {
        return 'conforme o método de entrega escolhido: ' . $method_name;
    }

    return 'conforme o método de entrega escolhido';
}

function senderzz_thank_you_delivery_date_text( WC_Order $order ): string {
    $raw = '';
    foreach ( [ '_sz_delivery_date', '_senderzz_delivery_date', 'delivery_date', '_delivery_date' ] as $meta_key ) {
        $raw = trim( (string) $order->get_meta( $meta_key ) );
        if ( '' !== $raw ) { break; }
    }
    if ( '' === $raw ) { return ''; }

    $ts = strtotime( $raw );
    if ( ! $ts ) { return $raw; }

    $dias  = [ 'Sunday'=>'Domingo', 'Monday'=>'Segunda-feira', 'Tuesday'=>'Terça-feira', 'Wednesday'=>'Quarta-feira', 'Thursday'=>'Quinta-feira', 'Friday'=>'Sexta-feira', 'Saturday'=>'Sábado' ];
    $meses = [ 1=>'Janeiro', 2=>'Fevereiro', 3=>'Março', 4=>'Abril', 5=>'Maio', 6=>'Junho', 7=>'Julho', 8=>'Agosto', 9=>'Setembro', 10=>'Outubro', 11=>'Novembro', 12=>'Dezembro' ];

    return ( $dias[ date( 'l', $ts ) ] ?? date_i18n( 'l', $ts ) ) . ', ' . date_i18n( 'd', $ts ) . ' de ' . ( $meses[(int) date_i18n( 'n', $ts )] ?? date_i18n( 'F', $ts ) );
}

function senderzz_thank_you_date_cards( WC_Order $order ): array {
    $raw = '';
    foreach ( [ '_sz_delivery_date', '_senderzz_delivery_date', 'delivery_date', '_delivery_date' ] as $meta_key ) {
        $raw = trim( (string) $order->get_meta( $meta_key ) );
        if ( '' !== $raw ) { break; }
    }
    $base = $raw ? strtotime( $raw ) : strtotime( '+1 day' );
    if ( ! $base ) { $base = strtotime( '+1 day' ); }

    $dias = [ 'Dom','Seg','Ter','Qua','Qui','Sex','Sáb' ];
    $meses = [ 1=>'Jan',2=>'Fev',3=>'Mar',4=>'Abr',5=>'Mai',6=>'Jun',7=>'Jul',8=>'Ago',9=>'Set',10=>'Out',11=>'Nov',12=>'Dez' ];
    $cards = [];
    $ts = $base;
    while ( count( $cards ) < 3 ) {
        if ( (int) date( 'w', $ts ) !== 0 ) {
            $cards[] = [
                'dow' => $dias[(int) date( 'w', $ts )],
                'day' => date_i18n( 'd', $ts ),
                'mon' => $meses[(int) date_i18n( 'n', $ts )] ?? date_i18n( 'M', $ts ),
            ];
        }
        $ts = strtotime( '+1 day', $ts );
    }
    return $cards;
}

function senderzz_thank_you_fee_rows( WC_Order $order, bool $is_motoboy ): array {
    $rows = [];

    if ( $is_motoboy ) {
        $map = [
            '_sz_taxa_entrega'  => '🏍️ Taxa de entrega',
            '_sz_taxa_manuseio' => '📦 Taxa de manuseio',
            '_sz_taxa_servico'  => '🛡️ Taxa de serviço',
            '_sz_taxa_adicional'=> '➕ Taxa adicional',
        ];
        foreach ( $map as $key => $label ) {
            $amount = (float) $order->get_meta( $key );
            if ( $amount > 0 ) {
                $rows[] = [ 'label' => $label, 'amount' => $amount ];
            }
        }
    }

    foreach ( $order->get_items( 'fee' ) as $fee ) {
        $amount = 0.0;
        if ( is_object( $fee ) && method_exists( $fee, 'get_total' ) ) {
            $amount = (float) $fee->get_total();
            if ( method_exists( $fee, 'get_total_tax' ) ) {
                $amount += (float) $fee->get_total_tax();
            }
        }
        if ( $amount > 0 ) {
            $rows[] = [ 'label' => '➕ ' . (string) $fee->get_name(), 'amount' => $amount ];
        }
    }

    if ( empty( $rows ) ) {
        $label = $is_motoboy ? '🏍️ Entrega Motoboy Senderzz' : '🚚 ' . senderzz_thank_you_shipping_name( $order );
        $rows[] = [ 'label' => $label, 'amount' => senderzz_thank_you_shipping_total_amount( $order ) ];
    }

    return $rows;
}


function senderzz_thank_you_render_delivery_panel( WC_Order $order, bool $panel_motoboy, bool $active, string $delivery_text, string $order_total, string $products_total ): string {
    $type          = $panel_motoboy ? 'Motoboy' : 'Expedição';
    $slug          = $panel_motoboy ? 'motoboy' : 'expedicao';
    $icon          = $panel_motoboy ? '🏍️' : '🚚';
    $shipping_name = $panel_motoboy ? 'Motoboy Senderzz' : senderzz_thank_you_shipping_name( $order );
    $payment_label = $panel_motoboy ? 'Cash On Delivery' : senderzz_thank_you_payment_label( $order, false );
    if ( ! $panel_motoboy && 'Cash On Delivery' === $payment_label ) {
        $payment_label = 'Pagamento Online';
    }
    $shipping_total = senderzz_thank_you_money( senderzz_thank_you_shipping_total_amount( $order ), $order );
    $fee_rows       = senderzz_thank_you_fee_rows_for_panel( $order, $panel_motoboy );
    $tracking       = senderzz_thank_you_tracking_url( $order );

    ob_start();
    ?>
    <article class="sz-detail sz-menu-panel <?php echo $active ? 'is-active' : ''; ?>" data-sz-panel="<?php echo esc_attr( $slug ); ?>">
        <div class="sz-detail-head">
            <h2><?php echo esc_html( $icon ); ?> Resumo do pedido — <?php echo esc_html( strtoupper( $type ) ); ?></h2>
            <span class="sz-mode-badge"><?php echo esc_html( $icon . ' ' . $type ); ?></span>
        </div>
        <div class="sz-resume">
            <div class="sz-resume-item"><span class="ri"><?php echo esc_html( $icon ); ?></span><div><small>Entrega via</small><strong><?php echo esc_html( $shipping_name ); ?></strong></div></div>
            <div class="sz-resume-item"><span class="ri">📅</span><div><small><?php echo esc_html( $panel_motoboy ? 'Data de entrega' : 'Prazo estimado' ); ?></small><strong class="accent"><?php echo esc_html( $delivery_text ); ?></strong></div></div>
            <div class="sz-resume-item"><span class="ri">💳</span><div><small>Pagamento</small><strong class="accent"><?php echo esc_html( $payment_label ); ?></strong><?php if ( $panel_motoboy ) : ?><small>Pagamento na entrega</small><?php else : ?><small>Cartão / PIX / checkout</small><?php endif; ?></div></div>
            <div class="sz-resume-item"><span class="ri">💰</span><div><small>Total do pedido</small><strong class="accent"><?php echo wp_kses_post( $order_total ); ?></strong></div></div>
        </div>

        <div class="sz-break-grid">
            <div class="sz-fees">
                <p class="sz-list-title">Detalhamento do frete (<?php echo esc_html( $type ); ?>)</p>
                <?php foreach ( $fee_rows as $fee ) : ?>
                    <div class="sz-fee-row"><span><?php echo esc_html( $fee['label'] ); ?></span><strong><?php echo wp_kses_post( senderzz_thank_you_money( (float) $fee['amount'], $order ) ); ?></strong></div>
                <?php endforeach; ?>
                <div class="sz-fee-row total"><span>Total do frete</span><strong><?php echo wp_kses_post( $shipping_total ); ?></strong></div>
            </div>
            <div class="sz-totalbox">
                <div class="sz-tline"><span>Subtotal dos produtos</span><strong><?php echo wp_kses_post( $products_total ); ?></strong></div>
                <div class="sz-tline"><span>Total do frete</span><strong><?php echo wp_kses_post( $shipping_total ); ?></strong></div>
                <div class="sz-tline final"><span>Total do pedido</span><strong><?php echo wp_kses_post( $order_total ); ?></strong></div>
                <?php if ( $tracking ) : ?><div class="sz-actions"><a class="sz-btn sz-btn-secondary" href="<?php echo esc_url( $tracking ); ?>">Acompanhar pedido</a></div><?php endif; ?>
            </div>
        </div>

        <div class="sz-accordions">
            <?php echo senderzz_thank_you_render_customer_accordion( $order ); ?>
            <?php echo senderzz_thank_you_render_items_accordion( $order ); ?>
            <?php echo senderzz_thank_you_render_fee_accordion( $order, $fee_rows, $shipping_total, $type ); ?>
            <?php echo senderzz_thank_you_render_notes_accordion( $order ); ?>
        </div>
        <?php if ( $panel_motoboy ) : ?>
            <div class="sz-panel-note"><strong>Atenção:</strong> é obrigatório que uma pessoa maior de idade esteja presente no momento do recebimento.</div>
        <?php endif; ?>
    </article>
    <?php
    return (string) ob_get_clean();
}


function senderzz_thank_you_clean_phone( string $phone ): string {
    $digits = preg_replace( '/\D+/', '', $phone );
    if ( '' === $digits ) { return '—'; }
    if ( 0 === strpos( $digits, '55' ) && strlen( $digits ) >= 12 ) { $digits = substr( $digits, 2 ); }
    if ( 0 === strpos( $digits, '0' ) && strlen( $digits ) > 10 ) { $digits = ltrim( $digits, '0' ); }
    if ( strlen( $digits ) === 9 ) { return substr( $digits, 0, 5 ) . '-' . substr( $digits, 5 ); }
    if ( strlen( $digits ) === 8 ) { return substr( $digits, 0, 4 ) . '-' . substr( $digits, 4 ); }
    if ( strlen( $digits ) === 10 || strlen( $digits ) === 11 ) {
        $ddd = substr( $digits, 0, 2 );
        $num = substr( $digits, 2 );
        return '(' . $ddd . ') ' . ( strlen( $num ) === 9 ? substr( $num, 0, 5 ) . '-' . substr( $num, 5 ) : substr( $num, 0, 4 ) . '-' . substr( $num, 4 ) );
    }
    return $digits;
}

function senderzz_thank_you_single_line_address( WC_Order $order ): string {
    $parts = [];
    $addr1 = trim( (string) ( $order->get_shipping_address_1() ?: $order->get_billing_address_1() ) );
    $num   = trim( (string) ( $order->get_meta( '_shipping_number' ) ?: $order->get_meta( '_billing_number' ) ?: $order->get_meta( 'shipping_number' ) ?: $order->get_meta( 'billing_number' ) ) );
    $addr2 = trim( (string) ( $order->get_shipping_address_2() ?: $order->get_billing_address_2() ) );
    $neigh = trim( (string) ( $order->get_meta( '_shipping_neighborhood' ) ?: $order->get_meta( '_billing_neighborhood' ) ?: $order->get_meta( 'shipping_neighborhood' ) ?: $order->get_meta( 'billing_neighborhood' ) ) );
    $city  = trim( (string) ( $order->get_shipping_city() ?: $order->get_billing_city() ) );
    $state = trim( (string) ( $order->get_shipping_state() ?: $order->get_billing_state() ) );
    $post  = trim( (string) ( $order->get_shipping_postcode() ?: $order->get_billing_postcode() ) );
    if ( $addr1 !== '' ) { $parts[] = $num !== '' && false === strpos( $addr1, $num ) ? $addr1 . ', ' . $num : $addr1; }
    if ( $addr2 !== '' ) { $parts[] = $addr2; }
    if ( $neigh !== '' ) { $parts[] = $neigh; }
    $city_state = trim( $city . ( $city && $state ? ' - ' : '' ) . $state );
    if ( $city_state !== '' ) { $parts[] = $city_state; }
    if ( $post !== '' ) { $parts[] = 'CEP ' . $post; }
    $line = preg_replace( '/\s+/', ' ', implode( ' · ', array_filter( $parts ) ) );
    return $line ?: '—';
}

function senderzz_thank_you_customer_document( WC_Order $order ): string {
    foreach ( [ '_billing_cpf', 'billing_cpf', '_billing_cnpj', 'billing_cnpj', 'cpf', 'cnpj' ] as $key ) {
        $value = trim( (string) $order->get_meta( $key ) );
        if ( '' !== $value ) { return $value; }
    }
    return '—';
}

function senderzz_thank_you_render_customer_accordion( WC_Order $order ): string {
    $name = trim( (string) $order->get_formatted_billing_full_name() );
    if ( '' === $name ) { $name = 'Cliente'; }
    $phone = senderzz_thank_you_clean_phone( (string) ( $order->get_billing_phone() ?: $order->get_shipping_phone() ) );
    $doc   = senderzz_thank_you_customer_document( $order );
    $addr  = senderzz_thank_you_single_line_address( $order );
    ob_start(); ?>
    <details class="sz-acc" open>
        <summary>👤 Informações do cliente</summary>
        <div class="sz-acc-body">
            <div class="sz-kv-grid">
                <div class="sz-kv"><small>Cliente</small><strong><?php echo esc_html( $name ); ?></strong></div>
                <div class="sz-kv"><small>Telefone</small><strong><?php echo esc_html( $phone ); ?></strong></div>
                <div class="sz-kv"><small>Documento</small><strong><?php echo esc_html( $doc ); ?></strong></div>
                <div class="sz-kv sz-kv--wide"><small>Endereço</small><strong title="<?php echo esc_attr( $addr ); ?>"><?php echo esc_html( $addr ); ?></strong></div>
            </div>
        </div>
    </details>
    <?php return (string) ob_get_clean();
}

function senderzz_thank_you_render_items_accordion( WC_Order $order ): string {
    ob_start(); ?>
    <details class="sz-acc">
        <summary>📦 Detalhes do pedido</summary>
        <div class="sz-acc-body">
            <table class="sz-items-table">
                <thead><tr><th>Produto</th><th class="num">Qtd.</th><th class="num">Total</th></tr></thead>
                <tbody>
                <?php foreach ( $order->get_items( 'line_item' ) as $item ) :
                    $product_name = function_exists( 'senderzz_clean_product_label' ) ? senderzz_clean_product_label( $item->get_name() ) : wp_strip_all_tags( (string) $item->get_name(), true );
                    $qty = is_object( $item ) && method_exists( $item, 'get_quantity' ) ? (int) $item->get_quantity() : 1;
                    $total = is_object( $item ) && method_exists( $item, 'get_total' ) ? (float) $item->get_total() : 0.0;
                    if ( is_object( $item ) && method_exists( $item, 'get_total_tax' ) ) { $total += (float) $item->get_total_tax(); }
                ?>
                    <tr><td class="prod"><?php echo esc_html( $product_name ); ?></td><td class="num"><?php echo esc_html( (string) $qty ); ?></td><td class="num"><?php echo wp_kses_post( senderzz_thank_you_money( $total, $order ) ); ?></td></tr>
                <?php endforeach; ?>
                </tbody>
            </table>
        </div>
    </details>
    <?php return (string) ob_get_clean();
}

function senderzz_thank_you_render_fee_accordion( WC_Order $order, array $fee_rows, string $shipping_total, string $type ): string {
    ob_start(); ?>
    <details class="sz-acc" open>
        <summary>💸 Taxas cobradas — <?php echo esc_html( $type ); ?></summary>
        <div class="sz-acc-body sz-fees-inline">
            <?php foreach ( $fee_rows as $fee ) : ?>
                <div class="sz-fee-row"><span><?php echo esc_html( $fee['label'] ?? '' ); ?></span><strong><?php echo wp_kses_post( senderzz_thank_you_money( (float) ( $fee['amount'] ?? 0 ), $order ) ); ?></strong></div>
            <?php endforeach; ?>
            <div class="sz-fee-row total"><span>Total do frete</span><strong><?php echo wp_kses_post( $shipping_total ); ?></strong></div>
        </div>
    </details>
    <?php return (string) ob_get_clean();
}

function senderzz_thank_you_render_notes_accordion( WC_Order $order ): string {
    $note = trim( (string) $order->get_customer_note() );
    if ( '' === $note ) { $note = 'Sem observações adicionais para este pedido.'; }
    ob_start(); ?>
    <details class="sz-acc">
        <summary>📝 Observações</summary>
        <div class="sz-acc-body"><div class="sz-obs"><?php echo esc_html( $note ); ?></div></div>
    </details>
    <?php return (string) ob_get_clean();
}

function senderzz_thank_you_fee_rows_for_panel( WC_Order $order, bool $panel_motoboy ): array {
    $shipping_total = senderzz_thank_you_shipping_total_amount( $order );

    if ( $panel_motoboy ) {
        $rows = [];
        $map = [
            '_sz_taxa_entrega'   => '🏍️ Taxa de entrega (Motoboy)',
            '_sz_taxa_combustivel' => '⛽ Taxa de combustível',
            '_sz_taxa_servico'   => '🛡️ Taxa de serviço',
            '_sz_taxa_rota'      => '🛣️ Pedágio / rota',
            '_sz_taxa_manuseio'  => '📦 Manuseio',
            '_sz_taxa_adicional' => '➕ Taxa adicional',
        ];
        foreach ( $map as $key => $label ) {
            $amount = (float) $order->get_meta( $key );
            if ( $amount > 0 ) { $rows[] = [ 'label' => $label, 'amount' => $amount ]; }
        }
        foreach ( $order->get_items( 'fee' ) as $fee ) {
            $amount = 0.0;
            if ( is_object( $fee ) && method_exists( $fee, 'get_total' ) ) {
                $amount = (float) $fee->get_total();
                if ( method_exists( $fee, 'get_total_tax' ) ) { $amount += (float) $fee->get_total_tax(); }
            }
            if ( $amount > 0 ) { $rows[] = [ 'label' => '➕ ' . (string) $fee->get_name(), 'amount' => $amount ]; }
        }
        if ( empty( $rows ) ) { $rows[] = [ 'label' => '🏍️ Entrega Motoboy Senderzz', 'amount' => $shipping_total ]; }
        return $rows;
    }

    $rows = [];
    $method_name = senderzz_thank_you_shipping_name( $order );
    $rows[] = [ 'label' => '🚚 Frete base — ' . $method_name, 'amount' => $shipping_total ];
    $expedition_meta = [
        '_sz_taxa_embalagem' => '📦 Taxa de embalagem',
        '_sz_taxa_seguro'    => '🛡️ Seguro',
        '_sz_taxa_despacho'  => '🏷️ Despacho / manuseio',
        '_sz_taxa_adicional_expedicao' => '➕ Taxa adicional',
    ];
    foreach ( $expedition_meta as $key => $label ) {
        $amount = (float) $order->get_meta( $key );
        if ( $amount > 0 ) { $rows[] = [ 'label' => $label, 'amount' => $amount ]; }
    }
    return $rows;
}


function senderzz_thank_you_items_summary( WC_Order $order ): string {
    $names = [];
    $count = 0;
    foreach ( $order->get_items( 'line_item' ) as $item ) {
        $qty = is_object( $item ) && method_exists( $item, 'get_quantity' ) ? max( 1, (int) $item->get_quantity() ) : 1;
        $count += $qty;
        $name = function_exists( 'senderzz_clean_product_label' ) ? senderzz_clean_product_label( $item->get_name() ) : wp_strip_all_tags( (string) $item->get_name(), true );
        if ( '' !== $name ) { $names[] = $name; }
    }
    $label = $count > 1 ? $count . ' itens' : '1 item';
    $names = array_slice( array_unique( $names ), 0, 3 );
    return $label . ( $names ? ' · ' . implode( ' + ', $names ) : '' );
}

function senderzz_thank_you_customer_summary( WC_Order $order ): string {
    $name = trim( (string) $order->get_formatted_billing_full_name() );
    if ( '' === $name ) { $name = 'Cliente'; }
    $phone = senderzz_thank_you_clean_phone( (string) ( $order->get_billing_phone() ?: $order->get_shipping_phone() ) );
    $addr  = senderzz_thank_you_single_line_address( $order );
    return preg_replace( '/\s+/', ' ', $name . ' · ' . $phone . ' · ' . $addr );
}


function senderzz_thank_you_offer_name( WC_Order $order ): string {
    $meta = trim( (string) $order->get_meta( '_senderzz_offer_name' ) );
    if ( '' !== $meta ) { return wp_strip_all_tags( $meta, true ); }

    foreach ( $order->get_items( 'line_item' ) as $item ) {
        if ( is_object( $item ) && method_exists( $item, 'get_meta' ) ) {
            $item_offer = trim( (string) $item->get_meta( '_senderzz_offer_name', true ) );
            if ( '' !== $item_offer ) { return wp_strip_all_tags( $item_offer, true ); }
        }
    }

    $summary = senderzz_thank_you_items_summary( $order );
    return '' !== trim( $summary ) ? $summary : 'Pedido Senderzz';
}

function senderzz_thank_you_customer_name( WC_Order $order ): string {
    $name = trim( (string) $order->get_formatted_billing_full_name() );
    return '' !== $name ? $name : 'Cliente';
}

function senderzz_thank_you_customer_phone( WC_Order $order ): string {
    return senderzz_thank_you_clean_phone( (string) ( $order->get_billing_phone() ?: $order->get_shipping_phone() ) );
}

function senderzz_thank_you_tracking_url( WC_Order $order ): string {
    $tracking = (string) $order->get_meta( '_melhor_envio_tracking' );
    if ( '' === $tracking ) { $tracking = (string) $order->get_meta( '_tracking_code' ); }
    if ( '' === $tracking ) { return ''; }
    return home_url( '/rastreio/' . rawurlencode( $tracking ) . '/' );
}


/**
 * Motoboy: pedido nasce como agendado quando o método de entrega é motoboy.
 * Mantém fallback seguro caso o status customizado ainda não exista no ambiente.
 */
add_action( 'woocommerce_checkout_order_processed', function( $order_id ): void {
    if ( ! function_exists( 'wc_get_order' ) ) { return; }
    $order = wc_get_order( $order_id );
    if ( ! $order instanceof WC_Order || ! senderzz_thank_you_is_motoboy( $order ) ) { return; }
    $order->update_meta_data( '_senderzz_delivery_mode', 'motoboy' );
    $order->update_meta_data( '_senderzz_motoboy_flow_status', 'agendado' );
    $order->save();
    $statuses = function_exists( 'wc_get_order_statuses' ) ? wc_get_order_statuses() : [];
    if ( isset( $statuses['wc-agendado'] ) && $order->get_status() !== 'agendado' ) {
        $order->update_status( 'agendado', 'Senderzz Motoboy: pedido enviado diretamente para Agendado.' );
    }
}, 40 );
