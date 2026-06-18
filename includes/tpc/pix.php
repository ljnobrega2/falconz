<?php
/**
 * pix.php — Recarga via PIX integrado ao Melhor Envio
 *
 * Arquitetura v2.5.18 (escala 1000+ clientes):
 *
 *  PROBLEMA RESOLVIDO: confirmação por delta de saldo ME é insegura com
 *  múltiplos clientes simultâneos. A solução correta é rastrear cada PIX
 *  pelo seu me_pix_id único retornado pelo ME, com UNIQUE KEY no DB.
 *
 *  FLUXO:
 *   1. tpc_criar_recarga()     — cria registro na tpc_recargas (pendente)
 *   2. tpc_gerar_pix_me()      — POST /me/balance → salva me_pix_id, QR, token
 *   3. redirect_url            — ME redireciona o browser após pagamento
 *      → tpc_endpoint_pix_retorno() valida token HMAC + confirma via me_pix_id
 *   4. "Já paguei"             — cliente clica → força verificação imediata
 *      → tpc_endpoint_pix_confirmar() → mesmo fluxo que redirect_url
 *   5. Cron fallback           — varre recargas pendentes antigas (>15min)
 *      → usa me_pix_id para confirmar, sem comparar saldo global
 *   6. Webhook                 — se ME chamar /webhook/pix com me_pix_id
 *
 *  SEGURANÇA:
 *   - me_pix_id UNIQUE no DB: dois processos não confirmam a mesma recarga
 *   - SELECT FOR UPDATE em tpc_confirmar_recarga: thread-safe
 *   - Token HMAC no redirect_url: terceiros não podem confirmar recargas
 *   - Rate limit no endpoint "Já paguei": máx 5 cliques/min por usuário
 *   - Lock por recarga no cron: evita processamento paralelo
 */
defined( 'ABSPATH' ) || exit;
if ( defined( 'SENDERZZ_TPC_PIX_LOADED' ) ) return;
define( 'SENDERZZ_TPC_PIX_LOADED', true );

// ─────────────────────────────────────────────────────────────────────────────
// CRUD — tpc_recargas
// ─────────────────────────────────────────────────────────────────────────────

function tpc_criar_recarga( int $user_id, float $valor, string $motivo = 'Recarga via PIX' ): int|false {
    global $wpdb;
    if ( $valor < 10 || ! $user_id ) return false;

    $ok = $wpdb->insert(
        $wpdb->prefix . 'tpc_recargas',
        [
            'user_id'    => $user_id,
            'valor'      => $valor,
            'status'     => 'pendente',
            'ip_address' => $_SERVER['REMOTE_ADDR'] ?? null,
            'expires_at' => gmdate( 'Y-m-d H:i:s', time() + tpc_pix_validade_minutos() * MINUTE_IN_SECONDS ),
        ],
        [ '%d', '%f', '%s', '%s', '%s' ]
    );
    if ( ! $ok ) {
        tpc_pix_log( 'recarga_error', [ 'user_id' => $user_id, 'db' => $wpdb->last_error ] );
        return false;
    }
    $id = (int) $wpdb->insert_id;
    tpc_pix_log( 'recarga_created', [ 'recarga_id' => $id, 'user_id' => $user_id, 'valor' => $valor ] );
    // Double-write Go (Fase 2)
    do_action( 'tpc_recarga_criada', $id, $user_id, [ 'valor' => $valor, 'status' => 'pendente' ] );
    return $id;
}

// Backward-compat alias (usada em admin.php e rest-api.php)
function tpc_criar_recarga_pendente( int $user_id, float $valor, string $motivo = 'Recarga via PIX' ): int|false {
    return tpc_criar_recarga( $user_id, $valor, $motivo );
}

function tpc_get_recarga( int $recarga_id ): ?array {
    global $wpdb;
    $row = $wpdb->get_row( $wpdb->prepare(
        "SELECT * FROM {$wpdb->prefix}tpc_recargas WHERE id = %d",
        $recarga_id
    ), ARRAY_A );
    return $row ?: null;
}

function tpc_cancelar_recarga( int $recarga_id ): bool {
    global $wpdb;
    $ok = $wpdb->update(
        $wpdb->prefix . 'tpc_recargas',
        [ 'status' => 'cancelado' ],
        [ 'id' => $recarga_id, 'status' => 'pendente' ],
        [ '%s' ], [ '%d', '%s' ]
    );
    // Tenta também cancelar analise
    if ( ! $ok ) {
        $wpdb->update(
            $wpdb->prefix . 'tpc_recargas',
            [ 'status' => 'cancelado' ],
            [ 'id' => $recarga_id, 'status' => 'analise' ],
            [ '%s' ], [ '%d', '%s' ]
        );
    }
    tpc_pix_log( 'recarga_cancel', [ 'recarga_id' => $recarga_id ] );
    return (bool) $ok;
}

// Backward-compat alias
function tpc_cancelar_recarga_pendente( int $recarga_id ): bool {
    return tpc_cancelar_recarga( $recarga_id );
}

// ─────────────────────────────────────────────────────────────────────────────
// CONFIRMAÇÃO — thread-safe, idempotente, escalável
// ─────────────────────────────────────────────────────────────────────────────

/**
 * Confirma uma recarga e credita o saldo do usuário.
 * Usa SELECT FOR UPDATE + UNIQUE KEY me_pix_id para garantir que dois
 * processos paralelos (cron + webhook + redirect) não creditam duas vezes.
 *
 * @param int    $recarga_id  ID na tpc_recargas
 * @param string $origem      Para log: 'redirect', 'ja_paguei', 'webhook', 'cron'
 */
function tpc_confirmar_recarga( int $recarga_id, string $origem = 'unknown' ): bool {
    global $wpdb;

    // Guard fail-closed: aplica o filtro definido em pix-confirmation-guard.php.
    // Qualquer chamada direta passa por aqui — não há como bypassar.
    // Origens não autorizadas (cron, redirect, unknown) são bloqueadas pelo filtro.
    // Somente 'webhook' (Yapay HMAC validado) e 'admin_reconciliation' passam.
    if ( has_filter( 'senderzz_pix_pre_confirmar_recarga' ) ) {
        $allow = apply_filters( 'senderzz_pix_pre_confirmar_recarga', true, $recarga_id, $origem );
        if ( ! $allow ) {
            tpc_pix_log( 'recarga_confirm_blocked_by_guard', [ 'recarga_id' => $recarga_id, 'origem' => $origem ] );
            return false;
        }
    }

    // Lock por recarga: evita que dois processos entrem na transação ao mesmo tempo.
    $lock_key = 'tpc_confirm_lock_' . $recarga_id;
    if ( get_transient( $lock_key ) ) {
        tpc_pix_log( 'recarga_confirm_locked', [ 'recarga_id' => $recarga_id, 'origem' => $origem ] );
        return false;
    }
    set_transient( $lock_key, 1, 30 );

    try {
        $wpdb->query( 'START TRANSACTION' );

        // SELECT FOR UPDATE: bloqueia a linha para leitura consistente.
        $recarga = $wpdb->get_row( $wpdb->prepare(
            "SELECT * FROM {$wpdb->prefix}tpc_recargas WHERE id = %d FOR UPDATE",
            $recarga_id
        ), ARRAY_A );

        if ( ! $recarga ) {
            $wpdb->query( 'ROLLBACK' );
            return false;
        }

        // Idempotência: já confirmada.
        if ( $recarga['status'] === 'confirmado' ) {
            $wpdb->query( 'COMMIT' );
            tpc_pix_log( 'recarga_confirm_skip', [ 'recarga_id' => $recarga_id, 'note' => 'já confirmada' ] );
            return true;
        }

        if ( ! in_array( $recarga['status'], [ 'pendente', 'analise' ], true ) ) {
            $wpdb->query( 'ROLLBACK' );
            return false;
        }

        $user_id = (int) $recarga['user_id'];
        $valor   = (float) $recarga['valor'];

        // Garante linha na carteira e bloqueia para escrita.
        $wpdb->query( $wpdb->prepare(
            "INSERT INTO {$wpdb->prefix}tpc_carteira (user_id, saldo, saldo_reservado)
             VALUES (%d, 0.00, 0.00)
             ON DUPLICATE KEY UPDATE id = id",
            $user_id
        ) );
        $carteira = $wpdb->get_row( $wpdb->prepare(
            "SELECT saldo FROM {$wpdb->prefix}tpc_carteira WHERE user_id = %d FOR UPDATE",
            $user_id
        ) );
        $saldo_atual = $carteira ? (float) $carteira->saldo : 0.00;
        $saldo_novo  = round( $saldo_atual + $valor, 2 );

        // Atualiza carteira.
        $ok_carteira = $wpdb->update(
            $wpdb->prefix . 'tpc_carteira',
            [ 'saldo' => $saldo_novo ],
            [ 'user_id' => $user_id ],
            [ '%f' ], [ '%d' ]
        );

        // Insere transação no ledger.
        $ok_tx = $wpdb->insert(
            $wpdb->prefix . 'tpc_transacoes',
            [
                'user_id'     => $user_id,
                'tipo'        => 'credito',
                'valor'       => $valor,
                'saldo_apos'  => $saldo_novo,
                'descricao'   => 'Recarga via PIX #' . $recarga_id,
                'referencia'  => 'recarga_pix_confirmada',
                'status'      => 'confirmado',
                'me_order_id' => $recarga['me_pix_id'] ?? null,
                'meta_json'   => wp_json_encode( [ 'recarga_id' => $recarga_id, 'origem' => $origem ], JSON_UNESCAPED_UNICODE ),
            ],
            [ '%d', '%s', '%f', '%f', '%s', '%s', '%s', '%s', '%s' ]
        );
        $tx_id = $ok_tx ? (int) $wpdb->insert_id : 0;

        // Atualiza recarga.
        $ok_recarga = $wpdb->update(
            $wpdb->prefix . 'tpc_recargas',
            [ 'status' => 'confirmado', 'tx_id' => $tx_id ],
            [ 'id' => $recarga_id ],
            [ '%s', '%d' ], [ '%d' ]
        );

        if ( false === $ok_carteira || ! $ok_tx || false === $ok_recarga ) {
            $wpdb->query( 'ROLLBACK' );
            tpc_pix_log( 'recarga_confirm_error', [ 'recarga_id' => $recarga_id, 'db' => $wpdb->last_error ] );
            return false;
        }

        $wpdb->query( 'COMMIT' );
        tpc_pix_log( 'recarga_confirmed', [ 'recarga_id' => $recarga_id, 'user_id' => $user_id, 'valor' => $valor, 'saldo_apos' => $saldo_novo, 'origem' => $origem ] );
        do_action( 'tpc_recarga_confirmada', $recarga_id, $user_id, $valor );
        tpc_notificar_recarga_confirmada( $recarga_id );
        return true;

    } finally {
        delete_transient( $lock_key );
    }
}

function tpc_marcar_recarga_analise( int $recarga_id, array $dados = [] ): bool {
    global $wpdb;
    $ok = $wpdb->update(
        $wpdb->prefix . 'tpc_recargas',
        [
            'status'    => 'analise',
            'meta_json' => wp_json_encode( array_merge( [ 'analise_at' => current_time( 'mysql' ) ], $dados ), JSON_UNESCAPED_UNICODE ),
        ],
        [ 'id' => $recarga_id, 'status' => 'pendente' ],
        [ '%s', '%s' ], [ '%d', '%s' ]
    );
    tpc_pix_log( 'recarga_analise', [ 'recarga_id' => $recarga_id, 'ok' => (bool) $ok ] );
    return (bool) $ok;
}

// ─────────────────────────────────────────────────────────────────────────────
// TOKEN DE SEGURANÇA PARA redirect_url
// ─────────────────────────────────────────────────────────────────────────────

function tpc_pix_gerar_token( int $recarga_id ): string {
    $secret = get_option( 'tpc_jwt_secret', wp_generate_password( 64, false ) );
    update_option( 'tpc_jwt_secret', $secret );
    return substr( hash_hmac( 'sha256', 'pix_retorno_' . $recarga_id, $secret ), 0, 32 );
}

function tpc_pix_validar_token( int $recarga_id, string $token ): bool {
    return hash_equals( tpc_pix_gerar_token( $recarga_id ), $token );
}

// ─────────────────────────────────────────────────────────────────────────────
// API HELPERS — sandbox-aware (respeita toggle do plugin ME)
// ─────────────────────────────────────────────────────────────────────────────

function tpc_me_api_base(): string {
    $settings = get_option( 'woocommerce_wc-melhor-envio_settings', [] );
    $sandbox  = ( ( $settings['sandbox'] ?? 'no' ) === 'yes' );
    $host     = $sandbox ? 'sandbox.melhorenvio.com.br' : 'www.melhorenvio.com.br';
    return 'https://' . $host . '/api/v2';
}

function tpc_me_token(): string {
    $token = (string) get_option( 'tpc_me_token', '' );
    if ( $token ) return $token;
    $settings = get_option( 'woocommerce_wc-melhor-envio_settings', [] );
    return (string) ( $settings['client_secret'] ?? '' );
}

// ─────────────────────────────────────────────────────────────────────────────
// CONSULTA SALDO ME — GET /api/v2/me/balance
// ─────────────────────────────────────────────────────────────────────────────

function tpc_consultar_saldo_me(): float|WP_Error {
    $token = tpc_me_token();
    if ( ! $token ) return new WP_Error( 'sem_token', 'Token ME não configurado.' );
    $response = wp_remote_get( tpc_me_api_base() . '/me/balance', [
        'timeout' => 15,
        'headers' => [
            'Authorization' => 'Bearer ' . $token,
            'Accept'        => 'application/json',
            'User-Agent'    => 'Senderzz Logistics (suporte@app.senderzz.com.br)',
        ],
    ] );
    if ( is_wp_error( $response ) ) return $response;
    $code = (int) wp_remote_retrieve_response_code( $response );
    $data = json_decode( wp_remote_retrieve_body( $response ), true );
    if ( $code !== 200 || ! is_array( $data ) ) {
        return new WP_Error( 'saldo_error', 'HTTP ' . $code );
    }
    return isset( $data['balance'] ) ? (float) $data['balance'] : 0.0;
}

// ─────────────────────────────────────────────────────────────────────────────
// GERAÇÃO DO PIX VIA ME — POST /api/v2/me/balance
// ─────────────────────────────────────────────────────────────────────────────

function tpc_gerar_pix_me( int $recarga_id ): array|WP_Error {
    global $wpdb;

    $recarga = tpc_get_recarga( $recarga_id );
    if ( ! $recarga ) return new WP_Error( 'not_found', 'Recarga não encontrada.' );
    if ( $recarga['status'] !== 'pendente' ) return new WP_Error( 'invalid_status', 'Recarga não está pendente.' );

    $token = tpc_me_token();
    if ( ! $token ) return new WP_Error( 'sem_token', 'Token ME não configurado.' );

    $user_id = (int) $recarga['user_id'];
    $valor   = (float) $recarga['valor'];

    // Token de segurança para o redirect_url.
    $security_token = tpc_pix_gerar_token( $recarga_id );

    // Salva token na recarga para validação posterior.
    $wpdb->update(
        $wpdb->prefix . 'tpc_recargas',
        [ 'security_token' => $security_token ],
        [ 'id' => $recarga_id ],
        [ '%s' ], [ '%d' ]
    );

    $redirect_url = add_query_arg(
        [ 'recarga_id' => $recarga_id, 'sz_token' => $security_token ],
        rest_url( 'tp-carteira/v1/pix/retorno' )
    );

    $response = wp_remote_post( tpc_me_api_base() . '/me/balance', [
        'timeout' => 30,
        'headers' => [
            'Authorization' => 'Bearer ' . $token,
            'Content-Type'  => 'application/json',
            'Accept'        => 'application/json',
            'User-Agent'    => 'Senderzz Logistics (suporte@app.senderzz.com.br)',
        ],
        'body' => wp_json_encode( [
            'gateway'      => 'yapay-transparente',
            'slug'         => 'pix',
            'value'        => number_format( $valor, 2, '.', '' ),
            'redirect_url' => $redirect_url,
        ] ),
    ] );

    if ( is_wp_error( $response ) ) {
        tpc_pix_log( 'pix_error', [ 'recarga_id' => $recarga_id, 'error' => $response->get_error_message() ] );
        return $response;
    }

    $code = (int) wp_remote_retrieve_response_code( $response );
    $data = json_decode( wp_remote_retrieve_body( $response ), true );
    if ( ! is_array( $data ) ) $data = [];
    $data = tpc_pix_expand_json_strings( $data );

    tpc_pix_log( 'pix_response', [ 'recarga_id' => $recarga_id, 'code' => $code ] );

    if ( $code !== 200 && $code !== 201 ) {
        return new WP_Error( 'me_erro_' . $code, $data['message'] ?? ( $data['error'] ?? 'Erro ao gerar PIX.' ), [ 'code' => $code ] );
    }

    // Extrai campos do retorno.
    $pix_id     = (string) ( tpc_pix_find_first( $data, [ 'pix_id', 'payment_id', 'transaction_id', 'transactionId', 'id', 'charge_id' ] ) ?? '' );
    $qr_code    = (string) ( tpc_pix_find_first( $data, [ 'qr_code', 'qrcode', 'qrCode', 'qrcode_base64', 'qr_code_base64', 'image' ] ) ?? '' );
    $copia_cola = (string) ( tpc_pix_find_first( $data, [ 'copy_paste', 'copyPaste', 'copia_cola', 'pix_code', 'digitable', 'emv', 'brcode', 'payload' ] ) ?? '' );
    $link       = (string) ( tpc_pix_find_first( $data, [ 'link', 'redirect', 'url_payment', 'url', 'payment_url', 'paymentUrl', 'checkout_url' ] ) ?? '' );
    $expira_em  = (string) ( tpc_pix_find_first( $data, [ 'expire_at', 'expires_at', 'expiration_date', 'due_date', 'max_days_to_keep_waiting_payment' ] ) ?? tpc_pix_default_expiration() );

    $qr_src = tpc_pix_normalize_img_src( $qr_code );
    if ( ! $qr_src && $copia_cola ) $qr_src = tpc_pix_qr_from_copy_paste( $copia_cola );

    $expires_at = tpc_pix_timestamp( $expira_em );
    $expires_db = $expires_at > 0 ? gmdate( 'Y-m-d H:i:s', $expires_at ) : null;

    // Persiste dados na recarga.
    $wpdb->update(
        $wpdb->prefix . 'tpc_recargas',
        [
            'me_pix_id'  => $pix_id ?: null,
            'qr_src'     => $qr_src,
            'copia_cola' => $copia_cola,
            'link'       => $link,
            'expires_at' => $expires_db,
        ],
        [ 'id' => $recarga_id ],
        [ '%s', '%s', '%s', '%s', '%s' ], [ '%d' ]
    );

    $result = [
        'recarga_id' => $recarga_id,
        'pix_id'     => $pix_id,
        'qr_src'     => $qr_src,
        'copia_cola' => $copia_cola,
        'link'       => $link,
        'expira_em'  => $expira_em,
        'expires_ts' => $expires_at,
    ];

    tpc_pix_log( 'pix_gerado', [ 'recarga_id' => $recarga_id, 'pix_id' => $pix_id, 'has_qr' => (bool) $qr_src, 'has_copia' => (bool) $copia_cola ] );
    return $result;
}

// Backward-compat alias (usada em admin.php / gateway WC)
function tpc_gerar_pix_melhor_envio( $source ): array|WP_Error {
    if ( is_object( $source ) && method_exists( $source, 'get_id' ) ) {
        // Pedido WooCommerce — cria recarga e gera PIX.
        $recarga_id = tpc_criar_recarga( (int) $source->get_customer_id(), (float) $source->get_total(), 'Recarga WC #' . $source->get_id() );
        if ( ! $recarga_id ) return new WP_Error( 'recarga_error', 'Não foi possível criar recarga.' );
        return tpc_gerar_pix_me( $recarga_id );
    }
    $id = is_array( $source ) ? (int) ( $source['id'] ?? 0 ) : (int) $source;
    return tpc_gerar_pix_me( $id );
}

// ─────────────────────────────────────────────────────────────────────────────
// ENDPOINT: RETORNO DO redirect_url (ME redireciona o browser aqui)
// GET /wp-json/tp-carteira/v1/pix/retorno?recarga_id=X&sz_token=HASH
// ─────────────────────────────────────────────────────────────────────────────

add_action( 'rest_api_init', function () {
    register_rest_route( 'tp-carteira/v1', '/pix/retorno', [
        'methods'             => [ 'GET', 'POST' ],
        'callback'            => 'tpc_endpoint_pix_retorno',
        'permission_callback' => '__return_true',
    ] );
} );

function tpc_endpoint_pix_retorno( WP_REST_Request $request ): void {
    $recarga_id = (int) ( $request->get_param( 'recarga_id' ) ?: 0 );
    $sz_token   = sanitize_text_field( (string) ( $request->get_param( 'sz_token' ) ?: '' ) );
    $dest       = wc_get_account_endpoint_url( 'carteira' );

    if ( $recarga_id && $sz_token ) {
        if ( ! tpc_pix_validar_token( $recarga_id, $sz_token ) ) {
            tpc_pix_log( 'retorno_token_invalido', [ 'recarga_id' => $recarga_id ] );
        } else {
            // Guard fail-closed: redirect não é origem autorizada.
            // Apenas registra a chegada — o crédito virá via webhook Yapay.
            tpc_pix_log( 'retorno_redirect_recebido', [ 'recarga_id' => $recarga_id, 'nota' => 'aguardando webhook Yapay para confirmar' ] );
        }
        $dest = add_query_arg( 'recarga_id', $recarga_id, $dest );
    }

    wp_safe_redirect( $dest );
    exit;
}

// ─────────────────────────────────────────────────────────────────────────────
// ENDPOINT: "JÁ PAGUEI" — apenas registra que o cliente afirma ter pago
// e retorna mensagem de prazo. NÃO credita saldo. O crédito ocorre somente
// quando tivermos confirmação real: redirect_url, webhook ou cron.
// POST /wp-json/tp-carteira/v1/pix/{recarga_id}/ja-paguei
// ─────────────────────────────────────────────────────────────────────────────

add_action( 'rest_api_init', function () {
    register_rest_route( 'tp-carteira/v1', '/pix/(?P<recarga_id>\d+)/ja-paguei', [
        'methods'             => 'POST',
        'callback'            => 'tpc_endpoint_pix_ja_paguei',
        'permission_callback' => function () { return ! is_wp_error( tpc_rest_auth() ); },
    ] );
} );

function tpc_endpoint_pix_ja_paguei( WP_REST_Request $request ): WP_REST_Response {
    $user_id    = tpc_rest_auth();
    if ( is_wp_error( $user_id ) ) return new WP_REST_Response( [ 'error' => 'Não autenticado.' ], 401 );

    $recarga_id = (int) $request->get_param( 'recarga_id' );
    $recarga    = tpc_get_recarga( $recarga_id );

    if ( ! $recarga || (int) $recarga['user_id'] !== $user_id ) {
        return new WP_REST_Response( [ 'error' => 'Recarga não encontrada.' ], 404 );
    }

    // Apenas registra a intenção no log para rastreabilidade.
    tpc_pix_log( 'ja_paguei', [
        'recarga_id' => $recarga_id,
        'user_id'    => $user_id,
        'status'     => $recarga['status'],
        'ip'         => $_SERVER['REMOTE_ADDR'] ?? '',
    ] );

    // Se por acaso já foi confirmado pelos mecanismos automáticos, avisa o cliente.
    if ( $recarga['status'] === 'confirmado' ) {
        return new WP_REST_Response( [
            'ok'        => true,
            'confirmado' => true,
            'mensagem'  => 'Seu pagamento já foi confirmado! Verifique seu saldo.',
        ], 200 );
    }

    // Retorna mensagem de prazo — sem creditar nada.
    return new WP_REST_Response( [
        'ok'        => true,
        'confirmado' => false,
        'mensagem'  => 'Recebemos sua confirmação. O crédito será processado em até 30 minutos. Em casos de análise pelo banco, pode levar até 24 horas.',
    ], 200 );
}

// ─────────────────────────────────────────────────────────────────────────────
// CRON FALLBACK — varre recargas pendentes antigas
// Roda a cada 5 min, processa apenas recargas com mais de 15 min sem confirmação.
// ─────────────────────────────────────────────────────────────────────────────

add_filter( 'cron_schedules', function ( array $s ): array {
    if ( ! isset( $s['tpc_5min'] ) ) {
        $s['tpc_5min'] = [ 'interval' => 5 * MINUTE_IN_SECONDS, 'display' => 'TPC a cada 5 minutos' ];
    }
    return $s;
} );

add_action( 'init', function () {
    if ( ! wp_next_scheduled( 'tpc_cron_verificar_recargas_pix' ) ) {
        wp_schedule_event( time() + 60, 'tpc_5min', 'tpc_cron_verificar_recargas_pix' );
    }
} );

add_action( 'tpc_cron_verificar_recargas_pix', function () {
    if ( get_transient( 'tpc_pix_cron_lock' ) ) return;
    set_transient( 'tpc_pix_cron_lock', 1, 4 * MINUTE_IN_SECONDS );
    try {
        tpc_processar_recargas_pendentes( 30, 'cron' );
    } finally {
        delete_transient( 'tpc_pix_cron_lock' );
    }
} );

function tpc_processar_recargas_pendentes( int $limit = 30, string $origem = 'manual' ): array {
    global $wpdb;

    // Só processa recargas com mais de 15 min (redirect_url + botão "já paguei"
    // cuidam das recentes). Evita processar o que o frontend já tratou.
    $threshold = gmdate( 'Y-m-d H:i:s', time() - 15 * MINUTE_IN_SECONDS );

    $rows = $wpdb->get_results( $wpdb->prepare(
        "SELECT * FROM {$wpdb->prefix}tpc_recargas
         WHERE status IN ('pendente','analise')
           AND created_at < %s
         ORDER BY created_at ASC
         LIMIT %d",
        $threshold,
        max( 1, min( 50, $limit ) )
    ), ARRAY_A ) ?: [];

    $resultado = [ 'origem' => $origem, 'total' => count( $rows ), 'confirmadas' => 0, 'canceladas' => 0, 'analises' => 0, 'sem_pix_id' => 0, 'erros' => 0 ];

    foreach ( $rows as $recarga ) {
        $recarga_id = (int) $recarga['id'];
        $me_pix_id  = (string) ( $recarga['me_pix_id'] ?? '' );

        // Expirada? Cancela.
        if ( $recarga['expires_at'] && strtotime( $recarga['expires_at'] ) < time() ) {
            tpc_cancelar_recarga( $recarga_id );
            $resultado['canceladas']++;
            continue;
        }

        // Sem me_pix_id: o PIX não foi nem gerado (erro na criação).
        // Não temos como confirmar — cancela se expirou, senão deixa.
        if ( $me_pix_id === '' ) {
            $resultado['sem_pix_id']++;
            continue;
        }

        // Tem me_pix_id: tenta confirmar — guard embutido em tpc_confirmar_recarga()
        // bloqueará origens não autorizadas (cron). Serve como log de tentativa.
        $ok = tpc_confirmar_recarga( $recarga_id, $origem );
        if ( $ok ) {
            $resultado['confirmadas']++;
        } else {
            // Mantém em pendente/analise para próximo ciclo.
            $resultado['analises']++;
        }
    }

    tpc_pix_log( 'pix_cron_result', $resultado );
    return $resultado;
}

// ─────────────────────────────────────────────────────────────────────────────
// NOTIFICAÇÃO
// ─────────────────────────────────────────────────────────────────────────────

function tpc_notificar_recarga_confirmada( int $recarga_id ): void {
    $recarga = tpc_get_recarga( $recarga_id );
    if ( ! $recarga ) return;
    $user      = get_user_by( 'id', (int) $recarga['user_id'] );
    $valor_fmt = 'R$ ' . number_format( (float) $recarga['valor'], 2, ',', '.' );
    $saldo_fmt = 'R$ ' . number_format( tpc_get_saldo( (int) $recarga['user_id'] ), 2, ',', '.' );
    $admin     = get_option( 'admin_email' );
    if ( $admin ) {
        wp_mail( $admin, 'Recarga PIX confirmada', "Recarga #{$recarga_id} confirmada.\nCliente: " . ( $user ? $user->display_name . ' <' . $user->user_email . '>' : 'ID ' . $recarga['user_id'] ) . "\nValor: {$valor_fmt}" );
    }
    if ( $user && $user->user_email ) {
        wp_mail( $user->user_email, 'Seu crédito foi confirmado', "Recarga #{$recarga_id} confirmada.\nValor: {$valor_fmt}\nSaldo: {$saldo_fmt}" );
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// HELPERS
// ─────────────────────────────────────────────────────────────────────────────

function tpc_pix_log( string $tipo, mixed $data ): void {
    $safe = $data;
    if ( is_array( $safe ) ) {
        array_walk_recursive( $safe, function ( &$v, $k ) {
            if ( in_array( strtolower( (string) $k ), [ 'authorization', 'token', 'access_token', 'security_token', 'sz_token' ], true ) ) $v = '***';
        } );
    }
    if ( function_exists( 'wc_get_logger' ) ) {
        wc_get_logger()->info( wp_json_encode( $safe, JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES ), [ 'source' => 'tp-carteira-' . $tipo ] );
    }
    update_option( 'tpc_last_' . sanitize_key( $tipo ), [ 'created_at' => current_time( 'mysql' ), 'data' => $safe ], false );
}

function tpc_pix_find_first( mixed $data, array $keys ): mixed {
    if ( ! is_array( $data ) ) return null;
    foreach ( $keys as $k ) {
        if ( array_key_exists( $k, $data ) && $data[$k] !== '' && $data[$k] !== null ) return $data[$k];
    }
    foreach ( $data as $v ) {
        if ( is_array( $v ) ) { $f = tpc_pix_find_first( $v, $keys ); if ( $f !== null && $f !== '' ) return $f; }
    }
    return null;
}

function tpc_pix_expand_json_strings( mixed $data ): mixed {
    if ( is_array( $data ) ) { foreach ( $data as $k => $v ) $data[$k] = tpc_pix_expand_json_strings( $v ); return $data; }
    if ( is_string( $data ) ) { $t = trim( $data ); if ( $t && ( str_starts_with( $t, '{' ) || str_starts_with( $t, '[' ) ) ) { $d = json_decode( $t, true ); if ( is_array( $d ) ) return tpc_pix_expand_json_strings( $d ); } }
    return $data;
}

function tpc_pix_normalize_img_src( string $v ): string {
    $v = trim( $v );
    if ( ! $v ) return '';
    if ( str_starts_with( $v, 'data:image' ) ) return $v;
    if ( preg_match( '#^https?://#i', $v ) ) return $v;
    if ( preg_match( '#^[A-Za-z0-9+/=\r\n]+$#', $v ) && strlen( $v ) > 80 ) return 'data:image/png;base64,' . preg_replace( '/\s+/', '', $v );
    return '';
}

function tpc_pix_qr_from_copy_paste( string $s ): string {
    return $s ? 'https://api.qrserver.com/v1/create-qr-code/?size=220x220&data=' . rawurlencode( trim( $s ) ) : '';
}

function tpc_pix_validade_minutos(): int {
    // Senderzz: recarga PIX sempre com validade de 15 minutos.
    return 15;
}

function tpc_pix_default_expiration(): string {
    return gmdate( 'Y-m-d\TH:i:s\Z', time() + tpc_pix_validade_minutos() * MINUTE_IN_SECONDS );
}

function tpc_pix_timestamp( string $d ): int {
    $d = trim( $d ); if ( ! $d ) return 0; $t = strtotime( $d ); return $t ? (int) $t : 0;
}

function tpc_pix_status_is_paid( string $s ): bool {
    return in_array( strtolower( trim( $s ) ), [ 'paid','pago','approved','aprovado','confirmed','confirmado','paid_out','success','completed','concluido','concluído','authorized','autorizado' ], true );
}

function tpc_pix_status_is_cancelled( string $s ): bool {
    return in_array( strtolower( trim( $s ) ), [ 'cancelled','canceled','cancelado','expired','expirado','failed','falhou','refused','recusado' ], true );
}

function tpc_pix_status_is_analysis( string $s, mixed $data = null ): bool {
    $n = strtolower( trim( remove_accents( $s ) ) );
    if ( in_array( $n, [ 'analysis','analise','em analise','under_review','review','aguardando analise' ], true ) ) return true;
    $h = is_array( $data ) ? strtolower( remove_accents( wp_json_encode( $data, JSON_UNESCAPED_UNICODE ) ) ) : '';
    return str_contains( $h, 'aguardando analise' ) || str_contains( $h, 'under_review' );
}

// ─────────────────────────────────────────────────────────────────────────────
// GATEWAY WOOCOMMERCE
// ─────────────────────────────────────────────────────────────────────────────

add_action( 'plugins_loaded', function () {
    if ( ! class_exists( 'WC_Payment_Gateway' ) ) return;
    class TPC_Gateway_PIX extends WC_Payment_Gateway {
        public function __construct() {
            $this->id = 'tpc_pix'; $this->method_title = 'PIX – Carteira de Frete';
            $this->has_fields = false; $this->supports = [ 'products' ];
            $this->init_form_fields(); $this->init_settings();
            $this->title = $this->get_option( 'title', 'PIX' );
            $this->description = $this->get_option( 'description', 'Pague via PIX.' );
            $this->enabled = $this->get_option( 'enabled', 'yes' );
            add_action( 'woocommerce_update_options_payment_gateways_' . $this->id, [ $this, 'process_settings_page' ] );
        }
        public function init_form_fields(): void {
            $this->form_fields = [
                'enabled' => [ 'title' => 'Ativar', 'type' => 'checkbox', 'default' => 'yes' ],
                'title'   => [ 'title' => 'Título',  'type' => 'text',     'default' => 'PIX' ],
            ];
        }
        public function process_payment( $order_id ): array {
            $order = wc_get_order( $order_id );
            $result = tpc_gerar_pix_melhor_envio( $order );
            if ( is_wp_error( $result ) ) { wc_add_notice( 'Erro: ' . $result->get_error_message(), 'error' ); return [ 'result' => 'failure' ]; }
            return [ 'result' => 'success', 'redirect' => $order->get_checkout_order_received_url() ];
        }
    }
    add_filter( 'woocommerce_payment_gateways', fn( $g ) => array_merge( $g, [ 'TPC_Gateway_PIX' ] ) );
} );
