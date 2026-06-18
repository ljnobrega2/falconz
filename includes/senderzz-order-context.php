<?php
/**
 * Senderzz Order Context
 *
 * Raio-X somente leitura do pedido. Não recalcula comissão, não muda estoque,
 * não cria transação. Serve como base segura para auditoria, suporte e telas.
 */
defined( 'ABSPATH' ) || exit;

if ( ! function_exists( 'senderzz_order_context_meta' ) ) {
    function senderzz_order_context_meta( WC_Order $order, array $keys, $default = '' ) {
        foreach ( $keys as $key ) {
            $value = $order->get_meta( $key, true );
            if ( $value !== '' && $value !== null ) return $value;
        }
        return $default;
    }
}

if ( ! function_exists( 'senderzz_order_context_money' ) ) {
    function senderzz_order_context_money( $value ): float {
        if ( is_string( $value ) ) $value = str_replace( ',', '.', $value );
        return round( (float) $value, 2 );
    }
}

if ( ! function_exists( 'senderzz_order_delivery_mode' ) ) {
    function senderzz_order_delivery_mode( WC_Order $order ): string {
        $mode = strtolower( (string) senderzz_order_context_meta( $order, [ '_senderzz_delivery_mode', '_sz_delivery_mode', '_delivery_mode' ], '' ) );
        $flow = strtolower( (string) senderzz_order_context_meta( $order, [ '_senderzz_motoboy_flow_status' ], '' ) );
        if ( in_array( $mode, [ 'motoboy', 'cod' ], true ) || $flow !== '' ) return 'motoboy';
        if ( in_array( $mode, [ 'expedicao', 'expedição', 'shipping', 'melhor_envio' ], true ) ) return 'expedition';
        foreach ( $order->get_shipping_methods() as $shipping_item ) {
            $method_id = strtolower( (string) $shipping_item->get_method_id() );
            if ( str_contains( $method_id, 'motoboy' ) || str_contains( $method_id, 'cod' ) ) return 'motoboy';
            if ( str_contains( $method_id, 'melhor' ) || str_contains( $method_id, 'correios' ) ) return 'expedition';
        }
        return 'unknown';
    }
}

if ( ! function_exists( 'senderzz_order_context' ) ) {
    function senderzz_order_context( int $order_id ): array {
        $empty = [ 'exists' => false, 'order_id' => $order_id ];
        if ( $order_id <= 0 || ! function_exists( 'wc_get_order' ) ) return $empty;
        $order = wc_get_order( $order_id );
        if ( ! $order instanceof WC_Order ) return $empty;

        $status_raw = function_exists( 'senderzz_order_status_raw' ) ? senderzz_order_status_raw( $order_id ) : (string) $order->get_status();
        $status = function_exists( 'senderzz_status_normalize' ) ? senderzz_status_normalize( $status_raw ) : strtolower( $status_raw );
        $delivery = senderzz_order_delivery_mode( $order );
        $total = senderzz_order_context_money( $order->get_total() );
        // Usar camada canônica quando disponível; fallback manual para compatibilidade
        if ( class_exists( 'Senderzz_Order_Meta' ) ) {
            $offer_value         = (float) Senderzz_Order_Meta::get_order_gross_total( $order ) ?: senderzz_order_context_money( senderzz_order_context_meta( $order, [ '_senderzz_offer_value', '_sz_aff_gross' ], $total ) );
            $aff_commission      = (float) Senderzz_Order_Meta::get_affiliate_commission( $order );
            $producer_commission = senderzz_order_context_money( senderzz_order_context_meta( $order, [ '_sz_prod_commission' ], 0 ) );
            $motoboy_fee         = (float) Senderzz_Order_Meta::get_fee_total( $order );
        } else {
            $offer_value         = senderzz_order_context_money( senderzz_order_context_meta( $order, [ '_senderzz_offer_value', '_sz_aff_gross' ], $total ) );
            $aff_commission      = senderzz_order_context_money( senderzz_order_context_meta( $order, [ '_sz_aff_commission' ], 0 ) );
            $producer_commission = senderzz_order_context_money( senderzz_order_context_meta( $order, [ '_sz_prod_commission' ], 0 ) );
            $motoboy_fee         = senderzz_order_context_money( senderzz_order_context_meta( $order, [ '_sz_mb_taxa_total' ], 0 ) );
        }

        return [
            'exists'                   => true,
            'order_id'                 => $order_id,
            'order_number'             => $order->get_order_number(),
            'status_raw'               => $status_raw,
            'status'                   => $status,
            'flow_status'              => class_exists( 'Senderzz_Order_Meta' )
                ? Senderzz_Order_Meta::get_motoboy_status( $order )
                : strtolower( (string) senderzz_order_context_meta( $order, [ '_senderzz_motoboy_flow_status' ], '' ) ),
            'delivery_mode'            => $delivery,
            'is_motoboy'               => $delivery === 'motoboy',
            'is_expedition'            => $delivery === 'expedition',
            'producer_id'              => class_exists( 'Senderzz_Order_Meta' )
                ? Senderzz_Order_Meta::get_producer_user_id( $order )
                : absint( senderzz_order_context_meta( $order, [ '_sz_aff_producer_id', '_producer_id', '_sz_producer_id', '_senderzz_producer_id', '_senderzz_owner_user_id' ], 0 ) ),
            'affiliate_id'             => class_exists( 'Senderzz_Order_Meta' )
                ? Senderzz_Order_Meta::get_affiliate_id( $order )
                : absint( senderzz_order_context_meta( $order, [ '_sz_affiliate_id', '_sz_affiliate_ref' ], 0 ) ),
            'affiliate_user_id'        => class_exists( 'Senderzz_Order_Meta' )
                ? Senderzz_Order_Meta::get_affiliate_user_id( $order )
                : absint( senderzz_order_context_meta( $order, [ '_sz_affiliate_user_id' ], 0 ) ),
            'shipping_class_id'        => absint( senderzz_order_context_meta( $order, [ '_senderzz_product_shipping_class_id', '_shipping_class_id', '_sz_shipping_class_id' ], 0 ) ),
            'checkout_link_id'         => absint( senderzz_order_context_meta( $order, [ '_senderzz_checkout_link_id' ], 0 ) ),
            'offer_token'              => (string) senderzz_order_context_meta( $order, [ '_senderzz_offer_token' ], '' ),
            'total'                    => $total,
            'offer_value'              => $offer_value,
            'affiliate_commission'     => $aff_commission,
            'producer_commission'      => $producer_commission,
            'motoboy_fee_total'        => $motoboy_fee,
            'split_sum'                => round( $aff_commission + $producer_commission + $motoboy_fee, 2 ),
            'stock_reduced'            => (string) senderzz_order_context_meta( $order, [ '_order_stock_reduced', '_senderzz_manual_stock_reduced' ], '' ),
            'stock_restored_frustrado' => (string) senderzz_order_context_meta( $order, [ '_senderzz_stock_restored_on_frustrado' ], '' ),
            'financially_settled'      => function_exists( 'senderzz_status_is_financially_settled' ) ? senderzz_status_is_financially_settled( $status ) : in_array( $status, [ 'completed', 'entregue' ], true ),
            'frustrated'               => function_exists( 'senderzz_status_is_frustrated' ) ? senderzz_status_is_frustrated( $status ) : $status === 'frustrado',
        ];
    }
}

if ( ! function_exists( 'senderzz_order_is_cod' ) ) {
    function senderzz_order_is_cod( int $order_id ): bool { $c = senderzz_order_context( $order_id ); return ! empty( $c['is_motoboy'] ); }
}
if ( ! function_exists( 'senderzz_order_is_expedition' ) ) {
    function senderzz_order_is_expedition( int $order_id ): bool { $c = senderzz_order_context( $order_id ); return ! empty( $c['is_expedition'] ); }
}

if ( ! function_exists( 'senderzz_render_order_xray_box' ) ) {
    function senderzz_render_order_xray_box( $post_or_order ): void {
        $order = null;
        if ( $post_or_order instanceof WC_Order ) $order = $post_or_order;
        elseif ( is_object( $post_or_order ) && ! empty( $post_or_order->ID ) && function_exists( 'wc_get_order' ) ) $order = wc_get_order( (int) $post_or_order->ID );
        if ( ! $order instanceof WC_Order ) { echo '<p>Pedido não encontrado.</p>'; return; }
        $ctx = senderzz_order_context( (int) $order->get_id() );
        if ( empty( $ctx['exists'] ) ) { echo '<p>Contexto indisponível.</p>'; return; }
        $rows = [
            'Tipo' => $ctx['delivery_mode'] === 'motoboy' ? 'Motoboy / COD' : ( $ctx['delivery_mode'] === 'expedition' ? 'Expedição' : 'Não identificado' ),
            'Status' => ( $ctx['status_raw'] ?? '' ) . ' → ' . ( $ctx['status'] ?? '' ),
            'Produtor' => $ctx['producer_id'] ?: '—',
            'Afiliado' => $ctx['affiliate_id'] ?: '—',
            'Usuário afiliado' => $ctx['affiliate_user_id'] ?: '—',
            'Link checkout' => $ctx['checkout_link_id'] ?: '—',
            'Oferta/token' => $ctx['offer_token'] ?: '—',
            'Valor oferta' => 'R$ ' . number_format( (float) $ctx['offer_value'], 2, ',', '.' ),
            'Comissão afiliado' => 'R$ ' . number_format( (float) $ctx['affiliate_commission'], 2, ',', '.' ),
            'Comissão produtor' => 'R$ ' . number_format( (float) $ctx['producer_commission'], 2, ',', '.' ),
            'Taxas motoboy' => 'R$ ' . number_format( (float) $ctx['motoboy_fee_total'], 2, ',', '.' ),
            'Soma split' => 'R$ ' . number_format( (float) $ctx['split_sum'], 2, ',', '.' ),
            'Financeiro liquidado' => ! empty( $ctx['financially_settled'] ) ? 'Sim' : 'Não',
            'Estoque baixado' => $ctx['stock_reduced'] ?: '—',
            'Estoque restaurado frustrado' => $ctx['stock_restored_frustrado'] ?: '—',
        ];
        echo '<table class="widefat striped" style="margin:0"><tbody>';
        foreach ( $rows as $label => $value ) {
            echo '<tr><th style="width:190px">' . esc_html( $label ) . '</th><td>' . esc_html( (string) $value ) . '</td></tr>';
        }
        echo '</tbody></table>';
        if ( function_exists( 'senderzz_audit_order' ) ) {
            $audit = senderzz_audit_order( (int) $order->get_id() );
            if ( ! empty( $audit['problems'] ) ) {
                echo '<p><strong>Auditoria:</strong> há divergência real nesse pedido.</p><ul>';
                foreach ( $audit['problems'] as $problem ) echo '<li>' . esc_html( (string) ( $problem['tipo'] ?? 'Divergência' ) ) . '</li>';
                echo '</ul>';
            } else {
                echo '<p><strong>Auditoria:</strong> sem divergência financeira real no momento.</p>';
            }
        }
    }
}

add_action( 'add_meta_boxes', function (): void {
    // v349: box Raio-X removido da lateral. As informações úteis foram consolidadas
    // no metabox "Resumo do pedido" para evitar duplicidade/confusão.
    remove_meta_box( 'senderzz_order_xray', 'shop_order', 'side' );
}, 99 );

add_action( 'add_meta_boxes_woocommerce_page_wc-orders', function (): void {
    // v349: box Raio-X removido da lateral HPOS.
    remove_meta_box( 'senderzz_order_xray', 'woocommerce_page_wc-orders', 'side' );
}, 99 );
