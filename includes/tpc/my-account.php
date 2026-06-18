<?php
/**
 * my-account.php — Aba "Minha Carteira" na conta do cliente
 *
 * v2.5.18:
 *  - Botão "Já paguei" que força verificação imediata sem depender de redirect
 *  - Polling automático enquanto QR está visível (a cada 8s, máx 12 tentativas)
 *  - Timer de expiração com auto-dismiss do QR ao expirar
 *  - UX: QR some automaticamente quando confirmado ou expirado
 *  - Segurança: polling para quando confirmado (não fica rodando pra sempre)
 */
defined( 'ABSPATH' ) || exit;
if ( defined( 'SENDERZZ_TPC_MY_ACCOUNT_LOADED' ) ) return;
define( 'SENDERZZ_TPC_MY_ACCOUNT_LOADED', true );

add_action( 'init', function () {
    add_rewrite_endpoint( 'carteira', EP_ROOT | EP_PAGES );
} );

add_filter( 'woocommerce_account_menu_items', function ( $items ) {
    $new = [];
    foreach ( $items as $k => $l ) {
        $new[$k] = $l;
        if ( $k === 'orders' ) $new['carteira'] = 'Minha Carteira';
    }
    return $new;
} );

add_action( 'woocommerce_account_carteira_endpoint', 'tpc_my_account_carteira' );

add_action( 'wp_enqueue_scripts', function () {
    if ( ! is_account_page() ) return;
    wp_enqueue_style( 'sz-tokens', TPC_URL . 'assets/css/senderzz-tokens.css', [], defined( 'TPC_VERSION' ) ? TPC_VERSION : '1.0' );
    wp_enqueue_style( 'tpc-my-account', TPC_URL . 'assets/css/my-account.css', [ 'sz-tokens' ], TPC_VERSION );
    wp_enqueue_style( 'sz-typography', TPC_URL . 'assets/css/senderzz-typography.css', [ 'tpc-my-account' ], TPC_VERSION );
    wp_enqueue_script( 'tpc-my-account', TPC_URL . 'assets/js/my-account.js', [ 'jquery' ], TPC_VERSION, true );
    wp_localize_script( 'tpc-my-account', 'tpcData', [
        'apiBase' => rest_url( 'tp-carteira/v1' ),
        'nonce'   => wp_create_nonce( 'wp_rest' ),
        'userId'  => get_current_user_id(),
        'i18n'    => [
            'confirmado'   => 'Pagamento confirmado! ✅',
            'analise'      => 'Pagamento recebido. Aguardando confirmação do banco...',
            'aguardando'   => 'Aguardando confirmação do pagamento...',
            'expirado'     => 'PIX expirado. Gere um novo se necessário.',
            'ja_paguei'    => 'Verificando pagamento...',
            'erro_gerar'   => 'Erro ao gerar PIX. Tente novamente.',
        ],
    ] );
} );

function tpc_my_account_carteira(): void {
    if ( ! is_user_logged_in() ) { echo '<p>Faça login para acessar sua carteira.</p>'; return; }

    $user_id = get_current_user_id();
    $saldo   = tpc_get_saldo( $user_id );
    $bkd     = function_exists( 'tpc_admin_get_user_wallet_breakdown' ) ? tpc_admin_get_user_wallet_breakdown( $user_id ) : [ 'saldo' => $saldo, 'reservado' => 0, 'disponivel' => $saldo ];
    $alerta  = (float) get_option( 'tpc_saldo_minimo', 0 );
    ?>
    <div class="tpc-carteira-wrapper">

        <!-- Saldo -->
        <div class="tpc-saldo-card <?php echo ( $alerta > 0 && $saldo <= $alerta ) ? 'tpc-saldo-baixo' : ''; ?>">
            <span class="tpc-saldo-label">Saldo disponível</span>
            <span class="tpc-saldo-valor" id="tpc-saldo-display">R$ <?php echo number_format( (float) $bkd['disponivel'], 2, ',', '.' ); ?></span>
            <?php if ( $alerta > 0 && $saldo <= $alerta ) : ?>
                <span class="tpc-saldo-alerta">⚠ Saldo baixo. Recarregue para continuar enviando.</span>
            <?php endif; ?>
        </div>

        <div class="tpc-subnav" role="tablist" aria-label="Carteira de Expedição">
            <button type="button" class="tpc-subtab ativo" data-tpc-tab="pix">Recarga PIX</button>
            <button type="button" class="tpc-subtab" data-tpc-tab="historico">Histórico</button>
            <button type="button" class="tpc-subtab" data-tpc-tab="recargas">Recargas</button>
        </div>

        <div class="tpc-panel tpc-panel-pix ativo" data-tpc-panel="pix">
        <!-- Recarga -->
        <div class="tpc-recarga-section" id="tpc-recarga-section">
            <h3>Recarregar carteira via PIX</h3>
            <div class="tpc-valores-rapidos">
                <?php foreach ( [ 50, 100, 200, 500 ] as $v ) : ?>
                    <button class="tpc-btn-valor button" data-valor="<?php echo $v; ?>">R$ <?php echo number_format( $v, 0, ',', '.' ); ?></button>
                <?php endforeach; ?>
            </div>
            <div class="tpc-recarga-form" style="margin-top:12px;display:flex;gap:8px;align-items:center;">
                <input type="number" id="tpc-valor-input" placeholder="Outro valor (mín. R$ 10)" min="10" step="0.01" style="max-width:200px;">
                <button id="tpc-btn-recarregar" class="button alt">Gerar PIX</button>
            </div>
            <div id="tpc-recarga-loading" style="display:none;margin-top:10px;">Gerando PIX...</div>
            <div id="tpc-recarga-erro" style="display:none;color:#c00;margin-top:8px;"></div>
            <div class="tpc-pix-helper" style="display:none"></div>
        </div>

        <!-- QR Code / PIX -->
        <div id="tpc-pix-container" style="display:none">
            <div class="tpc-pix-card" style="border:1px solid #ddd;border-radius:8px;padding:20px;max-width:420px;margin-top:20px;">
                <h3 style="margin-top:0">Pague via PIX</h3>

                <div class="tpc-pix-qr" style="text-align:center;margin-bottom:16px;">
                    <img id="tpc-pix-qr-img" src="" alt="QR Code PIX" style="max-width:220px;border-radius:4px;">
                </div>

                <div class="tpc-pix-copia-cola" style="display:flex;gap:8px;margin-bottom:12px;">
                    <input type="text" id="tpc-pix-copia-cola-input" readonly style="flex:1;font-size:var(--sz-text-meta);">
                    <button id="tpc-btn-copiar" class="button">Copiar</button>
                </div>

                <p style="margin:8px 0;font-size:var(--sz-text-base);color:#555;">
                    Expira em: <strong id="tpc-pix-expira-timer">--:--</strong>
                </p>

                <div id="tpc-pix-status-msg" class="tpc-pix-aguardando" style="margin:12px 0;padding:10px;background:#f9f9f9;border-radius:4px;">
                    Aguardando confirmação do pagamento...
                </div>

                <!-- Botão "Já paguei" -->
                <button id="tpc-btn-ja-paguei" class="button alt" style="width:100%;padding:12px;font-size:var(--sz-text-md);margin-bottom:8px;">
                    ✅ Já paguei — confirmar agora
                </button>

                <button id="tpc-btn-cancelar-pix" class="button" style="width:100%;font-size:var(--sz-text-meta);color:#888;background:none;border:1px solid #ddd;">
                    Cancelar / Gerar outro valor
                </button>
            </div>
        </div>

        <!-- Confirmação -->
        <div id="tpc-pix-confirmado" style="display:none;margin-top:16px;padding:16px;background:#e8f5e9;border-radius:8px;border:1px solid #a5d6a7;">
            <strong>✅ Pagamento confirmado!</strong><br>
            Novo saldo: <strong id="tpc-novo-saldo"></strong>
        </div>
        </div>

        <!-- Extrato -->
        <div class="tpc-extrato-section tpc-panel" data-tpc-panel="historico" style="margin-top:0;display:none;">
            <h3>Extrato</h3>
            <form class="tpc-filtros" id="tpc-filtro-form" style="display:flex;gap:8px;flex-wrap:wrap;margin-bottom:12px;">
                <select id="tpc-filtro-tipo">
                    <option value="">Todos</option>
                    <option value="credito">Créditos</option>
                    <option value="debito">Débitos</option>
                </select>
                <input type="date" id="tpc-filtro-ini">
                <input type="date" id="tpc-filtro-fim">
                <button type="submit" class="button">Filtrar</button>
            </form>
            <div id="tpc-extrato-lista"><div class="tpc-loading">Carregando...</div></div>
            <div id="tpc-extrato-paginacao" style="margin-top:12px;"></div>
        </div>

        <div class="tpc-recargas-section tpc-panel" data-tpc-panel="recargas" style="display:none;">
            <h3>Recargas</h3>
            <p class="tpc-loading">Use o histórico filtrando por créditos para acompanhar as recargas PIX concluídas.</p>
        </div>

    </div>

    <script>
    /* Lógica inline de recarga PIX — complementa my-account.js */
    (function($) {
        function tpcSetTab(tab){
            tab = tab || 'pix';
            $('.tpc-subtab').removeClass('ativo').filter('[data-tpc-tab="'+tab+'"]').addClass('ativo');
            $('.tpc-panel').hide().removeClass('ativo');
            $('.tpc-panel[data-tpc-panel="'+tab+'"]').show().addClass('ativo');
        }
        $(document).on('click','.tpc-subtab',function(){ tpcSetTab($(this).data('tpc-tab')); });
        $(function(){ tpcSetTab('pix'); });
        var recargaAtiva = null;
        var ultimoPixData = null;
        var pollingTimer = null;
        var pollingCount = 0;
        var MAX_POLLS = 18; // 18 x 8s = ~2.5min máx de polling ativo
        var expiraTimer = null;

        // Selecionar valor rápido
        $(document).on('click', '.tpc-btn-valor', function() {
            $('#tpc-valor-input').val($(this).data('valor'));
            $('.tpc-btn-valor').removeClass('button-primary');
            $(this).addClass('button-primary');
        });

        // Gerar PIX
        $('#tpc-btn-recarregar').on('click', function() {
            var valor = parseFloat($('#tpc-valor-input').val());
            if (!valor || valor < 10) { mostrarErro('Valor mínimo: R$ 10,00'); return; }

            $('#tpc-recarga-erro').hide();
            $('#tpc-recarga-loading').show();
            $(this).prop('disabled', true);

            $.ajax({
                url: tpcData.apiBase + '/recarregar',
                method: 'POST',
                headers: { 'X-WP-Nonce': tpcData.nonce },
                contentType: 'application/json',
                data: JSON.stringify({ valor: valor }),
                success: function(r) {
                    $('#tpc-recarga-loading').hide();
                    $('#tpc-btn-recarregar').prop('disabled', false);
                    if (r && (r.recarga_id || r.order_id || (r.pix && (r.pix.qr_src || r.pix.copia_cola)))) {
                        mostrarPix(r);
                    } else {
                        var pixVisivel = $('#tpc-pix-container:visible').length || $('#tpc-pix-qr-img:visible').length || $('#tpc-pix-copia-cola-input').val();
                        if (!pixVisivel) { $('#tpc-recarga-erro').hide().text(''); }
                    }
                },
                error: function(xhr) {
                    $('#tpc-recarga-loading').hide();
                    $('#tpc-btn-recarregar').prop('disabled', false);
                    var data = xhr.responseJSON || {};
                    if (data && (data.recarga_id || data.order_id || (data.pix && (data.pix.qr_src || data.pix.copia_cola)))) {
                        mostrarPix(data);
                        return;
                    }
                    var pixVisivel = $('#tpc-pix-container:visible').length || $('#tpc-pix-qr-img:visible').length || $('#tpc-pix-copia-cola-input').val();
                    if (pixVisivel || ultimoPixData) { $('#tpc-recarga-erro').hide().text(''); if(ultimoPixData) mostrarPix(ultimoPixData); return; }
                    $('#tpc-recarga-erro').hide().text('');
                }
            });
        });

        function mostrarPix(data) {
            ultimoPixData = data || ultimoPixData;
            recargaAtiva = data.recarga_id || data.order_id || recargaAtiva || null;
            pollingCount = 0;

            // QR e copia-e-cola
            if (data.pix && data.pix.qr_src) {
                $('#tpc-pix-qr-img').attr('src', data.pix.qr_src).css({width:'280px',height:'280px',objectFit:'contain'}).show();
            } else {
                $('#tpc-pix-qr-img').hide();
            }
            var copia = (data.pix && data.pix.copia_cola) ? data.pix.copia_cola : '';
            $('#tpc-pix-copia-cola-input').val(copia);

            // Timer de expiração
            var expiresTs = (data.pix && data.pix.expires_ts) ? data.pix.expires_ts * 1000 : 0;
            iniciarTimerExpiracao(expiresTs);

            tpcSetTab('pix');
            $('#tpc-pix-status-msg').text(tpcData.i18n.aguardando).css('background', '#f9f9f9');
            $('#tpc-pix-container').show();
            $('#tpc-pix-confirmado').hide();
            $('#tpc-recarga-section').hide();

            // Inicia polling
            iniciarPolling();
        }

        function iniciarTimerExpiracao(expiresMs) {
            if (expiraTimer) clearInterval(expiraTimer);
            if (!expiresMs) { $('#tpc-pix-expira-timer').text('--:--'); return; }

            expiraTimer = setInterval(function() {
                var diff = Math.floor((expiresMs - Date.now()) / 1000);
                if (diff <= 0) {
                    clearInterval(expiraTimer);
                    pararPolling();
                    $('#tpc-pix-expira-timer').text('00:00');
                    $('#tpc-pix-status-msg').text(tpcData.i18n.expirado).css('background', '#fff3e0');
                    $('#tpc-btn-ja-paguei').prop('disabled', true);
                    return;
                }
                var m = Math.floor(diff / 60), s = diff % 60;
                $('#tpc-pix-expira-timer').text((m < 10 ? '0' : '') + m + ':' + (s < 10 ? '0' : '') + s);
            }, 1000);
        }

        function iniciarPolling() {
            if (pollingTimer) clearInterval(pollingTimer);
            pollingTimer = setInterval(function() {
                if (!recargaAtiva) { pararPolling(); return; }
                pollingCount++;
                if (pollingCount > MAX_POLLS) { pararPolling(); return; }
                verificarStatus(false);
            }, 8000);
        }

        function pararPolling() {
            if (pollingTimer) { clearInterval(pollingTimer); pollingTimer = null; }
        }

        function verificarStatus(isManual) {
            if (!recargaAtiva) return;
            $.ajax({
                url: tpcData.apiBase + '/recarga/' + recargaAtiva + '/pix',
                headers: { 'X-WP-Nonce': tpcData.nonce },
                success: function(r) {
                    if (r.pago) {
                        confirmarUI(r.saldo_fmt);
                    } else if (r.em_analise) {
                        $('#tpc-pix-status-msg').text(tpcData.i18n.analise).css('background', '#fff8e1');
                    }
                }
            });
        }

        // Botão "Já paguei" — apenas fecha o QR e mostra mensagem de prazo
        $('#tpc-btn-ja-paguei').on('click', function() {
            if (!recargaAtiva) return;
            var rid = recargaAtiva;
            $(this).prop('disabled', true);

            // Registra no servidor (só para log/rastreabilidade, não credita)
            $.ajax({
                url: tpcData.apiBase + '/pix/' + rid + '/ja-paguei',
                method: 'POST',
                headers: { 'X-WP-Nonce': tpcData.nonce },
                complete: function(xhr) {
                    var r = xhr.responseJSON || {};
                    // Se por acaso já foi confirmado automaticamente, mostra saldo
                    if (r.confirmado) {
                        confirmarUI(r.saldo_fmt || null);
                        return;
                    }
                    // Caso normal: fecha QR e mostra mensagem de prazo
                    pararPolling();
                    if (expiraTimer) clearInterval(expiraTimer);
                    recargaAtiva = null;
                    $('#tpc-pix-container').hide();
                    $('#tpc-recarga-section').show();
                    $('#tpc-pix-confirmado')
                        .html('<strong>✅ Pagamento enviado!</strong><br>Seu crédito será processado em até <strong>30 minutos</strong>. Em casos de análise pelo banco, pode levar até <strong>24 horas</strong>. Você receberá um e-mail quando o saldo for creditado.')
                        .css('background', '#e3f2fd')
                        .show();
                    setTimeout(function() { $('#tpc-pix-confirmado').fadeOut(1500); }, 15000);
                }
            });
        });

        // Cancelar PIX
        $('#tpc-btn-cancelar-pix').on('click', function() {
            pararPolling();
            if (expiraTimer) clearInterval(expiraTimer);
            recargaAtiva = null;
            $('#tpc-pix-container').hide();
            $('#tpc-recarga-section').show();
            $('#tpc-valor-input').val('');
            $('.tpc-btn-valor').removeClass('button-primary');
        });

        // Copiar copia-e-cola
        $('#tpc-btn-copiar').on('click', function() {
            var val = $('#tpc-pix-copia-cola-input').val();
            if (!val) return;
            navigator.clipboard ? navigator.clipboard.writeText(val) : document.execCommand('copy');
            $(this).text('Copiado!');
            setTimeout(() => $(this).text('Copiar'), 2000);
        });

        function confirmarUI(saldoFmt) {
            pararPolling();
            if (expiraTimer) clearInterval(expiraTimer);
            recargaAtiva = null;
            $('#tpc-pix-container').hide();
            $('#tpc-recarga-section').show();
            if (saldoFmt) {
                $('#tpc-novo-saldo').text(saldoFmt);
                $('#tpc-saldo-display').text(saldoFmt);
            }
            $('#tpc-pix-confirmado').show();
            setTimeout(function() { $('#tpc-pix-confirmado').fadeOut(1000); }, 8000);
        }

        function mostrarErro(msg) {
            var pixVisivel = $('#tpc-pix-container:visible').length || ($('#tpc-pix-copia-cola-input').val() || '').length;
            if (pixVisivel || ultimoPixData) { $('#tpc-recarga-erro').hide().text(''); if(ultimoPixData) mostrarPix(ultimoPixData); return; }
            $('#tpc-recarga-erro').text(msg).show();
        }

        // Carrega extrato na abertura
        carregarExtrato(1);
        $('#tpc-filtro-form').on('submit', function(e) { e.preventDefault(); carregarExtrato(1); });

        function carregarExtrato(page) {
            var params = {
                tipo:   $('#tpc-filtro-tipo').val(),
                from:   $('#tpc-filtro-ini').val(),
                to:     $('#tpc-filtro-fim').val(),
                limit:  20,
                page:   page,
            };
            $('#tpc-extrato-lista').html('<div>Carregando...</div>');
            $.ajax({
                url: tpcData.apiBase + '/extrato',
                headers: { 'X-WP-Nonce': tpcData.nonce },
                data: params,
                success: function(r) {
                    if (!r.data || !r.data.length) {
                        $('#tpc-extrato-lista').html('<p>Nenhuma movimentação encontrada.</p>');
                        $('#tpc-extrato-paginacao').empty();
                        return;
                    }
                    var html = '<table style="width:100%;border-collapse:collapse"><thead><tr><th style="text-align:left;padding:6px;border-bottom:1px solid #ddd">Data</th><th style="text-align:left;padding:6px;border-bottom:1px solid #ddd">Descrição</th><th style="text-align:right;padding:6px;border-bottom:1px solid #ddd">Valor</th><th style="text-align:right;padding:6px;border-bottom:1px solid #ddd">Saldo após</th></tr></thead><tbody>';
                    r.data.forEach(function(t) {
                        var color = t.tipo === 'credito' ? '#2e7d32' : '#c62828';
                        var sinal = t.tipo === 'credito' ? '+' : '-';
                        html += '<tr><td style="padding:6px;border-bottom:1px solid #f0f0f0;font-size:var(--sz-text-meta)">' + (t.created_at || '').substring(0,16) + '</td>';
                        html += '<td style="padding:6px;border-bottom:1px solid #f0f0f0;font-size:var(--sz-text-meta)">' + (t.descricao || '') + '</td>';
                        html += '<td style="padding:6px;border-bottom:1px solid #f0f0f0;text-align:right;color:' + color + ';font-weight:700">' + sinal + 'R$ ' + parseFloat(t.valor).toFixed(2).replace('.', ',') + '</td>';
                        html += '<td style="padding:6px;border-bottom:1px solid #f0f0f0;text-align:right;font-size:var(--sz-text-meta)">R$ ' + parseFloat(t.saldo_apos || 0).toFixed(2).replace('.', ',') + '</td></tr>';
                    });
                    html += '</tbody></table>';
                    $('#tpc-extrato-lista').html(html);
                }
            });
        }

    })(jQuery);
    </script>
    <?php
}
