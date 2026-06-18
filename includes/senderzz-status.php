<?php
/**
 * Senderzz Status Helpers
 *
 * Centraliza normalização/leitura de status sem alterar hooks existentes.
 * Este arquivo é seguro porque só fornece funções de leitura/comparação.
 */
defined( 'ABSPATH' ) || exit;

if ( ! function_exists( 'senderzz_status_normalize' ) ) {
    function senderzz_status_normalize( $status ): string {
        $status = strtolower( trim( (string) $status ) );
        if ( $status === '' ) return '';
        if ( substr( $status, 0, 3 ) === 'wc-' ) $status = substr( $status, 3 );
        $status = str_replace( [ '_', ' ' ], '-', $status );
        $map = [
            'em-rota'      => 'em-rota',
            'emrota'       => 'em-rota',
            'rota'         => 'em-rota',
            'frustrada'    => 'frustrado',
            'frustracao'   => 'frustrado',
            'completo'     => 'completed',
            'concluido'    => 'completed',
            'concluído'    => 'completed',
            'entregue-cod' => 'completed',
            'entrega'      => 'entregue',
            'cancelado'    => 'cancelled',
            'cancelada'    => 'cancelled',
            'reembolsado'  => 'refunded',
            'falhou'       => 'failed',
        ];
        return $map[ $status ] ?? $status;
    }
}

if ( ! function_exists( 'senderzz_status_variants' ) ) {
    function senderzz_status_variants( string $normalized ): array {
        $normalized = senderzz_status_normalize( $normalized );
        $base = array_values( array_unique( array_filter( [ $normalized, 'wc-' . $normalized ] ) ) );
        $extra = [];
        if ( $normalized === 'em-rota' ) $extra = [ 'wc-em_rota', 'em_rota', 'emrota' ];
        if ( $normalized === 'frustrado' ) $extra = [ 'wc-frustrada', 'frustrada', 'wc-frustracao', 'frustracao' ];
        if ( $normalized === 'completed' ) $extra = [ 'wc-completo', 'completo', 'concluido', 'concluído' ];
        if ( $normalized === 'cancelled' ) $extra = [ 'wc-cancelado', 'cancelado' ];
        return array_values( array_unique( array_merge( $base, $extra ) ) );
    }
}

if ( ! function_exists( 'senderzz_status_is' ) ) {
    function senderzz_status_is( $status, array $normalized_targets ): bool {
        $status = senderzz_status_normalize( $status );
        $targets = array_map( 'senderzz_status_normalize', $normalized_targets );
        return in_array( $status, $targets, true );
    }
}

if ( ! function_exists( 'senderzz_status_is_frustrated' ) ) {
    function senderzz_status_is_frustrated( $status ): bool {
        return senderzz_status_is( $status, [ 'frustrado' ] );
    }
}

if ( ! function_exists( 'senderzz_status_is_cancelled' ) ) {
    function senderzz_status_is_cancelled( $status ): bool {
        return senderzz_status_is( $status, [ 'cancelled', 'refunded', 'failed' ] );
    }
}

if ( ! function_exists( 'senderzz_status_is_delivered_cod' ) ) {
    function senderzz_status_is_delivered_cod( $status ): bool {
        return senderzz_status_is( $status, [ 'completed' ] );
    }
}

if ( ! function_exists( 'senderzz_status_is_delivered_expedition' ) ) {
    function senderzz_status_is_delivered_expedition( $status ): bool {
        return senderzz_status_is( $status, [ 'entregue', 'completed' ] );
    }
}

if ( ! function_exists( 'senderzz_status_is_financially_settled' ) ) {
    function senderzz_status_is_financially_settled( $status ): bool {
        // Financeiro real: entregue/concluído. Processando/agendado/em rota = previsão.
        return senderzz_status_is( $status, [ 'completed', 'entregue' ] );
    }
}

if ( ! function_exists( 'senderzz_order_status_raw' ) ) {
    function senderzz_order_status_raw( int $order_id ): string {
        if ( function_exists( 'wc_get_order' ) ) {
            $order = wc_get_order( $order_id );
            if ( $order instanceof WC_Order ) return (string) $order->get_status();
        }
        global $wpdb;
        $orders = $wpdb->prefix . 'wc_orders';
        if ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $orders ) ) === $orders ) {
            $status = $wpdb->get_var( $wpdb->prepare( "SELECT status FROM {$orders} WHERE id=%d LIMIT 1", $order_id ) );
            if ( $status !== null ) return (string) $status;
        }
        return (string) $wpdb->get_var( $wpdb->prepare( "SELECT post_status FROM {$wpdb->posts} WHERE ID=%d LIMIT 1", $order_id ) );
    }
}

if ( ! function_exists( 'senderzz_order_status_normalized' ) ) {
    function senderzz_order_status_normalized( int $order_id ): string {
        return senderzz_status_normalize( senderzz_order_status_raw( $order_id ) );
    }
}
