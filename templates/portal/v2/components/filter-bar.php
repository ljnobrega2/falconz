<?php
/**
 * Componente: barra de filtros (Fase 1).
 * Estrutura visual apenas. Os IDs funcionais do contrato
 * (#f-q #f-st #f-fr #f-to #f-car, #r-*) serão atribuídos quando
 * cada seção for portada — este componente aceita 'id' por campo
 * exatamente para isso.
 */
defined( 'ABSPATH' ) || exit;

if ( ! function_exists( 'sz_v2_filter_bar' ) ) {
    function sz_v2_filter_bar( array $fields = [] ): string {
        $html = '<div class="szv2-filter-bar">';
        foreach ( $fields as $f ) {
            $type  = (string) ( $f['type'] ?? 'text' );
            $id    = (string) ( $f['id'] ?? '' );
            $ph    = (string) ( $f['placeholder'] ?? '' );
            $idattr = $id ? ' id="' . esc_attr( $id ) . '"' : '';
            if ( 'select' === $type ) {
                $html .= '<select class="szv2-select"' . $idattr . '>';
                foreach ( (array) ( $f['options'] ?? [] ) as $val => $label ) {
                    $html .= '<option value="' . esc_attr( (string) $val ) . '">' . esc_html( (string) $label ) . '</option>';
                }
                $html .= '</select>';
            } elseif ( 'chips' === $type ) {
                $html .= '<div class="szv2-chip-group" role="group">';
                foreach ( (array) ( $f['options'] ?? [] ) as $val => $label ) {
                    $pressed = ! empty( $f['active'] ) && $f['active'] === $val ? 'true' : 'false';
                    $html   .= '<button type="button" class="szv2-chip" data-value="' . esc_attr( (string) $val ) . '" aria-pressed="' . $pressed . '">'
                        . esc_html( (string) $label ) . '</button>';
                }
                $html .= '</div>';
            } else {
                $html .= '<input type="' . esc_attr( $type ) . '" class="szv2-input"' . $idattr
                    . ' placeholder="' . esc_attr( $ph ) . '">';
            }
        }
        return $html . '</div>';
    }
}
