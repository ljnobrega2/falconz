<?php
/**
 * Senderzz V2 – Conexões: Webhooks + Integrações (v455 — unificado funcional)
 * Sub-abas: Configurar / Webhooks ativos / Histórico / Integrações
 * @var object $sz_v2_user
 */
defined( 'ABSPATH' ) || exit;

$sz9wh_nonce    = wp_create_nonce( 'senderzz_portal' );
$sz9wh_ajax     = admin_url( 'admin-ajax.php' );
$sz9wh_w_uid    = function_exists( 'senderzz_portal_wallet_user_id' )
    ? (int) senderzz_portal_wallet_user_id( $sz_v2_user ) : 0;

// Classes de entrega do usuário para o select de classe no formulário de webhook
$sz9wh_class_ids = function_exists( 'sz_get_user_class_ids' )
    ? array_values( array_filter( array_map( 'intval', sz_get_user_class_ids( $sz_v2_user ) ) ) )
    : [ (int) ( $sz_v2_user->shipping_class_id ?? 0 ) ];

// Webhooks existentes
global $wpdb;
$sz9wh_rows = [];
if ( $sz9wh_w_uid ) {
    $sz9wh_rows = (array) $wpdb->get_results( $wpdb->prepare(
        "SELECT * FROM {$wpdb->prefix}senderzz_webhooks WHERE user_id = %d AND url != '' ORDER BY shipping_class_id ASC LIMIT 50",
        $sz9wh_w_uid
    ), ARRAY_A );
}

// Integração (Conexões) — dados via AJAX no front
$sz9wh_has_int_fn = function_exists( 'senderzz_int_get_or_create_for_user' );
?>
<section id="sec-webhooks" class="sz-sec" data-szv2-label="Conexões"
         data-nonce="<?php echo esc_attr( $sz9wh_nonce ); ?>"
         data-ajax="<?php echo esc_attr( $sz9wh_ajax ); ?>">
    <div class="szv2-prod-subtabs" role="tablist" id="szv2-conn-tabs">
        <button type="button" class="szv2-prod-subtab szv2-prod-subtab--active" role="tab" aria-selected="true" onclick="szV2ConnTab('webhooks-main',this)">Webhooks</button>
        <button type="button" class="szv2-prod-subtab" role="tab" aria-selected="false" onclick="szV2ConnTab('webhooks-history',this)" id="szv2-wh-history-tab">Histórico</button>
    </div>

    <!-- SUB: Webhooks (configurar + ativos unificados) -->
    <div class="szv2-conn-panel" id="szv2-panel-webhooks-main">

        <!-- Payload recolhido — acima do formulário de novo webhook -->
        <details class="szv2-payload-details">
            <summary class="szv2-payload-summary">
                <span>📦 Payload exemplo — estrutura completa do webhook</span>
                <span class="szv2-payload-hint">clique para expandir</span>
            </summary>
            <div style="padding:0">
                <pre class="szv2-code-pre szv2-code-pre--full" style="overflow-y:auto;border-radius:0 0 var(--szv2-radius-lg) var(--szv2-radius-lg)"><?php echo esc_html( json_encode( [
                    'event'          => 'order_status_enviado',
                    'status_ativo'   => true,
                    'pedido'         => [
                        'id'                  => 1,
                        'numero'              => '1',
                        'status'              => 'enviado',
                        'subtotal'            => 197.00,
                        'subtotal_formatado'  => 'R$ 197,00',
                        'total'               => 226.90,
                        'total_formatado'     => 'R$ 226,90',
                        'desconto'            => 0,
                        'desconto_formatado'  => '',
                        'metodo_pagamento'    => 'PIX',
                        'criado_em'           => '2026-06-15T01:14:08-03:00',
                        'atualizado_em'       => '2026-06-15T01:14:08-03:00',
                        'pago_em'             => '2026-06-15T01:14:08-03:00',
                        'enviado_em'          => '',
                        'entregue_em'         => '',
                    ],
                    'classe_entrega' => [
                        'id'   => 10,
                        'nome' => 'São Paulo',
                        'slug' => 'sao-paulo',
                    ],
                    'frete'          => [
                        'valor'            => 29.90,
                        'valor_formatado'  => 'R$ 29,90',
                        'prazo_dias_uteis' => 3,
                        'transportadora'   => 'Loggi',
                        'servico'          => 'Express',
                    ],
                    'cliente'        => [
                        'nome'              => 'Cliente Teste',
                        'telefone'          => '11999999999',
                        'telefone_completo' => '5511999999999',
                        'email'             => 'cliente@email.com',
                        'cpf'               => '000.000.000-00',
                    ],
                    'entrega'        => [
                        'nome'        => 'Cliente Teste',
                        'cep'         => '01001000',
                        'endereco'    => 'Rua Exemplo',
                        'numero'      => '100',
                        'complemento' => '',
                        'bairro'      => 'Centro',
                        'cidade'      => 'São Paulo',
                        'estado'      => 'SP',
                    ],
                    'rastreamento'         => [ 'BR123456789' ],
                    'link_rastreamento'    => 'https://testes.senderzz.com.br/rastreio/BR123456789/',
                    'itens'                => [
                        [ 'nome' => 'Produto', 'quantidade' => 1, 'subtotal' => 197.00 ],
                    ],
                    'transportadora'       => 'Loggi',
                    'servico'              => 'Express',
                ], JSON_PRETTY_PRINT | JSON_UNESCAPED_UNICODE ) ); ?></pre>
            </div>
        </details>

        <!-- Formulário: Novo webhook -->
        <div class="szv2-card">
            <div class="szv2-card-head"><h2>Novo webhook</h2><p class="szv2-card-sub">Receba eventos dos pedidos em tempo real.</p></div>
            <div class="szv2-input-group">
                <label class="szv2-label">Classe de entrega</label>
                <select class="szv2-input" id="szv2-wh-class">
                    <option value="">Selecionar...</option>
                    <?php foreach ( $sz9wh_class_ids as $cid ) :
                        $sz9wh_class_name = function_exists( 'sz_aff_shipping_class_label' )
                            ? sz_aff_shipping_class_label( $cid )
                            : 'Classe #' . $cid;
                    ?>
                    <option value="<?php echo esc_attr( (string) $cid ); ?>"><?php echo esc_html( $sz9wh_class_name ); ?></option>
                    <?php endforeach; ?>
                </select>
            </div>
            <div class="szv2-input-group">
                <label class="szv2-label">URL de destino</label>
                <input type="url" id="szv2-wh-url" class="szv2-input" placeholder="https://seusistema.com.br/webhook/senderzz">
            </div>
            <!-- DT-CODE-02: filtro de eventos por webhook -->
            <div class="szv2-input-group">
                <label class="szv2-label">Eventos (vazio = todos)</label>
                <div style="display:flex;gap:10px;flex-wrap:wrap;padding:8px 0">
                    <?php
                    $sz9wh_events = [
                        'order_status_enviado'   => 'Enviado',
                        'order_status_entregue'  => 'Entregue',
                        'order_status_cancelado' => 'Cancelado',
                        'order_status_frustrado' => 'Frustrado',
                        'order_status_em_rota'   => 'Em rota',
                        'order_status_embalado'  => 'Embalado',
                    ];
                    foreach ( $sz9wh_events as $sz9wh_ev_key => $sz9wh_ev_label ) : ?>
                    <label style="display:flex;align-items:center;gap:5px;font-size:13px;cursor:pointer">
                        <input type="checkbox" class="szv2-wh-event-cb" value="<?php echo esc_attr( $sz9wh_ev_key ); ?>">
                        <?php echo esc_html( $sz9wh_ev_label ); ?>
                    </label>
                    <?php endforeach; ?>
                </div>
            </div>
            <div style="display:flex;align-items:center;gap:10px;margin-bottom:16px">
                <label class="szv2-toggle-lbl">
                    <input type="checkbox" id="szv2-wh-active" checked>
                    <span class="szv2-toggle-slider"></span>
                </label>
                <span style="font-size:13px;color:var(--szv2-text)">Ativo</span>
            </div>
            <div id="szv2-wh-save-err" style="display:none;color:var(--szv2-danger);font-size:13px;margin-bottom:8px"></div>
            <button type="button" class="szv2-btn szv2-btn-brand" id="szv2-wh-save-btn" onclick="szV2WbSave()">Salvar webhook</button>
        </div>

        <!-- Webhooks configurados -->
        <div class="szv2-card" style="margin-top:0">
            <div class="szv2-card-head">
                <h2>Webhooks configurados</h2>
                <span class="szv2-card-sub"><?php echo esc_html( (string) count( $sz9wh_rows ) ); ?> endpoint(s)</span>
            </div>
            <?php if ( empty( $sz9wh_rows ) ) : ?>
                <?php // phpcs:ignore WordPress.Security.EscapeOutput
                echo sz_v2_empty_state( [ 'title' => 'Nenhum webhook configurado', 'text' => 'Use o formulário acima para adicionar seu primeiro endpoint.' ] ); ?>
            <?php else : ?>
            <div class="szv2-table-wrap">
                <table class="szv2-table">
                    <thead><tr><th>Classe</th><th>URL destino</th><th>Status</th><th style="text-align:right">Ações</th></tr></thead>
                    <tbody>
                    <?php foreach ( $sz9wh_rows as $sz9wh_r ) : ?>
                    <tr>
                        <td class="szv2-td-sub"><?php
                            $sz9wh_cid_r = (int) ( $sz9wh_r['shipping_class_id'] ?? 0 );
                            echo esc_html( $sz9wh_cid_r > 0 && function_exists( 'sz_aff_shipping_class_label' )
                                ? sz_aff_shipping_class_label( $sz9wh_cid_r )
                                : ( $sz9wh_cid_r ? ( 'CD #' . $sz9wh_cid_r ) : '—' ) );
                        ?></td>
                        <td>
                            <?php if ( ! empty( $sz9wh_r['url'] ) ) : ?>
                            <a href="<?php echo esc_url( $sz9wh_r['url'] ); ?>" target="_blank" rel="noopener" class="szv2-wh-url-link"><?php echo esc_html( $sz9wh_r['url'] ); ?></a>
                            <?php else : ?>
                            <span class="szv2-td-sub" style="color:var(--szv2-text-faint)">Aguardando URL</span>
                            <?php endif; ?>
                        </td>
                        <td>
                            <?php if ( ! empty( $sz9wh_r['active'] ) ) : ?>
                            <span class="sz-badge szv2-badge-success">Ativo</span>
                            <?php else : ?>
                            <span class="sz-badge szv2-badge-neutral">Inativo</span>
                            <?php endif; ?>
                        </td>
                        <td style="text-align:right">
                            <div style="display:flex;gap:6px;justify-content:flex-end">
                                <?php if ( ! empty( $sz9wh_r['url'] ) ) : ?>
                                <button type="button" class="szv2-btn szv2-btn-sm szv2-btn-secondary"
                                        data-webhook-id="<?php echo esc_attr( (string) ( $sz9wh_r['id'] ?? '' ) ); ?>"
                                        data-nonce="<?php echo esc_attr( $sz9wh_nonce ); ?>"
                                        onclick="szV2WbTest(this)">Testar</button>
                                <?php endif; ?>
                                <button type="button" class="szv2-btn szv2-btn-sm szv2-btn-danger"
                                        data-webhook-id="<?php echo esc_attr( (string) ( $sz9wh_r['id'] ?? '' ) ); ?>"
                                        data-class-id="<?php echo esc_attr( (string) ( $sz9wh_r['shipping_class_id'] ?? '' ) ); ?>"
                                        onclick="szV2WbDelete(this)">Excluir</button>
                            </div>
                        </td>
                    </tr>
                    <?php endforeach; ?>
                    </tbody>
                </table>
            </div>
            <?php endif; ?>
        </div>
    </div>

    <!-- SUB: Histórico de disparos -->
    <div class="szv2-conn-panel szv2-prod-sub--hidden" id="szv2-panel-webhooks-history">
        <div class="szv2-card">
            <div class="szv2-card-head">
                <div><h2>Histórico de disparos</h2><p class="szv2-card-sub">Eventos recentes, status e payloads enviados.</p></div>
                <div style="display:flex;flex-direction:column;gap:6px;min-width:150px">
                    <button type="button" class="szv2-btn szv2-btn-secondary szv2-btn-sm" style="width:100%" onclick="szV2WbLoadHistory(0)">Atualizar</button>
                    <button type="button" class="szv2-btn szv2-btn-sm" style="width:100%;color:var(--szv2-danger);border:1.5px solid var(--szv2-danger);background:transparent"
                            data-nonce="<?php echo esc_attr( $sz9wh_nonce ); ?>"
                            onclick="szV2WbClearHistory(this)">Limpar histórico</button>
                </div>
            </div>
            <div id="szv2-wh-history-body">
                <p class="szv2-td-sub" style="padding:20px">Clique em "Histórico" para carregar os disparos.</p>
            </div>
        </div>
    </div>

</section>
<script>
function szV2WbClearHistory(btn) {
    var sec   = document.getElementById('sec-webhooks');
    var nonce = btn.dataset.nonce || (sec ? sec.dataset.nonce : '');
    var ajax  = sec ? sec.dataset.ajax : '';
    szV2Confirm(
        { title: 'Limpar histórico de webhooks?', message: 'Todos os disparos registrados serão apagados.', btn: 'Limpar', danger: true },
        function() {
            var fd = new FormData();
            fd.append('action','senderzz_portal'); fd.append('szaction','webhooks_clear_history');
            fd.append('_ajax_nonce', nonce);
            fetch(ajax, { method:'POST', body:fd, credentials:'same-origin' })
                .then(function(r){ return r.json(); })
                .then(function() {
                    var body = document.getElementById('szv2-wh-history-body');
                    if (body) body.innerHTML = '<p class="szv2-td-sub" style="padding:20px">Histórico limpo.</p>';
                });
        }
    );
}
</script>
