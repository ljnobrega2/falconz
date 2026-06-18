<?php
/**
 * Senderzz — Audit Log de ações do produtor no portal
 *
 * Registra: aprovações, cancelamentos, chamados, reprocessamentos, 
 * mudanças de senha, configurações de transportadora.
 */

defined( 'ABSPATH' ) || exit;
if ( defined( 'SENDERZZ_AUDIT_LOG_LOADED' ) ) return;
define( 'SENDERZZ_AUDIT_LOG_LOADED', true );

define( 'SENDERZZ_AUDIT_TABLE', 'senderzz_portal_audit_log' );

// ── Instalar tabela ──────────────────────────────────────────────────────────

add_action( 'senderzz_portal_install', 'senderzz_audit_install_table' );

function senderzz_audit_install_table(): void {
    global $wpdb;
    $table   = $wpdb->prefix . SENDERZZ_AUDIT_TABLE;
    $charset = $wpdb->get_charset_collate();

    if ( $wpdb->get_var( $wpdb->prepare( "SHOW TABLES LIKE %s", $wpdb->esc_like( $table ) ) ) === $table ) return;

    $sql = "CREATE TABLE IF NOT EXISTS {$table} (
        id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        portal_user_id BIGINT UNSIGNED NOT NULL,
        action      VARCHAR(60) NOT NULL,
        order_id    BIGINT UNSIGNED NULL,
        meta        LONGTEXT NULL,
        ip          VARCHAR(45) NOT NULL DEFAULT '',
        created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (id),
        KEY portal_user_id (portal_user_id),
        KEY action (action),
        KEY created_at (created_at)
    ) {$charset};";

    require_once ABSPATH . 'wp-admin/includes/upgrade.php';
    dbDelta( $sql );
}

// Instalar na ativação do plugin
register_activation_hook( defined( 'SENDERZZ_PLUGIN_FILE' ) ? SENDERZZ_PLUGIN_FILE : __FILE__, 'senderzz_audit_install_table' );

// ── Função central de registro ───────────────────────────────────────────────

function senderzz_audit_log( int $portal_user_id, string $action, ?int $order_id = null, array $meta = [] ): void {
    if ( ! $portal_user_id || ! $action ) return;

    global $wpdb;
    $table = $wpdb->prefix . SENDERZZ_AUDIT_TABLE;

    // Verificar se tabela existe
    if ( $wpdb->get_var( $wpdb->prepare( "SHOW TABLES LIKE %s", $wpdb->esc_like( $table ) ) ) !== $table ) {
        senderzz_audit_install_table();
    }

    $ip = $_SERVER['HTTP_CF_CONNECTING_IP'] ?? $_SERVER['HTTP_X_REAL_IP'] ?? $_SERVER['HTTP_X_FORWARDED_FOR'] ?? $_SERVER['REMOTE_ADDR'] ?? '0.0.0.0';
    if ( strpos( $ip, ',' ) !== false ) $ip = trim( explode( ',', $ip )[0] );
    $ip = filter_var( trim( $ip ), FILTER_VALIDATE_IP ) ? trim( $ip ) : '0.0.0.0';

    $wpdb->insert( $table, [
        'portal_user_id' => $portal_user_id,
        'action'          => sanitize_key( $action ),
        'order_id'        => $order_id,
        'meta'            => ! empty( $meta ) ? wp_json_encode( $meta ) : null,
        'ip'              => $ip,
        'created_at'      => current_time( 'mysql', true ),
    ] );
}

// ── Hooks nas ações AJAX do portal ───────────────────────────────────────────

add_action( 'senderzz_portal_action', function( string $action, int $portal_user_id, array $result, array $post ) {
    if ( empty( $portal_user_id ) ) return;

    $loggable = [
        'approve'          => 'Aprovação de pedido',
        'cancel'           => 'Cancelamento de pedido',
        'loss'             => 'Relato de perda/extravio',
        'retry_label'      => 'Reprocessamento de etiqueta',
        'support'          => 'Chamado de suporte aberto',
        'set_preferred_carrier' => 'Modalidades de frete atualizadas',
        'set_blocked_carrier'   => 'Bloqueios de transportadora atualizados',
        'change_password'  => 'Senha alterada',
        'toggle_2fa'       => 'Configuração 2FA alterada',
    ];

    if ( ! isset( $loggable[ $action ] ) ) return;

    $order_id = isset( $post['order_id'] ) ? absint( $post['order_id'] ) : null;
    $meta = [ 'success' => ! empty( $result['success'] ) ];
    if ( ! empty( $result['message'] ) ) $meta['msg'] = substr( sanitize_text_field( $result['message'] ), 0, 200 );

    senderzz_audit_log( $portal_user_id, $action, $order_id, $meta );
}, 10, 4 );
