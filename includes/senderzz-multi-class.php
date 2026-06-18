<?php
/**
 * senderzz-multi-class.php
 *
 * Suporte a múltiplas classes de entrega por usuário do portal.
 *
 * Estratégia:
 *  - Tabela `senderzz_portal_user_classes` guarda relação N:N entre portal_user_id e shipping_class_id.
 *  - O campo legado `shipping_class_id` em `senderzz_portal_users` é MANTIDO como "classe primária"
 *    para zero breaking-change em código que ainda o lê diretamente.
 *  - Toda lógica de queries de pedidos/wallet/notificações passa a usar
 *    `sz_get_user_class_ids()` que retorna array unificado (legado + novas).
 */

if ( ! defined( 'ABSPATH' ) ) exit;

// ──────────────────────────────────────────────────────────────────────────────
// 1. INSTALL / MIGRATE
// ──────────────────────────────────────────────────────────────────────────────

function sz_mc_install(): void {
    global $wpdb;
    $charset = $wpdb->get_charset_collate();
    $table   = $wpdb->prefix . 'senderzz_portal_user_classes';

    $sql = "CREATE TABLE IF NOT EXISTS {$table} (
        id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        portal_user_id  BIGINT UNSIGNED NOT NULL,
        shipping_class_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
        created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (id),
        UNIQUE KEY uniq_user_class (portal_user_id, shipping_class_id),
        KEY idx_class (shipping_class_id),
        KEY idx_user  (portal_user_id)
    ) {$charset};";

    require_once ABSPATH . 'wp-admin/includes/upgrade.php';
    dbDelta( $sql );

    // Migração: popula a nova tabela com o shipping_class_id legado de cada usuário.
    $wpdb->query(
        "INSERT IGNORE INTO {$table} (portal_user_id, shipping_class_id)
         SELECT id, shipping_class_id
         FROM {$wpdb->prefix}senderzz_portal_users
         WHERE shipping_class_id > 0"
    );
}
add_action( 'senderzz_install', 'sz_mc_install' );
// Garante execução também em upgrade (chamado pelo hook existente de upgrade do plugin).
add_action( 'plugins_loaded', function() {
    $ver_key = 'sz_mc_db_ver';
    if ( get_option( $ver_key ) !== '1' ) {
        sz_mc_install();
        update_option( $ver_key, '1' );
    }
}, 5 );


// ──────────────────────────────────────────────────────────────────────────────
// 2. HELPERS CENTRAIS
// ──────────────────────────────────────────────────────────────────────────────

/**
 * Retorna array de shipping_class_ids do usuário do portal.
 * Fonte de verdade para todo código que precisa filtrar pedidos/webhook/etc.
 *
 * @param  int|object $portal_user  ID do portal_user OU objeto com ->id e ->shipping_class_id
 * @return int[]  Array de IDs (pode ser vazio).
 */
function sz_get_user_class_ids( $portal_user ): array {
    global $wpdb;

    $portal_user_id = is_object( $portal_user ) ? (int) $portal_user->id : (int) $portal_user;
    if ( $portal_user_id <= 0 ) return [];

    // Senderzz v282: aceita tanto senderzz_portal_users.id quanto wp_users.ID.
    // Isso evita gravar/consultar classes no usuário errado quando algum fluxo envia o WP ID.
    if ( ! is_object( $portal_user ) ) {
        $resolved_portal_id = (int) $wpdb->get_var( $wpdb->prepare(
            "SELECT id FROM {$wpdb->prefix}senderzz_portal_users WHERE id = %d LIMIT 1",
            $portal_user_id
        ) );
        if ( $resolved_portal_id <= 0 ) {
            $resolved_portal_id = (int) $wpdb->get_var( $wpdb->prepare(
                "SELECT id FROM {$wpdb->prefix}senderzz_portal_users WHERE wp_user_id = %d LIMIT 1",
                $portal_user_id
            ) );
        }
        if ( $resolved_portal_id > 0 ) {
            $portal_user_id = $resolved_portal_id;
        }
    }

    $table = $wpdb->prefix . 'senderzz_portal_user_classes';
    $table_exists = ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $table ) ) === $table );

    $ids = [];

    if ( $table_exists ) {
        $rows = $wpdb->get_col( $wpdb->prepare(
            "SELECT shipping_class_id FROM {$table} WHERE portal_user_id = %d",
            $portal_user_id
        ) );
        $ids = array_map( 'intval', $rows ?: [] );
    }

    // Fallback / legado: inclui o campo shipping_class_id direto do objeto/db
    if ( is_object( $portal_user ) ) {
        $legacy = (int) ( $portal_user->shipping_class_id ?? 0 );
    } else {
        $legacy = (int) $wpdb->get_var( $wpdb->prepare(
            "SELECT shipping_class_id FROM {$wpdb->prefix}senderzz_portal_users WHERE id = %d",
            $portal_user_id
        ) );
    }
    if ( $legacy > 0 && ! in_array( $legacy, $ids, true ) ) {
        $ids[] = $legacy;
    }

    return array_values( array_unique( array_filter( $ids, fn( $id ) => $id >= 0 ) ) );
}

/**
 * Atualiza as classes de um usuário do portal.
 * Sincroniza a tabela multi-class E atualiza shipping_class_id (primeira classe = primária).
 *
 * @param int   $portal_user_id
 * @param int[] $class_ids       Array de shipping_class_ids (pode ser vazio).
 */
function sz_set_user_class_ids( int $portal_user_id, array $class_ids ): void {
    global $wpdb;

    sz_mc_install(); // garante tabela

    // Senderzz v282: se chegar wp_users.ID, resolve para senderzz_portal_users.id.
    $resolved_portal_id = (int) $wpdb->get_var( $wpdb->prepare(
        "SELECT id FROM {$wpdb->prefix}senderzz_portal_users WHERE id = %d LIMIT 1",
        $portal_user_id
    ) );
    if ( $resolved_portal_id <= 0 ) {
        $resolved_portal_id = (int) $wpdb->get_var( $wpdb->prepare(
            "SELECT id FROM {$wpdb->prefix}senderzz_portal_users WHERE wp_user_id = %d LIMIT 1",
            $portal_user_id
        ) );
    }
    if ( $resolved_portal_id > 0 ) {
        $portal_user_id = $resolved_portal_id;
    }

    $class_ids = array_values( array_unique( array_map( 'intval', array_filter( $class_ids, fn($v) => (int)$v > 0 ) ) ) );
    $table     = $wpdb->prefix . 'senderzz_portal_user_classes';

    // Remove todas as classes atuais do usuário
    $wpdb->delete( $table, [ 'portal_user_id' => $portal_user_id ], [ '%d' ] );

    // Insere as novas
    foreach ( $class_ids as $cid ) {
        $wpdb->replace( $table, [
            'portal_user_id'   => $portal_user_id,
            'shipping_class_id' => $cid,
        ], [ '%d', '%d' ] );
    }

    // Atualiza campo legado (primeira classe como primária, 0 se nenhuma)
    $primary = ! empty( $class_ids ) ? $class_ids[0] : 0;
    $wpdb->update(
        $wpdb->prefix . 'senderzz_portal_users',
        [ 'shipping_class_id' => $primary ],
        [ 'id' => $portal_user_id ],
        [ '%d' ],
        [ '%d' ]
    );
}

/**
 * Verifica se um pedido pertence ao usuário (qualquer uma de suas classes).
 */
function sz_order_belongs_to_user( \WC_Order $order, $portal_user ): bool {
    $order_class = (int) $order->get_meta( '_senderzz_product_shipping_class_id' );
    $user_classes = sz_get_user_class_ids( $portal_user );
    if ( empty( $user_classes ) ) return false;
    return in_array( $order_class, $user_classes, true );
}

/**
 * Constrói cláusula SQL segura para "shipping_class_id IN (...)" dados um array de IDs.
 * Retorna string pronta para uso em wpdb->prepare() como literal (já escaped).
 */
function sz_class_ids_in_sql( array $class_ids ): string {
    global $wpdb;
    $safe = array_map( 'intval', $class_ids );
    $safe = array_filter( $safe, fn($id) => $id >= 0 );
    if ( empty( $safe ) ) return '(0)'; // impossível — garante query segura sem resultado
    return '(' . implode( ',', $safe ) . ')';
}

// ──────────────────────────────────────────────────────────────────────────────
// 4. SYNC LEGADO — corrige shipping_class_id=0 para todos os usuários que já
//    têm classes na tabela nova mas o campo legado ainda é 0.
// ──────────────────────────────────────────────────────────────────────────────

function sz_mc_sync_legacy_field(): int {
    global $wpdb;
    $mc_table   = $wpdb->prefix . 'senderzz_portal_user_classes';
    $user_table = $wpdb->prefix . 'senderzz_portal_users';

    $mc_exists = ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $mc_table ) ) === $mc_table );
    if ( ! $mc_exists ) return 0;

    // Encontra usuários onde shipping_class_id=0 mas existem entradas na tabela multi-class
    $users = $wpdb->get_results(
        "SELECT pu.id, MIN(mc.shipping_class_id) AS primary_class
           FROM {$user_table} pu
           INNER JOIN {$mc_table} mc ON mc.portal_user_id = pu.id
          WHERE pu.shipping_class_id = 0
            AND mc.shipping_class_id > 0
          GROUP BY pu.id"
    ) ?: [];

    $fixed = 0;
    foreach ( $users as $row ) {
        $wpdb->update(
            $user_table,
            [ 'shipping_class_id' => (int) $row->primary_class ],
            [ 'id' => (int) $row->id ],
            [ '%d' ],
            [ '%d' ]
        );
        $fixed++;
    }
    return $fixed;
}

// Roda automaticamente no plugins_loaded se houver usuários desincronizados
add_action( 'plugins_loaded', function() {
    // Só roda uma vez por hora para não sobrecarregar
    if ( get_transient( 'sz_mc_legacy_synced' ) ) return;
    set_transient( 'sz_mc_legacy_synced', 1, HOUR_IN_SECONDS );
    sz_mc_sync_legacy_field();
}, 20 );

// Endpoint admin-post para forçar sync manual
add_action( 'admin_post_sz_mc_force_sync', function() {
    if ( ! current_user_can( 'manage_woocommerce' ) ) wp_die( 'Sem permissão.' );
    check_admin_referer( 'sz_mc_force_sync' );
    $fixed = sz_mc_sync_legacy_field();
    set_transient( 'senderzz_portal_admin_notice', [
        'type'    => 'success',
        'message' => "Sincronização concluída: {$fixed} usuário(s) corrigido(s).",
    ], 30 );
    wp_safe_redirect( admin_url( 'admin.php?page=senderzz&tab=usuarios' ) );
    exit;
} );
