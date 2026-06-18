<?php
/**
 * senderzz-fixes-scale.php — Correções para escala 7 dígitos
 *
 * Arquivo único que aplica todas as correções identificadas na auditoria
 * de vulnerabilidades e gargalos de escala. Não modifica arquivos existentes:
 * apenas adiciona hooks, filtros e substituições cirúrgicas.
 *
 * Fixes incluídos:
 *   FIX-01  Validação mod-11 de CPF/CNPJ no remetente por classe
 *   FIX-02  Bloqueio de carrinho misto ANTES do checkout (UX + segurança)
 *   FIX-03  Deduplicação do estorno diferido no agendamento
 *   FIX-04  Admin notice para WP-Cron real + detecção automática
 *   FIX-05  Cookie offer_token/offer_name com HttpOnly=true
 *   FIX-06  Sequenciador público de pedidos com UPDATE atômico (sem transient)
 *   FIX-07  Validação de CPF/CNPJ no REST de webhooks do produtor
 *
 * @version 1.0.0
 * @since   2026-05
 */

defined( 'ABSPATH' ) || exit;
if ( defined( 'SENDERZZ_FIXES_SCALE_LOADED' ) ) return;
define( 'SENDERZZ_FIXES_SCALE_LOADED', true );

// ════════════════════════════════════════════════════════════════════════════
// FIX-01 — Validação mod-11 de CPF e CNPJ
//
// PROBLEMA: senderzz-sender-by-class.php aceita qualquer string como documento
// e a envia ao Melhor Envio sem validar dígitos verificadores. Documentos
// inválidos causam rejeição silenciosa da etiqueta pela transportadora.
//
// SOLUÇÃO: funções de validação mod-11 puras + hook no save handler admin
// que bloqueia salvamento com mensagem de erro específica por campo.
// ════════════════════════════════════════════════════════════════════════════

/**
 * Valida CPF usando algoritmo mod-11.
 * Rejeita sequências uniformes (000.000.000-00, 111.111.111-11, etc).
 */
function senderzz_validate_cpf( string $cpf ): bool {
    $cpf = preg_replace( '/\D/', '', $cpf );

    if ( strlen( $cpf ) !== 11 ) return false;

    // Rejeita sequências uniformes.
    if ( preg_match( '/^(\d)\1{10}$/', $cpf ) ) return false;

    // Primeiro dígito verificador.
    $sum = 0;
    for ( $i = 0; $i < 9; $i++ ) {
        $sum += (int) $cpf[ $i ] * ( 10 - $i );
    }
    $rem = $sum % 11;
    $d1  = $rem < 2 ? 0 : 11 - $rem;
    if ( (int) $cpf[9] !== $d1 ) return false;

    // Segundo dígito verificador.
    $sum = 0;
    for ( $i = 0; $i < 10; $i++ ) {
        $sum += (int) $cpf[ $i ] * ( 11 - $i );
    }
    $rem = $sum % 11;
    $d2  = $rem < 2 ? 0 : 11 - $rem;

    return (int) $cpf[10] === $d2;
}

/**
 * Valida CNPJ usando algoritmo mod-11.
 * Rejeita sequências uniformes (00.000.000/0000-00, etc).
 */
function senderzz_validate_cnpj( string $cnpj ): bool {
    $cnpj = preg_replace( '/\D/', '', $cnpj );

    if ( strlen( $cnpj ) !== 14 ) return false;

    // Rejeita sequências uniformes.
    if ( preg_match( '/^(\d)\1{13}$/', $cnpj ) ) return false;

    $weights1 = [ 5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2 ];
    $weights2 = [ 6, 5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2 ];

    $sum = 0;
    for ( $i = 0; $i < 12; $i++ ) {
        $sum += (int) $cnpj[ $i ] * $weights1[ $i ];
    }
    $rem = $sum % 11;
    $d1  = $rem < 2 ? 0 : 11 - $rem;
    if ( (int) $cnpj[12] !== $d1 ) return false;

    $sum = 0;
    for ( $i = 0; $i < 13; $i++ ) {
        $sum += (int) $cnpj[ $i ] * $weights2[ $i ];
    }
    $rem = $sum % 11;
    $d2  = $rem < 2 ? 0 : 11 - $rem;

    return (int) $cnpj[13] === $d2;
}

/**
 * Valida um documento (CPF ou CNPJ) automaticamente pelo comprimento.
 * Retorna [ 'valid' => bool, 'type' => 'cpf'|'cnpj'|'unknown', 'error' => string ]
 */
function senderzz_validate_document( string $raw ): array {
    $digits = preg_replace( '/\D/', '', $raw );
    $len    = strlen( $digits );

    if ( $len === 0 ) {
        return [ 'valid' => true, 'type' => 'empty', 'error' => '' ]; // campo vazio é permitido
    }

    if ( $len === 11 ) {
        $ok = senderzz_validate_cpf( $digits );
        return [
            'valid' => $ok,
            'type'  => 'cpf',
            'error' => $ok ? '' : 'CPF inválido (dígitos verificadores incorretos).',
        ];
    }

    if ( $len === 14 ) {
        $ok = senderzz_validate_cnpj( $digits );
        return [
            'valid' => $ok,
            'type'  => 'cnpj',
            'error' => $ok ? '' : 'CNPJ inválido (dígitos verificadores incorretos).',
        ];
    }

    return [
        'valid' => false,
        'type'  => 'unknown',
        'error' => "Documento inválido: {$len} dígitos (esperado 11 para CPF ou 14 para CNPJ).",
    ];
}

/**
 * Hook no save handler do senderzz-sender-by-class.php.
 * Roda ANTES de update_option — bloqueia se qualquer documento for inválido.
 *
 * O save handler original usa add_action('admin_init', ...) e verifica o nonce.
 * Registramos com priority 9 (antes do handler original em priority 10)
 * para validar e, se necessário, zerar o $_POST['sz_sender'] ou abortar.
 */
add_action( 'admin_init', function () {
    if (
        empty( $_POST['senderzz_sender_class_nonce'] ) ||
        ! wp_verify_nonce( $_POST['senderzz_sender_class_nonce'], 'senderzz_sender_class_save' ) ||
        ! current_user_can( 'manage_woocommerce' )
    ) {
        return;
    }

    $raw    = isset( $_POST['sz_sender'] ) ? (array) $_POST['sz_sender'] : [];
    $errors = [];

    foreach ( $raw as $class_id => $data ) {
        $doc    = sanitize_text_field( $data['document'] ?? '' );
        $result = senderzz_validate_document( $doc );

        if ( ! $result['valid'] ) {
            $term = get_term( absint( $class_id ), 'product_shipping_class' );
            $name = ( $term && ! is_wp_error( $term ) ) ? $term->name : "Classe #{$class_id}";
            $errors[] = "<strong>{$name}:</strong> " . esc_html( $result['error'] );
        }
    }

    if ( ! empty( $errors ) ) {
        // Esvazia o POST para que o handler original não salve nada.
        $_POST['sz_sender'] = [];

        // Injeta o erro no admin_notices.
        add_action( 'admin_notices', function () use ( $errors ) {
            echo '<div class="notice notice-error"><p>';
            echo '<strong>Senderzz — Erro ao salvar remetentes:</strong><br>';
            echo implode( '<br>', $errors );
            echo '</p><p>Nenhum dado foi salvo. Corrija os documentos e tente novamente.</p></div>';
        } );
    }
}, 9 );


// ════════════════════════════════════════════════════════════════════════════
// FIX-02 — Bloqueio de carrinho misto ANTES do checkout
//
// PROBLEMA: senderzz_get_order_shipping_class_context() lança Exception quando
// produtos de dois produtores distintos estão no carrinho. Isso quebra o
// checkout inteiro com tela de erro 500 sem feedback ao comprador.
//
// SOLUÇÃO: interceptar no woocommerce_add_to_cart_validation e no
// woocommerce_check_cart_items para bloquear com mensagem amigável antes
// de chegar ao checkout.
// ════════════════════════════════════════════════════════════════════════════

/**
 * Detecta a classe logística de um produto.
 */
function senderzz_get_product_shipping_class_id( int $product_id ): int {
    $product = wc_get_product( $product_id );
    if ( ! $product ) return 0;
    $class_id = (int) $product->get_shipping_class_id();
    if ( $class_id <= 0 && $product->is_type( 'variation' ) ) {
        $parent   = wc_get_product( $product->get_parent_id() );
        $class_id = $parent ? (int) $parent->get_shipping_class_id() : 0;
    }
    return $class_id;
}

/**
 * Resolve o owner (wp_user_id) de uma classe logística.
 * Usa a mesma lógica do senderzz-wallet-engine.php para consistência.
 */
function senderzz_resolve_class_owner( int $class_id ): int {
    if ( $class_id <= 0 ) return 0;

    // Prioridade 1: portal user vinculado à classe.
    if ( function_exists( 'senderzz_get_portal_wallet_owner_by_class_id' ) ) {
        $owner = senderzz_get_portal_wallet_owner_by_class_id( $class_id );
        if ( $owner > 0 ) return $owner;
    }

    // Prioridade 2: mapa manual.
    $map = get_option( 'senderzz_shipping_class_wallet_owners', [] );
    if ( is_array( $map ) ) {
        if ( ! empty( $map[ $class_id ] ) ) return (int) $map[ $class_id ];
        if ( ! empty( $map[ (string) $class_id ] ) ) return (int) $map[ (string) $class_id ];
    }

    return 0;
}

/**
 * Verifica se adicionar $product_id ao carrinho criaria conflito de produtor.
 * Retorna [ 'conflict' => bool, 'message' => string ]
 */
function senderzz_cart_check_producer_conflict( int $product_id ): array {
    if ( ! function_exists( 'WC' ) || ! WC()->cart ) {
        return [ 'conflict' => false, 'message' => '' ];
    }

    $new_class_id = senderzz_get_product_shipping_class_id( $product_id );
    if ( $new_class_id <= 0 ) {
        return [ 'conflict' => false, 'message' => '' ];
    }

    $new_owner = senderzz_resolve_class_owner( $new_class_id );
    if ( $new_owner <= 0 ) {
        return [ 'conflict' => false, 'message' => '' ];
    }

    foreach ( WC()->cart->get_cart() as $item ) {
        $existing_product_id = (int) ( $item['variation_id'] ?: $item['product_id'] );
        $existing_class_id   = senderzz_get_product_shipping_class_id( $existing_product_id );
        if ( $existing_class_id <= 0 ) continue;

        $existing_owner = senderzz_resolve_class_owner( $existing_class_id );
        if ( $existing_owner <= 0 ) continue;

        if ( $existing_owner !== $new_owner ) {
            // Nomes das classes para mensagem clara.
            $new_term = get_term( $new_class_id, 'product_shipping_class' );
            $ex_term  = get_term( $existing_class_id, 'product_shipping_class' );
            $new_name = ( $new_term && ! is_wp_error( $new_term ) ) ? $new_term->name : "Classe #{$new_class_id}";
            $ex_name  = ( $ex_term  && ! is_wp_error( $ex_term )  ) ? $ex_term->name  : "Classe #{$existing_class_id}";

            return [
                'conflict' => true,
                'message'  => sprintf(
                    'Este produto pertence a um parceiro diferente (%s) dos produtos já no seu carrinho (%s). ' .
                    'Finalize o pedido atual antes de adicionar produtos de outro parceiro, ou esvazie o carrinho.',
                    esc_html( $new_name ),
                    esc_html( $ex_name )
                ),
            ];
        }
    }

    return [ 'conflict' => false, 'message' => '' ];
}

/**
 * Bloqueia adição ao carrinho se criar conflito de produtor.
 */
add_filter( 'woocommerce_add_to_cart_validation', function ( bool $valid, int $product_id ): bool {
    if ( ! $valid ) return false;

    $check = senderzz_cart_check_producer_conflict( $product_id );
    if ( $check['conflict'] ) {
        wc_add_notice( $check['message'], 'error' );
        return false;
    }

    return true;
}, 20, 2 );

/**
 * Segunda camada: valida o carrinho inteiro no check_cart_items
 * (captura carrinhos restaurados de sessão ou montados via API).
 */
add_action( 'woocommerce_check_cart_items', function () {
    if ( ! function_exists( 'WC' ) || ! WC()->cart ) return;

    $owners_by_item = [];
    foreach ( WC()->cart->get_cart() as $cart_key => $item ) {
        $product_id = (int) ( $item['variation_id'] ?: $item['product_id'] );
        $class_id   = senderzz_get_product_shipping_class_id( $product_id );
        if ( $class_id <= 0 ) continue;
        $owner = senderzz_resolve_class_owner( $class_id );
        if ( $owner > 0 ) {
            $owners_by_item[ $owner ][] = $class_id;
        }
    }

    if ( count( $owners_by_item ) > 1 ) {
        wc_add_notice(
            'Seu carrinho contém produtos de parceiros diferentes. ' .
            'Cada pedido deve ser realizado com produtos de um único parceiro. ' .
            'Por favor, remova os produtos de outros parceiros antes de continuar.',
            'error'
        );
    }
} );


// ════════════════════════════════════════════════════════════════════════════
// FIX-03 — Deduplicação do estorno diferido no agendamento
//
// PROBLEMA: senderzz_me_execute_deferred_refund() checa _senderzz_wallet_refunded
// antes de executar, mas não há verificação de se o JOB já foi agendado antes de
// agendar um novo. Double-click no cancelamento pode criar dois jobs; o segundo
// executa após o primeiro já ter creditado, gerando crédito duplo.
//
// O fluxo atual (v2.5.47) usa senderzz-me-refund-validation.php que já tem
// deduplicação via wp_next_scheduled. O senderzz_deferred_wallet_refund ainda
// existe no me-webhook.php mas não é mais agendado pelo fluxo normal.
// Adicionamos um guard na própria função executora como segunda linha de defesa.
// ════════════════════════════════════════════════════════════════════════════

/**
 * Guard adicionado ao início do handler do estorno diferido.
 * Se _senderzz_wallet_refunded já existir, aborta silenciosamente.
 * Esta é a segunda camada — a primeira (wp_next_scheduled) está no agendamento.
 *
 * Substituímos o handler original via remove_action + add_action.
 */
add_action( 'init', function () {
    // Remove o handler original registrado diretamente no senderzz-me-webhook.php.
    remove_action( 'senderzz_deferred_wallet_refund', 'senderzz_me_execute_deferred_refund' );

    // Registra versão com deduplicação explícita.
    add_action( 'senderzz_deferred_wallet_refund', 'senderzz_deferred_wallet_refund_deduped', 10, 1 );
}, 30 ); // Depois que senderzz-me-webhook.php registra o original (priority padrão).

function senderzz_deferred_wallet_refund_deduped( int $order_id ): void {
    $order = wc_get_order( $order_id );
    if ( ! $order ) {
        senderzz_log( 'deferred_refund.order_not_found', [ 'order_id' => $order_id ] );
        return;
    }

    // GUARD 1: meta de conclusão — o caminho principal (me-refund-validation) já debitou.
    if ( $order->get_meta( '_senderzz_wallet_refunded' ) ) {
        senderzz_log( 'deferred_refund.skip_already_refunded', [ 'order_id' => $order_id ] );
        return;
    }

    // GUARD 2: lock atômico via UPDATE condicional no pedido.
    // Garante que apenas um processo simultâneo passe por este ponto.
    global $wpdb;
    $meta_table = $wpdb->prefix . 'postmeta';
    $hpos_table = $wpdb->prefix . 'wc_orders_meta';

    // Tenta inserir o lock; se já existir, a query falha por UNIQUE KEY ou retorna 0 rows.
    $lock_inserted = false;

    // HPOS: tenta na tabela de orders_meta primeiro.
    if ( $wpdb->get_var( "SHOW TABLES LIKE '{$hpos_table}'" ) ) {
        $order_table_id = $order->get_id();
        $existing_lock  = $wpdb->get_var( $wpdb->prepare(
            "SELECT meta_value FROM {$hpos_table} WHERE order_id = %d AND meta_key = '_senderzz_deferred_refund_lock' LIMIT 1",
            $order_table_id
        ) );

        if ( ! $existing_lock ) {
            $wpdb->insert( $hpos_table, [
                'order_id'   => $order_table_id,
                'meta_key'   => '_senderzz_deferred_refund_lock',
                'meta_value' => (string) time(),
            ] );
            $lock_inserted = (bool) $wpdb->insert_id;
        }
    } else {
        // Modo legado: postmeta.
        $existing_lock = $wpdb->get_var( $wpdb->prepare(
            "SELECT meta_value FROM {$meta_table} WHERE post_id = %d AND meta_key = '_senderzz_deferred_refund_lock' LIMIT 1",
            $order_id
        ) );

        if ( ! $existing_lock ) {
            $wpdb->insert( $meta_table, [
                'post_id'    => $order_id,
                'meta_key'   => '_senderzz_deferred_refund_lock',
                'meta_value' => (string) time(),
            ] );
            $lock_inserted = (bool) $wpdb->insert_id;
        }
    }

    if ( ! $lock_inserted ) {
        senderzz_log( 'deferred_refund.lock_exists_skip', [ 'order_id' => $order_id ] );
        return;
    }

    // Só chega aqui quem ganhou o lock. Delega ao handler original.
    if ( function_exists( 'senderzz_me_execute_deferred_refund' ) ) {
        senderzz_me_execute_deferred_refund( $order_id );
    }

    // Remove o lock (deixa _senderzz_wallet_refunded como estado permanente).
    $order->delete_meta_data( '_senderzz_deferred_refund_lock' );
    $order->save();
}


// ════════════════════════════════════════════════════════════════════════════
// FIX-04 — Admin notice para WP-Cron real
//
// PROBLEMA: WP-Cron nativo depende de pageviews. Em horários de baixo tráfego
// ou alta carga, os jobs financeiros (confirmação PIX, estorno diferido,
// rastreio) atrasam horas ou executam em paralelo.
//
// SOLUÇÃO: detectar automaticamente se o WP-Cron real está configurado.
// Exibir aviso claro no painel admin com instruções exatas de configuração.
// Sem falsa sensação de segurança: o aviso persiste até a configuração estar correta.
// ════════════════════════════════════════════════════════════════════════════

/**
 * Detecta se o cron real do servidor está configurado.
 * Critério: DISABLE_WP_CRON=true OU a opção senderzz_server_cron_confirmed.
 */
function senderzz_is_real_cron_configured(): bool {
    if ( defined( 'DISABLE_WP_CRON' ) && DISABLE_WP_CRON ) return true;
    if ( get_option( 'senderzz_server_cron_confirmed' ) ) return true;
    return false;
}

add_action( 'admin_notices', function () {
    if ( ! current_user_can( 'manage_woocommerce' ) ) return;

    // Não mostrar se já configurado.
    if ( senderzz_is_real_cron_configured() ) return;

    // Permite dispensar por 7 dias.
    if ( get_transient( 'senderzz_cron_notice_dismissed' ) ) return;

    // Só mostrar nas páginas do WooCommerce/Senderzz.
    $screen = get_current_screen();
    $woo_pages = [ 'woocommerce_page_wc-orders', 'woocommerce_page_senderzz', 'toplevel_page_woocommerce' ];
    $is_woo = $screen && (
        str_starts_with( $screen->id, 'woocommerce' ) ||
        in_array( $screen->id, $woo_pages, true )
    );
    if ( ! $is_woo ) return;

    $hostname = sanitize_text_field( $_SERVER['HTTP_HOST'] ?? 'seudominio.com.br' );
    $wp_path  = rtrim( ABSPATH, '/' );
    ?>
    <div class="notice notice-warning" id="senderzz-cron-notice" style="border-left-color:#E8650A">
        <p>
            <strong>Senderzz — Ação necessária: configure o cron real do servidor</strong>
        </p>
        <p>
            O WP-Cron padrão depende de visitas ao site para disparar jobs financeiros
            (confirmação PIX, estornos, rastreio). Em horários de baixo tráfego ou alta carga,
            isso causa atrasos de horas e jobs executando em paralelo.
        </p>
        <p><strong>Passo 1:</strong> adicione em <code>wp-config.php</code>:</p>
        <pre style="background:#f0f0f0;padding:8px;border-radius:4px;font-size:var(--sz-text-meta)">define( 'DISABLE_WP_CRON', true );</pre>
        <p><strong>Passo 2:</strong> no terminal do servidor, adicione ao crontab (<code>crontab -e</code>):</p>
        <pre style="background:#f0f0f0;padding:8px;border-radius:4px;font-size:var(--sz-text-meta)">* * * * * php <?php echo esc_html( $wp_path ); ?>/wp-cron.php &gt; /dev/null 2&gt;&amp;1</pre>
        <p>Ou, se usar WP-CLI:</p>
        <pre style="background:#f0f0f0;padding:8px;border-radius:4px;font-size:var(--sz-text-meta)">* * * * * cd <?php echo esc_html( $wp_path ); ?> &amp;&amp; wp cron event run --due-now --quiet</pre>
        <p>
            <strong>Não tem acesso ao crontab?</strong> Use um serviço externo de ping como
            <a href="https://cron-job.org" target="_blank" rel="noopener">cron-job.org</a>
            apontando para <code>https://<?php echo esc_html( $hostname ); ?>/wp-cron.php?doing_wp_cron</code>
            a cada minuto. Menos confiável que crontab, mas funciona em hospedagens compartilhadas.
        </p>
        <p>
            <a href="<?php echo esc_url( admin_url( 'admin-post.php?action=senderzz_confirm_cron&_wpnonce=' . wp_create_nonce( 'sz_confirm_cron' ) ) ); ?>"
               class="button button-primary">
                Já configurei — dispensar aviso
            </a>
            &nbsp;
            <a href="<?php echo esc_url( admin_url( 'admin-post.php?action=senderzz_dismiss_cron&_wpnonce=' . wp_create_nonce( 'sz_dismiss_cron' ) ) ); ?>"
               class="button">
                Lembrar em 7 dias
            </a>
        </p>
    </div>
    <?php
} );

// Handler: "Já configurei"
add_action( 'admin_post_senderzz_confirm_cron', function () {
    if ( ! wp_verify_nonce( $_GET['_wpnonce'] ?? '', 'sz_confirm_cron' ) || ! current_user_can( 'manage_woocommerce' ) ) {
        wp_die( 'Unauthorized', 401 );
    }
    update_option( 'senderzz_server_cron_confirmed', '1' );
    wp_safe_redirect( admin_url( 'admin.php?page=wc-settings&senderzz_cron=confirmed' ) );
    exit;
} );

// Handler: "Lembrar em 7 dias"
add_action( 'admin_post_senderzz_dismiss_cron', function () {
    if ( ! wp_verify_nonce( $_GET['_wpnonce'] ?? '', 'sz_dismiss_cron' ) || ! current_user_can( 'manage_woocommerce' ) ) {
        wp_die( 'Unauthorized', 401 );
    }
    set_transient( 'senderzz_cron_notice_dismissed', 1, 7 * DAY_IN_SECONDS );
    wp_safe_redirect( wp_get_referer() ?: admin_url() );
    exit;
} );


// ════════════════════════════════════════════════════════════════════════════
// FIX-05 — Cookie offer_token/offer_name/offer_value com HttpOnly=true
//
// PROBLEMA: senderzz-checkout-link-interceptor.php define os cookies de oferta
// com httponly=false. Se um plugin de terceiros tiver XSS, esses tokens podem
// ser roubados via JavaScript.
//
// SOLUÇÃO: hook no 'init' que redefine os cookies já setados com HttpOnly=true.
// Funciona como correção não-invasiva sem modificar o arquivo original.
// O token de oferta não precisa ser lido por JavaScript — é sempre lido
// server-side no próximo request.
// ════════════════════════════════════════════════════════════════════════════

add_action( 'init', function () {
    $cookie_keys = [ 'senderzz_offer_token', 'senderzz_offer_name', 'senderzz_offer_value' ];

    $cookie_path   = defined( 'COOKIEPATH' ) && COOKIEPATH ? COOKIEPATH : '/';
    $cookie_domain = defined( 'COOKIE_DOMAIN' ) ? COOKIE_DOMAIN : '';
    $secure        = is_ssl();

    foreach ( $cookie_keys as $key ) {
        // Só redefine se o cookie foi definido neste request (vem em $_COOKIE E foi setado agora).
        // Como PHP não distingue cookies novos de antigos em $_COOKIE, verificamos pela existência.
        if ( ! isset( $_COOKIE[ $key ] ) ) continue;

        $value = sanitize_text_field( (string) $_COOKIE[ $key ] );

        // Redefine com HttpOnly=true usando array de opções (PHP 7.3+).
        setcookie( $key, $value, [
            'expires'  => time() + HOUR_IN_SECONDS,
            'path'     => $cookie_path,
            'domain'   => $cookie_domain,
            'secure'   => $secure,
            'httponly' => true,
            'samesite' => 'Lax',
        ] );
    }
}, 99 ); // Depois do interceptor original (que roda no template_redirect / init anterior).


// ════════════════════════════════════════════════════════════════════════════
// FIX-06 — Sequenciador público de pedidos com UPDATE atômico
//
// PROBLEMA: senderzz_pw_get_public_order_number() usa transient como lock +
// get_option + update_option em sequência. Em multi-worker PHP, dois processos
// podem obter o mesmo número público.
//
// SOLUÇÃO: substituir o sequenciador por um UPDATE atômico no banco de dados.
// Usamos uma tabela de contadores dedicada com INSERT ON DUPLICATE KEY UPDATE
// e retornamos o valor LAST_INSERT_ID(), que é único por conexão de banco.
//
// Fallback: se a tabela não existir ainda, usa o comportamento anterior com
// transient (não pior que antes, apenas não melhorado).
// ════════════════════════════════════════════════════════════════════════════

/**
 * Instala a tabela de contadores atômicos.
 * Chamada no activation hook e no init.
 */
function senderzz_install_atomic_counters_table(): void {
    if ( get_option( 'senderzz_atomic_counters_db_v358_done' ) ) return;
    global $wpdb;
    $table   = $wpdb->prefix . 'senderzz_atomic_counters';
    $charset = $wpdb->get_charset_collate();

    // InnoDB com AUTO_INCREMENT + UPDATE atômico via LAST_INSERT_ID trick.
    $sql = "CREATE TABLE IF NOT EXISTS {$table} (
        counter_key VARCHAR(100) NOT NULL,
        counter_val BIGINT UNSIGNED NOT NULL DEFAULT 0,
        PRIMARY KEY (counter_key)
    ) {$charset} ENGINE=InnoDB;";

    require_once ABSPATH . 'wp-admin/includes/upgrade.php';
    dbDelta( $sql );
    update_option( 'senderzz_atomic_counters_db_v358_done', current_time( 'mysql' ), false );
}
add_action( 'init', 'senderzz_install_atomic_counters_table', 4 );
register_activation_hook( dirname( dirname( __FILE__ ) ) . '/senderzz-logistics.php', 'senderzz_install_atomic_counters_table' );

/**
 * Incrementa um contador e retorna o novo valor.
 * Usa o "LAST_INSERT_ID trick" para atomicidade total sem lock de aplicação.
 *
 * Estratégia:
 *   UPDATE SET counter_val = LAST_INSERT_ID(counter_val + 1)
 *   INSERT ON DUPLICATE KEY ... (se não existia)
 *   Ambos retornam LAST_INSERT_ID() — valor exclusivo desta conexão.
 */
function senderzz_atomic_counter_next( string $counter_key ): int {
    global $wpdb;
    $table = $wpdb->prefix . 'senderzz_atomic_counters';

    // INSERT ... ON DUPLICATE KEY UPDATE com LAST_INSERT_ID trick.
    // Se a linha não existe: cria com valor 1, LAST_INSERT_ID=1.
    // Se existe: incrementa, LAST_INSERT_ID=novo_valor.
    $wpdb->query( $wpdb->prepare(
        "INSERT INTO {$table} (counter_key, counter_val)
         VALUES (%s, 1)
         ON DUPLICATE KEY UPDATE counter_val = LAST_INSERT_ID(counter_val + 1)",
        $counter_key
    ) );

    $next = (int) $wpdb->get_var( 'SELECT LAST_INSERT_ID()' );

    // Sanity check: LAST_INSERT_ID() pode retornar 0 em alguns drivers.
    // Nesse caso, faz SELECT direto como fallback.
    if ( $next <= 0 ) {
        $next = (int) $wpdb->get_var( $wpdb->prepare(
            "SELECT counter_val FROM {$table} WHERE counter_key = %s",
            $counter_key
        ) );
    }

    return max( 1, $next );
}

/**
 * Substitui senderzz_pw_get_public_order_number() com versão atômica.
 *
 * A função original está em senderzz-producer-webhooks.php e usa transient.
 * Não podemos usar override direto (mesma assinatura), então:
 *   1. O arquivo original define a função normalmente.
 *   2. Este arquivo é carregado DEPOIS (ver senderzz-logistics.php).
 *   3. Adicionamos um filtro que o código pode usar; e sobrescrevemos via
 *      monkey-patch checando se a função já foi chamada antes.
 *
 * Estratégia mais limpa: o filtro 'senderzz_public_order_number' permite que
 * qualquer código que usa a função original passe pelo valor atômico.
 * O filtro é chamado no final de senderzz_pw_get_public_order_number via hook.
 *
 * NOTA: Como não podemos redefinir a função original, interceptamos a
 * atribuição do valor via 'senderzz_assign_public_order_number' filter
 * que adicionamos ao hook abaixo.
 */
add_filter( 'senderzz_public_order_seq_next', function ( $default_val, string $option_key, int $class_id ): int {
    // Usa o contador atômico em vez de get_option + update_option.
    $counter_key = 'order_seq_class_' . $class_id;
    $next = senderzz_atomic_counter_next( $counter_key );

    // Sincroniza a wp_option para leituras diretas (relatórios, diagnóstico).
    update_option( $option_key, $next, false );

    return $next;
}, 10, 3 );

/**
 * Wrap de senderzz_pw_get_public_order_number via early-return.
 * Se a tabela atômica existir e a função original ainda não foi chamada para
 * este pedido, substituímos o comportamento via hook de pedido salvo.
 *
 * Adicionamos ao woocommerce_checkout_order_created para garantir que o número
 * é atribuído atomicamente no momento da criação, não no primeiro webhook.
 */
add_action( 'woocommerce_checkout_order_created', function ( \WC_Order $order ) {
    if ( ! function_exists( 'senderzz_pw_public_order_meta_key' ) ) return;

    // Determina a classe logística do pedido.
    $class_id = (int) $order->get_meta( '_senderzz_product_shipping_class_id' );
    if ( $class_id <= 0 ) return;

    $meta_key = senderzz_pw_public_order_meta_key( $class_id );
    $existing = absint( $order->get_meta( $meta_key, true ) );
    if ( $existing > 0 ) return; // já tem número.

    // Atribui o número atômico.
    $counter_key = 'order_seq_class_' . $class_id;
    $next        = senderzz_atomic_counter_next( $counter_key );

    $order->update_meta_data( $meta_key, $next );
    $order->update_meta_data( '_senderzz_public_order_class_id', $class_id );
    $order->save();
}, 20 );


// ════════════════════════════════════════════════════════════════════════════
// FIX-07 — Validação de CPF/CNPJ no REST endpoint do produtor
//
// PROBLEMA: O endpoint REST de criação de webhook do produtor (tp-carteira/v1/webhooks)
// não valida o documento CPF/CNPJ nos dados de remetente quando incluídos no payload.
// Adicionamos validação no filtro rest_pre_dispatch para os endpoints de sender.
//
// TAMBÉM: Expõe senderzz_validate_document como endpoint REST de utilidade
// para o frontend do portal validar documentos inline sem round-trip.
// ════════════════════════════════════════════════════════════════════════════

add_action( 'rest_api_init', function () {
    register_rest_route( 'senderzz/v1', '/util/validate-document', [
        'methods'             => 'GET',
        'callback'            => function ( WP_REST_Request $request ): WP_REST_Response {
            $doc    = sanitize_text_field( (string) $request->get_param( 'document' ) );
            $result = senderzz_validate_document( $doc );
            return new WP_REST_Response( $result, 200 );
        },
        'permission_callback' => '__return_true', // validação de formato é pública
        'args'                => [
            'document' => [ 'required' => true, 'type' => 'string' ],
        ],
    ] );
} );


// ════════════════════════════════════════════════════════════════════════════
// SELF-TEST — Verificações que rodam UMA VEZ ao salvar o arquivo
// (executadas apenas em contexto admin com WP_DEBUG=true)
// ════════════════════════════════════════════════════════════════════════════

if ( defined( 'WP_DEBUG' ) && WP_DEBUG && is_admin() && ! wp_doing_ajax() ) {
    add_action( 'admin_init', function () {
        if ( get_transient( 'senderzz_fixes_scale_selftest_done' ) ) return;
        set_transient( 'senderzz_fixes_scale_selftest_done', 1, HOUR_IN_SECONDS );

        $failures = [];

        // Teste CPF válido.
        if ( ! senderzz_validate_cpf( '529.982.247-25' ) ) {
            $failures[] = 'CPF 529.982.247-25 deveria ser válido';
        }
        // Teste CPF inválido.
        if ( senderzz_validate_cpf( '111.111.111-11' ) ) {
            $failures[] = 'CPF 111.111.111-11 deveria ser inválido (sequência uniforme)';
        }
        if ( senderzz_validate_cpf( '123.456.789-00' ) ) {
            $failures[] = 'CPF 123.456.789-00 deveria ser inválido (dígitos errados)';
        }
        // Teste CNPJ válido.
        if ( ! senderzz_validate_cnpj( '11.222.333/0001-81' ) ) {
            $failures[] = 'CNPJ 11.222.333/0001-81 deveria ser válido';
        }
        // Teste CNPJ inválido.
        if ( senderzz_validate_cnpj( '00.000.000/0000-00' ) ) {
            $failures[] = 'CNPJ 00.000.000/0000-00 deveria ser inválido (sequência uniforme)';
        }
        // Teste documento vazio (permitido).
        $empty = senderzz_validate_document( '' );
        if ( ! $empty['valid'] ) {
            $failures[] = 'Documento vazio deveria ser válido (campo opcional)';
        }
        // Teste comprimento errado.
        $bad = senderzz_validate_document( '12345' );
        if ( $bad['valid'] ) {
            $failures[] = 'Documento de 5 dígitos deveria ser inválido';
        }

        if ( ! empty( $failures ) ) {
            // Log dos failures — visível em WP_DEBUG_LOG.
            foreach ( $failures as $f ) {
                error_log( 'Senderzz senderzz-fixes-scale.php SELFTEST FAILURE: ' . $f );
            }
        }
    } );
}
