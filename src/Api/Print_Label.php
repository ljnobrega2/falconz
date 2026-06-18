<?php

namespace WC_MelhorEnvio\Api;

use Exception;

class Print_Label extends Melhor_Envio_Api {
	protected $logger = 'wc-melhor-envio-cart';
	private $endpoint = '/api/v2/me/shipment/print';

	function __construct() {
		parent::__construct();
	}

	public function print( $order ) {
		$extra_params = $this->get_additional_params();
		$dynamic_data = isset( $extra_params['dynamic_data'] ) ? $extra_params['dynamic_data'] : [];

		$is_reverse = isset( $dynamic_data['reverse'] ) && $dynamic_data['reverse'];
		$suffix     = $is_reverse ? '_reverse' : '';

		$id = $order->get_meta( '_melhor_envio_item_id' . $suffix );

		if ( ! $id ) {
			throw new Exception( 'Adicione o pedido ao carrinho antes de comprar.' );
		}

		// Usa cache se já tiver URL salva
		if ( $url = $order->get_meta( '_melhor_envio_print_url' . $suffix ) ) {
			return $this->render_success( 'Link da etiqueta gerado com sucesso', $url );
		}

		$data = [
			'mode'   => 'public',
			'orders' => [ $id ],
		];

		$response = $this->do_request( $this->endpoint, 'POST', $data );

		if ( is_wp_error( $response ) ) {
			$this->log( 'Erro ao processar solicitação: ' . $response->get_error_message() );
			throw new Exception( 'Erro ao processar solicitação: ' . $response->get_error_message() );

		} elseif ( in_array( $response['response']['code'], [ 200, 201 ] ) ) {
			$this->log( 'Link gerado com sucesso: ' . $response['body'] );

			$body = json_decode( $response['body'] );

			$order->update_meta_data( '_melhor_envio_print_url' . $suffix, $body->url );
			$order->save();

			return $this->render_success( 'Link da etiqueta gerado com sucesso', $body->url );

		} elseif ( $response['response']['code'] === 401 ) {
			$this->log( 'Não autorizado.' );
			throw new Exception( 'Sem permissões na API. Entre em contato para obter assistência.' );

		} elseif ( $response['response']['code'] === 404 ) {
			$this->log( 'Rota de impressão não encontrada' );
			throw new Exception( 'Recurso de impressão não encontrado.' );
		}

		$this->log( 'Resposta inválida: ' . $response['response']['code'] . ': ' . print_r( $response['body'], true ) );

		$body = json_decode( $response['body'] );
		foreach ( $body as $error ) {
			$error = is_array( $error ) ? $error[ key( $error ) ] : $error;
			throw new Exception( 'Erro desconhecido ao imprimir etiqueta. ' . print_r( $error, true ) );
		}
	}
}
