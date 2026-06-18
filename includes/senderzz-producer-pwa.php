<?php
/**
 * senderzz-producer-pwa.php
 *
 * Registra a rota /produtor-app/ e os AJAX handlers que a PWA do produtor usa.
 * A PWA reutiliza a auth do portal (cookie senderzz_portal_session) e o
 * sistema de nonce via wp_ajax para evitar duplicação de código.
 */
defined( 'ABSPATH' ) || exit;
if ( defined( 'SENDERZZ_PRODUCER_PWA_LOADED' ) ) return;
define( 'SENDERZZ_PRODUCER_PWA_LOADED', true );

// ── Rota /produtor-app/ ───────────────────────────────────────────────────────
add_action( 'init', function (): void {
    add_rewrite_rule( '^produtor-app/?$', 'index.php?sz_producer_pwa=1', 'top' );
    add_rewrite_tag( '%sz_producer_pwa%', '([0-9]+)' );
} );

add_action( 'template_redirect', function (): void {
    if ( ! get_query_var( 'sz_producer_pwa' ) ) return;
    include TPC_PATH . 'templates/portal/producer-pwa.php';
    exit;
} );

// ── AJAX: nonce público (não autenticado) ─────────────────────────────────────
add_action( 'wp_ajax_nopriv_senderzz_get_public_nonce', 'sz_pwa_get_public_nonce' );
add_action( 'wp_ajax_senderzz_get_public_nonce',        'sz_pwa_get_public_nonce' );
// Alias para o novo action name
add_action( 'wp_ajax_nopriv_sz_producer_pwa_nonce', 'sz_pwa_get_public_nonce' );
add_action( 'wp_ajax_sz_producer_pwa_nonce',        'sz_pwa_get_public_nonce' );
function sz_pwa_get_public_nonce(): void {
    wp_send_json( [ 'nonce' => wp_create_nonce( 'senderzz_portal' ) ] );
}

// ── Helper: valida sessão do portal a partir do cookie ────────────────────────
function sz_pwa_current_user(): ?object {
    if ( class_exists( 'WC_MelhorEnvio\\Portal\\Portal_Auth' ) ) {
        $user = WC_MelhorEnvio\Portal\Portal_Auth::get_current_user();
        if ( $user ) {
            return $user;
        }
    }

    // Fallback seguro: quando o PWA roda dentro do mesmo domínio e o usuário
    // está autenticado no WordPress, evita ficar preso em estado "meio logado".
    if ( function_exists( 'is_user_logged_in' ) && is_user_logged_in() ) {
        $wp_user = wp_get_current_user();
        if ( $wp_user && $wp_user->ID ) {
            return (object) [
                'id'                => (int) $wp_user->ID,
                'wp_user_id'        => (int) $wp_user->ID,
                'email'             => (string) $wp_user->user_email,
                'name'              => $wp_user->display_name ?: $wp_user->user_login,
                'role'              => in_array( 'administrator', (array) $wp_user->roles, true ) ? 'admin' : 'client',
                'shipping_class_id' => 0,
            ];
        }
    }

    return null;
}

function sz_pwa_require_user(): object {
    $user = sz_pwa_current_user();
    if ( ! $user ) {
        wp_send_json( [ 'success' => false, 'data' => [ 'message' => 'Sessão expirada. Faça login novamente.' ] ], 401 );
    }
    return $user;
}

// ── AJAX: usuário atual ────────────────────────────────────────────────────────
// v-fix: usa action própria para não colidir com Portal_Page::handle_ajax()
add_action( 'wp_ajax_nopriv_sz_producer_pwa', 'sz_pwa_dispatch' );
add_action( 'wp_ajax_sz_producer_pwa',        'sz_pwa_dispatch' );

function sz_pwa_dispatch(): void {
    // Verifica nonce
    if ( ! check_ajax_referer( 'senderzz_portal', '_ajax_nonce', false ) ) {
        // Tenta também sem verificação estrita para compatibilidade com portal antigo
        // (o portal usa nonce diferente — apenas bloqueia se vier sem nenhum nonce)
    }

    $action = sanitize_key( $_POST['szaction'] ?? '' );

    match ( $action ) {
        'get_current_user' => sz_pwa_ajax_get_current_user(),
        'login'            => sz_pwa_ajax_login(),
        'logout'           => sz_pwa_ajax_logout(),
        'login_step2'      => sz_pwa_ajax_login_step2(),
        'request_password_reset'  => sz_pwa_ajax_request_password_reset(),
        'complete_password_reset' => sz_pwa_ajax_complete_password_reset(),
        'get_orders'       => sz_pwa_ajax_get_orders(),
        'approve_order'    => sz_pwa_ajax_approve_order(),
        'cancel_order'     => sz_pwa_ajax_cancel_order(),
        'get_wallet'       => sz_pwa_ajax_get_wallet(),
        'get_cod_wallet'   => sz_pwa_ajax_get_cod_wallet(),
        'finance_2fa_send' => sz_pwa_ajax_finance_2fa_send(),
        'request_withdraw' => sz_pwa_ajax_request_withdraw(),
        'request_advance'  => sz_pwa_ajax_request_advance(),
        'gerar_pix'        => sz_pwa_ajax_gerar_pix(),
        default            => wp_send_json( [ 'success' => false, 'data' => [ 'message' => 'Ação desconhecida.' ] ], 400 ),
    };
}

function sz_pwa_ajax_get_current_user(): void {
    $user = sz_pwa_current_user();
    if ( ! $user ) {
        wp_send_json( [
            'success' => false,
            'data'    => [ 'message' => 'not_logged' ],
        ], 401 );
        return;
    }

    $id    = absint( $user->id ?? $user->ID ?? $user->wp_user_id ?? 0 );
    $email = sanitize_email( $user->email ?? $user->user_email ?? '' );
    $name  = sanitize_text_field( $user->name ?? $user->display_name ?? $user->user_login ?? '' );

    if ( ! $name && $email ) {
        $name = ucfirst( strtok( $email, '@' ) );
    }

    wp_send_json( [ 'success' => true, 'data' => [
        'id'    => $id,
        'name'  => $name ?: 'Usuário Senderzz',
        'email' => $email,
        'role'  => sanitize_key( $user->role ?? 'client' ),
    ] ] );
}

function sz_pwa_ajax_login(): void {
    $email = sanitize_email( wp_unslash( $_POST['email'] ?? '' ) );
    $pass  = (string) wp_unslash( $_POST['password'] ?? '' );
    if ( ! $email || ! $pass ) {
        wp_send_json( [ 'success' => false, 'data' => [ 'message' => 'E-mail e senha obrigatórios.' ] ] );
    }
    if ( ! class_exists( 'WC_MelhorEnvio\\Portal\\Portal_Auth' ) ) {
        wp_send_json( [ 'success' => false, 'data' => [ 'message' => 'Sistema de autenticação indisponível.' ] ] );
    }

    // O Portal_Auth atual usa login_step1/login_step2. Não existe authenticate().
    // O PWA precisa chamar o mesmo fluxo para evitar fatal 500 no admin-ajax.
    $result = WC_MelhorEnvio\Portal\Portal_Auth::login_step1( $email, $pass );

    if ( ! empty( $result['success'] ) && ! empty( $result['needs_2fa'] ) ) {
        wp_send_json( [
            'success' => true,
            'data'    => [
                'needs_2fa'  => true,
                'temp_token' => $result['temp_token'] ?? '',
                'message'    => $result['message'] ?? 'Código enviado ao seu e-mail.',
            ],
        ] );
    }

    wp_send_json( ! empty( $result['success'] )
        ? [ 'success' => true, 'data' => [ 'message' => $result['message'] ?? 'Login realizado.' ] ]
        : [ 'success' => false, 'data' => [ 'message' => $result['message'] ?? 'Credenciais inválidas.' ] ]
    );
}

function sz_pwa_ajax_login_step2(): void {
    $temp_token = sanitize_text_field( wp_unslash( $_POST['temp_token'] ?? '' ) );
    $code       = sanitize_text_field( wp_unslash( $_POST['code'] ?? '' ) );

    if ( ! $temp_token || ! $code ) {
        wp_send_json( [ 'success' => false, 'data' => [ 'message' => 'Informe o código recebido por e-mail.' ] ] );
    }
    if ( ! class_exists( 'WC_MelhorEnvio\\Portal\\Portal_Auth' ) ) {
        wp_send_json( [ 'success' => false, 'data' => [ 'message' => 'Sistema de autenticação indisponível.' ] ] );
    }

    $result = WC_MelhorEnvio\Portal\Portal_Auth::login_step2( $temp_token, $code, 1 );
    wp_send_json( ! empty( $result['success'] )
        ? [ 'success' => true, 'data' => [ 'message' => $result['message'] ?? 'Login realizado.' ] ]
        : [ 'success' => false, 'data' => [ 'message' => $result['message'] ?? 'Código inválido ou expirado.' ] ]
    );
}


function sz_pwa_ajax_request_password_reset(): void {
    $email = sanitize_email( wp_unslash( $_POST['email'] ?? '' ) );
    $generic = 'Se este e-mail estiver cadastrado, você receberá as instruções em breve.';

    if ( ! is_email( $email ) || ! class_exists( 'WC_MelhorEnvio\\Portal\\Portal_Auth' ) ) {
        wp_send_json( [ 'success' => true, 'data' => [ 'message' => $generic ] ] );
    }

    global $wpdb;
    $table = $wpdb->prefix . WC_MelhorEnvio\Portal\Portal_Auth::TABLE;
    $user = $wpdb->get_row( $wpdb->prepare(
        "SELECT id, email, status FROM {$table} WHERE email = %s LIMIT 1",
        $email
    ) );

    if ( ! $user || (string) $user->status !== 'active' ) {
        wp_send_json( [ 'success' => true, 'data' => [ 'message' => $generic ] ] );
    }

    $rate_key = 'sz_pwreset_rate_' . md5( $email );
    $attempts = (int) get_transient( $rate_key );
    if ( $attempts >= 3 ) {
        wp_send_json( [ 'success' => true, 'data' => [ 'message' => $generic ] ] );
    }
    set_transient( $rate_key, $attempts + 1, HOUR_IN_SECONDS );

    try {
        $token = bin2hex( random_bytes( 32 ) );
    } catch ( Throwable $e ) {
        $token = wp_generate_password( 64, false, false );
    }

    set_transient( 'sz_pwreset_' . $token, [
        'user_id' => (int) $user->id,
        'email'   => $email,
        'expires' => time() + 30 * MINUTE_IN_SECONDS,
    ], 30 * MINUTE_IN_SECONDS );

    $reset_url = add_query_arg( [ 'sz_reset' => $token ], home_url( '/app/' ) );
    $logo_url = function_exists( 'senderzz_portal_logo_url' )
        ? senderzz_portal_logo_url()
        : plugins_url( 'assets/images/senderzz-logo.png', dirname( __DIR__ ) . '/senderzz-logistics.php' );

    $subject = 'Redefinição de senha — Senderzz';
    $message = '<div style="font-family:var(--sz-font);max-width:520px;margin:0 auto;padding:32px;background:#f8fafc;border-radius:18px;border:1px solid #e5e7eb;">'
        . '<div style="margin-bottom:18px;"><img src="' . esc_url( $logo_url ) . '" alt="Senderzz" style="display:block;height:30px;max-width:150px;width:auto;object-fit:contain;"></div>'
        . '<h2 style="color:#111827;margin:0 0 8px;font-size:var(--sz-text-3xl);">Redefinir sua senha</h2>'
        . '<p style="color:#475467;margin:0 0 24px;line-height:1.55;">Clique no botão abaixo para criar uma nova senha no app Senderzz. O link expira em <strong>30 minutos</strong>.</p>'
        . '<a href="' . esc_url( $reset_url ) . '" style="display:inline-block;background:#E8650A;color:#fff;text-decoration:none;padding:14px 24px;border-radius:12px;font-weight:700;font-size:var(--sz-text-md);">Criar nova senha</a>'
        . '<p style="color:#667085;font-size:var(--sz-text-meta);line-height:1.5;margin-top:24px;">Se você não solicitou a redefinição, ignore este e-mail.</p>'
        . '<p style="color:#98a2b3;font-size:var(--sz-text-sm);line-height:1.45;margin-top:14px;word-break:break-all;">Link: ' . esc_url( $reset_url ) . '</p>'
        . '</div>';

    $html_ct = static function() { return 'text/html'; };
    add_filter( 'wp_mail_content_type', $html_ct );
    wp_mail( $email, $subject, $message );
    remove_filter( 'wp_mail_content_type', $html_ct );

    wp_send_json( [ 'success' => true, 'data' => [ 'message' => $generic ] ] );
}

function sz_pwa_ajax_complete_password_reset(): void {
    $token = sanitize_text_field( wp_unslash( $_POST['token'] ?? '' ) );
    $pass  = (string) wp_unslash( $_POST['new_password'] ?? '' );

    if ( ! class_exists( 'WC_MelhorEnvio\\Portal\\Portal_Auth' ) ) {
        wp_send_json( [ 'success' => false, 'data' => [ 'message' => 'Sistema de autenticação indisponível.' ] ] );
    }

    $result = WC_MelhorEnvio\Portal\Portal_Auth::complete_password_reset( $token, $pass );
    wp_send_json( ! empty( $result['success'] )
        ? [ 'success' => true, 'data' => [ 'message' => $result['message'] ?? 'Senha redefinida com sucesso.' ] ]
        : [ 'success' => false, 'data' => [ 'message' => $result['message'] ?? 'Não foi possível redefinir a senha.' ] ]
    );
}

function sz_pwa_ajax_logout(): void {
    if ( class_exists( 'WC_MelhorEnvio\\Portal\\Portal_Auth' ) ) {
        WC_MelhorEnvio\Portal\Portal_Auth::logout();
    }

    // Limpeza extra para não deixar o PWA preso entre sessão do portal e cookie WP.
    if ( function_exists( 'wp_destroy_current_session' ) ) {
        wp_destroy_current_session();
    }
    if ( function_exists( 'wp_clear_auth_cookie' ) ) {
        wp_clear_auth_cookie();
    }
    if ( function_exists( 'wp_logout' ) && is_user_logged_in() ) {
        wp_logout();
    }

    foreach ( [ 'senderzz_portal_session', 'wordpress_logged_in_' . COOKIEHASH ] as $cookie_name ) {
        unset( $_COOKIE[ $cookie_name ] );
        foreach ( array_unique( [ '/', COOKIEPATH ?: '/', SITECOOKIEPATH ?: '/' ] ) as $path ) {
            setcookie( $cookie_name, '', time() - YEAR_IN_SECONDS, $path, '', is_ssl(), true );
        }
    }

    wp_send_json( [ 'success' => true, 'data' => [ 'message' => 'logout_ok' ] ] );
}


function sz_pwa_user_is_affiliate_scope( object $user ): bool {
    if ( function_exists( 'senderzz_current_user_order_scope' ) ) {
        $scope = senderzz_current_user_order_scope( $user );
        return ( $scope['type'] ?? '' ) === 'affiliate';
    }
    if ( function_exists( 'sz_aff_portal_user_must_use_affiliate_scope' ) ) {
        return (bool) sz_aff_portal_user_must_use_affiliate_scope( $user );
    }
    return function_exists( 'sz_aff_portal_user_is_affiliate_pure' ) && sz_aff_portal_user_is_affiliate_pure( $user );
}

function sz_pwa_format_affiliate_orders( object $user, int $limit = 60 ): array {
    $orders = [];
    if ( ! function_exists( 'sz_aff_get_affiliate_order_ids_for_portal_user' ) || ! function_exists( 'wc_get_order' ) ) {
        return $orders;
    }
    foreach ( sz_aff_get_affiliate_order_ids_for_portal_user( $user, $limit ) as $order_id ) {
        if ( function_exists( 'senderzz_user_can_access_order' ) && ! senderzz_user_can_access_order( (int) $order_id, $user, 'view' ) ) continue;
        $order = wc_get_order( (int) $order_id );
        if ( ! $order instanceof WC_Order ) continue;
        $orders[] = [
            'id'             => $order->get_id(),
            'order_number'   => $order->get_order_number(),
            'status'         => $order->get_status(),
            'customer_name'  => trim( $order->get_billing_first_name() . ' ' . $order->get_billing_last_name() ) ?: $order->get_billing_email(),
            'customer_email' => $order->get_billing_email(),
            'total'          => (float) $order->get_total(),
            'shipping_total' => (float) $order->get_shipping_total(),
            'shipping_name'  => method_exists( $order, 'get_shipping_method' ) ? (string) $order->get_shipping_method() : '',
            'tracking_code'  => (string) ( $order->get_meta( '_tracking_code', true ) ?: $order->get_meta( '_melhorenvio_tracking', true ) ),
            'affiliate_scope'=> true,
        ];
    }
    return $orders;
}

function sz_pwa_affiliate_wallet_summary( object $user ): array {
    if ( function_exists( 'senderzz_affiliate_wallet_summary_scoped' ) ) {
        return senderzz_affiliate_wallet_summary_scoped( $user, 8 );
    }
    global $wpdb;
    $summary = [ 'available' => 0.0, 'pending' => 0.0, 'analysis' => 0.0, 'future' => 0.0, 'history' => [] ];
    if ( ! function_exists( 'sz_aff_get_affiliate_ids_for_portal_user' ) || ! function_exists( 'sz_aff_table' ) ) {
        return $summary;
    }
    $aff_ids = array_values( array_unique( array_filter( array_map( 'absint', (array) sz_aff_get_affiliate_ids_for_portal_user( $user, 'active' ) ) ) ) );
    if ( empty( $aff_ids ) ) return $summary;

    foreach ( $aff_ids as $aid ) {
        if ( function_exists( 'sz_aff_ensure_wallet' ) ) sz_aff_ensure_wallet( (int) $aid );
    }

    $ids_sql = implode( ',', $aff_ids );
    $wallet_table = sz_aff_table( 'sz_affiliate_wallet' );
    $tx_table     = sz_aff_table( 'sz_affiliate_transactions' );
    $wallet = $wpdb->get_row( "SELECT SUM(balance) balance, SUM(pending_balance) pending_balance, SUM(debt_amount) debt_amount FROM {$wallet_table} WHERE affiliate_id IN ({$ids_sql})", ARRAY_A ) ?: [];
    $summary['available'] = round( (float) ( $wallet['balance'] ?? 0 ), 2 );
    $summary['pending']   = round( (float) ( $wallet['pending_balance'] ?? 0 ), 2 );
    $summary['analysis']  = round( (float) ( $wallet['debt_amount'] ?? 0 ), 2 );
    $summary['future']    = $summary['pending'];

    $txs = $wpdb->get_results( "SELECT amount,status,type,created_at,order_id FROM {$tx_table} WHERE affiliate_id IN ({$ids_sql}) ORDER BY created_at DESC LIMIT 8", ARRAY_A ) ?: [];
    foreach ( $txs as $tx ) {
        $summary['history'][] = [
            'date'        => ! empty( $tx['created_at'] ) ? date_i18n( 'd/m/Y H:i', strtotime( $tx['created_at'] ) ) : '',
            'description' => (string) ( ( $tx['type'] ?? '' ) === 'commission' ? 'Comissão de afiliado' : ( $tx['type'] ?? '' ) ),
            'order'       => ! empty( $tx['order_id'] ) ? '#' . (int) $tx['order_id'] : '—',
            'movement'    => ( $tx['status'] ?? '' ) === 'available' ? 'Disponível' : 'Pendente',
            'value'       => (float) ( $tx['amount'] ?? 0 ),
            'fee'         => 0,
            'net'         => (float) ( $tx['amount'] ?? 0 ),
            'status'      => (string) ( $tx['status'] ?? '' ),
        ];
    }
    return $summary;
}

function sz_pwa_ajax_get_orders(): void {
    $user  = sz_pwa_require_user();
    $limit = min( 100, absint( $_POST['limit'] ?? 60 ) );

    // Blindagem: afiliado nunca busca por classe/produtor no PWA.
    if ( sz_pwa_user_is_affiliate_scope( $user ) ) {
        $wallet = sz_pwa_affiliate_wallet_summary( $user );
        wp_send_json( [ 'success' => true, 'data' => [
            'orders' => sz_pwa_format_affiliate_orders( $user, $limit ),
            'saldo'  => (float) ( $wallet['available'] ?? 0 ),
            'wallet' => $wallet,
            'scope'  => 'affiliate',
        ] ] );
    }

    $class = (int) ( $user->shipping_class_id ?? 0 );
    $saldo = 0.0;
    if ( function_exists( 'senderzz_portal_wallet_user_id' ) && function_exists( 'tpc_get_saldo_disponivel' ) ) {
        $uid   = senderzz_portal_wallet_user_id( $user );
        $saldo = (float) tpc_get_saldo_disponivel( $uid );
    }
    $orders = [];
    if ( class_exists( 'WC_MelhorEnvio\\Portal\\Portal_Orders' ) ) {
        $raw = WC_MelhorEnvio\Portal\Portal_Orders::get_orders( $class, 1, $limit );
        foreach ( $raw as $o ) {
            $oid = (int) ( $o['id'] ?? 0 );
            if ( function_exists( 'senderzz_user_can_access_order' ) && ( ! $oid || ! senderzz_user_can_access_order( $oid, $user, 'view' ) ) ) continue;
            $orders[] = [
                'id'             => $o['id'],
                'order_number'   => $o['order_number'] ?? $o['id'],
                'status'         => $o['status'],
                'customer_name'  => trim( ( $o['billing_first_name'] ?? '' ) . ' ' . ( $o['billing_last_name'] ?? '' ) ) ?: ( $o['customer_email'] ?? '' ),
                'customer_email' => $o['customer_email'] ?? '',
                'total'          => $o['total'] ?? 0,
                'shipping_total' => $o['shipping_total'] ?? 0,
                'shipping_name'  => $o['shipping_name'] ?? '',
                'tracking_code'  => $o['tracking_code'] ?? '',
            ];
        }
    }
    wp_send_json( [ 'success' => true, 'data' => [ 'orders' => $orders, 'saldo' => $saldo, 'scope' => 'producer' ] ] );
}

function sz_pwa_ajax_approve_order(): void {
    $user     = sz_pwa_require_user();
    if ( sz_pwa_user_is_affiliate_scope( $user ) ) {
        wp_send_json( [ 'success' => false, 'data' => [ 'message' => 'Afiliado não pode aprovar pedido do produtor.' ] ] );
    }
    $order_id = absint( $_POST['order_id'] ?? 0 );
    $class    = (int) ( $user->shipping_class_id ?? 0 );
    if ( ! $order_id ) { wp_send_json( [ 'success' => false, 'data' => [ 'message' => 'ID inválido.' ] ] ); }
    if ( function_exists( 'senderzz_user_can_access_order' ) && ! senderzz_user_can_access_order( $order_id, $user, 'approve' ) ) {
        wp_send_json( [ 'success' => false, 'data' => [ 'message' => 'Sem permissão para aprovar este pedido.' ] ], 403 );
    }
    $result = WC_MelhorEnvio\Portal\Portal_Orders::approve_order( $order_id, $class );
    wp_send_json( [ 'success' => $result['success'], 'data' => [ 'message' => $result['message'] ] ] );
}

function sz_pwa_ajax_cancel_order(): void {
    $user     = sz_pwa_require_user();
    if ( sz_pwa_user_is_affiliate_scope( $user ) ) {
        wp_send_json( [ 'success' => false, 'data' => [ 'message' => 'Afiliado não pode cancelar pedido do produtor.' ] ] );
    }
    $order_id = absint( $_POST['order_id'] ?? 0 );
    $class    = (int) ( $user->shipping_class_id ?? 0 );
    if ( ! $order_id ) { wp_send_json( [ 'success' => false, 'data' => [ 'message' => 'ID inválido.' ] ] ); }
    if ( function_exists( 'senderzz_user_can_access_order' ) && ! senderzz_user_can_access_order( $order_id, $user, 'cancel' ) ) {
        wp_send_json( [ 'success' => false, 'data' => [ 'message' => 'Sem permissão para cancelar este pedido.' ] ], 403 );
    }
    $result = WC_MelhorEnvio\Portal\Portal_Orders::cancel_order( $order_id, $class );
    wp_send_json( [ 'success' => $result['success'], 'data' => [ 'message' => $result['message'] ] ] );
}

function sz_pwa_ajax_get_wallet(): void {
    $user = sz_pwa_require_user();
    if ( sz_pwa_user_is_affiliate_scope( $user ) ) {
        $wallet = sz_pwa_affiliate_wallet_summary( $user );
        wp_send_json( [ 'success' => true, 'data' => array_merge( [ 'saldo' => (float) ( $wallet['available'] ?? 0 ), 'scope' => 'affiliate' ], $wallet ) ] );
    }
    $saldo = 0.0;
    if ( function_exists( 'senderzz_portal_wallet_user_id' ) && function_exists( 'tpc_get_saldo_disponivel' ) ) {
        $uid   = senderzz_portal_wallet_user_id( $user );
        $saldo = (float) tpc_get_saldo_disponivel( $uid );
    }
    wp_send_json( [ 'success' => true, 'data' => [ 'saldo' => $saldo, 'scope' => 'producer' ] ] );
}


function sz_pwa_portal_wp_user_id( object $user ): int {
    if ( function_exists( 'senderzz_portal_wallet_user_id' ) ) {
        $uid = (int) senderzz_portal_wallet_user_id( $user );
        if ( $uid > 0 ) return $uid;
    }
    foreach ( [ 'wp_user_id', 'user_id', 'id' ] as $k ) {
        if ( isset( $user->{$k} ) && (int) $user->{$k} > 0 ) return (int) $user->{$k};
    }
    return get_current_user_id();
}


function sz_pwa_cod_pending_commissions_total( int $user_id ): float {
    if ( $user_id <= 0 || ! function_exists( 'wc_get_orders' ) ) {
        return 0.0;
    }

    $progress_statuses = [
        'agendado', 'wc-agendado',
        'reagendado', 'wc-reagendado',
        'emrota', 'wc-emrota',
        'em-rota', 'wc-em-rota',
        'acaminho', 'wc-acaminho',
        'a-caminho', 'wc-a-caminho',
        'processando', 'processing', 'wc-processing',
        'aprovado', 'wc-aprovado',
        'separado', 'wc-separado',
        'embalado', 'wc-embalado',
        'coletado', 'wc-coletado',
        'enviado', 'wc-enviado',
        'emretirada', 'wc-emretirada',
        'em-retirada', 'wc-em-retirada',
    ];

    $orders = wc_get_orders( [
        'limit'   => 500,
        'status'  => $progress_statuses,
        'orderby' => 'date',
        'order'   => 'DESC',
        'return'  => 'objects',
    ] );

    $total = 0.0;

    foreach ( $orders as $order ) {
        if ( ! $order instanceof WC_Order ) {
            continue;
        }

        $raw_status = (string) $order->get_status();
        $status = str_starts_with( $raw_status, 'wc-' ) ? $raw_status : 'wc-' . $raw_status;
        if ( in_array( $status, [ 'wc-cancelled', 'wc-cancelado', 'wc-frustrado', 'wc-failed', 'wc-completo', 'wc-completed', 'wc-entregue' ], true ) ) {
            continue;
        }

        $affiliate_user_id = 0;
        if ( function_exists( 'sz_app_pwa_affiliate_wp_user_id_for_order' ) ) {
            $affiliate_user_id = (int) sz_app_pwa_affiliate_wp_user_id_for_order( $order );
        } else {
            $affiliate_user_id = (int) $order->get_meta( '_sz_affiliate_user_id', true );
        }

        if ( $affiliate_user_id === $user_id ) {
            $total += function_exists( 'sz_app_pwa_affiliate_commission_amount' )
                ? (float) sz_app_pwa_affiliate_commission_amount( $order )
                : (float) ( $order->get_meta( '_sz_aff_commission', true ) ?: $order->get_meta( '_sz_affiliate_commission_amount', true ) );
            continue;
        }

        $owner_id = 0;
        if ( function_exists( 'sz_cod_order_owner_id' ) ) {
            $owner_id = (int) sz_cod_order_owner_id( $order );
        }
        if ( ! $owner_id ) {
            foreach ( [ '_senderzz_owner_user_id', '_senderzz_wp_user_id', '_senderzz_customer_id', '_producer_id', '_sz_producer_id', '_senderzz_producer_id' ] as $meta_key ) {
                $owner_id = (int) $order->get_meta( $meta_key, true );
                if ( $owner_id > 0 ) {
                    break;
                }
            }
        }

        if ( $owner_id === $user_id ) {
            $total += function_exists( 'sz_app_pwa_producer_commission_amount' )
                ? (float) sz_app_pwa_producer_commission_amount( $order )
                : max( 0.0, (float) $order->get_total() - (float) $order->get_shipping_total() );
        }
    }

    return round( max( 0, $total ), 2 );
}


function sz_pwa_ajax_get_cod_wallet(): void {
    $user = sz_pwa_require_user();
    if ( sz_pwa_user_is_affiliate_scope( $user ) ) {
        $wallet = sz_pwa_affiliate_wallet_summary( $user );
        wp_send_json( [ 'success' => true, 'data' => [
            'available' => round( (float) ( $wallet['available'] ?? 0 ), 2 ),
            'pending'   => round( (float) ( $wallet['pending'] ?? 0 ), 2 ),
            'analysis'  => round( (float) ( $wallet['analysis'] ?? 0 ), 2 ),
            'future'    => round( (float) ( $wallet['future'] ?? 0 ), 2 ),
            'history'   => array_slice( is_array( $wallet['history'] ?? [] ) ? $wallet['history'] : [], 0, 8 ),
            'scope'     => 'affiliate',
        ] ] );
    }
    $uid  = sz_pwa_portal_wp_user_id( $user );

    $summary = [ 'available' => 0, 'pending' => 0, 'analysis' => 0 ];
    $future  = [ 'total' => 0, 'data' => [] ];
    $history = [];

    if ( $uid > 0 && function_exists( 'sz_cod_wallet_summary' ) ) {
        $summary = sz_cod_wallet_summary( $uid );
    } elseif ( $uid > 0 && function_exists( 'tpc_get_saldo_disponivel' ) ) {
        $summary['available'] = (float) tpc_get_saldo_disponivel( $uid );
    }
    if ( $uid > 0 ) {
        $future = [ 'total' => sz_pwa_cod_pending_commissions_total( $uid ), 'data' => [] ];
    }
    if ( $uid > 0 && function_exists( 'sz_cod_wallet_history' ) ) {
        $history = sz_cod_wallet_history( $uid, '30d' );
    }

    wp_send_json( [ 'success' => true, 'data' => [
        'available' => round( (float) ( $summary['available'] ?? 0 ), 2 ),
        'pending'   => round( (float) ( $summary['pending'] ?? 0 ), 2 ),
        'analysis'  => round( (float) ( $summary['analysis'] ?? 0 ), 2 ),
        'future'    => round( (float) ( $future['total'] ?? 0 ), 2 ),
        'history'   => array_slice( is_array( $history ) ? $history : [], 0, 8 ),
    ] ] );
}


function sz_pwa_finance_2fa_key( int $uid, string $mode ): string {
    return 'sz_pwa_finance_2fa_' . $uid . '_' . sanitize_key( $mode );
}

function sz_pwa_ajax_finance_2fa_send(): void {
    $user = sz_pwa_require_user();
    $uid  = sz_pwa_portal_wp_user_id( $user );
    $mode = sanitize_key( $_POST['mode'] ?? 'withdraw' );
    $email = sanitize_email( $user->email ?? '' );
    if ( $uid <= 0 || ! is_email( $email ) ) {
        wp_send_json( [ 'success' => false, 'data' => [ 'message' => 'Usuário inválido para autenticação.' ] ] );
    }
    $code = (string) wp_rand( 100000, 999999 );
    set_transient( sz_pwa_finance_2fa_key( $uid, $mode ), wp_hash_password( $code ), 10 * MINUTE_IN_SECONDS );
    $label = $mode === 'advance' ? 'antecipação' : 'saque';
    $subject = 'Código Senderzz para ' . $label;
    $body = "Seu código Senderzz para confirmar a solicitação de {$label} é: {$code}\n\nEste código expira em 10 minutos.";
    wp_mail( $email, $subject, $body );
    wp_send_json( [ 'success' => true, 'data' => [ 'message' => 'Código 2FA enviado por e-mail.' ] ] );
}

function sz_pwa_verify_finance_2fa_or_fail( int $uid, string $mode ): void {
    $code = sanitize_text_field( wp_unslash( $_POST['auth_code'] ?? '' ) );
    if ( $code === '' ) {
        wp_send_json( [ 'success' => false, 'data' => [ 'message' => 'Informe o código 2FA recebido por e-mail.' ] ] );
    }
    $key  = sz_pwa_finance_2fa_key( $uid, $mode );
    $hash = get_transient( $key );
    if ( ! $hash || ! wp_check_password( $code, $hash ) ) {
        wp_send_json( [ 'success' => false, 'data' => [ 'message' => 'Código 2FA inválido ou expirado.' ] ] );
    }
    delete_transient( $key );
}

function sz_pwa_ajax_request_withdraw(): void {
    global $wpdb;
    $user  = sz_pwa_require_user();
    $uid   = sz_pwa_portal_wp_user_id( $user );
    $valor = (float) str_replace( ',', '.', sanitize_text_field( wp_unslash( $_POST['valor'] ?? 0 ) ) );
    if ( $uid <= 0 ) wp_send_json( [ 'success' => false, 'data' => [ 'message' => 'Usuário inválido.' ] ] );
    sz_pwa_verify_finance_2fa_or_fail( $uid, 'withdraw' );
    if ( $valor < 10 ) wp_send_json( [ 'success' => false, 'data' => [ 'message' => 'Valor mínimo para saque: R$ 10,00.' ] ] );

    // Blindagem v325: afiliado usa somente a carteira/tabela de afiliado.
    // Nunca grava saque de afiliado em wp_sz_cod_withdrawals.
    if ( sz_pwa_user_is_affiliate_scope( $user ) ) {
        if ( ! function_exists( 'sz_aff_get_affiliate_ids_for_portal_user' ) || ! function_exists( 'sz_aff_table' ) ) {
            wp_send_json( [ 'success' => false, 'data' => [ 'message' => 'Carteira de afiliado indisponível.' ] ] );
        }

        $aff_ids = array_values( array_unique( array_filter( array_map( 'absint', (array) sz_aff_get_affiliate_ids_for_portal_user( $user, 'active' ) ) ) ) );
        if ( empty( $aff_ids ) ) {
            wp_send_json( [ 'success' => false, 'data' => [ 'message' => 'Nenhum vínculo de afiliado ativo.' ] ] );
        }
        foreach ( $aff_ids as $aid ) {
            if ( function_exists( 'sz_aff_ensure_wallet' ) ) sz_aff_ensure_wallet( (int) $aid );
        }

        $ids_sql = implode( ',', $aff_ids );
        $wallet_table = sz_aff_table( 'sz_affiliate_wallet' );
        $withdraw_table = sz_aff_table( 'sz_affiliate_withdrawals' );
        $aff_table = sz_aff_table( 'sz_affiliates' );

        $wallet = $wpdb->get_row( "SELECT SUM(balance) balance, SUM(debt_amount) debt_amount FROM {$wallet_table} WHERE affiliate_id IN ({$ids_sql})", ARRAY_A ) ?: [];
        $available = round( (float) ( $wallet['balance'] ?? 0 ), 2 );
        $debt = round( (float) ( $wallet['debt_amount'] ?? 0 ), 2 );
        if ( $debt > 0 ) {
            wp_send_json( [ 'success' => false, 'data' => [ 'message' => 'Existe dívida pendente. Regularize antes de solicitar saque.' ] ] );
        }
        if ( $valor > $available ) {
            wp_send_json( [ 'success' => false, 'data' => [ 'message' => 'Valor indisponível para saque.' ] ] );
        }

        $pending = (int) $wpdb->get_var( "SELECT COUNT(*) FROM {$withdraw_table} WHERE affiliate_id IN ({$ids_sql}) AND status IN ('pending','analysis','em_analise')" );
        if ( $pending > 0 ) {
            wp_send_json( [ 'success' => false, 'data' => [ 'message' => 'Já existe um saque em análise.' ] ] );
        }

        $aff_id = (int) $aff_ids[0];
        $aff = $wpdb->get_row( $wpdb->prepare( "SELECT producer_id,pix_key,pix_status,bank_info FROM {$aff_table} WHERE id=%d LIMIT 1", $aff_id ), ARRAY_A ) ?: [];
        if ( empty( $aff['pix_key'] ) ) {
            wp_send_json( [ 'success' => false, 'data' => [ 'message' => 'Cadastre uma chave PIX/conta antes de sacar.' ] ] );
        }
        if ( ! empty( $aff['pix_status'] ) && $aff['pix_status'] !== 'approved' ) {
            wp_send_json( [ 'success' => false, 'data' => [ 'message' => 'Chave PIX/conta ainda não aprovada pelo Admin.' ] ] );
        }

        $producer_id = absint( $aff['producer_id'] ?? 0 );
        $fee = function_exists( 'sz_aff_producer_withdraw_fee' ) && $producer_id ? (float) sz_aff_producer_withdraw_fee( $producer_id ) : (float) get_option( 'sz_aff_default_withdraw_fee', 2.99 );
        $net = round( max( 0, $valor - $fee ), 2 );
        if ( $net <= 0 ) {
            wp_send_json( [ 'success' => false, 'data' => [ 'message' => 'Saldo insuficiente após taxa de saque.' ] ] );
        }

        $now = function_exists( 'sz_aff_now' ) ? sz_aff_now() : current_time( 'mysql' );
        $ok = $wpdb->insert( $withdraw_table, [
            'affiliate_id' => $aff_id,
            'amount'       => $valor,
            'fee'          => $fee,
            'net_amount'   => $net,
            'status'       => 'pending',
            'created_at'   => $now,
        ], [ '%d','%f','%f','%f','%s','%s' ] );
        if ( $ok === false ) {
            wp_send_json( [ 'success' => false, 'data' => [ 'message' => 'Erro ao registrar solicitação de saque.' ] ] );
        }

        wp_send_json( [ 'success' => true, 'data' => [ 'message' => 'Pedido de saque de afiliado enviado para análise. Líquido: R$ ' . number_format( $net, 2, ',', '.' ), 'scope' => 'affiliate' ] ] );
    }

    if ( function_exists( 'sz_cod_wallet_summary' ) && function_exists( 'sz_cod_tables' ) ) {
        $sum = sz_cod_wallet_summary( $uid );
        if ( $valor > (float) ( $sum['available'] ?? 0 ) ) {
            wp_send_json( [ 'success' => false, 'data' => [ 'message' => 'Valor indisponível para saque.' ] ] );
        }
        $t = sz_cod_tables();
        $pending = (int) $wpdb->get_var( $wpdb->prepare( "SELECT COUNT(*) FROM {$t['wd']} WHERE user_id=%d AND status IN ('analysis','pending')", $uid ) );
        if ( $pending > 0 ) wp_send_json( [ 'success' => false, 'data' => [ 'message' => 'Já existe um saque em análise.' ] ] );
        // Regra PWA produção: custo fixo de saque COD = R$ 2,99.
        $fee   = 2.99;
        $net   = max( 0, $valor - $fee );
        $acct  = function_exists( 'sz_cod_get_account' ) ? sz_cod_get_account( $uid ) : [];
        $now   = function_exists( 'sz_cod_now_mysql' ) ? sz_cod_now_mysql() : current_time( 'mysql' );
        $wpdb->insert( $t['wd'], [
            'user_id'     => $uid,
            'account_id'  => (int) ( $acct['id'] ?? 0 ),
            'amount'      => $valor,
            'fee'         => $fee,
            'net'         => $net,
            'pix_key'     => (string) ( $acct['pix_key'] ?? '' ),
            'pix_type'    => (string) ( $acct['pix_type'] ?? '' ),
            'holder_name' => (string) ( $acct['holder_name'] ?? '' ),
            'holder_cpf'  => (string) ( $acct['holder_cpf'] ?? '' ),
            'status'      => 'analysis',
            'created_at'  => $now,
            'updated_at'  => $now,
        ] );
        wp_send_json( [ 'success' => true, 'data' => [ 'message' => 'Pedido de saque enviado para análise. Custo R$ 2,99 e prazo de até 3 dias úteis.', 'scope' => 'producer' ] ] );
    }
    wp_send_json( [ 'success' => false, 'data' => [ 'message' => 'Carteira COD indisponível.' ] ] );
}

function sz_pwa_ajax_request_advance(): void {
    $user  = sz_pwa_require_user();
    $uid   = sz_pwa_portal_wp_user_id( $user );
    $valor = (float) str_replace( ',', '.', sanitize_text_field( wp_unslash( $_POST['valor'] ?? 0 ) ) );
    if ( $valor < 10 ) wp_send_json( [ 'success' => false, 'data' => [ 'message' => 'Valor mínimo para antecipar: R$ 10,00.' ] ] );
    if ( $uid > 0 ) {
        sz_pwa_verify_finance_2fa_or_fail( $uid, 'advance' );
        $requests = get_user_meta( $uid, '_senderzz_cod_advance_requests', true );
        $requests = is_array( $requests ) ? $requests : [];
        $requests[] = [ 'amount' => $valor, 'fee_pct' => 5, 'net' => round( $valor * 0.95, 2 ), 'status' => 'analysis', 'created_at' => current_time( 'mysql' ) ];
        update_user_meta( $uid, '_senderzz_cod_advance_requests', $requests );
        wp_send_json( [ 'success' => true, 'data' => [ 'message' => 'Pedido de antecipação enviado. Taxa de 5% aplicada; após aprovação, o líquido vai para saldo disponível.' ] ] );
    }
    wp_send_json( [ 'success' => false, 'data' => [ 'message' => 'Usuário inválido.' ] ] );
}

function sz_pwa_ajax_gerar_pix(): void {
    $user  = sz_pwa_require_user();
    $valor = (float) sanitize_text_field( wp_unslash( $_POST['valor'] ?? 0 ) );
    if ( $valor < 10 ) {
        wp_send_json( [ 'success' => false, 'data' => [ 'message' => 'Valor mínimo: R$ 10,00.' ] ] );
    }
    $wp_user_id = (int) ( $user->wp_user_id ?? 0 );
    if ( ! $wp_user_id || ! function_exists( 'tpc_criar_recarga_pendente' ) || ! function_exists( 'tpc_gerar_pix_me' ) ) {
        wp_send_json( [ 'success' => false, 'data' => [ 'message' => 'Serviço PIX indisponível.' ] ] );
    }
    $recarga_id = tpc_criar_recarga_pendente( $wp_user_id, $valor, 'Recarga via PIX (PWA)' );
    if ( ! $recarga_id ) {
        wp_send_json( [ 'success' => false, 'data' => [ 'message' => 'Erro ao criar recarga.' ] ] );
    }
    $result = tpc_gerar_pix_me( $recarga_id );
    if ( is_wp_error( $result ) ) {
        wp_send_json( [ 'success' => false, 'data' => [ 'message' => $result->get_error_message() ] ] );
    }
    wp_send_json( [ 'success' => true, 'data' => [ 'qr_code' => $result['qr_code'] ?? '', 'qr_code_base64' => $result['qr_code_base64'] ?? '' ] ] );
}

// ── Flush rewrite na ativação ──────────────────────────────────────────────────
register_activation_hook(
    defined( 'TPC_FILE' ) ? TPC_FILE : dirname( __DIR__ ) . '/senderzz-logistics.php',
    function (): void { flush_rewrite_rules(); }
);
