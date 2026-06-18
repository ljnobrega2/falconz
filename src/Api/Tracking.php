<?php

namespace WC_MelhorEnvio\Api;

use Exception;

class Tracking extends Melhor_Envio_Api {
	/**
	 * $logger
	 *
	 * @var string
	 */
	protected $logger = 'wc-melhor-envio-tracking';

	/**
	 * $endpoint
	 *
	 * @var string
	 */
	private $endpoint = '/api/v2/me/shipment/tracking';

	/**
	 * add_to_cart
	 *
	 * @return array
	 */
	public function tracking( $orders ) {
		if ( ! $orders ) {
			throw new Exception( 'Nenhum pedido informado' );
		}

		$data = [
			'orders' => $orders,
		];

		$response = $this->do_request( $this->endpoint, 'POST', $data );

		if ( is_wp_error( $response ) ) {
			$this->log( 'Erro ao processar solicitação: ' . $response->get_error_message() );

			throw new Exception( 'Erro ao processar solicitação: ' . $response->get_error_message() );

		} elseif ( in_array( $response['response']['code'], [ 200, 201 ] ) ) {
			$this->log( 'Consulta realizada com sucesso!' );
			$data = json_decode( $response['body'] );

			return $this->render_success( 'Rastreio realizado com sucesso', $data );

		} elseif ( $response['response']['code'] === 401 ) {
			$this->log( 'Não autorizado. Confirme que seu token é realmente válido para o ambiente informado (Sandbox ou produção)!' );

			throw new Exception( 'Sem permissões na API. Entre em contato para obter assistência' );
		}

		$this->log( 'Erro geral: ' . $response['response']['code'] . ': ' . print_r( $response['body'], true ) );

		throw new Exception( 'Erro desconhecido ao rastrear o envio.' );
	}
}
