<?php
/**
 * rest-api.php
 *
 * Endpoints consumidos pelo painel externo.
 * Autenticação: JWT via header Authorization: Bearer {token}
 *
 * GET  /wp-json/tp-carteira/v1/saldo
 * GET  /wp-json/tp-carteira/v1/extrato
 * POST /wp-json/tp-carteira/v1/recarregar
 * GET  /wp-json/tp-carteira/v1/recarga/{recarga_id}/pix
 * GET  /wp-json/tp-carteira/v1/me          (dados do usuário + saldo)
 */
defined( 'ABSPATH' ) || exit;
if ( defined( 'SENDERZZ_TPC_REST_API_LOADED' ) ) return;
define( 'SENDERZZ_TPC_REST_API_LOADED', true );

add_action( 'rest_api_init', function () {

    $auth = 'tpc_rest_auth'; // callback de permissão reutilizável

    register_rest_route( 'tp-carteira/v1', '/me', [
        'methods'             => 'GET',
        'callback'            => 'tpc_endpoint_me',
        'permission_callback' => $auth,
    ] );

    register_rest_route( 'tp-carteira/v1', '/saldo', [
        'methods'             => 'GET',
        'callback'            => 'tpc_endpoint_saldo',
        'permission_callback' => $auth,
    ] );

    register_rest_route( 'tp-carteira/v1', '/extrato', [
        'methods'             => 'GET',
        'callback'            => 'tpc_endpoint_extrato',
        'permission_callback' => $auth,
        'args'                => [
            'tipo'     => [ 'type' => 'string', 'enum' => [ 'credito', 'debito', '' ], 'default' => '' ],
            'status'   => [ 'type' => 'string', 'default' => '' ],
            'data_ini' => [ 'type' => 'string', 'format' => 'date' ],
            'data_fim' => [ 'type' => 'string', 'format' => 'date' ],
            'per_page' => [ 'type' => 'integer', 'default' => 20, 'minimum' => 1, 'maximum' => 100 ],
            'page'     => [ 'type' => 'integer', 'default' => 1,  'minimum' => 1 ],
        ],
    ] );

    register_rest_route( 'tp-carteira/v1', '/recarregar', [
        'methods'             => 'POST',
        'callback'            => 'tpc_endpoint_recarregar',
        'permission_callback' => $auth,
        'args'                => [
            'valor' => [ 'type' => 'number', 'required' => true, 'minimum' => 10 ],
        ],
    ] );

    register_rest_route( 'tp-carteira/v1', '/recarga/(?P<recarga_id>\d+)/pix', [
        'methods'             => 'GET',
        'callback'            => 'tpc_endpoint_pix_status',
        'permission_callback' => $auth,
    ] );

    /* Admin: visão de qualquer usuário */
    register_rest_route( 'tp-carteira/v1', '/admin/usuario/(?P<user_id>\d+)/saldo', [
        'methods'             => 'GET',
        'callback'            => 'tpc_endpoint_admin_saldo',
        'permission_callback' => fn() => current_user_can( 'manage_woocommerce' ),
    ] );

    register_rest_route( 'tp-carteira/v1', '/admin/usuario/(?P<user_id>\d+)/extrato', [
        'methods'             => 'GET',
        'callback'            => 'tpc_endpoint_admin_extrato',
        'permission_callback' => fn() => current_user_can( 'manage_woocommerce' ),
    ] );
} );

/* ── Autenticação JWT ──────────────────────────────────────────────────── */

function tpc_rest_auth(): int|WP_Error {
    $header = $_SERVER['HTTP_AUTHORIZATION']
           ?? $_SERVER['REDIRECT_HTTP_AUTHORIZATION']
           ?? '';

    if ( ! $header ) {
        // Tenta também via cookie WP (painel admin embutido)
        if ( is_user_logged_in() ) return get_current_user_id();
        return new WP_Error( 'sem_token', 'Token de autorização necessário.', [ 'status' => 401 ] );
    }

    $token = '';
    if ( str_starts_with( strtolower( $header ), 'bearer ' ) ) {
        $token = trim( substr( $header, 7 ) );
    }

    if ( ! $token ) {
        return new WP_Error( 'token_invalido', 'Formato Bearer inválido.', [ 'status' => 401 ] );
    }

    // Valida JWT simples (HS256) sem biblioteca externa
    $user_id = tpc_jwt_decode( $token );

    if ( is_wp_error( $user_id ) ) return $user_id;
    if ( ! get_user_by( 'id', $user_id ) ) {
        return new WP_Error( 'usuario_invalido', 'Usuário não encontrado.', [ 'status' => 401 ] );
    }

    return $user_id;
}

/* ── JWT helpers ────────────────────────────────────────────────────────── */

/**
 * Garante que o secret do JWT existe e tem entropia mínima.
 * Use em vez de get_option direto.
 */
function tpc_jwt_get_secret(): string {
    $secret = (string) get_option( 'tpc_jwt_secret', '' );
    if ( strlen( $secret ) < 32 ) {
        $secret = wp_generate_password( 64, false );
        update_option( 'tpc_jwt_secret', $secret, false );
    }
    return $secret;
}

function tpc_jwt_encode( int $user_id, int $expires = 0 ): string {
    $secret  = tpc_jwt_get_secret();

    $header  = tpc_base64url( wp_json_encode( [ 'alg' => 'HS256', 'typ' => 'JWT' ] ) );
    $exp     = $expires ?: ( time() + (int) apply_filters( 'tpc_jwt_access_token_ttl', HOUR_IN_SECONDS ) );
    $payload = tpc_base64url( wp_json_encode( [ 'sub' => $user_id, 'iat' => time(), 'exp' => $exp ] ) );
    $sig     = tpc_base64url( hash_hmac( 'sha256', "$header.$payload", $secret, true ) );

    return "$header.$payload.$sig";
}

function tpc_jwt_decode( string $token ): int|WP_Error {
    $parts = explode( '.', $token );
    if ( count( $parts ) !== 3 ) {
        return new WP_Error( 'jwt_malformado', 'Token inválido.', [ 'status' => 401 ] );
    }

    [ $header, $payload_b64, $sig ] = $parts;

    // CRIT-05: garante secret com entropia. Se ainda for vazio mesmo após o ensure,
    // recusa em vez de validar contra HMAC com chave vazia.
    $secret = tpc_jwt_get_secret();
    if ( strlen( $secret ) < 32 ) {
        return new WP_Error( 'jwt_servidor', 'Configuração de autenticação ausente.', [ 'status' => 503 ] );
    }

    // CRIT-05: valida que o header declara HS256 — bloqueia tokens com alg=none ou alg trocado.
    $header_decoded = json_decode( base64_decode( strtr( $header, '-_', '+/' ) ), true );
    if ( ! is_array( $header_decoded )
         || ( $header_decoded['alg'] ?? '' ) !== 'HS256'
         || ( $header_decoded['typ'] ?? 'JWT' ) !== 'JWT' ) {
        return new WP_Error( 'jwt_alg', 'Algoritmo do token inválido.', [ 'status' => 401 ] );
    }

    $expected = tpc_base64url( hash_hmac( 'sha256', "$header.$payload_b64", $secret, true ) );

    if ( ! hash_equals( $expected, $sig ) ) {
        return new WP_Error( 'jwt_assinatura', 'Assinatura inválida.', [ 'status' => 401 ] );
    }

    $payload = json_decode( base64_decode( strtr( $payload_b64, '-_', '+/' ) ), true );

    if ( empty( $payload['sub'] ) || empty( $payload['exp'] ) ) {
        return new WP_Error( 'jwt_payload', 'Payload inválido.', [ 'status' => 401 ] );
    }

    if ( $payload['exp'] < time() ) {
        return new WP_Error( 'jwt_expirado', 'Token expirado.', [ 'status' => 401 ] );
    }

    return (int) $payload['sub'];
}

/**
 * Hook de ativação: gera secret JWT antes do primeiro request.
 * Chamado pelo register_activation_hook em senderzz-logistics.php.
 */
function tpc_jwt_ensure_on_activation(): void {
    tpc_jwt_get_secret();
}
add_action( 'admin_init', 'tpc_jwt_ensure_on_activation' );

function tpc_base64url( string $data ): string {
    return rtrim( strtr( base64_encode( $data ), '+/', '-_' ), '=' );
}

/* ── Endpoints ──────────────────────────────────────────────────────────── */

function tpc_endpoint_me( WP_REST_Request $req ): WP_REST_Response {
    $user_id = tpc_rest_auth();
    $user    = get_user_by( 'id', $user_id );

    return new WP_REST_Response( [
        'id'         => $user_id,
        'nome'       => $user->display_name,
        'saldo'      => tpc_get_saldo( $user_id ),
        'saldo_reservado' => function_exists( 'tpc_get_saldo_reservado' ) ? tpc_get_saldo_reservado( $user_id ) : 0,
        'saldo_disponivel' => function_exists( 'tpc_get_saldo_disponivel' ) ? tpc_get_saldo_disponivel( $user_id ) : tpc_get_saldo( $user_id ),
        'saldo_fmt'  => 'R$ ' . number_format( tpc_get_saldo( $user_id ), 2, ',', '.' ),
    ] );
}

function tpc_endpoint_saldo( WP_REST_Request $req ): WP_REST_Response {
    $user_id = tpc_rest_auth();
    $saldo   = tpc_get_saldo( $user_id );

    return new WP_REST_Response( [
        'saldo' => $saldo,
        'saldo_reservado' => function_exists( 'tpc_get_saldo_reservado' ) ? tpc_get_saldo_reservado( $user_id ) : 0,
        'saldo_disponivel' => function_exists( 'tpc_get_saldo_disponivel' ) ? tpc_get_saldo_disponivel( $user_id ) : $saldo,
        'saldo_fmt' => 'R$ ' . number_format( $saldo, 2, ',', '.' ),
    ] );
}

function tpc_endpoint_extrato( WP_REST_Request $req ): WP_REST_Response {
    $user_id = tpc_rest_auth();
    $args    = [
        'tipo'     => $req->get_param( 'tipo' ),
        'status'   => $req->get_param( 'status' ),
        'data_ini' => $req->get_param( 'data_ini' ),
        'data_fim' => $req->get_param( 'data_fim' ),
        'per_page' => $req->get_param( 'per_page' ),
        'page'     => $req->get_param( 'page' ),
    ];

    $transacoes = tpc_get_transacoes( $user_id, $args );
    $total      = tpc_count_transacoes( $user_id, $args );

    // Formata valores para o painel
    $transacoes = array_map( function ( $t ) {
        $t['valor_fmt']     = 'R$ ' . number_format( (float) $t['valor'],    2, ',', '.' );
        $t['saldo_apos_fmt'] = 'R$ ' . number_format( (float) $t['saldo_apos'], 2, ',', '.' );
        return $t;
    }, $transacoes );

    return new WP_REST_Response( [
        'data'        => $transacoes,
        'total'       => $total,
        'per_page'    => (int) $args['per_page'],
        'page'        => (int) $args['page'],
        'total_pages' => (int) ceil( $total / $args['per_page'] ),
        'saldo_atual' => tpc_get_saldo( $user_id ),
    ] );
}

function tpc_endpoint_recarregar( WP_REST_Request $req ): WP_REST_Response {
    $user_id = tpc_rest_auth();
    $valor   = (float) $req->get_param( 'valor' );

    $recarga_id = tpc_criar_recarga_pendente( $user_id, $valor, 'Recarga via PIX pelo painel' );

    if ( ! $recarga_id ) {
        return new WP_REST_Response( [ 'error' => 'Não foi possível criar a recarga interna.' ], 500 );
    }

    $pix = tpc_gerar_pix_melhor_envio( $recarga_id );

    if ( is_wp_error( $pix ) ) {
        tpc_cancelar_recarga_pendente( $recarga_id );
        return new WP_REST_Response( [ 'error' => $pix->get_error_message() ], 500 );
    }

    return new WP_REST_Response( [
        'recarga_id' => $recarga_id,
        'valor'      => $valor,
        'valor_fmt'  => 'R$ ' . number_format( $valor, 2, ',', '.' ),
        'pix'        => $pix,
        'expira_em'  => $pix['expira_em'],
    ], 201 );
}



/* ── Admin endpoints ────────────────────────────────────────────────────── */

function tpc_endpoint_admin_saldo( WP_REST_Request $req ): WP_REST_Response {
    $user_id = (int) $req->get_param( 'user_id' );
    return new WP_REST_Response( [ 'user_id' => $user_id, 'saldo' => tpc_get_saldo( $user_id ) ] );
}

function tpc_endpoint_admin_extrato( WP_REST_Request $req ): WP_REST_Response {
    $user_id    = (int) $req->get_param( 'user_id' );
    $transacoes = tpc_get_transacoes( $user_id, $req->get_params() );
    return new WP_REST_Response( [ 'data' => $transacoes, 'user_id' => $user_id ] );
}

/* ── Gera token JWT para login externo ──────────────────────────────────── */

add_action( 'rest_api_init', function () {
    register_rest_route( 'tp-carteira/v1', '/auth/token', [
        'methods'             => 'POST',
        'callback'            => 'tpc_endpoint_auth_token',
        'permission_callback' => '__return_true',
        'args'                => [
            'username' => [ 'type' => 'string', 'required' => true ],
            'password' => [ 'type' => 'string', 'required' => true ],
        ],
    ] );

    register_rest_route( 'tp-carteira/v1', '/auth/token-from-portal-session', [
        'methods'             => 'POST',
        'callback'            => 'tpc_endpoint_auth_token_from_portal_session',
        'permission_callback' => '__return_true',
    ] );
} );

function tpc_endpoint_auth_token( WP_REST_Request $req ): WP_REST_Response {
    // Rate limit: máximo 10 tentativas por IP a cada 5 minutos (anti brute force).
    $ip       = (string) ( $_SERVER['REMOTE_ADDR'] ?? 'unknown' );
    $rl_key   = 'tpc_auth_rl_' . md5( $ip );
    $attempts = (int) get_transient( $rl_key );
    if ( $attempts >= 10 ) {
        return new WP_REST_Response( [ 'error' => 'Muitas tentativas. Aguarde alguns minutos.' ], 429 );
    }
    set_transient( $rl_key, $attempts + 1, 5 * MINUTE_IN_SECONDS );

    $user = wp_authenticate(
        sanitize_user( $req->get_param( 'username' ) ),
        $req->get_param( 'password' )
    );

    if ( is_wp_error( $user ) ) {
        return new WP_REST_Response( [ 'error' => 'Credenciais inválidas.' ], 401 );
    }

    // Sucesso: reseta contador de tentativas.
    delete_transient( $rl_key );

    $token = tpc_jwt_encode( $user->ID );

    return new WP_REST_Response( [
        'token'      => $token,
        'user_id'    => $user->ID,
        'nome'       => $user->display_name,
        'email'      => $user->user_email,
        'expires_in' => (int) apply_filters( 'tpc_jwt_access_token_ttl', HOUR_IN_SECONDS ),
    ] );
}

/**
 * Gera JWT a partir da sessão ativa do painel logístico Senderzz.
 */
function tpc_endpoint_auth_token_from_portal_session( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;

    $session_token = '';
    if ( ! empty( $_COOKIE['senderzz_portal_session'] ) ) {
        $session_token = sanitize_text_field( wp_unslash( $_COOKIE['senderzz_portal_session'] ) );
    }
    if ( ! $session_token ) {
        $header_token = (string) $req->get_header( 'x-senderzz-token' );
        if ( $header_token ) {
            $session_token = sanitize_text_field( $header_token );
        }
    }

    if ( ! $session_token || ! preg_match( '/^[a-f0-9]{64}$/i', $session_token ) ) {
        return new WP_REST_Response( [ 'error' => 'Sessão do painel ausente ou inválida.' ], 401 );
    }

    $sessions_table = $wpdb->prefix . 'senderzz_portal_sessions';
    $users_table    = $wpdb->prefix . 'senderzz_portal_users';

    // V-SEC-02: sessões novas armazenam HMAC do token — comparar raw+hash (suporte a sessões legacy)
    $session_token_hash = class_exists( '\\WC_MelhorEnvio\\Portal\\Portal_Auth' )
        ? \WC_MelhorEnvio\Portal\Portal_Auth::hash_session_token( $session_token )
        : hash_hmac( 'sha256', $session_token, defined( 'AUTH_SALT' ) && AUTH_SALT ? AUTH_SALT : wp_salt( 'auth' ) );

    $row = $wpdb->get_row( $wpdb->prepare(
        "SELECT u.id AS portal_user_id, u.email, u.name, u.role, u.status, u.wp_user_id, u.shipping_class_id, s.token, s.expires_at
           FROM {$sessions_table} s
           INNER JOIN {$users_table} u ON u.id = s.user_id
          WHERE s.token IN (%s,%s)
          LIMIT 1",
        $session_token, $session_token_hash
    ) );

    if ( ! $row || ( ! hash_equals( (string) $row->token, $session_token ) && ! hash_equals( (string) $row->token, $session_token_hash ) ) ) {
        return new WP_REST_Response( [ 'error' => 'Sessão do painel inválida.' ], 401 );
    }

    if ( ! empty( $row->expires_at ) && strtotime( (string) $row->expires_at . ' UTC' ) < time() ) {
        return new WP_REST_Response( [ 'error' => 'Sessão do painel expirada.' ], 401 );
    }

    if ( isset( $row->status ) && (string) $row->status !== 'active' ) {
        return new WP_REST_Response( [ 'error' => 'Usuário do painel inativo.' ], 403 );
    }

    $wp_user_id = absint( $row->wp_user_id ?? 0 );
    if ( ! $wp_user_id && ! empty( $row->email ) ) {
        $wp_user = get_user_by( 'email', sanitize_email( (string) $row->email ) );
        if ( $wp_user ) {
            $wp_user_id = (int) $wp_user->ID;
            $wpdb->update( $users_table, [ 'wp_user_id' => $wp_user_id ], [ 'id' => (int) $row->portal_user_id ], [ '%d' ], [ '%d' ] );
        }
    }

    if ( ! $wp_user_id || ! get_user_by( 'id', $wp_user_id ) ) {
        return new WP_REST_Response( [
            'error' => 'Usuário do painel sem usuário WordPress/carteira vinculada.',
            'hint'  => 'Vincule este cliente logístico a um usuário WordPress na carteira/classe de entrega.',
        ], 403 );
    }

    return new WP_REST_Response( [
        'token'             => tpc_jwt_encode( $wp_user_id ),
        'user_id'           => $wp_user_id,
        'portal_user_id'    => (int) $row->portal_user_id,
        'shipping_class_id' => (int) $row->shipping_class_id,
        'nome'              => (string) ( $row->name ?: $row->email ),
        'email'             => (string) $row->email,
        'role'              => (string) ( $row->role ?: 'client' ),
        'expires_in'        => (int) apply_filters( 'tpc_jwt_access_token_ttl', HOUR_IN_SECONDS ),
    ] );
}

/* pix_status já registrado no bloco principal de rest_api_init acima */

function tpc_endpoint_pix_status( WP_REST_Request $req ): WP_REST_Response {
    $user_id    = tpc_rest_auth();
    if ( is_wp_error( $user_id ) ) return new WP_REST_Response( [ 'error' => 'Não autenticado.' ], 401 );

    $recarga_id = (int) $req->get_param( 'recarga_id' );
    $recarga    = function_exists( 'tpc_get_recarga' ) ? tpc_get_recarga( $recarga_id ) : null;

    if ( ! $recarga || (int) $recarga['user_id'] !== $user_id ) {
        return new WP_REST_Response( [ 'error' => 'Recarga não encontrada.' ], 404 );
    }

    $pago = $recarga['status'] === 'confirmado';
    return new WP_REST_Response( [
        'recarga_id'  => $recarga_id,
        'status'      => $recarga['status'],
        'pago'        => $pago,
        'em_analise'  => $recarga['status'] === 'analise',
        'status_label' => match( $recarga['status'] ) {
            'confirmado' => 'Confirmado',
            'analise'    => 'Em análise — aguardando confirmação do banco',
            'cancelado'  => 'Cancelado',
            'expirado'   => 'Expirado',
            default      => 'Aguardando pagamento',
        },
        'qr_src'     => $recarga['qr_src']     ?? '',
        'copia_cola' => $recarga['copia_cola']  ?? '',
        'link'       => $recarga['link']        ?? '',
        'expira_em'  => $recarga['expires_at']  ?? '',
        'expires_ts' => $recarga['expires_at'] ? strtotime( $recarga['expires_at'] ) : 0,
        'saldo'      => $pago ? tpc_get_saldo( $user_id ) : null,
        'saldo_fmt'  => $pago ? ( 'R$ ' . number_format( tpc_get_saldo( $user_id ), 2, ',', '.' ) ) : null,
    ] );
}
