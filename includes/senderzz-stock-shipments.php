<?php
/**
 * senderzz-stock-shipments.php
 *
 * Módulo Senderzz — Envio de Estoque Multi-item.
 *
 * Modelo correto:
 *  - senderzz_stock_shipments: cabeçalho do envio
 *  - senderzz_stock_shipment_items: produtos/variações enviados
 *  - senderzz_stock_shipment_logs: histórico auditável
 *
 * Fluxo:
 *  pendente  -> produtor salva rascunho
 *  enviado   -> produtor confirma envio
 *  entregue  -> operador confirma recebimento e informa qtd recebida por item
 *  concluido -> operador conclui e adiciona estoque recebido por item no WooCommerce
 */
defined( 'ABSPATH' ) || exit;

if ( defined( 'SENDERZZ_STOCK_SHIPMENTS_LOADED' ) ) return;
define( 'SENDERZZ_STOCK_SHIPMENTS_LOADED', true );

define( 'SENDERZZ_STOCK_SHIP_TABLE', 'senderzz_stock_shipments' );
define( 'SENDERZZ_STOCK_SHIP_ITEM_TABLE', 'senderzz_stock_shipment_items' );
define( 'SENDERZZ_STOCK_SHIP_LOG_TABLE', 'senderzz_stock_shipment_logs' );

function senderzz_stock_shipments_install_table(): void {
    if ( get_option( 'senderzz_stock_shipments_db_v358_done' ) ) return;
    global $wpdb;
    require_once ABSPATH . 'wp-admin/includes/upgrade.php';

    $ship = $wpdb->prefix . SENDERZZ_STOCK_SHIP_TABLE;
    $items = $wpdb->prefix . SENDERZZ_STOCK_SHIP_ITEM_TABLE;
    $logs = $wpdb->prefix . SENDERZZ_STOCK_SHIP_LOG_TABLE;
    $charset = $wpdb->get_charset_collate();

    dbDelta( "CREATE TABLE {$ship} (
        id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        user_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
        tracking VARCHAR(128) NOT NULL DEFAULT '',
        carrier VARCHAR(80) NOT NULL DEFAULT '',
        status VARCHAR(32) NOT NULL DEFAULT 'pendente',
        notes TEXT NULL,
        sent_at DATETIME NULL,
        delivered_at DATETIME NULL,
        concluded_at DATETIME NULL,
        created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
        updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (id),
        KEY user_id (user_id),
        KEY status (status),
        KEY tracking (tracking)
    ) {$charset};" );

    dbDelta( "CREATE TABLE {$items} (
        id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        shipment_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
        product_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
        variation_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
        qty_sent INT UNSIGNED NOT NULL DEFAULT 1,
        qty_received INT UNSIGNED NOT NULL DEFAULT 0,
        unit_cost DECIMAL(10,2) NOT NULL DEFAULT 0.00,
        total_cost DECIMAL(10,2) NOT NULL DEFAULT 0.00,
        stock_added TINYINT(1) NOT NULL DEFAULT 0,
        created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
        updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (id),
        KEY shipment_id (shipment_id),
        KEY product_id (product_id),
        KEY variation_id (variation_id)
    ) {$charset};" );

    dbDelta( "CREATE TABLE {$logs} (
        id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        shipment_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
        user_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
        user_name VARCHAR(160) NOT NULL DEFAULT '',
        action VARCHAR(80) NOT NULL DEFAULT '',
        data_json LONGTEXT NULL,
        created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (id),
        KEY shipment_id (shipment_id),
        KEY action (action),
        KEY created_at (created_at)
    ) {$charset};" );

    // Compatibilidade com versões antigas: se a tabela antiga tinha colunas de item, mantém.
    $cols = $wpdb->get_col( "SHOW COLUMNS FROM {$ship}", 0 );
    $legacy_defs = [
        'product_id'    => "BIGINT UNSIGNED NOT NULL DEFAULT 0",
        'variation_id'  => "BIGINT UNSIGNED NOT NULL DEFAULT 0",
        'qty'           => "INT UNSIGNED NOT NULL DEFAULT 0",
        'qty_received'  => "INT UNSIGNED NOT NULL DEFAULT 0",
        'unit_cost'     => "DECIMAL(10,2) NOT NULL DEFAULT 0.00",
        'total_cost'    => "DECIMAL(10,2) NOT NULL DEFAULT 0.00",
        'history_json'  => "LONGTEXT NULL",
    ];
    foreach ( $legacy_defs as $col => $def ) {
        if ( ! in_array( $col, $cols, true ) ) {
            $wpdb->query( "ALTER TABLE {$ship} ADD {$col} {$def}" );
        }
    }

    senderzz_stock_shipments_migrate_legacy_rows();
    update_option( 'senderzz_stock_shipments_db_v358_done', current_time( 'mysql' ), false );
}
add_action( 'init', 'senderzz_stock_shipments_install_table', 8 );

function senderzz_stock_shipments_migrate_legacy_rows(): void {
    global $wpdb;
    $ship = $wpdb->prefix . SENDERZZ_STOCK_SHIP_TABLE;
    $items = $wpdb->prefix . SENDERZZ_STOCK_SHIP_ITEM_TABLE;

    $rows = $wpdb->get_results(
        "SELECT id, product_id, variation_id, qty, qty_received, unit_cost, total_cost, created_at
         FROM {$ship}
         WHERE product_id > 0
           AND id NOT IN (SELECT DISTINCT shipment_id FROM {$items})
         LIMIT 300",
        ARRAY_A
    );

    if ( ! is_array( $rows ) || ! $rows ) return;

    foreach ( $rows as $row ) {
        $qty = max( 1, absint( $row['qty'] ?? 1 ) );
        $unit = max( 0, (float) ( $row['unit_cost'] ?? 0 ) );
        $wpdb->insert( $items, [
            'shipment_id'  => (int) $row['id'],
            'product_id'   => (int) $row['product_id'],
            'variation_id' => (int) ( $row['variation_id'] ?? 0 ),
            'qty_sent'     => $qty,
            'qty_received' => absint( $row['qty_received'] ?? 0 ),
            'unit_cost'    => $unit,
            'total_cost'   => (float) ( $row['total_cost'] ?? round( $qty * $unit, 2 ) ),
            'created_at'   => $row['created_at'] ?: current_time( 'mysql', true ),
            'updated_at'   => current_time( 'mysql', true ),
        ] );
    }
}

add_action( 'rest_api_init', function () {
    $ns = 'senderzz/v1';
    register_rest_route( $ns, '/stock-shipments', [ 'methods' => 'POST', 'callback' => 'senderzz_rest_stock_shipment_create', 'permission_callback' => 'senderzz_rest_stock_shipment_permission' ] );
    register_rest_route( $ns, '/stock-shipments', [ 'methods' => 'GET', 'callback' => 'senderzz_rest_stock_shipments_list', 'permission_callback' => 'senderzz_rest_stock_shipment_permission' ] );
    register_rest_route( $ns, '/stock-shipments/(?P<id>\d+)/confirm', [ 'methods' => 'POST', 'callback' => 'senderzz_rest_stock_shipment_confirm', 'permission_callback' => 'senderzz_rest_stock_shipment_permission' ] );
    register_rest_route( $ns, '/stock-shipments/(?P<id>\d+)', [ 'methods' => 'DELETE', 'callback' => 'senderzz_rest_stock_shipment_delete', 'permission_callback' => 'senderzz_rest_stock_shipment_permission' ] );
    register_rest_route( $ns, '/stock-shipments/update', [ 'methods' => 'POST', 'callback' => 'senderzz_rest_stock_shipment_update', 'permission_callback' => 'senderzz_rest_stock_shipment_permission' ] );
    register_rest_route( $ns, '/stock-shipments/(?P<id>\d+)/deliver', [ 'methods' => 'POST', 'callback' => 'senderzz_rest_stock_shipment_deliver', 'permission_callback' => 'senderzz_rest_stock_shipment_operator_permission' ] );
    register_rest_route( $ns, '/stock-shipments/(?P<id>\d+)/conclude', [ 'methods' => 'POST', 'callback' => 'senderzz_rest_stock_shipment_conclude', 'permission_callback' => 'senderzz_rest_stock_shipment_operator_permission' ] );
} );

add_action( 'wp_ajax_senderzz_stock_shipments', 'senderzz_ajax_stock_shipments' );
add_action( 'wp_ajax_nopriv_senderzz_stock_shipments', 'senderzz_ajax_stock_shipments' );

function senderzz_stock_session_token_values( string $token ): array {
    $token = sanitize_text_field( $token );
    if ( $token === '' ) return [ '', '' ];
    if ( class_exists( '\\WC_MelhorEnvio\\Portal\\Portal_Auth' ) ) {
        return [ $token, \WC_MelhorEnvio\Portal\Portal_Auth::hash_session_token( $token ) ];
    }
    $salt = defined( 'AUTH_SALT' ) && AUTH_SALT ? AUTH_SALT : wp_salt( 'auth' );
    return [ $token, hash_hmac( 'sha256', $token, $salt ) ];
}


function senderzz_ajax_stock_shipments(): void {
    $op  = sanitize_key( $_POST['op'] ?? 'list' );
    if ( ! ( is_user_logged_in() && current_user_can( 'manage_woocommerce' ) ) ) {
        if ( ! check_ajax_referer( 'senderzz_portal', '_ajax_nonce', false ) ) {
            wp_send_json( [ 'success' => false, 'message' => 'Sessão expirada. Atualize a página e tente novamente.' ], 403 );
        }
    }
    $req = new WP_REST_Request( 'POST', '/senderzz/v1/stock-shipments' );

    foreach ( [ 'id', 'product_id', 'variation_id', 'qty', 'unit_cost', 'tracking', 'carrier', 'qty_received', 'notes', 'items', 'received_items' ] as $k ) {
        if ( isset( $_POST[ $k ] ) ) {
            $req->set_param( $k, wp_unslash( $_POST[ $k ] ) );
        }
    }

    if ( ! senderzz_rest_stock_shipment_permission() ) {
        wp_send_json( [ 'success' => false, 'message' => 'Sessão expirada. Faça login novamente.' ], 401 );
    }

    if ( in_array( $op, [ 'deliver', 'conclude' ], true ) && ! senderzz_rest_stock_shipment_operator_permission() ) {
        wp_send_json( [ 'success' => false, 'message' => 'Sem permissão de operador.' ], 403 );
    }

    if ( $op === 'create' ) {
        $res = senderzz_rest_stock_shipment_create( $req );
    } elseif ( $op === 'confirm' ) {
        $res = senderzz_rest_stock_shipment_confirm( $req );
    } elseif ( $op === 'delete' ) {
        $res = senderzz_rest_stock_shipment_delete( $req );
    } elseif ( $op === 'update' ) {
        $res = senderzz_rest_stock_shipment_update( $req );
    } elseif ( $op === 'deliver' ) {
        $res = senderzz_rest_stock_shipment_deliver( $req );
    } elseif ( $op === 'conclude' ) {
        $res = senderzz_rest_stock_shipment_conclude( $req );
    } else {
        $res = senderzz_rest_stock_shipments_list( $req );
    }

    $data = $res instanceof WP_REST_Response ? $res->get_data() : $res;
    wp_send_json( is_array( $data ) ? $data : [ 'success' => false, 'message' => 'Resposta inválida.' ] );
}

function senderzz_rest_stock_shipment_permission(): bool {
    if ( is_user_logged_in() && current_user_can( 'manage_woocommerce' ) ) return true;
    $token = $_COOKIE['senderzz_portal_session'] ?? ( $_SERVER['HTTP_X_SENDERZZ_TOKEN'] ?? '' );
    if ( ! $token ) return false;
    global $wpdb;
    list( $token_raw, $token_hash ) = senderzz_stock_session_token_values( (string) $token );
    $sess = $wpdb->get_row( $wpdb->prepare( "SELECT user_id FROM {$wpdb->prefix}senderzz_portal_sessions WHERE token IN (%s, %s) AND expires_at > NOW() LIMIT 1", $token_raw, $token_hash ) );
    if ( ! $sess ) return false;
    $user = $wpdb->get_row( $wpdb->prepare( "SELECT id, role, status FROM {$wpdb->prefix}senderzz_portal_users WHERE id = %d AND status = 'active'", (int) $sess->user_id ) );
    return $user && in_array( $user->role, [ 'client', 'operator' ], true );
}

function senderzz_rest_stock_shipment_operator_permission(): bool {
    if ( is_user_logged_in() && current_user_can( 'manage_woocommerce' ) ) return true;
    $user = senderzz_stock_shipment_current_user();
    return $user && (string) $user->role === 'operator';
}

function senderzz_stock_shipment_current_user(): ?object {
    global $wpdb;
    $token = $_COOKIE['senderzz_portal_session'] ?? ( $_SERVER['HTTP_X_SENDERZZ_TOKEN'] ?? '' );
    if ( ! $token && is_user_logged_in() && current_user_can( 'manage_woocommerce' ) ) {
        return (object) [ 'id' => get_current_user_id(), 'wp_user_id' => get_current_user_id(), 'role' => 'admin', 'status' => 'active', 'shipping_class_id' => 0, 'email' => wp_get_current_user()->user_email, 'name' => wp_get_current_user()->display_name ];
    }
    if ( ! $token ) return null;
    list( $token_raw, $token_hash ) = senderzz_stock_session_token_values( (string) $token );
    $sess = $wpdb->get_row( $wpdb->prepare( "SELECT user_id FROM {$wpdb->prefix}senderzz_portal_sessions WHERE token IN (%s, %s) AND expires_at > NOW() LIMIT 1", $token_raw, $token_hash ) );
    if ( ! $sess ) return null;
    return $wpdb->get_row( $wpdb->prepare( "SELECT * FROM {$wpdb->prefix}senderzz_portal_users WHERE id = %d AND status = 'active'", (int) $sess->user_id ) );
}

function senderzz_stock_shipment_parse_items( $raw ): array {
    if ( is_string( $raw ) ) {
        $decoded = json_decode( wp_unslash( $raw ), true );
        $raw = is_array( $decoded ) ? $decoded : [];
    }
    if ( ! is_array( $raw ) ) $raw = [];

    $items = [];
    foreach ( $raw as $it ) {
        if ( ! is_array( $it ) ) continue;
        $pid  = absint( $it['product_id'] ?? $it['pid'] ?? 0 );
        $vid  = absint( $it['variation_id'] ?? $it['vid'] ?? 0 );
        $qty  = max( 1, absint( $it['qty'] ?? $it['qty_sent'] ?? 0 ) );
        $unit = max( 0, (float) ( $it['unit_cost'] ?? $it['cost_unit'] ?? 0 ) );
        if ( ! $pid ) continue;
        $items[] = [ 'product_id' => $pid, 'variation_id' => $vid, 'qty_sent' => $qty, 'unit_cost' => $unit, 'total_cost' => round( $qty * $unit, 2 ) ];
    }
    return $items;
}

function senderzz_stock_shipment_items_from_legacy_request( WP_REST_Request $req ): array {
    $items = senderzz_stock_shipment_parse_items( $req->get_param( 'items' ) );
    if ( $items ) return $items;
    $pid = absint( $req->get_param( 'product_id' ) );
    if ( ! $pid ) return [];
    $qty = max( 1, absint( $req->get_param( 'qty' ) ) );
    $unit = max( 0, (float) $req->get_param( 'unit_cost' ) );
    return [ [ 'product_id' => $pid, 'variation_id' => absint( $req->get_param( 'variation_id' ) ), 'qty_sent' => $qty, 'unit_cost' => $unit, 'total_cost' => round( $qty * $unit, 2 ) ] ];
}

function senderzz_stock_validate_item_for_user( array $item, object $user ): ?string {
    if ( (string) ( $user->role ?? '' ) === 'admin' ) return null;
    $check_id = ! empty( $item['variation_id'] ) ? (int) $item['variation_id'] : (int) $item['product_id'];
    $product  = wc_get_product( $check_id );
    if ( ! $product ) return 'Produto inválido.';
    $_add_class_ids = function_exists('sz_get_user_class_ids') ? sz_get_user_class_ids($user) : [];
    $class_id = (int) ( $user->shipping_class_id ?? 0 );
    if ( empty($_add_class_ids) && $class_id > 0 ) $_add_class_ids = [$class_id];
    if ( ! empty($_add_class_ids) && ! in_array( (int)$product->get_shipping_class_id(), $_add_class_ids, true ) ) return 'Produto fora da sua classe logística.';
    return null;
}

function senderzz_stock_shipment_log( int $shipment_id, string $action, array $data = [] ): void {
    global $wpdb;
    $user = senderzz_stock_shipment_current_user();
    $wpdb->insert( $wpdb->prefix . SENDERZZ_STOCK_SHIP_LOG_TABLE, [
        'shipment_id' => $shipment_id,
        'user_id'     => $user ? (int) $user->id : 0,
        'user_name'   => $user ? sanitize_text_field( $user->name ?? $user->email ?? $user->role ?? 'Sistema' ) : 'Sistema',
        'action'      => sanitize_key( $action ),
        'data_json'   => wp_json_encode( $data, JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES ),
        'created_at'  => current_time( 'mysql', true ),
    ] );
}

function senderzz_rest_stock_shipment_create( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $user = senderzz_stock_shipment_current_user();
    if ( ! $user ) return new WP_REST_Response( [ 'success' => false, 'message' => 'Não autenticado.' ], 401 );
    if ( ! in_array( (string) $user->role, [ 'client', 'admin' ], true ) && ! current_user_can( 'manage_woocommerce' ) ) return new WP_REST_Response( [ 'success' => false, 'message' => 'Somente produtores podem informar envio.' ], 403 );

    $items = senderzz_stock_shipment_items_from_legacy_request( $req );
    if ( ! $items ) return new WP_REST_Response( [ 'success' => false, 'message' => 'Adicione pelo menos um produto ao envio.' ], 400 );

    foreach ( $items as $it ) {
        $err = senderzz_stock_validate_item_for_user( $it, $user );
        if ( $err ) return new WP_REST_Response( [ 'success' => false, 'message' => $err ], 400 );
    }

    $tracking = sanitize_text_field( $req->get_param( 'tracking' ) ?? '' );
    $carrier  = sanitize_text_field( $req->get_param( 'carrier' ) ?? '' );
    $now      = current_time( 'mysql', true );

    $ok = $wpdb->insert( $wpdb->prefix . SENDERZZ_STOCK_SHIP_TABLE, [
        'user_id'    => (int) $user->id,
        'tracking'   => $tracking,
        'carrier'    => $carrier,
        'status'     => 'pendente',
        'created_at' => $now,
        'updated_at' => $now,
        // Compatibilidade: espelha primeiro item nas colunas antigas.
        'product_id'   => (int) $items[0]['product_id'],
        'variation_id' => (int) $items[0]['variation_id'],
        'qty'          => (int) $items[0]['qty_sent'],
        'unit_cost'    => (float) $items[0]['unit_cost'],
        'total_cost'   => (float) array_sum( array_column( $items, 'total_cost' ) ),
    ] );

    if ( ! $ok ) return new WP_REST_Response( [ 'success' => false, 'message' => 'Erro ao salvar envio: ' . ( $wpdb->last_error ?: 'falha desconhecida' ) ], 500 );
    $shipment_id = (int) $wpdb->insert_id;
    foreach ( $items as $it ) {
        $wpdb->insert( $wpdb->prefix . SENDERZZ_STOCK_SHIP_ITEM_TABLE, [
            'shipment_id'  => $shipment_id,
            'product_id'   => (int) $it['product_id'],
            'variation_id' => (int) $it['variation_id'],
            'qty_sent'     => (int) $it['qty_sent'],
            'qty_received' => 0,
            'unit_cost'    => (float) $it['unit_cost'],
            'total_cost'   => (float) $it['total_cost'],
            'created_at'   => $now,
            'updated_at'   => $now,
        ] );
    }
    senderzz_stock_shipment_log( $shipment_id, 'rascunho_criado', [ 'tracking' => $tracking, 'carrier' => $carrier, 'items' => $items ] );
    return new WP_REST_Response( [ 'success' => true, 'id' => $shipment_id, 'message' => 'Rascunho de envio salvo.' ], 200 );
}

function senderzz_rest_stock_shipment_confirm( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $user = senderzz_stock_shipment_current_user();
    if ( ! $user ) return new WP_REST_Response( [ 'success' => false, 'message' => 'Não autenticado.' ], 401 );
    $id = absint( $req->get_param( 'id' ) );
    $row = $wpdb->get_row( $wpdb->prepare( "SELECT * FROM {$wpdb->prefix}" . SENDERZZ_STOCK_SHIP_TABLE . " WHERE id=%d AND user_id=%d", $id, (int) $user->id ), ARRAY_A );
    if ( ! $row ) return new WP_REST_Response( [ 'success' => false, 'message' => 'Envio não encontrado.' ], 404 );
    if ( $row['status'] !== 'pendente' ) return new WP_REST_Response( [ 'success' => false, 'message' => 'Somente rascunhos pendentes podem ser enviados.' ], 400 );
    $wpdb->update( $wpdb->prefix . SENDERZZ_STOCK_SHIP_TABLE, [ 'status' => 'enviado', 'sent_at' => current_time( 'mysql', true ), 'updated_at' => current_time( 'mysql', true ) ], [ 'id' => $id ] );
    $items = senderzz_stock_shipment_get_items( $id );
    senderzz_stock_shipment_log( $id, 'produtor_confirmou_envio', [ 'tracking' => $row['tracking'], 'carrier' => $row['carrier'], 'items' => $items ] );
    return new WP_REST_Response( [ 'success' => true, 'message' => 'Envio confirmado.' ], 200 );
}

function senderzz_rest_stock_shipments_list( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $user = senderzz_stock_shipment_current_user();
    if ( ! $user ) return new WP_REST_Response( [ 'success' => false, 'message' => 'Não autenticado.', 'shipments' => [] ], 401 );
    senderzz_stock_shipments_migrate_legacy_rows();

    $ship = $wpdb->prefix . SENDERZZ_STOCK_SHIP_TABLE;
    if ( senderzz_rest_stock_shipment_operator_permission() ) {
        // Operador vê: (a) todos os status dos próprios envios + (b) enviado/entregue/concluido dos clientes
        $uid = (int) $user->id;
        $rows = $wpdb->get_results( $wpdb->prepare(
            "SELECT s.*, u.name AS user_name, u.email AS user_email
             FROM {$ship} s
             LEFT JOIN {$wpdb->prefix}senderzz_portal_users u ON u.id = s.user_id
             WHERE (s.user_id = %d)
                OR (s.user_id != %d AND s.status IN ('enviado','entregue','concluido'))
             ORDER BY s.updated_at DESC, s.created_at DESC
             LIMIT 300",
            $uid, $uid
        ), ARRAY_A );
    } else {
        $rows = $wpdb->get_results( $wpdb->prepare( "SELECT s.* FROM {$ship} s WHERE s.user_id=%d ORDER BY s.created_at DESC LIMIT 150", (int) $user->id ), ARRAY_A );
    }
    if ( ! is_array( $rows ) ) {
        return new WP_REST_Response( [ 'success' => false, 'message' => 'Erro ao consultar envios: ' . ( $wpdb->last_error ?: 'tabela pode não existir' ), 'shipments' => [] ], 200 );
    }

    foreach ( $rows as &$row ) {
        $row = senderzz_stock_shipment_format_row( $row );
    }
    unset( $row );
    return new WP_REST_Response( [ 'success' => true, 'shipments' => $rows ], 200 );
}

function senderzz_stock_shipment_get_items( int $shipment_id ): array {
    global $wpdb;
    $rows = $wpdb->get_results( $wpdb->prepare( "SELECT * FROM {$wpdb->prefix}" . SENDERZZ_STOCK_SHIP_ITEM_TABLE . " WHERE shipment_id=%d ORDER BY id ASC", $shipment_id ), ARRAY_A );
    if ( ! is_array( $rows ) ) return [];
    foreach ( $rows as &$it ) {
        $pid = ! empty( $it['variation_id'] ) ? (int) $it['variation_id'] : (int) $it['product_id'];
        $product = wc_get_product( $pid );
        $it['product_name'] = $product ? ( function_exists('senderzz_clean_product_label') ? senderzz_clean_product_label( $product->get_name() ) : $product->get_name() ) : 'Produto #' . (int) $it['product_id'];
        $it['qty_sent'] = (int) $it['qty_sent'];
        $it['qty_received'] = (int) $it['qty_received'];
        $it['unit_cost'] = (float) $it['unit_cost'];
        $it['total_cost'] = (float) $it['total_cost'];
        $it['unit_cost_fmt'] = 'R$ ' . number_format( (float) $it['unit_cost'], 2, ',', '.' );
        $it['total_cost_fmt'] = 'R$ ' . number_format( (float) $it['total_cost'], 2, ',', '.' );
        $it['difference'] = (int) $it['qty_received'] - (int) $it['qty_sent'];
    }
    unset( $it );
    return $rows;
}

function senderzz_stock_shipment_get_logs( int $shipment_id ): array {
    global $wpdb;
    $logs = $wpdb->get_results( $wpdb->prepare( "SELECT * FROM {$wpdb->prefix}" . SENDERZZ_STOCK_SHIP_LOG_TABLE . " WHERE shipment_id=%d ORDER BY id ASC", $shipment_id ), ARRAY_A );
    if ( ! is_array( $logs ) ) return [];
    foreach ( $logs as &$log ) {
        $log['data'] = ! empty( $log['data_json'] ) ? ( json_decode( $log['data_json'], true ) ?: [] ) : [];
        $log['at'] = ! empty( $log['created_at'] ) ? wp_date( 'd/m/Y H:i', strtotime( $log['created_at'] . ' UTC' ) ) : '';
        $log['by_name'] = $log['user_name'] ?: 'Sistema';
        unset( $log['data_json'] );
    }
    unset( $log );
    return $logs;
}


function senderzz_stock_product_matches_class( $product, int $class_id ): bool {
    if ( ! $product instanceof \WC_Product ) return false;
    if ( (int) $product->get_shipping_class_id() === $class_id ) return true;
    if ( $product->is_type( 'variation' ) ) {
        $parent = wc_get_product( (int) $product->get_parent_id() );
        if ( $parent instanceof \WC_Product && (int) $parent->get_shipping_class_id() === $class_id ) return true;
    }
    return false;
}

function senderzz_stock_get_movement_rows( $class_id, int $limit = 120 ): array {
    $limit = max( 20, min( 300, $limit ) );
    $rows  = [];

    if ( function_exists( 'wc_get_orders' ) ) {
        $orders = wc_get_orders( [
            'limit'      => $limit,
            'orderby'    => 'date',
            'order'      => 'DESC',
            'return'     => 'objects',
            'meta_query' => [
                [
                    'key'   => '_senderzz_product_shipping_class_id',
                    'value'   => is_array($class_id) ? $class_id : [$class_id],
                    'compare' => is_array($class_id) && count($class_id) > 1 ? 'IN' : '=',
                ],
            ],
            'status'     => array_keys( wc_get_order_statuses() ),
        ] );

        foreach ( (array) $orders as $order ) {
            if ( ! $order instanceof \WC_Order ) continue;
            $status = (string) $order->get_status();
            if ( in_array( $status, [ 'cancelled', 'failed', 'trash', 'checkout-draft', 'draft' ], true ) ) continue;
            $date = $order->get_date_created();
            $timestamp = $date ? (int) $date->getTimestamp() : 0;
            $date_fmt  = $date ? $date->date_i18n( 'd/m/Y H:i' ) : '—';
            $status_label = class_exists( '\WC_MelhorEnvio\Portal\Portal_Orders' )
                ? \WC_MelhorEnvio\Portal\Portal_Orders::get_status_label( $status )
                : $status;
            $customer = trim( (string) $order->get_formatted_billing_full_name() );
            if ( $customer === '' ) $customer = 'Cliente';
            $ref_number = method_exists( $order, 'get_order_number' ) ? (string) $order->get_order_number() : (string) $order->get_id();
            $is_senderzz_offer = (string) $order->get_meta( '_senderzz_offer_token', true ) !== '';
            $product_rows = [];

            foreach ( $order->get_items() as $item ) {
                if ( ! $item instanceof \WC_Order_Item_Product ) continue;
                $product = $item->get_product();
                if ( ! $product instanceof \WC_Product ) continue;
                if ( ! senderzz_stock_product_matches_class( $product, $class_id ) ) continue;

                $base_component = (string) $item->get_meta( '_senderzz_is_base_component', true ) === 'yes';
                if ( $is_senderzz_offer && ! $base_component ) {
                    continue;
                }

                $product_key  = (string) (int) $product->get_id();
                $product_name = function_exists('senderzz_clean_product_label') ? senderzz_clean_product_label( $product->get_name() ) : $product->get_name();
                $qty          = absint( $item->get_quantity() );

                if ( $is_senderzz_offer && $base_component ) {
                    $component_qty = absint( $item->get_meta( '_senderzz_component_qty', true ) );
                    if ( $component_qty > 0 ) {
                        $qty = $component_qty;
                    }
                    if ( isset( $product_rows[ $product_key ] ) ) {
                        $product_rows[ $product_key ]['qty'] = max( (int) $product_rows[ $product_key ]['qty'], $qty );
                        continue;
                    }
                } else {
                    if ( isset( $product_rows[ $product_key ] ) ) {
                        $product_rows[ $product_key ]['qty'] += $qty;
                        continue;
                    }
                }

                if ( $qty < 1 ) continue;
                $product_rows[ $product_key ] = [
                    'timestamp'        => $timestamp,
                    'date_fmt'         => $date_fmt,
                    'direction'        => 'out',
                    'direction_label'  => 'Saída',
                    'source_label'     => 'Pedido #' . $ref_number,
                    'source_meta'      => $customer . ' · ' . $status_label,
                    'product_id'       => (int) $product->get_id(),
                    'product_name'     => $product_name,
                    'qty'              => $qty,
                    'status'           => $status_label,
                ];
            }

            foreach ( $product_rows as $product_row ) {
                $product_row['qty'] = - absint( $product_row['qty'] ?? 0 );
                if ( 0 === (int) $product_row['qty'] ) continue;
                $rows[] = $product_row;
            }
        }
    }

    global $wpdb;
    $ship_table = $wpdb->prefix . SENDERZZ_STOCK_SHIP_TABLE;
    $item_table = $wpdb->prefix . SENDERZZ_STOCK_SHIP_ITEM_TABLE;
    $ship_rows  = $wpdb->get_results(
        $wpdb->prepare(
            "SELECT s.id AS shipment_id, s.status, s.carrier, s.tracking, s.concluded_at, s.delivered_at, s.created_at,
                    i.product_id, i.variation_id, i.qty_sent, i.qty_received
             FROM {$ship_table} s
             INNER JOIN {$item_table} i ON i.shipment_id = s.id
             WHERE s.status = 'concluido'
             ORDER BY COALESCE(s.concluded_at, s.delivered_at, s.created_at) DESC
             LIMIT %d",
            $limit * 3
        ),
        ARRAY_A
    );

    foreach ( (array) $ship_rows as $row ) {
        $product_id = ! empty( $row['variation_id'] ) ? (int) $row['variation_id'] : (int) $row['product_id'];
        $product = wc_get_product( $product_id );
        if ( ! $product instanceof \WC_Product ) continue;
        if ( ! senderzz_stock_product_matches_class( $product, $class_id ) ) continue;
        $qty = absint( ! empty( $row['qty_received'] ) ? $row['qty_received'] : $row['qty_sent'] );
        if ( $qty < 1 ) continue;
        $when = ! empty( $row['concluded_at'] ) ? $row['concluded_at'] : ( ! empty( $row['delivered_at'] ) ? $row['delivered_at'] : $row['created_at'] );
        $ts   = $when ? strtotime( $when . ' UTC' ) : 0;
        $rows[] = [
            'timestamp'    => (int) $ts,
            'date_fmt'     => $when ? wp_date( 'd/m/Y H:i', $ts ) : '—',
            'direction'    => 'in',
            'direction_label' => 'Entrada',
            'source_label' => 'Envio #' . (int) $row['shipment_id'],
            'source_meta'  => trim( (string) ( $row['carrier'] ?: 'Reposição' ) . ( ! empty( $row['tracking'] ) ? ' · ' . $row['tracking'] : '' ) ),
            'product_id'    => (int) $product->get_id(),
            'product_name' => ( function_exists('senderzz_clean_product_label') ? senderzz_clean_product_label( $product->get_name() ) : $product->get_name() ),
            'qty'          => $qty,
            'status'       => 'Concluído',
        ];
    }

    usort( $rows, static function( $a, $b ) {
        return (int) ( $b['timestamp'] ?? 0 ) <=> (int) ( $a['timestamp'] ?? 0 );
    } );

    // Saldo antes/depois por produto.
    // Como a lista está do mais novo para o mais antigo, partimos do estoque atual
    // e voltamos no tempo. Assim o produtor enxerga a trilha operacional de cada SKU.
    $running_stock = [];
    foreach ( $rows as $idx => $row ) {
        $pid = (int) ( $row['product_id'] ?? 0 );
        if ( $pid <= 0 ) {
            $rows[ $idx ]['stock_before'] = null;
            $rows[ $idx ]['stock_after']  = null;
            continue;
        }

        if ( ! array_key_exists( $pid, $running_stock ) ) {
            $product = wc_get_product( $pid );
            $running_stock[ $pid ] = $product instanceof \WC_Product ? (int) $product->get_stock_quantity() : 0;
        }

        $after  = (int) $running_stock[ $pid ];
        $delta  = (int) ( $row['qty'] ?? 0 );
        $before = $after - $delta;

        $rows[ $idx ]['stock_before'] = $before;
        $rows[ $idx ]['stock_after']  = $after;

        $running_stock[ $pid ] = $before;
    }

    return array_slice( $rows, 0, $limit );
}

function senderzz_stock_shipment_format_row( array $row ): array {
    $items = senderzz_stock_shipment_get_items( (int) $row['id'] );
    $qty_sent = array_sum( array_map( fn( $it ) => (int) $it['qty_sent'], $items ) );
    $qty_received = array_sum( array_map( fn( $it ) => (int) $it['qty_received'], $items ) );
    $total_cost = array_sum( array_map( fn( $it ) => (float) $it['total_cost'], $items ) );
    $first = $items[0] ?? [];
    $row['items'] = $items;
    $row['items_count'] = count( $items );
    $row['product_name'] = count( $items ) > 1 ? count( $items ) . ' produtos no envio' : ( $first['product_name'] ?? '—' );
    $row['items_summary'] = implode( ', ', array_map( fn( $it ) => $it['product_name'] . ' × ' . (int) $it['qty_sent'], $items ) );
    $row['qty'] = $qty_sent;
    $row['qty_received'] = $qty_received;
    $row['unit_cost'] = $first['unit_cost'] ?? 0;
    $row['total_cost'] = $total_cost;
    $row['unit_cost_fmt'] = count( $items ) > 1 ? '—' : ( $first['unit_cost_fmt'] ?? '—' );
    $row['total_cost_fmt'] = 'R$ ' . number_format( $total_cost, 2, ',', '.' );
    $row['created_at_fmt'] = ! empty( $row['created_at'] ) ? wp_date( 'd/m/Y H:i', strtotime( $row['created_at'] . ' UTC' ) ) : '—';
    $row['sent_at_fmt'] = ! empty( $row['sent_at'] ) ? wp_date( 'd/m/Y H:i', strtotime( $row['sent_at'] . ' UTC' ) ) : '—';
    $row['delivered_at_fmt'] = ! empty( $row['delivered_at'] ) ? wp_date( 'd/m/Y H:i', strtotime( $row['delivered_at'] . ' UTC' ) ) : '—';
    $row['concluded_at_fmt'] = ! empty( $row['concluded_at'] ) ? wp_date( 'd/m/Y H:i', strtotime( $row['concluded_at'] . ' UTC' ) ) : '—';
    $row['history'] = senderzz_stock_shipment_get_logs( (int) $row['id'] );
    return $row;
}

function senderzz_rest_stock_shipment_delete( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $user = senderzz_stock_shipment_current_user();
    if ( ! $user ) return new WP_REST_Response( [ 'success' => false, 'message' => 'Não autenticado.' ], 401 );
    $id = absint( $req->get_param( 'id' ) );
    $row = $wpdb->get_row( $wpdb->prepare( "SELECT status FROM {$wpdb->prefix}" . SENDERZZ_STOCK_SHIP_TABLE . " WHERE id=%d AND user_id=%d", $id, (int) $user->id ), ARRAY_A );
    if ( ! $row ) return new WP_REST_Response( [ 'success' => false, 'message' => 'Não encontrado.' ], 404 );
    if ( $row['status'] !== 'pendente' ) return new WP_REST_Response( [ 'success' => false, 'message' => 'Somente rascunhos pendentes podem ser removidos.' ], 400 );
    $wpdb->delete( $wpdb->prefix . SENDERZZ_STOCK_SHIP_ITEM_TABLE, [ 'shipment_id' => $id ] );
    $wpdb->delete( $wpdb->prefix . SENDERZZ_STOCK_SHIP_LOG_TABLE, [ 'shipment_id' => $id ] );
    $wpdb->delete( $wpdb->prefix . SENDERZZ_STOCK_SHIP_TABLE, [ 'id' => $id, 'user_id' => (int) $user->id ] );
    return new WP_REST_Response( [ 'success' => true ], 200 );
}

function senderzz_rest_stock_shipment_update( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $user = senderzz_stock_shipment_current_user();
    if ( ! $user ) return new WP_REST_Response( [ 'success' => false, 'message' => 'Não autenticado.' ], 401 );
    $id = absint( $req->get_param( 'id' ) );
    $row = $wpdb->get_row( $wpdb->prepare( "SELECT * FROM {$wpdb->prefix}" . SENDERZZ_STOCK_SHIP_TABLE . " WHERE id=%d AND user_id=%d", $id, (int) $user->id ), ARRAY_A );
    if ( ! $row ) return new WP_REST_Response( [ 'success' => false, 'message' => 'Envio não encontrado.' ], 404 );
    if ( $row['status'] !== 'pendente' ) return new WP_REST_Response( [ 'success' => false, 'message' => 'Somente envios pendentes podem ser editados.' ], 400 );

    $items = senderzz_stock_shipment_items_from_legacy_request( $req );
    if ( ! $items ) return new WP_REST_Response( [ 'success' => false, 'message' => 'Adicione pelo menos um produto ao envio.' ], 400 );
    foreach ( $items as $it ) {
        $err = senderzz_stock_validate_item_for_user( $it, $user );
        if ( $err ) return new WP_REST_Response( [ 'success' => false, 'message' => $err ], 400 );
    }
    $tracking = sanitize_text_field( $req->get_param( 'tracking' ) ?? '' );
    $carrier = sanitize_text_field( $req->get_param( 'carrier' ) ?? '' );
    $now = current_time( 'mysql', true );
    $wpdb->update( $wpdb->prefix . SENDERZZ_STOCK_SHIP_TABLE, [ 'tracking' => $tracking, 'carrier' => $carrier, 'updated_at' => $now, 'product_id' => (int) $items[0]['product_id'], 'variation_id' => (int) $items[0]['variation_id'], 'qty' => (int) $items[0]['qty_sent'], 'unit_cost' => (float) $items[0]['unit_cost'], 'total_cost' => array_sum( array_column( $items, 'total_cost' ) ) ], [ 'id' => $id ] );
    $wpdb->delete( $wpdb->prefix . SENDERZZ_STOCK_SHIP_ITEM_TABLE, [ 'shipment_id' => $id ] );
    foreach ( $items as $it ) {
        $wpdb->insert( $wpdb->prefix . SENDERZZ_STOCK_SHIP_ITEM_TABLE, [ 'shipment_id' => $id, 'product_id' => (int) $it['product_id'], 'variation_id' => (int) $it['variation_id'], 'qty_sent' => (int) $it['qty_sent'], 'qty_received' => 0, 'unit_cost' => (float) $it['unit_cost'], 'total_cost' => (float) $it['total_cost'], 'created_at' => $now, 'updated_at' => $now ] );
    }
    senderzz_stock_shipment_log( $id, 'produtor_editou_rascunho', [ 'tracking' => $tracking, 'carrier' => $carrier, 'items' => $items ] );
    return new WP_REST_Response( [ 'success' => true, 'message' => 'Envio atualizado.' ], 200 );
}

function senderzz_stock_parse_received_items( $raw, array $items ): array {
    if ( is_string( $raw ) ) {
        $decoded = json_decode( wp_unslash( $raw ), true );
        $raw = is_array( $decoded ) ? $decoded : [];
    }
    $received = [];
    if ( is_array( $raw ) ) {
        foreach ( $raw as $k => $v ) {
            if ( is_array( $v ) ) {
                $id = absint( $v['id'] ?? $v['item_id'] ?? $k );
                $qty = absint( $v['qty_received'] ?? $v['qty'] ?? 0 );
            } else {
                $id = absint( $k );
                $qty = absint( $v );
            }
            if ( $id ) $received[ $id ] = $qty;
        }
    }
    foreach ( $items as $it ) {
        if ( ! isset( $received[ (int) $it['id'] ] ) ) {
            $received[ (int) $it['id'] ] = (int) $it['qty_sent'];
        }
    }
    return $received;
}

function senderzz_rest_stock_shipment_deliver( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $id = absint( $req->get_param( 'id' ) );
    $notes = sanitize_textarea_field( $req->get_param( 'notes' ) ?? '' );
    $row = $wpdb->get_row( $wpdb->prepare( "SELECT * FROM {$wpdb->prefix}" . SENDERZZ_STOCK_SHIP_TABLE . " WHERE id=%d", $id ), ARRAY_A );
    if ( ! $row ) return new WP_REST_Response( [ 'success' => false, 'message' => 'Não encontrado.' ], 404 );
    if ( $row['status'] !== 'enviado' ) return new WP_REST_Response( [ 'success' => false, 'message' => 'Somente envios com status enviado podem ser marcados como entregues.' ], 400 );
    $items = senderzz_stock_shipment_get_items( $id );
    if ( ! $items ) return new WP_REST_Response( [ 'success' => false, 'message' => 'Envio sem itens.' ], 400 );
    $received = senderzz_stock_parse_received_items( $req->get_param( 'received_items' ), $items );
    foreach ( $items as $it ) {
        $wpdb->update( $wpdb->prefix . SENDERZZ_STOCK_SHIP_ITEM_TABLE, [ 'qty_received' => absint( $received[ (int) $it['id'] ] ?? $it['qty_sent'] ), 'updated_at' => current_time( 'mysql', true ) ], [ 'id' => (int) $it['id'] ] );
    }
    $wpdb->update( $wpdb->prefix . SENDERZZ_STOCK_SHIP_TABLE, [ 'status' => 'entregue', 'qty_received' => array_sum( $received ), 'notes' => trim( (string) ( $row['notes'] ?? '' ) . ( $notes ? "\n[Entrega] " . $notes : '' ) ), 'delivered_at' => current_time( 'mysql', true ), 'updated_at' => current_time( 'mysql', true ) ], [ 'id' => $id ] );
    senderzz_stock_shipment_log( $id, 'operador_confirmou_recebimento', [ 'items_received' => $received, 'notes' => $notes ] );
    return new WP_REST_Response( [ 'success' => true, 'message' => 'Marcado como entregue. Confira as quantidades e conclua para subir o estoque.' ], 200 );
}

function senderzz_rest_stock_shipment_conclude( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $id = absint( $req->get_param( 'id' ) );
    $notes = sanitize_textarea_field( $req->get_param( 'notes' ) ?? '' );
    $row = $wpdb->get_row( $wpdb->prepare( "SELECT * FROM {$wpdb->prefix}" . SENDERZZ_STOCK_SHIP_TABLE . " WHERE id=%d", $id ), ARRAY_A );
    if ( ! $row ) return new WP_REST_Response( [ 'success' => false, 'message' => 'Não encontrado.' ], 404 );
    if ( $row['status'] !== 'entregue' ) return new WP_REST_Response( [ 'success' => false, 'message' => 'Somente envios entregues podem ser concluídos.' ], 400 );
    $items = senderzz_stock_shipment_get_items( $id );
    if ( ! $items ) return new WP_REST_Response( [ 'success' => false, 'message' => 'Envio sem itens.' ], 400 );
    $received = senderzz_stock_parse_received_items( $req->get_param( 'received_items' ), $items );
    $added = [];
    foreach ( $items as $it ) {
        $item_id = (int) $it['id'];
        $qty = absint( $received[ $item_id ] ?? $it['qty_received'] ?? $it['qty_sent'] );
        $product_id = ! empty( $it['variation_id'] ) ? (int) $it['variation_id'] : (int) $it['product_id'];
        $product = wc_get_product( $product_id );
        if ( $product && empty( $it['stock_added'] ) ) {
            if ( ! $product->managing_stock() ) { $product->set_manage_stock( true ); $product->save(); }
            if ( $qty > 0 ) wc_update_product_stock( $product, $qty, 'increase' );
            if ( (float) $it['unit_cost'] > 0 ) {
                update_post_meta( $product_id, 'custo_produto', (float) $it['unit_cost'] );
                update_post_meta( $product_id, 'custo_produto_updated_at', current_time( 'mysql', true ) );
            }
        }
        $wpdb->update( $wpdb->prefix . SENDERZZ_STOCK_SHIP_ITEM_TABLE, [ 'qty_received' => $qty, 'stock_added' => 1, 'updated_at' => current_time( 'mysql', true ) ], [ 'id' => $item_id ] );
        $added[] = [ 'item_id' => $item_id, 'product_id' => $product_id, 'product_name' => $it['product_name'] ?? '', 'qty_sent' => (int) $it['qty_sent'], 'qty_received' => $qty, 'difference' => $qty - (int) $it['qty_sent'] ];
    }
    $wpdb->update( $wpdb->prefix . SENDERZZ_STOCK_SHIP_TABLE, [ 'status' => 'concluido', 'qty_received' => array_sum( $received ), 'notes' => trim( (string) ( $row['notes'] ?? '' ) . ( $notes ? "\n[Conclusão] " . $notes : '' ) ), 'concluded_at' => current_time( 'mysql', true ), 'updated_at' => current_time( 'mysql', true ) ], [ 'id' => $id ] );
    senderzz_stock_shipment_log( $id, 'operador_concluiu_recebimento', [ 'items_confirmed' => $added, 'notes' => $notes ] );
    return new WP_REST_Response( [ 'success' => true, 'message' => 'Concluído. Estoque atualizado por item.', 'items' => $added ], 200 );
}
