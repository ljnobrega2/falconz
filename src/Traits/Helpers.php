<?php

namespace WC_MelhorEnvio\Traits;

trait Helpers {
	/**
	 * only_numbers
	 *
	 * @param  mixed  $value
	 *
	 * @return string
	 */
	public function only_numbers( $value ) {
		return preg_replace( '/[^0-9]/', '', $value );
	}


	/**
	 * is_valid_address
	 *
	 * @param  mixed  $address
	 *
	 * @return void
	 */
	public function is_valid_address( $address ) {
		if ( ! is_array( $address ) ) {
			return 'Endereço no formato inválido';
		}

		$required_fields = [
			'name',
			'address',
			'city',
			'postal_code',
		];

		foreach ( $required_fields as $field ) {
			if ( empty( $address[ $field ] ) ) {
				return 'Dados faltantes: ' . $field;
			}
		}

		return true;
	}


	/**
	 * get_customer_phone
	 *
	 * @param  mixed  $order
	 *
	 * @return string
	 */
	public function get_customer_phone( $class ) {
		if ( is_a( $class, 'WC_Order' ) ) {
			return '' !== $class->get_meta( '_billing_cellphone' ) ? $class->get_meta( '_billing_cellphone' ) : $class->get_billing_phone();
		} elseif ( is_a( $class, 'WC_Customer' ) ) {
			return '' !== $class->get_meta( 'billing_cellphone' ) ? $class->get_meta( 'billing_cellphone' ) : $class->get_billing_phone();
		}

		return null;
	}

	/**
	 * get_agencies_list
	 *
	 * @return array
	 */
	public function get_agencies_list() {
		return $this->get_agencies( 'jadlog' );
	}

	public function get_agencies( $company ) {
		$upload_dir = wp_get_upload_dir();
		$file       = $upload_dir['basedir'] . '/melhor-envio-' . $company . '-agencies.json';

		if ( ! file_exists( $file ) ) {
			return [];
		}

		$data = json_decode( file_get_contents( $file ), true );

		$agencies = [];

		foreach ( $data as $id => $info ) {
			// print_r($info);exit;
			$agencies[ $id ] = sprintf(
				'#%s - %s | %s %s, %s - %s',
				$id,
				$info['name'],
				$info['address']['address'],
				$info['address']['number'],
				$info['address']['city']['city'],
				$info['address']['city']['state']['state_abbr'],
			);
		}

		return $agencies;
	}

	/**
	 * get_agencies_list
	 *
	 * @return array
	 */
	public function get_latam_agencies_list() {
		return $this->get_agencies( 'latam' );
	}

	public function can_request_label( $order ) {
		$me_id = $this->get_customer_choice_service_id( $order );

		// Em contexto de cron/background não há usuário logado — permitir execução
		if ( defined( 'DOING_CRON' ) && DOING_CRON ) {
			return apply_filters( 'wc_melhor_envio_can_request_label', true, $order, $me_id );
		}

		if ( defined( 'DOING_AJAX' ) && DOING_AJAX && ! is_user_logged_in() ) {
			return apply_filters( 'wc_melhor_envio_can_request_label', false, $order, $me_id );
		}

		$can_request = is_user_logged_in() && current_user_can( 'manage_woocommerce' );

		return apply_filters( 'wc_melhor_envio_can_request_label', $can_request, $order, $me_id );
	}

	public function get_customer_choice_service_id( $order ) {
		// por padrão, não é Melhor Envio
		$me_id = false;

		foreach ( $order->get_items( 'shipping' ) as $method ) {
			// se é ME, pode
			if ( $me_code = $method->get_meta( 'melhorenvio_method_id' ) ) {
				$this->log( 'Método do Melhor Envio escolhido pelo cliente no pedido ' . $order->get_id() . ': ' . $me_code );

				$me_id = $me_code;
				break;
			}
		}

		return $me_id;
	}

	public function get_integration_settings() {
		$defaults = [
			'declared_value'     => 'yes',
			'status_after_print' => 'none',
		];

		$settings = get_option( 'woocommerce_wc-melhor-envio_settings', [] );

		return wp_parse_args( $settings, $defaults );
	}

	public function combine_methods( $new_methods, $default_methods ) {
		$final_methods = [];

		$this->log( 'COMBINANDO: ' . print_r( $new_methods, true ) );

		foreach ( $new_methods as $key => $method ) {
			if ( in_array( $method->id, $this->get_no_insurance_methods() ) ) {
				$final_methods[] = $method;
			}
		}

		foreach ( $default_methods as $key => $method ) {

			$this->log( 'validate new method DEFAULT:: ' . $method->id . ' == ' . wc_bool_to_string( in_array( $method->id,
					$this->get_no_insurance_methods() ) ) );

			if ( ! in_array( $method->id, $this->get_no_insurance_methods() ) ) {
				$final_methods[] = $method;
			}
		}

		return $final_methods;
	}

	public function get_no_insurance_methods() {
		return [ 1, 2, 17 ];
	}
}
