<?php

// desligar pedidos para principais caso seja  dokan e caso tenha subpedidos

namespace WC_MelhorEnvio\Admin\Orders;

use WC_Melhor_Envio;
use WC_MelhorEnvio\Traits\Helpers;
use WC_MelhorEnvio\Traits\Logger;

class Actions {
	use Logger, Helpers;

	public $logger = 'wc-melhor-envio-orders';

	public $integration;

	public function __construct() {
		add_action( 'woocommerce_admin_order_actions', [ $this, 'add_to_cart_action' ], 10, 2 );
		add_action( 'admin_enqueue_scripts', [ $this, 'load_custom_scripts' ], 10 );

		add_action( 'manage_shop_order_posts_custom_column', [ $this, 'tracking_code_orders_list' ], 100 );
		add_action( 'manage_woocommerce_page_wc-orders_custom_column', [ $this, 'tracking_code_orders_list' ], 100, 2 );

		add_action( 'woocommerce_order_item_get_formatted_meta_data', [ $this, 'adjust_metadata_display' ], 10, 2 );
	}


	public function add_to_cart_action( $actions, $order ) {
		$this->integration = $this->get_integration_settings();

		// mesmo que não seja permitido, pode imprimir etiqueta, se existir
		if ( $local_url = $order->get_meta( '_melhor_envio_pdf_local_url' ) ) {
			$actions[] = array(
				'url'    => $local_url,
				'name'   => __( 'Baixar PDF da etiqueta', 'woocommerce' ),
				'action' => "me_print_label",
			);
		} elseif ( $url = $order->get_meta( '_melhor_envio_print_url' ) ) {
			$actions[] = array(
				'url'    => $url,
				'name'   => __( 'Imprimir etiqueta de envio', 'woocommerce' ),
				'action' => "me_print_label",
			);
			if ( current_user_can( 'manage_woocommerce' ) ) {
				$actions[] = array(
					'url'    => rest_url( 'wc-melhor-envio/v1/labels/' . $order->get_id() . '/download' ),
					'name'   => __( 'Baixar PDF localmente', 'woocommerce' ),
					'action' => "me_add_to_cart",
				);
			}
		} elseif ( isset( $this->integration['allowed_status'] ) && $order->has_status( $this->integration['allowed_status'] ) && $this->can_request_label( $order ) ) {
			$actions[] = array(
				'url'    => add_query_arg( array(
					'action'    => 'melhor_envio_add_to_cart',
					'order_ids' => $order->get_id(),
				), admin_url( 'admin-ajax.php' ) ),
				'name'   => __( 'Gerar etiqueta de envio', 'woocommerce' ),
				'action' => "me_add_to_cart",
			);
		}

		return $actions;
	}


	public function load_custom_scripts() {
		wp_enqueue_style( 'wc-melhor-envio-orders', WC_Melhor_Envio::plugin_url() . '/assets/css/orders-1.0.0.css' );
		wp_enqueue_style( 'wc-melhor-envio-orders-fontawesome',
			'https://stackpath.bootstrapcdn.com/font-awesome/4.7.0/css/font-awesome.min.css' );

		wp_enqueue_script( 'wc-melhor-envio-orders', wc_melhor_envio_get_script_url(), array( 'jquery-blockui' ) );
	}


	/**
	 * Display tracking code into orders list.
	 *
	 * @param  string  $column  Current column.
	 */
	public function tracking_code_orders_list( $column, $order = null ) {
		if ( 'shipping_address' === $column ) {
			// HPOS not enabled
			if ( ! $order ) {

				global $post, $the_order;

				if ( $the_order ) {
					$order = $the_order;
				} elseif ( empty( $the_order ) && isset( $post->ID ) || isset( $post->ID ) && $the_order->get_id() !== $post->ID ) {

					$order = wc_get_order( $post->ID );
				}
			}

			if ( ! $order ) {
				return;
			}

			$codes = wc_melhor_envio_get_tracking_codes( $order );

			if ( ! empty( $codes ) ) {
				$tracking_codes = array_map( 'wc_melhor_envio_get_formatted_tracking_url', $codes );

				?>
                <div class="melhor-envio-tracking-code">
                    <small class="meta">
						<?php echo esc_html( _n( 'Código de rastreio:', 'Códigos de rastreio:',
							count( $tracking_codes ), 'wc-melhor-envio' ) ); ?>
						<?php echo implode( ' | ', $tracking_codes ); ?>
                    </small>
                </div>
				<?php
			}
		}
	}


	public function adjust_metadata_display( $formatted_meta, $item ) {
		foreach ( $formatted_meta as $key => $meta ) {
			if ( 'melhorenvio_method_id' === $meta->key ) {
				$formatted_meta[ $key ]->display_key = __( 'ID do método', 'wc-melhor-envio' );
			} elseif ( 'melhorenvio_original_cost' === $meta->key ) {
				$formatted_meta[ $key ]->display_key   = __( 'Valor da cotação', 'wc-melhor-envio' );
				$formatted_meta[ $key ]->display_value = wc_price( $meta->value );
			} elseif ( 'melhorenvio_delivery_time' === $meta->key ) {
				$formatted_meta[ $key ]->display_key   = __( 'Prazo de entrega', 'wc-melhor-envio' );
				$formatted_meta[ $key ]->display_value = $meta->value . __( ' dias', 'wc-melhor-envio' );
			}
		}

		return $formatted_meta;
	}
}
