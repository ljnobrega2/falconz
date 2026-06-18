<?php
/**
 * order-metabox.php
 * Meta boxes no pedido WooCommerce:
 * - "🛵 Entrega COD" — comprovantes, auditoria, dados de entrega
 */
defined( 'ABSPATH' ) || exit;

add_action( 'admin_post_sz_order_force_motoboy', function(): void {
    if ( ! current_user_can( 'manage_woocommerce' ) && ! current_user_can( 'manage_options' ) ) {
        wp_die( 'Sem permissão.' );
    }
    $order_id = absint( $_GET['order_id'] ?? $_POST['order_id'] ?? 0 );
    if ( ! $order_id || ! wp_verify_nonce( (string) ( $_GET['_wpnonce'] ?? $_POST['_wpnonce'] ?? '' ), 'sz_order_force_motoboy_' . $order_id ) ) {
        wp_die( 'Ação inválida.' );
    }
    $order = function_exists( 'wc_get_order' ) ? wc_get_order( $order_id ) : null;
    if ( ! $order instanceof WC_Order ) {
        wp_die( 'Pedido não encontrado.' );
    }

    $target = sanitize_key( (string) ( $_GET['mb_status'] ?? $_POST['mb_status'] ?? 'agendado' ) );
    $allowed = [ 'agendado', 'embalado', 'entregue', 'frustrado', 'cancelado' ];
    if ( $target === 'em_rota' ) {
        $redirect = wp_get_referer();
        if ( ! $redirect ) $redirect = admin_url( 'admin.php?page=wc-orders&action=edit&id=' . $order_id );
        wp_safe_redirect( add_query_arg( 'sz_mb_forced', 'route_qr_only', $redirect ) );
        exit;
    }
    if ( ! in_array( $target, $allowed, true ) ) $target = 'agendado';

    $order->update_meta_data( '_senderzz_delivery_mode', 'motoboy' );
    $order->update_meta_data( '_senderzz_motoboy_flow_status', $target );
    $order->add_order_note( 'Senderzz: pedido definido como Motoboy COD pelo admin. Status operacional: ' . $target );
    if ( function_exists( 'senderzz_set_order_status_from_motoboy_status' ) ) {
        senderzz_set_order_status_from_motoboy_status( $order, $target, 'Senderzz: status Motoboy definido pelo admin e sincronizado no WooCommerce.' );
    } else {
        $order->save();
    }

    $pedido_id = false;
    if ( function_exists( 'sz_motoboy_criar_pedido' ) ) {
        $pedido_id = sz_motoboy_criar_pedido( $order_id );
    }

    global $wpdb;
    $table = $wpdb->prefix . 'sz_motoboy_pedidos';
    if ( ! $pedido_id ) {
        $pedido_id = (int) $wpdb->get_var( $wpdb->prepare( "SELECT id FROM {$table} WHERE wc_order_id=%d LIMIT 1", $order_id ) );
    }
    if ( $pedido_id ) {
        if ( function_exists( 'sz_motoboy_mudar_status' ) && $target !== 'agendado' ) {
            sz_motoboy_mudar_status( (int) $pedido_id, $target, [], 'admin', get_current_user_id() );
        } else {
            $wpdb->update( $table, [ 'status' => $target, 'updated_at' => function_exists('sz_motoboy_now_mysql') ? sz_motoboy_now_mysql() : current_time('mysql') ], [ 'id' => (int) $pedido_id ], [ '%s', '%s' ], [ '%d' ] );
        }
    }

    $redirect = wp_get_referer();
    if ( ! $redirect ) $redirect = admin_url( 'admin.php?page=wc-orders&action=edit&id=' . $order_id );
    wp_safe_redirect( add_query_arg( 'sz_mb_forced', '1', $redirect ) );
    exit;
} );

function sz_mb_force_motoboy_url( int $order_id, string $status = 'agendado' ): string {
    return wp_nonce_url(
        admin_url( 'admin-post.php?action=sz_order_force_motoboy&order_id=' . $order_id . '&mb_status=' . rawurlencode( $status ) ),
        'sz_order_force_motoboy_' . $order_id
    );
}

function sz_mb_render_force_motoboy_controls( int $order_id, $ped_row = null ): void {
    $current = $ped_row && ! empty( $ped_row->mb_status ) ? sanitize_key( (string) $ped_row->mb_status ) : '';
    $statuses = [
        'agendado'  => 'Definir/voltar Agendado',
        'embalado'  => 'Marcar Embalado',
        'entregue'  => 'Marcar Entregue',
        'frustrado' => 'Marcar Frustrado',
        'cancelado' => 'Cancelar COD',
    ];
    echo '<div style="border:1px solid #fed7aa;background:#fff7ed;border-radius:10px;padding:12px;margin-bottom:14px;font-family:var(--sz-font);">';
    echo '<div style="display:flex;justify-content:space-between;gap:12px;align-items:center;margin-bottom:10px">';
    echo '<strong style="font-size:var(--sz-text-base);color:#111827">🛵 Operação Motoboy COD</strong>';
    echo '<span style="font-size:var(--sz-text-sm);color:#9a3412;font-weight:700;text-transform:none">' . esc_html( $current ? str_replace( '_', ' ', $current ) : 'não definido' ) . '</span>';
    echo '</div>';
    echo '<div style="display:flex;gap:8px;flex-wrap:wrap">';
    foreach ( $statuses as $st => $label ) {
        $bg = $st === 'agendado' ? '#E8650A' : '#111827';
        if ( $st === 'cancelado' || $st === 'frustrado' ) $bg = '#dc2626';
        if ( $st === 'entregue' ) $bg = '#16a34a';
        echo '<a href="' . esc_url( sz_mb_force_motoboy_url( $order_id, $st ) ) . '" style="background:' . esc_attr( $bg ) . ';color:#fff;text-decoration:none;border-radius:8px;padding:7px 10px;font-size:var(--sz-text-meta);font-weight:700">' . esc_html( $label ) . '</a>';
    }
    echo '</div>';
    echo '<p style="margin:10px 0 0;color:#9a3412;font-size:var(--sz-text-sm);font-weight:600">Use isso para colocar o pedido na fila Motoboy COD. Em rota é exclusivo do motoboy lendo o QR Code da etiqueta no PWA.</p>';
    echo '</div>';
}

add_action( 'add_meta_boxes', function() {
    $screens = [ 'shop_order', 'woocommerce_page_wc-orders' ];
    foreach ( $screens as $screen ) {
        add_meta_box(
            'sz_mb_entrega_cod',
            '🛵 Entrega COD — Senderzz',
            'sz_mb_render_entrega_metabox',
            $screen,
            'normal',
            'high'
        );
    }
} );

function sz_mb_render_entrega_metabox( $post_or_order ): void {
    $order = $post_or_order instanceof WC_Order ? $post_or_order : wc_get_order( $post_or_order->ID ?? $post_or_order );
    if ( ! $order instanceof WC_Order ) return;
    $order_id = $order->get_id();
    global $wpdb;

    // ── Dados básicos ──
    $baixa_por      = $order->get_meta( '_senderzz_motoboy_baixa_por', true );
    $baixa_admin    = $order->get_meta( '_senderzz_motoboy_baixa_admin_nome', true );
    $baixa_mb_nome  = $order->get_meta( '_senderzz_motoboy_baixa_motoboy_nome', true )
                   ?: $order->get_meta( '_senderzz_motoboy_entregador_nome', true );
    $baixa_at       = $order->get_meta( '_senderzz_motoboy_baixa_at', true )
                   ?: $order->get_meta( '_senderzz_motoboy_entregue_at', true );
    $recebedor      = $order->get_meta( '_senderzz_motoboy_recebedor_nome', true );
    $recebedor_cpf  = $order->get_meta( '_senderzz_motoboy_recebedor_cpf', true );
    $recebedor_tipo = $order->get_meta( '_senderzz_motoboy_recebedor_tipo', true ) ?: 'cliente';
    $pgto_total     = (float) $order->get_meta( '_senderzz_motoboy_pgto_total', true );
    $pgto_din       = (float) $order->get_meta( '_senderzz_motoboy_pgto_dinheiro', true );
    $pgto_pix       = (float) $order->get_meta( '_senderzz_motoboy_pgto_pix', true );
    $pgto_cartao    = (float) $order->get_meta( '_senderzz_motoboy_pgto_cartao', true );
    $entrega_lat    = $order->get_meta( '_senderzz_motoboy_entrega_lat', true );
    $entrega_lng    = $order->get_meta( '_senderzz_motoboy_entrega_lng', true );
    $confirmado     = $order->get_meta( '_sz_mb_confirmacao_repasse_confirmado', true );
    $confirm_at     = $order->get_meta( '_sz_mb_confirmacao_repasse_at', true );
    $audit_aff_name = (string) $order->get_meta( '_sz_aff_name', true );
    $audit_aff_id   = (int) $order->get_meta( '_sz_affiliate_id', true );

    // ── Motoboy atual ──
    $ped_row = $wpdb->get_row( $wpdb->prepare(
        "SELECT mp.id, mp.status AS mb_status, mp.dest_produto, mp.dest_numero, mp.dest_complemento,
                m.nome AS motoboy_nome, mp.motoboy_id
           FROM {$wpdb->prefix}sz_motoboy_pedidos mp
           LEFT JOIN {$wpdb->prefix}sz_motoboys m ON m.id = mp.motoboy_id
          WHERE mp.wc_order_id = %d LIMIT 1",
        $order_id
    ) );

    // Controle manual sempre visível no detalhe Woo: permite definir Motoboy em qualquer etapa.
    sz_mb_render_force_motoboy_controls( $order_id, $ped_row );

    // Se não tem dados COD, mantém o painel de ação e mostra apenas o aviso.
    if ( ! $baixa_por && ! $recebedor && ! $pgto_total && ! $ped_row ) {
        echo '<p style="color:#9ca3af;font-size:var(--sz-text-meta)">Nenhum dado de entrega COD registrado ainda. Use os botões acima para colocar o pedido na operação Motoboy.</p>';
        return;
    }

    // ── Comprovantes ──
    $comps = $wpdb->get_results( $wpdb->prepare(
        "SELECT tipo_pgto, foto_url, baixa_por, created_at FROM {$wpdb->prefix}sz_motoboy_comprovantes
          WHERE wc_order_id = %d ORDER BY created_at ASC",
        $order_id
    ), ARRAY_A ) ?: [];

    // ── Auditoria ──
    $mp = $wpdb->get_row( $wpdb->prepare(
        "SELECT id FROM {$wpdb->prefix}sz_motoboy_pedidos WHERE wc_order_id=%d LIMIT 1", $order_id
    ) );
    $audit = [];
    if ( $mp ) {
        $audit = $wpdb->get_results( $wpdb->prepare(
            "SELECT a.acao, a.de_status, a.para_status, a.actor_tipo, a.actor_id, a.created_at, a.meta_json,
                    m.nome AS motoboy_nome
               FROM {$wpdb->prefix}sz_motoboy_audit a
               LEFT JOIN {$wpdb->prefix}sz_motoboys m ON m.id = a.motoboy_id
              WHERE a.pedido_id = %d
              ORDER BY a.created_at ASC",
            $mp->id
        ), ARRAY_A ) ?: [];
    }

    $money = function($v) { return 'R$&nbsp;' . number_format( (float)$v, 2, ',', '.' ); };
    ?>
    <style>
    .sz-cod-box{font-family:var(--sz-font);font-size:var(--sz-text-base)}
    .sz-cod-section{margin-bottom:16px}
    .sz-cod-title{font-size:var(--sz-text-sm);font-weight:700;text-transform:none;letter-spacing:.02em;color:#9ca3af;margin-bottom:8px}
    .sz-cod-row{display:flex;justify-content:space-between;align-items:center;padding:6px 0;border-bottom:1px solid #f3f4f6}
    .sz-cod-row:last-child{border-bottom:none}
    .sz-cod-label{color:#6b7280;font-size:var(--sz-text-meta)}
    .sz-cod-val{font-weight:700;color:#111827;font-size:var(--sz-text-meta)}
    .sz-cod-badge{border-radius:99px;padding:3px 8px;font-size:var(--sz-text-xs);font-weight:700;display:inline-flex;align-items:center}
    .sz-cod-comp-grid{display:flex;flex-wrap:wrap;gap:8px;margin-top:8px}
    .sz-cod-comp-item{position:relative}
    .sz-cod-comp-item img{width:72px;height:72px;object-fit:cover;border-radius:8px;border:2px solid #e5e7eb;cursor:pointer}
    .sz-cod-comp-badge{position:absolute;top:-5px;right:-5px;background:#E8650A;color:#fff;border-radius:99px;width:18px;height:18px;font-size:var(--sz-text-xs);font-weight:700;display:flex;align-items:center;justify-content:center}
    .sz-cod-audit-row{display:flex;gap:10px;padding:6px 0;border-bottom:1px solid #f9fafb;font-size:var(--sz-text-sm);align-items:flex-start}
    .sz-cod-audit-row:last-child{border-bottom:none}
    .sz-cod-audit-time{color:#9ca3af;min-width:120px;flex-shrink:0}
    .sz-cod-audit-actor{font-weight:700;color:#374151;min-width:80px;flex-shrink:0}
    .sz-cod-audit-acao{color:#374151;flex:1}
    .sz-cod-arrow{color:#9ca3af;margin:0 4px}
    </style>
    <div class="sz-cod-box">

    <?php // ── Motoboy atual ── ?>
    <?php if ( $ped_row ) :
        $mb_status_colors = [
            'agendado'=>'#f59e0b','embalado'=>'#8b5cf6','em_rota'=>'#2563eb',
            'entregue'=>'#16a34a','frustrado'=>'#dc2626','cancelado'=>'#6b7280',
        ];
        $mb_status_color = $mb_status_colors[ $ped_row->mb_status ] ?? '#9ca3af';
    ?>
    <div class="sz-cod-section">
        <div class="sz-cod-title">🏍️ Motoboy responsável</div>
        <div class="sz-cod-row">
            <span class="sz-cod-label">Motoboy</span>
            <span class="sz-cod-val"><?php echo esc_html( $ped_row->motoboy_nome ?: '—' ); ?></span>
        </div>
        <div class="sz-cod-row">
            <span class="sz-cod-label">Status do pedido</span>
            <span class="sz-cod-val">
                <span class="sz-cod-badge" style="background:<?php echo esc_attr($mb_status_color); ?>;color:#fff">
                    <?php echo esc_html( strtoupper( str_replace('_',' ', $ped_row->mb_status) ) ); ?>
                </span>
            </span>
        </div>
        <?php if ( $ped_row->dest_produto ) : ?>
        <div class="sz-cod-row">
            <span class="sz-cod-label">Produto</span>
            <span class="sz-cod-val"><?php echo esc_html( function_exists('senderzz_clean_product_summary') ? senderzz_clean_product_summary( $ped_row->dest_produto ) : ( function_exists('senderzz_clean_product_label') ? senderzz_clean_product_label( $ped_row->dest_produto ) : $ped_row->dest_produto ) ); ?></span>
        </div>
        <?php endif; ?>
    </div>
    <?php endif; ?>

    <?php // ── Entrega ── ?>
    <div class="sz-cod-section">
        <div class="sz-cod-title">📦 Dados da entrega</div>
        <?php if ( $baixa_por ) : ?>
        <div class="sz-cod-row">
            <span class="sz-cod-label">Baixa por</span>
            <span class="sz-cod-val">
                <?php if ( $baixa_por === 'admin' ) : ?>
                    <span class="sz-cod-badge" style="background:#dbeafe;color:#1e40af">👤 Admin: <?php echo esc_html( $baixa_admin ); ?></span>
                <?php else : ?>
                    <span class="sz-cod-badge" style="background:#dcfce7;color:#166534">🛵 Motoboy</span>
                <?php endif; ?>
            </span>
        </div>
        <?php endif; ?>
        <?php if ( $baixa_mb_nome ) : ?>
        <div class="sz-cod-row">
            <span class="sz-cod-label">Motoboy entregador</span>
            <span class="sz-cod-val"><?php echo esc_html( $baixa_mb_nome ); ?></span>
        </div>
        <?php endif; ?>
        <?php if ( $baixa_at ) : ?>
        <div class="sz-cod-row">
            <span class="sz-cod-label">Data/hora da baixa</span>
            <span class="sz-cod-val"><?php echo esc_html( wp_date( 'd/m/Y H:i:s', strtotime( $baixa_at ) ) ); ?></span>
        </div>
        <?php endif; ?>
        <?php if ( $recebedor ) : ?>
        <div class="sz-cod-row">
            <span class="sz-cod-label">Recebedor</span>
            <span class="sz-cod-val">
                <?php echo esc_html( $recebedor ); ?>
                <span class="sz-cod-badge" style="background:#f3f4f6;color:#374151;margin-left:4px">
                    <?php echo $recebedor_tipo === 'terceiro' ? '👥 Terceiro' : '👤 Cliente'; ?>
                </span>
            </span>
        </div>
        <?php endif; ?>
        <?php if ( $recebedor_cpf ) : ?>
        <div class="sz-cod-row">
            <span class="sz-cod-label">CPF recebedor</span>
            <span class="sz-cod-val" style="font-family:var(--sz-font)"><?php echo esc_html( $recebedor_cpf ); ?></span>
        </div>
        <?php endif; ?>
        <?php if ( $pgto_total > 0 ) : ?>
        <div class="sz-cod-row">
            <span class="sz-cod-label">Total recebido</span>
            <span class="sz-cod-val" style="color:#16a34a;font-size:var(--sz-text-md)"><?php echo $money( $pgto_total ); ?></span>
        </div>
        <?php if ( $pgto_din > 0 ) : ?>
        <div class="sz-cod-row"><span class="sz-cod-label">└ Dinheiro</span><span class="sz-cod-val"><?php echo $money( $pgto_din ); ?></span></div>
        <?php endif; ?>
        <?php if ( $pgto_pix > 0 ) : ?>
        <div class="sz-cod-row"><span class="sz-cod-label">└ PIX</span><span class="sz-cod-val"><?php echo $money( $pgto_pix ); ?></span></div>
        <?php endif; ?>
        <?php if ( $pgto_cartao > 0 ) : ?>
        <div class="sz-cod-row"><span class="sz-cod-label">└ Cartão</span><span class="sz-cod-val"><?php echo $money( $pgto_cartao ); ?></span></div>
        <?php endif; ?>
        <?php endif; ?>
        <?php if ( $entrega_lat && $entrega_lng ) : ?>
        <div class="sz-cod-row">
            <span class="sz-cod-label">GPS entrega</span>
            <span class="sz-cod-val">
                <a href="https://maps.google.com/?q=<?php echo esc_attr($entrega_lat); ?>,<?php echo esc_attr($entrega_lng); ?>" target="_blank" style="color:#E8650A">
                    📍 Ver no mapa
                </a>
            </span>
        </div>
        <?php endif; ?>
        <?php if ( $confirmado ) : ?>
        <div class="sz-cod-row">
            <span class="sz-cod-label">Confirmação motoboy</span>
            <span class="sz-cod-val">
                <span class="sz-cod-badge" style="background:#dcfce7;color:#166534">✅ Confirmado em <?php echo esc_html( wp_date('d/m/Y H:i', strtotime($confirm_at)) ); ?></span>
            </span>
        </div>
        <?php elseif ( $baixa_por === 'admin' ) : ?>
        <div class="sz-cod-row">
            <span class="sz-cod-label">Confirmação motoboy</span>
            <span class="sz-cod-val"><span class="sz-cod-badge" style="background:#fef3c7;color:#92400e">⏳ Pendente</span></span>
        </div>
        <?php endif; ?>
    </div>

    <?php // ── Comprovantes ── ?>
    <?php if ( $comps ) : ?>
    <div class="sz-cod-section">
        <div class="sz-cod-title">📸 Comprovantes de pagamento</div>
        <div class="sz-cod-comp-grid">
        <?php foreach ( $comps as $c ) :
            $tipo_label = [ 'dinheiro'=>'💵', 'pix'=>'📱', 'cartao'=>'💳' ][$c['tipo_pgto']] ?? '📸';
            $baixa_label = $c['baixa_por'] === 'admin' ? 'ADM' : 'MB';
        ?>
            <div class="sz-cod-comp-item">
                <a href="<?php echo esc_url( $c['foto_url'] ); ?>" target="_blank">
                    <img src="<?php echo esc_url( $c['foto_url'] ); ?>" alt="Comprovante">
                </a>
                <div class="sz-cod-comp-badge"><?php echo $tipo_label; ?></div>
                <div style="font-size:var(--sz-text-xs);color:#9ca3af;text-align:center;margin-top:2px"><?php echo esc_html($baixa_label); ?></div>
            </div>
        <?php endforeach; ?>
        </div>
    </div>
    <?php endif; ?>

    <?php // ── Auditoria ── ?>
    <?php if ( $audit ) : ?>
    <div class="sz-cod-section">
        <div class="sz-cod-title">📋 Histórico de auditoria</div>
        <?php foreach ( $audit as $a ) :
            $actor = $a['motoboy_nome'] ?: ( $a['actor_tipo'] === 'alan' ? 'Operador' : ucfirst($a['actor_tipo']) );
            if ( $a['actor_id'] ) {
                $wp_user = get_userdata( $a['actor_id'] );
                if ( $wp_user ) $actor = $wp_user->display_name;
            }
            $status_colors = [
                'agendado'=>'#f59e0b','embalado'=>'#8b5cf6','em_rota'=>'#2563eb',
                'entregue'=>'#16a34a','frustrado'=>'#dc2626','cancelado'=>'#6b7280',
            ];
            $badge = function($st) use ($status_colors) { return $st ? '<span class="sz-cod-badge" style="background:'.($status_colors[$st]??'#9ca3af').';color:#fff">'.esc_html($st).'</span>' : ''; };
            $meta = $a['meta_json'] ? json_decode( $a['meta_json'], true ) : [];
            $aff_hist = '';
            if ( is_array( $meta ) && ! empty( $meta['affiliate_name'] ) ) {
                $aff_hist = (string) $meta['affiliate_name'];
            } elseif ( $audit_aff_name !== '' ) {
                $aff_hist = $audit_aff_name;
            }
            if ( $aff_hist !== '' && $audit_aff_id ) $aff_hist .= ' #' . $audit_aff_id;
        ?>
        <div class="sz-cod-audit-row">
            <span class="sz-cod-audit-time"><?php echo esc_html( wp_date('d/m H:i:s', strtotime($a['created_at'])) ); ?></span>
            <span class="sz-cod-audit-actor"><?php echo esc_html( $actor ); ?></span>
            <span class="sz-cod-audit-acao">
                <?php echo esc_html( $a['acao'] ); ?>
                <?php if ( $a['de_status'] || $a['para_status'] ) : ?>
                    <span style="margin-left:6px">
                        <?php echo $badge($a['de_status']); ?>
                        <?php if ( $a['de_status'] && $a['para_status'] ) echo '<span class="sz-cod-arrow">→</span>'; ?>
                        <?php echo $badge($a['para_status']); ?>
                    </span>
                <?php endif; ?>
                <?php if ( $aff_hist !== '' ) echo '<span style="color:#6b7280;font-size:var(--sz-text-xs);margin-left:4px">· Afiliado: ' . esc_html( $aff_hist ) . '</span>'; ?>
                <?php if ( ! empty($meta['recebedor_nome']) ) echo '<span style="color:#6b7280;font-size:var(--sz-text-xs);margin-left:4px">· ' . esc_html($meta['recebedor_nome']) . '</span>'; ?>
            </span>
        </div>
        <?php endforeach; ?>
    </div>
    <?php endif; ?>

    </div>
    <?php
}
