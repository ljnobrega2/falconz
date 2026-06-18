<?php
/**
 * Senderzz Standalone — Auth layer.
 * Implements nonces (HMAC-SHA256), password hashing (bcrypt), user sessions.
 */

if ( function_exists( 'wp_create_nonce' ) ) return;

// ── Nonce ─────────────────────────────────────────────────────────────────────

function wp_create_nonce( string $action = -1 ): string {
    $uid  = sz_get_current_user_id();
    $tick = (int) ( time() / 43200 ); // 12h tick
    $key  = defined( 'AUTH_KEY' ) ? AUTH_KEY : ( defined( 'SZ_AUTH_KEY' ) ? SZ_AUTH_KEY : 'sz-standalone-auth-key' );
    return substr( hash_hmac( 'sha256', "{$tick}|{$uid}|{$action}", $key ), -12 );
}

function wp_verify_nonce( string $nonce, string $action = -1 ): int|false {
    $uid = sz_get_current_user_id();
    $key = defined( 'AUTH_KEY' ) ? AUTH_KEY : ( defined( 'SZ_AUTH_KEY' ) ? SZ_AUTH_KEY : 'sz-standalone-auth-key' );
    for ( $i = 0; $i <= 1; $i++ ) {
        $tick     = (int) ( time() / 43200 ) - $i;
        $expected = substr( hash_hmac( 'sha256', "{$tick}|{$uid}|{$action}", $key ), -12 );
        if ( hash_equals( $expected, $nonce ) ) {
            return 2 - $i; // 2 = current tick, 1 = previous
        }
    }
    return false;
}

function wp_nonce_field( string $action = -1, string $name = '_wpnonce', bool $referer = true, bool $echo = true ): string {
    $nonce = wp_create_nonce( $action );
    $out   = '<input type="hidden" id="' . esc_attr( $name ) . '" name="' . esc_attr( $name ) . '" value="' . esc_attr( $nonce ) . '">';
    if ( $echo ) echo $out;
    return $out;
}

function wp_nonce_url( string $url, string $action = -1, string $name = '_wpnonce' ): string {
    return add_query_arg( $name, wp_create_nonce( $action ), $url );
}

// ── Password ──────────────────────────────────────────────────────────────────

function wp_hash_password( string $password ): string {
    return password_hash( $password, PASSWORD_BCRYPT, [ 'cost' => 12 ] );
}

function wp_check_password( string $password, string $hash, int|string $user_id = '' ): bool {
    // Support old WP phpass hashes starting with $P$
    if ( str_starts_with( $hash, '$P$' ) || str_starts_with( $hash, '$H$' ) ) {
        // Fallback: require wp-includes/class-phpass.php if WP is partially available
        if ( class_exists( 'PasswordHash' ) ) {
            $hasher = new PasswordHash( 8, true );
            return $hasher->CheckPassword( $password, $hash );
        }
        return false;
    }
    return password_verify( $password, $hash );
}

function wp_generate_password( int $length = 12, bool $special_chars = true, bool $extra_special_chars = false ): string {
    $chars = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789';
    if ( $special_chars ) $chars .= '!@#$%^&*()';
    if ( $extra_special_chars ) $chars .= '-_ []{}<>~`+=,.;:/?|';
    $pass = '';
    for ( $i = 0; $i < $length; $i++ ) {
        $pass .= $chars[ random_int( 0, strlen( $chars ) - 1 ) ];
    }
    return $pass;
}

// ── User session ──────────────────────────────────────────────────────────────

function sz_get_current_user_id(): int {
    return $GLOBALS['_sz_current_user_id'] ?? 0;
}

function sz_set_current_user( int $id ): void {
    $GLOBALS['_sz_current_user_id'] = $id;
}

function get_current_user_id(): int {
    return sz_get_current_user_id();
}

function wp_get_current_user(): object {
    global $wpdb;
    $id = sz_get_current_user_id();
    if ( ! $id ) return (object) [ 'ID' => 0, 'user_login' => '', 'user_email' => '', 'user_pass' => '', 'roles' => [] ];
    static $cache = [];
    if ( isset( $cache[ $id ] ) ) return $cache[ $id ];
    $user = $wpdb->get_row( $wpdb->prepare( "SELECT * FROM {$wpdb->prefix}users WHERE ID = %d LIMIT 1", $id ) );
    if ( ! $user ) return (object) [ 'ID' => 0 ];
    $cache[ $id ] = $user;
    return $user;
}

function is_user_logged_in(): bool {
    return sz_get_current_user_id() > 0;
}

function user_can( int|object $user, string $cap ): bool {
    $id = is_object( $user ) ? (int) $user->ID : $user;
    global $wpdb;
    // Check wp_usermeta capabilities
    $caps = $wpdb->get_var( $wpdb->prepare(
        "SELECT meta_value FROM {$wpdb->prefix}usermeta WHERE user_id = %d AND meta_key = %s LIMIT 1",
        $id, $wpdb->prefix . 'capabilities'
    ) );
    if ( ! $caps ) return false;
    $caps_arr = maybe_unserialize( $caps );
    return isset( $caps_arr[ $cap ] ) && $caps_arr[ $cap ];
}

function current_user_can( string $cap ): bool {
    return user_can( sz_get_current_user_id(), $cap );
}

// ── JWT helpers (used by portal) ─────────────────────────────────────────────

function sz_standalone_jwt_sign( array $payload, string $secret ): string {
    $header  = base64_encode( json_encode( [ 'alg' => 'HS256', 'typ' => 'JWT' ] ) );
    $body    = base64_encode( json_encode( $payload ) );
    $sig     = base64_encode( hash_hmac( 'sha256', "{$header}.{$body}", $secret, true ) );
    return "{$header}.{$body}.{$sig}";
}

function sz_standalone_jwt_verify( string $token, string $secret ): array|false {
    $parts = explode( '.', $token );
    if ( count( $parts ) !== 3 ) return false;
    [ $header, $body, $sig ] = $parts;
    $expected = base64_encode( hash_hmac( 'sha256', "{$header}.{$body}", $secret, true ) );
    if ( ! hash_equals( $expected, $sig ) ) return false;
    $payload = json_decode( base64_decode( $body ), true );
    if ( isset( $payload['exp'] ) && $payload['exp'] < time() ) return false;
    return $payload;
}
