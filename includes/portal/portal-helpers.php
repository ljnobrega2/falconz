<?php
/**
 * portal-helpers.php — Funções livres do portal Senderzz
 *
 * Extraídas de Portal_Page para eliminar acoplamento nos templates.
 * Cada função livre corresponde a um método privado da classe Portal_Page,
 * que agora é um wrapper de compatibilidade.
 *
 * Nomenclatura: senderzz_portal_{nome_original}()
 */

defined( 'ABSPATH' ) || exit;
if ( defined( 'SENDERZZ_PORTAL_HELPERS_LOADED' ) ) return;
define( 'SENDERZZ_PORTAL_HELPERS_LOADED', true );

// ── logo_url() ──
function senderzz_portal_logo_url(  ): string {

// 1) Procura por logo no filesystem (permite sobrescrever via upload).
$base_path = method_exists( '\WC_Melhor_Envio', 'get_plugin_path' )
    ? \WC_Melhor_Envio::get_plugin_path()
    : ( defined( 'SENDERZZ_PLUGIN_PATH' ) ? SENDERZZ_PLUGIN_PATH : plugin_dir_path( __FILE__ ) . '../../' );
$base_url  = method_exists( '\WC_Melhor_Envio', 'plugin_url' )
    ? \WC_Melhor_Envio::plugin_url()
    : plugins_url( '', __FILE__ );

$candidates = [
    'assets/images/senderzz-logo.svg',
    'assets/images/senderzz-logo.png',
    'assets/images/senderzz-logo.jpg',
    'assets/images/logo.svg',
    'assets/images/logo.png',
];
foreach ( $candidates as $rel ) {
    if ( file_exists( rtrim( $base_path, '/' ) . '/' . $rel ) ) {
        return rtrim( $base_url, '/' ) . '/' . $rel;
    }
}

// 2) Fallback: logo embedado em base64 (sempre funciona, sem dependência de arquivo).
return 'data:image/png;base64,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';
    
}

// ── carrier_methods() ──
function senderzz_portal_carrier_methods(  ): array {

if ( ! function_exists( 'wc_melhor_envio_get_account_methods' ) ) {
    return [];
}
$methods = wc_melhor_envio_get_account_methods();
if ( ! is_array( $methods ) ) return [];

// Filtra apenas métodos habilitados nas zonas de envio do WooCommerce
$enabled_ids = function_exists( 'senderzz_get_enabled_me_method_ids' )
    ? senderzz_get_enabled_me_method_ids()
    : [];

// IDs/nomes bloqueados — ajustável via filter
$blocked_companies = apply_filters( 'senderzz_blocked_carriers', [ 'Azul Cargo Express' ] );
$blocked_services  = apply_filters( 'senderzz_blocked_services', [ 'Mini Envios' ] ); // Correios Mini Envios

$grouped = [];
foreach ( $methods as $mid => $m ) {
    // Pula métodos não habilitados nas zonas WC
    if ( ! empty( $enabled_ids ) && ! in_array( intval( $mid ), $enabled_ids, true ) ) continue;

    $company = trim( (string) ( $m['company'] ?? '' ) );
    $service = trim( (string) ( $m['name']    ?? '' ) );

    // Bloqueia transportadora inteira
    // Bloqueia por match exato OU substring (cobre variações de nome da API do ME)
    $company_blocked = in_array( $company, $blocked_companies, true );
    if ( ! $company_blocked ) {
        foreach ( $blocked_companies as $bc ) {
            if ( stripos( $company, $bc ) !== false || stripos( $bc, $company ) !== false ) {
                $company_blocked = true; break;
            }
        }
    }
    if ( $company_blocked ) continue;
    // Bloqueia serviço específico (independente de transportadora)
    $service_blocked = in_array( $service, $blocked_services, true );
    if ( ! $service_blocked ) {
        foreach ( $blocked_services as $bs ) {
            if ( stripos( $service, $bs ) !== false || stripos( $bs, $service ) !== false ) {
                $service_blocked = true; break;
            }
        }
    }
    if ( $service_blocked ) continue;

    $label = $company ?: 'Outros';
    if ( ! isset( $grouped[ $label ] ) ) $grouped[ $label ] = [];
    $grouped[ $label ][ $mid ] = $m;
}

// Ordena transportadoras alfabeticamente; dentro de cada uma, por serviço
ksort( $grouped );
foreach ( $grouped as $carrier => &$svcs ) {
    uasort( $svcs, fn($a,$b) => strcmp( $a['name'] ?? '', $b['name'] ?? '' ) );
}
unset( $svcs );

return $grouped;
    
}

// ── carrier_methods_flat() ──
function senderzz_portal_carrier_methods_flat(  ): array {

$flat = [];
foreach ( senderzz_portal_carrier_methods() as $svcs ) {
    foreach ( $svcs as $mid => $m ) $flat[$mid] = $m;
}
return $flat;
    
}

// ── get_preferred_carrier_ids() ──
function senderzz_portal_preferred_carrier_ids(  int $class_id  ): array {

$map = get_option( defined('TP_PREFERIDA_OPTION') ? TP_PREFERIDA_OPTION : 'tp_preferida_map', [] );
$permitidas = $map[$class_id]['permitidas'] ?? [];
if ( ! is_array( $permitidas ) ) $permitidas = [];
return array_values( array_filter( array_map( 'strval', $permitidas ) ) );
    
}

// ── get_blocked_carrier_ids() ──
function senderzz_portal_blocked_carrier_ids(  int $class_id  ): array {

$option = defined('SENDERZZ_BLOCKED_OPTION') ? SENDERZZ_BLOCKED_OPTION : 'senderzz_blocked_carriers_map';
$map = get_option( $option, [] );
$bloqueadas = $map[$class_id]['bloqueadas'] ?? [];
if ( ! is_array( $bloqueadas ) ) $bloqueadas = [];
return array_values( array_filter( array_map( 'strval', $bloqueadas ) ) );
    
}

// ── senderzz_wallet_user_id_for_portal_user() ──
function senderzz_portal_wallet_user_id(  object $user  ): int {

// Carteira financeira sempre usa WP user_id; login do portal usa id próprio.
// Isso impede perda de saldo quando o e-mail do portal muda.
$wp_user_id = isset( $user->wp_user_id ) ? (int) $user->wp_user_id : 0;
if ( $wp_user_id && ( $linked_wp_user = get_user_by( 'id', $wp_user_id ) ) ) {
    $portal_email = sanitize_email( $user->email ?? '' );
    if ( $portal_email && strtolower( $portal_email ) !== strtolower( $linked_wp_user->user_email ) ) {
        // Não reverte o e-mail do portal. O portal é o login; o WP user_id é só o dono financeiro.
        // Se o novo e-mail estiver livre no WP, sincroniza. Se estiver ocupado, mantém o portal mesmo assim.
        update_user_meta( $wp_user_id, '_senderzz_previous_emails', array_values( array_unique( array_filter( array_merge(
            (array) get_user_meta( $wp_user_id, '_senderzz_previous_emails', true ),
            [ strtolower( sanitize_email( $linked_wp_user->user_email ) ), strtolower( $portal_email ) ]
        ) ) ) ) );
        $taken_wp = get_user_by( 'email', $portal_email );
        if ( ! $taken_wp || (int) $taken_wp->ID === (int) $wp_user_id ) {
            $updated = wp_update_user( [ 'ID' => $wp_user_id, 'user_email' => $portal_email ] );
            if ( ! is_wp_error( $updated ) ) clean_user_cache( $wp_user_id );
        }
    }
    if ( function_exists( 'senderzz_ensure_tpc_wallet' ) ) senderzz_ensure_tpc_wallet( $wp_user_id );
    if ( function_exists( 'senderzz_wallet_rebuild_from_transactions' ) ) senderzz_wallet_rebuild_from_transactions( $wp_user_id );
    return $wp_user_id;
}
if ( function_exists( 'senderzz_get_or_create_wp_user_for_portal_client' ) ) {
    $wp_user_id = (int) senderzz_get_or_create_wp_user_for_portal_client( $user );
    if ( $wp_user_id ) return $wp_user_id;
}
$email = sanitize_email( $user->email ?? '' );
$wp_user = $email ? get_user_by( 'email', $email ) : false;
if ( $wp_user ) {
    global $wpdb;
    $wpdb->update( $wpdb->prefix . Portal_Auth::TABLE, [ 'wp_user_id' => (int) $wp_user->ID ], [ 'id' => (int) $user->id ], [ '%d' ], [ '%d' ] );
    if ( function_exists( 'senderzz_ensure_tpc_wallet' ) ) senderzz_ensure_tpc_wallet( (int) $wp_user->ID );
    return (int) $wp_user->ID;
}
return 0;
    
}

// ── senderzz_get_order_tracking_codes() ──
function senderzz_portal_tracking_codes(  $order  ): array {

if ( ! $order || ! is_a( $order, 'WC_Order' ) ) return [];

// HPOS-safe: lê direto via WC_Order::get_meta(), que busca em wp_wc_orders_meta quando HPOS está ativo.
// Campo correto confirmado: _melhor_envio_tracking
$raw = $order->get_meta( '_melhor_envio_tracking', true );

if ( empty( $raw ) ) return [];

if ( is_array( $raw ) ) {
    $codes = $raw;
} else {
    $raw = trim( (string) $raw );
    $decoded = json_decode( $raw, true );
    if ( json_last_error() === JSON_ERROR_NONE && is_array( $decoded ) ) {
        $codes = $decoded;
    } else {
        $codes = preg_split( '/[\s,;|]+/', $raw );
    }
}

$codes = array_values( array_unique( array_filter( array_map( static function( $code ) {
    $code = strtoupper( trim( (string) $code ) );
    return preg_match( '/^[A-Z0-9\-]{6,}$/', $code ) ? $code : '';
}, $codes ) ) ) );

return $codes;
    
}

// ── senderzz_status_label() ──
function senderzz_portal_status_label(  string $status  ): string {

$status = sanitize_key( str_replace( 'wc-', '', $status ) );

$map = [
    'erro'              => 'Erro',
    'emcancelamento'    => 'Em cancelamento',
    'separado'          => 'Separado',
    'extravio'          => 'Extravio',
    'avariado'          => 'Avariado',
    'aprovado'          => 'Aprovado',
    'asuspender'        => 'A suspender',
    'emretirada'        => 'Em retirada',
    'acaminho'          => 'A caminho',
    'enviado'           => 'Enviado',
    'saldoinsuficiente' => 'Saldo insuficiente',
    'emdevolucao'       => 'Em devolução',
    'devolvido'         => 'Devolvido',
    'coletado'          => 'Coletado',
    'a-enviar-exp'      => 'A enviar',
    'pending'           => 'Pendente',
    'processing'        => 'Processando',
    'on-hold'           => 'Em espera',
    'completed'         => 'Concluído',
    'concluido'         => 'Concluído',
    'cancelled'         => 'Cancelado',
    'canceled'          => 'Cancelado',
    'cancelado'         => 'Cancelado',
    'refunded'          => 'Reembolsado',
    'failed'            => 'Falhou',
    'checkout-draft'    => 'Rascunho',
];

if ( isset( $map[ $status ] ) ) {
    return $map[ $status ];
}

return ucwords( str_replace( [ '_', '-' ], ' ', $status ) );
    
}

// ── status_class() — normaliza slug WC para classe CSS do badge ──
function senderzz_portal_status_class( string $status ): string {
    $status = sanitize_key( str_replace( 'wc-', '', $status ) );
    $map = [
        'cancelled'         => 'cancelado',
        'canceled'          => 'cancelado',
        'emcancelamento'    => 'emcancelamento',
        'pending'           => 'pendente',
        'on-hold'           => 'pendente',
        'processing'        => 'aprovado',
        'aprovado'          => 'aprovado',
        'completed'         => 'concluido',
        'concluido'         => 'concluido',
        'entregue'          => 'entregue',
        'enviado'           => 'enviado',
        'acaminho'          => 'acaminho',
        'emretirada'        => 'emretirada',
        'coletado'          => 'coletado',
        'embalado'          => 'embalado',
        'separado'          => 'separado',
        'erro'              => 'erro',
        'extravio'          => 'extravio',
        'avariado'          => 'avariado',
        'saldoinsuficiente' => 'saldoinsuficiente',
        'asuspender'        => 'asuspender',
        'refunded'          => 'concluido',
        'failed'            => 'erro',
    ];
    return $map[ $status ] ?? $status;
}

// ── money() ──
function senderzz_portal_money(  float $v  ): string {

return 'R$ ' . number_format($v, 2, ',', '.');
    
}

// ── public_wallet_description() ──
function senderzz_portal_wallet_description(  array $t  ): string {

$tipo = strtolower( (string) ( $t['tipo'] ?? '' ) );
$raw  = trim( wp_strip_all_tags( (string) ( $t['descricao'] ?? '' ) ) );

$order_ref = '';
if ( preg_match( '/#\s*(\d+)/', $raw, $m ) ) {
    $order_ref = ' - Pedido #' . $m[1];
} elseif ( preg_match( '/pedido\s*#?\s*(\d+)/i', $raw, $m ) ) {
    $order_ref = ' - Pedido #' . $m[1];
}

$reason = '';
$raw_lc = strtolower( remove_accents( $raw ) );
if ( strpos( $raw_lc, 'saldo insuficiente' ) !== false ) {
    $reason = ' | Motivo: saldo insuficiente';
} elseif ( strpos( $raw_lc, 'etiqueta' ) !== false || strpos( $raw_lc, 'pipeline' ) !== false ) {
    $reason = ' | Motivo: etiqueta não processada';
} elseif ( strpos( $raw_lc, 'cancel' ) !== false ) {
    $reason = ' | Motivo: cancelamento';
} elseif ( strpos( $raw_lc, 'estorno' ) !== false || strpos( $raw_lc, 'liberad' ) !== false ) {
    $reason = ' | Motivo: estorno liberado';
} elseif ( strpos( $raw_lc, 'administrativa' ) !== false || strpos( $raw_lc, 'melhor envio' ) !== false ) {
    $reason = ' | Motivo: operação logística';
}

if ( strpos( $raw_lc, 'alocacao' ) !== false || strpos( $raw_lc, 'alocação' ) !== false || strpos( $raw_lc, 'administrativa me' ) !== false ) {
    return 'Alocação logística' . $reason;
}

if ( $tipo === 'credito' ) {
    return 'Crédito na carteira' . $order_ref . $reason;
}

return 'Débito de frete' . $order_ref . $reason;
    
}

// ── ensure_checkout_links_table() ──
function senderzz_portal_ensure_checkout_links_table(  ): void {

global $wpdb;
$t = $wpdb->prefix . 'senderzz_checkout_links';

// Verifica se a tabela já existe — evita dbDelta silencioso
$exists = $wpdb->get_var( $wpdb->prepare( "SHOW TABLES LIKE %s", $t ) );
if ( $exists !== $t ) {
    $charset = $wpdb->get_charset_collate();
    $wpdb->query( "CREATE TABLE IF NOT EXISTS `{$t}` (
        `id`              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        `user_id`         BIGINT UNSIGNED NOT NULL DEFAULT 0,
        `post_id`         BIGINT UNSIGNED NOT NULL DEFAULT 0,
        `name`            VARCHAR(255) NOT NULL DEFAULT '',
        `slug`            VARCHAR(255) NOT NULL DEFAULT '',
        `url`             VARCHAR(512) NOT NULL DEFAULT '',
        `components_text` VARCHAR(512) NOT NULL DEFAULT '',
        `price_label`     VARCHAR(64)  NOT NULL DEFAULT '',
        `created_at`      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (`id`),
        KEY `user_id` (`user_id`),
        KEY `post_id` (`post_id`)
    ) {$charset};" );
}

// Adiciona colunas que possam faltar (upgrade silencioso)
$cols = $wpdb->get_col( "SHOW COLUMNS FROM `{$t}`", 0 );
if ( ! is_array( $cols ) ) return;
$defs = [
    'user_id'         => "BIGINT UNSIGNED NOT NULL DEFAULT 0",
    'post_id'         => "BIGINT UNSIGNED NOT NULL DEFAULT 0",
    'name'            => "VARCHAR(255) NOT NULL DEFAULT ''",
    'slug'            => "VARCHAR(255) NOT NULL DEFAULT ''",
    'url'             => "VARCHAR(512) NOT NULL DEFAULT ''",
    'components_text' => "VARCHAR(512) NOT NULL DEFAULT ''",
    'price_label'       => "VARCHAR(64) NOT NULL DEFAULT ''",
    'affiliate_visible' => "TINYINT(1) NOT NULL DEFAULT 0",
    'affiliate_commission_pct' => "DECIMAL(10,2) NOT NULL DEFAULT 0.00",
    'token'             => "VARCHAR(255) NOT NULL DEFAULT ''",
    'tipo'              => "VARCHAR(20) NOT NULL DEFAULT 'correio'",
    'link_motoboy_id'   => "BIGINT UNSIGNED NULL",
    'producer_id'       => "BIGINT UNSIGNED NOT NULL DEFAULT 0",
    'shipping_class_id' => "BIGINT UNSIGNED NOT NULL DEFAULT 0",
    'display_value'     => "DECIMAL(12,2) NOT NULL DEFAULT 0.00",
    'payload'           => "LONGTEXT NULL",
    'created_at'        => "DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP",
    'updated_at'        => "DATETIME NULL",
    'schema_version'    => "VARCHAR(32) NOT NULL DEFAULT ''",
];
foreach ( $defs as $col => $def ) {
    if ( ! in_array( $col, $cols, true ) ) {
        $wpdb->query( "ALTER TABLE `{$t}` ADD `{$col}` {$def}" );
    }
}
    
}

// ── calc_dashboard_metrics() ──
function senderzz_portal_calc_dashboard_metrics(  array $orders  ): array {

$receita = 0.0;
$frete = 0.0;
$cancelados = 0;
$enviados = 0;
$aprovados = 0;
$pendentes = 0;
$pedidos_atencao = 0;
$delivery_sum = 0.0;
$delivery_count = 0;
$by_day = [];
$by_status = [];
$by_region = [];
$by_carrier = [];
$attention_statuses = [ 'on-hold', 'saldoinsuficiente', 'erro', 'pendente_produtor', 'extravio', 'asuspender', 'emcancelamento', 'devolucao', 'devolução' ];
foreach ( $orders as $o ) {
    $total_bruto = isset($o['total_no_ship_raw']) ? (float)$o['total_no_ship_raw'] : (isset($o['total_raw']) ? max(0,(float)$o['total_raw'] - (float)($o['shipping_total_raw']??0)) : 0.0);
    $frete_val = isset($o['shipping_total_raw']) ? (float)$o['shipping_total_raw'] : 0.0;
    $receita += $total_bruto;
    $frete += $frete_val;
    $status = (string)($o['status'] ?? '');
    if ( in_array($status, ['enviado','emretirada','acaminho','coletado'], true) ) $enviados++;
    if ( $status === 'cancelled' ) $cancelados++;
    if ( $status === 'aprovado' ) $aprovados++;
    if ( $status === 'on-hold' ) $pendentes++;
    if ( in_array( $status, $attention_statuses, true ) ) $pedidos_atencao++;
    $delivery_time = trim( (string) ( $o['delivery_time'] ?? '' ) );
    if ( $delivery_time !== '' && preg_match('/(\d+(?:[\.,]\d+)?)/', $delivery_time, $mt ) ) {
        $delivery_sum += (float) str_replace(',', '.', $mt[1]);
        $delivery_count++;
    }
    $day = substr((string)($o['date_machine'] ?? ''), 0, 10);
    if ( $day ) $by_day[$day] = ($by_day[$day] ?? 0) + 1;
    $label = senderzz_portal_status_label( $status );
    $by_status[$label] = ($by_status[$label] ?? 0) + 1;
    $address = strtolower(wp_strip_all_tags($o['billing']['address'] ?? $o['shipping']['address'] ?? ''));
    $region = 'Outras regiões';
    foreach ( ['sp'=>'São Paulo','são paulo'=>'São Paulo','rj'=>'Rio de Janeiro','rio de janeiro'=>'Rio de Janeiro','mg'=>'Minas Gerais','minas gerais'=>'Minas Gerais','pr'=>'Paraná','sc'=>'Santa Catarina','rs'=>'Rio Grande do Sul','ba'=>'Bahia','pe'=>'Pernambuco','ce'=>'Ceará','go'=>'Goiás','df'=>'Distrito Federal'] as $needle => $name ) {
        if ( strpos($address, $needle) !== false ) { $region = $name; break; }
    }
    $by_region[$region] = ($by_region[$region] ?? 0) + 1;
    $carrier = trim((string)($o['shipping_name'] ?? $o['carrier'] ?? ''));
    if ($carrier === '') $carrier = 'Não informado';
    $carrier = preg_replace('/\s*[-–—]\s*.*/u', '', $carrier);
    $carrier = trim($carrier) ?: 'Não informado';
    $by_carrier[$carrier] = ($by_carrier[$carrier] ?? 0) + 1;
}
ksort($by_day);
arsort($by_status);
arsort($by_region);
arsort($by_carrier);
$total_orders = count($orders);
$ticket = $total_orders ? $receita / $total_orders : 0;
$frete_medio = $total_orders ? $frete / $total_orders : 0;
$prazo_medio = $delivery_count ? $delivery_sum / $delivery_count : 0;
return compact('receita','frete','ticket','frete_medio','prazo_medio','pedidos_atencao','cancelados','enviados','aprovados','pendentes','by_day','by_status','by_region','by_carrier');
    
}

// ── render_carrier_insights() ──
function senderzz_portal_render_carrier_insights(  array $rows, int $total  ): string {

if ( empty($rows) ) return '<p class="sz-hint">Sem transportadoras identificadas.</p>';
$h = '<div class="sz-insight-list sz-insight-carrier">';
foreach ( array_slice($rows, 0, 7) as $carrier => $count ) {
    $p = $total ? round(($count / $total) * 100) : 0;
    $h .= '<div class="sz-insight-row"><div class="sz-insight-main"><span class="sz-insight-truck">↗</span><strong>'.esc_html($carrier).'</strong></div><div class="sz-insight-meta"><b>'.$count.'</b><span>'.$p.'%</span></div><i><em style="width:'.$p.'%"></em></i></div>';
}
return $h.'</div>';
    
}

// ── render_order_status_cards() ──
function senderzz_portal_render_order_status_cards(  array $orders  ): string {

$map = [
    'on-hold' => [ 'label' => 'Aguardando aprovação', 'hint' => 'Pedidos esperando sua aprovação', 'class' => 'warn' ],
    'aprovado' => [ 'label' => 'Aprovados', 'hint' => 'Pedidos prontos para seguir', 'class' => 'ok' ],
    'a-enviar-exp' => [ 'label' => 'A enviar', 'hint' => 'Pedidos aguardando envio', 'class' => 'info' ],
    'saldoinsuficiente' => [ 'label' => 'Saldo insuficiente', 'hint' => 'Pedidos aguardando saldo', 'class' => 'warn' ],
    'erro' => [ 'label' => 'Erro', 'hint' => 'Pedidos que precisam de reprocessamento', 'class' => 'danger' ],
    'separado' => [ 'label' => 'Separados', 'hint' => 'Pedidos separados para envio', 'class' => 'info' ],
    'enviado' => [ 'label' => 'Enviados', 'hint' => 'Pedidos em transporte', 'class' => 'ok' ],
    'emretirada' => [ 'label' => 'Em retirada', 'hint' => 'Pedidos aguardando coleta', 'class' => 'info' ],
    'acaminho' => [ 'label' => 'A caminho', 'hint' => 'Pedidos a caminho do cliente', 'class' => 'info' ],
    'coletado' => [ 'label' => 'Coletados', 'hint' => 'Pedidos coletados pela transportadora', 'class' => 'ok' ],
    'completed' => [ 'label' => 'Concluídos', 'hint' => 'Pedidos finalizados com sucesso', 'class' => 'ok' ],
    'concluido' => [ 'label' => 'Concluídos', 'hint' => 'Pedidos finalizados com sucesso', 'class' => 'ok' ],
    'extravio' => [ 'label' => 'Extravio', 'hint' => 'Pedidos com ocorrência aberta', 'class' => 'danger' ],
    'asuspender' => [ 'label' => 'A suspender', 'hint' => 'Pedidos em análise operacional', 'class' => 'warn' ],
    'cancelled' => [ 'label' => 'Cancelados', 'hint' => 'Pedidos cancelados', 'class' => 'danger' ],
    'cancelado' => [ 'label' => 'Cancelados', 'hint' => 'Pedidos cancelados', 'class' => 'danger' ],
	'emcancelamento' => [ 'label' => 'Em cancelamento', 'hint' => 'Pedidos em cancelamento', 'class' => 'danger' ],
];

$counts = [];
foreach ( $orders as $o ) {
    $status = (string) ( $o['status'] ?? '' );
    if ( $status === '' ) continue;
    $counts[$status] = ( $counts[$status] ?? 0 ) + 1;
}

$cards = '';
foreach ( $map as $status => $info ) {
    $count = (int) ( $counts[$status] ?? 0 );
    if ( $count < 1 ) continue;

    $cards .= '<div class="sz-status-card '.esc_attr($info['class']).'">'
        . '<span>'.esc_html($info['label']).'</span>'
        . '<strong>'.esc_html((string)$count).'</strong>'
        . '<small>'.esc_html($info['hint']).'</small>'
        . '</div>';
}

return $cards === '' ? '' : '<div class="sz-status-card-grid">'.$cards.'</div>';
    
}

// ── render_status_bars() ──
function senderzz_portal_render_status_bars(  array $rows, int $total  ): string {

if ( empty($rows) ) return '<p class="sz-hint">Sem dados ainda.</p>';
$h = '<div class="sz-bars">';
foreach ( $rows as $label => $count ) {
    $p = $total ? round(($count / $total) * 100) : 0;
    $h .= '<div class="sz-bar-row"><div><span>'.esc_html($label).'</span><b>'.$count.'</b></div><i><em style="width:'.$p.'%"></em></i></div>';
}
return $h.'</div>';
    
}

// ── render_region_insights() ──
function senderzz_portal_render_region_insights(  array $rows, int $total  ): string {

if ( empty($rows) ) return '<p class="sz-hint">Sem regiões identificadas.</p>';
$h = '<div class="sz-insight-list sz-insight-region">';
foreach ( array_slice($rows, 0, 6) as $region => $count ) {
    $p = $total ? round(($count / $total) * 100) : 0;
    $h .= '<div class="sz-insight-row"><div class="sz-insight-main"><span class="sz-insight-dot"></span><strong>'.esc_html($region).'</strong></div><div class="sz-insight-meta"><b>'.$count.'</b><span>'.$p.'%</span></div><i><em style="width:'.$p.'%"></em></i></div>';
}
return $h.'</div>';
    
}
