<?php
/**
 * Senderzz — Notificações web app push para produtor
 * v136 — Sessão 5
 *
 * Eventos: pedido_feito, entregue, frustrado
 * Escopo: pedidos próprios do produtor + pedidos dos afiliados dele
 * Modo: por pedido (imediato)
 * Canal: Web Push via VAPID (service worker)
 */

if ( ! defined('ABSPATH') ) exit;

define('SZ_NOTIF_VERSION', '140');

// ── Tabelas ────────────────────────────────────────────────────────────────
function sz_notif_install(): void {
    global $wpdb;
    $charset = $wpdb->get_charset_collate();
    require_once ABSPATH . 'wp-admin/includes/upgrade.php';

    dbDelta("CREATE TABLE {$wpdb->prefix}sz_push_subscriptions (
        id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        user_id BIGINT UNSIGNED NOT NULL,
        endpoint TEXT NOT NULL,
        p256dh VARCHAR(512) NOT NULL,
        auth VARCHAR(255) NOT NULL,
        created_at DATETIME NOT NULL,
        PRIMARY KEY (id),
        KEY idx_user (user_id)
    ) {$charset};");

    dbDelta("CREATE TABLE {$wpdb->prefix}sz_notif_prefs (
        user_id BIGINT UNSIGNED NOT NULL,
        event VARCHAR(64) NOT NULL,
        enabled TINYINT(1) NOT NULL DEFAULT 1,
        include_affiliates TINYINT(1) NOT NULL DEFAULT 1,
        PRIMARY KEY (user_id, event)
    ) {$charset};");

    dbDelta("CREATE TABLE {$wpdb->prefix}sz_notif_log (
        id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        user_id BIGINT UNSIGNED NOT NULL,
        event VARCHAR(64) NOT NULL,
        recipient_type VARCHAR(24) NULL,
        order_id BIGINT UNSIGNED NULL,
        payload TEXT NULL,
        subscription_id BIGINT UNSIGNED NULL,
        status VARCHAR(24) NOT NULL DEFAULT 'queued',
        http_code INT NULL,
        response_text TEXT NULL,
        error_message TEXT NULL,
        sent_at DATETIME NOT NULL,
        PRIMARY KEY (id),
        KEY idx_user_event (user_id, event),
        KEY idx_status (status)
    ) {$charset};");

    $log_table = $wpdb->prefix . 'sz_notif_log';
    $cols = $wpdb->get_col( "DESC {$log_table}", 0 );
    $cols = is_array( $cols ) ? $cols : [];
    $maybe_cols = [
        'recipient_type'  => "ALTER TABLE {$log_table} ADD COLUMN recipient_type VARCHAR(24) NULL AFTER event",
        'subscription_id' => "ALTER TABLE {$log_table} ADD COLUMN subscription_id BIGINT UNSIGNED NULL AFTER payload",
        'status'          => "ALTER TABLE {$log_table} ADD COLUMN status VARCHAR(24) NOT NULL DEFAULT 'queued' AFTER subscription_id",
        'http_code'       => "ALTER TABLE {$log_table} ADD COLUMN http_code INT NULL AFTER status",
        'response_text'   => "ALTER TABLE {$log_table} ADD COLUMN response_text TEXT NULL AFTER http_code",
        'error_message'   => "ALTER TABLE {$log_table} ADD COLUMN error_message TEXT NULL AFTER response_text",
    ];
    foreach ( $maybe_cols as $col => $sql ) {
        if ( ! in_array( $col, $cols, true ) ) $wpdb->query( $sql );
    }
    update_option('sz_notif_db_version', SZ_NOTIF_VERSION);
}
add_action('init', function() {
    if ( get_option('sz_notif_db_version') !== SZ_NOTIF_VERSION ) sz_notif_install();
}, 5);

// ── Eventos a monitorar ───────────────────────────────────────────────────
$sz_notif_events = [
    // Cash On Delivery
    'agendamento_cod' => [ 'label' => '📅 Agendamento',      'wc_statuses' => ['wc-agendado'] ],
    'em_rota_cod'     => [ 'label' => '🏍️ Pedido Em Rota',   'wc_statuses' => ['wc-emrota','wc-em-rota','wc-a-caminho','wc-acaminho'] ],
    'completo_cod'    => [ 'label' => '✅ Pedido Completo',  'wc_statuses' => ['wc-completo'] ],
    'frustrado_cod'   => [ 'label' => '❌ Pedido Frustrado', 'wc_statuses' => ['wc-frustrado','wc-frustracao','wc-failed','wc-cancelled'] ],

    // Expedição
    'pedido_feito'    => [ 'label' => '🛒 Pedido Novo',      'wc_statuses' => ['wc-processing','wc-on-hold'] ],
    'enviado_pad'     => [ 'label' => '📦 Pedido Enviado',   'wc_statuses' => ['wc-enviado'] ],
    'entregue'        => [ 'label' => '✅ Pedido Entregue',  'wc_statuses' => ['wc-entregue'] ],
];

// ── Helpers ────────────────────────────────────────────────────────────────
function sz_notif_get_prefs( int $wp_user_id ): array {
    global $wpdb;
    $rows = $wpdb->get_results($wpdb->prepare(
        "SELECT event, enabled, include_affiliates FROM {$wpdb->prefix}sz_notif_prefs WHERE user_id=%d",
        $wp_user_id
    ), ARRAY_A) ?: [];
    $prefs = [];
    foreach ($rows as $r) $prefs[$r['event']] = $r;
    return $prefs;
}

function sz_notif_is_enabled( int $wp_user_id, string $event ): bool {
    $prefs = sz_notif_get_prefs($wp_user_id);
    if ( ! isset($prefs[$event]) ) return true; // padrão: habilitado
    return (bool) $prefs[$event]['enabled'];
}

function sz_notif_include_affiliates( int $wp_user_id, string $event ): bool {
    $prefs = sz_notif_get_prefs($wp_user_id);
    if ( ! isset($prefs[$event]) ) return true;
    return (bool) $prefs[$event]['include_affiliates'];
}


/**
 * Retorna se o pedido veio de afiliado. Usado para a preferência pessoal do produtor
 * "Incluir pedidos dos afiliados" falar mais alto que a configuração global do evento.
 */
function sz_notif_order_has_affiliate( WC_Order $order ): bool {
    $keys = [ '_sz_affiliate_id', '_sz_affiliate_ref', '_sz_affiliate_user_id', '_sz_aff_commission', '_sz_affiliate_commission_amount' ];
    foreach ( $keys as $key ) {
        $value = $order->get_meta( $key, true );
        if ( $value !== '' && $value !== null && $value !== false ) {
            if ( is_numeric( $value ) && (float) $value > 0 ) return true;
            if ( ! is_numeric( $value ) && trim( (string) $value ) !== '' ) return true;
        }
    }
    return false;
}

/**
 * Preferência final do destinatário. O que o usuário marca no PWA/Suporte prevalece
 * sobre a configuração global da notificação.
 */
function sz_notif_user_allows_event( int $wp_user_id, string $event, WC_Order $order, string $recipient_type = '' ): bool {
    if ( ! $wp_user_id ) return false;

    if ( ! sz_notif_is_enabled( $wp_user_id, $event ) ) {
        return false;
    }

    if ( $recipient_type === 'producer' && sz_notif_order_has_affiliate( $order ) && ! sz_notif_include_affiliates( $wp_user_id, $event ) ) {
        return false;
    }

    return true;
}

function sz_notif_producer_for_order( WC_Order $order ): int {
    // Tenta identificar o produtor pelo portal
    $producer_id = (int) $order->get_meta('_sz_aff_producer_id', true);
    if ( $producer_id ) return $producer_id;
    if ( function_exists('sz_aff_resolve_order_producer_id') ) {
        $producer_id = sz_aff_resolve_order_producer_id($order);
    }
    return (int) $producer_id;
}

function sz_notif_get_wp_user_id_for_portal_producer( int $producer_id ): int {
    global $wpdb;
    $producer_id = absint( $producer_id );
    if ( ! $producer_id ) return 0;

    // O producer_id pode chegar como ID da tabela senderzz_portal_users OU como WP user ID.
    $wp_id = (int) $wpdb->get_var( $wpdb->prepare(
        "SELECT wp_user_id FROM {$wpdb->prefix}senderzz_portal_users WHERE id=%d OR wp_user_id=%d ORDER BY id ASC LIMIT 1",
        $producer_id,
        $producer_id
    ) );
    if ( $wp_id ) return $wp_id;

    // Fallback seguro: se o ID existir no WP, usa ele diretamente.
    return get_user_by( 'id', $producer_id ) ? $producer_id : 0;
}

function sz_notif_normalize_wc_status( string $status ): string {
    $status = sanitize_key( $status );
    if ( $status === '' ) return '';
    $status = substr( $status, 0, 3 ) === 'wc-' ? $status : 'wc-' . $status;

    // Slugs legados/variações que apareceram no plugin em versões anteriores.
    $aliases = [
        'wc-em-rota'    => 'wc-emrota',
        'wc-sz-em-rota' => 'wc-emrota',
        'wc-acaminho'   => 'wc-a-caminho',
        'wc-concluido'  => 'wc-completo',
        'wc-completed'   => 'wc-completo',
        'wc-frustracao' => 'wc-frustrado',
    ];
    return $aliases[ $status ] ?? $status;
}

function sz_notif_status_variants( string $status ): array {
    $n = sz_notif_normalize_wc_status( $status );
    if ( $n === '' ) return [];
    $variants = [ $n ];
    $reverse_aliases = [
        'wc-emrota'     => [ 'wc-em-rota', 'wc-sz-em-rota' ],
        'wc-a-caminho'  => [ 'wc-acaminho' ],
        'wc-completo'   => [ 'wc-concluido', 'wc-completed' ],
        'wc-frustrado'  => [ 'wc-frustracao' ],
    ];
    foreach ( $reverse_aliases[ $n ] ?? [] as $v ) $variants[] = $v;
    return array_values( array_unique( array_filter( $variants ) ) );
}

function sz_notif_log_system( string $event, int $order_id, string $message, string $payload = '', string $recipient_type = 'system' ): void {
    sz_notif_log_delivery( 0, $event, $order_id, 0, 'skipped', 0, '', $message, $payload ?: '{}', $recipient_type );
}


function sz_notif_icon_url(): string {
    $icon = defined( 'TPC_URL' ) ? rtrim( TPC_URL, '/' ) . '/assets/images/senderzz-raio-192.png' : plugins_url( 'assets/images/senderzz-raio-192.png', dirname( __FILE__ ) );
    return esc_url_raw( $icon );
}

function sz_notif_app_name(): string {
    $name = trim( (string) get_option( 'sz_notif_app_name', 'Pedidos COD' ) );
    return $name !== '' ? $name : 'Pedidos COD';
}

// ── Disparar notificação ──────────────────────────────────────────────────
function sz_notif_fire( int $wp_user_id, string $event, WC_Order $order, string $recipient_type = '' ): void {
    global $wpdb;
    if ( ! sz_notif_is_enabled($wp_user_id, $event) ) return;

    $subs = $wpdb->get_results($wpdb->prepare(
        "SELECT * FROM {$wpdb->prefix}sz_push_subscriptions WHERE user_id=%d",
        $wp_user_id
    ), ARRAY_A);
    if ( empty($subs) ) {
        sz_notif_log_delivery($wp_user_id, $event, $order->get_id(), 0, 'failed', 0, '', 'Nenhum dispositivo inscrito para este usuário.', '{}', $recipient_type);
        return;
    }

    $event_labels = [
        'agendamento_cod' => '📅 Agendamento',
        'em_rota_cod'     => '🏍️ Pedido Em Rota',
        'completo_cod'    => '✅ Pedido Completo',
        'frustrado_cod'   => '❌ Pedido Frustrado',
        'pedido_feito'    => '🛒 Pedido Novo',
        'enviado_pad'     => '📦 Pedido Enviado',
        'entregue'        => '✅ Pedido Entregue',
        'frustrado'       => '❌ Pedido Frustrado',
        'admin_motoboy'   => '🏍️ Motoboy',
        'admin_expedicao' => '📦 Expedição',
        'admin_test'      => '🔔 Teste Senderzz',
    ];

    $title   = $event_labels[$event] ?? sz_notif_app_name();
    $total   = 'R$ ' . number_format((float)$order->get_total(), 2, ',', '.');
    $name    = $order->get_billing_first_name() . ' ' . $order->get_billing_last_name();
    $aff_id  = (int)($order->get_meta('_sz_affiliate_id',true) ?: $order->get_meta('_sz_affiliate_ref',true));
    $body    = trim($name) . ' · Pedido ' . $order->get_order_number() . ' · ' . $total . ($aff_id ? ' · via afiliado' : '');

    if ( function_exists( 'sz_app_pwa_apply_template' ) ) {
        $tpl_title = sz_app_pwa_apply_template( $event, $order, 'title', $wp_user_id, $recipient_type );
        $tpl_body  = sz_app_pwa_apply_template( $event, $order, 'body', $wp_user_id, $recipient_type );
        if ( $tpl_title !== '' ) $title = $tpl_title;
        if ( $tpl_body !== '' ) $body = $tpl_body;
    }

    $icon_url = sz_notif_icon_url();
    $payload = wp_json_encode([
        'title'   => $title,
        'body'    => $body,
        'icon'    => $icon_url,
        'badge'   => $icon_url,
        'image'   => '',
        'data'    => [ 'order_id' => $order->get_id(), 'url' => home_url('/meus-pedidos/') ],
    ]);

    foreach ($subs as $sub) {
        $result = sz_notif_send_push($sub, $payload);
        sz_notif_log_delivery(
            $wp_user_id,
            $event,
            (int) $order->get_id(),
            (int) ($sub['id'] ?? 0),
            (string) ($result['status'] ?? 'failed'),
            (int) ($result['http_code'] ?? 0),
            (string) ($result['response_text'] ?? ''),
            (string) ($result['error_message'] ?? ''),
            $payload,
            $recipient_type
        );
        if ( in_array( (int) ($result['http_code'] ?? 0), [404, 410], true ) && ! empty( $sub['id'] ) ) {
            $wpdb->delete( $wpdb->prefix . 'sz_push_subscriptions', [ 'id' => (int) $sub['id'] ], [ '%d' ] );
        }
    }
}

function sz_notif_log_delivery( int $user_id, string $event, int $order_id, int $subscription_id, string $status, int $http_code, string $response_text, string $error_message, string $payload, string $recipient_type = '' ): void {
    global $wpdb;
    $wpdb->insert("{$wpdb->prefix}sz_notif_log", [
        'user_id'         => $user_id,
        'event'           => $event,
        'recipient_type'  => $recipient_type ?: null,
        'order_id'        => $order_id ?: null,
        'payload'         => $payload,
        'subscription_id' => $subscription_id ?: null,
        'status'          => $status ?: 'failed',
        'http_code'       => $http_code ?: null,
        'response_text'   => mb_substr( $response_text, 0, 900 ),
        'error_message'   => mb_substr( $error_message, 0, 900 ),
        'sent_at'         => current_time('mysql'),
    ], ['%d','%s','%s','%d','%s','%d','%s','%d','%s','%s','%s']);
}

function sz_notif_send_push( array $sub, string $payload ): array {
    $vapid_public  = trim( (string) get_option( 'sz_notif_vapid_public',  '' ) );
    $vapid_private = trim( (string) get_option( 'sz_notif_vapid_private', '' ) );
    if ( ! $vapid_public || ! $vapid_private ) {
        return ['status'=>'failed','http_code'=>0,'response_text'=>'','error_message'=>'Chaves VAPID ausentes.'];
    }

    $endpoint = trim( (string) ( $sub['endpoint'] ?? '' ) );
    $p256dh   = trim( (string) ( $sub['p256dh']   ?? '' ) );
    $auth_key = trim( (string) ( $sub['auth']      ?? '' ) );
    if ( ! $endpoint ) {
        return ['status'=>'failed','http_code'=>0,'response_text'=>'','error_message'=>'Endpoint vazio.'];
    }
    if ( ! $p256dh || ! $auth_key ) {
        return ['status'=>'failed','http_code'=>0,'response_text'=>'','error_message'=>'Subscription sem p256dh/auth. Remova o dispositivo e ative novamente no PWA.'];
    }

    $origin = wp_parse_url( $endpoint, PHP_URL_SCHEME ) . '://' . wp_parse_url( $endpoint, PHP_URL_HOST );
    $jwt = sz_notif_build_vapid_jwt( $origin, $vapid_public, $vapid_private );
    if ( ! $jwt ) {
        return ['status'=>'failed','http_code'=>0,'response_text'=>'','error_message'=>'Não foi possível assinar o JWT VAPID. Regere as chaves e reative o celular.'];
    }

    $encrypted = sz_notif_encrypt_payload( $payload, $p256dh, $auth_key );
    if ( ! $encrypted || empty( $encrypted['body'] ) ) {
        return ['status'=>'failed','http_code'=>0,'response_text'=>'','error_message'=>'Falha ao criptografar payload Web Push. O endpoint pode estar corrompido; reative as notificações no celular.'];
    }

    $headers = [
        'Authorization'    => 'vapid t=' . $jwt . ', k=' . $vapid_public,
        'TTL'              => '86400',
        'Urgency'          => 'normal',
        'Content-Type'     => 'application/octet-stream',
        'Content-Encoding' => 'aes128gcm',
    ];

    $response = wp_remote_post( $endpoint, [
        'headers'   => $headers,
        'body'      => $encrypted['body'],
        'timeout'   => 15,
        'sslverify' => true,
    ] );

    if ( is_wp_error( $response ) ) {
        $msg = $response->get_error_message();
        if ( function_exists('senderzz_log') ) senderzz_log( 'push_error', [ 'endpoint' => substr($endpoint,0,80), 'error' => $msg ] );
        return ['status'=>'failed','http_code'=>0,'response_text'=>'','error_message'=>$msg];
    }

    $code = (int) wp_remote_retrieve_response_code( $response );
    $text = trim( (string) wp_remote_retrieve_body( $response ) );
    $ok = ($code >= 200 && $code < 300);

    $err = '';
    if ( ! $ok ) {
        $err = 'Push service retornou HTTP ' . $code;
        if ( $text !== '' ) $err .= ' — ' . mb_substr( $text, 0, 280 );
        if ( in_array( $code, [400, 401, 403], true ) ) {
            $err .= ' | Ação: no celular, desative e ative as notificações novamente para renovar a subscription com a chave VAPID atual.';
        }
    }

    return ['status'=>$ok ? 'sent' : 'failed','http_code'=>$code,'response_text'=>$text,'error_message'=>$err];
}

// ── Helpers VAPID / Web Push RFC8291 ─────────────────────────────────────
function sz_notif_base64url( string $data ): string {
    return rtrim( strtr( base64_encode( $data ), '+/', '-_' ), '=' );
}

function sz_notif_base64url_decode( string $data ): string {
    $data = trim( $data );
    $pad = strlen( $data ) % 4;
    if ( $pad ) $data .= str_repeat( '=', 4 - $pad );
    $decoded = base64_decode( strtr( $data, '-_', '+/' ), true );
    return $decoded === false ? '' : $decoded;
}

function sz_notif_build_vapid_jwt( string $audience, string $public_b64, string $private_b64 ): ?string {
    $private_raw = sz_notif_base64url_decode( $private_b64 );
    if ( strlen( $private_raw ) !== 32 ) return null;

    $pem = sz_notif_build_vapid_private_pem( $private_raw, $public_b64 );
    if ( ! $pem ) return null;

    $key = openssl_pkey_get_private( $pem );
    if ( ! $key ) return null;

    $header = sz_notif_base64url( wp_json_encode( [ 'typ' => 'JWT', 'alg' => 'ES256' ] ) );
    $claims = sz_notif_base64url( wp_json_encode( [
        'aud' => $audience,
        'exp' => time() + 43200,
        'sub' => 'mailto:' . get_option( 'admin_email' ),
    ] ) );
    $input = $header . '.' . $claims;

    $signature = '';
    if ( ! openssl_sign( $input, $signature, $key, OPENSSL_ALGO_SHA256 ) ) return null;
    $raw = sz_notif_der_to_raw_signature( $signature );
    if ( strlen( $raw ) !== 64 ) return null;
    return $input . '.' . sz_notif_base64url( $raw );
}

function sz_notif_build_vapid_private_pem( string $private_key_raw, string $vapid_public ): ?string {
    $public_key_raw = sz_notif_vapid_public_key_bytes( $vapid_public );
    if ( strlen( $private_key_raw ) !== 32 || strlen( $public_key_raw ) !== 65 || ord( $public_key_raw[0] ) !== 0x04 ) return null;

    $der = "\x30\x77"
         . "\x02\x01\x01"
         . "\x04\x20" . $private_key_raw
         . "\xA0\x0A\x06\x08\x2A\x86\x48\xCE\x3D\x03\x01\x07"
         . "\xA1\x44\x03\x42\x00" . $public_key_raw;

    return "-----BEGIN EC PRIVATE KEY-----\n"
        . chunk_split( base64_encode( $der ), 64, "\n" )
        . "-----END EC PRIVATE KEY-----\n";
}

function sz_notif_vapid_public_key_bytes( string $vapid_public ): string {
    $raw = sz_notif_base64url_decode( $vapid_public );
    if ( strlen( $raw ) === 64 ) $raw = "\x04" . $raw;
    return $raw;
}

function sz_notif_der_to_raw_signature( string $der ): string {
    $len = strlen( $der );
    if ( $len < 8 || ord($der[0]) !== 0x30 ) return '';
    $offset = 2;
    if ( ord($der[1]) === 0x81 ) $offset = 3;
    elseif ( ord($der[1]) === 0x82 ) $offset = 4;

    if ( $offset + 2 >= $len || ord($der[$offset]) !== 0x02 ) return '';
    $r_len = ord($der[$offset + 1]);
    $r = substr($der, $offset + 2, $r_len);
    $s_off = $offset + 2 + $r_len;
    if ( $s_off + 2 >= $len || ord($der[$s_off]) !== 0x02 ) return '';
    $s_len = ord($der[$s_off + 1]);
    $s = substr($der, $s_off + 2, $s_len);

    return sz_notif_pad32( $r ) . sz_notif_pad32( $s );
}

function sz_notif_pad32( string $val ): string {
    $val = ltrim( $val, "\x00" );
    if ( strlen( $val ) > 32 ) $val = substr( $val, -32 );
    return str_pad( $val, 32, "\x00", STR_PAD_LEFT );
}

function sz_notif_hkdf_expand( string $prk, string $info, int $length ): string {
    $okm = '';
    $previous = '';
    $i = 1;
    while ( strlen( $okm ) < $length ) {
        $previous = hash_hmac( 'sha256', $previous . $info . chr($i), $prk, true );
        $okm .= $previous;
        $i++;
    }
    return substr( $okm, 0, $length );
}

function sz_notif_encrypt_payload( string $payload, string $p256dh_b64, string $auth_b64 ): ?array {
    if ( ! function_exists('openssl_pkey_new') || ! function_exists('openssl_encrypt') ) return null;
    if ( ! in_array( 'aes-128-gcm', openssl_get_cipher_methods(), true ) ) return null;

    $receiver_key = sz_notif_base64url_decode( $p256dh_b64 );
    $auth_secret  = sz_notif_base64url_decode( $auth_b64 );
    if ( strlen( $receiver_key ) !== 65 || ord( $receiver_key[0] ) !== 0x04 || strlen( $auth_secret ) < 16 ) return null;

    $eph_key = openssl_pkey_new([
        'curve_name'       => 'prime256v1',
        'private_key_type' => OPENSSL_KEYTYPE_EC,
    ]);
    if ( ! $eph_key ) return null;

    $eph_details = openssl_pkey_get_details( $eph_key );
    $x = $eph_details['ec']['x'] ?? '';
    $y = $eph_details['ec']['y'] ?? '';
    if ( strlen($x) > 32 ) $x = substr($x, -32);
    if ( strlen($y) > 32 ) $y = substr($y, -32);
    $eph_pub_raw = "\x04" . str_pad($x, 32, "\x00", STR_PAD_LEFT) . str_pad($y, 32, "\x00", STR_PAD_LEFT);
    if ( strlen( $eph_pub_raw ) !== 65 ) return null;

    $receiver_pem = sz_notif_build_ec_public_pem( $receiver_key );
    if ( ! $receiver_pem ) return null;
    $receiver_pub = openssl_pkey_get_public( $receiver_pem );
    if ( ! $receiver_pub ) return null;

    $shared_secret = null;
    if ( function_exists( 'openssl_pkey_derive' ) ) {
        $shared_secret = openssl_pkey_derive( $receiver_pub, $eph_key, 32 );
    }
    if ( ! is_string( $shared_secret ) || strlen( $shared_secret ) < 32 ) return null;

    $salt = random_bytes(16);

    // RFC 8291 / RFC 8188 aes128gcm
    $prk_key = hash_hmac( 'sha256', $shared_secret, $auth_secret, true );
    $key_info = "WebPush: info\x00" . $receiver_key . $eph_pub_raw;
    $ikm = sz_notif_hkdf_expand( $prk_key, $key_info, 32 );

    $prk = hash_hmac( 'sha256', $ikm, $salt, true );
    $cek   = sz_notif_hkdf_expand( $prk, "Content-Encoding: aes128gcm\x00", 16 );
    $nonce = sz_notif_hkdf_expand( $prk, "Content-Encoding: nonce\x00", 12 );

    $record_size = 4096;
    $plain = $payload . "\x02";
    $tag = '';
    $ciphertext = openssl_encrypt( $plain, 'aes-128-gcm', $cek, OPENSSL_RAW_DATA, $nonce, $tag, '', 16 );
    if ( $ciphertext === false || strlen($tag) !== 16 ) return null;

    $body = $salt . pack('N', $record_size) . chr(65) . $eph_pub_raw . $ciphertext . $tag;
    return [ 'body' => $body ];
}

function sz_notif_build_ec_public_pem( string $raw_key ): ?string {
    if ( strlen($raw_key) !== 65 || ord($raw_key[0]) !== 0x04 ) return null;
    $alg = "\x30\x13\x06\x07\x2A\x86\x48\xCE\x3D\x02\x01\x06\x08\x2A\x86\x48\xCE\x3D\x03\x01\x07";
    $bit = "\x03\x42\x00" . $raw_key;
    $spki = "\x30" . chr( strlen($alg) + strlen($bit) ) . $alg . $bit;
    return "-----BEGIN PUBLIC KEY-----\n" . chunk_split( base64_encode($spki), 64, "\n" ) . "-----END PUBLIC KEY-----\n";
}

// ── Gerador de chaves VAPID no admin ─────────────────────────────────────
function sz_notif_generate_vapid_keys(): array {
    $key = openssl_pkey_new([
        'curve_name'       => 'prime256v1',
        'private_key_type' => OPENSSL_KEYTYPE_EC,
    ]);
    if ( ! $key ) return [];

    $details = openssl_pkey_get_details($key);
    $pub_x   = $details['ec']['x'] ?? '';
    $pub_y   = $details['ec']['y'] ?? '';
    $priv_d  = $details['ec']['d'] ?? '';
    if ( ! $pub_x || ! $priv_d ) return [];

    $pub_raw  = "" . str_pad($pub_x, 32, "", STR_PAD_LEFT) . str_pad($pub_y, 32, "", STR_PAD_LEFT);
    $priv_raw = str_pad($priv_d, 32, "", STR_PAD_LEFT);

    return [
        'public'  => sz_notif_base64url($pub_raw),
        'private' => sz_notif_base64url($priv_raw),
    ];
}

// ── Dispatcher central por destinatário configurado ───────────────────────
function sz_notif_dispatch_event_to_configured_recipients( string $event, WC_Order $order, int $wp_user_id, string $diagnostic_payload = '' ): void {
    $order_id = (int) $order->get_id();
    $recipients = function_exists( 'sz_app_pwa_get_recipients_for_event' )
        ? sz_app_pwa_get_recipients_for_event( $event )
        : [ 'producer' => 1, 'affiliate' => 1, 'admin' => 0 ];

    if ( ! empty( $recipients['producer'] ) ) {
        if ( function_exists( 'sz_notif_user_allows_event' ) && ! sz_notif_user_allows_event( $wp_user_id, $event, $order, 'producer' ) ) {
            $reason = sz_notif_order_has_affiliate( $order ) && ! sz_notif_include_affiliates( $wp_user_id, $event )
                ? 'Produtor não disparado: preferência do usuário/PWA bloqueia pedidos de afiliado.'
                : 'Produtor não disparado: preferência do usuário/PWA desativou este evento.';
            sz_notif_log_system( $event, $order_id, $reason, $diagnostic_payload, 'producer' );
        } else {
            sz_notif_fire( $wp_user_id, $event, $order, 'producer' );
        }
    } else {
        sz_notif_log_system( $event, $order_id, 'Produtor não disparado: destinatário produtor desativado para este evento.', $diagnostic_payload, 'producer' );
    }

    if ( ! empty( $recipients['affiliate'] ) ) {
        $aff_id = (int) ( $order->get_meta( '_sz_affiliate_id', true ) ?: $order->get_meta( '_sz_affiliate_ref', true ) );
        $aff_wp_id = 0;
        if ( $aff_id && function_exists( 'sz_aff_get_affiliate_row' ) ) {
            $aff_row   = sz_aff_get_affiliate_row( $aff_id );
            $aff_wp_id = (int) ( $aff_row['user_id'] ?? 0 );
        }
        if ( ! $aff_wp_id && function_exists( 'sz_app_pwa_affiliate_wp_user_id_for_order' ) ) {
            $aff_wp_id = (int) sz_app_pwa_affiliate_wp_user_id_for_order( $order );
        }
        if ( $aff_wp_id ) {
            if ( function_exists( 'sz_notif_user_allows_event' ) && ! sz_notif_user_allows_event( $aff_wp_id, $event, $order, 'affiliate' ) ) {
                sz_notif_log_system( $event, $order_id, 'Afiliado não disparado: preferência do usuário/PWA desativou este evento.', $diagnostic_payload, 'affiliate' );
            } else {
                sz_notif_fire( $aff_wp_id, $event, $order, 'affiliate' );
            }
        } else {
            sz_notif_log_system( $event, $order_id, 'Afiliado não disparado: evento permite afiliado, mas o pedido não possui afiliado/usuário vinculado.', $diagnostic_payload, 'affiliate' );
        }
    } else {
        sz_notif_log_system( $event, $order_id, 'Afiliado não disparado: destinatário afiliado desativado para este evento.', $diagnostic_payload, 'affiliate' );
    }

    if ( ! empty( $recipients['admin'] ) ) {
        $admin_ids = function_exists( 'sz_app_pwa_get_admin_recipients_for_event' ) ? sz_app_pwa_get_admin_recipients_for_event( $event ) : [];
        if ( ! empty( $admin_ids ) ) {
            foreach ( $admin_ids as $adm_wp ) {
                $adm_wp = (int) $adm_wp;
                if ( $adm_wp ) {
                    if ( function_exists( 'sz_notif_user_allows_event' ) && ! sz_notif_user_allows_event( $adm_wp, $event, $order, 'admin' ) ) {
                        sz_notif_log_system( $event, $order_id, 'Admin não disparado: preferência do usuário/PWA desativou este evento.', $diagnostic_payload, 'admin' );
                    } else {
                        sz_notif_fire( $adm_wp, $event, $order, 'admin' );
                    }
                }
            }
        } else {
            sz_notif_log_system( $event, $order_id, 'Admin não disparado: destinatário admin ativado, mas nenhum admin foi selecionado.', $diagnostic_payload, 'admin' );
        }
    } else {
        sz_notif_log_system( $event, $order_id, 'Admin não disparado: destinatário admin desativado para este evento.', $diagnostic_payload, 'admin' );
    }
}

// ── Hooks nos status do WooCommerce ──────────────────────────────────────
function sz_notif_on_status_change( int $order_id, string $old, string $new, WC_Order $order ): void {
    global $sz_notif_events;

    $new_wc          = sz_notif_normalize_wc_status( $new );
    $new_variants    = sz_notif_status_variants( $new );
    $old_wc          = sz_notif_normalize_wc_status( $old );
    $diagnostic_base = wp_json_encode( [
        'order_id'          => $order_id,
        'old_status'        => $old,
        'old_status_wc'     => $old_wc,
        'new_status'        => $new,
        'new_status_wc'     => $new_wc,
        'new_status_aliases'=> $new_variants,
    ] );

    if ( $new_wc === '' ) {
        sz_notif_log_system( 'status_change', $order_id, 'Status novo vazio; push automático ignorado.', $diagnostic_base );
        return;
    }

    $events_to_fire = [];
    $configured_status_map = [];
    $has_configured_status_map = function_exists( 'sz_app_pwa_get_notification_status_map' );

    // Mapa universal configurado no admin/PWA.
    // Regra: cada tipo de notificação aceita somente 1 status, e cada status só pode pertencer a 1 notificação.
    if ( $has_configured_status_map ) {
        $configured_status_map = sz_app_pwa_get_notification_status_map();
        foreach ( $configured_status_map as $event_key => $statuses ) {
            $normalized = [];
            foreach ( (array) $statuses as $status ) {
                foreach ( sz_notif_status_variants( (string) $status ) as $variant ) {
                    $normalized[] = $variant;
                }
            }
            $normalized = array_values( array_unique( array_filter( $normalized ) ) );
            if ( array_intersect( $new_variants, $normalized ) && isset( $sz_notif_events[ $event_key ] ) ) {
                $events_to_fire[ $event_key ] = $sz_notif_events[ $event_key ];
                break; // universal: um status dispara somente uma notificação operacional.
            }
        }
    } else {
        // Fallback técnico apenas se o módulo PWA não estiver carregado.
        foreach ( $sz_notif_events as $event_key => $cfg ) {
            $normalized = [];
            foreach ( (array) ( $cfg['wc_statuses'] ?? [] ) as $status ) {
                foreach ( sz_notif_status_variants( (string) $status ) as $variant ) {
                    $normalized[] = $variant;
                }
            }
            $normalized = array_values( array_unique( array_filter( $normalized ) ) );
            if ( array_intersect( $new_variants, $normalized ) ) {
                $events_to_fire[ $event_key ] = $cfg;
                break;
            }
        }
    }

    if ( empty( $events_to_fire ) ) {
        sz_notif_log_system( 'status_change', $order_id, 'Status recebido, mas nenhum evento de push está vinculado a este status.', $diagnostic_base );
        return;
    }

    $producer_id = sz_notif_producer_for_order( $order );
    if ( ! $producer_id && function_exists( 'sz_aff_resolve_order_producer_id' ) ) {
        $producer_id = (int) sz_aff_resolve_order_producer_id( $order );
    }
    if ( ! $producer_id ) {
        $producer_id = (int) $order->get_customer_id();
    }

    if ( ! $producer_id ) {
        sz_notif_log_system( implode( ',', array_keys( $events_to_fire ) ), $order_id, 'Evento encontrado, mas não foi possível identificar o produtor/dono do pedido.', $diagnostic_base );
        return;
    }

    $wp_user_id = sz_notif_get_wp_user_id_for_portal_producer( (int) $producer_id );
    if ( ! $wp_user_id ) {
        sz_notif_log_system( implode( ',', array_keys( $events_to_fire ) ), $order_id, 'Produtor identificado, mas não foi possível converter para WP user ID: ' . (int) $producer_id, $diagnostic_base );
        return;
    }

    foreach ( $events_to_fire as $event => $cfg ) {
        sz_notif_dispatch_event_to_configured_recipients( (string) $event, $order, (int) $wp_user_id, (string) $diagnostic_base );
    }
}
add_action('woocommerce_order_status_changed', 'sz_notif_on_status_change', 20, 4);

// ── REST: salvar/remover subscription + salvar prefs ─────────────────────
add_action('rest_api_init', function() {
    register_rest_route('sz-notif/v1', '/subscribe', [
        'methods'             => 'POST',
        'callback'            => 'sz_notif_rest_subscribe',
        'permission_callback' => '__return_true',
    ]);
    register_rest_route('sz-notif/v1', '/unsubscribe', [
        'methods'             => 'POST',
        'callback'            => 'sz_notif_rest_unsubscribe',
        'permission_callback' => '__return_true',
    ]);
    register_rest_route('sz-notif/v1', '/prefs', [
        'methods'             => 'POST',
        'callback'            => 'sz_notif_rest_save_prefs',
        'permission_callback' => '__return_true',
    ]);
});

function sz_notif_rest_get_user( WP_REST_Request $req ): ?int {
    // Método 1: cookie de sessão do portal (preferencial — funciona em todas as telas)
    if ( class_exists( '\WC_MelhorEnvio\Portal\Portal_Auth' ) ) {
        $portal_user = \WC_MelhorEnvio\Portal\Portal_Auth::get_current_user();
        if ( $portal_user ) {
            $wp_id = (int) ( $portal_user->wp_user_id ?? 0 );
            if ( ! $wp_id ) {
                global $wpdb;
                $wp_id = (int) $wpdb->get_var( $wpdb->prepare(
                    "SELECT wp_user_id FROM {$wpdb->prefix}senderzz_portal_users WHERE id=%d LIMIT 1",
                    (int) $portal_user->id
                ) );
            }
            if ( $wp_id ) return $wp_id;
        }
    }

    // Método 2: token de sessão passado via parâmetro n (fallback)
    $n = sanitize_text_field( (string) $req->get_param('n') );
    if ( $n && function_exists('sz_aff_get_portal_user_by_token') ) {
        $user = sz_aff_get_portal_user_by_token( $n );
        if ( $user ) {
            $wp_id = (int) ( $user->wp_user_id ?? 0 );
            if ( ! $wp_id ) {
                global $wpdb;
                $wp_id = (int) $wpdb->get_var( $wpdb->prepare(
                    "SELECT wp_user_id FROM {$wpdb->prefix}senderzz_portal_users WHERE id=%d LIMIT 1",
                    (int) $user->id
                ) );
            }
            if ( $wp_id ) return $wp_id;
        }
    }

    // Método 3: usuário WP logado diretamente
    $wp_user_id = get_current_user_id();
    return $wp_user_id ?: null;
}

function sz_notif_rest_subscribe( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $wp_user_id = sz_notif_rest_get_user($req);
    if ( ! $wp_user_id ) return new WP_REST_Response(['ok'=>false,'msg'=>'Sessão inválida.'],401);

    $endpoint = sanitize_text_field((string)$req->get_param('endpoint'));
    $p256dh   = sanitize_text_field((string)$req->get_param('p256dh'));
    $auth     = sanitize_text_field((string)$req->get_param('auth'));
    if ( ! $endpoint ) return new WP_REST_Response(['ok'=>false,'msg'=>'Endpoint obrigatório.'],400);

    // Upsert
    $exists = $wpdb->get_var($wpdb->prepare(
        "SELECT id FROM {$wpdb->prefix}sz_push_subscriptions WHERE user_id=%d AND endpoint=%s LIMIT 1",
        $wp_user_id, $endpoint
    ));
    if ( $exists ) {
        $wpdb->update("{$wpdb->prefix}sz_push_subscriptions",['p256dh'=>$p256dh,'auth'=>$auth],['id'=>(int)$exists],['%s','%s'],['%d']);
    } else {
        $wpdb->insert("{$wpdb->prefix}sz_push_subscriptions",[
            'user_id'=>$wp_user_id,'endpoint'=>$endpoint,'p256dh'=>$p256dh,'auth'=>$auth,'created_at'=>current_time('mysql')
        ],['%d','%s','%s','%s','%s']);
    }
    return new WP_REST_Response(['ok'=>true],200);
}

function sz_notif_rest_unsubscribe( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $wp_user_id = sz_notif_rest_get_user($req);
    if ( ! $wp_user_id ) return new WP_REST_Response(['ok'=>false],401);
    $endpoint = sanitize_text_field((string)$req->get_param('endpoint'));
    $wpdb->delete("{$wpdb->prefix}sz_push_subscriptions",['user_id'=>$wp_user_id,'endpoint'=>$endpoint],['%d','%s']);
    return new WP_REST_Response(['ok'=>true],200);
}

function sz_notif_rest_save_prefs( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $wp_user_id = sz_notif_rest_get_user($req);
    if ( ! $wp_user_id ) return new WP_REST_Response(['ok'=>false,'msg'=>'Sessão inválida.'],401);

    $prefs = $req->get_param('prefs'); // array [ event => { enabled, include_affiliates } ]
    if ( ! is_array($prefs) ) return new WP_REST_Response(['ok'=>false,'msg'=>'Formato inválido.'],400);

    global $sz_notif_events;
    foreach ($prefs as $event => $cfg) {
        $event = sanitize_text_field((string)$event);
        if ( ! array_key_exists($event, $sz_notif_events ?? ['pedido_feito'=>1,'entregue'=>1,'frustrado'=>1]) ) continue;
        $enabled    = isset($cfg['enabled'])            ? (int)(bool)$cfg['enabled']            : 1;
        $inc_aff    = isset($cfg['include_affiliates']) ? (int)(bool)$cfg['include_affiliates'] : 1;
        $wpdb->replace("{$wpdb->prefix}sz_notif_prefs",[
            'user_id'=>$wp_user_id,'event'=>$event,'enabled'=>$enabled,'include_affiliates'=>$inc_aff
        ],['%d','%s','%d','%d']);
    }
    return new WP_REST_Response(['ok'=>true,'msg'=>'Preferências salvas.'],200);
}

// ── Render painel de notificações no Suporte ─────────────────────────────
function sz_notif_render_prefs_panel( string $n, int $wp_user_id, bool $is_affiliate ): string {
    global $sz_notif_events, $wpdb;
    $prefs    = sz_notif_get_prefs($wp_user_id);
    $has_sub  = (bool)$wpdb->get_var($wpdb->prepare(
        "SELECT COUNT(*) FROM {$wpdb->prefix}sz_push_subscriptions WHERE user_id=%d", $wp_user_id
    ));
    $rest_sub  = rest_url('sz-notif/v1/subscribe');
    $rest_unsub= rest_url('sz-notif/v1/unsubscribe');
    $rest_prefs= rest_url('sz-notif/v1/prefs');

    $events_to_show = $sz_notif_events ?? [
        'pedido_feito' => ['label'=>'Pedido feito','icon'=>'🛒','desc'=>'Receba notificações quando um pedido for realizado.'],
        'entregue'     => ['label'=>'Pedido entregue','icon'=>'✓','desc'=>'Receba notificações quando um pedido for entregue.'],
        'frustrado'    => ['label'=>'Pedido frustrado','icon'=>'×','desc'=>'Receba notificações quando um pedido for frustrado.'],
    ];

    ob_start(); ?>
    <div class="sz-account-side-section" style="margin-top:16px" id="sz-notif-panel">
        <div class="sz-account-section-head">
            <div class="sz-account-section-head-left">
                <div class="sz-wh-ico sz-support-ico">🔔</div>
                <div>
                    <h3>Notificações push</h3>
                    <p>Receba alertas no navegador quando houver atividade nos pedidos.</p>
                </div>
            </div>
        </div>

        <div id="sz-notif-status" class="sz-notif-status-row">
            <span id="sz-notif-dot" class="sz-notif-dot" style="background:<?php echo $has_sub?'#E8650A':'#94a3b8'; ?>"></span>
            <span id="sz-notif-label" class="sz-notif-label"><?php echo $has_sub?'Notificações ativas':'Notificações desativadas'; ?></span>
            <div class="sz-notif-actions-inline"><button id="sz-notif-toggle-btn" class="sz-notif-toggle-btn">
                <?php echo $has_sub?'Desativar':'Ativar notificações'; ?>
            </button>
            <?php if ( ! $is_affiliate ) : ?>
            <?php $sz_any_include_aff = true; foreach ($prefs as $szp) { if (isset($szp['include_affiliates'])) { $sz_any_include_aff = (bool) $szp['include_affiliates']; break; } } ?>
            <label class="sz-notif-aff-global">
                <input type="checkbox" id="sz-notif-aff-global" <?php checked($sz_any_include_aff); ?>>
                <span class="sz-notif-aff-switch" aria-hidden="true"></span>
                <span class="sz-notif-aff-text">Incluir pedidos dos afiliados</span>
            </label>
            <?php endif; ?></div>
        </div>

        <div id="sz-notif-events" style="display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:12px;align-items:stretch">
            <?php foreach ($events_to_show as $event_key => $event_cfg):
                $pref    = $prefs[$event_key] ?? ['enabled'=>1,'include_affiliates'=>1];
                $enabled = (bool)($pref['enabled'] ?? 1);
                $inc_aff = (bool)($pref['include_affiliates'] ?? 1);
            ?>
            <?php
                $label = (string)($event_cfg['label'] ?? 'Pedido');
                $clean_label = trim(str_replace(['🛒','✅','❌'], '', $label));
                $icon = (string)($event_cfg['icon'] ?? ($event_key === 'entregue' ? '✓' : ($event_key === 'frustrado' ? '×' : '🛒')));
                $desc = (string)($event_cfg['desc'] ?? ($event_key === 'entregue' ? 'Receba notificações quando um pedido for entregue.' : ($event_key === 'frustrado' ? 'Receba notificações quando um pedido for frustrado.' : 'Receba notificações quando um pedido for realizado.')));
            ?>
            <div class="sz-notif-event-row" data-event="<?php echo esc_attr($event_key); ?>">
                <div class="sz-notif-card-main">
                    <span class="sz-notif-card-icon"><?php echo esc_html($icon); ?></span>
                    <div>
                        <strong><?php echo esc_html($clean_label); ?></strong>
                        <p><?php echo esc_html($desc); ?></p>
                    </div>
                </div>
                <label class="sz-notif-simple-switch sz-notif-card-toggle" aria-label="Ativar ou desativar notificação">
                    <input type="checkbox" class="sz-notif-event-check" data-event="<?php echo esc_attr($event_key); ?>"
                        <?php checked($enabled); ?>>
                    <span></span>
                </label>
            </div>
            <?php endforeach; ?>
        </div>


        <style>
        #sz-notif-panel{margin-top:18px!important;padding:22px 22px 18px!important;background:#fff!important;border:1px solid #e8edf4!important;border-radius:22px!important;box-shadow:none!important}
        #sz-notif-panel .sz-account-section-head{margin-bottom:16px!important}
        #sz-notif-status{margin:0 0 14px!important}
        #sz-notif-toggle-btn{height:34px!important;border-radius:11px!important;padding:0 14px!important;background:#E8650A!important;color:#fff!important;border:0!important;box-shadow:none!important}
        #sz-notif-events{display:grid!important;grid-template-columns:repeat(3,minmax(0,1fr))!important;gap:12px!important;align-items:stretch!important}
        #sz-notif-panel .sz-notif-event-row{background:#fff!important;border:1px solid #e8edf4!important;border-radius:18px!important;padding:18px!important;display:grid!important;grid-template-columns:1fr auto!important;grid-template-rows:auto auto!important;gap:14px!important;min-height:118px!important;box-shadow:none!important}
        #sz-notif-panel .sz-notif-card-main{display:flex!important;gap:14px!important;align-items:flex-start!important;min-width:0!important}
        #sz-notif-panel .sz-notif-card-icon{width:42px!important;height:42px!important;flex:0 0 42px!important;border-radius:14px!important;background:#fff1e8!important;color:#E8650A!important;display:flex!important;align-items:center!important;justify-content:center!important;font-size:var(--sz-text-lg);font-weight:700}
        #sz-notif-panel .sz-notif-card-main strong{display:block!important;font-size:var(--sz-text-md);font-weight:700;color:#111827!important;line-height:1.2}
        #sz-notif-panel .sz-notif-card-main p{margin:6px 0 0!important;font-size:var(--sz-text-meta);font-weight:700;line-height:1.35;color:#64748b!important}
        #sz-notif-panel .sz-notif-aff-label{grid-column:1/-1!important;display:flex!important;align-items:center!important;gap:7px!important;margin:0!important;color:#94a3b8!important;text-transform:none;letter-spacing:0;font-size:var(--sz-text-sm);font-weight:700;cursor:pointer!important}
        #sz-notif-panel .sz-notif-aff-label input{accent-color:#E8650A!important}
        #sz-notif-panel .sz-2fa-premium{width:auto!important;min-width:58px!important;margin-top:22px!important}
        #sz-notif-panel .sz-2fa-ui{width:58px!important;height:32px!important;border-radius:999px!important}
        #sz-notif-panel .sz-2fa-text{display:none!important}
        #sz-notif-panel .sz-2fa-dot{width:24px!important;height:24px!important;top:4px!important}
        #sz-notif-panel .sz-2fa-premium input:checked + .sz-2fa-ui .sz-2fa-dot{transform:translateX(26px)!important}

        .sz-notif-status-row{display:flex!important;align-items:center!important;gap:10px!important;margin:0 0 16px!important;flex-wrap:wrap!important}

        /* SENDERZZ v33 — dark premium final: notificações sem balão claro e ícone coerente */
        .sz-root.sz-dark #sz-notif-panel{background:linear-gradient(180deg,rgba(15,23,42,.98),rgba(9,17,31,.99))!important;border:1px solid rgba(255,255,255,.13)!important;color:#f8fafc!important;box-shadow:none!important}
        .sz-root.sz-dark #sz-notif-panel .sz-account-section-head-left .sz-wh-ico,
        .sz-root.sz-dark #sz-notif-panel .sz-wh-ico.sz-support-ico{background:rgba(232,101,10,.16)!important;color:#fb923c!important;border:1px solid rgba(232,101,10,.36)!important;width:48px!important;height:48px!important;min-width:48px!important;border-radius:16px!important;display:flex!important;align-items:center!important;justify-content:center!important;font-size:var(--sz-text-xl);line-height:1}
        .sz-root.sz-dark #sz-notif-panel .sz-account-section-head h3{color:#f8fafc!important}
        .sz-root.sz-dark #sz-notif-panel .sz-account-section-head p{color:#a8b3c7!important}
        .sz-root.sz-dark #sz-notif-panel .sz-notif-status-row{color:#cbd5e1!important}
        .sz-root.sz-dark #sz-notif-panel .sz-notif-aff-global{background:rgba(232,101,10,.10)!important;border:1px solid rgba(232,101,10,.42)!important;color:#f8fafc!important;border-radius:14px!important;padding:9px 12px!important;text-transform:none;letter-spacing:0;font-size:var(--sz-text-sm);font-weight:700}
        .sz-root.sz-dark #sz-notif-panel .sz-notif-aff-global input{accent-color:#E8650A!important}
        .sz-root.sz-dark #sz-notif-panel .sz-notif-event-row{background:linear-gradient(180deg,rgba(15,23,42,.98),rgba(10,18,32,.99))!important;border:1px solid rgba(255,255,255,.13)!important;color:#f8fafc!important;box-shadow:none!important;min-height:86px!important;padding:16px 18px!important;align-items:center!important}
        .sz-root.sz-dark #sz-notif-panel .sz-notif-card-icon{background:rgba(232,101,10,.16)!important;color:#fb923c!important;border:1px solid rgba(232,101,10,.22)!important}
        .sz-root.sz-dark #sz-notif-panel .sz-notif-card-main strong{color:#f8fafc!important}
        .sz-root.sz-dark #sz-notif-panel .sz-notif-card-main p{color:#cbd5e1!important}
        .sz-root.sz-dark #sz-notif-panel #sz-notif-toggle-btn{background:#E8650A!important;color:#fff!important}
        .sz-notif-dot{width:10px!important;height:10px!important;border-radius:50%!important;display:inline-block!important;flex:0 0 auto!important}
        .sz-notif-label{font-size:var(--sz-text-base);font-weight:700;color:var(--tx2,#64748b)!important}
        .sz-notif-toggle-btn{height:34px!important;border-radius:11px!important;padding:0 14px!important;background:#E8650A!important;color:#fff!important;border:0!important;box-shadow:none!important;font-size:var(--sz-text-meta);font-weight:700;cursor:pointer!important}
        .sz-notif-aff-global{margin-left:auto!important;height:34px!important;padding:0 14px!important;border:1px solid rgba(232,101,10,.35)!important;border-radius:12px!important;display:inline-flex!important;align-items:center!important;gap:8px!important;color:var(--tx2,#64748b)!important;font-size:var(--sz-text-sm);font-weight:700;text-transform:none;letter-spacing:0;background:rgba(232,101,10,.08)!important;cursor:pointer!important}
        .sz-notif-aff-global input{accent-color:#E8650A!important}
        .sz-root.sz-dark #sz-notif-panel{background:linear-gradient(180deg,rgba(17,25,40,.96),rgba(10,18,32,.98))!important;border-color:rgba(255,255,255,.13)!important;color:#f8fafc!important}
        .sz-root.sz-dark #sz-notif-panel .sz-account-section-head h3,.sz-root.sz-dark #sz-notif-panel .sz-notif-card-main strong{color:#f8fafc!important}
        .sz-root.sz-dark #sz-notif-panel .sz-account-section-head p,.sz-root.sz-dark #sz-notif-panel .sz-notif-card-main p,.sz-root.sz-dark #sz-notif-panel .sz-notif-label{color:#cbd5e1!important}
        .sz-root.sz-dark #sz-notif-panel .sz-notif-event-row{background:#0d1525!important;border-color:rgba(255,255,255,.13)!important}
        .sz-root.sz-dark #sz-notif-panel .sz-notif-card-icon{background:rgba(232,101,10,.14)!important;color:#fb923c!important}
        .sz-root.sz-dark #sz-notif-panel .sz-2fa-ui{background:#0a1220!important;border-color:rgba(255,255,255,.20)!important}
        @media(max-width:1100px){#sz-notif-events{grid-template-columns:1fr!important}.sz-notif-aff-global{margin-left:0!important;width:100%!important;justify-content:center!important}}

        /* SENDERZZ v160 — toggle afiliados sutil */
        #sz-notif-panel .sz-notif-aff-global{position:static!important;display:inline-flex!important;align-items:center!important;gap:10px!important;height:34px!important;margin:0!important;padding:0!important;border:0!important;border-radius:0!important;background:transparent!important;box-shadow:none!important;color:var(--sz-text-soft,#64748b)!important;font-size:var(--sz-text-base);font-weight:700;line-height:1;letter-spacing:0;text-transform:none;white-space:nowrap!important}
        #sz-notif-panel .sz-notif-aff-global input{appearance:none!important;-webkit-appearance:none!important;width:42px!important;height:24px!important;min-width:42px!important;flex:0 0 42px!important;margin:0!important;border:0!important;border-radius:999px!important;background:#e2e8f0!important;box-shadow:inset 0 0 0 1px rgba(148,163,184,.24)!important;position:relative!important;cursor:pointer!important;transition:background .18s ease,box-shadow .18s ease!important}
        #sz-notif-panel .sz-notif-aff-global input:before{content:""!important;position:absolute!important;width:18px!important;height:18px!important;left:3px!important;top:3px!important;border-radius:999px!important;background:#fff!important;box-shadow:0 2px 6px rgba(15,23,42,.16)!important;transform:none!important;transition:transform .18s ease!important}
        #sz-notif-panel .sz-notif-aff-global input:checked{background:#f97316!important;box-shadow:inset 0 0 0 1px rgba(249,115,22,.18)!important}
        #sz-notif-panel .sz-notif-aff-global input:checked:before{transform:translateX(18px)!important}
        .sz-root.sz-dark #sz-notif-panel .sz-notif-aff-global{color:var(--sz-text,#f8fafc)!important;background:transparent!important;border:0!important}
        .sz-root.sz-dark #sz-notif-panel .sz-notif-aff-global input{background:rgba(148,163,184,.24)!important;box-shadow:inset 0 0 0 1px rgba(255,255,255,.10)!important}
        .sz-root.sz-dark #sz-notif-panel .sz-notif-aff-global input:checked{background:#f97316!important}


        /* SENDERZZ v162 — toggles simples e sutis nas notificações */
        #sz-notif-panel .sz-notif-card-toggle.sz-notif-simple-switch{margin:0!important;align-self:center!important;justify-self:end!important;width:42px!important;height:24px!important;min-width:42px!important;display:inline-flex!important;align-items:center!important;position:relative!important;cursor:pointer!important}
        #sz-notif-panel .sz-notif-simple-switch input{position:absolute!important;opacity:0!important;pointer-events:none!important;width:1px!important;height:1px!important}
        #sz-notif-panel .sz-notif-simple-switch span{width:42px!important;height:24px!important;border-radius:999px!important;background:#e2e8f0!important;border:1px solid rgba(148,163,184,.28)!important;box-shadow:none!important;display:block!important;position:relative!important;transition:background .18s ease,border-color .18s ease!important}
        #sz-notif-panel .sz-notif-simple-switch span:before{content:""!important;position:absolute!important;width:18px!important;height:18px!important;left:2px!important;top:2px!important;border-radius:999px!important;background:#fff!important;box-shadow:0 1px 4px rgba(15,23,42,.16)!important;transition:transform .18s ease!important}
        #sz-notif-panel .sz-notif-simple-switch input:checked + span{background:#E8650A!important;border-color:#E8650A!important}
        #sz-notif-panel .sz-notif-simple-switch input:checked + span:before{transform:translateX(18px)!important}
        #sz-notif-panel .sz-notif-aff-global{gap:9px!important;font-size:var(--sz-text-base);font-weight:700;color:var(--sz-text-soft,#64748b)!important}
        #sz-notif-panel .sz-notif-aff-global input{width:42px!important;height:24px!important;min-width:42px!important;background:#e2e8f0!important;border:1px solid rgba(148,163,184,.28)!important;box-shadow:none!important}
        #sz-notif-panel .sz-notif-aff-global input:before{width:18px!important;height:18px!important;left:2px!important;top:2px!important;box-shadow:0 1px 4px rgba(15,23,42,.16)!important}
        #sz-notif-panel .sz-notif-aff-global input:checked{background:#E8650A!important;border-color:#E8650A!important;box-shadow:none!important}
        #sz-notif-panel .sz-notif-aff-global input:checked:before{transform:translateX(18px)!important}
        .sz-root.sz-dark #sz-notif-panel .sz-notif-simple-switch span,
        .sz-root.sz-dark #sz-notif-panel .sz-notif-aff-global input{background:rgba(148,163,184,.24)!important;border-color:rgba(255,255,255,.12)!important}
        .sz-root.sz-dark #sz-notif-panel .sz-notif-simple-switch input:checked + span,
        .sz-root.sz-dark #sz-notif-panel .sz-notif-aff-global input:checked{background:#E8650A!important;border-color:#E8650A!important}


        /* SENDERZZ v163 — usar o mesmo toggle simples no incluir pedidos dos afiliados */
        #sz-notif-panel .sz-notif-aff-global{
            display:inline-flex!important;align-items:center!important;gap:10px!important;margin:0!important;padding:0!important;
            height:24px!important;border:0!important;background:transparent!important;box-shadow:none!important;border-radius:0!important;
            color:var(--sz-text-soft,#64748b)!important;font-size:var(--sz-text-base);font-weight:700;line-height:1;
            text-transform:none;letter-spacing:0;white-space:nowrap!important;cursor:pointer!important;
        }
        #sz-notif-panel .sz-notif-aff-global input{
            position:absolute!important;opacity:0!important;pointer-events:none!important;width:1px!important;height:1px!important;margin:0!important;
            appearance:none!important;-webkit-appearance:none!important;
        }
        #sz-notif-panel .sz-notif-aff-switch{
            width:42px!important;height:24px!important;min-width:42px!important;border-radius:999px!important;background:#e2e8f0!important;
            border:1px solid rgba(148,163,184,.28)!important;box-shadow:none!important;display:block!important;position:relative!important;
            transition:background .18s ease,border-color .18s ease!important;
        }
        #sz-notif-panel .sz-notif-aff-switch:before{
            content:""!important;position:absolute!important;width:18px!important;height:18px!important;left:2px!important;top:2px!important;
            border-radius:999px!important;background:#fff!important;box-shadow:0 1px 4px rgba(15,23,42,.16)!important;transition:transform .18s ease!important;
        }
        #sz-notif-panel .sz-notif-aff-global input:checked + .sz-notif-aff-switch{background:#E8650A!important;border-color:#E8650A!important}
        #sz-notif-panel .sz-notif-aff-global input:checked + .sz-notif-aff-switch:before{transform:translateX(18px)!important}
        #sz-notif-panel .sz-notif-aff-text{display:inline-block!important;color:inherit!important;font:inherit!important;line-height:1}
        .sz-root.sz-dark #sz-notif-panel .sz-notif-aff-global{color:var(--sz-text,#f8fafc)!important;background:transparent!important;border:0!important;padding:0!important}
        .sz-root.sz-dark #sz-notif-panel .sz-notif-aff-switch{background:rgba(148,163,184,.24)!important;border-color:rgba(255,255,255,.12)!important}
        .sz-root.sz-dark #sz-notif-panel .sz-notif-aff-global input:checked + .sz-notif-aff-switch{background:#E8650A!important;border-color:#E8650A!important}

        
        /* SENDERZZ v169 — toggle afiliados sem sombra/bloco */
        #sz-notif-panel .sz-notif-aff-global{display:inline-flex!important;align-items:center!important;gap:9px!important;margin:0!important;padding:0!important;height:auto!important;border:0!important;background:transparent!important;background-image:none!important;box-shadow:none!important;filter:none!important;border-radius:0!important;color:var(--sz-text-soft,#64748b)!important;font-size:var(--sz-text-base);font-weight:700;letter-spacing:0;text-transform:none;line-height:1;white-space:nowrap!important}
        #sz-notif-panel .sz-notif-aff-global input{position:absolute!important;opacity:0!important;pointer-events:none!important;width:1px!important;height:1px!important;min-width:1px!important;margin:0!important;padding:0!important;appearance:none!important;-webkit-appearance:none!important;box-shadow:none!important}
        #sz-notif-panel .sz-notif-aff-switch{display:inline-block!important;width:42px!important;height:24px!important;min-width:42px!important;flex:0 0 42px!important;position:relative!important;border-radius:999px!important;background:#e2e8f0!important;border:1px solid rgba(148,163,184,.30)!important;box-shadow:none!important;filter:none!important;transition:background .18s ease,border-color .18s ease!important}
        #sz-notif-panel .sz-notif-aff-switch:before{content:""!important;position:absolute!important;width:18px!important;height:18px!important;left:2px!important;top:2px!important;border-radius:999px!important;background:#fff!important;box-shadow:0 1px 3px rgba(15,23,42,.16)!important;transform:none!important;transition:transform .18s ease!important}
        #sz-notif-panel .sz-notif-aff-global input:checked + .sz-notif-aff-switch{background:#E8650A!important;border-color:#E8650A!important;box-shadow:none!important}
        #sz-notif-panel .sz-notif-aff-global input:checked + .sz-notif-aff-switch:before{transform:translateX(18px)!important}
        #sz-notif-panel .sz-notif-aff-text{display:inline-block!important;background:transparent!important;box-shadow:none!important;border:0!important;color:inherit!important;font:inherit!important;line-height:1}
        .sz-root.sz-dark #sz-notif-panel .sz-notif-aff-global{background:transparent!important;box-shadow:none!important;color:#cbd5e1!important}
        .sz-root.sz-dark #sz-notif-panel .sz-notif-aff-switch{background:rgba(148,163,184,.24)!important;border-color:rgba(255,255,255,.14)!important;box-shadow:none!important}
        .sz-root.sz-dark #sz-notif-panel .sz-notif-aff-global input:checked + .sz-notif-aff-switch{background:#E8650A!important;border-color:#E8650A!important}
</style>
        <script>
        (function(){
            var n       = <?php echo wp_json_encode($n); ?>;
            var restSub = <?php echo wp_json_encode($rest_sub); ?>;
            var restUns = <?php echo wp_json_encode($rest_unsub); ?>;
            var restPrf = <?php echo wp_json_encode($rest_prefs); ?>;
            var hasSub  = <?php echo $has_sub ? 'true' : 'false'; ?>;
            var swReg   = null;

            function szNotifSavePrefs() {
                var prefs = {};
                var affGlobal = document.getElementById('sz-notif-aff-global');
                var includeAffiliates = affGlobal ? (affGlobal.checked ? 1 : 0) : 1;
                document.querySelectorAll('.sz-notif-event-check').forEach(function(cb) {
                    var evt = cb.getAttribute('data-event');
                    prefs[evt] = {
                        enabled: cb.checked ? 1 : 0,
                        include_affiliates: includeAffiliates
                    };
                });
                fetch(restPrf, {
                    method:'POST', credentials:'same-origin',
                    headers:{'Content-Type':'application/json'},
                    body: JSON.stringify({ n: n, prefs: prefs })
                }).then(function(r){ return r.json(); }).then(function(d){
                    if (window.szToast) szToast(d.msg || (d.ok?'Salvo!':'Erro'), d.ok?'success':'error', 2500);
                });
            }

            // Salvar prefs ao mudar qualquer toggle
            document.querySelectorAll('.sz-notif-event-check, #sz-notif-aff-global').forEach(function(cb) {
                cb.addEventListener('change', szNotifSavePrefs);
            });

            // Ativar/desativar notificações push
            var btn = document.getElementById('sz-notif-toggle-btn');
            if (btn) btn.addEventListener('click', function() {
                if (hasSub) {
                    // Desativar
                    if ('serviceWorker' in navigator) {
                        navigator.serviceWorker.ready.then(function(reg) {
                            return reg.pushManager.getSubscription();
                        }).then(function(sub) {
                            if (sub) {
                                return sub.unsubscribe().then(function() {
                                    return fetch(restUns, {
                                        method:'POST', credentials:'same-origin',
                                        headers:{'Content-Type':'application/json'},
                                        body: JSON.stringify({ n: n, endpoint: sub.endpoint })
                                    });
                                });
                            }
                        }).then(function() {
                            hasSub = false;
                            document.getElementById('sz-notif-dot').style.background = '#94a3b8';
                            document.getElementById('sz-notif-label').textContent = 'Notificações desativadas';
                            btn.textContent = 'Ativar notificações';
                            btn.style.background = '#E8650A'; btn.style.color = '#fff'; btn.style.border = '0';
                            if (window.szToast) szToast('Notificações desativadas','success',2000);
                        });
                    }
                } else {
                    // Ativar
                    if (!('Notification' in window)) {
                        if(window.szToast) szToast('Seu navegador não suporta notificações.','error'); return;
                    }
                    Notification.requestPermission().then(function(perm) {
                        if (perm !== 'granted') {
                            if(window.szToast) szToast('Permissão negada para notificações.','error'); return;
                        }
                        if ('serviceWorker' in navigator) {
                            navigator.serviceWorker.register('<?php echo esc_url( plugins_url( 'assets/js/sz-sw.js', dirname( __DIR__ ) . '/senderzz-logistics.php' ) ); ?>')
                            .then(function(reg) {
                                swReg = reg;
                                var vapidKey = <?php echo wp_json_encode(get_option('sz_notif_vapid_public','')); ?>;
                                if (!vapidKey) {
                                    if(window.szToast) szToast('Serviço de push não configurado. Fale com o suporte.','error',4000);
                                    return;
                                }
                                return reg.pushManager.subscribe({
                                    userVisibleOnly: true,
                                    applicationServerKey: szNotifUrlBase64ToUint8Array(vapidKey)
                                }).then(function(sub) {
                                    var json = sub.toJSON();
                                    return fetch(restSub, {
                                        method:'POST', credentials:'same-origin',
                                        headers:{'Content-Type':'application/json'},
                                        body: JSON.stringify({ n:n, endpoint:json.endpoint, p256dh:json.keys.p256dh, auth:json.keys.auth })
                                    });
                                });
                            }).then(function() {
                                hasSub = true; updateBtn();
                                if(window.szToast) szToast('Notificações ativadas!','success',2500);
                            }).catch(function(err) {
                                if(window.szToast) szToast('Erro ao ativar: '+err.message,'error');
                            });
                        }
                    });
                }
            });

            function updateBtn() {
                var b = document.getElementById('sz-notif-toggle-btn');
                var d = document.getElementById('sz-notif-dot');
                var l = document.getElementById('sz-notif-label');
                if (!b) return;
                if (hasSub) {
                    d.style.background='#E8650A'; l.textContent='Notificações ativas';
                    b.textContent='Desativar'; b.style.background='transparent'; b.style.color='#b91c1c'; b.style.border='1px solid #fca5a5';
                } else {
                    d.style.background='#94a3b8'; l.textContent='Notificações desativadas';
                    b.textContent='Ativar notificações'; b.style.background='#E8650A'; b.style.color='#fff'; b.style.border='0';
                }
            }

            function szNotifUrlBase64ToUint8Array(base64String) {
                var padding = '='.repeat((4 - base64String.length % 4) % 4);
                var base64  = (base64String + padding).replace(/-/g,'+').replace(/_/g,'/');
                var rawData = window.atob(base64);
                var outputArray = new Uint8Array(rawData.length);
                for (var i=0; i<rawData.length; ++i) outputArray[i] = rawData.charCodeAt(i);
                return outputArray;
            }
        })();
        </script>
    </div>
    <?php return ob_get_clean();
}


// ── Auto-gerar chaves VAPID se não existirem ─────────────────────────────
add_action('init', function(): void {
    if ( get_option('sz_notif_vapid_public') ) return;
    $keys = sz_notif_generate_vapid_keys();
    if ( $keys ) {
        update_option('sz_notif_vapid_public',  $keys['public'],  false);
        update_option('sz_notif_vapid_private', $keys['private'], false);
    }
}, 15);

// ── Painel admin: Notificações ────────────────────────────────────────────
add_filter('senderzz_admin_tabs', function(array $tabs): array {
    $tabs['notifications'] = 'Notificações Push';
    return $tabs;
});

function sz_notif_admin_page(): void {
    // Salvar ação de regenerar chaves
    if ( isset($_POST['sz_notif_regen']) && check_admin_referer('sz_notif_regen') ) {
        $keys = sz_notif_generate_vapid_keys();
        if ($keys) {
            update_option('sz_notif_vapid_public',  $keys['public'],  false);
            update_option('sz_notif_vapid_private', $keys['private'], false);
            echo '<div class="notice notice-success"><p>Novas chaves VAPID geradas. Todos os dispositivos precisarão reativar as notificações.</p></div>';
        }
    }

    if ( isset($_POST['sz_notif_reprocess']) && check_admin_referer('sz_notif_reprocess') ) {
        $order_id = absint($_POST['order_id'] ?? 0);
        $event = sanitize_key((string)($_POST['event'] ?? ''));
        $order = $order_id && function_exists('wc_get_order') ? wc_get_order($order_id) : null;
        if ($order && $event) {
            $producer_portal_id = sz_notif_producer_for_order($order);
            $wp_user_id = $producer_portal_id ? sz_notif_get_wp_user_id_for_portal_producer($producer_portal_id) : 0;
            if ($wp_user_id) sz_notif_dispatch_event_to_configured_recipients( $event, $order, $wp_user_id, wp_json_encode(['manual_reprocess'=>1,'order_id'=>$order_id,'event'=>$event]) );
            echo '<div class="notice notice-success"><p>Reprocessamento solicitado para o pedido #'.esc_html($order_id).'. Veja o histórico abaixo.</p></div>';
        } else {
            echo '<div class="notice notice-error"><p>Pedido/evento inválido para reprocessar.</p></div>';
        }
    }

    if ( isset($_POST['sz_notif_test_user']) && check_admin_referer('sz_notif_test_user') ) {
        global $wpdb;
        $test_user_id = absint($_POST['test_user_id'] ?? 0);
        $payload = wp_json_encode(['title'=>'🔔 Teste Senderzz','body'=>'Teste real de entrega push enviado pelo admin.','icon'=>sz_notif_icon_url(),'badge'=>sz_notif_icon_url(), 'data'=>['url'=>home_url('/meus-pedidos/')]]);
        $subs = $test_user_id ? ($wpdb->get_results($wpdb->prepare("SELECT * FROM {$wpdb->prefix}sz_push_subscriptions WHERE user_id=%d", $test_user_id), ARRAY_A) ?: []) : [];
        if (!$subs) {
            sz_notif_log_delivery($test_user_id, 'admin_test', 0, 0, 'failed', 0, '', 'Nenhum dispositivo inscrito.', $payload, 'admin_test');
            echo '<div class="notice notice-error"><p>Este usuário não tem dispositivo inscrito.</p></div>';
        } else {
            foreach ($subs as $sub) {
                $result = sz_notif_send_push($sub, $payload);
                sz_notif_log_delivery($test_user_id, 'admin_test', 0, (int)($sub['id'] ?? 0), (string)($result['status'] ?? 'failed'), (int)($result['http_code'] ?? 0), (string)($result['response_text'] ?? ''), (string)($result['error_message'] ?? ''), $payload, 'admin_test');
            }
            echo '<div class="notice notice-success"><p>Teste enviado. Confira o HTTP/status no histórico.</p></div>';
        }
    }

    $pub  = (string) get_option('sz_notif_vapid_public', '');
    $priv = (string) get_option('sz_notif_vapid_private', '');
    global $wpdb;
    $total_subs = (int) $wpdb->get_var("SELECT COUNT(*) FROM {$wpdb->prefix}sz_push_subscriptions");
    $total_log  = (int) $wpdb->get_var("SELECT COUNT(*) FROM {$wpdb->prefix}sz_notif_log");
    $last_logs  = $wpdb->get_results(
        "SELECT l.*, u.display_name FROM {$wpdb->prefix}sz_notif_log l
         LEFT JOIN {$wpdb->users} u ON u.ID = l.user_id
         ORDER BY l.id DESC LIMIT 25", ARRAY_A
    ) ?: [];
    ?>
    <div style="max-width:800px">
    <h2>Notificações Push — Senderzz</h2>

    <div style="background:#fff;border:1px solid #ccd0d4;border-radius:8px;padding:20px;margin-bottom:20px">
        <h3 style="margin-top:0">Status</h3>
        <p><strong><?php echo $total_subs; ?></strong> dispositivo(s) inscrito(s)</p>
        <p><strong><?php echo $total_log; ?></strong> notificação(ões) registrada(s) no histórico técnico</p>
        <p>Chave pública VAPID: <code style="font-size:var(--sz-text-sm);word-break:break-all"><?php echo esc_html($pub ?: '—'); ?></code></p>
        <p style="color:<?php echo $pub ? '#16a34a' : '#dc2626'; ?>;font-weight:700">
            <?php echo $pub ? '✓ Chaves configuradas — push ativo' : '✗ Sem chaves — push inativo'; ?>
        </p>
    </div>

    <div style="background:#fff;border:1px solid #ccd0d4;border-radius:8px;padding:20px;margin-bottom:20px">
        <h3 style="margin-top:0">Chaves VAPID</h3>
        <p style="color:#6b7280;font-size:var(--sz-text-base)">As chaves são geradas automaticamente. Regenerar fará com que todos os dispositivos precisem reativar as notificações.</p>
        <form method="post">
            <?php wp_nonce_field('sz_notif_regen'); ?>
            <input type="hidden" name="sz_notif_regen" value="1">
            <button type="submit" class="button button-secondary" onclick="return confirm('Regenerar chaves? Todos dispositivos perderão as notificações.')">Regenerar chaves VAPID</button>
        </form>
    </div>

    <div style="background:#fff;border:1px solid #ccd0d4;border-radius:8px;padding:20px;margin-bottom:20px">
        <h3 style="margin-top:0">Teste real por usuário</h3>
        <p style="color:#6b7280;font-size:var(--sz-text-base)">Envia push real para os dispositivos inscritos do usuário e grava HTTP/status no histórico.</p>
        <form method="post" style="display:flex;gap:10px;align-items:center">
            <?php wp_nonce_field('sz_notif_test_user'); ?>
            <input type="hidden" name="sz_notif_test_user" value="1">
            <input type="number" name="test_user_id" placeholder="WP user ID" style="width:140px">
            <button class="button button-primary">Enviar teste</button>
        </form>
    </div>

    <?php if ($last_logs): ?>
    <div style="background:#fff;border:1px solid #ccd0d4;border-radius:8px;padding:20px">
        <h3 style="margin-top:0">Últimos disparos</h3>
        <table class="widefat striped">
            <thead><tr><th>Data</th><th>Usuário</th><th>Evento</th><th>Destinatário</th><th>Pedido</th><th>Status</th><th>HTTP</th><th>Erro/Resposta</th><th>Ação</th></tr></thead>
            <tbody>
            <?php foreach ($last_logs as $l): ?>
            <tr>
                <td><?php echo esc_html(mysql2date('d/m/Y H:i', $l['sent_at'])); ?></td>
                <td><?php echo esc_html($l['display_name'] ?: "user #{$l['user_id']}"); ?></td>
                <td><?php echo esc_html($l['event']); ?></td>
                <td><?php echo esc_html($l['recipient_type'] ?: '—'); ?></td>
                <td><?php echo $l['order_id'] ? '#'.esc_html($l['order_id']) : '—'; ?></td>
                <td><strong style="color:<?php echo ($l['status'] ?? '') === 'sent' ? '#16a34a' : '#dc2626'; ?>"><?php echo esc_html($l['status'] ?? '—'); ?></strong></td>
                <td><?php echo esc_html($l['http_code'] ?? '—'); ?></td>
                <td style="max-width:320px;word-break:break-word;font-size:var(--sz-text-meta)"><?php echo esc_html(($l['error_message'] ?? '') ?: ($l['response_text'] ?? '')); ?></td>
                <td><?php if (!empty($l['order_id']) && !empty($l['event'])): ?><form method="post" style="margin:0"><?php wp_nonce_field('sz_notif_reprocess'); ?><input type="hidden" name="sz_notif_reprocess" value="1"><input type="hidden" name="order_id" value="<?php echo (int)$l['order_id']; ?>"><input type="hidden" name="event" value="<?php echo esc_attr($l['event']); ?>"><button class="button button-small">Reprocessar</button></form><?php else: ?>—<?php endif; ?></td>
            </tr>
            <?php endforeach; ?>
            </tbody>
        </table>
    </div>
    <?php endif; ?>
    </div>
    <?php
}
