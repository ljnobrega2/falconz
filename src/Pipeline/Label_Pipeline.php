<?php

namespace WC_MelhorEnvio\Pipeline;

use Exception;
use WC_MelhorEnvio\Api\Cart;
use WC_MelhorEnvio\Api\Checkout;
use WC_MelhorEnvio\Api\Download_Label;
use WC_MelhorEnvio\Api\Generate_Label;
use WC_MelhorEnvio\Api\Print_Label;
use WC_MelhorEnvio\Traits\Helpers;
use WC_MelhorEnvio\Traits\Logger;

class Label_Pipeline {
	use Helpers, Logger;

	protected $logger = 'wc-melhor-envio-pipeline';

	public function process( $order, $download_pdf = true ) {
		try {
			$settings = $this->get_integration_settings();

			$attempts = max( 10, absint( $settings['pipeline_attempts'] ?? 10 ) );
			$delay    = max( 4, absint( $settings['pipeline_delay'] ?? 4 ) );

			$cart      = new Cart();
			$checkout  = new Checkout();
			$generator = new Generate_Label();
			$print     = new Print_Label();
			$download  = new Download_Label();

			foreach ( [ $cart, $checkout, $generator, $print, $download ] as $api ) {
				$api->add_additional_param( 'order', $order );
			}

			$order->update_meta_data( '_melhor_envio_label_status', 'processing' );
			$order->update_meta_data( '_melhor_envio_label_last_attempt', time() );
			$order->delete_meta_data( '_melhor_envio_label_error' );
			$order->save();

			do_action( 'wc_melhor_envio_pipeline_before_process', $order );

			$cart->add_to_cart( $order );
			$checkout->buy( $order );

			/*
			 * IMPORTANTE:
			 * generate() pode aceitar a geração no ME, mas o PDF/link ainda não estar pronto.
			 * Por isso não podemos tratar "unknown / ainda não processada" como erro financeiro.
			 */
			$generator->generate( $order, $attempts, $delay );

			$printed        = null;
			$last_exception = null;

			for ( $i = 1; $i <= $attempts; $i++ ) {
				try {
					$printed = $print->print( $order );

					if ( $printed ) {
						break;
					}
				} catch ( Exception $e ) {
					$last_exception = $e;

					$this->log(
						sprintf(
							'Polling impressão etiqueta [tentativa %d/%d] pedido #%s: %s',
							$i,
							$attempts,
							$order->get_id(),
							$e->getMessage()
						)
					);

					if ( $i < $attempts ) {
						sleep( $delay );
					}
				}
			}

			/*
			 * Se comprou/gerou item no ME, mas o print ainda não ficou disponível,
			 * NÃO joga para erro. Mantém como processing para reprocessar depois.
			 */
			if ( ! $printed ) {
				$item_id     = (string) $order->get_meta( '_melhor_envio_item_id' );
				$me_order_id = (string) $order->get_meta( '_melhor_envio_order_id' );
				$was_bought  = trim( $item_id ) !== '' || trim( $me_order_id ) !== '';

				if ( $was_bought ) {
					$msg = $last_exception
						? $last_exception->getMessage()
						: 'Etiqueta aceita pelo Melhor Envio, mas ainda não disponível para impressão/download.';

					$order->update_meta_data( '_melhor_envio_label_status', 'processing' );
					$order->update_meta_data( '_melhor_envio_label_error', $msg );
					$order->update_meta_data( '_melhor_envio_label_next_retry', time() + 60 );
					$order->save();

					$order->add_order_note(
						'Senderzz: etiqueta comprada/aceita no Melhor Envio, mas ainda está processando. Não houve estorno nem mudança para erro. Tente reprocessar em instantes. Detalhe: ' . $msg
					);

					if ( ! wp_next_scheduled( 'senderzz_retry_label_pipeline', [ $order->get_id() ] ) ) {
						wp_schedule_single_event( time() + 60, 'senderzz_retry_label_pipeline', [ $order->get_id() ] );
					}

					return [
						'status'  => 'processing',
						'message' => $msg,
					];
				}

				throw $last_exception ?: new Exception( 'Falha ao gerar link da etiqueta.' );
			}

			$result = [ 'print' => $printed['data'] ?? '' ];

			if ( $download_pdf ) {
				$downloaded = $download->download( $order );
				$result['download'] = $downloaded['data'] ?? [];
			}

			$order->update_meta_data( '_melhor_envio_label_status', $download_pdf ? 'downloaded' : 'printed' );
			$order->delete_meta_data( '_melhor_envio_label_error' );
			$order->delete_meta_data( '_melhor_envio_label_next_retry' );
			$order->save();

			do_action( 'wc_melhor_envio_pipeline_after_process', $order, $result );

			try {
				$senderzz_label_data = function_exists( 'senderzz_order_shipping_snapshot' )
					? senderzz_order_shipping_snapshot( $order )
					: [];

				\WC_MelhorEnvio\Database\Label_Table::upsert( array_merge( $senderzz_label_data, [
					'order_id'    => $order->get_id(),
					'shipment_id' => (string) $order->get_meta( '_melhor_envio_item_id' ),
					'protocol'    => (string) $order->get_meta( '_melhor_envio_order_id' ),
					'status'      => $download_pdf ? 'downloaded' : 'printed',
					'print_url'   => (string) $order->get_meta( '_melhor_envio_print_url' ),
					'pdf_path'    => (string) $order->get_meta( '_melhor_envio_pdf_local_path' ),
					'pdf_url'     => (string) $order->get_meta( '_melhor_envio_pdf_local_url' ),
					'operator_id' => (int) get_current_user_id(),
				] ) );
			} catch ( \Throwable $e ) {
				$this->log( 'Erro ao sincronizar Label_Table: ' . $e->getMessage() );
			}

			return $result;

		} catch ( \Throwable $e ) {
			if ( $order instanceof \WC_Order ) {
				$item_id     = (string) $order->get_meta( '_melhor_envio_item_id' );
				$me_order_id = (string) $order->get_meta( '_melhor_envio_order_id' );
				$was_bought  = trim( $item_id ) !== '' || trim( $me_order_id ) !== '';

				if ( $was_bought && $this->senderzz_is_async_label_processing_error( $e->getMessage() ) ) {
					$order->update_meta_data( '_melhor_envio_label_status', 'processing' );
					$order->update_meta_data( '_melhor_envio_label_error', $e->getMessage() );
					$order->update_meta_data( '_melhor_envio_label_next_retry', time() + 60 );
					$order->save();

					$order->add_order_note(
						'Senderzz: Melhor Envio aceitou a etiqueta, mas ela ainda está processando. Pedido mantido fora de erro e carteira mantida debitada. Reprocessar em instantes. Detalhe: ' . $e->getMessage()
					);

					if ( ! wp_next_scheduled( 'senderzz_retry_label_pipeline', [ $order->get_id() ] ) ) {
						wp_schedule_single_event( time() + 60, 'senderzz_retry_label_pipeline', [ $order->get_id() ] );
					}

					return [
						'status'  => 'processing',
						'message' => $e->getMessage(),
					];
				}

				$order->update_meta_data( '_melhor_envio_label_status', 'error' );
				$order->update_meta_data( '_melhor_envio_label_error', $e->getMessage() );
				$order->save();

				if ( $was_bought ) {
					$order->update_status( 'erro', 'Senderzz: erro ao gerar PDF da etiqueta após compra no Melhor Envio. Carteira mantida debitada para reprocessamento.' );
					$order->add_order_note( 'Senderzz: etiqueta comprada no Melhor Envio, mas o PDF não foi gerado/salvo. NÃO houve estorno automático da carteira. Reprocessar a etiqueta ou cancelar o pedido. Erro: ' . $e->getMessage() );
				} else {
					if ( function_exists( 'senderzz_wallet_release_order' ) ) {
						senderzz_wallet_release_order( $order, 'erro no pipeline antes da compra: ' . $e->getMessage() );
					}

					$order->update_status( 'erro', 'Senderzz: erro antes da compra da etiqueta. Reserva/liberação de carteira processada quando aplicável.' );
					$order->add_order_note( 'Senderzz: erro antes da compra da etiqueta no Melhor Envio. Reserva/liberação de carteira processada quando aplicável. Erro: ' . $e->getMessage() );
				}
			}

			throw $e;
		}
	}

	private function senderzz_is_async_label_processing_error( string $message ): bool {
		$message = strtolower( $message );

		$markers = [
			'ainda não processada',
			'ainda nao processada',
			'ainda não disponível',
			'ainda nao disponivel',
			'não processada após',
			'nao processada apos',
			'unknown',
			'tente novamente em instantes',
			'processando',
		];

		foreach ( $markers as $marker ) {
			if ( strpos( $message, $marker ) !== false ) {
				return true;
			}
		}

		return false;
	}
}