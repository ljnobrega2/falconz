<?php

namespace WC_MelhorEnvio\Api;

use WC_MelhorEnvio\Traits\Helpers;
use WC_MelhorEnvio\Traits\Logger;

/**
 * Cancel_Label
 *
 * Cancela/exclui uma etiqueta no Melhor Envio via DELETE /api/v2/me/cart/{item_id}
 * e libera/estorna o saldo na carteira Senderzz.
 */
class Cancel_Label extends Melhor_Envio_Api {
	use Helpers, Logger;

	protected $logger = 'wc-melhor-envio-cancel-label';

	/**
	 * Cancela etiqueta no ME e estorna saldo.
	 *
	 * @param \WC_Order $order
	 * @return array ['success' => bool, 'message' => string]
	 */
	public function cancel( \WC_Order $order ): array {
		$item_id  = (string) $order->get_meta( '_melhor_envio_item_id' );
		$protocol = (string) $order->get_meta( '_melhor_envio_order_id' );

		// Se já foi comprada (tem protocolo), tenta cancelar no ME
		if ( $protocol ) {
			$this->log( 'Tentando cancelar etiqueta com protocolo ' . $protocol . ' para pedido #' . $order->get_id() );

			$response = $this->do_request( '/api/v2/me/shipment/cancel', 'POST', [
				'order' => $protocol,
			] );

			if ( is_wp_error( $response ) ) {
				$this->log( 'Erro ao cancelar protocolo no ME: ' . $response->get_error_message() );
				// Não bloqueia — prossegue com limpeza local
			} else {
				$code = wp_remote_retrieve_response_code( $response );
				$this->log( 'Resposta cancelamento ME: ' . $code );
			}
		} elseif ( $item_id ) {
			// Só está no carrinho, deleta do carrinho
			$this->log( 'Removendo item ' . $item_id . ' do carrinho ME para pedido #' . $order->get_id() );

			$response = $this->do_request( '/api/v2/me/cart/' . $item_id, 'DELETE' );

			if ( is_wp_error( $response ) ) {
				$this->log( 'Erro ao remover do carrinho ME: ' . $response->get_error_message() );
			} else {
				$code = wp_remote_retrieve_response_code( $response );
				$this->log( 'Resposta remoção carrinho ME: ' . $code );
			}
		}

		// Libera/estorna saldo na carteira independente do resultado no ME
		$this->release_wallet( $order );

		// Limpa todos os metadados de etiqueta
		$this->clear_label_meta( $order );

		$order->add_order_note( 'Senderzz: etiqueta cancelada e saldo liberado/estornado.' );
		$order->save();

		$this->log( 'Cancelamento concluído para pedido #' . $order->get_id() );

		return [ 'success' => true, 'message' => 'Etiqueta cancelada com sucesso.' ];
	}

	/**
	 * Libera reserva ou estorna débito na carteira.
	 */
	private function release_wallet( \WC_Order $order ): void {
		if ( function_exists( 'senderzz_wallet_refund_order' ) ) {
			senderzz_wallet_refund_order( $order->get_id() );
		}
	}

	/**
	 * Limpa todos os metadados relacionados à etiqueta.
	 */
	private function clear_label_meta( \WC_Order $order ): void {
		$metas = [
			'_melhor_envio_item_id',
			'_melhor_envio_order_id',
			'_melhor_envio_print_url',
			'_melhor_envio_pdf_local_path',
			'_melhor_envio_pdf_local_url',
			'_melhor_envio_label_status',
			'_melhor_envio_label_error',
			'_melhor_envio_auto_pipeline_state',
			'_melhor_envio_auto_pipeline_error',
			'_melhor_envio_auto_pipeline_started_at',
			'_melhor_envio_tracking_codes',
		];

		foreach ( $metas as $meta ) {
			$order->delete_meta_data( $meta );
		}
	}
}
