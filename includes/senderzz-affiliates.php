<?php
/**
 * Senderzz — Sistema de Afiliados (Etapa 2)
 * Backend financeiro, convites, vínculo produtor↔afiliado e painel produtor.
 */
defined( 'ABSPATH' ) || exit;

if ( defined( 'SENDERZZ_AFFILIATES_LOADED' ) ) return;
define( 'SENDERZZ_AFFILIATES_LOADED', true );
define( 'SZ_AFF_DB_VERSION', '1.0.7' );
define( 'SZ_AFF_COOKIE', 'sz_aff_token' );

function sz_aff_table( string $name ): string { global $wpdb; return $wpdb->prefix . $name; }
function sz_aff_money( float $v ): string { return 'R$ ' . number_format( $v, 2, ',', '.' ); }
function sz_aff_now(): string { return current_time( 'mysql' ); }

/**
 * v181: remove o status legado "blocked" do fluxo de afiliados.
 * Afiliado válido é apenas pending ou active. Recusa/exclusão fica por deleted_at.
 */
function sz_aff_cleanup_blocked_status_legacy(): void {
    static $done = false;
    if ( $done ) return;
    $done = true;

    global $wpdb;
    if ( ! isset( $wpdb ) ) return;
    $table = sz_aff_table( 'sz_affiliates' );
    $exists = $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $table ) );
    if ( $exists !== $table ) return;
    $wpdb->query( $wpdb->prepare(
        "UPDATE {$table} SET status='pending', deleted_at=IF(deleted_at IS NULL OR deleted_at='', %s, deleted_at) WHERE status='blocked'",
        sz_aff_now()
    ) );
}
add_action( 'plugins_loaded', 'sz_aff_cleanup_blocked_status_legacy', 20 );
add_action( 'init', 'sz_aff_cleanup_blocked_status_legacy', 5 );
add_action( 'admin_init', 'sz_aff_cleanup_blocked_status_legacy', 5 );
function sz_aff_status_label( string $status ): string {
    $map = [
        'active' => 'Ativo',
        'pending' => 'Pendente',
        'cancelled' => 'Cancelado',
        'available' => 'Disponível',
    ];
    return $map[ $status ] ?? ucfirst( $status );
}

function sz_aff_digits( string $value ): string { return preg_replace( '/\D+/', '', $value ); }
function sz_aff_validate_cpf( string $cpf ): bool {
    $cpf = sz_aff_digits( $cpf );
    if ( strlen( $cpf ) !== 11 ) return false;
    if ( preg_match( '/^(\d)\1{10}$/', $cpf ) ) return false;
    for ( $t = 9; $t < 11; $t++ ) {
        $sum = 0;
        for ( $i = 0; $i < $t; $i++ ) $sum += (int) $cpf[$i] * ( ( $t + 1 ) - $i );
        $digit = ( ( 10 * $sum ) % 11 ) % 10;
        if ( (int) $cpf[$t] !== $digit ) return false;
    }
    return true;
}
function sz_aff_validate_phone( string $phone ): bool {
    $digits = sz_aff_digits( $phone );
    return (bool) preg_match( '/^(?:55)?[1-9]{2}9?\d{8}$/', $digits );
}

if ( ! function_exists( 'sz_senderzz_account_exists_by_email_or_cpf' ) ) {
function sz_senderzz_account_exists_by_email_or_cpf( string $email = '', string $cpf = '', int $exclude_user_id = 0 ): bool {
    global $wpdb;
    $email = sanitize_email( $email );
    $cpf   = preg_replace( '/\D+/', '', $cpf );

    if ( $email !== '' ) {
        $user_id = (int) email_exists( $email );
        if ( $user_id > 0 && $user_id !== $exclude_user_id ) return true;

        $portal_table = $wpdb->prefix . 'senderzz_portal_users';
        $has_portal = (int) $wpdb->get_var( $wpdb->prepare(
            "SELECT id FROM {$portal_table} WHERE email=%s LIMIT 1",
            $email
        ) );
        if ( $has_portal > 0 ) return true;
    }

    if ( $cpf !== '' ) {
        $has_meta = (int) $wpdb->get_var( $wpdb->prepare(
            "SELECT user_id FROM {$wpdb->usermeta} WHERE meta_key='sz_document' AND REPLACE(REPLACE(REPLACE(meta_value,'.',''),'-',''),'/','')=%s LIMIT 1",
            $cpf
        ) );
        if ( $has_meta > 0 && $has_meta !== $exclude_user_id ) return true;

        if ( function_exists( 'sz_aff_table' ) ) {
            $aff_table = sz_aff_table( 'sz_affiliates' );
            $has_aff = (int) $wpdb->get_var( $wpdb->prepare(
                "SELECT id FROM {$aff_table} WHERE document=%s AND deleted_at IS NULL LIMIT 1",
                $cpf
            ) );
            if ( $has_aff > 0 ) return true;
        }
    }

    return false;
}
}

function sz_aff_format_cpf( string $cpf ): string {
    $cpf = substr( sz_aff_digits( $cpf ), 0, 11 );
    if ( strlen( $cpf ) !== 11 ) return $cpf;
    return substr( $cpf, 0, 3 ) . '.' . substr( $cpf, 3, 3 ) . '.' . substr( $cpf, 6, 3 ) . '-' . substr( $cpf, 9, 2 );
}
function sz_aff_format_phone( string $phone ): string {
    $digits = sz_aff_digits( $phone );
    if ( strpos( $digits, '55' ) === 0 && strlen( $digits ) > 11 ) $digits = substr( $digits, 2 );
    if ( strlen( $digits ) === 11 ) return '(' . substr( $digits, 0, 2 ) . ') ' . substr( $digits, 2, 5 ) . '-' . substr( $digits, 7, 4 );
    if ( strlen( $digits ) === 10 ) return '(' . substr( $digits, 0, 2 ) . ') ' . substr( $digits, 2, 4 ) . '-' . substr( $digits, 6, 4 );
    return $phone;
}
function sz_aff_plugin_logo_url(): string {
    return plugins_url( 'assets/images/senderzz-logo.png', dirname( __DIR__ ) . '/senderzz-logistics.php' );
}
function sz_aff_user_profile_value( int $user_id, string $key ): string { return (string) get_user_meta( $user_id, $key, true ); }
function sz_aff_get_producer_display_name( int $producer_id ): string {
    global $wpdb;
    $producer_id = absint( $producer_id );
    if ( $producer_id <= 0 ) return 'Produtor';

    // Busca primeiro por id exato, depois por wp_user_id — evita retornar usuário errado
    $portal = $wpdb->get_row( $wpdb->prepare(
        "SELECT name,email,wp_user_id FROM {$wpdb->prefix}senderzz_portal_users WHERE id=%d LIMIT 1",
        $producer_id
    ), ARRAY_A );
    if ( ! $portal ) {
        $portal = $wpdb->get_row( $wpdb->prepare(
            "SELECT name,email,wp_user_id FROM {$wpdb->prefix}senderzz_portal_users WHERE wp_user_id=%d LIMIT 1",
            $producer_id
        ), ARRAY_A );
    }
    if ( $portal ) {
        $name = trim( (string) ( $portal['name'] ?? '' ) );
        if ( $name !== '' ) return $name;
        $email = trim( (string) ( $portal['email'] ?? '' ) );
        if ( $email !== '' ) return $email;
        $wp_user_id = absint( $portal['wp_user_id'] ?? 0 );
        if ( $wp_user_id ) {
            $user = get_user_by( 'id', $wp_user_id );
            if ( $user && trim( (string) $user->display_name ) !== '' ) return (string) $user->display_name;
        }
    }

    $user = get_user_by( 'id', $producer_id );
    if ( $user && trim( (string) $user->display_name ) !== '' && strtolower( (string) $user->display_name ) !== 'senderzz' ) {
        return (string) $user->display_name;
    }
    return 'Produtor';
}


function sz_aff_ensure_portal_affiliate_user( int $wp_user_id ): int {
    global $wpdb;
    $wp_user_id = absint( $wp_user_id );
    $user = $wp_user_id ? get_user_by( 'id', $wp_user_id ) : null;
    if ( ! $user || empty( $user->user_email ) ) return 0;

    $table = $wpdb->prefix . 'senderzz_portal_users';
    if ( class_exists( '\WC_MelhorEnvio\Portal\Portal_Auth' ) ) {
        try { \WC_MelhorEnvio\Portal\Portal_Auth::install(); } catch ( Throwable $e ) {}
    }

    $portal_id = (int) $wpdb->get_var( $wpdb->prepare( "SELECT id FROM {$table} WHERE email=%s OR wp_user_id=%d ORDER BY id ASC LIMIT 1", $user->user_email, $wp_user_id ) );

    // Determina role: 'affiliate' apenas se não for produtor. Produtores existentes mantêm seu role.
    $target_role = 'affiliate';
    if ( $portal_id ) {
        $existing_role = (string) $wpdb->get_var( $wpdb->prepare( "SELECT role FROM {$table} WHERE id=%d", $portal_id ) );
        // Não rebaixar um produtor para afiliado
        if ( in_array( $existing_role, [ 'producer', 'admin', 'operator' ], true ) ) {
            $target_role = $existing_role;
        }
    }

    $data = [
        'email'         => (string) $user->user_email,
        'name'          => (string) ( $user->display_name ?: $user->user_login ),
        'status'        => 'active',
        'role'          => $target_role,
        'wp_user_id'    => $wp_user_id,
        'require_2fa'   => 1,
    ];
    if ( $portal_id ) {
        $wpdb->update( $table, $data, [ 'id' => $portal_id ] );
        return $portal_id;
    }

    $data['password_hash'] = wp_hash_password( wp_generate_password( 32, true, true ) );
    $data['shipping_class_id'] = 0;
    $data['parent_user_id'] = null;
    $data['permissions'] = wp_json_encode( [
        'wallet'  => true,
        'links'   => true,
        'reports' => true,
    ] );
    $wpdb->insert( $table, $data );
    return (int) $wpdb->insert_id;
}

function sz_aff_migrate_portal_affiliate_roles(): void {
    global $wpdb;
    $table = $wpdb->prefix . 'senderzz_portal_users';
    $exists = $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $table ) );
    if ( ! $exists ) return;

    $aff_table = sz_aff_table( 'sz_affiliates' );
    $aff_exists = $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $aff_table ) );
    if ( ! $aff_exists ) return;

    // v173+: Classifica como 'affiliate' qualquer portal user que:
    // (a) tenha role='client' (nunca foi explicitamente definido como produtor)
    // (b) possua vínculo ativo na tabela sz_affiliates
    // (c) não seja dono de shipping_class_id própria (não é produtor real)
    // (d) não tenha _sz_has_motoboy=1 ou _sz_has_expedicao=1 (sem modalidade de produtor)
    $wpdb->query(
        "UPDATE {$table} pu
         INNER JOIN {$aff_table} a ON a.user_id = pu.wp_user_id
         LEFT JOIN {$wpdb->usermeta} um_mb  ON um_mb.user_id  = pu.wp_user_id AND um_mb.meta_key  = '_sz_has_motoboy'
         LEFT JOIN {$wpdb->usermeta} um_exp ON um_exp.user_id = pu.wp_user_id AND um_exp.meta_key = '_sz_has_expedicao'
         SET pu.role = 'affiliate'
         WHERE pu.role IN ('client','')
           AND a.status = 'active'
           AND a.deleted_at IS NULL
           AND ( pu.shipping_class_id IS NULL OR pu.shipping_class_id = 0 )
           AND ( um_mb.meta_value  IS NULL OR um_mb.meta_value  != '1' )
           AND ( um_exp.meta_value IS NULL OR um_exp.meta_value != '1' )"
    );

    // Garante que nenhum afiliado puro tenha _sz_has_expedicao=1
    // (bloqueia reativação silenciosa de expedição via user meta)
    $wpdb->query(
        "UPDATE {$wpdb->usermeta} um
         INNER JOIN {$aff_table} a ON a.user_id = um.user_id
         LEFT JOIN {$wpdb->usermeta} um_mb ON um_mb.user_id = um.user_id AND um_mb.meta_key = '_sz_has_motoboy'
         SET um.meta_value = '0'
         WHERE um.meta_key = '_sz_has_expedicao'
           AND um.meta_value = '1'
           AND a.status = 'active'
           AND a.deleted_at IS NULL
           AND ( um_mb.meta_value IS NULL OR um_mb.meta_value != '1' )"
    );
}
// Migração de roles roda 1x por versão, não em toda request
add_action( 'init', function() {
    $migrated_version = get_option( 'sz_aff_role_migrate_version', '' );
    if ( $migrated_version !== SZ_AFF_DB_VERSION ) {
        sz_aff_migrate_portal_affiliate_roles();
        update_option( 'sz_aff_role_migrate_version', SZ_AFF_DB_VERSION, false );
    }
}, 9 );

function sz_aff_portal_user_wp_id( $portal_user ): int {
    $wp_user_id = absint( $portal_user->wp_user_id ?? 0 );
    if ( $wp_user_id ) return $wp_user_id;
    $email = sanitize_email( $portal_user->email ?? '' );
    if ( $email ) {
        $wp_user = get_user_by( 'email', $email );
        if ( $wp_user ) return (int) $wp_user->ID;
    }
    return 0;
}

function sz_aff_get_affiliate_ids_for_portal_user( $portal_user, string $status = 'active' ): array {
    global $wpdb;
    $wp_user_id = sz_aff_portal_user_wp_id( $portal_user );
    if ( ! $wp_user_id ) return [];
    $where_status = $status !== '' ? $wpdb->prepare( ' AND status=%s', $status ) : '';
    $ids = $wpdb->get_col( $wpdb->prepare(
        "SELECT id FROM " . sz_aff_table('sz_affiliates') . " WHERE user_id=%d AND deleted_at IS NULL{$where_status}",
        $wp_user_id
    ) );
    return array_values( array_unique( array_map( 'intval', (array) $ids ) ) );
}

function sz_aff_portal_user_has_active_affiliate_link( $portal_user ): bool {
    // Considera ativo: vínculo approved OU pending (já foi convidado e aguarda aprovação)
    $has_active  = ! empty( sz_aff_get_affiliate_ids_for_portal_user( $portal_user, 'active' ) );
    if ( $has_active ) return true;
    // Também reconhece pendentes para mostrar o portal de afiliado desde o convite
    $wp_user_id = sz_aff_portal_user_wp_id( $portal_user );
    if ( ! $wp_user_id ) return false;
    global $wpdb;
    return (bool) $wpdb->get_var( $wpdb->prepare(
        "SELECT id FROM " . sz_aff_table('sz_affiliates') . " WHERE user_id=%d AND status='pending' AND deleted_at IS NULL LIMIT 1",
        $wp_user_id
    ) );
}

/**
 * Retorna true quando o usuário TEM vínculo de afiliado mas TODOS estão pendentes (nenhum active).
 * Nesses casos o portal deve bloquear acesso e mostrar mensagem de aguardar aprovação.
 */
function sz_aff_portal_user_has_only_pending_affiliate( $portal_user ): bool {
    $has_active = ! empty( sz_aff_get_affiliate_ids_for_portal_user( $portal_user, 'active' ) );
    if ( $has_active ) return false; // tem pelo menos um ativo — não bloquear
    $wp_user_id = sz_aff_portal_user_wp_id( $portal_user );
    if ( ! $wp_user_id ) return false;
    global $wpdb;
    return (bool) $wpdb->get_var( $wpdb->prepare(
        "SELECT id FROM " . sz_aff_table('sz_affiliates') . " WHERE user_id=%d AND status='pending' AND deleted_at IS NULL LIMIT 1",
        $wp_user_id
    ) );
}

function sz_aff_portal_user_has_producer_access( $portal_user ): bool {
    // Produtor real no Senderzz = usuário do portal liberado pelo admin com classe própria.
    // Não depende mais só dos metas _sz_has_motoboy/_sz_has_expedicao, porque o admin pode
    // criar o produtor, selecionar classes e ele deve entrar imediatamente no portal.
    global $wpdb;

    $portal_id  = absint( $portal_user->id ?? ( is_numeric( $portal_user ) ? $portal_user : 0 ) );
    $wp_user_id = function_exists( 'sz_aff_portal_user_wp_id' ) ? sz_aff_portal_user_wp_id( $portal_user ) : absint( $portal_user->wp_user_id ?? 0 );
    $role       = strtolower( trim( (string) ( $portal_user->role ?? '' ) ) );

    // Blindagem: usuário explicitamente afiliado nunca tem acesso de produtor por classe herdada.
    if ( $role === 'affiliate' || $role === 'afiliado' ) {
        return false;
    }

    $class_ids = [];
    if ( function_exists( 'sz_get_user_class_ids' ) ) {
        $class_ids = sz_get_user_class_ids( $portal_user );
    }

    $legacy_class = absint( $portal_user->shipping_class_id ?? 0 );
    if ( $legacy_class > 0 && ! in_array( $legacy_class, $class_ids, true ) ) {
        $class_ids[] = $legacy_class;
    }

    // Fallback por banco para cobrir cache/objeto antigo: classe gravada pelo admin em tabela multi-class.
    if ( empty( $class_ids ) && $portal_id > 0 ) {
        $mc_table = $wpdb->prefix . 'senderzz_portal_user_classes';
        if ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $mc_table ) ) === $mc_table ) {
            $class_ids = array_map( 'absint', (array) $wpdb->get_col( $wpdb->prepare(
                "SELECT shipping_class_id FROM {$mc_table} WHERE portal_user_id=%d AND shipping_class_id > 0",
                $portal_id
            ) ) );
        }
    }

    if ( empty( $class_ids ) && $wp_user_id > 0 ) {
        $legacy_db = (int) $wpdb->get_var( $wpdb->prepare(
            "SELECT shipping_class_id FROM {$wpdb->prefix}senderzz_portal_users WHERE (id=%d OR wp_user_id=%d) AND shipping_class_id > 0 LIMIT 1",
            $portal_id, $wp_user_id
        ) );
        if ( $legacy_db > 0 ) $class_ids[] = $legacy_db;
    }

    // Role producer/produtor com classe sempre entra. Role client antigo com classe também entra
    // para não quebrar produtores criados antes da separação visual.
    $has = ! empty( array_filter( $class_ids, fn( $id ) => (int) $id > 0 ) )
        && in_array( $role, [ 'producer', 'produtor', 'client', '' ], true );

    return (bool) apply_filters( 'sz_aff_portal_user_has_producer_access', $has, $portal_user );
}


function sz_aff_sync_portal_access_after_affiliation_change( int $wp_user_id ): void {
    global $wpdb;
    $wp_user_id = absint( $wp_user_id );
    if ( $wp_user_id <= 0 ) return;

    $portal_table = $wpdb->prefix . 'senderzz_portal_users';
    $portal = $wpdb->get_row( $wpdb->prepare(
        "SELECT * FROM {$portal_table} WHERE wp_user_id=%d OR email=(SELECT user_email FROM {$wpdb->users} WHERE ID=%d) ORDER BY id ASC LIMIT 1",
        $wp_user_id,
        $wp_user_id
    ) );
    if ( ! $portal ) return;

    $has_active_affiliate = (int) $wpdb->get_var( $wpdb->prepare(
        "SELECT COUNT(*) FROM " . sz_aff_table('sz_affiliates') . " WHERE user_id=%d AND status='active' AND deleted_at IS NULL",
        $wp_user_id
    ) ) > 0;

    $has_producer_access = sz_aff_portal_user_has_producer_access( $portal );

    // Segurança: se a afiliação foi removida e o admin não liberou modalidade de produtor,
    // limpa qualquer classe/permissão herdada da afiliação para não abrir Expedição/Estoque/etc.
    if ( ! $has_active_affiliate && ! $has_producer_access ) {
        $mc_table = $wpdb->prefix . 'senderzz_portal_user_classes';
        if ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $mc_table ) ) === $mc_table ) {
            $wpdb->delete( $mc_table, [ 'portal_user_id' => (int) $portal->id ], [ '%d' ] );
        }
        $wpdb->update(
            $portal_table,
            [
                'permissions'       => wp_json_encode( [] ),
                'shipping_class_id' => 0,
                'shipping_class_ids'=> wp_json_encode( [] ),
                'role'              => 'client',
                'parent_user_id'    => null,
            ],
            [ 'id' => (int) $portal->id ],
            [ '%s', '%d', '%s', '%s', '%d' ],
            [ '%d' ]
        );
    } elseif ( $has_active_affiliate && ! $has_producer_access ) {
        // Afiliado puro: garante role='affiliate' e bloqueia expedição via user meta
        $wpdb->update( $portal_table, [ 'role' => 'affiliate' ], [ 'id' => (int) $portal->id ], [ '%s' ], [ '%d' ] );
        // Impede que afiliado tenha acesso a expedição Melhor Envio
        update_user_meta( $wp_user_id, '_sz_has_expedicao', '0' );
    }
}

function sz_aff_portal_user_is_affiliate_pure( $portal_user ): bool {
    // Afiliado não usa portal separado: esta flag controla somente permissões/menus dentro do portal único.
    return sz_aff_portal_user_has_active_affiliate_link( $portal_user ) && ! sz_aff_portal_user_has_producer_access( $portal_user );
}

/**
 * Escopo obrigatório de afiliado.
 *
 * Blindagem: se o usuário do portal está marcado como affiliate, ele nunca pode cair no
 * fallback de produtor/classe logística, mesmo que tenha herdado shipping_class_id antigo.
 */
function sz_aff_portal_user_must_use_affiliate_scope( $portal_user ): bool {
    $role = strtolower( trim( (string) ( $portal_user->role ?? '' ) ) );
    if ( $role === 'affiliate' || $role === 'afiliado' ) {
        return true;
    }
    return function_exists( 'sz_aff_portal_user_is_affiliate_pure' ) && sz_aff_portal_user_is_affiliate_pure( $portal_user );
}

function sz_aff_get_affiliate_order_ids_for_portal_user( $portal_user, int $limit = 100 ): array {
    $affiliate_ids = function_exists( 'sz_aff_get_affiliate_ids_for_portal_user' ) ? sz_aff_get_affiliate_ids_for_portal_user( $portal_user, 'active' ) : [];
    $wp_user_id    = function_exists( 'sz_aff_portal_user_wp_id' ) ? sz_aff_portal_user_wp_id( $portal_user ) : absint( $portal_user->wp_user_id ?? 0 );

    $affiliate_ids = array_values( array_unique( array_filter( array_map( 'absint', (array) $affiliate_ids ) ) ) );
    if ( empty( $affiliate_ids ) || ! function_exists( 'wc_get_orders' ) ) return [];

    // HPOS-safe e sem fallback por portal_id: afiliado só vê pedido com vínculo real dele.
    $meta_query = [ 'relation' => 'OR' ];
    foreach ( [ '_sz_affiliate_id', '_sz_affiliate_ref' ] as $meta_key ) {
        $meta_query[] = [
            'key'     => $meta_key,
            'value'   => array_map( 'strval', $affiliate_ids ),
            'compare' => 'IN',
        ];
    }
    if ( $wp_user_id > 0 ) {
        $meta_query[] = [
            'key'     => '_sz_affiliate_user_id',
            'value'   => (string) $wp_user_id,
            'compare' => '=',
        ];
    }

    $ids = wc_get_orders( [
        'limit'      => max( 1, min( 500, $limit ) ),
        'orderby'    => 'date',
        'order'      => 'DESC',
        'return'     => 'ids',
        'meta_query' => $meta_query,
    ] );

    return array_values( array_unique( array_map( 'intval', (array) $ids ) ) );
}

function sz_aff_send_affiliate_access_email( int $user_id, string $producer_name, string $status ): void {
    $user = get_user_by( 'id', $user_id );
    if ( ! $user || empty( $user->user_email ) ) return;

    // O login do portal valida senha em senderzz_portal_users.
    // O token precisa usar o ID do portal, não o ID do WordPress.
    $portal_user_id = function_exists( 'sz_aff_ensure_portal_affiliate_user' )
        ? (int) sz_aff_ensure_portal_affiliate_user( (int) $user_id )
        : 0;
    if ( $portal_user_id <= 0 ) return;

    $token   = bin2hex( random_bytes( 32 ) );
    $expires = time() + 30 * MINUTE_IN_SECONDS;
    set_transient( 'sz_pwreset_' . $token, [
        'user_id' => $portal_user_id,
        'email'   => (string) $user->user_email,
        'expires' => $expires,
    ], 30 * MINUTE_IN_SECONDS );

    $portal_url = get_permalink( get_option( 'senderzz_portal_page_id' ) ) ?: home_url( '/meus-pedidos/' );
    $login_url  = $portal_url;
    $reset_url  = add_query_arg( [ 'sz_reset' => $token ], $portal_url );
    $logo_url   = function_exists( 'senderzz_portal_logo_url' ) ? senderzz_portal_logo_url() : sz_aff_plugin_logo_url();

    $subject = 'Seu acesso de afiliado Senderzz';
    $status_text = $status === 'active'
        ? 'Seu cadastro já foi aprovado e seu acesso está pronto.'
        : 'Seu cadastro foi recebido e está em análise. Enquanto isso, você já pode definir sua senha.';

    $message = '
    <table role="presentation" width="100%" cellspacing="0" cellpadding="0" border="0" style="background:#f6f7f9;margin:0;padding:28px 12px;font-family:var(--sz-font);">
        <tr><td align="center">
            <table role="presentation" width="100%" cellspacing="0" cellpadding="0" border="0" style="max-width:560px;background:#ffffff;border:1px solid #e5e7eb;border-radius:20px;overflow:hidden;">
                <tr><td style="padding:30px 32px 10px 32px;">
                    <img src="' . esc_url( $logo_url ) . '" alt="Senderzz" style="display:block;height:30px;max-width:150px;width:auto;border:0;outline:none;text-decoration:none;">
                </td></tr>
                <tr><td style="padding:10px 32px 0 32px;">
                    <h1 style="margin:0;color:#111827;font-size:var(--sz-text-3xl);line-height:1.15;font-weight:700;">Seu acesso de afiliado</h1>
                    <p style="margin:14px 0 0 0;color:#475467;font-size:var(--sz-text-lg);line-height:1.55;">Olá, <strong>' . esc_html( $user->display_name ) . '</strong>!</p>
                    <p style="margin:14px 0 0 0;color:#475467;font-size:var(--sz-text-lg);line-height:1.55;">' . esc_html( $producer_name ) . ' convidou você para participar do programa de afiliados da Senderzz.</p>
                    <p style="margin:10px 0 0 0;color:#475467;font-size:var(--sz-text-lg);line-height:1.55;">' . esc_html( $status_text ) . '</p>
                </td></tr>
                <tr><td style="padding:24px 32px 8px 32px;">
                    <table role="presentation" cellspacing="0" cellpadding="0" border="0"><tr><td bgcolor="#E8650A" style="border-radius:13px;">
                        <a href="' . esc_url( $reset_url ) . '" style="display:inline-block;padding:14px 24px;color:#ffffff;text-decoration:none;font-weight:700;font-size:var(--sz-text-md);border-radius:13px;">Definir minha senha</a>
                    </td></tr></table>
                </td></tr>
                <tr><td style="padding:10px 32px 4px 32px;">
                    <p style="margin:0;color:#667085;font-size:var(--sz-text-base);line-height:1.5;">Depois, acesse sua área de afiliado:</p>
                    <p style="margin:6px 0 0 0;"><a href="' . esc_url( $login_url ) . '" style="color:#E8650A;text-decoration:none;font-weight:700;font-size:var(--sz-text-base);">' . esc_html( $login_url ) . '</a></p>
                </td></tr>
                <tr><td style="padding:16px 32px 30px 32px;">
                    <p style="margin:0;color:#667085;font-size:var(--sz-text-meta);line-height:1.5;">PIX e dados bancários podem ser preenchidos depois, dentro do seu perfil.</p>
                </td></tr>
            </table>
        </td></tr>
    </table>';

    $html_ct = function() { return 'text/html'; };
    add_filter( 'wp_mail_content_type', $html_ct );
    wp_mail( $user->user_email, $subject, $message );
    remove_filter( 'wp_mail_content_type', $html_ct );
}
function sz_aff_link_enabled_label( $enabled ): string { return ! empty( $enabled ) ? 'Link liberado' : 'Somente cadastro'; }
function sz_aff_checkout_public_code( array $row ): string {
    $name = trim( (string) ( $row['offer_name'] ?? '' ) );
    if ( $name !== '' ) return $name;
    $token = trim( (string) ( $row['offer_token'] ?? '' ) );
    if ( $token !== '' ) return strtoupper( substr( preg_replace( '/[^a-zA-Z0-9]/', '', $token ), 0, 8 ) );
    $id = (int) ( $row['offer_link_id'] ?? 0 );
    return $id > 0 ? '#' . $id : '—';
}
function sz_aff_default_retention_days(): int { return max( 30, (int) get_option( 'sz_aff_default_retention_days', 30 ) ); }
function sz_aff_default_commission_pct(): float { return max( 0, min( 100, (float) get_option( 'sz_aff_default_commission_pct', 10 ) ) ); } // Configurável: Admin → Senderzz → COD → Taxas COD
function sz_aff_producer_retention_days( int $producer_id ): int {
    $v = (int) get_user_meta( $producer_id, 'sz_aff_retention_days', true );
    return $v >= 7 ? $v : (int) sz_aff_default_retention_days();
}

function sz_aff_producer_withdraw_fee( int $producer_id ): float {
    $v = get_user_meta( $producer_id, 'sz_aff_withdraw_fee', true );
    return $v !== '' ? (float) $v : (float) sz_aff_default_withdraw_fee();
}

function sz_aff_producer_default_commission_pct( int $producer_id ): float { foreach ( sz_aff_possible_producer_ids( $producer_id ) as $pid ) { $v = get_user_meta( (int) $pid, '_sz_aff_default_commission_pct', true ); if ( $v !== '' ) return max( 0, min( 100, (float) $v ) ); } return sz_aff_default_commission_pct(); }
function sz_aff_first_frustration_penalty(): float { return max( 0, (float) get_option( 'sz_aff_first_frustration_penalty', 5 ) ); }
function sz_aff_default_withdraw_fee(): float { return max( 0, (float) get_option( 'sz_aff_default_withdraw_fee', 2.99 ) ); }
function sz_aff_default_penalty_value(): float { return max( 0, (float) get_option( 'sz_aff_default_penalty_value', 5 ) ); }
/** Penalidade cobrada do PRODUTOR na 2ª+ frustração (separada da penalidade do afiliado). */
function sz_aff_producer_frustration_penalty(): float { return max( 0, (float) get_option( 'sz_aff_producer_frustration_penalty', 8 ) ); }


// v340: regras de frustração com configuração coletiva e particular.
if ( ! function_exists( 'sz_aff_float_option' ) ) {
function sz_aff_float_option( string $name, float $default = 0.0 ): float {
    $v = get_option( $name, $default );
    if ( is_string( $v ) ) { $v = str_replace( ',', '.', $v ); }
    return max( 0.0, (float) $v );
}}
if ( ! function_exists( 'sz_aff_frustration_overrides' ) ) {
function sz_aff_frustration_overrides( string $scope ): array {
    $raw = get_option( $scope === 'producer' ? 'sz_frustration_prod_overrides' : 'sz_frustration_aff_overrides', [] );
    if ( is_string( $raw ) ) {
        $decoded = json_decode( $raw, true );
        $raw = is_array( $decoded ) ? $decoded : [];
    }
    return is_array( $raw ) ? $raw : [];
}}
if ( ! function_exists( 'sz_aff_resolve_frustration_penalty' ) ) {
function sz_aff_resolve_frustration_penalty( string $scope, int $entity_id, int $count ): float {
    $scope = $scope === 'producer' ? 'producer' : 'affiliate';
    $field = $count <= 1 ? 'first' : 'repeat';
    $overrides = sz_aff_frustration_overrides( $scope );
    if ( $entity_id > 0 && isset( $overrides[ (string) $entity_id ] ) && is_array( $overrides[ (string) $entity_id ] ) ) {
        $row = $overrides[ (string) $entity_id ];
        if ( array_key_exists( $field, $row ) && $row[ $field ] !== '' && $row[ $field ] !== null ) {
            return max( 0.0, (float) str_replace( ',', '.', (string) $row[ $field ] ) );
        }
    }
    if ( $scope === 'producer' ) {
        return $count <= 1 ? sz_aff_float_option( 'sz_prod_first_frustration_penalty', 0.0 ) : sz_aff_producer_frustration_penalty();
    }
    return $count <= 1 ? sz_aff_first_frustration_penalty() : sz_aff_default_penalty_value();
}}
function sz_aff_is_producer_auto_approve( int $producer_id ): bool { foreach ( sz_aff_possible_producer_ids( $producer_id ) as $pid ) { if ( get_user_meta( (int) $pid, '_sz_aff_auto_approve', true ) === '1' ) return true; } return false; }
function sz_aff_portal_user_from_session(): ?object {
    if ( class_exists( '\\WC_MelhorEnvio\\Portal\\Portal_Auth' ) ) {
        try {
            $user = \WC_MelhorEnvio\Portal\Portal_Auth::get_current_user();
            if ( is_object( $user ) && ! empty( $user->id ) ) return $user;
        } catch ( Throwable $e ) {}
    }
    return null;
}

function sz_aff_current_producer_id( $portal_user = null ): int {
    global $wpdb;
    if ( is_object( $portal_user ) && ! empty( $portal_user->id ) ) return (int) $portal_user->id;

    $session_user = sz_aff_portal_user_from_session();
    if ( $session_user && ! empty( $session_user->id ) ) return (int) $session_user->id;

    $wp_id = get_current_user_id();
    if ( $wp_id > 0 ) {
        $portal_id = (int) $wpdb->get_var( $wpdb->prepare( "SELECT id FROM {$wpdb->prefix}senderzz_portal_users WHERE wp_user_id=%d LIMIT 1", $wp_id ) );
        if ( $portal_id > 0 ) return $portal_id;
        return $wp_id;
    }
    return 0;
}

function sz_aff_possible_producer_ids( int $producer_id ): array {
    global $wpdb;
    if ( $producer_id <= 0 ) return [];
    $row_by_id = $wpdb->get_row( $wpdb->prepare(
        "SELECT id, wp_user_id FROM {$wpdb->prefix}senderzz_portal_users WHERE id=%d LIMIT 1",
        $producer_id
    ), ARRAY_A );
    if ( $row_by_id ) {
        $ids = [ (int) $row_by_id['id'] ];
        if ( ! empty( $row_by_id['wp_user_id'] ) ) $ids[] = (int) $row_by_id['wp_user_id'];
        return array_values( array_unique( array_filter( array_map( 'absint', $ids ) ) ) );
    }
    $row_by_wp = $wpdb->get_row( $wpdb->prepare(
        "SELECT id, wp_user_id FROM {$wpdb->prefix}senderzz_portal_users WHERE wp_user_id=%d LIMIT 1",
        $producer_id
    ), ARRAY_A );
    if ( $row_by_wp ) {
        $ids = [ (int) $row_by_wp['id'] ];
        if ( ! empty( $row_by_wp['wp_user_id'] ) ) $ids[] = (int) $row_by_wp['wp_user_id'];
        return array_values( array_unique( array_filter( array_map( 'absint', $ids ) ) ) );
    }
    return [ $producer_id ];
}

function sz_aff_offer_label_from_row( array $row ): string {
    $id = (int) ( $row['id'] ?? 0 );
    $name = trim( (string) ( $row['name'] ?? '' ) );
    $components = trim( (string) ( $row['components_text'] ?? '' ) );
    if ( $name === '' ) $name = $components ?: 'Checkout #' . $id;
    $name = preg_replace( '/\s+—\s+(Motoboy|Correio|Expedição)$/iu', '', $name );
    return trim( $name ) ?: 'Checkout #' . $id;
}


function sz_aff_install(): void {
    global $wpdb;
    require_once ABSPATH . 'wp-admin/includes/upgrade.php';
    $charset = $wpdb->get_charset_collate();

    dbDelta( "CREATE TABLE " . sz_aff_table('sz_affiliates') . " (
        id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        producer_id BIGINT UNSIGNED NOT NULL,
        user_id BIGINT UNSIGNED NOT NULL,
        status VARCHAR(20) NOT NULL DEFAULT 'pending',
        commission_pct DECIMAL(10,2) NOT NULL DEFAULT 10.00,
        withdraw_fee DECIMAL(10,2) NOT NULL DEFAULT 2.99,
        penalty_value DECIMAL(10,2) NOT NULL DEFAULT 0.00,
        retention_days INT UNSIGNED NOT NULL DEFAULT 7,
        pix_key VARCHAR(190) NULL,
        bank_data LONGTEXT NULL,
        document VARCHAR(32) NULL,
        phone VARCHAR(40) NULL,
        lgpd_accepted TINYINT(1) NOT NULL DEFAULT 0,
        bank_status VARCHAR(20) NOT NULL DEFAULT 'pending',
        created_at DATETIME NOT NULL,
        approved_at DATETIME NULL,
        invited_by BIGINT UNSIGNED NULL,
        last_sale_at DATETIME NULL,
        total_sales DECIMAL(12,2) NOT NULL DEFAULT 0.00,
        frustration_count INT UNSIGNED NOT NULL DEFAULT 0,
        debt_amount DECIMAL(10,2) NOT NULL DEFAULT 0.00,
        offer_link_id BIGINT UNSIGNED NULL,
        offer_token VARCHAR(190) NULL,
        offer_name VARCHAR(255) NULL,
        offer_url TEXT NULL,
        allow_checkout_link TINYINT(1) NOT NULL DEFAULT 1,
        deleted_at DATETIME NULL,
        PRIMARY KEY (id),
        UNIQUE KEY uniq_producer_user (producer_id,user_id),
        KEY idx_producer_status (producer_id,status),
        KEY idx_user_status (user_id,status)
    ) $charset;" );


    // Tabela de vínculo permanente checkout_token → affiliate_id
    // Gravada quando o afiliado copia o link no portal. Resolve sem depender de cookie.
    dbDelta( "CREATE TABLE " . sz_aff_table('sz_affiliate_checkout_tokens') . " (
        id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        affiliate_id BIGINT UNSIGNED NOT NULL,
        checkout_token VARCHAR(190) NOT NULL,
        producer_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
        created_at DATETIME NOT NULL,
        PRIMARY KEY (id),
        UNIQUE KEY token_aff (checkout_token, affiliate_id),
        KEY idx_token (checkout_token),
        KEY idx_affiliate (affiliate_id)
    ) $charset;" );

    dbDelta( "CREATE TABLE " . sz_aff_table('sz_affiliate_link_commissions') . " (
        id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        affiliate_id BIGINT UNSIGNED NOT NULL,
        link_id BIGINT UNSIGNED NOT NULL,
        commission_pct DECIMAL(10,2) NOT NULL DEFAULT 0.00,
        created_at DATETIME NOT NULL,
        updated_at DATETIME NOT NULL,
        PRIMARY KEY (id),
        UNIQUE KEY uniq_aff_link (affiliate_id,link_id),
        KEY idx_link (link_id)
    ) $charset;" );



    dbDelta( "CREATE TABLE " . sz_aff_table('sz_affiliate_product_class_commissions') . " (
        id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        affiliate_id BIGINT UNSIGNED NOT NULL,
        scope VARCHAR(40) NOT NULL DEFAULT 'global',
        adjustment_pct DECIMAL(10,2) NOT NULL DEFAULT 0.00,
        created_at DATETIME NOT NULL,
        updated_at DATETIME NOT NULL,
        PRIMARY KEY (id),
        UNIQUE KEY uniq_aff_scope (affiliate_id,scope),
        KEY idx_scope (scope)
    ) $charset;" );

    dbDelta( "CREATE TABLE " . sz_aff_table('sz_affiliate_wallet') . " (
        id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        affiliate_id BIGINT UNSIGNED NOT NULL,
        balance DECIMAL(12,2) NOT NULL DEFAULT 0.00,
        pending_balance DECIMAL(12,2) NOT NULL DEFAULT 0.00,
        debt_amount DECIMAL(10,2) NOT NULL DEFAULT 0.00,
        created_at DATETIME NOT NULL,
        updated_at DATETIME NULL,
        PRIMARY KEY (id),
        UNIQUE KEY uniq_affiliate (affiliate_id)
    ) $charset;" );

    dbDelta( "CREATE TABLE " . sz_aff_table('sz_affiliate_transactions') . " (
        id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        affiliate_id BIGINT UNSIGNED NOT NULL,
        producer_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
        order_id BIGINT UNSIGNED NULL,
        type VARCHAR(30) NOT NULL,
        amount DECIMAL(12,2) NOT NULL DEFAULT 0.00,
        status VARCHAR(30) NOT NULL DEFAULT 'pending',
        available_at DATETIME NULL,
        created_at DATETIME NOT NULL,
        meta_json LONGTEXT NULL,
        PRIMARY KEY (id),
        UNIQUE KEY uniq_order_type_aff (affiliate_id,order_id,type),
        KEY idx_aff_status (affiliate_id,status),
        KEY idx_available (status,available_at),
        KEY idx_order (order_id)
    ) $charset;" );

    dbDelta( "CREATE TABLE " . sz_aff_table('sz_invite_links') . " (
        id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        producer_id BIGINT UNSIGNED NOT NULL,
        token VARCHAR(80) NOT NULL,
        offer_link_id BIGINT UNSIGNED NULL,
        offer_token VARCHAR(190) NULL,
        offer_name VARCHAR(255) NULL,
        offer_url TEXT NULL,
        uses INT UNSIGNED NOT NULL DEFAULT 0,
        expires_at DATETIME NOT NULL,
        created_at DATETIME NOT NULL,
        created_by BIGINT UNSIGNED NULL,
        allow_checkout_link TINYINT(1) NOT NULL DEFAULT 1,
        status VARCHAR(20) NOT NULL DEFAULT 'active',
        PRIMARY KEY (id),
        UNIQUE KEY token (token),
        KEY idx_producer (producer_id),
        KEY idx_expires (expires_at)
    ) $charset;" );

    dbDelta( "CREATE TABLE " . sz_aff_table('sz_admin_wallet') . " (
        id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        balance DECIMAL(12,2) NOT NULL DEFAULT 0.00,
        pending_balance DECIMAL(12,2) NOT NULL DEFAULT 0.00,
        created_at DATETIME NOT NULL,
        updated_at DATETIME NULL,
        PRIMARY KEY (id)
    ) $charset;" );

    dbDelta( "CREATE TABLE " . sz_aff_table('sz_admin_wallet_transactions') . " (
        id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        order_id BIGINT UNSIGNED NULL,
        producer_id BIGINT UNSIGNED NULL,
        affiliate_id BIGINT UNSIGNED NULL,
        type VARCHAR(30) NOT NULL,
        amount DECIMAL(12,2) NOT NULL DEFAULT 0.00,
        status VARCHAR(30) NOT NULL DEFAULT 'available',
        created_at DATETIME NOT NULL,
        meta_json LONGTEXT NULL,
        PRIMARY KEY (id),
        UNIQUE KEY uniq_order_type (order_id,type),
        KEY idx_status (status)
    ) $charset;" );

    dbDelta( "CREATE TABLE " . sz_aff_table('sz_affiliate_withdrawals') . " (
        id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        affiliate_id BIGINT UNSIGNED NOT NULL,
        amount DECIMAL(12,2) NOT NULL,
        fee DECIMAL(10,2) NOT NULL DEFAULT 2.99,
        net_amount DECIMAL(12,2) NOT NULL,
        status VARCHAR(20) NOT NULL DEFAULT 'pending',
        created_at DATETIME NOT NULL,
        decided_at DATETIME NULL,
        decided_by BIGINT UNSIGNED NULL,
        note TEXT NULL,
        PRIMARY KEY (id),
        KEY idx_status (status),
        KEY idx_affiliate (affiliate_id)
    ) $charset;" );

    $wpdb->query( "INSERT INTO " . sz_aff_table('sz_admin_wallet') . " (id,balance,pending_balance,created_at) VALUES (1,0,0,'" . esc_sql( sz_aff_now() ) . "') ON DUPLICATE KEY UPDATE id=id" );
    add_option( 'sz_aff_default_commission_pct', 10 );
    add_option( 'sz_aff_default_withdraw_fee', 2.99 );
    add_option( 'sz_aff_default_penalty_value', 5 );
    add_option( 'sz_aff_first_frustration_penalty', 5 );
    if ( get_option( 'sz_aff_default_retention_days', false ) === false ) add_option( 'sz_aff_default_retention_days', 30 );
    if ( (int) get_option( 'sz_aff_default_retention_days', 0 ) < 30 ) update_option( 'sz_aff_default_retention_days', 30 );
    if ( get_option( 'sz_aff_first_frustration_penalty', false ) === false ) add_option( 'sz_aff_first_frustration_penalty', 5 );
    if ( get_option( 'sz_aff_default_penalty_value', false ) === false ) add_option( 'sz_aff_default_penalty_value', 5 );
    update_option( 'sz_aff_db_version', SZ_AFF_DB_VERSION );
}

function sz_aff_maybe_install(): void {
    if ( get_option( 'sz_aff_db_version' ) !== SZ_AFF_DB_VERSION ) sz_aff_install();
}
add_action( 'init', 'sz_aff_maybe_install', 6 );

function sz_aff_create_role(): void {
    add_role( 'sz_affiliate', 'Afiliado Senderzz', [ 'read' => true ] );
}
add_action( 'init', 'sz_aff_create_role', 7 );

function sz_aff_generate_token(): string {
    for ( $i = 0; $i < 10; $i++ ) {
        $token = 'aff-' . strtolower( wp_generate_password( 28, false, false ) );
        global $wpdb;
        $exists = (int) $wpdb->get_var( $wpdb->prepare( "SELECT id FROM " . sz_aff_table('sz_invite_links') . " WHERE token=%s LIMIT 1", $token ) );
        if ( ! $exists ) return $token;
    }
    return 'aff-' . strtolower( md5( uniqid( 'senderzz', true ) ) );
}

function sz_aff_create_invite( int $producer_id, int $created_by = 0, int $offer_link_id = 0, bool $allow_checkout_link = true ): string|false {
    global $wpdb;
    if ( $producer_id <= 0 || $offer_link_id <= 0 ) return false;

    $offer = sz_aff_get_checkout_offer_for_producer( $producer_id, $offer_link_id );
    if ( empty( $offer['id'] ) || empty( $offer['url'] ) ) return false;

    $allow_checkout_link = $allow_checkout_link ? 1 : 0;

    // Convite fixo por oferta: se já existe, reutiliza o mesmo token.
    $existing = $wpdb->get_var( $wpdb->prepare(
        "SELECT token FROM " . sz_aff_table('sz_invite_links') . " WHERE producer_id=%d AND offer_link_id=%d AND status='active' ORDER BY id DESC LIMIT 1",
        $producer_id,
        (int) $offer['id']
    ) );
    if ( $existing ) {
        $wpdb->update( sz_aff_table('sz_invite_links'), [
            'offer_token'         => (string) ( $offer['token'] ?? '' ),
            'offer_name'          => (string) ( $offer['name'] ?? '' ),
            'offer_url'           => (string) ( $offer['url'] ?? '' ),
            'allow_checkout_link' => $allow_checkout_link,
            // Mantém compatibilidade com a coluna, mas link de convite não expira na UX/regra nova.
            'expires_at'          => '2099-12-31 23:59:59',
        ], [ 'producer_id' => $producer_id, 'offer_link_id' => (int) $offer['id'], 'status' => 'active' ], [ '%s','%s','%s','%d','%s' ], [ '%d','%d','%s' ] );
        return (string) $existing;
    }

    $token = sz_aff_generate_token();
    $insert_data = [
        'producer_id'         => $producer_id,
        'token'               => $token,
        'offer_link_id'       => (int) ( $offer['id'] ?? 0 ),
        'offer_token'         => (string) ( $offer['token'] ?? '' ),
        'offer_name'          => (string) ( $offer['name'] ?? '' ),
        'offer_url'           => (string) ( $offer['url'] ?? '' ),
        'uses'                => 0,
        'expires_at'          => '2099-12-31 23:59:59',
        'created_at'          => sz_aff_now(),
        'created_by'          => $created_by ?: get_current_user_id(),
        'allow_checkout_link' => $allow_checkout_link,
        'status'              => 'active',
    ];
    $ok = $wpdb->insert( sz_aff_table('sz_invite_links'), $insert_data, [ '%d','%s','%d','%s','%s','%s','%d','%s','%s','%d','%d','%s' ] );
    return $ok ? $token : false;
}

function sz_aff_create_default_invite( int $producer_id, int $created_by = 0 ): string|false {
    global $wpdb;
    if ( $producer_id <= 0 ) return false;

    $existing = $wpdb->get_var( $wpdb->prepare(
        "SELECT token FROM " . sz_aff_table('sz_invite_links') . " WHERE producer_id=%d AND status='active' AND (offer_link_id IS NULL OR offer_link_id=0) ORDER BY id DESC LIMIT 1",
        $producer_id
    ) );
    if ( $existing ) return (string) $existing;

    $token = sz_aff_generate_token();
    $ok = $wpdb->insert( sz_aff_table('sz_invite_links'), [
        'producer_id'         => $producer_id,
        'token'               => $token,
        'offer_link_id'       => null,
        'offer_token'         => '',
        // v258: grava nome do produtor para rastreabilidade futura de recompensas
        'offer_name'          => ( function_exists( 'sz_aff_get_producer_display_name' ) && $producer_id > 0 )
                                    ? ( sz_aff_get_producer_display_name( $producer_id ) ?: 'Convite padrão' )
                                    : 'Convite padrão',
        'offer_url'           => '',
        'uses'                => 0,
        'expires_at'          => '2099-12-31 23:59:59',
        'created_at'          => sz_aff_now(),
        'created_by'          => $created_by ?: get_current_user_id(),
        'allow_checkout_link' => 1,
        'status'              => 'active',
    ], [ '%d','%s', null, '%s','%s','%s','%d','%s','%s','%d','%d','%s' ] );
    return $ok ? $token : false;
}

function sz_aff_invite_url( string $token ): string { return home_url( '/convite/' . rawurlencode( $token ) ); }
function sz_aff_checkout_url_with_aff( string $url, int $affiliate_id ): string {
    // Codificar o ID do afiliado num token opaco de sessão (não expõe ID nem semântica)
    $token = sz_aff_encode_ref_token( $affiliate_id );
    return add_query_arg( 'r', $token, $url );
}

/**
 * Resolve a URL pública que o afiliado deve copiar.
 *
 * Regra Senderzz: o afiliado sempre usa o MESMO modelo/link público do produtor.
 * Se a oferta principal possui um link Motoboy vinculado (link_motoboy_id),
 * o link de afiliado deve apontar para essa URL Motoboy do produtor
 * (/checkouts/codsfpc/?sz=...&szm=1, por exemplo), apenas adicionando o
 * identificador opaco do afiliado via r=.
 *
 * Isso evita cair em modelos antigos como /checkouts/checkout/.
 */
function sz_aff_resolve_producer_checkout_url_for_affiliate( array $link ): string {
    global $wpdb;

    $table = $wpdb->prefix . 'senderzz_checkout_links';
    $cols  = $wpdb->get_col( "SHOW COLUMNS FROM `{$table}`", 0 );
    $cols  = is_array( $cols ) ? $cols : [];

    $linked_motoboy_id = absint( $link['link_motoboy_id'] ?? 0 );
    if ( $linked_motoboy_id > 0 && in_array( 'id', $cols, true ) ) {
        $mb = $wpdb->get_row( $wpdb->prepare( "SELECT * FROM `{$table}` WHERE id=%d LIMIT 1", $linked_motoboy_id ), ARRAY_A );
        if ( is_array( $mb ) && ! empty( $mb ) ) {
            $mb_url = trim( (string) ( $mb['url'] ?? '' ) );
            $mb_token = trim( (string) ( $mb['token'] ?? '' ) );
            if ( $mb_token === '' ) $mb_token = trim( (string) ( $mb['slug'] ?? '' ) );

            if ( $mb_url === '' && $mb_token !== '' ) {
                $post_id = absint( $mb['post_id'] ?? 0 );
                $base_url = $post_id ? get_permalink( $post_id ) : '';
                if ( ! $base_url ) {
                    // Fallback seguro: usa a mesma configuração do Motoboy sem CPF, quando existir.
                    $template_id = (int) get_option( 'senderzz_checkout_template_id_sem_cpf', 1075 );
                    $base_url = $template_id ? get_permalink( $template_id ) : '';
                    if ( ! $base_url ) { return ''; }
                }
                $mb_url = add_query_arg( [ 'sz' => $mb_token, 'szm' => '1' ], $base_url );
            }

            if ( $mb_url !== '' ) {
                // Garante o marcador Motoboy, preservando a URL/modelo do produtor.
                return add_query_arg( 'szm', '1', $mb_url );
            }
        }
    }

    $url = trim( (string) ( $link['url'] ?? '' ) );
    $token = trim( (string) ( $link['token'] ?? '' ) );
    if ( $token === '' ) $token = trim( (string) ( $link['slug'] ?? '' ) );
    if ( $url === '' && $token !== '' ) {
        $post_id = absint( $link['post_id'] ?? 0 );
        $base_url = $post_id ? get_permalink( $post_id ) : '';
        if ( ! $base_url ) {
            $base_checkout_id = (int) get_option( 'senderzz_checkout_template_id', 140 );
            $base_url = $base_checkout_id ? get_permalink( $base_checkout_id ) : '';
            if ( ! $base_url ) { return ''; }
        }
        $url = add_query_arg( 'sz', $token, $base_url );
    }

    return $url;
}

function sz_aff_encode_ref_token( int $affiliate_id ): string {
    // Token: base64url( affiliate_id XOR salt ) truncado — reversível internamente
    $salt = (string) get_option( 'sz_aff_ref_salt', '' );
    if ( ! $salt ) {
        $salt = bin2hex( random_bytes(8) );
        update_option( 'sz_aff_ref_salt', $salt );
    }
    $packed = pack( 'N', $affiliate_id ) . substr( $salt, 0, 4 );
    return rtrim( strtr( base64_encode( $packed ), '+/', '-_' ), '=' );
}

function sz_aff_decode_ref_token( string $token ): int {
    if ( strlen($token) < 4 ) return 0;
    $decoded = base64_decode( strtr( $token, '-_', '+/' ) . '==' );
    if ( strlen($decoded) < 4 ) return 0;
    $unpacked = unpack( 'Nid', substr($decoded, 0, 4) );
    return (int) ($unpacked['id'] ?? 0);
}

function sz_aff_get_checkout_offers_for_producer( int $producer_id ): array {
    global $wpdb;
    $t = $wpdb->prefix . 'senderzz_checkout_links';
    if ( function_exists( 'senderzz_portal_ensure_checkout_links_table' ) ) {
        senderzz_portal_ensure_checkout_links_table();
    }
    $exists = $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $t ) );
    if ( $exists !== $t ) return [];

    $cols = $wpdb->get_col( "SHOW COLUMNS FROM `{$t}`", 0 );
    $cols = is_array( $cols ) ? $cols : [];
    if ( ! in_array( 'id', $cols, true ) ) return [];

    $selects = [ 'id' ];
    foreach ( [ 'user_id','producer_id','post_id','name','slug','token','url','price_label','components_text','tipo','link_motoboy_id','created_at','payload','display_value' ] as $col ) {
        if ( in_array( $col, $cols, true ) ) $selects[] = $col;
    }

    $producer_ids = sz_aff_possible_producer_ids( $producer_id );
    if ( empty( $producer_ids ) ) return [];

    $where_parts = [];
    foreach ( [ 'producer_id', 'user_id' ] as $owner_col ) {
        if ( ! in_array( $owner_col, $cols, true ) ) continue;
        $placeholders = implode( ',', array_fill( 0, count( $producer_ids ), '%d' ) );
        $where_parts[] = "`{$owner_col}` IN ({$placeholders})";
    }
    if ( empty( $where_parts ) ) return [];

    $params = [];
    foreach ( $where_parts as $_ ) {
        foreach ( $producer_ids as $pid ) $params[] = $pid;
    }

    $where = '(' . implode( ' OR ', $where_parts ) . ')';
    if ( in_array( 'tipo', $cols, true ) ) {
        // Oferta é o checkout criado no menu Checkouts. Linhas auxiliares de motoboy não entram no seletor.
        $where .= " AND (tipo IS NULL OR tipo='' OR tipo='correio' OR tipo='expedicao' OR tipo='expedição')";
    }

    $order = in_array( 'created_at', $cols, true ) ? 'created_at DESC,id DESC' : 'id DESC';
    $sql = "SELECT " . implode( ',', array_map( static fn( $c ) => "`{$c}`", $selects ) ) . " FROM `{$t}` WHERE {$where} ORDER BY {$order} LIMIT 500";
    $rows = $wpdb->get_results( $wpdb->prepare( $sql, $params ), ARRAY_A );
    if ( ! is_array( $rows ) ) return [];

    $out = [];
    $seen = [];
    foreach ( $rows as $row ) {
        $id = (int) ( $row['id'] ?? 0 );
        if ( $id <= 0 ) continue;

        $name = sz_aff_offer_label_from_row( $row );
        $price = trim( (string) ( $row['price_label'] ?? '' ) );
        if ( ( $price === '' || $price === '—' ) && isset( $row['display_value'] ) && (float) $row['display_value'] > 0 ) {
            $price = sz_aff_money( (float) $row['display_value'] );
        }

        $url = function_exists( 'sz_aff_resolve_producer_checkout_url_for_affiliate' ) ? sz_aff_resolve_producer_checkout_url_for_affiliate( $row ) : trim( (string) ( $row['url'] ?? '' ) );
        $token = trim( (string) ( $row['token'] ?? '' ) );
        if ( $token === '' ) $token = trim( (string) ( $row['slug'] ?? '' ) );
        if ( $url === '' ) continue;

        // Deduplica pelo ID real do checkout criado; se a mesma oferta aparecer por producer_id e user_id, mostra uma vez.
        $key = 'id:' . $id;
        if ( isset( $seen[ $key ] ) ) continue;
        $seen[ $key ] = true;

        $out[] = [
            'id' => $id,
            'name' => $name,
            'token' => $token,
            'url' => $url,
            'price_label' => $price,
            'components_text' => trim( (string) ( $row['components_text'] ?? '' ) ),
            'tipo' => (string) ( $row['tipo'] ?? '' ),
        ];
    }
    return $out;
}

function sz_aff_get_checkout_offer_for_producer( int $producer_id, int $offer_link_id ): array {
    $offers = sz_aff_get_checkout_offers_for_producer( $producer_id );
    if ( empty( $offers ) || $offer_link_id <= 0 ) return [];
    foreach ( $offers as $offer ) {
        if ( (int) $offer['id'] === $offer_link_id ) {
            $token = (string) ( $offer['token'] ?: '' );
            return [
                'id'    => (int) $offer['id'],
                'token' => $token,
                'name'  => (string) $offer['name'],
                'url'   => (string) $offer['url'],
                'price_label' => (string) ( $offer['price_label'] ?? '' ),
            ];
        }
    }
    return [];
}

function sz_aff_set_cookie_from_request(): void {
    $token = '';
    // Parâmetro novo: r= (token opaco codificado)
    if ( isset( $_GET['r'] ) ) {
        $raw = sanitize_text_field( wp_unslash( $_GET['r'] ) );
        $decoded_id = sz_aff_decode_ref_token( $raw );
        // Segurança: validar que o affiliate_id existe e está ativo antes de aceitar o token
        if ( $decoded_id > 0 ) {
            global $wpdb;
            $valid = (int) $wpdb->get_var( $wpdb->prepare(
                "SELECT id FROM " . sz_aff_table('sz_affiliates') . " WHERE id=%d AND status='active' AND deleted_at IS NULL LIMIT 1",
                $decoded_id
            ) );
            if ( $valid ) $token = 'id:' . $decoded_id;
        }
    }
    // Retrocompatibilidade: sz_aff_token= e sz_aff=
    if ( ! $token && isset( $_GET['sz_aff_token'] ) ) $token = sanitize_text_field( wp_unslash( $_GET['sz_aff_token'] ) );
    if ( ! $token && isset( $_GET['sz_aff'] ) ) {
        $aff_id = absint( $_GET['sz_aff'] );
        if ( $aff_id ) {
            global $wpdb;
            $valid = (int) $wpdb->get_var( $wpdb->prepare(
                "SELECT id FROM " . sz_aff_table('sz_affiliates') . " WHERE id=%d AND status='active' AND deleted_at IS NULL LIMIT 1",
                $aff_id
            ) );
            if ( $valid ) $token = 'id:' . $aff_id;
        }
    }
    if ( $token ) {
        // Nunca grava cookie de rastreamento para admins ou produtores logados.
        // Isso evita que testes contaminem pedidos de clientes reais.
        $current_user_id = get_current_user_id();
        $is_privileged   = false;
        if ( $current_user_id ) {
            if ( user_can( $current_user_id, 'manage_woocommerce' ) || user_can( $current_user_id, 'manage_options' ) ) {
                $is_privileged = true;
            }
            if ( ! $is_privileged ) {
                global $wpdb;
                $is_producer = (int) $wpdb->get_var( $wpdb->prepare(
                    "SELECT COUNT(*) FROM " . sz_aff_table('sz_affiliates') . " WHERE producer_id=%d AND user_id=%d LIMIT 1",
                    $current_user_id, $current_user_id
                ) );
                // Também bloqueia se o usuário É o produtor dono dos checkouts
                if ( ! $is_producer ) {
                    $is_producer = (int) $wpdb->get_var( $wpdb->prepare(
                        "SELECT COUNT(*) FROM {$wpdb->prefix}senderzz_checkout_links WHERE user_id=%d LIMIT 1",
                        $current_user_id
                    ) );
                }
                if ( $is_producer ) $is_privileged = true;
            }
        }
        if ( ! $is_privileged ) {
            setcookie( SZ_AFF_COOKIE, $token, time() + 30 * DAY_IN_SECONDS, COOKIEPATH ?: '/', COOKIE_DOMAIN, is_ssl(), true );
            $_COOKIE[ SZ_AFF_COOKIE ] = $token;
            if ( function_exists( 'WC' ) && WC() && WC()->session ) {
                WC()->session->set( SZ_AFF_COOKIE, $token );
            }
        }
    }
}
add_action( 'init', 'sz_aff_set_cookie_from_request', 1 );

/**
 * Migração retroativa: popula sz_affiliate_checkout_tokens a partir dos metas
 * _senderzz_offer_token + _sz_affiliate_id já gravados nos pedidos existentes.
 * Roda uma única vez via option flag.
 */
function sz_aff_migrate_checkout_tokens(): void {
    if ( get_option( 'sz_aff_checkout_tokens_migrated_v1' ) ) return;
    global $wpdb;
    $table = sz_aff_table('sz_affiliate_checkout_tokens');
    // Verifica se a tabela existe antes de tentar inserir
    if ( ! $wpdb->get_var( "SHOW TABLES LIKE '{$table}'" ) ) return;

    $rows = $wpdb->get_results(
        "SELECT mt.meta_value AS checkout_token,
                ma.meta_value AS affiliate_id,
                mp.meta_value AS producer_id
         FROM {$wpdb->prefix}wc_orders_meta mt
         INNER JOIN {$wpdb->prefix}wc_orders_meta ma
             ON ma.order_id = mt.order_id AND ma.meta_key = '_sz_affiliate_id'
         LEFT JOIN {$wpdb->prefix}wc_orders_meta mp
             ON mp.order_id = mt.order_id AND mp.meta_key = '_sz_aff_producer_id'
         WHERE mt.meta_key = '_senderzz_offer_token'
           AND mt.meta_value != ''
           AND ma.meta_value > 0
         GROUP BY mt.meta_value, ma.meta_value",
        ARRAY_A
    ) ?: [];

    foreach ( $rows as $row ) {
        $token      = sanitize_text_field( $row['checkout_token'] ?? '' );
        $aff_id     = absint( $row['affiliate_id'] ?? 0 );
        $producer   = absint( $row['producer_id'] ?? 0 );
        if ( $token && $aff_id ) {
            sz_aff_register_checkout_token( $token, $aff_id, $producer );
        }
    }
    update_option( 'sz_aff_checkout_tokens_migrated_v1', 1 );
}
add_action( 'init', 'sz_aff_migrate_checkout_tokens', 20 );

function sz_aff_current_tracking_token(): string {
    $raw = isset( $_COOKIE[ SZ_AFF_COOKIE ] ) ? sanitize_text_field( wp_unslash( $_COOKIE[ SZ_AFF_COOKIE ] ) ) : '';
    if ( ! $raw && function_exists( 'WC' ) && WC() && WC()->session ) {
        $raw = sanitize_text_field( (string) WC()->session->get( SZ_AFF_COOKIE, '' ) );
    }
    foreach ( [ $_GET['r'] ?? null, $_POST['r'] ?? null, $_POST['sz_aff_ref'] ?? null ] as $maybe_ref ) {
        if ( $raw || $maybe_ref === null ) continue;
        $decoded_id = sz_aff_decode_ref_token( sanitize_text_field( wp_unslash( $maybe_ref ) ) );
        if ( $decoded_id > 0 ) $raw = 'id:' . $decoded_id;
    }
    if ( ! $raw && isset( $_POST['sz_aff'] ) ) {
        $aff_id = absint( $_POST['sz_aff'] );
        if ( $aff_id > 0 ) $raw = 'id:' . $aff_id;
    }
    return $raw;
}

function sz_aff_get_tracked_affiliate_row(): ?array {
    global $wpdb;
    $raw = sz_aff_current_tracking_token();
    if ( ! $raw ) return null;
    if ( substr( $raw, 0, 3 ) === 'id:' ) {
        $id = absint( substr( $raw, 3 ) );
        if ( ! $id ) return null;
        $row = $wpdb->get_row( $wpdb->prepare( "SELECT * FROM " . sz_aff_table('sz_affiliates') . " WHERE id=%d AND status='active' LIMIT 1", $id ), ARRAY_A );
        return $row ?: null;
    }
    $row = $wpdb->get_row( $wpdb->prepare( "SELECT a.* FROM " . sz_aff_table('sz_invite_links') . " l INNER JOIN " . sz_aff_table('sz_affiliates') . " a ON a.producer_id=l.producer_id WHERE l.token=%s AND a.status='active' ORDER BY a.id DESC LIMIT 1", $raw ), ARRAY_A );
    return $row ?: null;
}

function sz_aff_get_cookie_affiliate_id( int $producer_id ): int {
    global $wpdb;
    $raw = sz_aff_current_tracking_token();
    if ( ! $raw ) return 0;
    $producer_ids = function_exists( 'sz_aff_possible_producer_ids' ) ? sz_aff_possible_producer_ids( $producer_id ) : [ $producer_id ];
    $producer_ids = array_values( array_unique( array_filter( array_map( 'absint', $producer_ids ) ) ) );
    if ( empty( $producer_ids ) ) return 0;
    $ph = implode( ',', array_fill( 0, count( $producer_ids ), '%d' ) );
    if ( substr( $raw, 0, 3 ) === 'id:' ) {
        $id = absint( substr( $raw, 3 ) );
        if ( ! $id ) return 0;
        return (int) $wpdb->get_var( $wpdb->prepare( "SELECT id FROM " . sz_aff_table('sz_affiliates') . " WHERE id=%d AND producer_id IN ({$ph}) AND status='active' LIMIT 1", array_merge( [ $id ], $producer_ids ) ) );
    }
    return (int) $wpdb->get_var( $wpdb->prepare( "SELECT a.id FROM " . sz_aff_table('sz_invite_links') . " l INNER JOIN " . sz_aff_table('sz_affiliates') . " a ON a.producer_id=l.producer_id WHERE l.token=%s AND a.producer_id IN ({$ph}) AND a.status='active' ORDER BY a.id DESC LIMIT 1", array_merge( [ $raw ], $producer_ids ) ) );
}

function sz_aff_resolve_order_producer_id( WC_Order $order ): int {
    foreach ( [ '_sz_aff_producer_id', '_senderzz_owner_user_id', '_sz_portal_user_id', '_senderzz_producer_id', '_senderzz_checkout_user_id' ] as $key ) {
        $producer_id = (int) $order->get_meta( $key, true );
        if ( $producer_id ) return $producer_id;
    }
    if ( function_exists( 'senderzz_pw_get_owner_user_id_for_order' ) ) {
        try { $producer_id = (int) senderzz_pw_get_owner_user_id_for_order( $order, 0 ); if ( $producer_id ) return $producer_id; } catch ( Throwable $e ) {}
    }
    $producer_id = (int) $order->get_customer_id();
    return $producer_id;
}

function sz_aff_attach_affiliate_to_order( $order, $data = [] ): void {
    if ( ! $order instanceof WC_Order ) return;

    // Guard: processa uma única vez por pedido por request
    static $sz_processed_orders = [];
    $order_id = $order->get_id();
    if ( $order_id && isset( $sz_processed_orders[ $order_id ] ) ) return;
    if ( $order_id ) $sz_processed_orders[ $order_id ] = true;

    // Prioridade 1: afiliado explícito do link (r=/cookie/sessão).
    // O token do checkout pertence ao produtor e pode estar ligado a vários afiliados;
    // portanto o r= é a fonte correta para atribuir o pedido.
    $aff    = null;
    $aff_id = 0;
    $offer_token_order = (string) ( $order->get_meta( '_senderzz_offer_token', true ) ?: '' );

    $aff = function_exists( 'sz_aff_get_tracked_affiliate_row' ) ? sz_aff_get_tracked_affiliate_row() : null;
    $aff_id = is_array( $aff ) ? absint( $aff['id'] ?? 0 ) : 0;
    if ( $aff_id && is_array( $aff ) ) {
        $producer_from_order = function_exists( 'sz_aff_resolve_order_producer_id' ) ? sz_aff_resolve_order_producer_id( $order ) : 0;
        if ( $producer_from_order && absint( $aff['producer_id'] ?? 0 ) !== $producer_from_order ) {
            // Cookie é de afiliado de outro produtor — descarta.
            $aff    = null;
            $aff_id = 0;
        } elseif ( $offer_token_order && function_exists( 'sz_aff_register_checkout_token' ) ) {
            // Garante que o token do checkout também fique vinculado ao afiliado correto para auditoria/futuro.
            sz_aff_register_checkout_token( $offer_token_order, $aff_id, absint( $aff['producer_id'] ?? 0 ) );
        }
    }

    // Prioridade 2: offer_token do pedido somente quando conseguir resolver sem ambiguidade.
    if ( ! $aff_id && $offer_token_order && function_exists( 'sz_aff_resolve_affiliate_by_checkout_token' ) ) {
        $aff_by_token = sz_aff_resolve_affiliate_by_checkout_token( $offer_token_order );
        if ( is_array( $aff_by_token ) && ! empty( $aff_by_token['id'] ) ) {
            $aff    = $aff_by_token;
            $aff_id = absint( $aff_by_token['id'] );
        }
    }

    // Fallback: quando já existe meta no pedido, reaproveita para recalcular/exibir comissão.
    if ( ! $aff_id ) {
        $existing_id = absint( $order->get_meta( '_sz_affiliate_id', true ) ?: $order->get_meta( '_sz_affiliate_ref', true ) );
        if ( $existing_id && function_exists( 'sz_aff_get_affiliate_row' ) ) {
            $aff = sz_aff_get_affiliate_row( $existing_id );
            $aff_id = is_array( $aff ) ? absint( $aff['id'] ?? 0 ) : 0;
        }
    }

    // Fallback legado: resolve por produtor + cookie antigo.
    if ( ! $aff_id ) {
        $producer_guess = function_exists( 'sz_aff_resolve_order_producer_id' ) ? sz_aff_resolve_order_producer_id( $order ) : 0;
        $cookie_id = $producer_guess && function_exists( 'sz_aff_get_cookie_affiliate_id' ) ? sz_aff_get_cookie_affiliate_id( $producer_guess ) : 0;
        if ( $cookie_id && function_exists( 'sz_aff_get_affiliate_row' ) ) {
            $aff = sz_aff_get_affiliate_row( $cookie_id );
            $aff_id = is_array( $aff ) ? absint( $aff['id'] ?? 0 ) : 0;
        }
    }

    if ( ! is_array( $aff ) || ! $aff_id || ( $aff['status'] ?? '' ) !== 'active' ) return;

    $producer_id = absint( $aff['producer_id'] ?? 0 );
    if ( ! $producer_id && function_exists( 'sz_aff_resolve_order_producer_id' ) ) {
        $producer_id = absint( sz_aff_resolve_order_producer_id( $order ) );
    }
    if ( ! $producer_id ) return;

    $link_id_for_commission = function_exists( 'sz_aff_order_checkout_link_id' ) ? sz_aff_order_checkout_link_id( $order ) : 0;
    $pct = function_exists( 'sz_aff_effective_affiliate_commission_pct' )
        ? sz_aff_effective_affiliate_commission_pct( $aff_id, $aff )
        : max( 0, min( 100, (float) ( $aff['commission_pct'] ?? 0 ) ) );
    if ( $link_id_for_commission ) $order->update_meta_data( '_senderzz_checkout_link_id', $link_id_for_commission );

    $base = (float) $order->get_meta( '_senderzz_offer_value', true );
    if ( $base <= 0 ) {
        $base = (float) $order->get_total();
    }
    $commission_amount = $pct > 0 ? round( max( 0, $base ) * $pct / 100, 2 ) : 0.0;

    $order->update_meta_data( '_sz_affiliate_id', $aff_id );
    $order->update_meta_data( '_sz_affiliate_ref', $aff_id );
    $order->update_meta_data( '_sz_affiliate_user_id', absint( $aff['user_id'] ?? 0 ) );
    $order->update_meta_data( '_sz_aff_producer_id', $producer_id );
    $order->update_meta_data( '_sz_aff_commission_pct', $pct );
    if ( $commission_amount > 0 ) {
        $order->update_meta_data( '_sz_aff_commission', $commission_amount );
    }
    // Metadados completos para gestão no WooCommerce admin
    $aff_wp_user = get_userdata( absint( $aff['user_id'] ?? 0 ) );
    $aff_name    = $aff_wp_user ? $aff_wp_user->display_name : ( 'Afiliado #' . $aff_id );
    $aff_email   = $aff_wp_user ? $aff_wp_user->user_email : '';
    $aff_doc     = (string) ( $aff['document'] ?? get_user_meta( absint( $aff['user_id'] ?? 0 ), 'sz_document', true ) );
    $prod_name   = function_exists( 'sz_aff_get_producer_display_name' ) ? sz_aff_get_producer_display_name( $producer_id ) : 'Produtor #' . $producer_id;
    $order->update_meta_data( '_sz_aff_name',          $aff_name );
    $order->update_meta_data( '_sz_aff_email',         $aff_email );
    $order->update_meta_data( '_sz_aff_document',      $aff_doc );
    $order->update_meta_data( '_sz_aff_producer_name', $prod_name );
    $order->update_meta_data( '_sz_aff_order_total',   $base );
}
function sz_aff_attach_affiliate_to_order_id( $order_id ): void {
    $order = function_exists( 'wc_get_order' ) ? wc_get_order( $order_id ) : null;
    if ( $order instanceof WC_Order ) {
        sz_aff_attach_affiliate_to_order( $order, [] );
        $order->save();
    }
}

add_action( 'woocommerce_checkout_create_order', 'sz_aff_attach_affiliate_to_order', 9999, 2 );
add_action( 'woocommerce_checkout_order_processed', 'sz_aff_attach_affiliate_to_order_id', 9999, 1 );
add_action( 'woocommerce_checkout_update_order_meta', 'sz_aff_attach_affiliate_to_order_id', 9999, 1 );
add_action( 'woocommerce_thankyou', 'sz_aff_attach_affiliate_to_order_id', 1, 1 );

function sz_aff_get_order_fee_total( WC_Order $order ): float {
    // _sz_mb_taxa_total já contém o total completo (entrega + manuseio + percentual).
    // _sz_taxa_total é uma meta de compatibilidade que NÃO inclui a taxa percentual
    // e portanto NÃO deve ser somada para evitar double-count e subcontagem simultâneos.
    // Fonte única de taxa motoboy: _sz_mb_taxa_total.
    $mb_total = (float) $order->get_meta( '_sz_mb_taxa_total', true );

    // Taxas de expedição Melhor Envio (não motoboy)
    $other_keys = [ '_senderzz_taxa_total', '_sz_senderzz_fee', '_tpc_total_fee' ];
    $other_total = 0.0;
    foreach ( $other_keys as $key ) $other_total += (float) $order->get_meta( $key, true );

    $total = $mb_total > 0 ? $mb_total + $other_total : $other_total;

    // Fees WooCommerce (fallback para pedidos sem meta gravada)
    if ( $total <= 0 ) {
        foreach ( $order->get_fees() as $fee ) {
            $name = strtolower( $fee->get_name() );
            if ( str_contains( $name, 'senderzz' ) || str_contains( $name, 'motoboy' ) || str_contains( $name, 'frete' ) ) {
                $total += (float) $fee->get_total();
            }
        }
    }

    return round( max( 0, $total ), 2 );
}

function sz_aff_credit_admin_wallet( int $order_id, int $producer_id, int $affiliate_id, float $amount, array $meta = [] ): void {
    global $wpdb;
    if ( $amount <= 0 ) return;
    $ok = $wpdb->insert( sz_aff_table('sz_admin_wallet_transactions'), [
        'order_id' => $order_id, 'producer_id' => $producer_id, 'affiliate_id' => $affiliate_id,
        'type' => 'senderzz_fee', 'amount' => $amount, 'status' => 'available', 'created_at' => sz_aff_now(),
        'meta_json' => wp_json_encode( $meta, JSON_UNESCAPED_UNICODE|JSON_UNESCAPED_SLASHES ),
    ], [ '%d','%d','%d','%s','%f','%s','%s','%s' ] );
    if ( $ok ) $wpdb->query( $wpdb->prepare( "UPDATE " . sz_aff_table('sz_admin_wallet') . " SET balance=balance+%f, updated_at=%s WHERE id=1", $amount, sz_aff_now() ) );
}

function sz_aff_get_affiliate_row( int $affiliate_id ): ?array {
    global $wpdb;
    $row = $wpdb->get_row( $wpdb->prepare( "SELECT * FROM " . sz_aff_table('sz_affiliates') . " WHERE id=%d LIMIT 1", $affiliate_id ), ARRAY_A );
    return $row ?: null;
}

function sz_aff_ensure_wallet( int $affiliate_id ): void {
    global $wpdb;
    $wpdb->query( $wpdb->prepare( "INSERT INTO " . sz_aff_table('sz_affiliate_wallet') . " (affiliate_id,balance,pending_balance,debt_amount,created_at) VALUES (%d,0,0,0,%s) ON DUPLICATE KEY UPDATE affiliate_id=affiliate_id", $affiliate_id, sz_aff_now() ) );
}

function sz_aff_split_order_on_completed( int $order_id ): void {
    if ( ! function_exists( 'wc_get_order' ) ) return;
    $order = wc_get_order( $order_id );
    if ( ! $order instanceof WC_Order ) return;
    if ( $order->get_meta( '_sz_aff_split_done', true ) === '1' ) return;

    // Lock atômico: tenta marcar como processando via DB antes de qualquer cálculo.
    // Evita double-credit em ambientes com múltiplos workers ou hooks duplicados.
    global $wpdb;
    $lock_key = 'sz_aff_split_lock_' . $order_id;
    $locked = $wpdb->query( $wpdb->prepare(
        "INSERT IGNORE INTO {$wpdb->options} (option_name, option_value, autoload) VALUES (%s, %s, 'no')",
        $lock_key,
        sz_aff_now()
    ) );
    if ( ! $locked ) return; // Outro worker já está processando

    try {
        // Re-verificar após lock (double-check)
        $order->read_meta_data( true );
        if ( $order->get_meta( '_sz_aff_split_done', true ) === '1' ) {
            $wpdb->delete( $wpdb->options, [ 'option_name' => $lock_key ], [ '%s' ] );
            return;
        }

        sz_aff_split_order_execute( $order_id, $order );
    } finally {
        $wpdb->delete( $wpdb->options, [ 'option_name' => $lock_key ], [ '%s' ] );
    }
}


if ( ! function_exists( 'sz_aff_order_transaction_fee_total' ) ) {
    function sz_aff_order_transaction_fee_total( WC_Order $order ): float {
        $tx = (float) $order->get_meta( '_sz_mb_taxa_adicional', true );
        if ( $tx <= 0 ) $tx = (float) $order->get_meta( '_sz_mb_taxa_transacao', true );
        return round( max( 0, $tx ), 2 );
    }
}

if ( ! function_exists( 'sz_aff_order_transaction_fee_mode' ) ) {
    function sz_aff_order_transaction_fee_mode( WC_Order $order ): string {
        $mode = sanitize_key( (string) $order->get_meta( '_sz_mb_taxa_transacao_modo', true ) );
        if ( function_exists( 'sz_mb_sanitize_taxa_transacao_mode' ) ) {
            $mode = sz_mb_sanitize_taxa_transacao_mode( $mode ?: 'split_producer_affiliate' );
        }
        return $mode ?: 'split_producer_affiliate';
    }
}

if ( ! function_exists( 'sz_aff_split_transaction_fee_parts' ) ) {
    /**
     * Divide a taxa percentual entre afiliado/produtor somente quando o pedido novo
     * trouxe o modo split_producer_affiliate. Pedidos antigos sem a meta ficam no
     * comportamento legado: 100% da transação no produtor.
     */
    function sz_aff_split_transaction_fee_parts( WC_Order $order, float $gross, float $commission_gross, int $affiliate_id = 0 ): array {
        $tx_total = sz_aff_order_transaction_fee_total( $order );
        $mode     = sz_aff_order_transaction_fee_mode( $order );
        $aff_tx   = 0.0;
        if ( $affiliate_id > 0 && $tx_total > 0 && $gross > 0 && $commission_gross > 0 && $mode === 'split_producer_affiliate' ) {
            $pct = (float) $order->get_meta( '_sz_mb_taxa_percentual', true );
            if ( $pct > 0 ) {
                $aff_tx = round( $commission_gross * ( $pct / 100 ), 2 );
            } else {
                $aff_tx = round( $tx_total * min( 1, $commission_gross / $gross ), 2 );
            }
            $aff_tx = min( $tx_total, max( 0, $aff_tx ) );
        }
        $producer_tx = round( max( 0, $tx_total - $aff_tx ), 2 );
        return [
            'mode'        => $mode,
            'tx_total'    => round( $tx_total, 2 ),
            'affiliate'   => round( $aff_tx, 2 ),
            'producer'    => $producer_tx,
        ];
    }
}

function sz_aff_split_order_execute( int $order_id, WC_Order $order ): void {
    global $wpdb;

    $affiliate_id = (int) $order->get_meta( '_sz_affiliate_id', true );
    $producer_id  = (int) $order->get_meta( '_sz_aff_producer_id', true );
    if ( ! $producer_id ) $producer_id = sz_aff_resolve_order_producer_id( $order );

    $aff = $affiliate_id ? sz_aff_get_affiliate_row( $affiliate_id ) : null;
    if ( $affiliate_id && ( ! $aff || $aff['status'] !== 'active' ) ) $affiliate_id = 0;
    if ( $affiliate_id && $aff ) {
        $valid_producers = function_exists( 'sz_aff_possible_producer_ids' ) ? sz_aff_possible_producer_ids( $producer_id ) : [ $producer_id ];
        $valid_producers[] = (int) $aff['producer_id'];
        $valid_producers = array_unique( array_filter( array_map( 'absint', $valid_producers ) ) );
        if ( ! in_array( (int) $aff['producer_id'], $valid_producers, true ) ) $affiliate_id = 0;
        else $producer_id = (int) $aff['producer_id'];
    }

    $gross = round( (float) $order->get_total(), 2 );
    $fees  = sz_aff_get_order_fee_total( $order );
    $net   = max( 0, round( $gross - $fees, 2 ) );
    $retention_days = $producer_id ? sz_aff_producer_retention_days( $producer_id ) : sz_aff_default_retention_days();
    $available_at = ( new DateTimeImmutable( 'now', new DateTimeZone('America/Sao_Paulo') ) )
        ->modify( '+' . (int)$retention_days . ' days' )
        ->format( 'Y-m-d H:i:s' );

    $commission_gross = 0.0;
    $commission       = 0.0; // líquido creditado ao afiliado
    $pct_for_tx       = 0.0;
    $tx_parts         = [ 'mode' => 'split_producer_affiliate', 'tx_total' => 0.0, 'affiliate' => 0.0, 'producer' => sz_aff_order_transaction_fee_total( $order ) ];

    if ( $affiliate_id && $aff ) {
        $pct_for_tx = function_exists( 'sz_aff_effective_affiliate_commission_pct' )
            ? sz_aff_effective_affiliate_commission_pct( (int) $aff['id'], $aff )
            : (float) $aff['commission_pct'];

        // Comissão bruta do afiliado continua baseada no valor do pedido.
        $commission_gross = round( max( 0, $gross ) * ( $pct_for_tx / 100 ), 2 );
        // A taxa de transação só é dividida quando o pedido novo trouxe essa configuração.
        $tx_parts = sz_aff_split_transaction_fee_parts( $order, $gross, $commission_gross, $affiliate_id );
        $commission = max( 0, round( $commission_gross - (float) $tx_parts['affiliate'], 2 ) );

        if ( $commission > 0 ) {
            sz_aff_ensure_wallet( $affiliate_id );
            $tx_table = sz_aff_table('sz_affiliate_transactions');
            $wallet_table = sz_aff_table('sz_affiliate_wallet');
            $now = sz_aff_now();
            $meta_json = wp_json_encode( [
                'gross' => $gross,
                'fees' => $fees,
                'net' => $net,
                'commission_pct' => $pct_for_tx,
                'commission_gross' => $commission_gross,
                'transaction_fee_mode' => (string) $tx_parts['mode'],
                'transaction_fee_affiliate' => (float) $tx_parts['affiliate'],
                'transaction_fee_producer' => (float) $tx_parts['producer'],
                'transaction_fee_total' => (float) $tx_parts['tx_total'],
                'calc_mode' => ( (string) $tx_parts['mode'] === 'split_producer_affiliate' )
                    ? 'gross_affiliate_split_transaction_fee'
                    : 'gross_affiliate_fees_producer',
            ], JSON_UNESCAPED_UNICODE|JSON_UNESCAPED_SLASHES );

            $existing = $wpdb->get_row( $wpdb->prepare(
                "SELECT id, amount, status FROM {$tx_table} WHERE affiliate_id=%d AND order_id=%d AND type='commission' LIMIT 1",
                $affiliate_id, $order_id
            ), ARRAY_A );

            if ( $existing ) {
                $old_amount = (float) ( $existing['amount'] ?? 0 );
                $delta = round( $commission - $old_amount, 2 );
                $wpdb->update( $tx_table, [
                    'producer_id'  => $producer_id,
                    'amount'       => $commission,
                    'status'       => in_array( (string) ( $existing['status'] ?? '' ), [ 'pending', 'available' ], true ) ? (string) $existing['status'] : 'pending',
                    'available_at' => $available_at,
                    'meta_json'    => $meta_json,
                ], [ 'id' => (int) $existing['id'] ], [ '%d','%f','%s','%s','%s' ], [ '%d' ] );
                if ( abs( $delta ) >= 0.01 && ( $existing['status'] ?? 'pending' ) === 'pending' ) {
                    $wpdb->query( $wpdb->prepare( "UPDATE {$wallet_table} SET pending_balance=pending_balance+%f, updated_at=%s WHERE affiliate_id=%d", $delta, $now, $affiliate_id ) );
                } elseif ( abs( $delta ) >= 0.01 && ( $existing['status'] ?? '' ) === 'available' ) {
                    $wpdb->query( $wpdb->prepare( "UPDATE {$wallet_table} SET balance=balance+%f, updated_at=%s WHERE affiliate_id=%d", $delta, $now, $affiliate_id ) );
                }
                $ok = true;
            } else {
                $ok = $wpdb->insert( $tx_table, [
                    'affiliate_id' => $affiliate_id, 'producer_id' => $producer_id, 'order_id' => $order_id,
                    'type' => 'commission', 'amount' => $commission, 'status' => 'pending',
                    'available_at' => $available_at, 'created_at' => $now,
                    'meta_json' => $meta_json,
                ], [ '%d','%d','%d','%s','%f','%s','%s','%s','%s' ] );
                if ( $ok ) {
                    $wpdb->query( $wpdb->prepare( "UPDATE {$wallet_table} SET pending_balance=pending_balance+%f, updated_at=%s WHERE affiliate_id=%d", $commission, $now, $affiliate_id ) );
                }
            }

            if ( $ok ) {
                $wpdb->query( $wpdb->prepare( "UPDATE " . sz_aff_table('sz_affiliates') . " SET last_sale_at=%s,total_sales=total_sales+%f WHERE id=%d", $now, $gross, $affiliate_id ) );
            }
        }
    }

    // Senderzz recebe o mesmo total de taxas; apenas a responsabilidade da taxa de transação muda.
    if ( $fees > 0 ) sz_aff_credit_admin_wallet( $order_id, $producer_id, $affiliate_id, $fees, [
        'gross' => $gross,
        'net' => $net,
        'transaction_fee_mode' => (string) $tx_parts['mode'],
        'transaction_fee_affiliate' => (float) $tx_parts['affiliate'],
        'transaction_fee_producer' => (float) $tx_parts['producer'],
        'transaction_fee_total' => (float) $tx_parts['tx_total'],
    ] );

    // Produtor paga entrega/manuseio e a parte dele da transação. Afiliado recebe comissão líquida.
    $producer_fee_responsibility = max( 0, round( $fees - (float) $tx_parts['affiliate'], 2 ) );
    $producer_amount = max( 0, round( $gross - $commission_gross - $producer_fee_responsibility, 2 ) );
    if ( $producer_id && $producer_amount > 0 && function_exists( 'tpc_creditar' ) ) {
        tpc_creditar( $producer_id, $producer_amount, 'Venda COD Senderzz #' . $order_id . ' (pendente de retenção)', [
            'referencia' => 'sz_cod_produtor_' . $order_id,
            'order_id' => $order_id,
            'status_financeiro' => 'pending',
            'available_at' => $available_at,
            'affiliate_id' => $affiliate_id,
            'transaction_fee_mode' => (string) $tx_parts['mode'],
            'transaction_fee_producer' => (float) $tx_parts['producer'],
        ] );
    }

    $order->update_meta_data( '_sz_aff_split_done', '1' );
    $order->update_meta_data( '_sz_aff_gross', $gross );
    $order->update_meta_data( '_sz_aff_fees', $fees );
    $order->update_meta_data( '_sz_aff_net', $net );
    if ( $affiliate_id && $pct_for_tx > 0 ) {
        $order->update_meta_data( '_sz_aff_commission_pct', $pct_for_tx );
    }
    $order->update_meta_data( '_sz_aff_commission_gross', $commission_gross );
    $order->update_meta_data( '_sz_aff_transaction_fee', (float) $tx_parts['affiliate'] );
    $order->update_meta_data( '_sz_prod_transaction_fee', (float) $tx_parts['producer'] );
    $order->update_meta_data( '_sz_aff_commission', $commission );
    $order->update_meta_data( '_sz_prod_commission', $producer_amount );
    $order->update_meta_data( '_sz_commission_calc_mode', ( (string) $tx_parts['mode'] === 'split_producer_affiliate' ) ? 'gross_affiliate_split_transaction_fee' : 'gross_affiliate_fees_producer' );
    $order->update_meta_data( '_sz_aff_available_at', $available_at );
    $order->save();
} // end sz_aff_split_order_execute
// Split dispara apenas no status wc-completo (motoboy entregue).
// wc-entregue é expedição Melhor Envio e não deve disparar split COD.
// completed (WooCommerce padrão) não é usado na Senderzz.
add_action( 'woocommerce_order_status_completo',    'sz_aff_split_order_on_completed', 20, 1 ); // Motoboy COD
add_action( 'woocommerce_order_status_wc-completo', 'sz_aff_split_order_on_completed', 20, 1 ); // Motoboy COD alt

function sz_aff_release_available_commissions(): void {
    global $wpdb;
    $rows = $wpdb->get_results( $wpdb->prepare( "SELECT id,affiliate_id,amount FROM " . sz_aff_table('sz_affiliate_transactions') . " WHERE status='pending' AND available_at IS NOT NULL AND available_at <= %s LIMIT 500", sz_aff_now() ), ARRAY_A );
    foreach ( $rows as $r ) {
        $wpdb->query( 'START TRANSACTION' );
        try {
            $tx = $wpdb->get_row( $wpdb->prepare( "SELECT * FROM " . sz_aff_table('sz_affiliate_transactions') . " WHERE id=%d AND status='pending' FOR UPDATE", (int)$r['id'] ), ARRAY_A );
            if ( ! $tx ) { $wpdb->query( 'ROLLBACK' ); continue; }
            sz_aff_ensure_wallet( (int) $tx['affiliate_id'] );
            $wpdb->query( $wpdb->prepare( "UPDATE " . sz_aff_table('sz_affiliate_transactions') . " SET status='available' WHERE id=%d", (int)$tx['id'] ) );
            $wpdb->query( $wpdb->prepare( "UPDATE " . sz_aff_table('sz_affiliate_wallet') . " SET pending_balance=GREATEST(0,pending_balance-%f), balance=balance+%f, updated_at=%s WHERE affiliate_id=%d", (float)$tx['amount'], (float)$tx['amount'], sz_aff_now(), (int)$tx['affiliate_id'] ) );
            $wpdb->query( 'COMMIT' );
        } catch ( \Throwable $e ) {
            $wpdb->query( 'ROLLBACK' );
            error_log( '[Senderzz][sz_aff_release_commissions] Transação revertida para tx #' . ( (int)$r['id'] ) . ': ' . $e->getMessage() );
            // Continua para a próxima transação
        }
    }
}
add_action( 'sz_aff_release_commissions', 'sz_aff_release_available_commissions' );
if ( ! wp_next_scheduled( 'sz_aff_release_commissions' ) ) wp_schedule_event( time() + HOUR_IN_SECONDS, 'hourly', 'sz_aff_release_commissions' );

function sz_aff_reverse_order( int $order_id ): void {
    global $wpdb;
    $order = function_exists('wc_get_order') ? wc_get_order( $order_id ) : null;
    if ( ! $order instanceof WC_Order ) return;
    if ( $order->get_meta( '_sz_aff_reversed', true ) === '1' ) return;

    // Lock atômico para evitar double-reverse em ambientes com múltiplos workers
    $lock_key = 'sz_aff_reverse_lock_' . $order_id;
    $locked = $wpdb->query( $wpdb->prepare(
        "INSERT IGNORE INTO {$wpdb->options} (option_name, option_value, autoload) VALUES (%s, %s, 'no')",
        $lock_key, sz_aff_now()
    ) );
    if ( ! $locked ) return;

    try {
        $order->read_meta_data( true );
        if ( $order->get_meta( '_sz_aff_reversed', true ) === '1' ) return;

        $affiliate_id = (int) $order->get_meta( '_sz_affiliate_id', true );
        if ( $affiliate_id ) {
            // Transação atômica com ROLLBACK explícito em qualquer falha
            $wpdb->query( 'START TRANSACTION' );
            try {
                $txs = $wpdb->get_results( $wpdb->prepare(
                    "SELECT * FROM " . sz_aff_table('sz_affiliate_transactions') . " WHERE affiliate_id=%d AND order_id=%d AND type='commission' AND status IN ('pending','available') FOR UPDATE",
                    $affiliate_id, $order_id
                ), ARRAY_A );
                foreach ( $txs as $tx ) {
                    sz_aff_ensure_wallet( $affiliate_id );
                    if ( $tx['status'] === 'pending' ) {
                        $wpdb->query( $wpdb->prepare( "UPDATE " . sz_aff_table('sz_affiliate_wallet') . " SET pending_balance=GREATEST(0,pending_balance-%f), updated_at=%s WHERE affiliate_id=%d", (float)$tx['amount'], sz_aff_now(), $affiliate_id ) );
                    }
                    if ( $tx['status'] === 'available' ) {
                        $wpdb->query( $wpdb->prepare( "UPDATE " . sz_aff_table('sz_affiliate_wallet') . " SET balance=GREATEST(0,balance-%f), updated_at=%s WHERE affiliate_id=%d", (float)$tx['amount'], sz_aff_now(), $affiliate_id ) );
                    }
                    $wpdb->update( sz_aff_table('sz_affiliate_transactions'), [ 'status' => 'cancelled' ], [ 'id' => (int)$tx['id'] ], [ '%s' ], [ '%d' ] );
                }
                $wpdb->query( 'COMMIT' );
            } catch ( \Throwable $e ) {
                $wpdb->query( 'ROLLBACK' );
                // Remove o lock para permitir nova tentativa de estorno
                $wpdb->delete( $wpdb->options, [ 'option_name' => $lock_key ], [ '%s' ] );
                error_log( '[Senderzz][sz_aff_reverse_order] Transação revertida para pedido #' . $order_id . ': ' . $e->getMessage() );
                return;
            }
        }
        $order->update_meta_data( '_sz_aff_reversed', '1' );
        $order->save();
    } finally {
        $wpdb->delete( $wpdb->options, [ 'option_name' => $lock_key ], [ '%s' ] );
    }
}
add_action( 'woocommerce_order_status_refunded',   'sz_aff_reverse_order', 20, 1 );
add_action( 'woocommerce_order_status_cancelled',  'sz_aff_reverse_order', 20, 1 );
add_action( 'woocommerce_order_status_failed',     'sz_aff_reverse_order', 20, 1 );
// Status customizados motoboy Senderzz — também devem estornar comissão do afiliado
add_action( 'woocommerce_order_status_frustracao',    'sz_aff_reverse_order', 20, 1 );
add_action( 'woocommerce_order_status_wc-frustracao', 'sz_aff_reverse_order', 20, 1 );
add_action( 'woocommerce_order_status_frustrado',     'sz_aff_reverse_order', 20, 1 );
add_action( 'woocommerce_order_status_wc-frustrado',  'sz_aff_reverse_order', 20, 1 );
add_action( 'woocommerce_order_status_cancelado',     'sz_aff_reverse_order', 20, 1 );
add_action( 'woocommerce_order_status_wc-cancelado',  'sz_aff_reverse_order', 20, 1 );

// v329: saneamento financeiro idempotente para pedidos antigos já frustrados/cancelados.
// Se uma transição antiga não disparou o hook de estorno, corrige as transações ativas
// ao abrir o admin após a atualização. Não mexe em transações já canceladas nem em pedidos sem afiliado.
add_action( 'admin_init', function() {
    if ( ! current_user_can( 'manage_options' ) && ! current_user_can( 'manage_woocommerce' ) ) { return; }
    if ( get_option( 'sz_aff_reverse_frustrated_v329' ) === 'done' ) { return; }
    if ( ! function_exists( 'sz_aff_reverse_order' ) ) { return; }

    global $wpdb;
    $order_ids = [];
    $wc_orders = $wpdb->prefix . 'wc_orders';
    if ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $wc_orders ) ) === $wc_orders ) {
        $order_ids = $wpdb->get_col(
            "SELECT DISTINCT o.id
               FROM {$wc_orders} o
               JOIN " . sz_aff_table('sz_affiliate_transactions') . " t ON t.order_id = o.id AND t.type='commission' AND t.status IN ('pending','available')
              WHERE o.status IN ('wc-frustrado','frustrado','wc-frustracao','frustracao','wc-cancelled','cancelled','wc-cancelado','cancelado','wc-failed','failed')
              LIMIT 500"
        ) ?: [];
    } else {
        $order_ids = $wpdb->get_col(
            "SELECT DISTINCT p.ID
               FROM {$wpdb->posts} p
               JOIN " . sz_aff_table('sz_affiliate_transactions') . " t ON t.order_id = p.ID AND t.type='commission' AND t.status IN ('pending','available')
              WHERE p.post_status IN ('wc-frustrado','frustrado','wc-frustracao','frustracao','wc-cancelled','cancelled','wc-cancelado','cancelado','wc-failed','failed')
              LIMIT 500"
        ) ?: [];
    }
    foreach ( array_map( 'absint', $order_ids ) as $oid ) {
        if ( $oid > 0 ) { sz_aff_reverse_order( $oid ); }
    }
    update_option( 'sz_aff_reverse_frustrated_v329', 'done', false );
}, 31 );

function sz_aff_apply_frustration_penalty( int $order_id ): void {
    global $wpdb;
    $order = function_exists('wc_get_order') ? wc_get_order( $order_id ) : null;
    if ( ! $order instanceof WC_Order ) return;
    if ( $order->get_meta( '_sz_aff_frustration_done', true ) === '1' ) return;
    $phone = preg_replace( '/\D+/', '', (string) $order->get_billing_phone() );
    if ( strlen( $phone ) < 8 ) return;
    $count = (int) get_option( 'sz_aff_frustration_' . md5( $phone ), 0 ) + 1;
    update_option( 'sz_aff_frustration_' . md5( $phone ), $count, false );
    $affiliate_id = (int) $order->get_meta( '_sz_affiliate_id', true );
    $producer_id  = (int) $order->get_meta( '_sz_aff_producer_id', true );
    $aff = $affiliate_id ? sz_aff_get_affiliate_row( $affiliate_id ) : null;
    $penalty_aff      = function_exists( 'sz_aff_resolve_frustration_penalty' ) ? sz_aff_resolve_frustration_penalty( 'affiliate', $affiliate_id, $count ) : ( $count <= 1 ? sz_aff_first_frustration_penalty() : sz_aff_default_penalty_value() );
    $penalty_producer = function_exists( 'sz_aff_resolve_frustration_penalty' ) ? sz_aff_resolve_frustration_penalty( 'producer', $producer_id, $count ) : ( $count <= 1 ? 0.0 : sz_aff_producer_frustration_penalty() );
    $order->update_meta_data( '_sz_aff_frustration_penalty', $penalty_aff );
    $order->update_meta_data( '_sz_prod_frustration_penalty', $penalty_producer );
    $penalty          = $penalty_aff; // compatibilidade com código legado que usa $penalty abaixo
    if ( $penalty <= 0 && $penalty_producer <= 0 ) {
        $order->update_meta_data( '_sz_aff_frustration_done', '1' );
        $order->save();
        return;
    }
    if ( $affiliate_id && $penalty > 0 ) {
        sz_aff_ensure_wallet( $affiliate_id );
        $wallet = $wpdb->get_row( $wpdb->prepare( "SELECT balance,debt_amount FROM " . sz_aff_table('sz_affiliate_wallet') . " WHERE affiliate_id=%d", $affiliate_id ), ARRAY_A );
        $balance = (float) ( $wallet['balance'] ?? 0 );
        $debt = (float) ( $wallet['debt_amount'] ?? 0 );
        $debit = min( $balance, $penalty );
        $new_debt = min( 100, $debt + max( 0, $penalty - $debit ) );
        $wpdb->query( $wpdb->prepare( "UPDATE " . sz_aff_table('sz_affiliate_wallet') . " SET balance=GREATEST(0,balance-%f), debt_amount=%f, updated_at=%s WHERE affiliate_id=%d", $debit, $new_debt, sz_aff_now(), $affiliate_id ) );
        $wpdb->insert( sz_aff_table('sz_affiliate_transactions'), [ 'affiliate_id'=>$affiliate_id,'producer_id'=>$producer_id,'order_id'=>$order_id,'type'=>'penalty','amount'=>$penalty,'status'=>'applied','created_at'=>sz_aff_now(),'meta_json'=>wp_json_encode(['phone_hash'=>md5($phone),'count'=>$count]) ], [ '%d','%d','%d','%s','%f','%s','%s','%s' ] );
    }
    if ( $producer_id && $penalty_producer > 0 && function_exists( 'tpc_debitar' ) ) {
        tpc_debitar( $producer_id, $penalty_producer, 'Penalidade por frustrado reincidente #' . $order_id, [ 'referencia' => 'sz_frustrado_' . $order_id, 'order_id' => $order_id ] );
    }
    $order->update_meta_data( '_sz_aff_frustration_done', '1' );
    $order->save();
}
// Penalidade de frustração: só em status de tentativa frustrada de entrega.
// 'cancelled' e 'failed' são cancelamentos normais — não cobram penalidade do afiliado.
foreach ( [ 'frustrado', 'wc-frustrado', 'frustracao', 'wc-frustracao' ] as $st ) {
    add_action( 'woocommerce_order_status_' . $st, 'sz_aff_apply_frustration_penalty', 30, 1 );
}

function sz_aff_handle_invite_route(): void {
    add_rewrite_rule( '^convite/([^/]+)/?$', 'index.php?sz_aff_invite=$matches[1]', 'top' );
    add_rewrite_tag( '%sz_aff_invite%', '([^&]+)' );
}
add_action( 'init', 'sz_aff_handle_invite_route' );

function sz_aff_render_invite_page(): void {
    $token = get_query_var( 'sz_aff_invite' );
    if ( ! $token ) {
        $path = parse_url( (string) ( $_SERVER['REQUEST_URI'] ?? '' ), PHP_URL_PATH );
        if ( preg_match( '#^/convite/([^/]+)/?$#', (string) $path, $m ) ) {
            $token = rawurldecode( $m[1] );
        }
    }
    if ( ! $token ) return;

    global $wpdb;
    $invite = $wpdb->get_row( $wpdb->prepare( "SELECT * FROM " . sz_aff_table('sz_invite_links') . " WHERE token=%s AND status='active' LIMIT 1", sanitize_text_field( $token ) ), ARRAY_A );
    status_header( 200 );
    nocache_headers();

    $producer_name = ! empty( $invite['producer_id'] ) ? sz_aff_get_producer_display_name( (int) $invite['producer_id'] ) : 'Produtor';
    $logo_url = esc_url( sz_aff_plugin_logo_url() );

    $msg = '';
    $ok  = false;
    $form_values = [
        'nome'     => '',
        'cpf'      => '',
        'email'    => '',
        'telefone' => '',
    ];

    if ( ! $invite || strtotime( (string) $invite['expires_at'] ) < current_time( 'timestamp' ) ) {
        $msg = 'Convite inválido ou indisponível.';
    } elseif ( $_SERVER['REQUEST_METHOD'] === 'POST' && isset( $_POST['sz_aff_nonce'] ) && wp_verify_nonce( sanitize_text_field( wp_unslash( $_POST['sz_aff_nonce'] ) ), 'sz_aff_join' ) ) {
        $form_values['nome']     = sanitize_text_field( wp_unslash( $_POST['nome'] ?? '' ) );
        $form_values['cpf']      = sz_aff_format_cpf( (string) wp_unslash( $_POST['cpf'] ?? '' ) );
        $form_values['email']    = sanitize_email( wp_unslash( $_POST['email'] ?? '' ) );
        $form_values['telefone'] = sz_aff_format_phone( (string) wp_unslash( $_POST['telefone'] ?? '' ) );

        $email = $form_values['email'];
        $name  = $form_values['nome'];
        $cpf   = sz_aff_digits( $form_values['cpf'] );
        $phone = sz_aff_digits( $form_values['telefone'] );

        if ( $name === '' ) {
            $msg = 'Informe seu nome completo.';
        } elseif ( ! is_email( $email ) ) {
            $msg = 'Informe um e-mail válido.';
        } elseif ( ! sz_aff_validate_cpf( $cpf ) ) {
            $msg = 'Informe um CPF válido.';
        } elseif ( ! sz_aff_validate_phone( $phone ) ) {
            $msg = 'Informe um telefone válido com DDD.';
        } elseif ( empty( $_POST['lgpd'] ) ) {
            $msg = 'Você precisa aceitar o tratamento dos dados para continuar.';
        } elseif ( sz_senderzz_account_exists_by_email_or_cpf( $email, $cpf ) ) {
            $msg = 'Este e-mail ou CPF já possui cadastro na Senderzz. Use recuperar senha para acessar sua conta.';
        } else {
            $user = get_user_by( 'email', $email );
            if ( ! $user ) {
                $pass = wp_generate_password( 18, true, true );
                $uid  = wp_create_user( $email, $pass, $email );
                if ( is_wp_error( $uid ) ) {
                    $msg = $uid->get_error_message();
                } else {
                    wp_update_user( [
                        'ID'           => $uid,
                        'display_name' => $name,
                        'first_name'   => $name,
                        'role'         => 'customer',
                    ] );
                    $user = get_user_by( 'id', $uid );
                }
            } else {
                $uid = (int) $user->ID;
                if ( ! in_array( 'customer', (array) $user->roles, true ) ) { $user->add_role( 'customer' ); }
                wp_update_user( [ 'ID' => $uid, 'display_name' => $name, 'first_name' => $name ] );
            }

            if ( empty( $msg ) && ! empty( $uid ) ) {
                update_user_meta( $uid, 'sz_document', $cpf );
                update_user_meta( $uid, 'sz_phone', $phone );

                $status = sz_aff_is_producer_auto_approve( (int) $invite['producer_id'] ) ? 'active' : 'pending';
                $existing = (int) $wpdb->get_var( $wpdb->prepare( "SELECT id FROM " . sz_aff_table('sz_affiliates') . " WHERE producer_id=%d AND user_id=%d", (int) $invite['producer_id'], $uid ) );
                $data = [
                    'producer_id'         => (int) $invite['producer_id'],
                    'user_id'             => $uid,
                    'status'              => $status,
                    'commission_pct'      => sz_aff_producer_default_commission_pct( (int) $invite['producer_id'] ),
                    'withdraw_fee'        => sz_aff_default_withdraw_fee(),
                    'penalty_value'       => sz_aff_default_penalty_value(),
                    'retention_days'      => sz_aff_default_retention_days(),
                    'offer_link_id'       => (int) ( $invite['offer_link_id'] ?? 0 ),
                    'offer_token'         => (string) ( $invite['offer_token'] ?? '' ),
                    // v258: convite padrão grava o nome do produtor no offer_name para rastreabilidade futura
                    'offer_name'          => ( (int) ( $invite['offer_link_id'] ?? 0 ) === 0 )
                                                ? ( $producer_name ?: (string) ( $invite['offer_name'] ?? 'Convite padrão' ) )
                                                : (string) ( $invite['offer_name'] ?? '' ),
                    'offer_url'           => (string) ( $invite['offer_url'] ?? '' ),
                    'allow_checkout_link' => 1,
                    'pix_key'             => sz_aff_user_profile_value( $uid, 'sz_pix_key' ),
                    'bank_data'           => sz_aff_user_profile_value( $uid, 'sz_bank_data' ),
                    'document'            => $cpf,
                    'phone'               => $phone,
                    'lgpd_accepted'       => 1,
                    'bank_status'         => 'pending',
                    'created_at'          => sz_aff_now(),
                    'approved_at'         => $status === 'active' ? sz_aff_now() : null,
                    // _sz_tokens_populated preenchido abaixo após insert
                    'invited_by'          => (int) $invite['created_by'],
                ];
                if ( $existing ) {
                    $wpdb->update( sz_aff_table('sz_affiliates'), $data, [ 'id' => $existing ] );
                } else {
                    $wpdb->insert( sz_aff_table('sz_affiliates'), $data );
                }
                $affiliate_id = $existing ?: (int) $wpdb->insert_id;
                sz_aff_ensure_wallet( $affiliate_id );
                sz_aff_ensure_portal_affiliate_user( (int) $uid );
                // Popula vínculos token→afiliado imediatamente no cadastro
                if ( $status === 'active' && function_exists( 'sz_aff_register_all_tokens_for_affiliate' ) ) {
                    sz_aff_register_all_tokens_for_affiliate( $affiliate_id, (int) $invite['producer_id'] );
                }
                $wpdb->query( $wpdb->prepare( "UPDATE " . sz_aff_table('sz_invite_links') . " SET uses=uses+1 WHERE id=%d", (int) $invite['id'] ) );
                sz_aff_send_affiliate_access_email( $uid, $producer_name, $status );
                $ok  = true;
                $msg = $status === 'active'
                    ? 'Cadastro aprovado. Seu acesso foi enviado por e-mail. Defina sua senha para entrar no painel.'
                    : 'Cadastro recebido. Enviamos o acesso por e-mail para definir sua senha e acompanhar a aprovação.';
            }
        }
    }
    ?>
    <!doctype html>
    <html <?php language_attributes(); ?>>
    <head>
        <meta charset="<?php bloginfo('charset'); ?>">
        <meta name="viewport" content="width=device-width,initial-scale=1">
        <title>Convite Afiliado Senderzz</title>
        <?php wp_head(); ?>
        <style>
            body{margin:0;background:#f4f5f7;font-family:var(--sz-font);color:#0f172a}
            .sz-aff-wrap{max-width:760px;margin:22px auto;padding:0 16px}
            .sz-aff-card{background:#fff;border:1px solid #e5e7eb;border-radius:24px;box-shadow:0 18px 50px rgba(15,23,42,.10);overflow:hidden}
            .sz-aff-top{background:linear-gradient(135deg,#0f172a 0%,#1b2436 56%,#8f350f 100%);padding:22px 26px;color:#fff}
            .sz-aff-logo{display:block;height:34px;max-width:150px;width:auto;object-fit:contain;margin-bottom:14px}
            .sz-aff-kicker{font-size:var(--sz-text-sm);font-weight:700;letter-spacing:0;text-transform:none;color:#fdba74;margin-bottom:7px}
            .sz-aff-top h1{margin:0;font-size:var(--sz-text-3xl);line-height:1.08;letter-spacing:-.015em}
            .sz-aff-top p{margin:9px 0 0;color:rgba(255,255,255,.82);font-size:var(--sz-text-md);line-height:1.45;max-width:620px}
            .sz-aff-body{padding:18px 20px 20px}
            .sz-aff-msg{padding:9px 12px;border-radius:12px;background:#fff7ed;border:1px solid #fed7aa;color:#9a3412;margin:0 0 10px;line-height:1.35;font-size:var(--sz-text-base);font-weight:700}
            .sz-aff-ok{background:#fff7ed;border-color:#fed7aa;color:#9a3412}
            .sz-aff-grid{display:grid;grid-template-columns:1fr 1fr;gap:10px}
            .sz-aff-field{display:flex;flex-direction:column;gap:6px}
            .sz-aff-field.full{grid-column:1/-1}
            .sz-aff-field label{font-weight:700;font-size:var(--sz-text-meta);color:#0f172a}
            .sz-aff-field input{border:1.5px solid #d1d5db;border-radius:12px;padding:10px 12px;font-size:var(--sz-text-md);min-height:40px;outline:none;box-sizing:border-box}
            .sz-aff-field input:focus{border-color:#E8650A;box-shadow:0 0 0 4px rgba(232,101,10,.10)}
            .sz-aff-consent{margin-top:2px}
            .sz-aff-check{display:flex;align-items:center;justify-content:space-between;gap:14px;border:1px solid #e2e8f0;background:#fbfcfd;border-radius:13px;padding:10px 12px;color:#334155;font-weight:600;line-height:1.36;font-size:var(--sz-text-base)}
            .sz-aff-check span{flex:1;min-width:0}
            .sz-aff-check .sz-aff-toggle{position:relative;display:inline-flex;flex:0 0 42px!important;width:42px!important;height:24px!important;min-width:42px!important;max-width:42px!important}
            .sz-aff-toggle input{position:absolute;opacity:0;inset:0;margin:0;cursor:pointer}
            .sz-aff-slider{position:absolute;inset:0;border-radius:999px;background:#e2e8f0;transition:.2s ease}
            .sz-aff-slider:before{content:'';position:absolute;top:3px;left:3px;width:18px;height:18px;border-radius:50%;background:#fff;box-shadow:0 1px 3px rgba(15,23,42,.25);transition:.2s ease}
            .sz-aff-toggle input:checked + .sz-aff-slider{background:#E8650A}
            .sz-aff-toggle input:checked + .sz-aff-slider:before{transform:translateX(18px)}
            .sz-aff-toggle input:focus + .sz-aff-slider{box-shadow:0 0 0 4px rgba(232,101,10,.12)}
            .sz-aff-mini-note{margin-top:6px;color:#64748b;font-size:var(--sz-text-meta);line-height:1.35;text-align:left;padding-left:2px}
            .sz-aff-btn{border:0;border-radius:14px;background:#E8650A;color:#fff;font-weight:700;padding:13px 16px;width:100%;font-size:var(--sz-text-lg);cursor:pointer;box-shadow:0 12px 24px rgba(232,101,10,.20)}
            .sz-aff-foot{margin-top:8px;color:#64748b;font-size:var(--sz-text-meta);text-align:center;line-height:1.4}
            @media(max-width:700px){.sz-aff-wrap{margin:12px auto}.sz-aff-top,.sz-aff-body{padding:18px}.sz-aff-top h1{font-size:var(--sz-text-3xl)}.sz-aff-grid{grid-template-columns:1fr}.sz-aff-check{align-items:flex-start}.sz-aff-toggle{margin-top:1px}}

            /* Senderzz v42 — ícones, botões e listagem afiliados padronizados */
            html body .sz-root .sz-aff-icon,
            html body .sz-root.sz-dark .sz-aff-icon{background:#E8650A!important;color:#fff!important;border:0!important;box-shadow:none!important}
            html body .sz-root .sz-aff-btn,
            html body .sz-root .sz-aff-btn.dark,
            html body .sz-root .sz-aff-btn.reject,
            html body .sz-root.sz-dark .sz-aff-btn,
            html body .sz-root.sz-dark .sz-aff-btn.dark,
            html body .sz-root.sz-dark .sz-aff-btn.reject{background:#E8650A!important;color:#fff!important;border:0!important;box-shadow:none!important}
            html body .sz-root .sz-aff-btn:disabled,
            html body .sz-root.sz-dark .sz-aff-btn:disabled{background:#cbd5e1!important;color:#fff!important;opacity:1!important;cursor:not-allowed!important}
            html body .sz-root.sz-dark .sz-aff-filterbar{background:#0f172a!important;border-color:rgba(255,255,255,.13)!important}
            html body .sz-root:not(.sz-dark) .sz-aff-filterbar{background:#fff!important;border-color:#edf1f5!important}
            html body .sz-root .sz-aff-row,
            html body .sz-root .sz-aff-row.head{grid-template-columns:34px 64px minmax(180px,1.1fr) minmax(240px,1.25fr) minmax(150px,.8fr) minmax(170px,.75fr) minmax(132px,.62fr)!important;gap:18px!important;align-items:center!important}
            html body .sz-root .sz-aff-row.head{min-height:32px!important;padding:0 18px 6px!important;border:0!important;border-radius:0!important;line-height:1.2;background:transparent!important;overflow:visible!important;white-space:nowrap!important}
            html body .sz-root .sz-aff-row.head>div{min-width:0!important;overflow:visible!important;text-overflow:clip!important;white-space:nowrap!important;line-height:1.2}
            html body .sz-root .sz-aff-status-actions{min-width:150px!important;justify-content:flex-start!important;white-space:nowrap!important}
            html body .sz-root.sz-dark .sz-aff-row:not(.head){background:#0f172a!important;border-color:rgba(255,255,255,.13)!important;box-shadow:none!important}
            html body .sz-root:not(.sz-dark) .sz-aff-row:not(.head){background:#fff!important;border-color:#e8edf3!important;box-shadow:none!important}
            html body .sz-root.sz-dark .sz-aff-tab,
            html body .sz-root.sz-dark .sz-aff-tab.active{background:#E8650A!important;border-color:#E8650A!important;color:#fff!important}
            html body .sz-root .sz-aff-link{background:transparent!important;border-color:#E8650A!important;color:var(--sz-text,#111827)!important}
            html body .sz-root.sz-dark .sz-aff-link{background:#0b1220!important;border-color:#E8650A!important;color:#f8fafc!important}
            @media(max-width:1180px){html body .sz-root .sz-aff-row,html body .sz-root .sz-aff-row.head{grid-template-columns:1fr!important}.sz-aff-row.head{display:none!important}.sz-aff-status-actions{min-width:0!important;white-space:normal!important}}

        
            /* Senderzz v143 — matriz de comissão organizada e dark mode consistente */
            .sz-aff-row,.sz-aff-row.head{grid-template-columns:42px 82px minmax(160px,.85fr) minmax(230px,1.05fr) minmax(140px,.72fr) minmax(120px,.55fr) minmax(300px,1.25fr)!important}
            .sz-aff-row.head.sz-aff-head-active>div:nth-child(6){visibility:hidden!important}
            .sz-aff-commission-cell{display:grid!important;grid-template-columns:minmax(132px,150px) minmax(132px,150px)!important;gap:10px!important;align-items:center!important;justify-content:end!important;justify-items:stretch!important;min-width:0!important}
            .sz-aff-commission-cell>.sz-aff-form-inline{display:block!important;min-width:0!important}
            .sz-aff-commission-cell .sz-aff-input-wrap{max-width:none!important;margin:0!important}
            .sz-aff-link-commissions{position:relative!important;width:auto!important;max-width:none!important;min-width:0!important}
            .sz-aff-link-commissions>summary{height:48px!important;display:flex!important;align-items:center!important;justify-content:center!important;border:1px solid #fed7aa!important;background:#fff7ed!important;color:#E8650A!important;border-radius:14px!important;padding:0 12px!important;font-size:var(--sz-text-meta);font-weight:700;list-style:none!important;white-space:nowrap!important;cursor:pointer!important}
            .sz-aff-link-commissions[open]>summary{background:#E8650A!important;color:#fff!important}
            .sz-aff-link-commission-list{position:absolute!important;right:0!important;top:54px!important;width:min(620px,calc(100vw - 72px))!important;background:#fff!important;border:1px solid #fed7aa!important;border-radius:18px!important;padding:14px!important;box-shadow:0 20px 45px rgba(15,23,42,.18)!important;z-index:999!important;display:grid!important;gap:10px!important}
            .sz-aff-matrix-title{font-weight:700;color:#0f172a!important;font-size:var(--sz-text-md)}
            .sz-aff-matrix-form{display:grid!important;grid-template-columns:minmax(220px,1fr) 118px 82px!important;gap:9px!important;align-items:end!important}
            .sz-aff-matrix-form label{display:grid!important;gap:6px!important;margin:0!important}
            .sz-aff-matrix-form label span{font-size:var(--sz-text-xs);text-transform:none;letter-spacing:0;color:#64748b!important;font-weight:700}
            .sz-aff-matrix-form select,.sz-aff-matrix-form input{height:40px!important;border:1px solid #e5eaf1!important;border-radius:12px!important;background:#fff!important;color:#0f172a!important;font-weight:700;padding:0 10px!important;min-width:0!important;box-shadow:none!important}
            .sz-aff-matrix-form input{text-align:center!important}
            .sz-aff-matrix-form button{height:40px!important;border:0!important;border-radius:12px!important;background:#E8650A!important;color:#fff!important;font-weight:700;cursor:pointer!important}
            .sz-aff-scope-form{border-top:1px dashed #e5eaf1!important;padding-top:10px!important}
            .sz-root.sz-dark .sz-aff-card{background:#111827!important;border-color:rgba(255,255,255,.10)!important;box-shadow:none!important}
            .sz-root.sz-dark .sz-aff-row:not(.head){background:#0f172a!important;border-color:rgba(255,255,255,.10)!important;box-shadow:none!important}
            .sz-root.sz-dark .sz-aff-title,.sz-root.sz-dark .sz-aff-name strong,.sz-root.sz-dark .sz-aff-matrix-title{color:#f8fafc!important}
            .sz-root.sz-dark .sz-aff-sub,.sz-root.sz-dark .sz-aff-cell-text,.sz-root.sz-dark .sz-aff-link-commission-hint{color:#94a3b8!important}
            .sz-root.sz-dark .sz-aff-tabs{background:#0f172a!important;border-color:rgba(255,255,255,.12)!important}
            .sz-root.sz-dark .sz-aff-tab{background:#0f172a!important;color:#cbd5e1!important}
            .sz-root.sz-dark .sz-aff-tab.active{background:#E8650A!important;color:#fff!important}
            .sz-root.sz-dark .sz-aff-input,.sz-root.sz-dark .sz-aff-row:not(.head) .sz-aff-input,.sz-root.sz-dark .sz-aff-matrix-form select,.sz-root.sz-dark .sz-aff-matrix-form input{background:#020617!important;color:#f8fafc!important;border-color:rgba(255,255,255,.14)!important}
            .sz-root.sz-dark .sz-aff-suffix{border-left-color:rgba(255,255,255,.14)!important;color:#cbd5e1!important}
            .sz-root.sz-dark .sz-aff-link-commission-list{background:#111827!important;border-color:rgba(249,115,22,.45)!important;box-shadow:0 20px 45px rgba(0,0,0,.45)!important}
            .sz-root.sz-dark .sz-aff-matrix-form label span{color:#94a3b8!important}
            .sz-root.sz-dark .sz-aff-link-commissions>summary{background:rgba(249,115,22,.10)!important;border-color:rgba(249,115,22,.45)!important;color:#fb923c!important}
            .sz-root.sz-dark .sz-aff-link-commissions[open]>summary{background:#E8650A!important;color:#fff!important}
            @media(max-width:1180px){.sz-aff-row,.sz-aff-row.head{grid-template-columns:1fr!important}.sz-aff-row.head{display:none!important}.sz-aff-commission-cell{grid-template-columns:1fr!important;justify-content:stretch!important}.sz-aff-link-commission-list{position:static!important;width:100%!important;margin-top:8px!important}.sz-aff-matrix-form{grid-template-columns:1fr!important}}


/* Senderzz v147 — afiliados dark sólido e sem laranja aguado */
.sz-root.sz-dark .sz-aff-card{background:#111827!important;border:1px solid rgba(255,255,255,.12)!important;box-shadow:none!important}
.sz-root.sz-dark .sz-aff-filterbar,.sz-root.sz-dark .sz-aff-row:not(.head){background:#0f172a!important;border-color:rgba(255,255,255,.14)!important;box-shadow:none!important}
.sz-root.sz-dark .sz-aff-btn,.sz-root.sz-dark .sz-aff-link-commissions>summary,.sz-root.sz-dark .sz-aff-tab.active,.sz-root.sz-dark .sz-aff-tabs .sz-aff-tab.active{background:#E8650A!important;color:#fff!important;border-color:#E8650A!important;box-shadow:none!important}
.sz-root.sz-dark .sz-aff-link-commissions:not([open])>summary{background:#E8650A!important;color:#fff!important;border-color:#E8650A!important}
.sz-root.sz-dark .sz-aff-input-wrap,.sz-root.sz-dark .sz-aff-input,.sz-root.sz-dark .sz-aff-search,.sz-root.sz-dark .sz-aff-date,.sz-root.sz-dark .sz-aff-filterbar select{background:#020617!important;color:#f8fafc!important;border-color:rgba(255,255,255,.16)!important}
.sz-root.sz-dark .sz-aff-suffix{background:#020617!important;color:#f8fafc!important;border-left-color:rgba(255,255,255,.16)!important}
.sz-root.sz-dark .sz-aff-link-commission-list{background:#111827!important;border-color:#E8650A!important;box-shadow:0 24px 55px rgba(0,0,0,.55)!important}
.sz-root.sz-dark .sz-aff-matrix-form button{background:#E8650A!important;color:#fff!important;border-color:#E8650A!important}

/* v200 removido: botões v2 agora usam a regra base .sz-aff-v2-btn, sem override html body/!important. */
/* v265 — cores finais afiliados, sem depender do tema do navegador */
#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-card,
#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-list-card,
#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-trow,
#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-hist-row { background: var(--sz-surface); border-color: var(--sz-border); color: var(--sz-text-secondary); box-shadow:none; }
#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-card-title,
#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-list-title,
#sec-affiliates .sz-aff-panel-v2 strong { color: var(--sz-text-primary); }
#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-card-sub,
#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-list-sub,
#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-row-cell,
#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-preview,
#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-toggle-lbl { color: var(--sz-text-secondary); }
#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-invite-url-bar,
#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-comm-wrap,
#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-ci,
#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-fbar,
#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-fbar input,
#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-fbar select,
#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-preview,
#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-info-box.neutral { background: var(--sz-surface-alt); border-color: var(--sz-border); color: var(--sz-text-secondary); }
#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-comm-input,
#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-ci input { color: var(--sz-text-primary); }
#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-comm-suf,
#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-ci-suf { background: var(--sz-bg); border-color: var(--sz-border); color: var(--sz-text-muted); }
#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-info-box.orange { background: var(--sz-brand-light); border-color: rgba(232,101,10,.28); color: var(--sz-text-secondary); }
#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-info-box.orange strong { color: var(--sz-brand); }
#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-btn,
#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-btn.approve,
#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-btn.excluir,
#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-btn.sz-aff-v2-invite-btn { background: var(--sz-brand); color:#fff; border-color:var(--sz-brand); }
#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-btn:hover,
#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-btn.approve:hover,
#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-btn.excluir:hover,
#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-btn.sz-aff-v2-invite-btn:hover { background: var(--sz-brand-hover); color:#fff; }
#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-stab.is-active { background:var(--sz-brand); color:#fff; border-bottom-color:var(--sz-brand); }
#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-toggle input:checked + .sz-aff-v2-toggle-slider { background:var(--sz-brand); }

</style>
    </head>
    <body>
        <div class="sz-aff-wrap">
            <div class="sz-aff-card">
                <div class="sz-aff-top">
                    <img class="sz-aff-logo" src="<?php echo $logo_url; ?>" alt="Senderzz" onerror="this.style.display='none'">
                    <div class="sz-aff-kicker">Programa de afiliados</div>
                    <h1><?php echo esc_html( $producer_name ); ?> te convidou para ser afiliado.</h1>
                    <p>Preencha seu cadastro para participar. Após aprovação, você receberá acesso aos links disponibilizados pelo produtor.</p>
                </div>
                <div class="sz-aff-body">
                    <?php if ( $msg ) : ?><div class="sz-aff-msg <?php echo $ok ? 'sz-aff-ok' : ''; ?>"><?php echo esc_html( $msg ); ?></div><?php endif; ?>
                    <?php if ( $invite && ! $ok ) : ?>
                        <form method="post" novalidate>
                            <div class="sz-aff-grid">
                                <?php wp_nonce_field( 'sz_aff_join', 'sz_aff_nonce' ); ?>
                                <div class="sz-aff-field">
                                    <label for="sz_aff_nome">Nome completo *</label>
                                    <input id="sz_aff_nome" name="nome" required maxlength="120" value="<?php echo esc_attr( $form_values['nome'] ); ?>" autocomplete="name">
                                </div>
                                <div class="sz-aff-field">
                                    <label for="sz_aff_cpf">CPF *</label>
                                    <input id="sz_aff_cpf" name="cpf" required inputmode="numeric" maxlength="14" placeholder="000.000.000-00" value="<?php echo esc_attr( $form_values['cpf'] ); ?>">
                                </div>
                                <div class="sz-aff-field">
                                    <label for="sz_aff_email">E-mail *</label>
                                    <input id="sz_aff_email" type="email" name="email" required placeholder="voce@email.com" value="<?php echo esc_attr( $form_values['email'] ); ?>" autocomplete="email">
                                </div>
                                <div class="sz-aff-field">
                                    <label for="sz_aff_tel">Telefone *</label>
                                    <input id="sz_aff_tel" name="telefone" required inputmode="tel" maxlength="15" placeholder="(11) 99999-9999" value="<?php echo esc_attr( $form_values['telefone'] ); ?>" autocomplete="tel">
                                </div>
                                <div class="sz-aff-field full sz-aff-consent">
                                    <label class="sz-aff-check" for="sz_aff_lgpd">
                                        <span>Autorizo o uso dos meus dados para cadastro, aprovação e pagamento de comissões.</span>
                                        <span class="sz-aff-toggle"><input id="sz_aff_lgpd" type="checkbox" name="lgpd" value="1" required <?php checked( ! empty( $_POST['lgpd'] ) ); ?>><span class="sz-aff-slider"></span></span>
                                    </label>
                                    <div class="sz-aff-mini-note">PIX e dados bancários serão preenchidos depois no perfil.</div>
                                </div>
                                <div class="sz-aff-field full"><button class="sz-aff-btn">Enviar cadastro</button></div>
                            </div>
                            <div class="sz-aff-foot">Você receberá por e-mail o acesso para definir sua senha e entrar na sua conta.</div>
                        </form>
                        <script>
                            (function(){
                                var cpf=document.getElementById('sz_aff_cpf'), tel=document.getElementById('sz_aff_tel');
                                function digits(v){return (v||'').replace(/\D+/g,'');}
                                function maskCpf(v){v=digits(v).slice(0,11);v=v.replace(/(\d{3})(\d)/,'$1.$2').replace(/(\d{3})(\d)/,'$1.$2').replace(/(\d{3})(\d{1,2})$/,'$1-$2');return v;}
                                function maskTel(v){v=digits(v);if(v.slice(0,2)==='55'&&v.length>11)v=v.slice(2);v=v.slice(0,11);if(v.length<=10)return v.replace(/(\d{0,2})(\d{0,4})(\d{0,4})/,function(_,a,b,c){return (a?'('+a:'')+(a&&a.length===2?') ':'')+b+(c?'-'+c:'');});return v.replace(/(\d{2})(\d{5})(\d{0,4})/,function(_,a,b,c){return '('+a+') '+b+(c?'-'+c:'');});}
                                if(cpf) cpf.addEventListener('input', function(){this.value=maskCpf(this.value)});
                                if(tel) tel.addEventListener('input', function(){this.value=maskTel(this.value)});
                            })();
                        </script>
                    <?php endif; ?>
                </div>
            </div>
        </div>
        <?php wp_footer(); ?>
    </body>
    </html>
    <?php
    exit;
}
add_action( 'template_redirect', 'sz_aff_render_invite_page', 0 );





function sz_aff_get_link_commission_pct( int $affiliate_id, int $link_id, ?float $fallback = null ): float {
    global $wpdb;
    $affiliate_id = absint( $affiliate_id ); $link_id = absint( $link_id );
    if ( ! $affiliate_id || ! $link_id ) return (float) ( $fallback ?? 0 );
    $t = sz_aff_table('sz_affiliate_link_commissions');
    $v = $wpdb->get_var( $wpdb->prepare( "SELECT commission_pct FROM {$t} WHERE affiliate_id=%d AND link_id=%d LIMIT 1", $affiliate_id, $link_id ) );
    if ( $v === null || $v === '' ) return (float) ( $fallback ?? 0 );
    return max( 0, min( 100, (float) $v ) );
}

function sz_aff_upsert_link_commission_pct( int $affiliate_id, int $link_id, float $pct ): bool {
    global $wpdb;
    $affiliate_id = absint( $affiliate_id ); $link_id = absint( $link_id ); $pct = max( 0, min( 100, $pct ) );
    if ( ! $affiliate_id || ! $link_id ) return false;
    $t = sz_aff_table('sz_affiliate_link_commissions');
    $now = sz_aff_now();
    $exists = (int) $wpdb->get_var( $wpdb->prepare( "SELECT id FROM {$t} WHERE affiliate_id=%d AND link_id=%d LIMIT 1", $affiliate_id, $link_id ) );
    if ( $exists ) return false !== $wpdb->update( $t, [ 'commission_pct'=>$pct, 'updated_at'=>$now ], [ 'id'=>$exists ], [ '%f','%s' ], [ '%d' ] );
    return false !== $wpdb->insert( $t, [ 'affiliate_id'=>$affiliate_id, 'link_id'=>$link_id, 'commission_pct'=>$pct, 'created_at'=>$now, 'updated_at'=>$now ], [ '%d','%d','%f','%s','%s' ] );
}


function sz_aff_ensure_product_class_commission_table(): void {
    global $wpdb;
    $table = sz_aff_table('sz_affiliate_product_class_commissions');
    $charset = $wpdb->get_charset_collate();
    require_once ABSPATH . 'wp-admin/includes/upgrade.php';
    dbDelta( "CREATE TABLE {$table} (
        id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        affiliate_id BIGINT UNSIGNED NOT NULL,
        scope VARCHAR(40) NOT NULL DEFAULT 'global',
        adjustment_pct DECIMAL(10,2) NOT NULL DEFAULT 0.00,
        created_at DATETIME NOT NULL,
        updated_at DATETIME NOT NULL,
        PRIMARY KEY (id),
        UNIQUE KEY uniq_aff_scope (affiliate_id,scope),
        KEY idx_scope (scope)
    ) {$charset};" );
}

function sz_aff_normalize_product_class_scope( string $scope ): string {
    $scope = sanitize_key( remove_accents( $scope ) );
    if ( preg_match( '/^class[_-]?(\d+)$/', $scope, $m ) && absint( $m[1] ) > 0 ) return 'class_' . absint( $m[1] );
    if ( preg_match( '/^shipping[_-]?class[_-]?(\d+)$/', $scope, $m ) && absint( $m[1] ) > 0 ) return 'class_' . absint( $m[1] );
    return 'global';
}

function sz_aff_get_product_class_adjustment_pct( int $affiliate_id, string $scope ): float {
    global $wpdb;
    $affiliate_id = absint( $affiliate_id );
    if ( ! $affiliate_id ) return 0.0;
    sz_aff_ensure_product_class_commission_table();
    $scope = sz_aff_normalize_product_class_scope( $scope );
    $table = sz_aff_table('sz_affiliate_product_class_commissions');
    $v = $wpdb->get_var( $wpdb->prepare( "SELECT adjustment_pct FROM {$table} WHERE affiliate_id=%d AND scope=%s LIMIT 1", $affiliate_id, $scope ) );
    return $v === null || $v === '' ? 0.0 : max( -100, min( 100, (float) $v ) );
}

function sz_aff_upsert_product_class_adjustment_pct( int $affiliate_id, string $scope, float $pct ): bool {
    global $wpdb;
    $affiliate_id = absint( $affiliate_id );
    if ( ! $affiliate_id ) return false;
    sz_aff_ensure_product_class_commission_table();
    $scope = sz_aff_normalize_product_class_scope( $scope );
    $pct = max( -100, min( 100, $pct ) );
    $table = sz_aff_table('sz_affiliate_product_class_commissions');
    $now = sz_aff_now();
    $exists = (int) $wpdb->get_var( $wpdb->prepare( "SELECT id FROM {$table} WHERE affiliate_id=%d AND scope=%s LIMIT 1", $affiliate_id, $scope ) );
    if ( $exists ) return false !== $wpdb->update( $table, [ 'adjustment_pct'=>$pct, 'updated_at'=>$now ], [ 'id'=>$exists ], [ '%f','%s' ], [ '%d' ] );
    return false !== $wpdb->insert( $table, [ 'affiliate_id'=>$affiliate_id, 'scope'=>$scope, 'adjustment_pct'=>$pct, 'created_at'=>$now, 'updated_at'=>$now ], [ '%d','%s','%f','%s','%s' ] );
}

function sz_aff_order_product_class_scope( WC_Order $order ): string {
    // Afiliado não recebe regra por Expedição. Quando houver ajuste por classe,
    // a classe é a classe de entrega do PRODUTO (product_shipping_class), não a modalidade do frete.
    foreach ( $order->get_items() as $item ) {
        if ( ! is_a( $item, 'WC_Order_Item_Product' ) ) continue;
        $product = $item->get_product();
        if ( ! $product ) continue;
        $cid = (int) $product->get_shipping_class_id();
        if ( $cid <= 0 && $product->is_type( 'variation' ) ) {
            $parent = wc_get_product( $product->get_parent_id() );
            $cid = $parent ? (int) $parent->get_shipping_class_id() : 0;
        }
        if ( $cid > 0 ) return 'class_' . $cid;
    }
    return 'global';
}


function sz_aff_ensure_checkout_link_commission_column(): void {
    global $wpdb;
    $table = $wpdb->prefix . 'senderzz_checkout_links';
    $cols = $wpdb->get_col( "SHOW COLUMNS FROM `{$table}`", 0 );
    $cols = is_array( $cols ) ? $cols : [];
    if ( ! in_array( 'affiliate_commission_pct', $cols, true ) ) {
        $wpdb->query( "ALTER TABLE `{$table}` ADD `affiliate_commission_pct` DECIMAL(10,2) NOT NULL DEFAULT 0.00" );
    }
}

function sz_aff_get_checkout_base_commission_pct( int $link_id ): float {
    global $wpdb;
    $link_id = absint( $link_id );
    if ( ! $link_id ) return 0.0;
    sz_aff_ensure_checkout_link_commission_column();
    $table = $wpdb->prefix . 'senderzz_checkout_links';
    $row = $wpdb->get_row( $wpdb->prepare( "SELECT affiliate_commission_pct, producer_id, user_id FROM {$table} WHERE id=%d LIMIT 1", $link_id ), ARRAY_A );
    if ( ! $row ) return 0.0;
    $v = $row['affiliate_commission_pct'] ?? '';
    if ( $v !== null && $v !== '' && (float) $v > 0 ) {
        return max( 0, min( 100, (float) $v ) );
    }
    // Links antigos, criados antes do campo de comissão do checkout, usam a comissão padrão do produtor.
    $producer_id = absint( $row['producer_id'] ?? 0 ) ?: absint( $row['user_id'] ?? 0 );
    return function_exists( 'sz_aff_producer_default_commission_pct' ) ? sz_aff_producer_default_commission_pct( $producer_id ) : 0.0;
}

function sz_aff_apply_commission_matrix_pct( int $affiliate_id, float $base_pct, ?int $link_id = null, ?WC_Order $order = null ): float {
    // REGRA OFICIAL SENDERZZ:
    // A comissão aplicada no pedido é SEMPRE a comissão cadastrada no afiliado pelo produtor
    // (wp_sz_affiliates.commission_pct).
    // O percentual do checkout/link NÃO pode sobrescrever a comissão do afiliado.
    // Isso corrige casos como checkout_link_id=73 gravando 10% mesmo o afiliado estando com 60%.
    return max( 0, min( 100, (float) $base_pct ) );
}

function sz_aff_effective_affiliate_commission_pct( int $affiliate_id, ?array $aff_row = null ): float {
    global $wpdb;
    $affiliate_id = absint( $affiliate_id );
    if ( ! $affiliate_id ) return 0.0;

    if ( ! is_array( $aff_row ) || ! array_key_exists( 'commission_pct', $aff_row ) ) {
        $aff_row = $wpdb->get_row( $wpdb->prepare(
            "SELECT commission_pct FROM " . sz_aff_table('sz_affiliates') . " WHERE id=%d LIMIT 1",
            $affiliate_id
        ), ARRAY_A );
    }

    return max( 0, min( 100, (float) ( $aff_row['commission_pct'] ?? 0 ) ) );
}

function sz_aff_order_checkout_link_id( WC_Order $order ): int {
    global $wpdb;
    $direct = absint( $order->get_meta( '_senderzz_checkout_link_id', true ) ?: $order->get_meta( '_senderzz_offer_link_id', true ) );
    if ( $direct ) return $direct;
    $token = trim( (string) $order->get_meta( '_senderzz_offer_token', true ) );
    if ( $token === '' && ! empty( $_POST['senderzz_offer_token'] ) ) $token = sanitize_text_field( wp_unslash( $_POST['senderzz_offer_token'] ) );
    if ( $token === '' && ! empty( $_COOKIE['senderzz_offer_token'] ) ) $token = sanitize_text_field( wp_unslash( $_COOKIE['senderzz_offer_token'] ) );
    if ( $token === '' ) return 0;
    $table = $wpdb->prefix . 'senderzz_checkout_links';
    return (int) $wpdb->get_var( $wpdb->prepare( "SELECT id FROM {$table} WHERE slug=%s OR token=%s LIMIT 1", $token, $token ) );
}


function sz_aff_product_class_scope_options_for_links( array $links ): array {
    $out = [];
    foreach ( $links as $link ) {
        $cid = function_exists( 'sz_aff_link_shipping_class_id' ) ? sz_aff_link_shipping_class_id( (array) $link, [] ) : 0;
        if ( $cid > 0 ) {
            $out[ 'class_' . $cid ] = function_exists( 'sz_aff_shipping_class_label' ) ? sz_aff_shipping_class_label( $cid ) : ( 'Classe #' . $cid );
        }
    }
    if ( empty( $out ) && function_exists( 'get_terms' ) ) {
        $terms = get_terms( [ 'taxonomy' => 'product_shipping_class', 'hide_empty' => false ] );
        if ( ! is_wp_error( $terms ) ) {
            foreach ( $terms as $term ) $out[ 'class_' . (int) $term->term_id ] = (string) $term->name;
        }
    }
    asort( $out, SORT_NATURAL | SORT_FLAG_CASE );
    return $out;
}

function sz_aff_visible_checkout_links_for_producer( int $producer_id ): array {
    global $wpdb;
    $producer_id = absint( $producer_id ); if ( ! $producer_id ) return [];
    $table = $wpdb->prefix . 'senderzz_checkout_links';
    $cols = $wpdb->get_col( "SHOW COLUMNS FROM {$table}", 0 ) ?: [];
    $owner_parts = [];$params=[];
    foreach ( [ 'user_id', 'producer_id' ] as $col ) { if ( in_array( $col, $cols, true ) ) { $owner_parts[] = "`{$col}`=%d"; $params[] = $producer_id; } }
    if ( empty( $owner_parts ) ) return [];
    $where = '(' . implode( ' OR ', $owner_parts ) . ')';
    if ( in_array( 'tipo', $cols, true ) ) $where .= " AND (tipo IS NULL OR tipo='' OR tipo='correio' OR tipo='expedicao' OR tipo='expedição')";
    if ( in_array( 'affiliate_visible', $cols, true ) ) $where .= " AND affiliate_visible=1";
    $order = in_array( 'created_at', $cols, true ) ? 'created_at DESC,id DESC' : 'id DESC';
    return $wpdb->get_results( $wpdb->prepare( "SELECT id,name,components_text,price_label FROM {$table} WHERE {$where} ORDER BY {$order} LIMIT 120", $params ), ARRAY_A ) ?: [];
}

function sz_aff_handle_producer_actions(): void {
    if ( empty( $_POST['sz_aff_action'] ) ) return;
    $uid = sz_aff_current_producer_id();
    if ( $uid <= 0 ) return;
    if ( ! wp_verify_nonce( sanitize_text_field( wp_unslash( $_POST['_wpnonce'] ?? '' ) ), 'sz_aff_panel' ) ) return;

    // Verificar que o uid é realmente um produtor válido no portal
    global $wpdb;
    $portal_row = $wpdb->get_row( $wpdb->prepare(
        "SELECT id, role, status FROM {$wpdb->prefix}senderzz_portal_users WHERE (id=%d OR wp_user_id=%d) AND status='active' LIMIT 1",
        $uid, $uid
    ) );
    // Afiliados puros não podem executar ações de produtor
    if ( $portal_row && $portal_row->role === 'affiliate' ) return;

    $act = sanitize_key( $_POST['sz_aff_action'] );

    if ( $act === 'create_invite' ) {
        sz_aff_create_default_invite( $uid, $uid );
    }

    if ( $act === 'toggle_auto' ) {
        update_user_meta( $uid, '_sz_aff_auto_approve', ! empty( $_POST['auto'] ) ? '1' : '0' );
        // default_commission_pct ignorado aqui: comissão base é do checkout, não do produtor
        update_user_meta( $uid, '_sz_aff_default_commission_pct', max( 0, min( 100, (float) str_replace( ',', '.', sanitize_text_field( wp_unslash( $_POST['default_commission_pct'] ?? '' ) ) ?: sz_aff_default_commission_pct() ) ) ) );
        if ( isset( $_POST['retention_days'] ) ) {
            update_user_meta( $uid, 'sz_aff_retention_days', max( 7, min( 365, absint( $_POST['retention_days'] ) ) ) );
        }
        if ( isset( $_POST['withdraw_fee'] ) ) {
            update_user_meta( $uid, 'sz_aff_withdraw_fee', max( 0, min( 50, (float) str_replace( ',', '.', sanitize_text_field( wp_unslash( $_POST['withdraw_fee'] ) ) ) ) ) );
        }
    }

    if ( $act === 'delete_affiliate' || $act === 'reject_affiliate' ) {
        $id = absint( $_POST['affiliate_id'] ?? 0 );
        if ( $id <= 0 ) goto redirect;
        $row = $wpdb->get_row( $wpdb->prepare( "SELECT id,user_id,status FROM " . sz_aff_table('sz_affiliates') . " WHERE id=%d AND producer_id=%d AND deleted_at IS NULL", $id, $uid ) );
        if ( $row ) {
            $wpdb->update( sz_aff_table('sz_affiliates'), [ 'status' => 'pending', 'deleted_at' => sz_aff_now() ], [ 'id' => $id ], [ '%s','%s' ], [ '%d' ] );
            if ( function_exists( 'sz_aff_sync_portal_access_after_affiliation_change' ) ) sz_aff_sync_portal_access_after_affiliation_change( (int) ( $row->user_id ?? 0 ) );
        }
    }

    if ( $act === 'bulk_delete_affiliates' ) {
        $ids_raw = sanitize_text_field( wp_unslash( $_POST['affiliate_ids'] ?? '' ) );
        $ids = array_slice( array_filter( array_map( 'absint', explode( ',', $ids_raw ) ) ), 0, 100 ); // Limite: 100 por vez
        foreach ( $ids as $id ) {
            $row = $wpdb->get_row( $wpdb->prepare( "SELECT id,user_id FROM " . sz_aff_table('sz_affiliates') . " WHERE id=%d AND producer_id=%d AND deleted_at IS NULL", $id, $uid ) );
            if ( $row ) {
                $wpdb->update( sz_aff_table('sz_affiliates'), [ 'status' => 'pending', 'deleted_at' => sz_aff_now() ], [ 'id' => $id ], [ '%s','%s' ], [ '%d' ] );
                if ( function_exists( 'sz_aff_sync_portal_access_after_affiliation_change' ) ) sz_aff_sync_portal_access_after_affiliation_change( (int) ( $row->user_id ?? 0 ) );
            }
        }
    }

    if ( $act === 'update_link_commission' ) {
        $affiliate_id = absint( $_POST['affiliate_id'] ?? 0 );
        $link_id      = absint( $_POST['link_id'] ?? 0 );
        // Adicional do afiliado limitado a 50% para evitar bypass do cap global
        $pct = max( 0, min( 50, (float) str_replace( ',', '.', sanitize_text_field( wp_unslash( $_POST['commission_pct'] ?? '0' ) ) ) ) );
        $row = $wpdb->get_row( $wpdb->prepare( "SELECT id FROM " . sz_aff_table('sz_affiliates') . " WHERE id=%d AND producer_id=%d AND deleted_at IS NULL AND status='active'", $affiliate_id, $uid ), ARRAY_A );
        if ( $row && $link_id ) sz_aff_upsert_link_commission_pct( $affiliate_id, $link_id, $pct );
    }

    if ( $act === 'update_product_class_commission' ) {
        $affiliate_id = absint( $_POST['affiliate_id'] ?? 0 );
        $scope        = sanitize_text_field( wp_unslash( $_POST['scope'] ?? 'global' ) );
        $pct          = max( -50, min( 50, (float) str_replace( ',', '.', sanitize_text_field( wp_unslash( $_POST['adjustment_pct'] ?? '0' ) ) ) ) );
        $row = $wpdb->get_row( $wpdb->prepare( "SELECT id FROM " . sz_aff_table('sz_affiliates') . " WHERE id=%d AND producer_id=%d AND deleted_at IS NULL AND status='active'", $affiliate_id, $uid ), ARRAY_A );
        if ( $row ) sz_aff_upsert_product_class_adjustment_pct( $affiliate_id, $scope, $pct );
    }

    if ( $act === 'update_affiliate' || $act === 'update_commission' ) {
        $id = absint( $_POST['affiliate_id'] ?? 0 );
        if ( $id <= 0 ) goto redirect;
        $row = $wpdb->get_row( $wpdb->prepare( "SELECT id,status,user_id FROM " . sz_aff_table('sz_affiliates') . " WHERE id=%d AND producer_id=%d AND deleted_at IS NULL", $id, $uid ), ARRAY_A );
        if ( $row ) {
            $current_status = (string) ( $row['status'] ?? 'pending' );
            $status = sanitize_key( $_POST['status'] ?? $current_status );
            if ( ! in_array( $status, [ 'pending', 'active' ], true ) ) $status = $current_status;
            // Adicional de comissão do afiliado: cap em 50% (base vem do checkout)
            $commission_pct = max( 0, min( 50, (float) str_replace( ',', '.', sanitize_text_field( wp_unslash( $_POST['commission_pct'] ?? '0' ) ) ) ) );
            $data = [ 'commission_pct' => $commission_pct ];
            if ( $act === 'update_affiliate' ) {
                $data['status'] = $status;
                $data['approved_at'] = $status === 'active' ? sz_aff_now() : null;
            }
            $wpdb->update( sz_aff_table('sz_affiliates'), $data, [ 'id' => $id ] );
            if ( $act === 'update_affiliate' && $status === 'active' && $current_status !== 'active' ) {
                sz_aff_ensure_portal_affiliate_user( (int) ( $row['user_id'] ?? 0 ) );
                sz_aff_send_affiliate_access_email( (int) ( $row['user_id'] ?? 0 ), sz_aff_get_producer_display_name( $uid ), 'active' );
                sz_aff_sync_portal_access_after_affiliation_change( (int) ( $row['user_id'] ?? 0 ) );
                // Popula vínculos token→afiliado para todos os checkouts do produtor
                sz_aff_register_all_tokens_for_affiliate( $id, $uid );
            }
        }
    }

    redirect:

    $redirect = wp_get_referer();
    if ( ! $redirect ) {
        $request_uri = isset( $_SERVER['REQUEST_URI'] ) ? wp_unslash( $_SERVER['REQUEST_URI'] ) : home_url( '/' );
        $redirect = home_url( $request_uri );
    }
    $redirect = remove_query_arg( [ '_wpnonce', 'sz_aff_action', 'affiliate_id', 'affiliate_ids', 'status', 'commission_pct', 'default_commission_pct', 'auto', 'link_id', 'scope', 'adjustment_pct' ], $redirect );
    wp_safe_redirect( $redirect );
    exit;
}
add_action( 'init', 'sz_aff_handle_producer_actions', 20 );





function sz_aff_render_producer_panel( $portal_user = null ): string {
    global $wpdb;
    $uid = sz_aff_current_producer_id( $portal_user );
    if ( $uid <= 0 ) return '<div class="sz-card">Faça login para acessar afiliados.</div>';

    $invite = $wpdb->get_row( $wpdb->prepare(
        "SELECT * FROM " . sz_aff_table('sz_invite_links') . " WHERE producer_id=%d AND status='active' AND (offer_link_id IS NULL OR offer_link_id=0) ORDER BY id DESC LIMIT 1",
        $uid
    ), ARRAY_A );

    if ( ! $invite ) {
        $token = sz_aff_create_default_invite( $uid, $uid );
        if ( $token ) {
            $invite = $wpdb->get_row( $wpdb->prepare(
                "SELECT * FROM " . sz_aff_table('sz_invite_links') . " WHERE producer_id=%d AND token=%s LIMIT 1",
                $uid,
                $token
            ), ARRAY_A );
        }
    }

    $affs = $wpdb->get_results( $wpdb->prepare(
        "SELECT a.*,u.display_name,u.user_email,w.balance,w.pending_balance,w.debt_amount FROM " . sz_aff_table('sz_affiliates') . " a LEFT JOIN {$wpdb->users} u ON u.ID=a.user_id LEFT JOIN " . sz_aff_table('sz_affiliate_wallet') . " w ON w.affiliate_id=a.id WHERE a.producer_id=%d AND a.deleted_at IS NULL ORDER BY FIELD(a.status,'pending','active'), a.created_at DESC LIMIT 300",
        $uid
    ), ARRAY_A );

    // Normaliza dados de exibição para evitar coluna de e-mail vazia quando o cadastro veio de convite/usuário parcial.
    foreach ( (array) $affs as &$sz_aff_row ) {
        $email = trim( (string) ( $sz_aff_row['user_email'] ?? $sz_aff_row['email'] ?? $sz_aff_row['affiliate_email'] ?? $sz_aff_row['invite_email'] ?? $sz_aff_row['customer_email'] ?? '' ) );
        $user_id_for_email = (int) ( $sz_aff_row['user_id'] ?? 0 );
        if ( $email === '' && $user_id_for_email > 0 ) {
            $u_obj = get_userdata( $user_id_for_email );
            if ( $u_obj && ! empty( $u_obj->user_email ) ) $email = (string) $u_obj->user_email;
        }
        if ( $email === '' && $user_id_for_email > 0 ) {
            foreach ( [ 'billing_email', '_billing_email', 'sz_email', '_sz_email', '_sz_aff_email' ] as $meta_key ) {
                $mv = trim( (string) get_user_meta( $user_id_for_email, $meta_key, true ) );
                if ( is_email( $mv ) ) { $email = $mv; break; }
            }
        }
        $sz_aff_row['_sz_email_display'] = $email !== '' ? $email : '—';
    }
    unset( $sz_aff_row );

    $counts = [ 'active' => 0, 'pending' => 0 ];
    foreach ( (array) $affs as $a ) {
        $st = (string) ( $a['status'] ?? 'pending' );
        if ( isset( $counts[ $st ] ) ) $counts[ $st ]++;
    }

    $invite_url   = $invite ? sz_aff_invite_url( (string) $invite['token'] ) : '';
    $default_pct  = number_format( sz_aff_producer_default_commission_pct( $uid ), 2, '.', '' );
    $auto_approve = sz_aff_is_producer_auto_approve( $uid );
    $commission_links = function_exists( 'sz_aff_visible_checkout_links_for_producer' ) ? sz_aff_visible_checkout_links_for_producer( $uid ) : [];
    $link_commission_rows = [];
    if ( $commission_links ) {
        $ids_aff = array_values( array_filter( array_map( 'absint', wp_list_pluck( $affs, 'id' ) ) ) );
        if ( $ids_aff ) {
            $ph = implode( ',', array_fill( 0, count( $ids_aff ), '%d' ) );
            $rows_lc = $wpdb->get_results( $wpdb->prepare( "SELECT affiliate_id,link_id,commission_pct FROM " . sz_aff_table('sz_affiliate_link_commissions') . " WHERE affiliate_id IN ({$ph})", $ids_aff ), ARRAY_A ) ?: [];
            foreach ( $rows_lc as $lc ) $link_commission_rows[(int)$lc['affiliate_id']][(int)$lc['link_id']] = (float)$lc['commission_pct'];
        }
    }


    $product_class_commission_rows = [];
    if ( $affs ) {
        sz_aff_ensure_product_class_commission_table();
        $ids_aff_product_class = array_values( array_filter( array_map( 'absint', wp_list_pluck( $affs, 'id' ) ) ) );
        if ( $ids_aff_product_class ) {
            $phd = implode( ',', array_fill( 0, count( $ids_aff_product_class ), '%d' ) );
            $rows_dc = $wpdb->get_results( $wpdb->prepare( "SELECT affiliate_id,scope,adjustment_pct FROM " . sz_aff_table('sz_affiliate_product_class_commissions') . " WHERE affiliate_id IN ({$phd})", $ids_aff_product_class ), ARRAY_A ) ?: [];
            foreach ( $rows_dc as $dc ) $product_class_commission_rows[(int)$dc['affiliate_id']][sz_aff_normalize_product_class_scope((string)$dc['scope'])] = (float)$dc['adjustment_pct'];
        }
    }

    ob_start(); ?>
<div class="sz-aff-panel sz-aff-panel-v2">
<style>
/* ═══════════════════════════════════════════════════════
   SENDERZZ AFILIADOS v198 — Redesign limpo
   Sem CSS legado. Sem header/tabs duplicados.
   Tabs controladas pelo dashboard via data-aff-view.
   ═══════════════════════════════════════════════════════ */

/* ── Painéis controlados pelo dashboard ──────────────── */
/* config: mostra .sz-aff-v2-config, esconde .sz-aff-v2-list  */
#sec-affiliates[data-aff-view="config"]    .sz-aff-v2-list    { display: none !important; }
#sec-affiliates[data-aff-view="config"]    .sz-aff-v2-config  { display: flex !important; }
#sec-affiliates[data-aff-view="resultados"] .sz-aff-v2-config { display: none !important; }
#sec-affiliates[data-aff-view="resultados"] .sz-aff-v2-list   { display: flex !important; }
#sec-affiliates[data-aff-view="historico"]  .sz-aff-v2-config { display: none !important; }
#sec-affiliates[data-aff-view="historico"]  .sz-aff-v2-list   { display: none !important; }
/* fallback sem contexto de dashboard */
.sz-aff-panel-v2:not([data-aff-root]) .sz-aff-v2-config { display: flex !important; }

/* ── Layout geral ────────────────────────────────────── */
.sz-aff-panel-v2 {
    --sz-surface: #ffffff;
    --sz-surface-alt: #f9fafb;
    --sz-bg: #f4f5f7;
    --sz-border: #e5e7eb;
    --sz-border-dark: #d8dee8;
    --sz-text-primary: #111827;
    --sz-text-secondary: #475467;
    --sz-text-muted: #8a94a6;
    --sz-brand: #E8650A;
    --sz-brand-hover: #D05808;
    --sz-brand-light: #FFF3EA;
}
.sz-root.sz-dark #sec-affiliates .sz-aff-panel-v2,
.sz-dark #sec-affiliates .sz-aff-panel-v2 {
    --sz-surface: #111827;
    --sz-surface-alt: #0f172a;
    --sz-bg: #0b1220;
    --sz-border: rgba(255,255,255,.12);
    --sz-border-dark: rgba(255,255,255,.18);
    --sz-text-primary: #f8fafc;
    --sz-text-secondary: #cbd5e1;
    --sz-text-muted: #94a3b8;
    --sz-brand: #E8650A;
    --sz-brand-hover: #D05808;
    --sz-brand-light: rgba(232,101,10,.14);
}
.sz-aff-panel-v2 { display: flex; flex-direction: column; gap: 16px; min-width: 0; }
.sz-aff-v2-config { flex-direction: column; gap: 16px; }
.sz-aff-v2-list   { flex-direction: column; gap: 0; }

/* ── Card base ───────────────────────────────────────── */
.sz-aff-v2-card {
    background: var(--sz-surface, #fff);
    border: 1px solid var(--sz-border, #e0e0e0);
    border-radius: var(--sz-radius-xl, 16px);
    padding: 22px 24px;
    min-width: 0;
}

/* ── Ícone da seção ──────────────────────────────────── */
.sz-aff-v2-icon {
    width: 42px; height: 42px; flex-shrink: 0;
    border-radius: var(--sz-radius-lg, 12px);
    background: var(--sz-brand-light, #FFF3EA);
    color: var(--sz-brand, #E8650A);
    display: flex; align-items: center; justify-content: center;
    font-size: var(--sz-text-xl);
}

/* ── Card de convite ─────────────────────────────────── */
.sz-aff-v2-invite-row {
    display: flex; align-items: center;
    gap: 20px;
}
.sz-aff-v2-invite-info {
    display: flex; align-items: center; gap: 14px; flex-shrink: 0;
}
.sz-aff-v2-invite-text .sz-aff-v2-card-title { margin: 0 0 3px; font-size: var(--sz-text-lg); font-weight: 700; color: var(--sz-text-primary, #1a1a1a); }
.sz-aff-v2-invite-text .sz-aff-v2-card-sub   { margin: 0; font-size: var(--sz-text-base); color: var(--sz-text-muted, #888); }
.sz-aff-v2-invite-url-bar {
    flex: 1;
    display: flex; align-items: center; gap: 0;
    border: 1px solid var(--sz-border, #e0e0e0);
    border-radius: var(--sz-radius-md, 8px);
    overflow: hidden;
    background: var(--sz-bg, #f4f5f7);
    min-width: 0;
}
.sz-aff-v2-invite-url-bar span {
    flex: 1; padding: 0 14px; height: 44px;
    display: flex; align-items: center;
    font-size: var(--sz-text-meta); font-family:var(--sz-font);
    color: var(--sz-text-secondary, #555);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
    min-width: 0;
}
.sz-aff-v2-invite-url-bar .sz-aff-v2-invite-btn,
.sz-aff-v2-btn.sz-aff-v2-invite-btn {
    flex-shrink: 0;
    border-radius: 0 var(--sz-radius-md, 8px) var(--sz-radius-md, 8px) 0;
    height: 44px;
    background: var(--sz-brand, #E8650A);
    color: #fff;
    border: none;
}


.sz-aff-v2-btn.sz-aff-v2-invite-btn:hover,
.sz-aff-v2-invite-url-bar .sz-aff-v2-invite-btn:hover {
    background: var(--sz-brand-hover, #D05808);
    color:#fff;
}
/* ── Botões ──────────────────────────────────────────── */
.sz-aff-v2-btn {
    display: inline-flex; align-items: center; justify-content: center; gap: 7px;
    height: 42px; padding: 0 20px;
    border: none; border-radius: var(--sz-radius-lg, 12px);
    background: var(--sz-brand, #E8650A); color: #fff;
    font-size: var(--sz-text-base); font-weight: 700; cursor: pointer;
    transition: var(--sz-transition-base, all .15s ease);
    white-space: nowrap; flex-shrink: 0; text-decoration: none;
}
.sz-aff-v2-btn:hover   { background: var(--sz-brand-hover, #D05808); }
.sz-aff-v2-btn:active  { transform: scale(.98); }
.sz-aff-v2-btn:disabled { opacity: .45; cursor: not-allowed; }
.sz-aff-v2-btn.sm      { height: 34px; padding: 0 14px; font-size: var(--sz-text-meta); border-radius: var(--sz-radius-md, 8px); }
.sz-aff-v2-btn.approve { background: var(--sz-brand, #E8650A); color:#fff; border:none; }
.sz-aff-v2-btn.approve:hover { background: var(--sz-brand-hover, #D05808); color:#fff; }
.sz-aff-v2-btn.reject  {
    background: var(--sz-surface, #fff); color: #ef4444;
    border: 1px solid #fca5a5;
}
.sz-aff-v2-btn.reject:hover { background: #fef2f2; }
/* Excluir: fundo laranja, texto branco */
.sz-aff-v2-btn.excluir {
    background: var(--sz-brand, #E8650A); color: #fff;
    border: none;
}
.sz-aff-v2-btn.excluir:hover { background: var(--sz-brand-hover, #D05808); }
.sz-aff-v2-btn.excluir[aria-disabled="true"] { cursor: not-allowed; }

/* ── Card comissão padrão ────────────────────────────── */
.sz-aff-v2-comm-row {
    display: flex; align-items: center;
    justify-content: space-between; gap: 20px;
}
.sz-aff-v2-comm-info { display: flex; align-items: center; gap: 14px; flex: 1; min-width: 0; }
.sz-aff-v2-comm-info .sz-aff-v2-card-title { margin: 0 0 3px; font-size: var(--sz-text-lg); font-weight: 700; color: var(--sz-text-primary, #1a1a1a); }
.sz-aff-v2-comm-info .sz-aff-v2-card-sub   { margin: 0; font-size: var(--sz-text-base); color: var(--sz-text-muted, #888); }
.sz-aff-v2-comm-ctrl { display: flex; align-items: flex-end; gap: 12px; flex-shrink: 0; }
.sz-aff-v2-comm-field { display: flex; flex-direction: column; gap: 5px; }
.sz-aff-v2-comm-field-lbl { font-size: var(--sz-text-sm); font-weight: 600; color: var(--sz-text-muted, #888); text-transform: none; letter-spacing:0; }
.sz-aff-v2-comm-wrap {
    display: flex; align-items: stretch;
    border: 1px solid var(--sz-border, #e0e0e0);
    border-radius: var(--sz-radius-md, 8px);
    overflow: hidden; background: var(--sz-surface, #fff);
}
.sz-aff-v2-comm-wrap:focus-within { border-color: var(--sz-brand, #E8650A); box-shadow: 0 0 0 3px rgba(232,101,10,.12); }
.sz-aff-v2-comm-input {
    width: 80px; height: 44px; border: none; outline: none;
    background: transparent; color: var(--sz-text-primary, #1a1a1a);
    font-size: var(--sz-text-lg); font-weight: 700; padding: 0 12px;
    -moz-appearance: textfield;
}
.sz-aff-v2-comm-input::-webkit-outer-spin-button,
.sz-aff-v2-comm-input::-webkit-inner-spin-button { -webkit-appearance: none; margin: 0; }
.sz-aff-v2-comm-suf {
    height: 44px; padding: 0 13px; display: flex; align-items: center;
    background: var(--sz-bg, #f4f5f7);
    border-left: 1px solid var(--sz-border, #e0e0e0);
    font-size: var(--sz-text-md); font-weight: 700; color: var(--sz-text-secondary, #555);
}

/* ── Card config por afiliado ────────────────────────── */
.sz-aff-v2-cfg-row { display: flex; align-items: flex-start; gap: 14px; }
.sz-aff-v2-cfg-body { flex: 1; min-width: 0; }
.sz-aff-v2-cfg-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }
.sz-aff-v2-cfg-texts .sz-aff-v2-card-title { margin: 0 0 3px; font-size: var(--sz-text-lg); font-weight: 700; color: var(--sz-text-primary, #1a1a1a); }
.sz-aff-v2-cfg-texts .sz-aff-v2-card-sub   { margin: 0; font-size: var(--sz-text-base); color: var(--sz-text-muted, #888); }
.sz-aff-v2-toggle-wrap { display: flex; align-items: center; gap: 10px; flex-shrink: 0; padding-top: 2px; }
.sz-aff-v2-toggle-lbl  { font-size: var(--sz-text-base); color: var(--sz-text-secondary, #555); white-space: nowrap; }
.sz-aff-v2-toggle { position: relative; width: 42px; height: 24px; }
.sz-aff-v2-toggle input { opacity: 0; width: 0; height: 0; position: absolute; }
.sz-aff-v2-toggle-slider {
    position: absolute; inset: 0; border-radius: 999px;
    background: var(--sz-border-dark, #ddd);
    transition: var(--sz-transition-base, all .15s ease); cursor: pointer;
}
.sz-aff-v2-toggle-slider:before {
    content: ''; position: absolute;
    height: 18px; width: 18px; left: 3px; bottom: 3px;
    background: #fff; border-radius: 50%;
    box-shadow: 0 1px 4px rgba(0,0,0,.18);
    transition: var(--sz-transition-base, all .15s ease);
}
.sz-aff-v2-toggle input:checked + .sz-aff-v2-toggle-slider { background: var(--sz-brand, #E8650A); }
.sz-aff-v2-toggle input:checked + .sz-aff-v2-toggle-slider:before { transform: translateX(18px); }
.sz-aff-v2-toggle input:focus + .sz-aff-v2-toggle-slider { box-shadow: 0 0 0 3px rgba(232,101,10,.15); }
.sz-aff-v2-info-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; margin-top: 14px; }
.sz-aff-v2-info-box  { border-radius: var(--sz-radius-md, 8px); padding: 14px 16px; font-size: var(--sz-text-base); color: var(--sz-text-secondary, #555); line-height: 1.55; }
.sz-aff-v2-info-box strong { display: flex; align-items:center; justify-content:center; font-size: var(--sz-text-base); font-weight: 700; color: var(--sz-text-primary, #1a1a1a); margin-bottom: 6px; }
.sz-aff-v2-info-box.orange { background: var(--sz-brand-light, #FFF3EA); border: 1px solid rgba(232,101,10,.2); }
.sz-aff-v2-info-box.neutral { background: var(--sz-bg, #f4f5f7); border: 1px solid var(--sz-border, #e0e0e0); }
.sz-aff-v2-preview {
    margin-top: 12px; background: var(--sz-bg, #f4f5f7);
    border: 1px solid var(--sz-border, #e0e0e0);
    border-radius: var(--sz-radius-md, 8px);
    padding: 12px 16px; font-size: var(--sz-text-base); color: var(--sz-text-secondary, #555);
}
.sz-aff-v2-preview strong { color: var(--sz-brand, #E8650A); }

/* ── Painel de afiliados ─────────────────────────────── */
.sz-aff-v2-list-card {
    background: var(--sz-surface, #fff);
    border: 1px solid var(--sz-border, #e0e0e0);
    border-radius: var(--sz-radius-xl, 16px);
    overflow: visible;
}
.sz-aff-v2-list-head {
    display: flex; align-items: center; justify-content: space-between;
    gap: 16px; padding: 20px 24px 0;
}
.sz-aff-v2-list-title { font-size: var(--sz-text-lg); font-weight: 700; color: var(--sz-text-primary, #1a1a1a); margin: 0 0 3px; }
.sz-aff-v2-list-sub   { font-size: var(--sz-text-base); color: var(--sz-text-muted, #888); margin: 0; }

/* ── Sub-tabs Aprovados / Pendentes ──────────────────── */
.sz-aff-v2-subtabs {
    display: flex; gap: 0;
    padding: 0 24px;
    border-bottom: 1px solid var(--sz-border, #e0e0e0);
    margin-top: 16px;
}
.sz-aff-v2-stab {
    height: 40px; padding: 0 18px; font-size: var(--sz-text-base); font-weight: 600;
    background: none; border: none; border-bottom: 2px solid transparent;
    cursor: pointer; color: var(--sz-text-muted, #888);
    transition: color .15s, border-color .15s;
    display: inline-flex; align-items: center; gap: 8px;
    white-space: nowrap; margin-bottom: -1px;
}
.sz-aff-v2-stab:hover { color: var(--sz-text-primary, #1a1a1a); }
.sz-aff-v2-stab.is-active { color: var(--sz-text-primary, #1a1a1a); border-bottom-color: var(--sz-text-primary, #1a1a1a); font-weight: 700; }
.sz-aff-v2-stab-cnt {
    display: inline-flex; align-items: center; justify-content: center;
    min-width: 20px; height: 20px; padding: 0 6px;
    border-radius: 999px; font-size: var(--sz-text-sm); font-weight: 700;
    background: var(--sz-bg, #f4f5f7); color: var(--sz-text-muted, #888);
}
.sz-aff-v2-stab.is-active .sz-aff-v2-stab-cnt { background: var(--sz-brand-light, #FFF3EA); color: var(--sz-brand, #E8650A); }
.sz-aff-v2-stab-cnt.w { background: var(--sz-warning-bg, #fff3cd); color: var(--sz-warning, #856404); }

/* ── Sub-painéis ─────────────────────────────────────── */
.sz-aff-v2-subpanel { display: none; padding: 16px 24px 22px; }
.sz-aff-v2-subpanel.is-active { display: block; }

/* ── Barra de filtros ────────────────────────────────── */
.sz-aff-v2-fbar {
    display: grid;
    grid-template-columns: minmax(420px, 1fr) 150px 112px;
    gap: 10px; margin-bottom: 16px;
    padding: 12px 14px;
    background: var(--sz-bg, #f4f5f7);
    border: 1px solid var(--sz-border, #e0e0e0);
    border-radius: var(--sz-radius-lg, 12px);
}
.sz-aff-v2-fbar input,
.sz-aff-v2-fbar select {
    height: 38px;
    border: 1px solid var(--sz-border, #e0e0e0);
    border-radius: var(--sz-radius-md, 8px);
    background: var(--sz-surface, #fff);
    color: var(--sz-text-primary, #1a1a1a);
    font-size: var(--sz-text-base); padding: 0 12px;
    outline: none; transition: var(--sz-transition-base, all .15s ease);
    box-sizing: border-box; width: 100%;
}
.sz-aff-v2-fbar input:focus,
.sz-aff-v2-fbar select:focus {
    border-color: var(--sz-brand, #E8650A);
    box-shadow: 0 0 0 3px rgba(232,101,10,.1);
}
.sz-aff-v2-fbar .sz-aff-v2-bulk-actions { min-width: 112px; }
.sz-aff-v2-fbar .sz-aff-v2-btn.excluir {
    height: 38px;
    width: 100%;
    min-width: 112px;
    border-radius: var(--sz-radius-md, 8px);
    background: var(--sz-brand, #E8650A);
    color: #fff;
    border: none;
    opacity: 1;
    filter: none;
}

/* ── Tabela de afiliados ─────────────────────────────── */
.sz-aff-v2-thead,
.sz-aff-v2-trow {
    display: grid;
    grid-template-columns: 22px 50px minmax(160px, 1fr) minmax(190px, 1.2fr) minmax(110px, .65fr) 108px 148px;
    gap: 14px; align-items: center; padding: 0 4px;
}
.sz-aff-v2-thead {
    height: 30px;
    font-size: var(--sz-text-sm); font-weight: 600;
    color: var(--sz-text-muted, #888);
    text-transform: none; letter-spacing:0;
    margin-bottom: 6px;
}
.sz-aff-v2-trow {
    min-height: 66px;
    border: 1px solid var(--sz-border, #e0e0e0);
    border-radius: var(--sz-radius-lg, 12px);
    background: var(--sz-surface, #fff);
    margin-bottom: 8px;
    padding: 10px 4px;
    transition: border-color .12s, box-shadow .12s;
}
.sz-aff-v2-trow:hover { border-color: var(--sz-border-dark, #ddd); box-shadow: var(--sz-shadow-sm, 0 2px 8px rgba(0,0,0,.06)); }
.sz-aff-v2-trow.is-pending {
    border-color: rgba(232,101,10,.3);
    background: var(--sz-brand-light, #FFF3EA);
}
.sz-aff-v2-trow.is-filtered { display: none !important; }
.sz-aff-v2-row-id   { font-size: var(--sz-text-meta); font-weight: 600; color: var(--sz-text-muted, #888); }
.sz-aff-v2-row-name strong { display: block; font-size: var(--sz-text-base); font-weight: 700; color: var(--sz-text-primary, #1a1a1a); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.sz-aff-v2-row-name span   { font-size: var(--sz-text-meta); color: var(--sz-text-muted, #888); }
.sz-aff-v2-row-cell { font-size: var(--sz-text-base); color: var(--sz-text-secondary, #555); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

/* ── Checkbox customizado ────────────────────────────── */
.sz-aff-v2-cb {
    appearance: none; -webkit-appearance: none;
    width: 16px; height: 16px; min-width: 16px;
    border: 1.5px solid var(--sz-border-dark, #ddd);
    border-radius: 4px; background: var(--sz-surface, #fff);
    cursor: pointer; position: relative;
    transition: var(--sz-transition-fast, all .12s ease);
    display: block;
}
.sz-aff-v2-cb:checked { background: var(--sz-brand, #E8650A); border-color: var(--sz-brand, #E8650A); }
.sz-aff-v2-cb:checked::after {
    content: ''; position: absolute;
    left: 4px; top: 1px;
    width: 5px; height: 9px;
    border: 2px solid #fff; border-top: none; border-left: none;
    transform: rotate(45deg);
}
.sz-aff-v2-cb:indeterminate { background: var(--sz-brand, #E8650A); border-color: var(--sz-brand, #E8650A); }
.sz-aff-v2-cb:indeterminate::after {
    content: ''; position: absolute;
    left: 2px; top: 6px;
    width: 8px; height: 2px;
    background: #fff; border: none;
}
.sz-aff-v2-cb:focus { box-shadow: 0 0 0 3px rgba(232,101,10,.15); outline: none; }

/* ── Input comissão inline ───────────────────────────── */
.sz-aff-v2-ci {
    display: flex; align-items: stretch;
    border: 1px solid var(--sz-border, #e0e0e0);
    border-radius: var(--sz-radius-md, 8px);
    overflow: hidden; background: var(--sz-surface, #fff);
    transition: border-color .12s;
}
.sz-aff-v2-ci:focus-within { border-color: var(--sz-brand, #E8650A); box-shadow: 0 0 0 3px rgba(232,101,10,.1); }
.sz-aff-v2-ci input {
    width: 58px; height: 34px; border: none; outline: none;
    background: transparent; color: var(--sz-text-primary, #1a1a1a);
    font-size: var(--sz-text-base); font-weight: 700; text-align: center; padding: 0;
    -moz-appearance: textfield;
}
.sz-aff-v2-ci input::-webkit-outer-spin-button,
.sz-aff-v2-ci input::-webkit-inner-spin-button { -webkit-appearance: none; margin: 0; }
.sz-aff-v2-ci-suf {
    height: 34px; padding: 0 8px; display: flex; align-items: center;
    background: var(--sz-bg, #f4f5f7);
    border-left: 1px solid var(--sz-border, #e0e0e0);
    font-size: var(--sz-text-meta); font-weight: 700; color: var(--sz-text-muted, #888);
}

/* ── Ações da linha ──────────────────────────────────── */
.sz-aff-v2-acts { display: flex; gap: 6px; align-items: center; }

/* ── Empty state ─────────────────────────────────────── */
.sz-aff-v2-empty {
    display: flex; flex-direction: column; align-items: center;
    justify-content: center; gap: 8px;
    padding: 36px 20px;
    border: 1px dashed var(--sz-border, #e0e0e0);
    border-radius: var(--sz-radius-lg, 12px);
    color: var(--sz-text-muted, #888); text-align: center;
}
.sz-aff-v2-empty-ico { font-size: var(--sz-text-3xl); opacity: .35; }
.sz-aff-v2-empty strong { font-size: var(--sz-text-md); font-weight: 600; color: var(--sz-text-secondary, #555); }
.sz-aff-v2-empty-filter { display: none; padding: 14px; text-align: center; font-size: var(--sz-text-base); color: var(--sz-text-muted, #888); background: var(--sz-bg, #f4f5f7); border-radius: var(--sz-radius-md, 8px); margin-top: 8px; }

/* ── Histórico ───────────────────────────────────────── */
.sz-aff-v2-hist-row {
    display: flex; align-items: center; justify-content: space-between;
    padding: 11px 14px;
    border: 1px solid var(--sz-border, #e0e0e0);
    border-radius: var(--sz-radius-md, 8px);
    background: var(--sz-surface, #fff);
    margin-bottom: 7px; font-size: var(--sz-text-base);
}
.sz-aff-v2-hist-row.avail { border-left: 3px solid var(--sz-brand, #E8650A); }
.sz-aff-v2-hist-name { font-weight: 700; color: var(--sz-text-primary, #1a1a1a); display: block; }
.sz-aff-v2-hist-meta { font-size: var(--sz-text-meta); color: var(--sz-text-muted, #888); }
.sz-aff-v2-hist-val  { font-weight: 700; color: var(--sz-brand, #E8650A); white-space: nowrap; }

/* ── Dark mode ───────────────────────────────────────── */
.sz-dark .sz-aff-v2-card,
.sz-dark .sz-aff-v2-list-card,
.sz-dark .sz-aff-v2-trow,
.sz-dark .sz-aff-v2-hist-row { background: var(--sz-surface, #111827) !important; border-color: var(--sz-border, rgba(255,255,255,.12)) !important; }
.sz-dark .sz-aff-v2-trow.is-pending { background: rgba(232,101,10,.08) !important; border-color: rgba(232,101,10,.3) !important; }
.sz-dark .sz-aff-v2-icon { background: rgba(232,101,10,.15); }
.sz-dark .sz-aff-v2-invite-url-bar,
.sz-dark .sz-aff-v2-fbar,
.sz-dark .sz-aff-v2-fbar input,
.sz-dark .sz-aff-v2-fbar select,
.sz-dark .sz-aff-v2-preview,
.sz-dark .sz-aff-v2-info-box.neutral,
.sz-dark .sz-aff-v2-ci { background: var(--sz-surface-alt, #0f172a) !important; border-color: var(--sz-border, rgba(255,255,255,.12)) !important; color: var(--sz-text-primary, #f8fafc) !important; }
.sz-dark .sz-aff-v2-info-box.orange { background: rgba(232,101,10,.1) !important; border-color: rgba(232,101,10,.25) !important; }
.sz-dark .sz-aff-v2-comm-wrap { background: var(--sz-surface, #111827) !important; border-color: var(--sz-border, rgba(255,255,255,.12)) !important; }
.sz-dark .sz-aff-v2-comm-suf,
.sz-dark .sz-aff-v2-ci-suf { background: rgba(255,255,255,.05) !important; border-color: var(--sz-border, rgba(255,255,255,.12)) !important; color: var(--sz-text-muted, #94a3b8) !important; }
.sz-dark .sz-aff-v2-comm-input,
.sz-dark .sz-aff-v2-ci input { color: var(--sz-text-primary, #f8fafc) !important; }
.sz-dark .sz-aff-v2-toggle-slider { background: rgba(255,255,255,.15); }
.sz-dark .sz-aff-v2-btn.reject { background: transparent !important; color: #fca5a5 !important; border-color: rgba(252,165,165,.4) !important; }
.sz-dark .sz-aff-v2-cb { background: var(--sz-surface-alt, #0f172a); border-color: var(--sz-border, rgba(255,255,255,.18)); }
.sz-dark .sz-aff-v2-subtabs { border-color: var(--sz-border, rgba(255,255,255,.12)); }
.sz-dark .sz-aff-v2-stab-cnt { background: rgba(255,255,255,.08); color: var(--sz-text-muted, #94a3b8); }
.sz-dark .sz-aff-v2-empty { border-color: var(--sz-border, rgba(255,255,255,.12)); }
.sz-dark .sz-aff-v2-empty-filter { background: var(--sz-surface-alt, #0f172a); }
.sz-dark .sz-aff-v2-row-name strong { color: var(--sz-text-primary, #f8fafc); }

/* ── Responsivo ──────────────────────────────────────── */
@media (max-width: 1180px) {
    .sz-aff-v2-invite-row,
    .sz-aff-v2-comm-row { flex-wrap: wrap; }
    .sz-aff-v2-comm-ctrl { width: 100%; justify-content: flex-start; }
    .sz-aff-v2-fbar { grid-template-columns: 1fr 1fr !important; }
    .sz-aff-v2-fbar input[type="search"] { grid-column: 1 / -1; }
    .sz-aff-v2-thead { display: none; }
    .sz-aff-v2-trow { grid-template-columns: 22px 1fr 1fr !important; row-gap: 8px !important; padding: 12px 8px !important; }
}
@media (max-width: 760px) {
    .sz-aff-v2-cfg-head { flex-direction: column; }
    .sz-aff-v2-info-grid { grid-template-columns: 1fr !important; }
    .sz-aff-v2-fbar { grid-template-columns: 1fr !important; }
    .sz-aff-v2-trow { grid-template-columns: 1fr !important; }
    .sz-aff-v2-list-head { flex-direction: column; align-items: flex-start; }
    .sz-aff-v2-card, .sz-aff-v2-list-card { border-radius: var(--sz-radius-lg, 12px); }
    .sz-aff-v2-card { padding: 16px 18px; }
    .sz-aff-v2-subpanel { padding: 14px 16px 18px; }
}
</style>

<?php /* JS ───────────────────────────────────────────── */ ?>
<script id="sz-aff-v2-js">
(function(){
    window.szAffV2Toast = function(msg, type) {
        if (window.szToast) { try { return window.szToast(msg, type || 'success', 1800); } catch(e){} }
        var old = document.querySelector('.sz-aff-v2-toast'); if (old) old.remove();
        var d = document.createElement('div');
        d.className = 'sz-aff-v2-toast';
        d.style.cssText = 'position:fixed;right:22px;bottom:22px;background:#1a1a1a;color:#fff;border-radius:12px;padding:11px 17px;font-size:var(--sz-text-base);font-weight:600;box-shadow:0 16px 40px rgba(0,0,0,.28);z-index:99999;display:flex;align-items:center;gap:8px;animation:szTI .15s ease';
        d.innerHTML = '<span>' + (type === 'error' ? '✕' : '✓') + '</span><span>' + msg + '</span>';
        document.body.appendChild(d);
        setTimeout(function(){ if (d.parentNode) d.remove(); }, 1800);
    };

    window.szAffV2Copy = function(el) {
        var t = el.dataset.copy || ''; if (!t) return;
        var ok = function(){ szAffV2Toast('Convite copiado', 'success'); };
        var fb = function(){
            var a = document.createElement('textarea'); a.value = t;
            a.style.cssText = 'position:fixed;opacity:0;top:0;left:0';
            document.body.appendChild(a); a.focus(); a.select();
            try { document.execCommand('copy'); ok(); } catch(e) {}
            a.remove();
        };
        navigator.clipboard && window.isSecureContext ? navigator.clipboard.writeText(t).then(ok).catch(fb) : fb();
    };

    window.szAffV2SetSub = function(id, btn) {
        var root = btn.closest('.sz-aff-v2-list-card');
        root.querySelectorAll('.sz-aff-v2-stab').forEach(function(b){ b.classList.toggle('is-active', b === btn); });
        root.querySelectorAll('.sz-aff-v2-subpanel').forEach(function(p){ p.classList.toggle('is-active', p.dataset.sub === id); });
    };

    window.szAffV2Filter = function(subId, root) {
        var panel = root.querySelector('.sz-aff-v2-subpanel[data-sub="' + subId + '"]');
        if (!panel) return;
        var q = ((panel.querySelector('.sz-aff-v2-fsearch') || {}).value || '').toLowerCase().trim();
        var date = ((panel.querySelector('.sz-aff-v2-fdate') || {}).value || '').trim();
        var rows = panel.querySelectorAll('.sz-aff-v2-trow');
        var visible = 0;
        rows.forEach(function(r){
            var ok = !q || (r.textContent || '').toLowerCase().indexOf(q) > -1;
            if (ok && date) ok = (r.dataset.created || '').indexOf(date) === 0;
            r.classList.toggle('is-filtered', !ok);
            if (ok) visible++;
        });
        var ef = panel.querySelector('.sz-aff-v2-empty-filter');
        if (ef) ef.style.display = visible ? 'none' : 'block';
        szAffV2SyncBulk(subId, root);
    };

    window.szAffV2SyncBulk = function(subId, root) {
        var panel = root.querySelector('.sz-aff-v2-subpanel[data-sub="' + subId + '"]');
        if (!panel) return;
        var cbs = [].slice.call(panel.querySelectorAll('.sz-aff-v2-trow:not(.is-filtered) .sz-aff-v2-cb'));
        var checked = cbs.filter(function(c){ return c.checked; });
        var inp = panel.querySelector('.sz-aff-v2-bulk-ids');
        var btn = panel.querySelector('.sz-aff-v2-bulk-btn');
        var master = panel.querySelector('.sz-aff-v2-cb-all');
        if (inp) inp.value = checked.map(function(c){ return c.value; }).join(',');
        if (btn) { var inactive = !checked.length; btn.dataset.inactive = inactive ? '1' : '0'; btn.setAttribute('aria-disabled', inactive ? 'true' : 'false'); }
        if (master) {
            master.checked = !!cbs.length && checked.length === cbs.length;
            master.indeterminate = checked.length > 0 && checked.length < cbs.length;
        }
    };

    window.szAffV2ToggleAll = function(subId, el, root) {
        var panel = root.querySelector('.sz-aff-v2-subpanel[data-sub="' + subId + '"]');
        if (!panel) return;
        panel.querySelectorAll('.sz-aff-v2-trow:not(.is-filtered) .sz-aff-v2-cb').forEach(function(c){ c.checked = el.checked; });
        szAffV2SyncBulk(subId, root);
    };

    /* ── Fetch AJAX helper ────────────────────────────────────── */
    function szAffAjax(data, onOk, onErr) {
        var fd = new FormData();
        Object.keys(data).forEach(function(k){ fd.append(k, data[k]); });
        fetch(window.ajaxurl || '/wp-admin/admin-ajax.php', { method: 'POST', body: fd, credentials: 'same-origin' })
        .then(function(r){ return r.json(); })
        .then(function(d){
            var ok = d.success;
            var msg = (d.data && d.data.message) || d.message || (ok ? 'Feito!' : 'Erro.');
            if (window.szToast) szToast(msg, ok ? 'success' : 'error', 2600);
            if (ok && onOk) onOk(d);
            if (!ok && onErr) onErr(d);
        })
        .catch(function(){
            if (window.szToast) szToast('Erro de conexão.', 'error', 2600);
            if (onErr) onErr();
        });
    }

    /* ── Toggle aprovação automática ─────────────────────────── */
    window.szAffV2ToggleAuto = function(checkbox, nonce) {
        var val = checkbox.checked ? '1' : '0';
        szAffAjax({ action: 'sz_aff_panel_action', aff_act: 'toggle_auto', auto: val, _wpnonce: nonce },
            null,
            function(){ checkbox.checked = !checkbox.checked; }
        );
    };

    /* ── Salvar comissão padrão ───────────────────────────────── */
    window.szAffV2SaveDefaultComm = function(btn, nonce) {
        var form = btn.closest('form');
        var input = form ? form.querySelector('.sz-aff-v2-comm-input') : null;
        if (!input) return;
        var val = parseFloat(input.value) || 0;
        btn.disabled = true;
        szAffAjax({ action: 'sz_aff_panel_action', aff_act: 'default_commission', default_commission_pct: val, _wpnonce: nonce },
            function(){
                btn.disabled = false;
                document.querySelectorAll('.sz-aff-v2-preview-pct').forEach(function(el){
                    el.textContent = val.toFixed(2).replace('.', ',') + '%';
                });
            },
            function(){ btn.disabled = false; }
        );
    };

    /* ── Salvar comissão individual ───────────────────────────── */
    window.szAffV2SaveComm = function(input) {
        var aid = input.dataset.aid, nonce = input.dataset.nonce, val = parseFloat(input.value) || 0;
        if (input.dataset.last === String(val)) return;
        input.disabled = true;
        szAffAjax({ action: 'senderzz_portal', szaction: 'affiliate_action', aff_act: 'update_commission', affiliate_id: aid, commission_pct: val, _ajax_nonce: nonce },
            function(){ input.disabled = false; input.dataset.last = String(val); },
            function(){ input.disabled = false; input.value = input.dataset.last; }
        );
    };

    /* ── Aprovar / Recusar ────────────────────────────────────── */
    window.szAffV2Action = function(btn, aid, act, nonce) {
        if (btn.disabled) return;
        var proceed = function(){
            btn.dataset.busy = '1';
            btn.setAttribute('aria-busy', 'true');
            szAffAjax({ action: 'senderzz_portal', szaction: 'affiliate_action', aff_act: act, affiliate_id: aid, _ajax_nonce: nonce },
                function(){
                    var row = btn.closest('.sz-aff-v2-trow');
                    if (row) {
                        row.style.transition = 'opacity .3s';
                        row.style.opacity = '0';
                        setTimeout(function(){ if (row.parentNode) row.parentNode.removeChild(row); }, 320);
                    }
                },
                function(){ btn.disabled = false; }
            );
        };
        if (act === 'reject') {
            if (document.body && document.body.classList.contains('sz-pwa-only') && window.szSenderzzConfirm) {
                window.szSenderzzConfirm({ icon:'👥', title:'Recusar afiliado?', text:'Essa ação remove o convite pendente.', okText:'Recusar', onConfirm:proceed });
                return;
            }
            if (!confirm('Recusar este afiliado?')) return;
        }
        proceed();
    };

    /* ── Excluir em lote ─────────────────────────────────────── */
    window.szAffV2BulkDelete = function(btn, subId, nonce) {
        if (!btn || btn.dataset.inactive === '1' || btn.dataset.busy === '1') return;
        var root = btn.closest('.sz-aff-panel-v2');
        var panel = root ? root.querySelector('.sz-aff-v2-subpanel[data-sub="' + subId + '"]') : null;
        var idsInput = panel ? panel.querySelector('.sz-aff-v2-bulk-ids') : null;
        var ids = idsInput ? idsInput.value : '';
        if (!ids) { szAffV2SyncBulk(subId, root); return; }
        var proceed = function(){
            btn.dataset.busy = '1';
            btn.setAttribute('aria-busy', 'true');
            szAffAjax({ action: 'sz_aff_panel_action', aff_act: 'bulk_delete_affiliates', affiliate_ids: ids, _wpnonce: nonce },
                function(){
                    ids.split(',').forEach(function(id){
                        id = id.trim(); if (!id || !root) return;
                        var cb = root.querySelector('.sz-aff-v2-cb[value="' + id + '"]');
                        if (cb) {
                            var trow = cb.closest('.sz-aff-v2-trow');
                            if (trow) { trow.style.transition = 'opacity .3s'; trow.style.opacity = '0'; setTimeout(function(){ if (trow.parentNode) trow.parentNode.removeChild(trow); }, 320); }
                        }
                    });
                    if (idsInput) idsInput.value = '';
                    btn.dataset.busy = '0';
                    btn.dataset.inactive = '1';
                    btn.setAttribute('aria-busy', 'false');
                    btn.setAttribute('aria-disabled', 'true');
                },
                function(){ btn.dataset.busy = '0'; btn.setAttribute('aria-busy', 'false'); }
            );
        };
        if (document.body && document.body.classList.contains('sz-pwa-only') && window.szSenderzzConfirm) {
            window.szSenderzzConfirm({ icon:'🗑️', title:'Excluir afiliados?', text:'Os afiliados selecionados serão removidos desta lista.', okText:'Excluir', onConfirm:proceed });
            return;
        }
        if (!confirm('Excluir os afiliados selecionados?')) return;
        proceed();
    };

    /* init sub-tabs */
    document.addEventListener('DOMContentLoaded', function(){
        document.querySelectorAll('.sz-aff-v2-list-card').forEach(function(card){
            if (!card.querySelector('.sz-aff-v2-stab.is-active')) {
                var first = card.querySelector('.sz-aff-v2-stab');
                if (first) first.classList.add('is-active');
            }
            if (!card.querySelector('.sz-aff-v2-subpanel.is-active')) {
                var fp = card.querySelector('.sz-aff-v2-subpanel');
                if (fp) fp.classList.add('is-active');
            }
        });
    });

    document.addEventListener('submit', function(e){
        if (e.target && e.target.closest && e.target.closest('.sz-aff-panel-v2')) {
            try { if (window.szRememberSection) window.szRememberSection('affiliates'); } catch(_) {}
        }
    }, true);
})();
</script>


<style>
/* Senderzz v387 - limpeza raiz dos títulos/card do painel de afiliados */
html body #sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-card-title,
html body #sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-card-sub,
html body #sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-list-title,
html body #sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-list-sub,
html body #sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-invite-text,
html body #sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-cfg-texts,
html body #sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-comm-info > div {
  border:0!important;
  outline:0!important;
  box-shadow:none!important;
  background:transparent!important;
  background-image:none!important;
  border-radius:0!important;
  padding:0!important;
}
html body #sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-card-title{
  color:var(--sz-text,#111827)!important;
  font-size:16px;
  line-height:1.22;
  font-weight:700;
  letter-spacing:-.015em;
  margin:0 0 4px!important;
}
html body #sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-card-sub{
  color:var(--sz-muted,#64748b)!important;
  font-size:13px;
  line-height:1.4;
  font-weight:500;
  margin:0!important;
}
html body #sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-card{
  border:1px solid var(--sz-border,#e5e7eb)!important;
  border-radius:16px!important;
  box-shadow:none!important;
  background:#fff!important;
}
</style>

<?php /* ═══════════════════════════════════════════
         PAINEL CONFIG
         ═══════════════════════════════════════════ */ ?>
<div class="sz-aff-v2-config">

    <?php /* Card convite */ ?>
    <div class="sz-aff-v2-card">
        <div class="sz-aff-v2-invite-row">
            <div class="sz-aff-v2-invite-info">
                <div class="sz-aff-v2-icon" aria-hidden="true">🔗</div>
                <div class="sz-aff-v2-invite-text">
                    <p class="sz-aff-v2-card-title">Link de convite padrão</p>
                    <p class="sz-aff-v2-card-sub">Convite único do produtor. Clique no botão para copiar.</p>
                </div>
            </div>
            <?php if ($invite_url): ?>
            <div class="sz-aff-v2-invite-url-bar">
                <span><?php echo esc_html($invite_url); ?></span>
                <button type="button" class="sz-aff-v2-btn sz-aff-v2-invite-btn"
                        onclick="szAffV2Copy(this)"
                        data-copy="<?php echo esc_attr($invite_url); ?>">
                    ↗ Copiar convite
                </button>
            </div>
            <?php endif; ?>
        </div>
    </div>

    <?php /* Card comissão padrão */ ?>
    <div class="sz-aff-v2-card">
        <form>
            <div class="sz-aff-v2-comm-row">
                <div class="sz-aff-v2-comm-info">
                    <div class="sz-aff-v2-icon" aria-hidden="true">⚙️</div>
                    <div>
                        <p class="sz-aff-v2-card-title">Comissão padrão para novos afiliados</p>
                        <p class="sz-aff-v2-card-sub">Define a comissão (%) aplicada automaticamente para todos os novos afiliados.</p>
                    </div>
                </div>
                <div class="sz-aff-v2-comm-ctrl">
                    <div class="sz-aff-v2-comm-field">
                        <div class="sz-aff-v2-comm-field-lbl">Comissão (%)</div>
                        <div class="sz-aff-v2-comm-wrap">
                            <input class="sz-aff-v2-comm-input" type="number" min="0" max="100" step="0.01"
                                   value="<?php echo esc_attr(number_format((float)$default_pct, 2, '.', '')); ?>">
                            <div class="sz-aff-v2-comm-suf">%</div>
                        </div>
                    </div>
                    <button type="button" class="sz-aff-v2-btn"
                            onclick="szAffV2SaveDefaultComm(this, '<?php echo esc_js(wp_create_nonce('sz_aff_panel')); ?>')">
                        Salvar
                    </button>
                </div>
            </div>
        </form>
    </div>

    <?php /* Card config por afiliado */ ?>
    <div class="sz-aff-v2-card">
        <div class="sz-aff-v2-cfg-row">
                <div class="sz-aff-v2-icon" aria-hidden="true">👥</div>
                <div class="sz-aff-v2-cfg-body">
                    <div class="sz-aff-v2-cfg-head">
                        <div class="sz-aff-v2-cfg-texts">
                            <p class="sz-aff-v2-card-title">Configuração por afiliado</p>
                            <p class="sz-aff-v2-card-sub">A comissão é definida por afiliado. O checkout pode ter valor personalizado que substitui a base.</p>
                        </div>
                        <div class="sz-aff-v2-toggle-wrap">
                            <span class="sz-aff-v2-toggle-lbl">Aprovação automática</span>
                            <label class="sz-aff-v2-toggle">
                                <input type="checkbox" <?php checked($auto_approve); ?>
                                       onchange="szAffV2ToggleAuto(this, '<?php echo esc_js(wp_create_nonce('sz_aff_panel')); ?>')">
                                <span class="sz-aff-v2-toggle-slider"></span>
                            </label>
                        </div>
                    </div>
                    <div class="sz-aff-v2-info-grid">
                        <div class="sz-aff-v2-info-box orange">
                            <strong>Como funciona</strong>
                            Se o checkout estiver preenchido, ele sobrescreve esta comissão. Se o checkout estiver vazio, será usada a comissão do afiliado ou a comissão padrão para novos afiliados.
                        </div>
                        <div class="sz-aff-v2-info-box neutral">
                            <strong>Importante</strong>
                            Esta configuração não altera as comissões dos afiliados já existentes. Apenas novos afiliados cadastrados após salvar serão afetados.
                        </div>
                    </div>
                    <div class="sz-aff-v2-preview">
                        Novos afiliados receberão <strong class="sz-aff-v2-preview-pct"><?php echo esc_html(number_format((float)$default_pct, 2, ',', '.')); ?>%</strong> de comissão nas vendas quando o checkout não tiver valor personalizado.
                    </div>
                </div>
            </div>
    </div>

</div><!-- /config -->

<?php /* ═══════════════════════════════════════════
         PAINEL AFILIADOS (resultados)
         ═══════════════════════════════════════════ */ ?>
<div class="sz-aff-v2-list">
    <div class="sz-aff-v2-list-card">

        <!-- Cabeçalho do card -->
        <div class="sz-aff-v2-list-head">
            <div style="display:flex;align-items:center;gap:14px">
                <div class="sz-aff-v2-icon" aria-hidden="true">👥</div>
                <div>
                    <p class="sz-aff-v2-list-title">Afiliados</p>
                    <p class="sz-aff-v2-list-sub">Aprove, recuse ou ajuste a comissão por afiliado.</p>
                </div>
            </div>
        </div>

        <!-- Sub-tabs -->
        <div class="sz-aff-v2-subtabs">
            <button type="button" class="sz-aff-v2-stab is-active"
                    onclick="szAffV2SetSub('active', this)">
                Aprovados
                <span class="sz-aff-v2-stab-cnt"><?php echo esc_html($counts['active']); ?></span>
            </button>
            <button type="button" class="sz-aff-v2-stab"
                    onclick="szAffV2SetSub('pending', this)">
                Pendentes
                <span class="sz-aff-v2-stab-cnt<?php echo $counts['pending'] > 0 ? ' w' : ''; ?>"><?php echo esc_html($counts['pending']); ?></span>
            </button>
        </div>

        <?php foreach (['active' => 'Aprovados', 'pending' => 'Pendentes'] as $panel_status => $panel_label): ?>
        <div class="sz-aff-v2-subpanel<?php echo $panel_status === 'active' ? ' is-active' : ''; ?>"
             data-sub="<?php echo esc_attr($panel_status); ?>">

            <!-- Filtros -->
            <div class="sz-aff-v2-fbar">
                <input type="search" class="sz-aff-v2-fsearch"
                       placeholder="Buscar por nome, e-mail ou telefone…"
                       oninput="szAffV2Filter('<?php echo esc_attr($panel_status); ?>', this.closest('.sz-aff-panel-v2'))">
                <input type="date" class="sz-aff-v2-fdate"
                       onchange="szAffV2Filter('<?php echo esc_attr($panel_status); ?>', this.closest('.sz-aff-panel-v2'))">
                <div class="sz-aff-v2-bulk-actions">
                    <input type="hidden" class="sz-aff-v2-bulk-ids" value="">
                    <button type="button" class="sz-aff-v2-btn excluir sz-aff-v2-bulk-btn" aria-disabled="true" data-inactive="1"
                            onclick="szAffV2BulkDelete(this, '<?php echo esc_attr($panel_status); ?>', '<?php echo esc_js(wp_create_nonce('sz_aff_panel')); ?>')">
                        🗑 Excluir
                    </button>
                </div>
            </div>

            <?php if (!$affs): ?>
            <div class="sz-aff-v2-empty">
                <span class="sz-aff-v2-empty-ico">👥</span>
                <strong>Nenhum afiliado ainda</strong>
                <span>Compartilhe o link de convite para começar.</span>
            </div>
            <?php else: ?>

            <!-- Cabeçalho da tabela -->
            <div class="sz-aff-v2-thead">
                <div>
                    <input type="checkbox" class="sz-aff-v2-cb sz-aff-v2-cb-all"
                           onchange="szAffV2ToggleAll('<?php echo esc_attr($panel_status); ?>', this, this.closest('.sz-aff-panel-v2'))">
                </div>
                <div>ID</div>
                <div>Nome</div>
                <div>E-mail</div>
                <div>Telefone</div>
                <div>Comissão</div>
                <div>Ações</div>
            </div>

            <?php
            $has_rows = false;
            foreach ((array) $affs as $a):
                $st = (string) ($a['status'] ?? 'pending');
                if ($st !== $panel_status) continue;
                $has_rows = true;
                $aid = (int) $a['id'];
                $is_pending = $st === 'pending';
                $comm_val = number_format((float) $a['commission_pct'], 2, '.', '');
            ?>
            <div class="sz-aff-v2-trow<?php echo $is_pending ? ' is-pending' : ''; ?>"
                 data-status="<?php echo esc_attr($st); ?>"
                 data-created="<?php echo esc_attr(substr((string)($a['created_at'] ?? ''), 0, 10)); ?>">

                <div>
                    <input type="checkbox" class="sz-aff-v2-cb" value="<?php echo esc_attr($aid); ?>"
                           onchange="szAffV2SyncBulk('<?php echo esc_attr($panel_status); ?>', this.closest('.sz-aff-panel-v2'))">
                </div>

                <div class="sz-aff-v2-row-id">#<?php echo esc_html($aid); ?></div>

                <div class="sz-aff-v2-row-name">
                    <strong><?php echo esc_html($a['display_name'] ?? ('Afiliado #' . $aid)); ?></strong>
                    <?php if ($is_pending): ?>
                    <span>Aguardando aprovação</span>
                    <?php else: ?>
                    <span>✓ Ativo<?php if (!empty($a['approved_at'])): ?> desde <?php echo esc_html(mysql2date('m/Y', (string) $a['approved_at'])); ?><?php endif; ?></span>
                    <?php endif; ?>
                </div>

                <div class="sz-aff-v2-row-cell" title="<?php echo esc_attr((string)($a['_sz_email_display'] ?? '—')); ?>">
                    <?php echo esc_html((string)($a['_sz_email_display'] ?? '—')); ?>
                </div>

                <div class="sz-aff-v2-row-cell">
                    <?php echo !empty($a['phone']) ? esc_html(sz_aff_format_phone((string) $a['phone'])) : '—'; ?>
                </div>

                <div>
                    <?php if (!$is_pending): ?>
                    <div class="sz-aff-v2-ci">
                        <input type="number" min="0" max="100" step="0.01"
                               value="<?php echo esc_attr($comm_val); ?>"
                               data-last="<?php echo esc_attr($comm_val); ?>"
                               data-aid="<?php echo esc_attr($aid); ?>"
                               data-nonce="<?php echo esc_attr(wp_create_nonce('senderzz_portal')); ?>"
                               onchange="szAffV2SaveComm(this)">
                        <div class="sz-aff-v2-ci-suf">%</div>
                    </div>
                    <?php else: ?>
                    <span style="font-size:var(--sz-text-meta);color:var(--sz-text-muted,#888)">—</span>
                    <?php endif; ?>
                </div>

                <div class="sz-aff-v2-acts">
                    <?php if ($is_pending): ?>
                    <button type="button" class="sz-aff-v2-btn sm approve"
                            onclick="szAffV2Action(this, <?php echo esc_attr($aid); ?>, 'approve', '<?php echo esc_js(wp_create_nonce('senderzz_portal')); ?>')">
                        ✓ Aprovar
                    </button>
                    <button type="button" class="sz-aff-v2-btn sm reject"
                            onclick="szAffV2Action(this, <?php echo esc_attr($aid); ?>, 'reject', '<?php echo esc_js(wp_create_nonce('senderzz_portal')); ?>')">
                        Recusar
                    </button>
                    <?php else: ?>
                    <button type="button" class="sz-aff-v2-btn sm"
                            onclick="var inp=this.closest('.sz-aff-v2-trow').querySelector('input[data-aid]'); if(inp) szAffV2SaveComm(inp)">
                        Salvar
                    </button>
                    <?php endif; ?>
                </div>

            </div>
            <?php endforeach; ?>

            <?php if (!$has_rows): ?>
            <div class="sz-aff-v2-empty">
                <span class="sz-aff-v2-empty-ico">👥</span>
                <strong>Nenhum afiliado nesta categoria</strong>
            </div>
            <?php endif; ?>

            <div class="sz-aff-v2-empty-filter">Nenhum resultado para este filtro.</div>

            <?php endif; ?>
        </div>
        <?php endforeach; ?>

    </div>
</div><!-- /list -->

</div><!-- /.sz-aff-panel-v2 -->
    <?php return ob_get_clean();
}
add_shortcode( 'senderzz_affiliates_panel', 'sz_aff_render_producer_panel' );

/* ═══════════════════════════════════════════════════════════════════════════
 * AJAX — ações do painel de afiliados sem reload de página
 * Cobre: toggle_auto, default_commission, bulk_delete, delete_affiliate
 * As ações approve/reject/update_commission já existem no Portal_Page.php.
 * ═══════════════════════════════════════════════════════════════════════════ */
add_action( 'wp_ajax_sz_aff_panel_action', 'sz_aff_panel_action_ajax' );
add_action( 'wp_ajax_nopriv_sz_aff_panel_action', 'sz_aff_panel_action_ajax' );

function sz_aff_panel_action_ajax(): void {
    $nonce = sanitize_text_field( wp_unslash( $_POST['_wpnonce'] ?? '' ) );
    if ( ! wp_verify_nonce( $nonce, 'sz_aff_panel' ) ) {
        wp_send_json( [ 'success' => false, 'message' => 'Sessão expirada. Recarregue a página.' ], 403 );
    }

    /* Descobrir o produtor real — separa portal_id do wp_user_id para não salvar meta no ID errado */
    global $wpdb;
    $uid = 0;
    $producer_wp_id = 0;
    $pu = null;
    if ( function_exists( 'sz_aff_portal_user_from_session' ) ) {
        $pu = sz_aff_portal_user_from_session();
        if ( $pu && ! empty( $pu->id ) ) {
            $uid = (int) $pu->id;
            $producer_wp_id = function_exists( 'sz_aff_portal_user_wp_id' ) ? sz_aff_portal_user_wp_id( $pu ) : absint( $pu->wp_user_id ?? 0 );
        }
    }
    if ( ! $uid ) {
        $uid = sz_aff_current_producer_id();
    }
    if ( $uid <= 0 ) {
        wp_send_json( [ 'success' => false, 'message' => 'Faça login para continuar.' ], 401 );
    }
    if ( ! $producer_wp_id ) {
        $producer_wp_id = (int) $wpdb->get_var( $wpdb->prepare(
            "SELECT wp_user_id FROM {$wpdb->prefix}senderzz_portal_users WHERE id=%d OR wp_user_id=%d LIMIT 1",
            $uid, $uid
        ) );
    }
    if ( ! $producer_wp_id && get_user_by( 'id', $uid ) ) {
        $producer_wp_id = $uid;
    }
    if ( ! $producer_wp_id ) {
        wp_send_json( [ 'success' => false, 'message' => 'Produtor sem usuário WordPress vinculado.' ], 400 );
    }

    /* Bloquear apenas afiliado puro. Produtor com classe/liberação pode salvar configurações. */
    $portal_user = $wpdb->get_row( $wpdb->prepare(
        "SELECT * FROM {$wpdb->prefix}senderzz_portal_users WHERE (id=%d OR wp_user_id=%d) AND status='active' LIMIT 1",
        $uid, $producer_wp_id
    ) );
    $portal_role = strtolower( trim( (string) ( $portal_user->role ?? '' ) ) );
    $has_producer_access = function_exists( 'sz_aff_portal_user_has_producer_access' ) ? sz_aff_portal_user_has_producer_access( $portal_user ?: (object) [ 'id' => $uid, 'wp_user_id' => $producer_wp_id, 'role' => $portal_role ] ) : false;

    $act = sanitize_key( $_POST['aff_act'] ?? '' );

    /* ── Solicitar afiliação (vitrine) — permitido para todos, incluindo afiliados ── */
    if ( $act === 'request_affiliation' ) {
        $target_producer_id = absint( $_POST['producer_id'] ?? 0 );
        if ( ! $target_producer_id ) {
            wp_send_json( [ 'success' => false, 'message' => 'Produtor inválido.' ] );
        }
        if ( $target_producer_id === $producer_wp_id ) {
            wp_send_json( [ 'success' => false, 'message' => 'Não é possível se afiliar ao próprio produtor.' ] );
        }
        $aff_table = function_exists( 'sz_aff_table' ) ? sz_aff_table( 'sz_affiliates' ) : $wpdb->prefix . 'sz_affiliates';
        $existing = $wpdb->get_var( $wpdb->prepare(
            "SELECT id FROM {$aff_table} WHERE producer_id=%d AND user_id=%d AND deleted_at IS NULL LIMIT 1",
            $target_producer_id, $producer_wp_id
        ) );
        if ( $existing ) {
            wp_send_json( [ 'success' => false, 'message' => 'Já existe um vínculo de afiliação com este produtor.' ] );
        }
        $comm_pct = function_exists( 'sz_aff_producer_default_commission_pct' )
            ? sz_aff_producer_default_commission_pct( $target_producer_id )
            : (float) ( get_user_meta( $target_producer_id, '_sz_aff_default_commission_pct', true ) ?: 10 );
        $wpdb->insert( $aff_table, [
            'producer_id'    => $target_producer_id,
            'user_id'        => $producer_wp_id,
            'status'         => 'pending',
            'commission_pct' => $comm_pct,
            'created_at'     => current_time( 'mysql' ),
        ], [ '%d', '%d', '%s', '%f', '%s' ] );
        if ( ! $wpdb->insert_id ) {
            wp_send_json( [ 'success' => false, 'message' => 'Erro ao registrar afiliação.' ] );
        }
        wp_send_json( [ 'success' => true, 'message' => 'Solicitação enviada. Aguarde aprovação do produtor.' ] );
    }

    if ( $portal_role === 'affiliate' && ! $has_producer_access ) {
        wp_send_json( [ 'success' => false, 'message' => 'Sem permissão.' ], 403 );
    }

    /* ── Toggle aprovação automática ─────────────────────────── */
    if ( $act === 'toggle_auto' ) {
        $auto = ! empty( $_POST['auto'] ) && $_POST['auto'] !== '0' ? '1' : '0';
        update_user_meta( $producer_wp_id, '_sz_aff_auto_approve', $auto );
        $label = $auto === '1' ? 'Aprovação automática ativada.' : 'Aprovação automática desativada.';
        wp_send_json( [ 'success' => true, 'message' => $label, 'auto' => $auto ] );
    }

    /* ── Salvar comissão padrão ───────────────────────────────── */
    if ( $act === 'default_commission' ) {
        $pct_raw = sanitize_text_field( wp_unslash( $_POST['default_commission_pct'] ?? '' ) );
        $pct = max( 0, min( 100, (float) str_replace( ',', '.', $pct_raw ) ) );
        update_user_meta( $producer_wp_id, '_sz_aff_default_commission_pct', $pct );
        wp_send_json( [ 'success' => true, 'message' => 'Comissão padrão atualizada para ' . number_format( $pct, 2, ',', '.' ) . '%.' ] );
    }

    /* ── Excluir afiliado único ───────────────────────────────── */
    if ( $act === 'delete_affiliate' || $act === 'reject_affiliate' ) {
        $aff_id = absint( $_POST['affiliate_id'] ?? 0 );
        if ( ! $aff_id ) wp_send_json( [ 'success' => false, 'message' => 'ID inválido.' ] );
        $aff_table = function_exists( 'sz_aff_table' ) ? sz_aff_table( 'sz_affiliates' ) : $wpdb->prefix . 'sz_affiliates';
        $row = $wpdb->get_row( $wpdb->prepare(
            "SELECT id, user_id FROM {$aff_table} WHERE id=%d AND producer_id=%d AND deleted_at IS NULL",
            $aff_id, $uid
        ), ARRAY_A );
        if ( ! $row ) wp_send_json( [ 'success' => false, 'message' => 'Afiliado não encontrado.' ] );
        $wpdb->update( $aff_table, [ 'status' => 'pending', 'deleted_at' => current_time( 'mysql' ) ], [ 'id' => $aff_id ], [ '%s', '%s' ], [ '%d' ] );
        if ( function_exists( 'sz_aff_sync_portal_access_after_affiliation_change' ) ) {
            sz_aff_sync_portal_access_after_affiliation_change( (int) ( $row['user_id'] ?? 0 ) );
        }
        wp_send_json( [ 'success' => true, 'message' => 'Afiliado removido.' ] );
    }

    /* ── Excluir em lote ─────────────────────────────────────── */
    if ( $act === 'bulk_delete_affiliates' ) {
        $ids_raw = sanitize_text_field( wp_unslash( $_POST['affiliate_ids'] ?? '' ) );
        $ids = array_slice( array_filter( array_map( 'absint', explode( ',', $ids_raw ) ) ), 0, 100 );
        if ( empty( $ids ) ) wp_send_json( [ 'success' => false, 'message' => 'Nenhum afiliado selecionado.' ] );
        $aff_table = function_exists( 'sz_aff_table' ) ? sz_aff_table( 'sz_affiliates' ) : $wpdb->prefix . 'sz_affiliates';
        $removed = 0;
        foreach ( $ids as $id ) {
            $row = $wpdb->get_row( $wpdb->prepare(
                "SELECT id, user_id FROM {$aff_table} WHERE id=%d AND producer_id=%d AND deleted_at IS NULL",
                $id, $uid
            ), ARRAY_A );
            if ( ! $row ) continue;
            $wpdb->update( $aff_table, [ 'status' => 'pending', 'deleted_at' => current_time( 'mysql' ) ], [ 'id' => $id ], [ '%s', '%s' ], [ '%d' ] );
            if ( function_exists( 'sz_aff_sync_portal_access_after_affiliation_change' ) ) {
                sz_aff_sync_portal_access_after_affiliation_change( (int) ( $row['user_id'] ?? 0 ) );
            }
            $removed++;
        }
        $label = $removed === 1 ? '1 afiliado removido.' : "{$removed} afiliados removidos.";
        wp_send_json( [ 'success' => true, 'message' => $label, 'removed' => $removed ] );
    }

    // F4: listar links de convite ativos
    if ( $act === 'get_invite_links' ) {
        $rows = $wpdb->get_results( $wpdb->prepare(
            "SELECT id, token, uses, allow_checkout_link, created_at FROM " . sz_aff_table('sz_invite_links') . "
              WHERE producer_id = %d AND status = 'active'
              ORDER BY id DESC LIMIT 10",
            $producer_wp_id
        ), ARRAY_A ) ?: [];
        $data = array_map( function( $r ) {
            return [
                'id'    => (int) $r['id'],
                'url'   => function_exists( 'sz_aff_invite_url' ) ? sz_aff_invite_url( (string) $r['token'] ) : '',
                'uses'  => (int) $r['uses'],
                'created_at' => wp_date( 'd/m/Y', strtotime( (string) $r['created_at'] ) ),
            ];
        }, $rows );
        wp_send_json( [ 'success' => true, 'links' => $data ] );
    }

    // F4: criar link de convite
    if ( $act === 'create_invite_link' ) {
        if ( ! function_exists( 'sz_aff_create_default_invite' ) ) {
            wp_send_json( [ 'success' => false, 'message' => 'Função indisponível.' ] );
        }
        $token = sz_aff_create_default_invite( $producer_wp_id, $producer_wp_id );
        if ( ! $token ) {
            wp_send_json( [ 'success' => false, 'message' => 'Erro ao criar link. Tente novamente.' ] );
        }
        $url = function_exists( 'sz_aff_invite_url' ) ? sz_aff_invite_url( $token ) : '';
        wp_send_json( [ 'success' => true, 'message' => 'Link criado.', 'url' => $url ] );
    }

    // F4: revogar link de convite
    if ( $act === 'revoke_invite_link' ) {
        $link_id = absint( $_POST['link_id'] ?? 0 );
        if ( ! $link_id ) {
            wp_send_json( [ 'success' => false, 'message' => 'ID inválido.' ] );
        }
        $row = $wpdb->get_var( $wpdb->prepare(
            "SELECT id FROM " . sz_aff_table('sz_invite_links') . " WHERE id=%d AND producer_id=%d AND status='active'",
            $link_id, $producer_wp_id
        ) );
        if ( ! $row ) {
            wp_send_json( [ 'success' => false, 'message' => 'Link não encontrado.' ] );
        }
        $wpdb->update( sz_aff_table('sz_invite_links'), [ 'status' => 'inactive' ], [ 'id' => $link_id ] );
        wp_send_json( [ 'success' => true, 'message' => 'Link revogado.' ] );
    }

    wp_send_json( [ 'success' => false, 'message' => 'Ação desconhecida.' ] );
}





function sz_aff_render_user_profile_fields( WP_User $user ): void {
    ?>
    <h2>Senderzz — Dados financeiros</h2>
    <table class="form-table" role="presentation">
        <tr>
            <th><label for="sz_document">CPF</label></th>
            <td><input type="text" name="sz_document" id="sz_document" value="<?php echo esc_attr( sz_aff_format_cpf( sz_aff_user_profile_value( $user->ID, 'sz_document' ) ) ); ?>" class="regular-text" maxlength="14"><p class="description">CPF do produtor ou afiliado.</p></td>
        </tr>
        <tr>
            <th><label for="sz_phone">Telefone</label></th>
            <td><input type="text" name="sz_phone" id="sz_phone" value="<?php echo esc_attr( sz_aff_format_phone( sz_aff_user_profile_value( $user->ID, 'sz_phone' ) ) ); ?>" class="regular-text" maxlength="15"><p class="description">Telefone com DDD.</p></td>
        </tr>
        <tr>
            <th><label for="sz_pix_key">Chave PIX</label></th>
            <td><input type="text" name="sz_pix_key" id="sz_pix_key" value="<?php echo esc_attr( sz_aff_user_profile_value( $user->ID, 'sz_pix_key' ) ); ?>" class="regular-text"><p class="description">Configure a chave PIX diretamente no perfil do usuário.</p></td>
        </tr>
        <tr>
            <th><label for="sz_bank_data">Dados bancários</label></th>
            <td><textarea name="sz_bank_data" id="sz_bank_data" rows="4" class="large-text"><?php echo esc_textarea( sz_aff_user_profile_value( $user->ID, 'sz_bank_data' ) ); ?></textarea><p class="description">Banco, agência, conta e titular.</p></td>
        </tr>
    </table>
    <?php
}
add_action( 'show_user_profile', 'sz_aff_render_user_profile_fields' );
add_action( 'edit_user_profile', 'sz_aff_render_user_profile_fields' );

function sz_aff_save_user_profile_fields( int $user_id ): void {
    if ( ! current_user_can( 'edit_user', $user_id ) ) return;
    global $wpdb;
    $document = isset( $_POST['sz_document'] ) ? sz_aff_digits( sanitize_text_field( wp_unslash( $_POST['sz_document'] ) ) ) : '';
    $phone    = isset( $_POST['sz_phone'] ) ? sz_aff_digits( sanitize_text_field( wp_unslash( $_POST['sz_phone'] ) ) ) : '';
    $pix_key  = isset( $_POST['sz_pix_key'] ) ? sanitize_text_field( wp_unslash( $_POST['sz_pix_key'] ) ) : '';
    $bank     = isset( $_POST['sz_bank_data'] ) ? sanitize_textarea_field( wp_unslash( $_POST['sz_bank_data'] ) ) : '';
    update_user_meta( $user_id, 'sz_document', $document );
    update_user_meta( $user_id, 'sz_phone', $phone );
    update_user_meta( $user_id, 'sz_pix_key', $pix_key );
    update_user_meta( $user_id, 'sz_bank_data', $bank );
    $wpdb->update( sz_aff_table('sz_affiliates'), [ 'document' => $document, 'phone' => $phone, 'pix_key' => $pix_key, 'bank_data' => $bank ], [ 'user_id' => $user_id ] );
}
add_action( 'personal_options_update', 'sz_aff_save_user_profile_fields' );
add_action( 'edit_user_profile_update', 'sz_aff_save_user_profile_fields' );

function sz_aff_admin_menu(): void {
    // Senderzz v221: registro legado cortado; página renderizada pelo menu unificado.
    return;
}
// Senderzz v221: menu legado removido da origem; renderização via Admin > Senderzz.
// add_action( 'admin_menu', 'sz_aff_admin_menu', 70 );

function sz_aff_render_admin_page(): void {
    global $wpdb;
    if ( isset($_POST['sz_aff_admin_nonce']) && wp_verify_nonce(sanitize_text_field(wp_unslash($_POST['sz_aff_admin_nonce'])),'sz_aff_admin') ) {
        update_option('sz_aff_default_commission_pct',        max(0,min(100,(float)str_replace(',','.',$_POST['commission']          ?? 10))));
        update_option('sz_aff_default_withdraw_fee',          max(0,(float)str_replace(',','.',$_POST['withdraw_fee']               ?? 2.99)));
        update_option('sz_aff_first_frustration_penalty',     max(0,(float)str_replace(',','.',$_POST['penalty_first']              ?? 5)));
        update_option('sz_aff_default_penalty_value',         max(0,(float)str_replace(',','.',$_POST['penalty']                    ?? 5)));
        update_option('sz_aff_producer_frustration_penalty',  max(0,(float)str_replace(',','.',$_POST['penalty_producer']           ?? 8)));
        update_option('sz_aff_default_retention_days',        max(30,absint($_POST['retention']                                     ?? 30)));
    }
    $wallet = $wpdb->get_row( "SELECT * FROM " . sz_aff_table('sz_admin_wallet') . " WHERE id=1", ARRAY_A );
    $pending = $wpdb->get_results( "SELECT w.*,a.user_id,u.display_name,u.user_email FROM " . sz_aff_table('sz_affiliate_withdrawals') . " w LEFT JOIN " . sz_aff_table('sz_affiliates') . " a ON a.id=w.affiliate_id LEFT JOIN {$wpdb->users} u ON u.ID=a.user_id WHERE w.status='pending' ORDER BY w.created_at ASC LIMIT 50", ARRAY_A );
    ?><div class="wrap"><h1>Senderzz Afiliados</h1><p>Carteira admin: <strong><?php echo esc_html(sz_aff_money((float)($wallet['balance'] ?? 0))); ?></strong></p><form method="post" style="background:#fff;border:1px solid #ccd0d4;padding:18px;max-width:760px"><?php wp_nonce_field('sz_aff_admin','sz_aff_admin_nonce'); ?><h2>Taxas padrão</h2><p><label>Comissão padrão (%)<br><input name="commission" value="<?php echo esc_attr(sz_aff_default_commission_pct()); ?>"></label></p><p><label>Taxa saque afiliado<br><input name="withdraw_fee" value="<?php echo esc_attr(sz_aff_default_withdraw_fee()); ?>"></label></p><h2>Frustrados</h2><p><label>Penalidade 1ª frustração afiliado (R$)<br><input name="penalty_first" class="regular-text" value="<?php echo esc_attr(sz_aff_first_frustration_penalty()); ?>"></label></p><p><label>Penalidade frustrado reincidente afiliado (R$)<br><input name="penalty" class="regular-text" value="<?php echo esc_attr(sz_aff_default_penalty_value()); ?>"></label></p><p><label>Penalidade frustrado reincidente produtor (R$)<br><input name="penalty_producer" class="regular-text" value="<?php echo esc_attr(sz_aff_producer_frustration_penalty()); ?>"></label><br><small>1ª frustração: produtor isento. 2ª+: cobra este valor via carteira TPC.</small></p><p><label>Retenção padrão/admin (dias)<br><input name="retention" value="<?php echo esc_attr(sz_aff_default_retention_days()); ?>"></label></p><button class="button button-primary">Salvar</button></form><h2>Saques pendentes</h2><table class="widefat"><thead><tr><th>Afiliado</th><th>Valor</th><th>Taxa</th><th>Líquido</th><th>Criado</th><th>Status</th></tr></thead><tbody><?php if(!$pending) echo '<tr><td colspan="6">Nenhum saque pendente.</td></tr>'; foreach($pending as $p): ?><tr><td><?php echo esc_html($p['display_name'] ?: $p['user_email']); ?></td><td><?php echo esc_html(sz_aff_money((float)$p['amount'])); ?></td><td><?php echo esc_html(sz_aff_money((float)$p['fee'])); ?></td><td><?php echo esc_html(sz_aff_money((float)$p['net_amount'])); ?></td><td><?php echo esc_html(mysql2date('d/m/Y H:i',$p['created_at'])); ?></td><td><?php echo esc_html(sz_aff_status_label((string)$p['status'])); ?></td></tr><?php endforeach; ?></tbody></table></div><?php
}



function sz_aff_get_active_rows_for_portal_user( $portal_user ): array {
    global $wpdb;
    $aff_portal_id = (int) ( $portal_user->id ?? 0 );
    $aff_wp_id     = function_exists( 'sz_aff_portal_user_wp_id' ) ? sz_aff_portal_user_wp_id( $portal_user ) : (int) ( $portal_user->wp_user_id ?? 0 );
    $user_ids      = array_values( array_unique( array_filter( array_map( 'absint', [ $aff_wp_id, $aff_portal_id ] ) ) ) );
    if ( empty( $user_ids ) ) return [];
    $where = count( $user_ids ) === 1
        ? 'a.user_id = %d'
        : 'a.user_id IN (' . implode( ',', array_fill( 0, count( $user_ids ), '%d' ) ) . ')';
    return $wpdb->get_results( $wpdb->prepare(
        "SELECT a.* FROM " . sz_aff_table('sz_affiliates') . " a WHERE {$where} AND a.status='active' AND a.deleted_at IS NULL",
        $user_ids
    ), ARRAY_A ) ?: [];
}

function sz_aff_get_producer_lookup_ids_from_rows( array $rows ): array {
    global $wpdb;
    $ids = [];
    foreach ( $rows as $row ) {
        $producer_id = absint( $row['producer_id'] ?? 0 );
        if ( ! $producer_id ) continue;

        // Busca IDs do produtor de forma controlada: inclui portal_id E wp_user_id
        // MAS valida que o wp_user_id pertence ao mesmo registro do portal,
        // evitando cruzamento com outro produtor que tenha portal_id = wp_user_id de outro.
        $portal_row = $wpdb->get_row( $wpdb->prepare(
            "SELECT id, wp_user_id FROM {$wpdb->prefix}senderzz_portal_users
             WHERE id = %d OR wp_user_id = %d
             ORDER BY id ASC LIMIT 1",
            $producer_id, $producer_id
        ), ARRAY_A );

        if ( $portal_row ) {
            $ids[] = (int) $portal_row['id'];
            if ( ! empty( $portal_row['wp_user_id'] ) ) {
                // Só adiciona wp_user_id se ele pertence a este mesmo registro —
                // impede que wp_user_id=X coincida com portal_id=X de outro produtor.
                $ids[] = (int) $portal_row['wp_user_id'];
            }
        } else {
            $ids[] = $producer_id;
        }
    }
    return array_values( array_unique( array_filter( array_map( 'absint', $ids ) ) ) );
}

function sz_aff_shipping_class_label( int $class_id ): string {
    if ( $class_id <= 0 ) return 'Sem classe definida';
    $term = get_term( $class_id, 'product_shipping_class' );
    if ( $term && ! is_wp_error( $term ) ) return (string) $term->name;
    return 'Classe #' . $class_id;
}

function sz_aff_link_shipping_class_id( array $link, array $aff_row = [] ): int {
    global $wpdb;
    foreach ( [ 'shipping_class_id', 'class_id', 'product_class_class_id' ] as $class_col ) {
        if ( isset( $link[ $class_col ] ) && (int) $link[ $class_col ] > 0 ) return (int) $link[ $class_col ];
    }

    $payload = json_decode( (string) ( $link['payload'] ?? '' ), true );
    $components = is_array( $payload['components'] ?? null ) ? $payload['components'] : [];
    foreach ( $components as $component ) {
        $pid = absint( $component['product_id'] ?? $component['id'] ?? $component['product'] ?? 0 );
        if ( ! $pid || ! function_exists( 'wc_get_product' ) ) continue;
        $product = wc_get_product( $pid );
        if ( ! $product ) continue;
        $cid = (int) $product->get_shipping_class_id();
        if ( $cid <= 0 && $product->is_type( 'variation' ) ) {
            $parent = wc_get_product( $product->get_parent_id() );
            $cid = $parent ? (int) $parent->get_shipping_class_id() : 0;
        }
        if ( $cid > 0 ) return $cid;
    }

    $producer_id = absint( $aff_row['producer_id'] ?? ( $link['producer_id'] ?? ( $link['user_id'] ?? 0 ) ) );
    if ( $producer_id > 0 ) {
        $cid = (int) $wpdb->get_var( $wpdb->prepare(
            "SELECT shipping_class_id FROM {$wpdb->prefix}senderzz_portal_users WHERE id=%d OR wp_user_id=%d LIMIT 1",
            $producer_id, $producer_id
        ) );
        if ( $cid > 0 ) return $cid;
    }
    return 0;
}

function sz_aff_commission_label( array $aff_row ): string {
    $pct = isset( $aff_row['commission_pct'] ) ? (float) $aff_row['commission_pct'] : 0;
    return rtrim( rtrim( number_format( $pct, 2, ',', '.' ), '0' ), ',' ) . '%';
}

function sz_aff_get_visible_checkout_links_for_portal_user( $portal_user ): array {
    global $wpdb;
    $rows = sz_aff_get_active_rows_for_portal_user( $portal_user );
    if ( empty( $rows ) ) return [];

    if ( function_exists( 'senderzz_portal_ensure_checkout_links_table' ) ) senderzz_portal_ensure_checkout_links_table();
    $table = $wpdb->prefix . 'senderzz_checkout_links';
    $cols  = $wpdb->get_col( "SHOW COLUMNS FROM {$table}", 0 ) ?: [];
    if ( empty( $cols ) ) return [];

    $producer_ids = sz_aff_get_producer_lookup_ids_from_rows( $rows );
    if ( empty( $producer_ids ) ) return [];

    $where_parts = [];
    $params      = [];
    foreach ( [ 'user_id', 'producer_id' ] as $owner_col ) {
        if ( ! in_array( $owner_col, $cols, true ) ) continue;
        $ph = implode( ',', array_fill( 0, count( $producer_ids ), '%d' ) );
        $where_parts[] = "`{$owner_col}` IN ({$ph})";
        foreach ( $producer_ids as $pid ) $params[] = $pid;
    }
    if ( empty( $where_parts ) ) return [];

    $where = '(' . implode( ' OR ', $where_parts ) . ')';
    if ( in_array( 'affiliate_visible', $cols, true ) ) {
        $where .= ' AND affiliate_visible=1';
    }
    if ( in_array( 'tipo', $cols, true ) ) {
        // O afiliado deve enxergar as ofertas principais; as linhas motoboy auxiliares entram apenas como botão ligado à oferta.
        $where .= " AND (tipo IS NULL OR tipo='' OR tipo='correio' OR tipo='expedicao' OR tipo='expedição')";
    }

    $order = in_array( 'created_at', $cols, true ) ? 'created_at DESC,id DESC' : 'id DESC';
    $links = $wpdb->get_results( $wpdb->prepare( "SELECT * FROM {$table} WHERE {$where} ORDER BY {$order} LIMIT 300", $params ), ARRAY_A ) ?: [];

    $affiliate_rows_by_producer = [];
    foreach ( $rows as $row ) {
        $pids = function_exists( 'sz_aff_possible_producer_ids' ) ? sz_aff_possible_producer_ids( absint( $row['producer_id'] ?? 0 ) ) : [ absint( $row['producer_id'] ?? 0 ) ];
        foreach ( $pids as $pid ) $affiliate_rows_by_producer[ (int) $pid ] = $row;
    }

    $out = [];
    $seen = [];
    foreach ( $links as $link ) {
        $id = absint( $link['id'] ?? 0 );
        if ( ! $id || isset( $seen[ $id ] ) ) continue;
        $seen[ $id ] = true;

        $owner = 0;
        foreach ( [ 'user_id', 'producer_id' ] as $owner_col ) {
            if ( isset( $link[ $owner_col ] ) && isset( $affiliate_rows_by_producer[ (int) $link[ $owner_col ] ] ) ) {
                $owner = (int) $link[ $owner_col ];
                break;
            }
        }
        $aff_row = $owner && isset( $affiliate_rows_by_producer[ $owner ] ) ? $affiliate_rows_by_producer[ $owner ] : reset( $rows );
        $affiliate_id = absint( $aff_row['id'] ?? 0 );

        $url = function_exists( 'sz_aff_resolve_producer_checkout_url_for_affiliate' ) ? sz_aff_resolve_producer_checkout_url_for_affiliate( $link ) : trim( (string) ( $link['url'] ?? '' ) );
        $token = trim( (string) ( $link['token'] ?? '' ) );
        if ( $token === '' ) $token = trim( (string) ( $link['slug'] ?? '' ) );
        if ( $url === '' ) continue;
        if ( $affiliate_id && function_exists( 'sz_aff_checkout_url_with_aff' ) ) {
            $url = sz_aff_checkout_url_with_aff( $url, $affiliate_id );
        }

        $class_id = function_exists( 'sz_aff_link_shipping_class_id' ) ? sz_aff_link_shipping_class_id( $link, (array) $aff_row ) : 0;
        // Grava vínculo permanente token → afiliado para resolução sem cookie
        if ( $affiliate_id && $token ) {
            sz_aff_register_checkout_token( $token, $affiliate_id, absint( $aff_row['producer_id'] ?? 0 ) );
        }
        $link['affiliate_id'] = $affiliate_id;
        $link['affiliate_url'] = $url;
        $fallback_commission_pct = isset( $aff_row['commission_pct'] ) ? (float) $aff_row['commission_pct'] : 0;
        $link_commission_pct = ( $affiliate_id && function_exists( 'sz_aff_get_link_commission_pct' ) ) ? sz_aff_get_link_commission_pct( $affiliate_id, $id, $fallback_commission_pct ) : $fallback_commission_pct;
        $link['affiliate_commission_pct'] = $link_commission_pct;
        $link['affiliate_commission_label'] = number_format( (float) $link_commission_pct, 2, ',', '.' ) . '%';
        $link['affiliate_shipping_class_id'] = $class_id;
        $link['affiliate_shipping_class_label'] = function_exists( 'sz_aff_shipping_class_label' ) ? sz_aff_shipping_class_label( $class_id ) : ( $class_id ? ( 'Classe #' . $class_id ) : 'Sem classe definida' );
        $link['display_name'] = function_exists( 'sz_aff_offer_label_from_row' ) ? sz_aff_offer_label_from_row( $link ) : ( $link['name'] ?? 'Checkout #' . $id );
        $out[] = $link;
    }
    return $out;
}

/**
 * Grava ou atualiza o vínculo permanente checkout_token → affiliate_id.
 * Idempotente: INSERT IGNORE — não duplica registros.
 */
function sz_aff_register_checkout_token( string $token, int $affiliate_id, int $producer_id = 0 ): void {
    if ( ! $token || ! $affiliate_id ) return;
    global $wpdb;
    $table = sz_aff_table('sz_affiliate_checkout_tokens');
    $wpdb->query( $wpdb->prepare(
        "INSERT IGNORE INTO {$table} (affiliate_id, checkout_token, producer_id, created_at) VALUES (%d, %s, %d, %s)",
        $affiliate_id,
        $token,
        $producer_id,
        sz_aff_now()
    ) );
}

/**
 * Resolve affiliate_id a partir do checkout_token gravado permanentemente.
 * Retorna null se não encontrado ou afiliado inativo.
 */
function sz_aff_resolve_affiliate_by_checkout_token( string $token ): ?array {
    if ( ! $token ) return null;
    global $wpdb;
    $table_tokens = sz_aff_table('sz_affiliate_checkout_tokens');
    $table_affs   = sz_aff_table('sz_affiliates');

    // Busca todos os afiliados vinculados a esse token
    $candidates = $wpdb->get_results( $wpdb->prepare(
        "SELECT a.* FROM {$table_tokens} t
         INNER JOIN {$table_affs} a ON a.id = t.affiliate_id
         WHERE t.checkout_token = %s
           AND a.status = 'active'
           AND a.deleted_at IS NULL
         ORDER BY t.id ASC",
        $token
    ), ARRAY_A ) ?: [];

    if ( empty( $candidates ) ) return null;

    // Só um afiliado para esse token — retorna direto
    if ( count( $candidates ) === 1 ) return $candidates[0];

    // Mais de um afiliado para o mesmo token:
    // usa o cookie como desempate para preservar o rastreamento correto
    $cookie_raw = function_exists( 'sz_aff_current_tracking_token' ) ? sz_aff_current_tracking_token() : '';
    if ( $cookie_raw && substr( $cookie_raw, 0, 3 ) === 'id:' ) {
        $cookie_aff_id = absint( substr( $cookie_raw, 3 ) );
        foreach ( $candidates as $candidate ) {
            if ( absint( $candidate['id'] ) === $cookie_aff_id ) return $candidate;
        }
    }

    // Blindagem: mesmo checkout_token pode existir para vários afiliados.
    // Sem r=/cookie inequívoco, não escolhe "o primeiro" para não creditar no afiliado errado.
    return null;
}

/**
 * Popula vínculos token → afiliado para todos os checkouts visíveis do produtor.
 * Chamado na aprovação do afiliado para garantir que a tabela está preenchida
 * mesmo antes do afiliado acessar o portal pela primeira vez.
 */
function sz_aff_register_all_tokens_for_affiliate( int $affiliate_id, int $producer_id ): void {
    if ( ! $affiliate_id || ! $producer_id ) return;
    global $wpdb;
    $table = $wpdb->prefix . 'senderzz_checkout_links';
    if ( ! $wpdb->get_var( "SHOW TABLES LIKE '{$table}'" ) ) return;

    $tokens = $wpdb->get_col( $wpdb->prepare(
        "SELECT token FROM {$table}
         WHERE user_id = %d
           AND affiliate_visible = 1
           AND token != ''",
        $producer_id
    ) ) ?: [];

    // Também inclui tokens dos checkouts motoboy vinculados
    $motoboy_tokens = $wpdb->get_col( $wpdb->prepare(
        "SELECT cl2.token
         FROM {$table} cl1
         INNER JOIN {$table} cl2 ON cl2.id = cl1.link_motoboy_id
         WHERE cl1.user_id = %d
           AND cl1.affiliate_visible = 1
           AND cl2.token != ''",
        $producer_id
    ) ) ?: [];

    foreach ( array_unique( array_merge( $tokens, $motoboy_tokens ) ) as $token ) {
        sz_aff_register_checkout_token( $token, $affiliate_id, $producer_id );
    }
}

function sz_aff_get_allowed_shipping_class_ids_for_portal_user( $portal_user ): array {
    global $wpdb;
    $rows = sz_aff_get_active_rows_for_portal_user( $portal_user );
    if ( empty( $rows ) ) return [];

    $links = function_exists( 'sz_aff_get_visible_checkout_links_for_portal_user' ) ? sz_aff_get_visible_checkout_links_for_portal_user( $portal_user ) : [];
    $class_ids = [];

    foreach ( $links as $link ) {
        $cid = (int) ( $link['affiliate_shipping_class_id'] ?? 0 );
        if ( $cid <= 0 && function_exists( 'sz_aff_link_shipping_class_id' ) ) $cid = sz_aff_link_shipping_class_id( $link, [] );
        if ( $cid > 0 ) $class_ids[] = $cid;
    }

    // Fallback essencial: se o checkout não carrega a classe, usa a classe do produtor que convidou.
    if ( empty( $class_ids ) ) {
        $producer_ids = sz_aff_get_producer_lookup_ids_from_rows( $rows );
        foreach ( $producer_ids as $producer_id ) {
            $cid = (int) $wpdb->get_var( $wpdb->prepare(
                "SELECT shipping_class_id FROM {$wpdb->prefix}senderzz_portal_users WHERE id=%d OR wp_user_id=%d LIMIT 1",
                $producer_id, $producer_id
            ) );
            if ( $cid > 0 ) $class_ids[] = $cid;
        }
    }

    return array_values( array_unique( array_map( 'absint', array_filter( $class_ids ) ) ) );
}

function sz_aff_render_affiliate_links( $portal_user, string $n = '' ): string {
    global $wpdb;
    $links = function_exists( 'sz_aff_get_visible_checkout_links_for_portal_user' ) ? sz_aff_get_visible_checkout_links_for_portal_user( $portal_user ) : [];

    // v258 — quando não há checkouts liberados, montar cards informativos
    // baseados nas linhas ativas de sz_affiliates (offer_name + classe do produtor).
    $synthetic_groups = [];
    if ( empty( $links ) ) {
        $active_rows = function_exists( 'sz_aff_get_active_rows_for_portal_user' ) ? sz_aff_get_active_rows_for_portal_user( $portal_user ) : [];
        foreach ( $active_rows as $aff_row ) {
            $producer_id = (int) ( $aff_row['producer_id'] ?? 0 );
            if ( ! $producer_id ) continue;
            $portal_producer = $wpdb->get_row( $wpdb->prepare(
                "SELECT id, wp_user_id, shipping_class_id FROM {$wpdb->prefix}senderzz_portal_users WHERE id=%d OR wp_user_id=%d ORDER BY id ASC LIMIT 1",
                $producer_id, $producer_id
            ), ARRAY_A );
            $cid   = (int) ( $portal_producer['shipping_class_id'] ?? 0 );
            $label = function_exists( 'sz_aff_shipping_class_label' ) ? sz_aff_shipping_class_label( $cid ) : ( $cid ? 'Classe #' . $cid : 'Produto' );
            $offer_name = trim( (string) ( $aff_row['offer_name'] ?? '' ) ) ?: 'Produto Senderzz';
            $commission_pct = (float) ( $aff_row['commission_pct'] ?? 0 );
            $key = $cid > 0 ? (string) $cid : ( 'p' . $producer_id );
            if ( ! isset( $synthetic_groups[ $key ] ) ) $synthetic_groups[ $key ] = [ 'label' => $label, 'items' => [] ];
            $synthetic_groups[ $key ]['items'][] = [
                'offer_name'     => $offer_name,
                'commission_pct' => $commission_pct,
                'aff_id'         => (int) $aff_row['id'],
            ];
        }
    }

    $groups = [];
    foreach ( $links as $link ) {
        $cid = (int) ( $link['affiliate_shipping_class_id'] ?? 0 );
        $label = (string) ( $link['affiliate_shipping_class_label'] ?? '' );
        if ( $label === '' ) $label = function_exists( 'sz_aff_shipping_class_label' ) ? sz_aff_shipping_class_label( $cid ) : ( $cid ? 'Classe #' . $cid : 'Sem classe definida' );
        $key = $cid > 0 ? (string) $cid : '0';
        if ( ! isset( $groups[ $key ] ) ) $groups[ $key ] = [ 'label' => $label, 'links' => [] ];
        $groups[ $key ]['links'][] = $link;
    }

    $product_class_commission_rows = [];

    ob_start(); ?>
    <div class="sz-section-pad" id="sec-links-affiliate">
        <style>
        #sec-links-affiliate .sz-aff-links-card{background:var(--c1,#fff);border:1px solid var(--bd,#e5e7eb);border-radius:22px;overflow:hidden;margin-bottom:16px} 
        #sec-links-affiliate .sz-aff-links-head{padding:22px 24px;border-bottom:1px solid var(--bd,#e5e7eb);display:flex;align-items:center;justify-content:space-between;gap:14px} 
        #sec-links-affiliate .sz-aff-links-title{font-size:var(--sz-text-xl);font-weight:700;margin:0;color:var(--tx,#111827)} 
        #sec-links-affiliate .sz-aff-links-sub{font-size:var(--sz-text-base);color:var(--tx2,#64748b);margin:4px 0 0} 
        #sec-links-affiliate .sz-aff-links-body{padding:16px 18px 20px} 
        #sec-links-affiliate .sz-aff-class-title{font-size:var(--sz-text-lg);font-weight:700;color:var(--tx,#111827);display:flex;align-items:center;gap:8px;margin:0}
        #sec-links-affiliate .sz-aff-class-title:before{content:'';width:9px;height:9px;border-radius:999px;background:#E8650A;display:inline-block}
        #sec-links-affiliate .sz-aff-links-row,#sec-links-affiliate .sz-aff-links-grid{display:grid;grid-template-columns:1.15fr 1.35fr .65fr .65fr .65fr 1fr;gap:12px;align-items:center} #sec-links-affiliate .sz-aff-links-row>div:last-child{display:flex;align-items:center;justify-content:flex-end;gap:6px} 
        #sec-links-affiliate .sz-aff-links-grid{padding:0 12px 10px;font-size:var(--sz-text-xs);font-weight:700;color:var(--tx3,#94a3b8);text-transform:none;letter-spacing:0} 
        #sec-links-affiliate .sz-aff-links-row{padding:14px 12px;border:1px solid var(--bd,#e5e7eb);border-radius:16px;margin-bottom:10px;background:var(--c2,#f8fafc)} 
        #sec-links-affiliate .sz-aff-links-name{font-weight:700;color:var(--tx,#111827)} 
        #sec-links-affiliate .sz-aff-links-muted{font-size:var(--sz-text-base);color:var(--tx2,#64748b)} 
        #sec-links-affiliate .sz-aff-links-price,#sec-links-affiliate .sz-aff-links-comm{font-weight:700;color:var(--tx,#111827)} 
        #sec-links-affiliate .sz-aff-links-comm{color:#E8650A}
        #sec-links-affiliate .sz-aff-copy{height:38px;border:0;border-radius:12px;background:#E8650A;color:#fff;font-weight:700;cursor:pointer;padding:0 14px} 
        .sz-root.sz-dark #sec-links-affiliate .sz-aff-links-card{background:#111827;border-color:rgba(255,255,255,.10)} 
        .sz-root.sz-dark #sec-links-affiliate .sz-aff-links-row{background:#0f172a;border-color:rgba(255,255,255,.10)} 
        .sz-root.sz-dark #sec-links-affiliate .sz-aff-links-title,.sz-root.sz-dark #sec-links-affiliate .sz-aff-class-title,.sz-root.sz-dark #sec-links-affiliate .sz-aff-links-name,.sz-root.sz-dark #sec-links-affiliate .sz-aff-links-price{color:#f8fafc} 
        @media(max-width:900px){#sec-links-affiliate .sz-aff-links-grid{display:none}#sec-links-affiliate .sz-aff-links-row{grid-template-columns:1fr}}
        </style>
        <?php if ( function_exists( 'sz_banner' ) ) sz_banner( 'VENDAS', 'Links de checkout', 'Copie seus links identificados dos produtores vinculados.', '', '' ); ?>
        <?php if ( empty( $links ) ) : ?>
            <?php if ( ! empty( $synthetic_groups ) ) : ?>
                <?php foreach ( $synthetic_groups as $sg ) : ?>
                <div class="sz-aff-links-card">
                    <div class="sz-aff-links-head">
                        <div><h2 class="sz-aff-class-title"><?php echo esc_html( $sg['label'] ); ?></h2>
                        <p class="sz-aff-links-sub">Afiliação ativa · checkout ainda não publicado pelo produtor</p></div>
                    </div>
                    <div class="sz-aff-links-body">
                        <div class="sz-aff-links-grid"><span>Produto</span><span></span><span></span><span>Comissão</span><span></span></div>
                        <?php foreach ( $sg['items'] as $si ) : ?>
                        <div class="sz-aff-links-row">
                            <div class="sz-aff-links-name"><?php echo esc_html( $si['offer_name'] ); ?></div>
                            <div></div>
                            <div></div>
                            <div class="sz-aff-links-comm"><?php echo esc_html( number_format( $si['commission_pct'], 2, ',', '.' ) . '%' ); ?></div>
                            <div><span style="display:inline-flex;align-items:center;height:38px;padding:0 14px;border-radius:12px;background:#f1f5f9;color:#64748b;font-size:var(--sz-text-meta);font-weight:700">⏳ Aguardando checkout</span></div>
                        </div>
                        <?php endforeach; ?>
                    </div>
                </div>
                <?php endforeach; ?>
            <?php else : ?>
            <div class="sz-aff-links-card"><div class="sz-aff-links-head"><div><h2 class="sz-aff-links-title">Links disponíveis</h2><p class="sz-aff-links-sub">0 links liberados</p></div></div><div class="sz-aff-links-body"><div class="sz-empty"><p>Nenhum checkout foi liberado para afiliados pelo produtor ainda.</p></div></div></div>
            <?php endif; ?>
        <?php else : ?>
            <?php foreach ( $groups as $group ) : $group_links = $group['links']; ?>
                <div class="sz-aff-links-card">
                    <div class="sz-aff-links-head"><div><h2 class="sz-aff-class-title"><?php echo esc_html( $group['label'] ); ?></h2><p class="sz-aff-links-sub"><?php echo esc_html( count( $group_links ) . ' ' . ( count( $group_links ) === 1 ? 'link liberado' : 'links liberados' ) ); ?></p></div></div>
                    <div class="sz-aff-links-body">
                        <div class="sz-aff-links-grid"><span>Nome</span><span>Produto(s)</span><span>Valor</span><span>Comissão</span><span>Vlr. Comissão</span><span>Link</span></div>
                        <?php foreach ( $group_links as $link ) :
                            $url = (string) ( $link['affiliate_url'] ?? $link['url'] ?? '' );
                            $name = (string) ( $link['display_name'] ?? $link['name'] ?? 'Checkout' );
                            $products = (string) ( $link['components_text'] ?? '—' );
                            $price = (string) ( $link['price_label'] ?? '' );
                            $commission = (string) ( $link['affiliate_commission_label'] ?? '—' );
                            if ( $price === '' || $price === '—' ) $price = isset( $link['display_value'] ) && (float) $link['display_value'] > 0 ? sz_aff_money( (float) $link['display_value'] ) : '—';
                        ?>
                        <?php
                        // Calcular valor da comissão
                        $commission_value_raw = 0.0;
                        if ( isset( $link['display_value'] ) && (float) $link['display_value'] > 0 ) {
                            $commission_value_raw = (float) $link['display_value'] * ( (float) ( $link['affiliate_commission_pct'] ?? 0 ) / 100 );
                        }
                        $commission_value_fmt = $commission_value_raw > 0 ? sz_aff_money( $commission_value_raw ) : '—';
                        // Link motoboy
                        $mb_aff_url = '';
                        if ( ! empty( $link['link_motoboy_id'] ) ) {
                            global $wpdb;
                            $mb_raw = $wpdb->get_row( $wpdb->prepare( "SELECT url, token, slug FROM {$wpdb->prefix}senderzz_checkout_links WHERE id=%d LIMIT 1", (int)$link['link_motoboy_id'] ), ARRAY_A );
                            if ( $mb_raw ) {
                                $mb_aff_url = trim( (string)( $mb_raw['url'] ?? '' ) );
                                if ( $mb_aff_url === '' ) {
                                    $mb_tok = trim( (string)( $mb_raw['token'] ?? $mb_raw['slug'] ?? '' ) );
                                    if ( $mb_tok ) $mb_aff_url = add_query_arg( [ 'sz' => $mb_tok, 'szm' => '1' ], home_url('/checkouts/lp/') );
                                }
                                if ( $mb_aff_url && ! empty( $link['affiliate_id'] ) && function_exists('sz_aff_checkout_url_with_aff') ) {
                                    $mb_aff_url = sz_aff_checkout_url_with_aff( $mb_aff_url, (int)$link['affiliate_id'] );
                                }
                            }
                        }
                        ?>
                        <div class="sz-aff-links-row">
                            <div class="sz-aff-links-name"><?php echo esc_html( $name ); ?></div>
                            <div class="sz-aff-links-muted"><?php echo esc_html( $products ?: '—' ); ?></div>
                            <div class="sz-aff-links-price"><?php echo esc_html( $price ); ?></div>
                            <div class="sz-aff-links-comm"><?php echo esc_html( $commission ); ?></div>
                            <div class="sz-aff-links-comm"><?php echo esc_html( $commission_value_fmt ); ?></div>
                            <div style="display:flex;flex-direction:column;align-items:flex-start;gap:6px">
                                <button type="button" class="sz-aff-copy" onclick="szCopyText(this,<?php echo esc_js( wp_json_encode( $url ) ); ?>)">📋 Copiar link</button>
                                <?php if ( $mb_aff_url ) : ?>
                                <button type="button" class="sz-aff-motoboy-btn" onclick="szCopyText(this,<?php echo esc_js( wp_json_encode( $mb_aff_url ) ); ?>)">🏍️ Copiar link Motoboy</button>
                                <?php endif; ?>
                            </div>
                        </div>
                        <?php endforeach; ?>
                    </div>
                </div>
            <?php endforeach; ?>
        <?php endif; ?>
    </div>
    <?php return ob_get_clean();
}

function sz_aff_render_affiliate_wallet( $portal_user ): string {
    global $wpdb;

    $aff_ids = function_exists( 'sz_aff_get_affiliate_ids_for_portal_user' ) ? sz_aff_get_affiliate_ids_for_portal_user( $portal_user, 'active' ) : [];
    $aff_ids = array_values( array_unique( array_filter( array_map( 'absint', (array) $aff_ids ) ) ) );

    if ( empty( $aff_ids ) ) {
        return '<div class="sz-section-pad"><div class="sz-card" style="padding:22px;border-radius:20px"><div class="sz-kicker">Financeiro</div><h2 style="margin:6px 0 8px">Carteira</h2><p style="color:#64748b;margin:0">Você ainda não tem vínculo ativo.</p></div></div>';
    }

    foreach ( $aff_ids as $aid ) {
        if ( function_exists( 'sz_aff_ensure_wallet' ) ) sz_aff_ensure_wallet( (int) $aid );
    }

    $ids_sql = implode( ',', $aff_ids );
    $wallet_table = sz_aff_table( 'sz_affiliate_wallet' );
    $tx_table     = sz_aff_table( 'sz_affiliate_transactions' );

    $wallet = $wpdb->get_row( "SELECT SUM(balance) balance, SUM(pending_balance) pending_balance, SUM(debt_amount) debt_amount FROM {$wallet_table} WHERE affiliate_id IN ({$ids_sql})", ARRAY_A ) ?: [];

    $available = (float) ( $wallet['balance'] ?? 0 );
    $pending   = (float) ( $wallet['pending_balance'] ?? 0 );
    $analysis  = (float) ( $wallet['debt_amount'] ?? 0 );
    $portal_wp_uid = (int) ( $portal_user->wp_user_id ?? $portal_user->ID ?? 0 );
    if ( $portal_wp_uid && function_exists( 'sz_cod_wallet_summary' ) ) {
        $cod_sum = sz_cod_wallet_summary( $portal_wp_uid );
        $available += (float) ( $cod_sum['available'] ?? 0 );
        $pending   += (float) ( $cod_sum['pending'] ?? 0 );
        $analysis  += (float) ( $cod_sum['analysis'] ?? 0 );
    }

    $txs = $wpdb->get_results( "SELECT amount,status,description,created_at,order_id FROM {$tx_table} WHERE affiliate_id IN ({$ids_sql}) ORDER BY created_at DESC LIMIT 8", ARRAY_A ) ?: [];
    if ( ! empty( $portal_wp_uid ) && function_exists( 'sz_cod_wallet_history' ) ) {
        foreach ( sz_cod_wallet_history( $portal_wp_uid, '30d' ) as $cod_tx ) {
            $txs[] = [
                'amount'      => (float) ( $cod_tx['net'] ?? 0 ),
                'status'      => (string) ( $cod_tx['status'] ?? '' ),
                'description' => (string) ( $cod_tx['description'] ?? 'Recebimento COD' ),
                'created_at'  => current_time( 'mysql' ),
                'order_id'    => preg_replace( '/\D+/', '', (string) ( $cod_tx['order'] ?? '' ) ),
            ];
        }
    }


    $product_class_commission_rows = [];

    ob_start(); ?>
    <div class="sz-section-pad sz-aff-wallet-as-producer">
        <?php if ( function_exists( 'sz_banner' ) ) sz_banner( 'Financeiro', 'Carteira', 'Saldo, saques e histórico financeiro da sua conta.' ); ?>

        <div class="sz-wallet-top-cards">
            <div class="sz-wtc sz-wtc--available">
                <div class="sz-wtc-icon">▣</div>
                <div>
                    <div class="sz-wtc-label">Saldo disponível</div>
                    <div class="sz-wtc-value"><?php echo esc_html( sz_aff_money( $available ) ); ?></div>
                    <small>Disponível para saque</small>
                </div>
            </div>
            <div class="sz-wtc sz-wtc--pending">
                <div class="sz-wtc-icon">◷</div>
                <div>
                    <div class="sz-wtc-label">Saldo pendente</div>
                    <div class="sz-wtc-value"><?php echo esc_html( sz_aff_money( $pending ) ); ?></div>
                    <small>Aguardando liberação</small>
                </div>
            </div>
            <div class="sz-wtc sz-wtc--analysis">
                <div class="sz-wtc-icon">↕</div>
                <div>
                    <div class="sz-wtc-label">Saque em análise</div>
                    <div class="sz-wtc-value"><?php echo esc_html( sz_aff_money( $analysis ) ); ?></div>
                    <small>Aguardando aprovação</small>
                </div>
            </div>
        </div>

        
        <style id="sz-aff-wallet-v169-tabs">
          .sz-aff-wallet-as-producer .sz-wallet-panel{display:none}.sz-aff-wallet-as-producer .sz-wallet-panel.is-active{display:block}
          .sz-aff-wallet-as-producer .sz-wallet-tab{cursor:pointer}.sz-aff-wallet-as-producer .sz-wallet-tab.active{background:#E8650A!important;color:#fff!important;border-color:#E8650A!important}
        </style>
        <script>
          window.szAffWalletTab=function(tab,btn){
            var root=(btn&&btn.closest('.sz-aff-wallet-as-producer'))||document.querySelector('.sz-aff-wallet-as-producer'); if(!root) return false;
            root.querySelectorAll('.sz-wallet-tab').forEach(function(b){b.classList.remove('active')}); if(btn) btn.classList.add('active');
            root.querySelectorAll('[data-aff-wallet-panel]').forEach(function(p){p.classList.toggle('is-active',p.getAttribute('data-aff-wallet-panel')===tab)});
            return false;
          };
        </script>
        <div class="sz-wallet-tabs" aria-label="Navegação da carteira">
            <div class="sz-wallet-tabs-left">
                <button type="button" class="sz-wallet-tab active" onclick="return szAffWalletTab('overview',this)">Visão geral</button>
                <button type="button" class="sz-wallet-tab" onclick="return szAffWalletTab('transactions',this)">Transações</button>
                <button type="button" class="sz-wallet-tab" onclick="return szAffWalletTab('withdrawals',this)">Saques</button>
            </div>
            <div class="sz-wallet-date">📅 Últimos 30 dias</div>
        </div>

        <div class="sz-wallet-panel is-active" data-aff-wallet-panel="overview"><div class="sz-wallet-mock-grid">
            <div class="sz-wallet-mock-chart">
                <div class="sz-wallet-mock-chart-title">Resumo de movimentações</div>
                <p class="sz-wallet-mock-sub">Entradas e saídas no período selecionado.</p>
                <div class="sz-wallet-chart-lines"></div>
            </div>
            <div class="sz-wallet-mock-card">
                <div style="display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:10px">
                    <div>
                        <h3>Últimas transações</h3>
                        <p class="sz-wallet-mock-sub">Confira suas movimentações mais recentes.</p>
                    </div>
                    <a href="#" class="sz-wallet-outline-btn">Ver todas</a>
                </div>
                <?php if ( empty( $txs ) ) : ?>
                    <div class="sz-wallet-empty">
                        <div class="sz-wallet-empty-icon">▤</div>
                        <strong>Nenhuma transação encontrada</strong>
                        <span>Suas transações aparecerão aqui.</span>
                    </div>
                <?php else : ?>
                    <div class="sz-aff-wallet-tx-list">
                        <?php foreach ( $txs as $t ) : ?>
                            <div class="sz-aff-wallet-tx-row" style="display:grid;grid-template-columns:1fr auto;gap:10px;padding:10px 0;border-bottom:1px solid var(--bd,#e5e7eb);font-size:var(--sz-text-base)">
                                <div>
                                    <strong><?php echo esc_html( $t['description'] ?: ( 'Pedido #' . ( $t['order_id'] ?? '' ) ) ); ?></strong><br>
                                    <small style="color:var(--tx3,#94a3b8)"><?php echo esc_html( mysql2date( 'd/m/Y H:i', $t['created_at'] ?? '' ) ); ?> · <?php echo esc_html( ucfirst( (string) $t['status'] ) ); ?></small>
                                </div>
                                <strong style="color:#16a34a"><?php echo esc_html( sz_aff_money( (float) $t['amount'] ) ); ?></strong>
                            </div>
                        <?php endforeach; ?>
                    </div>
                <?php endif; ?>
            </div>
        </div></div>

        <div class="sz-wallet-panel" data-aff-wallet-panel="transactions"><div class="sz-wallet-ledgers-grid-v28">
            <div class="sz-card sz-wallet-ledger-card-v28">
                <div class="sz-card-head" style="display:flex;align-items:center;justify-content:space-between;gap:12px">
                    <div style="display:flex;align-items:center;gap:10px">
                        <div class="sz-wh-ico">📋</div>
                        <div><h3 style="margin:0">Histórico de movimentação</h3><small>Recebimentos passados confirmados.</small></div>
                    </div>
                </div>
                <div style="padding:20px 22px">
                    <div style="display:flex;gap:8px;flex-wrap:wrap;margin-bottom:16px">
                        <?php foreach ( [ 'Hoje', 'Ontem', '7 dias', '30 dias', 'Este mês' ] as $i => $lbl ) : ?>
                            <button type="button" class="sz-wallet-period-btn<?php echo $i === 0 ? ' active' : ''; ?>"><?php echo esc_html( $lbl ); ?></button>
                        <?php endforeach; ?>
                    </div>
                    <?php if ( empty( $txs ) ) : ?>
                        <p style="color:var(--tx3);font-size:var(--sz-text-base);padding:16px 12px">Nenhum registro.</p>
                    <?php else : ?>
                        <div class="sz-wallet-th-row" style="display:grid;grid-template-columns:100px minmax(130px,1fr) 78px 112px 76px 66px 78px;gap:8px;padding:8px 12px"><span>Data</span><span>Descrição</span><span>Pedido</span><span>Movimentação</span><span>Valor</span><span>Taxa</span><span>Líquido</span></div>
                        <?php foreach ( $txs as $t ) : $amt = (float) $t['amount']; ?>
                            <div style="display:grid;grid-template-columns:100px minmax(130px,1fr) 78px 112px 76px 66px 78px;gap:8px;padding:10px 12px;border-bottom:1px solid var(--bd,#e5e7eb);font-size:var(--sz-text-meta);align-items:center">
                                <span><?php echo esc_html( mysql2date( 'd/m/Y', $t['created_at'] ?? '' ) ); ?></span><span><?php echo esc_html( $t['description'] ?: 'Comissão de afiliado' ); ?></span><span><?php echo esc_html( $t['order_id'] ? '#' . $t['order_id'] : '—' ); ?></span><span><?php echo esc_html( ucfirst( (string) $t['status'] ) ); ?></span><strong><?php echo esc_html( sz_aff_money( $amt ) ); ?></strong><span>R$ 0,00</span><strong><?php echo esc_html( sz_aff_money( $amt ) ); ?></strong>
                            </div>
                        <?php endforeach; ?>
                    <?php endif; ?>
                </div>
            </div>

            <div class="sz-card sz-wallet-ledger-card-v28">
                <div class="sz-card-head" style="display:flex;align-items:center;justify-content:space-between;gap:12px">
                    <div style="display:flex;align-items:center;gap:10px">
                        <div class="sz-wh-ico">🗓️</div>
                        <div><h3 style="margin:0">Lançamentos Futuros</h3><small>Comissões a receber e datas de liberação.</small></div>
                    </div>
                </div>
                <div style="padding:20px 22px">
                    <div style="display:flex;gap:8px;flex-wrap:wrap;margin-bottom:16px">
                        <?php foreach ( [ 'Hoje', 'Amanhã', '7 dias', '15 dias', '30 dias' ] as $i => $lbl ) : ?>
                            <button type="button" class="sz-wallet-period-btn<?php echo $i === 0 ? ' active' : ''; ?>"><?php echo esc_html( $lbl ); ?></button>
                        <?php endforeach; ?>
                    </div>
                    <p style="color:var(--tx3);font-size:var(--sz-text-base);padding:16px 12px">Nenhum lançamento futuro.</p>
                </div>
            </div>
        </div></div>
        <div class="sz-wallet-panel" data-aff-wallet-panel="withdrawals">
          <div class="sz-card" style="padding:22px;border-radius:22px">
            <h3 style="margin:0 0 8px">Saques</h3>
            <p style="margin:0 0 14px;color:var(--tx2,#64748b);font-weight:700">Solicite saque quando houver saldo disponível. O Admin analisa em Financeiro COD &gt; Saques.</p>
            <div style="display:flex;gap:10px;flex-wrap:wrap;align-items:end;max-width:520px">
              <label style="font-weight:700;font-size:var(--sz-text-meta);color:var(--tx2,#64748b);flex:1;min-width:180px">Valor do saque<br>
                <input id="sz-aff-withdraw-amount" value="<?php echo esc_attr( number_format( max(0,$available), 2, ',', '.' ) ); ?>" style="width:100%;border:1px solid var(--bd,#d1d5db);border-radius:12px;padding:11px 12px;margin-top:6px">
              </label>
              <button type="button" class="sz-wallet-tab active" style="background:#E8650A!important;color:#fff!important;border-color:#E8650A!important;padding:12px 18px" onclick="return szAffRequestWithdraw(this)">Solicitar saque</button>
            </div>
            <small style="display:block;margin-top:10px;color:var(--tx3,#94a3b8)">Taxa aplicada conforme regra do produtor/admin. Valor mínimo: R$ 10,00.</small>
          </div>
          <script>
          window.szAffRequestWithdraw=function(btn){
            var amount=(document.getElementById('sz-aff-withdraw-amount')||{}).value||'';
            if(btn){btn.disabled=true;btn.textContent='Enviando…';}
            fetch(window.location.origin+'/wp-json/sz-aff/v1/saque',{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json'},body:JSON.stringify({amount:amount})})
              .then(function(r){return r.json();}).then(function(d){
                var ok=!!(d.ok||d.success); var msg=d.msg||d.message||(ok?'Solicitação enviada.':'Erro ao solicitar saque.');
                if(window.szToast) szToast(msg,ok?'success':'error',4000); else alert(msg);
                if(ok) setTimeout(function(){(window.szReloadSameSection?window.szReloadSameSection():location.reload());},900);
              }).catch(function(){ if(window.szToast) szToast('Erro de conexão.','error',4000); else alert('Erro de conexão.'); })
              .finally(function(){ if(btn){btn.disabled=false;btn.textContent='Solicitar saque';} });
            return false;
          };
          </script>
        </div>
    </div>
    <?php
    return ob_get_clean();
}

// ── 1. REST endpoint: solicitar saque// ── 1. REST endpoint: solicitar saque ─────────────────────────────────────
add_action( 'rest_api_init', function() {
    register_rest_route( 'sz-aff/v1', '/saque', [
        'methods'             => 'POST',
        'callback'            => 'sz_aff_rest_solicitar_saque',
        'permission_callback' => '__return_true',
    ] );
    register_rest_route( 'sz-aff/v1', '/saque/aprovar', [
        'methods'             => 'POST',
        'callback'            => 'sz_aff_rest_aprovar_saque',
        'permission_callback' => function() { return current_user_can('manage_options'); },
    ] );
} );

function sz_aff_rest_solicitar_saque( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;

    $n     = sanitize_text_field( (string) $req->get_param('n') ?: '' );
    if ( $n === '' && ! empty( $_COOKIE['senderzz_portal_session'] ) ) {
        $n = sanitize_text_field( wp_unslash( $_COOKIE['senderzz_portal_session'] ) );
    }
    $user  = $n ? sz_aff_get_portal_user_by_token( $n ) : null;
    if ( ! $user ) return new WP_REST_Response( [ 'ok' => false, 'msg' => 'Sessão inválida.' ], 401 );

    // Segurança: apenas afiliados podem solicitar saque, não produtores usando este endpoint
    if ( ! empty( $user->role ) && in_array( $user->role, [ 'producer', 'admin', 'operator' ], true ) ) {
        return new WP_REST_Response( [ 'ok' => false, 'msg' => 'Use a carteira de produtor para saques.' ], 403 );
    }

    // Rate limiting: máximo 5 tentativas por hora por usuário
    $rate_key = 'sz_aff_saque_rl_' . (int) ( $user->id ?? 0 );
    $attempts = (int) get_transient( $rate_key );
    if ( $attempts >= 5 ) {
        return new WP_REST_Response( [ 'ok' => false, 'msg' => 'Muitas tentativas. Aguarde 1 hora antes de tentar novamente.' ], 429 );
    }
    set_transient( $rate_key, $attempts + 1, HOUR_IN_SECONDS );

    $aff_ids = sz_aff_get_affiliate_ids_for_portal_user( $user, 'active' );
    if ( empty( $aff_ids ) ) return new WP_REST_Response( [ 'ok' => false, 'msg' => 'Nenhum vínculo ativo.' ], 400 );

    foreach ( $aff_ids as $aid ) sz_aff_ensure_wallet( (int) $aid );
    $ids_sql  = implode( ',', array_map( 'absint', $aff_ids ) );

    // Checar dívida: inadimplentes não podem sacar
    $total_debt = (float) $wpdb->get_var( "SELECT SUM(debt_amount) FROM " . sz_aff_table('sz_affiliate_wallet') . " WHERE affiliate_id IN ({$ids_sql})" );
    if ( $total_debt > 0 ) {
        return new WP_REST_Response( [ 'ok' => false, 'msg' => 'Existe uma dívida pendente de R$ ' . number_format( $total_debt, 2, ',', '.' ) . '. Regularize para solicitar saques.' ], 400 );
    }

    $balance  = round( (float) $wpdb->get_var( "SELECT SUM(balance) FROM " . sz_aff_table('sz_affiliate_wallet') . " WHERE affiliate_id IN ({$ids_sql})" ), 2 );
    $requested = (float) str_replace( ',', '.', sanitize_text_field( (string) $req->get_param( 'amount' ) ) );
    if ( $requested <= 0 ) $requested = $balance;
    $requested = round( $requested, 2 );
    if ( $requested > $balance ) return new WP_REST_Response( [ 'ok' => false, 'msg' => 'Valor indisponível para saque.' ], 400 );
    // Taxa configurada pelo produtor do vínculo ativo
    $aff_row_fee = $wpdb->get_row( $wpdb->prepare(
        "SELECT producer_id FROM " . sz_aff_table('sz_affiliates') . " WHERE id=%d LIMIT 1",
        (int)$aff_ids[0]
    ), ARRAY_A );
    $producer_id_fee = $aff_row_fee ? (int)$aff_row_fee['producer_id'] : 0;
    $fee      = $producer_id_fee ? sz_aff_producer_withdraw_fee($producer_id_fee) : (float) get_option( 'sz_aff_default_withdraw_fee', 2.99 );
    $net      = round( $requested - $fee, 2 );

    if ( $balance <= 0 )  return new WP_REST_Response( [ 'ok' => false, 'msg' => 'Saldo disponível insuficiente.' ], 400 );
    if ( $net <= 0 )      return new WP_REST_Response( [ 'ok' => false, 'msg' => 'Saldo insuficiente após taxa de R$ ' . number_format($fee,2,',','.') . '.' ], 400 );
    // Saldo mínimo para saque: R$ 10,00
    if ( $requested < 10 )  return new WP_REST_Response( [ 'ok' => false, 'msg' => 'Valor mínimo para saque é R$ 10,00.' ], 400 );

    // Verificar se tem PIX/conta aprovada
    $aff_id   = (int) $aff_ids[0];
    $pix_ok   = (bool) $wpdb->get_var( $wpdb->prepare(
        "SELECT COUNT(*) FROM " . sz_aff_table('sz_affiliates') . " WHERE id=%d AND pix_status='approved'", $aff_id
    ) );
    if ( ! $pix_ok ) return new WP_REST_Response( [ 'ok' => false, 'msg' => 'Chave PIX/conta bancária ainda não aprovada pelo Admin. Aguarde.' ], 400 );

    // Verificar saque pendente em aberto
    $pending_w = (int) $wpdb->get_var( "SELECT COUNT(*) FROM " . sz_aff_table('sz_affiliate_withdrawals') . " WHERE affiliate_id IN ({$ids_sql}) AND status='pending'" );
    if ( $pending_w > 0 ) return new WP_REST_Response( [ 'ok' => false, 'msg' => 'Já existe um saque pendente aguardando aprovação.' ], 400 );

    // Limpar rate limit em caso de sucesso
    delete_transient( $rate_key );

    // Inserir solicitação
    $ok = $wpdb->insert( sz_aff_table('sz_affiliate_withdrawals'), [
        'affiliate_id' => $aff_id,
        'amount'       => $requested,
        'fee'          => $fee,
        'net_amount'   => $net,
        'status'       => 'pending',
        'created_at'   => sz_aff_now(),
    ], [ '%d', '%f', '%f', '%f', '%s', '%s' ] );

    if ( ! $ok ) return new WP_REST_Response( [ 'ok' => false, 'msg' => 'Erro ao registrar solicitação.' ], 500 );

    return new WP_REST_Response( [ 'ok' => true, 'msg' => 'Solicitação enviada! O Admin aprovará em breve.', 'net' => sz_aff_money($net) ], 200 );
}

function sz_aff_rest_aprovar_saque( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $id     = absint( $req->get_param('id') );
    $action = sanitize_text_field( (string) $req->get_param('action') ?: 'approve' );
    $note   = sanitize_text_field( (string) $req->get_param('note') ?: '' );
    if ( ! $id ) return new WP_REST_Response( [ 'ok' => false ], 400 );

    $w = $wpdb->get_row( $wpdb->prepare( "SELECT * FROM " . sz_aff_table('sz_affiliate_withdrawals') . " WHERE id=%d", $id ), ARRAY_A );
    if ( ! $w || $w['status'] !== 'pending' ) return new WP_REST_Response( [ 'ok' => false, 'msg' => 'Saque não encontrado ou já processado.' ], 404 );

    $now   = sz_aff_now();
    $admin = get_current_user_id();

    if ( $action === 'approve' ) {
        $amount = (float) $w['amount'];
        // Debitar saldo do afiliado
        $wpdb->query( $wpdb->prepare(
            "UPDATE " . sz_aff_table('sz_affiliate_wallet') . " SET balance=balance-%f, updated_at=%s WHERE affiliate_id=%d",
            $amount, $now, (int) $w['affiliate_id']
        ) );
        $wpdb->update( sz_aff_table('sz_affiliate_withdrawals'), [ 'status' => 'approved', 'decided_at' => $now, 'decided_by' => $admin, 'note' => $note ], [ 'id' => $id ], [ '%s','%s','%d','%s' ], [ '%d' ] );
        // Registrar na transação
        $wpdb->insert( sz_aff_table('sz_affiliate_transactions'), [
            'affiliate_id' => (int) $w['affiliate_id'], 'type' => 'withdrawal', 'amount' => -$amount,
            'status' => 'available', 'available_at' => $now, 'created_at' => $now,
        ], [ '%d','%s','%f','%s','%s','%s' ] );
    } else {
        $wpdb->update( sz_aff_table('sz_affiliate_withdrawals'), [ 'status' => 'rejected', 'decided_at' => $now, 'decided_by' => $admin, 'note' => $note ], [ 'id' => $id ], [ '%s','%s','%d','%s' ], [ '%d' ] );
    }
    return new WP_REST_Response( [ 'ok' => true ], 200 );
}

// sz_aff_release_pending removida — sz_aff_release_available_commissions cobre o mesmo fluxo (com transação DB)

// ── 3. Helper: buscar portal user por token (compatibilidade) ─────────────
if ( ! function_exists( 'sz_aff_get_portal_user_by_token' ) ) {
    function sz_aff_get_portal_user_by_token( string $n ) {
        global $wpdb;
        if ( ! $n ) return null;

        $n = sanitize_text_field( $n );
        $hash = class_exists( '\\WC_MelhorEnvio\\Portal\\Portal_Auth' )
            ? \WC_MelhorEnvio\Portal\Portal_Auth::hash_session_token( $n )
            : hash( 'sha256', $n );

        $sess_table = $wpdb->prefix . 'senderzz_portal_sessions';
        $user_table = $wpdb->prefix . 'senderzz_portal_users';
        $cols = $wpdb->get_col( "DESC {$sess_table}", 0 );
        $cols = is_array( $cols ) ? $cols : [];

        $token_col = in_array( 'token', $cols, true ) ? 'token' : ( in_array( 'session_token', $cols, true ) ? 'session_token' : '' );
        if ( ! $token_col ) return null;

        $row = $wpdb->get_row( $wpdb->prepare(
            "SELECT u.* FROM {$sess_table} s
             INNER JOIN {$user_table} u ON u.id = s.user_id
             WHERE s.`{$token_col}` IN (%s, %s) AND s.expires_at > %s AND u.status='active' LIMIT 1",
            $n, $hash, current_time('mysql')
        ) );
        return $row ?: null;
    }
}

// ── 4. Adicionar coluna pix_status na tabela affiliates (migration segura) ──
add_action( 'init', function() {
    global $wpdb;
    $table = sz_aff_table('sz_affiliates');
    $col   = $wpdb->get_results( "SHOW COLUMNS FROM `{$table}` LIKE 'pix_status'" );
    if ( empty( $col ) ) {
        $wpdb->query( "ALTER TABLE `{$table}` ADD COLUMN pix_status VARCHAR(20) NOT NULL DEFAULT 'pending' AFTER status" );
        $wpdb->query( "ALTER TABLE `{$table}` ADD COLUMN pix_key VARCHAR(255) NULL AFTER pix_status" );
        $wpdb->query( "ALTER TABLE `{$table}` ADD COLUMN bank_info TEXT NULL AFTER pix_key" );
    }
}, 99 );

// ── 6. Render pedidos do afiliado (somente leitura, com comissão e previsão) ─
function sz_aff_render_affiliate_orders( $portal_user, array $order_ids ): string {
    if ( empty( $order_ids ) ) {
        return '<div class="sz-card" style="padding:24px"><p style="color:#64748b;margin:0">Nenhum pedido encontrado com o seu link identificado.</p></div>';
    }
    $aff_ids = function_exists('sz_aff_get_affiliate_ids_for_portal_user') ? sz_aff_get_affiliate_ids_for_portal_user($portal_user,'active') : [];
    global $wpdb;
    $ids_sql = implode(',', array_map('absint', $aff_ids ?: [0]));

    // Pré-carregar comissões dos pedidos
    $commissions = [];
    $available_ats = [];
    if ( $aff_ids ) {
        $order_ids_sql = implode(',', array_map('absint', $order_ids));
        $txs = $wpdb->get_results( "SELECT order_id, amount, available_at FROM " . sz_aff_table('sz_affiliate_transactions') . " WHERE affiliate_id IN ({$ids_sql}) AND order_id IN ({$order_ids_sql}) AND type='commission'", ARRAY_A ) ?: [];
        foreach ( $txs as $tx ) {
            $commissions[(int)$tx['order_id']]   = (float) $tx['amount'];
            $available_ats[(int)$tx['order_id']] = (string) $tx['available_at'];
        }
    }

    $status_labels = [
        'pending'       => 'Pendente',
        'processing'    => 'Processando',
        'on-hold'       => 'Aguardando',
        'completed'     => 'Concluído',
        'cancelled'     => 'Cancelado',
        'refunded'      => 'Reembolsado',
        'failed'        => 'Falhou',
        'emcancelamento'=> 'Cancelando',
        'embalado'      => 'Embalado',
        'aprovado'      => 'Aprovado',
        'saldoinsuficiente' => 'Saldo Insuf.',
        'frustracao'    => 'Frustrado',
    ];


    $product_class_commission_rows = [];

    ob_start(); ?>
    <div class="sz-aff-orders-panel">
        <style>
            .sz-aff-orders-panel{display:grid;gap:0}
            .sz-aff-ord-hd,.sz-aff-ord-row{display:grid;grid-template-columns:80px 1fr 1fr 100px 90px 90px 100px;gap:8px;align-items:center;padding:10px 14px}
            .sz-aff-ord-hd{font-size:var(--sz-text-xs);font-weight:700;color:#6b7280;text-transform:none;letter-spacing:0;border-bottom:2px solid #e5e7eb;margin-bottom:4px}
            .sz-aff-ord-row{border:1px solid var(--sz-border,#e5e7eb);border-radius:12px;background:var(--sz-surface,#fff);margin-bottom:6px;font-size:var(--sz-text-meta)}
            .sz-aff-ord-row:hover{background:var(--sz-surface-soft,#f8fafc)}
            .sz-aff-ord-id{font-weight:700;color:#E8650A}
            .sz-aff-ord-status{display:inline-block;padding:2px 8px;border-radius:20px;font-size:var(--sz-text-xs);font-weight:700;background:#f1f5f9;color:#475569}
            .sz-aff-ord-status.completed{background:#fff7ed;color:#9a3412}
            .sz-aff-ord-status.cancelled,.sz-aff-ord-status.failed,.sz-aff-ord-status.frustracao{background:#fee2e2;color:#991b1b}
            .sz-aff-ord-comm{font-weight:700;color:#E8650A}
            .sz-aff-ord-nocomm{color:#9ca3af;font-size:var(--sz-text-sm)}
            @media(max-width:900px){.sz-aff-ord-hd{display:none}.sz-aff-ord-row{grid-template-columns:1fr 1fr;row-gap:4px}}
        </style>
        <div class="sz-aff-ord-hd">
            <span>#Pedido</span><span>Produto</span><span>Cliente · Tel</span><span>Status</span><span>Valor</span><span>Comissão</span><span>Liberação</span>
        </div>
        <?php
        $shown = 0;
        foreach ( array_slice($order_ids, 0, 100) as $oid ) :
            $order = wc_get_order( $oid );
            if ( ! $order ) continue;
            $shown++;
            $items_text = implode(', ', array_filter( array_map( fn($i) => function_exists('senderzz_order_item_label') ? senderzz_order_item_label($i, true) : ( function_exists('senderzz_clean_product_label') ? senderzz_clean_product_label($i->get_name()) : $i->get_name() ), $order->get_items() ) ) );
            $phone      = $order->get_billing_phone();
            $fname      = $order->get_billing_first_name() . ' ' . $order->get_billing_last_name();
            $status     = $order->get_status();
            $status_lbl = $status_labels[$status] ?? ucfirst($status);
            $total      = (float) $order->get_total();
            $comm       = $commissions[$oid] ?? null;
            $avail      = $available_ats[$oid] ?? null;
            $status_cls = in_array($status,['completed','aprovado']) ? 'completed' : (in_array($status,['cancelled','failed','frustracao']) ? 'cancelled' : '');
        ?>
        <div class="sz-aff-ord-row">
            <span class="sz-aff-ord-id">#<?php echo esc_html($oid); ?></span>
            <span style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="<?php echo esc_attr($items_text); ?>"><?php echo esc_html( mb_strimwidth($items_text, 0, 40, '…') ); ?></span>
            <span><?php echo esc_html( trim($fname) ); ?><br><span style="color:#64748b"><?php echo esc_html($phone); ?></span></span>
            <span class="sz-aff-ord-status <?php echo esc_attr($status_cls); ?>"><?php echo esc_html($status_lbl); ?></span>
            <span style="font-weight:700"><?php echo esc_html( 'R$ ' . number_format($total,2,',','.') ); ?></span>
            <span>
                <?php if ( $comm !== null ) : ?>
                    <span class="sz-aff-ord-comm"><?php echo esc_html( sz_aff_money($comm) ); ?></span>
                <?php else : ?>
                    <span class="sz-aff-ord-nocomm">—</span>
                <?php endif; ?>
            </span>
            <span style="font-size:var(--sz-text-sm);color:#64748b">
                <?php echo $avail ? esc_html( mysql2date('d/m/Y', $avail) ) : '—'; ?>
            </span>
        </div>
        <?php endforeach; ?>
        <?php if ( ! $shown ) : ?>
        <div style="padding:20px;color:#64748b">Nenhum pedido WooCommerce encontrado para os IDs registrados.</div>
        <?php endif; ?>
    </div>
    <?php return ob_get_clean();
}

// ═══════════════════════════════════════════════════════════════════════════
// v136 — SESSÃO 3: PIX, Admin de saques, Admin Senderzz
// ═══════════════════════════════════════════════════════════════════════════

// ── REST: salvar PIX do afiliado ──────────────────────────────────────────
add_action( 'rest_api_init', function() {
    register_rest_route( 'sz-aff/v1', '/pix', [
        'methods'             => 'POST',
        'callback'            => 'sz_aff_rest_save_pix',
        'permission_callback' => '__return_true',
    ] );
    register_rest_route( 'sz-aff/v1', '/pix/aprovar', [
        'methods'             => 'POST',
        'callback'            => 'sz_aff_rest_aprovar_pix',
        'permission_callback' => function() { return current_user_can('manage_options'); },
    ] );
} );

function sz_aff_rest_save_pix( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $n        = sanitize_text_field( (string) $req->get_param('n') );
    $user     = $n ? sz_aff_get_portal_user_by_token( $n ) : null;
    if ( ! $user ) return new WP_REST_Response( [ 'ok' => false, 'msg' => 'Sessão inválida.' ], 401 );

    $aff_id   = absint( $req->get_param('aff_id') );
    $pix_key  = sanitize_text_field( (string) $req->get_param('pix_key') );
    $bank_info= sanitize_text_field( (string) $req->get_param('bank_info') );

    if ( ! $pix_key ) return new WP_REST_Response( [ 'ok' => false, 'msg' => 'Chave PIX obrigatória.' ], 400 );

    // Validar que o aff_id pertence ao usuário
    $aff_ids = sz_aff_get_affiliate_ids_for_portal_user( $user, 'active' );
    if ( $aff_id && ! in_array( $aff_id, $aff_ids, false ) ) {
        return new WP_REST_Response( [ 'ok' => false, 'msg' => 'Acesso negado.' ], 403 );
    }
    $target_id = $aff_id ?: ( $aff_ids[0] ?? 0 );
    if ( ! $target_id ) return new WP_REST_Response( [ 'ok' => false, 'msg' => 'Nenhum vínculo ativo.' ], 400 );

    $wpdb->update(
        sz_aff_table('sz_affiliates'),
        [ 'pix_key' => $pix_key, 'bank_info' => $bank_info, 'pix_status' => 'pending' ],
        [ 'id' => $target_id ],
        [ '%s','%s','%s' ], [ '%d' ]
    );
    return new WP_REST_Response( [ 'ok' => true, 'msg' => 'Dados salvos! Aguarde aprovação do Admin para habilitar saques.' ], 200 );
}

function sz_aff_rest_aprovar_pix( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $aff_id = absint( $req->get_param('aff_id') );
    $action = sanitize_text_field( (string) $req->get_param('action') ); // approve | reject
    if ( ! $aff_id ) return new WP_REST_Response( [ 'ok' => false ], 400 );

    $status = $action === 'approve' ? 'approved' : 'rejected';
    $wpdb->update( sz_aff_table('sz_affiliates'), [ 'pix_status' => $status ], [ 'id' => $aff_id ], [ '%s' ], [ '%d' ] );
    return new WP_REST_Response( [ 'ok' => true, 'status' => $status ], 200 );
}

// ── Admin Senderzz aprimorado: saques + PIX + carteira ───────────────────
function sz_aff_render_admin_page_v2(): void {
    global $wpdb;

    // Processar ações POST
    if ( isset($_POST['sz_aff_admin_nonce']) && wp_verify_nonce( sanitize_text_field(wp_unslash($_POST['sz_aff_admin_nonce'])), 'sz_aff_admin' ) ) {
        update_option('sz_aff_default_commission_pct',   max(0,min(100,(float)str_replace(',','.',$_POST['commission'] ?? 10))));
        update_option('sz_aff_default_withdraw_fee',     max(0,(float)str_replace(',','.',$_POST['withdraw_fee'] ?? 2.99)));
        update_option('sz_aff_first_frustration_penalty',max(0,(float)str_replace(',','.',$_POST['penalty_first'] ?? 0)));
        update_option('sz_aff_default_penalty_value',    max(0,(float)str_replace(',','.',$_POST['penalty'] ?? 0)));
        update_option('sz_aff_default_retention_days',   max(7,absint($_POST['retention'] ?? 30)));
        echo '<div class="notice notice-success"><p>Configurações salvas.</p></div>';
    }

    // Aprovar/rejeitar PIX
    if ( isset($_POST['sz_pix_nonce']) && wp_verify_nonce( sanitize_text_field(wp_unslash($_POST['sz_pix_nonce'])), 'sz_pix_action' ) ) {
        $pix_aff_id = absint($_POST['pix_aff_id'] ?? 0);
        $pix_action = sanitize_text_field($_POST['pix_action'] ?? '');
        if ( $pix_aff_id && in_array($pix_action,['approve','reject'],true) ) {
            $wpdb->update( sz_aff_table('sz_affiliates'), [ 'pix_status' => $pix_action === 'approve' ? 'approved' : 'rejected' ], [ 'id' => $pix_aff_id ], ['%s'], ['%d'] );
            echo '<div class="notice notice-success"><p>PIX ' . ($pix_action==='approve'?'aprovado':'rejeitado') . '.</p></div>';
        }
    }

    // Aprovar/rejeitar saque
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
                echo '<div class="notice notice-success"><p>Saque '.($saque_act==='approve'?'aprovado':'rejeitado').'.</p></div>';
            }
        }
    }

    $wallet   = $wpdb->get_row("SELECT * FROM ".sz_aff_table('sz_admin_wallet')." WHERE id=1", ARRAY_A) ?: ['balance'=>0,'pending_balance'=>0];
    $pending_w= $wpdb->get_results("SELECT w.*,a.pix_key,a.bank_info,u.display_name,u.user_email FROM ".sz_aff_table('sz_affiliate_withdrawals')." w LEFT JOIN ".sz_aff_table('sz_affiliates')." a ON a.id=w.affiliate_id LEFT JOIN {$wpdb->users} u ON u.ID=a.user_id WHERE w.status='pending' ORDER BY w.created_at ASC LIMIT 50", ARRAY_A) ?: [];
    $pending_pix=$wpdb->get_results("SELECT a.id,a.pix_key,a.bank_info,a.pix_status,u.display_name,u.user_email FROM ".sz_aff_table('sz_affiliates')." a LEFT JOIN {$wpdb->users} u ON u.ID=a.user_id WHERE a.pix_status='pending' AND a.pix_key IS NOT NULL AND a.pix_key != '' ORDER BY a.id DESC LIMIT 50", ARRAY_A) ?: [];
    $all_aff  = $wpdb->get_results("SELECT a.*,w.balance,w.pending_balance,u.display_name,u.user_email FROM ".sz_aff_table('sz_affiliates')." a LEFT JOIN ".sz_aff_table('sz_affiliate_wallet')." w ON w.affiliate_id=a.id LEFT JOIN {$wpdb->users} u ON u.ID=a.user_id WHERE a.deleted_at IS NULL ORDER BY a.id DESC LIMIT 100", ARRAY_A) ?: [];
    $sz = 'style="padding:8px 12px"';
    ?>
    <div class="wrap">
    <h1>📊 Senderzz — Afiliados &amp; Financeiro</h1>

    <!-- CARTEIRA ADMIN -->
    <div style="display:flex;gap:16px;flex-wrap:wrap;margin:16px 0">
        <div style="background:#fff7ed;border:1px solid #fed7aa;border-radius:12px;padding:16px 22px;min-width:180px">
            <div style="font-size:var(--sz-text-sm);font-weight:700;color:#9a3412;text-transform:none;letter-spacing:0">Carteira Admin</div>
            <div style="font-size:var(--sz-text-3xl);font-weight:700;color:#c2410c;margin-top:4px"><?php echo esc_html(sz_aff_money((float)($wallet['balance']??0))); ?></div>
            <div style="font-size:var(--sz-text-sm);color:#b45309">Taxas acumuladas de pedidos</div>
        </div>
        <div style="background:#f0f9ff;border:1px solid #bae6fd;border-radius:12px;padding:16px 22px;min-width:180px">
            <div style="font-size:var(--sz-text-sm);font-weight:700;color:#0369a1;text-transform:none;letter-spacing:0">Saques Pendentes</div>
            <div style="font-size:var(--sz-text-3xl);font-weight:700;color:#0284c7;margin-top:4px"><?php echo count($pending_w); ?></div>
            <div style="font-size:var(--sz-text-sm);color:#0369a1">Aguardando aprovação</div>
        </div>
        <div style="background:#fefce8;border:1px solid #fde047;border-radius:12px;padding:16px 22px;min-width:180px">
            <div style="font-size:var(--sz-text-sm);font-weight:700;color:#854d0e;text-transform:none;letter-spacing:0">PIX Pendentes</div>
            <div style="font-size:var(--sz-text-3xl);font-weight:700;color:#ca8a04;margin-top:4px"><?php echo count($pending_pix); ?></div>
            <div style="font-size:var(--sz-text-sm);color:#854d0e">Aguardando aprovação</div>
        </div>
    </div>

    <div style="display:grid;grid-template-columns:1fr 1fr;gap:24px;margin-top:8px">

    <!-- SAQUES PENDENTES -->
    <div>
    <h2>💰 Saques pendentes</h2>
    <?php if(empty($pending_w)): ?><p style="color:#64748b">Nenhum saque pendente.</p>
    <?php else: foreach($pending_w as $p): ?>
    <div style="background:#fff;border:1px solid #e5e7eb;border-radius:12px;padding:14px 16px;margin-bottom:10px">
        <div style="font-weight:700"><?php echo esc_html($p['display_name'] ?: $p['user_email']); ?></div>
        <div style="font-size:var(--sz-text-base);color:#64748b;margin:4px 0">
            Valor: <strong><?php echo esc_html(sz_aff_money((float)$p['amount'])); ?></strong> ·
            Taxa: <?php echo esc_html(sz_aff_money((float)$p['fee'])); ?> ·
            Líquido: <strong style="color:#E8650A"><?php echo esc_html(sz_aff_money((float)$p['net_amount'])); ?></strong>
        </div>
        <?php if($p['pix_key']): ?>
        <div style="font-size:var(--sz-text-meta);background:#f8fafc;border-radius:8px;padding:6px 10px;margin:6px 0">
            PIX: <strong><?php echo esc_html($p['pix_key']); ?></strong>
            <?php if($p['bank_info']): ?> · <?php echo esc_html($p['bank_info']); ?><?php endif; ?>
        </div>
        <?php endif; ?>
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

    <!-- PIX PENDENTES -->
    <div>
    <h2>🔑 PIX / Contas pendentes</h2>
    <?php if(empty($pending_pix)): ?><p style="color:#64748b">Nenhuma aprovação pendente.</p>
    <?php else: foreach($pending_pix as $p): ?>
    <div style="background:#fff;border:1px solid #e5e7eb;border-radius:12px;padding:14px 16px;margin-bottom:10px">
        <div style="font-weight:700"><?php echo esc_html($p['display_name'] ?: $p['user_email']); ?></div>
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

    <!-- TODOS OS AFILIADOS -->
    <?php
    if ( isset($_POST['sz_afc_nonce']) && wp_verify_nonce(sanitize_text_field(wp_unslash($_POST['sz_afc_nonce'])),'sz_afc_classes') ) {
        $afc_aff_id  = absint($_POST['sz_afc_aff_id'] ?? 0);
        $afc_classes = array_map('absint', (array)($_POST['sz_afc_classes'] ?? []));
        if ($afc_aff_id) {
            $afc_table = $wpdb->prefix . 'sz_affiliate_classes';
            $wpdb->query("CREATE TABLE IF NOT EXISTS {$afc_table} (id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT, affiliate_id BIGINT UNSIGNED NOT NULL, shipping_class_id BIGINT UNSIGNED NOT NULL, PRIMARY KEY (id), UNIQUE KEY uk (affiliate_id,shipping_class_id)) " . $wpdb->get_charset_collate());
            $wpdb->delete($afc_table, ['affiliate_id'=>$afc_aff_id], ['%d']);
            foreach ($afc_classes as $cid) { if ($cid > 0) $wpdb->insert($afc_table, ['affiliate_id'=>$afc_aff_id,'shipping_class_id'=>$cid], ['%d','%d']); }
            echo '<div class="notice notice-success is-dismissible"><p>Classes atualizadas.</p></div>';
        }
    }
    $afc_tbl = $wpdb->prefix . 'sz_affiliate_classes';
    $afc_map = [];
    if ($wpdb->get_var($wpdb->prepare('SHOW TABLES LIKE %s',$afc_tbl)) === $afc_tbl && !empty($all_aff)) {
        $afc_ids = implode(',', array_map('intval', array_column($all_aff,'id')));
        foreach ($wpdb->get_results("SELECT affiliate_id,shipping_class_id FROM {$afc_tbl} WHERE affiliate_id IN ({$afc_ids})", ARRAY_A) ?: [] as $r) $afc_map[(int)$r['affiliate_id']][] = (int)$r['shipping_class_id'];
    }
    $wc_classes = [];
    $wc_terms = get_terms(['taxonomy'=>'product_shipping_class','hide_empty'=>false]);
    if (!is_wp_error($wc_terms)) foreach ($wc_terms as $t) $wc_classes[(int)$t->term_id] = $t->name;
    $afc_edit_id = absint($_GET['sz_afc_edit'] ?? 0);
    ?>
    <h2 style="margin-top:24px">👥 Todos os afiliados</h2>
    <table class="widefat striped">
        <thead><tr><th>Afiliado</th><th>PIX</th><th>Status PIX</th><th>Saldo</th><th>A liberar</th><th>Comissão%</th><th>Status</th><th>Classes de entrega</th></tr></thead>
        <tbody>
        <?php if(empty($all_aff)) echo '<tr><td colspan="8">Nenhum afiliado cadastrado.</td></tr>';
        foreach($all_aff as $a):
            $pix_lbl=['pending'=>'⏳ Pendente','approved'=>'✅ Aprovado','rejected'=>'❌ Recusado'][$a['pix_status']??'pending']??'—';
            $afc_classes = $afc_map[(int)$a['id']] ?? [];
            $afc_labels = empty($afc_classes) ? '<em style="color:#999">Nenhuma</em>' : implode(', ', array_map(fn($c) => esc_html($wc_classes[$c] ?? 'Classe #'.$c), $afc_classes));
            $is_editing = ($afc_edit_id === (int)$a['id']);
        ?>
        <tr>
            <td><?php echo esc_html($a['display_name']??$a['user_email']??'#'.$a['id']); ?></td>
            <td><?php echo $a['pix_key'] ? esc_html(substr($a['pix_key'],0,20).'…') : '—'; ?></td>
            <td><?php echo $pix_lbl; ?></td>
            <td><?php echo esc_html(sz_aff_money((float)($a['balance']??0))); ?></td>
            <td><?php echo esc_html(sz_aff_money((float)($a['pending_balance']??0))); ?></td>
            <td><?php echo esc_html(number_format((float)($a['commission_pct']??0),1,',','.')); ?>%</td>
            <td><?php echo esc_html($a['status']??'—'); ?></td>
            <td>
                <?php if (!$is_editing): ?>
                    <?php echo $afc_labels; ?>
                    &nbsp;<a href="<?php echo esc_url(add_query_arg(['sz_afc_edit'=>(int)$a['id']])); ?>" class="button button-small">✏️ Editar</a>
                <?php else: ?>
                    <form method="post" style="display:inline-block">
                        <?php wp_nonce_field('sz_afc_classes','sz_afc_nonce'); ?>
                        <input type="hidden" name="sz_afc_aff_id" value="<?php echo (int)$a['id']; ?>">
                        <?php foreach($wc_classes as $cid => $clabel): ?>
                            <label style="display:inline-block;margin:0 8px 4px 0">
                                <input type="checkbox" name="sz_afc_classes[]" value="<?php echo $cid; ?>" <?php checked(in_array($cid,$afc_classes)); ?>>
                                <?php echo esc_html($clabel); ?>
                            </label>
                        <?php endforeach; ?>
                        <?php if(empty($wc_classes)): ?><em style="color:#999">Nenhuma classe WC.</em><?php endif; ?>
                        <br><button class="button button-primary" style="margin-top:6px">💾 Salvar</button>
                        <a href="<?php echo esc_url(remove_query_arg('sz_afc_edit')); ?>" class="button" style="margin-top:6px">Cancelar</a>
                    </form>
                <?php endif; ?>
            </td>
        </tr>
        <?php endforeach; ?>
        </tbody>
    </table>

    <!-- CONFIGURAÇÕES GLOBAIS -->
    <h2 style="margin-top:24px">⚙️ Configurações globais</h2>
    <form method="post" style="background:#fff;border:1px solid #ccd0d4;border-radius:8px;padding:18px;max-width:600px">
        <?php wp_nonce_field('sz_aff_admin','sz_aff_admin_nonce'); ?>
        <div style="display:grid;grid-template-columns:1fr 1fr;gap:14px">
            <label>Comissão padrão (%)<br><input name="commission" class="regular-text" value="<?php echo esc_attr(sz_aff_default_commission_pct()); ?>"></label>
            <label>Taxa saque afiliado (R$)<br><input name="withdraw_fee" class="regular-text" value="<?php echo esc_attr(sz_aff_default_withdraw_fee()); ?>"></label>
            <label>Penalidade 1ª frustração (R$)<br><input name="penalty_first" class="regular-text" value="<?php echo esc_attr(sz_aff_first_frustration_penalty()); ?>"></label>
            <label>Penalidade reincidente (R$)<br><input name="penalty" class="regular-text" value="<?php echo esc_attr(sz_aff_default_penalty_value()); ?>"></label>
            <label>Retenção mínima (dias)<br><input name="retention" class="regular-text" value="<?php echo esc_attr(sz_aff_default_retention_days()); ?>"></label>
        </div>
        <p><button class="button button-primary" style="margin-top:8px">Salvar configurações</button></p>
    </form>
    </div>
    <?php
}
// Substituir a função original
remove_action('admin_menu','sz_aff_admin_menu',70);
// Senderzz v221: submenu legado removido da origem.
// add_action('admin_menu', function() {
//     add_submenu_page('woocommerce','Senderzz Afiliados','Senderzz Afiliados','manage_woocommerce','senderzz-affiliates','sz_aff_render_admin_page_v2');
// }, 70);

// ═══════════════════════════════════════════════════════════════════════════
// v136 — SESSÃO 4: Dashboard Admin consolidado + Relatório afiliados
// ═══════════════════════════════════════════════════════════════════════════
// Senderzz v221: dashboard legado removido da origem; fica no menu Senderzz.
// add_action('admin_menu', function() {
//     add_submenu_page('woocommerce','Senderzz Dashboard','📊 Dashboard Senderzz','manage_woocommerce','senderzz-dashboard','sz_aff_render_dashboard_admin');
// }, 68);

function sz_aff_render_dashboard_admin(): void {
    global $wpdb;
    $period = sanitize_text_field($_GET['period'] ?? '30');
    $period_i = max(1, (int) $period);
    $since  = gmdate('Y-m-d H:i:s', current_time('timestamp') - $period_i * DAY_IN_SECONDS);

    $tx_table = sz_aff_table('sz_affiliate_transactions');
    $wal_table = sz_aff_table('sz_affiliate_wallet');
    $aff_table = sz_aff_table('sz_affiliates');
    $wd_table  = sz_aff_table('sz_affiliate_withdrawals');

    // KPIs financeiros — inclui pedidos agendados (com afiliado mas sem split ainda).
    $total_orders_split = (int) $wpdb->get_var($wpdb->prepare("SELECT COUNT(DISTINCT order_id) FROM {$tx_table} WHERE created_at >= %s AND type='commission'", $since));

    // Pedidos agendados com afiliado (ainda não entregues, sem split)
    $total_orders_agendado = (int) $wpdb->get_var( $wpdb->prepare(
        "SELECT COUNT(DISTINCT o.id)
         FROM {$wpdb->prefix}wc_orders o
         INNER JOIN {$wpdb->prefix}wc_orders_meta m ON m.order_id = o.id AND m.meta_key = '_sz_affiliate_id'
         LEFT  JOIN {$wpdb->prefix}wc_orders_meta ms ON ms.order_id = o.id AND ms.meta_key = '_sz_aff_split_done'
         WHERE o.date_created_gmt >= %s
           AND o.status IN ('wc-agendado','agendado','wc-processing','processing','wc-on-hold','on-hold')
           AND (ms.meta_value IS NULL OR ms.meta_value != '1')",
        $since
    ) );
    $total_orders = $total_orders_split + $total_orders_agendado;

    // Comissão estimada dos pedidos agendados (baseada em _sz_aff_commission gravada no checkout)
    $commission_agendado = (float) ( $wpdb->get_var( $wpdb->prepare(
        "SELECT SUM(CAST(mc.meta_value AS DECIMAL(10,2)))
         FROM {$wpdb->prefix}wc_orders o
         INNER JOIN {$wpdb->prefix}wc_orders_meta ma ON ma.order_id = o.id AND ma.meta_key = '_sz_affiliate_id'
         INNER JOIN {$wpdb->prefix}wc_orders_meta mc ON mc.order_id = o.id AND mc.meta_key = '_sz_aff_commission'
         LEFT  JOIN {$wpdb->prefix}wc_orders_meta ms ON ms.order_id = o.id AND ms.meta_key = '_sz_aff_split_done'
         WHERE o.date_created_gmt >= %s
           AND o.status IN ('wc-agendado','agendado','wc-processing','processing','wc-on-hold','on-hold')
           AND (ms.meta_value IS NULL OR ms.meta_value != '1')",
        $since
    ) ) ?: 0 );

    $commission_pending = (float) ($wpdb->get_var($wpdb->prepare("SELECT SUM(amount) FROM {$tx_table} WHERE created_at >= %s AND type='commission' AND status='pending'", $since)) ?: 0);
    $commission_pending += $commission_agendado; // inclui estimativa agendados
    $commission_available = (float) ($wpdb->get_var($wpdb->prepare("SELECT SUM(amount) FROM {$tx_table} WHERE created_at >= %s AND type='commission' AND status IN ('available','approved','paid')", $since)) ?: 0);
    $commission_total = (float) ($wpdb->get_var($wpdb->prepare("SELECT SUM(amount) FROM {$tx_table} WHERE created_at >= %s AND type='commission'", $since)) ?: 0);

    // Receita Senderzz deve seguir a mesma regra da tela Taxas COD: 1 pedido = 1 _sz_mb_taxa_total real.
    // A subquery agrupa metadados por pedido para impedir multiplicação por joins/metas duplicadas.
    $total_fees = (float) ( $wpdb->get_var( $wpdb->prepare(
        "SELECT SUM(x.taxa_total) FROM (
            SELECT o.id AS order_id,
                   MAX(CASE WHEN m.meta_key = '_sz_mb_taxa_total' THEN CAST(m.meta_value AS DECIMAL(10,2)) ELSE 0 END) AS taxa_total
            FROM {$wpdb->prefix}wc_orders o
            INNER JOIN {$wpdb->prefix}wc_orders_meta m ON m.order_id = o.id
            WHERE o.date_created_gmt >= %s
              AND m.meta_key = '_sz_mb_taxa_total'
            GROUP BY o.id
        ) x
        WHERE x.taxa_total > 0",
        $since
    ) ) ?: 0 );

    $tax_breakdown = $wpdb->get_row( $wpdb->prepare(
        "SELECT
            SUM(x.entrega) AS entrega,
            SUM(x.manuseio) AS manuseio,
            SUM(x.transacao) AS transacao,
            SUM(x.total) AS total
        FROM (
            SELECT o.id AS order_id,
                MAX(CASE WHEN m.meta_key = '_sz_mb_taxa_entrega' THEN CAST(m.meta_value AS DECIMAL(10,2)) ELSE 0 END) AS entrega,
                MAX(CASE WHEN m.meta_key = '_sz_mb_taxa_manuseio' THEN CAST(m.meta_value AS DECIMAL(10,2)) ELSE 0 END) AS manuseio,
                MAX(CASE WHEN m.meta_key IN ('_sz_mb_taxa_adicional','_sz_mb_taxa_transacao') THEN CAST(m.meta_value AS DECIMAL(10,2)) ELSE 0 END) AS transacao,
                MAX(CASE WHEN m.meta_key = '_sz_mb_taxa_total' THEN CAST(m.meta_value AS DECIMAL(10,2)) ELSE 0 END) AS total
            FROM {$wpdb->prefix}wc_orders o
            INNER JOIN {$wpdb->prefix}wc_orders_meta m ON m.order_id = o.id
            WHERE o.date_created_gmt >= %s
              AND m.meta_key IN ('_sz_mb_taxa_entrega','_sz_mb_taxa_manuseio','_sz_mb_taxa_adicional','_sz_mb_taxa_transacao','_sz_mb_taxa_total')
            GROUP BY o.id
        ) x",
        $since
    ), ARRAY_A ) ?: ['entrega'=>0,'manuseio'=>0,'transacao'=>0,'total'=>0];

    $total_pending_wallet = (float)($wpdb->get_var("SELECT SUM(pending_balance) FROM {$wal_table}") ?: 0);
    $total_available=(float)($wpdb->get_var("SELECT SUM(balance) FROM {$wal_table}") ?: 0);
    $active_affs   = (int) $wpdb->get_var("SELECT COUNT(*) FROM {$aff_table} WHERE status='active' AND deleted_at IS NULL");
    $pending_saques= (int) $wpdb->get_var("SELECT COUNT(*) FROM {$wd_table} WHERE status='pending'");

    // Top afiliados — combina split feito + pedidos agendados ainda sem split.
    $top_affs_split = $wpdb->get_results($wpdb->prepare("SELECT t.affiliate_id, COUNT(DISTINCT t.order_id) pedidos, SUM(t.amount) total_comissao, a.commission_pct, u.display_name, u.user_email FROM {$tx_table} t LEFT JOIN {$aff_table} a ON a.id=t.affiliate_id LEFT JOIN {$wpdb->users} u ON u.ID=a.user_id WHERE t.created_at>=%s AND t.type='commission' GROUP BY t.affiliate_id ORDER BY total_comissao DESC LIMIT 10",$since), ARRAY_A) ?: [];

    $top_affs_agendado = $wpdb->get_results( $wpdb->prepare(
        "SELECT
            CAST(ma.meta_value AS UNSIGNED) AS affiliate_id,
            COUNT(DISTINCT o.id) AS pedidos,
            SUM(CAST(mc.meta_value AS DECIMAL(10,2))) AS total_comissao,
            aff.commission_pct,
            u.display_name,
            u.user_email
         FROM {$wpdb->prefix}wc_orders o
         INNER JOIN {$wpdb->prefix}wc_orders_meta ma ON ma.order_id = o.id AND ma.meta_key = '_sz_affiliate_id'
         INNER JOIN {$wpdb->prefix}wc_orders_meta mc ON mc.order_id = o.id AND mc.meta_key = '_sz_aff_commission'
         LEFT  JOIN {$wpdb->prefix}wc_orders_meta ms ON ms.order_id = o.id AND ms.meta_key = '_sz_aff_split_done'
         LEFT  JOIN {$aff_table} aff ON aff.id = CAST(ma.meta_value AS UNSIGNED)
         LEFT  JOIN {$wpdb->users} u ON u.ID = aff.user_id
         WHERE o.date_created_gmt >= %s
           AND o.status IN ('wc-agendado','agendado','wc-processing','processing','wc-on-hold','on-hold')
           AND (ms.meta_value IS NULL OR ms.meta_value != '1')
         GROUP BY ma.meta_value",
        $since
    ), ARRAY_A ) ?: [];

    // Mescla: soma pedidos e comissão do mesmo afiliado
    $top_affs_map = [];
    foreach ( array_merge( $top_affs_split, $top_affs_agendado ) as $row ) {
        $id = (int) $row['affiliate_id'];
        if ( ! isset( $top_affs_map[$id] ) ) {
            $top_affs_map[$id] = $row;
            $top_affs_map[$id]['pedidos'] = 0;
            $top_affs_map[$id]['total_comissao'] = 0.0;
        }
        $top_affs_map[$id]['pedidos']       += (int)   $row['pedidos'];
        $top_affs_map[$id]['total_comissao'] += (float) $row['total_comissao'];
    }
    usort( $top_affs_map, fn($a,$b) => $b['total_comissao'] <=> $a['total_comissao'] );
    $top_affs = array_slice( array_values( $top_affs_map ), 0, 10 );

    // Ranking de produtores — comissão produtor = valor pedido - taxa total Senderzz - comissão afiliado.
    $top_producers = $wpdb->get_results( $wpdb->prepare(
        "SELECT
            NULLIF(x.produtor_nome,'') AS produtor_nome,
            COUNT(*) AS pedidos,
            SUM(x.valor_pedido) AS faturamento,
            SUM(x.taxa_total) AS taxas_senderzz,
            SUM(GREATEST(x.valor_pedido - x.taxa_total - x.comissao_afiliado, 0)) AS total_produtor
        FROM (
            SELECT
                o.id AS order_id,
                MAX(CASE WHEN m.meta_key = '_sz_aff_producer_name' THEN m.meta_value ELSE '' END) AS produtor_nome,
                GREATEST(MAX(CASE WHEN m.meta_key = '_sz_aff_gross' THEN CAST(m.meta_value AS DECIMAL(10,2)) ELSE 0 END),MAX(CASE WHEN m.meta_key = '_senderzz_offer_value' THEN CAST(m.meta_value AS DECIMAL(10,2)) ELSE 0 END)) AS valor_pedido,
                MAX(CASE WHEN m.meta_key = '_sz_mb_taxa_total' THEN CAST(m.meta_value AS DECIMAL(10,2)) ELSE 0 END) AS taxa_total,
                MAX(CASE WHEN m.meta_key = '_sz_aff_commission' THEN CAST(m.meta_value AS DECIMAL(10,2)) ELSE 0 END) AS comissao_afiliado
            FROM {$wpdb->prefix}wc_orders o
            INNER JOIN {$wpdb->prefix}wc_orders_meta m ON m.order_id = o.id
            WHERE o.date_created_gmt >= %s
              AND m.meta_key IN ('_sz_aff_producer_name','_sz_aff_gross','_senderzz_offer_value','_sz_mb_taxa_total','_sz_aff_commission')
            GROUP BY o.id
        ) x
        WHERE x.produtor_nome IS NOT NULL AND x.produtor_nome <> ''
        GROUP BY x.produtor_nome
        ORDER BY total_produtor DESC
        LIMIT 10",
        $since
    ), ARRAY_A ) ?: [];

    $producer_obligation = 0.0;
    foreach ($top_producers as $p) { $producer_obligation += (float) ($p['total_produtor'] ?? 0); }
    $total_obligations = $commission_pending + $producer_obligation;

    // Últimas transações — split feito + agendados pendentes.
    $recent_txs_split = $wpdb->get_results($wpdb->prepare("SELECT t.*,u.display_name FROM {$tx_table} t LEFT JOIN {$aff_table} a ON a.id=t.affiliate_id LEFT JOIN {$wpdb->users} u ON u.ID=a.user_id WHERE t.created_at>=%s ORDER BY t.id DESC LIMIT 20",$since), ARRAY_A) ?: [];

    $recent_txs_agendado = $wpdb->get_results( $wpdb->prepare(
        "SELECT
            CAST(ma.meta_value AS UNSIGNED) AS affiliate_id,
            o.id AS order_id,
            'commission' AS type,
            CAST(mc.meta_value AS DECIMAL(10,2)) AS amount,
            'agendado' AS status,
            NULL AS available_at,
            o.date_created_gmt AS created_at,
            NULL AS meta_json,
            u.display_name
         FROM {$wpdb->prefix}wc_orders o
         INNER JOIN {$wpdb->prefix}wc_orders_meta ma ON ma.order_id = o.id AND ma.meta_key = '_sz_affiliate_id'
         INNER JOIN {$wpdb->prefix}wc_orders_meta mc ON mc.order_id = o.id AND mc.meta_key = '_sz_aff_commission'
         LEFT  JOIN {$wpdb->prefix}wc_orders_meta ms ON ms.order_id = o.id AND ms.meta_key = '_sz_aff_split_done'
         LEFT  JOIN {$aff_table} aff ON aff.id = CAST(ma.meta_value AS UNSIGNED)
         LEFT  JOIN {$wpdb->users} u ON u.ID = aff.user_id
         WHERE o.date_created_gmt >= %s
           AND o.status IN ('wc-agendado','agendado','wc-processing','processing','wc-on-hold','on-hold')
           AND (ms.meta_value IS NULL OR ms.meta_value != '1')
         ORDER BY o.id DESC LIMIT 20",
        $since
    ), ARRAY_A ) ?: [];

    // Mescla e ordena por data desc, limita a 20
    $recent_txs_all = array_merge( $recent_txs_agendado, $recent_txs_split );
    usort( $recent_txs_all, fn($a,$b) => strcmp( $b['created_at'] ?? '', $a['created_at'] ?? '' ) );
    $recent_txs = array_slice( $recent_txs_all, 0, 20 );
    ?>
    <div class="wrap sz-fin-dashboard">
    <h1>📊 Dashboard financeiro COD</h1>
    <p style="margin:4px 0 16px;color:#64748b">Visão separada entre receita Senderzz, comissões, obrigações e saldo de carteira.</p>
    <form method="get" style="display:flex;gap:8px;align-items:center;margin-bottom:16px">
        <input type="hidden" name="page" value="<?php echo esc_attr($_GET['page'] ?? 'senderzz'); ?>">
        <?php if(isset($_GET['area'])): ?><input type="hidden" name="area" value="<?php echo esc_attr($_GET['area']); ?>"><?php endif; ?>
        <?php if(isset($_GET['tab'])): ?><input type="hidden" name="tab" value="<?php echo esc_attr($_GET['tab']); ?>"><?php endif; ?>
        <label>Período:
        <select name="period" onchange="this.form.submit()" style="margin-left:6px">
            <?php foreach([7=>'7 dias',15=>'15 dias',30=>'30 dias',60=>'60 dias',90=>'90 dias'] as $v=>$l): ?>
            <option value="<?php echo $v; ?>" <?php selected($period_i,$v); ?>><?php echo $l; ?></option>
            <?php endforeach; ?>
        </select></label>
    </form>

    <div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(185px,1fr));gap:12px;margin-bottom:18px">
        <?php foreach([
            ['🛒','Pedidos afiliados',$total_orders,'Pedidos COD com afiliado no período.'],
            ['💸','Comissão afiliados',sz_aff_money($commission_pending),'Pendente/em retenção para afiliados.'],
            ['🏦','Receita Senderzz',sz_aff_money($total_fees),'Taxas COD cobradas: entrega + manuseio + transação.'],
            ['📌','Obrigações pendentes',sz_aff_money($total_obligations),'Valores ainda devidos a afiliados/produtores.'],
            ['✅','Saldo carteira COD',sz_aff_money($total_available),'Saldo disponível nas carteiras de afiliados.'],
            ['👥','Afiliados aprovados',$active_affs,'Afiliados ativos e aprovados.'],
            ['📤','Solicitações saque',$pending_saques,'Pedidos de saque pendentes.'],
        ] as [$ico,$lbl,$val,$hint]): ?>
        <div title="<?php echo esc_attr($hint); ?>" style="background:#fff;border:1px solid #e5e7eb;border-radius:12px;padding:16px">
            <div style="font-size:var(--sz-text-xl)"><?php echo $ico; ?></div>
            <div style="font-size:var(--sz-text-sm);font-weight:700;color:#64748b;text-transform:none;letter-spacing:0;margin-top:4px"><?php echo esc_html($lbl); ?></div>
            <div style="font-size:var(--sz-text-xl);font-weight:700;color:#111827;margin-top:6px"><?php echo esc_html($val); ?></div>
            <div style="font-size:var(--sz-text-sm);color:#64748b;margin-top:6px;line-height:1.35"><?php echo esc_html($hint); ?></div>
        </div>
        <?php endforeach; ?>
    </div>

    <div style="display:grid;grid-template-columns:1fr 1fr;gap:16px;margin-bottom:24px">
        <div style="background:#fff;border:1px solid #e5e7eb;border-radius:12px;padding:16px">
            <h2 style="margin:0 0 10px;font-size:var(--sz-text-lg)">📥 Entradas COD — Senderzz</h2>
            <table class="widefat striped"><tbody>
                <tr><td>Taxa de entrega</td><td style="text-align:right;font-weight:700;color:#0b74b8"><?php echo esc_html(sz_aff_money((float)$tax_breakdown['entrega'])); ?></td></tr>
                <tr><td>Taxa de manuseio</td><td style="text-align:right;font-weight:700;color:#7c3aed"><?php echo esc_html(sz_aff_money((float)$tax_breakdown['manuseio'])); ?></td></tr>
                <tr><td>Taxa de transação</td><td style="text-align:right;font-weight:700;color:#059669"><?php echo esc_html(sz_aff_money((float)$tax_breakdown['transacao'])); ?></td></tr>
                <tr><td><strong>Total receita Senderzz</strong></td><td style="text-align:right;font-weight:700;color:#E8650A"><?php echo esc_html(sz_aff_money($total_fees)); ?></td></tr>
            </tbody></table>
        </div>
        <div style="background:#fff;border:1px solid #e5e7eb;border-radius:12px;padding:16px">
            <h2 style="margin:0 0 10px;font-size:var(--sz-text-lg)">📤 Obrigações e retenções</h2>
            <table class="widefat striped"><tbody>
                <tr><td>Comissão afiliados pendente</td><td style="text-align:right;font-weight:700;color:#E8650A"><?php echo esc_html(sz_aff_money($commission_pending)); ?></td></tr>
                <tr><td>Comissão afiliados liberada/paga</td><td style="text-align:right;font-weight:700"><?php echo esc_html(sz_aff_money($commission_available)); ?></td></tr>
                <tr><td>Repasse estimado produtores</td><td style="text-align:right;font-weight:700"><?php echo esc_html(sz_aff_money($producer_obligation)); ?></td></tr>
                <tr><td><strong>Total obrigações mapeadas</strong></td><td style="text-align:right;font-weight:700;color:#111827"><?php echo esc_html(sz_aff_money($total_obligations)); ?></td></tr>
            </tbody></table>
            <p style="margin:10px 0 0;color:#64748b;font-size:var(--sz-text-meta)">Afiliados puros seguem a retenção própria configurada. Overrides desta tela são de produtores.</p>
        </div>
    </div>

    <div style="display:grid;grid-template-columns:1fr 1fr;gap:24px;margin-bottom:24px">
    <div>
    <h2>🏆 Ranking afiliados — <?php echo esc_html($period_i); ?> dias</h2>
    <table class="widefat">
        <thead><tr><th>#</th><th>Afiliado</th><th>Pedidos</th><th>Comissão afiliado</th><th>%</th></tr></thead>
        <tbody>
        <?php if(empty($top_affs)) echo '<tr><td colspan="5" style="color:#64748b">Sem dados no período.</td></tr>';
        foreach($top_affs as $i=>$a): ?>
        <tr>
            <td style="font-weight:700;color:#E8650A"><?php echo $i+1; ?></td>
            <td><?php echo esc_html($a['display_name'] ?: $a['user_email'] ?: '#'.$a['affiliate_id']); ?></td>
            <td><?php echo (int)$a['pedidos']; ?></td>
            <td><strong><?php echo esc_html(sz_aff_money((float)$a['total_comissao'])); ?></strong></td>
            <td><?php echo esc_html(number_format((float)($a['commission_pct']??0),1,',','.')); ?>%</td>
        </tr>
        <?php endforeach; ?>
        </tbody>
    </table>
    </div>

    <div>
    <h2>🏅 Ranking produtores — <?php echo esc_html($period_i); ?> dias</h2>
    <table class="widefat">
        <thead><tr><th>#</th><th>Produtor</th><th>Pedidos</th><th>Repasse produtor</th><th>Taxas COD</th></tr></thead>
        <tbody>
        <?php if(empty($top_producers)) echo '<tr><td colspan="5" style="color:#64748b">Sem dados no período.</td></tr>';
        foreach($top_producers as $i=>$p): ?>
        <tr>
            <td style="font-weight:700;color:#E8650A"><?php echo $i+1; ?></td>
            <td><?php echo esc_html($p['produtor_nome'] ?: '—'); ?></td>
            <td><?php echo (int)$p['pedidos']; ?></td>
            <td><strong><?php echo esc_html(sz_aff_money((float)$p['total_produtor'])); ?></strong></td>
            <td style="color:#E8650A;font-weight:700"><?php echo esc_html(sz_aff_money((float)$p['taxas_senderzz'])); ?></td>
        </tr>
        <?php endforeach; ?>
        </tbody>
    </table>
    </div>
    </div>

    <div>
    <h2>📋 Últimas transações</h2>
    <table class="widefat">
        <thead><tr><th>Tipo</th><th>Favorecido</th><th>Pedido</th><th>Valor</th><th>Status</th><th>Data</th></tr></thead>
        <tbody>
        <?php if(empty($recent_txs)) echo '<tr><td colspan="6" style="color:#64748b">Sem transações no período.</td></tr>';
        foreach($recent_txs as $tx):
            $type_lbl = ['commission'=>'Comissão afiliado','withdrawal'=>'Saque','penalty'=>'Penalidade','adjustment'=>'Ajuste'][$tx['type']]??$tx['type'];
            $status_lbl=['pending'=>'Pendente','available'=>'Disponível','approved'=>'Aprovado','paid'=>'Pago','rejected'=>'Cancelado'][$tx['status']]??$tx['status'];
        ?>
        <tr>
            <td><?php echo esc_html($type_lbl); ?></td>
            <td><?php echo esc_html($tx['display_name']??'#'.$tx['affiliate_id']); ?></td>
            <td><?php echo $tx['order_id'] ? '#'.(int)$tx['order_id'] : '—'; ?></td>
            <td style="font-weight:700;color:<?php echo (float)$tx['amount']>=0?'#E8650A':'#b91c1c'; ?>"><?php echo esc_html(sz_aff_money(abs((float)$tx['amount']))); ?></td>
            <td><?php echo esc_html($status_lbl); ?></td>
            <td style="font-size:var(--sz-text-sm);color:#64748b"><?php echo esc_html(mysql2date('d/m H:i',$tx['created_at'])); ?></td>
        </tr>
        <?php endforeach; ?>
        </tbody>
    </table>
    </div>
    </div>
    <?php
}

// ── Relatório do afiliado no portal (seção Relatórios) ────────────────────
function sz_aff_render_affiliate_reports( $portal_user ): string {
    global $wpdb;
    $aff_ids = function_exists( 'sz_aff_get_affiliate_ids_for_portal_user' ) ? sz_aff_get_affiliate_ids_for_portal_user( $portal_user, 'active' ) : [];
    $aff_ids = array_values( array_unique( array_filter( array_map( 'absint', (array) $aff_ids ) ) ) );
    if ( empty( $aff_ids ) ) return '<div class="sz-section-pad"><div class="sz-card" style="padding:22px;border-radius:20px"><div class="sz-kicker">RELATÓRIOS</div><h2 style="margin:6px 0 8px">Desempenho</h2><p style="color:#64748b;margin:0">Nenhum vínculo ativo para relatório.</p></div></div>';
    // Limitar a 500 IDs para evitar queries pesadas com IN() muito grande
    $aff_ids = array_slice( $aff_ids, 0, 500 );
    $ids_sql = implode( ',', $aff_ids );
    $tx_table  = sz_aff_table('sz_affiliate_transactions');
    $wal_table = sz_aff_table('sz_affiliate_wallet');
    $total_comm   = (float) ( $wpdb->get_var( "SELECT SUM(amount) FROM {$tx_table} WHERE affiliate_id IN ({$ids_sql}) AND type='commission'" ) ?: 0 );
    $total_orders = (int)   ( $wpdb->get_var( "SELECT COUNT(DISTINCT order_id) FROM {$tx_table} WHERE affiliate_id IN ({$ids_sql}) AND type='commission'" ) ?: 0 );
    $available    = (float) ( $wpdb->get_var( "SELECT SUM(balance) FROM {$wal_table} WHERE affiliate_id IN ({$ids_sql})" ) ?: 0 );
    $pending      = (float) ( $wpdb->get_var( "SELECT SUM(pending_balance) FROM {$wal_table} WHERE affiliate_id IN ({$ids_sql})" ) ?: 0 );

    $product_class_commission_rows = [];

    ob_start(); ?><div class="sz-section-pad sz-aff-safe-panel"><div class="sz-aff-safe-hero" style="background:var(--sz-surface,#fff);border:1px solid var(--sz-border,#e5e7eb);border-radius:22px;padding:22px"><div class="sz-kicker">RELATÓRIOS</div><h1 style="margin:6px 0 4px;font-size:var(--sz-text-xl);font-weight:700">Desempenho do afiliado</h1><p style="color:#64748b;margin:0 0 16px;font-size:var(--sz-text-md)">Resumo dos pedidos e comissões gerados pelos seus links.</p><div class="sz-aff-safe-grid" style="display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:12px"><div class="sz-aff-safe-kpi"><small>Comissões</small><strong><?php echo esc_html( sz_aff_money( $total_comm ) ); ?></strong></div><div class="sz-aff-safe-kpi"><small>Pedidos</small><strong><?php echo esc_html( (string) $total_orders ); ?></strong></div><div class="sz-aff-safe-kpi"><small>Disponível</small><strong style="color:#E8650A"><?php echo esc_html( sz_aff_money( $available ) ); ?></strong></div><div class="sz-aff-safe-kpi"><small>Pendente</small><strong><?php echo esc_html( sz_aff_money( $pending ) ); ?></strong></div></div></div></div><?php return ob_get_clean();
}

// ─── Resumo de comissões pagas pelo produtor// ─── Resumo de comissões pagas pelo produtor (aparece na Carteira do produtor) ─
function sz_aff_render_producer_commissions_summary( $portal_user ): string {
    global $wpdb;

    $producer_uid = function_exists('sz_aff_portal_user_wp_id')
        ? sz_aff_portal_user_wp_id($portal_user)
        : (int)($portal_user->wp_user_id ?? 0);
    if ( ! $producer_uid ) return '';

    // Totais pagos a afiliados deste produtor
    $total_comm  = (float)($wpdb->get_var($wpdb->prepare(
        "SELECT SUM(t.amount) FROM ".sz_aff_table('sz_affiliate_transactions')." t
         INNER JOIN ".sz_aff_table('sz_affiliates')." a ON a.id=t.affiliate_id
         WHERE a.producer_id=%d AND t.type='commission' AND t.status IN ('available','pending')",
        $producer_uid
    )) ?: 0);
    $total_pending = (float)($wpdb->get_var($wpdb->prepare(
        "SELECT SUM(t.amount) FROM ".sz_aff_table('sz_affiliate_transactions')." t
         INNER JOIN ".sz_aff_table('sz_affiliates')." a ON a.id=t.affiliate_id
         WHERE a.producer_id=%d AND t.type='commission' AND t.status='pending'",
        $producer_uid
    )) ?: 0);
    $total_released = (float)($wpdb->get_var($wpdb->prepare(
        "SELECT SUM(t.amount) FROM ".sz_aff_table('sz_affiliate_transactions')." t
         INNER JOIN ".sz_aff_table('sz_affiliates')." a ON a.id=t.affiliate_id
         WHERE a.producer_id=%d AND t.type='commission' AND t.status='available'",
        $producer_uid
    )) ?: 0);
    $total_orders = (int)($wpdb->get_var($wpdb->prepare(
        "SELECT COUNT(DISTINCT t.order_id) FROM ".sz_aff_table('sz_affiliate_transactions')." t
         INNER JOIN ".sz_aff_table('sz_affiliates')." a ON a.id=t.affiliate_id
         WHERE a.producer_id=%d AND t.type='commission'",
        $producer_uid
    )) ?: 0);

    // Últimas comissões dos afiliados deste produtor
    $recent = $wpdb->get_results($wpdb->prepare(
        "SELECT t.*, u.display_name aff_name FROM ".sz_aff_table('sz_affiliate_transactions')." t
         INNER JOIN ".sz_aff_table('sz_affiliates')." a ON a.id=t.affiliate_id
         LEFT JOIN {$wpdb->users} u ON u.ID=a.user_id
         WHERE a.producer_id=%d AND t.type='commission'
         ORDER BY t.id DESC LIMIT 10",
        $producer_uid
    ), ARRAY_A) ?: [];

    if ( ! $total_comm && empty($recent) ) return '';


    $product_class_commission_rows = [];

    ob_start(); ?>
    <div style="margin-top:16px">
        <div style="background:var(--sz-surface,#fff);border:1px solid var(--sz-border,#e5e7eb);border-radius:20px;padding:20px">
            <div style="display:flex;align-items:center;gap:10px;margin-bottom:14px">
                <div style="width:38px;height:38px;border-radius:13px;background:#fff1e8;color:#E8650A;display:flex;align-items:center;justify-content:center;font-size:var(--sz-text-lg);flex-shrink:0">🤝</div>
                <div>
                    <div style="font-weight:700;font-size:var(--sz-text-lg);color:var(--sz-text,#111)">Comissões de afiliados</div>
                    <div style="font-size:var(--sz-text-meta);color:var(--sz-text-3,#94a3b8)">Separado do saldo de expedição · <?php echo $total_orders; ?> pedido(s)</div>
                </div>
            </div>
            <div style="display:grid;grid-template-columns:repeat(3,1fr);gap:10px;margin-bottom:14px">
                <div style="background:var(--sz-surface-soft,#f8fafc);border:1px solid var(--sz-border,#e5e7eb);border-radius:14px;padding:14px">
                    <div style="font-size:var(--sz-text-xs);font-weight:700;color:var(--sz-text-3,#94a3b8);text-transform:none;letter-spacing:0">Total gerado</div>
                    <div style="font-size:var(--sz-text-lg);font-weight:700;color:var(--sz-text,#111);margin-top:6px"><?php echo esc_html(sz_aff_money($total_comm)); ?></div>
                </div>
                <div style="background:rgba(232,101,10,.07);border:1px solid rgba(232,101,10,.2);border-radius:14px;padding:14px">
                    <div style="font-size:var(--sz-text-xs);font-weight:700;color:#9a3412;text-transform:none;letter-spacing:0">Liberado</div>
                    <div style="font-size:var(--sz-text-lg);font-weight:700;color:#E8650A;margin-top:6px"><?php echo esc_html(sz_aff_money($total_released)); ?></div>
                </div>
                <div style="background:var(--sz-surface-soft,#f8fafc);border:1px solid var(--sz-border,#e5e7eb);border-radius:14px;padding:14px">
                    <div style="font-size:var(--sz-text-xs);font-weight:700;color:var(--sz-text-3,#94a3b8);text-transform:none;letter-spacing:0">Em retenção</div>
                    <div style="font-size:var(--sz-text-lg);font-weight:700;color:var(--sz-text,#111);margin-top:6px"><?php echo esc_html(sz_aff_money($total_pending)); ?></div>
                </div>
            </div>
            <?php if ( ! empty($recent) ) : ?>
            <div style="font-size:var(--sz-text-sm);font-weight:700;color:var(--sz-text-3,#94a3b8);text-transform:none;letter-spacing:0;margin-bottom:8px">Últimas comissões</div>
            <?php foreach ($recent as $tx) :
                $is_avail = $tx['status'] === 'available';
                $libera   = ! $is_avail && $tx['available_at'] ? ' · libera '.mysql2date('d/m/Y',$tx['available_at']) : '';
            ?>
            <div style="display:flex;justify-content:space-between;align-items:center;padding:9px 12px;border:1px solid var(--sz-border,#e5e7eb);border-radius:12px;background:var(--sz-surface,#fff);margin-bottom:6px;<?php echo $is_avail?'border-left:3px solid #E8650A':''; ?>">
                <div>
                    <div style="font-size:var(--sz-text-base);font-weight:700;color:var(--sz-text,#111)"><?php echo esc_html($tx['aff_name'] ?: 'Afiliado'); ?><?php echo $tx['order_id'] ? ' · #'.esc_html($tx['order_id']) : ''; ?></div>
                    <div style="font-size:var(--sz-text-sm);color:var(--sz-text-3,#94a3b8)"><?php echo esc_html(mysql2date('d/m/Y H:i',$tx['created_at'])); ?><?php echo esc_html($libera); ?></div>
                </div>
                <div style="font-weight:700;color:#E8650A;font-size:var(--sz-text-md)"><?php echo esc_html(sz_aff_money((float)$tx['amount'])); ?></div>
            </div>
            <?php endforeach; ?>
            <?php endif; ?>
        </div>
    </div>
    <?php return ob_get_clean();
}

// ═══════════════════════════════════════════════════════════════════════════
// v140 — Barra de marcos COD (produtor e afiliado)
// ═══════════════════════════════════════════════════════════════════════════

// Tiers de faturamento COD
function sz_aff_cod_tiers(): array {
    return [
        [ 'min' =>        0, 'max' =>   10000, 'slug' => 'estreante',   'label' => 'Desafiante',    'icon' => '🌱' ],
        [ 'min' =>    10000, 'max' =>   50000, 'slug' => 'promissor',   'label' => 'Ascendente',    'icon' => '🔥' ],
        [ 'min' =>    50000, 'max' =>  100000, 'slug' => 'consolidado', 'label' => 'Dominante',     'icon' => '⚡' ],
        [ 'min' =>   100000, 'max' => 1000000, 'slug' => 'elite',       'label' => 'Elite',         'icon' => '💎' ],
        [ 'min' =>  1000000, 'max' => PHP_INT_MAX, 'slug' => 'lenda',   'label' => 'Lenda',         'icon' => '👑' ],
    ];
}

// Retorna SVG badge para um tier slug
function sz_aff_tier_badge_svg( string $slug, int $size = 32 ): string {
    $s = (int) $size;
    $badges = [
        'estreante' => '<svg width="'.$s.'" height="'.$s.'" viewBox="0 0 32 32" fill="none" xmlns="http://www.w3.org/2000/svg"><circle cx="16" cy="16" r="16" fill="#dcfce7"/><path d="M16 8c0 0-1 3-4 5s-3 5-1 7 5 2 5 2 3 0 5-2 1-5-1-7-4-5-4-5z" fill="#16a34a"/><path d="M16 13v9M13 16h6" stroke="#fff" stroke-width="1.5" stroke-linecap="round"/></svg>',
        'promissor' => '<svg width="'.$s.'" height="'.$s.'" viewBox="0 0 32 32" fill="none" xmlns="http://www.w3.org/2000/svg"><circle cx="16" cy="16" r="16" fill="#fff7ed"/><path d="M16 7c-.5 2.5-2 4-4 5 1 1 2.5 1.5 3 3 .5-1.5 2-2.5 3-3-2-1-2.5-2.5-2-5z" fill="#f97316"/><path d="M16 13c-.3 1.5-1.2 2.4-2.4 3 .6.6 1.5.9 1.8 1.8.3-.9 1.2-1.5 1.8-1.8-1.2-.6-1.5-1.5-1.2-3z" fill="#E8650A"/><ellipse cx="16" cy="22" rx="3" ry="2" fill="#fed7aa"/></svg>',
        'consolidado' => '<svg width="'.$s.'" height="'.$s.'" viewBox="0 0 32 32" fill="none" xmlns="http://www.w3.org/2000/svg"><circle cx="16" cy="16" r="16" fill="#fef9c3"/><polygon points="16,8 18.5,13.5 24,14 20,18 21,23.5 16,21 11,23.5 12,18 8,14 13.5,13.5" fill="#eab308" stroke="#ca8a04" stroke-width=".8"/></svg>',
        'elite'      => '<svg width="'.$s.'" height="'.$s.'" viewBox="0 0 32 32" fill="none" xmlns="http://www.w3.org/2000/svg"><circle cx="16" cy="16" r="16" fill="#ede9fe"/><polygon points="16,8 18,13 24,13 19.5,17 21,23 16,19.5 11,23 12.5,17 8,13 14,13" fill="#7c3aed" opacity=".2"/><path d="M16 10l1.8 5H23l-4.4 3.2 1.7 5.2L16 20.2l-4.3 3.2 1.7-5.2L9 15h5.2z" fill="#7c3aed"/><path d="M16 12l1.2 3.5H21l-3 2.2 1.1 3.5L16 19l-3.1 2.2 1.1-3.5-3-2.2h3.8z" fill="#c4b5fd"/></svg>',
        'lenda'      => '<svg width="'.$s.'" height="'.$s.'" viewBox="0 0 32 32" fill="none" xmlns="http://www.w3.org/2000/svg"><circle cx="16" cy="16" r="16" fill="#fef3c7"/><path d="M9 13h14l-1.5 9H10.5z" fill="#d97706"/><path d="M12 13V11a4 4 0 0 1 8 0v2" stroke="#d97706" stroke-width="2" stroke-linecap="round"/><circle cx="16" cy="11" r="2" fill="#fbbf24"/><path d="M11 16h10M11 19h10" stroke="#fff" stroke-width="1.2" stroke-linecap="round" opacity=".7"/></svg>',
    ];
    return $badges[$slug] ?? '<span style="font-size:'.(int)($s*.65).'px">'.(sz_aff_cod_tiers()[0]['icon']).'</span>';
}

// Faturamento COD acumulado do produtor
function sz_aff_producer_cod_total( int $wp_user_id ): float {
    global $wpdb;
    // Todos os pedidos com _sz_aff_gross (split executado), sem restrição de status terminal
    // pois pedidos entregues podem ter outros status intermediários.
    $terminal = "'wc-completed','wc-entregue','wc-delivered','wc-complete','completed','entregue','delivered'";
    $total = (float) $wpdb->get_var( $wpdb->prepare(
        "SELECT SUM(pm_gross.meta_value)
         FROM {$wpdb->posts} p
         INNER JOIN {$wpdb->postmeta} pm_gross  ON pm_gross.post_id=p.ID  AND pm_gross.meta_key='_sz_aff_gross'
         LEFT  JOIN {$wpdb->postmeta} pm_prod   ON pm_prod.post_id=p.ID   AND pm_prod.meta_key='_sz_aff_producer_id'
         WHERE p.post_type='shop_order'
           AND p.post_status IN ({$terminal})
           AND ( pm_prod.meta_value=%d OR p.post_author=%d )",
        $wp_user_id, $wp_user_id
    ) );
    // HPOS fallback
    if ( $total <= 0 ) {
        $total = (float) $wpdb->get_var( $wpdb->prepare(
            "SELECT SUM(om_gross.meta_value)
             FROM {$wpdb->prefix}wc_orders o
             INNER JOIN {$wpdb->prefix}wc_orders_meta om_gross  ON om_gross.order_id=o.id  AND om_gross.meta_key='_sz_aff_gross'
             LEFT  JOIN {$wpdb->prefix}wc_orders_meta om_prod   ON om_prod.order_id=o.id   AND om_prod.meta_key='_sz_aff_producer_id'
             WHERE o.status IN ({$terminal})
               AND ( om_prod.meta_value=%d OR o.customer_id=%d )",
            $wp_user_id, $wp_user_id
        ) );
    }
    // Fallback adicional: somar transações da carteira COD do produtor
    if ( $total <= 0 && function_exists( 'sz_cod_tables' ) ) {
        $t = sz_cod_tables();
        $total = (float) $wpdb->get_var( $wpdb->prepare(
            "SELECT COALESCE(SUM(gross),0) FROM {$t['tx']} WHERE user_id=%d AND status IN ('available','pending','paid')",
            $wp_user_id
        ) );
    }
    return round( max( 0, $total ), 2 );
}

// Faturamento COD acumulado do afiliado
function sz_aff_affiliate_cod_total( int $wp_user_id ): float {
    global $wpdb;
    $total = (float) $wpdb->get_var( $wpdb->prepare(
        "SELECT SUM(a.total_sales) FROM ".sz_aff_table('sz_affiliates')." a WHERE a.user_id=%d AND a.deleted_at IS NULL",
        $wp_user_id
    ) );
    return round( max(0, $total), 2 );
}

// Renderizar barra de marcos COD
function sz_aff_render_cod_milestone( float $total, string $context = 'producer', string $variant = 'default' ): string {
    $tiers = sz_aff_cod_tiers();

    // Identificar tier atual e próximo
    $current_tier = $tiers[0];
    $next_tier    = $tiers[1] ?? null;
    foreach ( $tiers as $i => $tier ) {
        if ( $total >= $tier['min'] ) {
            $current_tier = $tier;
            $next_tier    = $tiers[$i+1] ?? null;
        }
    }

    // Progresso dentro do tier atual
    if ( $next_tier ) {
        $range     = $next_tier['min'] - $current_tier['min'];
        $progress  = $range > 0 ? min(100, round( ($total - $current_tier['min']) / $range * 100, 1 )) : 100;
        $remaining = max(0, $next_tier['min'] - $total);
    } else {
        $progress  = 100;
        $remaining = 0;
    }

    $pct_label = rtrim(rtrim(number_format((float) $progress, 1, ',', '.'), '0'), ',') . '%';
    $variant   = $variant === 'sidebar' ? 'sidebar' : 'default';
    $role      = $context === 'affiliate' ? 'Afiliado' : 'Produtor';

    $fmt = fn(float $v) => $v >= 1000000
        ? 'R$ '.number_format($v/1000000,1,',','.').'M'
        : ( $v >= 1000 ? 'R$ '.number_format($v/1000,0,',','.').'k' : sz_aff_money($v) );


    $product_class_commission_rows = [];

    ob_start(); ?>
    <div class="sz-cod-milestone sz-cod-milestone--<?php echo esc_attr($variant); ?>">
        <style>
/* Senderzz v266 — bloco limpo: conquistas + afiliados sem overrides empilhados */
.sz-cod-milestone{background:var(--sz-surface,#fff);border:1px solid var(--sz-border,#e5e7eb);border-radius:18px;padding:14px 16px;margin-top:10px;overflow:hidden;box-shadow:none}
.sz-cod-ms-head{display:flex;align-items:center;gap:10px;margin-bottom:10px}.sz-cod-ms-icon{width:32px;height:32px;border-radius:12px;background:rgba(232,101,10,.10);color:#E8650A;display:flex;align-items:center;justify-content:center;flex-shrink:0}.sz-cod-ms-title{font-size:var(--sz-text-base);font-weight:700;color:var(--sz-text,#111);line-height:1.2}.sz-cod-ms-sub{font-size:var(--sz-text-sm);color:var(--sz-text-3,#94a3b8);margin-top:1px}.sz-cod-ms-tiers{display:flex;gap:5px;margin-bottom:10px;flex-wrap:wrap}.sz-cod-ms-tier{display:flex;align-items:center;gap:5px;padding:5px 10px;border-radius:999px;font-size:var(--sz-text-xs);font-weight:700;border:1px solid var(--sz-border,#e5e7eb);background:var(--sz-surface-soft,#f8fafc);color:var(--sz-text-2,#64748b);line-height:1.1}.sz-cod-ms-tier.done{background:rgba(232,101,10,.10);border-color:rgba(232,101,10,.22);color:#E8650A}.sz-cod-ms-tier.current{background:#E8650A;border-color:#E8650A;color:#fff;box-shadow:0 8px 18px rgba(232,101,10,.20)}.sz-cod-bar-meta{display:flex;align-items:center;justify-content:space-between;gap:10px;margin:2px 0 8px;font-size:var(--sz-text-sm)}.sz-cod-bar-pct,.sz-cod-bar-pct--max{font-size:var(--sz-text-sm);font-weight:700;color:#fff;background:#E8650A;border-radius:999px;padding:2px 9px;line-height:1.5;white-space:nowrap;display:inline-block}.sz-cod-bar-next{color:var(--sz-text-3,#94a3b8);min-width:0}.sz-cod-bar-next strong{color:var(--sz-text,#111);font-weight:700}.sz-cod-bar-wrap{position:relative;background:rgba(232,101,10,.12);border-radius:999px;height:7px;overflow:hidden}.sz-cod-bar-fill{height:100%;border-radius:999px;background:#E8650A}.sz-cod-bar-scale{display:flex;justify-content:space-between;gap:8px;font-size:var(--sz-text-xs);color:var(--sz-text-3,#94a3b8);margin-top:6px}.sz-cod-total{font-size:var(--sz-text-meta);font-weight:700;color:var(--sz-text-2,#64748b);margin-top:8px}.sz-cod-total strong{color:var(--sz-text,#111)}.sz-cod-milestone--sidebar{display:block;padding:0;border-radius:0;margin-top:6px;background:transparent;border:0;box-shadow:none}.sz-cod-milestone--sidebar .sz-cod-ms-head{margin-bottom:6px}.sz-cod-milestone--sidebar .sz-cod-ms-icon{width:26px;height:26px;border-radius:50%;font-size:var(--sz-text-base)}.sz-cod-milestone--sidebar .sz-cod-ms-tiers{display:none}.sz-cod-milestone--sidebar .sz-cod-bar-meta{margin:0 0 6px;font-size:var(--sz-text-xs)}.sz-cod-milestone--sidebar .sz-cod-bar-wrap{height:5px}.sz-cod-milestone--sidebar .sz-cod-bar-scale{font-size:var(--sz-text-xs);margin-top:5px}.sz-cod-milestone--sidebar .sz-cod-total{font-size:var(--sz-text-sm);margin-top:7px;text-align:left}.sz-sb.sz-collapsed .sz-cod-milestone--sidebar{display:none}
.sz-root.sz-dark .sz-cod-milestone{background:var(--sz-surface,#111827);border-color:var(--sz-border,rgba(255,255,255,.12))}.sz-root.sz-dark .sz-cod-ms-tier{background:rgba(255,255,255,.04);border-color:rgba(255,255,255,.10);color:#94a3b8}.sz-root.sz-dark .sz-cod-ms-tier.done{background:rgba(232,101,10,.13);border-color:rgba(232,101,10,.28);color:#fb923c}.sz-root.sz-dark .sz-cod-bar-next strong{color:#fff}.sz-root.sz-dark .sz-cod-bar-wrap{background:rgba(255,255,255,.08)}
#sec-affiliates .sz-aff-panel-v2{color:var(--sz-text,#111827)}#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-card,#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-list-card{background:var(--sz-surface,#fff);border:1px solid var(--sz-border,#e5e7eb);box-shadow:none}#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-card-title,#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-list-title{color:var(--sz-text,#111827);font-weight:700}#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-card-sub,#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-list-sub,#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-toggle-lbl{color:var(--sz-text-2,#64748b)}#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-icon{background:rgba(232,101,10,.10);color:#E8650A}#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-invite-url-bar,#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-comm-wrap,#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-preview,#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-info-box.neutral{background:var(--sz-surface-soft,#f8fafc);border-color:var(--sz-border,#e5e7eb);color:var(--sz-text-2,#64748b)}#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-info-box.orange{background:rgba(232,101,10,.08);border-color:rgba(232,101,10,.24);color:var(--sz-text-2,#64748b)}#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-info-box.orange strong{color:#E8650A}#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-comm-input{color:var(--sz-text,#111827);background:transparent}#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-comm-suf{background:var(--sz-surface-soft,#f8fafc);border-color:var(--sz-border,#e5e7eb);color:var(--sz-text-3,#94a3b8)}#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-btn,#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-invite-btn{background:#E8650A;border-color:#E8650A;color:#fff;box-shadow:none}#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-btn:hover,#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-invite-btn:hover{background:#d85c08;border-color:#d85c08;color:#fff}#sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-toggle input:checked + .sz-aff-v2-toggle-slider{background:#E8650A}
.sz-root.sz-dark #sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-card,.sz-root.sz-dark #sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-list-card{background:var(--sz-surface,#111827);border-color:var(--sz-border,rgba(255,255,255,.12))}.sz-root.sz-dark #sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-invite-url-bar,.sz-root.sz-dark #sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-comm-wrap,.sz-root.sz-dark #sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-preview,.sz-root.sz-dark #sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-info-box.neutral{background:rgba(255,255,255,.03);border-color:rgba(255,255,255,.12);color:#cbd5e1}.sz-root.sz-dark #sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-info-box.orange{background:rgba(232,101,10,.10);border-color:rgba(232,101,10,.28);color:#fed7aa}.sz-root.sz-dark #sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-card-title{color:#fff}.sz-root.sz-dark #sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-card-sub,.sz-root.sz-dark #sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-toggle-lbl{color:#cbd5e1}.sz-root.sz-dark #sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-comm-input{color:#fff}.sz-root.sz-dark #sec-affiliates .sz-aff-panel-v2 .sz-aff-v2-comm-suf{background:rgba(255,255,255,.06);border-color:rgba(255,255,255,.12);color:#cbd5e1}
</style>

        <div class="sz-cod-ms-head">
            <div class="sz-cod-ms-icon"><?php echo sz_aff_tier_badge_svg( $current_tier['slug'], $variant === 'sidebar' ? 22 : 28 ); ?></div>
            <div>
                <div class="sz-cod-ms-title"><?php echo esc_html($current_tier['label']); ?></div>
            </div>
        </div>

        <?php if ( $variant !== 'sidebar' ) : ?>
        <div class="sz-cod-ms-tiers">
            <?php foreach ( $tiers as $tier ) :
                if ( $tier['max'] === PHP_INT_MAX && $total < $tier['min'] ) continue;
                if ( $tier['min'] > 1000000 ) continue;
                $cls = $total >= $tier['max'] ? 'done' : ( $tier['slug'] === $current_tier['slug'] ? 'current' : 'next' );
                $tier_label = $cls === 'done' ? '✓ '.$tier['label'] : $tier['icon'].' '.$tier['label'];
            ?>
            <span class="sz-cod-ms-tier <?php echo esc_attr($cls); ?>"><?php
                if ( $cls === 'done' ) {
                    echo '<svg width="10" height="10" viewBox="0 0 10 10" fill="none"><path d="M2 5l2.5 2.5L8 3" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg> '.esc_html($tier['label']);
                } else {
                    echo sz_aff_tier_badge_svg($tier['slug'], 14).' '.esc_html($tier['label']);
                }
            ?></span>
            <?php endforeach; ?>
        </div>
        <?php endif; ?>

        <div class="sz-cod-bar-meta">
            <?php if ( $next_tier ) : ?>
            <span class="sz-cod-bar-pct"><?php echo esc_html($pct_label); ?></span>
            <span class="sz-cod-bar-next"><?php echo esc_html($next_tier['label']); ?> →</span>
            <?php else : ?>
            <span class="sz-cod-bar-pct sz-cod-bar-pct--max">100%</span>
            <span class="sz-cod-bar-next"><strong>Nível máximo 👑</strong></span>
            <?php endif; ?>
        </div>

        <div class="sz-cod-bar-wrap" aria-hidden="true">
            <div class="sz-cod-bar-fill" style="width:<?php echo esc_attr((string) $progress); ?>%"></div>
        </div>
        <div class="sz-cod-bar-scale">
            <span><?php echo esc_html($fmt((float) $current_tier['min'])); ?></span>
            <span><?php echo esc_html($next_tier ? $fmt((float) $next_tier['min']) : 'Topo'); ?></span>
        </div>

    </div>
    <?php return ob_get_clean();
}

// ═══════════════════════════════════════════════════════════════════════════
// METABOX AFILIADO NO ADMIN DO WOOCOMMERCE
// ═══════════════════════════════════════════════════════════════════════════
add_action( 'add_meta_boxes', function() {
    foreach ( [ 'shop_order', 'woocommerce_page_wc-orders' ] as $screen ) {
        add_meta_box( 'sz_aff_order_meta', '📋 Resumo do pedido', 'sz_aff_render_order_metabox', $screen, 'side', 'high' );
    }
} );
function sz_aff_render_order_metabox( $post_or_order ): void {
    $order = $post_or_order instanceof WC_Order ? $post_or_order : wc_get_order( $post_or_order->ID ?? 0 );
    if ( ! $order instanceof WC_Order ) return;

    $fmt = fn( float $v ) => 'R$ ' . number_format( $v, 2, ',', '.' );
    $order_id = (int) $order->get_id();

    // Contexto centralizado do pedido. O antigo box "Raio-X Senderzz" foi removido;
    // as informações realmente úteis ficam aqui, em um único resumo operacional.
    $ctx = function_exists( 'senderzz_order_context' ) ? senderzz_order_context( $order_id ) : [];

    $aff_id = (int) $order->get_meta( '_sz_affiliate_id', true );
    if ( ! $aff_id && ! empty( $ctx['affiliate_id'] ) ) $aff_id = (int) $ctx['affiliate_id'];

    // Metas gravadas no pedido
    $aff_name     = (string) $order->get_meta( '_sz_aff_name', true );
    $aff_email    = (string) $order->get_meta( '_sz_aff_email', true );
    $aff_doc      = (string) $order->get_meta( '_sz_aff_document', true );
    $prod_name    = (string) $order->get_meta( '_sz_aff_producer_name', true );
    $producer_id  = (int) ( $order->get_meta( '_sz_aff_producer_id', true ) ?: ( $ctx['producer_id'] ?? 0 ) );
    $order_total  = (float)  $order->get_meta( '_sz_aff_order_total', true );
    $comm_pct     = (float)  $order->get_meta( '_sz_aff_commission_pct', true );
    $commission   = (float)  $order->get_meta( '_sz_aff_commission', true );

    // Fallback: se metas de display estão vazias, busca direto na tabela sz_affiliates
    if ( $aff_id && ( $aff_name === '' || $aff_email === '' ) ) {
        global $wpdb;
        $aff_row = $wpdb->get_row( $wpdb->prepare(
            "SELECT a.*, u.display_name, u.user_email FROM " . sz_aff_table('sz_affiliates') . " a
             LEFT JOIN {$wpdb->users} u ON u.ID = a.user_id
             WHERE a.id = %d LIMIT 1",
            $aff_id
        ), ARRAY_A );
        if ( $aff_row ) {
            if ( $aff_name === '' )  $aff_name  = (string) ( $aff_row['display_name'] ?? '' );
            if ( $aff_email === '' ) $aff_email = (string) ( $aff_row['user_email'] ?? '' );
            if ( $aff_doc === '' )   $aff_doc   = (string) ( $aff_row['document'] ?? '' );
            if ( $comm_pct <= 0 )    $comm_pct  = (float)  ( $aff_row['commission_pct'] ?? 0 );
            $order->update_meta_data( '_sz_aff_name',  $aff_name );
            $order->update_meta_data( '_sz_aff_email', $aff_email );
            if ( $aff_doc ) $order->update_meta_data( '_sz_aff_document', $aff_doc );
            $order->save();
        }
    }

    if ( $order_total <= 0 ) {
        $order_total = (float) ( $order->get_meta( '_senderzz_offer_value', true ) ?: $order->get_total() );
    }

    if ( $prod_name === '' && $producer_id && function_exists( 'sz_aff_get_producer_display_name' ) ) {
        $prod_name = sz_aff_get_producer_display_name( $producer_id );
    }

    $delivery_label = 'Não identificado';
    if ( ! empty( $ctx['delivery_mode'] ) ) {
        $delivery_label = $ctx['delivery_mode'] === 'motoboy' ? 'Motoboy / COD' : ( $ctx['delivery_mode'] === 'expedition' ? 'Expedição' : 'Não identificado' );
    }
    $status_raw  = (string) ( $ctx['status_raw'] ?? $order->get_status() );
    $status_norm = (string) ( $ctx['status'] ?? $order->get_status() );
    $flow_status = (string) ( $ctx['flow_status'] ?? $order->get_meta( '_senderzz_motoboy_flow_status', true ) );
    $checkout_id = (int) ( $ctx['checkout_link_id'] ?? $order->get_meta( '_senderzz_checkout_link_id', true ) );
    $offer_token = (string) ( $ctx['offer_token'] ?? $order->get_meta( '_senderzz_offer_token', true ) );
    $offer_value = (float) ( $ctx['offer_value'] ?? ( $order->get_meta( '_senderzz_offer_value', true ) ?: $order_total ) );

    // Taxas discriminadas
    $taxa_entrega   = (float) $order->get_meta( '_sz_mb_taxa_entrega',   true );
    $taxa_manuseio  = (float) $order->get_meta( '_sz_mb_taxa_manuseio',  true );
    $taxa_adicional = (float) $order->get_meta( '_sz_mb_taxa_adicional', true );
    $taxa_total     = (float) $order->get_meta( '_sz_mb_taxa_total',     true );
    $taxa_mode      = (string) $order->get_meta( '_sz_mb_taxa_transacao_modo', true );
    $taxa_aff       = (float) $order->get_meta( '_sz_aff_transaction_fee', true );
    $taxa_prod      = (float) $order->get_meta( '_sz_prod_transaction_fee', true );
    $commission_gross_meta = (float) $order->get_meta( '_sz_aff_commission_gross', true );
    if ( $taxa_total <= 0 && ! empty( $ctx['motoboy_fee_total'] ) ) $taxa_total = (float) $ctx['motoboy_fee_total'];

    $net = (float) $order->get_meta( '_sz_aff_net', true );
    if ( $net <= 0 && $order_total > 0 && $taxa_total > 0 ) $net = $order_total - $taxa_total;
    if ( $net <= 0 && $order_total > 0 ) $net = $order_total;

    $producer_net = (float) ( $order->get_meta( '_sz_prod_commission', true ) ?: 0 );
    if ( $producer_net <= 0 && $net > 0 && $commission > 0 ) $producer_net = max( 0, round( $net - $commission, 2 ) );

    echo '<table style="width:100%;border-collapse:collapse;font-size:var(--sz-text-meta)">';

    $section = function( string $label ) {
        echo '<tr><td colspan="2" style="padding:7px 6px 2px;font-size:var(--sz-text-xs);font-weight:700;color:#94a3b8;text-transform:none;letter-spacing:0">' . esc_html( $label ) . '</td></tr>';
    };
    $row = function( string $label, string $value, string $color = '#111' ) {
        echo '<tr><td style="padding:3px 6px;color:#666;font-weight:600;vertical-align:top">' . esc_html( $label ) . '</td><td style="padding:3px 6px;font-weight:700;color:' . esc_attr( $color ) . ';word-break:break-word">' . esc_html( $value ) . '</td></tr>';
    };

    $section( 'Pedido' );
    $row( 'Tipo', $delivery_label );
    $row( 'Status', trim( $status_raw . ' → ' . $status_norm ) );
    if ( $flow_status !== '' ) $row( 'Fluxo COD', $flow_status );
    $row( 'Produtor', $prod_name ? $prod_name . ( $producer_id ? ' #' . $producer_id : '' ) : ( $producer_id ? '#' . $producer_id : '—' ) );
    $row( 'Afiliado', $aff_id ? ( $aff_name ? $aff_name . ' #' . $aff_id : '#' . $aff_id ) : '—' );
    if ( $aff_email !== '' ) $row( 'E-mail afiliado', $aff_email );
    if ( $aff_doc !== '' ) $row( 'Documento', $aff_doc );
    if ( $checkout_id ) $row( 'Link checkout', '#' . $checkout_id );
    if ( $offer_token !== '' ) $row( 'Oferta/token', $offer_token );

    $section( 'Financeiro' );
    $row( 'Total bruto', $fmt( $order_total ) );
    if ( $offer_value > 0 ) $row( 'Valor oferta', $fmt( $offer_value ) );

    if ( $taxa_total > 0 ) {
        $section( 'Taxas Senderzz' );
        if ( $taxa_entrega > 0 )   $row( 'Entrega', $fmt( $taxa_entrega ), '#475467' );
        if ( $taxa_manuseio > 0 )  $row( 'Manuseio', $fmt( $taxa_manuseio ), '#475467' );
        if ( $taxa_adicional > 0 ) {
            if ( $taxa_mode === 'split_producer_affiliate' && ( $taxa_aff > 0 || $taxa_prod > 0 ) ) {
                if ( $taxa_aff > 0 )  $row( 'Transação afiliado', $fmt( $taxa_aff ), '#475467' );
                if ( $taxa_prod > 0 ) $row( 'Transação produtor', $fmt( $taxa_prod ), '#475467' );
            } else {
                $row( 'Transação', $fmt( $taxa_adicional ), '#475467' );
            }
        }
        $row( 'Total taxas', $fmt( $taxa_total ), '#dc2626' );
    }

    if ( $net > 0 ) $row( 'Líquido', $fmt( $net ), '#0369a1' );

    $section( 'Distribuição' );
    if ( $comm_pct > 0 ) $row( 'Comissão%', number_format( $comm_pct, 1, ',', '.' ) . '%' );
    if ( $commission_gross_meta > 0 && $taxa_aff > 0 ) $row( 'Comissão bruta afiliado', $fmt( $commission_gross_meta ), '#E8650A' );
    if ( $taxa_aff > 0 ) $row( 'Taxa transação afiliado', '-' . $fmt( $taxa_aff ), '#dc2626' );
    $row( 'Afiliado recebe', $fmt( $commission ), '#E8650A' );
    $row( 'Produtor recebe', $fmt( $producer_net ), '#16a34a' );

    if ( ! empty( $ctx ) ) {
        $section( 'Operacional' );
        $row( 'Financeiro liquidado', ! empty( $ctx['financially_settled'] ) ? 'Sim' : 'Não' );
        $row( 'Estoque baixado', (string) ( $ctx['stock_reduced'] ?: '—' ) );
        $row( 'Estoque restaurado', (string) ( $ctx['stock_restored_frustrado'] ?: '—' ) );
    }

    echo '</table>';
}
add_filter( 'manage_woocommerce_page_wc-orders_columns', 'sz_aff_add_order_column', 20 );
add_filter( 'manage_edit-shop_order_columns', 'sz_aff_add_order_column', 20 );
add_filter( 'manage_woocommerce_page_wc-orders_columns', 'sz_aff_add_order_column', 20 );
function sz_aff_add_order_column( array $columns ): array {
    $new = [];
    foreach ( $columns as $key => $label ) {
        $new[$key] = $label;
        if ( $key === 'order_status' ) {
            $new['sz_affiliate']     = '🤝 Afiliado';
            $new['sz_delivery_date'] = '📅 Entrega';
        }
    }
    return $new;
}
add_action( 'manage_woocommerce_page_wc-orders_custom_column', 'sz_aff_render_order_column', 10, 2 );
add_action( 'manage_shop_order_posts_custom_column', 'sz_aff_render_order_column', 10, 2 );
function sz_aff_render_order_column( string $column, $post_or_order ): void {
    if ( $column === 'sz_delivery_date' ) {
        $order = $post_or_order instanceof WC_Order ? $post_or_order : wc_get_order( $post_or_order );
        if ( ! $order instanceof WC_Order ) { echo '—'; return; }
        // Busca data — _sz_delivery_date é a meta correta gravada pelo checkout motoboy
        $date = (string) (
            $order->get_meta( '_sz_delivery_date', true ) ?:
            $order->get_meta( '_senderzz_delivery_date', true ) ?:
            $order->get_meta( '_senderzz_delivery_time', true ) ?: ''
        );
        if ( ! $date ) {
            global $wpdb;
            $table = $wpdb->prefix . 'sz_motoboy_pedidos';
            if ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $table ) ) ) {
                $row = $wpdb->get_row( $wpdb->prepare(
                    "SELECT agendamento_data, entrega_data FROM {$table} WHERE wc_order_id = %d LIMIT 1",
                    $order->get_id()
                ) );
                if ( $row ) $date = (string) ( $row->agendamento_data ?: $row->entrega_data ?: '' );
            }
        }
        if ( $date && preg_match( '/^(\d{4}-\d{2}-\d{2})/', $date, $dm ) ) {
            // T12:00:00 evita perda de um dia em UTC-3
            $date = wp_date( 'd/m/Y', strtotime( $dm[1] . 'T12:00:00' ) );
        }
        if ( $date ) {
            echo '<span style="font-weight:700;color:#E8650A;font-size:var(--sz-text-meta)">📅 ' . esc_html( $date ) . '</span>';
        } else {
            echo '<span style="color:#ccc;font-size:var(--sz-text-sm)">—</span>';
        }
        return;
    }
    if ( $column !== 'sz_affiliate' ) return;
    $order = $post_or_order instanceof WC_Order ? $post_or_order : wc_get_order( $post_or_order );
    if ( ! $order instanceof WC_Order ) return;

    $aff_id      = (int) $order->get_meta( '_sz_affiliate_id', true );
    $aff_name    = (string) $order->get_meta( '_sz_aff_name', true );
    $aff_pct     = (float) $order->get_meta( '_sz_aff_commission_pct', true );
    if ( ! $aff_id ) { echo '<span style="color:#ccc">—</span>'; return; }

    // Auto-correção visual/operacional: se o pedido ficou com pct legado do checkout/link,
    // volta para a comissão atual do afiliado definida pelo produtor.
    $current_aff_pct = function_exists( 'sz_aff_effective_affiliate_commission_pct' ) ? sz_aff_effective_affiliate_commission_pct( $aff_id ) : $aff_pct;
    if ( $current_aff_pct > 0 && abs( $current_aff_pct - $aff_pct ) >= 0.01 ) {
        $aff_pct = $current_aff_pct;
        $order_total_fix = (float) $order->get_meta( '_sz_aff_order_total', true );
        if ( $order_total_fix <= 0 ) $order_total_fix = (float) $order->get_meta( '_senderzz_offer_value', true ) ?: (float) $order->get_total();
        $commission_fix = round( max( 0, $order_total_fix ) * $aff_pct / 100, 2 );
        $order->update_meta_data( '_sz_aff_commission_pct', $aff_pct );
        $order->update_meta_data( '_sz_aff_commission', $commission_fix );
        $order->save();
    }

    $commission  = (float) $order->get_meta( '_sz_aff_commission', true );
    $taxa_total  = (float) $order->get_meta( '_sz_mb_taxa_total', true );
    $order_total = (float) $order->get_meta( '_sz_aff_order_total', true );
    if ( $order_total <= 0 ) $order_total = (float) $order->get_meta( '_senderzz_offer_value', true ) ?: (float) $order->get_total();
    $net         = (float) $order->get_meta( '_sz_aff_net', true );
    if ( $net <= 0 && $order_total > 0 && $taxa_total > 0 ) $net = $order_total - $taxa_total;
    $producer_net = $net > 0 && $commission > 0 ? max( 0, round( $net - $commission, 2 ) ) : 0;
    $fmt = fn( float $v ) => 'R$&nbsp;' . number_format( $v, 2, ',', '.' );

    echo '<div style="font-size:var(--sz-text-sm);line-height:1.5;min-width:140px">';
    echo '<strong style="font-size:var(--sz-text-meta);color:#111">' . esc_html( $aff_name ?: '#'.$aff_id ) . '</strong>';
    if ( $order_total > 0 ) echo '<br><span style="color:#64748b">Bruto: ' . esc_html( strip_tags( $fmt( $order_total ) ) ) . '</span>';
    if ( $taxa_total > 0 )  echo '<br><span style="color:#dc2626">Taxas: ' . esc_html( strip_tags( $fmt( $taxa_total ) ) ) . '</span>';
    if ( $net > 0 )         echo '<br><span style="color:#0369a1">Líq: ' . esc_html( strip_tags( $fmt( $net ) ) ) . '</span>';
    echo '<br><span style="color:#E8650A;font-weight:700">Afil: ' . esc_html( strip_tags( $fmt( $commission ) ) ) . ' (' . esc_html( number_format( $aff_pct, 1, ',', '.' ) ) . '%)</span>';
    if ( $producer_net > 0 ) echo '<br><span style="color:#16a34a;font-weight:700">Prod: ' . esc_html( strip_tags( $fmt( $producer_net ) ) ) . '</span>';
    echo '</div>';
}

// ═══════════════════════════════════════════════════════════════════════════
// ADMIN — Relatório de Taxas COD por pedido
// ═══════════════════════════════════════════════════════════════════════════
function sz_admin_taxas_cod_page(): void {
    global $wpdb;

    // ── Salvar configurações de penalidades ───────────────────────────────────
    if ( isset( $_POST['sz_penalidades_save'] ) ) {
        check_admin_referer( 'sz_penalidades_save' );
        $parse = fn( $key, $default ) => max( 0, (float) str_replace( ',', '.', $_POST[ $key ] ?? $default ) );
        update_option( 'sz_aff_first_frustration_penalty',    $parse( 'penalty_aff_primeira',    5 ),    false );
        update_option( 'sz_aff_default_penalty_value',        $parse( 'penalty_aff_reincidente',  5 ),    false );
        update_option( 'sz_aff_producer_frustration_penalty', $parse( 'penalty_prod_reincidente', 8 ),    false );
        update_option( 'sz_motoboy_taxa_frustrado',           $parse( 'taxa_motoboy_frustrado',   10 ),   false );
        echo '<div class="notice notice-success is-dismissible"><p>✅ Configurações de penalidades salvas.</p></div>';
    }

    $per_page = 50;
    $paged    = max( 1, (int) ( $_GET['paged'] ?? 1 ) );
    $offset   = ( $paged - 1 ) * $per_page;
    $search   = sanitize_text_field( wp_unslash( $_GET['s'] ?? '' ) );
    $date_from = sanitize_text_field( wp_unslash( $_GET['date_from'] ?? '' ) );
    $date_to   = sanitize_text_field( wp_unslash( $_GET['date_to'] ?? '' ) );

    // Monta filtros dinamicamente sem JOIN direto em meta.
    // Regra única correta: 1 linha por pedido, usando MAX() por meta_key para não multiplicar
    // totais quando existir meta duplicada em wc_orders_meta.
    $where_orders = "WHERE 1=1";
    $params  = [];

    if ( $date_from && preg_match('/^\d{4}-\d{2}-\d{2}$/', $date_from) ) {
        $where_orders .= " AND o.date_created_gmt >= %s";
        $params[] = $date_from . ' 00:00:00';
    }
    if ( $date_to && preg_match('/^\d{4}-\d{2}-\d{2}$/', $date_to) ) {
        $where_orders .= " AND o.date_created_gmt <= %s";
        $params[] = $date_to . ' 23:59:59';
    }

    $base_subquery = "
        SELECT
            o.id AS order_id,
            o.date_created_gmt AS created_at,
            o.status,
            MAX(CASE WHEN m.meta_key = '_sz_mb_taxa_total'      THEN CAST(m.meta_value AS DECIMAL(10,2)) ELSE 0 END) AS taxa_total,
            MAX(CASE WHEN m.meta_key = '_sz_mb_taxa_entrega'    THEN CAST(m.meta_value AS DECIMAL(10,2)) ELSE 0 END) AS taxa_entrega,
            MAX(CASE WHEN m.meta_key = '_sz_mb_taxa_manuseio'   THEN CAST(m.meta_value AS DECIMAL(10,2)) ELSE 0 END) AS taxa_manuseio,
            MAX(CASE WHEN m.meta_key = '_sz_mb_taxa_adicional'  THEN CAST(m.meta_value AS DECIMAL(10,2)) ELSE 0 END) AS taxa_transacao,
            GREATEST(MAX(CASE WHEN m.meta_key = '_sz_aff_gross' THEN CAST(m.meta_value AS DECIMAL(10,2)) ELSE 0 END),MAX(CASE WHEN m.meta_key = '_senderzz_offer_value' THEN CAST(m.meta_value AS DECIMAL(10,2)) ELSE 0 END)) AS valor_pedido,
            MAX(CASE WHEN m.meta_key = '_sz_aff_commission'     THEN CAST(m.meta_value AS DECIMAL(10,2)) ELSE 0 END) AS comissao_afiliado,
            MAX(CASE WHEN m.meta_key = '_sz_aff_name'           THEN m.meta_value ELSE '' END) AS afiliado_nome,
            MAX(CASE WHEN m.meta_key = '_sz_aff_producer_name'  THEN m.meta_value ELSE '' END) AS produtor_nome
        FROM {$wpdb->prefix}wc_orders o
        INNER JOIN {$wpdb->prefix}wc_orders_meta m ON m.order_id = o.id
        {$where_orders}
          AND m.meta_key IN (
            '_sz_mb_taxa_total','_sz_mb_taxa_entrega','_sz_mb_taxa_manuseio','_sz_mb_taxa_adicional',
            '_sz_aff_gross','_senderzz_offer_value','_sz_aff_commission','_sz_aff_name','_sz_aff_producer_name'
          )
        GROUP BY o.id
    ";

    $select = "SELECT * FROM ({$base_subquery}) x
        WHERE x.taxa_total > 0
        ORDER BY x.order_id DESC
        LIMIT %d OFFSET %d";

    $count_sql = "SELECT COUNT(*) FROM ({$base_subquery}) x WHERE x.taxa_total > 0";

    $query_params  = array_merge( $params, [ $per_page, $offset ] );
    $count_params  = $params;

    $rows  = $wpdb->get_results( $wpdb->prepare( $select, $query_params ), ARRAY_A ) ?: [];
    $total = (int) $wpdb->get_var( $count_params ? $wpdb->prepare( $count_sql, $count_params ) : $count_sql );
    $pages = $total > 0 ? (int) ceil( $total / $per_page ) : 1;

    // Totais agregados pela mesma base deduplicada.
    $totals_sql = "SELECT
        SUM(x.taxa_total)     AS total_taxa,
        SUM(x.taxa_entrega)   AS total_entrega,
        SUM(x.taxa_manuseio)  AS total_manuseio,
        SUM(x.taxa_transacao) AS total_transacao
    FROM ({$base_subquery}) x
    WHERE x.taxa_total > 0";
    $totals = $wpdb->get_row( $count_params ? $wpdb->prepare( $totals_sql, $count_params ) : $totals_sql, ARRAY_A ) ?: [];

    $fmt = fn( $v ) => 'R$ ' . number_format( (float) $v, 2, ',', '.' );
    $cur_url = remove_query_arg( 'paged' );
    $taxa_motoboy_admin = (float) get_option( 'sz_admin_motoboy_fee', 18 );
    $taxa_frustrado_motoboy = 5.00; // despesa fixa ao motoboy por frustrado
    ?>
    <div class="wrap">
    <h1>🧾 Taxas COD — Breakdown por Pedido</h1>
    <p style="color:#666;max-width:800px;margin-bottom:16px;">Detalhamento completo das taxas cobradas em cada pedido motoboy: entrega + manuseio + transação (percentual).</p>

    <!-- Totalizadores -->
    <div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(160px,1fr));gap:12px;margin-bottom:24px;">
        <?php foreach([
            ['🧾','Total Taxas Arrecadadas', $totals['total_taxa'] ?? 0, '#E8650A'],
            ['🏍️','Taxa de Entrega',          $totals['total_entrega'] ?? 0, '#0369a1'],
            ['📦','Taxa de Manuseio',          $totals['total_manuseio'] ?? 0, '#7c3aed'],
            ['💳','Taxa de Transação (%)',      $totals['total_transacao'] ?? 0, '#059669'],
        ] as [$ico,$lbl,$val,$color]): ?>
        <div style="background:#fff;border:1px solid #e5e7eb;border-radius:14px;padding:16px;">
            <div style="font-size:var(--sz-text-lg)"><?php echo $ico; ?></div>
            <div style="font-size:var(--sz-text-sm);font-weight:700;color:#64748b;text-transform:none;letter-spacing:0;margin:4px 0"><?php echo esc_html($lbl); ?></div>
            <div style="font-size:var(--sz-text-xl);font-weight:700;color:<?php echo esc_attr($color); ?>"><?php echo esc_html($fmt($val)); ?></div>
        </div>
        <?php endforeach; ?>
    </div>

    <!-- Filtros -->
    <form method="get" style="display:flex;gap:10px;align-items:flex-end;flex-wrap:wrap;margin-bottom:16px;background:#fff;border:1px solid #e5e7eb;border-radius:12px;padding:14px 18px;">
        <input type="hidden" name="page" value="senderzz">
        <input type="hidden" name="tab" value="taxas-cod">
        <label style="display:flex;flex-direction:column;gap:4px;font-size:var(--sz-text-meta);font-weight:700;color:#64748b;">
            De <input type="date" name="date_from" value="<?php echo esc_attr($date_from); ?>" style="height:36px;border:1px solid #d1d5db;border-radius:8px;padding:0 10px;font-size:var(--sz-text-base);">
        </label>
        <label style="display:flex;flex-direction:column;gap:4px;font-size:var(--sz-text-meta);font-weight:700;color:#64748b;">
            Até <input type="date" name="date_to" value="<?php echo esc_attr($date_to); ?>" style="height:36px;border:1px solid #d1d5db;border-radius:8px;padding:0 10px;font-size:var(--sz-text-base);">
        </label>
        <button class="button button-primary" style="height:36px;margin-top:auto;">Filtrar</button>
        <?php if ($date_from || $date_to): ?><a href="<?php echo esc_url(remove_query_arg(['date_from','date_to','paged'])); ?>" class="button" style="height:36px;margin-top:auto;">Limpar</a><?php endif; ?>
    </form>

    <!-- Card de configurações de penalidades -->
    <div style="background:#fff;border:1px solid #e5e7eb;border-radius:16px;padding:20px 24px;margin-bottom:24px;">
        <h2 style="margin:0 0 4px;font-size:var(--sz-text-lg);font-weight:700;color:#111827">⚠️ Configuração de Penalidades por Frustração</h2>
        <p style="margin:0 0 18px;color:#6b7280;font-size:var(--sz-text-base)">Valores cobrados automaticamente quando uma entrega é frustrada. A 1ª frustração de um cliente é isenta para o produtor.</p>
        <form method="post">
            <?php wp_nonce_field( 'sz_penalidades_save' ); ?>
            <input type="hidden" name="sz_penalidades_save" value="1">
            <div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(200px,1fr));gap:16px;margin-bottom:16px">

                <div style="background:#fff7ed;border:1px solid #fed7aa;border-radius:12px;padding:16px">
                    <div style="font-size:var(--sz-text-sm);font-weight:700;color:#9a3412;text-transform:none;letter-spacing:0;margin-bottom:8px">👤 Afiliado — 1ª Frustração</div>
                    <label style="display:block;font-size:var(--sz-text-meta);font-weight:700;color:#374151;margin-bottom:6px">Valor cobrado do afiliado (R$)</label>
                    <input type="text" name="penalty_aff_primeira"
                           value="<?php echo esc_attr( number_format( sz_aff_first_frustration_penalty(), 2, ',', '.' ) ); ?>"
                           style="width:100%;height:40px;border:1px solid #d1d5db;border-radius:8px;padding:0 10px;font-size:var(--sz-text-md);font-weight:700">
                    <p style="font-size:var(--sz-text-sm);color:#9ca3af;margin:6px 0 0">Produtor isento na 1ª tentativa</p>
                </div>

                <div style="background:#fef2f2;border:1px solid #fecaca;border-radius:12px;padding:16px">
                    <div style="font-size:var(--sz-text-sm);font-weight:700;color:#991b1b;text-transform:none;letter-spacing:0;margin-bottom:8px">👤 Afiliado — 2ª+ Frustração</div>
                    <label style="display:block;font-size:var(--sz-text-meta);font-weight:700;color:#374151;margin-bottom:6px">Valor cobrado do afiliado (R$)</label>
                    <input type="text" name="penalty_aff_reincidente"
                           value="<?php echo esc_attr( number_format( sz_aff_default_penalty_value(), 2, ',', '.' ) ); ?>"
                           style="width:100%;height:40px;border:1px solid #d1d5db;border-radius:8px;padding:0 10px;font-size:var(--sz-text-md);font-weight:700">
                </div>

                <div style="background:#fef2f2;border:1px solid #fecaca;border-radius:12px;padding:16px">
                    <div style="font-size:var(--sz-text-sm);font-weight:700;color:#991b1b;text-transform:none;letter-spacing:0;margin-bottom:8px">🏭 Produtor — 2ª+ Frustração</div>
                    <label style="display:block;font-size:var(--sz-text-meta);font-weight:700;color:#374151;margin-bottom:6px">Valor cobrado do produtor (R$)</label>
                    <input type="text" name="penalty_prod_reincidente"
                           value="<?php echo esc_attr( number_format( sz_aff_producer_frustration_penalty(), 2, ',', '.' ) ); ?>"
                           style="width:100%;height:40px;border:1px solid #d1d5db;border-radius:8px;padding:0 10px;font-size:var(--sz-text-md);font-weight:700">
                    <p style="font-size:var(--sz-text-sm);color:#9ca3af;margin:6px 0 0">Debitado da carteira TPC do produtor</p>
                </div>

                <div style="background:#f0fdf4;border:1px solid #bbf7d0;border-radius:12px;padding:16px">
                    <div style="font-size:var(--sz-text-sm);font-weight:700;color:#166534;text-transform:none;letter-spacing:0;margin-bottom:8px">🏍️ Taxa Motoboy por Frustrado</div>
                    <label style="display:block;font-size:var(--sz-text-meta);font-weight:700;color:#374151;margin-bottom:6px">Valor pago ao motoboy (R$)</label>
                    <input type="text" name="taxa_motoboy_frustrado"
                           value="<?php echo esc_attr( number_format( (float) get_option( 'sz_motoboy_taxa_frustrado', 10 ), 2, ',', '.' ) ); ?>"
                           style="width:100%;height:40px;border:1px solid #d1d5db;border-radius:8px;padding:0 10px;font-size:var(--sz-text-md);font-weight:700">
                    <p style="font-size:var(--sz-text-sm);color:#9ca3af;margin:6px 0 0">Pago ao motoboy na tentativa frustrada</p>
                </div>

            </div>
            <button type="submit" class="button button-primary" style="height:38px;font-weight:700">💾 Salvar penalidades</button>
        </form>
    </div>

    <!-- Legenda status financeiro -->
    <div style="display:flex;gap:12px;flex-wrap:wrap;margin-bottom:12px;font-size:var(--sz-text-meta);color:#64748b;">
        <span>✅ <strong>Completo</strong>: Senderzz paga R$ <?php echo number_format($taxa_motoboy_admin,2,',','.'); ?> ao motoboy</span>
        <span>⚠️ <strong>Frustrado</strong>: Senderzz paga R$ <?php echo number_format($taxa_frustrado_motoboy,2,',','.'); ?> ao motoboy · Penalidades conforme configuração</span>
    </div>

    <!-- Tabela -->
    <table class="widefat striped" style="font-size:var(--sz-text-base);">
        <thead>
            <tr style="background:#f8fafc;">
                <th style="width:70px">#Pedido</th>
                <th>Data</th>
                <th>Status</th>
                <th>Produtor</th>
                <th>Afiliado</th>
                <th style="text-align:right">Valor Pedido</th>
                <th style="text-align:right">Entrega</th>
                <th style="text-align:right">Manuseio</th>
                <th style="text-align:right">Transação</th>
                <th style="text-align:right;background:#fff7ed;color:#9a3412;font-weight:700">Total Taxa</th>
                <th style="text-align:right;color:#E8650A">Comissão Afil.</th>
                <th style="text-align:right;color:#16a34a">Comissão Prod.</th>
                <th style="text-align:right;color:#7c3aed">Desp. Motoboy</th>
            </tr>
        </thead>
        <tbody>
        <?php if ( empty($rows) ): ?>
            <tr><td colspan="13" style="text-align:center;padding:24px;color:#64748b">Nenhum pedido encontrado.</td></tr>
        <?php else: foreach($rows as $r):
            $st_raw = ltrim( (string) $r['status'], 'wc-' );
            $is_completo  = in_array( $st_raw, [ 'completo', 'entregue', 'completed', 'delivered' ], true );
            $is_frustrado = in_array( $st_raw, [ 'frustracao', 'frustrado', 'failed' ], true );
            $is_cancelado = in_array( $st_raw, [ 'cancelado', 'cancelled' ], true );

            $status_label = [
                'completo'   => '✅ Completo',
                'entregue'   => '✅ Entregue',
                'completed'  => '✅ Completo',
                'cancelado'  => '❌ Cancelado',
                'cancelled'  => '❌ Cancelado',
                'frustracao' => '⚠️ Frustrado',
                'frustrado'  => '⚠️ Frustrado',
                'failed'     => '⚠️ Frustrado',
                'agendado'   => '📅 Agendado',
            ][ $st_raw ] ?? ucfirst( $r['status'] );

            // Comissão produtor = valor pedido - taxas - comissão afiliado
            $net_pedido       = max( 0, (float) $r['valor_pedido'] - (float) $r['taxa_total'] );
            $comissao_prod    = max( 0, round( $net_pedido - (float) $r['comissao_afiliado'], 2 ) );

            // Despesa motoboy: só aparece quando pedido está em status final
            $desp_motoboy = 0.0;
            if ( $is_completo )  $desp_motoboy = $taxa_motoboy_admin;
            if ( $is_frustrado ) $desp_motoboy = $taxa_frustrado_motoboy;

            // Comissão prod só mostra para status finalizados
            $show_prod = $is_completo || $is_frustrado || $is_cancelado;
        ?>
            <tr>
                <td><a href="<?php echo esc_url(admin_url('post.php?post='.$r['order_id'].'&action=edit')); ?>" style="font-weight:700;color:#E8650A">#<?php echo esc_html($r['order_id']); ?></a></td>
                <td><?php echo esc_html(mysql2date('d/m/Y H:i', $r['created_at'])); ?></td>
                <td style="font-size:var(--sz-text-sm)"><?php echo esc_html($status_label); ?></td>
                <td style="font-size:var(--sz-text-meta)"><?php echo esc_html($r['produtor_nome'] ?: '—'); ?></td>
                <td style="font-size:var(--sz-text-meta)"><?php echo esc_html($r['afiliado_nome'] ?: '—'); ?></td>
                <td style="text-align:right;font-weight:700"><?php echo esc_html($fmt($r['valor_pedido'])); ?></td>
                <td style="text-align:right;color:#0369a1;font-weight:700"><?php echo esc_html($fmt($r['taxa_entrega'])); ?></td>
                <td style="text-align:right;color:#7c3aed;font-weight:700"><?php echo esc_html($fmt($r['taxa_manuseio'])); ?></td>
                <td style="text-align:right;color:#059669;font-weight:700"><?php echo esc_html($fmt($r['taxa_transacao'])); ?></td>
                <td style="text-align:right;background:#fff7ed;font-weight:700;color:#E8650A"><?php echo esc_html($fmt($r['taxa_total'])); ?></td>
                <td style="text-align:right;color:#E8650A;font-weight:700"><?php echo $r['comissao_afiliado'] > 0 ? esc_html($fmt($r['comissao_afiliado'])) : '—'; ?></td>
                <td style="text-align:right;color:#16a34a;font-weight:700"><?php echo $show_prod && $comissao_prod > 0 ? esc_html($fmt($comissao_prod)) : '<span style="color:#cbd5e1">—</span>'; ?></td>
                <td style="text-align:right;color:#7c3aed;font-weight:700"><?php echo $desp_motoboy > 0 ? '<span style="color:#dc2626">−' . esc_html(number_format($desp_motoboy,2,',','.')) . '</span>' : '—'; ?></td>
            </tr>
        <?php endforeach; endif; ?>
        </tbody>
        <tfoot>
            <tr style="background:#f8fafc;font-weight:700;">
                <td colspan="5">Total (<?php echo esc_html($total); ?> pedidos)</td>
                <td style="text-align:right">—</td>
                <td style="text-align:right;color:#0369a1"><?php echo esc_html($fmt($totals['total_entrega'] ?? 0)); ?></td>
                <td style="text-align:right;color:#7c3aed"><?php echo esc_html($fmt($totals['total_manuseio'] ?? 0)); ?></td>
                <td style="text-align:right;color:#059669"><?php echo esc_html($fmt($totals['total_transacao'] ?? 0)); ?></td>
                <td style="text-align:right;background:#fff7ed;color:#E8650A;font-weight:700"><?php echo esc_html($fmt($totals['total_taxa'] ?? 0)); ?></td>
                <td></td><td></td><td></td>
            </tr>
        </tfoot>
    </table>

    <!-- Paginação -->
    <?php if ($pages > 1): ?>
    <div style="display:flex;gap:6px;margin-top:16px;align-items:center;flex-wrap:wrap;">
        <?php for ($p = 1; $p <= $pages; $p++): ?>
            <?php if ($p === $paged): ?>
                <span style="display:inline-flex;align-items:center;justify-content:center;width:32px;height:32px;border-radius:8px;background:#E8650A;color:#fff;font-weight:700;font-size:var(--sz-text-base);"><?php echo $p; ?></span>
            <?php else: ?>
                <a href="<?php echo esc_url(add_query_arg('paged',$p,$cur_url)); ?>" style="display:inline-flex;align-items:center;justify-content:center;width:32px;height:32px;border-radius:8px;background:#fff;border:1px solid #e5e7eb;color:#111;font-weight:700;font-size:var(--sz-text-base);text-decoration:none;"><?php echo $p; ?></a>
            <?php endif; ?>
        <?php endfor; ?>
    </div>
    <?php endif; ?>
    </div>
    <?php
}
