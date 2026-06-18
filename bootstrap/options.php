<?php
/**
 * Senderzz Standalone — WordPress options API backed by wp_options table.
 */

if ( function_exists( 'get_option' ) ) return;

/** In-process cache to avoid repeated DB hits. */
$GLOBALS['_sz_options_cache'] = [];

function get_option( string $option, mixed $default = false ): mixed {
    if ( array_key_exists( $option, $GLOBALS['_sz_options_cache'] ) ) {
        return $GLOBALS['_sz_options_cache'][ $option ];
    }
    global $wpdb;
    $row = $wpdb->get_row( $wpdb->prepare(
        "SELECT option_value FROM {$wpdb->prefix}options WHERE option_name = %s LIMIT 1",
        $option
    ) );
    $value = $row ? maybe_unserialize( $row->option_value ) : $default;
    $GLOBALS['_sz_options_cache'][ $option ] = $value;
    return $value;
}

function update_option( string $option, mixed $value, string|bool $autoload = '' ): bool {
    global $wpdb;
    $GLOBALS['_sz_options_cache'][ $option ] = $value;
    $serialized = maybe_serialize( $value );
    $exists = $wpdb->get_var( $wpdb->prepare(
        "SELECT COUNT(*) FROM {$wpdb->prefix}options WHERE option_name = %s",
        $option
    ) );
    if ( $exists ) {
        $r = $wpdb->update( $wpdb->prefix . 'options', [ 'option_value' => $serialized ], [ 'option_name' => $option ] );
    } else {
        $r = $wpdb->insert( $wpdb->prefix . 'options', [
            'option_name'  => $option,
            'option_value' => $serialized,
            'autoload'     => $autoload === true || $autoload === 'yes' ? 'yes' : 'no',
        ] );
    }
    return $r !== false;
}

function delete_option( string $option ): bool {
    global $wpdb;
    unset( $GLOBALS['_sz_options_cache'][ $option ] );
    return (bool) $wpdb->delete( $wpdb->prefix . 'options', [ 'option_name' => $option ] );
}

function add_option( string $option, mixed $value = '', string $deprecated = '', string|bool $autoload = 'yes' ): bool {
    if ( get_option( $option ) !== false ) return false;
    return update_option( $option, $value, $autoload );
}

// ── Post meta (wraps wc_orders_meta for HPOS orders, postmeta for legacy) ────

function get_post_meta( int $post_id, string $key = '', bool $single = false ): mixed {
    global $wpdb;
    if ( $key === '' ) {
        $rows = $wpdb->get_results( $wpdb->prepare(
            "SELECT meta_key, meta_value FROM {$wpdb->prefix}postmeta WHERE post_id = %d",
            $post_id
        ) );
        $result = [];
        foreach ( $rows as $r ) {
            $result[ $r->meta_key ][] = maybe_unserialize( $r->meta_value );
        }
        return $result;
    }
    if ( $single ) {
        $val = $wpdb->get_var( $wpdb->prepare(
            "SELECT meta_value FROM {$wpdb->prefix}postmeta WHERE post_id = %d AND meta_key = %s LIMIT 1",
            $post_id, $key
        ) );
        // Try wc_orders_meta if not found (HPOS)
        if ( $val === null ) {
            $val = $wpdb->get_var( $wpdb->prepare(
                "SELECT meta_value FROM {$wpdb->prefix}wc_orders_meta WHERE order_id = %d AND meta_key = %s LIMIT 1",
                $post_id, $key
            ) );
        }
        return $val !== null ? maybe_unserialize( $val ) : '';
    }
    $vals = $wpdb->get_col( $wpdb->prepare(
        "SELECT meta_value FROM {$wpdb->prefix}postmeta WHERE post_id = %d AND meta_key = %s",
        $post_id, $key
    ) );
    return array_map( 'maybe_unserialize', $vals );
}

function update_post_meta( int $post_id, string $meta_key, mixed $meta_value, mixed $prev_value = '' ): int|bool {
    global $wpdb;
    $value = maybe_serialize( $meta_value );
    $exists = $wpdb->get_var( $wpdb->prepare(
        "SELECT meta_id FROM {$wpdb->prefix}postmeta WHERE post_id = %d AND meta_key = %s LIMIT 1",
        $post_id, $meta_key
    ) );
    if ( $exists ) {
        return (bool) $wpdb->update(
            $wpdb->prefix . 'postmeta',
            [ 'meta_value' => $value ],
            [ 'post_id' => $post_id, 'meta_key' => $meta_key ]
        );
    }
    return (bool) $wpdb->insert( $wpdb->prefix . 'postmeta', [
        'post_id'    => $post_id,
        'meta_key'   => $meta_key,
        'meta_value' => $value,
    ] );
}

function add_post_meta( int $post_id, string $meta_key, mixed $meta_value, bool $unique = false ): int|false {
    global $wpdb;
    if ( $unique && get_post_meta( $post_id, $meta_key, true ) ) return false;
    $wpdb->insert( $wpdb->prefix . 'postmeta', [
        'post_id'    => $post_id,
        'meta_key'   => $meta_key,
        'meta_value' => maybe_serialize( $meta_value ),
    ] );
    return $wpdb->insert_id ?: false;
}

function delete_post_meta( int $post_id, string $meta_key, mixed $meta_value = '' ): bool {
    global $wpdb;
    $where = [ 'post_id' => $post_id, 'meta_key' => $meta_key ];
    if ( $meta_value !== '' ) $where['meta_value'] = maybe_serialize( $meta_value );
    return (bool) $wpdb->delete( $wpdb->prefix . 'postmeta', $where );
}

// ── User meta ─────────────────────────────────────────────────────────────────

function get_user_meta( int $user_id, string $key = '', bool $single = false ): mixed {
    global $wpdb;
    if ( $single ) {
        $val = $wpdb->get_var( $wpdb->prepare(
            "SELECT meta_value FROM {$wpdb->prefix}usermeta WHERE user_id = %d AND meta_key = %s LIMIT 1",
            $user_id, $key
        ) );
        return $val !== null ? maybe_unserialize( $val ) : '';
    }
    $vals = $wpdb->get_col( $wpdb->prepare(
        "SELECT meta_value FROM {$wpdb->prefix}usermeta WHERE user_id = %d AND meta_key = %s",
        $user_id, $key
    ) );
    return array_map( 'maybe_unserialize', $vals );
}

function update_user_meta( int $user_id, string $meta_key, mixed $meta_value, mixed $prev_value = '' ): int|bool {
    global $wpdb;
    $value  = maybe_serialize( $meta_value );
    $exists = $wpdb->get_var( $wpdb->prepare(
        "SELECT umeta_id FROM {$wpdb->prefix}usermeta WHERE user_id = %d AND meta_key = %s LIMIT 1",
        $user_id, $meta_key
    ) );
    if ( $exists ) {
        return (bool) $wpdb->update( $wpdb->prefix . 'usermeta', [ 'meta_value' => $value ], [ 'user_id' => $user_id, 'meta_key' => $meta_key ] );
    }
    return (bool) $wpdb->insert( $wpdb->prefix . 'usermeta', [ 'user_id' => $user_id, 'meta_key' => $meta_key, 'meta_value' => $value ] );
}

function delete_user_meta( int $user_id, string $meta_key, mixed $meta_value = '' ): bool {
    global $wpdb;
    return (bool) $wpdb->delete( $wpdb->prefix . 'usermeta', [ 'user_id' => $user_id, 'meta_key' => $meta_key ] );
}

// ── Term/Transient stubs (used sparingly) ─────────────────────────────────────

function set_transient( string $transient, mixed $value, int $expiration = 0 ): bool {
    $exp = $expiration > 0 ? time() + $expiration : 0;
    $data = json_encode( [ 'v' => $value, 'e' => $exp ] );
    return update_option( "_transient_{$transient}", $data );
}

function get_transient( string $transient ): mixed {
    $raw = get_option( "_transient_{$transient}" );
    if ( ! $raw ) return false;
    $data = json_decode( $raw, true );
    if ( ! $data ) return false;
    if ( $data['e'] > 0 && $data['e'] < time() ) {
        delete_option( "_transient_{$transient}" );
        return false;
    }
    return $data['v'];
}

function delete_transient( string $transient ): bool {
    return delete_option( "_transient_{$transient}" );
}
