<?php
/**
 * Senderzz V2 – Carteira de Expedição (v459b — premium, harmônico com COD)
 * @var object $sz_v2_user
 */
defined( 'ABSPATH' ) || exit;

$sz9we_wp_uid = (int) ( $sz_v2_user->wp_user_id ?? 0 );
$sz9we_ajax   = admin_url( 'admin-ajax.php' );
$sz9we_nonce  = wp_create_nonce( 'senderzz_portal' );

$sz9we_saldo = 0.0;
if ( $sz9we_wp_uid && function_exists( 'tpc_get_saldo_disponivel' ) ) {
    $sz9we_saldo = (float) tpc_get_saldo_disponivel( $sz9we_wp_uid );
}

$sz9we_history = [];
if ( $sz9we_wp_uid && function_exists( 'tpc_get_historico_carteira' ) ) {
    $sz9we_history = (array) tpc_get_historico_carteira( $sz9we_wp_uid, 30 );
}

$sz9we_money      = static fn( float $v ): string =>
    function_exists( 'senderzz_portal_money' ) ? senderzz_portal_money( $v ) : 'R$ ' . number_format( $v, 2, ',', '.' );
$sz9we_saldo_baixo = $sz9we_saldo < 10.0;
?>

<!-- KPI grid: saldo disponível (como nos 3 cards COD) -->
<div class="szv2-kpi-grid" style="grid-template-columns:repeat(3,minmax(0,1fr));margin-bottom:var(--szv2-space-3)">
    <div class="szv2-kpi-card">
        <div class="szv2-kpi-label">Saldo disponível</div>
        <div class="szv2-kpi-value szv2-num" style="color:<?php echo $sz9we_saldo_baixo ? 'var(--szv2-danger)' : 'var(--szv2-brand)'; ?>">
            <?php echo esc_html( $sz9we_money( $sz9we_saldo ) ); ?>
        </div>
        <div class="szv2-kpi-meta"><?php echo $sz9we_saldo_baixo ? 'Saldo insuficiente para envios' : 'Reservado para frete'; ?></div>
    </div>
    <div class="szv2-kpi-card" style="grid-column:span 2">
        <div class="szv2-kpi-label">Recarregar via PIX</div>
        <div style="display:flex;gap:6px;flex-wrap:wrap;align-items:center;margin:8px 0">
            <?php foreach ( [ 50, 100, 200, 500 ] as $sz9we_val ) : ?>
            <button type="button"
                    class="szv2-btn szv2-btn-sm szv2-btn-secondary szv2-we-pix-preset"
                    data-value="<?php echo esc_attr( (string) $sz9we_val ); ?>"
                    onclick="szV2WeSelectPix(this,<?php echo esc_js( (string) $sz9we_val ); ?>)">
                R$ <?php echo esc_html( number_format( $sz9we_val, 0, ',', '.' ) ); ?>
            </button>
            <?php endforeach; ?>
            <input type="number"
                   id="szv2-we-pix-custom"
                   class="szv2-input szv2-input-sm"
                   placeholder="Outro valor (mín. R$ 10)"
                   min="10" step="1"
                   style="width:160px"
                   oninput="szV2WeClearPreset()">
            <button type="button"
                    class="szv2-btn szv2-btn-brand"
                    id="szv2-we-pix-btn"
                    data-ajax="<?php echo esc_attr( $sz9we_ajax ); ?>"
                    data-nonce="<?php echo esc_attr( $sz9we_nonce ); ?>"
                    onclick="szV2WeGerarPix(this)">Gerar PIX</button>
        </div>
        <div id="szv2-we-pix-result" style="display:none;margin-top:4px">
            <span style="font-size:12px;color:var(--szv2-text-muted)">Gerando PIX...</span>
        </div>
    </div>
</div>

<!-- Modal PIX -->
<div id="szv2-pix-modal" class="szv2-modal-overlay" style="display:none" role="dialog" aria-modal="true">
    <div class="szv2-modal" style="max-width:460px;width:96vw">
        <div class="szv2-modal-head">
            <h3>Confirmar Pagamento</h3>
            <button type="button" class="szv2-modal-x" onclick="document.getElementById('szv2-pix-modal').style.display='none'">&times;</button>
        </div>
        <div class="szv2-modal-body" style="text-align:center">
            <p style="font-size:13px;color:var(--szv2-text-muted);margin:0 0 16px">Escaneie o QR Code ou copie o código abaixo</p>
            <div id="szv2-pix-qr-wrap" style="margin:0 auto 16px;width:200px;height:200px;display:flex;align-items:center;justify-content:center;background:var(--szv2-surface-alt);border-radius:var(--szv2-radius-md)">
                <span style="color:var(--szv2-text-faint);font-size:12px">Carregando...</span>
            </div>
            <div style="display:flex;gap:8px;margin-bottom:8px">
                <input type="text" id="szv2-pix-code-input" class="szv2-input szv2-input-sm" readonly style="font-family:var(--szv2-font-mono);font-size:11px;flex:1" placeholder="Código PIX">
                <button type="button" class="szv2-btn szv2-btn-brand szv2-btn-sm" onclick="var i=document.getElementById('szv2-pix-code-input');navigator.clipboard.writeText(i.value);this.textContent='Copiado!';setTimeout(()=>{this.textContent='Copiar'},2000)">Copiar</button>
            </div>
            <p style="font-size:11px;color:var(--szv2-text-faint);margin:0">O PIX pode levar até 1 minuto para ser confirmado.</p>
            <div style="margin-top:12px;text-align:center">
                <button type="button" class="szv2-btn szv2-btn-brand" id="szv2-pix-check-btn"
                        onclick="szV2WeCheckPix(this)"
                        style="gap:8px">
                    Já paguei — verificar confirmação
                </button>
                <div id="szv2-pix-check-msg" style="display:none;margin-top:8px;font-size:13px"></div>
            </div>
        </div>
    </div>
</div>

<!-- Histórico de movimentações -->
<div class="szv2-card">
    <div class="szv2-card-head">
        <div><h2>Histórico</h2><p class="szv2-card-sub">Recargas e consumos de frete.</p></div>
    </div>
    <!-- Filtros de período — abaixo do cabeçalho, igual ao COD -->
    <div style="display:flex;align-items:center;gap:4px;flex-wrap:wrap;margin-bottom:12px">
        <?php
        $sz9we_periods = [ 'Hoje' => 'hoje', '7d' => '7d', '30d' => '30d', 'Tudo' => 'all' ];
        $sz9we_first   = true;
        foreach ( $sz9we_periods as $sz9we_lbl => $sz9we_slug ) : ?>
        <button type="button"
                class="szv2-period-btn <?php echo $sz9we_slug === '7d' ? 'szv2-period-btn--active' : ''; ?>"
                data-period="<?php echo esc_attr( $sz9we_slug ); ?>"
                onclick="szV2WeHistPeriod(this)"><?php echo esc_html( $sz9we_lbl ); ?></button>
        <?php $sz9we_first = false; endforeach; ?>
        <input type="date" id="szv2-we-hist-from" class="szv2-input szv2-input-sm"
               value="<?php echo esc_attr( date( 'Y-m-d', strtotime( '-7 days' ) ) ); ?>"
               style="font-size:12px;padding:3px 6px;width:128px;margin-left:4px"
               onchange="document.querySelectorAll('.szv2-period-btn').forEach(function(b){b.classList.remove('szv2-period-btn--active')});">
        <span style="font-size:12px;color:var(--szv2-text-muted)">–</span>
        <input type="date" id="szv2-we-hist-to" class="szv2-input szv2-input-sm"
               value="<?php echo esc_attr( current_time( 'Y-m-d' ) ); ?>"
               style="font-size:12px;padding:3px 6px;width:128px"
               onchange="document.querySelectorAll('.szv2-period-btn').forEach(function(b){b.classList.remove('szv2-period-btn--active')});">
    </div>

    <?php if ( empty( $sz9we_history ) ) : ?>
        <?php // phpcs:ignore WordPress.Security.EscapeOutput
        echo sz_v2_empty_state( [ 'title' => 'Nenhuma movimentação encontrada', 'text' => 'Recargas e consumos de frete aparecerão aqui.' ] ); ?>
    <?php else : ?>
    <div class="szv2-table-wrap szv2-table-flush">
        <table class="szv2-table">
            <thead>
                <tr>
                    <th>Data</th>
                    <th>Descrição</th>
                    <th>Pedido</th>
                    <th>Tipo</th>
                    <th class="szv2-td-num">Valor</th>
                    <th class="szv2-td-num">Taxa</th>
                    <th class="szv2-td-num">Líquido</th>
                </tr>
            </thead>
            <tbody>
                <?php foreach ( array_slice( $sz9we_history, 0, 60 ) as $sz9we_tx ) :
                    $sz9we_tipo = (string) ( $sz9we_tx['tipo'] ?? $sz9we_tx['type'] ?? '' );
                    $sz9we_tipo_badge = match( strtolower( $sz9we_tipo ) ) {
                        'recarga', 'credit', 'pix' => [ 'success', 'Recarga' ],
                        'frete', 'debit', 'label'  => [ 'neutral',  'Frete'   ],
                        'estorno', 'refund'         => [ 'warning',  'Estorno' ],
                        default => [ 'neutral', ucfirst( $sz9we_tipo ) ],
                    };
                ?>
                <tr>
                    <td class="szv2-td-sub"><?php echo esc_html( (string) ( $sz9we_tx['date'] ?? $sz9we_tx['created_at'] ?? '—' ) ); ?></td>
                    <td><?php echo esc_html( (string) ( $sz9we_tx['description'] ?? $sz9we_tx['descricao'] ?? '—' ) ); ?></td>
                    <td class="szv2-num"><?php echo esc_html( (string) ( $sz9we_tx['pedido'] ?? $sz9we_tx['order_id'] ?? '—' ) ); ?></td>
                    <td><span class="sz-badge szv2-badge-<?php echo esc_attr( $sz9we_tipo_badge[0] ); ?>"><?php echo esc_html( $sz9we_tipo_badge[1] ); ?></span></td>
                    <td class="szv2-td-num szv2-num"><?php echo esc_html( $sz9we_money( (float) ( $sz9we_tx['valor'] ?? $sz9we_tx['amount'] ?? 0 ) ) ); ?></td>
                    <td class="szv2-td-num szv2-num"><?php echo esc_html( $sz9we_money( (float) ( $sz9we_tx['taxa'] ?? $sz9we_tx['fee'] ?? 0 ) ) ); ?></td>
                    <td class="szv2-td-num szv2-num"><?php echo esc_html( $sz9we_money( (float) ( $sz9we_tx['liquido'] ?? $sz9we_tx['net'] ?? ( $sz9we_tx['valor'] ?? 0 ) ) ) ); ?></td>
                </tr>
                <?php endforeach; ?>
            </tbody>
        </table>
    </div>
    <?php endif; ?>
</div>
<script>
var szV2WeSelectedPreset = 0;
function szV2WeSelectPix(btn, val) {
    szV2WeSelectedPreset = val;
    document.querySelectorAll('.szv2-we-pix-preset').forEach(function(b){ b.classList.remove('szv2-btn-brand'); b.classList.add('szv2-btn-secondary'); });
    btn.classList.add('szv2-btn-brand'); btn.classList.remove('szv2-btn-secondary');
    var inp = document.getElementById('szv2-we-pix-custom');
    if (inp) { inp.value = val; }
    // Auto-generate PIX immediately on preset selection
    var pixBtn = document.getElementById('szv2-we-pix-btn');
    if (pixBtn) szV2WeGerarPix(pixBtn);
}
function szV2WeClearPreset() {
    szV2WeSelectedPreset = 0;
    document.querySelectorAll('.szv2-we-pix-preset').forEach(function(b){ b.classList.remove('szv2-btn-brand'); b.classList.add('szv2-btn-secondary'); });
}
var szV2WeRecargaId = 0;
var szV2WeAjaxUrl = '';
var szV2WeNonce = '';

function szV2WeGerarPix(btn) {
    var custom = (document.getElementById('szv2-we-pix-custom') || {}).value;
    var valor  = szV2WeSelectedPreset || parseFloat(custom) || 0;
    if (valor < 10) {
        if (typeof window.szV2Toast === 'function') { window.szV2Toast('Informe um valor mínimo de R$ 10,00.', 'warning'); }
        else { alert('Informe um valor mínimo de R$ 10.'); }
        return;
    }
    var result = document.getElementById('szv2-we-pix-result');
    if (result) { result.style.display = ''; }
    var fd = new FormData();
    fd.append('action','senderzz_portal'); fd.append('szaction','generate_pix');
    fd.append('_ajax_nonce', btn.dataset.nonce); fd.append('amount', valor);
    szV2WeAjaxUrl = btn.dataset.ajax;
    szV2WeNonce   = btn.dataset.nonce;
    fetch(btn.dataset.ajax, { method:'POST', body:fd, credentials:'same-origin' })
        .then(function(r){ return r.json(); })
        .then(function(d){
            if (d && d.success && d.data && (d.data.copia_cola || d.data.pix_code)) {
                var pixCode = d.data.copia_cola || d.data.pix_code || '';
                var pixQr   = d.data.qr_src || d.data.pix_qr || '';
                szV2WeRecargaId = d.data.recarga_id || 0;
                // Populate modal
                var codeInput = document.getElementById('szv2-pix-code-input');
                if (codeInput) codeInput.value = pixCode;
                var qrWrap = document.getElementById('szv2-pix-qr-wrap');
                if (qrWrap) {
                    qrWrap.innerHTML = pixQr
                        ? '<img src="' + pixQr + '" style="width:180px;height:180px;display:block;border-radius:4px" alt="QR Code PIX">'
                        : '<span style="color:var(--szv2-text-faint);font-size:12px">QR Code indisponível</span>';
                }
                // Open modal
                var modal = document.getElementById('szv2-pix-modal');
                if (modal) modal.style.display = 'flex';
                if (result) result.style.display = 'none';
            } else {
                var errMsg = (d&&d.data&&d.data.message) || 'Erro ao gerar PIX. Tente novamente.';
                if (result) result.innerHTML = '<p style="color:var(--szv2-danger);font-size:13px;margin:0">' + errMsg + '</p>';
            }
        })
        .catch(function(){ if(result) result.innerHTML='<p style="color:var(--szv2-danger);font-size:13px;margin:0">Erro de conexão. Tente novamente.</p>'; });
}
function szV2WeHistPeriod(btn) {
    document.querySelectorAll('[data-period]').forEach(function(b){ b.classList.remove('szv2-period-btn--active'); });
    btn.classList.add('szv2-period-btn--active');
}
function szV2WeCheckPix(btn) {
    var msg = document.getElementById('szv2-pix-check-msg');
    if (!szV2WeRecargaId) { if(msg){msg.style.display='';msg.style.color='var(--szv2-text-muted)';msg.textContent='Nenhum PIX gerado nesta sessão.';} return; }
    btn.disabled = true; btn.textContent = 'Verificando...';
    var fd = new FormData();
    fd.append('action','senderzz_portal'); fd.append('szaction','check_pix');
    fd.append('_ajax_nonce', szV2WeNonce); fd.append('recarga_id', szV2WeRecargaId);
    fetch(szV2WeAjaxUrl, { method:'POST', body:fd, credentials:'same-origin' })
        .then(function(r){ return r.json(); })
        .then(function(d){
            btn.disabled = false; btn.textContent = 'Já paguei — verificar confirmação';
            if (!msg) return;
            msg.style.display = '';
            var ok = d && d.success && d.data && d.data.confirmed;
            msg.style.color = ok ? 'var(--szv2-success)' : 'var(--szv2-text-muted)';
            msg.textContent = (d&&d.data&&d.data.message) || (ok ? 'Pagamento confirmado!' : 'Ainda não confirmado. Aguarde e tente novamente.');
            if (ok) { setTimeout(function(){ document.getElementById('szv2-pix-modal').style.display='none'; location.reload(); }, 2000); }
        })
        .catch(function(){ btn.disabled=false; btn.textContent='Já paguei — verificar confirmação'; if(msg){msg.style.display='';msg.style.color='var(--szv2-danger)';msg.textContent='Erro de conexão.';} });
}
</script>
