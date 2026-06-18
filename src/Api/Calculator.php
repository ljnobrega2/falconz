<?php

namespace WC_MelhorEnvio\Api;

class Calculator extends Melhor_Envio_Api {
	/**
	 * $products
	 *
	 * @var array
	 */
	private $products = [];

	/**
	 * $origin_postcode
	 *
	 * @var string
	 */
	private $origin_postcode = '';

	/**
	 * $destination_postcode
	 *
	 * @var string
	 */
	private $destination_postcode = '';

	/**
	 * $insurance_value
	 *
	 * @var float
	 */
	private $insurance_value = 0;


	/**
	 * $endpoint
	 *
	 * @var string
	 */
	private $endpoint = '/api/v2/me/shipment/calculate';


	/**
	 * __construct
	 *
	 * @return void
	 */
	function __construct() {
		parent::__construct();
	}


	/**
	 * set_products
	 *
	 * @param  mixed  $products
	 *
	 * @return void
	 */
	public function set_products( $products ) {
		$this->products = $products;
	}


	/**
	 * set_origin_postcode
	 *
	 * @param  mixed  $postcode
	 *
	 * @return void
	 */
	public function set_origin_postcode( $postcode ) {
		$this->origin_postcode = $postcode;
	}


	/**
	 * set_destination_postcode
	 *
	 * @param  mixed  $postcode
	 *
	 * @return void
	 */
	public function set_destination_postcode( $postcode ) {
		$this->destination_postcode = $postcode;
	}


	/**
	 * set_insurance_value
	 *
	 * @param  mixed  $postcode
	 *
	 * @return void
	 */
	public function set_insurance_value( $value ) {
		$this->insurance_value = 1;
	}


	public function calculate( $services = null ) {
		$this->log( 'CEP de origem: ' . $this->origin_postcode );

		$data = [
			'from'     => [
				'postal_code' => $this->origin_postcode,
			],
			'to'       => [
				'postal_code' => $this->destination_postcode,
			],
			'products' => $this->products,
			'options'  => [
				'insurance_value' => wc_format_decimal( $this->insurance_value, 2 ),
			],
		];

		if ( wc_format_decimal( 0, 2 ) === $data['options']['insurance_value'] ) {
			unset( $data['options']['insurance_value'] );
		}

		if ( $services ) {
			$data['services'] = is_array( $services ) ? implode( ',', $services ) : $services;
		}

		$data = apply_filters( 'wc_melhor_envio_quote_params', $data, $this );

		$response = $this->do_request( $this->endpoint, 'POST', $data );

		return $response;
	}


	public function get_formatted_log( $body ) {
		$result = [];
		$body   = json_decode( $body );

		foreach ( $body as $item ) {
			$result[] = [
				'id'            => isset( $item->id ) ? $item->id : '',
				'name'          => isset( $item->name ) ? $item->name : '',
				'delivery_time' => isset( $item->delivery_time ) ? $item->delivery_time : '',
				'price'         => isset( $item->price ) ? $item->price : '',
				'custom_price'  => isset( $item->custom_price ) ? $item->custom_price : '',
				'discount'      => isset( $item->discount ) ? $item->discount : '',
			];
		}

		return print_r( $result, true );
	}
}
