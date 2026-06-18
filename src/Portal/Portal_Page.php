<?php
namespace WC_MelhorEnvio\Portal;

/**
 * Portal_Page v3 — Layout completo, tela cheia, zero WordPress.
 * Menus: Pedidos | Carteira | Links | Usuários | Relatórios
 */
class Portal_Page {

    public function __construct() {
        add_shortcode( 'senderzz_portal', [ $this, 'render' ] );
        add_action( 'init', [ $this, 'create_pages' ] );
        add_action( 'wp_ajax_nopriv_senderzz_portal', [ $this, 'handle_ajax' ] );
        add_action( 'wp_ajax_senderzz_portal', [ $this, 'handle_ajax' ] );
        add_action( 'template_redirect', [ $this, 'prevent_caching' ], 0 );
        add_action( 'template_redirect', [ $this, 'maybe_render_tracking' ] );
        add_action( 'template_redirect', [ $this, 'handle_logout' ], 1 );
        add_action( 'template_redirect', [ $this, 'maybe_render_register_v2' ], 2 );
        add_action( 'template_redirect', [ $this, 'maybe_render_dashboard_v2' ], 3 );
        add_action( 'wp_head', [ $this, 'maybe_hide_wp' ], 1 );
        add_action( 'wp_head', [ $this, 'dark_readability_css' ], 9999 );
        add_action( 'wp_enqueue_scripts', [ $this, 'enqueue_portal_assets' ] );
        add_filter( 'show_admin_bar', [ $this, 'hide_bar' ] );
    }

    /**
     * Carrega CSS do portal externalizado.
     * O JS do portal ainda é inline (tem interpolação PHP de nonce/API URL).
     * Migração futura: wp_localize_script + portal.js externo.
     */
    public function enqueue_portal_assets(): void {
        if ( ! $this->is_portal() && ! $this->is_tracking() ) return;

        // ── Dashboard V2 (Fase 1): assets próprios e escopados ───────────────
        // Na V2 ativa, o DOM legado não existe; carregar portal.css/portal.js
        // aqui seria peso morto e risco de colisão. A página de tracking e
        // qualquer fluxo legado (login, OL, mobile) seguem no bloco abaixo.
        if ( $this->is_portal() && ! $this->is_tracking() ) {
            $sz_v2_user = Portal_Auth::get_current_user();
            if ( $sz_v2_user && ! $this->is_mobile_browser() && $this->dashboard_v2_active( $sz_v2_user ) ) {
                $sz_v2_assets = [
                    [ 'style',  'senderzz-brand-tokens',   'assets/css/senderzz-brand-tokens.css' ],
                    [ 'style',  'senderzz-dashboard-v2',   'assets/css/senderzz-dashboard-v2.css' ],
                    [ 'style',  'senderzz-components-v2',  'assets/css/senderzz-components-v2.css' ],
                    [ 'script', 'senderzz-dashboard-v2',   'assets/js/senderzz-dashboard-v2.js' ],
                    [ 'script', 'senderzz-v2-products',    'assets/js/senderzz-v2-products.js' ],
                ];
                foreach ( $sz_v2_assets as $sz_v2_asset ) {
                    $sz_v2_path = TPC_PATH . $sz_v2_asset[2];
                    $sz_v2_ver  = file_exists( $sz_v2_path ) ? md5_file( $sz_v2_path ) : TPC_VERSION;
                    if ( 'style' === $sz_v2_asset[0] ) {
                        wp_enqueue_style( $sz_v2_asset[1], TPC_URL . $sz_v2_asset[2], [], $sz_v2_ver );
                    } else {
                        wp_enqueue_script( $sz_v2_asset[1], TPC_URL . $sz_v2_asset[2], [], $sz_v2_ver, true );
                    }
                }
                return;
            }
        }
        // ── /Dashboard V2 ──────────────────────────────────────────────────────

        // Fallback path (login page, tracking, mobile redirect) — usa V2 assets.
        // V1 (portal.css, portal.js, portal-sidebar.js, portal-operator.js,
        // sz-portal-webhook-history.js) removidos.
        foreach ( [
            [ 'style',  'senderzz-brand-tokens',  'assets/css/senderzz-brand-tokens.css'  ],
            [ 'style',  'senderzz-dashboard-v2',   'assets/css/senderzz-dashboard-v2.css'  ],
            [ 'style',  'senderzz-components-v2',  'assets/css/senderzz-components-v2.css' ],
        ] as $a ) {
            $ver = file_exists( TPC_PATH . $a[2] ) ? md5_file( TPC_PATH . $a[2] ) : TPC_VERSION;
            wp_enqueue_style( $a[1], TPC_URL . $a[2], [], $ver );
        }
    }



    /**
     * Override carregado depois de assets/js/portal.js.
     * Mantém a API atual e força o Histórico de disparos a nascer com colgroup/classes.
     *
     * REFACTOR-v459: JS externalizado para assets/js/sz-portal-webhook-history.js.
     * Carregado via wp_enqueue_script em enqueue_portal_assets(). Método mantido para
     * compatibilidade de API — não remover (pode ser chamado por código legado/externo).
     *
     * @deprecated desde REFACTOR-v459 — retorna vazio; JS servido via wp_enqueue_script.
     */
private function webhook_history_alignment_js(): string {
    // REFACTOR-v459: JS movido para assets/js/sz-portal-webhook-history.js.
    // Carregado via wp_enqueue_script (dependência sz-portal) em enqueue_portal_assets().
    // Método mantido (retorna vazio) para compatibilidade com qualquer referência externa.
    return ''; // phpcs:ignore -- conteúdo movido para assets/js/sz-portal-webhook-history.js
    // return <<<'JS'  [conteúdo movido para assets/js/sz-portal-webhook-history.js — REFACTOR-v459]
    // [conteúdo suprimido — ver assets/js/sz-portal-webhook-history.js] END_MARKER
}

    /**
     * Impede que a página do portal seja cacheada (plugins de cache, navegador, CDN, edge).
     * Roda só na página do portal — não afeta o resto do site.
     */
    public function prevent_caching(): void {
        $page_id = (int) get_option( 'senderzz_portal_page_id' );
        if ( ! $page_id || ! is_page( $page_id ) ) return;

        // Constantes reconhecidas pelos principais plugins de cache do WP
        if ( ! defined( 'DONOTCACHEPAGE' ) )   define( 'DONOTCACHEPAGE', true );
        if ( ! defined( 'DONOTCACHEDB' ) )     define( 'DONOTCACHEDB', true );
        if ( ! defined( 'DONOTMINIFY' ) )      define( 'DONOTMINIFY', true );
        if ( ! defined( 'DONOTCDN' ) )         define( 'DONOTCDN', true );
        if ( ! defined( 'DONOTCACHEOBJECT' ) ) define( 'DONOTCACHEOBJECT', true );

        // Headers HTTP fortes contra cache de navegador, edge cache e CDN
        if ( ! headers_sent() ) {
            nocache_headers();
            header( 'Cache-Control: no-store, no-cache, must-revalidate, private, max-age=0' );
            header( 'Pragma: no-cache' );
            header( 'Expires: Thu, 01 Jan 1970 00:00:00 GMT' );
            // CDN / edge cache (Cloudflare, Fastly, etc.)
            header( 'CDN-Cache-Control: no-store' );
            header( 'Surrogate-Control: no-store' );
            // LiteSpeed Cache plugin
            header( 'X-LiteSpeed-Cache-Control: no-cache, private, no-store' );
            // Headers de segurança HTTP
            header( 'X-Frame-Options: SAMEORIGIN' );
            header( 'X-Content-Type-Options: nosniff' );
            header( 'Referrer-Policy: strict-origin-when-cross-origin' );
            header( 'X-XSS-Protection: 1; mode=block' );
            header( 'Permissions-Policy: geolocation=(), microphone=(), camera=()' );
        }

        // Meta tags no <head> como cinta-e-suspensório (alguns browsers/proxies só respeitam meta)
        add_action( 'wp_head', function () {
            echo "\n" . '<meta http-equiv="Cache-Control" content="no-cache, no-store, must-revalidate" />' . "\n";
            echo '<meta http-equiv="Pragma" content="no-cache" />' . "\n";
            echo '<meta http-equiv="Expires" content="0" />' . "\n";
        }, 0 );
    }

    public function handle_logout(): void {
        if ( ! isset( $_GET['senderzz_logout'] ) ) {
            return;
        }

        if ( ! headers_sent() ) {
            nocache_headers();
            header( 'Cache-Control: no-store, no-cache, must-revalidate, max-age=0' );
            header( 'Pragma: no-cache' );
            header( 'Expires: 0' );
            header( 'X-LiteSpeed-Cache-Control: no-cache, private, no-store' );
        }

        Portal_Auth::logout();

        $page_id  = (int) get_option( 'senderzz_portal_page_id' );
        $redirect = $page_id ? get_permalink( $page_id ) : home_url( '/' );

        // Redirect limpo — sem parâmetros na URL para evitar flash visual
        wp_safe_redirect( $redirect );
        exit;
    }

    public function hide_bar( $s ): bool {
        return ( $this->is_portal() || $this->is_tracking() ) ? false : $s;
    }

    public function dark_readability_css(): void {
        if ( ! $this->is_portal() ) return;
    }

    public function maybe_hide_wp(): void {
        if ( ! $this->is_portal() && ! $this->is_tracking() ) return;
    }

    private function is_portal(): bool {
        $id = (int) get_option( 'senderzz_portal_page_id' );
        return ( $id && is_page( $id ) );
    }

    private function is_tracking(): bool {
        return strpos( $_SERVER['REQUEST_URI'] ?? '', '/rastreio/' ) !== false;
    }

    private function logo_url(): string {
        return senderzz_portal_logo_url(  );
    }

    public function create_pages(): void {
        if ( ! get_option( 'senderzz_portal_page_id' ) ) {
            $id = wp_insert_post( [ 'post_title' => 'Meus Pedidos', 'post_name' => 'meus-pedidos', 'post_content' => '[senderzz_portal]', 'post_status' => 'publish', 'post_type' => 'page' ] );
            if ( $id && ! is_wp_error( $id ) ) update_option( 'senderzz_portal_page_id', $id );
        }
    }

    private function senderzz_get_order_tracking_codes( $order ): array {
        return senderzz_portal_tracking_codes( $order );
    }

    // ── TRACKING PAGE (white-label v2.4) ───────────────────────────────────

    public function maybe_render_tracking(): void {
        if ( ! preg_match( '#^/rastreio/([A-Za-z0-9\-]+)#', $_SERVER['REQUEST_URI'] ?? '', $m ) ) return;
        $code   = strtoupper( sanitize_text_field( $m[1] ) );
        $events = $this->fetch_tracking_events( $code );
        $this->render_tracking_page( $code, $events );
        exit;
    }

    private function fetch_tracking_events( string $code ): array {
        global $wpdb;

        // HPOS-safe: o rastreio do Melhor Envio está salvo em wp_wc_orders_meta como _melhor_envio_tracking.
        $orders_meta_table = $wpdb->prefix . 'wc_orders_meta';
        $order_id = $wpdb->get_var( $wpdb->prepare(
            "SELECT order_id FROM {$orders_meta_table}
             WHERE meta_key = '_melhor_envio_tracking'
             AND meta_value LIKE %s LIMIT 1",
            '%' . $wpdb->esc_like( $code ) . '%'
        ) );

        // Fallback apenas para instalações antigas sem HPOS.
        if ( ! $order_id ) {
            $order_id = $wpdb->get_var( $wpdb->prepare(
                "SELECT post_id FROM {$wpdb->postmeta}
                 WHERE meta_key = '_melhor_envio_tracking'
                 AND meta_value LIKE %s LIMIT 1",
                '%' . $wpdb->esc_like( $code ) . '%'
            ) );
        }

        if ( ! $order_id ) return [];
        $order = wc_get_order( (int) $order_id );
        if ( ! $order ) return [];
        $me_order_id = $order->get_meta( '_melhor_envio_order_id' );
        if ( ! $me_order_id ) return [];
        $token = get_option( 'tpc_me_token', '' );
        if ( ! $token ) return [];
        $response = wp_remote_get(
            TPC_ME_API . '/me/shipment/tracking?orders=' . urlencode( $me_order_id ),
            [ 'headers' => [ 'Authorization' => 'Bearer ' . $token, 'Accept' => 'application/json', 'User-Agent' => 'Senderzz/2.4' ], 'timeout' => 10 ]
        );
        if ( is_wp_error( $response ) || wp_remote_retrieve_response_code( $response ) !== 200 ) return [];
        $body     = json_decode( wp_remote_retrieve_body( $response ), true );
        $shipment = $body[ $me_order_id ] ?? ( isset( $body[0] ) ? $body[0] : [] );
        $items    = $shipment['tracking']['events'] ?? $shipment['events'] ?? [];
        if ( ! is_array( $items ) ) return [];
        $events = [];
        foreach ( $items as $ev ) {
            $events[] = [
                'date'        => $ev['happened_at'] ?? $ev['data'] ?? '',
                'description' => $ev['description'] ?? $ev['descricao'] ?? '',
                'location'    => trim( ( $ev['city'] ?? '' ) . ( isset( $ev['state'] ) ? '/' . $ev['state'] : '' ) ),
            ];
        }
        usort( $events, fn( $a, $b ) => strcmp( $b['date'], $a['date'] ) );
        return $events;
    }

    private function render_tracking_page( string $code, array $events ): void {
        $logo = esc_url( senderzz_portal_logo_url(), [ 'http', 'https', 'data' ] );
        ?><!DOCTYPE html>
<html lang="pt-BR">
<head>
<meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Rastreio <?php echo esc_html( $code ); ?> — Senderzz</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{background:#f5f5f7;color:#111827;font-family:var(--sz-font);min-height:100vh;padding:32px 16px}
.container{max-width:620px;margin:0 auto}
.header{display:flex;align-items:center;gap:10px;margin-bottom:28px}
.logo{height:34px;max-width:130px;object-fit:contain}
.card{background:#fff;border-radius:16px;border:1px solid #e5e7eb;padding:26px;box-shadow:0 4px 20px rgba(0,0,0,.06)}
.card-label{font-size:var(--sz-text-meta);font-weight:600;text-transform:none;letter-spacing:0;color:#9ca3af;margin-bottom:5px}
.code{font-family:var(--sz-font);font-size:var(--sz-text-xl);font-weight:700;letter-spacing:0;color:#111827;margin-bottom:22px}
.timeline{position:relative;padding-left:26px}
.timeline::before{content:"";position:absolute;left:6px;top:5px;bottom:5px;width:2px;background:#f3f4f6}
.ev{position:relative;margin-bottom:20px}
.ev:last-child{margin-bottom:0}
.ev-dot{position:absolute;left:-26px;top:2px;width:14px;height:14px;border-radius:50%;background:#fff;border:2px solid #e5e7eb}
.ev-dot.latest{background:#16a34a;border-color:#16a34a}
.ev-desc{font-size:var(--sz-text-base);font-weight:600;color:#111827;line-height:1.4;margin-bottom:2px}
.ev-meta{font-size:var(--sz-text-sm);color:#9ca3af;display:flex;gap:10px;flex-wrap:wrap}
.empty{text-align:center;padding:36px 0;color:#9ca3af}
.empty-ico{font-size:var(--sz-text-hero);margin-bottom:10px}
.empty-title{font-size:var(--sz-text-lg);font-weight:600;color:#6b7280;margin-bottom:5px}
.empty-sub{font-size:var(--sz-text-meta);line-height:1.6}
@media(prefers-color-scheme:dark){body{background:#0f172a;color:#f1f5f9}.card{background:#1e293b;border-color:#334155}.code,.ev-desc{color:#f1f5f9}.timeline::before{background:#334155}.ev-dot{background:#1e293b;border-color:#334155}}



/* ===== Senderzz patch v72.2 — hover unificado + copiar ao lado do link ===== */
#sec-links .szlk-list-hd,
#sec-links .szlk-row{
    grid-template-columns:minmax(150px,1.1fr) minmax(200px,1.25fr) minmax(92px,.55fr) minmax(320px,1.8fr) minmax(210px,.85fr)!important;
}
#sec-links .szlk-cell-url{overflow:visible!important;justify-content:center!important;}
#sec-links .szlk-url-inline{justify-content:center!important;}
#sec-links .szlk-url-inline{
    display:flex!important;
    align-items:center!important;
    gap:10px!important;
    width:100%!important;
    min-width:0!important;
}
#sec-links .szlk-url-text{
    display:block!important;
    min-width:0!important;
    flex:1 1 auto!important;
    overflow:hidden!important;
    text-overflow:ellipsis!important;
    white-space:nowrap!important;
    color:#E8650A!important;
    font-family:var(--sz-font);
    font-size:var(--sz-text-sm);
    font-weight:500;
}
#sec-links .szlk-btn-copy-inline{
    width:auto!important;
    min-width:0!important;
    flex:0 0 auto!important;
    height:28px!important;
    padding:0 9px!important;
    border-radius:8px!important;
    background:rgba(15,23,42,.04)!important;
    color:var(--tx2)!important;
    border:1px solid rgba(148,163,184,.24)!important;
    box-shadow:none!important;
    font-size:var(--sz-text-sm);
}
#sec-links .szlk-acts{
    grid-template-columns:repeat(2,minmax(0,1fr))!important;
}
#sec-links .szlk-btn,
#sec-links .szlk-toggle-form,
#sec-stock .szst-open-btn,
#sec-freight .sz-freight-toggle-btn,
#sec-webhooks .sz-mini,
#sec-webhooks .sz-btn-ghost,
#sec-webhooks .sz-primary,
.sz-root .sz-quick,
.sz-root .sz-primary,
.sz-root .sz-btn-ghost,
.sz-root .sz-mini,
.sz-root .sz-act{
    transition: background-color .18s ease, border-color .18s ease, color .18s ease, box-shadow .18s ease, filter .18s ease, opacity .18s ease !important;
}
#sec-links .szlk-btn:hover,
#sec-links .szlk-toggle-form:hover,
#sec-stock .szst-open-btn:hover,
#sec-freight .sz-freight-toggle-btn:hover,
#sec-webhooks .sz-mini:hover,
#sec-webhooks .sz-btn-ghost:hover,
#sec-webhooks .sz-primary:hover,
.sz-root .sz-quick:hover,
.sz-root .sz-primary:hover,
.sz-root .sz-btn-ghost:hover,
.sz-root .sz-mini:hover,
.sz-root .sz-act:hover{
    transform:none!important;
    translate:none!important;
}
#sec-links .szlk-btn-open:hover,
#sec-links .szlk-toggle-form:hover,
#sec-stock .szst-open-btn:hover,
#sec-freight .sz-freight-toggle-btn:hover,
#sec-webhooks .wh-test:hover,
#sec-webhooks .sz-primary:hover,
.sz-root .sz-quick:hover,
.sz-root .sz-primary:hover{
    filter:brightness(1.04)!important;
    box-shadow:0 10px 24px rgba(232,101,10,.24)!important;
}
#sec-links .szlk-btn-del:hover,
#sec-webhooks .wh-delete:hover,
.sz-root .sz-btn-danger:hover{
    filter:brightness(1.03)!important;
    box-shadow:0 10px 22px rgba(15,23,42,.18)!important;
}
#sec-links .szlk-btn-copy-inline:hover,
#sec-webhooks .sz-wh-copy:hover,
.sz-root .sz-act.copy:hover,
.sz-root button[onclick*="copy" i]:hover{
    filter:none!important;
    background:rgba(15,23,42,.08)!important;
    border-color:rgba(148,163,184,.34)!important;
    color:var(--tx)!important;
    box-shadow:0 8px 18px rgba(15,23,42,.08)!important;
}
.sz-root.sz-dark #sec-links .szlk-btn-copy-inline,
.sz-root.sz-dark #sec-webhooks .sz-wh-copy{
    background:rgba(255,255,255,.04)!important;
    color:#e2e8f0!important;
    border-color:rgba(148,163,184,.22)!important;
}
.sz-root.sz-dark #sec-links .szlk-btn-copy-inline:hover,
.sz-root.sz-dark #sec-webhooks .sz-wh-copy:hover{
    background:rgba(255,255,255,.08)!important;
    color:#fff!important;
    border-color:rgba(148,163,184,.34)!important;
    box-shadow:none!important;
}
@media(max-width:1180px){
    #sec-links .szlk-row{grid-template-columns:1fr!important;}
    #sec-links .szlk-cell:nth-child(4)::before{content:"URL"!important;}
    #sec-links .szlk-cell:nth-child(5)::before{content:"Ações"!important;}
    #sec-links .szlk-url-inline{flex-wrap:wrap!important;justify-content:flex-end!important;}
    #sec-links .szlk-acts{grid-template-columns:repeat(2,minmax(0,1fr))!important;}
}
@media(max-width:680px){
    #sec-links .szlk-url-inline{align-items:flex-start!important;}
    #sec-links .szlk-btn-copy-inline{width:100%!important;min-width:0!important;}
    #sec-links .szlk-acts{grid-template-columns:1fr!important;}
}
</style>
</head>
<body>
<div class="container">
    <div class="header"><?php if ( $logo ) : ?><img src="<?php echo $logo; ?>" class="logo" alt="Senderzz"><?php else : ?><span style="font-family:var(--sz-font);font-weight:700;font-size:var(--sz-text-3xl);color:#1a1814;letter-spacing:-.015em;">SENDERZZ</span><?php endif; ?></div>
    <div class="card">
        <div class="card-label">Código de rastreio</div>
        <div class="code"><?php echo esc_html( $code ); ?></div>
        <?php if ( $events ) : ?>
            <div class="timeline">
            <?php foreach ( $events as $i => $ev ) :
                $date_fmt = '';
                try { $dt = new \DateTime( $ev['date'] ); $date_fmt = $dt->format( 'd/m/Y \à\s H:i' ); } catch (\Exception $e) { $date_fmt = $ev['date']; }
            ?>
                <div class="ev">
                    <div class="ev-dot <?php echo $i === 0 ? 'latest' : ''; ?>"></div>
                    <div class="ev-desc"><?php echo esc_html( $ev['description'] ); ?></div>
                    <div class="ev-meta">
                        <?php if ( $date_fmt ) echo '<span>' . esc_html( $date_fmt ) . '</span>'; ?>
                        <?php if ( $ev['location'] ) echo '<span>📍 ' . esc_html( $ev['location'] ) . '</span>'; ?>
                    </div>
                </div>
            <?php endforeach; ?>
            </div>
        <?php else : ?>
            <div class="empty">
                <div class="empty-ico">📦</div>
                <div class="empty-title">Aguardando movimentação</div>
                <div class="empty-sub">O objeto foi registrado e em breve aparecerão atualizações de rastreio.</div>
            </div>
        <?php endif; ?>
    </div>
</div>
</body>
</html><?php
    }

    /**
     * Retorna métodos do ME agrupados por transportadora, excluindo
     * Azul Cargo Express (todas as modalidades) e Correios Mini Envios.
     *
     * Retorno: [ 'Correios' => [ mid => ['name'=>..,'company'=>..], ... ], ... ]
     */
    private function carrier_methods(): array {
        return senderzz_portal_carrier_methods(  );
    }

    /**
     * Retorna métodos flat (mid => method) para uso interno / checkout-hooks.
     */
    private function carrier_methods_flat(): array {
        return senderzz_portal_carrier_methods_flat(  );
    }

    private function get_preferred_carrier_ids( int $class_id ): array {
        return senderzz_portal_preferred_carrier_ids( $class_id );
    }

    private function set_preferred_carrier_ids( int $class_id, array $method_ids ): array {
        $option = defined('TP_PREFERIDA_OPTION') ? TP_PREFERIDA_OPTION : 'tp_preferida_map';
        $map = get_option( $option, [] );
        if ( ! is_array( $map ) ) $map = [];

        $method_ids = array_values( array_unique( array_filter( array_map( 'absint', $method_ids ) ) ) );

        if ( ! empty( $method_ids ) ) {
            // mantém o modo "mais barata", mas limita a cotação às modalidades escolhidas pelo produtor
            $map[$class_id] = [ 'modo' => 'mais_barata', 'permitidas' => $method_ids ];
        } else {
            // sem seleção = usar todas as modalidades disponíveis e deixar o checkout escolher a mais barata
            unset( $map[$class_id] );
        }

        update_option( $option, $map );
        if ( function_exists( 'wc_delete_shop_order_transients' ) ) wc_delete_shop_order_transients();
        if ( function_exists( 'WC' ) && WC()->cart ) {
            WC()->cart->calculate_shipping();
        }

        return [
            'success' => true,
            'message' => empty( $method_ids )
                ? 'Modalidades resetadas. O Senderzz voltará a recomendar automaticamente a opção mais barata disponível.'
                : 'Modalidades de frete atualizadas.',
        ];
    }


    private function get_blocked_carrier_ids( int $class_id ): array {
        return senderzz_portal_blocked_carrier_ids( $class_id );
    }

    private function set_blocked_carrier_ids( int $class_id, array $method_ids ): array {
        $option = defined('SENDERZZ_BLOCKED_OPTION') ? SENDERZZ_BLOCKED_OPTION : 'senderzz_blocked_carriers_map';
        $map = get_option( $option, [] );
        if ( ! is_array( $map ) ) $map = [];
        $method_ids = array_values( array_unique( array_filter( array_map( 'absint', $method_ids ) ) ) );
        if ( ! empty( $method_ids ) ) $map[$class_id] = [ 'bloqueadas' => $method_ids ]; else unset( $map[$class_id] );
        update_option( $option, $map );
        if ( function_exists( 'wc_delete_shop_order_transients' ) ) wc_delete_shop_order_transients();
        return [ 'success' => true, 'message' => empty( $method_ids ) ? 'Bloqueios removidos.' : 'Transportadoras bloqueadas atualizadas.' ];
    }

    // ── MAIN RENDER ────────────────────────────────────────────────────────

    public function render(): string {
        // logout handled in handle_logout() via template_redirect
        $user = Portal_Auth::get_current_user();

        // ── Mobile: redireciona para o app PWA ──────────────────────────────
        // O portal web não é responsivo — em dispositivos móveis exibimos uma
        // tela de direcionamento para o app em vez da versão bugada.
        if ( $this->is_mobile_browser() ) {
            return $this->mobile_redirect_screen();
        }
        // ── /Mobile ──────────────────────────────────────────────────────────

        // V1 removido — todos os usuários (incluindo operator) usam V2.
        // Este bloco só é atingido se dashboard_v2_active() retornar false,
        // o que não ocorre mais. Mantido como fallback de segurança.
        if ( $user ) {
            return $this->render_dashboard_v2( $user );
        }
        ob_start();
        echo $this->styles();
        echo $this->login();
        echo $this->scripts();
        return ob_get_clean();
    }

    /**
     * Dashboard V2 — entrega como documento próprio (Fase 1.1).
     *
     * Causa raiz corrigida: entregue via shortcode, a V2 nascia ANINHADA
     * no documento do tema e ficava exposta a CSS/JS do site (que oculta
     * elementos como <header>/<nav> na página do portal — o legado só
     * sobrevive a isso pela armadura de !important do portal.css).
     * A V2 não usa armadura: ela renderiza o próprio documento completo
     * em template_redirect e encerra — mesmo padrão já usado pela página
     * de rastreio deste plugin (maybe_render_tracking). Nenhum CSS do
     * tema participa; nenhum override é necessário.
     *
     * Fail-open: sem flag, sem usuário, OL, mobile ou template ausente →
     * retorna sem ecoar nada e o fluxo legado segue intacto.
     */
    public function maybe_render_dashboard_v2(): void {
        if ( ! $this->is_portal() || $this->is_tracking() ) {
            return;
        }
        if ( $this->is_mobile_browser() ) {
            return; // Redirect mobile→PWA permanece no fluxo legado.
        }
        $user = Portal_Auth::get_current_user();
        if ( ! $user || ! $this->dashboard_v2_active( $user ) ) {
            return;
        }
        $html = $this->render_dashboard_v2( $user );
        if ( '' === $html ) {
            return; // Template ausente: dashboard clássica assume.
        }
        echo $html; // phpcs:ignore WordPress.Security.EscapeOutput -- documento completo já escapado nos templates
        exit;
    }

    /**
     * Dashboard V2 — resolução da feature flag (Fase 1).
     *
     * Ordem de resolução (qualquer uma ativa):
     *  1. Option global `senderzz_dashboard_v2_enabled` = 'yes' (default OFF).
     *  2. Allowlist de piloto `senderzz_dashboard_v2_users` (ids de
     *     senderzz_portal_users; aceita array ou CSV).
     *  3. Preview `?sz_v2=1` (cookie 24h) — SOMENTE se a option
     *     `senderzz_dashboard_v2_preview` = 'yes'. `?sz_v2=0` desliga.
     *
     * Exclusões fixas da Fase 1: role `operator` (OL chega na Fase 7).
     * Mobile é tratado antes (redirect ao PWA permanece intacto).
     * Rollback: desligar a option restaura a dashboard clássica na hora.
     */
    private function dashboard_v2_active( $user ): bool {
        // V2 é o único front — todos os roles (incluindo operator) usam V2.
        // V1 removido. Flag de emergência: setar option senderzz_v2_emergency_off=yes
        // para desativar em produção sem deploy (volta ao login screen).
        if ( 'yes' === get_option( 'senderzz_v2_emergency_off', 'no' ) ) {
            return false;
        }
        return true;
    }

    /**
     * Dashboard V2 — renderiza o template da casca nova.
     * Retorna '' se o template não existir, para que render() caia
     * no fluxo legado sem tela em branco.
     */
    private function render_dashboard_v2( $user ): string {
        $template = TPC_PATH . 'templates/portal/v2/dashboard-v2.php';
        if ( ! file_exists( $template ) ) {
            return '';
        }
        $sz_v2_user = $user;
        ob_start();
        require $template;
        return (string) ob_get_clean();
    }

    /** Detecta User-Agent de dispositivo móvel. */
    private function is_mobile_browser(): bool {
        // Bypass: ?sz_force_desktop=1 — persiste por 24h em cookie
        if ( ! empty( $_GET['sz_force_desktop'] ) ) {
            setcookie( 'sz_force_desktop', '1', time() + 86400, '/', '', true, true );
            return false;
        }
        if ( ! empty( $_COOKIE['sz_force_desktop'] ) ) return false;

        $ua = strtolower( $_SERVER['HTTP_USER_AGENT'] ?? '' );
        if ( $ua === '' ) return false;
        // Exclui bots e crawlers para não afetar SEO/indexação
        if ( preg_match( '/bot|crawl|spider|slurp|facebookexternalhit/i', $ua ) ) return false;
        return (bool) preg_match( '/android|iphone|ipod|ipad|opera mini|iemobile|mobile|blackberry|phone/i', $ua );
    }

    /** Tela de boas-vindas para acesso mobile — direciona para o app PWA. */
    private function mobile_redirect_screen(): string {
        $logo = esc_url( senderzz_portal_logo_url(), [ 'http', 'https', 'data' ] );
        $app_url = esc_url( 'https://app.senderzz.com.br/app' );
        return '<!DOCTYPE html>
<html lang="pt-BR">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover">
<title>Senderzz — Acesse o App</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{min-height:100dvh;display:flex;flex-direction:column;align-items:center;justify-content:center;
     background:linear-gradient(160deg,#0f172a 0%,#1e2d3d 50%,#0f172a 100%);
     font-family:var(--sz-font);
     padding:32px 24px;text-align:center;color:#fff;}
.sz-mb-logo{height:48px;object-fit:contain;margin-bottom:32px;filter:drop-shadow(0 0 16px rgba(232,101,10,.35));}
.sz-mb-card{background:rgba(255,255,255,.04);border:1px solid rgba(255,255,255,.08);
            border-radius:28px;padding:36px 28px;max-width:400px;width:100%;}
.sz-mb-icon{width:72px;height:72px;border-radius:22px;background:linear-gradient(135deg,#E8650A,#f59e0b);
            display:flex;align-items:center;justify-content:center;margin:0 auto 24px;
            box-shadow:0 12px 32px rgba(232,101,10,.4);}
.sz-mb-icon svg{width:36px;height:36px;stroke:#fff;stroke-width:2;fill:none;}
h1{font-size:var(--sz-text-xl);font-weight:700;letter-spacing:-.015em;line-height:1.2;margin-bottom:12px;}
p{font-size:var(--sz-text-md);color:rgba(255,255,255,.6);line-height:1.6;margin-bottom:8px;}
.sz-mb-hint{font-size:var(--sz-text-meta);color:rgba(255,255,255,.35);margin-bottom:28px;}
.sz-mb-btn{display:flex;align-items:center;justify-content:center;gap:10px;
           background:linear-gradient(135deg,#E8650A,#f59e0b);color:#fff;
           text-decoration:none;font-size:var(--sz-text-lg);font-weight:700;letter-spacing:-.015em;
           border-radius:16px;padding:16px 28px;width:100%;
           box-shadow:0 8px 24px rgba(232,101,10,.45);
           transition:transform .15s,box-shadow .15s;}
.sz-mb-btn:active{transform:scale(.97);box-shadow:0 4px 12px rgba(232,101,10,.3);}
.sz-mb-btn svg{width:18px;height:18px;stroke:#fff;stroke-width:2.2;fill:none;flex-shrink:0;}
.sz-mb-sep{display:flex;align-items:center;gap:10px;margin:20px 0;color:rgba(255,255,255,.2);font-size:var(--sz-text-sm);}
.sz-mb-sep::before,.sz-mb-sep::after{content:"";flex:1;height:1px;background:rgba(255,255,255,.08);}
.sz-mb-desktop{font-size:var(--sz-text-meta);color:rgba(255,255,255,.3);line-height:1.5;}
.sz-mb-desktop a{color:rgba(232,101,10,.7);text-decoration:none;}
</style>
</head>
<body>
' . ( $logo ? '<img src="' . $logo . '" class="sz-mb-logo" alt="Senderzz">' : '<div style="font-size:var(--sz-text-3xl);font-weight:700;margin-bottom:32px;letter-spacing:-.015em">SENDERZZ</div>' ) . '
<div class="sz-mb-card">
  <div class="sz-mb-icon">
    <svg viewBox="0 0 24 24"><rect x="5" y="2" width="14" height="20" rx="2"/><circle cx="12" cy="17" r="1"/></svg>
  </div>
  <h1>Melhor no app!</h1>
  <p>Para a melhor experiência no celular, acesse pelo nosso aplicativo.</p>
  <p class="sz-mb-hint">Rápido, fluido e feito para mobile.</p>
  <a href="' . $app_url . '" class="sz-mb-btn">
    <svg viewBox="0 0 24 24"><path d="M12 2L2 7l10 5 10-5-10-5z"/><path d="M2 17l10 5 10-5"/><path d="M2 12l10 5 10-5"/></svg>
    Abrir o App Senderzz
  </a>
  <div class="sz-mb-sep">ou</div>
  <p class="sz-mb-desktop">Prefere continuar no browser? <a href="?sz_force_desktop=1">Acessar versão desktop</a></p>
</div>
</body>
</html>';
    }

    // ── LOGIN ──────────────────────────────────────────────────────────────

    private function login(): string {
        $n           = wp_create_nonce( 'senderzz_portal' );
        $logo        = esc_url( senderzz_portal_logo_url(), [ 'http', 'https', 'data' ] );
        $ajax_url    = esc_js( admin_url( 'admin-ajax.php' ) );
        $reset_token = isset( $_GET['sz_reset'] ) ? sanitize_text_field( wp_unslash( $_GET['sz_reset'] ) ) : '';

        return '
<style>
*{box-sizing:border-box;margin:0;padding:0}
html,body{height:100%}
body{font-family:-apple-system,BlinkMacSystemFont,"SF Pro Text",system-ui,sans-serif;overflow:hidden}
/* ── Layout ── */
.szl-wrap{height:100vh;display:flex}
/* ── Left hero ── */
.szl-left{flex:1;position:relative;background:linear-gradient(150deg,#0f172a 0%,#1a2035 45%,#7c1d06 100%);display:flex;flex-direction:column;justify-content:center;padding:56px 64px;color:#fff;overflow:hidden}
.szl-left::before{content:"";position:absolute;inset:0;background:radial-gradient(ellipse 80% 60% at 30% 80%,rgba(234,88,12,.18) 0%,transparent 70%)}
@media(max-width:900px){.szl-left{display:none}}
.szl-left-brand{display:flex;align-items:center;gap:10px;margin-bottom:40px}
.szl-left h1{font-size:40px;font-weight:900;line-height:1.1;letter-spacing:-.04em;margin-bottom:14px;max-width:440px}
.szl-left h1 em{color:#EA580C;font-style:normal;display:block}
.szl-left-sub{font-size:16px;color:#94a3b8;line-height:1.6;max-width:380px;margin-bottom:36px}
/* ── Notification feed ── */
.szl-feed{width:340px;max-width:100%}
.szl-feed-inner{display:flex;flex-direction:column;gap:10px}
.szl-notif{display:flex;align-items:center;gap:12px;background:rgba(255,255,255,.06);border:1px solid rgba(255,255,255,.1);border-radius:14px;padding:12px 16px;backdrop-filter:blur(8px);animation:szlSlideIn .4s ease both}
@keyframes szlSlideIn{from{opacity:0;transform:translateY(16px)}to{opacity:1;transform:translateY(0)}}
.szl-notif-icon{width:40px;height:40px;border-radius:10px;background:#EA580C;display:flex;align-items:center;justify-content:center;flex-shrink:0;font-size:18px}
.szl-notif-body{flex:1;min-width:0}
.szl-notif-title{font-size:13px;font-weight:700;color:#fff;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.szl-notif-msg{font-size:12px;color:#94a3b8;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.szl-notif-time{font-size:11px;color:#64748b;white-space:nowrap;margin-left:auto;flex-shrink:0;align-self:flex-start}
/* ── Right form ── */
.szl-right{width:460px;flex-shrink:0;display:flex;align-items:center;justify-content:center;padding:40px 32px;background:#fff;overflow-y:auto}
@media(max-width:900px){.szl-right{width:100%;padding:32px 24px}}
.szl-card{width:100%;max-width:380px}
.szl-logo{height:36px;object-fit:contain;margin-bottom:32px}
.szl-card h2{font-size:26px;font-weight:800;color:#0f172a;margin-bottom:6px;letter-spacing:-.03em}
.szl-hint{font-size:14px;color:#64748b;margin-bottom:28px;font-weight:400;line-height:1.5}
.szl-field{margin-bottom:16px}
.szl-field label{display:block;font-size:11px;font-weight:700;color:#374151;letter-spacing:.06em;text-transform:uppercase;margin-bottom:6px}
.szl-field input{width:100%;height:46px;border:1.5px solid #e2e8f0;border-radius:12px;padding:0 14px;font-size:14px;font-family:inherit;outline:none;transition:border-color .12s,box-shadow .12s;color:#0f172a;background:#f8fafc}
.szl-field input:focus{border-color:#EA580C;box-shadow:0 0 0 3px rgba(234,88,12,.12);background:#fff}
.szl-pw-wrap{position:relative}
.szl-pw-wrap input{padding-right:44px}
.szl-pw-btn{position:absolute;right:12px;top:50%;transform:translateY(-50%);background:none;border:none;cursor:pointer;color:#94a3b8;display:flex;align-items:center;padding:4px}
.szl-pw-btn:hover{color:#64748b}
.szl-btn{width:100%;height:48px;background:#EA580C;color:#fff;border:none;border-radius:12px;font-size:15px;font-weight:700;cursor:pointer;margin-top:4px;transition:background .12s,transform .08s;font-family:inherit;letter-spacing:.01em}
.szl-btn:hover{background:#c2410c;transform:translateY(-1px)}
.szl-btn:active{transform:translateY(0)}
.szl-btn:disabled{opacity:.6;cursor:not-allowed;transform:none}
.szl-btn-outline{width:100%;height:48px;background:transparent;color:#EA580C;border:1.5px solid #EA580C;border-radius:12px;font-size:15px;font-weight:700;cursor:pointer;margin-top:10px;transition:background .12s;font-family:inherit}
.szl-btn-outline:hover{background:rgba(234,88,12,.06)}
.szl-back{background:none;border:none;cursor:pointer;color:#EA580C;font-size:13px;font-weight:700;padding:0;margin-bottom:20px;font-family:inherit;display:flex;align-items:center;gap:4px}
.szl-link{background:none;border:none;cursor:pointer;color:#94a3b8;font-size:13px;font-weight:500;text-decoration:none;font-family:inherit;transition:color .12s}
.szl-link:hover{color:#EA580C;text-decoration:underline}
.szl-otp{width:100%;height:60px;border:1.5px solid #e2e8f0;border-radius:12px;text-align:center;font-size:30px;font-weight:700;letter-spacing:.3em;color:#0f172a;font-family:monospace;outline:none;margin-bottom:16px;transition:border-color .12s;background:#f8fafc}
.szl-otp:focus{border-color:#EA580C;box-shadow:0 0 0 3px rgba(234,88,12,.12);background:#fff}
.szl-alert{border-radius:10px;padding:10px 14px;font-size:13px;font-weight:600;margin-bottom:16px;display:none}
.szl-alert.error{background:#fef2f2;color:#dc2626;border:1px solid #fecaca}
.szl-alert.success{background:#f0fdf4;color:#16a34a;border:1px solid #bbf7d0}
.szl-sep{display:flex;align-items:center;gap:12px;margin:20px 0 4px;color:#cbd5e1;font-size:12px}
.szl-sep::before,.szl-sep::after{content:"";flex:1;height:1px;background:#e2e8f0}
.szl-forgotrow{text-align:center;margin-top:16px}
</style>
<div class="szl-wrap">
  <!-- ── Left: Hero ── -->
  <div class="szl-left">
    <div class="szl-left-brand">
      ' . ( $logo ? '<img src="' . $logo . '" style="height:28px;object-fit:contain;filter:brightness(0)invert(1)" alt="Senderzz">' : '<span style="font-size:22px;font-weight:900;letter-spacing:-.04em">SENDERZZ</span>' ) . '
    </div>
    <h1>Sua plataforma de<br>Cash on Delivery.<br><em>Completos que pagam.</em></h1>
    <p class="szl-left-sub">Gerencie pedidos, motoboys e comissões de afiliados em tempo real.</p>
    <!-- Notification feed animado -->
    <div class="szl-feed">
      <div class="szl-feed-inner" id="szl-feed"></div>
    </div>
  </div>
  <!-- ── Right: Form ── -->
  <div class="szl-right">
    <div class="szl-card">
      ' . ( $logo ? '<img src="' . $logo . '" class="szl-logo" alt="Senderzz">' : '' ) . '
      <!-- Step 1: Login -->
      <div id="szl-s1">
        <h2>Seja bem-vindo!</h2>
        <p class="szl-hint">Entre com suas credenciais para acessar o painel.</p>
        <div id="szl-m1" class="szl-alert error"></div>
        <div class="szl-field"><label>Email</label><input type="email" id="szl-em" placeholder="seu@email.com" autocomplete="email" onkeydown="if(event.key===\'Enter\')szlLogin()"></div>
        <div class="szl-field"><label>Senha</label><div class="szl-pw-wrap"><input type="password" id="szl-pw" placeholder="Sua senha" autocomplete="current-password" onkeydown="if(event.key===\'Enter\')szlLogin()"><button type="button" class="szl-pw-btn" onclick="szlTogglePw()" aria-label="Mostrar senha"><svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg></button></div></div>
        <div class="szl-forgotrow"><button type="button" class="szl-link" onclick="szlForgotPassword()">Esqueci minha senha</button></div>
        <button class="szl-btn" id="szl-btn1" onclick="szlLogin()" style="margin-top:18px">Entrar →</button>
        <div class="szl-sep">ou</div>
        <button type="button" class="szl-btn-outline" onclick="szlShowReg()">Criar nova conta</button>
      </div>
      <!-- Step 4: Register -->
      <div id="szl-s4" style="display:none">
        <button class="szl-back" onclick="szlShowLogin()">← Voltar ao login</button>
        <h2>Criar conta</h2>
        <p class="szl-hint">Acesse produtos, checkouts e métricas de venda.</p>
        <div id="szl-m4" class="szl-alert error"></div>
        <div style="display:grid;grid-template-columns:1fr 1fr;gap:12px">
          <div class="szl-field"><label>Nome completo</label><input type="text" id="szl-reg-name" placeholder="Seu nome" autocomplete="off" readonly onfocus="this.removeAttribute(\'readonly\')"></div>
          <div class="szl-field"><label>CPF</label><input type="text" id="szl-reg-cpf" placeholder="000.000.000-00" autocomplete="off" maxlength="14" oninput="szlFmtCpf(this)"></div>
        </div>
        <div class="szl-field"><label>E-mail</label><input type="text" id="szl-reg-email" placeholder="seu@email.com" autocomplete="off" readonly onfocus="this.removeAttribute(\'readonly\')"></div>
        <div class="szl-field"><label>Telefone</label><input type="text" id="szl-reg-phone" placeholder="(11) 99999-9999" autocomplete="off" maxlength="15" oninput="szlFmtPhone(this)"></div>
        <div style="display:grid;grid-template-columns:1fr 1fr;gap:12px">
          <div class="szl-field"><label>Senha</label><input type="password" id="szl-reg-pw" placeholder="Mín. 8 caracteres" autocomplete="new-password"></div>
          <div class="szl-field"><label>Confirmar</label><input type="password" id="szl-reg-pw2" placeholder="Repita a senha" autocomplete="new-password"></div>
        </div>
        <button class="szl-btn" id="szl-btn4" onclick="szlRegister()">Criar conta →</button>
      </div>
      <!-- Step 2: 2FA -->
      <div id="szl-s2" style="display:none">
        <button class="szl-back" onclick="szlBack()">← Voltar</button>
        <h2>Verificação 2FA</h2>
        <p class="szl-hint">Código de 6 dígitos enviado ao seu e-mail. Válido por 15 min.</p>
        <div id="szl-m2" class="szl-alert error"></div>
        <input type="text" id="szl-code" class="szl-otp" placeholder="000000" maxlength="6" inputmode="numeric" autocomplete="one-time-code" onkeydown="if(event.key===\'Enter\')szlVerify()">
        <button class="szl-btn" id="szl-btn2" onclick="szlVerify()">Verificar →</button>
        <div class="szl-forgotrow" style="margin-top:14px"><button type="button" class="szl-link" onclick="szlResend2FA()">Reenviar código</button></div>
      </div>
      <!-- Step 3: Reset password -->
      <div id="szl-s3" style="display:none">
        <h2>Nova senha</h2>
        <p class="szl-hint">Crie uma nova senha com no mínimo 8 caracteres.</p>
        <div id="szl-m3" class="szl-alert error"></div>
        <div class="szl-field"><label>Nova senha</label><input type="password" id="szl-np" placeholder="Mínimo 8 caracteres" autocomplete="new-password"></div>
        <div class="szl-field"><label>Confirmar senha</label><input type="password" id="szl-nc" placeholder="Repita a senha" autocomplete="new-password"></div>
        <button class="szl-btn" id="szl-btn3" onclick="szlResetPassword()">Salvar nova senha</button>
      </div>
    </div>
  </div>
</div>
<script>
(function(){
var AJ="' . $ajax_url . '",N="' . esc_js($n) . '",TK="",RT="' . esc_js($reset_token) . '";
function msg(id,m,t){var e=document.getElementById(id);if(!e)return;e.className="szl-alert "+(t||"error");e.textContent=m;e.style.display=m?"block":"none";}
function btn(id,on,t){var b=document.getElementById(id);if(!b)return;b.disabled=!!on;b.textContent=on?(t||"Aguarde…"):b._o||(on?"Aguarde…":b.textContent);if(!on&&b._o)b.textContent=b._o;if(on&&!b._o)b._o=b.textContent;}
async function ajax(d){try{var r=await fetch(AJ,{method:"POST",credentials:"same-origin",body:new URLSearchParams(d)});var t=await r.text();try{return JSON.parse(t);}catch(e){var s=t.indexOf("{"),en=t.lastIndexOf("}");if(s>=0&&en>s)try{return JSON.parse(t.slice(s,en+1));}catch(_){}return{success:false,data:{message:"Resposta inválida."}};};}catch(e){return{success:false,data:{message:"Erro de conexão."}}; }}
window.szlTogglePw=function(){var e=document.getElementById("szl-pw");e.type=e.type==="password"?"text":"password";};
window.szlLogin=async function(){var em=document.getElementById("szl-em").value.trim(),pw=document.getElementById("szl-pw").value;if(!em||!pw){msg("szl-m1","Preencha email e senha.");return;}btn("szl-btn1",true,"Entrando…");msg("szl-m1","");var r=await ajax({action:"senderzz_portal",szaction:"login_step1",email:em,password:pw,remember:7,_ajax_nonce:N});btn("szl-btn1",false);if(r&&r.success){if(r.data&&r.data.direct_login){location.reload();return;}TK=(r.data&&r.data.temp_token)||"";document.getElementById("szl-s1").style.display="none";document.getElementById("szl-s2").style.display="block";setTimeout(function(){document.getElementById("szl-code").focus();},80);}else{msg("szl-m1",(r&&r.data&&r.data.message)||"Credenciais inválidas.");}};
window.szlVerify=async function(){var c=document.getElementById("szl-code").value.trim();if(c.length!==6){msg("szl-m2","Digite 6 dígitos.");return;}btn("szl-btn2",true,"Verificando…");msg("szl-m2","");var r=await ajax({action:"senderzz_portal",szaction:"login_step2",temp_token:TK,code:c,remember:7,_ajax_nonce:N});btn("szl-btn2",false);if(r&&r.success){location.reload();}else{msg("szl-m2",(r&&r.data&&r.data.message)||"Código inválido.");}};
window.szlBack=function(){TK="";document.getElementById("szl-s2").style.display="none";document.getElementById("szl-s1").style.display="block";msg("szl-m2","");};
window.szlForgotPassword=async function(){var em=document.getElementById("szl-em").value.trim();if(!em){msg("szl-m1","Informe seu e-mail antes de clicar em Esqueci a senha.");return;}msg("szl-m1","");var r=await ajax({action:"senderzz_portal",szaction:"request_password_reset",email:em,_ajax_nonce:N});msg("szl-m1",r.data&&r.data.message?r.data.message:"Verifique seu e-mail.",r&&r.success?"success":"error");};
window.szlResend2FA=async function(){if(!TK){msg("szl-m2","Sessão expirada. Faça login novamente.");return;}var r=await ajax({action:"senderzz_portal",szaction:"resend_2fa",temp_token:TK,_ajax_nonce:N});msg("szl-m2",r.data&&r.data.message?r.data.message:"Verifique seu e-mail.",r&&r.success?"success":"error");};
window.szlResetPassword=async function(){var np=document.getElementById("szl-np").value,nc=document.getElementById("szl-nc").value;if(!np||np.length<8){msg("szl-m3","A senha deve ter no mínimo 8 caracteres.");return;}if(np!==nc){msg("szl-m3","As senhas não coincidem.");return;}btn("szl-btn3",true,"Salvando…");var r=await ajax({action:"senderzz_portal",szaction:"complete_password_reset",token:RT,new_password:np,_ajax_nonce:N});btn("szl-btn3",false);msg("szl-m3",r.data&&r.data.message?r.data.message:r&&r.success?"Senha redefinida.":"Erro.",r&&r.success?"success":"error");if(r&&r.success)setTimeout(function(){location.reload();},2000);};
if(RT){document.getElementById("szl-s1").style.display="none";document.getElementById("szl-s3").style.display="block";}
window.szlShowReg=function(){["szl-s1","szl-s2","szl-s3"].forEach(function(id){var e=document.getElementById(id);if(e)e.style.display="none";});var s4=document.getElementById("szl-s4");if(s4)s4.style.display="block";};
window.szlShowLogin=function(){["szl-s2","szl-s3","szl-s4"].forEach(function(id){var e=document.getElementById(id);if(e)e.style.display="none";});var s1=document.getElementById("szl-s1");if(s1)s1.style.display="block";};
window.szlFmtCpf=function(i){var v=i.value.replace(/\D/g,"").slice(0,11);i.value=v.replace(/(\d{3})(\d)/,"$1.$2").replace(/(\d{3})(\d)/,"$1.$2").replace(/(\d{3})(\d{1,2})$/,"$1-$2");};
window.szlFmtPhone=function(i){var v=i.value.replace(/\D/g,"").slice(0,11);if(v.length>=2)v="("+v.slice(0,2)+") "+v.slice(2);if(v.length>10)v=v.slice(0,10)+"-"+v.slice(10);i.value=v;};
window.szlRegister=async function(){
  var name=document.getElementById("szl-reg-name").value.trim();
  var cpf=document.getElementById("szl-reg-cpf").value.trim();
  var email=document.getElementById("szl-reg-email").value.trim();
  var phone=document.getElementById("szl-reg-phone").value.trim();
  var pw=document.getElementById("szl-reg-pw").value;
  var pw2=document.getElementById("szl-reg-pw2").value;
  if(!name||!email||!pw){msg("szl-m4","Preencha nome, e-mail e senha.");return;}
  if(pw.length<8){msg("szl-m4","Senha deve ter pelo menos 8 caracteres.");return;}
  if(pw!==pw2){msg("szl-m4","As senhas não coincidem.");return;}
  btn("szl-btn4",true,"Criando conta…");msg("szl-m4","");
  var fd=new FormData();
  fd.append("action","senderzz_portal");fd.append("szaction","portal_register");
  fd.append("_ajax_nonce",N);fd.append("name",name);fd.append("email",email);
  fd.append("password",pw);fd.append("cpf",cpf);fd.append("phone",phone);
  var r=await ajax({action:"senderzz_portal",szaction:"portal_register",_ajax_nonce:N,name:name,email:email,password:pw,cpf:cpf,phone:phone});
  btn("szl-btn4",false);
  if(r&&r.success){msg("szl-m4","Conta criada com sucesso! Entrando…","success");setTimeout(function(){location.reload();},1200);}
  else{msg("szl-m4",(r&&r.data&&r.data.message)||"Erro ao criar conta.");}
};
// Notification feed animation
(function(){
var feed=document.getElementById("szl-feed");
if(!feed)return;
var items=[
  {v:"R$ 89,90",t:"há 1 min"},  {v:"R$ 134,00",t:"há 2 min"}, {v:"R$ 67,50",t:"há 3 min"},
  {v:"R$ 210,00",t:"há 4 min"}, {v:"R$ 55,00",t:"há 6 min"},  {v:"R$ 98,90",t:"há 7 min"},
  {v:"R$ 175,00",t:"há 9 min"}, {v:"R$ 42,00",t:"há 11 min"}, {v:"R$ 320,00",t:"há 12 min"},
  {v:"R$ 76,90",t:"há 14 min"}
];
var icons=["🛵","📦","✅","🎯","💰"];
var idx=0;
function addCard(){
  if(!feed)return;
  var item=items[idx%items.length];idx++;
  var icon=icons[idx%icons.length];
  var el=document.createElement("div");
  el.className="szl-notif";
  el.style.animationDelay="0s";
  el.innerHTML=\'<div class="szl-notif-icon">\'+icon+\'</div>\'+
    \'<div class="szl-notif-body">\'+
      \'<div class="szl-notif-title">Pedido agendado!</div>\'+
      \'<div class="szl-notif-msg">Sua comissão é \'+item.v+\'</div>\'+
    \'</div>\'+
    \'<div class="szl-notif-time">\'+item.t+\'</div>\';
  feed.insertBefore(el,feed.firstChild);
  while(feed.children.length>4)feed.removeChild(feed.lastChild);
}
for(var i=0;i<4;i++){(function(delay){setTimeout(addCard,delay);})(i*180);}
setInterval(addCard,2800);
})();
})();
</script>';
    }

    // ── OPERATOR DASHBOARD ─────────────────────────────────────────────────

    // operator_dashboard() e dashboard() removidos — V1 descontinuado.
    // Todos os roles (incluindo operator) usam dashboard_v2().

    private function senderzz_wallet_user_id_for_portal_user( object $user ): int {
        return senderzz_portal_wallet_user_id( $user );
    }

    // render_stock_panel() removido — V2 usa templates/portal/v2/sections/stock.php diretamente.

    public function render_products_panel( int $class_id, ?object $portal_user = null ): string {
        if ( ! function_exists( 'wc_get_products' ) ) {
            return '<div class="sz-card"><p>Produtos indisponíveis no momento.</p></div>';
        }

        $is_affiliate = $portal_user && function_exists( 'sz_aff_portal_user_must_use_affiliate_scope' ) && sz_aff_portal_user_must_use_affiliate_scope( $portal_user );
        $affiliate_links = [];
        if ( $is_affiliate && function_exists( 'sz_aff_get_visible_checkout_links_for_portal_user' ) ) {
            $affiliate_links = sz_aff_get_visible_checkout_links_for_portal_user( $portal_user );
        }

        $class_ids = $GLOBALS['_sz_stock_class_ids'] ?? ( $class_id > 0 ? [ $class_id ] : [] );
        // Senderzz v282: fonte canônica da tela Produtos é a classe vinculada ao usuário do portal.
        // Não depender de post_author nem de metas inexistentes no produto.
        if ( ! $is_affiliate && $portal_user && function_exists( 'sz_get_user_class_ids' ) ) {
            $producer_class_ids = sz_get_user_class_ids( $portal_user );
            if ( ! empty( $producer_class_ids ) ) {
                $class_ids = $producer_class_ids;
            }
        }
        if ( $is_affiliate && function_exists( 'sz_aff_get_allowed_shipping_class_ids_for_portal_user' ) ) {
            $aff_class_ids = sz_aff_get_allowed_shipping_class_ids_for_portal_user( $portal_user );
            if ( ! empty( $aff_class_ids ) ) $class_ids = $aff_class_ids;
        }

        $all_products = wc_get_products( [
            'limit'   => -1,
            'status'  => 'publish',
            'type'    => [ 'simple', 'variation', 'variable' ],
            'orderby' => 'title',
            'order'   => 'ASC',
            'return'  => 'objects',
        ] );

        $items = [];
        foreach ( $all_products as $p ) {
            if ( ! $p instanceof \WC_Product ) continue;
            $cid = (int) $p->get_shipping_class_id();
            if ( $cid <= 0 && $p->is_type( 'variation' ) ) {
                $parent = wc_get_product( $p->get_parent_id() );
                $cid = $parent ? (int) $parent->get_shipping_class_id() : 0;
            }
            if ( ! empty( $class_ids ) && ! in_array( $cid, array_map( 'intval', (array) $class_ids ), true ) ) continue;
            $type = $p->get_type();
            $name = $p->get_name();
            $sku  = (string) $p->get_sku();
            $hay  = strtolower( remove_accents( $name . ' ' . $sku . ' ' . $type ) );
            // Senderzz v282: não descartar produto apenas por nome comercial.
            // Antes a palavra "teste" fazia o produto sumir da tela Produtos mesmo com classe correta.
            // Mantemos apenas exclusões estruturais de tipos compostos/combos, sem bloquear nomes simples.
            if ( in_array( $type, [ 'grouped', 'bundle', 'composite', 'woosb', 'mix-and-match' ], true ) || preg_match( '/\b(kit|kits|combo|combos|pacote|pack|bundle|conjunto|recarga)\b/i', $hay ) || preg_match( '/\b\d+\s*(frasco|frascos|pomada|pomadas|gota|gotas)\b/i', $hay ) || preg_match( '/\+/', $hay ) ) continue;
            $qty = $p->get_stock_quantity();
            $items[ (int) $p->get_id() ] = [
                'id' => (int) $p->get_id(),
                'name' => $name,
                'sku' => $sku,
                'available' => max( 0, (int) ( $qty ?? 0 ) ),
                'reserved' => 0,
                'in_route' => 0,
                'delivered' => 0,
                'frustrated' => 0,
                'image' => wp_get_attachment_image_url( $p->get_image_id(), 'thumbnail' ),
                'checkouts' => [],
                'shipments' => [],
                'movements' => [],
            ];
        }

        // Links/checkouts: no produtor usa links próprios; no afiliado somente links liberados.
        global $wpdb;
        $links = [];
        if ( $is_affiliate ) {
            $links = $affiliate_links;
        } elseif ( $portal_user ) {
            $links_table = $wpdb->prefix . 'senderzz_checkout_links';
            if ( function_exists( 'senderzz_portal_ensure_checkout_links_table' ) ) senderzz_portal_ensure_checkout_links_table();
            $links = $wpdb->get_results( $wpdb->prepare(
                "SELECT * FROM {$links_table} WHERE user_id = %d ORDER BY created_at DESC LIMIT 300",
                (int) $portal_user->id
            ), ARRAY_A ) ?: [];
        }

        $product_name_by_id = [];
        foreach ( $items as $pid => $it ) $product_name_by_id[ $pid ] = $it['name'];

        foreach ( $links as $link ) {
            $linked_product_ids = [];
            $payload_raw = (string) ( $link['payload'] ?? '' );
            if ( $payload_raw !== '' ) {
                $payload = json_decode( $payload_raw, true );
                if ( is_array( $payload ) ) {
                    $walk = function( $node ) use ( &$walk, &$linked_product_ids ) {
                        if ( is_array( $node ) ) {
                            foreach ( $node as $k => $v ) {
                                $key = strtolower( (string) $k );
                                if ( in_array( $key, [ 'product_id', 'produto_id', 'id_produto', 'id', 'pid' ], true ) && is_numeric( $v ) ) {
                                    $linked_product_ids[] = (int) $v;
                                }
                                if ( is_array( $v ) ) $walk( $v );
                            }
                        }
                    };
                    $walk( $payload );
                }
            }

            $txt = strtolower( remove_accents( implode( ' ', [
                (string) ( $link['name'] ?? '' ),
                (string) ( $link['display_name'] ?? '' ),
                (string) ( $link['components_text'] ?? '' ),
                $payload_raw,
                (string) ( $link['url'] ?? '' ),
            ] ) ) );

            foreach ( $items as $pid => &$it ) {
                $needle = strtolower( remove_accents( $it['name'] ) );
                $sku = strtolower( remove_accents( (string) $it['sku'] ) );
                $pid_patterns = [ '"'.$pid.'"', ':'.$pid, 'product_id='.$pid, 'produto_id='.$pid, 'pid='.$pid, 'id='.$pid ];
                $matched_id = in_array( (int) $pid, array_map( 'intval', $linked_product_ids ), true );
                $matched_text = ( $needle && strpos( $txt, $needle ) !== false ) || ( $sku && strpos( $txt, $sku ) !== false );
                foreach ( $pid_patterns as $pat ) { if ( strpos( $txt, strtolower( $pat ) ) !== false ) { $matched_id = true; break; } }
                if ( $matched_id || $matched_text ) {
                    $it['checkouts'][] = $link;
                }
            }
            unset( $it );
        }

        $orders = \WC_MelhorEnvio\Portal\Portal_Orders::get_orders( ! empty( $class_ids ) ? $class_ids : $class_id, 1, 500 );
        if ( function_exists( 'senderzz_user_can_access_order' ) && ! empty( $portal_user ) ) {
            $orders = array_values( array_filter( (array) $orders, static function( $o ) use ( $portal_user ) {
                $oid = (int) ( $o['id'] ?? 0 );
                return $oid > 0 && senderzz_user_can_access_order( $oid, $portal_user, 'view' );
            } ) );
        }
        $reserved_statuses   = [ 'separado', 'erro', 'agendado', 'embalado', 'on-hold', 'processing', 'pending' ];
        $route_statuses      = [ 'em_rota', 'em-rota', 'a_caminho', 'acaminho' ];
        $delivered_statuses  = [ 'entregue', 'completed', 'completo' ];
        $frustrated_statuses = [ 'frustrado', 'frustracao' ];
        // Movimentações agora são somente movimentos originados por pedidos da plataforma:
        // saída/reserva quando o pedido entra no fluxo e devolução quando o pedido é cancelado/estornado.
        $returned_statuses  = [ 'cancelled', 'canceled', 'refunded', 'failed', 'cancelado', 'estornado', 'devolvido' ];
        $kit_map = defined( 'SENDERZZ_KIT_STOCK_OPTION' ) ? (array) get_option( SENDERZZ_KIT_STOCK_OPTION, [] ) : [];

        foreach ( $orders as $o ) {
            $st = (string) ( $o['status'] ?? '' );
            $oid = (int) ( $o['id'] ?? 0 );
            $wc_order = $oid > 0 ? wc_get_order( $oid ) : null;
            if ( ! $wc_order ) continue;
            $date = (string) ( $o['date'] ?? '' );
            foreach ( $wc_order->get_items() as $line ) {
                $pid = (int) $line->get_product_id();
                $qty = max( 0, (int) $line->get_quantity() );
                if ( $qty <= 0 || $pid <= 0 ) continue;
                $targets = [];
                if ( isset( $kit_map[ $pid ] ) ) {
                    foreach ( (array) ( $kit_map[ $pid ]['items'] ?? [] ) as $component ) {
                        $cpid = (int) ( $component['product_id'] ?? 0 );
                        $cqty = max( 1, (int) ( $component['qty'] ?? 1 ) );
                        if ( $cpid > 0 ) $targets[ $cpid ] = ( $targets[ $cpid ] ?? 0 ) + ( $qty * $cqty );
                    }
                } else {
                    $targets[ $pid ] = $qty;
                }
                foreach ( $targets as $tid => $tqty ) {
                    if ( ! isset( $items[ $tid ] ) ) continue;
                    if ( in_array( $st, $reserved_statuses, true ) )   $items[ $tid ]['reserved']   += (int) $tqty;
                    if ( in_array( $st, $route_statuses, true ) )      $items[ $tid ]['in_route']   += (int) $tqty;
                    if ( in_array( $st, $delivered_statuses, true ) )  $items[ $tid ]['delivered']  += (int) $tqty;
                    if ( in_array( $st, $frustrated_statuses, true ) ) $items[ $tid ]['frustrated'] += (int) $tqty;
                    $label = function_exists( 'senderzz_portal_status_label' ) ? senderzz_portal_status_label( $st ) : $st;

                    // Pedidos não entram em "Envios". Envios é somente remessa de estoque do produtor.
                    // Movimentações ficam restritas ao estoque causado por pedidos da plataforma:
                    // saída/reserva/entrega e devolução/cancelamento.
                    if ( in_array( $st, $returned_statuses, true ) ) {
                        $items[ $tid ]['movements'][] = [
                            'type' => 'Devolução / Pedido cancelado',
                            'qty' => (int) $tqty,
                            'date' => $date,
                            'ref' => 'Pedido #' . (string) ( $o['number'] ?? $oid ),
                            'meta' => $label,
                            'direction' => 'in',
                        ];
                    } elseif ( in_array( $st, $reserved_statuses, true ) || in_array( $st, $route_statuses, true ) || in_array( $st, $delivered_statuses, true ) || in_array( $st, $frustrated_statuses, true ) ) {
                        $items[ $tid ]['movements'][] = [
                            'type' => in_array( $st, $delivered_statuses, true ) ? 'Saída / Pedido entregue' : ( in_array( $st, $route_statuses, true ) ? 'Saída / Em rota' : ( in_array( $st, $frustrated_statuses, true ) ? 'Custódia / Frustrado' : 'Saída / Reserva de pedido' ) ),
                            'qty' => (int) $tqty,
                            'date' => $date,
                            'ref' => 'Pedido #' . (string) ( $o['number'] ?? $oid ),
                            'meta' => $label,
                            'direction' => 'out',
                        ];
                    }
                }
            }
        }


        // Envios = somente remessas/envios de estoque feitos pelo produtor.
        // Movimentações = auditoria detalhada de entradas de estoque e saídas/reservas por pedidos.
        if ( $portal_user && isset( $wpdb ) ) {
            $sz_ship_table = $wpdb->prefix . ( defined( 'SENDERZZ_STOCK_SHIP_TABLE' ) ? SENDERZZ_STOCK_SHIP_TABLE : 'senderzz_stock_shipments' );
            $sz_item_table = $wpdb->prefix . ( defined( 'SENDERZZ_STOCK_SHIP_ITEM_TABLE' ) ? SENDERZZ_STOCK_SHIP_ITEM_TABLE : 'senderzz_stock_shipment_items' );
            $sz_ship_exists = $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $sz_ship_table ) );
            $sz_item_exists = $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $sz_item_table ) );
            if ( $sz_ship_exists === $sz_ship_table && $sz_item_exists === $sz_item_table ) {
                $sz_stock_shipments = $wpdb->get_results( $wpdb->prepare(
                    "SELECT s.id, s.status, s.carrier, s.tracking, s.sent_at, s.delivered_at, s.concluded_at, s.created_at, i.product_id, i.variation_id, i.qty_sent, i.qty_received
                     FROM {$sz_ship_table} s
                     INNER JOIN {$sz_item_table} i ON i.shipment_id = s.id
                     WHERE s.user_id = %d
                     ORDER BY COALESCE(s.concluded_at, s.delivered_at, s.sent_at, s.created_at) DESC
                     LIMIT 300",
                    (int) $portal_user->id
                ), ARRAY_A ) ?: [];

                foreach ( $sz_stock_shipments as $szs ) {
                    $pid = ! empty( $szs['variation_id'] ) ? (int) $szs['variation_id'] : (int) $szs['product_id'];
                    $prod = $pid > 0 ? wc_get_product( $pid ) : null;
                    $target_id = 0;
                    if ( isset( $items[ $pid ] ) ) {
                        $target_id = $pid;
                    } elseif ( $prod instanceof \WC_Product && $prod->is_type( 'variation' ) && isset( $items[ (int) $prod->get_parent_id() ] ) ) {
                        $target_id = (int) $prod->get_parent_id();
                    }
                    if ( ! $target_id || ! isset( $items[ $target_id ] ) ) continue;

                    $sent_qty = max( 0, (int) ( $szs['qty_sent'] ?? 0 ) );
                    $received_qty = max( 0, (int) ( $szs['qty_received'] ?? 0 ) );
                    $shown_qty = $received_qty > 0 ? $received_qty : $sent_qty;
                    if ( $shown_qty <= 0 ) continue;

                    $when = ! empty( $szs['concluded_at'] ) ? $szs['concluded_at'] : ( ! empty( $szs['delivered_at'] ) ? $szs['delivered_at'] : ( ! empty( $szs['sent_at'] ) ? $szs['sent_at'] : $szs['created_at'] ) );
                    $date = $when ? wp_date( 'd/m/Y H:i', strtotime( $when . ' UTC' ) ) : '—';
                    $status_key = (string) ( $szs['status'] ?? '' );
                    $status_map = [ 'pendente' => 'Pendente', 'enviado' => 'Enviado pelo produtor', 'entregue' => 'Recebido', 'concluido' => 'Concluído' ];
                    $status_label = $status_map[ $status_key ] ?? ucfirst( str_replace( '_', ' ', $status_key ) );
                    $meta = trim( (string) ( $szs['carrier'] ?: 'Envio de estoque' ) . ( ! empty( $szs['tracking'] ) ? ' · ' . $szs['tracking'] : '' ) );

                    $items[ $target_id ]['shipments'][] = [
                        'order' => 'Envio #' . (int) $szs['id'],
                        'date' => $date,
                        'status' => $status_label . ( $meta ? ' · ' . $meta : '' ),
                        'qty' => $shown_qty,
                    ];

                    // Não adiciona remessa de estoque em Movimentações.
                    // Movimentações mostra somente entradas/saídas/devoluções originadas por pedidos da plataforma.
                }
            }
        }

        foreach ( $items as &$sz_item_for_sort ) {
            if ( ! empty( $sz_item_for_sort['shipments'] ) ) {
                usort( $sz_item_for_sort['shipments'], static function( $a, $b ) { return strcmp( (string) ( $b['date'] ?? '' ), (string) ( $a['date'] ?? '' ) ); } );
            }
            if ( ! empty( $sz_item_for_sort['movements'] ) ) {
                usort( $sz_item_for_sort['movements'], static function( $a, $b ) { return strcmp( (string) ( $b['date'] ?? '' ), (string) ( $a['date'] ?? '' ) ); } );
            }
        }
        unset( $sz_item_for_sort );

        // No afiliado, não mostra produto sem link liberado.
        // v259: se não há checkouts mas há produtos da classe do produtor, mantém os items
        // para exibir card informativo "Aguardando checkout" em vez de tela vazia.
        if ( $is_affiliate ) {
            $items_with_checkout    = array_filter( $items, function( $it ) { return ! empty( $it['checkouts'] ); } );
            $items_without_checkout = array_filter( $items, function( $it ) { return empty( $it['checkouts'] ); } );
            // Se há ao menos um item com checkout, exibe só esses (comportamento original).
            // Se nenhum tem checkout mas há itens da classe, mantém todos marcados como pendentes.
            $items = ! empty( $items_with_checkout ) ? $items_with_checkout : $items;
            $sz_affiliate_no_checkout = empty( $items_with_checkout ) && ! empty( $items_without_checkout );
        } else {
            $sz_affiliate_no_checkout = false;
        }

        // Produtos disponíveis no select de criação do checkout do produto.
        // Não reutiliza $products de outros métodos; aqui a fonte correta é $all_products.
        $sz_checkout_products = [];
        foreach ( $all_products as $sz_p ) {
            if ( ! $sz_p instanceof \WC_Product ) continue;
            $sz_cid = (int) $sz_p->get_shipping_class_id();
            if ( $sz_cid <= 0 && $sz_p->is_type( 'variation' ) ) {
                $sz_parent = wc_get_product( $sz_p->get_parent_id() );
                $sz_cid = $sz_parent ? (int) $sz_parent->get_shipping_class_id() : 0;
            }
            if ( ! empty( $class_ids ) && ! in_array( $sz_cid, array_map( 'intval', (array) $class_ids ), true ) ) continue;
            $sz_type = $sz_p->get_type();
            if ( ! in_array( $sz_type, [ 'simple', 'variable', 'variation' ], true ) ) continue;
            $sz_checkout_products[] = $sz_p;
        }

        ob_start();
        ?>
        <div class="sz-section-pad sz-products-page-v135">
            <?php if ( empty( $items ) ) : ?>
                <div class="sz-card"><p style="margin:0;color:#64748b;font-weight:700"><?php echo $is_affiliate ? 'Nenhum produto/link liberado para esta afiliação.' : 'Nenhum produto disponível.'; ?></p></div>
            <?php elseif ( ! empty( $sz_affiliate_no_checkout ) ) : ?>
                <div class="sz-card" style="padding:22px 24px;border-radius:20px">
                    <h3 style="margin:0 0 6px;font-size:var(--sz-text-lg);font-weight:700;color:var(--tx,#111827)">Produtos vinculados à sua afiliação</h3>
                    <p style="margin:0 0 16px;color:#64748b;font-size:var(--sz-text-base);font-weight:700">O produtor ainda não publicou checkouts. Quando estiver disponível, seus links aparecerão aqui.</p>
                    <?php foreach ( $items as $it ) : ?>
                    <div style="display:flex;align-items:center;gap:14px;padding:14px;border:1px solid var(--bd,#e5e7eb);border-radius:14px;margin-bottom:10px;background:var(--c2,#f8fafc)">
                        <?php if ( ! empty( $it['image'] ) ) : ?><img src="<?php echo esc_url( $it['image'] ); ?>" style="width:48px;height:48px;border-radius:10px;object-fit:cover;border:1px solid var(--bd,#e5e7eb)" alt=""><?php endif; ?>
                        <div style="flex:1;min-width:0">
                            <div style="font-weight:700;color:var(--tx,#111827);font-size:var(--sz-text-md)"><?php echo esc_html( $it['name'] ); ?></div>
                            <?php if ( $it['sku'] ) : ?><div style="font-size:var(--sz-text-sm);color:#94a3b8;margin-top:2px">SKU: <?php echo esc_html( $it['sku'] ); ?></div><?php endif; ?>
                        </div>
                        <span style="display:inline-flex;align-items:center;height:34px;padding:0 14px;border-radius:10px;background:#f1f5f9;color:#64748b;font-size:var(--sz-text-meta);font-weight:700">⏳ Aguardando checkout</span>
                    </div>
                    <?php endforeach; ?>
                </div>
            <?php else : $first = true; ?>
                <div class="sz-products-v135-tabs" role="tablist" aria-label="Produtos">
                    <?php foreach ( $items as $it ) : ?>
                        <button type="button" class="sz-product-tab <?php echo $first ? 'is-active' : ''; ?>" onclick="szProductsTab('<?php echo esc_attr( $it['id'] ); ?>',this)">📦 <?php echo esc_html( $it['name'] ); ?></button>
                    <?php $first = false; endforeach; ?>
                </div>
                <?php $first = true; foreach ( $items as $it ) : ?>
                    <div class="sz-product-panel <?php echo $first ? 'is-active' : ''; ?>" data-product-id="<?php echo esc_attr( $it['id'] ); ?>">
                        <div class="sz-prod-hero-v135">
                            <div class="sz-prod-title-v135">
                                <?php if ( ! empty( $it['image'] ) ) : ?><img src="<?php echo esc_url( $it['image'] ); ?>" alt=""><?php endif; ?>
                                <div><h2><?php echo esc_html( $it['name'] ); ?></h2><?php if ( $it['sku'] ) : ?><p>ID/SKU: <?php echo esc_html( $it['sku'] ); ?></p><?php else : ?><p>Checkouts, estoque, envios e movimentações deste produto.</p><?php endif; ?></div>
                            </div>
                            <?php if ( ! $is_affiliate ) : ?>
                            <div class="sz-prod-kpis-v135">
                                <div><span>Disponíveis</span><strong><?php echo esc_html( (string) $it['available'] ); ?></strong><small>unidades</small></div>
                                <div><span>Reservados</span><strong><?php echo esc_html( (string) $it['reserved'] ); ?></strong><small>unidades</small></div>
                                <div><span>Em rota</span><strong><?php echo esc_html( (string) $it['in_route'] ); ?></strong><small>unidades</small></div>
                                <div><span>Entregues</span><strong><?php echo esc_html( (string) $it['delivered'] ); ?></strong><small>unidades</small></div>
                                <div><span>Frustrados</span><strong><?php echo esc_html( (string) $it['frustrated'] ); ?></strong><small>unidades</small></div>
                            </div>
                            <?php endif; ?>
                        </div>

                        <div class="sz-prod-subnav-v136">
                            <button type="button" class="is-active" onclick="szProductSubtab(this,'checkouts')">🧾 Checkouts</button>
                            <?php if ( ! $is_affiliate ) : ?>
                            <button type="button" onclick="szProductSubtab(this,'envios')">📦 Envios</button>
                            <button type="button" onclick="szProductSubtab(this,'movimentacoes')">📋 Movimentações</button>
                            <?php endif; ?>
                        </div>
                        <div class="sz-prod-layout-v135">
                            <div class="sz-prod-main-v135">
                                <div class="sz-prod-card-v135 sz-prod-section-v136 is-active" data-prod-section="checkouts">
                                    <div class="sz-prod-card-head-v135"><div><h3>Checkouts deste produto</h3><p>Links vinculados a este produto. Se o checkout tiver mais produtos, ele aparece em todos os produtos vinculados.</p></div><?php if ( ! $is_affiliate ) : ?><button type="button" onclick="szProductCheckoutFormToggle('<?php echo esc_js( $it['id'] ); ?>',true)">+ Gerar checkout</button><?php endif; ?></div>
                                    <div class="sz-prod-table-v135">
                                        <div class="sz-prod-row-v135 head <?php echo $is_affiliate ? 'is-affiliate-row' : ''; ?>"><span>Nome / identificação</span><span>Produtos</span><span>Valor</span><span>Comissão</span><?php if ( $is_affiliate ) : ?><span>Valor comissão</span><span>Link</span><?php else : ?><span>Links</span><span>Ações</span><?php endif; ?></div>
                                        <?php if ( empty( $it['checkouts'] ) ) : ?>
                                            <div class="sz-prod-empty-v135">Nenhum checkout vinculado a este produto.</div>
                                        <?php else :
                                            // Monta pares Expedição/Motoboy também para links antigos que só têm slug/token,
                                            // ou para quando a linha exibida no produto é o espelho Motoboy.
                                            $sz_checkout_make_url = function( $row, $force_motoboy = false ) {
                                                $raw_url = trim( (string) ( $row['affiliate_url'] ?? $row['url'] ?? '' ) );
                                                if ( $raw_url !== '' ) { return $raw_url; }
                                                $tok = trim( (string) ( $row['token'] ?? $row['slug'] ?? '' ) );
                                                if ( $tok === '' ) { return ''; }
                                                $is_mb = $force_motoboy || strtolower( (string) ( $row['tipo'] ?? '' ) ) === 'motoboy';
                                                $disp_cpf = false;
                                                if ( function_exists( 'get_current_user_id' ) ) {
                                                    $uid = get_current_user_id();
                                                    $disp_cpf = $uid && get_user_meta( $uid, '_sz_dispensar_cpf_motoboy', true ) === '1';
                                                }
                                                if ( $is_mb ) {
                                                    $template_key = $disp_cpf ? 'senderzz_checkout_template_id_sem_cpf' : 'senderzz_checkout_template_id';
                                                    $default_id   = $disp_cpf ? 1075 : (int) get_option( 'senderzz_checkout_template_id', 140 );
                                                    $template_id  = (int) get_option( $template_key, $default_id );
                                                    $base = $template_id ? get_permalink( $template_id ) : '';
                                                    if ( ! $base ) { $base = home_url( $disp_cpf ? '/checkouts/codsfpc/' : '/checkouts/cod/' ); }
                                                } else {
                                                    // Expedição deve sair sempre pela LP pública, não pelo template /checkouts/checkout/.
                                                    $base = home_url( '/checkouts/lp/' );
                                                }
                                                $args = [ 'sz' => $tok ];
                                                if ( $is_mb ) { $args['szm'] = '1'; }
                                                return add_query_arg( $args, $base );
                                            };
                                            $sz_checkout_norm_name = function( $row ) {
                                                return strtolower( remove_accents( preg_replace( '/\s+—\s+(Motoboy|Correio|Expedição)$/iu', '', (string) ( $row['display_name'] ?? $row['name'] ?? '' ) ) ) );
                                            };
                                        foreach ( $it['checkouts'] as $link ) :
                                            // Exibe apenas o link principal. O espelho Motoboy aparece como botão na mesma linha.
                                            if ( strtolower( (string) ( $link['tipo'] ?? '' ) ) === 'motoboy' ) { continue; }
                                            $sz_mb_row = null;
                                            if ( ! empty( $link['link_motoboy_id'] ) && isset( $wpdb ) ) {
                                                $sz_mb_row = $wpdb->get_row( $wpdb->prepare( "SELECT * FROM {$wpdb->prefix}senderzz_checkout_links WHERE id=%d AND user_id=%d LIMIT 1", (int) $link['link_motoboy_id'], (int) ( $user->id ?? 0 ) ), ARRAY_A );
                                            }
                                            $url = $sz_checkout_make_url( $link );
                                            $name = (string) ( $link['display_name'] ?? $link['name'] ?? 'Checkout' );
                                            $checkout_products_label = (string) ( $link['components_text'] ?? $it['name'] );
                                            $price = (string) ( $link['price_label'] ?? '' );
                                            if ( $price === '' && isset( $link['display_value'] ) && (float) $link['display_value'] > 0 ) $price = 'R$ ' . number_format( (float) $link['display_value'], 2, ',', '.' );
                                        ?>
                                            <?php
                                            $mb_url = '';
                                            $exp_url = '';
                                            $clean_row = $link;
                                            // Linhas antigas salvas como Motoboy podem carregar URL já forçada. Para o produtor,
                                            // reconstrói sempre os dois botões por slug/token, sem herdar URL contaminada.
                                            if ( ! $is_affiliate ) { unset( $clean_row['url'], $clean_row['affiliate_url'] ); }

                                            if ( $is_affiliate ) {
                                                // Afiliado recebe somente os links liberados para ele, com identificação própria.
                                                $exp_url = '';
                                                $mb_url  = $sz_checkout_make_url( $link, true ) ?: $url;
                                            } else {
                                                // Expedição é o link principal; Motoboy é o espelho vinculado na mesma linha.
                                                $exp_url = $sz_checkout_make_url( $clean_row, false ) ?: $url;
                                                if ( is_array( $sz_mb_row ) && ! empty( $sz_mb_row ) ) {
                                                    unset( $sz_mb_row['affiliate_url'] );
                                                    $mb_url = $sz_checkout_make_url( $sz_mb_row, true );
                                                } else {
                                                    $mb_url  = $sz_checkout_make_url( $clean_row, true ) ?: $url;
                                                }
                                            }
                                            ?>
                                            <div class="sz-prod-row-v135 <?php echo $is_affiliate ? 'is-affiliate-row' : ''; ?>" data-link-id="<?php echo (int) ( $link['id'] ?? 0 ); ?>"><span><strong><?php echo esc_html( $name ); ?></strong><small><?php echo esc_html( (string) ( $link['slug'] ?? $link['token'] ?? '' ) ); ?></small></span><span><?php echo esc_html( $checkout_products_label ?: $it['name'] ); ?></span><span><?php echo esc_html( $price ?: '—' ); ?></span><?php
                                            // Comissão exibida:
                                            // - link novo/editado: usa o valor salvo no próprio checkout (inclusive 0%).
                                            // - link antigo sem marca de atualização e com 0: usa a comissão padrão antiga do produtor, se houver.
                                            // - se não houver padrão: fica 0 e continua editável.
                                            $sz_saved_commission = isset( $link['affiliate_commission_pct'] ) ? (float) $link['affiliate_commission_pct'] : null;
                                            $sz_link_commission = $sz_saved_commission !== null ? $sz_saved_commission : 0.0;
                                            $sz_is_old_unset_commission = ( $sz_saved_commission === null || $sz_saved_commission <= 0 ) && empty( $link['updated_at'] );
                                            if ( $sz_is_old_unset_commission && function_exists( 'sz_aff_producer_default_commission_pct' ) ) {
                                                $sz_default_commission = (float) sz_aff_producer_default_commission_pct( (int) ( $link['producer_id'] ?? $user->id ?? 0 ) );
                                                if ( $sz_default_commission > 0 ) { $sz_link_commission = $sz_default_commission; }
                                            }
                                            ?><?php
                                            $sz_offer_value_num = 0.0;
                                            if ( isset( $link['display_value'] ) ) {
                                                $sz_offer_value_num = (float) $link['display_value'];
                                            } elseif ( isset( $link['value'] ) ) {
                                                $sz_offer_value_num = (float) $link['value'];
                                            } elseif ( isset( $link['price'] ) ) {
                                                $sz_offer_value_num = (float) $link['price'];
                                            }
                                            if ( $sz_offer_value_num <= 0 && $price !== '' ) {
                                                $sz_offer_value_num = (float) str_replace( ',', '.', preg_replace( '/[^0-9,\.]/', '', str_replace( '.', '', $price ) ) );
                                            }
                                            $sz_commission_amount = max( 0, $sz_offer_value_num * ( $sz_link_commission / 100 ) );
                                            $sz_commission_amount_label = $sz_commission_amount > 0 ? 'R$ ' . number_format( $sz_commission_amount, 2, ',', '.' ) : '—';
                                            ?><span class="sz-prod-commission-cell"><?php if ( ! $is_affiliate ) : ?><div class="sz-prod-commission-editor"><input class="sz-prod-commission-input" type="number" min="0" max="100" step="0.01" value="<?php echo esc_attr( number_format( $sz_link_commission, 2, '.', '' ) ); ?>"><span class="sz-prod-commission-suffix">%</span></div><button type="button" class="sz-prod-commission-save" onclick="szCLSaveCommission(this,<?php echo (int) ( $link['id'] ?? 0 ); ?>,window.SZ_NONCE||window.SZ_PORTAL_NONCE||'')">Salvar</button><?php else : ?><?php echo esc_html( number_format( $sz_link_commission, 2, ',', '.' ) . '%' ); ?><?php endif; ?></span><?php if ( $is_affiliate ) : ?><span class="sz-prod-commission-value"><?php echo esc_html( $sz_commission_amount_label ); ?></span><span class="actions sz-prod-checkout-actions"><?php if ( $mb_url ) : ?><button type="button" class="sz-prod-btn-copy sz-prod-btn-solid" onclick="szCopyText(this,<?php echo esc_js( wp_json_encode( $mb_url ) ); ?>)">🏍️ Motoboy</button><?php endif; ?></span><?php else : ?><span class="sz-prod-links-cell"><?php if ( $exp_url ) : ?><button type="button" class="sz-prod-btn-copy sz-prod-btn-solid" onclick="szCopyText(this,<?php echo esc_js( wp_json_encode( $exp_url ) ); ?>)">📦 Expedição</button><?php endif; ?><?php if ( $mb_url ) : ?><button type="button" class="sz-prod-btn-copy sz-prod-btn-solid" onclick="szCopyText(this,<?php echo esc_js( wp_json_encode( $mb_url ) ); ?>)">🏍️ Motoboy</button><?php endif; ?></span><span class="actions sz-prod-checkout-actions"><label class="sz-prod-aff-toggle" title="Liberar para afiliados"><input type="checkbox" <?php checked( ! empty( $link['affiliate_visible'] ) ); ?> onchange="szCLToggleAffiliate(<?php echo (int) ( $link['id'] ?? 0 ); ?>,this.checked,window.SZ_NONCE||window.SZ_PORTAL_NONCE||'')"><span></span><em>Afiliados</em></label><button type="button" class="sz-prod-btn-delete" onclick="szCLDelete(<?php echo (int) ( $link['id'] ?? 0 ); ?>,window.SZ_NONCE||window.SZ_PORTAL_NONCE||'',this)">Excluir</button></span><?php endif; ?></div>
                                        <?php endforeach; endif; ?>
                                    </div>
                                    <?php if ( ! $is_affiliate ) : ?>
                                    <div class="sz-prod-inline-create is-collapsed" id="sz-prod-create-<?php echo esc_attr( $it['id'] ); ?>" data-product-id="<?php echo esc_attr( $it['id'] ); ?>">
                                        <input type="hidden" class="sz-prod-cl-current-product" value="<?php echo esc_attr( $it['id'] ); ?>">
                                        <div class="sz-prod-inline-create-head">
                                            <div><strong>Novo checkout deste produto</strong><small>Crie na mesma tela, sem abrir a tela antiga de links.</small></div>
                                            <button type="button" class="sz-prod-inline-close" onclick="szProductCheckoutFormToggle('<?php echo esc_js( $it['id'] ); ?>',false)">Fechar</button>
                                        </div>
                                        <div class="sz-prod-inline-msg sz-alert" style="display:none"></div>
                                        <div class="sz-prod-inline-grid">
                                            <label><span>Nome do link</span><input class="sz-prod-cl-name" type="text" placeholder="Ex: Kit 3 Frascos R$197"></label>
                                            <label><span>Valor da oferta</span><input class="sz-prod-cl-valor" type="number" min="0" step="0.01" placeholder="197,00"></label>
                                            <label><span>Comissão do checkout (%)</span><input class="sz-prod-cl-commission" type="number" min="0" max="100" step="0.01" placeholder="Ex: 50"></label>
                                        </div>
                                        <div class="sz-prod-inline-components">
                                            <div class="sz-prod-inline-component sz-cl-item">
                                                <select class="sz-cl-product" onchange="szCLProductChanged(this)">
                                                    <option value="">Selecione o produto</option>
                                                    <?php foreach ( $sz_checkout_products as $p ) : ?>
                                                    <option value="<?php echo esc_attr( $p->get_id() ); ?>" data-type="<?php echo esc_attr( $p->get_type() ); ?>" <?php selected( (int) $p->get_id(), (int) $it['id'] ); ?>><?php echo esc_html( $p->get_name() ); ?></option>
                                                    <?php endforeach; ?>
                                                </select>
                                                <select class="sz-cl-variation" style="display:none"><option value="">Variação</option></select>
                                                <input class="sz-cl-qty" type="number" min="1" step="1" value="1" title="Quantidade">
                                                <button type="button" class="szlk-rm-btn" onclick="szProductCLRemoveItem(this)">×</button>
                                            </div>
                                        </div>
                                        <div class="sz-prod-inline-actions">
                                            <button type="button" class="sz-prod-inline-add" onclick="szProductCLAddItem('<?php echo esc_js( $it['id'] ); ?>')">+ Produto</button>
                                            <label class="sz-prod-inline-aff"><span>Disponibilizar para afiliados</span><input type="checkbox" class="sz-prod-cl-affiliate-visible"><i></i></label>
                                        </div>
                                        <button type="button" class="sz-prod-inline-generate" onclick="szProductCLGerar('<?php echo esc_js( $it['id'] ); ?>','<?php echo esc_js( $n ); ?>')">Gerar link</button>
                                    </div>
                                    <?php endif; ?>
                                </div>

                            </div>
                            <div class="sz-prod-side-v135">
                                <?php if ( ! $is_affiliate ) : ?><div class="sz-prod-card-v135 sz-prod-collapse sz-prod-section-v136" data-prod-section="envios" data-prod-collapse="ship-<?php echo esc_attr( $it['id'] ); ?>">
                                    <button type="button" class="sz-prod-collapse-head" onclick="szProdToggleCollapse(this)"><span>Envios de estoque</span><b>Ver envios</b></button>
                                    <div class="sz-prod-collapse-body sz-prod-movs-v135 sz-prod-envios-unified">
                                        <?php if ( empty( $it['shipments'] ) ) : ?><p>Nenhum envio de estoque encontrado para este produto.</p><?php else : foreach ( array_slice( $it['shipments'], 0, 30 ) as $sh ) : ?>
                                            <div><strong><?php echo esc_html( (string) $sh['order'] ); ?></strong><span><?php echo esc_html( (string) ( $sh['qty'] ?? 1 ) ); ?> un</span><small><?php echo esc_html( $sh['date'] ); ?> · <?php echo esc_html( $sh['status'] ); ?></small></div>
                                        <?php endforeach; endif; ?>
                                    </div>
                                </div>
                                <div class="sz-prod-card-v135 sz-prod-collapse sz-prod-section-v136" data-prod-section="movimentacoes" data-prod-collapse="mov-<?php echo esc_attr( $it['id'] ); ?>">
                                    <button type="button" class="sz-prod-collapse-head" onclick="szProdToggleCollapse(this)"><span>Movimentações de estoque</span><b>Ver movimentações</b></button>
                                    <div class="sz-prod-collapse-body sz-prod-movs-v135 sz-prod-movements-detailed">
                                        <?php if ( empty( $it['movements'] ) ) : ?><p>Nenhuma entrada ou saída encontrada para este produto.</p><?php else : foreach ( array_slice( $it['movements'], 0, 50 ) as $mv ) :
                                            $mv_type = (string) ( $mv['type'] ?? 'Movimentação' );
                                            $mv_qty_raw = (int) ( $mv['qty'] ?? 0 );
                                            $mv_direction = strtolower( (string) ( $mv['direction'] ?? '' ) );
                                            $mv_type_norm = strtolower( remove_accents( $mv_type ) );
                                            $mv_is_out = $mv_direction === 'out' || strpos( $mv_type_norm, 'saida' ) !== false || strpos( $mv_type_norm, 'reserva' ) !== false || strpos( $mv_type_norm, 'entregue' ) !== false;
                                            $mv_prefix = $mv_is_out ? '-' : '+';
                                        ?>
                                            <div class="sz-prod-movement-row"><strong><?php echo esc_html( $mv_type ); ?></strong><span><?php echo esc_html( $mv_prefix . abs( $mv_qty_raw ) ); ?> un</span><small><?php echo esc_html( (string) ( $mv['date'] ?? '—' ) ); ?> · <?php echo esc_html( (string) ( $mv['ref'] ?? '' ) ); ?><?php if ( ! empty( $mv['meta'] ) ) : ?> · <?php echo esc_html( (string) $mv['meta'] ); ?><?php endif; ?></small></div>
                                        <?php endforeach; endif; ?>
                                    </div>
                                </div>
                            <?php endif; ?></div>
                        </div>
                    </div>
                <?php $first = false; endforeach; ?>
            <?php endif; ?>
        </div>
        <?php
        // REFACTOR-v459: CSS e JS do painel de produtos externalizados.
        // CSS → assets/css/sz-portal-products-checkout.css
        // JS  → assets/js/sz-portal-products-checkout.js
        $sz_prod_css_ver = file_exists( TPC_PATH . 'assets/css/sz-portal-products-checkout.css' )
            ? md5_file( TPC_PATH . 'assets/css/sz-portal-products-checkout.css' ) : TPC_VERSION;
        $sz_prod_js_ver = file_exists( TPC_PATH . 'assets/js/sz-portal-products-checkout.js' )
            ? md5_file( TPC_PATH . 'assets/js/sz-portal-products-checkout.js' ) : TPC_VERSION;
        ?>
        <link rel="stylesheet" id="sz-portal-products-checkout-css"
              href="<?php echo esc_url( TPC_URL . 'assets/css/sz-portal-products-checkout.css' ); ?>?v=<?php echo esc_attr( $sz_prod_css_ver ); ?>"
              type="text/css">
        <script id="sz-product-checkout-inline-js"
                src="<?php echo esc_url( TPC_URL . 'assets/js/sz-portal-products-checkout.js' ); ?>?v=<?php echo esc_attr( $sz_prod_js_ver ); ?>"></script>
        <?php return (string) ob_get_clean(); // phpcs:ignore -- ob_start() chamado no início desta função
    }


private function order_row(): string { return ''; } // V1 removido — nunca chamado; V2 orders.php tem implementação própria

// ── LINKS ──────────────────────────────────────────────────────────────

private function render_links( object $user, string $n ): string {
    global $wpdb;

    // Afiliado usa a MESMA seção de Checkouts, porém sem criar/editar: mostra os links liberados
    // pelo produtor que convidou, já com identificação do afiliado.
    if ( function_exists( 'sz_aff_portal_user_must_use_affiliate_scope' ) && sz_aff_portal_user_must_use_affiliate_scope( $user ) && function_exists( 'sz_aff_render_affiliate_links' ) ) {
        return sz_aff_render_affiliate_links( $user, $n );
    }

    $class_ids_links = function_exists('sz_get_user_class_ids') ? sz_get_user_class_ids($user) : [];
    if ( empty($class_ids_links) ) $class_ids_links = array_filter([(int)($user->shipping_class_id??0)]);
    $class_id = !empty($class_ids_links) ? $class_ids_links[0] : 0; // primária para compatibilidade

    // Produtos simples da classe (não kits — esses são selecionados individualmente)
    $all_products = wc_get_products( [
        'limit'   => -1,
        'status'  => 'publish',
        'type'    => [ 'simple', 'variable' ],
        'orderby' => 'title',
        'order'   => 'ASC',
        'return'  => 'objects',
    ] );
    $products = [];
    foreach ( $all_products as $p ) {
        if ( ! $p instanceof \WC_Product ) continue;
        if ( ! in_array( (int) $p->get_shipping_class_id(), $class_ids_links, true ) ) continue;
        $hay = strtolower( remove_accents( $p->get_name() . ' ' . $p->get_sku() ) );
        // Exclui kits/combos da lista de seleção individual
        if (
            preg_match( '/\b(kit|combo|pacote|pack|bundle|conjunto)\b/i', $hay ) ||
            preg_match( '/\+/', $hay )
        ) continue;
        $products[] = $p;
    }

    // Links já criados por este usuário
    $links_table = $wpdb->prefix . 'senderzz_checkout_links';
    senderzz_portal_ensure_checkout_links_table();
    $existing_links = $wpdb->get_results( $wpdb->prepare(
        "SELECT * FROM {$links_table} WHERE user_id = %d ORDER BY created_at DESC LIMIT 50",
        $user->id
    ), ARRAY_A );

    // Resolve permissões pelo DONO do portal, não pelo WP user logado.
    // Quando o admin acessa a conta de um cliente, get_current_user_id() vira o admin
    // e isso escondia/mostrava Motoboy de forma errada na tela do cliente.
    $sz_links_owner_wp_uid = 0;
    if ( function_exists('sz_mb_get_wp_user_id_for_portal') ) {
        $sz_links_owner_wp_uid = (int) sz_mb_get_wp_user_id_for_portal( $user );
    }
    if ( ! $sz_links_owner_wp_uid && ! empty( $user->wp_user_id ) ) {
        $sz_links_owner_wp_uid = (int) $user->wp_user_id;
    }
    if ( ! $sz_links_owner_wp_uid && ! empty( $user->email ) ) {
        $sz_links_wp_user = get_user_by( 'email', sanitize_email( $user->email ) );
        if ( $sz_links_wp_user ) {
            $sz_links_owner_wp_uid = (int) $sz_links_wp_user->ID;
        }
    }

    // Autorrecuperação: se já existe link principal, mas o espelho Motoboy não foi criado
    // por falha antiga/silenciosa de insert, cria o par agora.
    $sz_owner_has_motoboy = ! function_exists('sz_producer_has_motoboy') || sz_producer_has_motoboy( $sz_links_owner_wp_uid );
    if ( $sz_owner_has_motoboy && ! empty( $existing_links ) ) {
        $sz_cols_links = $wpdb->get_col( "SHOW COLUMNS FROM `{$links_table}`", 0 );
        $sz_cols_links = is_array( $sz_cols_links ) ? $sz_cols_links : [];
        foreach ( $existing_links as $sz_main_link ) {
            $sz_tipo = (string) ( $sz_main_link['tipo'] ?? 'correio' );
            if ( $sz_tipo !== '' && $sz_tipo !== 'correio' ) { continue; }
            if ( ! empty( $sz_main_link['link_motoboy_id'] ) ) { continue; }

            $sz_token_mb = bin2hex( random_bytes( 16 ) );
            $sz_disp_cpf = $sz_links_owner_wp_uid && get_user_meta( $sz_links_owner_wp_uid, '_sz_dispensar_cpf_motoboy', true ) === '1';
            $sz_template_key = $sz_disp_cpf ? 'senderzz_checkout_template_id_sem_cpf' : 'senderzz_checkout_template_id';
            $sz_default_id   = $sz_disp_cpf ? 1075 : (int) get_option( 'senderzz_checkout_template_id', 140 );
            $sz_template_id  = (int) get_option( $sz_template_key, $sz_default_id );
            $sz_base_url     = $sz_template_id ? get_permalink( $sz_template_id ) : '';
            if ( ! $sz_base_url ) { continue; }
            $sz_url_mb       = add_query_arg( [ 'sz' => $sz_token_mb, 'szm' => '1' ], $sz_base_url );

            $sz_insert = [
                'user_id'         => (int) $user->id,
                'post_id'         => $sz_template_id,
                'name'            => ( $sz_disp_cpf ? 'COD - Senderzz' : ( preg_replace('/ — (Motoboy|Correio|Expedição)$/i', '', (string) ( $sz_main_link['name'] ?? '' ) ) . ' — Motoboy' ) ),
                'slug'            => $sz_token_mb,
                'url'             => $sz_url_mb,
                'tipo'            => 'motoboy',
                'components_text' => (string) ( $sz_main_link['components_text'] ?? '' ),
                'price_label'     => (string) ( $sz_main_link['price_label'] ?? '' ),
                'created_at'      => current_time( 'mysql' ),
            ];
            if ( in_array( 'token', $sz_cols_links, true ) ) { $sz_insert['token'] = $sz_token_mb; }
            if ( in_array( 'producer_id', $sz_cols_links, true ) ) { $sz_insert['producer_id'] = (int) ( $sz_main_link['producer_id'] ?? $user->id ); }
            if ( in_array( 'shipping_class_id', $sz_cols_links, true ) ) { $sz_insert['shipping_class_id'] = (int) ( $sz_main_link['shipping_class_id'] ?? 0 ); }
            if ( in_array( 'display_value', $sz_cols_links, true ) ) { $sz_insert['display_value'] = (float) ( $sz_main_link['display_value'] ?? 0 ); }
            if ( in_array( 'payload', $sz_cols_links, true ) ) { $sz_insert['payload'] = (string) ( $sz_main_link['payload'] ?? '' ); }
            if ( in_array( 'affiliate_visible', $sz_cols_links, true ) ) { $sz_insert['affiliate_visible'] = (int) ( $sz_main_link['affiliate_visible'] ?? 0 ); }

            $sz_ok = $wpdb->insert( $links_table, $sz_insert );
            if ( $sz_ok && $wpdb->insert_id ) {
                $wpdb->update( $links_table, [ 'link_motoboy_id' => (int) $wpdb->insert_id ], [ 'id' => (int) $sz_main_link['id'] ], [ '%d' ], [ '%d' ] );
            }
        }

        $existing_links = $wpdb->get_results( $wpdb->prepare(
            "SELECT * FROM {$links_table} WHERE user_id = %d ORDER BY created_at DESC LIMIT 50",
            $user->id
        ), ARRAY_A );
    }

    ob_start();
    ?>
    <style>
/* =========================================================
   Senderzz — Links de checkout responsivo premium
   Corrige coluna Ações cortada e grids quebrados.
========================================================= */
.szlk-card{background:var(--c1)!important;border:1px solid var(--bd)!important;border-radius:22px!important;overflow:hidden!important;margin:0 0 18px!important;box-shadow:0 18px 54px rgba(15,23,42,.06)!important;color:var(--tx)!important;min-width:0!important}.szlk-head{min-height:72px!important;display:flex!important;align-items:center!important;justify-content:space-between!important;gap:16px!important;padding:18px 22px!important;border-bottom:1px solid var(--bd)!important;background:linear-gradient(180deg,rgba(15,23,42,.035),rgba(15,23,42,0))!important}.szlk-head-left{display:flex!important;align-items:center!important;gap:12px!important;min-width:0!important}.szlk-icon{width:42px!important;height:42px!important;min-width:42px!important;border-radius:14px!important;background:rgba(249,115,22,.12)!important;display:flex!important;align-items:center!important;justify-content:center!important;color:var(--ac)!important}.szlk-icon svg{width:20px!important;height:20px!important;stroke:var(--ac)!important;stroke-width:2!important;fill:none!important;stroke-linecap:round!important;stroke-linejoin:round!important}.szlk-title{font-size:var(--sz-text-lg);font-weight:700;line-height:1.15;color:var(--tx)!important;letter-spacing:-.015em}.szlk-sub{font-size:var(--sz-text-meta);color:var(--tx2)!important;margin-top:3px!important;line-height:1.35}.szlk-body{padding:20px 22px 22px!important;background:var(--c1)!important;min-width:0!important;overflow-x:auto!important;overflow-y:visible!important}.szlk-grid2{display:grid!important;grid-template-columns:repeat(2,minmax(0,1fr))!important;gap:14px!important;margin-bottom:16px!important}.szlk-field{display:flex!important;flex-direction:column!important;gap:7px!important;min-width:0!important}.szlk-lbl{font-size:var(--sz-text-xs);font-weight:700;color:var(--tx3)!important;text-transform:none;letter-spacing:0;line-height:1}.szlk-input,.szlk-select{width:100%!important;height:46px!important;padding:0 14px!important;border:1.5px solid var(--bd)!important;border-radius:12px!important;background:var(--c1)!important;color:var(--tx)!important;font-size:var(--sz-text-base);font-weight:700;font-family:var(--sz-font);outline:none!important;box-shadow:none!important;min-width:0!important}.szlk-input:focus,.szlk-select:focus,.szlk-item-row select:focus,.szlk-item-row input[type=number]:focus{border-color:var(--ac)!important;box-shadow:0 0 0 4px rgba(249,115,22,.13)!important}.szlk-input::placeholder{color:var(--tx3)!important;opacity:1!important}.szlk-pfx-wrap{position:relative!important}.szlk-pfx{position:absolute!important;left:13px!important;top:50%!important;transform:translateY(-50%)!important;font-size:var(--sz-text-meta);color:var(--tx3)!important;pointer-events:none!important;font-weight:700}.szlk-pfx-wrap .szlk-input{padding-left:36px!important}.szlk-section-row{display:flex!important;align-items:center!important;justify-content:space-between!important;gap:12px!important;margin:8px 0 10px!important}.szlk-section-lbl{font-size:var(--sz-text-xs);font-weight:700;color:var(--tx3)!important;text-transform:none;letter-spacing:0}.szlk-add-btn{height:38px!important;display:inline-flex!important;align-items:center!important;justify-content:center!important;gap:7px!important;padding:0 14px!important;border-radius:12px!important;border:1.5px dashed rgba(249,115,22,.38)!important;background:rgba(249,115,22,.10)!important;color:var(--ac)!important;font-size:var(--sz-text-meta);font-weight:700;cursor:pointer!important;font-family:var(--sz-font);white-space:nowrap!important}.szlk-add-btn:hover{background:rgba(249,115,22,.16)!important;border-color:var(--ac)!important}
.szlk-item-row{display:grid!important;grid-template-columns:minmax(260px,1fr) minmax(170px,.55fr) 96px 42px!important;gap:10px!important;align-items:center!important;padding:14px!important;border:1px solid var(--bd)!important;border-radius:16px!important;background:var(--c2)!important;margin:0 0 10px!important;min-width:0!important;overflow:visible!important}.szlk-item-row select,.szlk-item-row input[type=number]{width:100%!important;height:42px!important;padding:0 12px!important;border:1.5px solid var(--bd)!important;border-radius:11px!important;background:var(--c1)!important;color:var(--tx)!important;font-size:var(--sz-text-base);font-weight:700;font-family:var(--sz-font);outline:none!important;min-width:0!important}.szlk-item-row .sz-cl-product{width:100%!important;min-width:0!important}.szlk-item-row .sz-cl-variation{min-width:0!important}.szlk-rm-btn{width:42px!important;height:42px!important;border-radius:12px!important;border:1px solid rgba(239,68,68,.28)!important;background:rgba(239,68,68,.10)!important;color:#ef4444!important;display:flex!important;align-items:center!important;justify-content:center!important;cursor:pointer!important;font-size:var(--sz-text-lg);font-weight:700;margin:0!important;flex-shrink:0!important}.szlk-gerar-btn{height:52px!important;width:100%!important;margin:8px 0 0!important;border-radius:14px!important;background:var(--ac)!important;color:#fff!important;font-size:var(--sz-text-md);font-weight:700;letter-spacing:-.015em;border:0!important;cursor:pointer!important;font-family:var(--sz-font);box-shadow:0 12px 28px rgba(249,115,22,.22)!important}.szlk-gerar-btn:hover{filter:brightness(.96)!important}
.szlk-list-hd,.szlk-row{display:grid!important;grid-template-columns:minmax(150px,1.15fr) minmax(190px,1.25fr) minmax(92px,.55fr) minmax(260px,1.6fr) minmax(86px,.55fr) minmax(260px,1fr)!important;gap:10px!important;align-items:center!important;min-width:1120px!important}.szlk-list-hd{padding:0 12px 10px!important;font-size:var(--sz-text-xs);font-weight:700;color:var(--tx3)!important;text-transform:none;letter-spacing:0}.szlk-row{padding:12px!important;border:1px solid var(--bd)!important;border-radius:14px!important;background:var(--c1)!important;margin-bottom:8px!important;transition:background .1s,border-color .1s!important;overflow:visible!important}.szlk-row:hover{background:var(--c2)!important;border-color:rgba(249,115,22,.25)!important}.szlk-cell{font-size:var(--sz-text-meta);color:var(--tx)!important;overflow:hidden!important;text-overflow:ellipsis!important;white-space:nowrap!important;min-width:0!important}.szlk-cell-name{font-weight:700}.szlk-cell-url{font-size:var(--sz-text-sm);color:var(--ac)!important;font-family:var(--sz-font);overflow-wrap:anywhere!important}.szlk-cell-muted{color:var(--tx2)!important;font-size:var(--sz-text-meta)}.szlk-badge-active{display:inline-flex!important;align-items:center!important;justify-content:center!important;padding:5px 9px!important;border-radius:999px!important;background:rgba(22,163,74,.12)!important;color:#16a34a!important;border:1px solid rgba(22,163,74,.22)!important;font-size:var(--sz-text-sm);font-weight:700;white-space:nowrap!important}.szlk-cell:last-child,.szlk-acts{overflow:visible!important}.szlk-acts{display:flex!important;align-items:center!important;justify-content:flex-end!important;gap:8px!important;flex-wrap:nowrap!important;min-width:250px!important}.szlk-btn{height:34px!important;min-width:70px!important;padding:0 12px!important;border-radius:10px!important;font-size:var(--sz-text-meta);font-weight:700;cursor:pointer!important;font-family:var(--sz-font);border:1.5px solid!important;white-space:nowrap!important;display:inline-flex!important;align-items:center!important;justify-content:center!important;text-decoration:none!important;line-height:1}.szlk-btn-open{color:#fff!important;border-color:#E8650A!important;background:linear-gradient(135deg,#f97316,#E8650A)!important;box-shadow:0 8px 20px rgba(232,101,10,.30)!important}.szlk-btn-del{color:#fff!important;border-color:#1e293b!important;background:#1e293b!important}
.szlk-btn-open:hover{filter:brightness(1.08)!important;box-shadow:0 12px 26px rgba(232,101,10,.40)!important;transform:translateY(-1px)!important}.szlk-btn-del:hover{background:#0f172a!important;border-color:#0f172a!important;transform:translateY(-1px)!important}
.sz-root.sz-dark .szlk-btn-open{background:linear-gradient(135deg,#f97316,#E8650A)!important;border-color:rgba(249,115,22,.55)!important;color:#fff!important}.sz-root.sz-dark .szlk-btn-del{background:#334155!important;border-color:#475569!important;color:#f1f5f9!important}.sz-root.sz-dark .szlk-btn-del:hover{background:#1e293b!important;border-color:#334155!important}
.szlk-cell-url-copy:hover{opacity:.75!important;text-decoration:underline!important}
.szlk-acts{display:grid!important;grid-template-columns:1fr 1fr!important;gap:6px!important;width:100%!important;min-width:0!important}
.sz-root.sz-dark .szlk-card{background:#111827!important;border-color:rgba(255,255,255,.10)!important;box-shadow:0 22px 60px rgba(0,0,0,.26)!important;color:#e5e7eb!important}.sz-root.sz-dark .szlk-head{background:rgba(15,23,42,.72)!important;border-color:rgba(255,255,255,.10)!important}.sz-root.sz-dark .szlk-body{background:#111827!important}.sz-root.sz-dark .szlk-title,.sz-root.sz-dark .szlk-cell{color:#f8fafc!important}.sz-root.sz-dark .szlk-sub,.sz-root.sz-dark .szlk-lbl,.sz-root.sz-dark .szlk-section-lbl,.sz-root.sz-dark .szlk-cell-muted{color:#94a3b8!important}.sz-root.sz-dark .szlk-input,.sz-root.sz-dark .szlk-select,.sz-root.sz-dark .szlk-item-row select,.sz-root.sz-dark .szlk-item-row input[type=number]{background:#020617!important;color:#f8fafc!important;border-color:rgba(255,255,255,.14)!important}.sz-root.sz-dark .szlk-item-row,.sz-root.sz-dark .szlk-row{background:#0f172a!important;border-color:rgba(255,255,255,.10)!important}.sz-root.sz-dark .szlk-row:hover{background:#172033!important;border-color:rgba(249,115,22,.32)!important}.sz-root.sz-dark .szlk-cell-url{color:#fb923c!important}.sz-root.sz-dark .szlk-icon{background:rgba(249,115,22,.22)!important;color:#fb923c!important}
/* v260: sz-checkout-cpf-notice removido */

@media(max-width:1180px){.szlk-list-hd{display:none!important}.szlk-row{min-width:0!important;grid-template-columns:1fr!important;gap:9px!important;padding:15px!important}.szlk-cell{display:flex!important;justify-content:space-between!important;gap:14px!important;white-space:normal!important;text-align:right!important}.szlk-cell:nth-child(1)::before{content:"Nome"}.szlk-cell:nth-child(2)::before{content:"Produto(s)"}.szlk-cell:nth-child(3)::before{content:"Valor"}.szlk-cell:nth-child(4)::before{content:"URL"}.szlk-cell:nth-child(5)::before{content:"Status"}.szlk-cell:nth-child(6)::before{content:"Ações"}.szlk-cell::before{color:var(--tx3)!important;font-size:var(--sz-text-xs);font-weight:700;text-transform:none;letter-spacing:0;text-align:left!important;flex:0 0 auto!important}.szlk-cell-url{max-width:none!important}.szlk-acts{justify-content:flex-end!important;min-width:0!important;flex-wrap:wrap!important}.szlk-body{overflow-x:visible!important}}
@media(max-width:760px){.szlk-card{border-radius:18px!important;margin-bottom:14px!important}.szlk-head{align-items:flex-start!important;flex-direction:column!important;padding:16px!important}.szlk-body{padding:16px!important}.szlk-grid2,.szlk-item-row{grid-template-columns:1fr!important}.szlk-section-row{align-items:flex-start!important;flex-direction:column!important}.szlk-add-btn,.szlk-gerar-btn,.szlk-rm-btn{width:100%!important}.szlk-cell{display:block!important;text-align:left!important}.szlk-cell::before{display:block!important;margin-bottom:4px!important}.szlk-acts{width:100%!important;justify-content:stretch!important}.szlk-btn{flex:1 1 0!important;min-width:0!important}}


/* =========================================================
   SENDERZZ FINAL OVERRIDE — Webhook badge + checkout actions
========================================================= */
.sz-webhook-hero .sz-webhook-pill,
#sec-webhooks .sz-webhook-pill,
.sz-webhook-pill{
    height:42px!important;min-height:42px!important;padding:0 18px!important;border-radius:999px!important;
    display:inline-flex!important;align-items:center!important;justify-content:center!important;gap:8px!important;
    background:rgba(255,255,255,.94)!important;border:1px solid rgba(255,255,255,.72)!important;color:#111827!important;
    box-shadow:0 14px 34px rgba(15,23,42,.18), inset 0 1px 0 rgba(255,255,255,.92)!important;
    backdrop-filter:blur(16px)!important;-webkit-backdrop-filter:blur(16px)!important;
    font-size:var(--sz-text-meta);font-weight:700;letter-spacing:-.015em;text-shadow:none;white-space:nowrap!important;
}
.sz-webhook-hero .sz-webhook-pill::before,#sec-webhooks .sz-webhook-pill::before,.sz-webhook-pill::before{
    content:""!important;width:8px!important;height:8px!important;border-radius:999px!important;background:#16a34a!important;box-shadow:0 0 0 4px rgba(22,163,74,.13)!important;flex:0 0 auto!important;
}
.sz-root.sz-dark .sz-webhook-hero .sz-webhook-pill,.sz-root.sz-dark #sec-webhooks .sz-webhook-pill,.sz-root.sz-dark .sz-webhook-pill{
    background:rgba(248,250,252,.95)!important;border-color:rgba(255,255,255,.70)!important;color:#0f172a!important;box-shadow:0 16px 38px rgba(0,0,0,.32), inset 0 1px 0 rgba(255,255,255,.92)!important;
}
/* ── Links table — estrutura própria independente do szlk ── */
.sz-lk-hd,.sz-lk-row{display:flex;align-items:center;gap:12px;padding:10px 14px;min-width:0;}
.sz-lk-hd{font-size:var(--sz-text-xs);font-weight:700;color:var(--tx3);text-transform:none;letter-spacing:0;padding-bottom:8px;}
.sz-lk-row{border:1px solid var(--bd);border-radius:12px;background:var(--c1);margin-bottom:8px;transition:background .1s,border-color .1s;}
.sz-lk-row:hover{background:var(--c2);border-color:rgba(249,115,22,.25);}
.sz-root.sz-dark .sz-lk-row{background:#0f172a;border-color:rgba(255,255,255,.08);}
.sz-root.sz-dark .sz-lk-row:hover{background:#172033;border-color:rgba(249,115,22,.32);}
.sz-lk-col-name{flex:0 0 150px;font-weight:700;font-size:var(--sz-text-base);color:var(--tx);overflow:hidden;text-overflow:ellipsis;white-space:nowrap;}
.sz-lk-col-prod{flex:1 1 0;min-width:0;font-size:var(--sz-text-meta);color:var(--tx2);overflow:hidden;text-overflow:ellipsis;white-space:nowrap;}
.sz-lk-col-val{flex:0 0 100px;font-size:var(--sz-text-base);font-weight:700;color:var(--tx);}
.sz-lk-col-links{flex:0 0 220px;display:flex;align-items:center;justify-content:center;gap:8px;}
.sz-lk-col-del{flex:0 0 90px;display:flex;align-items:center;justify-content:flex-end;}
.sz-lk-hd .sz-lk-col-val{font-weight:700;font-size:var(--sz-text-xs);color:var(--tx3);}
.sz-lk-hd .sz-lk-col-links{font-weight:700;font-size:var(--sz-text-xs);color:var(--tx3);}
.sz-lk-cell-muted{color:var(--tx2)!important;}
.sz-lk-cell-name{color:var(--tx)!important;}
#sec-links .szlk-row .szlk-cell:last-child{width:100%!important;min-width:0!important;overflow:visible!important;justify-self:stretch!important;}
#sec-links .szlk-acts{width:100%!important;min-width:0!important;display:grid!important;grid-template-columns:1fr 1fr 1fr!important;gap:8px!important;align-items:center!important;justify-content:stretch!important;padding:0!important;margin:0!important;}
#sec-links .szlk-btn{width:100%!important;min-width:0!important;height:38px!important;min-height:38px!important;padding:0 10px!important;border-radius:12px!important;font-size:var(--sz-text-meta);font-weight:700;letter-spacing:-.015em;line-height:1;display:inline-flex!important;align-items:center!important;justify-content:center!important;border-width:1.5px!important;box-shadow:none!important;transition:all .14s ease!important;}
#sec-links .szlk-btn-open{background:linear-gradient(135deg,#f97316,#E8650A)!important;border-color:rgba(249,115,22,.55)!important;color:#fff!important;box-shadow:0 6px 18px rgba(232,101,10,.28)!important;}
#sec-links .szlk-btn-copy{background:#fff!important;color:#334155!important;border:1px solid rgba(148,163,184,.28)!important;box-shadow:0 4px 12px rgba(15,23,42,.035)!important;}
#sec-links .szlk-btn-del{background:#1e293b!important;border-color:#1e293b!important;color:#fff!important;}
#sec-links .szlk-btn:hover{filter:none!important;transform:translateY(-1px)!important;}
#sec-links .szlk-btn-open:hover{filter:brightness(1.08)!important;box-shadow:0 10px 24px rgba(232,101,10,.40)!important;}
#sec-links .szlk-btn-copy:hover{background:#f1f5f9!important;border-color:rgba(148,163,184,.45)!important;}
#sec-links .szlk-btn-del:hover{background:#0f172a!important;border-color:#0f172a!important;}
#sec-links .szlk-btn:focus,#sec-links .szlk-btn:focus-visible{outline:none!important;box-shadow:none!important;}
#sec-links .szlk-cell-url-copy:hover{opacity:.7!important;text-decoration:underline!important;cursor:pointer!important;}
#sec-links .szlk-row{overflow:visible!important;}#sec-links .szlk-body{overflow-x:auto!important;overflow-y:visible!important;}
.sz-root.sz-dark #sec-links .szlk-btn-open{background:linear-gradient(135deg,#f97316,#E8650A)!important;border-color:rgba(249,115,22,.55)!important;color:#fff!important;box-shadow:0 6px 18px rgba(232,101,10,.28)!important;}
.sz-root.sz-dark #sec-links .szlk-btn-copy{background:#0f172a!important;color:#cbd5e1!important;border-color:rgba(148,163,184,.22)!important;box-shadow:none!important;}
.sz-root.sz-dark #sec-links .szlk-btn-copy:hover{background:#1e293b!important;color:#f1f5f9!important;}
.sz-root.sz-dark #sec-links .szlk-btn-del{background:#334155!important;border-color:#475569!important;color:#f1f5f9!important;}
.sz-root.sz-dark #sec-links .szlk-btn-del:hover{background:#1e293b!important;border-color:#334155!important;}
#sec-links .szlk-toggle-form{background:linear-gradient(135deg,#f97316,#E8650A)!important;color:#fff!important;border:1px solid rgba(249,115,22,.55)!important;box-shadow:0 8px 22px rgba(232,101,10,.26)!important;align-self:center!important;flex-shrink:0!important;}
#sec-links .szlk-create-card.is-collapsed .szlk-head{border-bottom:0!important;padding-top:0!important;padding-bottom:0!important;min-height:72px!important;display:flex!important;align-items:center!important;}
#sec-links .szlk-create-card.is-collapsed{padding:0!important;}
#sec-links .szlk-toggle-form:hover{filter:brightness(1.08)!important;transform:none!important;}
#sec-links .szlk-toggle-form:focus,#sec-links .szlk-toggle-form:focus-visible{outline:none!important;box-shadow:none!important;}
@media(max-width:1180px){#sec-links .szlk-row{grid-template-columns:1fr!important;}#sec-links .szlk-row .szlk-cell:last-child{display:block!important;}#sec-links .szlk-acts{grid-template-columns:repeat(3,minmax(0,1fr))!important;width:100%!important;}}
@media(max-width:560px){#sec-links .szlk-acts{grid-template-columns:1fr!important;}}


/* ===== Senderzz patch v72.3 — badges sólidas + hover laranja unificado ===== */
#sec-links .szlk-cell-url,
#sec-links .szlk-url-text{
    font-weight:700;
}
#sec-links .szlk-btn-copy-inline,
#sec-webhooks .sz-wh-copy{
    background:rgba(255,255,255,.03)!important;
    color:var(--tx)!important;
    border:1px solid rgba(148,163,184,.24)!important;
    box-shadow:none!important;
    font-weight:700;
}
.sz-root.sz-dark #sec-links .szlk-btn-copy-inline,
.sz-root.sz-dark #sec-webhooks .sz-wh-copy{
    background:rgba(255,255,255,.04)!important;
    color:#e5e7eb!important;
    border-color:rgba(148,163,184,.22)!important;
}
#sec-links .szlk-btn,
#sec-links .szlk-toggle-form,
#sec-webhooks .sz-mini,
#sec-webhooks .sz-btn-ghost,
#sec-webhooks .sz-primary,
#sec-stock .szst-open-btn,
#sec-freight .sz-freight-toggle-btn,
.sz-root .sz-quick,
.sz-root .sz-primary,
.sz-root .sz-btn-ghost,
.sz-root .sz-mini,
.sz-root .sz-act,
.sz-root button{
    outline:none!important;
    -webkit-tap-highlight-color:transparent!important;
}
#sec-links .szlk-btn:focus,
#sec-links .szlk-btn:focus-visible,
#sec-links .szlk-toggle-form:focus,
#sec-links .szlk-toggle-form:focus-visible,
#sec-webhooks .sz-mini:focus,
#sec-webhooks .sz-mini:focus-visible,
#sec-webhooks .sz-btn-ghost:focus,
#sec-webhooks .sz-btn-ghost:focus-visible,
#sec-stock .szst-open-btn:focus,
#sec-stock .szst-open-btn:focus-visible,
#sec-freight .sz-freight-toggle-btn:focus,
#sec-freight .sz-freight-toggle-btn:focus-visible,
.sz-root .sz-quick:focus,
.sz-root .sz-quick:focus-visible,
.sz-root .sz-primary:focus,
.sz-root .sz-primary:focus-visible,
.sz-root .sz-btn-ghost:focus,
.sz-root .sz-btn-ghost:focus-visible,
.sz-root .sz-mini:focus,
.sz-root .sz-mini:focus-visible,
.sz-root .sz-act:focus,
.sz-root .sz-act:focus-visible,
.sz-root button:focus,
.sz-root button:focus-visible{
    outline:none!important;
    box-shadow:none!important;
}
#sec-links .szlk-btn:hover,
#sec-links .szlk-btn:active,
#sec-links .szlk-toggle-form:hover,
#sec-links .szlk-toggle-form:active,
#sec-webhooks .sz-mini:hover,
#sec-webhooks .sz-mini:active,
#sec-webhooks .sz-btn-ghost:hover,
#sec-webhooks .sz-btn-ghost:active,
#sec-webhooks .sz-primary:hover,
#sec-webhooks .sz-primary:active,
#sec-stock .szst-open-btn:hover,
#sec-stock .szst-open-btn:active,
#sec-freight .sz-freight-toggle-btn:hover,
#sec-freight .sz-freight-toggle-btn:active,
.sz-root .sz-quick:hover,
.sz-root .sz-quick:active,
.sz-root .sz-primary:hover,
.sz-root .sz-primary:active,
.sz-root .sz-btn-ghost:hover,
.sz-root .sz-btn-ghost:active,
.sz-root .sz-mini:hover,
.sz-root .sz-mini:active,
.sz-root .sz-act:hover,
.sz-root .sz-act:active{
    transform:none!important;
    filter:none!important;
    border-color:rgba(249,115,22,.62)!important;
    box-shadow:0 0 0 1px rgba(249,115,22,.18),0 8px 18px rgba(232,101,10,.18)!important;
}
#sec-links .szlk-btn-copy-inline:hover,
#sec-links .szlk-btn-copy-inline:active,
#sec-webhooks .sz-wh-copy:hover,
#sec-webhooks .sz-wh-copy:active{
    background:rgba(249,115,22,.08)!important;
    color:var(--tx)!important;
}
.sz-root.sz-dark #sec-links .szlk-btn-copy-inline:hover,
.sz-root.sz-dark #sec-links .szlk-btn-copy-inline:active,
.sz-root.sz-dark #sec-webhooks .sz-wh-copy:hover,
.sz-root.sz-dark #sec-webhooks .sz-wh-copy:active{
    background:rgba(249,115,22,.14)!important;
    color:#fff!important;
}


/* ===== Senderzz patch v72.4 — re-alinhamento, emojis e botão azul removido ===== */
#sec-webhooks .sz-wh-url-col,
#sec-webhooks .sz-wh-actions-col,
#sec-webhooks .sz-wh-class-col,
#sec-webhooks .sz-tbl td{vertical-align:middle!important;}
#sec-webhooks .sz-wh-url-line{display:flex!important;align-items:center!important;justify-content:space-between!important;gap:12px!important;width:100%!important;min-width:0!important;}
#sec-webhooks .sz-wh-url-line code{font-weight:700;flex:1 1 auto!important;min-width:0!important;}
#sec-webhooks .sz-wh-copy,#sec-links .szlk-btn-copy-inline{background:rgba(255,255,255,.03)!important;color:inherit!important;border:1px solid rgba(148,163,184,.24)!important;box-shadow:none!important;}
#sec-links .szlk-btn-open,a.szlk-btn.szlk-btn-open,#sec-links .szlk-btn-open:visited,.sz-root.sz-dark #sec-links .szlk-btn-open,.sz-root.sz-dark a.szlk-btn.szlk-btn-open{background:linear-gradient(135deg,#f97316,#ea580c)!important;color:#fff!important;border:1px solid rgba(249,115,22,.58)!important;box-shadow:0 8px 20px rgba(232,101,10,.22)!important;}
#sec-links .szlk-btn-open:hover,#sec-links .szlk-btn-open:active,#sec-links .szlk-btn-open:focus,#sec-links .szlk-btn-open:focus-visible,a.szlk-btn.szlk-btn-open:hover,a.szlk-btn.szlk-btn-open:active,a.szlk-btn.szlk-btn-open:focus,a.szlk-btn.szlk-btn-open:focus-visible{background:linear-gradient(135deg,#fb7b1d,#f97316)!important;color:#fff!important;border-color:rgba(249,115,22,.72)!important;box-shadow:0 0 0 1px rgba(249,115,22,.18),0 8px 18px rgba(232,101,10,.18)!important;outline:none!important;}
#sec-freight .sz-log-icon,#sec-freight .sz-freight-ico,.sz-root.sz-dark #sec-freight .sz-log-icon,.sz-root.sz-dark #sec-freight .sz-freight-ico{background:rgba(249,115,22,.12)!important;color:#f97316!important;}
#sec-freight .sz-log-icon svg,#sec-freight .sz-freight-ico svg,.sz-root.sz-dark #sec-freight .sz-log-icon svg,.sz-root.sz-dark #sec-freight .sz-freight-ico svg{stroke:currentColor!important;color:currentColor!important;fill:none!important;}

.sz-root .szlk-btn,.sz-root .szlk-toggle-form,.sz-root .sz-mini,.sz-root .sz-btn-ghost,.sz-root .sz-primary,.sz-root .sz-quick,.sz-root .sz-act,.sz-root .szst-open-btn,.sz-root .sz-freight-toggle-btn,.sz-root button,.sz-root a.szlk-btn{outline:none!important;box-shadow:none!important;-webkit-appearance:none!important;appearance:none!important;-webkit-tap-highlight-color:transparent!important;}
.sz-root .szlk-btn:focus,.sz-root .szlk-btn:focus-visible,.sz-root .szlk-toggle-form:focus,.sz-root .szlk-toggle-form:focus-visible,.sz-root .sz-mini:focus,.sz-root .sz-mini:focus-visible,.sz-root .sz-btn-ghost:focus,.sz-root .sz-btn-ghost:focus-visible,.sz-root .sz-primary:focus,.sz-root .sz-primary:focus-visible,.sz-root .sz-quick:focus,.sz-root .sz-quick:focus-visible,.sz-root .sz-act:focus,.sz-root .sz-act:focus-visible,.sz-root .szst-open-btn:focus,.sz-root .szst-open-btn:focus-visible,.sz-root .sz-freight-toggle-btn:focus,.sz-root .sz-freight-toggle-btn:focus-visible,.sz-root button:focus,.sz-root button:focus-visible,.sz-root a.szlk-btn:focus,.sz-root a.szlk-btn:focus-visible,.sz-root .sz-copied{outline:none!important;box-shadow:none!important;}


/* =========================================================
   SAFE REFACTOR 02 — Checkouts actions + Reports/Webhooks/Users/Support
   Escopo controlado por página: não mexe em Pedidos/Estoque/Logística/Carteira.
========================================================= */
.sz-root{
  --sz-ref-btn-h:40px;
  --sz-ref-btn-r:12px;
  --sz-ref-orange-1:#ff7a1a;
  --sz-ref-orange-2:#ea580c;
  --sz-ref-hover-shadow:0 16px 34px rgba(232,101,10,.34);
  --sz-ref-normal-shadow:0 10px 24px rgba(232,101,10,.22);
}

/* Checkouts — todos os botões da célula Ações no mesmo padrão laranja. */
html body .sz-root #sec-links .szlk-acts,
html body .sz-root #sec-links .szlk-actions{
  display:grid!important;
  grid-template-columns:repeat(3,112px)!important;
  justify-content:center!important;
  align-items:center!important;
  gap:8px!important;
  width:auto!important;
  min-width:352px!important;
}
html body .sz-root #sec-links .szlk-acts .szlk-btn,
html body .sz-root #sec-links .szlk-actions .szlk-btn,
html body .sz-root #sec-links .szlk-acts > a,
html body .sz-root #sec-links .szlk-acts > button,
html body .sz-root #sec-links .szlk-actions > a,
html body .sz-root #sec-links .szlk-actions > button,
html body .sz-root #sec-links a.szlk-btn.szlk-btn-open,
html body .sz-root #sec-links .szlk-btn-open,
html body .sz-root #sec-links .szlk-btn-copy,
html body .sz-root #sec-links .szlk-btn-del{
  width:112px!important;
  min-width:112px!important;
  max-width:112px!important;
  height:var(--sz-ref-btn-h)!important;
  min-height:var(--sz-ref-btn-h)!important;
  padding:0 14px!important;
  border-radius:var(--sz-ref-btn-r)!important;
  border:1px solid rgba(249,115,22,.56)!important;
  background:linear-gradient(135deg,var(--sz-ref-orange-1),var(--sz-ref-orange-2))!important;
  color:#fff!important;
  display:inline-flex!important;
  align-items:center!important;
  justify-content:center!important;
  gap:6px!important;
  font-size:var(--sz-text-meta);
  font-weight:700;
  line-height:1;
  white-space:nowrap!important;
  text-decoration:none!important;
  box-shadow:var(--sz-ref-normal-shadow)!important;
  transform:none!important;
  filter:none!important;
  outline:none!important;
  transition:transform .16s ease, box-shadow .16s ease, filter .16s ease, background .16s ease!important;
}
html body .sz-root #sec-links .szlk-acts .szlk-btn:hover,
html body .sz-root #sec-links .szlk-actions .szlk-btn:hover,
html body .sz-root #sec-links .szlk-acts > a:hover,
html body .sz-root #sec-links .szlk-acts > button:hover,
html body .sz-root #sec-links .szlk-actions > a:hover,
html body .sz-root #sec-links .szlk-actions > button:hover,
html body .sz-root #sec-links .szlk-btn-open:hover,
html body .sz-root #sec-links .szlk-btn-copy:hover,
html body .sz-root #sec-links .szlk-btn-del:hover{
  background:linear-gradient(135deg,#ff8a2a,#f97316)!important;
  color:#fff!important;
  filter:brightness(1.04)!important;
  transform:translateY(-1px)!important;
  box-shadow:var(--sz-ref-hover-shadow)!important;
}
html body .sz-root #sec-links .szlk-btn:focus,
html body .sz-root #sec-links .szlk-btn:focus-visible,
html body .sz-root #sec-links .szlk-btn:active{
  background:linear-gradient(135deg,var(--sz-ref-orange-1),var(--sz-ref-orange-2))!important;
  color:#fff!important;
  box-shadow:var(--sz-ref-normal-shadow)!important;
  outline:none!important;
}

/* Refactor de botões somente nas páginas liberadas: Relatórios, Webhooks, Usuários e Suporte/Conta. */
html body .sz-root:is(#sz-root,#sz-portal-root) #sec-reports .sz-btn-ghost,
html body .sz-root:is(#sz-root,#sz-portal-root) #sec-reports .sz-primary,
html body .sz-root:is(#sz-root,#sz-portal-root) #sec-reports .sz-quick,
html body .sz-root:is(#sz-root,#sz-portal-root) #sec-reports button:not(.sz-ni),
html body .sz-root:is(#sz-root,#sz-portal-root) #sec-webhooks .sz-btn-ghost,
html body .sz-root:is(#sz-root,#sz-portal-root) #sec-webhooks .sz-primary,
html body .sz-root:is(#sz-root,#sz-portal-root) #sec-webhooks .sz-quick,
html body .sz-root:is(#sz-root,#sz-portal-root) #sec-webhooks .sz-wh-action-btn,
html body .sz-root:is(#sz-root,#sz-portal-root) #sec-webhooks .sz-mini,
html body .sz-root:is(#sz-root,#sz-portal-root) #sec-webhooks button:not(.sz-ni),
html body .sz-root:is(#sz-root,#sz-portal-root) #sec-users .sz-btn-ghost,
html body .sz-root:is(#sz-root,#sz-portal-root) #sec-users .sz-primary,
html body .sz-root:is(#sz-root,#sz-portal-root) #sec-users .sz-quick,
html body .sz-root:is(#sz-root,#sz-portal-root) #sec-users .sz-mini,
html body .sz-root:is(#sz-root,#sz-portal-root) #sec-users button:not(.sz-ni),
html body .sz-root:is(#sz-root,#sz-portal-root) #sec-settings .sz-btn-ghost,
html body .sz-root:is(#sz-root,#sz-portal-root) #sec-settings .sz-primary,
html body .sz-root:is(#sz-root,#sz-portal-root) #sec-settings .sz-quick,
html body .sz-root:is(#sz-root,#sz-portal-root) #sec-settings .sz-mini,
html body .sz-root:is(#sz-root,#sz-portal-root) #sec-settings button:not(.sz-ni){
  min-height:var(--sz-ref-btn-h)!important;
  height:var(--sz-ref-btn-h)!important;
  padding:0 16px!important;
  border-radius:var(--sz-ref-btn-r)!important;
  border:1px solid rgba(249,115,22,.56)!important;
  background:linear-gradient(135deg,var(--sz-ref-orange-1),var(--sz-ref-orange-2))!important;
  color:#fff!important;
  display:inline-flex!important;
  align-items:center!important;
  justify-content:center!important;
  gap:8px!important;
  font-size:var(--sz-text-meta);
  font-weight:700;
  line-height:1;
  white-space:nowrap!important;
  text-decoration:none!important;
  box-shadow:var(--sz-ref-normal-shadow)!important;
  transform:none!important;
  filter:none!important;
  outline:none!important;
  transition:transform .16s ease, box-shadow .16s ease, filter .16s ease, background .16s ease!important;
}
html body .sz-root #sec-reports button:not(.sz-ni):hover,
html body .sz-root #sec-webhooks button:not(.sz-ni):hover,
html body .sz-root #sec-users button:not(.sz-ni):hover,
html body .sz-root #sec-settings button:not(.sz-ni):hover,
html body .sz-root #sec-reports .sz-btn-ghost:hover,
html body .sz-root #sec-webhooks .sz-btn-ghost:hover,
html body .sz-root #sec-users .sz-btn-ghost:hover,
html body .sz-root #sec-settings .sz-btn-ghost:hover,
html body .sz-root #sec-webhooks .sz-wh-action-btn:hover,
html body .sz-root #sec-webhooks .sz-mini:hover,
html body .sz-root #sec-users .sz-mini:hover,
html body .sz-root #sec-settings .sz-mini:hover{
  background:linear-gradient(135deg,#ff8a2a,#f97316)!important;
  color:#fff!important;
  filter:brightness(1.04)!important;
  transform:translateY(-1px)!important;
  box-shadow:var(--sz-ref-hover-shadow)!important;
}

/* Cards, tabelas e formulários dessas páginas ficam só normalizados, sem trocar identidade. */
html body .sz-root #sec-reports .sz-card,
html body .sz-root #sec-webhooks .sz-card,
html body .sz-root #sec-users .sz-card,
html body .sz-root #sec-settings .sz-card,
html body .sz-root #sec-webhooks .sz-webhook-form,
html body .sz-root #sec-webhooks .sz-webhook-payload{
  border-radius:22px!important;
  border:1px solid var(--bd,rgba(15,23,42,.10))!important;
  box-shadow:0 18px 54px rgba(15,23,42,.06)!important;
  background:var(--c1,#fff)!important;
}
html body .sz-root #sec-reports .sz-fi,
html body .sz-root #sec-reports .sz-fs,
html body .sz-root #sec-webhooks .sz-field-input,
html body .sz-root #sec-users input,
html body .sz-root #sec-users select,
html body .sz-root #sec-settings input,
html body .sz-root #sec-settings select{
  min-height:42px!important;
  border-radius:12px!important;
  border:1.5px solid var(--bd,rgba(15,23,42,.10))!important;
  background:var(--c1,#fff)!important;
  color:var(--tx,#111827)!important;
  box-shadow:none!important;
}
html body .sz-root #sec-reports .sz-tbl,
html body .sz-root #sec-webhooks .sz-tbl,
html body .sz-root #sec-users .sz-tbl{
  border-collapse:separate!important;
  border-spacing:0 10px!important;
}
html body .sz-root #sec-reports .sz-tbl th,
html body .sz-root #sec-webhooks .sz-tbl th,
html body .sz-root #sec-users .sz-tbl th{
  background:transparent!important;
  color:var(--tx3,#94a3b8)!important;
  text-transform:none;
  letter-spacing:0;
  font-size:var(--sz-text-sm);
  font-weight:700;
  vertical-align:middle!important;
}
html body .sz-root #sec-reports .sz-tbl td,
html body .sz-root #sec-webhooks .sz-tbl td,
html body .sz-root #sec-users .sz-tbl td{
  vertical-align:middle!important;
}

/* Dark mode para as páginas refatoradas. */
html body .sz-root.sz-dark #sec-reports .sz-card,
html body .sz-root.sz-dark #sec-webhooks .sz-card,
html body .sz-root.sz-dark #sec-users .sz-card,
html body .sz-root.sz-dark #sec-settings .sz-card,
html body .sz-root.sz-dark #sec-webhooks .sz-webhook-form,
html body .sz-root.sz-dark #sec-webhooks .sz-webhook-payload{
  background:#111827!important;
  border-color:rgba(255,255,255,.10)!important;
  box-shadow:0 22px 60px rgba(0,0,0,.26)!important;
}
html body .sz-root.sz-dark #sec-reports .sz-fi,
html body .sz-root.sz-dark #sec-reports .sz-fs,
html body .sz-root.sz-dark #sec-webhooks .sz-field-input,
html body .sz-root.sz-dark #sec-users input,
html body .sz-root.sz-dark #sec-users select,
html body .sz-root.sz-dark #sec-settings input,
html body .sz-root.sz-dark #sec-settings select{
  background:#020617!important;
  color:#f8fafc!important;
  border-color:rgba(255,255,255,.14)!important;
}
html body .sz-root.sz-dark #sec-reports .sz-tbl tbody tr,
html body .sz-root.sz-dark #sec-webhooks .sz-tbl tbody tr,
html body .sz-root.sz-dark #sec-users .sz-tbl tbody tr{
  background:#0f172a!important;
  box-shadow:0 0 0 1px rgba(255,255,255,.10)!important;
}

@media(max-width:1180px){
  html body .sz-root #sec-links .szlk-acts,
  html body .sz-root #sec-links .szlk-actions{
    grid-template-columns:repeat(3,minmax(0,1fr))!important;
    width:100%!important;
    min-width:0!important;
  }
  html body .sz-root #sec-links .szlk-acts .szlk-btn,
  html body .sz-root #sec-links .szlk-actions .szlk-btn,
  html body .sz-root #sec-links .szlk-acts > a,
  html body .sz-root #sec-links .szlk-acts > button,
  html body .sz-root #sec-links .szlk-actions > a,
  html body .sz-root #sec-links .szlk-actions > button{
    width:100%!important;min-width:0!important;max-width:none!important;flex:1 1 auto!important;
  }
}
@media(max-width:560px){
  html body .sz-root #sec-links .szlk-acts,
  html body .sz-root #sec-links .szlk-actions{grid-template-columns:1fr!important;}
}
/* SAFE REFACTOR 02.1 — Ajustes centrais sem duplicar componente
   - Suporte: botão Atualizar fica dentro do card e respeita a largura disponível.
   - Webhooks: URL destino vira área clicável para copiar; remove dependência do botão Copiar.
   - Links: Expedição/Motoboy/Excluir seguem exatamente a mesma regra global de ação.
*/
html body .sz-root #sec-settings .sz-account-email-row{
  display:grid!important;
  grid-template-columns:minmax(0,1fr) 152px!important;
  gap:10px!important;
  align-items:center!important;
  width:100%!important;
  max-width:100%!important;
  overflow:hidden!important;
}
html body .sz-root #sec-settings .sz-account-email-row .sz-field-input,
html body .sz-root #sec-settings .sz-account-email-row input{
  min-width:0!important;
  width:100%!important;
  max-width:100%!important;
}
html body .sz-root #sec-settings .sz-account-email-row .sz-account-action-btn,
html body .sz-root #sec-settings .sz-account-action-btn{
  width:152px!important;
  min-width:152px!important;
  max-width:152px!important;
  justify-self:end!important;
  flex:0 0 152px!important;
}

html body .sz-root #sec-webhooks .sz-wh-url-line{
  display:block!important;
  width:100%!important;
  min-width:0!important;
}
html body .sz-root #sec-webhooks .sz-wh-url-line .sz-wh-copy-link,
html body .sz-root #sec-webhooks .sz-wh-url-line code.sz-wh-copy-link{
  cursor:pointer!important;
  user-select:none!important;
  display:block!important;
  width:100%!important;
  min-width:0!important;
  max-width:100%!important;
  padding:13px 14px!important;
  border-radius:13px!important;
  color:#E8650A!important;
  background:transparent!important;
  border:1px solid transparent!important;
  overflow:hidden!important;
  text-overflow:ellipsis!important;
  white-space:nowrap!important;
  font-weight:700;
  transition:background .16s ease,border-color .16s ease,box-shadow .16s ease,transform .16s ease!important;
}
html body .sz-root #sec-webhooks .sz-wh-url-line .sz-wh-copy-link:hover,
html body .sz-root #sec-webhooks .sz-wh-url-line code.sz-wh-copy-link:hover{
  background:rgba(249,115,22,.07)!important;
  border-color:rgba(249,115,22,.20)!important;
  box-shadow:0 10px 24px rgba(232,101,10,.10)!important;
  transform:translateY(-1px)!important;
}
html body .sz-root.sz-dark #sec-webhooks .sz-wh-url-line .sz-wh-copy-link,
html body .sz-root.sz-dark #sec-webhooks .sz-wh-url-line code.sz-wh-copy-link{
  color:#fb923c!important;
  background:transparent!important;
}
html body .sz-root #sec-webhooks .sz-wh-copy{display:none!important;}

html body .sz-root #sec-links .szlk-acts{
  display:grid!important;
  grid-template-columns:repeat(3,112px)!important;
  justify-content:center!important;
  align-items:center!important;
  gap:8px!important;
  width:auto!important;
  min-width:352px!important;
  overflow:visible!important;
}
html body .sz-root #sec-links .szlk-acts .szlk-btn,
html body .sz-root #sec-links .szlk-acts > button,
html body .sz-root #sec-links .szlk-acts > a,
html body .sz-root #sec-links .szlk-btn-open,
html body .sz-root #sec-links .szlk-btn-del{
  width:112px!important;
  min-width:112px!important;
  max-width:112px!important;
  height:40px!important;
  min-height:40px!important;
  max-height:40px!important;
  padding:0 14px!important;
  border-radius:12px!important;
  border:1px solid rgba(249,115,22,.56)!important;
  background:linear-gradient(135deg,#ff7a1a,#ea580c)!important;
  color:#fff!important;
  box-shadow:0 10px 24px rgba(232,101,10,.22)!important;
  display:inline-flex!important;
  align-items:center!important;
  justify-content:center!important;
  gap:6px!important;
  font-size:var(--sz-text-meta);
  font-weight:700;
  line-height:1;
  white-space:nowrap!important;
  text-decoration:none!important;
  flex:0 0 112px!important;
  filter:none!important;
  transform:none!important;
}
html body .sz-root #sec-links .szlk-acts .szlk-btn:hover,
html body .sz-root #sec-links .szlk-acts > button:hover,
html body .sz-root #sec-links .szlk-acts > a:hover,
html body .sz-root #sec-links .szlk-btn-open:hover,
html body .sz-root #sec-links .szlk-btn-del:hover{
  background:linear-gradient(135deg,#ff8a2a,#f97316)!important;
  color:#fff!important;
  box-shadow:0 16px 34px rgba(232,101,10,.34)!important;
  transform:translateY(-1px)!important;
  filter:brightness(1.04)!important;
}
@media(max-width:1180px){
  html body .sz-root #sec-settings .sz-account-email-row{grid-template-columns:1fr!important;overflow:visible!important;}
  html body .sz-root #sec-settings .sz-account-email-row .sz-account-action-btn,
  html body .sz-root #sec-settings .sz-account-action-btn{width:100%!important;max-width:100%!important;min-width:0!important;justify-self:stretch!important;}
  html body .sz-root #sec-links .szlk-acts{grid-template-columns:repeat(3,minmax(0,1fr))!important;width:100%!important;min-width:0!important;}
  html body .sz-root #sec-links .szlk-acts .szlk-btn,
  html body .sz-root #sec-links .szlk-acts > button,
  html body .sz-root #sec-links .szlk-acts > a{width:100%!important;min-width:0!important;max-width:none!important;flex:1 1 auto!important;}
}
@media(max-width:560px){html body .sz-root #sec-links .szlk-acts{grid-template-columns:1fr!important;}}


/* Afiliados nos checkouts */
.sz-lk-col-aff{flex:0 0 118px;display:flex;align-items:center;justify-content:center;font-size:var(--sz-text-xs);font-weight:700;color:var(--tx3);text-transform:none;letter-spacing:0}.szlk-aff-switch{position:relative;display:inline-flex;align-items:center;cursor:pointer}.szlk-aff-switch input{position:absolute;opacity:0;pointer-events:none}.szlk-aff-switch span{width:38px;height:22px;border-radius:999px;background:#d1d5db;display:inline-block;position:relative;transition:.18s}.szlk-aff-switch span:before{content:"";position:absolute;width:16px;height:16px;border-radius:999px;background:#fff;left:3px;top:3px;box-shadow:0 1px 3px rgba(15,23,42,.22);transition:.18s}.szlk-aff-switch input:checked+span{background:#34d399}.szlk-aff-switch input:checked+span:before{transform:translateX(16px)}.szlk-aff-create{display:flex;align-items:center;justify-content:space-between;gap:12px;border:1px solid var(--bd);border-radius:14px;background:var(--c2);padding:14px 16px;margin:8px 0 4px}.szlk-aff-create span{font-size:var(--sz-text-meta);font-weight:700;color:var(--tx);letter-spacing:0}



/* Senderzz v25 — Checkouts: grid premium, botões alinhados e visual limpo */
html body .sz-root #sec-links .sz-hero .sz-kicker{letter-spacing:0;color:#E8650A!important;font-weight:700}
html body .sz-root #sec-links .sz-hero h1{letter-spacing:-.015em}
html body .sz-root #sec-links .szlk-card{border:1px solid #e8edf3!important;border-radius:22px!important;background:#fff!important;box-shadow:0 10px 28px rgba(15,23,42,.035)!important;overflow:hidden!important;margin-top:18px!important}
html body .sz-root #sec-links .szlk-head{display:none!important}
html body .sz-root #sec-links .szlk-body{padding:30px 30px 34px!important;background:#fff!important;overflow:visible!important}
html body .sz-root #sec-links .sz-lk-hd,
html body .sz-root #sec-links .sz-lk-row{display:grid!important;grid-template-columns:minmax(120px,.9fr) minmax(220px,1.35fr) minmax(120px,.65fr) minmax(310px,1.25fr) minmax(120px,.55fr) minmax(92px,.45fr)!important;gap:24px!important;align-items:center!important;width:100%!important;box-sizing:border-box!important}
html body .sz-root #sec-links .sz-lk-hd{padding:18px 28px 14px!important;border:0!important;background:transparent!important;color:#6f7b8d!important;font-size:var(--sz-text-sm);font-weight:700;letter-spacing:0;text-transform:none}
html body .sz-root #sec-links .sz-lk-hd .sz-lk-col-del:before{content:"Excluir"!important}
html body .sz-root #sec-links .sz-lk-row{min-height:94px!important;padding:0 28px!important;margin:0 0 14px!important;border:1px solid #e8edf3!important;border-radius:17px!important;background:#fff!important;box-shadow:0 7px 20px rgba(15,23,42,.025)!important;transition:border-color .16s ease,box-shadow .16s ease,transform .16s ease!important}
html body .sz-root #sec-links .sz-lk-row:hover{background:#fff!important;border-color:rgba(232,101,10,.22)!important;box-shadow:0 14px 30px rgba(15,23,42,.045)!important;transform:translateY(-1px)!important}
html body .sz-root #sec-links .sz-lk-col-name,
html body .sz-root #sec-links .sz-lk-col-prod,
html body .sz-root #sec-links .sz-lk-col-val,
html body .sz-root #sec-links .sz-lk-col-links,
html body .sz-root #sec-links .sz-lk-col-aff,
html body .sz-root #sec-links .sz-lk-col-del{flex:none!important;width:auto!important;min-width:0!important;max-width:none!important;display:flex!important;align-items:center!important;box-sizing:border-box!important}
html body .sz-root #sec-links .sz-lk-col-name{justify-content:flex-start!important;font-size:var(--sz-text-lg);font-weight:700;color:#0f172a!important;letter-spacing:-.015em;overflow:hidden!important;text-overflow:ellipsis!important;white-space:nowrap!important}
html body .sz-root #sec-links .sz-lk-col-prod{justify-content:flex-start!important;font-size:var(--sz-text-lg);font-weight:600;color:#64748b!important;overflow:hidden!important;text-overflow:ellipsis!important;white-space:nowrap!important}
html body .sz-root #sec-links .sz-lk-col-val{justify-content:flex-start!important;font-size:var(--sz-text-lg);font-weight:700;color:#0f172a!important;white-space:nowrap!important}
html body .sz-root #sec-links .sz-lk-col-links{justify-content:flex-start!important;gap:10px!important;flex-wrap:nowrap!important}
html body .sz-root #sec-links .sz-lk-col-aff{justify-content:center!important;font-size:var(--sz-text-sm);font-weight:700;color:#6f7b8d!important;letter-spacing:0;text-transform:none}
html body .sz-root #sec-links .sz-lk-col-del{justify-content:center!important}
html body .sz-root #sec-links .szlk-btn{height:54px!important;min-height:54px!important;border-radius:14px!important;padding:0 22px!important;font-size:var(--sz-text-lg);font-weight:700;line-height:1;border:0!important;display:inline-flex!important;align-items:center!important;justify-content:center!important;gap:9px!important;white-space:nowrap!important;box-shadow:none!important;transition:transform .14s ease,filter .14s ease,box-shadow .14s ease!important}
html body .sz-root #sec-links .szlk-btn-open{min-width:128px!important;background:#E8650A!important;color:#fff!important;box-shadow:0 12px 24px rgba(232,101,10,.18)!important}
html body .sz-root #sec-links .szlk-btn-open:hover{filter:brightness(1.04)!important;box-shadow:0 16px 28px rgba(232,101,10,.24)!important}
html body .sz-root #sec-links .szlk-btn-del{width:66px!important;min-width:66px!important;padding:0!important;background:#07111f!important;border-color:#07111f!important;color:transparent!important;box-shadow:0 10px 22px rgba(7,17,31,.12)!important;font-size:0}
html body .sz-root #sec-links .szlk-btn-del:before{content:none!important;display:none!important}
html body .sz-root #sec-links .szlk-aff-switch{width:54px!important;height:30px!important;display:inline-flex!important;align-items:center!important;justify-content:center!important}
html body .sz-root #sec-links .szlk-aff-switch span{width:54px!important;height:30px!important;border-radius:999px!important;background:#d7dce3!important;box-shadow:inset 0 0 0 1px rgba(15,23,42,.04)!important}
html body .sz-root #sec-links .szlk-aff-switch span:before{width:22px!important;height:22px!important;left:4px!important;top:4px!important;box-shadow:0 2px 7px rgba(15,23,42,.20)!important}
html body .sz-root #sec-links .szlk-aff-switch input:checked+span{background:#E8650A!important}
html body .sz-root #sec-links .szlk-aff-switch input:checked+span:before{transform:translateX(24px)!important}
html body .sz-root.sz-dark #sec-links .szlk-card,
html body .sz-root.sz-dark #sec-links .szlk-body,
html body .sz-root.sz-dark #sec-links .sz-lk-row{background:#0f172a!important;border-color:rgba(255,255,255,.10)!important}
html body .sz-root.sz-dark #sec-links .sz-lk-col-name,
html body .sz-root.sz-dark #sec-links .sz-lk-col-val{color:#f8fafc!important}
html body .sz-root.sz-dark #sec-links .sz-lk-col-prod{color:#94a3b8!important}
/* v22 UX compacto — Checkouts sem gigantismo */
html body .sz-root #sec-links .sz-section-hero{min-height:108px!important;padding:22px 26px!important;border-radius:22px!important;margin-bottom:16px!important}
html body .sz-root #sec-links .sz-section-hero h1{font-size:var(--sz-text-hero);line-height:1.05;margin:6px 0!important}
html body .sz-root #sec-links .sz-section-hero p{font-size:var(--sz-text-base);margin:0!important}
html body .sz-root #sec-links .sz-section-hero .sz-quick{height:42px!important;min-height:42px!important;padding:0 22px!important;border-radius:14px!important;font-size:var(--sz-text-base);box-shadow:none!important}
html body .sz-root #sec-links .szlk-card{border-radius:22px!important;padding:0!important;margin-bottom:16px!important;box-shadow:none!important}
html body .sz-root #sec-links .szlk-head{min-height:58px!important;padding:16px 18px!important;border-bottom:1px solid #edf1f5!important}
html body .sz-root #sec-links .szlk-icon{width:38px!important;height:38px!important;border-radius:13px!important}
html body .sz-root #sec-links .szlk-title{font-size:var(--sz-text-lg);line-height:1.15}
html body .sz-root #sec-links .szlk-sub{font-size:var(--sz-text-meta);margin-top:3px!important}
html body .sz-root #sec-links .szlk-body{padding:16px 18px!important;border-radius:0 0 22px 22px!important}
html body .sz-root #sec-links .sz-lk-hd{grid-template-columns:1.2fr 1.55fr .7fr 1.15fr .58fr .38fr!important;padding:0 18px 8px!important;font-size:var(--sz-text-sm);letter-spacing:0}
html body .sz-root #sec-links .sz-lk-row{grid-template-columns:1.2fr 1.55fr .7fr 1.15fr .58fr .38fr!important;min-height:70px!important;height:auto!important;padding:10px 18px!important;border-radius:16px!important;margin-bottom:10px!important;gap:12px!important;box-shadow:none!important}
html body .sz-root #sec-links .sz-lk-col-name{font-size:var(--sz-text-md)}
html body .sz-root #sec-links .sz-lk-col-prod{font-size:var(--sz-text-base)}
html body .sz-root #sec-links .sz-lk-col-val{font-size:var(--sz-text-md)}
html body .sz-root #sec-links .sz-lk-col-links{gap:8px!important}
html body .sz-root #sec-links .szlk-btn{height:36px!important;min-height:36px!important;padding:0 13px!important;border-radius:12px!important;font-size:var(--sz-text-meta);gap:6px!important;box-shadow:none!important}
html body .sz-root #sec-links .szlk-btn-open{min-width:104px!important;box-shadow:none!important}
html body .sz-root #sec-links .szlk-btn-del{width:42px!important;min-width:42px!important;height:38px!important;min-height:38px!important;border-radius:12px!important;box-shadow:none!important}
html body .sz-root #sec-links .szlk-btn-del:before{content:none!important;display:none!important}
html body .sz-root #sec-links .szlk-aff-switch{transform:scale(.86)!important;transform-origin:center!important}
/* v260: overrides sz-checkout-cpf-notice removidos */
html body .sz-root #sec-links .szlk-create-card .szlk-head{padding:14px 18px!important;min-height:58px!important}
html body .sz-root #sec-links .szlk-toggle-form{height:40px!important;min-height:40px!important;padding:0 18px!important;border-radius:13px!important;font-size:var(--sz-text-meta)}
@media(max-width:1180px){
  html body .sz-root #sec-links .sz-lk-hd{display:none!important}
  html body .sz-root #sec-links .sz-lk-row{grid-template-columns:1fr!important;height:auto!important;min-height:0!important;padding:20px!important;gap:12px!important}
  html body .sz-root #sec-links .sz-lk-col-links{justify-content:stretch!important;display:grid!important;grid-template-columns:1fr 1fr!important;width:100%!important}
  html body .sz-root #sec-links .szlk-btn-open{width:100%!important;min-width:0!important}
  html body .sz-root #sec-links .sz-lk-col-aff,.sz-lk-col-del{justify-content:flex-start!important}
}
</style>

    <?php sz_banner( 'VENDAS', 'Links de checkout', 'Crie e gerencie links de venda, expedição e afiliados.', 'Novo link', 'szOpenCheckoutLinkForm()' ); ?>

<script>
function szToggleCheckoutLinkForm(forceOpen){
  var card = document.getElementById('szlk-create-card');
  if(!card) return;
  var shouldOpen = forceOpen === true ? true : card.classList.contains('is-collapsed');
  card.classList.toggle('is-collapsed', !shouldOpen);
  card.classList.toggle('is-open', shouldOpen);
  var btn = card.querySelector('.szlk-toggle-form');
  if(btn) btn.textContent = shouldOpen ? 'Fechar formulário' : 'Criar checkout';
}
function szOpenCheckoutLinkForm(){
  szToggleCheckoutLinkForm(true);
  setTimeout(function(){
    var el = document.getElementById('sz-cl-nome');
    if(el) el.focus();
    var card = document.getElementById('szlk-create-card');
    if(card) card.scrollIntoView({behavior:'smooth',block:'start'});
  }, 80);
}
</script>


    <div class="szlk-card">
        <div class="szlk-head">
            <div class="szlk-head-left">
                <div class="szlk-icon">
                    <svg viewBox="0 0 24 24"><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/></svg>
                </div>
                <div>
                <div class="szlk-title">Checkouts criados</div>
                    <div class="szlk-sub"><?php echo empty($existing_links) ? 'Nenhum link ainda' : count($existing_links) . ' ' . (count($existing_links) === 1 ? 'link ativo' : 'links ativos'); ?></div>
                </div>
            </div>
        </div>
        <div class="szlk-body">
            <?php if ( empty( $existing_links ) ) : ?>
                <div class="sz-empty"><p>Nenhum checkout criado ainda. Use o formulário abaixo.</p></div>
            <?php else : ?>
                <div class="sz-lk-hd">
                    <span class="sz-lk-col-name">Nome</span>
                    <span class="sz-lk-col-prod">Produto(s)</span>
                    <span class="sz-lk-col-val">Valor</span>
                    <span class="sz-lk-col-commission">Comissão</span>
                    <span class="sz-lk-col-links">Links</span>
                    <span class="sz-lk-col-aff">Afiliados</span>
                    <span class="sz-lk-col-del"></span>
                </div>
               <?php
                $links_principais = array_filter( $existing_links, function($l) {
                    return empty($l['tipo']) || $l['tipo'] === 'correio';
                });
                $motoboy_map = [];
                foreach ( $existing_links as $l ) {
                    if ( ($l['tipo'] ?? '') === 'motoboy' ) {
                        $motoboy_map[$l['id']] = $l['url'];
                    }
                }
                $portal_wp_uid_lk   = (int) ( $sz_links_owner_wp_uid ?? 0 );
                $lk_has_expedicao   = ! function_exists('sz_producer_has_expedicao') || sz_producer_has_expedicao( $portal_wp_uid_lk );
                $lk_has_motoboy_mod = ! function_exists('sz_producer_has_motoboy')   || sz_producer_has_motoboy( $portal_wp_uid_lk );
                foreach ( $links_principais as $link ) :
                    $mb_id  = (int) ($link['link_motoboy_id'] ?? 0);
                    $mb_url = $mb_id ? ($motoboy_map[$mb_id] ?? '') : '';
                    if ( ! $mb_url && $mb_id ) {
                        global $wpdb;
                        $mb_url = (string) $wpdb->get_var( $wpdb->prepare(
                            "SELECT url FROM {$wpdb->prefix}senderzz_checkout_links WHERE id = %d LIMIT 1",
                            $mb_id
                        ) );
                    }
                    $display_name = preg_replace('/ — (Motoboy|Correio|Expedição)$/i', '', $link['name'] ?? '');
                    $sz_exp_lk_token = trim( (string) ( $link['token'] ?? $link['slug'] ?? '' ) );
                    $sz_exp_lk_url = $sz_exp_lk_token !== '' ? add_query_arg( 'sz', $sz_exp_lk_token, home_url( '/checkouts/lp/' ) ) : (string) ( $link['url'] ?? '' );
                ?>
                <div class="sz-lk-row">
                    <span class="sz-lk-col-name sz-lk-cell-name"><?php echo esc_html( $display_name ); ?></span>
                    <span class="sz-lk-col-prod sz-lk-cell-muted"><?php echo esc_html( $link['components_text'] ?? '—' ); ?></span>
                    <span class="sz-lk-col-val sz-lk-cell-val"><?php echo esc_html( $link['price_label'] ?? '—' ); ?></span>
                    <?php $sz_link_commission_lk = isset( $link['affiliate_commission_pct'] ) && (float) $link['affiliate_commission_pct'] > 0 ? (float) $link['affiliate_commission_pct'] : ( function_exists( 'sz_aff_producer_default_commission_pct' ) ? sz_aff_producer_default_commission_pct( (int) ( $link['producer_id'] ?? $link['user_id'] ?? 0 ) ) : 0 ); ?>
                    <span class="sz-lk-col-commission sz-lk-cell-val"><?php echo esc_html( number_format( $sz_link_commission_lk, 2, ',', '.' ) . '%' ); ?></span>
                    <span class="sz-lk-col-links sz-lk-cell-btns">
                        <?php
                        $btn_count = ($lk_has_expedicao ? 1 : 0) + ($mb_url && $lk_has_motoboy_mod ? 1 : 0);
                        ?>
                        <span class="szlk-acts szlk-acts-<?php echo $btn_count; ?>">
                        <?php if ( $lk_has_expedicao ) : ?>
                        <button type="button" class="szlk-btn szlk-btn-open" onclick="szCopyText(this,<?php echo esc_js( wp_json_encode( $sz_exp_lk_url ) ); ?>)">📦 Expedição</button>
                        <?php endif; ?>
                        <?php if ( $mb_url && $lk_has_motoboy_mod ) : ?>
                        <button type="button" class="szlk-btn szlk-btn-open" onclick="szCopyText(this,<?php echo esc_js( wp_json_encode( $mb_url ) ); ?>)">🏍️ Motoboy</button>
                        <?php endif; ?>
                        </span>
                    </span>
                    <span class="sz-lk-col-aff">
                        <label class="szlk-aff-switch" title="Indica se a oferta estará visível para afiliados">
                            <input type="checkbox" <?php checked( ! empty( $link['affiliate_visible'] ) ); ?> onchange="szCLToggleAffiliate(<?php echo (int) $link['id']; ?>,this.checked,'<?php echo esc_js($n); ?>')">
                            <span></span>
                        </label>
                    </span>
                    <span class="sz-lk-col-del">
                        <button type="button" class="szlk-btn szlk-btn-del" onclick="szCLDelete(<?php echo (int) $link['id']; ?>,'<?php echo esc_js($n); ?>')">Excluir</button>
                    </span>
                </div>
                <?php endforeach; ?>
            <?php endif; ?>
        </div>

    </div>

<div class="szlk-card szlk-create-card is-collapsed" id="szlk-create-card">
        <div class="szlk-head">
            <div class="szlk-head-left">
                <div class="szlk-icon">
                    <svg viewBox="0 0 24 24"><path d="M10 13a5 5 0 007.54.54l3-3a5 5 0 00-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 00-7.54-.54l-3 3a5 5 0 007.07 7.07l1.71-1.71"/></svg>
                </div>
                <div>
                    <div class="szlk-title">Novo link de checkout exclusivo</div>
                    <div class="szlk-sub">Crie um link de pagamento com produto e valor fixos para campanhas e vendas diretas.</div>
                </div>
            </div>
            <button type="button" class="szlk-toggle-form" onclick="szToggleCheckoutLinkForm()">Criar checkout</button>
        </div>
        <div class="szlk-body">
            <div id="sz-cl-msg" class="sz-alert" style="display:none;margin-bottom:14px;"></div>
            <div class="szlk-grid2">
                <div class="szlk-field">
                    <label class="szlk-lbl">Nome do link</label>
                    <input autocomplete="off" autocorrect="off" autocapitalize="off" spellcheck="false" id="sz-cl-nome" class="szlk-input" type="text" placeholder="Ex: Kit 3 Frascos R$197">
                </div>
                <div class="szlk-field">
                    <label class="szlk-lbl">Valor da oferta</label>
                    <div class="szlk-pfx-wrap">
                        <span class="szlk-pfx">R$</span>
                        <input autocomplete="off" autocorrect="off" autocapitalize="off" spellcheck="false" id="sz-cl-valor" class="szlk-input" type="number" min="0" step="0.01" placeholder="197,00">
                    </div>
                </div>
            </div>
            <div class="szlk-section-row">
                <span class="szlk-section-lbl">Composição</span>
                <button type="button" class="szlk-add-btn" onclick="szCLAddItem()">
                    <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
                    Produto
                </button>
            </div>
            <div id="sz-cl-items">
                <div class="szlk-item-row sz-cl-item">
                    <select autocomplete="off" class="sz-cl-product" onchange="szCLProductChanged(this)">
                        <option value="">Selecione o produto</option>
                        <?php foreach ( $products as $p ) : ?>
                        <option value="<?php echo esc_attr( $p->get_id() ); ?>" data-type="<?php echo esc_attr( $p->get_type() ); ?>"><?php echo esc_html( $p->get_name() ); ?></option>
                        <?php endforeach; ?>
                    </select>
                    <select autocomplete="off" class="sz-cl-variation" style="display:none;"><option value="">Variação</option></select>
                    <input autocomplete="off" autocorrect="off" autocapitalize="off" spellcheck="false" class="sz-cl-qty" type="number" min="1" step="1" value="1" title="Quantidade">
                    <button type="button" onclick="szCLRemoveItem(this)" class="szlk-rm-btn">&#215;</button>
                </div>
            </div>
            <div class="szlk-aff-create">
                <span>Disponibilizar para afiliados</span>
                <label class="szlk-aff-switch" title="Indica se a oferta estará visível ou não para afiliados">
                    <input type="checkbox" id="sz-cl-affiliate-visible" value="1">
                    <span></span>
                </label>
            </div>
            <button type="button" class="szlk-gerar-btn" id="sz-cl-btn-gerar" onclick="szCLGerar('<?php echo esc_js($n); ?>')">Gerar link</button>
        </div>
    </div>

    
    <?php
    return ob_get_clean();
}

private function get_products_by_shipping_class( $class_id ): array {
        // Aceita int ou array de class IDs
        $class_ids_arr = is_array($class_id) ? array_map('intval', $class_id) : [ (int)$class_id ];
        $class_ids_arr = array_values( array_filter($class_ids_arr, fn($id) => $id > 0) );
        if ( empty($class_ids_arr) ) return [];
    if ( ! function_exists( 'wc_get_product' ) ) return [];

    $first_class_id = is_array($class_id) ? (int)($class_id[0] ?? 0) : (int)$class_id;
    $term = $first_class_id > 0 ? get_term( $first_class_id, 'product_shipping_class' ) : null;
    if ( ! $term || is_wp_error( $term ) ) return [];

    $q = new \WP_Query( [
        'post_type'      => 'product',
        'post_status'    => 'publish',
        'posts_per_page' => 100,
        'orderby'        => 'title',
        'order'          => 'ASC',
        'tax_query'      => [ [
            'taxonomy' => 'product_shipping_class',
            'field'    => 'term_id',
            'terms'    => $class_ids_arr,
        ] ],
    ] );

    $products = [];

    foreach ( $q->posts as $post ) {
        $product = wc_get_product( $post->ID );
        if ( ! $product ) continue;

        $name = strtolower( $product->get_name() );

        // Mostra somente kits
        if (
            strpos( $name, 'kit' ) === false &&
            strpos( $name, 'combo' ) === false &&
            strpos( $name, 'pomada +' ) === false &&
            strpos( $name, 'gotas +' ) === false
        ) {
            continue;
        }

        $products[] = $product;
    }

    wp_reset_postdata();
    return $products;
}

private function get_funnelkit_checkout_url_by_product( int $product_id ): string {
    $url = trim( (string) get_post_meta( $product_id, 'senderzz_checkout_url', true ) );

    if ( ! empty( $url ) ) {
        return esc_url_raw( $url );
    }

    $product = function_exists( 'wc_get_product' ) ? wc_get_product( $product_id ) : false;

    if ( $product && $product->is_type( 'variation' ) ) {
        $parent_url = trim( (string) get_post_meta( $product->get_parent_id(), 'senderzz_checkout_url', true ) );

        if ( ! empty( $parent_url ) ) {
            return esc_url_raw( $parent_url );
        }
    }

    return '';
}
    // ── USERS ──────────────────────────────────────────────────────────────

// ── USERS ──────────────────────────────────────────────────────────────

private function render_users( object $owner, string $n ): string {
    global $wpdb;

    $table = $wpdb->prefix . Portal_Auth::TABLE;

    $sub_users = $wpdb->get_results(
        $wpdb->prepare(
            "SELECT * FROM {$table} WHERE parent_user_id=%d ORDER BY created_at DESC",
            $owner->id
        ),
        ARRAY_A
    ) ?: [];

    $perms_list = [
        'approve' => 'Autorizar envio',
        'cancel'  => 'Cancelar pedido',
        'suspend' => 'Perda / extravio',
        'wallet'  => 'Ver carteira',
        'links'   => 'Gerenciar links',
    ];

    $html = '<div class="sz-users-layout sz-users-layout-v4">';

    // Card: criar acesso
    $html .= '
        <div class="sz-card sz-user-create-card sz-user-create-card-v4">
            <div class="sz-users-card-head">
                <div>
                    <h3>Novo acesso</h3>
                    <p>Cadastre uma pessoa da equipe e defina as permissões.</p>
                </div>
            </div>

            <div class="sz-user-form">
                <div class="sz-user-field">
                    <label>E-mail</label>
                    <input type="email" id="u-em" class="sz-fi" placeholder="funcionario@email.com" autocomplete="off" autocorrect="off" autocapitalize="off" spellcheck="false">
                </div>

                <div class="sz-user-field">
                    <label>Senha</label>
                    <input type="password" id="u-pw" class="sz-fi" placeholder="Mínimo 8 caracteres" autocomplete="new-password">
                </div>

                <div class="sz-user-field">
                    <label>Nome</label>
                    <input type="text" id="u-nm" class="sz-fi" placeholder="Nome do funcionário" autocomplete="off" autocorrect="off" autocapitalize="off" spellcheck="false">
                </div>

                <div class="sz-user-field">
                    <label>Permissões</label>

                    <div class="sz-user-permissions-wrap">
                        <div class="sz-user-permissions-grid">
    ';

    foreach ( $perms_list as $key => $label ) {
        $html .= '
            <label class="sz-user-permission-item">
                <input autocomplete="off" autocorrect="off" autocapitalize="off" spellcheck="false" type="checkbox" name="perm" value="' . esc_attr( $key ) . '">
                <span>' . esc_html( $label ) . '</span>
            </label>
        ';
    }

    $html .= '
                        </div>
                    </div>
                </div>

                <button class="sz-user-create-btn" onclick="szCreateUser(\'' . esc_js( $n ) . '\')">
                    Criar acesso
                </button>

                <div id="u-msg" class="sz-alert" style="display:none;margin-top:12px"></div>
            </div>
        </div>
    ';

    // Card: lista de acessos
    $html .= '
        <div class="sz-card sz-users-list-card sz-users-list-card-v4">
            <div class="sz-users-list-head">
                <div>
                    <h3>Acessos criados</h3>
                    <p>Usuários autorizados a acessar este painel.</p>
                </div>

                <span class="sz-users-counter">' . count( $sub_users ) . '</span>
            </div>
    ';

    if ( empty( $sub_users ) ) {
        $html .= '
            <div class="sz-users-empty">
                <div class="sz-users-empty-inner">
                    <div class="sz-users-empty-icon">👤</div>
                    <h4>Nenhum acesso criado</h4>
                    <p>Quando você cadastrar membros da equipe, eles aparecerão aqui com permissões e ações disponíveis.</p>
                </div>
            </div>
        ';
    } else {
        $html .= '<div class="sz-user-access-list">';

        foreach ( $sub_users as $u ) {
            $perms       = json_decode( $u['permissions'] ?? '{}', true ) ?: [];
            $perm_labels = array_map(
                fn( $k ) => $perms_list[ $k ] ?? $k,
                array_keys( array_filter( $perms ) )
            );

            $email   = (string) ( $u['email'] ?? '' );
            $name    = trim( (string) ( $u['name'] ?? '' ) );
            $initial = strtoupper( mb_substr( $name ?: $email, 0, 1 ) );

            $html .= '
                <div class="sz-user-access-item">
                    <div class="sz-user-access-main">
                        <div class="sz-user-access-avatar">' . esc_html( $initial ) . '</div>

                        <div class="sz-user-access-copy">
                            <strong>' . esc_html( $name ?: $email ) . '</strong>
                            <small>' . esc_html( $email ) . '</small>

                            <div class="sz-user-access-perms">
                                ' . esc_html( ! empty( $perm_labels ) ? implode( ' · ', $perm_labels ) : 'Somente visualizar' ) . '
                            </div>
                        </div>
                    </div>

                    <div class="sz-user-access-actions">
                        <button class="sz-user-delete-btn" onclick="szDeleteUser(' . absint( $u['id'] ) . ',\'' . esc_js( $n ) . '\')">
                            Excluir
                        </button>
                    </div>
                </div>
            ';
        }

        $html .= '</div>';
    }

    $html .= '</div></div>';

    return $html;
}

    // ── TRANSACTIONS ───────────────────────────────────────────────────────

    private function public_wallet_description( array $t ): string {
        return senderzz_portal_wallet_description( $t );
    }
	
    private function render_transactions( int $uid ): string {
        global $wpdb;
        $rows = $wpdb->get_results($wpdb->prepare(
            "SELECT * FROM {$wpdb->prefix}tpc_transacoes WHERE user_id=%d ORDER BY created_at DESC LIMIT 30",
            $uid), ARRAY_A) ?: [];
        if(empty($rows)) return '<p class="sz-hint" style="padding:20px 0;text-align:center">Nenhuma transação.</p>';
        $h = '';
        foreach($rows as $t){
            $c=$t['tipo']==='credito';
            $desc = senderzz_portal_wallet_description( $t );
            $h.='<div class="sz-tx'.($t['status']!=='confirmado'?' pend':'').'">'
               .'<div><div class="sz-tx-d">'.esc_html($desc).'</div><div class="sz-tx-dt">'.esc_html(date_i18n('d/m/Y H:i',strtotime($t['created_at']))).  ' · '.esc_html(ucfirst($t['status'])).'</div></div>'
               .'<div class="sz-tx-v '.($c?'cr':'db').'">'.($c?'+':'-').' R$ '.number_format((float)$t['valor'],2,',','.').'</div>'
               .'</div>';
        }
        return $h;
    }
    // ── REPORTS ────────────────────────────────────────────────────────────

    private function is_motoboy_report_order( array $o ): bool {
        if ( ( $o['delivery_mode'] ?? '' ) === 'motoboy' ) {
            return true;
        }
        $hay = strtolower( (string) ( $o['shipping_name'] ?? '' ) . ' ' . ( $o['shipping_method'] ?? '' ) . ' ' . ( $o['carrier'] ?? '' ) );
        return strpos( $hay, 'motoboy' ) !== false || strpos( $hay, 'moto boy' ) !== false;
    }

    private function filter_report_orders_by_mode( array $orders, string $mode ): array {
        $mode = $mode === 'expedicao' ? 'expedicao' : 'motoboy';
        return array_values( array_filter( $orders, function( $o ) use ( $mode ) {
            $is_motoboy = $this->is_motoboy_report_order( (array) $o );
            return $mode === 'motoboy' ? $is_motoboy : ! $is_motoboy;
        } ) );
    }

    private function report_status_matches( array $o, string $wanted ): bool {
        $wanted = strtolower( trim( preg_replace( '/^wc-/', '', $wanted ) ) );
        if ( $wanted === '' ) return true;
        $actual = strtolower( trim( preg_replace( '/^wc-/', '', (string) ( $o['status'] ?? '' ) ) ) );
        $aliases = [
            'cancelado' => [ 'cancelado', 'cancelled', 'canceled' ],
            'cancelled' => [ 'cancelled', 'cancelado', 'canceled' ],
            'em_rota' => [ 'em_rota', 'em-rota', 'emrota', 'out-for-delivery' ],
            'a_caminho' => [ 'a_caminho', 'a-caminho', 'acaminho', 'enviado' ],
            'entregue' => [ 'entregue', 'completed', 'concluido', 'concluído', 'delivered' ],
            'completed' => [ 'completed', 'entregue', 'concluido', 'concluído', 'delivered' ],
            'agendado' => [ 'agendado', 'on-hold', 'aprovado' ],
            'on-hold' => [ 'on-hold', 'agendado', 'aguardando' ],
        ];
        $accepted = $aliases[ $wanted ] ?? [ $wanted ];
        return in_array( $actual, $accepted, true );
    }

    private function render_reports( array $orders ): string {
        if(empty($orders)) return '<div class="sz-empty"><p>Nenhum pedido.</p></div>';
        $by_c=[]; $by_s=[];
        foreach($orders as $o){ $c=$o['shipping_name']?:'—'; $by_c[$c]=($by_c[$c]??0)+1; $status_label = senderzz_portal_status_label((string)($o['status'] ?? '')); $by_s[$status_label]=($by_s[$status_label]??0)+1; }
        arsort($by_c); arsort($by_s);
        $total=count($orders);
        $h='<div class="sz-rep-grid">';
        $h.='<div class="sz-card sz-report-mini-card"><h3>Por transportadora</h3><small>Distribuição dos pedidos por modalidade.</small>';
        foreach($by_c as $nm=>$cnt){ $p=round($cnt/$total*100); $h.='<div class="sz-rep-row"><span class="sz-rep-l">'.esc_html($nm).'</span><div class="sz-rep-bw"><div class="sz-rep-b" style="width:'.$p.'%"></div></div><span class="sz-rep-n">'.$cnt.'</span></div>'; }
        $h.='</div><div class="sz-card sz-report-mini-card"><h3>Por status</h3><small>Resumo operacional dos pedidos filtrados.</small>';
        foreach($by_s as $nm=>$cnt){ $p=round($cnt/$total*100); $h.='<div class="sz-rep-row"><span class="sz-rep-l">'.esc_html($nm).'</span><div class="sz-rep-bw"><div class="sz-rep-b" style="width:'.$p.'%"></div></div><span class="sz-rep-n">'.$cnt.'</span></div>'; }
        $h.='</div><div class="sz-card sz-rep-full"><div class="sz-report-table-head"><div><h3>Tabela de pedidos</h3><small>Pedidos encontrados no período selecionado.</small></div></div><div class="sz-tbl-w"><table class="sz-tbl"><thead><tr><th>Pedido</th><th>Data</th><th>Cliente</th><th>CPF/CNPJ</th><th>Telefone</th><th>Produto</th><th>Status</th><th>Transportadora</th><th>Valor</th><th>Frete</th><th>Rastreio</th><th>Endereço de entrega</th></tr></thead><tbody>';
        foreach($orders as $o){
            $t=!empty($o['tracking_codes'])?implode(', ',$o['tracking_codes']):'—';
            $cpf_r = $o['billing_cpf'] ?? '';
            $phone_r = $o['billing']['phone'] ?? '';
            $customer_r = trim($o['billing']['name'] ?? '');
            $addr_r = wp_strip_all_tags($o['shipping']['address'] ?? $o['billing']['address'] ?? '');
            $h.='<tr>'
                .'<td>#'.esc_html($o['number']).'</td>'
                .'<td>'.esc_html($o['date']).'</td>'
                .'<td>'.esc_html($customer_r ?: '—').'</td>'
                .'<td>'.esc_html($cpf_r ?: '—').'</td>'
                .'<td>'.esc_html($phone_r ?: '—').'</td>'
                .'<td>'.esc_html($o['product_name']?:'—').'</td>'
                .'<td><span class="sz-wh-status '.senderzz_portal_status_class((string)$o['status']).'">'.esc_html(senderzz_portal_status_label((string)$o['status'])).'</span></td>'
                .'<td>'.esc_html($o['shipping_name']?:'—').'</td>'
                .'<td>'.wp_kses_post($o['total_no_ship'] ?? $o['total']).'</td>'
                .'<td>'.wp_kses_post($o['shipping_total'] ?? 'R$ 0,00').'</td>'
                .'<td>'.esc_html($t).'</td>'
                .'<td><span class="sz-addr-cell">'.esc_html($addr_r ?: '—').'</span></td>'
                .'</tr>';
        }
        $h.='</tbody></table></div></div></div>';
        return $h;
    }

    // ── DASHBOARD PREMIUM ─────────────────────────────────────────────────

    private function calc_dashboard_metrics( array $orders ): array {
        return senderzz_portal_calc_dashboard_metrics( $orders );
    }

    private function money( float $v ): string {
        return senderzz_portal_money( $v );
    }

    private function senderzz_status_label( string $status ): string {
        return senderzz_portal_status_label( $status );
    }


    private function render_order_status_cards( array $orders ): string {
        return senderzz_portal_render_order_status_cards( $orders );
    }

    private function render_dashboard_overview( array $orders, string $saldo_fmt ): string {
        $m = senderzz_portal_calc_dashboard_metrics($orders);
        $total = count($orders);
        $lucro_operacional = max(0, (float) $m['receita'] - (float) $m['frete']);

        $cancelados = 0;
        foreach ( $orders as $o ) {
            $st = (string) ($o['status'] ?? '');
            if ( in_array( $st, ['cancelled','cancelado'], true ) ) {
                $cancelados++;
            }
        }

        $top_carrier_label = '';
        $top_carrier_count = 0;
        $top_carrier_pct   = 0;
        if ( ! empty( $m['by_carrier'] ) ) {
            $carrier_keys      = array_keys( $m['by_carrier'] );
            $top_carrier_label = (string) reset( $carrier_keys );
            $top_carrier_count = (int) reset( $m['by_carrier'] );
            $top_carrier_pct   = $total ? (int) round( ( $top_carrier_count / $total ) * 100 ) : 0;
        }

        $financial_cards = [];
        if ( $total > 0 ) {
            $financial_cards[] = [ 'Pedidos no período', (string) $total ];
        }
        if ( (float) $m['receita'] > 0 ) {
            $financial_cards[] = [ 'Receita bruta', senderzz_portal_money( (float) $m['receita'] ) ];
        }
        if ( (float) $m['frete'] > 0 ) {
            $financial_cards[] = [ 'Custo de envio', senderzz_portal_money( (float) $m['frete'] ) ];
        }
        if ( $lucro_operacional > 0 ) {
            $financial_cards[] = [ 'Receita líquida', senderzz_portal_money( (float) $lucro_operacional ) ];
        }
        if ( preg_match( '/[1-9]/', wp_strip_all_tags( $saldo_fmt ) ) ) {
            $financial_cards[] = [ 'Saldo disponível', $saldo_fmt ];
        }
        if ( $cancelados > 0 ) {
            $financial_cards[] = [ 'Cancelados', (string) $cancelados ];
        }

        $insight_cards = [];
        if ( ! empty( $m['frete_medio'] ) && (float) $m['frete_medio'] > 0 ) {
            $insight_cards[] = [ 'Frete médio', senderzz_portal_money( (float) $m['frete_medio'] ), 'Custo médio de frete por pedido', '' ];
        }
        if ( ! empty( $m['prazo_medio'] ) && (float) $m['prazo_medio'] > 0 ) {
            $insight_cards[] = [ 'Prazo médio', number_format( (float) $m['prazo_medio'], 1, ',', '.' ) . ' dias', 'Média estimada de entrega', '' ];
        }
        if ( ! empty( $m['ticket'] ) && (float) $m['ticket'] > 0 ) {
            $insight_cards[] = [ 'Ticket médio', senderzz_portal_money( (float) $m['ticket'] ), 'Valor médio por pedido', '' ];
        }
        if ( $top_carrier_label && $top_carrier_count > 0 ) {
            $insight_cards[] = [ 'Transportadora líder', $top_carrier_label, $top_carrier_pct . '% dos envios', '' ];
        }
        $pedidos_atencao = (int) ( $m['pedidos_atencao'] ?? 0 );
        if ( $pedidos_atencao > 0 ) {
            $hint = $pedidos_atencao === 1
                ? '1 pedido em on-hold, saldo, erro ou pendência'
                : $pedidos_atencao . ' pedidos em on-hold, saldo, erro ou pendência';
            $insight_cards[] = [ 'Pedidos com atenção', (string) $pedidos_atencao, $hint, ' sz-compact-insight-attention' ];
        }

        ob_start(); ?>
        <div class="sz-dash-hero sz-dash-hero-compact">
            <div>
                <span class="sz-kicker">Senderzz Intelligence</span>
                <h1>Visão da operação</h1>
                <p>Acompanhe pedidos, fretes, saldo e desempenho da operação em tempo real.</p>
            </div>
            <div class="sz-hero-actions">
                <button class="sz-quick dark" onclick="szGo('reports',document.querySelector('[data-nav=reports]'))">Exportar pedidos</button>
            </div>
        </div>

        <?php if ( ! empty( $financial_cards ) ) : ?>
            <div class="sz-fin-grid sz-fin-grid-compact">
                <?php foreach ( $financial_cards as $card ) : ?>
                    <div class="sz-fin-card"><span><?php echo esc_html( $card[0] ); ?></span><strong><?php echo esc_html( $card[1] ); ?></strong></div>
                <?php endforeach; ?>
            </div>
        <?php endif; ?>

        <?php if ( ! empty( $insight_cards ) ) : ?>
            <div class="sz-compact-insights sz-compact-insights-dynamic">
                <?php foreach ( $insight_cards as $card ) : ?>
                    <div class="sz-compact-insight<?php echo esc_attr( $card[3] ); ?>">
                        <span><?php echo esc_html( $card[0] ); ?></span>
                        <strong><?php echo esc_html( $card[1] ); ?></strong>
                        <small><?php echo esc_html( $card[2] ); ?></small>
                    </div>
                <?php endforeach; ?>
            </div>
        <?php endif; ?>

        <?php return ob_get_clean();
    }

    private function render_operations_board( array $orders ): string {
        $cols = [
            'on-hold' => 'Aguardando',
            'aprovado' => 'Aprovados',
            'enviado' => 'Enviados',
            'acaminho' => 'A caminho',
            'saldoinsuficiente' => 'Saldo insuf.',
            'extravio' => 'Extravio',
            'cancelled' => 'Cancelados',
            'completed' => 'Concluídos',
        ];
        $h = '<div class="sz-kanban">';
        foreach ( $cols as $st => $label ) {
            $list = array_values(array_filter($orders, fn($o) => ($o['status'] ?? '') === $st));
            if ( count($list) < 1 ) {
                continue;
            }
            $h .= '<div class="sz-kcol"><div class="sz-khead"><span>'.esc_html($label).'</span><b>'.count($list).'</b></div>';
            foreach ( array_slice($list, 0, 4) as $o ) {
                $h .= '<div class="sz-kitem"><strong>#'.esc_html($o['number']).'</strong><span>'.esc_html($o['product_name'] ?: 'Pedido').'</span></div>';
            }
            if ( count($list) > 4 ) $h .= '<small>+'.(count($list)-4).' pedidos</small>';
            $h .= '</div>';
        }
        if ( $h === '<div class="sz-kanban">' ) {
            return '<p class="sz-hint">Nenhum pedido em operação agora.</p>';
        }
        return $h.'</div>';
    }

    private function render_status_bars( array $rows, int $total ): string {
        return senderzz_portal_render_status_bars( $rows, $total );
    }

    private function render_region_insights( array $rows, int $total ): string {
        return senderzz_portal_render_region_insights( $rows, $total );
    }

    private function render_carrier_insights( array $rows, int $total ): string {
        return senderzz_portal_render_carrier_insights( $rows, $total );
    }


    // ── STYLES ─────────────────────────────────────────────────────────────

    private function styles(): string {
        return '';
    }

    private function critical_sidebar_script(): string {
        // JS externalizado para assets/js/portal-sidebar.js
        // Carregado via wp_enqueue_script em enqueue_portal_assets()
        return '';
    }

    // ── SCRIPTS ────────────────────────────────────────────────────────────

    private function scripts(): string {
        // JS externalizado para assets/js/portal.js
        // Vars PHP passadas via wp_localize_script (enqueue_portal_assets)
        return '';
    }

    // ── AJAX ───────────────────────────────────────────────────────────────


    private function ajax_webhooks_log( object $user ): array {
        global $wpdb;
        $wallet_user_id = senderzz_portal_wallet_user_id( $user );
        if ( ! $wallet_user_id ) return [ 'success' => false, 'message' => 'Carteira não localizada.' ];
        $log_table = $wpdb->prefix . 'senderzz_webhook_log';
        $wh_table  = $wpdb->prefix . 'senderzz_webhooks';
        // Verificar se tabela existe
        if ( $wpdb->get_var( $wpdb->prepare( "SHOW TABLES LIKE %s", $wpdb->esc_like( $log_table ) ) ) !== $log_table ) {
            return [ 'success' => true, 'logs' => [] ];
        }
        $limit  = min( absint( $_POST['limit'] ?? 50 ), 100 );
        $offset = absint( $_POST['offset'] ?? 0 );
        $logs = $wpdb->get_results( $wpdb->prepare(
            "SELECT l.id, l.webhook_id, l.order_id, l.event, l.response_code, l.fired_at,
                    l.payload_json,
                    LEFT(l.payload_json, 500) as payload_preview,
                    LEFT(l.response_body, 300) as response_preview
             FROM {$log_table} l
             INNER JOIN {$wh_table} w ON w.id = l.webhook_id
             WHERE w.user_id = %d
             ORDER BY l.fired_at DESC
             LIMIT %d OFFSET %d",
            (int) $wallet_user_id, $limit, $offset
        ), ARRAY_A ) ?: [];
        foreach ( $logs as &$log ) {
            $log['fired_at_fmt'] = $log['fired_at']
                ? wp_date( 'd/m/Y H:i:s', strtotime( $log['fired_at'] ) )
                : '—';
            $log['ok'] = in_array( (int) $log['response_code'], [ 200, 201, 202, 204 ], true );
            $log['response_code'] = (int) $log['response_code'];
            $payload = ! empty( $log['payload_json'] ) ? json_decode( (string) $log['payload_json'], true ) : [];
            $public_number = '';
            if ( is_array( $payload ) && isset( $payload['pedido'] ) && is_array( $payload['pedido'] ) ) {
                $public_number = (string) ( $payload['pedido']['numero'] ?? $payload['pedido']['id'] ?? '' );
            }
            if ( $public_number === '' && ! empty( $log['order_id'] ) ) {
                $order = wc_get_order( (int) $log['order_id'] );
                if ( $order ) {
                    $public_number = $order->get_order_number();
                }
            }
            $log['public_order_number'] = $public_number;
        }
        unset( $log );
        $total = (int) $wpdb->get_var( $wpdb->prepare(
            "SELECT COUNT(*) FROM {$log_table} l INNER JOIN {$wh_table} w ON w.id = l.webhook_id WHERE w.user_id = %d",
            (int) $wallet_user_id
        ) );
        return [ 'success' => true, 'logs' => array_values( $logs ), 'total' => $total ];
    }

    private function ajax_webhooks_resend( object $user ): array {
        global $wpdb;
        $wallet_user_id = senderzz_portal_wallet_user_id( $user );
        if ( ! $wallet_user_id ) return [ 'success' => false, 'message' => 'Carteira não localizada.' ];
        $log_id = absint( $_POST['log_id'] ?? 0 );
        if ( ! $log_id ) return [ 'success' => false, 'message' => 'Log inválido.' ];
        $log_table = $wpdb->prefix . 'senderzz_webhook_log';
        $wh_table  = $wpdb->prefix . 'senderzz_webhooks';
        $log = $wpdb->get_row( $wpdb->prepare(
            "SELECT l.*, w.url FROM {$log_table} l
             INNER JOIN {$wh_table} w ON w.id = l.webhook_id
             WHERE l.id = %d AND w.user_id = %d LIMIT 1",
            $log_id, (int) $wallet_user_id
        ), ARRAY_A );
        if ( ! $log ) return [ 'success' => false, 'message' => 'Log não encontrado ou sem permissão.' ];
        if ( empty( $log['url'] ) ) return [ 'success' => false, 'message' => 'Webhook sem URL configurada.' ];
        if ( ! function_exists( 'senderzz_pw_fire_url' ) ) return [ 'success' => false, 'message' => 'Módulo de webhooks indisponível.' ];
        $payload = $log['payload_json'] ? json_decode( $log['payload_json'], true ) : [];
        if ( ! is_array( $payload ) ) $payload = [];
        $payload['_reprocessed_at'] = current_time( 'mysql', true );
        $log['_senderzz_update_log_id'] = $log_id;
        $result = senderzz_pw_fire_url( $log, $payload, (int) $log['order_id'], (string) $log['event'] );
        return [
            'success' => ! empty( $result['ok'] ),
            'message' => ! empty( $result['ok'] )
                ? 'Webhook reprocessado com sucesso no mesmo registro (HTTP ' . ( $result['code'] ?? '—' ) . ').'
                : 'Falha no reprocessamento: ' . ( $result['error'] ?? 'erro desconhecido' ) . '.',
        ];
    }

    private function ajax_integrations_get( object $user ): array {
        $wallet_user_id = senderzz_portal_wallet_user_id( $user );
        if ( ! $wallet_user_id ) return [ 'success' => false, 'message' => 'Carteira do produtor não localizada.' ];
        if ( ! function_exists( 'senderzz_int_get_or_create_for_user' ) ) return [ 'success' => false, 'message' => 'Módulo de integrações indisponível.' ];
        global $wpdb;
        $row = senderzz_int_get_or_create_for_user( (int) $wallet_user_id, (int) ( $user->id ?? 0 ) );
        $logs = $wpdb->get_results( $wpdb->prepare(
            "SELECT id, external_order_id, wc_order_id, status, message, payload_json, mapped_json, created_at FROM " . senderzz_int_log_table() . " WHERE integration_id=%d ORDER BY id DESC LIMIT 30",
            (int) ( $row['id'] ?? 0 )
        ), ARRAY_A ) ?: [];
        foreach ( $logs as &$log ) {
            $log['created_at_fmt'] = $log['created_at'] ? date_i18n( 'd/m H:i', strtotime( $log['created_at'] ) ) : '';
        }
        $mapping = json_decode( (string) ( $row['mapping_json'] ?? '' ), true );
        if ( ! is_array( $mapping ) ) $mapping = function_exists( 'senderzz_int_default_mapping' ) ? senderzz_int_default_mapping() : [];
        $last_payload = json_decode( (string) ( $row['last_payload_json'] ?? '' ), true );
        if ( ! is_array( $last_payload ) ) $last_payload = [];
        // v134: na aba Payload mostrar somente o payload recebido/salvo, sem envelope normalizado ou preview de mapeamento.
        $display_payload = $last_payload;
        $mapped_preview = [];
        if ( function_exists( 'senderzz_int_mapped_preview' ) && ! empty( $last_payload ) ) {
            $mapped_preview = senderzz_int_mapped_preview( $last_payload, $row, $mapping );
        }
        foreach ( $logs as &$log ) {
            $mapped_log = ! empty( $log['mapped_json'] ) ? json_decode( (string) $log['mapped_json'], true ) : [];
            if ( is_array( $mapped_log ) && ! empty( $mapped_log['items'] ) ) {
                $log['mapped_items'] = $mapped_log['items'];
            }
            unset( $log['payload_json'], $log['mapped_json'] );
        }
        unset( $log );
        return [
            'success' => true,
            'integration' => [
                'id' => (int) ( $row['id'] ?? 0 ),
                'name' => (string) ( $row['name'] ?? 'Integração padrão' ),
                'token' => (string) ( $row['token'] ?? '' ),
                'active' => ! empty( $row['active'] ),
                'auto_cheapest' => ! empty( $row['auto_cheapest'] ),
                'require_paid' => ! empty( $row['require_paid'] ),
                'ignore_duplicates' => ! empty( $row['ignore_duplicates'] ),
                'url' => rest_url( 'senderzz/v1/integrations/' . rawurlencode( (string) ( $row['token'] ?? '' ) ) ),
                'last_received_at' => (string) ( $row['last_received_at'] ?? '' ),
                'last_status' => (string) ( $row['last_status'] ?? '' ),
                'last_error' => (string) ( $row['last_error'] ?? '' ),
            ],
            'mapping' => $mapping,
            'last_payload' => $display_payload,
            'last_payload_flat' => function_exists( 'senderzz_int_flatten' ) ? senderzz_int_flatten( $display_payload ) : [],
            'mapped_preview' => $mapped_preview,
            'logs' => $logs,
        ];
    }

    private function ajax_integrations_save( object $user ): array {
        $wallet_user_id = senderzz_portal_wallet_user_id( $user );
        if ( ! $wallet_user_id ) return [ 'success' => false, 'message' => 'Carteira do produtor não localizada.' ];
        if ( ! function_exists( 'senderzz_int_get_or_create_for_user' ) ) return [ 'success' => false, 'message' => 'Módulo de integrações indisponível.' ];
        global $wpdb;
        $row = senderzz_int_get_or_create_for_user( (int) $wallet_user_id, (int) ( $user->id ?? 0 ) );
        $mapping_raw = wp_unslash( $_POST['mapping'] ?? '' );
        $mapping = json_decode( (string) $mapping_raw, true );
        if ( ! is_array( $mapping ) ) return [ 'success' => false, 'message' => 'Mapeamento inválido.' ];
        $clean = [];
        foreach ( $mapping as $k => $v ) {
            $clean[ sanitize_key( (string) $k ) ] = sanitize_text_field( (string) $v );
        }
        $data = [
            'mapping_json' => wp_json_encode( $clean, JSON_UNESCAPED_UNICODE ),
            'active' => empty( $_POST['active'] ) ? 0 : 1,
            'auto_cheapest' => 1,
            'require_paid' => empty( $_POST['require_paid'] ) ? 0 : 1,
            'ignore_duplicates' => empty( $_POST['ignore_duplicates'] ) ? 0 : 1,
            'updated_at' => current_time( 'mysql' ),
        ];
        $ok = $wpdb->update( senderzz_int_table(), $data, [ 'id' => (int) $row['id'], 'user_id' => (int) $wallet_user_id ], [ '%s','%d','%d','%d','%d','%s' ], [ '%d','%d' ] );
        if ( false === $ok ) return [ 'success' => false, 'message' => 'Erro ao salvar integração.' ];
        return [ 'success' => true, 'message' => 'Integração salva.' ];
    }

    private function ajax_integrations_reprocess( object $user ): array {
        $sz_dbg = WP_CONTENT_DIR . '/senderzz-logs/integration-reprocess-debug.log';
        @file_put_contents($sz_dbg, date('[Y-m-d H:i:s] ').'ajax_integrations_reprocess chamado. user_id='.($user->id??'?').PHP_EOL, FILE_APPEND);
        $wallet_user_id = senderzz_portal_wallet_user_id( $user );
        if ( ! $wallet_user_id ) {
            @file_put_contents($sz_dbg, date('[Y-m-d H:i:s] ').'ERRO: Carteira nao localizada.'.PHP_EOL, FILE_APPEND);
            return [ 'success' => false, 'message' => 'Carteira do produtor não localizada.' ];
        }
        if ( ! function_exists( 'senderzz_int_process_payload_for_integration' ) ) {
            @file_put_contents($sz_dbg, date('[Y-m-d H:i:s] ').'ERRO: funcao senderzz_int_process_payload_for_integration nao existe.'.PHP_EOL, FILE_APPEND);
            return [ 'success' => false, 'message' => 'Módulo de reprocessamento indisponível.' ];
        }
        global $wpdb;
        $row = senderzz_int_get_or_create_for_user( (int) $wallet_user_id, (int) ( $user->id ?? 0 ) );
        $log_id = absint( $_POST['log_id'] ?? 0 );
        if ( $log_id ) {
            $log = $wpdb->get_row( $wpdb->prepare( "SELECT * FROM " . senderzz_int_log_table() . " WHERE id=%d AND integration_id=%d AND user_id=%d LIMIT 1", $log_id, (int) $row['id'], (int) $wallet_user_id ), ARRAY_A );
        } else {
            $log = $wpdb->get_row( $wpdb->prepare( "SELECT * FROM " . senderzz_int_log_table() . " WHERE integration_id=%d AND user_id=%d AND status IN ('pending','error','ignored','received') ORDER BY id DESC LIMIT 1", (int) $row['id'], (int) $wallet_user_id ), ARRAY_A );
        }
        if ( ! $log ) return [ 'success' => false, 'message' => 'Nenhum recebimento pendente/erro encontrado para reprocessar.' ];
        $payload = ! empty( $log['payload_json'] ) ? json_decode( (string) $log['payload_json'], true ) : [];
        if ( ! is_array( $payload ) || empty( $payload ) ) return [ 'success' => false, 'message' => 'Payload do log indisponível.' ];
        $mapping = json_decode( (string) ( $row['mapping_json'] ?? '' ), true );
        if ( ! is_array( $mapping ) && function_exists( 'senderzz_int_default_mapping' ) ) $mapping = senderzz_int_default_mapping();
        $lock_key = 'senderzz_int_reprocess_' . (int) $log['id'];
        if ( get_transient( $lock_key ) ) {
            return [ 'success' => false, 'message' => 'Reprocessamento já em andamento para este payload. Aguarde alguns segundos e atualize os logs.' ];
        }
        delete_transient( $lock_key ); // limpa lock anterior se houver
        set_transient( $lock_key, 1, 15 );
        @file_put_contents($sz_dbg, date('[Y-m-d H:i:s] ').'Chamando process_payload. log_id='.$log['id'].' external_id='.($log['external_order_id']??'?').PHP_EOL, FILE_APPEND);
        try {
            $processed = senderzz_int_process_payload_for_integration( $payload, $row, $mapping );
        } catch ( Throwable $e ) {
            delete_transient( $lock_key );
            @file_put_contents($sz_dbg, date('[Y-m-d H:i:s] ').'EXCEPTION: '.$e->getMessage().' in '.$e->getFile().':'.$e->getLine().PHP_EOL, FILE_APPEND);
            return [ 'success' => false, 'message' => 'Falha fatal no reprocessamento: ' . $e->getMessage() ];
        }
        delete_transient( $lock_key );
        $mapped = $processed['mapped'] ?? [];
        $status = (string) ( $processed['status'] ?? 'error' );
        $message = (string) ( $processed['message'] ?? 'Falha ao reprocessar.' );
        $order_id = (int) ( $processed['order_id'] ?? 0 );
        @file_put_contents($sz_dbg, date('[Y-m-d H:i:s] ').'Resultado: status='.$status.' order_id='.$order_id.' msg='.$message.PHP_EOL, FILE_APPEND);
        $update_log_data = [
            'external_order_id' => (string) ( $mapped['external_order_id'] ?? $log['external_order_id'] ?? '' ),
            'wc_order_id' => $order_id > 0 ? $order_id : (int) ( $log['wc_order_id'] ?? 0 ),
            'status' => $status,
            'message' => $message . ' Reprocessado em ' . current_time( 'mysql' ) . '.',
            'mapped_json' => wp_json_encode( $mapped, JSON_UNESCAPED_UNICODE ),
        ];
        $wpdb->update( senderzz_int_log_table(), $update_log_data, [ 'id' => (int) $log['id'], 'integration_id' => (int) $row['id'] ], [ '%s','%d','%s','%s','%s' ], [ '%d','%d' ] );
        $wpdb->update( senderzz_int_table(), [ 'last_status' => $status, 'last_error' => in_array( $status, [ 'error','pending' ], true ) ? $message : '', 'updated_at' => current_time( 'mysql' ) ], [ 'id' => (int) $row['id'] ], [ '%s','%s','%s' ], [ '%d' ] );
        return [ 'success' => true, 'message' => $message, 'status' => $status, 'order_id' => $order_id, 'mapped' => $mapped, 'attempted' => true, 'created' => ! in_array( $status, [ 'error','pending' ], true ) ];
    }

    private function ajax_integrations_rotate_token( object $user ): array {
        $wallet_user_id = senderzz_portal_wallet_user_id( $user );
        if ( ! $wallet_user_id ) return [ 'success' => false, 'message' => 'Carteira do produtor não localizada.' ];
        if ( ! function_exists( 'senderzz_int_get_or_create_for_user' ) ) return [ 'success' => false, 'message' => 'Módulo de integrações indisponível.' ];
        global $wpdb;
        $row = senderzz_int_get_or_create_for_user( (int) $wallet_user_id, (int) ( $user->id ?? 0 ) );
        $token = 'sz_' . wp_generate_password( 48, false, false );
        $wpdb->update( senderzz_int_table(), [ 'token' => $token, 'updated_at' => current_time( 'mysql' ) ], [ 'id' => (int) $row['id'], 'user_id' => (int) $wallet_user_id ], [ '%s','%s' ], [ '%d','%d' ] );
        return [ 'success' => true, 'message' => 'Token renovado.', 'url' => rest_url( 'senderzz/v1/integrations/' . rawurlencode( $token ) ), 'token' => $token ];
    }

    private function ajax_webhooks_list( object $user ): array {
        $wallet_user_id = senderzz_portal_wallet_user_id( $user );
        if ( ! $wallet_user_id ) return [ 'success' => false, 'message' => 'Carteira do usuário não localizada.' ];
        if ( function_exists( 'senderzz_pw_install_tables' ) ) senderzz_pw_install_tables();
        if ( function_exists( 'senderzz_pw_ensure_user_webhook_slots' ) ) senderzz_pw_ensure_user_webhook_slots( (int) $wallet_user_id );
        global $wpdb;
        $is_affiliate_pure = function_exists( 'sz_aff_portal_user_must_use_affiliate_scope' ) && sz_aff_portal_user_must_use_affiliate_scope( $user );
        $allowed_class_ids = [];
        if ( $is_affiliate_pure && function_exists( 'sz_aff_get_allowed_shipping_class_ids_for_portal_user' ) ) {
            $allowed_class_ids = sz_aff_get_allowed_shipping_class_ids_for_portal_user( $user );
        } else {
            $allowed_class_ids = function_exists('sz_get_user_class_ids') ? sz_get_user_class_ids($user) : [];
            if ( empty($allowed_class_ids) ) {
                $portal_class_id = (int)($user->shipping_class_id ?? 0);
                if ( $portal_class_id > 0 ) $allowed_class_ids = [ $portal_class_id ];
            }
        }
        $allowed_class_ids = array_values( array_unique( array_map( 'absint', array_filter( $allowed_class_ids ) ) ) );
        if ( empty( $allowed_class_ids ) ) {
            return [ 'success' => true, 'webhooks' => [], 'classes' => [], 'payload_exemplo' => function_exists( 'senderzz_pw_payload_example' ) ? senderzz_pw_payload_example() : [], 'message' => $is_affiliate_pure ? 'Nenhuma classe de entrega liberada nos checkouts dos produtores vinculados.' : 'Este usuário não está vinculado a nenhuma classe de entrega.' ];
        }
        foreach ( $allowed_class_ids as $cid ) {
            $exists = (int) $wpdb->get_var( $wpdb->prepare( "SELECT id FROM {$wpdb->prefix}senderzz_webhooks WHERE user_id=%d AND shipping_class_id=%d LIMIT 1", (int) $wallet_user_id, (int) $cid ) );
            if ( ! $exists ) {
                $wpdb->insert( $wpdb->prefix . 'senderzz_webhooks', [ 'user_id'=>(int)$wallet_user_id, 'portal_user_id'=>(int)$user->id, 'shipping_class_id'=>(int)$cid, 'url'=>'', 'secret'=>'', 'active'=>0, 'created_at'=>current_time('mysql'), 'updated_at'=>current_time('mysql') ], [ '%d','%d','%d','%s','%s','%d','%s','%s' ] );
            }
        }
        $rows = $wpdb->get_results( $wpdb->prepare(
            "SELECT * FROM {$wpdb->prefix}senderzz_webhooks WHERE user_id=%d AND shipping_class_id IN (" . implode( ',', array_map( 'absint', $allowed_class_ids ) ) . ") ORDER BY shipping_class_id ASC",
            (int) $wallet_user_id
        ), ARRAY_A ) ?: [];
        $rows = function_exists( 'senderzz_pw_enrich_rows' ) ? senderzz_pw_enrich_rows( $rows ) : $rows;
        $all_classes = function_exists( 'senderzz_pw_get_classes_for_select' ) ? senderzz_pw_get_classes_for_select() : [];
        $classes = array_values( array_filter( $all_classes, function( $c ) use ( $allowed_class_ids ) {
            return in_array( (int) ( $c['id'] ?? 0 ), $allowed_class_ids, true );
        } ) );
        return [
            'success' => true,
            'webhooks' => $rows,
            'classes' => $classes,
            'payload_exemplo' => function_exists( 'senderzz_pw_payload_example' ) ? senderzz_pw_payload_example() : [],
        ];
    }

    private function ajax_webhooks_save( object $user ): array {
        global $wpdb;
        $wallet_user_id = senderzz_portal_wallet_user_id( $user );
        if ( ! $wallet_user_id ) return [ 'success' => false, 'message' => 'Carteira do produtor não localizada.' ];
        if ( function_exists( 'senderzz_pw_install_tables' ) ) senderzz_pw_install_tables();
        if ( function_exists( 'senderzz_pw_ensure_user_webhook_slots' ) ) senderzz_pw_ensure_user_webhook_slots( (int) $wallet_user_id );
        $class_id = absint( $_POST['shipping_class_id'] ?? 0 );
        $is_affiliate_pure = function_exists( 'sz_aff_portal_user_must_use_affiliate_scope' ) && sz_aff_portal_user_must_use_affiliate_scope( $user );
        if ( $is_affiliate_pure && function_exists( 'sz_aff_get_allowed_shipping_class_ids_for_portal_user' ) ) {
            $allowed_class_ids = sz_aff_get_allowed_shipping_class_ids_for_portal_user( $user );
            if ( ! in_array( $class_id, array_map( 'absint', $allowed_class_ids ), true ) ) return [ 'success' => false, 'message' => 'Classe de entrega não liberada para seus checkouts de afiliado.' ];
        } else {
            $_wh_user_classes = function_exists('sz_get_user_class_ids') ? sz_get_user_class_ids($user) : [];
            if ( empty($_wh_user_classes) ) {
                $portal_class_id = (int)($user->shipping_class_id ?? 0);
                $_wh_user_classes = $portal_class_id > 0 ? [$portal_class_id] : [];
            }
            if ( empty($_wh_user_classes) ) return [ 'success' => false, 'message' => 'Este usuário não está vinculado a nenhuma classe de entrega.' ];
            if ( ! in_array( $class_id, $_wh_user_classes, true ) ) return [ 'success' => false, 'message' => 'Classe de entrega inválida para este usuário.' ];
        }
        $url = esc_url_raw( trim( (string) ( $_POST['url'] ?? '' ) ) );
        $secret = '';
        $active = ! empty( $_POST['active'] ) ? 1 : 0;
        // DT-CODE-02: filtro por tipo de evento; vazio = dispara para todos
        $event_types_raw = sanitize_text_field( wp_unslash( $_POST['event_types'] ?? '' ) );
        $event_types = '';
        if ( $event_types_raw ) {
            $decoded = json_decode( $event_types_raw, true );
            if ( is_array( $decoded ) ) {
                $allowed_events = [ 'order_status_enviado','order_status_entregue','order_status_cancelado','order_status_frustrado','order_status_em_rota','order_status_embalado' ];
                $filtered = array_values( array_intersect( array_map( 'sanitize_key', $decoded ), $allowed_events ) );
                $event_types = $filtered ? wp_json_encode( $filtered ) : '';
            }
        }
        if ( $url && ! wp_http_validate_url( $url ) ) return [ 'success' => false, 'message' => 'URL de destino inválida.' ];
        $existing = (int) $wpdb->get_var( $wpdb->prepare( "SELECT id FROM {$wpdb->prefix}senderzz_webhooks WHERE user_id=%d AND shipping_class_id=%d LIMIT 1", (int) $wallet_user_id, $class_id ) );
        $data = [
            'user_id'           => (int) $wallet_user_id,
            'portal_user_id'    => (int) $user->id,
            'shipping_class_id' => $class_id,
            'url'               => $url,
            'secret'            => $secret,
            'active'            => $url ? $active : 0,
            'event_types'       => $event_types,
            'updated_at'        => current_time( 'mysql' ),
        ];
        if ( $existing ) {
            $ok = $wpdb->update( $wpdb->prefix . 'senderzz_webhooks', $data, [ 'id' => $existing ], [ '%d','%d','%d','%s','%s','%d','%s','%s' ], [ '%d' ] );
        } else {
            $data['created_at'] = current_time( 'mysql' );
            $ok = $wpdb->insert( $wpdb->prefix . 'senderzz_webhooks', $data, [ '%d','%d','%d','%s','%s','%d','%s','%s','%s' ] );
        }
        if ( false === $ok ) return [ 'success' => false, 'message' => 'Erro ao salvar webhook.' ];
        return [ 'success' => true, 'message' => 'Webhook salvo.' ];
    }

    private function ajax_webhooks_test( object $user ): array {
        global $wpdb;
        $wallet_user_id = senderzz_portal_wallet_user_id( $user );
        $id = absint( $_POST['webhook_id'] ?? 0 );
        if ( ! $wallet_user_id || ! $id ) return [ 'success' => false, 'message' => 'Webhook inválido.' ];
        $is_affiliate_pure = function_exists( 'sz_aff_portal_user_must_use_affiliate_scope' ) && sz_aff_portal_user_must_use_affiliate_scope( $user );
        $class_filter_sql = '';
        if ( $is_affiliate_pure && function_exists( 'sz_aff_get_allowed_shipping_class_ids_for_portal_user' ) ) {
            $allowed_class_ids = array_values( array_unique( array_map( 'absint', sz_aff_get_allowed_shipping_class_ids_for_portal_user( $user ) ) ) );
            if ( empty( $allowed_class_ids ) ) return [ 'success' => false, 'message' => 'Nenhuma classe de entrega liberada para seus checkouts de afiliado.' ];
            $class_filter_sql = ' AND shipping_class_id IN (' . implode( ',', $allowed_class_ids ) . ')';
            $hook = $wpdb->get_row( $wpdb->prepare( "SELECT * FROM {$wpdb->prefix}senderzz_webhooks WHERE id=%d AND user_id=%d{$class_filter_sql} LIMIT 1", $id, (int) $wallet_user_id ), ARRAY_A );
        } else {
            $_wh_test_classes = function_exists('sz_get_user_class_ids') ? sz_get_user_class_ids($user) : [];
            if ( empty($_wh_test_classes) ) {
                $portal_class_id = (int)($user->shipping_class_id ?? 0);
                $_wh_test_classes = $portal_class_id > 0 ? [$portal_class_id] : [];
            }
            if ( empty($_wh_test_classes) ) return [ 'success' => false, 'message' => 'Este usuário não está vinculado a nenhuma classe de entrega.' ];
            $class_in_sql = '(' . implode(',', array_map('intval', $_wh_test_classes)) . ')';
            $hook = $wpdb->get_row( $wpdb->prepare( "SELECT * FROM {$wpdb->prefix}senderzz_webhooks WHERE id=%d AND user_id=%d AND shipping_class_id IN {$class_in_sql} LIMIT 1", $id, (int) $wallet_user_id ), ARRAY_A );
        }
        if ( ! $hook ) return [ 'success' => false, 'message' => 'Webhook não encontrado.' ];
        if ( empty( $hook['url'] ) ) return [ 'success' => false, 'message' => 'Informe a URL de destino antes de testar.' ];
        if ( ! function_exists( 'senderzz_pw_fire_url' ) || ! function_exists( 'senderzz_pw_payload_example' ) ) return [ 'success' => false, 'message' => 'Módulo de webhooks indisponível.' ];
        $result     = senderzz_pw_fire_url( $hook, senderzz_pw_payload_example(), 0, 'teste_webhook' );
        $ok         = ! empty( $result['ok'] );
        $code       = (int) ( $result['code'] ?? 0 );
        $body       = (string) ( $result['body'] ?? '' );
        $error      = (string) ( $result['error'] ?? '' );
        $dispatched = $code > 0;

        return [
            'success'    => true,
            'dispatched' => $dispatched,
            'http_ok'    => $ok,
            'code'       => $code,
            'body'       => $body,
            'message'    => $dispatched
                ? ( $ok
                    ? 'Payload entregue com sucesso. HTTP ' . $code . '.'
                    : 'Payload entregue, mas o destino retornou HTTP ' . $code . '. O endpoint recebeu os dados mas rejeitou por validação própria — isso não é um erro do Senderzz.' )
                : ( $error ?: 'Não foi possível conectar ao endpoint. Verifique a URL.' ),
        ];
    }

    private function ajax_webhooks_delete( object $user ): array {
        global $wpdb;
        $wallet_user_id = senderzz_portal_wallet_user_id( $user );
        $id = absint( $_POST['webhook_id'] ?? 0 );
        if ( ! $wallet_user_id || ! $id ) return [ 'success' => false, 'message' => 'Webhook inválido.' ];
        // Soft-delete: limpa url e desativa — hard-delete faz o slot ser recriado pelo cron de slots.
        $updated = $wpdb->update(
            $wpdb->prefix . 'senderzz_webhooks',
            [ 'url' => '', 'secret' => '', 'active' => 0, 'updated_at' => current_time( 'mysql' ) ],
            [ 'id' => $id, 'user_id' => (int) $wallet_user_id ],
            [ '%s', '%s', '%d', '%s' ],
            [ '%d', '%d' ]
        );
        if ( $updated === false ) return [ 'success' => false, 'message' => 'Webhook não encontrado ou sem permissão.' ];
        return [ 'success' => true, 'message' => 'Webhook excluído.' ];
    }

    private function ajax_change_email( object $user ): array {
        global $wpdb;
        $new_email = sanitize_email( $_POST['new_email'] ?? '' );
        if ( ! is_email( $new_email ) ) return [ 'success' => false, 'message' => 'E-mail inválido.' ];
        $taken = $wpdb->get_var( $wpdb->prepare(
            "SELECT id FROM {$wpdb->prefix}senderzz_portal_users WHERE email=%s AND id<>%d",
            $new_email, (int) $user->id
        ) );
        if ( $taken ) return [ 'success' => false, 'message' => 'Este e-mail já está em uso.' ];

        $old_email = sanitize_email( $user->email ?? '' );
        $wallet_user_id = senderzz_portal_wallet_user_id( $user );
        if ( $wallet_user_id && function_exists( 'senderzz_ensure_tpc_wallet' ) ) senderzz_ensure_tpc_wallet( (int) $wallet_user_id );

        $ok = $wpdb->update(
            $wpdb->prefix . 'senderzz_portal_users',
            [ 'email' => $new_email, 'wp_user_id' => $wallet_user_id ?: null ],
            [ 'id' => (int) $user->id ],
            [ '%s','%d' ],
            [ '%d' ]
        );
        if ( false === $ok ) return [ 'success' => false, 'message' => 'Erro ao atualizar e-mail do portal.' ];

        if ( $wallet_user_id ) {
            update_user_meta( (int) $wallet_user_id, '_senderzz_previous_emails', array_values( array_unique( array_filter( [
                strtolower( $old_email ), strtolower( $new_email ),
            ] ) ) ) );
            $taken_wp = get_user_by( 'email', $new_email );
            if ( ! $taken_wp || (int) $taken_wp->ID === (int) $wallet_user_id ) {
                $updated = wp_update_user( [ 'ID' => (int) $wallet_user_id, 'user_email' => $new_email ] );
                if ( is_wp_error( $updated ) ) {
                    update_user_meta( (int) $wallet_user_id, '_senderzz_wp_email_sync_error', $updated->get_error_message() );
                } else {
                    clean_user_cache( (int) $wallet_user_id );
                }
            } else {
                update_user_meta( (int) $wallet_user_id, '_senderzz_wp_email_sync_error', 'Novo e-mail já existe em outro usuário WP. Login do portal mantido no novo e-mail; carteira mantida por ID.' );
            }
            if ( function_exists( 'senderzz_wallet_rebuild_from_transactions' ) ) senderzz_wallet_rebuild_from_transactions( (int) $wallet_user_id );
        }

        $wpdb->delete( $wpdb->prefix . 'senderzz_portal_sessions', [ 'user_id' => (int) $user->id ] );
        return [ 'success' => true, 'message' => 'E-mail alterado. Faça login novamente com o novo e-mail.' ];
    }

    private function ajax_change_password( object $user ): array {
        $current_pw = sanitize_text_field( $_POST['current_password'] ?? '' );
        $new_pw     = sanitize_text_field( $_POST['new_password'] ?? '' );

        if ( strlen( $new_pw ) < 10 || ! preg_match( '/[A-Za-z]/', $new_pw ) || ! preg_match( '/[0-9]/', $new_pw ) ) {
    return [ 'success' => false, 'message' => 'Nova senha deve ter no mínimo 10 caracteres com letras e números.' ];
}

        // Verify current password
        global $wpdb;
        $stored = $wpdb->get_var( $wpdb->prepare(
            "SELECT password_hash FROM {$wpdb->prefix}senderzz_portal_users WHERE id = %d",
            (int) $user->id
        ) );
        if ( ! wp_check_password( $current_pw, $stored ) ) {
            return [ 'success' => false, 'message' => 'Senha atual incorreta.' ];
        }

        Portal_Auth::change_password( (int) $user->id, $new_pw );
        // Revoke other sessions
        global $wpdb;
        $wpdb->delete( $wpdb->prefix . 'senderzz_portal_sessions', [ 'user_id' => (int) $user->id ] );
        setcookie( 'senderzz_portal_session', '', time() - 3600, '/', '', is_ssl(), true );

        return [ 'success' => true, 'message' => 'Senha alterada. Faça login novamente.' ];
    }

    public function handle_ajax(): void {
        // Garante JSON limpo no admin-ajax: remove qualquer CSS/HTML vazado por hooks do portal.
        // Isso evita respostas quebradas em Webhooks/Conexões quando algum módulo imprime <style> antes do JSON.
        if ( defined( 'DOING_AJAX' ) && DOING_AJAX ) {
            while ( ob_get_level() > 0 ) { @ob_end_clean(); }
            ob_start();
        }
        $ac = sanitize_text_field( wp_unslash( $_POST['szaction'] ?? '' ) );

        // v159: não deixar o WordPress matar o AJAX com "-1" quando o nonce da página fica velho/cacheado.
        // Primeiro tenta o nonce normal. Se falhar, aceita somente ações do portal com sessão válida
        // e origem/referer do próprio site. Assim o front recebe JSON real e o fluxo não quebra.
        // Cadastro público: não exige sessão, apenas nonce básico
        if ( $ac === 'portal_register' ) {
            check_ajax_referer( 'senderzz_portal', '_ajax_nonce' );
            $r = $this->ajax_portal_register();
            wp_send_json( array_merge( [ 'success' => false ], $r ) );
            return;
        }

        $nonce_ok = check_ajax_referer( 'senderzz_portal', '_ajax_nonce', false );
        if ( ! $nonce_ok ) {
            $portal_user = Portal_Auth::get_current_user();
            $host = (string) wp_parse_url( home_url(), PHP_URL_HOST );
            $origin_host  = ! empty( $_SERVER['HTTP_ORIGIN'] )  ? (string) wp_parse_url( sanitize_text_field( wp_unslash( $_SERVER['HTTP_ORIGIN'] ) ), PHP_URL_HOST )  : '';
            $referer_host = ! empty( $_SERVER['HTTP_REFERER'] ) ? (string) wp_parse_url( esc_url_raw( wp_unslash( $_SERVER['HTTP_REFERER'] ) ), PHP_URL_HOST ) : '';

            // S11: same-site check mais estrito — se Origin presente deve bater; se ausente, Referer deve estar presente e bater.
            // Aceitar Origin vazio + Referer vazio = nenhuma evidência de same-site (suspeito).
            if ( $origin_host !== '' ) {
                $same_site = $origin_host === $host;
            } else {
                $same_site = $referer_host !== '' && $referer_host === $host;
            }

            // No fallback sem nonce, apenas ações de leitura de baixo impacto são permitidas.
            // list_sessions expõe sessões ativas do usuário — requer nonce (removido do fallback).
            $session_allowed_actions = [
                'checkout_link_affiliate_toggle', // toggle visibilidade — baixo impacto
                'get_variations',                 // leitura
                'get_report',                     // leitura: Relatórios
                'webhooks_list',                  // leitura: Conexões
                'webhooks_log',                   // leitura: histórico de Conexões
                'get_sender',                     // leitura: dados do remetente
            ];

            if ( ! $portal_user || ! $same_site || ! in_array( $ac, $session_allowed_actions, true ) ) {
                wp_send_json_error( [
                    'message' => 'Sessão expirada ou token inválido. Recarregue a página e tente novamente.',
                    'code'    => 'invalid_nonce',
                ], 403 );
                return;
            }
        }

        switch($ac){
            case 'login_step1':
                $r=Portal_Auth::login_step1(sanitize_email($_POST['email']??''),$_POST['password']??''); break;
            case 'login_step2':
                $r=Portal_Auth::login_step2(sanitize_text_field($_POST['temp_token']??''),sanitize_text_field($_POST['code']??''),(int)($_POST['remember']??7));
                // Append role to response using the just-created session token.
                if ( ! empty( $r['success'] ) ) {
                    global $wpdb;
                    $token = sanitize_text_field( $r['session_token'] ?? '' );
                    if ( $token ) {
                        $u = $wpdb->get_row( $wpdb->prepare(
                            "SELECT u.role FROM {$wpdb->prefix}senderzz_portal_users u
                             INNER JOIN {$wpdb->prefix}senderzz_portal_sessions s ON s.user_id = u.id
                             WHERE s.token IN (%s, %s) LIMIT 1",
                            $token, \WC_MelhorEnvio\Portal\Portal_Auth::hash_session_token( $token )
                        ) );
                        $r['role'] = $u ? ( $u->role ?: 'client' ) : 'client';
                    }
                }
                break;
            case 'request_password_reset':
                $r=Portal_Auth::request_password_reset(sanitize_email($_POST['email']??'')); break;
            case 'complete_password_reset':
                $r=Portal_Auth::complete_password_reset(sanitize_text_field($_POST['token']??''),sanitize_text_field($_POST['new_password']??'')); break;
            case 'resend_2fa':
                // Reenvio do código 2FA — gera novo código para o temp_token existente
                $r=Portal_Auth::resend_2fa_code(sanitize_text_field($_POST['temp_token']??'')); break;
            case 'approve':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                $oid=(int)$_POST['order_id'];
                if(function_exists('senderzz_user_can_access_order') && !senderzz_user_can_access_order($oid,$u,'approve')){wp_send_json_error(['message'=>'Sem permissão para aprovar este pedido.']);return;}
                $r=Portal_Orders::approve_order($oid, function_exists('sz_get_user_class_ids') ? sz_get_user_class_ids($u) : (int)$u->shipping_class_id); break;
            case 'cancel':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                $oid=(int)$_POST['order_id'];
                if(function_exists('senderzz_user_can_access_order') && !senderzz_user_can_access_order($oid,$u,'cancel')){wp_send_json_error(['message'=>'Sem permissão para cancelar este pedido.']);return;}
                $r=Portal_Orders::cancel_order($oid, function_exists('sz_get_user_class_ids') ? sz_get_user_class_ids($u) : (int)$u->shipping_class_id); break;
            case 'retry':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                $oid=(int)$_POST['order_id'];
                if(function_exists('senderzz_user_can_access_order') && !senderzz_user_can_access_order($oid,$u,'edit')){wp_send_json_error(['message'=>'Sem permissão para reprocessar este pedido.']);return;}
                $r=Portal_Orders::retry_label($oid, function_exists('sz_get_user_class_ids') ? sz_get_user_class_ids($u) : (int)$u->shipping_class_id); break;
            case 'bulk_action':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                $action=sanitize_text_field($_POST['bulk_action']??'');
                $ids=array_map('intval',explode(',',(string)($_POST['order_ids']??'')));
                $ids=array_filter($ids,fn($id)=>$id>0);
                if(function_exists('senderzz_user_can_access_order')){ $ids=array_values(array_filter($ids,fn($id)=>senderzz_user_can_access_order((int)$id,$u,'edit'))); }
                if(empty($ids)){wp_send_json_error(['message'=>'Nenhum pedido permitido selecionado.']);return;}
                if($action==='loss'){$ok=[];$fail=[];foreach($ids as $id){$rr=Portal_Orders::report_loss((int)$id, function_exists('sz_get_user_class_ids') ? sz_get_user_class_ids($u) : (int)$u->shipping_class_id);if(!empty($rr['success']))$ok[]=$id;else $fail[]=['id'=>$id,'reason'=>$rr['message']??'Erro'];}$r=['success'=>count($ok)>0,'message'=>count($ok).' pedido(s) processado(s) com sucesso.'.(count($fail)?' '.count($fail).' pedido(s) com erro.':''),'detail'=>['success'=>$ok,'failed'=>$fail]];}else{$r=Portal_Orders::bulk_action($action,$ids, function_exists('sz_get_user_class_ids') ? sz_get_user_class_ids($u) : (int)$u->shipping_class_id);} break;
            case 'toggle_2fa':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                $require=(int)($_POST['require']??1)===1;
                $ok=Portal_Auth::set_require_2fa((int)$u->id,$require);
                $r=['success'=>$ok,'message'=>$ok?($require?'2FA ativado.':'2FA desativado. Próximos logins não pedirão código.') :'Erro ao salvar preferência.']; break;
            case 'list_sessions':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                global $wpdb;
                $sessions=$wpdb->get_results($wpdb->prepare(
                    "SELECT id,created_at,expires_at,SUBSTRING(token,1,8) as token_preview FROM {$wpdb->prefix}senderzz_portal_sessions WHERE user_id=%d AND expires_at>NOW() ORDER BY created_at DESC LIMIT 10",
                    (int)$u->id
                ),ARRAY_A);
                $current_token=sanitize_text_field($_COOKIE['senderzz_portal_session']??'');
                foreach($sessions as &$s){ $s['current']=($wpdb->get_var($wpdb->prepare("SELECT token FROM {$wpdb->prefix}senderzz_portal_sessions WHERE id=%d",(int)$s['id']))===$current_token)?1:0; unset($s['token_preview']); $s['created_at_fmt']=wp_date('d/m/Y H:i',$s['created_at']?strtotime($s['created_at']):0); }
                $r=['success'=>true,'sessions'=>array_values($sessions)]; break;
            case 'revoke_all_sessions':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                global $wpdb;
                $current_token=sanitize_text_field($_COOKIE['senderzz_portal_session']??'');
                $wpdb->query($wpdb->prepare("DELETE FROM {$wpdb->prefix}senderzz_portal_sessions WHERE user_id=%d AND token NOT IN (%s,%s)",(int)$u->id,$current_token,\WC_MelhorEnvio\Portal\Portal_Auth::hash_session_token($current_token)));
                $r=['success'=>true,'message'=>'Todas as outras sessões foram encerradas.']; break;
            case 'suspend':
            case 'loss':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                $oid=(int)$_POST['order_id'];
                if(function_exists('senderzz_user_can_access_order') && !senderzz_user_can_access_order($oid,$u,'edit')){wp_send_json_error(['message'=>'Sem permissão para este pedido.']);return;}
                $r=Portal_Orders::report_loss($oid, function_exists('sz_get_user_class_ids') ? sz_get_user_class_ids($u) : (int)$u->shipping_class_id); break;
            case 'support':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                $oid=(int)$_POST['order_id'];
                if(function_exists('senderzz_user_can_access_order') && !senderzz_user_can_access_order($oid,$u,'view')){wp_send_json_error(['message'=>'Sem permissão para este pedido.']);return;}
                $reason=(string)($_POST['reason']??'');
                $r=Portal_Orders::request_support($oid, function_exists('sz_get_user_class_ids') ? sz_get_user_class_ids($u) : (int)$u->shipping_class_id, $reason); break;
            case 'generate_pix':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                $amt=floatval($_POST['amount']??0);if($amt<10){wp_send_json_error(['message'=>'Valor mínimo R$ 10,00.']);return;}
                if(!function_exists('tpc_criar_recarga_pendente')||!function_exists('tpc_gerar_pix_melhor_envio')){wp_send_json_error(['message'=>'Indisponível.']);return;}
                $rid=tpc_criar_recarga_pendente((int)senderzz_portal_wallet_user_id($u),$amt,'Recarga via painel');
                if(!$rid){wp_send_json_error(['message'=>'Erro ao criar recarga.']);return;}
                $pix=tpc_gerar_pix_melhor_envio($rid);
                if(is_wp_error($pix)){wp_send_json_error(['message'=>$pix->get_error_message()]);return;}
                wp_send_json_success(['copia_cola'=>$pix['copia_cola']??'','qr_src'=>$pix['qr_src']??'']); return;
            case 'check_pix':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                $rid=absint($_POST['recarga_id']??0);if(!$rid){wp_send_json_error(['message'=>'ID de recarga inválido.']);return;}
                // Verify the recarga belongs to this user before confirming
                global $wpdb;
                $uid_chk=(int)senderzz_portal_wallet_user_id($u);
                $recarga=$wpdb->get_row($wpdb->prepare("SELECT * FROM {$wpdb->prefix}tpc_recargas WHERE id=%d AND user_id=%d LIMIT 1",$rid,$uid_chk),ARRAY_A);
                if(!$recarga){wp_send_json_error(['message'=>'Recarga não encontrada.']);return;}
                if(($recarga['status']??'')===('confirmado'||'paid')){wp_send_json_success(['message'=>'Recarga já confirmada!','confirmed'=>true]);return;}
                // Try to confirm via webhook-style call
                if(function_exists('tpc_confirmar_recarga')){
                    $ok=tpc_confirmar_recarga($rid,'portal_check');
                    if($ok){wp_send_json_success(['message'=>'Pagamento confirmado! Seu saldo foi atualizado.','confirmed'=>true]);return;}
                }
                // Poll status from payment provider
                if(function_exists('tpc_verificar_status_pix')){
                    $status=tpc_verificar_status_pix($rid);
                    if($status&&in_array($status,['paid','approved','confirmed'],true)){
                        wp_send_json_success(['message'=>'PIX confirmado! Saldo atualizado.','confirmed'=>true]);return;
                    }
                }
                wp_send_json_error(['message'=>'Pagamento ainda não confirmado. Aguarde alguns instantes.','confirmed'=>false]); return;
            case 'webhooks_delete':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                $wh_id=absint($_POST['webhook_id']??0);if(!$wh_id){wp_send_json_error(['message'=>'ID inválido.']);return;}
                global $wpdb;
                $wh_wuid=(int)senderzz_portal_wallet_user_id($u);
                $deleted=$wpdb->delete($wpdb->prefix.'senderzz_webhooks',['id'=>$wh_id,'user_id'=>$wh_wuid],['%d','%d']);
                if($deleted){wp_send_json_success(['message'=>'Webhook excluído.']);}
                else{wp_send_json_error(['message'=>'Webhook não encontrado ou sem permissão.']);}
                return;
            case 'set_preferred_carrier':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                if(!empty($u->parent_user_id)){wp_send_json_error(['message'=>'Sem permissão.']);return;}
                $raw = sanitize_text_field($_POST['method_ids'] ?? '');
                $ids = $raw === '' ? [] : array_map('absint', explode(',', $raw));
                $_sz_primary_class = function_exists('sz_get_user_class_ids') ? (sz_get_user_class_ids($u)[0] ?? (int)$u->shipping_class_id) : (int)$u->shipping_class_id;
                $r=$this->set_preferred_carrier_ids($_sz_primary_class, $ids); break;
            case 'set_blocked_carrier':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                if(!empty($u->parent_user_id)){wp_send_json_error(['message'=>'Sem permissão.']);return;}
                $raw = sanitize_text_field($_POST['method_ids'] ?? '');
                $ids = $raw === '' ? [] : array_map('absint', explode(',', $raw));
                $_sz_primary_class = function_exists('sz_get_user_class_ids') ? (sz_get_user_class_ids($u)[0] ?? (int)$u->shipping_class_id) : (int)$u->shipping_class_id;
                $r=$this->set_blocked_carrier_ids($_sz_primary_class, $ids); break;
            case 'get_sender':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                $cid= function_exists('sz_get_user_class_ids') ? (sz_get_user_class_ids($u)[0] ?? (int)($u->shipping_class_id ?? 0)) : (int)($u->shipping_class_id ?? 0);

                // 1) Tenta dados específicos da classe do cliente
                if($cid && function_exists('senderzz_sender_get_for_class')){
                    $row=senderzz_sender_get_for_class($cid);
                } else {
                    $map=get_option('senderzz_sender_by_class',[]);
                    $r0=is_array($map[$cid]??null)?$map[$cid]:[];
                    $row=[
                        'name'=>$r0['name']??'',
                        'document'=>$r0['document']??'',
                        'telephone'=>$r0['telephone']??'',
                    ];
                }

                // 2) Fallback: se algum campo estiver vazio, usa os dados globais do Melhor Envio
                if(empty($row['name']) || empty($row['document']) || empty($row['telephone'])){
                    $global = get_option('woocommerce_wc-melhor-envio_settings', []);
                    $g_addr = is_array($global['address'] ?? null) ? $global['address'] : [];
                    if(empty($row['name']))      $row['name']      = (string)($g_addr['name'] ?? '');
                    if(empty($row['document'])){
                        // ME pode usar 'document' (CPF) ou 'company_document' (CNPJ)
                        $row['document'] = (string)($g_addr['document'] ?? $g_addr['company_document'] ?? '');
                    }
                    if(empty($row['telephone'])) $row['telephone'] = (string)($g_addr['phone'] ?? '');
                }

                // Histórico NÃO é exposto ao cliente — visível apenas no admin WP.
                wp_send_json_success([
                    'name'      => (string)$row['name'],
                    'document'  => (string)$row['document'],
                    'telephone' => (string)$row['telephone'],
                ]); return;
            case 'save_sender':
                // Bloqueado para o cliente — alterações somente via admin/suporte.
                wp_send_json_error(['message'=>'Os dados do remetente são gerenciados pelo administrador. Para alterar, abra um chamado no menu Suporte.']); return;
            case 'create_link':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                $r=$this->ajax_create_link($u); break;
            case 'delete_link':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                $r=$this->ajax_delete_link($u); break;
            case 'checkout_link_generate':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                if(!empty($u->parent_user_id)){wp_send_json_error(['message'=>'Sem permissão.']);return;}
                $r=$this->ajax_checkout_link_generate($u); break;
            case 'checkout_link_delete':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                $r=$this->ajax_checkout_link_delete($u); break;
            case 'checkout_link_affiliate_toggle':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                if(!empty($u->parent_user_id)){wp_send_json_error(['message'=>'Sem permissão.']);return;}
                $r=$this->ajax_checkout_link_affiliate_toggle($u); break;
            case 'checkout_link_commission_update':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                if(!empty($u->parent_user_id)){wp_send_json_error(['message'=>'Sem permissão.']);return;}
                $r=$this->ajax_checkout_link_commission_update($u); break;
            case 'get_variations':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                $pid=absint($_POST['product_id']??0);
                $product=wc_get_product($pid);
                $vars=[];
                if($product&&$product->is_type('variable')){
                    foreach($product->get_available_variations() as $v){
                        $vid=$v['variation_id'];$vp=wc_get_product($vid);
                        if(!$vp)continue;
                        $attrs=array_filter($v['attributes']);
                        $label=implode(' / ',array_map(function($k,$val){return wc_attribute_label(str_replace('attribute_','',$k)).': '.$val;},$attrs,array_keys($attrs)));
                        $vars[]=['id'=>$vid,'label'=>$label?:('#'.$vid)];
                    }
                }
                wp_send_json_success(['variations'=>$vars]);return;
            case 'create_sub_user':
                $u=Portal_Auth::get_current_user();if(!$u||!empty($u->parent_user_id)){wp_send_json_error(['message'=>'Sem permissão.']);return;}
                $r=$this->ajax_create_sub_user($u); break;
            case 'delete_sub_user':
                $u=Portal_Auth::get_current_user();if(!$u||!empty($u->parent_user_id)){wp_send_json_error(['message'=>'Sem permissão.']);return;}
                $r=$this->ajax_delete_sub_user($u); break;
            // refresh_orders removido — V2 orders.php tem implementação própria.

            case 'get_report':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                $fr=sanitize_text_field($_POST['date_from']??'');
                $to=sanitize_text_field($_POST['date_to']??'');
                $st=sanitize_text_field($_POST['status']??'');
                $mode=sanitize_text_field($_POST['mode']??'motoboy');
                $orders=$this->get_visible_orders_for_user($u,200);
                $orders=$this->filter_report_orders_by_mode($orders,$mode);
                if($fr)$orders=array_filter($orders,fn($o)=>($o['date_machine'] ?? $o['date'])>=$fr);
                if($to)$orders=array_filter($orders,fn($o)=>($o['date_machine'] ?? $o['date'])<=$to.' 23:59:59');
                if($st)$orders=array_filter($orders,fn($o)=>$this->report_status_matches((array)$o,$st));
                wp_send_json_success(['html'=>$this->render_reports(array_values($orders))]); return;
            case 'export_csv':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                $r=$this->ajax_export_csv($u); break;
            case 'change_email':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                $r=$this->ajax_change_email($u); break;
            case 'change_password':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                $r=$this->ajax_change_password($u); break;
            case 'integrations_toggle':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                if(function_exists('senderzz_int_get_or_create_for_user')){
                    global $wpdb; $wuid=senderzz_portal_wallet_user_id($u);
                    if($wuid){ $row=senderzz_int_get_or_create_for_user((int)$wuid,(int)($u->id??0));
                        $key=sanitize_key($_POST['key']??''); $val=(int)($_POST['value']??0);
                        $allowed=['active','paused','require_paid','ignore_duplicates','auto_cheapest'];
                        if(in_array($key,$allowed,true)){ $wpdb->update(senderzz_int_table(),[$key=>$val,'updated_at'=>current_time('mysql')],['id'=>(int)$row['id'],'user_id'=>(int)$wuid]); }
                    }
                }
                $r=['success'=>true]; break;
            case 'integrations_get':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                $r=$this->ajax_integrations_get($u); break;
            case 'integrations_save':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                $r=$this->ajax_integrations_save($u); break;
            case 'integrations_reprocess':
                @file_put_contents(WP_CONTENT_DIR.'/senderzz-logs/integration-reprocess-debug.log', date('[Y-m-d H:i:s] ').'DISPATCH integrations_reprocess chegou.'.PHP_EOL, FILE_APPEND);
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                $r=$this->ajax_integrations_reprocess($u); break;
            case 'integrations_rotate_token':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                $r=$this->ajax_integrations_rotate_token($u); break;
            case 'integrations_clear_logs':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                {global $wpdb; $wuid=(int)senderzz_portal_wallet_user_id($u);
                if($wuid){$lt=$wpdb->prefix.'senderzz_integration_log';
                if($wpdb->get_var($wpdb->prepare('SHOW TABLES LIKE %s',$lt))===$lt){$wpdb->delete($lt,['user_id'=>$wuid],['%d']);}}}
                $r=['success'=>true,'data'=>['message'=>'Logs de integração apagados.']]; break;
            case 'webhooks_clear_history':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                {global $wpdb; $wuid2=(int)senderzz_portal_wallet_user_id($u);
                if($wuid2){$lt2=$wpdb->prefix.'senderzz_webhook_log';$wt2=$wpdb->prefix.'senderzz_webhooks';
                if($wpdb->get_var($wpdb->prepare('SHOW TABLES LIKE %s',$lt2))===$lt2){
                    $wids=$wpdb->get_col($wpdb->prepare("SELECT id FROM {$wt2} WHERE user_id=%d",$wuid2));
                    if(!empty($wids)){$ph=implode(',',array_fill(0,count($wids),'%d'));$wpdb->query($wpdb->prepare("DELETE FROM {$lt2} WHERE webhook_id IN ({$ph})",...$wids));}}}}
                $r=['success'=>true,'data'=>['message'=>'Histórico de webhooks limpo.']]; break;
            case 'webhooks_list':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                $r=$this->ajax_webhooks_list($u); break;
            case 'webhooks_save':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                $r=$this->ajax_webhooks_save($u); break;
            case 'webhooks_test':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                $r=$this->ajax_webhooks_test($u); break;
            case 'webhooks_log':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                $r=$this->ajax_webhooks_log($u); break;
            case 'webhooks_resend':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                $r=$this->ajax_webhooks_resend($u); break;
            case 'save_vitrine_description':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                $pid  = absint($_POST['product_id'] ?? 0);
                $desc = sanitize_textarea_field(wp_unslash($_POST['description'] ?? ''));
                if (!$pid) { wp_send_json_error(['message'=>'Produto inválido.']); return; }
                // Valida que o produto pertence ao produtor (via shipping_class)
                $prod = function_exists('wc_get_product') ? wc_get_product($pid) : null;
                if (!$prod) { wp_send_json_error(['message'=>'Produto não encontrado.']); return; }
                update_post_meta($pid, '_sz_vitrine_description', $desc);
                wp_send_json_success(['message'=>'Descrição salva.']);
                return;
            case 'portal_register':
                $r=$this->ajax_portal_register(); break;
            case 'affiliate_action':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                $r=$this->ajax_affiliate_action($u); break;
            // ── Suporte / Tickets ────────────────────────────────────────────
            case 'tickets_list':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                $r=$this->ajax_tickets_list($u); break;
            case 'ticket_create':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                $r=$this->ajax_ticket_create($u); break;
            case 'ticket_msgs':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                $r=$this->ajax_ticket_msgs($u); break;
            case 'ticket_send_msg':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                $r=$this->ajax_ticket_send_msg($u); break;
            case 'ticket_close':
                $u=Portal_Auth::get_current_user();if(!$u){wp_send_json_error(['message'=>'Não autenticado.']);return;}
                $r=$this->ajax_ticket_close($u); break;
            // ── /Suporte / Tickets ───────────────────────────────────────────
            default: wp_send_json_error(['message'=>'Ação inválida.']); return;
        }
        // Dispara audit hook para ações relevantes
        if ( isset( $u ) && $u && isset( $ac ) ) {
            do_action( 'senderzz_portal_action', $ac, (int) ( $u->id ?? $u->ID ?? $u->user_id ?? 0 ), $r ?? [], $_POST );
        }

        // Antes de enviar, limpa novamente qualquer saída acidental produzida durante a ação.
        if ( defined( 'DOING_AJAX' ) && DOING_AJAX && ob_get_level() > 0 ) { @ob_clean(); }
        if(isset($r['success'])&&$r['success']) wp_send_json_success($r);
        else wp_send_json_error($r);
    }

    private function ajax_create_link( object $user ): array {
        global $wpdb;
        $this->ensure_links_table();
        $name  = sanitize_text_field($_POST['name']??'');
        $email = sanitize_email($_POST['email']??'');
        $days      = absint($_POST['expires_days']??0);
        $max_uses  = absint($_POST['max_uses']??0) ?: null;
        $token = bin2hex(random_bytes(20));
        $exp   = $days>0 ? gmdate('Y-m-d H:i:s',time()+$days*DAY_IN_SECONDS) : null;
        $wpdb->insert($wpdb->prefix.'senderzz_portal_links',[
            'user_id'=>$user->id,'shipping_class_id'=>$user->shipping_class_id, 'shipping_class_ids'=> function_exists('sz_get_user_class_ids') ? sz_get_user_class_ids($user) : [(int)$user->shipping_class_id],
            'token'=>$token,'name'=>$name,'email'=>$email,'expires_at'=>$exp,'max_uses'=>$max_uses,'use_count'=>0
        ]);
        if(!$wpdb->insert_id) return ['success'=>false,'message'=>'Erro ao criar link.'];
        $portal_url = get_permalink(get_option('senderzz_portal_page_id')) ?: home_url('/meus-pedidos/');
        $url = $portal_url.'?szlink='.urlencode($token);
        return ['success'=>true,'url'=>$url,'token'=>$token];
    }

    private function ajax_delete_link( object $user ): array {
        global $wpdb;
        $id = absint($_POST['link_id']??0);
        $wpdb->delete($wpdb->prefix.'senderzz_portal_links',['id'=>$id,'user_id'=>$user->id]);
        return ['success'=>true,'message'=>'Link excluído.'];
    }

    private function ajax_create_sub_user( object $owner ): array {
        global $wpdb;
        $email = sanitize_email($_POST['email']??'');
        $pw    = sanitize_text_field( $_POST['password'] ?? '' );
        $name  = sanitize_text_field($_POST['name']??'');
        $perms = sanitize_text_field($_POST['permissions']??'{}');
        if(strlen($pw)<8) return ['success'=>false,'message'=>'Senha mínima 8 caracteres.'];
        $table = $wpdb->prefix.Portal_Auth::TABLE;
        $exists = $wpdb->get_var($wpdb->prepare("SELECT id FROM {$table} WHERE email=%s",$email));
        if($exists) return ['success'=>false,'message'=>'Email já cadastrado.'];
        $wpdb->insert($table,[
            'email'=>$email,'password_hash'=>wp_hash_password($pw),
            'shipping_class_id'=>$owner->shipping_class_id, 'shipping_class_ids'=> function_exists('sz_get_user_class_ids') ? sz_get_user_class_ids($owner) : [(int)$owner->shipping_class_id],
            'name'=>$name,
            'parent_user_id'=>$owner->id,
            'permissions'=>$perms,
            'status'=>'active'
        ]);
        return $wpdb->insert_id ? ['success'=>true,'message'=>'Acesso criado.'] : ['success'=>false,'message'=>'Erro ao criar.'];
    }

    private function ajax_delete_sub_user( object $owner ): array {
        global $wpdb;
        $id = absint($_POST['user_id']??0);
        $table = $wpdb->prefix.Portal_Auth::TABLE;
        $check = $wpdb->get_var($wpdb->prepare("SELECT parent_user_id FROM {$table} WHERE id=%d",$id));
        if((int)$check!==(int)$owner->id) return ['success'=>false,'message'=>'Sem permissão.'];
        $wpdb->delete($table,['id'=>$id]);
        return ['success'=>true,'message'=>'Acesso excluído.'];
    }


    private function get_visible_orders_for_user( object $user, int $limit = 200 ): array {
        if ( function_exists( 'senderzz_get_visible_orders_for_user' ) ) {
            return senderzz_get_visible_orders_for_user( $user, $limit );
        }
        if ( function_exists( 'sz_aff_portal_user_must_use_affiliate_scope' ) && sz_aff_portal_user_must_use_affiliate_scope( $user ) && function_exists( 'sz_aff_get_affiliate_order_ids_for_portal_user' ) ) {
            $orders = [];
            foreach ( sz_aff_get_affiliate_order_ids_for_portal_user( $user, $limit ) as $order_id ) {
                $order = function_exists( 'wc_get_order' ) ? wc_get_order( (int) $order_id ) : null;
                if ( $order ) $orders[] = Portal_Orders::format_order( $order );
            }
            return $orders;
        }
        return Portal_Orders::get_orders( function_exists('sz_get_user_class_ids') ? sz_get_user_class_ids($user) : (int)$user->shipping_class_id, 1, $limit );
    }

    private function ajax_export_csv( object $user ): array {
        $fr = sanitize_text_field( $_POST['date_from'] ?? '' );
        $to = sanitize_text_field( $_POST['date_to'] ?? '' );
        $st = sanitize_text_field( $_POST['status'] ?? '' );
        $mode = sanitize_text_field( $_POST['mode'] ?? 'motoboy' );

        $orders = $this->get_visible_orders_for_user( $user, 500 );
        $orders = $this->filter_report_orders_by_mode( $orders, $mode );

        if ( $fr ) {
            $orders = array_filter( $orders, fn( $o ) => ( $o['date_machine'] ?? $o['date'] ) >= $fr );
        }

        if ( $to ) {
            $orders = array_filter( $orders, fn( $o ) => ( $o['date_machine'] ?? $o['date'] ) <= $to . ' 23:59:59' );
        }

        if ( $st ) {
            $orders = array_filter( $orders, fn( $o ) => $this->report_status_matches( (array) $o, $st ) );
        }

        // Limpa texto para CSV: remove HTML/entities, tags e espaços duplicados.
        $csv_clean = static function( $value ): string {
            if ( is_array( $value ) || is_object( $value ) ) {
                $value = wp_json_encode( $value, JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES );
            }

            $value = html_entity_decode( (string) $value, ENT_QUOTES | ENT_HTML5, 'UTF-8' );
            $value = wp_strip_all_tags( $value );
            $value = preg_replace( '/\s+/u', ' ', $value );
            $value = trim( (string) $value );

            // CSV Injection: bloqueia fórmulas sem estragar telefones iniciados em +55.
            if ( $value !== '' && preg_match( '/^[=@\t\r]/', $value ) ) {
                $value = "'" . $value;
            }

            return $value;
        };

        // Telefone limpo e legível, sem apóstrofo no CSV bruto.
        $csv_phone = static function( $value ) use ( $csv_clean ): string {
            $value = $csv_clean( $value );
            $value = preg_replace( '/[^0-9+]/', '', $value );
            $value = preg_replace( '/(?!^)\+/', '', (string) $value );
            return trim( (string) $value );
        };

        // Moeda em padrão planilha PT-BR: 123,00. Sem R$ nem HTML entity.
        $csv_money = static function( $value ): string {
            if ( is_numeric( $value ) ) {
                return number_format( (float) $value, 2, ',', '' );
            }

            $value = html_entity_decode( (string) $value, ENT_QUOTES | ENT_HTML5, 'UTF-8' );
            $value = wp_strip_all_tags( $value );
            $value = str_replace( [ 'R$', 'R＄', '&#82;&#36;', '&nbsp;', ' ' ], '', $value );

            if ( preg_match( '/,\d{1,2}$/', $value ) ) {
                $value = str_replace( '.', '', $value );
                $value = str_replace( ',', '.', $value );
            }

            $value = preg_replace( '/[^0-9.\-]/', '', $value );
            $num   = is_numeric( $value ) ? (float) $value : 0.0;

            return number_format( $num, 2, ',', '' );
        };

        $delimiter = ';';
        $out       = fopen( 'php://temp', 'r+' );

        // BOM UTF-8: preserva acentos ao abrir direto no Excel.
        fwrite( $out, "\xEF\xBB\xBF" );

        fputcsv( $out, [
            'Pedido',
            'Data do pedido',
            'Cliente',
            'CPF/CNPJ',
            'Telefone',
            'Produto(s)',
            'Status',
            'Transportadora',
            'Valor',
            'Frete',
            'Rastreio',
            'Endereço de entrega',
        ], $delimiter );

        foreach ( array_values( $orders ) as $o ) {
            $tracking = ! empty( $o['tracking_codes'] ) && is_array( $o['tracking_codes'] )
                ? implode( ' | ', array_map( 'strval', $o['tracking_codes'] ) )
                : (string) ( $o['tracking'] ?? '' );

            $cpf      = $o['billing_cpf'] ?? '';
            $phone    = $o['billing']['phone'] ?? '';
            $customer = trim( (string) ( $o['billing']['name'] ?? '' ) );
            $address  = $o['shipping']['address'] ?? ( $o['billing']['address'] ?? '' );

            $value_no_shipping = $o['total_no_ship_raw']
                ?? ( $o['total_no_ship'] ?? ( $o['total_raw'] ?? ( $o['total'] ?? 0 ) ) );

            $shipping_value = $o['shipping_total_raw']
                ?? ( $o['shipping_total'] ?? 0 );

            fputcsv( $out, [
                $csv_clean( '#' . ( $o['number'] ?? '' ) ),
                $csv_clean( $o['date'] ?? '' ),
                $csv_clean( $customer ),
                $csv_clean( $cpf ),
                $csv_phone( $phone ),
                $csv_clean( $o['product_name'] ?? '' ),
                $csv_clean( senderzz_portal_status_label( (string) ( $o['status'] ?? '' ) ) ),
                $csv_clean( $o['shipping_name'] ?? '' ),
                $csv_money( $value_no_shipping ),
                $csv_money( $shipping_value ),
                $csv_clean( $tracking ),
                $csv_clean( $address ),
            ], $delimiter );
        }

        rewind( $out );
        $csv = stream_get_contents( $out );
        fclose( $out );

        return [ 'success' => true, 'csv' => $csv ];
    }
    private function ensure_links_table(): void {
        global $wpdb;
        $t = $wpdb->prefix.'senderzz_portal_links';
        require_once ABSPATH.'wp-admin/includes/upgrade.php';
        dbDelta("CREATE TABLE IF NOT EXISTS $t (
            id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
            user_id BIGINT UNSIGNED NOT NULL,
            shipping_class_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
            token VARCHAR(64) NOT NULL,
            name VARCHAR(191) NULL,
            email VARCHAR(191) NULL,
            expires_at DATETIME NULL,
            used_at DATETIME NULL,
            max_uses INT UNSIGNED NULL DEFAULT NULL,
            use_count INT UNSIGNED NOT NULL DEFAULT 0,
            created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
            PRIMARY KEY (id), UNIQUE KEY token (token), KEY user_id (user_id)
        ) ".$wpdb->get_charset_collate().";");
        // Migração segura para instalações antigas
        $cols = $wpdb->get_col("DESC {$t}", 0);
        if(is_array($cols) && !in_array('max_uses',$cols,true)){
            $wpdb->query("ALTER TABLE {$t} ADD max_uses INT UNSIGNED NULL DEFAULT NULL, ADD use_count INT UNSIGNED NOT NULL DEFAULT 0");
        }
    }

    // ── CHECKOUT LINKS (FunnelKit) ────────────────────────────────────────

    /**
     * Garante a existência da tabela senderzz_checkout_links.
     * Campos: id, user_id, post_id (ID do wfacp_checkout duplicado),
     *         name, slug, url, components_text, price_label, created_at.
     */
    private function ensure_checkout_links_table(): void {
        senderzz_portal_ensure_checkout_links_table();
    }

    /**
     * Duplica um post do tipo wfacp_checkout (template ID 140) e todas as suas metas.
     * Retorna o novo post_id ou WP_Error.
     *
     * @param string $new_title  Título do novo checkout.
     * @param string $new_slug   Slug único (garantido via wp_unique_post_slug).
     * @param int    $template_id Post ID do template a duplicar (default 140).
     * @return int|WP_Error
     */
    private function duplicate_wfacp_checkout( string $new_title, string $new_slug, int $template_id = 140 ) {
        $template = get_post( $template_id );
        if ( ! $template || $template->post_type !== 'wfacp_checkout' ) {
            return new \WP_Error( 'template_not_found', "Template wfacp_checkout ID {$template_id} não encontrado." );
        }

        $unique_slug = wp_unique_post_slug(
            sanitize_title( $new_slug ),
            0,
            'publish',
            'wfacp_checkout',
            0
        );

        // Cria o post com post_content vazio — igual ao FunnelKit nativo
        // O WFACP gera o post_content dinamicamente na primeira visita
        $new_post_id = wp_insert_post( [
            'post_title'   => sanitize_text_field( $new_title ),
            'post_name'    => $unique_slug,
            'post_type'    => 'wfacp_checkout',
            'post_status'  => 'publish',
            'post_author'  => get_current_user_id() ?: 1,
            'post_parent'  => 0,
            'post_content' => '',
        ], true );

        if ( is_wp_error( $new_post_id ) ) {
            return $new_post_id;
        }

        // Copia metas do template (exceto as que o WFACP vai regenerar)
        $metas = get_post_meta( $template_id );
        $skip_keys = [
            '_edit_lock', '_edit_last',
            '_elementor_css', '_elementor_page_assets', '_elementor_element_cache',
            '_wfacp_selected_products', '_wfacp_product_switcher_setting',
        ];
        foreach ( $metas as $meta_key => $meta_values ) {
            if ( in_array( $meta_key, $skip_keys, true ) ) continue;
            foreach ( $meta_values as $meta_value ) {
                add_post_meta( $new_post_id, $meta_key, maybe_unserialize( $meta_value ) );
            }
        }

        // Copia taxonomias
        $taxonomies = get_object_taxonomies( 'wfacp_checkout' );
        foreach ( $taxonomies as $taxonomy ) {
            $terms = wp_get_object_terms( $template_id, $taxonomy, [ 'fields' => 'ids' ] );
            if ( ! is_wp_error( $terms ) && ! empty( $terms ) ) {
                wp_set_object_terms( $new_post_id, $terms, $taxonomy );
            }
        }

        // Referência ao template
        update_post_meta( $new_post_id, '_senderzz_checkout_template_id', $template_id );
        update_post_meta( $new_post_id, '_senderzz_checkout_created_by', 'senderzz_portal' );

        // Metas críticas do FunnelKit
        $wfacp_version = get_post_meta( $template_id, '_wfacp_version', true );
        update_post_meta( $new_post_id, '_wfacp_version', $wfacp_version ?: '3.18.0' );
        update_post_meta( $new_post_id, '_bwf_in_funnel', 3 );

        // Registra no array steps do funnel 3
        global $wpdb;
        $funnel_row = $wpdb->get_row( "SELECT * FROM {$wpdb->prefix}bwf_funnels WHERE id = 3", ARRAY_A );
        if ( $funnel_row ) {
            $steps = json_decode( $funnel_row['steps'] ?? '[]', true );
            if ( ! is_array( $steps ) ) $steps = [];
            $already = false;
            foreach ( $steps as $step ) {
                if ( isset( $step['id'] ) && (int) $step['id'] === (int) $new_post_id ) {
                    $already = true; break;
                }
            }
            if ( ! $already ) {
                $steps[] = [ 'type' => 'wc_checkout', 'id' => (int) $new_post_id ];
                $wpdb->update(
                    $wpdb->prefix . 'bwf_funnels',
                    [ 'steps' => wp_json_encode( $steps ) ],
                    [ 'id'    => 3 ]
                );
            }
        }

        // Dispara save_post para o WFACP gerar o post_content dinamicamente
        // (igual ao que acontece quando o admin salva o post no painel)
        return $new_post_id;
    }

    private function senderzz_apply_wfacp_products( int $checkout_id, array $components, float $offer_value = 0.0 ): void {
        $products = [];
        foreach ( $components as $comp ) {
            $pid = absint( $comp['pid'] ?? 0 );
            $vid = absint( $comp['vid'] ?? 0 );
            $qty = max( 1, absint( $comp['qty'] ?? 1 ) );
            $real_id = $vid ?: $pid;
            $product = wc_get_product( $real_id );
            if ( ! $product ) continue;

            $price          = (float) $product->get_price();
            $sale_price     = (float) $product->get_sale_price();
            $regular_price  = (float) $product->get_regular_price();
            $discount_type  = 'percent_discount_sale';
            $discount_amount = '0';

            if ( $offer_value > 0 && count( $components ) === 1 ) {
                $price         = $offer_value;
                $sale_price    = $offer_value;
                $regular_price = $offer_value;
                $discount_type  = 'fixed_price';
                $discount_amount = '0';
            }

            $key = 'wfacp_' . substr( md5( $checkout_id . '|' . $real_id . '|' . $qty . '|' . wp_rand() ), 0, 13 );

            // Formato mínimo de 19 campos — igual ao que o FunnelKit nativo gera
            $products[ $key ] = [
                'title'               => $product->get_name(),
                'discount_type'       => $discount_type,
                'discount_amount'     => $discount_amount,
                'discount_price'      => '0',
                'quantity'            => (string) $qty,
                'type'                => $product->get_type(),
                'id'                  => (string) $real_id,
                'parent_product_id'   => '0',
                'stock'               => '1',
                'is_sold_individually' => '1',
                'product_image'       => 'https://app.senderzz.com.br/wp-content/plugins/funnel-builder/admin/assets/img/product_default_icon.jpg',
                'product_type'        => $product->get_type(),
                'product_attribute'   => '-',
                'is_on_sale'          => '',
                'currency_symbol'     => '&#82;&#36;',
                'product_stock_status' => '1',
                'product_stock'       => 'in-stock',
                'price_range'         => '',
                'product_status'      => 'publish',
            ];
        }
        if ( ! empty( $products ) ) {
            // Usa API nativa do WFACP se disponível — garante formato correto
            if ( class_exists( '\WFACP_Common' ) && method_exists( '\WFACP_Common', 'update_page_product' ) ) {
                \WFACP_Common::update_page_product( $checkout_id, $products );
            } else {
                update_post_meta( $checkout_id, '_wfacp_selected_products', $products );
            }
            update_post_meta( $checkout_id, '_wfacp_product_switcher_setting', [
                'products'         => $products,
                'default_products' => array_keys( $products ),
                'settings'         => [
                    'enable_delete_item'                  => false,
                    'enable_custom_name_in_order_summary' => false,
                    'is_hide_additional_information'      => 'true',
                    'additional_information_title'        => "WHAT'S INCLUDED IN YOUR PLAN?",
                    'hide_quantity_switcher'              => false,
                    'hide_quick_view'                     => false,
                    'hide_product_image'                  => 'true',
                    'hide_best_value'                     => false,
                    'hide_you_save'                       => 'true',
                    'best_value_product'                  => '',
                    'best_value_position'                 => 'below',
                    'best_value_text'                     => 'Best Value',
                    'product_switcher_template'           => 'default',
                    'setting_migrate'                     => '3.18.0',
                ],
            ] );
            update_post_meta( $checkout_id, '_bwf_in_funnel', 3 );
            clean_post_cache( $checkout_id );
            if ( class_exists( '\Elementor\Plugin' ) ) {
                delete_post_meta( $checkout_id, '_elementor_css' );
            }
        }
    }

    /**
     * AJAX: gera novo checkout personalizado e salva na tabela real/legada.
     * szaction = 'checkout_link_generate'
     *
     * Regra v158:
     * - Usa a lógica antiga/estável de permalink do template configurado.
     * - Mantém comissão por link em affiliate_commission_pct.
     * - Se a comissão do link vier vazia, usa a comissão padrão do produtor no momento da criação.
     * - Se não houver comissão padrão, bloqueia.
     * - Preenche TODOS os campos NOT NULL da tabela antiga wp_senderzz_checkout_links.
     */
    private function ajax_checkout_link_generate( object $user ): array {
        global $wpdb;

        if ( function_exists( 'senderzz_portal_ensure_checkout_links_table' ) ) {
            senderzz_portal_ensure_checkout_links_table();
        }

        $table = $wpdb->prefix . 'senderzz_checkout_links';
        $table_exists = $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $table ) );
        if ( $table_exists !== $table ) {
            return [ 'success' => false, 'message' => 'Tabela de links não encontrada: ' . $table ];
        }

        $cols = $wpdb->get_col( "SHOW COLUMNS FROM `{$table}`", 0 );
        $cols = is_array( $cols ) ? $cols : [];

        // Garante colunas críticas sem quebrar instalações antigas.
        if ( ! in_array( 'token', $cols, true ) ) {
            $wpdb->query( "ALTER TABLE `{$table}` ADD `token` VARCHAR(80) NOT NULL DEFAULT '' AFTER `display_value`" );
            $cols[] = 'token';
        }
        if ( ! in_array( 'affiliate_commission_pct', $cols, true ) ) {
            $wpdb->query( "ALTER TABLE `{$table}` ADD `affiliate_commission_pct` DECIMAL(10,2) NOT NULL DEFAULT 0.00" );
            $cols[] = 'affiliate_commission_pct';
        }
        if ( ! in_array( 'affiliate_visible', $cols, true ) ) {
            $wpdb->query( "ALTER TABLE `{$table}` ADD `affiliate_visible` TINYINT(1) NOT NULL DEFAULT 0" );
            $cols[] = 'affiliate_visible';
        }
        if ( ! in_array( 'link_motoboy_id', $cols, true ) ) {
            $wpdb->query( "ALTER TABLE `{$table}` ADD `link_motoboy_id` BIGINT(20) UNSIGNED NULL" );
            $cols[] = 'link_motoboy_id';
        }

        $name  = sanitize_text_field( wp_unslash( $_POST['cl_name'] ?? '' ) );
        $valor = (float) str_replace( ',', '.', sanitize_text_field( wp_unslash( $_POST['cl_valor'] ?? '0' ) ) );

        if ( $name === '' ) {
            return [ 'success' => false, 'message' => 'Informe um nome para o link.' ];
        }
        if ( $valor < 0 ) {
            return [ 'success' => false, 'message' => 'Valor da oferta inválido.' ];
        }

        $components_raw = $_POST['cl_components'] ?? [];
        if ( is_string( $components_raw ) ) {
            $decoded = json_decode( wp_unslash( $components_raw ), true );
            $components_raw = is_array( $decoded ) ? $decoded : [];
        }
        $components_raw = is_array( $components_raw ) ? $components_raw : [];
        if ( empty( $components_raw ) ) {
            return [ 'success' => false, 'message' => 'Adicione pelo menos um produto.' ];
        }

        $components = [];
        $components_text_parts = [];
        $first_product_id = 0;
        foreach ( $components_raw as $comp ) {
            $pid = absint( $comp['product_id'] ?? 0 );
            $vid = absint( $comp['variation_id'] ?? 0 );
            $qty = max( 1, absint( $comp['qty'] ?? 1 ) );
            if ( ! $pid ) { continue; }

            $product = function_exists( 'wc_get_product' ) ? wc_get_product( $pid ) : null;
            if ( ! $product ) { continue; }

            if ( ! $first_product_id ) { $first_product_id = $pid; }
            $pname = $product->get_name();
            if ( $vid ) {
                $variation = wc_get_product( $vid );
                if ( $variation ) {
                    $attrs = array_filter( array_values( (array) $variation->get_variation_attributes() ) );
                    if ( ! empty( $attrs ) ) {
                        $pname .= ' — ' . implode( ' / ', $attrs );
                    }
                }
            }
            $components[] = [ 'pid' => $pid, 'vid' => $vid, 'qty' => $qty ];
            $components_text_parts[] = ( $qty > 1 ? $qty . 'x ' : '' ) . $pname;
        }

        if ( empty( $components ) ) {
            return [ 'success' => false, 'message' => 'Nenhum produto válido selecionado.' ];
        }

        $producer_portal_id = (int) ( $user->id ?? 0 );
        $producer_wp_id = 0;
        if ( ! empty( $user->wp_user_id ) ) {
            $producer_wp_id = (int) $user->wp_user_id;
        } elseif ( $producer_portal_id ) {
            $producer_wp_id = (int) $wpdb->get_var( $wpdb->prepare( "SELECT wp_user_id FROM {$wpdb->prefix}senderzz_portal_users WHERE id=%d LIMIT 1", $producer_portal_id ) );
        }
        if ( ! $producer_wp_id && ! empty( $user->email ) ) {
            $wp_user = get_user_by( 'email', sanitize_email( (string) $user->email ) );
            if ( $wp_user ) { $producer_wp_id = (int) $wp_user->ID; }
        }

        $commission_raw = isset( $_POST['cl_commission_pct'] ) ? trim( (string) wp_unslash( $_POST['cl_commission_pct'] ) ) : '';
        if ( $commission_raw !== '' ) {
            $commission_norm = str_replace( ',', '.', $commission_raw );
            if ( ! is_numeric( $commission_norm ) ) {
                return [ 'success' => false, 'message' => 'Comissão inválida. Informe um número de 0 a 100.' ];
            }
            $commission_pct = (float) $commission_norm;
        } else {
            $meta_keys = [
                '_sz_aff_default_commission_pct',
                'sz_aff_default_commission_pct',
                'senderzz_affiliate_default_commission',
                'senderzz_default_commission',
                'sz_default_commission',
            ];
            $default_raw = '';
            if ( $producer_wp_id ) {
                foreach ( $meta_keys as $mk ) {
                    $tmp = get_user_meta( $producer_wp_id, $mk, true );
                    if ( $tmp !== '' && $tmp !== null ) { $default_raw = $tmp; break; }
                }
            }
            if ( $default_raw === '' || $default_raw === null ) {
                return [ 'success' => false, 'message' => 'Configure a comissão padrão do produtor antes de criar um checkout sem comissão própria.' ];
            }
            $default_norm = str_replace( ',', '.', (string) $default_raw );
            if ( ! is_numeric( $default_norm ) ) {
                return [ 'success' => false, 'message' => 'A comissão padrão do produtor está inválida. Ajuste no menu Afiliados.' ];
            }
            $commission_pct = (float) $default_norm;
        }
        if ( $commission_pct < 0 || $commission_pct > 100 ) {
            return [ 'success' => false, 'message' => 'A comissão do checkout deve ficar entre 0% e 100%.' ];
        }

        $make_token = function() use ( $wpdb, $table ): string {
            for ( $i = 0; $i < 8; $i++ ) {
                try { $token = bin2hex( random_bytes( 16 ) ); }
                catch ( Throwable $e ) { $token = wp_generate_password( 32, false, false ); }
                $exists = (int) $wpdb->get_var( $wpdb->prepare( "SELECT COUNT(1) FROM `{$table}` WHERE token=%s OR slug=%s", $token, $token ) );
                if ( ! $exists ) { return $token; }
            }
            return md5( uniqid( 'sz_', true ) . wp_rand() );
        };

        $components_text = implode( ' + ', $components_text_parts );
        $price_label = $valor > 0 ? 'R$ ' . number_format( $valor, 2, ',', '.' ) : 'R$ 0,00';
        $affiliate_visible = ! empty( $_POST['cl_affiliate_visible'] ) ? 1 : 0;

        $user_classes = function_exists( 'sz_get_user_class_ids' ) ? (array) sz_get_user_class_ids( $user ) : [];
        $shipping_class_id = ! empty( $user_classes ) ? (int) reset( $user_classes ) : (int) ( $user->shipping_class_id ?? 0 );

        $base_checkout_id = (int) get_option( 'senderzz_checkout_template_id', 140 );
        // Expedição sempre usa a LP pública padrão.
        $base_url = home_url( '/checkouts/lp/' );

        $token = $make_token();
        $url = add_query_arg( 'sz', $token, $base_url );
        $now = current_time( 'mysql' );
        $payload = wp_json_encode( [
            'components' => $components,
            'valor' => $valor,
            'source' => 'inline_product_checkout',
            'first_product_id' => $first_product_id,
        ], JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES );

        $data = [];
        $format = [];
        $add = function( string $key, $value, string $fmt ) use ( &$data, &$format, $cols ) {
            if ( in_array( $key, $cols, true ) ) {
                $data[ $key ] = $value;
                $format[] = $fmt;
            }
        };

        // Campos obrigatórios da estrutura real/antiga.
        $add( 'producer_id', (int) $producer_portal_id, '%d' );
        $add( 'shipping_class_id', (int) $shipping_class_id, '%d' );
        $add( 'name', $name, '%s' );
        $add( 'display_value', (float) $valor, '%f' );
        $add( 'token', $token, '%s' );
        $add( 'payload', $payload, '%s' );
        $add( 'created_at', $now, '%s' );
        $add( 'updated_at', $now, '%s' );
        $add( 'schema_version', 'v374', '%s' );
        $add( 'user_id', (int) $producer_portal_id, '%d' );
        $add( 'post_id', (int) $base_checkout_id, '%d' );
        $add( 'slug', $token, '%s' );
        $add( 'url', $url, '%s' );
        $add( 'tipo', 'correio', '%s' );
        $add( 'link_motoboy_id', null, '%d' );
        $add( 'components_text', $components_text, '%s' );
        $add( 'price_label', $price_label, '%s' );
        $add( 'affiliate_visible', $affiliate_visible, '%d' );
        $add( 'affiliate_commission_pct', (float) $commission_pct, '%f' );

        $inserted = $wpdb->insert( $table, $data, $format );
        if ( $inserted === false || ! $wpdb->insert_id ) {
            return [
                'success' => false,
                'message' => 'Erro ao inserir checkout: ' . ( $wpdb->last_error ?: 'falha desconhecida' ),
                'debug_query' => $wpdb->last_query,
            ];
        }
        $link_id = (int) $wpdb->insert_id;

        // Cria espelho Motoboy de forma compatível, sem depender do filtro legado.
        $motoboy_id = 0;
        $motoboy_url = '';
        $wp_user_has_motoboy_disabled = $producer_wp_id && get_user_meta( $producer_wp_id, '_sz_has_motoboy', true ) === '0';
        if ( ! $wp_user_has_motoboy_disabled ) {
            $token_mb = $make_token();
            $disp_cpf = $producer_wp_id && get_user_meta( $producer_wp_id, '_sz_dispensar_cpf_motoboy', true ) === '1';
            $template_key = $disp_cpf ? 'senderzz_checkout_template_id_sem_cpf' : 'senderzz_checkout_template_id';
            $default_template = $disp_cpf ? 1075 : $base_checkout_id;
            $motoboy_template_id = (int) get_option( $template_key, $default_template );
            $motoboy_base_url = $motoboy_template_id ? get_permalink( $motoboy_template_id ) : '';
            if ( ! $motoboy_base_url ) { $motoboy_base_url = home_url( $disp_cpf ? '/checkouts/codsfpc/' : '/checkouts/cod/' ); }
            $motoboy_url = add_query_arg( [ 'sz' => $token_mb, 'szm' => '1' ], $motoboy_base_url );

            $mb_data = [];
            $mb_format = [];
            $mb_add = function( string $key, $value, string $fmt ) use ( &$mb_data, &$mb_format, $cols ) {
                if ( in_array( $key, $cols, true ) ) {
                    $mb_data[ $key ] = $value;
                    $mb_format[] = $fmt;
                }
            };
            $mb_add( 'producer_id', (int) $producer_portal_id, '%d' );
            $mb_add( 'shipping_class_id', 0, '%d' );
            $mb_add( 'name', $name . ' — Motoboy', '%s' );
            $mb_add( 'display_value', (float) $valor, '%f' );
            $mb_add( 'token', $token_mb, '%s' );
            $mb_add( 'payload', wp_json_encode( [ 'parent_link_id' => $link_id, 'components' => $components, 'valor' => $valor, 'source' => 'inline_product_checkout_motoboy' ], JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES ), '%s' );
            $mb_add( 'created_at', $now, '%s' );
            $mb_add( 'updated_at', $now, '%s' );
            $mb_add( 'schema_version', 'v374', '%s' );
            $mb_add( 'user_id', (int) $producer_portal_id, '%d' );
            $mb_add( 'post_id', (int) $motoboy_template_id, '%d' );
            $mb_add( 'slug', $token_mb, '%s' );
            $mb_add( 'url', $motoboy_url, '%s' );
            $mb_add( 'tipo', 'motoboy', '%s' );
            $mb_add( 'link_motoboy_id', null, '%d' );
            $mb_add( 'components_text', $components_text, '%s' );
            $mb_add( 'price_label', $price_label, '%s' );
            $mb_add( 'affiliate_visible', $affiliate_visible, '%d' );
            $mb_add( 'affiliate_commission_pct', (float) $commission_pct, '%f' );

            $mb_inserted = $wpdb->insert( $table, $mb_data, $mb_format );
            if ( $mb_inserted !== false && $wpdb->insert_id ) {
                $motoboy_id = (int) $wpdb->insert_id;
                $wpdb->update( $table, [ 'link_motoboy_id' => $motoboy_id ], [ 'id' => $link_id ], [ '%d' ], [ '%d' ] );
            } else {
                // Não perde o link principal, mas mostra erro real para ajuste se o espelho falhar.
                return [
                    'success' => false,
                    'message' => 'Checkout principal criado, mas erro ao criar Motoboy: ' . ( $wpdb->last_error ?: 'falha desconhecida' ),
                    'link_id' => $link_id,
                    'url' => $url,
                    'debug_query' => $wpdb->last_query,
                ];
            }
        }

        return [
            'success' => true,
            'message' => 'Checkout criado com sucesso!',
            'link_id' => $link_id,
            'motoboy_id' => $motoboy_id,
            'post_id' => $base_checkout_id,
            'url' => $url,
            'url_motoboy' => $motoboy_url,
            'token' => $token,
            'affiliate_visible' => $affiliate_visible,
            'name' => $name,
            'price_label' => $price_label,
            'components' => $components_text,
            'affiliate_commission_pct' => $commission_pct,
        ];
    }

    /**
     * AJAX: atualiza comissão do checkout principal e do espelho Motoboy vinculado.
     * szaction = 'checkout_link_commission_update'
     */
    private function ajax_checkout_link_commission_update( object $user ): array {
        global $wpdb;
        senderzz_portal_ensure_checkout_links_table();
        $link_id = absint( $_POST['link_id'] ?? 0 );
        $raw = isset( $_POST['commission_pct'] ) ? trim( (string) wp_unslash( $_POST['commission_pct'] ) ) : '';
        $norm = str_replace( ',', '.', $raw );
        if ( ! $link_id || $norm === '' || ! is_numeric( $norm ) ) return [ 'success' => false, 'message' => 'Comissão inválida.' ];
        $pct = (float) $norm;
        if ( $pct < 0 || $pct > 100 ) return [ 'success' => false, 'message' => 'A comissão deve ficar entre 0% e 100%.' ];
        $t = $wpdb->prefix . 'senderzz_checkout_links';
        $link = $wpdb->get_row( $wpdb->prepare( "SELECT id, link_motoboy_id FROM {$t} WHERE id=%d AND user_id=%d LIMIT 1", $link_id, (int) $user->id ), ARRAY_A );
        if ( ! $link ) return [ 'success' => false, 'message' => 'Link não encontrado.' ];
        $ids = [ $link_id ];
        if ( ! empty( $link['link_motoboy_id'] ) ) $ids[] = absint( $link['link_motoboy_id'] );
        $updated_any = false;
        foreach ( array_values( array_unique( array_filter( $ids ) ) ) as $id ) {
            $ok = $wpdb->update( $t, [ 'affiliate_commission_pct' => $pct, 'updated_at' => current_time( 'mysql' ) ], [ 'id' => $id, 'user_id' => (int) $user->id ], [ '%f','%s' ], [ '%d','%d' ] );
            if ( $ok !== false ) $updated_any = true;
        }
        if ( ! $updated_any ) return [ 'success' => false, 'message' => 'Erro ao salvar comissão.' ];
        return [ 'success' => true, 'message' => 'Comissão salva.', 'affiliate_commission_pct' => $pct ];
    }

    /**
     * AJAX: habilita/desabilita checkout para afiliados.
     * szaction = 'checkout_link_affiliate_toggle'
     */
    private function ajax_affiliate_action( object $user ): array {
        global $wpdb;
        $act          = sanitize_key( $_POST['aff_act'] ?? '' );
        $affiliate_id = absint( $_POST['affiliate_id'] ?? 0 );
        if ( ! $act || ! $affiliate_id ) return [ 'success' => false, 'message' => 'Parâmetros inválidos.' ];

        // Identificar o producer_id do usuário logado
        $producer_id = absint( $user->id ?? 0 );
        if ( ! $producer_id ) return [ 'success' => false, 'message' => 'Usuário inválido.' ];

        // Verificar que o afiliado pertence a este produtor
        $row = $wpdb->get_row( $wpdb->prepare(
            "SELECT id, status, user_id FROM " . ( function_exists('sz_aff_table') ? sz_aff_table('sz_affiliates') : $wpdb->prefix.'sz_affiliates' ) . " WHERE id=%d AND producer_id=%d AND deleted_at IS NULL",
            $affiliate_id, $producer_id
        ), ARRAY_A );

        if ( ! $row ) return [ 'success' => false, 'message' => 'Afiliado não encontrado.' ];
        $aff_table = function_exists('sz_aff_table') ? sz_aff_table('sz_affiliates') : $wpdb->prefix.'sz_affiliates';

        if ( $act === 'approve' ) {
            $wpdb->update( $aff_table, [ 'status' => 'active', 'approved_at' => current_time('mysql') ], [ 'id' => $affiliate_id ], [ '%s', '%s' ], [ '%d' ] );
            if ( function_exists('sz_aff_ensure_portal_affiliate_user') ) sz_aff_ensure_portal_affiliate_user( (int)( $row['user_id'] ?? 0 ) );
            if ( function_exists('sz_aff_send_affiliate_access_email') ) {
                $producer_name = trim( (string)( $user->name ?? $user->email ?? 'Produtor' ) );
                sz_aff_send_affiliate_access_email( (int)( $row['user_id'] ?? 0 ), $producer_name, 'active' );
            }
            if ( function_exists('sz_aff_sync_portal_access_after_affiliation_change') ) sz_aff_sync_portal_access_after_affiliation_change( (int)( $row['user_id'] ?? 0 ) );
            return [ 'success' => true, 'message' => 'Afiliado aprovado com sucesso!' ];
        }

        if ( $act === 'reject' ) {
            $wpdb->update( $aff_table, [ 'status' => 'pending', 'deleted_at' => current_time('mysql') ], [ 'id' => $affiliate_id ], [ '%s', '%s' ], [ '%d' ] );
            if ( function_exists('sz_aff_sync_portal_access_after_affiliation_change') ) sz_aff_sync_portal_access_after_affiliation_change( (int)( $row['user_id'] ?? 0 ) );
            return [ 'success' => true, 'message' => 'Afiliado recusado.' ];
        }

        if ( $act === 'update_commission' ) {
            $pct = max( 0, min( 100, (float) str_replace( ',', '.', sanitize_text_field( wp_unslash( $_POST['commission_pct'] ?? '0' ) ) ) ) );
            $wpdb->update( $aff_table, [ 'commission_pct' => $pct ], [ 'id' => $affiliate_id ], [ '%f' ], [ '%d' ] );
            return [ 'success' => true, 'message' => 'Comissão atualizada.' ];
        }

        return [ 'success' => false, 'message' => 'Ação desconhecida.' ];
    }

    private function ajax_checkout_link_affiliate_toggle( object $user ): array {
        global $wpdb;
        senderzz_portal_ensure_checkout_links_table();
        $link_id = absint( $_POST['link_id'] ?? 0 );
        $enabled = ! empty( $_POST['enabled'] ) ? 1 : 0;
        if ( ! $link_id ) return [ 'success' => false, 'message' => 'ID inválido.' ];
        $t = $wpdb->prefix . 'senderzz_checkout_links';
        $cols = $wpdb->get_col( "SHOW COLUMNS FROM `{$t}`", 0 );
        if ( ! is_array( $cols ) || ! in_array( 'affiliate_visible', $cols, true ) ) {
            $wpdb->query( "ALTER TABLE `{$t}` ADD `affiliate_visible` TINYINT(1) NOT NULL DEFAULT 0" );
        }
        $belongs = $wpdb->get_var( $wpdb->prepare(
            "SELECT id FROM `{$t}` WHERE id=%d AND (user_id=%d OR producer_id=%d) LIMIT 1",
            $link_id, (int) $user->id, (int) $user->id
        ) );
        if ( ! $belongs ) return [ 'success' => false, 'message' => 'Sem permissão para este checkout.' ];
        $ok = $wpdb->update( $t, [ 'affiliate_visible' => $enabled ], [ 'id' => $link_id ], [ '%d' ], [ '%d' ] );
        if ( $ok === false ) return [ 'success' => false, 'message' => 'Erro ao salvar.' ];
        return [ 'success' => true, 'message' => $enabled ? 'Oferta liberada para afiliados.' : 'Oferta removida dos afiliados.' ];
    }

    /**
     * AJAX: exclui um checkout link e o post FunnelKit associado.
     * szaction = 'checkout_link_delete'
     */
    private function ajax_checkout_link_delete( object $user ): array {
        global $wpdb;
        senderzz_portal_ensure_checkout_links_table();
        $link_id = absint( $_POST['link_id'] ?? 0 );
        if ( ! $link_id ) return [ 'success' => false, 'message' => 'ID inválido.' ];

        $link = $wpdb->get_row( $wpdb->prepare(
            "SELECT * FROM {$wpdb->prefix}senderzz_checkout_links WHERE id = %d AND user_id = %d",
            $link_id, $user->id
        ), ARRAY_A );

        if ( ! $link ) return [ 'success' => false, 'message' => 'Link não encontrado.' ];

        // Remove o link selecionado e o espelho vinculado (Expedição/Motoboy), sem apagar o template base.
        $ids_to_delete = [ $link_id ];
        $mirror_id = absint( $link['link_motoboy_id'] ?? 0 );
        if ( $mirror_id ) {
            $ids_to_delete[] = $mirror_id;
        }
        $children = $wpdb->get_col( $wpdb->prepare(
            "SELECT id FROM {$wpdb->prefix}senderzz_checkout_links WHERE user_id = %d AND link_motoboy_id = %d",
            $user->id,
            $link_id
        ) );
        foreach ( (array) $children as $cid ) {
            $ids_to_delete[] = absint( $cid );
        }
        $ids_to_delete = array_values( array_unique( array_filter( array_map( 'absint', $ids_to_delete ) ) ) );
        foreach ( $ids_to_delete as $did ) {
            $wpdb->delete( $wpdb->prefix . 'senderzz_checkout_links', [ 'id' => $did, 'user_id' => $user->id ], [ '%d', '%d' ] );
        }
        return [ 'success' => true, 'message' => 'Checkout excluído.' ];
    }

    // ── Suporte / Tickets ────────────────────────────────────────────────────

    private function ajax_tickets_list( object $user ): array {
        global $wpdb;
        $p   = $wpdb->prefix;
        $uid = (int) $user->id;
        $rows = $wpdb->get_results( $wpdb->prepare(
            "SELECT t.id, t.assunto, t.categoria, t.status, t.prioridade, t.created_at, t.updated_at,
                    (SELECT COUNT(*) FROM {$p}sz_portal_ticket_msgs m WHERE m.ticket_id = t.id) AS total_msgs,
                    (SELECT m2.autor_tipo FROM {$p}sz_portal_ticket_msgs m2 WHERE m2.ticket_id = t.id ORDER BY m2.id DESC LIMIT 1) AS ultima_msg_autor
             FROM {$p}sz_portal_tickets t
             WHERE t.portal_user_id = %d
             ORDER BY t.updated_at DESC LIMIT 50",
            $uid
        ), ARRAY_A );
        return [ 'success' => true, 'tickets' => $rows ?: [] ];
    }

    private function ajax_ticket_create( object $user ): array {
        $assunto   = sanitize_text_field( $_POST['assunto'] ?? '' );
        $categoria = sanitize_key( $_POST['categoria'] ?? 'outro' );
        $mensagem  = sanitize_textarea_field( $_POST['mensagem'] ?? '' );

        if ( strlen( $assunto ) < 5 )  return [ 'success' => false, 'message' => 'Assunto muito curto.' ];
        if ( strlen( $mensagem ) < 10 ) return [ 'success' => false, 'message' => 'Mensagem muito curta.' ];
        if ( ! in_array( $categoria, [ 'financeiro', 'pedido', 'tecnico', 'outro' ], true ) ) $categoria = 'outro';

        global $wpdb;
        $p = $wpdb->prefix;
        $wpdb->insert( "{$p}sz_portal_tickets", [
            'portal_user_id' => (int) $user->id,
            'assunto'        => $assunto,
            'categoria'      => $categoria,
            'status'         => 'aberto',
            'prioridade'     => 'normal',
        ] );
        $ticket_id = (int) $wpdb->insert_id;
        if ( ! $ticket_id ) return [ 'success' => false, 'message' => 'Erro ao criar ticket.' ];
        $wpdb->insert( "{$p}sz_portal_ticket_msgs", [
            'ticket_id'  => $ticket_id,
            'autor_tipo' => 'cliente',
            'autor_id'   => (int) $user->id,
            'autor_nome' => sanitize_text_field( $user->name ?? 'Cliente' ),
            'mensagem'   => $mensagem,
        ] );
        return [ 'success' => true, 'ok' => true, 'ticket_id' => $ticket_id ];
    }

    private function ajax_ticket_msgs( object $user ): array {
        global $wpdb;
        $p         = $wpdb->prefix;
        $ticket_id = (int) ( $_POST['ticket_id'] ?? 0 );
        $uid       = (int) $user->id;
        $ticket    = $wpdb->get_row( $wpdb->prepare(
            "SELECT * FROM {$p}sz_portal_tickets WHERE id=%d AND portal_user_id=%d LIMIT 1",
            $ticket_id, $uid
        ), ARRAY_A );
        if ( ! $ticket ) return [ 'success' => false, 'message' => 'Ticket não encontrado.' ];
        $msgs = $wpdb->get_results( $wpdb->prepare(
            "SELECT id, autor_tipo, autor_nome, mensagem, created_at
             FROM {$p}sz_portal_ticket_msgs WHERE ticket_id=%d ORDER BY id ASC LIMIT 100",
            $ticket_id
        ), ARRAY_A );
        return [ 'success' => true, 'ok' => true, 'ticket' => $ticket, 'msgs' => $msgs ?: [] ];
    }

    private function ajax_ticket_send_msg( object $user ): array {
        global $wpdb;
        $p         = $wpdb->prefix;
        $ticket_id = (int) ( $_POST['ticket_id'] ?? 0 );
        $uid       = (int) $user->id;
        $mensagem  = sanitize_textarea_field( $_POST['mensagem'] ?? '' );
        if ( strlen( $mensagem ) < 3 ) return [ 'success' => false, 'message' => 'Mensagem vazia.' ];
        $ticket = $wpdb->get_row( $wpdb->prepare(
            "SELECT id, status FROM {$p}sz_portal_tickets WHERE id=%d AND portal_user_id=%d AND status != 'fechado' LIMIT 1",
            $ticket_id, $uid
        ) );
        if ( ! $ticket ) return [ 'success' => false, 'message' => 'Ticket não encontrado ou fechado.' ];
        $wpdb->insert( "{$p}sz_portal_ticket_msgs", [
            'ticket_id'  => $ticket_id,
            'autor_tipo' => 'cliente',
            'autor_id'   => $uid,
            'autor_nome' => sanitize_text_field( $user->name ?? 'Cliente' ),
            'mensagem'   => $mensagem,
        ] );
        if ( $ticket->status === 'respondido' ) {
            $wpdb->update( "{$p}sz_portal_tickets", [ 'status' => 'em_analise' ], [ 'id' => $ticket_id ] );
        }
        return [ 'success' => true, 'ok' => true ];
    }

    private function ajax_ticket_close( object $user ): array {
        global $wpdb;
        $p         = $wpdb->prefix;
        $ticket_id = (int) ( $_POST['ticket_id'] ?? 0 );
        $uid       = (int) $user->id;
        $ticket    = $wpdb->get_row( $wpdb->prepare(
            "SELECT id FROM {$p}sz_portal_tickets WHERE id=%d AND portal_user_id=%d AND status != 'fechado' LIMIT 1",
            $ticket_id, $uid
        ) );
        if ( ! $ticket ) return [ 'success' => false, 'message' => 'Ticket não encontrado.' ];
        $wpdb->update( "{$p}sz_portal_tickets", [ 'status' => 'fechado', 'fechado_at' => current_time( 'mysql' ) ], [ 'id' => $ticket_id ] );
        return [ 'success' => true, 'ok' => true ];
    }

    // ── /Suporte / Tickets ───────────────────────────────────────────────────

    // ── Cadastro público V2 ──────────────────────────────────────────────────
    private function ajax_portal_register(): array {
        global $wpdb;
        $name  = sanitize_text_field( wp_unslash( $_POST['name']  ?? '' ) );
        $email = sanitize_email( wp_unslash( $_POST['email'] ?? '' ) );
        $pw    = (string) ( $_POST['password'] ?? '' );
        $cpf   = preg_replace( '/\D/', '', sanitize_text_field( wp_unslash( $_POST['cpf']   ?? '' ) ) );
        $phone = sanitize_text_field( wp_unslash( $_POST['phone'] ?? '' ) );

        if ( ! $name || ! $email || ! $pw ) {
            return [ 'success' => false, 'data' => [ 'message' => 'Nome, e-mail e senha são obrigatórios.' ] ];
        }
        if ( strlen( $pw ) < 8 ) {
            return [ 'success' => false, 'data' => [ 'message' => 'Senha deve ter pelo menos 8 caracteres.' ] ];
        }
        if ( ! is_email( $email ) ) {
            return [ 'success' => false, 'data' => [ 'message' => 'E-mail inválido.' ] ];
        }

        $table = $wpdb->prefix . Portal_Auth::TABLE;
        // Verifica duplicata
        if ( $wpdb->get_var( $wpdb->prepare( "SELECT id FROM {$table} WHERE email=%s LIMIT 1", $email ) ) ) {
            return [ 'success' => false, 'data' => [ 'message' => 'E-mail já cadastrado. Faça login.' ] ];
        }

        // Cria usuário WP
        $wp_id = 0;
        if ( function_exists( 'get_user_by' ) && ! get_user_by( 'email', $email ) ) {
            $wp_id = (int) wp_insert_user( [
                'user_login'   => sanitize_user( $email ),
                'user_email'   => $email,
                'user_pass'    => $pw,
                'display_name' => $name,
                'role'         => 'subscriber',
            ] );
            if ( is_wp_error( $wp_id ) ) { $wp_id = 0; }
            if ( $wp_id && $cpf )  update_user_meta( $wp_id, 'sz_document', $cpf );
            if ( $wp_id && $phone ) update_user_meta( $wp_id, 'billing_phone', $phone );
        }

        // Cria portal user sem shipping_class_id (role=client, sem afiliação, sem expedição)
        $pw_hash = wp_hash_password( $pw );
        $wpdb->insert( $table, [
            'name'             => $name,
            'email'            => $email,
            'password_hash'    => $pw_hash,
            'role'             => 'client',
            'status'           => 'active',
            'shipping_class_id'=> null,
            'wp_user_id'       => $wp_id ?: null,
            'created_at'       => current_time( 'mysql' ),
        ] );
        if ( $wpdb->last_error ) {
            return [ 'success' => false, 'data' => [ 'message' => 'Erro ao criar conta. Tente novamente.' ] ];
        }

        // Auto-login: cria sessão
        $new_user = $wpdb->get_row( $wpdb->prepare( "SELECT * FROM {$table} WHERE email=%s LIMIT 1", $email ) );
        if ( $new_user && function_exists( 'Portal_Auth::create_session' ) ) {
            Portal_Auth::create_session( (int) $new_user->id, 7 );
        }

        return [ 'success' => true, 'data' => [ 'message' => 'Conta criada com sucesso!' ] ];
    }

    // Rota da página de cadastro V2
    public function maybe_render_register_v2(): void {
        if ( ! $this->is_portal() ) return;
        if ( ! isset( $_GET['sz_register'] ) ) return;
        $template = TPC_PATH . 'templates/portal/v2/register.php';
        if ( ! file_exists( $template ) ) return;
        $sz_v2_user = null;
        ob_start();
        require $template;
        echo ob_get_clean(); // phpcs:ignore
        exit;
    }
}

add_action('template_redirect', function () {
    if (function_exists('wfacp_is_checkout_page') && wfacp_is_checkout_page()) {
        if (WC()->cart && WC()->cart->is_empty()) {
            wp_redirect(home_url());
            exit;
        }
    }
});