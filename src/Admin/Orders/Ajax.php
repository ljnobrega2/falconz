<?php

namespace WC_MelhorEnvio\Admin\Orders;

use Exception;
use WC_MelhorEnvio\Api\Cart;
use WC_MelhorEnvio\Api\Checkout;
use WC_MelhorEnvio\Api\Generate_Label;
use WC_MelhorEnvio\Api\Print_Label;
use WC_MelhorEnvio\Api\Download_Label;
use WC_MelhorEnvio\Traits\Helpers;
use WC_MelhorEnvio\Traits\Logger;

class Ajax {
	use Logger, Helpers;

	public $integration;

	public $logger = 'wc-melhor-envio-orders';

	function __construct() {
		add_action( 'wp_ajax_melhor_envio_add_to_cart', [ $this, 'handle_request' ] );
	}

	public function handle_request() {
		try {
			if ( $_SERVER['REQUEST_METHOD'] !== 'POST' ) {
				throw new Exception( __( 'Invalid request type', 'wc-melhor-envio' ) );
			}

			if ( ! isset( $_GET['order_ids'] ) ) {
				throw new Exception( __( 'Pedido inválido', 'wc-melhor-envio' ) );
			}

			$order_ids = array_map( 'intval', explode( ',', $_GET['order_ids'] ) );

			if ( 1 !== count( $order_ids ) ) {
				throw new Exception( __( 'Selecione pelo menos 1 pedido válido.', 'wc-melhor-envio' ) );
			}

			// Verificar nonce — aceita nonce do metabox single ou da bulk action
			$nonce_single = isset( $_POST['melhor_envio_nonce'] ) ? $_POST['melhor_envio_nonce'] : '';
			$nonce_bulk   = isset( $_POST['melhor_envio_bulk_nonce'] ) ? $_POST['melhor_envio_bulk_nonce'] : '';

			$nonce_valid = ( $nonce_single && wp_verify_nonce( $nonce_single, 'melhor_envio_request_label_' . $order_ids[0] ) )
			               || ( $nonce_bulk && wp_verify_nonce( $nonce_bulk, 'melhor_envio_bulk_action' ) );

			if ( ! $nonce_valid ) {
				throw new Exception( __( 'Ação não autorizada. Recarregue a página e tente novamente.', 'wc-melhor-envio' ) );
			}

			$is_reverse = wc_melhor_envio_enabled_reverse_logistic() && isset( $_POST['reverse'] ) && 'true' === $_POST['reverse'];

			$order = wc_get_order( $order_ids[0] );

			if ( ! $order ) {
				throw new Exception( __( 'Erro ao carregar o pedido.', 'wc-melhor-envio' ) );
			}

			$this->integration = get_option( 'woocommerce_wc-melhor-envio_settings', [] );

			if ( ! $this->can_request_label( $order ) ) {
				throw new Exception( __( 'Sem permissões para solicitar etiqueta neste pedido.', 'wc-melhor-envio' ) );
			}

			if ( $is_reverse ) {
				return $this->process_reverse_shipping( $order );
			}

			if ( isset( $this->integration['allowed_status'] ) && ! $order->has_status( $this->integration['allowed_status'] ) ) {
				throw new Exception( __( 'Status do pedido é inválido.', 'wc-melhor-envio' ) );
			}

			$cart      = new Cart();
			$checkout  = new Checkout();
			$generator = new Generate_Label();
			$print     = new Print_Label();
			$download  = new Download_Label();

			do_action( 'wc_melhor_envio_ajax_before_process', $order );

			$cart->add_additional_param( 'order', $order );
			$checkout->add_additional_param( 'order', $order );
			$generator->add_additional_param( 'order', $order );
			$print->add_additional_param( 'order', $order );
			$download->add_additional_param( 'order', $order );

			if ( isset( $_POST['nfe'] ) ) {
				$order->update_meta_data( '_order_invoice_id', wc_clean( $_POST['nfe'] ) );
				$order->save();
			} else {
				// no invoice anymore
				$order->delete_meta_data( '_order_invoice_id' );
			}

			if ( isset( $_POST['service'] ) ) {
				$order->update_meta_data( '_order_me_custom_service_id', wc_clean( $_POST['service'] ) );
				$order->save();
			} else {
				// no custom method anymore
				$order->delete_meta_data( '_order_me_custom_service_id' );
			}

			$response = $cart->add_to_cart( $order );

			$order->update_meta_data( '_me_original_status', $order->get_status() );
			$order->save();

			if ( ! isset( $this->integration['add_to_cart_only'] ) || 'yes' !== $this->integration['add_to_cart_only'] ) {

				$checkout->buy( $order );
				$generator->generate( $order );

				$response = $print->print( $order );

				if ( ! empty( $this->integration['auto_download_pdf'] ) && 'yes' === $this->integration['auto_download_pdf'] ) {
					$download->download( $order );
				}

				$link = apply_filters( 'wc_melhor_envio_print_ajax_response',
					'<a class="button wc-action-button wc-action-button-me_print_label me_print_label" href="' . $response['data'] . '" aria-label="Imprimir etiqueta de envio">Imprimir etiqueta de envio</a>',
					$order, $response );

				$status = ( empty( $this->integration['status_after_print'] ) || 'none' === $this->integration['status_after_print'] ) ? false : $this->integration['status_after_print'];

				if ( $status && ! $order->has_status( $status ) ) {
					$order->update_status( $status, 'Etiqueta impressa.' );
				} else {
					$order->add_order_note( 'Etiqueta impressa.' );
				}

			} else {
				$status = ( empty( $this->integration['status_after_cart'] ) || 'none' === $this->integration['status_after_cart'] ) ? false : $this->integration['status_after_cart'];

				if ( $status && ! $order->has_status( $status ) ) {
					$order->update_status( $status, 'Pedido enviado ao Melhor Envio.' );
				} else {
					$order->add_order_note( 'Pedido enviado ao Melhor Envio.' );
				}
			}

			if ( ! isset( $this->integration['add_to_cart_only'] ) || 'yes' !== $this->integration['add_to_cart_only'] ) {
				do_action( 'wc_melhor_envio_label_processed', $order, $response );
			}

			if ( isset( $_POST['is_single'] ) && 'true' === $_POST['is_single'] ) {
				$metabox = new Metabox();
				ob_start();
				echo '<div class="inside">';
				$metabox->render_content( $order->get_id() );
				echo '</div>';
				$link = ob_get_clean();
			}

			wp_send_json_success( $link );

		} catch ( \Exception $e ) {
			if ( isset( $order ) && $order instanceof \WC_Order && function_exists( 'senderzz_wallet_release_order' ) ) {
				senderzz_wallet_release_order( $order, 'erro na emissao: ' . $e->getMessage() );
			}
			wp_send_json_error( $e->getMessage() );
		}
	}


	public function process_reverse_shipping( $order ) {
		$cart      = new Cart();
		$checkout  = new Checkout();
		$generator = new Generate_Label();
		$print     = new Print_Label();

		$cart->add_additional_param( 'order', $order );
		$checkout->add_additional_param( 'order', $order );
		$generator->add_additional_param( 'order', $order );
		$print->add_additional_param( 'order', $order );

		$dynamic_data = [
			'reverse' => true,
			'nfe'     => isset( $_POST['nfe'] ) ? wc_clean( $_POST['nfe'] ) : '',
			'service' => isset( $_POST['service'] ) ? wc_clean( $_POST['service'] ) : '',
		];

		$cart->add_additional_param( 'dynamic_data', $dynamic_data );
		$checkout->add_additional_param( 'dynamic_data', $dynamic_data );
		$generator->add_additional_param( 'dynamic_data', $dynamic_data );
		$print->add_additional_param( 'dynamic_data', $dynamic_data );

		$response = $cart->add_to_cart( $order );
		$response = $checkout->buy( $order );
		$response = $generator->generate( $order );
		$response = $print->print( $order );

		$link = apply_filters( 'wc_melhor_envio_print_ajax_response',
			'<a class="button wc-action-button wc-action-button-me_print_label me_print_label" href="' . $response['data'] . '" aria-label="Imprimir etiqueta de envio">Imprimir etiqueta de envio</a>',
			$order, $response );

		$order->add_order_note( sprintf( __( 'Logística reversa solicitada, notifique o cliente: %s',
			'wc-melhor-envio' ), $response['data'] ) );

		if ( isset( $_POST['is_single'] ) && 'true' === $_POST['is_single'] ) {
			$metabox = new Metabox();
			ob_start();
			echo '<div class="inside">';
			$metabox->render_content( $order->get_id() );
			echo '</div>';
			$link = ob_get_clean();
		}

		wp_send_json_success( $link );
	}
}
