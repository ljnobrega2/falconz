<?php
/**
 * Senderzz Standalone Bootstrap
 *
 * Entry point for running without WordPress.
 * Load order matters — do not reorder.
 *
 * Usage:
 *   require __DIR__ . '/bootstrap/bootstrap.php';
 *   // All WP/WC APIs now available.
 *   // Optionally: SZ_Router::dispatch() to handle REST requests.
 */

// ── 1. Environment constants ─────────────────────────────────────────────────

if ( ! defined( 'ABSPATH' ) ) {
    define( 'ABSPATH', dirname( __DIR__ ) . '/' );
}

define( 'SZ_STANDALONE', true );

$_env_file = dirname( __DIR__ ) . '/.env';
if ( file_exists( $_env_file ) ) {
    foreach ( file( $_env_file, FILE_IGNORE_NEW_LINES | FILE_SKIP_EMPTY_LINES ) as $line ) {
        if ( str_starts_with( trim( $line ), '#' ) ) continue;
        if ( strpos( $line, '=' ) !== false ) {
            [ $k, $v ] = array_map( 'trim', explode( '=', $line, 2 ) );
            if ( ! defined( $k ) ) define( $k, $v );
            if ( ! isset( $_ENV[ $k ] ) ) $_ENV[ $k ] = $v;
        }
    }
}

// Required constants — set via .env or environment variables
foreach ( [ 'DB_HOST', 'DB_NAME', 'DB_USER', 'DB_PASSWORD' ] as $_const ) {
    if ( ! defined( $_const ) ) {
        $env_val = $_ENV[ $_const ] ?? getenv( $_const );
        if ( $env_val !== false ) define( $_const, $env_val );
    }
}

if ( ! defined( 'DB_PREFIX' ) ) {
    define( 'DB_PREFIX', $_ENV['DB_PREFIX'] ?? getenv( 'DB_PREFIX' ) ?: 'wp_' );
}

if ( ! defined( 'AUTH_KEY' ) ) {
    $key = $_ENV['AUTH_KEY'] ?? getenv( 'AUTH_KEY' ) ?: 'sz-standalone-change-me';
    define( 'AUTH_KEY', $key );
}

if ( ! defined( 'AUTH_SALT' ) ) {
    $salt = $_ENV['AUTH_SALT'] ?? getenv( 'AUTH_SALT' ) ?: 'sz-standalone-salt-change-me';
    define( 'AUTH_SALT', $salt );
}

// ── 2. Core classes and functions ────────────────────────────────────────────

require_once __DIR__ . '/class-wp-error.php';
require_once __DIR__ . '/class-wpdb.php';
require_once __DIR__ . '/hooks.php';
require_once __DIR__ . '/functions-core.php';
require_once __DIR__ . '/auth.php';
require_once __DIR__ . '/options.php';
require_once __DIR__ . '/class-wp-rest.php';
require_once __DIR__ . '/router.php';
require_once __DIR__ . '/class-wc-compat.php';

// ── 3. Database connection ────────────────────────────────────────────────────

global $wpdb;

if ( ! ( $wpdb instanceof wpdb ) ) {
    $wpdb = new wpdb(
        defined( 'DB_USER' )     ? DB_USER     : '',
        defined( 'DB_PASSWORD' ) ? DB_PASSWORD : '',
        defined( 'DB_NAME' )     ? DB_NAME     : '',
        defined( 'DB_HOST' )     ? DB_HOST     : 'localhost'
    );
    $wpdb->prefix = defined( 'DB_PREFIX' ) ? DB_PREFIX : 'wp_';
}

// ── 4. Session (for portal auth) ──────────────────────────────────────────────

if ( session_status() === PHP_SESSION_NONE && ! headers_sent() ) {
    session_name( 'sz_session' );
    session_start();
}

// Restore current user from session if set
if ( isset( $_SESSION['sz_user_id'] ) ) {
    sz_set_current_user( (int) $_SESSION['sz_user_id'] );
}

// ── 5. DOING_AJAX detection ───────────────────────────────────────────────────

if ( ! defined( 'DOING_AJAX' ) ) {
    define( 'DOING_AJAX',
        ( isset( $_SERVER['HTTP_X_REQUESTED_WITH'] ) && strtolower( $_SERVER['HTTP_X_REQUESTED_WITH'] ) === 'xmlhttprequest' )
        || ( isset( $_POST['action'] ) )
    );
}

// ── 6. Fire WP init-equivalent hooks ─────────────────────────────────────────

do_action( 'init' );
do_action( 'rest_api_init' );     // Causes all register_rest_route() calls to fire
do_action( 'wp_loaded' );
do_action( 'plugins_loaded' );    // Some includes hook here

// ── 7. WordPress stubs that depend on global state ───────────────────────────

if ( ! function_exists( 'is_admin' ) ) {
    function is_admin(): bool {
        return defined( 'SZ_IS_ADMIN' ) && SZ_IS_ADMIN;
    }
}

if ( ! function_exists( 'wp_doing_cron' ) ) {
    function wp_doing_cron(): bool { return false; }
}

if ( ! function_exists( 'get_current_blog_id' ) ) {
    function get_current_blog_id(): int { return 1; }
}

if ( ! function_exists( 'is_multisite' ) ) {
    function is_multisite(): bool { return false; }
}

if ( ! function_exists( 'get_post' ) ) {
    function get_post( int|null $post = null, string $output = OBJECT ): mixed {
        if ( ! $post ) return null;
        global $wpdb;
        $row = $wpdb->get_row( $wpdb->prepare( "SELECT * FROM {$wpdb->prefix}posts WHERE ID = %d LIMIT 1", $post ) );
        if ( ! $row ) return null;
        return $output === ARRAY_A ? (array) $row : $row;
    }
}

if ( ! function_exists( 'get_posts' ) ) {
    function get_posts( array $args = [] ): array {
        global $wpdb;
        $type  = $args['post_type'] ?? 'post';
        $limit = (int) ( $args['numberposts'] ?? $args['posts_per_page'] ?? 10 );
        $rows  = $wpdb->get_results( "SELECT * FROM {$wpdb->prefix}posts WHERE post_type = '" . esc_sql( $type ) . "' AND post_status = 'publish' LIMIT {$limit}" );
        return $rows ?: [];
    }
}

if ( ! function_exists( 'get_post_field' ) ) {
    function get_post_field( string $field, int|object $post = null ): mixed {
        $post = is_int( $post ) ? get_post( $post ) : $post;
        return $post ? ( $post->$field ?? '' ) : '';
    }
}

if ( ! function_exists( 'wp_insert_post' ) ) {
    function wp_insert_post( array $args, bool $wp_error = false ): int|WP_Error {
        global $wpdb;
        $data = [
            'post_title'   => $args['post_title']   ?? '',
            'post_content' => $args['post_content'] ?? '',
            'post_status'  => $args['post_status']  ?? 'publish',
            'post_type'    => $args['post_type']     ?? 'post',
            'post_author'  => $args['post_author']   ?? 0,
            'post_date'    => current_time( 'mysql' ),
            'post_date_gmt'=> current_time( 'mysql', true ),
        ];
        $wpdb->insert( $wpdb->prefix . 'posts', $data );
        return $wpdb->insert_id ?: ( $wp_error ? new WP_Error( 'insert_failed', 'Insert failed' ) : 0 );
    }
}

if ( ! function_exists( 'wp_update_post' ) ) {
    function wp_update_post( array $args, bool $wp_error = false ): int|WP_Error {
        global $wpdb;
        $id = $args['ID'] ?? 0;
        if ( ! $id ) return $wp_error ? new WP_Error( 'no_id', 'No ID' ) : 0;
        unset( $args['ID'] );
        $wpdb->update( $wpdb->prefix . 'posts', $args, [ 'ID' => $id ] );
        return $id;
    }
}

if ( ! function_exists( 'wp_delete_post' ) ) {
    function wp_delete_post( int $post_id, bool $force_delete = false ): object|false {
        global $wpdb;
        $post = get_post( $post_id );
        if ( ! $post ) return false;
        if ( $force_delete ) {
            $wpdb->delete( $wpdb->prefix . 'posts', [ 'ID' => $post_id ] );
            $wpdb->delete( $wpdb->prefix . 'postmeta', [ 'post_id' => $post_id ] );
        } else {
            $wpdb->update( $wpdb->prefix . 'posts', [ 'post_status' => 'trash' ], [ 'ID' => $post_id ] );
        }
        return $post;
    }
}

if ( ! function_exists( 'wp_set_object_terms' ) ) {
    function wp_set_object_terms( int $object_id, mixed $terms, string $taxonomy, bool $append = false ): array|WP_Error {
        return [];
    }
}

if ( ! function_exists( 'get_term' ) ) {
    function get_term( int $term, string $taxonomy = '' ): object|null {
        global $wpdb;
        return $wpdb->get_row( $wpdb->prepare( "SELECT * FROM {$wpdb->prefix}terms WHERE term_id = %d LIMIT 1", $term ) );
    }
}

if ( ! function_exists( 'get_term_by' ) ) {
    function get_term_by( string $field, mixed $value, string $taxonomy = '' ): object|false {
        global $wpdb;
        $row = $wpdb->get_row( $wpdb->prepare(
            "SELECT t.*, tt.* FROM {$wpdb->prefix}terms t JOIN {$wpdb->prefix}term_taxonomy tt ON t.term_id = tt.term_id WHERE t.{$field} = %s AND tt.taxonomy = %s LIMIT 1",
            $value, $taxonomy
        ) );
        return $row ?: false;
    }
}

if ( ! function_exists( 'wp_cache_get' ) ) {
    function wp_cache_get( string|int $key, string $group = '', bool $force = false, ?bool &$found = null ): mixed {
        $found = false;
        return false;
    }
}

if ( ! function_exists( 'wp_cache_set' ) ) {
    function wp_cache_set( string|int $key, mixed $data, string $group = '', int $expire = 0 ): bool { return true; }
}

if ( ! function_exists( 'wp_cache_delete' ) ) {
    function wp_cache_delete( string|int $key, string $group = '' ): bool { return true; }
}

if ( ! function_exists( 'clean_post_cache' ) ) {
    function clean_post_cache( int $post_id ): void {}
}

if ( ! function_exists( 'apply_filters_ref_array' ) ) {
    function apply_filters_ref_array( string $hook, array $args ): mixed {
        return apply_filters( $hook, ...$args );
    }
}

if ( ! function_exists( 'do_action_ref_array' ) ) {
    function do_action_ref_array( string $hook, array $args ): void {
        do_action( $hook, ...$args );
    }
}
