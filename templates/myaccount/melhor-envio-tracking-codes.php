<?php
/**
 * Account Tracking codes.
 *
 */

if ( ! defined( 'ABSPATH' ) ) {
	exit;
}

echo '<div class="melhor-envio-tracking-codes" style="margin-bottom: 20px;">';

echo '<h2>' . __( 'Seu pedido foi enviado', 'wc-melhor-envio' ) . '</h2>';

foreach ( $tracking_codes as $tracking_code ) {
	echo '<div class="melhor-envio-single-tracking-code"><p>' . __( 'Código de rastreio',
			'wc-melhor-envio' ) . ': ' . $tracking_code . '</p>' . wc_melhor_envio_get_formatted_tracking_url( $tracking_code,
			apply_filters( 'wc_melhor_envio_account_tracking_button', 'Acompanhar entrega' ),
			'button btn btn-primary button-primary' ) . '</div>';
}

echo '</div>';
