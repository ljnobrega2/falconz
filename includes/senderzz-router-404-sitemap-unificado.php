<?php
/**
 * Plugin Name: Senderzz Router + 404 Limpo + Ocultar Sitemap
 * Description: Unifica whitelist de rotas públicas, 404 limpo e bloqueio/remoção de sitemaps XML/robots.txt do WordPress.
 * Version: 1.0.0
 * Author: Senderzz
 */

if (!defined('ABSPATH')) {
    exit;
}

if (!function_exists('senderzz_unificado_clean_404_exit')) {
    function senderzz_unificado_clean_404_exit(): void {
        if (!headers_sent()) {
            status_header(404);
            nocache_headers();
            header('X-Robots-Tag: noindex, nofollow, noarchive', true);
            header('Content-Type: text/html; charset=utf-8', true, 404);
        }

        echo '<!doctype html><html lang="pt-BR"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>404</title><style>html,body{margin:0;padding:0;width:100%;height:100%;background:#000;color:#fff;font-family:var(--sz-font)}body{display:flex;align-items:center;justify-content:center}h1{margin:0;font-size:var(--sz-text-hero);font-weight:700;letter-spacing:.02em;opacity:.9}</style></head><body><h1>404</h1></body></html>';
        exit;
    }
}

if (!function_exists('senderzz_unificado_get_path')) {
    function senderzz_unificado_get_path(): string {
        $request_uri = $_SERVER['REQUEST_URI'] ?? '/';
        $path = parse_url((string) $request_uri, PHP_URL_PATH);
        $path = '/' . trim((string) $path, '/');

        return $path === '/' ? '/' : rtrim($path, '/');
    }
}

if (!function_exists('senderzz_unificado_path_starts_with')) {
    function senderzz_unificado_path_starts_with(string $path, string $prefix): bool {
        $prefix = rtrim($prefix, '/');

        return $path === $prefix || stripos($path, $prefix . '/') === 0;
    }
}

if (!function_exists('senderzz_unificado_is_sitemap_path')) {
    function senderzz_unificado_is_sitemap_path(string $path): bool {
        $filename = basename($path);

        return (bool) preg_match('/^(wp-sitemap.*\.xml|sitemap.*\.xml|.*-sitemap.*\.xml)$/i', $filename);
    }
}


// Atalho administrativo legado: /wp-admin/senderzz-cod-finance
// Redireciona para a aba unificada sem cair no 404 limpo.
add_action('init', function () {
    $path = senderzz_unificado_get_path();
    if ($path === '/wp-admin/senderzz-cod-finance' || $path === '/wp-admin/senderzz-cod-finance/') {
        if (is_user_logged_in() && current_user_can('manage_woocommerce')) {
            wp_safe_redirect(admin_url('admin.php?page=senderzz&tab=financeiro-cod'));
        } else {
            wp_safe_redirect(wp_login_url(admin_url('admin.php?page=senderzz&tab=financeiro-cod')));
        }
        exit;
    }
}, 0);

// Desativa o sitemap nativo do WordPress.
add_filter('wp_sitemaps_enabled', '__return_false', 9999);

// Remove referências de sitemap do robots.txt virtual do WordPress.
add_filter('robots_txt', function ($output) {
    $lines = preg_split('/\r\n|\r|\n/', (string) $output);
    $filtered = array();

    foreach ($lines as $line) {
        if (stripos(trim((string) $line), 'Sitemap:') === 0) {
            continue;
        }
        $filtered[] = $line;
    }

    return implode(PHP_EOL, $filtered);
}, 20);

// Bloqueia sitemaps antes do tema carregar.
add_action('init', function () {
    $path = senderzz_unificado_get_path();

    if (senderzz_unificado_is_sitemap_path($path)) {
        senderzz_unificado_clean_404_exit();
    }
}, 0);

// Roteador por whitelist + 404 limpo.
add_action('template_redirect', function () {
    $path = senderzz_unificado_get_path();

    // Bloqueia acesso direto a sitemap também nesta fase, por segurança.
    if (senderzz_unificado_is_sitemap_path($path)) {
        senderzz_unificado_clean_404_exit();
    }

    // Libera administração, login, AJAX, REST e assets para não quebrar WordPress/plugins.
    $allowed_system_prefixes = array(
        '/wp-admin',
        '/wp-login.php',
        '/wp-content',
        '/wp-includes',
        '/wp-json',
        '/xmlrpc.php',
        '/admin-ajax.php',
    );

    foreach ($allowed_system_prefixes as $prefix) {
        if (senderzz_unificado_path_starts_with($path, $prefix)) {
            return;
        }
    }

    // Libera somente páginas/rotas públicas autorizadas do Senderzz.
    $allowed_public_prefixes = array(
        '/',
        '/meus-pedidos',
        '/rastreio',
        '/sz-4f9d2e8a1c7b',
        '/checkouts',
        '/checkout',
        '/convite',
        '/order-confirmed',
        '/motoboy-app',
        '/rastreio-motoboy',
        '/sw-motoboy.js',
    );

    foreach ($allowed_public_prefixes as $prefix) {
        if ($prefix === '/' && $path === '/') {
            return;
        }

        if ($prefix !== '/' && senderzz_unificado_path_starts_with($path, $prefix)) {
            return;
        }
    }

    // Qualquer rota pública fora da whitelist vira 404 limpo.
    senderzz_unificado_clean_404_exit();
}, 0);

// Garante que qualquer 404 gerado pelo WordPress também saia limpo.
add_filter('template_include', function ($template) {
    if (is_404()) {
        senderzz_unificado_clean_404_exit();
    }

    return $template;
}, 0);

// Legacy: /afiliado-app/ foi eliminado. Afiliado usa o mesmo portal do produtor.
// Redireciona qualquer link antigo (/afiliado-app ou /afiliado-app/*) para o portal principal, preservando token de senha.
add_action( 'template_redirect', function() {
    $path = trim( (string) parse_url( $_SERVER['REQUEST_URI'] ?? '', PHP_URL_PATH ), '/' );
    if ( $path === 'afiliado-app' || strpos( $path, 'afiliado-app/' ) === 0 ) {
        $portal_page_id = absint( get_option( 'senderzz_portal_page_id' ) );
        $target = $portal_page_id ? get_permalink( $portal_page_id ) : home_url( '/meus-pedidos/' );
        if ( isset( $_GET['sz_reset'] ) ) {
            $target = add_query_arg( 'sz_reset', sanitize_text_field( wp_unslash( $_GET['sz_reset'] ) ), $target );
        }
        wp_safe_redirect( $target );
        exit;
    }
}, -1 );
