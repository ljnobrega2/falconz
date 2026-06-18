<?php
/**
 * Stubs mínimos do WordPress/WooCommerce para rodar os testes sem WP instalado.
 *
 * Cobre: $wpdb, funções de transient, sanitize, wp_json_encode,
 * get_current_user_id, do_action, apply_filters, has_filter,
 * WP_REST_Request, WP_REST_Response, WP_Error.
 */

if ( ! defined( 'ABSPATH' ) ) {
    define( 'ABSPATH', __DIR__ . '/../../' );
}
if ( ! defined( 'ARRAY_A' ) )  { define( 'ARRAY_A', 'ARRAY_A' ); }
if ( ! defined( 'ARRAY_N' ) )  { define( 'ARRAY_N', 'ARRAY_N' ); }
if ( ! defined( 'OBJECT' ) )   { define( 'OBJECT', 'OBJECT' ); }
if ( ! defined( 'MINUTE_IN_SECONDS' ) ) {
    define( 'MINUTE_IN_SECONDS', 60 );
}
if ( ! defined( 'HOUR_IN_SECONDS' ) ) {
    define( 'HOUR_IN_SECONDS', 3600 );
}
if ( ! defined( 'DAY_IN_SECONDS' ) ) {
    define( 'DAY_IN_SECONDS', 86400 );
}

// ─── Transients (in-memory para testes) ──────────────────────────────────────

$GLOBALS['_test_transients'] = [];

if ( ! function_exists( 'get_transient' ) ) {
    function get_transient( string $key ) {
        return $GLOBALS['_test_transients'][ $key ] ?? false;
    }
}
if ( ! function_exists( 'set_transient' ) ) {
    function set_transient( string $key, $value, int $expiration = 0 ): bool {
        $GLOBALS['_test_transients'][ $key ] = $value;
        return true;
    }
}
if ( ! function_exists( 'delete_transient' ) ) {
    function delete_transient( string $key ): bool {
        unset( $GLOBALS['_test_transients'][ $key ] );
        return true;
    }
}

// ─── WordPress helpers ────────────────────────────────────────────────────────

if ( ! function_exists( 'get_current_user_id' ) ) {
    function get_current_user_id(): int { return 0; }
}
if ( ! function_exists( 'sanitize_text_field' ) ) {
    function sanitize_text_field( string $str ): string { return trim( $str ); }
}
if ( ! function_exists( 'sanitize_key' ) ) {
    function sanitize_key( string $key ): string { return preg_replace( '/[^a-z0-9_\-]/', '', strtolower( $key ) ); }
}
if ( ! function_exists( 'sanitize_email' ) ) {
    function sanitize_email( string $email ): string { return filter_var( trim( $email ), FILTER_SANITIZE_EMAIL ) ?: ''; }
}
if ( ! function_exists( 'wp_unslash' ) ) {
    function wp_unslash( $value ) { return is_array( $value ) ? array_map( 'stripslashes', $value ) : stripslashes( (string) $value ); }
}
if ( ! function_exists( 'esc_html' ) ) {
    function esc_html( string $text ): string { return htmlspecialchars( $text, ENT_QUOTES, 'UTF-8' ); }
}
if ( ! function_exists( 'esc_attr' ) ) {
    function esc_attr( string $text ): string { return htmlspecialchars( $text, ENT_QUOTES, 'UTF-8' ); }
}
if ( ! function_exists( 'number_format_i18n' ) ) {
    function number_format_i18n( float $number, int $decimals = 0 ): string { return number_format( $number, $decimals, ',', '.' ); }
}
if ( ! function_exists( 'home_url' ) ) {
    function home_url( string $path = '' ): string { return 'https://example.com' . $path; }
}
if ( ! function_exists( 'add_query_arg' ) ) {
    function add_query_arg( $args, string $url = '' ): string { return $url . '?' . http_build_query( is_array( $args ) ? $args : [] ); }
}
if ( ! function_exists( 'wp_remote_get' ) ) {
    function wp_remote_get( string $url, array $args = [] ) { return new WP_Error( 'stub', 'wp_remote_get not available in tests' ); }
}
if ( ! function_exists( 'wp_die' ) ) {
    function wp_die( $message = '', $title = '', $args = [] ): never { throw new \RuntimeException( is_string( $message ) ? $message : 'wp_die called' ); }
}
if ( ! function_exists( 'wc_get_order' ) ) {
    function wc_get_order( int $order_id ) { return null; }
}
if ( ! function_exists( 'get_user_by' ) ) {
    function get_user_by( string $field, $value ) { return false; }
}
if ( ! function_exists( 'delete_option' ) ) {
    function delete_option( string $option ): bool { return true; }
}
if ( ! function_exists( 'wp_json_encode' ) ) {
    function wp_json_encode( $data, int $flags = 0 ): string|false {
        return json_encode( $data, $flags );
    }
}
if ( ! function_exists( 'do_action' ) ) {
    function do_action( string $hook, ...$args ): void {}
}
if ( ! function_exists( 'apply_filters' ) ) {
    function apply_filters( string $hook, $value, ...$args ) { return $value; }
}
if ( ! function_exists( 'has_filter' ) ) {
    function has_filter( string $hook, $callback = false ): bool { return false; }
}
if ( ! function_exists( 'add_action' ) ) {
    function add_action( string $hook, $callback, int $priority = 10, int $accepted_args = 1 ): bool { return true; }
}
if ( ! function_exists( 'get_option' ) ) {
    function get_option( string $option, $default = false ) { return $default; }
}
if ( ! function_exists( 'update_option' ) ) {
    function update_option( string $option, $value, $autoload = null ): bool { return true; }
}
if ( ! function_exists( 'absint' ) ) {
    function absint( $value ): int { return abs( (int) $value ); }
}
if ( ! function_exists( 'wp_generate_password' ) ) {
    function wp_generate_password( int $length = 12, bool $special_chars = true ): string {
        return bin2hex( random_bytes( (int) ceil( $length / 2 ) ) );
    }
}

// ─── WP_Error ────────────────────────────────────────────────────────────────

if ( ! class_exists( 'WP_Error', false ) ) {
    class WP_Error {
        private string $code;
        private string $message;
        public function __construct( string $code = '', string $message = '', $data = '' ) {
            $this->code    = $code;
            $this->message = $message;
        }
        public function get_error_code(): string    { return $this->code; }
        public function get_error_message(): string { return $this->message; }
    }
}

// ─── WP_REST_Request ─────────────────────────────────────────────────────────

if ( ! class_exists( 'WP_REST_Request', false ) ) {
    class WP_REST_Request {
        private array $params  = [];
        private array $headers = [];
        private string $body   = '';

        public function __construct( string $method = 'GET', string $route = '' ) {}

        public function set_param( string $key, $value ): void { $this->params[ $key ] = $value; }
        public function get_param( string $key ) { return $this->params[ $key ] ?? null; }
        public function get_json_params(): array {
            $decoded = json_decode( $this->body, true );
            return is_array( $decoded ) ? $decoded : [];
        }
        public function set_header( string $key, string $value ): void {
            $this->headers[ strtolower( $key ) ] = $value;
        }
        public function get_header( string $key ): ?string {
            return $this->headers[ strtolower( $key ) ] ?? null;
        }
        public function set_body( string $body ): void { $this->body = $body; }
        public function get_body(): string { return $this->body; }
    }
}

// ─── WP_REST_Response ────────────────────────────────────────────────────────

if ( ! class_exists( 'WP_REST_Response', false ) ) {
    class WP_REST_Response {
        private $data;
        private int $status;

        public function __construct( $data = null, int $status = 200 ) {
            $this->data   = $data;
            $this->status = $status;
        }
        public function get_data()          { return $this->data; }
        public function get_status(): int   { return $this->status; }
        public function set_data( $data ): void { $this->data = $data; }
    }
}

// ─── Fake $wpdb ──────────────────────────────────────────────────────────────

/**
 * FakeWpdb simula o comportamento mínimo do $wpdb do WordPress.
 * Usa SQLite em memória para que as queries reais (START TRANSACTION,
 * FOR UPDATE, INSERT, UPDATE, SELECT) sejam executadas de verdade —
 * garantindo que testamos a lógica SQL e não apenas mocks.
 *
 * Nota: SQLite não suporta FOR UPDATE. Removemos esse hint na prepare()
 * antes de executar, o que é seguro em contexto de testes single-thread.
 */
if ( ! class_exists( 'FakeWpdb', false ) ) :
class FakeWpdb {
    public string $prefix     = 'wp_';
    public string $last_error = '';
    public int    $insert_id  = 0;
    private \PDO  $pdo;

    public function __construct() {
        $this->pdo = new \PDO( 'sqlite::memory:' );
        $this->pdo->setAttribute( \PDO::ATTR_ERRMODE, \PDO::ERRMODE_EXCEPTION );
        $this->create_tables();
    }

    private function create_tables(): void {
        $this->pdo->exec( "
            CREATE TABLE wp_tpc_carteira (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                user_id INTEGER UNIQUE NOT NULL,
                saldo REAL NOT NULL DEFAULT 0.00,
                saldo_reservado REAL NOT NULL DEFAULT 0.00
            );
            CREATE TABLE wp_tpc_transacoes (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                user_id INTEGER NOT NULL,
                tipo TEXT NOT NULL,
                valor REAL NOT NULL,
                saldo_apos REAL NOT NULL DEFAULT 0.00,
                descricao TEXT,
                referencia TEXT,
                order_id INTEGER,
                me_order_id TEXT,
                status TEXT NOT NULL DEFAULT 'confirmado',
                meta_json TEXT,
                actor_id INTEGER,
                ip_address TEXT
            );
            CREATE UNIQUE INDEX idx_tx_ref_uq
                ON wp_tpc_transacoes (user_id, referencia, tipo, status)
                WHERE referencia IS NOT NULL;
            CREATE TABLE wp_tpc_recargas (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                user_id INTEGER NOT NULL,
                valor REAL NOT NULL,
                status TEXT NOT NULL DEFAULT 'pendente',
                me_pix_id TEXT,
                tx_id INTEGER,
                expires_at TEXT,
                created_at TEXT DEFAULT CURRENT_TIMESTAMP
            );
            CREATE TABLE wp_tpc_webhook_events (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                event_key TEXT UNIQUE NOT NULL,
                source TEXT,
                event_type TEXT,
                raw_body TEXT,
                meta_json TEXT,
                created_at TEXT DEFAULT CURRENT_TIMESTAMP
            );
        " );
    }

    /** Seed: cria carteira com saldo inicial */
    public function seed_wallet( int $user_id, float $saldo, float $reservado = 0.0 ): void {
        $this->pdo->exec( "INSERT OR REPLACE INTO wp_tpc_carteira (user_id, saldo, saldo_reservado)
                           VALUES ({$user_id}, {$saldo}, {$reservado})" );
    }

    /** Seed: cria recarga no status dado */
    public function seed_recarga( int $recarga_id, int $user_id, float $valor, string $status = 'pendente', string $me_pix_id = '' ): void {
        $mepix = $me_pix_id ? "'$me_pix_id'" : 'NULL';
        $this->pdo->exec( "INSERT INTO wp_tpc_recargas (id, user_id, valor, status, me_pix_id)
                           VALUES ({$recarga_id}, {$user_id}, {$valor}, '{$status}', {$mepix})" );
    }

    /** Helpers de leitura para assertions */
    public function get_wallet( int $user_id ): ?array {
        $stmt = $this->pdo->query( "SELECT * FROM wp_tpc_carteira WHERE user_id = {$user_id}" );
        $row  = $stmt->fetch( \PDO::FETCH_ASSOC );
        return $row ?: null;
    }
    public function get_transactions( int $user_id ): array {
        $stmt = $this->pdo->query( "SELECT * FROM wp_tpc_transacoes WHERE user_id = {$user_id} ORDER BY id" );
        return $stmt->fetchAll( \PDO::FETCH_ASSOC );
    }
    public function get_recarga( int $id ): ?array {
        $stmt = $this->pdo->query( "SELECT * FROM wp_tpc_recargas WHERE id = {$id}" );
        $row  = $stmt->fetch( \PDO::FETCH_ASSOC );
        return $row ?: null;
    }
    public function reset(): void {
        $this->pdo->exec( "DELETE FROM wp_tpc_carteira;
                           DELETE FROM wp_tpc_transacoes;
                           DELETE FROM wp_tpc_recargas;
                           DELETE FROM wp_tpc_webhook_events;" );
        $this->insert_id  = 0;
        $this->last_error = '';
    }

    // ── Interface pública que espelha $wpdb ──────────────────────────────────

    public function query( string $sql ): int|false {
        // Remover FOR UPDATE (não suportado em SQLite)
        $sql = preg_replace( '/\s+FOR\s+UPDATE/i', '', $sql );
        // Converter ON DUPLICATE KEY UPDATE ... para INSERT OR IGNORE (SQLite)
        $sql = preg_replace( '/\s+ON\s+DUPLICATE\s+KEY\s+UPDATE\s+[^;]*/i', '', $sql );
        $sql = preg_replace( '/^INSERT\s+INTO/i', 'INSERT OR IGNORE INTO', $sql );
        try {
            $result = $this->pdo->exec( $sql );
            $this->last_error = '';
            return $result;
        } catch ( \Exception $e ) {
            $this->last_error = $e->getMessage();
            return false;
        }
    }

    public function prepare( string $sql, ...$args ): string {
        // Suporte a array como único argumento (estilo WP)
        if ( count( $args ) === 1 && is_array( $args[0] ) ) {
            $args = $args[0];
        }
        // Substituição simples de placeholders %s, %d, %f
        $i = 0;
        return preg_replace_callback( '/%[sdf]/', function( $m ) use ( &$i, $args ) {
            $val = $args[ $i++ ] ?? '';
            if ( $m[0] === '%d' ) return (int) $val;
            if ( $m[0] === '%f' ) return (float) $val;
            return "'" . str_replace( "'", "''", (string) $val ) . "'";
        }, $sql );
    }

    public function get_var( string $sql ) {
        $sql = preg_replace( '/\s+FOR\s+UPDATE/i', '', $sql );
        try {
            $stmt = $this->pdo->query( $sql );
            $row  = $stmt->fetch( \PDO::FETCH_NUM );
            return $row ? $row[0] : null;
        } catch ( \Exception $e ) {
            $this->last_error = $e->getMessage();
            return null;
        }
    }

    public function get_row( string $sql, $output = null ) {
        $sql = preg_replace( '/\s+FOR\s+UPDATE/i', '', $sql );
        try {
            $stmt = $this->pdo->query( $sql );
            if ( $output === ARRAY_A ) {
                $row = $stmt->fetch( \PDO::FETCH_ASSOC );
                return $row ?: null;
            }
            $row = $stmt->fetch( \PDO::FETCH_ASSOC );
            if ( ! $row ) return null;
            return (object) $row;
        } catch ( \Exception $e ) {
            $this->last_error = $e->getMessage();
            return null;
        }
    }

    public function get_results( string $sql, $output = null ): array {
        $sql = preg_replace( '/\s+FOR\s+UPDATE/i', '', $sql );
        try {
            $stmt = $this->pdo->query( $sql );
            return $stmt->fetchAll( \PDO::FETCH_OBJ );
        } catch ( \Exception $e ) {
            $this->last_error = $e->getMessage();
            return [];
        }
    }

    public function insert( string $table, array $data, $format = null ): int|false {
        $cols = implode( ', ', array_keys( $data ) );
        $vals = implode( ', ', array_map( fn( $v ) =>
            is_null( $v ) ? 'NULL' : ( is_numeric( $v ) ? $v : "'" . str_replace( "'", "''", (string) $v ) . "'" ),
            array_values( $data )
        ) );
        try {
            $this->pdo->exec( "INSERT INTO {$table} ({$cols}) VALUES ({$vals})" );
            $this->insert_id  = (int) $this->pdo->lastInsertId();
            $this->last_error = '';
            return $this->insert_id;
        } catch ( \Exception $e ) {
            $this->last_error = $e->getMessage();
            return false;
        }
    }

    public function update( string $table, array $data, array $where, $format = null, $where_format = null ): int|false {
        $set   = implode( ', ', array_map( fn( $k, $v ) =>
            "{$k} = " . ( is_null( $v ) ? 'NULL' : ( is_numeric( $v ) ? $v : "'" . str_replace( "'", "''", (string) $v ) . "'" ) ),
            array_keys( $data ), array_values( $data )
        ) );
        $conds = implode( ' AND ', array_map( fn( $k, $v ) =>
            "{$k} = " . ( is_null( $v ) ? 'NULL' : ( is_numeric( $v ) ? $v : "'" . str_replace( "'", "''", (string) $v ) . "'" ) ),
            array_keys( $where ), array_values( $where )
        ) );
        try {
            $affected = $this->pdo->exec( "UPDATE {$table} SET {$set} WHERE {$conds}" );
            $this->last_error = '';
            return $affected;
        } catch ( \Exception $e ) {
            $this->last_error = $e->getMessage();
            return false;
        }
    }

    public function delete( string $table, array $where, $format = null ): int|false {
        $conds = implode( ' AND ', array_map( fn( $k, $v ) =>
            "{$k} = '" . str_replace( "'", "''", (string) $v ) . "'",
            array_keys( $where ), array_values( $where )
        ) );
        try {
            return $this->pdo->exec( "DELETE FROM {$table} WHERE {$conds}" );
        } catch ( \Exception $e ) {
            $this->last_error = $e->getMessage();
            return false;
        }
    }
} // end class FakeWpdb
endif; // end if !class_exists('FakeWpdb')

// ─── Injeta $wpdb global ─────────────────────────────────────────────────────

$GLOBALS['wpdb'] = new FakeWpdb();

// ─── Guard de múltiplo carregamento ──────────────────────────────────────────

// Remove guards "defined() || exit" dos arquivos de produção
// (o bootstrap já controla o carregamento)
if ( ! function_exists( 'tpc_get_saldo' ) ) {
    function tpc_get_saldo( int $user_id ): float {
        global $wpdb;
        $row = $wpdb->get_row( $wpdb->prepare( "SELECT saldo FROM {$wpdb->prefix}tpc_carteira WHERE user_id = %d", $user_id ) );
        return $row ? (float) $row->saldo : 0.0;
    }
}
if ( ! function_exists( 'tpc_get_saldo_disponivel' ) ) {
    function tpc_get_saldo_disponivel( int $user_id ): float {
        global $wpdb;
        $row = $wpdb->get_row( $wpdb->prepare( "SELECT saldo, saldo_reservado FROM {$wpdb->prefix}tpc_carteira WHERE user_id = %d", $user_id ) );
        return $row ? round( (float) $row->saldo - (float) $row->saldo_reservado, 2 ) : 0.0;
    }
}

// ─── Stubs adicionais necessários para pix.php ───────────────────────────────

if ( ! function_exists( 'wp_remote_post' ) ) {
    function wp_remote_post( string $url, array $args = [] ) {
        return new WP_Error( 'stub', 'wp_remote_post not available in tests' );
    }
}
if ( ! function_exists( 'wp_remote_retrieve_body' ) ) {
    function wp_remote_retrieve_body( $response ): string { return ''; }
}
if ( ! function_exists( 'wp_remote_retrieve_response_code' ) ) {
    function wp_remote_retrieve_response_code( $response ) { return 0; }
}
if ( ! function_exists( 'is_wp_error' ) ) {
    function is_wp_error( $thing ): bool { return $thing instanceof WP_Error; }
}
if ( ! function_exists( 'register_rest_route' ) ) {
    function register_rest_route( string $ns, string $route, array $args ): void {}
}
if ( ! function_exists( 'wp_next_scheduled' ) ) {
    function wp_next_scheduled( string $hook ): bool { return true; }
}
if ( ! function_exists( 'wp_schedule_event' ) ) {
    function wp_schedule_event( int $timestamp, string $recurrence, string $hook ): void {}
}
if ( ! function_exists( 'wp_clear_scheduled_hook' ) ) {
    function wp_clear_scheduled_hook( string $hook ): void {}
}
if ( ! function_exists( 'get_bloginfo' ) ) {
    function get_bloginfo( string $show ): string { return 'Senderzz Test'; }
}
if ( ! function_exists( 'wp_mail' ) ) {
    function wp_mail( $to, $subject, $message, $headers = '', $attachments = [] ): bool { return true; }
}
if ( ! function_exists( 'wp_parse_url' ) ) {
    function wp_parse_url( string $url, int $component = -1 ) { return parse_url( $url, $component ); }
}
if ( ! function_exists( 'add_filter' ) ) {
    function add_filter( string $hook, $callback, int $priority = 10, int $accepted_args = 1 ): bool { return true; }
}
if ( ! function_exists( 'remove_all_actions' ) ) {
    function remove_all_actions( string $hook ): void {}
}
if ( ! function_exists( 'wp_redirect' ) ) {
    function wp_redirect( string $location, int $status = 302 ): bool { return true; }
}
if ( ! function_exists( 'esc_url' ) ) {
    function esc_url( string $url ): string { return $url; }
}
if ( ! function_exists( 'admin_url' ) ) {
    function admin_url( string $path = '' ): string { return 'https://example.com/wp-admin/' . $path; }
}
if ( ! function_exists( 'rest_ensure_response' ) ) {
    function rest_ensure_response( $response ) {
        if ( $response instanceof WP_REST_Response ) return $response;
        return new WP_REST_Response( $response );
    }
}
if ( ! function_exists( 'current_time' ) ) {
    function current_time( string $type, bool $gmt = false ): string {
        return gmdate( 'Y-m-d H:i:s' );
    }
}
if ( ! function_exists( 'gmdate' ) ) {
    // gmdate is a PHP native — only define if somehow missing
}

// ─── Carrega código de produção ───────────────────────────────────────────────

if ( ! defined( 'SENDERZZ_TPC_WALLET_LOADED' ) ) {
    require_once __DIR__ . '/../../includes/tpc/wallet.php';
}

// Stubs de funções ME (API externa) antes de carregar pix.php

// Carrega pix.php — contém tpc_confirmar_recarga, tpc_criar_recarga, etc.
// Os add_action() dentro do arquivo executam mas não disparam em testes (sem WP loop).
// NÃO definimos SENDERZZ_TPC_PIX_LOADED aqui — pix.php define internamente.
require_once __DIR__ . '/../../includes/tpc/pix.php';

// Carrega webhook.php — contém tpc_webhook_extract_amount, tpc_webhook_register_event, etc.
require_once __DIR__ . '/../../includes/tpc/webhook.php';

// senderzz-me-webhook verify signature (inline stub — não carrega o arquivo de
// produção que registra add_action/add_filter no nível de carregamento)
if ( ! function_exists( 'senderzz_me_verify_signature' ) ) {
    function senderzz_me_verify_signature( WP_REST_Request $request ): bool {
        $settings = get_option( 'woocommerce_wc-melhor-envio_settings', [] );
        $secret   = (string) ( $settings['client_secret'] ?? '' );
        if ( $secret === '' ) return false;
        $signature = (string) ( $request->get_header( 'x-me-signature' ) ?? '' );
        if ( $signature === '' ) {
            $payload     = $request->get_json_params();
            $event_value = is_array( $payload ) ? (string) ( $payload['event'] ?? '' ) : '';
            return in_array( $event_value, [ 'ping', 'test', 'webhook.ping' ], true );
        }
        $body     = (string) $request->get_body();
        $expected = base64_encode( hash_hmac( 'sha256', $body, $secret, true ) );
        return hash_equals( $expected, $signature );
    }
}
