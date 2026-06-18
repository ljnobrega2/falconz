<?php

function sz_onboarding_mail_html_ct(): string { return 'text/html'; }
/**
 * senderzz-onboarding.php
 *
 * Formulário de auto-cadastro de novos produtores.
 * Shortcode: [senderzz_onboarding]
 *
 * Fluxo:
 *  1. Produtor preenche nome, e-mail, telefone e aceita os termos.
 *  2. Sistema cria um registro pendente em senderzz_onboarding_requests.
 *  3. Admin recebe e-mail com link de aprovação no WP Admin.
 *  4. Ao aprovar, cria automaticamente: usuário WP + usuário portal + shipping class.
 *  5. Produtor recebe e-mail com acesso.
 */

defined( 'ABSPATH' ) || exit;
if ( defined( 'SENDERZZ_ONBOARDING_LOADED' ) ) return;
define( 'SENDERZZ_ONBOARDING_LOADED', true );

if ( ! function_exists( 'sz_onboarding_digits' ) ) {
function sz_onboarding_digits( string $value ): string { return preg_replace( '/\D+/', '', $value ); }
}
if ( ! function_exists( 'sz_onboarding_validate_cpf' ) ) {
function sz_onboarding_validate_cpf( string $cpf ): bool {
    $cpf = sz_onboarding_digits( $cpf );
    if ( strlen( $cpf ) !== 11 || preg_match( '/^(\d)\1{10}$/', $cpf ) ) return false;
    for ( $t = 9; $t < 11; $t++ ) {
        $sum = 0;
        for ( $i = 0; $i < $t; $i++ ) $sum += (int) $cpf[$i] * ( ( $t + 1 ) - $i );
        $digit = ( ( 10 * $sum ) % 11 ) % 10;
        if ( (int) $cpf[$t] !== $digit ) return false;
    }
    return true;
}
}
if ( ! function_exists( 'sz_onboarding_format_cpf' ) ) {
function sz_onboarding_format_cpf( string $cpf ): string {
    $cpf = substr( sz_onboarding_digits( $cpf ), 0, 11 );
    return strlen( $cpf ) === 11 ? substr($cpf,0,3).'.'.substr($cpf,3,3).'.'.substr($cpf,6,3).'-'.substr($cpf,9,2) : $cpf;
}
}
if ( ! function_exists( 'sz_senderzz_account_exists_by_email_or_cpf' ) ) {
function sz_senderzz_account_exists_by_email_or_cpf( string $email = '', string $cpf = '', int $exclude_user_id = 0 ): bool {
    global $wpdb;
    $email = sanitize_email( $email );
    $cpf = sz_onboarding_digits( $cpf );
    if ( $email !== '' ) {
        $user_id = (int) email_exists( $email );
        if ( $user_id > 0 && $user_id !== $exclude_user_id ) return true;
        $portal_table = $wpdb->prefix . 'senderzz_portal_users';
        $has_portal = (int) $wpdb->get_var( $wpdb->prepare( "SELECT id FROM {$portal_table} WHERE email=%s LIMIT 1", $email ) );
        if ( $has_portal > 0 ) return true;
    }
    if ( $cpf !== '' ) {
        $has_meta = (int) $wpdb->get_var( $wpdb->prepare( "SELECT user_id FROM {$wpdb->usermeta} WHERE meta_key='sz_document' AND REPLACE(REPLACE(REPLACE(meta_value,'.',''),'-',''),'/','')=%s LIMIT 1", $cpf ) );
        if ( $has_meta > 0 && $has_meta !== $exclude_user_id ) return true;
        $aff_table = $wpdb->prefix . 'sz_affiliates';
        $has_aff = (int) $wpdb->get_var( $wpdb->prepare( "SELECT id FROM {$aff_table} WHERE document=%s AND deleted_at IS NULL LIMIT 1", $cpf ) );
        if ( $has_aff > 0 ) return true;
    }
    return false;
}
}

// ── Tabela ────────────────────────────────────────────────────────────────────
function sz_onboarding_install(): void {
    global $wpdb;
    $table   = $wpdb->prefix . 'senderzz_onboarding_requests';
    $charset = $wpdb->get_charset_collate();
    $sql = "CREATE TABLE IF NOT EXISTS {$table} (
        id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        nome        VARCHAR(191) NOT NULL,
        email       VARCHAR(191) NOT NULL,
        document    VARCHAR(20) NULL,
        telefone    VARCHAR(30) NULL,
        empresa     VARCHAR(191) NULL,
        status      VARCHAR(20) NOT NULL DEFAULT 'pending',
        token       VARCHAR(64) NOT NULL,
        created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
        approved_at DATETIME NULL,
        notes       TEXT NULL,
        PRIMARY KEY (id),
        UNIQUE KEY email (email),
        KEY document (document),
        KEY status (status)
    ) {$charset};";
    require_once ABSPATH . 'wp-admin/includes/upgrade.php';
    dbDelta( $sql );
}
add_action( 'init', 'sz_onboarding_install', 8 );

// ── Shortcode ─────────────────────────────────────────────────────────────────
function sz_onboarding_render(): string {
    ob_start();
    // Processar envio
    $msg   = '';
    $error = false;
    if ( isset( $_POST['sz_onboarding_nonce'] ) && wp_verify_nonce( $_POST['sz_onboarding_nonce'], 'sz_onboarding_submit' ) ) {
        $nome     = sanitize_text_field( wp_unslash( $_POST['sz_nome'] ?? '' ) );
        $email    = sanitize_email( wp_unslash( $_POST['sz_email'] ?? '' ) );
        $cpf      = sz_onboarding_digits( sanitize_text_field( wp_unslash( $_POST['sz_cpf'] ?? '' ) ) );
        $tel      = sanitize_text_field( wp_unslash( $_POST['sz_telefone'] ?? '' ) );
        $empresa  = sanitize_text_field( wp_unslash( $_POST['sz_empresa'] ?? '' ) );
        $termos   = ! empty( $_POST['sz_termos'] );

        if ( ! $nome || ! $email || ! is_email( $email ) || ! $termos ) {
            $msg = 'Preencha todos os campos obrigatórios e aceite os termos.'; $error = true;
        } elseif ( ! sz_onboarding_validate_cpf( $cpf ) ) {
            $msg = 'Informe um CPF válido.'; $error = true;
        } elseif ( sz_senderzz_account_exists_by_email_or_cpf( $email, $cpf ) ) {
            $msg = 'Este e-mail ou CPF já possui cadastro na Senderzz. Use recuperar senha para acessar sua conta.'; $error = true;
        } else {
            global $wpdb;
            $table = $wpdb->prefix . 'senderzz_onboarding_requests';
            $exists = $wpdb->get_var( $wpdb->prepare( "SELECT id FROM {$table} WHERE email = %s OR document = %s", $email, $cpf ) );
            if ( $exists ) {
                $msg = 'Este e-mail já possui uma solicitação em análise. Aguarde o contato da equipe Senderzz.'; $error = true;
            } else {
                $token = wp_generate_password( 48, false );
                $wpdb->insert( $table, [
                    'nome' => $nome, 'email' => $email, 'document' => $cpf, 'telefone' => $tel,
                    'empresa' => $empresa, 'token' => $token, 'status' => 'pending',
                ], [ '%s','%s','%s','%s','%s','%s','%s' ] );
                $request_id = (int) $wpdb->insert_id;
                if ( $request_id ) {
                    sz_onboarding_notify_admin( $request_id, $nome, $email, $tel, $empresa, $token );
                    $msg = 'Solicitação enviada com sucesso! Nossa equipe entrará em contato em até 1 dia útil.';
                } else {
                    $msg = 'Erro ao registrar. Tente novamente ou entre em contato pelo WhatsApp.'; $error = true;
                }
            }
        }
    }
    ?>
    <div class="sz-ob-wrap">
    <style>
    .sz-ob-wrap{max-width:540px;margin:0 auto;font-family:var(--sz-font)}
    .sz-ob-card{background:#fff;border:1px solid #e5e7eb;border-radius:22px;padding:36px 40px;box-shadow:0 8px 32px rgba(15,23,42,.07)}
    .sz-ob-logo{height:38px;margin-bottom:22px}
    .sz-ob-title{font-size:var(--sz-text-xl);font-weight:700;color:#111827;margin:0 0 6px;letter-spacing:-.02em}
    .sz-ob-sub{color:#6b7280;font-size:var(--sz-text-md);margin:0 0 28px;line-height:1.5}
    .sz-ob-field{margin-bottom:16px}
    .sz-ob-field label{display:block;font-size:var(--sz-text-sm);font-weight:700;color:#6b7280;text-transform:none;letter-spacing:.02em;margin-bottom:6px}
    .sz-ob-field input[type=text],.sz-ob-field input[type=email],.sz-ob-field input[type=tel]{width:100%;box-sizing:border-box;height:44px;border:1.5px solid #e5e7eb;border-radius:13px;padding:0 14px;font-size:var(--sz-text-md);color:#111827;outline:none;transition:border .15s}
    .sz-ob-field input:focus{border-color:#E8650A;box-shadow:0 0 0 4px rgba(232,101,10,.09)}
    .sz-ob-check{display:flex;align-items:flex-start;gap:10px;margin:18px 0 22px;font-size:var(--sz-text-base);color:#374151;line-height:1.45}
    .sz-ob-check input{margin-top:2px;accent-color:#E8650A;flex-shrink:0}
    .sz-ob-btn{display:block;width:100%;height:48px;background:#E8650A;color:#fff;border:0;border-radius:14px;font-size:var(--sz-text-lg);font-weight:700;cursor:pointer;letter-spacing:-.015em;transition:filter .15s}
    .sz-ob-btn:hover{filter:brightness(.92)}
    .sz-ob-msg{padding:14px 18px;border-radius:13px;font-size:var(--sz-text-base);font-weight:700;margin-bottom:20px}
    .sz-ob-msg.ok{background:#f0fdf4;color:#15803d;border:1px solid #bbf7d0}
    .sz-ob-msg.err{background:#fef2f2;color:#991b1b;border:1px solid #fecaca}
    .sz-ob-footer{margin-top:20px;font-size:var(--sz-text-meta);color:#9ca3af;text-align:center}
    </style>
    <?php if ( $msg ) : ?>
        <div class="sz-ob-msg <?php echo $error ? 'err' : 'ok'; ?>"><?php echo esc_html( $msg ); ?></div>
    <?php endif; ?>
    <?php if ( ! $msg || $error ) : ?>
    <div class="sz-ob-card">
        <?php
        $logo = function_exists('senderzz_portal_logo_url') ? senderzz_portal_logo_url() : plugins_url('assets/images/senderzz-logo.png', dirname(__FILE__) . '/senderzz-logistics.php');
        if ( $logo ) : ?><img src="<?php echo esc_url($logo); ?>" class="sz-ob-logo" alt="Senderzz"><?php endif; ?>
        <h2 class="sz-ob-title">Quero ser produtor</h2>
        <p class="sz-ob-sub">Preencha abaixo e nossa equipe entra em contato em até 1 dia útil para configurar sua conta.</p>
        <form method="post">
            <?php wp_nonce_field( 'sz_onboarding_submit', 'sz_onboarding_nonce' ); ?>
            <div class="sz-ob-field">
                <label>Nome completo *</label>
                <input type="text" name="sz_nome" required placeholder="Seu nome" value="<?php echo esc_attr( $_POST['sz_nome'] ?? '' ); ?>">
            </div>
            <div class="sz-ob-field">
                <label>E-mail *</label>
                <input type="email" name="sz_email" required placeholder="seu@email.com" value="<?php echo esc_attr( $_POST['sz_email'] ?? '' ); ?>">
            </div>
            <div class="sz-ob-field">
                <label>CPF *</label>
                <input type="text" name="sz_cpf" required inputmode="numeric" maxlength="14" placeholder="000.000.000-00" value="<?php echo esc_attr( sz_onboarding_format_cpf( sanitize_text_field( wp_unslash( $_POST['sz_cpf'] ?? '' ) ) ) ); ?>">
            </div>
            <div class="sz-ob-field">
                <label>WhatsApp</label>
                <input type="tel" name="sz_telefone" placeholder="(11) 99999-9999" value="<?php echo esc_attr( $_POST['sz_telefone'] ?? '' ); ?>">
            </div>
            <div class="sz-ob-field">
                <label>Empresa / Marca</label>
                <input type="text" name="sz_empresa" placeholder="Nome da sua empresa (opcional)" value="<?php echo esc_attr( $_POST['sz_empresa'] ?? '' ); ?>">
            </div>
            <label class="sz-ob-check">
                <input type="checkbox" name="sz_termos" value="1" <?php checked( ! empty( $_POST['sz_termos'] ) ); ?>>
                Concordo com os <a href="#" style="color:#E8650A">termos de uso</a> e autorizo o contato da equipe Senderzz.
            </label>
            <button type="submit" class="sz-ob-btn">Solicitar acesso</button>
        </form>
        <p class="sz-ob-footer">Dúvidas? <a href="https://wa.me/5511963486603" style="color:#E8650A">Fale pelo WhatsApp</a></p>
    </div>
    <?php endif; ?>
    </div>
    <?php
    return ob_get_clean();
}
add_shortcode( 'senderzz_onboarding', 'sz_onboarding_render' );

// ── Notificação ao admin ───────────────────────────────────────────────────────
function sz_onboarding_notify_admin( int $id, string $nome, string $email, string $tel, string $empresa, string $token ): void {
    $approve_url = add_query_arg( [
        'sz_onboarding_approve' => $id,
        'sz_token'              => $token,
        '_wpnonce'              => wp_create_nonce( 'sz_onboarding_approve_' . $id ),
    ], admin_url( 'admin.php?page=senderzz' ) );

    $body = '<!DOCTYPE html><html><body style="font-family:var(--sz-font);color:#111;padding:24px">'
        . '<h2 style="color:#E8650A">Nova solicitação de produtor</h2>'
        . '<p><strong>Nome:</strong> ' . esc_html( $nome ) . '</p>'
        . '<p><strong>E-mail:</strong> ' . esc_html( $email ) . '</p>'
        . '<p><strong>Telefone:</strong> ' . esc_html( $tel ?: '—' ) . '</p>'
        . '<p><strong>Empresa:</strong> ' . esc_html( $empresa ?: '—' ) . '</p>'
        . '<p style="margin-top:24px"><a href="' . esc_url( $approve_url ) . '" style="background:#E8650A;color:#fff;padding:12px 22px;border-radius:10px;text-decoration:none;font-weight:700">Aprovar e criar conta</a></p>'
        . '<p style="font-size:var(--sz-text-meta);color:#9ca3af;margin-top:16px">Ou acesse WooCommerce → Senderzz → Onboarding para gerenciar.</p>'
        . '</body></html>';

    add_filter( 'wp_mail_content_type', 'sz_onboarding_mail_html_ct' );
    wp_mail( get_option( 'admin_email' ), '📥 Nova solicitação de produtor — ' . $nome, $body );
    remove_filter( 'wp_mail_content_type', 'sz_onboarding_mail_html_ct' );
}

// ── Aprovação pelo admin ───────────────────────────────────────────────────────
add_action( 'admin_init', function (): void {
    if ( empty( $_GET['sz_onboarding_approve'] ) || ! current_user_can( 'manage_woocommerce' ) ) return;
    $id    = absint( $_GET['sz_onboarding_approve'] );
    $token = sanitize_text_field( wp_unslash( $_GET['sz_token'] ?? '' ) );
    if ( ! wp_verify_nonce( $_GET['_wpnonce'] ?? '', 'sz_onboarding_approve_' . $id ) ) return;

    global $wpdb;
    $table = $wpdb->prefix . 'senderzz_onboarding_requests';
    $req   = $wpdb->get_row( $wpdb->prepare( "SELECT * FROM {$table} WHERE id = %d AND token = %s AND status = 'pending'", $id, $token ), ARRAY_A );
    if ( ! $req ) { add_action( 'admin_notices', fn() => print '<div class="notice notice-error"><p>Solicitação não encontrada ou já processada.</p></div>' ); return; }

    // 1. Criar usuário WP.
    $username = sanitize_user( strtolower( explode( '@', $req['email'] )[0] ) . '_sz', true );
    if ( username_exists( $username ) ) $username .= '_' . $id;
    $password = wp_generate_password( 14, true );
    $wp_user_id = wp_insert_user( [
        'user_login' => $username,
        'user_email' => $req['email'],
        'display_name' => $req['nome'],
        'user_pass' => $password,
        'role' => 'customer',
    ] );
    if ( is_wp_error( $wp_user_id ) ) {
        $err = $wp_user_id->get_error_message();
        add_action( 'admin_notices', function() use ($err) { echo '<div class="notice notice-error"><p>Erro ao criar usuário WP: ' . esc_html($err) . '</p></div>'; } );
        return;
    }

    update_user_meta( (int) $wp_user_id, 'sz_document', sz_onboarding_digits( (string) ( $req['document'] ?? '' ) ) );

    // 2. Criar shipping class.
    $class_slug = 'sz-' . sanitize_title( $req['nome'] ) . '-' . $id;
    $term = wp_insert_term( $req['nome'] . ' (Senderzz #' . $id . ')', 'product_shipping_class', [ 'slug' => $class_slug ] );
    $class_id = is_array( $term ) ? (int) $term['term_id'] : 0;

    // 3. Criar usuário no portal Senderzz.
    $portal_user_id = 0;
    if ( class_exists( 'WC_MelhorEnvio\\Portal\\Portal_Auth' ) ) {
        $portal_pass = wp_generate_password( 16, true );
        $insert_ok = $wpdb->insert( $wpdb->prefix . 'senderzz_portal_users', [
            'email'             => $req['email'],
            'password_hash'     => wp_hash_password( $portal_pass ),
            'name'              => $req['nome'],
            'shipping_class_id' => $class_id,
            'status'            => 'active',
            'role'              => 'client',
            'wp_user_id'        => $wp_user_id,
            'created_at'        => current_time( 'mysql', true ),
        ], [ '%s','%s','%s','%d','%s','%s','%d','%s' ] );
        $portal_user_id = $insert_ok ? (int) $wpdb->insert_id : 0;
    }

    // 4. Aplicar markup padrão global para a classe criada.
    if ( $class_id > 0 ) {
        $markup_rules = get_option( 'senderzz_markup_rules', [] );
        if ( ! is_array( $markup_rules ) ) $markup_rules = [];
        // Só seta se ainda não tem regra específica para esta classe.
        if ( ! isset( $markup_rules[ $class_id ] ) ) {
            $default = get_option( 'senderzz_markup_default', [ 'pct' => 20.0, 'fixed' => 3.99 ] );
            $markup_rules[ $class_id ] = [
                'pct'   => (float) ( $default['pct']   ?? 20.0 ),
                'fixed' => (float) ( $default['fixed'] ?? 3.99 ),
            ];
            update_option( 'senderzz_markup_rules', $markup_rules );
        }
    }

    // 5. Marcar como aprovado.
    $wpdb->update( $table, [ 'status' => 'approved', 'approved_at' => current_time( 'mysql', true ) ], [ 'id' => $id ], [ '%s', '%s' ], [ '%d' ] );

    // 6. Enviar e-mail ao produtor com credenciais.
    $portal_url = home_url( '/meus-pedidos/' );
    $body = '<!DOCTYPE html><html><body style="font-family:var(--sz-font);color:#111;padding:24px">'
        . '<h2 style="color:#E8650A">Bem-vindo à Senderzz!</h2>'
        . '<p>Olá, <strong>' . esc_html( $req['nome'] ) . '</strong>! Sua conta foi aprovada.</p>'
        . '<p><strong>Acesso ao portal:</strong> <a href="' . esc_url( $portal_url ) . '">' . esc_url( $portal_url ) . '</a></p>'
        . '<p><strong>E-mail:</strong> ' . esc_html( $req['email'] ) . '</p>'
        . '<p><strong>Senha temporária:</strong> <code>' . esc_html( $portal_pass ?? $password ) . '</code></p>'
        . '<p style="color:#9ca3af;font-size:var(--sz-text-meta)">Troque sua senha no primeiro acesso.</p>'
        . '</body></html>';
    add_filter( 'wp_mail_content_type', 'sz_onboarding_mail_html_ct' );
    wp_mail( $req['email'], '✅ Sua conta Senderzz foi aprovada!', $body );
    remove_filter( 'wp_mail_content_type', 'sz_onboarding_mail_html_ct' );

    add_action( 'admin_notices', function() use ($req, $class_id, $portal_user_id) {
        echo '<div class="notice notice-success"><p>Produtor <strong>' . esc_html($req['nome']) . '</strong> aprovado. Classe criada (ID ' . $class_id . '), portal user ID ' . $portal_user_id . '. Configure o markup em Carteira de Frete → Taxas.</p></div>';
    } );
} );

// ── Aba de onboarding no admin Senderzz ───────────────────────────────────────
add_filter( 'senderzz_admin_tabs', function( array $tabs ): array {
    $tabs['onboarding'] = 'Onboarding';
    return $tabs;
} );

function sz_onboarding_admin_page(): void {
    global $wpdb;
    $table = $wpdb->prefix . 'senderzz_onboarding_requests';
    if ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $table ) ) !== $table ) {
        sz_onboarding_install();
    }
    $requests = $wpdb->get_results( "SELECT * FROM {$table} ORDER BY created_at DESC LIMIT 100", ARRAY_A ) ?: [];
    echo '<h2>Solicitações de onboarding</h2>';
    echo '<p>Shortcode para colocar na página: <code>[senderzz_onboarding]</code></p>';
    if ( ! $requests ) { echo '<p>Nenhuma solicitação ainda.</p>'; return; }
    echo '<table class="widefat striped"><thead><tr><th>ID</th><th>Nome</th><th>E-mail</th><th>Telefone</th><th>Empresa</th><th>Status</th><th>Data</th><th>Ação</th></tr></thead><tbody>';
    foreach ( $requests as $r ) {
        $status_label = [ 'pending' => '⏳ Pendente', 'approved' => '✅ Aprovado', 'rejected' => '❌ Recusado' ][ $r['status'] ] ?? $r['status'];
        $approve_url = $r['status'] === 'pending' ? add_query_arg( [
            'sz_onboarding_approve' => $r['id'],
            'sz_token'              => $r['token'],
            '_wpnonce'              => wp_create_nonce( 'sz_onboarding_approve_' . $r['id'] ),
        ], admin_url( 'admin.php?page=senderzz' ) ) : '';
        echo '<tr>';
        echo '<td>' . esc_html( $r['id'] ) . '</td>';
        echo '<td>' . esc_html( $r['nome'] ) . '</td>';
        echo '<td>' . esc_html( $r['email'] ) . '</td>';
        echo '<td>' . esc_html( $r['telefone'] ?: '—' ) . '</td>';
        echo '<td>' . esc_html( $r['empresa'] ?: '—' ) . '</td>';
        echo '<td>' . esc_html( $status_label ) . '</td>';
        echo '<td>' . esc_html( mysql2date( 'd/m/Y H:i', $r['created_at'] ) ) . '</td>';
        echo '<td>' . ( $approve_url ? '<a href="' . esc_url( $approve_url ) . '" class="button button-primary">Aprovar</a>' : '—' ) . '</td>';
        echo '</tr>';
    }
    echo '</tbody></table>';
}
