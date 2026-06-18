<?php
/**
 * Senderzz Standalone — HTTP Entry Point
 *
 * Nginx/Apache rewrites all requests here.
 * Routes:
 *   /wp-json/*          → REST API dispatcher
 *   /wp-admin/admin-ajax.php → AJAX dispatcher
 *   /portal             → Portal V2 dashboard
 *   /rastreio-motoboy/* → Public motoboy tracking
 *   /motoboy-app/*      → Motoboy PWA
 *   /checkout/*         → Checkout canvas
 */

define( 'SZ_PUBLIC_DIR', __DIR__ );

require_once dirname( __DIR__ ) . '/bootstrap/bootstrap.php';

// Plugin bootstrap (loads all includes, hooks, REST routes)
require_once dirname( __DIR__ ) . '/senderzz-logistics.php';

$uri = strtok( $_SERVER['REQUEST_URI'] ?? '/', '?' );

// ── REST API ─────────────────────────────────────────────────────────────────
if ( preg_match( '#^/wp-json/#', $uri ) ) {
    SZ_Router::dispatch();
}

// ── AJAX (admin-ajax.php equivalent) ─────────────────────────────────────────
if ( $uri === '/wp-admin/admin-ajax.php' || $uri === '/ajax' ) {
    $action = $_REQUEST['action'] ?? '';
    if ( ! $action ) {
        wp_send_json_error( 'No action', 400 );
    }
    do_action( "wp_ajax_{$action}" );
    do_action( "wp_ajax_nopriv_{$action}" );
    wp_die( 'Invalid action', '', [ 'response' => 400 ] );
}

// ── Static assets ─────────────────────────────────────────────────────────────
if ( preg_match( '#\.(css|js|png|jpg|jpeg|gif|svg|woff2?|ttf|ico)$#i', $uri ) ) {
    $file = dirname( __DIR__ ) . $uri;
    if ( file_exists( $file ) ) {
        $ext  = strtolower( pathinfo( $file, PATHINFO_EXTENSION ) );
        $mime = [
            'css'   => 'text/css',
            'js'    => 'application/javascript',
            'png'   => 'image/png',
            'jpg'   => 'image/jpeg',
            'jpeg'  => 'image/jpeg',
            'gif'   => 'image/gif',
            'svg'   => 'image/svg+xml',
            'woff'  => 'font/woff',
            'woff2' => 'font/woff2',
            'ttf'   => 'font/ttf',
            'ico'   => 'image/x-icon',
        ][ $ext ] ?? 'application/octet-stream';
        header( "Content-Type: {$mime}" );
        header( 'Cache-Control: public, max-age=31536000' );
        readfile( $file );
        exit;
    }
}

// ── Portal V2 ─────────────────────────────────────────────────────────────────
if ( preg_match( '#^/portal#', $uri ) || $uri === '/' ) {
    // Render portal (Portal_Page handles auth redirect internally)
    $page_class = '\WC_MelhorEnvio\Portal\Portal_Page';
    if ( class_exists( $page_class ) ) {
        echo ( new $page_class() )->render_portal();
    } else {
        // Direct include fallback
        $template = dirname( __DIR__ ) . '/templates/portal/v2/dashboard-v2.php';
        if ( file_exists( $template ) ) {
            // Need a minimal $sz_v2_user
            $sz_v2_user = function_exists( 'sz_portal_auth_user' ) ? sz_portal_auth_user() : null;
            if ( ! $sz_v2_user && ! isset( $_GET['sz_register'] ) ) {
                // Show login
                http_response_code( 200 );
                echo ( new \WC_MelhorEnvio\Portal\Portal_Page() )->render_login_page();
                exit;
            }
            require $template;
        } else {
            http_response_code( 503 );
            echo '<h1>Portal indisponível</h1>';
        }
    }
    exit;
}

// ── Motoboy PWA ───────────────────────────────────────────────────────────────
if ( preg_match( '#^/motoboy-app#', $uri ) ) {
    $template = dirname( __DIR__ ) . '/templates/motoboy/pwa.php';
    if ( file_exists( $template ) ) { require $template; } else { http_response_code( 404 ); }
    exit;
}

// ── Public tracking ───────────────────────────────────────────────────────────
if ( preg_match( '#^/rastreio-motoboy#', $uri ) ) {
    $template = dirname( __DIR__ ) . '/templates/tracking.php';
    if ( file_exists( $template ) ) { require $template; } else { http_response_code( 404 ); }
    exit;
}

// ── Checkout ──────────────────────────────────────────────────────────────────
if ( preg_match( '#^/checkout#', $uri ) ) {
    $template = dirname( __DIR__ ) . '/templates/public-checkout-canvas.php';
    if ( file_exists( $template ) ) { require $template; } else { http_response_code( 404 ); }
    exit;
}

// ── 404 ───────────────────────────────────────────────────────────────────────
http_response_code( 404 );
echo '<h1>404 — Página não encontrada</h1>';
exit;
