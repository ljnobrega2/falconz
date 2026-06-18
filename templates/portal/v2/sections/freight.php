<?php
/**
 * Senderzz V2 – Frete / Logística (v456 — Preferidas + Bloqueadas com checkboxes reais)
 * Usa senderzz_portal_carrier_methods(), get_preferred_carrier_ids(), get_blocked_carrier_ids()
 * Salva via szaction=set_preferred_carrier e szaction=set_blocked_carrier
 * @var object $sz_v2_user
 */
defined( 'ABSPATH' ) || exit;

$sz9fr_nonce  = wp_create_nonce( 'senderzz_portal' );
$sz9fr_ajax   = admin_url( 'admin-ajax.php' );

// Classe primária
$sz9fr_class_ids = function_exists( 'sz_get_user_class_ids' ) ? sz_get_user_class_ids( $sz_v2_user ) : [];
$sz9fr_primary   = (int) ( $sz9fr_class_ids[0] ?? $sz_v2_user->shipping_class_id ?? 0 );

// Remetente
$sz9fr_sender_row = [];
if ( $sz9fr_primary > 0 ) {
    if ( function_exists( 'senderzz_sender_get_for_class' ) ) {
        $sz9fr_sender_row = (array) senderzz_sender_get_for_class( $sz9fr_primary );
    }
    if ( empty( $sz9fr_sender_row['name'] ) ) {
        $global = get_option( 'woocommerce_wc-melhor-envio_settings', [] );
        $g_addr = is_array( $global['address'] ?? null ) ? $global['address'] : [];
        $sz9fr_sender_row['name']      = $sz9fr_sender_row['name']      ?? (string) ( $g_addr['name']      ?? '' );
        $sz9fr_sender_row['document']  = $sz9fr_sender_row['document']  ?? (string) ( $g_addr['document']  ?? $g_addr['company_document'] ?? '' );
        $sz9fr_sender_row['telephone'] = $sz9fr_sender_row['telephone'] ?? (string) ( $g_addr['phone']     ?? '' );
    }
}

// Transportadoras reais (groupadas por empresa)
$sz9fr_carriers = function_exists( 'senderzz_portal_carrier_methods' )
    ? senderzz_portal_carrier_methods()
    : [];

// IDs preferidas e bloqueadas já configuradas
$sz9fr_preferred_ids = [];
$sz9fr_blocked_ids   = [];
if ( $sz9fr_primary > 0 ) {
    if ( function_exists( 'senderzz_portal_preferred_carrier_ids' ) ) {
        $sz9fr_preferred_ids = array_map( 'intval', senderzz_portal_preferred_carrier_ids( $sz9fr_primary ) );
    }
    if ( function_exists( 'senderzz_portal_blocked_carrier_ids' ) ) {
        $sz9fr_blocked_ids = array_map( 'intval', senderzz_portal_blocked_carrier_ids( $sz9fr_primary ) );
    }
}
?>
<section id="sec-freight" class="sz-sec" data-szv2-label="Frete">
    <div class="szv2-prod-subtabs" role="tablist">
        <button type="button" class="szv2-prod-subtab szv2-prod-subtab--active" data-szv2-conn-tab="fr-favoritas">Favoritas</button>
        <button type="button" class="szv2-prod-subtab" data-szv2-conn-tab="fr-bloqueadas">Bloqueadas</button>
    </div>

    <!-- Remetente -->
    <!-- Remetente -->
    <div class="szv2-conn-panel szv2-prod-sub--hidden" id="szv2-panel-fr-remetente">
        <div class="szv2-card">
            <div class="szv2-card-head">
                <div>
                    <h2>Dados do remetente</h2>
                    <p class="szv2-card-sub">Informações utilizadas para etiquetas de envio.</p>
                </div>
                <button type="button" class="szv2-btn szv2-btn-secondary szv2-btn-sm"
                        data-szv2-go="settings">Editar em Configurações</button>
            </div>
            <div class="szv2-info-grid">
                <div class="szv2-info-cell">
                    <div class="szv2-info-label">Nome / Razão Social</div>
                    <div class="szv2-info-value"><?php echo esc_html( (string) ( $sz9fr_sender_row['name'] ?? '—' ) ); ?></div>
                </div>
                <div class="szv2-info-cell">
                    <div class="szv2-info-label">CNPJ / CPF</div>
                    <div class="szv2-info-value szv2-num"><?php echo esc_html( (string) ( $sz9fr_sender_row['document'] ?? '—' ) ); ?></div>
                </div>
                <div class="szv2-info-cell">
                    <div class="szv2-info-label">Telefone</div>
                    <?php
                    $sz9fr_phone = '';
                    $sz9fr_wp_uid = (int) ( $sz_v2_user->wp_user_id ?? 0 );
                    if ( $sz9fr_wp_uid ) {
                        $sz9fr_phone = (string) get_user_meta( $sz9fr_wp_uid, 'billing_phone', true );
                        if ( ! $sz9fr_phone ) $sz9fr_phone = (string) get_user_meta( $sz9fr_wp_uid, 'phone', true );
                    }
                    if ( ! $sz9fr_phone ) $sz9fr_phone = (string) ( $sz9fr_sender_row['telephone'] ?? '' );
                    ?>
                    <div class="szv2-info-value"><?php echo esc_html( $sz9fr_phone ?: '—' ); ?></div>
                </div>
            </div>
            <p style="margin-top:12px;font-size:13px;color:var(--szv2-text-muted)">
                Para alterar estes dados, acesse <strong>Configurações → Conta</strong> ou abra um chamado em <strong>Suporte</strong>.
            </p>
        </div>
    </div>

    <!-- Favoritas -->
    <div class="szv2-conn-panel" id="szv2-panel-fr-favoritas">
        <div class="szv2-card">
            <div class="szv2-card-head">
                <div>
                    <h2>Modalidades de frete favoritas</h2>
                    <p class="szv2-card-sub">Escolha as opções priorizadas. O Senderzz recomenda a menor entre as selecionadas.</p>
                </div>
            </div>
            <?php if ( empty( $sz9fr_carriers ) ) : ?>
                <?php // phpcs:ignore WordPress.Security.EscapeOutput
                echo sz_v2_empty_state( [ 'title' => 'Nenhuma transportadora disponível', 'text' => 'As transportadoras são configuradas pelo operador Senderzz.' ] ); ?>
            <?php else : ?>
            <label class="szv2-fr-cheapest-toggle">
                <input type="checkbox" id="szv2-fr-pref-cheapest"
                       <?php checked( empty( $sz9fr_preferred_ids ) ); ?>
                       onchange="szV2FrAutoCheck(this,'pref')">
                <span>Mais barata entre todas as modalidades disponíveis</span>
            </label>
            <div class="szv2-fr-carrier-grid" id="szv2-fr-pref-grid">
                <?php foreach ( $sz9fr_carriers as $sz9fr_company => $sz9fr_methods ) : ?>
                <div class="szv2-fr-carrier-card">
                    <div class="szv2-fr-carrier-name"><?php echo esc_html( $sz9fr_company ); ?></div>
                    <?php foreach ( $sz9fr_methods as $sz9fr_mid => $sz9fr_m ) : ?>
                    <label class="szv2-fr-method-label">
                        <input type="checkbox"
                               class="szv2-fr-pref-cb"
                               name="fr_pref[]"
                               value="<?php echo esc_attr( (string) $sz9fr_mid ); ?>"
                               <?php checked( in_array( (int) $sz9fr_mid, $sz9fr_preferred_ids, true ) ); ?>>
                        <span class="szv2-fr-method-name"><?php echo esc_html( (string) ( $sz9fr_m['name'] ?? $sz9fr_m['title'] ?? '—' ) ); ?></span>
                    </label>
                    <?php endforeach; ?>
                </div>
                <?php endforeach; ?>
            </div>
            <div style="margin-top:16px;display:flex;align-items:center;gap:12px">
                <button type="button" class="szv2-btn szv2-btn-brand"
                        data-ajax="<?php echo esc_attr( $sz9fr_ajax ); ?>"
                        data-nonce="<?php echo esc_attr( $sz9fr_nonce ); ?>"
                        data-action="fr-save" data-fr-type="pref">
                    Salvar modalidades
                </button>
                <div id="szv2-fr-pref-msg" style="font-size:13px;display:none"></div>
            </div>
            <p style="margin-top:10px;font-size:12px;color:var(--szv2-text-muted)">
                Selecionando mais de uma, o Senderzz mantém somente essas modalidades e recomenda a menor disponível.
            </p>
            <?php endif; ?>
        </div>
    </div>

    <!-- Bloqueadas -->
    <div class="szv2-conn-panel szv2-prod-sub--hidden" id="szv2-panel-fr-bloqueadas">
        <div class="szv2-card">
            <div class="szv2-card-head">
                <div>
                    <h2>Transportadoras bloqueadas</h2>
                    <p class="szv2-card-sub">As modalidades marcadas aqui NÃO aparecem no checkout nem entram na recomendação.</p>
                </div>
            </div>
            <?php if ( empty( $sz9fr_carriers ) ) : ?>
                <?php // phpcs:ignore WordPress.Security.EscapeOutput
                echo sz_v2_empty_state( [ 'title' => 'Nenhuma transportadora disponível', 'text' => '' ] ); ?>
            <?php else : ?>
            <div class="szv2-fr-carrier-grid" id="szv2-fr-blk-grid">
                <?php foreach ( $sz9fr_carriers as $sz9fr_company => $sz9fr_methods ) : ?>
                <div class="szv2-fr-carrier-card">
                    <div class="szv2-fr-carrier-name"><?php echo esc_html( $sz9fr_company ); ?></div>
                    <?php foreach ( $sz9fr_methods as $sz9fr_mid => $sz9fr_m ) : ?>
                    <label class="szv2-fr-method-label">
                        <input type="checkbox"
                               class="szv2-fr-blk-cb"
                               name="fr_blk[]"
                               value="<?php echo esc_attr( (string) $sz9fr_mid ); ?>"
                               <?php checked( in_array( (int) $sz9fr_mid, $sz9fr_blocked_ids, true ) ); ?>>
                        <span class="szv2-fr-method-name"><?php echo esc_html( (string) ( $sz9fr_m['name'] ?? $sz9fr_m['title'] ?? '—' ) ); ?></span>
                    </label>
                    <?php endforeach; ?>
                </div>
                <?php endforeach; ?>
            </div>
            <div style="margin-top:16px;display:flex;align-items:center;gap:12px">
                <button type="button" class="szv2-btn szv2-btn-brand"
                        data-ajax="<?php echo esc_attr( $sz9fr_ajax ); ?>"
                        data-nonce="<?php echo esc_attr( $sz9fr_nonce ); ?>"
                        data-action="fr-save" data-fr-type="blk">
                    Salvar bloqueios
                </button>
                <div id="szv2-fr-blk-msg" style="font-size:13px;display:none"></div>
            </div>
            <p style="margin-top:10px;font-size:12px;color:var(--szv2-text-muted)">
                Bloqueio tem prioridade sobre modalidades permitidas. Modalidades marcadas aqui não aparecem no checkout.
            </p>
            <?php endif; ?>
        </div>
    </div>
</section>
<script>
(function(){
    var root = document.getElementById('sec-freight');
    if (!root) return;
    root.addEventListener('click', function(e) {
        var tab = e.target.closest('[data-szv2-conn-tab]');
        if (tab) { szV2ConnTab(tab.getAttribute('data-szv2-conn-tab'), tab); return; }
        var go = e.target.closest('[data-szv2-go]');
        if (go && typeof window.szGo === 'function') { szGo(go.getAttribute('data-szv2-go')); return; }
        var fr = e.target.closest('[data-action="fr-save"]');
        if (fr && typeof window.szV2FrSave === 'function') { szV2FrSave(fr.getAttribute('data-fr-type'), fr); return; }
    });
}());
</script>
