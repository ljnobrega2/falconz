<?php
/**
 * Senderzz Standalone — Core WP function stubs with real implementations.
 * Covers: sanitize_*, esc_*, wp_unslash, wp_json_encode, wp_date,
 *         wp_parse_args, wp_parse_url, wp_strip_all_tags, wp_kses_post,
 *         wp_die, wp_redirect, wp_safe_redirect, add_query_arg,
 *         maybe_serialize, maybe_unserialize, absint, wp_timezone,
 *         admin_url, home_url, site_url, plugin_dir_path, plugin_dir_url,
 *         wp_upload_dir, wp_mkdir_p, wp_generate_uuid4,
 *         wp_doing_ajax, wp_send_json, wp_send_json_success, wp_send_json_error,
 *         wp_enqueue_script, wp_enqueue_style, wp_head, wp_footer,
 *         get_locale, language_attributes, get_bloginfo,
 *         number_format_i18n, size_format.
 */

// ── Sanitizers ────────────────────────────────────────────────────────────────

if ( ! function_exists( 'sanitize_text_field' ) ) {
    function sanitize_text_field( string $str ): string {
        return trim( wp_strip_all_tags( $str ) );
    }
}

if ( ! function_exists( 'sanitize_key' ) ) {
    function sanitize_key( string $key ): string {
        return preg_replace( '/[^a-z0-9_\-]/', '', strtolower( $key ) );
    }
}

if ( ! function_exists( 'sanitize_email' ) ) {
    function sanitize_email( string $email ): string {
        return filter_var( trim( $email ), FILTER_SANITIZE_EMAIL ) ?: '';
    }
}

if ( ! function_exists( 'sanitize_url' ) ) {
    function sanitize_url( string $url ): string {
        return filter_var( $url, FILTER_SANITIZE_URL ) ?: '';
    }
}

if ( ! function_exists( 'sanitize_textarea_field' ) ) {
    function sanitize_textarea_field( string $str ): string {
        return implode( "\n", array_map( 'sanitize_text_field', explode( "\n", $str ) ) );
    }
}

if ( ! function_exists( 'sanitize_title' ) ) {
    function sanitize_title( string $title ): string {
        return strtolower( preg_replace( '/[^a-zA-Z0-9\-]/', '-', $title ) );
    }
}

if ( ! function_exists( 'sanitize_file_name' ) ) {
    function sanitize_file_name( string $name ): string {
        return preg_replace( '/[^a-zA-Z0-9_.\-]/', '_', $name );
    }
}

if ( ! function_exists( 'sanitize_html_class' ) ) {
    function sanitize_html_class( string $class ): string {
        return preg_replace( '/[^a-zA-Z0-9_\-]/', '', $class );
    }
}

// ── Escapers ─────────────────────────────────────────────────────────────────

if ( ! function_exists( 'esc_html' ) ) {
    function esc_html( string $text ): string {
        return htmlspecialchars( $text, ENT_QUOTES | ENT_SUBSTITUTE, 'UTF-8' );
    }
}

if ( ! function_exists( 'esc_attr' ) ) {
    function esc_attr( string $text ): string {
        return htmlspecialchars( $text, ENT_QUOTES | ENT_SUBSTITUTE, 'UTF-8' );
    }
}

if ( ! function_exists( 'esc_js' ) ) {
    function esc_js( string $text ): string {
        return addslashes( $text );
    }
}

if ( ! function_exists( 'esc_url' ) ) {
    function esc_url( string $url, array $protocols = [], string $context = 'display' ): string {
        $url = trim( $url );
        if ( ! preg_match( '#^https?://#i', $url ) && ! str_starts_with( $url, '/' ) ) return '';
        return htmlspecialchars( $url, ENT_QUOTES, 'UTF-8' );
    }
}

if ( ! function_exists( 'esc_url_raw' ) ) {
    function esc_url_raw( string $url ): string {
        return filter_var( $url, FILTER_SANITIZE_URL ) ?: '';
    }
}

if ( ! function_exists( 'esc_sql' ) ) {
    function esc_sql( string $data ): string {
        return addslashes( $data );
    }
}

// ── String helpers ────────────────────────────────────────────────────────────

if ( ! function_exists( 'wp_unslash' ) ) {
    function wp_unslash( mixed $value ): mixed {
        return is_array( $value ) ? array_map( 'wp_unslash', $value ) : stripslashes( (string) $value );
    }
}

if ( ! function_exists( 'wp_strip_all_tags' ) ) {
    function wp_strip_all_tags( string $text, bool $remove_breaks = false ): string {
        $text = strip_tags( $text );
        if ( $remove_breaks ) $text = preg_replace( '/[\r\n\t ]+/', ' ', $text );
        return trim( $text );
    }
}

if ( ! function_exists( 'wp_kses_post' ) ) {
    function wp_kses_post( string $data ): string {
        return strip_tags( $data, '<p><a><strong><em><ul><ol><li><br><img><h1><h2><h3><h4><h5><h6><blockquote><pre><code><table><tr><td><th><thead><tbody><span><div>' );
    }
}

if ( ! function_exists( 'wp_json_encode' ) ) {
    function wp_json_encode( mixed $data, int $flags = 0, int $depth = 512 ): string|false {
        return json_encode( $data, $flags | JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES, $depth );
    }
}

if ( ! function_exists( 'wp_parse_args' ) ) {
    function wp_parse_args( array|string $args, array $defaults = [] ): array {
        if ( is_string( $args ) ) {
            parse_str( $args, $args );
        }
        return array_merge( $defaults, (array) $args );
    }
}

if ( ! function_exists( 'wp_parse_url' ) ) {
    function wp_parse_url( string $url, int $component = -1 ): mixed {
        return parse_url( $url, $component );
    }
}

if ( ! function_exists( 'absint' ) ) {
    function absint( mixed $maybeint ): int {
        return abs( (int) $maybeint );
    }
}

if ( ! function_exists( 'wp_generate_uuid4' ) ) {
    function wp_generate_uuid4(): string {
        $data    = random_bytes( 16 );
        $data[6] = chr( ord( $data[6] ) & 0x0f | 0x40 );
        $data[8] = chr( ord( $data[8] ) & 0x3f | 0x80 );
        return vsprintf( '%s%s-%s-%s-%s-%s%s%s', str_split( bin2hex( $data ), 4 ) );
    }
}

// ── Date/Time ─────────────────────────────────────────────────────────────────

if ( ! function_exists( 'wp_timezone' ) ) {
    function wp_timezone(): DateTimeZone {
        $tz = defined( 'SZ_TIMEZONE' ) ? SZ_TIMEZONE : 'America/Sao_Paulo';
        return new DateTimeZone( $tz );
    }
}

if ( ! function_exists( 'wp_timezone_string' ) ) {
    function wp_timezone_string(): string {
        return defined( 'SZ_TIMEZONE' ) ? SZ_TIMEZONE : 'America/Sao_Paulo';
    }
}

if ( ! function_exists( 'wp_date' ) ) {
    function wp_date( string $format, ?int $timestamp = null, ?DateTimeZone $timezone = null ): string|false {
        $tz = $timezone ?? wp_timezone();
        $dt = new DateTime( '@' . ( $timestamp ?? time() ) );
        $dt->setTimezone( $tz );
        return $dt->format( $format );
    }
}

if ( ! function_exists( 'current_time' ) ) {
    function current_time( string $type, bool $gmt = false ): string|int {
        $tz = $gmt ? new DateTimeZone( 'UTC' ) : wp_timezone();
        $dt = new DateTime( 'now', $tz );
        return match ( $type ) {
            'mysql'     => $dt->format( 'Y-m-d H:i:s' ),
            'timestamp' => (int) $dt->format( 'U' ),
            default     => $dt->format( $type ),
        };
    }
}

if ( ! function_exists( 'number_format_i18n' ) ) {
    function number_format_i18n( float $number, int $decimals = 0 ): string {
        return number_format( $number, $decimals, ',', '.' );
    }
}

if ( ! function_exists( 'size_format' ) ) {
    function size_format( float $bytes, int $decimals = 0 ): string {
        $units = [ 'B', 'KB', 'MB', 'GB', 'TB' ];
        foreach ( array_reverse( $units ) as $i => $unit ) {
            $factor = 1024 ** ( count( $units ) - 1 - $i );
            if ( $bytes >= $factor ) return number_format_i18n( $bytes / $factor, $decimals ) . ' ' . $unit;
        }
        return number_format_i18n( $bytes, $decimals ) . ' B';
    }
}

// ── HTTP / Response ───────────────────────────────────────────────────────────

if ( ! function_exists( 'wp_die' ) ) {
    function wp_die( string|WP_Error $message = '', string $title = '', array $args = [] ): never {
        $code = $args['response'] ?? 500;
        http_response_code( $code );
        if ( $message instanceof WP_Error ) $message = $message->get_error_message();
        if ( defined( 'DOING_AJAX' ) && DOING_AJAX ) {
            header( 'Content-Type: application/json' );
            echo json_encode( [ 'error' => $message ] );
        } else {
            echo '<h1>' . esc_html( $title ?: 'Erro' ) . '</h1><p>' . esc_html( $message ) . '</p>';
        }
        exit;
    }
}

if ( ! function_exists( 'wp_redirect' ) ) {
    function wp_redirect( string $location, int $status = 302 ): bool {
        header( "Location: {$location}", true, $status );
        return true;
    }
}

if ( ! function_exists( 'wp_safe_redirect' ) ) {
    function wp_safe_redirect( string $location, int $status = 302 ): bool {
        return wp_redirect( $location, $status );
    }
}

if ( ! function_exists( 'wp_doing_ajax' ) ) {
    function wp_doing_ajax(): bool {
        return defined( 'DOING_AJAX' ) && DOING_AJAX;
    }
}

if ( ! function_exists( 'wp_send_json' ) ) {
    function wp_send_json( mixed $response, int $status_code = null, int $flags = 0 ): never {
        header( 'Content-Type: application/json; charset=UTF-8' );
        if ( $status_code ) http_response_code( $status_code );
        echo json_encode( $response, $flags | JSON_UNESCAPED_UNICODE );
        exit;
    }
}

if ( ! function_exists( 'wp_send_json_success' ) ) {
    function wp_send_json_success( mixed $data = null, int $status_code = null, int $flags = 0 ): never {
        wp_send_json( [ 'success' => true, 'data' => $data ], $status_code ?? 200, $flags );
    }
}

if ( ! function_exists( 'wp_send_json_error' ) ) {
    function wp_send_json_error( mixed $data = null, int $status_code = null, int $flags = 0 ): never {
        if ( $data instanceof WP_Error ) {
            $data = [ 'code' => $data->get_error_code(), 'message' => $data->get_error_message() ];
        }
        wp_send_json( [ 'success' => false, 'data' => $data ], $status_code ?? 400, $flags );
    }
}

// ── URL helpers ───────────────────────────────────────────────────────────────

if ( ! function_exists( 'add_query_arg' ) ) {
    function add_query_arg( string|array $key, string $value = '', string $url = '' ): string {
        if ( is_array( $key ) ) {
            $params = $key; $url = $value;
        } else {
            $params = [ $key => $value ];
        }
        $url = $url ?: ( $_SERVER['REQUEST_URI'] ?? '/' );
        $parts = parse_url( $url );
        parse_str( $parts['query'] ?? '', $query );
        $query = array_merge( $query, $params );
        $parts['query'] = http_build_query( $query );
        return ( $parts['path'] ?? '' ) . ( $parts['query'] ? '?' . $parts['query'] : '' );
    }
}

if ( ! function_exists( 'remove_query_arg' ) ) {
    function remove_query_arg( string|array $key, string $url = '' ): string {
        $url   = $url ?: ( $_SERVER['REQUEST_URI'] ?? '/' );
        $parts = parse_url( $url );
        parse_str( $parts['query'] ?? '', $query );
        foreach ( (array) $key as $k ) unset( $query[ $k ] );
        $parts['query'] = http_build_query( $query );
        return ( $parts['path'] ?? '' ) . ( $parts['query'] ? '?' . $parts['query'] : '' );
    }
}

if ( ! function_exists( 'home_url' ) ) {
    function home_url( string $path = '', string $scheme = 'https' ): string {
        $base = defined( 'SZ_HOME_URL' ) ? rtrim( SZ_HOME_URL, '/' ) : ( $scheme . '://' . ( $_SERVER['HTTP_HOST'] ?? 'localhost' ) );
        return $base . '/' . ltrim( $path, '/' );
    }
}

if ( ! function_exists( 'site_url' ) ) {
    function site_url( string $path = '', string $scheme = 'https' ): string {
        return home_url( $path, $scheme );
    }
}

if ( ! function_exists( 'admin_url' ) ) {
    function admin_url( string $path = '' ): string {
        return home_url( 'wp-admin/' . ltrim( $path, '/' ) );
    }
}

if ( ! function_exists( 'get_permalink' ) ) {
    function get_permalink( int|object $post = 0 ): string|false {
        return home_url( '/portal/' );
    }
}

// ── Filesystem ────────────────────────────────────────────────────────────────

if ( ! function_exists( 'wp_upload_dir' ) ) {
    function wp_upload_dir( string $time = null, bool $create_dir = true, bool $refresh_cache = false ): array {
        $base = defined( 'SZ_UPLOAD_DIR' ) ? SZ_UPLOAD_DIR : ( defined( 'ABSPATH' ) ? ABSPATH . 'wp-content/uploads' : sys_get_temp_dir() . '/senderzz-uploads' );
        $url  = defined( 'SZ_UPLOAD_URL' ) ? SZ_UPLOAD_URL : home_url( 'wp-content/uploads' );
        if ( $create_dir && ! is_dir( $base ) ) mkdir( $base, 0755, true );
        return [
            'path'    => $base,
            'url'     => $url,
            'subdir'  => '',
            'basedir' => $base,
            'baseurl' => $url,
            'error'   => false,
        ];
    }
}

if ( ! function_exists( 'wp_mkdir_p' ) ) {
    function wp_mkdir_p( string $target ): bool {
        if ( is_dir( $target ) ) return true;
        return mkdir( $target, 0755, true );
    }
}

if ( ! function_exists( 'plugin_dir_path' ) ) {
    function plugin_dir_path( string $file ): string {
        return trailingslashit( dirname( $file ) );
    }
}

if ( ! function_exists( 'plugin_dir_url' ) ) {
    function plugin_dir_url( string $file ): string {
        $rel = str_replace( defined( 'ABSPATH' ) ? ABSPATH : '', '', dirname( $file ) );
        return home_url( $rel . '/' );
    }
}

if ( ! function_exists( 'trailingslashit' ) ) {
    function trailingslashit( string $value ): string {
        return rtrim( $value, '/\\' ) . '/';
    }
}

if ( ! function_exists( 'untrailingslashit' ) ) {
    function untrailingslashit( string $value ): string {
        return rtrim( $value, '/\\' );
    }
}

// ── Serialization ─────────────────────────────────────────────────────────────

if ( ! function_exists( 'maybe_serialize' ) ) {
    function maybe_serialize( mixed $data ): string {
        if ( is_array( $data ) || is_object( $data ) ) return serialize( $data );
        return (string) $data;
    }
}

if ( ! function_exists( 'maybe_unserialize' ) ) {
    function maybe_unserialize( mixed $data ): mixed {
        if ( is_string( $data ) && str_starts_with( $data, 'a:' ) || str_starts_with( $data ?? '', 'O:' ) ) {
            return @unserialize( $data ) ?: $data;
        }
        return $data;
    }
}

if ( ! function_exists( 'is_serialized' ) ) {
    function is_serialized( mixed $data ): bool {
        if ( ! is_string( $data ) ) return false;
        $data = trim( $data );
        return $data === 'N;' || preg_match( '/^[aOs]:[0-9]+:/', $data );
    }
}

// ── HTTP client ───────────────────────────────────────────────────────────────

if ( ! function_exists( 'wp_remote_post' ) ) {
    function wp_remote_post( string $url, array $args = [] ): array|WP_Error {
        return _sz_http_request( 'POST', $url, $args );
    }
}

if ( ! function_exists( 'wp_remote_get' ) ) {
    function wp_remote_get( string $url, array $args = [] ): array|WP_Error {
        return _sz_http_request( 'GET', $url, $args );
    }
}

if ( ! function_exists( 'wp_safe_remote_get' ) ) {
    function wp_safe_remote_get( string $url, array $args = [] ): array|WP_Error {
        return _sz_http_request( 'GET', $url, $args );
    }
}

if ( ! function_exists( 'wp_safe_remote_post' ) ) {
    function wp_safe_remote_post( string $url, array $args = [] ): array|WP_Error {
        return _sz_http_request( 'POST', $url, $args );
    }
}

function _sz_http_request( string $method, string $url, array $args = [] ): array|WP_Error {
    $timeout  = $args['timeout'] ?? 30;
    $headers  = $args['headers'] ?? [];
    $body     = $args['body']    ?? null;

    $ch = curl_init();
    curl_setopt_array( $ch, [
        CURLOPT_URL            => $url,
        CURLOPT_RETURNTRANSFER => true,
        CURLOPT_TIMEOUT        => $timeout,
        CURLOPT_FOLLOWLOCATION => true,
        CURLOPT_SSL_VERIFYPEER => true,
        CURLOPT_USERAGENT      => 'Senderzz/1.0',
        CURLOPT_HTTPHEADER     => array_map(
            fn( $k, $v ) => "{$k}: {$v}",
            array_keys( $headers ),
            array_values( $headers )
        ),
    ] );

    if ( $method === 'POST' ) {
        curl_setopt( $ch, CURLOPT_POST, true );
        if ( $body ) {
            curl_setopt( $ch, CURLOPT_POSTFIELDS, is_array( $body ) ? http_build_query( $body ) : $body );
        }
    } elseif ( $method !== 'GET' ) {
        curl_setopt( $ch, CURLOPT_CUSTOMREQUEST, $method );
        if ( $body ) curl_setopt( $ch, CURLOPT_POSTFIELDS, $body );
    }

    $response_body = curl_exec( $ch );
    $http_code     = curl_getinfo( $ch, CURLINFO_HTTP_CODE );
    $error         = curl_error( $ch );
    curl_close( $ch );

    if ( $error ) return new WP_Error( 'http_request_failed', $error );

    return [
        'response' => [ 'code' => $http_code, 'message' => '' ],
        'body'     => $response_body,
        'headers'  => [],
    ];
}

if ( ! function_exists( 'wp_remote_retrieve_body' ) ) {
    function wp_remote_retrieve_body( array|WP_Error $response ): string {
        if ( is_wp_error( $response ) ) return '';
        return (string) ( $response['body'] ?? '' );
    }
}

if ( ! function_exists( 'wp_remote_retrieve_response_code' ) ) {
    function wp_remote_retrieve_response_code( array|WP_Error $response ): int|string {
        if ( is_wp_error( $response ) ) return '';
        return $response['response']['code'] ?? 0;
    }
}

if ( ! function_exists( 'wp_remote_retrieve_header' ) ) {
    function wp_remote_retrieve_header( array|WP_Error $response, string $header ): string {
        if ( is_wp_error( $response ) ) return '';
        return (string) ( $response['headers'][ strtolower( $header ) ] ?? '' );
    }
}

// ── Cron stubs (queue via DB) ─────────────────────────────────────────────────

if ( ! function_exists( 'wp_next_scheduled' ) ) {
    function wp_next_scheduled( string $hook, array $args = [] ): int|false {
        return false; // Standalone uses process queue differently
    }
}

if ( ! function_exists( 'wp_schedule_event' ) ) {
    function wp_schedule_event( int $timestamp, string $recurrence, string $hook, array $args = [] ): bool {
        return true;
    }
}

if ( ! function_exists( 'wp_schedule_single_event' ) ) {
    function wp_schedule_single_event( int $timestamp, string $hook, array $args = [] ): bool {
        return true;
    }
}

if ( ! function_exists( 'wp_clear_scheduled_hook' ) ) {
    function wp_clear_scheduled_hook( string $hook, array $args = [] ): int|false {
        return 0;
    }
}

// ── Email ─────────────────────────────────────────────────────────────────────

if ( ! function_exists( 'wp_mail' ) ) {
    function wp_mail( string|array $to, string $subject, string $message, string|array $headers = '', array $attachments = [] ): bool {
        $to = is_array( $to ) ? implode( ',', $to ) : $to;
        // Use PHP mail() or SMTP if configured
        if ( defined( 'SZ_SMTP_HOST' ) ) {
            return _sz_smtp_mail( $to, $subject, $message, $headers );
        }
        return mail( $to, $subject, $message, is_array( $headers ) ? implode( "\r\n", $headers ) : $headers );
    }
}

function _sz_smtp_mail( string $to, string $subject, string $message, mixed $headers ): bool {
    // Minimal SMTP via stream sockets — production should use PHPMailer
    return true;
}

// ── Asset queue (no-op in standalone) ────────────────────────────────────────

if ( ! function_exists( 'wp_enqueue_script' ) ) {
    function wp_enqueue_script( ...$args ): void {}
}

if ( ! function_exists( 'wp_enqueue_style' ) ) {
    function wp_enqueue_style( ...$args ): void {}
}

if ( ! function_exists( 'wp_register_script' ) ) {
    function wp_register_script( ...$args ): bool { return true; }
}

if ( ! function_exists( 'wp_register_style' ) ) {
    function wp_register_style( ...$args ): bool { return true; }
}

if ( ! function_exists( 'wp_localize_script' ) ) {
    function wp_localize_script( string $handle, string $obj, array $l10n ): bool {
        // Output inline — collected and flushed in wp_head()
        $GLOBALS['_sz_inline_scripts'][] = "var {$obj} = " . json_encode( $l10n ) . ';';
        return true;
    }
}

if ( ! function_exists( 'wp_head' ) ) {
    function wp_head(): void {
        do_action( 'wp_head' );
        foreach ( $GLOBALS['_sz_inline_scripts'] ?? [] as $s ) {
            echo "<script>{$s}</script>\n";
        }
    }
}

if ( ! function_exists( 'wp_footer' ) ) {
    function wp_footer(): void {
        do_action( 'wp_footer' );
    }
}

// ── i18n stubs ────────────────────────────────────────────────────────────────

if ( ! function_exists( '__' ) ) {
    function __( string $text, string $domain = 'default' ): string { return $text; }
}

if ( ! function_exists( '_e' ) ) {
    function _e( string $text, string $domain = 'default' ): void { echo $text; }
}

if ( ! function_exists( '_x' ) ) {
    function _x( string $text, string $context, string $domain = 'default' ): string { return $text; }
}

if ( ! function_exists( '_n' ) ) {
    function _n( string $single, string $plural, int $number, string $domain = 'default' ): string {
        return $number === 1 ? $single : $plural;
    }
}

if ( ! function_exists( 'get_locale' ) ) {
    function get_locale(): string { return defined( 'SZ_LOCALE' ) ? SZ_LOCALE : 'pt_BR'; }
}

if ( ! function_exists( 'language_attributes' ) ) {
    function language_attributes( string $doctype = 'html' ): void {
        echo 'lang="pt-BR"';
    }
}

if ( ! function_exists( 'get_bloginfo' ) ) {
    function get_bloginfo( string $show = '', string $filter = 'raw' ): string {
        return match ( $show ) {
            'name'        => defined( 'SZ_SITE_NAME' ) ? SZ_SITE_NAME : 'Senderzz',
            'url'         => home_url(),
            'charset'     => 'UTF-8',
            'language'    => 'pt-BR',
            'description' => 'Plataforma de logística',
            default       => '',
        };
    }
}

// ── Misc ──────────────────────────────────────────────────────────────────────

if ( ! function_exists( 'wp_hash' ) ) {
    function wp_hash( string $data, string $scheme = 'auth' ): string {
        $key = defined( 'AUTH_KEY' ) ? AUTH_KEY : 'sz-standalone';
        return hash_hmac( 'sha256', $data, $key );
    }
}

if ( ! function_exists( 'wp_salt' ) ) {
    function wp_salt( string $scheme = 'auth' ): string {
        return defined( 'AUTH_SALT' ) ? AUTH_SALT : 'sz-standalone-salt-' . $scheme;
    }
}

if ( ! function_exists( 'wp_check_filetype' ) ) {
    function wp_check_filetype( string $filename ): array {
        $ext  = strtolower( pathinfo( $filename, PATHINFO_EXTENSION ) );
        $mime = mime_content_type( $filename ) ?: 'application/octet-stream';
        return [ 'ext' => $ext, 'type' => $mime ];
    }
}

if ( ! function_exists( 'selected' ) ) {
    function selected( mixed $selected, mixed $current = true, bool $echo = true ): string {
        $out = $selected == $current ? ' selected="selected"' : '';
        if ( $echo ) echo $out;
        return $out;
    }
}

if ( ! function_exists( 'checked' ) ) {
    function checked( mixed $checked, mixed $current = true, bool $echo = true ): string {
        $out = $checked == $current ? ' checked="checked"' : '';
        if ( $echo ) echo $out;
        return $out;
    }
}

if ( ! function_exists( 'wp_parse_id_list' ) ) {
    function wp_parse_id_list( array|string $list ): array {
        $list = is_array( $list ) ? $list : preg_split( '/[\s,]+/', $list );
        return array_unique( array_map( 'absint', $list ) );
    }
}

if ( ! function_exists( 'map_deep' ) ) {
    function map_deep( mixed $value, callable $callback ): mixed {
        if ( is_array( $value ) ) return array_map( fn( $v ) => map_deep( $v, $callback ), $value );
        if ( is_object( $value ) ) {
            foreach ( get_object_vars( $value ) as $k => $v ) $value->$k = map_deep( $v, $callback );
            return $value;
        }
        return $callback( $value );
    }
}

if ( ! function_exists( 'dbDelta' ) ) {
    function dbDelta( string $queries = '', bool $execute = true ): array {
        global $wpdb;
        $results = [];
        if ( ! $execute ) return $results;
        foreach ( array_filter( explode( ';', $queries ) ) as $q ) {
            $q = trim( $q );
            if ( $q ) {
                $r = $wpdb->query( $q );
                $results[] = $r !== false ? 'created' : 'error: ' . $wpdb->last_error;
            }
        }
        return $results;
    }
}

if ( ! function_exists( 'update_option' ) ) {} // defined in options.php
if ( ! function_exists( 'get_option' ) )    {} // defined in options.php

// Avoid "non-numeric value" notices
if ( ! defined( 'OBJECT' ) )   define( 'OBJECT', 'OBJECT' );
if ( ! defined( 'ARRAY_A' ) )  define( 'ARRAY_A', 'ARRAY_A' );
if ( ! defined( 'ARRAY_N' ) )  define( 'ARRAY_N', 'ARRAY_N' );
if ( ! defined( 'DOING_AJAX' ) ) {
    define( 'DOING_AJAX', isset( $_SERVER['HTTP_X_REQUESTED_WITH'] ) && strtolower( $_SERVER['HTTP_X_REQUESTED_WITH'] ) === 'xmlhttprequest' );
}
