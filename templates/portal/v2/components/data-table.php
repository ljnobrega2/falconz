<?php
/**
 * Componente: tabela de dados (Fase 1).
 * Markup-only: cabeçalhos + linhas pré-renderizadas (strings já
 * escapadas pelo chamador via componentes). Sem dados reais na
 * Fase 1; as seções usam nas Fases 2–7.
 */
defined( 'ABSPATH' ) || exit;

if ( ! function_exists( 'sz_v2_data_table' ) ) {
    function sz_v2_data_table( array $args = [] ): string {
        $columns = (array) ( $args['columns'] ?? [] ); // ['label' => ..., 'numeric' => bool]
        $rows    = (array) ( $args['rows_html'] ?? [] ); // strings <tr> prontas
        $html    = '<div class="szv2-table-wrap"><table class="szv2-table"><thead><tr>';
        foreach ( $columns as $col ) {
            $label   = is_array( $col ) ? (string) ( $col['label'] ?? '' ) : (string) $col;
            $numeric = is_array( $col ) && ! empty( $col['numeric'] );
            $html   .= '<th' . ( $numeric ? ' class="szv2-td-num"' : '' ) . '>' . esc_html( $label ) . '</th>';
        }
        $html .= '</tr></thead><tbody>';
        $html .= implode( '', $rows );
        $html .= '</tbody></table></div>';
        return $html;
    }
}
