<?php
/**
 * Senderzz V2 – Configurações (v456 — sub-abas completas: Conta / Saques / Notificações / Taxas & Prazos / Usuários)
 * @var object $sz_v2_user
 */
defined( 'ABSPATH' ) || exit;

$sz9st_nonce  = wp_create_nonce( 'senderzz_portal' );
$sz9st_ajax   = admin_url( 'admin-ajax.php' );
$sz9st_wp_uid = (int) ( $sz_v2_user->wp_user_id ?? 0 );
$sz9st_can_mgr = empty( $sz_v2_user->parent_user_id );

$sz9st_name    = (string) ( $sz_v2_user->name  ?? '' );
$sz9st_email   = (string) ( $sz_v2_user->email ?? '' );
$sz9st_role_raw = strtolower( trim( (string) ( $sz_v2_user->role ?? 'client' ) ) );
$sz9st_role_labels = [
    'affiliate' => 'Afiliado', 'afiliado' => 'Afiliado',
    'operator' => 'Operador Logístico', 'operador' => 'Operador Logístico',
    'operador_logistico' => 'Operador Logístico', 'logistics_operator' => 'Operador Logístico',
    'admin' => 'Admin', 'client' => 'Produtor', 'producer' => 'Produtor',
];
$sz9st_role = $sz9st_role_labels[ $sz9st_role_raw ] ?? ucfirst( $sz9st_role_raw );
$sz9st_is_producer = in_array( $sz9st_role_raw, ['client','producer',''], true );
$sz9st_class   = (string) ( $sz_v2_user->shipping_class_id ?? '' );
$sz9st_mb_act  = ! empty( $sz_v2_user->motoboy_active );
$sz9st_exp_act = ! empty( $sz_v2_user->expedicao_active );
// 2FA state (default 1 = ativo, conforme coluna DB)
$sz9st_2fa_on  = ! isset( $sz_v2_user->require_2fa ) || (int) $sz_v2_user->require_2fa !== 0;

// PIX accounts
global $wpdb;
$sz9st_pix_accounts = [];
if ( $sz9st_wp_uid ) {
    $pix_raw = get_user_meta( $sz9st_wp_uid, '_sz_pix_accounts', true );
    if ( is_array( $pix_raw ) ) $sz9st_pix_accounts = $pix_raw;
}

// Sub-usuários
$sz9st_sub_users = [];
$sz9st_pu_table  = $wpdb->prefix . 'senderzz_portal_users';
if ( $sz9st_can_mgr && $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $sz9st_pu_table ) ) === $sz9st_pu_table ) {
    $sz9st_sub_users = $wpdb->get_results( $wpdb->prepare(
        "SELECT id, name, email, role, status, created_at FROM {$sz9st_pu_table}
          WHERE parent_user_id = %d AND status != 'deleted' ORDER BY created_at DESC LIMIT 30",
        (int) $sz_v2_user->id
    ), ARRAY_A ) ?: [];
}

// Taxas
$sz9st_taxa_saque   = 2.99;
$sz9st_taxa_antecip = 4.99;
$sz9st_taxa_frustr  = 8.00;
$sz9st_retencao     = 7;
if ( $sz9st_wp_uid ) {
    if ( function_exists( 'sz_mb_calcular_taxa_produtor' ) ) {
        $tx = sz_mb_calcular_taxa_produtor( $sz9st_wp_uid, 0 );
        // Não expõe taxa_entrega/manuseio — sensível. Só o que o produtor vê no perfil.
    }
    $sz9st_retencao   = (int) get_user_meta( $sz9st_wp_uid, 'sz_aff_retention_days', true ) ?: 7;
    $sz9st_taxa_saque = (float) get_user_meta( $sz9st_wp_uid, '_sz_withdrawal_fee', true ) ?: 2.99;
}

// Documento e nome empresarial
$sz9st_document_raw = $sz9st_wp_uid ? (string) get_user_meta( $sz9st_wp_uid, 'sz_document', true ) : '';
$sz9st_doc_digits   = preg_replace( '/\D/', '', $sz9st_document_raw );
$sz9st_is_cnpj      = strlen( $sz9st_doc_digits ) === 14;
$sz9st_is_cpf       = strlen( $sz9st_doc_digits ) === 11;
if ( $sz9st_is_cnpj ) {
    $sz9st_doc_fmt   = preg_replace( '/(\d{2})(\d{3})(\d{3})(\d{4})(\d{2})/', '$1.$2.$3/$4-$5', $sz9st_doc_digits );
    $sz9st_doc_label = 'CNPJ';
} elseif ( $sz9st_is_cpf ) {
    $sz9st_doc_fmt   = preg_replace( '/(\d{3})(\d{3})(\d{3})(\d{2})/', '$1.$2.$3-$4', $sz9st_doc_digits );
    $sz9st_doc_label = 'CPF';
} else {
    $sz9st_doc_fmt   = $sz9st_document_raw ?: '—';
    $sz9st_doc_label = 'CPF / CNPJ';
}
$sz9st_company             = $sz9st_wp_uid ? (string) get_user_meta( $sz9st_wp_uid, 'billing_company', true ) : '';
$sz9st_display_name_label  = $sz9st_is_cnpj ? 'Nome empresarial (se aplicável)' : 'Nome';
$sz9st_display_name_value  = ( $sz9st_is_cnpj && $sz9st_company !== '' ) ? $sz9st_company : $sz9st_name;
// Telefone - formatted BR style, strip +55 prefix
$sz9st_phone_raw = $sz9st_wp_uid ? (string) get_user_meta( $sz9st_wp_uid, 'billing_phone', true ) : '';
if ( ! $sz9st_phone_raw && $sz9st_wp_uid ) {
    $sz9st_phone_raw = (string) get_user_meta( $sz9st_wp_uid, 'phone', true );
}
if ( ! $sz9st_phone_raw ) {
    $sz9st_phone_raw = (string) ( $sz_v2_user->telephone ?? $sz_v2_user->phone ?? '' );
}
// Strip +55 country code and format as (XX) XXXXX-XXXX
$sz9st_phone_digits = preg_replace( '/\D/', '', $sz9st_phone_raw );
if ( strlen( $sz9st_phone_digits ) > 11 && substr( $sz9st_phone_digits, 0, 2 ) === '55' ) {
    $sz9st_phone_digits = substr( $sz9st_phone_digits, 2 );
}
if ( strlen( $sz9st_phone_digits ) === 11 ) {
    $sz9st_phone = '(' . substr( $sz9st_phone_digits, 0, 2 ) . ') ' . substr( $sz9st_phone_digits, 2, 5 ) . '-' . substr( $sz9st_phone_digits, 7 );
} elseif ( strlen( $sz9st_phone_digits ) === 10 ) {
    $sz9st_phone = '(' . substr( $sz9st_phone_digits, 0, 2 ) . ') ' . substr( $sz9st_phone_digits, 2, 4 ) . '-' . substr( $sz9st_phone_digits, 6 );
} else {
    $sz9st_phone = $sz9st_phone_raw ?: '—';
}
?>
<section id="sec-settings" class="sz-sec" data-szv2-label="Configurações"
         data-ajax="<?php echo esc_attr( $sz9st_ajax ); ?>"
         data-nonce="<?php echo esc_attr( $sz9st_nonce ); ?>"
         data-szv2-ajax-url="<?php echo esc_attr( $sz9st_ajax ); ?>"
         data-szv2-nonce="<?php echo esc_attr( $sz9st_nonce ); ?>">
    <div class="szv2-prod-subtabs" role="tablist">
        <button type="button" class="szv2-prod-subtab szv2-prod-subtab--active" role="tab" aria-selected="true" onclick="szV2ConnTab('st-conta',this)">Conta</button>
        <button type="button" class="szv2-prod-subtab" role="tab" aria-selected="false" onclick="szV2ConnTab('st-saques',this)">Saques</button>
        <button type="button" class="szv2-prod-subtab" role="tab" aria-selected="false" onclick="szV2ConnTab('st-notif',this)">Notificações</button>
        <button type="button" class="szv2-prod-subtab" role="tab" aria-selected="false" onclick="szV2ConnTab('st-taxas',this)">Taxas & Prazos</button>
        <button type="button" class="szv2-prod-subtab" role="tab" aria-selected="false" onclick="szV2ConnTab('st-support',this)">Suporte</button>
    </div>

    <!-- Conta -->
    <div class="szv2-conn-panel" id="szv2-panel-st-conta">
        <div class="szv2-card">
            <div class="szv2-card-head"><h2>Sua conta</h2></div>
            <div style="display:grid;grid-template-columns:repeat(5,minmax(0,1fr));gap:12px">
                <div class="szv2-info-cell">
                    <div class="szv2-info-label"><?php echo esc_html( $sz9st_display_name_label ); ?></div>
                    <div class="szv2-info-value"><?php echo esc_html( $sz9st_display_name_value ); ?></div>
                </div>
                <div class="szv2-info-cell">
                    <div class="szv2-info-label"><?php echo esc_html( $sz9st_doc_label ); ?></div>
                    <div class="szv2-info-value szv2-num"><?php echo esc_html( $sz9st_doc_fmt ); ?></div>
                </div>
                <div class="szv2-info-cell">
                    <div class="szv2-info-label">E-mail</div>
                    <div class="szv2-info-value"><?php echo esc_html( $sz9st_email ); ?></div>
                </div>
                <div class="szv2-info-cell">
                    <div class="szv2-info-label">Telefone</div>
                    <div class="szv2-info-value"><?php echo esc_html( $sz9st_phone ); ?></div>
                </div>
                <div class="szv2-info-cell">
                    <div class="szv2-info-label">Perfil</div>
                    <div class="szv2-info-value"><?php echo esc_html( $sz9st_role ); ?></div>
                </div>
            </div>
        </div>

        <div class="szv2-card">
            <div class="szv2-card-head"><h2>Dados de acesso</h2></div>
            <div class="szv2-dash-row" style="align-items:flex-start">
                <div>
                    <p style="font-size:13px;font-weight:600;color:var(--szv2-text);margin:0 0 4px">Alterar e-mail</p>
                    <p style="font-size:12px;color:var(--szv2-text-muted);margin:0 0 12px">Ao alterar, você precisará fazer login novamente com o novo e-mail.</p>
                    <div class="szv2-input-group">
                        <label class="szv2-label">Novo e-mail</label>
                        <input type="text" id="szv2-st-email" class="szv2-input" placeholder="novo@email.com"
                               autocomplete="off" readonly onfocus="this.removeAttribute('readonly')"
                               data-ajax="<?php echo esc_attr( $sz9st_ajax ); ?>"
                               data-nonce="<?php echo esc_attr( $sz9st_nonce ); ?>">
                    </div>
                    <div class="szv2-input-group">
                        <label class="szv2-label">Confirmar novo e-mail</label>
                        <input type="text" id="szv2-st-email-confirm" class="szv2-input" placeholder="repita o novo e-mail"
                               autocomplete="off" readonly onfocus="this.removeAttribute('readonly')">
                    </div>
                    <p style="font-size:12px;color:var(--szv2-text-muted);margin:0 0 8px">Você será desconectado após alterar o e-mail.</p>
                    <div id="szv2-st-email-msg" style="display:none;font-size:13px;margin-bottom:8px"></div>
                    <button type="button" class="szv2-btn szv2-btn-brand" onclick="szV2StSaveEmail()">Atualizar e-mail</button>
                </div>
                <div>
                    <p style="font-size:13px;font-weight:600;color:var(--szv2-text);margin:0 0 4px">Alterar senha</p>
                    <p style="font-size:12px;color:var(--szv2-text-muted);margin:0 0 12px">Use uma senha forte com no mínimo 8 caracteres.</p>
                    <div class="szv2-input-group"><label class="szv2-label">Senha atual</label><input type="password" id="szv2-st-pw-cur" class="szv2-input" placeholder="Senha atual" autocomplete="off" readonly onfocus="this.removeAttribute('readonly')"></div>
                    <div class="szv2-input-group"><label class="szv2-label">Nova senha</label><input type="password" id="szv2-st-pw-new" class="szv2-input" placeholder="Nova senha" autocomplete="off" readonly onfocus="this.removeAttribute('readonly')"></div>
                    <p style="font-size:12px;color:var(--szv2-text-muted);margin:0 0 8px">Você será desconectado após alterar a senha.</p>
                    <div id="szv2-st-pw-msg" style="display:none;font-size:13px;margin-bottom:8px"></div>
                    <button type="button" class="szv2-btn szv2-btn-brand" onclick="szV2StSavePw()">Alterar senha</button>
                </div>
            </div>
            <!-- 2FA — posicionado entre alterar e-mail e alterar senha, alinhado às duas colunas acima -->
            <div style="display:flex;align-items:center;justify-content:space-between;margin-top:var(--szv2-space-4);padding-top:var(--szv2-space-4);border-top:1px solid var(--szv2-divider)">
                <div>
                    <p style="font-size:13px;font-weight:600;color:var(--szv2-text);margin:0 0 2px">Verificação em duas etapas (2FA)</p>
                    <p style="font-size:12px;color:var(--szv2-text-muted);margin:0">Código por e-mail a cada login. Recomendado para proteger o acesso ao painel.</p>
                </div>
                <label class="szv2-toggle-lbl">
                    <input type="checkbox" id="szv2-st-2fa"
                           <?php checked( $sz9st_2fa_on ); ?>
                           data-ajax="<?php echo esc_attr( $sz9st_ajax ); ?>"
                           data-nonce="<?php echo esc_attr( $sz9st_nonce ); ?>"
                           onchange="szV2Toggle2FA(this)">
                    <span class="szv2-toggle-slider"></span>
                </label>
            </div>
        </div>
    </div>

    <!-- Saques (PIX) -->
    <div class="szv2-conn-panel szv2-prod-sub--hidden" id="szv2-panel-st-saques">
        <div class="szv2-card">
            <div class="szv2-card-head"><div><h2>Contas para saque</h2><p class="szv2-card-sub">Cadastre até 3 contas PIX. O CPF deve pertencer ao titular da conta.</p></div></div>
            <div class="szv2-fr-carrier-grid" style="grid-template-columns:repeat(2,minmax(0,1fr))">
                <div class="szv2-input-group"><label class="szv2-label">Titular da conta</label><input type="text" id="szv2-pix-name" class="szv2-input" placeholder="Nome completo do titular" autocomplete="off"></div>
                <div class="szv2-input-group"><label class="szv2-label">CPF do titular</label><input type="text" id="szv2-pix-cpf" class="szv2-input" placeholder="000.000.000-00" autocomplete="off"></div>
                <div class="szv2-input-group"><label class="szv2-label">Banco</label><input type="text" id="szv2-pix-banco" class="szv2-input" placeholder="Ex: Nubank, Itaú..." autocomplete="off"></div>
                <div class="szv2-input-group"><label class="szv2-label">Tipo de chave PIX</label>
                    <select id="szv2-pix-tipo" class="szv2-input">
                        <option value="CPF">CPF</option><option value="email">E-mail</option>
                        <option value="telefone">Telefone</option><option value="aleatoria">Chave aleatória</option>
                    </select>
                </div>
                <div class="szv2-input-group" style="grid-column:span 2"><label class="szv2-label">Conteúdo da chave PIX</label><input type="text" id="szv2-pix-chave" class="szv2-input" placeholder="CPF, e-mail, telefone ou chave aleatória" autocomplete="off"></div>
            </div>
            <div id="szv2-pix-msg" style="display:none;font-size:13px;margin-bottom:8px"></div>
            <button type="button" class="szv2-btn szv2-btn-brand" onclick="szV2PixAdd()">Adicionar conta</button>

            <?php if ( ! empty( $sz9st_pix_accounts ) ) : ?>
            <div style="margin-top:20px;display:flex;flex-direction:column;gap:8px">
                <div class="szv2-info-label">Contas cadastradas</div>
                <?php foreach ( $sz9st_pix_accounts as $sz9st_pix ) : ?>
                <div style="display:flex;align-items:center;gap:12px;padding:10px 14px;background:var(--szv2-surface-alt);border:1px solid var(--szv2-border);border-radius:var(--szv2-radius-md)">
                    <div>
                        <div style="font-size:13px;font-weight:600;color:var(--szv2-text)"><?php echo esc_html( (string) ( $sz9st_pix['titular'] ?? $sz9st_pix['name'] ?? '—' ) ); ?></div>
                        <div style="font-size:12px;color:var(--szv2-text-muted)"><?php echo esc_html( ucfirst( (string) ( $sz9st_pix['tipo_pix'] ?? '' ) ) ); ?>: <?php echo esc_html( function_exists( 'sz_portal_v2_mask_pix' ) ? sz_portal_v2_mask_pix( (string) ( $sz9st_pix['chave'] ?? '' ) ) : '••••' ); ?></div>
                    </div>
                    <span style="margin-left:auto;font-size:12px;color:var(--szv2-text-muted)"><?php echo esc_html( (string) ( $sz9st_pix['banco'] ?? '' ) ); ?></span>
                </div>
                <?php endforeach; ?>
            </div>
            <?php else : ?>
            <p style="margin-top:16px;font-size:13px;color:var(--szv2-text-muted)">Nenhuma conta cadastrada. Adicione uma conta PIX para solicitar saques.</p>
            <?php endif; ?>
        </div>
    </div>

    <!-- Notificações -->
    <div class="szv2-conn-panel szv2-prod-sub--hidden" id="szv2-panel-st-notif">
        <div class="szv2-card">
            <div class="szv2-card-head">
                <div><h2>Notificações push</h2><p class="szv2-card-sub">Receba alertas no navegador quando houver atividade nos pedidos.</p></div>
            </div>
            <div style="display:flex;flex-direction:column;gap:0">
                <?php
                $sz9st_notif_events = [
                    'agendado'    => [ 'Agendamento',   'Pedido agendado com o motoboy.' ],
                    'embalado'    => [ 'Pedido embalado', 'Embalado e aguardando coleta.' ],
                    'acaminho'    => [ 'Em rota',        'Motoboy saiu para entrega.' ],
                    'entregue'    => [ 'Entregue',       'Entrega confirmada com sucesso.' ],
                    'frustrado'   => [ 'Frustrado',      'Tentativa de entrega não concluída.' ],
                    'pedido_novo' => [ 'Novo pedido',    'Novo pedido recebido na plataforma.' ],
                    'enviado'     => [ 'Pedido enviado', 'Etiqueta gerada e pedido despachado.' ],
                ];
                $sz9st_notif_last = array_key_last( $sz9st_notif_events );
                foreach ( $sz9st_notif_events as $sz9st_ev => $sz9st_info ) :
                    $sz9st_notif_val = $sz9st_wp_uid
                        ? (bool) get_user_meta( $sz9st_wp_uid, '_sz_notif_' . $sz9st_ev, true )
                        : true;
                    $sz9st_is_last = ( $sz9st_ev === $sz9st_notif_last );
                ?>
                <div style="display:flex;align-items:center;justify-content:space-between;padding:14px 0;<?php echo $sz9st_is_last ? '' : 'border-bottom:1px solid var(--szv2-divider)'; ?>">
                    <div>
                        <div style="font-size:13px;font-weight:600;color:var(--szv2-text);margin-bottom:2px"><?php echo esc_html( $sz9st_info[0] ); ?></div>
                        <div style="font-size:12px;color:var(--szv2-text-muted)"><?php echo esc_html( $sz9st_info[1] ); ?></div>
                    </div>
                    <label class="szv2-toggle-lbl">
                        <input type="checkbox"
                               class="szv2-notif-cb"
                               data-event="<?php echo esc_attr( $sz9st_ev ); ?>"
                               data-nonce="<?php echo esc_attr( $sz9st_nonce ); ?>"
                               <?php checked( $sz9st_notif_val ); ?>
                               onchange="szV2NotifSave(this)">
                        <span class="szv2-toggle-slider"></span>
                    </label>
                </div>
                <?php endforeach; ?>
            </div>
        </div>
    </div>

    <!-- Taxas & Prazos -->
    <div class="szv2-conn-panel szv2-prod-sub--hidden" id="szv2-panel-st-taxas">

        <!-- Prazo de recebíveis -->
        <div class="szv2-card">
            <div class="szv2-card-head">
                <h2>Prazo de recebíveis</h2>
                <span class="szv2-card-sub">Tempo para crédito aparecer como disponível para saque.</span>
            </div>
            <div style="display:grid;grid-template-columns:1fr 1fr;gap:12px;margin-bottom:16px">
                <div style="background:var(--szv2-surface-alt);border-radius:var(--szv2-radius-md);padding:16px 20px">
                    <div style="font-size:12px;color:var(--szv2-text-muted);margin-bottom:6px">Retenção após entrega</div>
                    <div style="font-size:28px;font-weight:700;color:var(--szv2-brand);line-height:1"><?php echo esc_html( $sz9st_retencao ); ?><span style="font-size:14px;font-weight:500;margin-left:4px">dias</span></div>
                    <div style="font-size:12px;color:var(--szv2-text-muted);margin-top:4px">após entrega confirmada</div>
                </div>
                <div style="background:var(--szv2-surface-alt);border-radius:var(--szv2-radius-md);padding:16px 20px;display:flex;align-items:center">
                    <p style="font-size:13px;color:var(--szv2-text-muted);margin:0;line-height:1.5">O prazo protege contra chargebacks. Ao final, o saldo muda de <strong style="color:var(--szv2-text)">Pendente</strong> para <strong style="color:var(--szv2-success)">Disponível</strong> automaticamente.</p>
                </div>
            </div>
        </div>

        <!-- Taxas de saque -->
        <div class="szv2-card">
            <div class="szv2-card-head">
                <h2>Taxas de saque</h2>
            </div>
            <div style="display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:12px;margin-bottom:16px">
                <div style="background:var(--szv2-surface-alt);border-radius:var(--szv2-radius-md);padding:16px 20px">
                    <div style="font-size:12px;color:var(--szv2-text-muted);margin-bottom:6px">Taxa de saque padrão</div>
                    <div style="font-size:26px;font-weight:700;color:var(--szv2-brand);line-height:1">R$&nbsp;<?php echo esc_html( number_format( $sz9st_taxa_saque, 2, ',', '.' ) ); ?></div>
                    <div style="font-size:12px;color:var(--szv2-text-muted);margin-top:4px">por saque aprovado</div>
                </div>
                <div style="background:var(--szv2-surface-alt);border-radius:var(--szv2-radius-md);padding:16px 20px">
                    <div style="font-size:12px;color:var(--szv2-text-muted);margin-bottom:6px">Taxa de antecipação</div>
                    <div style="font-size:26px;font-weight:700;color:var(--szv2-brand);line-height:1"><?php echo esc_html( number_format( $sz9st_taxa_antecip, 2, ',', '.' ) ); ?><span style="font-size:16px;margin-left:2px">%</span></div>
                    <div style="font-size:12px;color:var(--szv2-text-muted);margin-top:4px">sobre o valor antecipado</div>
                </div>
                <div style="background:var(--szv2-surface-alt);border-radius:var(--szv2-radius-md);padding:16px 20px">
                    <div style="font-size:12px;color:var(--szv2-text-muted);margin-bottom:6px">Taxa de frustração</div>
                    <div style="font-size:26px;font-weight:700;color:var(--szv2-brand);line-height:1">R$&nbsp;<?php echo esc_html( number_format( $sz9st_taxa_frustr, 2, ',', '.' ) ); ?></div>
                    <div style="font-size:12px;color:var(--szv2-text-muted);margin-top:4px">1ª ocorrência grátis</div>
                </div>
            </div>
            <div style="padding:12px 14px;background:var(--szv2-surface-alt);border-left:3px solid var(--szv2-border);border-radius:0 var(--szv2-radius-sm) var(--szv2-radius-sm) 0;font-size:12px;color:var(--szv2-text-muted)">
                Taxas e prazos podem variar conforme a configuração comercial da conta. Sempre verifique o valor líquido na carteira antes de solicitar saque.
            </div>
        </div>
    </div>

    <!-- Usuários -->
    <div class="szv2-conn-panel szv2-prod-sub--hidden" id="szv2-panel-st-users">
        <div class="szv2-fr-carrier-grid" style="grid-template-columns:380px 1fr">
            <!-- Novo acesso -->
            <div class="szv2-card">
                <div class="szv2-card-head"><div><h2>Novo acesso</h2><p class="szv2-card-sub">Cadastre uma pessoa da equipe e defina as permissões.</p></div></div>
                <div class="szv2-input-group"><label class="szv2-label">E-mail</label><input type="email" id="szv2-usr-email" class="szv2-input" placeholder="funcionario@email.com"></div>
                <div class="szv2-input-group"><label class="szv2-label">Senha</label><input type="password" id="szv2-usr-pw" class="szv2-input" placeholder="Mínimo 8 caracteres"></div>
                <div class="szv2-input-group"><label class="szv2-label">Nome</label><input type="text" id="szv2-usr-name" class="szv2-input" placeholder="Nome do funcionário"></div>
                <div id="szv2-usr-err" style="display:none;color:var(--szv2-danger);font-size:13px;margin-bottom:8px"></div>
                <button type="button" class="szv2-btn szv2-btn-brand" style="width:100%"
                        data-ajax="<?php echo esc_attr( $sz9st_ajax ); ?>"
                        data-nonce="<?php echo esc_attr( $sz9st_nonce ); ?>"
                        onclick="szV2UsrCreate(this)">Criar acesso</button>
            </div>
            <!-- Acessos existentes -->
            <div class="szv2-card">
                <div class="szv2-card-head">
                    <div><h2>Acessos criados</h2><p class="szv2-card-sub">Usuários autorizados a acessar este painel.</p></div>
                    <span class="szv2-badge szv2-badge-neutral"><?php echo esc_html( (string) count( $sz9st_sub_users ) ); ?></span>
                </div>
                <?php if ( empty( $sz9st_sub_users ) ) : ?>
                    <?php // phpcs:ignore WordPress.Security.EscapeOutput
                    echo sz_v2_empty_state( [ 'title' => 'Nenhum acesso criado', 'text' => 'Quando você cadastrar membros da equipe, eles aparecerão aqui.' ] ); ?>
                <?php else : ?>
                <div class="szv2-table-wrap">
                    <table class="szv2-table">
                        <thead><tr><th>Nome</th><th>E-mail</th><th>Status</th><th style="text-align:right">Ações</th></tr></thead>
                        <tbody>
                        <?php foreach ( $sz9st_sub_users as $su ) : ?>
                        <tr>
                            <td class="szv2-td-main"><?php echo esc_html( (string) ( $su['name'] ?? '—' ) ); ?></td>
                            <td class="szv2-td-sub"><?php echo esc_html( (string) ( $su['email'] ?? '—' ) ); ?></td>
                            <td><?php echo ( $su['status'] ?? '' ) === 'active'
                                ? '<span class="sz-badge szv2-badge-success">Ativo</span>'
                                : '<span class="sz-badge szv2-badge-neutral">' . esc_html( (string) ( $su['status'] ?? '—' ) ) . '</span>'; ?></td>
                            <td style="text-align:right">
                                <button type="button" class="szv2-btn szv2-btn-sm szv2-btn-danger"
                                        data-user-id="<?php echo esc_attr( (string) ( $su['id'] ?? '' ) ); ?>"
                                        data-user-name="<?php echo esc_attr( (string) ( $su['name'] ?? '' ) ); ?>"
                                        data-nonce="<?php echo esc_attr( $sz9st_nonce ); ?>"
                                        onclick="szV2UsrDelete(this)">Remover</button>
                            </td>
                        </tr>
                        <?php endforeach; ?>
                        </tbody>
                    </table>
                </div>
                <?php endif; ?>
            </div>
        </div>
    </div>

    <!-- Suporte — último submenu de Configurações -->
    <div class="szv2-conn-panel szv2-prod-sub--hidden" id="szv2-panel-st-support">
        <div class="szv2-card-head" style="margin-bottom:var(--szv2-space-3)">
            <div><h2>Suporte</h2><p class="szv2-card-sub">Abra chamados e acompanhe o atendimento.</p></div>
            <button type="button" class="szv2-btn szv2-btn-brand" onclick="szV2SpShowCreate()">+ Novo chamado</button>
        </div>
        <div id="szv2-support-list-inner"><p class="szv2-td-sub" style="padding:20px">Carregando chamados...</p></div>
        <div id="szv2-support-detail-inner" style="display:none">
            <div class="szv2-card">
                <div class="szv2-card-head">
                    <div><h2 id="szv2-ticket-title">Chamado</h2><span id="szv2-ticket-badge"></span></div>
                    <div style="display:flex;gap:8px">
                        <button type="button" class="szv2-btn szv2-btn-sm szv2-btn-secondary" onclick="szV2SpBack()">Voltar</button>
                        <button type="button" class="szv2-btn szv2-btn-sm szv2-btn-danger" id="szv2-ticket-close-btn" onclick="szV2SpCloseTicket()">Fechar chamado</button>
                    </div>
                </div>
                <div id="szv2-ticket-msgs" class="szv2-ticket-msgs"></div>
                <div style="display:flex;gap:8px;margin-top:12px;padding-top:12px;border-top:1px solid var(--szv2-divider)">
                    <textarea id="szv2-ticket-reply" class="szv2-input" placeholder="Escreva sua resposta..." rows="3" style="flex:1;resize:vertical"></textarea>
                    <button type="button" class="szv2-btn szv2-btn-brand" onclick="szV2SpSendMsg()" style="align-self:flex-end">Enviar</button>
                </div>
            </div>
        </div>
        <!-- Modal: novo chamado -->
        <div id="szv2-sp-create-modal" class="szv2-modal-overlay" style="display:none" role="dialog" aria-modal="true">
            <div class="szv2-modal">
                <div class="szv2-modal-head"><h3>Novo chamado</h3><button type="button" class="szv2-modal-x" onclick="document.getElementById('szv2-sp-create-modal').style.display='none'">&times;</button></div>
                <div class="szv2-modal-body">
                    <div class="szv2-input-group"><label class="szv2-label">Assunto</label><input type="text" id="szv2-sp-assunto" class="szv2-input" placeholder="Descreva brevemente o problema"></div>
                    <div class="szv2-input-group">
                        <label class="szv2-label">Categoria</label>
                        <select id="szv2-sp-categoria" class="szv2-input">
                            <option value="pedido">Pedido</option>
                            <option value="financeiro">Financeiro</option>
                            <option value="tecnico">Técnico</option>
                            <option value="outro">Outro</option>
                        </select>
                    </div>
                    <div class="szv2-input-group"><label class="szv2-label">Mensagem</label><textarea id="szv2-sp-mensagem" class="szv2-input" rows="4" placeholder="Descreva seu problema em detalhes..."></textarea></div>
                    <div id="szv2-sp-create-err" style="display:none;color:var(--szv2-danger);font-size:13px"></div>
                </div>
                <div class="szv2-modal-foot">
                    <button type="button" class="szv2-btn szv2-btn-secondary" onclick="document.getElementById('szv2-sp-create-modal').style.display='none'">Cancelar</button>
                    <button type="button" class="szv2-btn szv2-btn-brand" onclick="szV2SpCreateTicket()">Abrir chamado</button>
                </div>
            </div>
        </div>
    </div>
</section>
<script>
// szV22FAToggle — alias para compatibilidade com código externo que referencie este nome
window.szV22FAToggle = function(cb) {
    var track = document.getElementById('szv2-2fa-track');
    var thumb = document.getElementById('szv2-2fa-thumb');
    var on = cb.checked;
    if (track) track.style.background = on ? '#EA580C' : '#e5e7eb';
    if (thumb) thumb.style.left = on ? '23px' : '3px';
    var aj = document.querySelector('[data-ajax]');
    var ajUrl = aj ? aj.getAttribute('data-ajax') : '/wp-admin/admin-ajax.php';
    var sec = document.getElementById('sec-settings');
    var nonce = sec ? sec.dataset.nonce : '';
    var fd = new FormData();
    fd.append('action','senderzz_portal'); fd.append('szaction','toggle_2fa');
    fd.append('enable', on ? '1' : '0'); fd.append('_ajax_nonce', nonce);
    fetch(ajUrl, {method:'POST',credentials:'same-origin',body:fd})
        .then(function(r){return r.json();})
        .then(function(d){ if(window.szV2Toast) szV2Toast(d.data && d.data.message ? d.data.message : (on ? '2FA ativado.' : '2FA desativado.'), d.success ? 'success' : 'danger'); })
        .catch(function(){ if(window.szV2Toast) szV2Toast('Erro ao salvar.','danger'); });
};
</script>
