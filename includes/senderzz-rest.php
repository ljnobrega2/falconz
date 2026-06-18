<?php
/**
 * Senderzz REST API para painel externo.
 */
defined( 'ABSPATH' ) || exit;
if ( defined( 'SENDERZZ_SENDERZZ_REST_LOADED' ) ) return;
define( 'SENDERZZ_SENDERZZ_REST_LOADED', true );

add_action( 'rest_api_init', function() {
    register_rest_route( 'senderzz/v1', '/dashboard', [ 'methods' => 'GET', 'callback' => 'senderzz_rest_dashboard', 'permission_callback' => 'senderzz_rest_permission' ] );
    register_rest_route( 'senderzz/v1', '/orders', [ 'methods' => 'GET', 'callback' => 'senderzz_rest_orders', 'permission_callback' => 'senderzz_rest_permission' ] );
    register_rest_route( 'senderzz/v1', '/orders/(?P<order_id>\d+)', [ 'methods' => 'GET', 'callback' => 'senderzz_rest_order_detail', 'permission_callback' => 'senderzz_rest_permission' ] );
    register_rest_route( 'senderzz/v1', '/orders/(?P<order_id>\d+)/status', [ 'methods' => 'POST', 'callback' => 'senderzz_rest_update_order_status', 'permission_callback' => 'senderzz_rest_permission' ] );
    register_rest_route( 'senderzz/v1', '/reports', [ 'methods' => 'GET', 'callback' => 'senderzz_rest_reports', 'permission_callback' => 'senderzz_rest_permission' ] );
    register_rest_route( 'senderzz/v1', '/wallet', [ 'methods' => 'GET', 'callback' => 'senderzz_rest_wallet', 'permission_callback' => 'senderzz_rest_permission' ] );
    register_rest_route( 'senderzz/v1', '/transactions', [ 'methods' => 'GET', 'callback' => 'senderzz_rest_transactions', 'permission_callback' => 'senderzz_rest_permission' ] );
} );

function senderzz_rest_permission() {
    if ( is_user_logged_in() ) return true;
    if ( function_exists( 'tpc_rest_auth' ) ) {
        $auth = tpc_rest_auth();
        return is_wp_error( $auth ) ? $auth : true;
    }
    return new WP_Error( 'unauthorized', 'Login necessário.', [ 'status' => 401 ] );
}

function senderzz_rest_user_id_from_request( WP_REST_Request $request ): int {
    if ( current_user_can( 'manage_woocommerce' ) && $request->get_param( 'user_id' ) ) return absint( $request->get_param( 'user_id' ) );
    if ( function_exists( 'tpc_rest_auth' ) ) { $auth = tpc_rest_auth(); if ( ! is_wp_error( $auth ) ) return (int) $auth; }
    return get_current_user_id();
}

function senderzz_rest_owned_shipping_class_ids( int $user_id ): array {
    $map = get_option( 'senderzz_shipping_class_wallet_owners', [] );
    if ( ! is_array( $map ) ) return [];

    $ids = [];
    foreach ( $map as $class_id => $owner_id ) {
        if ( absint( $owner_id ) === $user_id ) {
            $ids[] = absint( $class_id );
        }
    }
    return array_values( array_unique( array_filter( $ids, fn( $id ) => $id >= 0 ) ) );
}

function senderzz_rest_order_owner_user_id( WC_Order $order ): int {
    $meta_owner = absint( $order->get_meta( '_senderzz_owner_user_id' ) );
    if ( $meta_owner ) return $meta_owner;

    if ( function_exists( 'senderzz_get_order_wallet_owner_id' ) ) {
        try {
            $owner = absint( senderzz_get_order_wallet_owner_id( $order ) );
            if ( $owner ) return $owner;
        } catch ( Throwable $e ) {}
    }

    $class_id = absint( $order->get_meta( '_senderzz_product_shipping_class_id' ) );
    if ( ! $class_id ) {
        foreach ( $order->get_items( 'line_item' ) as $item ) {
            $product = $item instanceof WC_Order_Item_Product ? $item->get_product() : null;
            if ( $product ) {
                $class_id = absint( $product->get_shipping_class_id() );
                if ( $class_id ) break;
            }
        }
    }

    if ( $class_id ) {
        $map = get_option( 'senderzz_shipping_class_wallet_owners', [] );
        if ( is_array( $map ) ) {
            if ( isset( $map[ (string) $class_id ] ) ) return absint( $map[ (string) $class_id ] );
            if ( isset( $map[ $class_id ] ) ) return absint( $map[ $class_id ] );
        }
    }

    return (int) $order->get_customer_id();
}

function senderzz_rest_orders_query_args( WP_REST_Request $request ): array {
    $user_id = senderzz_rest_user_id_from_request( $request );
    $args = [
        'limit'   => min( 500, max( 1, absint( $request->get_param( 'limit' ) ?: 50 ) ) ),
        'return'  => 'objects',
        'orderby' => 'date',
        'order'   => 'DESC',
    ];

    if ( ! current_user_can( 'manage_woocommerce' ) ) {
        $class_ids = senderzz_rest_owned_shipping_class_ids( $user_id );
        if ( $class_ids ) {
            $args['meta_query'] = [
                'relation' => 'OR',
                [ 'key' => '_senderzz_owner_user_id', 'value' => $user_id, 'compare' => '=' ],
                [ 'key' => '_senderzz_product_shipping_class_id', 'value' => $class_ids, 'compare' => 'IN' ],
            ];
        } else {
            $args['customer_id'] = $user_id;
        }
    } elseif ( $user_id && $request->get_param( 'user_id' ) ) {
        $args['customer_id'] = $user_id;
    }

    if ( $status = $request->get_param( 'status' ) ) $args['status'] = sanitize_text_field( $status );
    $from = $request->get_param( 'from' ); $to = $request->get_param( 'to' );
    if ( $from && $to ) $args['date_created'] = sanitize_text_field( $from ) . '...' . sanitize_text_field( $to );
    elseif ( $from ) $args['date_created'] = '>=' . sanitize_text_field( $from );
    elseif ( $to ) $args['date_created'] = '<=' . sanitize_text_field( $to );
    return $args;
}

function senderzz_rest_can_view_order( WC_Order $order ): bool {
    if ( current_user_can( 'manage_woocommerce' ) ) return true;
    $current_user_id = get_current_user_id();
    if ( ! $current_user_id && function_exists( 'tpc_rest_auth' ) ) {
        $auth = tpc_rest_auth();
        if ( ! is_wp_error( $auth ) ) $current_user_id = (int) $auth;
    }
    return $current_user_id > 0 && senderzz_rest_order_owner_user_id( $order ) === $current_user_id;
}

function senderzz_format_order_for_panel( WC_Order $order ): array {
    $snap = function_exists( 'senderzz_sync_order_shipping_meta' ) ? senderzz_sync_order_shipping_meta( $order ) : ( function_exists( 'senderzz_order_shipping_snapshot' ) ? senderzz_order_shipping_snapshot( $order ) : [] );
    $data = [
        'order_id' => $order->get_id(), 'number' => $order->get_order_number(), 'date' => $order->get_date_created() ? $order->get_date_created()->date( 'Y-m-d H:i:s' ) : '', 'status' => $order->get_status(),
        'customer_id' => (int) $order->get_customer_id(), 'customer_name' => trim( $order->get_formatted_billing_full_name() ), 'customer_email' => $order->get_billing_email(),
        'total' => (float) $order->get_total(), 'shipping_total' => (float) $order->get_shipping_total(),
        'carrier' => $snap['carrier_name'] ?? '', 'service_id' => $snap['service_id'] ?? '', 'service_name' => $snap['service_name'] ?? '', 'delivery_time' => $snap['delivery_time'] ?? '',
        'shipping_real_cost' => (float) ( $snap['shipping_real_cost'] ?? 0 ), 'shipping_charged' => (float) ( $snap['shipping_charged'] ?? 0 ), 'service_fee' => (float) ( $snap['service_fee'] ?? 0 ), 'margin' => (float) ( $snap['margin'] ?? 0 ),
        'uf' => $snap['destination_uf'] ?? '', 'city' => $snap['destination_city'] ?? '', 'postcode' => $snap['destination_postcode'] ?? '', 'postcode_prefix' => $snap['postcode_prefix'] ?? '',
        'tracking' => $order->get_meta( '_melhor_envio_tracking_codes' ), 'label_status' => $order->get_meta( '_melhor_envio_label_status' ), 'label_error' => $order->get_meta( '_melhor_envio_label_error' ),
        'me_item_id' => $order->get_meta( '_melhor_envio_item_id' ), 'me_protocol' => $order->get_meta( '_melhor_envio_order_id' ), 'print_url' => $order->get_meta( '_melhor_envio_print_url' ), 'pdf_url' => $order->get_meta( '_melhor_envio_pdf_local_url' ),
        'wallet_reserved' => (bool) $order->get_meta( '_senderzz_wallet_reserved' ), 'wallet_debited' => (bool) $order->get_meta( '_senderzz_wallet_debited' ), 'wallet_refunded' => (bool) $order->get_meta( '_senderzz_wallet_refunded' ),
        'wallet_reserve_tx' => (int) $order->get_meta( '_senderzz_wallet_reserve_tx' ), 'wallet_debit_value' => (float) $order->get_meta( '_senderzz_wallet_debit_value' ),
    ];

    // Espelho COD Motoboy: a operação Motoboy é isolada da Expedição/ME, mas o
    // portal do produtor precisa ler o status real da tabela operacional.
    global $wpdb;
    $mb_table = $wpdb->prefix . 'sz_motoboy_pedidos';
    if ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $mb_table ) ) ) {
        $mb = $wpdb->get_row( $wpdb->prepare(
            "SELECT status, ts_aprovado, ts_embalado, ts_em_rota, ts_a_caminho, ts_entregue, ts_frustrado, created_at, updated_at
               FROM {$mb_table}
              WHERE wc_order_id = %d
              ORDER BY id DESC
              LIMIT 1",
            $order->get_id()
        ), ARRAY_A );
        if ( $mb ) {
            $mb_status = sanitize_key( (string) ( $mb['status'] ?? '' ) );
            if ( $mb_status === 'aprovado' ) $mb_status = 'agendado';
            if ( $mb_status !== '' ) {
                $data['status'] = $mb_status;
                $data['motoboy_status'] = $mb_status;
                $data['delivery_mode'] = 'motoboy';
                $data['is_motoboy'] = true;
            }
            $map = [
                'agendado'  => $mb['created_at'] ?? '',
                'embalado'  => $mb['ts_embalado'] ?? '',
                'em_rota'   => $mb['ts_em_rota'] ?? '',
                'a_caminho' => $mb['ts_a_caminho'] ?? '',
                'entregue'  => $mb['ts_entregue'] ?? '',
                'frustrado' => $mb['ts_frustrado'] ?? '',
            ];
            foreach ( $map as $k => $v ) {
                $data[ 'motoboy_' . $k . '_at' ] = $v ? sz_motoboy_format_br_datetime( $v ) : '';
            }
            $last = '';
            foreach ( [ 'frustrado','entregue','a_caminho','em_rota','embalado','agendado' ] as $k ) {
                if ( ! empty( $data[ 'motoboy_' . $k . '_at' ] ) ) { $last = $data[ 'motoboy_' . $k . '_at' ]; break; }
            }
            if ( $last !== '' ) $data['date_updated'] = $last;
        }
    }

    return $data;
}
function senderzz_rest_orders( WP_REST_Request $request ): WP_REST_Response {
    $query_args = senderzz_rest_orders_query_args( $request );
    $orders = wc_get_orders( $query_args );

    // Compatibilidade: pedidos antigos/visitantes podem ainda não ter os metas
    // _senderzz_owner_user_id/_senderzz_product_shipping_class_id sincronizados.
    // Se a busca por meta não voltar nada para cliente logístico, busca o lote recente
    // e aplica filtro seguro em PHP por classe/carteira antes de responder.
    if ( ! current_user_can( 'manage_woocommerce' ) && empty( $orders ) && ! empty( $query_args['meta_query'] ) ) {
        $fallback_args = $query_args;
        unset( $fallback_args['meta_query'], $fallback_args['customer_id'] );
        $fallback_args['limit'] = min( (int) ( $fallback_args['limit'] ?? 50 ), 100 );
        $orders = wc_get_orders( $fallback_args );
    }

    $orders = array_values( array_filter( $orders, function( $order ) {
        if ( ! senderzz_rest_can_view_order( $order ) ) return false;
        if ( function_exists( 'senderzz_force_delete_if_draft_order' ) && senderzz_force_delete_if_draft_order( $order ) ) return false;
        return ! ( function_exists( 'senderzz_is_draft_order_status' ) && senderzz_is_draft_order_status( $order->get_status() ) );
    } ) );
    $data = array_map( 'senderzz_format_order_for_panel', $orders );
    return new WP_REST_Response( [ 'data' => $data, 'total' => count( $data ) ] );
}

function senderzz_rest_order_detail( WP_REST_Request $request ): WP_REST_Response {
    $order = wc_get_order( absint( $request['order_id'] ) );
    if ( $order && function_exists( 'senderzz_force_delete_if_draft_order' ) && senderzz_force_delete_if_draft_order( $order ) ) {
        return new WP_REST_Response( [ 'error' => 'Pedido não encontrado.' ], 404 );
    }
    if ( ! $order || ! senderzz_rest_can_view_order( $order ) ) return new WP_REST_Response( [ 'error' => 'Pedido não encontrado.' ], 404 );
    $data = senderzz_format_order_for_panel( $order );
    $data['items'] = [];
    foreach ( $order->get_items() as $item ) $data['items'][] = [ 'name' => ( function_exists('senderzz_clean_product_label') ? senderzz_clean_product_label( $item->get_name() ) : $item->get_name() ), 'qty' => $item->get_quantity(), 'total' => (float) $item->get_total() ];
    return new WP_REST_Response( $data );
}

function senderzz_rest_update_order_status( WP_REST_Request $request ): WP_REST_Response {
    $order = wc_get_order( absint( $request['order_id'] ) );
    if ( ! $order || ! senderzz_rest_can_view_order( $order ) ) return new WP_REST_Response( [ 'error' => 'Pedido não encontrado.' ], 404 );
    $status = sanitize_key( $request->get_param( 'status' ) );
    if ( ! $status ) return new WP_REST_Response( [ 'error' => 'Status obrigatório.' ], 400 );

    $is_admin = current_user_can( 'manage_woocommerce' );
    $is_owner = senderzz_rest_can_view_order( $order );

    if ( ! $is_admin && ! $is_owner ) {
        return new WP_REST_Response( [ 'error' => 'Sem permissão.' ], 403 );
    }

    // CRIT-02: cliente só pode realizar transições explicitamente autorizadas.
    // Admin (manage_woocommerce) continua livre, como antes.
    if ( ! $is_admin ) {
        $current = $order->get_status();

        // CRIT-02: cliente só pode solicitar cancelamento de pedidos ainda não despachados.
        // Pedidos em aprovado/approved/erro já iniciaram processamento — só admin pode alterar.
        // Referência: CHANGELOG-SECURITY.md CRIT-02.
        $allowed = [
            'pending'        => [ 'emcancelamento' ],
            'on-hold'        => [ 'emcancelamento' ],
            'pendente'       => [ 'emcancelamento' ],
            'aguardando'     => [ 'emcancelamento' ],
            // emcancelamento: nenhuma transição permitida ao cliente
            'emcancelamento' => [],
        ];
        $permitted = $allowed[ $current ] ?? [];

        if ( ! in_array( $status, $permitted, true ) ) {
            return new WP_REST_Response( [
                'error'   => 'Transição de status não permitida para este pedido.',
                'current' => $current,
                'allowed' => $permitted,
            ], 403 );
        }
    }

    $order->update_status( $status, 'Status alterado pelo painel Senderzz.' );
    senderzz_sync_order_shipping_meta( $order );
    return new WP_REST_Response( senderzz_format_order_for_panel( $order ) );
}

function senderzz_rest_wallet( WP_REST_Request $request ): WP_REST_Response {
    $user_id = senderzz_rest_user_id_from_request( $request );
    return new WP_REST_Response( [
        'user_id' => $user_id,
        'saldo' => function_exists( 'tpc_get_saldo' ) ? tpc_get_saldo( $user_id ) : 0,
        'saldo_reservado' => function_exists( 'tpc_get_saldo_reservado' ) ? tpc_get_saldo_reservado( $user_id ) : 0,
        'saldo_disponivel' => function_exists( 'tpc_get_saldo_disponivel' ) ? tpc_get_saldo_disponivel( $user_id ) : 0,
        'admin_alocado' => function_exists( 'tpc_admin_get_admin_allocated_balance' ) ? tpc_admin_get_admin_allocated_balance( $user_id ) : 0,
        'saldo_usuario_proprio' => function_exists( 'tpc_admin_get_user_wallet_breakdown' ) ? tpc_admin_get_user_wallet_breakdown( $user_id )['usuario_proprio'] : 0,
    ] );
}

function senderzz_rest_transactions( WP_REST_Request $request ): WP_REST_Response {
    $user_id = senderzz_rest_user_id_from_request( $request );
    $args = [ 'tipo' => $request->get_param( 'tipo' ), 'status' => $request->get_param( 'status' ), 'data_ini' => $request->get_param( 'from' ), 'data_fim' => $request->get_param( 'to' ), 'per_page' => $request->get_param( 'limit' ) ?: 50, 'page' => $request->get_param( 'page' ) ?: 1 ];
    return new WP_REST_Response( [ 'data' => function_exists( 'tpc_get_transacoes' ) ? tpc_get_transacoes( $user_id, $args ) : [], 'user_id' => $user_id ] );
}

function senderzz_rest_dashboard( WP_REST_Request $request ): WP_REST_Response {
    $user_id = senderzz_rest_user_id_from_request( $request );
    $orders = wc_get_orders( senderzz_rest_orders_query_args( $request ) );
    $saldo = function_exists( 'tpc_get_saldo' ) ? tpc_get_saldo( $user_id ) : 0;
    $reserved = function_exists( 'tpc_get_saldo_reservado' ) ? tpc_get_saldo_reservado( $user_id ) : 0;
    $available = function_exists( 'tpc_get_saldo_disponivel' ) ? tpc_get_saldo_disponivel( $user_id ) : $saldo;
    $spent = 0; $real = 0; $margin = 0;
    foreach ( $orders as $order ) { $s = senderzz_order_shipping_snapshot( $order ); $spent += (float) $s['shipping_charged']; $real += (float) $s['shipping_real_cost']; $margin += (float) $s['margin']; }
    return new WP_REST_Response( [ 'user_id' => $user_id, 'saldo' => $saldo, 'saldo_reservado' => $reserved, 'saldo_disponivel' => $available, 'orders_count' => count( $orders ), 'shipping_spent' => round( $spent, 2 ), 'shipping_real_cost' => round( $real, 2 ), 'margin' => round( $margin, 2 ) ] );
}

function senderzz_report_add( array &$bucket, string $key, array $s ): void {
    if ( ! isset( $bucket[ $key ] ) ) $bucket[ $key ] = [ 'count' => 0, 'charged' => 0, 'real' => 0, 'margin' => 0 ];
    $bucket[ $key ]['count']++;
    $bucket[ $key ]['charged'] = round( $bucket[ $key ]['charged'] + (float) $s['shipping_charged'], 2 );
    $bucket[ $key ]['real'] = round( $bucket[ $key ]['real'] + (float) $s['shipping_real_cost'], 2 );
    $bucket[ $key ]['margin'] = round( $bucket[ $key ]['margin'] + (float) $s['margin'], 2 );
}

function senderzz_rest_reports( WP_REST_Request $request ): WP_REST_Response {
    $orders = wc_get_orders( senderzz_rest_orders_query_args( $request ) );
    $by_carrier = []; $by_uf = []; $by_city = []; $by_prefix = []; $by_customer = []; $by_status = [];
    foreach ( $orders as $order ) {
        $s = senderzz_order_shipping_snapshot( $order );
        senderzz_report_add( $by_carrier, $s['carrier_name'] ?: 'Indefinido', $s );
        senderzz_report_add( $by_uf, $s['destination_uf'] ?: 'NA', $s );
        senderzz_report_add( $by_city, $s['destination_city'] ?: 'NA', $s );
        senderzz_report_add( $by_prefix, $s['postcode_prefix'] ?: 'NA', $s );
        senderzz_report_add( $by_customer, (string) ( $order->get_customer_id() ?: 0 ), $s );
        senderzz_report_add( $by_status, $order->get_status(), $s );
    }
    return new WP_REST_Response( [ 'by_carrier' => $by_carrier, 'by_uf' => $by_uf, 'by_city' => $by_city, 'by_postcode_prefix' => $by_prefix, 'by_customer' => $by_customer, 'by_status' => $by_status ] );
}

// ── sz-portal/v1: bulk-cancel, reschedule, wallet history/future ──────────────

add_action( 'rest_api_init', function () {
    register_rest_route( 'sz-portal/v1', '/motoboy/bulk-cancel', [ 'methods' => 'POST', 'callback' => 'sz_rest_motoboy_bulk_cancel', 'permission_callback' => '__return_true' ] );
    register_rest_route( 'sz-portal/v1', '/motoboy/reschedule',  [ 'methods' => 'POST', 'callback' => 'sz_rest_motoboy_reschedule',  'permission_callback' => '__return_true' ] );
    register_rest_route( 'sz-portal/v1', '/wallet/history',      [ 'methods' => 'GET',  'callback' => 'sz_rest_wallet_history',       'permission_callback' => '__return_true' ] );
    register_rest_route( 'sz-portal/v1', '/wallet/future',       [ 'methods' => 'GET',  'callback' => 'sz_rest_wallet_future',        'permission_callback' => '__return_true' ] );
    register_rest_route( 'sz-portal/v1', '/wallet/export-csv',   [ 'methods' => 'GET',  'callback' => 'sz_rest_wallet_export_csv',    'permission_callback' => '__return_true' ] );
    register_rest_route( 'sz-portal/v1', '/pix/confirm',         [ 'methods' => 'POST', 'callback' => 'sz_rest_pix_confirm',          'permission_callback' => '__return_true' ] );
} );

function sz_portal_auth_user(): ?object {
    if ( class_exists( '\\WC_MelhorEnvio\\Portal\\Portal_Auth' ) ) {
        $u = \WC_MelhorEnvio\Portal\Portal_Auth::get_current_user();
        if ( $u ) return $u;
    }
    if ( function_exists( 'senderzz_portal_get_current_user' ) ) {
        $u = senderzz_portal_get_current_user();
        if ( $u ) return $u;
    }
    return null;
}

// ── sz-portal/v2: camada segura para futuras ações financeiras ────────────────
//
// Todas as rotas V2 passam por sz_portal_v2_financial_permission_check():
//   1. Sessão do portal válida              → erro 401
//   2. X-WP-Nonce válido (wp_rest)          → erro 403 (CSRF camada WP)
//   3. X-Senderzz-Financial-Nonce válido    → erro 403 (CSRF vinculado à sessão)
//   4. Usuário não é afiliado               → erro 403
//   5. Usuário não é sub-user               → erro 403
//   6. Feature flag específica              → erro 403 (se exigida pela rota)
//
// V1 (/sz-portal/v1) permanece intacta — não alterada aqui.
// ─────────────────────────────────────────────────────────────────────────────

// ── Helpers: nonce financeiro vinculado à sessão do portal ───────────────────

/**
 * Retorna o hash seguro do token de sessão do portal atualmente ativo.
 *
 * Lê o cookie senderzz_portal_session (HttpOnly — só disponível no servidor)
 * e aplica o mesmo hash_hmac(sha256, token, AUTH_SALT) usado por Portal_Auth.
 * Retorna '' se não houver cookie válido.
 */
function sz_portal_v2_get_session_token_hash(): string {
    $cookie_name = class_exists( \WC_MelhorEnvio\Portal\Portal_Auth::class )
        && defined( \WC_MelhorEnvio\Portal\Portal_Auth::class . '::COOKIE_NAME' )
        ? \WC_MelhorEnvio\Portal\Portal_Auth::COOKIE_NAME
        : 'senderzz_portal_session';

    $raw = sanitize_text_field( (string) ( $_COOKIE[ $cookie_name ] ?? '' ) );
    if ( $raw === '' ) {
        return '';
    }

    // Reutiliza Portal_Auth::hash_session_token se disponível; fallback idêntico.
    if ( class_exists( '\\WC_MelhorEnvio\\Portal\\Portal_Auth' ) && method_exists( '\\WC_MelhorEnvio\\Portal\\Portal_Auth', 'hash_session_token' ) ) {
        return \WC_MelhorEnvio\Portal\Portal_Auth::hash_session_token( $raw );
    }

    // Fallback: replicar exatamente o mesmo algoritmo de Portal_Auth.
    $salt = defined( 'AUTH_SALT' ) && AUTH_SALT ? AUTH_SALT : wp_salt( 'auth' );
    return hash_hmac( 'sha256', $raw, $salt );
}

/**
 * Retorna a action string para o nonce financeiro.
 *
 * Formato: sz_financial_{portal_user_id}_{session_hash}
 * — Amarrada ao ID do usuário do portal (não do WP user).
 * — Amarrada ao hash da sessão ativa.
 * — Qualquer troca de sessão (logout/login) invalida nonces anteriores.
 * — Qualquer troca de usuário invalida nonces anteriores.
 *
 * @param object $portal_user  Objeto retornado por sz_portal_auth_user().
 * @param string $session_hash Hash da sessão (de sz_portal_v2_get_session_token_hash()).
 */
function sz_portal_v2_financial_nonce_action( object $portal_user, string $session_hash ): string {
    $uid = (int) ( $portal_user->id ?? 0 );
    // Usa somente os primeiros 16 chars do hash — suficiente para unicidade
    // e evita expor o hash completo na action string.
    $short_hash = substr( $session_hash, 0, 16 );
    return 'sz_financial_' . $uid . '_' . $short_hash;
}

/**
 * Gera o nonce financeiro para o usuário e sessão atuais.
 * Retorna '' se não for possível determinar a sessão.
 *
 * @param object $portal_user  Objeto retornado por sz_portal_auth_user().
 */
function sz_portal_v2_create_financial_nonce( object $portal_user ): string {
    $session_hash = sz_portal_v2_get_session_token_hash();
    if ( $session_hash === '' ) {
        return '';
    }
    return wp_create_nonce( sz_portal_v2_financial_nonce_action( $portal_user, $session_hash ) );
}

/**
 * Verifica o nonce financeiro enviado no header X-Senderzz-Financial-Nonce.
 *
 * @param WP_REST_Request $request      Requisição REST.
 * @param object          $portal_user  Usuário do portal (já validado).
 * @return bool  true se válido.
 */
function sz_portal_v2_verify_financial_nonce( WP_REST_Request $request, object $portal_user ): bool {
    $nonce = (string) ( $request->get_header( 'X-Senderzz-Financial-Nonce' ) ?? '' );
    if ( $nonce === '' ) {
        return false;
    }

    $session_hash = sz_portal_v2_get_session_token_hash();
    if ( $session_hash === '' ) {
        return false;
    }

    $action = sz_portal_v2_financial_nonce_action( $portal_user, $session_hash );
    return (bool) wp_verify_nonce( $nonce, $action );
}

// ── /Helpers: nonce financeiro ───────────────────────────────────────────────

/**
 * Permission callback seguro para rotas financeiras V2.
 *
 * Valida sessão do portal, dois nonces (wp_rest + financeiro vinculado à sessão)
 * e perfil do usuário.
 *
 * Parâmetros opcionais via $args (passados via closure na rota):
 *   'require_flag'    string — option name que deve ser 'yes' para liberar.
 *   'allow_affiliate' bool   — se true, afiliados são aceitos (default false).
 *   'allow_subuser'   bool   — se true, sub-usuários são aceitos (default false).
 *
 * @param WP_REST_Request $request  Requisição REST.
 * @param array           $args     Opções adicionais (ver acima).
 * @return true|WP_Error
 */
function sz_portal_v2_financial_permission_check( WP_REST_Request $request, array $args = [] ) {
    // 1. Sessão do portal
    $user = sz_portal_auth_user();
    if ( ! $user ) {
        return new WP_Error(
            'sz_portal_v2_unauthenticated',
            'Sessão expirada. Faça login novamente.',
            [ 'status' => 401 ]
        );
    }

    // 2. Nonce WordPress (camada anti-CSRF padrão WP)
    //    O front V2 envia X-WP-Nonce criado com wp_create_nonce('wp_rest').
    $wp_nonce = $request->get_header( 'X-WP-Nonce' );
    if ( ! $wp_nonce || ! wp_verify_nonce( $wp_nonce, 'wp_rest' ) ) {
        return new WP_Error(
            'sz_portal_v2_invalid_nonce',
            'Token de segurança inválido. Recarregue a página e tente novamente.',
            [ 'status' => 403 ]
        );
    }

    // 3. Nonce financeiro vinculado à sessão do portal (camada adicional)
    //    Diferente do wp_rest nonce: está amarrado ao ID do usuário do portal
    //    E ao hash da sessão senderzz_portal_session ativa.
    //    Se o usuário fizer logout/login ou trocar de conta, o nonce falha.
    if ( ! sz_portal_v2_verify_financial_nonce( $request, $user ) ) {
        return new WP_Error(
            'sz_portal_v2_invalid_financial_nonce',
            'Token financeiro inválido ou expirado. Recarregue a página e tente novamente.',
            [ 'status' => 403 ]
        );
    }

    // 4. Perfil: afiliado
    $role = strtolower( trim( (string) ( $user->role ?? 'client' ) ) );
    if ( empty( $args['allow_affiliate'] ) && 'affiliate' === $role ) {
        return new WP_Error(
            'sz_portal_v2_forbidden_role',
            'Operação não disponível para afiliados.',
            [ 'status' => 403 ]
        );
    }

    // 5. Perfil: sub-usuário
    if ( empty( $args['allow_subuser'] ) && ! empty( $user->parent_user_id ) ) {
        return new WP_Error(
            'sz_portal_v2_forbidden_subuser',
            'Operação não disponível para sub-usuários.',
            [ 'status' => 403 ]
        );
    }

    // 6. Feature flag específica (opcional)
    if ( ! empty( $args['require_flag'] ) ) {
        if ( 'yes' !== get_option( $args['require_flag'], 'no' ) ) {
            return new WP_Error(
                'sz_portal_v2_feature_disabled',
                'Esta funcionalidade ainda não está disponível. Utilize o painel clássico.',
                [ 'status' => 403 ]
            );
        }
    }

    return true;
}

/**
 * Permission callback para rotas OPERACIONAIS V2 (não financeiras).
 *
 * Valida sessão do portal + X-WP-Nonce. NÃO exige nonce financeiro nem flag de
 * saque — é usado por Motoboy (reagendar/cancelar), cujo ownership é verificado
 * dentro dos próprios callbacks (por classe de entrega do produtor).
 *
 * @param WP_REST_Request $request
 * @return true|WP_Error
 */
function sz_portal_v2_operational_permission_check( WP_REST_Request $request ) {
    // 1. Sessão do portal
    $user = sz_portal_auth_user();
    if ( ! $user ) {
        return new WP_Error(
            'sz_portal_v2_unauthenticated',
            'Sessão expirada. Faça login novamente.',
            [ 'status' => 401 ]
        );
    }

    // 2. Nonce WordPress (anti-CSRF). O front V2 envia X-WP-Nonce ('wp_rest').
    $wp_nonce = $request->get_header( 'X-WP-Nonce' );
    if ( ! $wp_nonce || ! wp_verify_nonce( $wp_nonce, 'wp_rest' ) ) {
        return new WP_Error(
            'sz_portal_v2_invalid_nonce',
            'Token de segurança inválido. Recarregue a página e tente novamente.',
            [ 'status' => 403 ]
        );
    }

    return true;
}

/**
 * Registra as rotas do namespace sz-portal/v2.
 *
 * Regras:
 *   - Nenhuma rota V2 chama diretamente sz_cod_rest_withdraw() nesta fase.
 *   - Rotas retornam 403 controlado enquanto a flag estiver desligada.
 *   - V1 (/sz-portal/v1) não é alterada.
 */
add_action( 'rest_api_init', function () {

    // ── GET /sz-portal/v2/cod/accounts ───────────────────────────────────────
    // Lista as contas PIX do produtor logado, com nonce e perfil validados.
    // Flag não exigida aqui: leitura de dados próprios é segura mesmo antes
    // do saque estar ativo, e o front V2 não chama esta rota em v441.
    // v445: accounts agora exige a MESMA flag de saque — coerência com o gating
    // do withdraw. Com flag OFF retorna 403; dados bancários são mascarados.
    register_rest_route( 'sz-portal/v2', '/cod/accounts', [
        'methods'             => 'GET',
        'permission_callback' => static function ( WP_REST_Request $req ) {
            return sz_portal_v2_financial_permission_check( $req, [
                'require_flag' => 'senderzz_dashboard_v2_withdraw_enabled',
            ] );
        },
        'callback'            => 'sz_portal_v2_rest_cod_accounts',
    ] );

    // ── POST /sz-portal/v2/cod/withdraw ──────────────────────────────────────
    // Rota de saque COD V2 — bloqueada pela flag até liberação explícita.
    // Não chama sz_cod_rest_withdraw() enquanto flag = 'no'.
    register_rest_route( 'sz-portal/v2', '/cod/withdraw', [
        'methods'             => 'POST',
        'permission_callback' => static function ( WP_REST_Request $req ) {
            return sz_portal_v2_financial_permission_check( $req, [
                'require_flag' => 'senderzz_dashboard_v2_withdraw_enabled',
            ] );
        },
        'callback'            => 'sz_portal_v2_rest_cod_withdraw',
    ] );

    // ── POST /sz-portal/v2/cod/anticipate ────────────────────────────────────
    // Antecipação de recebíveis COD V2 — mesma flag e mesma auth do saque.
    // Delega para sz_cod_rest_withdraw() com tipo=antecipacao forçado server-side.
    register_rest_route( 'sz-portal/v2', '/cod/anticipate', [
        'methods'             => 'POST',
        'permission_callback' => static function ( WP_REST_Request $req ) {
            return sz_portal_v2_financial_permission_check( $req, [
                'require_flag' => 'senderzz_dashboard_v2_withdraw_enabled',
            ] );
        },
        'callback'            => 'sz_portal_v2_rest_cod_anticipate',
    ] );

    // ── Rota operacional Motoboy V2 (v445/v446) ──────────────────────────────
    // Reutiliza o callback V1 (que valida sessão + ownership por classe),
    // mas adiciona validação de nonce no gate via permission_callback seguro.
    // Não é financeira → NÃO exige X-Senderzz-Financial-Nonce nem flag.
    // A rota V1 permanece registrada e intacta (painel clássico).
    //
    // v446: a rota /motoboy/bulk-cancel V2 foi REMOVIDA. A V2 não a consome
    // (cancelamento é individual, via AJAX szaction=cancel, que valida status
    // 'embalado'). O callback V1 sz_rest_motoboy_bulk_cancel não valida status
    // antes de cancelar, então não deve ser exposto como superfície V2.
    register_rest_route( 'sz-portal/v2', '/motoboy/reschedule', [
        'methods'             => 'POST',
        'permission_callback' => 'sz_portal_v2_operational_permission_check',
        'callback'            => 'sz_rest_motoboy_reschedule',
    ] );

} );

/**
 * Mascara um valor genérico mantendo apenas os últimos N caracteres visíveis.
 * Ex.: sz_portal_v2_mask_tail('12345678', 4) => '••••5678'
 * Strings curtas (≤ $visible) são totalmente mascaradas.
 */
function sz_portal_v2_mask_tail( string $value, int $visible = 4 ): string {
    $value = trim( $value );
    if ( $value === '' ) {
        return '';
    }
    $len = mb_strlen( $value );
    if ( $len <= $visible ) {
        return str_repeat( '•', $len );
    }
    return str_repeat( '•', $len - $visible ) . mb_substr( $value, -$visible );
}

/**
 * Mascara uma chave PIX preservando o tipo de exibição.
 * E-mails mostram a primeira letra + domínio mascarado; demais usam tail.
 */
function sz_portal_v2_mask_pix( string $key ): string {
    $key = trim( $key );
    if ( $key === '' ) {
        return '';
    }
    if ( is_email( $key ) ) {
        $parts = explode( '@', $key );
        $local = $parts[0] ?? '';
        $first = mb_substr( $local, 0, 1 );
        return $first . '•••@' . ( $parts[1] ?? '' );
    }
    return sz_portal_v2_mask_tail( $key, 4 );
}

/**
 * Callback: GET /sz-portal/v2/cod/accounts
 * Retorna lista de contas PIX do produtor autenticado, com TODOS os dados
 * sensíveis mascarados (v445). O saque usa apenas account_id — o front nunca
 * recebe chave PIX, agência, conta ou CPF completos.
 * Reutiliza sz_cod_get_accounts() do backend existente (V1) — leitura pura.
 */
function sz_portal_v2_rest_cod_accounts( WP_REST_Request $req ): WP_REST_Response {
    $user = sz_portal_auth_user();
    if ( ! $user ) {
        // Não deve acontecer (permission_callback já bloqueou), mas defensivo.
        return new WP_REST_Response( [ 'success' => false, 'message' => 'Não autenticado.' ], 401 );
    }

    $wp_uid = (int) ( $user->wp_user_id ?? 0 );
    if ( ! $wp_uid ) {
        return new WP_REST_Response( [ 'success' => false, 'message' => 'Usuário sem vínculo WP.' ], 403 );
    }

    if ( ! function_exists( 'sz_cod_get_accounts' ) ) {
        return new WP_REST_Response( [ 'success' => false, 'message' => 'Módulo de carteira indisponível.' ], 503 );
    }

    $accounts = sz_cod_get_accounts( $wp_uid );

    // Máscara de CPF: reutiliza sz_cod_mask_cpf se disponível.
    $mask_cpf = function_exists( 'sz_cod_mask_cpf' )
        ? 'sz_cod_mask_cpf'
        : static fn( string $v ): string => sz_portal_v2_mask_tail( $v, 2 );

    $formatted = array_map( static function ( array $a ) use ( $mask_cpf ): array {
        $holder   = (string) ( $a['holder_name']  ?? '' );
        $pix_type = (string) ( $a['pix_type']      ?? '' );
        $bank     = (string) ( $a['bank_name']     ?? '' );

        $pix_masked     = sz_portal_v2_mask_pix( (string) ( $a['pix_key']        ?? '' ) );
        $agency_masked  = sz_portal_v2_mask_tail( (string) ( $a['agency']         ?? '' ), 2 );
        $account_masked = sz_portal_v2_mask_tail( (string) ( $a['account_number'] ?? '' ), 3 );
        $cpf_masked     = (string) $mask_cpf( (string) ( $a['holder_cpf'] ?? '' ) );

        // Label seguro para o select/resumo do modal — sem dado completo.
        $label = $holder;
        if ( $pix_type !== '' ) {
            $label .= ' — PIX ' . strtoupper( $pix_type ) . ': ' . $pix_masked;
        }
        if ( $bank !== '' ) {
            $label .= ' (' . $bank . ')';
        }

        return [
            'id'                    => (int) ( $a['id'] ?? 0 ),
            'label'                 => $label,
            'holder_name'           => $holder,
            'bank_name'             => $bank,
            'account_type'          => (string) ( $a['account_type'] ?? '' ),
            'pix_type'              => $pix_type,
            // Somente versões mascaradas — nunca o dado completo:
            'pix_key_masked'        => $pix_masked,
            'agency_masked'         => $agency_masked,
            'account_number_masked' => $account_masked,
            'holder_cpf_masked'     => $cpf_masked,
        ];
    }, $accounts );

    return new WP_REST_Response( [
        'success'  => true,
        'accounts' => $formatted,
    ] );
}

/**
 * Callback: POST /sz-portal/v2/cod/withdraw
 *
 * Executa saque COD V2 com todas as validações server-side:
 *   - Flag senderzz_dashboard_v2_withdraw_enabled (guard duplo)
 *   - Sessão do portal (via permission_callback, reconfirmado aqui)
 *   - tipo sempre forçado para 'normal' (antecipação ignorada/bloqueada)
 *   - Valor mínimo R$ 10,00 (adicionado na V2 — ausente no V1)
 *   - Conta pertence ao usuário autenticado
 *   - Transação SQL com FOR UPDATE para serializar saques concorrentes
 *   - Lock de saque duplicado em análise
 *   - Rate limit 5 tentativas/hora (herdado de sz_cod_tables)
 *   - Taxa calculada server-side a partir de sz_cod_get_rules_for_user()
 *   - Notificação de admin e usuário
 *   - Resposta inclui fee e net para o front
 *
 * Não chama sz_cod_rest_withdraw() para evitar aceitar tipo=antecipacao
 * e para adicionar validação de R$10 e retornar fee/net no JSON.
 */
function sz_portal_v2_rest_cod_withdraw( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;

    // Guard duplo — flag (permission_callback já verificou, mas defense in depth)
    if ( 'yes' !== get_option( 'senderzz_dashboard_v2_withdraw_enabled', 'no' ) ) {
        return new WP_REST_Response( [
            'success' => false,
            'message' => 'Saque V2 não habilitado. Utilize o painel clássico.',
        ], 403 );
    }

    // Resolver usuário da sessão do portal
    $user = sz_portal_auth_user();
    if ( ! $user ) {
        return new WP_REST_Response( [ 'success' => false, 'message' => 'Sessão expirada. Recarregue a página.' ], 401 );
    }

    $wp_uid = (int) ( $user->wp_user_id ?? 0 );
    if ( ! $wp_uid ) {
        return new WP_REST_Response( [ 'success' => false, 'message' => 'Usuário sem vínculo WP.' ], 403 );
    }

    // Confirmar funções de dados disponíveis
    if ( ! function_exists( 'sz_cod_tables' ) || ! function_exists( 'sz_cod_get_account' ) ) {
        return new WP_REST_Response( [ 'success' => false, 'message' => 'Módulo de carteira indisponível.' ], 503 );
    }

    // tipo: V2 sempre força 'normal' — antecipação bloqueada nesta rota
    // (ignora qualquer 'tipo' vindo do front)
    $tipo   = 'normal';
    $is_ant = false; // nunca antecipação via V2

    // Rate limiting: máximo 5 tentativas por hora (mesmo padrão V1)
    $rl_key   = 'sz_cod_v2_withdraw_rl_' . $wp_uid;
    $attempts = (int) get_transient( $rl_key );
    if ( $attempts >= 5 ) {
        return new WP_REST_Response( [
            'success' => false,
            'message' => 'Muitas tentativas. Aguarde 1 hora antes de tentar novamente.',
        ], 429 );
    }
    set_transient( $rl_key, $attempts + 1, HOUR_IN_SECONDS );

    // Conta PIX: deve pertencer ao usuário autenticado
    $account_id = absint( $req->get_param( 'account_id' ) );
    $account    = sz_cod_get_account( $wp_uid, $account_id );
    if ( empty( $account ) ) {
        delete_transient( $rl_key ); // não penaliza por tentativa inválida de conta
        return new WP_REST_Response( [
            'success' => false,
            'message' => 'Cadastre uma conta PIX no Perfil antes de sacar.',
        ], 422 );
    }

    // Parsear valor (aceita "1.234,56" ou "1234.56")
    if ( ! function_exists( 'sz_cod_parse_money' ) ) {
        return new WP_REST_Response( [ 'success' => false, 'message' => 'Módulo de carteira indisponível.' ], 503 );
    }
    $amount = sz_cod_parse_money( $req->get_param( 'amount' ) );

    // Valor mínimo R$ 10 — validação SERVER-SIDE adicionada na V2
    if ( $amount < 10.0 ) {
        delete_transient( $rl_key );
        return new WP_REST_Response( [
            'success' => false,
            'message' => 'Valor mínimo para saque é R$ 10,00.',
        ], 422 );
    }

    // Taxa: calculada server-side, nunca do front
    $rules   = function_exists( 'sz_cod_get_rules_for_user' ) ? sz_cod_get_rules_for_user( $wp_uid ) : [];
    $t       = sz_cod_tables();
    $now     = function_exists( 'sz_cod_now_mysql' ) ? sz_cod_now_mysql() : wp_date( 'Y-m-d H:i:s' );

    // Transação SQL — serializa saques concorrentes
    $wpdb->query( 'START TRANSACTION' );
    try {
        // Lock de saque duplicado em análise
        $pending_wd = (int) $wpdb->get_var( $wpdb->prepare(
            "SELECT COUNT(*) FROM {$t['wd']} WHERE user_id=%d AND status IN ('analysis','pending','approved')",
            $wp_uid
        ) );
        if ( $pending_wd > 0 ) {
            $wpdb->query( 'ROLLBACK' );
            return new WP_REST_Response( [
                'success' => false,
                'message' => 'Já existe um saque em análise. Aguarde a conclusão.',
            ], 422 );
        }

        // Saldo disponível com FOR UPDATE (bloqueia leituras concorrentes)
        $available = (float) $wpdb->get_var( $wpdb->prepare(
            "SELECT COALESCE(SUM(net),0) FROM {$t['tx']}
              WHERE user_id=%d AND status='available'
              FOR UPDATE",
            $wp_uid
        ) );

        if ( $amount <= 0 || $amount > $available ) {
            $wpdb->query( 'ROLLBACK' );
            return new WP_REST_Response( [
                'success' => false,
                'message' => 'Valor indisponível para saque.',
            ], 422 );
        }

        $fee = min( $amount, (float) ( $rules['withdraw_fee'] ?? 0 ) );
        $net = max( 0, round( $amount - $fee, 2 ) );

        // ── INSERT 1: registro de saque ───────────────────────────────────────
        $wd_insert_result = $wpdb->insert( $t['wd'], [
            'user_id'     => $wp_uid,
            'account_id'  => (int) $account['id'],
            'amount'      => $amount,
            'fee'         => $fee,
            'net'         => $net,
            'pix_key'     => $account['pix_key'],
            'pix_type'    => $account['pix_type'],
            'holder_name' => $account['holder_name'],
            'holder_cpf'  => $account['holder_cpf'],
            'status'      => 'analysis',
            'created_at'  => $now,
            'updated_at'  => $now,
        ] );
        $wd_id = (int) $wpdb->insert_id;

        // Validação do insert de saque: false = falha de DB; wd_id = 0 = falha de PK
        if ( false === $wd_insert_result || $wd_id <= 0 ) {
            $wpdb->query( 'ROLLBACK' );
            error_log(
                '[Senderzz][sz_portal_v2_rest_cod_withdraw] INSERT wd falhou uid=' . $wp_uid .
                ' last_error=' . $wpdb->last_error
            );
            return new WP_REST_Response( [
                'success' => false,
                'message' => 'Não foi possível registrar o saque. Tente novamente.',
            ], 500 );
        }

        // ── INSERT 2: lançamento contábil de débito ───────────────────────────
        $tx_insert_result = $wpdb->insert( $t['tx'], [
            'user_id'     => $wp_uid,
            'type'        => 'withdrawal',
            'status'      => 'analysis',
            'gross'       => -$amount,
            'fee'         => $fee,
            'net'         => -$amount,
            'description' => 'Solicitação de saque COD V2 #' . $wd_id,
            'created_at'  => $now,
            'updated_at'  => $now,
        ] );

        // Validação do insert de transação
        if ( false === $tx_insert_result ) {
            $wpdb->query( 'ROLLBACK' );
            error_log(
                '[Senderzz][sz_portal_v2_rest_cod_withdraw] INSERT tx falhou uid=' . $wp_uid .
                ' wd_id=' . $wd_id . ' last_error=' . $wpdb->last_error
            );
            return new WP_REST_Response( [
                'success' => false,
                'message' => 'Não foi possível registrar a movimentação do saque. Tente novamente.',
            ], 500 );
        }

        // ── COMMIT — só chega aqui se ambos os inserts foram bem-sucedidos ────
        $wpdb->query( 'COMMIT' );
        delete_transient( $rl_key );

        if ( function_exists( 'sz_cod_wallet_invalidate_summary_cache' ) ) {
            sz_cod_wallet_invalidate_summary_cache( $wp_uid );
        }

        // ── Notificações — somente após COMMIT confirmado ─────────────────────
        $admin_email = get_option( 'admin_email' );
        if ( $admin_email ) {
            wp_mail(
                $admin_email,
                "Senderzz: nova solicitação de SAQUE COD V2 #{$wd_id}",
                "Nova solicitação de SAQUE (Dashboard V2) recebida.\n\n" .
                "ID: #{$wd_id}\nValor: " . sz_cod_money( $amount ) .
                "\nTaxa: " . sz_cod_money( $fee ) .
                "\nLíquido: " . sz_cod_money( $net ) .
                "\nConta: " . ( $account['holder_name'] ?? '' ) .
                " · PIX " . strtoupper( $account['pix_type'] ?? '' ) .
                ": " . ( $account['pix_key'] ?? '' ) .
                "\n\nAcesse WP Admin > Senderzz > Financeiro COD para processar."
            );
        }

        if ( function_exists( 'sz_cod_notify_user' ) ) {
            sz_cod_notify_user(
                $wp_uid,
                "Senderzz: Saque #{$wd_id} recebido",
                "Recebemos sua solicitação #{$wd_id}.\n" .
                "Valor: " . sz_cod_money( $amount ) .
                "\nTaxa: " . sz_cod_money( $fee ) .
                "\nLíquido previsto: " . sz_cod_money( $net ) .
                ".\n\nProcessamento em até 1 dia útil."
            );
        }

        return new WP_REST_Response( [
            'success' => true,
            'message' => 'Saque solicitado com sucesso.',
            'fee'     => $fee,
            'net'     => $net,
            'wd_id'   => $wd_id,
        ] );

    } catch ( \Throwable $e ) {
        $wpdb->query( 'ROLLBACK' );
        error_log( '[Senderzz][sz_portal_v2_rest_cod_withdraw] Transação revertida uid=' . $wp_uid . ': ' . $e->getMessage() );
        return new WP_REST_Response( [ 'success' => false, 'message' => 'Erro interno. Tente novamente.' ], 500 );
    }
}

/**
 * Callback: POST /sz-portal/v2/cod/anticipate
 *
 * Antecipa todos os recebíveis pendentes do usuário via conta PIX informada.
 * Delega para sz_cod_rest_withdraw() com tipo=antecipacao forçado server-side —
 * o front nunca controla o tipo, eliminando o bypass de privilégios.
 *
 * O permission_callback já validou: sessão, dois nonces (wp_rest + financeiro),
 * perfil (não afiliado, não sub-user) e flag senderzz_dashboard_v2_withdraw_enabled.
 *
 * Requer no body: account_id (int)
 * Não lê 'amount' do front — o valor antecipado é a soma de todas as tx pendentes.
 */
if ( ! function_exists( 'sz_portal_v2_rest_cod_anticipate' ) ) {
    function sz_portal_v2_rest_cod_anticipate( WP_REST_Request $req ): WP_REST_Response {
        // Guard duplo — flag (permission_callback já verificou, mas defense in depth)
        if ( 'yes' !== get_option( 'senderzz_dashboard_v2_withdraw_enabled', 'no' ) ) {
            return new WP_REST_Response( [
                'success' => false,
                'message' => 'Antecipação não habilitada.',
            ], 403 );
        }

        // Verificar disponibilidade da função de antecipação
        if ( ! function_exists( 'sz_cod_rest_withdraw' ) ) {
            return new WP_REST_Response( [ 'success' => false, 'message' => 'Módulo de carteira indisponível.' ], 503 );
        }

        // Força tipo=antecipacao via set_param (não via $_POST — evita bypass)
        $req->set_param( 'tipo', 'antecipacao' );

        // Delega para a implementação v1 que contém toda a lógica de antecipação:
        // SELECT FOR UPDATE, marcação anticipation_pending, INSERT wd (approved),
        // INSERT tx (anticipation + anticipation_credit), COMMIT, notificações.
        return sz_cod_rest_withdraw( $req );
    }
}

// ── /sz-portal/v2 ────────────────────────────────────────────────────────────


function sz_rest_motoboy_bulk_cancel( WP_REST_Request $req ): WP_REST_Response {
    $user = sz_portal_auth_user();
    if ( ! $user ) return new WP_REST_Response( [ 'success' => false, 'message' => 'Não autenticado.' ], 403 );
    $ids = array_filter( array_map( 'intval', (array) $req->get_param( 'order_ids' ) ) );
    if ( empty( $ids ) ) return new WP_REST_Response( [ 'success' => false, 'message' => 'Nenhum pedido.' ], 400 );

    // Guard: só cancela pedidos que pertencem à classe do produtor logado.
    $_sz_ownership_class_ids = function_exists( 'sz_get_user_class_ids' ) ? sz_get_user_class_ids( $user ) : [ (int) ( $user->shipping_class_id ?? 0 ) ];
    $user_class_id = ! empty( $_sz_ownership_class_ids ) ? $_sz_ownership_class_ids[0] : 0; // legado
    $done = 0; $skipped = 0;
    foreach ( $ids as $id ) {
        $order = wc_get_order( $id );
        if ( ! $order ) { $skipped++; continue; }
        if ( $user_class_id > 0 ) {
            $order_class = (int) $order->get_meta( '_senderzz_product_shipping_class_id', true );
            if ( $order_class !== $user_class_id ) { $skipped++; continue; }
        }
        $order->update_status( 'cancelled', 'Cancelado em lote pelo produtor.' );
        $done++;
    }
    $msg = $done . ' pedido(s) cancelado(s).';
    if ( $skipped > 0 ) $msg .= ' ' . $skipped . ' ignorado(s) por não pertencer a esta conta.';
    return new WP_REST_Response( [ 'success' => true, 'message' => $msg ] );
}

function sz_rest_motoboy_reschedule( WP_REST_Request $req ): WP_REST_Response {
    $user = sz_portal_auth_user();
    if ( ! $user ) return new WP_REST_Response( [ 'success' => false, 'message' => 'Não autenticado.' ], 403 );
    $ids  = array_filter( array_map( 'intval', (array) $req->get_param( 'order_ids' ) ) );
    $date = sanitize_text_field( (string) $req->get_param( 'date' ) );
    if ( empty( $ids ) || ! $date || $date < date('Y-m-d') ) return new WP_REST_Response( [ 'success' => false, 'message' => 'Dados inválidos.' ], 400 );

    // Guard ownership: só reagenda pedidos que pertencem à classe do produtor logado.
    $_sz_ownership_class_ids = function_exists( 'sz_get_user_class_ids' ) ? sz_get_user_class_ids( $user ) : [ (int) ( $user->shipping_class_id ?? 0 ) ];
    $user_class_id = ! empty( $_sz_ownership_class_ids ) ? $_sz_ownership_class_ids[0] : 0;

    $done = 0; $skipped = 0;
    foreach ( $ids as $id ) {
        $order = wc_get_order( $id );
        if ( ! $order ) { $skipped++; continue; }
        // Verifica ownership pela classe de entrega do pedido
        if ( $user_class_id > 0 ) {
            $order_class = (int) $order->get_meta( '_senderzz_product_shipping_class_id', true );
            if ( $order_class !== $user_class_id ) { $skipped++; continue; }
        }
        $order->update_meta_data( '_sz_delivery_date', $date );
        $order->update_meta_data( '_senderzz_delivery_date', $date );
        $order->update_meta_data( '_sz_motoboy_entrega_data', $date );
        $order->update_meta_data( '_senderzz_rescheduled_at', current_time( 'mysql' ) );
        $order->update_meta_data( '_senderzz_rescheduled_by', get_current_user_id() );
        global $wpdb;
        $pedido = $wpdb->get_row( $wpdb->prepare( "SELECT id, status FROM {$wpdb->prefix}sz_motoboy_pedidos WHERE wc_order_id = %d LIMIT 1", $id ) );
        if ( $pedido ) {
            $wpdb->update( $wpdb->prefix . 'sz_motoboy_pedidos', [ 'reagendado_para' => $date, 'updated_at' => current_time( 'mysql' ) ], [ 'id' => (int) $pedido->id ], [ '%s', '%s' ], [ '%d' ] );
        }
        $order->save();
        $order->add_order_note( 'Data de entrega alterada para ' . $date . ' via portal sem alterar status.' );
        $done++;
    }
    return new WP_REST_Response( [ 'success' => true, 'message' => $done . ' pedido(s) reagendado(s) para ' . $date . '.' ] );
}

function sz_rest_wallet_history( WP_REST_Request $req ): WP_REST_Response {
    $user = sz_portal_auth_user();
    if ( ! $user ) return new WP_REST_Response( [ 'success' => false, 'message' => 'Não autenticado.' ], 403 );
    $wp_user_id = (int) ( $user->wp_user_id ?? 0 );
    if ( ! $wp_user_id && function_exists( 'sz_mb_get_portal_user_id' ) ) {
        $wp_user_id = (int) sz_mb_get_portal_user_id();
    }
    if ( ! $wp_user_id ) return new WP_REST_Response( [ 'success' => false, 'message' => 'Usuário sem vínculo WP.' ], 403 );
    $period = sanitize_text_field( (string) $req->get_param( 'period' ) );
    if ( function_exists( 'sz_cod_wallet_history' ) ) {
        return new WP_REST_Response( [ 'success' => true, 'data' => sz_cod_wallet_history( $wp_user_id, $period ?: '30d' ) ] );
    }
    return new WP_REST_Response( [ 'success' => true, 'data' => [] ] );
}

function sz_rest_wallet_future( WP_REST_Request $req ): WP_REST_Response {
    $user = sz_portal_auth_user();
    if ( ! $user ) return new WP_REST_Response( [ 'success' => false, 'message' => 'Não autenticado.' ], 403 );
    $wp_user_id = (int) ( $user->wp_user_id ?? 0 );
    if ( ! $wp_user_id && function_exists( 'sz_mb_get_portal_user_id' ) ) {
        $wp_user_id = (int) sz_mb_get_portal_user_id();
    }
    $period = sanitize_text_field( (string) $req->get_param( 'period' ) );
    if ( $wp_user_id && function_exists( 'sz_cod_wallet_future' ) ) {
        $future = sz_cod_wallet_future( $wp_user_id, $period ?: '30d' );
        return new WP_REST_Response( [ 'success' => true, 'data' => $future['data'], 'total_commission' => $future['total'] ] );
    }
    return new WP_REST_Response( [ 'success' => true, 'data' => [], 'total_commission' => 0 ] );
}


// ── Exportação CSV da carteira (portal do produtor) ──────────────────────────
function sz_rest_wallet_export_csv( WP_REST_Request $req ): void {
    $user = sz_portal_auth_user();
    if ( ! $user ) { status_header(403); exit; }
    global $wpdb;
    $wp_user_id = (int) ( $user->wp_user_id ?? 0 );
    if ( ! $wp_user_id ) { status_header(403); exit; }

    $type   = sanitize_text_field( (string) $req->get_param( 'type' ) ?: 'history' );
    $period = sanitize_text_field( (string) $req->get_param( 'period' ) ?: '30d' );
    $now = current_time( 'timestamp' );
    $date_from = sanitize_text_field( (string) $req->get_param( 'date_from' ) );
    $date_to   = sanitize_text_field( (string) $req->get_param( 'date_to' ) );

    $from = null; $to = null;
    if ( $period === 'custom' && preg_match( '/^\d{4}-\d{2}-\d{2}$/', $date_from ) && preg_match( '/^\d{4}-\d{2}-\d{2}$/', $date_to ) ) {
        $from = strtotime( $date_from . ' 00:00:00' );
        $to   = strtotime( $date_to . ' 23:59:59' );
    } elseif ( $period !== 'all' ) {
        if ( $type === 'future' ) {
            switch ( $period ) {
                case 'today': $to = strtotime( 'today 23:59:59', $now ); break;
                case 'tomorrow': $to = strtotime( 'tomorrow 23:59:59', $now ); break;
                case '7d':  $to = strtotime( '+7 days', $now ); break;
                case '15d': $to = strtotime( '+15 days', $now ); break;
                default: $to = strtotime( '+30 days', $now ); break;
            }
        } else {
            switch ( $period ) {
                case 'today': $from = strtotime( 'today', $now ); $to = $now; break;
                case 'yesterday': $from = strtotime( 'yesterday', $now ); $to = strtotime( 'today', $now ) - 1; break;
                case '7d':  $from = strtotime( '-7 days', $now );  $to = $now; break;
                case 'month': $from = strtotime( 'first day of this month', $now ); $to = $now; break;
                default: $from = strtotime( '-30 days', $now ); $to = $now; break;
            }
        }
    }

    $tables = function_exists( 'sz_cod_tables' ) ? sz_cod_tables() : [];
    $table = $tables['tx'] ?? ( $wpdb->prefix . 'sz_cod_wallet_tx' );

    if ( $type === 'future' ) {
        if ( $period === 'custom' && $from && $to ) {
            $rows = $wpdb->get_results( $wpdb->prepare(
                "SELECT * FROM {$table} WHERE user_id=%d AND status='pending' AND release_at BETWEEN %s AND %s ORDER BY release_at ASC LIMIT 5000",
                $wp_user_id, date( 'Y-m-d H:i:s', $from ), date( 'Y-m-d H:i:s', $to )
            ), ARRAY_A ) ?: [];
        } elseif ( $period === 'all' ) {
            $rows = $wpdb->get_results( $wpdb->prepare(
                "SELECT * FROM {$table} WHERE user_id=%d AND status='pending' ORDER BY release_at ASC LIMIT 5000",
                $wp_user_id
            ), ARRAY_A ) ?: [];
        } else {
            $rows = $wpdb->get_results( $wpdb->prepare(
                "SELECT * FROM {$table} WHERE user_id=%d AND status='pending' AND release_at <= %s ORDER BY release_at ASC LIMIT 5000",
                $wp_user_id, date( 'Y-m-d H:i:s', $to ?: $now )
            ), ARRAY_A ) ?: [];
        }
        header( 'Content-Type: text/csv; charset=utf-8' );
        header( 'Content-Disposition: attachment; filename="senderzz-lancamentos-futuros-' . date('Y-m-d') . '.csv"' );
        header( 'Cache-Control: no-cache, no-store, must-revalidate' );
        $out = fopen( 'php://output', 'w' );
        fprintf( $out, chr(0xEF) . chr(0xBB) . chr(0xBF) );
        fputcsv( $out, [ 'Liberação', 'Descrição', 'Pedido', 'Conclusão', 'Comissão R$' ], ';' );
        foreach ( $rows as $r ) {
            fputcsv( $out, [
                ! empty( $r['release_at'] ) ? date_i18n( 'd/m/Y', strtotime( $r['release_at'] ) ) : '—',
                (string) ( $r['description'] ?? 'Recebimento COD' ),
                ! empty( $r['order_id'] ) ? '#' . $r['order_id'] : '—',
                ! empty( $r['created_at'] ) ? date_i18n( 'd/m/Y', strtotime( $r['created_at'] ) ) : '—',
                number_format( (float) ( $r['net'] ?? 0 ), 2, ',', '.' ),
            ], ';' );
        }
        fclose( $out );
        exit;
    }

    if ( $period === 'all' ) {
        $rows = $wpdb->get_results( $wpdb->prepare(
            "SELECT * FROM {$table} WHERE user_id=%d ORDER BY created_at DESC LIMIT 5000",
            $wp_user_id
        ), ARRAY_A ) ?: [];
    } else {
        if ( ! $from || ! $to || $from > $to ) { $from = strtotime( '-30 days', $now ); $to = $now; }
        $rows = $wpdb->get_results( $wpdb->prepare(
            "SELECT * FROM {$table} WHERE user_id=%d AND created_at BETWEEN %s AND %s ORDER BY created_at DESC LIMIT 5000",
            $wp_user_id, date( 'Y-m-d H:i:s', $from ), date( 'Y-m-d H:i:s', $to )
        ), ARRAY_A ) ?: [];
    }

    header( 'Content-Type: text/csv; charset=utf-8' );
    header( 'Content-Disposition: attachment; filename="senderzz-carteira-' . date('Y-m-d') . '.csv"' );
    header( 'Cache-Control: no-cache, no-store, must-revalidate' );
    $out = fopen( 'php://output', 'w' );
    fprintf( $out, chr(0xEF) . chr(0xBB) . chr(0xBF) );
    fputcsv( $out, [ 'Data', 'Descrição', 'Pedido', 'Valor R$', 'Taxa R$', 'Líquido R$', 'Status' ], ';' );
    foreach ( $rows as $r ) {
        fputcsv( $out, [
            ! empty( $r['created_at'] ) ? date_i18n( 'd/m/Y H:i', strtotime( $r['created_at'] ) ) : '—',
            (string) ( $r['description'] ?? 'Recebimento COD' ),
            ! empty( $r['order_id'] ) ? '#' . $r['order_id'] : '—',
            number_format( (float) ( $r['gross'] ?? 0 ), 2, ',', '.' ),
            number_format( (float) ( $r['fee'] ?? 0 ), 2, ',', '.' ),
            number_format( (float) ( $r['net'] ?? 0 ), 2, ',', '.' ),
            (string) ( $r['status'] ?? '—' ),
        ], ';' );
    }
    fclose( $out );
    exit;
}

// ── Botão "já paguei" PIX no portal do produtor ──────────────────────────────
function sz_rest_pix_confirm( WP_REST_Request $req ): WP_REST_Response {
    $user = sz_portal_auth_user();
    if ( ! $user ) return new WP_REST_Response( [ 'success' => false, 'message' => 'Não autenticado.' ], 403 );

    $wp_user_id = (int) ( $user->wp_user_id ?? 0 );
    if ( ! $wp_user_id ) return new WP_REST_Response( [ 'success' => false, 'message' => 'Usuário sem vínculo WP.' ], 403 );

    // Rate limit: máximo 1 confirmação manual por minuto por usuário.
    $lock_key = 'sz_pix_confirm_lock_' . $wp_user_id;
    if ( get_transient( $lock_key ) ) {
        return new WP_REST_Response( [ 'success' => false, 'message' => 'Aguarde 1 minuto antes de tentar novamente.' ], 429 );
    }
    set_transient( $lock_key, 1, 60 );

    global $wpdb;
    // Busca a recarga pendente mais recente deste usuário.
    $table   = $wpdb->prefix . 'tpc_recargas';
    $recarga = $wpdb->get_row( $wpdb->prepare(
        "SELECT * FROM {$table} WHERE user_id = %d AND status = 'pendente' ORDER BY created_at DESC LIMIT 1",
        $wp_user_id
    ), ARRAY_A );

    if ( ! $recarga ) {
        return new WP_REST_Response( [ 'success' => false, 'message' => 'Nenhuma recarga PIX pendente encontrada. O pagamento pode já ter sido confirmado automaticamente.' ], 404 );
    }

    // Dispara a reconciliação apenas desta recarga via cron imediato.
    if ( function_exists( 'tpc_verificar_recarga_pix' ) ) {
        $result = tpc_verificar_recarga_pix( (int) $recarga['id'] );
        if ( $result ) {
            $saldo = function_exists( 'tpc_get_saldo_disponivel' ) ? tpc_get_saldo_disponivel( $wp_user_id ) : 0;
            return new WP_REST_Response( [
                'success' => true,
                'message' => 'PIX confirmado! Seu saldo foi atualizado.',
                'saldo'   => 'R$ ' . number_format( (float) $saldo, 2, ',', '.' ),
            ] );
        }
    }

    // Fallback: agenda verificação do cron para rodar agora.
    wp_schedule_single_event( time(), 'tpc_cron_verificar_recargas_pix' );

    return new WP_REST_Response( [
        'success' => false,
        'message' => 'Pagamento ainda não identificado. O sistema verifica automaticamente a cada 5 minutos. Tente novamente em instantes.',
    ], 202 );
}


// ── Endpoint: margem por produtor (portal) ────────────────────────────────────
add_action( 'rest_api_init', function (): void {
    // Registrado em sz-portal/v1 (mesmo namespace do wallet/history) — cookie auth do portal
    register_rest_route( 'sz-portal/v1', '/producer-margin', [
        'methods'             => 'GET',
        'callback'            => 'sz_rest_producer_margin',
        'permission_callback' => '__return_true',
    ] );
    // Alias em senderzz/v1 para compatibilidade com qualquer chamada existente
    register_rest_route( 'senderzz/v1', '/producer-margin', [
        'methods'             => 'GET',
        'callback'            => 'sz_rest_producer_margin',
        'permission_callback' => '__return_true',
    ] );
} );

function sz_rest_producer_margin( WP_REST_Request $req ): WP_REST_Response {
    $user = sz_portal_auth_user();
    if ( ! $user ) return new WP_REST_Response( [ 'success' => false, 'message' => 'Não autenticado.' ], 403 );

    // Multi-class: usa todas as classes do usuário.
    $_sz_rest_class_ids = function_exists( 'sz_get_user_class_ids' ) ? sz_get_user_class_ids( $user ) : [ (int) ( $user->shipping_class_id ?? 0 ) ];
    $_sz_rest_class_ids = array_filter( $_sz_rest_class_ids, fn($id) => $id >= 0 );
    $class_id  = ! empty( $_sz_rest_class_ids ) ? $_sz_rest_class_ids[0] : (int)( $user->shipping_class_id ?? 0 ); // legado
    // Para queries com HAVING, usamos IN com todos os IDs.
    $_sz_rest_class_in = ! empty( $_sz_rest_class_ids ) ? '(' . implode( ',', array_map( 'intval', $_sz_rest_class_ids ) ) . ')' : '(0)';
    if ( ! $class_id ) return new WP_REST_Response( [ 'success' => false, 'message' => 'Classe não configurada.' ], 400 );

    $date_from = sanitize_text_field( $req->get_param( 'date_from' ) ?: date( 'Y-m-01' ) );
    $date_to   = sanitize_text_field( $req->get_param( 'date_to' )   ?: date( 'Y-m-d' ) );

    global $wpdb;

    $hpos_table  = $wpdb->prefix . 'wc_orders';
    $hpos_active = (bool) $wpdb->get_var(
        "SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='{$hpos_table}'"
    );

    if ( $hpos_active ) {
        $meta_table = $wpdb->prefix . 'wc_orders_meta';
        $sql = $wpdb->prepare(
            "SELECT o.id AS order_id,
                    REPLACE(o.status,'wc-','') AS order_status,
                    DATE(o.date_created_gmt) AS order_date,
                    MAX(CASE WHEN m.meta_key='_senderzz_shipping_charged'   THEN CAST(m.meta_value AS DECIMAL(10,2)) END) AS charged,
                    MAX(CASE WHEN m.meta_key='_senderzz_shipping_real_cost' THEN CAST(m.meta_value AS DECIMAL(10,2)) END) AS real_cost,
                    MAX(CASE WHEN m.meta_key='_senderzz_service_fee'        THEN CAST(m.meta_value AS DECIMAL(10,2)) END) AS margin,
                    MAX(CASE WHEN m.meta_key='_senderzz_carrier_name'       THEN m.meta_value END) AS carrier
             FROM {$hpos_table} o
             INNER JOIN {$meta_table} m ON m.order_id=o.id
                AND m.meta_key IN ('_senderzz_shipping_charged','_senderzz_shipping_real_cost','_senderzz_service_fee','_senderzz_carrier_name','_senderzz_product_shipping_class_id')
             WHERE o.type='shop_order' AND DATE(o.date_created_gmt) BETWEEN %s AND %s
             GROUP BY o.id
             HAVING MAX(CASE WHEN m.meta_key='_senderzz_product_shipping_class_id' THEN CAST(m.meta_value AS UNSIGNED) END) IN {$_sz_rest_class_in}
               AND margin IS NOT NULL
             ORDER BY o.date_created_gmt DESC LIMIT 500",
            $date_from, $date_to
        );
    } else {
        $sql = $wpdb->prepare(
            "SELECT p.ID AS order_id,
                    REPLACE(p.post_status,'wc-','') AS order_status,
                    DATE(p.post_date) AS order_date,
                    MAX(CASE WHEN m.meta_key='_senderzz_shipping_charged'   THEN CAST(m.meta_value AS DECIMAL(10,2)) END) AS charged,
                    MAX(CASE WHEN m.meta_key='_senderzz_shipping_real_cost' THEN CAST(m.meta_value AS DECIMAL(10,2)) END) AS real_cost,
                    MAX(CASE WHEN m.meta_key='_senderzz_service_fee'        THEN CAST(m.meta_value AS DECIMAL(10,2)) END) AS margin,
                    MAX(CASE WHEN m.meta_key='_senderzz_carrier_name'       THEN m.meta_value END) AS carrier
             FROM {$wpdb->posts} p
             INNER JOIN {$wpdb->postmeta} m ON m.post_id=p.ID
                AND m.meta_key IN ('_senderzz_shipping_charged','_senderzz_shipping_real_cost','_senderzz_service_fee','_senderzz_carrier_name','_senderzz_product_shipping_class_id')
             WHERE p.post_type='shop_order' AND DATE(p.post_date) BETWEEN %s AND %s
             GROUP BY p.ID
             HAVING MAX(CASE WHEN m.meta_key='_senderzz_product_shipping_class_id' THEN CAST(m.meta_value AS UNSIGNED) END) IN {$_sz_rest_class_in}
               AND margin IS NOT NULL
             ORDER BY p.post_date DESC LIMIT 500",
            $date_from, $date_to
        );
    }

    $rows = $wpdb->get_results( $sql, ARRAY_A ) ?: [];

    $total_charged = 0.0;
    $total_real    = 0.0;
    $total_margin  = 0.0;

    $formatted = array_map( function( $r ) use ( &$total_charged, &$total_real, &$total_margin ) {
        $charged   = round( (float) ( $r['charged']   ?? 0 ), 2 );
        $real      = round( (float) ( $r['real_cost'] ?? 0 ), 2 );
        $margin    = round( (float) ( $r['margin']    ?? 0 ), 2 );
        $total_charged += $charged;
        $total_real    += $real;
        $total_margin  += $margin;
        return [
            'order_id'     => (int) $r['order_id'],
            'order_status' => (string) $r['order_status'],
            'order_date'   => (string) $r['order_date'],
            'charged'      => $charged,
            'real_cost'    => $real,
            'margin'       => $margin,
            'carrier'      => (string) ( $r['carrier'] ?? '—' ),
        ];
    }, $rows );

    return new WP_REST_Response( [
        'success' => true,
        'data'    => [
            'rows'          => $formatted,
            'total_charged' => round( $total_charged, 2 ),
            'total_real'    => round( $total_real, 2 ),
            'total_margin'  => round( $total_margin, 2 ),
            'period'        => [ 'from' => $date_from, 'to' => $date_to ],
        ],
    ] );
}


// ── AJAX handler para margem (fallback sem nonce WP REST) ─────────────────────
add_action( 'wp_ajax_sz_producer_margin',        'sz_ajax_producer_margin' );
add_action( 'wp_ajax_nopriv_sz_producer_margin', 'sz_ajax_producer_margin' );
function sz_ajax_producer_margin(): void {
    $user = sz_portal_auth_user();
    if ( ! $user ) { wp_send_json_error( [ 'message' => 'Não autenticado.' ], 403 ); }
    $req = new WP_REST_Request( 'GET' );
    $req->set_param( 'date_from', sanitize_text_field( $_GET['date_from'] ?? date( 'Y-m-01' ) ) );
    $req->set_param( 'date_to',   sanitize_text_field( $_GET['date_to']   ?? date( 'Y-m-d' ) ) );
    $result = sz_rest_producer_margin( $req );
    wp_send_json( $result->get_data() );
}
