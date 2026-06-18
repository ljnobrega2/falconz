<?php
/**
 * Senderzz Dashboard V2 — Seção: Afiliados (Fase 7)
 * -----------------------------------------------------------------------
 * Ações portadas (chamam backend existente da V1):
 *
 *   Aprovar afiliado:        szaction=affiliate_action  aff_act=approve
 *   Recusar afiliado:        szaction=affiliate_action  aff_act=reject
 *   Ajustar comissão:        szaction=affiliate_action  aff_act=update_commission
 *   Handler: Portal_Page::ajax_affiliate_action()
 *   Nonce: senderzz_portal
 *   Ownership: backend valida producer_id = user->id
 *
 * Ações NÃO portadas (sem handler de save no portal):
 *   Salvar comissão padrão do produtor — nenhum szaction existente no portal
 *   Salvar auto-approve — idem
 *   Criar/excluir link de convite — lógica no painel de afiliados V1 inline
 *
 * Listagem de afiliados: query direta (mesma da V1 em senderzz-affiliates.php linha ~2508)
 * Configurações: read-only via user_meta (sz_aff_producer_default_commission_pct, etc.)
 *
 * @var object $sz_v2_user
 */
defined( 'ABSPATH' ) || exit;

//  Perfil 
$sz7af_is_aff   = ( 'affiliate' === strtolower( trim( (string) ( $sz_v2_user->role ?? 'client' ) ) ) );
$sz7af_is_sub   = ! empty( $sz_v2_user->parent_user_id );
$sz7af_can_act  = ! $sz7af_is_aff && ! $sz7af_is_sub; // produtor principal

// ID canônico do produtor — mesma origem que ajax_affiliate_action() usa para
// validar ownership: producer_id = $user->id (= portal user id, não WP user id).
$sz7af_producer_id = function_exists( 'sz_aff_current_producer_id' )
    ? sz_aff_current_producer_id( $sz_v2_user )
    : (int) ( $sz_v2_user->id ?? 0 );

$sz7af_nonce    = wp_create_nonce( 'sz_aff_panel' );
$sz7af_ajax_url = admin_url( 'admin-ajax.php' );

//  VISÃO AFILIADO 
if ( $sz7af_is_aff ) :
    $sz7af_aff_rows = function_exists( 'sz_aff_get_active_rows_for_portal_user' )
        ? sz_aff_get_active_rows_for_portal_user( $sz_v2_user ) : [];
    $sz7af_aff_links = function_exists( 'sz_aff_get_visible_checkout_links_for_portal_user' )
        ? sz_aff_get_visible_checkout_links_for_portal_user( $sz_v2_user ) : [];
?>
<section id="sec-affiliates" class="sz-sec" data-szv2-label="Afiliados">
    <?php if ( empty( $sz7af_aff_rows ) ) : ?>
        <?php echo sz_v2_empty_state( [ 'title' => 'Nenhuma afiliação ativa', 'text' => 'Aguarde aprovação do produtor.' ] ); // phpcs:ignore ?>
    <?php else : ?>
    <div class="szv2-card">
        <div class="szv2-card-head"><h2>Suas afiliações</h2></div>
        <div class="szv2-table-wrap szv2-table-flush">
            <table class="szv2-table">
                <thead><tr><th>Produtor</th><th>Status</th><th class="szv2-td-num">Comissão</th><th>Desde</th></tr></thead>
                <tbody>
                    <?php foreach ( $sz7af_aff_rows as $sz7af_ar ) :
                        $sz7af_prod_id  = (int) ( $sz7af_ar['producer_id'] ?? 0 );
                        $sz7af_prod_name = $sz7af_prod_id > 0 && function_exists( 'sz_aff_get_producer_display_name' )
                            ? sz_aff_get_producer_display_name( $sz7af_prod_id )
                            : ( $sz7af_prod_id > 0 && ( $sz7af_u = get_userdata( $sz7af_prod_id ) )
                                ? ( $sz7af_u->display_name ?: $sz7af_u->user_email )
                                : '—' );
                        $sz7af_comm = (float) ( $sz7af_ar['commission_pct'] ?? 0 );
                        $sz7af_status = (string) ( $sz7af_ar['status'] ?? 'active' );
                        $_sz7af_d    = (string) ( $sz7af_ar['approved_at'] ?? $sz7af_ar['created_at'] ?? '' );
                        $sz7af_since = $_sz7af_d ? wp_date( 'd/m/Y', strtotime( $_sz7af_d ) ) : '—';
                    ?>
                    <tr>
                        <td><?php echo esc_html( $sz7af_prod_name ); ?></td>
                        <td><?php echo sz_v2_status_badge( $sz7af_status ); // phpcs:ignore ?></td>
                        <td class="szv2-td-num szv2-num"><?php echo esc_html( number_format( $sz7af_comm, 2, ',', '' ) ); ?>%</td>
                        <td><?php echo esc_html( $sz7af_since ); ?></td>
                    </tr>
                    <?php endforeach; ?>
                </tbody>
            </table>
        </div>
    </div>
    <?php if ( ! empty( $sz7af_aff_links ) ) : ?>
    <div class="szv2-card">
        <div class="szv2-card-head"><h2>Links de venda disponíveis</h2></div>
        <div class="szv2-aff-links-list">
            <?php foreach ( $sz7af_aff_links as $sz7af_lk ) :
                $sz7af_lk_name = (string) ( $sz7af_lk['display_name'] ?? $sz7af_lk['name'] ?? '—' );
                $sz7af_lk_url  = trim( (string) ( $sz7af_lk['affiliate_url'] ?? '' ) );
                $sz7af_lk_comm = (float) ( $sz7af_lk['affiliate_commission_pct'] ?? 0 );
            ?>
            <div class="szv2-aff-link-row">
                <div class="szv2-aff-link-info">
                    <span class="szv2-aff-link-name"><?php echo esc_html( $sz7af_lk_name ); ?></span>
                    <?php if ( $sz7af_lk_comm > 0 ) : ?>
                    <span class="szv2-link-info-label"><?php echo esc_html( number_format( $sz7af_lk_comm, 2, ',', '' ) ); ?>% comissão</span>
                    <?php endif; ?>
                </div>
                <?php if ( $sz7af_lk_url !== '' ) : ?>
                <button type="button"
                        class="szv2-btn szv2-btn-sm szv2-btn-secondary szv2-link-copy-btn"
                        data-url="<?php echo esc_attr( $sz7af_lk_url ); ?>"
                        title="Copiar link de venda">Copiar link</button>
                <?php else : ?>
                <span class="szv2-text-faint-val">Link indisponível</span>
                <?php endif; ?>
            </div>
            <?php endforeach; ?>
        </div>
    </div>
    <?php endif; ?>
    <?php endif; ?>
</section>
<?php return; // Afiliado não vê o resto
endif;

//  VISÃO PRODUTOR 
global $wpdb;
$sz7af_aff_table = function_exists('sz_aff_table') ? sz_aff_table('sz_affiliates') : $wpdb->prefix.'sz_affiliates';

// Listar afiliados (mesma query da V1 ~linha 2508 de senderzz-affiliates.php)
$sz7af_affs = [];
if ( $sz7af_producer_id ) {
    $sz7af_affs = (array) $wpdb->get_results( $wpdb->prepare(
        "SELECT a.*,u.display_name,u.user_email FROM {$sz7af_aff_table} a
         LEFT JOIN {$wpdb->users} u ON u.ID=a.user_id
         WHERE a.producer_id=%d AND a.deleted_at IS NULL
         ORDER BY FIELD(a.status,'pending','active'), a.created_at DESC LIMIT 200",
        $sz7af_producer_id
    ), ARRAY_A );
}

// Normalizar e-mails (mesmo padrão da V1)
foreach ( $sz7af_affs as &$sz7af_row ) {
    $sz7af_email = trim( (string) ( $sz7af_row['user_email'] ?? $sz7af_row['email'] ?? '' ) );
    if ( $sz7af_email === '' && (int) ( $sz7af_row['user_id'] ?? 0 ) > 0 ) {
        $sz7af_u_obj = get_userdata( (int) $sz7af_row['user_id'] );
        if ( $sz7af_u_obj ) $sz7af_email = (string) $sz7af_u_obj->user_email;
    }
    $sz7af_row['_email_display'] = $sz7af_email !== '' ? $sz7af_email : '—';
    $sz7af_row['_name_display']  = trim( (string) ( $sz7af_row['display_name'] ?? $sz7af_row['affiliate_name'] ?? '' ) );
    if ( $sz7af_row['_name_display'] === '' ) $sz7af_row['_name_display'] = $sz7af_row['_email_display'];
}
unset( $sz7af_row );

$sz7af_pending  = array_values( array_filter( $sz7af_affs, static fn($a) => ( $a['status'] ?? '' ) === 'pending' ) );
$sz7af_approved = array_values( array_filter( $sz7af_affs, static fn($a) => ( $a['status'] ?? '' ) === 'active' ) );

// Configurações read-only (sem handler de save)
$sz7af_default_comm    = $sz7af_producer_id && function_exists( 'sz_aff_producer_default_commission_pct' )
    ? sz_aff_producer_default_commission_pct( $sz7af_producer_id ) : 0.0;
$sz7af_auto_approve    = $sz7af_producer_id && function_exists( 'sz_aff_is_producer_auto_approve' )
    ? sz_aff_is_producer_auto_approve( $sz7af_producer_id ) : false;
?>
<section id="sec-affiliates" class="sz-sec" data-szv2-label="Afiliados"
         data-szv2-ajax-url="<?php echo esc_attr( $sz7af_ajax_url ); ?>">
    <!-- Tabs -->
    <div class="szv2-dash-switcher" data-szv2-tabs="affiliates-view" role="tablist" aria-label="Visão de afiliados">
        <button type="button" class="szv2-dash-tab szv2-dash-tab--active" role="tab"
                data-szv2-tab="pending" aria-selected="true">
            Pendentes
            <?php if ( count( $sz7af_pending ) > 0 ) : ?>
            <span class="szv2-aff-count-badge"><?php echo esc_html( (string) count( $sz7af_pending ) ); ?></span>
            <?php endif; ?>
        </button>
        <button type="button" class="szv2-dash-tab" role="tab"
                data-szv2-tab="approved" aria-selected="false">Aprovados</button>
        <button type="button" class="szv2-dash-tab" role="tab"
                data-szv2-tab="minhas" aria-selected="false">Minha afiliação</button>
        <button type="button" class="szv2-dash-tab" role="tab"
                data-szv2-tab="config" aria-selected="false">Configurações</button>
    </div>

    <!-- Painel: Pendentes -->
    <div class="szv2-dash-panel" data-szv2-panel-group="affiliates-view" data-szv2-panel="pending">
        <?php if ( empty( $sz7af_pending ) ) : ?>
            <?php echo sz_v2_empty_state( [ 'title' => 'Nenhum afiliado pendente', 'text' => 'Novos pedidos de afiliação aparecem aqui.' ] ); // phpcs:ignore ?>
        <?php else : ?>
        <div class="szv2-card szv2-card-table">
            <div class="szv2-table-wrap szv2-table-flush">
                <table class="szv2-table">
                    <thead><tr><th>Nome</th><th>E-mail</th><th>Solicitação</th><th class="szv2-aff-actions-col">Ação</th></tr></thead>
                    <tbody>
                        <?php foreach ( $sz7af_pending as $sz7af_p ) :
                            $sz7af_pid = (int) ( $sz7af_p['id'] ?? 0 );
                        ?>
                        <tr id="szv2-aff-row-<?php echo esc_attr( (string) $sz7af_pid ); ?>">
                            <td><?php echo esc_html( $sz7af_p['_name_display'] ); ?></td>
                            <td><?php echo esc_html( $sz7af_p['_email_display'] ); ?></td>
                            <td><?php $__d = (string)($sz7af_p['created_at'] ?? ''); echo esc_html( $__d ? wp_date('d/m/Y', strtotime($__d)) : '—' ); ?></td>
                            <td class="szv2-aff-actions">
                                <?php if ( $sz7af_can_act ) : ?>
                                <button type="button"
                                        class="szv2-btn szv2-btn-sm szv2-btn-brand szv2-aff-action-btn"
                                        data-aff-action="approve"
                                        data-affiliate-id="<?php echo esc_attr( (string) $sz7af_pid ); ?>"
                                        data-aff-name="<?php echo esc_attr( $sz7af_p['_name_display'] ); ?>"
                                        data-nonce="<?php echo esc_attr( $sz7af_nonce ); ?>">Aprovar</button>
                                <button type="button"
                                        class="szv2-btn szv2-btn-sm szv2-btn-danger szv2-aff-action-btn"
                                        data-aff-action="reject"
                                        data-affiliate-id="<?php echo esc_attr( (string) $sz7af_pid ); ?>"
                                        data-aff-name="<?php echo esc_attr( $sz7af_p['_name_display'] ); ?>"
                                        data-nonce="<?php echo esc_attr( $sz7af_nonce ); ?>">Recusar</button>
                                <?php endif; ?>
                            </td>
                        </tr>
                        <?php endforeach; ?>
                    </tbody>
                </table>
            </div>
        </div>
        <?php endif; ?>
    </div>

    <!-- Painel: Aprovados -->
    <div class="szv2-dash-panel" data-szv2-panel-group="affiliates-view" data-szv2-panel="approved" hidden>
        <?php if ( empty( $sz7af_approved ) ) : ?>
            <?php echo sz_v2_empty_state( [ 'title' => 'Nenhum afiliado aprovado ainda', 'text' => 'Os afiliados aprovados aparecem aqui.' ] ); // phpcs:ignore ?>
        <?php else : ?>
        <div class="szv2-card szv2-card-table">
            <div class="szv2-table-wrap szv2-table-flush">
                <table class="szv2-table">
                    <thead><tr>
                        <th>Nome</th><th>E-mail</th>
                        <th class="szv2-td-num">Comissão %</th>
                        <th>Desde</th>
                        <?php if ( $sz7af_can_act ) : ?><th class="szv2-aff-actions-col">Comissão</th><th style="text-align:right">Excluir</th><?php endif; ?>
                    </tr></thead>
                    <tbody>
                        <?php foreach ( $sz7af_approved as $sz7af_a ) :
                            $sz7af_aid  = (int) ( $sz7af_a['id'] ?? 0 );
                            $sz7af_comm = (float) ( $sz7af_a['commission_pct'] ?? $sz7af_default_comm );
                        ?>
                        <tr id="szv2-aff-row-<?php echo esc_attr( (string) $sz7af_aid ); ?>">
                            <td><?php echo esc_html( $sz7af_a['_name_display'] ); ?></td>
                            <td><?php echo esc_html( $sz7af_a['_email_display'] ); ?></td>
                            <td class="szv2-td-num szv2-num"><?php echo esc_html( number_format( $sz7af_comm, 2, ',', '' ) ); ?>%</td>
                            <td><?php $__d2 = (string)($sz7af_a['approved_at'] ?? $sz7af_a['created_at'] ?? ''); echo esc_html( $__d2 ? wp_date('d/m/Y', strtotime($__d2)) : '—' ); ?></td>
                            <?php if ( $sz7af_can_act ) : ?>
                            <td>
                                <div class="szv2-aff-comm-row">
                                    <input type="number"
                                           class="szv2-input szv2-aff-comm-input"
                                           data-affiliate-id="<?php echo esc_attr( (string) $sz7af_aid ); ?>"
                                           data-nonce="<?php echo esc_attr( $sz7af_nonce ); ?>"
                                           value="<?php echo esc_attr( number_format( $sz7af_comm, 2, '.', '' ) ); ?>"
                                           min="0" max="100" step="0.01">
                                    <button type="button"
                                            class="szv2-btn szv2-btn-sm szv2-btn-secondary szv2-aff-comm-save"
                                            data-affiliate-id="<?php echo esc_attr( (string) $sz7af_aid ); ?>"
                                            data-nonce="<?php echo esc_attr( $sz7af_nonce ); ?>">Salvar</button>
                                </div>
                            </td>
                            <td style="text-align:right">
                                <button type="button"
                                        class="szv2-btn szv2-btn-sm szv2-btn-danger"
                                        data-affiliate-id="<?php echo esc_attr( (string) $sz7af_aid ); ?>"
                                        data-aff-name="<?php echo esc_attr( $sz7af_a['_name_display'] ); ?>"
                                        data-nonce="<?php echo esc_attr( $sz7af_nonce ); ?>"
                                        onclick="szV2AffDelete(this)">Excluir</button>
                            </td>
                            <?php endif; ?>
                        </tr>
                        <?php endforeach; ?>
                    </tbody>
                </table>
            </div>
        </div>
        <?php endif; ?>
    </div>

    <!-- Painel: Repasse mensal por afiliado -->
    <?php
    // Repasse mensal por afiliado (últimos meses, somente para produtor)
    $sz_aff_repasse = [];
    if ( function_exists( 'senderzz_get_visible_orders_for_user' ) ) {
        $sz_aff_all = senderzz_get_visible_orders_for_user( $sz_v2_user, 1000 );
        foreach ( $sz_aff_all as $sz_aff_o ) {
            $aff_name = (string) ( $sz_aff_o['affiliate_name'] ?? '' );
            $comm     = (float)  ( $sz_aff_o['affiliate_commission'] ?? 0 );
            $month    = substr( (string) ( $sz_aff_o['date_machine'] ?? $sz_aff_o['date'] ?? '' ), 0, 7 );
            if ( ! $aff_name || $comm <= 0 || ! $month ) continue;
            $key = $aff_name . '|' . $month;
            $sz_aff_repasse[ $key ] = ( $sz_aff_repasse[ $key ] ?? 0 ) + $comm;
        }
        krsort( $sz_aff_repasse ); // mais recente primeiro
    }
    ?>
    <?php if ( ! empty( $sz_aff_repasse ) ) : ?>
    <div class="szv2-card" style="margin-top:20px">
        <div class="szv2-card-head">
            <div>
                <h2>Repasse mensal por afiliado</h2>
                <p class="szv2-card-sub">Comissões geradas nos últimos meses.</p>
            </div>
        </div>
        <div class="szv2-table-wrap">
            <table class="szv2-table">
                <thead><tr>
                    <th>Afiliado</th><th>Mês</th><th style="text-align:right">Total comissão</th>
                </tr></thead>
                <tbody>
                    <?php foreach ( $sz_aff_repasse as $sz_aff_key => $sz_aff_val ) :
                        [$sz_aff_name, $sz_aff_month] = explode( '|', $sz_aff_key, 2 );
                        $sz_aff_month_fmt = function_exists( 'wp_date' )
                            ? wp_date( 'm/Y', strtotime( $sz_aff_month . '-01' ) )
                            : $sz_aff_month;
                    ?>
                    <tr>
                        <td style="padding:10px 14px;font-size:13px;font-weight:600;color:var(--szv2-text)"><?php echo esc_html( $sz_aff_name ); ?></td>
                        <td style="padding:10px 14px;font-size:13px;color:var(--szv2-text-soft)"><?php echo esc_html( $sz_aff_month_fmt ); ?></td>
                        <td style="padding:10px 14px;font-size:13px;font-weight:700;color:#16a34a;text-align:right">R$ <?php echo esc_html( number_format( $sz_aff_val, 2, ',', '.' ) ); ?></td>
                    </tr>
                    <?php endforeach; ?>
                </tbody>
            </table>
        </div>
    </div>
    <?php endif; ?>

    <!-- Painel: Minha afiliação (como afiliado de outros produtores) -->
    <?php
    $sz7af_my_aff_table  = function_exists('sz_aff_table') ? sz_aff_table('sz_affiliates') : $wpdb->prefix.'sz_affiliates';
    $sz7af_my_wp_uid     = function_exists('sz_aff_portal_user_wp_id') ? (int) sz_aff_portal_user_wp_id( $sz_v2_user ) : (int)($sz_v2_user->wp_user_id ?? 0);
    $sz7af_my_aff_rows   = [];
    if ( $sz7af_my_wp_uid ) {
        $sz7af_my_aff_rows = (array) $wpdb->get_results( $wpdb->prepare(
            "SELECT a.*, u.display_name AS prod_name, u.user_email AS prod_email
             FROM {$sz7af_my_aff_table} a
             LEFT JOIN {$wpdb->users} u ON u.ID = a.producer_id
             WHERE a.user_id = %d AND a.deleted_at IS NULL
             ORDER BY FIELD(a.status,'active','pending'), a.created_at DESC LIMIT 100",
            $sz7af_my_wp_uid
        ), ARRAY_A ) ?: [];
    }
    $sz7af_my_links = [];
    if ( ! empty( $sz7af_my_aff_rows ) ) {
        $sz7af_my_links = function_exists('sz_aff_get_visible_checkout_links_for_portal_user')
            ? sz_aff_get_visible_checkout_links_for_portal_user( $sz_v2_user )
            : [];
    }
    ?>
    <div class="szv2-dash-panel" data-szv2-panel-group="affiliates-view" data-szv2-panel="minhas" hidden>
        <?php if ( empty( $sz7af_my_aff_rows ) ) : ?>
            <?php echo sz_v2_empty_state( [ 'title' => 'Nenhuma afiliação', 'text' => 'Acesse a Vitrine para se afiliar a produtos disponíveis.' ] ); // phpcs:ignore ?>
        <?php else : ?>
        <div class="szv2-card" style="margin-bottom:var(--szv2-space-4)">
            <div class="szv2-card-head"><h2>Meus vínculos como afiliado</h2></div>
            <div class="szv2-table-wrap szv2-table-flush">
                <table class="szv2-table">
                    <thead><tr><th>Produtor</th><th>Status</th><th class="szv2-td-num">Comissão</th><th>Desde</th></tr></thead>
                    <tbody>
                    <?php foreach ( $sz7af_my_aff_rows as $sz7af_mar ) :
                        $sz7af_mpname = trim( (string)($sz7af_mar['prod_name'] ?? '') ) ?: (string)($sz7af_mar['prod_email'] ?? '—');
                        $sz7af_mcomm  = (float)($sz7af_mar['commission_pct'] ?? 0);
                        $sz7af_mstat  = (string)($sz7af_mar['status'] ?? '');
                        $_sz7af_md = (string)($sz7af_mar['approved_at'] ?? $sz7af_mar['created_at'] ?? '');
                        $sz7af_msince = $_sz7af_md ? wp_date('d/m/Y', strtotime($_sz7af_md)) : '—';
                    ?>
                    <tr>
                        <td><?php echo esc_html($sz7af_mpname); ?></td>
                        <td><?php echo sz_v2_status_badge($sz7af_mstat); // phpcs:ignore ?></td>
                        <td class="szv2-td-num szv2-num"><?php echo esc_html(number_format($sz7af_mcomm, 2, ',', '')); ?>%</td>
                        <td><?php echo esc_html($sz7af_msince); ?></td>
                    </tr>
                    <?php endforeach; ?>
                    </tbody>
                </table>
            </div>
        </div>
        <?php if ( ! empty( $sz7af_my_links ) ) : ?>
        <div class="szv2-card">
            <div class="szv2-card-head"><h2>Links de venda disponíveis</h2></div>
            <div class="szv2-aff-links-list">
                <?php foreach ( $sz7af_my_links as $sz7af_mlk ) :
                    $sz7af_mlk_name = (string)($sz7af_mlk['display_name'] ?? $sz7af_mlk['name'] ?? '—');
                    $sz7af_mlk_url  = trim((string)($sz7af_mlk['affiliate_url'] ?? ''));
                    $sz7af_mlk_comm = (float)($sz7af_mlk['affiliate_commission_pct'] ?? 0);
                ?>
                <div class="szv2-aff-link-row">
                    <div class="szv2-aff-link-info">
                        <span class="szv2-aff-link-name"><?php echo esc_html($sz7af_mlk_name); ?></span>
                        <?php if ($sz7af_mlk_comm > 0) : ?>
                        <span class="szv2-link-info-label"><?php echo esc_html(number_format($sz7af_mlk_comm, 2, ',', '')); ?>% comissão</span>
                        <?php endif; ?>
                    </div>
                    <?php if ($sz7af_mlk_url !== '') : ?>
                    <button type="button" class="szv2-btn szv2-btn-sm szv2-btn-secondary szv2-link-copy-btn"
                            data-url="<?php echo esc_attr($sz7af_mlk_url); ?>" title="Copiar link">Copiar link</button>
                    <?php else : ?>
                    <span class="szv2-text-faint-val">Link indisponível</span>
                    <?php endif; ?>
                </div>
                <?php endforeach; ?>
            </div>
        </div>
        <?php endif; ?>
        <?php endif; ?>
    </div>

    <!-- Painel: Configurações (editável no V2) -->
    <div class="szv2-dash-panel" data-szv2-panel-group="affiliates-view" data-szv2-panel="config" hidden>
        <div class="szv2-card">
            <div class="szv2-card-head"><h2>Configurações do programa de afiliados</h2></div>
            <div style="display:flex;flex-direction:column;gap:0">
                <div style="display:flex;align-items:center;justify-content:space-between;padding:14px 0;border-bottom:1px solid var(--szv2-divider)">
                    <div>
                        <div style="font-size:13px;font-weight:600;color:var(--szv2-text);margin-bottom:2px">Aprovação automática</div>
                        <div style="font-size:12px;color:var(--szv2-text-muted)">Novos afiliados são aprovados automaticamente ao se cadastrar.</div>
                    </div>
                    <label class="szv2-toggle-lbl">
                        <input type="checkbox" id="szv2-aff-auto"
                               <?php checked( $sz7af_auto_approve ); ?>
                               data-nonce="<?php echo esc_attr( wp_create_nonce('senderzz_portal') ); ?>"
                               onchange="szV2AffToggleAuto(this)">
                        <span class="szv2-toggle-slider"></span>
                    </label>
                </div>
                <div style="display:flex;align-items:center;justify-content:space-between;padding:14px 0">
                    <div>
                        <div style="font-size:13px;font-weight:600;color:var(--szv2-text);margin-bottom:2px">Comissão padrão dos afiliados</div>
                        <div style="font-size:12px;color:var(--szv2-text-muted)">Aplicada automaticamente quando nenhuma comissão específica é definida no checkout.</div>
                    </div>
                    <div style="display:flex;align-items:center;gap:8px">
                        <input type="number"
                               id="szv2-aff-default-comm"
                               class="szv2-input szv2-input-sm"
                               value="<?php echo esc_attr( number_format( $sz7af_default_comm, 2, '.', '' ) ); ?>"
                               min="0" max="100" step="0.01"
                               style="width:80px;text-align:right">
                        <span style="font-size:13px;color:var(--szv2-text-muted)">%</span>
                        <button type="button"
                                class="szv2-btn szv2-btn-brand szv2-btn-sm"
                                data-nonce="<?php echo esc_attr( wp_create_nonce('senderzz_portal') ); ?>"
                                onclick="szV2AffSaveDefaultComm(this)">Salvar</button>
                    </div>
                </div>
            </div>
            <div id="szv2-aff-config-msg" style="display:none;margin-top:8px;font-size:13px"></div>
        </div>

        <!-- F4: Links de convite -->
        <div class="szv2-card" style="margin-top:var(--szv2-space-3)">
            <div class="szv2-card-head">
                <div><h2>Links de convite</h2><p class="szv2-card-sub">Compartilhe para novos afiliados se cadastrarem direto no seu programa.</p></div>
                <button type="button" class="szv2-btn szv2-btn-brand szv2-btn-sm"
                        id="szv2-aff-invite-create-btn"
                        data-nonce="<?php echo esc_attr( $sz7af_nonce ); ?>"
                        data-ajax="<?php echo esc_attr( $sz7af_ajax_url ); ?>"
                        onclick="szV2AffCreateInvite(this)">+ Gerar link</button>
            </div>
            <div id="szv2-aff-invite-list">
                <p class="szv2-td-sub" style="padding:12px 0">Carregando...</p>
            </div>
            <div id="szv2-aff-invite-msg" style="display:none;font-size:13px;margin-top:8px"></div>
        </div>
    </div>

</section>
<script>
function szV2AffDelete(btn) {
    var aid  = btn.dataset.affiliateId;
    var name = btn.dataset.affName || 'este afiliado';
    window.szV2Confirm({
        title: 'Excluir afiliado',
        message: 'Tem certeza que deseja excluir ' + name + '? Esta ação não pode ser desfeita.',
        btn: 'Excluir',
        danger: true
    }, function() {
        var sec = document.getElementById('sec-affiliates');
        var fd  = new FormData();
        fd.append('action', 'sz_aff_panel_action');
        fd.append('aff_act', 'delete_affiliate');
        fd.append('affiliate_id', aid);
        fd.append('_wpnonce', btn.dataset.nonce);
        btn.disabled = true;
        fetch(sec ? sec.dataset.szv2AjaxUrl : '', { method:'POST', body:fd, credentials:'same-origin' })
            .then(function(r){ return r.json(); })
            .then(function(d){
                if (d && d.success) {
                    var row = document.getElementById('szv2-aff-row-' + aid);
                    if (row) row.remove();
                } else {
                    window.szV2Confirm({ title: 'Erro', message: (d && d.message) ? d.message : 'Erro ao excluir.', btn: 'OK', danger: false }, function(){});
                    btn.disabled = false;
                }
            })
            .catch(function(){ btn.disabled = false; });
    });
}
function szV2AffToggleAuto(cb) {
    var fd = new FormData();
    fd.append('action', 'sz_aff_panel_action');
    fd.append('aff_act', 'toggle_auto');
    fd.append('auto', cb.checked ? '1' : '0');
    fd.append('_wpnonce', cb.dataset.nonce);
    fetch(window.ajaxurl || '', { method:'POST', body:fd, credentials:'same-origin' }).catch(function(){});
}
function szV2AffSaveDefaultComm(btn) {
    var pct = document.getElementById('szv2-aff-default-comm').value;
    var msg = document.getElementById('szv2-aff-config-msg');
    var fd  = new FormData();
    fd.append('action', 'sz_aff_panel_action');
    fd.append('aff_act', 'default_commission');
    fd.append('default_commission_pct', pct);
    fd.append('_wpnonce', btn.dataset.nonce);
    fetch(window.ajaxurl || '', { method:'POST', body:fd, credentials:'same-origin' })
        .then(function(r){ return r.json(); })
        .then(function(d){
            if (msg) { msg.style.display=''; msg.style.color=d&&d.success?'var(--szv2-success)':'var(--szv2-danger)'; msg.textContent=d&&d.message?d.message:(d&&d.success?'Salvo!':'Erro.'); setTimeout(function(){ if(msg) msg.style.display='none'; },3000); }
        });
}

// F4: convite afiliados
function szV2AffLoadInvites() {
    var list  = document.getElementById('szv2-aff-invite-list');
    var btn   = document.getElementById('szv2-aff-invite-create-btn');
    if (!list || !btn) return;
    var fd = new FormData();
    fd.append('action', 'sz_aff_panel_action');
    fd.append('aff_act', 'get_invite_links');
    fd.append('_wpnonce', btn.dataset.nonce);
    fetch(btn.dataset.ajax, { method: 'POST', body: fd, credentials: 'same-origin' })
        .then(function(r){ return r.json(); })
        .then(function(r) {
            if (!r.success || !r.links || !r.links.length) {
                list.innerHTML = '<p class="szv2-td-sub" style="padding:12px 0">Nenhum link ativo. Clique em "+ Gerar link" para criar.</p>';
                return;
            }
            var html = '<div class="szv2-table-wrap"><table class="szv2-table"><thead><tr><th>URL</th><th>Usos</th><th>Criado</th><th style="text-align:right">Ação</th></tr></thead><tbody>';
            r.links.forEach(function(l) {
                html += '<tr>'
                    + '<td style="word-break:break-all;max-width:200px"><span style="font-size:12px">' + l.url + '</span>'
                    + ' <button type="button" class="szv2-btn szv2-btn-secondary szv2-btn-sm" onclick="navigator.clipboard.writeText(\'' + l.url.replace(/'/g, "\\'") + '\');this.textContent=\'Copiado!\';setTimeout(()=>{this.textContent=\'Copiar\'},1800)" style="white-space:nowrap">Copiar</button></td>'
                    + '<td class="szv2-num">' + l.uses + '</td>'
                    + '<td class="szv2-td-sub">' + l.created_at + '</td>'
                    + '<td style="text-align:right"><button type="button" class="szv2-btn szv2-btn-danger szv2-btn-sm szv2-aff-invite-revoke" data-link-id="' + l.id + '">Revogar</button></td>'
                    + '</tr>';
            });
            html += '</tbody></table></div>';
            list.innerHTML = html;
            list.querySelectorAll('.szv2-aff-invite-revoke').forEach(function(b) {
                b.addEventListener('click', function() {
                    var btn2 = document.getElementById('szv2-aff-invite-create-btn');
                    szV2Confirm({ title: 'Revogar link', message: 'Link revogado não poderá ser usado para novos cadastros. Continuar?', btn: 'Revogar', danger: true }, function() {
                        var fd2 = new FormData();
                        fd2.append('action', 'sz_aff_panel_action');
                        fd2.append('aff_act', 'revoke_invite_link');
                        fd2.append('link_id', b.dataset.linkId);
                        fd2.append('_wpnonce', btn2 ? btn2.dataset.nonce : '');
                        fetch(btn2 ? btn2.dataset.ajax : '', { method: 'POST', body: fd2, credentials: 'same-origin' })
                            .then(function(r){ return r.json(); })
                            .then(function(r) {
                                if (typeof window.szV2Toast === 'function') window.szV2Toast(r.message || (r.success ? 'Revogado.' : 'Erro.'), r.success ? 'success' : 'danger');
                                if (r.success) szV2AffLoadInvites();
                            });
                    });
                });
            });
        })
        .catch(function() { list.innerHTML = '<p style="color:var(--szv2-danger);padding:12px 0">Erro ao carregar links.</p>'; });
}

function szV2AffCreateInvite(btn) {
    var fd = new FormData();
    fd.append('action', 'sz_aff_panel_action');
    fd.append('aff_act', 'create_invite_link');
    fd.append('_wpnonce', btn.dataset.nonce);
    btn.disabled = true;
    fetch(btn.dataset.ajax, { method: 'POST', body: fd, credentials: 'same-origin' })
        .then(function(r){ return r.json(); })
        .then(function(r) {
            btn.disabled = false;
            if (typeof window.szV2Toast === 'function') window.szV2Toast(r.message || (r.success ? 'Link criado.' : 'Erro.'), r.success ? 'success' : 'danger');
            if (r.success) szV2AffLoadInvites();
        })
        .catch(function() { btn.disabled = false; if (typeof window.szV2Toast === 'function') window.szV2Toast('Erro de conexão.', 'danger'); });
}

// Carregar invites quando a aba Config é ativada
(function() {
    document.addEventListener('DOMContentLoaded', function() {
        var configTab = document.querySelector('[data-szv2-tab="config"]');
        if (configTab) {
            configTab.addEventListener('click', function() { setTimeout(szV2AffLoadInvites, 100); });
        }
    });
}());
</script>
