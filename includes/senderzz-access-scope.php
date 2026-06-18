<?php
/**
 * Senderzz Access Scope Guard
 *
 * Camada única de permissão para impedir vazamento de pedidos entre produtor,
 * afiliado, operador e motoboy. Todas as telas/AJAX devem consultar estas
 * funções antes de listar, detalhar, exportar ou alterar pedidos.
 */
defined( 'ABSPATH' ) || exit;

if ( ! function_exists( 'senderzz_scope_normalize_ids' ) ) {
    function senderzz_scope_normalize_ids( $ids ): array {
        return array_values( array_unique( array_filter( array_map( 'absint', (array) $ids ) ) ) );
    }
}

if ( ! function_exists( 'senderzz_scope_portal_wp_user_id' ) ) {
    function senderzz_scope_portal_wp_user_id( $user ): int {
        if ( is_numeric( $user ) ) return absint( $user );
        if ( is_object( $user ) ) {
            if ( function_exists( 'sz_aff_portal_user_wp_id' ) ) {
                $id = (int) sz_aff_portal_user_wp_id( $user );
                if ( $id > 0 ) return $id;
            }
            foreach ( [ 'wp_user_id', 'user_id', 'ID', 'id' ] as $key ) {
                if ( isset( $user->{$key} ) && (int) $user->{$key} > 0 ) return (int) $user->{$key};
            }
            $email = sanitize_email( $user->email ?? $user->user_email ?? '' );
            if ( $email ) {
                $wp_user = get_user_by( 'email', $email );
                if ( $wp_user ) return (int) $wp_user->ID;
            }
        }
        return get_current_user_id();
    }
}

if ( ! function_exists( 'senderzz_current_user_order_scope' ) ) {
    function senderzz_current_user_order_scope( $user = null ): array {
        $user = $user ?: ( is_user_logged_in() ? wp_get_current_user() : null );
        $wp_user_id = senderzz_scope_portal_wp_user_id( $user );
        $role = is_object( $user ) ? strtolower( trim( (string) ( $user->role ?? '' ) ) ) : '';
        $wp_roles = [];
        if ( $wp_user_id > 0 ) {
            $wp_user = get_user_by( 'id', $wp_user_id );
            if ( $wp_user ) $wp_roles = (array) $wp_user->roles;
        }

        $is_admin = $wp_user_id > 0 && ( user_can( $wp_user_id, 'manage_options' ) || user_can( $wp_user_id, 'manage_woocommerce' ) );
        if ( $is_admin ) {
            return [ 'type' => 'admin', 'wp_user_id' => $wp_user_id, 'affiliate_ids' => [], 'class_ids' => [], 'raw_role' => $role ];
        }

        $affiliate_ids = [];
        if ( function_exists( 'sz_aff_get_affiliate_ids_for_portal_user' ) && is_object( $user ) ) {
            $affiliate_ids = sz_aff_get_affiliate_ids_for_portal_user( $user, 'active' );
        } elseif ( $wp_user_id > 0 && function_exists( 'sz_aff_table' ) ) {
            global $wpdb;
            $affiliate_ids = $wpdb->get_col( $wpdb->prepare(
                "SELECT id FROM " . sz_aff_table( 'sz_affiliates' ) . " WHERE user_id=%d AND status='active' AND deleted_at IS NULL",
                $wp_user_id
            ) );
        }
        $affiliate_ids = senderzz_scope_normalize_ids( $affiliate_ids );

        $must_affiliate = false;
        if ( is_object( $user ) ) {
            if ( function_exists( 'sz_aff_portal_user_must_use_affiliate_scope' ) ) {
                $must_affiliate = (bool) sz_aff_portal_user_must_use_affiliate_scope( $user );
            } elseif ( in_array( $role, [ 'affiliate', 'afiliado' ], true ) ) {
                $must_affiliate = true;
            }
        }
        if ( $must_affiliate || in_array( 'affiliate', $wp_roles, true ) || in_array( 'afiliado', $wp_roles, true ) ) {
            return [ 'type' => 'affiliate', 'wp_user_id' => $wp_user_id, 'affiliate_ids' => $affiliate_ids, 'class_ids' => [], 'raw_role' => $role ];
        }

        $class_ids = [];
        if ( is_object( $user ) && function_exists( 'sz_get_user_class_ids' ) ) {
            $class_ids = sz_get_user_class_ids( $user );
        }
        if ( is_object( $user ) && ! empty( $user->shipping_class_id ) ) {
            $class_ids[] = (int) $user->shipping_class_id;
        }
        $class_ids = senderzz_scope_normalize_ids( $class_ids );

        if ( in_array( $role, [ 'motoboy', 'driver' ], true ) || in_array( 'motoboy', $wp_roles, true ) ) {
            return [ 'type' => 'motoboy', 'wp_user_id' => $wp_user_id, 'affiliate_ids' => [], 'class_ids' => $class_ids, 'raw_role' => $role ];
        }
        if ( in_array( $role, [ 'operator', 'operador', 'logistic_operator' ], true ) || in_array( 'operator', $wp_roles, true ) ) {
            return [ 'type' => 'operator', 'wp_user_id' => $wp_user_id, 'affiliate_ids' => [], 'class_ids' => $class_ids, 'raw_role' => $role ];
        }

        return [ 'type' => 'producer', 'wp_user_id' => $wp_user_id, 'affiliate_ids' => [], 'class_ids' => $class_ids, 'raw_role' => $role ];
    }
}

if ( ! function_exists( 'senderzz_order_meta_int' ) ) {
    function senderzz_order_meta_int( WC_Order $order, array $keys ): int {
        foreach ( $keys as $key ) {
            $v = absint( $order->get_meta( $key, true ) );
            if ( $v > 0 ) return $v;
        }
        return 0;
    }
}

if ( ! function_exists( 'senderzz_user_can_access_order' ) ) {
    function senderzz_user_can_access_order( int $order_id, $user = null, string $action = 'view' ): bool {
        if ( $order_id <= 0 || ! function_exists( 'wc_get_order' ) ) return false;
        $order = wc_get_order( $order_id );
        if ( ! $order instanceof WC_Order ) return false;
        $scope = senderzz_current_user_order_scope( $user );

        if ( ( $scope['type'] ?? '' ) === 'admin' ) return true;

        if ( ( $scope['type'] ?? '' ) === 'affiliate' ) {
            // Afiliado nunca altera pedido operacional do produtor; apenas visualiza os próprios.
            if ( in_array( $action, [ 'approve', 'cancel', 'status', 'edit', 'delete' ], true ) ) return false;
            $affiliate_ids = senderzz_scope_normalize_ids( $scope['affiliate_ids'] ?? [] );
            $wp_user_id    = absint( $scope['wp_user_id'] ?? 0 );
            $order_aff_id  = senderzz_order_meta_int( $order, [ '_sz_affiliate_id', '_sz_affiliate_ref' ] );
            $order_aff_uid = senderzz_order_meta_int( $order, [ '_sz_affiliate_user_id' ] );
            return ( $order_aff_id > 0 && in_array( $order_aff_id, $affiliate_ids, true ) )
                || ( $wp_user_id > 0 && $order_aff_uid === $wp_user_id );
        }

        if ( in_array( $scope['type'] ?? '', [ 'producer', 'operator' ], true ) ) {
            $class_ids = senderzz_scope_normalize_ids( $scope['class_ids'] ?? [] );
            $order_class = senderzz_order_meta_int( $order, [ '_senderzz_product_shipping_class_id', '_shipping_class_id', '_sz_shipping_class_id' ] );
            if ( $order_class > 0 && in_array( $order_class, $class_ids, true ) ) return true;

            $wp_user_id = absint( $scope['wp_user_id'] ?? 0 );
            $owner_id = senderzz_order_meta_int( $order, [ '_senderzz_owner_user_id', '_senderzz_wp_user_id', '_senderzz_customer_id', '_producer_id', '_sz_producer_id', '_senderzz_producer_id', '_sz_aff_producer_id' ] );
            return $wp_user_id > 0 && $owner_id === $wp_user_id;
        }

        if ( ( $scope['type'] ?? '' ) === 'motoboy' ) {
            $wp_user_id = absint( $scope['wp_user_id'] ?? 0 );
            $driver_id = senderzz_order_meta_int( $order, [ '_sz_motoboy_user_id', '_senderzz_motoboy_user_id', '_motoboy_user_id' ] );
            return $wp_user_id > 0 && $driver_id === $wp_user_id;
        }

        return false;
    }
}

if ( ! function_exists( 'senderzz_get_visible_order_ids_for_user' ) ) {
    function senderzz_get_visible_order_ids_for_user( $user, int $limit = 200 ): array {
        $limit = max( 1, min( 500, $limit ) );
        $scope = senderzz_current_user_order_scope( $user );

        if ( ( $scope['type'] ?? '' ) === 'affiliate' ) {
            if ( function_exists( 'sz_aff_get_affiliate_order_ids_for_portal_user' ) ) {
                return senderzz_scope_normalize_ids( sz_aff_get_affiliate_order_ids_for_portal_user( $user, $limit ) );
            }
            if ( ! function_exists( 'wc_get_orders' ) ) return [];
            $meta_query = [ 'relation' => 'OR' ];
            foreach ( senderzz_scope_normalize_ids( $scope['affiliate_ids'] ?? [] ) as $aid ) {
                $meta_query[] = [ 'key' => '_sz_affiliate_id', 'value' => (string) $aid, 'compare' => '=' ];
                $meta_query[] = [ 'key' => '_sz_affiliate_ref', 'value' => (string) $aid, 'compare' => '=' ];
            }
            if ( absint( $scope['wp_user_id'] ?? 0 ) > 0 ) {
                $meta_query[] = [ 'key' => '_sz_affiliate_user_id', 'value' => (string) absint( $scope['wp_user_id'] ), 'compare' => '=' ];
            }
            if ( count( $meta_query ) <= 1 ) return [];
            return senderzz_scope_normalize_ids( wc_get_orders( [ 'limit' => $limit, 'orderby' => 'date', 'order' => 'DESC', 'return' => 'ids', 'meta_query' => $meta_query ] ) );
        }

        if ( ( $scope['type'] ?? '' ) === 'admin' && function_exists( 'wc_get_orders' ) ) {
            return senderzz_scope_normalize_ids( wc_get_orders( [ 'limit' => $limit, 'orderby' => 'date', 'order' => 'DESC', 'return' => 'ids' ] ) );
        }

        if ( class_exists( 'WC_MelhorEnvio\\Portal\\Portal_Orders' ) && ! empty( $scope['class_ids'] ) ) {
            $orders = WC_MelhorEnvio\Portal\Portal_Orders::get_orders( $scope['class_ids'], 1, $limit );
            return senderzz_scope_normalize_ids( array_map( static fn( $o ) => (int) ( $o['id'] ?? 0 ), (array) $orders ) );
        }

        return [];
    }
}

if ( ! function_exists( 'senderzz_get_visible_orders_for_user' ) ) {
    function senderzz_get_visible_orders_for_user( $user, int $limit = 200 ): array {
        $orders = [];
        if ( ! function_exists( 'wc_get_order' ) ) return $orders;
        foreach ( senderzz_get_visible_order_ids_for_user( $user, $limit ) as $order_id ) {
            if ( ! senderzz_user_can_access_order( (int) $order_id, $user, 'view' ) ) continue;
            $order = wc_get_order( (int) $order_id );
            if ( ! $order instanceof WC_Order ) continue;
            if ( class_exists( 'WC_MelhorEnvio\\Portal\\Portal_Orders' ) ) {
                $orders[] = WC_MelhorEnvio\Portal\Portal_Orders::format_order( $order );
            } else {
                $orders[] = [ 'id' => $order->get_id(), 'order_number' => $order->get_order_number(), 'status' => $order->get_status(), 'total' => $order->get_total() ];
            }
        }
        return $orders;
    }
}

if ( ! function_exists( 'senderzz_affiliate_wallet_summary_scoped' ) ) {
    function senderzz_affiliate_wallet_summary_scoped( $user, int $history_limit = 8 ): array {
        global $wpdb;
        $summary = [ 'available' => 0.0, 'pending' => 0.0, 'analysis' => 0.0, 'future' => 0.0, 'history' => [] ];
        $scope = senderzz_current_user_order_scope( $user );
        if ( ( $scope['type'] ?? '' ) !== 'affiliate' || ! function_exists( 'sz_aff_table' ) ) return $summary;
        $aff_ids = senderzz_scope_normalize_ids( $scope['affiliate_ids'] ?? [] );
        if ( empty( $aff_ids ) ) return $summary;
        $ids_sql = implode( ',', $aff_ids );
        $tx_table = sz_aff_table( 'sz_affiliate_transactions' );
        $hpos_meta = $wpdb->prefix . 'wc_orders_meta';
        $postmeta  = $wpdb->postmeta;
        $meta_table = $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $hpos_meta ) ) === $hpos_meta ? $hpos_meta : $postmeta;
        $order_col = $meta_table === $hpos_meta ? 'order_id' : 'post_id';

        $rows = $wpdb->get_results(
            "SELECT t.id,t.affiliate_id,t.order_id,t.amount,t.status,t.type,t.created_at
             FROM {$tx_table} t
             INNER JOIN {$meta_table} m ON m.{$order_col}=t.order_id AND m.meta_key='_sz_affiliate_id' AND CAST(m.meta_value AS UNSIGNED)=t.affiliate_id
             WHERE t.affiliate_id IN ({$ids_sql})
             ORDER BY t.created_at DESC
             LIMIT 300",
            ARRAY_A
        ) ?: [];

        foreach ( $rows as $tx ) {
            $amount = (float) ( $tx['amount'] ?? 0 );
            $status = (string) ( $tx['status'] ?? '' );
            if ( $status === 'available' ) $summary['available'] += $amount;
            elseif ( $status === 'pending' ) $summary['pending'] += $amount;
            elseif ( in_array( $status, [ 'analysis', 'withdrawal_analysis', 'em_analise' ], true ) ) $summary['analysis'] += abs( $amount );

            if ( count( $summary['history'] ) < $history_limit ) {
                $summary['history'][] = [
                    'date'        => ! empty( $tx['created_at'] ) ? date_i18n( 'd/m/Y H:i', strtotime( $tx['created_at'] ) ) : '',
                    'description' => ( ( $tx['type'] ?? '' ) === 'commission' ? 'Comissão de afiliado' : (string) ( $tx['type'] ?? '' ) ),
                    'order'       => ! empty( $tx['order_id'] ) ? '#' . (int) $tx['order_id'] : '—',
                    'movement'    => $status === 'available' ? 'Disponível' : 'Pendente',
                    'value'       => $amount,
                    'fee'         => 0,
                    'net'         => $amount,
                    'status'      => $status,
                ];
            }
        }
        $summary['available'] = round( max( 0, $summary['available'] ), 2 );
        $summary['pending']   = round( max( 0, $summary['pending'] ), 2 );
        $summary['analysis']  = round( max( 0, $summary['analysis'] ), 2 );
        $summary['future']    = $summary['pending'];
        return $summary;
    }
}

if ( ! function_exists( 'senderzz_affiliate_orphan_order_ids' ) ) {
    function senderzz_affiliate_orphan_order_ids( int $limit = 100 ): array {
        global $wpdb;
        $limit = max( 1, min( 500, $limit ) );
        $meta = $wpdb->prefix . 'wc_orders_meta';
        if ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $meta ) ) !== $meta ) return [];
        return senderzz_scope_normalize_ids( $wpdb->get_col( $wpdb->prepare(
            "SELECT DISTINCT source.order_id
             FROM {$meta} source
             WHERE source.meta_key IN ('_senderzz_checkout_link_id','_senderzz_offer_token')
               AND source.meta_value <> ''
               AND NOT EXISTS (SELECT 1 FROM {$meta} a WHERE a.order_id=source.order_id AND a.meta_key IN ('_sz_affiliate_id','_sz_affiliate_ref','_sz_affiliate_user_id') AND a.meta_value <> '')
             ORDER BY source.order_id DESC
             LIMIT %d",
            $limit
        ) ) );
    }
}

if ( ! function_exists( 'senderzz_repair_affiliate_order_bindings' ) ) {
    function senderzz_repair_affiliate_order_bindings( int $limit = 100 ): array {
        $fixed = 0; $skipped = 0; $errors = [];
        if ( ! function_exists( 'wc_get_order' ) || ! function_exists( 'sz_aff_attach_affiliate_to_order' ) ) return compact( 'fixed', 'skipped', 'errors' );
        foreach ( senderzz_affiliate_orphan_order_ids( $limit ) as $order_id ) {
            $order = wc_get_order( (int) $order_id );
            if ( ! $order instanceof WC_Order ) { $skipped++; continue; }
            $before = (int) $order->get_meta( '_sz_affiliate_id', true );
            try {
                sz_aff_attach_affiliate_to_order( $order, [] );
                $order = wc_get_order( (int) $order_id );
                $after = $order instanceof WC_Order ? (int) $order->get_meta( '_sz_affiliate_id', true ) : 0;
                if ( ! $before && $after ) $fixed++; else $skipped++;
            } catch ( Throwable $e ) {
                $errors[] = '#' . $order_id . ': ' . $e->getMessage();
            }
        }
        return compact( 'fixed', 'skipped', 'errors' );
    }
}

// v369: menu legado 'WooCommerce > Senderzz Auditoria Afiliados' removido.
// A auditoria/correções Senderzz devem ficar somente dentro do admin Senderzz central.

if ( ! function_exists( 'senderzz_render_affiliate_audit_page' ) ) {
    function senderzz_render_affiliate_audit_page(): void {
        if ( ! current_user_can( 'manage_woocommerce' ) && ! current_user_can( 'manage_options' ) ) wp_die( 'Sem permissão.' );
        $result = null;
        if ( isset( $_POST['senderzz_repair_affiliates'] ) && check_admin_referer( 'senderzz_repair_affiliates' ) ) {
            $result = senderzz_repair_affiliate_order_bindings( 300 );
        }
        $orphans = senderzz_affiliate_orphan_order_ids( 100 );
        echo '<div class="wrap"><h1>Senderzz — Auditoria de afiliados</h1>';
        if ( $result ) {
            echo '<div class="notice notice-info"><p>Correção executada. Corrigidos: <strong>' . esc_html( (string) $result['fixed'] ) . '</strong>. Ignorados: <strong>' . esc_html( (string) $result['skipped'] ) . '</strong>.</p></div>';
        }
        echo '<p>Lista pedidos que possuem origem de checkout/oferta, mas ainda não possuem vínculo direto de afiliado.</p>';
        echo '<form method="post">'; wp_nonce_field( 'senderzz_repair_affiliates' );
        submit_button( 'Tentar corrigir vínculos antigos sem afiliado', 'primary', 'senderzz_repair_affiliates', false );
        echo '</form><hr>';
        if ( empty( $orphans ) ) {
            echo '<p><strong>Nenhum pedido órfão encontrado.</strong></p>';
        } else {
            echo '<h2>Pedidos órfãos encontrados</h2><table class="widefat striped"><thead><tr><th>Pedido</th><th>Ação</th></tr></thead><tbody>';
            foreach ( $orphans as $id ) {
                echo '<tr><td>#' . esc_html( (string) $id ) . '</td><td><a href="' . esc_url( admin_url( 'post.php?post=' . (int) $id . '&action=edit' ) ) . '">Abrir pedido</a></td></tr>';
            }
            echo '</tbody></table>';
        }
        echo '</div>';
    }
}
