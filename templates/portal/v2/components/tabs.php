<?php
/**
 * Componente: tabs visuais (Fase 1).
 * data-attributes do contrato (data-sz-wallet-tab, data-tpc-tab,
 * data-sz-int-tab...) são repassados por aba via 'attrs' quando a
 * seção for portada — o JS legado continuará encontrando-os.
 */
defined( 'ABSPATH' ) || exit;

if ( ! function_exists( 'sz_v2_tabs' ) ) {
    function sz_v2_tabs( string $group, array $tabs ): string {
        $html  = '<div class="szv2-tabs" role="tablist" data-szv2-tabs="' . esc_attr( $group ) . '">';
        $first = true;
        foreach ( $tabs as $key => $tab ) {
            $label = is_array( $tab ) ? (string) ( $tab['label'] ?? $key ) : (string) $tab;
            $attrs = '';
            if ( is_array( $tab ) ) {
                foreach ( (array) ( $tab['attrs'] ?? [] ) as $k => $v ) {
                    $attrs .= ' ' . esc_attr( $k ) . '="' . esc_attr( (string) $v ) . '"';
                }
            }
            $html .= '<button type="button" class="szv2-tab" role="tab" data-szv2-tab="' . esc_attr( (string) $key ) . '"'
                . ' aria-selected="' . ( $first ? 'true' : 'false' ) . '"' . $attrs . '>'
                . esc_html( $label ) . '</button>';
            $first = false;
        }
        return $html . '</div>';
    }
}

if ( ! function_exists( 'sz_v2_tab_panel' ) ) {
    function sz_v2_tab_panel( string $group, string $key, string $inner_html, bool $open = false ): string {
        return '<div class="szv2-tab-panel" data-szv2-panel-group="' . esc_attr( $group ) . '"'
            . ' data-szv2-panel="' . esc_attr( $key ) . '"' . ( $open ? '' : ' hidden' ) . '>'
            . $inner_html . '</div>';
    }
}
