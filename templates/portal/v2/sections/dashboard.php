<?php
/**
 * Senderzz Dashboard V2 — Visão Geral (v426)
 * ----------------------------------------------------------------
 * Read-only. Filtros de período recalculam cards/KPIs localmente via
 * JSON embutido no DOM (Opção A). Padrão: 30 dias. Zero fetch/AJAX.
 *
 * Dados expostos por pedido (mínimos, sem sensíveis desnecessários):
 *   grupo, date, status, value, commission, product, email_hash,
 *   carrier, region, items_count
 * E-mail é exposto apenas como hash SHA1 truncado (10 chars) para
 * contagem de clientes únicos sem expor PII.
 *
 * Placeholders documentados:
 *   - Tempo médio de envio: _senderzz_operator_sent_at não está em
 *     format_order; exigiria N+1 wc_get_order → placeholder mantido.
 *   - Gráfico de tendência: by_day existe mas SVG/chart fora do escopo.
 *
 * mb_strimwidth: usa helper seguro sz_v2_strimwidth() com fallback.
 *
 * Barras de porcentagem (style="width:N%"): exceção controlada.
 *   São valores dinâmicos calculados em runtime — não podem ser classes
 *   CSS estáticas. Aplicados via data-percent + JS para manter HTML limpo.
 *
 * @var object $sz_v2_user
 * @var bool   $sz_v2_is_affiliate
 */
defined( 'ABSPATH' ) || exit;

//  Perfil / UIDs 
$szd_is_aff = ( 'affiliate' === strtolower( trim( (string) ( $sz_v2_user->role ?? 'client' ) ) ) );
$szd_w_uid  = function_exists( 'senderzz_portal_wallet_user_id' )
    ? (int) senderzz_portal_wallet_user_id( $sz_v2_user ) : 0;
$szd_wp_uid = (int) ( $sz_v2_user->wp_user_id ?? $szd_w_uid );

// F8: Tempo médio de envio — single aggregate query + transient 30min (evita N+1)
$szd_avg_ship_key  = 'sz_v2_avg_ship_' . abs( (int) ( $sz_v2_user->wp_user_id ?? 0 ) );
$szd_avg_ship_days = get_transient( $szd_avg_ship_key );
if ( $szd_avg_ship_days === false ) {
    global $wpdb;
    $szd_hpos = (bool) $wpdb->get_var( "SHOW TABLES LIKE '{$wpdb->prefix}wc_orders_meta'" );
    if ( $szd_hpos ) {
        $szd_avg_ship_days = $wpdb->get_var(
            "SELECT ROUND(AVG(TIMESTAMPDIFF(DAY, DATE(o.date_created_gmt), DATE(pm.meta_value))),1)
               FROM {$wpdb->prefix}wc_orders o
               INNER JOIN {$wpdb->prefix}wc_orders_meta pm ON pm.order_id = o.id
                 AND pm.meta_key = '_senderzz_operator_sent_at' AND pm.meta_value != ''
              WHERE o.date_created_gmt >= DATE_SUB(NOW(), INTERVAL 90 DAY)"
        );
    } else {
        $szd_avg_ship_days = $wpdb->get_var(
            "SELECT ROUND(AVG(TIMESTAMPDIFF(DAY, DATE(p.post_date), DATE(pm.meta_value))),1)
               FROM {$wpdb->prefix}posts p
               INNER JOIN {$wpdb->prefix}postmeta pm ON pm.post_id = p.ID
                 AND pm.meta_key = '_senderzz_operator_sent_at' AND pm.meta_value != ''
              WHERE p.post_type = 'shop_order'
                AND p.post_date >= DATE_SUB(NOW(), INTERVAL 90 DAY)"
        );
    }
    set_transient( $szd_avg_ship_key, $szd_avg_ship_days ?? '0', 30 * MINUTE_IN_SECONDS );
}
$szd_avg_ship_label = ( $szd_avg_ship_days !== null && (float) $szd_avg_ship_days > 0 )
    ? number_format( (float) $szd_avg_ship_days, 1, ',', '.' ) . ' dias'
    : '—';

//  Saldos (server-side, não reagem ao filtro de período)
$szd_cod_sum   = ( $szd_w_uid && function_exists( 'sz_cod_wallet_summary' ) )
    ? sz_cod_wallet_summary( $szd_w_uid )
    : [ 'available' => 0.0, 'pending' => 0.0 ];
$szd_cod_avail = (float) ( $szd_cod_sum['available'] ?? 0 );
$szd_cod_pend  = (float) ( $szd_cod_sum['pending']   ?? 0 );

$szd_tpc_avail  = 0.0;
$szd_has_exp    = false;
if ( ! $szd_is_aff && $szd_wp_uid ) {
    $szd_has_exp = ! function_exists( 'sz_producer_has_expedicao' )
        || sz_producer_has_expedicao( $szd_wp_uid );
    if ( $szd_has_exp && function_exists( 'tpc_get_saldo_disponivel' ) ) {
        $szd_tpc_avail = (float) tpc_get_saldo_disponivel( $szd_wp_uid );
    }
}

//  Pedidos (única chamada) 
// v445: cap configurável via option (default 500). Limite usado apenas para
// performance visual do Dashboard Home — não afeta cálculo financeiro nem saldo.
// Para bases muito grandes, ajustar com cautela: `wp option update senderzz_dashboard_v2_home_cap 800`.
$szd_cap = (int) get_option( 'senderzz_dashboard_v2_home_cap', 500 );
if ( $szd_cap < 50 || $szd_cap > 2000 ) {
    $szd_cap = 500; // guarda-corpo contra valores extremos
}
$szd_all = function_exists( 'senderzz_get_visible_orders_for_user' )
    ? senderzz_get_visible_orders_for_user( $sz_v2_user, $szd_cap ) : [];

$szd_is_mb = static function( array $o ): bool {
    if ( ( $o['delivery_mode'] ?? '' ) === 'motoboy' ) return true;
    $hay = strtolower( ( $o['shipping_name'] ?? '' ) . ' ' . ( $o['shipping_method'] ?? '' ) );
    return strpos( $hay, 'motoboy' ) !== false;
};

//  Helper mb_strimwidth seguro 
$szd_trim = static function( string $s, int $len, string $suffix = '…' ): string {
    if ( $s === '' ) return '';
    if ( function_exists( 'mb_strimwidth' ) ) {
        return mb_strimwidth( $s, 0, $len, $suffix );
    }
    return strlen( $s ) > $len ? substr( $s, 0, $len - strlen( $suffix ) ) . $suffix : $s;
};

//  Moeda 
$szd_money = static fn( float $v ): string =>
    function_exists( 'senderzz_portal_money' )
        ? senderzz_portal_money( $v )
        : 'R$ ' . number_format( $v, 2, ',', '.' );

//  Construir JSON seguro por pedido (dados mínimos) 
// E-mail: apenas hash SHA1 truncado (10 chars) para contagem de clientes únicos.
// Nenhum dado financeiro pessoal além do valor do pedido (que aparece na tabela).
$szd_json_rows = [];
foreach ( $szd_all as $szd_o ) {
    $szd_st  = strtolower( str_replace( 'wc-', '', (string) ( $szd_o['status'] ?? '' ) ) );
    $szd_dt  = substr( (string) ( $szd_o['date_machine'] ?? '' ), 0, 10 );
    $szd_grp = $szd_is_mb( $szd_o ) ? 'cod' : 'exp';
    $szd_email_raw = strtolower( trim( (string) ( $szd_o['billing']['email'] ?? '' ) ) );
    $szd_email_key = $szd_email_raw !== '' ? substr( sha1( $szd_email_raw ), 0, 10 ) : '';
    $szd_address   = strtolower( wp_strip_all_tags( (string) ( $szd_o['billing']['address'] ?? $szd_o['shipping']['address'] ?? '' ) ) );
    // Detecta região pela mesma lógica do calc_dashboard_metrics
    $szd_region = 'Outras';
    foreach ( [ 'são paulo' => 'São Paulo', 'sp' => 'São Paulo', 'rio de janeiro' => 'Rio de Janeiro', 'rj' => 'Rio de Janeiro',
                'minas gerais' => 'Minas Gerais', 'mg' => 'Minas Gerais', 'paraná' => 'Paraná', 'pr' => 'Paraná',
                'santa catarina' => 'Santa Catarina', 'sc' => 'Santa Catarina', 'rio grande do sul' => 'Rio Grande do Sul',
                'rs' => 'Rio Grande do Sul', 'bahia' => 'Bahia', 'ba' => 'Bahia', 'pernambuco' => 'Pernambuco',
                'pe' => 'Pernambuco', 'ceará' => 'Ceará', 'ce' => 'Ceará', 'goiás' => 'Goiás', 'go' => 'Goiás',
                'distrito federal' => 'Distrito Federal', 'df' => 'Distrito Federal' ] as $szd_needle => $szd_rname ) {
        if ( strpos( $szd_address, $szd_needle ) !== false ) { $szd_region = $szd_rname; break; }
    }
    $szd_json_rows[] = [
        'g'  => $szd_grp,
        'd'  => $szd_dt,
        'st' => $szd_st,
        'v'  => round( (float) ( $szd_o['total_no_ship_raw'] ?? $szd_o['total_raw'] ?? 0 ), 2 ),
        'co' => round( (float) ( $szd_o['affiliate_commission'] ?? 0 ), 2 ),
        'p'  => $szd_trim( (string) ( $szd_o['product_name'] ?? '' ), 64 ),
        'e'  => $szd_email_key,
        'ca' => $szd_trim( (string) ( $szd_o['shipping_name'] ?? '' ), 40 ),
        'r'  => $szd_region,
        'i'  => (int) ( $szd_o['items_count'] ?? 1 ),
        'fr' => round( (float) ( $szd_o['shipping_total_raw'] ?? 0 ), 2 ),
        'af' => $szd_trim( (string) ( $szd_o['affiliate_name'] ?? '' ), 48 ),
    ];
}
?>
<section id="sec-dashboard" class="sz-sec" data-szv2-label="Dashboard" data-nonce="<?php echo esc_attr( wp_create_nonce('senderzz_portal') ); ?>">

<!-- Dados serializados para recalculo client-side.
     Chaves compactas para reduzir payload. Sem PII direta. -->
<script type="application/json" id="szv2-dash-data"><?php
echo wp_json_encode( [
    'orders'    => $szd_json_rows,
    'is_aff'    => $szd_is_aff,
    'cod_avail' => $szd_cod_avail,
    'tpc_avail' => $szd_tpc_avail,
    'currency'  => html_entity_decode( get_woocommerce_currency_symbol(), ENT_QUOTES | ENT_HTML5, 'UTF-8' ),
], JSON_HEX_TAG | JSON_HEX_AMP | JSON_HEX_APOS | JSON_HEX_QUOT );
?></script>

<!--  Modo: Dashboard  -->
<div class="szv2-dash-top-tabs" role="tablist" style="display:flex;align-items:center;justify-content:space-between;margin-bottom:var(--szv2-space-3)">
    <div style="display:flex;gap:0">
        <button type="button" class="szv2-dash-top-tab szv2-dash-top-tab--active" data-top-tab="visao" onclick="szV2DashTopTab('visao',this)">Visão geral</button>
    </div>
    <button type="button" class="szv2-btn szv2-btn-secondary szv2-btn-sm" id="szv2-dash-export-btn"
            onclick="szV2DashExportXlsx(this)"
            style="display:flex;align-items:center;gap:6px">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
        Exportar Excel
    </button>
</div>

<div id="szv2-dash-top-visao">
<!--  Seletor COD / Expedição  -->
<div class="szv2-dash-switcher" data-szv2-tabs="dashboard-mode" role="tablist" aria-label="Modo de visualização">
    <button type="button" class="szv2-dash-tab szv2-dash-tab--active"
            role="tab" data-szv2-tab="cod" aria-selected="true">
        <svg viewBox="0 0 20 20" aria-hidden="true"><path d="M5 14a2.5 2.5 0 1 0 0 .01zM15 14a2.5 2.5 0 1 0 0 .01zM11 5h3l3 4v4h-2a3 3 0 0 0-6 0H8a3 3 0 0 0-5.4-1.8L2 9l4-1 2-3h3z"/></svg>
        Cash on Delivery
    </button>
    <?php if ( $szd_has_exp ) : ?>
    <button type="button" class="szv2-dash-tab"
            role="tab" data-szv2-tab="exp" aria-selected="false">
        <svg viewBox="0 0 20 20" aria-hidden="true"><path d="M10 2 3 5.5v9L10 18l7-3.5v-9L10 2zm0 2.2 4.6 2.3L10 8.8 5.4 6.5 10 4.2zM5 8.1l4 2v5.3l-4-2V8.1zm10 0v5.3l-4 2v-5.3l4-2z"/></svg>
        Expedição
    </button>
    <?php endif; ?>
</div>

<!--  PAINEL COD  -->
<div class="szv2-dash-panel" data-szv2-panel-group="dashboard-mode" data-szv2-panel="cod">

    <div class="szv2-period-bar" data-szv2-period-group="cod" role="group" aria-label="Período COD">
        <span class="szv2-period-label">Período:</span>
        <button type="button" class="szv2-period-btn" data-days="1"  data-szv2-period="cod">Hoje</button>
        <button type="button" class="szv2-period-btn" data-days="7"  data-szv2-period="cod" class="szv2-period-btn szv2-period-btn--active">7 dias</button>
        <button type="button" class="szv2-period-btn" data-days="30" data-szv2-period="cod">30 dias</button>
        <button type="button" class="szv2-period-btn" data-days="90" data-szv2-period="cod">90 dias</button>
        <span style="display:inline-flex;align-items:center;gap:4px;margin-left:6px">
            <label style="font-size:12px;color:var(--szv2-text-muted);white-space:nowrap">De</label>
            <input type="date" id="szv2-dash-from" class="szv2-input szv2-input-sm"
                   value="<?php echo esc_attr( date( 'Y-m-d', strtotime( '-7 days' ) ) ); ?>"
                   style="font-size:12px;padding:3px 6px;"
                   onchange="szV2DashCustomRange()">
            <label style="font-size:12px;color:var(--szv2-text-muted);white-space:nowrap">até</label>
            <input type="date" id="szv2-dash-to" class="szv2-input szv2-input-sm"
                   value="<?php echo esc_attr( current_time( 'Y-m-d' ) ); ?>"
                   style="font-size:12px;padding:3px 6px;"
                   onchange="szV2DashCustomRange()">
        </span>
    </div>

    <!-- KPIs COD (slots preenchidos pelo JS) -->
    <div class="szv2-kpi-grid" id="szv2-cod-kpis">
        <?php
        echo sz_v2_kpi_card( [ 'label' => 'Saldo disponível', 'value' => esc_html( $szd_money( $szd_cod_avail ) ), 'value_class' => 'szv2-num', 'data_kpi' => 'cod-avail' ] ); // phpcs:ignore
        echo sz_v2_kpi_card( [ 'label' => 'Faturamento', 'value_class' => 'szv2-num', 'data_kpi' => 'cod-rev' ] ); // phpcs:ignore
        if ( $szd_is_aff ) :
            echo sz_v2_kpi_card( [ 'label' => 'Suas comissões', 'value_class' => 'szv2-num', 'data_kpi' => 'cod-comm' ] ); // phpcs:ignore
        endif;
        echo sz_v2_kpi_card( [ 'label' => 'Pedidos no período', 'value_class' => 'szv2-num', 'data_kpi' => 'cod-total' ] ); // phpcs:ignore
        echo sz_v2_kpi_card( [ 'label' => 'Ticket médio', 'value_class' => 'szv2-num', 'data_kpi' => 'cod-ticket' ] ); // phpcs:ignore
        // KPI: Ticket Médio COD — pedidos COD ativos/finalizados
        $sz_dash_mb_orders = array_filter( $szd_all, static fn( $o ) => $szd_is_mb( $o ) );
        $sz_dash_mb_ativos = array_filter( $sz_dash_mb_orders, static fn( $o ) => in_array(
            strtolower( str_replace( 'wc-', '', (string) ( $o['status'] ?? '' ) ) ),
            [ 'entregue', 'completo', 'completed', 'embalado', 'em_rota', 'agendado', 'acaminho' ], true
        ) );
        $sz_dash_mb_total      = array_sum( array_map( static fn( $o ) => (float) ( $o['total_no_ship_raw'] ?? 0 ), $sz_dash_mb_ativos ) );
        $sz_dash_ticket_medio  = count( $sz_dash_mb_ativos ) > 0 ? $sz_dash_mb_total / count( $sz_dash_mb_ativos ) : 0.0;
        echo sz_v2_kpi_card( [ 'label' => 'TICKET MÉDIO COD', 'value' => esc_html( 'R$ ' . number_format( $sz_dash_ticket_medio, 2, ',', '.' ) ), 'value_class' => 'szv2-num' ] ); // phpcs:ignore
        ?>
    </div>

    <!-- Linha operacional: situação + eficiência -->
    <div class="szv2-dash-row">
        <div class="szv2-card szv2-dash-opcard" style="flex:1.4">
            <div class="szv2-card-head">
                <h2>Pedidos por status</h2>
                <span class="szv2-card-sub" data-szv2-kpi="cod-total2">— no período</span>
            </div>
            <!-- Status bars — renderizadas pelo JS em szv2-cod-status-rows -->
            <div id="szv2-cod-status-rows" style="display:flex;flex-direction:column;gap:6px;margin-top:4px">
                <!-- JS preenche com rows: label | bar | count -->
                <div style="font-size:12px;color:var(--szv2-text-faint)">Carregando...</div>
            </div>
        </div>
        <div class="szv2-card szv2-dash-opcard" style="flex:1">
            <div class="szv2-card-head"><h2>Eficiência logística</h2></div>
            <div class="szv2-eff-row">
                <span class="szv2-eff-pct" data-szv2-kpi="cod-pct" style="font-size:28px;font-weight:600;color:var(--szv2-brand)">—</span>
                <div class="szv2-eff-bar-wrap"><div class="szv2-eff-bar-fill" data-szv2-bar="cod-pct"></div></div>
                <span class="szv2-eff-label">entregas concluídas</span>
            </div>
            <div style="display:grid;grid-template-columns:1fr 1fr;gap:8px;margin-top:12px">
                <div style="text-align:center;padding:8px;background:var(--szv2-surface-alt);border-radius:var(--szv2-radius-md)">
                    <div style="font-size:18px;font-weight:600;color:var(--szv2-text)" data-szv2-kpi="cod-active">—</div>
                    <div style="font-size:11px;color:var(--szv2-text-muted)">Em rota</div>
                </div>
                <div style="text-align:center;padding:8px;background:var(--szv2-surface-alt);border-radius:var(--szv2-radius-md)">
                    <div style="font-size:18px;font-weight:600;color:var(--szv2-danger)" data-szv2-kpi="cod-frustr">—</div>
                    <div style="font-size:11px;color:var(--szv2-text-muted)">Frustrados</div>
                </div>
                <div style="text-align:center;padding:8px;background:var(--szv2-surface-alt);border-radius:var(--szv2-radius-md)">
                    <div style="font-size:18px;font-weight:600;color:var(--szv2-text)" data-szv2-kpi="cod-done">—</div>
                    <div style="font-size:11px;color:var(--szv2-text-muted)">Entregues</div>
                </div>
                <div style="text-align:center;padding:8px;background:var(--szv2-surface-alt);border-radius:var(--szv2-radius-md)">
                    <div style="font-size:18px;font-weight:600;color:var(--szv2-text-muted)" data-szv2-kpi="cod-canc">—</div>
                    <div style="font-size:11px;color:var(--szv2-text-muted)">Cancelados</div>
                </div>
            </div>
        </div>
    </div>

    <!-- Produtos + Regiões side by side -->
    <div class="szv2-dash-row">
        <div class="szv2-card" style="flex:1.4">
            <div class="szv2-card-head"><h2>Produtos mais vendidos</h2></div>
            <div class="szv2-table-wrap szv2-table-flush">
                <table class="szv2-table">
                    <thead><tr>
                        <th>Produto</th>
                        <th class="szv2-td-num">Pedidos</th>
                        <th class="szv2-td-num">Faturamento</th>
                        <?php if ( $szd_is_aff ) : ?><th class="szv2-td-num">Comissão</th><?php endif; ?>
                    </tr></thead>
                    <tbody data-szv2-list="cod-products" data-is-aff="<?php echo $szd_is_aff ? '1' : '0'; ?>"></tbody>
                </table>
            </div>
        </div>
        <div class="szv2-card" style="flex:1">
            <div class="szv2-card-head">
                <h2>Pedidos por região</h2>
                <span class="szv2-card-sub" data-szv2-kpi="cod-total2">—</span>
            </div>
            <div class="szv2-region-list" data-szv2-list="cod-regions"></div>
        </div>
    </div>

    <!-- Afiliados (somente produtor) -->
    <?php if ( ! $szd_is_aff ) : ?>
    <div class="szv2-card" id="szv2-cod-affiliates">
        <div class="szv2-card-head"><h2>Afiliados ativos no período</h2></div>
        <div class="szv2-table-wrap szv2-table-flush">
            <table class="szv2-table">
                <thead><tr><th>Afiliado</th><th class="szv2-td-num">Pedidos</th><th class="szv2-td-num">Faturamento</th><th class="szv2-td-num">Comissão</th></tr></thead>
                <tbody data-szv2-list="cod-affiliates"></tbody>
            </table>
        </div>
    </div>
    <?php endif; ?>

    <p class="szv2-dash-nav-link">
        <button type="button" class="szv2-link-btn" data-nav="motoboy" onclick="szGo('motoboy',this)">Ver pedidos COD →</button>
    </p>
</div><!-- /painel cod -->

<!--  PAINEL EXPEDIÇÃO  -->
<?php if ( $szd_has_exp ) : ?>
<div class="szv2-dash-panel" data-szv2-panel-group="dashboard-mode" data-szv2-panel="exp" hidden>

    <div class="szv2-period-bar" data-szv2-period-group="exp" role="group" aria-label="Período Expedição">
        <span class="szv2-period-label">Período:</span>
        <button type="button" class="szv2-period-btn" data-days="1"  data-szv2-period="exp">Hoje</button>
        <button type="button" class="szv2-period-btn" data-days="7"  data-szv2-period="exp">7 dias</button>
        <button type="button" class="szv2-period-btn szv2-period-btn--active" data-days="30" data-szv2-period="exp">30 dias</button>
        <button type="button" class="szv2-period-btn" data-days="90" data-szv2-period="exp">90 dias</button>
    </div>

    <!-- KPIs Expedição (slots preenchidos pelo JS) -->
    <div class="szv2-kpi-grid" id="szv2-exp-kpis">
        <?php
        echo sz_v2_kpi_card( [ 'label' => 'Saldo Expedição', 'value' => esc_html( $szd_money( $szd_tpc_avail ) ), 'value_class' => 'szv2-num', 'data_kpi' => 'exp-avail' ] ); // phpcs:ignore
        echo sz_v2_kpi_card( [ 'label' => 'Total cobrado', 'value_class' => 'szv2-num', 'data_kpi' => 'exp-rev' ] ); // phpcs:ignore
        echo sz_v2_kpi_card( [ 'label' => 'Pedidos Expedição', 'meta' => 'pedidos no período', 'value_class' => 'szv2-num', 'data_kpi' => 'exp-total' ] ); // phpcs:ignore
        echo sz_v2_kpi_card( [ 'label' => 'Total de clientes', 'value_class' => 'szv2-num', 'data_kpi' => 'exp-clients' ] ); // phpcs:ignore
        echo sz_v2_kpi_card( [ 'label' => 'Total de produtos', 'meta' => 'itens expedidos', 'value_class' => 'szv2-num', 'data_kpi' => 'exp-items' ] ); // phpcs:ignore
        echo sz_v2_kpi_card( [ 'label' => 'Custo médio de frete', 'value_class' => 'szv2-num', 'data_kpi' => 'exp-avgship' ] ); // phpcs:ignore
        ?>
    </div>

    <!-- Transportadora destaque + Tempo médio -->
    <div class="szv2-dash-row">
        <div class="szv2-card szv2-dash-opcard">
            <div class="szv2-card-head"><h2>Transportadora destaque</h2></div>
            <p class="szv2-card-sub">Mais utilizada no período</p>
            <div data-szv2-list="exp-carrier-highlight"></div>
        </div>
        <div class="szv2-card szv2-dash-opcard">
            <div class="szv2-card-head"><h2>Tempo médio de envio</h2></div>
            <p class="szv2-kpi-value szv2-num" style="font-size:28px;font-weight:800;margin:4px 0"><?php echo esc_html( $szd_avg_ship_label ); ?></p>
            <p class="szv2-card-sub">Pedido → despacho (últimos 90 dias)</p>
            <div class="szv2-meta-row">
                <span class="szv2-meta-label">Ticket médio</span>
                <span class="szv2-meta-val szv2-num" data-szv2-kpi="exp-ticket">—</span>
            </div>
        </div>
    </div>

    <!-- Situação dos pedidos Expedição -->
    <div class="szv2-card">
        <div class="szv2-card-head"><h2>Situação dos pedidos</h2></div>
        <div class="szv2-opcard-grid szv2-opcard-grid-3">
            <div class="szv2-opcard-item"><span class="szv2-opcard-val" data-szv2-kpi="exp-pend">—</span><span class="szv2-opcard-label">Aguardando envio</span></div>
            <div class="szv2-opcard-item"><span class="szv2-opcard-val" data-szv2-kpi="exp-transit">—</span><span class="szv2-opcard-label">Em trânsito</span></div>
            <div class="szv2-opcard-item"><span class="szv2-opcard-val" data-szv2-kpi="exp-done">—</span><span class="szv2-opcard-label">Entregues</span></div>
            <div class="szv2-opcard-item szv2-opcard-item--muted"><span class="szv2-opcard-val" data-szv2-kpi="exp-canc">—</span><span class="szv2-opcard-label">Cancelados</span></div>
            <div class="szv2-opcard-item"><span class="szv2-opcard-val" data-szv2-kpi="exp-pct">—</span><span class="szv2-opcard-label">Taxa de entrega</span></div>
            <div class="szv2-opcard-item"><span class="szv2-opcard-val" data-szv2-kpi="exp-clients2">—</span><span class="szv2-opcard-label">Clientes únicos</span></div>
        </div>
    </div>

    <!-- Status breakdown Expedição -->
    <div class="szv2-card">
        <div class="szv2-card-head"><h2>Pedidos por status</h2></div>
        <div class="szv2-region-list" data-szv2-list="exp-status"></div>
    </div>

    <!-- Produtos mais expedidos -->
    <div class="szv2-card">
        <div class="szv2-card-head"><h2>Produtos mais expedidos</h2></div>
        <div class="szv2-table-wrap szv2-table-flush">
            <table class="szv2-table">
                <thead><tr><th>Produto</th><th class="szv2-td-num">Expedições</th><th class="szv2-td-num">Faturamento</th><th class="szv2-td-num">%</th></tr></thead>
                <tbody data-szv2-list="exp-products"></tbody>
            </table>
        </div>
    </div>

    <!-- Regiões Expedição -->
    <div class="szv2-card">
        <div class="szv2-card-head"><h2>Expedições por região</h2></div>
        <div class="szv2-region-list" data-szv2-list="exp-regions"></div>
    </div>

    <p class="szv2-dash-nav-link">
        <button type="button" class="szv2-link-btn" data-nav="expedicao" onclick="szGo('expedicao',this)">Ver expedições →</button>
    </p>
</div><!-- /painel exp -->
<?php endif; ?>

</div><!-- /szv2-dash-top-visao -->

</section>
<script>
(function(){
// Desativa botões de atalho quando intervalo personalizado é usado
window.szV2DashCustomRange = function() {
    document.querySelectorAll('.szv2-period-btn').forEach(function(b){ b.classList.remove('szv2-period-btn--active'); });
};

// Renderiza barras de status no painel COD
window.szV2RenderStatusBars = function(statusCounts, total) {
    var container = document.getElementById('szv2-cod-status-rows');
    if (!container) return;
    var labels = { entregue:'Entregue', acaminho:'A caminho', emrota:'Em rota', frustrado:'Frustrado', cancelado:'Cancelado', cancelled:'Cancelado', agendado:'Agendado', embalado:'Embalado', devolvido:'Devolvido' };
    var colors = { entregue:'var(--szv2-success)', acaminho:'#378ADD', emrota:'#378ADD', frustrado:'var(--szv2-danger)', cancelado:'var(--szv2-text-faint)', cancelled:'var(--szv2-text-faint)', agendado:'var(--szv2-brand)', embalado:'var(--szv2-brand)', devolvido:'var(--szv2-warning)' };
    var html = '';
    var sorted = Object.keys(statusCounts).sort(function(a,b){ return statusCounts[b]-statusCounts[a]; });
    sorted.forEach(function(st) {
        var cnt = statusCounts[st]; if (!cnt) return;
        var pct = total > 0 ? Math.round(cnt/total*100) : 0;
        var lbl = labels[st] || st.charAt(0).toUpperCase() + st.slice(1);
        var col = colors[st] || 'var(--szv2-brand)';
        html += '<div style="display:flex;align-items:center;gap:10px">'
              + '<span style="min-width:88px;font-size:12px;color:var(--szv2-text-secondary);text-align:right">' + lbl + '</span>'
              + '<div style="flex:1;height:6px;background:var(--szv2-surface-alt);border-radius:3px;overflow:hidden">'
              + '<div style="width:' + pct + '%;height:100%;background:' + col + ';border-radius:3px;transition:width .4s ease"></div>'
              + '</div>'
              + '<span style="min-width:64px;font-size:12px;color:var(--szv2-text-muted);text-align:right">' + cnt + ' (' + pct + '%)</span>'
              + '</div>';
    });
    container.innerHTML = html || '<div style="font-size:12px;color:var(--szv2-text-faint)">Sem dados no período.</div>';
};

// Export Excel
window.szV2DashExportXlsx = function(btn) {
    var fromEl = document.getElementById('szv2-dash-from');
    var toEl   = document.getElementById('szv2-dash-to');
    var today  = new Date(); var pad = function(n){ return n < 10 ? '0'+n : n; };
    var defTo  = today.getFullYear()+'-'+pad(today.getMonth()+1)+'-'+pad(today.getDate());
    var defFr  = new Date(today - 30*86400000);
    var defFrom = defFr.getFullYear()+'-'+pad(defFr.getMonth()+1)+'-'+pad(defFr.getDate());
    var from   = fromEl ? fromEl.value : defFrom;
    var to     = toEl   ? toEl.value   : defTo;
    var mode   = document.querySelector('[data-szv2-tab="exp"].szv2-dash-tab--active') ? 'expedicao' : 'motoboy';
    var secEl  = document.getElementById('sec-dashboard');
    var nonce  = secEl && secEl.dataset.nonce ? secEl.dataset.nonce : '';
    var form = document.createElement('form');
    form.method = 'POST'; form.action = window.ajaxurl || '';
    var fields = { action:'senderzz_portal', szaction:'export_orders_csv', mode:mode, date_from:from, date_to:to, _ajax_nonce: nonce };
    Object.keys(fields).forEach(function(k){ var i=document.createElement('input'); i.type='hidden'; i.name=k; i.value=fields[k]; form.appendChild(i); });
    document.body.appendChild(form); form.submit(); document.body.removeChild(form);
};
})();
</script>
