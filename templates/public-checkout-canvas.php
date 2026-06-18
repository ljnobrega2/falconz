<?php
/**
 * Template limpo para checkout público Senderzz/FunnelKit.
 *
 * Não chama get_header()/get_footer() para impedir que o tema imprima:
 * menu, título do post, autor, "Mais posts", rodapé e containers externos.
 */
defined( 'ABSPATH' ) || exit;

if ( function_exists( 'senderzz_sz_get_data' ) ) {
    senderzz_sz_get_data();
}

?><!doctype html>
<html <?php language_attributes(); ?> class="senderzz-checkout-canvas senderzz-public-checkout senderzz-standalone-checkout">
<head>
    <meta charset="<?php bloginfo( 'charset' ); ?>">
    <meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
    <meta name="robots" content="noindex,nofollow">
    <?php wp_head(); ?>
    <style id="senderzz-standalone-checkout-v402">
        html.senderzz-standalone-checkout,
        html.senderzz-standalone-checkout body{
            margin:0!important;
            padding:0!important;
            width:100%!important;
            min-width:0!important;
            max-width:none!important;
            min-height:100%!important;
            background:#f8fafc!important;
            overflow-x:hidden!important;
        }
        body.senderzz-standalone-checkout{
            margin:0!important;
            padding:0!important;
            width:100%!important;
            min-width:0!important;
            max-width:none!important;
            min-height:100vh!important;
            background:#f8fafc!important;
        }
        .senderzz-public-checkout-root{
            width:100%!important;
            min-width:0!important;
            max-width:none!important;
            min-height:100vh!important;
            margin:0!important;
            padding:0!important;
            background:#f8fafc!important;
            border:0!important;
            box-shadow:none!important;
            overflow-x:hidden!important;
        }
        .senderzz-public-checkout-content{
            width:100%!important;
            min-width:0!important;
            max-width:none!important;
            margin:0!important;
            padding:22px 0 52px!important;
            background:#f8fafc!important;
            border:0!important;
            box-shadow:none!important;
            box-sizing:border-box!important;
        }
        .senderzz-public-checkout-content > *{
            margin-top:0!important;
        }
        .senderzz-public-checkout-content .wfacp_main_form,
        .senderzz-public-checkout-content form.checkout,
        .senderzz-public-checkout-content .woocommerce-checkout{
            position:relative!important;
            z-index:2!important;
        }
        .senderzz-checkout-top-layout,
        .senderzz-first-step-offer-slot,
        .senderzz-external-steps-slot{
            width:100%!important;
            max-width:980px!important;
            margin-left:auto!important;
            margin-right:auto!important;
            box-sizing:border-box!important;
        }
        .senderzz-checkout-top-layout{
            padding:0 16px!important;
            margin-bottom:14px!important;
        }
        .senderzz-first-step-offer-slot{
            padding:0!important;
            margin-bottom:14px!important;
        }
        .senderzz-first-step-offer-slot #sz-oferta-bar,
        .senderzz-first-step-offer-slot .sz-mb-offer-strip{
            margin:0!important;
            width:100%!important;
            max-width:none!important;
        }
        .senderzz-external-steps-slot{
            padding:0!important;
            margin-bottom:14px!important;
        }
        .senderzz-external-steps-slot .senderzz-external-steps,
        .senderzz-external-steps-slot .wfacp_steps_wrap,
        .senderzz-external-steps-slot .wfacp-payment-tab-wrapper{
            width:100%!important;
            max-width:none!important;
            margin:0!important;
            padding:0!important;
            background:transparent!important;
            border:0!important;
            box-shadow:none!important;
        }
        body.senderzz-checkout-step-two .senderzz-first-step-offer-slot,
        body.sz-mb-final-step .senderzz-first-step-offer-slot{
            display:none!important;
        }
        @media (max-width:780px){
            .senderzz-public-checkout-content{padding:14px 0 36px!important;}
        }
    </style>
</head>
<body <?php body_class( 'senderzz-checkout-canvas senderzz-public-checkout senderzz-standalone-checkout' ); ?>>
<?php
if ( function_exists( 'wp_body_open' ) ) {
    wp_body_open();
}
?>
<main id="senderzz-public-checkout-root" class="senderzz-public-checkout-root" role="main">
    <div class="senderzz-public-checkout-content">
        <?php
        if ( function_exists( 'sz_mb_current_checkout_is_motoboy' ) && sz_mb_current_checkout_is_motoboy() && function_exists( 'sz_mb_render_checkout_offer_strip' ) ) {
            $offer_data = function_exists( 'sz_mb_checkout_offer_display_data' ) ? sz_mb_checkout_offer_display_data() : [];
            echo '<div class="senderzz-first-step-offer-slot">';
            echo sz_mb_render_checkout_offer_strip( is_array( $offer_data ) ? $offer_data : [], 'first' );
            echo '</div>';
        }

        $rendered = false;

        if ( have_posts() ) {
            while ( have_posts() ) {
                the_post();
                the_content();
                $rendered = true;
            }
            wp_reset_postdata();
        }

        if ( ! $rendered && function_exists( 'senderzz_sz_get_data' ) ) {
            $data    = senderzz_sz_get_data();
            $post_id       = absint( $data['post_id'] ?? 0 );
            $checkout_post = $post_id ? get_post( $post_id ) : null;

            if ( $checkout_post ) {
                $GLOBALS['post'] = $checkout_post;
                setup_postdata( $checkout_post );
                echo apply_filters( 'the_content', $checkout_post->post_content );
                wp_reset_postdata();
                $rendered = true;
            }
        }

        if ( ! $rendered ) {
            echo do_shortcode( '[woocommerce_checkout]' );
        }
        ?>
    </div>
</main>
<?php wp_footer(); ?>
</body>
</html>
