<?php
/**
 * Senderzz v2.5.47 — Estorno confiável Melhor Envio.
 * Webhook order.cancelled apenas agenda validação; crédito só após API confirmar cancelled/canceled.
 */
defined( 'ABSPATH' ) || exit;

if ( defined( 'SENDERZZ_ME_REFUND_VALIDATION_LOADED' ) ) return;
define( 'SENDERZZ_ME_REFUND_VALIDATION_LOADED', true );

add_action( 'senderzz_check_me_refund_status', 'senderzz_check_me_refund_status', 10, 1 );

function senderzz_schedule_me_refund_validation( int $order_id, int $delay = 300 ): void {
    $delay = max( 60, $delay );
    if ( ! wp_next_scheduled( 'senderzz_check_me_refund_status', [ $order_id ] ) ) {
        wp_schedule_single_event( time() + $delay, 'senderzz_check_me_refund_status', [ $order_id ] );
    }
}

/*
 * Token do Melhor Envio: usar a função central de includes/senderzz-secrets.php.
 * Não declarar senderzz_get_me_token() aqui, porque isso gera fatal error quando
 * senderzz-secrets.php já foi carregado pelo bootstrap principal do plugin.
 */

function senderzz_get_me_order_status( $me_order_id ): string {
    $me_order_id = trim( (string) $me_order_id );
    if ( $me_order_id === '' ) return 'unknown';

    $token = senderzz_get_me_token();
    if ( $token === '' ) return 'no_token';

    $api_base = function_exists( 'tpc_me_api_base' ) ? rtrim( (string) tpc_me_api_base(), '/' ) : 'https://melhorenvio.com.br/api/v2';
    $urls = array_values( array_unique( [
        $api_base . '/me/orders/' . rawurlencode( $me_order_id ),
        'https://api.melhorenvio.com.br/api/v2/me/shipment/orders/' . rawurlencode( $me_order_id ),
        'https://melhorenvio.com.br/api/v2/me/orders/' . rawurlencode( $me_order_id ),
    ] ) );

    foreach ( $urls as $url ) {
        $response = wp_safe_remote_get( $url, [
            'timeout' => 20,
            'headers' => [
                'Authorization' => 'Bearer ' . $token,
                'Accept'        => 'application/json',
                'User-Agent'    => 'Senderzz Logistics (suporte@app.senderzz.com.br)',
            ],
        ] );

        if ( is_wp_error( $response ) ) {
            continue;
        }

        $code = (int) wp_remote_retrieve_response_code( $response );
        $body = json_decode( (string) wp_remote_retrieve_body( $response ), true );
        if ( $code >= 200 && $code < 300 && is_array( $body ) ) {
            $status = strtolower( sanitize_text_field( (string) ( $body['status'] ?? $body['data']['status'] ?? 'unknown' ) ) );
            if ( $status !== '' && $status !== 'unknown' ) return $status;
        }
    }

    return 'error';
}

function senderzz_wallet_refund_order_confirmed( WC_Order $order ): bool {
    if ( $order->get_meta( '_senderzz_wallet_refunded' ) === 'yes' ) return true;

    $user_id = (int) $order->get_meta( '_senderzz_wallet_debited_user_id' );
    if ( ! $user_id && function_exists( 'senderzz_get_order_wallet_owner_id' ) ) {
        $user_id = (int) senderzz_get_order_wallet_owner_id( $order );
    }

    $value = (float) $order->get_meta( '_senderzz_wallet_debit_value' );
    if ( $value <= 0 ) $value = (float) $order->get_meta( '_senderzz_wallet_debited' );
    if ( $value <= 0 && function_exists( 'senderzz_wallet_required_value' ) ) {
        $value = (float) senderzz_wallet_required_value( $order );
    }

    if ( $user_id <= 0 || $value <= 0 || ! function_exists( 'tpc_creditar' ) ) {
        $order->add_order_note( 'Senderzz: estorno validado, mas sem usuário/valor/função de crédito disponível.' );
        $order->save();
        return false;
    }

    $tx = tpc_creditar( $user_id, $value, 'Estorno frete Pedido #' . $order->get_id(), [
        'type'        => 'refund',
        'order_id'    => $order->get_id(),
        'me_order_id' => (string) $order->get_meta( '_melhor_envio_order_id' ),
        'reference'   => 'refund_order_' . $order->get_id(),
        'referencia'  => 'refund_order_' . $order->get_id(),
    ] );

    if ( $tx ) {
        $order->update_meta_data( '_senderzz_wallet_refunded', 'yes' );
        $order->update_meta_data( '_senderzz_wallet_refund_tx', $tx );
        $order->save();
        $order->add_order_note( 'Senderzz: estorno confirmado via API ME. Crédito de R$ ' . number_format( $value, 2, ',', '.' ) . ' realizado na carteira. Transação #' . $tx . '.' );
        return true;
    }

    return false;
}

function senderzz_check_me_refund_status( int $order_id ): void {
    $order = wc_get_order( $order_id );
    if ( ! $order ) return;

    if ( $order->get_meta( '_senderzz_wallet_refunded' ) === 'yes' ) return;

    $lock = (int) $order->get_meta( '_senderzz_refund_lock' );
    if ( $lock && ( time() - $lock ) < 600 ) return;

    $order->update_meta_data( '_senderzz_refund_lock', time() );
    $order->save();

    try {
        $me_order_id = (string) $order->get_meta( '_melhor_envio_order_id' );
        if ( $me_order_id === '' ) {
            $me_order_id = (string) $order->get_meta( '_melhor_envio_item_id' );
        }
        if ( $me_order_id === '' ) {
            $order->update_meta_data( '_senderzz_cancel_validation_status', 'failed' );
            $order->add_order_note( 'Senderzz: sem ME order_id para validar estorno.' );
            return;
        }

        $status = senderzz_get_me_order_status( $me_order_id );
        $order->update_meta_data( '_senderzz_cancel_validation_last_status', $status );
        $order->update_meta_data( '_senderzz_cancel_validation_last_check', current_time( 'mysql' ) );

        if ( in_array( $status, [ 'cancelled', 'canceled' ], true ) ) {
            $ok = senderzz_wallet_refund_order_confirmed( $order );
            $order = wc_get_order( $order_id );
            if ( $order ) {
                $order->update_meta_data( '_senderzz_cancel_validation_status', $ok ? 'confirmed' : 'failed_credit' );
                $order->delete_meta_data( '_senderzz_refund_lock' );
                $order->save();
            }
            return;
        }

        $attempts = (int) $order->get_meta( '_senderzz_cancel_validation_attempts' );
        $attempts++;
        $order->update_meta_data( '_senderzz_cancel_validation_attempts', $attempts );

        if ( $attempts < 5 ) {
            $order->update_meta_data( '_senderzz_cancel_validation_status', 'pending' );
            $order->add_order_note( 'Senderzz: ME ainda não confirmou cancelamento via API (status: ' . $status . '). Nova tentativa agendada.' );
            senderzz_schedule_me_refund_validation( $order_id, 300 * $attempts );
        } else {
            $order->update_meta_data( '_senderzz_cancel_validation_status', 'failed' );
            $order->add_order_note( 'Senderzz: falha ao validar estorno após múltiplas tentativas. Último status ME: ' . $status . '.' );
        }
    } finally {
        $order = wc_get_order( $order_id );
        if ( $order ) {
            $order->delete_meta_data( '_senderzz_refund_lock' );
            $order->save();
        }
    }
}

// Blindagem final: qualquer tentativa de jogar para "separado" volta para aprovado.
add_action( 'woocommerce_order_status_changed', function( $order_id, $old_status, $new_status ) {
    $new = strtolower( str_replace( 'wc-', '', (string) $new_status ) );
    if ( $new !== 'separado' ) return;

    $order = wc_get_order( $order_id );
    if ( ! $order ) return;

    $order->update_status( 'aprovado', 'Senderzz: status separado desativado. Pedido mantido como aprovado com etiqueta gerada.' );
}, 999, 3 );
