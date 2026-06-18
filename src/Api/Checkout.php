<?php

namespace WC_MelhorEnvio\Api;

use Exception;

class Checkout extends Melhor_Envio_Api {
	/**
	 * $logger
	 *
	 * @var string
	 */
	protected $logger = 'wc-melhor-envio-checkout';
	/**
	 * $endpoint
	 *
	 * @var string
	 */
	private $endpoint = '/api/v2/me/shipment/checkout';
	/**
	 * Cart item data
	 *
	 * @var array
	 */
	private $data = [];

	/**
	 * __construct
	 *
	 * @return void
	 */
	function __construct() {
		parent::__construct();
	}


	/**
	 * checkout
	 *
	 * @return array
	 * @throws Exception
	 */
	public function buy( $order ) {
		$extra_params = $this->get_additional_params();
		$dynamic_data = isset( $extra_params['dynamic_data'] ) ? $extra_params['dynamic_data'] : [];

		$extra_params = $this->get_additional_params();
		$dynamic_data = isset( $extra_params['dynamic_data'] ) ? $extra_params['dynamic_data'] : [];
		$is_reverse   = isset( $dynamic_data['reverse'] ) && $dynamic_data['reverse'];
		$suffix       = $is_reverse ? '_reverse' : '';


		$id = $order->get_meta( '_melhor_envio_item_id' . $suffix );

		if ( ! $id ) {
			throw new Exception( 'Adicione o pedido ao carrinho antes de comprar.' );
		}

		// não comprar mais de uma vez
		if ( $order->get_meta( '_melhor_envio_bought_shipping' . $suffix ) ) {
			return $this->render_success( 'Etiqueta comprada com sucesso' );
		}

		$data = [
			'orders' => [ $id ],
		];

		$response = $this->do_request( $this->endpoint, 'POST', $data );

		if ( is_wp_error( $response ) ) {
			$this->log( 'Erro ao processar solicitação: ' . $response->get_error_message() );

			throw new Exception( 'Erro ao processar solicitação: ' . $response->get_error_message() );

		} elseif ( in_array( $response['response']['code'], [ 200, 201 ] ) ) {
			$this->log( 'Etiqueta comprada com sucesso: ' . $response['body'] );

			$data = json_decode( $response['body'] );

			$order->update_meta_data( '_melhor_envio_bought_shipping' . $suffix, $id );
			$order->save();

			return $this->render_success( 'Etiqueta comprada com sucesso' );

		} elseif ( $response['response']['code'] === 401 ) {
			$this->log( 'Não autorizado. Confirme que seu token é realmente válido para o ambiente informado (Sandbox ou produção)!' );

			throw new Exception( 'Sem permissões na API. Entre em contato para obter assistência' );
		}

		$this->log( 'Erro geral: ' . $response['response']['code'] . ': ' . print_r( $response['body'], true ) );

		$data = json_decode( $response['body'] );
		foreach ( $data as $error ) {
			$error = is_array( $error ) ? $error[ key( $error ) ] : $error;
			throw new Exception( 'Erro desconhecido ao comprar etiqueta. ' . print_r( $error, true ) );
		}
	}
}
