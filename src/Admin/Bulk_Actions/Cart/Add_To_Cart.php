<?php

namespace WC_MelhorEnvio\Admin\Bulk_Actions\Cart;

use WC_MelhorEnvio\Api\Cart;
use WC_MelhorEnvio\Traits\Helpers;
use WC_MelhorEnvio\Traits\Logger;
use WC_Order;

class Add_To_Cart {
	use Helpers, Logger;

	protected $logger = 'wc-melhor-envio-add-to-cart';

	function __construct() {
		add_action( 'woocommerce_order_status_changed', [ $this, 'auto_add_to_cart' ], 100, 4 );
	}

	/**
	 * @param  int  $order_id
	 * @param  string  $from
	 * @param  string  $to
	 * @param  WC_Order  $order
	 *
	 * @return void
	 */
	public function auto_add_to_cart( $order_id, $from, $to, $order ) {
		// Guard de reentrância: impede re-execução causada por update_status() interno.
		static $processing = [];
		if ( isset( $processing[ $order_id ] ) ) {
			return;
		}
		$processing[ $order_id ] = true;

		$integration = get_option( 'woocommerce_wc-melhor-envio_settings', [] );

		if ( ! isset( $integration['add_to_cart_only'] ) || 'yes' !== $integration['add_to_cart_only'] ) {
			return;
		}

		$configured_status = str_replace( 'wc-', '', $integration['status_add_to_cart'] ?? '' );
		$current_status    = str_replace( 'wc-', '', $to );

		if ( ! $configured_status || $current_status !== $configured_status ) {
			return;
		}

		try {
			$cart = new Cart();

			$cart->add_additional_param( 'order', $order );

			$cart_id  = $order->get_meta( '_melhor_envio_item_id' );
			$protocol = $order->get_meta( '_melhor_envio_order_id' );

			if ( $cart_id || $protocol ) {
				throw new \Exception( 'Pedido já estava no carrinho. Se você removeu por engano, é necessário refazer manualmente' );
			}

			$cart->add_to_cart( $order );

			$order->update_meta_data( '_me_original_status', $order->get_status() );
			$order->update_meta_data( '_me_auto_added_to_cart', time() );

			$status = ( empty( $integration['status_after_cart'] ) || 'none' === $integration['status_after_cart'] ) ? false : $integration['status_after_cart'];

			if ( $status && ! $order->has_status( $status ) ) {
				$order->update_status( $status, 'Pedido enviado automaticamente ao Melhor Envio.' );
			} else {
				$order->add_order_note( 'Pedido enviado automaticamente ao Melhor Envio.' );
			}
		} catch ( \Exception $exception ) {
			$order->add_order_note( 'Ocorreu um erro ao enviar ao carrinho do Melhor Envio: ' . $exception->getMessage() );
		}
	}
}
