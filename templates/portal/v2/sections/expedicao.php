<?php
/**
 * Senderzz Dashboard V2 — Seção: Expedição (Fase 5 — ações operacionais)
 * -----------------------------------------------------------------------
 * Ações portadas (chamam backend existente da V1):
 *
 *   Aprovar (on-hold → aprovado)
 *     AJAX: action=senderzz_portal szaction=approve
 *     Handler: Portal_Orders::approve_order()
 *     Nonce: senderzz_portal
 *     Permissão: senderzz_user_can_access_order($id, $user, 'approve')
 *     Status origem: on-hold  |  Status destino: aprovado (ou saldoinsuficiente)
 *     Dispara: débito TPC via hook WC — idempotente (rejeita se não on-hold)
 *
 *   Cancelar (on-hold → cancelled | com etiqueta → emcancelamento)
 *     AJAX: action=senderzz_portal szaction=cancel
 *     Handler: Portal_Orders::cancel_order()
 *     Status origem: on-hold  |  Destino: cancelled / emcancelamento
 *     Dispara: estorno TPC, webhook, notificação (via hooks WC)
 *
 *   Reprocessar etiqueta (saldoinsuficiente / erro → aprovado)
 *     AJAX: action=senderzz_portal szaction=retry
 *     Handler: Portal_Orders::retry_label()
 *     Status origem: saldoinsuficiente, erro  |  Destino: aprovado
 *     Idempotente: não debita se _senderzz_wallet_debited=yes
 *
 *   Ver / copiar rastreio: client-side, sem AJAX — sem risco.
 *
 * Ações NÃO portadas:
 *   Gerar/comprar etiqueta — sem handler portal, exigiria endpoint novo
 *   Reagendar Expedição — endpoint REST só para motoboy
 *   Relatar perda/suspensão — status LOSS_STATUSES fora do fluxo normal de expedição
 *
 * Afiliado: acesso bloqueado (não vê Expedição se V1 proíbe).
 *
 * @var object $sz_v2_user
 * @var bool   $sz_v2_is_affiliate
 */
defined( 'ABSPATH' ) || exit;

//  Helper mb_strimwidth seguro
$sz3_trim = static function( string $s, int $len, string $suffix = '…' ): string {
    if ( $s === '' ) return '';
    if ( function_exists( 'mb_strimwidth' ) ) {
        return mb_strimwidth( $s, 0, $len, $suffix );
    }
    return strlen( $s ) > $len ? substr( $s, 0, $len - strlen( $suffix ) ) . $suffix : $s;
};

//  Permissão por pedido (v430)
// Fail-closed: sem função = false = sem botão.
$sz5ex_can_order_action = static function( int $order_id, object $user, string $action ): bool {
    if ( ! function_exists( 'senderzz_user_can_access_order' ) ) {
        return false;
    }
    return (bool) senderzz_user_can_access_order( $order_id, $user, $action );
};

//  Perfil / escopo
$sz5ex_is_aff = ( 'affiliate' === strtolower( trim( (string) ( $sz_v2_user->role ?? 'client' ) ) ) );
// Afiliado nunca vê Expedição — mesmo guard da V1
if ( $sz5ex_is_aff ) {
    echo sz_v2_empty_state( [ 'title' => 'Expedição não disponível', 'text' => 'Expedição não está disponível no seu perfil.' ] ); // phpcs:ignore
    return;
}
$sz5ex_can_act = true; // produtor; admin já filtrado pelo escopo de visibilidade

//  URLs e Nonces
$sz5ex_ajax_url   = admin_url( 'admin-ajax.php' );
$sz5ex_ajax_nonce = wp_create_nonce( 'senderzz_portal' );

//  Pedidos visíveis (escopo já aplicado)
$sz5ex_all = function_exists( 'senderzz_get_visible_orders_for_user' )
    ? senderzz_get_visible_orders_for_user( $sz_v2_user, 500 ) : [];

$sz5ex_is_mb = static function( array $o ): bool {
    if ( ( $o['delivery_mode'] ?? '' ) === 'motoboy' ) return true;
    $hay = strtolower( ( $o['shipping_name'] ?? '' ) . ' ' . ( $o['shipping_method'] ?? '' ) );
    return strpos( $hay, 'motoboy' ) !== false;
};
$sz5ex_orders = array_values( array_filter( $sz5ex_all, static function( array $o ) use ( $sz5ex_is_mb ): bool {
    return ! $sz5ex_is_mb( $o );
} ) );

//  Grupos de filtro (alinhados ao mockup V2)
$sz5ex_filter_groups = [
    'todos'     => [],
    'pendente'  => [ 'on-hold', 'pending', 'processing', 'aprovado', 'saldoinsuficiente', 'erro' ],
    'em_rota'   => [ 'enviado', 'emretirada', 'acaminho', 'em-rota', 'separado', 'embalado', 'coletado' ],
    'entregue'  => [ 'entregue', 'completed', 'completo' ],
    'cancelado' => [ 'cancelled', 'cancelado', 'emcancelamento' ],
];
$sz5ex_filter_labels = [
    'todos'    => 'Todos',
    'pendente' => 'Pendente',
    'em_rota'  => 'Em rota',
    'entregue' => 'Entregue',
    'cancelado'=> 'Cancelado',
];

// Contagem por grupo + opções únicas para filtros
$sz5ex_group_counts = array_fill_keys( array_keys( $sz5ex_filter_labels ), 0 );
$sz5ex_f_products   = [];
$sz5ex_f_affs       = [];
$sz5ex_f_offers     = [];

foreach ( $sz5ex_orders as $sz5ex__o ) {
    $sz5ex__s = strtolower( str_replace( 'wc-', '', (string) ( $sz5ex__o['status'] ?? '' ) ) );
    $sz5ex_group_counts['todos']++;
    foreach ( $sz5ex_filter_groups as $sz5ex__gk => $sz5ex__gl ) {
        if ( $sz5ex__gk !== 'todos' && in_array( $sz5ex__s, $sz5ex__gl, true ) ) {
            $sz5ex_group_counts[ $sz5ex__gk ]++;
            break;
        }
    }
    $p = (string) ( $sz5ex__o['product_name'] ?? '' );
    if ( $p ) $sz5ex_f_products[ $p ] = $p;
    $a = (string) ( $sz5ex__o['affiliate_name'] ?? '' );
    if ( $a ) $sz5ex_f_affs[ $a ] = $a;
    $of = (string) ( $sz5ex__o['senderzz_offer_name'] ?? '' );
    if ( $of ) $sz5ex_f_offers[ $of ] = $of;
}
ksort( $sz5ex_f_products );
ksort( $sz5ex_f_affs );
ksort( $sz5ex_f_offers );

$sz5ex_money = static function( float $v ): string {
    return function_exists( 'senderzz_portal_money' )
        ? senderzz_portal_money( $v )
        : 'R$ ' . number_format( $v, 2, ',', '.' );
};
?>
<section id="sec-expedicao" class="sz-sec" data-szv2-label="Expedição"
         data-szv2-ajax-url="<?php echo esc_attr( $sz5ex_ajax_url ); ?>">

    <?php if ( empty( $sz5ex_orders ) ) : ?>
        <?php echo sz_v2_empty_state( [ 'title' => 'Nenhum pedido de Expedição', 'text' => 'Os pedidos com frete aparecem aqui assim que chegarem.' ] ); // phpcs:ignore ?>
    <?php else : ?>

    <!-- Cabeçalho + filtros -->
    <div style="margin-bottom:18px">
        <h1 style="font-size:22px;font-weight:700;color:var(--szv2-text-main,#111827);margin:0 0 4px">Expedição</h1>
        <p style="font-size:13px;color:var(--szv2-text-muted,#6b7280);margin:0 0 16px">Gerencie seus envios com eficiência. Aprove, cancele ou reprocesse em tempo real.</p>

        <!-- Chips de status -->
        <div style="display:flex;gap:6px;flex-wrap:wrap;align-items:center;margin-bottom:12px">
            <?php foreach ( $sz5ex_filter_labels as $sz5ex_fk => $sz5ex_fl ) :
                $sz5ex_chip_n  = $sz5ex_group_counts[ $sz5ex_fk ] ?? 0;
                $sz5ex_chip_active = $sz5ex_fk === 'todos';
                if ( $sz5ex_fk !== 'todos' && $sz5ex_chip_n === 0 ) continue;
            ?>
            <button type="button"
                    class="szv2-chip szv2-filter-chip <?php echo $sz5ex_chip_active ? 'szv2-chip-active' : ''; ?>"
                    data-ex-filter="<?php echo esc_attr( $sz5ex_fk ); ?>"
                    onclick="szV2ExChipFilter(this)"
                    style="display:inline-flex;align-items:center;gap:6px;padding:5px 13px;border-radius:99px;border:1.5px solid <?php echo $sz5ex_chip_active ? 'var(--szv2-brand,#EA580C)' : '#e5e7eb'; ?>;background:<?php echo $sz5ex_chip_active ? 'var(--szv2-brand,#EA580C)' : '#fff'; ?>;color:<?php echo $sz5ex_chip_active ? '#fff' : '#374151'; ?>;font-size:13px;font-weight:600;cursor:pointer;white-space:nowrap;transition:all .12s">
                <?php echo esc_html( $sz5ex_fl ); ?>
                <span style="display:inline-flex;align-items:center;justify-content:center;min-width:20px;height:20px;padding:0 5px;font-size:11px;font-weight:700;border-radius:99px;background:<?php echo $sz5ex_chip_active ? 'rgba(255,255,255,.25)' : 'rgba(0,0,0,.08)'; ?>"><?php echo esc_html( (string) $sz5ex_chip_n ); ?></span>
            </button>
            <?php endforeach; ?>
        </div>

        <!-- Barra de busca + selects -->
        <div style="display:flex;gap:8px;align-items:center;flex-wrap:wrap">
            <input type="search" class="szv2-input" id="szv2-ex-search"
                   placeholder="Buscar pedido, cliente ou rastreio…" autocomplete="new-password" spellcheck="false"
                   oninput="szV2ExFilter()"
                   style="flex:1;min-width:180px;height:38px;border:1.5px solid #e5e7eb;border-radius:9px;padding:0 12px;font-size:13px;outline:none;transition:border-color .12s"
                   onfocus="this.style.borderColor='var(--szv2-brand,#EA580C)'" onblur="this.style.borderColor='#e5e7eb'">
            <?php if ( ! empty( $sz5ex_f_products ) ) : ?>
            <select class="szv2-input" id="szv2-ex-f-product" onchange="szV2ExFilter()"
                    style="height:38px;border:1.5px solid #e5e7eb;border-radius:9px;padding:0 10px;font-size:13px;min-width:160px;max-width:220px;background:#fff">
                <option value="">Todos os produtos</option>
                <?php foreach ( $sz5ex_f_products as $v ) : ?>
                <option value="<?php echo esc_attr( $v ); ?>"><?php echo esc_html( $v ); ?></option>
                <?php endforeach; ?>
            </select>
            <?php endif; ?>
            <?php if ( ! empty( $sz5ex_f_affs ) ) : ?>
            <select class="szv2-input" id="szv2-ex-f-aff" onchange="szV2ExFilter()"
                    style="height:38px;border:1.5px solid #e5e7eb;border-radius:9px;padding:0 10px;font-size:13px;min-width:140px;max-width:180px;background:#fff">
                <option value="">Todos os afiliados</option>
                <?php foreach ( $sz5ex_f_affs as $v ) : ?>
                <option value="<?php echo esc_attr( $v ); ?>"><?php echo esc_html( $v ); ?></option>
                <?php endforeach; ?>
            </select>
            <?php endif; ?>
            <?php if ( ! empty( $sz5ex_f_offers ) ) : ?>
            <select class="szv2-input" id="szv2-ex-f-offer" onchange="szV2ExFilter()"
                    style="height:38px;border:1.5px solid #e5e7eb;border-radius:9px;padding:0 10px;font-size:13px;min-width:140px;max-width:180px;background:#fff">
                <option value="">Todas as ofertas</option>
                <?php foreach ( $sz5ex_f_offers as $v ) : ?>
                <option value="<?php echo esc_attr( $v ); ?>"><?php echo esc_html( $v ); ?></option>
                <?php endforeach; ?>
            </select>
            <?php endif; ?>
            <select class="szv2-input" id="szv2-ex-f-date" onchange="szV2ExFilter()"
                    style="height:38px;border:1.5px solid #e5e7eb;border-radius:9px;padding:0 10px;font-size:13px;min-width:120px;max-width:160px;background:#fff">
                <option value="">Qualquer data</option>
                <option value="hoje">Hoje</option>
                <option value="semana">Esta semana</option>
                <option value="mes">Este mês</option>
            </select>
        </div>
    </div>

    <div class="szv2-card" style="padding:0;overflow:hidden">
        <div class="szv2-table-wrap">
            <table class="szv2-table" id="szv2-expedicao-table" style="width:100%;border-collapse:collapse">
                <thead>
                    <tr>
                        <th style="white-space:nowrap">PEDIDO</th>
                        <th>CLIENTE</th>
                        <th>PRODUTO</th>
                        <th>TRANSPORTADORA</th>
                        <th>RASTREIO</th>
                        <th>STATUS</th>
                        <th style="text-align:right;white-space:nowrap">VALOR</th>
                        <th style="text-align:right;white-space:nowrap">COMISSÃO</th>
                        <th style="white-space:nowrap">DATA</th>
                        <th style="text-align:right;white-space:nowrap">AÇÕES</th>
                    </tr>
                </thead>
                <tbody>
                    <?php foreach ( $sz5ex_orders as $sz5ex_o ) :
                        $sz5ex_id         = (int) ( $sz5ex_o['id'] ?? 0 );
                        $sz5ex_num        = (string) ( $sz5ex_o['number'] ?? $sz5ex_id );
                        $sz5ex_status_raw = strtolower( str_replace( 'wc-', '', (string) ( $sz5ex_o['status'] ?? '' ) ) );
                        $sz5ex_client     = (string) ( $sz5ex_o['billing']['name'] ?? '—' );
                        $sz5ex_product    = (string) ( $sz5ex_o['product_name'] ?? '—' );
                        $sz5ex_offer_name = (string) ( $sz5ex_o['senderzz_offer_name'] ?? '' );
                        $sz5ex_aff_name   = (string) ( $sz5ex_o['affiliate_name'] ?? '' );
                        $sz5ex_carrier    = (string) ( $sz5ex_o['shipping_name'] ?? '—' );
                        $sz5ex_tracking   = implode( ', ', array_slice( (array) ( $sz5ex_o['tracking_codes'] ?? [] ), 0, 2 ) );
                        $sz5ex_val_fmt    = $sz5ex_money( (float) ( $sz5ex_o['total_no_ship_raw'] ?? $sz5ex_o['total_raw'] ?? 0 ) );
                        $sz5ex_comm_raw   = (float) ( $sz5ex_o['affiliate_commission'] ?? 0 );
                        $sz5ex_date_raw   = (string) ( $sz5ex_o['date_machine'] ?? $sz5ex_o['date'] ?? '' );
                        $sz5ex_date_fmt   = $sz5ex_date_raw ? wp_date( 'd/m/Y', strtotime( $sz5ex_date_raw ) ) : '—';
                        $sz5ex_date_iso   = $sz5ex_date_raw ? substr( $sz5ex_date_raw, 0, 10 ) : '';
                        // Ações: flags do format_order (status) + permissão real por pedido
                        $sz5ex_can_approve = $sz5ex_can_act
                            && ! empty( $sz5ex_o['actions']['can_approve'] )
                            && $sz5ex_can_order_action( $sz5ex_id, $sz_v2_user, 'approve' );
                        $sz5ex_can_cancel  = $sz5ex_can_act
                            && ! empty( $sz5ex_o['actions']['can_cancel'] )
                            && $sz5ex_can_order_action( $sz5ex_id, $sz_v2_user, 'cancel' );
                        $sz5ex_can_retry   = $sz5ex_can_act
                            && ! empty( $sz5ex_o['actions']['can_retry'] )
                            && $sz5ex_can_order_action( $sz5ex_id, $sz_v2_user, 'edit' );
                        $sz5ex_filter_key  = 'outros';
                        foreach ( $sz5ex_filter_groups as $sz5ex_fg_key => $sz5ex_fg_list ) {
                            if ( in_array( $sz5ex_status_raw, $sz5ex_fg_list, true ) ) {
                                $sz5ex_filter_key = $sz5ex_fg_key; break;
                            }
                        }
                    ?>
                        <tr id="szv2-exp-row-<?php echo esc_attr( (string) $sz5ex_id ); ?>"
                            data-status="<?php echo esc_attr( $sz5ex_status_raw ); ?>"
                            data-filter-group="<?php echo esc_attr( $sz5ex_filter_key ); ?>"
                            data-search="<?php echo esc_attr( strtolower( $sz5ex_num . ' ' . $sz5ex_client . ' ' . $sz5ex_product . ' ' . $sz5ex_tracking . ' ' . $sz5ex_aff_name ) ); ?>"
                            data-product="<?php echo esc_attr( $sz5ex_product ); ?>"
                            data-aff="<?php echo esc_attr( $sz5ex_aff_name ); ?>"
                            data-offer="<?php echo esc_attr( $sz5ex_offer_name ); ?>"
                            data-date-iso="<?php echo esc_attr( $sz5ex_date_iso ); ?>"
                            data-order-id="<?php echo esc_attr( (string) $sz5ex_id ); ?>"
                            style="transition:background .08s">
                            <td style="white-space:nowrap">
                                <span style="font-weight:700;font-size:13px;color:var(--szv2-text)"><?php echo esc_html( $sz5ex_num ); ?></span>
                            </td>
                            <td>
                                <div style="font-weight:600;font-size:13px;color:var(--szv2-text)"><?php echo esc_html( $sz5ex_client ); ?></div>
                                <?php if ( $sz5ex_aff_name !== '' ) : ?>
                                <div style="font-size:11px;margin-top:2px"><span style="padding:1px 6px;background:rgba(234,88,12,.08);color:var(--szv2-brand,#EA580C);border-radius:99px;font-weight:600"><?php echo esc_html( $sz5ex_aff_name ); ?></span></div>
                                <?php endif; ?>
                            </td>
                            <td style="font-size:13px;color:var(--szv2-text-soft);max-width:180px">
                                <div style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="<?php echo esc_attr( $sz5ex_product ); ?>"><?php echo esc_html( $sz3_trim( $sz5ex_product, 32 ) ); ?></div>
                            </td>
                            <td style="font-size:13px;color:var(--szv2-text-soft)"><?php echo esc_html( $sz5ex_carrier ); ?></td>
                            <td style="font-size:13px;color:var(--szv2-text-soft);white-space:nowrap">
                                <?php if ( '' !== $sz5ex_tracking ) : ?>
                                    <span title="<?php echo esc_attr( $sz5ex_tracking ); ?>"><?php echo esc_html( $sz3_trim( $sz5ex_tracking, 20 ) ); ?></span>
                                    <button type="button"
                                            class="szv2-link-btn szv2-exp-copy-tracking"
                                            data-tracking="<?php echo esc_attr( $sz5ex_tracking ); ?>"
                                            title="Copiar rastreio"
                                            aria-label="Copiar código de rastreio"></button>
                                <?php else : ?>
                                    <span style="color:var(--szv2-text-faint,#d1d5db)">—</span>
                                <?php endif; ?>
                            </td>
                            <td style="padding:12px 14px"><?php echo sz_v2_status_badge( $sz5ex_status_raw ); // phpcs:ignore ?></td>
                            <td style="text-align:right;white-space:nowrap">
                                <span style="font-weight:700;font-size:13px;color:var(--szv2-text)"><?php echo esc_html( $sz5ex_money( (float) ( $sz5ex_o['shipping_total_raw'] ?? 0 ) ) ); ?></span>
                            </td>
                            <td style="text-align:right;white-space:nowrap;font-size:13px;color:var(--szv2-text-soft)">
                                <?php echo $sz5ex_comm_raw > 0 ? esc_html( $sz5ex_money( $sz5ex_comm_raw ) ) : '<span style="color:var(--szv2-text-faint,#d1d5db)">—</span>'; // phpcs:ignore ?>
                            </td>
                            <td style="white-space:nowrap;font-size:13px;color:var(--szv2-text-soft)"><?php echo esc_html( $sz5ex_date_fmt ); ?></td>
                            <td style="text-align:right;white-space:nowrap">
                                <div style="display:flex;gap:8px;justify-content:flex-end;align-items:center">
                                <?php if ( $sz5ex_can_approve ) : ?>
                                <button type="button"
                                        class="szv2-exp-action-btn"
                                        data-action="approve"
                                        data-order-id="<?php echo esc_attr( (string) $sz5ex_id ); ?>"
                                        data-order-num="<?php echo esc_attr( $sz5ex_num ); ?>"
                                        data-nonce="<?php echo esc_attr( $sz5ex_ajax_nonce ); ?>"
                                        style="background:none;border:none;cursor:pointer;font-size:13px;font-weight:600;color:var(--szv2-brand,#EA580C);padding:0;text-decoration:underline">Aprovar</button>
                                <?php else : ?>
                                <span style="font-size:13px;color:var(--szv2-text-faint);font-weight:600">Aprovar</span>
                                <?php endif; ?>
                                <?php if ( $sz5ex_can_cancel ) : ?>
                                <button type="button"
                                        class="szv2-exp-action-btn"
                                        data-action="cancel"
                                        data-order-id="<?php echo esc_attr( (string) $sz5ex_id ); ?>"
                                        data-order-num="<?php echo esc_attr( $sz5ex_num ); ?>"
                                        data-has-label="<?php echo esc_attr( ! empty( $sz5ex_o['has_label'] ) ? '1' : '0' ); ?>"
                                        data-nonce="<?php echo esc_attr( $sz5ex_ajax_nonce ); ?>"
                                        style="background:none;border:none;cursor:pointer;font-size:13px;font-weight:600;color:var(--szv2-danger,#dc2626);padding:0;text-decoration:underline">Cancelar</button>
                                <?php else : ?>
                                <span style="font-size:13px;color:var(--szv2-text-faint);font-weight:600">Cancelar</span>
                                <?php endif; ?>
                                <?php if ( $sz5ex_can_retry ) : ?>
                                <button type="button"
                                        class="szv2-exp-action-btn"
                                        data-action="retry"
                                        data-order-id="<?php echo esc_attr( (string) $sz5ex_id ); ?>"
                                        data-order-num="<?php echo esc_attr( $sz5ex_num ); ?>"
                                        data-nonce="<?php echo esc_attr( $sz5ex_ajax_nonce ); ?>"
                                        style="background:none;border:none;cursor:pointer;font-size:13px;font-weight:600;color:var(--szv2-text-muted);padding:0;text-decoration:underline">Reprocessar</button>
                                <?php endif; ?>
                                <?php if ( '' !== $sz5ex_tracking ) : ?>
                                <button type="button"
                                        style="background:none;border:none;cursor:pointer;font-size:13px;font-weight:600;color:var(--szv2-text-muted);padding:0;text-decoration:underline"
                                        onclick="navigator.clipboard&&navigator.clipboard.writeText('<?php echo esc_js( $sz5ex_tracking ); ?>').then(function(){if(window.szV2Toast)szV2Toast('Rastreio copiado!','success');})">Copiar</button>
                                <?php endif; ?>
                                <?php if ( ! $sz5ex_can_approve && ! $sz5ex_can_cancel && ! $sz5ex_can_retry && $sz5ex_tracking === '' ) : ?>
                                <span style="color:var(--szv2-text-faint,#d1d5db);font-size:12px">—</span>
                                <?php endif; ?>
                                </div>
                            </td>
                        </tr>
                    <?php endforeach; ?>
                </tbody>
            </table>
        </div>
        <div style="padding:10px 16px;border-top:1px solid var(--szv2-border);font-size:13px;color:var(--szv2-text-muted)">
            <span id="szv2-ex-foot-count">Mostrando <?php echo esc_html( (string) count( $sz5ex_orders ) ); ?> pedidos</span>
        </div>
    </div>

    <?php endif; ?>
</section>

<script>
(function(){
    var _szExActiveChip = 'todos';

    function szV2ExChipFilter(btn) {
        _szExActiveChip = btn.getAttribute('data-ex-filter') || 'todos';
        // Atualiza visual dos chips
        var chips = document.querySelectorAll('[data-ex-filter]');
        chips.forEach(function(c) {
            var active = c.getAttribute('data-ex-filter') === _szExActiveChip;
            c.style.background = active ? 'var(--szv2-brand,#EA580C)' : '#fff';
            c.style.borderColor = active ? 'var(--szv2-brand,#EA580C)' : '#e5e7eb';
            c.style.color = active ? '#fff' : '#374151';
            var badge = c.querySelector('span');
            if (badge) badge.style.background = active ? 'rgba(255,255,255,.25)' : 'rgba(0,0,0,.08)';
        });
        szV2ExFilter();
    }

    function szV2ExFilter() {
        var search  = (document.getElementById('szv2-ex-search') || {}).value || '';
        var product = (document.getElementById('szv2-ex-f-product') || {}).value || '';
        var aff     = (document.getElementById('szv2-ex-f-aff') || {}).value || '';
        var offer   = (document.getElementById('szv2-ex-f-offer') || {}).value || '';
        var date    = (document.getElementById('szv2-ex-f-date') || {}).value || '';
        var q       = search.trim().toLowerCase();

        var today = new Date();
        today.setHours(0,0,0,0);
        var weekStart = new Date(today);
        weekStart.setDate(today.getDate() - today.getDay());
        var monthStart = new Date(today.getFullYear(), today.getMonth(), 1);

        var rows = document.querySelectorAll('#szv2-expedicao-table tbody tr');
        var visible = 0;

        rows.forEach(function(row) {
            var fg        = row.getAttribute('data-filter-group') || '';
            var rowSearch = row.getAttribute('data-search') || '';
            var rowProd   = row.getAttribute('data-product') || '';
            var rowAff    = row.getAttribute('data-aff') || '';
            var rowOffer  = row.getAttribute('data-offer') || '';
            var rowDate   = row.getAttribute('data-date-iso') || '';

            var ok = true;

            // Chip de status
            if (_szExActiveChip && _szExActiveChip !== 'todos' && fg !== _szExActiveChip) ok = false;

            // Busca texto
            if (ok && q && rowSearch.indexOf(q) === -1) ok = false;

            // Produto
            if (ok && product && rowProd !== product) ok = false;

            // Afiliado
            if (ok && aff && rowAff !== aff) ok = false;

            // Oferta
            if (ok && offer && rowOffer !== offer) ok = false;

            // Data
            if (ok && date && rowDate) {
                var d = new Date(rowDate + 'T00:00:00');
                if (date === 'hoje' && d.getTime() !== today.getTime()) ok = false;
                if (date === 'semana' && d < weekStart) ok = false;
                if (date === 'mes' && d < monthStart) ok = false;
            } else if (ok && date && !rowDate) {
                ok = false;
            }

            row.style.display = ok ? '' : 'none';
            if (ok) visible++;
        });

        var foot = document.getElementById('szv2-ex-foot-count');
        if (foot) foot.textContent = 'Mostrando ' + visible + ' pedido' + (visible !== 1 ? 's' : '');
    }

    window.szV2ExChipFilter = szV2ExChipFilter;
    window.szV2ExFilter = szV2ExFilter;
})();
</script>
