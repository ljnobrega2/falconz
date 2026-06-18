<?php
/**
 * Senderzz — Gestão de Classes de Entrega por Afiliado
 *
 * Funcionalidades:
 *  1. Painel admin (WooCommerce → Senderzz Afiliados) com aba "Classes de Entrega"
 *     que mostra todos os afiliados agrupados por classe, com editar/excluir/liberar.
 *  2. Auto-vinculação: ao aceitar convite de produtor, afiliado herda
 *     automaticamente todas as classes de entrega do produtor.
 *
 * Dependências: senderzz-affiliates.php, senderzz-multi-class.php
 */

defined( 'ABSPATH' ) || exit;
if ( defined( 'SENDERZZ_AFFILIATE_CLASSES_LOADED' ) ) return;
define( 'SENDERZZ_AFFILIATE_CLASSES_LOADED', true );

// ─────────────────────────────────────────────────────────────────────────────
// 1. HELPERS
// ─────────────────────────────────────────────────────────────────────────────

/**
 * Retorna classes de entrega (WC product_shipping_class) do produtor,
 * consultando senderzz_portal_user_classes (multi-class) + legado shipping_class_id.
 */
function sz_afc_get_producer_class_ids( int $producer_id ): array {
    global $wpdb;
    if ( $producer_id <= 0 ) return [];

    // Resolve portal_user_id do produtor (pode vir como wp_user_id ou portal id)
    $portal_ids = function_exists( 'sz_aff_possible_producer_ids' )
        ? sz_aff_possible_producer_ids( $producer_id )
        : [ $producer_id ];

    $mc_table   = $wpdb->prefix . 'senderzz_portal_user_classes';
    $user_table = $wpdb->prefix . 'senderzz_portal_users';

    $class_ids = [];

    // Tenta via tabela multi-class
    $mc_exists = $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $mc_table ) ) === $mc_table;
    if ( $mc_exists && ! empty( $portal_ids ) ) {
        $ph   = implode( ',', array_fill( 0, count( $portal_ids ), '%d' ) );
        // Busca por portal_user_id (direto) e por portal users com esses wp_user_ids
        $pu_ids = $wpdb->get_col( $wpdb->prepare(
            "SELECT id FROM {$user_table} WHERE id IN ({$ph}) OR wp_user_id IN ({$ph})",
            array_merge( $portal_ids, $portal_ids )
        ) );
        if ( $pu_ids ) {
            $ph2   = implode( ',', array_fill( 0, count( $pu_ids ), '%d' ) );
            $rows  = $wpdb->get_col( $wpdb->prepare(
                "SELECT shipping_class_id FROM {$mc_table} WHERE portal_user_id IN ({$ph2}) AND shipping_class_id > 0",
                $pu_ids
            ) );
            $class_ids = array_map( 'intval', $rows ?: [] );
        }
    }

    // Fallback: campo legado
    if ( empty( $class_ids ) && ! empty( $portal_ids ) ) {
        $ph = implode( ',', array_fill( 0, count( $portal_ids ), '%d' ) );
        $rows = $wpdb->get_col( $wpdb->prepare(
            "SELECT shipping_class_id FROM {$user_table}
             WHERE (id IN ({$ph}) OR wp_user_id IN ({$ph})) AND shipping_class_id > 0",
            array_merge( $portal_ids, $portal_ids )
        ) );
        $class_ids = array_map( 'intval', $rows ?: [] );
    }

    return array_values( array_unique( array_filter( $class_ids ) ) );
}

/**
 * Retorna classes de entrega associadas a um portal_user_id de afiliado.
 * Usa a tabela sz_affiliate_classes (criada por este módulo).
 */
function sz_afc_get_affiliate_class_ids( int $affiliate_id ): array {
    global $wpdb;
    $table = $wpdb->prefix . 'sz_affiliate_classes';
    if ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $table ) ) !== $table ) return [];
    $rows = $wpdb->get_col( $wpdb->prepare(
        "SELECT shipping_class_id FROM {$table} WHERE affiliate_id=%d AND active=1",
        $affiliate_id
    ) );
    return array_values( array_map( 'intval', $rows ?: [] ) );
}

/**
 * Label de uma shipping class do WooCommerce.
 */
function sz_afc_class_label( int $class_id ): string {
    if ( $class_id <= 0 ) return 'Sem classe';
    if ( function_exists( 'sz_aff_shipping_class_label' ) ) return sz_aff_shipping_class_label( $class_id );
    $term = get_term( $class_id, 'product_shipping_class' );
    return ( $term && ! is_wp_error( $term ) ) ? (string) $term->name : 'Classe #' . $class_id;
}

/**
 * Retorna todas as shipping classes do WC.
 */
function sz_afc_all_wc_classes(): array {
    $terms = get_terms( [ 'taxonomy' => 'product_shipping_class', 'hide_empty' => false ] );
    if ( is_wp_error( $terms ) || empty( $terms ) ) return [];
    $out = [];
    foreach ( $terms as $t ) $out[(int)$t->term_id] = (string)$t->name;
    return $out;
}

// ─────────────────────────────────────────────────────────────────────────────
// 2. INSTALL — tabela sz_affiliate_classes
// ─────────────────────────────────────────────────────────────────────────────

function sz_afc_install(): void {
    global $wpdb;
    require_once ABSPATH . 'wp-admin/includes/upgrade.php';
    $charset = $wpdb->get_charset_collate();
    $table   = $wpdb->prefix . 'sz_affiliate_classes';

    dbDelta( "CREATE TABLE {$table} (
        id                BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        affiliate_id      BIGINT UNSIGNED NOT NULL,
        producer_id       BIGINT UNSIGNED NOT NULL DEFAULT 0,
        shipping_class_id BIGINT UNSIGNED NOT NULL,
        active            TINYINT(1) NOT NULL DEFAULT 1,
        created_at        DATETIME NOT NULL,
        updated_at        DATETIME NULL,
        PRIMARY KEY (id),
        UNIQUE KEY uniq_aff_class (affiliate_id, shipping_class_id),
        KEY idx_producer  (producer_id),
        KEY idx_class     (shipping_class_id),
        KEY idx_active    (active)
    ) {$charset};" );

    // Garante coluna affiliate_visible em senderzz_checkout_links
    $cl_table = $wpdb->prefix . 'senderzz_checkout_links';
    $cl_exists = $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $cl_table ) );
    if ( $cl_exists === $cl_table ) {
        $cols = $wpdb->get_col( "SHOW COLUMNS FROM `{$cl_table}`", 0 );
        if ( ! in_array( 'affiliate_visible', (array) $cols, true ) ) {
            $wpdb->query( "ALTER TABLE `{$cl_table}` ADD COLUMN `affiliate_visible` TINYINT(1) NOT NULL DEFAULT 0" );
        }
    }
}
add_action( 'init', function() {
    if ( get_option( 'sz_afc_db_ver' ) !== '1' ) {
        sz_afc_install();
        update_option( 'sz_afc_db_ver', '1' );
    }
}, 8 );

// ─────────────────────────────────────────────────────────────────────────────
// 3. AUTO-VINCULAÇÃO: ao afiliado ser criado/aprovado, herda classes do produtor
// ─────────────────────────────────────────────────────────────────────────────

/**
 * Vincula o afiliado a todas as classes do produtor.
 * Chamado ao criar vínculo (convite aceito) ou ao aprovar.
 */
function sz_afc_sync_affiliate_classes( int $affiliate_id, int $producer_id, bool $overwrite = false ): void {
    global $wpdb;
    if ( $affiliate_id <= 0 || $producer_id <= 0 ) return;

    sz_afc_install(); // garante tabela

    $class_ids = sz_afc_get_producer_class_ids( $producer_id );
    if ( empty( $class_ids ) ) return;

    $table = $wpdb->prefix . 'sz_affiliate_classes';
    $now   = current_time( 'mysql' );

    foreach ( $class_ids as $cid ) {
        $exists = (int) $wpdb->get_var( $wpdb->prepare(
            "SELECT id FROM {$table} WHERE affiliate_id=%d AND shipping_class_id=%d LIMIT 1",
            $affiliate_id, $cid
        ) );
        if ( $exists ) {
            if ( $overwrite ) {
                $wpdb->update( $table, [ 'active' => 1, 'updated_at' => $now ], [ 'id' => $exists ], [ '%d','%s' ], [ '%d' ] );
            }
        } else {
            $wpdb->insert( $table, [
                'affiliate_id'      => $affiliate_id,
                'producer_id'       => $producer_id,
                'shipping_class_id' => $cid,
                'active'            => 1,
                'created_at'        => $now,
            ], [ '%d','%d','%d','%d','%s' ] );
        }
    }

    // Libera checkouts do produtor desta classe para o afiliado
    sz_afc_sync_checkout_visibility( $producer_id, $class_ids );
}

/**
 * Garante que os checkouts do produtor com as classes dadas fiquem com affiliate_visible=1.
 */
function sz_afc_sync_checkout_visibility( int $producer_id, array $class_ids ): void {
    // affiliate_visible é por checkout, não por classe — libera todos os checkouts
    // do produtor que ainda não estão visíveis para afiliados.
    // Respeita a intenção do produtor: apenas ativa os que ainda são 0 (nunca tocados),
    // não reativa os que foram desativados intencionalmente (updated manualmente).
    // Neste módulo, ativamos todos na criação do vínculo. O produtor pode desativar depois.
    global $wpdb;
    $producer_ids = function_exists( 'sz_aff_possible_producer_ids' )
        ? sz_aff_possible_producer_ids( $producer_id )
        : [ $producer_id ];
    if ( empty( $producer_ids ) ) return;

    $cl_table = $wpdb->prefix . 'senderzz_checkout_links';
    if ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $cl_table ) ) !== $cl_table ) return;

    $cols = $wpdb->get_col( "SHOW COLUMNS FROM `{$cl_table}`", 0 );
    if ( ! in_array( 'affiliate_visible', (array) $cols, true ) ) return;

    $ph = implode( ',', array_fill( 0, count( $producer_ids ), '%d' ) );
    $where = "affiliate_visible = 0 AND (";
    $parts = [];
    $params = [];
    foreach ( [ 'user_id', 'producer_id' ] as $col ) {
        if ( in_array( $col, (array) $cols, true ) ) {
            $parts[]  = "`{$col}` IN ({$ph})";
            $params   = array_merge( $params, $producer_ids );
        }
    }
    if ( empty( $parts ) ) return;
    $where .= implode( ' OR ', $parts ) . ')';

    // Somente tipo não-motoboy
    if ( in_array( 'tipo', (array) $cols, true ) ) {
        $where .= " AND (tipo IS NULL OR tipo='' OR tipo='correio' OR tipo='expedicao' OR tipo='expedição')";
    }

    $wpdb->query( $wpdb->prepare( "UPDATE `{$cl_table}` SET affiliate_visible=1 WHERE {$where}", $params ) );
}

/**
 * Hook: após inserir um novo afiliado (convite aceito) — auto-vincula classes.
 * Integra com o flow de sz_aff_render_invite_page (que insere na sz_affiliates).
 */
add_action( 'sz_affiliate_created', function( int $affiliate_id, int $producer_id ) {
    sz_afc_sync_affiliate_classes( $affiliate_id, $producer_id );
}, 10, 2 );

/**
 * Hook: ao aprovar afiliado pendente — garante classes.
 */
add_action( 'sz_affiliate_approved', function( int $affiliate_id, int $producer_id ) {
    sz_afc_sync_affiliate_classes( $affiliate_id, $producer_id, true );
}, 10, 2 );

/**
 * Intercepta insert na sz_affiliates para disparar o hook sz_affiliate_created.
 * Como o código de convite usa $wpdb->insert diretamente, monitoramos via
 * override do wpdb::insert com um filter no init — abordagem mais limpa:
 * hookar na action woocommerce_checkout_update_order_meta é agressivo demais.
 *
 * Alternativa direta: patch na função sz_aff_render_invite_page via filter.
 * Usamos um approach de late-bind: verificamos na action 'init' (priority 25)
 * se existem afiliados sem classes e sincronizamos.
 */
add_action( 'init', function() {
    // Não rodar em toda request — só quando há afiliados ativos sem classes associadas.
    // Evita overhead desnecessário.
    if ( ! is_admin() && ! wp_doing_ajax() ) return;

    global $wpdb;
    $aff_table = $wpdb->prefix . 'sz_affiliates';
    $cls_table = $wpdb->prefix . 'sz_affiliate_classes';
    if ( ! $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $cls_table ) ) ) return;
    if ( ! $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $aff_table ) ) ) return;

    // Afiliados ativos que ainda não têm nenhuma classe vinculada
    $missing = $wpdb->get_results(
        "SELECT a.id, a.producer_id FROM {$aff_table} a
         LEFT JOIN {$cls_table} c ON c.affiliate_id = a.id
         WHERE a.status = 'active' AND a.deleted_at IS NULL AND c.id IS NULL
         LIMIT 50",
        ARRAY_A
    );
    if ( empty( $missing ) ) return;

    foreach ( (array) $missing as $row ) {
        sz_afc_sync_affiliate_classes( (int) $row['id'], (int) $row['producer_id'] );
    }
}, 25 );

// ─────────────────────────────────────────────────────────────────────────────
// 4. AJAX — ações do painel admin
// ─────────────────────────────────────────────────────────────────────────────

add_action( 'wp_ajax_sz_afc_action', 'sz_afc_handle_ajax' );

function sz_afc_handle_ajax(): void {
    if ( ! current_user_can( 'manage_woocommerce' ) ) wp_send_json_error( [ 'msg' => 'Sem permissão.' ], 403 );
    if ( ! check_ajax_referer( 'sz_afc_admin', '_nonce', false ) ) wp_send_json_error( [ 'msg' => 'Nonce inválido.' ], 403 );

    global $wpdb;
    $act          = sanitize_key( $_POST['act'] ?? '' );
    $affiliate_id = absint( $_POST['affiliate_id'] ?? 0 );
    $class_id     = absint( $_POST['class_id'] ?? 0 );
    $now          = current_time( 'mysql' );
    $cls_table    = $wpdb->prefix . 'sz_affiliate_classes';
    $aff_table    = $wpdb->prefix . 'sz_affiliates';

    switch ( $act ) {

        // Ativar/suspender associação afiliado↔classe
        case 'toggle_class':
            if ( ! $affiliate_id || ! $class_id ) wp_send_json_error( [ 'msg' => 'Parâmetros inválidos.' ] );
            $active = absint( $_POST['active'] ?? 1 );
            $row_id = (int) $wpdb->get_var( $wpdb->prepare(
                "SELECT id FROM {$cls_table} WHERE affiliate_id=%d AND shipping_class_id=%d LIMIT 1",
                $affiliate_id, $class_id
            ) );
            if ( $row_id ) {
                $wpdb->update( $cls_table, [ 'active' => $active, 'updated_at' => $now ], [ 'id' => $row_id ], [ '%d','%s' ], [ '%d' ] );
            } else {
                $prod_id = (int) $wpdb->get_var( $wpdb->prepare( "SELECT producer_id FROM {$aff_table} WHERE id=%d LIMIT 1", $affiliate_id ) );
                $wpdb->insert( $cls_table, [
                    'affiliate_id' => $affiliate_id, 'producer_id' => $prod_id,
                    'shipping_class_id' => $class_id, 'active' => $active, 'created_at' => $now,
                ], [ '%d','%d','%d','%d','%s' ] );
            }
            wp_send_json_success( [ 'msg' => $active ? 'Classe liberada.' : 'Classe suspensa.' ] );

        // Remover associação completamente
        case 'delete_class':
            if ( ! $affiliate_id || ! $class_id ) wp_send_json_error( [ 'msg' => 'Parâmetros inválidos.' ] );
            $wpdb->delete( $cls_table, [ 'affiliate_id' => $affiliate_id, 'shipping_class_id' => $class_id ], [ '%d','%d' ] );
            wp_send_json_success( [ 'msg' => 'Associação removida.' ] );

        // Adicionar nova classe a um afiliado
        case 'add_class':
            if ( ! $affiliate_id || ! $class_id ) wp_send_json_error( [ 'msg' => 'Parâmetros inválidos.' ] );
            $prod_id = (int) $wpdb->get_var( $wpdb->prepare( "SELECT producer_id FROM {$aff_table} WHERE id=%d LIMIT 1", $affiliate_id ) );
            $wpdb->replace( $cls_table, [
                'affiliate_id' => $affiliate_id, 'producer_id' => $prod_id,
                'shipping_class_id' => $class_id, 'active' => 1, 'created_at' => $now, 'updated_at' => $now,
            ], [ '%d','%d','%d','%d','%s','%s' ] );
            wp_send_json_success( [ 'msg' => 'Classe adicionada.', 'label' => sz_afc_class_label( $class_id ) ] );

        // Resincronizar classes do produtor para um afiliado específico
        case 'sync_from_producer':
            if ( ! $affiliate_id ) wp_send_json_error( [ 'msg' => 'Parâmetros inválidos.' ] );
            $prod_id = (int) $wpdb->get_var( $wpdb->prepare( "SELECT producer_id FROM {$aff_table} WHERE id=%d LIMIT 1", $affiliate_id ) );
            sz_afc_sync_affiliate_classes( $affiliate_id, $prod_id, true );
            $count = (int) $wpdb->get_var( $wpdb->prepare( "SELECT COUNT(*) FROM {$cls_table} WHERE affiliate_id=%d AND active=1", $affiliate_id ) );
            wp_send_json_success( [ 'msg' => "Sincronizado! {$count} classe(s) ativa(s)." ] );

        // Toggle affiliate_visible de um checkout
        case 'toggle_checkout':
            $checkout_id = absint( $_POST['checkout_id'] ?? 0 );
            $visible     = absint( $_POST['visible'] ?? 1 );
            if ( ! $checkout_id ) wp_send_json_error( [ 'msg' => 'Parâmetros inválidos.' ] );
            $cl_table = $wpdb->prefix . 'senderzz_checkout_links';
            $wpdb->update( $cl_table, [ 'affiliate_visible' => $visible ], [ 'id' => $checkout_id ], [ '%d' ], [ '%d' ] );
            wp_send_json_success( [ 'msg' => $visible ? 'Checkout liberado para afiliados.' : 'Checkout ocultado.' ] );

        // Excluir afiliado completamente
        case 'delete_affiliate':
            if ( ! $affiliate_id ) wp_send_json_error( [ 'msg' => 'Parâmetros inválidos.' ] );
            $wpdb->update( $aff_table, [ 'status' => 'pending', 'deleted_at' => $now ], [ 'id' => $affiliate_id ], [ '%s','%s' ], [ '%d' ] );
            $wpdb->delete( $cls_table, [ 'affiliate_id' => $affiliate_id ], [ '%d' ] );
            wp_send_json_success( [ 'msg' => 'Afiliado removido.' ] );

        default:
            wp_send_json_error( [ 'msg' => 'Ação desconhecida.' ] );
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// 5. RENDER — aba "Classes" no admin Senderzz Afiliados
// ─────────────────────────────────────────────────────────────────────────────

function sz_afc_render_admin_tab(): void {
    global $wpdb;

    sz_afc_install();

    $aff_table = $wpdb->prefix . 'sz_affiliates';
    $cls_table = $wpdb->prefix . 'sz_affiliate_classes';
    $pu_table  = $wpdb->prefix . 'senderzz_portal_users';
    $cl_table  = $wpdb->prefix . 'senderzz_checkout_links';
    $nonce     = wp_create_nonce( 'sz_afc_admin' );
    $ajax_url  = admin_url( 'admin-ajax.php' );

    // ── Dados: afiliados com classes ─────────────────────────────────────────
    $affiliates = $wpdb->get_results(
        "SELECT a.id, a.producer_id, a.status, a.commission_pct,
                u.display_name, u.user_email,
                pu.name AS portal_name, pu.email AS portal_email
         FROM {$aff_table} a
         LEFT JOIN {$wpdb->users} u  ON u.ID  = a.user_id
         LEFT JOIN {$pu_table} pu    ON (pu.wp_user_id = a.user_id OR pu.email = u.user_email)
         WHERE a.status IN ('active','pending') AND a.deleted_at IS NULL
         ORDER BY FIELD(a.status,'pending','active'), a.id DESC
         LIMIT 300",
        ARRAY_A
    ) ?: [];

    // Pré-carregar classes de todos os afiliados
    $aff_ids   = array_filter( array_map( fn($r) => (int)$r['id'], $affiliates ) );
    $class_map = []; // [ affiliate_id => [ [ shipping_class_id, active ], ... ] ]

    if ( $aff_ids ) {
        $ph   = implode( ',', $aff_ids );
        $rows = $wpdb->get_results(
            "SELECT affiliate_id, shipping_class_id, active FROM {$cls_table}
             WHERE affiliate_id IN ({$ph})
             ORDER BY affiliate_id, shipping_class_id",
            ARRAY_A
        ) ?: [];
        foreach ( $rows as $r ) {
            $class_map[(int)$r['affiliate_id']][] = [ 'class_id' => (int)$r['shipping_class_id'], 'active' => (int)$r['active'] ];
        }
    }

    // ── Dados: checkouts com flag affiliate_visible ───────────────────────────
    $checkouts = [];
    $cl_exists = $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $cl_table ) );
    if ( $cl_exists === $cl_table ) {
        $cl_cols = $wpdb->get_col( "SHOW COLUMNS FROM `{$cl_table}`", 0 );
        if ( in_array( 'affiliate_visible', (array) $cl_cols, true ) ) {
            $checkouts = $wpdb->get_results(
                "SELECT id, name, components_text, affiliate_visible,
                        " . ( in_array( 'user_id', (array) $cl_cols, true ) ? 'user_id' : '0 AS user_id' ) . ",
                        " . ( in_array( 'producer_id', (array) $cl_cols, true ) ? 'producer_id' : '0 AS producer_id' ) . "
                 FROM `{$cl_table}`
                 WHERE tipo IS NULL OR tipo='' OR tipo='correio' OR tipo='expedicao' OR tipo='expedição'
                 ORDER BY affiliate_visible DESC, id DESC
                 LIMIT 200",
                ARRAY_A
            ) ?: [];
        }
    }

    // Todas as classes WC
    $all_classes = sz_afc_all_wc_classes();

    // Agrupar afiliados por classe
    $by_class = []; // [ class_id => [ aff_id => aff_data ] ]
    $no_class = [];
    foreach ( $affiliates as $aff ) {
        $aid     = (int) $aff['id'];
        $classes = $class_map[$aid] ?? [];
        if ( empty( $classes ) ) {
            $no_class[] = $aff;
        } else {
            foreach ( $classes as $c ) {
                $by_class[$c['class_id']][$aid] = array_merge( $aff, [ '_active' => $c['active'] ] );
            }
        }
    }

    ?>
    <div id="sz-afc-root" style="font-family:var(--sz-font);color:#0f172a;max-width:1400px">

    <style>
        #sz-afc-root *{box-sizing:border-box}
        #sz-afc-root .sz-afc-section{background:#fff;border:1px solid #e5eaf3;border-radius:20px;padding:22px 24px;margin-bottom:18px}
        #sz-afc-root .sz-afc-section-title{font-size:var(--sz-text-lg);font-weight:700;letter-spacing:-.02em;color:#0f172a;margin:0 0 4px}
        #sz-afc-root .sz-afc-section-sub{font-size:var(--sz-text-base);color:#64748b;margin:0 0 16px}
        #sz-afc-root .sz-afc-class-block{border:1px solid #e5eaf3;border-radius:16px;overflow:hidden;margin-bottom:14px}
        #sz-afc-root .sz-afc-class-head{display:flex;align-items:center;gap:10px;padding:12px 16px;background:#f8fafc;border-bottom:1px solid #e5eaf3}
        #sz-afc-root .sz-afc-class-badge{display:inline-flex;align-items:center;gap:6px;background:#fff1e8;color:#9a3412;border:1px solid #fed7aa;border-radius:999px;padding:5px 13px;font-size:var(--sz-text-meta);font-weight:700}
        #sz-afc-root .sz-afc-class-badge .dot{width:7px;height:7px;border-radius:50%;background:#E8650A;flex-shrink:0}
        #sz-afc-root .sz-afc-aff-table{width:100%;border-collapse:collapse}
        #sz-afc-root .sz-afc-aff-table th{padding:9px 14px;text-align:left;font-size:var(--sz-text-sm);font-weight:700;text-transform:none;letter-spacing:.02em;color:#6b7280;border-bottom:1px solid #edf1f7}
        #sz-afc-root .sz-afc-aff-table td{padding:11px 14px;font-size:var(--sz-text-base);border-bottom:1px solid #f1f5f9;vertical-align:middle}
        #sz-afc-root .sz-afc-aff-table tr:last-child td{border-bottom:0}
        #sz-afc-root .sz-afc-aff-table tr:hover td{background:#fafbfc}
        #sz-afc-root .sz-afc-name{font-weight:700;color:#111827}
        #sz-afc-root .sz-afc-muted{color:#64748b;font-size:var(--sz-text-meta)}
        #sz-afc-root .sz-afc-badge{display:inline-flex;align-items:center;padding:3px 10px;border-radius:999px;font-size:var(--sz-text-sm);font-weight:700}
        #sz-afc-root .sz-afc-badge.active{background:#dcfce7;color:#166534}
        #sz-afc-root .sz-afc-badge.pending{background:#fef9c3;color:#854d0e}
        #sz-afc-root .sz-afc-badge.suspended{background:#f1f5f9;color:#64748b}
        #sz-afc-root .sz-afc-btn{display:inline-flex;align-items:center;justify-content:center;gap:5px;height:32px;padding:0 12px;border:0;border-radius:9px;font-size:var(--sz-text-meta);font-weight:700;cursor:pointer;white-space:nowrap;transition:filter .15s}
        #sz-afc-root .sz-afc-btn:hover{filter:brightness(.94)}
        #sz-afc-root .sz-afc-btn:disabled{opacity:.45;cursor:not-allowed}
        #sz-afc-root .sz-afc-btn.primary{background:#E8650A;color:#fff}
        #sz-afc-root .sz-afc-btn.ghost{background:#f1f5f9;color:#334155;border:1px solid #e2e8f0}
        #sz-afc-root .sz-afc-btn.danger{background:#fff;color:#dc2626;border:1px solid #fca5a5}
        #sz-afc-root .sz-afc-btn.warn{background:#fff7ed;color:#c2410c;border:1px solid #fed7aa}
        #sz-afc-root .sz-afc-btn.green{background:#dcfce7;color:#166534;border:1px solid #bbf7d0}
        #sz-afc-root .sz-afc-actions{display:flex;gap:6px;flex-wrap:wrap;align-items:center}
        #sz-afc-root .sz-afc-add-form{display:flex;gap:8px;align-items:center;flex-wrap:wrap;padding:10px 14px;background:#f8fafc;border-top:1px solid #edf1f7}
        #sz-afc-root .sz-afc-add-form select{height:34px;border:1px solid #d1d5db;border-radius:9px;padding:0 10px;font-size:var(--sz-text-meta);font-weight:700;color:#334155;background:#fff;min-width:180px}
        #sz-afc-root .sz-afc-toast{position:fixed;right:24px;bottom:24px;background:#111827;color:#fff;border-radius:13px;padding:11px 16px;font-weight:700;font-size:var(--sz-text-base);box-shadow:0 16px 40px rgba(15,23,42,.28);z-index:99999;animation:sz-afc-fadein .2s}
        @keyframes sz-afc-fadein{from{opacity:0;transform:translateY(6px)}to{opacity:1;transform:none}}
        #sz-afc-root .sz-afc-noclass{background:#fef9c3;border:1px solid #fde047;border-radius:13px;padding:12px 16px;font-size:var(--sz-text-base);color:#713f12;margin-bottom:14px}
        #sz-afc-root .sz-afc-empty{padding:14px 16px;color:#64748b;font-size:var(--sz-text-base);font-style:italic}
        #sz-afc-root .sz-afc-tabs{display:flex;gap:0;background:#f1f5f9;border-radius:13px;padding:3px;margin-bottom:20px;width:fit-content}
        #sz-afc-root .sz-afc-tab{height:38px;border:0;background:transparent;border-radius:10px;padding:0 20px;font-size:var(--sz-text-base);font-weight:700;cursor:pointer;color:#475569}
        #sz-afc-root .sz-afc-tab.active{background:#fff;color:#E8650A;box-shadow:0 2px 8px rgba(15,23,42,.08)}
        #sz-afc-root .sz-afc-panel{display:none}
        #sz-afc-root .sz-afc-panel.active{display:block}
        #sz-afc-root .sz-afc-checkout-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(280px,1fr));gap:10px}
        #sz-afc-root .sz-afc-checkout-card{border:1px solid #e5eaf3;border-radius:14px;padding:14px 16px;background:#fff;display:flex;align-items:center;justify-content:space-between;gap:12px}
        #sz-afc-root .sz-afc-checkout-card.visible{border-color:#bbf7d0;background:#f0fdf4}
        #sz-afc-root .sz-afc-toggle{position:relative;display:inline-block;width:40px;height:22px;flex-shrink:0}
        #sz-afc-root .sz-afc-toggle input{opacity:0;width:0;height:0;position:absolute}
        #sz-afc-root .sz-afc-slider{position:absolute;inset:0;border-radius:999px;background:#d1d5db;cursor:pointer;transition:.2s}
        #sz-afc-root .sz-afc-slider:before{content:'';position:absolute;top:3px;left:3px;width:16px;height:16px;border-radius:50%;background:#fff;transition:.2s;box-shadow:0 1px 3px rgba(15,23,42,.2)}
        #sz-afc-root .sz-afc-toggle input:checked+.sz-afc-slider{background:#E8650A}
        #sz-afc-root .sz-afc-toggle input:checked+.sz-afc-slider:before{transform:translateX(18px)}
        #sz-afc-root .sz-afc-stat-row{display:grid;grid-template-columns:repeat(3,1fr);gap:12px;margin-bottom:18px}
        #sz-afc-root .sz-afc-stat{background:#f8fafc;border:1px solid #e5eaf3;border-radius:14px;padding:14px 16px}
        #sz-afc-root .sz-afc-stat-val{font-size:var(--sz-text-3xl);font-weight:700;color:#111827;line-height:1.1}
        #sz-afc-root .sz-afc-stat-lbl{font-size:var(--sz-text-sm);font-weight:700;color:#64748b;text-transform:none;letter-spacing:.02em;margin-top:3px}
        @media(max-width:900px){
            #sz-afc-root .sz-afc-stat-row{grid-template-columns:1fr 1fr}
            #sz-afc-root .sz-afc-checkout-grid{grid-template-columns:1fr}
            #sz-afc-root .sz-afc-aff-table{display:block;overflow-x:auto}
        }
    </style>

    <script>
    (function(){
        var NONCE='<?php echo esc_js( $nonce ); ?>';
        var AJAX='<?php echo esc_js( $ajax_url ); ?>';

        window.szAfcToast=function(msg,ok){
            var old=document.querySelector('.sz-afc-toast'); if(old) old.remove();
            var d=document.createElement('div'); d.className='sz-afc-toast';
            d.style.borderLeft='4px solid '+(ok===false?'#ef4444':'#E8650A');
            d.textContent=msg; document.body.appendChild(d);
            setTimeout(function(){d.remove()},2400);
        };

        window.szAfcPost=function(data,btn,onSuccess){
            if(btn) btn.disabled=true;
            var fd=new FormData();
            fd.append('action','sz_afc_action'); fd.append('_nonce',NONCE);
            Object.keys(data).forEach(function(k){fd.append(k,data[k]);});
            fetch(AJAX,{method:'POST',body:fd,credentials:'same-origin'})
            .then(function(r){return r.json();})
            .then(function(d){
                szAfcToast(d.data&&d.data.msg?d.data.msg:(d.success?'Feito!':'Erro!'), d.success);
                if(d.success && typeof onSuccess==='function') onSuccess(d.data);
                if(btn) btn.disabled=false;
            }).catch(function(){szAfcToast('Erro de conexão.',false);if(btn)btn.disabled=false;});
        };

        window.szAfcTab=function(tab,btn){
            document.querySelectorAll('#sz-afc-root .sz-afc-tab').forEach(function(b){b.classList.remove('active');});
            document.querySelectorAll('#sz-afc-root .sz-afc-panel').forEach(function(p){p.classList.remove('active');});
            if(btn) btn.classList.add('active');
            var panel=document.getElementById('sz-afc-panel-'+tab);
            if(panel) panel.classList.add('active');
        };

        window.szAfcToggleClass=function(btn,affId,classId,currentActive){
            var newActive=currentActive?0:1;
            szAfcPost({act:'toggle_class',affiliate_id:affId,class_id:classId,active:newActive},btn,function(){
                btn.textContent=newActive?'Suspender':'Liberar';
                btn.className=btn.className.replace(/\b(warn|green)\b/g,newActive?'warn':'green');
                var badge=document.getElementById('sz-afc-badge-'+affId+'-'+classId);
                if(badge){badge.textContent=newActive?'Ativa':'Suspensa';badge.className='sz-afc-badge '+(newActive?'active':'suspended');}
                btn.onclick=function(){szAfcToggleClass(btn,affId,classId,newActive);};
            });
        };

        window.szAfcDeleteClass=function(btn,affId,classId){
            if(!confirm('Remover esta classe do afiliado?')) return;
            szAfcPost({act:'delete_class',affiliate_id:affId,class_id:classId},btn,function(){
                var row=document.getElementById('sz-afc-row-'+affId+'-'+classId);
                if(row) row.remove();
            });
        };

        window.szAfcDeleteAffiliate=function(btn,affId){
            if(!confirm('Excluir este afiliado e todas suas classes?')) return;
            szAfcPost({act:'delete_affiliate',affiliate_id:affId},btn,function(){
                document.querySelectorAll('[data-aff-id="'+affId+'"]').forEach(function(el){el.closest('tr')&&el.closest('tr').remove();});
                szAfcToast('Afiliado excluído.',true);
            });
        };

        window.szAfcAddClass=function(btn,affId){
            var sel=document.getElementById('sz-afc-add-sel-'+affId);
            if(!sel||!sel.value) return szAfcToast('Selecione uma classe.',false);
            var classId=sel.value, classLabel=sel.options[sel.selectedIndex].text;
            szAfcPost({act:'add_class',affiliate_id:affId,class_id:classId},btn,function(data){
                var tbody=document.getElementById('sz-afc-tbody-'+affId);
                if(!tbody) return;
                var lbl=data&&data.label?data.label:classLabel;
                var tr=document.createElement('tr');
                tr.id='sz-afc-row-'+affId+'-'+classId;
                tr.innerHTML='<td>'+lbl+'</td>'
                    +'<td><span id="sz-afc-badge-'+affId+'-'+classId+'" class="sz-afc-badge active">Ativa</span></td>'
                    +'<td><div class="sz-afc-actions">'
                    +'<button class="sz-afc-btn warn" onclick="szAfcToggleClass(this,'+affId+','+classId+',1)">Suspender</button>'
                    +'<button class="sz-afc-btn danger" onclick="szAfcDeleteClass(this,'+affId+','+classId+')">Excluir</button>'
                    +'</div></td>';
                tbody.appendChild(tr);
                sel.value='';
            });
        };

        window.szAfcSyncProducer=function(btn,affId){
            szAfcPost({act:'sync_from_producer',affiliate_id:affId},btn,function(){
                setTimeout(function(){location.reload();},800);
            });
        };

        window.szAfcToggleCheckout=function(toggle,checkoutId){
            var visible=toggle.checked?1:0;
            var card=toggle.closest('.sz-afc-checkout-card');
            szAfcPost({act:'toggle_checkout',checkout_id:checkoutId,visible:visible},null,function(){
                if(card) card.classList.toggle('visible',visible===1);
            });
        };
    })();
    </script>

    <?php
    // ── Stats
    $total_affs    = count( $affiliates );
    $total_active  = count( array_filter( $affiliates, fn($a) => $a['status'] === 'active' ) );
    $total_pending = $total_affs - $total_active;
    $total_classes = count( $all_classes );
    ?>

    <div class="sz-afc-stat-row">
        <div class="sz-afc-stat"><div class="sz-afc-stat-val"><?php echo $total_active; ?></div><div class="sz-afc-stat-lbl">Afiliados ativos</div></div>
        <div class="sz-afc-stat"><div class="sz-afc-stat-val"><?php echo $total_pending; ?></div><div class="sz-afc-stat-lbl">Pendentes</div></div>
        <div class="sz-afc-stat"><div class="sz-afc-stat-val"><?php echo $total_classes; ?></div><div class="sz-afc-stat-lbl">Classes WC disponíveis</div></div>
    </div>

    <div class="sz-afc-tabs">
        <button class="sz-afc-tab active" onclick="szAfcTab('classes',this)">Por classe de entrega</button>
        <button class="sz-afc-tab" onclick="szAfcTab('affiliates',this)">Por afiliado</button>
        <button class="sz-afc-tab" onclick="szAfcTab('checkouts',this)">Checkouts visíveis</button>
    </div>

    <?php /* ═══ PAINEL 1: Por classe ═══ */ ?>
    <div id="sz-afc-panel-classes" class="sz-afc-panel active">
        <div class="sz-afc-section">
            <div class="sz-afc-section-title">🏷️ Afiliados por classe de entrega</div>
            <div class="sz-afc-section-sub">Veja quais afiliados têm acesso a cada classe e gerencie individualmente.</div>

            <?php if ( ! empty( $no_class ) ) : ?>
            <div class="sz-afc-noclass">
                ⚠️ <strong><?php echo count($no_class); ?> afiliado(s)</strong> sem nenhuma classe vinculada.
                Acesse a aba <em>Por afiliado</em> para sincronizar.
            </div>
            <?php endif; ?>

            <?php if ( empty( $by_class ) ) : ?>
                <div class="sz-afc-empty">Nenhum afiliado com classes vinculadas. Use "Sincronizar do produtor" na aba <em>Por afiliado</em>.</div>
            <?php else :
                // Ordenar classes por nome
                uksort( $by_class, fn($a,$b) => strcmp( sz_afc_class_label($a), sz_afc_class_label($b) ) );
                foreach ( $by_class as $cid => $aff_list ) :
                    $class_label = sz_afc_class_label( $cid );
                    $active_count = count( array_filter( $aff_list, fn($a) => $a['_active'] ) );
            ?>
            <div class="sz-afc-class-block">
                <div class="sz-afc-class-head">
                    <span class="sz-afc-class-badge"><span class="dot"></span><?php echo esc_html($class_label); ?></span>
                    <span style="font-size:var(--sz-text-meta);color:#64748b"><?php echo count($aff_list); ?> afiliado(s) · <?php echo $active_count; ?> ativo(s)</span>
                </div>
                <table class="sz-afc-aff-table">
                    <thead><tr>
                        <th>Afiliado</th>
                        <th>E-mail</th>
                        <th>Status afiliado</th>
                        <th>Acesso classe</th>
                        <th>Ações</th>
                    </tr></thead>
                    <tbody>
                    <?php foreach ( $aff_list as $aid => $aff ) :
                        $name  = trim( $aff['display_name'] ?: $aff['portal_name'] ?: '' ) ?: 'Afiliado #'.$aid;
                        $email = $aff['user_email'] ?: $aff['portal_email'] ?: '—';
                        $is_active_class = (bool)$aff['_active'];
                    ?>
                    <tr id="sz-afc-row-<?php echo $aid; ?>-<?php echo $cid; ?>" data-aff-id="<?php echo $aid; ?>">
                        <td><div class="sz-afc-name"><?php echo esc_html($name); ?></div><div class="sz-afc-muted">#<?php echo $aid; ?></div></td>
                        <td class="sz-afc-muted"><?php echo esc_html($email); ?></td>
                        <td><span class="sz-afc-badge <?php echo $aff['status']; ?>"><?php echo $aff['status']==='active'?'Ativo':'Pendente'; ?></span></td>
                        <td><span id="sz-afc-badge-<?php echo $aid; ?>-<?php echo $cid; ?>" class="sz-afc-badge <?php echo $is_active_class ? 'active' : 'suspended'; ?>"><?php echo $is_active_class ? 'Ativa' : 'Suspensa'; ?></span></td>
                        <td><div class="sz-afc-actions">
                            <button class="sz-afc-btn <?php echo $is_active_class ? 'warn' : 'green'; ?>"
                                onclick="szAfcToggleClass(this,<?php echo $aid; ?>,<?php echo $cid; ?>,<?php echo (int)$is_active_class; ?>)">
                                <?php echo $is_active_class ? 'Suspender' : 'Liberar'; ?>
                            </button>
                            <button class="sz-afc-btn danger"
                                onclick="szAfcDeleteClass(this,<?php echo $aid; ?>,<?php echo $cid; ?>)">Remover</button>
                        </div></td>
                    </tr>
                    <?php endforeach; ?>
                    </tbody>
                </table>
            </div>
            <?php endforeach; endif; ?>
        </div>
    </div>

    <?php /* ═══ PAINEL 2: Por afiliado ═══ */ ?>
    <div id="sz-afc-panel-affiliates" class="sz-afc-panel">
        <div class="sz-afc-section">
            <div class="sz-afc-section-title">👤 Gestão por afiliado</div>
            <div class="sz-afc-section-sub">Veja as classes de cada afiliado, adicione, suspenda, remova ou sincronize com o produtor.</div>

            <?php if ( empty( $affiliates ) ) : ?>
                <div class="sz-afc-empty">Nenhum afiliado cadastrado.</div>
            <?php else : foreach ( $affiliates as $aff ) :
                $aid   = (int) $aff['id'];
                $name  = trim( $aff['display_name'] ?: $aff['portal_name'] ?: '' ) ?: 'Afiliado #'.$aid;
                $email = $aff['user_email'] ?: $aff['portal_email'] ?: '—';
                $aff_classes = $class_map[$aid] ?? [];
                $prod_label = sz_aff_get_producer_display_name( (int)$aff['producer_id'] );
            ?>
            <div class="sz-afc-class-block" style="margin-bottom:14px">
                <div class="sz-afc-class-head" style="justify-content:space-between">
                    <div style="display:flex;align-items:center;gap:10px">
                        <span class="sz-afc-badge <?php echo $aff['status']==='active'?'active':'pending'; ?>"><?php echo $aff['status']==='active'?'Ativo':'Pendente'; ?></span>
                        <div>
                            <div style="font-weight:700;font-size:var(--sz-text-md);color:#111827"><?php echo esc_html($name); ?></div>
                            <div style="font-size:var(--sz-text-meta);color:#64748b"><?php echo esc_html($email); ?> · Produtor: <?php echo esc_html($prod_label); ?></div>
                        </div>
                    </div>
                    <div class="sz-afc-actions">
                        <button class="sz-afc-btn ghost" onclick="szAfcSyncProducer(this,<?php echo $aid; ?>)">🔄 Sincronizar do produtor</button>
                        <button class="sz-afc-btn danger" onclick="szAfcDeleteAffiliate(this,<?php echo $aid; ?>)">🗑 Excluir afiliado</button>
                    </div>
                </div>

                <?php if ( empty( $aff_classes ) ) : ?>
                    <div class="sz-afc-empty">Sem classes vinculadas. Clique em "Sincronizar do produtor" para herdar automaticamente.</div>
                <?php else : ?>
                <table class="sz-afc-aff-table">
                    <thead><tr><th>Classe de entrega</th><th>Status</th><th>Ações</th></tr></thead>
                    <tbody id="sz-afc-tbody-<?php echo $aid; ?>">
                    <?php foreach ( $aff_classes as $c ) :
                        $cid = $c['class_id'];
                        $is_active_class = (bool)$c['active'];
                    ?>
                    <tr id="sz-afc-row-<?php echo $aid; ?>-<?php echo $cid; ?>">
                        <td style="font-weight:700"><?php echo esc_html( sz_afc_class_label($cid) ); ?></td>
                        <td><span id="sz-afc-badge-<?php echo $aid; ?>-<?php echo $cid; ?>" class="sz-afc-badge <?php echo $is_active_class ? 'active' : 'suspended'; ?>"><?php echo $is_active_class ? 'Ativa' : 'Suspensa'; ?></span></td>
                        <td><div class="sz-afc-actions">
                            <button class="sz-afc-btn <?php echo $is_active_class ? 'warn' : 'green'; ?>"
                                onclick="szAfcToggleClass(this,<?php echo $aid; ?>,<?php echo $cid; ?>,<?php echo (int)$is_active_class; ?>)">
                                <?php echo $is_active_class ? 'Suspender' : 'Liberar'; ?>
                            </button>
                            <button class="sz-afc-btn danger" onclick="szAfcDeleteClass(this,<?php echo $aid; ?>,<?php echo $cid; ?>)">Remover</button>
                        </div></td>
                    </tr>
                    <?php endforeach; ?>
                    </tbody>
                </table>
                <?php endif; ?>

                <?php if ( ! empty( $all_classes ) ) : ?>
                <div class="sz-afc-add-form">
                    <span style="font-size:var(--sz-text-meta);font-weight:700;color:#64748b">Adicionar classe:</span>
                    <select id="sz-afc-add-sel-<?php echo $aid; ?>">
                        <option value="">— selecionar —</option>
                        <?php foreach ( $all_classes as $cid => $clabel ) : ?>
                            <option value="<?php echo esc_attr($cid); ?>"><?php echo esc_html($clabel); ?></option>
                        <?php endforeach; ?>
                    </select>
                    <button class="sz-afc-btn primary" onclick="szAfcAddClass(this,<?php echo $aid; ?>)">+ Adicionar</button>
                </div>
                <?php endif; ?>
            </div>
            <?php endforeach; endif; ?>
        </div>
    </div>

    <?php /* ═══ PAINEL 3: Checkouts visíveis ═══ */ ?>
    <div id="sz-afc-panel-checkouts" class="sz-afc-panel">
        <div class="sz-afc-section">
            <div class="sz-afc-section-title">🔗 Checkouts visíveis para afiliados</div>
            <div class="sz-afc-section-sub">
                Ative ou desative quais checkouts os afiliados podem ver e usar nos seus links identificados.
                O toggle <strong>verde</strong> = visível para afiliados.
            </div>

            <?php if ( empty( $checkouts ) ) : ?>
                <div class="sz-afc-empty">Nenhum checkout encontrado. Verifique se a tabela <code>senderzz_checkout_links</code> existe e tem a coluna <code>affiliate_visible</code>.</div>
            <?php else : ?>
                <div class="sz-afc-checkout-grid">
                <?php foreach ( $checkouts as $co ) :
                    $visible   = (bool)(int)$co['affiliate_visible'];
                    $cname     = trim( $co['name'] ?: $co['components_text'] ?: '' ) ?: 'Checkout #'.$co['id'];
                    $prod_name = sz_aff_get_producer_display_name( (int)($co['producer_id'] ?: $co['user_id']) );
                ?>
                <div class="sz-afc-checkout-card <?php echo $visible ? 'visible' : ''; ?>">
                    <div style="min-width:0">
                        <div style="font-weight:700;font-size:var(--sz-text-base);overflow:hidden;text-overflow:ellipsis;white-space:nowrap"><?php echo esc_html($cname); ?></div>
                        <div style="font-size:var(--sz-text-sm);color:#64748b;margin-top:2px"><?php echo esc_html($prod_name); ?> · #<?php echo (int)$co['id']; ?></div>
                        <span class="sz-afc-badge <?php echo $visible ? 'active' : 'suspended'; ?>" style="margin-top:6px"><?php echo $visible ? 'Visível' : 'Oculto'; ?></span>
                    </div>
                    <label class="sz-afc-toggle" title="<?php echo $visible ? 'Ocultar para afiliados' : 'Liberar para afiliados'; ?>">
                        <input type="checkbox" <?php checked($visible); ?> onchange="szAfcToggleCheckout(this,<?php echo (int)$co['id']; ?>)">
                        <span class="sz-afc-slider"></span>
                    </label>
                </div>
                <?php endforeach; ?>
                </div>
            <?php endif; ?>
        </div>
    </div>

    </div><!-- #sz-afc-root -->
    <?php
}

// ─────────────────────────────────────────────────────────────────────────────
// 6. PONTO DE ENTRADA — chamado pelo Unified_Menu (tab afiliados)
// O Unified_Menu.php roteia ?page=senderzz&tab=afiliados → sz_afc_render_full_admin_page()
// Não registramos submenu separado para evitar conflito com Unified_Menu.
// ─────────────────────────────────────────────────────────────────────────────

function sz_afc_render_full_admin_page(): void {
    $tab = sanitize_key( $_GET['sz_tab'] ?? 'financeiro' );
    $page_url = admin_url( 'admin.php?page=senderzz-affiliates' );
    ?>
    <div class="wrap">
        <h1 style="margin-bottom:16px">Senderzz Afiliados</h1>
        <nav class="nav-tab-wrapper" style="margin-bottom:20px">
            <a href="<?php echo esc_url( add_query_arg( 'sz_tab', 'financeiro', $page_url ) ); ?>"
               class="nav-tab <?php echo $tab === 'financeiro' ? 'nav-tab-active' : ''; ?>">💰 Financeiro & PIX</a>
            <a href="<?php echo esc_url( add_query_arg( 'sz_tab', 'classes', $page_url ) ); ?>"
               class="nav-tab <?php echo $tab === 'classes' ? 'nav-tab-active' : ''; ?>">🏷️ Classes de Entrega</a>
            <a href="<?php echo esc_url( add_query_arg( 'sz_tab', 'config', $page_url ) ); ?>"
               class="nav-tab <?php echo $tab === 'config' ? 'nav-tab-active' : ''; ?>">⚙️ Configurações</a>
        </nav>

        <?php
        if ( $tab === 'classes' ) {
            sz_afc_render_admin_tab();
        } elseif ( $tab === 'config' ) {
            sz_afc_render_config_tab();
        } else {
            sz_afc_render_financeiro_tab();
        }
        ?>
    </div>
    <?php
}

// ─────────────────────────────────────────────────────────────────────────────
// 7. ABAS FINANCEIRO E CONFIG (extraídas de sz_aff_render_admin_page_v2)
// ─────────────────────────────────────────────────────────────────────────────

function sz_afc_render_financeiro_tab(): void {
    global $wpdb;

    // Processar PIX
    if ( isset($_POST['sz_pix_nonce']) && wp_verify_nonce( sanitize_text_field(wp_unslash($_POST['sz_pix_nonce'])), 'sz_pix_action' ) ) {
        $pix_aff_id = absint($_POST['pix_aff_id'] ?? 0);
        $pix_action = sanitize_text_field($_POST['pix_action'] ?? '');
        if ( $pix_aff_id && in_array($pix_action,['approve','reject'],true) ) {
            $wpdb->update( sz_aff_table('sz_affiliates'), [ 'pix_status' => $pix_action === 'approve' ? 'approved' : 'rejected' ], [ 'id' => $pix_aff_id ], ['%s'], ['%d'] );
            echo '<div class="notice notice-success is-dismissible"><p>PIX '.($pix_action==='approve'?'aprovado':'rejeitado').'.</p></div>';
        }
    }

    // Processar Saque
    if ( isset($_POST['sz_saque_nonce']) && wp_verify_nonce( sanitize_text_field(wp_unslash($_POST['sz_saque_nonce'])), 'sz_saque_action' ) ) {
        $saque_id  = absint($_POST['saque_id'] ?? 0);
        $saque_act = sanitize_text_field($_POST['saque_action'] ?? '');
        $saque_note= sanitize_text_field($_POST['saque_note'] ?? '');
        if ( $saque_id && in_array($saque_act,['approve','reject'],true) ) {
            $w = $wpdb->get_row( $wpdb->prepare("SELECT * FROM ".sz_aff_table('sz_affiliate_withdrawals')." WHERE id=%d",$saque_id), ARRAY_A );
            if ( $w && $w['status'] === 'pending' ) {
                $now = sz_aff_now();
                if ( $saque_act === 'approve' ) {
                    $wpdb->query( $wpdb->prepare("UPDATE ".sz_aff_table('sz_affiliate_wallet')." SET balance=balance-%f, updated_at=%s WHERE affiliate_id=%d",(float)$w['amount'],$now,(int)$w['affiliate_id']));
                    $wpdb->insert(sz_aff_table('sz_affiliate_transactions'),['affiliate_id'=>(int)$w['affiliate_id'],'type'=>'withdrawal','amount'=>-(float)$w['amount'],'status'=>'available','available_at'=>$now,'created_at'=>$now],['%d','%s','%f','%s','%s','%s']);
                }
                $wpdb->update(sz_aff_table('sz_affiliate_withdrawals'),['status'=>$saque_act==='approve'?'approved':'rejected','decided_at'=>$now,'decided_by'=>get_current_user_id(),'note'=>$saque_note],['id'=>$saque_id],['%s','%s','%d','%s'],['%d']);
                echo '<div class="notice notice-success is-dismissible"><p>Saque '.($saque_act==='approve'?'aprovado':'rejeitado').'.</p></div>';
            }
        }
    }

    $wallet    = $wpdb->get_row("SELECT * FROM ".sz_aff_table('sz_admin_wallet')." WHERE id=1", ARRAY_A) ?: ['balance'=>0,'pending_balance'=>0];
    $pending_w = $wpdb->get_results("SELECT w.*,a.pix_key,a.bank_info,u.display_name,u.user_email FROM ".sz_aff_table('sz_affiliate_withdrawals')." w LEFT JOIN ".sz_aff_table('sz_affiliates')." a ON a.id=w.affiliate_id LEFT JOIN {$wpdb->users} u ON u.ID=a.user_id WHERE w.status='pending' ORDER BY w.created_at ASC LIMIT 50", ARRAY_A) ?: [];
    $pending_pix=$wpdb->get_results("SELECT a.id,a.pix_key,a.bank_info,a.pix_status,u.display_name,u.user_email FROM ".sz_aff_table('sz_affiliates')." a LEFT JOIN {$wpdb->users} u ON u.ID=a.user_id WHERE a.pix_status='pending' AND a.pix_key IS NOT NULL AND a.pix_key != '' ORDER BY a.id DESC LIMIT 50", ARRAY_A) ?: [];
    $all_aff   = $wpdb->get_results("SELECT a.*,w.balance,w.pending_balance,u.display_name,u.user_email FROM ".sz_aff_table('sz_affiliates')." a LEFT JOIN ".sz_aff_table('sz_affiliate_wallet')." w ON w.affiliate_id=a.id LEFT JOIN {$wpdb->users} u ON u.ID=a.user_id WHERE a.deleted_at IS NULL ORDER BY a.id DESC LIMIT 100", ARRAY_A) ?: [];
    ?>
    <div style="display:flex;gap:16px;flex-wrap:wrap;margin-bottom:20px">
        <div style="background:#fff7ed;border:1px solid #fed7aa;border-radius:12px;padding:16px 22px;min-width:180px">
            <div style="font-size:var(--sz-text-sm);font-weight:700;color:#9a3412;text-transform:none;letter-spacing:.02em">Carteira Admin</div>
            <div style="font-size:var(--sz-text-3xl);font-weight:700;color:#c2410c;margin-top:4px"><?php echo esc_html(sz_aff_money((float)($wallet['balance']??0))); ?></div>
        </div>
        <div style="background:#f0f9ff;border:1px solid #bae6fd;border-radius:12px;padding:16px 22px;min-width:180px">
            <div style="font-size:var(--sz-text-sm);font-weight:700;color:#0369a1;text-transform:none;letter-spacing:.02em">Saques Pendentes</div>
            <div style="font-size:var(--sz-text-3xl);font-weight:700;color:#0284c7;margin-top:4px"><?php echo count($pending_w); ?></div>
        </div>
        <div style="background:#fefce8;border:1px solid #fde047;border-radius:12px;padding:16px 22px;min-width:180px">
            <div style="font-size:var(--sz-text-sm);font-weight:700;color:#854d0e;text-transform:none;letter-spacing:.02em">PIX Pendentes</div>
            <div style="font-size:var(--sz-text-3xl);font-weight:700;color:#ca8a04;margin-top:4px"><?php echo count($pending_pix); ?></div>
        </div>
    </div>

    <div style="display:grid;grid-template-columns:1fr 1fr;gap:24px;margin-bottom:24px">
    <div>
    <h2>💰 Saques pendentes</h2>
    <?php if(empty($pending_w)): ?><p style="color:#64748b">Nenhum saque pendente.</p>
    <?php else: foreach($pending_w as $p): ?>
    <div style="background:#fff;border:1px solid #e5e7eb;border-radius:12px;padding:14px 16px;margin-bottom:10px">
        <div style="font-weight:700"><?php echo esc_html($p['display_name']?:$p['user_email']); ?></div>
        <div style="font-size:var(--sz-text-base);color:#64748b;margin:4px 0">
            Valor: <strong><?php echo esc_html(sz_aff_money((float)$p['amount'])); ?></strong> ·
            Taxa: <?php echo esc_html(sz_aff_money((float)$p['fee'])); ?> ·
            Líquido: <strong style="color:#E8650A"><?php echo esc_html(sz_aff_money((float)$p['net_amount'])); ?></strong>
        </div>
        <?php if($p['pix_key']): ?><div style="font-size:var(--sz-text-meta);background:#f8fafc;border-radius:8px;padding:6px 10px;margin:6px 0">PIX: <strong><?php echo esc_html($p['pix_key']); ?></strong><?php if($p['bank_info']): ?> · <?php echo esc_html($p['bank_info']); ?><?php endif; ?></div><?php endif; ?>
        <div style="font-size:var(--sz-text-sm);color:#94a3b8;margin-bottom:8px">Criado: <?php echo esc_html(mysql2date('d/m/Y H:i',$p['created_at'])); ?></div>
        <form method="post" style="display:flex;gap:8px;flex-wrap:wrap;align-items:center">
            <?php wp_nonce_field('sz_saque_action','sz_saque_nonce'); ?>
            <input type="hidden" name="saque_id" value="<?php echo esc_attr($p['id']); ?>">
            <input type="text" name="saque_note" placeholder="Nota (opcional)" style="border:1px solid #d1d5db;border-radius:8px;padding:5px 10px;font-size:var(--sz-text-meta);flex:1;min-width:120px">
            <button name="saque_action" value="approve" class="button button-primary">✓ Aprovar</button>
            <button name="saque_action" value="reject"  class="button button-secondary">✗ Rejeitar</button>
        </form>
    </div>
    <?php endforeach; endif; ?>
    </div>
    <div>
    <h2>🔑 PIX / Contas pendentes</h2>
    <?php if(empty($pending_pix)): ?><p style="color:#64748b">Nenhuma aprovação pendente.</p>
    <?php else: foreach($pending_pix as $p): ?>
    <div style="background:#fff;border:1px solid #e5e7eb;border-radius:12px;padding:14px 16px;margin-bottom:10px">
        <div style="font-weight:700"><?php echo esc_html($p['display_name']?:$p['user_email']); ?></div>
        <div style="font-size:var(--sz-text-base);color:#64748b;margin:4px 0">PIX: <strong><?php echo esc_html($p['pix_key']); ?></strong></div>
        <?php if($p['bank_info']): ?><div style="font-size:var(--sz-text-meta);color:#64748b">Banco: <?php echo esc_html($p['bank_info']); ?></div><?php endif; ?>
        <form method="post" style="display:flex;gap:8px;margin-top:8px">
            <?php wp_nonce_field('sz_pix_action','sz_pix_nonce'); ?>
            <input type="hidden" name="pix_aff_id" value="<?php echo esc_attr($p['id']); ?>">
            <button name="pix_action" value="approve" class="button button-primary">✓ Aprovar</button>
            <button name="pix_action" value="reject"  class="button button-secondary">✗ Rejeitar</button>
        </form>
    </div>
    <?php endforeach; endif; ?>
    </div>
    </div>

    <h2>👥 Todos os afiliados</h2>
    <table class="widefat striped">
        <thead><tr><th>Afiliado</th><th>PIX</th><th>Status PIX</th><th>Saldo</th><th>A liberar</th><th>Comissão%</th><th>Status</th></tr></thead>
        <tbody>
        <?php if(empty($all_aff)) echo '<tr><td colspan="7">Nenhum afiliado cadastrado.</td></tr>';
        foreach($all_aff as $a):
            $pix_lbl=['pending'=>'⏳ Pendente','approved'=>'✅ Aprovado','rejected'=>'❌ Recusado'][$a['pix_status']??'pending']??'—';
        ?>
        <tr>
            <td><?php echo esc_html($a['display_name']??$a['user_email']??'#'.$a['id']); ?></td>
            <td><?php echo $a['pix_key'] ? esc_html(substr($a['pix_key'],0,20).'…') : '—'; ?></td>
            <td><?php echo $pix_lbl; ?></td>
            <td><?php echo esc_html(sz_aff_money((float)($a['balance']??0))); ?></td>
            <td><?php echo esc_html(sz_aff_money((float)($a['pending_balance']??0))); ?></td>
            <td><?php echo esc_html(number_format((float)($a['commission_pct']??0),1,',','.')); ?>%</td>
            <td><?php echo esc_html($a['status']??'—'); ?></td>
        </tr>
        <?php endforeach; ?>
        </tbody>
    </table>
    <?php
}

function sz_afc_render_config_tab(): void {
    global $wpdb;
    if ( isset($_POST['sz_aff_admin_nonce']) && wp_verify_nonce( sanitize_text_field(wp_unslash($_POST['sz_aff_admin_nonce'])), 'sz_aff_admin' ) ) {
        update_option('sz_aff_default_commission_pct',   max(0,min(100,(float)str_replace(',','.',$_POST['commission'] ?? 10))));
        update_option('sz_aff_default_withdraw_fee',     max(0,(float)str_replace(',','.',$_POST['withdraw_fee'] ?? 2.99)));
        update_option('sz_aff_first_frustration_penalty',max(0,(float)str_replace(',','.',$_POST['penalty_first'] ?? 0)));
        update_option('sz_aff_default_penalty_value',    max(0,(float)str_replace(',','.',$_POST['penalty'] ?? 0)));
        update_option('sz_aff_default_retention_days',   max(7,absint($_POST['retention'] ?? 30)));
        echo '<div class="notice notice-success is-dismissible"><p>Configurações salvas.</p></div>';
    }
    ?>
    <h2>⚙️ Configurações globais de afiliados</h2>
    <form method="post" style="background:#fff;border:1px solid #ccd0d4;border-radius:8px;padding:22px;max-width:600px">
        <?php wp_nonce_field('sz_aff_admin','sz_aff_admin_nonce'); ?>
        <div style="display:grid;grid-template-columns:1fr 1fr;gap:16px">
            <label style="display:grid;gap:6px;font-size:var(--sz-text-base);font-weight:700">
                Comissão padrão (%)
                <input name="commission" class="regular-text" value="<?php echo esc_attr(sz_aff_default_commission_pct()); ?>">
            </label>
            <label style="display:grid;gap:6px;font-size:var(--sz-text-base);font-weight:700">
                Taxa saque afiliado (R$)
                <input name="withdraw_fee" class="regular-text" value="<?php echo esc_attr(sz_aff_default_withdraw_fee()); ?>">
            </label>
            <label style="display:grid;gap:6px;font-size:var(--sz-text-base);font-weight:700">
                Penalidade 1ª frustração (R$)
                <input name="penalty_first" class="regular-text" value="<?php echo esc_attr(sz_aff_first_frustration_penalty()); ?>">
            </label>
            <label style="display:grid;gap:6px;font-size:var(--sz-text-base);font-weight:700">
                Penalidade reincidente (R$)
                <input name="penalty" class="regular-text" value="<?php echo esc_attr(sz_aff_default_penalty_value()); ?>">
            </label>
            <label style="display:grid;gap:6px;font-size:var(--sz-text-base);font-weight:700">
                Retenção mínima (dias)
                <input name="retention" class="regular-text" value="<?php echo esc_attr(sz_aff_default_retention_days()); ?>">
            </label>
        </div>
        <p style="margin-top:16px"><button class="button button-primary">Salvar configurações</button></p>
    </form>
    <hr style="margin:24px 0">
    <h2>🔄 Ferramentas de manutenção</h2>
    <p style="color:#64748b;font-size:var(--sz-text-base)">Sincronizar manualmente todos os afiliados ativos com as classes dos seus produtores:</p>
    <form method="post">
        <?php wp_nonce_field('sz_afc_maint','sz_afc_maint_nonce'); ?>
        <input type="hidden" name="sz_afc_action" value="sync_all">
        <button class="button button-secondary" onclick="return confirm('Sincronizar classes de todos os afiliados ativos?')">🔄 Sincronizar todas as classes</button>
    </form>
    <?php
    // Processar sincronização manual
    if ( isset($_POST['sz_afc_maint_nonce']) && wp_verify_nonce( sanitize_text_field(wp_unslash($_POST['sz_afc_maint_nonce'])), 'sz_afc_maint' )
         && ( $_POST['sz_afc_action'] ?? '' ) === 'sync_all' ) {
        $all = $wpdb->get_results(
            "SELECT id, producer_id FROM ".sz_aff_table('sz_affiliates')." WHERE status='active' AND deleted_at IS NULL LIMIT 500",
            ARRAY_A
        ) ?: [];
        $count = 0;
        foreach ( $all as $row ) {
            sz_afc_sync_affiliate_classes( (int)$row['id'], (int)$row['producer_id'], true );
            $count++;
        }
        echo '<div class="notice notice-success is-dismissible"><p>'.esc_html($count).' afiliado(s) sincronizados.</p></div>';
    }
}
