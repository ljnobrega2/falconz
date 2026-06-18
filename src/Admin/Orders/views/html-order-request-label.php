<?php $customer_choice_label = ''; ?>

<label style="display: block; margin-bottom: 5px;" for="order-invoice-id"><?php echo __( 'Chave NF (opcional)',
		'wc-melhor-envio' ); ?></label>
<input
        type="text"
        id="order-invoice-id"
        placeholder="Opcional"
        name="order_invoice_id"
        value="<?php echo esc_attr( $invoice_id ); ?>"
/>

<label style="display: block; margin: 10px 0 5px 0;" for="order-service-id"><?php echo __( 'Método de envio',
		'wc-melhor-envio' ); ?></label>
<select
        id="order-service-id"
        placeholder="Opcional"
        name="order_service_id"
        class="<?php echo apply_filters( 'wc_melhor_envio_request_label_methods_class', 'me-shipping-list' ); ?>"
>
	<?php foreach (
		apply_filters( 'wc_melhor_envio_order_available_methods', wc_melhor_envio_get_account_methods(),
			$order ) as $_key => $_value
	) {
		if ( $_value['id'] == $customer_choice_service_id ) {
			$customer_choice_label = sprintf(
				'<p>%s</p>',
				sprintf( __( 'Escolha do cliente: %s', 'wc-melhor-envio' ),
					esc_html( $_value['company'] . ' - ' . $_value['name'] ) )
			);
		}
		?>
        <option value="<?php echo esc_attr( $_value['id'] ); ?>" <?php selected( $_value['id'], $current_service_id ); ?>>
			<?php echo esc_html( $_value['company'] . ' - ' . $_value['name'] ); ?>
        </option>
	<?php } ?>
</select>

<?php echo $customer_choice_label; ?>

<?php if ( wc_melhor_envio_enabled_reverse_logistic() ) { ?>
    <label for="melhor-envio-is-reverse" style="margin: 10px 0; display: block;">
        <input type="checkbox" id="melhor-envio-is-reverse" value="yes"/>
		<?php echo __( 'Logística reversa', 'wc-melhor-envio' ); ?>
    </label>
<?php } ?>

<?php do_action( 'wc_melhor_envio_after_metabox_fields', $order ); ?>

<?php wp_nonce_field( 'melhor_envio_request_label_' . $order->get_id(), 'melhor_envio_nonce' ); ?>

<a href="<?php echo add_query_arg( array(
	'action'    => 'melhor_envio_add_to_cart',
	'order_ids' => $order->get_id(),
), admin_url( 'admin-ajax.php' ) ); ?>"
   class="button me-is-single me_add_to_cart button-primary"
   style="margin-top: 10px;"
   data-order-id="<?php echo esc_attr( $order->get_id() ); ?>"
><?php echo __( 'Solicitar etiqueta', 'wc-melhor-envio' ); ?></a>
