<?php

namespace WC_MelhorEnvio\Emails;

use WC_Email;
use WC_Melhor_Envio;

if ( ! defined( 'ABSPATH' ) ) {
	exit;
}

class New_Tracking_Code extends WC_Email {

	public $message;


	/**
	 * Initialize tracking template.
	 */
	public function __construct() {
		$this->id             = 'melhor_envio_tracking_code';
		$this->title          = 'Melhor Envio: código de rastreio';
		$this->customer_email = true;
		$this->description    = 'Este e-mail é enviado ao cliente imediatamente após o rastreio ser adicionado ao pedido';
		$this->heading        = 'Seu pedido foi enviado';
		$this->subject        = '[{site_title}] Oba! O pedido #{order_number} foi entregue à transportadora!';
		$this->message        = 'Olá! Agora basta aguardar a entrega.'
		                        . PHP_EOL . ' ' . PHP_EOL .
		                        'O código de rastreio é: {tracking_code}'
		                        . PHP_EOL . ' ' . PHP_EOL;

		$this->template_html = 'emails/melhor-envio-tracking-code.php';

		// Call parent constructor.
		parent::__construct();

		$this->template_base = WC_Melhor_Envio::get_templates_path();
	}

	/**
	 * Initialise settings form fields.
	 */
	public function init_form_fields() {
		$this->form_fields = array(
			'enabled'    => array(
				'title'   => 'Habilitar/Desabilitar',
				'type'    => 'checkbox',
				'label'   => 'Habilitar esse e-mail de notificação',
				'default' => 'yes',
			),
			'subject'    => array(
				'title'       => 'Assunto',
				'type'        => 'text',
				'description' => 'O texto exibido no cabeçalho do e-mail. Deixe em branco para exibir o padrão.',
				'placeholder' => $this->subject,
				'default'     => '',
				'desc_tip'    => true,
			),
			'heading'    => array(
				'title'       => 'Cabeçalho do e-mail',
				'type'        => 'text',
				'description' => 'Isto controla o cabeçalho do conteúdo dentro do e-mail de notificação.',
				'placeholder' => $this->heading,
				'default'     => '',
				'desc_tip'    => true,
			),
			'message'    => array(
				'title'       => __( 'Email Content', 'woocommerce' ),
				'type'        => 'textarea',
				'description' => 'Conteúdo exibido antes da tabela de informações. Deixe em branco para utilizar o texto padrão. Você pode utilizar a variável <code>{tracking_code}</code>',
				'placeholder' => $this->message,
				'default'     => '',
				'desc_tip'    => true,
			),
			'email_type' => array(
				'title'       => __( 'Email type', 'woocommerce' ),
				'type'        => 'select',
				'description' => 'Escolha o formato de e-mail para enviar. Apenas HTML está disponível, por enquanto.',
				'default'     => 'html',
				'class'       => 'email_type wc-enhanced-select',
				'options'     => $this->get_custom_email_type_options(),
				'desc_tip'    => true,
			),
		);
	}

	/**
	 * Email type options.
	 *
	 * @return array
	 */
	protected function get_custom_email_type_options() {
		if ( class_exists( 'DOMDocument' ) ) {
			$types['html'] = __( 'HTML', 'woocommerce' );
		}

		return $types;
	}

	/**
	 *
	 * Trigger email.
	 *
	 */
	public function trigger( $order, $tracking_code = '' ) {
		// default values
		$this->tracking_code = $tracking_code;


		if ( is_object( $order ) ) {
			$this->object    = $order;
			$this->recipient = $order->get_billing_email();

			$this->placeholders = [
				'{order_number}'  => $order->get_order_number(),
				'{date}'          => wc_format_datetime( $this->object->get_date_created() ),
				'{tracking_code}' => wc_melhor_envio_get_formatted_tracking_url( $this->tracking_code ),
			];
		}

		if ( ! $this->get_recipient() ) {
			return;
		}

		$this->send( $this->get_recipient(), $this->get_subject(), $this->get_content(), $this->get_headers(),
			$this->get_attachments() );
	}

	/**
	 * Get content HTML.
	 *
	 * @return string
	 */
	public function get_content_html() {
		ob_start();

		wc_get_template( $this->template_html, array(
			'order'         => $this->object,
			'email_heading' => $this->get_heading(),
			'message'       => $this->get_message(),
			'sent_to_admin' => false,
			'plain_text'    => false,
			'code'          => $this->tracking_code,
			'email'         => $this,
		), '', $this->template_base );

		return ob_get_clean();
	}

	/**
	 * Get email tracking message.
	 *
	 * @return string
	 */
	public function get_message() {
		return $this->format_string( $this->message );
	}
}
