<?php
/**
 * Senderzz V2 – Produtos (v454 — unificada completa)
 * Porta render_products_panel() da V1 nativamente.
 * Checkouts integrados — o menu "Links" é extinto.
 *
 * Dados reais: produtos por classe, KPIs de custódia, checkouts por produto,
 * envios (shipments) por produto, movimentações por produto.
 * Ações: gerar checkout, editar comissão, toggle afiliado, excluir — tudo via
 * AJAX senderzz_portal já existente (sem endpoint novo).
 *
 * @var object $sz_v2_user
 */
defined( 'ABSPATH' ) || exit;

/* ── helpers ─────────────────────────────────────────────────────────────── */
$sz9pr_ajax    = admin_url( 'admin-ajax.php' );
$sz9pr_nonce   = wp_create_nonce( 'senderzz_portal' );
$sz9pr_is_aff  = function_exists( 'sz_aff_portal_user_must_use_affiliate_scope' )
    && sz_aff_portal_user_must_use_affiliate_scope( $sz_v2_user );
$sz9pr_can_mgr = ! $sz9pr_is_aff && empty( $sz_v2_user->parent_user_id );

$sz9pr_money = static fn( float $v ): string =>
    function_exists( 'senderzz_portal_money' )
        ? senderzz_portal_money( $v )
        : 'R$ ' . number_format( $v, 2, ',', '.' );

/* ── classes de entrega ──────────────────────────────────────────────────── */
$sz9pr_class_ids = [];
if ( ! $sz9pr_is_aff ) {
    if ( function_exists( 'sz_get_user_class_ids' ) ) {
        $sz9pr_class_ids = array_values( array_filter( array_map( 'intval', sz_get_user_class_ids( $sz_v2_user ) ) ) );
    }
    if ( empty( $sz9pr_class_ids ) ) {
        $cid_leg = (int) ( $sz_v2_user->shipping_class_id ?? 0 );
        if ( $cid_leg > 0 ) $sz9pr_class_ids = [ $cid_leg ];
    }
} elseif ( function_exists( 'sz_aff_get_allowed_shipping_class_ids_for_portal_user' ) ) {
    $sz9pr_class_ids = array_values( array_filter( array_map( 'intval',
        sz_aff_get_allowed_shipping_class_ids_for_portal_user( $sz_v2_user ) ) ) );
}

// CD filter — CDs reais da tabela sz_motoboy_cds (igual à seção Localidades)
// Fallback para classes de entrega caso a tabela não exista ou esteja vazia.
global $wpdb;
$sz9pr_classes_map = [];
$sz9pr_cd_table    = $wpdb->prefix . 'sz_motoboy_cds';
if ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $sz9pr_cd_table ) ) === $sz9pr_cd_table ) {
    // phpcs:ignore WordPress.DB.PreparedSQL
    $sz9pr_real_cds = $wpdb->get_results(
        "SELECT id, nome FROM {$sz9pr_cd_table} ORDER BY nome ASC LIMIT 100",  // sem filtro ativo para garantir resultado
        ARRAY_A
    ) ?: [];
    foreach ( $sz9pr_real_cds as $sz9pr_cd_row ) {
        $sz9pr_classes_map[ (int) $sz9pr_cd_row['id'] ] = (string) $sz9pr_cd_row['nome'];
    }
}
// Fallback: se não encontrou CDs reais, usa as classes de entrega do produtor
if ( empty( $sz9pr_classes_map ) && ! empty( $sz9pr_class_ids ) ) {
    foreach ( $sz9pr_class_ids as $sz9pr_cid_loop ) {
        $sz9pr_term = get_term( $sz9pr_cid_loop, 'product_shipping_class' );
        $sz9pr_classes_map[ $sz9pr_cid_loop ] = ( $sz9pr_term && ! is_wp_error( $sz9pr_term ) )
            ? (string) $sz9pr_term->name
            : 'CD #' . $sz9pr_cid_loop;
    }
}
$sz9pr_cd_filter = isset( $_GET['cd'] ) ? absint( $_GET['cd'] ) : 0; // phpcs:ignore

/* ── produtos ────────────────────────────────────────────────────────────── */
$sz9pr_items = [];
if ( function_exists( 'wc_get_products' ) ) {
    $sz9pr_all = wc_get_products( [
        'limit'   => -1,
        'status'  => 'publish',
        'type'    => [ 'simple', 'variation', 'variable' ],
        'orderby' => 'title',
        'order'   => 'ASC',
        'return'  => 'objects',
    ] );
    foreach ( (array) $sz9pr_all as $sz9pr_p ) {
        if ( ! $sz9pr_p instanceof WC_Product ) continue;
        $cid = (int) $sz9pr_p->get_shipping_class_id();
        if ( $cid <= 0 && $sz9pr_p->is_type( 'variation' ) ) {
            $par = wc_get_product( $sz9pr_p->get_parent_id() );
            $cid = $par ? (int) $par->get_shipping_class_id() : 0;
        }
        if ( ! empty( $sz9pr_class_ids ) && ! in_array( $cid, $sz9pr_class_ids, true ) ) continue;
        $type = $sz9pr_p->get_type();
        $name = $sz9pr_p->get_name();
        $hay  = strtolower( remove_accents( $name . ' ' . $type ) );
        if ( in_array( $type, [ 'grouped', 'bundle', 'composite', 'woosb' ], true )
             || preg_match( '/\b(kit|combo|pacote|pack|bundle|conjunto)\b/i', $hay )
             || preg_match( '/\+/', $hay ) ) continue;
        $sz9pr_items[ (int) $sz9pr_p->get_id() ] = [
            'id'          => (int) $sz9pr_p->get_id(),
            'name'        => $name,
            'sku'         => $sz9pr_p->get_sku(),
            'available'   => max( 0, (int) ( $sz9pr_p->get_stock_quantity() ?? 0 ) ),
            'reserved'    => 0,
            'route'       => 0,
            'delivered'   => 0,
            'frustrated'  => 0,
            'image'       => wp_get_attachment_image_url( (int) $sz9pr_p->get_image_id(), 'thumbnail' ) ?: '',
            'checkouts'   => [],
            'shipments'   => [],
            'movements'   => [],
            'cd_ids'      => [],
        ];
    }
}

/* ── KPIs físicos via custódia ───────────────────────────────────────────── */
global $wpdb;
$sz9pr_cust = $wpdb->prefix . 'sz_motoboy_stock_custody';
if ( ! empty( $sz9pr_items ) &&
     $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $sz9pr_cust ) ) === $sz9pr_cust ) {
    $sz9pr_pids = array_keys( $sz9pr_items );
    $sz9pr_ph   = implode( ',', array_fill( 0, count( $sz9pr_pids ), '%d' ) );
    // Filtro de CD: se um CD estiver selecionado, restringe os KPIs ao cd_id escolhido
    $sz9pr_cd_where = '';
    $sz9pr_cd_args  = [];
    if ( $sz9pr_cd_filter > 0 ) {
        $sz9pr_cd_where = ' AND cd_id = %d';
        $sz9pr_cd_args  = [ $sz9pr_cd_filter ];
    }
    // phpcs:ignore WordPress.DB.PreparedSQL
    $sz9pr_crow = $wpdb->get_results( $wpdb->prepare(
        "SELECT product_id, variation_id, physical_status, COALESCE(SUM(quantity),0) AS qty
           FROM {$sz9pr_cust}
          WHERE ( product_id IN ({$sz9pr_ph}) OR variation_id IN ({$sz9pr_ph}) )
            AND physical_status IN ('reserved','with_motoboy','frustrated','return_declared','delivered'){$sz9pr_cd_where}
          GROUP BY product_id, variation_id, physical_status",
        array_merge( $sz9pr_pids, $sz9pr_pids, $sz9pr_cd_args )
    ) );
    foreach ( (array) $sz9pr_crow as $cr ) {
        $tid = (int) ( $cr->variation_id ?: $cr->product_id );
        if ( ! isset( $sz9pr_items[ $tid ] ) ) continue;
        $q   = max( 0, (int) $cr->qty );
        $cid_item = (int) ( $cr->cd_id ?? 0 );
        if ( $cid_item > 0 ) {
            $sz9pr_items[ $tid ]['cd_ids'][ $cid_item ] = true;
        }
        switch ( (string) $cr->physical_status ) {
            case 'reserved':          $sz9pr_items[ $tid ]['reserved']   += $q; break;
            case 'with_motoboy':      $sz9pr_items[ $tid ]['route']       += $q; break;
            case 'delivered':         $sz9pr_items[ $tid ]['delivered']   += $q; break;
            case 'frustrated':
            case 'return_declared':   $sz9pr_items[ $tid ]['frustrated']  += $q; break;
        }
    }
}

/* ── Checkouts/Links por produto ─────────────────────────────────────────── */
$sz9pr_lk_table = $wpdb->prefix . 'senderzz_checkout_links';
$sz9pr_all_links = [];
if ( $sz9pr_is_aff ) {
    if ( function_exists( 'sz_aff_get_visible_checkout_links_for_portal_user' ) ) {
        $sz9pr_all_links = (array) sz_aff_get_visible_checkout_links_for_portal_user( $sz_v2_user );
    }
} elseif ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $sz9pr_lk_table ) ) === $sz9pr_lk_table ) {
    if ( function_exists( 'senderzz_portal_ensure_checkout_links_table' ) ) {
        senderzz_portal_ensure_checkout_links_table();
    }
    // phpcs:ignore WordPress.DB.PreparedSQL
    $sz9pr_all_links = $wpdb->get_results( $wpdb->prepare(
        "SELECT * FROM {$sz9pr_lk_table} WHERE user_id = %d ORDER BY created_at DESC LIMIT 300",
        (int) $sz_v2_user->id
    ), ARRAY_A ) ?: [];
}

// Associa links → produto por payload/components_text.
foreach ( $sz9pr_all_links as $lk ) {
    $lk_prod_ids = [];
    $pay = (string) ( $lk['payload'] ?? '' );
    if ( $pay !== '' ) {
        $pay_arr = json_decode( wp_unslash( $pay ), true );
        if ( is_array( $pay_arr ) ) {
            array_walk_recursive( $pay_arr, static function ( $v, $k ) use ( &$lk_prod_ids ) {
                if ( in_array( strtolower( (string) $k ), [ 'product_id', 'id', 'pid' ], true ) && is_numeric( $v ) ) {
                    $lk_prod_ids[] = (int) $v;
                }
            } );
        }
    }
    $comp = strtolower( remove_accents( (string) ( $lk['components_text'] ?? '' ) . ' ' . (string) ( $lk['name'] ?? '' ) ) );
    $tipo = strtolower( (string) ( $lk['tipo'] ?? '' ) );
    if ( $tipo === 'motoboy' ) continue; // espelhos Motoboy não aparecem como card principal.
    $matched = false;
    foreach ( $sz9pr_items as $pid => $it ) {
        $prod_name_n = strtolower( remove_accents( $it['name'] ) );
        $name_match  = ( $comp !== '' && false !== strpos( $comp, $prod_name_n ) );
        $pid_match   = in_array( $pid, $lk_prod_ids, true );
        if ( $name_match || $pid_match ) {
            $sz9pr_items[ $pid ]['checkouts'][] = $lk;
            $matched = true;
        }
    }
    // Se o link não foi associado a nenhum produto, coloca no primeiro (fallback).
    if ( ! $matched && ! empty( $sz9pr_items ) ) {
        $first_pid = array_key_first( $sz9pr_items );
        $sz9pr_items[ $first_pid ]['checkouts'][] = $lk;
    }
}

/* ── Envios por produto ──────────────────────────────────────────────────── */
$sz9pr_ship_t = $wpdb->prefix . 'senderzz_stock_shipments';
$sz9pr_item_t = $wpdb->prefix . 'senderzz_stock_shipment_items';
if ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $sz9pr_ship_t ) ) === $sz9pr_ship_t &&
     $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $sz9pr_item_t ) ) === $sz9pr_item_t ) {
    // phpcs:ignore WordPress.DB.PreparedSQL
    $sz9pr_ships = $wpdb->get_results( $wpdb->prepare(
        "SELECT s.id, s.status, s.carrier, s.tracking, s.created_at, s.sent_at, s.delivered_at, s.concluded_at,
                i.product_id, i.variation_id, i.qty_sent, i.qty_received
           FROM {$sz9pr_ship_t} s
           JOIN {$sz9pr_item_t} i ON i.shipment_id = s.id
          WHERE s.user_id = %d
          ORDER BY COALESCE(s.concluded_at,s.delivered_at,s.sent_at,s.created_at) DESC
          LIMIT 300",
        (int) $sz_v2_user->id
    ), ARRAY_A ) ?: [];
    foreach ( $sz9pr_ships as $sh ) {
        $pid  = ! empty( $sh['variation_id'] ) ? (int) $sh['variation_id'] : (int) $sh['product_id'];
        $tid  = isset( $sz9pr_items[ $pid ] ) ? $pid : 0;
        if ( ! $tid ) {
            $pp = $pid > 0 ? wc_get_product( $pid ) : null;
            if ( $pp instanceof WC_Product && $pp->is_type( 'variation' ) ) {
                $par_id = (int) $pp->get_parent_id();
                if ( isset( $sz9pr_items[ $par_id ] ) ) $tid = $par_id;
            }
        }
        if ( ! $tid ) continue;
        $shown_qty = max( (int) ( $sh['qty_received'] ?? 0 ), (int) ( $sh['qty_sent'] ?? 0 ) );
        if ( $shown_qty <= 0 ) continue;
        $when = $sh['concluded_at'] ?? $sh['delivered_at'] ?? $sh['sent_at'] ?? $sh['created_at'] ?? '';
        $date = $when ? wp_date( 'd/m/Y H:i', strtotime( $when . ' UTC' ) ) : '—';
        $st_map = [ 'pendente' => 'Pendente', 'enviado' => 'Enviado', 'entregue' => 'Recebido', 'concluido' => 'Concluído' ];
        $st_label = $st_map[ (string) ( $sh['status'] ?? '' ) ] ?? ucfirst( (string) ( $sh['status'] ?? '' ) );
        $meta     = trim( (string) ( $sh['carrier'] ?: '' ) . ( ! empty( $sh['tracking'] ) ? ' · ' . $sh['tracking'] : '' ) );
        $sz9pr_items[ $tid ]['shipments'][] = [
            'id'     => (int) $sh['id'],
            'label'  => 'Envio #' . (int) $sh['id'],
            'date'   => $date,
            'status' => $st_label,
            'meta'   => $meta,
            'qty'    => $shown_qty,
        ];
    }
}

$sz9pr_first = ! empty( $sz9pr_items ) ? array_key_first( $sz9pr_items ) : 0;
?>
<section id="sec-products" class="sz-sec" data-szv2-label="Produtos">

    <?php if ( count( $sz9pr_classes_map ) >= 1 ) : ?>
    <!-- Seletor de CD — etapa antes dos produtos -->
    <div class="szv2-card" style="margin-bottom:var(--szv2-space-4)">
        <div class="szv2-card-head">
            <div>
                <h2>Centro de Distribuição</h2>
                <p class="szv2-card-sub">Selecione o CD para visualizar os produtos e estoque disponível.</p>
            </div>
            <?php if ( $sz9pr_can_mgr ) : ?>
            <button type="button"
                    class="szv2-btn szv2-btn-secondary szv2-btn-sm"
                    onclick="document.getElementById('szv2-add-product-modal').style.display='flex'">
                + Adicionar produto
            </button>
            <?php endif; ?>
        </div>
        <div style="display:flex;gap:var(--szv2-space-3);flex-wrap:wrap">
            <button type="button"
                    class="szv2-btn szv2-btn-sm <?php echo $sz9pr_cd_filter === 0 ? 'szv2-btn-brand' : 'szv2-btn-secondary'; ?>"
                    data-action="cd-select"
                    data-cd-id="0">
                Todos os CDs
            </button>
            <?php foreach ( $sz9pr_classes_map as $sz9pr_cd_id => $sz9pr_cd_name ) : ?>
            <button type="button"
                    class="szv2-btn szv2-btn-sm <?php echo $sz9pr_cd_filter === $sz9pr_cd_id ? 'szv2-btn-brand' : 'szv2-btn-secondary'; ?>"
                    data-action="cd-select"
                    data-cd-id="<?php echo esc_attr( (string) $sz9pr_cd_id ); ?>">
                <?php echo esc_html( $sz9pr_cd_name ); ?>
            </button>
            <?php endforeach; ?>
        </div>
    </div>
    <?php endif; ?>

    <!-- Abas de produto + botão Adicionar -->
    <div class="szv2-prod-tabs-row">
        <div class="szv2-prod-tabs" role="tablist">
        <?php foreach ( $sz9pr_items as $sz9pr_pid => $sz9pr_it ) :
            $sz9pr_act = ( $sz9pr_pid === $sz9pr_first ); ?>
        <button type="button"
                class="szv2-prod-tab<?php echo $sz9pr_act ? ' szv2-prod-tab--active' : ''; ?>"
                role="tab"
                aria-selected="<?php echo $sz9pr_act ? 'true' : 'false'; ?>"
                data-prod="<?php echo esc_attr( (string) $sz9pr_pid ); ?>"
                data-cd-ids="<?php echo esc_attr( implode( ',', array_keys( $sz9pr_it['cd_ids'] ) ) ); ?>"
                data-action="prod-select"
                data-product-id="<?php echo esc_attr( (string) $sz9pr_pid ); ?>">
            <?php echo esc_html( $sz9pr_it['name'] ); ?>
        </button>
        <?php endforeach; ?>
        </div>

        <?php if ( $sz9pr_can_mgr && count( $sz9pr_classes_map ) == 0 ) : ?>
        <button type="button"
                class="szv2-btn szv2-btn-secondary szv2-btn-sm"
                onclick="document.getElementById('szv2-add-product-modal').style.display='flex'">
            + Adicionar produto
        </button>
        <?php endif; ?>
    </div>

    <!-- Painel por produto -->
    <?php foreach ( $sz9pr_items as $sz9pr_pid => $sz9pr_it ) :
        $sz9pr_act  = ( $sz9pr_pid === $sz9pr_first );
        $sz9pr_pchk = $sz9pr_it['checkouts'];
        $sz9pr_pshp = $sz9pr_it['shipments'];
    ?>
    <div class="szv2-prod-panel<?php echo $sz9pr_act ? '' : ' szv2-prod-panel--hidden'; ?>"
         data-prod-panel="<?php echo esc_attr( (string) $sz9pr_pid ); ?>"
         data-cd-ids="<?php echo esc_attr( implode( ',', array_keys( $sz9pr_it['cd_ids'] ) ) ); ?>"
         role="tabpanel">

        <!-- Cabeçalho do produto -->
        <div class="szv2-card szv2-prod-head-card">
            <?php if ( $sz9pr_it['image'] ) : ?>
            <img class="szv2-prod-thumb" src="<?php echo esc_url( $sz9pr_it['image'] ); ?>" alt="" loading="lazy">
            <?php else : ?>
            <div class="szv2-prod-thumb szv2-prod-thumb--ph" aria-hidden="true">
                <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/></svg>
            </div>
            <?php endif; ?>
            <div class="szv2-prod-head-meta">
                <h2><?php echo esc_html( $sz9pr_it['name'] ); ?></h2>
                <p>Checkouts, estoque, envios e movimentações deste produto.</p>
                <?php if ( $sz9pr_it['sku'] ) : ?>
                <span class="szv2-prod-sku">SKU <?php echo esc_html( $sz9pr_it['sku'] ); ?></span>
                <?php endif; ?>
            </div>
        </div>

        <!-- KPIs físicos -->
        <div class="szv2-kpi-grid szv2-kpi-grid-5">
            <?php // phpcs:ignore WordPress.Security.EscapeOutput
            echo sz_v2_kpi_card( [ 'label' => 'Disponíveis', 'value' => (string) $sz9pr_it['available'],  'meta' => 'unidades', 'value_class' => 'szv2-num' ] );
            echo sz_v2_kpi_card( [ 'label' => 'Reservados',  'value' => (string) $sz9pr_it['reserved'],   'meta' => 'unidades', 'value_class' => 'szv2-num' ] );
            echo sz_v2_kpi_card( [ 'label' => 'Em rota',     'value' => (string) $sz9pr_it['route'],      'meta' => 'unidades', 'value_class' => 'szv2-num' ] );
            echo sz_v2_kpi_card( [ 'label' => 'Entregues',   'value' => (string) $sz9pr_it['delivered'],  'meta' => 'unidades', 'value_class' => 'szv2-num' ] );
            echo sz_v2_kpi_card( [ 'label' => 'Frustrados',  'value' => (string) $sz9pr_it['frustrated'], 'meta' => 'unidades', 'value_class' => 'szv2-num' ] );
            ?>
        </div>
        <!-- Sub-abas -->
        <div class="szv2-prod-subtabs" role="tablist">
            <button type="button" class="szv2-prod-subtab szv2-prod-subtab--active"
                    data-action="prod-sub"
                    data-product-id="<?php echo esc_attr( (string) $sz9pr_pid ); ?>"
                    data-sub="checkouts">
                Checkouts <?php if ( ! empty( $sz9pr_pchk ) ) : ?><span class="szv2-prod-subtab-count"><?php echo esc_html( (string) count( $sz9pr_pchk ) ); ?></span><?php endif; ?>
            </button>
            <button type="button" class="szv2-prod-subtab"
                    data-action="prod-sub"
                    data-product-id="<?php echo esc_attr( (string) $sz9pr_pid ); ?>"
                    data-sub="envios">
                Envios <?php if ( ! empty( $sz9pr_pshp ) ) : ?><span class="szv2-prod-subtab-count"><?php echo esc_html( (string) count( $sz9pr_pshp ) ); ?></span><?php endif; ?>
            </button>
            <button type="button" class="szv2-prod-subtab"
                    data-action="prod-sub"
                    data-product-id="<?php echo esc_attr( (string) $sz9pr_pid ); ?>"
                    data-sub="movs">Movimentações</button>
        </div>

        <!-- SUB: Checkouts -->
        <div class="szv2-prod-sub" data-sub-panel="checkouts">
            <div class="szv2-card">
                <div class="szv2-card-head">
                    <div><h3>Checkouts deste produto</h3><p class="szv2-card-sub">Links de Expedição e Motoboy vinculados.</p></div>
                    <?php if ( $sz9pr_can_mgr ) : ?>
                    <button type="button" class="szv2-btn szv2-btn-brand szv2-btn-sm"
                            data-action="new-checkout"
                            data-product-id="<?php echo esc_attr( (string) $sz9pr_pid ); ?>"
                            data-name="<?php echo esc_attr( $sz9pr_it['name'] ); ?>">
                        + Gerar checkout
                    </button>
                    <?php endif; ?>
                </div>
                <?php if ( empty( $sz9pr_pchk ) ) : ?>
                    <?php // phpcs:ignore WordPress.Security.EscapeOutput
                    echo sz_v2_empty_state( [
                        'title' => 'Nenhum checkout vinculado',
                        'text'  => $sz9pr_can_mgr ? 'Clique em "+ Gerar checkout" para criar o primeiro link.' : 'Nenhum checkout liberado para você neste produto.',
                    ] ); ?>
                <?php else : ?>
                <div class="szv2-table-wrap">
                    <table class="szv2-table">
                        <thead>
                            <tr>
                                <th>Nome</th>
                                <th>Valor</th>
                                <th style="white-space:nowrap">Comissão %</th>
                                <th>Links</th>
                                <?php if ( $sz9pr_can_mgr ) : ?><th>Afiliados</th><th></th><?php endif; ?>
                            </tr>
                        </thead>
                        <tbody>
                    <?php foreach ( $sz9pr_pchk as $sz9pr_lk ) :
                        $sz9pr_lk_id    = (int) ( $sz9pr_lk['id'] ?? 0 );
                        $sz9pr_lk_name_raw = (string) ( $sz9pr_lk['name'] ?? '' );
                        $sz9pr_lk_disp     = (string) ( $sz9pr_lk['display_name'] ?? '' );
                        // Use display_name only if it's not UUID-format; otherwise fall back to name
                        $sz9pr_lk_is_uuid  = (bool) preg_match( '/^[0-9a-f]{8,}/i', $sz9pr_lk_disp );
                        $sz9pr_lk_name     = ( $sz9pr_lk_name_raw !== '' ) ? $sz9pr_lk_name_raw : ( ! $sz9pr_lk_is_uuid ? $sz9pr_lk_disp : '—' );
                        $sz9pr_lk_comps = (string) ( $sz9pr_lk['components_text'] ?? '' );
                        $sz9pr_lk_price = (string) ( $sz9pr_lk['price_label'] ?? '' );
                        $sz9pr_lk_vis   = ! empty( $sz9pr_lk['affiliate_visible'] );
                        $sz9pr_lk_comm  = (float) ( $sz9pr_lk['affiliate_commission_pct'] ?? 0 );
                        $sz9pr_lk_slug  = (string) ( $sz9pr_lk['slug'] ?? $sz9pr_lk['token'] ?? substr( (string) ( $sz9pr_lk['id'] ?? '' ), 0, 8 ) );
                        $sz9pr_lk_url_exp = (string) ( $sz9pr_is_aff
                            ? ( $sz9pr_lk['affiliate_url'] ?? '' )
                            : ( $sz9pr_lk['checkout_url'] ?? $sz9pr_lk['url'] ?? '' ) );
                        $sz9pr_lk_mb      = ! empty( $sz9pr_lk['link_motoboy_id'] )
                            ? (array) $wpdb->get_row( $wpdb->prepare( "SELECT url FROM {$sz9pr_lk_table} WHERE id=%d LIMIT 1", (int) $sz9pr_lk['link_motoboy_id'] ), ARRAY_A )
                            : null;
                        $sz9pr_lk_url_mb  = $sz9pr_lk_mb ? (string) ( $sz9pr_lk_mb['url'] ?? '' ) : '';
                    ?>
                        <tr data-link-id="<?php echo esc_attr( (string) $sz9pr_lk_id ); ?>">
                            <td>
                                <div class="szv2-td-main"><?php echo esc_html( $sz9pr_lk_name ); ?></div>
                            </td>
                            <td class="szv2-num" style="white-space:nowrap"><?php echo esc_html( $sz9pr_lk_price ); ?></td>
                            <td>
                                <?php if ( $sz9pr_can_mgr ) : ?>
                                <div style="display:flex;align-items:center;gap:6px;justify-content:flex-start">
                                    <input type="number"
                                           class="szv2-input szv2-lk-comm-input"
                                           value="<?php echo esc_attr( number_format( $sz9pr_lk_comm, 0, '.', '' ) ); ?>"
                                           step="1" min="0" max="99"
                                           data-link-id="<?php echo esc_attr( (string) $sz9pr_lk_id ); ?>"
                                           data-nonce="<?php echo esc_attr( $sz9pr_nonce ); ?>"
                                           style="width:52px;height:32px;text-align:center;padding:4px 6px"
                                           oninput="if(this.value.length>2)this.value=this.value.slice(0,2);if(parseInt(this.value)>99)this.value='99'">
                                    <button type="button" class="szv2-btn szv2-btn-secondary szv2-btn-sm" data-action="save-comm">Salvar</button>
                                </div>
                                <?php else : ?>
                                <span class="szv2-num"><?php echo esc_html( number_format( $sz9pr_lk_comm, 0, ',', '.' ) ); ?>%</span>
                                <?php endif; ?>
                            </td>
                            <td>
                                <div style="display:flex;gap:6px;flex-wrap:wrap;align-items:center">
                                    <?php if ( $sz9pr_lk_url_exp ) : ?>
                                    <button type="button" class="szv2-btn szv2-btn-brand szv2-btn-sm"
                                            data-action="copy-url"
                                            data-url="<?php echo esc_attr( $sz9pr_lk_url_exp ); ?>">Expedição</button>
                                    <?php endif; ?>
                                    <?php if ( $sz9pr_lk_url_mb && ! $sz9pr_is_aff ) : ?>
                                    <button type="button" class="szv2-btn szv2-btn-brand szv2-btn-sm"
                                            data-action="copy-url"
                                            data-url="<?php echo esc_attr( $sz9pr_lk_url_mb ); ?>">Motoboy</button>
                                    <?php endif; ?>
                                </div>
                            </td>
                            <?php if ( $sz9pr_can_mgr ) : ?>
                            <td>
                                <label class="szv2-toggle-lbl">
                                    <input type="checkbox"
                                           <?php checked( $sz9pr_lk_vis ); ?>
                                           data-link-id="<?php echo esc_attr( (string) $sz9pr_lk_id ); ?>"
                                           data-nonce="<?php echo esc_attr( $sz9pr_nonce ); ?>"
                                           onchange="szV2ToggleAffVisible(this)">
                                    <span class="szv2-toggle-slider"></span>
                                </label>
                            </td>
                            <td style="text-align:right">
                                <button type="button" class="szv2-btn szv2-btn-sm szv2-btn-danger"
                                        data-link-id="<?php echo esc_attr( (string) $sz9pr_lk_id ); ?>"
                                        data-link-name="<?php echo esc_attr( $sz9pr_lk_name ); ?>"
                                        data-nonce="<?php echo esc_attr( $sz9pr_nonce ); ?>"
                                        data-action="delete-link">Excluir</button>
                            </td>
                            <?php endif; ?>
                        </tr>
                    <?php endforeach; ?>
                        </tbody>
                    </table>
                </div>
                <p style="font-size:12px;color:var(--szv2-text-faint);margin-top:8px">Se o checkout tiver mais produtos, ele aparece em todos os produtos vinculados.</p>
                <?php endif; ?>
            </div>
        </div>

        <!-- SUB: Envios -->
        <div class="szv2-prod-sub szv2-prod-sub--hidden" data-sub-panel="envios">
            <div class="szv2-card">
                <div class="szv2-card-head"><div><h3>Envios de estoque</h3><p class="szv2-card-sub">Reposições enviadas para o CD.</p></div></div>
                <?php if ( empty( $sz9pr_pshp ) ) : ?>
                    <?php // phpcs:ignore WordPress.Security.EscapeOutput
                    echo sz_v2_empty_state( [ 'title' => 'Nenhum envio registrado', 'text' => 'Os envios de reposição aparecem aqui assim que forem registrados.' ] ); ?>
                <?php else : ?>
                <div class="szv2-wh-list">
                    <?php foreach ( $sz9pr_pshp as $sh ) : ?>
                    <div class="szv2-ship-row">
                        <div class="szv2-ship-info">
                            <span class="szv2-ship-title"><?php echo esc_html( $sh['label'] ); ?></span>
                            <span class="szv2-ship-meta"><?php echo esc_html( $sh['date'] . ' · ' . $sh['status'] . ( $sh['meta'] ? ' · ' . $sh['meta'] : '' ) ); ?></span>
                        </div>
                        <span class="szv2-ship-qty szv2-num"><?php echo esc_html( (string) $sh['qty'] ); ?> un</span>
                    </div>
                    <?php endforeach; ?>
                </div>
                <?php endif; ?>
            </div>
        </div>

        <!-- SUB: Movimentações -->
        <div class="szv2-prod-sub szv2-prod-sub--hidden" data-sub-panel="movs">
            <div class="szv2-card">
                <div class="szv2-card-head"><div><h3>Movimentações de estoque</h3><p class="szv2-card-sub">Entradas e saídas deste produto.</p></div></div>
                <?php
                $sz9pr_movs = [];
                if ( function_exists( 'senderzz_stock_get_movement_rows' ) ) {
                    foreach ( $sz9pr_class_ids as $sz9pr_c_mv ) {
                        $sz9pr_movs = array_merge( $sz9pr_movs, senderzz_stock_get_movement_rows( (int) $sz9pr_c_mv, 60 ) );
                    }
                    $prod_n = strtolower( $sz9pr_it['name'] );
                    $sz9pr_movs = array_values( array_filter( $sz9pr_movs, static fn( $mv ) =>
                        '' === strtolower( (string) ( $mv['product_name'] ?? '' ) )
                        || false !== strpos( strtolower( (string) ( $mv['product_name'] ?? '' ) ), $prod_n )
                    ) );
                    usort( $sz9pr_movs, static fn( $a, $b ) => (int) ( $b['timestamp'] ?? 0 ) - (int) ( $a['timestamp'] ?? 0 ) );
                    $sz9pr_movs = array_slice( $sz9pr_movs, 0, 40 );
                }
                ?>
                <?php if ( empty( $sz9pr_movs ) ) : ?>
                    <?php // phpcs:ignore WordPress.Security.EscapeOutput
                    echo sz_v2_empty_state( [ 'title' => 'Nenhuma entrada ou saída', 'text' => 'As movimentações deste produto aparecem aqui.' ] ); ?>
                <?php else : ?>
                <div class="szv2-table-wrap">
                    <table class="szv2-table">
                        <thead><tr><th>Data</th><th>Tipo</th><th>Referência</th><th class="szv2-td-num">Qtd</th><th class="szv2-td-num">Saldo</th></tr></thead>
                        <tbody>
                            <?php foreach ( $sz9pr_movs as $mv ) : ?>
                            <tr>
                                <td class="szv2-td-sub"><?php echo esc_html( (string) ( $mv['date_fmt'] ?? '—' ) ); ?></td>
                                <td><?php echo esc_html( (string) ( $mv['direction_label'] ?? 'Mov.' ) ); ?></td>
                                <td class="szv2-td-sub"><?php echo esc_html( (string) ( $mv['source_label'] ?? '—' ) ); ?></td>
                                <td class="szv2-td-num szv2-num"><?php echo esc_html( (string) ( $mv['qty_display'] ?? ( $mv['qty'] ?? '—' ) ) ); ?></td>
                                <td class="szv2-td-num szv2-num"><?php echo esc_html( (string) ( $mv['after'] ?? '—' ) ); ?></td>
                            </tr>
                            <?php endforeach; ?>
                        </tbody>
                    </table>
                </div>
                <?php endif; ?>
            </div>
        </div>

    </div><!-- /prod-panel -->
    <?php endforeach; ?>

    <!-- Modal: Gerar checkout (somente produtor) -->
    <?php if ( $sz9pr_can_mgr ) : ?>
    <!-- Modal: Gerar checkout com seleção de produtos -->
    <div id="szv2-new-checkout-modal" class="szv2-modal-overlay" style="display:none" role="dialog" aria-modal="true" aria-labelledby="szv2-modal-title">
        <div class="szv2-modal" style="width:580px;max-width:96vw">
            <div class="szv2-modal-head">
                <h3 id="szv2-modal-title">Gerar novo checkout</h3>
                <button type="button" class="szv2-modal-x" onclick="szV2CloseNewCheckout()">×</button>
            </div>
            <div class="szv2-modal-body">
                <div class="szv2-input-group">
                    <label class="szv2-label">Valor (R$)</label>
                    <input type="number" id="szv2-cl-valor" class="szv2-input" placeholder="0.00" step="0.01" min="0">
                </div>
                <div class="szv2-input-group">
                    <label class="szv2-label">Comissão do afiliado (%)</label>
                    <input type="number" id="szv2-cl-comm" class="szv2-input"
                           placeholder="0" value="0" min="0" max="99" step="1"
                           oninput="this.value=Math.min(99,Math.max(0,parseInt(this.value.replace(/\D/g,''))||0)).toString()">
                </div>
                <!-- Seleção de produtos: todos os produtos do produtor -->
                <div class="szv2-input-group">
                    <label class="szv2-label">Produtos incluídos</label>
                    <div id="szv2-cl-products-list" class="szv2-prod-select-list">
                        <?php foreach ( $sz9pr_items as $sz9pr_pid_m => $sz9pr_it_m ) : ?>
                        <label class="szv2-prod-select-item">
                            <input type="checkbox" class="szv2-cl-prod-cb" value="<?php echo esc_attr( (string) $sz9pr_pid_m ); ?>" checked>
                            <span class="szv2-prod-select-name"><?php echo esc_html( $sz9pr_it_m['name'] ); ?></span>
                            <span class="szv2-prod-select-qty">
                                × <input type="number" class="szv2-cl-prod-qty" value="1" min="1" max="99" style="width:44px;padding:2px 6px;border:1px solid var(--szv2-border);border-radius:4px;font-size:12px;text-align:center">
                            </span>
                        </label>
                        <?php endforeach; ?>
                    </div>
                    <p style="font-size:11px;color:var(--szv2-text-faint);margin:4px 0 0">O nome do checkout é gerado automaticamente com base nos produtos e quantidades. Selecione um ou mais produtos.</p>
                </div>
                <label class="szv2-lk-aff-toggle">
                    <input type="checkbox" id="szv2-cl-aff-vis"> <span>Visível para afiliados</span>
                </label>
                <div id="szv2-modal-err" style="display:none;margin-top:10px;color:var(--szv2-danger);font-size:13px"></div>
            </div>
            <div class="szv2-modal-foot">
                <button type="button" class="szv2-btn szv2-btn-secondary" onclick="szV2CloseNewCheckout()">Cancelar</button>
                <button type="button" class="szv2-btn szv2-btn-brand" id="szv2-cl-save-btn"
                        data-ajax="<?php echo esc_attr( $sz9pr_ajax ); ?>"
                        data-nonce="<?php echo esc_attr( $sz9pr_nonce ); ?>"
                        onclick="szV2SaveNewCheckout(this)">
                    Gerar checkout
                </button>
            </div>
        </div>
    </div>

    <!-- Modal: Adicionar produto (envia para aprovação do admin) -->
    <div id="szv2-add-product-modal" class="szv2-modal-overlay" style="display:none" role="dialog" aria-modal="true">
        <div class="szv2-modal" style="width:540px;max-width:96vw">
            <div class="szv2-modal-head">
                <h3>Adicionar produto</h3>
                <button type="button" class="szv2-modal-x" onclick="document.getElementById('szv2-add-product-modal').style.display='none'">×</button>
            </div>
            <div class="szv2-modal-body">
                <p style="font-size:13px;color:var(--szv2-text-muted);margin:0 0 14px">Após o envio, o produto passa por revisão. Quando aprovado, estará disponível para criação de checkouts e envio de estoque.</p>
                <div class="szv2-input-group">
                    <label class="szv2-label">Nome do produto</label>
                    <input type="text" id="szv2-np-name" class="szv2-input" placeholder="Nome completo do produto">
                </div>
                <div class="szv2-input-group">
                    <label class="szv2-label">Categoria</label>
                    <select id="szv2-np-cat" class="szv2-input">
                        <option value="suplemento">Suplemento</option>
                        <option value="cosmetico">Cosmético</option>
                        <option value="eletronico">Eletrônico</option>
                        <option value="vestuario">Vestuário</option>
                        <option value="alimento">Alimento</option>
                        <option value="outro">Outro</option>
                    </select>
                </div>
                <div class="szv2-fr-carrier-grid" style="grid-template-columns:repeat(3,1fr)">
                    <div class="szv2-input-group"><label class="szv2-label">Peso (g)</label><input type="number" id="szv2-np-peso" class="szv2-input" placeholder="Ex: 250" min="1"></div>
                    <div class="szv2-input-group"><label class="szv2-label">Alt (cm)</label><input type="number" id="szv2-np-alt" class="szv2-input" placeholder="Ex: 10" min="1"></div>
                    <div class="szv2-input-group"><label class="szv2-label">Larg (cm)</label><input type="number" id="szv2-np-larg" class="szv2-input" placeholder="Ex: 8" min="1"></div>
                    <div class="szv2-input-group"><label class="szv2-label">Comp (cm)</label><input type="number" id="szv2-np-comp" class="szv2-input" placeholder="Ex: 5" min="1"></div>
                </div>
                <div class="szv2-input-group">
                    <label class="szv2-label">URL da foto do rótulo / produto</label>
                    <input type="url" id="szv2-np-foto" class="szv2-input" placeholder="https://... (link da imagem)">
                </div>
                <div class="szv2-input-group">
                    <label class="szv2-label">Descrição para a Vitrine</label>
                    <textarea id="szv2-np-vitrine-desc" class="szv2-input" rows="3" placeholder="Descreva o produto para afiliados: benefícios, público-alvo, diferenciais..."></textarea>
                </div>
                <div class="szv2-input-group">
                    <label class="szv2-label">Observações adicionais (para o operador)</label>
                    <textarea id="szv2-np-obs" class="szv2-input" rows="2" placeholder="Informações adicionais para o operador..."></textarea>
                </div>
                <!-- Variações -->
                <div style="margin-top:var(--szv2-space-3)">
                    <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:8px">
                        <p style="font-size:13px;font-weight:600;color:var(--szv2-text);margin:0">Variações (opcional)</p>
                        <button type="button" class="szv2-btn szv2-btn-secondary szv2-btn-sm" data-action="np-add-var">+ Adicionar variação</button>
                    </div>
                    <p style="font-size:12px;color:var(--szv2-text-muted);margin:0 0 8px">Ex: P, M, G, GG, 100ml, 500mg — com seus respectivos pesos e medidas se diferentes.</p>
                    <div id="szv2-np-vars" style="display:flex;flex-direction:column;gap:8px"></div>
                </div>
                <div id="szv2-np-err" style="display:none;color:var(--szv2-danger);font-size:13px"></div>
            </div>
            <div class="szv2-modal-foot">
                <button type="button" class="szv2-btn szv2-btn-secondary" onclick="document.getElementById('szv2-add-product-modal').style.display='none'">Cancelar</button>
                <button type="button" class="szv2-btn szv2-btn-brand"
                        data-ajax="<?php echo esc_attr( $sz9pr_ajax ); ?>"
                        data-nonce="<?php echo esc_attr( $sz9pr_nonce ); ?>"
                        data-action="submit-new-product">Enviar para aprovação</button>
            </div>
        </div>
    </div>
    <?php endif; ?>
</section>