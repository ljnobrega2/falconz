<?php
/**
 * Senderzz Dashboard V2 — Topbar (Fase 3 — consolidação visual)
 * ----------------------------------------------------------------
 * Topbar compacta com: toggle sidebar | título | pill beta
 *   [spacer]
 *   ranking pill | saldo (com olho) | "Voltar à clássica"? | tema | sair | avatar
 *
 * Ranking e saldo: calculados server-side com as mesmas fontes já
 * usadas na seção Dashboard (sem cálculo novo).
 * Sidebar: não contém mais ranking/saldo/tema/sair.
 *
 * @var object $sz_v2_user        Portal user (do controller).
 * @var string $sz_v2_first_name  Primeiro nome.
 * @var string $sz_v2_role        Role normalizado.
 * @var bool   $sz_v2_is_affiliate
 */
defined( 'ABSPATH' ) || exit;

$sztp_initial = trim((string) $sz_v2_first_name);
$sztp_avatar  = $sztp_initial !== ''
    ? ( function_exists('mb_strtoupper') && function_exists('mb_substr')
        ? mb_strtoupper( mb_substr( $sztp_initial, 0, 1 ), 'UTF-8' )
        : strtoupper( substr( $sztp_initial, 0, 1 ) ) )
    : 'U';
$sztp_preview = ! empty( $_COOKIE['sz_v2_preview'] );

// ── Saldo disponível (mesma fonte do dashboard.php) ───────────────────────────
$sztp_wallet_uid = function_exists( 'senderzz_portal_wallet_user_id' )
    ? (int) senderzz_portal_wallet_user_id( $sz_v2_user ) : 0;
$sztp_wp_uid     = (int) ( $sz_v2_user->wp_user_id ?? $sztp_wallet_uid );

$sztp_cod_available = 0.0;
if ( $sztp_wallet_uid && function_exists( 'sz_cod_wallet_summary' ) ) {
    $sztp_cod = sz_cod_wallet_summary( $sztp_wallet_uid );
    $sztp_cod_available = (float) ( $sztp_cod['available'] ?? 0 );
}
$sztp_tpc_available = 0.0;
if ( ! $sz_v2_is_affiliate && $sztp_wp_uid && function_exists( 'tpc_get_saldo_disponivel' ) ) {
    $sztp_has_exp = ! function_exists( 'sz_producer_has_expedicao' ) || sz_producer_has_expedicao( $sztp_wp_uid );
    if ( $sztp_has_exp ) {
        $sztp_tpc_available = (float) tpc_get_saldo_disponivel( $sztp_wp_uid );
    }
}
$sztp_saldo_total = $sztp_cod_available + $sztp_tpc_available;
$sztp_saldo_fmt   = function_exists( 'senderzz_portal_money' )
    ? senderzz_portal_money( $sztp_saldo_total )
    : 'R$ ' . number_format( $sztp_saldo_total, 2, ',', '.' );

// ── Ranking: tier atual (mesma fonte do dashboard.php) ────────────────────────
$sztp_tier_label = '';
$sztp_badge_svg  = '';
if ( function_exists( 'sz_aff_cod_tiers' ) ) {
    $sztp_cod_total = 0.0;
    if ( $sz_v2_is_affiliate ) {
        $sztp_aff_uid = (int) ( $sz_v2_user->aff_user_id ?? $sztp_wp_uid );
        if ( $sztp_aff_uid && function_exists( 'sz_aff_affiliate_cod_total' ) ) {
            $sztp_cod_total = (float) sz_aff_affiliate_cod_total( $sztp_aff_uid );
        }
    } else {
        if ( $sztp_wp_uid && function_exists( 'sz_aff_producer_cod_total' ) ) {
            $sztp_cod_total = (float) sz_aff_producer_cod_total( $sztp_wp_uid );
        }
        if ( $sztp_cod_total <= 0 ) $sztp_cod_total = $sztp_cod_available;
    }
    $sztp_tiers   = sz_aff_cod_tiers();
    $sztp_current = $sztp_tiers[0];
    foreach ( $sztp_tiers as $sztp_t ) {
        if ( $sztp_cod_total >= $sztp_t['min'] ) $sztp_current = $sztp_t;
    }
    $sztp_tier_label = (string) ( $sztp_current['label'] ?? '' );
    $sztp_badge_svg  = function_exists( 'sz_aff_tier_badge_svg' )
        ? sz_aff_tier_badge_svg( $sztp_current['slug'] ?? '', 18 ) : '';
}

$sztp_logout_url = esc_url( add_query_arg( 'senderzz_logout', '1' ) );
$sztp_classic_url = esc_url( add_query_arg( 'sz_v2', '0' ) );
?>
<header class="szv2-topbar">
    <!-- Esquerda: toggle + título + pill -->
    <button type="button" class="szv2-topbar-toggle" data-szv2-sidebar-toggle aria-label="Abrir ou recolher menu">
        <svg viewBox="0 0 20 20" aria-hidden="true"><path d="M3 5h14v2H3zM3 9h14v2H3zM3 13h14v2H3z"/></svg>
    </button>
    <h1 class="szv2-topbar-title" data-szv2-title>Dashboard</h1>

    <div class="szv2-topbar-spacer"></div>

    <!-- Direita: ranking | saldo | clássica? | tema | sair | avatar -->
    <div class="szv2-topbar-actions">

        <?php if ( '' !== $sztp_tier_label ) : ?>
        <span class="szv2-topbar-pill szv2-topbar-rank" title="Seu ranking: <?php echo esc_attr( $sztp_tier_label ); ?>">
            <?php if ( '' !== $sztp_badge_svg ) : ?>
                <span class="szv2-topbar-pill-icon" aria-hidden="true"><?php echo $sztp_badge_svg; // phpcs:ignore -- SVG estático ?></span>
            <?php endif; ?>
            <span class="szv2-topbar-pill-text"><?php echo esc_html( $sztp_tier_label ); ?></span>
        </span>
        <?php endif; ?>

        <!-- Notificações -->
        <div class="szv2-notif-wrap" id="szv2-notif-wrap">
            <button type="button" class="szv2-topbar-icon-btn" id="szv2-notif-btn"
                    title="Notificações" onclick="szV2ToggleNotif()"
                    aria-haspopup="true" aria-expanded="false">
                <svg viewBox="0 0 20 20" aria-hidden="true"><path d="M10 2a6 6 0 0 0-6 6v3l-1.7 2.6A1 1 0 0 0 3 15h14a1 1 0 0 0 .7-1.7L16 11V8a6 6 0 0 0-6-6zm0 16a2 2 0 0 0 2-2H8a2 2 0 0 0 2 2z"/></svg>
                <span class="szv2-notif-badge" id="szv2-notif-badge" style="display:none">0</span>
            </button>
            <div class="szv2-notif-panel" id="szv2-notif-panel" style="display:none" role="menu">
                <div class="szv2-notif-head">
                    <span>Notificações</span>
                    <button type="button" class="szv2-btn szv2-btn-sm" onclick="szV2MarkAllRead()" style="font-size:11px;padding:2px 6px">Marcar tudo lido</button>
                </div>
                <div id="szv2-notif-list" class="szv2-notif-list">
                    <div class="szv2-notif-empty">Nenhuma notificação</div>
                </div>
            </div>
        </div>

        <button type="button" class="szv2-topbar-icon-btn" onclick="szGo && szGo('settings')" title="Configurações da conta"
                style="border:2px solid var(--szv2-brand);border-radius:50%;padding:0;width:32px;height:32px;display:flex;align-items:center;justify-content:center;overflow:hidden;background:var(--szv2-brand);color:#fff;font-weight:700;font-size:13px">
            <?php echo esc_html( $sztp_avatar ); ?>
        </button>
    </div>
</header>
