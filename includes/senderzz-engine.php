<?php
/**
 * Senderzz Engine
 * Liga Carteira + Melhor Envio sem remover funções existentes.
 */
defined( 'ABSPATH' ) || exit;
if ( defined( 'SENDERZZ_SENDERZZ_ENGINE_LOADED' ) ) return;
define( 'SENDERZZ_SENDERZZ_ENGINE_LOADED', true );

function senderzz_log( string $source, array $data = [] ): void {
    if ( function_exists( 'tpc_mask_sensitive_log_data' ) ) {
        $data = tpc_mask_sensitive_log_data( $data );
    }
    if ( function_exists( 'wc_get_logger' ) ) {
        wc_get_logger()->info( wp_json_encode( $data, JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES ), [ 'source' => 'senderzz-' . $source ] );
    }
}

function senderzz_order_shipping_snapshot( WC_Order $order ): array {
    $charged = (float) $order->get_shipping_total();
    $real = 0.0;

    // Motoboy: o cliente paga somente o valor do link no checkout, mas a taxa
    // cadastrada do produtor precisa alimentar painel/carteira como frete Senderzz.
    $is_motoboy_snapshot = (string) $order->get_meta( '_senderzz_delivery_mode', true ) === 'motoboy';
    if ( ! $is_motoboy_snapshot ) {
        foreach ( $order->get_items( 'shipping' ) as $_mb_item ) {
            if ( stripos( (string) $_mb_item->get_method_id(), 'sz_motoboy' ) !== false || stripos( (string) $_mb_item->get_name(), 'motoboy' ) !== false ) { $is_motoboy_snapshot = true; break; }
        }
    }
    if ( $is_motoboy_snapshot ) {
        $mb_fee = (float) $order->get_meta( '_sz_mb_taxa_total', true );
        if ( $mb_fee <= 0 ) {
            $mb_fee = (float) $order->get_meta( '_sz_mb_taxa_entrega', true ) + (float) $order->get_meta( '_sz_mb_taxa_manuseio', true ) + (float) $order->get_meta( '_sz_mb_taxa_adicional', true );
        }
        if ( $mb_fee > 0 ) {
            $charged = round( $mb_fee, 2 );
            $real = round( $mb_fee, 2 );
        }
    }
    $carrier = '';
    $service_id = '';
    $delivery_time = '';

    foreach ( $order->get_items( 'shipping' ) as $item ) {
        $carrier = $carrier ?: $item->get_name();
        $service_id = $service_id ?: (string) $item->get_meta( 'melhorenvio_method_id' );
        $delivery_time = $delivery_time ?: (string) $item->get_meta( 'melhorenvio_delivery_time' );
        $original = $item->get_meta( 'melhorenvio_original_cost' );
        if ( $original !== '' && $original !== null ) {
            $real += (float) $original;
        }
    }

    if ( $real <= 0 && $charged > 0 ) {
        // Descobre classe do pedido para reverter com taxa correta
        $ctx_class_id = 0;
        foreach ( $order->get_items( 'line_item' ) as $_item ) {
            $_product = $_item instanceof WC_Order_Item_Product ? $_item->get_product() : null;
            if ( $_product ) { $ctx_class_id = (int) $_product->get_shipping_class_id(); break; }
        }
        $real = function_exists( 'senderzz_reverse_markup' )
            ? senderzz_reverse_markup( $charged, $ctx_class_id )
            : round( max( 0, ( $charged - 3.99 ) / 1.20 ), 2 );
    }

    $service_fee = round( max( 0, $charged - $real ), 2 );
    $margin      = $service_fee;
    $postcode    = $order->get_shipping_postcode() ?: $order->get_billing_postcode();
    $city        = $order->get_shipping_city() ?: $order->get_billing_city();
    $uf          = $order->get_shipping_state() ?: $order->get_billing_state();
    $postcode_digits = preg_replace( '/\D+/', '', (string) $postcode );
    $owner_ctx = senderzz_sync_order_owner_meta( $order );

    return [
        'customer_id'       => (int) $order->get_customer_id(),
        'customer_email'    => (string) $order->get_billing_email(),
        'owner_user_id'     => (int) ( $owner_ctx['effective_owner_user_id'] ?? 0 ),
        'owner_email'       => (string) ( $owner_ctx['effective_owner_email'] ?? '' ),
        'product_shipping_class_id' => (int) ( $owner_ctx['class_id'] ?? 0 ),
        'product_shipping_class_slug' => (string) ( $owner_ctx['class_slug'] ?? '' ),
        'product_shipping_class_name' => (string) ( $owner_ctx['class_name'] ?? '' ),
        'shipping_real_cost'=> round( $real, 2 ),
        'shipping_charged'  => round( $charged, 2 ),
        'service_fee'       => $service_fee,
        'margin'            => $margin,
        'carrier_name'      => $carrier,
        'service_id'        => $service_id,
        'service_name'      => $carrier,
        'delivery_time'     => $delivery_time,
        'destination_uf'    => strtoupper( (string) $uf ),
        'destination_city'  => (string) $city,
        'destination_postcode' => $postcode_digits,
        'postcode_prefix'   => substr( $postcode_digits, 0, 3 ),
        'postcode_prefix_5' => substr( $postcode_digits, 0, 5 ),
    ];
}

function senderzz_sync_order_shipping_meta( WC_Order $order ): array {
    $s = senderzz_order_shipping_snapshot( $order );
    foreach ( $s as $key => $value ) {
        $order->update_meta_data( '_senderzz_' . $key, $value );
    }
    $order->save();
    return $s;
}

function senderzz_sync_order_offer_meta( WC_Order $order ): void {
    global $wpdb;

    $token = '';

    if ( ! empty( $_REQUEST['sz'] ) ) {
        $token = sanitize_text_field( wp_unslash( $_REQUEST['sz'] ) );
    }

    if ( ! $token && ! empty( $_POST['senderzz_offer_token'] ) ) {
        $token = sanitize_text_field( wp_unslash( $_POST['senderzz_offer_token'] ) );
    }

    if ( ! $token && ! empty( $_COOKIE['senderzz_offer_token'] ) ) {
        $token = sanitize_text_field( wp_unslash( $_COOKIE['senderzz_offer_token'] ) );
    }

    if ( ! $token && ! empty( $_SERVER['HTTP_REFERER'] ) ) {
        $ref   = esc_url_raw( wp_unslash( $_SERVER['HTTP_REFERER'] ) );
        $parts = wp_parse_url( $ref );

        if ( ! empty( $parts['query'] ) ) {
            parse_str( $parts['query'], $query );

            if ( ! empty( $query['sz'] ) ) {
                $token = sanitize_text_field( $query['sz'] );
            }
        }
    }

    $offer_name  = '';
    $offer_value = 0.0;

    if ( $token ) {
        $table = $wpdb->prefix . 'senderzz_checkout_links';

        $link = $wpdb->get_row(
            $wpdb->prepare(
                "SELECT * FROM `{$table}` WHERE slug = %s OR token = %s LIMIT 1",
                $token,
                $token
            ),
            ARRAY_A
        );

        if ( $link ) {
            $payload = ! empty( $link['payload'] )
                ? ( json_decode( $link['payload'], true ) ?: [] )
                : [];

            $offer_name  = sanitize_text_field( $link['name'] ?? '' );
            $offer_value = (float) ( $payload['valor'] ?? 0 );
        }
    }

    if ( ! $offer_name && ! empty( $_COOKIE['senderzz_offer_name'] ) ) {
        $offer_name = sanitize_text_field( wp_unslash( $_COOKIE['senderzz_offer_name'] ) );
    }

    if ( $offer_value <= 0 && ! empty( $_COOKIE['senderzz_offer_value'] ) ) {
        $offer_value = (float) sanitize_text_field( wp_unslash( $_COOKIE['senderzz_offer_value'] ) );
    }

    if ( ! $offer_name && ! $offer_value ) {
        return;
    }

    if ( $offer_name !== '' ) {
        $order->update_meta_data( '_senderzz_offer_name', $offer_name );
    }

    if ( $offer_value > 0 ) {
        $order->update_meta_data( '_senderzz_offer_value', $offer_value );
    }

    if ( $token !== '' ) {
        $order->update_meta_data( '_senderzz_offer_token', $token );
    }

    foreach ( $order->get_items( 'line_item' ) as $item ) {
        if ( $offer_name !== '' ) {
            $item->update_meta_data( '_senderzz_offer_name', $offer_name );
        }

        if ( $offer_value > 0 ) {
            $item->update_meta_data( '_senderzz_offer_value', $offer_value );
        }

        if ( $token !== '' ) {
            $item->update_meta_data( '_senderzz_offer_token', $token );
        }

        $item->save();
    }

    $order->save();
}
add_action( 'woocommerce_checkout_order_processed', function( $order_id ) {
    $order = wc_get_order( $order_id );

    if ( ! $order ) {
        return;
    }

    senderzz_sync_order_offer_meta( $order );
    senderzz_sync_order_shipping_meta( $order );
}, 30 );
add_action( 'woocommerce_update_order', function( $order_id ) {
    // Guard de reentrância: senderzz_sync_order_shipping_meta() chama $order->save(),
    // que re-dispara woocommerce_update_order — este flag evita loop infinito de saves.
    static $syncing = [];
    if ( isset( $syncing[ $order_id ] ) ) {
        return;
    }
    $syncing[ $order_id ] = true;
    $order = wc_get_order( $order_id );
    if ( $order ) {
        senderzz_sync_order_shipping_meta( $order );
    }
    unset( $syncing[ $order_id ] );
}, 30 );


// Funções de carteira por pedido → senderzz-wallet-engine.php
