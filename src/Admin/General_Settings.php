<?php

namespace WC_MelhorEnvio\Admin;

use WC_Integration;
use WC_Melhor_Envio;
use WC_MelhorEnvio\Traits\Helpers;

class General_Settings extends WC_Integration {
	use Helpers;

	public function __construct() {
		// Setup general properties
		$this->setup();

		// Load the settings
		$this->init_form_fields();
		$this->init_settings();

		// Actions.
		add_action(
			'woocommerce_update_options_integration_' . $this->id,
			[ $this, 'process_admin_options' ]
		);

		// Add settings link to plugins page
		add_filter(
			'plugin_action_links_' . \plugin_basename( WC_Melhor_Envio::get_main_file() ),
			[ $this, 'add_settings_link' ]
		);
	}

	protected function setup() {
		$this->id                 = 'wc-melhor-envio';
		$this->method_title       = __( 'Melhor Envio', 'wc-melhor-envio' );
		$this->method_description = __( 'Configure sua integração com Melhor Envio. Após clicar em salvar, revise a seção "Eventos agendados" no final da página para verificar se o agendamento funcionou corretamente.',
			'wc-melhor-envio' );
	}

	public function init_form_fields() {
		$this->form_fields = apply_filters( 'melhor_envio_integration_fields', [
			'client_secret'          => [
				'title'       => __( 'Token', 'wc-melhor-envio' ),
				'type'        => 'text',
				'description' => __( 'Informe o token de API Melhor Envio.', 'wc-melhor-envio' ),
				'default'     => '',
			],
			'allowed_status'         => [
				'title'       => __( 'Status para gerar etiquetas', 'wc-melhor-envio' ),
				'type'        => 'multiselect',
				'description' => __( 'Selecione os status onde é possível solicitar etiquetas. Após criadas, o link de impressão sempre estará disponível.',
					'wc-melhor-envio' ),
				'class'       => 'wc-enhanced-select',
				'default'     => [ 'processing' ],
				'options'     => $this->get_order_statuses(),
			],
			'status_after_print'     => [
				'title'       => __( 'Status após gerar etiqueta', 'wc-melhor-envio' ),
				'type'        => 'select',
				'description' => __( 'Assim que a etiqueta for gerada, o pedido será atualizado com este status. Ex.: separação e expedição',
					'wc-melhor-envio' ),
				'class'       => 'wc-enhanced-select',
				'default'     => '',
				'options'     => [ 'none' => 'Nenhum' ] + $this->get_order_statuses(),
			],
			'status_after_posted'    => [
				'title'       => __( 'Status após envio', 'wc-melhor-envio' ),
				'type'        => 'select',
				'description' => __( 'Assim que o envio for identificado, o pedido será atualizado com este status.',
					'wc-melhor-envio' ),
				'class'       => 'wc-enhanced-select',
				'default'     => 'none',
				'options'     => [ 'none' => 'Nenhum' ] + $this->get_order_statuses(),
			],
			'status_after_delivered' => [
				'title'       => __( 'Status após entrega', 'wc-melhor-envio' ),
				'type'        => 'select',
				'description' => __( 'Assim que a entrega for finalizada, o pedido será atualizado com este status.',
					'wc-melhor-envio' ),
				'class'       => 'wc-enhanced-select',
				'default'     => 'completed',
				'options'     => [ 'none' => 'Nenhum' ] + $this->get_order_statuses(),
			],
			'undelivered_status'     => [
				'title'       => __( 'Status quando a entrega falhar', 'wc-melhor-envio' ),
				'type'        => 'select',
				'description' => __( 'Se por algum motivo a transportadora não conseguir concluir a entrega, o pedido será atualizado para este status.',
					'wc-melhor-envio' ),
				'class'       => 'wc-enhanced-select',
				'default'     => 'completed',
				'options'     => [ 'none' => 'Nenhum' ] + $this->get_order_statuses(),
			],
			'status_to_tracking'     => [
				'title'       => __( 'Status para monitorar', 'wc-melhor-envio' ),
				'type'        => 'multiselect',
				'description' => __( 'Selecione os status que devem ser monitorados para verificar se o pedido já foi enviado. Ex.: separação e expedição. <strong>Sempre incluir o status de "Status após gerar etiqueta"</strong>.',
					'wc-melhor-envio' ),
				'class'       => 'wc-enhanced-select',
				'default'     => [ 'processing' ],
				'options'     => $this->get_order_statuses(),
			],
			'jadlog_agency'          => [
				'title'       => __( 'Agência Jadlog', 'wc-melhor-envio' ),
				'type'        => 'select',
				'description' => __( 'Para usar Jadlog é necessário informar a agência de postagem. Faltando alguma agência? <a href="' . wp_nonce_url( admin_url( 'admin.php?page=wc-status&tab=tools&action=wc_melhor_envio_refresh_agencies' ),
						'debug_action' ) . '">Atualizar lista</a>', 'wc-melhor-envio' ),
				'class'       => 'wc-enhanced-select',
				'options'     => $this->get_agencies_list(),
			],
			'latam_agency'           => [
				'title'       => __( 'Agência Latam', 'wc-melhor-envio' ),
				'type'        => 'select',
				'description' => __( 'Para usar Latam é necessário informar a agência de postagem. Faltando alguma agência? <a href="' . wp_nonce_url( admin_url( 'admin.php?page=wc-status&tab=tools&action=wc_melhor_envio_refresh_agencies' ),
						'debug_action' ) . '">Atualizar lista</a>', 'wc-melhor-envio' ),
				'class'       => 'wc-enhanced-select',
				'options'     => $this->get_latam_agencies_list(),
			],
				'cnae'                   => [
					'title'             => __( 'CNAE', 'wc-melhor-envio' ),
					'type'              => 'text',
					'description'       => __( 'Obrigatório para envio pela LATAM. Informe apenas números, com no máximo 7 dígitos.', 'wc-melhor-envio' ),
					'default'           => '',
					'custom_attributes' => [
						'maxlength' => '7',
						'pattern'   => '[0-9]*',
					],
				],
			'address'                => [
				'title'       => __( 'Endereço do remetente', 'wc-melhor-envio' ),
				'type'        => 'melhor_envio_address',
				'description' => __( 'Endereço utilizado na compra e geração de etiquetas', 'wc-melhor-envio' ),
				'options'     => $this->get_agencies_list(),
			],
			'declared_value'         => array(
				'title'       => 'Ativar valor declarado para Correios',
				'type'        => 'checkbox',
				'description' => 'Nos demais métodos, não é possível desativar.',
				'default'     => 'yes',
				'desc_tip'    => true,
			),
			'log'                    => [
				'title'       => __( 'Log', 'wc-melhor-envio' ),
				'type'        => 'checkbox',
				'label'       => __( 'Habilitar', 'wc-melhor-envio' ),
				'description' => __( 'Registrar conexões com a API.', 'wc-melhor-envio' ),
				'default'     => 'no',
			],
			'add_to_cart_only'       => [
				'title'       => __( 'Apenas adicionar ao carrinho', 'wc-melhor-envio' ),
				'type'        => 'checkbox',
				'label'       => __( 'Finalizar compra direto no portal do Melhor Envio', 'wc-melhor-envio' ),
				'description' => __( 'No Melhor Envio será possível modificar informações e realizar pagamento pós-pago.',
					'wc-melhor-envio' ),
				'default'     => 'no',
			],
			'status_add_to_cart'     => [
				'title'       => __( 'Adicionar ao carrinho automaticamente',
					'wc-melhor-envio' ),
				'type'        => 'select',
				'description' => __( 'É possível adicionar ao carrinho automaticamente quando o pedido tiver determinado status.',
					'wc-melhor-envio' ),
				'class'       => 'wc-enhanced-select',
				'default'     => 'none',
				'options'     => [ 'none' => 'Não adicionar automaticamente' ] + $this->get_order_statuses(),
			],
			'status_after_cart'      => [
				'title'       => __( 'Se apenas adicionando ao carrinho, selecione o status para atualizar',
					'wc-melhor-envio' ),
				'type'        => 'select',
				'description' => __( 'Status do pedido após enviar ao Melhor Envio. Não esqueça de incluir este status em "Status para monitorar"',
					'wc-melhor-envio' ),
				'class'       => 'wc-enhanced-select',
				'default'     => 'none',
				'options'     => [ 'none' => 'Nenhum' ] + $this->get_order_statuses(),
			],
			'full_auto_pipeline'     => [
				'title'       => __( 'Fluxo completo automático', 'wc-melhor-envio' ),
				'type'        => 'checkbox',
				'label'       => __( 'Adicionar ao carrinho, comprar, gerar e baixar PDF automaticamente', 'wc-melhor-envio' ),
				'description' => __( 'Quando ativo, o plugin executa o pipeline completo do Melhor Envio em um status específico.', 'wc-melhor-envio' ),
				'default'     => 'no',
			],
			'status_full_pipeline'    => [
				'title'       => __( 'Status para fluxo completo automático', 'wc-melhor-envio' ),
				'type'        => 'select',
				'description' => __( 'Ao entrar neste status, o pedido será enviado ao carrinho, comprado, terá a etiqueta gerada e o PDF baixado.', 'wc-melhor-envio' ),
				'class'       => 'wc-enhanced-select',
				'default'     => 'none',
				'options'     => [ 'none' => 'Não executar automaticamente' ] + $this->get_order_statuses(),
			],
			'auto_download_pdf'       => [
				'title'       => __( 'Baixar PDF automaticamente', 'wc-melhor-envio' ),
				'type'        => 'checkbox',
				'label'       => __( 'Salvar o PDF da etiqueta no servidor após gerar o link', 'wc-melhor-envio' ),
				'default'     => 'yes',
			],
			'pipeline_attempts'       => [
				'title'       => __( 'Tentativas do pipeline', 'wc-melhor-envio' ),
				'type'        => 'number',
				'description' => __( 'Quantidade de tentativas para gerar e imprimir a etiqueta quando a API ainda estiver processando.', 'wc-melhor-envio' ),
				'default'     => 8,
				'custom_attributes' => [ 'min' => 1, 'step' => 1 ],
			],
			'pipeline_delay'          => [
				'title'       => __( 'Atraso entre tentativas (segundos)', 'wc-melhor-envio' ),
				'type'        => 'number',
				'description' => __( 'Tempo de espera entre cada tentativa de obtenção do link / PDF da etiqueta.', 'wc-melhor-envio' ),
				'default'     => 10,
				'custom_attributes' => [ 'min' => 1, 'step' => 1 ],
			],
			'sandbox'                => [
				'title'       => __( 'Sandbox', 'wc-melhor-envio' ),
				'type'        => 'checkbox',
				'label'       => __( 'Habilitar', 'wc-melhor-envio' ),
				'description' => __( 'Usar modo de testes da API.', 'wc-melhor-envio' ),
				'default'     => 'no',
			],
			'check_interval'         => [
				'title'       => __( 'Intervalo de verificação', 'wc-melhor-envio' ),
				'type'        => 'select',
				'description' => __( 'A primeira verificação sempre será feita ao salvar as configurações e as próximas conforme escolhido aqui.',
					'wc-melhor-envio' ),
				'class'       => 'wc-enhanced-select',
				'default'     => 'daily',
				'options'     => [
					'daily'      => 'Uma vez ao dia',
					'twicedaily' => 'A cada 12 horas',
					'hourly'     => 'A cada hora',
				],
			],
			'cron_events'            => [
				'title'       => __( 'Eventos agendados', 'wc-melhor-envio' ),
				'type'        => 'melhor_envio_queue',
				'description' => __( 'Vejas os eventos agendados pelo plugin.', 'wc-melhor-envio' ),
				'default'     => 'no',
			],
		] );
	}

	private function get_order_statuses() {
		$statuses = [];

		if ( isset( $_GET['section'] ) && $this->id === $_GET['section'] ) {
			foreach ( wc_get_order_statuses() as $slug => $name ) {
				$statuses[ str_replace( 'wc-', '', $slug ) ] = $name;
			}

			if ( function_exists( 'wc_order_status_manager_get_order_status_posts' ) ) {
				foreach ( wc_order_status_manager_get_order_status_posts() as $status ) {
					$statuses[ $status->post_name ] = $status->post_title;
				}
			}
		}

		return $statuses;
	}

	public function add_settings_link( $links ) {
		$page_id = $this->id;
		$url     = admin_url( "/admin.php?page=wc-settings&tab=integration&section=$page_id" );
		$label   = esc_html__( 'Configurações', 'wc-melhor-envio' );
		$link    = "<a href='$url'>$label</a>";
		\array_unshift( $links, $link );

		return $links;
	}

	public function generate_melhor_envio_queue_html( $key, $data ) {
		$field_key = $this->get_field_key( $key );
		$defaults  = array(
			'title'             => '',
			'disabled'          => false,
			'class'             => '',
			'css'               => '',
			'placeholder'       => '',
			'type'              => 'text',
			'desc_tip'          => false,
			'description'       => '',
			'custom_attributes' => array(),
		);

		$data = wp_parse_args( $data, $defaults );

		ob_start();
		?>
        <tr valign="top">
            <th scope="row" class="titledesc">
                <label for="<?php echo esc_attr( $field_key ); ?>"><?php echo wp_kses_post( $data['title'] ); ?><?php echo $this->get_tooltip_html( $data ); // WPCS: XSS ok. ?></label>
            </th>
            <td class="forminp">
				<?php
				$settings    = $this->get_integration_settings();
				$after_print = $settings['status_after_print'];

				if ( ! $after_print || 'none' === $after_print ) {
					echo '<p>' . __( 'Você não configurou nenhum status para atualizar o pedido após gerar a etiqueta. Certifique-se de que isso está correto.',
							'wc-melhor-envio' ) . '</p>';
				}

				if ( empty( $settings['status_to_tracking'] ) ) {
					echo '<p>' . __( 'Nenhum status configurado para rastreio', 'wc-melhor-envio' ) . '</p>';
				} else {
					if ( 'none' !== $after_print && ! in_array( $after_print, $settings['status_to_tracking'] ) ) {
						echo '<p style="color: red; font-weight: 700;">' . __( 'O status definido para atualizar o pedido após gerar a etiqueta não está na lista de status para monitorar. Isso quer dizer que o pedido ficará preso nesse status.',
								'wc-melhor-envio' ) . '</p>';
					}
				}

				$next_event = wp_get_scheduled_event( 'wc_melhor_envio_check_posted' );

				if ( ! $next_event ) {
					echo '<p>' . __( 'O evento de monitoramento não está agendado. Há algo de errado com seu site.',
							'wc-melhor-envio' ) . '</p>';
				} else {
					echo '<p>' . __( 'O evento de monitoramento está agendado.',
							'wc-melhor-envio' ) . ' <a target="_blank" href="' . admin_url( '?melhor-envio-force-cron' ) . '">' . __( 'Verificar todos os pedidos agora',
							'correios-updater' ) . '</a>' . '.</p>';

					$utc_offset = $this->get_utc_offset();

					echo $this->format_next_event_date( $next_event->timestamp );
				}
				?>
            </td>
        </tr>
		<?php

		return ob_get_clean();
	}

	public function get_utc_offset() {
		$offset = get_option( 'gmt_offset', 0 );

		if ( empty( $offset ) ) {
			return 'UTC';
		}

		if ( 0 <= $offset ) {
			$formatted_offset = '+' . (string) $offset;
		} else {
			$formatted_offset = (string) $offset;
		}
		$formatted_offset = str_replace(
			array( '.25', '.5', '.75' ),
			array( ':15', ':30', ':45' ),
			$formatted_offset
		);

		return 'UTC' . $formatted_offset;
	}

	public function format_next_event_date( $time ) {
		$date_local_format = 'Y-m-d H:i:s';
		$offset_site       = get_date_from_gmt( 'now', 'P' );
		$offset_event      = get_date_from_gmt( gmdate( 'Y-m-d H:i:s', $time ), 'P' );

		if ( $offset_site !== $offset_event ) {
			$date_local_format .= ' P';
		}

		$date_utc   = gmdate( 'c', $time );
		$date_local = get_date_from_gmt( gmdate( 'Y-m-d H:i:s', $time ), $date_local_format );

		$formatted_time = sprintf(
			'<time datetime="%1$s">%2$s</time>',
			esc_attr( $date_utc ),
			esc_html( $date_local )
		);

		$until = $time - time();
		$late  = ( $until < ( 0 - ( 10 * MINUTE_IN_SECONDS ) ) );

		if ( $late ) {
			// Show a warning for events that are late.
			$ago = sprintf(
			/* translators: %s: Time period, for example "8 minutes" */
				__( '%s ago', 'wp-crontrol' ),
				$this->interval( abs( $until ) )
			);

			return sprintf(
				'<p>Próximo evento: %s <br /><span class="status-crontrol-warning"><span class="dashicons dashicons-warning" aria-hidden="true"></span> %s</span><br />Os eventos do seu site não estão funcionando. Revise suas configurações ou entre em contato com sua hospedagem.</p>',
				$formatted_time,
				esc_html( $ago )
			);
		}

		return '<p>Próximo evento: ' . $formatted_time . '</p>';
	}

	public function interval( $since ) {
		// Array of time period chunks.
		$chunks = array(
			/* translators: 1: The number of years in an interval of time. */
			array( 60 * 60 * 24 * 365, _n_noop( '%s year', '%s years', 'wp-crontrol' ) ),
			/* translators: 1: The number of months in an interval of time. */
			array( 60 * 60 * 24 * 30, _n_noop( '%s month', '%s months', 'wp-crontrol' ) ),
			/* translators: 1: The number of weeks in an interval of time. */
			array( 60 * 60 * 24 * 7, _n_noop( '%s week', '%s weeks', 'wp-crontrol' ) ),
			/* translators: 1: The number of days in an interval of time. */
			array( 60 * 60 * 24, _n_noop( '%s day', '%s days', 'wp-crontrol' ) ),
			/* translators: 1: The number of hours in an interval of time. */
			array( 60 * 60, _n_noop( '%s hour', '%s hours', 'wp-crontrol' ) ),
			/* translators: 1: The number of minutes in an interval of time. */
			array( 60, _n_noop( '%s minute', '%s minutes', 'wp-crontrol' ) ),
			/* translators: 1: The number of seconds in an interval of time. */
			array( 1, _n_noop( '%s second', '%s seconds', 'wp-crontrol' ) ),
		);

		if ( $since <= 0 ) {
			return __( 'now', 'wp-crontrol' );
		}

		/**
		 * We only want to output two chunks of time here, eg:
		 * x years, xx months
		 * x days, xx hours
		 * so there's only two bits of calculation below:
		 */

		// Step one: the first chunk.
		foreach ( array_keys( $chunks ) as $i ) {
			$seconds = $chunks[ $i ][0];
			$name    = $chunks[ $i ][1];

			// Finding the biggest chunk (if the chunk fits, break).
			$count = (int) floor( $since / $seconds );
			if ( $count ) {
				break;
			}
		}

		// Set output var.
		$output = sprintf( translate_nooped_plural( $name, $count, 'wp-crontrol' ), $count );

		// Step two: the second chunk.
		if ( $i + 1 < count( $chunks ) ) {
			$seconds2 = $chunks[ $i + 1 ][0];
			$name2    = $chunks[ $i + 1 ][1];
			$count2   = (int) floor( ( $since - ( $seconds * $count ) ) / $seconds2 );
			if ( $count2 ) {
				// Add to output var.
				$output .= ' ' . sprintf( translate_nooped_plural( $name2, $count2, 'wp-crontrol' ), $count2 );
			}
		}

		return $output;
	}

	public function validate_melhor_envio_address_field( $key, $value ) {
		return $value;
	}

	/**
	 * Generate Melhor Envio Address Input HTML.
	 *
	 * @param  string  $key  Input key.
	 * @param  array  $data  Input data.
	 *
	 * @return string
	 */
	public function generate_melhor_envio_address_html( $key, $data ) {
		$field_key = $this->get_field_key( $key );
		$defaults  = array(
			'title'       => '',
			'label'       => '',
			'desc_tip'    => false,
			'description' => '',
		);

		$data = wp_parse_args( $data, $defaults );

		$value = $this->get_option( $key );

		ob_start();
		?>
        <tr valign="top">
            <th scope="row" class="titledesc">
                <label for="<?php echo esc_attr( $field_key ); ?>"><?php echo wp_kses_post( $data['title'] ); ?></label>
				<?php echo $this->get_tooltip_html( $data ); // WPCS: XSS ok. ?>
            </th>
            <td class="forminp">
                <style>
                    .me-address-group {
                        margin-bottom: 15px;
                        display: flex;
                        width: 100%;
                        max-width: 550px;
                    }

                    .me-address-field {
                        width: 47%;
                    }

                    .me-address-field input {
                        width: 100% !important;
                    }

                    .me-address-field + .me-address-field {
                        margin-left: 15px;
                    }
                </style>

                <div class="me-address-group">
                    <div class="me-address-field">
                        <input
                                class="input-text regular-input"
                                type="text"
                                name="<?php echo esc_attr( $field_key ); ?>[name]"
                                id="<?php echo esc_attr( $field_key ); ?>_name"
                                value="<?php echo isset( $value['name'] ) ? $value['name'] : ''; ?>"
                                placeholder="Remetente - Nome Completo"
                        />
                    </div>

                    <div class="me-address-field">
                        <input
                                class="input-text regular-input"
                                type="text"
                                name="<?php echo esc_attr( $field_key ); ?>[document]"
                                id="<?php echo esc_attr( $field_key ); ?>_document"
                                value="<?php echo isset( $value['document'] ) ? $value['document'] : ''; ?>"
                                placeholder="CPF/CNPJ"
                        />
                    </div>
                </div>
                <div class="me-address-group">
                    <div class="me-address-field">
                        <input
                                class="input-text regular-input"
                                type="email"
                                name="<?php echo esc_attr( $field_key ); ?>[email]"
                                id="<?php echo esc_attr( $field_key ); ?>_email"
                                value="<?php echo isset( $value['email'] ) ? $value['email'] : ''; ?>"
                                placeholder="E-mail"
                        />
                    </div>
                    <div class="me-address-field">
                        <input
                                class="input-text regular-input"
                                type="text"
                                name="<?php echo esc_attr( $field_key ); ?>[phone]"
                                id="<?php echo esc_attr( $field_key ); ?>_phone"
                                value="<?php echo isset( $value['phone'] ) ? $value['phone'] : ''; ?>"
                                placeholder="Telefone"
                        />
                    </div>
                </div>
                <div class="me-address-group">
                    <div class="me-address-field" style="width: 70%;">
                        <input
                                class="input-text regular-input"
                                type="text"
                                name="<?php echo esc_attr( $field_key ); ?>[address]"
                                id="<?php echo esc_attr( $field_key ); ?>_address"
                                value="<?php echo isset( $value['address'] ) ? $value['address'] : ''; ?>"
                                placeholder="Rua"
                        />
                    </div>
                    <div class="me-address-field" style="width: 24%;">
                        <input
                                class="input-text regular-input"
                                type="text"
                                name="<?php echo esc_attr( $field_key ); ?>[number]"
                                id="<?php echo esc_attr( $field_key ); ?>_number"
                                value="<?php echo isset( $value['number'] ) ? $value['number'] : ''; ?>"
                                placeholder="Número"
                        />
                    </div>
                </div>
                <div class="me-address-group">
                    <div class="me-address-field">
                        <input
                                class="input-text regular-input"
                                type="text"
                                name="<?php echo esc_attr( $field_key ); ?>[complement]"
                                id="<?php echo esc_attr( $field_key ); ?>_complement"
                                value="<?php echo isset( $value['complement'] ) ? $value['complement'] : ''; ?>"
                                placeholder="Complemento"
                        />
                    </div>
                    <div class="me-address-field">
                        <input
                                class="input-text regular-input"
                                type="text"
                                name="<?php echo esc_attr( $field_key ); ?>[district]"
                                id="<?php echo esc_attr( $field_key ); ?>_district"
                                value="<?php echo isset( $value['district'] ) ? $value['district'] : ''; ?>"
                                placeholder="Bairro"
                        />
                    </div>
                </div>
                <div class="me-address-group">
                    <div class="me-address-field">
                        <input
                                class="input-text regular-input"
                                type="text"
                                name="<?php echo esc_attr( $field_key ); ?>[city]"
                                id="<?php echo esc_attr( $field_key ); ?>_city"
                                value="<?php echo isset( $value['city'] ) ? $value['city'] : ''; ?>"
                                placeholder="Cidade"
                        />
                    </div>
                    <div class="me-address-field">
                        <input
                                class="input-text regular-input"
                                type="text"
                                name="<?php echo esc_attr( $field_key ); ?>[postal_code]"
                                id="<?php echo esc_attr( $field_key ); ?>_postal_code"
                                value="<?php echo isset( $value['postal_code'] ) ? $value['postal_code'] : ''; ?>"
                                placeholder="CEP"
                        />
                    </div>
                </div>

				<?php echo $this->get_description_html( $data ); // WPCS: XSS ok. ?>
            </td>
        </tr>
		<?php

		return ob_get_clean();
	}

	public function process_admin_options() {
		wp_clear_scheduled_hook( 'wc_melhor_envio_check_posted' );

		parent::process_admin_options();
	}
}
