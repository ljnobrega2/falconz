<?php
/**
 * Senderzz — Captura de UTM e integração UTMify
 * Captura parâmetros UTM da URL, armazena na sessão WooCommerce e salva no pedido.
 * Expõe endpoint REST para webhook do UTMify.
 */
defined( 'ABSPATH' ) || exit;

if ( ! function_exists( 'sz_utm_capture_session' ) ) :

// Captura UTMs da URL → sessão WC
add_action( 'wp_loaded', 'sz_utm_capture_session', 5 );
function sz_utm_capture_session(): void {
    if ( is_admin() ) return;
    $utm_keys = [ 'utm_source', 'utm_medium', 'utm_campaign', 'utm_content', 'utm_term' ];
    $found = false;
    $utm   = [];
    foreach ( $utm_keys as $k ) {
        // phpcs:ignore WordPress.Security.NonceVerification.Recommended
        $v = sanitize_text_field( $_GET[ $k ] ?? '' );
        if ( $v !== '' ) {
            $utm[ $k ] = $v;
            $found     = true;
        }
    }
    if ( ! $found ) return;
    // WC session pode não estar disponível imediatamente; tenta duas estratégias
    if ( function_exists( 'WC' ) && WC()->session ) {
        WC()->session->set( 'sz_utm', $utm );
    } else {
        // Fallback: cookie 30 dias
        $expire = time() + 30 * DAY_IN_SECONDS;
        foreach ( $utm as $k => $v ) {
            setcookie( 'sz_' . $k, $v, $expire, COOKIEPATH, COOKIE_DOMAIN, is_ssl(), true );
        }
    }
}

// Salva UTM no pedido WC ao finalizar checkout
add_action( 'woocommerce_checkout_order_processed', 'sz_utm_save_to_order', 10, 1 );
function sz_utm_save_to_order( int $order_id ): void {
    $utm = [];

    // Tenta sessão WC primeiro
    if ( function_exists( 'WC' ) && WC()->session ) {
        $utm = (array) ( WC()->session->get( 'sz_utm' ) ?: [] );
    }

    // Fallback: cookies
    if ( empty( $utm ) ) {
        foreach ( [ 'utm_source', 'utm_medium', 'utm_campaign', 'utm_content', 'utm_term' ] as $k ) {
            $v = sanitize_text_field( $_COOKIE[ 'sz_' . $k ] ?? '' );
            if ( $v !== '' ) $utm[ $k ] = $v;
        }
    }

    if ( empty( $utm ) ) return;

    $order = wc_get_order( $order_id );
    if ( ! $order ) return;

    foreach ( $utm as $k => $v ) {
        if ( in_array( $k, [ 'utm_source', 'utm_medium', 'utm_campaign', 'utm_content', 'utm_term' ], true ) ) {
            $order->update_meta_data( '_sz_' . $k, sanitize_text_field( $v ) );
        }
    }
    $order->save();
}

// REST: webhook UTMify → atualiza meta do pedido
add_action( 'rest_api_init', 'sz_utm_register_routes' );
function sz_utm_register_routes(): void {
    register_rest_route( 'senderzz/v1', '/utmify/webhook', [
        'methods'             => 'POST',
        'callback'            => 'sz_utm_utmify_webhook',
        'permission_callback' => '__return_true',
    ] );
}

function sz_utm_utmify_webhook( WP_REST_Request $req ): WP_REST_Response {
    $body = $req->get_json_params();
    if ( empty( $body ) ) {
        $body = $req->get_body_params();
    }

    // UTMify envia order_id ou external_id com o ID do pedido WC
    $order_id = absint( $body['order_id'] ?? $body['external_id'] ?? $body['orderId'] ?? 0 );
    if ( ! $order_id ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'order_id obrigatório.' ], 422 );
    }

    $order = wc_get_order( $order_id );
    if ( ! $order ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Pedido não encontrado.' ], 404 );
    }

    $utm_map = [
        'utm_source'   => '_sz_utm_source',
        'utm_medium'   => '_sz_utm_medium',
        'utm_campaign' => '_sz_utm_campaign',
        'utm_content'  => '_sz_utm_content',
        'utm_term'     => '_sz_utm_term',
        'src'          => '_sz_utm_source',   // alias UTMify
        'sck'          => '_sz_utm_content',  // alias UTMify src click id
    ];

    $saved = [];
    foreach ( $utm_map as $field => $meta_key ) {
        $val = sanitize_text_field( (string) ( $body[ $field ] ?? '' ) );
        if ( $val !== '' ) {
            $order->update_meta_data( $meta_key, $val );
            $saved[ $meta_key ] = $val;
        }
    }

    if ( ! empty( $saved ) ) {
        $order->update_meta_data( '_sz_utmify_raw', wp_json_encode( $body ) );
        $order->save();
    }

    do_action( 'sz_utmify_webhook_received', $order_id, $body );

    return new WP_REST_Response( [ 'ok' => true, 'saved' => count( $saved ) ] );
}

endif;
