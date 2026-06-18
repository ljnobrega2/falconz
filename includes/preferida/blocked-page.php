<?php
/**
 * blocked-page.php — Transportadoras Bloqueadas por Classe de Entrega
 *
 * Estrutura salva em wp_options (SENDERZZ_BLOCKED_OPTION):
 * [
 *   class_id (int) => [
 *     'bloqueadas' => [3, 7, 12],  // method_ids do ME que estão bloqueados
 *   ],
 * ]
 *
 * Regra: bloqueado sobrescreve permitida — se um method_id estiver em
 * bloqueadas E em permitidas, ele é removido da disputa no checkout.
 */
defined( 'ABSPATH' ) || exit;
if ( defined( 'SENDERZZ_BLOCKED_PAGE_LOADED' ) ) return;
define( 'SENDERZZ_BLOCKED_PAGE_LOADED', true );

if ( ! defined( 'SENDERZZ_BLOCKED_OPTION' ) ) {
    define( 'SENDERZZ_BLOCKED_OPTION', 'senderzz_blocked_carriers_map' );
}

// ─── Menu ────────────────────────────────────────────────────────────────────
// Senderzz v221: submenu legado removido da origem; aba Bloqueadas vive em Admin > Senderzz.
// add_action( 'admin_menu', function () {
//     add_submenu_page(
//         'woocommerce',
//         'Transportadoras Bloqueadas',
//         'Transportadoras Bloqueadas',
//         'manage_woocommerce',
//         'senderzz-blocked-carriers',
//         'senderzz_blocked_render_page'
//     );
// } );

// ─── Salvar ──────────────────────────────────────────────────────────────────
add_action( 'admin_init', function () {
    if (
        ! isset( $_POST['senderzz_blocked_nonce'] ) ||
        ! wp_verify_nonce( $_POST['senderzz_blocked_nonce'], 'senderzz_blocked_save' ) ||
        ! current_user_can( 'manage_woocommerce' )
    ) {
        return;
    }

    $raw_map = isset( $_POST['sz_blocked_map'] ) ? (array) $_POST['sz_blocked_map'] : [];
    $clean   = [];

    foreach ( $raw_map as $class_id => $data ) {
        $bloqueadas = isset( $data['bloqueadas'] ) ? array_map( 'intval', (array) $data['bloqueadas'] ) : [];
        $bloqueadas = array_values( array_filter( $bloqueadas ) );

        $clean[ intval( $class_id ) ] = [
            'bloqueadas' => $bloqueadas,
        ];
    }

    update_option( SENDERZZ_BLOCKED_OPTION, $clean );

    add_action( 'admin_notices', function () {
        echo '<div class="notice notice-success is-dismissible"><p><strong>Transportadoras Bloqueadas:</strong> configurações salvas!</p></div>';
    } );
} );

// ─── Render ──────────────────────────────────────────────────────────────────
function senderzz_blocked_render_page() {
    $saved            = get_option( SENDERZZ_BLOCKED_OPTION, [] );
    $shipping_classes = WC()->shipping->get_shipping_classes();
    $me_methods_raw   = function_exists( 'wc_melhor_envio_get_account_methods' ) ? wc_melhor_envio_get_account_methods() : [];

    // Mesmos filtros da página Preferida: só métodos habilitados nas zonas WC
    $blocked_companies = apply_filters( 'senderzz_blocked_carriers', [ 'Azul Cargo Express' ] );
    $blocked_services  = apply_filters( 'senderzz_blocked_services', [ 'Mini Envios' ] );
    $enabled_ids       = function_exists( 'senderzz_get_enabled_me_method_ids' )
        ? senderzz_get_enabled_me_method_ids()
        : [];

    $me_grouped = [];
    if ( is_array( $me_methods_raw ) ) {
        foreach ( $me_methods_raw as $mid => $m ) {
            if ( ! empty( $enabled_ids ) && ! in_array( intval( $mid ), $enabled_ids, true ) ) continue;
            $company = trim( (string) ( $m['company'] ?? '' ) );
            $service = trim( (string) ( $m['name']    ?? '' ) );
            $company_blocked = in_array( $company, $blocked_companies, true );
            if ( ! $company_blocked ) {
                foreach ( $blocked_companies as $bc ) {
                    if ( stripos( $company, $bc ) !== false || stripos( $bc, $company ) !== false ) {
                        $company_blocked = true; break;
                    }
                }
            }
            if ( $company_blocked ) continue;
            if ( in_array( $service, $blocked_services, true ) ) continue;
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
    .tp-badge-blocked { font-size:var(--sz-text-sm);background:#f0f7ff;color:#2271b1;padding:3px 10px;border-radius:20px;border:1px solid #c8e0f5; }
    </style>

    <div class="wrap">
        <h1>🚫 Transportadoras bloqueadas</h1>
        <p style="color:#555;max-width:760px;">
            As modalidades marcadas aqui <strong>NÃO aparecem</strong> no checkout nem entram na recomendação.
        </p>
        <p style="font-size:var(--sz-text-meta);color:#777;max-width:760px;background:#fafafa;border:1px solid #eee;padding:7px 12px;border-radius:4px;">
            ⚠️ Bloqueio tem prioridade sobre permitido. Um serviço marcado aqui nunca aparecerá no checkout desta classe.
        </p>

        <?php if ( empty( $me_grouped ) ) : ?>
            <div class="notice notice-error">
                <p>Não foi possível carregar os métodos do Melhor Envio. Verifique a autenticação.</p>
            </div>
        <?php endif; ?>

        <form method="post" action="">
            <?php wp_nonce_field( 'senderzz_blocked_save', 'senderzz_blocked_nonce' ); ?>

            <?php foreach ( $all_classes as $class ) :
                $cid       = intval( $class->term_id );
                $config    = $saved[ $cid ] ?? [];
                $bloqueadas = array_map( 'intval', (array) ( $config['bloqueadas'] ?? [] ) );
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
                    <span class="tp-badge-blocked">🚫 bloqueio ativo</span>
                </div>

                <?php if ( empty( $me_grouped ) ) : ?>
                    <p style="color:#999;font-size:var(--sz-text-base);">Nenhum método disponível.</p>
                <?php else : ?>
                <?php foreach ( $me_grouped as $carrier_label => $carrier_services ) : ?>
                <div class="tp-carrier-group">
                    <div class="tp-carrier-group-header"><?php echo esc_html( $carrier_label ); ?></div>
                    <div class="tp-method-grid">
                        <?php foreach ( $carrier_services as $mid => $m ) :
                            $checked = in_array( $mid, $bloqueadas, true );
                        ?>
                        <label>
                            <input
                                type="checkbox"
                                name="sz_blocked_map[<?php echo esc_attr( $cid ); ?>][bloqueadas][]"
                                value="<?php echo esc_attr( $mid ); ?>"
                                <?php checked( $checked ); ?>
                                
                            >
                            <?php echo esc_html( $m['name'] ?? '' ); ?>
                        </label>
                        <?php endforeach; ?>
                    </div>
                </div>
                <?php endforeach; ?>
                <?php endif; ?>
            </div>
            <?php endforeach; ?>

            <p style="max-width:860px;margin-top:4px;">
                <input type="submit" class="button-primary" value="Atualizar bloqueios" />
            </p>
        </form>
    </div>
    <?php
}
