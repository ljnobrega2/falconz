<?php
defined( 'ABSPATH' ) || exit;
if ( defined( 'SENDERZZ_PREFERIDA_ADMIN_PAGE_LOADED' ) ) return;
define( 'SENDERZZ_PREFERIDA_ADMIN_PAGE_LOADED', true );

/*
 * Estrutura salva no wp_options (TP_PREFERIDA_OPTION):
 * [
 *   class_id (int) => [
 *     'modo'       => 'mais_barata',
 *     'permitidas' => [3, 1, 7, 12],   // method_ids do ME que entram na disputa
 *   ],
 * ]
 */

// ─── Menu ────────────────────────────────────────────────────────────────────
// Senderzz v221: submenu legado removido da origem; aba Preferidas vive em Admin > Senderzz.
// add_action( 'admin_menu', function () {
//     add_submenu_page(
//         'woocommerce',
//         'Transportadora Preferida',
//         'Transportadora Preferida',
//         'manage_woocommerce',
//         'tp-preferida',
//         'tp_preferida_render_page'
//     );
// } );

// ─── Salvar ──────────────────────────────────────────────────────────────────
add_action( 'admin_init', function () {
    if (
        ! isset( $_POST['tp_preferida_nonce'] ) ||
        ! wp_verify_nonce( $_POST['tp_preferida_nonce'], 'tp_preferida_save' ) ||
        ! current_user_can( 'manage_woocommerce' )
    ) {
        return;
    }

    $raw_map = isset( $_POST['tp_map'] ) ? (array) $_POST['tp_map'] : [];
    $clean   = [];

    foreach ( $raw_map as $class_id => $data ) {
        $permitidas = isset( $data['permitidas'] ) ? array_map( 'intval', (array) $data['permitidas'] ) : [];
        $permitidas = array_values( array_filter( $permitidas ) );

        $clean[ intval( $class_id ) ] = [
            'modo'       => 'mais_barata',
            'permitidas' => $permitidas,
        ];
    }

    update_option( TP_PREFERIDA_OPTION, $clean );

    add_action( 'admin_notices', function () {
        echo '<div class="notice notice-success is-dismissible"><p><strong>Transportadora Preferida:</strong> configurações salvas!</p></div>';
    } );
} );

// ─── Render ──────────────────────────────────────────────────────────────────
function tp_preferida_render_page() {
    $saved            = get_option( TP_PREFERIDA_OPTION, [] );
    $shipping_classes = WC()->shipping->get_shipping_classes();
    $me_methods_raw   = function_exists( 'wc_melhor_envio_get_account_methods' ) ? wc_melhor_envio_get_account_methods() : [];

    // Transportadoras e serviços bloqueados
    $blocked_companies = apply_filters( 'senderzz_blocked_carriers', [ 'Azul Cargo Express' ] );
    $blocked_services  = apply_filters( 'senderzz_blocked_services', [ 'Mini Envios' ] );

    // Filtra apenas métodos habilitados nas zonas de envio do WooCommerce
    $enabled_ids = function_exists( 'senderzz_get_enabled_me_method_ids' )
        ? senderzz_get_enabled_me_method_ids()
        : [];

    // Agrupa por transportadora, excluindo bloqueados e não habilitados
    $me_grouped = [];
    if ( is_array( $me_methods_raw ) ) {
        foreach ( $me_methods_raw as $mid => $m ) {
            // Filtra métodos não habilitados no WooCommerce (se temos a lista)
            if ( ! empty( $enabled_ids ) && ! in_array( intval( $mid ), $enabled_ids, true ) ) continue;
            $company = trim( (string) ( $m['company'] ?? '' ) );
            $service = trim( (string) ( $m['name']    ?? '' ) );
            // Bloqueia por match exato OU substring (cobre variações de nome da API do ME)
            $company_blocked = in_array( $company, $blocked_companies, true );
            if ( ! $company_blocked ) {
                foreach ( $blocked_companies as $bc ) {
                    if ( stripos( $company, $bc ) !== false || stripos( $bc, $company ) !== false ) {
                        $company_blocked = true; break;
                    }
                }
            }
            if ( $company_blocked ) continue;
            if ( in_array( $service, $blocked_services,  true ) ) continue;
            $label = $company ?: 'Outros';
            $me_grouped[ $label ][ intval( $mid ) ] = $m;
        }
        ksort( $me_grouped );
        foreach ( $me_grouped as &$svcs ) {
            uasort( $svcs, fn($a,$b) => strcmp( $a['name'] ?? '', $b['name'] ?? '' ) );
        }
        unset( $svcs );
    }

    $all_classes = array_merge(
        [ (object)[ 'term_id' => 0, 'name' => 'Produtos sem classe', 'slug' => '' ] ],
        is_array( $shipping_classes ) ? $shipping_classes : []
    );
    ?>
    <style>
    .tp-carrier-group { margin-bottom:12px; }
    .tp-carrier-group-header { font-size:var(--sz-text-sm);font-weight:700;text-transform:none;letter-spacing:.02em;color:#50575e;padding:4px 0 6px;border-bottom:1px solid #e0e0e0;margin-bottom:8px; }
    .tp-method-grid { display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:4px 16px; }
    .tp-method-grid label { display:flex;align-items:center;gap:7px;font-size:var(--sz-text-base);cursor:pointer;padding:3px 0; }
    .tp-class-card { background:#fff;border:1px solid #ddd;border-radius:6px;padding:16px 20px;max-width:860px;margin-bottom:16px; }
    .tp-class-card-header { display:flex;align-items:center;justify-content:space-between;margin-bottom:12px; }
    .tp-badge { font-size:var(--sz-text-sm);background:#f0f7ff;color:#2271b1;padding:3px 10px;border-radius:20px;border:1px solid #c8e0f5; }
    </style>

    <div class="wrap">
        <!-- SENDERZZ-BUILD: v2539-TIER4-GROUPED -->
        <h1>🚚 Transportadora Preferida por Classe de Entrega <span style="font-size:var(--sz-text-sm);background:#e6f3fb;color:#0a6ea4;padding:2px 8px;border-radius:20px;vertical-align:middle;">v2539</span></h1>
        <p style="color:#555;max-width:760px;">
            Para cada <strong>classe de entrega</strong>, marque quais <strong>serviços por transportadora</strong> entram na disputa.
            O Senderzz destaca a <strong>mais barata</strong> para o CEP do cliente no topo do checkout.
        </p>
        <p style="font-size:var(--sz-text-meta);color:#777;max-width:760px;background:#fafafa;border:1px solid #eee;padding:7px 12px;border-radius:4px;">
            ⚠️ <strong>Exibidos apenas os métodos</strong> com <em>Ativar este método</em> marcado nas zonas de envio do WooCommerce,
            exceto Azul Cargo Express e Correios Mini Envios. Para ajustar os bloqueios use os filters
            <code>senderzz_blocked_carriers</code> / <code>senderzz_blocked_services</code>.
        </p>

        <?php if ( empty( $shipping_classes ) ) : ?>
            <div class="notice notice-warning">
                <p>Nenhuma classe de entrega encontrada.
                   <a href="<?php echo esc_url( admin_url( 'admin.php?page=wc-settings&tab=shipping&section=classes' ) ); ?>">Crie classes de entrega aqui</a>.</p>
            </div>
        <?php endif; ?>

        <?php if ( empty( $me_grouped ) ) : ?>
            <div class="notice notice-error">
                <p>Não foi possível carregar os métodos do Melhor Envio. Verifique a autenticação.</p>
            </div>
        <?php endif; ?>

        <form method="post" action="">
            <?php wp_nonce_field( 'tp_preferida_save', 'tp_preferida_nonce' ); ?>

            <?php foreach ( $all_classes as $class ) :
                $cid        = intval( $class->term_id );
                $config     = $saved[ $cid ] ?? [];
                $permitidas = array_map( 'intval', (array) ( $config['permitidas'] ?? [] ) );
            ?>
            <div class="tp-class-card">

                <div class="tp-class-card-header">
                    <div>
                        <strong style="font-size:var(--sz-text-md);"><?php echo esc_html( $class->name ); ?></strong>
                        <?php if ( $cid > 0 ) : ?>
                            <span style="font-size:var(--sz-text-sm);color:#999;margin-left:8px;">
                                ID: <?php echo esc_html( $cid ); ?> | slug: <?php echo esc_html( $class->slug ); ?>
                            </span>
                        <?php endif; ?>
                    </div>
                    <span class="tp-badge">✦ mais barata em destaque</span>
                </div>

                <?php foreach ( $me_grouped as $carrier_label => $carrier_services ) : ?>
                <div class="tp-carrier-group">
                    <div class="tp-carrier-group-header"><?php echo esc_html( $carrier_label ); ?></div>
                    <div class="tp-method-grid">
                        <?php foreach ( $carrier_services as $mid => $m ) :
                            $checked = in_array( $mid, $permitidas, true );
                        ?>
                        <label>
                            <input
                                type="checkbox"
                                name="tp_map[<?php echo esc_attr( $cid ); ?>][permitidas][]"
                                value="<?php echo esc_attr( $mid ); ?>"
                                <?php checked( $checked ); ?>
                            >
                            <?php echo esc_html( $m['name'] ?? '' ); ?>
                        </label>
                        <?php endforeach; ?>
                    </div>
                </div>
                <?php endforeach; ?>

            </div>
            <?php endforeach; ?>

            <p style="max-width:860px;margin-top:4px;">
                <input type="submit" class="button-primary" value="Salvar configurações" />
            </p>
        </form>
    </div>
    <?php
}
