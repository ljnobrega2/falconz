<?php
/**
 * Componente: KPI card (v448 — padronizado).
 * Usado em Dashboard, Carteira e qualquer seção com métricas.
 *
 * Args:
 *   label       string  — label acima do valor
 *   value       string  — valor inicial (pode ser "—" se JS preencher)
 *   meta        string  — texto menor abaixo do valor (opcional)
 *   value_class string  — classes extras na span de valor (ex: "szv2-num")
 *   data_kpi    string  — valor para data-szv2-kpi (ex: "cod-avail")
 *   attrs       array   — atributos extras no card (k => v)
 */
defined( 'ABSPATH' ) || exit;

if ( ! function_exists( 'sz_v2_kpi_card' ) ) {
    function sz_v2_kpi_card( array $args = [] ): string {
        $label       = (string) ( $args['label']       ?? '' );
        $value       = (string) ( $args['value']       ?? '—' );
        $meta        = (string) ( $args['meta']        ?? '' );
        $value_class = trim( (string) ( $args['value_class'] ?? '' ) );
        $data_kpi    = trim( (string) ( $args['data_kpi']    ?? '' ) );

        // Atributos do card
        $attrs = '';
        foreach ( (array) ( $args['attrs'] ?? [] ) as $k => $v ) {
            $attrs .= ' ' . esc_attr( $k ) . '="' . esc_attr( (string) $v ) . '"';
        }

        // Classe da span de valor
        $val_class = 'szv2-kpi-value';
        if ( $value_class !== '' ) {
            $val_class .= ' ' . esc_attr( $value_class );
        }

        // Atributo data-szv2-kpi na span de valor (para binding do JS)
        $kpi_attr = $data_kpi !== '' ? ' data-szv2-kpi="' . esc_attr( $data_kpi ) . '"' : '';

        $html  = '<div class="szv2-card szv2-kpi"' . $attrs . '>';
        $html .= '<span class="szv2-kpi-label">' . esc_html( $label ) . '</span>';
        $html .= '<span class="' . $val_class . '"' . $kpi_attr . '>' . esc_html( $value ) . '</span>';
        if ( '' !== $meta ) {
            $html .= '<span class="szv2-kpi-meta">' . esc_html( $meta ) . '</span>';
        }
        return $html . '</div>';
    }
}
