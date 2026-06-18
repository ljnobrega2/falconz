<?php

namespace WC_MelhorEnvio\Api;

use Exception;
use WC_Order;

class Cart extends Melhor_Envio_Api {
	public $service;
	public $from_address;
	/**
	 * $logger
	 *
	 * @var string
	 */
	protected $logger = 'wc-melhor-envio-cart';
	/**
	 * $order
	 *
	 * @var WC_Order
	 */
	private $order;
	/**
	 * $endpoint
	 *
	 * @var string
	 */
	private $endpoint = '/api/v2/me/cart';

	public function get_shipping_status( $me_order ) {
		return $this->do_request( $this->endpoint . '/' . $me_order, 'GET' );
	}

	/**
	 * add_to_cart
	 *
	 * @param  WC_Order  $order
	 *
	 * @return array
	 * @throws Exception
	 */
	public function add_to_cart( $order ): array {
		$this->set_order( $order );

		if ( ! $this->get_order() ) {
			throw new Exception( 'Pedido inválido' );
		}

		$extra_params = $this->get_additional_params();
		$dynamic_data = $extra_params['dynamic_data'] ?? [];

		$is_reverse = isset( $dynamic_data['reverse'] ) && $dynamic_data['reverse'];
		$suffix     = $is_reverse ? '_reverse' : '';

		$cart_id  = $this->get_order()->get_meta( '_melhor_envio_item_id' . $suffix );
		$protocol = $this->get_order()->get_meta( '_melhor_envio_order_id' . $suffix );

		if ( $is_reverse ) {
			$this->log( '===== LOGÍSTICA REVERSA =====' );
		}

		if ( $cart_id && $protocol ) {
			return $this->render_success( 'Pedido já adicionado ao carrinho', [
				'id'       => $cart_id,
				'protocol' => $protocol,
			] );
		}

		$this->service = ! empty( $dynamic_data['service'] ) ? $dynamic_data['service'] : $this->get_service();

		if ( ! $this->service ) {
			throw new Exception( 'Serviço não selecionado.' );
		}

		if ( $is_reverse ) {
			$destination        = $this->get_from_address(); // customer address
			$this->from_address = $this->get_to_address();
		} else {
			$this->from_address = $this->get_from_address();
			$destination        = $this->get_to_address();
		}

		if ( true !== ( $is_valid = $this->is_valid_address( $this->from_address ) ) ) {
			throw new Exception( 'Remetente inválido: ' . $is_valid );
		}

		if ( true !== ( $is_valid = $this->is_valid_address( $destination ) ) ) {
			throw new Exception( 'Destinatário inválido: ' . $is_valid );
		}

		$shipping = $this->get_shipping_costs( $this->service );


		if ( is_string( $shipping ) ) {
			$this->log( 'Falha ao processar os custos deste envio no pedido #' . $this->get_order()->get_id() . ': ' . $shipping );
			throw new Exception( 'Falha ao processar os custos deste envio no pedido #' . $this->get_order()->get_id() . ': ' . $shipping );
		}

		$this->service = $shipping->id;  // caso o ID tenha sido modificado, pegamos esse atualizado

		$data = [
			'service'  => $this->service,
			'from'     => $this->from_address,
			'to'       => $destination,
			'products' => $this->get_products(),
			'volumes'  => $this->get_volumns( $shipping ), // no caso de correios, é permitido apenas 1 pacote!
			'options'  => $this->get_options( $this->service ),
		];

		if ( in_array( intval( $shipping->company->id ), [ 2, 6 ] ) ) {
			$data['agency'] = $this->get_agency( $shipping->company->id );
		}

		$response = $this->do_request( $this->endpoint, 'POST', $data );

		if ( is_wp_error( $response ) ) {
			$this->log( 'Erro ao processar solicitação: ' . $response->get_error_message() );

			throw new Exception( 'Erro ao processar solicitação: ' . $response->get_error_message() );

		} elseif ( in_array( $response['response']['code'], [ 200, 201 ] ) ) {
			$data = json_decode( $response['body'] );

			$this->log( 'Item adicionado ao carrinho: ' . print_r( $data, true ) );

			$this->get_order()->add_meta_data( '_melhor_envio_requested_by' . $suffix, get_current_user_id() );
			$this->get_order()->add_meta_data( '_melhor_envio_item_id' . $suffix, $data->id );
			$this->get_order()->add_meta_data( '_melhor_envio_order_id' . $suffix, $data->protocol );
			$this->get_order()->save();

			return $this->render_success( 'Pedido adicionado ao carrinho', [
				'id'       => $data->id,
				'protocol' => $data->protocol,
			] );

		} elseif ( $response['response']['code'] === 401 ) {
			$this->log( 'Não autorizado. Confirme que seu token é realmente válido para o ambiente informado (Sandbox ou produção)!' );

			throw new Exception( 'Sem permissões na API. Entre em contato para obter assistência' );
		}

		$this->log( 'Erro geral: ' . $response['response']['code'] . ': ' . print_r( $response['body'], true ) );

		$result = json_decode( $response['body'], true );

		if ( isset( $result['errors'] ) || isset( $result['error'] ) ) {
			$message = "Erro ao adicionar ao carrinho";

			$errors = isset( $result['errors'] ) ? $result['errors'] : $result['error'];

			if ( is_array( $errors ) ) {
				foreach ( $errors as $group => $errors_list ) {
					// handling weired errors response
					foreach ( $errors_list as $k => $v ) {
						if ( is_array( $v ) ) {
							$errors_list[ $k ] = implode( '; ', $v );
						}
					}

					$message .= "\r\n";
					$message .= ucfirst( $group ) . ': ' . implode( "\r\n", $errors_list );
				}
			} else {
				$message .= "\r\n";
				$message .= $errors;
			}

			throw new Exception( $message );
		}
	}

	public function get_order() {
		return $this->order;
	}

	public function set_order( $order ) {
		$this->add_additional_param( 'order', $order );

		$this->order = $order;
	}

	/**
	 * Return de method ID, if available
	 *
	 * @param  mixed  $order_id
	 * @param  mixed  $order
	 *
	 * @return void
	 */
	private function get_service() {
		$service = null;

		$custom_service = $this->get_order()->get_meta( '_order_me_custom_service_id' );

		if ( ! $custom_service ) {
			$service = $this->get_customer_choice_service_id( $this->get_order() );
		}

		$settings = $this->get_integration_settings();

		$service = $custom_service ? intval( $custom_service ) : $service;

		$service = apply_filters( 'melhor_envio_order_service', $service, $this->get_order(), $settings );

		if ( ! $service ) {
			$this->log( 'Nenhum método definido para o pedido #' . $this->get_order()->get_id() );
		}

		return $service;
	}


	/**
	 * get_from_address
	 *
	 * @return void
	 */
	public function get_from_address( $is_reverse = false ) {
		$address  = [];
		$settings = $this->get_integration_settings();

		if ( isset( $settings['address'] ) && is_array( $settings['address'] ) ) {
			$address  = $settings['address'];
			$document = $this->only_numbers( $address['document'] );

			$address = [
				'name'             => $address['name'],
				'phone'            => $this->only_numbers( $address['phone'] ),
				'email'            => $address['email'],
				'document'         => $document,
				'address'          => $address['address'],
				'complement'       => $address['complement'],
				'number'           => $address['number'],
				'district'         => $address['district'],
				'city'             => $address['city'],
				'country_id'       => 'BR',
				'postal_code'      => $this->only_numbers( $address['postal_code'] ),
				'note'             => '',
				'company_document' => '',
				'state_register'   => '',
			];

				// CNAE/economic_activity_code: a API do Melhor Envio aceita no máximo 7 caracteres.
				// Se o campo vier contaminado com endereço completo, não enviamos para evitar erro 422.
				if ( ! empty( $settings['cnae'] ) ) {
					$cnae = $this->only_numbers( $settings['cnae'] );

					if ( strlen( $cnae ) > 0 && strlen( $cnae ) <= 7 ) {
						$address['economic_activity_code'] = $cnae;
					}
				}

			if ( strlen( $document ) > 11 ) {
				unset( $address['document'] );
				$address['company_document'] = $document;
			}
		}

		return apply_filters( 'melhor_envio_from_address', $address, $this->get_order() );
	}

	/**
	 * get_to_address
	 *
	 * @return void
	 */
	public function get_to_address() {
		$person_type = intval( $this->get_order()->get_meta( '_billing_persontype' ) );

		$document = null;
		if ( 1 === $person_type ) {
			$document = $this->get_order()->get_meta( '_billing_cpf' );
		} elseif ( 2 === $person_type ) {
			$document = $this->get_order()->get_meta( '_billing_cnpj' );
		} else {
			$document = $this->get_order()->get_meta( '_billing_cpf' );

			if ( empty ( $document ) ) {
				$document = $this->get_order()->get_meta( '_billing_cnpj' );
			}
		}

		$document = $this->only_numbers( $document );

		if ( $this->get_order()->get_shipping_address_1() ) {
			$address = [
				'name'             => trim( $this->get_order()->get_formatted_shipping_full_name() ) ? $this->get_order()->get_formatted_shipping_full_name() : $this->get_order()->get_formatted_billing_full_name(),
				'phone'            => $this->only_numbers( $this->get_customer_phone( $this->get_order() ) ),
				'email'            => $this->get_order()->get_billing_email(),
				'document'         => $document,
				'address'          => $this->get_order()->get_shipping_address_1(),
				'complement'       => $this->get_order()->get_shipping_address_2(),
				'number'           => $this->get_order()->get_meta( '_shipping_number' ),
				'district'         => $this->get_order()->get_meta( '_shipping_neighborhood' ),
				'city'             => $this->get_order()->get_shipping_city(),
				'state_abbr'       => $this->get_order()->get_shipping_state(),
				'country_id'       => 'BR',
				'postal_code'      => $this->only_numbers( $this->get_order()->get_shipping_postcode() ),
				'note'             => '',
				'company_document' => '',
				'state_register'   => '',
			];
		} else {
			$address = [
				'name'             => $this->get_order()->get_formatted_billing_full_name(),
				'phone'            => $this->only_numbers( $this->get_customer_phone( $this->get_order() ) ),
				'email'            => $this->get_order()->get_billing_email(),
				'document'         => $document,
				'address'          => $this->get_order()->get_billing_address_1(),
				'complement'       => $this->get_order()->get_billing_address_2(),
				'number'           => $this->get_order()->get_meta( '_billing_number' ),
				'district'         => $this->get_order()->get_meta( '_billing_neighborhood' ),
				'city'             => $this->get_order()->get_billing_city(),
				'state_abbr'       => $this->get_order()->get_billing_state(),
				'country_id'       => 'BR',
				'postal_code'      => $this->only_numbers( $this->get_order()->get_billing_postcode() ),
				'note'             => '',
				'company_document' => '',
				'state_register'   => '',
			];
		}

		if ( strlen( $document ) > 11 ) {
			unset( $address['document'] );
			$address['company_document'] = $document;
		}

		return $address;
	}

	/**
	 * get_shipping_costs
	 * Devemos calcular os custos de envio para
	 * conseguir recuperar o tamanho adequado do volume
	 *
	 * @param  mixed  $service
	 *
	 * @return void
	 */
	public function get_shipping_costs( $service = null ) {
		$calculator = new Calculator();

		if ( ! $service && ! $this->service ) {
			throw new Exception( 'Serviço inválido' );
		}

		$settings = $this->get_integration_settings();

		if ( ! $service && $this->service ) {
			$service = $this->service;
		}

		if ( ! $this->from_address ) {
			throw new Exception( 'Remetente inválido' );
		}

		$extra_params = $this->get_additional_params();
		$dynamic_data = $extra_params['dynamic_data'] ?? [];

		$is_reverse = isset( $dynamic_data['reverse'] ) && $dynamic_data['reverse'];
		$suffix     = $is_reverse ? '_reverse' : '';

		$calculator->add_additional_param( 'order', $this->get_order() );

		$calculator->set_origin_postcode( $this->from_address['postal_code'] );

		$postcode = $this->get_order()->get_shipping_postcode() ? $this->get_order()->get_shipping_postcode() : $this->get_order()->get_billing_postcode();
		if ( $is_reverse ) {
			$shop_address = $this->get_from_address();
			$postcode     = $shop_address['postal_code'];

			$this->log( 'LOGÍSTICA REVERSA' );
		}

		$this->log( 'CEP destino no pedido ' . $this->get_order()->get_id() . ': ' . $postcode );


		$declared_value = ! isset( $settings['declared_value'] )
		                  || 'yes' === $settings['declared_value']
		                  || 'no' === $settings['declared_value'] && ! in_array( $service,
				$this->get_no_insurance_methods() );

		$products = $this->get_products_for_quote( $declared_value );

		$calculator->set_destination_postcode( $postcode );
		$calculator->set_products( $products );
		$calculator->set_logger( 'wc-melhor-envio-order-quote' );
		$calculator->set_insurance_value( $declared_value ? 1 : 0 );
		$response = $calculator->calculate( $service );

		if ( is_wp_error( $response ) ) {
			$this->log( 'Erro ao processar solicitação: ' . $response->get_error_message() );

			throw new Exception( $response->get_error_message() );

		} elseif ( $response['response']['code'] === 200 ) {
			$methods = json_decode( $response['body'] );

			// random response... just one method not in the right format
			if ( isset( $methods->name ) ) {
				$methods = [ $methods ];
			}

			$summary_methods = [];
			foreach ( (array) $methods as $m ) {
				$summary_methods[] = [
					'id' => $m->id ?? '',
					'name' => $m->name ?? '',
					'company' => $m->company->name ?? '',
					'price' => $m->custom_price ?? $m->price ?? null,
					'error' => $m->error ?? '',
				];
			}
			$this->log( 'Cálculo realizado com sucesso. Resumo: ' . wp_json_encode( $summary_methods, JSON_UNESCAPED_UNICODE ) );

			// como aqui é só para testes, não precisa. exceto se eu quiser exibir as opções com preço no pedido!
			// if ( 'yes' !== $this->integration['declared_value'] ) {
			//   $this->log( 'Iniciando cotação dos Correios, sem valor declarado' );
			//   $products  = $this->get_products_for_quote( false );
			//   $calculator->set_insurance_value( 0 );
			//   $calculator->set_products( $products );

			//   $response = $calculator->calculate( $this->get_no_insurance_methods() );

			//   if ( is_wp_error( $response ) ) {
			//     $this->log( 'Erro: ' . $response->get_error_message() );
			//   } else {
			//     $correios_methods = json_decode( $response['body'] );
			//     $this->log( 'sucesso: ' . print_r( $correios_methods, true ) );
			//   }

			//   $methods = $this->combine_methods( $correios_methods, $methods );
			// }

			foreach ( $methods as $method ) {
				if ( intval( $service ) === intval( $method->id ) ) {
					if ( isset( $method->error ) && ! empty( $method->error ) ) {
						throw new Exception( 'Erro no método escolhido: ' . print_r( $method->error, true ) );
					}

					return $method;
				}
			}

			// o cálculo foi feito corretamente, mas o método não está disponível entre o CEP de origem e destino
			return apply_filters( 'melhor_envio_invalid_method_error', 'Método indisponível entre estes CEPs', $methods,
				$service, $this->integration, $this->get_order() );

		} elseif ( $response['response']['code'] === 401 ) {
			$this->log( 'Não autorizado. Confirme que seu token é realmente válido para o ambiente informado (Sandbox ou produção)!' );

			throw new Exception( "O token expirou. Gere um novo.", 1 );
		}

		$result = json_decode( $response['body'], true );

		if ( isset( $result['errors'] ) ) {
			$message = "\r\n";
			$message = "";

			$errors = $result['errors'];

			foreach ( $errors as $group => $errors_list ) {
				$message .= "\r\n";
				$message .= ucfirst( $group ) . ': ' . implode( "; ", $errors_list );
			}

			throw new Exception( $message );
		}

		$this->log( 'Erro geral: ' . $response['response']['code'] . ': ' . print_r( $response['body'], true ) );

		throw new Exception( 'Ocorreu um erro na cotação do frete. Tente novamente', 1 );

	}

	/**
	 * get_products_for_quote
	 *
	 * @return array
	 */
	public function get_products_for_quote( $declared_value = true ) {
		$products = [];

		foreach ( $this->get_order()->get_items() as $item ) {
			$product = $item->get_product();

			if ( ! $product || ! $product->needs_shipping() ) {
				continue;
			}

			$me_product = [
				'id'              => $product ? $product->get_id() : '',
				'quantity'        => $item->get_quantity(),
				'insurance'       => 1,
				'insurance_value' => 1,
				'height'          => $product->get_height() ? $product->get_height() : 15,
				'width'           => $product->get_width() ? $product->get_width() : 15,
				'length'          => $product->get_length() ? $product->get_length() : 15,
				'weight'          => wc_format_decimal( wc_get_weight( $product->get_weight() ? $product->get_weight() : 1.0,
					'kg' ), 2 ),
			];

			if ( ! $declared_value ) {
				unset( $me_product['insurance'] );
				unset( $me_product['insurance_value'] );
			}

			$products[] = $me_product;
		}

		return $products;
	}


	/**
	 * get_products
	 *
	 * @return void
	 */
	public function get_products() {
		$products = [];

		foreach ( $this->get_order()->get_items() as $item ) {
			$product_object = is_callable( array( $item, 'get_product' ) ) ? $item->get_product() : null;

			if ( $product_object && ! $product_object->needs_shipping() ) {
				continue;
			}

			$products[] = [
				'name'          => $item->get_name(),
				'quantity'      => $item->get_quantity(),
				'unitary_value' => wc_format_decimal( $item->get_total() / $item->get_quantity(), 2, false ),
			];
		}

		return $products;
	}


	/**
	 * get_volumns
	 *
	 * @param  mixed  $shipping
	 *
	 * @return void
	 */
	public function get_volumns( $shipping ) {
		$volumns = [];

		foreach ( $shipping->packages as $package ) {
			$volumns[] = [
				'height' => $package->dimensions->height,
				'width'  => $package->dimensions->width,
				'length' => $package->dimensions->length,
				'weight' => $package->weight,
			];
		}

		return $volumns;
	}


	/**
	 * get_options
	 *
	 * @return void
	 */
	public function get_options( $service ) {
		$invoice = $this->get_invoice( $this->get_order() );

		$data = [
			'insurance_value' => 1,
			'receipt'         => false,
			'own_hand'        => false,
			'reverse'         => false,
			'non_commercial'  => empty( $invoice ),
			'invoice'         => $invoice,
			'platform'        => get_bloginfo( 'name' ),
			'tags'            =>
				[
					[
						'tag' => (string) $this->get_order()->get_id(),
						'url' => $this->get_order()->get_edit_order_url(),
					],
				],
		];

		if ( 'yes' !== $this->integration['declared_value'] && in_array( $service,
				$this->get_no_insurance_methods() ) ) {
			$data['insurance_value'] = 0.00;
		}

		return $data;
	}


	/**
	 * get_invoice
	 * Obrigatório para pedidos non_commercial = false
	 *
	 * @return void
	 */
	public function get_invoice( $order = null ) {
		if ( $order && $invoice_id = $order->get_meta( '_order_invoice_id' ) ) {
			return [
				'key' => $invoice_id,
			];
		}

		return [];
	}

	/**
	 * get_from_address
	 *
	 * @return void
	 */
	public function get_agency( $company_id ) {
		$valid_ids = [
			2 => 'jadlog',
			6 => 'latam',
		];

		if ( ! isset( $valid_ids[ $company_id ] ) ) {
			return false;
		}

		$agency = isset( $this->integration[ $valid_ids[ $company_id ] . '_agency' ] ) ? $this->integration[ $valid_ids[ $company_id ] . '_agency' ] : '';

		return apply_filters( 'melhor_envio_agency', $agency, $this->get_order(), $company_id );
	}
}
