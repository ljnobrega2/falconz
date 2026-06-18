<?php
/**
 * Senderzz — Custódia física de estoque Motoboy.
 *
 * Fluxo físico:
 * reservado no CD -> QR bipado pelo motoboy -> com motoboy -> entregue/frustrado
 * frustrado só volta ao estoque depois que o motoboy declara devolução por QR e o OL confirma a condição vendável.
 */
defined( 'ABSPATH' ) || exit;

if ( ! defined( 'SZ_MBC_VERSION' ) ) define( 'SZ_MBC_VERSION', '1.0.1' );

function sz_mbc_now(): string { return function_exists( 'sz_motoboy_now_mysql' ) ? sz_motoboy_now_mysql() : current_time( 'mysql' ); }
function sz_mbc_table( string $name ): string { global $wpdb; return $wpdb->prefix . $name; }

function sz_mbc_install(): void {
    global $wpdb;
    if ( get_option( 'sz_mbc_version' ) === SZ_MBC_VERSION ) return;
    require_once ABSPATH . 'wp-admin/includes/upgrade.php';
    $charset = $wpdb->get_charset_collate();
    $custody = sz_mbc_table( 'sz_motoboy_stock_custody' );
    $moves   = sz_mbc_table( 'sz_motoboy_stock_movements' );

    dbDelta( "CREATE TABLE {$custody} (
        id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        pedido_id BIGINT UNSIGNED NOT NULL,
        wc_order_id BIGINT UNSIGNED NOT NULL,
        package_code VARCHAR(80) NOT NULL,
        product_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
        variation_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
        sku VARCHAR(120) NULL,
        product_name VARCHAR(255) NULL,
        quantity INT UNSIGNED NOT NULL DEFAULT 1,
        producer_id BIGINT UNSIGNED NULL,
        motoboy_id BIGINT UNSIGNED NULL,
        cd_id BIGINT UNSIGNED NULL,
        physical_status VARCHAR(32) NOT NULL DEFAULT 'reserved',
        visible_status VARCHAR(32) NOT NULL DEFAULT 'reservado',
        occurrence_type VARCHAR(32) NULL,
        occurrence_note TEXT NULL,
        occurrence_photos LONGTEXT NULL,
        cost_product DECIMAL(10,2) NOT NULL DEFAULT 0.00,
        frustration_refund_amount DECIMAL(10,2) NOT NULL DEFAULT 0.00,
        producer_credit_amount DECIMAL(10,2) NOT NULL DEFAULT 0.00,
        route_at DATETIME NULL,
        delivered_at DATETIME NULL,
        frustrated_at DATETIME NULL,
        returned_at DATETIME NULL,
        damaged_at DATETIME NULL,
        credited_at DATETIME NULL,
        created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
        updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
        PRIMARY KEY (id),
        UNIQUE KEY uk_pedido_product (pedido_id, product_id, variation_id),
        KEY idx_package (package_code),
        KEY idx_order (wc_order_id),
        KEY idx_motoboy_status (motoboy_id, physical_status),
        KEY idx_visible (visible_status),
        KEY idx_product (product_id)
    ) {$charset};" );

    dbDelta( "CREATE TABLE {$moves} (
        id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        custody_id BIGINT UNSIGNED NULL,
        pedido_id BIGINT UNSIGNED NOT NULL,
        wc_order_id BIGINT UNSIGNED NOT NULL,
        package_code VARCHAR(80) NOT NULL,
        product_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
        quantity INT UNSIGNED NOT NULL DEFAULT 1,
        from_status VARCHAR(32) NULL,
        to_status VARCHAR(32) NOT NULL,
        motoboy_id BIGINT UNSIGNED NULL,
        actor_tipo VARCHAR(32) NULL,
        actor_id BIGINT UNSIGNED NULL,
        note TEXT NULL,
        meta_json LONGTEXT NULL,
        created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (id),
        KEY idx_pedido (pedido_id),
        KEY idx_order (wc_order_id),
        KEY idx_package (package_code),
        KEY idx_motoboy (motoboy_id),
        KEY idx_created (created_at)
    ) {$charset};" );

    update_option( 'sz_mbc_version', SZ_MBC_VERSION, false );
}
add_action( 'init', 'sz_mbc_install', 3 );

function sz_mbc_package_code( int $pedido_id, int $wc_order_id ): string {
    $seed = $pedido_id . '|' . $wc_order_id;
    $sig  = substr( hash_hmac( 'sha256', $seed, wp_salt( 'auth' ) ), 0, 14 );
    return 'SZ-' . $wc_order_id . '-' . $pedido_id . '-' . strtoupper( $sig );
}

function sz_mbc_parse_package_code( string $code ): array {
    $code = strtoupper( trim( sanitize_text_field( $code ) ) );
    if ( preg_match( '/SZ-(\d+)-(\d+)-([A-F0-9]{8,})/', $code, $m ) ) {
        return [ 'wc_order_id' => (int) $m[1], 'pedido_id' => (int) $m[2], 'code' => $code ];
    }
    return [ 'wc_order_id' => 0, 'pedido_id' => 0, 'code' => $code ];
}

function sz_mbc_get_package_code_for_pedido( int $pedido_id ): string {
    global $wpdb;
    $pedido = $wpdb->get_row( $wpdb->prepare( "SELECT id,wc_order_id FROM {$wpdb->prefix}sz_motoboy_pedidos WHERE id=%d LIMIT 1", $pedido_id ) );
    return $pedido ? sz_mbc_package_code( (int) $pedido->id, (int) $pedido->wc_order_id ) : '';
}

function sz_mbc_get_order_producer_id( WC_Order $order ): int {
    foreach ( [ '_sz_aff_producer_id', '_senderzz_producer_id', '_sz_producer_id', '_seller_id' ] as $key ) {
        $v = (int) $order->get_meta( $key, true );
        if ( $v > 0 ) return $v;
    }
    if ( function_exists( 'senderzz_get_order_wallet_owner_id' ) ) {
        $v = (int) senderzz_get_order_wallet_owner_id( $order );
        if ( $v > 0 ) return $v;
    }
    return (int) $order->get_user_id();
}

function sz_mbc_product_cost( int $product_id, int $variation_id = 0 ): float {
    foreach ( array_filter( [ $variation_id, $product_id ] ) as $id ) {
        foreach ( [ 'custo_produto', '_custo_produto', '_senderzz_product_cost', '_sz_product_cost', '_wc_cog_cost' ] as $key ) {
            $raw = get_post_meta( (int) $id, $key, true );
            if ( $raw !== '' && $raw !== null ) {
                $value = (float) str_replace( ',', '.', (string) $raw );
                if ( $value > 0 ) return $value;
            }
        }
    }
    return 0.0;
}

function sz_mbc_order_item_rows( WC_Order $order ): array {
    $rows = [];
    $kit_map = defined( 'SENDERZZ_KIT_STOCK_OPTION' ) ? (array) get_option( SENDERZZ_KIT_STOCK_OPTION, [] ) : [];
    foreach ( $order->get_items( 'line_item' ) as $item ) {
        if ( ! $item instanceof WC_Order_Item_Product ) continue;
        $qty = max( 1, (int) $item->get_quantity() );
        $product = $item->get_product();
        $base_pid = (int) $item->get_product_id();
        $variation_id = (int) $item->get_variation_id();
        if ( $base_pid > 0 && isset( $kit_map[ $base_pid ] ) ) {
            foreach ( (array) ( $kit_map[ $base_pid ]['items'] ?? [] ) as $component ) {
                $cpid = (int) ( $component['product_id'] ?? 0 );
                if ( $cpid <= 0 ) continue;
                $cqty = max( 1, (int) ( $component['qty'] ?? 1 ) );
                $cp = function_exists( 'wc_get_product' ) ? wc_get_product( $cpid ) : null;
                $rows[] = [
                    'product_id' => $cpid, 'variation_id' => 0, 'sku' => $cp ? (string) $cp->get_sku() : '',
                    'name' => $cp ? (string) $cp->get_name() : (string) $item->get_name(),
                    'quantity' => $qty * $cqty, 'unit_cost' => sz_mbc_product_cost( $cpid, 0 ),
                ];
            }
            continue;
        }
        $pid = $base_pid ?: ( $product ? (int) $product->get_id() : 0 );
        $rows[] = [
            'product_id' => $pid, 'variation_id' => $variation_id,
            'sku' => $product ? (string) $product->get_sku() : '',
            'name' => function_exists( 'senderzz_clean_product_label' ) ? senderzz_clean_product_label( $item->get_name() ) : (string) $item->get_name(),
            'quantity' => $qty, 'unit_cost' => sz_mbc_product_cost( $pid, $variation_id ),
        ];
    }
    return $rows;
}

function sz_mbc_status_labels( string $physical ): array {
    $map = [
        'reserved'     => [ 'visible' => 'reservado',  'label' => 'Reservado' ],
        'with_motoboy' => [ 'visible' => 'rota',       'label' => 'Rota' ],
        'delivered'    => [ 'visible' => 'entregue',   'label' => 'Entregue' ],
        'frustrated'   => [ 'visible' => 'frustrado',  'label' => 'Frustrado' ],
        'return_declared' => [ 'visible' => 'frustrado', 'label' => 'Aguardando OL' ],
        'available'    => [ 'visible' => 'disponivel', 'label' => 'Disponível' ],
        'damaged'      => [ 'visible' => 'avariado',   'label' => 'Avariado' ],
        'cancelled'    => [ 'visible' => 'cancelado',  'label' => 'Cancelado' ],
    ];
    return $map[ $physical ] ?? $map['reserved'];
}

function sz_mbc_insert_movement( array $data ): void {
    global $wpdb;
    $wpdb->insert( sz_mbc_table( 'sz_motoboy_stock_movements' ), [
        'custody_id' => isset( $data['custody_id'] ) ? (int) $data['custody_id'] : null,
        'pedido_id' => (int) ( $data['pedido_id'] ?? 0 ),
        'wc_order_id' => (int) ( $data['wc_order_id'] ?? 0 ),
        'package_code' => (string) ( $data['package_code'] ?? '' ),
        'product_id' => (int) ( $data['product_id'] ?? 0 ),
        'quantity' => max( 1, (int) ( $data['quantity'] ?? 1 ) ),
        'from_status' => isset( $data['from_status'] ) ? (string) $data['from_status'] : null,
        'to_status' => (string) ( $data['to_status'] ?? '' ),
        'motoboy_id' => isset( $data['motoboy_id'] ) ? (int) $data['motoboy_id'] : null,
        'actor_tipo' => (string) ( $data['actor_tipo'] ?? '' ),
        'actor_id' => isset( $data['actor_id'] ) ? (int) $data['actor_id'] : null,
        'note' => (string) ( $data['note'] ?? '' ),
        'meta_json' => wp_json_encode( (array) ( $data['meta'] ?? [] ) ),
        'created_at' => sz_mbc_now(),
    ] );
}

function sz_mbc_ensure_for_pedido( int $pedido_id, string $physical = 'reserved' ): bool {
    global $wpdb;
    sz_mbc_install();
    if ( ! function_exists( 'wc_get_order' ) ) return false;
    $pedido = $wpdb->get_row( $wpdb->prepare( "SELECT * FROM {$wpdb->prefix}sz_motoboy_pedidos WHERE id=%d LIMIT 1", $pedido_id ) );
    if ( ! $pedido || empty( $pedido->wc_order_id ) ) return false;
    $order = wc_get_order( (int) $pedido->wc_order_id );
    if ( ! $order instanceof WC_Order ) return false;
    $package_code = sz_mbc_package_code( (int) $pedido->id, (int) $pedido->wc_order_id );
    $producer_id = sz_mbc_get_order_producer_id( $order );
    $label = sz_mbc_status_labels( $physical );
    $items = sz_mbc_order_item_rows( $order );
    if ( empty( $items ) ) $items = [[ 'product_id'=>0,'variation_id'=>0,'sku'=>'','name'=>(string)($pedido->dest_produto ?? 'Produto'),'quantity'=>1,'unit_cost'=>0 ]];

    foreach ( $items as $it ) {
        $existing = $wpdb->get_row( $wpdb->prepare( "SELECT * FROM " . sz_mbc_table( 'sz_motoboy_stock_custody' ) . " WHERE pedido_id=%d AND product_id=%d AND variation_id=%d LIMIT 1", $pedido_id, (int) $it['product_id'], (int) $it['variation_id'] ) );
        $row = [
            'pedido_id' => $pedido_id, 'wc_order_id' => (int) $pedido->wc_order_id, 'package_code' => $package_code,
            'product_id' => (int) $it['product_id'], 'variation_id' => (int) $it['variation_id'], 'sku' => (string) $it['sku'],
            'product_name' => (string) $it['name'], 'quantity' => max( 1, (int) $it['quantity'] ),
            'producer_id' => $producer_id ?: null, 'motoboy_id' => ! empty( $pedido->motoboy_id ) ? (int) $pedido->motoboy_id : null,
            'cd_id' => ! empty( $pedido->cd_id ) ? (int) $pedido->cd_id : null,
            'physical_status' => $physical, 'visible_status' => $label['visible'], 'cost_product' => (float) $it['unit_cost'], 'updated_at' => sz_mbc_now(),
        ];
        if ( $existing ) {
            if ( in_array( (string) $existing->physical_status, [ 'with_motoboy', 'frustrated', 'delivered', 'damaged', 'available', 'cancelled' ], true ) && $physical === 'reserved' ) continue;
            $old = (string) $existing->physical_status;
            $wpdb->update( sz_mbc_table( 'sz_motoboy_stock_custody' ), $row, [ 'id' => (int) $existing->id ] );
            if ( $old !== $physical ) sz_mbc_insert_movement( [ 'custody_id'=>(int)$existing->id,'pedido_id'=>$pedido_id,'wc_order_id'=>(int)$pedido->wc_order_id,'package_code'=>$package_code,'product_id'=>(int)$it['product_id'],'quantity'=>max(1,(int)$it['quantity']),'from_status'=>$old,'to_status'=>$physical,'motoboy_id'=>!empty($pedido->motoboy_id)?(int)$pedido->motoboy_id:null,'note'=>'Transição automática de custódia.' ] );
        } else {
            $row['created_at'] = sz_mbc_now();
            $wpdb->insert( sz_mbc_table( 'sz_motoboy_stock_custody' ), $row );
            sz_mbc_insert_movement( [ 'custody_id'=>(int)$wpdb->insert_id,'pedido_id'=>$pedido_id,'wc_order_id'=>(int)$pedido->wc_order_id,'package_code'=>$package_code,'product_id'=>(int)$it['product_id'],'quantity'=>max(1,(int)$it['quantity']),'from_status'=>null,'to_status'=>$physical,'motoboy_id'=>!empty($pedido->motoboy_id)?(int)$pedido->motoboy_id:null,'note'=>'Custódia criada.' ] );
        }
    }
    $order->update_meta_data( '_senderzz_package_code', $package_code );
    $order->update_meta_data( '_senderzz_stock_custody_status', $label['visible'] );
    $order->save();
    return true;
}

function sz_mbc_set_pedido_status( int $pedido_id, string $physical, array $args = [] ): void {
    global $wpdb;
    sz_mbc_ensure_for_pedido( $pedido_id, $physical );
    $label = sz_mbc_status_labels( $physical );
    $fields = [ 'physical_status'=>$physical, 'visible_status'=>$label['visible'], 'updated_at'=>sz_mbc_now() ];
    if ( isset( $args['motoboy_id'] ) ) $fields['motoboy_id'] = (int) $args['motoboy_id'];
    if ( $physical === 'with_motoboy' ) $fields['route_at'] = sz_mbc_now();
    if ( $physical === 'delivered' ) $fields['delivered_at'] = sz_mbc_now();
    if ( $physical === 'frustrated' ) $fields['frustrated_at'] = sz_mbc_now();
    if ( $physical === 'return_declared' ) $fields['returned_at'] = sz_mbc_now();
    if ( $physical === 'available' ) $fields['returned_at'] = sz_mbc_now();
    if ( $physical === 'damaged' ) $fields['damaged_at'] = sz_mbc_now();
    if ( isset( $args['occurrence_type'] ) ) $fields['occurrence_type'] = sanitize_key( $args['occurrence_type'] );
    if ( isset( $args['occurrence_note'] ) ) $fields['occurrence_note'] = sanitize_textarea_field( (string) $args['occurrence_note'] );
    if ( isset( $args['occurrence_photos'] ) ) $fields['occurrence_photos'] = wp_json_encode( (array) $args['occurrence_photos'] );
    $custody = sz_mbc_table( 'sz_motoboy_stock_custody' );
    $rows = $wpdb->get_results( $wpdb->prepare( "SELECT * FROM {$custody} WHERE pedido_id=%d", $pedido_id ) );
    foreach ( (array) $rows as $row ) {
        $old = (string) $row->physical_status;
        $wpdb->update( $custody, $fields, [ 'id' => (int) $row->id ] );
        if ( $old !== $physical ) sz_mbc_insert_movement( [ 'custody_id'=>(int)$row->id,'pedido_id'=>$pedido_id,'wc_order_id'=>(int)$row->wc_order_id,'package_code'=>(string)$row->package_code,'product_id'=>(int)$row->product_id,'quantity'=>(int)$row->quantity,'from_status'=>$old,'to_status'=>$physical,'motoboy_id'=>isset($fields['motoboy_id'])?(int)$fields['motoboy_id']:(int)$row->motoboy_id,'actor_tipo'=>(string)($args['actor_tipo']??''),'actor_id'=>isset($args['actor_id'])?(int)$args['actor_id']:null,'note'=>(string)($args['note']??''),'meta'=>$args ] );
    }
}

function sz_mbc_validate_transition( int $pedido_id, string $novo_status, array $extra = [], string $actor_tipo = 'sistema', ?int $actor_id = null ) {
    global $wpdb;
    $pedido = $wpdb->get_row( $wpdb->prepare( "SELECT * FROM {$wpdb->prefix}sz_motoboy_pedidos WHERE id=%d LIMIT 1", $pedido_id ) );
    if ( ! $pedido ) return new WP_Error( 'sz_mbc_not_found', 'Pedido Motoboy não encontrado.' );
    $atual = (string) $pedido->status;
    if ( in_array( $novo_status, [ 'agendado', 'reagendado', 'embalado', 'cancelado' ], true ) ) return true;
    if ( $novo_status === 'em_rota' ) {
        $qr_actor_ok = ( $actor_tipo === 'motoboy' && ! empty( $actor_id ) ) || ( $actor_tipo === 'admin_qr_assist' && ! empty( $extra['motoboy_id'] ) );
        if ( ! $qr_actor_ok ) return new WP_Error( 'sz_mbc_route_qr_only', 'Em rota só pode ser iniciado por leitura do QR Code da etiqueta.' );
        if ( empty( $extra['qr_validated'] ) ) return new WP_Error( 'sz_mbc_qr_required', 'Leia o QR Code da etiqueta para colocar o pedido em rota.' );
        if ( $atual !== 'embalado' ) return new WP_Error( 'sz_mbc_route_from_embalado', 'Pedido só pode ir para rota quando estiver embalado.' );
        return true;
    }
    if ( in_array( $novo_status, [ 'entregue', 'frustrado' ], true ) ) {
        if ( ! in_array( $atual, [ 'em_rota', 'a_caminho' ], true ) ) return new WP_Error( 'sz_mbc_finish_from_route', 'Pedido só pode ser baixado quando estiver em rota.' );
        $responsavel = (int) ( $pedido->motoboy_id ?: 0 );
        if ( $actor_tipo === 'motoboy' && $actor_id && $responsavel > 0 && (int) $actor_id !== $responsavel ) return new WP_Error( 'sz_mbc_wrong_motoboy', 'Este pacote está em custódia de outro motoboy.' );
        $with = (int) $wpdb->get_var( $wpdb->prepare( "SELECT COUNT(*) FROM " . sz_mbc_table( 'sz_motoboy_stock_custody' ) . " WHERE pedido_id=%d AND physical_status IN ('with_motoboy','frustrated')", $pedido_id ) );
        if ( $with < 1 ) return new WP_Error( 'sz_mbc_no_custody', 'Não há custódia ativa do pacote com motoboy para baixar este pedido.' );
        return true;
    }
    return true;
}

function sz_mbc_start_route_by_qr( int $motoboy_id, string $qr_code, string $actor_tipo = 'motoboy', ?int $actor_id = null, int $expected_pedido_id = 0 ) {
    $parsed = sz_mbc_parse_package_code( $qr_code );
    if ( empty( $parsed['pedido_id'] ) ) return new WP_Error( 'sz_mbc_invalid_qr', 'QR Code da etiqueta inválido.' );
    $pedido_id = (int) $parsed['pedido_id'];
    if ( $expected_pedido_id > 0 && $pedido_id !== $expected_pedido_id ) return new WP_Error( 'sz_mbc_qr_wrong_order', 'QR Code não corresponde ao pedido aberto.' );
    $expected = sz_mbc_get_package_code_for_pedido( $pedido_id );
    if ( ! $expected || strtoupper( $expected ) !== strtoupper( $parsed['code'] ) ) return new WP_Error( 'sz_mbc_qr_mismatch', 'QR Code não confere com o pacote.' );
    global $wpdb;
    $pedido = $wpdb->get_row( $wpdb->prepare( "SELECT motoboy_id,status FROM {$wpdb->prefix}sz_motoboy_pedidos WHERE id=%d LIMIT 1", $pedido_id ) );
    if ( ! $pedido ) return new WP_Error( 'sz_mbc_not_found', 'Pedido Motoboy não encontrado.' );
    if ( ! empty( $pedido->motoboy_id ) && (int) $pedido->motoboy_id !== $motoboy_id ) {
        return new WP_Error( 'sz_mbc_assigned_to_another', 'Este pacote está atribuído a outro motoboy.' );
    }
    $pendentes = (int) $wpdb->get_var( $wpdb->prepare(
        "SELECT COUNT(*) FROM " . sz_mbc_table( 'sz_motoboy_stock_custody' ) . " WHERE motoboy_id=%d AND pedido_id<>%d AND physical_status IN ('frustrated','return_declared')",
        $motoboy_id,
        $pedido_id
    ) );
    if ( $pendentes > 0 ) {
        return new WP_Error( 'sz_mbc_pending_return', 'Este motoboy possui pacote frustrado aguardando devolução/confirmação do OL. Regularize antes de iniciar nova rota.' );
    }
    sz_mbc_ensure_for_pedido( $pedido_id, 'reserved' );
    $actor_tipo = $actor_tipo === 'admin_qr_assist' ? 'admin_qr_assist' : 'motoboy';
    $actor_id   = $actor_tipo === 'motoboy' ? $motoboy_id : ( $actor_id ?: get_current_user_id() );
    $ok = function_exists( 'sz_motoboy_mudar_status' ) && sz_motoboy_mudar_status( $pedido_id, 'em_rota', [ 'motoboy_id'=>$motoboy_id, 'qr_validated'=>1, 'package_code'=>$expected ], $actor_tipo, $actor_id );
    return $ok ? $pedido_id : new WP_Error( 'sz_mbc_route_failed', 'Não foi possível colocar o pedido em rota.' );
}

function sz_mbc_declare_return_by_qr( int $motoboy_id, string $qr_code, int $expected_pedido_id = 0 ) {
    $parsed = sz_mbc_parse_package_code( $qr_code );
    if ( empty( $parsed['pedido_id'] ) ) return new WP_Error( 'sz_mbc_invalid_qr', 'QR Code da etiqueta inválido.' );
    $pedido_id = (int) $parsed['pedido_id'];
    if ( $expected_pedido_id > 0 && $pedido_id !== $expected_pedido_id ) return new WP_Error( 'sz_mbc_qr_wrong_order', 'QR Code não corresponde ao pedido aberto.' );
    $expected = sz_mbc_get_package_code_for_pedido( $pedido_id );
    if ( ! $expected || strtoupper( $expected ) !== strtoupper( $parsed['code'] ) ) return new WP_Error( 'sz_mbc_qr_mismatch', 'QR Code não confere com o pacote.' );

    global $wpdb;
    $pedido = $wpdb->get_row( $wpdb->prepare( "SELECT * FROM {$wpdb->prefix}sz_motoboy_pedidos WHERE id=%d LIMIT 1", $pedido_id ) );
    if ( ! $pedido ) return new WP_Error( 'sz_mbc_not_found', 'Pedido Motoboy não encontrado.' );
    if ( (int) ( $pedido->motoboy_id ?: 0 ) !== $motoboy_id ) return new WP_Error( 'sz_mbc_wrong_motoboy', 'Este pacote está em custódia de outro motoboy.' );
    if ( (string) $pedido->status !== 'frustrado' ) return new WP_Error( 'sz_mbc_return_only_frustrated', 'Só é possível declarar devolução de pedido frustrado.' );

    $custody = sz_mbc_table( 'sz_motoboy_stock_custody' );
    $rows = $wpdb->get_results( $wpdb->prepare( "SELECT * FROM {$custody} WHERE pedido_id=%d", $pedido_id ) );
    if ( empty( $rows ) ) return new WP_Error( 'sz_mbc_no_custody', 'Pacote não encontrado na custódia.' );
    $current = (string) $rows[0]->physical_status;
    if ( $current === 'return_declared' ) return $pedido_id;
    if ( $current !== 'frustrated' ) return new WP_Error( 'sz_mbc_bad_return_state', 'Este pacote não está frustrado em custódia do motoboy.' );

    sz_mbc_set_pedido_status( $pedido_id, 'return_declared', [
        'motoboy_id' => $motoboy_id,
        'actor_tipo' => 'motoboy',
        'actor_id'   => $motoboy_id,
        'note'       => 'Motoboy bipou o QR e declarou devolução. Aguardando confirmação do OL.',
    ] );

    if ( function_exists( 'wc_get_order' ) && ! empty( $pedido->wc_order_id ) ) {
        $order = wc_get_order( (int) $pedido->wc_order_id );
        if ( $order instanceof WC_Order ) {
            $order->update_meta_data( '_senderzz_package_return_declared', '1' );
            $order->update_meta_data( '_senderzz_package_return_declared_at', sz_mbc_now() );
            $order->add_order_note( 'Senderzz: motoboy declarou devolução do pacote por QR. Estoque aguardando conferência do OL.' );
            $order->save();
        }
    }
    return $pedido_id;
}

function sz_mbc_restore_stock_on_return_once( int $order_id ): bool {
    if ( function_exists( 'senderzz_sz_restore_stock_on_frustrado_once' ) ) return (bool) senderzz_sz_restore_stock_on_frustrado_once( $order_id, 'return_scan' );
    if ( function_exists( 'wc_maybe_increase_stock_levels' ) ) { wc_maybe_increase_stock_levels( $order_id ); return true; }
    return false;
}

function sz_mbc_apply_avariado_finance( int $pedido_id, string $occurrence_type, string $note = '', array $photos = [] ): void {
    global $wpdb;
    if ( ! function_exists( 'wc_get_order' ) ) return;
    $custody = sz_mbc_table( 'sz_motoboy_stock_custody' );
    $rows = $wpdb->get_results( $wpdb->prepare( "SELECT * FROM {$custody} WHERE pedido_id=%d", $pedido_id ) );
    if ( empty( $rows ) ) return;
    $order_id = (int) $rows[0]->wc_order_id;
    $order = wc_get_order( $order_id );
    if ( ! $order instanceof WC_Order || $order->get_meta( '_senderzz_avariado_finance_done', true ) === '1' ) return;
    $producer_id = sz_mbc_get_order_producer_id( $order );
    $product_cost_total = 0.0;
    foreach ( $rows as $row ) $product_cost_total += max( 0, (float) $row->cost_product ) * max( 1, (int) $row->quantity );

    $refund_total = 0.0;
    $producer_penalty = (float) $order->get_meta( '_sz_prod_frustration_penalty', true );
    if ( $producer_penalty > 0 && $producer_id > 0 && function_exists( 'tpc_creditar' ) ) {
        $tx = tpc_creditar( $producer_id, $producer_penalty, 'Estorno taxa de frustração por avaria #' . $order_id, [ 'referencia'=>'sz_avariado_refund_frustracao_prod_' . $order_id, 'order_id'=>$order_id, 'tipo'=>'avariado_frustracao_refund' ] );
        if ( $tx ) $refund_total += $producer_penalty;
    }

    $affiliate_id = (int) $order->get_meta( '_sz_affiliate_id', true );
    $affiliate_penalty = (float) $order->get_meta( '_sz_aff_frustration_penalty', true );
    if ( $affiliate_id > 0 && $affiliate_penalty > 0 && function_exists( 'sz_aff_table' ) ) {
        $wallet_table = sz_aff_table( 'sz_affiliate_wallet' );
        $tx_table = sz_aff_table( 'sz_affiliate_transactions' );
        if ( function_exists( 'sz_aff_ensure_wallet' ) ) sz_aff_ensure_wallet( $affiliate_id );
        $existing = (int) $wpdb->get_var( $wpdb->prepare( "SELECT id FROM {$tx_table} WHERE affiliate_id=%d AND order_id=%d AND type='refund_frustration' LIMIT 1", $affiliate_id, $order_id ) );
        if ( ! $existing ) {
            $now = function_exists( 'sz_aff_now' ) ? sz_aff_now() : current_time( 'mysql' );
            $wallet = $wpdb->get_row( $wpdb->prepare( "SELECT balance,debt_amount FROM {$wallet_table} WHERE affiliate_id=%d LIMIT 1", $affiliate_id ), ARRAY_A );
            $current_debt = max( 0, (float) ( $wallet['debt_amount'] ?? 0 ) );
            $debt_reduce = min( $current_debt, $affiliate_penalty );
            $balance_credit = max( 0, $affiliate_penalty - $debt_reduce );
            $wpdb->query( $wpdb->prepare( "UPDATE {$wallet_table} SET balance=balance+%f, debt_amount=GREATEST(0,debt_amount-%f), updated_at=%s WHERE affiliate_id=%d", $balance_credit, $debt_reduce, $now, $affiliate_id ) );
            $wpdb->insert( $tx_table, [ 'affiliate_id'=>$affiliate_id,'producer_id'=>$producer_id,'order_id'=>$order_id,'type'=>'refund_frustration','amount'=>$affiliate_penalty,'status'=>'available','available_at'=>$now,'created_at'=>$now,'meta_json'=>wp_json_encode([ 'referencia'=>'sz_avariado_refund_frustracao_aff_' . $order_id, 'balance_credit'=>$balance_credit, 'debt_reduce'=>$debt_reduce ]) ], [ '%d','%d','%d','%s','%f','%s','%s','%s','%s' ] );
            $refund_total += $affiliate_penalty;
        }
    }

    $producer_credit = 0.0;
    if ( $producer_id > 0 && $product_cost_total > 0 && function_exists( 'tpc_creditar' ) ) {
        $tx = tpc_creditar( $producer_id, $product_cost_total, 'Crédito por perda operacional #' . $order_id, [ 'referencia'=>'sz_avariado_custo_produto_' . $order_id, 'order_id'=>$order_id, 'tipo'=>'avariado_custo_produto', 'occurrence_type'=>$occurrence_type ] );
        if ( $tx ) $producer_credit = $product_cost_total;
    }

    $wpdb->update( $custody, [ 'frustration_refund_amount'=>$refund_total, 'producer_credit_amount'=>$producer_credit, 'credited_at'=>sz_mbc_now() ], [ 'pedido_id'=>$pedido_id ] );
    $order->update_meta_data( '_senderzz_avariado_finance_done', '1' );
    $order->update_meta_data( '_senderzz_avariado_occurrence_type', $occurrence_type );
    $order->update_meta_data( '_senderzz_avariado_note', $note );
    $order->update_meta_data( '_senderzz_avariado_photos', wp_json_encode( $photos ) );
    $order->update_meta_data( '_senderzz_avariado_product_cost_credit', $producer_credit );
    $order->update_meta_data( '_senderzz_avariado_frustration_refund', $refund_total );
    $order->add_order_note( 'Senderzz: Avariado/perda operacional registrada. Taxa de frustração estornada para quem pagou: R$ ' . number_format( $refund_total, 2, ',', '.' ) . '. Crédito de custo do produto ao produtor: R$ ' . number_format( $producer_credit, 2, ',', '.' ) . '. Ocorrência: ' . $occurrence_type . '. ' . $note );
    $order->save();
}

function sz_mbc_return_by_qr( string $qr_code, string $condition, string $note = '', array $photos = [], string $actor_tipo = 'admin', ?int $actor_id = null ) {
    global $wpdb;
    $parsed = sz_mbc_parse_package_code( $qr_code );
    if ( empty( $parsed['pedido_id'] ) ) return new WP_Error( 'sz_mbc_invalid_qr', 'QR Code da etiqueta inválido.' );
    $pedido_id = (int) $parsed['pedido_id'];
    $rows = $wpdb->get_results( $wpdb->prepare( "SELECT * FROM " . sz_mbc_table( 'sz_motoboy_stock_custody' ) . " WHERE pedido_id=%d", $pedido_id ) );
    if ( empty( $rows ) ) return new WP_Error( 'sz_mbc_no_custody', 'Pacote não encontrado na custódia.' );
    $status = (string) $rows[0]->physical_status;
    if ( $status === 'frustrated' ) return new WP_Error( 'sz_mbc_return_not_declared', 'O motoboy precisa bipar a devolução antes do OL confirmar a condição.' );
    if ( $status !== 'return_declared' ) return new WP_Error( 'sz_mbc_bad_return_state', 'Este pacote não está aguardando confirmação de devolução pelo OL.' );
    $condition = sanitize_key( $condition );
    if ( in_array( $condition, [ 'vendavel', 'ok' ], true ) ) {
        sz_mbc_set_pedido_status( $pedido_id, 'available', [ 'actor_tipo'=>$actor_tipo, 'actor_id'=>$actor_id, 'note'=>'Produto retornado vendável ao CD.' ] );
        sz_mbc_restore_stock_on_return_once( (int) $rows[0]->wc_order_id );
        return $pedido_id;
    }
    if ( in_array( $condition, [ 'avariado', 'extravio', 'perda', 'divergente', 'violado' ], true ) ) {
        if ( trim( $note ) === '' ) return new WP_Error( 'sz_mbc_note_required', 'Informe o relato da ocorrência.' );
        if ( empty( $photos ) ) return new WP_Error( 'sz_mbc_photo_required', 'Anexe ao menos uma foto/evidência da ocorrência.' );
        sz_mbc_set_pedido_status( $pedido_id, 'damaged', [ 'actor_tipo'=>$actor_tipo, 'actor_id'=>$actor_id, 'occurrence_type'=>$condition, 'occurrence_note'=>$note, 'occurrence_photos'=>$photos, 'note'=>'Avariado/perda operacional.' ] );
        sz_mbc_apply_avariado_finance( $pedido_id, $condition, $note, $photos );
        $order = function_exists( 'wc_get_order' ) ? wc_get_order( (int) $rows[0]->wc_order_id ) : null;
        if ( $order instanceof WC_Order ) {
            $order->update_meta_data( '_senderzz_motoboy_flow_status', 'avariado' );
            if ( function_exists( 'senderzz_set_order_status_from_motoboy_status' ) ) senderzz_set_order_status_from_motoboy_status( $order, 'avariado', 'Senderzz: produto avariado/perda operacional registrado pelo OL.' );
            elseif ( ! $order->has_status( 'avariado' ) ) $order->update_status( 'avariado', 'Senderzz: produto avariado/perda operacional registrado pelo OL.' );
            else $order->save();
        }
        return $pedido_id;
    }
    return new WP_Error( 'sz_mbc_bad_condition', 'Condição de retorno inválida.' );
}

add_action( 'sz_motoboy_status_changed', function( int $pedido_id, string $old, string $new, $pedido ): void {
    $map = [ 'agendado'=>'reserved','reagendado'=>'reserved','embalado'=>'reserved','em_rota'=>'with_motoboy','a_caminho'=>'with_motoboy','entregue'=>'delivered','frustrado'=>'frustrated','cancelado'=>'cancelled' ];
    if ( isset( $map[ $new ] ) ) sz_mbc_set_pedido_status( $pedido_id, $map[ $new ], [ 'motoboy_id'=>isset($pedido->motoboy_id)?(int)$pedido->motoboy_id:null, 'actor_tipo'=>'status_hook' ] );
}, 20, 4 );

add_action( 'admin_init', function(): void {
    if ( ! current_user_can( 'manage_woocommerce' ) ) return;
    if ( (int) get_transient( 'sz_mbc_backfill_recent' ) > time() - 600 ) return;
    set_transient( 'sz_mbc_backfill_recent', time(), 600 );
    global $wpdb;
    $table = $wpdb->prefix . 'sz_motoboy_pedidos';
    if ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $table ) ) !== $table ) return;
    $rows = $wpdb->get_results( "SELECT id,status FROM {$table} WHERE status IN ('agendado','reagendado','embalado','em_rota','a_caminho','frustrado','entregue') ORDER BY updated_at DESC LIMIT 300" );
    $map = [ 'agendado'=>'reserved','reagendado'=>'reserved','embalado'=>'reserved','em_rota'=>'with_motoboy','a_caminho'=>'with_motoboy','frustrado'=>'frustrated','entregue'=>'delivered' ];
    foreach ( (array) $rows as $r ) sz_mbc_ensure_for_pedido( (int) $r->id, $map[ (string) $r->status ] ?? 'reserved' );
}, 60 );
