<?php
/**
 * operator.php
 *
 * Módulo do Operador Logístico (OL) — sócio físico em SP.
 *
 * - Login no mesmo portal Senderzz (senderzz_portal_users com role=operator)
 * - Dashboard de turno: prontos / embalados / coletados
 * - Fila de trabalho filtrada (sem dados financeiros)
 * - Ações: marcar como Embalado e Entregue na Coleta
 * - Impressão em lote de etiquetas PDF
 */
defined( 'ABSPATH' ) || exit;
if ( defined( 'SENDERZZ_OPERATOR_LOADED' ) ) return;
define( 'SENDERZZ_OPERATOR_LOADED', true );


/**
 * Libera leitura HTTP dos PDFs de etiqueta já salvos.
 * Antes havia "Deny from all" em /uploads/senderzz-labels, causando 403 Forbidden.
 */
function senderzz_operator_fix_labels_htaccess(): void {
    $upload_dir = wp_upload_dir();
    if ( empty( $upload_dir['basedir'] ) ) return;

    $root_labels = trailingslashit( $upload_dir['basedir'] ) . 'senderzz-labels';
    if ( ! file_exists( $root_labels ) ) {
        wp_mkdir_p( $root_labels );
    }

    file_put_contents( $root_labels . '/.htaccess', "Options -Indexes
<FilesMatch \"\\.pdf$\">
Require all granted
</FilesMatch>
" );

    if ( ! file_exists( $root_labels . '/index.php' ) ) {
        file_put_contents( $root_labels . '/index.php', '<?php // Silence is golden.' );
    }
}
add_action( 'init', 'senderzz_operator_fix_labels_htaccess', 6 );

/**
 * REST auth: injeta o usuário do portal na sessão WP quando o cookie
 * senderzz_portal_session identifica um operador válido.
 *
 * Sem isso, o WP REST retorna 401 antes de chamar permission_callback
 * quando o operador não tem conta WP logada (usuário só do portal).
 *
 * Funciona via determine_current_user — o WP chama isso cedo no bootstrap
 * REST e o resultado é usado para validar nonces e permissions.
 */
add_filter( 'rest_authentication_errors', function ( $result ) {
    // Se já autenticou ou deu erro real, não interfere
    if ( $result !== null ) return $result;

    $token = $_COOKIE['senderzz_portal_session'] ?? '';
    if ( ! $token ) return $result;

    global $wpdb;
    [ $token_raw, $token_hash ] = senderzz_operator_session_token_values( (string) $token );

    $sess_table = $wpdb->prefix . 'senderzz_portal_sessions';
    $user_table = $wpdb->prefix . 'senderzz_portal_users';

    $session = $wpdb->get_row( $wpdb->prepare(
        "SELECT s.user_id FROM {$sess_table} s WHERE s.token IN (%s,%s) AND s.expires_at > NOW() LIMIT 1",
        $token_raw, $token_hash
    ) );
    if ( ! $session ) return $result;

    $portal_user = $wpdb->get_row( $wpdb->prepare(
        "SELECT role, status, wp_user_id FROM {$user_table} WHERE id=%d AND status='active' LIMIT 1",
        (int) $session->user_id
    ) );
    if ( ! $portal_user ) return $result;

    $role = strtolower( trim( (string) ( $portal_user->role ?? '' ) ) );
    $is_operator = strpos( $role, 'operator' ) !== false || strpos( $role, 'operador' ) !== false;
    if ( ! $is_operator ) return $result;

    // Operador válido — retorna null (sem erro) para o WP não bloquear com 401
    // A permission_callback do endpoint vai fazer a validação completa
    return null;
}, 10 );


/**
 * Quando um usuário WordPress é criado/atualizado,
 * verifica se existe um portal user com o mesmo e-mail
 * e preenche o wp_user_id automaticamente.
 */
add_action( 'user_register', 'senderzz_link_portal_user_on_wp_register', 10, 1 );
add_action( 'profile_update', 'senderzz_link_portal_user_on_wp_register', 10, 1 );
function senderzz_link_portal_user_on_wp_register( int $wp_user_id ): void {
    $user = get_userdata( $wp_user_id );
    if ( ! $user ) return;
    global $wpdb;
    $table = $wpdb->prefix . 'senderzz_portal_users';
    // Só atualiza se ainda está NULL para não sobrescrever uma ligação intencional
    $wpdb->query( $wpdb->prepare(
        "UPDATE {$table} SET wp_user_id = %d WHERE email = %s AND (wp_user_id IS NULL OR wp_user_id = 0)",
        $wp_user_id,
        $user->user_email
    ) );
}

/**
 * Quando um portal user é criado sem wp_user_id,
 * tenta linkar em segundo plano via WP Cron (caso o WP user seja criado depois).
 */
add_action( 'tpc_pos_movimentacao', 'senderzz_link_portal_user_on_transaction', 10, 1 );
function senderzz_link_portal_user_on_transaction( int $wp_user_id ): void {
    senderzz_link_portal_user_on_wp_register( $wp_user_id );
}

/* ── Migração: adiciona coluna role na tabela de usuários do portal ──── */

add_action( 'init', function () {
    global $wpdb;
    $table = $wpdb->prefix . 'senderzz_portal_users';
    $col   = $wpdb->get_var( $wpdb->prepare( "SHOW COLUMNS FROM `{$table}` LIKE %s", 'role' ) );
    if ( ! $col ) {
        $wpdb->query( "ALTER TABLE `{$table}` ADD `role` VARCHAR(30) NOT NULL DEFAULT 'client' AFTER `status`" );
    }
    // Tabela de webhook configs
    $wpdb->query( "CREATE TABLE IF NOT EXISTS {$wpdb->prefix}senderzz_webhooks (
        id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        portal_user_id BIGINT UNSIGNED NOT NULL,
        url         VARCHAR(512) NOT NULL,
        secret      VARCHAR(128) NOT NULL DEFAULT '',
        active      TINYINT(1) NOT NULL DEFAULT 1,
        created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (id),
        KEY idx_user (portal_user_id)
    ) {$wpdb->get_charset_collate()};" );

    // DT-CODE-02: adicionar event_types para filtragem por evento (migração segura com IF NOT EXISTS)
    $wpdb->query( "ALTER TABLE {$wpdb->prefix}senderzz_webhooks
        ADD COLUMN IF NOT EXISTS event_types VARCHAR(500) NOT NULL DEFAULT '' COMMENT 'JSON array de eventos; vazio = todos'" );

    // Tabela de log de disparos de webhook
    $wpdb->query( "CREATE TABLE IF NOT EXISTS {$wpdb->prefix}senderzz_webhook_log (
        id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        webhook_id      BIGINT UNSIGNED NOT NULL,
        order_id        BIGINT UNSIGNED NOT NULL,
        event           VARCHAR(60) NOT NULL,
        payload_json    LONGTEXT NULL,
        response_code   SMALLINT NULL,
        response_body   TEXT NULL,
        fired_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (id),
        KEY idx_webhook (webhook_id),
        KEY idx_order   (order_id),
        KEY idx_fired   (fired_at)
    ) {$wpdb->get_charset_collate()};" );
}, 6 );

/* ── Helpers ─────────────────────────────────────────────────────────── */

function senderzz_create_operator( string $email, string $password, string $name = '' ): array {
    global $wpdb;
    $table = $wpdb->prefix . 'senderzz_portal_users';
    $email = sanitize_email( $email );
    if ( ! is_email( $email ) ) return [ 'success' => false, 'message' => 'Email inválido.' ];
    if ( $wpdb->get_var( $wpdb->prepare( "SELECT id FROM {$table} WHERE email = %s", $email ) ) ) {
        return [ 'success' => false, 'message' => 'Email já cadastrado.' ];
    }
    $wp_user    = get_user_by( 'email', $email );
    $wp_user_id = $wp_user ? $wp_user->ID : null;

    $ok = $wpdb->insert( $table, [
        'email'             => $email,
        'password_hash'     => wp_hash_password( $password ),
        'shipping_class_id' => 0,
        'name'              => sanitize_text_field( $name ),
        'parent_user_id'    => null,
        'permissions'       => wp_json_encode( [ 'operator' => true ] ),
        'status'            => 'active',
        'role'              => 'operator',
        'wp_user_id'        => $wp_user_id,
    ] );
    if ( ! $ok ) return [ 'success' => false, 'message' => 'Erro ao criar operador.' ];
    return [ 'success' => true, 'user_id' => (int) $wpdb->insert_id ];
}

function senderzz_create_portal_client( string $email, string $password, string $name = '' ): array {
    global $wpdb;
    $table = $wpdb->prefix . 'senderzz_portal_users';
    $email = sanitize_email( $email );
    if ( ! is_email( $email ) ) return [ 'success' => false, 'message' => 'Email inválido.' ];
    if ( $wpdb->get_var( $wpdb->prepare( "SELECT id FROM {$table} WHERE email = %s", $email ) ) ) {
        return [ 'success' => false, 'message' => 'Email já cadastrado.' ];
    }
    $wp_user_c    = get_user_by( 'email', $email );
    $wp_user_id_c = $wp_user_c ? $wp_user_c->ID : null;

    $ok = $wpdb->insert( $table, [
        'email'             => $email,
        'password_hash'     => wp_hash_password( $password ),
        'shipping_class_id' => 0,
        'name'              => sanitize_text_field( $name ),
        'permissions'       => wp_json_encode( [ 'wallet' => true, 'approve' => true, 'cancel' => true, 'links' => true ] ),
        'status'            => 'active',
        'role'              => 'client',
        'wp_user_id'        => $wp_user_id_c,
    ] );
    if ( ! $ok ) return [ 'success' => false, 'message' => 'Erro ao criar cliente.' ];
    return [ 'success' => true, 'user_id' => (int) $wpdb->insert_id ];
}

function senderzz_is_operator( ?object $user ): bool {
    if ( ! $user ) return false;
    return isset( $user->role ) && $user->role === 'operator';
}

/**
 * Retorna pedidos para a fila do operador.
 * Inclui APENAS pedidos com etiqueta emitida e sem dados financeiros.
 */
function senderzz_operator_order_class_info( \WC_Order $order ): array {
    $class_id = (int) $order->get_meta( '_senderzz_product_shipping_class_id' );
    $class_name = '';

    if ( $class_id > 0 ) {
        $term = get_term( $class_id, 'product_shipping_class' );
        if ( $term && ! is_wp_error( $term ) ) {
            $class_name = $term->name;
        }
    }

    if ( $class_id <= 0 ) {
        foreach ( $order->get_items() as $item ) {
            $product = $item->get_product();
            if ( ! $product ) continue;
            $pid = $product->is_type( 'variation' ) ? $product->get_parent_id() : $product->get_id();
            $terms = get_the_terms( $pid, 'product_shipping_class' );
            if ( is_array( $terms ) && ! empty( $terms ) && ! is_wp_error( $terms ) ) {
                $class_id = (int) $terms[0]->term_id;
                $class_name = $terms[0]->name;
                break;
            }
        }
    }

    return [
        'id'   => $class_id,
        'name' => $class_id > 0 ? ( $class_name ?: '#' . $class_id ) : 'Sem classe',
        'slug' => $class_id > 0 ? sanitize_title( $class_name ?: 'classe-' . $class_id ) : 'sem-classe',
    ];
}

function senderzz_operator_money( float $value ): string {
    return 'R$ ' . number_format( $value, 2, ',', '.' );
}

function senderzz_operator_is_valid_pdf_file( $path ): bool {
    if ( ! $path || ! is_string( $path ) || ! file_exists( $path ) || filesize( $path ) < 20 ) return false;
    $fh = fopen( $path, 'rb' );
    if ( ! $fh ) return false;
    $head = fread( $fh, 4 );
    fclose( $fh );
    return $head === '%PDF';
}

function senderzz_operator_business_hours_since( int $from_ts, int $to_ts = 0 ): int {
    if ( $to_ts <= 0 ) $to_ts = current_time( 'timestamp' );
    if ( $from_ts <= 0 || $to_ts <= $from_ts ) return 0;
    $hours = 0;
    $cursor = $from_ts;
    while ( $cursor < $to_ts ) {
        $dow = (int) wp_date( 'N', $cursor );
        if ( $dow >= 1 && $dow <= 5 ) $hours++;
        $cursor += HOUR_IN_SECONDS;
        if ( $hours > 10000 ) break;
    }
    return $hours;
}

function senderzz_operator_items_summary( \WC_Order $order ): string {
    // Regra Senderzz: quantidade + nome, uma vez só (sem listar itens extras).
    if ( function_exists( 'senderzz_order_primary_item_label' ) ) {
        $label = senderzz_order_primary_item_label( $order );
        if ( $label !== '' ) return $label;
    }
    foreach ( $order->get_items() as $item ) {
        $name = function_exists( 'senderzz_clean_product_label' ) ? senderzz_clean_product_label( $item->get_name() ) : trim( (string) $item->get_name() );
        if ( $name === '' ) continue;
        $qty = max( 1, (int) $item->get_quantity() );
        return $qty . 'x ' . $name;
    }
    return '';
}


function senderzz_operator_motoboy_row( int $order_id ) {
    global $wpdb;
    $table = $wpdb->prefix . 'sz_motoboy_pedidos';
    return $wpdb->get_row( $wpdb->prepare( "SELECT * FROM {$table} WHERE wc_order_id = %d LIMIT 1", $order_id ) );
}

function senderzz_operator_order_motoboy_meta_keys_id(): array {
    // Fonte única de leitura/escrita a partir da v339.
    return [ '_senderzz_motoboy_id' ];
}

function senderzz_operator_order_motoboy_alias_meta_keys_id(): array {
    // Legado: só serve para migração/limpeza, nunca como fonte principal.
    return [
        '_sz_motoboy_id',
        '_motoboy_user_id',
        '_senderzz_motoboy_responsavel_id',
        '_senderzz_motoboy_entregador_id',
        '_senderzz_motoboy_assigned_id',
        '_senderzz_motoboy_atribuido_id',
        '_senderzz_motoboy_baixa_motoboy_id',
    ];
}

function senderzz_operator_order_motoboy_alias_meta_keys_name(): array {
    return [
        '_senderzz_motoboy_name',
        '_sz_motoboy_name',
        '_sz_motoboy_nome',
        '_motoboy_name',
        '_motoboy_nome',
        '_senderzz_motoboy_responsavel_nome',
        '_senderzz_motoboy_entregador_nome',
    ];
}

function senderzz_operator_lookup_motoboy_nome( int $motoboy_id ): string {
    global $wpdb;
    if ( $motoboy_id <= 0 ) return '';
    $nome = $wpdb->get_var( $wpdb->prepare( "SELECT nome FROM {$wpdb->prefix}sz_motoboys WHERE id=%d LIMIT 1", $motoboy_id ) );
    return $nome ? (string) $nome : '';
}

function senderzz_operator_cleanup_order_motoboy_duplicates( \WC_Order $order, $motoboy_row = null, bool $save = true ): array {
    global $wpdb;

    $order_id = (int) $order->get_id();
    $canonical_id = (int) $order->get_meta( '_senderzz_motoboy_id', true );

    // Migração segura: se ainda não existe fonte oficial, usa a linha operacional como ponte.
    if ( $canonical_id <= 0 && $motoboy_row && (int) ( $motoboy_row->motoboy_id ?? 0 ) > 0 ) {
        $canonical_id = (int) $motoboy_row->motoboy_id;
    }

    // Último fallback apenas para pedidos antigos, antes de apagar os metadados legados.
    if ( $canonical_id <= 0 ) {
        foreach ( senderzz_operator_order_motoboy_alias_meta_keys_id() as $key ) {
            $v = $order->get_meta( $key, true );
            if ( $v !== '' && $v !== null && (int) $v > 0 ) { $canonical_id = (int) $v; break; }
        }
    }

    $canonical_name = trim( (string) $order->get_meta( '_senderzz_motoboy_nome', true ) );
    if ( $canonical_name === '' && $canonical_id > 0 ) $canonical_name = senderzz_operator_lookup_motoboy_nome( $canonical_id );

    // Apaga metadados duplicados que já causaram divergência visual/financeira.
    foreach ( senderzz_operator_order_motoboy_alias_meta_keys_id() as $key ) {
        if ( $order->get_meta( $key, true ) !== '' ) $order->delete_meta_data( $key );
    }
    foreach ( senderzz_operator_order_motoboy_alias_meta_keys_name() as $key ) {
        if ( $order->get_meta( $key, true ) !== '' ) $order->delete_meta_data( $key );
    }

    if ( $canonical_id > 0 ) {
        $order->update_meta_data( '_senderzz_motoboy_id', $canonical_id );
        $order->update_meta_data( '_senderzz_motoboy_nome', $canonical_name );
        $order->update_meta_data( '_senderzz_delivery_mode', 'motoboy' );

        // A tabela operacional deixa de ser fonte; apenas espelha o pedido Woo.
        if ( $motoboy_row && (int) ( $motoboy_row->motoboy_id ?? 0 ) !== $canonical_id ) {
            $wpdb->update(
                $wpdb->prefix . 'sz_motoboy_pedidos',
                [ 'motoboy_id' => $canonical_id, 'updated_at' => current_time( 'mysql' ) ],
                [ 'wc_order_id' => $order_id ]
            );
        }
    }

    if ( $save ) $order->save();
    return [ 'id' => $canonical_id, 'nome' => $canonical_name ];
}

function senderzz_operator_order_motoboy_id( \WC_Order $order, $motoboy_row = null ): int {
    $data = senderzz_operator_cleanup_order_motoboy_duplicates( $order, $motoboy_row, false );
    return (int) ( $data['id'] ?? 0 );
}

function senderzz_operator_order_motoboy_name( \WC_Order $order, $motoboy_row = null ): string {
    $data = senderzz_operator_cleanup_order_motoboy_duplicates( $order, $motoboy_row, false );
    return (string) ( $data['nome'] ?? '' );
}

function senderzz_operator_set_order_motoboy_meta( int $order_id, int $motoboy_id, string $motoboy_nome = '' ): void {
    if ( $order_id <= 0 || $motoboy_id <= 0 || ! function_exists( 'wc_get_order' ) ) return;
    $order = wc_get_order( $order_id );
    if ( ! $order instanceof \WC_Order ) return;
    if ( $motoboy_nome === '' ) $motoboy_nome = senderzz_operator_lookup_motoboy_nome( $motoboy_id );

    // Escrita única. Não grava mais aliases/duplicidades.
    $order->update_meta_data( '_senderzz_motoboy_id', $motoboy_id );
    $order->update_meta_data( '_senderzz_motoboy_nome', $motoboy_nome );
    $order->update_meta_data( '_senderzz_delivery_mode', 'motoboy' );

    foreach ( senderzz_operator_order_motoboy_alias_meta_keys_id() as $key ) $order->delete_meta_data( $key );
    foreach ( senderzz_operator_order_motoboy_alias_meta_keys_name() as $key ) $order->delete_meta_data( $key );

    $order->save();
}


function senderzz_operator_purge_motoboy_financial_rows_for_order( int $order_id ): void {
    global $wpdb;
    if ( $order_id <= 0 ) return;
    foreach ( [ 'sz_motoboy_wallet', 'sz_motoboy_fechamento' ] as $suffix ) {
        $table = $wpdb->prefix . $suffix;
        if ( ! $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $table ) ) ) continue;
        $cols = $wpdb->get_col( "SHOW COLUMNS FROM {$table}", 0 );
        if ( in_array( 'wc_order_id', (array) $cols, true ) ) {
            $wpdb->delete( $table, [ 'wc_order_id' => $order_id ], [ '%d' ] );
        } elseif ( in_array( 'order_id', (array) $cols, true ) ) {
            $wpdb->delete( $table, [ 'order_id' => $order_id ], [ '%d' ] );
        }
    }
}

function senderzz_operator_cleanup_recent_motoboy_duplicates( int $limit = 800 ): void {
    global $wpdb;
    if ( ! function_exists( 'wc_get_order' ) ) return;
    $table = $wpdb->prefix . 'sz_motoboy_pedidos';
    if ( ! $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $table ) ) ) return;

    $ids = $wpdb->get_col( $wpdb->prepare(
        "SELECT wc_order_id FROM {$table} WHERE wc_order_id > 0 ORDER BY COALESCE(updated_at, created_at) DESC LIMIT %d",
        max( 50, min( 3000, $limit ) )
    ) );

    foreach ( (array) $ids as $oid ) {
        $order = wc_get_order( (int) $oid );
        if ( ! $order instanceof \WC_Order ) continue;
        $row = senderzz_operator_motoboy_row( (int) $oid );
        senderzz_operator_cleanup_order_motoboy_duplicates( $order, $row, true );
    }
}

function senderzz_operator_is_motoboy_order( int $order_id ): bool {
    if ( senderzz_operator_motoboy_row( $order_id ) ) return true;
    $order = wc_get_order( $order_id );
    if ( ! $order instanceof \WC_Order ) return false;
    return senderzz_operator_order_is_motoboy_cod( $order );
}

function senderzz_operator_order_is_motoboy_cod( \WC_Order $order ): bool {
    if ( function_exists( 'sz_motoboy_order_is_cod_motoboy' ) ) {
        return sz_motoboy_order_is_cod_motoboy( $order );
    }

    $mode = strtolower( (string) $order->get_meta( '_senderzz_delivery_mode', true ) );
    $flow = strtolower( (string) $order->get_meta( '_senderzz_motoboy_flow_status', true ) );
    if ( $mode === 'motoboy' || $flow !== '' ) return true;

    foreach ( $order->get_items( 'shipping' ) as $shipping_item ) {
        $hay = strtolower( implode( ' ', [
            (string) $shipping_item->get_method_id(),
            (string) $shipping_item->get_method_title(),
            (string) $shipping_item->get_name(),
            wp_json_encode( $shipping_item->get_meta_data() ),
        ] ) );
        if ( strpos( $hay, 'motoboy' ) !== false || strpos( $hay, 'senderzz' ) !== false ) return true;
    }

    global $wpdb;
    $links_table = $wpdb->prefix . 'senderzz_checkout_links';
    $token = trim( (string) ( $order->get_meta( '_senderzz_offer_token', true ) ?: $order->get_meta( '_senderzz_checkout_token', true ) ) );
    if ( $token !== '' ) {
        $tipo = (string) $wpdb->get_var( $wpdb->prepare( "SELECT tipo FROM {$links_table} WHERE token = %s LIMIT 1", $token ) );
        if ( strtolower( $tipo ) === 'motoboy' ) return true;
    }
    $link_id = absint( $order->get_meta( '_senderzz_checkout_link_id', true ) ?: $order->get_meta( '_senderzz_offer_link_id', true ) );
    if ( $link_id > 0 ) {
        $tipo = (string) $wpdb->get_var( $wpdb->prepare( "SELECT tipo FROM {$links_table} WHERE id = %d LIMIT 1", $link_id ) );
        if ( strtolower( $tipo ) === 'motoboy' ) return true;
    }

    foreach ( $order->get_items() as $item ) {
        $offer = strtolower( (string) $item->get_meta( '_senderzz_offer_name', true ) . ' ' . $item->get_name() );
        if ( strpos( $offer, 'motoboy' ) !== false ) return true;
    }

    return strpos( strtolower( (string) $order->get_shipping_method() ), 'motoboy' ) !== false;
}


/**
 * Garante que pedidos COD Motoboy em HPOS/Woo tenham linha operacional para o OL.
 * Sem isso o portal do produtor mostra o pedido, mas o Operador Logístico fica vazio.
 */
function senderzz_operator_ensure_motoboy_row_for_order( \WC_Order $order ) {
    $order_id = (int) $order->get_id();
    $existing = senderzz_operator_motoboy_row( $order_id );
    if ( $existing ) return $existing;

    if ( ! senderzz_operator_order_is_motoboy_cod( $order ) ) return null;

    if ( function_exists( 'sz_motoboy_criar_pedido' ) ) {
        sz_motoboy_criar_pedido( $order_id );
    }

    $row = senderzz_operator_motoboy_row( $order_id );
    if ( ! $row && function_exists( 'senderzz_me_log' ) ) {
        senderzz_me_log( 'operator.motoboy_row_missing_after_backfill', [ 'order_id' => $order_id ] );
    }
    return $row;
}

/**
 * Busca IDs recentes direto em HPOS. É um fallback proposital para instalações
 * onde wp_posts está vazio e wc_get_orders/status customizado não devolve tudo.
 */
function senderzz_operator_recent_hpos_cod_order_ids( int $limit = 500 ): array {
    global $wpdb;
    $table = $wpdb->prefix . 'wc_orders';
    $exists = $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $table ) );
    if ( ! $exists ) return [];

    $limit = max( 1, min( 500, $limit ) );
    $ids = $wpdb->get_col( $wpdb->prepare(
        "SELECT id FROM {$table}
         WHERE type = 'shop_order'
         ORDER BY id DESC
         LIMIT %d",
        $limit
    ) );
    return array_values( array_filter( array_map( 'absint', (array) $ids ) ) );
}

function senderzz_operator_motoboy_status_for_order( \WC_Order $order, $motoboy_row = null ): string {
    // O status do WooCommerce é a fonte principal. A tabela Motoboy é só operacional
    // e não deve concorrer com o pedido original.
    $st = sanitize_key( (string) $order->get_status() );
    if ( function_exists( 'senderzz_woo_status_to_motoboy_status' ) ) {
        $mapped = senderzz_woo_status_to_motoboy_status( $st );
        if ( $mapped !== '' && $mapped !== $st ) return $mapped === 'a_caminho' ? 'em_rota' : $mapped;
    }
    $map = [
        'completo'   => 'entregue',
        'completed'  => 'entregue',
        'entregue'   => 'entregue',
        'frustrado'  => 'frustrado',
        'frustracao' => 'frustrado',
        'cancelled'  => 'cancelado',
        'canceled'   => 'cancelado',
        'cancelado'  => 'cancelado',
        'failed'     => 'cancelado',
        'embalado'   => 'embalado',
        'em-rota'    => 'em_rota',
        'em_rota'    => 'em_rota',
        'emrota'     => 'em_rota',
        'a-caminho'  => 'em_rota',
        'a_caminho'  => 'em_rota',
        'acaminho'   => 'em_rota',
        'agendado'   => 'agendado',
        'aprovado'   => 'agendado',
        'processing' => 'agendado',
        'on-hold'    => 'agendado',
    ];
    if ( isset( $map[ $st ] ) ) return $map[ $st ];

    $flow = sanitize_key( (string) $order->get_meta( '_senderzz_motoboy_flow_status', true ) );
    if ( $flow ) return $flow === 'a_caminho' ? 'em_rota' : $flow;
    if ( $motoboy_row && ! empty( $motoboy_row->status ) ) {
        $row_st = sanitize_key( (string) $motoboy_row->status );
        return $row_st === 'a_caminho' ? 'em_rota' : $row_st;
    }
    return 'agendado';
}

function senderzz_operator_status_view( \WC_Order $order ): array {
    $st = $order->get_status();
    $mb = senderzz_operator_motoboy_row( (int) $order->get_id() );
    if ( $mb || senderzz_operator_order_is_motoboy_cod( $order ) ) {
        $labels = [
            'agendado'  => 'Agendado Motoboy',
            'embalado'  => 'Embalado Motoboy',
            'em_rota'   => 'Em rota Motoboy',
            'entregue'  => 'Entregue Motoboy',
            'frustrado' => 'Frustrado Motoboy',
            'cancelado' => 'Cancelado Motoboy',
        ];
        $key = senderzz_operator_motoboy_status_for_order( $order, $mb );
        return [ 'key' => $key, 'label' => $labels[ $key ] ?? ucfirst( str_replace( '_', ' ', $key ) ) ];
    }
    $map = [
        'pending'    => [ 'pendente', 'Pendente' ],
        'on-hold'    => [ 'pendente', 'Pendente' ],
        'processing' => [ 'pendente', 'Pendente' ],
        'aprovado'   => [ 'aprovado', 'Aprovado' ],
        'separado'   => [ 'separado', 'Separado' ],
        'embalado'   => [ 'embalado', 'Embalado' ],
        'emretirada' => [ 'a_enviar', 'A enviar' ],
        'coletado'   => [ 'coletado', 'Coletado' ],
        'enviado'    => [ 'enviado', 'Enviado' ],
        'completed'  => [ 'concluido', 'Concluído' ],
        'extravio'   => [ 'extravio', 'Extravio' ],
        'cancelled'  => [ 'cancelado', 'Cancelado' ],
        'canceled'   => [ 'cancelado', 'Cancelado' ],
        'refunded'   => [ 'cancelado', 'Reembolsado' ],
        'failed'     => [ 'cancelado', 'Falhou' ],
        'erro'       => [ 'erro', 'Erro' ],
    ];
    if ( isset( $map[ $st ] ) ) return [ 'key' => $map[ $st ][0], 'label' => $map[ $st ][1] ];
    return [ 'key' => sanitize_key( $st ), 'label' => wc_get_order_status_name( 'wc-' . $st ) ?: ucfirst( $st ) ];
}

function senderzz_operator_gross_profit( \WC_Order $order ): float {
    $keys = [ 'senderzz_margin', '_senderzz_margin', 'senderzz_gross_profit', '_senderzz_gross_profit' ];
    foreach ( $keys as $key ) {
        $v = $order->get_meta( $key );
        if ( $v !== '' && $v !== null && is_numeric( $v ) ) return max( 0.0, (float) $v );
    }
    if ( function_exists( 'senderzz_order_shipping_snapshot' ) ) {
        $snap = senderzz_order_shipping_snapshot( $order );
        if ( isset( $snap['margin'] ) && is_numeric( $snap['margin'] ) ) return max( 0.0, (float) $snap['margin'] );
    }
    return 0.0;
}

/**
 * Retorna pedidos para a fila do operador.
 * Inclui pedidos com e sem etiqueta, por classe e sem classe.
 */
function senderzz_operator_get_orders( array $args = [] ): array {
    $defaults = [
        'status'     => array_keys( wc_get_order_statuses() ),
        'limit'      => 50,
        'page'       => 1,
        'search'     => '',
        'carrier'    => '',
        'class'      => '',
        'date_from'  => '',
        'kind'       => '', // '' | motoboy | expedicao
    ];
    $a = wp_parse_args( $args, $defaults );
    if ( empty( $a['status'] ) || $a['status'] === 'all' || $a['status'] === [ 'all' ] ) {
        $a['status'] = array_keys( wc_get_order_statuses() );
    }

    $kind = sanitize_key( (string) ( $a['kind'] ?? '' ) );
    $orders = [];

    /*
     * COD Motoboy é uma fila própria. A tela do OL não pode depender da query
     * de Expedição/Woo, nem de Melhor Envio. A fonte operacional é sempre
     * wp_sz_motoboy_pedidos. Quando o WC_Order não carregar, montamos a linha
     * direto do banco para não deixar a operação invisível.
     */
    if ( $kind === 'motoboy' && false ) {
        global $wpdb;
        $mb_table  = $wpdb->prefix . 'sz_motoboy_pedidos';
        $mb_exists = $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $mb_table ) );
        if ( ! $mb_exists ) return [];

        $limit  = max( 1, min( 500, (int) $a['limit'] ) );
        $offset = ( max( 1, (int) $a['page'] ) - 1 ) * $limit;

        // Backfill HPOS antes de listar: pedido wc-agendado + cod + meta motoboy
        // deve ganhar linha operacional caso ainda não tenha.
        foreach ( senderzz_operator_recent_hpos_cod_order_ids( 500 ) as $hpos_order_id ) {
            $exists_row = $wpdb->get_var( $wpdb->prepare( "SELECT id FROM {$mb_table} WHERE wc_order_id=%d LIMIT 1", $hpos_order_id ) );
            if ( $exists_row ) continue;
            $hpos_order = wc_get_order( $hpos_order_id );
            if ( $hpos_order instanceof \WC_Order && senderzz_operator_order_is_motoboy_cod( $hpos_order ) ) {
                senderzz_operator_ensure_motoboy_row_for_order( $hpos_order );
            }
        }

        $where = "WHERE mp.status IN ('agendado','aprovado','embalado','em_rota','frustrado','entregue','cancelado')";
        $params = [];
        if ( $a['search'] ) {
            $like = '%' . $wpdb->esc_like( sanitize_text_field( $a['search'] ) ) . '%';
            $where .= " AND (mp.wc_order_id LIKE %s OR mp.dest_nome LIKE %s OR mp.dest_telefone LIKE %s OR mp.dest_cep LIKE %s)";
            $params = array_merge( $params, [ $like, $like, $like, $like ] );
        }
        $sql = "SELECT mp.* FROM {$mb_table} mp {$where} ORDER BY COALESCE(mp.updated_at, mp.created_at) DESC LIMIT %d OFFSET %d";
        $params[] = $limit;
        $params[] = $offset;
        $mb_rows = $wpdb->get_results( $wpdb->prepare( $sql, $params ) );

        $direct = [];
        foreach ( (array) $mb_rows as $mp ) {
            $order = wc_get_order( (int) $mp->wc_order_id );
            $status = sanitize_key( (string) ( $mp->status ?: 'agendado' ) );
            $created_ts = ! empty( $mp->created_at ) ? strtotime( (string) $mp->created_at ) : current_time( 'timestamp' );
            $updated_ts = ! empty( $mp->updated_at ) ? strtotime( (string) $mp->updated_at ) : $created_ts;
            $date_fmt = $created_ts ? wp_date( 'd/m/Y H:i', $created_ts ) : '';
            $delivery_raw = '';
            if ( ! empty( $mp->reagendado_para ) ) $delivery_raw = (string) $mp->reagendado_para;
            elseif ( ! empty( $mp->agendamento_data ) ) $delivery_raw = (string) $mp->agendamento_data;
            elseif ( ! empty( $mp->entrega_data ) ) $delivery_raw = (string) $mp->entrega_data;
            if ( ! $delivery_raw && $order instanceof \WC_Order ) {
                $delivery_raw = (string) (
                    $order->get_meta( '_sz_delivery_date', true ) ?:
                    $order->get_meta( '_senderzz_delivery_date', true ) ?:
                    $order->get_meta( '_senderzz_delivery_time', true ) ?:
                    $order->get_meta( '_sz_motoboy_entrega_data', true ) ?:
                    ''
                );
            }
            $delivery_fmt = $delivery_raw;
            if ( $delivery_raw && preg_match( '/^\d{4}-\d{2}-\d{2}/', $delivery_raw ) ) {
                $delivery_fmt = wp_date( 'd/m/Y', strtotime( $delivery_raw ) );
            }
            $name = trim( (string) ( $mp->dest_nome ?? '' ) );
            $phone = trim( (string) ( $mp->dest_telefone ?? '' ) );
            $address_parts = array_filter( [
                trim( (string) ( $mp->dest_endereco ?? '' ) ),
                trim( (string) ( $mp->dest_numero ?? '' ) ),
                trim( (string) ( $mp->dest_complemento ?? '' ) ),
                trim( (string) ( $mp->dest_bairro ?? '' ) ),
                trim( (string) ( $mp->dest_cidade ?? '' ) ),
                trim( (string) ( $mp->dest_uf ?? '' ) ),
                trim( (string) ( $mp->dest_cep ?? '' ) ),
            ] );
            $total_raw = isset( $mp->valor_pedido ) ? (float) $mp->valor_pedido : 0.0;
            $kit = '';
            if ( $order instanceof \WC_Order ) {
                if ( ! $name ) $name = trim( $order->get_shipping_first_name() . ' ' . $order->get_shipping_last_name() ) ?: trim( $order->get_billing_first_name() . ' ' . $order->get_billing_last_name() );
                if ( ! $phone ) $phone = (string) $order->get_billing_phone();
                if ( $total_raw <= 0 ) $total_raw = (float) $order->get_total();
                $kit = senderzz_operator_items_summary( $order );
            }
            $labels = [
                'agendado'  => 'Agendado Motoboy',
                'aprovado'   => 'Agendado Motoboy',
                'embalado'  => 'Embalado Motoboy',
                'em_rota'   => 'Em rota Motoboy',
                'entregue'  => 'Entregue Motoboy',
                'frustrado' => 'Frustrado Motoboy',
                'cancelado' => 'Cancelado Motoboy',
            ];
            if ( $status === 'aprovado' ) $status = 'agendado';
            $direct[] = [
                'id'                 => (int) $mp->wc_order_id,
                'motoboy_pedido_id'  => (int) $mp->id,
                'number'             => (string) $mp->wc_order_id,
                'status'             => $order instanceof \WC_Order ? $order->get_status() : $status,
                'ol_status'          => $status,
                'status_label'        => $labels[ $status ] ?? ucfirst( str_replace( '_', ' ', $status ) ),
                'date'               => $date_fmt,
                'recipient_name'     => $name ?: '—',
                'customer_name'       => $name ?: '—',
                'phone'              => $phone,
                'kit'                => $kit ?: 'Motoboy COD',
                'city'               => (string) ( $mp->dest_cidade ?? '' ),
                'state'              => (string) ( $mp->dest_uf ?? '' ),
                'cep'                => (string) ( $mp->dest_cep ?? '' ),
                'address'            => implode( ' · ', $address_parts ),
                'delivery_date'      => $delivery_fmt ?: '—',
                'delivery_raw'       => $delivery_raw,
                'carrier'            => 'Motoboy COD',
                'shipping_class_id'  => 0,
                'shipping_class'     => 'Motoboy COD',
                'shipping_class_slug'=> 'motoboy-cod',
                'tracking'           => [],
                'label_url'          => '',
                'has_label'          => true,
                'label_missing'      => false,
                'me_order_id'        => '',
                'age_hours'          => 0,
                'sla_status'         => 'ok',
                'total_raw'          => $total_raw,
                'total_fmt'          => senderzz_operator_money( $total_raw ),
                'shipping_total_raw' => 0,
                'shipping_total_fmt' => senderzz_operator_money( 0 ),
                'gross_profit_raw'   => 0,
                'gross_profit_fmt'   => senderzz_operator_money( 0 ),
                'margin_raw'         => 0,
                'margin_fmt'         => senderzz_operator_money( 0 ),
                'is_loss'            => false,
                'is_motoboy'         => true,
                'motoboy_status'     => $status,
                'motoboy_id'         => (int) ( $mp->motoboy_id ?? 0 ),
                'motoboy_name'       => (function() use ( $mp, $wpdb ) {
                    $mid = (int) ( $mp->motoboy_id ?? 0 );
                    if ( ! $mid ) return '';
                    $row = $wpdb->get_row( $wpdb->prepare( "SELECT nome FROM {$wpdb->prefix}sz_motoboys WHERE id=%d LIMIT 1", $mid ) );
                    return $row ? (string) ( $row->nome ?? '' ) : '';
                })(),
            ];
        }
        return $direct;
    } else {
        $query_args = [
            'type'    => 'shop_order',
            'status'  => $a['status'],
            'limit'   => (int) $a['limit'],
            'offset'  => ( max( 1, (int) $a['page'] ) - 1 ) * (int) $a['limit'],
            'orderby' => 'date',
            'order'   => 'DESC',
        ];

        if ( $a['search'] ) {
            $query_args['search'] = '*' . sanitize_text_field( $a['search'] ) . '*';
        }
        if ( $a['date_from'] ) {
            $query_args['date_created'] = '>' . sanitize_text_field( $a['date_from'] );
        }

        $orders = wc_get_orders( $query_args );

        // Visão geral: injeta pedidos Motoboy que estejam fora dos status Woo registrados.
        if ( $kind === '' ) {
            global $wpdb;
            $mb_table = $wpdb->prefix . 'sz_motoboy_pedidos';
            $mb_exists = $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $mb_table ) );
            if ( $mb_exists ) {
                $mb_rows = $wpdb->get_results( "SELECT wc_order_id FROM {$mb_table} WHERE status IN ('agendado','aprovado','embalado','em_rota','frustrado','entregue','cancelado') ORDER BY COALESCE(updated_at, created_at) DESC LIMIT 500" );
                $seen_ids = [];
                foreach ( $orders as $ord ) {
                    if ( $ord instanceof \WC_Order ) $seen_ids[ (int) $ord->get_id() ] = true;
                }
                foreach ( (array) $mb_rows as $mb_row ) {
                    $oid = (int) ( $mb_row->wc_order_id ?? 0 );
                    if ( $oid <= 0 || isset( $seen_ids[ $oid ] ) ) continue;
                    $mb_order = wc_get_order( $oid );
                    if ( $mb_order instanceof \WC_Order ) {
                        $orders[] = $mb_order;
                        $seen_ids[ $oid ] = true;
                    }
                }
            }
        }
    }

    $result = [];
    $now_ts = current_time( 'timestamp' );

    foreach ( $orders as $order ) {
        $carrier = '';
        foreach ( $order->get_items( 'shipping' ) as $s ) {
            $carrier = $s->get_name();
            break;
        }
        $motoboy_row = senderzz_operator_motoboy_row( (int) $order->get_id() );
        $is_motoboy_cod = $motoboy_row || senderzz_operator_order_is_motoboy_cod( $order );
        if ( $kind === 'motoboy' && $is_motoboy_cod && ! $motoboy_row ) {
            $motoboy_row = senderzz_operator_ensure_motoboy_row_for_order( $order );
        }
        if ( $kind === 'motoboy' && ! $is_motoboy_cod ) continue;
        if ( $kind === 'expedicao' && $is_motoboy_cod ) continue;
        if ( $is_motoboy_cod ) {
            $carrier = 'Motoboy COD';
            // Normaliza a fonte única antes de renderizar: Woo meta oficial manda; tabela operacional espelha.
            senderzz_operator_cleanup_order_motoboy_duplicates( $order, $motoboy_row, true );
            $motoboy_row = senderzz_operator_motoboy_row( (int) $order->get_id() );
        }
        if ( $a['carrier'] && stripos( $carrier, $a['carrier'] ) === false ) continue;

        $class = senderzz_operator_order_class_info( $order );
        if ( $a['class'] !== '' && ctype_digit( (string) $a['class'] ) && intval( $a['class'] ) > 0 ) {
            if ( (string) $class['id'] !== (string) intval( $a['class'] ) ) continue;
        }

        $tracking = [];
        if ( function_exists( 'wc_melhor_envio_get_tracking_codes' ) ) {
            $tracking = array_values( array_filter( (array) wc_melhor_envio_get_tracking_codes( $order ) ) );
        }
        if ( empty( $tracking ) ) {
            $raw_tracking = $order->get_meta( '_melhor_envio_tracking_codes' );
            if ( is_array( $raw_tracking ) ) {
                foreach ( $raw_tracking as $t ) {
                    $tracking[] = is_array( $t ) ? ( $t['tracking'] ?? $t['code'] ?? '' ) : (string) $t;
                }
            } elseif ( $raw_tracking ) {
                $tracking[] = (string) $raw_tracking;
            }
            $tracking = array_values( array_filter( $tracking ) );
        }

        $label_path = (string) $order->get_meta( '_melhor_envio_pdf_local_path' );
        $label_url  = senderzz_operator_is_valid_pdf_file( $label_path ) ? (string) $order->get_meta( '_melhor_envio_pdf_local_url' ) : '';
        $has_label  = (bool) $label_url;
        if ( $is_motoboy_cod ) {
            $label_url = '';
            $has_label = true;
        }
        $status_view = senderzz_operator_status_view( $order );
        $ol_status   = $status_view['key'];
        $status_label = $status_view['label'];

        $created_ts = $order->get_date_created() ? $order->get_date_created()->getTimestamp() : $now_ts;
        $age_hours  = senderzz_operator_business_hours_since( $created_ts, $now_ts );
        $sla_status = $age_hours >= 24 ? 'atrasado' : ( $age_hours >= 18 ? 'atenção' : 'ok' );
        $shipping_total = (float) $order->get_shipping_total() + (float) $order->get_shipping_tax();
        $total_raw = (float) $order->get_total();
        $gross_profit = senderzz_operator_gross_profit( $order );
        $operator_margin = round( $gross_profit * 0.5, 2 );
        $is_loss = $order->has_status( 'extravio' );

        $result[] = [
            'id'                 => $order->get_id(),
            'number'             => $order->get_order_number(),
            'status'             => $order->get_status(),
            'ol_status'          => $ol_status,
            'status_label'        => $status_label,
            'date'               => $order->get_date_created() ? $order->get_date_created()->date_i18n( 'd/m/Y H:i' ) : '',
            'recipient_name'     => trim( $order->get_shipping_first_name() . ' ' . $order->get_shipping_last_name() ) ?: trim( $order->get_billing_first_name() . ' ' . $order->get_billing_last_name() ),
            'customer_name'       => trim( $order->get_billing_first_name() . ' ' . $order->get_billing_last_name() ),
            'kit'                 => senderzz_operator_items_summary( $order ),
            'city'               => $order->get_shipping_city() ?: $order->get_billing_city(),
            'state'              => $order->get_shipping_state() ?: $order->get_billing_state(),
            'carrier'            => $carrier,
            'shipping_class_id'  => $class['id'],
            'shipping_class'     => $class['name'],
            'shipping_class_slug'=> $class['slug'],
            'tracking'           => $tracking,
            'label_url'          => $label_url ? esc_url( $label_url ) : '',
            'has_label'          => $has_label,
            'label_missing'      => $is_motoboy_cod ? false : ( ! $has_label || ! $label_url ),
            'me_order_id'        => $order->get_meta( '_melhor_envio_order_id' ) ?: '',
            'age_hours'          => $age_hours,
            'sla_status'         => $sla_status,
            'total_raw'          => $total_raw,
            'total_fmt'          => senderzz_operator_money( $total_raw ),
            'shipping_total_raw' => $shipping_total,
            'shipping_total_fmt' => senderzz_operator_money( $shipping_total ),
            'gross_profit_raw'   => $gross_profit,
            'gross_profit_fmt'   => senderzz_operator_money( $gross_profit ),
            'margin_raw'         => $is_loss ? 0.0 : $operator_margin,
            'margin_fmt'         => senderzz_operator_money( $is_loss ? 0.0 : $operator_margin ),
            'is_loss'            => $is_loss,
            'is_motoboy'         => (bool) $is_motoboy_cod,
            'motoboy_status'     => $is_motoboy_cod ? senderzz_operator_motoboy_status_for_order( $order, $motoboy_row ) : '',
            'motoboy_name'       => $is_motoboy_cod ? senderzz_operator_order_motoboy_name( $order, $motoboy_row ) : '',
            'motoboy_id'         => $is_motoboy_cod ? senderzz_operator_order_motoboy_id( $order, $motoboy_row ) : 0,
            'phone'              => $order->get_billing_phone(),
            'address'            => trim( ($order->get_shipping_address_1()?:$order->get_billing_address_1()) . ', ' . ($order->get_meta('_shipping_number',true)?:$order->get_meta('_billing_number',true)?:'') ),
        ];
    }

    return $result;
}

/**
 * Atualiza status OL e status WooCommerce do pedido.
 */
function senderzz_operator_update_status( int $order_id, string $ol_status ): array {
    $order = wc_get_order( $order_id );
    if ( ! $order ) return [ 'success' => false, 'message' => 'Pedido não encontrado.' ];

    $motoboy_row = senderzz_operator_motoboy_row( $order_id );
    $is_motoboy_cod = $motoboy_row || senderzz_operator_order_is_motoboy_cod( $order );
    if ( $is_motoboy_cod ) {
        // Garante a linha operacional Motoboy quando o pedido nasceu apenas no Woo/meta.
        if ( ! $motoboy_row && function_exists( 'sz_motoboy_criar_pedido' ) ) {
            sz_motoboy_criar_pedido( $order_id );
            $motoboy_row = senderzz_operator_motoboy_row( $order_id );
        }
        if ( ! $motoboy_row ) {
            return [ 'success' => false, 'message' => 'Pedido motoboy não encontrado.' ];
        }

        $current_mb_status = senderzz_operator_motoboy_status_for_order( $order, $motoboy_row );

        if ( $ol_status === 'embalado' ) {
            if ( ! in_array( $current_mb_status, [ 'agendado', 'aprovado' ], true ) ) {
                return [ 'success' => false, 'message' => 'Motoboy COD permite embalar apenas pedidos agendados.' ];
            }
            if ( function_exists( 'sz_motoboy_mudar_status' ) ) {
                sz_motoboy_mudar_status( (int) $motoboy_row->id, 'embalado', [], 'ol', get_current_user_id() );
            }
            if ( ! $order->has_status( 'embalado' ) ) {
                $order->update_status( 'embalado', 'Senderzz COD Motoboy: pedido embalado pelo operador logístico.' );
            }
            $order->update_meta_data( '_senderzz_delivery_mode', 'motoboy' );
            $order->update_meta_data( '_senderzz_motoboy_flow_status', 'embalado' );
            $order->save();
            return [ 'success' => true, 'message' => 'Pedido Motoboy embalado.' ];
        }

        if ( $ol_status === 'em_rota' ) {
            return [ 'success' => false, 'message' => 'Em rota só pode ser iniciado pelo motoboy lendo o QR Code da etiqueta.' ];
        }

        if ( $ol_status === 'entregue' ) {
            if ( ! in_array( $current_mb_status, [ 'em_rota', 'a_caminho' ], true ) ) {
                return [ 'success' => false, 'message' => 'Só é possível marcar como entregue pedido em rota.' ];
            }
            if ( function_exists( 'sz_mbc_validate_transition' ) ) {
                $custody_check = sz_mbc_validate_transition( (int) $motoboy_row->id, 'entregue', [], 'ol', get_current_user_id() );
                if ( is_wp_error( $custody_check ) ) return [ 'success' => false, 'message' => $custody_check->get_error_message() ];
            }
            if ( function_exists( 'sz_motoboy_mudar_status' ) ) {
                sz_motoboy_mudar_status( (int) $motoboy_row->id, 'entregue', [], 'ol', get_current_user_id() );
            } else {
                global $wpdb;
                $wpdb->update( $wpdb->prefix . 'sz_motoboy_pedidos', [ 'status' => 'entregue', 'ts_entregue' => current_time( 'mysql' ), 'updated_at' => current_time( 'mysql' ) ], [ 'id' => (int) $motoboy_row->id ] );
            }
            $order->update_meta_data( '_senderzz_delivery_mode', 'motoboy' );
            $order->update_meta_data( '_senderzz_motoboy_flow_status', 'entregue' );
            $order->update_meta_data( '_senderzz_motoboy_status', 'entregue' );
            if ( ! $order->has_status( 'completo' ) && ! $order->has_status( 'completed' ) ) {
                $order->update_status( 'completo', 'Senderzz COD Motoboy: pedido marcado como entregue pelo operador logístico.' );
            } else {
                $order->save();
            }
            return [ 'success' => true, 'message' => 'Pedido Motoboy entregue.' ];
        }

        if ( $ol_status === 'frustrado' ) {
            if ( ! in_array( $current_mb_status, [ 'em_rota', 'a_caminho' ], true ) ) {
                return [ 'success' => false, 'message' => 'Só é possível frustrar pedido em rota.' ];
            }
            if ( function_exists( 'sz_mbc_validate_transition' ) ) {
                $custody_check = sz_mbc_validate_transition( (int) $motoboy_row->id, 'frustrado', [], 'ol', get_current_user_id() );
                if ( is_wp_error( $custody_check ) ) return [ 'success' => false, 'message' => $custody_check->get_error_message() ];
            }
            if ( function_exists( 'sz_motoboy_mudar_status' ) ) {
                sz_motoboy_mudar_status( (int) $motoboy_row->id, 'frustrado', [ 'frustrado_motivo' => 'Marcado pelo operador logístico' ], 'ol', get_current_user_id() );
            } else {
                global $wpdb;
                $wpdb->update( $wpdb->prefix . 'sz_motoboy_pedidos', [ 'status' => 'frustrado', 'ts_frustrado' => current_time( 'mysql' ), 'updated_at' => current_time( 'mysql' ) ], [ 'id' => (int) $motoboy_row->id ] );
            }
            $order->update_meta_data( '_senderzz_delivery_mode', 'motoboy' );
            $order->update_meta_data( '_senderzz_motoboy_flow_status', 'frustrado' );
            $order->update_meta_data( '_senderzz_motoboy_status', 'frustrado' );
            if ( ! $order->has_status( 'frustrado' ) ) {
                $order->update_status( 'frustrado', 'Senderzz COD Motoboy: pedido marcado como frustrado pelo operador logístico.' );
            } else {
                $order->save();
            }
            return [ 'success' => true, 'message' => 'Pedido Motoboy frustrado.' ];
        }

        return [ 'success' => false, 'message' => 'Ação Motoboy não permitida pelo OL.' ];
    }

    // Expedição/OL comum: separado → embalado apenas. Envio externo segue automações próprias.
    $allowed = [
        'separado' => 'embalado',
    ];

    // IDOR: verificar se o pedido pertence à classe de entrega do operador logístico.
    // Sem essa checagem, um operador de classe A poderia alterar pedidos de classe B.
    $token = sanitize_text_field( $_COOKIE['senderzz_portal_session'] ?? ( $_SERVER['HTTP_X_SENDERZZ_TOKEN'] ?? '' ) );
    if ( $token ) {
        global $wpdb;
        $operator_user = $wpdb->get_row( $wpdb->prepare(
            "SELECT u.shipping_class_id FROM {$wpdb->prefix}senderzz_portal_sessions s
             JOIN {$wpdb->prefix}senderzz_portal_users u ON u.id = s.user_id
             WHERE s.token = %s AND s.expires_at > NOW() AND u.role = 'operator' AND u.status = 'active'
             LIMIT 1",
            $token
        ) );
        if ( $operator_user && (int) $operator_user->shipping_class_id > 0 ) {
            $order_class = senderzz_operator_order_class_info( $order );
            if ( (int) ( $order_class['id'] ?? 0 ) !== (int) $operator_user->shipping_class_id ) {
                return [ 'success' => false, 'message' => 'Pedido não pertence à sua classe logística.' ];
            }
        }
    }

    $current = $order->get_status();
    $target  = $allowed[ $current ] ?? null;

    if ( ! $target || $ol_status !== $target ) {
        return [ 'success' => false, 'message' => "Transição de '{$current}' para '{$ol_status}' não permitida." ];
    }

    $labels = [
        'embalado' => 'Senderzz: pedido embalado pelo operador logístico.',
        'enviado'  => 'Senderzz: pedido marcado como enviado pelo operador logístico.',
    ];

    $order->update_status( $target, $labels[ $target ] ?? 'Status atualizado pelo operador.' );

    if ( function_exists( 'senderzz_me_log' ) ) {
        senderzz_me_log( 'operator.status_updated', [ 'order_id' => $order_id, 'from' => $current, 'to' => $target ] );
    }

    return [ 'success' => true, 'message' => "Pedido #{$order_id} movido para '{$target}'." ];
}

/* ── Histórico mínimo para relatório do operador ─────────────────────── */
function senderzz_operator_is_sent_status( string $status ): bool {
    $status = sanitize_key( str_replace( 'wc-', '', $status ) );
    return in_array( $status, [ 'enviado', 'sent' ], true );
}

function senderzz_operator_is_loss_status( string $status ): bool {
    $status = sanitize_key( str_replace( 'wc-', '', $status ) );
    return in_array( $status, [ 'extravio' ], true );
}

function senderzz_operator_track_status_timestamps( int $order_id, string $old_status, string $new_status, $order ): void {
    if ( ! $order instanceof \WC_Order ) {
        $order = wc_get_order( $order_id );
    }
    if ( ! $order ) return;
    $now = ( function_exists( 'sz_motoboy_now_mysql' ) ? sz_motoboy_now_mysql() : current_time( 'mysql' ) );
    if ( senderzz_operator_is_sent_status( $new_status ) && ! $order->get_meta( '_senderzz_operator_sent_at' ) ) {
        $order->update_meta_data( '_senderzz_operator_sent_at', $now );
        $order->save();
    }
    if ( senderzz_operator_is_loss_status( $new_status ) && ! $order->get_meta( '_senderzz_operator_extravio_at' ) ) {
        $order->update_meta_data( '_senderzz_operator_extravio_at', $now );
        $order->save();
    }
}
add_action( 'woocommerce_order_status_changed', 'senderzz_operator_track_status_timestamps', 10, 4 );

function senderzz_operator_get_sent_at( \WC_Order $order ): string {
    $sent_at = (string) $order->get_meta( '_senderzz_operator_sent_at' );
    if ( $sent_at ) return $sent_at;
    if ( senderzz_operator_is_sent_status( $order->get_status() ) ) {
        $dt = $order->get_date_modified() ?: $order->get_date_created();
        if ( $dt ) return $dt->date_i18n( 'Y-m-d H:i:s' );
    }
    return '';
}

function senderzz_operator_report( string $start, string $end ): array {
    $start = preg_match( '/^\d{4}-\d{2}-\d{2}$/', $start ) ? $start : wp_date( 'Y-m-d', strtotime( '-30 days', current_time( 'timestamp' ) ) );
    $end   = preg_match( '/^\d{4}-\d{2}-\d{2}$/', $end ) ? $end : wp_date( 'Y-m-d', current_time( 'timestamp' ) );
    $start_ts = strtotime( $start . ' 00:00:00' );
    $end_ts   = strtotime( $end . ' 23:59:59' );

    // Filtra por data diretamente na query para evitar carregar 1000 pedidos e descartar no PHP.
    $orders = wc_get_orders( [
        'type'        => 'shop_order',
        'status'      => array_keys( wc_get_order_statuses() ),
        'limit'       => 500,
        'orderby'     => 'date',
        'order'       => 'DESC',
        'date_query'  => [ [ 'after' => $start . ' 00:00:00', 'before' => $end . ' 23:59:59', 'inclusive' => true ] ],
    ] );

    $valid = [];
    $loss_count = 0;
    $total_sent = 0;
    $margin = 0.0;

    foreach ( $orders as $order ) {
        if ( ! $order instanceof \WC_Order ) continue;
        $sent_at = senderzz_operator_get_sent_at( $order );
        if ( ! $sent_at ) continue;
        $ts = strtotime( $sent_at );
        if ( ! $ts || $ts < $start_ts || $ts > $end_ts ) continue;

        $total_sent++;
        $is_loss = senderzz_operator_is_loss_status( $order->get_status() ) || (bool) $order->get_meta( '_senderzz_operator_extravio_at' );
        if ( $is_loss ) {
            $loss_count++;
            continue;
        }

        $class = senderzz_operator_order_class_info( $order );
        $carrier = '';
        foreach ( $order->get_items( 'shipping' ) as $s ) { $carrier = $s->get_name(); break; }
        $order_margin = round( senderzz_operator_gross_profit( $order ) * 0.5, 2 );
        $margin += $order_margin;
        $valid[] = [
            'id'             => $order->get_id(),
            'number'         => $order->get_order_number(),
            'customer'       => trim( $order->get_billing_first_name() . ' ' . $order->get_billing_last_name() ) ?: trim( $order->get_shipping_first_name() . ' ' . $order->get_shipping_last_name() ),
            'kit'            => senderzz_operator_items_summary( $order ),
            'shipping_class' => $class['name'],
            'carrier'        => $carrier,
            'sent_at'        => wp_date( 'd/m/Y H:i', $ts ),
            'margin_raw'     => $order_margin,
            'margin_fmt'     => senderzz_operator_money( $order_margin ),
        ];
    }

    $success_rate = $total_sent > 0 ? round( ( count( $valid ) / $total_sent ) * 100, 1 ) : 0;
    return [
        'success'          => true,
        'start'            => $start,
        'end'              => $end,
        'valid_count'      => count( $valid ),
        'loss_count'       => $loss_count,
        'total_sent'       => $total_sent,
        'margin_raw'       => $margin,
        'margin_fmt'       => senderzz_operator_money( $margin ),
        'success_rate'     => $success_rate,
        'success_rate_fmt' => number_format_i18n( $success_rate, 1 ) . '%',
        'orders'           => $valid,
    ];
}

/**
 * Fonte dedicada para a fila COD Motoboy do OL.
 * Lê diretamente wp_sz_motoboy_pedidos e não passa pela listagem de Expedição.
 */
function senderzz_operator_get_motoboy_orders_direct( array $args = [] ): array {
    global $wpdb;

    if ( function_exists( 'sz_motoboy_backfill_recent_orders' ) ) {
        sz_motoboy_backfill_recent_orders( 1000 );
    }

    $a = wp_parse_args( $args, [
        'limit'  => 500,
        'page'   => 1,
        'search' => '',
    ] );

    $table = $wpdb->prefix . 'sz_motoboy_pedidos';
    if ( ! $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $table ) ) ) {
        return [];
    }

    $limit  = max( 1, min( 500, (int) $a['limit'] ) );
    $offset = ( max( 1, (int) $a['page'] ) - 1 ) * $limit;

    $where  = "WHERE status IN ('agendado','aprovado','embalado','em_rota')";
    $params = [];

    $search = trim( (string) ( $a['search'] ?? '' ) );
    if ( $search !== '' ) {
        $like = '%' . $wpdb->esc_like( $search ) . '%';
        $where .= " AND (CAST(wc_order_id AS CHAR) LIKE %s OR dest_nome LIKE %s OR dest_telefone LIKE %s OR dest_cep LIKE %s)";
        $params = array_merge( $params, [ $like, $like, $like, $like ] );
    }

    $sql = "SELECT * FROM {$table} {$where} ORDER BY id DESC LIMIT %d OFFSET %d";
    $params[] = $limit;
    $params[] = $offset;

    $rows = $wpdb->get_results( $wpdb->prepare( $sql, $params ) );
    $out  = [];

    foreach ( (array) $rows as $mp ) {
        $order_id = (int) ( $mp->wc_order_id ?? 0 );
        $order    = $order_id ? wc_get_order( $order_id ) : false;

        // Se o pedido Woo foi excluído definitivamente, não pode continuar
        // aparecendo na operação Motoboy. Mantém o registro histórico no banco,
        // mas tira da fila operacional imediatamente.
        if ( $order_id > 0 && ! $order ) {
            $wpdb->update(
                $table,
                [ 'status' => 'cancelado', 'updated_at' => ( function_exists( 'sz_motoboy_now_mysql' ) ? sz_motoboy_now_mysql() : current_time( 'mysql' ) ) ],
                [ 'id' => (int) $mp->id ],
                [ '%s', '%s' ],
                [ '%d' ]
            );
            continue;
        }

        $status   = sanitize_key( (string) ( $mp->status ?: 'agendado' ) );
        if ( $status === 'aprovado' ) $status = 'agendado';

        $name  = trim( (string) ( $mp->dest_nome ?? '' ) );
        $phone = trim( (string) ( $mp->dest_telefone ?? '' ) );
        $total = isset( $mp->valor_pedido ) ? (float) $mp->valor_pedido : 0.0;
        $kit   = 'Motoboy COD';

        if ( $order instanceof \WC_Order ) {
            if ( $name === '' ) {
                $name = trim( $order->get_shipping_first_name() . ' ' . $order->get_shipping_last_name() );
                if ( $name === '' ) $name = trim( $order->get_billing_first_name() . ' ' . $order->get_billing_last_name() );
            }
            if ( $phone === '' ) $phone = (string) $order->get_billing_phone();
            if ( $total <= 0 ) $total = (float) $order->get_total();
            $items = senderzz_operator_items_summary( $order );
            if ( $items !== '' ) $kit = $items;
        }

        $delivery = '';
        foreach ( [ 'reagendado_para', 'agendamento_data', 'entrega_data', 'delivery_date' ] as $field ) {
            if ( isset( $mp->{$field} ) && $mp->{$field} !== '' ) { $delivery = (string) $mp->{$field}; break; }
        }
        if ( $delivery === '' && $order instanceof \WC_Order ) {
            $delivery = (string) ( 
                $order->get_meta( '_sz_delivery_date', true ) ?:
                $order->get_meta( '_senderzz_delivery_date', true ) ?:
                $order->get_meta( '_senderzz_delivery_time', true ) ?:
                $order->get_meta( '_sz_motoboy_entrega_data', true ) ?:
                ''
            );
        }
        if ( $delivery !== '' && preg_match( '/^(\d{4}-\d{2}-\d{2})/', $delivery, $dm ) ) {
            // Fixa timezone: usa Y-m-d T12:00:00 para evitar perda de um dia em UTC-3
            $delivery = wp_date( 'd/m/Y', strtotime( $dm[1] . 'T12:00:00' ) );
        }

        $address = implode( ' · ', array_filter( array_map( 'trim', [
            (string) ( $mp->dest_endereco ?? '' ),
            (string) ( $mp->dest_numero ?? '' ),
            (string) ( $mp->dest_complemento ?? '' ),
            (string) ( $mp->dest_bairro ?? '' ),
            (string) ( $mp->dest_cidade ?? '' ),
            (string) ( $mp->dest_uf ?? '' ),
            (string) ( $mp->dest_cep ?? '' ),
        ] ) ) );

        $created_ts = ! empty( $mp->created_at ) ? strtotime( (string) $mp->created_at ) : 0;
        if ( ! $created_ts && $order instanceof \WC_Order && $order->get_date_created() ) {
            $created_ts = $order->get_date_created()->getTimestamp();
        }

        $labels = [
            'agendado' => 'Agendado',
            'embalado' => 'Embalado',
            'em_rota'  => 'Em rota',
        ];

        // Nome do motoboy atribuído
        $motoboy_id   = (int) ( $mp->motoboy_id ?? 0 );
        $motoboy_name = '';
        if ( $motoboy_id ) {
            $mb_row = $wpdb->get_row( $wpdb->prepare(
                "SELECT nome FROM {$wpdb->prefix}sz_motoboys WHERE id = %d LIMIT 1",
                $motoboy_id
            ) );
            $motoboy_name = $mb_row ? (string) ( $mb_row->nome ?? '' ) : '';
        }

        $out[] = [
            'id'                => $order_id ?: (int) $mp->id,
            'motoboy_pedido_id' => (int) $mp->id,
            'number'            => $order_id ? (string) $order_id : (string) $mp->id,
            'status'            => $status,
            'ol_status'         => $status,
            'status_label'      => $labels[ $status ] ?? ucfirst( str_replace( '_', ' ', $status ) ),
            'date'              => $created_ts ? wp_date( 'd/m/Y H:i', $created_ts ) : '',
            'recipient_name'    => $name ?: '—',
            'customer_name'     => $name ?: '—',
            'phone'             => $phone,
            'kit'               => $kit,
            'city'              => (string) ( $mp->dest_cidade ?? '' ),
            'state'             => (string) ( $mp->dest_uf ?? '' ),
            'cep'               => (string) ( $mp->dest_cep ?? '' ),
            'address'           => $address,
            'delivery_date'     => $delivery ?: '—',
            'delivery_raw'      => ( isset( $mp->reagendado_para ) && $mp->reagendado_para ? (string) $mp->reagendado_para : ( $order instanceof \WC_Order ? (string) ( $order->get_meta( '_sz_delivery_date', true ) ?: $order->get_meta( '_senderzz_delivery_date', true ) ?: $order->get_meta( '_sz_motoboy_entrega_data', true ) ) : '' ) ),
            'carrier'           => 'Motoboy COD',
            'shipping_class_id' => 0,
            'shipping_class'    => 'Motoboy COD',
            'tracking'          => [],
            'label_url'         => '',
            'has_label'         => true,
            'label_missing'     => false,
            'age_hours'         => 0,
            'sla_status'        => 'ok',
            'total_raw'         => $total,
            'total_fmt'         => senderzz_operator_money( $total ),
            'is_loss'           => false,
            'is_motoboy'        => true,
            'motoboy_status'    => $status,
            'motoboy_id'        => $motoboy_id,
            'motoboy_name'      => $motoboy_name,
        ];
    }

    // v320 fallback final: se a tabela operacional ainda estiver vazia por qualquer motivo,
    // mostra diretamente os pedidos Woo Motoboy recentes no OL e tenta recriar a linha.
    // Isso evita tela zerada em operação mesmo antes de corrigir banco/colunas antigas.
    if ( empty( $out ) && function_exists( 'wc_get_order' ) ) {
        foreach ( senderzz_operator_recent_hpos_cod_order_ids( 500 ) as $oid ) {
            $order = wc_get_order( (int) $oid );
            if ( ! $order instanceof \WC_Order ) continue;
            if ( ! senderzz_operator_order_is_motoboy_cod( $order ) ) continue;

            $pid = senderzz_operator_ensure_motoboy_row_for_order( $order );
            $status = sanitize_key( (string) ( $order->get_meta( '_senderzz_motoboy_flow_status', true ) ?: 'agendado' ) );
            if ( in_array( $status, [ 'entregue', 'frustrado', 'cancelado' ], true ) ) continue;

            $name = trim( $order->get_shipping_first_name() . ' ' . $order->get_shipping_last_name() );
            if ( $name === '' ) $name = trim( $order->get_billing_first_name() . ' ' . $order->get_billing_last_name() );
            $address = implode( ' · ', array_filter( array_map( 'trim', [
                $order->get_shipping_address_1() ?: $order->get_billing_address_1(),
                $order->get_shipping_address_2() ?: $order->get_billing_address_2(),
                $order->get_shipping_city() ?: $order->get_billing_city(),
                $order->get_shipping_state() ?: $order->get_billing_state(),
                $order->get_shipping_postcode() ?: $order->get_billing_postcode(),
            ] ) ) );
            $delivery = (string) ( $order->get_meta( '_sz_delivery_date', true ) ?: $order->get_meta( '_senderzz_delivery_date', true ) ?: $order->get_meta( '_sz_motoboy_entrega_data', true ) ?: '' );
            if ( $delivery && preg_match( '/^(\d{4}-\d{2}-\d{2})/', $delivery, $dm ) ) $delivery = wp_date( 'd/m/Y', strtotime( $dm[1] . 'T12:00:00' ) );
            if ( $delivery === '' ) $delivery = '—';

            $out[] = [
                'id'                => (int) $oid,
                'motoboy_pedido_id' => is_object( $pid ) ? (int) ( $pid->id ?? 0 ) : (int) $pid,
                'number'            => (string) $oid,
                'status'            => $status,
                'ol_status'         => $status,
                'status_label'      => $status === 'em_rota' ? 'Em rota' : ucfirst( str_replace( '_', ' ', $status ) ),
                'date'              => $order->get_date_created() ? $order->get_date_created()->date_i18n( 'd/m/Y H:i' ) : '',
                'recipient_name'    => $name ?: '—',
                'customer_name'     => $name ?: '—',
                'phone'             => (string) $order->get_billing_phone(),
                'kit'               => senderzz_operator_items_summary( $order ) ?: 'Motoboy COD',
                'city'              => (string) ( $order->get_shipping_city() ?: $order->get_billing_city() ),
                'state'             => (string) ( $order->get_shipping_state() ?: $order->get_billing_state() ),
                'cep'               => (string) ( $order->get_shipping_postcode() ?: $order->get_billing_postcode() ),
                'address'           => $address,
                'delivery_date'     => $delivery,
                'delivery_raw'      => $delivery,
                'carrier'           => 'Motoboy COD',
                'shipping_class_id' => 0,
                'shipping_class'    => 'Motoboy COD',
                'tracking'          => [],
                'label_url'         => '',
                'has_label'         => true,
                'label_missing'     => false,
                'age_hours'         => 0,
                'sla_status'        => 'ok',
                'total_raw'         => (float) $order->get_total(),
                'total_fmt'         => senderzz_operator_money( (float) $order->get_total() ),
                'is_loss'           => false,
                'is_motoboy'        => true,
                'motoboy_status'    => $status,
                'motoboy_id'        => 0,
                'motoboy_name'      => '',
            ];
        }
    }

    return $out;
}

function senderzz_rest_operator_motoboy_orders( WP_REST_Request $request ): WP_REST_Response {
    return new WP_REST_Response( [
        'success' => true,
        'orders'  => senderzz_operator_get_motoboy_orders_direct( [
            'limit'  => min( 500, absint( $request->get_param( 'limit' ) ?: 500 ) ),
            'page'   => max( 1, absint( $request->get_param( 'page' ) ?: 1 ) ),
            'search' => sanitize_text_field( $request->get_param( 'search' ) ?: '' ),
        ] ),
    ], 200 );
}

/* ── REST: endpoints do operador ─────────────────────────────────────── */

add_action( 'rest_api_init', function () {
    $ns = 'senderzz/v1';



    // GET /senderzz/v1/operator/motoboy-orders — fila COD Motoboy isolada
    register_rest_route( $ns, '/operator/motoboy-orders', [
        'methods'             => 'GET',
        'callback'            => 'senderzz_rest_operator_motoboy_orders',
        'permission_callback' => 'senderzz_rest_operator_permission',
    ] );

    // GET /senderzz/v1/operator/orders
    register_rest_route( $ns, '/operator/orders', [
        'methods'             => 'GET',
        'callback'            => 'senderzz_rest_operator_orders',
        'permission_callback' => 'senderzz_rest_operator_permission',
    ] );

    // POST /senderzz/v1/operator/orders/{id}/status
    register_rest_route( $ns, '/operator/orders/(?P<id>\d+)/status', [
        'methods'             => 'POST',
        'callback'            => 'senderzz_rest_operator_status',
        'permission_callback' => 'senderzz_rest_operator_permission',
    ] );

    // POST /senderzz/v1/operator/orders/{id}/baixa — baixa manual por admin
    register_rest_route( $ns, '/operator/orders/(?P<id>\d+)/baixa', [
        'methods'             => 'POST',
        'callback'            => 'senderzz_rest_operator_baixa',
        'permission_callback' => 'senderzz_rest_operator_permission',
    ] );

    // GET /senderzz/v1/operator/motoboys — lista motoboys ativos para atribuição manual
    register_rest_route( $ns, '/operator/motoboys', [
        'methods'             => 'GET',
        'callback'            => 'senderzz_rest_operator_list_motoboys',
        'permission_callback' => 'senderzz_rest_operator_permission',
    ] );

    // POST /senderzz/v1/operator/orders/{id}/assign-motoboy
    register_rest_route( $ns, '/operator/orders/(?P<id>\d+)/assign-motoboy', [
        'methods'             => 'POST',
        'callback'            => 'senderzz_rest_operator_assign_motoboy',
        'permission_callback' => 'senderzz_rest_operator_permission',
    ] );

    // POST /senderzz/v1/operator/orders/{id}/reschedule — altera a data de entrega motoboy no Admin Senderzz
    register_rest_route( $ns, '/operator/orders/(?P<id>\d+)/reschedule', [
        'methods'             => 'POST',
        'callback'            => 'senderzz_rest_operator_reschedule_order',
        'permission_callback' => 'senderzz_rest_operator_permission',
    ] );

    // GET /senderzz/v1/operator/stats
    register_rest_route( $ns, '/operator/stats', [
        'methods'             => 'GET',
        'callback'            => 'senderzz_rest_operator_stats',
        'permission_callback' => 'senderzz_rest_operator_permission',
    ] );

    // GET /senderzz/v1/operator/report
    register_rest_route( $ns, '/operator/report', [
        'methods'             => 'GET',
        'callback'            => 'senderzz_rest_operator_report',
        'permission_callback' => 'senderzz_rest_operator_permission',
    ] );

    register_rest_route( $ns, '/operator/labels/download-label', [
        'methods'             => 'POST',
        'callback'            => function ( WP_REST_Request $request ) {
            $order_ids = array_values( array_filter( array_map( 'absint', (array) $request->get_param( 'order_ids' ) ) ) );
            if ( empty( $order_ids ) ) {
                return new WP_REST_Response( [ 'success' => false, 'message' => 'Selecione pelo menos um pedido.' ], 400 );
            }

            $settings = get_option( 'woocommerce_wc-melhor-envio_settings', [] );
            $token    = (string) ( $settings['client_secret'] ?? '' );
            $sandbox  = ( ( $settings['sandbox'] ?? 'no' ) === 'yes' );
            $host     = $sandbox ? 'sandbox.melhorenvio.com.br' : 'www.melhorenvio.com.br';

            $upload_dir = wp_upload_dir();
            $subdir     = '/senderzz-labels/etiquetas/' . gmdate( 'Y/m' );
            $dir        = trailingslashit( $upload_dir['basedir'] ) . ltrim( $subdir, '/' );
            if ( ! file_exists( $dir ) ) {
                wp_mkdir_p( $dir );
            }

            // As etiquetas são abertas pelo operador via URL pública do uploads.
            // O .htaccess antigo com "Deny from all" causava 403 Forbidden.
            // Mantém listagem bloqueada, mas libera leitura dos PDFs gerados.
            $root_labels = trailingslashit( $upload_dir['basedir'] ) . 'senderzz-labels';
            if ( ! file_exists( $root_labels ) ) {
                wp_mkdir_p( $root_labels );
            }
            file_put_contents( $root_labels . '/.htaccess', "Options -Indexes\n<FilesMatch \"\\.pdf$\">\nRequire all granted\n</FilesMatch>\n" );
            if ( ! file_exists( $root_labels . '/index.php' ) ) {
                file_put_contents( $root_labels . '/index.php', '<?php // Silence is golden.' );
            }

            $label_paths = [];
            $errors      = [];

            foreach ( $order_ids as $order_id ) {
                $order = wc_get_order( $order_id );
                if ( ! $order ) { $errors[] = '#' . $order_id . ': não encontrado'; continue; }

                $item_id = (string) $order->get_meta( '_melhor_envio_item_id' );
                if ( ! $item_id ) { $errors[] = '#' . $order_id . ': sem item_id ME'; continue; }

                // Verifica cache da etiqueta pura
                $cached_path = (string) $order->get_meta( '_melhor_envio_label_only_path' );
                $cached_url  = (string) $order->get_meta( '_melhor_envio_label_only_url' );
                if ( $cached_url && file_exists( $cached_path ) && filesize( $cached_path ) > 100 ) {
                    $fh   = fopen( $cached_path, 'rb' );
                    $head = $fh ? fread( $fh, 4 ) : '';
                    if ( $fh ) fclose( $fh );
                    if ( $head === '%PDF' ) { $label_paths[] = [ 'path' => $cached_path, 'url' => $cached_url ]; continue; }
                }

                // Baixa etiqueta direto da API
                $url      = "https://{$host}/api/v2/me/imprimir/pdf/" . rawurlencode( $item_id );
                $response = wp_safe_remote_get( $url, [
                    'timeout' => 90,
                    'headers' => [
                        'Authorization' => 'Bearer ' . $token,
                        'Accept'        => 'application/json, application/pdf',
                        'Content-Type'  => 'application/json',
                        'User-Agent'    => 'Senderzz Logistics (suporte@app.senderzz.com.br)',
                    ],
                ] );

                if ( is_wp_error( $response ) ) { $errors[] = '#' . $order_id . ': ' . $response->get_error_message(); continue; }

                $code = (int) wp_remote_retrieve_response_code( $response );
                $body = (string) wp_remote_retrieve_body( $response );

                if ( ! in_array( $code, [ 200, 201 ], true ) ) { $errors[] = '#' . $order_id . ': HTTP ' . $code; continue; }

                $pdf_body = '';
                if ( strncmp( $body, '%PDF', 4 ) === 0 ) {
                    $pdf_body = $body;
                } else {
                    // URL assinada do S3
                    $data    = json_decode( $body, true );
                    $pdf_url = '';
                    if ( is_array( $data ) ) {
                        foreach ( $data as $value ) {
                            if ( is_string( $value ) && preg_match( '#^https?://#i', $value ) ) { $pdf_url = $value; break; }
                        }
                    }
                    if ( $pdf_url ) {
                        $dl = wp_safe_remote_get( $pdf_url, [ 'timeout' => 90, 'headers' => [ 'Accept' => 'application/pdf', 'User-Agent' => 'Senderzz Logistics (suporte@app.senderzz.com.br)' ] ] );
                        if ( ! is_wp_error( $dl ) && strncmp( (string) wp_remote_retrieve_body( $dl ), '%PDF', 4 ) === 0 ) {
                            $pdf_body = (string) wp_remote_retrieve_body( $dl );
                        }
                    }
                }

                if ( ! $pdf_body ) { $errors[] = '#' . $order_id . ': etiqueta não disponível'; continue; }

                $fname = sanitize_file_name( sprintf( 'etiqueta-%d-%s.pdf', $order_id, gmdate( 'Ymd-His' ) ) );
                $fpath = trailingslashit( $dir ) . $fname;
                $furl  = trailingslashit( $upload_dir['baseurl'] ) . trim( $subdir, '/' ) . '/' . $fname;
                file_put_contents( $fpath, $pdf_body );

                $order->update_meta_data( '_melhor_envio_label_only_path', $fpath );
                $order->update_meta_data( '_melhor_envio_label_only_url', $furl );
                $order->save();

                $label_paths[] = [ 'path' => $fpath, 'url' => $furl ];
            }

            if ( empty( $label_paths ) ) {
                return new WP_REST_Response( [ 'success' => false, 'message' => 'Nenhuma etiqueta disponível. ' . implode( ' | ', $errors ) ], 422 );
            }

            // Um pedido — retorna direto
            if ( count( $label_paths ) === 1 ) {
                return new WP_REST_Response( [ 'success' => true, 'url' => $label_paths[0]['url'] ], 200 );
            }

            // Vários pedidos — une com Batch_Composer
            try {
                $composer = new \WC_MelhorEnvio\Pdf\Batch_Composer();
                $result   = $composer->compose( $order_ids, [ 'include_packing_slip' => false ] );
                return new WP_REST_Response( [ 'success' => true, 'url' => $result['url'] ?? '' ], 200 );
            } catch ( Throwable $e ) {
                return new WP_REST_Response( [ 'success' => true, 'url' => $label_paths[0]['url'] ], 200 );
            }
        },
        'permission_callback' => 'senderzz_rest_operator_permission',
    ] );

    register_rest_route( $ns, '/operator/labels/download-dace', [
        'methods'             => 'POST',
        'callback'            => function ( WP_REST_Request $request ) {
            $order_ids = array_values( array_filter( array_map( 'absint', (array) $request->get_param( 'order_ids' ) ) ) );
            if ( empty( $order_ids ) ) {
                return new WP_REST_Response( [ 'success' => false, 'message' => 'Selecione pelo menos um pedido.' ], 400 );
            }

            $settings = get_option( 'woocommerce_wc-melhor-envio_settings', [] );
            $token    = (string) ( $settings['client_secret'] ?? '' );
            $sandbox  = ( ( $settings['sandbox'] ?? 'no' ) === 'yes' );
            $host     = $sandbox ? 'sandbox.melhorenvio.com.br' : 'www.melhorenvio.com.br';

            $upload_dir = wp_upload_dir();
            $subdir     = '/senderzz-labels/dace/' . gmdate( 'Y/m' );
            $dir        = trailingslashit( $upload_dir['basedir'] ) . ltrim( $subdir, '/' );
            if ( ! file_exists( $dir ) ) {
                wp_mkdir_p( $dir );
                // .htaccess já criado pelo bloco de etiquetas acima se existir o diretório raiz.
            }

            $dace_paths = [];
            $errors     = [];

            foreach ( $order_ids as $order_id ) {
                $order = wc_get_order( $order_id );
                if ( ! $order ) { $errors[] = '#' . $order_id . ': não encontrado'; continue; }

                // Verifica cache da DACE
                $cached_path = (string) $order->get_meta( '_melhor_envio_dace_pdf_path' );
                $cached_url  = (string) $order->get_meta( '_melhor_envio_dace_pdf_url' );
                if ( $cached_url && file_exists( $cached_path ) && filesize( $cached_path ) > 100 ) {
                    $fh   = fopen( $cached_path, 'rb' );
                    $head = $fh ? fread( $fh, 4 ) : '';
                    if ( $fh ) fclose( $fh );
                    if ( $head === '%PDF' ) { $dace_paths[] = $cached_path; continue; }
                }

                $item_id = (string) $order->get_meta( '_melhor_envio_item_id' );
                if ( ! $item_id ) { $errors[] = '#' . $order_id . ': sem item_id ME'; continue; }

                $url      = "https://{$host}/api/v2/me/imprimir/dace/pdf/" . rawurlencode( $item_id );
                $response = wp_safe_remote_get( $url, [
                    'timeout' => 60,
                    'headers' => [
                        'Authorization' => 'Bearer ' . $token,
                        'Accept'        => 'application/json, application/pdf',
                        'Content-Type'  => 'application/json',
                        'User-Agent'    => 'Senderzz Logistics (suporte@app.senderzz.com.br)',
                    ],
                ] );

                if ( is_wp_error( $response ) ) { $errors[] = '#' . $order_id . ': ' . $response->get_error_message(); continue; }

                $code = (int) wp_remote_retrieve_response_code( $response );
                $body = (string) wp_remote_retrieve_body( $response );

                if ( ! in_array( $code, [ 200, 201 ], true ) ) { $errors[] = '#' . $order_id . ': HTTP ' . $code; continue; }

                // PDF direto ou URL assinada
                $pdf_body = '';
                if ( strncmp( $body, '%PDF', 4 ) === 0 ) {
                    $pdf_body = $body;
                } else {
                    $data = json_decode( $body, true );
                    $pdf_url = '';
                    if ( is_string( $data ) && preg_match( '#^https?://#i', $data ) ) {
                        $pdf_url = $data;
                    } elseif ( is_array( $data ) ) {
                        foreach ( $data as $value ) {
                            if ( is_string( $value ) && preg_match( '#^https?://#i', $value ) ) { $pdf_url = $value; break; }
                        }
                    }
                    if ( $pdf_url ) {
                        $dl = wp_safe_remote_get( $pdf_url, [ 'timeout' => 60, 'headers' => [ 'Accept' => 'application/pdf', 'User-Agent' => 'Senderzz Logistics (suporte@app.senderzz.com.br)' ] ] );
                        if ( ! is_wp_error( $dl ) && strncmp( (string) wp_remote_retrieve_body( $dl ), '%PDF', 4 ) === 0 ) {
                            $pdf_body = (string) wp_remote_retrieve_body( $dl );
                        }
                    }
                }

                if ( ! $pdf_body ) { $errors[] = '#' . $order_id . ': DACE não disponível'; continue; }

                $fname = sanitize_file_name( sprintf( 'dace-%d-%s.pdf', $order_id, gmdate( 'Ymd-His' ) ) );
                $fpath = trailingslashit( $dir ) . $fname;
                $furl  = trailingslashit( $upload_dir['baseurl'] ) . trim( $subdir, '/' ) . '/' . $fname;
                file_put_contents( $fpath, $pdf_body );

                $order->update_meta_data( '_melhor_envio_dace_pdf_path', $fpath );
                $order->update_meta_data( '_melhor_envio_dace_pdf_url', $furl );
                $order->save();

                $dace_paths[] = $fpath;
            }

            if ( empty( $dace_paths ) ) {
                return new WP_REST_Response( [ 'success' => false, 'message' => 'Nenhuma DACE disponível. ' . implode( ' | ', $errors ) ], 422 );
            }

            // Une múltiplas DACEs se necessário
            if ( count( $dace_paths ) === 1 ) {
                $order = wc_get_order( $order_ids[0] );
                $url   = $order ? (string) $order->get_meta( '_melhor_envio_dace_pdf_url' ) : '';
                return new WP_REST_Response( [ 'success' => true, 'url' => $url ], 200 );
            }

            try {
                $composer = new \WC_MelhorEnvio\Pdf\Batch_Composer();
                $result   = $composer->compose( $order_ids, [ 'include_packing_slip' => false ] );
                return new WP_REST_Response( [ 'success' => true, 'url' => $result['url'] ?? '' ], 200 );
            } catch ( Throwable $e ) {
                // Fallback: retorna a primeira DACE
                $order = wc_get_order( $order_ids[0] );
                $url   = $order ? (string) $order->get_meta( '_melhor_envio_dace_pdf_url' ) : '';
                return new WP_REST_Response( [ 'success' => true, 'url' => $url ], 200 );
            }
        },
        'permission_callback' => 'senderzz_rest_operator_permission',
    ] );

    register_rest_route( $ns, '/operator/labels/print-urls', [
        'methods'             => 'POST',
        'callback'            => function ( WP_REST_Request $request ) {
            $order_ids = array_values( array_filter( array_map( 'absint', (array) $request->get_param( 'order_ids' ) ) ) );
            if ( empty( $order_ids ) ) {
                return new WP_REST_Response( [ 'success' => false, 'message' => 'Nenhum pedido informado.' ], 400 );
            }
            $result = [];
            foreach ( $order_ids as $order_id ) {
                $order = wc_get_order( $order_id );
                if ( ! $order ) continue;
                $print_url = (string) $order->get_meta( '_melhor_envio_print_url' );
                $result[] = [
                    'order_id'  => $order_id,
                    'print_url' => $print_url,
                ];
            }
            return new WP_REST_Response( [ 'success' => true, 'orders' => $result ], 200 );
        },
        'permission_callback' => 'senderzz_rest_operator_permission',
    ] );

    register_rest_route( $ns, '/operator/labels/batch-print', [
        'methods'             => 'POST',
        'callback'            => 'senderzz_rest_operator_batch_print',
        'permission_callback' => 'senderzz_rest_operator_permission',
    ] );

    register_rest_route( $ns, '/operator/labels/generate', [
        'methods'             => 'POST',
        'callback'            => 'senderzz_rest_operator_generate_labels',
        'permission_callback' => 'senderzz_rest_operator_permission',
    ] );
} );

function senderzz_operator_session_token_values( string $token ): array {
    $token = sanitize_text_field( wp_unslash( $token ) );
    if ( $token === '' ) {
        return [ '', '' ];
    }

    if ( class_exists( '\\WC_MelhorEnvio\\Portal\\Portal_Auth' ) ) {
        return [ $token, \WC_MelhorEnvio\Portal\Portal_Auth::hash_session_token( $token ) ];
    }

    $salt = defined( 'AUTH_SALT' ) && AUTH_SALT ? AUTH_SALT : wp_salt( 'auth' );
    return [ $token, hash_hmac( 'sha256', $token, $salt ) ];
}

function senderzz_rest_operator_permission(): bool {
    // Admin/gerente WooCommerce via sessão WordPress continua liberado.
    if ( is_user_logged_in() && ( current_user_can( 'manage_options' ) || current_user_can( 'manage_woocommerce' ) ) ) {
        return true;
    }

    // Auth via sessão do portal Senderzz. A sessão é salva no banco com hash,
    // então precisamos aceitar o token bruto do cookie e a versão hash.
    $token = $_COOKIE['senderzz_portal_session'] ?? ( $_SERVER['HTTP_X_SENDERZZ_TOKEN'] ?? '' );
    if ( ! $token ) {
        return false;
    }

    global $wpdb;
    [ $token_raw, $token_hash ] = senderzz_operator_session_token_values( (string) $token );

    $sess_table = $wpdb->prefix . 'senderzz_portal_sessions';
    $user_table = $wpdb->prefix . 'senderzz_portal_users';

    $session = $wpdb->get_row( $wpdb->prepare(
        "SELECT s.user_id FROM {$sess_table} s WHERE s.token IN (%s, %s) AND s.expires_at > NOW() LIMIT 1",
        $token_raw,
        $token_hash
    ) );
    if ( ! $session ) {
        return false;
    }

    $user = $wpdb->get_row( $wpdb->prepare(
        "SELECT id, role, status, permissions FROM {$user_table} WHERE id = %d AND status = 'active' LIMIT 1",
        (int) $session->user_id
    ) );

    if ( ! $user ) {
        return false;
    }

    $role = strtolower( trim( (string) $user->role ) );
    $allowed_roles = [
        'operator',
        'operador',
        'operador_logistico',
        'logistics_operator',
        'senderzz_operator',
        'sz_operator',
    ];

    if ( in_array( $role, $allowed_roles, true ) || strpos( $role, 'operator' ) !== false || strpos( $role, 'operador' ) !== false ) {
        return true;
    }

    $permissions = json_decode( (string) ( $user->permissions ?? '' ), true );
    return is_array( $permissions ) && ! empty( $permissions['operator'] );
}

function senderzz_rest_operator_orders( WP_REST_Request $request ): WP_REST_Response {
    $status_param = sanitize_text_field( $request->get_param( 'status' ) ?: 'all' );
    $statuses = ( $status_param === 'all' ) ? array_keys( wc_get_order_statuses() ) : array_filter( explode( ',', $status_param ) );
    $orders = senderzz_operator_get_orders( [
        'status'    => $statuses,
        'limit'     => min( 500, absint( $request->get_param( 'limit' ) ?: 200 ) ),
        'page'      => max( 1, absint( $request->get_param( 'page' ) ?: 1 ) ),
        'search'    => sanitize_text_field( $request->get_param( 'search' ) ?: '' ),
        'carrier'   => sanitize_text_field( $request->get_param( 'carrier' ) ?: '' ),
        'class'     => sanitize_text_field( $request->get_param( 'class' ) ?: '' ),
        'kind'      => sanitize_key( $request->get_param( 'kind' ) ?: '' ),
    ] );
    return new WP_REST_Response( [ 'success' => true, 'orders' => $orders ], 200 );
}

function senderzz_rest_operator_status( WP_REST_Request $request ): WP_REST_Response {
    $result = senderzz_operator_update_status(
        absint( $request->get_param( 'id' ) ),
        sanitize_key( $request->get_param( 'ol_status' ) ?: '' )
    );
    return new WP_REST_Response( $result, $result['success'] ? 200 : 400 );
}

// ── Baixa por admin — POST /operator/orders/{id}/baixa ──────────────────────
function senderzz_rest_operator_baixa( WP_REST_Request $request ): WP_REST_Response {
    global $wpdb;
    $order_id        = absint( $request->get_param( 'id' ) );
    $recebedor_nome  = sanitize_text_field( $request->get_param( 'recebedor_nome' ) ?: '' );
    $recebedor_cpf   = preg_replace( '/\D+/', '', sanitize_text_field( $request->get_param( 'recebedor_cpf' ) ?: '' ) );
    $recebedor_tipo  = in_array( $request->get_param( 'recebedor_tipo' ), ['cliente','terceiro'], true ) ? $request->get_param( 'recebedor_tipo' ) : 'cliente';
    $motoboy_id      = absint( $request->get_param( 'motoboy_id' ) ?: 0 );
    $dinheiro        = sz_mb_parse_money( $request->get_param( 'pgto_dinheiro' ) ?: 0 );
    $pix             = sz_mb_parse_money( $request->get_param( 'pgto_pix' ) ?: 0 );
    $cartao          = sz_mb_parse_money( $request->get_param( 'pgto_cartao' ) ?: 0 );
    $admin_user_id   = get_current_user_id();
    $admin_user_name = get_userdata( $admin_user_id )->display_name ?? 'Admin';

    if ( ! $order_id ) return new WP_REST_Response( ['success'=>false,'message'=>'Pedido inválido.'], 400 );
    if ( ! $recebedor_cpf || ! sz_mb_validar_cpf( $recebedor_cpf ) ) return new WP_REST_Response( ['success'=>false,'message'=>'CPF inválido.'], 422 );
    if ( $recebedor_tipo === 'terceiro' && strlen( trim( $recebedor_nome ) ) < 3 ) return new WP_REST_Response( ['success'=>false,'message'=>'Informe o nome do recebedor.'], 422 );

    $order = wc_get_order( $order_id );
    if ( ! $order ) return new WP_REST_Response( ['success'=>false,'message'=>'Pedido WC não encontrado.'], 404 );

    $pedido = $wpdb->get_row( $wpdb->prepare(
        "SELECT * FROM {$wpdb->prefix}sz_motoboy_pedidos WHERE wc_order_id = %d LIMIT 1", $order_id
    ) );
    if ( ! $pedido ) return new WP_REST_Response( ['success'=>false,'message'=>'Pedido motoboy não encontrado.'], 404 );

    $valor_pedido = round( (float) $pedido->valor_pedido, 2 );
    $total_pago   = round( $dinheiro + $pix + $cartao, 2 );
    if ( $total_pago <= 0 ) return new WP_REST_Response( ['success'=>false,'message'=>'Informe o valor recebido.'], 422 );
    if ( abs( $total_pago - $valor_pedido ) > 0.009 ) return new WP_REST_Response( ['success'=>false,'message'=>'Total recebido deve bater com o valor do pedido (R$ '.number_format($valor_pedido,2,',','.').' ).'], 422 );

    if ( function_exists( 'sz_mbc_validate_transition' ) ) {
        $custody_check = sz_mbc_validate_transition( (int) $pedido->id, 'entregue', [], 'admin', $admin_user_id );
        if ( is_wp_error( $custody_check ) ) {
            return new WP_REST_Response( [ 'success' => false, 'message' => $custody_check->get_error_message() ], 409 );
        }
    }

    // Se for cliente, usa nome do destinatário
    if ( $recebedor_tipo === 'cliente' ) $recebedor_nome = (string) $pedido->dest_nome;

    // Motoboy que recebe o valor
    $mb_nome = '';
    if ( $motoboy_id ) {
        $mb_row = $wpdb->get_row( $wpdb->prepare( "SELECT nome FROM {$wpdb->prefix}sz_motoboys WHERE id=%d LIMIT 1", $motoboy_id ) );
        $mb_nome = $mb_row ? $mb_row->nome : '';
    }

    $now_mysql = current_time( 'mysql' );
    $cpf_mask  = function_exists( 'sz_mb_mask_cpf' ) ? sz_mb_mask_cpf( $recebedor_cpf ) : $recebedor_cpf;

    // Atualiza tabela motoboy_pedidos
    $wpdb->update( $wpdb->prefix . 'sz_motoboy_pedidos', [
        'status'            => 'entregue',
        'recebedor_nome'    => $recebedor_nome,
        'recebedor_cpf'     => $recebedor_cpf,
        'recebedor_tipo'    => $recebedor_tipo,
        'pgto_dinheiro'     => $dinheiro,
        'pgto_pix'          => $pix,
        'pgto_cartao'       => $cartao,
        'baixa_por'         => 'admin',
        'baixa_admin_user_id' => $admin_user_id,
        'baixa_motoboy_id'  => $motoboy_id ?: null,
        'baixa_at'          => $now_mysql,
        'ts_entregue'       => $now_mysql,
    ], ['id' => (int) $pedido->id] );

    // Dispara hook de status
    do_action( 'sz_motoboy_status_changed', (int) $pedido->id, $pedido->status, 'entregue', $pedido );

    // Atualiza metas WC
    $order->update_meta_data( '_senderzz_motoboy_flow_status',      'entregue' );
    $order->update_meta_data( '_senderzz_motoboy_status',           'entregue' );
    $order->update_meta_data( '_senderzz_motoboy_entregue_at',      $now_mysql );
    $order->update_meta_data( '_senderzz_motoboy_baixa_por',        'admin' );
    $order->update_meta_data( '_senderzz_motoboy_baixa_admin_id',   $admin_user_id );
    $order->update_meta_data( '_senderzz_motoboy_baixa_admin_nome', $admin_user_name );
    $order->update_meta_data( '_senderzz_motoboy_baixa_at',         $now_mysql );
    // Flag para o motoboy confirmar recebimento do dinheiro na conferência do dia seguinte
    if ( $motoboy_id ) {
        $order->update_meta_data( '_sz_mb_pendente_confirmacao_repasse', '1' );
        $order->update_meta_data( '_sz_mb_confirmacao_repasse_motoboy_id', $motoboy_id );
    }
    $order->update_meta_data( '_senderzz_motoboy_recebedor_nome',   $recebedor_nome );
    $order->update_meta_data( '_senderzz_motoboy_recebedor_cpf',    $recebedor_cpf );
    $order->update_meta_data( '_senderzz_motoboy_recebedor_tipo',   $recebedor_tipo );
    $order->update_meta_data( '_senderzz_motoboy_pgto_dinheiro',    $dinheiro );
    $order->update_meta_data( '_senderzz_motoboy_pgto_pix',         $pix );
    $order->update_meta_data( '_senderzz_motoboy_pgto_cartao',      $cartao );
    $order->update_meta_data( '_senderzz_motoboy_pgto_total',       $total_pago );
    if ( $motoboy_id ) {
        $order->update_meta_data( '_senderzz_motoboy_entregador_id',   $motoboy_id );
        $order->update_meta_data( '_senderzz_motoboy_entregador_nome', $mb_nome );
    }
    if ( ! $order->has_status( 'entregue' ) ) {
        $order->update_status( 'entregue',
            'Senderzz COD: baixa manual por ' . $admin_user_name .
            '. Recebedor: ' . $recebedor_nome . ' / CPF ' . $cpf_mask .
            '. Total: R$ ' . number_format( $total_pago, 2, ',', '.' ) .
            ( $mb_nome ? '. Motoboy: ' . $mb_nome : ' (sem motoboy atribuído)' ) . '.'
        );
    } else {
        $order->add_order_note(
            'Senderzz COD: baixa manual por ' . $admin_user_name .
            '. Recebedor: ' . $recebedor_nome . ' / CPF ' . $cpf_mask .
            '. Total: R$ ' . number_format( $total_pago, 2, ',', '.' ) .
            ( $mb_nome ? '. Motoboy: ' . $mb_nome : ' (sem motoboy atribuído)' ) . '.'
        );
        $order->save();
    }
    if ( function_exists( 'sz_cod_wallet_record_delivery' ) ) {
        sz_cod_wallet_record_delivery( $order, $total_pago, $motoboy_id ?: null );
    }

    // Registra auditoria
    if ( function_exists( 'sz_motoboy_audit' ) && $pedido ) {
        sz_motoboy_audit( [
            'pedido_id'  => (int) $pedido->id,
            'motoboy_id' => $motoboy_id ?: null,
            'actor_tipo' => 'admin',
            'actor_id'   => $admin_user_id,
            'acao'       => 'baixa_admin',
            'de_status'  => $pedido->status,
            'para_status'=> 'entregue',
            'meta'       => [
                'recebedor_nome' => $recebedor_nome,
                'recebedor_cpf'  => $recebedor_cpf,
                'recebedor_tipo' => $recebedor_tipo,
                'total_pago'     => $total_pago,
                'motoboy_nome'   => $mb_nome,
                'admin_nome'     => $admin_user_name,
            ],
        ] );
    }

    return new WP_REST_Response( ['success'=>true,'message'=>'Baixa registrada por '.$admin_user_name.'.'], 200 );
}

function senderzz_rest_operator_list_motoboys( WP_REST_Request $request ): WP_REST_Response {
    global $wpdb;
    $motoboys = $wpdb->get_results(
        "SELECT m.id, m.nome, m.telefone, z.nome AS zona_nome, cd.nome AS cd_nome
           FROM {$wpdb->prefix}sz_motoboys m
           LEFT JOIN {$wpdb->prefix}sz_motoboy_zonas z  ON z.id  = m.zona_id
           LEFT JOIN {$wpdb->prefix}sz_motoboy_cds  cd ON cd.id = m.cd_id
          WHERE m.ativo = 1
          ORDER BY m.nome",
        ARRAY_A
    ) ?: [];

    return new WP_REST_Response( [
        'success'   => true,
        'motoboys'  => array_map( function( $mb ) {
            return [
                'id'       => (int) $mb['id'],
                'nome'     => $mb['nome'],
                'telefone' => $mb['telefone'],
                'label'    => $mb['nome'] . ( $mb['zona_nome'] ? ' — ' . $mb['zona_nome'] : '' ),
            ];
        }, $motoboys ),
    ], 200 );
}


function senderzz_rest_operator_reschedule_order( WP_REST_Request $request ): WP_REST_Response {
    global $wpdb;

    $order_id = absint( $request->get_param( 'id' ) );
    $date_raw = sanitize_text_field( (string) ( $request->get_param( 'delivery_date' ) ?: '' ) );

    if ( ! $order_id ) {
        return new WP_REST_Response( [ 'success' => false, 'message' => 'Pedido inválido.' ], 400 );
    }
    if ( ! preg_match( '/^\d{4}-\d{2}-\d{2}$/', $date_raw ) ) {
        return new WP_REST_Response( [ 'success' => false, 'message' => 'Informe uma data válida.' ], 422 );
    }

    $ts = strtotime( $date_raw . 'T12:00:00' );
    if ( ! $ts ) {
        return new WP_REST_Response( [ 'success' => false, 'message' => 'Data inválida.' ], 422 );
    }

    $order = wc_get_order( $order_id );
    if ( ! $order ) {
        return new WP_REST_Response( [ 'success' => false, 'message' => 'Pedido WooCommerce não encontrado.' ], 404 );
    }

    $city = (string) ( $order->get_shipping_city() ?: $order->get_billing_city() );
    if ( ! $city ) {
        $row_city = $wpdb->get_var( $wpdb->prepare( "SELECT dest_cidade FROM {$wpdb->prefix}sz_motoboy_pedidos WHERE wc_order_id = %d LIMIT 1", $order_id ) );
        $city = (string) $row_city;
    }
    if ( $city && function_exists( 'sz_dr_date_allowed_for_city' ) && ! sz_dr_date_allowed_for_city( $city, $date_raw ) ) {
        $region = function_exists( 'sz_dr_region_label_for_city' ) ? sz_dr_region_label_for_city( $city ) : '';
        $msg = $region
            ? sprintf( 'A cidade %s pertence à rota %s. Escolha uma data permitida para essa rota.', $city, $region )
            : sprintf( 'A data selecionada não é permitida para a cidade %s.', $city );
        return new WP_REST_Response( [ 'success' => false, 'message' => $msg ], 422 );
    }

    $old_date = (string) ( $order->get_meta( '_sz_delivery_date', true ) ?: $order->get_meta( '_senderzz_delivery_date', true ) ?: $order->get_meta( '_senderzz_delivery_time', true ) ?: $order->get_meta( '_sz_motoboy_entrega_data', true ) ?: '' );

    // Mantém as metas usadas nas telas antigas e novas sincronizadas.
    $order->update_meta_data( '_sz_delivery_date', $date_raw );
    $order->update_meta_data( '_senderzz_delivery_date', $date_raw );
    $order->update_meta_data( '_sz_motoboy_entrega_data', $date_raw );
    $order->update_meta_data( '_senderzz_rescheduled_at', current_time( 'mysql' ) );
    $order->update_meta_data( '_senderzz_rescheduled_by', get_current_user_id() );
    // Reagendamento feito pelo Admin Senderzz não altera status operacional/Woo.
    // Mantém exatamente o status atual do pedido e apenas troca a data de entrega.

    $formatted = wp_date( 'd/m/Y', $ts );
    $note_old = $old_date && preg_match( '/^(\d{4}-\d{2}-\d{2})/', $old_date, $m ) ? wp_date( 'd/m/Y', strtotime( $m[1] . 'T12:00:00' ) ) : ( $old_date ?: 'sem data' );
    $order->add_order_note( 'Senderzz: entrega reagendada de ' . $note_old . ' para ' . $formatted . '.' );

    $pedido = $wpdb->get_row( $wpdb->prepare(
        "SELECT id, status FROM {$wpdb->prefix}sz_motoboy_pedidos WHERE wc_order_id = %d LIMIT 1",
        $order_id
    ) );

    if ( $pedido ) {
        $wpdb->update(
            $wpdb->prefix . 'sz_motoboy_pedidos',
            [
                'reagendado_para' => $date_raw,
                // Mantém o status atual da linha Motoboy.
                'updated_at'      => current_time( 'mysql' ),
            ],
            [ 'id' => (int) $pedido->id ],
            [ '%s', '%s' ],
            [ '%d' ]
        );
        // Sem hook de mudança de status: a ação foi apenas reagendamento administrativo.
    }

    $order->save();

    return new WP_REST_Response( [
        'success'       => true,
        'message'       => 'Pedido #' . $order_id . ' reagendado para ' . $formatted . '.',
        'delivery_date' => $formatted,
        'delivery_raw'  => $date_raw,
    ], 200 );
}

function senderzz_rest_operator_assign_motoboy( WP_REST_Request $request ): WP_REST_Response {
    global $wpdb;
    $order_id   = absint( $request->get_param( 'id' ) );
    $motoboy_id = absint( $request->get_param( 'motoboy_id' ) );

    if ( ! $order_id || ! $motoboy_id ) {
        return new WP_REST_Response( [ 'success' => false, 'message' => 'Parâmetros inválidos.' ], 400 );
    }

    // Confirma que motoboy existe e está ativo
    $mb = $wpdb->get_row( $wpdb->prepare(
        "SELECT id, nome FROM {$wpdb->prefix}sz_motoboys WHERE id = %d AND ativo = 1 LIMIT 1",
        $motoboy_id
    ) );
    if ( ! $mb ) {
        return new WP_REST_Response( [ 'success' => false, 'message' => 'Motoboy não encontrado.' ], 404 );
    }

    // Busca pedido motoboy pelo wc_order_id
    $pedido = $wpdb->get_row( $wpdb->prepare(
        "SELECT id, motoboy_id, status FROM {$wpdb->prefix}sz_motoboy_pedidos WHERE wc_order_id = %d LIMIT 1",
        $order_id
    ) );

    if ( ! $pedido ) {
        // Cria o registro motoboy se não existir ainda
        if ( function_exists( 'sz_motoboy_criar_pedido' ) ) {
            sz_motoboy_criar_pedido( $order_id );
            $pedido = $wpdb->get_row( $wpdb->prepare(
                "SELECT id, motoboy_id, status FROM {$wpdb->prefix}sz_motoboy_pedidos WHERE wc_order_id = %d LIMIT 1",
                $order_id
            ) );
        }
        if ( ! $pedido ) {
            return new WP_REST_Response( [ 'success' => false, 'message' => 'Pedido motoboy não encontrado.' ], 404 );
        }
    }

    // Atualiza/troca motoboy_id sem alterar status do pedido.
    $old_motoboy_id = (int) ( $pedido->motoboy_id ?? 0 );
    $update_data = [ 'motoboy_id' => $motoboy_id, 'updated_at' => current_time( 'mysql' ) ];
    if ( in_array( sanitize_key( (string) ( $pedido->status ?? '' ) ), [ 'entregue', 'frustrado' ], true ) ) {
        $update_data['baixa_motoboy_id'] = $motoboy_id;
    }
    $wpdb->update(
        $wpdb->prefix . 'sz_motoboy_pedidos',
        $update_data,
        [ 'id' => (int) $pedido->id ]
    );

    $order = wc_get_order( $order_id );
    if ( $order ) {
        senderzz_operator_set_order_motoboy_meta( $order_id, (int) $motoboy_id, (string) $mb->nome );
        senderzz_operator_purge_motoboy_financial_rows_for_order( $order_id );
        $order->add_order_note( 'Senderzz COD: motoboy ' . ( $old_motoboy_id ? 'trocado' : 'atribuído' ) . ' pelo operador logístico para ' . $mb->nome . '. Carteira/conciliação do pedido foram limpas para reconstrução pela fonte única.' );
        $order->save();
    }

    // Notifica o motoboy via push
    if ( function_exists( 'sz_mb_push_motoboy' ) ) {
        $order = wc_get_order( $order_id );
        $dest  = $order ? trim( $order->get_shipping_first_name() . ' ' . $order->get_shipping_last_name() ) : '#' . $order_id;
        sz_mb_push_motoboy( $motoboy_id, '📦 Novo pedido atribuído', 'Pedido #' . $order_id . ' para ' . $dest . ' foi atribuído a você.' );
    }

    return new WP_REST_Response( [
        'success' => true,
        'message' => 'Motoboy ' . $mb->nome . ' atribuído ao pedido #' . $order_id . '.',
        'motoboy' => [ 'id' => (int) $mb->id, 'nome' => $mb->nome ],
    ], 200 );
}

function senderzz_rest_operator_stats( WP_REST_Request $request ): WP_REST_Response {
    global $wpdb;

    $orders = array_merge(
        senderzz_operator_get_orders( [
            'status' => array_keys( wc_get_order_statuses() ),
            'limit'  => 500,
            'kind'   => 'motoboy',
        ] ),
        senderzz_operator_get_orders( [
            'status' => array_keys( wc_get_order_statuses() ),
            'limit'  => 500,
            'kind'   => 'expedicao',
        ] )
    );

    $stats = [
        'aprovados'       => 0,
        'separados'       => 0,
        'embalados'       => 0,
        'aguardando'      => 0,
        'erro'            => 0,
        'atrasados'       => 0,
        'margem_hoje_raw' => 0.0,
        'saldo_total_raw' => 0.0,
        'etiquetas_hoje'  => 0,
        'pix_pendentes'   => 0,
        'usuarios_ativos' => 0,
        'classes'         => [],
        'motoboy_agendados' => 0,
        'motoboy_embalados' => 0,
        'expedicao_aprovados' => 0,
        'expedicao_embalados' => 0,
    ];

    foreach ( $orders as $o ) {
        $ol = (string) ( $o['ol_status'] ?? '' );
        $has_label = empty( $o['label_missing'] ) && ! empty( $o['label_url'] );
        $is_final = in_array( $ol, [ 'enviado', 'concluido', 'completed', 'cancelado', 'extravio', 'devolvido', 'entregue', 'refunded', 'failed' ], true );
        $is_motoboy = ! empty( $o['is_motoboy'] );
        if ( $is_motoboy && $ol === 'agendado' && ! $is_final ) $stats['motoboy_agendados']++;
        if ( $is_motoboy && $ol === 'embalado' && ! $is_final ) $stats['motoboy_embalados']++;
        if ( ! $is_motoboy && in_array( $ol, [ 'aprovado', 'separado' ], true ) && ! $is_final ) $stats['expedicao_aprovados']++;
        if ( ! $is_motoboy && $ol === 'embalado' && ! $is_final ) $stats['expedicao_embalados']++;

        if ( $ol === 'pendente' && ! $is_final ) {
            $stats['aguardando']++;
        }
        if ( $ol === 'erro' && ! $is_final ) {
            $stats['erro']++;
        }
        if ( in_array( $ol, [ 'aprovado', 'agendado' ], true ) && ! $is_final ) {
            $stats['aprovados']++;
        }
        if ( $ol === 'separado' && ! $is_final ) {
            $stats['separados']++;
        }
        if ( $ol === 'embalado' && ! $is_final ) {
            $stats['embalados']++;
        }
        if ( ( $o['sla_status'] ?? '' ) === 'atrasado' && ! $is_final ) {
            $stats['atrasados']++;
        }

        $key = ! empty( $o['shipping_class_id'] ) ? (string) $o['shipping_class_id'] : 'sem-classe';
        if ( ! isset( $stats['classes'][ $key ] ) ) {
            $stats['classes'][ $key ] = [
                'id'    => $o['shipping_class_id'] ?? 0,
                'name'  => $o['shipping_class'] ?? 'Sem classe',
                'count' => 0,
            ];
        }
        $stats['classes'][ $key ]['count']++;
    }

    // Saldo total em carteiras — mesmo dado-base do dashboard Senderzz.
    $table_wallet = $wpdb->prefix . 'tpc_carteira';
    $exists_wallet = $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $table_wallet ) );
    if ( $exists_wallet ) {
        $stats['saldo_total_raw'] = (float) $wpdb->get_var( "SELECT COALESCE(SUM(saldo),0) FROM {$table_wallet}" );
    }

    // PIX pendentes — recargas PIX ainda pendentes/análise.
    $table_tx = $wpdb->prefix . 'tpc_transacoes';
    $exists_tx = $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $table_tx ) );
    if ( $exists_tx ) {
        $stats['pix_pendentes'] = (int) $wpdb->get_var( "SELECT COUNT(*) FROM {$table_tx} WHERE tipo = 'credito' AND status IN ('pendente','analise') AND referencia LIKE 'recarga_pix%'" );
    }

    // Usuários ativos no portal.
    $table_users = $wpdb->prefix . 'senderzz_portal_users';
    $exists_users = $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $table_users ) );
    if ( $exists_users ) {
        $stats['usuarios_ativos'] = (int) $wpdb->get_var( "SELECT COUNT(*) FROM {$table_users} WHERE status = 'active'" );
    }

    // Etiquetas emitidas hoje e margem válida hoje.
    $today_start = strtotime( current_time( 'Y-m-d' ) . ' 00:00:00' );
    $today_end   = strtotime( current_time( 'Y-m-d' ) . ' 23:59:59' );
    $today_orders = wc_get_orders( [
        'type'    => 'shop_order',
        'status'  => array_keys( wc_get_order_statuses() ),
        'limit'   => 500,
        'orderby' => 'date',
        'order'   => 'DESC',
    ] );

    foreach ( $today_orders as $order ) {
        if ( ! $order instanceof \WC_Order ) continue;

        // Etiquetas emitidas hoje: pedido tem item_id ME e foi modificado hoje
        $item_id = $order->get_meta( '_melhor_envio_item_id' );
        if ( $item_id ) {
            $dt = $order->get_date_modified() ?: $order->get_date_created();
            $label_time = $dt ? $dt->getTimestamp() : 0;
            if ( $label_time >= $today_start && $label_time <= $today_end ) {
                $stats['etiquetas_hoje']++;
            }
        }

        // Margem hoje: apenas pedidos em status "enviado" com sent_at registrado hoje.
        // Não usa date_modified como fallback para evitar contar pedidos cancelados/em andamento.
        $is_loss = senderzz_operator_is_loss_status( $order->get_status() ) || (bool) $order->get_meta( '_senderzz_operator_extravio_at' );
        if ( $is_loss ) continue;

        $sent_at_raw = (string) $order->get_meta( '_senderzz_operator_sent_at' );
        if ( ! $sent_at_raw ) continue; // só conta se tem timestamp explícito de envio

        $sent_ts = strtotime( $sent_at_raw );
        if ( $sent_ts >= $today_start && $sent_ts <= $today_end ) {
            $stats['margem_hoje_raw'] += round( senderzz_operator_gross_profit( $order ) * 0.5, 2 );
        }
    }


    // Contadores COD Motoboy diretos da tabela operacional.
    $mb_table = $wpdb->prefix . 'sz_motoboy_pedidos';
    if ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $mb_table ) ) ) {
        $stats['motoboy_agendados'] = (int) $wpdb->get_var( "SELECT COUNT(*) FROM {$mb_table} WHERE status IN ('agendado','aprovado')" );
        $stats['motoboy_embalados'] = (int) $wpdb->get_var( "SELECT COUNT(*) FROM {$mb_table} WHERE status = 'embalado'" );
    }

    $stats['saldo_total_fmt'] = senderzz_operator_money( $stats['saldo_total_raw'] );
    $stats['margem_hoje_fmt'] = senderzz_operator_money( $stats['margem_hoje_raw'] );
    $stats['classes']         = array_values( $stats['classes'] );

    // Compatibilidade retroativa
    $stats['prontos']        = $stats['aguardando'];
    $stats['coletados']      = 0; // removido do fluxo v2.5.45
    $stats['enviar']         = $stats['aprovados']; // alias legado
    $stats['acao_requerida'] = $stats['aprovados'] + $stats['erro'];
    $stats['sem_etiqueta']   = $stats['aprovados'];
    $stats['sem_classe']     = 0;
    $stats['margem_raw']     = $stats['margem_hoje_raw'];
    $stats['margem_fmt']     = $stats['margem_hoje_fmt'];
    $stats['extravio_raw']   = 0.0;
    $stats['extravio_fmt']   = senderzz_operator_money( 0.0 );

    return new WP_REST_Response( array_merge( [ 'success' => true, 'data' => current_time( 'Y-m-d' ) ], $stats ), 200 );
}
function senderzz_rest_operator_report( WP_REST_Request $request ): WP_REST_Response {
    return new WP_REST_Response( senderzz_operator_report(
        sanitize_text_field( $request->get_param( 'start' ) ?: '' ),
        sanitize_text_field( $request->get_param( 'end' ) ?: '' )
    ), 200 );
}

function senderzz_rest_operator_batch_print( WP_REST_Request $request ): WP_REST_Response {
    $order_ids = array_values( array_filter( array_map( 'absint', (array) $request->get_param( 'order_ids' ) ) ) );
    if ( empty( $order_ids ) ) {
        return new WP_REST_Response( [ 'success' => false, 'message' => 'Selecione pelo menos um pedido.' ], 400 );
    }

    if ( count( $order_ids ) === 1 ) {
        $single = wc_get_order( $order_ids[0] );
        if ( $single ) {
            $cached_path = (string) $single->get_meta( '_senderzz_postal_dace_pdf_path' );
            $cached_url  = (string) $single->get_meta( '_senderzz_postal_dace_pdf_url' );
            if ( $cached_url && senderzz_operator_is_valid_pdf_file( $cached_path ) ) {
                return new WP_REST_Response( [
                    'success' => true,
                    'cached'  => true,
                    'message' => 'PDF postal + DACE ja processado para este pedido.',
                    'url'     => $cached_url,
                    'result'  => [ 'url' => $cached_url, 'path' => $cached_path, 'orders' => $order_ids, 'count' => 1 ],
                ], 200 );
            }
        }
    }

    $errors = [];
    foreach ( $order_ids as $order_id ) {
        $order = wc_get_order( $order_id );
        if ( ! $order ) {
            $errors[] = '#' . $order_id . ': pedido não encontrado';
            continue;
        }
        try {
            senderzz_operator_ensure_label_pdf( $order );
        } catch ( Throwable $e ) {
            $errors[] = '#' . $order_id . ': ' . $e->getMessage();
        }
    }

    if ( ! empty( $errors ) ) {
        return new WP_REST_Response( [
            'success' => false,
            'message' => 'Não foi possível salvar todos os PDFs no servidor: ' . implode( ' | ', $errors ),
            'errors'  => $errors,
        ], 207 );
    }

    try {
        $composer = new \WC_MelhorEnvio\Pdf\Batch_Composer();
        $result = $composer->compose( $order_ids, [
            'include_packing_slip' => false,
            'include_cover'        => (bool) $request->get_param( 'include_cover' ),
        ] );
        if ( count( $order_ids ) === 1 ) {
            $single = wc_get_order( $order_ids[0] );
            if ( $single && ! empty( $result['path'] ) && ! empty( $result['url'] ) ) {
                $single->update_meta_data( '_senderzz_postal_dace_pdf_path', $result['path'] );
                $single->update_meta_data( '_senderzz_postal_dace_pdf_url', $result['url'] );
                $single->update_meta_data( '_senderzz_postal_dace_pdf_at', time() );
                $single->save();
            }
        }
        return new WP_REST_Response( [ 'success' => true, 'message' => 'PDF postal + DACE salvo no servidor.', 'result' => $result, 'url' => $result['url'] ?? '' ], 200 );
    } catch ( Throwable $e ) {
        return new WP_REST_Response( [ 'success' => false, 'message' => $e->getMessage() ], 500 );
    }
}

/**
 * Garante que a etiqueta do pedido exista no Melhor Envio e que o PDF local esteja salvo.
 * Corrige o caso em que a etiqueta era apenas "solicitada", mas o operador não recebia o PDF.
 */
function senderzz_operator_ensure_label_pdf( \WC_Order $order ): array {
    if ( ! class_exists( '\\WC_MelhorEnvio\\Api\\Print_Label' ) || ! class_exists( '\\WC_MelhorEnvio\\Api\\Download_Label' ) ) {
        throw new \Exception( 'Módulo de impressão/download de etiqueta indisponível.' );
    }

    $existing_path = (string) $order->get_meta( '_melhor_envio_pdf_local_path' );
    $existing_url  = (string) $order->get_meta( '_melhor_envio_pdf_local_url' );
    if ( $existing_url && senderzz_operator_is_valid_pdf_file( $existing_path ) ) {
        return [
            'order_id' => $order->get_id(),
            'url'      => $existing_url,
            'path'     => $existing_path,
            'cached'   => true,
        ];
    }

    $lock_key = 'senderzz_label_lock_' . $order->get_id();
    if ( get_transient( $lock_key ) ) {
        throw new \Exception( 'Etiqueta já está em processamento para este pedido. Aguarde alguns segundos e tente novamente.' );
    }
    set_transient( $lock_key, 1, 120 );

    try {
        $item_id = (string) $order->get_meta( '_melhor_envio_item_id' );

        if ( ! $item_id ) {
            if ( ! class_exists( '\\WC_MelhorEnvio\\Pipeline\\Label_Pipeline' ) ) {
                throw new \Exception( 'Pipeline de etiqueta indisponível.' );
            }
            $pipeline = new \WC_MelhorEnvio\Pipeline\Label_Pipeline();
            $pipeline->process( $order, true );
            $order = wc_get_order( $order->get_id() );
            if ( ! $order ) {
                throw new \Exception( 'Pedido indisponível após emissão.' );
            }
        } else {
            // Se ja existe item_id, nao reprocessa nem reimprime a etiqueta.
            // Apenas baixa o PDF real da API/URL assinada e salva no servidor.
            $download = new \WC_MelhorEnvio\Api\Download_Label();
            $download->add_additional_param( 'order', $order );
            $downloaded = $download->download( $order );
        }

        $order = wc_get_order( $order->get_id() );
        if ( ! $order ) {
            throw new \Exception( 'Pedido indisponível após download da etiqueta.' );
        }

        $url  = (string) $order->get_meta( '_melhor_envio_pdf_local_url' );
        $path = (string) $order->get_meta( '_melhor_envio_pdf_local_path' );

        if ( ! $url || ! senderzz_operator_is_valid_pdf_file( $path ) ) {
            throw new \Exception( 'Etiqueta emitida, mas o PDF local válido não foi salvo.' );
        }

        $order->update_meta_data( '_melhor_envio_label_status', 'downloaded' );
        $order->delete_meta_data( '_melhor_envio_label_error' );
        $order->save();

        if ( class_exists( '\\WC_MelhorEnvio\\Database\\Label_Table' ) ) {
            try {
                $senderzz_label_data = function_exists( 'senderzz_order_shipping_snapshot' ) ? senderzz_order_shipping_snapshot( $order ) : [];
                \WC_MelhorEnvio\Database\Label_Table::upsert( array_merge( $senderzz_label_data, [
                    'order_id'    => $order->get_id(),
                    'shipment_id' => (string) $order->get_meta( '_melhor_envio_item_id' ),
                    'protocol'    => (string) $order->get_meta( '_melhor_envio_order_id' ),
                    'status'      => 'downloaded',
                    'print_url'   => (string) $order->get_meta( '_melhor_envio_print_url' ),
                    'pdf_path'    => $path,
                    'pdf_url'     => $url,
                    'operator_id' => (int) get_current_user_id(),
                ] ) );
            } catch ( \Throwable $e ) {}
        }

        return [
            'order_id' => $order->get_id(),
            'url'      => $url,
            'path'     => $path,
            'download' => isset( $downloaded ) ? ( $downloaded['data'] ?? [] ) : [],
        ];
    } finally {
        delete_transient( $lock_key );
    }
}

function senderzz_rest_operator_generate_labels( WP_REST_Request $request ): WP_REST_Response {
    $order_ids = array_values( array_filter( array_map( 'absint', (array) $request->get_param( 'order_ids' ) ) ) );
    if ( empty( $order_ids ) ) {
        return new WP_REST_Response( [ 'success' => false, 'message' => 'Selecione pelo menos um pedido.' ], 400 );
    }

    $done = [];
    $labels = [];
    $errors = [];

    foreach ( $order_ids as $order_id ) {
        $order = wc_get_order( $order_id );
        if ( ! $order ) {
            $errors[] = '#' . $order_id . ': pedido não encontrado';
            continue;
        }

        try {
            $label = senderzz_operator_ensure_label_pdf( $order );
            $done[] = $order_id;
            $labels[] = $label;
            $order->add_order_note( 'Senderzz: etiqueta gerada e PDF local salvo para o operador logístico.' );

            // Senderzz: não usamos mais o status "separado".
            // Gera/salva a etiqueta e mantém o pedido como APROVADO.
            $order = wc_get_order( $order_id );
            if ( $order && in_array( $order->get_status(), [ 'processing', 'on-hold' ], true ) ) {
                $order->update_status( 'aprovado', 'Senderzz: etiqueta gerada pelo operador. Pedido aprovado para expedição.' );
            } elseif ( $order && in_array( $order->get_status(), [ 'aprovado', 'approved' ], true ) ) {
                $order->add_order_note( 'Senderzz: etiqueta gerada pelo operador. Pedido mantido como aprovado.' );
                $order->save();
            }
        } catch ( Throwable $e ) {
            $order->update_meta_data( '_melhor_envio_label_status', 'error' );
            $order->update_meta_data( '_melhor_envio_label_error', $e->getMessage() );
            $order->save();
            $order->update_status( 'erro', 'Senderzz: erro ao gerar/salvar etiqueta pelo operador. Aguardando reprocessamento.' );
            $order->add_order_note( 'Senderzz: falha na geração/salvamento do PDF da etiqueta pelo operador. Pedido movido para a fila Erro. Erro: ' . $e->getMessage() );
            $errors[] = '#' . $order_id . ': ' . $e->getMessage();
        }
    }

    $message = empty( $errors )
        ? 'Etiquetas geradas e PDF principal salvo no servidor.'
        : 'Algumas etiquetas falharam: ' . implode( ' | ', $errors );

    return new WP_REST_Response( [
        'success' => empty( $errors ),
        'done'    => $done,
        'labels'  => $labels,
        'errors'  => $errors,
        'message' => $message,
    ], empty( $errors ) ? 200 : 207 );
}

/* ── Fonte única Motoboy: limpeza controlada de metas duplicadas ───────── */

add_action( 'admin_init', function () {
    if ( ! current_user_can( 'manage_woocommerce' ) ) return;
    $flag = 'senderzz_operator_single_motoboy_source_v339';
    if ( get_option( $flag ) === '1' ) return;
    senderzz_operator_cleanup_recent_motoboy_duplicates( 1200 );
    update_option( $flag, '1', false );
}, 12 );

/* ── Admin: cadastro do operador na página Carteira de Frete ─────────── */

add_action( 'admin_init', function () {
    if (
        ! isset( $_POST['senderzz_operator_nonce'] ) ||
        ! wp_verify_nonce( $_POST['senderzz_operator_nonce'], 'senderzz_criar_operator' ) ||
        ! current_user_can( 'manage_woocommerce' )
    ) return;

    $email = sanitize_email( $_POST['operator_email'] ?? '' );
    $pass  = sanitize_text_field( $_POST['operator_password'] ?? '' );
    $name  = sanitize_text_field( $_POST['operator_name'] ?? '' );
    $role  = in_array( $_POST['operator_role'] ?? '', [ 'client', 'operator' ], true ) ? sanitize_key( $_POST['operator_role'] ) : 'operator';

    if ( ! $email || ! $pass ) {
        add_action( 'admin_notices', function() { echo '<div class="notice notice-error"><p>Email e senha são obrigatórios para criar o operador.</p></div>'; } );
        return;
    }

    $result = $role === 'operator'
        ? senderzz_create_operator( $email, $pass, $name )
        : senderzz_create_portal_client( $email, $pass, $name );
    if ( $result['success'] ) {
        $uid_created = (int) $result['user_id'];
        add_action( 'admin_notices', function() use ( $uid_created ) { echo '<div class="notice notice-success is-dismissible"><p>Operador criado com sucesso! ID: ' . $uid_created . '</p></div>'; } );
    } else {
        $msg = esc_html( $result['message'] );
        add_action( 'admin_notices', function() use ( $msg ) { echo '<div class="notice notice-error"><p>Erro: ' . $msg . '</p></div>'; } );
    }
} );

// Menu unificado em Portal_Admin.php

// Admin page unificada em Portal_Admin.php