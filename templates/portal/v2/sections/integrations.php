<?php
/**
 * Senderzz V2 – Integrações (v459 — seção própria, sem redirecionamento para Webhooks)
 * Sub-abas: Endpoint / Configurações / Histórico & Payload
 * Payload abre inline ao clicar no evento (sem botão individual por linha).
 * @var object $sz_v2_user
 */
defined( 'ABSPATH' ) || exit;

$sz9in_nonce   = wp_create_nonce( 'senderzz_portal' );
$sz9in_ajax    = admin_url( 'admin-ajax.php' );
$sz9in_wp_uid  = (int) ( $sz_v2_user->wp_user_id ?? 0 );
$sz9in_w_uid   = function_exists( 'senderzz_portal_wallet_user_id' )
    ? (int) senderzz_portal_wallet_user_id( $sz_v2_user )
    : $sz9in_wp_uid;
$sz9in_portal_id = (int) ( $sz_v2_user->id ?? 0 );

// Integração — resolve wallet_user_id (int) antes de chamar a função
global $wpdb;
$sz9in_int = null;
$sz9in_w_uid_safe = (int) $sz9in_w_uid; // garantia de tipo int — nunca passa objeto
if ( $sz9in_w_uid_safe > 0 && function_exists( 'senderzz_int_get_or_create_for_user' ) ) {
    $sz9in_int = senderzz_int_get_or_create_for_user( $sz9in_w_uid_safe, $sz9in_portal_id );
}
$sz9in_endpoint   = '';
if ( ! empty( $sz9in_int['token'] ) ) {
    $sz9in_endpoint = rest_url( 'senderzz/v1/integrations/' . $sz9in_int['token'] );
}
$sz9in_active       = ! empty( $sz9in_int['active'] );
$sz9in_cheapest     = ! empty( $sz9in_int['auto_cheapest'] );
$sz9in_paid_only    = ! empty( $sz9in_int['require_paid'] );
$sz9in_ignore_dup   = ! empty( $sz9in_int['ignore_duplicates'] );
$sz9in_complete_sku = false; // campo não existe na tabela — ocultar este toggle
$sz9in_save_pend    = false; // campo não existe na tabela — ocultar este toggle
$sz9in_last_recv    = (string) ( $sz9in_int['last_received_at'] ?? '' );
$sz9in_total_recv   = 0; // contagem via log
$sz9in_freight_rule = 'Mais barato disponível';

// Token
$sz9in_has_token = ! empty( $sz9in_int['token'] );

// Mapeamento de campos
$sz9in_mapping = [];
if ( ! empty( $sz9in_int['mapping_json'] ) ) {
    $sz9in_mapping = is_array( $sz9in_int['mapping_json'] )
        ? $sz9in_int['mapping_json']
        : (array) json_decode( wp_unslash( (string) $sz9in_int['mapping_json'] ), true );
}

// Últimos logs
$sz9in_logs = [];
$sz9in_log_t = $wpdb->prefix . 'senderzz_integration_log';
if ( $sz9in_w_uid_safe > 0 &&
     $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $sz9in_log_t ) ) === $sz9in_log_t ) {
    $sz9in_logs = $wpdb->get_results( $wpdb->prepare(
        "SELECT id, created_at AS received_at, external_order_id AS external_id, status, message, payload_json AS payload
           FROM {$sz9in_log_t}
          WHERE user_id = %d
          ORDER BY created_at DESC
          LIMIT 50",
        $sz9in_w_uid_safe
    ), ARRAY_A ) ?: [];
    $sz9in_total_recv = (int) $wpdb->get_var( $wpdb->prepare(
        "SELECT COUNT(*) FROM {$sz9in_log_t} WHERE user_id = %d",
        $sz9in_w_uid_safe
    ) );
}

// Último payload global (tab de debug)
$sz9in_last_payload = '';
if ( ! empty( $sz9in_logs[0]['payload'] ) ) {
    $decoded = json_decode( wp_unslash( $sz9in_logs[0]['payload'] ), true );
    $sz9in_last_payload = $decoded ? wp_json_encode( $decoded, JSON_PRETTY_PRINT | JSON_UNESCAPED_UNICODE ) : $sz9in_logs[0]['payload'];
}
?>
<section id="sec-integrations" class="sz-sec" data-szv2-label="Integrações"
         data-nonce="<?php echo esc_attr( $sz9in_nonce ); ?>"
         data-ajax="<?php echo esc_attr( $sz9in_ajax ); ?>">

    <div class="szv2-prod-subtabs" role="tablist" id="szv2-int-tabs">
        <button type="button" class="szv2-prod-subtab szv2-prod-subtab--active" role="tab" aria-selected="true" data-szv2-int-tab="int-endpoint">Endpoint</button>
        <button type="button" class="szv2-prod-subtab" role="tab" aria-selected="false" data-szv2-int-tab="int-config">Configurações</button>
    </div>

    <!-- SUB: Endpoint -->
    <div class="szv2-conn-panel" id="szv2-panel-int-endpoint">
        <div class="szv2-dash-row" style="align-items:flex-start">
            <div class="szv2-card" style="flex:1">
                <div class="szv2-card-head"><h2>Endpoint da integração</h2><p class="szv2-card-sub">Use este endpoint para enviar pedidos da sua plataforma para o Senderzz.</p></div>
                <div class="szv2-input-group">
                    <label class="szv2-label">URL do seu endpoint</label>
                    <div style="display:flex;gap:8px;width:100%">
                        <input type="text" id="szv2-int-url" class="szv2-input" readonly
                               value="<?php echo esc_attr( $sz9in_endpoint ); ?>"
                               style="font-family:var(--szv2-font-mono);font-size:12px;flex:1;min-width:0">
                        <button type="button" class="szv2-btn szv2-btn-secondary szv2-btn-sm"
                                style="flex-shrink:0"
                                data-action="int-copy-url">Copiar</button>
                    </div>
                </div>
                <div style="display:flex;gap:8px;flex-wrap:wrap;margin-top:8px">
                    <span class="sz-badge szv2-badge-neutral">POST</span>
                    <span class="sz-badge szv2-badge-neutral">JSON</span>
                    <?php if ( $sz9in_has_token ) : ?>
                    <span class="sz-badge szv2-badge-success">Token ativo</span>
                    <?php endif; ?>
                </div>
                <div style="margin-top:16px;display:flex;gap:8px">
                    <button type="button" class="szv2-btn szv2-btn-secondary szv2-btn-sm"
                            data-nonce="<?php echo esc_attr( $sz9in_nonce ); ?>"
                            data-action="int-renew-token">Renovar token</button>
                    <button type="button" class="szv2-btn szv2-btn-secondary szv2-btn-sm"
                            data-action="int-reprocess">Reprocessar último</button>
                </div>
                <div id="szv2-int-msg" style="display:none;margin-top:8px;font-size:13px"></div>
            </div>
            <div class="szv2-card" style="min-width:220px;flex:0 0 auto">
                <div class="szv2-card-head"><h2>Status da integração</h2>
                    <?php if ( $sz9in_active ) : ?>
                    <span class="sz-badge szv2-badge-success">Ativa</span>
                    <?php else : ?>
                    <span class="sz-badge szv2-badge-neutral">Pausada</span>
                    <?php endif; ?>
                </div>
                <div style="display:flex;flex-direction:column;gap:8px;font-size:13px">
                    <?php if ( $sz9in_last_recv ) : ?>
                    <div style="display:flex;justify-content:space-between;gap:8px">
                        <span style="color:var(--szv2-text-muted)">Último recebimento</span>
                        <span><?php echo esc_html( wp_date( 'Y-m-d H:i', strtotime( $sz9in_last_recv ) ) ); ?></span>
                    </div>
                    <?php endif; ?>
                    <div style="display:flex;justify-content:space-between;gap:8px">
                        <span style="color:var(--szv2-text-muted)">Total recebido</span>
                        <span class="szv2-num"><?php echo esc_html( (string) $sz9in_total_recv ); ?> registros</span>
                    </div>
                    <div style="display:flex;justify-content:space-between;gap:8px">
                        <span style="color:var(--szv2-text-muted)">Regra de frete</span>
                        <span><?php echo esc_html( $sz9in_freight_rule ); ?></span>
                    </div>
                </div>
                <div style="border-top:1px solid var(--szv2-divider);margin-top:14px;padding-top:12px;display:flex;align-items:center;justify-content:space-between">
                    <span style="font-size:13px">Pausar recebimento</span>
                    <label class="szv2-toggle-lbl">
                        <input type="checkbox" id="szv2-int-pause"
                               <?php checked( ! $sz9in_active ); ?>
                               data-key="active"
                               data-nonce="<?php echo esc_attr( $sz9in_nonce ); ?>"
                               onchange="szV2IntTogglePause(this)">
                        <span class="szv2-toggle-slider"></span>
                    </label>
                </div>
            </div>
        </div>
    </div>
    <!-- /end status card row -->

    <!-- Histórico & Payload — dentro da aba Endpoint, abaixo dos cards -->
    <div class="szv2-conn-panel" id="szv2-panel-int-history-card">
    <div class="szv2-card">
        <div class="szv2-card-head">
            <div><h2>Logs de recebimento</h2><p class="szv2-card-sub">Clique em um registro para expandir o payload.</p></div>
            <div style="display:flex;flex-direction:column;gap:6px;min-width:140px">
                <button type="button" class="szv2-btn szv2-btn-secondary szv2-btn-sm" style="width:100%" data-action="int-load-logs">Atualizar</button>
                <button type="button" class="szv2-btn szv2-btn-sm" style="width:100%;color:var(--szv2-danger);border:1.5px solid var(--szv2-danger);background:transparent"
                        data-nonce="<?php echo esc_attr( $sz9in_nonce ); ?>"
                        data-action="int-clear-logs">Limpar logs</button>
            </div>
        </div>
        <?php if ( empty( $sz9in_logs ) ) : ?>
            <?php // phpcs:ignore WordPress.Security.EscapeOutput
            echo sz_v2_empty_state( [ 'title' => 'Nenhum log encontrado', 'text' => 'Os registros de recebimento aparecem aqui após o primeiro envio.' ] ); ?>
        <?php else : ?>
        <div id="szv2-int-log-list">
            <?php foreach ( $sz9in_logs as $sz9in_log ) :
                $sz9in_log_id  = (int) ( $sz9in_log['id'] ?? 0 );
                $sz9in_log_ext = (string) ( $sz9in_log['external_id'] ?? '—' );
                $sz9in_log_st  = (string) ( $sz9in_log['status'] ?? '' );
                $sz9in_log_msg = (string) ( $sz9in_log['message'] ?? '' );
                $sz9in_log_dt  = ! empty( $sz9in_log['received_at'] )
                    ? wp_date( 'd/m H:i', strtotime( $sz9in_log['received_at'] ) ) : '—';
                $sz9in_log_ok  = in_array( $sz9in_log_st, [ 'processed', 'processado', 'ok' ], true );
                $sz9in_raw     = (string) ( $sz9in_log['payload'] ?? '' );
                $sz9in_dec     = $sz9in_raw ? json_decode( wp_unslash( $sz9in_raw ), true ) : null;
                $sz9in_pretty2 = $sz9in_dec ? wp_json_encode( $sz9in_dec, JSON_PRETTY_PRINT | JSON_UNESCAPED_UNICODE ) : $sz9in_raw;
            ?>
            <div class="szv2-int-log-row" data-action="int-toggle-log"
                 style="cursor:pointer;padding:10px 0;border-bottom:1px solid var(--szv2-divider)">
                <div style="display:flex;align-items:center;gap:12px">
                    <span style="width:8px;height:8px;border-radius:50%;flex-shrink:0;display:inline-block;background:<?php echo $sz9in_log_ok ? 'var(--szv2-success)' : 'var(--szv2-danger)'; ?>"></span>
                    <span style="font-size:12px;color:var(--szv2-text-muted);min-width:80px"><?php echo esc_html( $sz9in_log_dt ); ?></span>
                    <span style="font-size:13px;font-family:var(--szv2-font-mono);flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap"><?php echo esc_html( $sz9in_log_ext ); ?></span>
                    <span class="sz-badge <?php echo $sz9in_log_ok ? 'szv2-badge-success' : 'szv2-badge-danger'; ?>"><?php echo $sz9in_log_ok ? 'Processado' : 'Erro'; ?></span>
                    <span style="font-size:12px;color:var(--szv2-text-muted);max-width:300px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap"><?php echo esc_html( $sz9in_log_msg ); ?></span>
                    <span class="szv2-int-log-chevron" style="font-size:11px;color:var(--szv2-text-muted);transition:transform .2s">&#9662;</span>
                </div>
                <div class="szv2-int-log-payload" style="display:none;margin-top:10px">
                    <?php if ( $sz9in_pretty2 ) : ?>
                    <pre class="szv2-code-pre szv2-code-pre--compact" style="overflow:auto"><?php echo esc_html( $sz9in_pretty2 ); ?></pre>
                    <?php else : ?>
                    <p style="font-size:12px;color:var(--szv2-text-muted)">Nenhum payload armazenado.</p>
                    <?php endif; ?>
                </div>
            </div>
            <?php endforeach; ?>
        </div>
        <div style="text-align:center;margin-top:12px">
            <button type="button" class="szv2-btn szv2-btn-secondary szv2-btn-sm" data-action="int-load-logs">Ver todos os logs</button>
        </div>
        <?php endif; ?>
    </div><!-- /szv2-card -->
    </div><!-- /szv2-panel-int-history-card conn-panel -->

    <!-- SUB: Configurações -->
    <div class="szv2-conn-panel szv2-prod-sub--hidden" id="szv2-panel-int-config">
        <div class="szv2-dash-row" style="align-items:stretch">
            <div style="flex:1;display:flex;flex-direction:column;gap:var(--szv2-space-4)">

                <!-- Configurações da integração -->
                <div class="szv2-card">
                    <div class="szv2-card-head"><div><h2>Configurações da integração</h2></div></div>
                    <?php
                    $sz9in_toggles = [
                        [ 'id'=>'szv2-int-active',  'key'=>'active',            'label'=>'Integração ativa',                        'sub'=>'Recebe e processa webhooks automaticamente.',             'val'=>$sz9in_active    ],
                        [ 'id'=>'szv2-int-paid',    'key'=>'require_paid',      'label'=>'Exigir pagamento confirmado',              'sub'=>'Só processa pedidos com status de pagamento confirmado.',  'val'=>$sz9in_paid_only ],
                        [ 'id'=>'szv2-int-dup',     'key'=>'ignore_duplicates', 'label'=>'Ignorar pedido duplicado pelo ID externo', 'sub'=>'Evita criação de pedidos duplicados usando o ID externo.', 'val'=>$sz9in_ignore_dup ],
                        [ 'id'=>'szv2-int-cheap',   'key'=>'auto_cheapest',     'label'=>'Menor frete automático',                  'sub'=>'Escolhe automaticamente o frete mais barato disponível.',  'val'=>$sz9in_cheapest  ],
                        [ 'id'=>'szv2-int-sku',     'key'=>'complete_sku',      'label'=>'Completar dados com cadastro do Senderzz (SKU)', 'sub'=>'Se o produto existir, usamos peso, medidas e classe do cadastro.', 'val'=>false ],
                        [ 'id'=>'szv2-int-pending',  'key'=>'save_pending_error','label'=>'Salvar pendentes com erro',               'sub'=>'Se faltar dado crítico, o pedido fica pendente para revisão.', 'val'=>false ],
                    ];
                    foreach ( $sz9in_toggles as $sz9in_tg ) : ?>
                    <div style="display:flex;align-items:flex-start;justify-content:space-between;padding:12px 0;border-bottom:1px solid var(--szv2-divider);gap:12px">
                        <div>
                            <div style="font-size:13px;font-weight:600;color:var(--szv2-text);margin-bottom:2px"><?php echo esc_html( $sz9in_tg['label'] ); ?></div>
                            <div style="font-size:12px;color:var(--szv2-text-muted)"><?php echo esc_html( $sz9in_tg['sub'] ); ?></div>
                        </div>
                        <label class="szv2-toggle-lbl" style="flex-shrink:0">
                            <input type="checkbox" id="<?php echo esc_attr( $sz9in_tg['id'] ); ?>"
                                   data-key="<?php echo esc_attr( $sz9in_tg['key'] ); ?>"
                                   data-nonce="<?php echo esc_attr( $sz9in_nonce ); ?>"
                                   <?php checked( $sz9in_tg['val'] ); ?>
                                   onchange="szV2IntSaveToggle(this)">
                            <span class="szv2-toggle-slider"></span>
                        </label>
                    </div>
                    <?php endforeach; ?>
                </div>

                <!-- Regras de processamento -->
                <div class="szv2-card">
                    <div class="szv2-card-head"><h2>Regras de processamento</h2></div>
                    <div style="display:grid;grid-template-columns:1fr 1fr 1fr;gap:0;border:1px solid var(--szv2-border);border-radius:var(--szv2-radius-md);overflow:hidden">
                        <div style="padding:8px 12px;font-size:12px;font-weight:600;color:var(--szv2-text-muted);background:var(--szv2-surface-alt);border-bottom:1px solid var(--szv2-border)">Regra de frete</div>
                        <div style="padding:8px 12px;font-size:12px;font-weight:600;color:var(--szv2-text-muted);background:var(--szv2-surface-alt);border-bottom:1px solid var(--szv2-border);border-left:1px solid var(--szv2-border)">Classe de entrega permitida</div>
                        <div style="padding:8px 12px;font-size:12px;font-weight:600;color:var(--szv2-text-muted);background:var(--szv2-surface-alt);border-bottom:1px solid var(--szv2-border);border-left:1px solid var(--szv2-border)">Transportadoras</div>
                        <div style="padding:10px 12px;font-size:13px;color:var(--szv2-text-muted)">—</div>
                        <div style="padding:10px 12px;font-size:13px;color:var(--szv2-text-muted);border-left:1px solid var(--szv2-border)">—</div>
                        <div style="padding:10px 12px;font-size:13px;color:var(--szv2-text-muted);border-left:1px solid var(--szv2-border)">—</div>
                    </div>
                    <div style="margin-top:10px;padding:10px 12px;background:var(--szv2-surface-alt);border-left:3px solid var(--szv2-border);border-radius:0 var(--szv2-radius-sm) var(--szv2-radius-sm) 0;font-size:12px;color:var(--szv2-text-muted)">
                        O menor frete é escolhido no momento da cotação, entre as transportadoras e classes liberadas para o cadastro.
                    </div>
                </div>
            </div>

            <!-- Mapeamento de campos -->
            <div class="szv2-card" style="flex:1">
                <div class="szv2-card-head"><h2>Mapeamento de campos</h2><p class="szv2-card-sub">Associe os campos recebidos da plataforma aos campos do Senderzz.</p></div>
                <?php
                $sz9in_map_fields = [
                    'ID externo do pedido' => 'pedido.id_externo',
                    'Status do pagamento'  => 'pedido.status_pagamento',
                    'Nome do cliente'      => 'cliente.nome',
                    'Telefone'             => 'cliente.telefone',
                    'E-mail'               => 'cliente.email',
                    'CPF/CNPJ'             => 'cliente.documento',
                    'CEP'                  => 'endereco.cep',
                    'Rua'                  => 'endereco.rua',
                    'Número'               => 'endereco.numero',
                    'Complemento'          => 'endereco.complemento',
                    'Bairro'               => 'endereco.bairro',
                ];
                foreach ( $sz9in_map_fields as $sz9in_mf_label => $sz9in_mf_key ) :
                    $sz9in_mf_val = (string) ( $sz9in_mapping[ $sz9in_mf_key ] ?? '' );
                ?>
                <div style="display:flex;align-items:center;gap:8px;padding:8px 0;border-bottom:1px solid var(--szv2-divider)">
                    <span style="font-size:12px;color:var(--szv2-text-muted);min-width:150px;flex-shrink:0"><?php echo esc_html( $sz9in_mf_label ); ?></span>
                    <div style="flex:1;display:flex;align-items:center;gap:4px;font-size:12px;font-family:var(--szv2-font-mono);background:var(--szv2-surface-alt);padding:4px 8px;border-radius:var(--szv2-radius-sm);color:var(--szv2-text-muted)">
                        <?php echo esc_html( $sz9in_mf_key ); ?> →
                    </div>
                    <input type="text"
                           class="szv2-input szv2-input-sm szv2-int-map-input"
                           data-field="<?php echo esc_attr( $sz9in_mf_key ); ?>"
                           value="<?php echo esc_attr( $sz9in_mf_val ); ?>"
                           placeholder="campo.do.payload"
                           style="flex:1;font-size:12px;font-family:var(--szv2-font-mono)">
                    <button type="button" class="szv2-btn-icon-clear"
                            title="Limpar"
                            data-action="int-clear-input">&times;</button>
                </div>
                <?php endforeach; ?>
                <div style="margin-top:12px">
                    <button type="button" class="szv2-btn szv2-btn-brand"
                            data-nonce="<?php echo esc_attr( $sz9in_nonce ); ?>"
                            data-action="int-save-mapping">Salvar mapeamento</button>
                </div>
            </div>
        </div>
    </div>

</section>
<script>
// Custom tab switcher for integrations: endpoint shows both endpoint+history panels
function szV2IntTab(id, btn) {
    var sec = btn.closest('section') || document;
    sec.querySelectorAll('.szv2-conn-panel').forEach(function(p) {
        if (id === 'int-endpoint') {
            // Show endpoint and history; hide config
            p.classList.toggle('szv2-prod-sub--hidden',
                p.id !== 'szv2-panel-int-endpoint' && p.id !== 'szv2-panel-int-history-card');
        } else {
            // Show only config; hide endpoint and history
            p.classList.toggle('szv2-prod-sub--hidden', p.id !== 'szv2-panel-' + id);
        }
    });
    sec.querySelectorAll('.szv2-prod-subtab').forEach(function(t) { t.classList.remove('szv2-prod-subtab--active'); });
    if (btn) btn.classList.add('szv2-prod-subtab--active');
}
function szV2IntToggleLog(row) {
    var payload = row.querySelector('.szv2-int-log-payload');
    var chev    = row.querySelector('.szv2-int-log-chevron');
    if (!payload) return;
    var isOpen = payload.style.display === 'block';
    // Fecha todos
    document.querySelectorAll('.szv2-int-log-payload').forEach(function(p){ p.style.display='none'; });
    document.querySelectorAll('.szv2-int-log-chevron').forEach(function(c){ c.style.transform=''; });
    if (!isOpen) {
        payload.style.display = 'block';
        if (chev) chev.style.transform = 'rotate(180deg)';
    }
}
function szV2CopyIntUrl() {
    var inp = document.getElementById('szv2-int-url');
    if (!inp) return;
    navigator.clipboard.writeText(inp.value).then(function(){
        var msg = document.getElementById('szv2-int-msg');
        if (msg) { msg.style.display=''; msg.textContent='URL copiada!'; setTimeout(function(){ msg.style.display='none'; }, 2000); }
    });
}
function szV2IntSaveToggle(cb) {
    var sec   = document.getElementById('sec-integrations');
    var ajax  = sec ? sec.dataset.ajax : '';
    var nonce = cb.dataset.nonce || (sec ? sec.dataset.nonce : '');
    var label = cb.closest('[style]') ? (cb.closest('div').querySelector('.szv2-label')||{}).textContent : (cb.dataset.key||'');
    var fd    = new FormData();
    fd.append('action','senderzz_portal'); fd.append('szaction','integrations_toggle');
    fd.append('_ajax_nonce', nonce); fd.append('key', cb.dataset.key || ''); fd.append('value', cb.checked ? '1' : '0');
    fetch(ajax, { method:'POST', body:fd, credentials:'same-origin' })
        .then(function(r){ return r.json(); })
        .then(function(d){
            var ok = d && d.success;
            var msg = ok ? (cb.checked ? 'Ativado.' : 'Desativado.') : 'Erro ao salvar.';
            if (window.szV2Toast) window.szV2Toast(msg, ok ? 'success' : 'danger');
        })
        .catch(function(){ if (window.szV2Toast) window.szV2Toast('Erro de conexão.','danger'); });
}
// Pausar = inverso de active: checked → active=0, unchecked → active=1
function szV2IntTogglePause(cb) {
    var sec   = document.getElementById('sec-integrations');
    var ajax  = sec ? sec.dataset.ajax : '';
    var nonce = cb.dataset.nonce || (sec ? sec.dataset.nonce : '');
    var fd    = new FormData();
    fd.append('action','senderzz_portal'); fd.append('szaction','integrations_toggle');
    fd.append('_ajax_nonce', nonce); fd.append('key', 'active'); fd.append('value', cb.checked ? '0' : '1');
    fetch(ajax, { method:'POST', body:fd, credentials:'same-origin' })
        .then(function(r){ return r.json(); })
        .then(function(d){
            var ok = d && d.success;
            var msg = ok ? (cb.checked ? 'Recebimento pausado.' : 'Recebimento ativado.') : 'Erro ao salvar.';
            if (window.szV2Toast) window.szV2Toast(msg, ok ? 'success' : 'danger');
        })
        .catch(function(){ if (window.szV2Toast) window.szV2Toast('Erro de conexão.','danger'); });
}
function szV2IntClearLogs(btn) {
    var sec = document.getElementById('sec-integrations');
    var nonce = btn.dataset.nonce || (sec ? sec.dataset.nonce : '');
    var ajax  = sec ? sec.dataset.ajax : '';
    szV2Confirm(
        { title: 'Limpar logs de integração?', message: 'Todos os registros serão apagados permanentemente.', btn: 'Limpar', danger: true },
        function() {
            var fd = new FormData();
            fd.append('action','senderzz_portal'); fd.append('szaction','integrations_clear_logs');
            fd.append('_ajax_nonce', nonce);
            fetch(ajax, { method:'POST', body:fd, credentials:'same-origin' })
                .then(function(r){ return r.json(); })
                .then(function(){ location.reload(); });
        }
    );
}
function szV2IntRenewToken(btn) {
    var sec = document.getElementById('sec-integrations');
    btn.disabled = true;
    var fd = new FormData();
    fd.append('action','senderzz_portal'); fd.append('szaction','integrations_rotate_token');
    fd.append('_ajax_nonce', btn.dataset.nonce || (sec ? sec.dataset.nonce : ''));
    fetch(sec ? sec.dataset.ajax : '', { method:'POST', body:fd, credentials:'same-origin' })
        .then(function(r){ return r.json(); })
        .then(function(d){
            btn.disabled = false;
            if (d && d.success) {
                if (window.szV2Toast) window.szV2Toast('Token renovado! Recarregando…','success');
                setTimeout(function(){ location.reload(); }, 1200);
            } else {
                if (window.szV2Toast) window.szV2Toast((d&&d.data&&d.data.message)||'Erro ao renovar token.','danger');
            }
        })
        .catch(function(){ btn.disabled=false; if(window.szV2Toast) window.szV2Toast('Erro de conexão.','danger'); });
}
function szV2IntSaveMapping(btn) {
    var sec = document.getElementById('sec-integrations');
    btn.disabled = true;
    var fd  = new FormData();
    fd.append('action','senderzz_portal'); fd.append('szaction','integrations_save');
    fd.append('_ajax_nonce', btn.dataset.nonce);
    var mapping = {};
    document.querySelectorAll('.szv2-int-map-input').forEach(function(inp) {
        if (inp.dataset.field && inp.value.trim()) { mapping[inp.dataset.field] = inp.value.trim(); }
    });
    fd.append('mapping_json', JSON.stringify(mapping));
    fetch(sec ? sec.dataset.ajax : '/wp-admin/admin-ajax.php', { method:'POST', body:fd, credentials:'same-origin' })
        .then(function(r){ return r.json(); })
        .then(function(d){
            btn.disabled = false;
            var ok  = d && d.success;
            var txt = (d&&d.data&&d.data.message) ? d.data.message : (ok ? 'Mapeamento salvo!' : 'Erro ao salvar.');
            if (window.szV2Toast) window.szV2Toast(txt, ok ? 'success' : 'danger');
        })
        .catch(function(){ btn.disabled=false; if(window.szV2Toast) window.szV2Toast('Erro de conexão.','danger'); });
}
function szV2IntReprocess() {
    var sec = document.getElementById('sec-integrations');
    var fd  = new FormData();
    fd.append('action','senderzz_portal'); fd.append('szaction','integrations_reprocess');
    fd.append('nonce', sec ? sec.dataset.nonce : '');
    fetch(sec ? sec.dataset.ajax : '', { method:'POST', body:fd, credentials:'same-origin' })
        .then(function(r){ return r.json(); })
        .then(function(d){
            var msg = (d&&d.data&&d.data.message) ? d.data.message : (d&&d.success ? 'Reprocessamento solicitado.' : 'Erro.');
            if (window.szV2Toast) window.szV2Toast(msg, d&&d.success ? 'success' : 'danger');
        })
        .catch(function(){ if(window.szV2Toast) window.szV2Toast('Erro de conexão.','danger'); });
}
function szV2IntLoadLogs() { location.reload(); }
(function(){
    var root = document.getElementById('sec-integrations');
    if (!root) return;
    root.addEventListener('click', function(e) {
        // Tab switcher
        var tab = e.target.closest('[data-szv2-int-tab]');
        if (tab) { szV2IntTab(tab.getAttribute('data-szv2-int-tab'), tab); return; }
        // Action buttons
        var btn = e.target.closest('[data-action]');
        if (!btn) return;
        var act = btn.getAttribute('data-action');
        if (act === 'int-copy-url' && typeof window.szV2CopyIntUrl === 'function') { szV2CopyIntUrl(); }
        else if (act === 'int-renew-token' && typeof window.szV2IntRenewToken === 'function') { szV2IntRenewToken(btn); }
        else if (act === 'int-reprocess' && typeof window.szV2IntReprocess === 'function') { szV2IntReprocess(); }
        else if (act === 'int-load-logs' && typeof window.szV2IntLoadLogs === 'function') { szV2IntLoadLogs(); }
        else if (act === 'int-clear-logs' && typeof window.szV2IntClearLogs === 'function') { szV2IntClearLogs(btn); }
        else if (act === 'int-toggle-log' && typeof window.szV2IntToggleLog === 'function') { szV2IntToggleLog(btn); }
        else if (act === 'int-clear-input') { var prev = btn.previousElementSibling; if (prev) prev.value = ''; }
        else if (act === 'int-save-mapping' && typeof window.szV2IntSaveMapping === 'function') { szV2IntSaveMapping(btn); }
    });
}());
</script>
