<?php
/**
 * database.php — Schema e helpers de query
 *
 * Tabelas:
 *   wp_tpc_carteira    — saldo atual por usuário (com lock otimista)
 *   wp_tpc_transacoes  — ledger imutável de movimentações
 *   wp_tpc_recargas    — recargas PIX (tabela dedicada para escala)
 *
 * v2.5.18:
 *   - Nova tabela tpc_recargas com me_pix_id UNIQUE (garante idempotência
 *     em ambiente com 1000+ clientes sem depender de wp_options)
 *   - Índice composto (user_id, status) para queries de polling eficientes
 *   - Campo expires_at para expiração sem full-scan
 *   - Índice me_pix_id para lookup O(log n) na confirmação
 */
defined( 'ABSPATH' ) || exit;
if ( defined( 'SENDERZZ_TPC_DATABASE_LOADED' ) ) return;
define( 'SENDERZZ_TPC_DATABASE_LOADED', true );

function tpc_create_tables(): void {
    global $wpdb;
    $charset = $wpdb->get_charset_collate();
    require_once ABSPATH . 'wp-admin/includes/upgrade.php';

    dbDelta( "CREATE TABLE IF NOT EXISTS {$wpdb->prefix}tpc_carteira (
        id              BIGINT UNSIGNED  NOT NULL AUTO_INCREMENT,
        user_id         BIGINT UNSIGNED  NOT NULL,
        saldo           DECIMAL(10,2)    NOT NULL DEFAULT 0.00,
        saldo_reservado DECIMAL(10,2)    NOT NULL DEFAULT 0.00,
        updated_at      DATETIME         NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
        PRIMARY KEY (id),
        UNIQUE KEY uq_user (user_id)
    ) $charset;" );

    dbDelta( "CREATE TABLE IF NOT EXISTS {$wpdb->prefix}tpc_transacoes (
        id              BIGINT UNSIGNED  NOT NULL AUTO_INCREMENT,
        user_id         BIGINT UNSIGNED  NOT NULL,
        tipo            ENUM('credito','debito') NOT NULL,
        valor           DECIMAL(10,2)    NOT NULL,
        saldo_apos      DECIMAL(10,2)    NOT NULL,
        descricao       VARCHAR(255)     NOT NULL DEFAULT '',
        referencia      VARCHAR(100)     NULL,
        order_id        BIGINT UNSIGNED  NULL,
        me_order_id     VARCHAR(100)     NULL,
        status          ENUM('pendente','analise','confirmado','cancelado') NOT NULL DEFAULT 'confirmado',
        meta_json       LONGTEXT         NULL,
        actor_id        BIGINT UNSIGNED  NULL,
        ip_address      VARCHAR(64)      NULL,
        created_at      DATETIME         NOT NULL DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (id),
        KEY idx_user        (user_id),
        KEY idx_order       (order_id),
        KEY idx_status      (status),
        KEY idx_user_status (user_id, status),
        KEY idx_created     (created_at),
        KEY idx_referencia  (referencia)
    ) $charset;" );

    // Tabela dedicada de recargas PIX — escalável para 1000+ clientes simultâneos.
    // me_pix_id é o ID retornado pelo ME ao criar o PIX: garante que dois processos
    // não confirmem a mesma recarga (UNIQUE impede INSERT duplicado no nível do DB).
    dbDelta( "CREATE TABLE IF NOT EXISTS {$wpdb->prefix}tpc_recargas (
        id              BIGINT UNSIGNED  NOT NULL AUTO_INCREMENT,
        user_id         BIGINT UNSIGNED  NOT NULL,
        valor           DECIMAL(10,2)    NOT NULL,
        status          ENUM('pendente','analise','confirmado','cancelado','expirado') NOT NULL DEFAULT 'pendente',
        me_pix_id       VARCHAR(128)     NULL COMMENT 'ID do pagamento retornado pelo ME',
        qr_src          TEXT             NULL,
        copia_cola      TEXT             NULL,
        link            VARCHAR(512)     NULL,
        expires_at      DATETIME         NULL,
        security_token  VARCHAR(64)      NULL COMMENT 'HMAC para validar redirect_url',
        saldo_me_antes  DECIMAL(10,2)    NULL COMMENT 'Saldo ME no momento da criação (backup)',
        tx_id           BIGINT UNSIGNED  NULL COMMENT 'ID da transação na tpc_transacoes após confirmação',
        ip_address      VARCHAR(64)      NULL,
        meta_json       LONGTEXT         NULL,
        created_at      DATETIME         NOT NULL DEFAULT CURRENT_TIMESTAMP,
        updated_at      DATETIME         NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
        PRIMARY KEY (id),
        UNIQUE KEY uq_me_pix_id (me_pix_id),
        KEY idx_user_status  (user_id, status),
        KEY idx_status       (status),
        KEY idx_expires_at   (expires_at),
        KEY idx_created_at   (created_at)
    ) $charset;" );


    // Eventos de webhook processados: idempotência persistente e auditoria.
    dbDelta( "CREATE TABLE IF NOT EXISTS {$wpdb->prefix}tpc_webhook_events (
        id              BIGINT UNSIGNED  NOT NULL AUTO_INCREMENT,
        event_key       VARCHAR(191)     NOT NULL,
        source          VARCHAR(50)      NOT NULL DEFAULT '',
        event_type      VARCHAR(80)      NOT NULL DEFAULT '',
        order_id        BIGINT UNSIGNED  NULL,
        recarga_id      BIGINT UNSIGNED  NULL,
        me_id           VARCHAR(128)     NULL,
        status          VARCHAR(80)      NULL,
        payload_hash    CHAR(64)         NOT NULL,
        processed_at    DATETIME         NOT NULL DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (id),
        UNIQUE KEY uq_event_key (event_key),
        KEY idx_source_type (source, event_type),
        KEY idx_order (order_id),
        KEY idx_recarga (recarga_id),
        KEY idx_me_status (me_id, status)
    ) $charset;" );

    // Migrações de colunas para tabelas existentes.
    tpc_maybe_add_column( $wpdb->prefix . 'tpc_carteira',   'saldo_reservado', 'DECIMAL(10,2) NOT NULL DEFAULT 0.00' );
    tpc_maybe_add_column( $wpdb->prefix . 'tpc_transacoes', 'meta_json',   'LONGTEXT NULL' );
    tpc_maybe_add_column( $wpdb->prefix . 'tpc_transacoes', 'actor_id',    'BIGINT UNSIGNED NULL' );
    tpc_maybe_add_column( $wpdb->prefix . 'tpc_transacoes', 'ip_address',  'VARCHAR(64) NULL' );
    tpc_maybe_add_column( $wpdb->prefix . 'tpc_transacoes', 'referencia',  'VARCHAR(100) NULL' );

    // Garante ENUM atualizado.
    $wpdb->query( "ALTER TABLE {$wpdb->prefix}tpc_transacoes
        MODIFY status ENUM('pendente','analise','confirmado','cancelado') NOT NULL DEFAULT 'confirmado'" );

    // Índices compostos para performance em alta escala (ignora erro se já existem).
    $wpdb->query( "ALTER TABLE {$wpdb->prefix}tpc_transacoes
        ADD KEY IF NOT EXISTS idx_user_status (user_id, status),
        ADD KEY IF NOT EXISTS idx_referencia (referencia)" );

    // S8: UNIQUE de idempotência no nível DB — impede race condition entre dois processos
    // com mesma referencia e tipo. NULLs em `referencia` são tratados como distintos pelo MySQL (OK).
    $wpdb->query( "ALTER TABLE {$wpdb->prefix}tpc_transacoes
        ADD UNIQUE KEY IF NOT EXISTS uq_user_ref_tipo (user_id, referencia, tipo)" );

    update_option( 'tpc_db_version', TPC_VERSION );
}

function tpc_maybe_add_column( string $table, string $column, string $definition ): void {
    global $wpdb;
    $exists = $wpdb->get_var( $wpdb->prepare( "SHOW COLUMNS FROM `$table` LIKE %s", $column ) );
    if ( ! $exists ) {
        $wpdb->query( "ALTER TABLE `$table` ADD `$column` $definition" );
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// HELPERS DE SALDO
// ─────────────────────────────────────────────────────────────────────────────

function tpc_get_saldo( int $user_id ): float {
    global $wpdb;
    $user_id = (int) apply_filters( 'senderzz_before_wallet_read_user_id', $user_id );
    $row = $wpdb->get_row( $wpdb->prepare(
        "SELECT saldo FROM {$wpdb->prefix}tpc_carteira WHERE user_id = %d",
        $user_id
    ) );
    return $row ? (float) $row->saldo : 0.00;
}

function tpc_get_saldo_reservado( int $user_id ): float {
    global $wpdb;
    $user_id = (int) apply_filters( 'senderzz_before_wallet_read_user_id', $user_id );
    $row = $wpdb->get_row( $wpdb->prepare(
        "SELECT saldo_reservado FROM {$wpdb->prefix}tpc_carteira WHERE user_id = %d",
        $user_id
    ) );
    return $row ? (float) $row->saldo_reservado : 0.00;
}

function tpc_get_saldo_disponivel( int $user_id ): float {
    return round( max( 0, tpc_get_saldo( $user_id ) - tpc_get_saldo_reservado( $user_id ) ), 2 );
}

function tpc_get_transacoes( int $user_id, array $args = [] ): array {
    global $wpdb;
    $a = wp_parse_args( $args, [
        'tipo'     => '', 'status'   => '',
        'data_ini' => '', 'data_fim' => '',
        'per_page' => 20, 'page'     => 1,
        'orderby'  => 'created_at', 'order' => 'DESC',
    ] );
    $where = [ $wpdb->prepare( 'user_id = %d', $user_id ) ];
    if ( $a['tipo'] )     $where[] = $wpdb->prepare( 'tipo = %s',   $a['tipo'] );
    if ( $a['status'] )   $where[] = $wpdb->prepare( 'status = %s', $a['status'] );
    if ( $a['data_ini'] ) $where[] = $wpdb->prepare( 'created_at >= %s', $a['data_ini'] . ' 00:00:00' );
    if ( $a['data_fim'] ) $where[] = $wpdb->prepare( 'created_at <= %s', $a['data_fim'] . ' 23:59:59' );
    $order   = strtoupper( $a['order'] ) === 'ASC' ? 'ASC' : 'DESC';
    $orderby = in_array( $a['orderby'], [ 'created_at', 'valor', 'tipo' ], true ) ? $a['orderby'] : 'created_at';
    $limit   = max( 1, min( 100, (int) $a['per_page'] ) );
    $offset  = ( max( 1, (int) $a['page'] ) - 1 ) * $limit;
    return $wpdb->get_results(
        $wpdb->prepare(
            "SELECT * FROM {$wpdb->prefix}tpc_transacoes WHERE " . implode( ' AND ', $where ) . " ORDER BY $orderby $order LIMIT %d OFFSET %d",
            $limit, $offset
        ),
        ARRAY_A
    ) ?: [];
}

function tpc_count_transacoes( int $user_id, array $args = [] ): int {
    global $wpdb;
    $where = [ $wpdb->prepare( 'user_id = %d', $user_id ) ];
    if ( ! empty( $args['tipo'] ) )     $where[] = $wpdb->prepare( 'tipo = %s',   $args['tipo'] );
    if ( ! empty( $args['status'] ) )   $where[] = $wpdb->prepare( 'status = %s', $args['status'] );
    if ( ! empty( $args['data_ini'] ) ) $where[] = $wpdb->prepare( 'created_at >= %s', $args['data_ini'] . ' 00:00:00' );
    if ( ! empty( $args['data_fim'] ) ) $where[] = $wpdb->prepare( 'created_at <= %s', $args['data_fim'] . ' 23:59:59' );
    return (int) $wpdb->get_var(
        "SELECT COUNT(*) FROM {$wpdb->prefix}tpc_transacoes WHERE " . implode( ' AND ', $where )
    );
}
