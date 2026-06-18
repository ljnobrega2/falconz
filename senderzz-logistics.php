<?php
/**
 * Plugin Name: Senderzz Logistics - Carteira + Melhor Envio
 * Description: Motor unificado Senderzz: Melhor Envio, carteira pré-paga, PIX, etiquetas, rastreio white-label, webhook de tracking, operador logístico, taxa por classe, analytics de margem e API para painel externo.
 * Plugin URI: https://senderzz.com.br
 * Author: Senderzz
 * Author URI: https://senderzz.com.br
 * Version: 459
 * Text Domain: wc-melhor-envio
 * Domain Path: languages/wc-melhor-envio
 * WC requires at least: 7.0.0
 * WC tested up to:      9.3.1
 */

use WC_MelhorEnvio\Emails\New_Tracking_Code;


// Senderzz admin integrity guard: the platform keeps legacy modules with mixed
// WooCommerce capabilities. A WordPress administrator must be able to open every
// Senderzz administrative/configuration screen without losing access to module tabs.
if ( ! function_exists( 'senderzz_admin_capability_guard' ) ) {
    function senderzz_admin_capability_guard( array $allcaps, array $caps, array $args, WP_User $user ): array {
        if ( ! empty( $allcaps['manage_options'] ) ) {
            foreach ( [
                'manage_woocommerce',
                'view_woocommerce_reports',
                'edit_shop_orders',
                'read_shop_order',
                'senderzz_admin',
                'senderzz_manage',
                'senderzz_manage_motoboy',
                'senderzz_manage_finance',
            ] as $cap ) {
                $allcaps[ $cap ] = true;
            }
        }
        return $allcaps;
    }
    add_filter( 'user_has_cap', 'senderzz_admin_capability_guard', 20, 4 );
}

/* Senderzz Logistics: modulos unificados */
if ( ! defined( 'SENDERZZ_LOGISTICS_VERSION' ) ) { define( 'SENDERZZ_LOGISTICS_VERSION', '2.6.11-v459' ); }
if ( ! defined( 'TPC_VERSION' ) ) {
    define('TPC_VERSION', '2.6.11-v459' );
    define( 'TPC_PATH', plugin_dir_path( __FILE__ ) );
    define( 'TPC_URL', plugin_dir_url( __FILE__ ) );
    define( 'TPC_ME_API', 'https://melhorenvio.com.br/api/v2' );
}


// Normalizador único de nome de produto/kit para evitar combinações duplicadas
// Ex.: "Datalaprox, 4x 3 potes + 1 brinde" => "Datalaprox".
// Quando a quantidade é exibida fora do nome, o caller monta "4x Datalaprox".
if ( ! function_exists( 'senderzz_clean_product_label' ) ) {
    function senderzz_clean_product_label( $name ): string {
        $name = trim( wp_strip_all_tags( (string) $name, true ) );
        if ( $name === '' ) return '';
        $name = preg_replace( '/\s+/u', ' ', $name );
        // A primeira parte antes da vírgula é o produto real. O restante normalmente é a oferta/kit/brinde.
        if ( strpos( $name, ',' ) !== false ) {
            $parts = array_map( 'trim', explode( ',', $name ) );
            if ( ! empty( $parts[0] ) ) $name = $parts[0];
        }
        // Segurança adicional para nomes que chegam sem vírgula, mas com brinde anexado.
        $name = preg_replace( '/\s*\+\s*\d+\s*(brinde|brindes)\b.*$/iu', '', $name );
        return trim( $name );
    }
}

if ( ! function_exists( 'senderzz_clean_product_summary' ) ) {
    /**
     * Limpa resumos já montados/salvos, inclusive legados em banco.
     * Ex.: "3x Datalaprox, 3x 3 Potes Padrão" => "3x Datalaprox".
     * Regra Senderzz: quando houver combinação de nome de produto + kit/brinde,
     * somente a primeira parte (quantidade + produto) é oficial.
     */
    function senderzz_clean_product_summary( $text ): string {
        $text = trim( wp_strip_all_tags( (string) $text, true ) );
        if ( $text === '' ) return '';
        $text = preg_replace( '/\s+/u', ' ', $text );
        if ( strpos( $text, ',' ) !== false ) {
            $parts = array_map( 'trim', explode( ',', $text ) );
            if ( ! empty( $parts[0] ) ) $text = $parts[0];
        }
        $text = preg_replace( '/\s*\+\s*\d+\s*(brinde|brindes)\b.*$/iu', '', $text );
        return trim( $text );
    }
}

if ( ! function_exists( 'senderzz_order_item_label' ) ) {
    function senderzz_order_item_label( $item, bool $with_qty = true ): string {
        if ( ! is_object( $item ) || ! method_exists( $item, 'get_name' ) ) return '';
        $name = senderzz_clean_product_label( $item->get_name() );
        if ( $name === '' ) return '';
        $qty = ( method_exists( $item, 'get_quantity' ) ? max( 1, (int) $item->get_quantity() ) : 1 );
        return ( $with_qty && $qty > 1 ) ? $qty . 'x ' . $name : $name;
    }
}

if ( ! function_exists( 'senderzz_order_primary_item_label' ) ) {
    /**
     * Rótulo OFICIAL de produto para exibição operacional (PWA motoboy, painel OL,
     * portal). Regra Senderzz: "quantidade e nome, uma vez só" — NUNCA lista vários
     * itens separados por vírgula/"+". Retorna apenas o item principal no formato
     * "{qty}x {nome}". O nome do produto que viria depois da vírgula não deve existir.
     */
    function senderzz_order_primary_item_label( $order ): string {
        if ( ! ( $order instanceof WC_Order ) ) return '';
        foreach ( $order->get_items() as $item ) {
            if ( ! ( $item instanceof WC_Order_Item_Product ) ) continue;
            $name = senderzz_clean_product_label( $item->get_name() );
            if ( $name === '' ) continue;
            $qty = max( 1, (int) $item->get_quantity() );
            return $qty . 'x ' . $name;
        }
        return '';
    }
}
if ( ! defined( 'TP_PREFERIDA_PATH' ) ) {
    define( 'TP_PREFERIDA_PATH', plugin_dir_path( __FILE__ ) . 'includes/preferida/' );
    define( 'TP_PREFERIDA_URL', plugin_dir_url( __FILE__ ) );  // aponta para raiz do plugin (assets/ canônico)
    define( 'TP_PREFERIDA_OPTION', 'tp_preferida_map' );
    define( 'SENDERZZ_BLOCKED_OPTION', 'senderzz_blocked_carriers_map' );
}

// ── v-scale: guard de idempotência para woocommerce_order_status_changed ─────
// Com 13 hooks no mesmo action, garante que mudanças de status não disparam
// processamento duplicado em race conditions (ex: dois saves rápidos do mesmo pedido).
// Cada módulo já tem seu próprio guard (static $running), este é a camada externa.

if ( ! function_exists( 'senderzz_status_change_guard' ) ) {
    function senderzz_status_change_guard( int $order_id, string $new_status ): bool {
        $key = 'sz_sc_guard_' . $order_id . '_' . $new_status;
        if ( get_transient( $key ) ) return false; // já processando
        set_transient( $key, 1, 10 ); // 10s — suficiente para o pipeline completar
        return true;
    }
    function senderzz_status_change_guard_clear( int $order_id, string $new_status ): void {
        delete_transient( 'sz_sc_guard_' . $order_id . '_' . $new_status );
    }
}


// ── Hardening seguro: protege diretórios de log sem alterar fluxo do plugin ──
if ( ! function_exists( 'senderzz_ensure_private_runtime_dirs' ) ) {
    function senderzz_ensure_private_runtime_dirs(): void {
        if ( ! defined( 'WP_CONTENT_DIR' ) ) {
            return;
        }

        $dirs = [
            WP_CONTENT_DIR . '/senderzz-logs',
        ];

        foreach ( $dirs as $dir ) {
            if ( ! is_dir( $dir ) ) {
                wp_mkdir_p( $dir );
            }
            if ( is_dir( $dir ) ) {
                $htaccess = $dir . '/.htaccess';
                $index    = $dir . '/index.php';

                if ( ! file_exists( $htaccess ) ) {
                    @file_put_contents(
                        $htaccess,
                        "Options -Indexes\n<IfModule mod_authz_core.c>\nRequire all denied\n</IfModule>\n<IfModule !mod_authz_core.c>\nDeny from all\n</IfModule>\n"
                    );
                }
                if ( ! file_exists( $index ) ) {
                    @file_put_contents( $index, '<?php // Silence is golden.' );
                }
            }
        }
    }
    add_action( 'init', 'senderzz_ensure_private_runtime_dirs', 1 );
}

if ( ! function_exists( 'senderzz_logistics_activate' ) ) {
    function senderzz_logistics_activate() {
        if ( function_exists( 'senderzz_ensure_private_runtime_dirs' ) ) { senderzz_ensure_private_runtime_dirs(); }
        if ( file_exists( TPC_PATH . 'includes/tpc/database.php' ) ) {
            require_once TPC_PATH . 'includes/tpc/database.php';
            if ( function_exists( 'tpc_create_tables' ) ) { tpc_create_tables(); }

        }
        if ( class_exists( '\\WC_MelhorEnvio\\Database\\Label_Table' ) ) { \WC_MelhorEnvio\Database\Label_Table::install(); }
        if ( class_exists( '\\WC_MelhorEnvio\\Portal\\Portal_Auth' ) ) { \WC_MelhorEnvio\Portal\Portal_Auth::install(); }
        if ( ! wp_next_scheduled( 'tpc_cron_verificar_recargas_pix' ) ) { wp_schedule_event( time() + 60, 'tpc_5min', 'tpc_cron_verificar_recargas_pix' ); }
        // Módulo Motoboy
        if ( function_exists( 'sz_motoboy_create_tables' ) ) { sz_motoboy_create_tables(); }
        if ( function_exists( 'sz_aff_install' ) ) { sz_aff_install(); }
        if ( function_exists( 'sz_aff_handle_invite_route' ) ) { sz_aff_handle_invite_route(); }
        flush_rewrite_rules();
    }
    register_activation_hook( __FILE__, 'senderzz_logistics_activate' );
}
if ( ! function_exists( 'senderzz_logistics_deactivate' ) ) {
    function senderzz_logistics_deactivate() {
        wp_clear_scheduled_hook( 'tpc_cron_verificar_recargas_pix' );
        wp_clear_scheduled_hook( 'senderzz_db_cleanup' );
        wp_clear_scheduled_hook( 'sz_aff_release_commissions' );
    }
    register_deactivation_hook( __FILE__, 'senderzz_logistics_deactivate' );
}
add_action( 'init', function() {
    if ( defined( 'TPC_VERSION' ) && get_option( 'tpc_db_version' ) !== TPC_VERSION && function_exists( 'tpc_create_tables' ) ) {
        tpc_create_tables();
    }
    if ( defined( 'SZ_MOTOBOY_DB_VERSION' ) && get_option( 'sz_motoboy_db_version' ) !== SZ_MOTOBOY_DB_VERSION && function_exists( 'sz_motoboy_create_tables' ) ) {
        sz_motoboy_create_tables();
    }
}, 5 );

// ── Cron semanal de limpeza de tabelas de log/sessão ──────────────────────────
if ( ! wp_next_scheduled( 'senderzz_db_cleanup' ) ) {
    wp_schedule_event( time() + HOUR_IN_SECONDS, 'daily', 'senderzz_db_cleanup' );
}
add_action( 'senderzz_db_cleanup', function() {
    global $wpdb;

    // 1. Sessões expiradas do portal (senderzz_portal_sessions)
    $wpdb->query( "DELETE FROM {$wpdb->prefix}senderzz_portal_sessions WHERE expires_at < NOW()" );

    // 2. Códigos 2FA expirados (senderzz_portal_2fa)
    $wpdb->query( "DELETE FROM {$wpdb->prefix}senderzz_portal_2fa WHERE expires_at < DATE_SUB(NOW(), INTERVAL 1 DAY)" );

    // 3. Eventos de webhook com mais de 90 dias (tpc_webhook_events)
    $wpdb->query( "DELETE FROM {$wpdb->prefix}tpc_webhook_events WHERE created_at < DATE_SUB(NOW(), INTERVAL 90 DAY) LIMIT 5000" );

    // 4. Log de webhooks de produtor com mais de 90 dias (senderzz_webhook_log)
    $wpdb->query( "DELETE FROM {$wpdb->prefix}senderzz_webhook_log WHERE fired_at < DATE_SUB(NOW(), INTERVAL 90 DAY) LIMIT 5000" );

    // 5. Recargas PIX expiradas há mais de 30 dias (tpc_recargas)
    $wpdb->query( "DELETE FROM {$wpdb->prefix}tpc_recargas WHERE status IN ('expirado','cancelado') AND created_at < DATE_SUB(NOW(), INTERVAL 30 DAY) LIMIT 2000" );

    // v-scale: webhook events > 60 dias e sessions expiradas com LIMIT
    $wpdb->query( "DELETE FROM {$wpdb->prefix}tpc_webhook_events WHERE created_at < DATE_SUB(NOW(), INTERVAL 60 DAY) LIMIT 10000" );
    $wpdb->query( "DELETE FROM {$wpdb->prefix}senderzz_webhook_log WHERE fired_at < DATE_SUB(NOW(), INTERVAL 60 DAY) LIMIT 10000" );
    $wpdb->query( "DELETE FROM {$wpdb->prefix}senderzz_portal_sessions WHERE expires_at < NOW() LIMIT 2000" );

    if ( function_exists( 'senderzz_log' ) ) {
        senderzz_log( 'db_cleanup', [ 'ran_at' => current_time( 'mysql' ) ] );
    }
} );

// ── v-scale: índices de banco para escala com 30+ produtores ────────────────
// Garante que a meta_query por _senderzz_product_shipping_class_id não faça
// full scan em postmeta/wc_orders_meta. Executado uma vez e cacheado em option.

add_action( 'init', function (): void {
    $ver = '2'; // incrementar se precisar recriar
    if ( get_option( 'senderzz_db_indexes_v' ) === $ver ) return;

    global $wpdb;
    $errors = [];

    // 1. Índice composto em wp_postmeta (WC legado)
    $idx_exists = $wpdb->get_var(
        "SELECT COUNT(1) FROM information_schema.statistics
         WHERE table_schema = DATABASE()
           AND table_name   = '{$wpdb->postmeta}'
           AND index_name   = 'sz_class_id_idx'"
    );
    if ( ! $idx_exists ) {
        $r = $wpdb->query(
            "ALTER TABLE {$wpdb->postmeta}
             ADD INDEX sz_class_id_idx (meta_key(32), meta_value(16))"
        );
        if ( $r === false ) $errors[] = 'postmeta index: ' . $wpdb->last_error;
    }

    // 2. Índice em wc_orders_meta (HPOS)
    $hpos_table = $wpdb->prefix . 'wc_orders_meta';
    $hpos_exists_tbl = $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $hpos_table ) );
    if ( $hpos_exists_tbl ) {
        $hpos_idx = $wpdb->get_var(
            "SELECT COUNT(1) FROM information_schema.statistics
             WHERE table_schema = DATABASE()
               AND table_name   = '{$hpos_table}'
               AND index_name   = 'sz_class_id_hpos_idx'"
        );
        if ( ! $hpos_idx ) {
            $r = $wpdb->query(
                "ALTER TABLE {$hpos_table}
                 ADD INDEX sz_class_id_hpos_idx (meta_key(32), meta_value(16))"
            );
            if ( $r === false ) $errors[] = 'hpos index: ' . $wpdb->last_error;
        }
    }

    // 3. Índice em senderzz_portal_users por shipping_class_id (lookup frequente)
    $pu_table = $wpdb->prefix . 'senderzz_portal_users';
    $pu_exists = $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $pu_table ) );
    if ( $pu_exists ) {
        $pu_idx = $wpdb->get_var(
            "SELECT COUNT(1) FROM information_schema.statistics
             WHERE table_schema = DATABASE()
               AND table_name   = '{$pu_table}'
               AND index_name   = 'sz_class_status_idx'"
        );
        if ( ! $pu_idx ) {
            $wpdb->query(
                "ALTER TABLE {$pu_table}
                 ADD INDEX sz_class_status_idx (shipping_class_id, status)"
            );
        }
    }

    if ( empty( $errors ) ) {
        update_option( 'senderzz_db_indexes_v', $ver );
    }
    if ( function_exists( 'senderzz_log' ) ) {
        senderzz_log( 'db_indexes', [ 'version' => $ver, 'errors' => $errors ] );
    }
}, 20 );
// Portal core classes usadas por templates/AJAX — carregar antes do shortcode renderizar
foreach ( array(
    'src/Portal/Portal_Orders.php',
) as $senderzz_portal_core_file ) {
    $senderzz_portal_core_path = plugin_dir_path( __FILE__ ) . $senderzz_portal_core_file;
    if ( file_exists( $senderzz_portal_core_path ) ) { require_once $senderzz_portal_core_path; }
}

// Portal helpers — funções livres extraídas de Portal_Page
// Carregado antes do autoload para estar disponível quando Portal_Page instanciar
if ( file_exists( plugin_dir_path( __FILE__ ) . 'includes/portal/sz-banner.php' ) ) {
    require_once plugin_dir_path( __FILE__ ) . 'includes/portal/sz-banner.php';
}
if ( file_exists( plugin_dir_path( __FILE__ ) . 'includes/portal/portal-helpers.php' ) ) {
    require_once plugin_dir_path( __FILE__ ) . 'includes/portal/portal-helpers.php';
}

foreach ( array(
    // ── Secrets via env — DEVE ser o primeiro include ──
    'includes/senderzz-secrets.php',                          // ← env vars para webhook_secret, jwt_secret, me_token
    // ── Patches Tier 2 (segurança) ──
    'includes/senderzz-security-patches.php',                 // ← patches de segurança consolidados
    // ── Camada canônica de order meta (carrega antes de tudo que lê/escreve meta) ──
    'includes/class-senderzz-order-meta.php',
    // ── Centralização segura (somente leitura): status, contexto e auditoria ──
    'includes/senderzz-status.php',
    'includes/senderzz-woo-status-authority.php',
    'includes/senderzz-order-context.php',
    'includes/senderzz-audit-engine.php',

    'includes/tpc/database.php',
    'includes/tpc/doublewrite.php',  // Fase 2 migração Go — remover após cutover
    'includes/tpc/wallet.php',
    'includes/core-functions.php',
    'includes/tpc/pix.php',
    'includes/tpc/pix-confirmation-guard.php',                // ← V-NEW-01/02: fail-closed PIX (DEPOIS do pix.php)
    'includes/tpc/pix-auto-reconcile.php',                    // ← V-NEW-AUTO: cron reconciliação automática via saldo ME
    'includes/tpc/webhook.php',
    'includes/tpc/rest-api.php',
    'includes/tpc/admin.php',
    'includes/tpc/my-account.php',
    'includes/tpc/painel.php',
    'includes/preferida/admin-page.php',
    'includes/preferida/checkout-hooks.php',
    'includes/preferida/blocked-page.php',
    'includes/senderzz-engine.php',
    'includes/senderzz-multi-class.php',       // ← suporte multi-class por usuário
    'includes/senderzz-wallet-engine.php',    // ← funções wallet separadas do engine
    // ── Sistema de Afiliados COD (Etapa 2) ──
    'includes/senderzz-affiliates.php',
    'includes/senderzz-affiliate-classes.php', // ← gestão de classes por afiliado + auto-vinculação
    'includes/senderzz-access-scope.php', // ← blindagem central de escopo/permissão de pedidos
    'includes/senderzz-cod-wallet.php',
    'includes/senderzz-notifications.php',
    'includes/senderzz-rest.php',
    'includes/senderzz-cancel-intercept.php',
    'includes/senderzz-me-cancel-label.php',
    'includes/senderzz-me-webhook.php',
    'includes/senderzz-me-refund-validation.php',
    // v2.4 — novos módulos
    'includes/senderzz-markup.php',
    'includes/operator/operator.php',
    'includes/senderzz-utils.php',
    'includes/senderzz-stock-shipments.php',
    'includes/senderzz-producer-webhooks.php',
    'includes/senderzz-integrations.php',
    'includes/senderzz-checkout-link-legacy-repair.php',
    'includes/senderzz-checkout-link-interceptor.php',
    // ── REQ6: recomposição de estoque em cancelamento/exclusão + regras de cancelamento ──
    'includes/senderzz-cancel-restock.php',
    // ── Página de obrigado Senderzz para Woo/FunnelKit ──
    'includes/senderzz-thank-you-page.php',
    // v2.5.20 — router público, 404 limpo e ocultação de sitemap
    'includes/senderzz-router-404-sitemap-unificado.php',

    // ── V-NEW-05: rate limit + 2FA obrigatório operator ──
    	// ── V-NEW-06/07/09/11/13: idempotência DB, cross-check, rate limits ──
    	    // ── Valor declarado por transportadora (cotação + etiqueta) ──
    'includes/senderzz-declared-value.php',
    // ── V-NEW-16/21/25/27: SSRF AJAX, rate limits PIX/senha ──
    	    // ── Tier 4: correções de auditoria ──
        // ── Remetente por classe de entrega ──
    'includes/senderzz-sender-by-class.php',
    // ── HPOS fix: rastreio em wp_wc_orders_meta ──
    'includes/senderzz-hpos-fix.php',
    // ── Marca por classe na página de rastreamento ──
    'includes/senderzz-tracking-brand.php',
	    // ── Aprovação assíncrona de etiquetas ──
    'includes/senderzz-async-approval.php',
    // ── Notificações por e-mail ao produtor ──
    'includes/senderzz-producer-notifications.php',
    // ── Audit log de ações do produtor ──
    'includes/senderzz-audit-log.php',
    // ── Fixes de escala 7 dígitos (carregado por último para sobrescrever hooks) ──
    'includes/senderzz-fixes-scale.php',
    // ── Onboarding self-service de produtores ──
    'includes/senderzz-onboarding.php',
    // ── PWA do produtor ──
    'includes/senderzz-producer-pwa.php',
    'includes/senderzz-app-pwa.php',         // App PWA leve: rota /app/ isolada
    'includes/senderzz-maintenance.php',
    // ── Módulo Motoboy ──
    'includes/motoboy/delivery-regions.php',
    'includes/motoboy/database.php',
    'includes/senderzz-motoboy-custody.php',
    'includes/motoboy/doublewrite.php',  // Fase 1 migração Go — remover após cutover
    'includes/motoboy/router.php',
    'includes/motoboy/rest-api.php',
    'includes/motoboy/admin.php',
    'includes/motoboy/order-metabox.php',
    'includes/motoboy/relatorios.php',
    'includes/motoboy/routes.php',
    'includes/motoboy/motoboy-producer.php',
    'includes/motoboy/checkout-link-motoboy.php',
    'includes/motoboy/wc-translations-ptbr.php',
    'includes/motoboy/motoboy-wallet.php',
    // ── Captura de UTM e integração UTMify ──
    'includes/senderzz-utm.php',
) as $senderzz_file ) {
    $senderzz_path = plugin_dir_path( __FILE__ ) . $senderzz_file;
    if ( file_exists( $senderzz_path ) ) { require_once $senderzz_path; }
}

class WC_Melhor_Envio {
	public const VERSION = '2.2.7-portal-premium';
	/**
	 * Instance of this class.
	 *
	 * @var object
	 */
	protected static $instance = null;

	/**
	 * Initialize the plugin public actions.
	 */
	function __construct() {
		if ( ! defined( 'WC_ABSPATH' ) ) {
			return;
		}

		$this->includes();

		/*
		* Shipping Features
		*/
		add_filter( 'woocommerce_shipping_methods', [ $this, 'add_shipping_methods' ] );
		add_filter( 'woocommerce_integrations', [ $this, 'add_integration_page' ] );

		add_filter( 'woocommerce_email_classes', [ $this, 'include_emails' ] );

		// add_filter('option_woocommerce_shipping_debug_mode', function() {
		//   return 'yes';
		// });

		new WC_MelhorEnvio\Methods\Delivery_Time();


		/*
		* Admin features
		*/
		// orders related
		new WC_MelhorEnvio\Admin\Orders\Actions();
		new WC_MelhorEnvio\Admin\Orders\Ajax();
		$me_admin_hooks = new WC_MelhorEnvio\Admin\Orders\Metabox();
		$me_admin_hooks->add_hooks();

		// WC custom tools
		new WC_MelhorEnvio\Admin\Tools();


		// Background Jobs
		new WC_MelhorEnvio\Queue\Register();


		/*
		* Request/Print Labels
		*/
		new WC_MelhorEnvio\Labels\Request();


		/*
		* My Account Page
		*/
		new WC_MelhorEnvio\My_Account\Tracking_Codes();

		/*
		 * Automatic features
		 */
		new WC_MelhorEnvio\Admin\Bulk_Actions\Cart\Add_To_Cart();
		new WC_MelhorEnvio\Admin\Bulk_Actions\Cart\Auto_Fulfillment();

		/*
		 * Bulk action de pipeline em lote
		 */
		new WC_MelhorEnvio\Admin\Bulk_Actions\Bulk_Pipeline();

		/*
		 * REST API / Painel
		 */
		new WC_MelhorEnvio\Rest\Labels();

		/*
		 * Tabela própria — instala/atualiza se necessário
		 */
		WC_MelhorEnvio\Database\Label_Table::install();
		/*
		 * Portal do cliente
		 */
		WC_MelhorEnvio\Portal\Portal_Auth::install();
		new WC_MelhorEnvio\Portal\Portal_Page();
		new WC_MelhorEnvio\Portal\Portal_Admin();

		/*
		 * v2.4 — Webhook de rastreio (entrada ME + saída por OL)
		 */
		new WC_MelhorEnvio\Webhook\Tracking_Webhook();

		/*
		 * v2.4 — Dashboard de margem e analytics
		 */
		new WC_MelhorEnvio\Analytics\Margin_Dashboard();

		/*
		 * v2.5 — Menu unificado Senderzz
		 */
		new WC_MelhorEnvio\Admin\Unified_Menu();
	}

	/*
	 * Add shipping method to WooCommerce
	 */

	public function includes() {
		$file = __DIR__ . '/vendor/autoload.php';

		if ( file_exists( $file ) ) {
			require_once $file;
		}

		// Autoload interno do plugin.
		// O composer deste ZIP carrega apenas bibliotecas externas; sem este fallback,
		// classes de src/ como WC_MelhorEnvio\Methods\Delivery_Time geram fatal error.
		static $senderzz_autoload_registered = false;
		if ( ! $senderzz_autoload_registered ) {
			$senderzz_autoload_registered = true;
			spl_autoload_register( function ( $class ) {
				$prefix = 'WC_MelhorEnvio\\';
				if ( 0 !== strpos( $class, $prefix ) ) {
					return;
				}

				$relative = substr( $class, strlen( $prefix ) );
				// Guard: bloqueia path traversal em nomes de classe (ex: Foo\..\..\evil)
				if ( str_contains( $relative, '..' ) || str_contains( $relative, '/' ) ) return;
				$path     = __DIR__ . '/src/' . str_replace( '\\', '/', $relative ) . '.php';

				if ( file_exists( $path ) ) {
					require_once $path;
				}
			} );
		}
	}

	/**
	 * Return an instance of this class.
	 *
	 * @return object A single instance of this class.
	 */
	public static function get_instance() {
		// If the single instance hasn't been set, set it now.
		if ( null == self::$instance ) {
			self::$instance = new self;
		}

		return self::$instance;
	}

	/**
	 * Get main file.
	 *
	 * @return string
	 */
	public static function get_main_file() {
		return __FILE__;
	}

	/**
	 * Get the plugin url.
	 * @return string
	 */
	public static function plugin_url() {
		return untrailingslashit( plugins_url( '/', __FILE__ ) );
	}

	/**
	 * Get the plugin dir url.
	 * @return string
	 */
	public static function plugin_dir_url() {
		return plugin_dir_url( __FILE__ );
	}

	/**
	 * Get templates path.
	 *
	 * @return string
	 */
	public static function get_templates_path() {
		return self::get_plugin_path() . 'templates/';
	}

	/**
	 * Get plugin path.
	 *
	 * @return string
	 */

	public static function get_plugin_path() {
		return plugin_dir_path( __FILE__ );
	}

	/**
	 * Get plugin path.
	 *
	 * @return string
	 */

	public static function get_plugin_basename() {
		return plugin_basename( __FILE__ );
	}

	public function add_shipping_methods( $methods ) {
		$methods['melhor_envio'] = '\WC_MelhorEnvio\Methods\Method';

		return $methods;
	}

	public function add_integration_page( $integrations ) {
		$integrations[] = '\WC_MelhorEnvio\Admin\General_Settings';

		return $integrations;
	}

	/**
	 * Include emails.
	 *
	 * @param  array  $emails  Default emails.
	 *
	 * @return array
	 */
	public function include_emails( $emails ) {
		$emails['melhor_envio_tracking_code'] = new New_Tracking_Code();

		return $emails;
	}
}

add_action( 'plugins_loaded', [ 'WC_Melhor_Envio', 'get_instance' ], 15 );

// Registra hook de normalização retroativa de order meta (ação manual/WP-CLI)
add_action( 'plugins_loaded', static function () {
    if ( class_exists( 'Senderzz_Order_Meta' ) ) {
        Senderzz_Order_Meta::register_normalization_hook();
    }
}, 20 );

// erp integration!
add_filter( 'woocommerce_shipping_rate_method_id', 'melhor_envio_rate_method_id', 80, 2 );
function melhor_envio_rate_method_id( $rate_id, $rate ) {
	if ( false !== strpos( $rate_id, 'melhor_envio' ) ) {
		$rate_id = $rate->get_id() . ':' . $rate->get_instance_id();
	}

	return $rate_id;
}


add_action(
	'before_woocommerce_init',
	function () {
		if ( class_exists( '\Automattic\WooCommerce\Utilities\FeaturesUtil' ) ) {
			\Automattic\WooCommerce\Utilities\FeaturesUtil::declare_compatibility( 'custom_order_tables', __FILE__,
				true );
		}
	}
);
add_filter( 'woocommerce_package_rates', function( $rates, $package ) {
    uasort( $rates, function( $a, $b ) {
        return $a->cost <=> $b->cost;
    });
    return $rates;
}, 20, 2 );


/**
 * Registro dos status customizados do Senderzz.
 * Garante que o WooCommerce reconheça os status independente de plugin externo.
 */
add_action( 'init', function() {
	$statuses = [
		'aprovado'          => [ 'label' => 'Aprovado',            'color' => '#16a34a' ],
		'asuspender'        => [ 'label' => 'A Suspender',         'color' => '#dc2626' ],
		'separado'          => [ 'label' => 'Separado',            'color' => '#E8650A' ],
		'embalado'          => [ 'label' => 'Embalado',            'color' => '#8b5cf6' ],
		'emretirada'        => [ 'label' => 'Em Retirada',         'color' => '#7c3aed' ],
		'acaminho'          => [ 'label' => 'A Caminho',           'color' => '#2563eb' ],
		'coletado'          => [ 'label' => 'Coletado',            'color' => '#16a34a' ],
		'enviado'           => [ 'label' => 'Enviado',             'color' => '#0891b2' ],
		'saldoinsuficiente' => [ 'label' => 'Saldo Insuficiente',  'color' => '#d97706' ],
		'extravio'          => [ 'label' => 'Extravio',            'color' => '#991b1b' ],
		'emcancelamento'    => [ 'label' => 'Em Cancelamento',       'color' => '#991b1b' ],
	];

	foreach ( $statuses as $slug => $data ) {
		if ( ! function_exists( 'wc_register_order_status' ) ) break;
		wc_register_order_status( 'wc-' . $slug, [
			'label'                     => $data['label'],
			'public'                    => false,
			'exclude_from_search'       => false,
			'show_in_admin_all_list'    => true,
			'show_in_admin_status_list' => true,
			'label_count'               => _n_noop( $data['label'] . ' <span class="count">(%s)</span>', $data['label'] . ' <span class="count">(%s)</span>' ),
		] );
	}
}, 10 );

add_filter( 'wc_order_statuses', function( $statuses ) {
	$custom = [
		'wc-aprovado'          => 'Aprovado',
		'wc-asuspender'        => 'A Suspender',
		'wc-separado'          => 'Separado',
		'wc-embalado'          => 'Embalado',
		'wc-emretirada'        => 'Em Retirada',
		'wc-acaminho'          => 'A Caminho',
		'wc-coletado'          => 'Coletado',
		'wc-enviado'           => 'Enviado',
		'wc-saldoinsuficiente' => 'Saldo Insuficiente',
		'wc-extravio'          => 'Extravio',
		'wc-emcancelamento'        => 'Em Cancelamento',
	];
	return array_merge( $statuses, $custom );
} );



define( 'SENDERZZ_KIT_STOCK_OPTION', 'senderzz_kit_stock_map_v1' );
if ( ! defined( 'SENDERZZ_KIT_STOCK_SETTINGS_OPTION' ) ) {
    define( 'SENDERZZ_KIT_STOCK_SETTINGS_OPTION', 'senderzz_kit_stock_status_settings_v1' );
}

/**
 * Compatibilidade com códigos antigos.
 * Retorna o mapa salvo pelo painel.
 */
if ( ! function_exists( 'senderzz_get_kits' ) ) {
    function senderzz_get_kits(): array {
        $kits = get_option( SENDERZZ_KIT_STOCK_OPTION, [] );
        return is_array( $kits ) ? $kits : [];
    }
}

/**
 * Configura os status que debitam e creditam estoque dos kits.
 * Slugs são salvos sem "wc-". Ex.: wc-aprovado => aprovado.
 */
function senderzz_kits_default_stock_status_settings(): array {
    return [
        'debit_statuses'  => [ 'aprovado' ],
        'credit_statuses' => [ 'devolvido', 'cancelled' ],
    ];
}

function senderzz_kits_clean_status_slug( $status ): string {
    $status = sanitize_key( (string) $status );
    if ( strpos( $status, 'wc-' ) === 0 ) {
        $status = substr( $status, 3 );
    }
    return $status;
}

function senderzz_kits_get_stock_status_settings(): array {
    $defaults = senderzz_kits_default_stock_status_settings();
    $saved    = get_option( SENDERZZ_KIT_STOCK_SETTINGS_OPTION, [] );
    if ( ! is_array( $saved ) ) {
        $saved = [];
    }

    $debit  = $saved['debit_statuses'] ?? $saved['debit_status'] ?? $defaults['debit_statuses'];
    $credit = $saved['credit_statuses'] ?? $saved['credit_status'] ?? $defaults['credit_statuses'];

    if ( ! is_array( $debit ) ) {
        $debit = [ $debit ];
    }
    if ( ! is_array( $credit ) ) {
        $credit = [ $credit ];
    }

    $debit  = array_values( array_unique( array_filter( array_map( 'senderzz_kits_clean_status_slug', $debit ) ) ) );
    $credit = array_values( array_unique( array_filter( array_map( 'senderzz_kits_clean_status_slug', $credit ) ) ) );

    if ( empty( $debit ) ) {
        $debit = $defaults['debit_statuses'];
    }
    if ( empty( $credit ) ) {
        $credit = $defaults['credit_statuses'];
    }

    return [
        'debit_statuses'  => $debit,
        'credit_statuses' => $credit,
    ];
}

function senderzz_kits_get_order_status_options(): array {
    $statuses = function_exists( 'wc_get_order_statuses' ) ? wc_get_order_statuses() : [];
    $extra = [
        'wc-aprovado'          => 'Aprovado',
        'wc-processing'        => 'Processando',
        'wc-completed'         => 'Concluído',
        'wc-completo'          => 'Completo',
        'wc-entregue'          => 'Entregue',
        'wc-cancelled'         => 'Cancelado',
        'wc-devolvido'         => 'Devolvido',
        'wc-extravio'          => 'Extravio',
        'wc-enviado'           => 'Enviado',
        'wc-saldoinsuficiente' => 'Saldo Insuficiente',
    ];
    foreach ( $extra as $key => $label ) {
        if ( ! isset( $statuses[ $key ] ) ) {
            $statuses[ $key ] = $label;
        }
    }

    $out = [];
    foreach ( $statuses as $key => $label ) {
        $slug = senderzz_kits_clean_status_slug( $key );
        if ( $slug ) {
            $out[ $slug ] = $label;
        }
    }
    return $out;
}

/**
 * Admin menu.
 */
// Senderzz v221: submenu legado removido da origem; aba Kits vive em Admin > Senderzz.
// add_action( 'admin_menu', function () {
//     add_submenu_page(
//         'woocommerce',
//         'Senderzz Kits',
//         'Senderzz Kits',
//         'manage_woocommerce',
//         'senderzz-kits',
//         'senderzz_kits_admin_page'
//     );
// } );

/**
 * Carrega estilos simples do admin.
 */
add_action( 'admin_head', function () {
    if ( empty( $_GET['page'] ) ) {
        return;
    }
    $szk_page = sanitize_key( $_GET['page'] );
    $szk_tab  = sanitize_key( $_GET['tab'] ?? '' );
    if ( $szk_page !== 'senderzz-kits' && ! ( $szk_page === 'senderzz' && $szk_tab === 'kits' ) ) {
        return;
    }
    ?>
    <style>
        .szk-wrap{max-width:1180px}
        .szk-card{background:#fff;border:1px solid #dcdcde;border-radius:12px;padding:18px 20px;margin:16px 0;box-shadow:0 1px 2px rgba(0,0,0,.04)}
        .szk-grid{display:grid;grid-template-columns:1.4fr 1fr;gap:16px}
        .szk-row{display:grid;grid-template-columns:1.2fr 90px 1fr 34px;gap:10px;align-items:center;margin:9px 0}
        .szk-row select,.szk-row input{width:100%}
        .szk-badge{display:inline-flex;border-radius:999px;padding:4px 9px;background:#fff4ed;color:#c2410c;font-size:var(--sz-text-sm);font-weight:700}
        .szk-muted{color:#667085;font-size:var(--sz-text-meta)}
        .szk-table{width:100%;border-collapse:collapse;margin-top:12px}
        .szk-table th,.szk-table td{border-bottom:1px solid #eee;padding:10px;text-align:left;vertical-align:top}
        .szk-actions{display:flex;gap:8px;align-items:center;flex-wrap:wrap}
        .szk-danger{color:#b91c1c}
        .szk-ok{color:#15803d;font-weight:700}
        .szk-small{font-size:var(--sz-text-meta);color:#667085}.szk-settings-grid{display:grid;grid-template-columns:1fr 1fr;gap:14px}.szk-checks{max-height:190px;overflow:auto;border:1px solid #dcdcde;border-radius:10px;padding:10px;background:#fafafa}.szk-checks label{display:block;margin:6px 0}.szk-current{display:flex;gap:8px;flex-wrap:wrap;margin-top:8px}
        @media(max-width:900px){.szk-grid{grid-template-columns:1fr}.szk-row{grid-template-columns:1fr 90px}}
    </style>
    <?php
} );

/**
 * Helpers.
 */
function senderzz_kits_get_products_for_select(): array {
    if ( ! function_exists( 'wc_get_products' ) ) {
        return [];
    }

    $products = wc_get_products( [
        'limit'   => -1,
        'status'  => [ 'publish', 'private', 'draft' ],
        'type'    => [ 'simple', 'variation', 'variable' ],
        'orderby' => 'title',
        'order'   => 'ASC',
        'return'  => 'objects',
    ] );

    $out = [];
    foreach ( $products as $p ) {
        if ( ! $p instanceof WC_Product ) {
            continue;
        }

        $name = function_exists('senderzz_clean_product_label') ? senderzz_clean_product_label( $p->get_name() ) : $p->get_name();
        $sku  = $p->get_sku();
        $class_id = (int) $p->get_shipping_class_id();
        $class_name = $class_id ? get_term_field( 'name', $class_id, 'product_shipping_class' ) : 'Sem classe';

        $out[] = [
            'id'         => (int) $p->get_id(),
            'name'       => $name,
            'sku'        => $sku,
            'class_id'   => $class_id,
            'class_name' => is_wp_error( $class_name ) ? 'Sem classe' : $class_name,
            'stock'      => $p->managing_stock() ? $p->get_stock_quantity() : null,
        ];
    }

    return $out;
}

function senderzz_kits_product_label( int $product_id ): string {
    $p = wc_get_product( $product_id );
    if ( ! $p ) {
        return '#' . $product_id . ' — produto não encontrado';
    }
    $sku = $p->get_sku();
    $class_id = (int) $p->get_shipping_class_id();
    $class_name = $class_id ? get_term_field( 'name', $class_id, 'product_shipping_class' ) : 'Sem classe';
    if ( is_wp_error( $class_name ) ) {
        $class_name = 'Sem classe';
    }
    return '#' . $product_id . ' — ' . ( function_exists('senderzz_clean_product_label') ? senderzz_clean_product_label( $p->get_name() ) : $p->get_name() ) . ( $sku ? ' | SKU: ' . $sku : '' ) . ' | Classe: ' . $class_name;
}

function senderzz_kits_normalize_map( array $kits ): array {
    $normalizados = [];

    foreach ( $kits as $kit_product_id => $config ) {
        $kit_product_id = absint( $kit_product_id );
        if ( ! $kit_product_id ) {
            continue;
        }

        $kit_product = wc_get_product( $kit_product_id );
        if ( ! $kit_product ) {
            continue;
        }

        $kit_class_id = absint( $config['class_id'] ?? $kit_product->get_shipping_class_id() ?? 0 );
        $items = $config['items'] ?? $config;
        if ( ! is_array( $items ) ) {
            continue;
        }

        $components = [];
        foreach ( array_slice( $items, 0, 4 ) as $item ) {
            $component_id = absint( $item['product_id'] ?? 0 );
            $qty          = absint( $item['qty'] ?? 0 );

            if ( ! $component_id || $qty < 1 ) {
                continue;
            }

            // Bloqueia auto-referência.
            if ( $component_id === $kit_product_id ) {
                continue;
            }

            // Bloqueia componente que é também um kit (mesmo que adicionado depois).
            // Revalida em tempo de carregamento, não só no cadastro.
            if ( isset( $kits[ $component_id ] ) ) {
                continue;
            }

            $component_product = wc_get_product( $component_id );
            if ( ! $component_product ) {
                continue;
            }

            $components[] = [
                'product_id' => $component_id,
                'qty'        => $qty,
                'class_id'   => absint( $item['class_id'] ?? $component_product->get_shipping_class_id() ?? 0 ),
            ];
        }

        if ( ! empty( $components ) ) {
            $normalizados[ $kit_product_id ] = [
                'class_id' => $kit_class_id,
                'items'    => $components,
            ];
        }
    }

    return $normalizados;
}


if ( ! function_exists( 'senderzz_kits_admin_url' ) ) {
    function senderzz_kits_admin_url( array $args = [] ): string {
        return add_query_arg( array_merge( [ 'page' => 'senderzz', 'tab' => 'kits' ], $args ), admin_url( 'admin.php' ) );
    }
}

/**
 * Admin POST actions.
 */
add_action( 'admin_init', function () {
    if ( empty( $_POST['senderzz_kits_action'] ) ) {
        return;
    }

    if ( ! current_user_can( 'manage_woocommerce' ) ) {
        wp_die( 'Sem permissão.' );
    }

    check_admin_referer( 'senderzz_kits_save', 'senderzz_kits_nonce' );

    $action = sanitize_text_field( $_POST['senderzz_kits_action'] );
    $kits   = senderzz_kits_normalize_map( senderzz_get_kits() );

    if ( $action === 'save_stock_settings' ) {
        $debit_statuses  = array_map( 'senderzz_kits_clean_status_slug', (array) ( $_POST['debit_statuses'] ?? [] ) );
        $credit_statuses = array_map( 'senderzz_kits_clean_status_slug', (array) ( $_POST['credit_statuses'] ?? [] ) );

        $debit_statuses  = array_values( array_unique( array_filter( $debit_statuses ) ) );
        $credit_statuses = array_values( array_unique( array_filter( $credit_statuses ) ) );

        $defaults = senderzz_kits_default_stock_status_settings();
        if ( empty( $debit_statuses ) ) {
            $debit_statuses = $defaults['debit_statuses'];
        }
        if ( empty( $credit_statuses ) ) {
            $credit_statuses = $defaults['credit_statuses'];
        }

        update_option( SENDERZZ_KIT_STOCK_SETTINGS_OPTION, [
            'debit_statuses'  => $debit_statuses,
            'credit_statuses' => $credit_statuses,
        ], false );

        wp_safe_redirect( senderzz_kits_admin_url( [ 'szk_msg' => 'settings_saved' ] ) );
        exit;
    }

    if ( $action === 'save_kit' ) {
        $kit_product_id = absint( $_POST['kit_product_id'] ?? 0 );
        $kit_product = $kit_product_id ? wc_get_product( $kit_product_id ) : false;

        if ( ! $kit_product ) {
            wp_safe_redirect( senderzz_kits_admin_url( [ 'szk_msg' => 'kit_invalid' ] ) );
            exit;
        }

        $kit_class_id = absint( $kit_product->get_shipping_class_id() );
        $items = [];

        for ( $i = 1; $i <= 4; $i++ ) {
            $component_id = absint( $_POST[ 'component_' . $i ] ?? 0 );
            $qty          = absint( $_POST[ 'qty_' . $i ] ?? 0 );

            if ( ! $component_id || $qty < 1 ) {
                continue;
            }

            if ( $component_id === $kit_product_id ) {
                continue; // self-reference
            }

            $component_product = wc_get_product( $component_id );
            if ( ! $component_product ) {
                continue;
            }

            // Bloqueia componente que é também um kit (evita loop de estoque).
            if ( isset( senderzz_get_kits()[ $component_id ] ) ) {
                continue;
            }

            $items[] = [
                'product_id' => $component_id,
                'qty'        => $qty,
                'class_id'   => absint( $component_product->get_shipping_class_id() ),
            ];
        }

        if ( empty( $items ) ) {
            wp_safe_redirect( senderzz_kits_admin_url( [ 'szk_msg' => 'empty_items' ] ) );
            exit;
        }

        $kits[ $kit_product_id ] = [
            'class_id' => $kit_class_id,
            'items'    => array_slice( $items, 0, 4 ),
        ];

        update_option( SENDERZZ_KIT_STOCK_OPTION, $kits, false );

        wp_safe_redirect( senderzz_kits_admin_url( [ 'szk_msg' => 'saved' ] ) );
        exit;
    }

    if ( $action === 'delete_kit' ) {
        $kit_product_id = absint( $_POST['kit_product_id'] ?? 0 );
        if ( $kit_product_id && isset( $kits[ $kit_product_id ] ) ) {
            unset( $kits[ $kit_product_id ] );
            update_option( SENDERZZ_KIT_STOCK_OPTION, $kits, false );
        }

        wp_safe_redirect( senderzz_kits_admin_url( [ 'szk_msg' => 'deleted' ] ) );
        exit;
    }
} );

/**
 * Admin screen.
 */
function senderzz_kits_admin_page(): void {
    if ( ! function_exists( 'wc_get_products' ) ) {
        echo '<div class="notice notice-error"><p>WooCommerce não está ativo.</p></div>';
        return;
    }

    $products = senderzz_kits_get_products_for_select();
    $kits     = senderzz_kits_normalize_map( senderzz_get_kits() );
    $stock_settings = senderzz_kits_get_stock_status_settings();
    $status_options = senderzz_kits_get_order_status_options();

    $msg = sanitize_text_field( $_GET['szk_msg'] ?? '' );
    if ( $msg ) {
        $messages = [
            'saved'       => 'Kit salvo com sucesso.',
            'deleted'     => 'Kit removido.',
            'kit_invalid' => 'Produto kit inválido.',
            'empty_items' => 'Inclua pelo menos 1 produto interno com quantidade.',
            'settings_saved' => 'Status de baixa/reposição do estoque atualizados.',
        ];
        echo '<div class="notice notice-' . ( in_array( $msg, [ 'kit_invalid', 'empty_items' ], true ) ? 'error' : 'success' ) . ' is-dismissible"><p>' . esc_html( $messages[ $msg ] ?? 'Atualizado.' ) . '</p></div>';
    }
    ?>
    <div class="wrap szk-wrap">
        <h1>Senderzz Kits</h1>
        <p class="szk-muted">Crie kits que baixam automaticamente o estoque de até 4 produtos internos. A classe de entrega do kit e dos componentes fica salva no mapa.</p>

        <div class="szk-grid">
            <div class="szk-card">
                <h2>Criar / atualizar kit</h2>
                <form method="post">
                    <?php wp_nonce_field( 'senderzz_kits_save', 'senderzz_kits_nonce' ); ?>
                    <input type="hidden" name="senderzz_kits_action" value="save_kit">

                    <p>
                        <label><strong>Produto vendido como kit</strong></label><br>
                        <select name="kit_product_id" required>
                            <option value="">Selecione o produto kit...</option>
                            <?php foreach ( $products as $p ) : ?>
                                <option value="<?php echo esc_attr( $p['id'] ); ?>">
                                    <?php echo esc_html( '#' . $p['id'] . ' — ' . $p['name'] . ( $p['sku'] ? ' | SKU: ' . $p['sku'] : '' ) . ' | Classe: ' . $p['class_name'] ); ?>
                                </option>
                            <?php endforeach; ?>
                        </select>
                    </p>

                    <h3>Produtos internos do kit</h3>
                    <p class="szk-muted">Informe de 1 até 4 produtos. A quantidade será multiplicada pela quantidade comprada no pedido.</p>

                    <?php for ( $i = 1; $i <= 4; $i++ ) : ?>
                        <div class="szk-row">
                            <select name="component_<?php echo esc_attr( $i ); ?>">
                                <option value="">Produto interno <?php echo esc_html( $i ); ?>...</option>
                                <?php foreach ( $products as $p ) : ?>
                                    <option value="<?php echo esc_attr( $p['id'] ); ?>">
                                        <?php echo esc_html( '#' . $p['id'] . ' — ' . $p['name'] . ( $p['sku'] ? ' | SKU: ' . $p['sku'] : '' ) . ' | Classe: ' . $p['class_name'] ); ?>
                                    </option>
                                <?php endforeach; ?>
                            </select>
                            <input type="number" name="qty_<?php echo esc_attr( $i ); ?>" min="1" step="1" placeholder="Qtd">
                            <span class="szk-muted">Classe salva automaticamente</span>
                            <span></span>
                        </div>
                    <?php endfor; ?>

                    <p>
                        <button class="button button-primary button-large">Salvar kit</button>
                    </p>
                </form>
            </div>

            <div class="szk-card">
                <h2>Status do estoque</h2>
                <p><span class="szk-badge">Controle pelo painel</span></p>
                <p class="szk-muted">Escolha em quais status o estoque dos componentes do kit deve ser debitado e em quais deve ser creditado/reposto.</p>

                <form method="post">
                    <?php wp_nonce_field( 'senderzz_kits_save', 'senderzz_kits_nonce' ); ?>
                    <input type="hidden" name="senderzz_kits_action" value="save_stock_settings">

                    <div class="szk-settings-grid">
                        <div>
                            <h3>Debitar estoque quando entrar em:</h3>
                            <div class="szk-checks">
                                <?php foreach ( $status_options as $slug => $label ) : ?>
                                    <label>
                                        <input type="checkbox" name="debit_statuses[]" value="<?php echo esc_attr( $slug ); ?>" <?php checked( in_array( $slug, $stock_settings['debit_statuses'], true ) ); ?>>
                                        <?php echo esc_html( $label ); ?> <span class="szk-small">(<?php echo esc_html( $slug ); ?>)</span>
                                    </label>
                                <?php endforeach; ?>
                            </div>
                        </div>

                        <div>
                            <h3>Creditar/repor estoque quando entrar em:</h3>
                            <div class="szk-checks">
                                <?php foreach ( $status_options as $slug => $label ) : ?>
                                    <label>
                                        <input type="checkbox" name="credit_statuses[]" value="<?php echo esc_attr( $slug ); ?>" <?php checked( in_array( $slug, $stock_settings['credit_statuses'], true ) ); ?>>
                                        <?php echo esc_html( $label ); ?> <span class="szk-small">(<?php echo esc_html( $slug ); ?>)</span>
                                    </label>
                                <?php endforeach; ?>
                            </div>
                        </div>
                    </div>

                    <p><button class="button button-primary">Salvar status de estoque</button></p>
                </form>

                <p class="szk-muted">A baixa fica registrada no pedido para evitar duplicidade. A reposição usa exatamente os movimentos salvos na baixa.</p>
                <div class="szk-current">
                    <span class="szk-badge">Debita: <?php echo esc_html( implode( ', ', $stock_settings['debit_statuses'] ) ); ?></span>
                    <span class="szk-badge">Credita: <?php echo esc_html( implode( ', ', $stock_settings['credit_statuses'] ) ); ?></span>
                </div>
            </div>
        </div>

        <div class="szk-card">
            <h2>Kits cadastrados</h2>
            <?php if ( empty( $kits ) ) : ?>
                <p class="szk-muted">Nenhum kit cadastrado.</p>
            <?php else : ?>
                <table class="szk-table">
                    <thead>
                        <tr>
                            <th>Kit vendido</th>
                            <th>Classe do kit</th>
                            <th>Produtos internos</th>
                            <th>Ações</th>
                        </tr>
                    </thead>
                    <tbody>
                    <?php foreach ( $kits as $kit_id => $kit ) :
                        $kit_product = wc_get_product( (int) $kit_id );
                        $kit_class_id = absint( $kit['class_id'] ?? 0 );
                        $kit_class_name = $kit_class_id ? get_term_field( 'name', $kit_class_id, 'product_shipping_class' ) : 'Sem classe';
                        if ( is_wp_error( $kit_class_name ) ) $kit_class_name = 'Sem classe';
                        ?>
                        <tr>
                            <td>
                                <strong><?php echo esc_html( senderzz_kits_product_label( (int) $kit_id ) ); ?></strong>
                            </td>
                            <td><?php echo esc_html( $kit_class_name ); ?></td>
                            <td>
                                <?php foreach ( (array) $kit['items'] as $it ) :
                                    $component_class_id = absint( $it['class_id'] ?? 0 );
                                    $component_class_name = $component_class_id ? get_term_field( 'name', $component_class_id, 'product_shipping_class' ) : 'Sem classe';
                                    if ( is_wp_error( $component_class_name ) ) $component_class_name = 'Sem classe';
                                    ?>
                                    <div>
                                        <span class="szk-ok"><?php echo esc_html( (int) $it['qty'] ); ?>x</span>
                                        <?php echo esc_html( senderzz_kits_product_label( (int) $it['product_id'] ) ); ?>
                                        <span class="szk-small"> | Classe salva: <?php echo esc_html( $component_class_name ); ?></span>
                                    </div>
                                <?php endforeach; ?>
                            </td>
                            <td>
                                <form method="post" onsubmit="return confirm('Remover este kit?');">
                                    <?php wp_nonce_field( 'senderzz_kits_save', 'senderzz_kits_nonce' ); ?>
                                    <input type="hidden" name="senderzz_kits_action" value="delete_kit">
                                    <input type="hidden" name="kit_product_id" value="<?php echo esc_attr( $kit_id ); ?>">
                                    <button class="button button-link-delete">Excluir</button>
                                </form>
                            </td>
                        </tr>
                    <?php endforeach; ?>
                    </tbody>
                </table>
            <?php endif; ?>
        </div>
    </div>
    <?php
}

/**
 * Hooks de estoque configuráveis no painel Senderzz > Kits.
 */
function senderzz_kits_register_stock_status_hooks(): void {
    $settings = senderzz_kits_get_stock_status_settings();

    // Lista de slugs válidos: apenas status WooCommerce registrados.
    // Impede que um slug arbitrário salvo no banco mapeie para um hook
    // não relacionado a pedidos (ex: 'wp_loaded', 'init', etc.).
    $known_statuses = array_map(
        fn( $s ) => senderzz_kits_clean_status_slug( $s ),
        array_keys( function_exists( 'wc_get_order_statuses' ) ? wc_get_order_statuses() : [] )
    );
    $known_statuses = array_filter( $known_statuses );

    foreach ( (array) $settings['debit_statuses'] as $status_slug ) {
        $status_slug = senderzz_kits_clean_status_slug( $status_slug );
        if ( $status_slug && in_array( $status_slug, $known_statuses, true ) ) {
            add_action( 'woocommerce_order_status_' . $status_slug, 'senderzz_baixar_estoque_kits', 20 );
        }
    }

    foreach ( (array) $settings['credit_statuses'] as $status_slug ) {
        $status_slug = senderzz_kits_clean_status_slug( $status_slug );
        if ( $status_slug && in_array( $status_slug, $known_statuses, true ) ) {
            add_action( 'woocommerce_order_status_' . $status_slug, 'senderzz_repor_estoque_kits_devolvido', 20 );
        }
    }
}
// Registra hooks de estoque após plugins carregados para garantir que
// woocommerce_order_status_* esteja disponível.
add_action( 'init', 'senderzz_kits_register_stock_status_hooks', 20 );

/**
 * Baixa estoque dos componentes.
 */
function senderzz_baixar_estoque_kits( $order_id ): void {
    if ( ! $order_id ) return;

    $order = wc_get_order( $order_id );
    if ( ! $order ) return;

    /*
     * Links personalizados Senderzz já entram no pedido com os componentes reais
     * (pomada, gotas etc.) e o WooCommerce baixa o estoque desses itens nativos.
     * Se também rodarmos a baixa de kit aqui, o mesmo componente é debitado duas vezes
     * (ex.: 5 + 5 vira 10). Por isso, pedido com token/link sincronizado NÃO passa
     * pela baixa de kit composta.
     */
    $senderzz_offer_token = (string) $order->get_meta( '_senderzz_offer_token' );
    $senderzz_synced      = (string) $order->get_meta( '_senderzz_components_synced' );
    $senderzz_link_item   = false;

    foreach ( $order->get_items( 'line_item' ) as $senderzz_check_item ) {
        if ( ! $senderzz_check_item instanceof WC_Order_Item_Product ) continue;
        if (
            (string) $senderzz_check_item->get_meta( '_senderzz_offer_token', true ) !== '' ||
            (string) $senderzz_check_item->get_meta( '_senderzz_is_base_component', true ) === 'yes' ||
            (string) $senderzz_check_item->get_meta( '_senderzz_component_qty', true ) !== ''
        ) {
            $senderzz_link_item = true;
            break;
        }
    }

    if ( $senderzz_offer_token !== '' || $senderzz_synced === 'yes' || $senderzz_link_item ) {
        return;
    }

    if ( $order->get_meta( '_senderzz_kits_baixado' ) === 'yes' ) {
        return;
    }

    $kits = senderzz_kits_normalize_map( senderzz_get_kits() );
    if ( empty( $kits ) ) return;

    // Seta flag ANTES do loop para evitar dupla baixa em requests paralelos.
    $order->update_meta_data( '_senderzz_kits_baixado', 'yes' );
    $order->save();

    $movimentos = [];

    foreach ( $order->get_items() as $item ) {
        $kit_product_id = absint( $item->get_product_id() );
        $order_qty      = absint( $item->get_quantity() );

        if ( ! $kit_product_id || ! $order_qty || empty( $kits[ $kit_product_id ] ) ) {
            continue;
        }

        $kit = $kits[ $kit_product_id ];
        $kit_product = wc_get_product( $kit_product_id );
        if ( ! $kit_product ) continue;

        $real_class_id = absint( $kit_product->get_shipping_class_id() );
        $saved_class_id = absint( $kit['class_id'] ?? 0 );

        // Classe do kit salva e conferida.
        if ( $saved_class_id && $real_class_id && $saved_class_id !== $real_class_id ) {
            $order->add_order_note(
                sprintf(
                    'Senderzz Kits: kit #%d ignorado. Classe salva (%d) diferente da classe atual (%d).',
                    $kit_product_id,
                    $saved_class_id,
                    $real_class_id
                )
            );
            continue;
        }

        foreach ( array_slice( (array) $kit['items'], 0, 4 ) as $component ) {
            $component_id = absint( $component['product_id'] ?? 0 );
            $qty_per_kit  = absint( $component['qty'] ?? 0 );
            if ( ! $component_id || ! $qty_per_kit ) continue;

            $total_qty = $qty_per_kit * $order_qty;
            $comp_product = wc_get_product( $component_id );
            if ( ! $comp_product || ! $comp_product->managing_stock() ) {
                continue; // só baixa estoque de produtos com controle ativo
            }
            wc_update_product_stock( $component_id, $total_qty, 'decrease' );

            $movimentos[] = [
                'kit_product_id'       => $kit_product_id,
                'kit_class_id'         => $saved_class_id ?: $real_class_id,
                'component_product_id' => $component_id,
                'component_class_id'   => absint( $component['class_id'] ?? 0 ),
                'qty_per_kit'          => $qty_per_kit,
                'order_qty'            => $order_qty,
                'total_qty'            => $total_qty,
            ];
        }
    }

    if ( empty( $movimentos ) ) return;

    $order->update_meta_data( '_senderzz_kits_devolvido_reposto', 'no' );
    $order->update_meta_data( '_senderzz_kits_movimentos', $movimentos );
    $order->add_order_note( 'Senderzz Kits: estoque do kit baixado automaticamente. Movimentos: ' . count( $movimentos ) . '.' );
    $order->save();
}

/**
 * Repõe estoque dos componentes quando devolver.
 */

/**
 * Repõe estoque quando um pedido é cancelado, estornado, enviado para lixeira
 * ou excluído definitivamente.
 *
 * Pontos importantes:
 * - wc_maybe_increase_stock_levels() é idempotente via meta _order_stock_reduced.
 * - senderzz_repor_estoque_kits_devolvido() também é idempotente via
 *   _senderzz_kits_devolvido_reposto.
 * - Isso cobre pedidos antigos que foram apagados sem passar por status cancelado.
 */
function senderzz_repor_estoque_pedido_removido_ou_cancelado( $order_id, string $motivo = 'remocao' ): void {
    $order_id = absint( $order_id );
    if ( ! $order_id || ! function_exists( 'wc_get_order' ) ) {
        return;
    }

    $order = wc_get_order( $order_id );
    if ( ! $order ) {
        return;
    }

    // Primeiro repõe estoque de kits/componentes Senderzz, quando houve baixa própria.
    if ( function_exists( 'senderzz_repor_estoque_kits_devolvido' ) ) {
        senderzz_repor_estoque_kits_devolvido( $order_id );
    }

    // Depois repõe estoque nativo WooCommerce dos itens/componentes reais do pedido.
    if ( function_exists( 'wc_maybe_increase_stock_levels' ) ) {
        wc_maybe_increase_stock_levels( $order_id );
    }

    $order = wc_get_order( $order_id );
    if ( $order && $order->get_meta( '_senderzz_stock_restore_logged_' . sanitize_key( $motivo ) ) !== 'yes' ) {
        $order->update_meta_data( '_senderzz_stock_restore_logged_' . sanitize_key( $motivo ), 'yes' );
        $order->add_order_note( 'Senderzz: conferência de reposição de estoque executada por ' . $motivo . '.' );
        $order->save();
    }
}

function senderzz_repor_estoque_ao_cancelar_pedido( $order_id ): void {
    senderzz_repor_estoque_pedido_removido_ou_cancelado( $order_id, 'cancelamento' );
}

function senderzz_repor_estoque_ao_excluir_pedido_post( $post_id ): void {
    $post_id = absint( $post_id );
    if ( ! $post_id ) return;

    $post_type = get_post_type( $post_id );
    if ( $post_type !== 'shop_order' && $post_type !== 'shop_order_refund' ) {
        return;
    }

    senderzz_repor_estoque_pedido_removido_ou_cancelado( $post_id, 'exclusao' );
}

function senderzz_repor_estoque_ao_excluir_pedido_hpos( $order ): void {
    if ( $order instanceof WC_Order ) {
        senderzz_repor_estoque_pedido_removido_ou_cancelado( $order->get_id(), 'exclusao_hpos' );
    } elseif ( is_numeric( $order ) ) {
        senderzz_repor_estoque_pedido_removido_ou_cancelado( (int) $order, 'exclusao_hpos' );
    }
}

// Status que devem devolver estoque.
add_action( 'woocommerce_order_status_cancelled', 'senderzz_repor_estoque_ao_cancelar_pedido', 5 );
add_action( 'woocommerce_order_status_refunded',  'senderzz_repor_estoque_ao_cancelar_pedido', 5 );
add_action( 'woocommerce_order_status_failed',    'senderzz_repor_estoque_ao_cancelar_pedido', 5 );

// Exclusão/lixeira no modo posts tradicional.
add_action( 'wp_trash_post',       'senderzz_repor_estoque_ao_excluir_pedido_post', 5 );
add_action( 'before_delete_post',  'senderzz_repor_estoque_ao_excluir_pedido_post', 5 );

// Exclusão/lixeira no HPOS/WooCommerce moderno, quando disponível.
add_action( 'woocommerce_before_trash_order',  'senderzz_repor_estoque_ao_excluir_pedido_hpos', 5 );
add_action( 'woocommerce_before_delete_order', 'senderzz_repor_estoque_ao_excluir_pedido_hpos', 5 );

function senderzz_repor_estoque_kits_devolvido( $order_id ): void {
    if ( ! $order_id ) return;

    $order = wc_get_order( $order_id );
    if ( ! $order ) return;

    if ( $order->get_meta( '_senderzz_kits_baixado' ) !== 'yes' ) {
        return;
    }

    if ( $order->get_meta( '_senderzz_kits_devolvido_reposto' ) === 'yes' ) {
        return;
    }

    $movimentos = $order->get_meta( '_senderzz_kits_movimentos' );
    if ( ! is_array( $movimentos ) || empty( $movimentos ) ) {
        return;
    }

    $repostos = 0;

    foreach ( $movimentos as $mov ) {
        $component_id = absint( $mov['component_product_id'] ?? 0 );
        $qty          = absint( $mov['total_qty'] ?? 0 );

        if ( ! $component_id || ! $qty ) {
            continue;
        }

        $comp_product = wc_get_product( $component_id );
        if ( ! $comp_product || ! $comp_product->managing_stock() ) {
            continue;
        }
        wc_update_product_stock( $component_id, $qty, 'increase' );
        $repostos++;
    }

    if ( ! $repostos ) return;

    $order->update_meta_data( '_senderzz_kits_devolvido_reposto', 'yes' );
    $order->add_order_note( 'Senderzz Kits: estoque do kit reposto automaticamente por devolução. Movimentos: ' . $repostos . '.' );
    $order->save();
}
// Notice de webhook: movido para hook admin_init para evitar write no boot.
add_action( 'admin_init', function() {
    if ( ! get_option( 'senderzz_webhook_notice_hidden' ) ) {
        update_option( 'senderzz_webhook_notice_hidden', 1 );
        add_action( 'admin_notices', function() {
            echo '<div class="notice notice-warning is-dismissible"><p>Senderzz: configure o endpoint de webhook no painel do Melhor Envio.</p></div>';
        } );
    }
} );
add_action('wp_head', function () {
?>
<style>
body table.wfacp_shipping_table ul#shipping_method.wfacp_no_add_here li p {
    margin: 0 !important;
    padding: 14px 16px !important;
    width: 100% !important;
    box-sizing: border-box !important;

    background: linear-gradient(180deg, #fff7ed, #fff1e8) !important;
    border: 1px solid rgba(249,115,22,.22) !important;
    border-radius: 14px !important;

    color: #1f2937 !important;
    font-size: var(--sz-text-base) !important;
    font-weight: 700!important;
    line-height: 1.4 !important;

    box-shadow: 0 12px 28px rgba(15,23,42,.05) !important;
    position: relative;
}

/* linha lateral estilo SaaS */
body table.wfacp_shipping_table ul#shipping_method.wfacp_no_add_here li p::after {
    content: "";
    position: absolute;
    left: 0;
    top: 10px;
    bottom: 10px;
    width: 4px;
    border-radius: 4px;
    background: linear-gradient(180deg,#E8650A,#F5920A);
}

/* remove qualquer ícone padrão */
body table.wfacp_shipping_table ul#shipping_method.wfacp_no_add_here li p::before {
    display: none !important;
}
</style>
<?php
}, 999999);
// ── Cron: limpeza de sessões e códigos 2FA expirados ─────────────────────────
add_action( 'senderzz_portal_cleanup_sessions', [ 'WC_MelhorEnvio\\Portal\\Portal_Auth', 'cleanup_expired_sessions' ] );



// SENDERZZ v78: garante acesso do administrador e redireciona slugs antigas para o menu unificado.
add_action('admin_init', function(){
    if (empty($_GET['page'])) { return; }
    if (!(current_user_can('manage_options') || current_user_can('manage_woocommerce'))) { return; }
    $map = [
        'sz-motoboy' => 'motoboy',
        'senderzz-cod-finance' => 'financeiro-cod',
        'senderzz-webhooks' => 'webhooks-classe',
        'senderzz-affiliates' => 'afiliados',
        'senderzz-dashboard' => 'dashboard',
        'senderzz-portal-users' => 'usuarios',
        'senderzz-analytics' => 'relatorios',
        'senderzz-markup' => 'taxas',
        'tp-preferida' => 'preferidas',
        'senderzz-blocked-carriers' => 'bloqueadas',
        'senderzz-tracking-brand' => 'rastreio',
        'senderzz-remetentes' => 'remetentes',
        'senderzz-kits' => 'kits',
        'tpc-carteira' => 'carteira',
    ];
    $page = sanitize_key($_GET['page']);
    if (isset($map[$page]) && $page !== 'senderzz') {
        wp_safe_redirect(admin_url('admin.php?page=senderzz&tab='.$map[$page]));
        exit;
    }
}, 0);
