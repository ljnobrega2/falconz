<?php
/**
 * senderzz-markup.php
 *
 * Taxa de serviço configurável por classe de entrega.
 * Substitui o hardcode ×1,20 + R$3,99 do Method.php.
 *
 * Opções salvas:
 *   senderzz_markup_rules → array[ class_id => [ 'pct' => float, 'fixed' => float ] ]
 *   senderzz_markup_default → [ 'pct' => 20.0, 'fixed' => 3.99 ]  (fallback global)
 */
defined( 'ABSPATH' ) || exit;
if ( defined( 'SENDERZZ_SENDERZZ_MARKUP_LOADED' ) ) return;
define( 'SENDERZZ_SENDERZZ_MARKUP_LOADED', true );

/* ── Leitura ──────────────────────────────────────────────────────────── */

function senderzz_get_markup_default(): array {
    $d = get_option( 'senderzz_markup_default', [] );
    return [
        'pct'   => isset( $d['pct'] )   ? (float) $d['pct']   : 20.0,
        'fixed' => isset( $d['fixed'] ) ? (float) $d['fixed'] : 3.99,
    ];
}

function senderzz_get_markup_for_class( int $class_id ): array {
    if ( $class_id > 0 ) {
        $rules = get_option( 'senderzz_markup_rules', [] );
        if ( is_array( $rules ) && isset( $rules[ $class_id ] ) ) {
            $r = $rules[ $class_id ];
            return [
                'pct'   => isset( $r['pct'] )   ? (float) $r['pct']   : senderzz_get_markup_default()['pct'],
                'fixed' => isset( $r['fixed'] ) ? (float) $r['fixed'] : senderzz_get_markup_default()['fixed'],
            ];
        }
    }
    return senderzz_get_markup_default();
}

/**
 * Aplica markup ao custo real do ME.
 * @param float $real_cost  Custo bruto retornado pelo ME (custom_price).
 * @param int   $class_id   ID da classe de entrega do produto.
 * @return float            Valor final cobrado do cliente.
 */
function senderzz_apply_markup( float $real_cost, int $class_id = 0 ): float {
    if ( $real_cost <= 0 ) return 0.0;
    $m = senderzz_get_markup_for_class( $class_id );
    $result = ( $real_cost * ( 1 + $m['pct'] / 100 ) ) + $m['fixed'];
    return round( max( 0, $result ), 2 );
}

/**
 * Reverte o markup para estimar o custo real (quando original_cost não está disponível).
 */
function senderzz_reverse_markup( float $charged, int $class_id = 0 ): float {
    if ( $charged <= 0 ) return 0.0;
    $m = senderzz_get_markup_for_class( $class_id );
    $divisor = 1 + $m['pct'] / 100;
    if ( $divisor <= 0 ) return 0.0;
    return round( max( 0, ( $charged - $m['fixed'] ) / $divisor ), 2 );
}

/* ── Hook: substitui cálculo em Method.php via filter ────────────────── */

/**
 * Filter: wc_melhor_envio_rate_args
 * Recalcula o cost com taxa dinâmica antes de registrar o rate.
 */
add_filter( 'wc_melhor_envio_rate_args', function( array $args, $method, $package ) {
    // Descobre a classe de entrega predominante do pacote
    $class_id = 0;
    if ( ! empty( $package['contents'] ) ) {
        foreach ( $package['contents'] as $item ) {
            $product = $item['data'] ?? null;
            if ( $product && method_exists( $product, 'get_shipping_class_id' ) ) {
                $class_id = (int) $product->get_shipping_class_id();
                if ( $class_id > 0 ) break;
            }
        }
    }

    $original = isset( $args['meta_data']['melhorenvio_original_cost'] )
        ? (float) $args['meta_data']['melhorenvio_original_cost']
        : 0.0;

    if ( $original > 0 ) {
        $args['cost'] = senderzz_apply_markup( $original, $class_id );
        // Salva taxa usada no meta para rastreabilidade
        $m = senderzz_get_markup_for_class( $class_id );
        $args['meta_data']['senderzz_markup_pct']   = $m['pct'];
        $args['meta_data']['senderzz_markup_fixed']  = $m['fixed'];
        $args['meta_data']['senderzz_markup_class']  = $class_id;
    }

    return $args;
}, 5, 3 );

/* ── Admin: salvar configurações ─────────────────────────────────────── */

add_action( 'admin_init', function () {
    if (
        ! isset( $_POST['senderzz_markup_nonce'] ) ||
        ! wp_verify_nonce( $_POST['senderzz_markup_nonce'], 'senderzz_salvar_markup' ) ||
        ! current_user_can( 'manage_woocommerce' )
    ) return;

    // Markup global padrão
    update_option( 'senderzz_markup_default', [
        'pct'   => max( 0, (float) ( $_POST['senderzz_markup_default_pct']   ?? 20 ) ),
        'fixed' => max( 0, (float) ( $_POST['senderzz_markup_default_fixed'] ?? 3.99 ) ),
    ] );

    // Markup por classe
    $rules = [];
    $raw = $_POST['senderzz_markup_rules'] ?? [];
    if ( is_array( $raw ) ) {
        foreach ( $raw as $class_id => $vals ) {
            $cid = absint( $class_id );
            if ( $cid > 0 ) {
                $rules[ $cid ] = [
                    'pct'   => max( 0, (float) ( $vals['pct']   ?? 20 ) ),
                    'fixed' => max( 0, (float) ( $vals['fixed'] ?? 3.99 ) ),
                ];
            }
        }
    }
    update_option( 'senderzz_markup_rules', $rules );

    add_action( 'admin_notices', function() {
        echo '<div class="notice notice-success is-dismissible"><p>Taxas de markup atualizadas com sucesso.</p></div>';
    } );
} );

/* ── Admin: aba de markup na página Carteira de Frete ───────────────── */

// Senderzz v221: submenu legado removido da origem; aba Taxas vive em Admin > Senderzz.
// add_action( 'admin_menu', function () {
//     add_submenu_page(
//         'woocommerce',
//         'Taxas de Frete',
//         'Taxas de Frete',
//         'manage_woocommerce',
//         'senderzz-markup',
//         'senderzz_markup_admin_page'
//     );
// } );

function senderzz_markup_admin_page(): void {
    $default = senderzz_get_markup_default();
    $rules   = get_option( 'senderzz_markup_rules', [] );
    $classes = get_terms( [ 'taxonomy' => 'product_shipping_class', 'hide_empty' => false ] );
    if ( is_wp_error( $classes ) ) $classes = [];
    ?>
    <div class="wrap">
        <h1>📦 Taxas de Frete por Classe de Entrega</h1>
        <p style="color:#666;max-width:700px;">
            Define quanto é cobrado do cliente acima do custo real do Melhor Envio.
            O custo final é: <strong>custo_ME × (1 + taxa%) + taxa_fixa</strong>.
            Você pode definir uma taxa global padrão e sobrescrever por classe de entrega (produtor).
        </p>

        <form method="post">
            <?php wp_nonce_field( 'senderzz_salvar_markup', 'senderzz_markup_nonce' ); ?>

            <!-- Taxa padrão global -->
            <h2 style="margin-top:28px;">Taxa padrão global</h2>
            <p style="color:#888;font-size:var(--sz-text-base);margin-bottom:12px;">Aplicada quando a classe de entrega não tem taxa específica configurada.</p>
            <table class="form-table" style="max-width:600px;">
                <tr>
                    <th style="width:200px;">Taxa percentual (%)</th>
                    <td>
                        <input type="number" name="senderzz_markup_default_pct"
                               value="<?php echo esc_attr( $default['pct'] ); ?>"
                               min="0" max="500" step="0.01" style="width:100px;">
                        <span style="color:#888;font-size:var(--sz-text-base);">% sobre o custo ME</span>
                    </td>
                </tr>
                <tr>
                    <th>Taxa fixa (R$)</th>
                    <td>
                        <input type="number" name="senderzz_markup_default_fixed"
                               value="<?php echo esc_attr( $default['fixed'] ); ?>"
                               min="0" max="999" step="0.01" style="width:100px;">
                        <span style="color:#888;font-size:var(--sz-text-base);">adicionados ao total</span>
                    </td>
                </tr>
            </table>

            <!-- Taxas por classe -->
            <?php if ( ! empty( $classes ) ) : ?>
                <h2 style="margin-top:28px;">Taxa por classe de entrega (produtor)</h2>
                <p style="color:#888;font-size:var(--sz-text-base);margin-bottom:12px;">
                    Deixe em branco para usar a taxa padrão global. Preencha para sobrescrever.
                </p>
                <table class="widefat" style="max-width:700px;">
                    <thead>
                        <tr>
                            <th>Classe de entrega</th>
                            <th>Taxa % (sobre custo ME)</th>
                            <th>Taxa fixa R$</th>
                            <th>Exemplo: custo ME R$20,00 → cobra</th>
                        </tr>
                    </thead>
                    <tbody>
                        <?php foreach ( $classes as $class ) :
                            $cid = $class->term_id;
                            $rule = $rules[ $cid ] ?? null;
                            $pct   = $rule ? $rule['pct']   : '';
                            $fixed = $rule ? $rule['fixed'] : '';
                            $preview_pct   = $rule ? $rule['pct']   : $default['pct'];
                            $preview_fixed = $rule ? $rule['fixed'] : $default['fixed'];
                            $preview = round( ( 20 * ( 1 + $preview_pct / 100 ) ) + $preview_fixed, 2 );
                        ?>
                        <tr>
                            <td>
                                <strong><?php echo esc_html( $class->name ); ?></strong>
                                <br><span style="color:#999;font-size:var(--sz-text-meta);"><?php echo esc_html( $class->slug ); ?></span>
                            </td>
                            <td>
                                <input type="number"
                                       name="senderzz_markup_rules[<?php echo esc_attr( $cid ); ?>][pct]"
                                       value="<?php echo esc_attr( $pct ); ?>"
                                       placeholder="<?php echo esc_attr( $default['pct'] ); ?>"
                                       min="0" max="500" step="0.01" style="width:80px;">
                            </td>
                            <td>
                                <input type="number"
                                       name="senderzz_markup_rules[<?php echo esc_attr( $cid ); ?>][fixed]"
                                       value="<?php echo esc_attr( $fixed ); ?>"
                                       placeholder="<?php echo esc_attr( $default['fixed'] ); ?>"
                                       min="0" max="999" step="0.01" style="width:80px;">
                            </td>
                            <td style="color:#16a34a;font-weight:600;">
                                R$ <?php echo number_format( $preview, 2, ',', '.' ); ?>
                            </td>
                        </tr>
                        <?php endforeach; ?>
                    </tbody>
                </table>
            <?php else : ?>
                <div style="background:#fffbeb;border:1px solid #fcd34d;padding:12px 16px;border-radius:6px;margin-top:16px;max-width:700px;">
                    ⚠️ Nenhuma classe de entrega encontrada. Crie classes em
                    <a href="<?php echo admin_url( 'admin.php?page=wc-settings&tab=shipping&section=classes' ); ?>">
                        WooCommerce → Configurações → Entrega → Classes de entrega
                    </a>.
                </div>
            <?php endif; ?>

            <p style="margin-top:24px;">
                <button type="submit" class="button button-primary button-large">Salvar taxas</button>
            </p>
        </form>
    </div>
    <?php
}
