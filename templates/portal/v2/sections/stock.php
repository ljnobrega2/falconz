<?php
/**
 * Senderzz Dashboard V2 — Estoque
 * ─────────────────────────────────────────────────────────────────────────────
 * Portado de templates/portal/stock-panel.php (V1, ~690 linhas).
 *
 * Features portadas (read-only):
 *  - KPIs: Disponíveis / Reservados / Em rota / Entregues / Frustrados
 *  - Grade de produtos com estoque gerenciado + badges de custódia por produto
 *    (usa tabela sz_motoboy_stock_custody quando disponível, senão fallback
 *     via Portal_Orders + kit_map).
 *  - Movimentações (senderzz_stock_get_movement_rows): filtro por produto e
 *    exportação CSV (JS auto-contido copiado do V1, sem deps de portal.js).
 *
 * Features postergadas / fora do escopo desta porta:
 *  - Formulário de envio de reposição (szShipSave / szShipAddItem) — depende
 *    de handlers V1 (portal.js). Exibido como placeholder "Em breve".
 *  - Histórico de envios (#sz-ship-history-body) — também usa handler V1.
 *    Substituído por empty state com texto orientador.
 *
 * Regras de inclusão:
 *  - Pode ser incluído diretamente (sem $this); usa apenas $sz_v2_user.
 *  - Fallback defensivo se funções WC / módulos Senderzz não estiverem
 *    disponíveis: exibe empty state em vez de erro fatal.
 *
 * @var object $sz_v2_user   Usuário do portal (Portal_Auth::get_current_user()).
 */
defined( 'ABSPATH' ) || exit;

global $wpdb;

/* ─── Dados do usuário ───────────────────────────────────────────────────── */
$sz9st_wp_uid  = (int) ( $sz_v2_user->wp_user_id ?? 0 );
$sz9st_class_id = (int) ( $sz_v2_user->shipping_class_id ?? 0 );
// O V1 aceitava array de class_ids para produtores multi-classe.
// Na V2, afiliados não chegam aqui (não constam em $sz_v2_sections_producer['stock']).
// Usamos um array de 1 elemento; quando multi-classe entrar na V2, basta expandir aqui.
$sz9st_class_ids = $sz9st_class_id > 0 ? [ $sz9st_class_id ] : [];

/* ─── Autoload defensivo de Portal_Orders (igual ao V1 lines 14-19) ─────── */
if ( ! class_exists( '\WC_MelhorEnvio\Portal\Portal_Orders' ) ) {
    $sz9st_po_file = dirname( __DIR__, 4 ) . '/src/Portal/Portal_Orders.php';
    if ( file_exists( $sz9st_po_file ) ) {
        require_once $sz9st_po_file;
    }
}

/* ─── Guard: funções essenciais não disponíveis ──────────────────────────── */
$sz9st_unavailable = ! function_exists( 'wc_get_products' ) || empty( $sz9st_class_ids );
?>
<section id="sec-stock" class="sz-sec" data-szv2-label="Estoque">
<?php if ( $sz9st_unavailable ) : ?>

    <div class="szv2-card">
        <?php
        // phpcs:ignore WordPress.Security.EscapeOutput
        echo sz_v2_empty_state( [
            'title' => $sz9st_class_id <= 0
                ? 'Estoque indisponível'
                : 'Módulo de estoque não configurado',
            'text'  => $sz9st_class_id <= 0
                ? 'Sua conta ainda não possui uma classe de entrega associada. Entre em contato com o suporte.'
                : 'O WooCommerce não está disponível no momento. Tente novamente em instantes.',
        ] );
        ?>
    </div>

<?php else : /* ──────────────── BLOCO PRINCIPAL ─────────────────────────── */ ?>

<?php
/* ─── Carga de produtos com estoque gerenciado ───────────────────────────── */
$sz9st_products_raw = wc_get_products( [
    'limit'   => -1,
    'status'  => 'publish',
    'type'    => [ 'simple', 'variation', 'variable' ],
    'orderby' => 'title',
    'order'   => 'ASC',
    'return'  => 'objects',
] );

// Palavras-chave de exclusão (mesmas do V1 lines 56-63)
$sz9st_items = [];
foreach ( (array) $sz9st_products_raw as $sz9st_p ) {
    if ( ! $sz9st_p instanceof \WC_Product ) continue;

    // Filtro por classe de entrega do produtor
    if ( ! in_array( (int) $sz9st_p->get_shipping_class_id(), $sz9st_class_ids, true ) ) continue;

    // Somente produtos com estoque gerenciado e quantia definida
    if ( ! $sz9st_p->managing_stock() ) continue;
    $sz9st_qty = $sz9st_p->get_stock_quantity();
    if ( $sz9st_qty === null ) continue;

    $sz9st_name = $sz9st_p->get_name();
    $sz9st_type = $sz9st_p->get_type();
    $sz9st_sku  = (string) $sz9st_p->get_sku();
    $sz9st_hay  = strtolower( remove_accents( $sz9st_name . ' ' . $sz9st_sku . ' ' . $sz9st_type ) );

    // Excluir kits, bundles, recargas etc. (V1 lines 56-63)
    if (
        in_array( $sz9st_type, [ 'grouped', 'bundle', 'composite', 'woosb', 'mix-and-match' ], true ) ||
        preg_match( '/\b(kit|kits|combo|combos|pacote|pack|bundle|conjunto|recarga|carteira|frete|teste)\b/i', $sz9st_hay ) ||
        preg_match( '/\b\d+\s*(frasco|frascos|pomada|pomadas|gota|gotas)\b/i', $sz9st_hay ) ||
        preg_match( '/\+/', $sz9st_hay )
    ) {
        continue;
    }

    $sz9st_stock_status = $sz9st_p->get_stock_status();
    $sz9st_status_class = $sz9st_stock_status === 'instock'
        ? 'success'
        : ( $sz9st_stock_status === 'onbackorder' ? 'warning' : 'danger' );
    $sz9st_status_label = $sz9st_stock_status === 'instock'
        ? 'Disponível'
        : ( $sz9st_stock_status === 'onbackorder' ? 'Sob encomenda' : 'Sem estoque' );

    $sz9st_items[] = [
        'id'           => $sz9st_p->get_id(),
        'name'         => $sz9st_name,
        'sku'          => $sz9st_sku,
        'qty'          => (int) $sz9st_qty,
        'status_label' => $sz9st_status_label,
        'status_class' => $sz9st_status_class,
        'image'        => wp_get_attachment_image_url( $sz9st_p->get_image_id(), 'thumbnail' ),
    ];
}

$sz9st_total_items = count( $sz9st_items );
$sz9st_available   = array_sum( array_map( fn( $i ) => max( 0, (int) $i['qty'] ), $sz9st_items ) );

/* ─── Unidades por status de custódia (V1 lines 87-181) ─────────────────── */
$sz9st_reserved_units       = 0;
$sz9st_route_units          = 0;
$sz9st_delivered_units      = 0;
$sz9st_frustrated_units     = 0;
$sz9st_reserved_per_product  = [];
$sz9st_route_per_product     = [];
$sz9st_delivered_per_product = [];
$sz9st_frustrated_per_product= [];

// Tenta primeiro a tabela de custódia física (preferida desde v372)
$sz9st_custody_table = $wpdb->prefix . 'sz_motoboy_stock_custody';
$sz9st_item_ids = array_values( array_unique( array_filter(
    array_map( 'intval', array_column( $sz9st_items, 'id' ) )
) ) );

$sz9st_custody_loaded = false;
if (
    ! empty( $sz9st_item_ids ) &&
    $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $sz9st_custody_table ) ) === $sz9st_custody_table
) {
    $sz9st_placeholders = implode( ',', array_fill( 0, count( $sz9st_item_ids ), '%d' ) );
    $sz9st_custody_rows = $wpdb->get_results( $wpdb->prepare(
        "SELECT product_id, variation_id, physical_status, COALESCE(SUM(quantity),0) AS qty
           FROM {$sz9st_custody_table}
          WHERE ( product_id IN ({$sz9st_placeholders}) OR variation_id IN ({$sz9st_placeholders}) )
            AND physical_status IN ('reserved','with_motoboy','frustrated','return_declared','delivered')
          GROUP BY product_id, variation_id, physical_status",
        array_merge( $sz9st_item_ids, $sz9st_item_ids )
    ) );

    if ( ! empty( $sz9st_custody_rows ) ) {
        $sz9st_custody_loaded = true;
        foreach ( $sz9st_custody_rows as $sz9st_cr ) {
            $sz9st_tid = (int) ( $sz9st_cr->variation_id ?: $sz9st_cr->product_id );
            $sz9st_q   = max( 0, (int) $sz9st_cr->qty );
            if ( $sz9st_q <= 0 || $sz9st_tid <= 0 ) continue;
            $sz9st_st  = (string) $sz9st_cr->physical_status;
            if ( $sz9st_st === 'reserved' ) {
                $sz9st_reserved_per_product[ $sz9st_tid ]  = ( $sz9st_reserved_per_product[ $sz9st_tid ]  ?? 0 ) + $sz9st_q;
                $sz9st_reserved_units += $sz9st_q;
            } elseif ( $sz9st_st === 'with_motoboy' ) {
                $sz9st_route_per_product[ $sz9st_tid ]     = ( $sz9st_route_per_product[ $sz9st_tid ]     ?? 0 ) + $sz9st_q;
                $sz9st_route_units += $sz9st_q;
            } elseif ( $sz9st_st === 'delivered' ) {
                $sz9st_delivered_per_product[ $sz9st_tid ] = ( $sz9st_delivered_per_product[ $sz9st_tid ] ?? 0 ) + $sz9st_q;
                $sz9st_delivered_units += $sz9st_q;
            } elseif ( in_array( $sz9st_st, [ 'frustrated', 'return_declared' ], true ) ) {
                $sz9st_frustrated_per_product[ $sz9st_tid ]= ( $sz9st_frustrated_per_product[ $sz9st_tid ]?? 0 ) + $sz9st_q;
                $sz9st_frustrated_units += $sz9st_q;
            }
        }
    }
}

// Fallback: Portal_Orders + kit_map (V1 lines 87-150)
if ( ! $sz9st_custody_loaded && class_exists( '\WC_MelhorEnvio\Portal\Portal_Orders' ) ) {
    $sz9st_stock_orders = \WC_MelhorEnvio\Portal\Portal_Orders::get_orders( $sz9st_class_ids, 1, 500 );

    $sz9st_reserved_statuses   = [ 'separado', 'erro', 'agendado', 'embalado', 'on-hold', 'processing', 'pending' ];
    $sz9st_route_statuses      = [ 'em_rota', 'em-rota', 'a_caminho', 'acaminho' ];
    $sz9st_delivered_statuses  = [ 'entregue', 'completed', 'completo' ];
    $sz9st_frustrated_statuses = [ 'frustrado', 'frustracao' ];

    $sz9st_kit_map = defined( 'SENDERZZ_KIT_STOCK_OPTION' )
        ? (array) get_option( SENDERZZ_KIT_STOCK_OPTION, [] ) : [];

    foreach ( $sz9st_stock_orders as $sz9st_o ) {
        $sz9st_ost = (string) ( $sz9st_o['status'] ?? '' );
        if ( in_array( $sz9st_ost, $sz9st_reserved_statuses, true ) ) {
            $sz9st_bucket = 'reserved';
        } elseif ( in_array( $sz9st_ost, $sz9st_route_statuses, true ) ) {
            $sz9st_bucket = 'route';
        } elseif ( in_array( $sz9st_ost, $sz9st_delivered_statuses, true ) ) {
            $sz9st_bucket = 'delivered';
        } elseif ( in_array( $sz9st_ost, $sz9st_frustrated_statuses, true ) ) {
            $sz9st_bucket = 'frustrated';
        } else {
            continue;
        }

        $sz9st_oid = (int) ( $sz9st_o['id'] ?? 0 );
        if ( $sz9st_oid <= 0 ) continue;
        $sz9st_wco = wc_get_order( $sz9st_oid );
        if ( ! $sz9st_wco ) continue;

        foreach ( $sz9st_wco->get_items() as $sz9st_line ) {
            $sz9st_pid = (int) $sz9st_line->get_product_id();
            $sz9st_lq  = (int) $sz9st_line->get_quantity();
            if ( $sz9st_lq <= 0 || $sz9st_pid <= 0 ) continue;

            $sz9st_targets = [];
            if ( isset( $sz9st_kit_map[ $sz9st_pid ] ) ) {
                foreach ( (array) ( $sz9st_kit_map[ $sz9st_pid ]['items'] ?? [] ) as $sz9st_comp ) {
                    $sz9st_cpid = (int) ( $sz9st_comp['product_id'] ?? 0 );
                    $sz9st_cqty = (int) ( $sz9st_comp['qty'] ?? 1 );
                    if ( $sz9st_cpid <= 0 ) continue;
                    $sz9st_targets[ $sz9st_cpid ] = ( $sz9st_targets[ $sz9st_cpid ] ?? 0 ) + ( $sz9st_lq * max( 1, $sz9st_cqty ) );
                }
            } else {
                $sz9st_targets[ $sz9st_pid ] = $sz9st_lq;
            }

            foreach ( $sz9st_targets as $sz9st_tid => $sz9st_total ) {
                if ( $sz9st_bucket === 'reserved' ) {
                    $sz9st_reserved_per_product[ $sz9st_tid ]  = ( $sz9st_reserved_per_product[ $sz9st_tid ]  ?? 0 ) + $sz9st_total;
                    $sz9st_reserved_units += $sz9st_total;
                } elseif ( $sz9st_bucket === 'route' ) {
                    $sz9st_route_per_product[ $sz9st_tid ]     = ( $sz9st_route_per_product[ $sz9st_tid ]     ?? 0 ) + $sz9st_total;
                    $sz9st_route_units += $sz9st_total;
                } elseif ( $sz9st_bucket === 'delivered' ) {
                    $sz9st_delivered_per_product[ $sz9st_tid ] = ( $sz9st_delivered_per_product[ $sz9st_tid ] ?? 0 ) + $sz9st_total;
                    $sz9st_delivered_units += $sz9st_total;
                } elseif ( $sz9st_bucket === 'frustrated' ) {
                    $sz9st_frustrated_per_product[ $sz9st_tid ]= ( $sz9st_frustrated_per_product[ $sz9st_tid ]?? 0 ) + $sz9st_total;
                    $sz9st_frustrated_units += $sz9st_total;
                }
            }
        }
    }
}

/* ─── Movimentações (senderzz_stock_get_movement_rows) ───────────────────── */
$sz9st_movement_rows     = [];
$sz9st_movement_products = [];

if ( function_exists( 'senderzz_stock_get_movement_rows' ) ) {
    foreach ( $sz9st_class_ids as $sz9st_mov_cid ) {
        $sz9st_movement_rows = array_merge(
            $sz9st_movement_rows,
            senderzz_stock_get_movement_rows( (int) $sz9st_mov_cid, 120 )
        );
    }
    usort( $sz9st_movement_rows, fn( $a, $b ) => strcmp(
        (string) ( $b['date'] ?? '' ),
        (string) ( $a['date'] ?? '' )
    ) );
    $sz9st_movement_rows = array_slice( $sz9st_movement_rows, 0, 120 );

    foreach ( $sz9st_movement_rows as $sz9st_mv ) {
        $sz9st_mvp = trim( (string) ( $sz9st_mv['product_name'] ?? '' ) );
        if ( $sz9st_mvp !== '' ) {
            $sz9st_movement_products[ $sz9st_mvp ] = true;
        }
    }
    $sz9st_movement_products = array_keys( $sz9st_movement_products );
    sort( $sz9st_movement_products, SORT_NATURAL | SORT_FLAG_CASE );
}
?>

<!-- ══════════════════════════════════════════════════════════════════════════
     KPI cards — 5 métricas de custódia física
     ══════════════════════════════════════════════════════════════════════════ -->
<div class="szv2-kpi-grid" style="margin-bottom:var(--szv2-space-4)">
    <?php
    // phpcs:disable WordPress.Security.EscapeOutput
    echo sz_v2_kpi_card( [
        'label'       => 'Disponíveis',
        'value'       => (string) $sz9st_available,
        'meta'        => $sz9st_total_items . ' produto(s) com estoque',
        'value_class' => 'szv2-num',
        'data_kpi'    => 'stock-available',
    ] );
    echo sz_v2_kpi_card( [
        'label'       => 'Reservados',
        'value'       => (string) $sz9st_reserved_units,
        'meta'        => 'Aguardando envio',
        'value_class' => 'szv2-num',
        'data_kpi'    => 'stock-reserved',
    ] );
    echo sz_v2_kpi_card( [
        'label'       => 'Em rota',
        'value'       => (string) $sz9st_route_units,
        'meta'        => 'Com motoboy',
        'value_class' => 'szv2-num',
        'data_kpi'    => 'stock-route',
    ] );
    echo sz_v2_kpi_card( [
        'label'       => 'Entregues',
        'value'       => (string) $sz9st_delivered_units,
        'meta'        => 'Confirmados',
        'value_class' => 'szv2-num',
        'data_kpi'    => 'stock-delivered',
    ] );
    echo sz_v2_kpi_card( [
        'label'       => 'Frustrados',
        'value'       => (string) $sz9st_frustrated_units,
        'meta'        => 'Retorno/frustração',
        'value_class' => 'szv2-num',
        'data_kpi'    => 'stock-frustrated',
    ] );
    // phpcs:enable
    ?>
</div>

<?php if ( empty( $sz9st_items ) ) : ?>
<!-- Nenhum produto ── -->
    <?php // phpcs:ignore WordPress.Security.EscapeOutput
    echo sz_v2_empty_state( [
        'title' => 'Nenhum produto com estoque disponível',
        'text'  => 'Os produtos com estoque gerenciado para a sua classe de entrega aparecerão aqui.',
    ] ); ?>

<?php else : /* ── Grade de produtos ─────────────────────────────────────── */ ?>

<!-- ══════════════════════════════════════════════════════════════════════════
     Lista de produtos com estoque
     ══════════════════════════════════════════════════════════════════════════ -->
<div class="szv2-card" style="margin-bottom:var(--szv2-space-4)">
    <div class="szv2-card-head">
        <div>
            <h2>Produtos em estoque</h2>
            <p class="szv2-card-sub"><?php echo esc_html( $sz9st_total_items ); ?> produto(s) com controle de estoque ativo</p>
        </div>
        <input type="search"
               id="sz9st-search"
               class="szv2-input"
               placeholder="Buscar produto…"
               autocomplete="new-password"
               style="width:220px;font-size:12px"
               oninput="sz9stFilter()">
    </div>

    <div class="szv2-table-wrap">
        <table class="szv2-table" id="sz9st-table">
            <thead>
                <tr>
                    <th style="width:48px"></th>
                    <th>Produto</th>
                    <th>SKU</th>
                    <th class="szv2-td-num">Disponível</th>
                    <th class="szv2-td-num">Reservado</th>
                    <th class="szv2-td-num">Em rota</th>
                    <th class="szv2-td-num">Frustrado</th>
                    <th class="szv2-td-num">Total</th>
                    <th>Status</th>
                </tr>
            </thead>
            <tbody>
                <?php foreach ( $sz9st_items as $sz9st_item ) :
                    $sz9st_iid   = (int) $sz9st_item['id'];
                    $sz9st_avail = max( 0, (int) $sz9st_item['qty'] );
                    $sz9st_rsv   = (int) ( $sz9st_reserved_per_product[ $sz9st_iid ]   ?? 0 );
                    $sz9st_rt    = (int) ( $sz9st_route_per_product[ $sz9st_iid ]      ?? 0 );
                    $sz9st_fru   = (int) ( $sz9st_frustrated_per_product[ $sz9st_iid ] ?? 0 );
                    $sz9st_total = $sz9st_avail + $sz9st_rsv + $sz9st_rt + $sz9st_fru;
                    $sz9st_q_key = strtolower( remove_accents( $sz9st_item['name'] . ' ' . $sz9st_item['sku'] ) );
                ?>
                <tr data-search="<?php echo esc_attr( $sz9st_q_key ); ?>">
                    <td style="padding:8px">
                        <?php if ( $sz9st_item['image'] ) : ?>
                        <img src="<?php echo esc_url( $sz9st_item['image'] ); ?>"
                             alt=""
                             style="width:38px;height:38px;object-fit:cover;border-radius:8px;border:1px solid var(--szv2-border)">
                        <?php else : ?>
                        <span style="display:inline-flex;align-items:center;justify-content:center;width:38px;height:38px;border-radius:8px;background:var(--szv2-surface-alt);font-size:18px">&#128230;</span>
                        <?php endif; ?>
                    </td>
                    <td class="szv2-td-main"><?php echo esc_html( $sz9st_item['name'] ); ?></td>
                    <td class="szv2-td-sub" style="font-family:monospace;font-size:11px"><?php echo esc_html( $sz9st_item['sku'] ?: '—' ); ?></td>
                    <td class="szv2-td-num szv2-num" style="color:<?php echo $sz9st_avail > 0 ? 'var(--szv2-success)' : 'var(--szv2-danger)'; ?>;font-weight:700"><?php echo esc_html( (string) $sz9st_avail ); ?></td>
                    <td class="szv2-td-num szv2-td-sub"><?php echo esc_html( $sz9st_rsv > 0 ? (string) $sz9st_rsv : '—' ); ?></td>
                    <td class="szv2-td-num szv2-td-sub"><?php echo esc_html( $sz9st_rt  > 0 ? (string) $sz9st_rt  : '—' ); ?></td>
                    <td class="szv2-td-num szv2-td-sub" style="<?php echo $sz9st_fru > 0 ? 'color:var(--szv2-danger);font-weight:600' : ''; ?>"><?php echo esc_html( $sz9st_fru > 0 ? (string) $sz9st_fru : '—' ); ?></td>
                    <td class="szv2-td-num szv2-num"><?php echo esc_html( (string) $sz9st_total ); ?></td>
                    <td>
                        <span class="sz-badge szv2-badge-<?php echo esc_attr( $sz9st_item['status_class'] ); ?>">
                            <?php echo esc_html( $sz9st_item['status_label'] ); ?>
                        </span>
                    </td>
                </tr>
                <?php endforeach; ?>
            </tbody>
        </table>
    </div>
    <div style="padding:8px 16px;border-top:1px solid var(--szv2-divider);font-size:12px;color:var(--szv2-text-muted)">
        <span id="sz9st-count"><?php echo esc_html( (string) $sz9st_total_items ); ?> produto(s)</span>
    </div>
</div>

<?php endif; /* fim grade de produtos */ ?>

<!-- ══════════════════════════════════════════════════════════════════════════
     Movimentações de estoque
     ══════════════════════════════════════════════════════════════════════════ -->
<div class="szv2-card" style="margin-bottom:var(--szv2-space-4)">
    <div class="szv2-card-head">
        <div>
            <h2>Movimentações</h2>
            <p class="szv2-card-sub">Entradas, saídas e saldo por produto</p>
        </div>
        <div style="display:flex;gap:8px;align-items:center;flex-wrap:wrap">
            <?php if ( ! empty( $sz9st_movement_products ) ) : ?>
            <select id="sz9st-mov-filter"
                    class="szv2-input"
                    style="max-width:220px;font-size:12px"
                    autocomplete="off"
                    onchange="sz9stMovFilter()">
                <option value="">Todos os produtos</option>
                <?php foreach ( $sz9st_movement_products as $sz9st_mpn ) : ?>
                <option value="<?php echo esc_attr( strtolower( remove_accents( $sz9st_mpn ) ) ); ?>">
                    <?php echo esc_html( $sz9st_mpn ); ?>
                </option>
                <?php endforeach; ?>
            </select>
            <?php endif; ?>
            <button type="button"
                    class="szv2-btn szv2-btn-brand szv2-btn-sm"
                    onclick="sz9stMovExportCSV()">↓ Exportar CSV</button>
        </div>
    </div>

    <?php if ( empty( $sz9st_movement_rows ) ) : ?>
        <?php // phpcs:ignore WordPress.Security.EscapeOutput
        echo sz_v2_empty_state( [
            'title' => 'Nenhuma movimentação registrada',
            'text'  => 'Entradas e saídas de estoque aparecerão aqui conforme os pedidos forem processados.',
        ] ); ?>
    <?php else : ?>
    <div class="szv2-table-wrap" style="max-height:520px;overflow:auto" id="sz9st-mov-scroll">
        <table class="szv2-table" id="sz9st-mov-table">
            <thead>
                <tr>
                    <th>Data</th>
                    <th>Tipo</th>
                    <th>Referência</th>
                    <th>Produto</th>
                    <th class="szv2-td-num">Qtd</th>
                    <th class="szv2-td-num">Antes</th>
                    <th class="szv2-td-num">Depois</th>
                    <th>Status</th>
                </tr>
            </thead>
            <tbody>
                <?php foreach ( $sz9st_movement_rows as $sz9st_mv ) :
                    $sz9st_mv_product_name = (string) ( $sz9st_mv['product_name'] ?? '—' );
                    $sz9st_mv_product_key  = strtolower( remove_accents( $sz9st_mv_product_name ) );
                    $sz9st_mv_qty          = (int) ( $sz9st_mv['qty'] ?? 0 );
                    $sz9st_mv_qty_display  = ( $sz9st_mv_qty > 0 ? '+' : '' ) . (string) $sz9st_mv_qty;
                    $sz9st_mv_before       = array_key_exists( 'stock_before', $sz9st_mv ) && $sz9st_mv['stock_before'] !== null
                        ? (string) (int) $sz9st_mv['stock_before'] : '—';
                    $sz9st_mv_after        = array_key_exists( 'stock_after', $sz9st_mv ) && $sz9st_mv['stock_after'] !== null
                        ? (string) (int) $sz9st_mv['stock_after'] : '—';
                    $sz9st_mv_dir          = (string) ( $sz9st_mv['direction'] ?? 'out' );
                    // Badge de tipo: in = verde, out = vermelho (usando inline style com tokens V2)
                    $sz9st_mv_dir_color = $sz9st_mv_dir === 'in'
                        ? 'color:var(--szv2-success);background:var(--szv2-success-bg)'
                        : 'color:var(--szv2-danger);background:var(--szv2-danger-bg)';
                ?>
                <tr data-product="<?php echo esc_attr( $sz9st_mv_product_key ); ?>"
                    data-csv-date="<?php echo esc_attr( $sz9st_mv['date_fmt']       ?? '—' ); ?>"
                    data-csv-type="<?php echo esc_attr( $sz9st_mv['direction_label'] ?? 'Mov.' ); ?>"
                    data-csv-ref="<?php echo esc_attr( $sz9st_mv['source_label']    ?? '—' ); ?>"
                    data-csv-meta="<?php echo esc_attr( $sz9st_mv['source_meta']    ?? '' ); ?>"
                    data-csv-product="<?php echo esc_attr( $sz9st_mv_product_name ); ?>"
                    data-csv-qty="<?php echo esc_attr( $sz9st_mv_qty_display ); ?>"
                    data-csv-before="<?php echo esc_attr( $sz9st_mv_before ); ?>"
                    data-csv-after="<?php echo esc_attr( $sz9st_mv_after ); ?>"
                    data-csv-status="<?php echo esc_attr( $sz9st_mv['status'] ?? '—' ); ?>">
                    <td class="szv2-td-sub" style="white-space:nowrap"><?php echo esc_html( $sz9st_mv['date_fmt'] ?? '—' ); ?></td>
                    <td>
                        <span style="display:inline-flex;align-items:center;padding:3px 9px;border-radius:99px;font-size:11px;font-weight:700;<?php echo $sz9st_mv_dir_color; ?>">
                            <?php echo esc_html( $sz9st_mv['direction_label'] ?? 'Mov.' ); ?>
                        </span>
                    </td>
                    <td class="szv2-td-main" style="font-size:12px">
                        <?php echo esc_html( $sz9st_mv['source_label'] ?? '—' ); ?>
                        <?php if ( ! empty( $sz9st_mv['source_meta'] ) ) : ?>
                        <small style="display:block;color:var(--szv2-text-muted);font-size:11px"><?php echo esc_html( $sz9st_mv['source_meta'] ); ?></small>
                        <?php endif; ?>
                    </td>
                    <td class="szv2-td-sub"><?php echo esc_html( $sz9st_mv_product_name ); ?></td>
                    <td class="szv2-td-num" style="font-weight:700;color:<?php echo $sz9st_mv_dir === 'in' ? 'var(--szv2-success)' : 'var(--szv2-danger)'; ?>"><?php echo esc_html( $sz9st_mv_qty_display ); ?></td>
                    <td class="szv2-td-num szv2-td-sub"><?php echo esc_html( $sz9st_mv_before ); ?></td>
                    <td class="szv2-td-num szv2-td-sub"><?php echo esc_html( $sz9st_mv_after ); ?></td>
                    <td>
                        <span style="display:inline-flex;align-items:center;padding:3px 9px;border-radius:99px;font-size:11px;font-weight:700;background:var(--szv2-surface-alt);color:var(--szv2-text)">
                            <?php echo esc_html( $sz9st_mv['status'] ?? '—' ); ?>
                        </span>
                    </td>
                </tr>
                <?php endforeach; ?>
            </tbody>
        </table>
    </div>
    <?php endif; ?>
</div>

<!-- ══════════════════════════════════════════════════════════════════════════
     Formulário de envio de reposição — postergado para fase futura
     (depende de handlers portal.js V1 não presentes na V2)
     ══════════════════════════════════════════════════════════════════════════ -->
<div class="szv2-card">
    <div class="szv2-card-head">
        <div>
            <h2>Envio de reposição</h2>
            <p class="szv2-card-sub">Registre remessas de estoque enviadas ao armazém</p>
        </div>
        <span class="sz-badge szv2-badge-neutral" style="font-size:11px">Em breve</span>
    </div>
    <?php // phpcs:ignore WordPress.Security.EscapeOutput
    echo sz_v2_empty_state( [
        'title' => 'Formulário em migração',
        'text'  => 'O formulário de envio de reposição e o histórico de envios estarão disponíveis aqui em breve. Para utilizar agora, acesse o painel clássico.',
    ] ); ?>
</div>

<?php endif; /* fim bloco principal */ ?>

</section>

<script>
(function(){
    /* ── Filtro de busca da tabela de produtos ─────────────────────────── */
    window.sz9stFilter = function() {
        var q = ((document.getElementById('sz9st-search') || {}).value || '').toLowerCase();
        var rows = document.querySelectorAll('#sz9st-table tbody tr');
        var vis = 0;
        rows.forEach(function(r) {
            var ok = !q || (r.dataset.search || '').indexOf(q) !== -1;
            r.style.display = ok ? '' : 'none';
            if (ok) vis++;
        });
        var cnt = document.getElementById('sz9st-count');
        if (cnt) cnt.textContent = vis + ' produto(s)';
    };

    /* ── Filtro de produto no histórico de movimentações ──────────────── */
    window.sz9stMovFilter = function() {
        var sel = document.getElementById('sz9st-mov-filter');
        var key = sel ? String(sel.value || '') : '';
        var rows = document.querySelectorAll('#sz9st-mov-scroll tr[data-product]');
        rows.forEach(function(r) {
            r.style.display = (!key || r.dataset.product === key) ? '' : 'none';
        });
    };

    /* ── Exportar CSV das movimentações visíveis ──────────────────────── */
    window.sz9stMovExportCSV = function() {
        var rows = Array.prototype.slice.call(
            document.querySelectorAll('#sz9st-mov-scroll tr[data-product]')
        ).filter(function(r) { return r.style.display !== 'none'; });

        var header = ['Data','Tipo','Referência','Detalhe','Produto','Quantidade','Saldo anterior','Saldo posterior','Status'];
        var csv = [header];
        rows.forEach(function(r) {
            csv.push([
                r.getAttribute('data-csv-date')    || '',
                r.getAttribute('data-csv-type')    || '',
                r.getAttribute('data-csv-ref')     || '',
                r.getAttribute('data-csv-meta')    || '',
                r.getAttribute('data-csv-product') || '',
                r.getAttribute('data-csv-qty')     || '',
                r.getAttribute('data-csv-before')  || '',
                r.getAttribute('data-csv-after')   || '',
                r.getAttribute('data-csv-status')  || ''
            ]);
        });
        var body = csv.map(function(line) {
            return line.map(function(v) {
                v = String(v).replace(/"/g, '""');
                return '"' + v + '"';
            }).join(';');
        }).join('\n');

        var blob = new Blob(['﻿' + body], { type: 'text/csv;charset=utf-8;' });
        var url  = URL.createObjectURL(blob);
        var a    = document.createElement('a');
        a.href     = url;
        a.download = 'senderzz-movimentacoes-estoque.csv';
        document.body.appendChild(a);
        a.click();
        a.remove();
        setTimeout(function() { URL.revokeObjectURL(url); }, 1000);
    };
})();
</script>
