<?php
/**
 * Senderzz — Módulo Motoboy
 * Admin: menu, CDs, zonas, motoboys, configurações
 */
if ( ! defined( 'ABSPATH' ) ) exit;

// Senderzz v221: submenu legado removido da origem; aba Motoboy vive em Admin > Senderzz.
// add_action( 'admin_menu', function() {
//     add_submenu_page(
//         'woocommerce',
//         'Senderzz Motoboy',
//         '🏍️ Motoboy',
//         'manage_options',
//         'sz-motoboy',
//         'sz_mb_admin_page'
//     );
// }, 30 );

add_action( 'admin_head', function() {
    $sz_page = sanitize_key( $_GET['page'] ?? '' );
    $sz_tab  = sanitize_key( $_GET['tab'] ?? '' );
    $sz_area = sanitize_key( $_GET['area'] ?? '' );
    if ( $sz_page !== 'sz-motoboy' && ! ( $sz_page === 'senderzz' && ( $sz_tab === 'motoboy' || ( $sz_area === 'cod' && in_array( $sz_tab, [ 'cod-pedidos', 'cod-motoboys', 'cod-relatorios' ], true ) ) ) ) ) return;
    ?>
    <style>
    :root{--sz-admin-orange:#E8650A;--sz-admin-orange-dark:#c8540a;--sz-admin-bg:#f6f7f9;--sz-admin-card:#fff;--sz-admin-border:#e5e7eb;--sz-admin-text:#111827;--sz-admin-soft:#6b7280;--sz-admin-muted:#9ca3af}
    body.toplevel_page_sz-motoboy,body.woocommerce_page_sz-motoboy{background:var(--sz-admin-bg)}
    .sz-mb-wrap{max-width:1220px;margin-right:24px;color:var(--sz-admin-text)}
    .sz-mb-wrap h1{font-size:var(--sz-text-3xl);font-weight:700;margin:18px 0 18px;color:var(--sz-admin-text);letter-spacing:-.015em}
    .sz-mb-wrap h2{font-size:var(--sz-text-lg);font-weight:700;margin:0 0 14px;color:var(--sz-admin-text);letter-spacing:-.015em}
    .sz-mb-wrap h3{font-size:var(--sz-text-md);font-weight:700;color:var(--sz-admin-text)}
    .sz-mb-tabs{display:flex;gap:8px;margin:0 0 18px;padding:8px;background:#fff;border:1px solid var(--sz-admin-border);border-radius:16px;box-shadow:0 1px 2px rgba(15,23,42,.04);overflow:auto}
    .sz-mb-tab{padding:10px 14px;cursor:pointer;border-radius:12px;font-weight:700;font-size:var(--sz-text-base);color:var(--sz-admin-soft);text-decoration:none;border:1px solid transparent;background:transparent;white-space:nowrap;transition:.15s ease}
    .sz-mb-tab:hover{color:var(--sz-admin-orange);background:#fff7ed}
    .sz-mb-tab.active{background:var(--sz-admin-orange);color:#fff;border-color:var(--sz-admin-orange);box-shadow:0 8px 20px rgba(232,101,10,.18)}
    .sz-mb-card{background:var(--sz-admin-card);border:1px solid var(--sz-admin-border);border-radius:18px;padding:20px;margin-bottom:16px;box-shadow:0 1px 3px rgba(15,23,42,.04)}
    .sz-mb-grid-3{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:16px}
    .sz-mb-grid-2{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:16px}
    .sz-mb-stat{background:linear-gradient(180deg,#fff,#fafafa);border:1px solid var(--sz-admin-border);border-radius:16px;padding:18px;text-align:left;box-shadow:0 1px 2px rgba(15,23,42,.04)}
    .sz-mb-stat-num{font-size:var(--sz-text-3xl);font-weight:700;color:var(--sz-admin-orange);line-height:1}
    .sz-mb-stat-label{font-size:var(--sz-text-meta);color:var(--sz-admin-soft);margin-top:8px;font-weight:700}
    .sz-mb-table{width:100%;border-collapse:separate;border-spacing:0;border:1px solid var(--sz-admin-border);border-radius:14px;overflow:hidden;background:#fff}
    .sz-mb-table th,.sz-mb-table td{border-bottom:1px solid #f1f5f9;padding:12px 14px;text-align:left;font-size:var(--sz-text-base);vertical-align:middle}
    .sz-mb-table th{background:#f9fafb;font-weight:700;color:#374151;font-size:var(--sz-text-meta);text-transform:none;letter-spacing:.02em}
    .sz-mb-table tr:last-child td{border-bottom:none}
    .sz-mb-table tbody tr:hover{background:#fffaf5}
    .sz-mb-badge{display:inline-flex;align-items:center;gap:5px;padding:4px 10px;border-radius:999px;font-size:var(--sz-text-sm);font-weight:700;border:1px solid transparent}
    .sz-mb-badge.aprovado{background:#dcfce7;color:#166534}.sz-mb-badge.agendado{background:#fef3c7;color:#92400e}.sz-mb-badge.embalado{background:#ede9fe;color:#5b21b6}.sz-mb-badge.em_rota{background:#dbeafe;color:#1e40af}.sz-mb-badge.a_caminho{background:#e0f2fe;color:#0369a1}.sz-mb-badge.entregue{background:#d1fae5;color:#065f46}.sz-mb-badge.frustrado,.sz-mb-badge.avariado{background:#fee2e2;color:#991b1b}.sz-mb-badge.reagendado{background:#fef9c3;color:#854d0e}
    .sz-mb-form label{display:block;font-weight:700;font-size:var(--sz-text-meta);margin-bottom:6px;margin-top:12px;color:#374151}
    .sz-mb-form input,.sz-mb-form select,.sz-mb-form textarea{width:100%;min-height:38px;padding:8px 11px;border:1px solid #d1d5db;border-radius:11px;font-size:var(--sz-text-base);background:#fff;color:#111827;box-shadow:none;box-sizing:border-box}
    .sz-mb-form input:focus,.sz-mb-form select:focus,.sz-mb-form textarea:focus{border-color:var(--sz-admin-orange);box-shadow:0 0 0 3px rgba(232,101,10,.13);outline:none}
    .sz-mb-weekdays{display:flex;gap:8px;flex-wrap:wrap;margin-top:6px}
    .sz-mb-weekdays label{display:inline-flex!important;align-items:center;gap:6px;margin:0!important;padding:7px 10px;border:1px solid #e5e7eb;border-radius:999px;background:#f9fafb;font-size:var(--sz-text-meta);color:#374151;font-weight:700}
    .sz-mb-weekdays input{width:auto!important;min-height:auto;margin:0;accent-color:var(--sz-admin-orange)}
    .sz-mb-cutoff-grid{display:grid;grid-template-columns:repeat(7,minmax(90px,1fr));gap:8px;margin-top:8px}.sz-mb-cutoff-grid label{margin:0!important;font-size:var(--sz-text-sm);color:#64748b;font-weight:700}.sz-mb-cutoff-grid input{margin-top:4px!important;min-height:34px!important;padding:6px 8px!important;border-radius:10px!important}.sz-mb-cutoff-grid-edit{grid-column:1/-1;grid-template-columns:repeat(7,minmax(82px,1fr));}.sz-mb-cutoff-single-edit input{margin-top:4px!important;min-height:36px!important;padding:6px 8px!important;border-radius:10px!important}
    .sz-mb-btn,.sz-mb-card button.button-primary{background:var(--sz-admin-orange)!important;color:#fff!important;border:none!important;padding:9px 16px;border-radius:11px;font-weight:700;cursor:pointer;font-size:var(--sz-text-base);text-decoration:none;display:inline-flex;align-items:center;justify-content:center;gap:6px;line-height:1.2;box-shadow:none!important}
    .sz-mb-btn:hover,.sz-mb-card button.button-primary:hover{background:var(--sz-admin-orange-dark)!important;color:#fff!important}
    .sz-mb-btn-sm{padding:7px 11px;font-size:var(--sz-text-meta);border-radius:10px}
    .sz-mb-danger{color:#b91c1c;text-decoration:none;font-weight:700;border:1px solid #fecaca;background:#fff1f2;padding:7px 11px;border-radius:10px;display:inline-flex;align-items:center;gap:5px}
    .sz-mb-danger:hover{background:#fee2e2;color:#991b1b}
    .sz-mb-muted{color:var(--sz-admin-muted);font-size:var(--sz-text-meta)}.sz-mb-soft{color:var(--sz-admin-soft);font-size:var(--sz-text-meta)}
    .sz-mb-alert-ok,.sz-mb-alert-err{padding:12px 16px;border-radius:14px;margin-bottom:16px;font-weight:700;border:1px solid}
    .sz-mb-alert-ok{background:#ecfdf5;border-color:#bbf7d0;color:#166534}.sz-mb-alert-err{background:#fff1f2;border-color:#fecaca;color:#991b1b}
    .sz-mb-actions{display:flex;gap:8px;flex-wrap:wrap;align-items:center}
    .sz-mb-inline-form{display:inline;margin:0}
    .sz-mb-form-row{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:14px;align-items:end;margin-bottom:18px}
    .sz-mb-filterbar{display:grid;grid-template-columns:2fr 1fr 1fr;gap:10px;margin:14px 0 16px}.sz-mb-filterbar input,.sz-mb-filterbar select{min-height:38px;border:1px solid #d1d5db;border-radius:11px;padding:8px 11px;box-shadow:none}
    .sz-mb-op-badge{display:inline-flex;align-items:center;gap:6px;padding:6px 10px;background:#f3f4f6;color:#111827;border:1px solid #e5e7eb;border-radius:999px;font-size:var(--sz-text-meta);font-weight:700;white-space:nowrap}
    .sz-mb-cep-edit{display:none;grid-template-columns:130px 130px minmax(260px,1fr) 160px auto;gap:8px;align-items:end}.sz-mb-cep-edit input{width:100%;min-height:36px}.sz-mb-cep-edit label{font-size:var(--sz-text-sm);margin-bottom:3px}.sz-mb-weekdays-edit{margin-top:0}.sz-mb-cep-row.is-editing .sz-mb-cep-view{display:none}.sz-mb-cep-row.is-editing .sz-mb-cep-edit{display:grid}
    .sz-mb-preview{display:grid;grid-template-columns:1fr auto;gap:10px;align-items:end;margin-top:10px}.sz-mb-preview-result{margin-top:12px;padding:13px 14px;border-radius:14px;background:#f9fafb;border:1px solid #e5e7eb;font-weight:700;color:#374151}
    .sz-mb-empty{padding:20px;border:1px dashed #d1d5db;border-radius:14px;background:#fafafa;color:var(--sz-admin-soft);font-weight:700;text-align:center}
    .sz-mb-card .button,.sz-mb-card .button-secondary{border-radius:11px!important;border-color:#d1d5db!important;box-shadow:none!important;font-weight:700!important}
    .sz-mb-card .button:hover,.sz-mb-card .button-secondary:hover{border-color:var(--sz-admin-orange)!important;color:var(--sz-admin-orange)!important}
    .sz-mb-grid-4{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:14px}
    .sz-mb-queue{display:grid;gap:12px}
    .sz-mb-order-card{border:1px solid var(--sz-admin-border);border-radius:18px;background:#fff;padding:14px 16px;display:grid;grid-template-columns:minmax(190px,1.1fr) minmax(210px,1.25fr) minmax(150px,.75fr) minmax(150px,.75fr) minmax(320px,1.65fr);gap:14px;align-items:center;box-shadow:0 1px 2px rgba(15,23,42,.04)}
    .sz-mb-order-card:hover{border-color:#fed7aa;background:#fffaf5}
    .sz-mb-order-main{display:flex;align-items:center;gap:10px;min-width:0}.sz-mb-order-id{font-size:var(--sz-text-lg);font-weight:700;color:var(--sz-admin-orange);text-decoration:none;white-space:nowrap}.sz-mb-order-title{font-size:var(--sz-text-base);font-weight:700;color:var(--sz-admin-text);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.sz-mb-order-sub{font-size:var(--sz-text-meta);font-weight:700;color:var(--sz-admin-soft);margin-top:2px}.sz-mb-order-meta{display:grid;gap:4px;min-width:0}.sz-mb-order-label{font-size:var(--sz-text-xs);font-weight:700;letter-spacing:.02em;text-transform:none;color:var(--sz-admin-muted)}.sz-mb-order-value{font-size:var(--sz-text-base);font-weight:700;color:var(--sz-admin-text);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.sz-mb-order-price{font-size:var(--sz-text-lg);font-weight:700;color:var(--sz-admin-text)}.sz-mb-order-actions{display:flex;gap:7px;align-items:center;justify-content:flex-end;flex-wrap:wrap}.sz-mb-order-actions form{margin:0}.sz-mb-order-actions select{height:34px;border:1px solid #d1d5db;border-radius:10px;padding:0 8px;max-width:145px;font-size:var(--sz-text-meta);background:#fff}
    .sz-mb-wrap .notice,.sz-mb-wrap .updated,.sz-mb-wrap .error{border-radius:12px;box-shadow:none;border-left-color:var(--sz-admin-orange)}
    .sz-mb-modal-backdrop{display:none;position:fixed;inset:0;background:rgba(15,23,42,.45);z-index:99999;align-items:center;justify-content:center;padding:18px}
    .sz-mb-modal{background:#fff;border-radius:18px;box-shadow:0 24px 60px rgba(15,23,42,.28);width:min(520px,100%);padding:22px}.sz-mb-modal h3{margin:0 0 10px;font-size:var(--sz-text-lg)}.sz-mb-modal p{margin:0 0 14px;color:#4b5563}.sz-mb-modal img{max-width:100%;border-radius:12px;border:1px solid #e5e7eb;margin-top:12px}

    .sz-mb-section-title{display:flex;align-items:flex-end;justify-content:space-between;gap:12px;margin:18px 0 10px}
    .sz-mb-section-title h2{margin:0 0 4px;font-size:var(--sz-text-lg);font-weight:700;color:var(--sz-admin-text)}
    .sz-mb-section-title p{margin:0}
    .sz-mb-custody-kpis{display:grid;grid-template-columns:repeat(5,minmax(0,1fr));gap:12px;margin:0 0 18px}
    .sz-mb-kpi-card{background:#fff;border:1px solid var(--sz-admin-border);border-radius:16px;padding:14px 16px;min-height:92px;display:flex;flex-direction:column;justify-content:space-between;box-shadow:0 1px 2px rgba(15,23,42,.04)}
    .sz-mb-kpi-card span{font-size:var(--sz-text-xs);font-weight:700;letter-spacing:.02em;text-transform:none;color:#64748b}
    .sz-mb-kpi-card strong{display:block;font-size:var(--sz-text-2xl);font-weight:700;line-height:1;color:#111827;margin:7px 0}
    .sz-mb-kpi-card small{font-size:var(--sz-text-sm);font-weight:700;color:#64748b}
    .sz-mb-accordion{background:#fff;border:1px solid var(--sz-admin-border);border-radius:16px;margin:0 0 12px;box-shadow:0 1px 2px rgba(15,23,42,.04);overflow:hidden}
    .sz-mb-accordion summary{list-style:none;cursor:pointer;display:flex;align-items:center;justify-content:space-between;gap:12px;padding:14px 16px;background:#fff}
    .sz-mb-accordion summary::-webkit-details-marker{display:none}
    .sz-mb-accordion summary span{display:grid;gap:2px}
    .sz-mb-accordion summary strong{font-size:var(--sz-text-base);font-weight:700;color:#111827}
    .sz-mb-accordion summary small{font-size:var(--sz-text-sm);font-weight:700;color:#64748b}
    .sz-mb-accordion summary em{font-style:normal;font-size:var(--sz-text-xs);font-weight:700;text-transform:none;letter-spacing:.02em;color:#E8650A;background:#fff7ed;border:1px solid #fed7aa;border-radius:999px;padding:5px 9px}
    .sz-mb-accordion[open] summary{border-bottom:1px solid #f1f5f9;background:#fffaf5}
    .sz-mb-accordion[open] summary em{color:#64748b;background:#f8fafc;border-color:#e2e8f0}
    .sz-mb-accordion[open] summary em::before{content:"fechar";font-size:var(--sz-text-xs)}
    .sz-mb-accordion[open] summary em{font-size:0}
    .sz-mb-accordion-body{padding:14px 16px 16px}
    .sz-mb-custody-form{display:grid;gap:10px;align-items:end}
    .sz-mb-custody-form-route{grid-template-columns:minmax(260px,2fr) minmax(220px,1fr) auto}
    .sz-mb-custody-form-return{grid-template-columns:minmax(220px,1.5fr) minmax(150px,.7fr) minmax(260px,1.4fr) minmax(180px,1fr) auto}
    .sz-mb-custody-form label{margin-top:0}
    .sz-mb-table-scroll{width:100%;overflow:auto}

    @media(max-width:1180px){.sz-mb-order-card{grid-template-columns:1fr 1fr}.sz-mb-order-actions{justify-content:flex-start}}
    @media(max-width:960px){.sz-mb-grid-2,.sz-mb-grid-3,.sz-mb-grid-4,.sz-mb-form-row{grid-template-columns:1fr}.sz-mb-wrap{margin-right:10px}.sz-mb-table{display:block;overflow-x:auto}.sz-mb-tabs{border-radius:14px}.sz-mb-order-card{grid-template-columns:1fr}.sz-mb-order-actions{justify-content:flex-start}}

    @media(max-width:1180px){.sz-mb-custody-kpis{grid-template-columns:repeat(3,minmax(0,1fr))}.sz-mb-custody-form-route,.sz-mb-custody-form-return{grid-template-columns:1fr 1fr}}
    @media(max-width:720px){.sz-mb-custody-kpis{grid-template-columns:1fr 1fr}.sz-mb-custody-form-route,.sz-mb-custody-form-return{grid-template-columns:1fr}.sz-mb-section-title{align-items:flex-start}.sz-mb-accordion summary{align-items:flex-start}.sz-mb-kpi-card{min-height:84px}}

    </style>
    <?php
} );

function sz_mb_admin_page(): void {
    // When rendered inside unified Senderzz menu, the main tab is `motoboy`;
    // the Motoboy internal tab must use `mb_tab`. Old direct URLs are still supported.
    $tab = sanitize_key( $_GET['mb_tab'] ?? $_GET['tab'] ?? 'dashboard' );
    if ( $tab === 'motoboy' ) { $tab = 'dashboard'; }
    $msg = sanitize_text_field( $_GET['msg'] ?? '' );

    // v311: ação manual para reconstruir carteira/fechamento dos motoboys.
    if ( isset( $_POST['sz_mb_sync_wallets'] ) ) {
        check_admin_referer( 'sz_mb_sync_wallets' );
        if ( function_exists( 'sz_mbw_sync_all_data' ) ) {
            $sync = sz_mbw_sync_all_data();
            $msg = 'wallets_synced';
        } else {
            $msg = 'error';
        }
    }
    ?>
    <div class="wrap sz-mb-wrap">
        <h1>🏍️ Motoboy</h1>

        <?php if ( $msg === 'saved' ) : ?>
            <div class="sz-mb-alert-ok">✅ Salvo com sucesso.</div>
        <?php elseif ( $msg === 'deleted' ) : ?>
            <div class="sz-mb-alert-ok">🗑️ Removido com sucesso.</div>
        <?php elseif ( $msg === 'wallets_synced' ) : ?>
            <div class="sz-mb-alert-ok">✅ Carteiras e fechamentos atualizados. Frustrados ficam em custódia do motoboy até devolução por QR no CD; entregas dependem de conciliação.</div>
        <?php elseif ( $msg === 'error' ) : ?>
            <div class="sz-mb-alert-err">❌ Erro ao processar. Verifique os dados.</div>
        <?php elseif ( $msg === 'cep_invalid' ) : ?>
            <div class="sz-mb-alert-err">❌ Informe CEP inicial e final válidos. Zona sem CEP não é mais criada.</div>
        <?php elseif ( $msg === 'cep_order' ) : ?>
            <div class="sz-mb-alert-err">❌ CEP inicial não pode ser maior que o CEP final.</div>
        <?php elseif ( $msg === 'cep_overlap' ) : ?>
            <div class="sz-mb-alert-err">❌ Essa faixa de CEP já cruza com outra zona cadastrada.</div>
        <?php elseif ( $msg === 'ghost_prevented' ) : ?>
            <div class="sz-mb-alert-ok">✅ Ação bloqueada para evitar zona fantasma. Crie a zona já com CEP.</div>
        <?php elseif ( $msg === 'route_qr_only' ) : ?>
            <div class="sz-mb-alert-err">❌ Em rota só pode ser iniciado pelo motoboy lendo o QR Code da etiqueta.</div>
        <?php elseif ( $msg === 'custody_returned' ) : ?>
            <div class="sz-mb-alert-ok">✅ Produto recebido, conferido como vendável e devolvido ao estoque.</div>
        <?php elseif ( $msg === 'custody_damaged' ) : ?>
            <div class="sz-mb-alert-ok">✅ Avariado/perda operacional registrada com evidência e relato.</div>
        <?php elseif ( $msg === 'route_assisted' ) : ?>
            <div class="sz-mb-alert-ok">✅ Rota assistida iniciada por QR pelo OL.</div>
        <?php elseif ( $msg === 'photo_required' ) : ?>
            <div class="sz-mb-alert-err">❌ Para marcar Avariado/Extravio/Perda é obrigatório anexar foto/evidência e relato.</div>
        <?php endif; ?>

        <div class="sz-mb-tabs">
            <?php
            $tabs = [
            'dashboard'   => 'Dashboard',
            'pedidos'     => 'Pedidos',
            'etiquetas'   => 'Etiquetas',
            'estoque'     => 'Estoque Motoboy',
            'mapa'        => 'Mapa ao vivo',
            'conciliacao' => 'Conciliação',
            'carteiras'   => 'Carteiras',
            'saques'      => 'Saques',
            'relatorios'  => 'Relatórios',
            'motoboys'    => 'Motoboys',
            'zonas'       => 'Zonas / CEPs',
            'config'      => 'Configurações',
        ];
            foreach ( $tabs as $k => $label ) :
                $active = $tab === $k ? 'active' : '';
                if ( ( $_GET['page'] ?? '' ) === 'senderzz' ) {
                    // Navegação interna preservada dentro do admin novo.
                    // Pedidos precisa abrir a tela operacional completa antiga, com Embalar,
                    // Enviar para rota, definir/trocar entregador, imprimir e mudança de status.
                    $outer_tab = 'cod-pedidos';
                    $url = add_query_arg( [ 'page' => 'senderzz', 'area' => 'cod', 'tab' => $outer_tab, 'mb_tab' => $k ], admin_url('admin.php') );
                } else {
                    $outer_tab = 'cod-pedidos';
                    $url = add_query_arg( [ 'page' => 'senderzz', 'area' => 'cod', 'tab' => $outer_tab, 'mb_tab' => $k ], admin_url('admin.php') );
                }
            ?>
                <a href="<?php echo esc_url($url); ?>" class="sz-mb-tab <?php echo $active; ?>"><?php echo esc_html($label); ?></a>
            <?php endforeach; ?>
        </div>

        <?php
        match( $tab ) {
            'dashboard'   => sz_mb_tab_dashboard(),
            'pedidos'     => sz_mb_tab_pedidos(),
            'etiquetas'   => sz_mb_tab_etiquetas(),
            'estoque'     => function_exists('sz_mb_tab_estoque_motoboy') ? sz_mb_tab_estoque_motoboy() : sz_mb_tab_dashboard(),
            'mapa'        => sz_mb_tab_mapa(),
            'conciliacao' => sz_mb_tab_conciliacao(),
            'carteiras'   => sz_mb_tab_carteiras(),
            'saques'      => sz_mb_tab_saques(),
            'relatorios'  => function_exists('sz_mb_relatorios_page') ? sz_mb_relatorios_page() : sz_mb_tab_dashboard(),
            'motoboys'    => sz_mb_tab_motoboys(),
            'zonas'       => sz_mb_tab_zonas(),
            'config'      => sz_mb_tab_config(),
            default       => sz_mb_tab_dashboard(),
        };
        ?>
    </div>
    <?php
}

// ── Dashboard ─────────────────────────────────────────────────────────────────
function sz_mb_tab_dashboard(): void {
    global $wpdb;
    $p    = $wpdb->prefix;
    $hoje = ( new DateTimeImmutable( 'now', new DateTimeZone( 'America/Sao_Paulo' ) ) )->format( 'Y-m-d' );

    $stats = $wpdb->get_results( $wpdb->prepare(
        "SELECT status, COUNT(*) AS total FROM {$p}sz_motoboy_pedidos WHERE DATE(created_at) = %s GROUP BY status",
        $hoje
    ) );
    $map = [];
    foreach ( $stats as $s ) $map[$s->status] = (int) $s->total;

    $motoboys = $wpdb->get_results( $wpdb->prepare(
        "SELECT m.nome, z.nome AS zona, cd.nome AS cd,
                COUNT(mp.id) AS pedidos,
                SUM(CASE WHEN mp.status='entregue' THEN 1 ELSE 0 END) AS entregues,
                SUM(CASE WHEN mp.status='frustrado' THEN 1 ELSE 0 END) AS frustrados,
                ROUND(SUM(CASE WHEN mp.status='entregue' THEN 1 ELSE 0 END)/NULLIF(COUNT(mp.id),0)*100,1) AS taxa_sucesso
           FROM {$p}sz_motoboys m
           LEFT JOIN {$p}sz_motoboy_zonas z ON z.id=m.zona_id
           LEFT JOIN {$p}sz_motoboy_cds cd ON cd.id=m.cd_id
           LEFT JOIN {$p}sz_motoboy_pedidos mp ON mp.motoboy_id=m.id AND DATE(mp.created_at) = %s
          WHERE m.ativo=1 GROUP BY m.id ORDER BY entregues DESC",
        $hoje, $hoje
    ) );

    $pendentes = (int) $wpdb->get_var( $wpdb->prepare(
        "SELECT COUNT(*) FROM {$p}sz_motoboy_fechamento
          WHERE data_fechamento < %s
            AND alan_confirmou = 0
            AND total_entregues > 0
            AND total_a_repassar > 0",
        $hoje
    ) );
    ?>
    <div class="sz-mb-grid-3" style="margin-bottom:20px">
        <?php
        $cards = [
            'agendado'  => ['Agendados', '🟠'],
            'embalado'  => ['Embalados', '🟣'],
            'em_rota'   => ['Em Rota', '🔵'],
            'entregue'  => ['Entregues', '✅'],
            'frustrado' => ['Frustrados', '❌'],
        ];
        foreach ( $cards as $k => [$label, $icon] ) : ?>
            <div class="sz-mb-stat">
                <div class="sz-mb-stat-num"><?php echo $icon . ' ' . ( $map[$k] ?? 0 ); ?></div>
                <div class="sz-mb-stat-label"><?php echo $label; ?> hoje</div>
            </div>
        <?php endforeach; ?>
    </div>

    <?php if ( $pendentes ) : ?>
        <div class="sz-mb-alert-err">⚠️ <?php echo $pendentes; ?> fechamento(s) de caixa pendente(s) de confirmação.</div>
    <?php endif; ?>

    <div class="sz-mb-card">
        <h2>Ranking de Motoboys — Hoje</h2>
        <table class="sz-mb-table">
            <thead><tr><th>Motoboy</th><th>CD / Zona</th><th>Pedidos</th><th>Entregues</th><th>Frustrados</th><th>Taxa Sucesso</th></tr></thead>
            <tbody>
            <?php foreach ( $motoboys as $mb ) : ?>
                <tr>
                    <td><strong><?php echo esc_html($mb->nome); ?></strong></td>
                    <td><?php echo esc_html($mb->cd . ' / ' . $mb->zona); ?></td>
                    <td><?php echo (int)$mb->pedidos; ?></td>
                    <td style="color:#16a34a;font-weight:700"><?php echo (int)$mb->entregues; ?></td>
                    <td style="color:#dc2626;font-weight:700"><?php echo (int)$mb->frustrados; ?></td>
                    <td><strong><?php echo $mb->taxa_sucesso ?? 0; ?>%</strong></td>
                </tr>
            <?php endforeach; ?>
            </tbody>
        </table>
    </div>
    <?php
}


// Status exibido no admin Motoboy: o WooCommerce é a fonte principal.
// A tabela operacional é apenas apoio para motoboy/rota, nunca fonte concorrente.
function sz_mb_admin_status_from_wc( $wc_order_id, string $fallback = 'agendado' ): string {
    if ( function_exists( 'wc_get_order' ) ) {
        $order = wc_get_order( (int) $wc_order_id );
        if ( $order instanceof \WC_Order ) {
            $st = str_replace( '-', '_', sanitize_key( (string) $order->get_status() ) );
            $map = [
                'completo'    => 'entregue',
                'completed'   => 'entregue',
                'entregue'    => 'entregue',
                'frustrado'   => 'frustrado',
                'frustracao'  => 'frustrado',
                'cancelled'   => 'cancelado',
                'canceled'    => 'cancelado',
                'cancelado'   => 'cancelado',
                'failed'      => 'cancelado',
                'embalado'    => 'embalado',
                'em_rota'     => 'em_rota',
                'a_caminho'   => 'em_rota',
                'agendado'    => 'agendado',
                'aprovado'    => 'agendado',
                'processing'  => 'agendado',
                'on_hold'     => 'agendado',
            ];
            if ( isset( $map[ $st ] ) ) return $map[ $st ];
            if ( $st !== '' ) return $st;
        }
    }
    $fallback = str_replace( '-', '_', sanitize_key( $fallback ) );
    if ( $fallback === 'a_caminho' ) return 'em_rota';
    return $fallback ?: 'agendado';
}

function sz_mb_admin_order_meta_first( \WC_Order $order, array $keys, $default = '' ) {
    foreach ( $keys as $key ) {
        $v = $order->get_meta( $key, true );
        if ( $v !== '' && $v !== null ) return $v;
    }
    return $default;
}

function sz_mb_admin_motoboy_meta_keys_id(): array {
    // Fonte oficial de atribuição: pedido WooCommerce. Campos de baixa ficam por último,
    // pois são histórico da entrega e não podem sobrescrever uma troca administrativa.
    return [
        '_senderzz_motoboy_id',
        '_sz_motoboy_id',
        '_motoboy_user_id',
        '_senderzz_motoboy_responsavel_id',
        '_senderzz_motoboy_entregador_id',
        '_senderzz_motoboy_assigned_id',
        '_senderzz_motoboy_atribuido_id',
        '_senderzz_motoboy_baixa_motoboy_id',
    ];
}

function sz_mb_admin_set_order_motoboy_meta( int $wc_order_id, int $motoboy_id ): void {
    global $wpdb;
    if ( ! function_exists( 'wc_get_order' ) || $wc_order_id <= 0 || $motoboy_id <= 0 ) return;
    $order = wc_get_order( $wc_order_id );
    if ( ! $order instanceof \WC_Order ) return;
    $mb = $wpdb->get_row( $wpdb->prepare( "SELECT id,nome FROM {$wpdb->prefix}sz_motoboys WHERE id=%d LIMIT 1", $motoboy_id ) );
    $nome = $mb ? (string) $mb->nome : '';

    foreach ( [ '_senderzz_motoboy_id', '_sz_motoboy_id', '_motoboy_user_id', '_senderzz_motoboy_responsavel_id', '_senderzz_motoboy_entregador_id', '_senderzz_motoboy_assigned_id', '_senderzz_motoboy_atribuido_id' ] as $key ) {
        $order->update_meta_data( $key, $motoboy_id );
    }
    foreach ( [ '_senderzz_motoboy_name', '_senderzz_motoboy_nome', '_sz_motoboy_name', '_sz_motoboy_nome', '_motoboy_name', '_motoboy_nome', '_senderzz_motoboy_responsavel_nome' ] as $key ) {
        $order->update_meta_data( $key, $nome );
    }
    $order->update_meta_data( '_senderzz_delivery_mode', 'motoboy' );
    $order->add_order_note( 'Senderzz Motoboy: motoboy definido como ' . ( $nome ?: ( 'ID ' . $motoboy_id ) ) . ' pelo admin.' );
    $order->save();
}

function sz_mb_admin_delete_motoboy_financial_rows_for_order( int $wc_order_id ): void {
    global $wpdb;
    if ( $wc_order_id <= 0 ) return;
    foreach ( [ 'sz_motoboy_wallet', 'sz_motoboy_fechamento' ] as $suffix ) {
        $table = $wpdb->prefix . $suffix;
        if ( ! $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $table ) ) ) continue;
        $cols = $wpdb->get_col( "SHOW COLUMNS FROM {$table}", 0 );
        if ( in_array( 'wc_order_id', (array) $cols, true ) ) {
            $wpdb->delete( $table, [ 'wc_order_id' => $wc_order_id ] );
        } elseif ( in_array( 'order_id', (array) $cols, true ) ) {
            $wpdb->delete( $table, [ 'order_id' => $wc_order_id ] );
        }
    }
}

function sz_mb_admin_sync_row_from_wc( object $ped ): object {
    global $wpdb;
    if ( ! function_exists( 'wc_get_order' ) ) return $ped;
    $order = wc_get_order( (int) ( $ped->wc_order_id ?? 0 ) );
    if ( ! $order instanceof \WC_Order ) return $ped;

    $real_status = sz_mb_admin_status_from_wc( (int) $ped->wc_order_id, (string) ( $ped->status ?? 'agendado' ) );
    $meta_mid = (int) sz_mb_admin_order_meta_first( $order, sz_mb_admin_motoboy_meta_keys_id(), 0 );

    $update = [];
    if ( $real_status && $real_status !== (string) ( $ped->status ?? '' ) ) {
        $update['status'] = $real_status;
        $ped->status = $real_status;
    }
    if ( $meta_mid > 0 && $meta_mid !== (int) ( $ped->motoboy_id ?? 0 ) ) {
        $update['motoboy_id'] = $meta_mid;
        $ped->motoboy_id = $meta_mid;
        if ( in_array( $real_status, [ 'entregue', 'frustrado' ], true ) ) {
            $update['baixa_motoboy_id'] = $meta_mid;
            $ped->baixa_motoboy_id = $meta_mid;
        }
    }
    if ( $update ) {
        $update['updated_at'] = function_exists( 'sz_motoboy_now_mysql' ) ? sz_motoboy_now_mysql() : current_time( 'mysql' );
        $wpdb->update( $wpdb->prefix . 'sz_motoboy_pedidos', $update, [ 'id' => (int) $ped->id ] );
    }

    if ( (int) ( $ped->motoboy_id ?? 0 ) > 0 ) {
        $mb_nome = $wpdb->get_var( $wpdb->prepare( "SELECT nome FROM {$wpdb->prefix}sz_motoboys WHERE id=%d LIMIT 1", (int) $ped->motoboy_id ) );
        $ped->motoboy_nome = $mb_nome ? (string) $mb_nome : '';
    } else {
        $ped->motoboy_nome = '';
    }
    return $ped;
}

function sz_mb_admin_cleanup_invalid_motoboy_zones(): void {
    global $wpdb;
    $p = $wpdb->prefix;
    $wpdb->query( "DELETE piv FROM {$p}sz_motoboy_zona_pivot piv
        LEFT JOIN {$p}sz_motoboy_zonas z ON z.id = piv.zona_id AND z.ativo = 1
        LEFT JOIN {$p}sz_motoboy_cep_zonas cz ON cz.zona_id = piv.zona_id
        WHERE z.id IS NULL OR cz.id IS NULL" );
    $wpdb->query( "UPDATE {$p}sz_motoboys m
        LEFT JOIN {$p}sz_motoboy_zonas z ON z.id = m.zona_id AND z.ativo = 1
        LEFT JOIN {$p}sz_motoboy_cep_zonas cz ON cz.zona_id = m.zona_id
        SET m.zona_id = 0
        WHERE m.zona_id IS NOT NULL AND m.zona_id <> 0 AND (z.id IS NULL OR cz.id IS NULL)" );
}

// ── Pedidos ───────────────────────────────────────────────────────────────────
function sz_mb_tab_pedidos(): void {
    global $wpdb;
    $p      = $wpdb->prefix;
    $status = sanitize_text_field( $_GET['status'] ?? '' );
    $where  = "WHERE 1=1"; // filtro de status é aplicado abaixo usando WooCommerce como fonte real

    // Alan pode embalar via POST
    if ( $_SERVER['REQUEST_METHOD'] === 'POST' && isset($_POST['sz_mb_embalar']) ) {
        check_admin_referer('sz_mb_embalar');
        $pid = (int) $_POST['pedido_id'];
        $mid = (int) $_POST['motoboy_id'];
        sz_motoboy_mudar_status( $pid, 'embalado', $mid ? ['motoboy_id'=>$mid] : [], 'alan', get_current_user_id() );
        wp_safe_redirect( add_query_arg(['page'=>'senderzz','area'=>'cod','tab'=>'cod-pedidos','mb_tab'=>'pedidos','msg'=>'saved'], admin_url('admin.php')) );
        exit;
    }


    // Trocar motoboy sem mudar status (embalado/em_rota)
    if ( $_SERVER['REQUEST_METHOD'] === 'POST' && isset($_POST['sz_mb_trocar_motoboy']) ) {
        check_admin_referer('sz_mb_trocar_motoboy');
        $pid    = (int) ( $_POST['pedido_id'] ?? 0 );
        $mb_novo= (int) ( $_POST['motoboy_id'] ?? 0 );
        if ( $pid && $mb_novo ) {
            $pedido_row = $wpdb->get_row( $wpdb->prepare( "SELECT wc_order_id,status FROM {$p}sz_motoboy_pedidos WHERE id=%d", $pid ) );
            $update = [ 'motoboy_id' => $mb_novo, 'updated_at' => sz_motoboy_now_mysql() ];
            if ( $pedido_row && in_array( (string) $pedido_row->status, [ 'entregue', 'frustrado' ], true ) ) { $update['baixa_motoboy_id'] = $mb_novo; }
            $wpdb->update( $p . 'sz_motoboy_pedidos', $update, [ 'id' => $pid ] );
            if ( $pedido_row ) {
                sz_mb_admin_set_order_motoboy_meta( (int) $pedido_row->wc_order_id, $mb_novo );
                sz_mb_admin_delete_motoboy_financial_rows_for_order( (int) $pedido_row->wc_order_id );
                if ( function_exists( 'sz_mbc_set_pedido_status' ) ) {
                    $st_custody = in_array( (string) $pedido_row->status, [ 'em_rota', 'a_caminho' ], true ) ? 'with_motoboy' : ( (string) $pedido_row->status === 'frustrado' ? 'frustrated' : ( (string) $pedido_row->status === 'entregue' ? 'delivered' : 'reserved' ) );
                    sz_mbc_set_pedido_status( $pid, $st_custody, [ 'motoboy_id' => $mb_novo, 'actor_tipo' => 'admin', 'actor_id' => get_current_user_id(), 'note' => in_array( $st_custody, [ 'with_motoboy', 'frustrated' ], true ) ? 'Transferência de custódia entre motoboys.' : 'Ajuste de motoboy responsável antes da rota.' ] );
                }
            }
            if ( function_exists( 'sz_mbw_sync_all_data' ) ) { sz_mbw_sync_all_data(); }
        }
        wp_safe_redirect( add_query_arg(['page'=>'senderzz','area'=>'cod','tab'=>'cod-pedidos','mb_tab'=>'pedidos','msg'=>'saved'], admin_url('admin.php')) );
        exit;
    }

    // Transferir pedido em_rota para outro motoboy
    if ( $_SERVER['REQUEST_METHOD'] === 'POST' && isset($_POST['sz_mb_transferir']) ) {
        check_admin_referer('sz_mb_transferir');
        $pid    = (int) ( $_POST['pedido_id'] ?? 0 );
        $mb_novo= (int) ( $_POST['motoboy_id'] ?? 0 );
        if ( $pid && $mb_novo ) {
            $pedido_row_for_meta = $wpdb->get_row( $wpdb->prepare( "SELECT wc_order_id,status FROM {$p}sz_motoboy_pedidos WHERE id=%d", $pid ) );
            $update = [ 'motoboy_id' => $mb_novo, 'updated_at' => sz_motoboy_now_mysql() ];
            if ( $pedido_row_for_meta && in_array( (string) $pedido_row_for_meta->status, [ 'entregue', 'frustrado' ], true ) ) { $update['baixa_motoboy_id'] = $mb_novo; }
            $wpdb->update( $p . 'sz_motoboy_pedidos', $update, [ 'id' => $pid ] );
            if ( $pedido_row_for_meta ) {
                sz_mb_admin_set_order_motoboy_meta( (int) $pedido_row_for_meta->wc_order_id, $mb_novo );
                sz_mb_admin_delete_motoboy_financial_rows_for_order( (int) $pedido_row_for_meta->wc_order_id );
                if ( function_exists( 'sz_mbc_set_pedido_status' ) ) {
                    $st_custody = in_array( (string) $pedido_row_for_meta->status, [ 'em_rota', 'a_caminho' ], true ) ? 'with_motoboy' : ( (string) $pedido_row_for_meta->status === 'frustrado' ? 'frustrated' : ( (string) $pedido_row_for_meta->status === 'entregue' ? 'delivered' : 'reserved' ) );
                    sz_mbc_set_pedido_status( $pid, $st_custody, [ 'motoboy_id' => $mb_novo, 'actor_tipo' => 'admin', 'actor_id' => get_current_user_id(), 'note' => in_array( $st_custody, [ 'with_motoboy', 'frustrated' ], true ) ? 'Transferência de custódia entre motoboys.' : 'Ajuste de motoboy responsável antes da rota.' ] );
                }
            }
            if ( function_exists( 'sz_mbw_sync_all_data' ) ) { sz_mbw_sync_all_data(); }
            // Notifica o novo motoboy via push se disponível
            if ( function_exists('sz_mb_push_motoboy') ) {
                $ped_row = $wpdb->get_row( $wpdb->prepare(
                    "SELECT wc_order_id, dest_nome FROM {$p}sz_motoboy_pedidos WHERE id=%d", $pid
                ) );
                if ( $ped_row ) {
                    sz_mb_push_motoboy( $mb_novo,
                        '📦 Pedido transferido para você',
                        'Pedido #' . $ped_row->wc_order_id . ' — ' . $ped_row->dest_nome . ' está na sua rota.'
                    );
                }
            }
        }
        wp_safe_redirect( add_query_arg(['page'=>'senderzz','area'=>'cod','tab'=>'cod-pedidos','mb_tab'=>'pedidos','status'=>'em_rota','msg'=>'saved'], admin_url('admin.php')) );
        exit;
    }

    // Em rota não é mais ação manual do admin/OL: somente o motoboy bipando QR Code da etiqueta.
    if ( $_SERVER['REQUEST_METHOD'] === 'POST' && isset($_POST['sz_mb_enviar_rota']) ) {
        check_admin_referer('sz_mb_enviar_rota');
        wp_safe_redirect( add_query_arg(['page'=>'senderzz','area'=>'cod','tab'=>'cod-pedidos','mb_tab'=>'pedidos','status'=>'embalado','msg'=>'route_qr_only'], admin_url('admin.php')) );
        exit;
    }

    // Marcar como entregue pelo admin Motoboy. Usa a rotina central para sincronizar Woo + tabela operacional.
    if ( $_SERVER['REQUEST_METHOD'] === 'POST' && isset($_POST['sz_mb_entregar']) ) {
        check_admin_referer('sz_mb_entregar');
        $pid = (int) ( $_POST['pedido_id'] ?? 0 );
        if ( $pid ) {
            sz_motoboy_mudar_status( $pid, 'entregue', [], 'alan', get_current_user_id() );
        }
        wp_safe_redirect( add_query_arg(['page'=>'senderzz','area'=>'cod','tab'=>'cod-pedidos','mb_tab'=>'pedidos','status'=>'entregue','msg'=>'saved'], admin_url('admin.php')) );
        exit;
    }

    // Reagendar pedido pelo admin sem entrar no portal do produtor
    if ( $_SERVER['REQUEST_METHOD'] === 'POST' && isset($_POST['sz_mb_reagendar']) ) {
        check_admin_referer('sz_mb_reagendar');
        $pid  = (int) ($_POST['pedido_id'] ?? 0);
        $date = sanitize_text_field( wp_unslash( $_POST['reagendado_para'] ?? '' ) );
        if ( $pid && preg_match('/^\d{4}-\d{2}-\d{2}$/', $date) ) {
            $pedido_row = $wpdb->get_row( $wpdb->prepare("SELECT wc_order_id, status FROM {$p}sz_motoboy_pedidos WHERE id=%d", $pid) );
            // Reagendamento pelo admin não altera status: mantém agendado/embalado/em_rota/etc.
            $wpdb->update(
                $p . 'sz_motoboy_pedidos',
                [ 'reagendado_para' => $date, 'updated_at' => sz_motoboy_now_mysql() ],
                [ 'id' => $pid ],
                [ '%s', '%s' ],
                [ '%d' ]
            );
            if ( $pedido_row && function_exists('wc_get_order') ) {
                $order = wc_get_order( (int) $pedido_row->wc_order_id );
                if ( $order ) {
                    $order->update_meta_data( '_sz_delivery_date', $date );
                    $order->update_meta_data( '_senderzz_delivery_date', $date );
                    $order->update_meta_data( '_sz_motoboy_entrega_data', $date );
                    $order->update_meta_data( '_senderzz_motoboy_reagendado_para', $date );
                    $order->update_meta_data( '_senderzz_rescheduled_at', sz_motoboy_now_mysql() );
                    $order->update_meta_data( '_senderzz_rescheduled_by', get_current_user_id() );
                    $order->add_order_note( 'Senderzz Motoboy: data de entrega alterada pelo admin para ' . date_i18n( 'd/m/Y', strtotime( $date ) ) . ' sem alterar status.' );
                    $order->save();
                }
            }
            wp_safe_redirect( add_query_arg(['page'=>'senderzz','area'=>'cod','tab'=>'cod-pedidos','mb_tab'=>'pedidos','msg'=>'saved'], admin_url('admin.php')) );
            exit;
        }
        wp_safe_redirect( add_query_arg(['page'=>'senderzz','area'=>'cod','tab'=>'cod-pedidos','mb_tab'=>'pedidos','msg'=>'error'], admin_url('admin.php')) );
        exit;
    }

    // Cancelar pedido motoboy pelo admin
    if ( $_SERVER['REQUEST_METHOD'] === 'POST' && isset($_POST['sz_mb_cancelar']) ) {
        check_admin_referer('sz_mb_cancelar');
        $pid = (int) ($_POST['pedido_id'] ?? 0);
        if ( $pid ) {
            $pedido_row = $wpdb->get_row( $wpdb->prepare("SELECT wc_order_id FROM {$p}sz_motoboy_pedidos WHERE id=%d", $pid) );
            sz_motoboy_mudar_status( $pid, 'cancelado', [], 'alan', get_current_user_id() );
            if ( $pedido_row && function_exists('wc_get_order') ) {
                $order = wc_get_order( (int) $pedido_row->wc_order_id );
                if ( $order ) {
                    $order->update_meta_data( '_senderzz_motoboy_flow_status', 'cancelado' );
                    $order->add_order_note( 'Senderzz Motoboy: pedido cancelado pelo admin do motoboy.' );
                    $order->save();
                }
            }
            wp_safe_redirect( add_query_arg(['page'=>'senderzz','area'=>'cod','tab'=>'cod-pedidos','mb_tab'=>'pedidos','msg'=>'saved'], admin_url('admin.php')) );
            exit;
        }
        wp_safe_redirect( add_query_arg(['page'=>'senderzz','area'=>'cod','tab'=>'cod-pedidos','mb_tab'=>'pedidos','msg'=>'error'], admin_url('admin.php')) );
        exit;
    }

    // Antes de listar, reconstruímos as linhas operacionais que possam ter nascido só no Woo/HPOS.
    // Isso evita pedido COD motoboy existir no pedido WooCommerce, mas não aparecer na aba Motoboy.
    if ( function_exists( 'sz_motoboy_backfill_recent_orders' ) ) {
        sz_motoboy_backfill_recent_orders( 3000 );
    }
    if ( function_exists( 'sz_mbw_backfill_operational_from_wc_meta' ) ) {
        sz_mbw_backfill_operational_from_wc_meta();
    }

    $pedidos = $wpdb->get_results(
        "SELECT mp.*, m.nome AS motoboy_nome, z.nome AS zona_nome, cd.nome AS cd_nome
           FROM {$p}sz_motoboy_pedidos mp
           LEFT JOIN {$p}sz_motoboys m ON m.id=mp.motoboy_id
           LEFT JOIN {$p}sz_motoboy_zonas z ON z.id=mp.zona_id
           LEFT JOIN {$p}sz_motoboy_cds cd ON cd.id=mp.cd_id
          $where ORDER BY mp.wc_order_id DESC, mp.created_at DESC LIMIT 500"
    );

    $pedidos = array_map( 'sz_mb_admin_sync_row_from_wc', (array) $pedidos );

    if ( $status !== '' ) {
        $pedidos = array_values( array_filter( (array) $pedidos, function( $ped ) use ( $status ) {
            return sz_mb_admin_status_from_wc( (int) ( $ped->wc_order_id ?? 0 ), (string) ( $ped->status ?? '' ) ) === $status;
        } ) );
    }

    if ( function_exists( 'sz_mbw_sync_all_data' ) ) { sz_mbw_sync_all_data(); }

    $motoboys_list = $wpdb->get_results( "SELECT id, nome FROM {$p}sz_motoboys WHERE ativo=1 ORDER BY nome" );
    ?>
    <div class="sz-mb-card">
        <div style="display:flex;gap:8px;flex-wrap:wrap;margin-bottom:16px">
            <?php
            $filtros = [''=>'Todos','agendado'=>'Agendados','embalado'=>'Embalados','em_rota'=>'Em Rota','entregue'=>'Entregues','frustrado'=>'Frustrados','cancelado'=>'Cancelados'];
            foreach ( $filtros as $k => $label ) :
                $url = add_query_arg(['page'=>'senderzz','area'=>'cod','tab'=>'cod-pedidos','mb_tab'=>'pedidos','status'=>$k], admin_url('admin.php'));
                $active = ($status===$k) ? 'background:#E8650A;color:#fff' : 'background:#f3f4f6;color:#374151';
            ?>
                <a href="<?php echo esc_url($url); ?>" style="<?php echo $active; ?>;padding:6px 14px;border-radius:999px;font-size:var(--sz-text-meta);font-weight:700;text-decoration:none"><?php echo $label; ?></a>
            <?php endforeach; ?>
        </div>

        <?php if ( ! $pedidos ) : ?>
            <div class="sz-mb-empty">Nenhum pedido encontrado para este filtro.</div>
        <?php else : ?>
        <div class="sz-mb-queue">
            <?php foreach ( $pedidos as $ped ) : ?>
                <?php $sz_real_status = sz_mb_admin_status_from_wc( (int) $ped->wc_order_id, (string) $ped->status ); ?>
                <div class="sz-mb-order-card">
                    <div class="sz-mb-order-main">
                        <a class="sz-mb-order-id" href="<?php echo esc_url( get_edit_post_link($ped->wc_order_id) ); ?>" target="_blank">#<?php echo (int) $ped->wc_order_id; ?></a>
                        <div style="min-width:0">
                            <div class="sz-mb-order-title"><?php echo esc_html($ped->dest_nome ?: 'Cliente sem nome'); ?></div>
                            <div class="sz-mb-order-sub"><?php echo esc_html(trim(($ped->dest_cidade ?: '—') . ' — ' . ($ped->dest_cep ?: '—'))); ?></div>
                        </div>
                    </div>
                    <div class="sz-mb-order-meta"><span class="sz-mb-order-label">CD / Zona</span><span class="sz-mb-order-value"><?php echo esc_html(trim(($ped->cd_nome ?: '—') . ' / ' . ($ped->zona_nome ?: '—'))); ?></span></div>
                    <div class="sz-mb-order-meta"><span class="sz-mb-order-label">Motoboy</span><span class="sz-mb-order-value"><?php echo esc_html($ped->motoboy_nome ?: '—'); ?></span></div>
                    <div class="sz-mb-order-meta"><span class="sz-mb-order-label">Status / Valor</span><span><span class="sz-mb-badge <?php echo esc_attr($sz_real_status); ?>"><?php echo esc_html(strtoupper(str_replace('_',' ',$sz_real_status))); ?></span></span><span class="sz-mb-order-price">R$ <?php echo number_format((float)$ped->valor_pedido,2,',','.'); ?></span></div>
                    <div class="sz-mb-order-actions">
                        <?php if ( in_array( $sz_real_status, [ 'agendado', 'aprovado' ], true ) ) : ?>
                            <form method="post" style="display:inline">
                                <?php wp_nonce_field('sz_mb_embalar'); ?>
                                <input type="hidden" name="pedido_id" value="<?php echo (int) $ped->id; ?>">
                                <select name="motoboy_id" style="font-size:var(--sz-text-meta);padding:4px;margin-right:4px;max-width:130px">
                                    <option value="">Motoboy automático</option>
                                    <?php foreach($motoboys_list as $mb): ?>
                                        <option value="<?php echo (int) $mb->id; ?>" <?php selected($mb->id,$ped->motoboy_id); ?>><?php echo esc_html($mb->nome); ?></option>
                                    <?php endforeach; ?>
                                </select>
                                <button name="sz_mb_embalar" value="1" class="sz-mb-btn sz-mb-btn-sm">📦 Embalar</button>
                            </form>
                        <?php endif; ?>

                        <?php if ( $sz_real_status === 'embalado' ) : ?>
                            <span class="sz-mb-soft" style="display:inline-flex;align-items:center;height:32px;padding:0 10px;border-radius:9px;font-size:var(--sz-text-meta);font-weight:700">Rota somente via QR no PWA do motoboy</span>
                        <?php endif; ?>

                        <?php if ( in_array( $sz_real_status, [ 'agendado', 'frustrado' ], true ) ) : ?>
                            <button type="button" class="sz-mb-btn sz-mb-btn-sm" style="background:#2563eb" onclick="szAdminReschedule(<?php echo (int) $ped->id; ?>, <?php echo (int) $ped->wc_order_id; ?>)">📅 Reagendar</button>
                            <form method="post" style="display:inline" onsubmit="return confirm('Cancelar este pedido?')">
                                <?php wp_nonce_field('sz_mb_cancelar'); ?>
                                <input type="hidden" name="sz_mb_cancelar" value="1">
                                <input type="hidden" name="pedido_id" value="<?php echo (int) $ped->id; ?>">
                                <button type="submit" class="sz-mb-btn sz-mb-btn-sm" style="background:#dc2626">✕ Cancelar</button>
                            </form>
                        <?php endif; ?>

                        <?php if ( in_array( $sz_real_status, [ 'agendado', 'embalado', 'em_rota' ], true ) ) : ?>
                            <form method="post" style="display:inline" onsubmit="return confirm('Marcar este pedido como entregue?')">
                                <?php wp_nonce_field('sz_mb_entregar'); ?>
                                <input type="hidden" name="sz_mb_entregar" value="1">
                                <input type="hidden" name="pedido_id" value="<?php echo (int) $ped->id; ?>">
                                <button type="submit" class="sz-mb-btn sz-mb-btn-sm" style="background:#16a34a">✅ Entregue</button>
                            </form>
                            <?php if ( in_array( $sz_real_status, [ 'embalado', 'em_rota' ], true ) ) : ?>
                                <form method="post" style="display:inline" onsubmit="return confirm('Transferir pedido para outro motoboy?')">
                                    <?php wp_nonce_field('sz_mb_transferir'); ?>
                                    <input type="hidden" name="sz_mb_transferir" value="1">
                                    <input type="hidden" name="pedido_id" value="<?php echo (int) $ped->id; ?>">
                                    <select name="motoboy_id" style="font-size:var(--sz-text-meta);padding:4px;margin-right:4px;max-width:130px">
                                        <?php foreach($motoboys_list as $mb): ?>
                                            <option value="<?php echo (int) $mb->id; ?>" <?php selected($mb->id,$ped->motoboy_id); ?>><?php echo esc_html($mb->nome); ?></option>
                                        <?php endforeach; ?>
                                    </select>
                                    <button type="submit" class="sz-mb-btn sz-mb-btn-sm" style="background:#7c3aed">🔄 Transferir</button>
                                </form>
                            <?php endif; ?>
                        <?php endif; ?>

                        <?php if ( in_array( $sz_real_status, [ 'embalado', 'em_rota' ], true ) ) : ?>
                            <form method="post" style="display:inline" onsubmit="return confirm('Cancelar este pedido?')">
                                <?php wp_nonce_field('sz_mb_cancelar'); ?>
                                <input type="hidden" name="sz_mb_cancelar" value="1">
                                <input type="hidden" name="pedido_id" value="<?php echo (int) $ped->id; ?>">
                                <button type="submit" class="sz-mb-btn sz-mb-btn-sm" style="background:#dc2626">✕ Cancelar</button>
                            </form>
                        <?php endif; ?>

                        <?php if ( $sz_real_status === 'frustrado' && ! empty( $ped->frustrado_motivo ) ) : ?>
                            <button type="button" class="sz-mb-btn sz-mb-btn-sm" style="background:#6b7280" onclick="szAdminVerFrustracao(<?php echo (int) $ped->id; ?>, '<?php echo esc_js($ped->frustrado_motivo); ?>', '<?php echo esc_js($ped->frustrado_observacao ?? ''); ?>', '<?php echo esc_js($ped->entrega_foto ?? ''); ?>')">🔍 Ver motivo</button>
                        <?php endif; ?>

                        <?php if ( ! in_array( $sz_real_status, [ 'agendado', 'embalado', 'em_rota', 'frustrado' ], true ) ) : ?>
                            <span class="sz-mb-muted"><?php echo esc_html(date('d/m H:i', strtotime($ped->updated_at))); ?></span>
                        <?php endif; ?>
                    </div>
                </div>
            <?php endforeach; ?>
        </div>
        <?php endif; ?>
    </div>

    <div id="szMbAdminModal" class="sz-mb-modal-backdrop" onclick="if(event.target===this)this.style.display='none'">
        <div class="sz-mb-modal">
            <h3 id="szMbAdminModalTitle">Pedido</h3>
            <div id="szMbAdminModalBody"></div>
            <div style="margin-top:16px;text-align:right"><button type="button" class="sz-mb-btn" onclick="document.getElementById('szMbAdminModal').style.display='none'">Fechar</button></div>
        </div>
    </div>
    <form id="szMbAdminRescheduleForm" method="post" style="display:none">
        <?php wp_nonce_field('sz_mb_reagendar'); ?>
        <input type="hidden" name="sz_mb_reagendar" value="1">
        <input type="hidden" name="pedido_id" id="szMbAdminReschedulePedido" value="">
        <input type="hidden" name="reagendado_para" id="szMbAdminRescheduleDate" value="">
    </form>
    <script>
    function szAdminReschedule(pedidoId, wcOrderId){
        var date = window.prompt('Nova data de entrega para o pedido #' + wcOrderId + ' (AAAA-MM-DD):');
        if(!date){ return; }
        if(!/^\d{4}-\d{2}-\d{2}$/.test(date)){ alert('Informe a data no formato AAAA-MM-DD.'); return; }
        document.getElementById('szMbAdminReschedulePedido').value = pedidoId;
        document.getElementById('szMbAdminRescheduleDate').value = date;
        document.getElementById('szMbAdminRescheduleForm').submit();
    }
    function szAdminVerFrustracao(pedidoId, motivo, observacao, foto){
        var modal = document.getElementById('szMbAdminModal');
        var esc = function(v){ return String(v || '').replace(/[&<>"]/g, function(c){return {'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c];}); };
        document.getElementById('szMbAdminModalTitle').textContent = 'Frustração do pedido motoboy #' + pedidoId;
        var html = '<p><strong>Motivo:</strong><br>' + esc(motivo || 'Não informado') + '</p>';
        html += '<p><strong>Descrição:</strong><br>' + esc(observacao || 'Não informada') + '</p>';
        if(foto){ html += '<p><strong>Foto registrada:</strong></p><img src="' + foto + '" alt="Foto da frustração">'; }
        else { html += '<p class="sz-mb-muted">Nenhuma foto registrada pelo motoboy.</p>'; }
        document.getElementById('szMbAdminModalBody').innerHTML = html;
        modal.style.display = 'flex';
    }
    </script>
    <?php
}

// ── Helpers: múltiplas zonas por motoboy ─────────────────────────────────────
function sz_mb_save_motoboy_zonas( int $motoboy_id, array $zona_ids ): void {
    global $wpdb;
    $t = $wpdb->prefix . 'sz_motoboy_zona_pivot';
    // Verifica se tabela existe
    if ( ! $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $t ) ) ) return;
    $wpdb->delete( $t, ['motoboy_id' => $motoboy_id] );
    foreach ( array_unique( array_filter( $zona_ids ) ) as $zona_id ) {
        $wpdb->insert( $t, ['motoboy_id' => $motoboy_id, 'zona_id' => (int) $zona_id] );
    }
}

function sz_mb_get_motoboy_zonas( int $motoboy_id ): array {
    global $wpdb;
    $t = $wpdb->prefix . 'sz_motoboy_zona_pivot';
    if ( ! $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $t ) ) ) return [];
    return $wpdb->get_results( $wpdb->prepare(
        "SELECT z.id, z.nome FROM {$t} p
         INNER JOIN {$wpdb->prefix}sz_motoboy_zonas z ON z.id = p.zona_id
         WHERE p.motoboy_id = %d AND z.ativo = 1
         ORDER BY z.nome",
        $motoboy_id
    ) ) ?: [];
}

// ── Motoboys ──────────────────────────────────────────────────────────────────
function sz_mb_tab_motoboys(): void {
    global $wpdb;
    $p = $wpdb->prefix;

    if ( $_SERVER['REQUEST_METHOD'] === 'POST' && isset( $_POST['_wpnonce'] ) ) {
        if ( ! wp_verify_nonce( sanitize_text_field( wp_unslash( $_POST['_wpnonce'] ) ), 'sz_mb_motoboy_save' ) ) {
            wp_die( 'Nonce inválido. Volte e tente novamente.', 'Erro de segurança', ['back_link'=>true] );
        }
        if ( ! current_user_can( 'manage_woocommerce' ) && ! current_user_can( 'manage_options' ) ) {
            wp_die( 'Permissão negada.', 'Erro', ['back_link'=>true] );
        }

        $id   = (int) ( $_POST['motoboy_id'] ?? 0 );
        $nome = sanitize_text_field( $_POST['nome'] ?? '' );
        if ( ! $nome ) {
            wp_safe_redirect( admin_url( 'admin.php?page=senderzz&area=cod&tab=cod-motoboys&mb_tab=motoboys&msg=error' ) );
            exit;
        }

        // Zonas: múltiplas (checkbox) ou única (select legacy)
        $zonas_sel = isset( $_POST['zona_ids'] ) ? array_map('absint', (array) $_POST['zona_ids'] ) : [];
        if ( empty( $zonas_sel ) && ! empty( $_POST['zona_id'] ) ) {
            $zonas_sel = [ absint( $_POST['zona_id'] ) ];
        }
        if ( $zonas_sel ) {
            $valid_zones = $wpdb->get_col( "SELECT z.id FROM {$p}sz_motoboy_zonas z WHERE z.ativo=1 AND EXISTS (SELECT 1 FROM {$p}sz_motoboy_cep_zonas cz WHERE cz.zona_id=z.id)" );
            $valid_zones = array_map( 'intval', (array) $valid_zones );
            $zonas_sel = array_values( array_intersect( array_map( 'intval', $zonas_sel ), $valid_zones ) );
        }
        $zona_principal = $zonas_sel[0] ?? 0;

        $data = [
            'cd_id'    => (int) ( $_POST['cd_id'] ?? 0 ),
            'zona_id'  => $zona_principal,
            'nome'     => $nome,
            'telefone' => sanitize_text_field( $_POST['telefone'] ?? '' ),
            'cpf'      => sanitize_text_field( $_POST['cpf'] ?? '' ),
            'email'    => sanitize_email( $_POST['email'] ?? '' ),
            'tipo_pgto'=> sanitize_key( $_POST['tipo_pgto'] ?? 'autonomo' ),
            'ativo'    => (int) ( $_POST['ativo'] ?? 1 ),
        ];

        // PIN: só atualiza se preenchido no form (não apaga PIN existente ao editar sem mudar)
        $novo_pin = sanitize_text_field( $_POST['pin'] ?? '' );
        if ( $novo_pin !== '' ) {
            if ( strlen( $novo_pin ) < 4 ) {
                wp_safe_redirect( admin_url( 'admin.php?page=senderzz&area=cod&tab=cod-motoboys&mb_tab=motoboys&msg=pin_curto' ) );
                exit;
            }
            $data['pin_hash'] = password_hash( $novo_pin, PASSWORD_BCRYPT );
        }

        if ( $id ) {
            $wpdb->update( $p . 'sz_motoboys', $data, ['id'=>$id] );
        } else {
            $data['token_app'] = wp_generate_password( 32, false );
            $wpdb->insert( $p . 'sz_motoboys', $data );
            $id = (int) $wpdb->insert_id;
        }

        // Grava zonas adicionais na tabela pivot; vazio limpa atribuições inválidas.
        if ( $id ) {
            sz_mb_save_motoboy_zonas( $id, $zonas_sel );
        }

        wp_safe_redirect( admin_url( 'admin.php?page=senderzz&area=cod&tab=cod-motoboys&mb_tab=motoboys&msg=saved' ) );
        exit;
    }

    if ( isset($_GET['delete_mb']) ) {
        check_admin_referer('sz_mb_delete');
        $wpdb->update( $p . 'sz_motoboys', ['ativo'=>0], ['id'=>(int)$_GET['delete_mb']] );
        wp_safe_redirect( admin_url( 'admin.php?page=senderzz&area=cod&tab=cod-motoboys&mb_tab=motoboys&msg=deleted' ) );
        exit;
    }

    sz_mb_admin_cleanup_invalid_motoboy_zones();

    $motoboys = $wpdb->get_results(
        "SELECT m.*, CASE WHEN z.id IS NOT NULL AND EXISTS (SELECT 1 FROM {$p}sz_motoboy_cep_zonas cz WHERE cz.zona_id=z.id) THEN z.nome ELSE NULL END AS zona_nome, cd.nome AS cd_nome
           FROM {$p}sz_motoboys m
           LEFT JOIN {$p}sz_motoboy_zonas z ON z.id=m.zona_id AND z.ativo=1
           LEFT JOIN {$p}sz_motoboy_cds cd ON cd.id=m.cd_id
          WHERE m.ativo=1 ORDER BY cd.nome, zona_nome, m.nome"
    );

    $cds   = $wpdb->get_results( "SELECT * FROM {$p}sz_motoboy_cds WHERE ativo=1" );
    $zonas = $wpdb->get_results( "SELECT z.* FROM {$p}sz_motoboy_zonas z WHERE z.ativo=1 AND EXISTS (SELECT 1 FROM {$p}sz_motoboy_cep_zonas cz WHERE cz.zona_id=z.id) ORDER BY z.cd_id, z.nome" );

    // Edição: carrega dados do motoboy existente
    $edit_id  = absint( $_GET['edit_mb'] ?? 0 );
    $editing  = $edit_id ? $wpdb->get_row( $wpdb->prepare( "SELECT * FROM {$p}sz_motoboys WHERE id=%d AND ativo=1 LIMIT 1", $edit_id ) ) : null;
    $edit_zonas_ids = $edit_id ? array_column( sz_mb_get_motoboy_zonas( $edit_id ), 'id' ) : [];

    // URL base sempre dentro do Senderzz
    $sz_mb_base_url = admin_url( 'admin.php?page=senderzz&area=cod&tab=cod-motoboys&mb_tab=motoboys' );

    $msg = sanitize_key( $_GET['msg'] ?? '' );
    ?>
    <?php if ( $msg === 'saved' ): ?><div class="notice notice-success is-dismissible"><p>✅ Motoboy salvo com sucesso.</p></div><?php endif; ?>
    <?php if ( $msg === 'deleted' ): ?><div class="notice notice-success is-dismissible"><p>🗑 Motoboy desativado.</p></div><?php endif; ?>
    <?php if ( $msg === 'error' ): ?><div class="notice notice-error"><p>❌ Nome obrigatório.</p></div><?php endif; ?>

    <div class="sz-mb-grid-2">
        <div class="sz-mb-card">
            <h2><?php echo $editing ? '✏️ Editar Motoboy: ' . esc_html($editing->nome) : '➕ Cadastrar Motoboy'; ?></h2>
            <?php if ( $editing ): ?>
                <a href="<?php echo esc_url( $sz_mb_base_url ); ?>" class="button" style="margin-bottom:12px;display:inline-block">← Novo cadastro</a>
            <?php endif; ?>
            <form method="post" class="sz-mb-form">
                <?php wp_nonce_field('sz_mb_motoboy_save'); ?>
                <input type="hidden" name="motoboy_id" value="<?php echo (int)($editing->id??0); ?>">

                <label>Nome *</label>
                <input type="text" name="nome" value="<?php echo esc_attr($editing->nome??''); ?>" required>

                <label>Telefone * (com DDD)</label>
                <input type="text" name="telefone" value="<?php echo esc_attr($editing->telefone??''); ?>" required>

                <label>CPF</label>
                <input type="text" name="cpf" value="<?php echo esc_attr($editing->cpf??''); ?>">

                <label>E-mail</label>
                <input type="email" name="email" value="<?php echo esc_attr($editing->email??''); ?>">

                <label>CD *</label>
                <select name="cd_id" required>
                    <option value="">Selecione...</option>
                    <?php foreach($cds as $cd): ?>
                        <option value="<?php echo $cd->id; ?>" <?php selected((int)($editing->cd_id??0),(int)$cd->id); ?>>
                            <?php echo esc_html($cd->nome); ?>
                        </option>
                    <?php endforeach; ?>
                </select>

                <label>Zonas de atuação <span style="font-size:var(--sz-text-sm);color:#999">* Ctrl+clique para marcar várias</span></label>
                <select name="zona_ids[]" multiple size="8" style="width:100%;min-height:180px;font-size:var(--sz-text-base);padding:4px;border:1px solid #8c8f94;border-radius:4px;background:#fff">
                <?php
                $cd_atual = null;
                foreach ( $zonas as $z ) :
                    $sel = in_array( (int) $z->id, array_map( 'intval', $edit_zonas_ids ), true )
                        || ( empty( $edit_zonas_ids ) && $editing && (int) $editing->zona_id === (int) $z->id );
                    if ( $cd_atual !== (int) $z->cd_id ) :
                        if ( $cd_atual !== null ) echo '</optgroup>';
                        $cd_atual = (int) $z->cd_id;
                        $cd_obj   = array_filter( $cds, fn($c) => (int)$c->id === $cd_atual );
                        $cd_nome  = $cd_obj ? reset($cd_obj)->nome : 'CD ' . $cd_atual;
                        echo '<optgroup label="' . esc_attr($cd_nome) . '">';
                    endif;
                ?>
                    <option value="<?php echo (int)$z->id; ?>" <?php selected($sel); ?>><?php echo esc_html($z->nome); ?></option>
                <?php endforeach; if($cd_atual!==null) echo '</optgroup>'; ?>
                </select>

                <label>Tipo</label>
                <select name="tipo_pgto">
                    <option value="autonomo" <?php selected($editing->tipo_pgto??'autonomo','autonomo'); ?>>Autônomo</option>
                    <option value="pj"       <?php selected($editing->tipo_pgto??'','pj'); ?>>PJ / MEI</option>
                    <option value="clt"      <?php selected($editing->tipo_pgto??'','clt'); ?>>CLT</option>
                </select>

                <?php if ( $editing ): ?>
                <label>Token App <span style="font-size:var(--sz-text-sm);color:#9ca3af">(somente leitura)</span></label>
                <input type="text" value="<?php echo esc_attr($editing->token_app??''); ?>" readonly style="font-family:var(--sz-font);font-size:var(--sz-text-sm);background:#f9fafb">
              </div>
              <div class="form-group">
                <label class="form-label">PIN de acesso ao PWA <small style="color:#9ca3af">(deixe em branco para não alterar)</small></label>
                <input type="password" name="pin" placeholder="4 a 8 dígitos" maxlength="8" autocomplete="new-password" style="width:100%;height:40px;border:1px solid #d1d5db;border-radius:8px;padding:0 12px">
                <div style="font-size:var(--sz-text-sm);color:#6b7280;margin-top:4px">
                  <?php echo $editing && !empty($editing->pin_hash) ? '🔒 PIN configurado' : '⚠️ Sem PIN — motoboy pode logar só com telefone'; ?>
                </div>
                <?php endif; ?>

                <br>
                <button class="sz-mb-btn"><?php echo $editing ? '💾 Salvar alterações' : 'Salvar Motoboy'; ?></button>
            </form>
        </div>

        <div class="sz-mb-card">
            <h2>Motoboys Ativos</h2>
            <table class="sz-mb-table">
                <thead><tr><th>Nome</th><th>CD / Zonas</th><th>Telefone</th><th>Token</th><th></th></tr></thead>
                <tbody>
                <?php foreach ( $motoboys as $mb ) :
                    $mb_zonas = sz_mb_get_motoboy_zonas( $mb->id );
                    $zona_labels = array_map(function($z){ return $z->nome; }, $mb_zonas);
                    if ( empty($zona_labels) && $mb->zona_nome ) $zona_labels = [$mb->zona_nome];
                    $is_editing = $editing && (int)$editing->id === (int)$mb->id;
                ?>
                    <tr style="<?php echo $is_editing ? 'background:#fff7ed;' : ''; ?>">
                        <td><strong><?php echo esc_html($mb->nome); ?></strong></td>
                        <td>
                            <span style="font-size:var(--sz-text-meta);color:#374151"><?php echo esc_html($mb->cd_nome); ?></span><br>
                            <span style="font-size:var(--sz-text-sm);color:#6b7280"><?php echo esc_html(implode(', ', $zona_labels) ?: '—'); ?></span>
                        </td>
                        <td><?php echo esc_html($mb->telefone); ?></td>
                        <td style="font-family:var(--sz-font);font-size:var(--sz-text-xs);color:#9ca3af"><?php echo esc_html(substr($mb->token_app??'',0,10)); ?>…</td>
                        <td style="white-space:nowrap">
                            <a href="<?php echo esc_url( add_query_arg( ['edit_mb'=>$mb->id], $sz_mb_base_url ) ); ?>" class="button button-small">✏️</a>
                            <a href="<?php echo esc_url( wp_nonce_url( add_query_arg( ['delete_mb'=>$mb->id], $sz_mb_base_url ), 'sz_mb_delete' ) ); ?>" class="sz-mb-danger" onclick="return confirm('Desativar este motoboy?')" style="margin-left:4px">✕</a>
                        </td>
                    </tr>
                <?php endforeach; ?>
                </tbody>
            </table>
        </div>
    </div>
    <?php
}

// ── CDs & Zonas ───────────────────────────────────────────────────────────────
function sz_mb_tab_zonas(): void {
    global $wpdb;
    $p = $wpdb->prefix;

    $base_page_args = ( ( $_GET['page'] ?? '' ) === 'senderzz' )
        ? [ 'page' => 'senderzz', 'tab' => 'motoboy', 'mb_tab' => 'zonas' ]
        : [ 'page' => 'senderzz', 'area' => 'cod', 'tab' => 'cod-motoboys', 'mb_tab' => 'zonas' ];

    $sz_mb_normalize_cep = static function( $cep ) {
        return substr( preg_replace( '/\D/', '', (string) $cep ), 0, 8 );
    };
    $sz_mb_find_overlap = static function( int $zona_id, string $inicio, string $fim, int $ignore_id = 0 ) use ( $wpdb, $p ) {
        $sql = "SELECT cz.id, z.nome AS zona_nome, cz.cep_inicio, cz.cep_fim
                  FROM {$p}sz_motoboy_cep_zonas cz
                  INNER JOIN {$p}sz_motoboy_zonas z ON z.id=cz.zona_id AND z.ativo=1
                 WHERE cz.id <> %d
                   AND NOT (cz.cep_fim < %s OR cz.cep_inicio > %s)
                 LIMIT 1";
        return $wpdb->get_row( $wpdb->prepare( $sql, $ignore_id, $inicio, $fim ) );
    };

    // Delete CEP
    if ( isset($_GET['delete_cep']) && $_SERVER['REQUEST_METHOD'] === 'GET' ) {
        check_admin_referer('sz_mb_delete_cep');
        $cep_id = absint( $_GET['delete_cep'] );
        if ( $cep_id > 0 ) {
            $zona_id_deleted_cep = (int) $wpdb->get_var( $wpdb->prepare( "SELECT zona_id FROM {$p}sz_motoboy_cep_zonas WHERE id=%d", $cep_id ) );
            $wpdb->delete( $p . 'sz_motoboy_cep_zonas', [ 'id' => $cep_id ], [ '%d' ] );
            if ( $zona_id_deleted_cep > 0 ) { sz_mb_admin_cleanup_invalid_motoboy_zones(); }
        }
        wp_safe_redirect( add_query_arg( array_merge( $base_page_args, [ 'msg' => ( $wpdb->last_error ? 'error' : 'deleted' ) ] ), admin_url('admin.php') ) );
        exit;
    }

    // Delete Zona: remove primeiro as faixas de CEP vinculadas e depois desativa/remove a zona.
    // Mantém compatibilidade com tabelas antigas que usam coluna `ativo`.
    if ( isset($_GET['delete_zona']) && $_SERVER['REQUEST_METHOD'] === 'GET' ) {
        check_admin_referer('sz_mb_delete_zona');
        $zona_id = absint( $_GET['delete_zona'] );
        if ( $zona_id > 0 ) {
            $wpdb->delete( $p . 'sz_motoboy_cep_zonas', [ 'zona_id' => $zona_id ], [ '%d' ] );
            $wpdb->delete( $p . 'sz_motoboy_zona_pivot', [ 'zona_id' => $zona_id ], [ '%d' ] );
            $wpdb->update( $p . 'sz_motoboys', [ 'zona_id' => 0 ], [ 'zona_id' => $zona_id ], [ '%d' ], [ '%d' ] );
            $wpdb->delete( $p . 'sz_motoboy_zonas', [ 'id' => $zona_id ], [ '%d' ] );
        }
        wp_safe_redirect( add_query_arg( array_merge( $base_page_args, [ 'msg' => 'deleted' ] ), admin_url('admin.php') ) );
        exit;
    }

    if ( $_SERVER['REQUEST_METHOD'] === 'POST' ) {
        $acao = sanitize_key( $_POST['acao'] ?? '' );
        // Nonce específico por ação
        $nonce_action = 'sz_mb_zona_save_' . $acao;
        if ( ! wp_verify_nonce( $_POST['_wpnonce'] ?? '', $nonce_action ) ) {
            wp_die('Nonce inválido. Volte e tente novamente.');
        }

        if ( $acao === 'save_cd' ) {
            $wpdb->insert( $p . 'sz_motoboy_cds', [
                'nome'     => sanitize_text_field($_POST['cd_nome']),
                'cidade'   => sanitize_text_field($_POST['cd_cidade']),
                'uf'       => sanitize_key($_POST['cd_uf'] ?? 'SP'),
                'endereco' => sanitize_text_field($_POST['cd_endereco'] ?? ''),
            ] );
        } elseif ( $acao === 'save_zona' ) {
            // Zona sem faixa de CEP vira zona fantasma no seletor. A partir daqui,
            // toda zona operacional precisa nascer já com uma faixa válida.
            $zona_dias = function_exists( 'sz_motoboy_sanitize_zone_days' ) ? sz_motoboy_sanitize_zone_days( $_POST['zona_dias'] ?? [] ) : '1,2,3,4,5,6';
            $zona_cutoffs = function_exists( 'sz_motoboy_single_cutoff_payload' ) ? sz_motoboy_single_cutoff_payload( $_POST['zona_cutoff_time'] ?? '21:00' ) : '';
            $inicio = $sz_mb_normalize_cep( $_POST['zona_cep_inicio'] ?? '' );
            $fim    = $sz_mb_normalize_cep( $_POST['zona_cep_fim'] ?? '' );

            if ( strlen( $inicio ) !== 8 || strlen( $fim ) !== 8 ) {
                wp_safe_redirect( add_query_arg( array_merge( $base_page_args, [ 'msg' => 'cep_invalid' ] ), admin_url('admin.php') ) );
                exit;
            }
            if ( (int) $inicio > (int) $fim ) {
                wp_safe_redirect( add_query_arg( array_merge( $base_page_args, [ 'msg' => 'cep_order' ] ), admin_url('admin.php') ) );
                exit;
            }
            $overlap = $sz_mb_find_overlap( 0, $inicio, $fim, 0 );
            if ( $overlap ) {
                wp_safe_redirect( add_query_arg( array_merge( $base_page_args, [ 'msg' => 'cep_overlap' ] ), admin_url('admin.php') ) );
                exit;
            }

            $wpdb->insert( $p . 'sz_motoboy_zonas', [
                'cd_id'    => (int) $_POST['zona_cd_id'],
                'nome'     => sanitize_text_field($_POST['zona_nome']),
                'descricao'=> sanitize_text_field($_POST['zona_desc'] ?? ''),
                'dias_funcionamento' => $zona_dias,
                'cutoff_horarios' => $zona_cutoffs,
            ] );

            $nova_zona_id = (int) $wpdb->insert_id;
            if ( $nova_zona_id > 0 ) {
                $wpdb->insert( $p . 'sz_motoboy_cep_zonas', [
                    'zona_id' => $nova_zona_id,
                    'cep_inicio' => $inicio,
                    'cep_fim' => $fim,
                ], [ '%d', '%s', '%s' ] );
            }
        } elseif ( $acao === 'save_cep' || $acao === 'update_cep' ) {
            $cep_id = (int) ( $_POST['cep_id'] ?? 0 );
            $zona_id = (int) ( $_POST['cep_zona_id'] ?? 0 );
            $inicio = $sz_mb_normalize_cep( $_POST['cep_inicio'] ?? '' );
            $fim    = $sz_mb_normalize_cep( $_POST['cep_fim'] ?? '' );
            if ( strlen( $inicio ) !== 8 || strlen( $fim ) !== 8 || $zona_id <= 0 ) {
                wp_safe_redirect( add_query_arg( array_merge( $base_page_args, [ 'msg' => 'cep_invalid' ] ), admin_url('admin.php') ) );
                exit;
            }
            if ( (int) $inicio > (int) $fim ) {
                wp_safe_redirect( add_query_arg( array_merge( $base_page_args, [ 'msg' => 'cep_order' ] ), admin_url('admin.php') ) );
                exit;
            }
            $overlap = $sz_mb_find_overlap( $zona_id, $inicio, $fim, $acao === 'update_cep' ? $cep_id : 0 );
            if ( $overlap ) {
                wp_safe_redirect( add_query_arg( array_merge( $base_page_args, [ 'msg' => 'cep_overlap' ] ), admin_url('admin.php') ) );
                exit;
            }
            if ( $acao === 'update_cep' && $cep_id > 0 ) {
                $wpdb->update( $p . 'sz_motoboy_cep_zonas', [
                    'zona_id' => $zona_id, 'cep_inicio' => $inicio, 'cep_fim' => $fim,
                ], [ 'id' => $cep_id ], [ '%d', '%s', '%s' ], [ '%d' ] );
                $wpdb->update( $p . 'sz_motoboy_zonas', [
                    'dias_funcionamento' => function_exists( 'sz_motoboy_sanitize_zone_days' ) ? sz_motoboy_sanitize_zone_days( $_POST['zona_dias'] ?? [] ) : '1,2,3,4,5,6',
                    'cutoff_horarios' => function_exists( 'sz_motoboy_single_cutoff_payload' ) ? sz_motoboy_single_cutoff_payload( $_POST['zona_cutoff_time'] ?? '21:00' ) : '',
                ], [ 'id' => $zona_id ], [ '%s', '%s' ], [ '%d' ] );
            } else {
                $wpdb->insert( $p . 'sz_motoboy_cep_zonas', [
                    'zona_id' => $zona_id, 'cep_inicio' => $inicio, 'cep_fim' => $fim,
                ], [ '%d', '%s', '%s' ] );
            }
        } elseif ( $acao === 'duplicate_zona' ) {
            // Desativado: duplicar zona sem CEP criava zonas fantasmas.
            wp_safe_redirect( add_query_arg( array_merge( $base_page_args, [ 'msg' => 'ghost_prevented' ] ), admin_url('admin.php') ) );
            exit;
        }

        wp_safe_redirect( add_query_arg( array_merge( $base_page_args, [ 'msg' => 'saved' ] ), admin_url('admin.php') ) );
        exit;
    }

    $cds   = $wpdb->get_results( "SELECT * FROM {$p}sz_motoboy_cds WHERE ativo=1 ORDER BY nome" );
    // Somente zonas operacionais: zona ativa + pelo menos uma faixa de CEP.
    // Isso remove do visual as zonas antigas/fantasmas criadas sem cobertura.
    $zonas = $wpdb->get_results( "SELECT z.*, cd.nome AS cd_nome FROM {$p}sz_motoboy_zonas z LEFT JOIN {$p}sz_motoboy_cds cd ON cd.id=z.cd_id WHERE z.ativo=1 AND EXISTS (SELECT 1 FROM {$p}sz_motoboy_cep_zonas cz WHERE cz.zona_id=z.id) ORDER BY cd.nome, z.nome" );
    $ceps  = $wpdb->get_results( "SELECT cz.*, z.nome AS zona_nome, cd.nome AS cd_nome, z.dias_funcionamento, z.cutoff_horarios FROM {$p}sz_motoboy_cep_zonas cz INNER JOIN {$p}sz_motoboy_zonas z ON z.id=cz.zona_id AND z.ativo=1 LEFT JOIN {$p}sz_motoboy_cds cd ON cd.id=z.cd_id ORDER BY z.nome, cz.cep_inicio" );
    ?>
    <div class="sz-mb-grid-2" style="margin-bottom:16px">
        <div class="sz-mb-card">
            <h2>Novo Centro de Distribuição</h2>
            <form method="post" class="sz-mb-form">
                <?php wp_nonce_field('sz_mb_zona_save_save_cd'); ?>
                <input type="hidden" name="acao" value="save_cd">
                <label>Nome do CD *</label><input type="text" name="cd_nome" required placeholder="Ex: CD São Paulo Centro">
                <label>Cidade *</label><input type="text" name="cd_cidade" required placeholder="São Paulo">
                <label>UF</label><input type="text" name="cd_uf" value="SP" maxlength="2">
                <label>Endereço</label><input type="text" name="cd_endereco">
                <br><br><button class="sz-mb-btn">Cadastrar CD</button>
            </form>
        </div>

        <div class="sz-mb-card">
            <h2>Nova Zona + CEP</h2>
            <form method="post" class="sz-mb-form">
                <?php wp_nonce_field('sz_mb_zona_save_save_zona'); ?>
                <input type="hidden" name="acao" value="save_zona">
                <label>CD *</label>
                <select name="zona_cd_id" required>
                    <option value="">Selecione...</option>
                    <?php foreach($cds as $cd): ?><option value="<?php echo $cd->id; ?>"><?php echo esc_html($cd->nome); ?></option><?php endforeach; ?>
                </select>
                <label>Nome da Zona *</label><input type="text" name="zona_nome" required placeholder="Ex: Zona Sul">
                <label>Descrição</label><input type="text" name="zona_desc" placeholder="Opcional">
                <label>Dias de funcionamento *</label>
                <div class="sz-mb-weekdays">
                    <?php foreach ( [1=>'Seg',2=>'Ter',3=>'Qua',4=>'Qui',5=>'Sex',6=>'Sáb',0=>'Dom'] as $day_num => $day_label ) : ?>
                        <label><input type="checkbox" name="zona_dias[]" value="<?php echo esc_attr( $day_num ); ?>" <?php checked( $day_num !== 0 ); ?>> <?php echo esc_html( $day_label ); ?></label>
                    <?php endforeach; ?>
                </div>
                <label style="margin-top:12px">Horário limite único *</label>
                <input type="time" name="zona_cutoff_time" value="21:00" required style="max-width:160px">
                <p class="sz-mb-soft" style="margin:6px 0 0">Esse horário vale para todos os dias selecionados. Ex.: Sex 12:00 = entrega de sexta aceita até quinta 12:00; Sáb 12:00 = entrega de sábado aceita até sexta 12:00.</p>
                <div class="sz-mb-form-row" style="margin-top:12px">
                    <div><label>CEP início *</label><input type="text" name="zona_cep_inicio" required placeholder="01000000" maxlength="9"></div>
                    <div><label>CEP fim *</label><input type="text" name="zona_cep_fim" required placeholder="08499999" maxlength="9"></div>
                </div>
                <p class="sz-mb-soft" style="margin:8px 0 0">Obrigatório: zona operacional precisa ter faixa de CEP para aparecer no roteamento e evitar zona fantasma.</p>
                <br><button class="sz-mb-btn">Cadastrar Zona com CEP</button>
            </form>
        </div>
    </div>

    <div class="sz-mb-card">
        <h2>Faixas de CEP por Zona</h2>
        <form method="post" class="sz-mb-form sz-mb-form-row">
            <?php wp_nonce_field('sz_mb_zona_save_save_cep'); ?>
            <input type="hidden" name="acao" value="save_cep">
            <div><label>Zona *</label>
            <select name="cep_zona_id" required>
                <option value="">Selecione...</option>
                <?php foreach($zonas as $z): ?><option value="<?php echo $z->id; ?>"><?php echo esc_html($z->cd_nome . ' > ' . $z->nome); ?></option><?php endforeach; ?>
            </select></div>
            <div><label>CEP Início *</label><input type="text" name="cep_inicio" required placeholder="01000000" maxlength="9"></div>
            <div><label>CEP Fim *</label><input type="text" name="cep_fim" required placeholder="01999999" maxlength="9"></div>
            <div><button class="sz-mb-btn" style="margin-top:4px">Adicionar</button></div>
        </form>

        <div class="sz-mb-filterbar">
            <input type="search" id="szCepSearch" placeholder="Buscar zona, CD ou CEP...">
            <select id="szCepOperationFilter">
                <option value="">Todas as operações</option>
                <option value="seg-sab">Segunda a sábado</option>
                <option value="sexta">Apenas sexta-feira</option>
                <option value="sabado">Apenas sábado</option>
                <option value="domingo">Inclui domingo</option>
            </select>
            <select id="szCepZoneFilter">
                <option value="">Todas as zonas</option>
                <?php foreach($zonas as $z): ?><option value="<?php echo esc_attr( strtolower( $z->nome ) ); ?>"><?php echo esc_html($z->nome); ?></option><?php endforeach; ?>
            </select>
        </div>

        <table class="sz-mb-table" id="szCepTable">
            <thead><tr><th>Zona</th><th>CEP início</th><th>CEP fim</th><th>Operação</th><th>Ações</th></tr></thead>
            <tbody>
            <?php foreach ( $ceps as $cep ) : ?>
                <?php
                    $dias_cep = function_exists( 'sz_motoboy_zone_days_array' ) ? sz_motoboy_zone_days_array( $cep->dias_funcionamento ?? '' ) : [1,2,3,4,5,6];
                    $dias_label = function_exists( 'sz_motoboy_zone_days_label' ) ? sz_motoboy_zone_days_label( $cep->dias_funcionamento ?? '' ) : 'Seg, Ter, Qua, Qui, Sex, Sáb';
                    $cutoffs_cep = function_exists( 'sz_motoboy_zone_cutoffs_array' ) ? sz_motoboy_zone_cutoffs_array( $cep->cutoff_horarios ?? '' ) : [ '0'=>'21:00','1'=>'21:00','2'=>'21:00','3'=>'21:00','4'=>'21:00','5'=>'21:00','6'=>'21:00' ];
                    $single_cutoff = function_exists( 'sz_motoboy_zone_single_cutoff_time' ) ? sz_motoboy_zone_single_cutoff_time( $cep->cutoff_horarios ?? '', $cep->dias_funcionamento ?? '' ) : '21:00';
                    $cutoff_label = function_exists( 'sz_motoboy_zone_cutoff_label' ) ? sz_motoboy_zone_cutoff_label( $cep->cutoff_horarios ?? '', $cep->dias_funcionamento ?? '' ) : 'Limite padrão 21:00 do dia anterior';
                    $op_key = ( $dias_cep === [5] ) ? 'sexta' : ( ( $dias_cep === [6] ) ? 'sabado' : ( in_array( 0, $dias_cep, true ) ? 'domingo' : 'seg-sab' ) );
                ?>
                <tr class="sz-mb-cep-row" data-search="<?php echo esc_attr( strtolower( $cep->zona_nome . ' ' . $cep->cd_nome . ' ' . $cep->cep_inicio . ' ' . $cep->cep_fim ) ); ?>" data-zone="<?php echo esc_attr( strtolower( $cep->zona_nome ) ); ?>" data-op="<?php echo esc_attr( $op_key ); ?>" data-start="<?php echo esc_attr( $cep->cep_inicio ); ?>" data-end="<?php echo esc_attr( $cep->cep_fim ); ?>" data-label="<?php echo esc_attr( $dias_label ); ?>">
                    <td>
                        <strong><?php echo esc_html($cep->zona_nome); ?></strong><br>
                        <span class="sz-mb-soft"><?php echo esc_html($cep->cd_nome); ?></span>
                    </td>
                    <td><span class="sz-mb-cep-view"><?php echo esc_html($cep->cep_inicio); ?></span></td>
                    <td><span class="sz-mb-cep-view"><?php echo esc_html($cep->cep_fim); ?></span></td>
                    <td><span class="sz-mb-op-badge">🗓️ <?php echo esc_html( $dias_label ); ?></span><br><small class="sz-mb-soft">⏰ <?php echo esc_html( $cutoff_label ); ?></small></td>
                    <td>
                        <div class="sz-mb-actions sz-mb-cep-view">
                            <button type="button" class="button sz-edit-cep">✏️ Editar</button>
                            <a href="<?php echo esc_url( wp_nonce_url( add_query_arg( array_merge( $base_page_args, [ 'delete_cep' => $cep->id ] ), admin_url('admin.php') ), 'sz_mb_delete_cep' ) ); ?>" class="sz-mb-danger" onclick="return confirm('Remover esta faixa de CEP?')">🗑️ Remover faixa</a>
                            <a href="<?php echo esc_url( wp_nonce_url( add_query_arg( array_merge( $base_page_args, [ 'delete_zona' => $cep->zona_id ] ), admin_url('admin.php') ), 'sz_mb_delete_zona' ) ); ?>" class="sz-mb-danger" onclick="return confirm('Remover esta zona? As faixas de CEP vinculadas também serão removidas.')">🗑️ Remover zona</a>
                        </div>
                        <form method="post" class="sz-mb-form sz-mb-cep-edit">
                            <?php wp_nonce_field('sz_mb_zona_save_update_cep'); ?>
                            <input type="hidden" name="acao" value="update_cep">
                            <input type="hidden" name="cep_id" value="<?php echo (int) $cep->id; ?>">
                            <input type="hidden" name="cep_zona_id" value="<?php echo (int) $cep->zona_id; ?>">
                            <div><label>CEP início</label><input type="text" name="cep_inicio" value="<?php echo esc_attr( $cep->cep_inicio ); ?>" maxlength="9" required></div>
                            <div><label>CEP fim</label><input type="text" name="cep_fim" value="<?php echo esc_attr( $cep->cep_fim ); ?>" maxlength="9" required></div>
                            <div class="sz-mb-weekdays sz-mb-weekdays-edit">
                                <?php foreach ( [1=>'Seg',2=>'Ter',3=>'Qua',4=>'Qui',5=>'Sex',6=>'Sáb',0=>'Dom'] as $day_num => $day_label ) : ?>
                                    <label><input type="checkbox" name="zona_dias[]" value="<?php echo esc_attr( $day_num ); ?>" <?php checked( in_array( (int) $day_num, $dias_cep, true ) ); ?>> <?php echo esc_html( $day_label ); ?></label>
                                <?php endforeach; ?>
                            </div>
                            <div class="sz-mb-cutoff-single-edit">
                                <label>Horário limite único<input type="time" name="zona_cutoff_time" value="<?php echo esc_attr( $single_cutoff ); ?>" required></label>
                                <small class="sz-mb-soft">Vale para todos os dias selecionados da zona.</small>
                            </div>
                            <span class="sz-mb-actions"><button class="sz-mb-btn sz-mb-btn-sm">Salvar CEP e dias</button><button type="button" class="button sz-cancel-cep">Cancelar</button></span>
                        </form>
                    </td>
                </tr>
            <?php endforeach; ?>
            </tbody>
        </table>

        <div class="sz-mb-preview">
            <div class="sz-mb-form"><label>Preview de cobertura por CEP</label><input type="text" id="szCepPreviewInput" placeholder="Digite um CEP para testar" maxlength="9"></div>
            <button type="button" class="sz-mb-btn" id="szCepPreviewBtn">Verificar</button>
        </div>
        <div class="sz-mb-preview-result" id="szCepPreviewResult">Digite um CEP para ver zona, operação e cobertura.</div>
    </div>


    <div class="sz-mb-card">
        <h2>CDs Cadastrados</h2>
        <table class="sz-mb-table">
            <thead><tr><th>Nome</th><th>Cidade/UF</th><th>Zonas</th></tr></thead>
            <tbody>
            <?php foreach ( $cds as $cd ) :
                $qtd_zonas = count(array_filter($zonas, fn($z) => $z->cd_id == $cd->id));
            ?>
                <tr>
                    <td><strong><?php echo esc_html($cd->nome); ?></strong></td>
                    <td><?php echo esc_html($cd->cidade . '/' . $cd->uf); ?></td>
                    <td><?php echo $qtd_zonas; ?> zona(s)</td>
                </tr>
            <?php endforeach; ?>
            </tbody>
        </table>
    </div>
    <script>
    (function(){
        const rows = Array.from(document.querySelectorAll('#szCepTable .sz-mb-cep-row'));
        const search = document.getElementById('szCepSearch');
        const op = document.getElementById('szCepOperationFilter');
        const zone = document.getElementById('szCepZoneFilter');
        function normalizeCep(v){ return String(v||'').replace(/\D/g,'').slice(0,8); }
        function applyFilters(){
            const q = (search && search.value || '').toLowerCase().trim();
            const opv = op && op.value || '';
            const zv = zone && zone.value || '';
            rows.forEach(function(row){
                const ok = (!q || row.dataset.search.indexOf(q) >= 0) && (!opv || row.dataset.op === opv) && (!zv || row.dataset.zone === zv);
                row.style.display = ok ? '' : 'none';
            });
        }
        [search,op,zone].forEach(function(el){ if(el) el.addEventListener('input', applyFilters); if(el) el.addEventListener('change', applyFilters); });
        document.querySelectorAll('.sz-edit-cep').forEach(function(btn){
            btn.addEventListener('click', function(){ btn.closest('.sz-mb-cep-row').classList.add('is-editing'); });
        });
        document.querySelectorAll('.sz-cancel-cep').forEach(function(btn){
            btn.addEventListener('click', function(){ btn.closest('.sz-mb-cep-row').classList.remove('is-editing'); });
        });
        const previewInput = document.getElementById('szCepPreviewInput');
        const previewBtn = document.getElementById('szCepPreviewBtn');
        const previewResult = document.getElementById('szCepPreviewResult');
        function preview(){
            const cep = normalizeCep(previewInput && previewInput.value);
            if(!previewResult) return;
            if(cep.length !== 8){ previewResult.textContent = 'Informe um CEP com 8 dígitos.'; return; }
            const found = rows.find(function(row){ return Number(cep) >= Number(row.dataset.start) && Number(cep) <= Number(row.dataset.end); });
            if(!found){ previewResult.innerHTML = '❌ CEP fora das faixas cadastradas.'; return; }
            const zona = found.querySelector('td strong') ? found.querySelector('td strong').textContent : found.dataset.zone;
            previewResult.innerHTML = '✅ <strong>' + cep + '</strong> está em <strong>' + zona + '</strong><br>Operação: <span class="sz-mb-op-badge">🗓️ ' + (found.dataset.label || '-') + '</span>';
        }
        if(previewBtn) previewBtn.addEventListener('click', preview);
        if(previewInput) previewInput.addEventListener('keydown', function(e){ if(e.key === 'Enter'){ e.preventDefault(); preview(); } });
    })();
    </script>
    <?php
}

// ── Configurações ─────────────────────────────────────────────────────────────
function sz_mb_tab_config(): void {
    if ( $_SERVER['REQUEST_METHOD'] === 'POST' ) {
        check_admin_referer('sz_mb_config_save');
        update_option( 'sz_motoboy_geofence_metros', (int) $_POST['geofence_metros'] );
        update_option( 'sz_motoboy_horario_inicio', sanitize_text_field($_POST['horario_inicio']) );
        update_option( 'sz_motoboy_horario_fim',    sanitize_text_field($_POST['horario_fim']) );
        update_option( 'sz_motoboy_cc_fee_pct', max( 0, min( 30, (float) str_replace(',', '.', sanitize_text_field( $_POST['cc_fee_pct'] ?? '0' ) ) ) ) );
        wp_safe_redirect( add_query_arg(['page'=>'senderzz','area'=>'cod','tab'=>'cod-motoboys','mb_tab'=>'config','msg'=>'saved'], admin_url('admin.php')) );
        exit;
    }
    ?>
    <div class="sz-mb-card" style="max-width:600px">
        <h2>Configurações do Módulo Motoboy</h2>
        <form method="post" class="sz-mb-form">
            <?php wp_nonce_field('sz_mb_config_save'); ?>
            <div class="sz-mb-soft" style="margin-bottom:14px">Taxas de entrega, pagamento de motoboy e regras de frustrado ficam centralizadas em <strong>Financeiro COD &gt; Taxas de Entrega</strong>.</div>

            <label>Geofence "A Caminho" (metros)</label>
            <input type="number" name="geofence_metros" value="<?php echo (int) get_option('sz_motoboy_geofence_metros', 500); ?>">

            <label>Horário início operação</label>
            <input type="time" name="horario_inicio" value="<?php echo esc_attr(get_option('sz_motoboy_horario_inicio','08:00')); ?>">

            <label>Horário fim operação</label>
            <input type="time" name="horario_fim" value="<?php echo esc_attr(get_option('sz_motoboy_horario_fim','18:00')); ?>">

            <label>Taxa de cartão de crédito (%) — exibida como segundo valor na etiqueta e na tela do motoboy</label>
            <input type="number" name="cc_fee_pct" min="0" max="30" step="0.01" value="<?php echo esc_attr( number_format( (float) get_option('sz_motoboy_cc_fee_pct', 0), 2, '.', '' ) ); ?>" placeholder="Ex: 3.99">
            <small style="color:#6b7280;display:block;margin-top:4px">Deixe 0 para não exibir segundo valor. O valor cobrado no cartão = valor do pedido × (1 + taxa/100).</small>

            <br><br>
            <button class="sz-mb-btn">Salvar Configurações</button>
        </form>
    </div>
    <?php
}

// ─── Aba: Etiquetas ───────────────────────────────────────────────────────────
function sz_mb_tab_etiquetas(): void {
    global $wpdb;
    $p      = $wpdb->prefix;
    $data   = sanitize_text_field( $_GET['data_etiq'] ?? ( new DateTimeImmutable( 'now', new DateTimeZone( 'America/Sao_Paulo' ) ) )->format( 'Y-m-d' ) );
    $status = sanitize_text_field( $_GET['st_etiq']   ?? 'embalado' );

    $pedidos = $wpdb->get_results( $wpdb->prepare(
        "SELECT mp.id, mp.wc_order_id, mp.status,
                mp.dest_nome, mp.dest_telefone, mp.dest_cep,
                mp.dest_endereco, mp.dest_numero, mp.dest_complemento,
                mp.dest_bairro, mp.dest_cidade, mp.dest_uf,
                mp.valor_pedido,
                mp.pgto_dinheiro, mp.pgto_pix, mp.pgto_cartao,
                m.nome AS motoboy_nome, z.nome AS zona_nome
           FROM {$p}sz_motoboy_pedidos mp
           LEFT JOIN {$p}sz_motoboys m ON m.id = mp.motoboy_id
           LEFT JOIN {$p}sz_motoboy_zonas z ON z.id = mp.zona_id
          WHERE DATE(mp.created_at) = %s AND mp.status = %s
          ORDER BY mp.motoboy_id, mp.id",
        $data, $status
    ) );
    ?>
    <div class="sz-mb-card" style="margin-bottom:16px">
        <form method="get" style="display:flex;gap:10px;align-items:center;flex-wrap:wrap">
            <input type="hidden" name="page" value="senderzz"><input type="hidden" name="area" value="cod"><input type="hidden" name="tab" value="cod-motoboys">
            <input type="hidden" name="mb_tab" value="etiquetas">
            <label style="font-size:var(--sz-text-base);font-weight:600">Data:</label>
            <input type="date" name="data_etiq" value="<?php echo esc_attr($data); ?>" style="padding:6px 10px;border:1px solid #d1d5db;border-radius:8px;font-size:var(--sz-text-base)">
            <label style="font-size:var(--sz-text-base);font-weight:600">Status:</label>
            <select name="st_etiq" style="padding:6px 10px;border:1px solid #d1d5db;border-radius:8px;font-size:var(--sz-text-base)">
                <?php foreach(['agendado'=>'Agendados','embalado'=>'Embalados','em_rota'=>'Em rota'] as $k=>$l): ?>
                    <option value="<?php echo $k ?>" <?php selected($k,$status) ?>><?php echo $l ?></option>
                <?php endforeach; ?>
            </select>
            <button class="sz-mb-btn" type="submit">Filtrar</button>
            <?php if($pedidos): ?>
            <button class="sz-mb-btn" type="button" onclick="window.print()">🖨️ Imprimir etiquetas</button>
            <?php endif; ?>
        </form>
    </div>

    <style>
    @media print {
        body * { visibility:hidden }
        #sz-etiquetas-print, #sz-etiquetas-print * { visibility:visible }
        #sz-etiquetas-print { position:fixed;top:0;left:0;width:100% }
        .sz-etiq { page-break-inside:avoid; border:1.5px solid #000!important; }
        .sz-mb-card:not(#sz-etiquetas-print) { display:none }
    }
    .sz-etiq-grid { display:grid; grid-template-columns:repeat(auto-fill,minmax(280px,1fr)); gap:14px; }
    .sz-etiq {
        border:1.5px solid #374151; border-radius:10px; padding:14px 16px;
        font-size:var(--sz-text-meta); font-family:var(--sz-font); background:#fff; color:#111;
        position:relative;
    }
    .sz-etiq-header { display:flex; justify-content:space-between; align-items:flex-start; margin-bottom:8px; }
    .sz-etiq-num { font-size:var(--sz-text-lg); font-weight:700; color:#E8650A; }
    .sz-etiq-mb { font-size:var(--sz-text-sm); background:#f3f4f6; padding:3px 8px; border-radius:6px; color:#374151; }
    .sz-etiq-dest { font-size:var(--sz-text-base); font-weight:700; margin-bottom:4px; }
    .sz-etiq-addr { color:#374151; margin-bottom:8px; line-height:1.4; }
    .sz-etiq-footer { display:flex; justify-content:space-between; border-top:1px dashed #d1d5db; padding-top:8px; margin-top:6px; }
    .sz-etiq-val { font-weight:700; font-size:var(--sz-text-base); }
    .sz-etiq-pgto { font-size:var(--sz-text-sm); color:#6b7280; }
    .sz-etiq-cep { position:absolute; bottom:14px; right:16px; font-size:var(--sz-text-xs); color:#9ca3af; }
    .sz-etiq-qr-wrap { display:flex; align-items:center; gap:10px; margin-top:10px; border-top:1px dashed #d1d5db; padding-top:8px; }
    .sz-etiq-qr-wrap img { width:64px; height:64px; image-rendering:pixelated; }
    .sz-etiq-qr-code { font-size:10px; line-height:1.35; word-break:break-all; color:#111827; }

    @media(max-width:1180px){.sz-mb-custody-kpis{grid-template-columns:repeat(3,minmax(0,1fr))}.sz-mb-custody-form-route,.sz-mb-custody-form-return{grid-template-columns:1fr 1fr}}
    @media(max-width:720px){.sz-mb-custody-kpis{grid-template-columns:1fr 1fr}.sz-mb-custody-form-route,.sz-mb-custody-form-return{grid-template-columns:1fr}.sz-mb-section-title{align-items:flex-start}.sz-mb-accordion summary{align-items:flex-start}.sz-mb-kpi-card{min-height:84px}}

    </style>

    <div id="sz-etiquetas-print">
    <?php if ( ! $pedidos ): ?>
        <div class="sz-mb-card"><p style="color:#6b7280;text-align:center">Nenhum pedido <?php echo esc_html($status); ?> em <?php echo esc_html($data); ?>.</p></div>
    <?php else: ?>
        <div style="margin-bottom:12px;font-size:var(--sz-text-base);color:#6b7280">
            <?php echo count($pedidos); ?> etiqueta(s) — <?php echo esc_html(date('d/m/Y', strtotime($data))); ?>
        </div>
        <div class="sz-etiq-grid">
        <?php foreach ( $pedidos as $ped ):
            $pgto_parts = [];
            if ($ped->pgto_dinheiro > 0) $pgto_parts[] = 'Dinheiro R$'.number_format($ped->pgto_dinheiro,2,',','.');
            if ($ped->pgto_pix > 0)      $pgto_parts[] = 'PIX R$'.number_format($ped->pgto_pix,2,',','.');
            if ($ped->pgto_cartao > 0)   $pgto_parts[] = 'Cartão R$'.number_format($ped->pgto_cartao,2,',','.');
            $pgto_str = $pgto_parts ? implode(' + ', $pgto_parts) : 'A cobrar';
            $package_code = function_exists( 'sz_mbc_package_code' ) ? sz_mbc_package_code( (int) $ped->id, (int) $ped->wc_order_id ) : ( 'SZ-' . (int) $ped->wc_order_id . '-' . (int) $ped->id );
            $qr_url = 'https://api.qrserver.com/v1/create-qr-code/?size=160x160&data=' . rawurlencode( $package_code );
        ?>
        <div class="sz-etiq">
            <div class="sz-etiq-header">
                <div class="sz-etiq-num">#<?php echo $ped->wc_order_id; ?></div>
                <div class="sz-etiq-mb"><?php echo esc_html($ped->motoboy_nome ?: '—'); ?> · <?php echo esc_html($ped->zona_nome ?: ''); ?></div>
            </div>
            <div class="sz-etiq-dest"><?php echo esc_html($ped->dest_nome); ?></div>
            <div class="sz-etiq-addr">
                <?php echo esc_html($ped->dest_endereco . ', ' . $ped->dest_numero); ?>
                <?php if($ped->dest_complemento) echo ' — ' . esc_html($ped->dest_complemento); ?><br>
                <?php echo esc_html($ped->dest_bairro); ?><?php if($ped->dest_bairro) echo ' · '; ?><?php echo esc_html($ped->dest_cidade . '/' . $ped->dest_uf); ?>
            </div>
            <div class="sz-etiq-footer">
                <div>
                    <div class="sz-etiq-val">R$ <?php echo number_format($ped->valor_pedido,2,',','.'); ?></div>
                    <div class="sz-etiq-pgto"><?php echo esc_html($pgto_str); ?></div>
                </div>
                <?php if($ped->dest_telefone): ?>
                <div style="font-size:var(--sz-text-sm);color:#6b7280;align-self:flex-end"><?php echo esc_html($ped->dest_telefone); ?></div>
                <?php endif; ?>
            </div>
            <div class="sz-etiq-qr-wrap">
                <img src="<?php echo esc_url( $qr_url ); ?>" alt="QR Code do pacote">
                <div>
                    <strong>QR ROTA / DEVOLUÇÃO</strong>
                    <div class="sz-etiq-qr-code"><?php echo esc_html( $package_code ); ?></div>
                </div>
            </div>
            <div class="sz-etiq-cep">CEP <?php echo esc_html(substr($ped->dest_cep,0,5).'-'.substr($ped->dest_cep,5)); ?></div>
        </div>
        <?php endforeach; ?>
        </div>
    <?php endif; ?>
    </div>
    <?php
}

// ─── Aba: Estoque Motoboy / Custódia ─────────────────────────────────────────
function sz_mb_tab_estoque_motoboy(): void {
    global $wpdb;
    if ( function_exists( 'sz_mbc_install' ) ) sz_mbc_install();
    $p = $wpdb->prefix;
    $custody_table = $p . 'sz_motoboy_stock_custody';
    $motoboy_table = $p . 'sz_motoboys';
    if ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $custody_table ) ) !== $custody_table ) {
        echo '<div class="sz-mb-card"><p>Controle de custódia ainda não inicializado.</p></div>';
        return;
    }

    if ( $_SERVER['REQUEST_METHOD'] === 'POST' && isset( $_POST['sz_mb_custody_route_assist'] ) ) {
        check_admin_referer( 'sz_mb_custody_route_assist' );
        $qr_code    = sanitize_text_field( wp_unslash( $_POST['route_package_code'] ?? '' ) );
        $motoboy_id = absint( $_POST['route_motoboy_id'] ?? 0 );
        $result = ( $motoboy_id > 0 && function_exists( 'sz_mbc_start_route_by_qr' ) )
            ? sz_mbc_start_route_by_qr( $motoboy_id, $qr_code, 'admin_qr_assist', get_current_user_id() )
            : new WP_Error( 'sz_mbc_missing', 'Informe motoboy e QR válidos.' );
        $msg = is_wp_error( $result ) ? 'error' : 'route_assisted';
        wp_safe_redirect( add_query_arg( [ 'page'=>'senderzz', 'area'=>'cod', 'tab'=>'cod-pedidos', 'mb_tab'=>'estoque', 'msg'=>$msg ], admin_url( 'admin.php' ) ) );
        exit;
    }

    if ( $_SERVER['REQUEST_METHOD'] === 'POST' && isset( $_POST['sz_mb_custody_return'] ) ) {
        check_admin_referer( 'sz_mb_custody_return' );
        $qr_code   = sanitize_text_field( wp_unslash( $_POST['package_code'] ?? '' ) );
        $condition = sanitize_key( $_POST['condition'] ?? 'vendavel' );
        $note      = sanitize_textarea_field( wp_unslash( $_POST['occurrence_note'] ?? '' ) );
        $photos    = [];
        if ( ! empty( $_FILES['occurrence_photo']['name'] ) ) {
            require_once ABSPATH . 'wp-admin/includes/file.php';
            $uploaded = wp_handle_upload( $_FILES['occurrence_photo'], [ 'test_form' => false ] );
            if ( empty( $uploaded['error'] ) && ! empty( $uploaded['url'] ) ) {
                $photos[] = esc_url_raw( $uploaded['url'] );
            }
        }
        $result = function_exists( 'sz_mbc_return_by_qr' ) ? sz_mbc_return_by_qr( $qr_code, $condition, $note, $photos, 'admin', get_current_user_id() ) : new WP_Error( 'sz_mbc_missing', 'Controle de custódia indisponível.' );
        $msg = 'error';
        if ( is_wp_error( $result ) ) {
            $code = $result->get_error_code();
            $msg = in_array( $code, [ 'sz_mbc_photo_required', 'sz_mbc_note_required' ], true ) ? 'photo_required' : 'error';
        } else {
            $msg = in_array( $condition, [ 'vendavel', 'ok' ], true ) ? 'custody_returned' : 'custody_damaged';
        }
        wp_safe_redirect( add_query_arg( [ 'page'=>'senderzz', 'area'=>'cod', 'tab'=>'cod-pedidos', 'mb_tab'=>'estoque', 'msg'=>$msg ], admin_url( 'admin.php' ) ) );
        exit;
    }

    $motoboys = $wpdb->get_results( "SELECT id,nome FROM {$motoboy_table} WHERE ativo=1 ORDER BY nome ASC" );
    if ( empty( $motoboys ) ) {
        $motoboys = $wpdb->get_results( "SELECT id,nome FROM {$motoboy_table} ORDER BY nome ASC" );
    }

    $totals = $wpdb->get_results( "SELECT physical_status, COUNT(DISTINCT pedido_id) AS pedidos, COALESCE(SUM(quantity),0) AS unidades FROM {$custody_table} GROUP BY physical_status", OBJECT_K );
    $qty = static function( string $status ) use ( $totals ): int { return isset( $totals[ $status ] ) ? (int) $totals[ $status ]->unidades : 0; };
    $ped = static function( string $status ) use ( $totals ): int { return isset( $totals[ $status ] ) ? (int) $totals[ $status ]->pedidos : 0; };

    $summary = $wpdb->get_results(
        "SELECT COALESCE(m.nome,'Sem motoboy') AS motoboy_nome,
                c.motoboy_id,
                COUNT(DISTINCT c.pedido_id) AS pedidos,
                COALESCE(SUM(c.quantity),0) AS unidades,
                COALESCE(SUM(CASE WHEN c.physical_status='frustrated' THEN c.quantity ELSE 0 END),0) AS frustrados,
                COALESCE(SUM(CASE WHEN c.physical_status='return_declared' THEN c.quantity ELSE 0 END),0) AS aguardando_ol,
                COALESCE(SUM(c.quantity * c.cost_product),0) AS valor_custo,
                MAX(c.updated_at) AS ultima_movimentacao
           FROM {$custody_table} c
           LEFT JOIN {$motoboy_table} m ON m.id = c.motoboy_id
          WHERE c.physical_status IN ('with_motoboy','frustrated','return_declared')
          GROUP BY c.motoboy_id, m.nome
          ORDER BY unidades DESC, motoboy_nome ASC"
    );

    $rows = $wpdb->get_results(
        "SELECT c.*, m.nome AS motoboy_nome
           FROM {$custody_table} c
           LEFT JOIN {$motoboy_table} m ON m.id = c.motoboy_id
          WHERE c.physical_status IN ('with_motoboy','frustrated','return_declared','damaged')
          ORDER BY c.updated_at DESC
          LIMIT 300"
    );

    $label_map = [ 'with_motoboy'=>'Rota', 'frustrated'=>'Frustrado', 'return_declared'=>'Aguardando OL', 'damaged'=>'Avariado', 'available'=>'Disponível', 'reserved'=>'Reservado', 'delivered'=>'Entregue' ];
    ?>
    <div class="sz-mb-section-title">
        <div>
            <h2>Estoque Motoboy</h2>
            <p class="sz-mb-soft">Custódia física por QR: saída pelo motoboy, devolução declarada pelo motoboy e confirmação final pelo OL.</p>
        </div>
    </div>

    <div class="sz-mb-custody-kpis">
        <div class="sz-mb-kpi-card">
            <span>Com motoboy</span>
            <strong><?php echo (int) $qty('with_motoboy'); ?></strong>
            <small><?php echo (int) $ped('with_motoboy'); ?> pedido(s)</small>
        </div>
        <div class="sz-mb-kpi-card">
            <span>Frustrados</span>
            <strong><?php echo (int) $qty('frustrated'); ?></strong>
            <small>pendentes de bipagem</small>
        </div>
        <div class="sz-mb-kpi-card">
            <span>Aguardando OL</span>
            <strong><?php echo (int) $qty('return_declared'); ?></strong>
            <small>devolução declarada</small>
        </div>
        <div class="sz-mb-kpi-card">
            <span>Avariados</span>
            <strong><?php echo (int) $qty('damaged'); ?></strong>
            <small>perda operacional</small>
        </div>
        <div class="sz-mb-kpi-card">
            <span>Reservados</span>
            <strong><?php echo (int) $qty('reserved'); ?></strong>
            <small>no CD para pedido</small>
        </div>
    </div>

    <div class="sz-mb-section-title">
        <div>
            <h2>Ações operacionais</h2>
            <p class="sz-mb-soft">Menus recolhíveis para evitar poluir a tela. Abra somente quando precisar operar.</p>
        </div>
    </div>

    <details class="sz-mb-accordion">
        <summary>
            <span>
                <strong>Rota assistida por QR</strong>
                <small>Use apenas se o celular do motoboy falhar.</small>
            </span>
            <em>abrir</em>
        </summary>
        <div class="sz-mb-accordion-body">
            <p class="sz-mb-soft">O pedido continua exigindo QR da etiqueta e motoboy responsável.</p>
            <form method="post" class="sz-mb-form sz-mb-custody-form sz-mb-custody-form-route">
                <?php wp_nonce_field( 'sz_mb_custody_route_assist' ); ?>
                <input type="hidden" name="sz_mb_custody_route_assist" value="1">
                <label>QR / Código do pacote<br><input type="text" name="route_package_code" placeholder="SZ-1234-55-..." required></label>
                <label>Motoboy<br><select name="route_motoboy_id" required><option value="">Selecione</option><?php foreach ( (array) $motoboys as $mb_item ) : ?><option value="<?php echo (int) $mb_item->id; ?>"><?php echo esc_html( $mb_item->nome ); ?></option><?php endforeach; ?></select></label>
                <button class="sz-mb-btn" type="submit">Iniciar por QR</button>
            </form>
        </div>
    </details>

    <details class="sz-mb-accordion" open>
        <summary>
            <span>
                <strong>Confirmar devolução / condição</strong>
                <small>Após o motoboy declarar devolução no PWA.</small>
            </span>
            <em>abrir</em>
        </summary>
        <div class="sz-mb-accordion-body">
            <p class="sz-mb-soft">Vendável volta ao estoque. Avariado/Extravio/Perda/Violado/Divergente exige foto e relato.</p>
            <form method="post" enctype="multipart/form-data" class="sz-mb-form sz-mb-custody-form sz-mb-custody-form-return">
                <?php wp_nonce_field( 'sz_mb_custody_return' ); ?>
                <input type="hidden" name="sz_mb_custody_return" value="1">
                <label>QR / Código do pacote<br><input type="text" name="package_code" placeholder="SZ-1234-55-..." required></label>
                <label>Condição<br><select name="condition"><option value="vendavel">Vendável</option><option value="avariado">Avariado</option><option value="extravio">Extravio</option><option value="perda">Perda</option><option value="violado">Violado</option><option value="divergente">Divergente</option></select></label>
                <label>Relato do OL<br><textarea name="occurrence_note" rows="2" placeholder="Obrigatório para avariado/extravio/perda"></textarea></label>
                <label>Foto/evidência<br><input type="file" name="occurrence_photo" accept="image/*"></label>
                <button class="sz-mb-btn" type="submit">Registrar</button>
            </form>
        </div>
    </details>

    <div class="sz-mb-section-title">
        <div>
            <h2>Consulta e auditoria</h2>
            <p class="sz-mb-soft">Visão por motoboy e histórico de pacotes em custódia.</p>
        </div>
    </div>

    <details class="sz-mb-accordion" open>
        <summary>
            <span>
                <strong>Resumo por motoboy</strong>
                <small>Produtos e custos atualmente em custódia.</small>
            </span>
            <em>abrir</em>
        </summary>
        <div class="sz-mb-accordion-body">
            <div class="sz-mb-table-scroll">
                <table class="widefat striped">
                    <thead><tr><th>Motoboy</th><th>Pedidos</th><th>Unidades</th><th>Frustrados</th><th>Aguardando OL</th><th>Custo em custódia</th><th>Última movimentação</th></tr></thead>
                    <tbody>
                    <?php if ( empty( $summary ) ) : ?>
                        <tr><td colspan="7">Nenhum produto em custódia de motoboy.</td></tr>
                    <?php else : foreach ( $summary as $r ) : ?>
                        <tr>
                            <td><?php echo esc_html( $r->motoboy_nome ); ?></td>
                            <td><?php echo (int) $r->pedidos; ?></td>
                            <td><?php echo (int) $r->unidades; ?></td>
                            <td><?php echo (int) $r->frustrados; ?></td>
                            <td><?php echo (int) $r->aguardando_ol; ?></td>
                            <td>R$ <?php echo number_format( (float) $r->valor_custo, 2, ',', '.' ); ?></td>
                            <td><?php echo esc_html( $r->ultima_movimentacao ? date_i18n( 'd/m/Y H:i', strtotime( $r->ultima_movimentacao ) ) : '—' ); ?></td>
                        </tr>
                    <?php endforeach; endif; ?>
                    </tbody>
                </table>
            </div>
        </div>
    </details>

    <details class="sz-mb-accordion">
        <summary>
            <span>
                <strong>Pacotes em custódia / ocorrência</strong>
                <small>Lista detalhada dos pacotes em rota, frustrados, aguardando OL ou avariados.</small>
            </span>
            <em>abrir</em>
        </summary>
        <div class="sz-mb-accordion-body">
            <div class="sz-mb-table-scroll">
                <table class="widefat striped">
                    <thead><tr><th>Pedido</th><th>Status</th><th>Motoboy</th><th>Produto</th><th>Qtd.</th><th>QR</th><th>Ocorrência</th><th>Atualizado</th></tr></thead>
                    <tbody>
                    <?php if ( empty( $rows ) ) : ?>
                        <tr><td colspan="8">Nenhum pacote em rota, frustrado, aguardando OL ou avariado.</td></tr>
                    <?php else : foreach ( $rows as $r ) :
                        $photos = json_decode( (string) $r->occurrence_photos, true );
                        $photos = is_array( $photos ) ? $photos : [];
                    ?>
                        <tr>
                            <td>#<?php echo (int) $r->wc_order_id; ?></td>
                            <td><strong><?php echo esc_html( $label_map[ (string) $r->physical_status ] ?? $r->physical_status ); ?></strong></td>
                            <td><?php echo esc_html( $r->motoboy_nome ?: '—' ); ?></td>
                            <td><?php echo esc_html( $r->product_name ?: $r->sku ?: 'Produto' ); ?></td>
                            <td><?php echo (int) $r->quantity; ?></td>
                            <td><code><?php echo esc_html( $r->package_code ); ?></code></td>
                            <td>
                                <?php if ( $r->occurrence_type ) : ?>
                                    <?php echo esc_html( ucfirst( (string) $r->occurrence_type ) ); ?><?php if ( $r->occurrence_note ) : ?> — <?php echo esc_html( wp_trim_words( (string) $r->occurrence_note, 12 ) ); ?><?php endif; ?>
                                    <?php foreach ( $photos as $url ) : ?> <a href="<?php echo esc_url( $url ); ?>" target="_blank" rel="noopener">foto</a><?php endforeach; ?>
                                <?php else : ?>—<?php endif; ?>
                            </td>
                            <td><?php echo esc_html( $r->updated_at ? date_i18n( 'd/m/Y H:i', strtotime( $r->updated_at ) ) : '—' ); ?></td>
                        </tr>
                    <?php endforeach; endif; ?>
                    </tbody>
                </table>
            </div>
        </div>
    </details>
    <?php
}

// ─── Aba: Mapa ao vivo ────────────────────────────────────────────────────────
function sz_mb_tab_mapa(): void {
    $rest_url = esc_js( rest_url('sz-motoboy/v1/alan/localizacao') );
    $nonce    = wp_create_nonce('wp_rest');
    ?>
    <div class="sz-mb-card" style="padding:0;overflow:hidden">
        <div style="display:flex;justify-content:space-between;align-items:center;padding:14px 18px;border-bottom:1px solid #f3f4f6">
            <span style="font-size:var(--sz-text-md);font-weight:700;color:#111827">Motoboys em operação</span>
            <button class="sz-mb-btn sz-mb-btn-sm" onclick="szMbMapaRefresh()">↺ Atualizar</button>
        </div>
        <div id="sz-mb-mapa" style="height:480px;width:100%"></div>
        <div id="sz-mb-mapa-lista" style="padding:14px 18px;border-top:1px solid #f3f4f6;display:flex;gap:10px;flex-wrap:wrap"></div>
    </div>

    <link rel="stylesheet" href="https://unpkg.com/leaflet@1.9.4/dist/leaflet.css"/>
    <script src="https://unpkg.com/leaflet@1.9.4/dist/leaflet.js"></script>
    <script>
    let szMbMap = null, szMbMarkers = {};

    function szMbMapaInit() {
        szMbMap = L.map('sz-mb-mapa').setView([-23.55, -46.63], 11);
        L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
            attribution: '© OpenStreetMap'
        }).addTo(szMbMap);
        szMbMapaRefresh();
        setInterval(szMbMapaRefresh, 30000);
    }

    async function szMbMapaRefresh() {
        const r = await fetch('<?php echo $rest_url; ?>', {
            headers: { 'X-WP-Nonce': '<?php echo $nonce; ?>' }
        });
        const d = await r.json();
        if (!d.ok) return;

        const lista = document.getElementById('sz-mb-mapa-lista');
        lista.innerHTML = '';

        d.motoboys.forEach(mb => {
            const pill = document.createElement('div');
            const ativo = mb.ultimo_lat && mb.ultimo_ping &&
                (Date.now() - new Date(mb.ultimo_ping + ' UTC').getTime()) < 300000;
            pill.style.cssText = 'padding:6px 12px;border-radius:999px;font-size:var(--sz-text-meta);font-weight:700;background:' +
                (ativo ? '#dcfce7;color:#166534' : '#f3f4f6;color:#6b7280');
            const mins = mb.ultimo_ping
                ? Math.round((Date.now() - new Date(mb.ultimo_ping + ' UTC').getTime()) / 60000) : null;
            pill.textContent = mb.nome + ' · ' + (mb.pedidos_abertos||0) + ' pedidos' +
                (mins !== null ? ' · ' + (mins < 1 ? 'agora' : mins + 'min') : '');
            lista.appendChild(pill);

            if (!mb.ultimo_lat || !mb.ultimo_lng) return;
            const lat = parseFloat(mb.ultimo_lat), lng = parseFloat(mb.ultimo_lng);

            const icon = L.divIcon({
                html: '<div style="background:#E8650A;color:#fff;padding:4px 8px;border-radius:8px;font-size:var(--sz-text-sm);font-weight:700;white-space:nowrap;box-shadow:0 2px 6px rgba(0,0,0,.3)">' + mb.nome + '</div>',
                className: '', iconAnchor: [0,0]
            });

            if (szMbMarkers[mb.id]) {
                szMbMarkers[mb.id].setLatLng([lat, lng]);
            } else {
                szMbMarkers[mb.id] = L.marker([lat, lng], {icon}).addTo(szMbMap);
                szMbMarkers[mb.id].bindPopup(
                    '<b>' + mb.nome + '</b><br>' + (mb.zona||'') + '<br>' +
                    mb.pedidos_abertos + ' pedidos abertos · ' + mb.entregues_hoje + ' entregues hoje'
                );
            }
        });
    }

    document.addEventListener('DOMContentLoaded', szMbMapaInit);
    </script>
    <?php
}


// ─── Aba: Carteiras dos motoboys ──────────────────────────────────────────────
function sz_mb_tab_carteiras(): void {
    global $wpdb;
    $p = $wpdb->prefix;

    // Garante suporte a pagamento parcial sem quebrar instalações antigas.
    $has_valor_pago = $wpdb->get_var( $wpdb->prepare( "SHOW COLUMNS FROM {$p}sz_motoboy_ganhos LIKE %s", 'valor_pago' ) );
    if ( ! $has_valor_pago ) {
        $wpdb->query( "ALTER TABLE {$p}sz_motoboy_ganhos ADD COLUMN valor_pago DECIMAL(8,2) NOT NULL DEFAULT 0.00 AFTER valor" );
    }

    if ( isset( $_POST['sz_mb_registrar_pagamento_valor'] ) ) {
        check_admin_referer( 'sz_mb_registrar_pagamento_valor' );
        $mid = absint( $_POST['motoboy_id'] ?? 0 );
        $valor_input = str_replace( [ '.', ',' ], [ '', '.' ], sanitize_text_field( $_POST['valor_pago'] ?? '0' ) );
        $valor_pago = round( max( 0, (float) $valor_input ), 2 );
        $data_pagamento = sanitize_text_field( $_POST['data_pagamento'] ?? current_time( 'Y-m-d' ) );
        if ( ! preg_match( '/^\d{4}-\d{2}-\d{2}$/', $data_pagamento ) ) { $data_pagamento = current_time( 'Y-m-d' ); }

        $saldo_disponivel = $mid > 0 ? (float) $wpdb->get_var( $wpdb->prepare(
            "SELECT COALESCE(SUM(GREATEST(valor-COALESCE(valor_pago,0),0)),0)
               FROM {$p}sz_motoboy_ganhos
              WHERE motoboy_id=%d AND status='disponivel'",
            $mid
        ) ) : 0.0;

        if ( $mid <= 0 || $valor_pago <= 0 || $valor_pago > round( $saldo_disponivel, 2 ) ) {
            echo '<div class="sz-mb-alert-err">⚠️ Informe um valor válido, menor ou igual ao disponível do motoboy.</div>';
        } else {
            $wpdb->insert( $p . 'sz_motoboy_pagamentos', [
                'motoboy_id'     => $mid,
                'valor_total'    => $valor_pago,
                'data_pagamento' => $data_pagamento,
                'status'         => 'pago',
                'obs'            => 'Pagamento parcial/manual registrado na carteira Senderzz.',
            ] );
            $pagamento_id = (int) $wpdb->insert_id;
            $restante = $valor_pago;
            $ganhos = $wpdb->get_results( $wpdb->prepare(
                "SELECT id, valor, COALESCE(valor_pago,0) AS valor_pago
                   FROM {$p}sz_motoboy_ganhos
                  WHERE motoboy_id=%d AND status='disponivel' AND GREATEST(valor-COALESCE(valor_pago,0),0) > 0
                  ORDER BY created_at ASC, id ASC",
                $mid
            ) ) ?: [];
            foreach ( $ganhos as $g ) {
                if ( $restante <= 0 ) { break; }
                $aberto = round( max( 0, (float) $g->valor - (float) $g->valor_pago ), 2 );
                if ( $aberto <= 0 ) { continue; }
                $aplicar = min( $aberto, $restante );
                $novo_pago = round( (float) $g->valor_pago + $aplicar, 2 );
                $novo_status = $novo_pago + 0.0001 >= (float) $g->valor ? 'pago' : 'disponivel';
                $wpdb->update(
                    $p . 'sz_motoboy_ganhos',
                    [ 'valor_pago' => $novo_pago, 'status' => $novo_status, 'pagamento_id' => $pagamento_id ],
                    [ 'id' => (int) $g->id ],
                    [ '%f', '%s', '%d' ],
                    [ '%d' ]
                );
                $restante = round( $restante - $aplicar, 2 );
            }
            echo '<div class="sz-mb-alert-ok">✅ Pagamento registrado. O valor pago foi lançado no extrato e o saldo restante continua disponível.</div>';
        }
    }

    $motoboys = $wpdb->get_results(
        "SELECT m.id, m.nome, m.telefone, m.ativo,
                z.nome AS zona_nome,
                COALESCE(SUM(CASE WHEN g.status='disponivel' THEN GREATEST(g.valor-COALESCE(g.valor_pago,0),0) ELSE 0 END),0) AS saldo_disponivel,
                COALESCE(SUM(CASE WHEN g.status='pendente' THEN g.valor ELSE 0 END),0) AS saldo_em_conferencia,
                COALESCE(SUM(COALESCE(g.valor_pago,0)),0) AS saques_pagos,
                COUNT(CASE WHEN g.status='disponivel' AND GREATEST(g.valor-COALESCE(g.valor_pago,0),0)>0 THEN 1 END) AS qtd_disponivel,
                COUNT(CASE WHEN g.status='pendente' THEN 1 END) AS qtd_conferencia,
                COUNT(CASE WHEN COALESCE(g.valor_pago,0)>0 THEN 1 END) AS qtd_pago,
                MAX(g.created_at) AS ultimo_lancamento
           FROM {$p}sz_motoboys m
      LEFT JOIN {$p}sz_motoboy_zonas z ON z.id = m.zona_id
      LEFT JOIN {$p}sz_motoboy_ganhos g ON g.motoboy_id = m.id
          WHERE m.ativo = 1
       GROUP BY m.id, m.nome, m.telefone, m.ativo, z.nome
       ORDER BY m.nome"
    ) ?: [];

    $total_disp = 0; $total_conf = 0; $total_pago = 0;
    foreach ( $motoboys as $m ) { $total_disp += (float) $m->saldo_disponivel; $total_conf += (float) $m->saldo_em_conferencia; $total_pago += (float) $m->saques_pagos; }
    ?>
    <style>
        .sz-mb-wallet-pay{display:flex;gap:8px;align-items:center;flex-wrap:wrap}.sz-mb-wallet-pay input{height:36px;border:1px solid #d1d5db;border-radius:10px;padding:0 10px;max-width:118px}.sz-mb-wallet-pay input[type=date]{max-width:145px}.sz-mb-wallet-note{font-size:var(--sz-text-meta);color:#64748b;margin:0 0 14px}

    @media(max-width:1180px){.sz-mb-custody-kpis{grid-template-columns:repeat(3,minmax(0,1fr))}.sz-mb-custody-form-route,.sz-mb-custody-form-return{grid-template-columns:1fr 1fr}}
    @media(max-width:720px){.sz-mb-custody-kpis{grid-template-columns:1fr 1fr}.sz-mb-custody-form-route,.sz-mb-custody-form-return{grid-template-columns:1fr}.sz-mb-section-title{align-items:flex-start}.sz-mb-accordion summary{align-items:flex-start}.sz-mb-kpi-card{min-height:84px}}

    </style>
    <div class="sz-mb-card">
        <form method="post" style="display:flex;gap:10px;align-items:center;justify-content:space-between;flex-wrap:wrap">
            <div>
                <h2 style="margin:0">Carteira dos Motoboys</h2>
                <div class="sz-mb-soft">Disponível = saldo já conciliado e ainda não pago. Pagamento pode ser parcial; cada lançamento fica registrado com data de saque.</div>
            </div>
            <?php wp_nonce_field( 'sz_mb_sync_wallets' ); ?>
            <button class="sz-mb-btn" name="sz_mb_sync_wallets" value="1">🔄 Atualizar dados dos motoboys</button>
        </form>
    </div>

    <div class="sz-mb-grid-3" style="margin-bottom:16px">
        <div class="sz-mb-stat"><div class="sz-mb-stat-num">R$ <?php echo number_format($total_disp,2,',','.'); ?></div><div class="sz-mb-stat-label">Disponível</div></div>
        <div class="sz-mb-stat"><div class="sz-mb-stat-num">R$ <?php echo number_format($total_conf,2,',','.'); ?></div><div class="sz-mb-stat-label">A conciliar</div></div>
        <div class="sz-mb-stat"><div class="sz-mb-stat-num">R$ <?php echo number_format($total_pago,2,',','.'); ?></div><div class="sz-mb-stat-label">Pago acumulado</div></div>
    </div>

    <div class="sz-mb-card">
        <p class="sz-mb-wallet-note">Digite o valor pago. O sistema soma no histórico de pagamentos e deixa o restante em disponível.</p>
        <table class="sz-mb-table">
            <thead><tr><th>Motoboy</th><th>Zona</th><th>Telefone</th><th>Disponível</th><th>A conciliar</th><th>Pago</th><th>Lançamentos</th><th>Pagamento</th><th>Histórico</th></tr></thead>
            <tbody>
            <?php foreach ( $motoboys as $m ) : ?>
                <tr>
                    <td><strong><?php echo esc_html($m->nome); ?></strong><div class="sz-mb-soft">ID <?php echo (int)$m->id; ?></div></td>
                    <td><?php echo esc_html($m->zona_nome ?: '—'); ?></td>
                    <td><?php echo esc_html($m->telefone ?: '—'); ?></td>
                    <td style="font-weight:700;color:#16a34a">R$ <?php echo number_format((float)$m->saldo_disponivel,2,',','.'); ?></td>
                    <td style="font-weight:700;color:#E8650A">R$ <?php echo number_format((float)$m->saldo_em_conferencia,2,',','.'); ?></td>
                    <td>R$ <?php echo number_format((float)$m->saques_pagos,2,',','.'); ?></td>
                    <td><?php echo (int)$m->qtd_disponivel; ?> disp. / <?php echo (int)$m->qtd_conferencia; ?> conf. / <?php echo (int)$m->qtd_pago; ?> pagos</td>
                    <td>
                        <?php if ( (float) $m->saldo_disponivel > 0 ) : ?>
                        <form method="post" class="sz-mb-wallet-pay" onsubmit="return confirm('Registrar este pagamento para <?php echo esc_js($m->nome); ?>?')">
                            <?php wp_nonce_field( 'sz_mb_registrar_pagamento_valor' ); ?>
                            <input type="hidden" name="motoboy_id" value="<?php echo (int)$m->id; ?>">
                            <input name="valor_pago" inputmode="decimal" placeholder="Valor" value="<?php echo esc_attr(number_format((float)$m->saldo_disponivel,2,',','.')); ?>">
                            <input type="date" name="data_pagamento" value="<?php echo esc_attr(current_time('Y-m-d')); ?>">
                            <button class="sz-mb-btn sz-mb-btn-sm" name="sz_mb_registrar_pagamento_valor" value="1">Registrar</button>
                        </form>
                        <?php else : ?>—<?php endif; ?>
                    </td>
                    <td><a class="button" href="<?php echo esc_url(add_query_arg(['page'=>'senderzz','tab'=>'motoboy','mb_tab'=>'carteiras','historico_wallet'=>(int)$m->id], admin_url('admin.php'))); ?>">Histórico</a></td>
                </tr>
            <?php endforeach; ?>
            </tbody>
        </table>
    </div>
    <?php
    if ( isset( $_GET['historico_wallet'] ) ) :
        $mid = (int) $_GET['historico_wallet'];
        $mb_nome = $wpdb->get_var( $wpdb->prepare("SELECT nome FROM {$p}sz_motoboys WHERE id=%d", $mid) );
        $hist = $wpdb->get_results( $wpdb->prepare(
            "SELECT g.*, mp.wc_order_id, mp.status AS pedido_status, mp.pgto_dinheiro, mp.pgto_pix, mp.pgto_cartao, pg.data_pagamento, pg.valor_total AS valor_pagamento
               FROM {$p}sz_motoboy_ganhos g
          LEFT JOIN {$p}sz_motoboy_pedidos mp ON mp.id = g.pedido_id
          LEFT JOIN {$p}sz_motoboy_pagamentos pg ON pg.id = g.pagamento_id
              WHERE g.motoboy_id = %d
           ORDER BY g.created_at DESC, g.id DESC
              LIMIT 150", $mid
        ) );
        ?>
        <div class="sz-mb-card">
            <h2>Histórico da carteira — <?php echo esc_html($mb_nome ?: ('Motoboy '.$mid)); ?></h2>
            <table class="sz-mb-table">
                <thead><tr><th>Data</th><th>Pedido</th><th>Tipo</th><th>Valor ganho</th><th>Pago neste ganho</th><th>Saldo aberto</th><th>Data saque</th><th>Status</th><th>Recebido cliente</th></tr></thead>
                <tbody>
                <?php foreach ( $hist as $h ) : $recebido = (float)$h->pgto_dinheiro + (float)$h->pgto_pix + (float)$h->pgto_cartao; $aberto = max(0,(float)$h->valor-(float)$h->valor_pago); ?>
                    <tr>
                        <td><?php echo esc_html( date_i18n('d/m/Y H:i', strtotime($h->created_at)) ); ?></td>
                        <td>#<?php echo (int)($h->wc_order_id ?: $h->pedido_id); ?></td>
                        <td><?php echo esc_html($h->tipo); ?></td>
                        <td style="font-weight:700">R$ <?php echo number_format((float)$h->valor,2,',','.'); ?></td>
                        <td>R$ <?php echo number_format((float)$h->valor_pago,2,',','.'); ?></td>
                        <td>R$ <?php echo number_format($aberto,2,',','.'); ?></td>
                        <td><?php echo ! empty( $h->data_pagamento ) ? esc_html( date_i18n('d/m/Y', strtotime($h->data_pagamento)) ) : '—'; ?></td>
                        <td><?php echo esc_html($h->status); ?></td>
                        <td>R$ <?php echo number_format($recebido,2,',','.'); ?></td>
                    </tr>
                <?php endforeach; ?>
                </tbody>
            </table>
        </div>
    <?php endif;
}

// ─── Aba: Saques dos motoboys / PWA ──────────────────────────────────────────
function sz_mb_tab_saques(): void {
    global $wpdb;
    $p = $wpdb->prefix;

    // v345: limpa a fila antiga de pedidos/lançamentos de saque de motoboy uma única vez,
    // conforme solicitado. A carteira continua sendo recalculada pelos ganhos; esta aba volta a mostrar somente novos pedidos.
    $pg_table = $p . 'sz_motoboy_pagamentos';
    if ( ! get_option( 'sz_mb_saques_zerados_v345' ) && $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $pg_table ) ) ) {
        $wpdb->query( "TRUNCATE TABLE {$pg_table}" );
        update_option( 'sz_mb_saques_zerados_v345', current_time( 'mysql' ), false );
    }

    $rows = $wpdb->get_results(
        "SELECT pg.*, m.nome AS motoboy_nome, m.telefone
           FROM {$p}sz_motoboy_pagamentos pg
      LEFT JOIN {$p}sz_motoboys m ON m.id = pg.motoboy_id
       ORDER BY CASE pg.status WHEN 'aguardando' THEN 0 WHEN 'pago' THEN 1 ELSE 2 END, pg.created_at DESC, pg.id DESC
          LIMIT 200"
    ) ?: [];
    ?>
    <div class="sz-mb-card">
        <h2 style="margin:0 0 8px">Saques / pagamentos de motoboys</h2>
        <div class="sz-mb-soft">Fila zerada nesta versão. Novos pedidos de saque criados pelo PWA aparecerão aqui como aguardando.</div>
    </div>
    <div class="sz-mb-card">
        <table class="sz-mb-table">
            <thead><tr><th>ID</th><th>Motoboy</th><th>Telefone</th><th>Valor</th><th>Data saque</th><th>Status</th><th>Observação</th><th>Criado em</th></tr></thead>
            <tbody>
            <?php if ( ! $rows ) : ?><tr><td colspan="8" style="text-align:center;color:#64748b">Nenhum saque/pagamento registrado.</td></tr><?php endif; ?>
            <?php foreach ( $rows as $r ) : ?>
                <tr>
                    <td>#<?php echo (int)$r->id; ?></td>
                    <td><strong><?php echo esc_html($r->motoboy_nome ?: ('Motoboy '.$r->motoboy_id)); ?></strong><div class="sz-mb-soft">ID <?php echo (int)$r->motoboy_id; ?></div></td>
                    <td><?php echo esc_html($r->telefone ?: '—'); ?></td>
                    <td style="font-weight:700">R$ <?php echo number_format((float)$r->valor_total,2,',','.'); ?></td>
                    <td><?php echo esc_html( date_i18n('d/m/Y', strtotime($r->data_pagamento)) ); ?></td>
                    <td><?php echo esc_html($r->status); ?></td>
                    <td><?php echo esc_html($r->obs ?: '—'); ?></td>
                    <td><?php echo esc_html( date_i18n('d/m/Y H:i', strtotime($r->created_at)) ); ?></td>
                </tr>
            <?php endforeach; ?>
            </tbody>
        </table>
    </div>
    <?php
}

// ─── Aba: Conciliação bancária ────────────────────────────────────────────────
function sz_mb_tab_conciliacao(): void {
    global $wpdb;
    $p = $wpdb->prefix;

    if ( function_exists( 'sz_mbw_rebuild_all_from_orders' ) ) { sz_mbw_rebuild_all_from_orders(); }

    $tz = new DateTimeZone( 'America/Sao_Paulo' );
    $hoje = new DateTimeImmutable( 'now', $tz );
    $default_fim = $hoje->format( 'Y-m-d' );
    $default_inicio = $hoje->modify( '-7 days' )->format( 'Y-m-d' );
    $inicio = sanitize_text_field( $_GET['data_inicio'] ?? $_GET['data_de'] ?? $_GET['data_conc'] ?? $default_inicio );
    $fim    = sanitize_text_field( $_GET['data_fim'] ?? $_GET['data_ate'] ?? $_GET['data_conc'] ?? $default_fim );
    if ( ! preg_match( '/^\d{4}-\d{2}-\d{2}$/', $inicio ) ) { $inicio = $default_inicio; }
    if ( ! preg_match( '/^\d{4}-\d{2}-\d{2}$/', $fim ) ) { $fim = $default_fim; }
    if ( strtotime( $inicio ) > strtotime( $fim ) ) { $tmp = $inicio; $inicio = $fim; $fim = $tmp; }

    if ( isset( $_POST['sz_mb_conciliar_pedido'] ) ) {
        check_admin_referer( 'sz_mb_conciliar_pedido' );
        $wc_order_id = absint( $_POST['wc_order_id'] ?? 0 );
        if ( $wc_order_id > 0 ) {
            $ganho = $wpdb->get_row( $wpdb->prepare( "SELECT * FROM {$p}sz_motoboy_ganhos WHERE wc_order_id=%d AND status='pendente' LIMIT 1", $wc_order_id ) );
            if ( $ganho ) {
                $wpdb->update( $p . 'sz_motoboy_ganhos', [ 'status' => 'disponivel' ], [ 'id' => (int) $ganho->id ] );
                echo '<div class="sz-mb-alert-ok">✅ Pedido #' . (int) $wc_order_id . ' conciliado individualmente e liberado para a carteira do motoboy.</div>';
            } else {
                echo '<div class="sz-mb-alert-err">⚠️ Pedido #' . (int) $wc_order_id . ' não possui lançamento pendente para conciliar.</div>';
            }
        }
    }

    $pedidos = $wpdb->get_results( $wpdb->prepare(
        "SELECT mp.*, m.nome AS motoboy_nome, z.nome AS zona_nome, g.id AS ganho_id, g.valor AS valor_ganho, g.status AS ganho_status,
                DATE(COALESCE(mp.baixa_at, mp.ts_entregue, mp.ts_frustrado, mp.updated_at, mp.created_at)) AS data_baixa,
                CASE WHEN mp.status='frustrado' THEN COALESCE(mp.valor_taxa_frustrado,0) ELSE COALESCE(mp.pgto_dinheiro,0)+COALESCE(mp.pgto_pix,0)+COALESCE(mp.pgto_cartao,0) END AS total_validado
           FROM {$p}sz_motoboy_pedidos mp
      LEFT JOIN {$p}sz_motoboys m ON m.id = COALESCE(NULLIF(mp.baixa_motoboy_id,0), mp.motoboy_id)
      LEFT JOIN {$p}sz_motoboy_zonas z ON z.id = m.zona_id
      LEFT JOIN {$p}sz_motoboy_ganhos g ON g.wc_order_id = mp.wc_order_id AND g.pedido_id = mp.id AND g.tipo = CASE WHEN mp.status='frustrado' THEN 'frustrado' ELSE 'entrega' END
          WHERE mp.status IN ('entregue','frustrado')
            AND DATE(COALESCE(mp.baixa_at, mp.ts_entregue, mp.ts_frustrado, mp.updated_at, mp.created_at)) BETWEEN %s AND %s
          ORDER BY data_baixa DESC, mp.wc_order_id DESC",
        $inicio, $fim
    ) ) ?: [];

    $totais = [ 'pedidos'=>0, 'entregues'=>0, 'frustrados'=>0, 'dinheiro'=>0.0, 'pix'=>0.0, 'cartao'=>0.0, 'taxa_frustrado'=>0.0, 'total'=>0.0, 'aguardando'=>0.0 ];
    foreach ( $pedidos as $r ) {
        $totais['pedidos']++;
        if ( (string) $r->status === 'frustrado' ) { $totais['frustrados']++; $totais['taxa_frustrado'] += (float) $r->valor_taxa_frustrado; }
        else { $totais['entregues']++; $totais['dinheiro'] += (float) $r->pgto_dinheiro; $totais['pix'] += (float) $r->pgto_pix; $totais['cartao'] += (float) $r->pgto_cartao; }
        $totais['total'] += (float) $r->total_validado;
        if ( (string) $r->ganho_status === 'pendente' ) { $totais['aguardando'] += (float) $r->total_validado; }
    }
    ?>
    <div class="sz-mb-card" style="margin-bottom:16px">
        <form method="get" style="display:flex;gap:10px;align-items:center;flex-wrap:wrap">
            <input type="hidden" name="page" value="senderzz">
            <input type="hidden" name="area" value="cod">
            <input type="hidden" name="tab" value="cod-pedidos">
            <input type="hidden" name="mb_tab" value="conciliacao">
            <label style="font-size:var(--sz-text-base);font-weight:600">De:</label>
            <input type="date" name="data_inicio" value="<?php echo esc_attr($inicio); ?>" style="padding:6px 10px;border:1px solid #d1d5db;border-radius:8px;font-size:var(--sz-text-base)">
            <label style="font-size:var(--sz-text-base);font-weight:600">Até:</label>
            <input type="date" name="data_fim" value="<?php echo esc_attr($fim); ?>" style="padding:6px 10px;border:1px solid #d1d5db;border-radius:8px;font-size:var(--sz-text-base)">
            <button class="sz-mb-btn" type="submit">Filtrar</button>
            <a class="sz-mb-btn" href="<?php echo esc_url(add_query_arg(['page'=>'senderzz','area'=>'cod','tab'=>'cod-pedidos','mb_tab'=>'conciliacao','data_inicio'=>$default_inicio,'data_fim'=>$default_fim], admin_url('admin.php'))); ?>">Últimos 7 dias</a>
            <?php if($pedidos): ?><button class="sz-mb-btn" type="button" onclick="szMbExportCSV()">↓ CSV</button><?php endif; ?>
        </form>
    </div>

    <div class="sz-mb-grid-4" style="margin-bottom:20px">
        <div class="sz-mb-stat"><div class="sz-mb-stat-num"><?php echo (int)$totais['pedidos']; ?></div><div class="sz-mb-stat-label">Pedidos no período</div></div>
        <div class="sz-mb-stat"><div class="sz-mb-stat-num" style="color:#16a34a"><?php echo (int)$totais['entregues']; ?></div><div class="sz-mb-stat-label">Entregues</div></div>
        <div class="sz-mb-stat"><div class="sz-mb-stat-num" style="color:#dc2626"><?php echo (int)$totais['frustrados']; ?></div><div class="sz-mb-stat-label">Frustrados</div></div>
        <div class="sz-mb-stat"><div class="sz-mb-stat-num" style="color:#E8650A">R$ <?php echo number_format((float)$totais['aguardando'],2,',','.'); ?></div><div class="sz-mb-stat-label">Aguardando conciliação</div></div>
    </div>
    <div class="sz-mb-grid-4" style="margin-bottom:20px">
        <div class="sz-mb-stat"><div class="sz-mb-stat-num">R$ <?php echo number_format((float)$totais['dinheiro'],2,',','.'); ?></div><div class="sz-mb-stat-label">Dinheiro</div></div>
        <div class="sz-mb-stat"><div class="sz-mb-stat-num">R$ <?php echo number_format((float)$totais['pix'],2,',','.'); ?></div><div class="sz-mb-stat-label">PIX</div></div>
        <div class="sz-mb-stat"><div class="sz-mb-stat-num">R$ <?php echo number_format((float)$totais['cartao'],2,',','.'); ?></div><div class="sz-mb-stat-label">Cartão</div></div>
        <div class="sz-mb-stat"><div class="sz-mb-stat-num" style="color:#dc2626">R$ <?php echo number_format((float)$totais['taxa_frustrado'],2,',','.'); ?></div><div class="sz-mb-stat-label">Taxa de frustrado</div></div>
    </div>

    <div class="sz-mb-card">
        <h2 style="margin-bottom:6px">Conciliação por pedido — <?php echo esc_html(date('d/m/Y', strtotime($inicio)) . ' a ' . date('d/m/Y', strtotime($fim))); ?></h2>
        <div class="sz-mb-soft" style="margin-bottom:14px">Cada linha é um pedido. Entregas validam o valor recebido do cliente; frustrados validam somente a taxa de frustrado gravada no pedido. Taxas novas não alteram pedidos antigos.</div>
        <?php if(!$pedidos): ?>
            <p style="color:#6b7280;text-align:center">Nenhum pedido válido no período.</p>
        <?php else: ?>
        <table class="sz-mb-table" id="sz-conc-table">
            <thead><tr><th>Data</th><th>Pedido</th><th>Motoboy</th><th>Zona</th><th>Cliente</th><th>Status</th><th>Forma</th><th>Dinheiro</th><th>PIX</th><th>Cartão</th><th>Taxa frustrado</th><th>Total validado</th><th>Conciliação</th><th>Ação</th></tr></thead>
            <tbody>
            <?php foreach($pedidos as $d):
                $formas = [];
                if ( (float) $d->pgto_dinheiro > 0 ) { $formas[] = 'Dinheiro'; }
                if ( (float) $d->pgto_pix > 0 ) { $formas[] = 'PIX'; }
                if ( (float) $d->pgto_cartao > 0 ) { $formas[] = 'Cartão'; }
                if ( (string) $d->status === 'frustrado' ) { $formas[] = 'Frustrado'; }
                $forma = $formas ? implode( ' + ', $formas ) : '—';
                $conciliado = in_array( (string) $d->ganho_status, [ 'disponivel', 'pago' ], true );
            ?>
                <tr>
                    <td><?php echo esc_html(date('d/m/Y', strtotime($d->data_baixa))); ?></td>
                    <td><a href="<?php echo esc_url( admin_url( 'post.php?post=' . (int) $d->wc_order_id . '&action=edit' ) ); ?>">#<?php echo (int)$d->wc_order_id; ?></a></td>
                    <td><strong><?php echo esc_html($d->motoboy_nome ?: '—'); ?></strong></td>
                    <td><?php echo esc_html($d->zona_nome ?: '—'); ?></td>
                    <td><?php echo esc_html($d->dest_nome ?: '—'); ?></td>
                    <td><?php echo esc_html(strtoupper((string)$d->status)); ?></td>
                    <td><?php echo esc_html($forma); ?></td>
                    <td>R$ <?php echo number_format((float)$d->pgto_dinheiro,2,',','.'); ?></td>
                    <td>R$ <?php echo number_format((float)$d->pgto_pix,2,',','.'); ?></td>
                    <td>R$ <?php echo number_format((float)$d->pgto_cartao,2,',','.'); ?></td>
                    <td style="color:#dc2626;font-weight:700">R$ <?php echo number_format((float)$d->valor_taxa_frustrado,2,',','.'); ?></td>
                    <td style="font-weight:700;color:#E8650A">R$ <?php echo number_format((float)$d->total_validado,2,',','.'); ?></td>
                    <td><?php echo $conciliado ? '<span style="color:#16a34a;font-weight:700">Conciliado</span>' : '<span style="color:#E8650A;font-weight:700">Aguardando</span>'; ?></td>
                    <td>
                        <?php if ( $conciliado ) : ?>
                            <span class="sz-mb-soft">Liberado</span>
                        <?php else : ?>
                            <form method="post" onsubmit="return confirm('Conciliar somente o pedido #<?php echo (int)$d->wc_order_id; ?> e liberar a taxa para a carteira?')">
                                <?php wp_nonce_field('sz_mb_conciliar_pedido'); ?>
                                <input type="hidden" name="sz_mb_conciliar_pedido" value="1">
                                <input type="hidden" name="wc_order_id" value="<?php echo (int)$d->wc_order_id; ?>">
                                <button class="sz-mb-btn sz-mb-btn-sm">Conciliar pedido</button>
                            </form>
                        <?php endif; ?>
                    </td>
                </tr>
            <?php endforeach; ?>
            </tbody>
        </table>
        <?php endif; ?>
    </div>
    <script>
    function szMbExportCSV() {
        const table = document.getElementById('sz-conc-table');
        if (!table) return;
        let csv = [];
        table.querySelectorAll('tr').forEach(row => {
            const cols = [...row.querySelectorAll('th,td')].map(c => '"' + c.innerText.replace(/"/g,'""').replace(/\n/g,' ') + '"');
            csv.push(cols.join(','));
        });
        const blob = new Blob(['\uFEFF'+csv.join('\n')], {type:'text/csv;charset=utf-8'});
        const a = document.createElement('a');
        a.href = URL.createObjectURL(blob);
        a.download = 'conciliacao-pedidos-<?php echo esc_js($inicio . "-" . $fim); ?>.csv';
        a.click();
    }
    </script>
    <?php
}

// ─── CSS extra para grid-4 ────────────────────────────────────────────────────
add_action('admin_head', function() {
    $screen = get_current_screen();
    if (!$screen || (strpos($screen->id,'sz-motoboy') === false && strpos($screen->id,'senderzz') === false)) return;
    echo '<style>
    .sz-mb-grid-4{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:14px}
    @media(max-width:900px){.sz-mb-grid-4{grid-template-columns:repeat(2,1fr)}}

    @media(max-width:1180px){.sz-mb-custody-kpis{grid-template-columns:repeat(3,minmax(0,1fr))}.sz-mb-custody-form-route,.sz-mb-custody-form-return{grid-template-columns:1fr 1fr}}
    @media(max-width:720px){.sz-mb-custody-kpis{grid-template-columns:1fr 1fr}.sz-mb-custody-form-route,.sz-mb-custody-form-return{grid-template-columns:1fr}.sz-mb-section-title{align-items:flex-start}.sz-mb-accordion summary{align-items:flex-start}.sz-mb-kpi-card{min-height:84px}}

    </style>';
});

// ─── Push subscribe form + service worker para alan ───────────────────────────
add_action('admin_footer', function() {
    $screen = get_current_screen();
    if (!$screen || strpos($screen->id,'sz-motoboy') === false) return;
    $vapid_pub = get_option('sz_notif_vapid_public','');
    if (!$vapid_pub) return;
    $rest_url  = esc_js(rest_url('sz-motoboy/v1/alan/push-subscribe'));
    $nonce     = wp_create_nonce('wp_rest');
    ?>
    <script>
    (async function szMbAdminPush() {
        if (!('serviceWorker' in navigator) || !('PushManager' in window)) return;
        const vapidKey = '<?php echo esc_js($vapid_pub); ?>';
        function urlBase64ToUint8Array(base64String) {
            const padding = '='.repeat((4 - base64String.length % 4) % 4);
            const base64 = (base64String + padding).replace(/-/g,'+').replace(/_/g,'/');
            const rawData = window.atob(base64);
            return Uint8Array.from([...rawData].map(c => c.charCodeAt(0)));
        }
        try {
            const reg = await navigator.serviceWorker.register('/wp-content/plugins/senderzz-logistics/assets/sw-admin.js');
            const existing = await reg.pushManager.getSubscription();
            if (existing) return;
            const perm = await Notification.requestPermission();
            if (perm !== 'granted') return;
            const sub = await reg.pushManager.subscribe({
                userVisibleOnly: true,
                applicationServerKey: urlBase64ToUint8Array(vapidKey)
            });
            await fetch('<?php echo $rest_url; ?>', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json', 'X-WP-Nonce': '<?php echo $nonce; ?>' },
                body: JSON.stringify(sub.toJSON())
            });
        } catch(e) { console.log('Push admin setup:', e); }
    })();
    </script>
    <?php
});
