<?php

namespace WC_MelhorEnvio\Api;

use WC_MelhorEnvio\Traits\Helpers;
use WC_MelhorEnvio\Traits\Logger;
use WP_Error;

abstract class Melhor_Envio_Api {
	use Logger, Helpers;

	/**
	 * $additional_params
	 *
	 * @var array
	 */
	public $additional_params = [
		'vendor' => 'marketplace',
	];
	/**
	 * $logger
	 *
	 * @var object
	 */
	protected $logger;
	/**
	 * $integration
	 *
	 * @var array
	 */
	protected $integration = [];

	function __construct() {
		$this->integration = $this->get_integration_settings();
	}

	/**
	 * logger
	 *
	 * @param  mixed  $products
	 *
	 * @return void
	 */
	public function set_logger( $logger ) {
		$this->logger = $logger;
	}

	/**
	 * add_additional_param
	 *
	 * @param  mixed  $key
	 * @param  mixed  $value
	 *
	 * @return void
	 */
	public function add_additional_param( $key, $value ) {
		$this->additional_params[ $key ] = $value;
	}

	public function render_error( $message, $data = [] ) {
		return [
			'success' => false,
			'message' => $message,
			'data'    => $data,
		];
	}

	public function render_success( $message, $data = [] ) {
		return [
			'success' => true,
			'message' => $message,
			'data'    => $data,
		];
	}

	/**
	 * Do requests in the Melhor Envio API.
	 *
	 * @param  string  $url  URL.
	 * @param  string  $method  Request method.
	 * @param  array  $data  Request data.
	 * @param  array  $headers  Request headers.
	 *
	 * @return object|WP_Error            Request response.
	 */
	protected function do_request( $endpoint, $method = 'POST', $data = [], $headers = [] ) {
		$url = $this->get_api_url() . $endpoint;
		$this->log( 'URL: ' . $url . ' (' . $method . ')' );

		$token = $this->integration['client_secret'];

		$extra_params = $this->get_additional_params();

		if ( isset( $extra_params['token'] ) ) {
			$this->log( 'Token personalizado do vendedor: ' . $extra_params['vendor'] );
			$token = $extra_params['token'];
		}

		$params = [
			'method'  => $method,
			'timeout' => 60,
			'headers' => [
				'Accept'        => 'application/json',
				'Content-Type'  => 'application/json',
				'Authorization' => 'Bearer ' . $token,
				'User-Agent'    => 'Senderzz Logistics (suporte@app.senderzz.com.br)',
			],
		];

		if ( 'POST' === $method && ! empty( $data ) ) {
			$params['body'] = wp_json_encode( $data );

			$body_summary = json_decode( $params['body'], true );
			if ( is_array( $body_summary ) ) {
				$body_summary = [
					'from' => $body_summary['from']['postal_code'] ?? '',
					'to' => $body_summary['to']['postal_code'] ?? '',
					'products' => is_array( $body_summary['products'] ?? null ) ? count( $body_summary['products'] ) : 0,
					'services' => $body_summary['services'] ?? '',
				];
			}
			$this->log( 'ARGS resumo: ' . wp_json_encode( $body_summary, JSON_UNESCAPED_UNICODE ) );
		}

		if ( ! empty( $headers ) ) {
			$params['headers'] = wp_parse_args( $headers, $params['headers'] );
		}

		if ( 'GET' === strtoupper( $method ) ) {
			if ( ! empty( $data ) ) {
				$url = add_query_arg( $data, $url );
			}
			return wp_safe_remote_get( $url, $params );
		}

		// DELETE e PUT precisam do método explícito via wp_remote_request
		if ( in_array( strtoupper( $method ), [ 'DELETE', 'PUT', 'PATCH' ], true ) ) {
			$params['method'] = strtoupper( $method );
			if ( ! empty( $data ) ) {
				$params['body'] = wp_json_encode( $data );
			}
			return wp_remote_request( $url, $params );
		}

		return wp_safe_remote_post( $url, $params );
	}

	/**
	 * Get the API URL.
	 *
	 * @return string.
	 */
	protected function get_api_url() {
		return 'https://' . $this->get_environment_prefix() . '.melhorenvio.com.br';
	}

	/**
	 * Get the API environment.
	 *
	 * @return string
	 */
	protected function get_environment_prefix() {
		return ( 'yes' == $this->integration['sandbox'] ) ? 'sandbox' : 'www';
	}

	/**
	 * get_additional_params
	 *
	 * @return array
	 */
	public function get_additional_params() {
		return apply_filters( 'melhor_envio_additional_params', $this->additional_params, $this );
	}

	/**
	 * set_additional_params
	 *
	 * @param  array  $params
	 *
	 * @return void
	 */
	public function set_additional_params( $params = [] ) {
		$this->additional_params = wp_parse_args( $params, $this->additional_params );
	}
}
