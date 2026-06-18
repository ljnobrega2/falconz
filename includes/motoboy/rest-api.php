<?php
/**
 * Senderzz — Módulo Motoboy
 * REST API: endpoints para o PWA do motoboy e dashboard Alan
 *
 * Base: /wp-json/sz-motoboy/v1/
 */
if ( ! defined( 'ABSPATH' ) ) exit;

add_action( 'rest_api_init', function() {

    // ── Auth ──────────────────────────────────────────────────────────────────

    // POST /login  {telefone, token_app} — legado, mantido para compatibilidade
    register_rest_route( 'sz-motoboy/v1', '/login', [
        'methods'             => 'POST',
        'callback'            => 'sz_mb_api_login',
        'permission_callback' => '__return_true',
    ] );

    // POST /login/verificar — verifica se telefone está cadastrado e se tem senha
    register_rest_route( 'sz-motoboy/v1', '/login/verificar', [
        'methods'             => 'POST',
        'callback'            => 'sz_mb_api_login_verificar',
        'permission_callback' => '__return_true',
    ] );

    // POST /login/definir-senha — primeiro acesso: define senha e loga
    register_rest_route( 'sz-motoboy/v1', '/login/definir-senha', [
        'methods'             => 'POST',
        'callback'            => 'sz_mb_api_login_definir_senha',
        'permission_callback' => '__return_true',
    ] );

    // POST /login/autenticar — login com senha existente
    register_rest_route( 'sz-motoboy/v1', '/login/autenticar', [
        'methods'             => 'POST',
        'callback'            => 'sz_mb_api_login_autenticar',
        'permission_callback' => '__return_true',
    ] );

    // POST /motoboy/trocar-senha — troca senha (requer token)
    register_rest_route( 'sz-motoboy/v1', '/motoboy/trocar-senha', [
        'methods'             => 'POST',
        'callback'            => 'sz_mb_api_trocar_senha',
        'permission_callback' => 'sz_mb_auth_motoboy',
    ] );

    // POST /otp/solicitar e /otp/confirmar — mantidos mas não usados pelo novo fluxo
    register_rest_route( 'sz-motoboy/v1', '/otp/solicitar', [
        'methods'             => 'POST',
        'callback'            => 'sz_mb_api_otp_solicitar',
        'permission_callback' => '__return_true',
    ] );
    register_rest_route( 'sz-motoboy/v1', '/otp/confirmar', [
        'methods'             => 'POST',
        'callback'            => 'sz_mb_api_otp_confirmar',
        'permission_callback' => '__return_true',
    ] );

    // ── Motoboy (requer token_app header) ────────────────────────────────────

    // GET /motoboy/lote  — pedidos do dia
    register_rest_route( 'sz-motoboy/v1', '/motoboy/lote', [
        'methods'             => 'GET',
        'callback'            => 'sz_mb_api_lote',
        'permission_callback' => 'sz_mb_auth_motoboy',
    ] );

    // POST /motoboy/iniciar-rota — exige QR da etiqueta do pacote
    register_rest_route( 'sz-motoboy/v1', '/motoboy/iniciar-rota', [
        'methods'             => 'POST',
        'callback'            => 'sz_mb_api_iniciar_rota',
        'permission_callback' => 'sz_mb_auth_motoboy',
    ] );

    // POST /motoboy/devolver-qr — motoboy declara devolução por QR; estoque só volta após confirmação do OL
    register_rest_route( 'sz-motoboy/v1', '/motoboy/devolver-qr', [
        'methods'             => 'POST',
        'callback'            => 'sz_mb_api_devolver_qr',
        'permission_callback' => 'sz_mb_auth_motoboy',
    ] );

    // POST /motoboy/ping  {lat, lng}
    register_rest_route( 'sz-motoboy/v1', '/motoboy/ping', [
        'methods'             => 'POST',
        'callback'            => 'sz_mb_api_ping',
        'permission_callback' => 'sz_mb_auth_motoboy',
    ] );

    // POST /motoboy/entregar  {pedido_id, recebedor_nome, cpf, pgto_dinheiro, pgto_pix, pgto_cartao, lat, lng}
    register_rest_route( 'sz-motoboy/v1', '/motoboy/entregar', [
        'methods'             => 'POST',
        'callback'            => 'sz_mb_api_entregar',
        'permission_callback' => 'sz_mb_auth_motoboy',
    ] );

    // POST /motoboy/frustrar  {pedido_id, motivo, foto_base64, lat, lng}
    register_rest_route( 'sz-motoboy/v1', '/motoboy/frustrar', [
        'methods'             => 'POST',
        'callback'            => 'sz_mb_api_frustrar',
        'permission_callback' => 'sz_mb_auth_motoboy',
    ] );

    // GET /motoboy/fechamento  — resumo do dia
    register_rest_route( 'sz-motoboy/v1', '/motoboy/fechamento', [
        'methods'             => 'GET',
        'callback'            => 'sz_mb_api_get_fechamento',
        'permission_callback' => 'sz_mb_auth_motoboy',
    ] );

    // POST /motoboy/confirmar-repasse  — motoboy confirma que enviou PIX do dinheiro físico
    register_rest_route( 'sz-motoboy/v1', '/motoboy/confirmar-repasse', [
        'methods'             => 'POST',
        'callback'            => 'sz_mb_api_confirmar_repasse',
        'permission_callback' => 'sz_mb_auth_motoboy',
    ] );

    // GET /motoboy/pendentes-confirmacao  — baixas por admin pendentes de confirmação motoboy
    register_rest_route( 'sz-motoboy/v1', '/motoboy/pendentes-confirmacao', [
        'methods'             => 'GET',
        'callback'            => 'sz_mb_api_pendentes_confirmacao',
        'permission_callback' => 'sz_mb_auth_motoboy',
    ] );

    // POST /motoboy/comprovante  — upload de foto de comprovante
    register_rest_route( 'sz-motoboy/v1', '/motoboy/comprovante', [
        'methods'             => 'POST',
        'callback'            => 'sz_mb_api_upload_comprovante',
        'permission_callback' => 'sz_mb_auth_motoboy',
    ] );

    // GET /motoboy/comprovantes/{order_id}
    register_rest_route( 'sz-motoboy/v1', '/motoboy/comprovantes/(?P<order_id>\d+)', [
        'methods'             => 'GET',
        'callback'            => 'sz_mb_api_listar_comprovantes',
        'permission_callback' => 'sz_mb_auth_motoboy',
    ] );

    // POST /motoboy/push-subscribe
    register_rest_route( 'sz-motoboy/v1', '/motoboy/push-subscribe', [
        'methods'             => 'POST',
        'callback'            => 'sz_mb_api_motoboy_push_subscribe',
        'permission_callback' => 'sz_mb_auth_motoboy',
    ] );

    // GET /alan/localizacao
    register_rest_route( 'sz-motoboy/v1', '/alan/localizacao', [
        'methods'             => 'GET',
        'callback'            => 'sz_mb_api_alan_localizacao',
        'permission_callback' => 'sz_mb_auth_alan',
    ] );

    // GET /alan/historico/{motoboy_id}
    register_rest_route( 'sz-motoboy/v1', '/alan/historico/(?P<motoboy_id>\d+)', [
        'methods'             => 'GET',
        'callback'            => 'sz_mb_api_alan_historico',
        'permission_callback' => 'sz_mb_auth_alan',
    ] );

    // GET /alan/etiquetas
    register_rest_route( 'sz-motoboy/v1', '/alan/etiquetas', [
        'methods'             => 'GET',
        'callback'            => 'sz_mb_api_alan_etiquetas',
        'permission_callback' => 'sz_mb_auth_alan',
    ] );

    // POST /alan/push-subscribe
    register_rest_route( 'sz-motoboy/v1', '/alan/push-subscribe', [
        'methods'             => 'POST',
        'callback'            => 'sz_mb_api_alan_push_subscribe',
        'permission_callback' => 'sz_mb_auth_alan',
    ] );

    // GET /zona-cep
    register_rest_route( 'sz-motoboy/v1', '/zona-cep', [
        'methods'             => 'GET',
        'callback'            => 'sz_mb_api_zona_cep',
        'permission_callback' => '__return_true',
    ] );

    // GET /link-expedicao
    register_rest_route( 'sz-motoboy/v1', '/link-expedicao', [
        'methods'             => 'GET',
        'callback'            => 'sz_mb_api_link_expedicao',
        'permission_callback' => '__return_true',
    ] );

    // POST /dispensar-cpf
    register_rest_route( 'sz-motoboy/v1', '/dispensar-cpf', [
        'methods'             => 'POST',
        'callback'            => 'sz_rest_toggle_dispensar_cpf',
        'permission_callback' => '__return_true',
    ] );

    // ── Carteira do motoboy ──────────────────────────────────────────────────

    // GET /wallet/saldo
    register_rest_route( 'sz-motoboy/v1', '/wallet/saldo', [
        'methods'             => 'GET',
        'callback'            => 'sz_mbw_rest_saldo',
        'permission_callback' => 'sz_mb_auth_motoboy',
    ] );

    // GET /wallet/historico
    register_rest_route( 'sz-motoboy/v1', '/wallet/historico', [
        'methods'             => 'GET',
        'callback'            => 'sz_mbw_rest_historico',
        'permission_callback' => 'sz_mb_auth_motoboy',
    ] );

    // GET /wallet/bancario
    register_rest_route( 'sz-motoboy/v1', '/wallet/bancario', [
        'methods'             => 'GET',
        'callback'            => 'sz_mbw_rest_get_bancario',
        'permission_callback' => 'sz_mb_auth_motoboy',
    ] );

    // POST /wallet/bancario
    register_rest_route( 'sz-motoboy/v1', '/wallet/bancario', [
        'methods'             => 'POST',
        'callback'            => 'sz_mbw_rest_save_bancario',
        'permission_callback' => 'sz_mb_auth_motoboy',
    ] );

    // ── Alan (requer cookie WP admin ou nonce) ───────────────────────────────

    // GET /alan/pedidos  — todos pedidos ativos por CD
    register_rest_route( 'sz-motoboy/v1', '/alan/pedidos', [
        'methods'             => 'GET',
        'callback'            => 'sz_mb_api_alan_pedidos',
        'permission_callback' => 'sz_mb_auth_alan',
    ] );

    // POST /alan/embalar  {pedido_id, motoboy_id?}  — Alan confirma embalagem e valida motoboy
    register_rest_route( 'sz-motoboy/v1', '/alan/embalar', [
        'methods'             => 'POST',
        'callback'            => 'sz_mb_api_alan_embalar',
        'permission_callback' => 'sz_mb_auth_alan',
    ] );

    // POST /alan/confirmar-fechamento  {motoboy_id, data}
    register_rest_route( 'sz-motoboy/v1', '/alan/confirmar-fechamento', [
        'methods'             => 'POST',
        'callback'            => 'sz_mb_api_alan_confirmar_fechamento',
        'permission_callback' => 'sz_mb_auth_alan',
    ] );

    // GET /alan/dashboard  — métricas gerais
    register_rest_route( 'sz-motoboy/v1', '/alan/dashboard', [
        'methods'             => 'GET',
        'callback'            => 'sz_mb_api_alan_dashboard',
        'permission_callback' => 'sz_mb_auth_alan',
    ] );

    // ── Tracking público ──────────────────────────────────────────────────────

    // GET /tracking/{wc_order_id}
    register_rest_route( 'sz-motoboy/v1', '/tracking/(?P<order_id>\d+)', [
        'methods'             => 'GET',
        'callback'            => 'sz_mb_api_tracking',
        'permission_callback' => '__return_true',
    ] );


    // POST /tracking/{wc_order_id}/reagendar {date:YYYY-MM-DD}
    register_rest_route( 'sz-motoboy/v1', '/tracking/(?P<order_id>\d+)/reagendar', [
        'methods'             => 'POST',
        'callback'            => 'sz_mb_api_tracking_reagendar',
        'permission_callback' => '__return_true',
    ] );

    // ── Endpoints OL (portal v2 — autenticados via portal session) ──────────

    // POST /ol/mudar-status {pedido_id, status, [motoboy_id, motivo, observacao]}
    register_rest_route( 'sz-motoboy/v1', '/ol/mudar-status', [
        'methods'             => 'POST',
        'callback'            => 'sz_mb_api_ol_mudar_status',
        'permission_callback' => 'sz_mb_auth_ol_portal',
    ] );

    // POST /ol/trocar-motoboy {pedido_id, motoboy_id}
    register_rest_route( 'sz-motoboy/v1', '/ol/trocar-motoboy', [
        'methods'             => 'POST',
        'callback'            => 'sz_mb_api_ol_trocar_motoboy',
        'permission_callback' => 'sz_mb_auth_ol_portal',
    ] );

    // GET /ol/motoboys-do-dia
    register_rest_route( 'sz-motoboy/v1', '/ol/motoboys-do-dia', [
        'methods'             => 'GET',
        'callback'            => 'sz_mb_api_ol_motoboys_dia',
        'permission_callback' => 'sz_mb_auth_ol_portal',
    ] );

    // GET /ol/motoboys — lista motoboys ativos para seleção
    register_rest_route( 'sz-motoboy/v1', '/ol/motoboys', [
        'methods'             => 'GET',
        'callback'            => 'sz_mb_api_ol_listar_motoboys',
        'permission_callback' => 'sz_mb_auth_ol_portal',
    ] );

    // GET /ol/pedido-historico?pedido_id=X — histórico de status de um pedido
    register_rest_route( 'sz-motoboy/v1', '/ol/pedido-historico', [
        'methods'             => 'GET',
        'callback'            => 'sz_mb_api_ol_pedido_historico',
        'permission_callback' => 'sz_mb_auth_ol_portal',
    ] );
} );

// ─── Auth helpers ─────────────────────────────────────────────────────────────

function sz_mb_get_motoboy_by_token(): ?object {
    global $wpdb;
    // Prioridade: header HTTP (padrão) → query param _szt → REQUEST → corpo JSON →
    // Authorization: Bearer. Vários proxies/CDN (Hostinger/LiteSpeed) removem headers
    // customizados em POST; por isso há múltiplos fallbacks.
    $token = sanitize_text_field( $_SERVER['HTTP_X_MOTOBOY_TOKEN'] ?? '' );
    if ( ! $token ) $token = sanitize_text_field( $_GET['_szt'] ?? '' );
    if ( ! $token ) $token = sanitize_text_field( $_REQUEST['_szt'] ?? '' );
    if ( ! $token && ! empty( $_SERVER['HTTP_AUTHORIZATION'] ) && stripos( (string) $_SERVER['HTTP_AUTHORIZATION'], 'Bearer ' ) === 0 ) {
        $token = sanitize_text_field( trim( substr( (string) $_SERVER['HTTP_AUTHORIZATION'], 7 ) ) );
    }
    if ( ! $token ) {
        $raw = file_get_contents( 'php://input' );
        if ( $raw ) {
            $body = json_decode( $raw, true );
            if ( is_array( $body ) ) {
                $token = sanitize_text_field( (string) ( $body['_szt'] ?? $body['token'] ?? '' ) );
            }
        }
    }
    if ( ! $token ) return null;

    // Desabilita cache do LiteSpeed para esta request (Hostinger usa LiteSpeed Cache)
    if ( ! headers_sent() ) {
        header( 'X-LiteSpeed-Cache-Control: no-cache' );
        header( 'Cache-Control: no-store, no-cache, must-revalidate' );
    }

    // Usa SQL_NO_CACHE para evitar query cache do MySQL/MariaDB
    return $wpdb->get_row( $wpdb->prepare(
        "SELECT SQL_NO_CACHE * FROM {$wpdb->prefix}sz_motoboys WHERE token_app = %s AND ativo = 1",
        $token
    ) );
}

function sz_mb_auth_motoboy(): bool {
    return (bool) sz_mb_get_motoboy_by_token();
}

function sz_mb_auth_alan(): bool {
    return current_user_can( 'manage_woocommerce' );
}

// Permite: admin WooCommerce OU usuário de portal com role operator/producer
function sz_mb_auth_ol_portal(): bool {
    if ( current_user_can( 'manage_woocommerce' ) ) return true;
    if ( function_exists( '\\Senderzz\\Portal\\Portal_Auth::get_current_user' ) ) {
        $u = \Senderzz\Portal\Portal_Auth::get_current_user();
        if ( $u && in_array( strtolower( trim( (string) ( $u->role ?? '' ) ) ), [ 'operator', 'producer', 'client' ], true ) ) return true;
    }
    // Fallback: verifica sessão portal via class PSR-4
    if ( class_exists( 'WC_MelhorEnvio\\Portal\\Portal_Auth' ) ) {
        $u = \WC_MelhorEnvio\Portal\Portal_Auth::get_current_user();
        if ( $u ) return true;
    }
    return false;
}

// ── Callbacks OL ─────────────────────────────────────────────────────────────

function sz_mb_api_ol_mudar_status( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $pedido_id = absint( $req->get_param( 'pedido_id' ) );
    $novo_status = sanitize_key( $req->get_param( 'status' ) ?? '' );
    $allowed = [ 'agendado', 'embalado', 'em_rota', 'a_caminho', 'entregue', 'frustrado', 'cancelado', 'devolvido' ];
    if ( ! $pedido_id || ! in_array( $novo_status, $allowed, true ) ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Parâmetros inválidos.' ], 422 );
    }
    $pedido = $wpdb->get_row( $wpdb->prepare( "SELECT id, status, motoboy_id FROM {$wpdb->prefix}sz_motoboy_pedidos WHERE id = %d", $pedido_id ) );
    if ( ! $pedido ) return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Pedido não encontrado.' ], 404 );

    if ( $novo_status === 'embalado' && ! $req->get_param( 'motoboy_id' ) && ! $pedido->motoboy_id ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Defina o motoboy responsável antes de embalar o pedido.' ], 422 );
    }

    $extra = [];
    if ( $req->get_param( 'motoboy_id' ) ) $extra['motoboy_id']       = absint( $req->get_param( 'motoboy_id' ) );
    if ( $req->get_param( 'motivo' ) )    $extra['frustrado_motivo']  = sanitize_text_field( $req->get_param( 'motivo' ) );
    if ( $req->get_param( 'observacao' ) ) $extra['frustrado_observacao'] = sanitize_textarea_field( $req->get_param( 'observacao' ) );
    if ( $req->get_param( 'motoboy_id' ) ) $extra['baixa_motoboy_id'] = absint( $req->get_param( 'motoboy_id' ) );

    $ok = function_exists( 'sz_motoboy_mudar_status' )
        ? sz_motoboy_mudar_status( $pedido_id, $novo_status, $extra, 'ol' )
        : false;

    return new WP_REST_Response( [ 'ok' => $ok ], $ok ? 200 : 500 );
}

function sz_mb_api_ol_trocar_motoboy( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $pedido_id  = absint( $req->get_param( 'pedido_id' ) );
    $motoboy_id = absint( $req->get_param( 'motoboy_id' ) );
    if ( ! $pedido_id ) return new WP_REST_Response( [ 'ok' => false, 'erro' => 'pedido_id obrigatório.' ], 422 );

    $updated = $wpdb->update(
        $wpdb->prefix . 'sz_motoboy_pedidos',
        [ 'motoboy_id' => $motoboy_id ?: null ],
        [ 'id' => $pedido_id ],
        [ $motoboy_id ? '%d' : 'NULL' ],
        [ '%d' ]
    );
    if ( $updated !== false ) {
        // Double-write Go (Fase 1)
        do_action( 'sz_motoboy_motoboy_trocado', $pedido_id, $motoboy_id ?: null, 0 );
    }
    return new WP_REST_Response( [ 'ok' => $updated !== false ], 200 );
}

function sz_mb_api_ol_pedido_historico( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $pedido_id = absint( $req->get_param( 'pedido_id' ) );
    if ( ! $pedido_id ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'pedido_id obrigatório.' ], 422 );
    }

    $rows = $wpdb->get_results( $wpdb->prepare(
        "SELECT a.acao, a.de_status, a.para_status, a.actor_tipo, a.created_at,
                COALESCE(m.nome, '') AS motoboy_nome
           FROM {$wpdb->prefix}sz_motoboy_audit a
           LEFT JOIN {$wpdb->prefix}sz_motoboys m ON m.id = a.motoboy_id
          WHERE a.pedido_id = %d
          ORDER BY a.created_at ASC
          LIMIT 200",
        $pedido_id
    ) );

    $status_labels = [
        'agendado'  => 'Agendado',
        'embalado'  => 'Embalado',
        'em_rota'   => 'Em rota',
        'a_caminho' => 'A caminho',
        'entregue'  => 'Entregue',
        'frustrado' => 'Frustrado',
        'cancelado' => 'Cancelado',
        'aprovado'  => 'Aprovado',
        'reagendado'=> 'Reagendado',
    ];

    $history = array_map( static function( $r ) use ( $status_labels ): array {
        return [
            'acao'          => (string) $r->acao,
            'de'            => $status_labels[ (string) $r->de_status ] ?? (string) $r->de_status,
            'para'          => $status_labels[ (string) $r->para_status ] ?? (string) $r->para_status,
            'actor'         => (string) $r->actor_tipo,
            'motoboy_nome'  => (string) $r->motoboy_nome,
            'ts'            => wp_date( 'd/m/Y H:i', strtotime( (string) $r->created_at ) ),
        ];
    }, $rows ?: [] );

    return new WP_REST_Response( [ 'ok' => true, 'history' => $history ] );
}

function sz_mb_api_ol_motoboys_dia( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $hoje = wp_date( 'Y-m-d' );
    $motoboys = $wpdb->get_results(
        "SELECT m.id, m.nome, m.telefone,
                COUNT(p.id) AS total_pedidos,
                SUM(CASE WHEN p.status='entregue' THEN 1 ELSE 0 END) AS entregues,
                SUM(CASE WHEN p.status='frustrado' THEN 1 ELSE 0 END) AS frustrados,
                SUM(CASE WHEN p.status IN ('em_rota','a_caminho') THEN 1 ELSE 0 END) AS em_rota
         FROM {$wpdb->prefix}sz_motoboys m
         LEFT JOIN {$wpdb->prefix}sz_motoboy_pedidos p ON p.motoboy_id = m.id AND DATE(p.created_at) = '{$hoje}'
         WHERE m.ativo = 1
         GROUP BY m.id, m.nome, m.telefone
         ORDER BY m.nome ASC",
        ARRAY_A
    ) ?: [];

    // Para cada motoboy, busca pedidos do dia
    foreach ( $motoboys as &$mb ) {
        $mb_id = (int) $mb['id'];
        $mb['pedidos'] = $wpdb->get_results( $wpdb->prepare(
            "SELECT id, wc_order_id, dest_nome, dest_endereco, dest_numero, valor_pedido, status, ts_embalado, ts_em_rota, ts_entregue
             FROM {$wpdb->prefix}sz_motoboy_pedidos
             WHERE motoboy_id = %d AND DATE(created_at) = %s
             ORDER BY id ASC LIMIT 50",
            $mb_id, $hoje
        ), ARRAY_A ) ?: [];
    }
    unset( $mb );

    return new WP_REST_Response( $motoboys, 200 );
}

function sz_mb_api_ol_listar_motoboys( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $motoboys = $wpdb->get_results(
        "SELECT id, nome, telefone FROM {$wpdb->prefix}sz_motoboys WHERE ativo = 1 ORDER BY nome ASC LIMIT 100",
        ARRAY_A
    ) ?: [];
    return new WP_REST_Response( $motoboys, 200 );
}


if ( ! function_exists( 'sz_mb_validar_cpf' ) ) {
    function sz_mb_validar_cpf( string $cpf ): bool {
        $cpf = preg_replace( '/\D+/', '', $cpf );
        if ( strlen( $cpf ) !== 11 ) return false;
        if ( preg_match( '/^(\d)\1{10}$/', $cpf ) ) return false;

        for ( $t = 9; $t < 11; $t++ ) {
            $soma = 0;
            for ( $i = 0; $i < $t; $i++ ) {
                $soma += (int) $cpf[$i] * ( ( $t + 1 ) - $i );
            }
            $digito = ( $soma * 10 ) % 11;
            if ( $digito === 10 ) $digito = 0;
            if ( (int) $cpf[$t] !== $digito ) return false;
        }
        return true;
    }
}

if ( ! function_exists( 'sz_mb_parse_money' ) ) {
    function sz_mb_parse_money( $value ): float {
        if ( is_string( $value ) ) {
            $value = trim( $value );
            $value = str_replace( [ 'R$', ' ', '.' ], '', $value );
            $value = str_replace( ',', '.', $value );
        }
        return round( max( 0, (float) $value ), 2 );
    }
}

if ( ! function_exists( 'sz_mb_validar_gps_operacional' ) ) {
    function sz_mb_validar_gps_operacional( $lat, $lng, $accuracy = null ): array {
        if ( ! is_numeric( $lat ) || ! is_numeric( $lng ) ) {
            return [ false, 'Localização GPS obrigatória.' ];
        }
        $lat = (float) $lat;
        $lng = (float) $lng;
        if ( abs( $lat ) < 0.000001 || abs( $lng ) < 0.000001 || $lat < -90 || $lat > 90 || $lng < -180 || $lng > 180 ) {
            return [ false, 'Localização GPS inválida.' ];
        }
        if ( is_numeric( $accuracy ) && (float) $accuracy > 150 ) {
            return [ false, 'Precisão do GPS insuficiente. Aguarde melhorar o sinal e tente novamente.' ];
        }
        return [ true, '' ];
    }
}

if ( ! function_exists( 'sz_mb_validar_foto_base64' ) ) {
    function sz_mb_validar_foto_base64( $base64 ): array {
        if ( ! is_string( $base64 ) || trim( $base64 ) === '' ) {
            return [ false, 'Foto obrigatória.' ];
        }
        if ( ! preg_match( '#^data:image/(jpeg|jpg|png|webp);base64,#i', $base64 ) ) {
            return [ false, 'Formato da foto inválido. Tire uma foto pelo aplicativo.' ];
        }
        $payload = preg_replace( '#^data:image/[a-zA-Z0-9.+-]+;base64,#', '', $base64 );
        $bin = base64_decode( $payload, true );
        if ( ! $bin || strlen( $bin ) < 3000 ) {
            return [ false, 'Foto inválida ou muito pequena. Tire uma nova foto.' ];
        }
        if ( strlen( $bin ) > 8 * 1024 * 1024 ) {
            return [ false, 'Foto muito grande. Tire uma foto menor.' ];
        }
        $info = @getimagesizefromstring( $bin );
        if ( ! $info || empty( $info[0] ) || empty( $info[1] ) ) {
            return [ false, 'Arquivo enviado não parece ser uma foto válida.' ];
        }
        if ( (int) $info[0] < 320 || (int) $info[1] < 240 ) {
            return [ false, 'Foto com resolução baixa. Tire uma nova foto.' ];
        }
        return [ true, '' ];
    }
}

// ─── Endpoints ────────────────────────────────────────────────────────────────

function sz_mb_api_login( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $telefone  = sanitize_text_field( $req->get_param('telefone') );
    $token_app = sanitize_text_field( $req->get_param('token_app') );
    $pin_raw   = sanitize_text_field( (string) ( $req->get_param('pin') ?? '' ) );

    if ( ! $telefone || ! $token_app ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Parâmetros inválidos.' ], 400 );
    }

    $mb = sz_mb_buscar_por_telefone( sz_mb_normalizar_telefone( $telefone ) );
    if ( ! $mb ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Telefone ou PIN incorretos.' ], 401 );
    }

    // Verificação de PIN — migração gradual:
    // Se o motoboy ainda não tem pin_hash (cadastro antigo), aceita sem PIN e define o PIN informado.
    // Assim motoboys existentes não são bloqueados na atualização.
    if ( ! empty( $mb->pin_hash ) ) {
        if ( $pin_raw === '' ) {
            return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Informe o PIN de acesso.', 'needs_pin' => true ], 401 );
        }
        if ( ! password_verify( $pin_raw, $mb->pin_hash ) ) {
            return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Telefone ou PIN incorretos.' ], 401 );
        }
    } elseif ( $pin_raw !== '' ) {
        // Primeiro login com PIN: persiste o hash para próximos logins
        $wpdb->update(
            $wpdb->prefix . 'sz_motoboys',
            [ 'pin_hash' => password_hash( $pin_raw, PASSWORD_BCRYPT ) ],
            [ 'id' => $mb->id ]
        );
    }

    $wpdb->update( $wpdb->prefix . 'sz_motoboys', [ 'token_app' => $token_app ], [ 'id' => $mb->id ] );
    $wpdb->flush();

    return new WP_REST_Response( [
        'ok'        => true,
        'motoboy'   => [ 'id' => $mb->id, 'nome' => $mb->nome, 'zona_id' => $mb->zona_id ],
        'token_app' => $token_app,
        'has_pin'   => ! empty( $mb->pin_hash ) || $pin_raw !== '',
    ] );
}


// ── OTP Login — solicitar código ──────────────────────────────────────────────
function sz_mb_api_otp_solicitar( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $telefone = sanitize_text_field( $req->get_param('telefone') ?? '' );
    $telefone = preg_replace( '/\D/', '', $telefone );

    // Rate limit: máx 3 tentativas por telefone por 5 minutos
    $rl_key = 'sz_mb_otp_rl_' . md5( $telefone );
    $attempts = (int) get_transient( $rl_key );
    if ( $attempts >= 3 ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Muitas tentativas. Aguarde alguns minutos.' ], 429 );
    }

    $mb = $wpdb->get_row( $wpdb->prepare(
        "SELECT id, nome, telefone FROM {$wpdb->prefix}sz_motoboys WHERE telefone = %s AND ativo = 1",
        $telefone
    ) );

    if ( ! $mb ) {
        // Mensagem genérica — não revela se o telefone está cadastrado
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Número não encontrado. Fale com o administrador.' ], 404 );
    }

    $set_transient = set_transient( $rl_key, $attempts + 1, 5 * MINUTE_IN_SECONDS );

    // Gera OTP de 6 dígitos com entropia segura
    $otp  = str_pad( (string) random_int( 0, 999999 ), 6, '0', STR_PAD_LEFT );
    $hash = wp_hash_password( $otp ); // bcrypt — seguro para armazenar temporariamente
    $key  = 'sz_mb_otp_' . md5( $telefone );
    set_transient( $key, [ 'hash' => $hash, 'mb_id' => (int) $mb->id, 'ts' => time() ], 10 * MINUTE_IN_SECONDS );

    // Envia via WhatsApp (usa a infra de notificações do plugin)
    $msg = "🏍️ *Senderzz*\nSeu código de acesso: *{$otp}*\nVálido por 10 minutos. Não compartilhe.";

    if ( function_exists( 'sz_send_whatsapp' ) ) {
        sz_send_whatsapp( $telefone, $msg );
    } elseif ( function_exists( 'egw_send_whatsapp' ) ) {
        egw_send_whatsapp( $telefone, $msg );
    } elseif ( function_exists( 'senderzz_send_notification' ) ) {
        senderzz_send_notification( $telefone, $msg, 'whatsapp' );
    } else {
        // Fallback: loga para depuração (remover em produção)
        error_log( "[Senderzz OTP] Tel: {$telefone} | OTP: {$otp}" );
    }

    return new WP_REST_Response( [ 'ok' => true ] );
}

// ── OTP Login — confirmar código ──────────────────────────────────────────────
function sz_mb_api_otp_confirmar( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $telefone  = preg_replace( '/\D/', '', sanitize_text_field( $req->get_param('telefone') ?? '' ) );
    $otp_raw   = sanitize_text_field( $req->get_param('otp') ?? '' );
    $token_app = sanitize_text_field( $req->get_param('token_app') ?? '' );

    if ( ! $telefone || ! $otp_raw || ! $token_app ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Parâmetros inválidos.' ], 400 );
    }

    // Rate limit para tentativas de OTP: máx 5 por telefone por 10 minutos
    $rl_key = 'sz_mb_otp_confirm_rl_' . md5( $telefone );
    $attempts = (int) get_transient( $rl_key );
    if ( $attempts >= 5 ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Muitas tentativas incorretas. Solicite um novo código.' ], 429 );
    }

    $key  = 'sz_mb_otp_' . md5( $telefone );
    $data = get_transient( $key );

    if ( ! $data || empty( $data['hash'] ) || empty( $data['mb_id'] ) ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Código expirado. Solicite um novo.' ], 401 );
    }

    if ( ! wp_check_password( $otp_raw, $data['hash'] ) ) {
        set_transient( $rl_key, $attempts + 1, 10 * MINUTE_IN_SECONDS );
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Código incorreto.' ], 401 );
    }

    // OTP válido — grava token e limpa transients
    delete_transient( $key );
    delete_transient( $rl_key );
    delete_transient( 'sz_mb_otp_rl_' . md5( $telefone ) );

    $mb_id = (int) $data['mb_id'];
    $mb = $wpdb->get_row( $wpdb->prepare(
        "SELECT id, nome, zona_id FROM {$wpdb->prefix}sz_motoboys WHERE id = %d AND ativo = 1",
        $mb_id
    ) );

    if ( ! $mb ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Motoboy inativo.' ], 403 );
    }

    $wpdb->update( $wpdb->prefix . 'sz_motoboys', [ 'token_app' => $token_app ], [ 'id' => $mb_id ] );

    return new WP_REST_Response( [
        'ok'      => true,
        'motoboy' => [ 'id' => $mb->id, 'nome' => $mb->nome, 'zona_id' => $mb->zona_id ],
    ] );
}


// ── Login por senha ────────────────────────────────────────────────────────────

function sz_mb_normalizar_telefone( string $raw ): string {
    // Remove tudo que não é dígito
    $digits = preg_replace( '/\D/', '', $raw );
    // Remove prefixo internacional +55 / 55 se tiver 12-13 dígitos
    if ( strlen( $digits ) === 13 && substr( $digits, 0, 2 ) === '55' ) {
        $digits = substr( $digits, 2 );
    } elseif ( strlen( $digits ) === 12 && substr( $digits, 0, 2 ) === '55' ) {
        $digits = substr( $digits, 2 );
    }
    return $digits;
}

function sz_mb_buscar_por_telefone( string $telefone_normalizado ): ?object {
    global $wpdb;
    $t = $wpdb->prefix . 'sz_motoboys';
    // Busca pelo número normalizado OU pelo número com +55 salvo no banco
    // Usa REPLACE no banco para remover +, (, ), -, espaços antes de comparar
    return $wpdb->get_row( $wpdb->prepare(
        "SELECT * FROM {$t}
          WHERE REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(telefone,'+',''),'-',''),' ',''),'(',''),')','') IN (%s, %s)
            AND ativo = 1
          LIMIT 1",
        $telefone_normalizado,
        '55' . $telefone_normalizado
    ) );
}

function sz_mb_api_login_verificar( WP_REST_Request $req ): WP_REST_Response {
    $telefone = sz_mb_normalizar_telefone( sanitize_text_field( $req->get_param('telefone') ?? '' ) );
    if ( ! $telefone ) return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Informe o telefone.' ], 400 );

    // Rate limit: 10 verificações por IP por 5 min
    $ip    = sanitize_text_field( $_SERVER['REMOTE_ADDR'] ?? '0.0.0.0' );
    $rl    = 'sz_mb_vrf_' . md5( $ip );
    $count = (int) get_transient( $rl );
    if ( $count >= 10 ) return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Muitas tentativas. Aguarde alguns minutos.' ], 429 );
    set_transient( $rl, $count + 1, 5 * MINUTE_IN_SECONDS );

    $mb = sz_mb_buscar_por_telefone( $telefone );
    if ( ! $mb ) return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Número não cadastrado. Fale com o administrador.' ], 404 );

    // pin_hash pode estar em option (fallback se coluna não existia quando senha foi definida)
    $pin_hash = $mb->pin_hash ?? get_option( 'sz_mb_pin_tmp_' . $mb->id, '' );

    return new WP_REST_Response( [
        'ok'       => true,
        'nome'     => $mb->nome,
        'tem_senha'=> ! empty( $pin_hash ),
    ] );
}

function sz_mb_api_login_definir_senha( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $telefone  = preg_replace( '/\D/', '', sanitize_text_field( $req->get_param('telefone') ?? '' ) );
    $senha     = (string) ( $req->get_param('senha') ?? '' );
    $token_app = sanitize_text_field( $req->get_param('token_app') ?? '' );

    if ( ! $telefone || strlen( $senha ) < 4 || ! $token_app ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Parâmetros inválidos.' ], 400 );
    }

    $mb = sz_mb_buscar_por_telefone( sz_mb_normalizar_telefone( $telefone ) );
    if ( ! $mb ) return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Telefone não cadastrado.' ], 404 );

    // Não permite redefinir se já tem senha — deve usar trocar-senha (requer senha atual)
    if ( ! empty( $mb->pin_hash ) ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Conta já possui senha. Use "Alterar senha" no perfil.' ], 409 );
    }

    // Verifica se a coluna pin_hash existe — pode não existir se a migration não rodou ainda
    $has_pin_hash = (bool) $wpdb->get_var(
        "SHOW COLUMNS FROM {$wpdb->prefix}sz_motoboys LIKE 'pin_hash'"
    );

    if ( $has_pin_hash ) {
        $updated = $wpdb->update( $wpdb->prefix . 'sz_motoboys', [
            'pin_hash'  => password_hash( $senha, PASSWORD_BCRYPT ),
            'token_app' => $token_app,
        ], [ 'id' => $mb->id ] );
    } else {
        // Coluna ainda não existe — roda a migration agora e tenta de novo
        sz_motoboy_create_tables();
        $has_pin_hash = (bool) $wpdb->get_var(
            "SHOW COLUMNS FROM {$wpdb->prefix}sz_motoboys LIKE 'pin_hash'"
        );
        if ( $has_pin_hash ) {
            $updated = $wpdb->update( $wpdb->prefix . 'sz_motoboys', [
                'pin_hash'  => password_hash( $senha, PASSWORD_BCRYPT ),
                'token_app' => $token_app,
            ], [ 'id' => $mb->id ] );
        } else {
            // Último fallback: salva senha em user_meta e atualiza só token
            update_option( 'sz_mb_pin_tmp_' . $mb->id, password_hash( $senha, PASSWORD_BCRYPT ) );
            $updated = $wpdb->update( $wpdb->prefix . 'sz_motoboys',
                [ 'token_app' => $token_app ],
                [ 'id' => $mb->id ]
            );
        }
    }

    // Força limpeza do cache de queries do wpdb (importante no Hostinger com LiteSpeed Cache)
    $wpdb->flush();

    if ( $updated === false ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Erro ao salvar. Tente novamente.' ], 500 );
    }

    return new WP_REST_Response( [
        'ok'      => true,
        'motoboy' => [ 'id' => $mb->id, 'nome' => $mb->nome, 'zona_id' => $mb->zona_id ],
    ] );
}

function sz_mb_api_login_autenticar( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $telefone  = preg_replace( '/\D/', '', sanitize_text_field( $req->get_param('telefone') ?? '' ) );
    $senha     = (string) ( $req->get_param('senha') ?? '' );
    $token_app = sanitize_text_field( $req->get_param('token_app') ?? '' );

    if ( ! $telefone || ! $senha || ! $token_app ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Parâmetros inválidos.' ], 400 );
    }

    // Rate limit: máx 5 tentativas por telefone por 10 min
    $rl       = 'sz_mb_auth_rl_' . md5( $telefone );
    $attempts = (int) get_transient( $rl );
    if ( $attempts >= 5 ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Muitas tentativas incorretas. Aguarde alguns minutos.' ], 429 );
    }

    $mb = sz_mb_buscar_por_telefone( sz_mb_normalizar_telefone( $telefone ) );
    $msg_invalido = 'Telefone ou senha incorretos.'; // Genérico — não revela se telefone existe
    if ( ! $mb ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => $msg_invalido ], 401 );
    }

    // Suporta pin_hash em option (fallback se coluna não existia quando senha foi definida)
    $pin_hash = ! empty( $mb->pin_hash )
        ? $mb->pin_hash
        : (string) get_option( 'sz_mb_pin_tmp_' . $mb->id, '' );

    if ( empty( $pin_hash ) ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => $msg_invalido ], 401 );
    }

    if ( ! password_verify( $senha, $pin_hash ) ) {
        set_transient( $rl, $attempts + 1, 10 * MINUTE_IN_SECONDS );
        return new WP_REST_Response( [ 'ok' => false, 'erro' => $msg_invalido ], 401 );
    }

    // Login OK — zera rate limit e atualiza token
    delete_transient( $rl );
    // Se a senha estava em option e agora pin_hash existe, migra para o banco
    if ( empty( $mb->pin_hash ) && $pin_hash ) {
        $has_col = (bool) $wpdb->get_var( "SHOW COLUMNS FROM {$wpdb->prefix}sz_motoboys LIKE 'pin_hash'" );
        if ( $has_col ) {
            $wpdb->update( $wpdb->prefix . 'sz_motoboys',
                [ 'pin_hash' => $pin_hash, 'token_app' => $token_app ],
                [ 'id' => $mb->id ]
            );
            delete_option( 'sz_mb_pin_tmp_' . $mb->id );
        } else {
            $wpdb->update( $wpdb->prefix . 'sz_motoboys', [ 'token_app' => $token_app ], [ 'id' => $mb->id ] );
        }
    } else {
        $wpdb->update( $wpdb->prefix . 'sz_motoboys', [ 'token_app' => $token_app ], [ 'id' => $mb->id ] );
    }
    $wpdb->flush();

    return new WP_REST_Response( [
        'ok'      => true,
        'motoboy' => [ 'id' => $mb->id, 'nome' => $mb->nome, 'zona_id' => $mb->zona_id ],
    ] );
}

function sz_mb_api_trocar_senha( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $mb          = sz_mb_get_motoboy_by_token();
    $senha_atual = (string) ( $req->get_param('senha_atual') ?? '' );
    $senha_nova  = (string) ( $req->get_param('senha_nova')  ?? '' );

    if ( strlen( $senha_nova ) < 4 ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Nova senha deve ter ao menos 4 caracteres.' ], 422 );
    }

    $row = $wpdb->get_row( $wpdb->prepare(
        "SELECT pin_hash FROM {$wpdb->prefix}sz_motoboys WHERE id = %d",
        $mb->id
    ) );

    if ( ! empty( $row->pin_hash ) && ! password_verify( $senha_atual, $row->pin_hash ) ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Senha atual incorreta.' ], 401 );
    }

    $wpdb->update( $wpdb->prefix . 'sz_motoboys',
        [ 'pin_hash' => password_hash( $senha_nova, PASSWORD_BCRYPT ) ],
        [ 'id' => $mb->id ]
    );

    return new WP_REST_Response( [ 'ok' => true ] );
}

function sz_mb_api_lote( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $mb      = sz_mb_get_motoboy_by_token();
    $p       = $wpdb->prefix;
    $incluir = sanitize_text_field( $req->get_param('incluir') ?: '' );

    // Padrão: apenas ativos. Com ?incluir=historico: também entregue/frustrado do dia.
    if ( $incluir === 'historico' ) {
        $hoje_br = ( new DateTimeImmutable( 'now', new DateTimeZone( 'America/Sao_Paulo' ) ) )->format( 'Y-m-d' );
        $pedidos = $wpdb->get_results( $wpdb->prepare(
            "SELECT * FROM {$p}sz_motoboy_pedidos
              WHERE motoboy_id = %d
                AND (
                    status IN ('embalado','em_rota','a_caminho','frustrado')
                    OR ( status = 'entregue'  AND DATE(ts_entregue)  = %s )
                )
              ORDER BY created_at ASC",
            $mb->id, $hoje_br
        ) );
    } else {
        $pedidos = $wpdb->get_results( $wpdb->prepare(
            "SELECT * FROM {$p}sz_motoboy_pedidos
              WHERE motoboy_id = %d
                AND status IN ('embalado','em_rota','a_caminho','frustrado')
              ORDER BY created_at ASC",
            $mb->id
        ) );
    }

    // Anexa status físico de custódia para o PWA não pedir nova bipagem de devolução
    // quando o pacote já está aguardando confirmação do OL.
    $ids = array_values( array_filter( array_map( static fn( $r ) => (int) ( $r->id ?? 0 ), (array) $pedidos ) ) );
    if ( ! empty( $ids ) ) {
        $custody_table = $p . 'sz_motoboy_stock_custody';
        if ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $custody_table ) ) === $custody_table ) {
            $ph = implode( ',', array_fill( 0, count( $ids ), '%d' ) );
            $rows = $wpdb->get_results( $wpdb->prepare( "SELECT pedido_id, physical_status FROM {$custody_table} WHERE pedido_id IN ({$ph}) GROUP BY pedido_id, physical_status", $ids ) );
            $map = [];
            foreach ( (array) $rows as $r ) {
                $map[ (int) $r->pedido_id ] = (string) $r->physical_status;
            }
            foreach ( (array) $pedidos as $ped ) {
                $pid = (int) ( $ped->id ?? 0 );
                if ( isset( $map[ $pid ] ) ) {
                    $ped->custody_status = $map[ $pid ];
                    $ped->return_declared = $map[ $pid ] === 'return_declared' ? 1 : 0;
                }
            }
        }
    }
    $pedidos = array_values( array_filter( (array) $pedidos, static function( $ped ) {
        if ( (string) ( $ped->status ?? '' ) !== 'frustrado' ) return true;
        $custody_status = (string) ( $ped->custody_status ?? 'frustrated' );
        return in_array( $custody_status, [ 'frustrated', 'return_declared' ], true );
    } ) );

    return new WP_REST_Response( [ 'ok' => true, 'pedidos' => $pedidos ] );
}

function sz_mb_api_iniciar_rota( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $mb = sz_mb_get_motoboy_by_token();
    $p  = $wpdb->prefix;

    if ( ! $mb || empty( $mb->id ) ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Sessão do motoboy expirada. Faça login novamente.' ], 401 );
    }

    if ( ! headers_sent() ) {
        header( 'X-LiteSpeed-Cache-Control: no-cache' );
        header( 'Cache-Control: no-store, no-cache, must-revalidate, max-age=0' );
        header( 'Pragma: no-cache' );
    }

    // Verifica fechamento pendente real
    $fechamento_pendente = $wpdb->get_var( $wpdb->prepare(
        "SELECT id FROM {$p}sz_motoboy_fechamento
          WHERE motoboy_id = %d
            AND repasse_confirmado = 0
            AND data_fechamento < DATE(CONVERT_TZ(NOW(), '+00:00', '-03:00'))
            AND total_entregues > 0
            AND total_a_repassar > 0",
        $mb->id
    ) );

    if ( $fechamento_pendente ) {
        return new WP_REST_Response( [
            'ok'   => false,
            'erro' => 'Você tem um fechamento de caixa pendente do dia anterior. Confirme o repasse antes de iniciar nova rota.',
        ], 403 );
    }

    $package_code = sanitize_text_field( (string) ( $req->get_param( 'package_code' ) ?: $req->get_param( 'qr_code' ) ?: '' ) );
    if ( $package_code === '' ) {
        $json = $req->get_json_params();
        if ( is_array( $json ) ) {
            $package_code = sanitize_text_field( (string) ( $json['package_code'] ?? $json['qr_code'] ?? '' ) );
        }
    }
    if ( $package_code === '' ) {
        return new WP_REST_Response( [
            'ok' => false,
            'erro' => 'Para iniciar rota, leia o QR Code da etiqueta do pacote.',
            'em_rota' => 0,
            'qr_required' => true,
        ], 422 );
    }
    if ( function_exists( 'sz_mbc_start_route_by_qr' ) ) {
        $res = sz_mbc_start_route_by_qr( (int) $mb->id, $package_code, 'motoboy', (int) $mb->id, absint( $req->get_param( 'pedido_id' ) ) );
        if ( is_wp_error( $res ) ) {
            return new WP_REST_Response( [ 'ok' => false, 'erro' => $res->get_error_message(), 'em_rota' => 0 ], 409 );
        }
        $pedido_param = absint( $req->get_param( 'pedido_id' ) );
        if ( $pedido_param > 0 && (int) $res !== $pedido_param ) {
            return new WP_REST_Response( [ 'ok' => false, 'erro' => 'QR Code não corresponde ao pedido aberto.', 'em_rota' => 0 ], 409 );
        }
        return new WP_REST_Response( [ 'ok' => true, 'em_rota' => 1, 'pedido_id' => (int) $res ], 200 );
    }
    return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Controle de custódia indisponível. Atualize o plugin.', 'em_rota' => 0 ], 500 );

    // Suporte a seleção individual: se pedido_ids[] vier no body, usa só esses.
    // Sem pedido_ids: comportamento original — todos os embalados do motoboy.
    // Blindagem v323: alguns WebViews/PWAs/proxies entregam JSON de forma diferente;
    // por isso lemos get_param, JSON explícito e body bruto.
    $raw_ids = $req->get_param( 'pedido_ids' );
    if ( empty( $raw_ids ) ) {
        $json = $req->get_json_params();
        if ( is_array( $json ) && isset( $json['pedido_ids'] ) ) {
            $raw_ids = $json['pedido_ids'];
        }
    }
    if ( empty( $raw_ids ) ) {
        $body = json_decode( (string) $req->get_body(), true );
        if ( is_array( $body ) && isset( $body['pedido_ids'] ) ) {
            $raw_ids = $body['pedido_ids'];
        }
    }

    $ids_filtro = null;
    if ( is_array( $raw_ids ) && count( $raw_ids ) > 0 ) {
        $ids_filtro = array_values( array_filter( array_unique( array_map( 'absint', $raw_ids ) ) ) );
    }

    if ( $ids_filtro !== null && empty( $ids_filtro ) ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Nenhum pedido válido foi enviado pelo aplicativo.' ], 422 );
    }

    if ( $ids_filtro !== null ) {
        $placeholders = implode( ',', array_fill( 0, count( $ids_filtro ), '%d' ) );
        $pedidos = $wpdb->get_results( $wpdb->prepare(
            "SELECT id, wc_order_id, motoboy_id, status FROM {$p}sz_motoboy_pedidos
              WHERE status = 'embalado'
                AND id IN ({$placeholders})
                AND ( motoboy_id = %d OR motoboy_id IS NULL OR motoboy_id = 0 )",
            array_merge( $ids_filtro, [ (int) $mb->id ] )
        ) );
    } else {
        $pedidos = $wpdb->get_results( $wpdb->prepare(
            "SELECT id, wc_order_id, motoboy_id, status FROM {$p}sz_motoboy_pedidos
              WHERE motoboy_id = %d AND status = 'embalado'",
            (int) $mb->id
        ) );
    }

    if ( empty( $pedidos ) ) {
        return new WP_REST_Response( [
            'ok'      => false,
            'erro'    => 'Nenhum pedido embalado encontrado para este motoboy. Atualize a tela e confira se o pedido já está em rota.',
            'em_rota' => 0,
        ], 409 );
    }

    $atualizados = 0;
    $falhas      = [];
    foreach ( $pedidos as $ped ) {
        $extra = [];
        if ( empty( $ped->motoboy_id ) || (int) $ped->motoboy_id !== (int) $mb->id ) {
            $extra['motoboy_id'] = (int) $mb->id;
        }

        $ok = false;
        if ( function_exists( 'sz_motoboy_mudar_status' ) ) {
            $ok = sz_motoboy_mudar_status( (int) $ped->id, 'em_rota', $extra, 'motoboy', (int) $mb->id );
        }

        // Fallback defensivo: se por algum motivo o helper não atualizou, faz a transição mínima
        // igual ao desktop: tabela motoboy + espelho Woo/HPOS.
        $status_atual = (string) $wpdb->get_var( $wpdb->prepare(
            "SELECT status FROM {$p}sz_motoboy_pedidos WHERE id = %d LIMIT 1",
            (int) $ped->id
        ) );
        if ( ! $ok || $status_atual !== 'em_rota' ) {
            $data = array_merge( [
                'status'      => 'em_rota',
                'updated_at'  => function_exists( 'sz_motoboy_now_mysql' ) ? sz_motoboy_now_mysql() : current_time( 'mysql' ),
            ], $extra );
            $existing_em_rota = (string) $wpdb->get_var( $wpdb->prepare( "SELECT ts_em_rota FROM {$p}sz_motoboy_pedidos WHERE id = %d LIMIT 1", (int) $ped->id ) );
            if ( trim( $existing_em_rota ) === '' || trim( $existing_em_rota ) === '0000-00-00 00:00:00' ) {
                $data['ts_em_rota'] = function_exists( 'sz_motoboy_now_mysql' ) ? sz_motoboy_now_mysql() : current_time( 'mysql' );
            }
            $wpdb->update( $p . 'sz_motoboy_pedidos', $data, [ 'id' => (int) $ped->id ] );
            $ok = ( $wpdb->last_error === '' );

            if ( $ok && ! empty( $ped->wc_order_id ) && function_exists( 'wc_get_order' ) ) {
                $order = wc_get_order( (int) $ped->wc_order_id );
                if ( $order instanceof WC_Order ) {
                    $now = function_exists( 'sz_motoboy_now_mysql' ) ? sz_motoboy_now_mysql() : current_time( 'mysql' );
                    $order->update_meta_data( '_senderzz_delivery_mode', 'motoboy' );
                    $order->update_meta_data( '_senderzz_motoboy_flow_status', 'em_rota' );
                    $order->update_meta_data( '_senderzz_motoboy_status', 'em_rota' );
                    if ( ! $order->get_meta( '_senderzz_motoboy_em_rota_at', true ) ) { $order->update_meta_data( '_senderzz_motoboy_em_rota_at', $now ); }
                    $order->update_meta_data( '_senderzz_motoboy_status_updated_at', $now );
                    $order->update_meta_data( '_senderzz_motoboy_id', (int) $mb->id );
                    $order->update_meta_data( '_sz_motoboy_id', (int) $mb->id );
                    if ( ! empty( $mb->nome ) ) {
                        $order->update_meta_data( '_senderzz_motoboy_name', (string) $mb->nome );
                        $order->update_meta_data( '_sz_motoboy_name', (string) $mb->nome );
                    }
                    if ( function_exists( 'senderzz_set_order_status_from_motoboy_status' ) ) {
                        senderzz_set_order_status_from_motoboy_status( $order, 'em_rota', 'Senderzz COD Motoboy: pedido colocado em rota pelo app.' );
                    } elseif ( ! $order->has_status( 'em-rota' ) ) {
                        $order->update_status( 'em-rota', 'Senderzz COD Motoboy: pedido colocado em rota pelo app.' );
                    } else {
                        $order->save();
                    }
                }
            }
        }

        $status_final = (string) $wpdb->get_var( $wpdb->prepare(
            "SELECT status FROM {$p}sz_motoboy_pedidos WHERE id = %d LIMIT 1",
            (int) $ped->id
        ) );
        if ( $ok && $status_final === 'em_rota' ) {
            $atualizados++;
        } else {
            $falhas[] = (int) $ped->id;
        }
    }

    if ( $atualizados < 1 ) {
        return new WP_REST_Response( [
            'ok'      => false,
            'erro'    => 'Não foi possível colocar o pedido em rota. Tente atualizar a tela; se persistir, verifique permissões/tabela do motoboy.',
            'em_rota' => 0,
            'falhas'  => $falhas,
        ], 500 );
    }

    return new WP_REST_Response( [ 'ok' => true, 'em_rota' => $atualizados, 'falhas' => $falhas ], 200 );
}


function sz_mb_api_devolver_qr( WP_REST_Request $req ): WP_REST_Response {
    $mb = sz_mb_get_motoboy_by_token();
    if ( ! $mb || empty( $mb->id ) ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Sessão do motoboy expirada. Faça login novamente.' ], 401 );
    }
    if ( ! headers_sent() ) {
        header( 'X-LiteSpeed-Cache-Control: no-cache' );
        header( 'Cache-Control: no-store, no-cache, must-revalidate, max-age=0' );
        header( 'Pragma: no-cache' );
    }

    $package_code = sanitize_text_field( (string) ( $req->get_param( 'package_code' ) ?: $req->get_param( 'qr_code' ) ?: '' ) );
    if ( $package_code === '' ) {
        $json = $req->get_json_params();
        if ( is_array( $json ) ) {
            $package_code = sanitize_text_field( (string) ( $json['package_code'] ?? $json['qr_code'] ?? '' ) );
        }
    }
    if ( $package_code === '' ) {
        return new WP_REST_Response( [
            'ok' => false,
            'erro' => 'Para declarar devolução, leia o QR Code da etiqueta do pacote.',
            'qr_required' => true,
        ], 422 );
    }

    if ( function_exists( 'sz_mbc_declare_return_by_qr' ) ) {
        $res = sz_mbc_declare_return_by_qr( (int) $mb->id, $package_code, absint( $req->get_param( 'pedido_id' ) ) );
        if ( is_wp_error( $res ) ) {
            return new WP_REST_Response( [ 'ok' => false, 'erro' => $res->get_error_message() ], 409 );
        }
        $pedido_param = absint( $req->get_param( 'pedido_id' ) );
        if ( $pedido_param > 0 && (int) $res !== $pedido_param ) {
            return new WP_REST_Response( [ 'ok' => false, 'erro' => 'QR Code não corresponde ao pedido aberto.' ], 409 );
        }
        return new WP_REST_Response( [ 'ok' => true, 'pedido_id' => (int) $res, 'aguardando_ol' => true ], 200 );
    }
    return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Controle de custódia indisponível. Atualize o plugin.' ], 500 );
}


function sz_mb_api_pendentes_confirmacao( WP_REST_Request $req ): WP_REST_Response {
    $mb = sz_mb_get_motoboy_by_token();
    $user_ids = [];
    // Busca WP users vinculados a este motoboy
    global $wpdb;
    $orders = $wpdb->get_col( $wpdb->prepare(
        "SELECT om.order_id
           FROM {$wpdb->prefix}wc_orders_meta om
           INNER JOIN {$wpdb->prefix}wc_orders_meta om2
               ON om2.order_id = om.order_id
               AND om2.meta_key = '_sz_mb_confirmacao_repasse_motoboy_id'
               AND om2.meta_value = %d
          WHERE om.meta_key = '_sz_mb_pendente_confirmacao_repasse'
            AND om.meta_value = '1'",
        $mb->id
    ) ) ?: [];

    $result = [];
    foreach ( $orders as $order_id ) {
        $order = wc_get_order( $order_id );
        if ( ! $order ) continue;
        $result[] = [
            'order_id'      => (int) $order_id,
            'number'        => $order->get_order_number(),
            'valor'         => (float) $order->get_meta( '_senderzz_motoboy_pgto_total', true ),
            'valor_fmt'     => 'R$ ' . number_format( (float) $order->get_meta( '_senderzz_motoboy_pgto_total', true ), 2, ',', '.' ),
            'baixa_admin'   => (string) $order->get_meta( '_senderzz_motoboy_baixa_admin_nome', true ),
            'baixa_at'      => (string) $order->get_meta( '_senderzz_motoboy_baixa_at', true ),
            'recebedor'     => (string) $order->get_meta( '_senderzz_motoboy_recebedor_nome', true ),
        ];
    }

    return new WP_REST_Response( ['ok'=>true, 'pendentes'=>$result], 200 );
}


function sz_mb_api_upload_comprovante( WP_REST_Request $req ): WP_REST_Response {
    $mb         = sz_mb_get_motoboy_by_token();
    $pedido_id  = absint( $req->get_param('pedido_id') );
    $tipo_pgto  = sanitize_key( $req->get_param('tipo_pgto') ?: 'dinheiro' );
    $fotos      = (array) ( $req->get_param('fotos') ?: [] ); // array de base64
    $baixa_por  = sanitize_key( $req->get_param('baixa_por') ?: 'motoboy' );

    if ( ! in_array( $tipo_pgto, ['dinheiro','pix','cartao'], true ) ) $tipo_pgto = 'dinheiro';
    if ( empty( $fotos ) ) return new WP_REST_Response( ['ok'=>false,'erro'=>'Nenhuma foto enviada.'], 422 );
    if ( count( $fotos ) > 5 ) return new WP_REST_Response( ['ok'=>false,'erro'=>'Máximo de 5 fotos por comprovante.'], 422 );

    global $wpdb;
    $pedido = $wpdb->get_row( $wpdb->prepare(
        "SELECT * FROM {$wpdb->prefix}sz_motoboy_pedidos WHERE id=%d LIMIT 1", $pedido_id
    ) );
    if ( ! $pedido ) return new WP_REST_Response( ['ok'=>false,'erro'=>'Pedido não encontrado.'], 404 );

    $saved = [];
    foreach ( $fotos as $foto_b64 ) {
        [ $ok, $msg ] = sz_mb_validar_foto_base64( $foto_b64 );
        if ( ! $ok ) return new WP_REST_Response( ['ok'=>false,'erro'=>$msg], 422 );
        $path = sz_mb_salvar_foto_comprovante( $foto_b64, $pedido_id, $tipo_pgto );
        if ( ! $path ) return new WP_REST_Response( ['ok'=>false,'erro'=>'Erro ao salvar foto.'], 500 );

        $upload_dir = wp_upload_dir();
        $url = str_replace( $upload_dir['basedir'], $upload_dir['baseurl'], $path );

        $wpdb->insert( $wpdb->prefix . 'sz_motoboy_comprovantes', [
            'pedido_id'   => (int) $pedido->id,
            'wc_order_id' => (int) $pedido->wc_order_id,
            'motoboy_id'  => (int) $mb->id,
            'tipo_pgto'   => $tipo_pgto,
            'foto_url'    => $url,
            'foto_path'   => $path,
            'baixa_por'   => $baixa_por,
            'created_at'  => sz_motoboy_now_mysql(),
        ] );
        $comprovante_id = (int) $wpdb->insert_id;
        if ( $comprovante_id ) {
            // Double-write Go (Fase 1)
            do_action( 'sz_motoboy_comprovante_salvo', $comprovante_id, (int) $pedido->id );
        }
        $saved[] = $url;
    }

    // Atualiza contador no pedido
    $count = (int) $wpdb->get_var( $wpdb->prepare(
        "SELECT COUNT(*) FROM {$wpdb->prefix}sz_motoboy_comprovantes WHERE pedido_id=%d", $pedido->id
    ) );
    $wpdb->update( $wpdb->prefix . 'sz_motoboy_pedidos', ['comprovantes_count'=>$count], ['id'=>(int)$pedido->id] );

    // Salva URL da primeira foto no pedido WC também (retrocompat)
    $order = wc_get_order( $pedido->wc_order_id );
    if ( $order ) {
        foreach ( $saved as $i => $url ) {
            $order->update_meta_data( '_sz_mb_comprovante_' . $tipo_pgto . '_' . ($i+1), $url );
        }
        if ( ! $order->get_meta( '_senderzz_motoboy_comprovante_url', true ) ) {
            $order->update_meta_data( '_senderzz_motoboy_comprovante_url', $saved[0] );
        }
        $order->add_order_note( count($saved) . ' comprovante(s) de ' . $tipo_pgto . ' enviado(s) pelo motoboy ' . $mb->nome . '.' );
        $order->save();
    }

    return new WP_REST_Response( ['ok'=>true,'saved'=>count($saved),'urls'=>$saved], 200 );
}

function sz_mb_api_listar_comprovantes( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $order_id = absint( $req->get_param('order_id') );
    $rows = $wpdb->get_results( $wpdb->prepare(
        "SELECT tipo_pgto, foto_url, baixa_por, created_at FROM {$wpdb->prefix}sz_motoboy_comprovantes
          WHERE wc_order_id=%d ORDER BY created_at ASC",
        $order_id
    ), ARRAY_A ) ?: [];
    return new WP_REST_Response( ['ok'=>true,'comprovantes'=>$rows], 200 );
}

function sz_mb_api_confirmar_repasse_baixa( WP_REST_Request $req ): WP_REST_Response {
    $mb       = sz_mb_get_motoboy_by_token();
    $order_id = absint( $req->get_param('order_id') );
    $order    = wc_get_order( $order_id );
    if ( ! $order ) return new WP_REST_Response( ['ok'=>false,'erro'=>'Pedido não encontrado.'], 404 );

    $mb_id_expected = (int) $order->get_meta( '_sz_mb_confirmacao_repasse_motoboy_id', true );
    if ( $mb_id_expected !== (int) $mb->id ) {
        return new WP_REST_Response( ['ok'=>false,'erro'=>'Este pedido não pertence a você.'], 403 );
    }

    $order->update_meta_data( '_sz_mb_pendente_confirmacao_repasse', '0' );
    $order->update_meta_data( '_sz_mb_confirmacao_repasse_at', sz_motoboy_now_mysql() );
    $order->update_meta_data( '_sz_mb_confirmacao_repasse_confirmado', '1' );
    $order->add_order_note( 'Motoboy ' . $mb->nome . ' confirmou recebimento do dinheiro em caixa.' );
    $order->save();

    return new WP_REST_Response( ['ok'=>true,'msg'=>'Confirmação registrada.'], 200 );
}

function sz_mb_api_ping( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $mb  = sz_mb_get_motoboy_by_token();
    $lat = (float) $req->get_param('lat');
    $lng = (float) $req->get_param('lng');

    $wpdb->update( $wpdb->prefix . 'sz_motoboys', [
        'ultimo_lat'  => $lat,
        'ultimo_lng'  => $lng,
        'ultimo_ping' => sz_motoboy_now_mysql(),
    ], [ 'id' => $mb->id ] );

    // Auto "a caminho" quando motoboy chega até 2km do cliente
    $pedidos = $wpdb->get_results( $wpdb->prepare(
        "SELECT id,dest_lat,dest_lng,status FROM {$wpdb->prefix}sz_motoboy_pedidos
         WHERE motoboy_id=%d AND status='em_rota' AND dest_lat IS NOT NULL AND dest_lng IS NOT NULL",
        $mb->id
    ) );

    foreach ( (array) $pedidos as $p ) {
        $earth = 6371;
        $dLat = deg2rad( (float)$p->dest_lat - $lat );
        $dLng = deg2rad( (float)$p->dest_lng - $lng );
        $a = sin($dLat/2)*sin($dLat/2) + cos(deg2rad($lat))*cos(deg2rad((float)$p->dest_lat))*sin($dLng/2)*sin($dLng/2);
        $dist = $earth * (2 * atan2(sqrt($a), sqrt(1-$a)));
        if ( $dist <= 2 ) {
            sz_motoboy_mudar_status( (int)$p->id, 'a_caminho', [], 'motoboy', $mb->id );
        }
    }

    return new WP_REST_Response( [ 'ok' => true ] );
}

function sz_mb_api_entregar( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $mb             = sz_mb_get_motoboy_by_token();
    $pedido_id      = (int) $req->get_param('pedido_id');
    $recebedor_nome = sanitize_text_field( $req->get_param('recebedor_nome') );
    $cpf_raw        = sanitize_text_field( $req->get_param('cpf') );
    $cpf            = preg_replace( '/\D+/', '', $cpf_raw );
    $dinheiro       = sz_mb_parse_money( $req->get_param('pgto_dinheiro') );
    $pix            = sz_mb_parse_money( $req->get_param('pgto_pix') );
    $cartao         = sz_mb_parse_money( $req->get_param('pgto_cartao') );
    $lat_req        = $req->get_param('lat');
    $lng_req        = $req->get_param('lng');
    $acc_req        = $req->get_param('accuracy');
    $lat            = is_numeric( $lat_req ) ? (float) $lat_req : (float) $mb->ultimo_lat;
    $lng            = is_numeric( $lng_req ) ? (float) $lng_req : (float) $mb->ultimo_lng;
    $accuracy       = is_numeric( $acc_req ) ? (float) $acc_req : null;

    // Busca primeiro o pedido do motoboy logado. Se o pedido existir sem motoboy
    // responsável gravado, assume o motoboy logado como responsável na baixa.
    $pedido = $wpdb->get_row( $wpdb->prepare(
        "SELECT * FROM {$wpdb->prefix}sz_motoboy_pedidos WHERE id = %d AND motoboy_id = %d",
        $pedido_id, $mb->id
    ) );

    if ( ! $pedido ) {
        $pedido_sem_motoboy = $wpdb->get_row( $wpdb->prepare(
            "SELECT * FROM {$wpdb->prefix}sz_motoboy_pedidos WHERE id = %d",
            $pedido_id
        ) );
        if ( $pedido_sem_motoboy && empty( $pedido_sem_motoboy->motoboy_id ) ) {
            $wpdb->update(
                $wpdb->prefix . 'sz_motoboy_pedidos',
                [ 'motoboy_id' => (int) $mb->id ],
                [ 'id' => $pedido_id ],
                [ '%d' ],
                [ '%d' ]
            );
            $pedido = $wpdb->get_row( $wpdb->prepare(
                "SELECT * FROM {$wpdb->prefix}sz_motoboy_pedidos WHERE id = %d AND motoboy_id = %d",
                $pedido_id, $mb->id
            ) );
        }
    }

    if ( ! $pedido ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Pedido não encontrado para este motoboy.' ], 404 );
    }

    // A baixa deve funcionar tanto em rota quanto a caminho. O próprio GPS pode
    // promover em_rota -> a_caminho antes do motoboy tocar em Confirmar Entrega.
    if ( ! in_array( (string) $pedido->status, [ 'em_rota', 'a_caminho' ], true ) ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Pedido precisa estar em rota/a caminho para ser entregue.' ], 409 );
    }

    if ( trim( $recebedor_nome ) === '' || mb_strlen( trim( $recebedor_nome ) ) < 3 ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Informe o nome do recebedor.' ], 422 );
    }
    if ( ! sz_mb_validar_cpf( $cpf ) ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'CPF do recebedor inválido.' ], 422 );
    }
    // Assinatura removida — não é auditável
    $recebedor_tipo = sanitize_key( $req->get_param('recebedor_tipo') ?: 'cliente' );
    if ( ! in_array( $recebedor_tipo, ['cliente','terceiro'], true ) ) $recebedor_tipo = 'cliente';

    $valor_pedido = round( (float) $pedido->valor_pedido, 2 );
    $total_pago   = round( $dinheiro + $pix + $cartao, 2 );
    if ( $total_pago <= 0 ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Informe o valor recebido.' ], 422 );
    }
    if ( $total_pago > $valor_pedido + 0.009 ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Valor recebido não pode exceder o valor do pedido.' ], 422 );
    }
    if ( abs( $total_pago - $valor_pedido ) > 0.009 ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Para entregar, o total recebido deve bater exatamente com o valor do pedido.' ], 422 );
    }

    [ $gps_ok, $gps_msg ] = sz_mb_validar_gps_operacional( $lat, $lng, $accuracy );
    if ( ! $gps_ok ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => $gps_msg ], 422 );
    }

    sz_motoboy_mudar_status( $pedido_id, 'entregue', [
        'recebedor_nome'     => $recebedor_nome,
        'recebedor_cpf'      => $cpf,
        'recebedor_tipo'     => $recebedor_tipo,
        'pgto_dinheiro'      => $dinheiro,
        'pgto_pix'           => $pix,
        'pgto_cartao'        => $cartao,
        'entrega_lat'        => $lat,
        'entrega_lng'        => $lng,
        'baixa_por'          => 'motoboy',
        'baixa_motoboy_id'   => (int) $mb->id,
        'baixa_at'           => sz_motoboy_now_mysql(),
    ], 'motoboy', $mb->id );
    // sz_motoboy_mudar_status já fez update_status('completo') e gravou metas base.
    // Aqui gravamos apenas metas exclusivas da prova de entrega COD que o router não conhece.
    $order = wc_get_order( $pedido->wc_order_id );
    if ( $order ) {
        $now_br   = sz_motoboy_now_mysql();
        $cpf_mask = function_exists( 'sz_mb_mask_cpf' ) ? sz_mb_mask_cpf( $cpf ) : $cpf;
        $order->update_meta_data( '_senderzz_motoboy_entrega_confirmed_at',    $now_br );
        $order->update_meta_data( '_senderzz_motoboy_entrega_data_hora_br',    $now_br );
        $order->update_meta_data( '_senderzz_motoboy_recebedor_nome',          $recebedor_nome );
        $order->update_meta_data( '_senderzz_motoboy_recebedor_cpf',           $cpf );
        $order->update_meta_data( '_senderzz_motoboy_recebedor_cpf_formatado', $cpf_mask );
        $order->update_meta_data( '_senderzz_motoboy_recebedor_tipo',          $recebedor_tipo );
        $order->update_meta_data( '_senderzz_motoboy_baixa_por',               'motoboy' );
        $order->update_meta_data( '_senderzz_motoboy_baixa_motoboy_id',        (int) $mb->id );
        $order->update_meta_data( '_senderzz_motoboy_baixa_motoboy_nome',      (string) $mb->nome );
        $order->update_meta_data( '_senderzz_motoboy_baixa_at',                $now_br );
        // Metas canônicas/legadas para o pedido ficar atribuído ao motoboy responsável.
        $order->update_meta_data( '_senderzz_motoboy_id',                     (int) $mb->id );
        $order->update_meta_data( '_senderzz_motoboy_name',                   (string) $mb->nome );
        $order->update_meta_data( '_sz_motoboy_id',                           (int) $mb->id );
        $order->update_meta_data( '_sz_motoboy_name',                         (string) $mb->nome );
        $order->update_meta_data( '_motoboy_user_id',                         (int) $mb->id );
        $order->update_meta_data( '_motoboy_name',                            (string) $mb->nome );
        $order->update_meta_data( '_senderzz_motoboy_responsavel_id',          (int) $mb->id );
        $order->update_meta_data( '_senderzz_motoboy_responsavel_nome',        (string) $mb->nome );
        $order->update_meta_data( '_senderzz_motoboy_entregador_id',           (int) $mb->id );
        $order->update_meta_data( '_senderzz_motoboy_entregador_nome',         (string) $mb->nome );
        $order->update_meta_data( '_senderzz_motoboy_entrega_lat',             $lat );
        $order->update_meta_data( '_senderzz_motoboy_entrega_lng',             $lng );
        if ( $accuracy !== null ) {
            $order->update_meta_data( '_senderzz_motoboy_entrega_gps_accuracy', $accuracy );
        }
        $order->update_meta_data( '_senderzz_motoboy_pgto_dinheiro', $dinheiro );
        $order->update_meta_data( '_senderzz_motoboy_pgto_pix',      $pix );
        $order->update_meta_data( '_senderzz_motoboy_pgto_cartao',   $cartao );
        $order->update_meta_data( '_senderzz_motoboy_pgto_total',    $total_pago );
        $order->update_meta_data( '_senderzz_motoboy_valor_pedido',  $valor_pedido );
        // Nota ao histórico do pedido (não dispara hook de status — só add_order_note)
        $order->add_order_note(
            'Senderzz COD: entregue por ' . $mb->nome .
            '. Recebedor: ' . $recebedor_nome . ' / CPF ' . $cpf_mask .
            '. Total: R$ ' . number_format( $total_pago, 2, ',', '.' ) .
            ' (' . implode( ' + ', array_filter([
                $dinheiro > 0 ? 'Dinheiro R$' . number_format($dinheiro,2,',','.') : '',
                $pix      > 0 ? 'PIX R$'      . number_format($pix,2,',','.')      : '',
                $cartao   > 0 ? 'Cartão R$'   . number_format($cartao,2,',','.')   : '',
            ]) ) . ').'
        );
        $order->save();
        if ( function_exists( 'sz_cod_wallet_record_delivery' ) ) {
            sz_cod_wallet_record_delivery( $order, $total_pago );
        }
    }

    sz_mb_gerar_fechamento( $mb->id, (int) $pedido->cd_id );

    return new WP_REST_Response( [ 'ok' => true ] );
}

function sz_mb_api_frustrar( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $mb        = sz_mb_get_motoboy_by_token();
    $pedido_id = (int) $req->get_param('pedido_id');
    $motivo    = sanitize_text_field( $req->get_param('motivo') );
    $observacao = sanitize_textarea_field( $req->get_param('observacao') );
    $foto_b64  = $req->get_param('foto_base64');
    $lat_raw   = $req->get_param('lat');
    $lng_raw   = $req->get_param('lng');
    $acc_raw   = $req->get_param('accuracy');
    $lat       = is_numeric( $lat_raw ) ? (float) $lat_raw : 0.0;
    $lng       = is_numeric( $lng_raw ) ? (float) $lng_raw : 0.0;
    $accuracy  = is_numeric( $acc_raw ) ? (float) $acc_raw : null;

    $pedido = $wpdb->get_row( $wpdb->prepare(
        "SELECT * FROM {$wpdb->prefix}sz_motoboy_pedidos WHERE id = %d AND motoboy_id = %d",
        $pedido_id, $mb->id
    ) );

    if ( ! $pedido ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Pedido não encontrado.' ], 404 );
    }
    if ( $pedido->status !== 'em_rota' ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Pedido precisa estar em rota para ser frustrado.' ], 409 );
    }
    if ( trim( $motivo ) === '' ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Informe o motivo da tentativa frustrada.' ], 422 );
    }
    if ( trim( $observacao ) === '' ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Descreva o motivo da frustração.' ], 422 );
    }
    [ $gps_ok, $gps_msg ] = sz_mb_validar_gps_operacional( $lat, $lng, $accuracy );
    if ( ! $gps_ok ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => $gps_msg ], 422 );
    }
    [ $foto_ok, $foto_msg ] = sz_mb_validar_foto_base64( $foto_b64 );
    if ( ! $foto_ok ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => $foto_msg ], 422 );
    }

    // Salvar foto auditável. A foto deve vir do input mobile com capture="environment";
    // o GPS oficial é salvo separadamente para evitar depender de EXIF, que navegadores removem.
    $foto_path = sz_mb_salvar_foto( $foto_b64, $pedido_id );
    if ( ! $foto_path ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Não foi possível salvar a foto. Tente novamente.' ], 500 );
    }

    $taxa_info = sz_motoboy_calcular_taxa_frustrado( $pedido->wc_order_id );
    // Valor financeiro do frustrado é congelado no momento da baixa. Alterações futuras de taxa não recalculam este pedido.
    $taxa_info['taxa'] = function_exists( 'sz_mbw_get_taxa_frustrado' ) ? sz_mbw_get_taxa_frustrado( (int) $mb->id ) : (float) get_option( 'sz_mbw_taxa_frustrado', get_option( 'sz_motoboy_taxa_frustrado', 5.00 ) );

    $now_br = sz_motoboy_now_mysql();
    sz_motoboy_mudar_status( $pedido_id, 'frustrado', [
        'motoboy_id'          => (int) $mb->id,
        'baixa_por'           => 'motoboy',
        'baixa_motoboy_id'    => (int) $mb->id,
        'baixa_at'            => $now_br,
        'frustrado_motivo'    => $motivo,
        'frustrado_observacao'=> $observacao,
        'frustrado_isento'    => $taxa_info['isento'] ? 1 : 0,
        'valor_taxa_frustrado'=> $taxa_info['taxa'],
        'entrega_foto'        => $foto_path,
        'entrega_lat'         => $lat,
        'entrega_lng'         => $lng,
    ], 'motoboy', $mb->id );
    // sz_motoboy_mudar_status já atualizou flow_status, metas base e update_status('frustrado').
    // Aqui gravamos apenas metas de auditoria exclusivas da frustração (foto, GPS detalhado).
    $order = wc_get_order( $pedido->wc_order_id );
    if ( $order ) {
        $order->update_meta_data( '_senderzz_motoboy_baixa_por',          'motoboy' );
        $order->update_meta_data( '_senderzz_motoboy_baixa_motoboy_id',   (int) $mb->id );
        $order->update_meta_data( '_senderzz_motoboy_baixa_motoboy_nome', (string) $mb->nome );
        $order->update_meta_data( '_senderzz_motoboy_baixa_at',           $now_br );
        $order->update_meta_data( '_senderzz_motoboy_frustrado_motivo',    $motivo );
        $order->update_meta_data( '_senderzz_motoboy_frustrado_observacao', $observacao );
        $order->update_meta_data( '_senderzz_motoboy_frustrado_foto',      $foto_path );
        $order->update_meta_data( '_senderzz_motoboy_frustrado_lat',       $lat );
        $order->update_meta_data( '_senderzz_motoboy_frustrado_lng',       $lng );
        if ( $accuracy !== null ) {
            $order->update_meta_data( '_senderzz_motoboy_frustrado_gps_accuracy', $accuracy );
        }
        $order->add_order_note(
            'Senderzz COD: tentativa frustrada por ' . $mb->nome .
            '. Motivo: ' . $motivo . '. ' . $observacao .
            ( $taxa_info['isento'] ? ' (isento — 1ª tentativa).' : ' Taxa: R$ ' . number_format( $taxa_info['taxa'], 2, ',', '.' ) . '.' )
        );
        $order->save();
    }

    // Notifica cliente para reagendar
    do_action( 'sz_motoboy_frustrado', $pedido_id, $pedido->wc_order_id );

    sz_mb_gerar_fechamento( $mb->id, (int) $pedido->cd_id );

    return new WP_REST_Response( [
        'ok'     => true,
        'isento' => $taxa_info['isento'],
        'taxa'   => $taxa_info['taxa'],
    ] );
}

function sz_mb_api_get_fechamento( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $mb   = sz_mb_get_motoboy_by_token();
    $data = sanitize_text_field( $req->get_param('data') ?: ( new DateTimeImmutable( 'now', new DateTimeZone( 'America/Sao_Paulo' ) ) )->format( 'Y-m-d' ) );

    $fech = $wpdb->get_row( $wpdb->prepare(
        "SELECT * FROM {$wpdb->prefix}sz_motoboy_fechamento
          WHERE motoboy_id = %d AND data_fechamento = %s",
        $mb->id, $data
    ) );

    if ( ! $fech ) {
        sz_mb_gerar_fechamento( $mb->id, (int) $mb->cd_id, $data );
        $fech = $wpdb->get_row( $wpdb->prepare(
            "SELECT * FROM {$wpdb->prefix}sz_motoboy_fechamento
              WHERE motoboy_id = %d AND data_fechamento = %s",
            $mb->id, $data
        ) );
    }

    return new WP_REST_Response( [ 'ok' => true, 'fechamento' => $fech ] );
}

function sz_mb_api_confirmar_repasse( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $mb   = sz_mb_get_motoboy_by_token();
    $data = sanitize_text_field( $req->get_param('data') ?: ( new DateTimeImmutable( 'now', new DateTimeZone( 'America/Sao_Paulo' ) ) )->format( 'Y-m-d' ) );

    // Valida formato da data
    if ( ! preg_match( '/^\d{4}-\d{2}-\d{2}$/', $data ) ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Data inválida.' ], 422 );
    }

    // Não permite confirmar datas futuras
    if ( $data > ( new DateTimeImmutable( 'now', new DateTimeZone( 'America/Sao_Paulo' ) ) )->format( 'Y-m-d' ) ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Não é possível confirmar repasse de data futura.' ], 422 );
    }

    // Verifica que o fechamento pertence a este motoboy e tem entregas reais a repassar
    $fech = $wpdb->get_row( $wpdb->prepare(
        "SELECT id, total_entregues, total_a_repassar, repasse_confirmado
           FROM {$wpdb->prefix}sz_motoboy_fechamento
          WHERE motoboy_id = %d AND data_fechamento = %s
          LIMIT 1",
        $mb->id, $data
    ) );

    if ( ! $fech ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Fechamento não encontrado para esta data.' ], 404 );
    }
    if ( (int) $fech->total_entregues <= 0 ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Sem entregas registradas nesta data.' ], 409 );
    }
    if ( (float) $fech->total_a_repassar <= 0 ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Nenhum valor pendente de repasse nesta data.' ], 409 );
    }
    if ( $fech->repasse_confirmado ) {
        return new WP_REST_Response( [ 'ok' => true, 'msg' => 'Repasse já confirmado anteriormente.' ] );
    }

    $wpdb->update(
        $wpdb->prefix . 'sz_motoboy_fechamento',
        [ 'repasse_confirmado' => 1, 'repasse_ts' => sz_motoboy_now_mysql() ],
        [ 'id' => (int) $fech->id ]   // usa PK — mais seguro que motoboy_id+data
    );

    return new WP_REST_Response( [ 'ok' => true ] );
}

function sz_mb_api_alan_pedidos( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $p      = $wpdb->prefix;
    $status = sanitize_text_field( $req->get_param('status') ?: 'agendado,embalado,em_rota,entregue,frustrado,cancelado' );
    $cd_id  = (int) $req->get_param('cd_id');

    $placeholders = implode( ',', array_fill( 0, count( explode(',', $status) ), '%s' ) );
    $args = array_merge( explode(',', $status) );

    $where_cd = '';
    if ( $cd_id ) {
        $where_cd = $wpdb->prepare( ' AND mp.cd_id = %d', $cd_id );
    }

    $pedidos = $wpdb->get_results( $wpdb->prepare(
        "SELECT mp.*, m.nome AS motoboy_nome, z.nome AS zona_nome, cd.nome AS cd_nome
           FROM {$p}sz_motoboy_pedidos mp
           LEFT JOIN {$p}sz_motoboys m ON m.id = mp.motoboy_id
           LEFT JOIN {$p}sz_motoboy_zonas z ON z.id = mp.zona_id
           LEFT JOIN {$p}sz_motoboy_cds cd ON cd.id = mp.cd_id
          WHERE mp.status IN ($placeholders)
            $where_cd
          ORDER BY mp.created_at DESC
          LIMIT 200",
        ...$args
    ) );

    return new WP_REST_Response( [ 'ok' => true, 'pedidos' => $pedidos ] );
}

function sz_mb_api_alan_embalar( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $pedido_id  = (int) $req->get_param('pedido_id');
    $motoboy_id = (int) $req->get_param('motoboy_id');

    if ( ! $motoboy_id ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Defina o motoboy responsável antes de embalar o pedido.' ], 422 );
    }

    $ok = sz_motoboy_mudar_status( $pedido_id, 'embalado', [ 'motoboy_id' => $motoboy_id ], 'alan', get_current_user_id() );

    return new WP_REST_Response( [ 'ok' => $ok ] );
}

function sz_mb_api_alan_confirmar_fechamento( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $motoboy_id = (int) $req->get_param('motoboy_id');
    $data       = sanitize_text_field( $req->get_param('data') ?: ( new DateTimeImmutable( 'now', new DateTimeZone( 'America/Sao_Paulo' ) ) )->format( 'Y-m-d' ) );

    $wpdb->update( $wpdb->prefix . 'sz_motoboy_fechamento', [
        'alan_confirmou' => 1,
        'alan_ts'        => sz_motoboy_now_mysql(),
    ], [ 'motoboy_id' => $motoboy_id, 'data_fechamento' => $data ] );

    return new WP_REST_Response( [ 'ok' => true ] );
}

function sz_mb_api_alan_dashboard( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $p    = $wpdb->prefix;
    $data = ( new DateTimeImmutable( 'now', new DateTimeZone( 'America/Sao_Paulo' ) ) )->format( 'Y-m-d' );

    $resumo = $wpdb->get_results(
        "SELECT mp.status, COUNT(*) AS total
           FROM {$p}sz_motoboy_pedidos mp
          WHERE DATE(mp.created_at) = '' . $data . ''
          GROUP BY mp.status"
    );

    $motoboys = $wpdb->get_results(
        "SELECT m.id, m.nome, z.nome AS zona, cd.nome AS cd,
                COUNT(mp.id) AS pedidos,
                SUM(CASE WHEN mp.status='entregue' THEN 1 ELSE 0 END) AS entregues,
                SUM(CASE WHEN mp.status='frustrado' THEN 1 ELSE 0 END) AS frustrados
           FROM {$p}sz_motoboys m
           LEFT JOIN {$p}sz_motoboy_zonas z ON z.id = m.zona_id
           LEFT JOIN {$p}sz_motoboy_cds cd ON cd.id = m.cd_id
           LEFT JOIN {$p}sz_motoboy_pedidos mp ON mp.motoboy_id = m.id AND DATE(mp.created_at) = '' . $data . ''
          WHERE m.ativo = 1
          GROUP BY m.id"
    );

    $fechamentos_pendentes = (int) $wpdb->get_var(
        "SELECT COUNT(*) FROM {$p}sz_motoboy_fechamento
          WHERE data_fechamento < '$data'
            AND alan_confirmou = 0
            AND total_entregues > 0
            AND total_a_repassar > 0"
    );

    return new WP_REST_Response( [
        'ok'                   => true,
        'data'                 => $data,
        'resumo_status'        => $resumo,
        'motoboys'             => $motoboys,
        'fechamentos_pendentes'=> $fechamentos_pendentes,
    ] );
}

function sz_mb_api_tracking( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $order_id = (int) $req->get_param('order_id');

    $pedido = $wpdb->get_row( $wpdb->prepare(
        "SELECT mp.status, mp.ts_aprovado, mp.ts_embalado, mp.ts_em_rota,
                mp.ts_a_caminho, mp.ts_entregue, mp.ts_frustrado,
                mp.dest_cidade, mp.reagendado_para,
                m.ultimo_lat, m.ultimo_lng
           FROM {$wpdb->prefix}sz_motoboy_pedidos mp
           LEFT JOIN {$wpdb->prefix}sz_motoboys m ON m.id = mp.motoboy_id
          WHERE mp.wc_order_id = %d",
        $order_id
    ) );

    if ( ! $pedido ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Pedido não encontrado.' ], 404 );
    }

    // Não expõe lat/lng do motoboy se já entregue/frustrado
    if ( in_array( $pedido->status, ['entregue','frustrado','cancelado'] ) ) {
        $pedido->ultimo_lat = null;
        $pedido->ultimo_lng = null;
    }

    return new WP_REST_Response( [ 'ok' => true, 'tracking' => $pedido ] );
}


function sz_mb_api_tracking_reagendar( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;

    $order_id = (int) $req->get_param( 'order_id' );
    $date     = sanitize_text_field( (string) ( $req->get_param( 'date' ) ?: $req->get_param( 'delivery_date' ) ?: '' ) );

    if ( ! $order_id || ! preg_match( '/^\d{4}-\d{2}-\d{2}$/', $date ) ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Informe uma data válida.' ], 422 );
    }
    if ( $date < date( 'Y-m-d' ) ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'A nova data não pode ser anterior a hoje.' ], 422 );
    }

    $pedido = $wpdb->get_row( $wpdb->prepare(
        "SELECT * FROM {$wpdb->prefix}sz_motoboy_pedidos WHERE wc_order_id = %d LIMIT 1",
        $order_id
    ) );
    if ( ! $pedido ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Pedido não encontrado.' ], 404 );
    }

    $status_atual = (string) $pedido->status;
    if ( ! in_array( $status_atual, [ 'agendado', 'reagendado', 'frustrado' ], true ) ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Este pedido só pode ser reagendado pelo cliente quando estiver agendado, reagendado ou frustrado.' ], 409 );
    }

    $order = function_exists( 'wc_get_order' ) ? wc_get_order( $order_id ) : null;
    if ( ! $order instanceof \WC_Order ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Pedido WooCommerce não encontrado.' ], 404 );
    }

    // V-SEC-03: validar posse do pedido via order key (enviado no e-mail de confirmação WC).
    // Sem isso, qualquer pessoa com o order_id numérico pode reagendar pedidos alheios (IDOR).
    $order_key_provided = sanitize_text_field( (string) ( $req->get_param('key') ?: '' ) );
    if ( ! $order_key_provided || ! hash_equals( $order->get_order_key(), $order_key_provided ) ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Chave do pedido inválida.' ], 403 );
    }

    $city = (string) ( $pedido->dest_cidade ?? '' );
    if ( ! $city ) $city = (string) ( $order->get_shipping_city() ?: $order->get_billing_city() );
    if ( $city && function_exists( 'sz_dr_date_allowed_for_city' ) && ! sz_dr_date_allowed_for_city( $city, $date ) ) {
        $region = function_exists( 'sz_dr_region_label_for_city' ) ? sz_dr_region_label_for_city( $city ) : '';
        $msg = $region
            ? sprintf( 'A cidade %s pertence à rota %s. Escolha uma data permitida para essa rota.', $city, $region )
            : sprintf( 'A data selecionada não é permitida para a cidade %s.', $city );
        return new WP_REST_Response( [ 'ok' => false, 'erro' => $msg ], 422 );
    }

    $new_order_id = $order_id;

    if ( $status_atual === 'frustrado' ) {
        // Frustrado: cria novo pedido com os mesmos dados e nova data, sem reaproveitar o pedido frustrado.
        $new_order = wc_create_order( [ 'customer_id' => $order->get_customer_id() ] );
        if ( ! $new_order instanceof \WC_Order ) {
            return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Não foi possível criar o novo pedido.' ], 500 );
        }

        $new_order->set_address( $order->get_address( 'billing' ), 'billing' );
        $new_order->set_address( $order->get_address( 'shipping' ), 'shipping' );
        $new_order->set_currency( $order->get_currency() );
        $new_order->set_payment_method( $order->get_payment_method() );
        $new_order->set_payment_method_title( $order->get_payment_method_title() );
        if ( method_exists( $new_order, 'set_created_via' ) ) $new_order->set_created_via( 'senderzz_reagendamento_cliente' );

        foreach ( $order->get_items( 'line_item' ) as $item ) {
            $product = $item->get_product();
            $new_item = new WC_Order_Item_Product();
            if ( $product ) $new_item->set_product( $product );
            $new_item->set_name( function_exists('senderzz_clean_product_label') ? senderzz_clean_product_label( $item->get_name() ) : $item->get_name() );
            $new_item->set_quantity( $item->get_quantity() );
            $new_item->set_subtotal( $item->get_subtotal() );
            $new_item->set_total( $item->get_total() );
            foreach ( $item->get_meta_data() as $meta ) {
                $new_item->add_meta_data( $meta->key, $meta->value, true );
            }
            $new_order->add_item( $new_item );
        }
        foreach ( $order->get_items( 'shipping' ) as $ship ) {
            $new_ship = new WC_Order_Item_Shipping();
            $new_ship->set_method_title( $ship->get_method_title() );
            $new_ship->set_method_id( $ship->get_method_id() );
            $new_ship->set_total( $ship->get_total() );
            $new_order->add_item( $new_ship );
        }
        foreach ( $order->get_meta_data() as $meta ) {
            $key = (string) $meta->key;
            if ( in_array( $key, [ '_edit_lock', '_edit_last' ], true ) ) continue;
            $new_order->update_meta_data( $key, $meta->value );
        }
        $new_order->update_meta_data( '_sz_delivery_date', $date );
        $new_order->update_meta_data( '_senderzz_delivery_date', $date );
        $new_order->update_meta_data( '_sz_motoboy_entrega_data', $date );
        $new_order->update_meta_data( '_senderzz_motoboy_reagendado_para', $date );
        $new_order->update_meta_data( '_senderzz_delivery_mode', 'motoboy' );
        $new_order->update_meta_data( '_senderzz_motoboy_flow_status', 'reagendado' );
        $new_order->update_meta_data( '_senderzz_motoboy_status', 'reagendado' );
        $new_order->update_meta_data( '_senderzz_original_frustrado_order_id', $order_id );
        $new_order->calculate_totals();
        $new_order->update_status( 'reagendado', 'Senderzz: novo pedido criado por reagendamento do cliente após frustração.' );
        $new_order->save();
        $new_order_id = $new_order->get_id();

        $row = (array) $pedido;
        unset( $row['id'] );
        $row['wc_order_id'] = $new_order_id;
        $row['status'] = 'reagendado';
        $row['reagendado_para'] = $date;
        $row['ts_aprovado'] = null;
        $row['ts_embalado'] = null;
        $row['ts_em_rota'] = null;
        $row['ts_a_caminho'] = null;
        $row['ts_entregue'] = null;
        $row['ts_frustrado'] = null;
        $row['created_at'] = sz_motoboy_now_mysql();
        $row['updated_at'] = sz_motoboy_now_mysql();
        $wpdb->insert( $wpdb->prefix . 'sz_motoboy_pedidos', $row );

        $order->add_order_note( 'Senderzz: cliente reagendou pedido frustrado para ' . wp_date( 'd/m/Y', strtotime( $date . 'T12:00:00' ) ) . '. Novo pedido #' . $new_order_id . '.' );
        $order->save();
    } else {
        // Agendado/Reagendado: mantém no mesmo pedido e altera somente a data.
        $order->update_meta_data( '_sz_delivery_date', $date );
        $order->update_meta_data( '_senderzz_delivery_date', $date );
        $order->update_meta_data( '_sz_motoboy_entrega_data', $date );
        $order->update_meta_data( '_senderzz_motoboy_reagendado_para', $date );
        $order->update_meta_data( '_senderzz_motoboy_flow_status', 'reagendado' );
        $order->update_meta_data( '_senderzz_motoboy_status', 'reagendado' );
        $order->update_status( 'reagendado', 'Senderzz: entrega reagendada pelo cliente.' );
        $order->save();

        $wpdb->update(
            $wpdb->prefix . 'sz_motoboy_pedidos',
            [ 'status' => 'reagendado', 'reagendado_para' => $date, 'updated_at' => sz_motoboy_now_mysql() ],
            [ 'id' => (int) $pedido->id ],
            [ '%s', '%s', '%s' ],
            [ '%d' ]
        );
        do_action( 'sz_motoboy_status_changed', (int) $pedido->id, $status_atual, 'reagendado', $pedido );
    }

    return new WP_REST_Response( [
        'ok'           => true,
        'message'      => 'Entrega reagendada para ' . wp_date( 'd/m/Y', strtotime( $date . 'T12:00:00' ) ) . '.',
        'order_id'     => $new_order_id,
        'delivery_raw' => $date,
    ] );
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

function sz_mb_gerar_fechamento( int $motoboy_id, int $cd_id, string $data = null ): void {
    global $wpdb;
    // Sempre usa data no timezone de Brasília.
    $data = $data ?: ( new DateTimeImmutable( 'now', new DateTimeZone( 'America/Sao_Paulo' ) ) )->format( 'Y-m-d' );
    $p    = $wpdb->prefix;

    // v313: conciliação bancária é somente de ENTREGAS com valor recebido > 0,
    // vinculadas ao motoboy responsável atual da baixa. Frustrado não entra aqui;
    // vai direto para a carteira do motoboy.
    $stats = $wpdb->get_row( $wpdb->prepare(
        "SELECT COUNT(*) AS total,
                COUNT(*) AS entregues,
                0 AS frustrados,
                SUM(pgto_dinheiro) AS dinheiro,
                SUM(pgto_pix)      AS pix,
                SUM(pgto_cartao)   AS cartao
           FROM {$p}sz_motoboy_pedidos
          WHERE COALESCE(NULLIF(baixa_motoboy_id,0), motoboy_id) = %d
            AND status = 'entregue'
            AND (pgto_dinheiro + pgto_pix + pgto_cartao) > 0
            AND DATE(COALESCE(baixa_at, ts_entregue, updated_at, created_at)) = %s",
        $motoboy_id, $data
    ) );

    if ( ! $stats || (int) $stats->entregues <= 0 || ((float)$stats->dinheiro + (float)$stats->pix + (float)$stats->cartao) <= 0 ) {
        $wpdb->delete( "{$p}sz_motoboy_fechamento", [
            'motoboy_id'       => $motoboy_id,
            'data_fechamento'  => $data,
        ], [ '%d', '%s' ] );
        return;
    }

    $total = (float) $stats->dinheiro + (float) $stats->pix + (float) $stats->cartao;
    $wpdb->query( $wpdb->prepare(
        "INSERT INTO {$p}sz_motoboy_fechamento
            (motoboy_id, cd_id, data_fechamento, total_pedidos, total_entregues, total_frustrados,
             total_dinheiro, total_pix, total_cartao, total_a_repassar)
         VALUES (%d,%d,%s,%d,%d,0,%f,%f,%f,%f)
         ON DUPLICATE KEY UPDATE
            total_pedidos    = VALUES(total_pedidos),
            total_entregues  = VALUES(total_entregues),
            total_frustrados = 0,
            total_dinheiro   = VALUES(total_dinheiro),
            total_pix        = VALUES(total_pix),
            total_cartao     = VALUES(total_cartao),
            total_a_repassar = VALUES(total_a_repassar),
            updated_at       = CURRENT_TIMESTAMP",
        $motoboy_id, $cd_id, $data,
        (int) $stats->total,
        (int) $stats->entregues,
        (float) $stats->dinheiro,
        (float) $stats->pix,
        (float) $stats->cartao,
        $total
    ) );
}

function sz_mb_salvar_foto( string $base64, int $pedido_id ): ?string {
    return sz_mb_salvar_foto_interna( $base64, 'frustrado-' . $pedido_id );
}

/**
 * Salva foto de comprovante de pagamento.
 * Prefixo diferente de frustrado para facilitar auditoria por tipo.
 */
function sz_mb_salvar_foto_comprovante( string $base64, int $pedido_id, string $tipo_pgto ): ?string {
    return sz_mb_salvar_foto_interna( $base64, 'comp-' . $pedido_id . '-' . $tipo_pgto );
}

/**
 * Núcleo compartilhado de persistência de foto base64.
 * Não chamar diretamente — use sz_mb_salvar_foto ou sz_mb_salvar_foto_comprovante.
 */
function sz_mb_salvar_foto_interna( string $base64, string $prefix ): ?string {
    $upload_dir = wp_upload_dir();
    $dir        = trailingslashit( $upload_dir['basedir'] ) . 'sz-motoboy-fotos/';
    wp_mkdir_p( $dir );

    if ( ! preg_match( '#^data:image/(jpeg|jpg|png|webp);base64,#i', $base64, $m ) ) return null;
    $ext = strtolower( $m[1] );
    if ( $ext === 'jpeg' ) $ext = 'jpg';

    $payload = preg_replace( '#^data:image/[a-zA-Z0-9.+-]+;base64,#', '', $base64 );
    $data    = base64_decode( $payload, true );
    if ( ! $data ) return null;

    $filename = $prefix . '-' . gmdate( 'YmdHis' ) . '-' . wp_generate_password( 8, false, false ) . '.' . $ext;
    $path     = $dir . $filename;
    if ( false === file_put_contents( $path, $data, LOCK_EX ) ) return null;

    return trailingslashit( $upload_dir['baseurl'] ) . 'sz-motoboy-fotos/' . $filename;
}


if ( ! function_exists( 'sz_mb_cep_exists' ) ) {
    /**
     * Validação real de CEP com proteção de CPU/rede.
     * - Cache estático por request para não consultar várias vezes na mesma tela.
     * - Transient por CEP para não repetir ViaCEP.
     * - Timeout curto para não travar checkout/hospedagem.
     * - Retorna null quando não conseguiu consultar; nesse caso a regra de zona decide.
     */
    function sz_mb_cep_exists( string $cep ): ?bool {
        static $local_cache = [];
        $cep = preg_replace( '/\D/', '', $cep );
        if ( strlen( $cep ) !== 8 || preg_match( '/^(\d)\1{7}$/', $cep ) ) return false;
        if ( array_key_exists( $cep, $local_cache ) ) return $local_cache[ $cep ];

        $key = 'sz_mb_cep_exists_' . $cep;
        $cached = get_transient( $key );
        if ( $cached === 'yes' ) return $local_cache[ $cep ] = true;
        if ( $cached === 'no' ) return $local_cache[ $cep ] = false;
        if ( $cached === 'fail' ) return $local_cache[ $cep ] = null;

        $res = wp_remote_get( 'https://viacep.com.br/ws/' . rawurlencode( $cep ) . '/json/', [
            'timeout'     => 0.8,
            'redirection' => 0,
            'headers'     => [ 'Accept' => 'application/json' ],
        ] );
        if ( is_wp_error( $res ) ) {
            set_transient( $key, 'fail', 10 * MINUTE_IN_SECONDS );
            return $local_cache[ $cep ] = null;
        }
        $code = (int) wp_remote_retrieve_response_code( $res );
        if ( $code < 200 || $code >= 300 ) {
            set_transient( $key, 'fail', 10 * MINUTE_IN_SECONDS );
            return $local_cache[ $cep ] = null;
        }
        $body = json_decode( (string) wp_remote_retrieve_body( $res ), true );
        if ( is_array( $body ) && ! empty( $body['erro'] ) ) {
            set_transient( $key, 'no', 30 * DAY_IN_SECONDS );
            return $local_cache[ $cep ] = false;
        }
        if ( is_array( $body ) && ! empty( $body['cep'] ) ) {
            set_transient( $key, 'yes', 30 * DAY_IN_SECONDS );
            return $local_cache[ $cep ] = true;
        }
        set_transient( $key, 'fail', 10 * MINUTE_IN_SECONDS );
        return $local_cache[ $cep ] = null;
    }
}

// ─── Endpoint público: verifica cobertura de CEP ─────────────────────────────
function sz_mb_api_zona_cep( WP_REST_Request $req ): WP_REST_Response {
    $cep  = preg_replace( '/\D/', '', $req->get_param('cep') ?? '' );
    $block_msg = 'CEP fora da área de entrega do Motoboy Senderzz. Informe um CEP atendido para continuar.';
    if ( strlen($cep) !== 8 || preg_match( '/^(\d)\1{7}$/', $cep ) ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'CEP fora da área.', 'mensagem' => $block_msg ], 400 );
    }

    // Consulta leve por padrão: valida zona local sem depender de API externa.
    // A validação real do CEP só roda quando o checkout pedir explicitamente real=1
    // (normalmente no clique/submit). Isso evita travar o checkout e derrubar CPU.
    $real_check = (string) $req->get_param( 'real' ) === '1';
    $cache_key = 'sz_mb_zona_cep_resp_v390_' . ( $real_check ? 'real_' : 'zone_' ) . $cep;
    $cached = get_transient( $cache_key );
    if ( is_array( $cached ) ) {
        return new WP_REST_Response( $cached );
    }

    // Quando o checkout pedir validação real, primeiro confirma se o CEP existe.
    // Se não existir, o frontend deve mostrar somente "CEP não encontrado" — nunca "Região não atendida".
    if ( $real_check && function_exists( 'sz_mb_cep_exists' ) ) {
        $cep_exists = sz_mb_cep_exists( $cep );
        if ( $cep_exists === false ) {
            $payload = [
                'ok'       => false,
                'zona_id'  => null,
                'code'     => 'cep_not_found',
                'tipo'     => 'cep_not_found',
                'mensagem' => 'CEP não encontrado.',
            ];
            set_transient( $cache_key, $payload, 30 * DAY_IN_SECONDS );
            return new WP_REST_Response( $payload );
        }
    }

    // Depois da existência real, confere a cobertura local.
    $zona = function_exists( 'sz_motoboy_resolver_zona' ) ? sz_motoboy_resolver_zona( $cep ) : null;
    if ( ! $zona ) {
        $payload = [ 'ok' => false, 'zona_id' => null, 'code' => 'region_not_covered', 'tipo' => 'region_not_covered', 'mensagem' => $block_msg ];
        set_transient( $cache_key, $payload, 6 * HOUR_IN_SECONDS );
        return new WP_REST_Response( $payload );
    }

    $dias = $zona['dias_funcionamento'] ?? '1,2,3,4,5,6';
    $cutoffs = $zona['cutoff_horarios'] ?? '';
    $payload = [
        'ok'                 => true,
        'zona_id'            => $zona['zona_id'],
        'zona_nome'          => $zona['zona_nome'] ?? '',
        'cd_id'              => $zona['cd_id'],
        'dias_funcionamento' => $dias,
        'dias_label'         => function_exists( 'sz_motoboy_zone_days_label' ) ? sz_motoboy_zone_days_label( $dias ) : '',
        'cutoff_horarios'    => function_exists( 'sz_motoboy_zone_cutoffs_array' ) ? sz_motoboy_zone_cutoffs_array( $cutoffs ) : [],
        'cutoff_label'       => function_exists( 'sz_motoboy_zone_cutoff_label' ) ? sz_motoboy_zone_cutoff_label( $cutoffs, $dias ) : '',
        'checkout_message'   => function_exists( 'sz_motoboy_zone_cutoff_commercial_message' ) ? sz_motoboy_zone_cutoff_commercial_message( $dias, $cutoffs, (string) ( $zona['zona_nome'] ?? '' ) ) : '',
        'next_dates'         => function_exists( 'sz_motoboy_zone_next_dates' ) ? sz_motoboy_zone_next_dates( $dias, 5, $cutoffs ) : [],
    ];
    set_transient( $cache_key, $payload, 12 * HOUR_IN_SECONDS );
    return new WP_REST_Response( $payload );
}

// ─── Endpoint: retorna URL de expedição dado token do link motoboy ───────────
function sz_mb_api_link_expedicao( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $token = sanitize_text_field( $req->get_param('sz') ?? '' );
    if ( ! $token ) return new WP_REST_Response( [ 'ok' => false ], 400 );

    $t  = $wpdb->prefix . 'senderzz_checkout_links';
    $mb = $wpdb->get_row( $wpdb->prepare(
        "SELECT * FROM {$t} WHERE slug = %s AND tipo = 'motoboy' LIMIT 1",
        $token
    ) );
    if ( ! $mb ) return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Link não encontrado.' ], 404 );

    $exp = $wpdb->get_row( $wpdb->prepare(
        "SELECT url, name FROM {$t} WHERE link_motoboy_id = %d AND tipo = 'correio' LIMIT 1",
        $mb->id
    ) );
    if ( ! $exp ) return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Link de expedição não encontrado.' ], 404 );

    return new WP_REST_Response( [
        'ok'   => true,
        'url'  => $exp->url,
        'nome' => $exp->name,
    ] );
}


function sz_rest_toggle_dispensar_cpf( WP_REST_Request $req ): WP_REST_Response {
    $value = (bool) $req->get_param('value');

    // Autenticar via cookie de sessão do portal (mesmo mecanismo do Portal_Auth)
    $auth_class = class_exists( '\\WC_MelhorEnvio\\Portal\\Portal_Auth' )
        ? '\\WC_MelhorEnvio\\Portal\\Portal_Auth'
        : null;

    $wp_user_id = 0;

    if ( $auth_class ) {
        // Usar Portal_Auth::get_current_user() que lê o cookie internamente
        $portal_user = $auth_class::get_current_user();
        if ( $portal_user && ! empty( $portal_user->wp_user_id ) ) {
            $wp_user_id = (int) $portal_user->wp_user_id;
        }
    }

    // Fallback: buscar diretamente via cookie
    if ( ! $wp_user_id ) {
        $cookie_name = 'senderzz_portal_session';
        $token_raw   = sanitize_text_field( wp_unslash( $_COOKIE[ $cookie_name ] ?? '' ) );
        if ( $token_raw ) {
            $token_hash = $auth_class ? $auth_class::hash_session_token( $token_raw ) : hash( 'sha256', $token_raw );
            global $wpdb;
            $table_sess = $wpdb->prefix . 'senderzz_portal_sessions';
            $table_user = $wpdb->prefix . 'senderzz_portal_users';
            $session = $wpdb->get_row( $wpdb->prepare(
                "SELECT s.user_id FROM {$table_sess} s WHERE s.token IN (%s,%s) AND s.expires_at > NOW() LIMIT 1",
                $token_raw, $token_hash
            ) );
            if ( $session ) {
                $u = $wpdb->get_row( $wpdb->prepare(
                    "SELECT wp_user_id FROM {$table_user} WHERE id=%d AND status='active' LIMIT 1",
                    $session->user_id
                ) );
                if ( $u ) $wp_user_id = (int) $u->wp_user_id;
            }
        }
    }

    // Último fallback: WP admin logado
    if ( ! $wp_user_id && is_user_logged_in() ) {
        $wp_user_id = get_current_user_id();
    }

    if ( ! $wp_user_id ) {
        return new WP_REST_Response( [ 'ok' => false, 'msg' => 'Sessão inválida.' ], 401 );
    }

    update_user_meta( $wp_user_id, SZ_META_DISPENSAR_CPF, $value ? '1' : '0' );
    wp_cache_delete( 'sz_mb_portal_uid_' . $wp_user_id, 'sz_motoboy' );

    return new WP_REST_Response( [ 'ok' => true, 'dispensar_cpf' => $value ] );
}


function sz_mb_api_alan_localizacao( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $p = $wpdb->prefix;

    $motoboys = $wpdb->get_results(
        "SELECT m.id, m.nome, z.nome AS zona, cd.nome AS cd,
                m.ultimo_lat, m.ultimo_lng,
                m.ultimo_ping,
                COUNT(mp.id) AS pedidos_abertos,
                SUM(CASE WHEN mp.status='entregue' THEN 1 ELSE 0 END) AS entregues_hoje
           FROM {$p}sz_motoboys m
           LEFT JOIN {$p}sz_motoboy_zonas z ON z.id = m.zona_id
           LEFT JOIN {$p}sz_motoboy_cds cd ON cd.id = m.cd_id
           LEFT JOIN {$p}sz_motoboy_pedidos mp
                  ON mp.motoboy_id = m.id AND DATE(mp.created_at) = ( SELECT DATE(CONVERT_TZ(NOW(), '+00:00', '-03:00')) )
          WHERE m.ativo = 1
          GROUP BY m.id"
    );

    return new WP_REST_Response( [ 'ok' => true, 'motoboys' => $motoboys ] );
}


function sz_mb_api_alan_historico( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $p          = $wpdb->prefix;
    $motoboy_id = (int) $req->get_param('motoboy_id');

    $historico = $wpdb->get_results( $wpdb->prepare(
        "SELECT data_fechamento,
                total_pedidos, total_entregues, total_frustrados,
                total_dinheiro, total_pix, total_cartao,
                total_a_repassar, repasse_confirmado, alan_confirmou
           FROM {$p}sz_motoboy_fechamento
          WHERE motoboy_id = %d
          ORDER BY data_fechamento DESC
          LIMIT 30",
        $motoboy_id
    ) );

    $motoboy = $wpdb->get_row( $wpdb->prepare(
        "SELECT m.nome, z.nome AS zona, cd.nome AS cd
           FROM {$p}sz_motoboys m
           LEFT JOIN {$p}sz_motoboy_zonas z ON z.id = m.zona_id
           LEFT JOIN {$p}sz_motoboy_cds cd ON cd.id = m.cd_id
          WHERE m.id = %d",
        $motoboy_id
    ) );

    return new WP_REST_Response( [ 'ok' => true, 'motoboy' => $motoboy, 'historico' => $historico ] );
}


function sz_mb_api_alan_etiquetas( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $p      = $wpdb->prefix;
    $data   = sanitize_text_field( $req->get_param('data') ?: ( new DateTimeImmutable( 'now', new DateTimeZone( 'America/Sao_Paulo' ) ) )->format( 'Y-m-d' ) );
    $status = sanitize_text_field( $req->get_param('status') ?: 'embalado' );

    $pedidos = $wpdb->get_results( $wpdb->prepare(
        "SELECT mp.id, mp.wc_order_id, mp.status,
                mp.dest_nome, mp.dest_telefone, mp.dest_cep,
                mp.dest_endereco, mp.dest_numero, mp.dest_complemento,
                mp.dest_bairro, mp.dest_cidade, mp.dest_uf,
                mp.dest_produto, mp.valor_pedido, mp.valor_taxa,
                COALESCE(
                    (SELECT meta_value FROM {$wpdb->prefix}wc_orders_meta
                     WHERE order_id = mp.wc_order_id AND meta_key = '_sz_delivery_date' LIMIT 1),
                    ''
                ) AS delivery_date,
                mp.pgto_dinheiro, mp.pgto_pix, mp.pgto_cartao,
                m.nome AS motoboy_nome, z.nome AS zona_nome, cd.nome AS cd_nome,
                mp.created_at
           FROM {$p}sz_motoboy_pedidos mp
           LEFT JOIN {$p}sz_motoboys m ON m.id = mp.motoboy_id
           LEFT JOIN {$p}sz_motoboy_zonas z ON z.id = mp.zona_id
           LEFT JOIN {$p}sz_motoboy_cds cd ON cd.id = mp.cd_id
          WHERE DATE(mp.created_at) = %s AND mp.status = %s
          ORDER BY mp.motoboy_id, mp.id",
        $data, $status
    ) );

    if ( $pedidos && function_exists( 'senderzz_clean_product_summary' ) ) {
        foreach ( $pedidos as $pedido ) {
            if ( isset( $pedido->dest_produto ) ) $pedido->dest_produto = senderzz_clean_product_summary( $pedido->dest_produto );
        }
    }
    return new WP_REST_Response( [ 'ok' => true, 'pedidos' => $pedidos, 'data' => $data ] );
}


function sz_mb_api_alan_push_subscribe( WP_REST_Request $req ): WP_REST_Response {
    $sub = $req->get_json_params();
    if ( empty($sub['endpoint']) ) {
        return new WP_REST_Response( [ 'ok' => false, 'erro' => 'Endpoint inválido.' ], 400 );
    }
    $subs   = get_option( 'sz_motoboy_admin_push_subs', [] );
    $key    = md5( $sub['endpoint'] );
    $subs[$key] = $sub;
    update_option( 'sz_motoboy_admin_push_subs', $subs );
    return new WP_REST_Response( [ 'ok' => true ] );
}

/**
 * Envia push para todos os admins/alan inscritos.
 */
function sz_mb_push_admin( string $title, string $body, array $data = [] ): void {
    $subs        = get_option( 'sz_motoboy_admin_push_subs', [] );
    $vapid_pub   = get_option( 'sz_notif_vapid_public', '' );
    $vapid_priv  = get_option( 'sz_notif_vapid_private', '' );
    if ( ! $subs || ! $vapid_pub ) return;

    $payload = wp_json_encode( array_merge( [ 'title' => $title, 'body' => $body ], $data ) );
    foreach ( $subs as $sub ) {
        do_action( 'sz_notif_push_send', $sub, $payload );
    }
}

// ─── Hook: notifica admins quando motoboy entrega ou frustra ─────────────────
add_action( 'sz_motoboy_status_changed', function( int $pedido_id, string $de, string $para, object $pedido ) {
    // Notifica operador em todos os eventos relevantes da fila
    if ( $para === 'agendado' || $para === 'aprovado' ) {
        $dest = $pedido->dest_nome ?? 'Cliente';
        $val  = isset( $pedido->valor_pedido ) ? 'R$ ' . number_format( (float) $pedido->valor_pedido, 2, ',', '.' ) : '';
        sz_mb_push_admin(
            '📦 Novo pedido COD',
            $dest . ( $val ? ' — ' . $val : '' ) . ' — #' . ( $pedido->wc_order_id ?? $pedido_id ),
            [ 'pedido_id' => $pedido_id, 'status' => $para, 'url' => home_url('/meus-pedidos/') ]
        );
        return;
    }
    if ( ! in_array( $para, [ 'entregue', 'frustrado' ], true ) ) return;

    global $wpdb;
    $mb = $wpdb->get_row( $wpdb->prepare(
        "SELECT nome FROM {$wpdb->prefix}sz_motoboys WHERE id = %d",
        $pedido->motoboy_id
    ) );

    $nome_mb = $mb->nome ?? 'Motoboy';
    $dest    = $pedido->dest_nome ?? '';

    if ( $para === 'entregue' ) {
        sz_mb_push_admin(
            '✅ Pedido entregue',
            "{$nome_mb} entregou para {$dest} — #{$pedido->wc_order_id}",
            [ 'pedido_id' => $pedido_id, 'status' => 'entregue' ]
        );
    } else {
        sz_mb_push_admin(
            '❌ Entrega frustrada',
            "{$nome_mb} não conseguiu entregar para {$dest} — #{$pedido->wc_order_id}",
            [ 'pedido_id' => $pedido_id, 'status' => 'frustrado' ]
        );
    }
}, 10, 4 );


function sz_mb_api_motoboy_push_subscribe( WP_REST_Request $req ): WP_REST_Response {
    $mb  = sz_mb_get_motoboy_by_token();
    if ( ! $mb ) return new WP_REST_Response( ['ok'=>false], 401 );
    $sub = $req->get_json_params();
    if ( empty($sub['endpoint']) ) return new WP_REST_Response( ['ok'=>false,'erro'=>'Endpoint inválido.'], 400 );
    update_user_meta( 0, 'sz_mb_push_' . $mb->id, wp_json_encode($sub) );
    // Usar opção por ser motoboy (não wp_user)
    $all = get_option('sz_motoboy_push_subs', []);
    $all[$mb->id] = $sub;
    update_option('sz_motoboy_push_subs', $all);
    return new WP_REST_Response( ['ok'=>true] );
}

/**
 * Envia push para um motoboy específico.
 */
function sz_mb_push_motoboy( int $motoboy_id, string $title, string $body ): void {
    $all = get_option('sz_motoboy_push_subs', []);
    if ( empty($all[$motoboy_id]) ) return;
    $payload = wp_json_encode(['title'=>$title,'body'=>$body]);
    do_action('sz_notif_push_send', $all[$motoboy_id], $payload);
}

// Notifica motoboy quando pedido é atribuído (embalado → pronto pra pegar)
add_action('sz_motoboy_status_changed', function(int $pedido_id, string $de, string $para, object $pedido) {
    if ($para !== 'embalado' || !$pedido->motoboy_id) return;
    sz_mb_push_motoboy(
        (int)$pedido->motoboy_id,
        '📦 Novo pedido no seu lote',
        'Pedido #'.$pedido->wc_order_id.' para '.$pedido->dest_nome.' está pronto para entrega.'
    );
}, 10, 4);


// ═══════════════════════════════════════════════════════════════════════════════
// SUPORTE / TICKETS — Portal Producer/Affiliate
// Namespace: senderzz/v1 (mesmo do portal principal)
// Autenticação: cookie senderzz_portal_session (Portal_Auth)
// ═══════════════════════════════════════════════════════════════════════════════

add_action( 'rest_api_init', function () {
    // Listar tickets do usuário
    register_rest_route( 'senderzz/v1', '/tickets', [
        'methods'             => 'GET',
        'callback'            => 'sz_ticket_api_list',
        'permission_callback' => '__return_true',
    ] );
    // Criar novo ticket
    register_rest_route( 'senderzz/v1', '/tickets', [
        'methods'             => 'POST',
        'callback'            => 'sz_ticket_api_create',
        'permission_callback' => '__return_true',
    ] );
    // Mensagens de um ticket
    register_rest_route( 'senderzz/v1', '/tickets/(?P<id>\d+)/msgs', [
        'methods'             => 'GET',
        'callback'            => 'sz_ticket_api_msgs',
        'permission_callback' => '__return_true',
    ] );
    // Enviar mensagem em ticket existente
    register_rest_route( 'senderzz/v1', '/tickets/(?P<id>\d+)/msgs', [
        'methods'             => 'POST',
        'callback'            => 'sz_ticket_api_send_msg',
        'permission_callback' => '__return_true',
    ] );
    // Fechar ticket (cliente fecha o próprio)
    register_rest_route( 'senderzz/v1', '/tickets/(?P<id>\d+)/fechar', [
        'methods'             => 'POST',
        'callback'            => 'sz_ticket_api_close',
        'permission_callback' => '__return_true',
    ] );
} );

/**
 * Autentica usuário do portal via cookie de sessão.
 * Retorna objeto do portal_user ou WP_Error.
 */
function sz_ticket_auth_portal_user() {
    $auth_class = class_exists( '\\WC_MelhorEnvio\\Portal\\Portal_Auth' )
        ? '\\WC_MelhorEnvio\\Portal\\Portal_Auth'
        : null;

    if ( $auth_class ) {
        $user = $auth_class::get_current_user();
        if ( $user && ! empty( $user->id ) ) return $user;
    }

    return new WP_Error( 'unauthorized', 'Sessão inválida.', [ 'status' => 401 ] );
}

function sz_ticket_api_list( WP_REST_Request $req ): WP_REST_Response {
    $user = sz_ticket_auth_portal_user();
    if ( is_wp_error( $user ) ) return new WP_REST_Response( [ 'ok' => false, 'msg' => $user->get_error_message() ], 401 );

    global $wpdb;
    $p  = $wpdb->prefix;
    $uid = (int) $user->id;

    $tickets = $wpdb->get_results( $wpdb->prepare(
        "SELECT t.id, t.assunto, t.categoria, t.status, t.prioridade, t.created_at, t.updated_at,
                (SELECT COUNT(*) FROM {$p}sz_portal_ticket_msgs m WHERE m.ticket_id = t.id) AS total_msgs,
                (SELECT m2.created_at FROM {$p}sz_portal_ticket_msgs m2 WHERE m2.ticket_id = t.id ORDER BY m2.id DESC LIMIT 1) AS ultima_msg_at,
                (SELECT m3.autor_tipo FROM {$p}sz_portal_ticket_msgs m3 WHERE m3.ticket_id = t.id ORDER BY m3.id DESC LIMIT 1) AS ultima_msg_autor
         FROM {$p}sz_portal_tickets t
         WHERE t.portal_user_id = %d
         ORDER BY t.updated_at DESC
         LIMIT 50",
        $uid
    ), ARRAY_A );

    return new WP_REST_Response( [ 'ok' => true, 'tickets' => $tickets ?: [] ] );
}

function sz_ticket_api_create( WP_REST_Request $req ): WP_REST_Response {
    $user = sz_ticket_auth_portal_user();
    if ( is_wp_error( $user ) ) return new WP_REST_Response( [ 'ok' => false, 'msg' => $user->get_error_message() ], 401 );

    $assunto    = sanitize_text_field( $req->get_param( 'assunto' ) ?? '' );
    $categoria  = sanitize_key( $req->get_param( 'categoria' ) ?: 'outro' );
    $mensagem   = sanitize_textarea_field( $req->get_param( 'mensagem' ) ?? '' );

    if ( strlen( $assunto ) < 5 )  return new WP_REST_Response( [ 'ok' => false, 'msg' => 'Assunto muito curto (mín. 5 caracteres).' ], 400 );
    if ( strlen( $mensagem ) < 10 ) return new WP_REST_Response( [ 'ok' => false, 'msg' => 'Mensagem muito curta (mín. 10 caracteres).' ], 400 );
    if ( ! in_array( $categoria, [ 'financeiro', 'pedido', 'tecnico', 'outro' ], true ) ) $categoria = 'outro';

    global $wpdb;
    $p = $wpdb->prefix;

    $wpdb->insert( "{$p}sz_portal_tickets", [
        'portal_user_id' => (int) $user->id,
        'assunto'        => $assunto,
        'categoria'      => $categoria,
        'status'         => 'aberto',
        'prioridade'     => 'normal',
    ] );
    $ticket_id = (int) $wpdb->insert_id;
    if ( ! $ticket_id ) return new WP_REST_Response( [ 'ok' => false, 'msg' => 'Erro ao criar ticket.' ], 500 );

    $wpdb->insert( "{$p}sz_portal_ticket_msgs", [
        'ticket_id'  => $ticket_id,
        'autor_tipo' => 'cliente',
        'autor_id'   => (int) $user->id,
        'autor_nome' => sanitize_text_field( $user->name ?? 'Cliente' ),
        'mensagem'   => $mensagem,
    ] );

    return new WP_REST_Response( [ 'ok' => true, 'ticket_id' => $ticket_id ] );
}

function sz_ticket_api_msgs( WP_REST_Request $req ): WP_REST_Response {
    $user = sz_ticket_auth_portal_user();
    if ( is_wp_error( $user ) ) return new WP_REST_Response( [ 'ok' => false, 'msg' => $user->get_error_message() ], 401 );

    global $wpdb;
    $p         = $wpdb->prefix;
    $ticket_id = (int) $req->get_param( 'id' );
    $uid       = (int) $user->id;

    $ticket = $wpdb->get_row( $wpdb->prepare(
        "SELECT * FROM {$p}sz_portal_tickets WHERE id=%d AND portal_user_id=%d LIMIT 1",
        $ticket_id, $uid
    ), ARRAY_A );
    if ( ! $ticket ) return new WP_REST_Response( [ 'ok' => false, 'msg' => 'Ticket não encontrado.' ], 404 );

    $msgs = $wpdb->get_results( $wpdb->prepare(
        "SELECT id, autor_tipo, autor_nome, mensagem, created_at
         FROM {$p}sz_portal_ticket_msgs WHERE ticket_id=%d ORDER BY id ASC LIMIT 100",
        $ticket_id
    ), ARRAY_A );

    return new WP_REST_Response( [ 'ok' => true, 'ticket' => $ticket, 'msgs' => $msgs ?: [] ] );
}

function sz_ticket_api_send_msg( WP_REST_Request $req ): WP_REST_Response {
    $user = sz_ticket_auth_portal_user();
    if ( is_wp_error( $user ) ) return new WP_REST_Response( [ 'ok' => false, 'msg' => $user->get_error_message() ], 401 );

    global $wpdb;
    $p         = $wpdb->prefix;
    $ticket_id = (int) $req->get_param( 'id' );
    $uid       = (int) $user->id;
    $mensagem  = sanitize_textarea_field( $req->get_param( 'mensagem' ) ?? '' );

    if ( strlen( $mensagem ) < 3 ) return new WP_REST_Response( [ 'ok' => false, 'msg' => 'Mensagem vazia.' ], 400 );

    $ticket = $wpdb->get_row( $wpdb->prepare(
        "SELECT * FROM {$p}sz_portal_tickets WHERE id=%d AND portal_user_id=%d AND status != 'fechado' LIMIT 1",
        $ticket_id, $uid
    ) );
    if ( ! $ticket ) return new WP_REST_Response( [ 'ok' => false, 'msg' => 'Ticket não encontrado ou já fechado.' ], 404 );

    $wpdb->insert( "{$p}sz_portal_ticket_msgs", [
        'ticket_id'  => $ticket_id,
        'autor_tipo' => 'cliente',
        'autor_id'   => $uid,
        'autor_nome' => sanitize_text_field( $user->name ?? 'Cliente' ),
        'mensagem'   => $mensagem,
    ] );

    // Reabre se estava respondido
    if ( $ticket->status === 'respondido' ) {
        $wpdb->update( "{$p}sz_portal_tickets", [ 'status' => 'em_analise' ], [ 'id' => $ticket_id ] );
    }

    return new WP_REST_Response( [ 'ok' => true ] );
}

function sz_ticket_api_close( WP_REST_Request $req ): WP_REST_Response {
    $user = sz_ticket_auth_portal_user();
    if ( is_wp_error( $user ) ) return new WP_REST_Response( [ 'ok' => false, 'msg' => $user->get_error_message() ], 401 );

    global $wpdb;
    $p         = $wpdb->prefix;
    $ticket_id = (int) $req->get_param( 'id' );
    $uid       = (int) $user->id;

    $ticket = $wpdb->get_row( $wpdb->prepare(
        "SELECT id FROM {$p}sz_portal_tickets WHERE id=%d AND portal_user_id=%d AND status != 'fechado' LIMIT 1",
        $ticket_id, $uid
    ) );
    if ( ! $ticket ) return new WP_REST_Response( [ 'ok' => false, 'msg' => 'Ticket não encontrado ou já fechado.' ], 404 );

    $wpdb->update( "{$p}sz_portal_tickets", [
        'status'     => 'fechado',
        'fechado_at' => sz_motoboy_now_mysql(),
    ], [ 'id' => $ticket_id ] );

    return new WP_REST_Response( [ 'ok' => true ] );
}
