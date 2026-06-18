<?php
/**
 * Senderzz_Order_Meta — Camada canônica de leitura/gravação de order meta.
 *
 * REGRAS:
 *  - Leitura: tenta campo canônico primeiro, depois fallbacks legados em ordem de prioridade.
 *  - Escrita: grava campo canônico + todos os aliases legados (compatibilidade total).
 *  - HPOS: usa $order->get_meta() / update_meta_data() quando objeto WC_Order disponível.
 *  - Nunca apaga campos antigos.
 *  - Zero alteração de regra financeira.
 *
 * Uso rápido (estático, aceita int ou WC_Order):
 *   $taxa = Senderzz_Order_Meta::get_fee_total( $order_or_id );
 *   Senderzz_Order_Meta::set_fee_total( $order_or_id, 36.46 );
 *
 * @package Senderzz
 * @since   2.6.11-v459
 */

defined( 'ABSPATH' ) || exit;

if ( class_exists( 'Senderzz_Order_Meta' ) ) return;

class Senderzz_Order_Meta {

    // ── Campos canônicos ────────────────────────────────────────────────────────
    const FEE_TOTAL              = '_senderzz_fee_total';
    const FEE_DELIVERY           = '_senderzz_fee_delivery';
    const FEE_TRANSACTION        = '_senderzz_fee_transaction';
    const FEE_PERCENT            = '_senderzz_fee_percent';
    const DELIVERY_DATE          = '_senderzz_delivery_date';
    const MOTOBOY_STATUS         = '_senderzz_motoboy_status';
    const ORDER_GROSS_TOTAL      = '_senderzz_order_gross_total';
    const AFFILIATE_COMMISSION   = '_senderzz_affiliate_commission';
    const AFFILIATE_COMM_PCT     = '_senderzz_affiliate_commission_pct';
    const PRODUCER_USER_ID       = '_senderzz_producer_user_id';
    const AFFILIATE_ID           = '_senderzz_affiliate_id';
    const AFFILIATE_USER_ID      = '_senderzz_affiliate_user_id';

    // ── Fallback maps (canônico → legados, em ordem de prioridade) ──────────────
    private static array $READ_FALLBACKS = [
        self::FEE_TOTAL => [
            '_senderzz_fee_total',
            '_sz_mb_taxa_total',
            '_sz_taxa_total',
            '_senderzz_shipping_charged',
            '_senderzz_shipping_real_cost',
        ],
        self::FEE_DELIVERY => [
            '_senderzz_fee_delivery',
            '_sz_mb_taxa_entrega',
            '_sz_taxa_entrega',
        ],
        self::FEE_TRANSACTION => [
            '_senderzz_fee_transaction',
            '_sz_mb_taxa_adicional',
            '_sz_taxa_adicional',
        ],
        self::FEE_PERCENT => [
            '_senderzz_fee_percent',
            '_sz_mb_taxa_percentual',
        ],
        self::DELIVERY_DATE => [
            '_senderzz_delivery_date',
            '_sz_delivery_date',
            '_sz_motoboy_entrega_data',
        ],
        self::MOTOBOY_STATUS => [
            '_senderzz_motoboy_status',
            '_senderzz_motoboy_flow_status',
        ],
        self::ORDER_GROSS_TOTAL => [
            '_senderzz_order_gross_total',
            '_senderzz_offer_value',
            '_sz_aff_order_total',
        ],
        self::AFFILIATE_COMMISSION => [
            '_senderzz_affiliate_commission',
            '_sz_aff_commission',
        ],
        self::AFFILIATE_COMM_PCT => [
            '_senderzz_affiliate_commission_pct',
            '_sz_aff_commission_pct',
        ],
        self::PRODUCER_USER_ID => [
            '_senderzz_producer_user_id',
            '_senderzz_owner_user_id',
            '_sz_aff_producer_id',
        ],
        self::AFFILIATE_ID => [
            '_senderzz_affiliate_id',
            '_sz_affiliate_id',
            '_sz_affiliate_ref',
        ],
        self::AFFILIATE_USER_ID => [
            '_senderzz_affiliate_user_id',
            '_sz_affiliate_user_id',
        ],
    ];

    // ── Aliases a escrever em conjunto com o canônico ───────────────────────────
    private static array $WRITE_ALIASES = [
        self::FEE_TOTAL => [
            '_sz_mb_taxa_total',
            '_sz_taxa_total',
            '_senderzz_shipping_charged',
            '_senderzz_shipping_real_cost',
        ],
        self::FEE_DELIVERY => [
            '_sz_mb_taxa_entrega',
            '_sz_taxa_entrega',
        ],
        self::FEE_TRANSACTION => [
            '_sz_mb_taxa_adicional',
            '_sz_taxa_adicional',
        ],
        self::FEE_PERCENT => [
            '_sz_mb_taxa_percentual',
        ],
        self::DELIVERY_DATE => [
            '_sz_delivery_date',
            '_sz_motoboy_entrega_data',
        ],
        self::MOTOBOY_STATUS => [
            '_senderzz_motoboy_flow_status',
        ],
        self::ORDER_GROSS_TOTAL => [
            '_senderzz_offer_value',
            '_sz_aff_order_total',
        ],
        self::AFFILIATE_COMMISSION => [
            '_sz_aff_commission',
        ],
        self::AFFILIATE_COMM_PCT => [
            '_sz_aff_commission_pct',
        ],
        self::PRODUCER_USER_ID => [
            '_senderzz_owner_user_id',
            '_sz_aff_producer_id',
        ],
        self::AFFILIATE_ID => [
            '_sz_affiliate_id',
            '_sz_affiliate_ref',
        ],
        self::AFFILIATE_USER_ID => [
            '_sz_affiliate_user_id',
        ],
    ];

    // ══════════════════════════════════════════════════════════════════════════════
    // LEITURA GENÉRICA
    // ══════════════════════════════════════════════════════════════════════════════

    /**
     * Lê um meta canônico com fallback automático para campos legados.
     *
     * @param int|object $order_or_id
     * @param string       $canonical_key Constante de Senderzz_Order_Meta.
     * @param bool         $promote       Se true, grava canônico quando encontrar valor em legado.
     * @return mixed
     */
    public static function get( int|object $order_or_id, string $canonical_key, bool $promote = false ): mixed {
        $order = self::_order( $order_or_id );
        $fallbacks = self::$READ_FALLBACKS[ $canonical_key ] ?? [ $canonical_key ];

        foreach ( $fallbacks as $key ) {
            $v = $order ? $order->get_meta( $key, true ) : get_post_meta( self::_id( $order_or_id ), $key, true );
            if ( $v !== '' && $v !== null && $v !== false ) {
                // Se encontramos em campo legado, promover para canônico
                if ( $promote && $key !== $canonical_key ) {
                    self::_write_canonical_only( $order_or_id, $canonical_key, $v );
                    self::_log_divergence( self::_id( $order_or_id ), $canonical_key, '', $key, $v );
                }
                return $v;
            }
        }
        return '';
    }

    // ══════════════════════════════════════════════════════════════════════════════
    // ESCRITA GENÉRICA
    // ══════════════════════════════════════════════════════════════════════════════

    /**
     * Escreve campo canônico + todos os aliases legados.
     *
     * @param int|object $order_or_id
     * @param string       $canonical_key
     * @param mixed        $value
     * @param bool         $save_order Se true (default), chama $order->save().
     */
    public static function set( int|object $order_or_id, string $canonical_key, mixed $value, bool $save_order = true ): void {
        $order = self::_order( $order_or_id );
        $all   = array_merge( [ $canonical_key ], self::$WRITE_ALIASES[ $canonical_key ] ?? [] );

        if ( $order ) {
            foreach ( $all as $key ) {
                $order->update_meta_data( $key, $value );
            }
            if ( $save_order ) $order->save();
        } else {
            $id = self::_id( $order_or_id );
            foreach ( $all as $key ) {
                update_post_meta( $id, $key, $value );
            }
        }
    }

    // ══════════════════════════════════════════════════════════════════════════════
    // ATALHOS TIPADOS
    // ══════════════════════════════════════════════════════════════════════════════

    public static function get_fee_total( int|object $o ): float          { return (float) self::get( $o, self::FEE_TOTAL ); }
    public static function set_fee_total( int|object $o, float $v, bool $save = true ): void { self::set( $o, self::FEE_TOTAL, round( $v, 2 ), $save ); }

    public static function get_fee_delivery( int|object $o ): float        { return (float) self::get( $o, self::FEE_DELIVERY ); }
    public static function set_fee_delivery( int|object $o, float $v, bool $save = true ): void { self::set( $o, self::FEE_DELIVERY, round( $v, 2 ), $save ); }

    public static function get_fee_transaction( int|object $o ): float     { return (float) self::get( $o, self::FEE_TRANSACTION ); }
    public static function set_fee_transaction( int|object $o, float $v, bool $save = true ): void { self::set( $o, self::FEE_TRANSACTION, round( $v, 2 ), $save ); }

    public static function get_fee_percent( int|object $o ): float          { return (float) self::get( $o, self::FEE_PERCENT ); }
    public static function set_fee_percent( int|object $o, float $v, bool $save = true ): void { self::set( $o, self::FEE_PERCENT, round( $v, 4 ), $save ); }

    public static function get_delivery_date( int|object $o ): string       { return (string) self::get( $o, self::DELIVERY_DATE ); }
    public static function set_delivery_date( int|object $o, string $v, bool $save = true ): void { self::set( $o, self::DELIVERY_DATE, $v, $save ); }

    public static function get_motoboy_status( int|object $o ): string      { return (string) self::get( $o, self::MOTOBOY_STATUS ); }
    public static function set_motoboy_status( int|object $o, string $v, bool $save = true ): void { self::set( $o, self::MOTOBOY_STATUS, $v, $save ); }

    public static function get_order_gross_total( int|object $o ): float    { return (float) self::get( $o, self::ORDER_GROSS_TOTAL ); }
    public static function set_order_gross_total( int|object $o, float $v, bool $save = true ): void { self::set( $o, self::ORDER_GROSS_TOTAL, round( $v, 2 ), $save ); }

    public static function get_affiliate_commission( int|object $o ): float { return (float) self::get( $o, self::AFFILIATE_COMMISSION ); }
    public static function set_affiliate_commission( int|object $o, float $v, bool $save = true ): void { self::set( $o, self::AFFILIATE_COMMISSION, round( $v, 2 ), $save ); }

    public static function get_affiliate_commission_pct( int|object $o ): float { return (float) self::get( $o, self::AFFILIATE_COMM_PCT ); }
    public static function set_affiliate_commission_pct( int|object $o, float $v, bool $save = true ): void { self::set( $o, self::AFFILIATE_COMM_PCT, round( $v, 4 ), $save ); }

    public static function get_producer_user_id( int|object $o ): int       { return (int) self::get( $o, self::PRODUCER_USER_ID ); }
    public static function set_producer_user_id( int|object $o, int $v, bool $save = true ): void { self::set( $o, self::PRODUCER_USER_ID, $v, $save ); }

    public static function get_affiliate_id( int|object $o ): int            { return (int) self::get( $o, self::AFFILIATE_ID ); }
    public static function set_affiliate_id( int|object $o, int $v, bool $save = true ): void { self::set( $o, self::AFFILIATE_ID, $v, $save ); }

    public static function get_affiliate_user_id( int|object $o ): int       { return (int) self::get( $o, self::AFFILIATE_USER_ID ); }
    public static function set_affiliate_user_id( int|object $o, int $v, bool $save = true ): void { self::set( $o, self::AFFILIATE_USER_ID, $v, $save ); }

    // ══════════════════════════════════════════════════════════════════════════════
    // NORMALIZAÇÃO RETROATIVA (manual, em lotes, nunca apaga dados)
    // ══════════════════════════════════════════════════════════════════════════════

    /**
     * Registra a action WP para normalização manual:
     *   do_action('senderzz_normalize_order_meta', $args);
     *   ou via WP-CLI:
     *   wp eval "do_action('senderzz_normalize_order_meta', ['batch_size'=>50, 'dry_run'=>false]);"
     */
    public static function register_normalization_hook(): void {
        add_action( 'senderzz_normalize_order_meta', [ __CLASS__, 'run_normalization' ] );
        // Admin manual trigger
        add_action( 'admin_post_senderzz_normalize_meta', [ __CLASS__, 'admin_normalize_handler' ] );
    }

    /**
     * Handler chamado via POST /wp-admin/admin-post.php?action=senderzz_normalize_meta
     * Requer: manage_woocommerce capability + nonce sz_normalize_meta.
     */
    public static function admin_normalize_handler(): void {
        if ( ! current_user_can( 'manage_woocommerce' ) ) {
            wp_die( 'Acesso negado.', 403 );
        }
        check_admin_referer( 'sz_normalize_meta' );

        $dry   = isset( $_POST['dry_run'] );
        $batch = max( 10, min( 500, (int) ( $_POST['batch_size'] ?? 50 ) ) );
        $from  = max( 0, (int) ( $_POST['offset'] ?? 0 ) );

        $result = self::run_normalization( [
            'batch_size' => $batch,
            'offset'     => $from,
            'dry_run'    => $dry,
        ] );

        wp_redirect( add_query_arg( [
            'page'             => 'senderzz-settings',
            'sz_norm_done'     => $result['processed'],
            'sz_norm_updated'  => $result['updated'],
            'sz_norm_next'     => $result['next_offset'],
        ], admin_url( 'admin.php' ) ) );
        exit;
    }

    /**
     * Executa a normalização.
     *
     * @param array{batch_size?:int, offset?:int, dry_run?:bool} $args
     * @return array{processed:int, updated:int, next_offset:int, log:array}
     */
    public static function run_normalization( array $args = [] ): array {
        $batch_size  = (int) ( $args['batch_size'] ?? 50 );
        $offset      = (int) ( $args['offset']     ?? 0  );
        $dry_run     = (bool) ( $args['dry_run']   ?? false );

        $processed = 0;
        $updated   = 0;
        $log       = [];

        // Busca pedidos (HPOS + legacy)
        $order_ids = self::_get_order_ids( $batch_size, $offset );

        foreach ( $order_ids as $order_id ) {
            $order  = function_exists( 'wc_get_order' ) ? wc_get_order( $order_id ) : null;
            $needs_save = false;
            $processed++;

            foreach ( self::$READ_FALLBACKS as $canonical => $fallbacks ) {
                // Valor atual no canônico
                $canonical_val = $order
                    ? $order->get_meta( $canonical, true )
                    : get_post_meta( $order_id, $canonical, true );

                if ( $canonical_val !== '' && $canonical_val !== null ) {
                    // Canônico já preenchido — verificar divergências com legados
                    foreach ( array_slice( $fallbacks, 1 ) as $legacy ) {
                        $legacy_val = $order
                            ? $order->get_meta( $legacy, true )
                            : get_post_meta( $order_id, $legacy, true );
                        if ( $legacy_val !== '' && $legacy_val !== null && $legacy_val != $canonical_val ) {
                            $log[] = [
                                'order_id'        => $order_id,
                                'canonical'       => $canonical,
                                'canonical_value' => $canonical_val,
                                'legacy_field'    => $legacy,
                                'legacy_value'    => $legacy_val,
                                'action'          => 'divergence',
                            ];
                            self::_log_divergence( $order_id, $canonical, $canonical_val, $legacy, $legacy_val );
                        }
                    }
                    continue;
                }

                // Canônico vazio — tentar preencher com fallback
                foreach ( array_slice( $fallbacks, 1 ) as $legacy ) {
                    $legacy_val = $order
                        ? $order->get_meta( $legacy, true )
                        : get_post_meta( $order_id, $legacy, true );
                    if ( $legacy_val !== '' && $legacy_val !== null ) {
                        $log[] = [
                            'order_id'   => $order_id,
                            'canonical'  => $canonical,
                            'from_field' => $legacy,
                            'value'      => $legacy_val,
                            'action'     => $dry_run ? 'would_fill' : 'filled',
                        ];
                        if ( ! $dry_run ) {
                            if ( $order ) {
                                $order->update_meta_data( $canonical, $legacy_val );
                                $needs_save = true;
                            } else {
                                update_post_meta( $order_id, $canonical, $legacy_val );
                            }
                            $updated++;
                        }
                        break;
                    }
                }
            }

            if ( ! $dry_run && $needs_save && $order ) {
                $order->save();
            }
        }

        // Gravar log de divergências em option (últimas 1000 entradas)
        if ( ! empty( $log ) ) {
            $stored = array_slice(
                array_merge( (array) get_option( 'senderzz_meta_norm_log', [] ), $log ),
                -1000
            );
            update_option( 'senderzz_meta_norm_log', $stored, false );
        }

        return [
            'processed'   => $processed,
            'updated'     => $updated,
            'next_offset' => $offset + $processed,
            'log'         => $log,
        ];
    }

    /**
     * Retorna relatório das divergências registradas.
     */
    public static function get_divergence_report(): array {
        return (array) get_option( 'senderzz_meta_norm_log', [] );
    }

    /**
     * Limpa relatório de divergências.
     */
    public static function clear_divergence_report(): void {
        delete_option( 'senderzz_meta_norm_log' );
    }

    // ══════════════════════════════════════════════════════════════════════════════
    // PRIVADOS
    // ══════════════════════════════════════════════════════════════════════════════

    private static function _order( int|object $o ): ?object {
        if ( is_object( $o ) && method_exists( $o, 'get_meta' ) ) return $o;
        if ( is_int( $o ) && function_exists( 'wc_get_order' ) ) {
            $order = wc_get_order( $o );
            return ( $order && method_exists( $order, 'get_meta' ) ) ? $order : null;
        }
        return null;
    }

    private static function _id( int|object $o ): int {
        if ( is_object( $o ) && method_exists( $o, 'get_id' ) ) return (int) $o->get_id();
        return (int) $o;
    }

    private static function _write_canonical_only( int|object $o, string $key, mixed $value ): void {
        $order = self::_order( $o );
        if ( $order ) {
            $order->update_meta_data( $key, $value );
            $order->save();
        } else {
            update_post_meta( self::_id( $o ), $key, $value );
        }
    }

    private static function _log_divergence( int $order_id, string $canonical, mixed $canonical_val, string $legacy, mixed $legacy_val ): void {
        if ( defined( 'WP_DEBUG_LOG' ) && WP_DEBUG_LOG ) {
            // phpcs:ignore WordPress.PHP.DevelopmentFunctions.error_log_error_log
            error_log( sprintf(
                '[senderzz_meta_divergence] order=%d canonical=%s(%s) legacy=%s(%s)',
                $order_id,
                $canonical,
                is_scalar( $canonical_val ) ? $canonical_val : wp_json_encode( $canonical_val ),
                $legacy,
                is_scalar( $legacy_val ) ? $legacy_val : wp_json_encode( $legacy_val )
            ) );
        }
    }

    private static function _get_order_ids( int $limit, int $offset ): array {
        global $wpdb;

        // HPOS
        $ids = $wpdb->get_col( $wpdb->prepare(
            "SELECT id FROM {$wpdb->prefix}wc_orders WHERE type='shop_order' ORDER BY id ASC LIMIT %d OFFSET %d",
            $limit, $offset
        ) );
        if ( ! empty( $ids ) ) return array_map( 'intval', $ids );

        // Legacy posts
        $ids = $wpdb->get_col( $wpdb->prepare(
            "SELECT ID FROM {$wpdb->prefix}posts WHERE post_type='shop_order' ORDER BY ID ASC LIMIT %d OFFSET %d",
            $limit, $offset
        ) );
        return array_map( 'intval', $ids );
    }
}
