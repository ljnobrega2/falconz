<?php
/**
 * Senderzz — Módulo Motoboy
 * Rotas: registra URLs públicas do PWA e tracking
 */
if ( ! defined( 'ABSPATH' ) ) exit;

/**
 * Service Worker na raiz sem depender de rewrite flush.
 * Em alguns ambientes o /sw-motoboy.js retorna 404 até salvar permalinks;
 * servir cedo por REQUEST_URI mantém o push ativo após atualização do plugin.
 */
function sz_motoboy_output_service_worker(): void {
    header( 'Content-Type: application/javascript; charset=utf-8' );
    header( 'Service-Worker-Allowed: /' );
    header( 'Cache-Control: no-cache, no-store, must-revalidate' );
    $sw_path = TPC_PATH . 'assets/sw-motoboy.js';
    if ( file_exists( $sw_path ) ) {
        readfile( $sw_path );
    } else {
        echo "self.addEventListener('fetch',function(e){e.respondWith(fetch(e.request));});
";
        echo "self.addEventListener('push',function(e){var d={title:'Senderzz',body:'Nova notificacao.'};try{d=e.data.json();}catch(err){}e.waitUntil(self.registration.showNotification(d.title,{body:d.body,icon:'/wp-content/plugins/senderzz/assets/icon-192.png',tag:'sz-motoboy-push',renotify:true}));});
";
        echo "self.addEventListener('notificationclick',function(e){e.notification.close();e.waitUntil(clients.openWindow('/motoboy-app/'));});
";
    }
}

add_action( 'init', function() {
    $path = parse_url( $_SERVER['REQUEST_URI'] ?? '', PHP_URL_PATH );
    if ( $path === '/sw-motoboy.js' ) {
        sz_motoboy_output_service_worker();
        exit;
    }
}, 0 );

add_action( 'init', function() {
    add_rewrite_rule( '^motoboy-app/?$', 'index.php?sz_motoboy_pwa=1', 'top' );
    add_rewrite_rule( '^rastreio-motoboy/?$', 'index.php?sz_motoboy_tracking=1', 'top' );
    add_rewrite_tag( '%sz_motoboy_pwa%', '([0-9]+)' );
    add_rewrite_tag( '%sz_motoboy_tracking%', '([0-9]+)' );
    // Serve SW via PHP em /sw-motoboy.js — evita 404 quando Hostinger bloqueia estáticos
    add_rewrite_rule( '^sw-motoboy\.js$', 'index.php?sz_motoboy_sw=1', 'top' );
    add_rewrite_tag( '%sz_motoboy_sw%', '([0-9]+)' );

    // Auto-migration: roda create_tables sempre que a versão do DB divergir
    // Garante que colunas novas (pin_hash, dest_complemento, etc.) sejam criadas
    // mesmo quando o plugin é atualizado sem desativar/reativar
    if ( get_option( 'sz_motoboy_db_version' ) !== SZ_MOTOBOY_DB_VERSION ) {
        sz_motoboy_create_tables();
        flush_rewrite_rules();
    }
} );

add_action( 'template_redirect', function() {
    if ( get_query_var('sz_motoboy_pwa') ) {
        include TPC_PATH . 'templates/motoboy/pwa.php';
        exit;
    }
    if ( get_query_var('sz_motoboy_tracking') ) {
        include TPC_PATH . 'templates/motoboy/tracking.php';
        exit;
    }
    if ( get_query_var('sz_motoboy_sw') ) {
        sz_motoboy_output_service_worker();
        exit;
    }
} );

// Flush rewrite rules na ativação
register_activation_hook( TPC_PATH . 'senderzz-logistics.php', function() {
    sz_motoboy_create_tables();
    flush_rewrite_rules();
} );

// Flush também após update — garante que /motoboy-app/ funciona após atualizar o plugin
add_action( 'upgrader_process_complete', function( $upgrader, $options ) {
    if (
        isset( $options['type'], $options['plugins'] ) &&
        $options['type'] === 'plugin' &&
        in_array( 'senderzz-logistics/senderzz-logistics.php', (array) $options['plugins'], true )
    ) {
        flush_rewrite_rules();
    }
}, 10, 2 );
