<?php
/**
 * Senderzz V2 – Motoboy (v455 funcional)
 * Modal de detalhes com olho, reagendamento com 3 dias úteis em botões.
 * @var object $sz_v2_user
 */
defined( 'ABSPATH' ) || exit;

global $wpdb;

$sz4mb_rest_base  = esc_url( rest_url( 'sz-portal/v2' ) );
$sz4mb_rest_nonce = wp_create_nonce( 'wp_rest' );
$sz4mb_nonce      = wp_create_nonce( 'senderzz_portal' );
$sz4mb_ajax       = admin_url( 'admin-ajax.php' );

$sz4mb_is_aff = function_exists( 'sz_aff_portal_user_must_use_affiliate_scope' )
    && sz_aff_portal_user_must_use_affiliate_scope( $sz_v2_user );
$sz4mb_can_act    = ! $sz4mb_is_aff && empty( $sz_v2_user->parent_user_id );

$sz4mb_orders = function_exists( 'senderzz_get_visible_orders_for_user' )
    ? array_filter( senderzz_get_visible_orders_for_user( $sz_v2_user, 200 ), static function ( $o ) {
        return in_array( (string) ( $o['status'] ?? '' ), [
            'agendado','embalado','acaminho','em_rota','emrota','entregue',
            'frustrado','devolvido','avariado','cancelado','coletado','asuspender',
        ], true )
            || strpos( (string) ( $o['delivery_mode'] ?? '' ), 'motoboy' ) !== false
            || ! empty( $o['motoboy_status'] );
    } )
    : [];

// Contagem por status + opções únicas para filtros
$sz4mb_counts       = [];
$sz4mb_f_base_prods = []; // produto WC base (agrupa todas as ofertas)
$sz4mb_f_offers     = []; // oferta específica (senderzz_offer_name)
$sz4mb_f_affs       = [];
foreach ( $sz4mb_orders as $sz4mb_co ) {
    $s = (string) ( $sz4mb_co['status'] ?? '' );
    $sz4mb_counts[ $s ] = ( $sz4mb_counts[ $s ] ?? 0 ) + 1;
    // Produto base: primeiro item WC do pedido (sem branding de oferta)
    $sz4mb_items = (array) ( $sz4mb_co['items'] ?? [] );
    $sz4mb_base_p = count( $sz4mb_items ) > 0
        ? (string) ( $sz4mb_items[0]['name'] ?? '' )
        : (string) ( $sz4mb_co['product_name'] ?? '' );
    if ( $sz4mb_base_p ) $sz4mb_f_base_prods[ $sz4mb_base_p ] = $sz4mb_base_p;
    // Oferta: senderzz_offer_name
    $sz4mb_offer = (string) ( $sz4mb_co['senderzz_offer_name'] ?? '' );
    if ( $sz4mb_offer ) $sz4mb_f_offers[ $sz4mb_offer ] = $sz4mb_offer;
    $a = (string) ( $sz4mb_co['affiliate_name'] ?? '' );
    if ( $a ) $sz4mb_f_affs[ $a ] = $a;
}
$sz4mb_total = count( $sz4mb_orders );
ksort( $sz4mb_f_base_prods );
ksort( $sz4mb_f_offers );
ksort( $sz4mb_f_affs );

$sz4mb_fmt_phone = static function( string $phone ): string {
    $d = preg_replace( '/\D+/', '', $phone );
    // Strip country code +55
    if ( ( strlen( $d ) === 13 || strlen( $d ) === 12 ) && substr( $d, 0, 2 ) === '55' ) {
        $d = substr( $d, 2 );
    }
    if ( strlen( $d ) === 11 ) return '(' . substr( $d, 0, 2 ) . ') ' . substr( $d, 2, 5 ) . '-' . substr( $d, 7 );
    if ( strlen( $d ) === 10 ) return '(' . substr( $d, 0, 2 ) . ') ' . substr( $d, 2, 4 ) . '-' . substr( $d, 6 );
    return trim( $phone ) !== '' ? $phone : '—';
};

// Próximos 3 dias úteis
$sz4mb_day_names_pt = ['Monday'=>'Segunda-feira','Tuesday'=>'Terça-feira','Wednesday'=>'Quarta-feira','Thursday'=>'Quinta-feira','Friday'=>'Sexta-feira'];
$sz4mb_biz_days = [];
$sz4mb_day = new DateTime( 'tomorrow', wp_timezone() );
while ( count( $sz4mb_biz_days ) < 3 ) {
    $wd = (int) $sz4mb_day->format( 'N' );
    if ( $wd < 6 ) {
        $sz4mb_en_day = $sz4mb_day->format( 'l' );
        $sz4mb_biz_days[] = [
            'value' => $sz4mb_day->format( 'Y-m-d' ),
            'label' => ( $sz4mb_day_names_pt[ $sz4mb_en_day ] ?? $sz4mb_en_day ) . ', ' . $sz4mb_day->format( 'd/m' ),
        ];
    }
    $sz4mb_day->modify( '+1 day' );
}
?>
<section id="sec-motoboy" class="sz-sec" data-szv2-label="Cash On Delivery"
         data-szv2-ajax-url="<?php echo esc_attr( $sz4mb_ajax ); ?>"
         data-nonce="<?php echo esc_attr( $sz4mb_nonce ); ?>">

    <?php if ( empty( $sz4mb_orders ) ) : ?>
        <?php // phpcs:ignore WordPress.Security.EscapeOutput
        echo sz_v2_empty_state( [ 'title' => 'Nenhum pedido COD', 'text' => 'Os pedidos Cash on Delivery aparecem aqui assim que chegarem.' ] ); ?>
    <?php else : ?>

    <!-- Filtros + chips -->
    <div style="margin-bottom:18px">

        <!-- Chips de status (igual ao mockup: pills com contagem) -->
        <div style="display:flex;gap:6px;flex-wrap:wrap;align-items:center;margin-bottom:12px">
            <?php
            $sz4mb_chip_defs = [
                ''         => 'Todos',
                'agendado' => 'Agendado',
                'embalado' => 'Embalado',
                'acaminho' => 'A caminho',
                'em_rota'  => 'Em rota',
                'entregue' => 'Entregue',
                'frustrado'=> 'Frustrado',
            ];
            foreach ( $sz4mb_chip_defs as $sz4mb_chip_key => $sz4mb_chip_label ) :
                $sz4mb_chip_n = $sz4mb_chip_key === '' ? $sz4mb_total : ( $sz4mb_counts[ $sz4mb_chip_key ] ?? 0 );
                if ( $sz4mb_chip_key !== '' && $sz4mb_chip_n === 0 ) continue;
                $sz4mb_chip_active = $sz4mb_chip_key === '';
            ?>
            <button type="button"
                    class="szv2-chip szv2-filter-chip <?php echo $sz4mb_chip_active ? 'szv2-chip-active' : ''; ?>"
                    data-mb-filter="<?php echo esc_attr( $sz4mb_chip_key ); ?>"
                    onclick="szV2MbChipFilter(this)">
                <?php echo esc_html( $sz4mb_chip_label ); ?>
                <span class="szv2-chip-count"><?php echo esc_html( (string) $sz4mb_chip_n ); ?></span>
            </button>
            <?php endforeach; ?>
        </div>

        <!-- Barra de busca + filtros -->
        <div style="display:flex;gap:8px;align-items:center;flex-wrap:wrap">
            <input type="text" class="szv2-input" id="szv2-mb-search"
                   placeholder="Buscar pedido, cliente…"
                   autocomplete="off" autocorrect="off" autocapitalize="off" spellcheck="false"
                   name="sz_search_<?php echo esc_attr( wp_generate_uuid4() ); ?>"
                   oninput="szV2MbFilter()"
                   style="flex:1;min-width:200px;height:38px;font-size:13px">
            <select class="szv2-input" id="szv2-mb-f-base-prod" onchange="szV2MbFilter()"
                    style="height:38px;font-size:13px;min-width:130px;max-width:180px">
                <option value="">Produto</option>
                <?php foreach ( $sz4mb_f_base_prods as $v ) : ?>
                <option value="<?php echo esc_attr( $v ); ?>"><?php echo esc_html( $v ); ?></option>
                <?php endforeach; ?>
            </select>
            <select class="szv2-input" id="szv2-mb-f-offer" onchange="szV2MbFilter()"
                    style="height:38px;font-size:13px;min-width:130px;max-width:180px">
                <option value="">Oferta</option>
                <?php foreach ( $sz4mb_f_offers as $v ) : ?>
                <option value="<?php echo esc_attr( $v ); ?>"><?php echo esc_html( $v ); ?></option>
                <?php endforeach; ?>
            </select>
            <select class="szv2-input" id="szv2-mb-f-aff" onchange="szV2MbFilter()"
                    style="height:38px;font-size:13px;min-width:120px;max-width:160px">
                <option value="">Afiliado</option>
                <?php foreach ( $sz4mb_f_affs as $v ) : ?>
                <option value="<?php echo esc_attr( $v ); ?>"><?php echo esc_html( $v ); ?></option>
                <?php endforeach; ?>
            </select>
            <input type="date" class="szv2-input" id="szv2-mb-f-data-de"
                   placeholder="Entrega de"
                   onchange="szV2MbFilter()"
                   style="height:38px;font-size:13px;min-width:140px">
            <input type="date" class="szv2-input" id="szv2-mb-f-data-ate"
                   placeholder="Entrega até"
                   onchange="szV2MbFilter()"
                   style="height:38px;font-size:13px;min-width:140px">
            <button type="button" onclick="szV2MbExportCSV()" class="szv2-btn szv2-btn-secondary" style="height:38px;padding:0 14px;font-size:13px;white-space:nowrap;flex-shrink:0">↓ CSV</button>
        </div>
    </div>

    <div id="szv2-mb-alert-atrasados" style="display:none;margin-bottom:12px;padding:10px 14px;background:#fff7ed;border:1.5px solid #fed7aa;border-radius:9px;color:#92400e;font-size:13px;font-weight:600">
        ⚠ <span></span> <a href="#" onclick="document.getElementById('szv2-mb-f-entrega').value='atrasado';szV2MbFilter();return false" style="color:#EA580C;text-decoration:underline;font-weight:700">Ver atrasados</a>
    </div>

    <div class="szv2-card" style="padding:0;overflow:hidden">
        <div class="szv2-table-wrap">
            <table class="szv2-table" id="szv2-mb-table" style="width:100%;border-collapse:collapse">
                <thead>
                    <tr>
                        <th style="white-space:nowrap">PEDIDO</th>
                        <th>CLIENTE</th>
                        <th>PRODUTO</th>
                        <th>STATUS</th>
                        <th style="text-align:right;white-space:nowrap">VALOR</th>
                        <th style="text-align:right;white-space:nowrap">COMISSÃO</th>
                        <th style="white-space:nowrap">DATA PEDIDO</th>
                        <th style="white-space:nowrap">ENTREGA</th>
                        <th style="text-align:right;white-space:nowrap">AÇÕES</th>
                    </tr>
                </thead>
                <tbody>
                    <?php foreach ( $sz4mb_orders as $sz4mb_o ) :
                        $sz4mb_id       = (int) ( $sz4mb_o['id'] ?? 0 );
                        $sz4mb_num      = (string) ( $sz4mb_o['number'] ?? $sz4mb_id );
                        $sz4mb_status   = (string) ( $sz4mb_o['status'] ?? '' );
                        $sz4mb_client   = (string) ( $sz4mb_o['billing']['name'] ?? '—' );
                        $sz4mb_phone    = $sz4mb_fmt_phone( (string) ( $sz4mb_o['billing']['phone'] ?? '' ) );
                        $sz4mb_product    = (string) ( $sz4mb_o['senderzz_offer_name'] ?? $sz4mb_o['product_name'] ?? '—' );
                        $sz4mb_offer_name = (string) ( $sz4mb_o['senderzz_offer_name'] ?? '' );
                        $sz4mb_items_arr  = (array) ( $sz4mb_o['items'] ?? [] );
                        $sz4mb_base_prod  = count( $sz4mb_items_arr ) > 0
                            ? (string) ( $sz4mb_items_arr[0]['name'] ?? $sz4mb_product )
                            : $sz4mb_product;
                        $sz4mb_total    = (string) ( $sz4mb_o['total_no_ship'] ?? ( is_callable( 'wc_price' ) ? wc_price( (float) ( $sz4mb_o['total_no_ship_raw'] ?? 0 ) ) : '—' ) );
                        $sz4mb_date_raw = (string) ( $sz4mb_o['date_machine'] ?? $sz4mb_o['date'] ?? '' );
                        $sz4mb_date     = $sz4mb_date_raw ? wp_date( 'd/m/Y', strtotime( $sz4mb_date_raw ) ) : '—';
                        $sz4mb_addr     = (string) ( $sz4mb_o['shipping']['address'] ?? $sz4mb_o['billing']['address'] ?? '' );
                        $sz4mb_cpf      = (string) ( $sz4mb_o['billing_cpf'] ?? '' );
                        $sz4mb_email    = (string) ( $sz4mb_o['billing']['email'] ?? '' );
                        // UTM params do pedido WC
                        $sz4mb_utm = [];
                        foreach ( [ 'utm_source', 'utm_medium', 'utm_campaign', 'utm_content', 'utm_term' ] as $_utm_k ) {
                            $v = '';
                            if ( function_exists( 'wc_get_order' ) ) {
                                $v = (string) get_post_meta( $sz4mb_id, '_sz_' . $_utm_k, true );
                            }
                            if ( $v !== '' ) $sz4mb_utm[ $_utm_k ] = $v;
                        }
                        // agendado/embalado → cancela E reagenda; frustrado/cancelado → só reagenda
                        $sz4mb_can_cancel  = $sz4mb_can_act && in_array( $sz4mb_status, [ 'agendado', 'embalado' ], true );
                        $sz4mb_can_resched = $sz4mb_can_act && in_array( $sz4mb_status, [ 'agendado', 'embalado', 'frustrado', 'cancelado' ], true );
                        // Complemento > 32 chars + data de entrega: busca do pedido motoboy
                        $sz4mb_motoboy_row = null;
                        $sz4mb_complement_flag = false;
                        $sz4mb_delivery_date = '';
                        if ( $sz4mb_can_act ) {
                            $sz4mb_motoboy_row = $wpdb->get_row( $wpdb->prepare(
                                "SELECT id, motoboy_id, dest_complemento, valor_pedido, reagendado_para FROM {$wpdb->prefix}sz_motoboy_pedidos WHERE wc_order_id = %d LIMIT 1",
                                $sz4mb_id
                            ) );
                            if ( $sz4mb_motoboy_row ) {
                                if ( mb_strlen( (string) $sz4mb_motoboy_row->dest_complemento ) > 32 ) {
                                    $sz4mb_complement_flag = true;
                                }
                                if ( ! empty( $sz4mb_motoboy_row->reagendado_para ) ) {
                                    $sz4mb_delivery_date = wp_date( 'd/m/Y', strtotime( $sz4mb_motoboy_row->reagendado_para ) );
                                }
                            }
                        }
                        $sz4mb_meta_date = '';
                        if ( ! $sz4mb_delivery_date ) {
                            // Usar camada canônica (HPOS-safe) quando disponível
                            if ( class_exists( 'Senderzz_Order_Meta' ) ) {
                                $sz4mb_meta_date = Senderzz_Order_Meta::get_delivery_date( $sz4mb_id );
                            } else {
                                foreach ( [ '_senderzz_delivery_date', '_sz_delivery_date', '_sz_motoboy_entrega_data', 'delivery_date' ] as $_dk ) {
                                    $sz4mb_meta_date = get_post_meta( $sz4mb_id, $_dk, true );
                                    if ( $sz4mb_meta_date ) break;
                                }
                            }
                            if ( $sz4mb_meta_date ) {
                                $sz4mb_delivery_date = wp_date( 'd/m/Y', strtotime( (string) $sz4mb_meta_date ) );
                            }
                        }
                        // Afiliado: somente se o pedido tem afiliado
                        $sz4mb_seller     = (string) ( $sz4mb_o['affiliate_name'] ?? '' );
                        // Financeiro: bruto, taxas, líquido do produtor
                        // produtor = bruto - aff_BRUTO - taxas (não usar aff líquido, pois a taxa do afiliado não reverte ao produtor)
                        $sz4mb_bruto_raw  = (float) ( $sz4mb_o['total_no_ship_raw'] ?? $sz4mb_o['total_raw'] ?? 0 );
                        $sz4mb_taxa_raw   = (float) ( $sz4mb_o['mb_taxa_total'] ?? 0 );
                        $sz4mb_comm_raw   = (float) ( $sz4mb_o['affiliate_commission'] ?? 0 );       // NET afiliado (display)
                        $sz4mb_aff_gross  = (float) ( $sz4mb_o['affiliate_commission_gross'] ?? $sz4mb_comm_raw ); // BRUTO afiliado
                        $sz4mb_liq_raw    = max( 0, $sz4mb_bruto_raw - $sz4mb_taxa_raw - $sz4mb_aff_gross );
                        $sz4mb_fmt_r      = static fn( float $v ): string => 'R$ ' . number_format( $v, 2, ',', '.' );
                        $sz4mb_mb_pedido_id = $sz4mb_motoboy_row ? (int) $sz4mb_motoboy_row->id : 0;
                        $sz4mb_mb_motoboy   = $sz4mb_motoboy_row ? (int) ( $sz4mb_motoboy_row->motoboy_id ?? 0 ) : 0;
                    ?>
                    <?php
                        $sz4mb_date_iso = (string) ( $sz4mb_o['date_machine'] ?? '' );
                        $sz4mb_date_iso_short = $sz4mb_date_iso ? substr( $sz4mb_date_iso, 0, 10 ) : '';
                        $sz4mb_entrega_iso = '';
                        if ( ! empty( $sz4mb_motoboy_row->reagendado_para ) ) {
                            $sz4mb_entrega_iso = substr( $sz4mb_motoboy_row->reagendado_para, 0, 10 );
                        } elseif ( $sz4mb_meta_date ) {
                            $sz4mb_entrega_iso = substr( $sz4mb_meta_date, 0, 10 );
                        }
                    ?>
                    <tr data-search="<?php echo esc_attr( strtolower( $sz4mb_num . ' ' . $sz4mb_client . ' ' . $sz4mb_product . ' ' . $sz4mb_seller ) ); ?>"
                        data-status="<?php echo esc_attr( $sz4mb_status ); ?>"
                        data-date="<?php echo esc_attr( $sz4mb_date_iso_short ); ?>"
                        data-product="<?php echo esc_attr( $sz4mb_product ); ?>"
                        data-base-product="<?php echo esc_attr( $sz4mb_base_prod ); ?>"
                        data-offer="<?php echo esc_attr( $sz4mb_offer_name ); ?>"
                        data-aff="<?php echo esc_attr( $sz4mb_seller ); ?>"
                        data-entrega-iso="<?php echo esc_attr( $sz4mb_entrega_iso ); ?>"
                        style="transition:background .08s<?php echo $sz4mb_complement_flag ? ';background:rgba(234,88,12,.04)' : ''; ?>">
                        <!-- PEDIDO -->
                        <td style="white-space:nowrap">
                            <span style="font-weight:700;font-size:13px;color:var(--szv2-text)"><?php echo esc_html( $sz4mb_num ); ?></span>
                            <?php if ( $sz4mb_complement_flag ) : ?>
                            <span title="Complemento longo: <?php echo esc_attr( (string) $sz4mb_motoboy_row->dest_complemento ); ?>"
                                  style="display:inline-block;width:7px;height:7px;border-radius:50%;background:var(--szv2-brand,#EA580C);vertical-align:middle;margin-left:4px"></span>
                            <?php endif; ?>
                        </td>
                        <!-- CLIENTE -->
                        <td>
                            <div style="font-weight:600;font-size:13px;color:var(--szv2-text,#111827)"><?php echo esc_html( $sz4mb_client ); ?></div>
                            <?php if ( $sz4mb_phone !== '—' && $sz4mb_phone !== '' ) : ?>
                            <div style="font-size:11px;color:var(--szv2-text-muted);margin-top:1px"><?php echo esc_html( $sz4mb_phone ); ?></div>
                            <?php endif; ?>
                            <?php if ( $sz4mb_seller !== '' ) : ?>
                            <div style="font-size:11px;margin-top:2px"><span style="padding:1px 6px;background:rgba(234,88,12,.08);color:var(--szv2-brand,#EA580C);border-radius:99px;font-weight:600"><?php echo esc_html( $sz4mb_seller ); ?></span></div>
                            <?php endif; ?>
                        </td>
                        <!-- PRODUTO -->
                        <td style="font-size:13px;color:var(--szv2-text-soft,#374151);max-width:200px">
                            <div style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="<?php echo esc_attr( $sz4mb_product ); ?>"><?php echo esc_html( $sz4mb_product ); ?></div>
                        </td>
                        <!-- STATUS -->
                        <td><?php // phpcs:ignore WordPress.Security.EscapeOutput
                            echo sz_v2_status_badge( $sz4mb_status ); ?></td>
                        <!-- VALOR (bruto) -->
                        <td style="text-align:right;white-space:nowrap">
                            <span style="font-weight:700;font-size:13px;color:var(--szv2-text,#111827)"><?php echo esc_html( $sz4mb_fmt_r( $sz4mb_bruto_raw ) ); ?></span>
                        </td>
                        <!-- LÍQUIDO -->
                        <td style="text-align:right;white-space:nowrap;font-size:13px">
                            <span style="font-weight:700;color:var(--szv2-success,#16a34a)"><?php echo esc_html( $sz4mb_fmt_r( $sz4mb_liq_raw ) ); ?></span>
                        </td>
                        <!-- DATA PEDIDO -->
                        <td style="white-space:nowrap;font-size:13px;color:var(--szv2-text-soft,#374151)"><?php echo esc_html( $sz4mb_date ); ?></td>
                        <!-- ENTREGA -->
                        <td style="white-space:nowrap;font-size:13px;color:var(--szv2-text-soft,#374151)">
                            <?php if ( $sz4mb_delivery_date ) : ?>
                                <span style="color:var(--szv2-brand,#EA580C);font-weight:600"><?php echo esc_html( $sz4mb_delivery_date ); ?></span>
                            <?php else : ?>
                                <span style="color:var(--szv2-text-faint,#94a3b8)">—</span>
                            <?php endif; ?>
                        </td>
                        <!-- AÇÕES: select dropdown + olho -->
                        <td style="text-align:right;white-space:nowrap">
                            <div style="display:flex;gap:6px;justify-content:flex-end;align-items:center">
                                <?php
                                $sz4mb_has_actions = $sz4mb_can_cancel || $sz4mb_can_resched;
                                if ( $sz4mb_has_actions ) :
                                ?>
                                <select class="szv2-input"
                                        style="height:30px;font-size:12px;padding:0 6px;min-width:100px;cursor:pointer"
                                        onchange="szV2MbActionSelect(this)"
                                        data-order-id="<?php echo esc_attr( (string) $sz4mb_id ); ?>"
                                        data-order-num="<?php echo esc_attr( $sz4mb_num ); ?>"
                                        data-nonce="<?php echo esc_attr( $sz4mb_nonce ); ?>"
                                        data-rest-nonce="<?php echo esc_attr( $sz4mb_rest_nonce ); ?>"
                                        data-rest-base="<?php echo esc_attr( $sz4mb_rest_base ); ?>"
                                        data-can-cancel="<?php echo esc_attr( $sz4mb_can_cancel ? '1' : '0' ); ?>"
                                        data-can-resched="<?php echo esc_attr( $sz4mb_can_resched ? '1' : '0' ); ?>">
                                    <option value="">Ação…</option>
                                    <?php if ( $sz4mb_can_resched ) : ?>
                                    <option value="resched">Reagendar</option>
                                    <?php endif; ?>
                                    <?php if ( $sz4mb_can_cancel ) : ?>
                                    <option value="cancel">Cancelar</option>
                                    <?php endif; ?>
                                </select>
                                <?php else : ?>
                                <span style="font-size:12px;color:var(--szv2-text-faint)">—</span>
                                <?php endif; ?>
                                <!-- Olho: detalhes completos -->
                                <button type="button"
                                        class="szv2-mb-detail-btn"
                                        title="Ver detalhes"
                                        data-order-id="<?php echo esc_attr( (string) $sz4mb_id ); ?>"
                                        data-mb-pedido-id="<?php echo esc_attr( (string) $sz4mb_mb_pedido_id ); ?>"
                                        data-mb-motoboy-id="<?php echo esc_attr( (string) $sz4mb_mb_motoboy ); ?>"
                                        data-order-num="<?php echo esc_attr( $sz4mb_num ); ?>"
                                        data-seller="<?php echo esc_attr( $sz4mb_seller ); ?>"
                                        data-delivery-date="<?php echo esc_attr( $sz4mb_delivery_date ); ?>"
                                        data-order-date="<?php echo esc_attr( $sz4mb_date ); ?>"
                                        data-client="<?php echo esc_attr( $sz4mb_client ); ?>"
                                        data-phone="<?php echo esc_attr( $sz4mb_phone ); ?>"
                                        data-email="<?php echo esc_attr( $sz4mb_email ); ?>"
                                        data-cpf="<?php echo esc_attr( $sz4mb_cpf ); ?>"
                                        data-product="<?php echo esc_attr( $sz4mb_product ); ?>"
                                        data-total="<?php echo esc_attr( wp_strip_all_tags( $sz4mb_total ) ); ?>"
                                        data-bruto="<?php echo esc_attr( $sz4mb_fmt_r( $sz4mb_bruto_raw ) ); ?>"
                                        data-taxa="<?php echo esc_attr( $sz4mb_fmt_r( $sz4mb_taxa_raw ) ); ?>"
                                        data-comm="<?php echo esc_attr( $sz4mb_fmt_r( $sz4mb_comm_raw ) ); ?>"
                                        data-liq="<?php echo esc_attr( $sz4mb_fmt_r( $sz4mb_liq_raw ) ); ?>"
                                        data-addr="<?php echo esc_attr( $sz4mb_addr ); ?>"
                                        data-complement="<?php echo esc_attr( (string)($sz4mb_motoboy_row->dest_complemento ?? '') ); ?>"
                                        data-status="<?php echo esc_attr( $sz4mb_status ); ?>"
                                        data-can-resched="<?php echo esc_attr( $sz4mb_can_resched ? '1' : '0' ); ?>"
                                        data-utm="<?php echo esc_attr( wp_json_encode( $sz4mb_utm ) ); ?>"
                                        data-order-id-resched="<?php echo esc_attr( (string) $sz4mb_id ); ?>"
                                        data-rest-base="<?php echo esc_attr( $sz4mb_rest_base ); ?>"
                                        data-rest-nonce="<?php echo esc_attr( $sz4mb_rest_nonce ); ?>"
                                        onclick="szV2MbDetail(this)"
                                        style="background:none;border:none;cursor:pointer;color:#9ca3af;padding:2px;display:inline-flex;align-items:center">
                                    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>
                                </button>
                            </div>
                        </td>
                    </tr>
                    <?php endforeach; ?>
                </tbody>
            </table>
        </div>
        <div style="padding:10px 16px;border-top:1px solid var(--szv2-border);font-size:13px;color:var(--szv2-text-muted)">
            <span id="szv2-mb-foot-count">Mostrando <?php echo esc_html( (string) count( $sz4mb_orders ) ); ?> pedidos Motoboy</span>
        </div>
    </div>

    <?php endif; ?>

    <!-- Modal: Detalhes do pedido (OL) -->
    <?php
    $sz4mb_cc_fee = (float) get_option( 'sz_motoboy_cc_fee_pct', 0.0 );
    ?>
    <div id="szv2-mb-detail-modal" class="szv2-modal-overlay" style="display:none" role="dialog" aria-modal="true">
        <div class="szv2-modal" style="width:560px;max-width:96vw">
            <div class="szv2-modal-head">
                <h3>Pedido <span id="szv2-detail-num"></span></h3>
                <button type="button" class="szv2-modal-x" onclick="document.getElementById('szv2-mb-detail-modal').style.display='none'">×</button>
            </div>
            <!-- Tabs do modal -->
            <div style="display:flex;border-bottom:1px solid var(--szv2-divider);padding:0 20px;flex-shrink:0">
                <button type="button" id="szv2-detail-tab-info" onclick="szV2MbDetailTab('info')"
                        style="background:none;border:none;border-bottom:2px solid var(--szv2-brand);padding:10px 16px 8px;font-size:13px;font-weight:700;color:var(--szv2-brand);cursor:pointer;margin-bottom:-1px">Informações</button>
                <button type="button" id="szv2-detail-tab-hist" onclick="szV2MbDetailTab('hist')"
                        style="background:none;border:none;border-bottom:2px solid transparent;padding:10px 16px 8px;font-size:13px;font-weight:600;color:var(--szv2-text-muted);cursor:pointer;margin-bottom:-1px">Histórico</button>
            </div>
            <div class="szv2-modal-body" id="szv2-detail-body-info">

                <!-- ── Seção: Cliente ──────────────────────────────── -->
                <p style="font-size:10px;font-weight:700;color:var(--szv2-text-muted);text-transform:uppercase;letter-spacing:.06em;margin:0 0 8px">Cliente</p>
                <div class="szv2-detail-grid" style="margin-bottom:16px">
                    <div class="szv2-detail-row">
                        <span class="szv2-detail-label">Data do pedido</span>
                        <span id="szv2-detail-order-date" class="szv2-detail-val" style="font-weight:600"></span>
                    </div>
                    <div class="szv2-detail-row">
                        <span class="szv2-detail-label">Data de entrega</span>
                        <span id="szv2-detail-delivery" class="szv2-detail-val" style="color:var(--szv2-brand);font-weight:600"></span>
                    </div>
                    <div class="szv2-detail-row"><span class="szv2-detail-label">CPF</span><span id="szv2-detail-cpf" class="szv2-detail-val szv2-num"></span></div>
                    <div class="szv2-detail-row"><span class="szv2-detail-label">Endereço</span><span id="szv2-detail-addr" class="szv2-detail-val"></span></div>
                    <div class="szv2-detail-row" id="szv2-detail-complement-row" style="display:none">
                        <span class="szv2-detail-label" style="color:var(--szv2-brand)">⚠ Complemento</span>
                        <span id="szv2-detail-complement" class="szv2-detail-val" style="color:var(--szv2-brand);font-weight:600"></span>
                    </div>
                </div>

                <!-- ── Seção: Afiliado ─────────────────────────────── -->
                <div id="szv2-detail-seller-wrap" style="display:none;margin-bottom:16px;padding:10px 12px;background:var(--szv2-brand-light);border-radius:8px;border:1px solid var(--szv2-border)">
                    <p style="font-size:10px;font-weight:700;color:var(--szv2-text-muted);text-transform:uppercase;letter-spacing:.06em;margin:0 0 6px">Afiliado</p>
                    <div style="font-size:14px;font-weight:700;color:var(--szv2-brand)" id="szv2-detail-seller"></div>
                </div>

                <!-- ── Seção: Financeiro ───────────────────────────── -->
                <p style="font-size:10px;font-weight:700;color:var(--szv2-text-muted);text-transform:uppercase;letter-spacing:.06em;margin:0 0 8px">Financeiro</p>
                <div style="margin-bottom:16px;padding:12px 14px;background:var(--szv2-surface-alt);border-radius:10px;border:1px solid var(--szv2-border);display:grid;grid-template-columns:1fr 1fr;gap:10px">
                    <div>
                        <div style="font-size:10px;color:var(--szv2-text-muted);font-weight:600;text-transform:uppercase;letter-spacing:.04em;margin-bottom:3px">Valor bruto</div>
                        <div id="szv2-detail-bruto" style="font-size:15px;font-weight:700;color:var(--szv2-text)"></div>
                    </div>
                    <div>
                        <div style="font-size:10px;color:var(--szv2-text-muted);font-weight:600;text-transform:uppercase;letter-spacing:.04em;margin-bottom:3px">Taxas Senderzz</div>
                        <div id="szv2-detail-taxa" style="font-size:15px;font-weight:700;color:var(--szv2-danger)"></div>
                    </div>
                    <div>
                        <div style="font-size:10px;color:var(--szv2-text-muted);font-weight:600;text-transform:uppercase;letter-spacing:.04em;margin-bottom:3px">Comissão afiliado</div>
                        <div id="szv2-detail-comm" style="font-size:15px;font-weight:700;color:var(--szv2-text-muted)"></div>
                    </div>
                    <div>
                        <div style="font-size:10px;color:var(--szv2-text-muted);font-weight:600;text-transform:uppercase;letter-spacing:.04em;margin-bottom:3px">Valor líquido</div>
                        <div id="szv2-detail-liq" style="font-size:15px;font-weight:700;color:var(--szv2-success)"></div>
                    </div>
                    <?php if ( $sz4mb_cc_fee > 0 ) : ?>
                    <div style="grid-column:1/-1;border-top:1px solid var(--szv2-border);padding-top:8px">
                        <div style="font-size:10px;color:var(--szv2-text-muted);font-weight:600;text-transform:uppercase;letter-spacing:.04em;margin-bottom:3px">Com cartão (<?php echo esc_html( number_format( $sz4mb_cc_fee, 1 ) ); ?>% taxa)</div>
                        <div id="szv2-detail-total-cc" style="font-size:15px;font-weight:700;color:var(--szv2-text)"></div>
                    </div>
                    <?php endif; ?>
                </div>

                <!-- ── Seção: UTM (origem da venda) ───────────────── -->
                <div id="szv2-detail-utm-wrap" style="display:none;margin-bottom:16px">
                    <p style="font-size:10px;font-weight:700;color:var(--szv2-text-muted);text-transform:uppercase;letter-spacing:.06em;margin:0 0 8px">Origem da venda (UTM)</p>
                    <div id="szv2-detail-utm-content" style="padding:10px 12px;background:var(--szv2-surface-alt);border-radius:8px;border:1px solid var(--szv2-border)"></div>
                </div>

                <!-- ── Seção: Fiscal ──────────────────────────────── -->
                <div id="szv2-detail-fiscal-wrap" style="display:none;margin-bottom:16px">
                    <p style="font-size:10px;font-weight:700;color:var(--szv2-text-muted);text-transform:uppercase;letter-spacing:.06em;margin:0 0 8px">Fiscal</p>
                    <div id="szv2-detail-fiscal-content" style="padding:10px 12px;background:var(--szv2-surface-alt);border-radius:8px;border:1px solid var(--szv2-border);font-size:13px"></div>
                </div>
                <!-- Troca de motoboy e mudança de status: somente no painel OL -->
                <!-- Reagendamento (somente se elegível) -->
                <div id="szv2-detail-resched-wrap" style="display:none;margin-top:16px;padding-top:16px;border-top:1px solid var(--szv2-divider)">
                    <p style="font-size:12px;font-weight:600;color:var(--szv2-text-muted);margin-bottom:10px;text-transform:uppercase;letter-spacing:.04em">Reagendar entrega</p>
                    <p style="font-size:13px;color:var(--szv2-text-muted);margin-bottom:12px">Selecione um dos próximos dias úteis:</p>
                    <div class="szv2-resched-days" id="szv2-detail-resched-days">
                        <?php foreach ( $sz4mb_biz_days as $sz4mb_bd ) : ?>
                        <button type="button"
                                class="szv2-resched-day-btn"
                                data-date="<?php echo esc_attr( $sz4mb_bd['value'] ); ?>"
                                onclick="szV2MbDetailSelectDay(this)">
                            <?php echo esc_html( $sz4mb_bd['label'] ); ?>
                        </button>
                        <?php endforeach; ?>
                    </div>
                    <div id="szv2-detail-resched-err" style="display:none;margin-top:8px;color:var(--szv2-danger);font-size:13px"></div>
                </div>
            </div>
            <!-- Tab: Histórico -->
            <div class="szv2-modal-body" id="szv2-detail-body-hist" style="display:none">
                <div id="szv2-detail-hist-content" style="min-height:120px;display:flex;align-items:center;justify-content:center;color:var(--szv2-text-muted);font-size:13px">
                    Carregando histórico…
                </div>
                <!-- UTM params -->
                <div id="szv2-detail-utm-wrap" style="display:none;margin-top:16px;padding-top:16px;border-top:1px solid var(--szv2-divider)">
                    <p style="font-size:11px;font-weight:700;color:var(--szv2-text-muted);text-transform:uppercase;letter-spacing:.05em;margin-bottom:8px">Origem da venda (UTM)</p>
                    <div id="szv2-detail-utm-content"></div>
                </div>
            </div>
            <div class="szv2-modal-foot">
                <button type="button" onclick="szV2MbWhatsApp()"
                  style="height:38px;padding:0 14px;border-radius:9px;border:none;background:#25D366;color:#fff;font-size:13px;font-weight:700;cursor:pointer;display:inline-flex;align-items:center;gap:6px">
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor"><path d="M17.472 14.382c-.297-.149-1.758-.867-2.03-.967-.273-.099-.471-.148-.67.15-.197.297-.767.966-.94 1.164-.173.199-.347.223-.644.075-.297-.15-1.255-.463-2.39-1.475-.883-.788-1.48-1.761-1.653-2.059-.173-.297-.018-.458.13-.606.134-.133.298-.347.446-.52.149-.174.198-.298.298-.497.099-.198.05-.371-.025-.52-.075-.149-.669-1.612-.916-2.207-.242-.579-.487-.5-.669-.51-.173-.008-.371-.01-.57-.01-.198 0-.52.074-.792.372-.272.297-1.04 1.016-1.04 2.479 0 1.462 1.065 2.875 1.213 3.074.149.198 2.096 3.2 5.077 4.487.709.306 1.262.489 1.694.625.712.227 1.36.195 1.871.118.571-.085 1.758-.719 2.006-1.413.248-.694.248-1.289.173-1.413-.074-.124-.272-.198-.57-.347z"/><path d="M11.999 2C6.477 2 2 6.477 2 12c0 1.99.584 3.842 1.587 5.4L2 22l4.765-1.548A9.945 9.945 0 0012 22c5.523 0 10-4.477 10-10S17.523 2 12 2z" fill-rule="evenodd" clip-rule="evenodd"/></svg>
                  WhatsApp
                </button>
                <button type="button" class="szv2-btn szv2-btn-secondary" onclick="document.getElementById('szv2-mb-detail-modal').style.display='none'">Fechar</button>
                <button type="button" class="szv2-btn szv2-btn-brand" id="szv2-detail-resched-confirm-btn" style="display:none"
                        onclick="szV2MbDetailConfirmResched()">Confirmar reagendamento</button>
            </div>
        </div>
    </div>

    <script>
    /* Motoboy – filtros extras + exportar CSV */
    (function() {
        var _origFilter = window.szV2MbFilter;
        window.szV2MbFilter = function() {
            if (_origFilter) _origFilter();
            var dataDeEl  = document.getElementById('szv2-mb-f-data-de');
            var dataAteEl = document.getElementById('szv2-mb-f-data-ate');
            var dataDe    = dataDeEl  ? dataDeEl.value  : '';
            var dataAte   = dataAteEl ? dataAteEl.value : '';
            var atrasados = 0;
            var finalStatuses = ['entregue','cancelado','frustrado','devolvido'];
            var today = new Date(); today.setHours(0,0,0,0);
            document.querySelectorAll('#szv2-mb-table tbody tr').forEach(function(tr) {
                if (tr.style.display === 'none') return;
                // Entrega date range filter
                var iso = tr.getAttribute('data-entrega-iso') || '';
                if ((dataDe || dataAte) && iso) {
                    var dEnt = new Date(iso); dEnt.setHours(0,0,0,0);
                    if (dataDe) { var dDe = new Date(dataDe); dDe.setHours(0,0,0,0); if (dEnt < dDe) { tr.style.display = 'none'; return; } }
                    if (dataAte) { var dAte = new Date(dataAte); dAte.setHours(0,0,0,0); if (dEnt > dAte) { tr.style.display = 'none'; return; } }
                } else if ((dataDe || dataAte) && !iso) { tr.style.display = 'none'; return; }
                // Count delayed
                var status = tr.getAttribute('data-status') || '';
                if (iso && finalStatuses.indexOf(status) === -1) {
                    var d2 = new Date(iso); d2.setHours(0,0,0,0);
                    if (d2 < today) atrasados++;
                }
            });
            // Alerta de atrasados
            var alertEl = document.getElementById('szv2-mb-alert-atrasados');
            if (alertEl) {
                alertEl.style.display = atrasados > 0 ? '' : 'none';
                var sp = alertEl.querySelector('span');
                if (sp) sp.textContent = atrasados + ' pedido(s) com entrega em atraso.';
            }
            // Atualizar rodapé
            var foot = document.getElementById('szv2-mb-foot-count');
            if (foot) {
                var vis = document.querySelectorAll('#szv2-mb-table tbody tr:not([style*="display:none"])').length;
                foot.textContent = 'Mostrando ' + vis + ' pedidos Motoboy';
            }
        };
    })();

    window.szV2MbWhatsApp = function() {
        var num = document.getElementById('szv2-detail-num') ? document.getElementById('szv2-detail-num').textContent.trim() : '';
        var activBtn = document.querySelector('.szv2-mb-detail-btn[data-order-num="' + num + '"]');
        var phone = activBtn ? (activBtn.getAttribute('data-phone') || '') : '';
        var rawPhone = phone.replace(/\D/g, '');
        if (!rawPhone || rawPhone.length < 8) {
            if (window.szV2Toast) szV2Toast('Telefone não disponível.', 'warning');
            return;
        }
        var brPhone = rawPhone.startsWith('55') ? rawPhone : '55' + rawPhone;
        var msg = encodeURIComponent('Olá! Seu pedido #' + num + ' está confirmado. Em breve nossa equipe entrará em contato para agendar a entrega. 🚀');
        window.open('https://wa.me/' + brPhone + '?text=' + msg, '_blank');
    };

    window.szV2MbActionSelect = function(sel) {
        var val = sel.value;
        if (!val) return;
        sel.value = '';
        var orderId  = sel.getAttribute('data-order-id');
        var orderNum = sel.getAttribute('data-order-num');
        var nonce    = sel.getAttribute('data-nonce');
        var restNonce= sel.getAttribute('data-rest-nonce');
        var restBase = sel.getAttribute('data-rest-base');
        if (val === 'cancel') {
            // Reuse existing cancel logic via a temporary proxy btn
            var proxy = document.createElement('button');
            proxy.setAttribute('data-order-id', orderId);
            proxy.setAttribute('data-order-num', orderNum);
            proxy.setAttribute('data-nonce', nonce);
            proxy.setAttribute('data-rest-base', restBase);
            if (window.szV2MbCancel) { szV2MbCancel(proxy); }
        } else if (val === 'resched') {
            // Open resched modal via proxy
            var proxy2 = document.createElement('button');
            proxy2.setAttribute('data-order-id', orderId);
            proxy2.setAttribute('data-order-num', orderNum);
            proxy2.setAttribute('data-nonce', restNonce);
            proxy2.setAttribute('data-rest-nonce', restNonce);
            proxy2.setAttribute('data-rest-base', restBase);
            if (window.szV2MbOpenResched) { szV2MbOpenResched(proxy2); }
        }
    };

    window.szV2MbExportCSV = function() {
        var rows  = document.querySelectorAll('#szv2-mb-table tbody tr');
        var lines = [['Pedido','Cliente','Produto','Status','Valor','Líquido','Data Pedido','Entrega','Afiliado']];
        rows.forEach(function(tr) {
            if (tr.style.display === 'none') return;
            var cells   = tr.querySelectorAll('td');
            var pedido  = cells[0] ? cells[0].textContent.trim() : '';
            var client  = (cells[1] && cells[1].querySelector('div:first-child') ? cells[1].querySelector('div:first-child').textContent : '').trim();
            var product = cells[2] ? cells[2].textContent.trim() : '';
            var status  = cells[3] ? cells[3].textContent.trim() : '';
            var valor   = cells[4] ? (cells[4].querySelector('span') ? cells[4].querySelector('span').textContent.trim() : cells[4].textContent.trim()) : '';
            var liq     = cells[5] ? cells[5].textContent.trim() : '';
            var dataPed = cells[6] ? cells[6].textContent.trim() : '';
            var entrega = cells[7] ? cells[7].textContent.trim() : '';
            var aff     = tr.getAttribute('data-aff') || '';
            lines.push([pedido, client, product, status, valor, liq, dataPed, entrega, aff]);
        });
        var csv  = lines.map(function(r) { return r.map(function(c) { return '"'+String(c).replace(/"/g,'""')+'"'; }).join(','); }).join('\n');
        var blob = new Blob(['﻿'+csv], {type:'text/csv;charset=utf-8'});
        var a    = document.createElement('a');
        a.href   = URL.createObjectURL(blob);
        a.download = 'motoboy-' + new Date().toISOString().slice(0,10) + '.csv';
        a.click();
    };
    </script>

    <!-- Modal: Reagendamento (3 dias úteis em botões) -->
    <div id="szv2-mb-resched-modal" class="szv2-modal-overlay" style="display:none" role="dialog" aria-modal="true">
        <div class="szv2-modal">
            <div class="szv2-modal-head">
                <h3>Reagendar pedido <span id="szv2-resched-num"></span></h3>
                <button type="button" class="szv2-modal-x" onclick="document.getElementById('szv2-mb-resched-modal').style.display='none'">×</button>
            </div>
            <div class="szv2-modal-body">
                <p style="font-size:13px;color:var(--szv2-text-muted);margin-bottom:16px">Selecione um dos próximos dias úteis disponíveis para entrega:</p>
                <div class="szv2-resched-days">
                    <?php foreach ( $sz4mb_biz_days as $sz4mb_bd ) : ?>
                    <button type="button"
                            class="szv2-resched-day-btn"
                            data-date="<?php echo esc_attr( $sz4mb_bd['value'] ); ?>"
                            onclick="szV2MbSelectDay(this)">
                        <?php echo esc_html( $sz4mb_bd['label'] ); ?>
                    </button>
                    <?php endforeach; ?>
                </div>
                <div id="szv2-resched-err" style="display:none;margin-top:10px;color:var(--szv2-danger);font-size:13px"></div>
            </div>
            <div class="szv2-modal-foot">
                <button type="button" class="szv2-btn szv2-btn-secondary" onclick="document.getElementById('szv2-mb-resched-modal').style.display='none'">Cancelar</button>
                <button type="button" class="szv2-btn szv2-btn-brand" id="szv2-resched-confirm-btn"
                        onclick="szV2MbConfirmResched()">Confirmar reagendamento</button>
            </div>
        </div>
    </div>
</section>
