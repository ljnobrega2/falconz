<?php

namespace WC_MelhorEnvio\Admin;

class Tools {
	function __construct() {
		add_filter( 'woocommerce_debug_tools', [ $this, 'melhor_envio_tools' ], 20, 4 );
	}


	public function melhor_envio_tools( $tools ) {
		$tools['wc_melhor_envio_refresh_methods'] = array(
			'name'     => __( 'Atualizar lista de métodos', 'wc-melhor-envio' ),
			'button'   => __( 'Atualizar agora', 'wc-melhor-envio' ),
			'desc'     => __( 'A lista de dados métodos disponíveis é atualizada apenas sob demanda por questões de performance.',
				'wc-melhor-envio' ),
			'callback' => array( $this, 'refresh_available_methods_list' ),
		);

		$tools['wc_melhor_envio_refresh_agencies'] = array(
			'name'     => __( 'Atualizar lista de agências', 'wc-melhor-envio' ),
			'button'   => __( 'Atualizar agora', 'wc-melhor-envio' ),
			'desc'     => __( 'A lista de agências disponíveis é atualizada apenas sob demanda por questões de performance.',
				'wc-melhor-envio' ),
			'callback' => array( $this, 'refresh_agencies_list' ),
		);

		return $tools;
	}


	public function refresh_available_methods_list() {
		try {
			wc_melhor_envio_update_account_methods();

			return 'Métodos atualizados com sucesso.';

		} catch ( \Exception $e ) {
			return $e->getMessage();
		}
	}


	public function refresh_agencies_list() {
		$url      = 'https://www.melhorenvio.com.br';
		$endpoint = '/api/v2/me/shipment/agencies';

		$upload_dir         = wp_get_upload_dir();
		$destination_folder = $upload_dir['basedir'];

		$args = array(
			'timeout' => 60,
			'headers' => array(
				'Accept'       => 'application/json',
				'Content-Type' => 'application/json',
			),
			'method'  => 'GET',
		);

		$response = wp_remote_post( $url . $endpoint, $args );

		if ( is_wp_error( $response ) ) {
			return $response->get_error_message();
		}

		if ( 200 !== $response['response']['code'] ) {
			return 'Resposta inválida do Melhor Envio. Código retornado: ' . $response['response']['code'];
		}

		$result = json_decode( $response['body'] );

		$jadlog_agencies = [];
		$latam_agencies  = [];

		foreach ( $result as $agency ) {

			// available only
			if ( empty( $agency->companies ) ) {
				continue;
			}

			$is_jadlog = wp_list_filter( $agency->companies, [ 'id' => 2 ] );
			$is_latam  = wp_list_filter( $agency->companies, [ 'id' => 6 ] );

			if ( ! $is_jadlog && ! $is_latam ) {
				continue;
			}

			$data = [
				'name'         => $agency->name,
				'company_name' => $agency->company_name ?? $agency->name,
				'address'      => $agency->address,
			];

			if ( $is_jadlog ) {
				$jadlog_agencies[ $agency->id ] = $data;
			}

			if ( $is_latam ) {
				$latam_agencies[ $agency->id ] = $data;
			}
		}

		file_put_contents( $destination_folder . '/melhor-envio-jadlog-agencies.json',
			json_encode( $jadlog_agencies ) );
		file_put_contents( $destination_folder . '/melhor-envio-latam-agencies.json', json_encode( $latam_agencies ) );

		return 'Agências atualizadas com sucesso.';
	}
}
