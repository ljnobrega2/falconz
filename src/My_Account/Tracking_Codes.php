<?php

namespace WC_MelhorEnvio\My_Account;

use WC_Melhor_Envio;

if ( ! defined( 'ABSPATH' ) ) {
	exit;
}

class Tracking_Codes {
	function __construct() {
		add_action( 'woocommerce_order_details_after_order_table', array( $this, 'show_on_order_details' ), 1 );

		add_filter( 'woocommerce_my_account_my_orders_actions', array( $this, 'action_order_tracking' ), 500, 2 );
	}

	/**
	 * Output tracking code
	 *
	 */
	public function show_on_order_details( $order ) {
		$tracking_codes = wc_melhor_envio_get_tracking_codes( $order );

		// Check if exist a tracking code for the order.
		if ( empty( $tracking_codes ) ) {
			return;
		}

		wc_get_template(
			'myaccount/melhor-envio-tracking-codes.php',
			array(
				'tracking_codes' => $tracking_codes,
				'order'          => $order,
			),
			'',
			WC_Melhor_Envio::get_templates_path()
		);
	}


	public function action_order_tracking( $actions, $order ) {
		$tracking_codes = wc_melhor_envio_get_tracking_codes( $order );

		// Check if exist a tracking code for the order.
		if ( empty( $tracking_codes ) ) {
			return $actions;
		}

		foreach ( $tracking_codes as $key => $tracking_code ) {
			$actions[ 'melhor_envio_tracking_' . $key ] = array(
				'url'  => wc_melhor_envio_get_tracking_url( $tracking_code ),
				'name' => __( 'Rastrear envio', 'wc-melhor-envio' ),
			);
		}

		return $actions;
	}
}
