<?php
/**
 * Senderzz Dashboard V2 — Sidebar (Fase 3 — consolidação visual)
 * ----------------------------------------------------------------
 * Sidebar focada apenas em navegação. Ranking, saldo, tema e sair
 * foram movidos para a topbar. Rodapé removido.
 *
 * Contratos preservados: itens com classe .sz-ni e data-nav com as
 * mesmas chaves do legado.
 *
 * @var object $sz_v2_user
 * @var string $sz_v2_first_name
 * @var array  $sz_v2_nav_groups
 * @var array  $sz_v2_sections
 */
defined( 'ABSPATH' ) || exit;

$szs_plugin_root = dirname( __DIR__, 3 );
$szs_logo_file   = $szs_plugin_root . '/assets/images/senderzz-logo-horizontal.svg';
$szs_mark_file   = $szs_plugin_root . '/assets/images/senderzz-mascot.svg';
$szs_logo_svg    = file_exists( $szs_logo_file ) ? (string) file_get_contents( $szs_logo_file ) : '';
$szs_mark_svg    = file_exists( $szs_mark_file ) ? (string) file_get_contents( $szs_mark_file ) : '';

$szs_icons = [
    'dashboard'    => '<svg viewBox="0 0 20 20"><path d="M3 3h6v8H3zM11 3h6v5h-6zM11 10h6v7h-6zM3 13h6v4H3z"/></svg>',
    'orders'       => '<svg viewBox="0 0 20 20"><path d="M4 3h12a1 1 0 0 1 1 1v13l-3-2-2 2-2-2-2 2-2-2-3 2V4a1 1 0 0 1 1-1zm2 4v2h8V7H6zm0 4v2h6v-2H6z"/></svg>',
    'motoboy'      => '<svg viewBox="0 0 20 20"><path d="M5 14a2.5 2.5 0 1 0 0 .01zM15 14a2.5 2.5 0 1 0 0 .01zM11 5h3l3 4v4h-2a3 3 0 0 0-6 0H8a3 3 0 0 0-5.4-1.8L2 9l4-1 2-3h3z"/></svg>',
    'expedicao'    => '<svg viewBox="0 0 20 20"><path d="M10 2 3 5.5v9L10 18l7-3.5v-9L10 2zm0 2.2 4.6 2.3L10 8.8 5.4 6.5 10 4.2zM5 8.1l4 2v5.3l-4-2V8.1zm10 0v5.3l-4 2v-5.3l4-2z"/></svg>',
    'links'        => '<svg viewBox="0 0 20 20"><path d="M8.6 11.4a1 1 0 0 1 0-1.4l2.8-2.8a3 3 0 1 1 4.2 4.2l-1.4 1.4-1.4-1.4 1.4-1.4a1 1 0 1 0-1.4-1.4L10 11.4a1 1 0 0 1-1.4 0zm2.8-2.8a1 1 0 0 1 0 1.4L8.6 12.8a3 3 0 1 1-4.2-4.2l1.4-1.4 1.4 1.4-1.4 1.4a1 1 0 1 0 1.4 1.4L10 8.6a1 1 0 0 1 1.4 0z"/></svg>',
    'products'     => '<svg viewBox="0 0 20 20"><path d="M4 4h5v5H4zM11 4h5v5h-5zM4 11h5v5H4zM11 11h5v5h-5z"/></svg>',
    'affiliates'   => '<svg viewBox="0 0 20 20"><path d="M7 9a3 3 0 1 0-.01-6.01A3 3 0 0 0 7 9zm6 1a2.5 2.5 0 1 0-.01-5.01A2.5 2.5 0 0 0 13 10zM2 17v-1.5C2 13.6 4.2 12 7 12s5 1.6 5 3.5V17H2zm12 0v-1.5c0-1-.4-1.9-1.1-2.6.7-.3 1.4-.4 2.1-.4 2.2 0 4 1.3 4 3V17h-5z"/></svg>',
    'wallet'       => '<svg viewBox="0 0 20 20"><path d="M3 5a2 2 0 0 1 2-2h10v3H5a2 2 0 0 1-2-1zm0 2.5V15a2 2 0 0 0 2 2h12V7.5H3zM14 12a1.2 1.2 0 1 1 0 .01z"/></svg>',
    'reports'      => '<svg viewBox="0 0 20 20"><path d="M4 16h2V8H4v8zm5 0h2V4H9v12zm5 0h2v-6h-2v6zM3 18h14v1.5H3z"/></svg>',
    'webhooks'     => '<svg viewBox="0 0 20 20"><path d="M10 3a3 3 0 0 1 2.6 4.5l-2 3.4-1.7-1 2-3.4A1 1 0 1 0 9 5.4L7.3 4.6A3 3 0 0 1 10 3zM4.6 17a3 3 0 0 1-1.3-5.7l3.6-1.7.8 1.8-3.5 1.6a1 1 0 1 0 1 1.7l1.7 1A3 3 0 0 1 4.6 17zm10.8 0a3 3 0 0 1-2.9-2.2H8.4v-2h4.1a3 3 0 1 1 2.9 4.2z"/></svg>',
    'integrations' => '<svg viewBox="0 0 20 20"><path d="M8 3h4v3.1a4 4 0 0 1 0 7.8V17H8v-3.1a4 4 0 0 1 0-7.8V3zm2 5a2 2 0 1 0 0 4 2 2 0 0 0 0-4z"/></svg>',
    'freight'      => '<svg viewBox="0 0 20 20"><path d="M2 5h10v8H2zM12 8h3l3 3v2h-6V8zM5.5 17a1.8 1.8 0 1 1 0-.01zM14.5 17a1.8 1.8 0 1 1 0-.01z"/></svg>',
    'users'        => '<svg viewBox="0 0 20 20"><path d="M10 9a3.2 3.2 0 1 0-.01-6.41A3.2 3.2 0 0 0 10 9zm0 2c-3.4 0-6 1.8-6 4v2h12v-2c0-2.2-2.6-4-6-4z"/></svg>',
    'support'      => '<svg viewBox="0 0 20 20"><path d="M10 2a7 7 0 0 0-7 7v4a2 2 0 0 0 2 2h2V9H5a5 5 0 0 1 10 0h-2v6h2a2 2 0 0 0 2-2V9a7 7 0 0 0-7-7zm-1 14h2v2H9z"/></svg>',
    'settings'     => '<svg viewBox="0 0 20 20"><path d="m11.4 2 .5 2.1c.5.2 1 .4 1.4.7l2-.9 1.4 2.4-1.6 1.5c.1.5.1 1 0 1.4l1.6 1.5-1.4 2.4-2-.9c-.4.3-.9.5-1.4.7l-.5 2.1H8.6l-.5-2.1a6 6 0 0 1-1.4-.7l-2 .9-1.4-2.4 1.6-1.5a6 6 0 0 1 0-1.4L3.3 6.3 4.7 4l2 .9c.4-.3.9-.5 1.4-.7L8.6 2h2.8zM10 7.5A2.5 2.5 0 1 0 10 12.5 2.5 2.5 0 0 0 10 7.5z"/></svg>',
    'localidades'  => '<svg viewBox="0 0 20 20"><path d="M10 2a5 5 0 0 0-5 5c0 3.5 5 11 5 11s5-7.5 5-11a5 5 0 0 0-5-5zm0 7a2 2 0 1 1 0-4 2 2 0 0 1 0 4z"/></svg>',
    'vitrine'      => '<svg viewBox="0 0 20 20"><path d="M2 3h16v2.5l-1 1H3l-1-1V3zm1 4h14v10H3V7zm3 2v2h8V9H6zm0 4v2h5v-2H6z"/></svg>',
    'motoboys-dia' => '<svg viewBox="0 0 20 20"><path d="M3 4h14v2H3zm0 4h9v2H3zm0 4h6v2H3zm11-1a3 3 0 1 1 0 6 3 3 0 0 1 0-6zm0 2a1 1 0 1 0 0 2 1 1 0 0 0 0-2z"/></svg>',
];
?>
<aside class="szv2-sidebar" aria-label="Navegação principal">
    <div class="szv2-sidebar-head">
        <span class="szv2-logo-full"><?php echo $szs_logo_svg; // phpcs:ignore -- SVG estático ?></span>
        <span class="szv2-logo-mark" aria-hidden="true"><?php echo $szs_mark_svg; // phpcs:ignore -- SVG estático ?></span>
    </div>

    <?php
    $szs_first = trim( (string) $sz_v2_first_name );
    // Saldo for sidebar (reuse topbar data)
    $szs_saldo_fmt = '';
    if ( function_exists( 'sz_cod_wallet_summary' ) || function_exists( 'tpc_get_saldo_disponivel' ) ) {
        $szs_wp_uid = (int) ( $sz_v2_user->wp_user_id ?? 0 );
        $szs_wallet_uid = function_exists( 'senderzz_portal_wallet_user_id' )
            ? (int) senderzz_portal_wallet_user_id( $sz_v2_user ) : $szs_wp_uid;
        $szs_cod = 0.0;
        $szs_tpc = 0.0;
        if ( $szs_wallet_uid && function_exists( 'sz_cod_wallet_summary' ) ) {
            $szs_cod_data = sz_cod_wallet_summary( $szs_wallet_uid );
            $szs_cod = (float) ( $szs_cod_data['available'] ?? 0 );
        }
        $szs_has_exp = ! $sz_v2_is_affiliate && ( ! function_exists( 'sz_producer_has_expedicao' ) || sz_producer_has_expedicao( $szs_wp_uid ) );
        if ( $szs_has_exp && $szs_wp_uid && function_exists( 'tpc_get_saldo_disponivel' ) ) {
            $szs_tpc = (float) tpc_get_saldo_disponivel( $szs_wp_uid );
        }
        $szs_total = $szs_cod + $szs_tpc;
        $szs_saldo_fmt = function_exists( 'senderzz_portal_money' )
            ? senderzz_portal_money( $szs_total )
            : 'R$ ' . number_format( $szs_total, 2, ',', '.' );
    }
    // Progress bar — faturamento acumulado (produtor ou afiliado)
    $szs_fat_total = 0.0;
    $szs_fat_goal  = 10000.0;
    $szs_fat_pct   = 0;
    if ( function_exists( 'sz_aff_cod_tiers' ) && ( function_exists( 'sz_aff_producer_cod_total' ) || function_exists( 'sz_aff_affiliate_cod_total' ) ) ) {
        $szs_wp_uid_rank = (int) ( $sz_v2_user->wp_user_id ?? 0 );
        if ( $sz_v2_is_affiliate && function_exists( 'sz_aff_affiliate_cod_total' ) ) {
            $szs_fat_total = (float) sz_aff_affiliate_cod_total( $szs_wp_uid_rank );
        } elseif ( ! $sz_v2_is_affiliate && function_exists( 'sz_aff_producer_cod_total' ) ) {
            $szs_fat_total = (float) sz_aff_producer_cod_total( $szs_wp_uid_rank );
        }
        $szs_tiers = sz_aff_cod_tiers();
        // Find next tier threshold
        foreach ( $szs_tiers as $szs_t ) {
            if ( $szs_fat_total < (float) ( $szs_t['min'] ?? 0 ) ) {
                $szs_fat_goal = (float) ( $szs_t['min'] ?? 10000 );
                break;
            }
        }
        $szs_fat_pct = $szs_fat_goal > 0 ? min( 100, (int) ( $szs_fat_total / $szs_fat_goal * 100 ) ) : 100;
    }
    if ( $szs_first !== '' ) :
    ?>
    <div class="szv2-sidebar-hello">
        <strong>Olá, <?php echo esc_html( $szs_first ); ?></strong>
        <span>Bem-vindo de volta</span>
        <?php if ( $szs_saldo_fmt !== '' ) : ?>
        <div class="szv2-sidebar-saldo" data-szv2-saldo-value
             data-szv2-hidden="0"
             data-szv2-display="<?php echo esc_attr( $szs_saldo_fmt ); ?>">
            <span class="szv2-sidebar-saldo-label">Saldo</span>
            <span class="szv2-sidebar-saldo-value szv2-num"><?php echo esc_html( $szs_saldo_fmt ); ?></span>
        </div>
        <?php endif; ?>
        <?php if ( $szs_fat_pct > 0 || ! $sz_v2_is_affiliate ) : ?>
        <div class="szv2-rank-progress-wrap" style="margin-top:8px">
            <div class="szv2-rank-progress-bar">
                <div class="szv2-rank-progress-fill" style="width:<?php echo esc_attr( (string) $szs_fat_pct ); ?>%"></div>
            </div>
            <div class="szv2-rank-progress-meta">
                <span><?php echo esc_html( function_exists( 'senderzz_portal_money' ) ? senderzz_portal_money( $szs_fat_total ) : 'R$ ' . number_format( $szs_fat_total, 2, ',', '.' ) ); ?></span>
                <span><?php echo esc_html( function_exists( 'senderzz_portal_money' ) ? senderzz_portal_money( $szs_fat_goal ) : 'R$ ' . number_format( $szs_fat_goal, 2, ',', '.' ) ); ?></span>
            </div>
        </div>
        <?php endif; ?>
    </div>
    <?php endif; ?>

    <nav class="szv2-nav">
        <?php foreach ( $sz_v2_nav_groups as $szs_group_label => $szs_group_keys ) : ?>
            <div class="szv2-nav-group">
                <?php if ( '' !== $szs_group_label ) : ?>
                    <div class="szv2-nav-kicker"><?php echo esc_html( $szs_group_label ); ?></div>
                <?php endif; ?>
                <?php foreach ( $szs_group_keys as $szs_nav_key ) :
                    if ( ! isset( $sz_v2_sections[ $szs_nav_key ] ) ) continue;
                    $szs_nav_label = $sz_v2_sections[ $szs_nav_key ][1];
                ?>
                    <button type="button"
                            class="sz-ni"
                            data-nav="<?php echo esc_attr( $szs_nav_key ); ?>"
                            title="<?php echo esc_attr( $szs_nav_label ); ?>">
                        <span class="szv2-ni-icon" aria-hidden="true"><?php echo $szs_icons[ $szs_nav_key ] ?? ''; // phpcs:ignore ?></span>
                        <span class="szv2-ni-label"><?php echo esc_html( $szs_nav_label ); ?></span>
                    </button>
                <?php endforeach; ?>
            </div>
        <?php endforeach; ?>
    </nav>

    <!-- Rodapé da sidebar: tema | clássica | sair -->
    <div class="szv2-sidebar-foot">
        <button type="button" class="szv2-sidebar-foot-btn" data-szv2-theme-toggle title="Alternar tema">
            <svg viewBox="0 0 20 20" aria-hidden="true"><path d="M10 3a7 7 0 0 0 0 14c1.2 0 2.4-.3 3.4-.9A7 7 0 0 1 10 3z"/></svg>
            <span class="szv2-ni-label">Tema</span>
        </button>
        <?php if ( $sztp_preview ) : ?>
        <a class="szv2-sidebar-foot-btn" href="<?php echo esc_url( add_query_arg( 'sz_v2', '0' ) ); ?>" title="Portal clássico">
            <svg viewBox="0 0 20 20" aria-hidden="true"><path d="M3 4h14v2H3zM3 8h10v2H3zM3 12h14v2H3z"/></svg>
            <span class="szv2-ni-label">Clássica</span>
        </a>
        <?php endif; ?>
        <a class="szv2-sidebar-foot-btn" href="<?php echo esc_url( add_query_arg( 'senderzz_logout', '1' ) ); ?>" title="Sair da conta">
            <svg viewBox="0 0 20 20" aria-hidden="true"><path d="M8 3h5a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H8v-2h5V5H8V3zm1 5 3 2-3 2v-1.2H3V9.2h6V8z"/></svg>
            <span class="szv2-ni-label">Sair</span>
        </a>
    </div>
</aside>
