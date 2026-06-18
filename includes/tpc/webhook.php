<?php
/**
 * webhook.php — Webhooks financeiros da Carteira de Frete
 * Segurança: assinatura obrigatória, idempotência persistente, PIX somente pago.
 */
defined( 'ABSPATH' ) || exit;
if ( defined( 'SENDERZZ_TPC_WEBHOOK_LOADED' ) ) return;
define( 'SENDERZZ_TPC_WEBHOOK_LOADED', true );

function tpc_webhook_ensure_secret(): string {
    $secret = (string) get_option( 'tpc_webhook_secret', '' );
    if ( strlen( $secret ) < 32 ) {
        $secret = wp_generate_password( 48, false );
        update_option( 'tpc_webhook_secret', $secret, false );
        update_option( 'tpc_webhook_secret_generated_at', current_time( 'mysql', true ), false );
        update_option( 'tpc_webhook_secret_admin_notice', 1, false );
    }
    return $secret;
}
add_action( 'admin_init', 'tpc_webhook_ensure_secret' );

add_action( 'admin_notices', function () {
    if ( ! current_user_can( 'manage_woocommerce' ) || ! get_option( 'tpc_webhook_secret_admin_notice' ) ) return;
    $dismiss_url = wp_nonce_url( add_query_arg( 'tpc_dismiss_secret_notice', '1' ), 'tpc_dismiss_secret_notice' );
    echo '<div class="notice notice-warning is-dismissible"><p><strong>Senderzz:</strong> um novo secret de webhook foi gerado. Configure no provedor e depois oculte/regenere se necessário. <a href="' . esc_url( $dismiss_url ) . '">Dispensar</a></p></div>';
} );

add_action( 'admin_init', function () {
    if (
        isset( $_GET['tpc_dismiss_secret_notice'] ) &&
        current_user_can( 'manage_woocommerce' ) &&
        wp_verify_nonce( sanitize_text_field( wp_unslash( $_GET['_wpnonce'] ?? '' ) ), 'tpc_dismiss_secret_notice' )
    ) {
        delete_option( 'tpc_webhook_secret_admin_notice' );
        wp_safe_redirect( remove_query_arg( [ 'tpc_dismiss_secret_notice', '_wpnonce' ] ) );
        exit;
    }
    // Auto-dismiss: se o secret já existe e tem mais de 7 dias, remove o aviso silenciosamente.
    if ( get_option( 'tpc_webhook_secret_admin_notice' ) ) {
        $generated_at = (string) get_option( 'tpc_webhook_secret_generated_at', '' );
        if ( $generated_at && ( time() - strtotime( $generated_at ) ) > 7 * DAY_IN_SECONDS ) {
            delete_option( 'tpc_webhook_secret_admin_notice' );
        }
    }
} );

add_action( 'rest_api_init', function () {
    register_rest_route( 'tp-carteira/v1', '/webhook/pix', [
        'methods'             => 'POST',
        'callback'            => 'tpc_webhook_pix_handler',
        'permission_callback' => '__return_true',
    ] );
    register_rest_route( 'tp-carteira/v1', '/webhook/envio', [
        'methods'             => 'POST',
        'callback'            => 'tpc_webhook_envio_handler',
        'permission_callback' => '__return_true',
    ] );
} );

function tpc_webhook_pix_handler( WP_REST_Request $request ): WP_REST_Response {
    $ip  = (string) ( $_SERVER['REMOTE_ADDR'] ?? 'x' );
    $rl  = 'tpc_pix_wh_rl_' . md5( $ip );
    $cnt = (int) get_transient( $rl );
    if ( $cnt >= 30 ) return new WP_REST_Response( [ 'ok' => false, 'note' => 'rate_limited' ], 429 );
    set_transient( $rl, $cnt + 1, MINUTE_IN_SECONDS );

    $raw     = (string) $request->get_body();
    $payload = $request->get_json_params() ?: [];
    tpc_log( 'webhook_pix', $payload );

    if ( ! tpc_validar_assinatura_webhook( $request ) ) {
        return new WP_REST_Response( [ 'error' => 'assinatura inválida' ], 401 );
    }

    $recarga_id = (int) ( $request->get_param( 'recarga_id' ) ?: ( $payload['metadata']['recarga_id'] ?? 0 ) );
    $me_pix_id  = sanitize_text_field( (string) ( $payload['id'] ?? ( $payload['payment']['id'] ?? ( $payload['payment_id'] ?? '' ) ) ) );
    $status     = sanitize_text_field( (string) ( $payload['status'] ?? ( $payload['payment']['status'] ?? '' ) ) );
    $valor_webhook = tpc_webhook_extract_amount( $payload );

    if ( ! $recarga_id && $me_pix_id ) {
        global $wpdb;
        $recarga_id = (int) $wpdb->get_var( $wpdb->prepare(
            "SELECT id FROM {$wpdb->prefix}tpc_recargas WHERE me_pix_id = %s LIMIT 1",
            $me_pix_id
        ) );
    }

    if ( ! $recarga_id ) {
        tpc_log( 'webhook_pix_error', [ 'error' => 'recarga_id e me_pix_id ausentes', 'payload' => $payload ] );
        return new WP_REST_Response( [ 'ok' => false, 'note' => 'sem recarga_id' ], 422 );
    }

    $recarga = function_exists( 'tpc_get_recarga' ) ? tpc_get_recarga( $recarga_id ) : null;
    if ( ! $recarga ) return new WP_REST_Response( [ 'ok' => false, 'note' => 'recarga não encontrada' ], 404 );

    $event_key = tpc_webhook_build_event_key( 'pix', $payload, [
        'recarga_id' => $recarga_id,
        'me_id'      => $me_pix_id ?: ( $recarga['me_pix_id'] ?? '' ),
        'status'     => $status,
        'raw'        => $raw,
    ] );
    if ( ! tpc_webhook_register_event( $event_key, 'pix', 'payment', $raw, [ 'recarga_id' => $recarga_id, 'me_id' => $me_pix_id, 'status' => $status ] ) ) {
        return new WP_REST_Response( [ 'ok' => true, 'note' => 'duplicate' ], 200 );
    }

    if ( ! $status ) {
        tpc_log( 'webhook_pix_ignored', [ 'recarga_id' => $recarga_id, 'reason' => 'status ausente' ] );
        return new WP_REST_Response( [ 'ok' => false, 'note' => 'status obrigatório para confirmar PIX' ], 422 );
    }

    if ( $status && tpc_pix_status_is_cancelled( $status ) ) {
        tpc_cancelar_recarga( $recarga_id );
        return new WP_REST_Response( [ 'ok' => true, 'note' => 'cancelada' ], 200 );
    }

    if ( tpc_pix_status_is_analysis( $status, $payload ) ) {
        tpc_marcar_recarga_analise( $recarga_id, [ 'status_me' => $status ] );
        return new WP_REST_Response( [ 'ok' => true, 'note' => 'analise' ], 200 );
    }

    if ( ! tpc_pix_status_is_paid( $status ) ) {
        return new WP_REST_Response( [ 'ok' => false, 'note' => 'status não confirma pagamento' ], 422 );
    }

    if ( $valor_webhook !== null && abs( $valor_webhook - (float) $recarga['valor'] ) > 0.01 ) {
        tpc_log( 'webhook_pix_amount_mismatch', [ 'recarga_id' => $recarga_id, 'esperado' => $recarga['valor'], 'recebido' => $valor_webhook ] );
        return new WP_REST_Response( [ 'ok' => false, 'note' => 'valor divergente' ], 409 );
    }

    if ( $me_pix_id && ! empty( $recarga['me_pix_id'] ) && ! hash_equals( (string) $recarga['me_pix_id'], $me_pix_id ) ) {
        return new WP_REST_Response( [ 'ok' => false, 'note' => 'me_pix_id divergente' ], 409 );
    }

    // Guard agora embutido em tpc_confirmar_recarga() — 'webhook' é origem autorizada.
    $ok = tpc_confirmar_recarga( $recarga_id, 'webhook' );
    return new WP_REST_Response( [ 'ok' => $ok, 'recarga_id' => $recarga_id ], $ok ? 200 : 500 );
}

function tpc_webhook_envio_handler( WP_REST_Request $request ): WP_REST_Response {
    $raw     = (string) $request->get_body();
    $payload = $request->get_json_params() ?: [];
    tpc_log( 'webhook_envio', $payload );
    if ( ! tpc_validar_assinatura_webhook( $request ) ) return new WP_REST_Response( [ 'error' => 'assinatura inválida' ], 401 );

    $user_id  = (int) ( $payload['metadata']['user_id'] ?? 0 );
    $valor    = (float) ( $payload['price'] ?? 0 );
    $me_id    = sanitize_text_field( (string) ( $payload['id'] ?? '' ) );
    $order_id = (int) ( $payload['metadata']['wc_order_id'] ?? 0 );
    $servico  = sanitize_text_field( (string) ( $payload['service']['name'] ?? 'Frete' ) );
    $status   = sanitize_text_field( (string) ( $payload['status'] ?? '' ) );

    if ( ! $user_id || $valor <= 0 || ! $me_id ) return new WP_REST_Response( [ 'error' => 'dados insuficientes' ], 400 );

    $event_key = tpc_webhook_build_event_key( 'envio', $payload, [ 'order_id' => $order_id, 'me_id' => $me_id, 'status' => $status, 'raw' => $raw ] );
    if ( ! tpc_webhook_register_event( $event_key, 'envio', 'shipment', $raw, [ 'order_id' => $order_id, 'me_id' => $me_id, 'status' => $status ] ) ) {
        return new WP_REST_Response( [ 'ok' => true, 'note' => 'duplicate' ], 200 );
    }

    $tx = tpc_debitar( $user_id, $valor, "Frete $servico" . ( $order_id ? " - Pedido #$order_id" : '' ), [
        'me_order_id' => $me_id, 'order_id' => $order_id ?: null, 'referencia' => 'frete_gerado',
    ] );
    if ( ! $tx ) return new WP_REST_Response( [ 'error' => 'saldo insuficiente' ], 402 );
    return new WP_REST_Response( [ 'ok' => true, 'transacao_id' => $tx ] );
}

function tpc_validar_assinatura_webhook( WP_REST_Request $request ): bool {
    $secret = (string) get_option( 'tpc_webhook_secret', '' );
    if ( strlen( $secret ) < 32 ) {
        tpc_log( 'webhook_no_secret', [ 'ip' => $_SERVER['REMOTE_ADDR'] ?? '', 'path' => $_SERVER['REQUEST_URI'] ?? '' ] );
        return false;
    }
    $sig = (string) ( $request->get_header( 'x-signature' ) ?: $request->get_header( 'x-hub-signature' ) ?: '' );
    $expected = 'sha256=' . hash_hmac( 'sha256', $request->get_body(), $secret );
    return $sig && hash_equals( $expected, $sig );
}

function tpc_webhook_extract_amount( array $payload ): ?float {
    foreach ( [ 'amount', 'value', 'price', 'total', 'paid_amount' ] as $k ) {
        if ( isset( $payload[ $k ] ) && is_numeric( $payload[ $k ] ) ) return (float) $payload[ $k ];
    }
    foreach ( [ 'payment', 'data' ] as $parent ) {
        if ( isset( $payload[ $parent ] ) && is_array( $payload[ $parent ] ) ) {
            $v = tpc_webhook_extract_amount( $payload[ $parent ] );
            if ( $v !== null ) return $v;
        }
    }
    return null;
}

function tpc_webhook_build_event_key( string $source, array $payload, array $fallback ): string {
    $explicit = (string) ( $payload['event_id'] ?? $payload['event']['id'] ?? $payload['uuid'] ?? '' );
    if ( $explicit ) return $source . ':' . sanitize_key( $explicit );
    $parts = [ $source, (string) ( $fallback['me_id'] ?? '' ), (string) ( $fallback['order_id'] ?? '' ), (string) ( $fallback['recarga_id'] ?? '' ), (string) ( $fallback['status'] ?? '' ) ];
    $base = implode( '|', array_filter( $parts, static fn( $v ) => $v !== '' ) );
    if ( strlen( $base ) > strlen( $source ) + 1 ) return substr( hash( 'sha256', $base ), 0, 48 );
    return hash( 'sha256', (string) ( $fallback['raw'] ?? wp_json_encode( $payload ) ) );
}

function tpc_webhook_register_event( string $event_key, string $source, string $event_type, string $raw, array $meta = [] ): bool {
    global $wpdb;
    $table = $wpdb->prefix . 'tpc_webhook_events';
    $hash  = hash( 'sha256', $raw );
    $ok = $wpdb->insert( $table, [
        'event_key'    => $event_key,
        'source'       => $source,
        'event_type'   => $event_type,
        'order_id'     => isset( $meta['order_id'] ) ? absint( $meta['order_id'] ) : null,
        'recarga_id'   => isset( $meta['recarga_id'] ) ? absint( $meta['recarga_id'] ) : null,
        'me_id'        => isset( $meta['me_id'] ) ? sanitize_text_field( (string) $meta['me_id'] ) : null,
        'status'       => isset( $meta['status'] ) ? sanitize_text_field( (string) $meta['status'] ) : null,
        'payload_hash' => $hash,
    ], [ '%s','%s','%s','%d','%d','%s','%s','%s' ] );
    if ( $ok ) return true;
    if ( str_contains( strtolower( (string) $wpdb->last_error ), 'duplicate' ) ) return false;
    // Fallback: se tabela ainda não existe em instalação antiga, usa transient mas loga falha.
    tpc_log( 'webhook_event_table_error', [ 'db' => $wpdb->last_error, 'event_key' => $event_key ] );
    $transient = 'tpc_wh_evt_' . md5( $event_key );
    if ( get_transient( $transient ) ) return false;
    set_transient( $transient, 1, DAY_IN_SECONDS );
    return true;
}

function tpc_log( string $tipo, mixed $data ): void {
    $data = tpc_mask_sensitive_log_data( $data );
    if ( function_exists( 'wc_get_logger' ) ) {
        wc_get_logger()->info( wp_json_encode( $data, JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES ), [ 'source' => 'tp-carteira-' . $tipo ] );
    }
    // S7: payloads PIX não persistem em wp_options (vaza em backups). WC logger cobre arquivo.
    // Apenas timestamp sem dados: admin pode ver "último evento" sem expor payload.
    update_option( 'tpc_last_' . sanitize_key( $tipo ), [ 'created_at' => current_time( 'mysql' ) ], false );
}

function tpc_mask_sensitive_log_data( mixed $data ): mixed {
    if ( is_array( $data ) ) {
        $masked = [];
        foreach ( $data as $k => $v ) {
            $lk = strtolower( (string) $k );
            if ( preg_match( '/(token|secret|senha|password|authorization|document|cpf|cnpj|phone|telefone|email|address|endereco|cep)/', $lk ) ) {
                $masked[ $k ] = is_scalar( $v ) ? substr( hash( 'sha256', (string) $v ), 0, 12 ) . '***' : '[masked]';
            } else {
                $masked[ $k ] = tpc_mask_sensitive_log_data( $v );
            }
        }
        return $masked;
    }
    return $data;
}
