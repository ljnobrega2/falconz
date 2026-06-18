<?php
/**
 * Senderzz — Cancelamento de etiqueta no Melhor Envio via status "emcancelamento".
 *
 * O que faz:
 *  1. Registra o status WooCommerce "wc-emcancelamento" (Em Cancelamento).
 *  2. Ao entrar nesse status, dispara POST /api/v2/me/shipment/cancel
 *     na API do Melhor Envio de forma assíncrona (via WP Cron imediato).
 *  3. Adiciona REST endpoint de teste manual:
 *     POST /wp-json/senderzz/v1/cancel-label  { "order_id": 123 }
 *
 * Instalação: inclua este arquivo no plugin principal (senderzz-logistics.php)
 * ou coloque-o em includes/ e adicione um require_once aqui.
 *
 * Compatível com: senderzz-logistics v2.5.17+  |  WooCommerce 7+  |  PHP 8.0+
 */

defined( 'ABSPATH' ) || exit;

if ( defined( 'SENDERZZ_ME_CANCEL_LOADED' ) ) return;
define( 'SENDERZZ_ME_CANCEL_LOADED', true );

// ─────────────────────────────────────────────────────────────────────────────
// 1. REGISTRAR STATUS "Em Cancelamento"
// ─────────────────────────────────────────────────────────────────────────────

add_action( 'init', function () {
    if ( ! function_exists( 'wc_register_order_status' ) ) return;

    wc_register_order_status( 'wc-emcancelamento', [
        'label'                     => 'Em Cancelamento',
        'public'                    => false,
        'exclude_from_search'       => false,
        'show_in_admin_all_list'    => true,
        'show_in_admin_status_list' => true,
        'label_count'               => _n_noop(
            'Em Cancelamento <span class="count">(%s)</span>',
            'Em Cancelamento <span class="count">(%s)</span>'
        ),
    ] );
}, 10 );

add_filter( 'wc_order_statuses', function ( array $statuses ) {
    $statuses['wc-emcancelamento'] = 'Em Cancelamento';
    return $statuses;
} );

// ─────────────────────────────────────────────────────────────────────────────
// 2. HOOK: ao entrar em "emcancelamento", agenda cancelamento assíncrono
// ─────────────────────────────────────────────────────────────────────────────

add_action( 'woocommerce_order_status_emcancelamento', function ( int $order_id ) {
    // Agenda execução imediata via WP Cron (não bloqueia o request do admin).
    wp_schedule_single_event( time(), 'senderzz_cancel_me_label_async', [ $order_id ] );
    spawn_cron();

    // Nota imediata para o admin saber que foi agendado.
    $order = wc_get_order( $order_id );
    if ( $order ) {
        $item_id = (string) $order->get_meta( '_melhor_envio_item_id' );
        $order->add_order_note(
            'Senderzz: pedido entrou em "Em Cancelamento". ' .
            ( $item_id ? "Cancelamento da etiqueta ME ({$item_id}) agendado." : 'Nenhuma etiqueta ME encontrada — apenas status alterado.' )
        );
        $order->save();
    }
}, 10, 1 );

add_action( 'senderzz_cancel_me_label_async', 'senderzz_me_cancel_label_for_order', 10, 1 );

// ─────────────────────────────────────────────────────────────────────────────
// 3. FUNÇÃO CENTRAL: chama POST /me/shipment/cancel
// ─────────────────────────────────────────────────────────────────────────────

/**
 * Cancela a etiqueta do Melhor Envio para o pedido WooCommerce indicado.
 *
 * @param  int  $order_id  ID do pedido WooCommerce.
 * @return array { success: bool, http_code: int, body: string, message: string }
 */
function senderzz_me_cancel_label_for_order( int $order_id ): array {
    $order = wc_get_order( $order_id );
    if ( ! $order ) {
        return [ 'success' => false, 'http_code' => 0, 'body' => '', 'message' => 'Pedido não encontrado.' ];
    }

    $item_id = (string) $order->get_meta( '_melhor_envio_item_id' );
    if ( $item_id === '' ) {
        $note = 'Senderzz: nenhuma etiqueta ME (item_id vazio) — cancelamento ignorado.';
        $order->add_order_note( $note );
        $order->save();
        return [ 'success' => false, 'http_code' => 0, 'body' => '', 'message' => $note ];
    }

    // Verifica se a etiqueta já está num estado que não pode ser cancelado.
    $label_status = (string) $order->get_meta( '_melhor_envio_label_status' );
    if ( in_array( $label_status, [ 'cancelled', 'canceled', 'posted', 'delivered' ], true ) ) {
        $note = "Senderzz: etiqueta ME já está em status '{$label_status}' — cancelamento abortado.";
        $order->add_order_note( $note );
        $order->save();
        return [ 'success' => false, 'http_code' => 0, 'body' => '', 'message' => $note ];
    }

    // Credenciais ME (mesmo padrão do senderzz-me-webhook.php)
    $settings = get_option( 'woocommerce_wc-melhor-envio_settings', [] );
    $token    = (string) ( $settings['client_secret'] ?? '' );
    if ( $token === '' ) {
        $note = 'Senderzz: token Melhor Envio ausente — cancelamento abortado.';
        $order->add_order_note( $note );
        $order->save();
        return [ 'success' => false, 'http_code' => 0, 'body' => '', 'message' => $note ];
    }

    $sandbox = ( ( $settings['sandbox'] ?? 'no' ) === 'yes' );
    $host    = $sandbox ? 'sandbox.melhorenvio.com.br' : 'www.melhorenvio.com.br';
    $url     = "https://{$host}/api/v2/me/shipment/cancel";

    $body = wp_json_encode( [
        [ 'id' => $item_id, 'reason_id' => 2, 'description' => 'Cancelamento solicitado pelo lojista' ],
    ] );

    $response = wp_safe_remote_post( $url, [
        'timeout' => 30,
        'headers' => [
            'Accept'        => 'application/json',
            'Content-Type'  => 'application/json',
            'Authorization' => 'Bearer ' . $token,
            'User-Agent'    => 'Senderzz Logistics (suporte@app.senderzz.com.br)',
        ],
        'body' => $body,
    ] );

    if ( is_wp_error( $response ) ) {
        $msg  = 'Senderzz: erro ao cancelar etiqueta ME — ' . $response->get_error_message();
        $order->add_order_note( $msg );
        $order->update_meta_data( '_senderzz_me_cancel_error', $response->get_error_message() );
        $order->update_meta_data( '_senderzz_me_cancel_tried_at', current_time( 'mysql' ) );
        $order->save();
        return [ 'success' => false, 'http_code' => 0, 'body' => '', 'message' => $msg ];
    }

    $http_code = (int) wp_remote_retrieve_response_code( $response );
    $body      = (string) wp_remote_retrieve_body( $response );
    $success   = in_array( $http_code, [ 200, 201, 204 ], true );

    // Registra resultado nos metadados do pedido.
    $order->update_meta_data( '_senderzz_me_cancel_http_code', $http_code );
    $order->update_meta_data( '_senderzz_me_cancel_response', substr( $body, 0, 500 ) );
    $order->update_meta_data( '_senderzz_me_cancel_tried_at', current_time( 'mysql' ) );

    if ( $success ) {
        $order->update_meta_data( '_melhor_envio_label_status', 'cancelled' );
        $note = "Senderzz: etiqueta ME ({$item_id}) cancelada com sucesso via API (HTTP {$http_code}).";
        $order->add_order_note( $note );
        // Não muda status WooCommerce aqui — o webhook do ME fará isso quando confirmar.
        // Se preferir forçar cancelled imediatamente, descomente a linha abaixo:
        // $order->update_status( 'cancelled', $note );
    } else {
        $note = "Senderzz: FALHA ao cancelar etiqueta ME ({$item_id}) — HTTP {$http_code}: " . substr( $body, 0, 200 );
        $order->add_order_note( $note );
    }

    $order->save();

    if ( function_exists( 'senderzz_me_log' ) ) {
        senderzz_me_log( 'cancel_label', [
            'order_id'  => $order_id,
            'item_id'   => $item_id,
            'http_code' => $http_code,
            'success'   => $success,
        ] );
    }

    return [ 'success' => $success, 'http_code' => $http_code, 'body' => $body, 'message' => $note ];
}

// ─────────────────────────────────────────────────────────────────────────────
// 4. REST ENDPOINT para teste manual sem mudar status
//    POST /wp-json/senderzz/v1/cancel-label
//    Body: { "order_id": 123 }
//    Auth: precisa estar logado como administrador ou usar Application Password.
// ─────────────────────────────────────────────────────────────────────────────

add_action( 'rest_api_init', function () {
    register_rest_route( 'senderzz/v1', '/cancel-label', [
        'methods'             => 'POST',
        'callback'            => function ( WP_REST_Request $request ) {
            $order_id = (int) $request->get_param( 'order_id' );
            if ( ! $order_id ) {
                return new WP_REST_Response( [ 'ok' => false, 'message' => 'Parâmetro order_id obrigatório.' ], 400 );
            }
            $result = senderzz_me_cancel_label_for_order( $order_id );
            return new WP_REST_Response( array_merge( [ 'ok' => $result['success'] ], $result ), $result['success'] ? 200 : 422 );
        },
        'permission_callback' => function () {
            return current_user_can( 'manage_woocommerce' );
        },
        'args' => [
            'order_id' => [
                'required'          => true,
                'sanitize_callback' => 'absint',
            ],
        ],
    ] );
} );
