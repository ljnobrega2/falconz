<?php
/**
 * Senderzz Dashboard V2 — Carteira (Fase 10 — Saque COD V2 real)
 * ---------------------------------------------------------------
 * Saque V2 ativo: controlado pela flag `senderzz_dashboard_v2_withdraw_enabled`.
 * Default 'no' → botão/modal nunca renderizam.
 * Quando 'yes': somente produtor principal com saldo > 0 vê o botão.
 *
 * Endpoints usados (todos V2):
 *   GET  /wp-json/sz-portal/v2/cod/accounts  — lista contas PIX
 *   POST /wp-json/sz-portal/v2/cod/withdraw  — solicita saque
 *
 * Nenhuma chamada a sz-portal/v1.
 *
 * @var object $sz_v2_user
 */
defined( 'ABSPATH' ) || exit;

$sz8wl_role   = strtolower( trim( (string) ( $sz_v2_user->role ?? 'client' ) ) );
$sz8wl_is_aff = ( 'affiliate' === $sz8wl_role );
$sz8wl_is_sub = ! empty( $sz_v2_user->parent_user_id );

// Feature flag — default 'no'.
$sz8wl_withdraw_flag = get_option( 'senderzz_dashboard_v2_withdraw_enabled', 'no' );
$sz8wl_can_withdraw  = ( 'yes' === $sz8wl_withdraw_flag && ! $sz8wl_is_aff && ! $sz8wl_is_sub );

$sz8wl_w_uid  = function_exists( 'senderzz_portal_wallet_user_id' )
    ? (int) senderzz_portal_wallet_user_id( $sz_v2_user ) : 0;
$sz8wl_wp_uid = (int) ( $sz_v2_user->wp_user_id ?? $sz8wl_w_uid );

$sz8wl_money = static fn( float $v ): string =>
    function_exists( 'senderzz_portal_money' ) ? senderzz_portal_money( $v ) : 'R$ ' . number_format( $v, 2, ',', '.' );

// COD
$sz8wl_cod = ( $sz8wl_w_uid && function_exists( 'sz_cod_wallet_summary' ) )
    ? sz_cod_wallet_summary( $sz8wl_w_uid ) : [ 'available' => 0.0, 'pending' => 0.0, 'analysis' => 0.0 ];

$sz8wl_available = (float) ( $sz8wl_cod['available'] ?? 0 );

// TPC / Expedição (somente produtor com expedição)
$sz8wl_tpc_avail = 0.0;
$sz8wl_has_exp   = false;
if ( ! $sz8wl_is_aff && $sz8wl_wp_uid ) {
    $sz8wl_has_exp = ! function_exists( 'sz_producer_has_expedicao' ) || sz_producer_has_expedicao( $sz8wl_wp_uid );
    if ( $sz8wl_has_exp && function_exists( 'tpc_get_saldo_disponivel' ) ) {
        $sz8wl_tpc_avail = (float) tpc_get_saldo_disponivel( $sz8wl_wp_uid );
    }
}

// Extrato recente COD (30d)
$sz8wl_history = ( $sz8wl_w_uid && function_exists( 'sz_cod_wallet_history' ) )
    ? sz_cod_wallet_history( $sz8wl_w_uid, '30d' ) : [];

// Lançamentos futuros COD
$sz8wl_future_raw   = ( $sz8wl_w_uid && function_exists( 'sz_cod_wallet_future' ) )
    ? sz_cod_wallet_future( $sz8wl_w_uid, '30d' ) : [];
$sz8wl_future_rows  = is_array( $sz8wl_future_raw['data'] ?? null ) ? $sz8wl_future_raw['data'] : [];
$sz8wl_future_total = (float) ( $sz8wl_future_raw['total'] ?? 0 );

// Histórico de saques — somente produtor (leitura pura)
$sz8wl_show_wd_history = ( ! $sz8wl_is_aff && ! $sz8wl_is_sub && $sz8wl_w_uid );
$sz8wl_withdrawals     = ( $sz8wl_show_wd_history && function_exists( 'sz_cod_user_withdrawals' ) )
    ? sz_cod_user_withdrawals( $sz8wl_w_uid ) : [];

// Taxa server-side — para exibir no modal antes do submit (informativo).
// Valor real é recalculado no backend; o front nunca confia neste valor.
$sz8wl_fee_flat    = 0.0;
$sz8wl_ant_fee_pct = 0.0;
if ( $sz8wl_can_withdraw && $sz8wl_w_uid && function_exists( 'sz_cod_get_rules_for_user' ) ) {
    $sz8wl_rules       = sz_cod_get_rules_for_user( $sz8wl_w_uid );
    $sz8wl_fee_flat    = (float) ( $sz8wl_rules['withdraw_fee'] ?? 0 );
    $sz8wl_ant_fee_pct = (float) ( $sz8wl_rules['anticipation_fee_pct'] ?? 4.99 );
}

// Nonce REST e base URL — passados via data attributes (mesma convenção de motoboy.php)
$sz8wl_rest_nonce      = $sz8wl_can_withdraw ? wp_create_nonce( 'wp_rest' ) : '';
$sz8wl_rest_base       = $sz8wl_can_withdraw ? esc_attr( rtrim( rest_url( 'sz-portal/v2' ), '/' ) ) : '';
// Nonce financeiro: vinculado ao usuário do portal E à sessão ativa.
// Gerado server-side — o cookie é HttpOnly, impossível de ler no JS.
// Se o usuário fizer logout/login, este nonce invalida automaticamente.
$sz8wl_financial_nonce = ( $sz8wl_can_withdraw && function_exists( 'sz_portal_v2_create_financial_nonce' ) )
    ? sz_portal_v2_create_financial_nonce( $sz_v2_user ) : '';

// Mapa de status de saque → variante + label (local, sem sz_v2_status_badge com 2 args)
$sz8wl_wd_status_map = [
    'analysis'  => [ 'warning',   'Em análise' ],
    'pending'   => [ 'warning',   'Pendente'   ],
    'approved'  => [ 'success',   'Aprovado'   ],
    'paid'      => [ 'completed', 'Concluído'  ],
    'rejected'  => [ 'neutral',   'Recusado'   ],
    'cancelled' => [ 'neutral',   'Cancelado'  ],
];
?>
<section id="sec-wallet" class="sz-sec" data-szv2-label="Carteira">
    <!-- Abas Carteira COD / Expedição -->
    <div class="szv2-prod-subtabs" role="tablist" style="margin-bottom:var(--szv2-space-3)">
        <button type="button" class="szv2-prod-subtab szv2-prod-subtab--active" role="tab" aria-selected="true" id="szv2-wlt-tab-cod" onclick="szV2WltSwitch('cod',this)">
            Carteira COD
        </button>
        <?php if ( $sz8wl_has_exp ) : ?>
        <button type="button" class="szv2-prod-subtab" role="tab" aria-selected="false" id="szv2-wlt-tab-exp" onclick="szV2WltSwitch('exp',this)">
            Carteira de Expedição
        </button>
        <?php endif; ?>
    </div>
    <script>
    function szV2WltSwitch(mode, btn) {
        document.querySelectorAll('#sec-wallet .szv2-prod-subtab').forEach(function(t){ t.classList.remove('szv2-prod-subtab--active'); });
        btn.classList.add('szv2-prod-subtab--active');
        var cod = document.getElementById('szv2-wlt-cod-body');
        var exp = document.getElementById('szv2-wallet-exp-panel');
        if (cod) cod.style.display = (mode === 'cod') ? '' : 'none';
        if (exp) exp.style.display = (mode === 'exp') ? '' : 'none';
    }
    </script>
    <div id="szv2-wlt-cod-body">

    <!-- KPIs: 3 cards (saldo, pendente, análise) -->
    <div class="szv2-kpi-grid">
        <?php
        // phpcs:ignore WordPress.Security.EscapeOutput
        echo sz_v2_kpi_card( [ 'label' => 'Saldo disponível', 'value' => $sz8wl_money( $sz8wl_available ), 'meta' => 'Disponível para saque', 'value_class' => 'szv2-num' ] );
        echo sz_v2_kpi_card( [ 'label' => 'Saldo pendente',   'value' => $sz8wl_money( (float) ( $sz8wl_cod['pending']  ?? 0 ) ), 'meta' => 'Aguardando liberação', 'value_class' => 'szv2-num' ] );
        echo sz_v2_kpi_card( [ 'label' => 'Saque em análise', 'value' => $sz8wl_money( (float) ( $sz8wl_cod['analysis'] ?? 0 ) ), 'meta' => 'Aguardando aprovação', 'value_class' => 'szv2-num' ] );
        ?>
    </div>

    <!-- Sub-abas Transações / Saques + ações -->
    <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:var(--szv2-space-3);margin-top:var(--szv2-space-4)">
        <div class="szv2-prod-subtabs" role="tablist" style="margin-bottom:0">
            <button type="button" class="szv2-prod-subtab szv2-prod-subtab--active" role="tab" aria-selected="true" id="szv2-wlt-cod-tab-tx" onclick="szV2WltCodTab('transacoes',this)">Transações</button>
            <button type="button" class="szv2-prod-subtab" role="tab" aria-selected="false" id="szv2-wlt-cod-tab-wd" onclick="szV2WltCodTab('saques',this)">Saques</button>
        </div>
        <div style="display:flex;gap:8px">
            <?php if ( $sz8wl_can_withdraw ) : ?>
            <button type="button"
                    class="szv2-btn szv2-btn-brand szv2-wd-open-btn"
                    data-available="<?php echo esc_attr( number_format( $sz8wl_available, 2, '.', '' ) ); ?>"
                    data-fee="<?php echo esc_attr( number_format( $sz8wl_fee_flat, 2, '.', '' ) ); ?>"
                    data-rest-base="<?php echo $sz8wl_rest_base; ?>"
                    data-nonce="<?php echo esc_attr( $sz8wl_rest_nonce ); ?>"
                    data-financial-nonce="<?php echo esc_attr( $sz8wl_financial_nonce ); ?>">
                Saque
            </button>
            <?php else : ?>
            <button type="button" class="szv2-btn szv2-btn-brand" disabled title="Saque não disponível para este perfil">
                Saque
            </button>
            <?php endif; ?>
            <?php if ( $sz8wl_can_withdraw ) : ?>
            <button type="button" class="szv2-btn szv2-btn-secondary" id="szv2-wlt-antecipar-btn"
                    data-future="<?php echo esc_attr( number_format( $sz8wl_future_total, 2, '.', '' ) ); ?>"
                    data-ant-fee-pct="<?php echo esc_attr( number_format( $sz8wl_ant_fee_pct, 2, '.', '' ) ); ?>"
                    data-rest-base="<?php echo $sz8wl_rest_base; ?>"
                    data-nonce="<?php echo esc_attr( $sz8wl_rest_nonce ); ?>"
                    data-financial-nonce="<?php echo esc_attr( $sz8wl_financial_nonce ); ?>"
                    onclick="szV2WltAntecipar(this)">Antecipar</button>
            <?php else : ?>
            <button type="button" class="szv2-btn szv2-btn-secondary" disabled title="Antecipação não disponível para este perfil">Antecipar</button>
            <?php endif; ?>
        </div>
    </div>
    <script>
    function szV2WltCodTab(mode, btn) {
        document.querySelectorAll('#szv2-wlt-cod-body .szv2-prod-subtab').forEach(function(b){ b.classList.remove('szv2-prod-subtab--active'); });
        btn.classList.add('szv2-prod-subtab--active');
        var tx = document.getElementById('szv2-wlt-cod-transacoes');
        var wd = document.getElementById('szv2-wlt-cod-saques');
        if (tx) tx.style.display = mode === 'transacoes' ? '' : 'none';
        if (wd) wd.style.display = mode === 'saques'     ? '' : 'none';
    }
    window.szV2WltAntecipar = function(btn) {
        var future = parseFloat(btn.dataset.future || '0');
        if (future <= 0) {
            var toast = document.getElementById('szv2-ant-toast');
            if (!toast) {
                toast = document.createElement('div');
                toast.id = 'szv2-ant-toast';
                toast.style.cssText = 'position:fixed;bottom:24px;left:50%;transform:translateX(-50%);background:var(--szv2-surface);border:1px solid var(--szv2-border);border-radius:var(--szv2-radius-md);padding:12px 20px;font-size:13px;color:var(--szv2-text);box-shadow:0 4px 16px rgba(0,0,0,.12);z-index:9999;white-space:nowrap';
                document.body.appendChild(toast);
            }
            toast.textContent = 'Não há valores disponíveis para antecipar no momento.';
            toast.style.display = 'block';
            setTimeout(function(){ toast.style.display = 'none'; }, 3500);
            return;
        }

        // Abre modal de antecipação COD V2
        var modal   = document.getElementById('szv2-ant-modal');
        var body    = document.getElementById('szv2-ant-modal-body');
        var restBase      = btn.dataset.restBase      || '';
        var nonce         = btn.dataset.nonce         || '';
        var financialNonce = btn.dataset.financialNonce || '';
        var antFeePct     = parseFloat(btn.dataset.antFeePct || '0');

        if (!modal || !body || !restBase) return;

        // Formatar moeda local
        var szFmtMoney = function(v) {
            return 'R$ ' + parseFloat(v).toFixed(2).replace('.', ',').replace(/\B(?=(\d{3})+(?!\d))/g, '.');
        };

        var fee = Math.round(future * (antFeePct / 100) * 100) / 100;
        var net = Math.round(Math.max(0, future - fee) * 100) / 100;

        // Renderiza corpo do modal com resumo e select de contas
        body.innerHTML = '<p style="font-size:13px;color:var(--szv2-text-muted)">Carregando contas...</p>';
        modal.style.display = 'flex';
        modal.classList.add('szv2-open');

        // Busca contas PIX via /cod/accounts
        fetch(restBase + '/cod/accounts', {
            method: 'GET',
            headers: {
                'X-WP-Nonce': nonce,
                'X-Senderzz-Financial-Nonce': financialNonce
            },
            credentials: 'same-origin'
        })
        .then(function(r){ return r.json(); })
        .then(function(data) {
            var accounts = (data && data.accounts) ? data.accounts : [];

            var feeInfo = antFeePct > 0
                ? '<div style="display:flex;justify-content:space-between;padding:6px 0;font-size:13px"><span style="color:var(--szv2-text-muted)">Taxa de antecipação (' + antFeePct.toFixed(2).replace('.', ',') + '%)</span><span>' + szFmtMoney(fee) + '</span></div>'
                  + '<div style="display:flex;justify-content:space-between;padding:6px 0;font-size:14px;font-weight:600;border-top:1px solid var(--szv2-border);margin-top:4px"><span>Valor a receber (líquido)</span><span style="color:var(--szv2-brand)">' + szFmtMoney(net) + '</span></div>'
                : '<div style="display:flex;justify-content:space-between;padding:6px 0;font-size:14px;font-weight:600;border-top:1px solid var(--szv2-border);margin-top:4px"><span>Valor a receber</span><span style="color:var(--szv2-brand)">' + szFmtMoney(future) + '</span></div>';

            var acctOptions = '';
            if (accounts.length === 0) {
                acctOptions = '<option value="">Nenhuma conta PIX cadastrada</option>';
            } else {
                acctOptions = accounts.map(function(a) {
                    var label = (a.holder_name || 'Conta') + ' · PIX ' + (a.pix_type || '').toUpperCase() + ': ' + (a.pix_key_masked || a.pix_key || '');
                    return '<option value="' + parseInt(a.id, 10) + '">' + label + '</option>';
                }).join('');
            }

            body.innerHTML =
                '<div style="background:rgba(234,88,12,.08);border-radius:var(--szv2-radius-sm);padding:12px 14px;margin-bottom:14px">'
                + '<div style="display:flex;justify-content:space-between;padding:0 0 6px;font-size:13px"><span style="color:var(--szv2-text-muted)">Total a antecipar</span><span class="szv2-num" style="font-size:15px;font-weight:600">' + szFmtMoney(future) + '</span></div>'
                + feeInfo
                + '</div>'
                + '<div class="szv2-field" style="margin-bottom:14px">'
                + '<label class="szv2-label" for="szv2-ant-account-select">Conta para recebimento</label>'
                + '<select id="szv2-ant-account-select" class="szv2-input">' + acctOptions + '</select>'
                + (accounts.length === 0 ? '<span class="szv2-field-hint">Adicione uma conta PIX em <strong>Configurações &rarr; Saques</strong>.</span>' : '')
                + '</div>'
                + '<div id="szv2-ant-msg" style="display:none;font-size:13px;color:#dc2626;margin-bottom:8px"></div>'
                + '<div style="display:flex;gap:8px;justify-content:flex-end">'
                + '<button type="button" class="szv2-btn szv2-btn-secondary" id="szv2-ant-cancel-btn">Cancelar</button>'
                + '<button type="button" class="szv2-btn szv2-btn-brand" id="szv2-ant-confirm-btn"'
                + (accounts.length === 0 ? ' disabled' : '') + '>Confirmar antecipação</button>'
                + '</div>';

            // Fechar modal ao cancelar
            document.getElementById('szv2-ant-cancel-btn').addEventListener('click', function() {
                modal.classList.remove('szv2-open');
                modal.style.display = 'none';
            });

            if (accounts.length === 0) return;

            // Submit de antecipação
            document.getElementById('szv2-ant-confirm-btn').addEventListener('click', function() {
                var accountId = parseInt(document.getElementById('szv2-ant-account-select').value, 10);
                if (!accountId) return;

                var confirmBtn = this;
                var msgEl      = document.getElementById('szv2-ant-msg');
                confirmBtn.disabled = true;
                confirmBtn.textContent = 'Processando...';
                if (msgEl) msgEl.style.display = 'none';

                fetch(restBase + '/cod/anticipate', {
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json',
                        'X-WP-Nonce': nonce,
                        'X-Senderzz-Financial-Nonce': financialNonce
                    },
                    credentials: 'same-origin',
                    body: JSON.stringify({ account_id: accountId })
                })
                .then(function(r){ return r.json(); })
                .then(function(resp) {
                    if (resp && resp.success) {
                        modal.classList.remove('szv2-open');
                        modal.style.display = 'none';
                        if (typeof szV2Toast === 'function') {
                            szV2Toast('Antecipação aprovada! ' + (resp.message || ''), 'success');
                        }
                        setTimeout(function(){ location.reload(); }, 1800);
                    } else {
                        confirmBtn.disabled = false;
                        confirmBtn.textContent = 'Confirmar antecipação';
                        if (msgEl) {
                            msgEl.textContent = (resp && resp.message) ? resp.message : 'Erro ao processar. Tente novamente.';
                            msgEl.style.display = 'block';
                        }
                    }
                })
                .catch(function() {
                    confirmBtn.disabled = false;
                    confirmBtn.textContent = 'Confirmar antecipação';
                    if (msgEl) {
                        msgEl.textContent = 'Falha de conexão. Tente novamente.';
                        msgEl.style.display = 'block';
                    }
                });
            });
        })
        .catch(function() {
            body.innerHTML = '<p style="font-size:13px;color:#dc2626">Erro ao carregar contas PIX. Recarregue a página.</p>';
        });
    };
    // Period pill toggle for wallet — activates selected, deactivates others in same group
    window.szV2WltPeriod = function(btn, group) {
        var container = btn.closest('div');
        if (container) container.querySelectorAll('.szv2-period-btn').forEach(function(b){ b.classList.remove('szv2-period-btn--active'); });
        btn.classList.add('szv2-period-btn--active');
    };
    </script>

    <?php if ( $sz8wl_can_withdraw ) : ?>
    <!-- F3: Modal antecipação COD V2 -->
    <div id="szv2-ant-modal" class="szv2-modal-overlay" role="dialog" aria-modal="true" aria-labelledby="szv2-ant-modal-title" style="display:none">
        <div class="szv2-modal" style="max-width:460px;width:96vw">
            <div class="szv2-modal-head">
                <h3 id="szv2-ant-modal-title">Antecipar recebíveis</h3>
                <button type="button" class="szv2-modal-x" aria-label="Fechar"
                        onclick="document.getElementById('szv2-ant-modal').classList.remove('szv2-open');document.getElementById('szv2-ant-modal').style.display='none'">&times;</button>
            </div>
            <div class="szv2-modal-body" id="szv2-ant-modal-body">
                <p style="font-size:13px;color:var(--szv2-text-muted)">Carregando contas...</p>
            </div>
        </div>
    </div>
    <?php endif; ?>

    <!-- Painel: Transações -->
    <div id="szv2-wlt-cod-transacoes">
        <div class="szv2-dash-row" style="align-items:flex-start">

            <!-- Histórico de movimentação -->
            <div class="szv2-card" style="flex:1">
                <div class="szv2-card-head">
                    <div>
                        <h2>Histórico de movimentação</h2>
                        <p class="szv2-card-sub">Recebimentos passados confirmados.</p>
                    </div>
                    <?php if ( function_exists( 'sz_cod_wallet_history_export_url' ) ) : ?>
                    <a href="<?php echo esc_url( sz_cod_wallet_history_export_url( $sz8wl_w_uid ) ); ?>" class="szv2-btn szv2-btn-secondary szv2-btn-sm">Exportar CSV</a>
                    <?php endif; ?>
                </div>
                <!-- Filtros de período compactos com intervalo personalizado -->
                <div style="display:flex;align-items:center;gap:4px;flex-wrap:wrap;margin-bottom:12px">
                    <?php foreach ( [ 'Hoje' => 'hoje', 'Ontem' => 'ontem', '7d' => '7d', '30d' => '30d', 'Mês' => 'mes' ] as $sz8wl_lbl => $sz8wl_slug ) : ?>
                    <button type="button"
                            class="szv2-period-btn <?php echo $sz8wl_slug === '7d' ? 'szv2-period-btn--active' : ''; ?>"
                            data-wlt-period="<?php echo esc_attr( $sz8wl_slug ); ?>"
                            onclick="szV2WltPeriod(this,'cod-hist')"><?php echo esc_html( $sz8wl_lbl ); ?></button>
                    <?php endforeach; ?>
                    <input type="date" id="szv2-wlt-hist-from" class="szv2-input szv2-input-sm"
                           value="<?php echo esc_attr( date( 'Y-m-d', strtotime( '-7 days' ) ) ); ?>"
                           style="font-size:12px;padding:3px 6px;width:128px;margin-left:4px"
                           onchange="document.querySelectorAll('.szv2-period-btn').forEach(function(b){b.classList.remove('szv2-period-btn--active')});">
                    <span style="font-size:12px;color:var(--szv2-text-muted)">–</span>
                    <input type="date" id="szv2-wlt-hist-to" class="szv2-input szv2-input-sm"
                           value="<?php echo esc_attr( current_time( 'Y-m-d' ) ); ?>"
                           style="font-size:12px;padding:3px 6px;width:128px"
                           onchange="document.querySelectorAll('.szv2-period-btn').forEach(function(b){b.classList.remove('szv2-period-btn--active')});">
                </div>
                <div class="szv2-table-wrap szv2-table-flush" id="szv2-wlt-cod-hist-wrap">
                    <?php if ( ! empty( $sz8wl_history ) ) : ?>
                    <table class="szv2-table">
                        <thead><tr><th>Data</th><th>Descrição</th><th>Pedido</th><th>Tipo</th><th class="szv2-td-num">Valor</th><th class="szv2-td-num">Taxa</th><th class="szv2-td-num">Líquido</th></tr></thead>
                        <tbody>
                            <?php foreach ( array_slice( $sz8wl_history, 0, 50 ) as $sz8wl_tx ) : ?>
                            <tr>
                                <td class="szv2-td-sub"><?php echo esc_html( (string) ( $sz8wl_tx['date'] ?? '' ) ); ?></td>
                                <td><?php echo esc_html( (string) ( $sz8wl_tx['description'] ?? '' ) ); ?></td>
                                <td class="szv2-num"><?php echo esc_html( (string) ( $sz8wl_tx['order'] ?? '—' ) ); ?></td>
                                <td><?php $sz8wl_mv = (string) ( $sz8wl_tx['movement'] ?? '' ); echo esc_html( $sz8wl_mv ); ?></td>
                                <td class="szv2-td-num szv2-num"><?php echo esc_html( $sz8wl_money( (float) ( $sz8wl_tx['value'] ?? $sz8wl_tx['net'] ?? 0 ) ) ); ?></td>
                                <td class="szv2-td-num szv2-num"><?php echo esc_html( $sz8wl_money( (float) ( $sz8wl_tx['fee'] ?? 0 ) ) ); ?></td>
                                <td class="szv2-td-num szv2-num"><?php echo esc_html( $sz8wl_money( (float) ( $sz8wl_tx['net'] ?? 0 ) ) ); ?></td>
                            </tr>
                            <?php endforeach; ?>
                        </tbody>
                    </table>
                    <?php else : ?>
                    <p class="szv2-td-sub" style="padding:16px">Nenhum registro encontrado.</p>
                    <?php endif; ?>
                </div>
            </div>

            <!-- Lançamentos futuros -->
            <div class="szv2-card" style="flex:1">
                <div class="szv2-card-head">
                    <div>
                        <h2>Lançamentos futuros</h2>
                        <p class="szv2-card-sub">Comissões a receber e datas de liberação.</p>
                    </div>
                    <?php if ( $sz8wl_future_total > 0 ) : ?>
                    <span class="szv2-num" style="font-size:13px;color:var(--szv2-brand)"><?php echo esc_html( $sz8wl_money( $sz8wl_future_total ) ); ?></span>
                    <?php endif; ?>
                </div>
                <!-- Filtros de período compactos com intervalo personalizado -->
                <div style="display:flex;align-items:center;gap:4px;flex-wrap:wrap;margin-bottom:12px">
                    <?php foreach ( [ 'Hoje' => 'hoje', 'Amanhã' => 'amanha', '7d' => '7d', '15d' => '15d', '30d' => '30d' ] as $sz8wl_lbl2 => $sz8wl_slug2 ) : ?>
                    <button type="button"
                            class="szv2-period-btn <?php echo $sz8wl_slug2 === 'hoje' ? 'szv2-period-btn--active' : ''; ?>"
                            data-wlt-period="<?php echo esc_attr( $sz8wl_slug2 ); ?>"
                            onclick="szV2WltPeriod(this,'cod-fut')"><?php echo esc_html( $sz8wl_lbl2 ); ?></button>
                    <?php endforeach; ?>
                    <input type="date" id="szv2-wlt-fut-from" class="szv2-input szv2-input-sm"
                           value="<?php echo esc_attr( current_time( 'Y-m-d' ) ); ?>"
                           style="font-size:12px;padding:3px 6px;width:128px;margin-left:4px"
                           onchange="document.querySelectorAll('[data-wlt-period]').forEach(function(b){b.classList.remove('szv2-period-btn--active')});">
                    <span style="font-size:12px;color:var(--szv2-text-muted)">–</span>
                    <input type="date" id="szv2-wlt-fut-to" class="szv2-input szv2-input-sm"
                           value="<?php echo esc_attr( date( 'Y-m-d', strtotime( '+30 days' ) ) ); ?>"
                           style="font-size:12px;padding:3px 6px;width:128px"
                           onchange="document.querySelectorAll('[data-wlt-period]').forEach(function(b){b.classList.remove('szv2-period-btn--active')});">
                </div>
                <div class="szv2-table-wrap szv2-table-flush" id="szv2-wlt-cod-fut-wrap">
                    <?php if ( ! empty( $sz8wl_future_rows ) ) : ?>
                    <table class="szv2-table">
                        <thead><tr><th>Data</th><th>Descrição</th><th>Pedido</th><th class="szv2-td-num">Comissão</th><th class="szv2-td-num">Liberação</th></tr></thead>
                        <tbody>
                            <?php foreach ( array_slice( $sz8wl_future_rows, 0, 30 ) as $sz8wl_ft ) : ?>
                            <tr>
                                <td class="szv2-td-sub"><?php echo esc_html( (string) ( $sz8wl_ft['date'] ?? '' ) ); ?></td>
                                <td><?php echo esc_html( (string) ( $sz8wl_ft['description'] ?? '' ) ); ?></td>
                                <td class="szv2-num"><?php echo esc_html( (string) ( $sz8wl_ft['order'] ?? '—' ) ); ?></td>
                                <td class="szv2-td-num szv2-num"><?php echo esc_html( $sz8wl_money( (float) ( $sz8wl_ft['commission'] ?? $sz8wl_ft['net'] ?? 0 ) ) ); ?></td>
                                <td class="szv2-td-num szv2-td-sub"><?php echo esc_html( (string) ( $sz8wl_ft['release_at'] ?? $sz8wl_ft['date'] ?? '—' ) ); ?></td>
                            </tr>
                            <?php endforeach; ?>
                        </tbody>
                    </table>
                    <?php else : ?>
                    <p class="szv2-td-sub" style="padding:16px">Nenhum lançamento futuro.</p>
                    <?php endif; ?>
                </div>
            </div>
        </div>
        <?php if ( $sz8wl_can_withdraw && $sz8wl_future_total > 0 ) : ?>
        <p class="szv2-card-sub" style="margin-top:8px">Clique em <strong>Antecipar</strong> para receber agora os valores pendentes, com taxa de <?php echo esc_html( number_format( $sz8wl_ant_fee_pct, 2, ',', '.' ) ); ?>%.</p>
        <?php elseif ( ! $sz8wl_can_withdraw ) : ?>
        <p class="szv2-card-sub" style="margin-top:8px">Para antecipações, entre em contato com o suporte Senderzz.</p>
        <?php endif; ?>
    </div>

    <!-- Painel: Saques -->
    <div id="szv2-wlt-cod-saques" style="display:none">
        <div class="szv2-card">
            <div class="szv2-card-head">
                <div><h2>Ordens de saque</h2><p class="szv2-card-sub">Acompanhe solicitações, taxas, comprovantes e conclusão.</p></div>
            </div>
            <?php if ( $sz8wl_show_wd_history && ! empty( $sz8wl_withdrawals ) ) : ?>
            <div class="szv2-table-wrap szv2-table-flush">
                <table class="szv2-table">
                    <thead>
                        <tr>
                            <th>Data</th><th>Valor</th><th>Taxa</th><th>Líquido</th><th>Conta</th><th>Status</th><th>Comprovante</th>
                        </tr>
                    </thead>
                    <tbody>
                        <?php foreach ( array_slice( $sz8wl_withdrawals, 0, 50 ) as $sz8wl_wd ) :
                            $sz8wl_wd_st   = (string) ( $sz8wl_wd['status'] ?? 'analysis' );
                            $sz8wl_wd_item = $sz8wl_wd_status_map[ $sz8wl_wd_st ] ?? [ 'neutral', ucfirst( $sz8wl_wd_st ) ];
                            $sz8wl_wd_holder = trim( (string) ( $sz8wl_wd['holder_name'] ?? '' ) );
                            $sz8wl_wd_pix    = trim( (string) ( $sz8wl_wd['pix_key'] ?? '' ) );
                            $sz8wl_wd_ptype  = strtoupper( (string) ( $sz8wl_wd['pix_type'] ?? '' ) );
                        ?>
                        <tr>
                            <td class="szv2-td-sub"><?php echo esc_html( date_i18n( 'd/m/Y H:i', strtotime( (string) ( $sz8wl_wd['created_at'] ?? '' ) ) ) ); ?></td>
                            <td class="szv2-num"><?php echo esc_html( $sz8wl_money( (float) ( $sz8wl_wd['amount'] ?? 0 ) ) ); ?></td>
                            <td class="szv2-num"><?php echo esc_html( $sz8wl_money( (float) ( $sz8wl_wd['fee'] ?? 0 ) ) ); ?></td>
                            <td class="szv2-num szv2-td-num"><?php echo esc_html( $sz8wl_money( (float) ( $sz8wl_wd['net'] ?? 0 ) ) ); ?></td>
                            <td class="szv2-td-sub">
                                <?php if ( $sz8wl_wd_holder ) : ?>
                                <?php echo esc_html( $sz8wl_wd_holder ); ?>
                                <?php if ( $sz8wl_wd_pix ) : ?><br><span style="font-size:11px;color:var(--szv2-text-faint)">PIX <?php echo esc_html( $sz8wl_wd_ptype ); ?>: <?php echo esc_html( $sz8wl_wd_pix ); ?></span><?php endif; ?>
                                <?php else : ?><span class="szv2-td-sub">—</span><?php endif; ?>
                            </td>
                            <td><span class="sz-badge szv2-badge-<?php echo esc_attr( $sz8wl_wd_item[0] ); ?>"><?php echo esc_html( $sz8wl_wd_item[1] ); ?></span></td>
                            <td><?php if ( ! empty( $sz8wl_wd['proof_url'] ) ) : ?><a href="<?php echo esc_url( $sz8wl_wd['proof_url'] ); ?>" target="_blank" rel="noopener" class="szv2-link-btn">Ver</a><?php else : ?><span class="szv2-td-sub">—</span><?php endif; ?></td>
                        </tr>
                        <?php endforeach; ?>
                    </tbody>
                </table>
            </div>
            <?php else : ?>
                <?php echo sz_v2_empty_state( [ 'title' => 'Nenhum saque solicitado', 'text' => 'Saques realizados aparecem aqui.' ] ); // phpcs:ignore ?>
            <?php endif; ?>
        </div>
    </div>

    <!-- Modal de saque COD V2 -->
    <?php if ( $sz8wl_can_withdraw ) : ?>
    <div class="szv2-modal-overlay" id="szv2-wd-modal" data-szv2-modal="wd-cod"
         role="dialog" aria-modal="true" aria-label="Solicitar saque COD">
        <div class="szv2-modal">
            <div class="szv2-modal-head">
                <h3>Solicitar saque COD</h3>
                <button type="button" class="szv2-modal-x" data-szv2-modal-close aria-label="Fechar">&times;</button>
            </div>
            <div class="szv2-modal-body" id="szv2-wd-step-form">
                <div class="szv2-wd-summary-row">
                    <span class="szv2-label">Saldo disponível</span>
                    <strong><?php echo esc_html( $sz8wl_money( $sz8wl_available ) ); ?></strong>
                </div>
                <?php if ( $sz8wl_fee_flat > 0 ) : ?>
                <div class="szv2-wd-summary-row">
                    <span class="szv2-label">Taxa de saque</span>
                    <span><?php echo esc_html( $sz8wl_money( $sz8wl_fee_flat ) ); ?></span>
                </div>
                <?php endif; ?>
                <div class="szv2-field">
                    <label class="szv2-label" for="szv2-wd-amount">Valor a sacar</label>
                    <input type="text" id="szv2-wd-amount" class="szv2-input"
                           inputmode="numeric" autocomplete="off" placeholder="0,00"
                           data-max="<?php echo esc_attr( number_format( $sz8wl_available, 2, '.', '' ) ); ?>">
                    <span class="szv2-field-hint" id="szv2-wd-amount-hint"></span>
                </div>
                <div class="szv2-field">
                    <label class="szv2-label" for="szv2-wd-account-select">Conta para recebimento</label>
                    <select id="szv2-wd-account-select" class="szv2-input">
                        <option value="">Carregando contas...</option>
                    </select>
                    <span class="szv2-field-hint" id="szv2-wd-no-acct" hidden>
                        Nenhuma conta PIX cadastrada. Adicione em <strong>Configurações &rarr; Saques</strong>.
                    </span>
                </div>
            </div>
            <div class="szv2-modal-body" id="szv2-wd-step-confirm" hidden>
                <p class="szv2-wd-confirm-label">Confirme os dados antes de solicitar:</p>
                <div class="szv2-wd-confirm-block">
                    <div class="szv2-wd-confirm-row"><span>Valor solicitado</span><strong id="szv2-wd-cf-amount">—</strong></div>
                    <div class="szv2-wd-confirm-row"><span>Taxa</span><span id="szv2-wd-cf-fee">—</span></div>
                    <div class="szv2-wd-confirm-row szv2-wd-confirm-row-total"><span>Valor a receber</span><strong id="szv2-wd-cf-net">—</strong></div>
                    <div class="szv2-wd-confirm-row"><span>Conta PIX</span><span id="szv2-wd-cf-account">—</span></div>
                </div>
                <p class="szv2-card-sub">O saque ficará em análise. Processamento em até 1 dia útil.</p>
            </div>
            <div class="szv2-modal-foot">
                <button type="button" class="szv2-btn szv2-btn-secondary" id="szv2-wd-back-btn" hidden>Voltar</button>
                <button type="button" class="szv2-btn szv2-btn-secondary" id="szv2-wd-cancel-btn" data-szv2-modal-close>Cancelar</button>
                <button type="button" class="szv2-btn szv2-btn-brand" id="szv2-wd-next-btn">Continuar</button>
                <button type="button" class="szv2-btn szv2-btn-brand" id="szv2-wd-submit-btn" hidden>Confirmar saque</button>
            </div>
        </div>
    </div>
    <?php endif; ?>
</div><!-- /#szv2-wlt-cod-body -->

<?php if ( $sz8wl_has_exp ) : ?>
<div id="szv2-wallet-exp-panel" style="display:none">
    <?php require __DIR__ . '/wallet-expedition.php'; ?>
</div>
<?php endif; ?>
</section>
