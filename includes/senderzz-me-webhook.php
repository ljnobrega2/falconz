<?php
/**
 * Senderzz — Melhor Envio webhook + reconciliação ativa.
 *
 * Endpoint público para o painel Área Dev do Melhor Envio:
 *   POST /wp-json/senderzz/v1/webhook/me
 *
 * Correções v2.5.18:
 *  - Validação de assinatura x-me-signature (HMAC-SHA256)
 *  - Log movido para diretório privado (fora de uploads/)
 *  - Idempotência: eventos duplicados ignorados via hash + meta por pedido
 *  - Rate limit: transient bloqueia flood por IP
 *  - Polling: lock global + lock por pedido evitam race condition
 *  - Polling: contador de zumbi para pedidos com 404 repetidos
 *  - Estorno de carteira diferido 12h via WP-Cron (modo deferred_12h / aprovado)
 */
defined( 'ABSPATH' ) || exit;

if ( defined( 'SENDERZZ_ME_WEBHOOK_LOADED' ) ) return;
define( 'SENDERZZ_ME_WEBHOOK_LOADED', true );

// ─────────────────────────────────────────────────────────────────────────────
// REGISTRO DE ROTAS E CRON
// ─────────────────────────────────────────────────────────────────────────────

add_action( 'rest_api_init', function () {
    register_rest_route( 'senderzz/v1', '/webhook/me', [
        'methods'             => [ 'POST', 'GET' ],
        'callback'            => 'senderzz_me_webhook_endpoint',
        'permission_callback' => '__return_true',
    ] );
} );

add_filter( 'cron_schedules', function ( $schedules ) {
    if ( empty( $schedules['senderzz_5min'] ) ) {
        $schedules['senderzz_5min'] = [
            'interval' => 5 * MINUTE_IN_SECONDS,
            'display'  => 'Senderzz a cada 5 minutos',
        ];
    }
    return $schedules;
} );

add_action( 'init', function () {
    if ( ! wp_next_scheduled( 'senderzz_me_reconcile_shipments' ) ) {
        wp_schedule_event( time() + 120, 'senderzz_5min', 'senderzz_me_reconcile_shipments' );
    }
}, 20 );

add_action( 'senderzz_me_reconcile_shipments', 'senderzz_me_reconcile_shipments' );
add_action( 'senderzz_deferred_wallet_refund',  'senderzz_me_execute_deferred_refund' );

// ─────────────────────────────────────────────────────────────────────────────
// LOG — grava em diretório privado (não acessível via URL)
// ─────────────────────────────────────────────────────────────────────────────

function senderzz_me_log( string $message, $context = null ): void {
    $line = gmdate( 'Y-m-d\\TH:i:s+00:00' ) . ' ' . $message;
    if ( $context !== null ) {
        // V-NEW-08: mascara PII (CPF/CNPJ/email/telefone/endereço) antes de logar.
        if ( ! is_string( $context ) && function_exists( 'tpc_mask_sensitive_log_data' ) ) {
            $context = tpc_mask_sensitive_log_data( $context );
        }
        $line .= ' ' . ( is_string( $context ) ? $context : wp_json_encode( $context, JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES ) );
    }

    if ( function_exists( 'wc_get_logger' ) ) {
        try { wc_get_logger()->info( $line, [ 'source' => 'senderzz-me-webhook' ] ); } catch ( \Throwable $e ) {}
    }

    // Diretório privado fora de uploads/ — não acessível via URL.
    $log_dir = WP_CONTENT_DIR . '/senderzz-logs';
    if ( ! is_dir( $log_dir ) ) {
        wp_mkdir_p( $log_dir );
        file_put_contents( $log_dir . '/.htaccess', "Options -Indexes\nDeny from all\n" );
        file_put_contents( $log_dir . '/index.php', '<?php // Silence is golden.' );
    }

    $file   = $log_dir . '/senderzz-webhook-' . gmdate( 'Y-m' ) . '.log';
    $result = file_put_contents( $file, $line . PHP_EOL, FILE_APPEND | LOCK_EX );
    if ( $result === false ) {
        error_log( 'Senderzz: falha ao gravar log — ' . $line );
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// VALIDAÇÃO DE ASSINATURA HMAC (x-me-signature)
// ─────────────────────────────────────────────────────────────────────────────

function senderzz_me_verify_signature( WP_REST_Request $request ): bool {
    $settings = get_option( 'woocommerce_wc-melhor-envio_settings', [] );
    $secret   = (string) ( $settings['client_secret'] ?? '' );

    // CRIT-03: fail-closed. Se o secret não está configurado, recusa.
    // Estado de "secret ausente" deve aparecer ALTO/VISIVEL no admin (notice abaixo)
    // — não pode virar bypass silencioso.
    if ( $secret === '' ) {
        senderzz_me_log( 'webhook.signature_no_secret', 'Token ME ausente — webhook rejeitado por segurança.' );
        return false;
    }

    $signature = (string) ( $request->get_header( 'x-me-signature' ) ?? '' );

    if ( $signature === '' ) {
        $payload = $request->get_json_params();
        // CRIT-04: aceitar apenas "ping" exato (whitelist), não substring.
        // O bug original usava strpos(...,'ping') que casava "pingorder.cancelled" etc.
        $event_value = is_array( $payload ) ? (string) ( $payload['event'] ?? '' ) : '';
        if ( in_array( $event_value, [ 'ping', 'test', 'webhook.ping' ], true ) ) {
            return true;
        }
        senderzz_me_log( 'webhook.signature_missing', [ 'ip' => $_SERVER['REMOTE_ADDR'] ?? '' ] );
        return false;
    }

    $body     = (string) $request->get_body();
    $expected = base64_encode( hash_hmac( 'sha256', $body, $secret, true ) );

    return hash_equals( $expected, $signature );
}

// ─────────────────────────────────────────────────────────────────────────────
// RATE LIMIT — máximo 60 req/min por IP
// ─────────────────────────────────────────────────────────────────────────────

function senderzz_me_check_rate_limit(): bool {
    $ip    = (string) ( $_SERVER['REMOTE_ADDR'] ?? 'unknown' );
    $key   = 'senderzz_me_rl_' . md5( $ip );
    $count = (int) get_transient( $key );
    $limit = 60;

    if ( $count >= $limit ) {
        senderzz_me_log( 'webhook.rate_limited', [ 'ip' => $ip, 'count' => $count ] );
        return false;
    }

    set_transient( $key, $count + 1, MINUTE_IN_SECONDS );
    return true;
}

// ─────────────────────────────────────────────────────────────────────────────
// ENDPOINT PRINCIPAL
// ─────────────────────────────────────────────────────────────────────────────

function senderzz_me_webhook_endpoint( WP_REST_Request $request ): WP_REST_Response {
    $raw     = (string) $request->get_body();
    $payload = $request->get_json_params();
    if ( ! is_array( $payload ) ) $payload = [];

    // GET e POST vazio = teste do painel ME.
    if ( $request->get_method() === 'GET' || empty( $payload ) ) {
        senderzz_me_log( 'webhook.test', [ 'method' => $request->get_method() ] );
        return new WP_REST_Response( [
            'ok'       => true,
            'endpoint' => rest_url( 'senderzz/v1/webhook/me' ),
            'note'     => 'Webhook Melhor Envio ativo.',
        ], 200 );
    }

    // Rate limit.
    if ( ! senderzz_me_check_rate_limit() ) {
        return new WP_REST_Response( [ 'ok' => false, 'note' => 'rate_limited' ], 429 );
    }

    // Validação de assinatura HMAC.
    if ( ! senderzz_me_verify_signature( $request ) ) {
        senderzz_me_log( 'webhook.signature_invalid', [ 'ip' => $_SERVER['REMOTE_ADDR'] ?? '' ] );
        return new WP_REST_Response( [ 'ok' => false, 'note' => 'unauthorized' ], 401 );
    }

    // S10: idempotência persistente via wp_tpc_webhook_events (UNIQUE KEY) — replica exata após 5 min
    // era ignorada com transient. tpc_webhook_register_event() retorna false se evento já existe.
    $event      = sanitize_text_field( (string) ( $payload['event'] ?? '' ) );
    $me_id      = sanitize_text_field( (string) ( $payload['data']['order_id'] ?? $payload['data']['id'] ?? '' ) );
    $me_status  = sanitize_text_field( (string) ( $payload['data']['status'] ?? '' ) );
    $event_key  = function_exists( 'tpc_webhook_build_event_key' )
        ? tpc_webhook_build_event_key( 'me', $payload, [ 'me_id' => $me_id, 'status' => $me_status, 'raw' => $raw ] )
        : 'me:' . hash( 'sha256', $raw );
    if ( function_exists( 'tpc_webhook_register_event' ) ) {
        if ( ! tpc_webhook_register_event( $event_key, 'melhor_envio', $event ?: 'unknown', $raw, [ 'me_id' => $me_id, 'status' => $me_status ] ) ) {
            senderzz_me_log( 'webhook.duplicate', [ 'event_key' => $event_key ] );
            return new WP_REST_Response( [ 'ok' => true, 'note' => 'duplicate_ignored' ], 200 );
        }
    } else {
        // Fallback se tpc_webhook_register_event não disponível (instalação antiga)
        $idem_key = 'senderzz_me_idem_' . $event_key;
        if ( get_transient( $idem_key ) ) {
            senderzz_me_log( 'webhook.duplicate', [ 'event_key' => $event_key ] );
            return new WP_REST_Response( [ 'ok' => true, 'note' => 'duplicate_ignored' ], 200 );
        }
        set_transient( $idem_key, 1, DAY_IN_SECONDS );
    }

    senderzz_me_log( 'webhook.received', $payload );

    $result = senderzz_me_process_webhook_payload( $payload, 'webhook' );
    return new WP_REST_Response( array_merge( [ 'ok' => true ], $result ), 200 );
}

// ─────────────────────────────────────────────────────────────────────────────
// PROCESSAMENTO DO PAYLOAD
// ─────────────────────────────────────────────────────────────────────────────

function senderzz_me_process_webhook_payload( array $payload, string $source = 'webhook' ): array {
    $event    = sanitize_text_field( (string) ( $payload['event'] ?? '' ) );
    $data     = $payload['data'] ?? [];
    if ( ! is_array( $data ) ) $data = [];

    $me_id    = sanitize_text_field( (string) ( $data['id'] ?? $data['order_id'] ?? $data['shipment_id'] ?? '' ) );
    $protocol = sanitize_text_field( (string) ( $data['protocol'] ?? '' ) );
    $status   = sanitize_key( (string) ( $data['status'] ?? '' ) );

    if ( $me_id === '' && $protocol === '' ) {
        return [ 'processed' => false, 'note' => 'Payload sem data.id/protocol — ignorado.' ];
    }

    $order_id = senderzz_me_find_order_id( $me_id, $protocol );
    if ( ! $order_id ) {
        senderzz_me_log( 'webhook.order_not_found', [ 'event' => $event, 'me_id' => $me_id, 'protocol' => $protocol, 'status' => $status ] );
        return [ 'processed' => false, 'note' => 'Pedido não encontrado.', 'me_id' => $me_id, 'protocol' => $protocol ];
    }

    $order = wc_get_order( $order_id );
    if ( ! $order ) return [ 'processed' => false, 'note' => 'Pedido inválido.' ];

    $normalized_event = strtolower( $event );

    // Idempotência por pedido: eventos finais idênticos ao último processado são ignorados.
    $last_event  = (string) $order->get_meta( '_senderzz_me_last_event' );
    $last_status = (string) $order->get_meta( '_senderzz_me_last_status' );
    if (
        $source === 'webhook' &&
        $last_event !== '' &&
        $last_event === $normalized_event &&
        $last_status === $status &&
        in_array( $normalized_event, [ 'order.cancelled', 'order.delivered', 'order.posted' ], true )
    ) {
        senderzz_me_log( 'webhook.idempotent_skip', [ 'order_id' => $order_id, 'event' => $event, 'status' => $status ] );
        return [ 'processed' => false, 'note' => 'Evento idêntico já processado.', 'order_id' => $order_id ];
    }

    // Atualiza metadados de rastreamento.
    $order->update_meta_data( '_senderzz_me_last_event',   $normalized_event );
    $order->update_meta_data( '_senderzz_me_last_status',  $status );
    $order->update_meta_data( '_senderzz_me_last_payload', wp_json_encode( $payload, JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES ) );
    $order->update_meta_data( '_senderzz_me_last_seen_at', current_time( 'mysql' ) );

    if ( $me_id )                           $order->update_meta_data( '_melhor_envio_item_id',    $me_id );
    if ( $protocol )                        $order->update_meta_data( '_melhor_envio_order_id',   $protocol );
    if ( ! empty( $data['tracking'] ) )     $order->update_meta_data( '_melhor_envio_tracking',   sanitize_text_field( (string) $data['tracking'] ) );
    if ( ! empty( $data['tracking_url'] ) ) $order->update_meta_data( '_melhor_envio_tracking_url', esc_url_raw( (string) $data['tracking_url'] ) );

    $action = 'logged';

    if ( $normalized_event === 'order.created' || $status === 'created' ) {
        $order->update_meta_data( '_melhor_envio_created_at', sanitize_text_field( (string) ( $data['created_at'] ?? current_time( 'mysql' ) ) ) );
        $action = 'created_meta';
    }

    if ( $normalized_event === 'order.pending' || $status === 'pending' ) {
        $order->update_meta_data( '_melhor_envio_label_status', 'pending' );
        $action = 'pending_meta';
    }

    if ( $normalized_event === 'order.released' || $status === 'released' ) {
        $order->update_meta_data( '_melhor_envio_bought_shipping', 'yes' );
        $order->update_meta_data( '_melhor_envio_released_at', sanitize_text_field( (string) ( $data['paid_at'] ?? current_time( 'mysql' ) ) ) );
        $action = 'released_meta';
    }
    if ( $normalized_event === 'order.generated' || $status === 'generated' ) {
        $order->update_meta_data( '_melhor_envio_label_generated', true );
        $order->update_meta_data( '_melhor_envio_generated_at', sanitize_text_field( (string) ( $data['generated_at'] ?? current_time( 'mysql' ) ) ) );
        $order->save();
        $downloaded = senderzz_me_try_download_pdf_for_order( $order );
        $order = wc_get_order( $order_id );
        if ( $downloaded && $order && in_array( $order->get_status(), [ 'aprovado', 'approved' ], true ) ) {
            $order->add_order_note( 'Senderzz: etiqueta gerada pelo webhook do Melhor Envio e PDF salvo no servidor. Pedido mantido como aprovado.' );
            $order->save();
        }
        $action = $downloaded ? 'generated_pdf_saved_aprovado' : 'generated_waiting_pdf';
    }
    if ( $normalized_event === 'order.posted' || $status === 'posted' ) {
        $posted_at = sanitize_text_field( (string) ( $data['posted_at'] ?? current_time( 'mysql' ) ) );
        $order->update_meta_data( '_senderzz_operator_sent_at', $posted_at );
        $order->update_meta_data( '_melhor_envio_posted_at',    $posted_at );
        if ( ! in_array( $order->get_status(), [ 'enviado', 'completed', 'cancelled', 'refunded' ], true ) ) {
            $order->update_status( 'enviado', 'Senderzz: Melhor Envio confirmou postagem — pedido enviado.' );
        }
        $action = 'posted_status_enviado';
    }

    if ( $normalized_event === 'order.delivered' || $status === 'delivered' ) {
        $order->update_meta_data( '_melhor_envio_delivered_at', sanitize_text_field( (string) ( $data['delivered_at'] ?? current_time( 'mysql' ) ) ) );
        if ( ! in_array( $order->get_status(), [ 'entregue', 'cancelled', 'refunded' ], true ) ) {
            $order->update_status( 'entregue', 'Senderzz: Melhor Envio informou entrega do envio.' );
        }
        $action = 'delivered_entregue';
    }

    if ( $normalized_event === 'order.cancelled' || $status === 'cancelled' || $status === 'canceled' || ! empty( $data['canceled_at'] ) ) {
        $cancelled_at = sanitize_text_field( (string) ( $data['canceled_at'] ?? current_time( 'mysql' ) ) );
        $order->update_meta_data( '_senderzz_me_cancelled_at',  $cancelled_at );
        $order->update_meta_data( '_melhor_envio_label_status', 'cancelled' );
        $order->update_meta_data( '_senderzz_me_cancelled_webhook', 'yes' );
        $order->update_meta_data( '_senderzz_cancel_validation_status', 'pending' );
        $order->save();

        // Senderzz v2.5.47: webhook order.cancelled NÃO estorna e NÃO muda o pedido para cancelled.
        // Estorno só depois de confirmação real pela API do Melhor Envio.
        if ( function_exists( 'senderzz_schedule_me_refund_validation' ) ) {
            senderzz_schedule_me_refund_validation( $order_id, 300 );
        } elseif ( ! wp_next_scheduled( 'senderzz_check_me_refund_status', [ $order_id ] ) ) {
            wp_schedule_single_event( time() + 300, 'senderzz_check_me_refund_status', [ $order_id ] );
        }
        $order->add_order_note( 'Senderzz: webhook de cancelamento ME recebido. Estorno pendente de validação via API do Melhor Envio.' );
        $action = 'cancelled_validation_scheduled_no_refund';
    }

    $order = wc_get_order( $order_id );
    if ( $order ) {
        $order->add_order_note( 'Senderzz ME webhook: ' . ( $event ?: 'sem_evento' ) . ' | status=' . ( $status ?: '-' ) . ' | ação=' . $action . ' | origem=' . $source );
        $order->save();
    }

    senderzz_me_log( 'webhook.processed', [ 'order_id' => $order_id, 'event' => $event, 'status' => $status, 'action' => $action, 'source' => $source ] );
    return [ 'processed' => true, 'order_id' => $order_id, 'event' => $event, 'status' => $status, 'action' => $action ];
}

// ─────────────────────────────────────────────────────────────────────────────
// ESTORNO DIFERIDO — executado 12h após o cancelled (modo deferred_12h)
// ─────────────────────────────────────────────────────────────────────────────

function senderzz_me_execute_deferred_refund( int $order_id ): void {
    $order = wc_get_order( $order_id );
    if ( ! $order ) {
        senderzz_me_log( 'wallet_refund.deferred_not_found', [ 'order_id' => $order_id ] );
        return;
    }

    // Só estorna se o pedido ainda estiver em cancelled ou refunded.
    if ( ! in_array( $order->get_status(), [ 'cancelled', 'refunded' ], true ) ) {
        senderzz_me_log( 'wallet_refund.deferred_wrong_status', [
            'order_id' => $order_id,
            'status'   => $order->get_status(),
        ] );
        return;
    }

    // Proteção contra duplo estorno.
    if ( $order->get_meta( '_senderzz_wallet_refunded' ) ) {
        senderzz_me_log( 'wallet_refund.deferred_already_done', [ 'order_id' => $order_id ] );
        return;
    }

    if ( function_exists( 'senderzz_wallet_refund_order' ) ) {
        senderzz_wallet_refund_order( $order_id, 'cancelled', 'deferred_refund_12h' );
        senderzz_me_log( 'wallet_refund.deferred_executed', [ 'order_id' => $order_id ] );
        $order = wc_get_order( $order_id );
        if ( $order ) {
            $order->add_order_note( 'Senderzz: estorno diferido de 12h executado — saldo devolvido à carteira do cliente.' );
            $order->save();
        }
    } else {
        senderzz_me_log( 'wallet_refund.deferred_fn_missing', [ 'order_id' => $order_id ] );
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// BUSCA DE PEDIDO POR META DO ME
// ─────────────────────────────────────────────────────────────────────────────

function senderzz_me_find_order_id( string $me_id = '', string $protocol = '' ): int {
    $queries = [];
    if ( $me_id !== '' ) {
        $queries[] = [ '_melhor_envio_item_id',    $me_id ];
        $queries[] = [ '_melhor_envio_shipment_id', $me_id ];
    }
    if ( $protocol !== '' ) {
        $queries[] = [ '_melhor_envio_order_id', $protocol ];
    }

    foreach ( $queries as $q ) {
        [ $key, $value ] = $q;
        $ids = wc_get_orders( [
            'type'       => 'shop_order',
            'limit'      => 1,
            'return'     => 'ids',
            'meta_key'   => $key,
            'meta_value' => $value,
            'orderby'    => 'date',
            'order'      => 'DESC',
        ] );
        if ( ! empty( $ids ) ) return (int) $ids[0];
    }

    return 0;
}

// ─────────────────────────────────────────────────────────────────────────────
// DOWNLOAD DE PDF DA ETIQUETA
// ─────────────────────────────────────────────────────────────────────────────

function senderzz_me_try_download_pdf_for_order( WC_Order $order ): bool {
    if ( ! class_exists( '\WC_MelhorEnvio\Api\Download_Label' ) ) return false;
    $path = (string) $order->get_meta( '_melhor_envio_pdf_local_path' );
    if ( $path && file_exists( $path ) && filesize( $path ) > 20 ) {
        $fh   = fopen( $path, 'rb' );
        $head = $fh ? fread( $fh, 4 ) : '';
        if ( $fh ) fclose( $fh );
        if ( $head === '%PDF' ) return true;
    }
    try {
        $download = new \WC_MelhorEnvio\Api\Download_Label();
        $download->add_additional_param( 'order', $order );
        $download->download( $order );
        $order = wc_get_order( $order->get_id() );
        $path  = $order ? (string) $order->get_meta( '_melhor_envio_pdf_local_path' ) : '';
        if ( $path && file_exists( $path ) && filesize( $path ) > 20 ) {
            $fh   = fopen( $path, 'rb' );
            $head = $fh ? fread( $fh, 4 ) : '';
            if ( $fh ) fclose( $fh );
            return $head === '%PDF';
        }
    } catch ( \Throwable $e ) {
        senderzz_me_log( 'pdf.download_failed', [ 'order_id' => $order->get_id(), 'error' => $e->getMessage() ] );
    }
    return false;
}

// ─────────────────────────────────────────────────────────────────────────────
// API GET — consulta item no cart do ME
// ─────────────────────────────────────────────────────────────────────────────

function senderzz_me_api_get_cart_item( string $item_id ): array {
    $settings = get_option( 'woocommerce_wc-melhor-envio_settings', [] );
    $token    = (string) ( $settings['client_secret'] ?? '' );
    if ( $token === '' ) throw new Exception( 'Token Melhor Envio ausente.' );
    $host = ( ( $settings['sandbox'] ?? 'no' ) === 'yes' ) ? 'sandbox.melhorenvio.com.br' : 'www.melhorenvio.com.br';
    $url  = 'https://' . $host . '/api/v2/me/cart/' . rawurlencode( $item_id );
    $response = wp_safe_remote_get( $url, [
        'timeout' => 30,
        'headers' => [
            'Accept'        => 'application/json',
            'Authorization' => 'Bearer ' . $token,
            'User-Agent'    => 'Senderzz Logistics (suporte@app.senderzz.com.br)',
        ],
    ] );
    if ( is_wp_error( $response ) ) throw new Exception( $response->get_error_message() );
    $code = (int) wp_remote_retrieve_response_code( $response );
    $body = (string) wp_remote_retrieve_body( $response );
    if ( ! in_array( $code, [ 200, 201 ], true ) ) throw new Exception( 'HTTP ' . $code . ': ' . substr( $body, 0, 160 ) );
    $data = json_decode( $body, true );
    return is_array( $data ) ? $data : [];
}

// ─────────────────────────────────────────────────────────────────────────────
// POLLING DE RECONCILIAÇÃO — a cada 5 minutos
// ─────────────────────────────────────────────────────────────────────────────

function senderzz_me_reconcile_shipments(): void {
    // Lock global: evita execução paralela do cron.
    $global_lock = 'senderzz_reconcile_lock';
    if ( get_transient( $global_lock ) ) {
        senderzz_me_log( 'polling.global_lock_active', 'Outra instância em andamento — pulando.' );
        return;
    }
    set_transient( $global_lock, 1, 4 * MINUTE_IN_SECONDS );

    try {
        $orders = wc_get_orders( [
            'type'    => 'shop_order',
            'status'  => [ 'aprovado', 'emretirada', 'coletado', 'enviado' ],
            'limit'   => 50,
            'orderby' => 'modified',
            'order'   => 'DESC',
        ] );

        foreach ( $orders as $order ) {
            if ( ! $order instanceof WC_Order ) continue;

            $item_id = (string) $order->get_meta( '_melhor_envio_item_id' );
            if ( $item_id === '' ) continue;

            // Pula etiquetas já finalizadas.
            $label_status = (string) $order->get_meta( '_melhor_envio_label_status' );
            if ( in_array( $label_status, [ 'cancelled', 'canceled', 'delivered' ], true ) ) continue;

            // Pula pedidos "zumbi" (5+ 404 consecutivos).
            $zombie_count = (int) $order->get_meta( '_senderzz_poll_zombie_count' );
            if ( $zombie_count >= 5 ) {
                senderzz_me_log( 'polling.zombie_skip', [ 'order_id' => $order->get_id(), 'zombie_count' => $zombie_count ] );
                continue;
            }

            // Cooldown por pedido (4 min).
            $last = (int) $order->get_meta( '_senderzz_me_last_poll_ts' );
            if ( $last && ( time() - $last ) < 240 ) continue;

            // Lock por pedido: evita race condition entre processos paralelos.
            $order_lock = 'senderzz_poll_lock_' . $order->get_id();
            if ( get_transient( $order_lock ) ) continue;
            set_transient( $order_lock, 1, 3 * MINUTE_IN_SECONDS );

            $order->update_meta_data( '_senderzz_me_last_poll_ts', time() );
            $order->save();

            try {
                $label_generated = (bool) $order->get_meta( '_melhor_envio_label_generated' );

                if ( $label_generated ) {
                    try {
                        $data   = senderzz_me_api_get_cart_item( $item_id );
                        $status = sanitize_key( (string) ( $data['status'] ?? '' ) );
                        $event  = $status ? 'order.' . ( $status === 'canceled' ? 'cancelled' : $status ) : 'order.updated';
                        senderzz_me_process_webhook_payload( [ 'event' => $event, 'data' => array_merge( $data, [ 'id' => $item_id ] ) ], 'polling' );
                        // Sucesso: reseta contador de zumbi.
                        if ( $zombie_count > 0 ) {
                            $order->update_meta_data( '_senderzz_poll_zombie_count', 0 );
                            $order->save();
                        }
                    } catch ( \Throwable $e ) {
                        if ( strpos( $e->getMessage(), '404' ) !== false ) {
                            $new_zombie = $zombie_count + 1;
                            $order->update_meta_data( '_senderzz_poll_zombie_count', $new_zombie );
                            $order->save();
                            senderzz_me_log( 'polling.skip_generated', [
                                'order_id'     => $order->get_id(),
                                'item_id'      => $item_id,
                                'zombie_count' => $new_zombie,
                            ] );
                        } else {
                            senderzz_me_log( 'polling.failed', [ 'order_id' => $order->get_id(), 'item_id' => $item_id, 'error' => $e->getMessage() ] );
                        }
                    }
                } else {
                    $data   = senderzz_me_api_get_cart_item( $item_id );
                    $status = sanitize_key( (string) ( $data['status'] ?? '' ) );
                    $event  = $status ? 'order.' . ( $status === 'canceled' ? 'cancelled' : $status ) : 'order.updated';
                    senderzz_me_process_webhook_payload( [ 'event' => $event, 'data' => array_merge( $data, [ 'id' => $item_id ] ) ], 'polling' );
                }
            } catch ( \Throwable $e ) {
                senderzz_me_log( 'polling.failed', [ 'order_id' => $order->get_id(), 'item_id' => $item_id, 'error' => $e->getMessage() ] );
            }
        }
    } finally {
        delete_transient( $global_lock );
    }
}
