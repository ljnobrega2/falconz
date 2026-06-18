<?php
/**
 * Senderzz V2 – Relatórios (v457 — filtro de data, COD/Expedição, exportar CSV)
 * @var object $sz_v2_user
 */
defined( 'ABSPATH' ) || exit;

$sz8rp_nonce    = wp_create_nonce( 'senderzz_portal' );
$sz8rp_ajax     = admin_url( 'admin-ajax.php' );
$sz8rp_is_aff   = 'affiliate' === strtolower( trim( (string) ( $sz_v2_user->role ?? '' ) ) );
$sz8rp_wp_uid   = (int) ( $sz_v2_user->wp_user_id ?? 0 );
$sz8rp_has_exp  = ! $sz8rp_is_aff && $sz8rp_wp_uid
    && ( ! function_exists( 'sz_producer_has_expedicao' ) || sz_producer_has_expedicao( $sz8rp_wp_uid ) );

$sz8rp_money = static fn( float $v ): string =>
    function_exists( 'senderzz_portal_money' ) ? senderzz_portal_money( $v ) : 'R$ ' . number_format( $v, 2, ',', '.' );

// Carregar pedidos reais
$sz8rp_all = function_exists( 'senderzz_get_visible_orders_for_user' )
    ? senderzz_get_visible_orders_for_user( $sz_v2_user, 500 ) : [];

// Separar COD e Expedição
$sz8rp_is_mb = static function( array $o ): bool {
    if ( ( $o['delivery_mode'] ?? '' ) === 'motoboy' ) return true;
    if ( ! empty( $o['motoboy_status'] ) ) return true;
    $mbst = [ 'agendado','embalado','acaminho','emrota','em_rota','entregue','frustrado','devolvido' ];
    return in_array( (string) ( $o['status'] ?? '' ), $mbst, true );
};
$sz8rp_cod = array_values( array_filter( $sz8rp_all, $sz8rp_is_mb ) );
$sz8rp_exp = array_values( array_filter( $sz8rp_all, fn($o) => ! $sz8rp_is_mb($o) ) );

// Métricas por grupo
function sz9rp_metrics( array $orders, callable $money ): array {
    if ( empty( $orders ) ) return [ 'total'=>0,'receita'=>0.0,'cancelados'=>0,'ticket'=>0.0,'by_status'=>[],'by_product'=>[],'by_region'=>[] ];
    $total = count( $orders );
    $receita = 0.0; $cancelados = 0; $by_status = []; $by_product = []; $by_region = [];
    foreach ( $orders as $o ) {
        $st = (string) ( $o['status'] ?? '' );
        $receita += (float) ( $o['total_no_ship_raw'] ?? $o['total_raw'] ?? 0 );
        if ( in_array( $st, [ 'cancelled', 'cancelado' ], true ) ) $cancelados++;
        $by_status[ $st ] = ( $by_status[ $st ] ?? 0 ) + 1;
        $p = (string) ( $o['product_name'] ?? '' );
        if ( $p ) { if ( ! isset($by_product[$p]) ) $by_product[$p] = ['qty'=>0,'rev'=>0.0]; $by_product[$p]['qty']++; $by_product[$p]['rev'] += (float)($o['total_no_ship_raw']??0); }
        $r = (string) ( $o['region'] ?? '' );
        if ( $r ) $by_region[ $r ] = ( $by_region[ $r ] ?? 0 ) + 1;
    }
    uasort( $by_product, fn($a,$b) => $b['rev']<=>$a['rev'] );
    arsort( $by_region );
    return [ 'total'=>$total,'receita'=>$receita,'cancelados'=>$cancelados,'ticket'=>$total>0?round($receita/$total,2):0,'by_status'=>$by_status,'by_product'=>array_slice($by_product,0,10,true),'by_region'=>array_slice($by_region,0,6,true) ];
}

$sz8rp_m_cod = sz9rp_metrics( $sz8rp_cod, $sz8rp_money );
$sz8rp_m_exp = sz9rp_metrics( $sz8rp_exp, $sz8rp_money );

$sz8rp_st_labels = [
    'agendado'=>['Agendado','brand'],'embalado'=>['Embalado','info'],'acaminho'=>['Em rota','info'],
    'entregue'=>['Entregue','success'],'frustrado'=>['Frustrado','danger'],'cancelado'=>['Cancelado','neutral'],
    'cancelled'=>['Cancelado','neutral'],'aprovado'=>['Aprovado','success'],'enviado'=>['Enviado','info'],
    'extravio'=>['Extravio','danger'],'devolvido'=>['Devolvido','warning'],
];

// Datas padrão (últimos 30 dias)
$sz8rp_to   = current_time( 'Y-m-d' );
$sz8rp_from = date( 'Y-m-d', strtotime( '-30 days' ) );
?>
<section id="sec-reports" class="sz-sec" data-szv2-label="Relatórios">

    <!-- Filtros + seletor de modalidade + exportar -->
    <div class="szv2-card szv2-report-filter-bar">
        <div style="display:flex;align-items:center;gap:var(--szv2-space-3);flex-wrap:wrap">
            <?php if ( $sz8rp_has_exp ) : ?>
            <div style="display:flex;gap:0;background:var(--szv2-surface-alt);border:1px solid var(--szv2-border);border-radius:var(--szv2-radius-md);padding:3px">
                <button type="button" id="szv2-rp-tab-cod"
                        class="szv2-btn szv2-btn-brand szv2-btn-sm"
                        style="border-radius:calc(var(--szv2-radius-md) - 3px);min-width:130px"
                        data-action="rp-modal" data-rp-target="cod">Motoboy / COD</button>
                <button type="button" id="szv2-rp-tab-exp"
                        class="szv2-btn szv2-btn-ghost szv2-btn-sm"
                        style="border-radius:calc(var(--szv2-radius-md) - 3px);min-width:130px;border:none;background:transparent"
                        data-action="rp-modal" data-rp-target="exp">Expedição</button>
            </div>
            <?php endif; ?>
            <div style="display:flex;gap:8px;align-items:center">
                <label class="szv2-label" style="white-space:nowrap;margin:0">De</label>
                <input type="date" id="szv2-rp-from" class="szv2-input szv2-input-sm" value="<?php echo esc_attr( $sz8rp_from ); ?>">
                <label class="szv2-label" style="white-space:nowrap;margin:0">Até</label>
                <input type="date" id="szv2-rp-to" class="szv2-input szv2-input-sm" value="<?php echo esc_attr( $sz8rp_to ); ?>">
            </div>
            <select id="szv2-rp-status" class="szv2-input szv2-input-sm">
                <option value="">Todos os status</option>
                <option value="entregue">Entregue</option>
                <option value="frustrado">Frustrado</option>
                <option value="cancelado">Cancelado</option>
                <option value="acaminho">Em rota</option>
            </select>
            <button type="button" class="szv2-btn szv2-btn-brand szv2-btn-sm" data-action="rp-filter">Filtrar</button>
        </div>
        <div style="display:flex;gap:8px">
            <?php if ( ! $sz8rp_is_aff ) : ?>
            <button type="button" class="szv2-btn szv2-btn-secondary szv2-btn-sm"
                    id="szv2-rp-export-btn"
                    data-ajax="<?php echo esc_attr( $sz8rp_ajax ); ?>"
                    data-nonce="<?php echo esc_attr( $sz8rp_nonce ); ?>"
                    data-mode="motoboy"
                    data-action="rp-export">↓ Exportar</button>
            <?php endif; ?>
        </div>
    </div>
    <script>
    function szV2RpSwitchModal(mode) {
        var isCod = (mode === 'cod');
        var panels = { cod: document.getElementById('szv2-panel-rp-cod'), exp: document.getElementById('szv2-panel-rp-exp') };
        var tabs   = { cod: document.getElementById('szv2-rp-tab-cod'),   exp: document.getElementById('szv2-rp-tab-exp') };
        var exp = document.getElementById('szv2-rp-export-btn');
        if (panels.cod) panels.cod.style.display = isCod ? '' : 'none';
        if (panels.exp) panels.exp.style.display = isCod ? 'none' : '';
        if (tabs.cod) { tabs.cod.className = isCod ? 'szv2-btn szv2-btn-brand szv2-btn-sm' : 'szv2-btn szv2-btn-secondary szv2-btn-sm'; tabs.cod.style.cssText = ''; }
        if (tabs.exp) { tabs.exp.className = isCod ? 'szv2-btn szv2-btn-secondary szv2-btn-sm' : 'szv2-btn szv2-btn-brand szv2-btn-sm'; tabs.exp.style.cssText = ''; }
        if (exp) { exp.dataset.mode = isCod ? 'motoboy' : 'expedicao'; }
    }
    function szV2RpExportActive(btn) {
        var mode = btn.dataset.mode || 'motoboy';
        var label = (mode === 'expedicao') ? 'Expedicao' : 'COD';
        if (typeof szV2RpExport === 'function') szV2RpExport(btn, label);
    }
    (function(){
        var root = document.getElementById('sec-reports');
        if (!root) return;
        root.addEventListener('click', function(e) {
            var rp = e.target.closest('[data-action^="rp-"]');
            if (!rp) return;
            var act = rp.getAttribute('data-action');
            if (act === 'rp-modal' && typeof window.szV2RpSwitchModal === 'function') { szV2RpSwitchModal(rp.getAttribute('data-rp-target')); }
            else if (act === 'rp-filter' && typeof window.szV2RpFilter === 'function') { szV2RpFilter(); }
            else if (act === 'rp-export' && typeof window.szV2RpExportActive === 'function') { szV2RpExportActive(rp); }
        });
    }());
    </script>

    <!-- Painel COD -->
    <div class="szv2-conn-panel" id="szv2-panel-rp-cod">
        <?php sz9rp_render_panel( $sz8rp_m_cod, $sz8rp_money, $sz8rp_st_labels, $sz8rp_is_aff ); ?>
    </div>

    <?php if ( $sz8rp_has_exp ) : ?>
    <!-- Painel Expedição -->
    <div class="szv2-conn-panel" id="szv2-panel-rp-exp" style="display:none">
        <?php sz9rp_render_panel( $sz8rp_m_exp, $sz8rp_money, $sz8rp_st_labels, $sz8rp_is_aff ); ?>
    </div>
    <?php endif; ?>

    <p id="szv2-rp-note" style="font-size:12px;color:var(--szv2-text-faint);margin-top:8px">
        Mostrando <?php echo esc_html( (string) count( $sz8rp_all ) ); ?> pedido(s) visíveis dos últimos 30 dias.
    </p>
</section>
<?php
function sz9rp_render_panel( array $m, callable $money, array $st_labels, bool $is_aff ): void {
    if ( $m['total'] === 0 ) :
        // phpcs:ignore WordPress.Security.EscapeOutput
        echo sz_v2_empty_state( [ 'title' => 'Sem pedidos no período', 'text' => 'Os relatórios aparecem assim que houver pedidos visíveis.' ] );
        return;
    endif;
    ?>
    <!-- KPIs -->
    <div class="szv2-kpi-grid">
        <?php // phpcs:ignore WordPress.Security.EscapeOutput
        echo sz_v2_kpi_card( [ 'label' => 'Total de pedidos',    'value' => (string) $m['total'],         'value_class' => 'szv2-num' ] );
        echo sz_v2_kpi_card( [ 'label' => 'Faturamento total',   'value' => $money( $m['receita'] ),      'value_class' => 'szv2-num' ] );
        echo sz_v2_kpi_card( [ 'label' => 'Cancelados',          'value' => (string) $m['cancelados'],    'value_class' => $m['cancelados'] > 0 ? 'szv2-num szv2-kpi-danger' : 'szv2-num' ] );
        echo sz_v2_kpi_card( [ 'label' => 'Ticket médio',        'value' => $money( $m['ticket'] ),       'value_class' => 'szv2-num' ] );
        ?>
    </div>

    <!-- Pedidos por status -->
    <div class="szv2-card">
        <div class="szv2-card-head">
            <h2>Pedidos por status</h2>
            <span class="szv2-card-sub"><?php echo esc_html( (string) $m['total'] ); ?> total</span>
        </div>
        <?php $sz9rp_st_total = array_sum( $m['by_status'] );
        foreach ( $m['by_status'] as $sz9rp_st => $sz9rp_cnt ) :
            $sz9rp_pct = $sz9rp_st_total > 0 ? (int) round( $sz9rp_cnt / $sz9rp_st_total * 100 ) : 0;
            $sz9rp_lbl = $st_labels[ $sz9rp_st ] ?? [ ucfirst( $sz9rp_st ), 'neutral' ]; ?>
        <div class="szv2-report-status-row">
            <span class="sz-badge szv2-badge-<?php echo esc_attr( $sz9rp_lbl[1] ); ?> szv2-report-status-badge"><?php echo esc_html( $sz9rp_lbl[0] ); ?></span>
            <div class="szv2-report-bar-track"><div class="szv2-report-bar szv2-report-bar--<?php echo esc_attr( $sz9rp_lbl[1] ); ?>" style="width:<?php echo esc_attr( (string) $sz9rp_pct ); ?>%"></div></div>
            <span class="szv2-report-count szv2-num"><?php echo esc_html( $sz9rp_cnt . ' (' . $sz9rp_pct . '%)' ); ?></span>
        </div>
        <?php endforeach; ?>
    </div>

    <!-- Produtos mais vendidos -->
    <?php if ( ! empty( $m['by_product'] ) ) : ?>
    <div class="szv2-card">
        <div class="szv2-card-head"><h2>Produtos mais vendidos</h2></div>
        <div class="szv2-table-wrap">
            <table class="szv2-table">
                <thead><tr><th>Produto</th><th class="szv2-td-num">Pedidos</th><th class="szv2-td-num">Faturamento</th></tr></thead>
                <tbody>
                <?php foreach ( $m['by_product'] as $sz9rp_pn => $sz9rp_pd ) : ?>
                <tr>
                    <td><?php echo esc_html( $sz9rp_pn ); ?></td>
                    <td class="szv2-td-num szv2-num"><?php echo esc_html( (string) $sz9rp_pd['qty'] ); ?></td>
                    <td class="szv2-td-num szv2-num"><?php echo esc_html( $money( $sz9rp_pd['rev'] ) ); ?></td>
                </tr>
                <?php endforeach; ?>
                </tbody>
            </table>
        </div>
    </div>
    <?php endif; ?>

    <!-- Pedidos por região -->
    <?php if ( ! empty( $m['by_region'] ) ) : ?>
    <div class="szv2-card">
        <div class="szv2-card-head">
            <h2>Pedidos por região</h2>
            <span class="szv2-card-sub"><?php echo esc_html( (string) $m['total'] ); ?> total</span>
        </div>
        <?php $sz9rp_rg_total = array_sum( $m['by_region'] );
        foreach ( $m['by_region'] as $sz9rp_rg => $sz9rp_rc ) :
            $sz9rp_rpct = $sz9rp_rg_total > 0 ? (int) round( $sz9rp_rc / $sz9rp_rg_total * 100 ) : 0; ?>
        <div class="szv2-report-status-row">
            <span class="szv2-report-region"><?php echo esc_html( $sz9rp_rg ); ?></span>
            <div class="szv2-report-bar-track"><div class="szv2-report-bar szv2-report-bar--brand" style="width:<?php echo esc_attr( (string) $sz9rp_rpct ); ?>%"></div></div>
            <span class="szv2-report-count szv2-num"><?php echo esc_html( $sz9rp_rc . ' (' . $sz9rp_rpct . '%)' ); ?></span>
        </div>
        <?php endforeach; ?>
    </div>
    <?php endif; ?>
    <?php
}
?>
