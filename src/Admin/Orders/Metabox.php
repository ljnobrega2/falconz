<?php

namespace WC_MelhorEnvio\Admin\Orders;

use WC_Melhor_Envio;
use WC_MelhorEnvio\Traits\Helpers;
use WC_MelhorEnvio\Traits\Logger;

class Metabox {
	use Logger, Helpers;

	public $integration;

	function add_hooks() {
		add_action( 'add_meta_boxes', [ $this, 'register_metabox' ] );
		add_action( 'wp_ajax_melhor_envio_delete_meta', [ $this, 'delete_content' ] );
		add_action( 'wp_ajax_melhor_envio_delete_meta_reverse', [ $this, 'delete_reverse_content' ] );

	}

	public function register_metabox() {
		$hpos_enabled = false;

		if ( defined( 'WC_VERSION' ) && version_compare( WC_VERSION, '7.1', '>=' ) ) {
			$hpos_enabled = wc_get_container()->get( \Automattic\WooCommerce\Internal\DataStores\Orders\CustomOrdersTableController::class )->custom_orders_table_usage_is_enabled();
		}

		$screen = $hpos_enabled ? wc_get_page_screen_id( 'shop-order' ) : 'shop_order';

		add_meta_box(
			'wc_melhor_envio_admin',
			'Etiquetas Melhor Envio',
			[ $this, 'metabox_content' ],
			$screen,
			'side',
			'default'
		);
	}


	public function metabox_content( $post ) {
		$order = is_a( $post, '\WC_Order' ) ? $post : wc_get_order( $post->ID );

		if ( ! $order || ! $this->can_request_label( $order ) ) {
			return;
		}

		$order_id = $order->get_id();

		$this->render_content( $order_id );
	}


	public function render_content( $order_id ) {
		$order = is_a( $order_id, '\WC_Order' ) ? $order_id : wc_get_order( $order_id );

		$invoice_id = $order->get_meta( '_order_invoice_id' );

		$service_id                 = $order->get_meta( '_order_me_custom_service_id' );
		$customer_choice_service_id = $this->get_customer_choice_service_id( $order );
		$current_service_id         = $service_id ? $service_id : $customer_choice_service_id;

		$item_id   = $order->get_meta( '_melhor_envio_item_id' );
		$protocol  = $order->get_meta( '_melhor_envio_order_id' );
		$print_url = $order->get_meta( '_melhor_envio_print_url' );
		$local_pdf = $order->get_meta( '_melhor_envio_pdf_local_url' );
		$pipeline_status = $order->get_meta( '_melhor_envio_label_status' );
		$pipeline_error  = $order->get_meta( '_melhor_envio_label_error' );
		$has_label = $order->get_meta( '_melhor_envio_label_generated' ) || ! ! $print_url;
		$paid      = $order->get_meta( '_melhor_envio_bought_shipping' ) || ! ! $print_url;

		$reverse_item_id   = $order->get_meta( '_melhor_envio_item_id_reverse' );
		$reverse_protocol  = $order->get_meta( '_melhor_envio_order_id_reverse' );
		$reverse_paid      = $order->get_meta( '_melhor_envio_bought_shipping_reverse' );
		$reverse_has_label = $order->get_meta( '_melhor_envio_label_generated_reverse' );
		$reverse_print_url = $order->get_meta( '_melhor_envio_print_url_reverse' );

		echo '<div class="me-metabox-content">';

		if ( $reverse_item_id ) {
			echo '<h4>' . __( 'Logística reversa', 'wc-melhor-envio' ) . '</h4>';

			printf( '<strong>ID</strong>: %s<br />', $reverse_item_id ? $reverse_item_id : 'Não  disponível' );
			printf( '<strong>Pedido</strong>: %s<br />', $reverse_protocol ? $reverse_protocol : 'Não  disponível' );
			printf( '<strong>Envio pago</strong>: %s<br />', $reverse_paid ? 'Sim' : 'Não' );
			printf( '<strong>Etiqueta gerada?</strong>: %s<br />', $reverse_has_label ? 'Sim' : 'Não' );

			if ( $reverse_print_url ) {
				echo '<br /><a class="button" href="' . $reverse_print_url . '" target="_blank">Imprimir</a><br />';
			}

			echo '<a href="' . add_query_arg( array(
					'action'   => 'melhor_envio_delete_meta_reverse',
					'order_id' => $order_id,
				),
					admin_url( 'admin-ajax.php' ) ) . '" style="color: red; display: inline-block; font-size: var(--sz-text-meta); margin-top: 15px;" class="delete-melhor-envio-data">Excluir logística reversa</a>';

			echo '<hr />';
		}

		$tracking_codes = wc_melhor_envio_get_tracking_codes( $order );

		if ( $tracking_codes ) {
			echo '<h4>' . __( 'Códigos de rastreio', 'wc-melhor-envio' ) . '</h4>';
			echo '<ul>';
			foreach ( $tracking_codes as $company => $tracking_code ) {
				echo sprintf(
					'<li>%s</li>',
					wc_melhor_envio_get_formatted_tracking_url( $tracking_code )
				);
			}
			echo '</ul>';
		}

		$this->integration = $this->get_integration_settings();

		$status_is_valid = isset( $this->integration['allowed_status'] ) && $order->has_status( $this->integration['allowed_status'] );
		$can_request     = $status_is_valid && $this->can_request_label( $order );

		$show_form = true;

		if ( ! $item_id && ! $can_request ) {
			if ( $status_is_valid ) {
				echo 'Você não tem permissões para solicitar etiquetas neste pedido.';
			} else {
				echo 'Não é possível solicitar etiquetas no status atual.';
			}

			$show_form = false;
		} elseif ( $item_id ) {

			echo '<h4>' . __( 'Envio', 'wc-melhor-envio' ) . '</h4>';

			printf( '<strong>ID</strong>: %s<br />', $item_id ? $item_id : 'Não  disponível' );
			printf( '<strong>Pedido</strong>: %s<br />', $protocol ? $protocol : 'Não  disponível' );
			printf( '<strong>Envio pago</strong>: %s<br />', $paid ? 'Sim' : 'Não' );
			printf( '<strong>Etiqueta gerada?</strong>: %s<br />', $has_label ? 'Sim' : 'Não' );
			printf( '<strong>Status pipeline</strong>: %s<br />', $pipeline_status ? esc_html( $pipeline_status ) : 'Não disponível' );

			if ( $print_url ) {
				echo '<br /><a class="button" href="' . $print_url . '" target="_blank">Imprimir</a> ';
			}

			if ( $local_pdf ) {
				echo '<a class="button" href="' . esc_url( $local_pdf ) . '" target="_blank">Baixar PDF</a><br />';
			}

			if ( $pipeline_error ) {
				echo '<br /><small style="color:#b32d2e">' . esc_html( $pipeline_error ) . '</small><br />';
			}

			echo '<a href="' . add_query_arg( array( 'action' => 'melhor_envio_delete_meta', 'order_id' => $order_id ),
					admin_url( 'admin-ajax.php' ) ) . '" style="color: red; display: inline-block; font-size: var(--sz-text-meta); margin-top: 15px;" class="delete-melhor-envio-data">Excluir dados</a>';

			echo '<br/><small>' . __( 'Dados atualizados na última solicitação feita pelo site. Caso tenha modificado o pedido diretamente no Melhor Envio, o site não será atualizado. Solicitar uma nova etiqueta irá sobrepor a já existente, mas não irá cancelá-la no Melhor Envio.',
					'wc-melhor-envio' ) . '</small><br /><br />';
		}

		if ( $show_form ) {
			include 'views/html-order-request-label.php';
		}

		echo '</div>';
	}

	public function delete_reverse_content() {
		return $this->delete_content( '_reverse' );
	}

	public function delete_content( $suffix = '' ) {
		if ( ! isset( $_GET['order_id'] ) ) {
			wp_send_json_error( 'Pedido inválido.' );
		}

		if ( ! apply_filters( 'wc_melhor_envio_can_delete_data', current_user_can( 'manage_woocommerce' ) ) ) {
			wp_send_json_error( __( 'Sem permissões.', 'wc-melhor-envio' ) );
		}

		$order_id = intval( $_GET['order_id'] );

		$order = wc_get_order( $order_id );

		if ( ! $order ) {
			wp_send_json_error( __( 'Pedido inválido.', 'wc-melhor-envio' ) );
		}

		$item_id = $order->get_meta( '_melhor_envio_item_id' . $suffix );
		$note    = sprintf(
			__( 'Excluído dados de postagem %s ID %s', 'wc-melhor-envio' ),
			'_reverse' === $suffix ? 'reversa' : '',
			$item_id
		);

		if ( $item_id ) {
			$order->delete_meta_data( '_melhor_envio_item_id' . $suffix );
			$order->delete_meta_data( '_melhor_envio_order_id' . $suffix );
			$order->delete_meta_data( '_melhor_envio_bought_shipping' . $suffix );
			$order->delete_meta_data( '_melhor_envio_label_generated' . $suffix );
			$order->delete_meta_data( '_melhor_envio_print_url' . $suffix );
			$order->delete_meta_data( '_melhor_envio_label_status' . $suffix );
			$order->delete_meta_data( '_melhor_envio_label_error' . $suffix );
			$order->delete_meta_data( '_melhor_envio_pdf_local_path' . $suffix );
			$order->delete_meta_data( '_melhor_envio_pdf_local_url' . $suffix );
			$order->delete_meta_data( '_melhor_envio_auto_pipeline_state' );
			$order->delete_meta_data( '_melhor_envio_auto_pipeline_started_at' );
			$order->delete_meta_data( '_melhor_envio_auto_pipeline_error' );

			// tracking codes
			$order->delete_meta_data( '_melhor_envio_tracking_codes' . $suffix );

			if ( $status = $order->get_meta( '_me_original_status' ) ) {
				$order->update_status( $status, $note . ' Restaurando status anterior.' );
			} else {
				$order->add_order_note( $note, 0, true );
			}

			$order->save();
		}

		ob_start();
		$this->render_content( $order );
		$html = ob_get_clean();

		wp_send_json_success( $html );
	}
}
