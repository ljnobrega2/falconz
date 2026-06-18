<?php
/**
 * Senderzz Standalone — WooCommerce compatibility layer.
 * Provides WC_Order, WC_Product, wc_get_order(), wc_get_product(), etc.
 * Reads from existing WC tables (compatible with HPOS and legacy).
 */

// ── WC_Order ──────────────────────────────────────────────────────────────────

if ( ! class_exists( 'WC_Order' ) ) :

class WC_Order {

    protected int    $id;
    protected array  $data   = [];
    protected array  $meta   = [];
    protected array  $items  = [];
    protected bool   $meta_loaded  = false;
    protected bool   $items_loaded = false;

    public function __construct( int|object $id_or_row = 0 ) {
        if ( is_object( $id_or_row ) ) {
            $this->id   = (int) ( $id_or_row->id ?? $id_or_row->ID ?? 0 );
            $this->data = (array) $id_or_row;
        } else {
            $this->id = (int) $id_or_row;
            if ( $this->id ) $this->_load();
        }
    }

    private function _load(): void {
        global $wpdb;
        // HPOS first
        $row = $wpdb->get_row( $wpdb->prepare(
            "SELECT * FROM {$wpdb->prefix}wc_orders WHERE id = %d LIMIT 1",
            $this->id
        ), ARRAY_A );
        if ( ! $row ) {
            // Legacy posts
            $row = $wpdb->get_row( $wpdb->prepare(
                "SELECT * FROM {$wpdb->prefix}posts WHERE ID = %d AND post_type = 'shop_order' LIMIT 1",
                $this->id
            ), ARRAY_A );
        }
        $this->data = $row ?: [];
    }

    private function _load_meta(): void {
        if ( $this->meta_loaded ) return;
        global $wpdb;
        // HPOS meta
        $rows = $wpdb->get_results( $wpdb->prepare(
            "SELECT meta_key, meta_value FROM {$wpdb->prefix}wc_orders_meta WHERE order_id = %d",
            $this->id
        ) );
        foreach ( $rows as $r ) {
            $this->meta[ $r->meta_key ] = maybe_unserialize( $r->meta_value );
        }
        // Legacy postmeta (merge)
        $rows2 = $wpdb->get_results( $wpdb->prepare(
            "SELECT meta_key, meta_value FROM {$wpdb->prefix}postmeta WHERE post_id = %d",
            $this->id
        ) );
        foreach ( $rows2 as $r ) {
            if ( ! isset( $this->meta[ $r->meta_key ] ) ) {
                $this->meta[ $r->meta_key ] = maybe_unserialize( $r->meta_value );
            }
        }
        $this->meta_loaded = true;
    }

    private function _load_items(): void {
        if ( $this->items_loaded ) return;
        global $wpdb;
        $items = $wpdb->get_results( $wpdb->prepare(
            "SELECT oi.*, oim.meta_key, oim.meta_value
             FROM {$wpdb->prefix}woocommerce_order_items oi
             LEFT JOIN {$wpdb->prefix}woocommerce_order_itemmeta oim ON oim.order_item_id = oi.order_item_id
             WHERE oi.order_id = %d AND oi.order_item_type = 'line_item'",
            $this->id
        ) );
        $grouped = [];
        foreach ( $items as $row ) {
            $iid = $row->order_item_id;
            if ( ! isset( $grouped[ $iid ] ) ) {
                $grouped[ $iid ] = [ 'name' => $row->order_item_name, 'meta' => [] ];
            }
            if ( $row->meta_key ) {
                $grouped[ $iid ]['meta'][ $row->meta_key ] = maybe_unserialize( $row->meta_value );
            }
        }
        foreach ( $grouped as $iid => $item ) {
            $qty     = (int) ( $item['meta']['_qty'] ?? 1 );
            $product_id = (int) ( $item['meta']['_product_id'] ?? 0 );
            $variation_id = (int) ( $item['meta']['_variation_id'] ?? 0 );
            $subtotal = (float) ( $item['meta']['_line_subtotal'] ?? 0 );
            $this->items[ $iid ] = (object) [
                'order_item_id' => $iid,
                'name'          => $item['name'],
                'quantity'      => $qty,
                'qty'           => $qty,
                'product_id'    => $product_id,
                'variation_id'  => $variation_id,
                'total'         => $subtotal,
                'subtotal'      => $subtotal,
                'meta'          => $item['meta'],
            ];
        }
        $this->items_loaded = true;
    }

    // ── Getters ───────────────────────────────────────────────────────────────

    public function get_id(): int { return $this->id; }

    public function get_status(): string {
        $s = $this->data['status'] ?? $this->data['post_status'] ?? '';
        return str_replace( 'wc-', '', $s );
    }

    public function get_total(): float {
        return (float) ( $this->data['total_amount'] ?? $this->data['cart_total'] ?? 0 );
    }

    public function get_subtotal(): float { return $this->get_total(); }

    public function get_billing_first_name(): string { return (string) ( $this->data['billing_first_name'] ?? '' ); }
    public function get_billing_last_name():  string { return (string) ( $this->data['billing_last_name']  ?? '' ); }
    public function get_billing_email():      string { return (string) ( $this->data['billing_email']      ?? '' ); }
    public function get_billing_phone():      string { return (string) ( $this->data['billing_phone']      ?? '' ); }
    public function get_billing_address_1():  string { return (string) ( $this->data['billing_address_1']  ?? '' ); }
    public function get_billing_address_2():  string { return (string) ( $this->data['billing_address_2']  ?? '' ); }
    public function get_billing_city():       string { return (string) ( $this->data['billing_city']        ?? '' ); }
    public function get_billing_state():      string { return (string) ( $this->data['billing_state']       ?? '' ); }
    public function get_billing_postcode():   string { return (string) ( $this->data['billing_postcode']    ?? '' ); }

    public function get_shipping_address_1(): string { return (string) ( $this->data['shipping_address_1'] ?? '' ); }
    public function get_shipping_address_2(): string { return (string) ( $this->data['shipping_address_2'] ?? '' ); }
    public function get_shipping_city():      string { return (string) ( $this->data['shipping_city']       ?? '' ); }
    public function get_shipping_state():     string { return (string) ( $this->data['shipping_state']      ?? '' ); }
    public function get_shipping_postcode():  string { return (string) ( $this->data['shipping_postcode']   ?? '' ); }

    public function get_date_created(): ?object {
        $d = $this->data['date_created_gmt'] ?? $this->data['post_date_gmt'] ?? null;
        return $d ? (object) [ 'date' => $d, 'timestamp' => strtotime( $d ) ] : null;
    }

    public function get_customer_id(): int { return (int) ( $this->data['customer_id'] ?? $this->data['post_author'] ?? 0 ); }

    public function get_items( string $type = 'line_item' ): array {
        $this->_load_items();
        return $this->items;
    }

    public function get_meta( string $key, bool $single = true, string $context = 'view' ): mixed {
        $this->_load_meta();
        return $this->meta[ $key ] ?? '';
    }

    public function update_meta_data( string $key, mixed $value, int $meta_id = 0 ): void {
        $this->_load_meta();
        $this->meta[ $key ] = $value;
        $GLOBALS['_sz_order_meta_dirty'][ $this->id ][ $key ] = $value;
    }

    public function delete_meta_data( string $key ): void {
        unset( $this->meta[ $key ] );
        $GLOBALS['_sz_order_meta_dirty'][ $this->id ][ $key ] = null;
    }

    public function update_status( string $new_status, string $note = '', bool $manual = false ): bool {
        global $wpdb;
        $status = str_starts_with( $new_status, 'wc-' ) ? $new_status : 'wc-' . $new_status;
        // HPOS
        $r = $wpdb->update( $wpdb->prefix . 'wc_orders', [ 'status' => $status ], [ 'id' => $this->id ] );
        if ( $r === false ) {
            // Legacy
            $wpdb->update( $wpdb->prefix . 'posts', [ 'post_status' => $status ], [ 'ID' => $this->id ] );
        }
        $this->data['status'] = $new_status;
        return true;
    }

    public function save(): int {
        global $wpdb;
        // Flush dirty meta
        foreach ( $GLOBALS['_sz_order_meta_dirty'][ $this->id ] ?? [] as $key => $value ) {
            if ( $value === null ) {
                $wpdb->delete( $wpdb->prefix . 'wc_orders_meta', [ 'order_id' => $this->id, 'meta_key' => $key ] );
                $wpdb->delete( $wpdb->prefix . 'postmeta', [ 'post_id' => $this->id, 'meta_key' => $key ] );
            } else {
                $sv = maybe_serialize( $value );
                $exists = $wpdb->get_var( $wpdb->prepare(
                    "SELECT id FROM {$wpdb->prefix}wc_orders_meta WHERE order_id = %d AND meta_key = %s LIMIT 1",
                    $this->id, $key
                ) );
                if ( $exists ) {
                    $wpdb->update( $wpdb->prefix . 'wc_orders_meta', [ 'meta_value' => $sv ], [ 'order_id' => $this->id, 'meta_key' => $key ] );
                } else {
                    $wpdb->insert( $wpdb->prefix . 'wc_orders_meta', [ 'order_id' => $this->id, 'meta_key' => $key, 'meta_value' => $sv ] );
                }
                // Also postmeta for compatibility
                update_post_meta( $this->id, $key, $value );
            }
        }
        unset( $GLOBALS['_sz_order_meta_dirty'][ $this->id ] );
        return $this->id;
    }

    public function add_order_note( string $note, bool $is_customer_note = false, bool $added_by_user = false ): int {
        global $wpdb;
        $wpdb->insert( $wpdb->prefix . 'comments', [
            'comment_post_ID'  => $this->id,
            'comment_content'  => $note,
            'comment_type'     => 'order_note',
            'comment_date'     => current_time( 'mysql' ),
            'comment_date_gmt' => current_time( 'mysql', true ),
            'comment_approved' => 1,
        ] );
        return $wpdb->insert_id;
    }

    public function get_order_number(): string { return (string) $this->id; }
    public function get_formatted_billing_full_name(): string {
        return trim( $this->get_billing_first_name() . ' ' . $this->get_billing_last_name() );
    }

    // WC compat aliases
    public function get_billing_address(): string { return $this->get_billing_address_1(); }
    public function has_status( string|array $status ): bool {
        return in_array( $this->get_status(), (array) $status, true );
    }
}

endif;

// ── WC_Product ────────────────────────────────────────────────────────────────

if ( ! class_exists( 'WC_Product' ) ) :

class WC_Product {
    protected int   $id;
    protected array $data = [];
    protected array $meta = [];

    public function __construct( int $id = 0 ) {
        $this->id = $id;
        if ( $id ) $this->_load();
    }

    private function _load(): void {
        global $wpdb;
        $row = $wpdb->get_row( $wpdb->prepare(
            "SELECT * FROM {$wpdb->prefix}posts WHERE ID = %d AND post_type IN ('product','product_variation') LIMIT 1",
            $this->id
        ), ARRAY_A );
        $this->data = $row ?: [];
    }

    public function get_id():    int    { return $this->id; }
    public function get_name():  string { return (string) ( $this->data['post_title'] ?? '' ); }
    public function get_price(): string {
        return (string) get_post_meta( $this->id, '_price', true );
    }
    public function get_stock_quantity(): ?int {
        $q = get_post_meta( $this->id, '_stock', true );
        return $q !== '' ? (int) $q : null;
    }
    public function get_sku():   string { return (string) get_post_meta( $this->id, '_sku', true ); }
    public function get_meta( string $key, bool $single = true ): mixed {
        return get_post_meta( $this->id, $key, $single );
    }
    public function update_meta_data( string $key, mixed $value ): void {
        update_post_meta( $this->id, $key, $value );
    }
    public function save(): int { return $this->id; }
    public function is_in_stock(): bool {
        return get_post_meta( $this->id, '_stock_status', true ) === 'instock';
    }
}

endif;

// ── WC functions ──────────────────────────────────────────────────────────────

if ( ! function_exists( 'wc_get_order' ) ) {
    function wc_get_order( int|bool $order_id = false ): WC_Order|false {
        if ( ! $order_id ) return false;
        $order = new WC_Order( (int) $order_id );
        return $order->get_id() ? $order : false;
    }
}

if ( ! function_exists( 'wc_create_order' ) ) {
    function wc_create_order( array $args = [] ): WC_Order|WP_Error {
        global $wpdb;
        $status = $args['status'] ?? 'pending';
        $wpdb->insert( $wpdb->prefix . 'posts', [
            'post_type'   => 'shop_order',
            'post_status' => 'wc-' . $status,
            'post_date'   => current_time( 'mysql' ),
            'post_author' => $args['customer_id'] ?? 0,
        ] );
        return new WC_Order( $wpdb->insert_id );
    }
}

if ( ! function_exists( 'wc_get_product' ) ) {
    function wc_get_product( int $product_id = 0 ): WC_Product|false {
        if ( ! $product_id ) return false;
        $p = new WC_Product( $product_id );
        return $p->get_id() ? $p : false;
    }
}

if ( ! function_exists( 'wc_get_orders' ) ) {
    function wc_get_orders( array $args = [] ): array {
        global $wpdb;
        $limit  = (int) ( $args['limit'] ?? 20 );
        $status = $args['status'] ?? '';
        $where  = '';
        if ( $status ) $where = "AND status = 'wc-" . esc_sql( $status ) . "'";
        $ids = $wpdb->get_col( "SELECT id FROM {$wpdb->prefix}wc_orders WHERE type = 'shop_order' {$where} ORDER BY date_created_gmt DESC LIMIT {$limit}" );
        if ( ! $ids ) {
            $ids = $wpdb->get_col( "SELECT ID FROM {$wpdb->prefix}posts WHERE post_type = 'shop_order' {$where} ORDER BY post_date DESC LIMIT {$limit}" );
        }
        return array_map( 'wc_get_order', array_map( 'intval', $ids ) );
    }
}

if ( ! function_exists( 'wc_price' ) ) {
    function wc_price( float $price, array $args = [] ): string {
        return 'R$&nbsp;' . number_format( $price, 2, ',', '.' );
    }
}

if ( ! function_exists( 'wc_format_decimal' ) ) {
    function wc_format_decimal( float|string $number, int $dp = false ): string {
        return $dp !== false ? number_format( (float) $number, $dp, '.', '' ) : (string) (float) $number;
    }
}

if ( ! function_exists( 'wc_add_notice' ) ) {
    function wc_add_notice( string $message, string $notice_type = 'success' ): void {
        $GLOBALS['_sz_wc_notices'][] = [ 'message' => $message, 'type' => $notice_type ];
    }
}

if ( ! function_exists( 'wc_get_notices' ) ) {
    function wc_get_notices( string $notice_type = '' ): array {
        $notices = $GLOBALS['_sz_wc_notices'] ?? [];
        if ( $notice_type ) {
            return array_filter( $notices, fn( $n ) => $n['type'] === $notice_type );
        }
        return $notices;
    }
}

if ( ! function_exists( 'wc_get_logger' ) ) {
    function wc_get_logger(): object {
        return new class {
            public function log( string $level, string $message, array $context = [] ): void {
                error_log( "[{$level}] " . ( $context['source'] ?? 'wc' ) . ": {$message}" );
            }
            public function debug( string $m, array $c = [] ): void { $this->log( 'DEBUG', $m, $c ); }
            public function info( string $m, array $c = [] ):  void { $this->log( 'INFO', $m, $c ); }
            public function warning( string $m, array $c = [] ): void { $this->log( 'WARNING', $m, $c ); }
            public function error( string $m, array $c = [] ):  void { $this->log( 'ERROR', $m, $c ); }
        };
    }
}

if ( ! function_exists( 'wc_clean' ) ) {
    function wc_clean( mixed $value ): mixed {
        if ( is_array( $value ) ) return array_map( 'wc_clean', $value );
        return sanitize_text_field( wp_unslash( (string) $value ) );
    }
}

if ( ! function_exists( 'wc_maybe_increase_stock_levels' ) ) {
    function wc_maybe_increase_stock_levels( WC_Order $order ): void {}
}

if ( ! function_exists( 'wc_update_product_stock' ) ) {
    function wc_update_product_stock( int|WC_Product $product, int $stock_quantity = null, string $operation = 'set', bool $updating = false ): int|false {
        $id = is_object( $product ) ? $product->get_id() : $product;
        update_post_meta( $id, '_stock', $stock_quantity );
        return $stock_quantity;
    }
}

if ( ! function_exists( 'wc_get_order_statuses' ) ) {
    function wc_get_order_statuses(): array {
        return [
            'wc-pending'    => 'Aguardando pagamento',
            'wc-processing' => 'Em processamento',
            'wc-on-hold'    => 'Aguardando',
            'wc-completed'  => 'Concluído',
            'wc-cancelled'  => 'Cancelado',
            'wc-refunded'   => 'Reembolsado',
            'wc-failed'     => 'Falhou',
        ];
    }
}

if ( ! function_exists( 'wc_status_slug' ) ) {
    function wc_status_slug( string $status ): string {
        return str_replace( 'wc-', '', $status );
    }
}

if ( ! function_exists( 'wc_format_datetime' ) ) {
    function wc_format_datetime( mixed $date, string $format = '' ): string {
        if ( is_object( $date ) ) return wp_date( $format ?: 'd/m/Y H:i', $date->timestamp );
        return is_numeric( $date ) ? wp_date( $format ?: 'd/m/Y H:i', $date ) : (string) $date;
    }
}

if ( ! function_exists( 'wc_get_products' ) ) {
    function wc_get_products( array $args = [] ): array {
        global $wpdb;
        $limit = (int) ( $args['limit'] ?? 20 );
        $ids   = $wpdb->get_col( "SELECT ID FROM {$wpdb->prefix}posts WHERE post_type = 'product' AND post_status = 'publish' LIMIT {$limit}" );
        return array_map( 'wc_get_product', array_map( 'intval', $ids ) );
    }
}

// ── WC constants ──────────────────────────────────────────────────────────────

if ( ! defined( 'WC_ABSPATH' ) )  define( 'WC_ABSPATH', defined( 'ABSPATH' ) ? ABSPATH . 'wp-content/plugins/woocommerce/' : '' );
if ( ! defined( 'WC_VERSION' ) )  define( 'WC_VERSION', '8.0.0' );
if ( ! defined( 'WC_TAX_ROUNDING_MODE' ) ) define( 'WC_TAX_ROUNDING_MODE', 'auto' );

// WC background process base class stub
if ( ! class_exists( 'WC_Background_Process' ) ) {
    abstract class WC_Background_Process {
        protected string $action = 'wc_bg_process';
        public function push_to_queue( mixed $data ): static { return $this; }
        public function save(): static { return $this; }
        public function dispatch(): void {}
        abstract protected function task( mixed $item ): mixed;
    }
}
