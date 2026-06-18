<?php
/**
 * Senderzz — App PWA v214
 *
 * Rota dedicada /app/ para o PWA do produtor.
 * COMPLETAMENTE SEPARADO de /produtor-app/ — não mexe em nada existente.
 *
 * Diferença chave vs /produtor-app/:
 *   - Usa parse_request early hook para sair ANTES do tema/WooCommerce carregarem
 *   - HTML estático de ~3KB — sem wp_head, sem wp_footer, sem plugins de loja
 *   - SW com scope /app/ isolado — não interfere com /meus-pedidos/
 *   - Reutiliza 100% os AJAX handlers de sz_producer_pwa e REST de sz-notif/v1
 */
defined( 'ABSPATH' ) || exit;
if ( defined( 'SENDERZZ_APP_PWA_LOADED' ) ) return;
define( 'SENDERZZ_APP_PWA_LOADED', true );

/* ── 1. Registrar rota /app/ ───────────────────────────────────────────── */
add_action( 'init', function (): void {
    add_rewrite_rule( '^app/?$', 'index.php?sz_app_pwa=1', 'top' );
    add_rewrite_tag( '%sz_app_pwa%', '([0-9]+)' );
} );

/* ── 2. Servir o app shell CEDO — antes do tema/WooCommerce ─────────────── */
add_action( 'parse_request', function ( WP $wp ): void {
    if ( empty( $wp->query_vars['sz_app_pwa'] ) ) return;

    /* Ainda precisamos de auth e options — encerramos depois deles.
       O hook parse_request já rodou wp_loaded, options e auth estão ok.
       WooCommerce, tema e plugins de loja NÃO foram inicializados ainda. */
    header( 'Content-Type: text/html; charset=utf-8' );
    header( 'Cache-Control: no-store' );
    header( 'X-Content-Type-Options: nosniff' );

    include defined( 'TPC_PATH' )
        ? TPC_PATH . 'templates/portal/app-pwa.php'
        : plugin_dir_path( dirname( __FILE__ ) ) . 'templates/portal/app-pwa.php';
    exit;
}, 1 ); // priority 1 = antes de qualquer outro parse_request

/* ── 3. Service worker da rota /app/ (arquivo estático gerado pelo PHP) ── */
add_action( 'init', function (): void {
    add_rewrite_rule( '^app-sw\.js$', 'index.php?sz_app_sw=1', 'top' );
    add_rewrite_tag( '%sz_app_sw%', '([0-9]+)' );
} );

add_action( 'parse_request', function ( WP $wp ): void {
    if ( empty( $wp->query_vars['sz_app_sw'] ) ) return;

    $plugin_url = defined( 'TPC_URL' ) ? TPC_URL : plugins_url( '', dirname( __FILE__ ) . '/senderzz-logistics.php' );
    $base_version = defined( 'TPC_VERSION' ) ? TPC_VERSION : get_option( 'senderzz_plugin_version', '1' );
    $version    = $base_version . '-app250';
    $logo_url   = esc_url( $plugin_url . 'assets/images/senderzz-logo.png' );
    $push_icon_url = esc_url( $plugin_url . 'assets/images/senderzz-raio-192.png?v=250' );

    header( 'Content-Type: application/javascript; charset=utf-8' );
    header( 'Cache-Control: no-cache' ); // SW nunca pode ter cache no próprio arquivo
    header( 'Service-Worker-Allowed: /' );

    echo "// Senderzz App SW v{$version} — gerado em " . gmdate( 'Y-m-d' ) . "\n";
    echo "const CACHE = 'sz-app-v{$version}';\n";
    echo "const SHELL_URL = '/app/';\n";
    echo "const LOGO = " . wp_json_encode( $logo_url ) . ";\n";
    echo "const PUSH_ICON = " . wp_json_encode( $push_icon_url ) . ";\n";
    ?>
const PRECACHE = [];

self.addEventListener('install', e => {
    self.skipWaiting();
});

self.addEventListener('activate', e => {
    e.waitUntil(
        caches.keys()
            .then(keys => Promise.all(
                keys.filter(k => k !== CACHE).map(k => caches.delete(k))
            ))
            .then(() => clients.claim())
    );
});

self.addEventListener('fetch', e => {
    const url = new URL(e.request.url);
    // AJAX e REST: sempre network — nunca cachear respostas de API
    if (url.pathname.startsWith('/wp-admin/admin-ajax.php') ||
        url.pathname.startsWith('/wp-json/')) return;
    // Navegação para /app/: sempre network para não prender login/sessão em shell antigo
    if (e.request.mode === 'navigate' && url.pathname.startsWith('/app')) {
        e.respondWith(fetch(e.request, { credentials: 'same-origin', cache: 'no-store' }));
        return;
    }
});

// Push notifications
self.addEventListener('push', e => {
    if (!e.data) return;
    let d = {};
    try { d = e.data.json(); } catch { d = { title: 'Pedidos COD', body: e.data.text() }; }
    const opts = {
        body:    d.body  || '',
        icon:    d.icon  || PUSH_ICON || LOGO,
        badge:   d.badge || d.icon || PUSH_ICON || LOGO,
        image:   d.image || undefined,
        data:    d.data  || {},
        vibrate: [200, 100, 200],
        tag:     'sz-app-' + (d.data?.order_id || Date.now()),
        requireInteraction: !!(d.data?.urgent),
    };
    e.waitUntil(self.registration.showNotification(d.title || 'Pedidos COD', opts));
});

self.addEventListener('notificationclick', e => {
    e.notification.close();
    const url = e.notification.data?.url || '/app/';
    e.waitUntil(
        clients.matchAll({ type: 'window', includeUncontrolled: true })
            .then(cls => {
                for (const c of cls) {
                    if (c.url.includes('/app') && 'focus' in c) return c.focus();
                }
                if (clients.openWindow) return clients.openWindow(url);
            })
    );
});
    <?php
    exit;
}, 1 );


/* ── 3.1 Manifest do /app/ para trocar o "from Senderzz" por nome operacional ── */
add_action( 'init', function (): void {
    add_rewrite_rule( '^app-manifest\.json$', 'index.php?sz_app_manifest=1', 'top' );
    add_rewrite_tag( '%sz_app_manifest%', '([0-9]+)' );
} );

add_action( 'parse_request', function ( WP $wp ): void {
    if ( empty( $wp->query_vars['sz_app_manifest'] ) ) return;

    $plugin_url = defined( 'TPC_URL' ) ? TPC_URL : plugins_url( '', dirname( __FILE__ ) . '/senderzz-logistics.php' ) . '/';
    $plugin_url = trailingslashit( $plugin_url );
    $app_name   = 'Senderzz';
    $icon_192   = esc_url_raw( $plugin_url . 'assets/images/senderzz-raio-192.png?v=250' );
    $icon_512   = esc_url_raw( $plugin_url . 'assets/images/senderzz-raio-512.png?v=250' );

    header( 'Content-Type: application/manifest+json; charset=utf-8' );
    header( 'Cache-Control: no-cache' );
    echo wp_json_encode( [
        'id'               => home_url( '/app/?v=250' ),
        'name'             => $app_name,
        'short_name'       => $app_name,
        'description'      => 'Notificações e carteira COD',
        'start_url'        => home_url( '/app/?v=250' ),
        'scope'            => home_url( '/app/' ),
        'display'          => 'standalone',
        'background_color' => '#f3f8fb',
        'theme_color'      => '#f3f8fb',
        'icons'            => [
            [ 'src' => $icon_192, 'sizes' => '192x192', 'type' => 'image/png', 'purpose' => 'any maskable' ],
            [ 'src' => $icon_512, 'sizes' => '512x512', 'type' => 'image/png', 'purpose' => 'any maskable' ],
        ],
    ], JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE );
    exit;
}, 1 );

/* ── 4. AJAX: preferências de notificação via pwa (sem autenticação WP) ── */
/* Os endpoints REST sz-notif/v1/{subscribe,unsubscribe,prefs} já existem
   em senderzz-notifications.php e funcionam com cookie de sessão do portal.
   Nada novo precisamos registrar aqui — o app vai chamar diretamente eles. */

/* ── 5. Flush rewrite na ativação ──────────────────────────────────────── */
add_action( 'senderzz_flush_rewrites', function (): void {
    flush_rewrite_rules();
} );

/* ── 6. Admin Senderzz: personalização de notificações do PWA ───────────── */
// Senderzz v221: submenu legado removido da origem; aba Notificações vive em Admin > Senderzz.
// add_action( 'admin_menu', function (): void {
//     add_submenu_page(
//         'woocommerce',
//         'Senderzz PWA / Notificações',
//         'Senderzz PWA / Notificações',
//         'manage_woocommerce',
//         'senderzz-pwa-notificacoes',
//         'sz_app_pwa_render_notifications_admin'
//     );
// }, 90 );

function sz_app_pwa_default_notification_templates(): array {
    return [
        // Cash On Delivery — produtor/afiliado recebem somente número do pedido + sua própria comissão.
        'agendamento_cod'  => [
            'title' => '📅 Agendamento',
            'body'  => '',
            'producer_title'  => '📅 Agendamento',
            'producer_body'   => 'Pedido {{numero_pedido}} · Sua comissão é {{comissao_produtor}}.',
            'affiliate_title' => '📅 Agendamento',
            'affiliate_body'  => 'Pedido {{numero_pedido}} · Sua comissão é {{comissao_afiliado}}.',
            'admin_title'     => '📅 Agendamento · {{cliente}}',
            'admin_body'      => 'Pedido {{numero_pedido}} · {{produto}} · {{cidade}} · Admin indiv. {{comissao_admin_liquida}} · Admin total {{comissao_admin_liquida_total}}',
        ],
        'em_rota_cod'      => [
            'title' => '🏍️ Pedido Em Rota',
            'body'  => '',
            'producer_title'  => '🏍️ Pedido Em Rota',
            'producer_body'   => 'Pedido {{numero_pedido}} · Sua comissão é {{comissao_produtor}}.',
            'affiliate_title' => '🏍️ Pedido Em Rota',
            'affiliate_body'  => 'Pedido {{numero_pedido}} · Sua comissão é {{comissao_afiliado}}.',
            'admin_title'     => '🏍️ Em rota · {{cliente}}',
            'admin_body'      => 'Pedido {{numero_pedido}} · {{produto}} · {{tipo_entrega}} · Admin indiv. {{comissao_admin_liquida}} · Admin total {{comissao_admin_liquida_total}}',
        ],
        'completo_cod'     => [
            'title' => '✅ Pedido Completo',
            'body'  => '',
            'producer_title'  => '✅ Pedido Entregue',
            'producer_body'   => 'Pedido {{numero_pedido}} entregue · Sua comissão é {{comissao_produtor}}.',
            'affiliate_title' => '✅ Pedido Entregue',
            'affiliate_body'  => 'Pedido {{numero_pedido}} entregue · Sua comissão é {{comissao_afiliado}}.',
            'admin_title'     => '✅ Completo · {{cliente}}',
            'admin_body'      => 'Pedido {{numero_pedido}} completo · {{produto}} · Admin indiv. {{comissao_admin_liquida}} · Admin total {{comissao_admin_liquida_total}}.',
        ],
        'frustrado_cod'    => [
            'title' => '❌ Pedido Frustrado',
            'body'  => '',
            'producer_title'  => '❌ Pedido Frustrado',
            'producer_body'   => 'Pedido {{numero_pedido}} atualizado.',
            'affiliate_title' => '❌ Pedido Frustrado',
            'affiliate_body'  => 'Pedido {{numero_pedido}} atualizado.',
            'admin_title'     => '❌ Frustrado · {{cliente}}',
            'admin_body'      => 'Pedido {{numero_pedido}} · {{produto}} · {{cidade}} · Status {{status}}.',
        ],

        // Expedição — sem vazamento de cliente/produto para produtor/afiliado no push.
        'pedido_feito'     => [ 'title' => '🛒 Pedido Novo', 'body' => '', 'producer_title' => '🛒 Pedido Novo', 'producer_body' => 'Pedido {{numero_pedido}} · Sua comissão é {{comissao_produtor}}.', 'affiliate_title' => '🛒 Pedido Novo', 'affiliate_body' => 'Pedido {{numero_pedido}} · Sua comissão é {{comissao_afiliado}}.', 'admin_title' => '🛒 Pedido Novo · {{cliente}}', 'admin_body' => 'Pedido {{numero_pedido}} · {{produto}} · Total {{valor_total_pedido}} · {{tipo_entrega}}' ],
        'enviado_pad'      => [ 'title' => '📦 Pedido Enviado', 'body' => '', 'producer_title' => '📦 Pedido Enviado', 'producer_body' => 'Pedido {{numero_pedido}}.', 'affiliate_title' => '📦 Pedido Enviado', 'affiliate_body' => 'Pedido {{numero_pedido}}.', 'admin_title' => '📦 Enviado · {{transportadora}}', 'admin_body' => 'Pedido {{numero_pedido}} · {{produto}} · Envio {{valor_envio}} · Total {{valor_total_pedido}}' ],
        'entregue'         => [ 'title' => '✅ Pedido Entregue', 'body' => '', 'producer_title' => '✅ Pedido Entregue', 'producer_body' => 'Pedido {{numero_pedido}} entregue.', 'affiliate_title' => '✅ Pedido Entregue', 'affiliate_body' => 'Pedido {{numero_pedido}} entregue.', 'admin_title' => '✅ Entregue · {{cliente}}', 'admin_body' => 'Pedido {{numero_pedido}} entregue · {{produto}} · Total {{valor_total_pedido}}.' ],

        // Admin Senderzz — variáveis completas permanecem somente para admin.
        'admin_motoboy'    => [ 'title' => '🏍️ Motoboy · {{cliente}}', 'body' => '', 'producer_title' => '🏍️ Motoboy', 'producer_body' => 'Pedido {{numero_pedido}}.', 'affiliate_title' => '🏍️ Motoboy', 'affiliate_body' => 'Pedido {{numero_pedido}}.', 'admin_title' => '🏍️ Motoboy · {{cliente}}', 'admin_body' => 'Pedido {{numero_pedido}} · {{produto}} · Admin indiv. {{comissao_admin_liquida}} · Admin total {{comissao_admin_liquida_total}}' ],
        'admin_expedicao'  => [ 'title' => '📦 Expedição · {{transportadora}}', 'body' => '', 'producer_title' => '📦 Expedição', 'producer_body' => 'Pedido {{numero_pedido}}.', 'affiliate_title' => '📦 Expedição', 'affiliate_body' => 'Pedido {{numero_pedido}}.', 'admin_title' => '📦 Expedição · {{transportadora}}', 'admin_body' => 'Pedido {{numero_pedido}} · {{produto}} · Taxa+% {{taxa_entrega_percentual_admin}} · Total {{valor_total_pedido}} · Produtor {{comissao_final_produtor}}' ],
    ];
}

function sz_app_pwa_get_notification_templates(): array {
    $saved = get_option( 'sz_app_pwa_notification_templates', [] );
    return array_replace_recursive( sz_app_pwa_default_notification_templates(), is_array( $saved ) ? $saved : [] );
}


/**
 * Variáveis permitidas por papel do destinatário.
 * Produtor e afiliado recebem somente número do pedido + sua própria comissão.
 * Admin continua com banco completo para auditoria operacional.
 */
function sz_app_pwa_notification_allowed_vars_for_role( string $role ): array {
    $role = sanitize_key( $role );
    $common_order = [ '{{numero_pedido}}', '{{pedido_id}}' ];
    if ( $role === 'producer' ) {
        return array_merge( $common_order, [ '{{comissao_produtor}}', '{{comissao_final_produtor}}', '{{comissao}}' ] );
    }
    if ( $role === 'affiliate' ) {
        return array_merge( $common_order, [ '{{comissao_afiliado}}', '{{comissao_final_afiliado}}', '{{comissao}}' ] );
    }
    return [
        '{{numero_pedido}}', '{{pedido_id}}', '{{cliente}}', '{{valor_total_pedido}}', '{{total}}', '{{valor_envio}}',
        '{{taxa_entrega_percentual_admin}}', '{{percentual_envio}}', '{{comissao_final_afiliado}}', '{{comissao_final_produtor}}',
        '{{comissao}}', '{{cidade}}', '{{produto}}', '{{produtos}}', '{{comissao_produtor}}', '{{comissao_afiliado}}',
        '{{comissao_admin_liquida}}', '{{comissao_admin_liquida_total}}', '{{comissao_admin}}', '{{comissao_admin_total}}',
        '{{fundo_operacional}}', '{{taxa_motoboy_admin}}', '{{status}}', '{{transportadora}}', '{{tipo_entrega}}'
    ];
}

function sz_app_pwa_restrict_template_variables( string $template, string $role ): string {
    $role = sanitize_key( $role );
    if ( $role === 'admin' || $template === '' ) return $template;
    $allowed = array_fill_keys( sz_app_pwa_notification_allowed_vars_for_role( $role ), true );
    return (string) preg_replace_callback( '/\{\{[a-zA-Z0-9_]+\}\}/', function( $m ) use ( $allowed ) {
        return isset( $allowed[ $m[0] ] ) ? $m[0] : '';
    }, $template );
}

function sz_app_pwa_filter_replacements_for_role( array $vars, string $role ): array {
    $role = sanitize_key( $role );
    if ( $role === 'admin' ) return $vars;
    $allowed = array_fill_keys( sz_app_pwa_notification_allowed_vars_for_role( $role ), true );
    return array_intersect_key( $vars, $allowed );
}



function sz_app_pwa_normalize_wc_status_slug( string $status ): string {
    $status = sanitize_key( $status );
    if ( $status === '' ) return '';
    return substr( $status, 0, 3 ) === 'wc-' ? $status : 'wc-' . $status;
}

function sz_app_pwa_default_notification_status_map(): array {
    return [
        // Cada status deve pertencer a apenas uma notificação.
        'agendamento_cod' => [ 'wc-agendado' ],
        'em_rota_cod'     => [ 'wc-emrota' ],
        'completo_cod'    => [ 'wc-completo' ],
        'frustrado_cod'   => [ 'wc-frustrado' ],
        'pedido_feito'    => [ 'wc-processing' ],
        'enviado_pad'     => [ 'wc-enviado' ],
        'entregue'        => [ 'wc-entregue' ],
        'admin_motoboy'   => [],
        'admin_expedicao' => [],
    ];
}

function sz_app_pwa_unique_notification_status_map( array $map, array $event_order = [] ): array {
    $event_order = $event_order ?: array_keys( sz_app_pwa_default_notification_templates() );
    $unique = [];
    $claimed = [];
    foreach ( $event_order as $event_key ) {
        $unique[ $event_key ] = [];
        foreach ( (array) ( $map[ $event_key ] ?? [] ) as $raw_status ) {
            $slug = sz_app_pwa_normalize_wc_status_slug( (string) $raw_status );
            if ( $slug === '' || isset( $claimed[ $slug ] ) ) continue;
            $claimed[ $slug ] = $event_key;
            $unique[ $event_key ] = [ $slug ];
            break; // cada tipo de notificação aceita somente um status.
        }
    }
    return $unique;
}

function sz_app_pwa_get_notification_status_map(): array {
    $saved = get_option( 'sz_app_pwa_notification_status_map', [] );
    $map = is_array( $saved ) && ! empty( $saved ) ? $saved : sz_app_pwa_default_notification_status_map();
    return sz_app_pwa_unique_notification_status_map( $map );
}



function sz_app_pwa_get_admin_recipients_map(): array {
    $saved = get_option( 'sz_app_pwa_admin_recipients', [] );
    return is_array( $saved ) ? $saved : [];
}

function sz_app_pwa_get_admin_recipients_for_event( string $event ): array {
    $map = sz_app_pwa_get_admin_recipients_map();
    $ids = isset( $map[ $event ] ) && is_array( $map[ $event ] ) ? $map[ $event ] : [];
    return array_values( array_filter( array_map( 'absint', $ids ) ) );
}

function sz_app_pwa_is_admin_recipient_for_event( string $event, int $recipient_user_id ): bool {
    return $recipient_user_id > 0 && in_array( $recipient_user_id, sz_app_pwa_get_admin_recipients_for_event( $event ), true );
}

function sz_app_pwa_default_notification_recipients(): array {
    $defaults = [];
    foreach ( array_keys( sz_app_pwa_default_notification_templates() ) as $event_key ) {
        if ( strpos( (string) $event_key, 'admin_' ) === 0 ) continue;
        $defaults[ $event_key ] = [
            'producer'  => 1,
            'affiliate' => 1,
            'admin'     => 0,
        ];
    }
    return $defaults;
}

function sz_app_pwa_get_notification_recipients_map(): array {
    $saved = get_option( 'sz_app_pwa_notification_recipients', [] );
    $saved = is_array( $saved ) ? $saved : [];
    $defaults = sz_app_pwa_default_notification_recipients();
    $out = [];
    foreach ( $defaults as $event_key => $cfg ) {
        $row = isset( $saved[ $event_key ] ) && is_array( $saved[ $event_key ] ) ? $saved[ $event_key ] : [];
        $out[ $event_key ] = [
            'producer'  => ! empty( $row['producer'] ) ? 1 : 0,
            'affiliate' => ! empty( $row['affiliate'] ) ? 1 : 0,
            'admin'     => ! empty( $row['admin'] ) ? 1 : 0,
        ];
        // Primeira instalação/sem opção salva: mantém defaults produtor + afiliado.
        if ( ! isset( $saved[ $event_key ] ) ) $out[ $event_key ] = $cfg;
    }
    return $out;
}

function sz_app_pwa_get_recipients_for_event( string $event ): array {
    $map = sz_app_pwa_get_notification_recipients_map();
    return $map[ $event ] ?? [ 'producer' => 1, 'affiliate' => 1, 'admin' => 0 ];
}


function sz_app_pwa_default_notification_order_number_flags(): array {
    return array_fill_keys( array_keys( sz_app_pwa_default_notification_templates() ), 1 );
}

function sz_app_pwa_get_notification_order_number_flags(): array {
    $saved = get_option( 'sz_app_pwa_notification_order_number_flags', [] );
    return array_replace( sz_app_pwa_default_notification_order_number_flags(), is_array( $saved ) ? $saved : [] );
}

function sz_app_pwa_render_notifications_admin(): void {
    if ( ! current_user_can( 'manage_woocommerce' ) ) return;
    $defaults = sz_app_pwa_default_notification_templates();
    if ( isset( $_POST['sz_app_pwa_notif_nonce'] ) && wp_verify_nonce( sanitize_text_field( wp_unslash( $_POST['sz_app_pwa_notif_nonce'] ) ), 'sz_app_pwa_notif_save' ) ) {
        $new = [];
        $status_map = [];
        $order_flags = [];
        $admin_recipients = [];
        $recipients_map = [];

        foreach ( $defaults as $key => $tpl ) {
            $new[ $key ] = [
                'title' => sanitize_text_field( wp_unslash( $_POST['tpl'][ $key ]['title'] ?? $tpl['title'] ) ),
                'body'  => '',
                'producer_title'  => sanitize_text_field( wp_unslash( $_POST['tpl'][ $key ]['producer_title'] ?? ( $tpl['producer_title'] ?? $tpl['title'] ) ) ),
                'producer_body'   => sanitize_textarea_field( wp_unslash( $_POST['tpl'][ $key ]['producer_body'] ?? ( $tpl['producer_body'] ?? '' ) ) ),
                'affiliate_title' => sanitize_text_field( wp_unslash( $_POST['tpl'][ $key ]['affiliate_title'] ?? ( $tpl['affiliate_title'] ?? $tpl['title'] ) ) ),
                'affiliate_body'  => sanitize_textarea_field( wp_unslash( $_POST['tpl'][ $key ]['affiliate_body'] ?? ( $tpl['affiliate_body'] ?? '' ) ) ),
                'admin_title'     => sanitize_text_field( wp_unslash( $_POST['tpl'][ $key ]['admin_title'] ?? ( $tpl['admin_title'] ?? $tpl['title'] ) ) ),
                'admin_body'      => sanitize_textarea_field( wp_unslash( $_POST['tpl'][ $key ]['admin_body'] ?? ( $tpl['admin_body'] ?? '' ) ) ),
            ];
            $raw_status = sanitize_key( wp_unslash( $_POST['status_map'][ $key ] ?? '' ) );
            $slug = sz_app_pwa_normalize_wc_status_slug( $raw_status );
            $status_map[ $key ] = $slug !== '' ? [ $slug ] : [];
            $order_flags[ $key ] = ! empty( $_POST['include_order_number'][ $key ] ) ? 1 : 0;
            $posted_admins = $_POST['admin_recipients'][ $key ] ?? [];
            $admin_recipients[ $key ] = is_array( $posted_admins ) ? array_values( array_filter( array_map( 'absint', $posted_admins ) ) ) : [];
            $posted_recipients = $_POST['recipients'][ $key ] ?? [];
            $posted_recipients = is_array( $posted_recipients ) ? $posted_recipients : [];
            $recipients_map[ $key ] = [
                'producer'  => ! empty( $posted_recipients['producer'] ) ? 1 : 0,
                'affiliate' => ! empty( $posted_recipients['affiliate'] ) ? 1 : 0,
                'admin'     => ! empty( $posted_recipients['admin'] ) ? 1 : 0,
            ];
        }
        foreach ( $new as $tpl_key => $tpl_row ) {
            foreach ( [ 'producer_title', 'producer_body' ] as $field_key ) {
                $new[ $tpl_key ][ $field_key ] = sz_app_pwa_restrict_template_variables( (string) ( $new[ $tpl_key ][ $field_key ] ?? '' ), 'producer' );
            }
            foreach ( [ 'affiliate_title', 'affiliate_body' ] as $field_key ) {
                $new[ $tpl_key ][ $field_key ] = sz_app_pwa_restrict_template_variables( (string) ( $new[ $tpl_key ][ $field_key ] ?? '' ), 'affiliate' );
            }
        }
        $status_map = sz_app_pwa_unique_notification_status_map( $status_map, array_keys( $defaults ) );
        update_option( 'sz_app_pwa_notification_templates', $new, false );
        update_option( 'sz_app_pwa_notification_status_map', $status_map, false );
        update_option( 'sz_app_pwa_notification_order_number_flags', $order_flags, false );
        update_option( 'sz_app_pwa_admin_recipients', $admin_recipients, false );
        update_option( 'sz_app_pwa_notification_recipients', $recipients_map, false );
        echo '<div class="notice notice-success"><p>Templates, destinatários e vínculos de status salvos.</p></div>';
    }
    $tpls = sz_app_pwa_get_notification_templates();
    $status_map = sz_app_pwa_get_notification_status_map();
    $order_flags = sz_app_pwa_get_notification_order_number_flags();
    $admin_recipients_map = sz_app_pwa_get_admin_recipients_map();
    $recipients_map = function_exists( 'sz_app_pwa_get_notification_recipients_map' ) ? sz_app_pwa_get_notification_recipients_map() : [];
    $admin_users = function_exists( 'get_users' ) ? get_users( [ 'role__in' => [ 'administrator' ], 'orderby' => 'display_name', 'order' => 'ASC' ] ) : [];
    $order_statuses = function_exists( 'wc_get_order_statuses' ) ? wc_get_order_statuses() : [];
    $vars = sz_app_pwa_notification_allowed_vars_for_role( 'admin' );
    $producer_vars = sz_app_pwa_notification_allowed_vars_for_role( 'producer' );
    $affiliate_vars = sz_app_pwa_notification_allowed_vars_for_role( 'affiliate' );
    ?>
    <style>
      .sz-pwa-admin{max-width:1040px}
      .sz-pwa-vars{background:#fff;border:1px solid #d8dee8;border-radius:12px;padding:16px;margin:14px 0 18px}
      .sz-pwa-card{background:#fff;border:1px solid #d8dee8;border-radius:12px;padding:18px;margin-bottom:14px}
      .sz-pwa-card h2{margin:0 0 14px;font-size:var(--sz-text-lg);font-weight:700;text-transform:none;color:#111827}
      .sz-pwa-field{margin:0 0 14px}.sz-pwa-field strong{display:block;margin-bottom:6px;color:#111827}
      .sz-pwa-status-box{margin-top:12px;padding:14px;background:#f8fafc;border:1px solid #e5e7eb;border-radius:12px}
      .sz-pwa-status-select{width:100%;max-width:460px;height:42px;border:1px solid #d8dee8;border-radius:10px;background:#fff;color:#111827;padding:0 12px;font-size:var(--sz-text-base);font-weight:700;box-shadow:none;outline:none}
      .sz-pwa-status-select:focus{border-color:#E8650A;box-shadow:0 0 0 3px rgba(232,101,10,.10)}
      .sz-pwa-status-hint{margin:7px 0 0;color:#64748b;font-size:var(--sz-text-meta)}.sz-pwa-status-hint code{background:#eef2f7;color:#475569;font-size:var(--sz-text-sm);padding:2px 5px;border-radius:4px}.sz-pwa-plain-check{appearance:auto!important;-webkit-appearance:auto!important;width:18px!important;height:18px!important;min-width:18px!important;min-height:18px!important;margin:2px 0 0!important;padding:0!important;accent-color:#E8650A!important;box-shadow:none!important;outline:none!important;position:static!important;opacity:1!important}.sz-pwa-flag{display:inline-flex;gap:9px;align-items:center;padding:9px 10px;border:1px solid #e5e7eb;border-radius:10px;background:#fff}
    .sz-pwa-varselect{display:flex;gap:8px;align-items:center;margin:8px 0 10px}.sz-pwa-varselect select{min-width:260px;max-width:360px;height:34px;border:1px solid #d8dee8;border-radius:9px;background:#fff;color:#111827;padding:0 10px;font-size:var(--sz-text-meta);font-family:var(--sz-font)}.sz-pwa-varselect button{height:34px;border-radius:9px;border:1px solid #E8650A;background:#fff7ed;color:#E8650A;font-weight:700;cursor:pointer;padding:0 12px}.sz-pwa-varselect button:hover{background:#E8650A;color:#fff}.sz-pwa-two{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:12px}.sz-pwa-recipient-row{display:flex;flex-wrap:wrap;gap:8px;margin:8px 0 14px}.sz-pwa-card details{margin-top:10px}.sz-pwa-card summary{list-style:none}.sz-pwa-card summary::-webkit-details-marker{display:none}.sz-pwa-card summary:after{content:'+';float:right;color:#E8650A;font-weight:700}.sz-pwa-card details[open] summary:after{content:'–'}
    </style>
    <div class="wrap sz-pwa-admin">
        <h1>Senderzz · PWA / Notificações</h1>
        <p>Configure a cadeia de cada notificação: produtor, afiliado e admins que devem receber. Cada notificação usa um único status universal.</p>
        <div class="sz-pwa-vars">
            <strong>Banco de variáveis</strong>
            <p class="description" style="margin:6px 0 0">As variáveis ficam disponíveis em um seletor compacto ao lado de cada campo de mensagem.</p>
        </div>
        <form method="post">
            <?php wp_nonce_field( 'sz_app_pwa_notif_save', 'sz_app_pwa_notif_nonce' ); ?>
            <?php foreach ( $tpls as $key => $tpl ) : if ( strpos( (string) $key, 'admin_' ) === 0 ) continue; ?>
                <div class="sz-pwa-card">
                    <h2 style="margin-top:0;text-transform:capitalize"><?php echo esc_html( str_replace( '_', ' ', $key ) ); ?></h2>
                    <p class="sz-pwa-field"><label><strong>Título do push</strong><input type="text" name="tpl[<?php echo esc_attr( $key ); ?>][title]" class="large-text" value="<?php echo esc_attr( $tpl['title'] ); ?>"></label></p>
                    <?php $event_recipients = $recipients_map[ $key ] ?? [ 'producer' => 1, 'affiliate' => 1, 'admin' => 0 ]; ?>
                    <div class="sz-pwa-status-box" style="margin-top:0;background:#fff7ed;border-color:#fed7aa">
                        <strong>Destinatários desta notificação</strong>
                        <p class="description" style="margin:5px 0 10px">Marque exatamente quem pode receber este evento. Isso permite produtor apenas, afiliado apenas, produtor + afiliado ou admin selecionado.</p>
                        <div class="sz-pwa-recipient-row">
                            <label class="sz-pwa-flag"><input type="checkbox" class="sz-pwa-plain-check" name="recipients[<?php echo esc_attr( $key ); ?>][producer]" value="1" <?php checked( ! empty( $event_recipients['producer'] ) ); ?>> <span>Produtor</span></label>
                            <label class="sz-pwa-flag"><input type="checkbox" class="sz-pwa-plain-check" name="recipients[<?php echo esc_attr( $key ); ?>][affiliate]" value="1" <?php checked( ! empty( $event_recipients['affiliate'] ) ); ?>> <span>Afiliado</span></label>
                            <label class="sz-pwa-flag"><input type="checkbox" class="sz-pwa-plain-check" name="recipients[<?php echo esc_attr( $key ); ?>][admin]" value="1" <?php checked( ! empty( $event_recipients['admin'] ) ); ?>> <span>Admin</span></label>
                        </div>
                    </div>
                    <?php if ( true ) : ?>
                        <div class="sz-pwa-status-box" style="background:#fff;border-color:#fed7aa">
                            <strong>Mensagem para produtor</strong>
                            <p class="description">Produtor só pode receber <code>{{numero_pedido}}</code>, <code>{{comissao_produtor}}</code>, <code>{{comissao_final_produtor}}</code> ou <code>{{comissao}}</code>. Qualquer outra variável é removida no envio.</p>
                            <p class="sz-pwa-field"><label>Título<input type="text" name="tpl[<?php echo esc_attr( $key ); ?>][producer_title]" class="large-text" value="<?php echo esc_attr( $tpl['producer_title'] ?? $tpl['title'] ); ?>"></label></p>
                            <div class="sz-pwa-varselect" data-target="producer-<?php echo esc_attr( $key ); ?>"><select><option value="">Inserir variável...</option><?php foreach ( $producer_vars as $v ) echo '<option value="' . esc_attr( $v ) . '">' . esc_html( $v ) . '</option>'; ?></select><button type="button" class="sz-pwa-insert-var">Inserir</button></div><p class="sz-pwa-field"><label>Mensagem<textarea id="producer-<?php echo esc_attr( $key ); ?>" name="tpl[<?php echo esc_attr( $key ); ?>][producer_body]" class="large-text" rows="3"><?php echo esc_textarea( $tpl['producer_body'] ?? $tpl['body'] ); ?></textarea></label></p>
                        </div>
                        <details class="sz-pwa-status-box" style="background:#fff;border-color:#d8dee8"><summary style="cursor:pointer;font-weight:700">Mensagem para afiliado</summary>
                            
                            <p class="description">Afiliado só pode receber <code>{{numero_pedido}}</code>, <code>{{comissao_afiliado}}</code>, <code>{{comissao_final_afiliado}}</code> ou <code>{{comissao}}</code>. Qualquer outra variável é removida no envio.</p>
                            <p class="sz-pwa-field"><label>Título<input type="text" name="tpl[<?php echo esc_attr( $key ); ?>][affiliate_title]" class="large-text" value="<?php echo esc_attr( $tpl['affiliate_title'] ?? $tpl['title'] ); ?>"></label></p>
                            <div class="sz-pwa-varselect" data-target="affiliate-<?php echo esc_attr( $key ); ?>"><select><option value="">Inserir variável...</option><?php foreach ( $affiliate_vars as $v ) echo '<option value="' . esc_attr( $v ) . '">' . esc_html( $v ) . '</option>'; ?></select><button type="button" class="sz-pwa-insert-var">Inserir</button></div><p class="sz-pwa-field"><label>Mensagem<textarea id="affiliate-<?php echo esc_attr( $key ); ?>" name="tpl[<?php echo esc_attr( $key ); ?>][affiliate_body]" class="large-text" rows="3"><?php echo esc_textarea( $tpl['affiliate_body'] ?? $tpl['body'] ); ?></textarea></label></p>
                        </details>
                        <details class="sz-pwa-status-box" style="background:#fff;border-color:#dbeafe"><summary style="cursor:pointer;font-weight:700">Mensagem para admin</summary>
                            
                            <p class="description">Marque o destinatário Admin acima e selecione quais administradores recebem. Se nenhum admin for selecionado, o histórico registra skipped.</p>
                            <div style="display:flex;flex-wrap:wrap;gap:8px;margin:8px 0 12px">
                                <?php $selected_admins = isset( $admin_recipients_map[ $key ] ) && is_array( $admin_recipients_map[ $key ] ) ? array_map( 'intval', $admin_recipients_map[ $key ] ) : []; ?>
                                <?php foreach ( $admin_users as $adm ) : $adm_id = (int) $adm->ID; ?>
                                    <label class="sz-pwa-flag"><input type="checkbox" class="sz-pwa-plain-check" name="admin_recipients[<?php echo esc_attr( $key ); ?>][]" value="<?php echo esc_attr( $adm_id ); ?>" <?php checked( in_array( $adm_id, $selected_admins, true ) ); ?>> <span><?php echo esc_html( ( $adm->display_name ?: $adm->user_login ) . ' · ID ' . $adm_id ); ?></span></label>
                                <?php endforeach; ?>
                            </div>
                            <p class="sz-pwa-field"><label>Título<input type="text" name="tpl[<?php echo esc_attr( $key ); ?>][admin_title]" class="large-text" value="<?php echo esc_attr( $tpl['admin_title'] ?? $tpl['title'] ); ?>"></label></p>
                            <div class="sz-pwa-varselect" data-target="admin-<?php echo esc_attr( $key ); ?>"><select><option value="">Inserir variável...</option><?php foreach ( $vars as $v ) echo '<option value="' . esc_attr( $v ) . '">' . esc_html( $v ) . '</option>'; ?></select><button type="button" class="sz-pwa-insert-var">Inserir</button></div><p class="sz-pwa-field"><label>Mensagem<textarea id="admin-<?php echo esc_attr( $key ); ?>" name="tpl[<?php echo esc_attr( $key ); ?>][admin_body]" class="large-text" rows="3"><?php echo esc_textarea( $tpl['admin_body'] ?? '' ); ?></textarea></label></p>
                        </details>
                    <?php endif; ?>
                    <p><label class="sz-pwa-flag"><input type="checkbox" class="sz-pwa-plain-check" name="include_order_number[<?php echo esc_attr( $key ); ?>]" value="1" <?php checked( ! empty( $order_flags[ $key ] ) ); ?>> <strong>Incluir número do pedido automaticamente nesta notificação</strong></label></p>
                    <div class="sz-pwa-status-box">
                        <strong>Status que dispara esta notificação</strong>
                        <p style="margin:6px 0 10px;color:#646970">Selecione um único status. O mesmo status será removido automaticamente das outras notificações.</p>
                        <?php $selected_status = (string) ( $status_map[ $key ][0] ?? '' ); ?>
                        <select class="sz-pwa-status-select" name="status_map[<?php echo esc_attr( $key ); ?>]" data-event="<?php echo esc_attr( $key ); ?>">
                            <option value="">— Não disparar por status —</option>
                            <?php foreach ( $order_statuses as $st_key => $st_label ) : $slug = sz_app_pwa_normalize_wc_status_slug( (string) $st_key ); ?>
                                <option value="<?php echo esc_attr( $slug ); ?>" <?php selected( $selected_status, $slug ); ?>><?php echo esc_html( $st_label . ' — ' . $slug ); ?></option>
                            <?php endforeach; ?>
                        </select>
                        <?php if ( $selected_status ) : ?><p class="sz-pwa-status-hint">Selecionado: <code><?php echo esc_html( $selected_status ); ?></code></p><?php endif; ?>
                    </div>
                </div>
            <?php endforeach; ?>
            <script>
            document.addEventListener('click', function(e){
                var btn = e.target && e.target.closest ? e.target.closest('.sz-pwa-insert-var') : null;
                if(!btn) return;
                var bar = btn.closest('.sz-pwa-varselect');
                var select = bar ? bar.querySelector('select') : null;
                var target = bar ? document.getElementById(bar.getAttribute('data-target')) : null;
                if(!target || !select || !select.value) return;
                var v = select.value;
                var start = target.selectionStart || target.value.length;
                var end = target.selectionEnd || target.value.length;
                target.value = target.value.slice(0,start) + v + target.value.slice(end);
                target.focus();
                target.selectionStart = target.selectionEnd = start + v.length;
            });
            document.addEventListener('change', function(e){
                var sel = e.target && e.target.classList && e.target.classList.contains('sz-pwa-status-select') ? e.target : null;
                if(!sel || !sel.value) return;
                document.querySelectorAll('.sz-pwa-status-select').forEach(function(other){
                    if(other !== sel && other.value === sel.value) other.value = '';
                });
            });
            </script>
            <button class="button button-primary button-large">Salvar templates</button>
        </form>
    </div>
    <?php
}

function sz_app_pwa_money( float $value ): string {
    return 'R$ ' . number_format( $value, 2, ',', '.' );
}

function sz_app_pwa_order_fee_total( WC_Order $order ): float {
    $mb_total = (float) $order->get_meta( '_sz_mb_taxa_total', true );
    if ( $mb_total > 0 ) return round( $mb_total, 2 );
    $sum = (float) $order->get_meta( '_sz_mb_taxa_entrega', true )
         + (float) $order->get_meta( '_sz_mb_taxa_manuseio', true )
         + (float) $order->get_meta( '_sz_mb_taxa_adicional', true )
         + (float) $order->get_meta( '_sz_mb_taxa_percentual', true );
    if ( $sum > 0 ) return round( $sum, 2 );
    return round( (float) $order->get_shipping_total() + (float) $order->get_shipping_tax(), 2 );
}


function sz_app_pwa_affiliate_transaction_fee_amount( WC_Order $order, float $gross = 0.0, float $commission_gross = 0.0 ): float {
    $saved = $order->get_meta( '_sz_aff_transaction_fee', true );
    if ( $saved !== '' ) return round( max( 0, (float) $saved ), 2 );
    if ( function_exists( 'sz_aff_split_transaction_fee_parts' ) ) {
        $parts = sz_aff_split_transaction_fee_parts( $order, $gross > 0 ? $gross : (float) $order->get_total(), $commission_gross );
        if ( is_array( $parts ) && isset( $parts['affiliate'] ) ) {
            return round( max( 0, (float) $parts['affiliate'] ), 2 );
        }
    }
    return 0.0;
}


function sz_app_pwa_order_products_label( WC_Order $order ): string {
    $names = [];
    foreach ( $order->get_items() as $item ) {
        if ( ! is_a( $item, 'WC_Order_Item_Product' ) ) continue;
        $name = function_exists( 'senderzz_clean_product_label' ) ? senderzz_clean_product_label( $item->get_name() ) : trim( (string) $item->get_name() );
        if ( $name === '' ) continue;
        $qty = (int) $item->get_quantity();
        $names[] = $qty > 1 ? $qty . 'x ' . $name : $name;
    }
    return $names ? implode( ', ', array_slice( $names, 0, 3 ) ) : 'Produto';
}

function sz_app_pwa_affiliate_wp_user_id_for_order( WC_Order $order ): int {
    $uid = (int) $order->get_meta( '_sz_affiliate_user_id', true );
    if ( $uid ) return $uid;
    $aff_id = (int) ( $order->get_meta( '_sz_affiliate_id', true ) ?: $order->get_meta( '_sz_affiliate_ref', true ) );
    if ( $aff_id && function_exists( 'sz_aff_get_affiliate_row' ) ) {
        $row = sz_aff_get_affiliate_row( $aff_id );
        return (int) ( $row['user_id'] ?? 0 );
    }
    return 0;
}

function sz_app_pwa_affiliate_commission_amount( WC_Order $order ): float {
    // Fonte oficial: meta final líquida gravada no split/carteira do afiliado.
    $stored = $order->get_meta( '_sz_aff_commission', true );
    if ( $stored !== '' && is_numeric( $stored ) ) return round( max( 0, (float) $stored ), 2 );
    $stored_legacy = $order->get_meta( '_sz_affiliate_commission_amount', true );
    if ( $stored_legacy !== '' && is_numeric( $stored_legacy ) ) return round( max( 0, (float) $stored_legacy ), 2 );

    $aff_id = (int) ( $order->get_meta( '_sz_affiliate_id', true ) ?: $order->get_meta( '_sz_affiliate_ref', true ) );
    if ( ! $aff_id ) return 0.0;

    $gross = (float) $order->get_meta( '_sz_aff_gross', true );
    if ( $gross <= 0 ) $gross = (float) $order->get_total();

    $commission_gross = (float) $order->get_meta( '_sz_aff_commission_gross', true );
    if ( $commission_gross <= 0 ) {
        $pct = (float) $order->get_meta( '_sz_aff_commission_pct', true );
        if ( ! $pct && function_exists( 'sz_aff_get_affiliate_row' ) ) {
            $row = sz_aff_get_affiliate_row( $aff_id );
            $pct = (float) ( $row['commission_pct'] ?? 0 );
        }
        $base = (float) $order->get_meta( '_senderzz_offer_value', true );
        if ( $base <= 0 ) $base = $gross;
        $commission_gross = $pct > 0 ? round( max( 0, $base ) * max( 0, min( 100, $pct ) ) / 100, 2 ) : 0.0;
    }

    $affiliate_tx_fee = sz_app_pwa_affiliate_transaction_fee_amount( $order, $gross, $commission_gross );
    return round( max( 0, $commission_gross - $affiliate_tx_fee ), 2 );
}


function sz_app_pwa_producer_commission_amount( WC_Order $order ): float {
    // Fonte oficial: meta final gravada para a carteira COD do produtor.
    // Nunca usar _sz_aff_net - comissão, pois _sz_aff_net é líquido antes do split e gerava divergência no push.
    $stored = $order->get_meta( '_sz_prod_commission', true );
    if ( $stored !== '' && is_numeric( $stored ) ) return round( max( 0, (float) $stored ), 2 );

    $gross = (float) $order->get_meta( '_sz_aff_gross', true );
    if ( $gross <= 0 ) $gross = (float) $order->get_total();

    $fees = (float) $order->get_meta( '_sz_aff_fees', true );
    if ( $fees <= 0 && function_exists( 'sz_aff_get_order_fee_total' ) ) $fees = sz_aff_get_order_fee_total( $order );
    if ( $fees <= 0 ) $fees = sz_app_pwa_order_fee_total( $order );

    $commission_gross = (float) $order->get_meta( '_sz_aff_commission_gross', true );
    if ( $commission_gross <= 0 ) {
        $aff_id = (int) ( $order->get_meta( '_sz_affiliate_id', true ) ?: $order->get_meta( '_sz_affiliate_ref', true ) );
        if ( $aff_id ) {
            $pct = (float) $order->get_meta( '_sz_aff_commission_pct', true );
            if ( ! $pct && function_exists( 'sz_aff_get_affiliate_row' ) ) {
                $row = sz_aff_get_affiliate_row( $aff_id );
                $pct = (float) ( $row['commission_pct'] ?? 0 );
            }
            $base = (float) $order->get_meta( '_senderzz_offer_value', true );
            if ( $base <= 0 ) $base = $gross;
            $commission_gross = $pct > 0 ? round( max( 0, $base ) * max( 0, min( 100, $pct ) ) / 100, 2 ) : 0.0;
        }
    }

    $affiliate_tx_fee = sz_app_pwa_affiliate_transaction_fee_amount( $order, $gross, $commission_gross );
    $producer_fee_responsibility = max( 0, round( $fees - $affiliate_tx_fee, 2 ) );
    return round( max( 0, $gross - $commission_gross - $producer_fee_responsibility ), 2 );
}


function sz_app_pwa_admin_commission_total_amount( WC_Order $order ): float {
    $total = (float) $order->get_total();
    $fees = (float) $order->get_meta( '_sz_aff_fees', true );
    if ( $fees <= 0 ) $fees = sz_app_pwa_order_fee_total( $order );
    $motoboy_fee = (float) get_option( 'sz_admin_motoboy_fee', 18 );
    $fund_fee = (float) get_option( 'sz_admin_operational_fund_fee', 2 );
    return round( max( 0, $total - $fees - $motoboy_fee - $fund_fee ), 2 );
}

function sz_app_pwa_admin_commission_individual_amount( WC_Order $order ): float {
    return round( sz_app_pwa_admin_commission_total_amount( $order ) / 2, 2 );
}

function sz_app_pwa_is_affiliate_recipient( WC_Order $order, int $recipient_user_id ): bool {
    return $recipient_user_id > 0 && $recipient_user_id === sz_app_pwa_affiliate_wp_user_id_for_order( $order );
}

function sz_app_pwa_apply_template( string $event, WC_Order $order, string $field, int $recipient_user_id = 0, string $recipient_type = '' ): string {
    $tpls = sz_app_pwa_get_notification_templates();
    $tpl = (array) ( $tpls[ $event ] ?? [] );
    if ( $recipient_type === 'admin' ) {
        $role_prefix = 'admin_';
    } elseif ( $recipient_type === 'affiliate' ) {
        $role_prefix = 'affiliate_';
    } elseif ( $recipient_type === 'producer' ) {
        $role_prefix = 'producer_';
    } elseif ( function_exists( 'sz_app_pwa_is_admin_recipient_for_event' ) && sz_app_pwa_is_admin_recipient_for_event( $event, $recipient_user_id ) ) {
        $role_prefix = 'admin_';
    } else {
        $role_prefix = sz_app_pwa_is_affiliate_recipient( $order, $recipient_user_id ) ? 'affiliate_' : 'producer_';
    }
    $role_key = rtrim( (string) $role_prefix, '_' );
    $template = (string) ( $tpl[ $role_prefix . $field ] ?? '' );
    if ( $template === '' && $field === 'title' ) $template = (string) ( $tpl['title'] ?? '' );
    if ( $template === '' && $field === 'body' ) $template = (string) ( $tpl['body'] ?? '' );
    if ( $template === '' ) return '';
    $template = sz_app_pwa_restrict_template_variables( $template, $role_key );

    $shipping       = (float) $order->get_shipping_total() + (float) $order->get_shipping_tax();
    $total          = (float) $order->get_total();
    $fee_plus_pct   = sz_app_pwa_order_fee_total( $order );
    $aff_commission = sz_app_pwa_affiliate_commission_amount( $order );
    $producer_final = sz_app_pwa_producer_commission_amount( $order );
    $admin_total = sz_app_pwa_admin_commission_total_amount( $order );
    $admin_individual = sz_app_pwa_admin_commission_individual_amount( $order );
    $motoboy_fee = (float) get_option( 'sz_admin_motoboy_fee', 18 );
    $fund_fee = (float) get_option( 'sz_admin_operational_fund_fee', 2 );
    $context_commission = ( $recipient_type === 'affiliate' || ( $recipient_type === '' && sz_app_pwa_is_affiliate_recipient( $order, $recipient_user_id ) ) ) ? $aff_commission : $producer_final;
    $order_number   = method_exists( $order, 'get_order_number' ) ? (string) $order->get_order_number() : (string) $order->get_id();
    $shipping_label = method_exists( $order, 'get_shipping_method' ) ? (string) $order->get_shipping_method() : '';
    $products_label = sz_app_pwa_order_products_label( $order );


    $vars = [
        '{{numero_pedido}}'                   => '#' . $order_number,
        '{{pedido_id}}'                       => '#' . $order_number,
        '{{cliente}}'                         => trim( $order->get_billing_first_name() . ' ' . $order->get_billing_last_name() ),
        '{{valor_envio}}'                     => sz_app_pwa_money( $shipping ),
        '{{taxa_entrega_percentual_admin}}'   => sz_app_pwa_money( $fee_plus_pct ),
        '{{valor_total_pedido}}'              => sz_app_pwa_money( $total ),
        '{{total}}'                           => sz_app_pwa_money( $total ),
        '{{cidade}}'                          => $order->get_billing_city(),
        '{{produto}}'                         => $products_label,
        '{{produtos}}'                        => $products_label,
        '{{status}}'                          => $order->get_status(),
        '{{transportadora}}'                  => $shipping_label,
        '{{percentual_envio}}'                => $total > 0 ? number_format( ( $fee_plus_pct / $total ) * 100, 2, ',', '.' ) . '%' : '0%',
        '{{comissao_final_afiliado}}'         => sz_app_pwa_money( $aff_commission ),
        '{{comissao_final_produtor}}'         => sz_app_pwa_money( $producer_final ),
        '{{comissao_produtor}}'               => sz_app_pwa_money( $producer_final ),
        '{{comissao_afiliado}}'               => sz_app_pwa_money( $aff_commission ),
        '{{comissao_admin_liquida}}'          => sz_app_pwa_money( $admin_individual ),
        '{{comissao_admin_liquida_total}}'    => sz_app_pwa_money( $admin_total ),
        '{{comissao_admin}}'                  => sz_app_pwa_money( $admin_individual ),
        '{{comissao_admin_total}}'            => sz_app_pwa_money( $admin_total ),
        '{{fundo_operacional}}'               => sz_app_pwa_money( $fund_fee ),
        '{{taxa_motoboy_admin}}'              => sz_app_pwa_money( $motoboy_fee ),
        '{{comissao}}'                        => sz_app_pwa_money( $context_commission ),
        '{{tipo_entrega}}'                    => $shipping_label,
    ];

    $vars = sz_app_pwa_filter_replacements_for_role( $vars, $role_key );
    $out = strtr( $template, $vars );
    if ( $role_key !== 'admin' ) {
        $out = (string) preg_replace( '/\s*·\s*(?=\s*(?:·|$))/', '', $out );
        $out = (string) preg_replace( '/\{\{[a-zA-Z0-9_]+\}\}/', '', $out );
        $out = trim( (string) preg_replace( '/\s{2,}/', ' ', $out ) );
    }

    $flags = sz_app_pwa_get_notification_order_number_flags();
    if ( ! empty( $flags[ $event ] ) && strpos( $out, '#' . $order_number ) === false ) {
        $out = 'Pedido #' . $order_number . ' · ' . $out;
    }

    return $out;
}
