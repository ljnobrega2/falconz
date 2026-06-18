<?php
/**
 * relatorios.php — Relatórios COD avançados
 * Filtros: período, motoboy, zona, status
 */
defined( 'ABSPATH' ) || exit;

function sz_mb_relatorios_page(): void {
    global $wpdb;
    $p = $wpdb->prefix;

    // ── Filtros ──
    $de      = sanitize_text_field( $_GET['de']       ?? ( new DateTimeImmutable( 'now', new DateTimeZone( 'America/Sao_Paulo' ) ) )->format( 'Y-m-01' ) );
    $ate     = sanitize_text_field( $_GET['ate']      ?? ( new DateTimeImmutable( 'now', new DateTimeZone( 'America/Sao_Paulo' ) ) )->format( 'Y-m-d' ) );
    $mb_id   = absint( $_GET['motoboy_id']            ?? 0 );
    $zona_id = absint( $_GET['zona_id']               ?? 0 );
    $status  = sanitize_key( $_GET['status']          ?? '' );
    $baixa   = sanitize_key( $_GET['baixa_por']       ?? '' );
    $export  = isset( $_GET['export_csv'] );

    // ── Listas para selects ──
    $motoboys = $wpdb->get_results( "SELECT id, nome FROM {$p}sz_motoboys WHERE ativo=1 ORDER BY nome", ARRAY_A ) ?: [];
    $zonas    = $wpdb->get_results( "SELECT z.id, z.nome FROM {$p}sz_motoboy_zonas z WHERE z.ativo=1 AND EXISTS (SELECT 1 FROM {$p}sz_motoboy_cep_zonas cz WHERE cz.zona_id=z.id) ORDER BY z.nome", ARRAY_A ) ?: [];

    // ── Query principal ──
    // Estratégia de filtro de data:
    // - Se filtro é especificamente "entregue": usa ts_entregue (data real da entrega)
    // - Se filtro é especificamente "frustrado": usa ts_frustrado (data real da frustração)
    // - Nos demais casos (todos ou status ativo): usa created_at (criação do pedido)
    if ( $status === 'entregue' ) {
        $where = [ "mp.ts_entregue >= %s", "mp.ts_entregue <= %s" ];
    } elseif ( $status === 'frustrado' ) {
        $where = [ "mp.ts_frustrado >= %s", "mp.ts_frustrado <= %s" ];
    } else {
        $where = [ "mp.created_at >= %s", "mp.created_at <= %s" ];
    }
    $args  = [ $de . ' 00:00:00', $ate . ' 23:59:59' ];
    if ( $mb_id   ) { $where[] = "mp.motoboy_id = %d";  $args[] = $mb_id; }
    if ( $zona_id ) { $where[] = "mp.zona_id = %d";     $args[] = $zona_id; }
    if ( $status  ) { $where[] = "mp.status = %s";      $args[] = $status; }
    if ( $baixa   ) { $where[] = "mp.baixa_por = %s";   $args[] = $baixa; }

    $sql = "SELECT mp.*,
                   m.nome   AS motoboy_nome,
                   z.nome   AS zona_nome,
                   cd.nome  AS cd_nome,
                   (SELECT COUNT(*) FROM {$p}sz_motoboy_comprovantes c WHERE c.pedido_id=mp.id) AS n_comp
              FROM {$p}sz_motoboy_pedidos mp
              LEFT JOIN {$p}sz_motoboys m      ON m.id  = mp.motoboy_id
              LEFT JOIN {$p}sz_motoboy_zonas z  ON z.id  = mp.zona_id
              LEFT JOIN {$p}sz_motoboy_cds cd   ON cd.id = mp.cd_id
             WHERE " . implode( ' AND ', $where ) . "
             ORDER BY mp.created_at DESC";

    $rows = $wpdb->get_results( $wpdb->prepare( $sql, ...$args ), ARRAY_A ) ?: [];

    // ── KPIs ──
    $total       = count( $rows );
    $entregues   = count( array_filter( $rows, function($r){ return $r['status'] === 'entregue'; } ) );
    $frustrados  = count( array_filter( $rows, function($r){ return $r['status'] === 'frustrado'; } ) );
    $em_rota     = count( array_filter( $rows, function($r){ return $r['status'] === 'em_rota'; } ) );
    $receita     = array_sum( array_column( array_filter( $rows, function($r){ return $r['status'] === 'entregue'; } ), 'valor_pedido' ) );
    $taxa_sucesso = $total > 0 ? round( ($entregues / $total) * 100, 1 ) : 0;
    $sem_comp    = count( array_filter( $rows, function($r){ return $r['status'] === 'entregue' && (int)$r['n_comp'] === 0; } ) );

    $money = function($v) { return 'R$&nbsp;' . number_format( (float)$v, 2, ',', '.' ); };
    $base_url = admin_url( 'admin.php?page=senderzz&area=cod&tab=cod-relatorios' );

    // ── Export CSV ──
    if ( $export ) {
        header( 'Content-Type: text/csv; charset=UTF-8' );
        header( 'Content-Disposition: attachment; filename="relatorio-cod-' . $de . '-' . $ate . '.csv"' );
        echo "\xEF\xBB\xBF"; // BOM UTF-8
        $cols = [ 'ID','Pedido WC','Status','Motoboy','Zona','CD','Destinatário','CEP','Cidade','Valor','Taxa',
                  'Pgto Dinheiro','Pgto PIX','Pgto Cartão','Recebedor','CPF Recebedor','Tipo Recebedor',
                  'Baixa Por','Data Baixa','Comprovantes','Data Criação' ];
        echo implode( ';', $cols ) . "\n";
        foreach ( $rows as $r ) {
            echo implode( ';', [
                $r['id'], $r['wc_order_id'], $r['status'],
                $r['motoboy_nome']??'', $r['zona_nome']??'', $r['cd_nome']??'',
                $r['dest_nome']??'', $r['dest_cep']??'', $r['dest_cidade']??'',
                number_format((float)$r['valor_pedido'],2,',','.'),
                number_format((float)$r['valor_taxa'],2,',','.'),
                number_format((float)$r['pgto_dinheiro'],2,',','.'),
                number_format((float)$r['pgto_pix'],2,',','.'),
                number_format((float)$r['pgto_cartao'],2,',','.'),
                $r['recebedor_nome']??'', $r['recebedor_cpf']??'', $r['recebedor_tipo']??'',
                $r['baixa_por']??'', $r['baixa_at']??'',
                $r['n_comp'], $r['created_at'],
            ] ) . "\n";
        }
        exit;
    }

    $status_colors = [
        'agendado'=>['#fef3c7','#92400e'], 'embalado'=>['#ede9fe','#6d28d9'],
        'em_rota'=>['#dbeafe','#1e40af'],  'entregue'=>['#dcfce7','#166534'],
        'frustrado'=>['#fee2e2','#991b1b'],'cancelado'=>['#f3f4f6','#374151'],
    ];
    ?>
    <div class="wrap" style="font-family:var(--sz-font)">
    <h1 style="display:flex;align-items:center;gap:10px;margin-bottom:20px">📈 Relatórios COD
        <a href="<?php echo esc_url( add_query_arg( array_merge( $_GET, ['export_csv'=>1] ), $base_url ) ); ?>"
           class="button" style="font-size:var(--sz-text-meta)">⬇ Exportar CSV</a>
    </h1>

    <!-- Filtros -->
    <form method="get" style="background:#fff;border:1px solid #e5e7eb;border-radius:12px;padding:16px;margin-bottom:20px;display:flex;flex-wrap:wrap;gap:12px;align-items:flex-end">
        <input type="hidden" name="page" value="senderzz">
        <input type="hidden" name="area" value="cod">
        <input type="hidden" name="tab"  value="cod-relatorios">
        <label style="font-size:var(--sz-text-sm);font-weight:700;color:#6b7280;text-transform:none">
            De<br><input type="date" name="de" value="<?php echo esc_attr($de); ?>" style="height:32px;border:1px solid #e5e7eb;border-radius:6px;padding:0 8px;margin-top:3px">
        </label>
        <label style="font-size:var(--sz-text-sm);font-weight:700;color:#6b7280;text-transform:none">
            Até<br><input type="date" name="ate" value="<?php echo esc_attr($ate); ?>" style="height:32px;border:1px solid #e5e7eb;border-radius:6px;padding:0 8px;margin-top:3px">
        </label>
        <label style="font-size:var(--sz-text-sm);font-weight:700;color:#6b7280;text-transform:none">
            Motoboy<br>
            <select name="motoboy_id" style="height:32px;border:1px solid #e5e7eb;border-radius:6px;padding:0 8px;margin-top:3px">
                <option value="">Todos</option>
                <?php foreach ( $motoboys as $mb ) : ?>
                    <option value="<?php echo $mb['id']; ?>" <?php selected($mb_id,(int)$mb['id']); ?>><?php echo esc_html($mb['nome']); ?></option>
                <?php endforeach; ?>
            </select>
        </label>
        <label style="font-size:var(--sz-text-sm);font-weight:700;color:#6b7280;text-transform:none">
            Zona<br>
            <select name="zona_id" style="height:32px;border:1px solid #e5e7eb;border-radius:6px;padding:0 8px;margin-top:3px">
                <option value="">Todas</option>
                <?php foreach ( $zonas as $z ) : ?>
                    <option value="<?php echo $z['id']; ?>" <?php selected($zona_id,(int)$z['id']); ?>><?php echo esc_html($z['nome']); ?></option>
                <?php endforeach; ?>
            </select>
        </label>
        <label style="font-size:var(--sz-text-sm);font-weight:700;color:#6b7280;text-transform:none">
            Status<br>
            <select name="status" style="height:32px;border:1px solid #e5e7eb;border-radius:6px;padding:0 8px;margin-top:3px">
                <option value="">Todos</option>
                <?php foreach ( ['agendado','embalado','em_rota','entregue','frustrado','cancelado'] as $st ) : ?>
                    <option value="<?php echo $st; ?>" <?php selected($status,$st); ?>><?php echo ucfirst($st); ?></option>
                <?php endforeach; ?>
            </select>
        </label>
        <label style="font-size:var(--sz-text-sm);font-weight:700;color:#6b7280;text-transform:none">
            Baixa por<br>
            <select name="baixa_por" style="height:32px;border:1px solid #e5e7eb;border-radius:6px;padding:0 8px;margin-top:3px">
                <option value="">Todos</option>
                <option value="motoboy" <?php selected($baixa,'motoboy'); ?>>Motoboy</option>
                <option value="admin"   <?php selected($baixa,'admin'); ?>>Admin/OL</option>
            </select>
        </label>
        <button type="submit" class="button button-primary" style="height:32px">Filtrar</button>
        <a href="<?php echo esc_url($base_url); ?>" class="button" style="height:32px">Limpar</a>
    </form>

    <!-- KPIs -->
    <div style="display:grid;grid-template-columns:repeat(6,1fr);gap:10px;margin-bottom:20px">
        <?php
        $kpis = [
            ['📦 Total',      $total,       '#374151','#f9fafb','#e5e7eb'],
            ['✅ Entregues',   $entregues,   '#166534','#dcfce7','#bbf7d0'],
            ['❌ Frustrados',  $frustrados,  '#991b1b','#fee2e2','#fecaca'],
            ['🛵 Em rota',     $em_rota,     '#1e40af','#dbeafe','#bfdbfe'],
            ['💰 Receita',     $money($receita), '#166534','#f0fdf4','#bbf7d0'],
            ['📊 Taxa sucesso',$taxa_sucesso.'%','#374151','#f9fafb','#e5e7eb'],
        ];
        foreach ( $kpis as [$label,$val,$tc,$bg,$border] ) : ?>
        <div style="background:<?php echo $bg;?>;border:1px solid <?php echo $border;?>;border-radius:10px;padding:12px 14px">
            <div style="font-size:var(--sz-text-xs);font-weight:700;text-transform:none;letter-spacing:.02em;color:#9ca3af"><?php echo $label; ?></div>
            <div style="font-size:var(--sz-text-xl);font-weight:700;color:<?php echo $tc;?>;margin-top:4px"><?php echo $val; ?></div>
        </div>
        <?php endforeach; ?>
    </div>

    <?php if ( $sem_comp > 0 ) : ?>
    <div style="background:#fef3c7;border:1px solid #f59e0b;border-radius:8px;padding:10px 14px;margin-bottom:16px;font-size:var(--sz-text-meta);color:#92400e;font-weight:700">
        ⚠️ <?php echo $sem_comp; ?> pedido(s) entregue(s) sem comprovante de pagamento registrado.
    </div>
    <?php endif; ?>

    <!-- Tabela -->
    <div style="background:#fff;border:1px solid #e5e7eb;border-radius:12px;overflow:hidden">
    <table class="wp-list-table widefat striped" style="font-size:var(--sz-text-meta)">
        <thead style="background:#fafafa">
            <tr>
                <th>Pedido</th><th>Data</th><th>Status</th><th>Motoboy</th><th>Zona</th>
                <th>Destinatário</th><th>Valor</th><th>Recebedor</th><th>Baixa</th><th>Comp.</th><th></th>
            </tr>
        </thead>
        <tbody>
        <?php if ( empty($rows) ) : ?>
            <tr><td colspan="11" style="text-align:center;padding:30px;color:#9ca3af">Nenhum pedido no período.</td></tr>
        <?php endif; ?>
        <?php foreach ( $rows as $r ) :
            [$bg,$tc] = $status_colors[$r['status']] ?? ['#f3f4f6','#374151'];
            $wc_url = get_edit_post_link( $r['wc_order_id'] ) ?: admin_url("post.php?post={$r['wc_order_id']}&action=edit");
        ?>
            <tr>
                <td><a href="<?php echo esc_url($wc_url); ?>" style="font-weight:700;color:#E8650A">#<?php echo $r['wc_order_id']; ?></a></td>
                <td><?php echo esc_html( wp_date('d/m/Y', strtotime($r['created_at'])) ); ?></td>
                <td><span style="background:<?php echo $bg;?>;color:<?php echo $tc;?>;border-radius:99px;padding:2px 8px;font-size:var(--sz-text-xs);font-weight:700"><?php echo esc_html($r['status']); ?></span></td>
                <td><?php echo esc_html($r['motoboy_nome'] ?? '—'); ?></td>
                <td style="font-size:var(--sz-text-sm)"><?php echo esc_html($r['zona_nome'] ?? '—'); ?></td>
                <td>
                    <strong><?php echo esc_html($r['dest_nome'] ?? '—'); ?></strong><br>
                    <span style="font-size:var(--sz-text-xs);color:#9ca3af"><?php echo esc_html($r['dest_cidade'] ?? ''); ?></span>
                </td>
                <td style="font-weight:700;color:#111827"><?php echo $money($r['valor_pedido']); ?></td>
                <td style="font-size:var(--sz-text-sm)">
                    <?php echo esc_html($r['recebedor_nome'] ?? '—'); ?>
                    <?php if ( $r['recebedor_cpf'] ) echo '<br><span style="font-family:var(--sz-font);color:#9ca3af">'.esc_html($r['recebedor_cpf']).'</span>'; ?>
                </td>
                <td style="font-size:var(--sz-text-xs)">
                    <?php if ( $r['baixa_por'] === 'admin' ) echo '<span style="background:#dbeafe;color:#1e40af;border-radius:99px;padding:2px 6px;font-weight:700">ADM</span>';
                    elseif ( $r['baixa_por'] === 'motoboy' ) echo '<span style="background:#dcfce7;color:#166534;border-radius:99px;padding:2px 6px;font-weight:700">MB</span>';
                    else echo '—'; ?>
                    <?php if ( $r['baixa_at'] ) echo '<br><span style="color:#9ca3af">'.esc_html(wp_date('d/m H:i',strtotime($r['baixa_at']))).'</span>'; ?>
                </td>
                <td style="text-align:center">
                    <?php if ( (int)$r['n_comp'] > 0 ) : ?>
                        <span style="background:#dcfce7;color:#166534;border-radius:99px;padding:2px 8px;font-size:var(--sz-text-xs);font-weight:700">📸 <?php echo (int)$r['n_comp']; ?></span>
                    <?php elseif ( $r['status'] === 'entregue' ) : ?>
                        <span style="background:#fee2e2;color:#991b1b;border-radius:99px;padding:2px 8px;font-size:var(--sz-text-xs);font-weight:700">⚠️ 0</span>
                    <?php else : echo '—'; endif; ?>
                </td>
                <td><a href="<?php echo esc_url($wc_url); ?>" class="button button-small">Ver</a></td>
            </tr>
        <?php endforeach; ?>
        </tbody>
    </table>
    </div>
    <p style="font-size:var(--sz-text-sm);color:#9ca3af;margin-top:8px"><?php echo $total; ?> registro(s) encontrado(s)</p>
    </div>
    <?php
}
