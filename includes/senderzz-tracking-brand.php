<?php
/**
 * Senderzz Tracking Brand
 * Configuração de identidade visual por classe de entrega para a página de rastreamento.
 *
 * Opção salva: senderzz_tracking_brands
 * Estrutura: [ class_id => [ 'logo' => 'url', 'cor' => '#hex', 'nome' => 'texto', 'rodape' => 'texto' ] ]
 */
defined( 'ABSPATH' ) || exit;
if ( defined( 'SENDERZZ_TRACKING_BRAND_LOADED' ) ) return;
define( 'SENDERZZ_TRACKING_BRAND_LOADED', true );

define( 'SENDERZZ_TRACKING_BRAND_OPTION', 'senderzz_tracking_brands' );

// ─── Admin menu ───────────────────────────────────────────────────────────────
// Senderzz v221: submenu legado removido da origem; aba Rastreio vive em Admin > Senderzz.
// add_action( 'admin_menu', function () {
//     add_submenu_page(
//         'woocommerce',
//         'Rastreio — Marca por Classe',
//         'Rastreio por Classe',
//         'manage_woocommerce',
//         'senderzz-tracking-brand',
//         'senderzz_tracking_brand_page'
//     );
// }, 45 );

// ─── Salvar ───────────────────────────────────────────────────────────────────
add_action( 'admin_init', function () {
    if (
        ! isset( $_POST['_senderzz_tb_nonce'] ) ||
        ! wp_verify_nonce( $_POST['_senderzz_tb_nonce'], 'senderzz_tb_save' ) ||
        ! current_user_can( 'manage_woocommerce' )
    ) return;

    $raw   = isset( $_POST['tb'] ) ? (array) $_POST['tb'] : [];
    $clean = [];
    foreach ( $raw as $class_id => $data ) {
        $cid = (int) $class_id;
        $clean[ $cid ] = [
            'logo'    => esc_url_raw( trim( $data['logo']    ?? '' ) ),
            'cor'     => sanitize_hex_color( trim( $data['cor']     ?? '#E8650A' ) ) ?: '#E8650A',
            'cor_texto' => sanitize_hex_color( trim( $data['cor_texto'] ?? '#ffffff' ) ) ?: '#ffffff',
            'nome'    => sanitize_text_field( trim( $data['nome']    ?? '' ) ),
            'rodape'  => sanitize_text_field( trim( $data['rodape']  ?? '' ) ),
        ];
    }
    update_option( SENDERZZ_TRACKING_BRAND_OPTION, $clean );
    add_settings_error( 'senderzz_tb', 'saved', 'Configurações salvas.', 'success' );
} );

// ─── Helpers ──────────────────────────────────────────────────────────────────
function senderzz_tb_get( int $class_id ): array {
    $map = get_option( SENDERZZ_TRACKING_BRAND_OPTION, [] );
    $defaults = [
        'logo'      => '',
        'cor'       => '#E8650A',
        'cor_texto' => '#ffffff',
        'nome'      => 'Senderzz',
        'rodape'    => '',
    ];
    $entry = $map[ $class_id ] ?? $map[0] ?? [];
    return array_merge( $defaults, $entry );
}

function senderzz_tb_get_for_order( ?WC_Order $order ): array {
    if ( ! $order ) return senderzz_tb_get( 0 );
    $class_id = 0;
    if ( function_exists( 'senderzz_operator_order_class_info' ) ) {
        $info     = senderzz_operator_order_class_info( $order );
        $class_id = (int) ( $info['id'] ?? 0 );
    } else {
        $class_id = (int) $order->get_meta( '_senderzz_product_shipping_class_id' );
        if ( ! $class_id ) {
            foreach ( $order->get_items() as $item ) {
                $product = $item->get_product();
                if ( $product ) { $class_id = (int) $product->get_shipping_class_id(); break; }
            }
        }
    }
    return senderzz_tb_get( $class_id );
}

// ─── Página admin ─────────────────────────────────────────────────────────────
function senderzz_tracking_brand_page(): void {
    $terms = get_terms( [ 'taxonomy' => 'product_shipping_class', 'hide_empty' => false ] );
    if ( is_wp_error( $terms ) ) $terms = [];
    $classes = array_merge( [ (object)[ 'term_id' => 0, 'name' => 'Padrão (sem classe)' ] ], $terms );
    $map = get_option( SENDERZZ_TRACKING_BRAND_OPTION, [] );

    settings_errors( 'senderzz_tb' );
    ?>
    <div class="wrap">
        <h1>🎨 Rastreio — Marca por Classe de Entrega</h1>
        <p style="color:#666;margin-bottom:20px">Configure logo, cor e nome para a página de rastreamento de cada classe. A URL de rastreamento é <code><?php echo esc_html( home_url('/rastreio/{CODIGO}') ); ?></code></p>
        <form method="post">
            <?php wp_nonce_field( 'senderzz_tb_save', '_senderzz_tb_nonce' ); ?>
            <table class="wp-list-table widefat fixed striped" style="max-width:900px">
                <thead>
                    <tr>
                        <th style="width:160px">Classe</th>
                        <th>URL da Logo</th>
                        <th style="width:110px">Cor primária</th>
                        <th style="width:110px">Cor do texto</th>
                        <th style="width:160px">Nome da marca</th>
                        <th>Texto rodapé</th>
                    </tr>
                </thead>
                <tbody>
                <?php foreach ( $classes as $class ) :
                    $cid     = (int) $class->term_id;
                    $current = array_merge( [
                        'logo'      => '',
                        'cor'       => '#E8650A',
                        'cor_texto' => '#ffffff',
                        'nome'      => '',
                        'rodape'    => '',
                    ], $map[ $cid ] ?? [] );
                ?>
                    <tr>
                        <td><strong><?php echo esc_html( $class->name ); ?></strong></td>
                        <td><input type="url" name="tb[<?php echo $cid; ?>][logo]" value="<?php echo esc_attr( $current['logo'] ); ?>" placeholder="https://..." style="width:100%"></td>
                        <td>
                            <input type="color" name="tb[<?php echo $cid; ?>][cor]" value="<?php echo esc_attr( $current['cor'] ); ?>" style="width:60px;height:34px;padding:2px;border:1px solid #ddd;border-radius:4px">
                            <input type="text" name="tb[<?php echo $cid; ?>][cor]" value="<?php echo esc_attr( $current['cor'] ); ?>" style="width:80px;font-size:var(--sz-text-sm)" placeholder="#E8650A" oninput="this.previousElementSibling.value=this.value">
                        </td>
                        <td>
                            <input type="color" name="tb[<?php echo $cid; ?>][cor_texto]" value="<?php echo esc_attr( $current['cor_texto'] ); ?>" style="width:60px;height:34px;padding:2px;border:1px solid #ddd;border-radius:4px">
                            <input type="text" name="tb[<?php echo $cid; ?>][cor_texto]" value="<?php echo esc_attr( $current['cor_texto'] ); ?>" style="width:80px;font-size:var(--sz-text-sm)" placeholder="#ffffff" oninput="this.previousElementSibling.value=this.value">
                        </td>
                        <td><input type="text" name="tb[<?php echo $cid; ?>][nome]" value="<?php echo esc_attr( $current['nome'] ); ?>" placeholder="Ex: Avenobis" style="width:100%"></td>
                        <td><input type="text" name="tb[<?php echo $cid; ?>][rodape]" value="<?php echo esc_attr( $current['rodape'] ); ?>" placeholder="Ex: Dúvidas? contato@avenobis.com.br" style="width:100%"></td>
                    </tr>
                <?php endforeach; ?>
                </tbody>
            </table>
            <p><button type="submit" class="button button-primary" style="margin-top:16px">Salvar configurações</button></p>
        </form>
    </div>
    <script>
    // Sync color picker com text input
    document.querySelectorAll('input[type=color]').forEach(function(picker){
        picker.addEventListener('input', function(){
            this.nextElementSibling.value = this.value;
        });
    });
    </script>
    <?php
}


// ─── REST API para o portal do usuário ───────────────────────────────────────
add_action( 'rest_api_init', function () {

    // GET — carrega brand do usuário logado
    register_rest_route( 'tp-carteira/v1', '/rastreio-brand', [
        'methods'             => 'GET',
        'permission_callback' => 'tpc_rest_auth',
        'callback'            => function () {
            $user_id  = get_current_user_id();
            $class_id = senderzz_tb_get_user_class_id( $user_id );
            $brand    = senderzz_tb_get( $class_id );
            return new WP_REST_Response( [ 'ok' => true, 'brand' => $brand, 'class_id' => $class_id ], 200 );
        },
    ] );

    // POST — salva brand do usuário logado
    register_rest_route( 'tp-carteira/v1', '/rastreio-brand', [
        'methods'             => 'POST',
        'permission_callback' => 'tpc_rest_auth',
        'callback'            => function ( WP_REST_Request $req ) {
            $user_id  = get_current_user_id();
            $class_id = senderzz_tb_get_user_class_id( $user_id );

            $map = get_option( SENDERZZ_TRACKING_BRAND_OPTION, [] );
            $map[ $class_id ] = [
                'logo'      => esc_url_raw( trim( (string) $req->get_param( 'logo' ) ) ),
                'cor'       => sanitize_hex_color( trim( (string) $req->get_param( 'cor' ) ) ) ?: '#E8650A',
                'cor_texto' => sanitize_hex_color( trim( (string) $req->get_param( 'cor_texto' ) ) ) ?: '#ffffff',
                'nome'      => sanitize_text_field( trim( (string) $req->get_param( 'nome' ) ) ),
                'rodape'    => sanitize_text_field( trim( (string) $req->get_param( 'rodape' ) ) ),
            ];
            update_option( SENDERZZ_TRACKING_BRAND_OPTION, $map );
            return new WP_REST_Response( [ 'ok' => true ], 200 );
        },
    ] );
} );

// Descobre qual classe está associada ao usuário logado
function senderzz_tb_get_user_class_id( int $user_id ): int {
    $map = get_option( 'senderzz_shipping_class_wallet_owners', [] );
    if ( is_array( $map ) ) {
        foreach ( $map as $class_id => $owner_id ) {
            if ( (int) $owner_id === $user_id ) return (int) $class_id;
        }
    }
    return 0;
}
