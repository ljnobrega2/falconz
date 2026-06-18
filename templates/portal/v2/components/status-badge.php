<?php
/**
 * Componente: badge de status (Fase 1).
 * Mapa FIXO slug wc-* → cor semântica + rótulo PT. Apenas
 * apresentação: nenhum slug novo, nenhum rename. Classe .sz-badge
 * preservada (contrato), variação visual em szv2-badge-*.
 */
defined( 'ABSPATH' ) || exit;

if ( ! function_exists( 'sz_v2_status_badge_map' ) ) {
    function sz_v2_status_badge_map(): array {
        return [
            // slug (sem wc-)        [variante, rótulo]
            'aprovado'           => [ 'info',    'Aprovado' ],
            'agendado'           => [ 'info',    'Agendado' ],
            'processing'         => [ 'info',    'Processando' ],
            'separado'           => [ 'warning', 'Separado' ],
            'embalado'           => [ 'warning', 'Embalado' ],
            'coletado'           => [ 'warning', 'Coletado' ],
            'emretirada'         => [ 'warning', 'Em retirada' ],
            'acaminho'           => [ 'brand',   'A caminho' ],
            'a_caminho'          => [ 'brand',   'A caminho' ],
            'em_rota'            => [ 'brand',   'Em rota' ],
            'em-rota'            => [ 'brand',   'Em rota' ],
            'emrota'             => [ 'brand',   'Em rota' ],
            'enviado'            => [ 'brand',   'Enviado' ],
            'entregue'           => [ 'success', 'Entregue' ],
            'completed'          => [ 'success', 'Concluído' ],
            'completo'           => [ 'success', 'Concluído' ],
            'frustrado'          => [ 'danger',  'Frustrado' ],
            'failed'             => [ 'danger',  'Falhou' ],
            'saldoinsuficiente'  => [ 'danger',  'Saldo insuf.' ],
            'cancelled'          => [ 'neutral', 'Cancelado' ],
            'cancelado'          => [ 'neutral', 'Cancelado' ],
            'emcancelamento'     => [ 'neutral', 'Em cancelamento' ],
            'devolvido'          => [ 'neutral', 'Devolvido' ],
            'avariado'           => [ 'neutral', 'Avariado' ],
            'asuspender'         => [ 'neutral', 'A suspender' ],
            'refunded'           => [ 'neutral', 'Reembolsado' ],
            'on-hold'            => [ 'warning', 'Em espera' ],
            'pending'            => [ 'neutral', 'Pendente' ],
            'pendente'           => [ 'warning', 'Pendente' ],
        ];
    }
}

if ( ! function_exists( 'sz_v2_status_badge' ) ) {
    function sz_v2_status_badge( string $status ): string {
        $slug = strtolower( trim( $status ) );
        if ( 0 === strpos( $slug, 'wc-' ) ) {
            $slug = substr( $slug, 3 );
        }
        if ( function_exists( 'senderzz_status_normalize' ) ) {
            $slug = senderzz_status_normalize( $slug ); // autoridade legada
        }
        $map  = sz_v2_status_badge_map();
        $item = $map[ $slug ] ?? [ 'neutral', ucfirst( str_replace( '-', ' ', $slug ) ) ];
        return '<span class="sz-badge szv2-badge-' . esc_attr( $item[0] ) . '" data-status="' . esc_attr( $slug ) . '">'
            . esc_html( $item[1] ) . '</span>';
    }
}
