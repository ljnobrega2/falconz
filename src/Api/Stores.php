<?php

namespace WC_MelhorEnvio\Api;

use Exception;

class Stores extends Melhor_Envio_Api {

	/**
	 * $logger
	 *
	 * @var string
	 */
	protected $logger = 'wc-melhor-envio-stores';

	/**
	 * $endpoint
	 *
	 * @var string
	 */
	private $endpoint = '/api/v2/me/companies';


	public function add_store( $data ) {
		$defaults = [
			'name'           => '',
			'email'          => '',
			'description'    => '',
			'company_name'   => '',
			'document'       => '',
			'state_register' => '',
		];

		$response = $this->do_request( $this->endpoint, 'POST', $data );

		if ( is_wp_error( $response ) ) {
			$this->log( 'Erro ao processar solicitação: ' . $response->get_error_message() );

			throw new Exception( 'Erro ao processar solicitação: ' . $response->get_error_message() );

		} elseif ( in_array( $response['response']['code'], [ 200, 201 ] ) ) {
			$this->log( 'Loja cadastrada: ' . print_r( $response['body'], true ) );

			$data = json_decode( $response['body'] );

			return $this->render_success( 'Loja cadastrada com sucesso', $data );

		} elseif ( $response['response']['code'] === 401 ) {
			$this->log( 'Não autorizado. Confirme que seu token é realmente válido para o ambiente informado (Sandbox ou produção)!' );

			throw new Exception( 'Sem permissões na API. Entre em contato para obter assistência' );
		}

		$this->log( 'Erro geral: ' . $response['response']['code'] . ': ' . print_r( $response['body'], true ) );

		$data = json_decode( $response['body'] );
		foreach ( $data as $error ) {
			$error = is_array( $error ) ? $error[ key( $error ) ] : $error;
			throw new Exception( 'Erro ao cadastrar loja.' . print_r( $error, true ) );
		}
	}


	public function get_store_address( $store_id ) {
		$response = $this->do_request( $this->endpoint . '/' . $store_id . '/addresses', 'GET', [] );

		if ( is_wp_error( $response ) ) {
			$this->log( 'Erro ao processar solicitação: ' . $response->get_error_message() );

			throw new Exception( 'Erro ao processar solicitação: ' . $response->get_error_message() );

		} elseif ( in_array( $response['response']['code'], [ 200, 201 ] ) ) {
			$this->log( 'Endereços retornados: ' . print_r( $response['body'], true ) );

			$data = json_decode( $response['body'] );

			return $this->render_success( 'Endereço exibido com sucesso', $data );

		} elseif ( $response['response']['code'] === 401 ) {
			$this->log( 'Não autorizado. Confirme que seu token é realmente válido para o ambiente informado (Sandbox ou produção)!' );

			throw new Exception( 'Sem permissões na API. Entre em contato para obter assistência' );
		}

		$this->log( 'Erro geral: ' . $response['response']['code'] . ': ' . print_r( $response['body'], true ) );

		$data = json_decode( $response['body'] );
		foreach ( $data as $error ) {
			$error = is_array( $error ) ? $error[ key( $error ) ] : $error;
			throw new Exception( 'Erro ao listar lojas. ' . print_r( $error, true ) );
		}
	}


	public function save_address( $store_id, $data ) {
		$defaults = [
			'postal_code' => '',
			'address'     => '',
			'number'      => '',
			'complement'  => '',
			'city'        => '',
			'state'       => '',
		];

		$data = wp_parse_args( $data, $defaults );

		$response = $this->do_request( $this->endpoint . '/' . $store_id . '/addresses', 'POST', $data );

		if ( is_wp_error( $response ) ) {
			$this->log( 'Erro ao processar solicitação: ' . $response->get_error_message() );

			throw new Exception( 'Erro ao processar solicitação: ' . $response->get_error_message() );

		} elseif ( in_array( $response['response']['code'], [ 200, 201 ] ) ) {
			$this->log( 'Endereço cadastrado: ' . print_r( $response['body'], true ) );

			$data = json_decode( $response['body'] );

			return $this->render_success( 'Endereço cadastrado com sucesso', $data );

		} elseif ( $response['response']['code'] === 401 ) {
			$this->log( 'Não autorizado. Confirme que seu token é realmente válido para o ambiente informado (Sandbox ou produção)!' );

			throw new Exception( 'Sem permissões na API. Entre em contato para obter assistência' );
		}

		$this->log( 'Erro geral: ' . $response['response']['code'] . ': ' . print_r( $response['body'], true ) );

		$data = json_decode( $response['body'] );
		foreach ( $data as $error ) {
			$error = is_array( $error ) ? $error[ key( $error ) ] : $error;
			throw new Exception( 'Erro ao cadastrar endereço. ' . print_r( $error, true ) );
		}
	}


	public function get_stores() {
		$response = $this->do_request( $this->endpoint, 'GET', [] );

		if ( is_wp_error( $response ) ) {
			$this->log( 'Erro ao processar solicitação: ' . $response->get_error_message() );

			throw new Exception( 'Erro ao processar solicitação: ' . $response->get_error_message() );

		} elseif ( in_array( $response['response']['code'], [ 200, 201 ] ) ) {
			$this->log( 'Lojas retornadas: ' . print_r( $response['body'], true ) );

			$data = json_decode( $response['body'] );

			return $this->render_success( 'Lojas exibidas com sucesso', $data );

		} elseif ( $response['response']['code'] === 401 ) {
			$this->log( 'Não autorizado. Confirme que seu token é realmente válido para o ambiente informado (Sandbox ou produção)!' );

			throw new Exception( 'Sem permissões na API. Entre em contato para obter assistência' );
		}

		$this->log( 'Erro geral: ' . $response['response']['code'] . ': ' . print_r( $response['body'], true ) );

		$data = json_decode( $response['body'] );
		foreach ( $data as $error ) {
			$error = is_array( $error ) ? $error[ key( $error ) ] : $error;
			throw new Exception( 'Erro ao listar lojas. ' . print_r( $error, true ) );
		}
	}
}
