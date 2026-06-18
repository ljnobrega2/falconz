<?php

namespace WC_MelhorEnvio\Methods;

/**
 * WooCommerce cart integration
 *
 * @package WC_JadLog/Classes/Cart
 * @since   1.0.0
 * @version 1.0.0
 */

defined( 'ABSPATH' ) || exit;

/**
 * Cart integration.
 */
class Delivery_Time {

	/**
	 * Init cart actions.
	 */
	public function __construct() {
		add_action( 'woocommerce_after_shipping_rate', array( $this, 'shipping_delivery_forecast' ), 100 );

		// WC Additional Days Per Product
		add_filter( 'wc_adpp_additional_days_custom_metadata', array( $this, 'wc_adpp_integration' ), 10, 3 );
	}

	/**
	 * Adds delivery forecast after method name.
	 *
	 * @param  WC_Shipping_Rate  $shipping_method  Shipping method data.
	 */
	public function shipping_delivery_forecast( $shipping_method ) {
		$meta_data = $shipping_method->get_meta_data();
		$total     = isset( $meta_data['melhorenvio_delivery_time'] ) ? intval( $meta_data['melhorenvio_delivery_time'] ) : 0;

		if ( $total ) {
			/* translators: %d: days to delivery */
			echo '<p><small>' . esc_html( sprintf( _n( 'Entrega em até %d dia útil', 'Entrega em até %d dias úteis',
					$total, 'wc-melhor-envio' ), $total ) ) . '</small></p>';
		}
	}


	public function wc_adpp_integration( $default, $meta_data, $rate ) {
		if ( isset( $meta_data['melhorenvio_delivery_time'] ) ) {
			return 'melhorenvio_delivery_time';
		}

		return $default;
	}
}
