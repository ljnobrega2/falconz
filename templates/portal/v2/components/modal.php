<?php
/**
 * Componente: modal (Fase 1).
 * Abre/fecha via data-szv2-modal-open / data-szv2-modal-close
 * (JS de casca). Os modais funcionais legados (#sz-antecip-modal,
 * saque etc.) NÃO são recriados aqui — entram nas fases das suas
 * seções com IDs originais, herdando apenas esta casca visual.
 */
defined( 'ABSPATH' ) || exit;

if ( ! function_exists( 'sz_v2_modal' ) ) {
    function sz_v2_modal( array $args = [] ): string {
        $key   = (string) ( $args['key'] ?? '' );
        $title = (string) ( $args['title'] ?? '' );
        $body  = (string) ( $args['body_html'] ?? '' );
        $foot  = (string) ( $args['foot_html'] ?? '' );
        $large = ! empty( $args['large'] );

        $html  = '<div class="szv2-modal-overlay" data-szv2-modal="' . esc_attr( $key ) . '" role="dialog" aria-modal="true" aria-label="' . esc_attr( $title ) . '">';
        $html .= '<div class="szv2-modal' . ( $large ? ' szv2-modal-lg' : '' ) . '">';
        $html .= '<div class="szv2-modal-head"><h3>' . esc_html( $title ) . '</h3>';
        $html .= '<button type="button" class="szv2-modal-x" data-szv2-modal-close aria-label="Fechar">×</button></div>';
        $html .= '<div class="szv2-modal-body">' . $body . '</div>';
        if ( '' !== $foot ) {
            $html .= '<div class="szv2-modal-foot">' . $foot . '</div>';
        }
        return $html . '</div></div>';
    }
}
