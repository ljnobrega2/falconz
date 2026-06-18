<?php
/**
 * Senderzz — Notificações por e-mail ao produtor
 *
 * Envia e-mail automático ao produtor quando o pedido muda para
 * status relevantes: enviado, entregue, erro, extravio, cancelado.
 *
 * Instalação: incluído automaticamente pelo senderzz-logistics.php
 */

defined( 'ABSPATH' ) || exit;
if ( defined( 'SENDERZZ_PRODUCER_NOTIFICATIONS_LOADED' ) ) return;
define( 'SENDERZZ_PRODUCER_NOTIFICATIONS_LOADED', true );

/**
 * Status que geram notificação e seus textos.
 */
function senderzz_notification_config(): array {
    return apply_filters( 'senderzz_notification_config', [
        'enviado'           => [ 'icon' => '📦', 'title' => 'Pedido enviado',             'color' => '#2563eb', 'msg' => 'Seu pedido foi coletado e está em rota de entrega.' ],
        'acaminho'          => [ 'icon' => '🚚', 'title' => 'Pedido a caminho',            'color' => '#7c3aed', 'msg' => 'O pedido está a caminho do destinatário.' ],
        'concluido'         => [ 'icon' => '✅', 'title' => 'Pedido entregue',             'color' => '#16a34a', 'msg' => 'O pedido foi entregue com sucesso.' ],
        'completed'         => [ 'icon' => '✅', 'title' => 'Pedido concluído',            'color' => '#16a34a', 'msg' => 'O pedido foi concluído.' ],
        'erro'              => [ 'icon' => '⚠️', 'title' => 'Erro na emissão de etiqueta', 'color' => '#d97706', 'msg' => 'Houve um erro ao emitir a etiqueta. Acesse o painel para reprocessar.' ],
        'extravio'          => [ 'icon' => '🔍', 'title' => 'Pedido em análise de extravio','color' => '#dc2626', 'msg' => 'Uma ocorrência de extravio foi registrada para este pedido.' ],
        'cancelled'         => [ 'icon' => '❌', 'title' => 'Pedido cancelado',            'color' => '#dc2626', 'msg' => 'O pedido foi cancelado e o saldo estornado.' ],
        'saldoinsuficiente' => [ 'icon' => '💳', 'title' => 'Saldo insuficiente',          'color' => '#d97706', 'msg' => 'Seu saldo está insuficiente para emitir a etiqueta. Recarregue a carteira.' ],
    ] );
}

/**
 * Hook principal — dispara quando um pedido muda de status.
 */
add_action( 'woocommerce_order_status_changed', 'senderzz_notify_producer_on_status_change', 20, 3 );

function senderzz_notify_producer_on_status_change( int $order_id, string $old_status, string $new_status ): void {
    $config = senderzz_notification_config();

    // Só notifica status configurados
    $new_clean = str_replace( 'wc-', '', $new_status );
    if ( ! isset( $config[ $new_clean ] ) ) return;

    $order = wc_get_order( $order_id );
    if ( ! $order ) return;

    // Busca o e-mail do produtor via portal_users
    $class_id = (int) $order->get_meta( '_senderzz_product_shipping_class_id' );
    if ( ! $class_id ) return;

    global $wpdb;
    // Multi-class: busca usuário via tabela de relação (se existir) ou fallback legado.
    $mc_table_n = $wpdb->prefix . 'senderzz_portal_user_classes';
    $mc_exists_n = ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $mc_table_n ) ) === $mc_table_n );
    if ( $mc_exists_n ) {
        $portal_user = $wpdb->get_row( $wpdb->prepare(
            "SELECT pu.email, pu.name
               FROM {$mc_table_n} mc
               INNER JOIN {$wpdb->prefix}senderzz_portal_users pu ON pu.id = mc.portal_user_id
              WHERE mc.shipping_class_id = %d
                AND pu.status = 'active'
                AND (pu.parent_user_id IS NULL OR pu.parent_user_id = 0)
              ORDER BY pu.id ASC LIMIT 1",
            $class_id
        ) );
    } else {
        $portal_user = $wpdb->get_row( $wpdb->prepare(
            "SELECT email, name FROM {$wpdb->prefix}senderzz_portal_users
             WHERE shipping_class_id = %d AND status = 'active' AND (parent_user_id IS NULL OR parent_user_id = 0)
             ORDER BY id ASC LIMIT 1",
            $class_id
        ) );
    }

    if ( ! $portal_user || empty( $portal_user->email ) ) return;

    // Evitar spam: não renotifica o mesmo status para o mesmo pedido
    $notified_key = '_senderzz_notified_' . $new_clean;
    if ( $order->get_meta( $notified_key ) ) return;
    $order->update_meta_data( $notified_key, current_time( 'mysql' ) );
    $order->save();

    $cfg         = $config[ $new_clean ];
    $portal_url  = get_permalink( get_option( 'senderzz_portal_page_id' ) ) ?: home_url( '/meus-pedidos/' );
    $tracking    = $order->get_meta( '_melhor_envio_tracking_code' ) ?: '—';
    $carrier     = '';
    foreach ( $order->get_items( 'shipping' ) as $s ) { $carrier = $s->get_name(); break; }

    $subject = $cfg['icon'] . ' ' . $cfg['title'] . ' — Pedido #' . $order->get_order_number();
    $message = '
    <div style="font-family:var(--sz-font);max-width:520px;margin:0 auto;padding:0;background:#f9f9f9;border-radius:12px;overflow:hidden;">
        <div style="background:' . $cfg['color'] . ';padding:22px 28px;color:#fff;">
            <span style="font-size:var(--sz-text-3xl);">' . $cfg['icon'] . '</span>
            <h2 style="margin:8px 0 0;font-size:var(--sz-text-xl);">' . esc_html( $cfg['title'] ) . '</h2>
        </div>
        <div style="padding:24px 28px;">
            <p style="color:#374151;font-size:var(--sz-text-lg);margin:0 0 20px;">' . esc_html( $cfg['msg'] ) . '</p>
            <table style="width:100%;border-collapse:collapse;font-size:var(--sz-text-base);color:#374151;">
                <tr><td style="padding:8px 0;border-bottom:1px solid #e5e7eb;font-weight:600;width:140px;">Pedido</td><td style="padding:8px 0;border-bottom:1px solid #e5e7eb;">#' . esc_html( $order->get_order_number() ) . '</td></tr>
                <tr><td style="padding:8px 0;border-bottom:1px solid #e5e7eb;font-weight:600;">Status</td><td style="padding:8px 0;border-bottom:1px solid #e5e7eb;">' . esc_html( $cfg['title'] ) . '</td></tr>
                <tr><td style="padding:8px 0;border-bottom:1px solid #e5e7eb;font-weight:600;">Rastreio</td><td style="padding:8px 0;border-bottom:1px solid #e5e7eb;">' . esc_html( $tracking ) . '</td></tr>
                <tr><td style="padding:8px 0;font-weight:600;">Transportadora</td><td style="padding:8px 0;">' . esc_html( $carrier ?: '—' ) . '</td></tr>
            </table>
            <a href="' . esc_url( $portal_url ) . '" style="display:inline-block;margin-top:22px;background:#E8650A;color:#fff;text-decoration:none;padding:12px 24px;border-radius:8px;font-weight:700;font-size:var(--sz-text-md);">Ver no painel →</a>
        </div>
        <div style="padding:14px 28px;background:#f3f4f6;text-align:center;font-size:var(--sz-text-sm);color:#9ca3af;">
            Senderzz · Notificação automática · <a href="' . esc_url( $portal_url ) . '" style="color:#9ca3af;">Acessar painel</a>
        </div>
    </div>';

    $html_content_type = function() { return 'text/html'; };
    add_filter( 'wp_mail_content_type', $html_content_type );
    wp_mail( $portal_user->email, $subject, $message );
    remove_filter( 'wp_mail_content_type', $html_content_type );
}


// ── Push notification ao produtor: novo pedido on-hold ───────────────────────
// Complementa o e-mail existente com notificação push imediata.
// Usa o mesmo sistema de subscriptions do módulo de notificações do operador.

add_action( 'woocommerce_new_order', function( int $order_id ): void {
    $order = wc_get_order( $order_id );
    if ( ! $order ) return;
    // Agenda para após o checkout finalizar (status pode ainda não estar setado).
    wp_schedule_single_event( time() + 5, 'senderzz_push_new_order', [ $order_id ] );
}, 10 );

add_action( 'woocommerce_order_status_changed', function( int $order_id, string $old, string $new ): void {
    if ( $new !== 'on-hold' ) return;
    wp_schedule_single_event( time() + 3, 'senderzz_push_new_order', [ $order_id ] );
}, 10, 3 );

add_action( 'senderzz_push_new_order', function( int $order_id ): void {
    $order = wc_get_order( $order_id );
    if ( ! $order ) return;
    if ( ! in_array( $order->get_status(), [ 'on-hold', 'pending' ], true ) ) return;

    // Throttle: 1 push por pedido.
    if ( get_transient( 'sz_push_new_order_' . $order_id ) ) return;
    set_transient( 'sz_push_new_order_' . $order_id, 1, HOUR_IN_SECONDS );

    // Resolve o produtor pela classe de entrega do pedido.
    $class_id = (int) $order->get_meta( '_senderzz_product_shipping_class_id', true );
    if ( ! $class_id ) {
        // Fallback: tenta extrair do produto.
        foreach ( $order->get_items( 'line_item' ) as $item ) {
            $product = $item instanceof WC_Order_Item_Product ? $item->get_product() : null;
            if ( $product ) { $class_id = (int) $product->get_shipping_class_id(); break; }
        }
    }
    if ( ! $class_id ) return;

    // Busca wp_user_id do produtor.
    $wp_user_id = function_exists( 'senderzz_get_portal_wallet_owner_by_class_id' )
        ? senderzz_get_portal_wallet_owner_by_class_id( $class_id )
        : 0;
    if ( ! $wp_user_id ) return;

    $title   = '📦 Novo pedido #' . $order->get_order_number();
    $body    = 'R$ ' . number_format( (float) $order->get_total(), 2, ',', '.' ) . ' aguardando aprovação.';

    // Usa o helper de push do módulo de notificações, se disponível.
    if ( function_exists( 'senderzz_send_push_to_user' ) ) {
        senderzz_send_push_to_user( $wp_user_id, $title, $body );
        return;
    }

    // Fallback: dispara diretamente via Web Push para as subscriptions do usuário.
    global $wpdb;
    $subs = $wpdb->get_results( $wpdb->prepare(
        "SELECT * FROM {$wpdb->prefix}sz_push_subscriptions WHERE user_id = %d LIMIT 10",
        $wp_user_id
    ), ARRAY_A );
    if ( empty( $subs ) ) return;

    foreach ( $subs as $sub ) {
        $payload = wp_json_encode( [ 'title' => $title, 'body' => $body, 'url' => home_url( '/meus-pedidos/' ) ] );
        wp_remote_post( $sub['endpoint'], [
            'timeout' => 5,
            'headers' => [ 'Content-Type' => 'application/json', 'TTL' => '86400' ],
            'body'    => $payload,
        ] );
    }
} );
