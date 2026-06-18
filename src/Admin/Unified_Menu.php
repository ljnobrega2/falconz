<?php
/**
 * Unified admin menu for Senderzz.
 * Admin Senderzz refatorado: operação, COD, financeiro COD, expedição e configurações isolados.
 */

namespace WC_MelhorEnvio\Admin;

defined( 'ABSPATH' ) || exit;

class Unified_Menu {
    private const ORANGE = '#E8650A';

    public function __construct() {
        add_action( 'admin_menu', [ $this, 'register' ], 99 );
        add_action( 'admin_menu', [ $this, 'remove_individual_menus' ], 1000 );
        add_action( 'admin_post_sz_admin_audit_fix_all', [ $this, 'handle_audit_fix_all' ] );
        add_action( 'admin_post_sz_admin_audit_fix_order', [ $this, 'handle_audit_fix_order' ] );
        add_action( 'admin_post_sz_admin_fix_affiliate_wallet', [ $this, 'handle_fix_affiliate_wallet' ] );
    }

    public function register(): void {
        add_menu_page(
            'Senderzz',
            'Senderzz',
            'manage_options',
            'senderzz',
            [ $this, 'render' ],
            'data:image/svg+xml;base64,' . base64_encode('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 40 40"><rect x="3" y="3" width="34" height="34" rx="9" fill="#E8650A"/><path d="M22 6 10 22h9l-2 12 13-18h-9z" fill="#fff"/></svg>'),
            56
        );

        add_action( 'admin_init', [ $this, 'redirect_legacy_pages' ] );
    }

    public function remove_individual_menus(): void {
        // Etapa 4: mantém o topo limpo. As rotas antigas continuam acessíveis via redirect_legacy_pages,
        // mas não aparecem como menus soltos/duplicados no WordPress.
        $legacy = [
            'tpc-carteira', 'senderzz-markup', 'tp-preferida', 'senderzz-blocked-carriers',
            'senderzz-tracking-brand', 'senderzz-remetentes', 'senderzz-webhooks',
            'senderzz-portal-users', 'senderzz-analytics', 'senderzz-kits', 'senderzz-cod-finance',
            'sz-motoboy', 'senderzz-affiliates', 'senderzz-dashboard', 'senderzz-onboarding',
            'senderzz-notifications', 'senderzz-pwa-notificacoes', 'senderzz-multi-class',
        ];
        foreach ( $legacy as $slug ) {
            remove_menu_page( $slug );
            remove_submenu_page( 'woocommerce', $slug );
            remove_submenu_page( 'tpc-carteira', $slug );
            remove_submenu_page( 'senderzz', $slug );
        }
    }

    public function redirect_legacy_pages(): void {
        if ( ! isset( $_GET['page'] ) ) return;
        if ( ! current_user_can( 'manage_options' ) && ! current_user_can( 'manage_woocommerce' ) ) return;

        $map = [
            'tpc-carteira'              => [ 'area' => 'expedicao', 'tab' => 'exp-carteira' ],
            'senderzz-markup'           => [ 'area' => 'expedicao', 'tab' => 'exp-integracoes' ],
            'tp-preferida'              => [ 'area' => 'expedicao', 'tab' => 'exp-pedidos' ],
            'senderzz-blocked-carriers' => [ 'area' => 'expedicao', 'tab' => 'exp-pedidos' ],
            'senderzz-tracking-brand'   => [ 'area' => 'expedicao', 'tab' => 'exp-pedidos' ],
            'senderzz-remetentes'       => [ 'area' => 'expedicao', 'tab' => 'exp-pedidos' ],
            'senderzz-webhooks'         => [ 'area' => 'expedicao', 'tab' => 'exp-webhooks' ],
            'senderzz-portal-users'     => [ 'area' => 'config', 'tab' => 'cfg-usuarios' ],
            'senderzz-analytics'        => [ 'area' => 'expedicao', 'tab' => 'exp-pedidos' ],
            'senderzz-kits'             => [ 'area' => 'expedicao', 'tab' => 'exp-pedidos' ],
            'senderzz-cod-finance'      => [ 'area' => 'financeiro', 'tab' => 'fin-livro-cod' ],
            'sz-motoboy'                => [ 'area' => 'cod', 'tab' => 'cod-pedidos' ],
            'senderzz-affiliates'       => [ 'area' => 'financeiro', 'tab' => 'fin-livro-cod' ],
            'senderzz-dashboard'        => [ 'area' => 'cod', 'tab' => 'cod-pedidos' ],
            'senderzz-onboarding'       => [ 'area' => 'config', 'tab' => 'cfg-geral' ],
            'senderzz-notifications'    => [ 'area' => 'config', 'tab' => 'cfg-push' ],
            'senderzz-pwa-notificacoes' => [ 'area' => 'config', 'tab' => 'cfg-notificacoes' ],
        ];
        $page = sanitize_key( wp_unslash( $_GET['page'] ) );
        if ( ! isset( $map[ $page ] ) ) return;

        $args = [ 'page' => 'senderzz', 'area' => $map[ $page ]['area'], 'tab' => $map[ $page ]['tab'] ];
        foreach ( [ 'subtab', 'section', 'view', 'status', 'paged', 's', 'mb_tab' ] as $keep ) {
            if ( isset( $_GET[ $keep ] ) ) $args[ $keep ] = sanitize_text_field( wp_unslash( $_GET[ $keep ] ) );
        }
        wp_safe_redirect( add_query_arg( $args, admin_url( 'admin.php' ) ) );
        exit;
    }

    public function render(): void {
        $areas = $this->areas();
        $area  = sanitize_key( $_GET['area'] ?? 'visao' );
        $tab   = sanitize_key( $_GET['tab'] ?? '' );

        $legacy_tabs = [
            'visao-geral' => [ 'area' => 'visao', 'tab' => 'overview' ],
            'dashboard' => [ 'area' => 'cod', 'tab' => 'cod-pedidos' ],
            'motoboy' => [ 'area' => 'cod', 'tab' => 'cod-pedidos' ],
            'financeiro-cod' => [ 'area' => 'financeiro', 'tab' => 'fin-livro-cod' ],
            'carteira' => [ 'area' => 'expedicao', 'tab' => 'exp-carteira' ],
            'afiliados' => [ 'area' => 'financeiro', 'tab' => 'fin-livro-cod' ],
            'taxas-cod' => [ 'area' => 'financeiro', 'tab' => 'fin-livro-cod' ],
            'notificacoes' => [ 'area' => 'config', 'tab' => 'cfg-notificacoes' ],
            'push-tecnico' => [ 'area' => 'config', 'tab' => 'cfg-push' ],
            'webhooks-classe' => [ 'area' => 'expedicao', 'tab' => 'exp-webhooks' ],
        ];
        if ( $tab === 'motoboy' && isset( $_GET['mb_tab'] ) ) {
            // URLs internas antigas do módulo Motoboy continuam dentro do módulo operacional completo.
            // Não trocar para a tabela resumida; ela escondia Embalar, Enviar para rota,
            // definir/trocar entregador, imprimir e mudar status.
            $area = 'cod';
            $mb_tab = sanitize_key( wp_unslash( $_GET['mb_tab'] ) );
            $tab = 'cod-pedidos';
        } elseif ( isset( $legacy_tabs[ $tab ] ) ) {
            $area = $legacy_tabs[ $tab ]['area'];
            $tab  = $legacy_tabs[ $tab ]['tab'];
        }

        if ( ! isset( $areas[ $area ] ) ) $area = 'visao';
        if ( ! $tab || ! isset( $areas[ $area ]['items'][ $tab ] ) ) $tab = $areas[ $area ]['default'];

        $current = $areas[ $area ];
        $item = $current['items'][ $tab ];
        $render_key = $item['render'] ?? 'overview';
        ?>
        <style><?php echo $this->admin_css(); ?></style>
        <div id="sz-admin-wrap">
            <div class="sz-shell">
                <aside class="sz-side">
                    <div class="sz-brand"><div class="sz-brand-ico">⚡</div><div><h1>Senderzz</h1><p>Operação sem ambiguidades</p></div></div>
                    <nav class="sz-main-nav" aria-label="Sidebar Senderzz">
                        <?php foreach ( $areas as $slug => $data ) : ?>
                            <a class="sz-area-link <?php echo $area === $slug ? 'active' : ''; ?>" href="<?php echo esc_url( admin_url( 'admin.php?page=senderzz&area=' . $slug . '&tab=' . $data['default'] ) ); ?>">
                                <span><?php echo esc_html( $data['icon'] ); ?></span><b><?php echo esc_html( $data['label'] ); ?></b>
                            </a>
                        <?php endforeach; ?>
                    </nav>
                </aside>
                <main class="sz-main">
                    <header class="sz-topbar">
                        <div><div class="sz-crumb"><?php echo esc_html( $current['label'] ); ?></div><h2><?php echo esc_html( $item['label'] ); ?></h2><p><?php echo esc_html( $current['tagline'] ); ?></p></div>
                    </header>
                    <?php if ( count( $current['items'] ) > 1 ) : ?>
                        <div class="sz-subnav">
                            <?php foreach ( $current['items'] as $slug => $sub ) : ?>
                                <a class="<?php echo $tab === $slug ? 'active' : ''; ?>" href="<?php echo esc_url( admin_url( 'admin.php?page=senderzz&area=' . $area . '&tab=' . $slug ) ); ?>"><?php echo esc_html( $sub['label'] ); ?></a>
                            <?php endforeach; ?>
                        </div>
                    <?php endif; ?>
                    <section class="sz-content"><div class="sz-tab-body">
                        <?php $this->notice_from_query(); ?>
                        <?php $this->dispatch( $render_key ); ?>
                    </div></section>
                </main>
            </div>
        </div>
        <?php
    }

    private function areas(): array {
        return [
            'visao' => [
                'icon' => '⚡', 'label' => 'Visão Geral', 'tagline' => 'Somente operação: pedidos, rota, entrega, frustração e alertas.', 'default' => 'overview',
                'items' => [ 'overview' => [ 'label' => 'Visão Geral', 'render' => 'overview' ] ],
            ],
            'cod' => [
                'icon' => '🚚', 'label' => 'Motoboy', 'tagline' => 'Operação logística COD.', 'default' => 'cod-pedidos',
                'items' => [
                    'cod-pedidos' => [ 'label' => 'Motoboy', 'render' => 'cod-pedidos' ],
                ],
            ],
            'financeiro' => [
                'icon' => '💰', 'label' => 'Financeiro COD', 'tagline' => 'Livro único: pedido, produtor, afiliado e repasses sem telas duplicadas.', 'default' => 'fin-livro-cod',
                'items' => [
                    'fin-livro-cod' => [ 'label' => 'Livro COD', 'render' => 'fin-taxas' ],
                    'fin-saques' => [ 'label' => 'Saques', 'render' => 'fin-saques' ],
                    'fin-taxas-entrega' => [ 'label' => 'Taxas de Entrega', 'render' => 'fin-taxas-entrega' ],
                    'fin-auditoria' => [ 'label' => 'Auditoria', 'render' => 'audit' ],
                ],
            ],
            'expedicao' => [
                'icon' => '📦', 'label' => 'Expedição', 'tagline' => 'Pedidos, integrações, webhooks e frete.', 'default' => 'exp-pedidos',
                'items' => [
                    'exp-pedidos' => [ 'label' => 'Pedidos', 'render' => 'exp-pedidos' ],
                    'exp-integracoes' => [ 'label' => 'Integrações', 'render' => 'exp-integracoes' ],
                    'exp-webhooks' => [ 'label' => 'Webhooks', 'render' => 'exp-webhooks' ],
                    'exp-carteira' => [ 'label' => 'Carteira Frete', 'render' => 'exp-carteira' ],
                ],
            ],
            'config' => [
                'icon' => '⚙️', 'label' => 'Configurações', 'tagline' => 'Ajustes gerais da plataforma.', 'default' => 'cfg-geral',
                'items' => [
                    'cfg-geral' => [ 'label' => 'Geral', 'render' => 'config' ],
                    'cfg-usuarios' => [ 'label' => 'Usuários', 'render' => 'usuarios' ],
                    'cfg-notificacoes' => [ 'label' => 'Notificações', 'render' => 'notificacoes' ],
                    'cfg-push' => [ 'label' => 'Push', 'render' => 'push' ],
                    'cfg-api' => [ 'label' => 'API', 'render' => 'api' ],
                    'cfg-saude' => [ 'label' => 'Diagnóstico', 'render' => 'saude' ],
                ],
            ],
        ];
    }

    private function dispatch( string $key ): void {
        switch ( $key ) {
            case 'overview': $this->tab_overview_operacao(); break;
            case 'cod-pedidos': $this->tab_wrap( 'sz_mb_admin_page', [ 'tab' => 'motoboy', 'mb_tab' => sanitize_key( $_GET['mb_tab'] ?? 'pedidos' ) ] ); break;
            case 'cod-motoboys': $this->tab_wrap( 'sz_mb_admin_page', [ 'tab' => 'motoboy', 'mb_tab' => sanitize_key( $_GET['mb_tab'] ?? 'motoboys' ) ] ); break;
            case 'cod-relatorios': function_exists( 'sz_mb_relatorios_page' ) ? $this->tab_wrap( 'sz_mb_relatorios_page' ) : $this->tab_wrap( 'sz_mb_admin_page', [ 'tab' => 'motoboy', 'mb_tab' => 'relatorios' ] ); break;
            case 'fin-produtores': $this->tab_fin_taxas(); break;
            case 'fin-afiliados': $this->tab_fin_taxas(); break;
            case 'fin-carteira-aff': $this->tab_fin_taxas(); break;
            case 'fin-taxas': $this->tab_fin_taxas(); break;
            case 'fin-livro-cod': $this->tab_fin_taxas(); break;
            case 'fin-carteiras': $this->tab_fin_carteiras(); break;
            case 'fin-saques': $this->tab_fin_saques(); break;
            case 'fin-taxas-entrega': $this->tab_fin_taxas_entrega(); break;
            case 'fin-frustrados': $this->tab_fin_taxas_entrega(); break;
            case 'exp-pedidos': $this->tab_exp_pedidos(); break;
            case 'exp-integracoes': $this->tab_exp_integracoes(); break;
            case 'exp-webhooks': $this->tab_exp_webhooks(); break;
            case 'exp-carteira': $this->tab_exp_carteira(); break;
            case 'usuarios': $this->tab_class( '\WC_MelhorEnvio\Portal\Portal_Admin', 'render_page' ); break;
            case 'notificacoes': $this->tab_notificacoes_pwa(); break;
            case 'push': $this->tab_push_tecnico(); break;
            case 'config': $this->tab_configuracoes(); break;
            case 'api': $this->tab_api(); break;
            case 'saude': $this->tab_saude_sistema(); break;
            case 'audit': $this->tab_auditoria(); break;
            default: $this->tab_overview_operacao();
        }
    }

    private function notice_from_query(): void {
        if ( empty( $_GET['sz_msg'] ) ) return;
        $msg = sanitize_text_field( wp_unslash( $_GET['sz_msg'] ) );
        $warn = ! empty( $_GET['sz_warn'] );
        echo '<div class="sz-section" style="margin-bottom:14px"><div class="sz-section-body"><span class="sz-status-pill ' . ( $warn ? 'warn' : 'ok' ) . '">' . esc_html( $msg ) . '</span></div></div>';
    }

    private function admin_css(): string {
        return <<<'CSS'
#sz-admin-wrap{font-family:var(--sz-font);margin:-8px 0 0 -20px;min-height:calc(100vh - 32px);background:#f6f7f9;color:#111827}
#sz-admin-wrap *{box-sizing:border-box}
#sz-admin-wrap a{text-decoration:none}
#sz-admin-wrap .wrap{margin:0;padding:0;max-width:none;background:transparent}
#sz-admin-wrap input,#sz-admin-wrap select,#sz-admin-wrap textarea{border:1px solid #d8dee8;border-radius:9px;box-shadow:none;min-height:38px;padding:7px 10px;background:#fff;color:#111827}
#sz-admin-wrap input:focus,#sz-admin-wrap select:focus,#sz-admin-wrap textarea:focus{border-color:#E8650A;box-shadow:0 0 0 3px rgba(232,101,10,.12);outline:none}
#sz-admin-wrap .button,#sz-admin-wrap button,#sz-admin-wrap input[type=submit]{border-radius:9px;min-height:38px;font-weight:700;box-shadow:none}
#sz-admin-wrap .button-primary,#sz-admin-wrap input[type=submit].button-primary,#sz-admin-wrap button.button-primary,.sz-primary{background:#E8650A;border-color:#E8650A;color:#fff}
#sz-admin-wrap table.widefat,#sz-admin-wrap table{border-color:#e5e7eb;background:#fff;border-radius:12px;overflow:hidden}
#sz-admin-wrap th{color:#64748b;font-size:var(--sz-text-sm);text-transform:none;letter-spacing:.02em}
.sz-shell{display:grid;grid-template-columns:248px minmax(0,1fr);min-height:calc(100vh - 32px)}
.sz-side{background:#111827;color:#fff;padding:18px 14px;position:sticky;top:32px;height:calc(100vh - 32px);overflow:auto;border-right:1px solid rgba(255,255,255,.08)}
.sz-brand{display:flex;gap:10px;align-items:center;padding:8px 8px 16px;border-bottom:1px solid rgba(255,255,255,.08);margin-bottom:14px}.sz-brand-ico{width:38px;height:38px;border-radius:10px;background:#E8650A;display:flex;align-items:center;justify-content:center;font-weight:700}.sz-brand h1{font-size:var(--sz-text-lg);line-height:1;margin:0;color:#fff}.sz-brand p{font-size:var(--sz-text-sm);line-height:1.25;color:rgba(255,255,255,.58);margin:4px 0 0}
.sz-main-nav{display:grid;gap:7px}.sz-area-link{display:flex;gap:10px;align-items:center;color:rgba(255,255,255,.78);background:rgba(255,255,255,.045);border:1px solid rgba(255,255,255,.07);border-radius:10px;padding:11px 10px;font-size:var(--sz-text-base)}.sz-area-link.active{background:#E8650A;border-color:#E8650A;color:#fff}.sz-area-link b{font-weight:700}
.sz-main{min-width:0}.sz-topbar{background:#fff;border-bottom:1px solid #e5e7eb;padding:18px 26px;display:flex;justify-content:space-between;gap:12px}.sz-crumb{font-size:var(--sz-text-sm);font-weight:700;text-transform:none;letter-spacing:.02em;color:#94a3b8;margin-bottom:5px}.sz-topbar h2{font-size:var(--sz-text-3xl);line-height:1.1;margin:0;color:#111827}.sz-topbar p{font-size:var(--sz-text-base);color:#64748b;margin:6px 0 0;max-width:820px}
.sz-subnav{display:flex;gap:8px;flex-wrap:wrap;background:#fff;border-bottom:1px solid #e5e7eb;padding:10px 26px}.sz-subnav a{display:inline-flex;align-items:center;min-height:34px;padding:0 12px;border-radius:9px;background:#f3f4f6;color:#475569;font-weight:700;font-size:var(--sz-text-meta)}.sz-subnav a.active{background:#E8650A;color:#fff}
.sz-content{padding:22px 26px}.sz-tab-body>div,.sz-tab-content{background:transparent;padding:0;min-height:auto}.sz-section,#sz-admin-wrap .postbox,#sz-admin-wrap .card,#sz-admin-wrap .stuffbox,#sz-admin-wrap .sz-panel{background:#fff;border:1px solid #e5e7eb;border-radius:12px;box-shadow:none;overflow:hidden}.sz-section{margin-bottom:18px}.sz-section-head{padding:15px 18px;border-bottom:1px solid #eef2f7;display:flex;align-items:center;gap:10px}.sz-section-head h3{font-size:var(--sz-text-lg);font-weight:700;color:#111827;margin:0;flex:1}.sz-section-body{padding:18px}
.sz-kpi-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(210px,1fr));gap:10px;margin-bottom:16px}.sz-kpi{background:#fff;border:1px solid #e5e7eb;border-radius:12px;padding:11px 14px;min-height:88px;display:flex;flex-direction:column;justify-content:center}.sz-kpi-label{font-size:var(--sz-text-xs);font-weight:700;text-transform:none;letter-spacing:.02em;color:#94a3b8;margin-bottom:4px}.sz-kpi-val{font-size:var(--sz-text-xl);font-weight:700;color:#111827;letter-spacing:-.02em;line-height:1.1}.sz-kpi-sub{font-size:var(--sz-text-sm);color:#64748b;margin-top:4px;line-height:1.25}.sz-kpi.ok .sz-kpi-val{color:#16a34a}.sz-kpi.warn .sz-kpi-val{color:#E8650A}.sz-filter-bar{display:flex;gap:10px;align-items:end;flex-wrap:wrap;margin:12px 0 16px;background:#fff;border:1px solid #e5e7eb;border-radius:12px;padding:12px}.sz-filter-bar label{font-size:var(--sz-text-meta);font-weight:700;color:#334155}.sz-filter-bar .button{display:inline-flex;align-items:center;justify-content:center;text-decoration:none}
.sz-collapse{margin-bottom:18px;background:#fff;border:1px solid #e5e7eb;border-radius:12px;overflow:hidden}.sz-collapse>summary{cursor:pointer;list-style:none;padding:15px 18px;font-weight:700;color:#111827;border-bottom:1px solid #eef2f7}.sz-collapse>summary::-webkit-details-marker{display:none}.sz-collapse>summary:after{content:'Recolher';float:right;font-size:var(--sz-text-sm);color:#64748b;font-weight:700;text-transform:none;letter-spacing:.02em}.sz-collapse:not([open])>summary{border-bottom:0}.sz-collapse:not([open])>summary:after{content:'Expandir'} .sz-collapse .sz-section{border:0;border-radius:0;margin-bottom:0}
.sz-filterbar{display:flex;gap:10px;flex-wrap:wrap;align-items:center;background:#fff;border:1px solid #e5e7eb;border-radius:12px;padding:12px;margin-bottom:16px}.sz-status-pill{display:inline-flex;align-items:center;padding:3px 8px;border-radius:999px;font-size:var(--sz-text-sm);font-weight:700;background:#f1f5f9;color:#475569}.sz-status-pill.warn{background:#fff7ed;color:#c2410c}.sz-status-pill.ok{background:#dcfce7;color:#166534}.sz-notice{padding:12px 14px;border-radius:12px;margin:0 0 16px;background:#ecfdf5;border:1px solid #bbf7d0;color:#166534;font-weight:700}.sz-notice.warn{background:#fff7ed;border-color:#fed7aa;color:#c2410c}.sz-actions{display:flex;gap:8px;flex-wrap:wrap;align-items:center}.sz-muted{color:#64748b;font-size:var(--sz-text-meta)}.sz-two-col{display:grid;grid-template-columns:minmax(0,1fr) minmax(320px,420px);gap:16px}.sz-clean-list{display:grid;gap:8px;margin:0;padding:0;list-style:none}.sz-clean-list li{display:flex;justify-content:space-between;gap:12px;border:1px solid #eef2f7;border-radius:10px;padding:10px 12px;background:#fff}.sz-code{background:#0f172a;color:#fff;border-radius:12px;padding:14px;overflow:auto;font-size:var(--sz-text-meta);line-height:1.45}.sz-apple-toggle{appearance:none;width:42px;height:26px;min-height:26px;border-radius:8px;background:#d1d5db;border:0;padding:0;position:relative;vertical-align:middle}.sz-apple-toggle:checked{background:#E8650A}.sz-apple-toggle:before{content:"";position:absolute;width:20px;height:20px;border-radius:6px;background:#fff;left:3px;top:3px;transition:.15s}.sz-apple-toggle:checked:before{left:19px}
.sz-tab-content .nav-tab-wrapper,.sz-tab-content .subsubsub{display:none}.sz-tab-content h1:first-child,.sz-tab-content h2:first-child{display:none}.sz-tab-content .wp-list-table{margin:0}.sz-tab-content form{max-width:100%}.sz-tab-content .notice{border-radius:10px;box-shadow:none}.sz-tab-content .button-primary{background:#E8650A;border-color:#E8650A;color:#fff}
#sz-admin-wrap .form-table th{width:220px;color:#334155;font-weight:700}
#sz-admin-wrap .form-table td{padding:12px 10px}
#sz-admin-wrap input[type=checkbox]:not(.sz-apple-toggle){appearance:none;width:22px;height:22px;min-height:22px;border-radius:6px;border:1px solid #cbd5e1;background:#fff;padding:0;vertical-align:middle}
#sz-admin-wrap input[type=checkbox]:not(.sz-apple-toggle):checked{background:#E8650A;border-color:#E8650A}
#sz-admin-wrap input[type=checkbox]:not(.sz-apple-toggle):checked:after{content:'✓';display:block;color:#fff;text-align:center;font-weight:700;line-height:20px}
@media(max-width:960px){#sz-admin-wrap{margin-left:-10px}.sz-shell{display:block}.sz-side{position:relative;top:auto;height:auto}.sz-content,.sz-topbar,.sz-subnav{padding-left:16px;padding-right:16px}.sz-two-col{grid-template-columns:1fr}}
CSS;
    }

    private function meta_table(): string {
        global $wpdb;
        $hpos = $wpdb->prefix . 'wc_orders_meta';
        return $this->table_exists( $hpos ) ? $hpos : $wpdb->postmeta;
    }

    private function meta_order_col(): string { return $this->meta_table() === $GLOBALS['wpdb']->postmeta ? 'post_id' : 'order_id'; }

    private function table_exists( string $table ): bool {
        global $wpdb;
        return (bool) $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $table ) );
    }

    private function money( $value ): string { return 'R$ ' . number_format( (float) $value, 2, ',', '.' ); }

    private function order_admin_url( int $order_id ): string {
        if ( function_exists( 'wc_get_container' ) && class_exists( '\Automattic\WooCommerce\Internal\DataStores\Orders\CustomOrdersTableController' ) ) {
            try {
                $controller = wc_get_container()->get( \Automattic\WooCommerce\Internal\DataStores\Orders\CustomOrdersTableController::class );
                if ( $controller && method_exists( $controller, 'custom_orders_table_usage_is_enabled' ) && $controller->custom_orders_table_usage_is_enabled() ) {
                    return admin_url( 'admin.php?page=wc-orders&action=edit&id=' . absint( $order_id ) );
                }
            } catch ( \Throwable $e ) {}
        }
        return admin_url( 'post.php?post=' . absint( $order_id ) . '&action=edit' );
    }

    private function get_order_status( int $order_id ): string {
        global $wpdb;
        if ( $this->table_exists( $wpdb->prefix . 'wc_orders' ) ) {
            return (string) $wpdb->get_var( $wpdb->prepare( "SELECT status FROM {$wpdb->prefix}wc_orders WHERE id=%d LIMIT 1", $order_id ) );
        }
        return (string) $wpdb->get_var( $wpdb->prepare( "SELECT post_status FROM {$wpdb->posts} WHERE ID=%d LIMIT 1", $order_id ) );
    }

    private function is_frustrated_order( int $order_id ): bool {
        if ( function_exists( 'senderzz_order_context' ) ) {
            $ctx = senderzz_order_context( $order_id );
            return ! empty( $ctx['frustrated'] );
        }
        $status = strtolower( $this->get_order_status( $order_id ) );
        if ( in_array( $status, [ 'wc-frustrado', 'frustrado' ], true ) ) return true;
        $flow = strtolower( (string) $this->get_order_meta_value( $order_id, '_senderzz_motoboy_flow_status' ) );
        return in_array( $flow, [ 'frustrado', 'frustrada' ], true );
    }

    private function is_completed_cod_order( int $order_id ): bool {
        $status = strtolower( $this->get_order_status( $order_id ) );
        return in_array( $status, [ 'wc-completed', 'completed' ], true );
    }

    private function is_financially_settled_order( int $order_id ): bool {
        if ( function_exists( 'senderzz_order_context' ) ) {
            $ctx = senderzz_order_context( $order_id );
            return ! empty( $ctx['financially_settled'] );
        }
        $status = strtolower( $this->get_order_status( $order_id ) );
        return in_array( $status, [ 'wc-completed', 'completed', 'wc-entregue', 'entregue' ], true );
    }

    private function audit_should_ignore_order( int $order_id ): bool {
        if ( function_exists( 'senderzz_audit_order_should_ignore_finance' ) ) {
            return senderzz_audit_order_should_ignore_finance( $order_id );
        }
        if ( $this->is_frustrated_order( $order_id ) ) return true;
        return ! $this->is_financially_settled_order( $order_id );
    }

    private function webhook_failure_count(): int {
        global $wpdb;
        $tables = [
            $wpdb->prefix . 'senderzz_producer_webhook_logs',
            $wpdb->prefix . 'senderzz_webhook_logs',
            $wpdb->prefix . 'sz_webhook_logs',
            $wpdb->prefix . 'wcme_webhook_logs',
        ];
        foreach ( array_unique( $tables ) as $table ) {
            if ( ! $this->table_exists( $table ) ) continue;
            $cols = $wpdb->get_col( "DESC {$table}", 0 );
            $cols = is_array( $cols ) ? $cols : [];
            $code_col = in_array( 'response_code', $cols, true ) ? 'response_code' : ( in_array( 'http_code', $cols, true ) ? 'http_code' : '' );
            if ( ! $code_col ) continue;
            $date_col = in_array( 'fired_at', $cols, true ) ? 'fired_at' : ( in_array( 'created_at', $cols, true ) ? 'created_at' : '' );
            $where = "({$code_col} IS NULL OR {$code_col}<200 OR {$code_col}>=300)";
            if ( $date_col ) $where .= " AND {$date_col} >= DATE_SUB(NOW(), INTERVAL 7 DAY)";
            return (int) $wpdb->get_var( "SELECT COUNT(*) FROM {$table} WHERE {$where}" );
        }
        return 0;
    }

    private function order_count_by_status( array $statuses, ?string $date = null ): int {
        global $wpdb;
        $statuses = array_map( 'sanitize_key', $statuses );
        $in = implode( ',', array_fill( 0, count( $statuses ), '%s' ) );
        if ( $this->table_exists( $wpdb->prefix . 'wc_orders' ) ) {
            $sql = "SELECT COUNT(*) FROM {$wpdb->prefix}wc_orders WHERE status IN ($in) AND type='shop_order'";
            $params = $statuses;
            if ( $date ) { $sql .= ' AND DATE(date_created_gmt)=%s'; $params[] = $date; }
            return (int) $wpdb->get_var( $wpdb->prepare( $sql, $params ) );
        }
        $sql = "SELECT COUNT(*) FROM {$wpdb->posts} WHERE post_type='shop_order' AND post_status IN ($in)";
        $params = $statuses;
        if ( $date ) { $sql .= ' AND DATE(post_date)=%s'; $params[] = $date; }
        return (int) $wpdb->get_var( $wpdb->prepare( $sql, $params ) );
    }


    private function get_order_meta_value( int $order_id, string $key ) {
        global $wpdb;
        $meta = $this->meta_table();
        $order_col = $this->meta_order_col();
        return $wpdb->get_var( $wpdb->prepare( "SELECT meta_value FROM {$meta} WHERE {$order_col}=%d AND meta_key=%s LIMIT 1", $order_id, $key ) );
    }

    private function update_order_meta_value( int $order_id, string $key, $value ): void {
        if ( function_exists( 'wc_get_order' ) ) {
            $order = wc_get_order( $order_id );
            if ( $order ) {
                $order->update_meta_data( $key, $value );
                $order->save();
                return;
            }
        }
        update_post_meta( $order_id, $key, $value );
    }

    private function audit_redirect( string $message = '', bool $warn = false ): void {
        $args = [ 'page' => 'senderzz', 'area' => 'financeiro', 'tab' => 'fin-auditoria' ];
        if ( $message ) {
            $args['sz_msg'] = rawurlencode( $message );
            if ( $warn ) $args['sz_warn'] = 1;
        }
        wp_safe_redirect( add_query_arg( $args, admin_url( 'admin.php' ) ) );
        exit;
    }

    public function handle_audit_fix_all(): void {
        if ( ! current_user_can( 'manage_options' ) && ! current_user_can( 'manage_woocommerce' ) ) wp_die( 'Sem permissão.' );
        check_admin_referer( 'sz_admin_audit_fix_all' );
        $fixed = 0;
        $fixed += $this->fix_missing_affiliate_transactions( 500 );
        $fixed += $this->fix_bad_affiliate_transactions( 500 );
        $fixed += $this->fix_bad_producer_wallet( 500 );
        $fixed += $this->fix_bad_affiliate_wallet_summaries( 500 );
        $this->audit_redirect( 'Auditoria executada. Registros corrigidos: ' . (int) $fixed );
    }

    public function handle_audit_fix_order(): void {
        if ( ! current_user_can( 'manage_options' ) && ! current_user_can( 'manage_woocommerce' ) ) wp_die( 'Sem permissão.' );
        $order_id = absint( $_GET['order_id'] ?? 0 );
        check_admin_referer( 'sz_admin_audit_fix_order_' . $order_id );
        if ( ! $order_id ) $this->audit_redirect( 'Pedido inválido.', true );
        $fixed = 0;
        $fixed += $this->fix_missing_affiliate_transactions( 1, $order_id );
        $fixed += $this->fix_bad_affiliate_transactions( 1, $order_id );
        $fixed += $this->fix_bad_producer_wallet( 1, $order_id );
        $this->audit_redirect( 'Pedido #' . $order_id . ' auditado. Registros corrigidos: ' . (int) $fixed );
    }

    public function handle_fix_affiliate_wallet(): void {
        if ( ! current_user_can( 'manage_options' ) && ! current_user_can( 'manage_woocommerce' ) ) wp_die( 'Sem permissão.' );
        $affiliate_id = absint( $_GET['affiliate_id'] ?? 0 );
        check_admin_referer( 'sz_admin_fix_affiliate_wallet_' . $affiliate_id );
        if ( ! $affiliate_id ) $this->audit_redirect( 'Afiliado inválido.', true );
        $fixed = $this->sync_affiliate_wallet_summary( $affiliate_id );
        wp_safe_redirect( add_query_arg( [ 'page'=>'senderzz', 'area'=>'financeiro', 'tab'=>'fin-livro-cod', 'sz_msg'=>rawurlencode( $fixed ? 'Carteira corrigida.' : 'Nenhuma diferença encontrada.' ) ], admin_url( 'admin.php' ) ) );
        exit;
    }

    private function sync_affiliate_wallet_summary( int $affiliate_id ): int {
        global $wpdb;
        $wallet = function_exists( 'sz_aff_table' ) ? sz_aff_table( 'sz_affiliate_wallet' ) : $wpdb->prefix . 'sz_affiliate_wallet';
        $aff_tx = function_exists( 'sz_aff_table' ) ? sz_aff_table( 'sz_affiliate_transactions' ) : $wpdb->prefix . 'sz_affiliate_transactions';
        if ( ! $affiliate_id || ! $this->table_exists( $wallet ) || ! $this->table_exists( $aff_tx ) ) return 0;

        $calc = $wpdb->get_row( $wpdb->prepare( "SELECT
                SUM(CASE WHEN type='commission' AND status='pending' THEN amount ELSE 0 END) pending_balance,
                SUM(CASE WHEN status='available' THEN amount ELSE 0 END) balance,
                SUM(CASE WHEN type='penalty' AND status IN ('applied','pending') THEN amount ELSE 0 END) debt_amount
            FROM {$aff_tx}
            WHERE affiliate_id=%d", $affiliate_id ), ARRAY_A ) ?: [];
        $pending = round( (float) ( $calc['pending_balance'] ?? 0 ), 2 );
        $balance = round( (float) ( $calc['balance'] ?? 0 ), 2 );
        $debt    = round( (float) ( $calc['debt_amount'] ?? 0 ), 2 );

        $now = current_time( 'mysql' );
        $exists = (int) $wpdb->get_var( $wpdb->prepare( "SELECT COUNT(*) FROM {$wallet} WHERE affiliate_id=%d", $affiliate_id ) );
        if ( ! $exists ) {
            $ok = $wpdb->insert( $wallet, [
                'affiliate_id' => $affiliate_id,
                'balance' => $balance,
                'pending_balance' => $pending,
                'debt_amount' => $debt,
                'created_at' => $now,
                'updated_at' => $now,
            ], [ '%d','%f','%f','%f','%s','%s' ] );
            return $ok === false ? 0 : 1;
        }

        $current = $wpdb->get_row( $wpdb->prepare( "SELECT balance,pending_balance,debt_amount FROM {$wallet} WHERE affiliate_id=%d", $affiliate_id ), ARRAY_A ) ?: [];
        $different = abs( (float) ( $current['pending_balance'] ?? 0 ) - $pending ) > 0.02
            || abs( (float) ( $current['balance'] ?? 0 ) - $balance ) > 0.02
            || abs( (float) ( $current['debt_amount'] ?? 0 ) - $debt ) > 0.02;
        if ( ! $different ) return 0;

        $ok = $wpdb->update( $wallet, [
            'balance' => $balance,
            'pending_balance' => $pending,
            'debt_amount' => $debt,
            'updated_at' => $now,
        ], [ 'affiliate_id' => $affiliate_id ], [ '%f','%f','%f','%s' ], [ '%d' ] );
        return $ok === false ? 0 : 1;
    }

    private function fix_bad_affiliate_wallet_summaries( int $limit = 500 ): int {
        global $wpdb;
        $aff_tb = function_exists( 'sz_aff_table' ) ? sz_aff_table( 'sz_affiliates' ) : $wpdb->prefix . 'sz_affiliates';
        $aff_tx = function_exists( 'sz_aff_table' ) ? sz_aff_table( 'sz_affiliate_transactions' ) : $wpdb->prefix . 'sz_affiliate_transactions';
        if ( ! $this->table_exists( $aff_tb ) || ! $this->table_exists( $aff_tx ) ) return 0;
        $ids = $wpdb->get_col( $wpdb->prepare( "SELECT DISTINCT a.id FROM {$aff_tb} a LEFT JOIN {$aff_tx} t ON t.affiliate_id=a.id WHERE a.deleted_at IS NULL GROUP BY a.id ORDER BY COALESCE(SUM(ABS(t.amount)),0) DESC LIMIT %d", $limit ) ) ?: [];
        $fixed = 0;
        foreach ( $ids as $id ) $fixed += $this->sync_affiliate_wallet_summary( absint( $id ) );
        return $fixed;
    }

    private function fix_missing_affiliate_transactions( int $limit = 500, int $only_order = 0 ): int {
        global $wpdb;
        $aff_tx = function_exists( 'sz_aff_table' ) ? sz_aff_table( 'sz_affiliate_transactions' ) : $wpdb->prefix . 'sz_affiliate_transactions';
        if ( ! $this->table_exists( $aff_tx ) ) return 0;
        $meta = $this->meta_table(); $order_col = $this->meta_order_col();
        $where = $only_order ? $wpdb->prepare( ' AND x.order_id=%d ', $only_order ) : '';
        $sql = "SELECT x.order_id,x.affiliate_id,x.producer_id,x.comissao FROM (SELECT {$order_col} order_id, MAX(CASE WHEN meta_key IN ('_sz_affiliate_id','_sz_affiliate_ref') THEN CAST(meta_value AS UNSIGNED) ELSE 0 END) affiliate_id, MAX(CASE WHEN meta_key='_sz_aff_producer_id' THEN CAST(meta_value AS UNSIGNED) ELSE 0 END) producer_id, MAX(CASE WHEN meta_key='_sz_aff_commission' THEN CAST(meta_value AS DECIMAL(12,2)) ELSE 0 END) comissao FROM {$meta} WHERE meta_key IN ('_sz_affiliate_id','_sz_affiliate_ref','_sz_aff_producer_id','_sz_aff_commission') GROUP BY {$order_col}) x LEFT JOIN {$aff_tx} t ON t.order_id=x.order_id AND t.affiliate_id=x.affiliate_id AND t.type='commission' AND t.status IN ('pending','available') WHERE x.affiliate_id>0 AND x.comissao>0 AND t.id IS NULL {$where} ORDER BY x.order_id DESC LIMIT %d";
        $rows = $wpdb->get_results( $wpdb->prepare( $sql, $limit ), ARRAY_A ) ?: [];
        $now = current_time( 'mysql' );
        $fixed = 0;
        foreach ( $rows as $r ) {
            if ( $this->audit_should_ignore_order( absint( $r['order_id'] ) ) ) continue;
            $producer_id = absint( $r['producer_id'] ?? 0 );
            if ( ! $producer_id && function_exists( 'wc_get_order' ) && function_exists( 'sz_aff_resolve_order_producer_id' ) ) {
                $order = wc_get_order( (int) $r['order_id'] );
                if ( $order ) $producer_id = absint( sz_aff_resolve_order_producer_id( $order ) );
            }
            $available_at = null;
            if ( function_exists( 'sz_aff_producer_retention_days' ) ) {
                $days = max( 0, (int) sz_aff_producer_retention_days( $producer_id ) );
                $available_at = wp_date( 'Y-m-d H:i:s', strtotime( '+' . $days . ' days' ), wp_timezone() );
            }
            $ok = $wpdb->replace( $aff_tx, [
                'affiliate_id' => absint( $r['affiliate_id'] ),
                'producer_id'  => $producer_id,
                'order_id'     => absint( $r['order_id'] ),
                'type'         => 'commission',
                'amount'       => round( (float) $r['comissao'], 2 ),
                'status'       => 'pending',
                'available_at' => $available_at,
                'created_at'   => $now,
                'meta_json'    => wp_json_encode( [ 'source' => 'admin_audit_fix', 'fixed_at' => $now ] ),
            ], [ '%d','%d','%d','%s','%f','%s','%s','%s','%s' ] );
            if ( $ok !== false ) $fixed++;
        }
        return $fixed;
    }

    private function fix_bad_affiliate_transactions( int $limit = 500, int $only_order = 0 ): int {
        global $wpdb;
        $aff_tx = function_exists( 'sz_aff_table' ) ? sz_aff_table( 'sz_affiliate_transactions' ) : $wpdb->prefix . 'sz_affiliate_transactions';
        if ( ! $this->table_exists( $aff_tx ) ) return 0;
        $meta = $this->meta_table(); $order_col = $this->meta_order_col();
        $where = $only_order ? $wpdb->prepare( ' AND t.order_id=%d ', $only_order ) : '';
        $rows = $wpdb->get_results( $wpdb->prepare( "SELECT t.id,t.order_id,m.meta_value oficial FROM {$aff_tx} t JOIN {$meta} m ON m.{$order_col}=t.order_id AND m.meta_key='_sz_aff_commission' WHERE t.type='commission' AND t.status IN ('pending','available') AND ROUND(CAST(t.amount AS DECIMAL(12,2)),2)<>ROUND(CAST(m.meta_value AS DECIMAL(12,2)),2) {$where} ORDER BY t.order_id DESC LIMIT %d", $limit ), ARRAY_A ) ?: [];
        $fixed = 0;
        foreach ( $rows as $r ) {
            if ( $this->audit_should_ignore_order( absint( $r['order_id'] ) ) ) continue;
            $ok = $wpdb->update( $aff_tx, [
                'amount' => round( (float) $r['oficial'], 2 ),
                'meta_json' => wp_json_encode( [ 'source' => 'admin_audit_fix_amount', 'fixed_at' => current_time( 'mysql' ) ] ),
            ], [ 'id' => absint( $r['id'] ) ], [ '%f','%s' ], [ '%d' ] );
            if ( $ok !== false ) $fixed++;
        }
        return $fixed;
    }

    private function fix_bad_producer_wallet( int $limit = 500, int $only_order = 0 ): int {
        global $wpdb;
        $cod_tx = $wpdb->prefix . 'sz_cod_wallet_transactions';
        if ( ! $this->table_exists( $cod_tx ) ) return 0;
        $meta = $this->meta_table(); $order_col = $this->meta_order_col();
        $where = $only_order ? $wpdb->prepare( ' AND w.order_id=%d ', $only_order ) : '';
        $rows = $wpdb->get_results( $wpdb->prepare( "SELECT w.id,w.user_id,w.order_id,m.meta_value oficial FROM {$cod_tx} w JOIN {$meta} m ON m.{$order_col}=w.order_id AND m.meta_key='_sz_prod_commission' WHERE w.type='credit' AND ROUND(CAST(w.net AS DECIMAL(12,2)),2)<>ROUND(CAST(m.meta_value AS DECIMAL(12,2)),2) {$where} ORDER BY w.order_id DESC LIMIT %d", $limit ), ARRAY_A ) ?: [];
        $fixed = 0; $users = [];
        foreach ( $rows as $r ) {
            if ( $this->audit_should_ignore_order( absint( $r['order_id'] ) ) ) continue;
            $valor = round( (float) $r['oficial'], 2 );
            $ok = $wpdb->update( $cod_tx, [
                'gross' => $valor,
                'fee' => 0,
                'net' => $valor,
                'description' => 'Recebimento COD Motoboy do pedido #' . absint( $r['order_id'] ) . ' (corrigido auditoria admin)',
                'updated_at' => current_time( 'mysql' ),
            ], [ 'id' => absint( $r['id'] ) ], [ '%f','%f','%f','%s','%s' ], [ '%d' ] );
            if ( $ok !== false ) { $fixed++; $users[] = absint( $r['user_id'] ); }
        }
        if ( function_exists( 'sz_cod_wallet_invalidate_summary_cache' ) ) {
            foreach ( array_unique( array_filter( $users ) ) as $uid ) sz_cod_wallet_invalidate_summary_cache( $uid );
        }
        return $fixed;
    }

    private function tab_cod_pedidos(): void {
        global $wpdb;
        $status = sanitize_key( $_GET['status'] ?? '' );
        $cidade = sanitize_text_field( wp_unslash( $_GET['cidade'] ?? '' ) );
        $busca  = sanitize_text_field( wp_unslash( $_GET['s'] ?? '' ) );
        $data   = sanitize_text_field( wp_unslash( $_GET['data'] ?? '' ) );
        $stopped_only = ! empty( $_GET['stopped'] );
        $statuses = [ 'wc-agendado','wc-em-rota','wc-em_rota','wc-frustrado','wc-completed','wc-processing' ];
        if ( $stopped_only ) {
            $base = $this->stopped_order_rows();
        } elseif ( $this->table_exists( $wpdb->prefix . 'wc_orders' ) ) {
            $orders = $wpdb->prefix . 'wc_orders';
            $where = [ "o.type='shop_order'" ]; $params = [];
            $where[] = $status ? 'o.status=%s' : "o.status IN ('" . implode( "','", array_map( 'esc_sql', $statuses ) ) . "')";
            if ( $status ) $params[] = $status;
            if ( $data ) { $where[] = 'DATE(o.date_created_gmt)=%s'; $params[] = $data; }
            if ( $busca ) { $where[] = '(o.id=%d OR o.billing_email LIKE %s)'; $params[] = absint( $busca ); $params[] = '%' . $wpdb->esc_like( $busca ) . '%'; }
            $sql = "SELECT o.id pedido,o.status status,o.total_amount total,o.billing_email email FROM {$orders} o WHERE " . implode( ' AND ', $where ) . " ORDER BY o.id DESC LIMIT 150";
            $base = $params ? $wpdb->get_results( $wpdb->prepare( $sql, $params ), ARRAY_A ) : $wpdb->get_results( $sql, ARRAY_A );
        } else {
            $where = [ "p.post_type='shop_order'" ]; $params = [];
            $where[] = $status ? 'p.post_status=%s' : "p.post_status IN ('" . implode( "','", array_map( 'esc_sql', $statuses ) ) . "')";
            if ( $status ) $params[] = $status;
            if ( $data ) { $where[] = 'DATE(p.post_date)=%s'; $params[] = $data; }
            if ( $busca ) { $where[] = '(p.ID=%d OR p.post_title LIKE %s)'; $params[] = absint( $busca ); $params[] = '%' . $wpdb->esc_like( $busca ) . '%'; }
            $sql = "SELECT p.ID pedido,p.post_status status,0 total,'' email FROM {$wpdb->posts} p WHERE " . implode( ' AND ', $where ) . " ORDER BY p.ID DESC LIMIT 150";
            $base = $params ? $wpdb->get_results( $wpdb->prepare( $sql, $params ), ARRAY_A ) : $wpdb->get_results( $sql, ARRAY_A );
        }
        $rows = [];
        foreach ( $base ?: [] as $r ) {
            $oid = absint( $r['pedido'] );
            $city = (string) ( $this->get_order_meta_value( $oid, '_billing_city' ) ?: $this->get_order_meta_value( $oid, 'billing_city' ) );
            if ( $cidade && stripos( $city, $cidade ) === false ) continue;
            $cliente = (string) trim( ( $this->get_order_meta_value( $oid, '_billing_first_name' ) ?: '' ) . ' ' . ( $this->get_order_meta_value( $oid, '_billing_last_name' ) ?: '' ) );
            $produto = (string) ( $this->get_order_meta_value( $oid, '_senderzz_offer_name' ) ?: $this->get_order_meta_value( $oid, '_sz_offer_name' ) );
            $aff_id = absint( $this->get_order_meta_value( $oid, '_sz_affiliate_id' ) ?: $this->get_order_meta_value( $oid, '_sz_affiliate_ref' ) );
            $aff_nome = $aff_id ? ( function_exists( 'sz_aff_get_affiliate_display_name' ) ? sz_aff_get_affiliate_display_name( $aff_id ) : '#' . $aff_id ) : '—';
            $rows[] = [
                'pedido' => $oid,
                'cliente' => $cliente ?: ( $r['email'] ?: 'Cliente' ),
                'cidade' => $city ?: '—',
                'status' => $r['status'] ?? '—',
                'produto' => $produto ?: '—',
                'afiliado' => $aff_nome,
                'comissao' => (float) $this->get_order_meta_value( $oid, '_sz_aff_commission' ),
                'acoes' => $oid,
            ];
        }
        ?>
        <form class="sz-filterbar" method="get">
            <input type="hidden" name="page" value="senderzz"><input type="hidden" name="area" value="cod"><input type="hidden" name="tab" value="cod-pedidos">
            <?php if ( $stopped_only ) : ?><input type="hidden" name="stopped" value="1"><?php endif; ?>
            <input type="date" name="data" value="<?php echo esc_attr( $data ); ?>">
            <select name="status"><option value="">Todos os status</option><?php foreach ( $statuses as $st ) echo '<option value="' . esc_attr( $st ) . '" ' . selected( $status, $st, false ) . '>' . esc_html( str_replace( 'wc-', '', $st ) ) . '</option>'; ?></select>
            <input type="text" name="cidade" placeholder="Cidade" value="<?php echo esc_attr( $cidade ); ?>">
            <input type="search" name="s" placeholder="Busca por pedido/e-mail" value="<?php echo esc_attr( $busca ); ?>">
            <button class="button button-primary">Filtrar</button>
        </form>
        <?php
        $this->render_table( 'Pedidos COD Motoboy', $rows, [ 'pedido'=>'Pedido', 'cliente'=>'Cliente', 'cidade'=>'Cidade', 'status'=>'Status', 'produto'=>'Produto', 'afiliado'=>'Afiliado', 'comissao'=>'Comissão', 'acoes'=>'Ações' ], function( $row, $key ) {
            if ( $key === 'status' ) return '<span class="sz-status-pill">' . esc_html( str_replace( 'wc-', '', (string) $row['status'] ) ) . '</span>';
            if ( $key === 'acoes' ) return '<div class="sz-actions"><a class="button button-small" href="' . esc_url( $this->order_admin_url( absint( $row['pedido'] ) ) ) . '">Abrir</a><a class="button button-small button-primary" href="' . esc_url( wp_nonce_url( admin_url( 'admin-post.php?action=sz_admin_audit_fix_order&order_id=' . absint( $row['pedido'] ) ), 'sz_admin_audit_fix_order_' . absint( $row['pedido'] ) ) ) . '">Auditar</a></div>';
            return null;
        } );
    }


    private function stopped_order_rows( int $hours = 24 ): array {
        global $wpdb;
        $meta = $this->meta_table();
        $order_col = $this->meta_order_col();
        if ( ! $this->table_exists( $meta ) ) return [];

        $threshold = gmdate( 'Y-m-d H:i:s', time() - ( $hours * HOUR_IN_SECONDS ) );
        $rows = [];

        if ( $this->table_exists( $wpdb->prefix . 'wc_orders' ) ) {
            $orders = $wpdb->prefix . 'wc_orders';
            $sql = $wpdb->prepare(
                "SELECT DISTINCT m.{$order_col} pedido, COALESCE(o.status,'') status, COALESCE(o.total_amount,0) total, COALESCE(o.billing_email,'') email
                 FROM {$meta} m
                 INNER JOIN {$orders} o ON o.id = m.{$order_col}
                 WHERE m.meta_key = '_senderzz_motoboy_flow_status'
                   AND m.meta_value IN ('agendado','embalado','em_rota','em-rota')
                   AND COALESCE(o.status,'') NOT IN ('wc-completed','wc-entregue','wc-frustrado','wc-cancelled','wc-refunded','wc-failed')
                   AND COALESCE(o.date_updated_gmt,o.date_created_gmt) < %s
                 ORDER BY COALESCE(o.date_updated_gmt,o.date_created_gmt) ASC
                 LIMIT 100",
                $threshold
            );
            $rows = $wpdb->get_results( $sql, ARRAY_A ) ?: [];
        } else {
            $sql = $wpdb->prepare(
                "SELECT DISTINCT m.{$order_col} pedido, COALESCE(p.post_status,'') status, 0 total, '' email
                 FROM {$meta} m
                 INNER JOIN {$wpdb->posts} p ON p.ID = m.{$order_col}
                 WHERE m.meta_key = '_senderzz_motoboy_flow_status'
                   AND m.meta_value IN ('agendado','embalado','em_rota','em-rota')
                   AND COALESCE(p.post_status,'') NOT IN ('wc-completed','wc-entregue','wc-frustrado','wc-cancelled','wc-refunded','wc-failed')
                   AND COALESCE(p.post_modified_gmt,p.post_date_gmt) < %s
                 ORDER BY COALESCE(p.post_modified_gmt,p.post_date_gmt) ASC
                 LIMIT 100",
                $threshold
            );
            $rows = $wpdb->get_results( $sql, ARRAY_A ) ?: [];
        }
        return $rows;
    }

    private function tab_overview_operacao(): void {
        global $wpdb;
        $today = current_time( 'Y-m-d' );
        $meta = $this->meta_table();
        $order_col = $this->meta_order_col();
        $alerts = $this->get_audit_counts();
        $webhook_fail = $this->webhook_failure_count();
        $stopped_rows = $this->stopped_order_rows();
        $stopped = count( $stopped_rows );
        ?>
        <div class="sz-kpi-grid">
            <div class="sz-kpi"><div class="sz-kpi-label">Pedidos Hoje</div><div class="sz-kpi-val"><?php echo $this->order_count_by_status( [ 'wc-processing','wc-agendado','wc-completed','wc-frustrado' ], $today ); ?></div><div class="sz-kpi-sub">Operação do dia</div></div>
            <div class="sz-kpi warn"><div class="sz-kpi-label">Agendados</div><div class="sz-kpi-val"><?php echo $this->order_count_by_status( [ 'wc-agendado' ] ); ?></div><div class="sz-kpi-sub">Aguardando separação/rota</div></div>
            <div class="sz-kpi"><div class="sz-kpi-label">Em Rota</div><div class="sz-kpi-val"><?php echo $this->order_count_by_status( [ 'wc-em-rota','wc-em_rota' ] ); ?></div><div class="sz-kpi-sub">Motoboy em entrega</div></div>
            <div class="sz-kpi ok"><div class="sz-kpi-label">Entregues</div><div class="sz-kpi-val"><?php echo $this->order_count_by_status( [ 'wc-completed','wc-entregue' ], $today ); ?></div><div class="sz-kpi-sub">Concluídos hoje</div></div>
            <div class="sz-kpi"><div class="sz-kpi-label">Frustrados</div><div class="sz-kpi-val"><?php echo $this->order_count_by_status( [ 'wc-frustrado' ], $today ); ?></div><div class="sz-kpi-sub">Ocorrências hoje</div></div>
            <div class="sz-kpi <?php echo ( array_sum( $alerts ) + $webhook_fail + $stopped ) ? 'warn' : 'ok'; ?>"><div class="sz-kpi-label">Alertas</div><div class="sz-kpi-val"><?php echo (int) ( array_sum( $alerts ) + $webhook_fail + $stopped ); ?></div><div class="sz-kpi-sub">Financeiro e operação</div></div>
        </div>
        <?php
        $financial_alerts = (int) array_sum( $alerts ) + (int) $webhook_fail;
        $alert_url = $financial_alerts ? admin_url( 'admin.php?page=senderzz&area=financeiro&tab=fin-auditoria' ) : admin_url( 'admin.php?page=senderzz&area=cod&tab=cod-pedidos&stopped=1' );
        $alert_label = $financial_alerts ? 'Abrir auditoria' : 'Ver pedidos';
        ?>
        <div class="sz-section"><div class="sz-section-head"><h3>Alertas operacionais</h3><a class="button button-primary" href="<?php echo esc_url( $alert_url ); ?>"><?php echo esc_html( $alert_label ); ?></a></div><div class="sz-section-body">
            <?php $this->alert_line( 'Saldo divergente', $alerts['wallet'] ?? 0 ); ?>
            <?php $this->alert_line( 'Afiliado sem transação', $alerts['aff_missing'] ?? 0 ); ?>
            <?php $this->alert_line( 'Wallet divergente', $alerts['aff_bad'] ?? 0 ); ?>
            <?php $this->alert_line( 'Split financeiro divergente', $alerts['split'] ?? 0 ); ?>
            <?php $this->alert_line( 'Webhook falhando', $webhook_fail ); ?>
            <?php $this->alert_line( 'Pedido parado', $stopped, admin_url( 'admin.php?page=senderzz&area=cod&tab=cod-pedidos&stopped=1' ) ); ?>
        </div></div>
        <?php
    }

    private function alert_line( string $label, int $count, string $url = '' ): void {
        $badge = '<span class="sz-status-pill ' . ( $count ? 'warn' : 'ok' ) . '">' . ( $count ? '⚠ ' . (int) $count : 'OK' ) . '</span>';
        if ( $count && $url ) {
            $badge = '<a class="sz-status-pill warn" href="' . esc_url( $url ) . '">⚠ ' . (int) $count . '</a>';
        }
        echo '<div style="display:flex;align-items:center;justify-content:space-between;padding:10px 0;border-bottom:1px solid #f1f5f9"><strong>' . esc_html( $label ) . '</strong>' . $badge . '</div>';
    }

    private function get_audit_counts(): array {
        if ( function_exists( 'senderzz_audit_counts' ) ) {
            return senderzz_audit_counts();
        }
        $out = [ 'split' => 0, 'aff_bad' => 0, 'aff_missing' => 0, 'wallet' => 0 ];
        foreach ( $this->audit_problem_rows() as $row ) {
            $tipo = (string) ( $row['tipo_key'] ?? $row['tipo'] ?? '' );
            if ( isset( $out[ $tipo ] ) ) $out[ $tipo ]++;
        }
        return $out;
    }

    private function tab_fin_produtores(): void {
        global $wpdb;
        $meta = $this->meta_table(); $order_col = $this->meta_order_col();
        if ( ! $this->table_exists( $meta ) ) { $this->empty_module( 'Metas de pedidos não encontradas.' ); return; }

        $status_join = '';
        $status_expr = "''";
        if ( $this->table_exists( $wpdb->prefix . 'wc_orders' ) ) {
            $status_join = "LEFT JOIN {$wpdb->prefix}wc_orders o ON o.id=x.order_id";
            $status_expr = "COALESCE(o.status,'')";
        } else {
            $status_join = "LEFT JOIN {$wpdb->posts} o ON o.ID=x.order_id";
            $status_expr = "COALESCE(o.post_status,'')";
        }

        $rows = $wpdb->get_results( "SELECT x.producer_id, COALESCE(u.display_name, CONCAT('Produtor #', x.producer_id)) nome,
                SUM(CASE WHEN {$status_expr} IN ('wc-frustrado','frustrado','wc-frustracao','frustracao','wc-cancelled','cancelled','wc-cancelado','cancelado','wc-failed','failed') THEN 0 ELSE x.bruto END) bruto,
                SUM(CASE WHEN {$status_expr} IN ('wc-frustrado','frustrado','wc-frustracao','frustracao','wc-cancelled','cancelled','wc-cancelado','cancelado','wc-failed','failed') THEN 0 ELSE x.aff END) afiliados,
                SUM(CASE WHEN {$status_expr} IN ('wc-frustrado','frustrado','wc-frustracao','frustracao','wc-cancelled','cancelled','wc-cancelado','cancelado','wc-failed','failed') THEN 0 ELSE x.taxa END) taxas_senderzz,
                SUM(CASE WHEN {$status_expr} IN ('wc-frustrado','frustrado','wc-frustracao','frustracao','wc-cancelled','cancelled','wc-cancelado','cancelado','wc-failed','failed') THEN 0 ELSE x.prod END) liquido_produtor,
                SUM(CASE WHEN {$status_expr} IN ('wc-frustrado','frustrado','wc-frustracao','frustracao','wc-cancelled','cancelled','wc-cancelado','cancelado','wc-failed','failed') THEN x.bruto ELSE 0 END) potencial_frustrado,
                SUM(CASE WHEN {$status_expr} NOT IN ('wc-completo','completo','wc-completed','completed','wc-frustrado','frustrado','wc-frustracao','frustracao','wc-cancelled','cancelled','wc-cancelado','cancelado','wc-failed','failed') THEN x.bruto ELSE 0 END) previsto_valor,
                COUNT(*) pedidos,
                SUM(CASE WHEN {$status_expr} IN ('wc-completo','completo','wc-completed','completed') THEN 1 ELSE 0 END) entregues,
                SUM(CASE WHEN {$status_expr} IN ('wc-frustrado','frustrado','wc-frustracao','frustracao','wc-cancelled','cancelled','wc-cancelado','cancelado','wc-failed','failed') THEN 1 ELSE 0 END) frustrados,
                SUM(CASE WHEN {$status_expr} NOT IN ('wc-completo','completo','wc-completed','completed','wc-frustrado','frustrado','wc-frustracao','frustracao','wc-cancelled','cancelled','wc-cancelado','cancelado','wc-failed','failed') THEN 1 ELSE 0 END) previstos
            FROM (
                SELECT {$order_col} order_id,
                    MAX(CASE WHEN meta_key IN ('_sz_aff_producer_id','_senderzz_owner_user_id','_sz_portal_user_id','_senderzz_producer_id','_senderzz_checkout_user_id') THEN CAST(meta_value AS UNSIGNED) ELSE 0 END) producer_id,
                    MAX(CASE WHEN meta_key IN ('_sz_aff_gross','_senderzz_offer_value') THEN CAST(meta_value AS DECIMAL(12,2)) ELSE 0 END) bruto,
                    MAX(CASE WHEN meta_key='_sz_aff_commission' THEN CAST(meta_value AS DECIMAL(12,2)) ELSE 0 END) aff,
                    MAX(CASE WHEN meta_key='_sz_mb_taxa_total' THEN CAST(meta_value AS DECIMAL(12,2)) ELSE 0 END) taxa,
                    MAX(CASE WHEN meta_key='_sz_prod_commission' THEN CAST(meta_value AS DECIMAL(12,2)) ELSE 0 END) prod
                FROM {$meta}
                WHERE meta_key IN ('_sz_aff_producer_id','_senderzz_owner_user_id','_sz_portal_user_id','_senderzz_producer_id','_senderzz_checkout_user_id','_sz_aff_gross','_senderzz_offer_value','_sz_aff_commission','_sz_mb_taxa_total','_sz_prod_commission')
                GROUP BY {$order_col}
            ) x
            {$status_join}
            LEFT JOIN {$wpdb->users} u ON u.ID=x.producer_id
            WHERE x.producer_id>0 OR x.prod>0 OR x.taxa>0 OR x.aff>0
            GROUP BY x.producer_id
            ORDER BY liquido_produtor DESC, bruto DESC
            LIMIT 150", ARRAY_A ) ?: [];

        $tot_bruto = array_sum( array_map( static fn($r) => (float) $r['bruto'], $rows ) );
        $tot_aff   = array_sum( array_map( static fn($r) => (float) $r['afiliados'], $rows ) );
        $tot_tax   = array_sum( array_map( static fn($r) => (float) $r['taxas_senderzz'], $rows ) );
        $tot_prod  = array_sum( array_map( static fn($r) => (float) $r['liquido_produtor'], $rows ) );
        $tot_prev  = array_sum( array_map( static fn($r) => (float) ( $r['previsto_valor'] ?? 0 ), $rows ) );
        echo '<div class="sz-kpi-grid">';
        echo '<div class="sz-kpi"><div class="sz-kpi-label">Bruto COD</div><div class="sz-kpi-val" style="font-size:var(--sz-text-3xl)">' . esc_html( $this->money( $tot_bruto ) ) . '</div><div class="sz-kpi-sub">sem frustrados</div></div>';
        echo '<div class="sz-kpi warn"><div class="sz-kpi-label">Afiliados</div><div class="sz-kpi-val" style="font-size:var(--sz-text-3xl)">' . esc_html( $this->money( $tot_aff ) ) . '</div><div class="sz-kpi-sub">repasse</div></div>';
        echo '<div class="sz-kpi"><div class="sz-kpi-label">Taxas Senderzz</div><div class="sz-kpi-val" style="font-size:var(--sz-text-3xl)">' . esc_html( $this->money( $tot_tax ) ) . '</div><div class="sz-kpi-sub">operação</div></div>';
        echo '<div class="sz-kpi ok"><div class="sz-kpi-label">Líquido produtor</div><div class="sz-kpi-val" style="font-size:var(--sz-text-3xl)">' . esc_html( $this->money( $tot_prod ) ) . '</div><div class="sz-kpi-sub">líquido</div></div>';
        echo '<div class="sz-kpi warn"><div class="sz-kpi-label">Previsto produtor</div><div class="sz-kpi-val" style="font-size:var(--sz-text-3xl)">' . esc_html( $this->money( $tot_prev ) ) . '</div><div class="sz-kpi-sub">agendados/em aberto</div></div>';
        echo '</div>';
        $this->render_table( 'Produtores', $rows, [ 'nome'=>'Produtor', 'bruto'=>'Bruto COD', 'afiliados'=>'Afiliados', 'taxas_senderzz'=>'Taxas Senderzz', 'liquido_produtor'=>'Líquido produtor', 'previsto_valor'=>'Previsto', 'potencial_frustrado'=>'Potencial frustrado', 'pedidos'=>'Pedidos', 'entregues'=>'Entregues', 'previstos'=>'Previstos', 'frustrados'=>'Frustrados' ] );
    }

    private function tab_fin_afiliados(): void {
        global $wpdb;
        $aff_tb = function_exists( 'sz_aff_table' ) ? sz_aff_table( 'sz_affiliates' ) : $wpdb->prefix . 'sz_affiliates';
        $aff_tx = function_exists( 'sz_aff_table' ) ? sz_aff_table( 'sz_affiliate_transactions' ) : $wpdb->prefix . 'sz_affiliate_transactions';
        $wallet = function_exists( 'sz_aff_table' ) ? sz_aff_table( 'sz_affiliate_wallet' ) : $wpdb->prefix . 'sz_affiliate_wallet';
        if ( ! $this->table_exists( $aff_tb ) ) { $this->empty_module( 'Nenhum afiliado encontrado.' ); return; }

        $has_tx     = $this->table_exists( $aff_tx );
        $has_wallet = $this->table_exists( $wallet );
        $meta       = $this->meta_table();
        $order_col  = $this->meta_order_col();

        // Uma única tela/fonte para afiliados: cadastro + pedidos vinculados + repasses.
        // Pedido = todo pedido que possui vínculo de afiliado. Repasse = somente transação ativa.
        // Assim o número de pedidos não some quando o pedido ainda está agendado, frustrado ou sem transação liberada.
        $join_order_count = $this->table_exists( $meta ) ? "LEFT JOIN (
                SELECT CAST(meta_value AS UNSIGNED) affiliate_id, COUNT(DISTINCT {$order_col}) pedidos
                FROM {$meta}
                WHERE meta_key IN ('_sz_affiliate_id','_sz_affiliate_ref')
                  AND CAST(meta_value AS UNSIGNED)>0
                GROUP BY CAST(meta_value AS UNSIGNED)
            ) oc ON oc.affiliate_id=a.id" : '';

        $tx_join = $has_tx ? "LEFT JOIN (
                SELECT affiliate_id,
                    SUM(CASE WHEN type='commission' AND status='pending' THEN amount ELSE 0 END) tx_pendente,
                    SUM(CASE WHEN type='commission' AND status='available' THEN amount ELSE 0 END) tx_disponivel,
                    SUM(CASE WHEN type='withdrawal' THEN ABS(amount) ELSE 0 END) saques,
                    SUM(CASE WHEN type='penalty' THEN ABS(amount) ELSE 0 END) penalidades,
                    COUNT(DISTINCT CASE WHEN type='commission' AND status IN ('pending','available') THEN order_id END) pedidos_com_repasse
                FROM {$aff_tx}
                GROUP BY affiliate_id
            ) tx ON tx.affiliate_id=a.id" : '';

        $wallet_join = $has_wallet ? "LEFT JOIN {$wallet} w ON w.affiliate_id=a.id" : '';
        $wallet_cols = $has_wallet ? 'COALESCE(w.pending_balance,0) pendente_wallet, COALESCE(w.balance,0) disponivel_wallet,' : '0 pendente_wallet, 0 disponivel_wallet,';
        $div_expr    = ( $has_tx && $has_wallet ) ? "CASE WHEN ABS(COALESCE(tx.tx_pendente,0)-COALESCE(w.pending_balance,0))>0.02 OR ABS(COALESCE(tx.tx_disponivel,0)-COALESCE(w.balance,0))>0.02 THEN 1 ELSE 0 END" : '0';

        $rows = $wpdb->get_results( "SELECT a.id affiliate_id,
                COALESCE(NULLIF(u.display_name,''), CONCAT('Afiliado #', a.id)) nome,
                u.user_email email,
                COALESCE(NULLIF(p.display_name,''), CASE WHEN a.producer_id=15 THEN 'Gabriel Campos' ELSE CONCAT('Produtor #', a.producer_id) END) produtor,
                a.commission_pct,
                COALESCE(oc.pedidos,0) pedidos,
                COALESCE(tx.pedidos_com_repasse,0) pedidos_com_repasse,
                COALESCE(tx.tx_pendente,0) pendente,
                COALESCE(tx.tx_disponivel,0) disponivel,
                COALESCE(tx.saques,0) saques,
                COALESCE(tx.penalidades,0) penalidades,
                {$wallet_cols}
                {$div_expr} divergencia
            FROM {$aff_tb} a
            LEFT JOIN {$wpdb->users} u ON u.ID=a.user_id
            LEFT JOIN {$wpdb->users} p ON p.ID=a.producer_id
            {$join_order_count}
            {$tx_join}
            {$wallet_join}
            WHERE a.deleted_at IS NULL
              AND a.status='active'
            ORDER BY COALESCE(oc.pedidos,0) DESC, (COALESCE(tx.tx_pendente,0)+COALESCE(tx.tx_disponivel,0)) DESC, a.id DESC
            LIMIT 300", ARRAY_A ) ?: [];

        $active       = count( $rows );
        $total_orders = array_sum( array_map( static fn($r) => (int) ( $r['pedidos'] ?? 0 ), $rows ) );
        $tot_pendente = array_sum( array_map( static fn($r) => (float) ( $r['pendente'] ?? 0 ), $rows ) );
        $tot_disp     = array_sum( array_map( static fn($r) => (float) ( $r['disponivel'] ?? 0 ), $rows ) );
        $tot_div      = array_sum( array_map( static fn($r) => (int) ( $r['divergencia'] ?? 0 ), $rows ) );

        echo '<div class="sz-notice">Menu unificado: afiliados ativos, produtor, comissão, quantidade real de pedidos e repasses em uma única visão. O contador de pedidos vem do vínculo do pedido; os valores vêm das transações ativas de repasse.</div>';
        echo '<div class="sz-kpi-grid">';
        echo '<div class="sz-kpi ok"><div class="sz-kpi-label">Afiliados ativos</div><div class="sz-kpi-val" style="font-size:var(--sz-text-3xl)">' . (int) $active . '</div><div class="sz-kpi-sub">cadastros aprovados</div></div>';
        echo '<div class="sz-kpi"><div class="sz-kpi-label">Pedidos vinculados</div><div class="sz-kpi-val" style="font-size:var(--sz-text-3xl)">' . (int) $total_orders . '</div><div class="sz-kpi-sub">contagem real por afiliado</div></div>';
        echo '<div class="sz-kpi warn"><div class="sz-kpi-label">Pendente</div><div class="sz-kpi-val" style="font-size:var(--sz-text-3xl)">' . esc_html( $this->money( $tot_pendente ) ) . '</div><div class="sz-kpi-sub">retenção/liberação</div></div>';
        echo '<div class="sz-kpi ok"><div class="sz-kpi-label">Disponível</div><div class="sz-kpi-val" style="font-size:var(--sz-text-3xl)">' . esc_html( $this->money( $tot_disp ) ) . '</div><div class="sz-kpi-sub">pode sacar</div></div>';
        echo '</div>';

        if ( $tot_div ) {
            echo '<div class="sz-notice sz-warn">Há ' . (int) $tot_div . ' afiliado(s) com diferença técnica entre transações e saldo da carteira. A coluna Ação aparece somente nesses casos.</div>';
        }

        $this->render_table( 'Afiliados e repasses', $rows, [
            'affiliate_id'      => 'ID',
            'nome'              => 'Afiliado',
            'email'             => 'E-mail',
            'produtor'          => 'Produtor',
            'commission_pct'    => 'Comissão %',
            'pedidos'           => 'Pedidos',
            'pendente'          => 'Pendente',
            'disponivel'        => 'Disponível',
            'saques'            => 'Saques',
            'penalidades'       => 'Penalidades',
            'acao'              => 'Ação',
        ], function( $row, $key ) {
            if ( in_array( $key, [ 'pendente', 'disponivel', 'saques', 'penalidades' ], true ) ) {
                return esc_html( $this->money( (float) ( $row[ $key ] ?? 0 ) ) );
            }
            if ( $key === 'commission_pct' ) {
                return esc_html( number_format( (float) ( $row['commission_pct'] ?? 0 ), 2, ',', '.' ) . '%' );
            }
            if ( $key === 'pedidos' ) {
                $pedidos = (int) ( $row['pedidos'] ?? 0 );
                $repasse = (int) ( $row['pedidos_com_repasse'] ?? 0 );
                if ( $pedidos > $repasse && $repasse > 0 ) {
                    return esc_html( $pedidos . ' (' . $repasse . ' com repasse)' );
                }
                return esc_html( (string) $pedidos );
            }
            if ( $key === 'acao' ) {
                if ( ! (int) ( $row['divergencia'] ?? 0 ) ) return '—';
                $id = absint( $row['affiliate_id'] ?? 0 );
                $tech = 'Tx pendente: ' . $this->money( (float) ( $row['pendente'] ?? 0 ) ) . ' · Carteira pendente: ' . $this->money( (float) ( $row['pendente_wallet'] ?? 0 ) ) . ' · Tx disp.: ' . $this->money( (float) ( $row['disponivel'] ?? 0 ) ) . ' · Carteira disp.: ' . $this->money( (float) ( $row['disponivel_wallet'] ?? 0 ) );
                return '<a class="button button-small button-primary" title="' . esc_attr( $tech ) . '" href="' . esc_url( wp_nonce_url( admin_url( 'admin-post.php?action=sz_admin_fix_affiliate_wallet&affiliate_id=' . $id ), 'sz_admin_fix_affiliate_wallet_' . $id ) ) . '">Corrigir carteira</a>';
            }
            return null;
        } );
    }

    private function tab_fin_carteira_afiliados(): void {
        global $wpdb;
        $aff_tb = function_exists( 'sz_aff_table' ) ? sz_aff_table( 'sz_affiliates' ) : $wpdb->prefix . 'sz_affiliates';
        $aff_tx = function_exists( 'sz_aff_table' ) ? sz_aff_table( 'sz_affiliate_transactions' ) : $wpdb->prefix . 'sz_affiliate_transactions';
        $wallet = function_exists( 'sz_aff_table' ) ? sz_aff_table( 'sz_affiliate_wallet' ) : $wpdb->prefix . 'sz_affiliate_wallet';
        if ( ! $this->table_exists( $aff_tb ) ) { $this->empty_module( 'Tabela de afiliados ausente.' ); return; }

        $has_tx     = $this->table_exists( $aff_tx );
        $has_wallet = $this->table_exists( $wallet );
        $tx_join = $has_tx ? "LEFT JOIN (SELECT affiliate_id,
                SUM(CASE WHEN type='commission' AND status='pending' THEN amount ELSE 0 END) tx_pendente,
                SUM(CASE WHEN type='commission' AND status='available' THEN amount ELSE 0 END) tx_disponivel,
                SUM(CASE WHEN type='withdrawal' THEN ABS(amount) ELSE 0 END) saques,
                SUM(CASE WHEN type='penalty' THEN ABS(amount) ELSE 0 END) penalidades,
                COUNT(DISTINCT CASE WHEN type='commission' AND status IN ('pending','available') THEN order_id END) pedidos_validos
            FROM {$aff_tx} GROUP BY affiliate_id) tx ON tx.affiliate_id=a.id" : '';
        $wallet_join = $has_wallet ? "LEFT JOIN {$wallet} w ON w.affiliate_id=a.id" : '';
        $wallet_cols = $has_wallet ? 'COALESCE(w.pending_balance,0) pendente_wallet, COALESCE(w.balance,0) disponivel_wallet' : '0 pendente_wallet, 0 disponivel_wallet';
        $div_expr = ( $has_tx && $has_wallet ) ? "CASE WHEN ABS(COALESCE(tx.tx_pendente,0)-COALESCE(w.pending_balance,0))>0.02 OR ABS(COALESCE(tx.tx_disponivel,0)-COALESCE(w.balance,0))>0.02 THEN 1 ELSE 0 END" : '0';
        $rows = $wpdb->get_results( "SELECT a.id affiliate_id, COALESCE(NULLIF(u.display_name,''), CONCAT('Afiliado #', a.id)) nome,
                COALESCE(tx.tx_pendente,0) pendente, COALESCE(tx.tx_disponivel,0) disponivel,
                {$wallet_cols},
                COALESCE(tx.saques,0) saques, COALESCE(tx.penalidades,0) penalidades, COALESCE(tx.pedidos_validos,0) pedidos,
                {$div_expr} divergencia
            FROM {$aff_tb} a
            LEFT JOIN {$wpdb->users} u ON u.ID=a.user_id
            {$wallet_join}
            {$tx_join}
            WHERE a.deleted_at IS NULL
            ORDER BY (COALESCE(tx.tx_pendente,0)+COALESCE(tx.tx_disponivel,0)) DESC, a.id DESC
            LIMIT 300", ARRAY_A ) ?: [];

        $tot_pendente = array_sum( array_map( static fn($r) => (float) ( $r['pendente'] ?? 0 ), $rows ) );
        $tot_disp     = array_sum( array_map( static fn($r) => (float) ( $r['disponivel'] ?? 0 ), $rows ) );
        $tot_div      = array_sum( array_map( static fn($r) => (int) ( $r['divergencia'] ?? 0 ), $rows ) );
        echo '<div class="sz-notice">Repasses de afiliados agora usam somente transações ativas. Comissões canceladas/estornadas não entram nos totais. As colunas técnicas da carteira ficam escondidas; aparecem apenas quando há divergência.</div>';
        echo '<div class="sz-kpi-grid">';
        echo '<div class="sz-kpi warn"><div class="sz-kpi-label">Pendente</div><div class="sz-kpi-val" style="font-size:var(--sz-text-3xl)">' . esc_html( $this->money( $tot_pendente ) ) . '</div><div class="sz-kpi-sub">retenção/liberação</div></div>';
        echo '<div class="sz-kpi ok"><div class="sz-kpi-label">Disponível</div><div class="sz-kpi-val" style="font-size:var(--sz-text-3xl)">' . esc_html( $this->money( $tot_disp ) ) . '</div><div class="sz-kpi-sub">pode sacar</div></div>';
        echo '</div>';
        $this->render_table( 'Repasses de afiliados', $rows, [ 'affiliate_id'=>'ID', 'nome'=>'Afiliado', 'pendente'=>'Pendente', 'disponivel'=>'Disponível', 'saques'=>'Saques', 'penalidades'=>'Penalidades', 'pedidos'=>'Pedidos válidos' ], function( $row, $key ) {
            if ( $key === 'divergencia' ) {
                if ( ! (int) ( $row['divergencia'] ?? 0 ) ) return '<span class="sz-status-pill ok">OK</span>';
                $tech = 'Tx pendente: ' . $this->money( (float) ( $row['pendente'] ?? 0 ) ) . ' · Carteira pendente: ' . $this->money( (float) ( $row['pendente_wallet'] ?? 0 ) ) . ' · Tx disp.: ' . $this->money( (float) ( $row['disponivel'] ?? 0 ) ) . ' · Carteira disp.: ' . $this->money( (float) ( $row['disponivel_wallet'] ?? 0 ) );
                return '<span class="sz-status-pill warn" title="' . esc_attr( $tech ) . '">⚠ Verificar</span>';
            }
            if ( $key === 'acao' ) {
                if ( ! (int) ( $row['divergencia'] ?? 0 ) ) return '—';
                $id = absint( $row['affiliate_id'] ?? 0 );
                return '<a class="button button-small button-primary" href="' . esc_url( wp_nonce_url( admin_url( 'admin-post.php?action=sz_admin_fix_affiliate_wallet&affiliate_id=' . $id ), 'sz_admin_fix_affiliate_wallet_' . $id ) ) . '">Corrigir carteira</a>';
            }
            return null;
        } );
    }

    private function tab_fin_taxas(): void {
        global $wpdb;
        $meta = $this->meta_table();
        $order_col = $this->meta_order_col();
        if ( ! $this->table_exists( $meta ) ) { $this->empty_module( 'Metas financeiras ausentes.' ); return; }

        $tz = new \DateTimeZone( 'America/Sao_Paulo' );
        $hoje = new \DateTimeImmutable( 'now', $tz );
        $default_ate = $hoje->format( 'Y-m-d' );
        $default_de  = $hoje->modify( '-7 days' )->format( 'Y-m-d' );
        $data_de = sanitize_text_field( $_GET['fin_de'] ?? $default_de );
        $data_ate = sanitize_text_field( $_GET['fin_ate'] ?? $default_ate );
        if ( ! preg_match( '/^\d{4}-\d{2}-\d{2}$/', $data_de ) ) { $data_de = $default_de; }
        if ( ! preg_match( '/^\d{4}-\d{2}-\d{2}$/', $data_ate ) ) { $data_ate = $default_ate; }
        if ( strtotime( $data_de ) > strtotime( $data_ate ) ) { $tmp = $data_de; $data_de = $data_ate; $data_ate = $tmp; }

        $orders_table = $wpdb->prefix . 'wc_orders';
        if ( $this->table_exists( $orders_table ) ) {
            $status_join = "LEFT JOIN {$orders_table} o ON o.id=x.pedido";
            $status_expr = "COALESCE(o.status,'')";
            $date_expr   = "o.date_created_gmt";
        } else {
            $status_join = "LEFT JOIN {$wpdb->posts} o ON o.ID=x.pedido";
            $status_expr = "COALESCE(o.post_status,'')";
            $date_expr   = "o.post_date_gmt";
        }

        $aff_tb = function_exists( 'sz_aff_table' ) ? sz_aff_table( 'sz_affiliates' ) : $wpdb->prefix . 'sz_affiliates';
        $aff_tx = function_exists( 'sz_aff_table' ) ? sz_aff_table( 'sz_affiliate_transactions' ) : $wpdb->prefix . 'sz_affiliate_transactions';
        $has_aff = $this->table_exists( $aff_tb );
        $has_tx  = $this->table_exists( $aff_tx );

        $aff_join = $has_aff ? "LEFT JOIN {$aff_tb} a ON a.id=x.affiliate_id LEFT JOIN {$wpdb->users} au ON au.ID=a.user_id LEFT JOIN {$wpdb->users} pu ON pu.ID=x.producer_id" : '';
        $tx_join = $has_tx ? "LEFT JOIN (
                SELECT order_id, affiliate_id,
                    SUM(CASE WHEN type='commission' AND status='pending' THEN amount ELSE 0 END) tx_pendente,
                    SUM(CASE WHEN type='commission' AND status='available' THEN amount ELSE 0 END) tx_disponivel,
                    SUM(CASE WHEN type='commission' AND status IN ('cancelled','canceled','refunded','reversed','void') THEN ABS(amount) ELSE 0 END) tx_estornado,
                    SUM(CASE WHEN type='penalty' THEN ABS(amount) ELSE 0 END) tx_penalidade_frustrado,
                    MAX(CASE WHEN type='commission' THEN status ELSE '' END) tx_status
                FROM {$aff_tx}
                WHERE order_id IS NOT NULL AND order_id>0
                GROUP BY order_id, affiliate_id
            ) tx ON tx.order_id=x.pedido AND (tx.affiliate_id=x.affiliate_id OR x.affiliate_id=0)" : '';

        $tpc_tx = $wpdb->prefix . 'tpc_transacoes';
        $has_tpc_tx = $this->table_exists( $tpc_tx );
        $tpc_join = $has_tpc_tx ? "LEFT JOIN (
                SELECT user_id producer_id, CAST(REPLACE(referencia,'sz_frustrado_','') AS UNSIGNED) pedido, SUM(ABS(valor)) prod_penalty
                FROM {$tpc_tx}
                WHERE tipo='debito' AND status='confirmado' AND referencia LIKE 'sz_frustrado_%'
                GROUP BY user_id, CAST(REPLACE(referencia,'sz_frustrado_','') AS UNSIGNED)
            ) tp ON tp.pedido=x.pedido AND tp.producer_id=x.producer_id" : "LEFT JOIN (SELECT 0 producer_id, 0 pedido, 0 prod_penalty) tp ON 1=0";

        $base = "SELECT {$order_col} pedido,
                    MAX(CASE WHEN meta_key IN ('_sz_aff_gross','_senderzz_offer_value','_sz_aff_order_total') THEN CAST(meta_value AS DECIMAL(12,2)) ELSE 0 END) bruto_raw,
                    MAX(CASE WHEN meta_key='_sz_aff_commission' THEN CAST(meta_value AS DECIMAL(12,2)) ELSE 0 END) afiliado_raw,
                    MAX(CASE WHEN meta_key='_sz_mb_taxa_total' THEN CAST(meta_value AS DECIMAL(12,2)) ELSE 0 END) taxas_senderzz_raw,
                    MAX(CASE WHEN meta_key='_sz_mb_taxa_entrega' THEN CAST(meta_value AS DECIMAL(12,2)) ELSE 0 END) taxa_entrega_raw,
                    MAX(CASE WHEN meta_key IN ('_sz_mb_taxa_adicional','_sz_mb_taxa_transacao') THEN CAST(meta_value AS DECIMAL(12,2)) ELSE 0 END) taxa_transacao_raw,
                    MAX(CASE WHEN meta_key='_sz_prod_commission' THEN CAST(meta_value AS DECIMAL(12,2)) ELSE 0 END) liquido_produtor_raw,
                    MAX(CASE WHEN meta_key IN ('_sz_aff_frustration_penalty','_sz_frustration_affiliate_penalty','_senderzz_aff_frustration_penalty') THEN CAST(meta_value AS DECIMAL(12,2)) ELSE 0 END) frustrado_afiliado_raw,
                    MAX(CASE WHEN meta_key IN ('_sz_prod_frustration_penalty','_sz_frustration_producer_penalty','_senderzz_prod_frustration_penalty') THEN CAST(meta_value AS DECIMAL(12,2)) ELSE 0 END) frustrado_produtor_raw,
                    MAX(CASE WHEN meta_key IN ('_sz_affiliate_id','_sz_affiliate_ref') THEN CAST(meta_value AS UNSIGNED) ELSE 0 END) affiliate_id,
                    MAX(CASE WHEN meta_key='_sz_affiliate_user_id' THEN CAST(meta_value AS UNSIGNED) ELSE 0 END) affiliate_user_id,
                    MAX(CASE WHEN meta_key='_sz_aff_producer_id' THEN CAST(meta_value AS UNSIGNED) ELSE 0 END) producer_id,
                    MAX(CASE WHEN meta_key='_sz_aff_commission_pct' THEN CAST(meta_value AS DECIMAL(8,2)) ELSE 0 END) commission_pct_meta,
                    MAX(CASE WHEN meta_key='_sz_aff_name' THEN meta_value ELSE '' END) afiliado_nome_meta,
                    MAX(CASE WHEN meta_key='_sz_aff_email' THEN meta_value ELSE '' END) afiliado_email_meta,
                    MAX(CASE WHEN meta_key='_sz_aff_producer_name' THEN meta_value ELSE '' END) produtor_nome_meta
                FROM {$meta}
                WHERE meta_key IN ('_sz_aff_gross','_senderzz_offer_value','_sz_aff_order_total','_sz_aff_commission','_sz_mb_taxa_total','_sz_mb_taxa_entrega','_sz_mb_taxa_adicional','_sz_mb_taxa_transacao','_sz_prod_commission','_sz_aff_frustration_penalty','_sz_frustration_affiliate_penalty','_senderzz_aff_frustration_penalty','_sz_prod_frustration_penalty','_sz_frustration_producer_penalty','_senderzz_prod_frustration_penalty','_sz_affiliate_id','_sz_affiliate_ref','_sz_affiliate_user_id','_sz_aff_producer_id','_sz_aff_commission_pct','_sz_aff_name','_sz_aff_email','_sz_aff_producer_name')
                GROUP BY {$order_col}";

        $rows = $wpdb->get_results( "SELECT x.pedido, {$date_expr} data_pedido, {$status_expr} order_status,
                CASE
                    WHEN {$status_expr} IN ('wc-frustrado','frustrado','wc-frustracao','frustracao','wc-cancelled','cancelled','wc-cancelado','cancelado','wc-failed','failed') THEN 'Estornado / não recebido'
                    WHEN {$status_expr} IN ('wc-completo','completo','wc-completed','completed') THEN 'Recebido'
                    ELSE 'Previsto'
                END situacao,
                x.affiliate_id,
                x.producer_id,
                COALESCE(NULLIF(x.afiliado_nome_meta,''), NULLIF(au.display_name,''), CASE WHEN x.affiliate_id>0 THEN CONCAT('Afiliado #', x.affiliate_id) ELSE '—' END) afiliado,
                COALESCE(NULLIF(x.afiliado_email_meta,''), au.user_email, '') afiliado_email,
                COALESCE(NULLIF(x.produtor_nome_meta,''), NULLIF(pu.display_name,''), CASE WHEN x.producer_id>0 THEN CONCAT('Produtor #', x.producer_id) ELSE '—' END) produtor,
                COALESCE(NULLIF(a.commission_pct,0), NULLIF(x.commission_pct_meta,0), 0) commission_pct,
                x.bruto_raw valor_pedido,
                CASE WHEN {$status_expr} IN ('wc-frustrado','frustrado','wc-frustracao','frustracao','wc-cancelled','cancelled','wc-cancelado','cancelado','wc-failed','failed') THEN 0 ELSE x.bruto_raw END bruto_valido,
                CASE WHEN {$status_expr} IN ('wc-frustrado','frustrado','wc-frustracao','frustracao','wc-cancelled','cancelled','wc-cancelado','cancelado','wc-failed','failed') THEN 0 ELSE x.taxas_senderzz_raw END taxas_senderzz,
                CASE WHEN {$status_expr} IN ('wc-frustrado','frustrado','wc-frustracao','frustracao','wc-cancelled','cancelled','wc-cancelado','cancelado','wc-failed','failed') THEN 0 ELSE x.taxa_entrega_raw END taxa_entrega,
                CASE WHEN {$status_expr} IN ('wc-frustrado','frustrado','wc-frustracao','frustracao','wc-cancelled','cancelled','wc-cancelado','cancelado','wc-failed','failed') THEN 0 ELSE x.taxa_transacao_raw END taxa_transacao,
                CASE WHEN {$status_expr} IN ('wc-frustrado','frustrado','wc-frustracao','frustracao','wc-cancelled','cancelled','wc-cancelado','cancelado','wc-failed','failed') THEN 0 ELSE x.afiliado_raw END afiliado_valor,
                CASE WHEN {$status_expr} IN ('wc-frustrado','frustrado','wc-frustracao','frustracao','wc-cancelled','cancelled','wc-cancelado','cancelado','wc-failed','failed') THEN 0 ELSE x.liquido_produtor_raw END liquido_produtor,
                CASE WHEN {$status_expr} IN ('wc-frustrado','frustrado','wc-frustracao','frustracao','wc-cancelled','cancelled','wc-cancelado','cancelado','wc-failed','failed') THEN x.bruto_raw ELSE 0 END valor_nao_recebido,
                CASE WHEN {$status_expr} IN ('wc-frustrado','frustrado','wc-frustracao','frustracao','wc-cancelled','cancelled','wc-cancelado','cancelado','wc-failed','failed') THEN (COALESCE(NULLIF(COALESCE(tx.tx_penalidade_frustrado,0),0), x.frustrado_afiliado_raw, 0) + COALESCE(NULLIF(COALESCE(tp.prod_penalty,0),0), x.frustrado_produtor_raw, 0)) ELSE 0 END bruto_estornado,
                CASE WHEN {$status_expr} IN ('wc-frustrado','frustrado','wc-frustracao','frustracao','wc-cancelled','cancelled','wc-cancelado','cancelado','wc-failed','failed') THEN COALESCE(NULLIF(COALESCE(tx.tx_penalidade_frustrado,0),0), x.frustrado_afiliado_raw, 0) ELSE 0 END frustrado_afiliado,
                CASE WHEN {$status_expr} IN ('wc-frustrado','frustrado','wc-frustracao','frustracao','wc-cancelled','cancelled','wc-cancelado','cancelado','wc-failed','failed') THEN COALESCE(NULLIF(COALESCE(tp.prod_penalty,0),0), x.frustrado_produtor_raw, 0) ELSE 0 END frustrado_produtor,
                COALESCE(tx.tx_pendente,0) repasse_pendente,
                COALESCE(tx.tx_disponivel,0) repasse_disponivel,
                COALESCE(tx.tx_estornado,0) repasse_estornado,
                COALESCE(tx.tx_status,'') tx_status,
                CASE
                    WHEN {$status_expr} IN ('wc-frustrado','frustrado','wc-frustracao','frustracao','wc-cancelled','cancelled','wc-cancelado','cancelado','wc-failed','failed') THEN 'Não repassar'
                    WHEN COALESCE(tx.tx_disponivel,0)>0 THEN 'Disponível'
                    WHEN COALESCE(tx.tx_pendente,0)>0 THEN 'Pendente'
                    WHEN x.affiliate_id>0 AND x.afiliado_raw>0 THEN 'Previsto'
                    ELSE 'Sem afiliado'
                END status_repasse
            FROM ({$base}) x
            {$status_join}
            {$aff_join}
            {$tx_join}
            {$tpc_join}
            WHERE DATE({$date_expr}) BETWEEN '{$data_de}' AND '{$data_ate}'
            HAVING valor_pedido>0 OR taxas_senderzz>0 OR afiliado_valor>0 OR liquido_produtor>0 OR affiliate_id>0
            ORDER BY x.pedido DESC
            LIMIT 300", ARRAY_A ) ?: [];

        $recebido = $taxas = $taxas_entrega = $taxas_transacao = $aff = $prod = $potencial = $previsto = 0.0;
        $order_count = $received_count = $frustrated_count = $scheduled_count = 0;
        $aff_sum = [];
        $prod_sum = [];
        foreach ( $rows as $r ) {
            $order_count++;
            $sit = (string) ( $r['situacao'] ?? '' );
            if ( $sit === 'Recebido' ) {
                $received_count++;
                $recebido += (float) $r['bruto_valido'];
                $taxas    += (float) $r['taxas_senderzz'];
                $taxas_entrega += (float) ( $r['taxa_entrega'] ?? 0 );
                $taxas_transacao += (float) ( $r['taxa_transacao'] ?? 0 );
                $aff      += (float) $r['afiliado_valor'];
                $prod     += (float) $r['liquido_produtor'];
            } elseif ( $sit === 'Estornado / não recebido' ) {
                $frustrated_count++;
                $potencial += (float) ( $r['valor_nao_recebido'] ?? 0 );
            } else {
                $scheduled_count++;
                $previsto += (float) $r['valor_pedido'];
            }

            $aid = (int) ( $r['affiliate_id'] ?? 0 );
            if ( $aid > 0 ) {
                if ( ! isset( $aff_sum[ $aid ] ) ) {
                    $aff_sum[ $aid ] = [
                        'affiliate_id' => $aid,
                        'afiliado' => $r['afiliado'] ?: ( 'Afiliado #' . $aid ),
                        'email' => $r['afiliado_email'] ?? '',
                        'produtor' => $r['produtor'] ?? '—',
                        'commission_pct' => (float) ( $r['commission_pct'] ?? 0 ),
                        'pedidos' => 0,
                        'recebidos' => 0,
                        'previstos' => 0,
                        'frustrados' => 0,
                        'pendente' => 0.0,
                        'disponivel' => 0.0,
                        'previsto_valor' => 0.0,
                    ];
                }
                $aff_sum[ $aid ]['pedidos']++;
                if ( $sit === 'Recebido' ) $aff_sum[ $aid ]['recebidos']++;
                elseif ( $sit === 'Estornado / não recebido' ) $aff_sum[ $aid ]['frustrados']++;
                else $aff_sum[ $aid ]['previstos']++;
                $aff_sum[ $aid ]['pendente'] += (float) ( $r['repasse_pendente'] ?? 0 );
                $aff_sum[ $aid ]['disponivel'] += (float) ( $r['repasse_disponivel'] ?? 0 );
                if ( $sit === 'Previsto' ) $aff_sum[ $aid ]['previsto_valor'] += (float) ( $r['afiliado_valor'] ?? 0 );
            }

            $pid_key = (string) ( (int) ( $r['producer_id'] ?? 0 ) ?: ( $r['produtor'] ?? '—' ) );
            if ( ! isset( $prod_sum[ $pid_key ] ) ) {
                $prod_sum[ $pid_key ] = [ 'produtor' => $r['produtor'] ?? '—', 'pedidos' => 0, 'recebidos' => 0, 'frustrados' => 0, 'previstos' => 0, 'bruto' => 0.0, 'bruto_previsto' => 0.0, 'taxas_senderzz' => 0.0, 'afiliado' => 0.0, 'liquido_produtor' => 0.0, 'frustrado_produtor' => 0.0, 'frustrado_afiliados' => 0.0, 'frustrado_valor' => 0.0 ];
            }
            $prod_sum[ $pid_key ]['pedidos']++;
            if ( $sit === 'Recebido' ) {
                $prod_sum[ $pid_key ]['recebidos']++;
                $prod_sum[ $pid_key ]['bruto'] += (float) $r['bruto_valido'];
                $prod_sum[ $pid_key ]['taxas_senderzz'] += (float) $r['taxas_senderzz'];
                $prod_sum[ $pid_key ]['afiliado'] += (float) $r['afiliado_valor'];
                $prod_sum[ $pid_key ]['liquido_produtor'] += (float) $r['liquido_produtor'];
            } elseif ( $sit === 'Estornado / não recebido' ) {
                $prod_sum[ $pid_key ]['frustrados']++;
                $prod_sum[ $pid_key ]['frustrado_produtor'] += (float) ( $r['frustrado_produtor'] ?? 0 );
                $prod_sum[ $pid_key ]['frustrado_afiliados'] += (float) ( $r['frustrado_afiliado'] ?? 0 );
                $prod_sum[ $pid_key ]['frustrado_valor'] += (float) ( $r['bruto_estornado'] ?? 0 );
            } else {
                $prod_sum[ $pid_key ]['previstos']++;
                $prod_sum[ $pid_key ]['bruto_previsto'] += (float) ( $r['valor_pedido'] ?? 0 );
            }
        }

        if ( $has_tx && $aff_sum ) {
            $ids = array_map( 'absint', array_keys( $aff_sum ) );
            $ph = implode( ',', array_fill( 0, count( $ids ), '%d' ) );
            $extra = $wpdb->get_results( $wpdb->prepare( "SELECT affiliate_id,
                    SUM(CASE WHEN type='withdrawal' THEN ABS(amount) ELSE 0 END) saques,
                    SUM(CASE WHEN type='penalty' THEN ABS(amount) ELSE 0 END) penalidades
                FROM {$aff_tx}
                WHERE affiliate_id IN ({$ph})
                GROUP BY affiliate_id", $ids ), ARRAY_A ) ?: [];
            foreach ( $extra as $e ) {
                $aid = (int) $e['affiliate_id'];
                if ( isset( $aff_sum[ $aid ] ) ) {
                    $aff_sum[ $aid ]['saques'] = (float) ( $e['saques'] ?? 0 );
                    $aff_sum[ $aid ]['penalidades'] = (float) ( $e['penalidades'] ?? 0 );
                }
            }
        }
        foreach ( $aff_sum as &$a ) { $a['saques'] = $a['saques'] ?? 0.0; $a['penalidades'] = $a['penalidades'] ?? 0.0; $a['carteira_real'] = max( 0, (float) ( $a['pendente'] ?? 0 ) + (float) ( $a['disponivel'] ?? 0 ) - (float) ( $a['penalidades'] ?? 0 ) ); }
        unset( $a );

        usort( $aff_sum, static function( $a, $b ) { return ( $b['pedidos'] <=> $a['pedidos'] ) ?: strcmp( $a['afiliado'], $b['afiliado'] ); } );
        usort( $prod_sum, static function( $a, $b ) { return ( $b['bruto'] <=> $a['bruto'] ) ?: strcmp( $a['produtor'], $b['produtor'] ); } );

        echo '<form method="get" class="sz-filter-bar">';
        echo '<input type="hidden" name="page" value="senderzz"><input type="hidden" name="area" value="financeiro"><input type="hidden" name="tab" value="fin-livro-cod">';
        echo '<label>De<br><input type="date" name="fin_de" value="' . esc_attr( $data_de ) . '"></label>';
        echo '<label>Até<br><input type="date" name="fin_ate" value="' . esc_attr( $data_ate ) . '"></label>';
        echo '<button class="button button-primary">Filtrar</button>';
        echo '<a class="button" href="' . esc_url( add_query_arg( [ 'page' => 'senderzz', 'area' => 'financeiro', 'tab' => 'fin-livro-cod', 'fin_de' => $default_de, 'fin_ate' => $default_ate ], admin_url( 'admin.php' ) ) ) . '">Últimos 7 dias</a>';
        echo '</form>';
        echo '<div class="sz-notice">Livro COD é a tela financeira única. Período atual: ' . esc_html( mysql2date( 'd/m/Y', $data_de ) ) . ' até ' . esc_html( mysql2date( 'd/m/Y', $data_ate ) ) . '. Campos de pedido vêm do WooCommerce; carteiras são calculadas a partir das transações e penalidades gravadas por pedido.</div>';

        $prod_pedidos = array_sum( array_map( static fn($r) => (int) ( $r['pedidos'] ?? 0 ), $prod_sum ) );
        $prod_previsto = array_sum( array_map( static fn($r) => (float) ( $r['bruto_previsto'] ?? 0 ), $prod_sum ) );
        $prod_frustrado_produtor = array_sum( array_map( static fn($r) => (float) ( $r['frustrado_produtor'] ?? 0 ), $prod_sum ) );
        $prod_frustrado_afiliados = array_sum( array_map( static fn($r) => (float) ( $r['frustrado_afiliados'] ?? 0 ), $prod_sum ) );
        $aff_pendente = array_sum( array_map( static fn($r) => (float) ( $r['pendente'] ?? 0 ), $aff_sum ) );
        $aff_disponivel = array_sum( array_map( static fn($r) => (float) ( $r['disponivel'] ?? 0 ), $aff_sum ) );
        $aff_penalidades = array_sum( array_map( static fn($r) => (float) ( $r['penalidades'] ?? 0 ), $aff_sum ) );
        $aff_carteira_real = array_sum( array_map( static fn($r) => (float) ( $r['carteira_real'] ?? 0 ), $aff_sum ) );

        $taxa_frustrados_total = (float) $prod_frustrado_produtor + (float) $prod_frustrado_afiliados;
        $taxa_transacao_total = (float) $taxas_transacao;

        // v347: a taxa de entrega do Livro COD precisa bater com a carteira dos motoboys.
        // Portanto, ela vem dos ganhos de motoboy já conciliados/liberados, nunca de meta prevista do pedido.
        // Motoboy sem conciliação permanece em "A conciliar" e não entra nas taxas nem no líquido do produtor.
        $taxa_entrega_total = null;
        $mb_ganhos_tb = $wpdb->prefix . 'sz_motoboy_ganhos';
        $mb_pedidos_tb = $wpdb->prefix . 'sz_motoboy_pedidos';
        if ( $this->table_exists( $mb_ganhos_tb ) && $this->table_exists( $mb_pedidos_tb ) ) {
            $taxa_entrega_total = (float) $wpdb->get_var( $wpdb->prepare(
                "SELECT COALESCE(SUM(g.valor),0)
                   FROM {$mb_ganhos_tb} g
              LEFT JOIN {$mb_pedidos_tb} mp ON mp.id = g.pedido_id
                  WHERE g.tipo = 'entrega'
                    AND g.status IN ('disponivel','pago')
                    AND DATE(COALESCE(mp.baixa_at, mp.ts_entregue, mp.updated_at, mp.created_at, g.created_at)) BETWEEN %s AND %s",
                $data_de, $data_ate
            ) );
        }
        if ( $taxa_entrega_total === null ) { $taxa_entrega_total = (float) $taxas_entrega; }

        $taxa_total_geral = (float) $taxa_entrega_total + (float) $taxa_transacao_total + (float) $taxa_frustrados_total;
        // Valor validado deve bater com o Livro COD/Conciliação: entregues = valor recebido; frustrados = somente taxa/penalidade gravada, nunca o valor cheio do pedido frustrado.
        $valor_validado_sem_previsto = (float) $recebido + (float) $taxa_frustrados_total;
        // v348: cards do produtor seguem exatamente a mesma lógica visual dos cards de afiliado.
        // Pendente = repasse do produtor ainda não sacado/pago no período; Disponível fica separado para carteira futura.
        // Carteira real = pendente + disponível - penalidade do produtor, sem usar penalidade do afiliado para aumentar saldo do produtor.
        $prod_pendente = max( 0, (float) $recebido - (float) $taxa_total_geral - (float) $aff_pendente - (float) $aff_disponivel );
        $prod_disponivel = 0.0;
        $prod_carteira_real = max( 0, (float) $prod_pendente + (float) $prod_disponivel - (float) $prod_frustrado_produtor );

        echo '<h3 style="margin:14px 0 10px">Produtor</h3>';
        echo '<div class="sz-kpi-grid">';
        echo '<div class="sz-kpi warn"><div class="sz-kpi-label">Pendente</div><div class="sz-kpi-val" style="font-size:var(--sz-text-3xl)">' . esc_html( $this->money( $prod_pendente ) ) . '</div><div class="sz-kpi-sub">repasse do produtor em aberto</div></div>';
        echo '<div class="sz-kpi ok"><div class="sz-kpi-label">Disponível</div><div class="sz-kpi-val" style="font-size:var(--sz-text-3xl)">' . esc_html( $this->money( $prod_disponivel ) ) . '</div><div class="sz-kpi-sub">liberado para saque</div></div>';
        echo '<div class="sz-kpi warn"><div class="sz-kpi-label">Frustrado produtor</div><div class="sz-kpi-val" style="font-size:var(--sz-text-3xl)">' . esc_html( $this->money( $prod_frustrado_produtor ) ) . '</div><div class="sz-kpi-sub">penalidade por pedidos frustrados</div></div>';
        echo '<div class="sz-kpi ok"><div class="sz-kpi-label">Carteira real</div><div class="sz-kpi-val" style="font-size:var(--sz-text-3xl)">' . esc_html( $this->money( $prod_carteira_real ) ) . '</div><div class="sz-kpi-sub">pendente + disponível - penalidades</div></div>';
        echo '</div>';
        echo '<h3 style="margin:20px 0 10px">Taxas Senderzz</h3>';
        echo '<div class="sz-kpi-grid">';
        echo '<div class="sz-kpi"><div class="sz-kpi-label">Entrega / Frustrado</div><div class="sz-kpi-val" style="font-size:var(--sz-text-xl);line-height:1.2"><span>Entrega: ' . esc_html( $this->money( $taxa_entrega_total ) ) . '</span><br><span>Frustrado: ' . esc_html( $this->money( $taxa_frustrados_total ) ) . '</span></div><div class="sz-kpi-sub">valores já gravados nos pedidos do período</div></div>';
        echo '<div class="sz-kpi"><div class="sz-kpi-label">Taxa de transação</div><div class="sz-kpi-val" style="font-size:var(--sz-text-3xl)">' . esc_html( $this->money( $taxa_transacao_total ) ) . '</div><div class="sz-kpi-sub">somente pedidos recebidos</div></div>';
        echo '<div class="sz-kpi ok"><div class="sz-kpi-label">Total de taxas</div><div class="sz-kpi-val" style="font-size:var(--sz-text-3xl)">' . esc_html( $this->money( $taxa_total_geral ) ) . '</div><div class="sz-kpi-sub">taxas recebidas + frustrados; sem previstos</div></div>';
        echo '<div class="sz-kpi"><div class="sz-kpi-label">Valor validado</div><div class="sz-kpi-val" style="font-size:var(--sz-text-3xl)">' . esc_html( $this->money( $valor_validado_sem_previsto ) ) . '</div><div class="sz-kpi-sub">recebidos + taxa de frustrados; sem previstos</div></div>';
        echo '</div>';

        echo '<h3 style="margin:20px 0 10px">Afiliados</h3>';
        echo '<div class="sz-kpi-grid">';
        echo '<div class="sz-kpi warn"><div class="sz-kpi-label">Pendente</div><div class="sz-kpi-val" style="font-size:var(--sz-text-3xl)">' . esc_html( $this->money( $aff_pendente ) ) . '</div><div class="sz-kpi-sub">comissões ainda em retenção</div></div>';
        echo '<div class="sz-kpi ok"><div class="sz-kpi-label">Disponível</div><div class="sz-kpi-val" style="font-size:var(--sz-text-3xl)">' . esc_html( $this->money( $aff_disponivel ) ) . '</div><div class="sz-kpi-sub">liberado para saque</div></div>';
        echo '<div class="sz-kpi warn"><div class="sz-kpi-label">Frustrado afiliados</div><div class="sz-kpi-val" style="font-size:var(--sz-text-3xl)">' . esc_html( $this->money( $prod_frustrado_afiliados ) ) . '</div><div class="sz-kpi-sub">penalidade por pedidos frustrados</div></div>';
        echo '<div class="sz-kpi ok"><div class="sz-kpi-label">Carteira real</div><div class="sz-kpi-val" style="font-size:var(--sz-text-3xl)">' . esc_html( $this->money( $aff_carteira_real ) ) . '</div><div class="sz-kpi-sub">pendente + disponível - penalidades</div></div>';
        echo '</div>';
        $this->render_collapsible_table( 'Resumo por produtor', array_values( $prod_sum ), [
            'produtor'          => 'Produtor',
            'pedidos'           => 'Pedidos',
            'recebidos'         => 'Recebidos',
            'previstos'         => 'Previstos',
            'frustrados'        => 'Frustrados',
            'frustrado_produtor' => 'Frustrado produtor',
            'frustrado_afiliados'=> 'Frustrado afiliados',
            'bruto'             => 'Bruto recebido',
            'bruto_previsto'    => 'Bruto previsto',
            'taxas_senderzz'    => 'Taxas Senderzz',
            'afiliado'          => 'Afiliados',
            'liquido_produtor'  => 'Líquido produtor',
        ] );

        $this->render_collapsible_table( 'Resumo por afiliado', array_values( $aff_sum ), [
            'affiliate_id'   => 'ID',
            'afiliado'       => 'Afiliado',
            'email'          => 'E-mail',
            'produtor'       => 'Produtor',
            'commission_pct' => 'Comissão %',
            'pedidos'        => 'Pedidos',
            'recebidos'      => 'Recebidos',
            'previstos'      => 'Previstos',
            'frustrados'     => 'Frustrados',
            'previsto_valor' => 'Comissão prevista',
            'pendente'       => 'Pendente',
            'disponivel'     => 'Disponível',
            'saques'         => 'Saques',
            'penalidades'    => 'Penalidades',
            'carteira_real'  => 'Carteira real',
        ], function( $row, $key ) {
            if ( $key === 'commission_pct' ) return esc_html( number_format( (float) ( $row['commission_pct'] ?? 0 ), 2, ',', '.' ) . '%' );
            if ( in_array( $key, [ 'previsto_valor', 'pendente', 'disponivel', 'saques', 'penalidades', 'carteira_real' ], true ) ) return esc_html( $this->money( (float) ( $row[ $key ] ?? 0 ) ) );
            return null;
        } );


        $this->render_collapsible_table( 'Livro COD por pedido', $rows, [
            'pedido'            => 'Pedido',
            'data_pedido'       => 'Data',
            'situacao'          => 'Situação',
            'produtor'          => 'Produtor',
            'afiliado'          => 'Afiliado',
            'commission_pct'    => 'Comissão %',
            'valor_pedido'      => 'Valor pedido',
            'taxas_senderzz'    => 'Taxas',
            'frustrado_produtor' => 'Frustrado produtor',
            'frustrado_afiliado' => 'Frustrado afiliado',
            'afiliado_valor'    => 'Afiliado',
            'liquido_produtor'  => 'Produtor',
            'status_repasse'    => 'Repasse',
        ], function( $row, $key ) {
            if ( $key === 'pedido' ) return '<a href="' . esc_url( $this->order_admin_url( absint( $row['pedido'] ) ) ) . '">#' . absint( $row['pedido'] ) . '</a>';
            if ( $key === 'data_pedido' ) {
                $v = (string) ( $row['data_pedido'] ?? '' );
                return $v ? esc_html( mysql2date( 'd/m/Y', $v ) ) : '—';
            }
            if ( $key === 'situacao' ) {
                $cls = $row['situacao'] === 'Recebido' ? 'ok' : 'warn';
                return '<span class="sz-status-pill ' . esc_attr( $cls ) . '">' . esc_html( $row['situacao'] ) . '</span><br><span class="sz-muted">' . esc_html( (string) ( $row['order_status'] ?? '' ) ) . '</span>';
            }
            if ( $key === 'status_repasse' ) {
                $st = (string) ( $row['status_repasse'] ?? '' );
                $cls = $st === 'Disponível' ? 'ok' : ( $st === 'Pendente' || $st === 'Previsto' ? 'warn' : '' );
                return '<span class="sz-status-pill ' . esc_attr( $cls ) . '">' . esc_html( $st ?: '—' ) . '</span>';
            }
            if ( $key === 'commission_pct' ) return esc_html( number_format( (float) ( $row['commission_pct'] ?? 0 ), 2, ',', '.' ) . '%' );
            if ( in_array( $key, [ 'valor_pedido', 'taxas_senderzz', 'frustrado_produtor', 'frustrado_afiliado', 'afiliado_valor', 'liquido_produtor' ], true ) ) return esc_html( $this->money( (float) ( $row[ $key ] ?? 0 ) ) );
            return null;
        } );

    }


    private function tab_fin_taxas_entrega(): void {
        global $wpdb;
        if ( ! current_user_can( 'manage_options' ) && ! current_user_can( 'manage_woocommerce' ) ) { return; }
        $parse = static function( $v ): float { return max( 0.0, (float) str_replace( ',', '.', (string) $v ) ); };

        if ( isset( $_POST['sz_save_taxas_entrega'] ) ) {
            check_admin_referer( 'sz_save_taxas_entrega' );

            $taxa_cliente = $parse( $_POST['taxa_cliente_cod'] ?? get_option( 'sz_motoboy_taxa_entrega', 25 ) );
            $taxa_transacao_percentual = $parse( $_POST['taxa_transacao_percentual'] ?? get_option( 'sz_motoboy_taxa_percentual', 0 ) );
            $taxa_motoboy_entrega = $parse( $_POST['taxa_motoboy_entrega'] ?? get_option( 'sz_mbw_taxa_entrega', 18 ) );
            $taxa_motoboy_frustrado = $parse( $_POST['taxa_motoboy_frustrado'] ?? get_option( 'sz_mbw_taxa_frustrado', 5 ) );

            update_option( 'sz_motoboy_taxa_entrega', $taxa_cliente, false );
            update_option( 'sz_motoboy_taxa_percentual', $taxa_transacao_percentual, false );
            // v369: regra fixa. A taxa percentual replica por participante; não há mais seleção de pagador.
            update_option( 'sz_motoboy_taxa_transacao_modo', 'split_producer_affiliate', false );
            update_option( 'sz_mbw_taxa_entrega', $taxa_motoboy_entrega, false );
            update_option( 'sz_mbw_taxa_frustrado', $taxa_motoboy_frustrado, false );
            // Mantém compatibilidade com rotinas antigas que ainda leem este option.
            update_option( 'sz_motoboy_taxa_frustrado', $taxa_motoboy_frustrado, false );

            update_option( 'sz_aff_first_frustration_penalty', $parse( $_POST['aff_first_global'] ?? 5 ), false );
            update_option( 'sz_aff_default_penalty_value', $parse( $_POST['aff_repeat_global'] ?? 5 ), false );
            update_option( 'sz_prod_first_frustration_penalty', $parse( $_POST['prod_first_global'] ?? 0 ), false );
            update_option( 'sz_aff_producer_frustration_penalty', $parse( $_POST['prod_repeat_global'] ?? 8 ), false );

            $aff_over = [];
            foreach ( (array) ( $_POST['aff_override'] ?? [] ) as $id => $row ) {
                $id = absint( $id ); if ( ! $id ) continue;
                $first = trim( (string) ( $row['first'] ?? '' ) );
                $repeat = trim( (string) ( $row['repeat'] ?? '' ) );
                if ( $first !== '' || $repeat !== '' ) $aff_over[ (string) $id ] = [ 'first' => $first === '' ? null : $parse( $first ), 'repeat' => $repeat === '' ? null : $parse( $repeat ) ];
            }
            $prod_over = [];
            foreach ( (array) ( $_POST['prod_override'] ?? [] ) as $id => $row ) {
                $id = absint( $id ); if ( ! $id ) continue;
                $first = trim( (string) ( $row['first'] ?? '' ) );
                $repeat = trim( (string) ( $row['repeat'] ?? '' ) );
                if ( $first !== '' || $repeat !== '' ) $prod_over[ (string) $id ] = [ 'first' => $first === '' ? null : $parse( $first ), 'repeat' => $repeat === '' ? null : $parse( $repeat ) ];
            }
            update_option( 'sz_frustration_aff_overrides', $aff_over, false );
            update_option( 'sz_frustration_prod_overrides', $prod_over, false );

            foreach ( (array) ( $_POST['producer_mb'] ?? [] ) as $id => $row ) {
                $id = absint( $id ); if ( ! $id ) continue;
                update_user_meta( $id, '_sz_motoboy_ativo', ! empty( $row['ativo'] ) ? '1' : '0' );

                foreach ( [
                    '_sz_motoboy_taxa_entrega'    => 'taxa_entrega',
                    '_sz_motoboy_taxa_manuseio'   => 'taxa_manuseio',
                    '_sz_motoboy_taxa_percentual' => 'taxa_percentual',
                ] as $meta_key => $field_key ) {
                    $raw = trim( (string) ( $row[ $field_key ] ?? '' ) );
                    if ( $raw === '' ) delete_user_meta( $id, $meta_key );
                    else update_user_meta( $id, $meta_key, $parse( $raw ) );
                }

                // v369: modo removido; a taxa de transação replica por participante para todos.
                delete_user_meta( $id, '_sz_motoboy_taxa_transacao_modo' );

                wp_cache_delete( 'sz_mb_portal_uid_' . $id, 'sz_motoboy' );
            }

            $mb_over = [];
            foreach ( (array) ( $_POST['mb_override'] ?? [] ) as $id => $row ) {
                $id = absint( $id ); if ( ! $id ) continue;
                $ent = trim( (string) ( $row['entrega'] ?? '' ) );
                $fru = trim( (string) ( $row['frustrado'] ?? '' ) );
                if ( $ent !== '' && $parse( $ent ) > 0 ) update_option( 'sz_mbw_taxa_entrega_mb_' . $id, $parse( $ent ), false );
                else delete_option( 'sz_mbw_taxa_entrega_mb_' . $id );
                if ( $fru !== '' && $parse( $fru ) > 0 ) update_option( 'sz_mbw_taxa_frustrado_mb_' . $id, $parse( $fru ), false );
                else delete_option( 'sz_mbw_taxa_frustrado_mb_' . $id );
            }

            echo '<div class="sz-notice">Taxas de entrega e regras de frustrados salvas em uma fonte central.</div>';
        }

        $aff_tb = function_exists( 'sz_aff_table' ) ? sz_aff_table( 'sz_affiliates' ) : $wpdb->prefix . 'sz_affiliates';
        $affs = $this->table_exists( $aff_tb ) ? ( $wpdb->get_results( "SELECT a.id, COALESCE(NULLIF(u.display_name,''), CONCAT('Afiliado #',a.id)) nome FROM {$aff_tb} a LEFT JOIN {$wpdb->users} u ON u.ID=a.user_id WHERE a.deleted_at IS NULL ORDER BY nome LIMIT 300", ARRAY_A ) ?: [] ) : [];
        $prods = [];
        if ( $this->table_exists( $aff_tb ) ) {
            foreach ( ( $wpdb->get_results( "SELECT DISTINCT a.producer_id id, COALESCE(NULLIF(u.display_name,''), CONCAT('Produtor #',a.producer_id)) nome FROM {$aff_tb} a LEFT JOIN {$wpdb->users} u ON u.ID=a.producer_id WHERE a.deleted_at IS NULL AND a.producer_id>0", ARRAY_A ) ?: [] ) as $row ) {
                $prods[ (int) $row['id'] ] = $row;
            }
        }
        $portal_users_tb = $wpdb->prefix . 'senderzz_portal_users';
        if ( $this->table_exists( $portal_users_tb ) ) {
            foreach ( ( $wpdb->get_results( "SELECT DISTINCT pu.wp_user_id id, COALESCE(NULLIF(wp.display_name,''), NULLIF(pu.name,''), NULLIF(pu.email,''), CONCAT('Produtor #',pu.wp_user_id)) nome FROM {$portal_users_tb} pu LEFT JOIN {$wpdb->users} wp ON wp.ID=pu.wp_user_id WHERE pu.wp_user_id>0", ARRAY_A ) ?: [] ) as $row ) {
                $prods[ (int) $row['id'] ] = $row;
            }
        }
        foreach ( ( $wpdb->get_results( $wpdb->prepare( "SELECT DISTINCT u.ID id, COALESCE(NULLIF(u.display_name,''), CONCAT('Produtor #',u.ID)) nome FROM {$wpdb->users} u INNER JOIN {$wpdb->usermeta} um ON um.user_id=u.ID WHERE um.meta_key IN (%s,%s,%s,%s)", '_sz_motoboy_ativo', '_sz_motoboy_taxa_entrega', '_sz_motoboy_taxa_manuseio', '_sz_motoboy_taxa_percentual' ), ARRAY_A ) ?: [] ) as $row ) {
            $prods[ (int) $row['id'] ] = $row;
        }
        $prods = array_values( $prods );
        usort( $prods, static fn( $a, $b ) => strcasecmp( (string) $a['nome'], (string) $b['nome'] ) );
        $motoboys_tb = $wpdb->prefix . 'sz_motoboys';
        $motoboys = $this->table_exists( $motoboys_tb ) ? ( $wpdb->get_results( "SELECT id, nome FROM {$motoboys_tb} WHERE ativo=1 ORDER BY nome LIMIT 300", ARRAY_A ) ?: [] ) : [];
        $aff_over = function_exists( 'sz_aff_frustration_overrides' ) ? sz_aff_frustration_overrides( 'affiliate' ) : (array) get_option( 'sz_frustration_aff_overrides', [] );
        $prod_over = function_exists( 'sz_aff_frustration_overrides' ) ? sz_aff_frustration_overrides( 'producer' ) : (array) get_option( 'sz_frustration_prod_overrides', [] );

        echo '<div class="sz-notice">Taxas de Entrega é a tela central de configuração financeira operacional da Senderzz: cobrança COD, taxa de transação replicada por participante, pagamento ao motoboy e penalidades de frustrado.</div>';
        echo '<form method="post">'; wp_nonce_field( 'sz_save_taxas_entrega' );

        echo '<h3>Taxas principais</h3><div class="sz-kpi-grid">';
        echo '<div class="sz-kpi"><div class="sz-kpi-label">Cliente COD · entrega</div><input name="taxa_cliente_cod" value="' . esc_attr( number_format( (float) get_option( 'sz_motoboy_taxa_entrega', 25 ), 2, ',', '.' ) ) . '" style="width:140px"><div class="sz-kpi-sub">cobrado no pedido/checkout</div></div>';
        echo '<div class="sz-kpi"><div class="sz-kpi-label">Venda · taxa de transação (%)</div><input name="taxa_transacao_percentual" value="' . esc_attr( number_format( (float) get_option( 'sz_motoboy_taxa_percentual', 0 ), 2, ',', '.' ) ) . '" style="width:140px"><div class="sz-kpi-sub">replica por participante em pedidos novos; sem afiliado cobra uma vez do produtor</div></div>';
        echo '<div class="sz-kpi"><div class="sz-kpi-label">Motoboy · entrega</div><input name="taxa_motoboy_entrega" value="' . esc_attr( number_format( (float) get_option( 'sz_mbw_taxa_entrega', 18 ), 2, ',', '.' ) ) . '" style="width:140px"><div class="sz-kpi-sub">valor a pagar por entrega</div></div>';
        echo '<div class="sz-kpi"><div class="sz-kpi-label">Motoboy · frustrado</div><input name="taxa_motoboy_frustrado" value="' . esc_attr( number_format( (float) get_option( 'sz_mbw_taxa_frustrado', get_option( 'sz_motoboy_taxa_frustrado', 5 ) ), 2, ',', '.' ) ) . '" style="width:140px"><div class="sz-kpi-sub">valor validado no frustrado</div></div>';
        echo '</div>';

        echo '<h3>Penalidades de frustrado</h3><div class="sz-kpi-grid">';
        echo '<div class="sz-kpi"><div class="sz-kpi-label">Afiliado · 1ª frustração</div><input name="aff_first_global" value="' . esc_attr( number_format( function_exists('sz_aff_first_frustration_penalty') ? sz_aff_first_frustration_penalty() : 5, 2, ',', '.' ) ) . '" style="width:140px"></div>';
        echo '<div class="sz-kpi"><div class="sz-kpi-label">Afiliado · reincidente</div><input name="aff_repeat_global" value="' . esc_attr( number_format( function_exists('sz_aff_default_penalty_value') ? sz_aff_default_penalty_value() : 5, 2, ',', '.' ) ) . '" style="width:140px"></div>';
        echo '<div class="sz-kpi"><div class="sz-kpi-label">Produtor · 1ª frustração</div><input name="prod_first_global" value="' . esc_attr( number_format( (float) get_option( 'sz_prod_first_frustration_penalty', 0 ), 2, ',', '.' ) ) . '" style="width:140px"></div>';
        echo '<div class="sz-kpi"><div class="sz-kpi-label">Produtor · reincidente</div><input name="prod_repeat_global" value="' . esc_attr( number_format( function_exists('sz_aff_producer_frustration_penalty') ? sz_aff_producer_frustration_penalty() : 8, 2, ',', '.' ) ) . '" style="width:140px"></div>';
        echo '</div>';

        echo '<details class="sz-collapse" open><summary>Taxas particulares por motoboy</summary><table class="sz-table"><thead><tr><th>Motoboy</th><th>Entrega</th><th>Frustrado</th></tr></thead><tbody>';
        foreach ( $motoboys as $mb ) { $id=(int)$mb['id']; $ent=get_option('sz_mbw_taxa_entrega_mb_'.$id,''); $fru=get_option('sz_mbw_taxa_frustrado_mb_'.$id,''); echo '<tr><td>'.esc_html($mb['nome']).' <span class="sz-muted">ID '.$id.'</span></td><td><input name="mb_override['.$id.'][entrega]" value="'.esc_attr($ent). '" placeholder="padrão"></td><td><input name="mb_override['.$id.'][frustrado]" value="'.esc_attr($fru).'" placeholder="padrão"></td></tr>'; }
        echo '</tbody></table></details>';

        echo '<details class="sz-collapse" open><summary>Configuração Senderzz por produtor</summary><table class="sz-table"><thead><tr><th>Produtor</th><th>Motoboy</th><th>Entrega cliente</th><th>Manuseio</th><th>Transação %</th><th>Frustrado 1ª</th><th>Frustrado reinc.</th></tr></thead><tbody>';
        foreach ( $prods as $pr ) {
            $id=(int)$pr['id']; $r=$prod_over[(string)$id]??[];
            $ativo = get_user_meta( $id, '_sz_motoboy_ativo', true ) === '1';
            $ent = get_user_meta( $id, '_sz_motoboy_taxa_entrega', true );
            $man = get_user_meta( $id, '_sz_motoboy_taxa_manuseio', true );
            $perc = get_user_meta( $id, '_sz_motoboy_taxa_percentual', true );
            echo '<tr><td>'.esc_html($pr['nome']).' <span class="sz-muted">ID '.$id.'</span></td>';
            echo '<td><label><input type="checkbox" name="producer_mb['.$id.'][ativo]" value="1" '.checked($ativo,true,false).'> ativo</label></td>';
            echo '<td><input name="producer_mb['.$id.'][taxa_entrega]" value="'.esc_attr($ent).'" placeholder="global"></td>';
            echo '<td><input name="producer_mb['.$id.'][taxa_manuseio]" value="'.esc_attr($man).'" placeholder="0,00"></td>';
            echo '<td><input name="producer_mb['.$id.'][taxa_percentual]" value="'.esc_attr($perc).'" placeholder="global"></td>';
            echo '<td><input name="prod_override['.$id.'][first]" value="'.esc_attr($r['first']??'').'" placeholder="coletivo"></td><td><input name="prod_override['.$id.'][repeat]" value="'.esc_attr($r['repeat']??'').'" placeholder="coletivo"></td></tr>';
        }
        echo '</tbody></table><p class="sz-muted">Campos vazios usam a configuração global. A taxa de transação replica por participante e vale somente para pedidos novos.</p></details>';
        echo '<details class="sz-collapse"><summary>Regras particulares por afiliado</summary><table class="sz-table"><thead><tr><th>Afiliado</th><th>1ª frustração</th><th>Reincidente</th></tr></thead><tbody>';
        foreach ( $affs as $af ) { $id=(int)$af['id']; $r=$aff_over[(string)$id]??[]; echo '<tr><td>'.esc_html($af['nome']).' <span class="sz-muted">ID '.$id.'</span></td><td><input name="aff_override['.$id.'][first]" value="'.esc_attr($r['first']??'').'" placeholder="coletivo"></td><td><input name="aff_override['.$id.'][repeat]" value="'.esc_attr($r['repeat']??'').'" placeholder="coletivo"></td></tr>'; }
        echo '</tbody></table></details><p><button class="button button-primary" name="sz_save_taxas_entrega" value="1">Salvar Taxas de Entrega</button></p></form>';
    }

    private function tab_fin_carteiras(): void {
        echo '<div class="sz-notice">Carteiras separadas apenas como visão de saldo. O Livro COD continua sendo a fonte principal por pedido.</div>';
        $this->tab_fin_produtores();
        $this->tab_fin_carteira_afiliados();
    }

    private function process_affiliate_withdrawal_admin_action(): void {
        global $wpdb;
        if ( empty( $_POST['sz_aff_withdraw_action_nonce'] ) ) return;
        if ( ! wp_verify_nonce( sanitize_text_field( wp_unslash( $_POST['sz_aff_withdraw_action_nonce'] ) ), 'sz_aff_withdraw_action' ) ) return;
        if ( ! current_user_can( 'manage_options' ) && ! current_user_can( 'manage_woocommerce' ) ) return;
        if ( ! function_exists( 'sz_aff_table' ) ) return;

        $id     = absint( $_POST['withdrawal_id'] ?? 0 );
        $action = sanitize_key( $_POST['withdrawal_action'] ?? '' );
        $note   = sanitize_text_field( wp_unslash( $_POST['admin_note'] ?? '' ) );
        if ( ! $id || ! in_array( $action, [ 'approve', 'reject' ], true ) ) return;

        $wd_table  = sz_aff_table( 'sz_affiliate_withdrawals' );
        $wal_table = sz_aff_table( 'sz_affiliate_wallet' );
        $tx_table  = sz_aff_table( 'sz_affiliate_transactions' );
        if ( ! $this->table_exists( $wd_table ) || ! $this->table_exists( $wal_table ) || ! $this->table_exists( $tx_table ) ) return;

        $w = $wpdb->get_row( $wpdb->prepare( "SELECT * FROM {$wd_table} WHERE id=%d LIMIT 1", $id ), ARRAY_A );
        if ( ! $w || ! in_array( (string) $w['status'], [ 'pending', 'analysis', 'em_analise' ], true ) ) return;

        $now = current_time( 'mysql' );
        if ( $action === 'approve' ) {
            $amount = round( (float) $w['amount'], 2 );
            $affiliate_id = absint( $w['affiliate_id'] );
            $balance = (float) $wpdb->get_var( $wpdb->prepare( "SELECT balance FROM {$wal_table} WHERE affiliate_id=%d LIMIT 1", $affiliate_id ) );
            if ( $balance + 0.02 < $amount ) {
                echo '<div class="sz-notice warn">Saldo insuficiente para aprovar o saque de afiliado #' . esc_html( (string) $id ) . '.</div>';
                return;
            }
            $wpdb->query( $wpdb->prepare( "UPDATE {$wal_table} SET balance=GREATEST(0,balance-%f), updated_at=%s WHERE affiliate_id=%d", $amount, $now, $affiliate_id ) );
            $wpdb->insert( $tx_table, [
                'affiliate_id' => $affiliate_id,
                'type'         => 'withdrawal',
                'amount'       => -$amount,
                'status'       => 'available',
                'available_at' => $now,
                'created_at'   => $now,
                'meta_json'    => wp_json_encode( [ 'source' => 'unified_admin_withdrawal', 'withdrawal_id' => $id ] ),
            ], [ '%d','%s','%f','%s','%s','%s','%s' ] );
            $wpdb->update( $wd_table, [ 'status' => 'approved', 'decided_at' => $now, 'decided_by' => get_current_user_id(), 'note' => $note ], [ 'id' => $id ], [ '%s','%s','%d','%s' ], [ '%d' ] );
            echo '<div class="sz-notice">Saque de afiliado aprovado.</div>';
        } else {
            $wpdb->update( $wd_table, [ 'status' => 'rejected', 'decided_at' => $now, 'decided_by' => get_current_user_id(), 'note' => $note ], [ 'id' => $id ], [ '%s','%s','%d','%s' ], [ '%d' ] );
            echo '<div class="sz-notice">Saque de afiliado recusado.</div>';
        }
    }

    private function tab_fin_saques(): void {
        global $wpdb;
        $this->process_affiliate_withdrawal_admin_action();

        echo '<div class="sz-section"><div class="sz-section-head"><h3>Saques COD / Produtor</h3><span class="sz-status-pill ok">Produtor</span></div><div class="sz-section-body">';
        if ( function_exists( 'sz_cod_admin_page' ) ) {
            $this->tab_wrap( 'sz_cod_admin_page' );
        } else {
            echo '<p>Módulo de saques COD indisponível.</p>';
        }
        echo '</div></div>';

        if ( ! function_exists( 'sz_aff_table' ) ) {
            echo '<div class="sz-section"><div class="sz-section-head"><h3>Saques de afiliados</h3></div><div class="sz-section-body"><p>Módulo de afiliados indisponível.</p></div></div>';
            return;
        }

        $wd_table = sz_aff_table( 'sz_affiliate_withdrawals' );
        $aff_table = sz_aff_table( 'sz_affiliates' );
        if ( ! $this->table_exists( $wd_table ) || ! $this->table_exists( $aff_table ) ) {
            echo '<div class="sz-section"><div class="sz-section-head"><h3>Saques de afiliados</h3></div><div class="sz-section-body"><p>Tabela de saques de afiliado não encontrada.</p></div></div>';
            return;
        }

        $rows = $wpdb->get_results( "SELECT w.*,a.pix_key,a.bank_info,u.display_name,u.user_email
            FROM {$wd_table} w
            LEFT JOIN {$aff_table} a ON a.id=w.affiliate_id
            LEFT JOIN {$wpdb->users} u ON u.ID=a.user_id
            ORDER BY FIELD(w.status,'pending','analysis','em_analise','approved','rejected'), w.id DESC
            LIMIT 120", ARRAY_A ) ?: [];

        echo '<div class="sz-section"><div class="sz-section-head"><h3>Saques de afiliados</h3><span class="sz-status-pill warn">Afiliado</span></div><div class="sz-section-body" style="padding:0;overflow:auto">';
        echo '<table class="widefat striped"><thead><tr><th>ID</th><th>Afiliado</th><th>Valor</th><th>Taxa</th><th>Líquido</th><th>Conta/PIX</th><th>Status</th><th>Ação</th></tr></thead><tbody>';
        if ( ! $rows ) echo '<tr><td colspan="8">Nenhum saque de afiliado.</td></tr>';
        foreach ( $rows as $w ) {
            $status = (string) ( $w['status'] ?? '' );
            $open = in_array( $status, [ 'pending', 'analysis', 'em_analise' ], true );
            echo '<tr>';
            echo '<td>#' . (int) $w['id'] . '</td>';
            echo '<td>' . esc_html( ( $w['display_name'] ?: 'Afiliado' ) . ( ! empty( $w['user_email'] ) ? ' · ' . $w['user_email'] : '' ) ) . '</td>';
            echo '<td>' . esc_html( $this->money( (float) $w['amount'] ) ) . '</td>';
            echo '<td>' . esc_html( $this->money( (float) $w['fee'] ) ) . '</td>';
            echo '<td><strong>' . esc_html( $this->money( (float) $w['net_amount'] ) ) . '</strong></td>';
            echo '<td>' . esc_html( trim( (string) ( $w['pix_key'] ?? '' ) . ( ! empty( $w['bank_info'] ) ? ' · ' . (string) $w['bank_info'] : '' ) ) ?: '—' ) . '</td>';
            echo '<td><span class="sz-status-pill ' . ( $open ? 'warn' : 'ok' ) . '">' . esc_html( $status === 'approved' ? 'Aprovado' : ( $status === 'rejected' ? 'Recusado' : 'Em análise' ) ) . '</span></td>';
            echo '<td>';
            if ( $open ) {
                echo '<form method="post" style="display:grid;gap:6px;max-width:260px">';
                wp_nonce_field( 'sz_aff_withdraw_action', 'sz_aff_withdraw_action_nonce' );
                echo '<input type="hidden" name="withdrawal_id" value="' . (int) $w['id'] . '">';
                echo '<textarea name="admin_note" placeholder="Observação interna"></textarea>';
                echo '<div style="display:flex;gap:6px;flex-wrap:wrap"><button class="button button-primary" name="withdrawal_action" value="approve">Aprovar</button><button class="button" name="withdrawal_action" value="reject">Recusar</button></div>';
                echo '</form>';
            } else {
                echo esc_html( ! empty( $w['decided_at'] ) ? mysql2date( 'd/m/Y H:i', $w['decided_at'] ) : '—' );
            }
            echo '</td></tr>';
        }
        echo '</tbody></table></div></div>';
    }

    private function tab_auditoria(): void {
        global $wpdb;
        $counts = $this->get_audit_counts();
        $msg = isset( $_GET['sz_msg'] ) ? sanitize_text_field( rawurldecode( wp_unslash( $_GET['sz_msg'] ) ) ) : '';
        if ( $msg ) echo '<div class="sz-notice ' . ( isset( $_GET['sz_warn'] ) ? 'warn' : '' ) . '">' . esc_html( $msg ) . '</div>';
        ?>
        <div class="sz-kpi-grid">
            <div class="sz-kpi <?php echo array_sum( $counts ) ? 'warn' : 'ok'; ?>"><div class="sz-kpi-label">Divergências totais</div><div class="sz-kpi-val"><?php echo (int) array_sum( $counts ); ?></div><div class="sz-kpi-sub">itens para revisão</div></div>
            <div class="sz-kpi"><div class="sz-kpi-label">Total divergente</div><div class="sz-kpi-val"><?php echo (int) $counts['split']; ?></div><div class="sz-kpi-sub">valores fora do esperado</div></div>
            <div class="sz-kpi"><div class="sz-kpi-label">Repasse pendente</div><div class="sz-kpi-val"><?php echo (int) $counts['aff_missing']; ?></div><div class="sz-kpi-sub">repasse pendente de ajuste</div></div>
            <div class="sz-kpi"><div class="sz-kpi-label">Carteira divergente</div><div class="sz-kpi-val"><?php echo (int) ( $counts['aff_bad'] + $counts['wallet'] ); ?></div><div class="sz-kpi-sub">carteira precisa de revisão</div></div>
        </div>
        <div class="sz-section"><div class="sz-section-head"><h3>Correção segura</h3><div class="sz-actions"><a class="button button-primary" href="<?php echo esc_url( wp_nonce_url( admin_url( 'admin-post.php?action=sz_admin_audit_fix_all' ), 'sz_admin_audit_fix_all' ) ); ?>">Corrigir tudo</a></div></div><div class="sz-section-body"><p class="sz-muted">Corrige apenas registros com divergência objetiva, sem alterar a regra financeira do pedido.</p></div></div>
        <?php
        $rows = $this->audit_problem_rows();
        $this->render_table( 'Pedidos com divergência', $rows, [ 'pedido'=>'Pedido', 'tipo'=>'Tipo', 'oficial'=>'Esperado', 'atual'=>'Atual', 'acao'=>'Ação' ], function( $row, $key ) {
            if ( $key === 'tipo' ) return '<span class="sz-status-pill warn">' . esc_html( $row['tipo'] ) . '</span>';
            if ( $key === 'acao' ) return '<a class="button button-small button-primary" href="' . esc_url( wp_nonce_url( admin_url( 'admin-post.php?action=sz_admin_audit_fix_order&order_id=' . absint( $row['pedido'] ) ), 'sz_admin_audit_fix_order_' . absint( $row['pedido'] ) ) ) . '">Corrigir pedido</a>';
            return null;
        } );
    }

    private function audit_problem_rows(): array {
        if ( function_exists( 'senderzz_audit_problem_rows' ) ) {
            return senderzz_audit_problem_rows( 100 );
        }
        global $wpdb;
        $meta = $this->meta_table(); $order_col = $this->meta_order_col();
        $rows = [];
        if ( ! $this->table_exists( $meta ) ) return $rows;

        // Split: só para pedido não-frustrado. Frustrado tem valores potenciais, não repasse real.
        $split_candidates = $wpdb->get_results( "SELECT {$order_col} pedido,
                MAX(CASE WHEN meta_key IN ('_sz_aff_gross','_senderzz_offer_value') THEN CAST(meta_value AS DECIMAL(12,2)) END) bruto,
                MAX(CASE WHEN meta_key='_sz_aff_commission' THEN CAST(meta_value AS DECIMAL(12,2)) ELSE 0 END) aff,
                MAX(CASE WHEN meta_key='_sz_mb_taxa_total' THEN CAST(meta_value AS DECIMAL(12,2)) ELSE 0 END) taxa,
                MAX(CASE WHEN meta_key='_sz_prod_commission' THEN CAST(meta_value AS DECIMAL(12,2)) ELSE 0 END) prod
            FROM {$meta}
            WHERE meta_key IN ('_sz_aff_gross','_senderzz_offer_value','_sz_aff_commission','_sz_mb_taxa_total','_sz_prod_commission')
            GROUP BY {$order_col}
            HAVING bruto IS NOT NULL AND ABS(ROUND(bruto,2)-ROUND(aff+taxa+prod,2))>0.02
            ORDER BY {$order_col} DESC LIMIT 80", ARRAY_A ) ?: [];
        foreach ( $split_candidates as $r ) {
            $oid = absint( $r['pedido'] );
            if ( $this->audit_should_ignore_order( $oid ) ) continue;
            $rows[] = [ 'pedido' => $oid, 'tipo' => 'Total divergente', 'tipo_key' => 'split', 'oficial' => (float) $r['bruto'], 'atual' => (float) $r['aff'] + (float) $r['taxa'] + (float) $r['prod'] ];
        }

        $aff_tx = function_exists( 'sz_aff_table' ) ? sz_aff_table( 'sz_affiliate_transactions' ) : $wpdb->prefix . 'sz_affiliate_transactions';
        if ( $this->table_exists( $aff_tx ) ) {
            $missing = $wpdb->get_results( "SELECT x.order_id pedido,'Afiliado sem transação' tipo,'aff_missing' tipo_key,x.comissao oficial,0 atual FROM (SELECT {$order_col} order_id, MAX(CASE WHEN meta_key IN ('_sz_affiliate_id','_sz_affiliate_ref') THEN CAST(meta_value AS UNSIGNED) ELSE 0 END) affiliate_id, MAX(CASE WHEN meta_key='_sz_aff_commission' THEN CAST(meta_value AS DECIMAL(12,2)) ELSE 0 END) comissao FROM {$meta} WHERE meta_key IN ('_sz_affiliate_id','_sz_affiliate_ref','_sz_aff_commission') GROUP BY {$order_col}) x LEFT JOIN {$aff_tx} t ON t.order_id=x.order_id AND t.affiliate_id=x.affiliate_id AND t.type='commission' AND t.status IN ('pending','available') WHERE x.affiliate_id>0 AND x.comissao>0 AND t.id IS NULL ORDER BY x.order_id DESC LIMIT 80", ARRAY_A ) ?: [];
            $bad = $wpdb->get_results( "SELECT t.order_id pedido,'Comissão afiliado divergente' tipo,'aff_bad' tipo_key,m.meta_value oficial,t.amount atual FROM {$aff_tx} t JOIN {$meta} m ON m.{$order_col}=t.order_id AND m.meta_key='_sz_aff_commission' WHERE t.type='commission' AND t.status IN ('pending','available') AND ROUND(CAST(t.amount AS DECIMAL(12,2)),2)<>ROUND(CAST(m.meta_value AS DECIMAL(12,2)),2) ORDER BY t.order_id DESC LIMIT 80", ARRAY_A ) ?: [];
            foreach ( array_merge( $missing, $bad ) as $r ) {
                if ( $this->audit_should_ignore_order( absint( $r['pedido'] ) ) ) continue;
                $rows[] = $r;
            }
        }
        $cod_tx = $wpdb->prefix . 'sz_cod_wallet_transactions';
        if ( $this->table_exists( $cod_tx ) ) {
            $wallet = $wpdb->get_results( "SELECT w.order_id pedido,'Produtor COD divergente' tipo,'wallet' tipo_key,m.meta_value oficial,w.net atual FROM {$cod_tx} w JOIN {$meta} m ON m.{$order_col}=w.order_id AND m.meta_key='_sz_prod_commission' WHERE w.type='credit' AND ROUND(CAST(w.net AS DECIMAL(12,2)),2)<>ROUND(CAST(m.meta_value AS DECIMAL(12,2)),2) ORDER BY w.order_id DESC LIMIT 80", ARRAY_A ) ?: [];
            foreach ( $wallet as $r ) {
                if ( $this->audit_should_ignore_order( absint( $r['pedido'] ) ) ) continue;
                $rows[] = $r;
            }
        }
        return array_slice( $rows, 0, 100 );
    }

    private function render_collapsible_table( string $title, array $rows, array $columns, ?callable $custom = null, bool $open = true ): void {
        echo '<details class="sz-collapse" ' . ( $open ? 'open' : '' ) . '><summary>' . esc_html( $title ) . '</summary>';
        $this->render_table( '', $rows, $columns, $custom );
        echo '</details>';
    }

    private function render_table( string $title, array $rows, array $columns, ?callable $custom = null ): void {
        echo '<div class="sz-section">';
        if ( $title !== '' ) echo '<div class="sz-section-head"><h3>' . esc_html( $title ) . '</h3></div>';
        echo '<div class="sz-section-body" style="padding:0;overflow:auto"><table class="widefat striped"><thead><tr>';
        foreach ( $columns as $label ) echo '<th>' . esc_html( $label ) . '</th>';
        echo '</tr></thead><tbody>';
        if ( ! $rows ) echo '<tr><td colspan="' . count( $columns ) . '">Nenhum registro encontrado.</td></tr>';
        foreach ( $rows as $row ) {
            echo '<tr>';
            foreach ( $columns as $key => $label ) {
                $html = $custom ? $custom( $row, $key ) : null;
                if ( $html === null ) {
                    $v = $row[ $key ] ?? '';
                    if ( is_numeric( $v ) && ! in_array( $key, [ 'pedido','pedidos','commission_pct','affiliate_id','user_id','entregues','frustrados','recebidos','previstos','pending_count','active_count' ], true ) ) $v = $this->money( $v );
                    if ( $key === 'commission_pct' && is_numeric( $v ) ) $v = number_format( (float) $v, 2, ',', '.' ) . '%';
                    $html = esc_html( (string) $v );
                }
                echo '<td>' . $html . '</td>';
            }
            echo '</tr>';
        }
        echo '</tbody></table></div></div>';
    }

    private function tab_wrap( string $fn, array $force_get = [] ): void {
        if ( ! function_exists( $fn ) ) { $this->empty_module( $fn ); return; }
        $old = [];
        foreach ( $force_get as $k => $v ) { $old[$k] = $_GET[$k] ?? null; $_GET[$k] = $v; }
        ob_start(); $fn(); $html = ob_get_clean();
        foreach ( $force_get as $k => $v ) { if ( $old[$k] === null ) unset( $_GET[$k] ); else $_GET[$k] = $old[$k]; }
        $html = preg_replace( '/<div[^>]+class=["\']wrap["\'][^>]*>/i', '', $html, 1 );
        $html = rtrim( $html );
        if ( substr( $html, -6 ) === '</div>' ) $html = substr( $html, 0, -6 );
        echo '<div class="sz-tab-content active" style="display:block">' . $html . '</div>';
    }

    private function tab_class( string $class, string $method ): void {
        if ( ! class_exists( $class ) ) { $this->empty_module( $class ); return; }
        ob_start(); ( new $class() )->$method(); $html = ob_get_clean();
        $html = preg_replace( '/<div[^>]+class=["\']wrap["\'][^>]*>/i', '', $html, 1 );
        echo '<div class="sz-tab-content active" style="display:block">' . $html . '</div>';
    }

    private function empty_module( string $label ): void {
        echo '<div class="sz-section"><div class="sz-section-head"><h3>' . esc_html( $label ) . '</h3></div><div class="sz-section-body"><p>Módulo não carregado ou sem dados.</p></div></div>';
    }


    private function tab_exp_pedidos(): void {
        global $wpdb;
        $statuses = [ 'wc-pending','wc-processing','wc-on-hold','wc-entregue','wc-completed','wc-cancelled' ];
        $cards = [
            'Hoje' => $this->order_count_by_status( $statuses, gmdate( 'Y-m-d' ) ),
            'Processando' => $this->order_count_by_status( [ 'wc-processing' ] ),
            'Entregue' => $this->order_count_by_status( [ 'wc-entregue','wc-completed' ] ),
            'Cancelado' => $this->order_count_by_status( [ 'wc-cancelled' ] ),
        ];
        echo '<div class="sz-kpi-grid">';
        foreach ( $cards as $label => $value ) {
            echo '<div class="sz-kpi"><div class="sz-kpi-label">' . esc_html( $label ) . '</div><div class="sz-kpi-val">' . (int) $value . '</div><div class="sz-kpi-sub">expedição</div></div>';
        }
        echo '</div>';
        if ( class_exists( '\\WC_MelhorEnvio\\Analytics\\Margin_Dashboard' ) ) {
            echo '<div class="sz-section"><div class="sz-section-head"><h3>Pedidos de expedição</h3></div><div class="sz-section-body">';
            ( new \WC_MelhorEnvio\Analytics\Margin_Dashboard() )->render_inline();
            echo '</div></div>';
        } else {
            $this->empty_module( 'Pedidos de expedição' );
        }
    }

    private function tab_exp_integracoes(): void {
        echo '<div class="sz-two-col">';
        echo '<div class="sz-section"><div class="sz-section-head"><h3>Integrações de entrada</h3></div><div class="sz-section-body"><ul class="sz-clean-list">';
        echo '<li><span>Entrada</span><b>Pedidos externos</b></li><li><span>Destino</span><b>Expedição</b></li><li><span>Uso</span><b>Importar pedidos automaticamente</b></li>';
        echo '</ul></div></div>';
        echo '<div class="sz-section"><div class="sz-section-head"><h3>Atalhos</h3></div><div class="sz-section-body"><div class="sz-actions">';
        echo '<a class="button button-primary" href="' . esc_url( admin_url( 'admin.php?page=senderzz&area=expedicao&tab=exp-webhooks' ) ) . '">Ver Webhooks</a>';
        echo '<a class="button" href="' . esc_url( admin_url( 'admin.php?page=senderzz&area=expedicao&tab=exp-carteira' ) ) . '">Carteira Frete</a>';
        echo '</div></div></div></div>';
        echo '<div class="sz-section"><div class="sz-section-head"><h3>Configuração</h3></div><div class="sz-section-body">';
        $this->tab_wrap( 'senderzz_markup_admin_page' );
        echo '</div></div>';
    }

    private function tab_exp_webhooks(): void {
        $endpoint = rest_url( 'senderzz/v1/webhook' );
        echo '<div class="sz-two-col">';
        echo '<div class="sz-section"><div class="sz-section-head"><h3>Webhooks de expedição</h3></div><div class="sz-section-body"><ul class="sz-clean-list">';
        echo '<li><span>Área</span><b>Expedição</b></li><li><span>Uso</span><b>Receber pedidos externos</b></li><li><span>Status</span><b>Ativo</b></li>';
        echo '</ul></div></div>';
        echo '<div class="sz-section"><div class="sz-section-head"><h3>Endpoint</h3></div><div class="sz-section-body"><pre class="sz-code">' . esc_html( $endpoint ) . '</pre><p class="sz-muted">Use este endereço para receber pedidos externos.</p></div></div>';
        echo '</div>';
        echo '<div class="sz-section"><div class="sz-section-head"><h3>Configuração de webhook</h3></div><div class="sz-section-body">';
        $this->tab_wrap( 'senderzz_pw_admin_page' );
        echo '</div></div>';
    }

    private function tab_exp_carteira(): void {
        echo '<div class="sz-section"><div class="sz-section-head"><h3>Carteira Frete</h3><span class="sz-status-pill ok">Ativa</span></div><div class="sz-section-body"><p class="sz-muted">Saldo e recarga de frete.</p></div></div>';
        $this->tab_wrap( 'tpc_admin_render' );
    }

    private function tab_configuracoes(): void {
        echo '<div class="sz-kpi-grid">';
        echo '<div class="sz-kpi"><div class="sz-kpi-label">Área</div><div class="sz-kpi-val" style="font-size:var(--sz-text-xl)">Config</div><div class="sz-kpi-sub">ajustes gerais</div></div>';
        echo '<div class="sz-kpi"><div class="sz-kpi-label">Menus</div><div class="sz-kpi-val" style="font-size:var(--sz-text-xl)">5</div><div class="sz-kpi-sub">Topo enxuto</div></div>';
        echo '<div class="sz-kpi ok"><div class="sz-kpi-label">Organização</div><div class="sz-kpi-val" style="font-size:var(--sz-text-xl)">Ativa</div><div class="sz-kpi-sub">menus simplificados</div></div>';
        echo '</div>';
        echo '<div class="sz-section"><div class="sz-section-head"><h3>Configurações gerais</h3><span class="sz-status-pill ok">Atualizado</span></div><div class="sz-section-body">';
        if ( function_exists( 'tpc_tab_configuracoes' ) ) tpc_tab_configuracoes(); else echo '<p>Configurações disponíveis no módulo principal.</p>';
        echo '</div></div>';
        // Normalização de order meta
        $this->tab_meta_normalization();
    }

    private function tab_meta_normalization(): void {
        if ( ! class_exists( 'Senderzz_Order_Meta' ) ) return;

        $nonce_action = 'sz_normalize_meta';
        $action_url   = admin_url( 'admin-post.php' );

        // Exibir resultado da última execução
        $done    = (int) ( $_GET['sz_norm_done']    ?? 0 );
        $updated = (int) ( $_GET['sz_norm_updated'] ?? 0 );
        $next    = (int) ( $_GET['sz_norm_next']    ?? 0 );
        if ( $done > 0 ) {
            echo '<div class="notice notice-success is-dismissible"><p>Normalização: <strong>' . esc_html( $done ) . '</strong> pedido(s) processados, <strong>' . esc_html( $updated ) . '</strong> meta(s) preenchidas. Próximo offset: ' . esc_html( $next ) . '.</p></div>';
        }

        $divergences = Senderzz_Order_Meta::get_divergence_report();
        echo '<div class="sz-section"><div class="sz-section-head"><h3>Normalização de Order Meta</h3><span class="sz-status-pill">Manual</span></div><div class="sz-section-body">';
        echo '<p class="sz-muted">Preenche campos canônicos a partir de campos legados. <strong>Nunca apaga dados antigos.</strong> Processa em lotes. Seguro para produção.</p>';
        echo '<form method="POST" action="' . esc_url( $action_url ) . '" style="display:flex;gap:12px;flex-wrap:wrap;align-items:flex-end;margin:16px 0">';
        echo '<input type="hidden" name="action" value="senderzz_normalize_meta">';
        wp_nonce_field( $nonce_action );
        echo '<label style="font-size:13px">Lote: <input type="number" name="batch_size" value="50" min="10" max="500" style="width:70px;margin-left:4px"></label>';
        echo '<label style="font-size:13px">Offset: <input type="number" name="offset" value="0" min="0" style="width:80px;margin-left:4px"></label>';
        echo '<label style="font-size:13px"><input type="checkbox" name="dry_run" value="1" style="margin-right:4px"> Simulação (sem gravar)</label>';
        echo '<button type="submit" class="button button-primary">Executar normalização</button>';
        echo '</form>';

        if ( ! empty( $divergences ) ) {
            echo '<h4 style="margin:16px 0 8px">Últimas divergências registradas (' . esc_html( count( $divergences ) ) . ')</h4>';
            echo '<table class="widefat striped" style="font-size:12px"><thead><tr><th>Pedido</th><th>Canônico</th><th>Valor canônico</th><th>Legado</th><th>Valor legado</th><th>Ação</th></tr></thead><tbody>';
            foreach ( array_slice( array_reverse( $divergences ), 0, 50 ) as $row ) {
                echo '<tr>';
                echo '<td>#' . esc_html( (string) ( $row['order_id'] ?? '' ) ) . '</td>';
                echo '<td><code>' . esc_html( $row['canonical'] ?? '' ) . '</code></td>';
                echo '<td>' . esc_html( (string) ( $row['canonical_value'] ?? $row['value'] ?? '' ) ) . '</td>';
                echo '<td><code>' . esc_html( $row['legacy_field'] ?? $row['from_field'] ?? '' ) . '</code></td>';
                echo '<td>' . esc_html( (string) ( $row['legacy_value'] ?? '' ) ) . '</td>';
                echo '<td>' . esc_html( $row['action'] ?? '' ) . '</td>';
                echo '</tr>';
            }
            echo '</tbody></table>';
            echo '<p style="margin-top:8px"><a href="' . esc_url( add_query_arg( 'sz_clear_meta_log', '1' ) ) . '" class="button button-secondary">Limpar log</a></p>';
        }
        echo '</div></div>';

        // Limpar log se solicitado
        if ( isset( $_GET['sz_clear_meta_log'] ) && current_user_can( 'manage_woocommerce' ) ) {
            Senderzz_Order_Meta::clear_divergence_report();
        }
    }

    private function tab_notificacoes_pwa(): void {
        echo '<div class="sz-section"><div class="sz-section-head"><h3>Notificações PWA</h3><span class="sz-status-pill ok">Produtor / Afiliado / Admin</span></div><div class="sz-section-body"><p class="sz-muted">Configure alertas e mensagens enviados pela plataforma.</p></div></div>';
        function_exists( 'sz_app_pwa_render_notifications_admin' ) ? $this->tab_wrap( 'sz_app_pwa_render_notifications_admin' ) : $this->empty_module( 'Notificações PWA' );
    }

    private function tab_push_tecnico(): void {
        echo '<div class="sz-section"><div class="sz-section-head"><h3>Push</h3><span class="sz-status-pill warn">Verificação</span></div><div class="sz-section-body"><ul class="sz-clean-list">';
        echo '<li><span>Uso</span><b>Envio de notificações</b></li>';
        echo '<li><span>Área</span><b>Notificações</b></li>';
        echo '<li><span>Agendamento COD</span><b>Validar evento/status nesta tela</b></li>';
        echo '</ul></div></div>';
        function_exists( 'sz_notif_admin_page' ) ? $this->tab_wrap( 'sz_notif_admin_page' ) : $this->empty_module( 'Push' );
    }

    private function tab_api(): void {
        echo '<div class="sz-section"><div class="sz-section-head"><h3>API</h3><span class="sz-status-pill ok">Ativa</span></div><div class="sz-section-body">';
        if ( function_exists( 'tpc_tab_api' ) ) tpc_tab_api(); else echo '<p>API Senderzz ativa no plugin.</p>';
        echo '</div></div>';
    }

    private function tab_saude_sistema(): void {
        global $wpdb;
        $checks = [];
        $checks[] = [ 'item' => 'Pedidos', 'status' => $this->table_exists( $this->meta_table() ) ? 'OK' : 'Falha', 'detalhe' => $this->meta_table() ];
        $checks[] = [ 'item' => 'Afiliados', 'status' => $this->table_exists( function_exists( 'sz_aff_table' ) ? sz_aff_table( 'sz_affiliate_transactions' ) : $wpdb->prefix . 'sz_affiliate_transactions' ) ? 'OK' : 'Ausente', 'detalhe' => 'Base financeira' ];
        $checks[] = [ 'item' => 'Carteira COD', 'status' => $this->table_exists( $wpdb->prefix . 'sz_cod_wallet_transactions' ) ? 'OK' : 'Ausente', 'detalhe' => 'Base financeira' ];
        $checks[] = [ 'item' => 'Problemas de auditoria', 'status' => count( $this->audit_problem_rows() ) ? 'Atenção' : 'OK', 'detalhe' => count( $this->audit_problem_rows() ) . ' item(ns)' ];
        $this->render_table( 'Diagnóstico', $checks, [ 'item' => 'Item', 'status' => 'Status', 'detalhe' => 'Detalhe' ], function( $row, $key ) {
            if ( $key === 'status' ) return '<span class="sz-status-pill ' . ( $row['status'] === 'OK' ? 'ok' : 'warn' ) . '">' . esc_html( $row['status'] ) . '</span>';
            return null;
        } );
        echo '<div class="sz-section"><div class="sz-section-head"><h3>Ações</h3></div><div class="sz-section-body"><div class="sz-actions">';
        echo '<a class="button button-primary" href="' . esc_url( admin_url( 'admin.php?page=senderzz&area=financeiro&tab=fin-auditoria' ) ) . '">Abrir auditoria</a>';
        echo '<a class="button" href="' . esc_url( admin_url( 'admin.php?page=senderzz&area=config&tab=cfg-push' ) ) . '">Ver Push</a>';
        if ( function_exists( 'senderzz_checkout_legacy_repair_button_html' ) ) {
            echo senderzz_checkout_legacy_repair_button_html();
        }
        echo '</div></div></div>';
    }
}