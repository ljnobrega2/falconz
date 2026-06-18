<?php
/**
 * Senderzz Logistics — HPOS Fix v2
 *
 * Corrige bugs do plugin original em ambientes HPOS (High-Performance Order
 * Storage) e blindando a rota /rastreio/{code} contra interpretação
 * automática de URL pelo WordPress (redirect_canonical / fuzzy match).
 *
 * v2 (vs v1):
 *   - Registra /rastreio/{code} como rewrite rule oficial do WP em vez de
 *     interceptar via template_redirect. Garante prioridade absoluta sobre
 *     interpretação de IDs numéricos pelo WP. Codes só-numéricos agora
 *     funcionam (v1 caía em redirect canônico).
 *   - Auto-flush de rewrite rules apenas uma vez (não a cada request).
 *
 * BUGS CORRIGIDOS:
 *
 * 1. Tracking code descartado quando status != posted/delivered
 *    Queue.php:150 só persiste em 'posted'/'delivered'. ME devolve 'tracking'
 *    em qualquer status. FIX: hook em http_response.
 *
 * 2. Página /rastreio/{code} não acha pedido em HPOS
 *    Portal_Page::fetch_tracking_events:1058 consulta wp_postmeta (vazio em
 *    HPOS). FIX: rewrite rule + handler HPOS-aware.
 *
 * 3. Webhook ME não acha pedido em HPOS
 *    Tracking_Webhook::find_order_by_payload:154 consulta wp_postmeta. FIX:
 *    intercepta em rest_pre_dispatch e processa HPOS-aware.
 *
 * APLICAR:
 *   1. Sobe pra includes/senderzz-hpos-fix.php (substitui versão antiga se houver)
 *   2. Já tem require? Se não, adiciona em senderzz-logistics.php (depois do
 *      tier3-batch):
 *
 *      'includes/senderzz-hpos-fix.php',
 *
 *   3. NA PRIMEIRA VISITA após aplicar, o patch detecta que falta a regra de
 *      rewrite e força flush automático. Não precisa mexer em "Permalinks".
 *
 * REVERSÃO:
 *   - Comenta o require.
 *   - Vai em wp-admin → Configurações → Links Permanentes → Salvar
 *     (limpa as rewrite rules antigas).
 */

defined( 'ABSPATH' ) || exit;
if ( defined( 'SENDERZZ_HPOS_FIX_LOADED' ) ) return;
define( 'SENDERZZ_HPOS_FIX_LOADED', true );
define( 'SENDERZZ_HPOS_FIX_VERSION', '2.0' );

// ═════════════════════════════════════════════════════════════════════════════
// HELPERS HPOS-aware
// ═════════════════════════════════════════════════════════════════════════════

function senderzz_hpos_find_order_by_meta( string $meta_key, string $meta_value ): ?\WC_Order {
    if ( ! function_exists( 'wc_get_orders' ) ) return null;
    $ids = wc_get_orders( [
        'meta_key'   => $meta_key,
        'meta_value' => $meta_value,
        'limit'      => 1,
        'return'     => 'ids',
    ] );
    if ( empty( $ids ) ) return null;
    $order = wc_get_order( (int) $ids[0] );
    return $order ?: null;
}

function senderzz_hpos_find_order_by_meta_like( array $meta_keys, string $like_value ): ?\WC_Order {
    global $wpdb;
    $hpos_table = $wpdb->prefix . 'wc_orders_meta';
    $hpos_exists = $wpdb->get_var( "SHOW TABLES LIKE '$hpos_table'" );
    $placeholders = implode( ',', array_fill( 0, count( $meta_keys ), '%s' ) );
    $like = '%' . $wpdb->esc_like( $like_value ) . '%';

    if ( $hpos_exists ) {
        $args = array_merge( $meta_keys, [ $like ] );
        $order_id = $wpdb->get_var( $wpdb->prepare(
            "SELECT order_id FROM $hpos_table
             WHERE meta_key IN ($placeholders) AND meta_value LIKE %s LIMIT 1",
            ...$args
        ) );
        if ( $order_id ) {
            $order = wc_get_order( (int) $order_id );
            if ( $order ) return $order;
        }
    }

    $args = array_merge( $meta_keys, [ $like ] );
    $order_id = $wpdb->get_var( $wpdb->prepare(
        "SELECT post_id FROM {$wpdb->postmeta}
         WHERE meta_key IN ($placeholders) AND meta_value LIKE %s LIMIT 1",
        ...$args
    ) );

    if ( $order_id ) {
        $order = wc_get_order( (int) $order_id );
        if ( $order ) return $order;
    }
    return null;
}

// ═════════════════════════════════════════════════════════════════════════════
// FIX 1: persistir tracking code em qualquer status
// ═════════════════════════════════════════════════════════════════════════════

add_filter( 'http_response', function ( $response, $args, $url ) {
    if ( ! is_string( $url ) ) return $response;
    if ( strpos( $url, '/api/v2/me/shipment/tracking' ) === false ) return $response;
    if ( is_wp_error( $response ) ) return $response;
    if ( wp_remote_retrieve_response_code( $response ) !== 200 ) return $response;

    $body = wp_remote_retrieve_body( $response );
    $data = json_decode( $body, true );
    if ( ! is_array( $data ) ) return $response;

    foreach ( $data as $me_id => $item ) {
        if ( ! is_array( $item ) ) continue;
        $tracking = (string) ( $item['tracking'] ?? '' );
        if ( $tracking === '' ) continue;

        $order = senderzz_hpos_find_order_by_meta( '_melhor_envio_order_id', (string) $me_id );
        if ( ! $order ) continue;

        $current_single = (string) $order->get_meta( '_melhor_envio_tracking' );
        $codes = $order->get_meta( '_melhor_envio_tracking_codes' );
        if ( ! is_array( $codes ) ) $codes = $codes ? [ $codes ] : [];

        $changed = false;
        if ( $current_single !== $tracking ) {
            $order->update_meta_data( '_melhor_envio_tracking', $tracking );
            $changed = true;
        }
        if ( ! in_array( $tracking, $codes, true ) ) {
            $codes[] = $tracking;
            $order->update_meta_data( '_melhor_envio_tracking_codes', $codes );
            $changed = true;
        }

        if ( $changed ) {
            $order->save();
            if ( function_exists( 'tpc_log' ) ) {
                tpc_log( 'hpos_tracking_persisted', [
                    'order_id' => $order->get_id(),
                    'me_id'    => $me_id,
                    'tracking' => $tracking,
                    'status'   => (string) ( $item['status'] ?? '' ),
                ] );
            }
        }
    }
    return $response;
}, 10, 3 );

// ═════════════════════════════════════════════════════════════════════════════
// FIX 2: rewrite rule oficial pra /rastreio/{code}
// ═════════════════════════════════════════════════════════════════════════════

/**
 * Registra a rewrite rule. Prioridade 'top' garante que vem ANTES das regras
 * automáticas do WP (que tratariam URLs numéricas como ID).
 */
add_action( 'init', function () {
    add_rewrite_rule(
        '^rastreio/([^/]+)/?$',
        'index.php?senderzz_track_code=$matches[1]',
        'top'
    );

    // Auto-flush apenas uma vez por versão. Persiste flag em options.
    $flushed_version = get_option( 'senderzz_hpos_rewrite_flushed', '' );
    if ( $flushed_version !== SENDERZZ_HPOS_FIX_VERSION ) {
        flush_rewrite_rules( false );
        update_option( 'senderzz_hpos_rewrite_flushed', SENDERZZ_HPOS_FIX_VERSION );
    }
}, 1 );

/**
 * Registra o query var pra que o WP entenda ?senderzz_track_code=
 */
add_filter( 'query_vars', function ( $vars ) {
    $vars[] = 'senderzz_track_code';
    return $vars;
} );

/**
 * Detecta a query var preenchida e dispara o render. Roda em 'parse_request'
 * (mais cedo que template_redirect) — antes de QUALQUER outra interpretação.
 */
add_action( 'parse_request', function ( $wp ) {
    if ( empty( $wp->query_vars['senderzz_track_code'] ) ) return;

    $code = strtoupper( sanitize_text_field( $wp->query_vars['senderzz_track_code'] ) );

    // Bloqueia que outros handlers continuem processando essa request.
    $wp->matched_rule  = 'rastreio';
    $wp->matched_query = '';
    $wp->query_vars    = [ 'senderzz_track_code' => $code ];
    $wp->request       = 'rastreio/' . $code;

    senderzz_hpos_handle_tracking_render( $code );
    exit;
}, 1 );

function senderzz_hpos_handle_tracking_render( string $code ): void {
    // Tenta achar pedido por código exato em _melhor_envio_tracking
    $order = senderzz_hpos_find_order_by_meta( '_melhor_envio_tracking', $code );

    // Fallback: busca em _melhor_envio_tracking_codes (array serializado)
    if ( ! $order ) {
        $order = senderzz_hpos_find_order_by_meta_like(
            [ '_melhor_envio_tracking', '_melhor_envio_tracking_codes' ],
            $code
        );
    }

    $events = [];
    if ( $order ) {
        $me_order_id = (string) $order->get_meta( '_melhor_envio_order_id' );
        if ( $me_order_id ) {
            $events = senderzz_hpos_fetch_tracking_events( $me_order_id );
        }
    }

    // Carrega marca da classe de entrega do pedido
    $brand = function_exists( 'senderzz_tb_get_for_order' )
        ? senderzz_tb_get_for_order( $order )
        : [ 'logo' => '', 'cor' => '#E8650A', 'cor_texto' => '#ffffff', 'nome' => 'Senderzz', 'rodape' => '' ];

    senderzz_hpos_render_tracking_page( $code, $events, $brand );
}

function senderzz_hpos_fetch_tracking_events( string $me_order_id ): array {
    $token = get_option( 'tpc_me_token', '' );
    if ( ! $token ) return [];

    $api_base = defined( 'TPC_ME_API' ) ? constant( 'TPC_ME_API' ) : 'https://www.melhorenvio.com.br/api/v2';

    $response = wp_remote_get(
        $api_base . '/me/shipment/tracking?orders=' . urlencode( $me_order_id ),
        [
            'headers' => [
                'Authorization' => 'Bearer ' . $token,
                'Accept'        => 'application/json',
                'User-Agent'    => 'Senderzz/HPOS-fix',
            ],
            'timeout' => 10,
        ]
    );

    if ( is_wp_error( $response ) || wp_remote_retrieve_response_code( $response ) !== 200 ) return [];

    $body     = json_decode( wp_remote_retrieve_body( $response ), true );
    $shipment = $body[ $me_order_id ] ?? ( isset( $body[0] ) ? $body[0] : [] );
    $items    = $shipment['tracking']['events'] ?? $shipment['events'] ?? [];
    if ( ! is_array( $items ) ) return [];

    $events = [];
    foreach ( $items as $ev ) {
        $events[] = [
            'date'        => (string) ( $ev['happened_at'] ?? $ev['data'] ?? '' ),
            'description' => (string) ( $ev['description'] ?? $ev['descricao'] ?? '' ),
            'location'    => trim( (string) ( $ev['city'] ?? '' ) . ( isset( $ev['state'] ) ? '/' . $ev['state'] : '' ) ),
        ];
    }
    usort( $events, fn( $a, $b ) => strcmp( $b['date'], $a['date'] ) );
    return $events;
}

function senderzz_hpos_render_tracking_page( string $code, array $events, array $brand = [] ): void {
    $brand = array_merge( [
        'logo'      => '',
        'cor'       => '#E8650A',
        'cor_texto' => '#ffffff',
        'nome'      => 'Senderzz',
        'rodape'    => '',
    ], $brand );

    if ( ! $brand['logo'] ) {
        if ( class_exists( '\WC_Melhor_Envio' ) && method_exists( '\WC_Melhor_Envio', 'plugin_url' ) ) {
            $brand['logo'] = esc_url( \WC_Melhor_Envio::plugin_url() . '/assets/images/senderzz-logo.png' );
        }
    }

    $cor       = esc_attr( $brand['cor'] );
    $cor_texto = esc_attr( $brand['cor_texto'] );
    $nome      = esc_html( $brand['nome'] ?: 'Senderzz' );
    $logo      = esc_url( $brand['logo'] );
    $rodape    = esc_html( $brand['rodape'] );

    nocache_headers();
    header( 'Content-Type: text/html; charset=utf-8' );
    ?><!DOCTYPE html>
<html lang="pt-BR">
<head>
<meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Rastreio <?php echo esc_html( $code ); ?> — <?php echo $nome; ?></title>
<style>
:root{--brand:<?php echo $cor; ?>;--brand-text:<?php echo $cor_texto; ?>}
*{box-sizing:border-box;margin:0;padding:0}
body{background:#f5f5f7;color:#111827;font-family:var(--sz-font);min-height:100vh;display:flex;flex-direction:column}
.top-bar{background:var(--brand);padding:14px 20px;display:flex;align-items:center;gap:12px}
.top-bar img{height:36px;max-width:150px;object-fit:contain}
.brand-name{color:var(--brand-text);font-size:var(--sz-text-lg);font-weight:700;letter-spacing:-.015em}
.main{flex:1;padding:28px 16px}
.container{max-width:620px;margin:0 auto}
.card{background:#fff;border-radius:16px;border:1px solid #e5e7eb;padding:26px;box-shadow:0 4px 20px rgba(0,0,0,.06);margin-bottom:16px}
.card-label{font-size:var(--sz-text-sm);font-weight:700;text-transform:none;letter-spacing:.02em;color:#9ca3af;margin-bottom:6px}
.code{font-family:var(--sz-font);font-size:var(--sz-text-xl);font-weight:700;letter-spacing:.02em;color:#111827;margin-bottom:20px}
.status-badge{display:inline-flex;align-items:center;gap:6px;background:color-mix(in srgb,var(--brand) 12%,#fff);color:var(--brand);font-size:var(--sz-text-meta);font-weight:700;padding:5px 12px;border-radius:99px;margin-bottom:20px;border:1px solid color-mix(in srgb,var(--brand) 25%,transparent)}
.status-dot{width:8px;height:8px;border-radius:50%;background:var(--brand)}
.timeline{position:relative;padding-left:26px}
.timeline::before{content:"";position:absolute;left:6px;top:8px;bottom:8px;width:2px;background:#f3f4f6}
.ev{position:relative;margin-bottom:22px}
.ev:last-child{margin-bottom:0}
.ev-dot{position:absolute;left:-26px;top:3px;width:14px;height:14px;border-radius:50%;background:#fff;border:2px solid #e5e7eb}
.ev-dot.latest{background:var(--brand);border-color:var(--brand);box-shadow:0 0 0 4px color-mix(in srgb,var(--brand) 18%,transparent)}
.ev-desc{font-size:var(--sz-text-base);font-weight:600;color:#111827;line-height:1.4;margin-bottom:3px}
.ev-meta{font-size:var(--sz-text-sm);color:#9ca3af;display:flex;gap:10px;flex-wrap:wrap}
.empty{text-align:center;padding:40px 0}
.empty-ico{font-size:var(--sz-text-hero);margin-bottom:12px}
.empty-title{font-size:var(--sz-text-lg);font-weight:700;color:#6b7280;margin-bottom:6px}
.empty-sub{font-size:var(--sz-text-meta);color:#9ca3af;line-height:1.7;max-width:300px;margin:0 auto}
.footer{padding:16px 20px;text-align:center;font-size:var(--sz-text-meta);color:#9ca3af;border-top:1px solid #f3f4f6;background:#fff}
.footer a{color:var(--brand);text-decoration:none}
@media(prefers-color-scheme:dark){
    body{background:#0f172a;color:#f1f5f9}
    .card{background:#1e293b;border-color:#334155}
    .code,.ev-desc{color:#f1f5f9}
    .timeline::before,.ev-dot{background:#1e293b;border-color:#334155}
    .footer{background:#1e293b;border-color:#334155}
}
</style>
</head>
<body>
<div class="top-bar">
    <?php if ( $logo ) : ?><img src="<?php echo $logo; ?>" alt="<?php echo $nome; ?>"><?php else : ?><span class="brand-name"><?php echo $nome; ?></span><?php endif; ?>
</div>
<div class="main"><div class="container">
    <div class="card">
        <div class="card-label">Código de rastreio</div>
        <div class="code"><?php echo esc_html( $code ); ?></div>
        <?php if ( $events ) :
            $latest = $events[0]; ?>
        <div class="status-badge"><span class="status-dot"></span><?php echo esc_html( $latest['description'] ?? 'Em trânsito' ); ?></div>
        <div class="timeline">
            <?php foreach ( $events as $i => $ev ) :
                $date_fmt = '';
                try { $dt = new \DateTime( $ev['date'] ); $date_fmt = $dt->format( 'd/m/Y \à\s H:i' ); } catch ( \Exception $e ) { $date_fmt = $ev['date']; }
            ?>
            <div class="ev">
                <div class="ev-dot <?php echo $i === 0 ? 'latest' : ''; ?>"></div>
                <div class="ev-desc"><?php echo esc_html( $ev['description'] ); ?></div>
                <div class="ev-meta">
                    <?php if ( $date_fmt ) echo '<span>' . esc_html( $date_fmt ) . '</span>'; ?>
                    <?php if ( ! empty( $ev['location'] ) ) echo '<span>📍 ' . esc_html( $ev['location'] ) . '</span>'; ?>
                </div>
            </div>
            <?php endforeach; ?>
        </div>
        <?php else : ?>
        <div class="empty">
            <div class="empty-ico">📦</div>
            <div class="empty-title">Aguardando movimentação</div>
            <div class="empty-sub">O objeto foi registrado e em breve aparecerão atualizações aqui.</div>
        </div>
        <?php endif; ?>
    </div>
</div></div>
<div class="footer">
    <?php if ( $rodape ) : ?><?php echo $rodape; ?><?php else : ?>Rastreamento por <a href="https://senderzz.com.br" target="_blank">Senderzz</a><?php endif; ?>
</div>
</body></html><?php
}


// ═════════════════════════════════════════════════════════════════════════════
// FIX 3: webhook ME HPOS-aware
// ═════════════════════════════════════════════════════════════════════════════

add_filter( 'rest_pre_dispatch', function ( $result, $server, $request ) {
    if ( $result !== null ) return $result;
    if ( $request->get_route() !== '/senderzz/v1/webhook/me' ) return $result;
    if ( $request->get_method() !== 'POST' ) return $result;

    $payload = $request->get_json_params();
    if ( ! is_array( $payload ) ) return $result;
    $data = $payload['data'] ?? [];
    if ( ! is_array( $data ) ) return $result;

    $me_id    = (string) ( $data['id'] ?? $data['order_id'] ?? $data['shipment_id'] ?? '' );
    $protocol = (string) ( $data['protocol'] ?? '' );
    if ( $me_id === '' && $protocol === '' ) return $result;

    $order = null;
    if ( $me_id !== '' ) {
        $order = senderzz_hpos_find_order_by_meta( '_melhor_envio_order_id', $me_id );
    }
    if ( ! $order && $protocol !== '' ) {
        $order = senderzz_hpos_find_order_by_meta( '_melhor_envio_order_id', $protocol );
    }
    if ( ! $order ) return $result;

    $tracking = (string) ( $data['tracking'] ?? $data['codigo'] ?? '' );
    if ( $tracking ) {
        $current = (string) $order->get_meta( '_melhor_envio_tracking' );
        if ( $current !== $tracking ) {
            $order->update_meta_data( '_melhor_envio_tracking', $tracking );
            $codes = $order->get_meta( '_melhor_envio_tracking_codes' );
            if ( ! is_array( $codes ) ) $codes = $codes ? [ $codes ] : [];
            if ( ! in_array( $tracking, $codes, true ) ) {
                $codes[] = $tracking;
                $order->update_meta_data( '_melhor_envio_tracking_codes', $codes );
            }
        }
    }

    $me_status = strtolower( (string) ( $data['status'] ?? $data['situacao'] ?? '' ) );
    $status_map = [
        'posted'      => 'wc-enviado',
        'in_transit'  => 'wc-acaminho',
        'delivered'   => 'wc-entregue',
        'collecting'  => 'wc-emretirada',
        'collected'   => 'wc-coletado',
        'lost'        => 'wc-extravio',
        'canceled'    => 'wc-cancelled',
        'postado'     => 'wc-enviado',
        'em_transito' => 'wc-acaminho',
        'entregue'    => 'wc-entregue',
    ];
    $new_status = $status_map[ $me_status ] ?? '';

    if ( $new_status ) {
        $order->update_status( $new_status, 'Rastreio ME (HPOS-fix): ' . ( $data['status'] ?? '' ) );
    }

    $order->update_meta_data( '_senderzz_last_tracking_event', wp_json_encode( $payload, JSON_UNESCAPED_UNICODE ) );
    $order->update_meta_data( '_senderzz_last_seen_at', current_time( 'mysql' ) );
    $order->save();

    if ( function_exists( 'tpc_log' ) ) {
        tpc_log( 'hpos_webhook_me_processed', [
            'order_id'   => $order->get_id(),
            'me_id'      => $me_id,
            'me_status'  => $me_status,
            'new_status' => $new_status,
            'tracking'   => $tracking,
        ] );
    }

    return new \WP_REST_Response( [ 'ok' => true, 'via' => 'hpos_fix' ], 200 );
}, 20, 3 );

// ═════════════════════════════════════════════════════════════════════════════
// FIX 3: Garante que wfacp_checkout (FunnelKit) tem rewrite rules ativas
// Se o FunnelKit não registrou as rewrite rules, força flush automático
// ═════════════════════════════════════════════════════════════════════════════
add_action( 'init', function() {
    // Só age se o FunnelKit estiver ativo
    if ( ! post_type_exists( 'wfacp_checkout' ) ) return;

    $pt = get_post_type_object( 'wfacp_checkout' );

    // Garante publicly_queryable
    if ( $pt && ! $pt->publicly_queryable ) {
        $pt->publicly_queryable = true;
        $pt->public             = true;
    }

    // Verifica se a rewrite rule de /checkouts/ existe
    $rules = get_option( 'rewrite_rules', [] );
    $has_rule = false;
    if ( is_array( $rules ) ) {
        foreach ( array_keys( $rules ) as $pattern ) {
            if ( strpos( $pattern, 'checkouts' ) !== false && strpos( $pattern, 'wfacp_checkout' ) !== false ) {
                $has_rule = true;
                break;
            }
        }
        // Fallback: any rule matching checkouts/
        if ( ! $has_rule ) {
            foreach ( array_keys( $rules ) as $pattern ) {
                if ( strpos( $pattern, 'checkouts' ) !== false ) {
                    $has_rule = true;
                    break;
                }
            }
        }
    }

    // Se não há rewrite rule para /checkouts/ força flush (uma vez)
    $flush_key = 'sz_wfacp_rewrite_flushed_v2';
    if ( ! $has_rule && ! get_transient( $flush_key ) ) {
        set_transient( $flush_key, 1, HOUR_IN_SECONDS );
        flush_rewrite_rules( false );
    }
}, 99 ); // Prioridade 99 — depois do FunnelKit registrar seus post types (priority ~10)

// Fallback direto: se a URL é /checkouts/{slug}/ e o WP retornou 404,
// tenta resolver manualmente pelo slug do post wfacp_checkout
add_action( 'wp', function() {
    if ( ! is_404() ) return;

    $path = trim( parse_url( $_SERVER['REQUEST_URI'] ?? '', PHP_URL_PATH ) ?? '', '/' );

    // Verifica se é /checkouts/{slug}
    if ( ! preg_match( '#^checkouts/([^/]+)/?$#', $path, $m ) ) return;
    $slug = sanitize_title( $m[1] );
    if ( ! $slug ) return;
    if ( ! post_type_exists( 'wfacp_checkout' ) ) return;

    $post = get_page_by_path( $slug, OBJECT, 'wfacp_checkout' );
    if ( ! $post || $post->post_status !== 'publish' ) return;

    // Post existe — redireciona para o permalink canônico para forçar o FunnelKit
    $permalink = get_permalink( $post->ID );
    if ( ! $permalink ) return;

    // Preserva query string (?sz=token&m=1 etc.)
    $qs = $_SERVER['QUERY_STRING'] ?? '';
    if ( $qs ) $permalink = $permalink . ( strpos( $permalink, '?' ) !== false ? '&' : '?' ) . $qs;

    // Seta o post globalmente para o FunnelKit/WP processar
    global $wp_query;
    $wp_query->is_404       = false;
    $wp_query->is_singular  = true;
    $wp_query->is_single    = false;
    $wp_query->is_page      = false;
    $wp_query->queried_object      = $post;
    $wp_query->queried_object_id   = $post->ID;
    $wp_query->post                = $post;
    $wp_query->posts               = [ $post ];
    $wp_query->post_count          = 1;
    $wp_query->found_posts         = 1;
    $wp_query->query_vars['post_type'] = 'wfacp_checkout';
    $wp_query->query_vars['name']      = $slug;
    $wp_query->query_vars['wfacp_checkout'] = $slug;

    setup_postdata( $post );
    status_header( 200 );
}, 1 );
