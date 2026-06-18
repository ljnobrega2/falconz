<?php
/**
 * senderzz-secrets.php — Camada de secrets via variáveis de ambiente
 *
 * Problema resolvido:
 *   tpc_webhook_secret, tpc_jwt_secret e tpc_me_token eram lidos diretamente
 *   de wp_options (texto plano no banco). Qualquer acesso ao banco — backup
 *   não criptografado, phpMyAdmin exposto, plugin vulnerável com leitura de
 *   options — expunha as chaves HMAC usadas para autenticar PIX e o token
 *   OAuth do Melhor Envio.
 *
 * Solução:
 *   Funções wrapper que lêem da variável de ambiente primeiro e só caem para
 *   wp_options como fallback. Sem breaking change: se as envs não estiverem
 *   definidas, tudo funciona exatamente como antes.
 *
 * Como configurar (adicionar em wp-config.php OU no ambiente do servidor):
 *
 *   // wp-config.php — antes do "That's all, stop editing!"
 *   define( 'SENDERZZ_WEBHOOK_SECRET', 'seu-secret-aqui-min-32-chars' );
 *   define( 'SENDERZZ_JWT_SECRET',     'seu-jwt-secret-aqui-min-32-chars' );
 *   define( 'SENDERZZ_ME_TOKEN',       'seu-token-oauth-melhor-envio' );
 *
 *   // Ou via variável de ambiente no servidor (nginx/apache/docker):
 *   SENDERZZ_WEBHOOK_SECRET=seu-secret-aqui
 *   SENDERZZ_JWT_SECRET=seu-jwt-secret-aqui
 *   SENDERZZ_ME_TOKEN=seu-token-oauth-melhor-envio
 *
 * Quando a env está ativa:
 *   - O campo correspondente na tela de configurações fica desabilitado
 *     (mostra asteriscos e instrução para editar wp-config.php)
 *   - O valor do banco NÃO é sobrescrito — a env tem prioridade
 *   - O valor do banco pode ser deixado vazio ou apagado manualmente
 *
 * Ordem de prioridade para cada secret:
 *   1. Constante PHP (define em wp-config.php) — mais comum em WP
 *   2. Variável de ambiente do servidor (getenv)
 *   3. wp_options — fallback para instalações sem configuração de env
 *
 * @version 1.0.0
 * @since   2026-05
 */

defined( 'ABSPATH' ) || exit;
if ( defined( 'SENDERZZ_SECRETS_LOADED' ) ) return;
define( 'SENDERZZ_SECRETS_LOADED', true );

// ════════════════════════════════════════════════════════════════════════════
// GETTERS — cada um tenta constante → getenv → wp_options
// ════════════════════════════════════════════════════════════════════════════

/**
 * Retorna o webhook secret (HMAC PIX).
 * Substitui: get_option( 'tpc_webhook_secret' )
 */
function senderzz_get_webhook_secret(): string {
    if ( defined( 'SENDERZZ_WEBHOOK_SECRET' ) && strlen( SENDERZZ_WEBHOOK_SECRET ) >= 32 ) {
        return (string) SENDERZZ_WEBHOOK_SECRET;
    }
    $env = (string) getenv( 'SENDERZZ_WEBHOOK_SECRET' );
    if ( strlen( $env ) >= 32 ) return $env;

    // Fallback: wp_options — comportamento original
    return (string) get_option( 'tpc_webhook_secret', '' );
}

/**
 * Retorna o JWT secret.
 * Substitui: get_option( 'tpc_jwt_secret' )
 */
function senderzz_get_jwt_secret(): string {
    if ( defined( 'SENDERZZ_JWT_SECRET' ) && strlen( SENDERZZ_JWT_SECRET ) >= 32 ) {
        return (string) SENDERZZ_JWT_SECRET;
    }
    $env = (string) getenv( 'SENDERZZ_JWT_SECRET' );
    if ( strlen( $env ) >= 32 ) return $env;

    return (string) get_option( 'tpc_jwt_secret', '' );
}

/**
 * Retorna o token OAuth do Melhor Envio.
 * Substitui: get_option( 'tpc_me_token' ) e tpc_me_token()
 */
function senderzz_get_me_token(): string {
    if ( defined( 'SENDERZZ_ME_TOKEN' ) && strlen( SENDERZZ_ME_TOKEN ) > 10 ) {
        return (string) SENDERZZ_ME_TOKEN;
    }
    $env = (string) getenv( 'SENDERZZ_ME_TOKEN' );
    if ( strlen( $env ) > 10 ) return $env;

    // Fallback: tpc_me_token() se existir, senão get_option direto
    if ( function_exists( 'tpc_me_token' ) ) {
        return (string) tpc_me_token();
    }
    // Também verifica WooCommerce settings (caminho original do plugin ME)
    $settings = get_option( 'woocommerce_wc-melhor-envio_settings', [] );
    foreach ( [ 'client_secret', 'token', 'access_token' ] as $k ) {
        if ( ! empty( $settings[ $k ] ) ) return (string) $settings[ $k ];
    }
    return (string) get_option( 'tpc_me_token', '' );
}

/**
 * Wrapper unificado de secrets — ponto único para migração off-WP.
 * Em Go, substituir este dispatch por variáveis de config/vault.
 *
 * Chaves suportadas: 'webhook_secret', 'jwt_secret', 'me_token'
 */
if ( ! function_exists( 'senderzz_get_secret' ) ) {
    function senderzz_get_secret( string $key ): string {
        switch ( $key ) {
            case 'webhook_secret': return senderzz_get_webhook_secret();
            case 'jwt_secret':     return senderzz_get_jwt_secret();
            case 'me_token':       return senderzz_get_me_token();
            default:
                // Fallback seguro para options não-secret (config geral)
                return (string) get_option( $key, '' );
        }
    }
}

/**
 * Indica se um secret específico veio de env/constante (não do banco).
 * Usado para desabilitar o campo na UI.
 */
function senderzz_secret_from_env( string $which ): bool {
    switch ( $which ) {
        case 'webhook_secret':
            return ( defined( 'SENDERZZ_WEBHOOK_SECRET' ) && strlen( SENDERZZ_WEBHOOK_SECRET ) >= 32 )
                || strlen( (string) getenv( 'SENDERZZ_WEBHOOK_SECRET' ) ) >= 32;
        case 'jwt_secret':
            return ( defined( 'SENDERZZ_JWT_SECRET' ) && strlen( SENDERZZ_JWT_SECRET ) >= 32 )
                || strlen( (string) getenv( 'SENDERZZ_JWT_SECRET' ) ) >= 32;
        case 'me_token':
            return ( defined( 'SENDERZZ_ME_TOKEN' ) && strlen( SENDERZZ_ME_TOKEN ) > 10 )
                || strlen( (string) getenv( 'SENDERZZ_ME_TOKEN' ) ) > 10;
    }
    return false;
}

// ════════════════════════════════════════════════════════════════════════════
// HOOKS — substituem os get_option() espalhados pelo codebase via filtros WP
//
// O WordPress tem um mecanismo nativo para interceptar get_option():
//   add_filter( 'option_{option_name}', callback )
//
// Isso garante que QUALQUER chamada a get_option('tpc_webhook_secret')
// — mesmo em código legado que não foi atualizado — retorna o valor da env
// quando disponível. Sem precisar tocar em cada arquivo.
// ════════════════════════════════════════════════════════════════════════════

add_filter( 'option_tpc_webhook_secret', function ( $value ) {
    if ( senderzz_secret_from_env( 'webhook_secret' ) ) {
        return senderzz_get_webhook_secret();
    }
    return $value;
}, 1 );

add_filter( 'option_tpc_jwt_secret', function ( $value ) {
    if ( senderzz_secret_from_env( 'jwt_secret' ) ) {
        return senderzz_get_jwt_secret();
    }
    return $value;
}, 1 );

add_filter( 'option_tpc_me_token', function ( $value ) {
    if ( senderzz_secret_from_env( 'me_token' ) ) {
        return senderzz_get_me_token();
    }
    return $value;
}, 1 );

// ════════════════════════════════════════════════════════════════════════════
// BLOQUEIO DE ESCRITA — impede que update_option sobrescreva um secret que
// veio de env. Se o admin tentar salvar o formulário com a env ativa, o
// update_option é silenciosamente ignorado para aquela opção.
// ════════════════════════════════════════════════════════════════════════════

add_filter( 'pre_update_option_tpc_webhook_secret', function ( $new_value, $old_value ) {
    if ( senderzz_secret_from_env( 'webhook_secret' ) ) {
        // Retorna o valor atual — update_option não faz nada quando new == old
        return $old_value;
    }
    return $new_value;
}, 1, 2 );

add_filter( 'pre_update_option_tpc_jwt_secret', function ( $new_value, $old_value ) {
    if ( senderzz_secret_from_env( 'jwt_secret' ) ) {
        return $old_value;
    }
    return $new_value;
}, 1, 2 );

add_filter( 'pre_update_option_tpc_me_token', function ( $new_value, $old_value ) {
    if ( senderzz_secret_from_env( 'me_token' ) ) {
        return $old_value;
    }
    return $new_value;
}, 1, 2 );

// ════════════════════════════════════════════════════════════════════════════
// ADMIN UI — mostra indicador visual e desabilita campos quando env está ativa
// ════════════════════════════════════════════════════════════════════════════

/**
 * Injeta JS no admin que desabilita os campos de secret quando env está ativa.
 * Evita que o admin edite um campo que não tem efeito.
 */
add_action( 'admin_footer', function () {
    if ( ! current_user_can( 'manage_woocommerce' ) ) return;

    $webhook_from_env = senderzz_secret_from_env( 'webhook_secret' );
    $jwt_from_env     = senderzz_secret_from_env( 'jwt_secret' );
    $token_from_env   = senderzz_secret_from_env( 'me_token' );

    if ( ! $webhook_from_env && ! $jwt_from_env && ! $token_from_env ) return;

    $fields = [];
    if ( $webhook_from_env ) $fields[] = '[name="tpc_webhook_secret"]';
    if ( $token_from_env )   $fields[] = '[name="tpc_me_token"]';
    // jwt_secret não tem campo no formulário admin — é gerado automaticamente

    if ( empty( $fields ) ) return;

    $selector = implode( ', ', $fields );
    $msg      = esc_js( 'Configurado via variável de ambiente — edite o wp-config.php para alterar.' );
    ?>
    <script>
    (function () {
        var sel = <?php echo wp_json_encode( $selector ); ?>;
        document.querySelectorAll(sel).forEach(function (el) {
            el.disabled = true;
            el.value    = '••••••••••••••••••••';
            el.title    = <?php echo wp_json_encode( $msg ); ?>;
            el.style.background = '#f0f0f1';
            el.style.color      = '#a7aaad';
            var hint = document.createElement('p');
            hint.className = 'description';
            hint.style.color = '#2271b1';
            hint.innerHTML = '🔒 ' + <?php echo wp_json_encode( $msg ); ?>;
            el.parentNode.insertBefore(hint, el.nextSibling);
        });
    })();
    </script>
    <?php
} );

// ════════════════════════════════════════════════════════════════════════════
// ADMIN NOTICE — aviso único ao ativar, com instrução para migrar para env
// Só aparece se NENHUM dos 3 secrets estiver em env e o banner ainda não
// foi dispensado. Não aparece em instalações novas que já usem env.
// ════════════════════════════════════════════════════════════════════════════

add_action( 'admin_notices', function () {
    if ( ! current_user_can( 'manage_woocommerce' ) ) return;
    if ( get_transient( 'senderzz_env_secrets_notice_dismissed' ) ) return;

    // Se todos os 3 já estão em env, não mostrar nada
    if (
        senderzz_secret_from_env( 'webhook_secret' ) &&
        senderzz_secret_from_env( 'jwt_secret' ) &&
        senderzz_secret_from_env( 'me_token' )
    ) return;

    // Só mostrar em páginas WooCommerce/Senderzz
    $screen = get_current_screen();
    if ( ! $screen || ! str_starts_with( $screen->id, 'woocommerce' ) ) return;

    // Só mostrar uma vez por semana se ignorado
    $dismiss_url = wp_nonce_url(
        admin_url( 'admin-post.php?action=senderzz_dismiss_env_notice' ),
        'sz_dismiss_env_notice'
    );

    $missing = [];
    if ( ! senderzz_secret_from_env( 'webhook_secret' ) ) $missing[] = '<code>SENDERZZ_WEBHOOK_SECRET</code>';
    if ( ! senderzz_secret_from_env( 'jwt_secret' ) )     $missing[] = '<code>SENDERZZ_JWT_SECRET</code>';
    if ( ! senderzz_secret_from_env( 'me_token' ) )       $missing[] = '<code>SENDERZZ_ME_TOKEN</code>';

    ?>
    <div class="notice notice-info" style="border-left-color:#2271b1">
        <p>
            <strong>Senderzz — recomendação de segurança:</strong>
            os secrets <?php echo implode( ', ', $missing ); ?> estão armazenados no banco de dados.
            Mova-os para <code>wp-config.php</code> para que não apareçam em backups ou exports do banco:
        </p>
        <pre style="background:#f0f0f1;padding:10px;border-radius:4px;font-size:var(--sz-text-meta);max-width:680px">// Adicione em wp-config.php antes de "That's all, stop editing!"
define( 'SENDERZZ_WEBHOOK_SECRET', '<?php echo esc_html( get_option( 'tpc_webhook_secret', 'gere-um-secret-aqui' ) ); ?>' );
define( 'SENDERZZ_JWT_SECRET',     '<?php echo esc_html( wp_generate_password( 32, false ) ); /* exemplo */ ?>' );
define( 'SENDERZZ_ME_TOKEN',       '<?php echo esc_html( strlen( get_option( 'tpc_me_token', '' ) ) > 0 ? substr( get_option( 'tpc_me_token', '' ), 0, 8 ) . '...' : 'seu-token-oauth-me' ); ?>' );</pre>
        <p style="font-size:var(--sz-text-meta);color:#666;">
            Após adicionar em wp-config.php, os campos correspondentes na tela de configurações serão
            desabilitados automaticamente e o valor do banco pode ser apagado.
            <a href="<?php echo esc_url( $dismiss_url ); ?>">Ignorar por 30 dias</a>
        </p>
    </div>
    <?php
} );

add_action( 'admin_post_senderzz_dismiss_env_notice', function () {
    if ( ! wp_verify_nonce( $_GET['_wpnonce'] ?? '', 'sz_dismiss_env_notice' ) || ! current_user_can( 'manage_woocommerce' ) ) {
        wp_die( 'Unauthorized', 401 );
    }
    set_transient( 'senderzz_env_secrets_notice_dismissed', 1, 30 * DAY_IN_SECONDS );
    wp_safe_redirect( wp_get_referer() ?: admin_url() );
    exit;
} );

// ════════════════════════════════════════════════════════════════════════════
// SEGURANÇA EXTRA — apaga o valor do banco se a env estiver ativa e o banco
// ainda tiver o secret. Roda uma única vez via transient para não executar
// em todo request. Só apaga se a env estiver configurada e com valor válido.
// ════════════════════════════════════════════════════════════════════════════

add_action( 'admin_init', function () {
    if ( get_transient( 'senderzz_secrets_env_cleanup_done' ) ) return;
    if ( ! current_user_can( 'manage_woocommerce' ) ) return;

    $cleaned = false;

    if ( senderzz_secret_from_env( 'webhook_secret' ) && get_option( 'tpc_webhook_secret', '' ) !== '' ) {
        // Substitui o valor no banco por um placeholder ilegível
        // NÃO apaga — apagar causaria regeneração automática pelo tpc_webhook_ensure_secret()
        update_option( 'tpc_webhook_secret', 'env-managed', false );
        $cleaned = true;
    }

    if ( senderzz_secret_from_env( 'jwt_secret' ) && get_option( 'tpc_jwt_secret', '' ) !== '' ) {
        update_option( 'tpc_jwt_secret', 'env-managed', false );
        $cleaned = true;
    }

    if ( senderzz_secret_from_env( 'me_token' ) && get_option( 'tpc_me_token', '' ) !== '' ) {
        update_option( 'tpc_me_token', 'env-managed', false );
        $cleaned = true;
    }

    if ( $cleaned ) {
        set_transient( 'senderzz_secrets_env_cleanup_done', 1, 30 * DAY_IN_SECONDS );
    }
} );
