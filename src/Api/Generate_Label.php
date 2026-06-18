<?php

namespace WC_MelhorEnvio\Api;

use Exception;

class Generate_Label extends Melhor_Envio_Api {
	/**
	 * $logger
	 *
	 * @var string
	 */
	protected $logger = 'wc-melhor-envio-cart';
	/**
	 * $endpoint
	 *
	 * @var string
	 */
	private $endpoint = '/api/v2/me/shipment/generate';

	/**
	 * __construct
	 *
	 * @return void
	 */
	function __construct() {
		parent::__construct();
	}


	/**
	 * generate
	 * Solicita geração da etiqueta e faz polling para confirmar conclusão.
	 *
	 * @return array
	 */
	public function generate( $order, $attempts = null, $delay = null ) {
		$extra_params = $this->get_additional_params();
		$dynamic_data = isset( $extra_params['dynamic_data'] ) ? $extra_params['dynamic_data'] : [];

		$is_reverse = isset( $dynamic_data['reverse'] ) && $dynamic_data['reverse'];
		$suffix     = $is_reverse ? '_reverse' : '';


		$id = $order->get_meta( '_melhor_envio_item_id' . $suffix );

		if ( ! $id ) {
			throw new Exception( 'Compre o envio antes de gerar a etiqueta.' );
		}

		if ( ! $order->get_meta( '_melhor_envio_bought_shipping' . $suffix ) ) {
			throw new Exception( 'Compre o envio antes de gerar a etiqueta.' );
		}

		if ( $order->get_meta( '_melhor_envio_label_generated' . $suffix ) ) {
			return $this->render_success( 'Etiqueta já gerada com sucesso' );
		}

		$data = [
			'mode'   => 'public',
			'orders' => [ $id ],
		];

		$response = $this->do_request( $this->endpoint, 'POST', $data );

		if ( is_wp_error( $response ) ) {
			$this->log( 'Erro ao gerar etiqueta: ' . $response->get_error_message() );

			throw new Exception( 'Erro ao gerar etiqueta: ' . $response->get_error_message() );

		} elseif ( in_array( $response['response']['code'], [ 200, 201 ] ) ) {
			$this->log( 'Solicitação de geração aceita: ' . $response['body'] );

			$decoded_body = json_decode( $response['body'], true );
			if ( is_array( $decoded_body ) ) {
				$body_text = wp_json_encode( $decoded_body, JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES );
				if ( stripos( (string) $body_text, 'já está gerado' ) !== false || stripos( (string) $body_text, 'ja esta gerado' ) !== false ) {
					$order->update_meta_data( '_melhor_envio_label_generated' . $suffix, true );
					$order->save();
					return $this->render_success( 'Etiqueta já estava gerada no Melhor Envio' );
				}
			}

			// Polling: confirma que a etiqueta foi concluída na API antes de marcar como gerada
			$settings = $this->get_integration_settings();
			$max      = $attempts !== null ? max( 1, (int) $attempts ) : max( 1, absint( $settings['pipeline_attempts'] ?? 3 ) );
			$wait     = $delay !== null   ? max( 1, (int) $delay )   : max( 1, absint( $settings['pipeline_delay'] ?? 3 ) );

			$confirmed = false;
			for ( $i = 1; $i <= $max; $i++ ) {
				$status = $this->poll_label_status( $id );
				$this->log( sprintf( 'Polling geração [tentativa %d/%d] item_id=%s status=%s', $i, $max, $id, $status ) );

				if ( 'released' === $status || 'posted' === $status || 'delivered' === $status ) {
					$confirmed = true;
					break;
				}

				if ( $i < $max ) {
					sleep( $wait );
				}
			}

			if ( ! $confirmed ) {
				throw new Exception( sprintf(
					'Etiqueta ainda não processada após %d tentativa(s). Tente novamente em instantes.',
					$max
				) );
			}

			$order->update_meta_data( '_melhor_envio_label_generated' . $suffix, true );
			$order->save();

			return $this->render_success( 'Etiqueta gerada com sucesso' );

		} elseif ( $response['response']['code'] === 401 ) {
			$this->log( 'Não autorizado. Confirme que seu token é realmente válido para o ambiente informado (Sandbox ou produção)!' );

			throw new Exception( 'Sem permissões na API. Entre em contato para obter assistência' );
		}

		$data = json_decode( $response['body'] );
		foreach ( $data as $error ) {
			$error = is_array( $error ) ? $error[ key( $error ) ] : $error;
			throw new Exception( 'Erro desconhecido ao gerar etiqueta. ' . print_r( $error, true ) );
		}
	}


	/**
	 * poll_label_status
	 * Consulta o status atual do item no carrinho do Melhor Envio.
	 * Retorna o status (string) ou 'unknown' em caso de erro.
	 *
	 * @param string $item_id
	 * @return string
	 */
	private function poll_label_status( $item_id ) {
		try {
			$response = $this->do_request( '/api/v2/me/cart/' . $item_id, 'GET' );

			if ( is_wp_error( $response ) ) {
				$this->log( 'Erro no polling: ' . $response->get_error_message() );
				return 'unknown';
			}

			if ( in_array( $response['response']['code'], [ 200, 201 ] ) ) {
				$body = json_decode( $response['body'] );
				return isset( $body->status ) ? $body->status : 'unknown';
			}
		} catch ( \Throwable $e ) {
			$this->log( 'Exceção no polling: ' . $e->getMessage() );
		}

		return 'unknown';
	}
}
