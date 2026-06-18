<?php
/**
 * Senderzz — Recomposição de estoque em cancelamento/exclusão + regras de cancelamento.
 *
 * Resolve:
 *  - Pedidos CANCELADOS ou EXCLUÍDOS recompõem o estoque (a Senderzz dá baixa manual
 *    na criação, então o restore nativo do Woo é bloqueado e precisa ser manual).
 *  - Recomposição RETROATIVA (pedidos já cancelados/excluídos antes deste patch).
 *  - Pedidos EXCLUÍDOS (trash/delete) nunca aparecem em etapa nenhuma para o cliente
 *    (remove a linha operacional do motoboy). Somente CANCELADOS continuam aparecendo.
 *  - Cancelamento só é aplicável quando o pedido está EMBALADO e, nesse caso, não gera
 *    custo para ninguém (zera taxas e comissões de produtor, Senderzz e afiliado).
 *
 * Idempotência: cada pedido só é recomposto uma vez (meta _senderzz_stock_restored).
 */

defined( 'ABSPATH' ) || exit;

if ( defined( 'SENDERZZ_CANCEL_RESTOCK_LOADED' ) ) return;
define( 'SENDERZZ_CANCEL_RESTOCK_LOADED', true );

/* ─────────────────────────────────────────────────────────────────────────────
 * 1. Recomposição idempotente de estoque
 * ──────────────────────────────────────────────────────────────────────────── */

if ( ! function_exists( 'senderzz_restore_stock_once' ) ) {
    /**
     * Repõe o estoque dos itens do pedido UMA única vez.
     * Retorna true se efetivamente repôs algo.
     */
    function senderzz_restore_stock_once( $order ): bool {
        $order = $order instanceof WC_Order ? $order : ( function_exists( 'wc_get_order' ) ? wc_get_order( $order ) : null );
        if ( ! $order instanceof WC_Order ) return false;

        // Só repõe pedidos que tiveram baixa de estoque.
        $manual_reduced = $order->get_meta( '_senderzz_manual_stock_reduced', true ) === 'yes';
        $wc_reduced     = false;
        $ds = $order->get_data_store();
        if ( $ds && method_exists( $ds, 'get_stock_reduced' ) ) {
            $wc_reduced = (bool) $ds->get_stock_reduced( $order->get_id() );
        }
        if ( ! $manual_reduced && ! $wc_reduced ) return false;

        // Idempotência: nunca repõe duas vezes.
        if ( $order->get_meta( '_senderzz_stock_restored', true ) === 'yes' ) return false;

        // Nova regra de custódia Motoboy: se o pacote saiu do CD, cancelamento/exclusão
        // não pode devolver estoque automaticamente. Só volta com QR lido pelo OL no CD.
        global $wpdb;
        $custody_table = $wpdb->prefix . 'sz_motoboy_stock_custody';
        if ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $custody_table ) ) === $custody_table ) {
            $active_custody = (int) $wpdb->get_var( $wpdb->prepare(
                "SELECT COUNT(*) FROM {$custody_table} WHERE wc_order_id=%d AND physical_status IN ('with_motoboy','frustrated')",
                (int) $order->get_id()
            ) );
            if ( $active_custody > 0 ) {
                $order->update_meta_data( '_senderzz_stock_restore_waiting_return_scan', 'yes' );
                $order->update_meta_data( '_senderzz_stock_restore_waiting_return_scan_at', current_time( 'mysql' ) );
                $order->add_order_note( 'Senderzz COD: cancelamento/exclusão não recompôs estoque porque o pacote está em custódia do motoboy. Reposição somente após QR de devolução no CD.' );
                $order->save();
                return false;
            }
        }

        $reposed = false;
        foreach ( $order->get_items() as $item ) {
            if ( ! ( $item instanceof WC_Order_Item_Product ) ) continue;
            $product = $item->get_product();
            if ( ! $product || ! $product->managing_stock() ) continue;
            $qty = (int) $item->get_quantity();
            if ( $qty <= 0 ) continue;
            if ( function_exists( 'wc_update_product_stock' ) ) {
                wc_update_product_stock( $product, $qty, 'increase' );
                $reposed = true;
            }
        }

        // Marca como reposto e zera os flags de baixa (permite re-baixa se o pedido voltar).
        $order->update_meta_data( '_senderzz_stock_restored', 'yes' );
        $order->update_meta_data( '_senderzz_stock_restored_at', current_time( 'mysql' ) );
        $order->update_meta_data( '_senderzz_manual_stock_reduced', 'no' );
        if ( $ds && method_exists( $ds, 'set_stock_reduced' ) ) {
            $ds->set_stock_reduced( $order->get_id(), false );
        }
        $order->add_order_note( 'Senderzz: estoque recomposto (pedido cancelado/excluído).' );
        $order->save();

        if ( function_exists( 'senderzz_me_log' ) ) {
            senderzz_me_log( 'stock.restored', [ 'order_id' => $order->get_id(), 'reposed' => $reposed ] );
        }
        return $reposed;
    }
}

// Bloqueia o restore nativo do Woo para pedidos de oferta Senderzz (baixa é manual),
// espelhando o bloqueio já existente em woocommerce_can_reduce_order_stock.
add_filter( 'woocommerce_can_restore_order_stock', function ( $can_restore, $order ) {
    $order = $order instanceof WC_Order ? $order : ( function_exists( 'wc_get_order' ) ? wc_get_order( $order ) : null );
    if ( ! $order instanceof WC_Order ) return $can_restore;
    if ( function_exists( 'senderzz_sz_offer_order_has_token' ) && senderzz_sz_offer_order_has_token( $order ) ) {
        return false; // Senderzz controla a reposição manualmente.
    }
    return $can_restore;
}, 9999, 2 );

/* ─────────────────────────────────────────────────────────────────────────────
 * 2. Gatilhos: cancelamento e exclusão
 * ──────────────────────────────────────────────────────────────────────────── */

// Cancelamento (status nativo e customizado).
foreach ( [ 'cancelled', 'cancelado', 'refunded', 'failed' ] as $sz_cancel_status ) {
    add_action( 'woocommerce_order_status_' . $sz_cancel_status, function ( $order_id ) {
        senderzz_restore_stock_once( $order_id );
    }, 30, 1 );
}

// Exclusão (trash) — restaura estoque e oculta do cliente.
add_action( 'woocommerce_trash_order', 'senderzz_on_order_trashed', 20, 1 );
add_action( 'wp_trash_post', function ( $post_id ) {
    if ( function_exists( 'get_post_type' ) && in_array( get_post_type( $post_id ), [ 'shop_order', 'shop_order_placehold' ], true ) ) {
        senderzz_on_order_trashed( (int) $post_id );
    }
}, 20, 1 );

if ( ! function_exists( 'senderzz_on_order_trashed' ) ) {
    function senderzz_on_order_trashed( $order_id ): void {
        $order_id = (int) $order_id;
        senderzz_restore_stock_once( $order_id );
        senderzz_hide_deleted_order_from_client( $order_id );
    }
}

// Exclusão permanente — restaura estoque ANTES de o pedido sumir do banco.
add_action( 'woocommerce_before_delete_order', function ( $order_id ) {
    senderzz_restore_stock_once( (int) $order_id );
    senderzz_hide_deleted_order_from_client( (int) $order_id );
}, 20, 1 );
add_action( 'before_delete_post', function ( $post_id ) {
    if ( function_exists( 'get_post_type' ) && in_array( get_post_type( $post_id ), [ 'shop_order', 'shop_order_placehold' ], true ) ) {
        senderzz_restore_stock_once( (int) $post_id );
        senderzz_hide_deleted_order_from_client( (int) $post_id );
    }
}, 20, 1 );

// Se o pedido for restaurado do lixo, dá baixa de estoque novamente.
add_action( 'untrashed_post', function ( $post_id ) {
    if ( function_exists( 'get_post_type' ) && in_array( get_post_type( $post_id ), [ 'shop_order', 'shop_order_placehold' ], true ) ) {
        $order = wc_get_order( $post_id );
        if ( $order instanceof WC_Order ) {
            $order->update_meta_data( '_senderzz_stock_restored', 'no' );
            $order->save();
            if ( function_exists( 'senderzz_sz_manual_reduce_stock_once' ) ) {
                senderzz_sz_manual_reduce_stock_once( $order );
            }
        }
    }
}, 20, 1 );

/* ─────────────────────────────────────────────────────────────────────────────
 * 3. Ocultar pedidos EXCLUÍDOS do cliente (só cancelados continuam visíveis)
 * ──────────────────────────────────────────────────────────────────────────── */

if ( ! function_exists( 'senderzz_hide_deleted_order_from_client' ) ) {
    function senderzz_hide_deleted_order_from_client( int $order_id ): void {
        global $wpdb;
        $table = $wpdb->prefix . 'sz_motoboy_pedidos';
        if ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $table ) ) !== $table ) return;
        // Remove a(s) linha(s) operacional(is) do motoboy: pedido excluído não pode
        // aparecer em nenhuma etapa para o cliente (PWA, OL, rastreio).
        $wpdb->delete( $table, [ 'wc_order_id' => $order_id ], [ '%d' ] );
        if ( function_exists( 'senderzz_me_log' ) ) {
            senderzz_me_log( 'order.deleted_hidden_from_client', [ 'order_id' => $order_id ] );
        }
    }
}

/* ─────────────────────────────────────────────────────────────────────────────
 * 4. Cancelamento de pedido motoboy: só quando EMBALADO, sem custo para ninguém
 * ──────────────────────────────────────────────────────────────────────────── */

if ( ! function_exists( 'senderzz_order_is_motoboy_cod' ) ) {
    function senderzz_order_is_motoboy_cod( WC_Order $order ): bool {
        if ( (string) $order->get_meta( '_senderzz_delivery_mode', true ) === 'motoboy' ) return true;
        if ( (string) $order->get_meta( '_senderzz_motoboy_flow_status', true ) !== '' ) return true;
        foreach ( $order->get_items( 'shipping' ) as $item ) {
            if ( stripos( (string) $item->get_method_id(), 'sz_motoboy' ) !== false
              || stripos( (string) $item->get_name(), 'motoboy' ) !== false ) return true;
        }
        return false;
    }
}

if ( ! function_exists( 'senderzz_motoboy_flow_status' ) ) {
    function senderzz_motoboy_flow_status( WC_Order $order ): string {
        $st = (string) $order->get_meta( '_senderzz_motoboy_flow_status', true );
        if ( $st !== '' ) return sanitize_key( $st );
        global $wpdb;
        $table = $wpdb->prefix . 'sz_motoboy_pedidos';
        if ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $table ) ) === $table ) {
            $row = $wpdb->get_var( $wpdb->prepare(
                "SELECT status FROM {$table} WHERE wc_order_id = %d ORDER BY id DESC LIMIT 1",
                $order->get_id()
            ) );
            if ( $row ) return sanitize_key( (string) $row );
        }
        return '';
    }
}

if ( ! function_exists( 'senderzz_motoboy_can_cancel' ) ) {
    /** Cancelamento motoboy só é permitido enquanto EMBALADO. */
    function senderzz_motoboy_can_cancel( WC_Order $order ): bool {
        return senderzz_order_is_motoboy_cod( $order ) && senderzz_motoboy_flow_status( $order ) === 'embalado';
    }
}

if ( ! function_exists( 'senderzz_motoboy_zero_costs' ) ) {
    /**
     * Zera todos os valores a pagar do pedido (produtor, Senderzz, afiliado).
     * Usado no cancelamento de pedido embalado — não gera custo para ninguém.
     */
    function senderzz_motoboy_zero_costs( WC_Order $order ): void {
        foreach ( [
            '_sz_mb_taxa_entrega', '_sz_mb_taxa_manuseio', '_sz_mb_taxa_adicional',
            '_sz_mb_taxa_percentual', '_sz_mb_taxa_total',
            '_sz_taxa_entrega', '_sz_taxa_manuseio', '_sz_taxa_adicional', '_sz_taxa_total',
            '_senderzz_shipping_charged', '_senderzz_shipping_real_cost', '_senderzz_service_fee',
            '_sz_aff_commission',
        ] as $meta ) {
            $order->update_meta_data( $meta, 0 );
        }
        $order->update_meta_data( '_senderzz_sem_custo', 'yes' );
        $order->update_meta_data( '_senderzz_cancel_zero_cost_at', current_time( 'mysql' ) );
        $order->save();

        // Cancela ganho pendente do motoboy, se houver.
        global $wpdb;
        $ganhos = $wpdb->prefix . 'sz_motoboy_ganhos';
        if ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $ganhos ) ) === $ganhos ) {
            $wpdb->query( $wpdb->prepare(
                "UPDATE {$ganhos} SET status='cancelado' WHERE wc_order_id=%d AND status IN ('pendente','disponivel')",
                $order->get_id()
            ) );
        }
        // A reversão da comissão do afiliado é disparada pelo hook de status 'cancelled'
        // (sz_aff_reverse_order). O zeramento de _sz_aff_commission acima evita exibição residual.
    }
}

/**
 * Executa o cancelamento de um pedido motoboy embalado (sem custo).
 * Retorna [ 'success' => bool, 'message' => string ].
 */
if ( ! function_exists( 'senderzz_motoboy_cancel_order' ) ) {
    function senderzz_motoboy_cancel_order( int $order_id ): array {
        $order = wc_get_order( $order_id );
        if ( ! $order instanceof WC_Order ) {
            return [ 'success' => false, 'message' => 'Pedido não encontrado.' ];
        }
        if ( ! senderzz_order_is_motoboy_cod( $order ) ) {
            return [ 'success' => false, 'message' => 'Pedido não é COD Motoboy.' ];
        }
        if ( ! senderzz_motoboy_can_cancel( $order ) ) {
            return [ 'success' => false, 'message' => 'O cancelamento só é permitido enquanto o pedido está EMBALADO.' ];
        }

        // 1. Zera custos (produtor, Senderzz, afiliado).
        senderzz_motoboy_zero_costs( $order );

        // 2. Atualiza a linha operacional do motoboy para cancelado.
        global $wpdb;
        $table = $wpdb->prefix . 'sz_motoboy_pedidos';
        $row   = $wpdb->get_row( $wpdb->prepare( "SELECT id FROM {$table} WHERE wc_order_id=%d ORDER BY id DESC LIMIT 1", $order_id ) );
        if ( $row && function_exists( 'sz_motoboy_mudar_status' ) ) {
            sz_motoboy_mudar_status( (int) $row->id, 'cancelado', [], 'admin', get_current_user_id() );
        } elseif ( $row ) {
            $wpdb->update( $table, [ 'status' => 'cancelado', 'updated_at' => current_time( 'mysql' ) ], [ 'id' => (int) $row->id ] );
        }

        // 3. Status Woo → cancelled (dispara reversão de comissão e restore de estoque).
        $order->update_meta_data( '_senderzz_motoboy_flow_status', 'cancelado' );
        if ( ! $order->has_status( 'cancelled' ) ) {
            $order->update_status( 'cancelled', 'Senderzz COD Motoboy: pedido cancelado (embalado) — sem custo.' );
        } else {
            $order->save();
        }

        // 4. Garante recomposição de estoque (idempotente).
        senderzz_restore_stock_once( $order );

        return [ 'success' => true, 'message' => 'Pedido cancelado. Estoque recomposto e valores zerados para todas as partes.' ];
    }
}

/* ─────────────────────────────────────────────────────────────────────────────
 * 5. Recomposição RETROATIVA (uma vez, por ação do admin)
 * ──────────────────────────────────────────────────────────────────────────── */

if ( ! function_exists( 'senderzz_retro_restock_run' ) ) {
    /**
     * Repõe o estoque de pedidos já cancelados/excluídos que ainda não foram
     * recompostos. Idempotente (usa _senderzz_stock_restored). Pedidos APAGADOS
     * permanentemente não têm registro e não podem ser recompostos automaticamente.
     */
    function senderzz_retro_restock_run( int $limit = 2000 ): array {
        if ( ! function_exists( 'wc_get_orders' ) ) return [ 'processed' => 0, 'restored' => 0 ];
        $statuses = [ 'cancelled', 'cancelado', 'refunded', 'failed', 'trash' ];
        $orders = wc_get_orders( [
            'limit'      => $limit,
            'status'     => $statuses,
            'return'     => 'ids',
            'meta_query' => [
                'relation' => 'AND',
                [ 'key' => '_senderzz_stock_restored', 'value' => 'yes', 'compare' => 'NOT EXISTS' ],
            ],
        ] );
        $restored = 0;
        foreach ( (array) $orders as $oid ) {
            if ( senderzz_restore_stock_once( $oid ) ) $restored++;
        }
        update_option( 'senderzz_retro_restock_done_at', current_time( 'mysql' ), false );
        return [ 'processed' => count( (array) $orders ), 'restored' => $restored ];
    }
}

// Ação admin para disparar a recomposição retroativa manualmente.
add_action( 'admin_post_senderzz_retro_restock', function () {
    if ( ! current_user_can( 'manage_woocommerce' ) ) wp_die( 'Sem permissão.' );
    check_admin_referer( 'senderzz_retro_restock' );
    $res = senderzz_retro_restock_run();
    wp_safe_redirect( add_query_arg( [
        'senderzz_retro_restock' => 'ok',
        'processed'              => (int) $res['processed'],
        'restored'               => (int) $res['restored'],
    ], wp_get_referer() ?: admin_url() ) );
    exit;
} );

// Aviso no admin com botão para rodar a recomposição retroativa uma vez.
add_action( 'admin_notices', function () {
    if ( ! current_user_can( 'manage_woocommerce' ) ) return;
    if ( get_option( 'senderzz_retro_restock_done_at' ) ) {
        if ( ! empty( $_GET['senderzz_retro_restock'] ) ) {
            echo '<div class="notice notice-success is-dismissible"><p>Senderzz: recomposição retroativa concluída — '
                . (int) ( $_GET['restored'] ?? 0 ) . ' de ' . (int) ( $_GET['processed'] ?? 0 )
                . ' pedido(s) tiveram estoque recomposto.</p></div>';
        }
        return;
    }
    $url = wp_nonce_url( admin_url( 'admin-post.php?action=senderzz_retro_restock' ), 'senderzz_retro_restock' );
    echo '<div class="notice notice-warning"><p><strong>Senderzz:</strong> recomposição de estoque de pedidos cancelados/excluídos anteriores ainda não foi executada. '
        . '<a class="button button-primary" href="' . esc_url( $url ) . '">Recompor estoque retroativo agora</a></p></div>';
} );
