<?php
/**
 * Senderzz V2 – Suporte (v455 completo)
 * @var object $sz_v2_user
 */
defined( 'ABSPATH' ) || exit;
$sz9sp_nonce    = wp_create_nonce( 'senderzz_portal' );
$sz9sp_ajax_url = admin_url( 'admin-ajax.php' );
?>
<section id="sec-support" class="sz-sec" data-szv2-label="Suporte"
         data-szv2-ajax-url="<?php echo esc_attr( $sz9sp_ajax_url ); ?>"
         data-szv2-nonce="<?php echo esc_attr( $sz9sp_nonce ); ?>">
    <div class="szv2-page-actions">
        <button type="button" class="szv2-btn szv2-btn-brand" id="szv2-support-new-btn" onclick="szV2SpShowCreate()">+ Novo chamado</button>
    </div>
    <div id="szv2-support-list"><p class="szv2-td-sub" style="padding:20px">Carregando chamados…</p></div>
    <div id="szv2-support-detail" style="display:none">
        <div class="szv2-card">
            <div class="szv2-card-head">
                <div><h2 id="szv2-ticket-title">Chamado</h2><span id="szv2-ticket-badge"></span></div>
                <div style="display:flex;gap:8px">
                    <button type="button" class="szv2-btn szv2-btn-sm szv2-btn-secondary" onclick="szV2SpBack()">← Voltar</button>
                    <button type="button" class="szv2-btn szv2-btn-sm szv2-btn-danger" id="szv2-ticket-close-btn" onclick="szV2SpCloseTicket()">Fechar chamado</button>
                </div>
            </div>
            <div id="szv2-ticket-msgs" class="szv2-ticket-msgs"></div>
            <div id="szv2-ticket-reply-area" style="display:flex;gap:8px;margin-top:12px;padding-top:12px;border-top:1px solid var(--szv2-divider)">
                <textarea id="szv2-ticket-reply" class="szv2-input" placeholder="Escreva sua resposta…" rows="3" style="flex:1;resize:vertical"></textarea>
                <button type="button" class="szv2-btn szv2-btn-brand" onclick="szV2SpSendMsg()" style="align-self:flex-end">Enviar</button>
            </div>
        </div>
    </div>
    <!-- Modal: novo chamado -->
    <div id="szv2-sp-create-modal" class="szv2-modal-overlay" style="display:none" role="dialog" aria-modal="true">
        <div class="szv2-modal">
            <div class="szv2-modal-head"><h3>Novo chamado</h3><button type="button" class="szv2-modal-x" onclick="document.getElementById('szv2-sp-create-modal').style.display='none'">×</button></div>
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
                <div class="szv2-input-group"><label class="szv2-label">Mensagem</label><textarea id="szv2-sp-mensagem" class="szv2-input" rows="4" placeholder="Descreva seu problema em detalhes…"></textarea></div>
                <div id="szv2-sp-create-err" style="display:none;color:var(--szv2-danger);font-size:13px"></div>
            </div>
            <div class="szv2-modal-foot">
                <button type="button" class="szv2-btn szv2-btn-secondary" onclick="document.getElementById('szv2-sp-create-modal').style.display='none'">Cancelar</button>
                <button type="button" class="szv2-btn szv2-btn-brand" onclick="szV2SpCreateTicket()">Abrir chamado</button>
            </div>
        </div>
    </div>
</section>
