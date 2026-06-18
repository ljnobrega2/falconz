<?php
defined( 'ABSPATH' ) || exit;
if ( defined( 'SENDERZZ_PREFERIDA_CHECKOUT_HOOKS_LOADED' ) ) return;
define( 'SENDERZZ_PREFERIDA_CHECKOUT_HOOKS_LOADED', true );

/**
 * checkout-hooks.php — v2 (modo mais barata)
 *
 * Fluxo:
 *  1. `wc_melhor_envio_rate_args`  → coleta todos os rates do ME num buffer por package.
 *  2. `woocommerce_package_rates`  → após todos os rates serem registrados, encontra
 *                                    o mais barato entre os permitidos, marca-o e reordena.
 *  3. `woocommerce_cart_shipping_method_full_label` → injeta marcador HTML para o JS/CSS.
 *  4. `wp_enqueue_scripts`         → carrega CSS + JS no checkout / FunnelKit.
 */
defined( 'ABSPATH' ) || exit;


// ─── Buffer: coleta os args de cada rate antes de add_rate() ─────────────────
// Chave: serialize do package (identifica o package único)
// Valor: array de [ rate_key => [ args, cost ] ]
$GLOBALS['_tp_rate_buffer'] = [];

add_filter( 'wc_melhor_envio_rate_args', function ( array $args, $method, $package ) {

    $pkg_key = tp_package_key( $package );
    $mid     = intval( $args['meta_data']['melhorenvio_method_id'] ?? 0 );

    if ( ! isset( $GLOBALS['_tp_rate_buffer'][ $pkg_key ] ) ) {
        $GLOBALS['_tp_rate_buffer'][ $pkg_key ] = [];
    }

    // Guarda method_id e custo para encontrar o mais barato depois
    $GLOBALS['_tp_rate_buffer'][ $pkg_key ][ $mid ] = floatval( $args['cost'] ?? 0 );

    return $args;

}, 10, 3 );


// ─── Reordenar + marcar o mais barato entre os permitidos ────────────────────
add_filter( 'woocommerce_package_rates', function ( array $rates, $package ) {

    if ( empty( $rates ) ) {
        return $rates;
    }

    // Descobre as permitidas para as classes do carrinho
    $permitidas = tp_get_permitidas_for_package( $package );

    if ( empty( $permitidas ) ) {
        // Nenhuma configuração de permitidas: ordena por preço e marca o mais barato com badge
        uasort( $rates, fn( $a, $b ) => $a->get_cost() <=> $b->get_cost() );
        $first_key = array_key_first( $rates );
        if ( $first_key !== null ) {
            $rates[ $first_key ]->add_meta_data( 'tp_preferido', true );
        }
        return $rates;
    }

    $pkg_key = tp_package_key( $package );
    $buffer  = $GLOBALS['_tp_rate_buffer'][ $pkg_key ] ?? [];

    // Filtra o buffer para apenas as permitidas que estão disponíveis
    $candidatos = [];
    foreach ( $permitidas as $mid ) {
        if ( isset( $buffer[ $mid ] ) ) {
            $candidatos[ $mid ] = $buffer[ $mid ]; // mid => custo
        }
    }

    // Encontra o mais barato
    $winner_mid = null;
    if ( ! empty( $candidatos ) ) {
        asort( $candidatos ); // ordena por custo
        $winner_mid = array_key_first( $candidatos );
    }

    // Separa preferida dos demais
    $preferred_key  = null;
    $preferred_rate = null;
    $others         = [];

    foreach ( $rates as $key => $rate ) {
        $meta   = method_exists( $rate, 'get_meta_data' ) ? $rate->get_meta_data() : [];
        $r_mid  = intval( $meta['melhorenvio_method_id'] ?? 0 );

        if ( $winner_mid && $r_mid === $winner_mid ) {
            // Injeta marcador nos meta_data do rate vencedor
            $rate->add_meta_data( 'tp_preferido', true );
            $preferred_key  = $key;
            $preferred_rate = $rate;
        } else {
            $others[ $key ] = $rate;
        }
    }

    // Ordena demais por preço
    uasort( $others, fn( $a, $b ) => $a->get_cost() <=> $b->get_cost() );

    if ( $preferred_rate ) {
        return array_merge( [ $preferred_key => $preferred_rate ], $others );
    }

    return $others;

}, 20, 2 );


// ─── Injeta marcador HTML no label para o JS aplicar o estilo ────────────────
add_filter( 'woocommerce_cart_shipping_method_full_label', function ( string $label, $method ) {

    $meta = method_exists( $method, 'get_meta_data' ) ? $method->get_meta_data() : [];

    if ( ! empty( $meta['tp_preferido'] ) ) {
        return '<span class="tp-label-preferida" data-tp-preferida="1">' . $label . '</span>';
    }

    return $label;

}, 10, 2 );


// ─── CSS + JS ─────────────────────────────────────────────────────────────────
add_action( 'wp_enqueue_scripts', function () {
    if ( ! is_checkout() && ! tp_is_funnelkit_step() ) {
        return;
    }

    wp_enqueue_style(
        'sz-tokens',
        TPC_URL . 'assets/css/senderzz-tokens.css',
        [],
        SENDERZZ_LOGISTICS_VERSION
    );
    wp_enqueue_style(
        'tp-preferida-checkout',
        TPC_URL . 'assets/checkout.css',
        [ 'sz-tokens' ],
        SENDERZZ_LOGISTICS_VERSION
    );
    wp_enqueue_style(
        'sz-typography',
        TPC_URL . 'assets/css/senderzz-typography.css',
        [ 'tp-preferida-checkout' ],
        SENDERZZ_LOGISTICS_VERSION
    );
    wp_enqueue_script(
        'tp-preferida-checkout',
        TP_PREFERIDA_URL . 'assets/checkout.js',
        [ 'jquery' ],
        SENDERZZ_LOGISTICS_VERSION,
        true
    );
} );


// ─────────────────────────────────────────────────────────────────────────────
// HELPERS
// ─────────────────────────────────────────────────────────────────────────────

/**
 * Retorna os method_ids permitidos (que concorrem) para as classes
 * de entrega presentes no package.
 *
 * @return int[]
 */
function tp_get_permitidas_for_package( array $package ): array {
    $map = get_option( TP_PREFERIDA_OPTION, [] );

    if ( empty( $map ) ) {
        return [];
    }

    $class_ids = [];

    foreach ( $package['contents'] as $item ) {
        $product = $item['data'];
        if ( ! $product || ! $product->needs_shipping() ) {
            continue;
        }
        $class_ids[] = intval( $product->get_shipping_class_id() ); // 0 = sem classe
    }

    $class_ids = array_unique( $class_ids );

    $permitidas = [];

    // Prioridade: primeiro class_id > 0 com configuração; depois fallback para 0
    foreach ( $class_ids as $cid ) {
        if ( $cid > 0 && ! empty( $map[ $cid ]['permitidas'] ) ) {
            $permitidas = (array) $map[ $cid ]['permitidas'];
            break;
        }
    }

    if ( empty( $permitidas ) && in_array( 0, $class_ids, true ) && ! empty( $map[0]['permitidas'] ) ) {
        $permitidas = (array) $map[0]['permitidas'];
    }

    if ( empty( $permitidas ) ) return [];

    // Remove métodos não habilitados nas zonas WC.
    $enabled_ids = function_exists( 'senderzz_get_enabled_me_method_ids' )
        ? senderzz_get_enabled_me_method_ids()
        : [];

    if ( ! empty( $enabled_ids ) ) {
        $permitidas = array_values( array_filter( $permitidas, fn( $mid ) => in_array( intval( $mid ), $enabled_ids, true ) ) );
    }

    // Remove métodos bloqueados por classe (SENDERZZ_BLOCKED_OPTION) — sobrescreve permitidas.
    $blocked_map = defined( 'SENDERZZ_BLOCKED_OPTION' ) ? get_option( SENDERZZ_BLOCKED_OPTION, [] ) : [];
    if ( ! empty( $blocked_map ) ) {
        $bloqueadas = [];
        foreach ( $class_ids as $cid ) {
            if ( $cid > 0 && ! empty( $blocked_map[ $cid ]['bloqueadas'] ) ) {
                $bloqueadas = array_map( 'intval', (array) $blocked_map[ $cid ]['bloqueadas'] );
                break;
            }
        }
        if ( empty( $bloqueadas ) && in_array( 0, $class_ids, true ) && ! empty( $blocked_map[0]['bloqueadas'] ) ) {
            $bloqueadas = array_map( 'intval', (array) $blocked_map[0]['bloqueadas'] );
        }
        if ( ! empty( $bloqueadas ) ) {
            $permitidas = array_values( array_filter( $permitidas, fn( $mid ) => ! in_array( intval( $mid ), $bloqueadas, true ) ) );
        }
    }

    // Remove métodos bloqueados de Azul Cargo Express e Mini Envios do conjunto permitido.
    // Isso garante que mesmo se um admin tiver selecionado esses IDs antes da regra existir,
    // eles não entrem na disputa no checkout.
    $blocked_companies = apply_filters( 'senderzz_blocked_carriers', [ 'Azul Cargo Express' ] );
    $blocked_services  = apply_filters( 'senderzz_blocked_services', [ 'Mini Envios' ] );

    if ( ! empty( $blocked_companies ) || ! empty( $blocked_services ) ) {
        $all_methods = function_exists( 'wc_melhor_envio_get_account_methods' )
            ? ( wc_melhor_envio_get_account_methods() ?: [] )
            : [];

        $permitidas = array_values( array_filter( $permitidas, function( $mid ) use ( $all_methods, $blocked_companies, $blocked_services ) {
            $mid = intval( $mid );
            if ( ! isset( $all_methods[ $mid ] ) ) return true; // desconhecido: deixa passar
            $m = $all_methods[ $mid ];
            $co = trim( (string) ( $m['company'] ?? '' ) );
            $sv = trim( (string) ( $m['name']    ?? '' ) );
            foreach ( $blocked_companies as $bc ) {
                if ( $co === $bc || stripos( $co, $bc ) !== false || stripos( $bc, $co ) !== false ) return false;
            }
            foreach ( $blocked_services as $bs ) {
                if ( $sv === $bs || stripos( $sv, $bs ) !== false || stripos( $bs, $sv ) !== false ) return false;
            }
            return true;
        } ) );
    }

    return $permitidas;
}

/**
 * Chave única para identificar um package no buffer.
 */
function tp_package_key( array $package ): string {
    $postcode = $package['destination']['postcode'] ?? '';
    $ids      = [];
    foreach ( $package['contents'] as $item ) {
        $ids[] = ( $item['product_id'] ?? '' ) . 'x' . ( $item['quantity'] ?? 1 );
    }
    return md5( $postcode . implode( ',', $ids ) );
}

/**
 * Detecta FunnelKit ativo.
 */
function tp_is_funnelkit_step(): bool {
    return class_exists( 'WFCO_Common' ) || function_exists( 'wfacp_get_checkout' );
}


// ─── Filtra transportadoras bloqueadas por classe no checkout ─────────────────
// Aplicado em woocommerce_package_rates ANTES da lógica de preferida (priority 15).
// Remove todos os rates cujo method_id estiver em bloqueadas para a classe.
add_filter( 'woocommerce_package_rates', function ( array $rates, $package ) {

    if ( empty( $rates ) ) return $rates;

    // Carrega mapa de bloqueadas
    $blocked_map = defined( 'SENDERZZ_BLOCKED_OPTION' ) ? get_option( SENDERZZ_BLOCKED_OPTION, [] ) : [];
    if ( empty( $blocked_map ) ) return $rates;

    // Descobre class_ids presentes no package (mesmo algoritmo da Preferida)
    $class_ids = [];
    foreach ( $package['contents'] as $item ) {
        $product = $item['data'];
        if ( ! $product || ! $product->needs_shipping() ) continue;
        $class_ids[] = intval( $product->get_shipping_class_id() );
    }
    $class_ids = array_unique( $class_ids );

    // Pega bloqueadas para a primeira classe configurada (prioridade maior) ou fallback 0
    $bloqueadas = [];
    foreach ( $class_ids as $cid ) {
        if ( $cid > 0 && ! empty( $blocked_map[ $cid ]['bloqueadas'] ) ) {
            $bloqueadas = array_map( 'intval', (array) $blocked_map[ $cid ]['bloqueadas'] );
            break;
        }
    }
    if ( empty( $bloqueadas ) && in_array( 0, $class_ids, true ) && ! empty( $blocked_map[0]['bloqueadas'] ) ) {
        $bloqueadas = array_map( 'intval', (array) $blocked_map[0]['bloqueadas'] );
    }
    if ( empty( $bloqueadas ) ) return $rates;

    // Remove rates cujo method_id está bloqueado
    foreach ( $rates as $key => $rate ) {
        $meta  = method_exists( $rate, 'get_meta_data' ) ? $rate->get_meta_data() : [];
        $r_mid = intval( $meta['melhorenvio_method_id'] ?? 0 );
        if ( $r_mid > 0 && in_array( $r_mid, $bloqueadas, true ) ) {
            unset( $rates[ $key ] );
        }
    }

    return $rates;

}, 15, 2 ); // priority 15 → roda antes do filtro de preferida (priority 20)


// ─── País sempre bloqueado + Estado bloqueado após CEP ────────────────────────
// Filtra o HTML renderizado pelo WooCommerce/FunnelKit para adicionar
// disabled + hidden input de backup nos campos de país (sempre) e estado (quando preenchido).

add_filter( 'woocommerce_form_field_select', 'senderzz_lock_country_state_field', 20, 4 );
function senderzz_lock_country_state_field( string $field, string $key, array $args, $value ): string {
    if ( ! is_checkout() && ! function_exists( 'wfacp_get_checkout' ) ) return $field;

    $lock_always = [ 'billing_country', 'shipping_country' ];
    $lock_if_val = [ 'billing_state', 'shipping_state' ];

    $should_lock = false;

    if ( in_array( $key, $lock_always, true ) ) {
        $should_lock = true;
    } elseif ( in_array( $key, $lock_if_val, true ) && ! empty( $value ) ) {
        $should_lock = true;
    }

    if ( ! $should_lock ) return $field;

    // Adiciona disabled no <select> e injeta hidden com o mesmo name para o valor ser enviado
    $field = preg_replace( '/<select\b/i', '<select disabled', $field, 1 );
    $hidden = '<input type="hidden" name="' . esc_attr( $key ) . '" value="' . esc_attr( $value ) . '">';
    $field .= $hidden;

    return $field;
}


// ─── Remove "(opcional)" do label do Complemento — WC padrão + FunnelKit ──────
add_filter( 'woocommerce_checkout_fields', function ( array $fields ): array {
    foreach ( [ 'billing', 'shipping' ] as $type ) {
        if ( isset( $fields[ $type ]['address_2']['label'] ) ) {
            $fields[ $type ]['address_2']['label'] = 'Complemento';
        }
        if ( isset( $fields[ $type ]['address_2']['placeholder'] ) ) {
            $fields[ $type ]['address_2']['placeholder'] = '';
        }
    }
    return $fields;
}, 20 );

// FunnelKit usa wfacp_checkout_fields
add_filter( 'wfacp_checkout_fields', function ( array $fields ): array {
    foreach ( [ 'billing', 'shipping' ] as $type ) {
        if ( isset( $fields[ $type ]['address_2']['label'] ) ) {
            $fields[ $type ]['address_2']['label'] = 'Complemento';
        }
    }
    return $fields;
}, 20 );

// Remove " (opcional)" de qualquer label via output buffer — cobre qualquer plugin
add_action( 'woocommerce_before_checkout_form', function () {
    ob_start( function ( $buffer ) {
        return str_replace( [ ' (opcional)', '(opcional)', ' (optional)', '(optional)' ], '', $buffer );
    } );
}, 1 );
add_action( 'woocommerce_after_checkout_form', function () {
    ob_end_flush();
}, 999 );


// ─── Força checkout limpo: zera TODOS os campos na sessão ao abrir ────────────
add_action( 'wp', function () {
    if ( ! function_exists( 'is_checkout' ) || ! is_checkout() || is_order_received_page() ) return;
    if ( ! WC()->session || ! WC()->customer ) return;
    if ( ! empty( $_POST ) ) return;

    $customer = WC()->customer;

    // Campos a zerar — inclui nome, email, telefone, endereço completo
    $fields_to_clear = [
        'billing_first_name', 'billing_last_name',
        'billing_company',    'billing_phone',
        'billing_email',
        'billing_address_1',  'billing_address_2',
        'billing_city',       'billing_state',
        'billing_postcode',   'billing_neighborhood',
        'shipping_first_name','shipping_last_name',
        'shipping_address_1', 'shipping_address_2',
        'shipping_city',      'shipping_state',
        'shipping_postcode',  'shipping_neighborhood',
    ];

    foreach ( $fields_to_clear as $field ) {
        $method = 'set_' . $field;
        if ( method_exists( $customer, $method ) ) {
            $customer->$method( '' );
        }
    }
    $customer->save();
}, 10 );
