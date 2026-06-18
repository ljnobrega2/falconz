<?php

namespace WC_MelhorEnvio\Admin\Bulk_Actions\Cart;

use WC_MelhorEnvio\Pipeline\Label_Pipeline;
use WC_MelhorEnvio\Traits\Helpers;
use WC_MelhorEnvio\Traits\Logger;
use WC_Order;

class Auto_Fulfillment {
	use Helpers, Logger;

	protected $logger = 'wc-melhor-envio-auto-fulfillment';

	public function __construct() {
		add_action( 'woocommerce_order_status_changed', [ $this, 'auto_process' ], 110, 4 );
	}

	public function auto_process( $order_id, $from, $to, $order ) {
		// Guard de reentrância: impede que update_status() chamado dentro do pipeline
		// re-dispare este hook para o mesmo pedido no mesmo request, causando travamento.
		static $processing = [];
		if ( isset( $processing[ $order_id ] ) ) {
			return;
		}
		$processing[ $order_id ] = true;

		$settings = $this->get_integration_settings();

		if ( empty( $settings['full_auto_pipeline'] ) || 'yes' !== $settings['full_auto_pipeline'] ) {
			return;
		}

		if ( empty( $settings['status_full_pipeline'] ) || 'none' === $settings['status_full_pipeline'] ) {
			return;
		}

		$configured_status = str_replace( 'wc-', '', $settings['status_full_pipeline'] );
		$current_status    = str_replace( 'wc-', '', $to );

		if ( $current_status !== $configured_status ) {
			return;
		}

		if ( ! $this->can_request_label( $order ) ) {
			$this->log( 'Pedido #' . $order_id . ' bloqueado pelo can_request_label.' );
			return;
		}

		$auto_state   = (string) $order->get_meta( '_melhor_envio_auto_pipeline_state' );
		$label_status = (string) $order->get_meta( '_melhor_envio_label_status' );
		$started_at   = absint( $order->get_meta( '_melhor_envio_auto_pipeline_started_at' ) );
		$is_recent_run = $started_at && ( time() - $started_at ) < 15 * MINUTE_IN_SECONDS;

		/*
		 * Não travar para sempre pedidos que deram erro temporário na API.
		 * Antes, state=error ou label_status=error bloqueavam novas tentativas.
		 * Agora erro anterior permite retry seguro; Cart/Checkout/Generate já reutilizam
		 * os metadados existentes e não recompram se já houver compra gravada.
		 */
		$already_finished = (bool) (
			$order->get_meta( '_melhor_envio_print_url' )
			|| $order->get_meta( '_melhor_envio_pdf_local_path' )
			|| in_array( $label_status, [ 'printed', 'downloaded' ], true )
			|| 'done' === $auto_state
		);

		$is_running = (bool) (
			'processing' === $label_status
			|| 'running' === $auto_state
		);

		if ( $already_finished || ( $is_running && $is_recent_run ) ) {
			$this->log( 'Auto pipeline ignorado para pedido #' . $order_id . ' (state=' . $auto_state . ', label_status=' . $label_status . ').' );
			return;
		}

		if ( 'error' === $auto_state || 'error' === $label_status || ( $is_running && ! $is_recent_run ) ) {
			$this->log( 'Auto pipeline retry liberado para pedido #' . $order_id . ' (state=' . $auto_state . ', label_status=' . $label_status . ').' );
			$order->delete_meta_data( '_melhor_envio_auto_pipeline_error' );
			$order->delete_meta_data( '_melhor_envio_label_error' );
			$order->delete_meta_data( '_melhor_envio_label_status' );
		}

		$download_pdf = ! empty( $settings['auto_download_pdf'] ) && 'yes' === $settings['auto_download_pdf'];
		$status_after = ( empty( $settings['status_after_print'] ) || 'none' === $settings['status_after_print'] ) ? false : $settings['status_after_print'];

		$this->log( 'Iniciando pipeline automático imediato para pedido #' . $order_id . ' (status: ' . $current_status . ')' );

		$order->update_meta_data( '_melhor_envio_auto_pipeline_state', 'running' );
		$order->update_meta_data( '_melhor_envio_auto_pipeline_started_at', time() );
		$order->save();

		try {
			$pipeline = new Label_Pipeline();
			$pipeline->process( $order, $download_pdf );

			if ( $status_after && ! $order->has_status( $status_after ) ) {
				$order->update_status( $status_after, 'Fluxo automático do Melhor Envio concluído.' );
			} else {
				$order->add_order_note( 'Fluxo automático do Melhor Envio concluído.' );
			}

			$order->update_meta_data( '_melhor_envio_auto_pipeline_state', 'done' );
			$order->delete_meta_data( '_melhor_envio_auto_pipeline_error' );
			$order->save();
		} catch ( \Throwable $e ) {
			$this->log( 'Erro no auto pipeline pedido #' . $order_id . ': ' . $e->getMessage() );
			$order->update_meta_data( '_melhor_envio_auto_pipeline_state', 'error' );
			$order->update_meta_data( '_melhor_envio_auto_pipeline_error', $e->getMessage() );
			$order->update_meta_data( '_melhor_envio_label_status', 'error' );
			$order->update_meta_data( '_melhor_envio_label_error', $e->getMessage() );
			$order->save();
			$order->add_order_note( 'Erro no fluxo automático do Melhor Envio: ' . $e->getMessage() );

			// Saldo insuficiente: mover para status específico para facilitar identificação.
			if ( false !== stripos( $e->getMessage(), 'Saldo disponível insuficiente' ) ) {
				$order->update_status( 'saldoinsuficiente', 'Senderzz: saldo insuficiente na carteira para emissão da etiqueta.' );
			}
		}
	}
}
