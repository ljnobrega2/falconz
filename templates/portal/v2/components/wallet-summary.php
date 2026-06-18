<?php
/**
 * Componente: resumo de carteira (Fase 1).
 * Estrutura visual de 3 cards (disponível / pendente / em análise).
 * Fase 1: SEM valores reais (regra: nenhuma leitura de dados).
 * Na Fase 6, os valores virão das MESMAS fontes legadas
 * (sz_cod_wallet_summary / tpc_* / REST sz-portal/v1) sem reformatar
 * número no front.
 */
defined( 'ABSPATH' ) || exit;

if ( ! function_exists( 'sz_v2_wallet_summary' ) ) {
    function sz_v2_wallet_summary( array $cards = [] ): string {
        if ( empty( $cards ) ) {
            $cards = [
                [ 'label' => 'Disponível', 'value' => '—' ],
                [ 'label' => 'Pendente', 'value' => '—' ],
                [ 'label' => 'Em análise', 'value' => '—' ],
            ];
        }
        $html = '<div class="szv2-kpi-grid szv2-kpi-grid-3col">';
        foreach ( $cards as $c ) {
            $html .= sz_v2_kpi_card( [
                'label' => (string) ( $c['label'] ?? '' ),
                'value' => (string) ( $c['value'] ?? '—' ),
                'meta'  => (string) ( $c['meta'] ?? '' ),
                'attrs' => (array) ( $c['attrs'] ?? [] ),
            ] );
        }
        return $html . '</div>';
    }
}
