<?php
/**
 * Senderzz V2 – Pedidos (tela oficial com filtros)
 * Filtros: produto, status, afiliado, kit de venda, data de venda, data de entrega.
 * @var object $sz_v2_user
 */
defined( 'ABSPATH' ) || exit;

global $wpdb;

$sz9or_nonce     = wp_create_nonce( 'senderzz_portal' );
$sz9or_is_aff    = function_exists( 'sz_aff_portal_user_must_use_affiliate_scope' )
    && sz_aff_portal_user_must_use_affiliate_scope( $sz_v2_user );

$sz9or_all_orders = function_exists( 'senderzz_get_visible_orders_for_user' )
    ? senderzz_get_visible_orders_for_user( $sz_v2_user, 300 )
    : [];

// Coleta opções únicas para filtros
$sz9or_products   = [];
$sz9or_statuses   = [];
$sz9or_affiliates = [];
$sz9or_kits       = [];

foreach ( $sz9or_all_orders as $o ) {
    $prod  = (string) ( $o['senderzz_offer_name'] ?? $o['product_name'] ?? '' );
    $stat  = (string) ( $o['status'] ?? '' );
    $aff   = (string) ( $o['affiliate_name'] ?? '' );
    $kit   = (string) ( $o['senderzz_offer_name'] ?? '' );
    if ( $prod && ! isset( $sz9or_products[ $prod ] ) ) $sz9or_products[ $prod ] = $prod;
    if ( $stat && ! isset( $sz9or_statuses[ $stat ] ) ) $sz9or_statuses[ $stat ] = $stat;
    if ( $aff  && ! isset( $sz9or_affiliates[ $aff ] ) ) $sz9or_affiliates[ $aff ] = $aff;
    if ( $kit  && ! isset( $sz9or_kits[ $kit ] ) ) $sz9or_kits[ $kit ] = $kit;
}
ksort( $sz9or_products );
ksort( $sz9or_statuses );
ksort( $sz9or_affiliates );
ksort( $sz9or_kits );

$sz9or_status_labels = [
    'pending'       => 'Aguardando',
    'on-hold'       => 'Em espera',
    'processing'    => 'Processando',
    'aprovado'      => 'Aprovado',
    'enviado'       => 'Enviado',
    'entregue'      => 'Entregue',
    'completed'     => 'Concluído',
    'cancelled'     => 'Cancelado',
    'refunded'      => 'Reembolsado',
    'failed'        => 'Falhou',
    'agendado'      => 'Agendado',
    'embalado'      => 'Embalado',
    'em_rota'       => 'Em rota',
    'acaminho'      => 'A caminho',
    'frustrado'     => 'Frustrado',
    'devolvido'     => 'Devolvido',
    'cancelado'     => 'Cancelado',
    'coletado'      => 'Coletado',
    'asuspender'    => 'A suspender',
];

$sz9or_fmt_money = static function( $v ): string {
    return 'R$ ' . number_format( (float) $v, 2, ',', '.' );
};
?>
<section id="sec-orders" class="sz-sec" data-szv2-label="Pedidos">

    <!-- Filtros — layout consistente com motoboy -->
    <div style="display:flex;flex-direction:column;gap:10px;margin-bottom:16px">

        <!-- Status chips -->
        <?php
        $sz9or_status_counts = [];
        foreach ( $sz9or_all_orders as $sz9or__o ) {
            $s = (string) ( $sz9or__o['status'] ?? '' );
            $sz9or_status_counts[ $s ] = ( $sz9or_status_counts[ $s ] ?? 0 ) + 1;
        }
        $sz9or_chip_defs = array_merge( [ '' => 'Todos' ], array_combine(
            array_keys( $sz9or_statuses ),
            array_map( fn($v) => $sz9or_status_labels[$v] ?? ucfirst($v), array_keys($sz9or_statuses) )
        ) );
        ?>
        <div style="display:flex;gap:6px;flex-wrap:wrap;align-items:center">
            <?php foreach ( $sz9or_chip_defs as $sz9or_ck => $sz9or_cl ) :
                $sz9or_cn = $sz9or_ck === '' ? count($sz9or_all_orders) : ($sz9or_status_counts[$sz9or_ck] ?? 0);
            ?>
            <button type="button"
                    class="szv2-chip<?php echo $sz9or_ck === '' ? ' szv2-chip-active' : ''; ?>"
                    data-or-status="<?php echo esc_attr($sz9or_ck); ?>"
                    onclick="szV2OrStatusChip(this)">
                <?php echo esc_html($sz9or_cl); ?>
                <?php if ($sz9or_cn > 0) : ?>
                <span style="display:inline-flex;align-items:center;justify-content:center;min-width:16px;height:16px;padding:0 3px;font-size:10px;font-weight:700;border-radius:99px;background:rgba(0,0,0,.15);margin-left:3px;line-height:1"><?php echo esc_html((string)$sz9or_cn); ?></span>
                <?php endif; ?>
            </button>
            <?php endforeach; ?>
        </div>

        <!-- F5: Period buttons — JS szV2OrPeriod() aguardava esses botões mas não eram renderizados -->
        <div style="display:flex;gap:6px;flex-wrap:wrap;align-items:center">
            <button type="button" class="szv2-chip szv2-or-period-btn szv2-chip-active" data-days="0"  onclick="szV2OrPeriod(this)">Hoje</button>
            <button type="button" class="szv2-chip szv2-or-period-btn"                  data-days="6"  onclick="szV2OrPeriod(this)">7 dias</button>
            <button type="button" class="szv2-chip szv2-or-period-btn"                  data-days="29" onclick="szV2OrPeriod(this)">30 dias</button>
            <button type="button" class="szv2-chip szv2-or-period-btn"                  data-days="-1" onclick="szV2OrPeriod(this)">Todos</button>
        </div>

        <!-- Dropdowns + busca -->
        <div style="display:flex;gap:8px;flex-wrap:wrap;align-items:center">
            <select class="szv2-input" id="szv2-or-f-prod" onchange="szV2OrFilter()" style="max-width:180px;padding:5px 8px;font-size:12px">
                <option value="">Todos os produtos</option>
                <?php foreach ( $sz9or_products as $v ) : ?>
                <option value="<?php echo esc_attr( $v ); ?>"><?php echo esc_html( $v ); ?></option>
                <?php endforeach; ?>
            </select>
            <?php if ( ! $sz9or_is_aff && ! empty( $sz9or_affiliates ) ) : ?>
            <select class="szv2-input" id="szv2-or-f-aff" onchange="szV2OrFilter()" style="max-width:160px;padding:5px 8px;font-size:12px">
                <option value="">Todos os afiliados</option>
                <option value="__direct__">Venda direta</option>
                <?php foreach ( $sz9or_affiliates as $v ) : ?>
                <option value="<?php echo esc_attr( $v ); ?>"><?php echo esc_html( $v ); ?></option>
                <?php endforeach; ?>
            </select>
            <?php endif; ?>
            <input type="date" id="szv2-or-f-date-from" class="szv2-input" style="width:130px;padding:5px 8px;font-size:12px" title="Data venda de" onchange="szV2OrFilter()">
            <input type="date" id="szv2-or-f-date-to"   class="szv2-input" style="width:130px;padding:5px 8px;font-size:12px" title="Data venda até" onchange="szV2OrFilter()">
            <input type="search" class="szv2-input" id="szv2-or-search"
                   placeholder="Buscar número, cliente ou produto…" autocomplete="new-password"
                   style="flex:1;min-width:160px;max-width:280px;font-size:12px" oninput="szV2OrFilter()">
            <button type="button" class="szv2-btn szv2-btn-secondary szv2-btn-sm" onclick="szV2OrReset()">Limpar</button>
            <button type="button" class="szv2-btn szv2-btn-secondary szv2-btn-sm" onclick="szV2OrExportCSV()" title="Exportar pedidos visíveis como CSV"
                    style="display:inline-flex;align-items:center;gap:5px">
                <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
                ↓ CSV
            </button>
            <span id="szv2-or-count" style="font-size:12px;color:var(--szv2-text-muted);white-space:nowrap"></span>
        </div>
    </div>

    <!-- kit/oferta hidden select (mantém compatibilidade com filtro JS) -->
    <select id="szv2-or-f-kit" onchange="szV2OrFilter()" style="display:none">
        <option value="">Todos</option>
        <?php foreach ( $sz9or_kits as $v ) : ?>
        <option value="<?php echo esc_attr($v); ?>"><?php echo esc_html($v); ?></option>
        <?php endforeach; ?>
    </select>
    <select id="szv2-or-f-status" onchange="szV2OrFilter()" style="display:none"><option value=""></option></select>
    <!-- F6: data de entrega — filtro estava oculto, JS já filtrava por `fDelFrom` -->
    <div style="display:flex;align-items:center;gap:6px;flex-wrap:wrap;margin-top:4px">
        <span style="font-size:11px;color:var(--szv2-text-muted);font-weight:600;white-space:nowrap">Entrega de:</span>
        <input type="date" id="szv2-or-f-del-from" class="szv2-input" style="width:130px;padding:5px 8px;font-size:12px" title="Data de entrega a partir de" onchange="szV2OrFilter()">
    </div>


    <?php if ( empty( $sz9or_all_orders ) ) : ?>
        <?php // phpcs:ignore WordPress.Security.EscapeOutput
        echo sz_v2_empty_state( [ 'title' => 'Nenhum pedido encontrado', 'text' => 'Os pedidos aparecerão aqui conforme chegarem.' ] ); ?>
    <?php else : ?>

    <div class="szv2-card" style="padding:0;overflow:hidden;margin-top:0">
        <div class="szv2-table-wrap">
            <table class="szv2-table" id="szv2-or-table">
                <thead>
                    <tr>
                        <th>Pedido</th>
                        <th>Cliente</th>
                        <th>Produto</th>
                        <?php if ( ! $sz9or_is_aff ) : ?><th>Afiliado</th><?php endif; ?>
                        <th class="szv2-td-num">Valor</th>
                        <th>Status</th>
                        <th>Data venda</th>
                        <th>Data entrega</th>
                    </tr>
                </thead>
                <tbody>
                    <?php foreach ( $sz9or_all_orders as $sz9or_o ) :
                        $sz9or_id       = (int) ( $sz9or_o['id'] ?? 0 );
                        $sz9or_num      = (string) ( $sz9or_o['number'] ?? $sz9or_id );
                        $sz9or_status   = (string) ( $sz9or_o['status'] ?? '' );
                        $sz9or_client   = (string) ( $sz9or_o['billing']['name'] ?? '—' );
                        $sz9or_product  = (string) ( $sz9or_o['senderzz_offer_name'] ?? $sz9or_o['product_name'] ?? '—' );
                        $sz9or_aff_name = (string) ( $sz9or_o['affiliate_name'] ?? '' );
                        $sz9or_kit      = (string) ( $sz9or_o['senderzz_offer_name'] ?? '' );
                        $sz9or_total    = (string) ( $sz9or_o['total_no_ship'] ?? ( is_callable( 'wc_price' ) ? wc_price( (float) ( $sz9or_o['total_no_ship_raw'] ?? 0 ) ) : '—' ) );
                        $sz9or_date_raw = (string) ( $sz9or_o['date_machine'] ?? $sz9or_o['date'] ?? '' );
                        $sz9or_date_fmt = $sz9or_date_raw ? wp_date( 'd/m/Y', strtotime( $sz9or_date_raw ) ) : '—';
                        $sz9or_date_iso = $sz9or_date_raw ? wp_date( 'Y-m-d', strtotime( $sz9or_date_raw ) ) : '';
                        // Data de entrega — F7: HPOS-safe via WC_Order::get_meta (funciona sob legacy e HPOS)
                        $sz9or_delivery   = '';
                        $_sz9or_obj       = function_exists( 'wc_get_order' ) ? wc_get_order( $sz9or_id ) : null;
                        $sz9or_deliv_meta = ( $_sz9or_obj instanceof \WC_Order )
                            ? (string) $_sz9or_obj->get_meta( '_sz_delivery_date' )
                            : (string) get_post_meta( $sz9or_id, '_sz_delivery_date', true );
                        if ( $sz9or_deliv_meta ) $sz9or_delivery = wp_date( 'd/m/Y', strtotime( $sz9or_deliv_meta ) );
                        if ( ! $sz9or_delivery ) {
                            $sz9or_mb = $wpdb->get_var( $wpdb->prepare(
                                "SELECT reagendado_para FROM {$wpdb->prefix}sz_motoboy_pedidos WHERE wc_order_id=%d LIMIT 1",
                                $sz9or_id
                            ) );
                            if ( $sz9or_mb ) $sz9or_delivery = wp_date( 'd/m/Y', strtotime( $sz9or_mb ) );
                        }
                        $sz9or_status_lbl = $sz9or_status_labels[ $sz9or_status ] ?? ucfirst( $sz9or_status );
                    ?>
                    <tr data-search="<?php echo esc_attr( strtolower( $sz9or_num . ' ' . $sz9or_client . ' ' . $sz9or_product . ' ' . $sz9or_aff_name ) ); ?>"
                        data-status="<?php echo esc_attr( $sz9or_status ); ?>"
                        data-product="<?php echo esc_attr( $sz9or_product ); ?>"
                        data-aff="<?php echo esc_attr( $sz9or_aff_name ); ?>"
                        data-kit="<?php echo esc_attr( $sz9or_kit ); ?>"
                        data-date="<?php echo esc_attr( $sz9or_date_iso ); ?>"
                        data-delivery="<?php echo esc_attr( $sz9or_delivery ? wp_date( 'Y-m-d', strtotime( str_replace( '/', '-', $sz9or_delivery ) ) ) : '' ); ?>">
                        <td class="szv2-td-main" style="font-family:monospace;font-size:12px">
                            <a href="<?php echo esc_url( admin_url( 'post.php?post=' . $sz9or_id . '&action=edit' ) ); ?>"
                               target="_blank" rel="noopener"
                               style="color:var(--szv2-brand);text-decoration:none">#<?php echo esc_html( $sz9or_num ); ?></a>
                        </td>
                        <td class="szv2-td-main"><?php echo esc_html( $sz9or_client ); ?></td>
                        <td class="szv2-td-sub"><?php echo esc_html( $sz9or_product ); ?></td>
                        <?php if ( ! $sz9or_is_aff ) : ?>
                        <td class="szv2-td-sub">
                            <?php if ( $sz9or_aff_name ) : ?>
                            <span style="font-size:11px;padding:2px 6px;background:rgba(234,88,12,.1);color:var(--szv2-brand);border-radius:99px;font-weight:600"><?php echo esc_html( $sz9or_aff_name ); ?></span>
                            <?php else : ?>
                            <span style="color:var(--szv2-text-faint);font-size:12px">—</span>
                            <?php endif; ?>
                        </td>
                        <?php endif; ?>
                        <td class="szv2-td-num szv2-num"><?php echo wp_kses_post( $sz9or_total ); ?></td>
                        <td><?php // phpcs:ignore WordPress.Security.EscapeOutput
                            echo sz_v2_status_badge( $sz9or_status ); ?></td>
                        <td class="szv2-td-sub" style="white-space:nowrap"><?php echo esc_html( $sz9or_date_fmt ); ?></td>
                        <td class="szv2-td-sub" style="white-space:nowrap;color:<?php echo $sz9or_delivery ? 'var(--szv2-brand)' : 'var(--szv2-text-faint)'; ?>;font-weight:<?php echo $sz9or_delivery ? '600' : '400'; ?>">
                            <?php echo esc_html( $sz9or_delivery ?: '—' ); ?>
                        </td>
                    </tr>
                    <?php endforeach; ?>
                </tbody>
            </table>
        </div>
        <div style="padding:8px 16px;border-top:1px solid var(--szv2-divider);font-size:12px;color:var(--szv2-text-muted)">
            <span id="szv2-or-foot-count"><?php echo esc_html( (string) count( $sz9or_all_orders ) ); ?> pedido(s)</span>
        </div>
    </div>

    <?php endif; ?>

</section>
<script>
(function(){
    var _szOrActiveStatus = '';
    function szV2OrStatusChip(btn) {
        _szOrActiveStatus = btn.getAttribute('data-or-status') || '';
        document.querySelectorAll('[data-or-status]').forEach(function(b){
            b.classList.toggle('szv2-chip-active', b === btn);
        });
        szV2OrFilter();
    }
    window.szV2OrStatusChip = szV2OrStatusChip;

    function szV2OrFilter() {
        var fProd    = (document.getElementById('szv2-or-f-prod')    ||{}).value || '';
        var fStatus  = _szOrActiveStatus;
        var fAff     = (document.getElementById('szv2-or-f-aff')     ||{}).value || '';
        var fKit     = (document.getElementById('szv2-or-f-kit')     ||{}).value || '';
        var fFrom    = (document.getElementById('szv2-or-f-date-from')||{}).value || '';
        var fTo      = (document.getElementById('szv2-or-f-date-to') ||{}).value || '';
        var fDelFrom = (document.getElementById('szv2-or-f-del-from')||{}).value || '';
        var fSearch  = ((document.getElementById('szv2-or-search')||{}).value||'').toLowerCase();
        var rows = document.querySelectorAll('#szv2-or-table tbody tr');
        var vis = 0;
        rows.forEach(function(r){
            var d = r.dataset;
            var ok = true;
            if (fProd   && d.product !== fProd)  ok = false;
            if (fStatus && d.status  !== fStatus) ok = false;
            if (fKit    && d.kit     !== fKit)    ok = false;
            if (fAff) {
                if (fAff === '__direct__') { if (d.aff !== '') ok = false; }
                else { if (d.aff !== fAff) ok = false; }
            }
            var dt = (d.date||'').slice(0,10);
            if (fFrom    && dt && dt < fFrom)    ok = false;
            if (fTo      && dt && dt > fTo)      ok = false;
            var del = (d.delivery||'').slice(0,10);
            if (fDelFrom && del && del < fDelFrom) ok = false;
            if (fSearch  && d.search && d.search.indexOf(fSearch) === -1) ok = false;
            r.style.display = ok ? '' : 'none';
            if (ok) vis++;
        });
        var cnt = document.getElementById('szv2-or-count');
        var ft  = document.getElementById('szv2-or-foot-count');
        var txt = vis + ' pedido(s)';
        if (cnt) cnt.textContent = txt;
        if (ft)  ft.textContent  = txt;
    }
    function szV2OrPeriod(btn) {
        var days = parseInt(btn.getAttribute('data-days'), 10);
        document.querySelectorAll('.szv2-or-period-btn').forEach(function(b){
            b.classList.toggle('szv2-chip-active', b === btn);
        });
        var fromEl = document.getElementById('szv2-or-f-date-from');
        var toEl   = document.getElementById('szv2-or-f-date-to');
        if (!fromEl || !toEl) return;
        if (days < 0) { fromEl.value = ''; toEl.value = ''; }
        else {
            var today = new Date();
            var from  = new Date(today);
            from.setDate(today.getDate() - days);
            fromEl.value = from.toISOString().slice(0,10);
            toEl.value   = today.toISOString().slice(0,10);
        }
        szV2OrFilter();
    }
    function szV2OrReset() {
        ['szv2-or-f-prod','szv2-or-f-aff','szv2-or-f-kit',
         'szv2-or-f-date-from','szv2-or-f-date-to','szv2-or-f-del-from','szv2-or-search'].forEach(function(id){
            var el = document.getElementById(id); if (el) el.value = '';
        });
        _szOrActiveStatus = '';
        document.querySelectorAll('[data-or-status]').forEach(function(b){
            b.classList.toggle('szv2-chip-active', b.getAttribute('data-or-status') === '');
        });
        szV2OrFilter();
    }
    window.szV2OrFilter = szV2OrFilter;
    window.szV2OrPeriod = szV2OrPeriod;
    window.szV2OrReset  = szV2OrReset;

    function szV2OrExportCSV() {
        var rows = document.querySelectorAll('#szv2-or-table tbody tr');
        var hasAff = !!document.querySelector('#szv2-or-table thead th:nth-child(4)[class=""]') ||
                     (document.querySelector('#szv2-or-table thead tr').children.length > 7);
        var lines = [];
        // Header
        var hdr = ['Pedido','Cliente','Produto','Status','Valor','Data','Afiliado'];
        lines.push(hdr.join(';'));
        rows.forEach(function(r) {
            if (r.style.display === 'none') return;
            var cells = r.querySelectorAll('td');
            var idx = 0;
            var num    = (cells[idx++]?.textContent||'').trim().replace(/^#/,'');
            var client = (cells[idx++]?.textContent||'').trim();
            var prod   = (cells[idx++]?.textContent||'').trim();
            var aff    = '';
            // detect if affiliate column exists (table has 8 cols)
            if (cells.length >= 8) { aff = (cells[idx++]?.textContent||'').trim(); }
            var valor  = (cells[idx++]?.textContent||'').trim();
            var status = r.dataset.status || '';
            var data   = (cells[idx+1]?.textContent||'').trim();
            var esc = function(v){ return '"'+v.replace(/"/g,'""')+'"'; };
            lines.push([esc(num),esc(client),esc(prod),esc(status),esc(valor),esc(data),esc(aff)].join(';'));
        });
        var csv = '﻿' + lines.join('\r\n');
        var blob = new Blob([csv], {type:'text/csv;charset=utf-8;'});
        var url = URL.createObjectURL(blob);
        var a = document.createElement('a');
        a.href = url; a.download = 'pedidos.csv';
        document.body.appendChild(a); a.click();
        document.body.removeChild(a); URL.revokeObjectURL(url);
    }
    window.szV2OrExportCSV = szV2OrExportCSV;
    document.addEventListener('DOMContentLoaded', function(){
        // Inicia com "Hoje"
        var todayBtn = document.querySelector('.szv2-or-period-btn[data-days="0"]');
        if (todayBtn) szV2OrPeriod(todayBtn); else szV2OrFilter();
    });
})();
</script>
