<?php
/**
 * Senderzz V2 – Motoboys do dia (OL)
 * Mostra todos os motoboys ativos e pedidos atrelados no dia atual.
 * @var object $sz_v2_user
 */
defined( 'ABSPATH' ) || exit;

$sz5md_rest_nonce = wp_create_nonce( 'wp_rest' );
$sz5md_is_aff = function_exists( 'sz_aff_portal_user_must_use_affiliate_scope' )
    && sz_aff_portal_user_must_use_affiliate_scope( $sz_v2_user );
if ( $sz5md_is_aff ) return; // afiliados não veem esta tela

$sz5md_money = static fn( float $v ): string =>
    function_exists( 'senderzz_portal_money' )
        ? senderzz_portal_money( $v )
        : 'R$ ' . number_format( $v, 2, ',', '.' );

global $wpdb;
$sz5md_hoje = wp_date( 'Y-m-d' );

// F13: wp_date() é server-generated mas violava convenção de não interpolar direto na SQL
$sz5md_motoboys = $wpdb->get_results( $wpdb->prepare(
    "SELECT m.id, m.nome, m.telefone,
            COUNT(p.id)                                                              AS total,
            SUM(CASE WHEN p.status='entregue'         THEN 1 ELSE 0 END)            AS entregues,
            SUM(CASE WHEN p.status='frustrado'        THEN 1 ELSE 0 END)            AS frustrados,
            SUM(CASE WHEN p.status IN('em_rota','a_caminho') THEN 1 ELSE 0 END)     AS em_rota,
            SUM(CASE WHEN p.status NOT IN('entregue','frustrado','cancelado') THEN 1 ELSE 0 END) AS pendentes,
            SUM(COALESCE(p.valor_pedido,0))                                          AS total_valor
     FROM {$wpdb->prefix}sz_motoboys m
     LEFT JOIN {$wpdb->prefix}sz_motoboy_pedidos p
            ON p.motoboy_id = m.id AND DATE(p.created_at) = %s
     WHERE m.ativo = 1
     GROUP BY m.id, m.nome, m.telefone
     ORDER BY m.nome ASC",
    $sz5md_hoje
), ARRAY_A ) ?: [];

// Pedidos sem motoboy hoje
$sz5md_sem_motoboy = (int) $wpdb->get_var( $wpdb->prepare(
    "SELECT COUNT(*) FROM {$wpdb->prefix}sz_motoboy_pedidos
     WHERE DATE(created_at) = %s AND (motoboy_id IS NULL OR motoboy_id = 0)
       AND status NOT IN('cancelado','devolvido')",
    $sz5md_hoje
) );
?>
<section id="sec-motoboys-dia" class="sz-sec" data-szv2-label="Motoboys — dia">

    <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:var(--szv2-space-4);flex-wrap:wrap;gap:8px">
        <div>
            <h2 style="margin:0 0 2px;font-size:18px;font-weight:800">Motoboys — <?php echo esc_html( wp_date( 'd/m/Y' ) ); ?></h2>
            <p style="margin:0;font-size:13px;color:var(--szv2-text-muted)">Pedidos atribuídos a cada motoboy hoje.</p>
        </div>
        <button type="button"
                class="szv2-btn szv2-btn-secondary szv2-btn-sm"
                onclick="location.reload()">Atualizar</button>
    </div>

    <?php if ( $sz5md_sem_motoboy > 0 ) : ?>
    <div style="background:#fff7ed;border:1px solid #fed7aa;border-radius:10px;padding:12px 16px;margin-bottom:var(--szv2-space-4);display:flex;align-items:center;gap:10px;font-size:13px">
        <svg viewBox="0 0 20 20" style="width:16px;height:16px;fill:#ea580c;flex-shrink:0"><path d="M10 2a8 8 0 1 0 0 16A8 8 0 0 0 10 2zm0 3v5l3 2-1 1.7L9 11V5h1z"/></svg>
        <span><strong><?php echo esc_html( (string) $sz5md_sem_motoboy ); ?> pedido(s)</strong> sem motoboy atribuído hoje.</span>
    </div>
    <?php endif; ?>

    <?php if ( empty( $sz5md_motoboys ) ) : ?>
        <?php echo sz_v2_empty_state( [ 'title' => 'Nenhum motoboy ativo', 'text' => 'Cadastre motoboys no painel administrativo.' ] ); // phpcs:ignore ?>
    <?php else : ?>

    <div style="display:grid;grid-template-columns:repeat(auto-fill,minmax(300px,1fr));gap:var(--szv2-space-4)">
        <?php foreach ( $sz5md_motoboys as $sz5mb ) :
            $sz5mb_id      = (int) $sz5mb['id'];
            $sz5mb_total   = (int) $sz5mb['total'];
            $sz5mb_ent     = (int) $sz5mb['entregues'];
            $sz5mb_frus    = (int) $sz5mb['frustrados'];
            $sz5mb_rota    = (int) $sz5mb['em_rota'];
            $sz5mb_pend    = (int) $sz5mb['pendentes'];
            $sz5mb_valor   = (float) $sz5mb['total_valor'];
            $sz5mb_pct     = $sz5mb_total > 0 ? round( $sz5mb_ent / $sz5mb_total * 100 ) : 0;

            // Pedidos do motoboy no dia
            $sz5mb_pedidos = $sz5mb_total > 0 ? $wpdb->get_results( $wpdb->prepare(
                "SELECT id, wc_order_id, dest_nome, dest_endereco, dest_numero, valor_pedido, status, ts_em_rota, ts_entregue
                 FROM {$wpdb->prefix}sz_motoboy_pedidos
                 WHERE motoboy_id = %d AND DATE(created_at) = %s
                 ORDER BY id ASC LIMIT 30",
                $sz5mb_id, $sz5md_hoje
            ), ARRAY_A ) ?: [] : [];
        ?>
        <div class="szv2-card" style="padding:0;overflow:hidden">
            <!-- Header motoboy -->
            <div style="padding:14px 16px;background:var(--szv2-surface-alt);border-bottom:1px solid var(--szv2-divider);display:flex;align-items:center;gap:10px">
                <div style="width:36px;height:36px;border-radius:50%;background:var(--szv2-brand);display:flex;align-items:center;justify-content:center;flex-shrink:0;color:#fff;font-weight:800;font-size:14px">
                    <?php echo esc_html( mb_strtoupper( mb_substr( (string) $sz5mb['nome'], 0, 1, 'UTF-8' ), 'UTF-8' ) ); ?>
                </div>
                <div style="min-width:0">
                    <div style="font-weight:700;font-size:14px;color:var(--szv2-text)"><?php echo esc_html( $sz5mb['nome'] ); ?></div>
                    <div style="font-size:12px;color:var(--szv2-text-muted)"><?php echo esc_html( $sz5mb['telefone'] ?: '—' ); ?></div>
                </div>
                <div style="margin-left:auto;text-align:right;flex-shrink:0">
                    <div style="font-size:20px;font-weight:800;color:var(--szv2-brand)"><?php echo esc_html( (string) $sz5mb_total ); ?></div>
                    <div style="font-size:10px;color:var(--szv2-text-muted)">pedidos</div>
                </div>
            </div>

            <!-- KPIs mini -->
            <div style="display:grid;grid-template-columns:repeat(4,1fr);border-bottom:1px solid var(--szv2-divider)">
                <div style="padding:8px;text-align:center;border-right:1px solid var(--szv2-divider)">
                    <div style="font-size:16px;font-weight:800;color:#16a34a"><?php echo esc_html( (string) $sz5mb_ent ); ?></div>
                    <div style="font-size:9px;color:var(--szv2-text-muted);text-transform:uppercase;letter-spacing:.04em">Entregues</div>
                </div>
                <div style="padding:8px;text-align:center;border-right:1px solid var(--szv2-divider)">
                    <div style="font-size:16px;font-weight:800;color:#dc2626"><?php echo esc_html( (string) $sz5mb_frus ); ?></div>
                    <div style="font-size:9px;color:var(--szv2-text-muted);text-transform:uppercase;letter-spacing:.04em">Frustrados</div>
                </div>
                <div style="padding:8px;text-align:center;border-right:1px solid var(--szv2-divider)">
                    <div style="font-size:16px;font-weight:800;color:var(--szv2-brand)"><?php echo esc_html( (string) $sz5mb_rota ); ?></div>
                    <div style="font-size:9px;color:var(--szv2-text-muted);text-transform:uppercase;letter-spacing:.04em">Em rota</div>
                </div>
                <div style="padding:8px;text-align:center">
                    <div style="font-size:16px;font-weight:800;color:var(--szv2-text-muted)"><?php echo esc_html( (string) $sz5mb_pend ); ?></div>
                    <div style="font-size:9px;color:var(--szv2-text-muted);text-transform:uppercase;letter-spacing:.04em">Pendentes</div>
                </div>
            </div>

            <!-- Barra de progresso -->
            <?php if ( $sz5mb_total > 0 ) : ?>
            <div style="padding:8px 14px;border-bottom:1px solid var(--szv2-divider)">
                <div style="display:flex;justify-content:space-between;font-size:11px;color:var(--szv2-text-muted);margin-bottom:4px">
                    <span>Taxa de entrega</span><span><?php echo esc_html( (string) $sz5mb_pct ); ?>%</span>
                </div>
                <div style="height:6px;background:var(--szv2-divider);border-radius:99px;overflow:hidden">
                    <div style="height:100%;width:<?php echo esc_attr( (string) $sz5mb_pct ); ?>%;background:<?php echo $sz5mb_pct >= 80 ? '#16a34a' : ( $sz5mb_pct >= 50 ? '#ea580c' : '#dc2626' ); ?>;border-radius:99px;transition:width .4s"></div>
                </div>
            </div>
            <?php endif; ?>

            <!-- Lista pedidos -->
            <?php if ( ! empty( $sz5mb_pedidos ) ) : ?>
            <div style="max-height:200px;overflow-y:auto">
                <?php foreach ( $sz5mb_pedidos as $sp ) :
                    $sp_status = (string) $sp['status'];
                    $sp_color = [ 'entregue' => '#16a34a', 'frustrado' => '#dc2626', 'em_rota' => '#ea580c', 'a_caminho' => '#ea580c' ][ $sp_status ] ?? '#6b7280';
                ?>
                <div style="padding:8px 14px;border-bottom:1px solid var(--szv2-divider);display:flex;align-items:center;gap:8px;font-size:12px">
                    <span style="font-family:monospace;color:var(--szv2-text-muted);font-size:11px">#<?php echo esc_html( (string) $sp['wc_order_id'] ); ?></span>
                    <span style="flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap"><?php echo esc_html( (string) $sp['dest_nome'] ); ?></span>
                    <span style="font-size:10px;font-weight:700;color:<?php echo esc_attr( $sp_color ); ?>;white-space:nowrap"><?php echo esc_html( ucfirst( str_replace( '_', ' ', $sp_status ) ) ); ?></span>
                    <span style="font-size:11px;color:var(--szv2-text-muted);white-space:nowrap"><?php echo esc_html( $sz5md_money( (float) $sp['valor_pedido'] ) ); ?></span>
                </div>
                <?php endforeach; ?>
            </div>
            <?php else : ?>
            <div style="padding:16px;text-align:center;font-size:12px;color:var(--szv2-text-faint)">Nenhum pedido hoje</div>
            <?php endif; ?>

            <!-- Total do dia -->
            <?php if ( $sz5mb_valor > 0 ) : ?>
            <div style="padding:10px 14px;background:var(--szv2-surface-alt);font-size:12px;display:flex;justify-content:space-between">
                <span style="color:var(--szv2-text-muted)">Total do dia</span>
                <strong><?php echo esc_html( $sz5md_money( $sz5mb_valor ) ); ?></strong>
            </div>
            <?php endif; ?>
        </div>
        <?php endforeach; ?>
    </div>

    <?php endif; ?>
</section>
