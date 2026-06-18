<?php
/**
 * Componente: linha de pedido (Fase 1 — definição apenas).
 * Preserva o contrato: <tr class="sz-order-row"> + checkbox com a
 * classe funcional da listagem (.sz-mb-row-chk / .sz-exp-row-chk)
 * passada pelo chamador, + data-order-status. Usado a partir da
 * Fase 3; definido agora para fixar o contrato do markup.
 */
defined( 'ABSPATH' ) || exit;

if ( ! function_exists( 'sz_v2_order_row' ) ) {
    function sz_v2_order_row( array $args = [] ): string {
        $order_id  = (string) ( $args['order_id'] ?? '' );
        $chk_class = (string) ( $args['chk_class'] ?? '' ); // ex.: sz-mb-row-chk
        $status    = (string) ( $args['status'] ?? '' );
        $cells     = (array) ( $args['cells_html'] ?? [] ); // strings <td> prontas

        $html  = '<tr class="sz-order-row" data-order-status="' . esc_attr( $status ) . '" data-value="' . esc_attr( $order_id ) . '">';
        if ( '' !== $chk_class ) {
            $html .= '<td><input type="checkbox" class="' . esc_attr( $chk_class ) . '" value="' . esc_attr( $order_id ) . '"></td>';
        }
        $html .= implode( '', $cells );
        return $html . '</tr>';
    }
}
