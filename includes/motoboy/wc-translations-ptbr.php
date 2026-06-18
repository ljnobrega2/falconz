<?php
/**
 * Senderzz - Mensagens de erro PT-BR WooCommerce/FunnelKit
 */
if ( ! defined( 'ABSPATH' ) ) exit;

add_filter('woocommerce_checkout_fields', function ($fields) {
    $labels = [
        'billing_first_name' => 'Nome',
        'billing_last_name'  => 'Sobrenome',
        'billing_company'    => 'Empresa',
        'billing_country'    => 'País',
        'billing_address_1'  => 'Endereço',
        'billing_address_2'  => 'Complemento',
        'billing_city'       => 'Cidade',
        'billing_state'      => 'Estado',
        'billing_postcode'   => 'CEP',
        'billing_phone'      => 'Telefone',
        'billing_email'      => 'E-mail',
        'billing_cpf'        => 'CPF',
        'billing_number'     => 'Número',
    ];
    foreach ($labels as $key => $label) {
        if (isset($fields['billing'][$key])) {
            $fields['billing'][$key]['label'] = $label;
        }
    }
    return $fields;
}, 9999);

add_filter('woocommerce_checkout_required_field_notice', function ($message, $field_label) {
    $label = wp_strip_all_tags((string) $field_label);
    $label = str_ireplace(['Billing ', 'Shipping ', 'Cobrança ', 'Entrega '], '', $label);
    $map = [
        'First name'       => 'Nome',
        'Last name'        => 'Sobrenome',
        'Phone'            => 'Telefone',
        'Email address'    => 'E-mail',
        'Postcode / ZIP'   => 'CEP',
        'Street address'   => 'Endereço',
        'Town / City'      => 'Cidade',
        'State / County'   => 'Estado',
        'Country / Region' => 'País',
        'Nome Completo'    => 'Nome completo',
    ];
    $label = $map[$label] ?? $label;
    return sprintf('%s é obrigatório.', $label);
}, 9999, 2);

function senderzz_translate_wc_error_ptbr($message) {
    if (!is_string($message) || trim($message) === '') return $message;
    $original = $message;
    $replaces = [
        'Please enter an address to continue.'       => 'Informe o endereço para continuar.',
        'Please enter a valid postcode / ZIP.'        => 'Informe um CEP válido.',
        'Please enter a valid phone number.'          => 'Informe um telefone válido.',
        'Please enter a valid email address.'         => 'Informe um e-mail válido.',
        'A valid phone number is required.'           => 'Informe um telefone válido.',
        'Invalid payment method.'                     => 'Forma de pagamento inválida.',
        'Please enter your password.'                 => 'Informe sua senha.',
        'Please enter your email address.'            => 'Informe seu e-mail.',
        'Please read and accept the terms and conditions to proceed with your order.' => 'Aceite os termos e condições para continuar.',
        'No shipping method has been selected. Please double check your address, or contact us if you need any help.' => 'Selecione uma forma de entrega para continuar.',
    ];
    foreach ($replaces as $from => $to) {
        if (stripos($message, $from) !== false) $message = str_ireplace($from, $to, $message);
    }
    $message = preg_replace_callback(
        '/(<strong[^>]*>)?(.*?)(<\/strong>)?\s+is\s+a\s+required\s+field\.?/i',
        function ($m) {
            $label = wp_strip_all_tags($m[2] ?? '');
            $label = trim(str_ireplace(['Billing ', 'Shipping ', 'billing ', 'shipping '], '', $label));
            $map = ['First name'=>'Nome','Last name'=>'Sobrenome','Phone'=>'Telefone','Email address'=>'E-mail','Postcode / ZIP'=>'CEP','Street address'=>'Endereço','Town / City'=>'Cidade','State / County'=>'Estado','Country / Region'=>'País'];
            $label = $map[$label] ?? $label;
            return '<strong>' . esc_html($label) . '</strong> é obrigatório.';
        }, $message
    );
    $message = preg_replace_callback(
        '/(<strong[^>]*>)?(.*?)(<\/strong>)?\s+is\s+required\.?/i',
        function ($m) {
            $label = wp_strip_all_tags($m[2] ?? '');
            $label = trim(str_ireplace(['Billing ', 'Shipping ', 'billing ', 'shipping '], '', $label));
            return '<strong>' . esc_html($label) . '</strong> é obrigatório.';
        }, $message
    );
    $message = preg_replace('/\s+is not a valid phone number\.?/i', ' não é um telefone válido.', $message);
    $message = preg_replace('/\s+is not a valid email address\.?/i', ' não é um e-mail válido.', $message);
    $message = preg_replace('/\s+is not a valid postcode \/ ZIP\.?/i', ' não é um CEP válido.', $message);
    return $message ?: $original;
}

add_filter('woocommerce_add_error',   'senderzz_translate_wc_error_ptbr', 9999);
add_filter('woocommerce_add_notice',  'senderzz_translate_wc_error_ptbr', 9999);
add_filter('woocommerce_add_success', 'senderzz_translate_wc_error_ptbr', 9999);

add_action('woocommerce_after_checkout_validation', function ($data, $errors) {
    if (!$errors || !is_wp_error($errors)) return;
    foreach ($errors->get_error_codes() as $code) {
        $messages = $errors->get_error_messages($code);
        if (method_exists($errors, 'remove')) $errors->remove($code);
        foreach ($messages as $msg) $errors->add($code, senderzz_translate_wc_error_ptbr($msg));
    }
}, 9999, 2);

add_filter('gettext', function ($translated, $text, $domain) {
    $translations = [
        '%s is a required field.'              => '%s é obrigatório.',
        '%s is required.'                      => '%s é obrigatório.',
        '%s is not a valid phone number.'      => '%s não é um telefone válido.',
        '%s is not a valid email address.'     => '%s não é um e-mail válido.',
        '%s is not a valid postcode / ZIP.'    => '%s não é um CEP válido.',
        'Please enter an address to continue.' => 'Informe o endereço para continuar.',
        'Please enter a valid postcode / ZIP.' => 'Informe um CEP válido.',
        'Invalid payment method.'              => 'Forma de pagamento inválida.',
    ];
    return $translations[$text] ?? $translated;
}, 9999, 3);

add_action('wp_footer', function () {
    if (!function_exists('is_checkout') || !is_checkout()) return;
    ?>
    <script>
    (function () {
        function senderzzReplaceText(node) {
            if (!node || node.nodeType !== Node.TEXT_NODE) return;
            var txt = node.nodeValue, old = txt;
            txt = txt.replace(/Billing\s+/gi, '').replace(/Shipping\s+/gi, '');
            txt = txt.replace(/\s+is a required field\.?/gi, ' é obrigatório.');
            txt = txt.replace(/\s+is required\.?/gi, ' é obrigatório.');
            txt = txt.replace(/\s+is not a valid phone number\.?/gi, ' não é um telefone válido.');
            txt = txt.replace(/\s+is not a valid email address\.?/gi, ' não é um e-mail válido.');
            txt = txt.replace(/\s+is not a valid postcode \/ ZIP\.?/gi, ' não é um CEP válido.');
            txt = txt.replace(/Please enter an address to continue\.?/gi, 'Informe o endereço para continuar.');
            txt = txt.replace(/Please enter a valid postcode \/ ZIP\.?/gi, 'Informe um CEP válido.');
            txt = txt.replace(/Invalid payment method\.?/gi, 'Forma de pagamento inválida.');
            if (txt !== old) node.nodeValue = txt;
        }
        function senderzzTranslateCheckoutErrors() {
            var selectors = ['.woocommerce-error','.woocommerce-message','.woocommerce-info','.wfacp_main_form','.wfacp_error','.wfacp-notice','.wfacp_notice','.wfacp_error_message','.wfacp_field_required_msg'];
            selectors.forEach(function (selector) {
                document.querySelectorAll(selector).forEach(function (el) {
                    var walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT, null), nodes = [];
                    while (walker.nextNode()) nodes.push(walker.currentNode);
                    nodes.forEach(senderzzReplaceText);
                });
            });
        }
        senderzzTranslateCheckoutErrors();
        document.body.addEventListener('updated_checkout', senderzzTranslateCheckoutErrors);
        document.body.addEventListener('checkout_error', senderzzTranslateCheckoutErrors);
        new MutationObserver(senderzzTranslateCheckoutErrors).observe(document.body, {childList:true,subtree:true});
    })();
    </script>
    <?php
}, 9999);
