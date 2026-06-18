<?php

function sz_wallet_mail_html_ct(): string { return 'text/html'; }
/**
 * senderzz-wallet-engine.php — Lógica de carteira por pedido
 *
 * Funções de reserva, débito, reembolso e validação da carteira do produtor
 * para cada pedido. Extraído de senderzz-engine.php para separar responsabilidades.
 *
 * Depende de: senderzz-engine.php (deve ser carregado antes)
 * Depende de: includes/tpc/wallet.php (tpc_reservar, tpc_debitar, etc.)
 */

defined( 'ABSPATH' ) || exit;
if ( defined( 'SENDERZZ_WALLET_ENGINE_LOADED' ) ) return;
define( 'SENDERZZ_WALLET_ENGINE_LOADED', true );

function senderzz_wallet_required_value( WC_Order $order ): float {
    $snap = senderzz_order_shipping_snapshot( $order );

    // REGRA DE NEGÓCIO SENDERZZ:
    // A carteira do produtor deve ser validada e debitada pelo FRETE COBRADO
    // no pedido (coluna Frete do painel), incluindo a taxa de intermediação.
    // Ex.: carteira R$ 30,00 + frete cobrado R$ 18,35 => aprova e debita R$ 18,35.
    $required = isset( $snap['shipping_charged'] ) ? (float) $snap['shipping_charged'] : 0.0;

    // Fallback: se o snapshot ainda não capturou o frete, usa o total de frete do Woo.
    if ( $required <= 0 ) {
        $required = (float) $order->get_shipping_total() + (float) $order->get_shipping_tax();
    }

    return round( (float) apply_filters( 'senderzz_wallet_required_value', $required, $order, $snap ), 2 );
}

function senderzz_wallet_reference( WC_Order $order ): string {
    return 'senderzz_wallet_reserve_order_' . $order->get_id();
}

function senderzz_order_already_has_label( WC_Order $order ): bool {
    $markers = [ '_melhor_envio_item_id', '_melhor_envio_order_id', '_melhor_envio_tracking_codes', '_melhor_envio_print_url', '_melhor_envio_pdf_local_url', '_senderzz_wallet_debited' ];
    foreach ( $markers as $meta_key ) {
        $value = $order->get_meta( $meta_key );
        if ( is_array( $value ) ? ! empty( $value ) : trim( (string) $value ) !== '' ) return true;
    }
    return false;
}

function senderzz_get_portal_wallet_owner_by_class_id( int $class_id ): int {
    global $wpdb;
    if ( $class_id <= 0 ) return 0;

    $table = $wpdb->prefix . 'senderzz_portal_users';
    $exists = $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $table ) );
    if ( ! $exists ) return 0;

    // Usuário principal ativo da classe é a fonte mais fiel do saldo mostrado no painel.
    // Multi-class: encontra qualquer usuário ativo que tenha esta classe cadastrada.
    $mc_table = $wpdb->prefix . 'senderzz_portal_user_classes';
    $mc_exists = ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $mc_table ) ) === $mc_table );
    if ( $mc_exists ) {
        $wp_user_id = (int) $wpdb->get_var( $wpdb->prepare(
            "SELECT COALESCE(pu.wp_user_id, 0)
               FROM {$mc_table} mc
               INNER JOIN {$table} pu ON pu.id = mc.portal_user_id
              WHERE mc.shipping_class_id = %d
                AND pu.status = 'active'
                AND (pu.parent_user_id IS NULL OR pu.parent_user_id = 0)
              ORDER BY pu.id ASC
              LIMIT 1",
            $class_id
        ) );
    } else {
        $wp_user_id = (int) $wpdb->get_var( $wpdb->prepare(
            "SELECT COALESCE(wp_user_id,0)
               FROM {$table}
              WHERE shipping_class_id = %d
                AND status = 'active'
                AND (parent_user_id IS NULL OR parent_user_id = 0)
              ORDER BY id ASC
              LIMIT 1",
            $class_id
        ) );
    }

    if ( $wp_user_id > 0 && get_user_by( 'id', $wp_user_id ) ) {
        if ( function_exists( 'senderzz_ensure_tpc_wallet' ) ) senderzz_ensure_tpc_wallet( $wp_user_id );
        return $wp_user_id;
    }

    return 0;
}

function senderzz_get_order_shipping_class_context( WC_Order $order ): array {
    $classes = [];
    foreach ( $order->get_items( 'line_item' ) as $item ) {
        $product = $item instanceof WC_Order_Item_Product ? $item->get_product() : null;
        if ( ! $product ) { continue; }
        $class_id = (int) $product->get_shipping_class_id();
        $term = $class_id > 0 ? get_term( $class_id, 'product_shipping_class' ) : null;
        $classes[ $class_id ] = [
            'id' => $class_id,
            'slug' => ( $term && ! is_wp_error( $term ) ) ? (string) $term->slug : '',
            'name' => ( $class_id > 0 && $term && ! is_wp_error( $term ) ) ? (string) $term->name : 'Sem classe de entrega',
        ];
    }
    $map = get_option( 'senderzz_shipping_class_wallet_owners', [] );
    if ( ! is_array( $map ) ) { $map = []; }
    $owners = [];
    foreach ( $classes as $class_id => $class ) {
        // Prioridade 1: usuário ativo do Portal vinculado à classe.
        // Isso evita divergência: painel mostra saldo do produtor, mas o backend tentava debitar outra carteira antiga do mapa.
        $owner_id = function_exists( 'senderzz_get_portal_wallet_owner_by_class_id' ) ? senderzz_get_portal_wallet_owner_by_class_id( (int) $class_id ) : 0;

        // Prioridade 2: mapa manual de Configurações, usado como fallback.
        if ( ! $owner_id ) {
            $owner_id = isset( $map[ (string) $class_id ] ) ? absint( $map[ (string) $class_id ] ) : 0;
        }

        if ( $owner_id > 0 ) { $owners[ $owner_id ][] = $class; }
    }
    if ( count( $owners ) > 1 ) {
        throw new Exception( 'Pedido possui produtos de classes vinculadas a carteiras diferentes. Separe os produtos em pedidos diferentes.' );
    }
    $owner_id = $owners ? (int) array_key_first( $owners ) : 0;
    $owner = $owner_id ? get_userdata( $owner_id ) : null;
    $selected = $owners ? reset( $owners ) : array_values( $classes );
    $first = $selected ? $selected[0] : [ 'id' => 0, 'slug' => '', 'name' => 'Sem classe de entrega' ];
    return [
        'owner_user_id' => $owner_id,
        'owner_email' => $owner ? (string) $owner->user_email : '',
        'owner_login' => $owner ? (string) $owner->user_login : '',
        'class_id' => (int) $first['id'],
        'class_slug' => (string) $first['slug'],
        'class_name' => (string) $first['name'],
        'classes' => array_values( $classes ),
    ];
}

function senderzz_get_order_wallet_owner_id( WC_Order $order ): int {
    $ctx = senderzz_get_order_shipping_class_context( $order );
    if ( ! empty( $ctx['owner_user_id'] ) ) { return (int) $ctx['owner_user_id']; }
    return (int) $order->get_customer_id();
}

function senderzz_sync_order_owner_meta( WC_Order $order ): array {
    $ctx = senderzz_get_order_shipping_class_context( $order );
    $owner_id = ! empty( $ctx['owner_user_id'] ) ? (int) $ctx['owner_user_id'] : (int) $order->get_customer_id();
    $owner = $owner_id ? get_userdata( $owner_id ) : null;
    $ctx['effective_owner_user_id'] = $owner_id;
    $ctx['effective_owner_email'] = $owner ? (string) $owner->user_email : (string) $order->get_billing_email();
    $order->update_meta_data( '_senderzz_owner_user_id', $owner_id );
    $order->update_meta_data( '_senderzz_owner_email', $ctx['effective_owner_email'] );
    $order->update_meta_data( '_senderzz_product_shipping_class_id', (int) $ctx['class_id'] );
    $order->update_meta_data( '_senderzz_product_shipping_class_slug', (string) $ctx['class_slug'] );
    $order->update_meta_data( '_senderzz_product_shipping_class_name', (string) $ctx['class_name'] );
    return $ctx;
}

function senderzz_wallet_assert_order_secure( WC_Order $order ): void {
    $owner_id = senderzz_get_order_wallet_owner_id( $order );
    if ( ! $owner_id ) {
        throw new Exception( 'Pedido sem cliente/carteira vinculada. Configure a classe de entrega do produto em Carteira de Frete → Configurações.' );
    }
    if ( ! current_user_can( 'manage_woocommerce' ) ) {
        $current = get_current_user_id();
        if ( ! $current || $current !== $owner_id ) {
            throw new Exception( 'Pedido não pertence à carteira do usuário logado.' );
        }
    }
}

function senderzz_wallet_reserve_order( WC_Order $order, string $context = 'label' ): int {
    senderzz_wallet_assert_order_secure( $order );

    if ( get_option( 'senderzz_block_duplicate_label', 'yes' ) === 'yes' && senderzz_order_already_has_label( $order ) && ! $order->get_meta( '_senderzz_wallet_reserved' ) ) {
        throw new Exception( 'Este pedido já possui etiqueta/protocolo/rastreio registrado. Nova emissão bloqueada para evitar duplicidade.' );
    }

    if ( get_option( 'senderzz_enforce_wallet_on_label', 'yes' ) !== 'yes' ) {
        senderzz_sync_order_shipping_meta( $order );
        return 0;
    }

    if ( $order->get_meta( '_senderzz_wallet_debited' ) ) {
        return (int) $order->get_meta( '_senderzz_wallet_reserve_tx' );
    }

    $existing_tx = (int) $order->get_meta( '_senderzz_wallet_reserve_tx' );
    if ( $existing_tx && ! $order->get_meta( '_senderzz_wallet_reserve_released' ) ) {
        return $existing_tx;
    }

    $user_id = senderzz_get_order_wallet_owner_id( $order );
    $required = senderzz_wallet_required_value( $order );
    if ( $required <= 0 ) return 0;

    if ( ! function_exists( 'tpc_reservar' ) || ! function_exists( 'tpc_get_saldo_disponivel' ) ) {
        throw new Exception( 'Carteira Senderzz indisponível. Não é possível reservar saldo.' );
    }

    $snap = senderzz_sync_order_shipping_meta( $order );
    $tx = tpc_reservar( $user_id, $required, 'Reserva de frete Senderzz - Pedido #' . $order->get_order_number(), [
        'referencia'  => senderzz_wallet_reference( $order ),
        'order_id'    => $order->get_id(),
        'owner_user_id' => $user_id,
        'context'     => $context,

        'snapshot'    => $snap,
    ] );

    if ( ! $tx ) {
        $saldo = tpc_get_saldo_disponivel( $user_id );
        $saldo_total = function_exists( 'tpc_get_saldo' ) ? (float) tpc_get_saldo( $user_id ) : $saldo;
        $saldo_reservado = function_exists( 'tpc_get_saldo_reservado' ) ? (float) tpc_get_saldo_reservado( $user_id ) : 0.0;
        throw new Exception( sprintf( 'Saldo disponível insuficiente. Carteira #%d | Total: R$ %s | Reservado: R$ %s | Disponível: R$ %s | Necessário frete: R$ %s', $user_id, number_format( $saldo_total, 2, ',', '.' ), number_format( $saldo_reservado, 2, ',', '.' ), number_format( $saldo, 2, ',', '.' ), number_format( $required, 2, ',', '.' ) ) );
    }

    $order->update_meta_data( '_senderzz_wallet_reserved', 'yes' );
    $order->update_meta_data( '_senderzz_wallet_reserve_tx', $tx );
    $order->update_meta_data( '_senderzz_wallet_reserved_value', $required );
    $order->delete_meta_data( '_senderzz_wallet_reserve_released' );
    $order->save();
    $owner_note = get_userdata( $user_id );
    $order->add_order_note( 'Senderzz: reservado R$ ' . number_format( $required, 2, ',', '.' ) . ' da carteira ' . ( $owner_note ? '(' . $owner_note->user_email . ')' : '#' . $user_id ) . '. Reserva #' . $tx . '.' );

    senderzz_log( 'wallet_reserve', [ 'order_id' => $order->get_id(), 'user_id' => $user_id, 'value' => $required, 'tx' => $tx, 'context' => $context ] );

    return (int) $tx;
}

function senderzz_wallet_release_order( WC_Order $order, string $reason = '' ): bool {
    if ( $order->get_meta( '_senderzz_wallet_debited' ) || $order->get_meta( '_senderzz_wallet_reserve_released' ) ) return false;
    $tx = (int) $order->get_meta( '_senderzz_wallet_reserve_tx' );
    if ( ! $tx || ! function_exists( 'tpc_liberar_reserva' ) ) return false;

    $ok = tpc_liberar_reserva( $tx, $reason );
    if ( $ok ) {
        $order->update_meta_data( '_senderzz_wallet_reserve_released', 'yes' );
        $order->save();
        $order->add_order_note( 'Senderzz: reserva de saldo liberada. Reserva #' . $tx . ( $reason ? ' | ' . $reason : '' ) );
        senderzz_log( 'wallet_release', [ 'order_id' => $order->get_id(), 'tx' => $tx, 'reason' => $reason ] );
    }
    return $ok;
}

function senderzz_wallet_assert_can_use( WC_Order $order ): void {
    // Relê o pedido do banco para garantir metas frescos.
    // O objeto recebido pelo hook pode ser stale (criado antes do débito por aprovação).
    $fresh = wc_get_order( $order->get_id() );
    if ( ! $fresh ) return;

    // Se o pedido já foi debitado diretamente (ex.: fluxo de aprovação imediata),
    // não há reserva pendente — o saldo já saiu da carteira. Deixa o pipeline prosseguir.
    if ( $fresh->get_meta( '_senderzz_wallet_debited' ) ) {
        return;
    }
    senderzz_wallet_reserve_order( $fresh, 'before_label' );
}

add_action( 'wc_melhor_envio_ajax_before_process', 'senderzz_wallet_assert_can_use', 5 );
add_action( 'wc_melhor_envio_pipeline_before_process', 'senderzz_wallet_assert_can_use', 5 );

function senderzz_wallet_debit_after_label( WC_Order $order, string $context = 'label' ): void {
    if ( get_option( 'senderzz_enforce_wallet_on_label', 'yes' ) !== 'yes' ) {
        senderzz_sync_order_shipping_meta( $order );
        return;
    }
    if ( $order->get_meta( '_senderzz_wallet_debited' ) ) return;
    // Lock para evitar duplo débito por cliques rápidos (race condition).
    $lock_key = 'senderzz_debit_lock_' . $order->get_id();
    if ( get_transient( $lock_key ) ) return;
    set_transient( $lock_key, 1, 30 );

    $tx = (int) $order->get_meta( '_senderzz_wallet_reserve_tx' );
    if ( ! $tx ) {
        $tx = senderzz_wallet_reserve_order( $order, 'before_debit_fallback' );
    }

    if ( ! $tx || ! function_exists( 'tpc_debitar_reserva' ) ) {
        throw new Exception( 'Reserva de saldo inexistente. Não foi possível debitar a carteira.' );
    }

    $value = senderzz_wallet_required_value( $order );
    $ok = tpc_debitar_reserva( $tx, [
        'order_id'    => $order->get_id(),
        'me_order_id' => (string) $order->get_meta( '_melhor_envio_order_id' ),
        'shipment_id' => (string) $order->get_meta( '_melhor_envio_item_id' ),
        'context'     => $context,
    ] );

    if ( ! $ok ) {
        throw new Exception( 'Falha ao debitar a reserva da carteira após emissão da etiqueta.' );
    }

    $snap = senderzz_sync_order_shipping_meta( $order );
    $order->update_meta_data( '_senderzz_wallet_debited', 'yes' );
    $order->update_meta_data( '_senderzz_wallet_debit_tx', $tx );
    $order->update_meta_data( '_senderzz_wallet_debit_value', $value );
    $order->save();

    $owner_note = get_userdata( senderzz_get_order_wallet_owner_id( $order ) );
    $order->add_order_note( 'Senderzz: debitado R$ ' . number_format( $value, 2, ',', '.' ) . ' da carteira ' . ( $owner_note ? '(' . $owner_note->user_email . ')' : '' ) . '. Transação #' . $tx . '.' );

    senderzz_log( 'wallet_debit', [ 'order_id' => $order->get_id(), 'user_id' => senderzz_get_order_wallet_owner_id( $order ), 'owner_user_id' => senderzz_get_order_wallet_owner_id( $order ), 'value' => $value, 'tx' => $tx, 'context' => $context, 'snapshot' => $snap ] );
}

add_action( 'wc_melhor_envio_label_processed', function( $order ) { senderzz_wallet_debit_after_label( $order, 'ajax' ); }, 10, 1 );
add_action( 'wc_melhor_envio_pipeline_after_process', function( $order ) { senderzz_wallet_debit_after_label( $order, 'pipeline' ); }, 10, 1 );

function senderzz_wallet_refund_order( $order_id, $old_status = '', $new_status = '' ): void {
    $order = wc_get_order( $order_id );
    if ( ! $order ) return;


    // Senderzz v2.5.47: cancelamento do Melhor Envio só estorna após validação real via API.
    $new_status_norm_guard = strtolower( str_replace( 'wc-', '', (string) $new_status ) );
    if ( in_array( $new_status_norm_guard, [ 'cancelled', 'canceled' ], true )
        && (string) $order->get_meta( '_melhor_envio_order_id' ) !== ''
        && (string) $order->get_meta( '_senderzz_cancel_validation_status' ) !== 'confirmed'
    ) {
        $order->update_meta_data( '_senderzz_cancel_validation_status', 'pending' );
        $order->save();
        if ( function_exists( 'senderzz_schedule_me_refund_validation' ) ) {
            senderzz_schedule_me_refund_validation( $order->get_id(), 300 );
        } elseif ( ! wp_next_scheduled( 'senderzz_check_me_refund_status', [ $order->get_id() ] ) ) {
            wp_schedule_single_event( time() + 300, 'senderzz_check_me_refund_status', [ $order->get_id() ] );
        }
        if ( function_exists( 'senderzz_log' ) ) {
            senderzz_log( 'wallet_refund_wait_me_api_confirmation', [ 'order_id' => $order->get_id(), 'new_status' => $new_status_norm_guard ] );
        }
        return;
    }
    // Se este pedido tem estorno diferido de 12h agendado (etiqueta ME cancelada),
    // o crédito será feito pelo cron senderzz_deferred_wallet_refund — não aqui.
    // Evita duplo crédito e garante que o prazo seja respeitado.
    $old_status_norm = strtolower( str_replace( 'wc-', '', (string) $old_status ) );
    $new_status_norm = strtolower( str_replace( 'wc-', '', (string) $new_status ) );
    $cancel_from_error = $old_status_norm === 'erro' && in_array( $new_status_norm, [ 'cancelled', 'canceled', 'refunded', 'failed' ], true );

    if ( $order->get_meta( '_senderzz_deferred_refund_scheduled' ) && ! $order->get_meta( '_senderzz_wallet_refunded' ) && ! $cancel_from_error ) {
        senderzz_log( 'wallet_refund_skip_deferred', [ 'order_id' => $order_id, 'note' => 'Estorno adiado — aguarda cron de 12h.' ] );
        return;
    }

    if ( $order->get_meta( '_senderzz_wallet_debited' ) && ! $order->get_meta( '_senderzz_wallet_refunded' ) ) {
        $user_id = senderzz_get_order_wallet_owner_id( $order );
        $value = (float) $order->get_meta( '_senderzz_wallet_debit_value' );
        if ( $value <= 0 && function_exists( 'senderzz_wallet_required_value' ) ) {
            $value = senderzz_wallet_required_value( $order );
        }
        if ( $user_id && $value > 0 && function_exists( 'tpc_creditar' ) ) {
            $tx = tpc_creditar( $user_id, $value, 'Estorno de frete Senderzz - Pedido #' . $order->get_order_number(), [
                'referencia'  => 'estorno_frete_pedido_' . $order->get_id(),
                'order_id'    => $order->get_id(),
                'me_order_id' => (string) $order->get_meta( '_melhor_envio_order_id' ),
            ] );
            if ( $tx ) {
                $order->update_meta_data( '_senderzz_wallet_refunded', 'yes' );
                $order->update_meta_data( '_senderzz_wallet_refund_tx', $tx );
                $order->save();
                $order->add_order_note( 'Senderzz: estornado R$ ' . number_format( $value, 2, ',', '.' ) . ' para a carteira. Transação #' . $tx . '.' );
                senderzz_log( 'wallet_refund', [ 'order_id' => $order_id, 'user_id' => $user_id, 'value' => $value, 'tx' => $tx, 'old_status' => $old_status, 'new_status' => $new_status ] );
            }
        }
        return;
    }

    senderzz_wallet_release_order( $order, 'pedido cancelado/estornado/falhou' );
}
add_action( 'woocommerce_order_status_cancelled', 'senderzz_wallet_refund_order', 20, 1 );
add_action( 'woocommerce_order_status_refunded', 'senderzz_wallet_refund_order', 20, 1 );
add_action( 'woocommerce_order_status_failed', 'senderzz_wallet_refund_order', 20, 1 );
add_action( 'woocommerce_order_status_changed', function( $order_id, $old_status, $new_status ) {
    $new = strtolower( str_replace( 'wc-', '', (string) $new_status ) );
    if ( in_array( $new, [ 'cancelled', 'canceled', 'refunded', 'failed' ], true ) ) {
        senderzz_wallet_refund_order( $order_id, $old_status, $new_status );
    }
}, 19, 3 );


/* ── Senderzz v2.5.30: débito imediato ao aprovar pedido ────────────────
 * Regra: ao entrar em status "aprovado", a carteira é debitada na hora.
 * IMPORTANTE: não usa a rotina de etiqueta (senderzz_wallet_debit_after_label),
 * porque ela depende de reserva/etiqueta e gerava falha indevida na aprovação.
 */
function senderzz_wallet_debit_immediate_on_approval( WC_Order $order, string $context = 'approved_status' ): void {
    if ( get_option( 'senderzz_enforce_wallet_on_label', 'yes' ) !== 'yes' ) {
        senderzz_sync_order_shipping_meta( $order );
        return;
    }

    if ( $order->get_meta( '_senderzz_wallet_debited' ) ) {
        return;
    }

    // Lock para evitar duplo débito por cliques rápidos (race condition).
    $lock_key = 'senderzz_debit_lock_' . $order->get_id();
    if ( get_transient( $lock_key ) ) return;
    set_transient( $lock_key, 1, 30 );

    // O assert_order_secure verifica usuário WP logado — válido para ações manuais.
    // Em contextos de sistema (hook de status, reprocessamento do portal, cron),
    // o usuário WP pode não estar logado; apenas valida que o pedido tem dono de carteira.
    $owner_id = senderzz_get_order_wallet_owner_id( $order );
    if ( ! $owner_id ) {
        throw new Exception( 'Pedido sem cliente/carteira vinculada. Configure a classe de entrega do produto em Carteira de Frete → Configurações.' );
    }

    $value   = senderzz_wallet_required_value( $order );
    $user_id = $owner_id;
    if ( $value <= 0 ) {
        return;
    }

    if ( ! $user_id || ! function_exists( 'tpc_debitar' ) || ! function_exists( 'tpc_get_saldo_disponivel' ) ) {
        throw new Exception( 'Carteira Senderzz indisponível. Não foi possível debitar ao aprovar.' );
    }

    $available = (float) tpc_get_saldo_disponivel( $user_id );
    if ( $available < $value ) {
        $saldo_total = function_exists( 'tpc_get_saldo' ) ? (float) tpc_get_saldo( $user_id ) : $available;
        $saldo_reservado = function_exists( 'tpc_get_saldo_reservado' ) ? (float) tpc_get_saldo_reservado( $user_id ) : 0.0;
        throw new Exception( sprintf( 'Saldo disponível insuficiente. Carteira #%d | Total: R$ %s | Reservado: R$ %s | Disponível: R$ %s | Necessário frete: R$ %s', $user_id, number_format( $saldo_total, 2, ',', '.' ), number_format( $saldo_reservado, 2, ',', '.' ), number_format( $available, 2, ',', '.' ), number_format( $value, 2, ',', '.' ) ) );
    }

    $snap = senderzz_sync_order_shipping_meta( $order );
    $tx = tpc_debitar(
        $user_id,
        $value,
        'Débito de frete Senderzz - Pedido #' . $order->get_order_number(),
        [
            'referencia'    => 'debito_frete_aprovacao_' . $order->get_id(),
            'order_id'      => $order->get_id(),
            'owner_user_id' => $user_id,
            'context'       => $context,
            'snapshot'      => $snap,
        ]
    );

    if ( ! $tx ) {
        throw new Exception( 'Falha ao debitar a carteira ao aprovar o pedido.' );
    }

    $order->update_meta_data( '_senderzz_wallet_debited', 'yes' );
    $order->update_meta_data( '_senderzz_wallet_debit_tx', $tx );
    $order->update_meta_data( '_senderzz_wallet_debit_value', $value );
    $order->update_meta_data( '_senderzz_wallet_debit_context', $context );
    $order->delete_meta_data( '_senderzz_wallet_reserved' );
    $order->delete_meta_data( '_senderzz_wallet_reserve_released' );
    $order->save();

    $owner_note = get_userdata( $user_id );
    $order->add_order_note( 'Senderzz: debitado R$ ' . number_format( $value, 2, ',', '.' ) . ' da carteira ao aprovar o pedido ' . ( $owner_note ? '(' . $owner_note->user_email . ')' : '#' . $user_id ) . '. Transação #' . $tx . '.' );
    senderzz_log( 'wallet_debit_on_approval', [ 'order_id' => $order->get_id(), 'user_id' => $user_id, 'value' => $value, 'tx' => $tx, 'context' => $context, 'snapshot' => $snap ] );
}

function senderzz_wallet_debit_on_approved_status( int $order_id, string $old_status = '', string $new_status = '' ): void {
    static $running = [];

    if ( isset( $running[ $order_id ] ) ) return;

    $new = strtolower( str_replace( 'wc-', '', (string) $new_status ) );
    if ( ! in_array( $new, [ 'aprovado', 'approved' ], true ) ) return;

    $order = wc_get_order( $order_id );
    if ( ! $order instanceof WC_Order ) return;

    if ( $order->get_meta( '_senderzz_wallet_debited' ) ) return;

    $running[ $order_id ] = true;

    try {
        senderzz_wallet_debit_immediate_on_approval( $order, 'approved_status' );
    } catch ( Throwable $e ) {
        $order->add_order_note( 'Senderzz: aprovação bloqueada por falha no débito de saldo: ' . $e->getMessage() );

        $old = strtolower( str_replace( 'wc-', '', (string) $old_status ) );
        if ( $old && $old !== $new ) {
            try {
                $order->update_status( $old, 'Senderzz: status revertido porque o saldo não pôde ser debitado ao aprovar.' );
            } catch ( Throwable $ignored ) {}
        }

        senderzz_log( 'wallet_approve_debit_failed', [
            'order_id'   => $order_id,
            'old_status' => $old_status,
            'new_status' => $new_status,
            'error'      => $e->getMessage(),
        ] );
    }

    unset( $running[ $order_id ] );
}
add_action( 'woocommerce_order_status_changed', 'senderzz_wallet_debit_on_approved_status', 1, 3 );

add_filter( 'wc_melhor_envio_rate_args', function( $args, $method, $package, $instance ) {
    $real = isset( $method->custom_price ) ? (float) $method->custom_price : 0.0;
    $charged = isset( $args['cost'] ) ? (float) $args['cost'] : 0.0;
    $args['meta_data']['senderzz_shipping_real_cost'] = $real;
    $args['meta_data']['senderzz_shipping_charged'] = $charged;
    $args['meta_data']['senderzz_service_fee'] = round( max( 0, $charged - $real ), 2 );
    $args['meta_data']['senderzz_margin'] = round( max( 0, $charged - $real ), 2 );
    return $args;
}, 20, 4 );
// ─────────────────────────────────────────────────────────────────────────────
// AUTO-GERAÇÃO DE ETIQUETA AO ENTRAR EM "APROVADO"
// v2.5.45 — gera etiqueta automaticamente para garantir item_id sempre presente
// ─────────────────────────────────────────────────────────────────────────────

add_action( 'woocommerce_order_status_changed', function ( int $order_id, string $old_status, string $new_status ) {

    if ( ! in_array( $new_status, [ 'aprovado', 'approved' ], true ) ) return;

    $order = wc_get_order( $order_id );
    if ( ! $order ) return;

    // Já tem etiqueta — não reprocessa.
    if ( (string) $order->get_meta( '_melhor_envio_item_id' ) !== '' ) return;
    if ( senderzz_order_already_has_label( $order ) ) return;

    // Agenda geração assíncrona para não travar a requisição atual.
    wp_schedule_single_event( time() + 5, 'senderzz_auto_generate_label', [ $order_id ] );

}, 20, 3 );

add_action( 'senderzz_auto_generate_label', function ( int $order_id ): void {

    if ( get_transient( 'senderzz_label_lock_' . $order_id ) ) {
        return;
    }

    $order = wc_get_order( $order_id );
    if ( ! $order ) {
        return;
    }

    $blocked_statuses = [
        'emcancelamento',
        'cancelamento',
        'cancelled',
        'canceled',
        'cancelado',
        'refunded',
        'failed',
        'erro',
    ];

    if ( in_array( $order->get_status(), $blocked_statuses, true ) ) {
        $order->add_order_note( 'Senderzz: auto-geração bloqueada porque o pedido está em cancelamento/cancelado/erro.' );
        $order->save();
        return;
    }

    if (
        $order->get_meta( '_senderzz_label_cancel_requested' ) ||
        $order->get_meta( '_senderzz_label_cancel_confirmed' ) ||
        $order->get_meta( '_senderzz_deferred_refund_scheduled' )
    ) {
        $order->add_order_note( 'Senderzz: auto-geração bloqueada porque a etiqueta foi cancelada ou está em cancelamento.' );
        $order->save();
        return;
    }

    // Só processa se ainda estiver em aprovado.
    if ( ! in_array( $order->get_status(), [ 'aprovado', 'approved' ], true ) ) {
        return;
    }

    // Já tem etiqueta — não reprocessa.
    if ( (string) $order->get_meta( '_melhor_envio_item_id' ) !== '' ) {
        return;
    }

    if ( ! function_exists( 'senderzz_operator_ensure_label_pdf' ) ) {
        $order->add_order_note( 'Senderzz: auto-geração de etiqueta falhou — função indisponível.' );
        $order->save();
        return;
    }

    try {
        senderzz_operator_ensure_label_pdf( $order );

        $order = wc_get_order( $order_id );
        if ( ! $order ) {
            return;
        }

        if ( in_array( $order->get_status(), $blocked_statuses, true ) ) {
            $order->add_order_note( 'Senderzz: mudança para separado bloqueada porque o pedido entrou em cancelamento/cancelado/erro durante a geração.' );
            $order->save();
            return;
        }

        if (
            $order->get_meta( '_senderzz_label_cancel_requested' ) ||
            $order->get_meta( '_senderzz_label_cancel_confirmed' ) ||
            $order->get_meta( '_senderzz_deferred_refund_scheduled' )
        ) {
            $order->add_order_note( 'Senderzz: mudança para separado bloqueada porque a etiqueta está cancelada ou em cancelamento.' );
            $order->save();
            return;
        }

        if ( in_array( $order->get_status(), [ 'aprovado', 'approved' ], true ) ) {
            // Senderzz: não usamos mais o status "separado".
            // A etiqueta é gerada, mas o pedido permanece como APROVADO para o operador processar.
            $order->add_order_note( 'Senderzz: etiqueta gerada automaticamente. Pedido mantido como aprovado.' );
            $order->save();
        }

        if ( function_exists( 'senderzz_me_log' ) ) {
            senderzz_me_log( 'auto_label.success', [ 'order_id' => $order_id ] );
        }

    } catch ( \Throwable $e ) {
        $order = wc_get_order( $order_id );

        if ( $order ) {
            if ( in_array( $order->get_status(), $blocked_statuses, true ) ) {
                $order->add_order_note( 'Senderzz: erro de auto-geração ignorado porque o pedido já está em cancelamento/cancelado/erro. Detalhe: ' . $e->getMessage() );
                $order->save();
            } else {
                $order->update_meta_data( '_melhor_envio_label_status', 'error' );
                $order->update_meta_data( '_melhor_envio_label_error', $e->getMessage() );
                $order->save();
                $order->update_status( 'erro', 'Senderzz: falha na auto-geração de etiqueta — ' . $e->getMessage() );
            }
        }

        if ( function_exists( 'senderzz_me_log' ) ) {
            senderzz_me_log( 'auto_label.error', [ 'order_id' => $order_id, 'error' => $e->getMessage() ] );
        }
    }

} );
add_action( 'senderzz_retry_label_pipeline', function ( int $order_id ) {
	$order = wc_get_order( $order_id );

	if ( ! $order ) {
		return;
	}

	if ( (string) $order->get_meta( '_melhor_envio_label_status' ) !== 'processing' ) {
		return;
	}

	try {
		$pipeline = new \WC_MelhorEnvio\Pipeline\Label_Pipeline();
		$pipeline->process( $order, true );
	} catch ( \Throwable $e ) {
		$order->add_order_note( 'Senderzz: nova tentativa de processamento da etiqueta ainda falhou: ' . $e->getMessage() );
		$order->update_meta_data( '_melhor_envio_label_last_retry_error', $e->getMessage() );
		$order->save();
	}
}, 10, 1 );


// ── Alerta automático de saldo baixo ────────────────────────────────────────
// Disparado após qualquer débito de carteira. Envia e-mail + push se saldo
// cair abaixo do threshold configurado em Carteira → Configurações (tpc_saldo_minimo).

function senderzz_maybe_alert_low_balance( int $user_id ): void {
    if ( ! function_exists( 'tpc_get_saldo_disponivel' ) ) return;

    $threshold = (float) get_option( 'tpc_saldo_minimo', 0 );
    if ( $threshold <= 0 ) return;

    $saldo = (float) tpc_get_saldo_disponivel( $user_id );
    if ( $saldo >= $threshold ) return;

    // Throttle: no máximo 1 alerta a cada 4 horas por usuário.
    $throttle_key = 'sz_low_balance_alert_' . $user_id;
    if ( get_transient( $throttle_key ) ) return;
    set_transient( $throttle_key, 1, 4 * HOUR_IN_SECONDS );

    $saldo_fmt    = 'R$ ' . number_format( $saldo, 2, ',', '.' );
    $threshold_fmt = 'R$ ' . number_format( $threshold, 2, ',', '.' );

    // Busca e-mail do usuário do portal.
    global $wpdb;
    $portal_user = $wpdb->get_row( $wpdb->prepare(
        "SELECT email, name FROM {$wpdb->prefix}senderzz_portal_users
          WHERE wp_user_id = %d AND status = 'active'
            AND (parent_user_id IS NULL OR parent_user_id = 0)
          ORDER BY id ASC LIMIT 1",
        $user_id
    ) );
    $email = $portal_user ? (string) $portal_user->email : '';
    $name  = $portal_user ? (string) $portal_user->name  : '';
    if ( ! $email ) {
        $wp_user = get_userdata( $user_id );
        if ( $wp_user ) { $email = $wp_user->user_email; $name = $wp_user->display_name; }
    }

    // E-mail.
    if ( $email ) {
        $subject = '⚠️ Senderzz — Saldo baixo na sua carteira';
        $body    = '<!DOCTYPE html><html><body style="font-family:var(--sz-font);color:#111;padding:24px">'
            . '<h2 style="color:#E8650A">Saldo baixo na sua carteira Senderzz</h2>'
            . '<p>Olá, <strong>' . esc_html( $name ) . '</strong>.</p>'
            . '<p>Seu saldo atual é <strong>' . esc_html( $saldo_fmt ) . '</strong>, abaixo do limite de alerta de <strong>' . esc_html( $threshold_fmt ) . '</strong>.</p>'
            . '<p>Para continuar expedindo pedidos sem interrupções, <strong>recarregue sua carteira via PIX</strong> acessando o portal.</p>'
            . '<p style="margin-top:24px"><a href="' . esc_url( home_url( '/meus-pedidos/' ) ) . '" style="background:#E8650A;color:#fff;padding:12px 22px;border-radius:10px;text-decoration:none;font-weight:700">Acessar portal e recarregar</a></p>'
            . '<p style="margin-top:24px;font-size:var(--sz-text-meta);color:#6b7280">Enviado automaticamente pelo sistema Senderzz.</p>'
            . '</body></html>';
        add_filter( 'wp_mail_content_type', 'sz_wallet_mail_html_ct' );
        wp_mail( $email, $subject, $body );
        remove_filter( 'wp_mail_content_type', 'sz_wallet_mail_html_ct' );
    }

    // Push notification (se módulo de push estiver ativo).
    if ( function_exists( 'senderzz_send_push_to_user' ) ) {
        senderzz_send_push_to_user( $user_id, '⚠️ Saldo baixo', 'Seu saldo é ' . $saldo_fmt . '. Recarregue para continuar enviando.' );
    }

    senderzz_log( 'low_balance_alert', [ 'user_id' => $user_id, 'saldo' => $saldo, 'threshold' => $threshold, 'email' => $email ] );
}

// Hook: dispara após qualquer débito de carteira.
add_action( 'woocommerce_order_status_changed', function( int $order_id, string $old, string $new ) {
    if ( ! in_array( $new, [ 'aprovado', 'approved' ], true ) ) return;
    $order = wc_get_order( $order_id );
    if ( ! $order ) return;
    // Só alerta após confirmação de débito (meta setado em senderzz_wallet_debit_after_label / debit_immediate).
    $user_id = senderzz_get_order_wallet_owner_id( $order );
    if ( $user_id > 0 ) {
        // Agenda para após o débito ser concluído.
        wp_schedule_single_event( time() + 30, 'senderzz_check_low_balance', [ $user_id ] );
    }
}, 99, 3 );

add_action( 'senderzz_check_low_balance', 'senderzz_maybe_alert_low_balance' );
