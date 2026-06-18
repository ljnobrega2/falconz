<?php
/**
 * senderzz-checkout-link-interceptor.php
 */
defined( 'ABSPATH' ) || exit;

global $senderzz_sz_link_data;
$senderzz_sz_link_data = null;

function senderzz_sz_token(): string {
    return sanitize_text_field( $_GET['sz'] ?? '' );
}

function senderzz_sz_load_data( string $token = '' ): ?array {
    global $wpdb;

    $token = $token ?: senderzz_sz_token();
    if ( ! $token ) return null;

    $t = $wpdb->prefix . 'senderzz_checkout_links';

    $link = $wpdb->get_row(
        $wpdb->prepare(
            "SELECT * FROM `{$t}` WHERE slug = %s OR token = %s LIMIT 1",
            $token,
            $token
        ),
        ARRAY_A
    );

    if ( ! $link ) return null;

    // v374: compatibilidade real com links antigos.
    // Se o link foi criado antes de token/payload/tipo serem gravados no padrão atual,
    // repara sob demanda sem trocar o hash ?sz= nem a URL pública.
    if ( function_exists( 'senderzz_checkout_legacy_repair_single_token' ) ) {
        $repaired = senderzz_checkout_legacy_repair_single_token( $token, $link );
        if ( is_array( $repaired ) && ! empty( $repaired['id'] ) ) {
            $link = $repaired;
        }
    }

    $payload = ! empty( $link['payload'] ) ? ( json_decode( $link['payload'], true ) ?: [] ) : [];

    // Compatibilidade com links Motoboy criados antes do payload ser gravado no insert.
    // Nesses casos, o registro Motoboy aponta pelo token próprio, mas o payload oficial
    // está no link original que possui link_motoboy_id = id do Motoboy.
    if ( empty( $payload['components'] ) && ! empty( $link['id'] ) ) {
        $parent_payload_json = $wpdb->get_var(
            $wpdb->prepare(
                "SELECT payload FROM `{$t}` WHERE link_motoboy_id = %d AND payload IS NOT NULL AND payload <> '' LIMIT 1",
                (int) $link['id']
            )
        );
        if ( $parent_payload_json ) {
            $parent_payload = json_decode( (string) $parent_payload_json, true );
            if ( is_array( $parent_payload ) && ! empty( $parent_payload['components'] ) ) {
                $payload = $parent_payload;
            }
        }
    }

    if ( ( empty( $payload['components'] ) || ! is_array( $payload['components'] ) ) && function_exists( 'senderzz_checkout_legacy_build_payload_for_link' ) ) {
        $legacy_payload = senderzz_checkout_legacy_build_payload_for_link( $link );
        if ( is_array( $legacy_payload ) && ! empty( $legacy_payload['components'] ) ) {
            $payload = $legacy_payload;
        }
    }

    $components = $payload['components'] ?? [];
    $valor      = (float) ( $payload['valor'] ?? 0 );
    if ( $valor <= 0 && function_exists( 'senderzz_checkout_legacy_parse_money' ) ) {
        $valor = max( (float) ( $link['display_value'] ?? 0 ), senderzz_checkout_legacy_parse_money( (string) ( $link['price_label'] ?? '' ) ) );
    }
    $name       = sanitize_text_field( $link['name'] ?? '' );
    $name       = preg_replace( '/\s+—\s+(Motoboy|Correio|Expedição)$/iu', '', $name );

    if ( empty( $components ) ) return null;

    return [
        'components' => $components,
        'valor'      => $valor,
        'name'       => $name,
        'post_id'    => (int) ( $link['post_id'] ?? 140 ),
        'token'      => $token,
        'link_id'    => (int) ( $link['id'] ?? 0 ),
        'tipo'       => function_exists( 'senderzz_checkout_legacy_infer_type' ) ? senderzz_checkout_legacy_infer_type( $link ) : (string) ( $link['tipo'] ?? '' ),
    ];
}
// Definir cookies apenas quando há um token válido na URL
add_action( 'wp', function() {
    $sz_data = senderzz_sz_load_data();
    if ( ! $sz_data ) return;

    $token  = (string) ( $sz_data['token'] ?? '' );
    $name   = (string) ( $sz_data['name']  ?? '' );
    $valor  = (string) ( $sz_data['valor'] ?? '' );

    if ( ! $token ) return;
    if ( headers_sent() ) return;

    $cookie_path   = defined( 'COOKIEPATH' ) && COOKIEPATH ? COOKIEPATH : '/';
    $cookie_domain = defined( 'COOKIE_DOMAIN' ) ? COOKIE_DOMAIN : '';
    $expires       = time() + HOUR_IN_SECONDS;
    $secure        = is_ssl();

    // senderzz_offer_token: usado por JS no checkout para referenciar a oferta — não pode ser HttpOnly
    // senderzz_offer_value: NÃO é mais usado como fonte de verdade do valor (cruzado com DB),
    // mas mantido para compatibilidade de exibição. HttpOnly = false pois JS precisa ler para exibir o valor.
    // Os valores financeiros reais sempre vêm do DB via token.
    setcookie( 'senderzz_offer_token',   $token,  $expires, $cookie_path, $cookie_domain, $secure, false );
    setcookie( 'senderzz_offer_name',    $name,   $expires, $cookie_path, $cookie_domain, $secure, false );
    setcookie( 'senderzz_offer_value',   $valor,  $expires, $cookie_path, $cookie_domain, $secure, false );
    setcookie( 'senderzz_offer_link_id', (string) ( $sz_data['link_id'] ?? '' ), $expires, $cookie_path, $cookie_domain, $secure, false );

    $_COOKIE['senderzz_offer_token']   = $token;
    $_COOKIE['senderzz_offer_name']    = $name;
    $_COOKIE['senderzz_offer_value']   = $valor;
    $_COOKIE['senderzz_offer_link_id'] = (string) ( $sz_data['link_id'] ?? '' );
} );
function senderzz_sz_get_data(): ?array {
    global $senderzz_sz_link_data;

    if ( ! empty( $senderzz_sz_link_data ) ) {
        return $senderzz_sz_link_data;
    }

    $token = senderzz_sz_token();

    if ( ! $token && ! empty( $_POST['senderzz_offer_token'] ) ) {
        $token = sanitize_text_field( wp_unslash( $_POST['senderzz_offer_token'] ) );
    }

    if ( $token ) {
        $data = senderzz_sz_load_data( $token );
        if ( $data ) {
            $senderzz_sz_link_data = $data;

            if ( function_exists( 'WC' ) && WC()->session ) {
                WC()->session->set( 'senderzz_sz_link_data', $data );
            }

            return $data;
        }
    }

    if ( function_exists( 'WC' ) && WC()->session ) {
        $data = WC()->session->get( 'senderzz_sz_link_data' );
        if ( ! empty( $data ) && is_array( $data ) ) {
            $senderzz_sz_link_data = $data;
            return $data;
        }
    }

    return null;
}


/**
 * v399 — Canvas limpo para links públicos Senderzz/FunnelKit.
 *
 * Quando o checkout é aberto por /checkouts/.../?sz=..., alguns temas/block themes
 * voltam a imprimir cabeçalho, menu, título do post "Pedido", autor e rodapé em volta
 * do FunnelKit. O checkout Senderzz precisa ficar isolado, sem a moldura do WordPress.
 */
if ( ! function_exists( 'senderzz_sz_is_clean_checkout_request' ) ) {
    function senderzz_sz_is_clean_checkout_request(): bool {
        static $cached = null;
        if ( $cached !== null ) return (bool) $cached;

        $cached = false;
        if ( is_admin() && ! wp_doing_ajax() ) return false;

        $token = isset( $_GET['sz'] ) ? sanitize_text_field( wp_unslash( $_GET['sz'] ) ) : '';
        if ( $token === '' ) return false;

        $request_uri = isset( $_SERVER['REQUEST_URI'] ) ? esc_url_raw( wp_unslash( $_SERVER['REQUEST_URI'] ) ) : '';
        $path        = trim( (string) wp_parse_url( $request_uri, PHP_URL_PATH ), '/' );

        $is_checkout_path = (bool) preg_match( '#^checkouts/[^/]+/?$#', $path );
        if ( ! $is_checkout_path && function_exists( 'is_checkout' ) ) {
            $is_checkout_path = (bool) is_checkout();
        }
        if ( ! $is_checkout_path ) return false;

        $cached = (bool) senderzz_sz_load_data( $token );
        return (bool) $cached;
    }
}


/**
 * v401 — isolamento estrutural real do checkout público Senderzz.
 *
 * O CSS/JS escondia sobras do tema, mas o WordPress ainda renderizava a página como
 * post normal em alguns temas (autor, "Mais posts", containers e bordas). Para links
 * públicos /checkouts/.../?sz=... válidos, forçamos um template próprio do plugin,
 * sem get_header(), sem get_footer(), sem título/meta/related posts do tema.
 */
if ( ! function_exists( 'senderzz_sz_public_checkout_template_path' ) ) {
    function senderzz_sz_public_checkout_template_path(): string {
        return trailingslashit( dirname( __DIR__ ) ) . 'templates/public-checkout-canvas.php';
    }
}

add_filter( 'show_admin_bar', function( $show ) {
    if ( function_exists( 'senderzz_sz_is_clean_checkout_request' ) && senderzz_sz_is_clean_checkout_request() ) {
        return false;
    }
    return $show;
}, 9999 );

add_filter( 'woocommerce_is_checkout', function( $is_checkout ) {
    if ( function_exists( 'senderzz_sz_is_clean_checkout_request' ) && senderzz_sz_is_clean_checkout_request() ) {
        return true;
    }
    return $is_checkout;
}, 9999 );

add_filter( 'template_include', function( $template ) {
    if ( ! function_exists( 'senderzz_sz_is_clean_checkout_request' ) || ! senderzz_sz_is_clean_checkout_request() ) {
        return $template;
    }

    $canvas_template = senderzz_sz_public_checkout_template_path();
    if ( is_readable( $canvas_template ) ) {
        global $senderzz_sz_original_checkout_template;
        $senderzz_sz_original_checkout_template = $template;

        if ( function_exists( 'status_header' ) ) {
            status_header( 200 );
        }
        if ( function_exists( 'nocache_headers' ) ) {
            nocache_headers();
        }

        return $canvas_template;
    }

    return $template;
}, 99999 );


add_filter( 'body_class', function( array $classes ): array {
    if ( function_exists( 'senderzz_sz_is_clean_checkout_request' ) && senderzz_sz_is_clean_checkout_request() ) {
        $classes[] = 'senderzz-checkout-canvas';
        $classes[] = 'senderzz-public-checkout';
    }
    return array_values( array_unique( $classes ) );
}, 999 );

add_action( 'wp_head', function(): void {
    if ( ! function_exists( 'senderzz_sz_is_clean_checkout_request' ) || ! senderzz_sz_is_clean_checkout_request() ) return;
    ?>
<script id="senderzz-checkout-canvas-boot">
(function(){document.documentElement.classList.add('senderzz-checkout-canvas','senderzz-public-checkout');})();
</script>
<style id="senderzz-checkout-canvas-v399">
html.senderzz-checkout-canvas,
html.senderzz-checkout-canvas body,
body.senderzz-checkout-canvas{
    margin:0!important;
    padding:0!important;
    width:100%!important;
    min-width:0!important;
    max-width:none!important;
    background:#f8fafc!important;
    overflow-x:hidden!important;
}
html.senderzz-checkout-canvas .wp-site-blocks > header,
html.senderzz-checkout-canvas .wp-site-blocks > footer,
html.senderzz-checkout-canvas header.wp-block-template-part,
html.senderzz-checkout-canvas footer.wp-block-template-part,
html.senderzz-checkout-canvas #masthead,
html.senderzz-checkout-canvas .site-header,
html.senderzz-checkout-canvas #site-header,
html.senderzz-checkout-canvas .elementor-location-header,
html.senderzz-checkout-canvas .elementor-location-footer,
html.senderzz-checkout-canvas .main-navigation,
html.senderzz-checkout-canvas .primary-navigation,
html.senderzz-checkout-canvas .wp-block-navigation,
html.senderzz-checkout-canvas .storefront-primary-navigation,
html.senderzz-checkout-canvas #colophon,
html.senderzz-checkout-canvas .site-footer,
html.senderzz-checkout-canvas #site-footer,
html.senderzz-checkout-canvas .entry-header,
html.senderzz-checkout-canvas .page-header,
html.senderzz-checkout-canvas .entry-title,
html.senderzz-checkout-canvas .page-title,
html.senderzz-checkout-canvas .wp-block-post-title,
html.senderzz-checkout-canvas .entry-meta,
html.senderzz-checkout-canvas .post-meta,
html.senderzz-checkout-canvas .posted-on,
html.senderzz-checkout-canvas .byline,
html.senderzz-checkout-canvas .post-author,
html.senderzz-checkout-canvas .wp-block-post-author,
html.senderzz-checkout-canvas .wp-block-post-author-name,
html.senderzz-checkout-canvas .wp-block-post-date,
html.senderzz-checkout-canvas .taxonomy-category,
html.senderzz-checkout-canvas .taxonomy-post_tag,
html.senderzz-checkout-canvas .comments-area,
html.senderzz-checkout-canvas .post-navigation,
html.senderzz-checkout-canvas .navigation.post-navigation,
html.senderzz-checkout-canvas .edit-link,
body.senderzz-checkout-canvas .wp-site-blocks > header,
body.senderzz-checkout-canvas .wp-site-blocks > footer,
body.senderzz-checkout-canvas header.wp-block-template-part,
body.senderzz-checkout-canvas footer.wp-block-template-part,
body.senderzz-checkout-canvas #masthead,
body.senderzz-checkout-canvas .site-header,
body.senderzz-checkout-canvas #site-header,
body.senderzz-checkout-canvas .elementor-location-header,
body.senderzz-checkout-canvas .elementor-location-footer,
body.senderzz-checkout-canvas .main-navigation,
body.senderzz-checkout-canvas .primary-navigation,
body.senderzz-checkout-canvas .wp-block-navigation,
body.senderzz-checkout-canvas .storefront-primary-navigation,
body.senderzz-checkout-canvas #colophon,
body.senderzz-checkout-canvas .site-footer,
body.senderzz-checkout-canvas #site-footer,
body.senderzz-checkout-canvas .entry-header,
body.senderzz-checkout-canvas .page-header,
body.senderzz-checkout-canvas .entry-title,
body.senderzz-checkout-canvas .page-title,
body.senderzz-checkout-canvas .wp-block-post-title,
body.senderzz-checkout-canvas .entry-meta,
body.senderzz-checkout-canvas .post-meta,
body.senderzz-checkout-canvas .posted-on,
body.senderzz-checkout-canvas .byline,
body.senderzz-checkout-canvas .post-author,
body.senderzz-checkout-canvas .wp-block-post-author,
body.senderzz-checkout-canvas .wp-block-post-author-name,
body.senderzz-checkout-canvas .wp-block-post-date,
body.senderzz-checkout-canvas .taxonomy-category,
body.senderzz-checkout-canvas .taxonomy-post_tag,
body.senderzz-checkout-canvas .comments-area,
body.senderzz-checkout-canvas .post-navigation,
body.senderzz-checkout-canvas .navigation.post-navigation,
body.senderzz-checkout-canvas .edit-link{
    display:none!important;
    visibility:hidden!important;
    height:0!important;
    min-height:0!important;
    margin:0!important;
    padding:0!important;
    overflow:hidden!important;
}
html.senderzz-checkout-canvas .wp-site-blocks,
html.senderzz-checkout-canvas .site,
html.senderzz-checkout-canvas #page,
html.senderzz-checkout-canvas #content,
html.senderzz-checkout-canvas .site-content,
html.senderzz-checkout-canvas .content-area,
html.senderzz-checkout-canvas #primary,
html.senderzz-checkout-canvas .site-main,
html.senderzz-checkout-canvas main,
html.senderzz-checkout-canvas article,
html.senderzz-checkout-canvas .hentry,
html.senderzz-checkout-canvas .entry-content,
html.senderzz-checkout-canvas .is-layout-constrained,
html.senderzz-checkout-canvas .wp-block-group,
body.senderzz-checkout-canvas .wp-site-blocks,
body.senderzz-checkout-canvas .site,
body.senderzz-checkout-canvas #page,
body.senderzz-checkout-canvas #content,
body.senderzz-checkout-canvas .site-content,
body.senderzz-checkout-canvas .content-area,
body.senderzz-checkout-canvas #primary,
body.senderzz-checkout-canvas .site-main,
body.senderzz-checkout-canvas main,
body.senderzz-checkout-canvas article,
body.senderzz-checkout-canvas .hentry,
body.senderzz-checkout-canvas .entry-content,
body.senderzz-checkout-canvas .is-layout-constrained,
body.senderzz-checkout-canvas .wp-block-group{
    width:100%!important;
    max-width:none!important;
    min-width:0!important;
    margin:0!important;
    padding:0!important;
    border:0!important;
    box-shadow:none!important;
}
html.senderzz-checkout-canvas .wp-site-blocks > *,
html.senderzz-checkout-canvas .entry-content > *,
body.senderzz-checkout-canvas .wp-site-blocks > *,
body.senderzz-checkout-canvas .entry-content > *{
    max-width:none!important;
    margin-block-start:0!important;
    margin-block-end:0!important;
}
html.senderzz-checkout-canvas .woocommerce,
html.senderzz-checkout-canvas .woocommerce-page,
html.senderzz-checkout-canvas .wfacp_main_form,
body.senderzz-checkout-canvas .woocommerce,
body.senderzz-checkout-canvas .woocommerce-page,
body.senderzz-checkout-canvas .wfacp_main_form{
    max-width:none!important;
    margin-top:0!important;
}
html.senderzz-checkout-canvas .woocommerce-breadcrumb,
body.senderzz-checkout-canvas .woocommerce-breadcrumb{
    display:none!important;
}
@media (max-width:780px){
    html.senderzz-checkout-canvas #wpadminbar + *,
    body.senderzz-checkout-canvas #wpadminbar + *{margin-top:0!important;}
}
</style>
    <?php
}, 0 );

add_action( 'wp_footer', function(): void {
    if ( ! function_exists( 'senderzz_sz_is_clean_checkout_request' ) || ! senderzz_sz_is_clean_checkout_request() ) return;
    ?>
<script id="senderzz-checkout-canvas-clean-v399">
(function(){
  document.documentElement.classList.add('senderzz-checkout-canvas','senderzz-public-checkout');
  if(document.body){document.body.classList.add('senderzz-checkout-canvas','senderzz-public-checkout');}
  var selectors=[
    '.wp-site-blocks > header','.wp-site-blocks > footer','header.wp-block-template-part','footer.wp-block-template-part',
    '#masthead','.site-header','#site-header','.elementor-location-header','.elementor-location-footer',
    '.main-navigation','.primary-navigation','.wp-block-navigation','.storefront-primary-navigation',
    '#colophon','.site-footer','#site-footer','.entry-header','.page-header','.entry-title','.page-title',
    '.wp-block-post-title','.entry-meta','.post-meta','.posted-on','.byline','.post-author',
    '.wp-block-post-author','.wp-block-post-author-name','.wp-block-post-date','.taxonomy-category','.taxonomy-post_tag',
    '.comments-area','.post-navigation','.navigation.post-navigation','.edit-link','.woocommerce-breadcrumb'
  ];
  function clean(){
    selectors.forEach(function(sel){
      document.querySelectorAll(sel).forEach(function(el){
        if(!el || el.closest('.wfacp_main_form,.woocommerce-checkout,form.checkout')) return;
        el.style.setProperty('display','none','important');
        el.style.setProperty('height','0','important');
        el.style.setProperty('margin','0','important');
        el.style.setProperty('padding','0','important');
        el.style.setProperty('overflow','hidden','important');
      });
    });
  }
  clean();
  setTimeout(clean,120);
  setTimeout(clean,600);
})();
</script>
    <?php
}, 9999 );


/**
 * v400 — limpeza complementar do canvas público.
 * Remove sobras de block theme ("Escrito por", "Mais posts", admin bar) sem alterar o fluxo do checkout.
 */
add_action( 'wp_head', function(): void {
    if ( ! function_exists( 'senderzz_sz_is_clean_checkout_request' ) || ! senderzz_sz_is_clean_checkout_request() ) return;
    ?>
<style id="senderzz-checkout-canvas-v400">
html.senderzz-checkout-canvas{
    margin-top:0!important;
    background:#f8fafc!important;
}
html.senderzz-checkout-canvas body,
body.senderzz-checkout-canvas{
    background:#f8fafc!important;
    min-height:100vh!important;
    margin-top:0!important;
    padding-top:0!important;
}
html.senderzz-checkout-canvas body.admin-bar,
body.senderzz-checkout-canvas.admin-bar{
    margin-top:0!important;
    padding-top:0!important;
}
html.senderzz-checkout-canvas #wpadminbar,
body.senderzz-checkout-canvas #wpadminbar{
    display:none!important;
    visibility:hidden!important;
    height:0!important;
    min-height:0!important;
    overflow:hidden!important;
}
html.senderzz-checkout-canvas .wp-block-post-author__content,
html.senderzz-checkout-canvas .wp-block-post-author__byline,
html.senderzz-checkout-canvas .wp-block-post-author__name,
html.senderzz-checkout-canvas .wp-block-post-author__avatar,
html.senderzz-checkout-canvas .wp-block-post-navigation-link,
html.senderzz-checkout-canvas .wp-block-post-terms,
html.senderzz-checkout-canvas .wp-block-template-part,
body.senderzz-checkout-canvas .wp-block-post-author__content,
body.senderzz-checkout-canvas .wp-block-post-author__byline,
body.senderzz-checkout-canvas .wp-block-post-author__name,
body.senderzz-checkout-canvas .wp-block-post-author__avatar,
body.senderzz-checkout-canvas .wp-block-post-navigation-link,
body.senderzz-checkout-canvas .wp-block-post-terms,
body.senderzz-checkout-canvas .wp-block-template-part{
    display:none!important;
    visibility:hidden!important;
    height:0!important;
    margin:0!important;
    padding:0!important;
    overflow:hidden!important;
}
html.senderzz-checkout-canvas .wp-block-group:has(.wp-block-post-author):not(:has(.wfacp_main_form)):not(:has(form.checkout)):not(:has(.woocommerce-checkout)),
html.senderzz-checkout-canvas .wp-block-group:has(.wp-block-post-date):not(:has(.wfacp_main_form)):not(:has(form.checkout)):not(:has(.woocommerce-checkout)),
html.senderzz-checkout-canvas .wp-block-group:has(.wp-block-query):not(:has(.wfacp_main_form)):not(:has(form.checkout)):not(:has(.woocommerce-checkout)),
html.senderzz-checkout-canvas .wp-block-query,
html.senderzz-checkout-canvas .wp-block-query-title,
html.senderzz-checkout-canvas .wp-block-query-pagination,
html.senderzz-checkout-canvas .wp-block-latest-posts,
html.senderzz-checkout-canvas .related,
html.senderzz-checkout-canvas .related-posts,
html.senderzz-checkout-canvas .yarpp-related,
body.senderzz-checkout-canvas .wp-block-group:has(.wp-block-post-author):not(:has(.wfacp_main_form)):not(:has(form.checkout)):not(:has(.woocommerce-checkout)),
body.senderzz-checkout-canvas .wp-block-group:has(.wp-block-post-date):not(:has(.wfacp_main_form)):not(:has(form.checkout)):not(:has(.woocommerce-checkout)),
body.senderzz-checkout-canvas .wp-block-group:has(.wp-block-query):not(:has(.wfacp_main_form)):not(:has(form.checkout)):not(:has(.woocommerce-checkout)),
body.senderzz-checkout-canvas .wp-block-query,
body.senderzz-checkout-canvas .wp-block-query-title,
body.senderzz-checkout-canvas .wp-block-query-pagination,
body.senderzz-checkout-canvas .wp-block-latest-posts,
body.senderzz-checkout-canvas .related,
body.senderzz-checkout-canvas .related-posts,
body.senderzz-checkout-canvas .yarpp-related{
    display:none!important;
    visibility:hidden!important;
    height:0!important;
    min-height:0!important;
    margin:0!important;
    padding:0!important;
    overflow:hidden!important;
}
html.senderzz-checkout-canvas .entry-content,
body.senderzz-checkout-canvas .entry-content{
    background:#f8fafc!important;
    box-sizing:border-box!important;
    min-height:100vh!important;
    padding:24px 16px 56px!important;
}
html.senderzz-checkout-canvas .wfacp_main_form,
body.senderzz-checkout-canvas .wfacp_main_form,
html.senderzz-checkout-canvas form.checkout,
body.senderzz-checkout-canvas form.checkout{
    position:relative!important;
    z-index:5!important;
}
html.senderzz-checkout-canvas .wfacp_main_form,
body.senderzz-checkout-canvas .wfacp_main_form{
    margin-left:auto!important;
    margin-right:auto!important;
}
@media (max-width: 780px){
    html.senderzz-checkout-canvas .entry-content,
    body.senderzz-checkout-canvas .entry-content{padding:16px 10px 42px!important;}
}
</style>
<script id="senderzz-checkout-canvas-clean-v400">
(function(){
  document.documentElement.classList.add('senderzz-checkout-canvas','senderzz-public-checkout');
  var checkoutSelector='.wfacp_main_form,.woocommerce-checkout,form.checkout,#order_review,.woocommerce';
  function insideCheckout(el){
    return !!(el && el.closest && el.closest(checkoutSelector));
  }
  function containsCheckout(el){
    return !!(el && el.querySelector && el.querySelector(checkoutSelector));
  }
  function outsideCheckout(el){
    return !!(el && !insideCheckout(el));
  }
  function canHideElement(el){
    return !!(el && !insideCheckout(el) && !containsCheckout(el));
  }
  function hideElement(el){
    if(!el || !canHideElement(el)) return;
    el.style.setProperty('display','none','important');
    el.style.setProperty('visibility','hidden','important');
    el.style.setProperty('height','0','important');
    el.style.setProperty('min-height','0','important');
    el.style.setProperty('margin','0','important');
    el.style.setProperty('padding','0','important');
    el.style.setProperty('overflow','hidden','important');
  }
  function textClean(){
    if(document.body){document.body.classList.add('senderzz-checkout-canvas','senderzz-public-checkout');}
    var selectors=[
      '#wpadminbar','.wp-block-post-author__content','.wp-block-post-author__byline','.wp-block-post-author__name',
      '.wp-block-post-author','.wp-block-post-date','.wp-block-post-terms','.wp-block-query','.wp-block-latest-posts',
      '.related','.related-posts','.yarpp-related','.comments-area','.post-navigation','.navigation.post-navigation',
      '.entry-meta','.post-meta','.posted-on','.byline','.post-author','.woocommerce-breadcrumb'
    ];
    selectors.forEach(function(sel){document.querySelectorAll(sel).forEach(hideElement);});

    document.querySelectorAll('h1,h2,h3,h4,h5,h6,p,div,section,aside').forEach(function(el){
      if(!outsideCheckout(el)) return;
      var txt=(el.textContent||'').replace(/\s+/g,' ').trim();
      if(!txt) return;
      if(/^(MAIS POSTS|MAIS POSTAGENS|MORE POSTS)$/i.test(txt) || /^Escrito\s+por\b/i.test(txt)){
        hideElement(el);
      }
    });

    try{
      var walker=document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT, {
        acceptNode:function(node){
          var parent=node.parentElement;
          if(!outsideCheckout(parent)) return NodeFilter.FILTER_REJECT;
          return /(Escrito\s+por|MAIS\s+POSTS|MAIS\s+POSTAGENS|MORE\s+POSTS)/i.test(node.nodeValue||'') ? NodeFilter.FILTER_ACCEPT : NodeFilter.FILTER_REJECT;
        }
      });
      var nodes=[], n;
      while((n=walker.nextNode())) nodes.push(n);
      nodes.forEach(function(node){node.nodeValue='';});
    }catch(e){}
  }
  if(document.readyState==='loading') document.addEventListener('DOMContentLoaded', textClean, {once:true});
  textClean();
  requestAnimationFrame(textClean);
  setTimeout(textClean,80);
  setTimeout(textClean,350);
  setTimeout(textClean,900);
})();
</script>
    <?php
}, 1 );


function senderzz_sz_component_qty_for_product( array $components, int $product_id, int $variation_id = 0 ): int {
    foreach ( $components as $comp ) {
        $pid = absint( $comp['pid'] ?? 0 );
        $vid = absint( $comp['vid'] ?? 0 );
        $qty = max( 1, absint( $comp['qty'] ?? 1 ) );

        if ( $variation_id > 0 && $vid === $variation_id ) {
            return $qty;
        }

        if ( $variation_id <= 0 && $pid === $product_id ) {
            return $qty;
        }

        if ( $vid > 0 && $pid === $product_id ) {
            return $qty;
        }
    }

    return 0;
}

add_action( 'wp_loaded', function () {
    senderzz_sz_get_data();
}, 1 );

// Popula o carrinho ANTES do template_redirect verificar se está vazio.
// Necessário porque o FunnelKit redireciona para home se cart->is_empty() no template_redirect.
add_action( 'wp_loaded', function () {
    $data = senderzz_sz_get_data();
    if ( empty( $data ) || empty( $data['components'] ) ) return;
    if ( ! function_exists( 'WC' ) || ! WC()->cart ) return;

    // Só popula se o carrinho está vazio — evita duplicar itens
    if ( ! WC()->cart->is_empty() ) {
        // Verifica se já tem o token correto no carrinho
        foreach ( WC()->cart->get_cart() as $item ) {
            if ( ( $item['senderzz_offer_token'] ?? '' ) === $data['token'] ) return;
        }
        WC()->cart->empty_cart();
    }

    $components  = $data['components'];
    $valor       = (float) $data['valor'];
    $name        = (string) $data['name'];
    $token       = (string) $data['token'];
    $count       = max( 1, count( $components ) );
    $total_units = 0;
    foreach ( $components as $c ) { $total_units += max( 1, absint( $c['qty'] ?? 1 ) ); }
    $total_units = max( 1, $total_units );

    foreach ( $components as $comp ) {
        $pid = absint( $comp['pid'] ?? 0 );
        $vid = absint( $comp['vid'] ?? 0 );
        $qty = max( 1, absint( $comp['qty'] ?? 1 ) );
        if ( ! $pid ) continue;
        $cart_item_data = [
            'senderzz_sz_link'            => 'yes',
            'senderzz_offer_name'         => $name,
            'senderzz_offer_value'        => $valor,
            'senderzz_offer_token'        => $token,
            'senderzz_combo_total'        => $valor,
            'senderzz_combo_count'        => $count,
            'senderzz_combo_total_units'  => $total_units,
        ];
        WC()->cart->add_to_cart( $pid, $qty, $vid, [], $cart_item_data );
    }
}, 99 ); // priority 99 — depois do WC inicializar sessão/cart

add_action( 'woocommerce_cart_loaded_from_session', function () {
    $data = senderzz_sz_get_data();

    if ( empty( $data ) || empty( $data['components'] ) ) return;
    if ( ! WC()->cart ) return;

    $components = $data['components'];
    $valor      = (float) $data['valor'];
    $name       = (string) $data['name'];
    $token      = (string) $data['token'];
    $count      = max( 1, count( $components ) );
    $total_units = 0;
    foreach ( $components as $c ) {
        $total_units += max( 1, absint( $c['qty'] ?? 1 ) );
    }
    $total_units = max( 1, $total_units );

    WC()->cart->empty_cart();

    foreach ( $components as $comp ) {
        $pid = absint( $comp['pid'] ?? 0 );
        $vid = absint( $comp['vid'] ?? 0 );
        $qty = max( 1, absint( $comp['qty'] ?? 1 ) );

        if ( ! $pid ) continue;

        $cart_item_data = [
            'senderzz_sz_link'     => 'yes',
            'senderzz_offer_name'  => $name,
            'senderzz_offer_value' => $valor,
            'senderzz_offer_token' => $token,
        ];

        if ( $valor > 0 ) {
            $cart_item_data['senderzz_combo_total'] = $valor;
            $cart_item_data['senderzz_combo_count'] = $count;
            $cart_item_data['senderzz_combo_total_units'] = $total_units;
        }

        WC()->cart->add_to_cart( $pid, $qty, $vid, [], $cart_item_data );
    }
} );

add_action( 'woocommerce_before_calculate_totals', function ( $cart ) {
    if ( is_admin() && ! defined( 'DOING_AJAX' ) ) return;
    if ( ! $cart ) return;

    foreach ( $cart->get_cart() as $item ) {
        if ( empty( $item['senderzz_combo_total'] ) ) continue;
        if ( empty( $item['data'] ) || ! is_object( $item['data'] ) ) continue;

        $total_units = max( 1, (int) ( $item['senderzz_combo_total_units'] ?? $item['senderzz_combo_count'] ?? 1 ) );
        $item['data']->set_price( round( (float) $item['senderzz_combo_total'] / $total_units, 6 ) );
    }
}, 99 );

add_filter( 'get_post_metadata', function ( $value, $post_id, $meta_key, $single ) {
    $data = senderzz_sz_get_data();

    if ( empty( $data ) ) return $value;
    if ( $meta_key !== '_wfacp_selected_products' ) return $value;
    if ( (int) $post_id !== (int) $data['post_id'] ) return $value;

    $components = is_array( $data['components'] ?? null ) ? $data['components'] : [];
    if ( empty( $components ) ) return $value;

    $valor = (float) ( $data['valor'] ?? 0 );
    $name  = (string) ( $data['name'] ?? '' );
    $token = (string) ( $data['token'] ?? '' );

    $result       = [];
    $total_units  = 0;
    foreach ( $components as $comp ) {
        $total_units += max( 1, absint( $comp['qty'] ?? 1 ) );
    }
    $unit_price = $valor > 0 && $total_units > 0 ? round( $valor / $total_units, 6 ) : 0;

    foreach ( $components as $idx => $comp ) {
        $pid = absint( $comp['pid'] ?? 0 );
        $vid = absint( $comp['vid'] ?? 0 );
        $qty = max( 1, absint( $comp['qty'] ?? 1 ) );
        if ( ! $pid ) continue;

        $real = $vid ?: $pid;
        $prod = $real ? wc_get_product( $real ) : null;
        if ( ! $prod ) continue;

        $price = $unit_price > 0 ? $unit_price : (float) $prod->get_price();
        $key   = 'wfacp_sz_' . substr( md5( $token . '|' . $pid . '|' . $vid . '|' . $idx ), 0, 13 );

        $result[ $key ] = [
            'title'                => function_exists('senderzz_clean_product_label') ? senderzz_clean_product_label( $idx === 0 && $name ? $name : $prod->get_name() ) : ( $idx === 0 && $name ? $name : $prod->get_name() ),
            'discount_type'        => 'fixed_price',
            'discount_amount'      => '0',
            'discount_price'       => '0',
            'quantity'             => (string) $qty,
            'type'                 => $vid ? 'variable' : 'simple',
            'id'                   => (string) $real,
            'parent_product_id'    => $vid ? (string) $pid : '0',
            'stock'                => '1',
            'is_sold_individually' => '0',
            'product_image'        => 'https://app.senderzz.com.br/wp-content/plugins/funnel-builder/admin/assets/img/product_default_icon.jpg',
            'product_type'         => $vid ? 'variable' : 'simple',
            'product_attribute'    => '-',
            'is_on_sale'           => '',
            'currency_symbol'      => '&#82;&#36;',
            'product_stock_status' => '1',
            'product_stock'        => 'in-stock',
            'price_range'          => '',
            'product_status'       => 'publish',
            'price'                => $price,
            'sale_price'           => $price,
            'regular_price'        => $price,
            'qty'                  => $qty,
            'default'              => $idx === 0 ? 'yes' : 'no',
            'is_hide'              => 'no',
            'variation_id'         => $vid,
            'product_id'           => $pid,
        ];
    }

    if ( empty( $result ) ) return $value;

    return $single ? [ $result ] : $result;
}, 10, 4 );

// Renderiza hidden fields — barra injetada via JS no checkout-link-motoboy.php
add_action( 'woocommerce_before_checkout_form', function() {
    $data = senderzz_sz_get_data();
    if ( empty( $data['name'] ) ) return;
    $name  = (string) $data['name'];
    $valor = (float) ( $data['valor'] ?? 0 );
    $token = (string) ( $data['token'] ?? '' );
    echo '<input type="hidden" name="senderzz_offer_name" value="' . esc_attr( $name ) . '">';
    echo '<input type="hidden" name="senderzz_offer_value" value="' . esc_attr( $valor ) . '">';
    echo '<input type="hidden" name="senderzz_offer_token" value="' . esc_attr( $token ) . '">';
}, 5 );

add_action( 'wp_footer', function () {
    if ( ! is_checkout() ) return;

    $data = senderzz_sz_get_data();
    if ( empty( $data['name'] ) ) return;

    $name  = esc_js( (string) $data['name'] );
    $valor = esc_js( (string) $data['valor'] );
    $token = esc_js( (string) $data['token'] );
?>
<script>
(function(){
    var offer = {
        name: "<?php echo $name; ?>",
        value: "<?php echo $valor; ?>",
        token: "<?php echo $token; ?>"
    };

    try {
        sessionStorage.setItem("senderzz_offer_name", offer.name);
        sessionStorage.setItem("senderzz_offer_value", offer.value);
        sessionStorage.setItem("senderzz_offer_token", offer.token);
        localStorage.setItem("senderzz_offer_name", offer.name);
        localStorage.setItem("senderzz_offer_value", offer.value);
        localStorage.setItem("senderzz_offer_token", offer.token);
    } catch(e) {}

    function addHidden(form, name, value){
        if (!form || !name) return;
        var input = form.querySelector('input[name="'+name+'"]');
        if (!input) {
            input = document.createElement("input");
            input.type = "hidden";
            input.name = name;
            form.appendChild(input);
        }
        input.value = value || "";
    }

    function inject(){
        document.querySelectorAll("form").forEach(function(form){
            addHidden(form, "senderzz_offer_name", offer.name);
            addHidden(form, "senderzz_offer_value", offer.value);
            addHidden(form, "senderzz_offer_token", offer.token);
        });
    }

    inject();
    setInterval(inject, 500);
})();
</script>
<?php
}, 99 );

function senderzz_sz_save_offer_to_order( $order_id_or_order ) {
    $order = $order_id_or_order instanceof WC_Order ? $order_id_or_order : wc_get_order( $order_id_or_order );
    if ( ! $order ) return;

    $data = senderzz_sz_get_data();

    $post_name  = sanitize_text_field( wp_unslash( $_POST['senderzz_offer_name'] ?? '' ) );
    $post_value = (float) sanitize_text_field( wp_unslash( $_POST['senderzz_offer_value'] ?? 0 ) );
    $post_token = sanitize_text_field( wp_unslash( $_POST['senderzz_offer_token'] ?? '' ) );

    $name  = $post_name ?: sanitize_text_field( $data['name'] ?? '' );
    $token = $post_token ?: sanitize_text_field( $data['token'] ?? '' );

    // SEGURANÇA: valor da oferta SEMPRE vem do banco via token — nunca do POST/cookie.
    // Isso previne price manipulation onde o comprador altera o campo hidden no formulário.
    $valor = (float) ( $data['valor'] ?? 0 );
    if ( $valor <= 0 && $token !== '' ) {
        // Recarregar do DB pelo token como segurança extra
        $db_data = senderzz_sz_load_data( $token );
        $valor = $db_data ? (float) ( $db_data['valor'] ?? 0 ) : 0;
    }
    // Fallback final: se ainda não temos valor do DB, ignora o POST (não grava valor manipulável)
    // Nota: $post_value é intencionalmente ignorado — o valor legítimo vem exclusivamente do DB.

    if ( $name !== '' ) {
        $order->update_meta_data( '_senderzz_offer_name', $name );
    }

    if ( $valor > 0 ) {
        $order->update_meta_data( '_senderzz_offer_value', $valor );
    }

    if ( $token !== '' ) {
        $order->update_meta_data( '_senderzz_offer_token', $token );
    }

    if ( $name !== '' || $valor > 0 || $token !== '' ) {
        $order->save();
    }
}

add_action( 'woocommerce_checkout_create_order', function ( $order, $data ) {
    senderzz_sz_save_offer_to_order( $order );
}, 9999, 2 );

add_action( 'woocommerce_checkout_update_order_meta', function ( $order_id ) {
    senderzz_sz_save_offer_to_order( $order_id );
}, 9999 );

add_action( 'woocommerce_new_order', function ( $order_id ) {
    senderzz_sz_save_offer_to_order( $order_id );
}, 9999 );

add_action( 'woocommerce_checkout_create_order_line_item', function ( $item, $cart_item_key, $values, $order ) {
    $data = senderzz_sz_get_data();

    $offer_name  = sanitize_text_field( $values['senderzz_offer_name'] ?? ( $data['name'] ?? '' ) );
    $offer_value = (float) ( $values['senderzz_offer_value'] ?? ( $data['valor'] ?? 0 ) );
    $offer_token = sanitize_text_field( $values['senderzz_offer_token'] ?? ( $data['token'] ?? '' ) );

    if ( $offer_name !== '' ) {
        $item->add_meta_data( '_senderzz_offer_name', $offer_name, true );
    }

    if ( $offer_value > 0 ) {
        $item->add_meta_data( '_senderzz_offer_value', $offer_value, true );
    }

    if ( $offer_token !== '' ) {
        $item->add_meta_data( '_senderzz_offer_token', $offer_token, true );
    }
}, 9999, 4 );

add_action( 'wp_head', function () {
    if ( ! is_checkout() ) return;
?>
<style id="senderzz-checkout-style-fix">
    :root {
        --sz-checkout-font:var(--sz-font);
    }

    body,
    button,
    input,
    textarea,
    select,
    .woocommerce,
    .woocommerce *,
    .wfacp_main_form,
    .wfacp_main_form *,
    .senderzz-checkout-offer-title,
    .senderzz-checkout-offer-title * {
        font-family: var(--sz-font);
        text-rendering: optimizeLegibility;
        -webkit-font-smoothing: antialiased;
        -moz-osx-font-smoothing: grayscale;
    }
/* Oculta "Visualizar Compra" (order summary toggle) */
.wfacp_order_summary_toggle,
.wfacp_order_summary_header,
.wfacp_order_summary_container,
.wfacp_order_summary_wrap,
.wfacp_order_summary {
    display: none !important;
}
    .senderzz-checkout-offer-bar {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: 14px;
        background: linear-gradient(135deg, #E8650A, #c8540a);
        border-radius: 18px;
        padding: 12px 16px;
        margin: 12px 0 14px;
        min-height: 58px;
        box-shadow: none;
    }
    .senderzz-checkout-offer-bar-name {
        color: #fff;
        font-size: var(--sz-text-md);
        font-weight: 700;
        flex: 1;
        min-width: 0;
        line-height: 1.15;
        white-space: nowrap;
        overflow: hidden;
        text-overflow: ellipsis;
    }
    .senderzz-checkout-offer-bar-name:before {
        content: '📦 Oferta selecionada';
        display: block;
        margin-bottom: 3px;
        color: rgba(255,255,255,.76);
        font-size: 11px;
        font-weight: 700;
        text-transform: none;
        letter-spacing:0;
    }
    .senderzz-checkout-offer-bar-price {
        color: #fff;
        font-size: var(--sz-text-lg);
        font-weight: 700;
        background: rgba(255,255,255,.2);
        padding: 7px 14px;
        border-radius: 999px;
        white-space: nowrap;
    }/* Oculta seleção/lista de produtos do FunnelKit */
.wfacp_product_switcher,
.wfacp_product_switcher_container,
.wfacp_product_switcher_wrap,
.wfacp_product_sec,
.wfacp_order_summary_product_switcher,
.wfacp_product_switcher_table,
.wfacp_ps_div,
.wfacp-product-switcher,
.wfacp-product-switcher-wrapper {
    display: none !important;
}

/* fallback pelo layout da tabela Produtos / Preço */
.wfacp_main_form table:has(th),
.wfacp_main_form .wfacp_order_summary,
.wfacp_main_form .wfacp_order_summary_container {
    display: none !important;
}

    .senderzz-checkout-price-badge {
        display: flex;
        align-items: center;
        justify-content: center;
        margin: 14px auto 0;
        width: fit-content;
        min-width: 112px;
        padding: 10px 20px;
        border-radius: 999px;
        background: linear-gradient(135deg, #ff8a00, #ff4d00);
        color: #fff;
        font-size: var(--sz-text-lg);
        font-weight: 700;
        vertical-align: middle;
        white-space: nowrap;
        box-shadow: 0 10px 20px rgba(255, 106, 0, .25);
    }

    @media (max-width: 640px) {
        .senderzz-checkout-offer-title {
            margin: 0 12px 16px;
            padding: 16px;
        }

        .senderzz-checkout-offer-title h2 {
            font-size: var(--sz-text-xl);
        }

        .senderzz-checkout-price-badge {
            display: block;
            margin: 10px auto 0;
            width: fit-content;
            font-size: var(--sz-text-lg);
            padding: 7px 12px;
        }
    }
</style>
<?php
}, 1 );
add_action( 'wp_footer', function () {
    if ( ! is_checkout() ) return;
?>
<script>
(function(){
    function hideFunnelKitProducts(){
        var selectors = [
            '.wfacp_product_switcher',
            '.wfacp_product_switcher_container',
            '.wfacp_product_switcher_wrap',
            '.wfacp_product_sec',
            '.wfacp_order_summary_product_switcher',
            '.wfacp_product_switcher_table',
            '.wfacp_ps_div',
            '.wfacp-product-switcher',
            '.wfacp-product-switcher-wrapper'
        ];

        selectors.forEach(function(sel){
            document.querySelectorAll(sel).forEach(function(el){
                el.style.display = 'none';
            });
        });

        document.querySelectorAll('*').forEach(function(el){
            var txt = (el.innerText || '').trim();
            if (txt.includes('Produtos') && txt.includes('Preço') && txt.length < 300) {
                el.style.display = 'none';
            }
        });
    }

    hideFunnelKitProducts();
    setInterval(hideFunnelKitProducts, 500);
})();
</script>
<?php
}, 100 );
/**
 * Força o item do pedido a salvar com o nome/valor do kit Senderzz.
 */
add_action( 'woocommerce_checkout_create_order_line_item', function ( $item, $cart_item_key, $values, $order ) {

    $offer_name = sanitize_text_field(
        $values['senderzz_offer_name']
        ?? $_POST['senderzz_offer_name']
        ?? $_COOKIE['senderzz_offer_name']
        ?? ''
    );

    $offer_value = (float) (
        $values['senderzz_offer_value']
        ?? $_POST['senderzz_offer_value']
        ?? $_COOKIE['senderzz_offer_value']
        ?? 0
    );

    $offer_token = sanitize_text_field(
        $values['senderzz_offer_token']
        ?? $_POST['senderzz_offer_token']
        ?? $_COOKIE['senderzz_offer_token']
        ?? ''
    );

    if ( $offer_token !== '' ) {
        $link_data = senderzz_sz_load_data( $offer_token );
        if ( ! empty( $link_data['components'] ) && is_array( $link_data['components'] ) ) {
            $desired_qty = senderzz_sz_component_qty_for_product(
                $link_data['components'],
                absint( $item->get_product_id() ),
                absint( method_exists( $item, 'get_variation_id' ) ? $item->get_variation_id() : 0 )
            );
            if ( $desired_qty > 0 && absint( $item->get_quantity() ) !== $desired_qty ) {
                $item->set_quantity( $desired_qty );
            }
            $item->update_meta_data( '_senderzz_component_qty', $desired_qty ?: absint( $item->get_quantity() ) );
        }
    }

    if ( $offer_name !== '' ) {
        // ESSENCIAL: altera o nome real do item no pedido
        $item->set_name( $offer_name );

        $item->update_meta_data( '_senderzz_offer_name', $offer_name );
    }

    if ( $offer_value > 0 ) {
        $item->update_meta_data( '_senderzz_offer_value', $offer_value );
    }

    if ( $offer_token !== '' ) {
        $item->update_meta_data( '_senderzz_offer_token', $offer_token );
    }

}, 99999, 4 );


/**
 * Salva também no pedido principal.
 */
add_action( 'woocommerce_checkout_create_order', function ( $order, $data ) {

    $offer_name = sanitize_text_field(
        $_POST['senderzz_offer_name']
        ?? $_COOKIE['senderzz_offer_name']
        ?? ''
    );

    $offer_value = (float) (
        $_POST['senderzz_offer_value']
        ?? $_COOKIE['senderzz_offer_value']
        ?? 0
    );

    $offer_token = sanitize_text_field(
        $_POST['senderzz_offer_token']
        ?? $_COOKIE['senderzz_offer_token']
        ?? ''
    );

    if ( $offer_name !== '' ) {
        $order->update_meta_data( '_senderzz_offer_name', $offer_name );
    }

    if ( $offer_value > 0 ) {
        $order->update_meta_data( '_senderzz_offer_value', $offer_value );
    }

    if ( $offer_token !== '' ) {
        $order->update_meta_data( '_senderzz_offer_token', $offer_token );
    }

}, 99999, 2 );
add_action( 'wp_footer', function () {
    if ( ! is_checkout() ) return;
?>
<script>
(function(){
    function hideSummary(){
        document.querySelectorAll(
            '.wfacp_order_summary_toggle, .wfacp_order_summary, .wfacp_order_summary_container'
        ).forEach(function(el){
            el.style.display = 'none';
        });

        document.querySelectorAll('*').forEach(function(el){
            if ((el.innerText || '').includes('Visualizar Compra')) {
                el.style.display = 'none';
            }
        });
    }

    hideSummary();
    setInterval(hideSummary, 500);
})();
</script>
<?php
}, 100 );
/**
 * Sincroniza os componentes reais do link personalizado no pedido.
 * Motivo: alguns checkouts FunnelKit criam apenas o primeiro produto do link.
 * Aqui garantimos que todos os componentes do payload entrem no pedido com a quantidade correta,
 * para que a baixa de estoque do WooCommerce desconte pomadas, gotas e demais itens do kit.
 */
if ( ! function_exists( 'senderzz_sz_sync_order_components_from_token' ) ) {
    function senderzz_sz_sync_order_components_from_token( $order, string $token = '' ): void {
        if ( ! $order instanceof WC_Order ) return;

        $token = $token ?: (string) $order->get_meta( '_senderzz_offer_token' );
        $token = sanitize_text_field( $token );
        if ( $token === '' ) return;

        $link_data = senderzz_sz_load_data( $token );
        if ( empty( $link_data['components'] ) || ! is_array( $link_data['components'] ) ) return;

        $components  = $link_data['components'];
        $offer_name  = sanitize_text_field( $link_data['name'] ?? '' );
        $offer_value = (float) ( $link_data['valor'] ?? 0 );

        // Mapa oficial do link: somente estes produtos-base podem ficar no pedido.
        // Nunca deixar o produto "kit"/modelo do checkout permanecer como item real,
        // senão o Woo baixa o kit e/ou o módulo de kits baixa componentes novamente.
        $desired = [];
        $total_units = 0;

        foreach ( $components as $idx => $comp ) {
            $pid = absint( $comp['pid'] ?? 0 );
            $vid = absint( $comp['vid'] ?? 0 );
            $qty = max( 1, absint( $comp['qty'] ?? 1 ) );
            if ( ! $pid ) continue;

            $key = $vid > 0 ? 'v:' . $vid : 'p:' . $pid;
            if ( ! isset( $desired[ $key ] ) ) {
                $desired[ $key ] = [
                    'pid' => $pid,
                    'vid' => $vid,
                    'qty' => 0,
                    'idx' => $idx,
                ];
            }
            $desired[ $key ]['qty'] += $qty;
            $total_units += $qty;
        }

        if ( empty( $desired ) ) return;

        $unit_price = $offer_value > 0 && $total_units > 0 ? round( $offer_value / $total_units, 6 ) : 0.0;
        $changed = false;
        $kept    = [];

        // Primeira passada: remove item kit/modelo e duplicatas; mantém só componentes base.
        foreach ( $order->get_items( 'line_item' ) as $item_id => $item ) {
            if ( ! $item instanceof WC_Order_Item_Product ) continue;

            $item_product_id   = absint( $item->get_product_id() );
            $item_variation_id = absint( $item->get_variation_id() );
            $item_key          = $item_variation_id > 0 ? 'v:' . $item_variation_id : 'p:' . $item_product_id;

            if ( ! isset( $desired[ $item_key ] ) ) {
                // Pedido vindo de link personalizado: qualquer item fora do payload é kit/modelo do checkout.
                $order->remove_item( $item_id );
                $changed = true;
                continue;
            }

            if ( isset( $kept[ $item_key ] ) ) {
                // Se o mesmo componente entrou duplicado, remove o extra para não baixar estoque duas vezes.
                $order->remove_item( $item_id );
                $changed = true;
                continue;
            }

            $kept[ $item_key ] = $item;
        }

        foreach ( $desired as $key => $comp ) {
            $pid = absint( $comp['pid'] );
            $vid = absint( $comp['vid'] );
            $qty = max( 1, absint( $comp['qty'] ) );

            $target_product_id = $vid ?: $pid;
            $product = wc_get_product( $target_product_id );
            if ( ! $product ) continue;

            $line_total = $unit_price > 0 ? round( $unit_price * $qty, 2 ) : 0;
            $found_item = $kept[ $key ] ?? null;

            if ( $found_item ) {
                if ( absint( $found_item->get_quantity() ) !== $qty ) {
                    $found_item->set_quantity( $qty );
                    $changed = true;
                }

                // Manter produto base real para estoque; nome comercial fica em meta/portal.
                $found_item->set_name( function_exists('senderzz_clean_product_label') ? senderzz_clean_product_label( $product->get_name() ) : $product->get_name() );

                if ( $offer_value > 0 ) {
                    if ( (float) $found_item->get_total() !== (float) $line_total || (float) $found_item->get_subtotal() !== (float) $line_total ) {
                        $found_item->set_subtotal( $line_total );
                        $found_item->set_total( $line_total );
                        $changed = true;
                    }
                }

                $found_item->update_meta_data( '_senderzz_component_qty', $qty );
                $found_item->update_meta_data( '_senderzz_offer_token', $token );
                $found_item->update_meta_data( '_senderzz_is_base_component', 'yes' );
                if ( $offer_name !== '' ) {
                    $found_item->update_meta_data( '_senderzz_offer_name', $offer_name );
                }
                if ( $offer_value > 0 ) {
                    $found_item->update_meta_data( '_senderzz_offer_value', $offer_value );
                }
                $found_item->save();
                continue;
            }

            $item = new WC_Order_Item_Product();
            $item->set_product( $product );
            $item->set_quantity( $qty );
            $item->set_name( function_exists('senderzz_clean_product_label') ? senderzz_clean_product_label( $product->get_name() ) : $product->get_name() );
            $item->set_subtotal( $line_total );
            $item->set_total( $line_total );

            $item->add_meta_data( '_senderzz_component_qty', $qty, true );
            $item->add_meta_data( '_senderzz_offer_token', $token, true );
            $item->add_meta_data( '_senderzz_is_base_component', 'yes', true );
            if ( $offer_name !== '' ) {
                $item->add_meta_data( '_senderzz_offer_name', $offer_name, true );
            }
            if ( $offer_value > 0 ) {
                $item->add_meta_data( '_senderzz_offer_value', $offer_value, true );
            }

            $order->add_item( $item );
            $changed = true;
        }

        if ( $offer_name !== '' ) {
            $order->update_meta_data( '_senderzz_offer_name', $offer_name );
        }
        if ( $offer_value > 0 ) {
            $order->update_meta_data( '_senderzz_offer_value', $offer_value );
        }
        $order->update_meta_data( '_senderzz_offer_token', $token );
        $order->update_meta_data( '_senderzz_components_synced', 'yes' );
        $order->update_meta_data( '_senderzz_components_synced_at', current_time( 'mysql' ) );

        if ( $changed ) {
            $order->calculate_totals( false );
        }
        $order->save();
    }
}



/*
 * Senderzz — baixa única para pedidos de link personalizado.
 * Necessário ficar ANTES dos hooks abaixo, pois woocommerce_new_order pode disparar durante o checkout.
 */
if ( ! function_exists( 'senderzz_sz_offer_order_has_token' ) ) {
    function senderzz_sz_offer_order_has_token( $order ): bool {
        $order = $order instanceof WC_Order ? $order : wc_get_order( $order );
        if ( ! $order instanceof WC_Order ) {
            return false;
        }
        return (string) $order->get_meta( '_senderzz_offer_token', true ) !== '';
    }
}

if ( ! function_exists( 'senderzz_sz_manual_reduce_stock_once' ) ) {
    function senderzz_sz_manual_reduce_stock_once( $order ): void {
        $order = $order instanceof WC_Order ? $order : wc_get_order( $order );
        if ( ! $order instanceof WC_Order ) {
            return;
        }
        if ( ! senderzz_sz_offer_order_has_token( $order ) ) {
            return;
        }
        if ( ! function_exists( 'wc_maybe_reduce_stock_levels' ) ) {
            return;
        }

        $order_id   = (int) $order->get_id();
        $data_store = $order->get_data_store();

        if ( $data_store && method_exists( $data_store, 'get_stock_reduced' ) && $data_store->get_stock_reduced( $order_id ) ) {
            return;
        }

        $GLOBALS['senderzz_sz_allow_manual_reduce_stock'] = true;
        try {
            wc_maybe_reduce_stock_levels( $order_id );
            $order->update_meta_data( '_senderzz_manual_stock_reduced', 'yes' );
            $order->update_meta_data( '_senderzz_manual_stock_reduced_at', current_time( 'mysql' ) );
            $order->save();
        } finally {
            unset( $GLOBALS['senderzz_sz_allow_manual_reduce_stock'] );
        }
    }
}

add_filter( 'woocommerce_can_reduce_order_stock', function ( $can_reduce, $order ) {
    $order = $order instanceof WC_Order ? $order : wc_get_order( $order );
    if ( ! $order instanceof WC_Order ) {
        return $can_reduce;
    }
    if ( ! senderzz_sz_offer_order_has_token( $order ) ) {
        return $can_reduce;
    }
    if ( ! empty( $GLOBALS['senderzz_sz_allow_manual_reduce_stock'] ) ) {
        return $can_reduce;
    }
    return false;
}, 9999, 2 );

add_action( 'woocommerce_checkout_order_created', function ( $order ) {
    if ( ! $order instanceof WC_Order ) return;

    $token = sanitize_text_field(
        $_POST['senderzz_offer_token']
        ?? $_COOKIE['senderzz_offer_token']
        ?? $order->get_meta( '_senderzz_offer_token' )
        ?? ''
    );

    senderzz_sz_sync_order_components_from_token( $order, $token );
}, 99999 );

add_action( 'woocommerce_new_order', function ( $order_id ) {
    $order = wc_get_order( $order_id );
    if ( ! $order ) return;

    $token = sanitize_text_field(
        $_POST['senderzz_offer_token']
        ?? $_COOKIE['senderzz_offer_token']
        ?? $order->get_meta( '_senderzz_offer_token' )
        ?? ''
    );

    senderzz_sz_sync_order_components_from_token( $order, $token );
}, 99999 );

add_action( 'woocommerce_checkout_order_processed', function ( $order_id, $posted_data = [], $order = null ) {
    $order = $order instanceof WC_Order ? $order : wc_get_order( $order_id );
    if ( ! $order instanceof WC_Order ) return;

    $token = sanitize_text_field(
        $_POST['senderzz_offer_token']
        ?? $_COOKIE['senderzz_offer_token']
        ?? $order->get_meta( '_senderzz_offer_token' )
        ?? ''
    );

    senderzz_sz_sync_order_components_from_token( $order, $token );
    senderzz_sz_manual_reduce_stock_once( $order );
}, 99999, 3 );

add_action( 'woocommerce_new_order', function ( $order_id ) {
    $order = wc_get_order( $order_id );
    if ( ! $order instanceof WC_Order ) return;
    senderzz_sz_manual_reduce_stock_once( $order );
}, 100000 );


/**
 * Senderzz — restauração imediata de estoque para COD Motoboy frustrado.
 *
 * v324: fallback manual para pedidos antigos.
 *
 * Motivo: em alguns pedidos antigos o Senderzz baixou o estoque e gravou
 * _senderzz_manual_stock_reduced=yes, mas o marcador interno do WooCommerce
 * (_order_stock_reduced / get_stock_reduced) pode não estar disponível para
 * wc_maybe_increase_stock_levels(). Nesse cenário a troca de status para
 * Frustrado dispara o hook, mas o Woo não devolve nada.
 *
 * Regra segura:
 * - nunca restaura duas vezes se _senderzz_stock_restored_on_frustrado=yes;
 * - tenta primeiro a restauração nativa do Woo;
 * - se o Woo não tinha o marcador, mas o pedido tem evidência Senderzz de
 *   baixa anterior, devolve item a item manualmente;
 * - marca o pedido como restaurado e limpa o marcador interno de estoque.
 */
if ( ! function_exists( 'senderzz_sz_order_is_frustrado_status' ) ) {
    function senderzz_sz_order_is_frustrado_status( $status ): bool {
        $status = strtolower( ltrim( (string) $status, 'wc-' ) );
        return false !== strpos( $status, 'frustr' );
    }
}

if ( ! function_exists( 'senderzz_sz_restore_stock_on_frustrado_once' ) ) {
    function senderzz_sz_restore_stock_on_frustrado_once( $order_id, string $reason = 'frustrado_auto' ): bool {
        if ( ! function_exists( 'wc_get_order' ) ) {
            return false;
        }

        $order = wc_get_order( $order_id );
        if ( ! $order instanceof WC_Order ) {
            return false;
        }

        $order_id = (int) $order->get_id();

        // Nova regra de custódia: Frustrado NÃO devolve estoque automaticamente.
        // Só a leitura do QR pelo OL, com conferência vendável no CD, libera estoque.
        if ( $reason !== 'return_scan' ) {
            if ( $order->get_meta( '_senderzz_stock_restore_waiting_return_scan', true ) !== 'yes' ) {
                $order->update_meta_data( '_senderzz_stock_restore_waiting_return_scan', 'yes' );
                $order->update_meta_data( '_senderzz_stock_restore_waiting_return_scan_at', current_time( 'mysql' ) );
                $order->add_order_note( 'Senderzz COD: pedido frustrado. Estoque mantido em custódia do motoboy até leitura do QR pelo OL no CD.' );
                $order->save();
            }
            return false;
        }

        if ( $order->get_meta( '_senderzz_stock_restored_on_frustrado', true ) === 'yes' ) {
            return false;
        }

        $mode      = (string) $order->get_meta( '_senderzz_delivery_mode', true );
        $flow      = (string) $order->get_meta( '_senderzz_motoboy_flow_status', true );
        $has_offer = function_exists( 'senderzz_sz_offer_order_has_token' ) ? senderzz_sz_offer_order_has_token( $order ) : false;
        $manual    = (string) $order->get_meta( '_senderzz_manual_stock_reduced', true );

        if ( $mode !== 'motoboy' && $flow !== 'frustrado' && ! $has_offer && $manual !== 'yes' ) {
            return false;
        }

        $data_store  = $order->get_data_store();
        $was_reduced = false;
        if ( $data_store && method_exists( $data_store, 'get_stock_reduced' ) ) {
            $was_reduced = (bool) $data_store->get_stock_reduced( $order_id );
        }

        $restored_by = '';
        if ( $was_reduced && function_exists( 'wc_maybe_increase_stock_levels' ) ) {
            wc_maybe_increase_stock_levels( $order_id );
            $restored_by = 'woocommerce_return_scan';
        }

        if ( $restored_by === '' && $manual === 'yes' && function_exists( 'wc_update_product_stock' ) ) {
            $changed = false;
            foreach ( $order->get_items( 'line_item' ) as $item ) {
                if ( ! $item instanceof WC_Order_Item_Product ) continue;
                $product = $item->get_product();
                if ( ! $product || ! $product->managing_stock() ) continue;
                $qty = (float) $item->get_quantity();
                if ( $qty <= 0 ) continue;
                wc_update_product_stock( $product, $qty, 'increase' );
                $changed = true;
            }
            if ( $changed ) $restored_by = 'senderzz_manual_return_scan';
        }

        if ( $restored_by === '' ) {
            $order->update_meta_data( '_senderzz_stock_restore_frustrado_skipped_at', current_time( 'mysql' ) );
            $order->update_meta_data( '_senderzz_stock_restore_frustrado_skipped_reason', 'no_stock_reduced_marker_or_manual_senderzz_flag' );
            $order->add_order_note( 'Senderzz COD: OL bipou devolução vendável, mas não foi encontrado marcador de baixa para restaurar estoque.' );
            $order->save();
            return false;
        }

        if ( $data_store && method_exists( $data_store, 'set_stock_reduced' ) ) {
            $data_store->set_stock_reduced( $order_id, false );
        }

        $order = wc_get_order( $order_id );
        if ( $order instanceof WC_Order ) {
            $order->update_meta_data( '_senderzz_stock_restored_on_frustrado', 'yes' );
            $order->update_meta_data( '_senderzz_stock_restored_on_frustrado_at', current_time( 'mysql' ) );
            $order->update_meta_data( '_senderzz_stock_restored_on_frustrado_by', $restored_by );
            $order->delete_meta_data( '_senderzz_stock_restore_waiting_return_scan' );
            $order->delete_meta_data( '_senderzz_stock_restore_waiting_return_scan_at' );
            $order->delete_meta_data( '_senderzz_stock_restore_frustrado_skipped_reason' );
            $order->delete_meta_data( '_senderzz_stock_restore_frustrado_skipped_at' );
            $order->add_order_note( 'Senderzz COD: estoque restaurado após devolução vendável bipada pelo OL. Método: ' . $restored_by . '.' );
            $order->save();
        }
        return true;
    }
}

add_action( 'woocommerce_order_status_frustrado', 'senderzz_sz_restore_stock_on_frustrado_once', 5, 1 );
add_action( 'woocommerce_order_status_wc-frustrado', 'senderzz_sz_restore_stock_on_frustrado_once', 5, 1 );
add_action( 'woocommerce_order_status_frustracao', 'senderzz_sz_restore_stock_on_frustrado_once', 5, 1 );
add_action( 'woocommerce_order_status_wc-frustracao', 'senderzz_sz_restore_stock_on_frustrado_once', 5, 1 );
add_action( 'woocommerce_order_status_changed', function ( $order_id, $old_status, $new_status ) {
    if ( senderzz_sz_order_is_frustrado_status( $new_status ) ) {
        senderzz_sz_restore_stock_on_frustrado_once( $order_id );
    }
}, 5, 3 );

// Backup para alguns fluxos internos do motoboy que disparam ação própria.
add_action( 'sz_motoboy_frustrado', function ( $pedido_id, $wc_order_id = 0 ) {
    if ( $wc_order_id ) {
        senderzz_sz_restore_stock_on_frustrado_once( $wc_order_id );
    }
}, 5, 2 );
