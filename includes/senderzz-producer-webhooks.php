<?php
/**
 * Senderzz Producer Webhooks
 * Webhooks por produtor e classe de entrega, sem expor origem/custos internos.
 */
defined( 'ABSPATH' ) || exit;
if ( defined( 'SENDERZZ_PRODUCER_WEBHOOKS_LOADED' ) ) return;
define( 'SENDERZZ_PRODUCER_WEBHOOKS_LOADED', true );

function senderzz_pw_table(): string {
    global $wpdb;
    return $wpdb->prefix . 'senderzz_webhooks';
}

function senderzz_pw_log_table(): string {
    global $wpdb;
    return $wpdb->prefix . 'senderzz_webhook_log';
}

// Instalação sob demanda/ativação: não rodar schema em todo request.
function senderzz_pw_install_tables(): void {
    global $wpdb;
    static $senderzz_pw_install_ran = false;
    if ( $senderzz_pw_install_ran ) return;
    $senderzz_pw_install_ran = true;

    $charset = $wpdb->get_charset_collate();
    $table = senderzz_pw_table();
    $log = senderzz_pw_log_table();

    require_once ABSPATH . 'wp-admin/includes/upgrade.php';

    $table_exists = ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $table ) ) === $table );
    if ( ! $table_exists ) {
        dbDelta( "CREATE TABLE {$table} (
            id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
            user_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
            portal_user_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
            shipping_class_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
            url VARCHAR(512) NOT NULL,
            secret VARCHAR(128) NOT NULL DEFAULT '',
            active TINYINT(1) NOT NULL DEFAULT 1,
            last_status VARCHAR(30) NOT NULL DEFAULT '',
            last_error TEXT NULL,
            last_fired_at DATETIME NULL,
            created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
            updated_at DATETIME NULL,
            PRIMARY KEY (id),
            KEY idx_user (user_id),
            KEY idx_portal_user (portal_user_id),
            KEY idx_class (shipping_class_id),
            UNIQUE KEY uniq_user_class (user_id, shipping_class_id)
        ) {$charset};" );
    } else {
        $cols = $wpdb->get_col( "SHOW COLUMNS FROM {$table}", 0 );
        $cols = is_array( $cols ) ? $cols : [];
        $alter = [];
        if ( ! in_array( 'user_id', $cols, true ) ) $alter[] = "ADD user_id BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER id";
        if ( ! in_array( 'portal_user_id', $cols, true ) ) $alter[] = "ADD portal_user_id BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER user_id";
        if ( ! in_array( 'shipping_class_id', $cols, true ) ) $alter[] = "ADD shipping_class_id BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER portal_user_id";
        if ( ! in_array( 'last_status', $cols, true ) ) $alter[] = "ADD last_status VARCHAR(30) NOT NULL DEFAULT '' AFTER active";
        if ( ! in_array( 'last_error', $cols, true ) ) $alter[] = "ADD last_error TEXT NULL AFTER last_status";
        if ( ! in_array( 'last_fired_at', $cols, true ) ) $alter[] = "ADD last_fired_at DATETIME NULL AFTER last_error";
        if ( ! in_array( 'updated_at', $cols, true ) ) $alter[] = "ADD updated_at DATETIME NULL AFTER created_at";
        if ( $alter ) $wpdb->query( "ALTER TABLE {$table} " . implode( ', ', $alter ) );
    }

    $log_exists = ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $log ) ) === $log );
    if ( ! $log_exists ) {
        dbDelta( "CREATE TABLE {$log} (
            id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
            webhook_id BIGINT UNSIGNED NOT NULL,
            user_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
            shipping_class_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
            order_id BIGINT UNSIGNED NOT NULL,
            event VARCHAR(60) NOT NULL,
            payload_json LONGTEXT NULL,
            response_code SMALLINT NULL,
            response_body TEXT NULL,
            reprocess_count INT UNSIGNED NOT NULL DEFAULT 0,
            last_reprocessed_at DATETIME NULL,
            fired_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
            PRIMARY KEY (id),
            KEY idx_webhook (webhook_id),
            KEY idx_user (user_id),
            KEY idx_order (order_id),
            KEY idx_fired (fired_at)
        ) {$charset};" );
    } else {
        $log_cols = $wpdb->get_col( "SHOW COLUMNS FROM {$log}", 0 );
        $log_cols = is_array( $log_cols ) ? $log_cols : [];
        $log_alter = [];
        if ( ! in_array( 'reprocess_count', $log_cols, true ) ) $log_alter[] = "ADD reprocess_count INT UNSIGNED NOT NULL DEFAULT 0 AFTER response_body";
        if ( ! in_array( 'last_reprocessed_at', $log_cols, true ) ) $log_alter[] = "ADD last_reprocessed_at DATETIME NULL AFTER reprocess_count";
        if ( $log_alter ) $wpdb->query( "ALTER TABLE {$log} " . implode( ', ', $log_alter ) );
    }
}

function senderzz_pw_format_money( float $value ): string {
    return 'R$ ' . number_format( $value, 2, ',', '.' );
}

function senderzz_pw_get_order_class( WC_Order $order ): array {
    if ( function_exists( 'senderzz_operator_order_class_info' ) ) {
        $c = senderzz_operator_order_class_info( $order );
        return [ 'id' => (int) ( $c['id'] ?? 0 ), 'name' => (string) ( $c['name'] ?? 'Sem classe' ), 'slug' => (string) ( $c['slug'] ?? 'sem-classe' ) ];
    }
    $class_id = (int) $order->get_meta( '_senderzz_product_shipping_class_id' );
    $name = '';
    if ( $class_id > 0 ) {
        $term = get_term( $class_id, 'product_shipping_class' );
        if ( $term && ! is_wp_error( $term ) ) $name = $term->name;
    }
    if ( $class_id <= 0 ) {
        foreach ( $order->get_items() as $item ) {
            $product = $item->get_product();
            if ( ! $product ) continue;
            $cid = (int) $product->get_shipping_class_id();
            if ( $cid > 0 ) {
                $term = get_term( $cid, 'product_shipping_class' );
                $class_id = $cid;
                $name = ( $term && ! is_wp_error( $term ) ) ? $term->name : '#' . $cid;
                break;
            }
        }
    }
    return [ 'id' => $class_id, 'name' => $class_id > 0 ? ( $name ?: '#' . $class_id ) : 'Sem classe', 'slug' => $class_id > 0 ? sanitize_title( $name ?: 'classe-' . $class_id ) : 'sem-classe' ];
}

function senderzz_pw_get_owner_user_id_for_order( WC_Order $order, int $class_id ): int {
    $map = get_option( 'senderzz_shipping_class_wallet_owners', [] );
    if ( is_array( $map ) && ! empty( $map[ $class_id ] ) ) return (int) $map[ $class_id ];
    if ( is_array( $map ) && ! empty( $map[ (string) $class_id ] ) ) return (int) $map[ (string) $class_id ];
    return (int) $order->get_customer_id();
}


/**
 * Contexto seguro de afiliação do pedido para webhooks.
 * O afiliado só pode receber evento quando o pedido contém _sz_affiliate_id/_sz_affiliate_ref
 * apontando para a linha ativa dele. Sem ref, não há disparo para afiliado.
 */
function senderzz_pw_get_order_affiliate_context( WC_Order $order ): array {
    $affiliate_id = absint( $order->get_meta( '_sz_affiliate_id', true ) ?: $order->get_meta( '_sz_affiliate_ref', true ) );
    if ( $affiliate_id <= 0 || ! function_exists( 'sz_aff_get_affiliate_row' ) ) {
        return [ 'valid' => false, 'affiliate_id' => 0, 'affiliate_user_id' => 0, 'producer_id' => 0, 'row' => null ];
    }

    $aff = sz_aff_get_affiliate_row( $affiliate_id );
    if ( ! is_array( $aff ) || empty( $aff['id'] ) ) {
        return [ 'valid' => false, 'affiliate_id' => $affiliate_id, 'affiliate_user_id' => 0, 'producer_id' => 0, 'row' => null ];
    }

    $status     = (string) ( $aff['status'] ?? '' );
    $deleted_at = trim( (string) ( $aff['deleted_at'] ?? '' ) );
    $aff_user   = absint( $aff['user_id'] ?? 0 );
    $producer   = absint( $aff['producer_id'] ?? 0 );

    if ( $status !== 'active' || $deleted_at !== '' || $aff_user <= 0 ) {
        return [ 'valid' => false, 'affiliate_id' => $affiliate_id, 'affiliate_user_id' => $aff_user, 'producer_id' => $producer, 'row' => $aff ];
    }

    // Se o pedido já gravou o user_id do afiliado, ele precisa bater com a linha da afiliação.
    $order_aff_user = absint( $order->get_meta( '_sz_affiliate_user_id', true ) );
    if ( $order_aff_user > 0 && $order_aff_user !== $aff_user ) {
        return [ 'valid' => false, 'affiliate_id' => $affiliate_id, 'affiliate_user_id' => $aff_user, 'producer_id' => $producer, 'row' => $aff ];
    }

    // Se o pedido gravou o produtor da comissão, ele precisa bater com a linha da afiliação.
    $order_producer = absint( $order->get_meta( '_sz_aff_producer_id', true ) );
    if ( $order_producer > 0 && $producer > 0 && $order_producer !== $producer ) {
        return [ 'valid' => false, 'affiliate_id' => $affiliate_id, 'affiliate_user_id' => $aff_user, 'producer_id' => $producer, 'row' => $aff ];
    }

    return [ 'valid' => true, 'affiliate_id' => $affiliate_id, 'affiliate_user_id' => $aff_user, 'producer_id' => $producer, 'row' => $aff ];
}
function senderzz_pw_public_order_meta_key( int $class_id ): string {
    return '_senderzz_public_class_order_number_' . max( 0, $class_id );
}

function senderzz_pw_get_public_order_number( WC_Order $order, int $class_id = 0 ): int {
    $class_id = max( 0, $class_id );
    $meta_key = senderzz_pw_public_order_meta_key( $class_id );
    $existing = absint( $order->get_meta( $meta_key, true ) );
    if ( $existing > 0 ) return $existing;

    $option = 'senderzz_public_order_seq_class_' . $class_id;
    $lock_key = 'senderzz_public_order_seq_lock_' . $class_id;
    $tries = 0;
    while ( get_transient( $lock_key ) && $tries < 20 ) {
        usleep( 50000 );
        $tries++;
    }
    set_transient( $lock_key, 1, 10 );
    $next = absint( get_option( $option, 0 ) ) + 1;
    update_option( $option, $next, false );
    delete_transient( $lock_key );

    $order->update_meta_data( $meta_key, $next );
    $order->update_meta_data( '_senderzz_public_order_class_id', $class_id );
    $order->save();
    return $next;
}

function senderzz_pw_status_should_dispatch( string $status ): bool {
    $blocked = [ 'trash', 'checkout-draft', 'draft', 'auto-draft' ];
    return ! in_array( str_replace( 'wc-', '', $status ), $blocked, true );
}

function senderzz_pw_status_event_name( string $status ): string {
    return 'order_status_' . sanitize_key( str_replace( 'wc-', '', $status ) );
}

function senderzz_pw_status_is_active( string $status ): bool {
    $inactive = [ 'cancelled', 'failed', 'refunded', 'trash', 'checkout-draft', 'draft', 'auto-draft' ];
    return ! in_array( str_replace( 'wc-', '', $status ), $inactive, true );
}

function senderzz_pw_tracking_url( string $code ): string {
    $code = trim( $code );
    if ( $code === '' ) return '';
    return home_url( '/rastreio/' . rawurlencode( $code ) . '/' );
}

function senderzz_pw_build_payload( WC_Order $order, string $event = 'pedido_atualizado' ): array {
    $class  = senderzz_pw_get_order_class( $order );
    $status = $order->get_status();

    // ── Valores financeiros ───────────────────────────────────────────────
    $frete_val    = (float) $order->get_shipping_total() + (float) $order->get_shipping_tax();
    $desconto_val = (float) $order->get_discount_total() + (float) $order->get_discount_tax();
    // Total dos produtos sem frete
    $subtotal_val = (float) $order->get_subtotal();
    // Total geral (subtotal + frete - desconto) — sem duplicar o frete
    $total_val    = (float) $order->get_total();

    // ── Rastreamento ─────────────────────────────────────────────────────
    $tracking = [];
    $raw_tracking = $order->get_meta( '_melhor_envio_tracking_codes' );
    if ( is_array( $raw_tracking ) ) {
        foreach ( $raw_tracking as $t ) $tracking[] = is_array( $t ) ? ( $t['tracking'] ?? $t['code'] ?? '' ) : (string) $t;
    } elseif ( $raw_tracking ) {
        $tracking[] = (string) $raw_tracking;
    }
    $tracking = array_values( array_filter( $tracking ) );

    // Link de rastreamento — página white-label do Senderzz
    $tracking_url = '';
    if ( ! empty( $tracking[0] ) ) {
        $tracking_url = senderzz_pw_tracking_url( (string) $tracking[0] );
    }

    // ── Itens ─────────────────────────────────────────────────────────────
    $items = [];
    foreach ( $order->get_items() as $item ) {
        $items[] = [
            'nome'       => ( function_exists('senderzz_clean_product_label') ? senderzz_clean_product_label( $item->get_name() ) : $item->get_name() ),
            'quantidade' => (int) $item->get_quantity(),
            'subtotal'   => (float) $item->get_subtotal(),
        ];
    }

    // ── Transportadora e serviço ──────────────────────────────────────────
    $transportadora = '';
    $servico        = '';
    foreach ( $order->get_items( 'shipping' ) as $shipping_item ) {
        $shipping_name = $shipping_item->get_name();
        if ( strpos( $shipping_name, ' - ' ) !== false ) {
            [ $transportadora, $servico ] = array_map( 'trim', explode( ' - ', $shipping_name, 2 ) );
        } else {
            $transportadora = $shipping_name;
        }
        break;
    }

    // ── Prazo de entrega ──────────────────────────────────────────────────
    $prazo_entrega = trim( (string) $order->get_meta( '_senderzz_delivery_time' ) );
    if ( ! $prazo_entrega ) {
        foreach ( $order->get_items( 'shipping' ) as $shi ) {
            $prazo_entrega = (string) $shi->get_meta( 'melhorenvio_delivery_time' );
            if ( $prazo_entrega ) break;
        }
    }

    // ── Datas do ciclo de vida ────────────────────────────────────────────
    $criado_em   = $order->get_date_created() ? $order->get_date_created()->date( 'c' ) : '';
    $pago_em     = $order->get_date_paid()    ? $order->get_date_paid()->date( 'c' )    : '';
    $enviado_em  = $order->get_meta( '_senderzz_operator_sent_at' ) ?: $order->get_meta( '_melhor_envio_posted_at' ) ?: '';
    $entregue_em = $order->get_meta( '_melhor_envio_delivered_at' ) ?: '';
    if ( $enviado_em )  $enviado_em  = wp_date( 'c', strtotime( $enviado_em ) ) ?: $enviado_em;
    if ( $entregue_em ) $entregue_em = wp_date( 'c', strtotime( $entregue_em ) ) ?: $entregue_em;

    // ── Pagamento ─────────────────────────────────────────────────────────
    $metodo_pagamento = $order->get_payment_method_title() ?: $order->get_payment_method();

    // ── Telefone sem DDI 55 ───────────────────────────────────────────────
    $telefone_raw = preg_replace( '/\D/', '', $order->get_billing_phone() );
    // Remove +55 ou 55 do início se presente e o número tiver 12+ dígitos
    if ( strlen( $telefone_raw ) >= 12 && strpos( $telefone_raw, '55' ) === 0 ) {
        $telefone_sem_ddi = substr( $telefone_raw, 2 );
    } else {
        $telefone_sem_ddi = $telefone_raw;
    }

    // ── Número do pedido público ──────────────────────────────────────────
    $public_order_number = senderzz_pw_get_public_order_number( $order, (int) $class['id'] );

    return [
        'event'        => $event,
        'status_ativo' => senderzz_pw_status_is_active( $status ),
        'pedido'       => [
            'id'               => (int) $public_order_number,
            'numero'           => (string) $public_order_number,
            'status'           => $status,
            'subtotal'         => $subtotal_val,
            'subtotal_formatado' => senderzz_pw_format_money( $subtotal_val ),
            'total'            => $total_val,
            'total_formatado'  => senderzz_pw_format_money( $total_val ),
            'desconto'         => $desconto_val,
            'desconto_formatado' => $desconto_val > 0 ? senderzz_pw_format_money( $desconto_val ) : '',
            'metodo_pagamento' => $metodo_pagamento,
            'criado_em'        => $criado_em,
            'atualizado_em'    => current_time( 'c' ),
            'pago_em'          => $pago_em,
            'enviado_em'       => $enviado_em,
            'entregue_em'      => $entregue_em,
        ],
        'classe_entrega' => [
            'id'   => (int) $class['id'],
            'nome' => (string) $class['name'],
            'slug' => (string) $class['slug'],
        ],
        'frete' => [
            'valor'           => $frete_val,
            'valor_formatado' => senderzz_pw_format_money( $frete_val ),
            'prazo_dias_uteis'=> $prazo_entrega ? (int) $prazo_entrega : null,
            'transportadora'  => $transportadora,
            'servico'         => $servico,
        ],
        'cliente' => [
            'nome'     => trim( $order->get_billing_first_name() . ' ' . $order->get_billing_last_name() ),
            'telefone' => $telefone_sem_ddi,
            'telefone_completo' => $telefone_raw,
            'email'    => $order->get_billing_email(),
            'cpf'      => $order->get_meta( '_billing_cpf' ) ?: $order->get_meta( 'billing_cpf' ) ?: $order->get_meta( '_billing_cnpj' ) ?: '',
        ],
        'entrega' => [
            'nome'        => trim( $order->get_shipping_first_name() . ' ' . $order->get_shipping_last_name() ) ?: trim( $order->get_billing_first_name() . ' ' . $order->get_billing_last_name() ),
            'cep'         => preg_replace( '/\D/', '', $order->get_shipping_postcode() ?: $order->get_billing_postcode() ),
            'endereco'    => $order->get_shipping_address_1() ?: $order->get_billing_address_1(),
            'numero'      => $order->get_meta( '_billing_number' ) ?: $order->get_meta( '_shipping_number' ),
            'complemento' => $order->get_shipping_address_2() ?: $order->get_billing_address_2(),
            'bairro'      => $order->get_meta( '_shipping_neighborhood' ) ?: $order->get_meta( '_billing_neighborhood' ),
            'cidade'      => $order->get_shipping_city() ?: $order->get_billing_city(),
            'estado'      => $order->get_shipping_state() ?: $order->get_billing_state(),
        ],
        'rastreamento'    => $tracking,
        'link_rastreamento' => $tracking_url,
        'itens'           => $items,
        // Mantidos na raiz para compatibilidade retroativa
        'transportadora'  => $transportadora,
        'servico'         => $servico,
    ];
}

function senderzz_pw_get_classes_for_select(): array {
    $terms = get_terms( [ 'taxonomy' => 'product_shipping_class', 'hide_empty' => false ] );
    if ( is_wp_error( $terms ) ) $terms = [];
    $classes = [ [ 'id' => 0, 'name' => 'Produtos sem classe', 'slug' => 'sem-classe' ] ];
    foreach ( $terms as $term ) {
        $classes[] = [ 'id' => (int) $term->term_id, 'name' => $term->name, 'slug' => $term->slug ];
    }
    return $classes;
}

add_action( 'rest_api_init', function() {
    register_rest_route( 'tp-carteira/v1', '/webhooks/classes', [
        'methods' => 'GET',
        'callback' => function() { return new WP_REST_Response( [ 'classes' => senderzz_pw_get_classes_for_select() ] ); },
        'permission_callback' => 'tpc_rest_auth',
    ] );

    register_rest_route( 'tp-carteira/v1', '/webhooks', [
        [
            'methods' => 'GET',
            'callback' => 'senderzz_pw_rest_list',
            'permission_callback' => 'tpc_rest_auth',
        ],
        [
            'methods' => 'POST',
            'callback' => 'senderzz_pw_rest_save',
            'permission_callback' => 'tpc_rest_auth',
        ],
    ] );

    register_rest_route( 'tp-carteira/v1', '/webhooks/(?P<id>\d+)', [
        'methods' => 'DELETE',
        'callback' => 'senderzz_pw_rest_delete',
        'permission_callback' => 'tpc_rest_auth',
    ] );

    register_rest_route( 'tp-carteira/v1', '/webhooks/(?P<id>\d+)/test', [
        'methods' => 'POST',
        'callback' => 'senderzz_pw_rest_test',
        'permission_callback' => 'tpc_rest_auth',
    ] );

    register_rest_route( 'tp-carteira/v1', '/webhooks/(?P<id>\d+)/logs', [
        'methods' => 'GET',
        'callback' => 'senderzz_pw_rest_logs',
        'permission_callback' => 'tpc_rest_auth',
    ] );

    register_rest_route( 'tp-carteira/v1', '/webhooks/(?P<id>\d+)/reprocess', [
        'methods' => 'POST',
        'callback' => 'senderzz_pw_rest_reprocess',
        'permission_callback' => 'tpc_rest_auth',
    ] );

    register_rest_route( 'tp-carteira/v1', '/admin/webhooks', [
        'methods' => 'GET',
        'callback' => 'senderzz_pw_rest_admin_list',
        'permission_callback' => fn() => tpc_is_admin_jwt() || current_user_can( 'manage_woocommerce' ),
    ] );
} );


function senderzz_pw_rest_logs( WP_REST_Request $req ): WP_REST_Response {
    $user_id = tpc_rest_auth();
    if ( is_wp_error( $user_id ) ) return new WP_REST_Response( [ 'error' => $user_id->get_error_message() ], 401 );
    global $wpdb;
    $webhook_id = absint( $req['id'] );
    $hook = $wpdb->get_row( $wpdb->prepare( "SELECT id FROM " . senderzz_pw_table() . " WHERE id=%d AND user_id=%d", $webhook_id, $user_id ), ARRAY_A );
    if ( ! $hook ) return new WP_REST_Response( [ 'error' => 'Webhook não encontrado.' ], 404 );
    $logs = $wpdb->get_results( $wpdb->prepare(
        "SELECT id, order_id, event, response_code, fired_at, reprocess_count, last_reprocessed_at FROM " . senderzz_pw_log_table() . " WHERE webhook_id=%d ORDER BY fired_at DESC LIMIT 50",
        $webhook_id
    ), ARRAY_A );
    foreach ( $logs as &$log ) {
        $log['status_label'] = ( (int) $log['response_code'] >= 200 && (int) $log['response_code'] < 300 ) ? 'Entregue' : 'Falha';
        $log['sucesso'] = ( (int) $log['response_code'] >= 200 && (int) $log['response_code'] < 300 );
    }
    return new WP_REST_Response( [ 'data' => $logs ] );
}

function senderzz_pw_rest_reprocess( WP_REST_Request $req ): WP_REST_Response {
    $user_id = tpc_rest_auth();
    if ( is_wp_error( $user_id ) ) return new WP_REST_Response( [ 'error' => $user_id->get_error_message() ], 401 );
    global $wpdb;
    $webhook_id = absint( $req['id'] );
    $log_id     = absint( $req->get_param( 'log_id' ) );
    $hook = $wpdb->get_row( $wpdb->prepare( "SELECT * FROM " . senderzz_pw_table() . " WHERE id=%d AND user_id=%d", $webhook_id, $user_id ), ARRAY_A );
    if ( ! $hook ) return new WP_REST_Response( [ 'error' => 'Webhook não encontrado.' ], 404 );
    $log = $wpdb->get_row( $wpdb->prepare( "SELECT * FROM " . senderzz_pw_log_table() . " WHERE id=%d AND webhook_id=%d", $log_id, $webhook_id ), ARRAY_A );
    if ( ! $log ) return new WP_REST_Response( [ 'error' => 'Log não encontrado.' ], 404 );
    $payload = json_decode( (string) $log['payload_json'], true ) ?: [];
    $hook['_senderzz_update_log_id'] = $log_id;
    $result = senderzz_pw_fire_url( $hook, $payload, (int) $log['order_id'], (string) $log['event'] );
    return new WP_REST_Response( [ 'ok' => $result['ok'], 'code' => $result['code'], 'error' => $result['error'] ] );
}

function senderzz_pw_rest_list( WP_REST_Request $req ): WP_REST_Response {
    $user_id = tpc_rest_auth();
    if ( is_wp_error( $user_id ) ) return new WP_REST_Response( [ 'error' => $user_id->get_error_message() ], 401 );
    if ( function_exists( 'senderzz_pw_ensure_user_webhook_slots' ) ) senderzz_pw_ensure_user_webhook_slots( (int) $user_id );
    global $wpdb;
    $rows = $wpdb->get_results( $wpdb->prepare( "SELECT * FROM " . senderzz_pw_table() . " WHERE user_id = %d ORDER BY shipping_class_id ASC", $user_id ), ARRAY_A );
    $rows = senderzz_pw_enrich_rows( $rows );
    foreach ( $rows as &$r ) { unset( $r['secret'] ); }
    return new WP_REST_Response( [ 'data' => $rows, 'classes' => senderzz_pw_get_classes_for_select(), 'payload_exemplo' => senderzz_pw_payload_example() ] );
}

function senderzz_pw_rest_save( WP_REST_Request $req ): WP_REST_Response {
    $user_id = tpc_rest_auth();
    if ( is_wp_error( $user_id ) ) return new WP_REST_Response( [ 'error' => $user_id->get_error_message() ], 401 );
    global $wpdb;
    $class_id = absint( $req->get_param( 'shipping_class_id' ) );
    $url = esc_url_raw( trim( (string) $req->get_param( 'url' ) ) );
    $active = $req->get_param( 'active' ) === null ? 1 : ( (int) (bool) $req->get_param( 'active' ) );
    if ( ! $url || ! wp_http_validate_url( $url ) ) {
        return new WP_REST_Response( [ 'error' => 'URL inválida.' ], 400 );
    }
    $secret = '';

    $existing = (int) $wpdb->get_var( $wpdb->prepare( "SELECT id FROM " . senderzz_pw_table() . " WHERE user_id=%d AND shipping_class_id=%d", $user_id, $class_id ) );
    $data = [ 'user_id' => $user_id, 'portal_user_id' => $user_id, 'shipping_class_id' => $class_id, 'url' => $url, 'secret' => $secret, 'active' => $active, 'updated_at' => current_time( 'mysql' ) ];
    if ( $existing ) {
        $wpdb->update( senderzz_pw_table(), $data, [ 'id' => $existing ], [ '%d','%d','%d','%s','%s','%d','%s' ], [ '%d' ] );
        $id = $existing;
    } else {
        $data['created_at'] = current_time( 'mysql' );
        $wpdb->insert( senderzz_pw_table(), $data, [ '%d','%d','%d','%s','%s','%d','%s','%s' ] );
        $id = (int) $wpdb->insert_id;
    }
    return new WP_REST_Response( [ 'ok' => true, 'id' => $id ] );
}

function senderzz_pw_rest_delete( WP_REST_Request $req ): WP_REST_Response {
    $user_id = tpc_rest_auth();
    if ( is_wp_error( $user_id ) ) return new WP_REST_Response( [ 'error' => $user_id->get_error_message() ], 401 );
    global $wpdb;
    $wpdb->delete( senderzz_pw_table(), [ 'id' => absint( $req['id'] ), 'user_id' => $user_id ], [ '%d', '%d' ] );
    return new WP_REST_Response( [ 'ok' => true ] );
}

function senderzz_pw_rest_test( WP_REST_Request $req ): WP_REST_Response {
    $user_id = tpc_rest_auth();
    if ( is_wp_error( $user_id ) ) return new WP_REST_Response( [ 'error' => $user_id->get_error_message() ], 401 );
    global $wpdb;
    $hook = $wpdb->get_row( $wpdb->prepare( "SELECT * FROM " . senderzz_pw_table() . " WHERE id=%d AND user_id=%d", absint( $req['id'] ), $user_id ), ARRAY_A );
    if ( ! $hook ) return new WP_REST_Response( [ 'error' => 'Webhook não encontrado.' ], 404 );
    $payload = senderzz_pw_payload_example();
    $result = senderzz_pw_fire_url( $hook, $payload, 0, 'teste_webhook' );
    return new WP_REST_Response( [ 'ok' => $result['ok'], 'code' => $result['code'], 'body' => $result['body'], 'error' => $result['error'] ] );
}

function senderzz_pw_rest_admin_list(): WP_REST_Response {
    return new WP_REST_Response( [ 'data' => senderzz_pw_admin_rows() ] );
}

function senderzz_pw_enrich_rows( array $rows ): array {
    $classes = [];
    foreach ( senderzz_pw_get_classes_for_select() as $c ) $classes[ (int) $c['id'] ] = $c;
    foreach ( $rows as &$r ) {
        $c = $classes[ (int) $r['shipping_class_id'] ] ?? [ 'name' => '#' . (int) $r['shipping_class_id'], 'slug' => '' ];
        $r['classe_nome'] = $c['name'];
        $r['classe_slug'] = $c['slug'];
    }
    return $rows;
}

function senderzz_pw_payload_example(): array {
    $now = current_time( 'c' );
    return [
        'event'        => 'order_status_enviado',
        'status_ativo' => true,
        'pedido'       => [
            'id'                  => 1,
            'numero'              => '1',
            'status'              => 'enviado',
            'subtotal'            => 197.00,
            'subtotal_formatado'  => 'R$ 197,00',
            'total'               => 226.90,
            'total_formatado'     => 'R$ 226,90',
            'desconto'            => 0,
            'desconto_formatado'  => '',
            'metodo_pagamento'    => 'PIX',
            'criado_em'           => $now,
            'atualizado_em'       => $now,
            'pago_em'             => $now,
            'enviado_em'          => '',
            'entregue_em'         => '',
        ],
        'classe_entrega'   => [ 'id' => 10, 'nome' => 'São Paulo', 'slug' => 'sao-paulo' ],
        'frete'            => [ 'valor' => 29.90, 'valor_formatado' => 'R$ 29,90', 'prazo_dias_uteis' => 3, 'transportadora' => 'Loggi', 'servico' => 'Express' ],
        'cliente'          => [ 'nome' => 'Cliente Teste', 'telefone' => '11999999999', 'telefone_completo' => '5511999999999', 'email' => 'cliente@email.com', 'cpf' => '000.000.000-00' ],
        'entrega'          => [ 'nome' => 'Cliente Teste', 'cep' => '01001000', 'endereco' => 'Rua Exemplo', 'numero' => '100', 'complemento' => '', 'bairro' => 'Centro', 'cidade' => 'São Paulo', 'estado' => 'SP' ],
        'rastreamento'     => [ 'BR123456789' ],
        'link_rastreamento'=> senderzz_pw_tracking_url( 'BR123456789' ),
        'itens'            => [ [ 'nome' => 'Produto', 'quantidade' => 1, 'subtotal' => 197.00 ] ],
        'transportadora'   => 'Loggi',
        'servico'          => 'Express',
    ];
}

function senderzz_pw_fire_url( array $hook, array $payload, int $order_id = 0, string $event = 'pedido_atualizado' ): array {
    global $wpdb;
    $secret = (string) ( $hook['secret'] ?? '' );
    $json = wp_json_encode( $payload );
    $headers = [ 'Content-Type' => 'application/json', 'User-Agent' => 'Senderzz/1.0' ];
    // Assinatura HMAC desativada — secret não utilizado
    // SEC-02: usar wrapper SSRF-safe que re-valida DNS no momento da chamada
    $res = function_exists( 'senderzz_safe_outbound_post' )
        ? senderzz_safe_outbound_post( $hook['url'], [ 'timeout' => 15, 'headers' => $headers, 'body' => $json ] )
        : wp_safe_remote_post( $hook['url'], [ 'timeout' => 15, 'headers' => $headers, 'body' => $json ] );
    $ok    = ! is_wp_error( $res ) && wp_remote_retrieve_response_code( $res ) >= 200 && wp_remote_retrieve_response_code( $res ) < 300;
    $code  = is_wp_error( $res ) ? 0 : (int) wp_remote_retrieve_response_code( $res );
    $body  = is_wp_error( $res ) ? '' : substr( (string) wp_remote_retrieve_body( $res ), 0, 1000 );
    $error = is_wp_error( $res ) ? $res->get_error_message() : ( $ok ? '' : $body );

    $log_table = senderzz_pw_log_table();
    $now = current_time( 'mysql' );
    $update_log_id = absint( $hook['_senderzz_update_log_id'] ?? 0 );
    if ( $update_log_id > 0 ) {
        $wpdb->query( $wpdb->prepare(
            "UPDATE {$log_table} SET payload_json=%s, response_code=%d, response_body=%s, reprocess_count=reprocess_count+1, last_reprocessed_at=%s WHERE id=%d AND webhook_id=%d",
            $json, $code, $body ?: $error, $now, $update_log_id, (int) $hook['id']
        ) );
    } else {
        $existing_log_id = 0;
        if ( $order_id > 0 && $event !== 'teste_webhook' ) {
            $existing_log_id = (int) $wpdb->get_var( $wpdb->prepare(
                "SELECT id FROM {$log_table} WHERE webhook_id=%d AND order_id=%d AND event=%s LIMIT 1",
                (int) $hook['id'], $order_id, $event
            ) );
        }
        if ( $existing_log_id > 0 ) {
            $wpdb->update( $log_table, [
                'payload_json' => $json,
                'response_code' => $code,
                'response_body' => $body ?: $error,
            ], [ 'id' => $existing_log_id ], [ '%s','%d','%s' ], [ '%d' ] );
        } else {
            $wpdb->insert( $log_table, [
                'webhook_id' => (int) $hook['id'],
                'user_id' => (int) ( $hook['user_id'] ?? 0 ),
                'shipping_class_id' => (int) ( $hook['shipping_class_id'] ?? 0 ),
                'order_id' => $order_id,
                'event' => $event,
                'payload_json' => $json,
                'response_code' => $code,
                'response_body' => $body ?: $error,
                'fired_at' => $now,
            ] );
        }
    }
    $wpdb->update( senderzz_pw_table(), [ 'last_status' => $ok ? 'ok' : 'erro', 'last_error' => $ok ? '' : $error, 'last_fired_at' => $now ], [ 'id' => (int) $hook['id'] ], [ '%s','%s','%s' ], [ '%d' ] );
    return [ 'ok' => $ok, 'code' => $code, 'body' => $body, 'error' => $error ];
}

add_action( 'woocommerce_order_status_changed', 'senderzz_pw_order_status_changed', 30, 4 );
function senderzz_pw_order_status_changed( $order_id, $old_status, $new_status, $order ): void {
    if ( ! $order instanceof WC_Order ) $order = wc_get_order( $order_id );
    if ( ! $order ) return;
    $old_status = sanitize_key( (string) $old_status );
    $new_status = sanitize_key( (string) $new_status );
    if ( $new_status === '' || $old_status === $new_status ) return;
    if ( ! senderzz_pw_status_should_dispatch( $new_status ) ) return;

    // Para status iniciais (on-hold/pending), adia o dispatch para depois do checkout
    // salvar todos os metas (endereço, shipping class, etc.)
    $initial_statuses = [ 'on-hold', 'pending', 'pending-payment' ];
    if ( in_array( $new_status, $initial_statuses, true ) ) {
        $order->update_meta_data( '_senderzz_pw_pending_event', senderzz_pw_status_event_name( $new_status ) );
        $order->save();
        return;
    }

    senderzz_pw_dispatch_order( $order, senderzz_pw_status_event_name( $new_status ) );
}

// Dispara após o checkout salvar todos os metas — prioridade 25 garante que roda
// depois do FunnelKit (prioridade 10 e 15) e do WC padrão.
add_action( 'woocommerce_checkout_order_processed', 'senderzz_pw_after_checkout_order_processed', 25, 3 );
add_action( 'woocommerce_store_api_checkout_order_processed', 'senderzz_pw_after_checkout_order_processed', 25, 1 );
add_action( 'woocommerce_payment_complete', 'senderzz_pw_after_checkout_order_processed', 25, 1 );
function senderzz_pw_after_checkout_order_processed( $order_id ): void {
    if ( $order_id instanceof WC_Order ) $order_id = $order_id->get_id();
    $order = wc_get_order( (int) $order_id );
    if ( ! $order ) return;

    // Verifica se há evento pendente salvo pelo status_changed
    $event = (string) $order->get_meta( '_senderzz_pw_pending_event' );

    // Fallback: se não há meta mas o status é inicial, constrói o evento agora
    if ( ! $event ) {
        $status = $order->get_status();
        $initial_statuses = [ 'on-hold', 'pending', 'pending-payment' ];
        if ( in_array( $status, $initial_statuses, true ) ) {
            $event = senderzz_pw_status_event_name( $status );
        }
    }

    if ( ! $event ) return;

    // Limpa o meta para evitar duplo disparo
    $order->delete_meta_data( '_senderzz_pw_pending_event' );
    $order->save();

    senderzz_pw_dispatch_order( $order, $event );
}

function senderzz_pw_dispatch_order( WC_Order $order, string $event = 'pedido_atualizado' ): void {
    global $wpdb;
    $class    = senderzz_pw_get_order_class( $order );
    $aff_ctx  = function_exists( 'senderzz_pw_get_order_affiliate_context' ) ? senderzz_pw_get_order_affiliate_context( $order ) : [ 'valid' => false ];
    $owner_id = senderzz_pw_get_owner_user_id_for_order( $order, (int) $class['id'] );

    // Se o pedido veio de link de afiliado, o webhook principal deve ser do produtor da afiliação.
    // Isso impede que um afiliado receba o evento principal por fallback de customer_id/owner errado.
    if ( ! empty( $aff_ctx['valid'] ) && ! empty( $aff_ctx['producer_id'] ) ) {
        $owner_id = (int) $aff_ctx['producer_id'];
    }

    if ( $owner_id <= 0 ) return;

    // Tenta encontrar o webhook pela classe exata do pedido.
    // Fallback 1: webhook configurado para class_id=0 (universal).
    // Fallback 2: qualquer webhook ativo do produtor (primeiro encontrado).
    $hook = $wpdb->get_row( $wpdb->prepare(
        "SELECT * FROM " . senderzz_pw_table() . " WHERE user_id=%d AND shipping_class_id=%d AND active=1 LIMIT 1",
        $owner_id, (int) $class['id']
    ), ARRAY_A );

    if ( ! $hook && (int) $class['id'] !== 0 ) {
        $hook = $wpdb->get_row( $wpdb->prepare(
            "SELECT * FROM " . senderzz_pw_table() . " WHERE user_id=%d AND shipping_class_id=0 AND active=1 LIMIT 1",
            $owner_id
        ), ARRAY_A );
    }

    if ( ! $hook ) {
        $hook = $wpdb->get_row( $wpdb->prepare(
            "SELECT * FROM " . senderzz_pw_table() . " WHERE user_id=%d AND active=1 ORDER BY id ASC LIMIT 1",
            $owner_id
        ), ARRAY_A );
    }

    $order_id = (int) $order->get_id();
    $payload = null;

    if ( $hook ) {
        if ( $order_id > 0 && $event !== 'teste_webhook' ) {
            $already_sent = (int) $wpdb->get_var( $wpdb->prepare(
                "SELECT id FROM " . senderzz_pw_log_table() . " WHERE webhook_id=%d AND order_id=%d AND event=%s LIMIT 1",
                (int) $hook['id'], $order_id, $event
            ) );
            if ( $already_sent <= 0 ) {
                $payload = senderzz_pw_build_payload( $order, $event );
                senderzz_pw_fire_url( $hook, $payload, $order_id, $event );
            }
        } else {
            $payload = senderzz_pw_build_payload( $order, $event );
            senderzz_pw_fire_url( $hook, $payload, $order_id, $event );
        }
    }

    // Webhook do afiliado: somente quando o pedido tem ref explícito daquele afiliado.
    // Não usa fallback amplo. Não envia pedido sem _sz_affiliate_id/_sz_affiliate_ref.
    if ( empty( $aff_ctx['valid'] ) || empty( $aff_ctx['affiliate_user_id'] ) || empty( $aff_ctx['affiliate_id'] ) ) return;

    $aff_user_id = (int) $aff_ctx['affiliate_user_id'];
    $affiliate_id = (int) $aff_ctx['affiliate_id'];

    $aff_hook = $wpdb->get_row( $wpdb->prepare(
        "SELECT * FROM " . senderzz_pw_table() . " WHERE user_id=%d AND shipping_class_id=%d AND active=1 LIMIT 1",
        $aff_user_id, (int) $class['id']
    ), ARRAY_A );

    if ( ! $aff_hook && (int) $class['id'] !== 0 ) {
        $aff_hook = $wpdb->get_row( $wpdb->prepare(
            "SELECT * FROM " . senderzz_pw_table() . " WHERE user_id=%d AND shipping_class_id=0 AND active=1 LIMIT 1",
            $aff_user_id
        ), ARRAY_A );
    }

    if ( ! $aff_hook ) return;

    if ( $order_id > 0 && $event !== 'teste_webhook' ) {
        $already_aff_sent = (int) $wpdb->get_var( $wpdb->prepare(
            "SELECT id FROM " . senderzz_pw_log_table() . " WHERE webhook_id=%d AND order_id=%d AND event=%s LIMIT 1",
            (int) $aff_hook['id'], $order_id, $event
        ) );
        if ( $already_aff_sent > 0 ) return;
    }

    if ( $payload === null ) $payload = senderzz_pw_build_payload( $order, $event );
    $aff_payload = $payload;
    $aff_payload['afiliado'] = [
        'id'      => $affiliate_id,
        'user_id' => $aff_user_id,
    ];
    if ( ! empty( $aff_ctx['producer_id'] ) ) {
        $aff_payload['afiliado']['producer_id'] = (int) $aff_ctx['producer_id'];
    }
    $aff_payload['affiliate_ref'] = $affiliate_id;

    senderzz_pw_fire_url( $aff_hook, $aff_payload, $order_id, $event );
}
// Senderzz v221: submenu legado removido da origem; aba Webhooks vive em Admin > Senderzz.
// add_action( 'admin_menu', function() {
//     add_submenu_page( 'tpc-carteira', 'Webhooks por Classe', 'Webhooks por Classe', 'manage_woocommerce', 'senderzz-webhooks', 'senderzz_pw_admin_page' );
// }, 40 );

function senderzz_pw_admin_rows(): array {
    global $wpdb;
    $rows = $wpdb->get_results( "SELECT w.*, u.display_name, u.user_email FROM " . senderzz_pw_table() . " w LEFT JOIN {$wpdb->users} u ON u.ID = w.user_id ORDER BY w.updated_at DESC, w.created_at DESC", ARRAY_A );
    return senderzz_pw_enrich_rows( $rows ?: [] );
}

function senderzz_pw_admin_page(): void {
    if ( ! current_user_can( 'manage_woocommerce' ) ) return;
    $rows = senderzz_pw_admin_rows();
    echo '<div class="wrap"><h1>Webhooks por Classe</h1><p>Resumo dos webhooks cadastrados pelos produtores. Regra: somente 1 webhook por produtor em cada classe de entrega.</p>';
    echo '<table class="widefat striped"><thead><tr><th>Produtor</th><th>Classe</th><th>URL</th><th>Status</th><th>Último disparo</th><th>Último erro</th></tr></thead><tbody>';
    if ( empty( $rows ) ) {
        echo '<tr><td colspan="6">Nenhum webhook cadastrado.</td></tr>';
    } else {
        foreach ( $rows as $r ) {
            echo '<tr>';
            echo '<td><strong>' . esc_html( $r['display_name'] ?: '#' . $r['user_id'] ) . '</strong><br><small>' . esc_html( $r['user_email'] ?? '' ) . '</small></td>';
            echo '<td>' . esc_html( $r['classe_nome'] ?? '' ) . '</td>';
            echo '<td><code>' . esc_html( $r['url'] ) . '</code></td>';
            echo '<td>' . ( ! empty( $r['active'] ) ? '<span style="color:#15803d;font-weight:700">Ativo</span>' : '<span style="color:#991b1b;font-weight:700">Inativo</span>' ) . '<br><small>' . esc_html( $r['last_status'] ?: '—' ) . '</small></td>';
            echo '<td>' . esc_html( $r['last_fired_at'] ?: '—' ) . '</td>';
            echo '<td>' . esc_html( $r['last_error'] ?: '—' ) . '</td>';
            echo '</tr>';
        }
    }
    echo '</tbody></table></div>';
}

/* ── Senderzz v2.5.13: reserva automática de webhook por classe ─────── */
function senderzz_pw_is_operator_user( int $user_id ): bool {
    $user = get_user_by( 'id', $user_id );
    if ( ! $user ) return false;
    $roles = array_map( 'strval', (array) $user->roles );
    foreach ( $roles as $role ) {
        if ( in_array( $role, [ 'operator', 'operador', 'operador_logistico', 'logistics_operator' ], true ) ) return true;
        if ( str_contains( $role, 'operator' ) || str_contains( $role, 'operador' ) ) return true;
    }
    return false;
}

function senderzz_pw_ensure_user_webhook_slots( int $user_id ): void {
    global $wpdb;
    if ( $user_id <= 0 || senderzz_pw_is_operator_user( $user_id ) ) return;
    $table = senderzz_pw_table();

    // Multi-class: cria slots apenas para as classes que o usuário possui.
    // Busca portal_user_id pelo wp_user_id.
    $portal_user_row = $wpdb->get_row( $wpdb->prepare(
        "SELECT id, shipping_class_id FROM {$wpdb->prefix}senderzz_portal_users WHERE wp_user_id = %d AND status='active' ORDER BY id ASC LIMIT 1",
        $user_id
    ) );
    if ( $portal_user_row && function_exists( 'sz_get_user_class_ids' ) ) {
        $user_class_ids = sz_get_user_class_ids( $portal_user_row );
    } else {
        $user_class_ids = null; // fallback: todas as classes (comportamento original)
    }

    $all_classes = senderzz_pw_get_classes_for_select();
    foreach ( $all_classes as $class ) {
        $class_id = (int) ( $class['id'] ?? 0 );
        // Se o usuário tem classes definidas, pula as que não são dele.
        if ( $user_class_ids !== null && ! in_array( $class_id, $user_class_ids, true ) ) continue;
        $exists = (int) $wpdb->get_var( $wpdb->prepare(
            "SELECT id FROM {$table} WHERE user_id=%d AND shipping_class_id=%d LIMIT 1",
            $user_id,
            $class_id
        ) );
        if ( $exists ) continue;
        $wpdb->insert( $table, [
            'user_id'           => $user_id,
            'portal_user_id'    => $user_id,
            'shipping_class_id' => $class_id,
            'url'               => '',
            'secret'            => '',
            'active'            => 0,
            'created_at'        => current_time( 'mysql' ),
            'updated_at'        => current_time( 'mysql' ),
        ], [ '%d', '%d', '%d', '%s', '%s', '%d', '%s', '%s' ] );
    }
}

add_action( 'user_register', function( $user_id ) {
    if ( function_exists( 'senderzz_pw_ensure_user_webhook_slots' ) ) {
        senderzz_pw_ensure_user_webhook_slots( (int) $user_id );
    }
}, 30 );

add_action( 'profile_update', function( $user_id ) {
    if ( function_exists( 'senderzz_pw_ensure_user_webhook_slots' ) ) {
        senderzz_pw_ensure_user_webhook_slots( (int) $user_id );
    }
}, 30 );

add_action( 'init', function() {
    if ( get_transient( 'senderzz_pw_slots_sync_lock' ) ) return;
    set_transient( 'senderzz_pw_slots_sync_lock', 1, 10 * MINUTE_IN_SECONDS );
    $users = get_users( [ 'fields' => [ 'ID' ], 'number' => 200, 'orderby' => 'ID', 'order' => 'DESC' ] );
    foreach ( $users as $u ) {
        senderzz_pw_ensure_user_webhook_slots( (int) $u->ID );
    }
}, 35 );
// ─── Webhook do produtor: status motoboy (em_rota, embalado, entregue, frustrado, reagendado, a_caminho) ─────
add_action( 'sz_motoboy_status_changed', function( int $pedido_id, string $de, string $para, object $pedido ) {
    // Disparar para TODOS os status relevantes do motoboy, não apenas entregue/frustrado
    $status_disparar = [ 'embalado', 'em_rota', 'a_caminho', 'entregue', 'frustrado', 'reagendado', 'cancelado' ];
    if ( ! in_array( $para, $status_disparar, true ) ) return;

    $wc_order_id = (int) ( $pedido->wc_order_id ?? 0 );
    if ( $wc_order_id <= 0 ) return;

    $order = wc_get_order( $wc_order_id );
    if ( ! $order ) return;

    // Mapa de status motoboy → event name
    $event_map = [
        'embalado'  => 'motoboy_embalado',
        'em_rota'   => 'motoboy_em_rota',
        'a_caminho' => 'motoboy_a_caminho',
        'entregue'  => 'motoboy_entregue',
        'frustrado' => 'motoboy_frustrado',
        'reagendado'=> 'motoboy_reagendado',
        'cancelado' => 'motoboy_cancelado',
    ];
    $event = $event_map[ $para ] ?? ( 'motoboy_' . sanitize_key( $para ) );

    senderzz_pw_dispatch_order( $order, $event );
}, 20, 4 );
