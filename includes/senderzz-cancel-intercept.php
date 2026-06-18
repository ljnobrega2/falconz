<?php
/**
 * Senderzz — Cancelamento direto para WooCommerce cancelled.
 *
 * Fluxo novo:
 *
 *  Pedido cancelado → vai direto para "cancelled"
 *
 * Regras:
 * - Não usa mais status intermediário "emcancelamento".
 * - Não intercepta cancelled.
 * - Não aguarda polling do Melhor Envio para mudar o status do pedido.
 * - Estorno/cancelamento de etiqueta deve ser tratado pelos hooks já existentes
 *   da carteira/Melhor Envio, sem alterar o status visual do pedido.
 */

defined( 'ABSPATH' ) || exit;

if ( defined( 'SENDERZZ_CANCEL_INTERCEPT_LOADED' ) ) return;
define( 'SENDERZZ_CANCEL_INTERCEPT_LOADED', true );

/**
 * 1. Cancelamento direto
 *
 * Antes:
 * - cancelled era interceptado e redirecionado para emcancelamento.
 *
 * Agora:
 * - cancelled passa direto.
 * - Apenas registra nota/log para auditoria.
 */
add_action( 'woocommerce_order_status_changed', function ( int $order_id, string $old_status, string $new_status ) {

    if ( $new_status !== 'cancelled' ) return;

    $order = wc_get_order( $order_id );
    if ( ! $order ) return;

    $already_noted = (string) $order->get_meta( '_senderzz_direct_cancel_noted' );

    if ( $already_noted !== 'yes' ) {
        $order->update_meta_data( '_senderzz_direct_cancel_noted', 'yes' );
        $order->add_order_note( 'Senderzz: pedido cancelado diretamente no padrão WooCommerce. Status intermediário em cancelamento não é mais utilizado.' );
        $order->save();
    }

    if ( function_exists( 'senderzz_me_log' ) ) {
        senderzz_me_log( 'cancel.direct_cancelled', [
            'order_id'   => $order_id,
            'old_status' => $old_status,
            'new_status' => $new_status,
        ] );
    }

}, 20, 3 );


/**
 * 2. Compatibilidade: modo de estorno quando houver confirmação ME.
 *
 * Mantido para não quebrar handlers existentes de webhook/polling externos.
 * Se algum handler perguntar o modo de estorno, responde immediate.
 */
add_filter( 'senderzz_me_cancel_refund_mode', function ( string $mode, int $order_id ): string {
    return 'immediate';
}, 10, 2 );


/**
 * 3. POLLING: embalado → enviado quando ME confirmar postagem
 *
 * Mantido, pois não tem relação com o status emcancelamento.
 */
add_action( 'init', function (): void {
    if ( ! wp_next_scheduled( 'sz_posted_polling_cron' ) ) {
        wp_schedule_event( time(), 'sz_every_5min', 'sz_posted_polling_cron' );
    }
} );

add_action( 'sz_posted_polling_cron', 'sz_posted_polling_check_orders' );

function sz_posted_polling_check_orders(): void {
    $orders = wc_get_orders( [
        'status' => 'wc-embalado',
        'limit'  => 20,
        'meta_query' => [
            [
                'key'     => '_melhor_envio_item_id',
                'compare' => 'EXISTS',
            ],
            [
                'key'     => '_melhor_envio_item_id',
                'value'   => '',
                'compare' => '!=',
            ],
        ],
    ] );

    if ( empty( $orders ) ) return;

    $token    = function_exists( 'tpc_me_token' ) ? tpc_me_token() : (string) get_option( 'tpc_me_token', '' );
    $api_base = function_exists( 'tpc_me_api_base' ) ? tpc_me_api_base() : 'https://melhorenvio.com.br/api/v2';

    if ( ! $token ) return;

    foreach ( $orders as $order ) {
        $order_id = $order->get_id();
        $item_id  = (string) $order->get_meta( '_melhor_envio_item_id' );

        if ( ! $item_id ) continue;

        $response = wp_remote_get( $api_base . '/me/orders/' . $item_id, [
            'timeout' => 15,
            'headers' => [
                'Authorization' => 'Bearer ' . $token,
                'Accept'        => 'application/json',
                'User-Agent'    => 'Senderzz Logistics (suporte@app.senderzz.com.br)',
            ],
        ] );

        if ( is_wp_error( $response ) ) continue;

        $code = (int) wp_remote_retrieve_response_code( $response );
        $data = json_decode( wp_remote_retrieve_body( $response ), true );

        if ( $code !== 200 || ! is_array( $data ) ) continue;

        $me_status = strtolower( (string) ( $data['status'] ?? '' ) );
        $posted_at = (string) ( $data['posted_at'] ?? '' );

        $is_posted = in_array( $me_status, [ 'posted', 'collected', 'delivered' ], true ) || $posted_at !== '';

        if ( function_exists( 'senderzz_me_log' ) ) {
            senderzz_me_log( 'posted_polling.checked', [
                'order_id'  => $order_id,
                'item_id'   => $item_id,
                'me_status' => $me_status,
                'is_posted' => $is_posted,
            ] );
        }

        if ( $is_posted ) {
            $order->update_meta_data( '_senderzz_operator_sent_at', $posted_at ?: current_time( 'mysql' ) );
            $order->update_meta_data( '_melhor_envio_posted_at', $posted_at ?: current_time( 'mysql' ) );
            $order->save();

            $order->update_status( 'enviado', 'Senderzz: Melhor Envio confirmou postagem via polling — pedido enviado.' );

            if ( function_exists( 'senderzz_me_log' ) ) {
                senderzz_me_log( 'posted_polling.confirmed', [
                    'order_id'  => $order_id,
                    'item_id'   => $item_id,
                    'me_status' => $me_status,
                ] );
            }
        }
    }
}


/**
 * 4. Limpa crons antigos e novo cron ao desativar.
 */
register_deactivation_hook( defined( 'TPC_FILE' ) ? TPC_FILE : __FILE__, function (): void {
    wp_clear_scheduled_hook( 'sz_cancel_polling_cron' );
    wp_clear_scheduled_hook( 'sz_posted_polling_cron' );
} );


/**
 * 5. Segurança visual: remove ação cancelar se por acaso ainda existir pedido antigo em emcancelamento.
 *
 * Isso só protege pedidos legados.
 * Pedido novo não deve mais entrar em emcancelamento.
 */
add_filter( 'woocommerce_my_account_my_orders_actions', function ( array $actions, $order ): array {
    if ( $order instanceof WC_Order ) {
        $status = str_replace( '-', '', (string) $order->get_status() );

        if ( $status === 'emcancelamento' && isset( $actions['cancel'] ) ) {
            unset( $actions['cancel'] );
        }
    }

    return $actions;
}, 20, 2 );