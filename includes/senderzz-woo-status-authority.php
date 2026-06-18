<?php
/**
 * Senderzz — WooCommerce como fonte única de status do pedido.
 *
 * Regra: qualquer status operacional do Senderzz/Motoboy deve existir como
 * status WooCommerce e a tabela/meta Senderzz ficam apenas como ESPELHO.
 */
defined( 'ABSPATH' ) || exit;

if ( ! function_exists( 'senderzz_woo_status_authority_statuses' ) ) {
    function senderzz_woo_status_authority_statuses(): array {
        return [
            'wc-agendado'   => 'Agendado',
            'wc-reagendado' => 'Reagendado',
            'wc-embalado'   => 'Embalado',
            'wc-em-rota'    => 'Em rota',
            'wc-acaminho'   => 'A caminho',
            'wc-frustrado'  => 'Frustrado',
            'wc-avariado'   => 'Avariado',
            'wc-completo'   => 'Completo',
            'wc-entregue'   => 'Entregue',
        ];
    }
}

add_action( 'init', function(): void {
    if ( ! function_exists( 'wc_register_order_status' ) ) return;
    foreach ( senderzz_woo_status_authority_statuses() as $slug => $label ) {
        wc_register_order_status( $slug, [
            'label'                     => $label,
            'public'                    => false,
            'exclude_from_search'       => false,
            'show_in_admin_all_list'    => true,
            'show_in_admin_status_list' => true,
            'label_count'               => _n_noop( $label . ' <span class="count">(%s)</span>', $label . ' <span class="count">(%s)</span>' ),
        ] );
    }
}, 4 );

add_filter( 'wc_order_statuses', function( array $statuses ): array {
    $custom = senderzz_woo_status_authority_statuses();
    $out = [];
    foreach ( $statuses as $key => $label ) {
        $out[ $key ] = $label;
        if ( $key === 'wc-on-hold' ) {
            foreach ( [ 'wc-agendado', 'wc-reagendado', 'wc-embalado', 'wc-em-rota', 'wc-acaminho', 'wc-frustrado', 'wc-avariado' ] as $slug ) {
                if ( isset( $custom[ $slug ] ) && ! isset( $out[ $slug ] ) ) $out[ $slug ] = $custom[ $slug ];
            }
        }
        if ( $key === 'wc-processing' ) {
            foreach ( [ 'wc-completo', 'wc-entregue' ] as $slug ) {
                if ( isset( $custom[ $slug ] ) && ! isset( $out[ $slug ] ) ) $out[ $slug ] = $custom[ $slug ];
            }
        }
    }
    foreach ( $custom as $slug => $label ) {
        if ( ! isset( $out[ $slug ] ) ) $out[ $slug ] = $label;
    }
    return $out;
}, 9 );

if ( ! function_exists( 'senderzz_status_clean_slug' ) ) {
    function senderzz_status_clean_slug( $status ): string {
        $status = strtolower( trim( (string) $status ) );
        if ( strpos( $status, 'wc-' ) === 0 ) $status = substr( $status, 3 );
        return str_replace( [ '_', ' ' ], '-', sanitize_title( $status ) );
    }
}

if ( ! function_exists( 'senderzz_motoboy_status_to_woo_status' ) ) {
    function senderzz_motoboy_status_to_woo_status( $status ): string {
        $s = senderzz_status_clean_slug( $status );
        $map = [
            'aprovado'    => 'agendado',
            'agendado'    => 'agendado',
            'reagendado'  => 'reagendado',
            'embalado'    => 'embalado',
            'em-rota'     => 'em-rota',
            'emrota'      => 'em-rota',
            'a-caminho'   => 'acaminho',
            'acaminho'    => 'acaminho',
            'entregue'    => 'completo', // COD Motoboy entregue = Woo "Completo".
            'completo'    => 'completo',
            'completed'   => 'completo',
            'frustrado'   => 'frustrado',
            'frustracao'  => 'frustrado',
            'avariado'    => 'avariado',
            'cancelado'   => 'cancelled',
            'cancelled'   => 'cancelled',
        ];
        return $map[ $s ] ?? $s;
    }
}

if ( ! function_exists( 'senderzz_woo_status_to_motoboy_status' ) ) {
    function senderzz_woo_status_to_motoboy_status( $status ): string {
        $s = senderzz_status_clean_slug( $status );
        $map = [
            'processing'  => 'agendado',
            'on-hold'     => 'agendado',
            'pending'     => 'agendado',
            'agendado'    => 'agendado',
            'reagendado'  => 'reagendado',
            'embalado'    => 'embalado',
            'em-rota'     => 'em_rota',
            'emrota'      => 'em_rota',
            'acaminho'    => 'a_caminho',
            'a-caminho'   => 'a_caminho',
            'completo'    => 'entregue',
            'completed'   => 'entregue',
            'frustrado'   => 'frustrado',
            'frustracao'  => 'frustrado',
            'avariado'    => 'avariado',
            'cancelled'   => 'cancelado',
            'cancelado'   => 'cancelado',
            'failed'      => 'cancelado',
        ];
        return $map[ $s ] ?? $s;
    }
}

if ( ! function_exists( 'senderzz_order_is_motoboy_for_status_sync' ) ) {
    function senderzz_order_is_motoboy_for_status_sync( WC_Order $order ): bool {
        if ( function_exists( 'sz_motoboy_order_is_cod_motoboy' ) && sz_motoboy_order_is_cod_motoboy( $order ) ) return true;
        $mode = strtolower( (string) $order->get_meta( '_senderzz_delivery_mode', true ) );
        if ( $mode === 'motoboy' ) return true;
        if ( (string) $order->get_meta( '_senderzz_motoboy_flow_status', true ) !== '' ) return true;
        global $wpdb;
        $table = $wpdb->prefix . 'sz_motoboy_pedidos';
        if ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $table ) ) === $table ) {
            return (bool) $wpdb->get_var( $wpdb->prepare( "SELECT id FROM {$table} WHERE wc_order_id=%d LIMIT 1", $order->get_id() ) );
        }
        return false;
    }
}

if ( ! function_exists( 'senderzz_sync_motoboy_mirrors_from_woo_order' ) ) {
    function senderzz_sync_motoboy_mirrors_from_woo_order( int $order_id, ?WC_Order $order = null ): void {
        static $syncing = false;
        if ( $syncing || ! function_exists( 'wc_get_order' ) ) return;
        $order = $order instanceof WC_Order ? $order : wc_get_order( $order_id );
        if ( ! $order instanceof WC_Order ) return;
        if ( ! senderzz_order_is_motoboy_for_status_sync( $order ) ) return;

        $syncing = true;
        $wc_status = $order->get_status();
        $mb_status = senderzz_woo_status_to_motoboy_status( $wc_status );
        if ( $mb_status !== '' ) {
            $now = current_time( 'mysql' );
            $order->update_meta_data( '_senderzz_delivery_mode', 'motoboy' );
            $order->update_meta_data( '_senderzz_motoboy_flow_status', $mb_status );
            $order->update_meta_data( '_senderzz_motoboy_status', $mb_status );
            $order->update_meta_data( '_senderzz_motoboy_status_updated_at', $now );
            $order->save();

            global $wpdb;
            $table = $wpdb->prefix . 'sz_motoboy_pedidos';
            if ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $table ) ) === $table ) {
                $data = [ 'status' => $mb_status, 'updated_at' => $now ];
                if ( $mb_status === 'embalado' )   $data['ts_embalado']   = $now;
                if ( $mb_status === 'em_rota' )    $data['ts_em_rota']    = $now;
                if ( $mb_status === 'a_caminho' )  $data['ts_a_caminho']  = $now;
                if ( $mb_status === 'entregue' )   $data['ts_entregue']   = $now;
                if ( $mb_status === 'frustrado' )  $data['ts_frustrado']  = $now;
                $wpdb->update( $table, $data, [ 'wc_order_id' => $order_id ] );
            }
        }
        $syncing = false;
    }
}

if ( ! function_exists( 'senderzz_set_order_status_from_motoboy_status' ) ) {
    function senderzz_set_order_status_from_motoboy_status( WC_Order $order, string $motoboy_status, string $note = '' ): void {
        static $syncing = false;
        if ( $syncing ) return;
        $syncing = true;
        $woo = senderzz_motoboy_status_to_woo_status( $motoboy_status );
        if ( $woo !== '' && ! $order->has_status( $woo ) ) {
            $order->update_status( $woo, $note ?: 'Senderzz: status sincronizado com o fluxo Motoboy.' );
        } else {
            $order->save();
        }
        $syncing = false;
    }
}

add_action( 'woocommerce_order_status_changed', function( int $order_id, string $old_status, string $new_status ): void {
    senderzz_sync_motoboy_mirrors_from_woo_order( $order_id );
}, 30, 3 );

add_action( 'sz_motoboy_status_changed', function( int $pedido_id, string $old, string $new, $pedido ): void {
    if ( empty( $pedido->wc_order_id ) || ! function_exists( 'wc_get_order' ) ) return;
    $order = wc_get_order( (int) $pedido->wc_order_id );
    if ( ! $order instanceof WC_Order ) return;
    senderzz_set_order_status_from_motoboy_status( $order, $new, 'Senderzz COD Motoboy: status operacional sincronizado com o WooCommerce.' );
}, 5, 4 );

// Saneamento leve: corrige pedidos recentes antigos em que a tabela/meta divergiu do Woo.
add_action( 'admin_init', function(): void {
    if ( ! current_user_can( 'manage_woocommerce' ) ) return;
    $last = (int) get_transient( 'senderzz_woo_status_authority_recent_sync' );
    if ( $last && $last > time() - 300 ) return;
    set_transient( 'senderzz_woo_status_authority_recent_sync', time(), 300 );
    if ( ! function_exists( 'wc_get_order' ) ) return;
    global $wpdb;
    $table = $wpdb->prefix . 'sz_motoboy_pedidos';
    if ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $table ) ) !== $table ) return;
    $ids = $wpdb->get_col( "SELECT wc_order_id FROM {$table} WHERE wc_order_id > 0 ORDER BY COALESCE(updated_at, created_at) DESC LIMIT 150" );
    foreach ( (array) $ids as $oid ) {
        senderzz_sync_motoboy_mirrors_from_woo_order( (int) $oid );
    }
}, 50 );
