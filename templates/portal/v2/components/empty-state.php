<?php
/**
 * Componente: empty state (Fase 1).
 * Padrão: falcão discreto + 1 linha de título + 1 linha de apoio +
 * no máximo 1 ação. Usado também como placeholder de seção em
 * migração ("regra de não poluição" do brand book: mascote discreto
 * em telas densas).
 */
defined( 'ABSPATH' ) || exit;

if ( ! function_exists( 'sz_v2_empty_state' ) ) {
    function sz_v2_empty_state( array $args = [] ): string {
        $title  = (string) ( $args['title'] ?? '' );
        $text   = (string) ( $args['text'] ?? '' );
        $phase  = (int) ( $args['phase'] ?? 0 );
        $action = (string) ( $args['action_html'] ?? '' );

        $mark_file = dirname( __DIR__, 4 ) . '/assets/images/senderzz-mascot.svg';
        $mark      = file_exists( $mark_file ) ? (string) file_get_contents( $mark_file ) : '';

        $html  = '<div class="szv2-empty">';
        $html .= '<span class="szv2-empty-mark" aria-hidden="true">' . $mark . '</span>';
        if ( $phase > 0 ) {
            // v448: número de fase não exibido ao usuário final — linguagem neutra
            $html .= '<span class="szv2-phase-pill">Disponível no painel clássico</span>';
        }
        if ( '' !== $title ) {
            $html .= '<h3>' . esc_html( $title ) . '</h3>';
        }
        if ( '' !== $text ) {
            $html .= '<p>' . esc_html( $text ) . '</p>';
        }
        if ( '' !== $action ) {
            $html .= $action;
        }
        return $html . '</div>';
    }
}
