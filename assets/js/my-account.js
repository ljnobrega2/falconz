/* Senderzz v124: evita quebra global quando a carteira TPC é exibida no portal sem wp_localize_script. */
window.tpcData = window.tpcData || {
  apiBase: (window.wpApiSettings && window.wpApiSettings.root ? window.wpApiSettings.root.replace(/\/$/,'') : '') + '/tp-carteira/v1',
  nonce: (window.wpApiSettings && window.wpApiSettings.nonce) || window.SZ_WP_NONCE || window.SZ_NONCE || '',
  userId: 0,
  i18n: {
    confirmado: 'Pagamento confirmado! ✅',
    analise: 'Pagamento recebido. Aguardando confirmação do banco...',
    aguardando: 'Aguardando confirmação do pagamento...',
    expirado: 'PIX expirado. Gere um novo se necessário.',
    ja_paguei: 'Verificando pagamento...',
    erro_gerar: 'Não foi possível confirmar a resposta do PIX. Se o QR Code apareceu, prossiga normalmente.'
  }
};
/* global jQuery, tpcData */
(function ($) {
    'use strict';

    var state = {
        page: 1,
        perPage: 10,
        tipo: '',
        dataIni: '',
        dataFim: '',
        orderId: null,
        lastPixData: null,
        pollingTimer: null,
        expiryTimer: null,
    };

    /* ── Extrato ─────────────────────────────────────────────────────── */
    function carregarExtrato() {
        $('#tpc-extrato-lista').html('<div class="tpc-loading">Carregando...</div>');

        var params = new URLSearchParams({
            per_page: state.perPage,
            page:     state.page,
        });
        if (state.tipo)    params.set('tipo',     state.tipo);
        if (state.dataIni) params.set('data_ini', state.dataIni);
        if (state.dataFim) params.set('data_fim', state.dataFim);

        fetch(tpcData.apiBase + '/extrato?' + params.toString(), {
            headers: {
                'X-WP-Nonce': tpcData.nonce,
            },
            credentials: 'same-origin',
        })
        .then(r => r.json())
        .then(data => {
            renderExtrato(data);
        })
        .catch(() => {
            $('#tpc-extrato-lista').html('<p style="color:#c0392b">Erro ao carregar extrato.</p>');
        });
    }

    function renderExtrato(data) {
        if (!data.data || data.data.length === 0) {
            $('#tpc-extrato-lista').html('<p class="tpc-loading">Nenhuma transação encontrada.</p>');
            $('#tpc-extrato-paginacao').empty();
            return;
        }

        var html = data.data.map(function (t) {
            var sinal = t.tipo === 'credito' ? '+' : '−';
            var data_fmt = new Date(t.created_at).toLocaleDateString('pt-BR', {
                day: '2-digit', month: '2-digit', year: 'numeric',
                hour: '2-digit', minute: '2-digit'
            });
            return '<div class="tpc-transacao">' +
                '<div class="tpc-transacao-info">' +
                    '<div class="tpc-transacao-desc">' + escHtml(t.descricao) + '</div>' +
                    '<div class="tpc-transacao-data">' + data_fmt + ' · Saldo após: ' + t.saldo_apos_fmt + '</div>' +
                '</div>' +
                '<span class="tpc-transacao-badge tpc-badge-' + t.tipo + '">' + t.tipo + '</span>' +
                '<span class="tpc-transacao-valor ' + t.tipo + '">' + sinal + ' ' + t.valor_fmt + '</span>' +
            '</div>';
        }).join('');

        $('#tpc-extrato-lista').html(html);
        renderPaginacao(data.total_pages);
    }

    function renderPaginacao(totalPages) {
        if (totalPages <= 1) { $('#tpc-extrato-paginacao').empty(); return; }

        var btns = '';
        btns += '<button ' + (state.page <= 1 ? 'disabled' : '') + ' id="tpc-prev">‹ Anterior</button>';
        for (var i = 1; i <= totalPages; i++) {
            btns += '<button class="' + (i === state.page ? 'ativo' : '') + '" data-pg="' + i + '">' + i + '</button>';
        }
        btns += '<button ' + (state.page >= totalPages ? 'disabled' : '') + ' id="tpc-next">Próxima ›</button>';

        $('#tpc-extrato-paginacao').html(btns);
        $('#tpc-prev').on('click', function () { state.page--; carregarExtrato(); });
        $('#tpc-next').on('click', function () { state.page++; carregarExtrato(); });
        $('[data-pg]').on('click', function () { state.page = parseInt($(this).data('pg')); carregarExtrato(); });
    }

    /* ── Recarga PIX ─────────────────────────────────────────────────── */
    function iniciarRecarga(valor) {
        var $btn = $('#tpc-btn-recarregar');
        $btn.text('Gerando PIX...').prop('disabled', true);

        fetch(tpcData.apiBase + '/recarregar', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-WP-Nonce': tpcData.nonce,
            },
            credentials: 'same-origin',
            body: JSON.stringify({ valor: parseFloat(valor) }),
        })
        .then(r => r.json())
        .then(function (data) {
            if (data.error && !(data.pix && (data.pix.qr_src || data.pix.copia_cola))) { if(state.lastPixData){ exibirPix(state.lastPixData.pix || state.lastPixData); return; } $('#tpc-recarga-erro').hide().text(''); return; }
            state.orderId = data.recarga_id || data.order_id || state.orderId;
            state.lastPixData = data;
            exibirPix(data.pix);
            iniciarPolling();
            iniciarTimer(data.pix.expira_em);
        })
        .catch(function () {
            if(state.lastPixData){ exibirPix(state.lastPixData.pix || state.lastPixData); return; } if(!(document.querySelector('#tpc-pix-container') && getComputedStyle(document.querySelector('#tpc-pix-container')).display !== 'none')){ $('#tpc-recarga-erro').hide().text(''); }
        })
        .finally(function () {
            $btn.text('Gerar PIX').prop('disabled', false);
        });
    }

    function exibirPix(pix) {
        var src = pix.qr_src || normalizePixImg(pix.qr_code || '', pix.copia_cola || '');
        if (src) {
            $('#tpc-pix-qr-img').attr('src', src).css({width:'280px',height:'280px',objectFit:'contain'}).show();
        } else {
            $('#tpc-pix-qr-img').hide();
        }
        $('#tpc-pix-copia-cola-input').val(pix.copia_cola || pix.link || '');
        $('#tpc-pix-confirmado').hide();
        $('.tpc-pix-aguardando').show();
        $('#tpc-pix-container').slideDown(200);
        $('html, body').animate({ scrollTop: $('#tpc-pix-container').offset().top - 40 }, 400);
    }

    function normalizePixImg(qr, copia) {
        qr = String(qr || '').trim();
        copia = String(copia || '').trim();
        if (qr.indexOf('data:image') === 0) return qr;
        if (/^https?:\/\//i.test(qr)) return qr;
        if (/^[A-Za-z0-9+/=\r\n]+$/.test(qr) && qr.length > 80) return 'data:image/png;base64,' + qr.replace(/\s+/g, '');
        if (copia) return 'https://api.qrserver.com/v1/create-qr-code/?size=220x220&data=' + encodeURIComponent(copia);
        return '';
    }

    function iniciarPolling() {
        clearInterval(state.pollingTimer);
        state.pollingTimer = setInterval(function () {
            fetch(tpcData.apiBase + '/recarga/' + state.orderId + '/pix', {
                headers: { 'X-WP-Nonce': tpcData.nonce },
                credentials: 'same-origin',
            })
            .then(r => r.json())
            .then(function (data) {
                if (data.pago) {
                    clearInterval(state.pollingTimer);
                    clearInterval(state.expiryTimer);
                    $('.tpc-pix-aguardando').hide();
                    $('#tpc-novo-saldo').text('R$ ' + formatBRL(data.saldo));
                    $('#tpc-pix-confirmado').slideDown();
                    $('.tpc-saldo-valor').text('R$ ' + formatBRL(data.saldo));
                    carregarExtrato();
                }
            });
        }, 5000); // polling a cada 5s
    }

    function iniciarTimer(expiraEm) {
        clearInterval(state.expiryTimer);
        var expiry = new Date(expiraEm).getTime();

        state.expiryTimer = setInterval(function () {
            var remaining = Math.max(0, expiry - Date.now());
            var min = Math.floor(remaining / 60000);
            var sec = Math.floor((remaining % 60000) / 1000);
            $('#tpc-pix-expira-timer').text(
                String(min).padStart(2, '0') + ':' + String(sec).padStart(2, '0')
            );
            if (remaining === 0) {
                clearInterval(state.expiryTimer);
                clearInterval(state.pollingTimer);
                $('.tpc-pix-aguardando').text('PIX expirado. Gere um novo.');
            }
        }, 1000);
    }

    /* ── Helpers ─────────────────────────────────────────────────────── */
    function formatBRL(v) {
        return parseFloat(v).toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
    }
    function escHtml(s) {
        return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
    }

    /* ── Bindings ────────────────────────────────────────────────────── */
    $(document).ready(function () {
        // Extrato inicial
        carregarExtrato();

        // Filtro
        $('#tpc-filtro-form').on('submit', function (e) {
            e.preventDefault();
            state.tipo    = $('#tpc-filtro-tipo').val();
            state.dataIni = $('#tpc-filtro-ini').val();
            state.dataFim = $('#tpc-filtro-fim').val();
            state.page    = 1;
            carregarExtrato();
        });

        // Botões de valor rápido
        $(document).on('click', '.tpc-btn-valor', function () {
            $('.tpc-btn-valor').removeClass('ativo');
            $(this).addClass('ativo');
            $('#tpc-valor-input').val($(this).data('valor'));
        });

        // Botão recarregar
        $('#tpc-btn-recarregar').on('click', function () {
            var valor = parseFloat($('#tpc-valor-input').val());
            if (isNaN(valor) || valor < 10) {
                alert('Valor mínimo: R$10,00');
                return;
            }
            iniciarRecarga(valor);
        });

        // Copiar código PIX
        $('#tpc-btn-copiar').on('click', function () {
            var input = document.getElementById('tpc-pix-copia-cola-input');
            input.select();
            document.execCommand('copy');
            $(this).text('Copiado!');
            setTimeout(() => $(this).text('Copiar código'), 2000);
        });
    });

}(jQuery));
