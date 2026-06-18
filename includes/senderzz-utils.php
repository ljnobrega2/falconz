<?php
/**
 * senderzz-utils.php — Funções utilitárias do Senderzz
 *
 * Funções helper usadas por Portal_Admin, Portal_Page, senderzz-engine e senderzz-rest.
 * Anteriormente: senderzz-fixes.php
 *
 * Inclui:
 *   - Helpers de role de operador
 *   - Funções de wallet e WP user (criar/sincronizar)
 *   - Limpeza de pedidos rascunho
 *   - Rastreamento de e-mail anterior
 */
defined( 'ABSPATH' ) || exit;
if ( defined( 'SENDERZZ_UTILS_LOADED' ) ) return;
define( 'SENDERZZ_UTILS_LOADED', true );

function senderzz_is_operator_portal_role( $role ): bool {
    return in_array( (string) $role, [ 'operator', 'operador', 'operador_logistico', 'logistics_operator' ], true );
}

function senderzz_is_draft_order_status( $status ): bool {
    $status = strtolower( str_replace( 'wc-', '', (string) $status ) );
    return in_array( $status, [ 'checkout-draft', 'draft', 'auto-draft' ], true );
}

function senderzz_force_delete_if_draft_order( $order ): bool {
    if ( ! $order instanceof WC_Order ) return false;
    if ( ! senderzz_is_draft_order_status( $order->get_status() ) ) return false;
    $order_id = $order->get_id();
    $order->delete( true );
    do_action( 'senderzz_draft_order_force_deleted', $order_id );
    return true;
}

function senderzz_cleanup_draft_orders( int $limit = 50 ): int {
    if ( ! function_exists( 'wc_get_orders' ) ) return 0;
    $statuses = [ 'checkout-draft', 'wc-checkout-draft', 'draft', 'wc-draft', 'auto-draft', 'wc-auto-draft' ];
    $orders = wc_get_orders( [
        'type'   => 'shop_order',
        'status' => $statuses,
        'limit'  => max( 1, min( 200, $limit ) ),
        'return' => 'objects',
    ] );
    $deleted = 0;
    foreach ( $orders as $order ) {
        if ( senderzz_force_delete_if_draft_order( $order ) ) $deleted++;
    }
    return $deleted;
}

function senderzz_ensure_tpc_wallet( int $wp_user_id ): bool {
    global $wpdb;
    if ( ! $wp_user_id || ! get_user_by( 'id', $wp_user_id ) ) return false;
    $wpdb->query( $wpdb->prepare(
        "INSERT INTO {$wpdb->prefix}tpc_carteira (user_id, saldo, saldo_reservado)
         VALUES (%d, 0.00, 0.00)
         ON DUPLICATE KEY UPDATE id = id",
        $wp_user_id
    ) );
    return true;
}

function senderzz_unique_user_login_from_email( string $email ): string {
    $base = sanitize_user( current( explode( '@', $email ) ), true );
    if ( ! $base ) $base = 'senderzz_user';
    $login = $base;
    $i = 1;
    while ( username_exists( $login ) ) {
        $login = $base . '_' . $i;
        $i++;
    }
    return $login;
}



if ( ! function_exists( 'senderzz_live_wp_user_display_name' ) ) {
    /**
     * Nome vivo do usuário Senderzz.
     * Prioridade: Nome/Sobrenome do WordPress, depois display_name, depois billing e login.
     * Evita usar nome congelado do senderzz_portal_users ou billing antigo no card lateral.
     */
    function senderzz_live_wp_user_display_name( int $wp_user_id, string $fallback = '' ): string {
        $wp_user_id = absint( $wp_user_id );
        $user = $wp_user_id ? get_userdata( $wp_user_id ) : null;
        if ( ! $user ) {
            return trim( $fallback ) !== '' ? trim( $fallback ) : '';
        }

        $first = trim( (string) get_user_meta( $wp_user_id, 'first_name', true ) );
        $last  = trim( (string) get_user_meta( $wp_user_id, 'last_name', true ) );
        $wp_name = trim( $first . ' ' . $last );

        $display = trim( (string) $user->display_name );

        $billing_first = trim( (string) get_user_meta( $wp_user_id, 'billing_first_name', true ) );
        $billing_last  = trim( (string) get_user_meta( $wp_user_id, 'billing_last_name', true ) );
        $billing_name  = trim( $billing_first . ' ' . $billing_last );

        foreach ( [ $wp_name, $display, $billing_name, trim( (string) $user->user_login ), trim( (string) $user->user_email ), trim( $fallback ) ] as $candidate ) {
            if ( $candidate !== '' ) {
                return $candidate;
            }
        }
        return '';
    }
}

if ( ! function_exists( 'senderzz_sync_portal_user_name_from_wp' ) ) {
    /** Sincroniza o nome vivo do WP para a tabela do portal sem alterar permissões/senha. */
    function senderzz_sync_portal_user_name_from_wp( int $wp_user_id ): void {
        global $wpdb;
        $wp_user_id = absint( $wp_user_id );
        if ( ! $wp_user_id ) return;

        $table = $wpdb->prefix . 'senderzz_portal_users';
        $exists = $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $table ) );
        if ( ! $exists ) return;

        $name = senderzz_live_wp_user_display_name( $wp_user_id );
        if ( $name === '' ) return;

        $user = get_userdata( $wp_user_id );
        $data = [ 'name' => $name ];
        $formats = [ '%s' ];
        if ( $user && is_email( $user->user_email ) ) {
            $data['email'] = $user->user_email;
            $formats[] = '%s';
        }

        $wpdb->update( $table, $data, [ 'wp_user_id' => $wp_user_id ], $formats, [ '%d' ] );
    }
}
add_action( 'profile_update', 'senderzz_sync_portal_user_name_from_wp', 20 );
add_action( 'personal_options_update', 'senderzz_sync_portal_user_name_from_wp', 20 );
add_action( 'edit_user_profile_update', 'senderzz_sync_portal_user_name_from_wp', 20 );

function senderzz_get_or_create_wp_user_for_portal_client( object $portal_user ): int {
    global $wpdb;
    $portal_table = $wpdb->prefix . 'senderzz_portal_users';
    $portal_id    = (int) ( $portal_user->id ?? 0 );
    $email        = sanitize_email( $portal_user->email ?? '' );
    $name         = sanitize_text_field( $portal_user->name ?? '' );
    $wp_user_id   = (int) ( $portal_user->wp_user_id ?? 0 );

    if ( $wp_user_id && ( $linked_wp_user = get_user_by( 'id', $wp_user_id ) ) ) {
        // O e-mail do portal é a fonte do login. Nunca reverte o portal para o e-mail antigo do WP,
        // porque isso desfaz a troca feita no painel e pode travar o acesso.
        // A carteira continua presa ao wp_user_id, então o saldo não depende do e-mail.
        if ( $email && strtolower( $email ) !== strtolower( $linked_wp_user->user_email ) ) {
            update_user_meta( $wp_user_id, '_senderzz_previous_emails', array_values( array_unique( array_filter( array_merge(
                (array) get_user_meta( $wp_user_id, '_senderzz_previous_emails', true ),
                [ strtolower( sanitize_email( $linked_wp_user->user_email ) ), strtolower( $email ) ]
            ) ) ) ) );
            $taken_wp = get_user_by( 'email', $email );
            if ( ! $taken_wp || (int) $taken_wp->ID === (int) $wp_user_id ) {
                $updated = wp_update_user( [ 'ID' => $wp_user_id, 'user_email' => $email ] );
                if ( ! is_wp_error( $updated ) ) {
                    clean_user_cache( $wp_user_id );
                }
            }
        }
        senderzz_ensure_tpc_wallet( $wp_user_id );
        if ( function_exists( 'senderzz_wallet_rebuild_from_transactions' ) ) senderzz_wallet_rebuild_from_transactions( $wp_user_id );
        return $wp_user_id;
    }

    $wp_user = $email ? get_user_by( 'email', $email ) : false;
    if ( $wp_user ) {
        $wp_user_id = (int) $wp_user->ID;
    } elseif ( $email && is_email( $email ) ) {
        $wp_user_id = (int) wp_insert_user( [
            'user_login'   => senderzz_unique_user_login_from_email( $email ),
            'user_email'   => $email,
            'display_name' => $name ?: $email,
            'first_name'   => $name,
            'user_pass'    => wp_generate_password( 24, true, true ),
            'role'         => 'customer',
        ] );
        if ( is_wp_error( $wp_user_id ) ) return 0;
    }

    if ( $wp_user_id ) {
        if ( $portal_id ) {
            $wpdb->update( $portal_table, [ 'wp_user_id' => $wp_user_id ], [ 'id' => $portal_id ], [ '%d' ], [ '%d' ] );
        }
        senderzz_ensure_tpc_wallet( $wp_user_id );
    }
    return $wp_user_id;
}

function senderzz_sync_portal_wallets(): void {
    global $wpdb;
    $table = $wpdb->prefix . 'senderzz_portal_users';
    $exists = $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $table ) );
    if ( ! $exists ) return;

    $rows = $wpdb->get_results( "SELECT id,email,name,role,wp_user_id FROM {$table} LIMIT 2000" ) ?: [];
    foreach ( $rows as $row ) {
        if ( senderzz_is_operator_portal_role( $row->role ?? '' ) ) {
            continue;
        }
        senderzz_get_or_create_wp_user_for_portal_client( $row );
    }
}

add_action( 'init', function() {
    if ( function_exists( 'tpc_create_tables' ) ) tpc_create_tables();

    // CRITICO: o login do portal usa admin-ajax.php.
    // Nao rode sincronizacao/limpeza pesada em AJAX/REST/CRON para nao travar o botao de login.
    if ( ( defined( 'DOING_AJAX' ) && DOING_AJAX ) || ( defined( 'REST_REQUEST' ) && REST_REQUEST ) || ( defined( 'DOING_CRON' ) && DOING_CRON ) ) {
        return;
    }

    // Sincronizacao pesada so no admin Woo, nunca em visita/login do portal.
    if ( is_admin() && current_user_can( 'manage_woocommerce' ) && ! get_transient( 'senderzz_wallet_sync_lock' ) ) {
        set_transient( 'senderzz_wallet_sync_lock', 1, 5 * MINUTE_IN_SECONDS );
        senderzz_sync_portal_wallets();
    }

    // Limpeza de rascunhos so no admin Woo, nunca no login.
    if ( is_admin() && current_user_can( 'manage_woocommerce' ) && ! get_transient( 'senderzz_draft_cleanup_lock' ) ) {
        set_transient( 'senderzz_draft_cleanup_lock', 1, 5 * MINUTE_IN_SECONDS );
        senderzz_cleanup_draft_orders( 50 );
    }
}, 30 );

add_action( 'woocommerce_new_order', function( $order_id ) {
    $order = wc_get_order( $order_id );
    if ( $order ) senderzz_force_delete_if_draft_order( $order );
}, 20 );

add_action( 'woocommerce_order_status_changed', function( $order_id, $old_status, $new_status ) {
    if ( senderzz_is_draft_order_status( $new_status ) ) {
        $order = wc_get_order( $order_id );
        if ( $order ) senderzz_force_delete_if_draft_order( $order );
    }
}, 20, 3 );

/* ── Senderzz v2.5.13: carteira imune à troca de e-mail ─────────────── */
function senderzz_wallet_rebuild_from_transactions( int $wp_user_id ): void {
    global $wpdb;
    if ( ! $wp_user_id ) return;
    $saldo = (float) $wpdb->get_var( $wpdb->prepare(
        "SELECT COALESCE(SUM(CASE WHEN tipo='credito' AND status='confirmado' THEN valor WHEN tipo='debito' AND status='confirmado' THEN -valor ELSE 0 END),0) FROM {$wpdb->prefix}tpc_transacoes WHERE user_id=%d",
        $wp_user_id
    ) );
    $reservado = (float) $wpdb->get_var( $wpdb->prepare(
        "SELECT COALESCE(SUM(CASE WHEN tipo='debito' AND status='pendente' THEN valor ELSE 0 END),0) FROM {$wpdb->prefix}tpc_transacoes WHERE user_id=%d",
        $wp_user_id
    ) );
    $wpdb->query( $wpdb->prepare(
        "INSERT INTO {$wpdb->prefix}tpc_carteira (user_id, saldo, saldo_reservado) VALUES (%d,%f,%f)
         ON DUPLICATE KEY UPDATE saldo=VALUES(saldo), saldo_reservado=VALUES(saldo_reservado)",
        $wp_user_id,
        round( $saldo, 2 ),
        round( $reservado, 2 )
    ) );
}

function senderzz_wallet_track_previous_email( int $user_id, WP_User $old_user_data = null ): void {
    static $running = false;
    if ( $running ) return;
    $running = true;
    if ( ! $user_id ) { $running = false; return; }
    $user = get_user_by( 'id', $user_id );
    if ( ! $user ) { $running = false; return; }
    $emails = get_user_meta( $user_id, '_senderzz_previous_emails', true );
    $emails = is_array( $emails ) ? $emails : [];
    if ( $old_user_data && ! empty( $old_user_data->user_email ) && strtolower( $old_user_data->user_email ) !== strtolower( $user->user_email ) ) {
        $emails[] = strtolower( sanitize_email( $old_user_data->user_email ) );
    }
    $emails[] = strtolower( sanitize_email( $user->user_email ) );
    $emails = array_values( array_unique( array_filter( $emails ) ) );
    update_user_meta( $user_id, '_senderzz_previous_emails', $emails );

    senderzz_ensure_tpc_wallet( $user_id );

    // Se a carteira existir vazia, mas houver transações antigas do mesmo usuário, recompõe saldo pelo user_id.
    $saldo_atual = function_exists( 'tpc_get_saldo' ) ? (float) tpc_get_saldo( $user_id ) : 0.0;
    if ( abs( $saldo_atual ) < 0.0001 ) {
        senderzz_wallet_rebuild_from_transactions( $user_id );
    }
    $running = false;
}
add_action( 'profile_update', 'senderzz_wallet_track_previous_email', 20, 2 );
add_action( 'user_register', function( $user_id ) { senderzz_wallet_track_previous_email( (int) $user_id, null ); }, 20 );
add_action( 'wp_login', function( $user_login, $user ) { if ( $user instanceof WP_User ) senderzz_wallet_track_previous_email( (int) $user->ID, null ); }, 20, 2 );

// Também garante a carteira ao consultar saldo via painel/API, sem depender do e-mail atual.
add_filter( 'senderzz_before_wallet_read_user_id', function( $user_id ) {
    senderzz_wallet_track_previous_email( (int) $user_id, null );
    return $user_id;
} );
