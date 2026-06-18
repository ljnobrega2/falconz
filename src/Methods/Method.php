<?php

namespace WC_MelhorEnvio\Methods;

use WC_MelhorEnvio\Api\Calculator;
use WC_MelhorEnvio\Traits\Helpers;
use WC_MelhorEnvio\Traits\Logger;
use WC_Shipping_Method;


if ( ! class_exists( 'WC_Melhor_Envio_Method' ) ) {
	class Method extends WC_Shipping_Method {
		use Logger, Helpers;

		public $global_settings;

		public $declared_value;

		public $additional_time;

		public $extra_weight;

		public $logger;

		/**
		 * Constructor for your shipping class
		 *
		 * @access public
		 * @return void
		 */
		public function __construct( $instance_id = 0 ) {
			$this->global_settings = $this->get_integration_settings();

			$this->instance_id        = absint( $instance_id );
			$this->id                 = 'melhor_envio';
			$this->title              = 'Melhor Envio';
			$this->method_title       = 'Melhor Envio';
			$this->method_description = 'Adicione Melhor Envio ao site.'; //
			$this->enabled            = $this->get_option( 'enabled' );
			$this->supports           = array( 'shipping-zones', 'instance-settings' );
			$this->init();

			/**
			 * $logger
			 *
			 * @var string
			 */
			$this->logger = 'wc-melhor-envio-calculator';
		}

		/**
		 * Init your settings
		 *
		 * @access public
		 * @return void
		 */
		function init() {
			// Load the settings API
			$this->init_form_fields(); // This is part of the settings API. Override the method to add your own settings
			$this->init_settings(); // This is part of the settings API. Loads settings you previously init.

			// Save settings in admin if you have any defined
			add_action( 'woocommerce_update_options_shipping_' . $this->id, array( $this, 'process_admin_options' ) );
		}

		public function init_form_fields() {
			$this->instance_form_fields = array(
				'enabled'         => array(
					'title'       => 'Ativar Melhor Envio',
					'type'        => 'checkbox',
					'description' => 'Habilitar esta forma de entrega ao cliente.',
					'default'     => 'yes',
					'desc_tip'    => true,
				),
				'postcode'        => array(
					'title'       => 'CEP de origem',
					'type'        => 'text',
					'description' => 'Informe o CEP de origem',
					'default'     => $this->get_base_postcode(),
					'desc_tip'    => true,
				),
				'additional_time' => array(
					'title'       => 'Dias adicionais',
					'type'        => 'number',
					'description' => 'Dias para somar ao prazo padrão',
					'default'     => 0,
					'desc_tip'    => true,
				),
				'extra_weight'    => array(
					'title'       => 'Peso extra',
					'type'        => 'text',
					'description' => 'Peso extra em kg',
					'default'     => 0,
					'desc_tip'    => true,
				),
				'fee'             => array(
					'title'       => 'Taxa de manuseio',
					'type'        => 'text',
					'description' => 'Taxa extra somada ao valor do envio. Pode ser porcentagem ou valor fixo.',
					'placeholder' => '0.00',
					'desc_tip'    => true,
				),
				'cart_fee'        => array(
					'title'       => 'Taxa do carrinho',
					'type'        => 'text',
					'description' => 'Taxa extra somada ao valor do envio com base no total dos produtos. Pode ser porcentagem ou valor fixo.',
					'placeholder' => '0.00',
					'desc_tip'    => true,
				),
				'labels'          => array(
					'title'             => 'Nomes personalizados',
					'type'              => 'melhor_envio_labels',
					'description'       => 'Altere o nome padrão exibido nos métodos',
					'desc_tip'          => true,
					'sanitize_callback' => false,
				),
			);

			$this->declared_value  = $this->global_settings['declared_value'] === 'yes'; // since 08-17-2022 its mandatory
			$this->additional_time = $this->get_option( 'additional_time' );
			$this->extra_weight    = $this->get_option( 'extra_weight', 0 );
		}

		/**
		 * calculate_shipping function.
		 *
		 * @access public
		 *
		 * @param  mixed  $package
		 *
		 * @return void
		 */
		public function calculate_shipping( $package = array() ) {
			// check if customer postcode is set
			if ( empty( $package['destination']['postcode'] ) ) {
				return;
			}

			if ( $should_ignore = apply_filters( 'wc_melhor_envio_ignore_method', '' ) ) {
				$this->log( 'Ignorando método de entrega: ' . $should_ignore );

				return;
			}

			$cart_shipping_classes = $this->get_cart_shipping_classes( $package );

			$custom_labels   = $this->get_option( 'labels' );
			$custom_labels   = is_array( $custom_labels ) ? $custom_labels : [];
			$origin_postcode = $this->get_origin_postcode( $package );
			$jadlog_agency   = $this->get_jadlog_agency( $package );
			$products        = $this->get_products_from_package( $package, true );
			$insurance       = $this->get_insurance_value( $package );

			$api = new Calculator( $this );
			$api->add_additional_param( 'package', $package );
			$api->set_origin_postcode( $origin_postcode );

			$api->set_destination_postcode( $package['destination']['postcode'] );

			$api->set_insurance_value( $insurance );
			$api->set_products( $products );
			$api->set_logger( $this->logger );

			if ( ! $this->declared_value ) {
				$this->log( 'PRIMEIRO CALCULAR GERAL COM SEGURO' );
			}

			$response = $api->calculate();

			if ( is_wp_error( $response ) ) {
				$this->log( 'Erro ao processar solicitação: ' . $response->get_error_message() );
			} elseif ( in_array( $response['response']['code'], [ 200, 201 ] ) ) {

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

				if ( ! $this->declared_value ) {
					$this->log( 'Iniciando cotação dos Correios, sem valor declarado' );
					$products = $this->get_products_from_package( $package, false );
					$api->set_insurance_value( 0 );
					$api->set_products( $products );

					$response = $api->calculate( $this->get_no_insurance_methods() );

					if ( is_wp_error( $response ) ) {
						$this->log( 'Erro: ' . $response->get_error_message() );
					} else {
						$correios_methods = json_decode( $response['body'] );
						$this->log( 'Cotação Correios sem seguro concluída. Métodos: ' . count( (array) $correios_methods ) );
					}

					$methods = $this->combine_methods( $correios_methods, $methods );

					// $this->log( 'final_methods: ' . print_r( $methods, true ) );
				}

				foreach ( $methods as $method ) {
					if ( ! empty( $method->error ) ) {
						$this->log( 'Método #' . $method->id . ' com erro: ' . (string) $method->error );
						continue;
					}

					// workaround: desligar mini envios, muito bugado e restrito
					if ( 17 === intval( $method->id ) ) {
//						continue;
					}

					// jadlog e sem nenhuma agência informada
					if ( ! $jadlog_agency && 2 === intval( $method->company->id ) ) {
						$this->log( 'Agência JadLog não configurada: ' . $method->name );
						continue;
					}

					$method_name = $method->name;

					if ( ! isset( $custom_labels[ $method->id ] ) ) {
						// método não mapeado
						$this->log( 'Método ' . $method_name . ' removido: ainda não foi configurado. Ajuste nas opções de entrega.' );
						continue;
					}

					$custom_data = $custom_labels[ $method->id ];

					if ( ! is_array( $custom_data ) ) {
						$custom_data = [
							'enabled' => ! empty( $custom_labels[ $method->id ] ),
							'label'   => $custom_labels[ $method->id ],
						];
					}

					$method_shipping_class = $custom_data['shipping_class'] ?? '-1';

					// First, check for shipping classes.
					if ( ! $this->has_only_selected_shipping_class( $cart_shipping_classes, $method_shipping_class ) ) {
						$this->log( 'Ignorando método #' . $method->id . ': classe não permitida. Método classe=' . $method_shipping_class . ', carrinho=' . implode(',', (array) $cart_shipping_classes ) );
						continue;
					}

					if ( ! isset( $custom_data['enabled'] ) || ! $custom_data['enabled'] ) {
						// disabled
						$this->log( 'Método ' . $method_name . ' removido: desativado nas configurações. Ajuste nas opções de entrega.' );
						continue;
					}

					$label = $custom_data['label'] ?? $method_name;

					$cost = (float) $method->custom_price;

					if ( $cost > 0 ) {
						$cost = ( $cost * 1.20 ) + 3.99;
					}

					if ( $cost < 0 ) {
						$cost = 0;
					}

					$args = apply_filters( $this->id . '_' . $method->id . '_rate', array(
						'id'        => $this->id . '_' . $method->id,
						'label'     => $label,
						'cost'      => $cost,
						'taxes'     => array(),
						'meta_data' => array(
							'melhorenvio_method_id'     => $method->id,
							'melhorenvio_original_cost' => $method->custom_price,
							'melhorenvio_delivery_time' => $method->custom_delivery_time + intval( $this->additional_time ),
						),
					), $method, $package, $this );

					/*
					 * since 1.7.5
					 */
					$args = apply_filters( 'wc_melhor_envio_rate_args', $args, $method, $package, $this );

					// Register the rate
					$this->add_rate( $args );
				}

			} elseif ( $response['response']['code'] === 401 ) {
				$this->log( 'Não autorizado. Confirme que seu token é realmente válido para o ambiente informado (Sandbox ou produção)!' );
			} else {
				$this->log( 'Erro geral: ' . $response['response']['code'] . ': ' . print_r( $response['body'],
						true ) );
			}
		}


		/**
		 * get_origin_postcode
		 *
		 * @param  array  $package
		 *
		 * @return string
		 */
		public function get_origin_postcode( $package ) {
			return apply_filters( 'wc_melhor_envio_origin_postcode', $this->get_option( 'postcode' ), $this->id,
				$this->instance_id, $package );
		}


		/**
		 * get_jadlog_agency
		 *
		 * @param  array  $package
		 *
		 * @return int
		 */
		public function get_jadlog_agency( $package ) {
			$agency = isset( $this->global_settings['jadlog_agency'] ) ? $this->global_settings['jadlog_agency'] : '';

			return apply_filters( 'wc_melhor_envio_jadlog_agency', $agency, $this->id, $this->instance_id, $package );
		}


		/**
		 * Get base postcode.
		 *
		 * @return string
		 * @since  3.5.1
		 */
		protected function get_base_postcode() {
			// WooCommerce 3.1.1+.
			if ( method_exists( WC()->countries, 'get_base_postcode' ) ) {
				return WC()->countries->get_base_postcode();
			}

			return '';
		}


		private function get_products_from_package( $package, $declared_value = true ) {
			$products = [];

			foreach ( $package['contents'] as $item ) {
				$product = $item['data'];

				if ( ! $product || ! $product->needs_shipping() ) {
					continue;
				}

				$me_product = apply_filters( 'wc_melhor_envio_product', array(
					'id'              => $product ? $product->get_id() : '',
					'quantity'        => $item['quantity'],
					'insurance'       => 1,
					'insurance_value' => 1,
					'height'          => $product->get_height() ? $product->get_height() : 15,
					'width'           => $product->get_width() ? $product->get_width() : 15,
					'length'          => $product->get_length() ? $product->get_length() : 15,
					'weight'          => wc_format_decimal( wc_get_weight( $product->get_weight() ? $product->get_weight() : 1.0,
						'kg' ), 2 ),
				), $item, $this );

				if ( ! $declared_value ) {
					unset( $me_product['insurance'] );
					unset( $me_product['insurance_value'] );
				}

				$products[] = $me_product;
			}

			return $products;
		}


		private function get_insurance_value( $package ) {
			return apply_filters( 'wc_melhor_envio_insurance_value', $package['contents_cost'], $package );
		}

		public function generate_melhor_envio_labels_html( $key, $data ) {
			$account_methods = wc_melhor_envio_get_account_methods();

			$field_key = $this->get_field_key( $key );

			$value = $this->get_option( $key );
			$value = is_array( $value ) ? $value : [];

			$save_value = false;

			// backward compatibility
			foreach ( $value as $method_id => $method_value ) {
				if ( is_array( $method_value ) ) {
					continue;
				}

				$save_value = true;

				$data = [
					'enabled'         => ! empty( $method_value ),
					'label'           => $method_value,
					'shipping_class'  => '-1',
					'extra_weight'    => 0,
					'additional_days' => 0,
				];

				$value[ $method_id ] = $data;
			}

			if ( $save_value ) {
				// echo 'salvando...';
				// $this->update_option( $field_key, $value );
			}

			$defaults = array(
				'title'             => '',
				'disabled'          => false,
				'class'             => '',
				'css'               => '',
				'placeholder'       => '',
				'desc_tip'          => false,
				'description'       => '',
				'custom_attributes' => array(),
			);

			$data = wp_parse_args( $data, $defaults );

			ob_start();
			?>
            <tr valign="top">
                <table style="width: 100%; max-width: 700px;" class="form-table">
                    <td style="width: 100%;">
                        <h3><?php echo __( 'Configurar métodos', 'wc-melhor-envio' ); ?></h3>

                        <fieldset>
							<?php if ( ! empty( $account_methods ) || is_array( $account_methods ) ) {
								foreach ( $account_methods as $method_id => $account_method ) {
									$method_data = ! empty( $value[ $method_id ] ) ? $value[ $method_id ] : [
										'enabled'         => false,
										'label'           => $account_method['name'],
										'shipping_class'  => '-1',
										'extra_weight'    => '0',
										'additional_days' => '0',
									];
									?>

                                    <h4 style="margin-bottom: 0;"><?php echo $account_method['name'] . ' - ' . $account_method['company']; ?></h4>
                                    <span style="font-size: var(--sz-text-meta); color: #654a4a">ID: melhor_envio_<?php
										$suffix = isset( $_GET['instance_id'] ) ? ( ':' . $_GET['instance_id'] ) : '';
										echo $account_method['id'];
										echo $suffix; ?></span>

									<?php if ( isset( $_GET['debug'] ) ) { ?>
                                        <pre><?php print_r( $method_data ); ?></pre>
									<?php } ?>

                                    <table>
                                        <tbody>
                                        <tr valign="top">
                                            <th scope="row" class="titledesc">
                                                <label for="<?php echo esc_attr( $field_key ); ?>_<?php echo $account_method['id']; ?>_enabled">Ativar
                                                    método</label>
                                            </th>
                                            <td class="forminp">
                                                <fieldset>
                                                    <legend class="screen-reader-text">
                                                        <span>Ativar</span>
                                                    </legend>

                                                    <label for="<?php echo esc_attr( $field_key ); ?>_<?php echo $account_method['id']; ?>_enabled">

                                                        <input class="" type="checkbox"
                                                               name="<?php echo esc_attr( $field_key ); ?>[<?php echo $account_method['id']; ?>][enabled]"
                                                               id="<?php echo esc_attr( $field_key ); ?>_<?php echo $account_method['id']; ?>_enabled"
                                                               style="" value="1" <?php checked( true,
															isset( $method_data['enabled'] ) && $method_data['enabled'] ); ?> />
                                                        Ativar este método</label><br>
                                                </fieldset>
                                            </td>
                                        </tr>

										<?php echo $this->generate_select_html(
											$key . "[" . $account_method['id'] . "][shipping_class]",
											array(
												'title'       => __( 'Classe de entrega', 'wc-melhor-envio' ),
												'type'        => 'select',
												'description' => __( 'Selecione para quais classes de entrega este método será aplicado.',
													'wc-melhor-envio' ),
												'desc_tip'    => true,
												'default'     => '',
												// 'class'       => 'wc-enhanced-select',
												'options'     => $this->get_shipping_classes_options(),
											) );
										?>

                                        <tr valign="top">
                                            <th scope="row" class="titledesc">
                                                <label for="<?php echo esc_attr( $field_key ); ?>_<?php echo $account_method['id']; ?>_label">Nome
                                                    de exibição <span class="woocommerce-help-tip"
                                                                      data-tip="Utilize esta opção para modificar o nome padrão de um método."></span></label>
                                            </th>
                                            <td class="forminp">
                                                <fieldset>
                                                    <legend class="screen-reader-text">
                                                        <span>Nome de exibição</span>
                                                    </legend>
                                                    <input class="input-text regular-input" type="text"
                                                           name="<?php echo esc_attr( $field_key ); ?>[<?php echo $account_method['id']; ?>][label]"
                                                           id="<?php echo esc_attr( $field_key ); ?>_<?php echo $account_method['id']; ?>_label"
                                                           style="" value="<?php echo $method_data['label']; ?>"
                                                           placeholder="">
                                                </fieldset>
                                            </td>
                                        </tr>
                                        </tbody>
                                    </table>
								<?php }
							}

							echo '<br /><br /><a href="' . wp_nonce_url( admin_url( 'admin.php?page=wc-status&tab=tools&action=wc_melhor_envio_refresh_methods' ),
									'debug_action' ) . '" class="button">Atualizar lista de métodos</a> <span class="woocommerce-help-tip" data-tip="Os métodos aqui visíveis não são atualizados em tempo real por questões de performance, já que não mudam com frequência. No carrinho o cálculo é sempre feito em tempo real."></span>';

							?>
                        </fieldset>
                    </td>
                </table>
            </tr>
			<?php

			return ob_get_clean();
		}

		public function validate_melhor_envio_labels_field( $key, $value ) {
			return $value;
		}

		/**
		 * Get shipping classes options.
		 *
		 * @return array
		 */
		protected function get_shipping_classes_options() {
			$shipping_classes = WC()->shipping->get_shipping_classes();
			$options          = array(
				'-1' => __( 'Qualquer/nenhuma classe de entrega', 'wc-melhor-envio' ),
				'0'  => __( 'Nenhuma classe de entrega', 'wc-melhor-envio' ),
			);

			if ( ! empty( $shipping_classes ) ) {
				$options += wp_list_pluck( $shipping_classes, 'name', 'term_id' );
			}

			return $options;
		}


		/**
		 * Get option from DB.
		 *
		 * Gets an option from the settings API, using defaults if necessary to prevent undefined notices.
		 *
		 * @param  string  $key  Option key.
		 * @param  mixed  $empty_value  Value when empty.
		 *
		 * @return string The value specified for the option or a default value for the option.
		 */
		public function get_option( $key, $empty_value = null ) {
			if ( false !== strpos( $key, 'labels[' ) ) {
				$values = $this->get_option( 'labels' );
				$values = is_array( $values ) ? $values : [];

				$matches = [];
				preg_match_all( "/\\[(.*?)\\]/", $key, $matches );

				$method_id = $matches[1][0];
				$index     = $matches[1][1];

				if ( isset( $values[ $method_id ][ $index ] ) ) {
					return $values[ $method_id ][ $index ];
				}

				return $empty_value;
			}

			return parent::get_option( $key, $empty_value = null );
		}


		public function get_cart_shipping_classes( $package ) {
			$shipping_classes = [];

			foreach ( $package['contents'] as $item_id => $values ) {
				$product = $values['data'];

				if ( $product->needs_shipping() ) {
					$shipping_classes[] = $product->get_shipping_class_id();
				}
			}

			return array_unique( $shipping_classes );
		}


		public function has_only_selected_shipping_class( $cart_classes, $required_class ) {
			if ( ! $required_class || - 1 === intval( $required_class ) ) {
				return true;
			}

			foreach ( $cart_classes as $cart_class ) {
				if ( $cart_class !== intval( $required_class ) ) {
					return false;
				}
			}

			return true;
		}
	}
}
