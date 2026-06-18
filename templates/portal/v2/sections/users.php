<?php
/**
 * Senderzz V2 – Usuários (v455 — sub-usuários reais)
 * @var object $sz_v2_user
 */
defined( 'ABSPATH' ) || exit;

$sz9us_can_mgr = empty( $sz_v2_user->parent_user_id );
$sz9us_nonce   = wp_create_nonce( 'senderzz_portal' );
$sz9us_ajax    = admin_url( 'admin-ajax.php' );

// Carrega sub-usuários reais
global $wpdb;
$sz9us_sub_users = [];
$sz9us_table = $wpdb->prefix . 'senderzz_portal_users';
if ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $sz9us_table ) ) === $sz9us_table ) {
    $sz9us_sub_users = $wpdb->get_results( $wpdb->prepare(
        "SELECT id, name, email, role, status, created_at FROM {$sz9us_table}
          WHERE parent_user_id = %d AND status != 'deleted' ORDER BY created_at DESC LIMIT 30",
        (int) $sz_v2_user->id
    ), ARRAY_A ) ?: [];
}
?>
<section id="sec-users" class="sz-sec" data-szv2-label="Usuários">
    <?php endif; ?>

    <!-- Modal criar subusuário -->
    <?php if ( $sz9us_can_mgr ) : ?>
    <div id="szv2-usr-create-modal" class="szv2-modal-overlay" style="display:none" role="dialog" aria-modal="true">
        <div class="szv2-modal">
            <div class="szv2-modal-head"><h3>Adicionar usuário</h3><button type="button" class="szv2-modal-x" onclick="document.getElementById('szv2-usr-create-modal').style.display='none'">×</button></div>
            <div class="szv2-modal-body">
                <div class="szv2-input-group"><label class="szv2-label">Nome completo</label><input type="text" id="szv2-usr-name" class="szv2-input" placeholder="Nome do usuário"></div>
                <div class="szv2-input-group"><label class="szv2-label">E-mail</label><input type="email" id="szv2-usr-email" class="szv2-input" placeholder="email@dominio.com"></div>
                <div class="szv2-input-group"><label class="szv2-label">Senha inicial</label><input type="password" id="szv2-usr-pw" class="szv2-input" placeholder="Mínimo 8 caracteres"></div>
                <div id="szv2-usr-err" style="display:none;color:var(--szv2-danger);font-size:13px"></div>
            </div>
            <div class="szv2-modal-foot">
                <button type="button" class="szv2-btn szv2-btn-secondary" onclick="document.getElementById('szv2-usr-create-modal').style.display='none'">Cancelar</button>
                <button type="button" class="szv2-btn szv2-btn-brand"
                        data-ajax="<?php echo esc_attr( $sz9us_ajax ); ?>"
                        data-nonce="<?php echo esc_attr( $sz9us_nonce ); ?>"
                        onclick="szV2UsrCreate(this)">Adicionar</button>
            </div>
        </div>
    </div>
    <?php endif; ?>
</section>
