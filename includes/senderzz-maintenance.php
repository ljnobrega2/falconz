<?php
/**
 * Senderzz Maintenance Mode
 * Tela de manutenção global com liberação apenas para administradores.
 */

defined( 'ABSPATH' ) || exit;

if ( ! defined( 'SENDERZZ_MAINTENANCE_OPTION' ) ) {
    define( 'SENDERZZ_MAINTENANCE_OPTION', 'senderzz_maintenance_settings' );
}

if ( ! function_exists( 'senderzz_maintenance_defaults' ) ) {
    function senderzz_maintenance_defaults(): array {
        return [
            'enabled'     => '0',
            'return_date' => '',
            'return_time' => '',
            'title'       => 'Estamos ajustando a operação',
            'message'     => 'A plataforma Senderzz está temporariamente em manutenção para melhorias operacionais. Voltaremos em breve.',
        ];
    }
}

if ( ! function_exists( 'senderzz_maintenance_get_settings' ) ) {
    function senderzz_maintenance_get_settings(): array {
        $settings = get_option( SENDERZZ_MAINTENANCE_OPTION, [] );
        if ( ! is_array( $settings ) ) {
            $settings = [];
        }
        return array_merge( senderzz_maintenance_defaults(), $settings );
    }
}

if ( ! function_exists( 'senderzz_maintenance_save_settings' ) ) {
    function senderzz_maintenance_save_settings(): void {
        if ( ! is_admin() || ! current_user_can( 'manage_options' ) ) {
            return;
        }
        if ( empty( $_POST['senderzz_maintenance_action'] ) || $_POST['senderzz_maintenance_action'] !== 'save' ) {
            return;
        }
        check_admin_referer( 'senderzz_maintenance_save', 'senderzz_maintenance_nonce' );

        $settings = [
            'enabled'     => ! empty( $_POST['enabled'] ) ? '1' : '0',
            'return_date' => sanitize_text_field( wp_unslash( $_POST['return_date'] ?? '' ) ),
            'return_time' => sanitize_text_field( wp_unslash( $_POST['return_time'] ?? '' ) ),
            'title'       => sanitize_text_field( wp_unslash( $_POST['title'] ?? '' ) ),
            'message'     => sanitize_textarea_field( wp_unslash( $_POST['message'] ?? '' ) ),
        ];

        if ( $settings['title'] === '' ) {
            $settings['title'] = senderzz_maintenance_defaults()['title'];
        }
        if ( $settings['message'] === '' ) {
            $settings['message'] = senderzz_maintenance_defaults()['message'];
        }

        update_option( SENDERZZ_MAINTENANCE_OPTION, $settings, false );

        wp_safe_redirect( add_query_arg( [ 'page' => 'senderzz', 'tab' => 'manutencao', 'saved' => '1' ], admin_url( 'admin.php' ) ) );
        exit;
    }
    add_action( 'admin_init', 'senderzz_maintenance_save_settings' );
}

if ( ! function_exists( 'senderzz_maintenance_portal_role' ) ) {
    function senderzz_maintenance_portal_role(): string {
        $token = sanitize_text_field( wp_unslash( $_COOKIE['senderzz_portal_session'] ?? '' ) );
        if ( $token === '' ) return '';
        global $wpdb;
        $sess_table = $wpdb->prefix . 'senderzz_portal_sessions';
        $user_table = $wpdb->prefix . 'senderzz_portal_users';
        $role = $wpdb->get_var( $wpdb->prepare(
            "SELECT u.role FROM {$sess_table} s
             JOIN {$user_table} u ON u.id = s.user_id
             WHERE s.token = %s AND s.expires_at > NOW() AND u.status = 'active'
             LIMIT 1",
            $token
        ) );
        return is_string( $role ) ? strtolower( trim( $role ) ) : '';
    }
}

if ( ! function_exists( 'senderzz_maintenance_is_allowed_request' ) ) {
    function senderzz_maintenance_is_allowed_request(): bool {
        if ( current_user_can( 'manage_options' ) ) {
            return true;
        }
        // Operação logística precisa continuar funcionando durante manutenção pública.
        if ( senderzz_maintenance_portal_role() === 'operator' ) {
            return true;
        }
        if ( is_admin() || wp_doing_ajax() || wp_doing_cron() ) {
            return true;
        }
        if ( defined( 'REST_REQUEST' ) && REST_REQUEST ) {
            return true;
        }
        $script = isset( $_SERVER['SCRIPT_NAME'] ) ? basename( sanitize_text_field( wp_unslash( $_SERVER['SCRIPT_NAME'] ) ) ) : '';
        if ( in_array( $script, [ 'wp-login.php', 'wp-cron.php' ], true ) ) {
            return true;
        }
        return false;
    }
}

if ( ! function_exists( 'senderzz_maintenance_return_label' ) ) {
    function senderzz_maintenance_return_label( array $settings ): string {
        $date = trim( (string) ( $settings['return_date'] ?? '' ) );
        $time = trim( (string) ( $settings['return_time'] ?? '' ) );

        if ( $date === '' && $time === '' ) {
            return 'Retorno previsto em breve';
        }

        $label = '';
        if ( $date !== '' ) {
            $ts = strtotime( $date );
            $label .= $ts ? date_i18n( 'd/m/Y', $ts ) : $date;
        }
        if ( $time !== '' ) {
            $label .= ( $label ? ' às ' : '' ) . $time;
        }

        return $label ? 'Retorno previsto: ' . $label : 'Retorno previsto em breve';
    }
}

if ( ! function_exists( 'senderzz_maintenance_render_screen' ) ) {
    function senderzz_maintenance_render_screen(): void {
        $settings = senderzz_maintenance_get_settings();
        $return_label = senderzz_maintenance_return_label( $settings );
        $title = $settings['title'] ?: senderzz_maintenance_defaults()['title'];
        $message = $settings['message'] ?: senderzz_maintenance_defaults()['message'];

        status_header( 503 );
        nocache_headers();
        header( 'Retry-After: 3600' );
        header( 'Content-Type: text/html; charset=' . get_bloginfo( 'charset' ) );
        ?>
<!doctype html>
<html <?php language_attributes(); ?>>
<head>
    <meta charset="<?php bloginfo( 'charset' ); ?>">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <meta name="robots" content="noindex,nofollow">
    <title><?php echo esc_html( $title ); ?> · Senderzz</title>
    <style>
        :root{--sz-orange:#E8650A;--sz-orange-2:#ff7a1a;--sz-bg:#0f172a;--sz-card:#111827;--sz-card-2:#172033;--sz-text:#f8fafc;--sz-soft:#cbd5e1;--sz-border:rgba(255,255,255,.10)}
        *{box-sizing:border-box} body{margin:0;min-height:100vh;font-family:var(--sz-font);background:radial-gradient(circle at top left,rgba(232,101,10,.26),transparent 34%),linear-gradient(135deg,#0b1120 0%,#111827 52%,#111827 100%);color:var(--sz-text);display:flex;align-items:center;justify-content:center;padding:24px}
        .sz-maint-card{width:min(680px,100%);background:linear-gradient(180deg,rgba(23,32,51,.94),rgba(17,24,39,.98));border:1px solid var(--sz-border);border-radius:28px;padding:34px;box-shadow:0 28px 90px rgba(0,0,0,.38);position:relative;overflow:hidden;text-align:left}
        .sz-maint-card:before{content:"";position:absolute;inset:0 0 auto 0;height:5px;background:linear-gradient(90deg,var(--sz-orange),var(--sz-orange-2));}
        .sz-brand{display:flex;align-items:center;gap:12px;margin-bottom:28px}.sz-mark{width:46px;height:46px;border-radius:16px;background:linear-gradient(135deg,var(--sz-orange),var(--sz-orange-2));display:grid;place-items:center;font-weight:700;color:#fff;font-size:var(--sz-text-xl);box-shadow:0 14px 34px rgba(232,101,10,.32)}.sz-brand-text strong{display:block;font-size:var(--sz-text-xl);letter-spacing:-.02em}.sz-brand-text span{display:block;font-size:var(--sz-text-meta);color:var(--sz-soft);margin-top:2px}
        .sz-pill{display:inline-flex;align-items:center;gap:8px;border:1px solid rgba(232,101,10,.32);background:rgba(232,101,10,.12);color:#fed7aa;border-radius:999px;padding:8px 12px;font-size:var(--sz-text-base);font-weight:700;margin-bottom:18px}
        h1{font-size:var(--sz-text-hero);line-height:1.02;margin:0 0 14px;letter-spacing:-.02em}p{font-size:var(--sz-text-lg);line-height:1.65;color:var(--sz-soft);margin:0 0 22px;max-width:590px}.sz-return{background:rgba(255,255,255,.06);border:1px solid var(--sz-border);border-radius:18px;padding:16px 18px;display:flex;gap:12px;align-items:center;color:#fff;font-weight:700}.sz-return span{width:36px;height:36px;border-radius:12px;background:rgba(232,101,10,.16);display:grid;place-items:center;color:#fdba74}.sz-footer{margin-top:24px;font-size:var(--sz-text-meta);color:#94a3b8;display:flex;justify-content:space-between;gap:12px;flex-wrap:wrap}.sz-dot{color:var(--sz-orange)}
        @media(max-width:520px){.sz-maint-card{padding:26px 22px;border-radius:22px}.sz-return{align-items:flex-start}.sz-brand{margin-bottom:22px}}
    </style>
</head>
<body>
    <main class="sz-maint-card" role="main" aria-label="Senderzz em manutenção">
        <div class="sz-brand">
            <div class="sz-mark">S</div>
            <div class="sz-brand-text"><strong>Senderzz</strong><span>Plataforma Hub Logística</span></div>
        </div>
        <div class="sz-pill">● Manutenção programada</div>
        <h1><?php echo esc_html( $title ); ?></h1>
        <p><?php echo nl2br( esc_html( $message ) ); ?></p>
        <div class="sz-return"><span>⏱</span><div><?php echo esc_html( $return_label ); ?></div></div>
        <div class="sz-footer"><div>Estamos preparando uma experiência mais estável.</div><div><span class="sz-dot">●</span> Acesso liberado apenas para administradores e operação logística.</div></div>
    </main>
</body>
</html>
        <?php
        exit;
    }
}

if ( ! function_exists( 'senderzz_maintenance_template_redirect' ) ) {
    function senderzz_maintenance_template_redirect(): void {
        $settings = senderzz_maintenance_get_settings();
        if ( empty( $settings['enabled'] ) || $settings['enabled'] !== '1' ) {
            return;
        }
        if ( senderzz_maintenance_is_allowed_request() ) {
            return;
        }
        senderzz_maintenance_render_screen();
    }
    add_action( 'template_redirect', 'senderzz_maintenance_template_redirect', 0 );
}

if ( ! function_exists( 'senderzz_maintenance_admin_page' ) ) {
    function senderzz_maintenance_admin_page(): void {
        if ( ! current_user_can( 'manage_options' ) ) {
            wp_die( esc_html__( 'Sem permissão.', 'wc-melhor-envio' ) );
        }
        $settings = senderzz_maintenance_get_settings();
        $enabled = $settings['enabled'] === '1';
        ?>
        <div class="sz-tab-content active" style="display:block">
            <?php if ( isset( $_GET['saved'] ) ) : ?>
                <div class="notice notice-success is-dismissible"><p>Modo manutenção atualizado.</p></div>
            <?php endif; ?>
            <div class="sz-section">
                <div class="sz-section-head"><h3>Modo manutenção</h3></div>
                <div class="sz-section-body">
                    <p style="margin-top:0;color:#64748b;max-width:760px">Quando ativado, clientes, produtores, afiliados e motoboys veem a tela de manutenção. Administradores e operador logístico continuam acessando normalmente.</p>
                    <form method="post">
                        <?php wp_nonce_field( 'senderzz_maintenance_save', 'senderzz_maintenance_nonce' ); ?>
                        <input type="hidden" name="senderzz_maintenance_action" value="save">
                        <table class="form-table" role="presentation">
                            <tr>
                                <th scope="row">Ativar manutenção</th>
                                <td>
                                    <label style="display:inline-flex;align-items:center;gap:10px;font-weight:600;color:#111827">
                                        <input type="checkbox" name="enabled" value="1" <?php checked( $enabled ); ?>>
                                        Exibir manutenção para público geral, produtores, afiliados e motoboys
                                    </label>
                                </td>
                            </tr>
                            <tr>
                                <th scope="row"><label for="sz-maint-date">Data prevista de retorno</label></th>
                                <td><input id="sz-maint-date" type="date" name="return_date" value="<?php echo esc_attr( $settings['return_date'] ); ?>" class="regular-text"></td>
                            </tr>
                            <tr>
                                <th scope="row"><label for="sz-maint-time">Horário previsto de retorno</label></th>
                                <td><input id="sz-maint-time" type="time" name="return_time" value="<?php echo esc_attr( $settings['return_time'] ); ?>" class="regular-text"></td>
                            </tr>
                            <tr>
                                <th scope="row"><label for="sz-maint-title">Título da tela</label></th>
                                <td><input id="sz-maint-title" type="text" name="title" value="<?php echo esc_attr( $settings['title'] ); ?>" class="large-text" maxlength="90"></td>
                            </tr>
                            <tr>
                                <th scope="row"><label for="sz-maint-message">Mensagem</label></th>
                                <td><textarea id="sz-maint-message" name="message" class="large-text" rows="4"><?php echo esc_textarea( $settings['message'] ); ?></textarea></td>
                            </tr>
                        </table>
                        <p class="submit"><button type="submit" class="button button-primary" style="background:#E8650A;border-color:#E8650A">Salvar modo manutenção</button></p>
                    </form>
                </div>
            </div>
            <div class="sz-section">
                <div class="sz-section-head"><h3>Prévia da informação exibida</h3></div>
                <div class="sz-section-body">
                    <div style="border-radius:18px;background:#111827;color:#fff;padding:20px;border-top:4px solid #E8650A;max-width:680px">
                        <strong style="font-size:var(--sz-text-xl);display:block;margin-bottom:8px"><?php echo esc_html( $settings['title'] ); ?></strong>
                        <div style="color:#cbd5e1;margin-bottom:14px"><?php echo nl2br( esc_html( $settings['message'] ) ); ?></div>
                        <div style="display:inline-flex;gap:8px;align-items:center;background:rgba(255,255,255,.08);border-radius:999px;padding:8px 12px;font-weight:700">⏱ <?php echo esc_html( senderzz_maintenance_return_label( $settings ) ); ?></div>
                    </div>
                </div>
            </div>
        </div>
        <?php
    }
}
